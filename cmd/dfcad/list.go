// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/z5labs/dfcad"
)

const listTypesUsage = `dfcad list-types — list the node types the model declares.

Usage:

	dfcad list-types [flags]

Every type the registry declares, with the kinds and the geometry forms an
instance of it may take, and how many instances the model holds. It takes no
arguments: the answer is the whole registry, which is what makes it the first
call to make against a model nothing has read before.

Flags:

	--describe       include the one line the registry gives each type
	--classification include how schemes outside this model name each type

The descriptions are left out unless they are asked for. They are prose about
the vocabulary rather than about this model, they grow with the registry rather
than with the model, and this is the call every cold start begins with — so
whoever is deciding which type to ask about next pays for them on every run and
reads them on almost none. Ask for them and they come back.

The classifications are left out for the same reason and not for the same
readers: the one caller which needs them is a caller mapping this model into a
foreign schema, and it asks once. A system and a code are two opaque strings the
registry wrote, reported exactly as written — no scheme is known here, and
nothing about a code is interpreted.

Types come back in name order, so two runs over one model produce the same
list and a diff between them means something.

A model which declares no type at all lists nothing and succeeds. That is an
empty registry rather than a failure, and it is what a tree nobody has written
a registry file into looks like.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object list-types writes carries "types": one entry per declared type, in
name order, each with its name, the kinds and geometry forms it permits,
whether an instance may omit its geometry, and its instance count. Under
--classification each entry also carries "classifications", in the order the
registry wrote them, each a system and a code.
`

const listInstancesUsage = `dfcad list-instances — list the instances of a type.

Usage:

	dfcad list-instances [flags] [type]

The id and the label of every instance of the named type. With no type, every
semantic node in the model, which is how a small model is read whole and a
large one is narrowed with the filters below.

Flags:

	--kind <kind>    only instances which declare this kind
	--frame <id>     only instances which declare this coordinate frame
	--retired        include the instances which stopped existing

Filters combine: an instance is listed when it satisfies every filter given.

A retired node is left out unless it is asked for. It is still a node the model
holds — its id is never issued again, and a reference to it still resolves — but
a listing is a question about what is there, and answering it with things which
stopped existing makes every caller filter them out again. Asked for, they come
back marked, so a caller reading a mixed listing can tell which is which.

Instances come back in id order, so the list does not change when a node moves
between files, and two runs over one model diff against each other.

A type the registry does not declare is a usage error naming it, rather than an
empty list: a type nobody declared and a type nothing instantiates are
different answers, and a caller which cannot tell them apart is one which
retries a misspelling forever. The same holds for a kind which is not one of
the seven and for a frame the registry does not declare.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object list-instances writes carries "instances": one entry per instance,
in id order, each with its id, its label, the type and kind it declares and the
frame it is expressed in.
`

const listGeometryUsage = `dfcad list-geometry — list the geometric nodes which carry a claim under a predicate.

Usage:

	dfcad list-geometry [flags]

Every vertex, edge and loop the model states something about under the named
predicate, with the family it belongs to, the frame it is expressed in and where
it was written. It is the geometric sibling of "dfcad list-instances", which
reports the type and the kind a vertex, an edge and a loop do not have.

This is how "which dimensions does this level carry" is asked. A measured span
between two corners is an ordinary edge carrying a claim — it need belong to no
loop and bound nothing — and without this the only way to reach one is to
already know its id, which means keeping a second list of them by hand.

Flags:

	--predicate <name>   the predicate the node carries; required
	--family <family>    only nodes of this family: vertex, edge or loop

--predicate has no default and never will, for the reason "dfcad buildable" has
none: which predicate carries a position, a setback or a span is something the
project wrote down, and a name compiled in here would be the engine deciding a
project's vocabulary on its behalf.

A node is listed when a live claim is written on it under that predicate. A
deprecated claim is a statement somebody withdrew, and a listing of what the
model records is not a listing of what it used to; "dfcad claims" is the audit
view which reports those, on one node at a time.

An edge names its two vertices in the order they were authored. The order is the
data — an edge is directed, and the region on the other side of it traverses it
the other way — so it is reported as written and is never sorted.

Nodes come back in id order, so the list does not change when a node moves
between files, and two runs over one model diff against each other.

A predicate no geometric node carries is an empty list and exit zero. A model
which records no spans is an ordinary model, and answering it with a failure
would make a caller parse a message to tell nothing-there from something-wrong.

A predicate the registry does not declare is a usage error naming it, rather
than an empty list: a predicate nobody declared and a predicate nothing is
written under are different answers, and a caller which cannot tell them apart
retries a misspelling forever. The same holds for a family which is none of the
three.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object list-geometry writes carries "predicate", the predicate it was asked
about, and "nodes": one entry per geometric node carrying a live claim under it,
in id order, each with its id, its family, its frame, the span it was written
at, and — for an edge — the two vertices it runs between, in the authored order.
`

