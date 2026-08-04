// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad_test

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/z5labs/dfcad"
)

func ExampleParse() {
	source := `(node site:S-101
  (label "Meeting Room B")
  (kind Space))
`

	file, err := dfcad.Parse("entities/level-1.dfc", strings.NewReader(source))
	if err != nil {
		fmt.Println(err)
		return
	}

	// The label, two lists down, still knows where it was written.
	label := file.Nodes[0].Children[2].Children[1]

	fmt.Println(label.Span.Start)
	fmt.Println(source[label.Span.Start.Offset:label.Span.End.Offset])

	// Output:
	// entities/level-1.dfc:2:10
	// "Meeting Room B"
}

func ExampleParse_failure() {
	_, err := dfcad.Parse("entities/level-1.dfc", strings.NewReader("(date 2026-03-14)\n"))

	var parseErr dfcad.ParseError
	if errors.As(err, &parseErr) {
		fmt.Println(parseErr.Position)
	}

	// Output:
	// entities/level-1.dfc:1:7
}

func ExampleLoad() {
	// Every entity file beneath the root arrives in a deterministic order, one
	// at a time, and a file which fails to load does not stop the walk.
	for file, err := range dfcad.Load("testdata/model") {
		if err != nil {
			fmt.Println("could not load:", err)
			continue
		}
		fmt.Printf("%s: %d top-level forms\n", file.Path, len(file.Nodes))
	}

	// Output:
	// testdata/model/entities/level-1.dfc: 2 top-level forms
	// testdata/model/registry/registry.dfc: 4 top-level forms
}

func ExampleLoadGraph() {
	// One call reads the whole model: the registry, both families of nodes, the
	// claims written on them, the frames and the boundaries. Every file beneath
	// the root is read once, whichever of the six the forms in it belong to.
	graph, diags := dfcad.LoadGraph("testdata/graph/valid")
	for _, diagnostic := range diags {
		fmt.Println(diagnostic)
	}

	fmt.Println(graph.Summary())

	// An id names one thing in the whole model, so a lookup takes an id and not
	// an id and a family.
	room, ok := graph.Node("site:S-101")
	if !ok {
		return
	}
	fmt.Println(room.Label())

	// What contains it, outwards.
	for related := range graph.Ancestors(room) {
		fmt.Println(related.Relation(), related.Node().ID())
	}

	// Output:
	// 7 nodes, 6 vertices, 7 edges, 2 loops, 10 claims, 1 conflicts, 0 unresolved
	// Meeting Room B
	// containment site:L-01
	// containment site:B-01
	// containment site:S-01
}

func ExampleDiagnostic_Render() {
	const path = "entities/level-1.dfc"

	source := `(node site:S-101
  (label "Meeting Room B")
  (position (value (0.0 4.05 0.0))))
`

	file, err := dfcad.Parse(path, strings.NewReader(source))
	if err != nil {
		fmt.Println(err)
		return
	}

	// The value written without the unit which has to follow it.
	value := file.Nodes[0].Children[3].Children[1]

	diagnostic := dfcad.Diagnostic{
		Severity: dfcad.SeverityError,
		Span:     value.Span,
		Message:  "expected a unit after the value, found none",
		Hint:     "units are registry data; a frame declares the one its coordinates are in",
	}

	if err := diagnostic.Render(os.Stdout, dfcad.Sources{path: []byte(source)}); err != nil {
		fmt.Println(err)
	}

	// Output:
	// entities/level-1.dfc:3:13: error: expected a unit after the value, found none
	// 3 |   (position (value (0.0 4.05 0.0))))
	//   |             ^^^^^^^^^^^^^^^^^^^^^^
	//   = hint: units are registry data; a frame declares the one its coordinates are in
}

func ExampleLoadRegistry() {
	// One registry for the whole source tree: the frame declared in the first
	// file names a parent declared in the second, and both are one registry.
	registry, diagnostics := dfcad.LoadRegistry("testdata/registry/valid")
	for _, diagnostic := range diagnostics {
		fmt.Println(diagnostic)
	}

	project, _ := registry.Project()
	fmt.Println(project.GlobalIDNamespace)

	room, _ := registry.Type("MeetingRoom")
	fmt.Println(room.PermitsKind(dfcad.KindSpace), room.PermitsGeometry(dfcad.GeometrySolid))

	building, _ := registry.Frame("frame:building")
	fmt.Println(building.Unit, building.Parent)

	// Output:
	// https://example.org/models/riverside
	// true false
	// m frame:survey-grid
}

