// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"iter"
	"slices"
	"strings"
)

// Relation is which of the two relations between semantic nodes produced a
// traversal result.
//
// The two are not interchangeable and are never collapsed into one. Containment
// is physical enclosure and nests strictly: a node is written inside at most one
// other, and following those references reaches a root. Membership is arbitrary
// grouping and is many to many: a node is written into any number of zones, and
// a zone holds any number of nodes.
//
// Every traversal in this file yields its results labelled with the relation
// which produced them, because a caller which cannot tell the two apart cannot
// trust either. "The things in this storey" and "the things grouped with this
// storey" are different questions, and a result which answers one while looking
// like the other is worse than no result at all.
type Relation string

// The relations, which are the two the format writes between semantic nodes.
const (
	// RelationContainment is the relation `(within <node-id>)` writes: the node
	// which strictly contains this one.
	RelationContainment Relation = "containment"

	// RelationMembership is the relation `(member-of <zone-id>)` writes: a zone
	// this node is grouped into.
	RelationMembership Relation = "membership"
)

// Related is one node a traversal reached, together with the relation which
// reached it.
//
// It is a value rather than a bare node so that the label travels with the
// result. A traversal which handed back nodes would leave the caller to remember
// which question it asked, and a wall belonging to two rooms and a circuit
// belonging to no room are exactly the cases where remembering wrong is easy.
//
// The zero value holds no node and no relation, which no traversal yields.
type Related struct {
	// node is the node which was reached.
	node *SemanticNode

	// relation is which relation reached it.
	relation Relation
}

// Node returns the node the traversal reached.
func (r Related) Node() *SemanticNode { return r.node }

// Relation returns which relation reached it, which is what says whether the
// result means physical enclosure or arbitrary grouping.
func (r Related) Relation() Relation { return r.relation }

// nests is the containment hierarchy: the kinds a node of each kind may be
// written within.
//
// It is compiled in for the reason [Kind] itself is. Which kinds nest inside
// which is a structural fact about the closed set of kinds rather than
// vocabulary about any one subject, so it is fixed by this engine and not by
// registry data
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
//
// A Site is the root: nothing contains one. A Zone is not in the hierarchy at
// all, in either direction — a zone groups nodes by membership, and a `within`
// naming one or written on one is the exact confusion this file exists to keep
// out of the model.
//
// Space nests inside Space and Element inside Element because both genuinely
// do: an alcove is a space inside a room, and a mullion is part of a curtain
// wall. Neither weakens strict nesting, which is about a node having one parent
// rather than about the parent being of a different kind.
var nests = map[Kind][]Kind{
	KindZone:      nil,
	KindSite:      nil,
	KindBuilding:  {KindSite},
	KindStorey:    {KindBuilding},
	KindSpace:     {KindStorey, KindSpace},
	KindElement:   {KindSite, KindBuilding, KindStorey, KindSpace, KindElement},
	KindInterface: {KindSite, KindBuilding, KindStorey, KindSpace},
}

// Nests reports whether the containment hierarchy permits a node of kind child
// to be written within a node of kind parent.
//
// It is the whole of what the engine knows about which kinds nest. A pairing it
// rejects is a load error rather than a warning, because a model in which a
// storey sits inside a room is a model whose traversals answer nonsense
// confidently.
func Nests(child, parent Kind) bool { return slices.Contains(nests[child], parent) }

// Within returns the node which strictly contains node, labelled with the
// relation which produced it, and whether the model holds one.
//
// A node with no parent is ordinary rather than incomplete: a site is the root
// of its hierarchy and a circuit group sits in no hierarchy at all, so the
// second result is a state and not a failure.
//
// It is one step outward rather than the whole chain, which is what makes
// containment walkable a level at a time. [Nodes.Ancestors] follows it to the
// root.
//
// A `within` naming a node this model does not hold is a load diagnostic rather
// than a state a caller has to interpret, and the reference is not followed
// here: a caller which loaded clean never sees a false for that reason.
func (n *Nodes) Within(node *SemanticNode) (Related, bool) {
	parent, ok := n.parent(node)
	if !ok {
		return Related{}, false
	}
	return Related{node: parent, relation: RelationContainment}, true
}

