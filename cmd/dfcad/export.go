// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/z5labs/dfcad"
	"github.com/z5labs/dfcad/ifc"
)

const exportUsage = `dfcad export — write the model's spatial structure as an IFC4 file.

Usage:

	dfcad export [flags]

The project, the sites, the buildings, the storeys, the spaces and the zones
this model holds, written as an ISO 10303-21 exchange file in the IFC4 schema,
with the aggregation relationships between them, a local placement for each and
the identifiers this project derives.

The artefact is a build output and never lands in the authored tree. By default
it is written beneath .dfcad/export, in a directory named for the digest of the
tree it was derived from, so a run against a new revision writes a new
directory rather than replacing anything and the whole of .dfcad may be deleted
at any time. --out names somewhere else, and somewhere else has to be outside
the model root: an IFC file beside the entity files is invisible to every check
this engine runs while looking exactly as authoritative as the coordinates it
may have stopped agreeing with.

The same tree exports to the same bytes, on any machine and at any time.
Nothing here reads a clock — the header's time stamp is the derivation epoch,
1970-01-01T00:00:00, and IfcOwnerHistory, which is where a creation time would
otherwise be mandatory, is left out throughout. So re-exporting a tree nothing
happened to writes nothing and reports the file it found as "unchanged".

What a node is written as comes from its kind, which is IFC's spatial
decomposition one for one. A node whose kind has no spatial entity — an element
or an interface — is written as the entity its type declares a classification
under in the "IFC4" system, and as an IfcBuildingElementProxy naming its type in
ObjectType where the type declares none, which is what that entity is for.

The entities a classification may name are:

	IfcBeam                  IfcFooting            IfcRoof
	IfcBuildingElementProxy  IfcFurnishingElement  IfcSlab
	IfcColumn                IfcMember             IfcStair
	IfcCovering              IfcPlate              IfcWall
	IfcCurtainWall           IfcRailing            IfcWindow
	IfcDoor                  IfcRamp

That set is what a registry is authored against. A classification naming
anything else still exports — the node reaches the file as an
IfcBuildingElementProxy naming its type, which is a complete statement of what
the model holds — and it is reported rather than left to be found by opening
the file, in the answer's "classifications" and as a warning per type on
stderr. The two mistakes are told apart, because their fixes differ: a code
IFC4 defines as a product and this export has no attribute list for is
"unwritten", and the proxy stands in faithfully until the entity is added; a
code IFC4 defines no product for — a misspelling, a relationship, a type
object — is "unknown", and the proxy is standing in for nothing anybody meant.

A storey declaring a frame is written at the elevation that frame's chain to the
root puts it at, and everything in it is placed relative to that. It is what
makes a building authored a level at a time — every level drawn in its own plan
frame, so every corner of it at nought — come out as levels stacked rather than
as levels interpenetrating. A storey declaring no frame is written at the
building's datum with no elevation stated at all, which is what a level nobody
has related to the building has. A storey whose frame does not reach the root is
refused naming the frame: an export which cannot say where a level sits writes
no file, rather than a file with every level flat on the ground.

Every coordinate in the file is written in the coordinates of the frame the
chain is rooted at. A shape authored on any other frame — a room drawn at
nought on the plan grid of its level, a wall set out on a fabrication grid — is
carried there first, by the chain of measured transforms the model states and
by nothing else: this is a similarity transform in the plane the survey was
already projected into, and there is no reprojection anywhere. A shape whose
frame does not reach the root is refused naming that frame. What the placements
above a shape already stand at is taken back off it, so a coordinate in the file
composed with the placements above it is the coordinate the model states rather
than the same lift applied twice.

Flags:

	--out <path>               where to write the file (default: beneath
	                           .dfcad/export, in a directory named for the
	                           digest of the source tree)
	--evidence                 add the identifier manifest: every node and the
	                           GlobalId derived for it
	--position <predicate>     the predicate a corner's position is claimed
	                           under, which a space's outline is read from and
	                           which a node drawn as a point is placed by
	--tolerance <name>         the tolerance corners are judged coincident
	                           against and rings judged planar against
	--chord <name>             the tolerance a segment standing in for a curve
	                           may fall from it by
	--arc-centre <predicate>   the predicate a curved edge's centre is claimed
	                           under
	--arc-through <predicate>  the predicate the point a curved edge passes
	                           through is claimed under
	--height <predicate>       the predicate a node's height is claimed under,
	                           which is what a body is swept through
	--thickness <predicate>    the predicate the thickness of a node drawn as a
	                           line is claimed under, which is what widens its
	                           run into a solid
	--crs <predicate>          the predicate the identifier of the project's
	                           coordinate reference system is written under
	--crs-definition <predicate>
	                           the predicate its full definition is written
	                           under, where the project holds one

The manifest is asked for rather than sent by default because it grows one
entry per node on a call whose answer is four fields, and because every entry
of it is recomputable exactly, by anybody holding the model, from the node's id
and the URL the project pins.

The first three geometry flags go together or not at all. A run which names
none exports the spatial structure and the identifiers and no shape, which is
a correct IFC file and is what this command did before it could draw anything.
A run which names all three draws everything it can: a node bounded by rings
carries a FootPrint representation built from them, holes and all, drawn to the
chord tolerance named so a curved wall reaches the file as the curve it is
rather than as a straight line nobody asked for.

What is drawn is decided by a node's boundary and its declared geometry, never
by its kind. A room and a countertop are both an area with a height over it,
and the sweep which makes a solid of either is the same operation — so an
element bounded by a ring is drawn exactly as a space is, and what its kind
decides is only which entity the shape is written on.

A node whose declared geometry is "point" is placed rather than drawn. Its
product stands at the position claimed of the node itself under --position,
carried into the root frame like every other coordinate here and written as its
local placement, and it carries no representation: a panel and a receptacle
have a position and no extent, and a rectangle invented for one would be
dimensions nobody measured. A node drawn as a point which nothing places is
refused naming it, rather than written at its container's origin — a device at
the corner of its storey looks exactly like a device somebody placed.

--height is what adds a body. Where it names a predicate and a node's height
resolves under it, the node additionally carries a SweptSolid representation —
the footprint extruded upwards, holes carried through as the profile's inner
curves — and the two live in one shape definition. The footprint is what the
model states and the body is a convenience built from a claim, which is why
they are two representations rather than one: a reader wanting what the model
says takes the FootPrint and never has to guess which of the two that was.

--thickness is the same for a node drawn as a line. A partition, a railing and
a duct run are each authored as a centreline, because that is what they are —
one run of the model, shared by whatever stands either side of it — and each is
built as a solid, so the thickness claimed of the node is what turns the one
into the other. Each straight segment of the run is widened by it, half either
side, and swept upwards through the height: a rectangle per segment rather than
one outline mitred around the whole run, because the joint where two segments
meet is a detail the model does not state and is not this command's to invent.

Neither has a default and there never will be one. Which predicate carries a
room's height or a wall's thickness is project vocabulary, and a run which
names none exports footprints rather than failing — a two dimensional export is
correct, and it is the one an author who has drawn plans and measured nothing
should get. A node nothing claims a height of is exported the same way, and one
drawn as a line with no thickness claimed carries no shape at all: a centreline
of no width is not a solid, and IFC has nowhere to put one. A height or a
thickness which resolves to nought or less is refused naming the claim, because
a solid is bounded by positive length measures.

A node somebody claimed a height of whose type is classified as an IFC entity
which cannot carry a shape — a relationship, a spatial element — is refused
naming that claim and that entity. Such a node is still written as a proxy when
nothing was claimed of it, which is what that entity is for; what it is not is a
place to put a body the model asked for.

--crs is what puts the project on the earth, and it has no default either. It
names the predicate the root frame carries the identifier of its projected
coordinate reference system under — a non-claim-bearing text predicate, written
(crs "EPSG:6543") — and the file then carries an IfcProjectedCRS naming it and
an IfcMapConversion into it. Naming no predicate exports without a georeference,
which is a correct file and is the one a model nobody has sited should get.

The identifier is recorded and never interpreted: it is checked for shape only,
an authority and a code, and nothing here resolves it, converts it or looks it
up. --crs-definition names the predicate carrying the register's own definition
of that system where the project holds one, and it is copied byte for byte. Its
linear unit token is checked against the unit the frame declares — the token
and never the factor beside it, because several correct spellings of one factor
differ in their last digits.

The map conversion states where the file's coordinates sit in that system, and
comes out as the identity: the root frame is the projected system the chain is
rooted at and every coordinate in the file has been carried into the root
frame, so there is nothing left between them to state. It is the identity
because those two facts hold and not because it is written that way — an export
which assumed it wrote an identity over coordinates nothing had carried, which
put every room of a levelled model at the system's origin. No rotation and no
scale is written whatever the frames say, because one would state a fit nobody
measured. A coordinate reference system written on any frame but the root is
refused — every other frame reaches the root through a measured transform, and a
second georeference beside it would be a second answer nothing reconciles.

The IfcProjectedCRS carries Name and Description and nothing else. GeodeticDatum,
VerticalDatum, MapProjection and MapZone are written absent because this model
holds two strings about a coordinate reference system — an entry in somebody
else's register, and that register's own text about it — and a datum or a
projection filled in from the identifier would be this command interpreting a
code it is careful never to interpret. MapUnit is absent because the file's unit
assignment already states it, and two places to state one unit is one place for
them to disagree.

Every IfcAxis2Placement3D in the file is axis-aligned, whatever the frames say.
Rotation between frames is applied to the coordinates as they are carried into
the root frame rather than written into a placement, so a consumer reading
placements alone sees an unrotated model and one reading coordinates sees the
model the frames describe. The coordinates are the statement; a placement here
says only where a thing stands.

The units are the frames' own, and the frames have to agree on one: a file
states one set of units, and a model whose frames declare two is refused rather
than exported in whichever was found first. A model authored in feet is written
in feet. Each foot is an IfcConversionBasedUnit stating its factor over the
metre — the international foot and the US survey foot under names which tell
them apart, because they differ by two parts per million and a reader keying
off the name rather than the factor would otherwise read one as the other.
Nothing is converted: a coordinate written in the source is that coordinate in
the file.

Where a body is written, each claim behind it goes into the file beside it as a
property set: the predicate, the value, the source, the method, the accuracy,
the date and which step of the resolution rule chose it. A height and a
thickness are two sets rather than one, because they are two measurements — a
wall's height may be surveyed and its thickness taken off a drawing. That is
what lets whoever opens the file tell a surveyed figure from an assumed one
without holding the model it came from.

A space's boundaries reach the file as relationships, drawn or not. Every edge
of a room's outline which names the element realising it is written as an
IfcRelSpaceBoundary between the two, classified physical because something
backs it and internal or external from the containment the model already
states: an element in the same building as the room is between it and another
room, and one anywhere else is between it and the outside. Where the run drew
the room, each relationship also carries the run of the outline that edge
produced as its connection geometry — the same drawing the footprint holds,
curves included — and where it drew nothing, the relationship is written
without one, which the schema allows and a topological model should prefer.

Two rooms with nothing built between them are reported rather than written.
The relationship's element is mandatory, IFC's answer to a boundary with no
element is one which is not there, and inventing it would put a thing into the
artefact which the model does not hold. The same is said of an edge backed by
an element outside the spatial structure, which is written nowhere for a
relationship to point at. Both are warnings: the file is still produced, and a
gap somebody is told about is one they can close.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object export writes carries "derived", the "digest" of the source tree the
artefact was derived from, the "schema" it was written in, and "files": one
entry per file the artefact consists of, each with the "path" it is at and a
"status" of "written" or "unchanged".

There is no --dry-run. What this command writes is disposable, ignored by git
and reproducible, so there is nothing for a dry run to protect and no diff for
it to show.

Exit code 1 is a model no artefact could be made of — one which pins no URL to
derive identifiers from, one whose frames disagree about the linear unit, or a
node whose shape was asked for and could not be drawn: a ring which does not
close, a corner nothing states the position of, a boundary which does not lie
at one level, a height or a thickness which is not a distance or is not
positive, a body claimed of something no entity here can carry one on. The object
still comes back, with "derived" false and no files, so a caller reads
why from the diagnostics on stderr rather than from an empty stream. Exit code
3 is a destination inside the authored tree, which is refused before anything
is read.
`