func ExampleLoadNodes() {
	// The registry resolves first. Whether a type is declared, and which kind
	// and which geometry form it permits, is the only thing which can judge a
	// node's axes.
	registry, diagnostics := dfcad.LoadRegistry("testdata/node/valid")
	for _, diagnostic := range diagnostics {
		fmt.Println(diagnostic)
	}

	nodes, diagnostics := dfcad.LoadNodes("testdata/node/valid", registry)
	for _, diagnostic := range diagnostics {
		fmt.Println(diagnostic)
	}

	for node := range nodes.All() {
		// A node with no geometry is an ordinary node and not a broken one, so
		// the axis reports absence rather than an empty value.
		geometry, ok := node.Geometry()
		if !ok {
			fmt.Printf("%s: %s %s, no geometry\n", node.ID(), node.Kind(), node.Type())
			continue
		}
		fmt.Printf("%s: %s %s, %s\n", node.ID(), node.Kind(), node.Type(), geometry)
	}

	// Output:
	// site:Z-01: Zone Campus, area
	// site:S-01: Site SiteBoundary, area
	// site:B-01: Building OfficeBuilding, solid
	// site:L-01: Storey Level, surface
	// site:S-101: Space MeetingRoom, area
	// site:E-01: Element Partition, line
	// site:I-01: Interface Doorway, point
	// site:C-01: Zone CircuitGroup, no geometry
}

func ExampleLoadTopology() {
	// The geometric family is read by a pass of its own, because it validates
	// under its own rules: a vertex is not a node missing its kind.
	registry, diagnostics := dfcad.LoadRegistry("testdata/topology/valid")
	for _, diagnostic := range diagnostics {
		fmt.Println(diagnostic)
	}

	topology, diagnostics := dfcad.LoadTopology("testdata/topology/valid", registry)
	for _, diagnostic := range diagnostics {
		fmt.Println(diagnostic)
	}

	// An edge is an ordered pair of vertices: start then end, never sorted.
	edge, _ := topology.Edge("geom:E-01")
	start, end := edge.Vertices()
	fmt.Printf("%s: %s to %s\n", edge.ID(), start, end)

	// A loop is the ring of edges the outline is traversed through, in the
	// order it was written. Two rooms either side of a partition reference the
	// same edge, which is what makes the partition one thing rather than two
	// copies free to drift apart.
	loop, _ := topology.Loop("geom:L-01")
	fmt.Println(loop.ID(), loop.Edges())

	// Output:
	// geom:E-01: geom:V-01 to geom:V-02
	// geom:L-01 [geom:E-03 geom:E-04 geom:E-01 geom:E-02]
}

// ExampleLoadTopology_position is the arrangement two node families exist for:
// where a corner was measured is a claim on the corner, with the same
// provenance and the same accuracy rules as the width of a room.
func ExampleLoadTopology_position() {
	registry, _ := dfcad.LoadRegistry("testdata/topology/valid")
	topology, _ := dfcad.LoadTopology("testdata/topology/valid", registry)
	claims, _ := dfcad.LoadClaims("testdata/topology/valid", registry)

	corner, _ := topology.Vertex("geom:V-01")

	// One corner, surveyed twice. Neither reading is thrown away, and the
	// engine has no coordinate field which could have held only one of them.
	for claim := range claims.Under(corner.ID(), "position") {
		position, _ := claim.Value().Coordinate()
		accuracy, _ := claim.Accuracy()

		fmt.Printf("%v %s +/- %g %s, %s\n",
			position, claim.Value().Unit(),
			accuracy.Terms[0].Magnitude, accuracy.Terms[0].Unit,
			claim.Date().Format("2006-01-02"))
	}

	// Output:
	// [0 0 0] m +/- 0.012 m, 2026-02-18
	// [0.004 0 0] m +/- 0.003 m, 2026-05-06
}

func ExampleLoadClaims() {
	// The registry resolves first. Which predicates exist, which of the four
	// shapes each one's value takes and which unit it is expressed in are the
	// only things which can judge a claim.
	registry, diagnostics := dfcad.LoadRegistry("testdata/claim/valid")
	for _, diagnostic := range diagnostics {
		fmt.Println(diagnostic)
	}

	claims, diagnostics := dfcad.LoadClaims("testdata/claim/valid", registry)
	for _, diagnostic := range diagnostics {
		fmt.Println(diagnostic)
	}

	// How wide is that room, and how do you know? One lookup answers both,
	// because a dimension here is a value plus the evidence for it rather than
	// a column with the provenance in another table.
	//
	// Two claims under one predicate is the normal case, and the disagreement
	// between them is the most valuable thing in the file.
	for claim := range claims.Under("site:S-101", "width") {
		width, _ := claim.Value().Scalar()
		accuracy, _ := claim.Accuracy()

		fmt.Printf("%g %s +/- %g %s, %s, %s\n",
			width, claim.Value().Unit(),
			accuracy.Terms[0].Magnitude, accuracy.Terms[0].Unit,
			claim.Source(), claim.Date().Format("2006-01-02"))
	}

	// Output:
	// 8.5 m +/- 0.05 m, Plan set A-101, sheet 3, 2026-01-09
	// 8.53 m +/- 0.003 m, As-built check AB-2026-009, Acme Surveys, 2026-05-06
}