// The flags list-geometry takes beyond the global ones, named here because the
// errors which refuse them name them.
const (
	flagPredicate = "predicate"
	flagFamily    = "family"
)

// families are the three families a geometric node can belong to, in the order
// the usage lists them.
//
// They are the tags the forms are written with, which is what [familyVertex]
// and its siblings hold, so a filter names a family the way the file does and
// the answer reports it the way "dfcad get" does.
var families = []string{familyVertex, familyEdge, familyLoop}

// familyPlurals is how a count of each family is spelled for a person.
//
// It is written down rather than derived because a vertex is the one noun in
// this command line interface whose plural is not itself with an s on the end,
// and "0 vertexs" in a summary is the sort of thing a reader stops at.
var familyPlurals = map[string]string{
	familyVertex: "vertices",
	familyEdge:   "edges",
	familyLoop:   "loops",
}

// UnknownFamilyError is a --family which names none of the three.
//
// The known set is listed in the message rather than pointed at, unlike a type,
// because there are three of them and there will never be more: the families
// are the closed set the format is written in, so printing them costs a line
// and saves a lookup.
type UnknownFamilyError struct {
	// Family is what was asked for.
	Family string

	// Known is every family there is, in the order the usage lists them.
	Known []string
}

// Error implements [error].
func (e UnknownFamilyError) Error() string {
	return fmt.Sprintf("unknown family %s: want one of %s", e.Family, strings.Join(e.Known, ", "))
}

// UnknownTypeError is a type argument no registry file declares.
//
// It carries the declared set rather than only the name, so that a caller can
// see what there was without re-running anything, while the message points at
// list-types instead of listing them: a registry large enough to be worth
// discovering is one too large to print in an error.
type UnknownTypeError struct {
	// Type is what was asked for.
	Type string

	// Declared is every type the registry declares, in name order.
	Declared []string
}

// Error implements [error].
func (e UnknownTypeError) Error() string {
	if len(e.Declared) == 0 {
		return fmt.Sprintf(
			"unknown type %s: this model declares no type at all; run `dfcad list-types` to see that for yourself",
			e.Type,
		)
	}
	return fmt.Sprintf(
		"unknown type %s: run `dfcad list-types` for the %s this model declares",
		e.Type, plural(len(e.Declared), "type"),
	)
}

// UnknownKindError is a --kind which names none of the kinds.
type UnknownKindError struct {
	// Kind is what was asked for.
	Kind string

	// Known is the closed set, in specification order.
	Known []dfcad.Kind
}

// Error implements [error].
func (e UnknownKindError) Error() string {
	return fmt.Sprintf("unknown kind %s: want one of %s", e.Kind, strings.Join(spellings(e.Known), ", "))
}

// UnknownFrameError is a --frame no registry file declares.
type UnknownFrameError struct {
	// Frame is what was asked for.
	Frame string

	// Declared is every frame the registry declares, in id order.
	Declared []string
}

