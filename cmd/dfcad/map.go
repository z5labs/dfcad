// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/z5labs/dfcad"
	"github.com/z5labs/dfcad/gml"
)

const exportMapUsage = `dfcad export-map — write the model's regions as a georeferenced vector file.

Usage:

	dfcad export-map [flags]

Every node the model gives a shape to, written as a GML 3.2 feature: the rings
bounding it, holes and all, in the coordinates of the frame the chain is rooted
at, with the identifier of that frame's coordinate reference system on every
geometry. What comes back is a layer a GIS opens with its coordinate system
already attached, so placing the model on the earth does not depend on somebody
remembering which projection it was drawn in.

A node whose declared geometry is "point" is written as a point feature rather
than as a ring, from the position claimed of the node itself under --position.
A panel, a condenser, a receptacle and a survey monument are each a thing whose
only interesting geometry is where it is, and a rectangle drawn around one
would be dimensions nobody measured.

A node whose declared geometry is "line" is written as a line feature: a
gml:MultiCurve holding one gml:LineString per loop of its boundary, the corners
of the run in the order the loop walks them, left open rather than closed. A
trench, a fence, a railing and a partition drawn as its centreline each run
somewhere and cover nothing, and a polygon drawn around one would be a width
nobody measured. A run is read exactly as a ring is: a curved edge in it is
drawn to --chord, it is carried into the root frame by the same chain of
transforms, and it is refused where its corners do not lie at one level.

The format is GML rather than the interchange format most reached for, and the
reason is recorded in
docs/decisions/0023-the-map-export-names-its-coordinate-system-in-the-file.md.
The short of it: GeoJSON fixes its coordinates at WGS 84 longitude and latitude
and removed the member which would have named anything else, so writing a
projected survey as that format means either violating it or reprojecting — and
reprojection is the geodesy this engine has decided not to do.

Nothing here reprojects, and there is no code path which could. The coordinates
written are the model's own and the identifier is carried verbatim: it is
checked for shape only, an authority and a code, and nothing resolves it,
converts it or looks it up.

A corner authored on the root frame reaches the file as the digits it was
written with. One authored on another frame, or one drawn along a curve, has
arithmetic done to it on the way — the measured transform which carries it, the
plane the arc is swept in — so it can land a bit or two from the decimal
somebody typed: 2000000.0 written as 1999999.9999999995, about 5e-10 of a foot
away. That is the last bits of a double and not a reprojection, and it is why a
check over this file compares a coordinate to a tolerance rather than as text.

The artefact is a build output and never lands in the authored tree. By default
it is written beneath .dfcad/export, in a directory named for the digest of the
tree it was derived from — the same directory the IFC export writes into, so
one revision's artefacts sit together. --out names somewhere else, and
somewhere else has to be outside the model root.

The same tree exports to the same bytes, on any machine and at any time.
Nothing here reads a clock. So re-exporting a tree nothing happened to writes
nothing and reports the file it found as "unchanged".

Flags:

	--out <path>               where to write the file (default: beneath
	                           .dfcad/export, in a directory named for the
	                           digest of the source tree)
	--position <predicate>     the predicate a corner's position is claimed
	                           under, which an outline is read from and which
	                           a node drawn as a point is placed by
	--tolerance <name>         the tolerance corners are judged coincident
	                           against and rings judged planar against
	--chord <name>             the tolerance a segment standing in for a curve
	                           may fall from it by
	--arc-centre <predicate>   the predicate a curved edge's centre is claimed
	                           under
	--arc-through <predicate>  the predicate the point a curved edge passes
	                           through is claimed under
	--crs <predicate>          the predicate the identifier of the project's
	                           coordinate reference system is written under

The first three go together and are required, unlike on export: a map is its
geometry, and a file of features with no shapes is not a degraded answer but an
empty one. --arc-centre and --arc-through go together or not at all, and a run
which names neither draws a curved edge as the straight line between its ends.

--crs has no default and never will. It names the predicate the root frame
carries the identifier of its projected system under — a non-claim-bearing text
predicate, written (crs "EPSG:6543"). A run which names none still writes the
file, and says so: the features are then in coordinates nothing in the document
identifies, which a reader has to be told about out of band and is the thing
this command exists to avoid.

A coordinate reference system written on any frame but the root is refused. The
root frame is the projected system the chain is rooted at, and every other
frame reaches it through a measured transform, so a second georeference beside
it would be a second answer nothing reconciles.

A region outlined on another frame is carried into the root frame by exactly
that chain of measured transforms, which is the same arithmetic dfcad site does
across frames and is the only thing in this command which moves a coordinate.
Where the chain does not reach, the export is refused rather than written with
the coordinates left where they were.

The elevation is dropped. A map is a plan, and each feature is written with two
ordinates, easting then northing, which is the axis order this format's readers
assume for a system named as an authority and a code. A boundary whose corners
do not lie at one level is refused for that reason: the plan of it would be a
projection this command chose.

` + mapPropertiesHeading + `

	id      the id of the node the feature was drawn from
	label   what that node is called
	kind    the kind the model gives it
	type    the type the project declared it as
	within  the node which immediately contains it
	frame   the frame its outline was declared in

A reader sees them in this tool's namespace, as dfcad:id, dfcad:label and so
on. Only id is written on every feature; the rest are written where the node
has them and are absent, rather than empty, where it does not. The id is a
property rather than the feature's XML identifier because an id in this
model's spelling — namespace, colon, local part — is not a name XML can write,
and because a property is what a GIS shows, sorts by and joins on.

within is the immediate container and never an ancestor: a room reports the
storey it sits on and not the site that storey stands on. Asking what a whole
site holds is therefore a join the reader makes — follow within from feature
to feature until it names nothing this document holds — or one dfcad traverse
contains <id> against the model, whose answer is in the same ids the id
property carries.

There is no selector, and a run cannot be narrowed to a kind, a type or a
container: every node the model gave a shape to is drawn, so a document of a
model holding rooms, countertops and closets holds all three stacked on the
parcel. Choosing what a sheet shows is the reader's, and the properties above
are the contract for doing it — a layer filter, a definition query or a style
rule written against them is how a plan of one storey comes out of a document
of the whole model
(docs/decisions/0025-the-map-export-draws-every-region-and-its-properties-are-the-filter.md).

The namespace those properties are written in identifies this vocabulary and
resolves to nothing, and the document names no xsi:schemaLocation. Both are
deliberate: there is no schema to fetch, GDAL asks for neither, and a schema
published at a URL would be a second definition of this document to keep in
step with the writer. A reader which refuses a document it cannot resolve a
schema for will refuse this one.

` + globalFlagsHelp + `
` + readFlagsHelp + `
` + outputContractHelp + `
The object export-map writes carries "derived", the "digest" of the source tree
the artefact was derived from, the "schema" it was written in, the "chord"
tolerance the document was drawn to with the "deviation" that drawing achieved,
and "files": one entry per file the artefact consists of, each with the "path"
it is at and a "status" of "written" or "unchanged".

Where a node the model gives a shape to is not a feature of the document it
also carries "undrawn": one entry per such node, in id order, with its "node"
id, its label, kind and type, and a "reason" — "unreadable-boundary" for one
whose edges could not be read, "no-position" for a node drawn as a point which
nothing places, "unrooted" for a model whose frames reach no root, "uncarried"
for one drawn on a frame the chain does not relate to the root, "not-level" for
one whose corners do not lie at one level. The features and "undrawn" account
between them for every shaped node the model has not retired, so nothing the
model shaped leaves this command without a word. Every entry is also an error on
stderr, so a run which names one writes no file. The key is absent where every
node was drawn.

The chord and the deviation are of the document rather than of any one feature,
and they are in the answer because the file carries neither: a GML document is
positions, and a reader holding one cannot tell a ring which follows its curve
to a tenth of a foot from one drawn coarsely. They are what a downstream check
reads to assert the layer was drawn to the tolerance it intended.

Where a curve went unread the object carries "chorded": the edges which state
one, each with the predicates it states it under, and no "deviation" at all. A
feature drawn straight through a curve nothing read is a boundary in the wrong
place in a file somebody keeps, and a deviation of nothing beside a named chord
tolerance would be this command saying it is in the right one.

Exit code 1 is a model no artefact could be made of: a region or a run which
could not be drawn, one which could not be carried into the root frame, a
coordinate reference system written where it does not belong. The object still comes back,
with "derived" false, no files and the refusal under "diagnostics", so a caller
reads why from the object rather than from stderr. Exit code 3 is a destination inside the
authored tree, which is refused before anything is read.
`