// The flags export takes beyond the global ones.
const (
	flagOut      = "out"
	flagEvidence = "evidence"
)

// exportSchema is the schema the artefact is written in, as the result reports
// it. It is [ifc.Schema] rather than a second spelling of it, because a result
// which named a schema the file does not declare would be worse than one which
// named none.
const exportSchema = ifc.Schema

// exportFile is the name of the one file an IFC export consists of.
const exportFile = "model.ifc"

// statusWritten is the status of a file this run wrote.
//
// The other status an artefact's file can carry is [statusUnchanged], which
// `fmt` already declares with the meaning this needs: the file on disk is
// already what this run would have written. There is no third one — an
// artefact is all or nothing, so there is no half-written export to describe.
const statusWritten = "written"

// classificationSystem is the scheme whose codes name IFC entities.
//
// It is the schema token, which is what a registry writing
// `(classification "IFC4" "IfcWall")` means by it. A model classifying its
// types under another scheme as well — Uniclass, OmniClass — is the ordinary
// case, and nothing here reads those: they name the thing in somebody else's
// vocabulary rather than naming an entity in this file's.
const classificationSystem = ifc.Schema

// DestinationInsideModelError is a destination which would put the artefact in
// the authored tree.
//
// It is a usage error rather than a diagnostic: it is a fact about the
// invocation rather than about the model, and it is decidable before a byte of
// the model is read. The artefact which must not exist is not written and then
// complained about.
type DestinationInsideModelError struct {
	// Path is the destination, as it resolves.
	Path string

	// Root is the model root it resolves inside.
	Root string
}

// Error implements [error].
func (e DestinationInsideModelError) Error() string {
	return fmt.Sprintf(
		"expected a destination outside the model root %s or beneath %s, found %s: an export inside the authored tree is "+
			"read by nothing and reviewed by nobody, and looks exactly as authoritative as the model it may have stopped "+
			"agreeing with",
		e.Root, dfcad.BuildDir, e.Path)
}

