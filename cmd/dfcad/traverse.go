// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"iter"
	"slices"
	"strconv"
	"strings"

	"github.com/z5labs/dfcad"
)

const traverseUsage = `dfcad traverse — walk the model: what contains what, what belongs to what, and
what borders what.

Usage:

	dfcad traverse [flags] <query> <id>

Queries:

	contains       what the thing holds, level by level inward
	contained-by   what holds the thing, outward towards the root
	members-of     the zones the thing is a member of, and the zones those are
	               members of where membership nests
	boundary-of    the edges the thing's outline is assembled from, each
	               classified by what physically realises it
	adjacent-to    the things which share a boundary edge with it

Flags:

	--depth <n>    how many steps of the relation to follow: a count of one or
	               more, or "all" to follow it as far as the model goes
	               (default "1")
	--kind <kind>  only results which declare this kind
	--type <name>  only results which declare this type

Every result says which relation produced it — containment, membership,
boundary or adjacency — and how many steps away it was found. Containment and
membership are never reported as each other: a wall inside a storey and grouped
into three zones is inside one thing and a member of three, and a result which
blurred the two would answer "what is in this storey" with the zones.

Adjacency is shared boundary edges and nothing else: two things are adjacent
when an edge is part of the boundary of both. An edge is one node with one
identity, so this is a fact about the model rather than a comparison of two
outlines — two boundaries drawn along the same line with two edges are not
adjacent, and they are not meant to be. A doorway and the wall it is cut into
are two shared edges between the same pair of rooms, so the neighbour is
reported once with both, and boundary-of says which of them is a wall.

Depth is bounded by default. Walking one step is the question most callers are
asking, and a traversal of a model nobody has read should not be able to return
the whole of it by accident; --depth all is how a caller asks for that on
purpose. Each thing is reported once, at the fewest steps it can be reached in,
so a cycle in the model terminates and something reachable two ways is one
result rather than two.

A filter narrows what is reported and never what is walked. Every room three
levels below a site is still reached with --kind Space, though the building and
the storey between them are not reported.

Results come back in depth order and then in id order, so two runs over one
model diff against each other and moving a node between files changes nothing.
The edges of a boundary are the exception: they come back in the order the
loops traverse them, because that order is the model's own.

An id nothing in the model holds is a usage error naming it, and naming the
nearest id there is, exactly as ` + "`dfcad get`" + ` reports one. An id which names a
vertex, an edge or a loop is a usage error too: the relations above are written
between semantic nodes, and a shape is reached through the node it bounds.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object traverse writes carries "subject", "query", the "depth" it was bounded
by, and "results": what the walk reached, each with the relation which reached
it, how far away it was, and — for the edges of a boundary — what physically
realises it.
`

// The queries traverse takes, which are the relations of the model in the
// directions they are asked in.
//
// They are spelled the way the format spells the references they follow, read
// with the subject first: `members-of <id>` answers what that thing is a member
// of, the way `(member-of <zone-id>)` is written on the member.
const (
	queryContains    = "contains"
	queryContainedBy = "contained-by"
	queryMembersOf   = "members-of"
	queryBoundaryOf  = "boundary-of"
	queryAdjacentTo  = "adjacent-to"
)

// The flags whose meaning depends on the query, named here because the errors
// which refuse them name them.
const (
	flagDepth = "depth"
	flagKind  = "kind"
	flagType  = "type"
)

// depthAll is the --depth which follows a relation as far as the model goes.
const depthAll = "all"

// query is one traversal the command can be asked for.
//
// A query is a value in [queries] rather than a case in a switch for the reason
// a subcommand is a value in [commands]: what has to be true of every one of
// them — that its results say which relation reached them, that it says whether
// a depth and a filter mean anything for it, and that its line appears in the
// usage — is then checkable by walking the list rather than by remembering.
type query struct {
	// name is what selects it on the command line.
	name string

	// deep says whether following the relation more than one step means
	// anything. It is false for a boundary, which is one step from the thing it
	// bounds by definition.
	deep bool

	// grouped says whether its results are semantic nodes, which are the things
	// a kind and a type are declared on. It is false for a boundary, whose
	// results are edges.
	grouped bool

	// walk is the traversal itself, already bounded.
	walk func(graph *dfcad.Graph, subject *dfcad.SemanticNode, depth int) []traversed
}

