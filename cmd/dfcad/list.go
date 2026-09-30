// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
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
` + readFlagsHelp + `
` + outputContractHelp + `
The object list-types writes carries "types": one entry per declared type, in
name order, each with its name, the kinds and geometry forms it permits,
whether an instance may omit its geometry, and its instance count. Under
--classification each entry also carries "classifications", in the order the
registry wrote them, each a system and a code.
`

const listPredicatesUsage = `dfcad list-predicates — list the claim predicates the registry declares.

Usage:

	dfcad list-predicates [flags]

Every predicate the registry declares, with the shape its values take, the unit
they are written in, and whether a value under it is a claim. These are the
names "dfcad list-geometry --predicate", "dfcad claims" and "dfcad resolve"
accept, and the only spellings a flag which names a predicate takes. It takes
no arguments: the answer is the whole of that sort of the registry.

The listing reports what was declared and singles nothing out. Which predicate
carries a position, a coordinate reference system or a setback is something the
project wrote down, and a listing which marked one would be the engine choosing.

Flags:

	--describe       include the one line the registry gives each predicate

The descriptions are left out unless they are asked for, for the reason
"dfcad list-types" leaves them out: they are prose about the vocabulary rather
than about this model, and whoever is checking a name pays for them on every
run and reads them on almost none.

Predicates come back in name order, so two runs over one model produce the same
list and a diff between them means something.

A model which declares no predicate at all lists nothing and succeeds. That is
an empty registry rather than a failure.

` + globalFlagsHelp + `
` + readFlagsHelp + `
` + outputContractHelp + `
The object list-predicates writes carries "predicates": one entry per declared
predicate, in name order, each with its name, its shape, its unit where it has
one, its dimension where it is a coordinate, whether it takes a claim, and
whether an ambiguous resolution of it is a failure where it is.
`

const listTolerancesUsage = `dfcad list-tolerances — list the named tolerances the registry declares.

Usage:

	dfcad list-tolerances [flags]

Every named tolerance the registry declares, with its value and the unit that
value was declared in. These are the names "--tolerance", "--chord" and every
other flag which takes a tolerance accept, and the only spellings they take. It
takes no arguments: the answer is the whole of that sort of the registry.

The listing reports what was declared and converts nothing: a tolerance written
in millimetres is listed in millimetres. Nor does it suggest a tolerance for any
operation. Which one a check or a derivation uses is the caller's to name, and a
listing which chose one would be the engine choosing.

Flags:

	--describe       include the one line the registry gives each tolerance

The descriptions are left out unless they are asked for, for the reason
"dfcad list-types" leaves them out: they are prose about the vocabulary rather
than about this model, and whoever is checking a name pays for them on every
run and reads them on almost none.

Tolerances come back in name order, so two runs over one model produce the same
list and a diff between them means something.

A model which declares no tolerance at all lists nothing and succeeds. That is
an empty registry rather than a failure.

` + globalFlagsHelp + `
` + readFlagsHelp + `
` + outputContractHelp + `
The object list-tolerances writes carries "tolerances": one entry per declared
tolerance, in name order, each with its name, its value and its unit.
`

const listFramesUsage = `dfcad list-frames — list the coordinate frames the registry declares.

Usage:

	dfcad list-frames [flags]

Every coordinate frame the registry declares, with its unit, the frame it is
expressed relative to and the claim holding its transform to that frame. These
are the ids "--frame" accepts on every command which takes one, and the only
spellings it takes. It takes no arguments: the answer is the whole of that sort
of the registry.

The listing reports what was declared and converts nothing: a frame's unit is
its one linear unit as written. The root is the frame with no parent, and it
carries neither a parent nor a transform. Nothing is inlined: the transform is
named by the id of its claim, and a claim or a plain value written on a frame
is "dfcad get" of that frame, not something this listing repeats. Nor does it
mark any frame as carrying a coordinate reference system: which predicate names
one is project data.

A frame declares a label and no description, so there is no --describe.

Frames come back in id order, so two runs over one model produce the same list
and a diff between them means something.

A model which declares no frame at all lists nothing and succeeds. That is an
empty registry rather than a failure.

` + globalFlagsHelp + `
` + readFlagsHelp + `
` + outputContractHelp + `
The object list-frames writes carries "frames": one entry per declared frame,
in id order, each with its id, its label where one was written, its unit, and —
on every frame but the root — its parent and the id of its transform claim.
`

const listInstancesUsage = `dfcad list-instances — list the instances of a type.

Usage:

	dfcad list-instances [flags] [type]