// exportResult is the object export writes to stdout.
//
// It is the artefact-command shape
// ([0022](docs/decisions/0022-a-command-whose-product-is-a-file-answers-on-stdout.md)):
// the account of a file rather than the file, which cannot be the answer
// because stdout is one JSON object for every command in this interface.
type exportResult struct {
	envelope

	// Derived reports whether an artefact was produced. It is written
	// whatever the outcome, with the meaning it has on measure, buildable and
	// site: a model no artefact could be made of reads as false, and a model
	// which held nothing this format carries reads as true with no files.
	Derived bool `json:"derived"`

	// Digest is the digest of the source tree the artefact was derived from,
	// which is what lets a caller say whether the artefact in front of them is
	// the one this tree produces. It is written on a refusal too.
	Digest string `json:"digest,omitempty"`

	// Schema is the schema the artefact was written in. It is a field of this
	// payload rather than of the shape every artefact command shares, because
	// what a format calls its version is that format's business.
	Schema string `json:"schema,omitempty"`

	// Files is one entry per file the artefact consists of, ascending by
	// path. It describes files which are on disk and never anything else, and
	// it is empty rather than absent when there are none.
	Files []exportedFile `json:"files"`

	// Classifications is one entry per node whose type declared an IFC4
	// classification this writer could not carry, ascending by node id, and is
	// empty rather than absent when there are none.
	//
	// It is part of the answer rather than evidence asked for under --evidence
	// ([0017](docs/decisions/0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md)),
	// because it is not recomputable by the caller: what this writer can carry
	// is a fact about this writer, and a proxy in the file looks exactly like a
	// thing nobody classified. A run which reported it only on stderr would
	// leave every machine consumer of this command unable to tell a door from a
	// duct.
	Classifications []exportedClassification `json:"classifications"`

	// Identifiers is the manifest, written only under --evidence.
	Identifiers []exportedIdentifier `json:"identifiers,omitempty"`
}

// exportedFile is one file of the artefact.
type exportedFile struct {
	// Path is where the file is, exactly as it would be opened.
	Path string `json:"path"`

	// Status is [statusWritten] or [statusUnchanged].
	Status string `json:"status"`
}

// exportedIdentifier is one line of the manifest: a node and the identifier
// derived for it.
type exportedIdentifier struct {
	ID       string `json:"id"`
	GlobalID string `json:"global-id"`
}

// The reasons a classification did not reach the file as the entity it named.
//
// They are two rather than one because they are two mistakes with two fixes.
// A code this writer has no attribute list for is a gap here: the model said
// what the thing is, the proxy stands in for it faithfully, and the fix is a
// line in the writer's table. A code naming no IFC4 product is a mistake in the
// registry: a misspelling, or a code naming something a product cannot be, and
// the proxy stands in for nothing anybody meant.
const (
	// classificationUnwritten is an IFC4 product entity this writer does not
	// write.
	classificationUnwritten = "unwritten"

	// classificationUnknown is a code IFC4 defines no product entity for.
	classificationUnknown = "unknown"
)

// exportedClassification is one node whose type's IFC4 classification this
// writer could not carry, and which therefore reached the file as an
// IfcBuildingElementProxy.
//
// A type declaring no IFC4 classification at all is not one of these. That is
// the case the proxy is specified for — an element which no more specific
// entity covers, named in ObjectType — and reporting it would bury the codes
// which are actually wrong under every node nobody has classified yet.
type exportedClassification struct {
	// ID is the node written as a proxy.
	ID string `json:"id"`

	// Type is the type it is declared as, which is what carries the
	// classification and is what would be edited to fix it.
	Type string `json:"type"`

	// Code is the classification the type declares under the "IFC4" system,
	// exactly as the registry spells it. It is the registry's spelling rather
	// than the upper-cased one this writer compares, because it is what a
	// person would search the registry for.
	Code string `json:"code"`

	// Entity is what the node was written as instead, which is always
	// IFCBUILDINGELEMENTPROXY today and is stated rather than assumed.
	Entity string `json:"entity"`

	// Reason is [classificationUnwritten] or [classificationUnknown].
	Reason string `json:"reason"`
}

