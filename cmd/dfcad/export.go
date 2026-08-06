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

Flags:

	--out <path>               where to write the file (default: beneath
	                           .dfcad/export, in a directory named for the
	                           digest of the source tree)
	--evidence                 add the identifier manifest: every node and the
	                           GlobalId derived for it
	--position <predicate>     the predicate a corner's position is claimed
	                           under, which a space's outline is read from
	--tolerance <name>         the tolerance corners are judged coincident
	                           against and rings judged planar against
	--chord <name>             the tolerance a segment standing in for a curve
	                           may fall from it by
	--arc-centre <predicate>   the predicate a curved edge's centre is claimed
	                           under
	--arc-through <predicate>  the predicate the point a curved edge passes
	                           through is claimed under
	--height <predicate>       the predicate a space's height is claimed under,
	                           which is what a body is swept through
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
A run which names all three draws every space it can: an IfcSpace carries a
FootPrint representation built from the rings bounding it, holes and all, drawn
to the chord tolerance named so a curved wall reaches the file as the curve it
is rather than as a straight line nobody asked for.

--height is what adds a body. Where it names a predicate and a space's height
resolves under it, the space additionally carries a SweptSolid representation —
the footprint extruded upwards, holes carried through as the profile's inner
curves — and the two live in one shape definition. The footprint is what the
model states and the body is a convenience built from a claim, which is why
they are two representations rather than one: a reader wanting what the model
says takes the FootPrint and never has to guess which of the two that was.

There is no default height and there never will be. Which predicate carries a
room's height is project vocabulary, and a run which names none exports
footprints rather than failing — a two dimensional export is correct, and it is
the one an author who has drawn plans and measured nothing should get. A space
nothing claims a height of is exported the same way. A height which resolves to
nought or less is refused naming the claim, because the depth a profile is
swept through is a positive length measure and there is no zero-height solid.

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

The map conversion is the identity, always. The root frame is the projected
system the chain is rooted at, so the file's coordinates are already the
system's: there is no offset between them, no rotation and no scale, and
writing one would state a fit nobody measured. A coordinate reference system
written on any frame but the root is refused for the same reason — every other
frame reaches the root through a measured transform, and a second georeference
beside it would be a second answer nothing reconciles.

The units are the frames' own, and the frames have to agree on one: a file
states one set of units, and a model whose frames declare two is refused rather
than exported in whichever was found first. A model authored in feet is written
in feet. Each foot is an IfcConversionBasedUnit stating its factor over the
metre — the international foot and the US survey foot under names which tell
them apart, because they differ by two parts per million and a reader keying
off the name rather than the factor would otherwise read one as the other.
Nothing is converted: a coordinate written in the source is that coordinate in
the file.

Where a body is written, the claim behind its height goes into the file beside
it as a property set: the predicate, the value, the source, the method, the
accuracy, the date and which step of the resolution rule chose it. That is what
lets whoever opens the file tell a surveyed height from an assumed one without
holding the model it came from.

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
space whose shape was asked for and could not be drawn: a ring which does not
close, a corner nothing states the position of, a boundary which does not lie
at one level, a height which is not a distance or is not positive. The object
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
		envelope: newEnvelope(cmd.name),
		Files:    []exportedFile{},
	}

	digest, keyed := graph.Digest()
	if keyed {
		result.Digest = digest.String()
	}

	model, manifest, diags := exported(graph, dfcad.DerivationEpoch(digest), drawn, sited)

	if destination == "" {
		destination = filepath.Join(dfcad.ExportDir(globals.Root), digest.String(), exportFile)
	}

	if render(diags, stderr) {
		if err := emit(stdout, result); err != nil {
			fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
			return exitLoad
		}
		return exitCheck
	}

	var written bytes.Buffer
	if err := ifc.Write(&written, model); err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)

		if err := emit(stdout, result); err != nil {
			fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
			return exitLoad
		}
		return exitCheck
	}

	status, err := place(destination, written.Bytes())
	if err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
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
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
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
	defer os.Remove(temporary.Name())

	if _, err := temporary.Write(artefact); err != nil {
		temporary.Close()
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
		fmt.Fprintf(stderr, "%s: %s (%s)\n", file.Path, file.Status, result.Schema)
	}
}