// ExampleLoadClaims_bareScalar is the rule which keeps every other example on
// this page meaning something: where a claim belongs, a number on its own does
// not load.
func ExampleLoadClaims_bareScalar() {
	registry, _ := dfcad.LoadRegistry("testdata/claim/bare-scalar")

	// `(width 8.5)` is one keystroke from correct and reads as a simplification
	// in review, which is why it is a load error rather than a warning: a
	// warning that appears ten thousand times is suppressed the same afternoon,
	// and the provenance model is gone with nothing in the history saying so.
	//
	// Nothing downgrades it. There is no flag, no environment variable and no
	// configuration, because the distinction between a diagnostic which fails
	// the load and one which does not is a property of the rule.
	claims, diagnostics := dfcad.LoadClaims("testdata/claim/bare-scalar", registry)
	for _, diagnostic := range diagnostics {
		fmt.Println(diagnostic)
		fmt.Println(diagnostic.Hint)
	}

	// The escape hatch is narrow and deliberate. A claim which cannot say how
	// good its value is leaves the accuracy out, and it loads — as unrankable,
	// which is visible as such rather than as a number quietly indistinguishable
	// from a surveyed one.
	for claim := range claims.Under("site:S-101", "width") {
		width, _ := claim.Value().Scalar()
		fmt.Printf("%g %s, %s, rankable %t\n", width, claim.Value().Unit(), claim.Method(), claim.Rankable())
	}

	// Output:
	// testdata/claim/bare-scalar/claims.dfc:11:3: error: expected the claim the predicate width bears, found a plain value
	// the least a claim may say is (width (value <number> m) (source "<evidence>") (method <method-id>) (date "<YYYY-MM-DD>")); accuracy may be left out, and the claim then loads as unrankable
	// 8.4 m, method:estimated, rankable false
	// 8.5 m, method:scaled-from-plan, rankable true
}

func ExampleClaim_Accuracy() {
	registry, _ := dfcad.LoadRegistry("testdata/claim/valid")
	claims, _ := dfcad.LoadClaims("testdata/claim/valid", registry)

	// A claim which does not say how well its value is known loads, and is
	// unrankable: it can never win resolution and it is not given a default,
	// because a default would be the engine inventing the one figure the claim
	// exists to record. It is still a candidate when nothing rankable exists.
	for claim := range claims.Under("site:S-101", "occupancy") {
		_, stated := claim.Accuracy()
		fmt.Println(claim.Predicate(), claim.Rank(), stated, claim.Rankable())
	}

	// Output:
	// occupancy normal false false
}

func ExampleRegistry_Undeclared() {
	const path = "entities/level-1.dfc"

	source := "(node site:S-101 (kind Space) (type MeetingRoom))\n"

	file, err := dfcad.Parse(path, strings.NewReader(source))
	if err != nil {
		fmt.Println(err)
		return
	}

	// A repository which has not written its registry yet loads, and every node
	// in it is invalid against a registry which declares nothing.
	registry, _ := dfcad.LoadRegistry("testdata/registry/empty")

	written := file.Nodes[0].Children[3].Children[1]
	undeclared := registry.Undeclared(dfcad.SortType, "MeetingRoom", written.Span)

	if err := undeclared.Render(os.Stdout, dfcad.Sources{path: []byte(source)}); err != nil {
		fmt.Println(err)
	}

	// Output:
	// entities/level-1.dfc:1:37: error: expected a declared type, found MeetingRoom, which no registry file declares
	// 1 | (node site:S-101 (kind Space) (type MeetingRoom))
	//   |                                     ^^^^^^^^^^^
	//   = hint: no type is declared; a registry file declares one with (type ...)
}

func ExampleValidate() {
	const path = "registry/registry.dfc"

	source := `(project
  (label "Riverside example")
  (globalid-namesapce "https://example.org/models/riverside"))
`

	file, err := dfcad.Parse(path, strings.NewReader(source))
	if err != nil {
		fmt.Println(err)
		return
	}

	// One pass reports both the misspelled tag and the required child the
	// misspelling leaves missing, rather than stopping at the first.
	var diagnostics dfcad.Diagnostics
	diagnostics.Add(dfcad.Validate(file)...)

	if err := diagnostics.Render(os.Stdout, dfcad.Sources{path: []byte(source)}); err != nil {
		fmt.Println(err)
	}

	// Output:
	// registry/registry.dfc:1:1: error: expected a (globalid-namespace ...) child of the project form, found none
	// 1 | (project
	//   | ^^^^^^^^
	// 2 |   (label "Riverside example")
	//   | ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
	// 3 |   (globalid-namesapce "https://example.org/models/riverside"))
	//   | ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
	// registry/registry.dfc:3:3: error: expected a child of the project form, found (globalid-namesapce ...), which is not a known form
	// 3 |   (globalid-namesapce "https://example.org/models/riverside"))
	//   |   ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
	//   = hint: did you mean (globalid-namespace ...)?
}