// runExport is the export command.
func runExport(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	out := flags.String(flagOut, "", "")
	evidencing := flags.Bool(flagEvidence, false, "")

	position := flags.String(flagPosition, "", "")
	tolerance := flags.String(flagTolerance, "", "")
	chord := flags.String(flagChord, "", "")
	centre := flags.String(flagArcCentre, "", "")
	through := flags.String(flagArcThrough, "", "")
	height := flags.String(flagHeight, "", "")
	thickness := flags.String(flagThickness, "", "")

	crs := flags.String(flagCRS, "", "")
	crsDefinition := flags.String(flagCRSDefinition, "", "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(arguments) > 0 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments}, stderr, true)
	}

	drawn := shapes{
		position:   *position,
		tolerance:  *tolerance,
		chord:      *chord,
		arcCentre:  *centre,
		arcThrough: *through,
		height:     *height,
		thickness:  *thickness,
	}

	if err := shapeVocabularyOf(drawn); err != nil {
		return usageError(cmd, err, stderr, true)
	}

	if err := arcVocabularyOf(*centre, *through); err != nil {
		return usageError(cmd, err, stderr, true)
	}

	sited := georeference{identifier: *crs, definition: *crsDefinition}

	if err := crsVocabularyOf(sited); err != nil {
		return usageError(cmd, err, stderr, true)
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

	graph, refused := loadGate(cmd, globals, stderr)
	if refused {
		return exitLoad
	}

	result := exportResult{
		envelope:        newEnvelope(cmd.name),
		Files:           []exportedFile{},
		Classifications: []exportedClassification{},
	}

	digest, keyed := graph.Digest()
	if keyed {
		result.Digest = digest.String()
	}

	model, manifest, classifications, diags := exported(graph, dfcad.DerivationEpoch(digest), drawn, sited)

	// Reported whatever the outcome, refusal included: which classifications
	// this writer could not carry is a fact about the model rather than about
	// the artefact, and a run which refused for some other reason has still
	// found it out.
	if len(classifications) > 0 {
		result.Classifications = classifications
	}

	if destination == "" {
		destination = filepath.Join(dfcad.ExportDir(globals.Root), digest.String(), exportFile)
	}

	if render(diags, stderr) {
		if err := emit(stdout, result); err != nil {
			_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
			return exitLoad
		}
		return exitCheck
	}

	var written bytes.Buffer
	if err := ifc.Write(&written, model); err != nil {
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
	result.Schema = exportSchema
	result.Files = append(result.Files, exportedFile{Path: destination, Status: status})

	if *evidencing {
		result.Identifiers = manifest
	}

	reportExport(result, globals, stderr)

	if err := emit(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// exportDestination is the path --out names, checked against the one rule
// there is about where an artefact may land.
//
// The check is on the resolved absolute path, so a relative path which walks
// back in through `..` is refused too. The build directory is the exception to
// the root's refusal because it is the one place under the root which the
// authored tree does not include.
func exportDestination(globals *globals, out string) (string, error) {
	destination := globals.resolve(out)

	absolute, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}

	root, err := filepath.Abs(globals.Root)
	if err != nil {
		return "", err
	}

	inside, err := filepath.Rel(root, absolute)
	if err != nil {
		// Two paths with no relation — different volumes on Windows — cannot
		// have one inside the other, which is the only thing being asked.
		return destination, nil
	}

	if inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return destination, nil
	}

	if inside == dfcad.BuildDir || strings.HasPrefix(inside, dfcad.BuildDir+string(filepath.Separator)) {
		return destination, nil
	}

	return "", DestinationInsideModelError{Path: destination, Root: globals.Root}
}

// place writes the artefact where it belongs, and says whether it had to.
//
// A run which finds the same bytes already there leaves them alone. That is
// the cache-hit property the digest key buys, made visible: a build script
// reading "unchanged" knows the artefact it holds is the artefact this tree
// produces, and it is a statement about bytes rather than a guess from a
// modification time.
//
// The write is through a temporary file in the destination's own directory,
// renamed into place. An artefact is all or nothing, and a half-written file
// under the digest of a tree is exactly the thing a later run would read as
// that tree's export.
func place(destination string, artefact []byte) (string, error) {
	if held, err := os.ReadFile(destination); err == nil && bytes.Equal(held, artefact) {
		return statusUnchanged, nil
	}

	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}

	temporary, err := os.CreateTemp(directory, "."+filepath.Base(destination)+".*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(temporary.Name()) }()

	if _, err := temporary.Write(artefact); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}

	if err := os.Chmod(temporary.Name(), 0o644); err != nil {
		return "", err
	}

	if err := os.Rename(temporary.Name(), destination); err != nil {
		return "", err
	}

	return statusWritten, nil
}

// reportExport renders an export for a person, on stderr.
//
// Nothing here reaches stdout, in any format and at any verbosity: stdout is
// the same bytes whether or not anybody asked to read the run.
func reportExport(result exportResult, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	for _, file := range result.Files {
		_, _ = fmt.Fprintf(stderr, "%s: %s (%s)\n", file.Path, file.Status, result.Schema)
	}
}

// exported is the model as IFC holds it, the manifest of the identifiers it
// carries, the classifications it could not carry, and whatever stopped any of
// them being derivable.
//
// This function is where the two vocabularies meet, and it is on this side of
// the boundary on purpose. A kind is this engine's word and an IfcSpace is
// IFC's, so the table joining them is a fact about this project rather than
// about either package
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)) —
// and a serialiser which knew it would be usable only by the one program whose
// opinion it held.
func exported(
	graph *dfcad.Graph,
	epoch dfcad.Epoch,
	drawn shapes,
	sited georeference,
) (ifc.Model, []exportedIdentifier, []exportedClassification, []dfcad.Diagnostic) {
	registry := graph.Registry()

	project, held := registry.Project()
	if !held || project.GlobalIDNamespace == "" {
		return ifc.Model{}, nil, nil, []dfcad.Diagnostic{{
			Severity: dfcad.SeverityError,
			Span:     project.Span,
			Message: "expected a project declaration pinning the URL identifiers derive from, found none: every object in " +
				"an IFC file carries a GlobalId, and there is nothing to derive one from",
			Hint: "write (globalid-namespace \"https://example.org/models/name\") on the project declaration",
		}}
	}

	units, diagnostic := exportedUnits(registry)
	if diagnostic != nil {
		return ifc.Model{}, nil, nil, []dfcad.Diagnostic{*diagnostic}
	}

	// The georeference is settled before the walk because it is a fact about
	// the registry rather than about any node, and a model which cannot say
	// where it sits is refused before an artefact is built out of it.
	placed, refused := georeferenced(registry, graph.Frames(), sited)
	if len(refused) > 0 {
		return ifc.Model{}, nil, nil, refused
	}

	// The frame every coordinate in the file is written in, settled once
	// before the walk. A model whose frames reach no root has no such frame,
	// and each shape it was going to draw is refused naming the frame it was
	// drawn on
	// ([0024](docs/decisions/0024-every-coordinate-in-an-export-is-written-in-the-root-frame.md)).
	root, rooted := graph.Frames().Root()

	out := &exporter{
		graph:    graph,
		registry: registry,
		url:      project.GlobalIDNamespace,
		shapes:   drawn,
		root:     root.ID,
		rooted:   rooted,
		derived:  make(map[dfcad.ID]ifc.GlobalID),
		written:  make(map[dfcad.ID]bool),
	}
	out.collect()

	// The two are built in statements rather than in the literal below because
	// the second reads what the first recorded: a group assigns objects this
	// file holds, and which those are is not known until they are held.
	//
	// The walk starts at the root frame's origin, which is where the file's own
	// coordinate space sits: every placement below it is written relative to
	// the one above, so what the walk carries down is how far the placements so
	// far have already moved a shape.
	sites := out.decompose(out.roots, 0)
	groups := out.zones()

	model := ifc.Model{
		Header: ifc.Header{
			Description: []string{"ViewDefinition [CoordinationView]"},
			Name:        exportFile,
			// Part 21 makes this field mandatory, so it is written, and it is
			// the derivation epoch rather than a clock reading: a time stamp
			// taken from the run is the one line which would stop two exports
			// of an unchanged tree being the same bytes
			// ([0021](docs/decisions/0021-an-export-is-a-build-output-keyed-by-its-source-digest.md)).
			TimeStamp:    epoch.STEP(),
			Author:       []string{""},
			Organisation: []string{""},
			// Neither carries a version. A build number here would change the
			// artefact of an unchanged model every time the tool was rebuilt.
			Preprocessor: "dfcad",
			Originating:  "dfcad",
		},
		Units: units,
		Context: ifc.RepresentationContext{
			Type:        contextType,
			Dimension:   3,
			World:       ifc.Placement{},
			Subcontexts: drawn.subcontexts(),
		},
		Georeference: placed.projected(out.sited(placed)),
		Project: ifc.Project{
			GlobalID:   out.identify(dfcad.ID("ifc/project")),
			Name:       project.Label,
			LongName:   project.Description,
			Aggregates: out.identify(dfcad.ID("ifc/aggregates/project")),
			Sites:      sites,
			Groups:     groups,
		},
	}

	return model, out.identifiers(), out.classifications(), out.diags
}

// exporter is one traversal of the graph into IFC's shape.
type exporter struct {
	graph    *dfcad.Graph
	registry *dfcad.Registry
	url      string

	// shapes is the vocabulary geometry is read under, and is empty for a run
	// which asked for none.
	shapes shapes

	// root is the frame the chain is rooted at, which is the frame every
	// coordinate in the file is written in, and rooted says whether the model
	// has one at all.
	//
	// It is one field rather than a lookup per shape because it is a fact
	// about the model: a walk which asked the frames again per node would be
	// asking a settled question, and a model with no root would answer it once
	// per shape.
	root   dfcad.ID
	rooted bool

	// diags is everything the traversal had to say about the model, in the
	// order it met it.
	//
	// An error is something which stopped a shape being drawn, and an export
	// which collected one writes no file at all: an artefact is all or
	// nothing, and a model file with one room's solid quietly missing is worse
	// than none.
	//
	// A warning is something this format cannot carry, which the file is
	// written without. A boundary with no element between the two sides is the
	// one there is: leaving it out is the only thing IFC allows, and saying so
	// is what makes the gap a stated one rather than a difference somebody
	// finds by counting walls in the receiving system.
	diags []dfcad.Diagnostic

	// roots are the spatial nodes nothing spatial contains, which hang off the
	// project.
	roots []*dfcad.SemanticNode

	// children are the spatial nodes beneath each spatial node, and products
	// the elements contained in each, both by the id of what holds them.
	children map[dfcad.ID][]*dfcad.SemanticNode
	products map[dfcad.ID][]*dfcad.SemanticNode

	// zoned are the zone nodes, and members the nodes assigned to each.
	zoned   []*dfcad.SemanticNode
	members map[dfcad.ID][]*dfcad.SemanticNode

	// proxied is every node whose type declared an IFC4 classification this
	// writer could not carry, by the id of the node.
	//
	// It is a map keyed by id rather than a list appended to, because the walk
	// may reach one node from more than one place and the answer is one entry
	// per node either way. [exporter.classifications] is what puts it in id
	// order, so the answer does not depend on the order the walk happened to
	// take.
	proxied map[dfcad.ID]exportedClassification

	// reclassify is the types already warned about, so that a model with a
	// hundred doors of one type is told once about the type rather than a
	// hundred times about the same line of the registry.
	reclassify map[string]bool

	// derived is every identifier derived so far, by the name it was derived
	// from. It is what makes deriving one twice cost nothing and report once.
	derived map[dfcad.ID]ifc.GlobalID

	// written is the ids of the nodes which reach the file, which is what a
	// zone's members and a space's boundaries are resolved against.
	//
	// It is settled by [exporter.collect], before anything is written, rather
	// than filled in as the walk reaches each node. A boundary names the
	// element which realises it and that element stands wherever the model put
	// it — which may be a storey the walk has not come to yet — so a set which
	// grew as the walk went would report a wall in the next storey as absent
	// from a file it is in.
	written map[dfcad.ID]bool
}