// queries is every query, in the order the usage lists them.
var queries = []query{
	{
		name:    queryContains,
		deep:    true,
		grouped: true,
		walk: func(graph *dfcad.Graph, subject *dfcad.SemanticNode, depth int) []traversed {
			return related(graph.DescendantsTo(subject, depth))
		},
	},
	{
		name:    queryContainedBy,
		deep:    true,
		grouped: true,
		walk: func(graph *dfcad.Graph, subject *dfcad.SemanticNode, depth int) []traversed {
			return related(graph.AncestorsTo(subject, depth))
		},
	},
	{
		name:    queryMembersOf,
		deep:    true,
		grouped: true,
		walk: func(graph *dfcad.Graph, subject *dfcad.SemanticNode, depth int) []traversed {
			return related(graph.ZonesTo(subject, depth))
		},
	},
	{
		name:    queryBoundaryOf,
		deep:    false,
		grouped: false,
		walk: func(graph *dfcad.Graph, subject *dfcad.SemanticNode, _ int) []traversed {
			var out []traversed
			for boundary := range graph.Classify(subject) {
				out = append(out, boundaryResult(boundary))
			}
			return out
		},
	},
	{
		name:    queryAdjacentTo,
		deep:    true,
		grouped: true,
		walk: func(graph *dfcad.Graph, subject *dfcad.SemanticNode, depth int) []traversed {
			var out []traversed
			for neighbour := range graph.AdjacentTo(subject, depth) {
				entry := nodeResult(neighbour.Node(), neighbour.Relation(), neighbour.Depth())
				for _, edge := range neighbour.Via() {
					entry.Via = append(entry.Via, string(edge.ID()))
				}
				out = append(out, entry)
			}
			return out
		},
	},
}

// queryNamed is the query of that name.
func queryNamed(name string) (query, bool) {
	for _, q := range queries {
		if q.name == name {
			return q, true
		}
	}
	return query{}, false
}

// queryNames is every query there is, in the order the usage lists them.
func queryNames() []string {
	out := make([]string, 0, len(queries))
	for _, q := range queries {
		out = append(out, q.name)
	}
	return out
}

// ErrMissingQuery is a traverse with no relation to follow.
var ErrMissingQuery = errors.New("expected a query and the id of the thing to walk from, found no argument")

// ErrMissingWalkFrom is a traverse with a query and nothing to ask it of.
//
// It is its own error rather than the one above because the two are different
// mistakes with different fixes, and a caller told only that an argument is
// missing has to count them to find out which.
var ErrMissingWalkFrom = errors.New("expected the id of the thing to walk from, found only the query")

// UnknownQueryError is a first argument which names none of the queries.
type UnknownQueryError struct {
	// Query is what was asked for.
	Query string

	// Known is every query there is, in the order the usage lists them.
	Known []string
}

// Error implements [error].
func (e UnknownQueryError) Error() string {
	return fmt.Sprintf("unknown query %q: want one of %s", e.Query, strings.Join(e.Known, ", "))
}

// InvalidDepthError is a --depth which is neither a count of steps nor the word
// which means all of them.
type InvalidDepthError struct {
	// Value is what was given.
	Value string
}

// Error implements [error].
func (e InvalidDepthError) Error() string {
	return fmt.Sprintf("invalid depth %q: want a count of one or more, or %q", e.Value, depthAll)
}

// FlagNotApplicableError is a flag which cannot be honoured beside the query it
// was written for.
//
// It is refused rather than ignored for the reason --deprecated is refused
// beside --claims resolved: a flag which is silently dropped answers a different
// question from the one that was asked, and reads as though it had been obeyed.
type FlagNotApplicableError struct {
	// Flag is the flag, spelled without its dashes.
	Flag string

	// Query is the query it was written beside.
	Query string

	// Reason is why the two cannot both be honoured.
	Reason string
}