The id and the label of every instance of the named type. With no type, every
semantic node in the model, which is how a small model is read whole and a
large one is narrowed with the filters below.

Flags:

	--kind <kind>    only instances which declare this kind; repeat for more
	--frame <id>     only instances which declare this coordinate frame; repeat
	--retired        include the instances which stopped existing

Filters combine: an instance is listed when it satisfies every filter given, and
a filter written more than once is satisfied by any of its values.

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
the seven and for a frame the registry does not declare, whichever of a
repeated filter's values it is.

` + globalFlagsHelp + `
` + readFlagsHelp + `
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

	--predicate <name>   the predicate the node carries; required, and once
	--family <family>    only nodes of this family: vertex, edge or loop;
	                     repeat for more
	--frame <id>         only nodes expressed in this coordinate frame; repeat
	                     for more
	--near "<x> <y> …"   only the vertices within --tolerance of this point, in
	                     the shape --predicate declares; needs one --frame and
	                     --tolerance
	--tolerance <name>   the declared tolerance a vertex has to be within of the
	                     --near point; read only beside --near

--family and --frame are filters. They combine: a node is listed when it
satisfies every filter given, and a filter written more than once is satisfied
by any of its values. A frame matches exactly, as it does for
"dfcad list-instances": a node expressed in a child frame is not listed for its
parent. --predicate is not a filter: it names what is listed, and the answer
reports it as one predicate, so writing it twice is a usage error rather than a
union of two listings.

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

--near asks which vertex is at a coordinate, without writing anything. A
vertex is listed when its position resolves under --predicate, in the unit of
the one --frame, and lies within the --tolerance of the point. That is the rule
"dfcad scaffold-loop" snaps a corner to an existing vertex by, and the two share
it, so the vertex a scaffold would reuse at a corner is the one listed nearest
to it here. Each vertex listed carries "at", where its position resolves, and
"distance", how far that is from the point in the frame's unit; they still come
in id order, and the distance is what picks the nearest. Only a vertex is at a
point, so --family edge or loop beside --near is a usage error. The predicate
has to declare a coordinate, the point has to have the number of components it
declares, and the tolerance has to be declared in the frame's unit, as it does
for scaffold-loop. Nothing within the tolerance is an empty list and exit zero.

A predicate no geometric node carries is an empty list and exit zero. A model
which records no spans is an ordinary model, and answering it with a failure
would make a caller parse a message to tell nothing-there from something-wrong.

A predicate the registry does not declare is a usage error naming it, rather
than an empty list: a predicate nobody declared and a predicate nothing is
written under are different answers, and a caller which cannot tell them apart
retries a misspelling forever. The same holds for a family which is none of the
three and for a frame the registry does not declare, whichever of a repeated
filter's values it is.