func ExampleDiagnostics() {
	const path = "entities/level-1.dfc"

	source := `(node site:S-101
  (label "Meeting Room B"))
(node site:S-101
  (label "Meeting Room C"))
`

	file, err := dfcad.Parse(path, strings.NewReader(source))
	if err != nil {
		fmt.Println(err)
		return
	}

	// One pass reports every problem it finds, in a deterministic order, even
	// though these two are collected in the order they happen to be noticed.
	var diagnostics dfcad.Diagnostics

	diagnostics.Add(dfcad.Diagnostic{
		Severity: dfcad.SeverityError,
		Span:     file.Nodes[1].Children[1].Span,
		Message:  "expected an unused id, found site:S-101, which is already defined",
		Related: []dfcad.RelatedLocation{
			{Span: file.Nodes[0].Children[1].Span, Message: "first defined here"},
		},
	})
	diagnostics.Add(dfcad.Diagnostic{
		Severity: dfcad.SeverityWarning,
		Span:     file.Nodes[0].Children[2].Span,
		Message:  "expected a (type ...) child before the claims, found none",
	})

	if err := diagnostics.Render(os.Stdout, dfcad.Sources{path: []byte(source)}); err != nil {
		fmt.Println(err)
	}

	fmt.Println(diagnostics.HasErrors())

	// Output:
	// entities/level-1.dfc:2:3: warning: expected a (type ...) child before the claims, found none
	// 2 |   (label "Meeting Room B"))
	//   |   ^^^^^^^^^^^^^^^^^^^^^^^^
	// entities/level-1.dfc:3:7: error: expected an unused id, found site:S-101, which is already defined
	// 3 | (node site:S-101
	//   |       ^^^^^^^^^^
	// entities/level-1.dfc:1:7: note: first defined here
	// 1 | (node site:S-101
	//   |       ^^^^^^^^^^
	// true
}

func ExamplePrint() {
	source := `(node site:S-101
  (frame frame:building)
  ; The corner the survey started from.
  (position
    (rank normal)
    (date "2026-02-18")
    (value (0.0 0.00 0.0) m)
    (method method:total-station)
    (source "Interior control set IC-01"))
  (kind Space)
  (label "Meeting Room B")
  (type MeetingRoom))
`

	file, err := dfcad.Parse("entities/level-1.dfc", strings.NewReader(source))
	if err != nil {
		fmt.Println(err)
		return
	}

	// Canonical form puts the children in the order the format gives them,
	// leaves out the rank which is already the default, and carries the comment
	// along with the claim it annotates.
	if err := dfcad.Print(os.Stdout, file); err != nil {
		fmt.Println(err)
	}

	// Output:
	// (node
	//   site:S-101
	//   (label "Meeting Room B")
	//   (kind Space)
	//   (type MeetingRoom)
	//   (frame frame:building)
	//   ; The corner the survey started from.
	//   (position
	//     (value (0.0 0.0 0.0) m)
	//     (source "Interior control set IC-01")
	//     (method method:total-station)
	//     (date "2026-02-18")))
}

func ExampleFormatter() {
	// The zero value writes nothing, so this reports what a rewrite would do
	// without doing any of it. Setting Rewrite is what replaces the files.
	for _, file := range (dfcad.Formatter{}).Format("testdata/model") {
		switch {
		case file.Failed():
			fmt.Printf("%s: could not be formatted\n", file.Path)
		case file.Changed:
			fmt.Printf("%s: not in canonical form\n", file.Path)
		}
	}

	// Output:
	// testdata/model/entities/level-1.dfc: not in canonical form
	// testdata/model/registry/registry.dfc: not in canonical form
}

func ExampleParseID() {
	// The split is on the first colon, so a local part may hold further ones
	// and the namespace never does.
	id, err := dfcad.ParseID("survey:2026:CP-3")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%s | %s\n", id.Namespace(), id.Local())

	// What an id which is not one broke is a field rather than wording inside a
	// message, so a caller can tell a forgotten namespace apart from a
	// misspelled one.
	_, err = dfcad.ParseID("corner")

	var malformed dfcad.MalformedIDError
	if errors.As(err, &malformed) {
		fmt.Printf("%s: %s\n", malformed.Written, malformed.Reason)
	}

	// Output:
	// survey | 2026:CP-3
	// corner: unqualified
}

func ExampleRegistry_GlobalID() {
	// The URL is pinned in the registry, and the GlobalId falls out of it and
	// the node id. Nothing is stored, nothing is authored, and the same two
	// inputs produce the same 22 characters on every machine.
	registry, _ := dfcad.LoadRegistry("testdata/model")

	globalID, ok := registry.GlobalID("site:S-101")
	if !ok {
		fmt.Println("no project declaration to derive from")
		return
	}

	project, _ := registry.Project()

	// The project namespace UUID is the first half of the derivation, so
	// anybody holding the URL can recompute it and check the arithmetic.
	fmt.Println(dfcad.DeriveGlobalIDNamespace(project.GlobalIDNamespace))
	fmt.Println(globalID)

	// Renaming the room would change its label and nothing here.
	fmt.Println(globalID == dfcad.DeriveGlobalID(project.GlobalIDNamespace, "site:S-101"))

	// Output:
	// bf22703b-ecd8-5c1f-929c-021883f35524
	// 2GX9NtsjvT$PykCkbFuEnE
	// true
}