// exported is the model as IFC holds it, the manifest of the identifiers it
// carries, and whatever stopped either being derivable.
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
) (ifc.Model, []exportedIdentifier, []dfcad.Diagnostic) {
	registry := graph.Registry()

	project, held := registry.Project()
	if !held || project.GlobalIDNamespace == "" {
		return ifc.Model{}, nil, []dfcad.Diagnostic{{
			Severity: dfcad.SeverityError,
			Span:     project.Span,
			Message: "expected a project declaration pinning the URL identifiers derive from, found none: every object in " +
				"an IFC file carries a GlobalId, and there is nothing to derive one from",
			Hint: "write (globalid-namespace \"https://example.org/models/name\") on the project declaration",
		}}
	}

	units, diagnostic := exportedUnits(registry)
	if diagnostic != nil {
		return ifc.Model{}, nil, []dfcad.Diagnostic{*diagnostic}
	}

	// The georeference is settled before the walk because it is a fact about
	// the registry rather than about any node, and a model which cannot say
	// where it sits is refused before an artefact is built out of it.
	placed, refused := georeferenced(registry, graph.Frames(), sited)
	if len(refused) > 0 {
		return ifc.Model{}, nil, refused
	}

	out := &exporter{
		graph:    graph,
		registry: registry,
		url:      project.GlobalIDNamespace,
		shapes:   drawn,
		derived:  make(map[dfcad.ID]ifc.GlobalID),
		written:  make(map[dfcad.ID]bool),
	}
	out.collect()

	// The two are built in statements rather than in the literal below because
	// the second reads what the first recorded: a group assigns objects this
	// file holds, and which those are is not known until they are held.
	sites := out.decompose(out.roots)
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
		Georeference: placed.projected(),
		Project: ifc.Project{
			GlobalID:   out.identify(dfcad.ID("ifc/project")),
			Name:       project.Label,
			LongName:   project.Description,
			Aggregates: out.identify(dfcad.ID("ifc/aggregates/project")),
			Sites:      sites,
			Groups:     groups,
		},
	}

	return model, out.identifiers(), out.diags
}

// exporter is one traversal of the graph into IFC's shape.
type exporter struct {
	graph    *dfcad.Graph
	registry *dfcad.Registry
	url      string

	// shapes is the vocabulary geometry is read under, and is empty for a run
	// which asked for none.
	shapes shapes

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
func (e *exporter) decompose(nodes []*dfcad.SemanticNode) []ifc.Spatial {
	out := make([]ifc.Spatial, 0, len(nodes))

	for _, node := range nodes {
		id := node.ID()

		element := ifc.Spatial{
			Entity:      spatialEntity(node.Kind()),
			GlobalID:    e.identify(id),
			Name:        string(id),
			LongName:    node.Label(),
			ObjectType:  node.Type(),
			Composition: ifc.CompositionElement,
			// Nothing here is placed anywhere but at its parent's origin. A
			// node carries no position — a position is claimed of a corner,
			// not of a room — so a placement with coordinates in it would be a
			// number this command made up. What the chain of placements does
			// carry is the structure: moving a building moves everything in
			// it.
			Placement: &ifc.Placement{},
			Children:  e.decompose(e.children[id]),
			Products:  e.contained(e.products[id]),
		}

		// A space is the one thing drawn here, because it is the one thing a
		// boundary is written of: a storey and a site are decompositions, and
		// the outline of either is the outline of what it holds.
		var drawn dfcad.RegionTessellation
		if e.shapes.complete() && node.Kind() == dfcad.KindSpace {
			element.Representation, element.Properties, drawn = e.shaped(node)
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

// contained is a list of products standing in one spatial element.
func (e *exporter) contained(nodes []*dfcad.SemanticNode) []ifc.Product {
	out := make([]ifc.Product, 0, len(nodes))

	for _, node := range nodes {
		entity, objectType := e.productEntity(node)

		out = append(out, ifc.Product{
			Entity:      entity,
			GlobalID:    e.identify(node.ID()),
			Name:        string(node.ID()),
			Description: node.Label(),
			ObjectType:  objectType,
			Placement:   &ifc.Placement{},
		})
	}

	return out
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
	entity := ifc.Entity(strings.ToUpper(code))
	if !slices.Contains(ifc.Products(), entity) {
		return ifc.EntityProxy, node.Type()
	}

	return entity, node.Type()
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
