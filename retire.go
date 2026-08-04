// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"iter"
	"time"
)

// The children of a node which say that it stopped existing, per specification
// section 6.7.
const (
	retiredChild      = "retired"
	supersededByChild = "superseded-by"
)

// Retirement is how a node stopped existing: when, why, and what stands in its
// place where anything does.
//
// Retiring is not deleting. The id stays in the graph, is never removed and is
// never issued again to a different thing
// ([0002](docs/decisions/0002-immutable-id-mutable-label.md)), so a reference
// written years ago either resolves to the thing it always named or resolves to
// a retired node which says what happened to it. A model which deleted instead
// would answer the second case with silence.
//
// The reason is required and the replacement is not, which is the asymmetry the
// format is built on: a retirement with no reason is a deletion wearing a hat,
// and a thing which stopped existing did not necessarily get replaced by
// something.
//
// The zero value is the retirement of a node which was not retired, which is
// what [SemanticNode.Retirement] reports with a false beside it.
type Retirement struct {
	// date is when the thing ceased to exist in the model.
	date time.Time

	// reason is why, in the author's words.
	reason string

	// supersededBy is the node which replaced it, and hasReplacement whether one
	// did. A `superseded-by` which no id could be read from leaves both as they
	// are, for the reason every other axis of a form does: the diagnostic
	// already carries what was written, and an empty id reported as present
	// names no node.
	supersededBy   ID
	hasReplacement bool

	// span is where the `retired` child was written, which is what a diagnostic
	// about the retirement as a whole points at.
	span Span
}

// Date returns when the thing ceased to exist in the model.
func (r Retirement) Date() time.Time { return r.date }

// Reason returns why it was retired, in the author's words.
func (r Retirement) Reason() string { return r.reason }

// SupersededBy returns the id of the node which replaced this one, and whether
// one did.
//
// A replacement is optional here — unlike on a claim, where a deprecation
// naming nothing is a load error — because a thing which stopped existing did
// not necessarily get replaced. A wall which was demolished was replaced by
// nothing at all, and saying so is not the same as failing to say what replaced
// it.
func (r Retirement) SupersededBy() (ID, bool) { return r.supersededBy, r.hasReplacement }

// Span returns where the `retired` child was written.
func (r Retirement) Span() Span { return r.span }

// Retirement returns how the node was retired, and whether it was.
//
// A retired node is still a node the model holds: it is reachable by its id,
// carries every claim it ever carried, and is what a reference to that id
// resolves to. What retirement says is that the thing it named stopped
// existing, which is a state of the model rather than an absence from it.
func (n *SemanticNode) Retirement() (Retirement, bool) {
	if n == nil || n.retirement == nil {
		return Retirement{}, false
	}
	return *n.retirement, true
}

// Retired reports whether the node was retired.
//
// It is [SemanticNode.Retirement] for the callers which only want the question
// answered — a listing which leaves retired nodes out unless it was asked for
// them reads better as a predicate than as a two-result call whose first result
// it drops.
func (n *SemanticNode) Retired() bool { return n != nil && n.retirement != nil }

// nodeSupersession is one `superseded-by` written inside a `retired`, as it was
// written: the node it was written on, where the replacement is named, and
// where the node itself is named.
//
// It is recorded as it is read and checked once the whole tree has been, for
// the reason a containment is: a node retired in the first file the walk
// reaches may be replaced by one written in the last, and a loader which
// resolved as it read would report it missing for no reason but the order the
// directory happened to be listed in.
type nodeSupersession struct {
	// node is the node which was retired.
	node *SemanticNode

	// at is where the replacement is named.
	at Span

	// where is where the retired node itself is named, which is the other end a
	// diagnostic about the reference points back at.
	where Span
}

// retire reads the `retired` child of a node form.
//
// Every axis is read whatever happened to the ones before it, for the reason
// [nodeLoader.declare] reads a node's axes that way: a retirement with an
// unreadable date still has a reason worth reading, and bailing out on the
// first would turn fixing a file into a guessing loop.
//
// The structural rules — that a date and a reason are written exactly once each
// and a replacement at most once — are the validator's, and a form which broke
// them never reaches here.
func (l *nodeLoader) retire(d *nodeDeclaration, form, child *Node) {
	retirement := Retirement{span: child.Span}

	if arg, ok := argumentOf(child, "date"); ok {
		retirement.date, _ = l.date(arg, "a date string")
	}

	if arg, ok := argumentOf(child, "reason"); ok {
		retirement.reason, _ = l.text(arg, "a string")
	}

	if arg, ok := argumentOf(child, supersededByChild); ok {
		if replacement, ok := l.id(arg, "a node id"); ok {
			retirement.supersededBy, retirement.hasReplacement = replacement, true
			l.registered(l.registry, replacement, arg.Span)

			where := d.id
			if where == (Span{}) {
				where = tagSpan(form)
			}

			l.supersessions = append(l.supersessions, nodeSupersession{node: d.node, at: arg.Span, where: where})
		}
	}

	d.node.retirement = &retirement
}