// Error implements [error].
func (e UnknownFrameError) Error() string {
	if len(e.Declared) == 0 {
		return fmt.Sprintf("unknown frame %s: this model declares no frame at all", e.Frame)
	}
	return fmt.Sprintf("unknown frame %s: want one of %s", e.Frame, strings.Join(e.Declared, ", "))
}

// UnexpectedArgumentsError is more arguments than the command takes.
type UnexpectedArgumentsError struct {
	// Extra is what was given beyond the last argument the command takes.
	Extra []string
}

// Error implements [error].
func (e UnexpectedArgumentsError) Error() string {
	return fmt.Sprintf("unexpected %s: %s", plural(len(e.Extra), "argument"), strings.Join(e.Extra, " "))
}

// listTypesResult is the object list-types writes to stdout.
type listTypesResult struct {
	envelope

	// Types is one entry per declared type, in name order.
	Types []listedType `json:"types"`
}

// listedType is one declared type as the discovery path reports it.
//
// It is the type's axes and its count, and not the forms it was written with:
// what a caller is deciding from this is which type to ask about next, and the
// declaration itself is in the registry file for anyone who needs it.
type listedType struct {
	// Name is the type name, which is what list-instances takes.
	Name string `json:"name"`

	// Kinds are the kinds an instance may declare, in specification order.
	Kinds []string `json:"kinds"`

	// Geometries are the geometry forms an instance may declare, in
	// specification order.
	Geometries []string `json:"geometries"`

	// Absent reports whether an instance may omit its geometry entirely. It is
	// separate from Geometries because absence is not a geometry form: a node
	// with no geometry omits the child rather than naming one.
	//
	// It is written only where it holds, the way Retired is on a listed
	// instance: most types require a geometry, and a false on every one of them
	// is a word per type saying what the type before it also said.
	Absent bool `json:"absent,omitempty"`

	// Description is the one line the registry gives the type. Written under
	// --describe and absent otherwise, and absent under it too when the
	// registry wrote none.
	Description string `json:"description,omitempty"`

	// Classifications are how schemes outside this model name the type, in the
	// order the registry wrote them. Written under --classification and absent
	// otherwise, and absent under it too when the registry wrote none, which is
	// the ordinary case.
	Classifications []listedClassification `json:"classifications,omitempty"`

	// Instances is how many semantic nodes declare this type.
	Instances int `json:"instances"`
}

// listedClassification is one external classification of a type.
//
// Both halves are reported exactly as the registry wrote them. Neither is
// normalised, folded or checked against anything: the engine knows no scheme,
// and a caller mapping this model into one is the only reader which can say what
// either string means.
type listedClassification struct {
	// System names the scheme.
	System string `json:"system"`

	// Code names this type within that scheme.
	Code string `json:"code"`
}

// listInstancesResult is the object list-instances writes to stdout.
type listInstancesResult struct {
	envelope

	// Instances is one entry per instance which satisfied every filter, in id
	// order.
	Instances []listedInstance `json:"instances"`
}

// listedInstance is one semantic node as the discovery path reports it.
//
// The type and the kind are reported whether or not they were filtered on, so
// that a caller reads one shape of entry whichever way it narrowed the model,
// and so that an unfiltered listing of a whole model is readable on its own.
type listedInstance struct {
	// ID is the id the model holds it under.
	ID string `json:"id"`

	// Label is its name for a person reading it. Empty when it was not
	// written.
	Label string `json:"label,omitempty"`

	// Type is the type it declares, which need not be one the registry
	// declares: a node naming an undeclared type is a diagnostic and is still
	// a node of the type it named.
	Type string `json:"type"`

	// Kind is the kind it declares.
	Kind string `json:"kind"`

	// Frame is the coordinate frame it is expressed in. Empty when it declares
	// none.
	Frame string `json:"frame,omitempty"`

	// Retired reports whether the thing it names stopped existing. It is absent
	// on the ordinary node rather than written false, because a listing which
	// was not asked for the retired ones holds nothing else.
	Retired bool `json:"retired,omitempty"`
}