// mapPropertiesHeading opens the block of the help text above which lists the
// properties every feature carries.
//
// It is a constant because a test reads that block back out of the usage and
// requires it to name exactly the properties a document holds, no more and no
// fewer. The help text used to describe a property called `container` which no
// run has ever written — the feature carries `within` — and a filter written
// against the documented name selects nothing and raises nothing, which is a
// defect a reader finds by measuring rather than by being told.
const mapPropertiesHeading = "A feature carries these properties, in the order they are written and where the node has them:"

// mapFile is the name of the one file a map export consists of.
const mapFile = "model.gml"

// mapSchema is the schema the artefact is written in, as the result reports
// it.
//
// It is built from [gml.Version] rather than spelled again here, because a
// result which named a version the document does not conform to would be worse
// than one which named none.
const mapSchema = "GML " + gml.Version

// The vocabulary the features are written in.
//
// The namespace is this tool's rather than the project's, because the property
// names below are this tool's words: `kind` means what SPEC §4.3 says it means
// whichever model is being exported. The trailing number is the version of
// that vocabulary, and it moves when a property here changes meaning — which
// is what a namespace URI is for. It identifies a vocabulary and is not
// expected to resolve to anything.
const (
	mapNamespace = "https://github.com/z5labs/dfcad/gml/1"
	mapPrefix    = "dfcad"
	mapType      = "region"
	mapID        = "model"
)

// The properties every feature carries, in the order they are written.
//
// They are the node taken apart rather than rendered into a label: a GIS shows
// one column per property, so somebody styling a plan by kind, filtering it by
// type or joining it back to the model by id can do each of those without
// parsing anything out of a string. That is the whole of this command's answer
// to selection — it draws every region it can and narrows nothing
// ([0025](docs/decisions/0025-the-map-export-draws-every-region-and-its-properties-are-the-filter.md))
// — so these names are a contract a downstream filter is written against, and
// the help text is required by a test to name exactly them.
//
// propertyWithin is the immediate container and never an ancestor, because it
// is [dfcad.SemanticNode.Within] written out and that is what containment is in
// this model: the transitive answer is a walk, which `dfcad traverse contains`
// already does against the model itself.
const (
	propertyNodeID = "id"
	propertyLabel  = "label"
	propertyKind   = "kind"
	propertyType   = "type"
	propertyWithin = "within"
	propertyFrame  = "frame"
)