// parent is [Nodes.Within] without the label, for the walks which need the node
// itself.
func (n *Nodes) parent(node *SemanticNode) (*SemanticNode, bool) {
	if n == nil || node == nil || !node.hasWithin {
		return nil, false
	}
	return n.Node(node.within)
}

// Contains iterates the nodes written directly within node, in the order the
// walk read them, each labelled with the relation which produced it.
//
// It is the reverse of [Nodes.Within] and is one level deep. The reference is
// written on the contained node, so this direction is not written anywhere; it
// is indexed at load, because computing it by scanning every node per question
// would make walking a hierarchy quadratic in the size of the model.
//
// A node written within itself is a load error rather than its own child, and
// is not yielded here: a caller iterating the contents of a node would otherwise
// meet the node itself, and every walk below it would be infinite.
func (n *Nodes) Contains(node *SemanticNode) iter.Seq[Related] {
	return func(yield func(Related) bool) {
		if n == nil || node == nil || node.id == "" {
			return
		}

		for _, contained := range n.contained[node.id] {
			if contained == node {
				continue
			}
			if !yield(Related{node: contained, relation: RelationContainment}) {
				return
			}
		}
	}
}

// Ancestors iterates the nodes which contain node, nearest first, each labelled
// with the relation which produced it.
//
// It is [Nodes.Within] followed to the root: the node this one sits in, then the
// node that one sits in, and so on to the node nothing contains. That walk is
// what answers "which building is this outlet in" without the caller writing the
// loop and deciding for itself when to stop.
//
// Each node comes back once. A cyclic containment is a load diagnostic and still
// a model this can be asked of, so the walk stops when it meets a node it has
// already reached rather than spinning.
func (n *Nodes) Ancestors(node *SemanticNode) iter.Seq[Related] {
	return func(yield func(Related) bool) {
		if n == nil || node == nil {
			return
		}

		seen := map[*SemanticNode]bool{node: true}

		for {
			parent, ok := n.parent(node)
			if !ok || seen[parent] {
				return
			}
			seen[parent] = true

			if !yield(Related{node: parent, relation: RelationContainment}) {
				return
			}
			node = parent
		}
	}
}

// Descendants iterates everything contained by node, however deep, each labelled
// with the relation which produced it.
//
// It is [Nodes.Contains] followed all the way down, depth first, and within one
// level in the order the walk read the nodes. That order is deterministic, so
// anything built from it diffs against the last run's.
//
// Each node comes back once, and a node already reached is not walked into
// twice, so a cyclic containment — a load diagnostic, and still a model this can
// be asked of — terminates here rather than spinning.
func (n *Nodes) Descendants(node *SemanticNode) iter.Seq[Related] {
	return func(yield func(Related) bool) {
		if n == nil || node == nil {
			return
		}

		seen := map[*SemanticNode]bool{node: true}

		var walk func(*SemanticNode) bool
		walk = func(node *SemanticNode) bool {
			for contained := range n.Contains(node) {
				if seen[contained.node] {
					continue
				}
				seen[contained.node] = true

				if !yield(contained) || !walk(contained.node) {
					return false
				}
			}
			return true
		}

		walk(node)
	}
}

// Zones iterates the zones node is a member of, in the order they were written,
// each labelled with the relation which produced it.
//
// Membership is many to many and says nothing about where the node is. A wall
// between two rooms belongs to the zones of both, a circuit group belongs to
// zones and sits in no hierarchy, and neither is a special case. Nothing here
// reads the containment references, so a node's zones are its own and not its
// parent's.
//
// A `member-of` naming a node this model does not hold, or naming one which is
// not a Zone, is a load diagnostic and is not followed here.
func (n *Nodes) Zones(node *SemanticNode) iter.Seq[Related] {
	return func(yield func(Related) bool) {
		if n == nil || node == nil {
			return
		}

		for _, id := range node.zones {
			zone, ok := n.Node(id)
			if !ok || zone == node {
				continue
			}
			if !yield(Related{node: zone, relation: RelationMembership}) {
				return
			}
		}
	}
}