// Error implements [error].
func (e FlagNotApplicableError) Error() string {
	return fmt.Sprintf("--%s says nothing under %s: %s", e.Flag, e.Query, e.Reason)
}

// The reasons a query refuses a flag, which are properties of the relation
// rather than of the invocation.
const (
	depthNotApplicable = "the boundary of a thing is one step from it — the edges its outline is assembled from — " +
		"and there is nothing beyond them to follow"

	filterNotApplicable = "the results are edges, which declare neither a kind nor a type; " +
		"what an edge is realised by is reported as its backing"
)

// NotTraversableError is an id which names something the relations are not
// written between.
//
// It is a usage error rather than an empty walk for the reason an unknown id is
// one: a shape which bounds nothing and a shape asked the wrong question are
// different answers, and a caller which cannot tell them apart goes looking for
// a relation which was never going to be there.
type NotTraversableError struct {
	// ID is what was asked about.
	ID string

	// Family is the family which holds it: vertex, edge or loop.
	Family string
}

// Error implements [error].
func (e NotTraversableError) Error() string {
	return fmt.Sprintf(
		"cannot walk from %s: it is %s %s, and the relations traverse follows are written between semantic nodes",
		e.ID, article(e.Family), e.Family,
	)
}

// article is the indefinite article a word reads with.
func article(word string) string {
	if word == "" {
		return "a"
	}
	if strings.ContainsRune("aeiou", rune(word[0])) {
		return "an"
	}
	return "a"
}

// traverseResult is the object traverse writes to stdout.
type traverseResult struct {
	envelope

	// Subject is the id the walk started from.
	Subject string `json:"subject"`

	// Query is the query it answered, which is what says which relation the
	// results carry.
	Query string `json:"query"`

	// Depth is the bound the walk was given, which is -1 where it was told to
	// follow the relation as far as the model goes. It is reported so that a
	// caller reading a stored result can tell a walk which ran out of model from
	// one which ran out of depth.
	Depth int `json:"depth"`

	// Results is what the walk reached. Empty rather than null where it reached
	// nothing.
	Results []traversed `json:"results"`
}

// traversed is one thing a traversal reached.
//
// It is one shape for every query rather than one per query, with "relation"
// saying which relation produced it and "family" which of the families holds it,
// for the reason [getEntity] is one shape for all four families: a caller
// driving five queries reads one payload and reads the fields the two of them
// name.
type traversed struct {
	// ID is the id the model holds it under.
	ID string `json:"id"`

	// Family is which family holds it: node, or edge for the boundary of one.
	Family string `json:"family"`

	// Relation is which relation reached it: containment, membership, boundary
	// or adjacency.
	Relation string `json:"relation"`

	// Depth is how many steps of that relation the walk took to reach it, which
	// is the fewest there are.
	Depth int `json:"depth"`

	// Label is its name for a person reading it. Absent when it was not written.
	Label string `json:"label,omitempty"`

	// Kind and Type are what a semantic node declares. Absent for an edge, which
	// declares neither.
	Kind string `json:"kind,omitempty"`
	Type string `json:"type,omitempty"`

	// Frame is the coordinate frame it is expressed in. Absent where it declares
	// none.
	Frame string `json:"frame,omitempty"`

	// Classification is what an edge of a boundary separates the region by:
	// physical, virtual, or unresolved where it names a backing element the
	// model does not hold. Absent for a result which is not an edge.
	Classification string `json:"classification,omitempty"`

	// Backing are the ids of the elements which physically realise an edge, in
	// the order the edge named them. Absent for a virtual edge, which names
	// none.
	Backing []string `json:"backing,omitempty"`

	// Via are the ids of the edges an adjacent thing shares with the thing it
	// was reached from, in the order that boundary traverses them. Absent for
	// every relation but adjacency.
	Via []string `json:"via,omitempty"`

	// Span is where it was written: the file, the line and the column.
	Span dfcad.Span `json:"span"`
}