// mapFeature is what the gml:id of each feature is built from: this word and
// the feature's position in the document.
//
// It is an ordinal rather than the node's id, and that is not a shortcut. A
// gml:id is an XML name — no colons, no slashes, no spaces — and a node's id is
// a namespace and a local part which may hold any of those, so mapping one to
// the other means a substitution, and a substitution means two different nodes
// can arrive at one name. The identity of the thing is in the `id` property,
// where it is text and is under no such rule, and the identifier here is what
// it is for: telling one element of this document from another.
const mapFeature = mapType + "."

// exportMapResult is the object export-map writes to stdout.
//
// It is the artefact-command shape
// ([0022](docs/decisions/0022-a-command-whose-product-is-a-file-answers-on-stdout.md)),
// and it is the same shape `export` writes for the reason the two commands are
// two: what differs between them is the format, and a caller driving both
// reads one contract.
type exportMapResult struct {
	envelope

	// Derived reports whether an artefact was produced.
	Derived bool `json:"derived"`

	// Digest is the digest of the source tree the artefact was derived from,
	// which is what lets a caller say whether the artefact in front of them is
	// the one this tree produces. It is written on a refusal too.
	Digest string `json:"digest,omitempty"`

	// Schema is the version of GML the document conforms to.
	Schema string `json:"schema,omitempty"`

	// Chord is the tolerance the curves in the document were drawn to and
	// Deviation how far the worst segment of the worst feature actually falls
	// from the curve it stands in for, in the unit that tolerance is declared
	// in. Both describe the whole document rather than any one feature, which
	// is the granularity a consumer of a layer has.
	//
	// They are here because the file carries neither. A GML document is
	// positions, so a reader holding one has no way to tell a ring which
	// follows its curve to a tenth of a foot from one which was drawn coarsely
	// — and a map is drawn once and read for years. Reporting them is what lets
	// a downstream check assert that the layer it was handed was drawn to the
	// tolerance it intended.
	//
	// The unit is the chord tolerance's own, which is the frame's: a tolerance
	// declared in any other unit than the frame it is applied in refuses the
	// drawing outright, so every region in a document which was written shares
	// this one.
	//
	// Both are absent for a run which drew no curve to any tolerance at all,
	// and Deviation alone is absent where Chorded below is not — see there.
	Chord     *toleranceEntry `json:"chord,omitempty"`
	Deviation *measuredValue  `json:"deviation,omitempty"`

	// Chorded is the edges of the drawn regions which state a curve this run
	// did not read, each with the predicates it states it under, over the whole
	// model and each edge once. Absent for a run which read every curve and for
	// a model which claims none.
	//
	// The artefact is the reason this is reported and the reason Deviation is
	// not reported beside it. A feature drawn straight through a curve is a
	// parcel boundary in the wrong place in a file somebody keeps, and a
	// deviation of nothing written next to a named chord tolerance is an
	// affirmative statement that it is in the right one.
	Chorded []chordedEntry `json:"chorded,omitempty"`

	// Undrawn is one entry per node the model gives a shape to which is not a
	// feature of the document, in id order: its id, what it is, and the reason.
	// Absent where every such node was drawn.
	//
	// It is what makes the document an account of the model rather than of the
	// shapes this command happened to manage. The features written and the
	// nodes named here are, between them, every shaped node the model has not
	// retired — so a reader who finds a thing missing from the layer finds it
	// here, and one who finds nothing here knows the layer is the whole model.
	// A node named here is an error on stderr too, so a run which names one
	// writes no file: a layer with one plot quietly missing is worse than none.
	Undrawn []mapUndrawnEntry `json:"undrawn,omitempty"`

	// Files is one entry per file the artefact consists of. It describes files
	// which are on disk and never anything else, and it is empty rather than
	// absent when there are none.
	Files []exportedFile `json:"files"`
}

// mapUndrawnEntry is one shaped node the document does not hold, and why.
//
// It is the plan's undrawn entry without the annotations: a map carries no
// claims, so there is nothing written on a node for this to report, and what a
// reader needs is which node, what it is, and what stopped it.
type mapUndrawnEntry struct {
	// Node is the id of the node which was not drawn.
	Node string `json:"node"`

	// Label is what it is called, absent where it is called nothing. Kind and
	// Type are what it is.
	Label string `json:"label,omitempty"`
	Kind  string `json:"kind,omitempty"`
	Type  string `json:"type,omitempty"`

	// Reason is why, as a token a caller branches on rather than a sentence it
	// matches. The diagnostics carry the rest — the loop, the file, the line —
	// on stderr and under "diagnostics" in the same object.
	Reason undrawnReason `json:"reason"`
}

// undrawnReason is why a shaped node is not a feature of the document.
type undrawnReason string