// collect sorts the model's nodes into the shape IFC decomposes.
//
// Everything is walked in id order rather than in the order the files were
// read, so the numbering of the emitted file is a property of the model rather
// than of which file a node happened to be written in.
func (e *exporter) collect() {
	e.children = make(map[dfcad.ID][]*dfcad.SemanticNode)
	e.products = make(map[dfcad.ID][]*dfcad.SemanticNode)
	e.members = make(map[dfcad.ID][]*dfcad.SemanticNode)

	var nodes []*dfcad.SemanticNode
	for node := range e.graph.Nodes().All() {
		// A retired node is one which stopped existing, and exporting it as a
		// live space is how a receiving system comes to hold a room which was
		// demolished. What a retirement means for an exchange — a delete, or
		// nothing at all — is the receiving system's, and saying nothing is
		// the only thing this can do without inventing a convention.
		if node.Retired() {
			continue
		}
		nodes = append(nodes, node)
	}

	slices.SortFunc(nodes, byNodeID)

	for _, node := range nodes {
		switch {
		case spatialEntity(node.Kind()) != "":
			if parent, ok := e.spatialParent(node); ok {
				e.children[parent] = append(e.children[parent], node)
				continue
			}
			e.roots = append(e.roots, node)

		case node.Kind() == dfcad.KindZone:
			e.zoned = append(e.zoned, node)

		default:
			// An element or an interface is a thing standing in a spatial
			// element rather than a part of one, so it is contained by the
			// nearest spatial ancestor it has. One with none is written
			// nowhere: IFC has no place for a product outside the spatial
			// structure, and inventing a storey to hold it would be this
			// command authoring a model.
			if parent, ok := e.spatialParent(node); ok {
				e.products[parent] = append(e.products[parent], node)
			}
		}
	}

	for _, node := range nodes {
		for _, zone := range node.MemberOf() {
			e.members[zone] = append(e.members[zone], node)
		}
	}

	// What the file will hold is decided here rather than during the walk,
	// because a reference across the decomposition — a zone assigning
	// something, a boundary naming the wall which realises it — has to be
	// answerable before the thing it names has been reached.
	e.hold(e.roots)
	for _, node := range e.zoned {
		e.written[node.ID()] = true
	}
}

// hold records the spatial elements beneath nodes, and the products standing
// in each, as things the file will hold.
//
// It walks exactly what [exporter.decompose] walks, so the answer is the set
// which will actually be written and not a superset of it: a node nothing
// reachable contains is written nowhere, and saying otherwise would leave a
// reference to it in the file.
func (e *exporter) hold(nodes []*dfcad.SemanticNode) {
	for _, node := range nodes {
		id := node.ID()
		e.written[id] = true

		for _, product := range e.products[id] {
			e.written[product.ID()] = true
		}

		e.hold(e.children[id])
	}
}

// spatialParent is the nearest spatial ancestor of node, and whether it has
// one.
//
// The walk is up the containment chain rather than one step, because an
// element may sit inside another element and IFC contains it in the storey
// either way. It is bounded by the number of nodes, which is what stops a
// containment cycle — a model which does not load, but this runs on models
// which loaded with warnings too — turning into a hang.
func (e *exporter) spatialParent(node *dfcad.SemanticNode) (dfcad.ID, bool) {
	seen := 0

	for {
		within, ok := node.Within()
		if !ok {
			return "", false
		}

		parent, held := e.graph.Node(within)
		if !held || parent.Retired() {
			return "", false
		}

		if spatialEntity(parent.Kind()) != "" {
			return parent.ID(), true
		}

		node = parent

		seen++
		if seen > e.graph.Nodes().Len() {
			return "", false
		}
	}
}

// decompose is a list of sibling spatial nodes as IFC holds them.
//
// datum is how far the placements above these nodes have already moved a shape
// off the root frame's origin. IFC writes a placement relative to the one it
// hangs from, so a coordinate in the file plus the placements above it is what
// a reader gets — and this export writes every coordinate in the root frame
// ([0024](docs/decisions/0024-every-coordinate-in-an-export-is-written-in-the-root-frame.md)),
// which is what the datum is taken back off so the two compose to the frame
// the geometry was carried into rather than to twice the lift.
func (e *exporter) decompose(nodes []*dfcad.SemanticNode, datum float64) []ifc.Spatial {
	out := make([]ifc.Spatial, 0, len(nodes))

	for _, node := range nodes {
		id := node.ID()

		// The elevation is read before the children are walked, so that a
		// chain which cannot be walked is reported against the storey it was
		// declared on rather than after everything beneath it.
		elevation := e.elevation(node)

		// Where this element's own placement stands, which is where its shape
		// and everything hanging off it are written from.
		standingAt := datum
		if elevation != nil {
			standingAt += *elevation
		}

		element := ifc.Spatial{
			Entity:      spatialEntity(node.Kind()),
			GlobalID:    e.identify(id),
			Name:        string(id),
			LongName:    node.Label(),
			ObjectType:  node.Type(),
			Composition: ifc.CompositionElement,
			Elevation:   elevation,
			// A storey stands at the elevation its frame chain puts it at, and
			// everything else is placed at its parent's origin. A node carries
			// no position of its own — a position is claimed of a corner, not
			// of a room — so the only coordinate this may write is one the
			// model already states, and a storey authored in its own plan
			// frame states exactly one: how far that frame's datum stands
			// above the root's. What the chain of placements carries beside it
			// is the structure, which is what makes moving a building move
			// everything in it.
			Placement: standing(elevation),
			Children:  e.decompose(e.children[id], standingAt),
			Products:  e.contained(e.products[id], standingAt),
		}

		// Whether there is anything to draw is the node's boundary's to say and
		// never its kind's. A storey and a site are decompositions and the
		// outline of either is the outline of what it holds, so in practice
		// neither references a loop and neither is drawn — but that is a fact
		// about how such models are authored, and a storey somebody did bound
		// and did measure the height of is a storey this draws.
		var drawn dfcad.RegionTessellation
		if e.shapes.complete() {
			element.Representation, element.Properties, drawn = e.shaped(node, standingAt)
		}

		// The boundaries are relationships rather than geometry, so they are
		// written for a space whether or not the run drew one. What the
		// drawing adds is the curve each of them ran along, and a run which
		// drew nothing writes the relationships without it.
		if node.Kind() == dfcad.KindSpace {
			element.Boundaries = e.bounding(node, drawn)
		}

		if len(element.Children) > 0 {
			element.Aggregates = e.identify(dfcad.ID("ifc/aggregates/" + id))
		}
		if len(element.Products) > 0 {
			element.Contains = e.identify(dfcad.ID("ifc/contains/" + id))
		}

		out = append(out, element)
	}

	return out
}