// Members iterates the nodes which are members of the zone, in the order the
// walk read them, each labelled with the relation which produced it.
//
// It is the reverse of [Nodes.Zones], and is indexed at load for the reason
// [Nodes.Contains] is: the reference is written on the member, so this direction
// is written nowhere.
//
// Membership never reaches through containment. The members of a storey's zone
// are the nodes which wrote `(member-of ...)` naming it, and not the nodes which
// happen to sit inside one of them — which is the whole of keeping "is a member
// of" from quietly meaning "is inside".
func (n *Nodes) Members(zone *SemanticNode) iter.Seq[Related] {
	return func(yield func(Related) bool) {
		if n == nil || zone == nil || zone.id == "" {
			return
		}

		for _, member := range n.members[zone.id] {
			if member == zone {
				continue
			}
			if !yield(Related{node: member, relation: RelationMembership}) {
				return
			}
		}
	}
}

// link indexes both relations by the node each reference names, which is what
// makes them walkable in the direction nothing is written in.
//
// Only a reference which resolves is indexed. A `within` or a `member-of`
// naming a node this model does not hold is a load diagnostic, and an index
// entry for it would be an edge with one end.
//
// A reference which resolves is indexed whatever else is wrong with it. A storey
// written inside a room is a load error and is still what the file says, and a
// traversal which quietly dropped the edge would disagree with the source
// somebody is being asked to fix.
func (n *Nodes) link() {
	if n == nil {
		return
	}

	n.contained = make(map[ID][]*SemanticNode)
	n.members = make(map[ID][]*SemanticNode)

	for _, node := range n.inOrder {
		if node.hasWithin {
			if _, ok := n.byID[node.within]; ok {
				n.contained[node.within] = append(n.contained[node.within], node)
			}
		}

		for _, zone := range node.zones {
			if _, ok := n.byID[zone]; ok {
				n.members[zone] = append(n.members[zone], node)
			}
		}
	}
}

// containment is one `within` as it was written: which node wrote it, where the
// id it names was written, and where the node itself is named.
//
// The spans are what the diagnostics point at, and a [SemanticNode] carries
// none of them: it holds the id of its parent, which is what a caller reads,
// rather than the position of the child it was written in.
type containment struct {
	// node is the node which wrote it.
	node *SemanticNode

	// at is where the id inside the `within` child was written, which is what a
	// diagnostic about the node it names points at.
	at Span

	// where is where the node itself is named, which is the other end such a
	// diagnostic points back at.
	where Span
}

// membership is one `member-of` as it was written, with the same three parts a
// [containment] has and the zone it names.
type membership struct {
	// node is the node which wrote it.
	node *SemanticNode

	// zone is the id it names.
	zone ID

	// at is where that id was written.
	at Span

	// where is where the node itself is named.
	where Span
}

// relate checks that the containment and the membership the tree wrote hold
// together: that every reference names a node, that containment nests the way
// the hierarchy permits, that following it terminates, and that membership
// names zones.
//
// It is a second pass because both relations are properties of the source tree
// rather than of a file: a space in the first file the walk reaches sits inside
// a storey which may be written in the last, and a loader which resolved as it
// read would report it missing for no reason but the order the directory
// happened to be listed in.
func (l *nodeLoader) relate() {
	l.nodes.link()

	for _, written := range l.containments {
		l.contained(written)
	}

	l.member()
	l.cycles()
}