// The reasons a shaped node is not drawn.
//
// The first three are the plan's own, spelled from the engine's constants
// rather than again here, because they are the same findings about the same
// model and a caller reading both answers should not have to learn two words
// for one. The rest are this command's: they are about putting a shape in the
// root frame and on a plan, which is what a map does and a plan does not.
const (
	// undrawnUnreadable is a node whose boundary could not be read: a ring
	// which does not close, one which crosses itself, a tolerance the
	// registry does not declare.
	undrawnUnreadable = undrawnReason(dfcad.UndrawnUnreadableBoundary)

	// undrawnNoPosition is a node drawn as a point which nothing places.
	undrawnNoPosition = undrawnReason(dfcad.UndrawnNoPosition)

	// undrawnUncarried is a node drawn on a frame the chain of measured
	// transforms does not relate to the root. A plan names a node it cannot
	// carry into its own frame with the same word.
	undrawnUncarried = undrawnReason(dfcad.UndrawnUncarried)

	// undrawnUnrooted is a node in a model whose frames reach no root, so
	// there are no coordinates to write it in.
	undrawnUnrooted undrawnReason = "unrooted"

	// undrawnUnlevel is a node whose corners do not lie at one level in the
	// root frame, which has no plan this command will draw.
	undrawnUnlevel undrawnReason = "not-level"
)

// runExportMap is the export-map command.
func runExportMap(cmd command, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	out := flags.String(flagOut, "", "")

	position := flags.String(flagPosition, "", "")
	tolerance := flags.String(flagTolerance, "", "")
	chord := flags.String(flagChord, "", "")
	centre := flags.String(flagArcCentre, "", "")
	through := flags.String(flagArcThrough, "", "")

	crs := flags.String(flagCRS, "", "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(arguments) > 0 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments}, stderr, true)
	}

	// Required here rather than optional as they are on `export`. A spatial
	// structure with no shape in it is a correct IFC file; a vector layer with
	// no shape in it is a file with nothing in it at all.
	if err := vocabularyOf(
		given{flagPosition, *position},
		given{flagTolerance, *tolerance},
		given{flagChord, *chord},
	); err != nil {
		return usageError(cmd, err, stderr, true)
	}

	if err := arcVocabularyOf(*centre, *through); err != nil {
		return usageError(cmd, err, stderr, true)
	}

	drawn := shapes{
		position:   *position,
		tolerance:  *tolerance,
		chord:      *chord,
		arcCentre:  *centre,
		arcThrough: *through,
	}

	// The destination is settled before the model is read, so a mistake in the
	// invocation is reported as one rather than after a load which was never
	// going to be used.
	destination := ""
	if *out != "" {
		resolved, err := exportDestination(globals, *out)
		if err != nil {
			return usageError(cmd, err, stderr, false)
		}
		destination = resolved
	}

	graph, refused, exit := loadGate(cmd, globals, stdin, stderr)
	if exit != exitSuccess {
		return exit
	}
	if refused {
		return exitLoad
	}

	result := exportMapResult{
		envelope: newEnvelope(cmd.name),
		Files:    []exportedFile{},
	}

	digest, keyed := graph.Digest()
	if keyed {
		result.Digest = digest.String()
	}

	collection, made, diags := drawnMap(graph, drawn, georeference{identifier: *crs})

	// The curves nothing read are a fact about the model rather than about the
	// file, so they are written whether or not a file came of it: a refused run
	// which also chorded a boundary has two things wrong with it and reporting
	// one of them sends the author round twice.
	result.Chorded = made.chorded
	result.Undrawn = made.undrawn

	if destination == "" {
		destination = filepath.Join(dfcad.ExportDir(globals.Root), digest.String(), mapFile)
	}

	if render(diags, stderr) {
		if err := emit(stdout, result); err != nil {
			_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
			return exitLoad
		}
		return exitCheck
	}

	var written bytes.Buffer
	if err := gml.Write(&written, collection); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)

		if err := emit(stdout, result); err != nil {
			_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
			return exitLoad
		}
		return exitCheck
	}

	status, err := place(destination, written.Bytes())
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	result.Derived = true
	result.Schema = mapSchema
	result.Files = append(result.Files, exportedFile{Path: destination, Status: status})

	// What the document was drawn to, on every run which drew anything. The
	// deviation goes with it only where every curve was read: a run which
	// chorded one is a document whose distance from the boundary it came from
	// is however far that wall bows, and this figure is not it.
	if made.chord.Name != "" {
		chord := declared(made.chord)
		result.Chord = &chord

		if len(made.chorded) == 0 {
			result.Deviation = &measuredValue{Value: made.deviation, Unit: string(made.chord.Unit)}
		}
	}

	reportExportMap(result, globals, stderr)

	if err := emit(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// reportExportMap renders an export for a person, on stderr.
func reportExportMap(result exportMapResult, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	for _, file := range result.Files {
		_, _ = fmt.Fprintf(stderr, "%s: %s (%s)\n", file.Path, file.Status, result.Schema)
	}

	// What it was drawn to, said once for the document. A person reading this
	// has the same question the machine contract answers above and no file to
	// read it out of; where a curve went unread there is no deviation to say,
	// and the warnings which say why are already on this stream.
	if result.Chord == nil {
		return
	}

	if result.Deviation == nil {
		_, _ = fmt.Fprintf(stderr, "drawn to %s, past %s\n",
			result.Chord.Name, plural(len(result.Chorded), "unread curve"))
		return
	}

	_, _ = fmt.Fprintf(stderr, "drawn to %s, within %s %s of the boundary\n",
		result.Chord.Name, figure(result.Deviation.Value), result.Deviation.Unit)
}

// drawnMap is the model as a vector layer holds it, and whatever stopped it
// being drawable.
//
// This function is where the two vocabularies meet, and it is on this side of
// the boundary on purpose. A region is this engine's word and a feature is
// GML's, so the table joining them is a fact about this project rather than
// about either package
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)) — and
// a writer which knew that a feature has a kind would be usable only by the one
// program whose kinds they are.
func drawnMap(graph *dfcad.Graph, drawn shapes, sited georeference) (gml.Collection, tessellated, []dfcad.Diagnostic) {
	// The georeference is settled before the walk because it is a fact about
	// the registry rather than about any region, and it is read by the same
	// code the IFC export reads it with: which frame may carry it, what shape
	// the identifier has and where it is written are properties of the model,
	// and two exports which answered those differently would disagree about
	// where one model is.
	out := &cartographer{graph: graph, shapes: drawn}

	placed, refused := georeferenced(graph.Registry(), graph.Frames(), sited)
	if len(refused) > 0 {
		// A georeference which cannot be written is the end of the document,
		// and nothing below is worth walking for. The curves nothing read are
		// still worth reading for: they are a fact about the model rather than
		// about the file, so an author whose registry is wrong and whose walls
		// are unread is told both at once rather than sent back twice.
		out.unread(out.drawable())

		return gml.Collection{}, out.tessellated, append(refused, out.diags...)
	}

	root, rooted := graph.Frames().Root()
	if rooted {
		out.root = root.ID
		out.unsited(root, placed)
	}

	return gml.Collection{
		ID:        mapID,
		Namespace: mapNamespace,
		Prefix:    mapPrefix,
		Type:      mapType,
		CRS:       placed.srsName(),
		Features:  out.features(rooted),
	}, out.tessellated, out.diags
}