func ExampleNodes_Node() {
	registry, _ := dfcad.LoadRegistry("testdata/node/valid")

	nodes, _ := dfcad.LoadNodes("testdata/node/valid", registry)

	// Lookup is by index rather than by a scan: everything above this layer
	// resolves references by id, and a scan apiece would make resolving a model
	// quadratic in its size.
	room, ok := nodes.Node("site:S-101")
	if !ok {
		fmt.Println("no such node")
		return
	}

	// The label is display text. Changing it would change this line and nothing
	// else about the node — the id it is found by least of all.
	fmt.Printf("%s: %s, a %s\n", room.ID(), room.Label(), room.Kind())

	// Output:
	// site:S-101: Meeting Room B, a Space
}

func ExampleNodes_Zones() {
	registry, _ := dfcad.LoadRegistry("testdata/node/containment")
	nodes, _ := dfcad.LoadNodes("testdata/node/containment", registry)

	partition, ok := nodes.Node("site:E-01")
	if !ok {
		fmt.Println("no such node")
		return
	}

	// The wall is inside exactly one thing and belongs to three zones which
	// overlap it. Every result says which relation produced it, so "is inside"
	// and "is a member of" can never be read as each other.
	if parent, ok := nodes.Within(partition); ok {
		fmt.Printf("%s %s\n", parent.Relation(), parent.Node().ID())
	}

	for zone := range nodes.Zones(partition) {
		fmt.Printf("%s %s\n", zone.Relation(), zone.Node().ID())
	}

	// Output:
	// containment site:L-01
	// membership site:Z-fire
	// membership site:Z-therm
	// membership site:Z-maint
}

func ExampleClaims_Resolve() {
	registry, _ := dfcad.LoadRegistry("testdata/claim/valid")
	claims, _ := dfcad.LoadClaims("testdata/claim/valid", registry)

	// Two claims disagree about how wide the room is. Which of them is current
	// is one stated rule rather than whichever file happened to load first:
	// accuracy decides it, and recency only breaks a tie, so a dimension
	// scaled off a plan does not beat a survey shot by being newer.
	resolution, err := claims.Resolve("site:S-101", "width", registry)
	if err != nil {
		fmt.Println(err)
		return
	}

	claim, ok := resolution.Claim()
	if !ok {
		fmt.Println("nothing rankable is claimed")
		return
	}

	width, _ := claim.Value().Scalar()
	fmt.Printf("%g %s, %s\n", width, claim.Value().Unit(), claim.Source())

	// The answer names the claim it came from. This one wrote no id of its own
	// — a claim needs a name only where something references it — so what
	// traces it back is where it was written.
	if id, wrote := resolution.ClaimID(); wrote {
		fmt.Println(id)
	} else {
		fmt.Println(claim.Span().Start)
	}

	// Output:
	// 8.53 m, As-built check AB-2026-009, Acme Surveys
	// testdata/claim/valid/claims.dfc:18:3
}

// ExampleClaims_Resolve_ambiguous is the other half of the rule: where nothing
// separates two claims, the engine says so instead of picking one.
func ExampleClaims_Resolve_ambiguous() {
	registry, _ := dfcad.LoadRegistry("testdata/claim/strict")
	claims, _ := dfcad.LoadClaims("testdata/claim/strict", registry)

	// Equally good, equally recent, and they disagree. That is a state of the
	// measurements rather than a mistake in the file, so both come back.
	resolution, err := claims.Resolve("site:S-101", "width", registry)
	fmt.Println(err, resolution.Ambiguous())
	for _, candidate := range resolution.Candidates() {
		id, _ := candidate.ID()
		value, _ := candidate.Value().Scalar()
		fmt.Printf("%s claims %g %s\n", id, value, candidate.Value().Unit())
	}

	// A predicate the registry declares strict escalates the same ambiguity to
	// a failure, because for some quantities no answer is safer than an
	// arbitrary one. The tied claims come back with the error rather than only
	// a count of them.
	_, err = claims.Resolve("site:S-101", "bearing", registry)

	var ambiguous dfcad.AmbiguousResolutionError
	if errors.As(err, &ambiguous) {
		fmt.Println(err)
		for _, candidate := range ambiguous.Candidates {
			fmt.Println(candidate.Span().Start)
		}
	}

	// Output:
	// <nil> true
	// survey:C-0312 claims 8.5 m
	// survey:C-0313 claims 8.53 m
	// expected one current bearing of site:S-101, found 2 equally current: bearing is declared strict
	// testdata/claim/strict/claims.dfc:10:3
	// testdata/claim/strict/claims.dfc:17:3
}