// listGeometryResult is the object list-geometry writes to stdout.
type listGeometryResult struct {
	envelope

	// Predicate is the predicate the nodes below carry, which is the one asked
	// for.
	//
	// It travels with the answer because the answer means nothing without it: a
	// caller collecting the listings of a level under three predicates has to be
	// able to say which object answers which question, and an empty list is
	// exactly the object it most needs that of.
	Predicate string `json:"predicate"`

	// Nodes is one entry per geometric node carrying a live claim under it, in
	// id order. Empty rather than null when nothing does.
	Nodes []listedGeometry `json:"nodes"`
}

// listedGeometry is one geometric node as the discovery path reports it.
//
// It is one shape for all three families rather than one per family, with
// "family" saying which came back, for the reason [getEntity] is one shape for
// all four: a caller reading a listing it did not filter by family would
// otherwise have to probe each entry's top-level key before it could read it.
//
// Every reference is an id and never the thing it names, which is what keeps
// the answer the size of the question: an edge names its two vertices, and
// where each of those is written is one more entry of this listing rather than
// a nesting inside this one.
type listedGeometry struct {
	// ID is the id the model holds it under, which is what every other command
	// takes.
	ID string `json:"id"`

	// Family is which family holds it: vertex, edge or loop. It is reported
	// whether or not it was filtered on, so that a listing read whole is
	// readable on its own.
	Family string `json:"family"`

	// Label is its name for a person reading it. Absent when it was not
	// written, which is the ordinary case for geometry.
	//
	// It is here for the reason a listed instance carries one — a listing is
	// read by a person as often as by a caller — and it costs nothing on the
	// model which wrote none.
	Label string `json:"label,omitempty"`

	// Frame is the coordinate frame it is expressed in. Absent only when it
	// declares none, which is a diagnostic rather than an ordinary node: a
	// geometric node is always in exactly one frame.
	Frame string `json:"frame,omitempty"`

	// Start and End are the ids of the vertices an edge runs between, in the
	// order they were authored. Absent for a vertex and for a loop.
	//
	// The order is the data and is never sorted: an edge is directed, and the
	// region on the other side of it traverses it the other way.
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`

	// Span is where it was written: the file, the line and the column, which is
	// what sends a reader to the definition rather than to a search.
	//
	// It is in the default answer rather than behind a flag because the whole
	// point of this listing is to reach nodes nothing else names: an id which
	// came back from a query nobody could have guessed is one whose next
	// question is where it is written.
	Span dfcad.Span `json:"span"`
}

// runListTypes is the list-types command.
func runListTypes(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	describing := flags.Bool("describe", false, "")
	classifying := flags.Bool("classification", false, "")

	extra, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(extra) > 0 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: extra}, stderr, true)
	}

	graph := loadModel(cmd, globals, stderr)

	result := listTypesResult{
		envelope: newEnvelope(cmd.name),

		// Made rather than declared so that a model declaring nothing writes an
		// empty list rather than a null, and a caller indexing it needs no
		// special case for the empty registry.
		Types: make([]listedType, 0),
	}
	for declared := range graph.Registry().Types() {
		entry := listedType{
			Name:       declared.Name,
			Kinds:      permittedKinds(declared),
			Geometries: permittedGeometries(declared),
			Absent:     declared.Absent,
			Instances:  graph.Summary().OfType(declared.Name),
		}
		if *describing {
			entry.Description = declared.Description
		}
		if *classifying {
			entry.Classifications = classificationsOf(declared)
		}

		result.Types = append(result.Types, entry)
	}

	reportTypes(result.Types, globals, stderr)

	if err := emit(stdout, result); err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// runListInstances is the list-instances command.
func runListInstances(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	kind := flags.String("kind", "", "")
	frame := flags.String("frame", "", "")
	retired := flags.Bool("retired", false, "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(arguments) > 1 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments[1:]}, stderr, true)
	}

	var declaredType string
	if len(arguments) == 1 {
		declaredType = arguments[0]
	}

	// The model is loaded before the arguments are checked because the registry
	// is what says whether a type or a frame exists, and the registry is the
	// model. Its diagnostics reach stderr either way, so a name which is
	// unknown because a registry file did not parse is reported beside the
	// reason it did not.
	graph := loadModel(cmd, globals, stderr)
	registry := graph.Registry()

	if err := checkFilters(registry, declaredType, *kind, *frame); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	result := listInstancesResult{
		envelope:  newEnvelope(cmd.name),
		Instances: make([]listedInstance, 0),
	}

	nodes := graph.Nodes().All()
	if declaredType != "" {
		nodes = graph.OfType(declaredType)
	}
	for node := range nodes {
		if !matches(node, *kind, *frame, *retired) {
			continue
		}

		entry := listedInstance{
			ID:      string(node.ID()),
			Label:   node.Label(),
			Type:    node.Type(),
			Kind:    string(node.Kind()),
			Retired: node.Retired(),
		}
		if id, ok := node.Frame(); ok {
			entry.Frame = string(id)
		}

		result.Instances = append(result.Instances, entry)
	}

	// Id order rather than walk order, because an id is what a caller asks
	// about next and is the one thing about a node which does not change. A
	// listing in walk order moves every line below a node which was cut from
	// one file and pasted into another, while the model it describes is the
	// same model.
	//
	// Stable, so that the walk order breaks the tie between the nodes whose id
	// could not be read at all — they share the empty id, and the load already
	// reported each of them.
	slices.SortStableFunc(result.Instances, func(a, b listedInstance) int {
		return strings.Compare(a.ID, b.ID)
	})

	reportInstances(result.Instances, globals, stderr)

	if err := emit(stdout, result); err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// runListGeometry is the list-geometry command.
func runListGeometry(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	predicate := flags.String(flagPredicate, "", "")
	family := flags.String(flagFamily, "", "")

	extra, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(extra) > 0 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: extra}, stderr, true)
	}

	// The vocabulary is checked before the model is read, which is the one
	// order-of-checks difference from list-instances. A run which did not say
	// which predicate to ask about has not asked a question yet, and no registry
	// can supply the word it left out — so reporting a whole model's diagnostics
	// first would bury the one thing wrong with the invocation.
	if err := vocabularyOf(given{flag: flagPredicate, value: *predicate}); err != nil {
		return usageError(cmd, err, stderr, true)
	}

	// A family is checked before the load too, for the same reason: the three
	// are a closed set compiled in, so nothing in the tree makes `--family
	// vertexes` any more of a family.
	if err := checkFamily(*family); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	// The predicate, in contrast, is registry data, and the registry is the
	// model. Its diagnostics reach stderr either way, so a predicate which is
	// unknown because a registry file did not parse is reported beside the
	// reason it did not.
	graph := loadModel(cmd, globals, stderr)

	if err := checkPredicate(graph.Registry(), *predicate); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	result := listGeometryResult{
		envelope:  newEnvelope(cmd.name),
		Predicate: *predicate,

		// Made rather than declared so that a predicate nothing carries writes an
		// empty list rather than a null, and a caller indexing it needs no
		// special case for the model which records no spans.
		Nodes: make([]listedGeometry, 0),
	}

	topology := graph.Topology()

	if wanted(*family, familyVertex) {
		for vertex := range topology.Vertices() {
			if !carries(graph, vertex.ID(), *predicate) {
				continue
			}

			result.Nodes = append(result.Nodes, listedGeometry{
				ID:     string(vertex.ID()),
				Family: familyVertex,
				Label:  vertex.Label(),
				Frame:  string(vertex.Frame()),
				Span:   vertex.Span(),
			})
		}
	}

	if wanted(*family, familyEdge) {
		for edge := range topology.Edges() {
			if !carries(graph, edge.ID(), *predicate) {
				continue
			}

			start, end := edge.Vertices()

			result.Nodes = append(result.Nodes, listedGeometry{
				ID:     string(edge.ID()),
				Family: familyEdge,
				Label:  edge.Label(),
				Frame:  string(edge.Frame()),
				Start:  string(start),
				End:    string(end),
				Span:   edge.Span(),
			})
		}
	}

	if wanted(*family, familyLoop) {
		for loop := range topology.Loops() {
			if !carries(graph, loop.ID(), *predicate) {
				continue
			}

			result.Nodes = append(result.Nodes, listedGeometry{
				ID:     string(loop.ID()),
				Family: familyLoop,
				Label:  loop.Label(),
				Frame:  string(loop.Frame()),
				Span:   loop.Span(),
			})
		}
	}

	// Id order rather than family order or walk order, for the reason a listing
	// of instances is in id order: an id is what a caller asks about next and is
	// the one thing about a node which does not change. Grouping by family would
	// reorder the whole answer the day an edge was given a claim it did not have
	// before.
	//
	// Stable, so that the walk order breaks the tie between the nodes whose id
	// could not be read at all — they share the empty id, and the load already
	// reported each of them.
	slices.SortStableFunc(result.Nodes, func(a, b listedGeometry) int {
		return strings.Compare(a.ID, b.ID)
	})

	reportGeometry(result, globals, stderr)

	if err := emit(stdout, result); err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// wanted reports whether a family is asked for, where the empty filter asks for
// all three.
func wanted(filter, family string) bool {
	return filter == "" || filter == family
}

// carries reports whether a live claim is written on the subject under the
// predicate.
//
// A deprecated claim does not count. It is retracted rather than out-ranked, and
// resolution never considers one, so a node whose only statement under the
// predicate was withdrawn records nothing under it — listing it would answer
// "which spans does this level record" with a span nobody stands behind.
func carries(graph *dfcad.Graph, subject dfcad.ID, predicate string) bool {
	for claim := range graph.Claims().Under(subject, predicate) {
		if claim.Rank() != dfcad.RankDeprecated {
			return true
		}
	}
	return false
}

// checkFamily reports a --family which is none of the three, and accepts the
// empty one, which is no filter at all.
func checkFamily(family string) error {
	if family == "" || slices.Contains(families, family) {
		return nil
	}
	return UnknownFamilyError{Family: family, Known: families}
}

// checkFilters reports the first filter which names something the model has no
// such thing of.
//
// An unknown name is a usage error rather than an empty list. A type nobody
// declared and a type nothing instantiates are different answers, and a caller
// which cannot tell them apart retries a misspelling forever; the same is true
// of a kind and of a frame.
func checkFilters(registry *dfcad.Registry, declaredType, kind, frame string) error {
	if declaredType != "" && !registry.Declares(dfcad.SortType, declaredType) {
		return UnknownTypeError{Type: declaredType, Declared: registry.Names(dfcad.SortType)}
	}

	if kind != "" && !slices.Contains(dfcad.Kinds(), dfcad.Kind(kind)) {
		return UnknownKindError{Kind: kind, Known: dfcad.Kinds()}
	}

	if frame != "" && !registry.Declares(dfcad.SortFrame, frame) {
		return UnknownFrameError{Frame: frame, Declared: registry.Names(dfcad.SortFrame)}
	}

	return nil
}

// matches reports whether a node satisfies the filters which are not the type.
//
// Retirement is one of them rather than a test beside them, so that "an instance
// is listed when it satisfies every filter" has one place which decides it. It
// is the one filter which is on by default: a listing is a question about what
// is there, and a node which stopped existing answers it only when it was asked
// for.
func matches(node *dfcad.SemanticNode, kind, frame string, retired bool) bool {
	if node.Retired() && !retired {
		return false
	}

	if kind != "" && node.Kind() != dfcad.Kind(kind) {
		return false
	}

	if frame != "" {
		id, ok := node.Frame()
		if !ok || string(id) != frame {
			return false
		}
	}

	return true
}

// permittedKinds is the kinds an instance of the type may declare, in
// specification order.
//
// The order is the closed set's rather than the order the declaration happened
// to be written in, so that two registries permitting the same kinds list them
// the same way and a diff between two runs is about what changed.
func permittedKinds(declared dfcad.Type) []string {
	out := make([]string, 0, len(declared.Kinds))
	for _, kind := range dfcad.Kinds() {
		if declared.PermitsKind(kind) {
			out = append(out, string(kind))
		}
	}
	return out
}

// permittedGeometries is the geometry forms an instance of the type may
// declare, in specification order. Absence is not among them: it is reported
// separately, because a node with no geometry omits the child rather than
// naming a form.
func permittedGeometries(declared dfcad.Type) []string {
	out := make([]string, 0, len(declared.Geometries))
	for _, geometry := range dfcad.Geometries() {
		if declared.PermitsGeometry(geometry) {
			out = append(out, string(geometry))
		}
	}
	return out
}

// classificationsOf is how schemes outside this model name the type, in the
// order the registry wrote them.
//
// Written order rather than sorted, unlike every other list this command
// reports: the registry file's own canonical form already sorts them, so what
// comes back here is a stable order somebody can diff, and re-sorting it on a
// second key here would only be a second opinion about the same list.
func classificationsOf(declared dfcad.Type) []listedClassification {
	if len(declared.Classifications) == 0 {
		return nil
	}

	out := make([]listedClassification, 0, len(declared.Classifications))
	for _, classification := range declared.Classifications {
		out = append(out, listedClassification{
			System: classification.System,
			Code:   classification.Code,
		})
	}
	return out
}

// loadModel reads the whole model beneath the root and renders whatever is
// wrong with it to stderr.
//
// Diagnostics go to stderr on every run and in every format, because they are
// for whoever wrote the file. The graph which comes back is usable whatever
// they say, so a listing of a model somebody is part way through writing is
// still a listing of what is there.
//
// They do not change the exit code. A listing says what a model holds, and a
// node whose containment does not resolve is still a node the model holds; the
// question of whether the model is sound is what `dfcad check` answers, and
// answering it twice, in two commands, with two definitions of sound, is how
// the two come to disagree. It also keeps discovery usable on a model somebody
// is halfway through writing, which is the model discovery is most needed on:
// a call that refuses to describe a tree until the tree is finished is a call
// nobody reaches for.
func loadModel(cmd command, globals *globals, stderr io.Writer) *dfcad.Graph {
	graph, _ := loadGate(cmd, globals, stderr)
	return graph
}

// loadGate is [loadModel] with what the diagnostics said about the model kept,
// which is what a gate needs and a listing does not.
//
// It reports whether the load refused the model — whether any diagnostic is an
// error rather than a warning — and is the one place which decides that, so
// that a read which ignores it and a gate which acts on it are reading the same
// answer.
func loadGate(cmd command, globals *globals, stderr io.Writer) (*dfcad.Graph, bool) {
	reportLoading(cmd, globals, stderr)

	graph, found := dfcad.LoadGraph(globals.Root)

	return graph, render(found, stderr)
}

// usageError reports an invocation which named something that does not exist.
//
// The usage message follows a wrong shape of invocation — an argument too many
// — because the shape is what it documents. It does not follow a name the model
// does not declare: the answer there is the name and where to look for the real
// ones, and a page of flags between the two buries it.
func usageError(cmd command, err error, stderr io.Writer, withUsage bool) int {
	fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
	if withUsage {
		fmt.Fprint(stderr, "\n")
		fmt.Fprint(stderr, cmd.usage)
	}
	return exitUsage
}

// reportTypes renders a list-types result for a person, on stderr.
//
// Nothing here reaches stdout, in any format and at any verbosity: stdout is
// the same bytes whether or not anybody asked to read the run.
func reportTypes(types []listedType, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	instances := 0
	for _, declared := range types {
		instances += declared.Instances

		// The detail behind the summary is progress rather than result — the
		// result is on stdout — so it is behind the verbosity flag.
		if globals.Verbosity >= verbosityProgress {
			fmt.Fprintf(stderr, "%s: kind %s, geometry %s, %s%s\n",
				declared.Name,
				join(declared.Kinds),
				join(permitted(declared)),
				plural(declared.Instances, "instance"),
				classifiedAs(declared),
			)
		}
	}

	fmt.Fprintf(stderr, "%s, %s\n", plural(len(types), "type"), plural(instances, "instance"))
}

// permitted is what a type allows in the geometry position, spelled for a
// person. Absence is a phrase rather than the word `absent`, because `absent`
// is written in a type declaration and nowhere else.
func permitted(declared listedType) []string {
	out := slices.Clone(declared.Geometries)
	if declared.Absent {
		out = append(out, "none at all")
	}
	return out
}

// classifiedAs is how a type's external classifications are spelled at the end
// of a progress line, and is empty where it has none — which is every type on a
// run that did not ask for them, and most types on one that did.
func classifiedAs(declared listedType) string {
	if len(declared.Classifications) == 0 {
		return ""
	}

	spelled := make([]string, 0, len(declared.Classifications))
	for _, classification := range declared.Classifications {
		spelled = append(spelled, classification.System+" "+classification.Code)
	}

	return ", " + join(spelled)
}

// reportInstances renders a list-instances result for a person, on stderr.
func reportInstances(instances []listedInstance, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	types := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		types[instance.Type] = struct{}{}

		if globals.Verbosity >= verbosityProgress {
			label := instance.Label
			if label == "" {
				label = "(no label)"
			}
			fmt.Fprintf(stderr, "%s: %s, %s %s\n", instance.ID, label, instance.Kind, instance.Type)
		}
	}

	fmt.Fprintf(stderr, "%s of %s\n", plural(len(instances), "instance"), plural(len(types), "type"))
}