// tessellated is what a walk which drew a layer has to say about the drawing
// itself rather than about any feature of it, and about the nodes it could not
// make features of.
//
// It is one value for the whole document because a layer is one artefact: a
// consumer opens the file, not the twelfth region of it, and the question it
// has is whether what it opened follows the model to the tolerance it asked
// for.
type tessellated struct {
	// chord is the declared tolerance the curves were drawn to. It is one
	// tolerance across the document rather than one per region, because a
	// tolerance declared in a unit other than the frame it is applied in
	// refuses the drawing.
	chord dfcad.Tolerance

	// deviation is the furthest any segment of any region falls from the curve
	// it stands in for, which is the worst the document is anywhere and is at
	// most chord.
	deviation float64

	// chorded is every edge of a drawn region which states a curve the run's
	// vocabulary did not read, each edge once.
	chorded []chordedEntry

	// undrawn is every node the walk selected and could not write as a
	// feature, in the order it met them, which is id order. It is here beside
	// the rest because it is the other half of the same account: the document
	// holds the features, and this holds everything the model shaped which the
	// document does not.
	undrawn []mapUndrawnEntry
}

// cartographer is one traversal of the graph into a vector layer's shape.
type cartographer struct {
	graph *dfcad.Graph

	// shapes is the vocabulary geometry is read under, all of which a run of
	// this command gave.
	shapes shapes

	// root is the frame every feature's coordinates are expressed in, which is
	// the frame the chain is rooted at and the one the coordinate reference
	// system describes.
	root dfcad.ID

	// tessellated is what the walk drew everything to, accumulated over every
	// region it drew and every curve it could not read.
	tessellated tessellated

	// diags is everything the traversal had to say about the model, in the
	// order it met it.
	//
	// An error is a region this cannot draw or cannot carry into the root
	// frame, and a run which collected one writes no file at all: an artefact
	// is all or nothing, and a layer with one plot quietly missing is worse
	// than none, because the missing plot looks like land nobody has claimed.
	diags []dfcad.Diagnostic
}

// unsited says, once, that the document will name no coordinate reference
// system.
//
// It is a warning rather than a refusal because a model nobody has sited is a
// model somebody is still working on, and a plan of it is still worth drawing.
// It is a warning rather than nothing because the file is the thing this
// command exists to prevent: a vector layer whose coordinates are in a system
// only its author knows, which the next person to open it will place by
// guessing.
func (c *cartographer) unsited(root dfcad.Frame, placed *recordedCRS) {
	if placed != nil {
		return
	}

	c.diags = append(c.diags, dfcad.Diagnostic{
		Severity: dfcad.SeverityWarning,
		Span:     root.Span,
		Message: fmt.Sprintf(
			"expected the coordinate reference system of %s to name the document's, found none: the features are "+
				"written in this frame's coordinates and nothing in the file says which system those are",
			root.ID),
		Hint: fmt.Sprintf(
			"write the identifier on this frame as a text predicate — (crs \"EPSG:6543\") — and name that predicate "+
				"with --%s", flagCRS),
	})
}