// ExampleClaims_Conflicts walks the conflict register: every subject and
// predicate the model states more than once, computed rather than recorded.
func ExampleClaims_Conflicts() {
	registry, _ := dfcad.LoadRegistry("testdata/claim/valid")
	claims, _ := dfcad.LoadClaims("testdata/claim/valid", registry)

	// The room's width is claimed twice, so the pair is in the register. The
	// vertex's position is claimed twice as well and is not: one of those two is
	// deprecated, which retracts it rather than out-ranking it, and that is the
	// only thing which silences a conflict.
	for conflict := range claims.Conflicts() {
		fmt.Printf("%s %s\n", conflict.Subject(), conflict.Predicate())

		for _, claim := range conflict.Claims() {
			value, _ := claim.Value().Scalar()
			fmt.Printf("  %g %s — %s\n", value, claim.Value().Unit(), claim.Source())
		}

		// The register says whether the disagreement has an answer. Having one
		// does not close it: both claims are still asserted, and the pair stays
		// here until one of them is deprecated or corrected.
		winner, resolved := conflict.Resolution().Claim()
		if !resolved {
			fmt.Println("  ambiguous")
			continue
		}

		value, _ := winner.Value().Scalar()
		fmt.Printf("  resolves to %g %s\n", value, winner.Value().Unit())
	}

	// Output:
	// site:S-101 width
	//   8.5 m — Plan set A-101, sheet 3
	//   8.53 m — As-built check AB-2026-009, Acme Surveys
	//   resolves to 8.53 m
}

// ExampleClaims_History traces a current value back through everything it
// replaced, which is what deprecating a claim rather than deleting one leaves
// behind.
func ExampleClaims_History() {
	registry, _ := dfcad.LoadRegistry("testdata/claim/supersession")
	claims, _ := dfcad.LoadClaims("testdata/claim/supersession", registry)

	// Where the vertex is now: three claims were made about it and two of them
	// were retracted, so resolution has one to choose from.
	resolution, _ := claims.Resolve("geom:V-02", "position", registry)

	current, ok := resolution.Claim()
	if !ok {
		fmt.Println("nothing rankable is claimed")
		return
	}

	fmt.Printf("%s — %s\n", mustID(current), current.Source())

	// And why it says that: each claim it stands in place of, nearest first.
	// Every one of them is still in the file, saying what was believed and on
	// what evidence.
	for replaced := range claims.History(current) {
		fmt.Printf("  replaced %s — %s, %s\n", mustID(replaced), replaced.Source(), replaced.Date().Format(time.DateOnly))
	}

	// The chain is walkable in the other direction too, which is how a value
	// read out of a superseded report is followed forward to what the model
	// says now.
	oldest, _ := claims.Claim("survey:C-0100")
	reached, _ := claims.Current(oldest)
	fmt.Printf("%s is now %s\n", mustID(oldest), mustID(reached))

	// Output:
	// survey:C-0181 — As-built check AB-2026-009, Acme Surveys
	//   replaced survey:C-0104 — Interior control set IC-01, Acme Surveys, 2026-02-18
	//   replaced survey:C-0100 — Plan set A-101, sheet 3, 2026-01-09
	// survey:C-0100 is now survey:C-0181
}

// mustID names a claim by the id it wrote, which every claim of a supersession
// writes: a claim something references is found by the name it wrote.
func mustID(claim *dfcad.Claim) dfcad.ID {
	id, _ := claim.ID()
	return id
}

// ExampleResolveBoundaries follows a wall which two rooms share, from each of
// them and from the wall.
//
// Neither room holds a coordinate. Each references a loop, the loops reference
// edges, and the edge between the rooms is named by both — so it is one node
// with one identity, moving it moves both rooms, and a sliver gap between them
// is not a state the model can express.
func ExampleResolveBoundaries() {
	root := "testdata/boundary/valid"

	registry, _ := dfcad.LoadRegistry(root)
	nodes, _ := dfcad.LoadNodes(root, registry)
	topology, _ := dfcad.LoadTopology(root, registry)

	// The two families are loaded separately and joined here, because a
	// `boundary` is written on a semantic node and names a loop: no pass which
	// has read one family can resolve it.
	boundaries, _ := dfcad.ResolveBoundaries(nodes, topology)

	room, _ := nodes.Node("site:S-101")
	for edge := range boundaries.Edges(room) {
		fmt.Printf("%s is bounded by %s\n", room.ID(), edge.ID())
	}

	// And the same question from the other end, which is written nowhere and is
	// what makes the sharing visible from the wall.
	partition, _ := topology.Edge("geom:E-02")
	for region := range boundaries.Regions(partition) {
		fmt.Printf("%s bounds %s\n", partition.ID(), region.ID())
	}

	// Output:
	// site:S-101 is bounded by geom:E-01
	// site:S-101 is bounded by geom:E-02
	// site:S-101 is bounded by geom:E-03
	// site:S-101 is bounded by geom:E-04
	// geom:E-02 bounds site:S-101
	// geom:E-02 bounds site:S-102
}