// elevation is how far a storey's frame stands above the root's, and is nil
// for anything which is not a storey, for a storey declaring no frame, and for
// one whose chain this refused to walk.
//
// It is the origin of the storey's frame carried into the root frame, which is
// the whole of what "the elevation of a level" means in a model authored a
// plan at a time: every corner of that level is written at nought in its own
// frame, and the height of the level is the transform between that frame and
// the one below it. Reading it from the chain rather than from the corners is
// what makes a model whose levels are authored in one frame come out unchanged
// — an identity chain gives an identity result — and what makes a model whose
// levels are authored in their own frames stop interpenetrating.
//
// Only the third coordinate is read. A plan frame offset horizontally from the
// root, or turned against it, is a thing this does not carry: IfcBuildingStorey
// states an elevation and nothing else about where its frame sits, and writing
// half of a rigid transform into the placement would put the storey's contents
// somewhere the model does not say. A frame chain which is more than a lift is
// a story of its own.
func (e *exporter) elevation(node *dfcad.SemanticNode) *float64 {
	if node.Kind() != dfcad.KindStorey {
		return nil
	}

	frame, declared := node.Frame()
	if !declared {
		return nil
	}

	frames := e.graph.Frames()

	root, rooted := frames.Root()
	if !rooted {
		e.refuse(node, fmt.Sprintf(
			"expected the frame %s of storey %s to reach a root frame to write the elevation of the storey, found "+
				"that this model declares none", frame, node.ID()),
			"a frame with no (parent ...) is the root every other frame is measured against; declare one, or take "+
				"the (frame ...) off the storey and it is written at the building's datum")
		return nil
	}

	origin, err := frames.TransformPoint(dfcad.Point{}, frame, root.ID)
	if err != nil {
		e.refuse(node, fmt.Sprintf(
			"expected to walk the frame chain from %s to %s to write the elevation of storey %s, found %s",
			frame, root.ID, node.ID(), err),
			"a storey whose frame does not reach the root has no elevation this can state, and writing it flat "+
				"would stack it inside the storey below")
		return nil
	}

	// The point comes back in the root frame's linear unit, which is the unit
	// the file states: an export whose frames disagree on one is refused
	// before the walk begins
	// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
	elevation := origin[2]

	return &elevation
}

// standing is where a spatial element's own coordinate system sits inside its
// parent's: at the parent's origin, or at the elevation a storey's frame chain
// put it at.
func standing(elevation *float64) *ifc.Placement {
	if elevation == nil {
		return &ifc.Placement{}
	}
	return &ifc.Placement{Location: ifc.Point{Z: *elevation}}
}

// contained is a list of products standing in one spatial element, written
// from where that element's placement stands.
func (e *exporter) contained(nodes []*dfcad.SemanticNode, datum float64) []ifc.Product {
	out := make([]ifc.Product, 0, len(nodes))

	for _, node := range nodes {
		entity, objectType := e.productEntity(node)

		product := ifc.Product{
			Entity:      entity,
			GlobalID:    e.identify(node.ID()),
			Name:        string(node.ID()),
			Description: node.Label(),
			ObjectType:  objectType,
			Placement:   &ifc.Placement{},
		}

		if e.shapes.complete() {
			e.carriable(node)
			product.Representation, product.Properties = e.modelled(node, datum)

			if placed, located := e.placed(node, datum); located {
				product.Placement = placed
			}
		}

		out = append(out, product)
	}

	return out
}

// carriable refuses a node whose type declares an IFC entity which cannot carry
// a shape, where the model claims a height of it.
//
// The proxy is the right answer to a classification this writer has no
// attribute list for, and it stays the answer for everything nobody has
// measured: a thing with no shape, written as an IfcBuildingElementProxy naming
// its own type, is a complete statement of what the model holds. What it is not
// is the right answer for a node somebody claimed a height of. The claim says a
// body was meant, the classification says where it was meant to go, and the two
// disagree — so this says which claim and which entity rather than writing the
// body somewhere the model did not point at, or quietly dropping it.
func (e *exporter) carriable(node *dfcad.SemanticNode) {
	declared, held := e.registry.Type(node.Type())
	if !held {
		return
	}

	code, classified := declared.ClassifiedAs(classificationSystem)
	if !classified {
		return
	}

	if ifc.Supports(ifc.Entity(strings.ToUpper(code))) == ifc.SupportWritable {
		return
	}

	claim, made := e.claimed(node, e.shapes.height)
	if !made {
		return
	}

	e.refuseAt(claim.Value().Span(), fmt.Sprintf(
		"expected the type %s of %s to be classified as an entity which can carry a shape, found %s: %s states a body "+
			"to draw and %s is not a thing to draw it on",
		node.Type(), node.ID(), code, namedClaim(claim, e.shapes.height, node.ID()), code),
		"classify the type as one of the entities this writer holds an attribute list for, or take the claim off the "+
			"node: a thing with no shape reaches the file as a proxy, and one with a shape has to be something which "+
			"can hold one")
}

// claimed is the claim a node makes under one predicate, where it makes one.
//
// It asks whether there is a claim rather than what the claim says, so a value
// which would be refused for its shape, its unit or its sign is a claim here
// all the same: the question is whether the author meant this node to have a
// body, and a height written wrongly means it exactly as much as one written
// well.
func (e *exporter) claimed(node *dfcad.SemanticNode, predicate string) (*dfcad.Claim, bool) {
	if predicate == "" {
		return nil, false
	}

	resolution, _ := e.graph.Claims().Resolve(node.ID(), predicate, nil)

	if claim, held := resolution.Claim(); held {
		return claim, true
	}

	candidates := resolution.Candidates()
	if len(candidates) == 0 {
		return nil, false
	}

	return candidates[0], true
}

// zones is every zone the model declares, with the members assigned to it.
func (e *exporter) zones() []ifc.Group {
	out := make([]ifc.Group, 0, len(e.zoned))

	for _, node := range e.zoned {
		id := node.ID()

		group := ifc.Group{
			GlobalID:   e.identify(id),
			Name:       string(id),
			LongName:   node.Label(),
			ObjectType: node.Type(),
		}

		for _, member := range e.members[id] {
			// A member which was not written — a node nothing spatial
			// contains, a retired one — is left out rather than referenced.
			// The assignment is over what this file holds, and a reference to
			// an object it does not is a file readers disagree about.
			if !e.written[member.ID()] {
				continue
			}
			group.Members = append(group.Members, e.identify(member.ID()))
		}

		if len(group.Members) > 0 {
			group.Assignment = e.identify(dfcad.ID("ifc/assigns/" + id))
		}

		out = append(out, group)
	}

	return out
}