// drawable is every node the model gave a shape to, in id order.
//
// Everything the model gives a shape to is drawn, whatever its kind. A
// boundary is the model saying where a thing is, and a command which wrote the
// rooms and left out the plot they stand on would be answering a question
// about kinds which the model already answered by drawing both.
//
// A node drawn as a line is here because it has a boundary, and it is written
// as the run that boundary is rather than as the area it does not enclose.
//
// A node drawn as a point has a shape and no boundary. It is here for the same
// reason a bounded node is: the model says where it is, and a layer which held
// the rooms and not the panels in them would be a map of a floor with its
// electrical layer missing.
//
// The order is by id rather than by the order the files were read, so the
// document is a property of the model rather than of which file a node
// happened to be written in.
//
// It is its own method because it is asked twice for two reasons: once to draw
// the layer, and once to say which curves nothing read — and the second is
// asked even where the first cannot be.
func (c *cartographer) drawable() []*dfcad.SemanticNode {
	var nodes []*dfcad.SemanticNode

	for node := range c.graph.Nodes().All() {
		// A retired node is one which stopped existing, and drawing it as a
		// live feature is how a plan comes to show a building which was
		// demolished.
		if node.Retired() {
			continue
		}

		if geometry, _ := node.Geometry(); geometry != dfcad.GeometryPoint && len(node.Boundaries()) == 0 {
			continue
		}
		nodes = append(nodes, node)
	}

	slices.SortFunc(nodes, byNodeID)

	return nodes
}

// features is every node the model gives a shape to, drawn, in the order
// [drawable] reaches them.
//
// Every node [drawable] selected either comes back as a feature or is named in
// [tessellated.undrawn], and there is no third way out of this loop. That is
// the whole of the rule it keeps: a node which went missing from the document
// with nothing said about it is a map a reader trusts and is wrong about — the
// open runs of a model used to leave exactly that way, drawn as a region, found
// to cover nothing and skipped without a word.
func (c *cartographer) features(rooted bool) []gml.Feature {
	nodes := c.drawable()

	c.unread(nodes)

	out := make([]gml.Feature, 0, len(nodes))

	for _, node := range nodes {
		said := len(c.diags)

		feature, reason, drawn := c.feature(node, rooted, mapFeature+strconv.Itoa(len(out)+1))
		if !drawn {
			c.undrawn(node, reason, c.diags[said:])
			continue
		}

		out = append(out, feature)
	}

	return out
}

// feature is one node as a feature of the layer, or why it could not be one.
//
// Which of the three shapes it is written as is the node's declared geometry
// and never its kind, for the reason the IFC export decides it that way: a
// railing and a trench are both a run, and what the kind decides is only which
// value the feature's kind property holds.
func (c *cartographer) feature(node *dfcad.SemanticNode, rooted bool, id string) (gml.Feature, undrawnReason, bool) {
	if !rooted {
		c.unrooted(node)
		return gml.Feature{}, undrawnUnrooted, false
	}

	feature := gml.Feature{ID: id, Properties: c.properties(node)}

	switch geometry, _ := node.Geometry(); geometry {
	case dfcad.GeometryPoint:
		at, reason, placed := c.located(node)
		if !placed {
			return gml.Feature{}, reason, false
		}
		feature.Points = []gml.Position{at}

	case dfcad.GeometryLine:
		curves, reason, drawn := c.curves(node)
		if !drawn {
			return gml.Feature{}, reason, false
		}
		feature.Curves = curves

	default:
		surfaces, reason, drawn := c.surfaces(node)
		if !drawn {
			return gml.Feature{}, reason, false
		}
		feature.Surfaces = surfaces
	}

	return feature, "", true
}

// undrawn records a node [drawable] selected and the document will not hold.
//
// It is named in the answer whatever the reason, and it is an error whatever
// the reason. Most of the ways a node goes undrawn have already said so — the
// assembly of a ring which does not close, the chain of frames which does not
// reach — and for those nothing more is raised here, because the same fault
// reported twice is noise. Where nothing was said, this says it: a node which
// could be left out of a layer silently is a layer with a plot quietly missing,
// which looks exactly like land nobody has claimed.
func (c *cartographer) undrawn(node *dfcad.SemanticNode, reason undrawnReason, said []dfcad.Diagnostic) {
	c.tessellated.undrawn = append(c.tessellated.undrawn, mapUndrawnEntry{
		Node:   string(node.ID()),
		Label:  node.Label(),
		Kind:   string(node.Kind()),
		Type:   node.Type(),
		Reason: reason,
	})

	for _, diagnostic := range said {
		if diagnostic.Severity == dfcad.SeverityError {
			return
		}
	}

	geometry, _ := node.Geometry()

	c.diags = append(c.diags, dfcad.Diagnostic{
		Severity: dfcad.SeverityError,
		Span:     node.Span(),
		Message: fmt.Sprintf(
			"expected %s, whose geometry is %s, to be drawn as a feature of the map, found nothing to draw it from",
			node.ID(), geometry),
		Hint: "every node the model gives a shape to is written or named; this one was neither refused nor drawn, " +
			"and the document would otherwise leave it out without a word",
	})
}