// ExampleBoundaries_Classify asks what actually separates a region from what is
// on the other side of each edge of its boundary.
//
// An edge which names an element the model holds is a physical boundary; one
// which names none is virtual — the open line between a foyer and a dining room.
// The answer is computed from the reference every time it is asked and is stored
// nowhere: nothing in the file says "physical", so adding a wall makes the
// boundary physical with no second edit.
func ExampleBoundaries_Classify() {
	root := "testdata/boundary/backed"

	registry, _ := dfcad.LoadRegistry(root)
	nodes, _ := dfcad.LoadNodes(root, registry)
	topology, _ := dfcad.LoadTopology(root, registry)

	boundaries, _ := dfcad.ResolveBoundaries(nodes, topology)

	room, _ := nodes.Node("site:S-101")
	for boundary := range boundaries.Classify(room) {
		// Every element is named rather than only the first. A stud wall with a
		// glazed screen over it is two things, and naming one of them would
		// answer with half the wall.
		var backing []string
		for _, element := range boundary.Backing() {
			backing = append(backing, string(element.ID()))
		}

		fmt.Printf("%s is %s", boundary.Edge().ID(), boundary.Classification())
		if len(backing) > 0 {
			fmt.Printf(", backed by %s", strings.Join(backing, " and "))
		}
		fmt.Println()
	}

	// Output:
	// geom:E-01 is virtual
	// geom:E-02 is physical, backed by site:W-14 and site:W-15
	// geom:E-03 is virtual
	// geom:E-04 is virtual
}

// ExampleTopology_Assemble reads a loop as the ring its edges traverse and says
// whether it closes, against a tolerance the registry declares by name.
//
// The answer is computed and never stored: adding an edge changes it with no
// other edit, and a recorded answer would be stale the moment a corner moved.
func ExampleTopology_Assemble() {
	root := "testdata/boundary/valid"

	registry, _ := dfcad.LoadRegistry(root)
	topology, _ := dfcad.LoadTopology(root, registry)
	claims, _ := dfcad.LoadClaims(root, registry)

	// Which predicate carries a position is vocabulary this repository owns, so
	// the positions are resolved here and handed in rather than looked up by a
	// name the engine would have to know.
	positions := make(dfcad.Positions)
	for vertex := range topology.Vertices() {
		resolution, _ := claims.Resolve(vertex.ID(), "position", registry)
		if value, ok := resolution.Value(); ok {
			positions[vertex.ID()] = value
		}
	}

	loop, _ := topology.Loop("geom:L-02")

	assembly, _ := topology.Assemble(loop, positions, "boundary-closure", registry)

	fmt.Printf("%s closes: %t, judged against %s (%v %s)\n",
		loop.ID(), assembly.Closed(), assembly.Tolerance().Name, assembly.Tolerance().Value, assembly.Tolerance().Unit)

	// The corridor runs through the shared partition against the order that
	// edge was written in, because the room on the other side runs through it
	// the other way. One edge, one identity, a direction per traversal.
	for _, step := range assembly.Steps() {
		fmt.Printf("  %s: %s to %s, reversed: %t\n", step.Edge().ID(), step.From(), step.To(), step.Reversed())
	}

	// Output:
	// geom:L-02 closes: true, judged against boundary-closure (0.005 m)
	//   geom:E-05: geom:V-02 to geom:V-05, reversed: false
	//   geom:E-06: geom:V-05 to geom:V-06, reversed: false
	//   geom:E-07: geom:V-06 to geom:V-03, reversed: false
	//   geom:E-02: geom:V-03 to geom:V-02, reversed: true
}

// ExampleResolveFrames relates an indoor floor plan to the survey it sits on,
// and says how well that relationship is actually known.
//
// A shape lives in exactly one frame and is transformed on demand. Storing one
// corner in two frames is two sources of truth which drift the moment the
// georeference is re-fitted — and the relationship between the frames is not a
// configuration constant either. It is a fit, produced by a method on a date
// with an accuracy, so it is a claim like every other measurement in the model.
func ExampleResolveFrames() {
	root := "testdata/frame/valid"

	registry, _ := dfcad.LoadRegistry(root)
	claims, _ := dfcad.LoadClaims(root, registry)

	// The frames are registry data and the transforms between them are claims,
	// so relating the two is a pass of its own.
	frames, _ := dfcad.ResolveFrames(registry, claims)

	// The building is modelled in millimetres and the survey grid is in metres.
	// Neither number is converted where it was written; the conversion happens
	// here, through the linear unit each frame declares.
	corner := dfcad.Point{3000.0, 4000.0, 0.0}

	on, err := frames.TransformPoint(corner, "frame:building", "frame:survey-grid")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%.1f mm becomes %.3f m\n", corner, on)

	// And the evidence for the step which put it there, which is the whole
	// reason the transform is a claim: a cross-frame answer can say how well the
	// relationship it was computed through is known.
	measurement, _ := frames.Measurement("frame:site")
	accuracy, _ := measurement.Accuracy()

	fmt.Printf("%s, by %s on %s, +/- %g %s\n",
		measurement.Source(), measurement.Method(),
		measurement.Date().Format("2006-01-02"),
		accuracy.Terms[0].Magnitude, accuracy.Terms[0].Unit)

	// Output:
	// [3000.0 4000.0 0.0] mm becomes [113.000 224.000 0.000] m
	// Georeferencing report GR-2026-002, Acme Surveys, by method:gnss-static on 2026-02-11, +/- 0.012 m
}