// contained checks one written containment: that it names a node, that it does
// not name the node which wrote it, and that the hierarchy permits the pairing.
//
// The three are ordered rather than independent because they build on each
// other. A reference naming nothing has no kind to judge, and a node inside
// itself is reported as that rather than as a kind the hierarchy does not permit
// to contain itself, which says the same thing in more words and in the wrong
// vocabulary.
func (l *nodeLoader) contained(written containment) {
	node := written.node

	if node.within == node.id && node.id != "" {
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     written.at,
			Message: fmt.Sprintf(
				"expected the node which contains %s, found %s, which is the node itself",
				nodeName(node), node.within,
			),
			Hint: "a node is contained by another node; a node inside itself encloses itself, which nothing can",
		})
		return
	}

	parent, ok := l.nodes.Node(node.within)
	if !ok {
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     written.at,
			Message: fmt.Sprintf(
				"expected a node id something in this model holds, found %s, which names no node",
				node.within,
			),
			Hint:    "containment is written on the contained node as (within <node-id>), naming the node which strictly contains it",
			Related: []RelatedLocation{{Span: written.where, Message: "the contained node is written here"}},
		})
		return
	}

	// A node whose kind could not be read has no place in the hierarchy to
	// judge, and neither has a parent whose kind could not be read. Both are
	// already a diagnostic saying what was written where a kind belongs, and a
	// second one about the nesting it produced reports one mistake twice.
	if node.kind == "" || parent.kind == "" {
		return
	}

	if Nests(node.kind, parent.kind) {
		return
	}

	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     written.at,
		Message: fmt.Sprintf(
			"expected a kind the hierarchy permits to contain %s, found %s, which is %s",
			kindName(node.kind), parent.id, kindName(parent.kind),
		),
		Hint:    nestingHint(node.kind),
		Related: []RelatedLocation{{Span: l.named(parent), Message: fmt.Sprintf("the %s named as the parent is written here", parent.kind)}},
	})
}

// nestingHint says where a node of this kind does belong, which is what turns
// "not permitted" into something the author can act on.
func nestingHint(kind Kind) string {
	permitted := nests[kind]

	switch {
	case kind == KindZone:
		return "a Zone is never written within anything; a zone groups nodes by (member-of <zone-id>), which is a different relation"
	case len(permitted) == 0:
		return fmt.Sprintf("%s is a root of the containment hierarchy and is never written within anything", kindName(kind))
	}

	parents := make([]string, 0, len(permitted))
	for _, parent := range permitted {
		parents = append(parents, kindName(parent))
	}

	return fmt.Sprintf("%s is written within %s", kindName(kind), join(parents, "or"))
}

// kindName names a kind for a diagnostic, with the article it reads with, so
// that a sentence about one is a sentence rather than a template with a word
// slotted into it.
func kindName(kind Kind) string {
	return article(strings.ToLower(string(kind))) + " " + string(kind)
}

// named is where the id of a node was written, which is what a related location
// about that node points at.
//
// It is the id rather than the form, because a span over the whole form quotes a
// dozen lines to point at one. A node whose id is not indexed — one which wrote
// none, or one whose id was already taken — has only its form to point at.
func (l *nodeLoader) named(node *SemanticNode) Span {
	if at, ok := l.defined[node.id]; ok {
		return at
	}
	return node.span
}

// member checks every written membership: that it names a node, that the node it
// names is a Zone, and that one node does not name one zone twice.
//
// Membership is otherwise unconstrained, and deliberately so. Any node may be a
// member of any number of zones and a zone may hold any number of nodes,
// including none — a wall belonging to two rooms and a circuit belonging to no
// room are the ordinary cases rather than the exceptions. What is checked here
// is that the reference names what it says it names, which is a rule about the
// writing and not about the shape of the relation.
func (l *nodeLoader) member() {
	// firstNamed is where each node first named each zone, which is what a
	// repeated membership points its reader back at.
	type naming struct {
		node *SemanticNode
		zone ID
	}
	firstNamed := make(map[naming]Span, len(l.memberships))

	for _, written := range l.memberships {
		node := written.node

		if written.zone == node.id && node.id != "" {
			l.add(Diagnostic{
				Severity: SeverityError,
				Span:     written.at,
				Message: fmt.Sprintf(
					"expected a zone %s is a member of, found %s, which is the node itself",
					nodeName(node), written.zone,
				),
				Hint: "membership groups a node into a zone written beside it; a zone is not a member of itself",
			})
			continue
		}

		if first, ok := firstNamed[naming{node: node, zone: written.zone}]; ok {
			l.add(Diagnostic{
				Severity: SeverityError,
				Span:     written.at,
				Message: fmt.Sprintf(
					"expected a zone %s does not already name, found %s a second time",
					nodeName(node), written.zone,
				),
				Hint:    "membership is a relation and not a count; naming a zone twice says exactly what naming it once says",
				Related: []RelatedLocation{{Span: first, Message: "first named here"}},
			})
			continue
		}
		firstNamed[naming{node: node, zone: written.zone}] = written.at

		zone, ok := l.nodes.Node(written.zone)
		if !ok {
			l.add(Diagnostic{
				Severity: SeverityError,
				Span:     written.at,
				Message: fmt.Sprintf(
					"expected a node id something in this model holds, found %s, which names no node",
					written.zone,
				),
				Hint:    "membership is written on the member as (member-of <zone-id>), naming a node of kind Zone",
				Related: []RelatedLocation{{Span: written.where, Message: "the member is written here"}},
			})
			continue
		}

		// A zone whose kind could not be read is already a diagnostic saying so,
		// for the reason the hierarchy check leaves one alone.
		if zone.kind == "" || zone.kind == KindZone {
			continue
		}

		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     written.at,
			Message: fmt.Sprintf(
				"expected a node of kind %s, found %s, which is %s",
				KindZone, zone.id, kindName(zone.kind),
			),
			Hint:    "membership groups nodes into zones and never says where a node is; a node inside another is written (within <node-id>)",
			Related: []RelatedLocation{{Span: l.named(zone), Message: fmt.Sprintf("the %s named as the zone is written here", zone.kind)}},
		})
	}
}