` + globalFlagsHelp + `
` + readFlagsHelp + `
` + outputContractHelp + `
The object list-geometry writes carries "predicate", the predicate it was asked
about, and "nodes": one entry per geometric node carrying a live claim under it,
in id order, each with its id, its family, its frame, the span it was written
at, and — for an edge — the two vertices it runs between, in the authored order.
Under --near it also carries "near", the point and the tolerance it was asked
with, and each vertex carries "at" and "distance".
`

// The flags list-geometry takes beyond the global ones, named here because the
// errors which refuse them name them.
const (
	flagPredicate = "predicate"
	flagFamily    = "family"
	flagFrame     = "frame"
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
	loadState

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

// listPredicatesResult is the object list-predicates writes to stdout.
type listPredicatesResult struct {
	envelope
	loadState

	// Predicates is one entry per declared predicate, in name order.
	Predicates []listedPredicate `json:"predicates"`
}

// listedPredicate is one declared predicate as the discovery path reports it.
//
// It is the declaration's axes, in the spelling a caller passes back, and not
// the span it was written at: what a caller is deciding from this is which name
// to pass and what shape of value comes back under it.
type listedPredicate struct {
	// Name is the predicate name, which is what list-geometry --predicate,
	// claims and resolve take.
	Name string `json:"name"`

	// Shape is the shape its values take.
	Shape string `json:"shape"`

	// Unit is the unit its values are written in. Absent for a non-dimensional
	// predicate.
	Unit string `json:"unit,omitempty"`

	// Dimension is how many components a coordinate has. Written for a
	// coordinate and for nothing else, where it would always be zero.
	Dimension int `json:"dimension,omitempty"`

	// ClaimBearing reports whether a value under the predicate is a claim.
	//
	// It is written on every entry, unlike Strict. Its default is true, and an
	// absent boolean reads as false to every JSON consumer, so omitting it on
	// the ordinary case would report every predicate as the exception.
	ClaimBearing bool `json:"claim-bearing"`

	// Strict reports whether an ambiguous resolution is a failure. Written only
	// where it holds, the way Absent is on a listed type: its default is false,
	// which is what an absent field reads as.
	Strict bool `json:"strict,omitempty"`

	// Description is the one line the registry gives the predicate. Written
	// under --describe and absent otherwise, and absent under it too when the
	// registry wrote none.
	Description string `json:"description,omitempty"`
}

// listTolerancesResult is the object list-tolerances writes to stdout.
type listTolerancesResult struct {
	envelope
	loadState

	// Tolerances is one entry per declared tolerance, in name order.
	Tolerances []listedTolerance `json:"tolerances"`
}

// listedTolerance is one declared named tolerance as the discovery path
// reports it.
//
// It is the declaration exactly as written — the magnitude in the unit it was
// declared in, never converted — and not the span it was written at: what a
// caller is deciding from this is which name to pass as a flag.
type listedTolerance struct {
	// Name is the tolerance name, which is what every flag naming a tolerance
	// takes.
	Name string `json:"name"`

	// Value is the declared magnitude.
	Value float64 `json:"value"`

	// Unit is the unit the magnitude was declared in.
	Unit string `json:"unit"`

	// Description is the one line the registry gives the tolerance. Written
	// under --describe and absent otherwise, and absent under it too when the
	// registry wrote none.
	Description string `json:"description,omitempty"`
}

// listFramesResult is the object list-frames writes to stdout.
type listFramesResult struct {
	envelope
	loadState

	// Frames is one entry per declared frame, in id order.
	Frames []listedFrame `json:"frames"`
}

// listedFrame is one declared coordinate frame as the discovery path reports
// it.
//
// It is the declaration exactly as written — the unit never converted, the
// parent and the transform named by id — and nothing written on the frame is
// inlined: a claim or a plain value on it is part of retrieving that frame,
// which is what "dfcad get" of its id returns.
type listedFrame struct {
	// ID is the frame's id, which is what every --frame flag takes.
	ID string `json:"id"`

	// Label is the frame's name for a person. Absent when it was not written.
	Label string `json:"label,omitempty"`

	// Unit is the frame's one linear unit, as declared.
	Unit string `json:"unit"`

	// Parent is the id of the frame this one is expressed relative to. Absent
	// on the root.
	Parent string `json:"parent,omitempty"`

	// Transform is the id of the claim the frame names as its transform to the
	// parent. Absent on the root.
	Transform string `json:"transform,omitempty"`
}

// listInstancesResult is the object list-instances writes to stdout.
type listInstancesResult struct {
	envelope
	loadState

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
	loadState

	// Predicate is the predicate the nodes below carry, which is the one asked
	// for.
	//
	// It travels with the answer because the answer means nothing without it: a
	// caller collecting the listings of a level under three predicates has to be
	// able to say which object answers which question, and an empty list is
	// exactly the object it most needs that of.
	Predicate string `json:"predicate"`

	// Near is the point the listing was narrowed to and the tolerance a vertex
	// had to be within of it. Absent where --near was not written.
	Near *nearEntry `json:"near,omitempty"`

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

	// At is where a vertex's position resolves to, component by component, in
	// the frame's unit. Present only under --near, which is the listing that
	// read it.
	At []float64 `json:"at,omitempty"`

	// Distance is how far the vertex is from the --near point, in the frame's
	// unit. Present only under --near, and a pointer so that a vertex exactly at
	// the point says 0 rather than leaving the field out.
	Distance *float64 `json:"distance,omitempty"`
}

// runListTypes is the list-types command.
func runListTypes(cmd command, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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

	graph, loaded, exit := loadModel(cmd, globals, stdin, stderr)
	if exit != exitSuccess {
		return exit
	}

	result := listTypesResult{
		envelope:  newEnvelope(cmd.name),
		loadState: loaded,

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
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// runListPredicates is the list-predicates command.
func runListPredicates(cmd command, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	describing := flags.Bool("describe", false, "")

	extra, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(extra) > 0 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: extra}, stderr, true)
	}

	graph, loaded, exit := loadModel(cmd, globals, stdin, stderr)
	if exit != exitSuccess {
		return exit
	}

	result := listPredicatesResult{
		envelope:  newEnvelope(cmd.name),
		loadState: loaded,

		// Made rather than declared, as list-types' is, so that a registry
		// declaring nothing writes an empty list rather than a null.
		Predicates: make([]listedPredicate, 0),
	}
	for declared := range graph.Registry().Predicates() {
		entry := listedPredicate{
			Name:         declared.Name,
			Shape:        string(declared.Shape),
			Unit:         string(declared.Unit),
			ClaimBearing: declared.ClaimBearing,
			Strict:       declared.Strict,
		}
		if declared.Shape == dfcad.ShapeCoordinate {
			entry.Dimension = declared.Dimension
		}
		if *describing {
			entry.Description = declared.Description
		}

		result.Predicates = append(result.Predicates, entry)
	}

	reportPredicates(result.Predicates, globals, stderr)

	if err := emit(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// runListTolerances is the list-tolerances command.
func runListTolerances(cmd command, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	describing := flags.Bool("describe", false, "")

	extra, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(extra) > 0 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: extra}, stderr, true)
	}

	graph, loaded, exit := loadModel(cmd, globals, stdin, stderr)
	if exit != exitSuccess {
		return exit
	}

	result := listTolerancesResult{
		envelope:  newEnvelope(cmd.name),
		loadState: loaded,

		// Made rather than declared, as list-types' is, so that a registry
		// declaring nothing writes an empty list rather than a null.
		Tolerances: make([]listedTolerance, 0),
	}
	for declared := range graph.Registry().Tolerances() {
		entry := listedTolerance{
			Name:  declared.Name,
			Value: declared.Value,
			Unit:  string(declared.Unit),
		}
		if *describing {
			entry.Description = declared.Description
		}

		result.Tolerances = append(result.Tolerances, entry)
	}

	reportTolerances(result.Tolerances, globals, stderr)

	if err := emit(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// runListFrames is the list-frames command.
func runListFrames(cmd command, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	extra, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(extra) > 0 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: extra}, stderr, true)
	}

	graph, loaded, exit := loadModel(cmd, globals, stdin, stderr)
	if exit != exitSuccess {
		return exit
	}

	result := listFramesResult{
		envelope:  newEnvelope(cmd.name),
		loadState: loaded,

		// Made rather than declared, as list-types' is, so that a registry
		// declaring nothing writes an empty list rather than a null.
		Frames: make([]listedFrame, 0),
	}
	for declared := range graph.Registry().Frames() {
		result.Frames = append(result.Frames, listedFrame{
			ID:        string(declared.ID),
			Label:     declared.Label,
			Unit:      string(declared.Unit),
			Parent:    string(declared.Parent),
			Transform: string(declared.Transform),
		})
	}

	reportFrames(result.Frames, globals, stderr)

	if err := emit(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// runListInstances is the list-instances command.
func runListInstances(cmd command, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	kindFlag := &repeated{}
	frameFlag := &repeated{}

	flags.Var(kindFlag, "kind", "")
	flags.Var(frameFlag, "frame", "")
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

	kinds, frames := filterOf(*kindFlag), filterOf(*frameFlag)

	// The model is loaded before the arguments are checked because the registry
	// is what says whether a type or a frame exists, and the registry is the
	// model. Its diagnostics reach stderr either way, so a name which is
	// unknown because a registry file did not parse is reported beside the
	// reason it did not.
	graph, loaded, exit := loadModel(cmd, globals, stdin, stderr)
	if exit != exitSuccess {
		return exit
	}
	registry := graph.Registry()

	if err := checkFilters(registry, filterOf([]string{declaredType}), kinds, frames); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	result := listInstancesResult{
		envelope:  newEnvelope(cmd.name),
		loadState: loaded,
		Instances: make([]listedInstance, 0),
	}

	nodes := graph.Nodes().All()
	if declaredType != "" {
		nodes = graph.OfType(declaredType)
	}
	for node := range nodes {
		if !matches(node, kinds, frames, *retired) {
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
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// runListGeometry is the list-geometry command.
func runListGeometry(cmd command, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	predicateFlag := &repeated{}
	familyFlag := &repeated{}
	frameFlag := &repeated{}
	nearFlag := &repeated{}
	toleranceFlag := &repeated{}

	flags.Var(predicateFlag, flagPredicate, "")
	flags.Var(familyFlag, flagFamily, "")
	flags.Var(frameFlag, flagFrame, "")
	flags.Var(nearFlag, flagNear, "")
	flags.Var(toleranceFlag, flagTolerance, "")

	extra, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(extra) > 0 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: extra}, stderr, true)
	}

	// --predicate names what the listing is of, and the answer reports it as one
	// string, so a second one is a second question rather than a wider filter.
	// It is refused before anything else is said about the invocation: every
	// check below is about the one predicate being listed, and a run which wrote
	// two has not said which that is.
	predicate, err := once(flagPredicate, *predicateFlag)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	// The vocabulary is checked before the model is read, which is the one
	// order-of-checks difference from list-instances. A run which did not say
	// which predicate to ask about has not asked a question yet, and no registry
	// can supply the word it left out — so reporting a whole model's diagnostics
	// first would bury the one thing wrong with the invocation.
	if err := vocabularyOf(given{flag: flagPredicate, value: predicate}); err != nil {
		return usageError(cmd, err, stderr, true)
	}

	// A family is checked before the load too, for the same reason: the three
	// are a closed set compiled in, so nothing in the tree makes `--family
	// vertexes` any more of a family.
	wantedFamilies := filterOf(*familyFlag)
	for _, asked := range wantedFamilies {
		if err := checkFamily(asked); err != nil {
			return usageError(cmd, err, stderr, false)
		}
	}

	// A point and a tolerance each name one thing, so a second of either is a
	// second question rather than a wider one, as a second predicate is.
	point, err := once(flagNear, *nearFlag)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	tolerance, err := once(flagTolerance, *toleranceFlag)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	near := nearAsked{
		point:     point,
		tolerance: tolerance,
		predicate: predicate,
		frames:    filterOf(*frameFlag),
		families:  wantedFamilies,
	}

	// Which flags a lookup needs beside it, and that only a vertex is at a
	// point, are settled before the load for the reason a family is: nothing in
	// the tree changes either.
	if err := near.check(); err != nil {
		return usageError(cmd, err, stderr, true)
	}

	// The predicate, in contrast, is registry data, and the registry is the
	// model. Its diagnostics reach stderr either way, so a predicate which is
	// unknown because a registry file did not parse is reported beside the
	// reason it did not.
	graph, loaded, exit := loadModel(cmd, globals, stdin, stderr)
	if exit != exitSuccess {
		return exit
	}

	if err := checkPredicate(graph.Registry(), predicate); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	// A frame is registry data as well, and is refused the way list-instances
	// refuses one, through the same check: one filter with one meaning, whichever
	// listing it narrows.
	wantedFrames := filterOf(*frameFlag)
	if err := checkFilters(graph.Registry(), nil, nil, wantedFrames); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	result := listGeometryResult{
		envelope:  newEnvelope(cmd.name),
		loadState: loaded,
		Predicate: predicate,

		// Made rather than declared so that a predicate nothing carries writes an
		// empty list rather than a null, and a caller indexing it needs no
		// special case for the model which records no spans.
		Nodes: make([]listedGeometry, 0),
	}

	// The vertices at the point, by the rule scaffold-loop snaps a corner by,
	// and nil where no point was asked about.
	var nearby map[dfcad.ID]dfcad.Nearby

	if near.asked() {
		found, err := near.lookup(graph)
		if err != nil {
			return usageError(cmd, err, stderr, false)
		}

		result.Near = &nearEntry{At: found.Point, Tolerance: declared(found.Tolerance)}

		nearby = make(map[dfcad.ID]dfcad.Nearby, len(found.Vertices))
		for _, vertex := range found.Vertices {
			nearby[vertex.Vertex] = vertex
		}
	}

	topology := graph.Topology()

	if admits(wantedFamilies, familyVertex) {
		for vertex := range topology.Vertices() {
			if !admits(wantedFrames, string(vertex.Frame())) || !carries(graph, vertex.ID(), predicate) {
				continue
			}

			listed := listedGeometry{
				ID:     string(vertex.ID()),
				Family: familyVertex,
				Label:  vertex.Label(),
				Frame:  string(vertex.Frame()),
				Span:   vertex.Span(),
			}

			if nearby != nil {
				found, ok := nearby[vertex.ID()]
				if !ok {
					continue
				}

				distance := found.Distance
				listed.At, listed.Distance = found.At, &distance
			}

			result.Nodes = append(result.Nodes, listed)
		}
	}

	// Only a vertex is at a point, so a lookup lists no edge and no loop even
	// where no family was named.
	if admits(wantedFamilies, familyEdge) && nearby == nil {
		for edge := range topology.Edges() {
			if !admits(wantedFrames, string(edge.Frame())) || !carries(graph, edge.ID(), predicate) {
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

	if admits(wantedFamilies, familyLoop) && nearby == nil {
		for loop := range topology.Loops() {
			if !admits(wantedFrames, string(loop.Frame())) || !carries(graph, loop.ID(), predicate) {
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
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// filterOf is the values a filter flag was written with, less the empty ones.
//
// An empty value has always been no filter at all — `--kind ""` lists every
// kind — and a filter which takes a repeat keeps it that way, so that an
// invocation which wrote each filter at most once answers exactly as it did
// before a repeat was honoured. The values stay in the order they were written,
// which is the order they are validated in and so the one whose first unknown
// name the usage error reports.
func filterOf(written []string) []string {
	out := make([]string, 0, len(written))
	for _, value := range written {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

// admits reports whether a value satisfies a filter: any of its values, or
// anything at all when it was not given.
//
// Any rather than every, because a thing declares one kind, one type, one frame
// and belongs to one family, so a filter holding two of them which demanded both
// would answer nothing by construction. That is the rule "dfcad check" has
// always had for its own filters, and it is one rule for every command rather
// than a decision each of them makes: within one flag any of its values, across
// flags every flag given.
func admits(filter []string, value string) bool {
	return len(filter) == 0 || slices.Contains(filter, value)
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

// checkFilters reports the first filter value which names something the model
// has no such thing of: the types, then the kinds, then the frames, each in the
// order they were written.
//
// An unknown name is a usage error rather than an empty list. A type nobody
// declared and a type nothing instantiates are different answers, and a caller
// which cannot tell them apart retries a misspelling forever; the same is true
// of a kind and of a frame. It is just as true of the second value of a repeated
// filter as of the first: answering with the values which did name something
// would be a narrower listing that reads as the one which was asked for.
func checkFilters(registry *dfcad.Registry, types, kinds, frames []string) error {
	for _, declaredType := range types {
		if !registry.Declares(dfcad.SortType, declaredType) {
			return UnknownTypeError{Type: declaredType, Declared: registry.Names(dfcad.SortType)}
		}
	}

	for _, kind := range kinds {
		if !slices.Contains(dfcad.Kinds(), dfcad.Kind(kind)) {
			return UnknownKindError{Kind: kind, Known: dfcad.Kinds()}
		}
	}

	for _, frame := range frames {
		if !registry.Declares(dfcad.SortFrame, frame) {
			return UnknownFrameError{Frame: frame, Declared: registry.Names(dfcad.SortFrame)}
		}
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
func matches(node *dfcad.SemanticNode, kinds, frames []string, retired bool) bool {
	if node.Retired() && !retired {
		return false
	}

	if !admits(kinds, string(node.Kind())) {
		return false
	}

	if len(frames) > 0 {
		id, ok := node.Frame()
		if !ok || !admits(frames, string(id)) {
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

// loadModel reads the whole model beneath the root, renders whatever is wrong
// with it to stderr, and says whether the load refused it.
//
// It is the load a discovery read makes: the listings, get, traverse, claims
// and conflicts. Diagnostics go to stderr on every run and in every format,
// because they are for whoever wrote the file. The graph which comes back is
// usable whatever they say, so a listing of a model somebody is part way
// through writing is still a listing of what is there.
//
// They do not change the exit code. A listing says what a model holds, and a
// node whose containment does not resolve is still a node the model holds; the
// question of whether the model is sound is what `dfcad check` answers, and
// answering it twice, in two commands, with two definitions of sound, is how
// the two come to disagree. It also keeps discovery usable on a model somebody
// is halfway through writing, which is the model discovery is most needed on:
// a call that refuses to describe a tree until the tree is finished is a call
// nobody reaches for.
//
// What it does change is the answer object, which carries the [loadState] this
// returns: a caller reading the exit code alone cannot tell a listing of a
// model which loads from one of a model which does not, and a caller acting on
// the listing is owed that difference in the object it acts on.
//
// A derivation — measure, resolve, tessellate, buildable, site, plan, route —
// does not load through here. Its answer is a figure computed out of the
// model, and a figure computed out of a model the load refused is an answer to
// a question nobody asked, so it loads through [loadGate] and exits as a load
// failure instead.
//
// A run told to assume a batch is the exception: a batch whose base tree or
// result the load refused is refused outright, as [loadGate] says, and the
// exit code that comes back is what the command returns without answering.
func loadModel(cmd command, globals *globals, stdin io.Reader, stderr io.Writer) (*dfcad.Graph, loadState, int) {
	graph, refused, exit := loadGate(cmd, globals, stdin, stderr)
	return graph, loadState{Refused: refused}, exit
}

// loadState is what a discovery read's answer says about the load it was read
// out of, embedded after the envelope of every object [loadModel] feeds.
type loadState struct {
	// Refused reports that the load refused the model: that at least one
	// diagnostic on stderr is an error rather than a warning, which is what
	// makes `dfcad check` exit 2 over the same tree. The answer beside it is
	// still what the model holds, read through those errors.
	Refused bool `json:"refused"`
}

// loadGate is [loadModel] with what the diagnostics said about the model kept,
// which is what a gate needs and a listing does not.
//
// It reports whether the load refused the model — whether any diagnostic is an
// error rather than a warning — and is the one place which decides that, so
// that a read which ignores it and a gate which acts on it are reading the same
// answer.
//
// It is also the one place a read honours --assume, which is what makes every
// read answer over a hypothetical without any of them knowing: under the flag
// the graph which comes back is the model the batch would produce
// ([dfcad.Assume]), which a command cannot tell from one read off disk. The exit
// code is [exitSuccess] where the command should go on and answer, and the code
// it returns without answering otherwise — which only a run under --assume ever
// sees, because a batch is refused as apply refuses it and the read does not
// run (docs/decisions/0030-a-read-may-assume-a-batch.md).
func loadGate(cmd command, globals *globals, stdin io.Reader, stderr io.Writer) (*dfcad.Graph, bool, int) {
	if globals.Assume.given {
		graph, exit := assumeGate(cmd, globals, stdin, stderr)
		return graph, false, exit
	}

	reportLoading(cmd, globals, stderr)

	graph, found := dfcad.LoadGraph(globals.Root)
	hold(stderr, graph)

	refused := render(found, stderr)
	if refused {
		refuse(stderr)
	}

	return graph, refused, exitSuccess
}

// assumeGate is [loadGate] under --assume: it reads the batch, interprets the
// model it would produce and notes the batch on the run's stderr, so that the
// object the command writes says it is hypothetical.
//
// Every refusal is apply's, with apply's exit code and in apply's words, because
// a batch which may be assumed is a batch which may be applied and the other way
// round: a file which cannot be read or is not a batch, a base tree which does
// not load and a result which would not load exit 2, and an operation the model
// refuses exits 3. Stdout is what `apply --dry-run` writes for the same input —
// nothing, or the refusal where a load refused — and never an answer, because a
// model nobody could write is not answered about. That holds for the discovery
// reads and `check` as well, which answer through a refused tree without the
// flag.
func assumeGate(cmd command, globals *globals, stdin io.Reader, stderr io.Writer) (*dfcad.Graph, int) {
	batch, exit, ok := batched(cmd, globals, []string{globals.Assume.path}, stdin, stderr)
	if !ok {
		return nil, exit
	}

	reportLoading(cmd, globals, stderr)

	assumed, diags, err := dfcad.Assume(globals.Root, batch)

	// Every diagnostic from here on, the refusal's and whatever the read goes
	// on to render, is about the tree the batch would produce, which is not on
	// disk, so it is quoted from what the engine printed for it.
	quoteFrom(stderr, assumed.Sources)

	refused := render(diags, stderr)

	if err != nil {
		if errors.As(err, new(dfcad.RootError)) {
			_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
			return nil, exitLoad
		}
		return nil, usageError(cmd, err, stderr, false)
	}

	if refused || assumed.Graph == nil {
		refuse(stderr)
		return nil, exitLoad
	}

	hold(stderr, assumed.Graph)

	digest, _ := assumed.Graph.Digest()
	assume(stderr, assumedBatch{
		Batch:      globals.Assume.path,
		Operations: len(batch.Operations),
		Base:       assumed.Base.String(),
		Digest:     digest.String(),
	})

	return assumed.Graph, exitSuccess
}

// usageError reports an invocation which named something that does not exist.
//
// The usage message follows a wrong shape of invocation — an argument too many
// — because the shape is what it documents. It does not follow a name the model
// does not declare: the answer there is the name and where to look for the real
// ones, and a page of flags between the two buries it.
func usageError(cmd command, err error, stderr io.Writer, withUsage bool) int {
	_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
	if withUsage {
		_, _ = fmt.Fprint(stderr, "\n")
		_, _ = fmt.Fprint(stderr, cmd.usage)
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
			_, _ = fmt.Fprintf(stderr, "%s: kind %s, geometry %s, %s%s\n",
				declared.Name,
				join(declared.Kinds),
				join(permitted(declared)),
				plural(declared.Instances, "instance"),
				classifiedAs(declared),
			)
		}
	}

	_, _ = fmt.Fprintf(stderr, "%s, %s\n", plural(len(types), "type"), plural(instances, "instance"))
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

// reportPredicates renders a list-predicates result for a person, on stderr:
// one line per predicate, then how many there were.
//
// The lines are not behind the verbosity flag, as list-types' are. A person
// asking which predicates there are is asking for the names, and a count on its
// own answers nothing they asked.
func reportPredicates(predicates []listedPredicate, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	for _, declared := range predicates {
		_, _ = fmt.Fprintf(stderr, "%s: %s\n", declared.Name, join(spelledPredicate(declared)))
	}

	_, _ = fmt.Fprintf(stderr, "%s\n", plural(len(predicates), "predicate"))
}

// spelledPredicate is what a predicate declares, spelled for a person: its
// shape, its unit, and the two opt-outs of the defaults where it wrote them.
func spelledPredicate(declared listedPredicate) []string {
	shape := declared.Shape
	if declared.Dimension > 0 {
		shape = fmt.Sprintf("%s in %d dimensions", shape, declared.Dimension)
	}

	unit := declared.Unit
	if unit == "" {
		unit = "no unit"
	}

	out := []string{shape, unit}
	if !declared.ClaimBearing {
		out = append(out, "plain")
	}
	if declared.Strict {
		out = append(out, "strict")
	}
	return out
}

// reportTolerances renders a list-tolerances result for a person, on stderr:
// one line per tolerance, then how many there were.
//
// The lines are not behind the verbosity flag, for the reason list-predicates'
// are not: a person asking which tolerances there are is asking for the names,
// and a count on its own answers nothing they asked.
func reportTolerances(tolerances []listedTolerance, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	for _, declared := range tolerances {
		_, _ = fmt.Fprintf(stderr, "%s: %s %s\n", declared.Name, number(declared.Value), declared.Unit)
	}

	_, _ = fmt.Fprintf(stderr, "%s\n", plural(len(tolerances), "tolerance"))
}

// reportFrames renders a list-frames result for a person, on stderr: one line
// per frame — its id, its unit, and the frame it is expressed relative to where
// it has one — then how many there were.
//
// The lines are not behind the verbosity flag, for the reason list-predicates'
// are not: a person asking which frames there are is asking for the chain, and
// a count on its own answers nothing they asked.
func reportFrames(frames []listedFrame, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	for _, declared := range frames {
		if declared.Parent == "" {
			_, _ = fmt.Fprintf(stderr, "%s: %s\n", declared.ID, declared.Unit)
			continue
		}

		_, _ = fmt.Fprintf(stderr, "%s: %s → %s\n", declared.ID, declared.Unit, declared.Parent)
	}

	_, _ = fmt.Fprintf(stderr, "%s\n", plural(len(frames), "frame"))
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
			_, _ = fmt.Fprintf(stderr, "%s: %s, %s %s\n", instance.ID, label, instance.Kind, instance.Type)
		}
	}

	_, _ = fmt.Fprintf(stderr, "%s of %s\n", plural(len(instances), "instance"), plural(len(types), "type"))
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
			_, _ = fmt.Fprintf(stderr, "%s: %s%s in %s%s\n", node.ID, node.Family, between(node), node.Frame, away(node, result.Near))
		}
	}

	spread := make([]string, 0, len(families))
	for _, family := range families {
		spread = append(spread, pluralOf(counted[family], family, familyPlurals[family]))
	}

	_, _ = fmt.Fprintf(stderr, "%s under %s%s: %s\n",
		plural(len(result.Nodes), "geometric node"), result.Predicate, within(result.Near), join(spread))
}

// within is the point a listing was narrowed to, for a person, and nothing for
// a listing which was not.
func within(near *nearEntry) string {
	if near == nil {
		return ""
	}
	return fmt.Sprintf(" within %s (%g %s) of %s",
		near.Tolerance.Name, near.Tolerance.Value, near.Tolerance.Unit, components(near.At))
}

// away is how far a listed vertex is from the point, for a person, and nothing
// for a listing which asked about no point.
func away(node listedGeometry, near *nearEntry) string {
	if near == nil || node.Distance == nil {
		return ""
	}
	return fmt.Sprintf(", at %s, %g %s away", components(node.At), *node.Distance, near.Tolerance.Unit)
}

// components is a coordinate for a person, as it is written on a command line.
func components(at []float64) string {
	out := make([]string, 0, len(at))
	for _, component := range at {
		out = append(out, strconv.FormatFloat(component, 'g', -1, 64))
	}
	return "(" + strings.Join(out, " ") + ")"
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