// reportGeometry renders a list-geometry result for a person, on stderr.
//
// Nothing here reaches stdout, in any format and at any verbosity: stdout is
// the same bytes whether or not anybody asked to read the run.
func reportGeometry(result listGeometryResult, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	counted := make(map[string]int, len(families))

	for _, node := range result.Nodes {
		counted[node.Family]++

		// The nodes themselves are already the result, on stdout, so the reading
		// of them is progress rather than result.
		if globals.Verbosity >= verbosityProgress {
			fmt.Fprintf(stderr, "%s: %s%s in %s\n", node.ID, node.Family, between(node), node.Frame)
		}
	}

	spread := make([]string, 0, len(families))
	for _, family := range families {
		spread = append(spread, pluralOf(counted[family], family, familyPlurals[family]))
	}

	fmt.Fprintf(stderr, "%s under %s: %s\n",
		plural(len(result.Nodes), "geometric node"), result.Predicate, join(spread))
}

// between is the ends of an edge for a person, and nothing at all for a family
// which does not run between anything.
func between(node listedGeometry) string {
	if node.Family != familyEdge {
		return ""
	}
	return fmt.Sprintf(" %s -> %s", node.Start, node.End)
}

// join writes a set for a person, which is a comma-separated list and nothing
// for the empty set.
func join(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

// spellings is a set of string-like values as plain strings, for a message
// which lists them.
func spellings[T ~string](set []T) []string {
	out := make([]string, 0, len(set))
	for _, item := range set {
		out = append(out, string(item))
	}
	return out
}