// traversalDepth is the --depth flag: a count of steps, or the word which means
// as far as the model goes.
//
// It is a [flag.Value] rather than an int so that "all" is spelled out rather
// than encoded as a number a reader has to know the meaning of, and so that a
// depth of zero — a walk which takes no step and reports nothing — is refused
// where it is written rather than answered with an empty result.
type traversalDepth int

// String implements [flag.Value].
//
// The nil check is not defensive: the flag package builds a zero value of this
// type by reflection to find the default, and for a pointer receiver that zero
// value is a nil pointer.
func (d *traversalDepth) String() string {
	if d == nil {
		return "1"
	}
	if *d == dfcad.Unbounded {
		return depthAll
	}
	return strconv.Itoa(int(*d))
}

// Set implements [flag.Value].
func (d *traversalDepth) Set(value string) error {
	if value == depthAll {
		*d = dfcad.Unbounded
		return nil
	}

	steps, err := strconv.Atoi(value)
	if err != nil || steps < 1 {
		return InvalidDepthError{Value: value}
	}

	*d = traversalDepth(steps)
	return nil
}

// runTraverse is the traverse command.
func runTraverse(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	depth := traversalDepth(1)
	flags.Var(&depth, flagDepth, "")
	kind := flags.String(flagKind, "", "")
	declaredType := flags.String(flagType, "", "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	switch {
	case len(arguments) == 0:
		return usageError(cmd, ErrMissingQuery, stderr, true)
	case len(arguments) == 1:
		return usageError(cmd, ErrMissingWalkFrom, stderr, true)
	case len(arguments) > 2:
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments[2:]}, stderr, true)
	}

	asked, ok := queryNamed(arguments[0])
	if !ok {
		return usageError(cmd, UnknownQueryError{Query: arguments[0], Known: queryNames()}, stderr, false)
	}

	if err := checkFlags(asked, written(flags)); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	id, err := dfcad.ParseID(arguments[1])
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	graph := loadModel(cmd, globals, stderr)

	if err := checkFilters(graph.Registry(), *declaredType, *kind, ""); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	subject, err := traversable(graph, id)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	result := traverseResult{
		envelope: newEnvelope(cmd.name),
		Subject:  string(id),
		Query:    asked.name,
		Depth:    int(depth),
		Results:  narrow(asked.walk(graph, subject, int(depth)), *kind, *declaredType),
	}

	// Depth first and then id, so that two runs over one model diff against each
	// other and a node moved between files does not move the answer. The edges
	// of a boundary are left where the loops put them: that order is the ring
	// itself, which is data rather than presentation, and sorting it would throw
	// away which edge is next to which.
	if asked.name != queryBoundaryOf {
		slices.SortStableFunc(result.Results, func(a, b traversed) int {
			return cmp.Or(cmp.Compare(a.Depth, b.Depth), strings.Compare(a.ID, b.ID))
		})
	}

	reportTraversal(result, globals, stderr)

	if err := emit(stdout, result); err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// written is the flags an invocation actually gave, as distinct from the ones
// which have a default.
//
// It is what lets a flag be refused where it says nothing without refusing the
// default it would have had anyway: `traverse boundary-of <id>` is an ordinary
// invocation, and `traverse boundary-of --depth 2 <id>` is a question the
// relation has no answer to.
func written(flags *flag.FlagSet) map[string]bool {
	given := make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { given[f.Name] = true })
	return given
}

// checkFlags reports a flag which cannot be honoured beside the query it was
// written for.
func checkFlags(asked query, given map[string]bool) error {
	if given[flagDepth] && !asked.deep {
		return FlagNotApplicableError{Flag: flagDepth, Query: asked.name, Reason: depthNotApplicable}
	}

	for _, filter := range []string{flagKind, flagType} {
		if given[filter] && !asked.grouped {
			return FlagNotApplicableError{Flag: filter, Query: asked.name, Reason: filterNotApplicable}
		}
	}

	return nil
}