// cycles reports every ring of nodes which contain one another.
//
// Containment is walked outward by following one reference per node, so the
// nodes form a graph in which every node has at most one parent. Walking from
// each node in turn and remembering the path is therefore the whole of finding a
// cycle: the walk either reaches a root, reaches a node an earlier walk already
// accounted for, or meets itself.
//
// Each ring is reported once, whichever of its nodes the walk reached first, and
// is named from the node of it which was read first. What is reported is a
// property of the model rather than of the order the walk happened to take.
func (l *nodeLoader) cycles() {
	written := make(map[*SemanticNode]containment, len(l.containments))
	for _, c := range l.containments {
		written[c.node] = c
	}

	order := make(map[*SemanticNode]int, len(l.nodes.inOrder))
	for i, node := range l.nodes.inOrder {
		order[node] = i
	}

	walked := make(map[*SemanticNode]bool)
	for _, start := range l.nodes.inOrder {
		if walked[start] {
			continue
		}

		var path []*SemanticNode
		reached := make(map[*SemanticNode]int)

		for node := start; !walked[node]; {
			if at, ok := reached[node]; ok {
				l.cycle(ring(path[at:], order), written)
				break
			}

			reached[node] = len(path)
			path = append(path, node)

			// A node written within itself is reported as that rather than as a
			// cycle of one, which says the same thing in more words.
			parent, ok := l.nodes.parent(node)
			if !ok || parent == node {
				break
			}
			node = parent
		}

		for _, node := range path {
			walked[node] = true
		}
	}
}

// cycle reports one ring of nodes which contain one another.
func (l *nodeLoader) cycle(ring []*SemanticNode, written map[*SemanticNode]containment) {
	names := make([]string, 0, len(ring)+1)
	for _, node := range ring {
		names = append(names, nodeName(node))
	}
	names = append(names, names[0])

	var related []RelatedLocation
	for _, node := range ring[1:] {
		related = append(related, RelatedLocation{
			Span:    written[node].at,
			Message: fmt.Sprintf("%s names the next node of the cycle here", nodeName(node)),
		})
	}

	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     written[ring[0]].at,
		Message: fmt.Sprintf(
			"expected a containment which ends in a node nothing contains, found a cycle: %s",
			strings.Join(names, ", then "),
		),
		Hint:    "every node is written inside one nearer the root, so following the containment has to reach a node with no parent",
		Related: related,
	})
}

// nodeName names a node for a diagnostic.
//
// A node whose id could not be read is named by what it is rather than by the
// id it does not have. Every diagnostic which uses this carries the span as
// well, which is what says which node is meant when the name does not.
func nodeName(node *SemanticNode) string {
	if node.id == "" {
		return "the node"
	}
	return string(node.id)
}