// productEntity is what a node whose kind has no spatial entity is written as,
// and what goes in its ObjectType.
//
// The type's classification in the IFC4 system is what names the entity, which
// is the whole reason that child exists: a mapping from this project's
// vocabulary to a foreign one is a line of registry data somebody reviews
// rather than a table compiled into a program. A type which declares none — or
// which names an entity the writer has no attribute list for — is written as a
// proxy with the type's own name in ObjectType, which is what the IFC
// specification blesses that entity for.
func (e *exporter) productEntity(node *dfcad.SemanticNode) (ifc.Entity, string) {
	declared, held := e.registry.Type(node.Type())
	if !held {
		return ifc.EntityProxy, node.Type()
	}

	code, classified := declared.ClassifiedAs(classificationSystem)
	if !classified {
		return ifc.EntityProxy, node.Type()
	}

	// A code is an opaque string the registry wrote and nothing checks it, so
	// it may name an entity which is not a product at all — a relationship, a
	// spatial element, something misspelled. The proxy is the answer to every
	// one of those: the thing is still in the model and still has to be in the
	// file, and its type's name in ObjectType says what it is.
	//
	// What it is not is a silent answer. A proxy in the file is
	// indistinguishable from a proxy written for a type nobody classified, so
	// the code which produced it is recorded here — with which of the two
	// mistakes it is — and reported whether or not anything else about the node
	// went wrong.
	entity := ifc.Entity(strings.ToUpper(code))

	support := ifc.Supports(entity)
	if support == ifc.SupportWritable {
		return entity, node.Type()
	}

	e.proxy(node, declared, code, support)

	return ifc.EntityProxy, node.Type()
}

// proxy records a node whose type's classification this writer could not carry,
// and says so once about the type which carried it.
//
// The two granularities are on purpose. The answer names nodes, because a node
// is what a caller holding the file has in front of it and a proxy in the file
// says nothing about which one it was. The diagnostic names the type, because
// the type is what would be edited to fix it — a house model with nineteen
// doors has one classification to correct and would otherwise be told nineteen
// times.
func (e *exporter) proxy(node *dfcad.SemanticNode, declared dfcad.Type, code string, support ifc.Support) {
	if e.proxied == nil {
		e.proxied = make(map[dfcad.ID]exportedClassification)
	}

	reason := classificationUnknown
	if support == ifc.SupportProduct {
		reason = classificationUnwritten
	}

	e.proxied[node.ID()] = exportedClassification{
		ID:     string(node.ID()),
		Type:   node.Type(),
		Code:   code,
		Entity: string(ifc.EntityProxy),
		Reason: reason,
	}

	if e.reclassify == nil {
		e.reclassify = make(map[string]bool)
	}
	if e.reclassify[node.Type()] {
		return
	}
	e.reclassify[node.Type()] = true

	message := fmt.Sprintf(
		"expected the IFC4 classification of the type %s to name an entity this export writes, found %s, which IFC4 "+
			"defines and this export holds no attribute list for: every node of this type reaches the file as an "+
			"IfcBuildingElementProxy naming the type",
		declared.Name, code)
	hint := "classify the type as one of " + writableEntities() + ", or keep the proxy knowingly: a receiving system " +
		"cannot tell a proxy standing in for a door from one standing in for a duct"

	if support != ifc.SupportProduct {
		message = fmt.Sprintf(
			"expected the IFC4 classification of the type %s to name an IFC4 product entity, found %s, which names "+
				"none: every node of this type reaches the file as an IfcBuildingElementProxy naming the type",
			declared.Name, code)
		hint = "check the spelling, and check that the code names a product rather than a relationship, a property " +
			"set or a type object; the entities this export writes are " + writableEntities()
	}

	e.diags = append(e.diags, dfcad.Diagnostic{
		Severity: dfcad.SeverityWarning,
		Span:     classificationSpan(declared, node),
		Message:  message,
		Hint:     hint,
	})
}

// classificationSpan is where the IFC4 classification of a type was written, or
// the node's own span where the type carries none this can point at.
//
// A diagnostic which cannot say where is a bug in the reporting rather than a
// terse diagnostic, so there is a fallback rather than an empty span: a
// registry loaded from somewhere without positions still gets an answer which
// names something the reader can find.
func classificationSpan(declared dfcad.Type, node *dfcad.SemanticNode) dfcad.Span {
	for _, classification := range declared.Classifications {
		if classification.System == classificationSystem {
			return classification.Span
		}
	}

	if declared.Span != (dfcad.Span{}) {
		return declared.Span
	}

	return node.Span()
}

// writableEntities is the set a registry is authored against, rendered for a
// person.
//
// It is read from the writer rather than transcribed here, so the list a
// diagnostic offers cannot drift from the list the writer actually holds an
// attribute list for.
func writableEntities() string {
	products := ifc.Products()

	written := make([]string, 0, len(products))
	for _, entity := range products {
		written = append(written, string(entity))
	}

	return strings.Join(written, ", ")
}

// classifications is what [exporter.proxy] recorded, ascending by node id.
func (e *exporter) classifications() []exportedClassification {
	out := make([]exportedClassification, 0, len(e.proxied))
	for _, held := range e.proxied {
		out = append(out, held)
	}

	slices.SortFunc(out, func(a, b exportedClassification) int { return strings.Compare(a.ID, b.ID) })

	return out
}

// identify is the GlobalId of one name, recorded so that the manifest can
// report it.
//
// The relationships are named with a prefix which no id can carry: an id is
// written `namespace:local` and a namespace is a plain symbol, so nothing with
// a slash in it is one. That is what stops the identifier of the aggregation
// beneath a storey from colliding with the identifier of some node.
func (e *exporter) identify(name dfcad.ID) ifc.GlobalID {
	if held, ok := e.derived[name]; ok {
		return held
	}

	derived := ifc.GlobalID(dfcad.DeriveGlobalID(e.url, name))
	e.derived[name] = derived

	return derived
}

// identifiers is the manifest: every identifier this export derived, ascending
// by the name it was derived from.
//
// The relationships are in it as well as the nodes. Each of them is a rooted
// object carrying an identifier of its own, and a manifest which left them out
// would not account for every GlobalId in the file — which is the one thing it
// is for.
func (e *exporter) identifiers() []exportedIdentifier {
	out := make([]exportedIdentifier, 0, len(e.derived))

	for name, derived := range e.derived {
		out = append(out, exportedIdentifier{ID: string(name), GlobalID: string(derived)})
	}

	slices.SortFunc(out, func(a, b exportedIdentifier) int { return strings.Compare(a.ID, b.ID) })

	return out
}