// located is one node drawn as a point, carried into the root frame and
// written as the position a map holds it at.
//
// It reads the same claim the plan and the export read, through the same
// region, so a device is at one place on every artefact this repository writes
// rather than at one place per reader of the model.
//
// The third component goes no further than here, exactly as a ring's does: a
// map is a plan, and this format has two ordinates. What is dropped is dropped
// after the carry rather than before it, because a transform between frames
// mixes the components and a position flattened first would land somewhere the
// model does not put it.
func (c *cartographer) located(node *dfcad.SemanticNode) (gml.Position, undrawnReason, bool) {
	region, diags := c.graph.Topology().RegionOf(
		node,
		c.graph.Boundaries(),
		bent(c.graph, c.shapes.position, c.shapes.tolerance, c.shapes.curvature(), node),
	)
	c.diags = append(c.diags, diags...)

	if _, placed := region.Location(); !placed {
		return gml.Position{}, undrawnNoPosition, false
	}

	if region.Frame() != c.root {
		carried, refused := region.In(c.root, c.graph.Frames())
		if len(refused) > 0 {
			c.diags = append(c.diags, refused...)
			return gml.Position{}, undrawnUncarried, false
		}
		region = carried
	}

	at, _ := region.Location()

	return gml.Position{Easting: at[0], Northing: at[1]}, "", true
}

// unread records every curve the vocabulary this run named could not read,
// over every region the layer would hold.
//
// It is one survey across all of them rather than one per region, which is what
// makes an edge two rooms share reported once: the same edge met twice is the
// same finding, and a layer whose answer named it twice would read as two walls
// drawn straight rather than one.
//
// It runs whether or not the frames reach a root. A curve nothing read is a
// fact about the model, and a run refused for having nowhere to put its
// coordinates has that wrong with it too.
func (c *cartographer) unread(nodes []*dfcad.SemanticNode) {
	subjects := asEntities(nodes...)

	survey := bent(c.graph, c.shapes.position, c.shapes.tolerance, c.shapes.curvature(), subjects...)

	chorded, diags := chordedOf(c.graph, survey, subjects...)

	c.tessellated.chorded = chorded
	c.diags = append(c.diags, diags...)
}

// drawn is one node's boundary drawn to the run's chord tolerance and carried
// into the root frame, which is the part of writing a ring and writing a run
// that is the same arithmetic.
//
// A node whose boundary could not be read comes back with nothing and no
// diagnostic of its own: the assembly has already said what was wrong with it,
// and a second complaint about the shape it therefore does not have would be
// the same fault reported twice. What it does come back with is the reason,
// which is what names it in the answer.
//
// The curves drawn are folded into what the document was drawn to before the
// region is carried, and whether or not it can be. The tolerance is the same
// declared value for every region — one in a unit other than the frame's
// refuses the drawing — and the deviation is the worst of them, because the
// document is as close to the model as its furthest segment is and no closer.
func (c *cartographer) drawn(node *dfcad.SemanticNode, read func(dfcad.Region) bool) (dfcad.Region, undrawnReason, bool) {
	drawn, diags := c.graph.Topology().TessellateRegion(
		node,
		c.graph.Boundaries(),
		bent(c.graph, c.shapes.position, c.shapes.tolerance, c.shapes.curvature(), node),
		c.shapes.chord,
	)
	c.diags = append(c.diags, diags...)

	region := drawn.Region()
	if !read(region) {
		return dfcad.Region{}, undrawnUnreadable, false
	}

	if made := drawn.ChordTolerance(); made.Name != "" {
		c.tessellated.chord = made
		c.tessellated.deviation = max(c.tessellated.deviation, drawn.Deviation())
	}

	// Carried before it is checked for level, and not after. A transform
	// between frames is rigid up to a scale, so it preserves the plane a
	// region lies in but not which plane that is: a boundary lying flat on one
	// grid can arrive tilted on another, and it is the coordinates being
	// written which have to be a plan.
	if region.Frame() != c.root {
		carried, refused := region.In(c.root, c.graph.Frames())
		if len(refused) > 0 {
			c.diags = append(c.diags, refused...)
			return dfcad.Region{}, undrawnUncarried, false
		}
		region = carried
	}

	return region, "", true
}

// level refuses a boundary whose corners do not lie at one level in the root
// frame, and reports whether they do.
//
// It is asked of a run exactly as of a ring. A map is a plan whichever shape
// is on it, and a sloping run written with its elevation dropped would be a
// projection this command chose as surely as a tilted room would.
func (c *cartographer) level(node *dfcad.SemanticNode, lies levels) bool {
	if lies.level {
		return true
	}

	c.diags = append(c.diags, dfcad.Diagnostic{
		Severity: dfcad.SeverityError,
		Span:     node.Span(),
		Message: fmt.Sprintf(
			"expected the boundary of %s to lie at one level in %s to draw it in plan, found corners at %s and %s",
			node.ID(), c.root, figure(lies.elevation), figure(lies.offending)),
		Hint: "a map is a plan; a boundary which is not level has none, and the projection which would give it " +
			"one is not this command's to choose",
	})

	return false
}

// surfaces is one node's outline, drawn, carried into the root frame and taken
// apart into polygons.
func (c *cartographer) surfaces(node *dfcad.SemanticNode) ([]gml.Polygon, undrawnReason, bool) {
	region, reason, drawn := c.drawn(node, func(region dfcad.Region) bool {
		return len(region.Pieces()) > 0
	})
	if !drawn {
		return nil, reason, false
	}

	pieces := region.Pieces()

	if !c.level(node, levelOf(pieces, region.Tolerance().Value)) {
		return nil, undrawnUnlevel, false
	}

	out := make([]gml.Polygon, 0, len(pieces))
	for _, piece := range pieces {
		out = append(out, surface(piece))
	}

	return out, "", true
}