// supersede checks that every node supersession the tree wrote names a node
// this model holds, and that no node was replaced by itself.
//
// A node replaced by itself is reported as that rather than as a node which
// still exists: it says the thing stopped existing and stands in its own place,
// which is not a state anything can be in and is not what the author meant by
// either half of it.
func (l *nodeLoader) supersede() {
	for _, written := range l.supersessions {
		node := written.node

		retirement, _ := node.Retirement()
		replacement, _ := retirement.SupersededBy()

		if replacement == node.id && node.id != "" {
			l.add(Diagnostic{
				Severity: SeverityError,
				Span:     written.at,
				Message: fmt.Sprintf(
					"expected the node which replaced %s, found %s, which is the node itself",
					nodeName(node), replacement,
				),
				Hint: "a retired node is replaced by another node, and a node which stands in its own place did not stop existing",
			})
			continue
		}

		if _, ok := l.nodes.Node(replacement); ok {
			continue
		}

		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     written.at,
			Message: fmt.Sprintf(
				"expected a node id something in this model holds, found %s, which names no node",
				replacement,
			),
			Hint:    "a retirement says what replaced the thing as (superseded-by <node-id>); a retirement which replaced it with nothing writes no such child at all",
			Related: []RelatedLocation{{Span: written.where, Message: "the retired node is written here"}},
		})
	}
}

// Reference is one place the model names an id from somewhere else.
//
// It is what "who points at this" means: the entity which wrote the reference,
// the relation it wrote it as, and where that entity is defined. A caller
// refusing to retire something still referenced reports these, because "still
// referenced" without saying by what leaves the author reading every file to
// find out.
type Reference struct {
	// From is the entity which made the reference.
	From ID

	// Relation is the child it was written as — `within`, `member-of`,
	// `boundary`, `superseded-by`, `vertices`, `edges` or `backed-by` — which is
	// what says how the two things are related and what has to change for the
	// reference to go away.
	Relation string

	// Span is where the entity making the reference is named, which is what a
	// message about it points at.
	Span Span
}

// References iterates every reference the model makes to id, from entities
// other than the one it names.
//
// A reference an entity makes to itself is not one: a node inside itself and a
// node which replaced itself are each a diagnostic of their own, and reporting
// them here as well would make a thing its own referrer and refuse a retirement
// on the strength of it.
//
// The order is the order the walk reached the referring entities, family by
// family, so a listing built from it is the same on every run.
//
// The scan is over every entity rather than over an index, because nothing else
// needs the reverse direction: it is asked once, by a change which is about to
// refuse itself, and an index maintained for it would have to stay true through
// every mutation of every transaction.
func (g *Graph) References(id ID) iter.Seq[Reference] {
	return func(yield func(Reference) bool) {
		if g == nil || id == "" {
			return
		}

		for node := range g.nodes.All() {
			if node.id == id {
				continue
			}

			// The span is looked up where a reference is found rather than for
			// every node, because very nearly none of them references any one
			// thing and the lookup is what the scan would otherwise spend most
			// of its time on.
			made := func(relation string) bool {
				return yield(Reference{From: node.id, Relation: relation, Span: g.nodes.named(node)})
			}

			if within, ok := node.Within(); ok && within == id && !made(withinChild) {
				return
			}

			for _, zone := range node.zones {
				if zone == id && !made(memberOfChild) {
					return
				}
			}

			for _, loop := range node.boundaries {
				if loop == id && !made(boundaryChild) {
					return
				}
			}

			retirement, _ := node.Retirement()
			if replacement, ok := retirement.SupersededBy(); ok && replacement == id && !made(supersededByChild) {
				return
			}
		}

		for edge := range g.topology.Edges() {
			if edge.id == id {
				continue
			}

			made := func(relation string) bool {
				return yield(Reference{From: edge.id, Relation: relation, Span: edge.Span()})
			}

			if start, end := edge.Vertices(); (start == id || end == id) && !made(verticesChild) {
				return
			}

			for _, backing := range edge.backing {
				if backing == id && !made(backedByChild) {
					return
				}
			}
		}

		for loop := range g.topology.Loops() {
			if loop.id == id {
				continue
			}

			for _, edge := range loop.edges {
				if edge == id && !yield(Reference{From: loop.id, Relation: edgesChild, Span: loop.Span()}) {
					return
				}
			}
		}
	}
}