// exportedUnits is the model's unit assignment, or the reason there is none.
//
// A file states one set of units and a model may declare a frame per grid, so
// the units are the frames' where they agree. They disagree in models which
// are perfectly sound — a survey grid in metres beside a fabrication grid in
// millimetres — and there is nothing here which could choose between them, so
// that is a refusal rather than a guess.
//
// The unit they agree on is written as it was authored, whichever it is.
// Nothing is converted here: a foot is an IfcConversionBasedUnit, which states
// the factor beside the numbers rather than applying it to them, and that is
// the whole of what [0005](docs/decisions/0005-one-linear-unit-per-frame.md)
// means by conversion happening at an export boundary. Converting the
// coordinates instead would round every one of them — 1200/3937 does not
// terminate in decimal — and the file would stop carrying the numbers the
// survey published.
func exportedUnits(registry *dfcad.Registry) (ifc.UnitAssignment, *dfcad.Diagnostic) {
	var declared []dfcad.Unit
	var span dfcad.Span

	for frame := range registry.Frames() {
		if !slices.Contains(declared, frame.Unit) {
			declared = append(declared, frame.Unit)
			span = frame.Span
		}
	}

	// A model declaring no frame at all measures nothing, and the metre is
	// what an IFC file with nothing measured in it says.
	unit := dfcad.UnitMetre
	if len(declared) == 1 {
		unit = declared[0]
	}

	if len(declared) > 1 {
		return ifc.UnitAssignment{}, &dfcad.Diagnostic{
			Severity: dfcad.SeverityError,
			Span:     span,
			Message: fmt.Sprintf(
				"expected the frames of this model to agree on one linear unit, found %s: an exchange file states one set "+
					"of units and there is nothing here which could choose between them",
				join(sorted(spellings(declared)))),
			Hint: "export the frames which share a unit as their own model, or declare them in one unit",
		}
	}

	// A foot has no SI spelling and IFC4 has a first-class form for exactly
	// that: the file names the conversion instead of applying it, so the
	// coordinates stay the numbers the survey published.
	if converted, held := conversions[unit]; held {
		return converted.assignment(), nil
	}

	prefix, held := siPrefixes[unit]
	if !held {
		return ifc.UnitAssignment{}, &dfcad.Diagnostic{
			Severity: dfcad.SeverityError,
			Span:     span,
			Message: fmt.Sprintf(
				"expected a unit this exporter writes, found %s: it is neither one of the metre's spellings nor a "+
					"conversion over the metre",
				unit),
			Hint: "author the frame in one of the units the engine defines",
		}
	}

	return ifc.UnitAssignment{Units: []ifc.Unit{
		ifc.SIUnit{Type: "LENGTHUNIT", Prefix: prefix, Name: "METRE"},
		ifc.SIUnit{Type: "AREAUNIT", Prefix: prefix, Name: "SQUARE_METRE"},
		ifc.SIUnit{Type: "VOLUMEUNIT", Prefix: prefix, Name: "CUBIC_METRE"},
		ifc.SIUnit{Type: "PLANEANGLEUNIT", Name: "RADIAN"},
	}}, nil
}

// siPrefixes is the IfcSIPrefix each of the engine's metric units is written
// with.
//
// The two feet are absent, which is what keeps this a table of prefixes rather
// than a list of special cases: IFC writes a foot as an IfcConversionBasedUnit
// over the metre, [conversions] is where that is written, and an entry here
// pretending otherwise would produce a file whose lengths are wrong by a factor
// of three.
var siPrefixes = map[dfcad.Unit]string{
	dfcad.UnitMillimetre: "MILLI",
	dfcad.UnitCentimetre: "CENTI",
	dfcad.UnitMetre:      "",
	dfcad.UnitKilometre:  "KILO",
}

// The two feet in metres, exactly as [SPEC §4.5](SPEC.md#45-units) pins them
// and [0005](docs/decisions/0005-one-linear-unit-per-frame.md) refuses to
// conflate them.
//
// They are untyped constants restating the engine's rather than a call to
// [dfcad.Unit.Metres], because the square and the cube below have to be one
// rounding of exact arithmetic and not the product of a number already
// rounded: a foot cubed is 0.028316846592, and the same product over float64s
// is 0.028316846592000004. A test asserts these are the engine's numbers, so
// restating them cannot mean disagreeing with them.
const (
	foot       = 0.3048
	surveyFoot = 1200.0 / 3937.0
)

// conversion is how one unit the SI has no name for is written: the name it is
// known by, and how much of the SI unit one of it, its square and its cube are.
type conversion struct {
	name   string
	length float64
	area   float64
	volume float64
}

// conversions is the IfcConversionBasedUnit each of the engine's feet is
// written as.
//
// The names are the two feet's own and they are deliberately unalike. A reader
// which keys off the name rather than the factor — IfcOpenShell's table holds
// `foot` at 0.3048 and has no entry for the survey foot at all — puts a model
// four feet out at a state plane false easting if both are called the same
// thing. The factor is what states the unit; a distinguishing name costs
// nothing and stops a careless reader from being confidently wrong.
var conversions = map[dfcad.Unit]conversion{
	dfcad.UnitFoot: {
		name:   "foot",
		length: foot,
		area:   foot * foot,
		volume: foot * foot * foot,
	},
	dfcad.UnitSurveyFoot: {
		name:   "US survey foot",
		length: surveyFoot,
		area:   surveyFoot * surveyFoot,
		volume: surveyFoot * surveyFoot * surveyFoot,
	},
}

// assignment is the four units a model authored in this one assigns.
//
// Three of them are conversions rather than one, because a length, an area and
// a volume are three units in an assignment and the factor between a square
// foot and a square metre is not the factor between a foot and a metre. What
// distinguishes them in the file is the dimensional exponent each carries. The
// plane angle stays an IfcSIUnit: a radian is a radian in every unit a model
// is authored in.
func (c conversion) assignment() ifc.UnitAssignment {
	return ifc.UnitAssignment{Units: []ifc.Unit{
		ifc.ConversionBasedUnit{
			Type:       "LENGTHUNIT",
			Dimensions: ifc.DimensionalExponents{Length: 1},
			Name:       c.name,
			Factor: ifc.MeasureWithUnit{
				Measure: "LENGTHMEASURE",
				Value:   c.length,
				Unit:    ifc.SIUnit{Type: "LENGTHUNIT", Name: "METRE"},
			},
		},
		ifc.ConversionBasedUnit{
			Type:       "AREAUNIT",
			Dimensions: ifc.DimensionalExponents{Length: 2},
			Name:       "square " + c.name,
			Factor: ifc.MeasureWithUnit{
				Measure: "AREAMEASURE",
				Value:   c.area,
				Unit:    ifc.SIUnit{Type: "AREAUNIT", Name: "SQUARE_METRE"},
			},
		},
		ifc.ConversionBasedUnit{
			Type:       "VOLUMEUNIT",
			Dimensions: ifc.DimensionalExponents{Length: 3},
			Name:       "cubic " + c.name,
			Factor: ifc.MeasureWithUnit{
				Measure: "VOLUMEMEASURE",
				Value:   c.volume,
				Unit:    ifc.SIUnit{Type: "VOLUMEUNIT", Name: "CUBIC_METRE"},
			},
		},
		ifc.SIUnit{Type: "PLANEANGLEUNIT", Name: "RADIAN"},
	}}
}

// spatialEntity is the IFC entity a kind is written as, and is empty for a
// kind which is not part of the spatial decomposition.
//
// This is the table the whole export turns on and it is four lines, because
// this engine's kinds are IFC's spatial decomposition one for one. A zone is
// not here: it is a group with members rather than a part of the tree, and
// [dfcad.KindElement] and [dfcad.KindInterface] are not either, because what
// they are written as is the type's to say.
func spatialEntity(kind dfcad.Kind) ifc.Entity {
	switch kind {
	case dfcad.KindSite:
		return ifc.EntitySite
	case dfcad.KindBuilding:
		return ifc.EntityBuilding
	case dfcad.KindStorey:
		return ifc.EntityBuildingStorey
	case dfcad.KindSpace:
		return ifc.EntitySpace
	default:
		return ""
	}
}

// byNodeID orders nodes by their id, which is the order everything about an
// export is walked in.
func byNodeID(a, b *dfcad.SemanticNode) int {
	return strings.Compare(string(a.ID()), string(b.ID()))
}

// sorted is a set in name order, so that a message which lists it reads the
// same however the model happened to be walked.
func sorted(items []string) []string {
	slices.Sort(items)
	return items
}
