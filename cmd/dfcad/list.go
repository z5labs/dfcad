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
instance of it may take, the one line the registry gives it, and how many
instances the model holds. It takes no arguments: the answer is the whole
registry, which is what makes it the first call to make against a model
nothing has read before.

Types come back in name order, so two runs over one model produce the same
list and a diff between them means something.

A model which declares no type at all lists nothing and succeeds. That is an
empty registry rather than a failure, and it is what a tree nobody has written
a registry file into looks like.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object list-types writes carries "types": one entry per declared type, in
name order, each with its name, the kinds and geometry forms it permits,
whether an instance may omit its geometry, its description and its instance
count.
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
	Absent bool `json:"absent"`

	// Description is the one line the registry gives the type. Empty when it
	// was not written.
	Description string `json:"description,omitempty"`

	// Instances is how many semantic nodes declare this type.
	Instances int `json:"instances"`
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

// runListTypes is the list-types command.
func runListTypes(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

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
		result.Types = append(result.Types, listedType{
			Name:        declared.Name,
			Kinds:       permittedKinds(declared),
			Geometries:  permittedGeometries(declared),
			Absent:      declared.Absent,
			Description: declared.Description,
			Instances:   graph.Summary().OfType(declared.Name),
		})
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
			fmt.Fprintf(stderr, "%s: kind %s, geometry %s, %s\n",
				declared.Name,
				join(declared.Kinds),
				join(permitted(declared)),
				plural(declared.Instances, "instance"),
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