// traversable is the semantic node id names, reporting an id nothing holds and
// one which names a shape rather than a thing.
func traversable(graph *dfcad.Graph, id dfcad.ID) (*dfcad.SemanticNode, error) {
	entity, ok := graph.Entity(id)
	if !ok {
		nearest, _ := graph.Nearest(id)
		return nil, UnknownIDError{ID: string(id), Nearest: string(nearest)}
	}

	node, ok := entity.(*dfcad.SemanticNode)
	if !ok {
		return nil, NotTraversableError{ID: string(id), Family: familyOf(entity)}
	}

	return node, nil
}

// familyOf is which family holds one entity, spelled the way the form which
// writes it is tagged.
func familyOf(entity dfcad.Entity) string {
	switch entity.(type) {
	case *dfcad.SemanticNode:
		return familyNode
	case *dfcad.Vertex:
		return familyVertex
	case *dfcad.Edge:
		return familyEdge
	case *dfcad.Loop:
		return familyLoop
	}
	return ""
}

// narrow drops the results which do not satisfy the filters.
//
// It narrows the results and never the walk. A room three levels below a site is
// reached whether or not the building and the storey between them satisfy the
// filter, because the filter says what to report rather than what to walk
// through — a walk which pruned on it would answer "no rooms" for a model whose
// every room is inside something else.
func narrow(results []traversed, kind, declaredType string) []traversed {
	// Made rather than declared so that a walk which reached nothing writes an
	// empty list rather than a null, and a caller indexing it needs no special
	// case for the thing at the edge of the model.
	out := make([]traversed, 0, len(results))

	for _, result := range results {
		if kind != "" && result.Kind != kind {
			continue
		}
		if declaredType != "" && result.Type != declaredType {
			continue
		}
		out = append(out, result)
	}

	return out
}

// related is a walk of one of the node relations, as the answer reports it.
func related(results iter.Seq[dfcad.Related]) []traversed {
	var out []traversed
	for result := range results {
		out = append(out, nodeResult(result.Node(), result.Relation(), result.Depth()))
	}
	return out
}

// nodeResult is one semantic node a traversal reached.
func nodeResult(node *dfcad.SemanticNode, relation dfcad.Relation, depth int) traversed {
	entry := traversed{
		ID:       string(node.ID()),
		Family:   familyNode,
		Relation: string(relation),
		Depth:    depth,
		Label:    node.Label(),
		Kind:     string(node.Kind()),
		Type:     node.Type(),
		Span:     node.Span(),
	}

	if frame, ok := node.Frame(); ok {
		entry.Frame = string(frame)
	}

	return entry
}

// boundaryResult is one edge of a boundary, with what physically realises it.
//
// The classification is reported whatever it is, including the answer an edge
// gets when it names a backing element the model does not hold. That is a load
// error, and reporting it as virtual would be the silent reclassification the
// error exists to prevent.
func boundaryResult(boundary dfcad.BoundaryEdge) traversed {
	edge := boundary.Edge()

	entry := traversed{
		ID:             string(edge.ID()),
		Family:         familyEdge,
		Relation:       string(boundary.Relation()),
		Depth:          1,
		Label:          edge.Label(),
		Frame:          string(edge.Frame()),
		Classification: string(boundary.Classification()),
		Span:           edge.Span(),
	}

	for _, element := range boundary.Backing() {
		entry.Backing = append(entry.Backing, string(element.ID()))
	}

	return entry
}

// reportTraversal renders a traverse result for a person, on stderr.
//
// Nothing here reaches stdout, in any format and at any verbosity: stdout is the
// same bytes whether or not anybody asked to read the run.
func reportTraversal(result traverseResult, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	deepest := 0
	for _, entry := range result.Results {
		deepest = max(deepest, entry.Depth)

		// The results themselves are already the answer, on stdout, so the
		// reading of them is progress rather than result.
		if globals.Verbosity >= verbosityProgress {
			fmt.Fprintf(stderr, "%s: %s at %d\n", entry.ID, entry.Relation, entry.Depth)
		}
	}

	fmt.Fprintf(stderr, "%s %s: %s, deepest at %d\n",
		result.Query, result.Subject, plural(len(result.Results), "result"), deepest)
}