// curves is one node drawn as a line: the runs its loops walk, drawn, carried
// into the root frame and written one line string per loop.
//
// A run covers nothing, so a region read from one has no pieces — and that is
// the shape and not a failure of it. What it has is its boundary, run by run in
// the order each loop walks it, and that is what the document writes. It used
// to be read the way a ring is, found to cover nothing and skipped with nothing
// said, which is how a map of a septic field came out holding the parcel and
// none of the trenches.
//
// One line string per loop rather than one per edge, because a loop is the run
// the model states: a partition turning a corner is one wall, and a layer which
// wrote it as two features' worth of pieces would be a join the reader has to
// make back.
func (c *cartographer) curves(node *dfcad.SemanticNode) ([]gml.LineString, undrawnReason, bool) {
	region, reason, drawn := c.drawn(node, func(region dfcad.Region) bool {
		return len(region.Segments()) > 0
	})
	if !drawn {
		return nil, reason, false
	}

	runs := walked(region.Segments())

	if !c.level(node, levelOfRings(runs, region.Tolerance().Value)) {
		return nil, undrawnUnlevel, false
	}

	out := make([]gml.LineString, 0, len(runs))
	for _, run := range runs {
		out = append(out, opened(run))
	}

	return out, "", true
}

// walked is the boundary of a run as the corners of each loop in the order it
// was walked: where the first straight run of the loop leaves from, and where
// every run of it arrives.
//
// The segments of one loop meet end to end, because the assembly walked them
// that way — which is what lets a chain be read back as its corners rather than
// as a heap of pairs.
func walked(segments []dfcad.BoundarySegment) [][]dfcad.Point {
	var out [][]dfcad.Point

	ring := -1
	for _, segment := range segments {
		if len(out) == 0 || segment.Ring() != ring {
			ring = segment.Ring()
			out = append(out, []dfcad.Point{segment.From()})
		}

		last := len(out) - 1
		out[last] = append(out[last], segment.To())
	}

	return out
}

// unrooted refuses a region in a model whose frames reach no root.
//
// It is per region rather than once for the model because it is the regions
// which cannot be written: a model declaring no frame at all holds no region
// either, and one whose chain is broken is reported by the loader as the
// broken chain it is.
func (c *cartographer) unrooted(node *dfcad.SemanticNode) {
	c.diags = append(c.diags, dfcad.Diagnostic{
		Severity: dfcad.SeverityError,
		Span:     node.Span(),
		Message: fmt.Sprintf(
			"expected the frames of this model to reach one root to express the boundary of %s in, found none",
			node.ID()),
		Hint: "every feature is written in the coordinates of the frame the chain is rooted at, which is the frame a " +
			"coordinate reference system describes; a model with no root has no such coordinates",
	})
}

// properties is one node as the layer's columns hold it.
func (c *cartographer) properties(node *dfcad.SemanticNode) []gml.Property {
	properties := []gml.Property{
		{Name: propertyNodeID, Value: string(node.ID())},
	}

	written := func(name, value string) {
		if value == "" {
			return
		}
		properties = append(properties, gml.Property{Name: name, Value: value})
	}

	written(propertyLabel, node.Label())
	written(propertyKind, string(node.Kind()))
	written(propertyType, node.Type())

	if within, held := node.Within(); held {
		written(propertyWithin, string(within))
	}

	// The frame the outline was declared in, which is not the frame the
	// coordinates are in unless the two are the same one. It is here because a
	// feature which arrived through a measured transform is a feature carrying
	// that transform's accuracy, and the first thing anybody asking about that
	// needs is which chain to go and read.
	if frame, held := node.Frame(); held {
		written(propertyFrame, string(frame))
	}

	return properties
}

// surface is one piece of a region as a polygon of this format.
func surface(piece dfcad.Piece) gml.Polygon {
	out := gml.Polygon{Exterior: closed(piece.Outer())}

	// The holes are the region's own, which is what carries the even-odd
	// nesting it worked out through to the file rather than leaving a reader
	// to work it out again from a heap of rings — or, worse, to draw the
	// courtyard as part of the block around it.
	for _, hole := range piece.Holes() {
		out.Interior = append(out.Interior, closed(hole))
	}

	return out
}

// closed is one ring of a region as GML wants it.
//
// Two things happen here and both are the format's rather than the model's.
// The ring loses its third coordinate, because a map is a plan. And the first
// position is repeated as the last, because a region's rings are closed by
// being rings and GML's are closed by saying so.
func closed(ring []dfcad.Point) gml.LinearRing {
	if len(ring) == 0 {
		return gml.LinearRing{}
	}

	positions := make([]gml.Position, 0, len(ring)+1)
	for _, at := range ring {
		positions = append(positions, gml.Position{Easting: at[0], Northing: at[1]})
	}

	return gml.LinearRing{Positions: append(positions, positions[0])}
}

// opened is one run of a line as GML wants it: the third coordinate dropped,
// because a map is a plan, and nothing repeated, because a run is not closed by
// being one and saying so would make it a ring.
func opened(run []dfcad.Point) gml.LineString {
	positions := make([]gml.Position, 0, len(run))
	for _, at := range run {
		positions = append(positions, gml.Position{Easting: at[0], Northing: at[1]})
	}

	return gml.LineString{Positions: positions}
}