func ExampleBudget() {
	root := "testdata/claim/valid"

	registry, _ := dfcad.LoadRegistry(root)
	claims, _ := dfcad.LoadClaims(root, registry)

	// Two facts, each resolved to the claim which is current. Both were shot
	// from the same control point, so both carry survey:CP-3.
	width, _ := claims.Resolve("site:S-101", "width", registry)
	position, _ := claims.Resolve("geom:V-02", "position", registry)

	var budget dfcad.Budget
	for _, resolution := range []dfcad.Resolution{width, position} {
		claim, _ := resolution.Claim()
		budget.Add(claim)
	}

	// The budget is itemised rather than a single number: which term dominates
	// is a more useful answer than the total, because it says what to
	// re-measure.
	for _, term := range budget.Terms() {
		fmt.Printf("%-11s %-14s %g %s, from %d claim(s)\n",
			term.Kind, term.Name, term.Magnitude, term.Unit, len(term.Contributors))
	}

	// The two independent terms combine in quadrature. The control point is one
	// systematic term shared by both facts, so it is counted once and added
	// linearly rather than being allowed to cancel against itself.
	combined, err := budget.Combined()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("combined %.6f %s at k = %g\n", combined.Magnitude, combined.Unit, combined.CoverageFactor)

	// Storage is always one standard deviation. Widening happens where the
	// figure is reported, and the factor travels with it.
	widened, _ := combined.Widen(2)
	fmt.Printf("reported %.6f %s at k = %g\n", widened.Magnitude, widened.Unit, widened.CoverageFactor)

	// Output:
	// independent the width of site:S-101 0.003 m, from 1 claim(s)
	// systematic  survey:CP-3    0.008 m, from 2 claim(s)
	// independent survey:C-0181  0.003 m, from 1 claim(s)
	// combined 0.009055 m at k = 1
	// reported 0.018111 m at k = 2
}

func ExampleBudget_Add_unknown() {
	root := "testdata/claim/valid"

	registry, _ := dfcad.LoadRegistry(root)
	claims, _ := dfcad.LoadClaims(root, registry)

	// The occupancy of the room is rated rather than measured, so it says
	// nothing about how well it is known.
	occupancy, _ := claims.Resolve("site:S-101", "occupancy", registry)

	var budget dfcad.Budget
	for _, claim := range occupancy.Candidates() {
		budget.Add(claim)
	}

	// An unstated accuracy is unknown and not zero. It taints the budget, and
	// no combined figure comes out of it at all — because a figure computed
	// from the inputs which did state one would be narrower than the truth
	// while looking exactly like it.
	fmt.Println("known:", budget.Known())

	if _, err := budget.Combined(); err != nil {
		var unknown dfcad.UnknownAccuracyError
		if errors.As(err, &unknown) {
			for _, claim := range unknown.Claims {
				fmt.Printf("no accuracy: %s of %s, %s\n",
					claim.Predicate(), claim.Subject(), claim.Source())
			}
		}
	}

	// Output:
	// known: false
	// no accuracy: occupancy of site:S-101, Fire strategy FS-2026-001
}

func ExampleFrames_TransformBudget() {
	root := "testdata/frame/valid"

	registry, _ := dfcad.LoadRegistry(root)
	claims, _ := dfcad.LoadClaims(root, registry)
	frames, _ := dfcad.ResolveFrames(registry, claims)

	// The route from the room to the annex climbs two fits and comes back down
	// through a third. Two of the three were fitted against the same control
	// point.
	budget, err := frames.TransformBudget("frame:room", "frame:annex")
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, term := range budget.Terms() {
		fmt.Printf("%-11s %-13s %g %s\n", term.Kind, term.Name, term.Magnitude, term.Unit)
	}

	combined, _ := budget.Combined()
	fmt.Printf("combined %.6f %s at k = %g\n", combined.Magnitude, combined.Unit, combined.CoverageFactor)

	dominant, _ := budget.Dominant()
	fmt.Printf("dominated by %s, shared by %d fits\n", dominant.Name, len(dominant.Contributors))

	// Output:
	// independent survey:C-0004 0.002 m
	// independent survey:C-0002 0.004 m
	// systematic  survey:CP-3   0.008 m
	// independent survey:C-0003 0.006 m
	// combined 0.010954 m at k = 1
	// dominated by survey:CP-3, shared by 2 fits
}
