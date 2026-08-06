// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad_test

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
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

func ExampleGraph_Nearest() {
	graph, _ := dfcad.LoadGraph("testdata/graph/valid")

	// An id which reaches nothing is usually the id which was meant with a
	// character wrong, so the answer to a failed lookup is what to try instead
	// rather than only that it failed.
	if _, ok := graph.Entity("site:S-1O1"); !ok {
		if nearest, close := graph.Nearest("site:S-1O1"); close {
			fmt.Println("did you mean", nearest)
		}
	}

	// A misspelling of a vertex is answered by the vertex: an id is unique
	// across the whole model, so the suggestion is not a question of family
	// either.
	fmt.Println(graph.Nearest("geom:V-O1"))

	// An id nothing in the model resembles gets no suggestion. One nobody meant
	// is worse than none: it sends the reader to change a line which was never
	// the problem.
	fmt.Println(graph.Nearest("other:nothing-like-it"))

	// Output:
	// did you mean site:S-101
	// geom:V-01 true
	//  false
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

func ExampleChecks() {
	// The check registry is closed and compiled into the engine, so this is the
	// whole set for every model: a command listing what an assertion may name
	// reads it here rather than out of a file.
	for _, check := range dfcad.Checks() {
		written := []string{check.Name}
		for _, parameter := range check.Parameters {
			written = append(written, fmt.Sprintf("(%s <%s>)", parameter.Name, parameter.Type))
		}
		fmt.Println(strings.Join(written, " "))
	}

	// Output:
	// boundary-loops-close (tolerance <tolerance>) (position <predicate>)
	// claim-agrees-with-geometry (predicate <predicate>) (position <predicate>) (tolerance <tolerance>) (discrepancy <tolerance>)
	// contained-areas-do-not-overlap (tolerance <tolerance>) (position <predicate>) (kind <kind>)
	// contained-areas-sum (tolerance <tolerance>) (area-tolerance <tolerance>) (position <predicate>) (kind <kind>)
	// cross-frame-budget-holds (frame <frame>) (limit <tolerance>)
	// edge-backing-resolves
	// edge-endpoints-differ
	// required-claim (predicate <predicate>)
	// stays-clear-of-zone (zone <id>) (tolerance <tolerance>) (position <predicate>)
	// within-resolves
	// zone-members-resolve
}

func ExampleGraph_Invariants() {
	graph, _ := dfcad.LoadGraph("testdata/invariant/valid")

	// An invariant is written once on the type and applies to every instance of
	// it, so what bears on one node is asked of the graph rather than read off
	// the node: nothing was copied onto it, and a room written after the rule
	// was declared carries it exactly as one written before.
	for node := range graph.Nodes().All() {
		for _, binding := range graph.Invariants(node) {
			fmt.Println(binding, "declared on", binding.Type)
		}
	}

	// The corridor's type declares no invariant, which is ordinary: nothing is
	// bound to it and nothing is printed about it. Nor does it inherit the
	// storey's, though it is written inside one.
	corridor, _ := graph.Node("site:S-201")
	fmt.Println("bound to the corridor:", len(graph.Invariants(corridor)))

	// Output:
	// site:S-103 required-claim (predicate width) declared on MeetingRoom
	// site:L-01 within-resolves declared on Level
	// site:Z-02 boundary-loops-close (tolerance boundary-closure) declared on OccupancyZone
	// site:S-101 required-claim (predicate width) declared on MeetingRoom
	// site:S-102 required-claim (predicate width) declared on MeetingRoom
	// bound to the corridor: 0
}

func ExampleGraph_Rules() {
	graph, _ := dfcad.LoadGraph("testdata/rules/valid")

	// A gate does not ask the two questions separately. Every rule the model
	// states comes back as one list, in the order it will run in: every
	// invariant, node by node in the order the model was read, and then every
	// assertion, thing by thing.
	for _, rule := range graph.Rules() {
		written := "assertion, written on it"
		if rule.Invariant() {
			written = "invariant of " + rule.Type
		}
		fmt.Printf("%s — %s\n", rule, written)
	}

	// Output:
	// site:Z-01 within-resolves — invariant of OccupancyZone
	// site:S-101 required-claim (predicate width) — invariant of MeetingRoom
	// site:S-102 required-claim (predicate width) — invariant of MeetingRoom
	// site:Z-01 required-claim (predicate width) — assertion, written on it
	// site:S-101 boundary-loops-close (tolerance boundary-closure) — assertion, written on it
	// geom:V-01 required-claim (predicate position) — assertion, written on it
	// geom:E-01 required-claim (predicate position) — assertion, written on it
}

func ExampleRules_Run() {
	graph, _ := dfcad.LoadGraph("testdata/rules/valid")

	// A gate somebody is iterating against runs one thing, one type or one
	// check rather than the model. Each filter can only take rules away, so the
	// answers compose the way a reader expects.
	rules := graph.Rules().Select(dfcad.RuleFilter{Types: []string{"MeetingRoom"}})
	for _, rule := range rules {
		fmt.Println(rule)
	}

	run := rules.Run()

	// Every check the engine registers declares what it constrains and takes,
	// and some of them have an implementation to run. So a run reports three
	// answers rather than two: the room references no loop, so the check which
	// reads its outline finds nothing to disagree with and passes, while the two
	// rules naming a check nothing implements decide nothing — which is not the
	// same answer as a rule which held.
	fmt.Println("rules:", run.Rules)
	fmt.Println("passed:", run.Passed)
	fmt.Println("undecided:", run.Rules-run.Ran)
	fmt.Println("failed:", run.Failed)

	// Output:
	// site:S-101 required-claim (predicate width)
	// site:S-102 required-claim (predicate width)
	// site:S-101 boundary-loops-close (tolerance boundary-closure)
	// rules: 3
	// passed: 1
	// undecided: 2
	// failed: 0
}

func ExampleRules_Run_structuralInvariants() {
	graph, _ := dfcad.LoadGraph("testdata/checks/violating")

	// Every failure below is a file which loads. A loop which does not close,
	// two rooms drawn over one another, parts which do not add up to the whole,
	// a part drawn in another frame, a fit too loose for the answer it is used
	// for and a room in a setback are all well-formed models, and nothing short
	// of running the rules finds any of them.
	for _, violation := range graph.Rules().Run().Violations {
		fmt.Printf("%s — %s\n", violation.Check, violation.Message)
	}

	// Output:
	// contained-areas-do-not-overlap — expected no two of the shapes within site:L-01 to cover the same ground, found site:S-101 and site:S-102 overlapping by 4.0 m²
	// contained-areas-sum — expected what site:L-01 contains to add up to its own 24.0 m², found 28.0 m², which is 4.0 m² more than the whole
	// stays-clear-of-zone — expected site:S-102 to stay clear of the zone site:Z-90, found it crossing into it over 4.0 m²
	// boundary-loops-close — expected the loop geom:L-13 to close, found a gap of 0.3 m between geom:V-13 and geom:V-09
	// contained-areas-sum — expected everything summed into site:L-05 to be declared in frame:building, the frame it is drawn in, found site:S-501 in frame:annex
	// boundary-loops-close — expected the loop geom:L-13 to close, found a gap between geom:V-13 and geom:V-09 whose size could not be measured
	// cross-frame-budget-holds — expected site:A-01 in frame:building to be known to within 0.008 m, found a combined uncertainty of 0.01 m (k = 1.0, ≈ 68%) accumulated from 2 terms
}

func ExampleGraph_Assertions() {
	graph, _ := dfcad.LoadGraph("testdata/assert/valid")

	// An assertion is written on the thing it constrains, so retrieving the
	// thing retrieves what has to hold of it. The claims say what the room
	// measures; these say what it may not stop measuring.
	room, _ := graph.Node("site:S-101")
	for _, assertion := range room.Assertions() {
		fmt.Println(assertion)
	}

	// Bound against the check registry, each one carries what the check it
	// names constrains — which is readable without running anything, because an
	// assertion is a name and its parameters and nothing else.
	for _, binding := range graph.Assertions(room) {
		fmt.Printf("%s: %s\n", binding.Check.Name, binding.Check.Description)
	}

	// Output:
	// within-resolves
	// required-claim (predicate width)
	// boundary-loops-close (tolerance boundary-closure)
	// within-resolves: The node the subject is written within is one the model holds, and the containment hierarchy permits it as a parent of the subject's kind.
	// required-claim: The subject carries a claim under the named predicate which is still asserted, so the predicate has a resolvable value on it.
	// boundary-loops-close: Every loop bounding the subject closes: traversing its edges returns to the vertex it started from, within the named tolerance.
}

func ExampleResolveAssertions() {
	source := `(node site:S-101
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (assert edge-endpoints-differ))
`

	dir, _ := os.MkdirTemp("", "dfcad")
	defer os.RemoveAll(dir)

	registry, _ := os.ReadFile("testdata/assert/valid/registry.dfc")
	_ = os.WriteFile(filepath.Join(dir, "registry.dfc"), registry, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "model.dfc"), []byte(source), 0o644)

	// A check declares what it can examine, and a check written on something it
	// cannot is refused when the model loads. It is not a rule which happens
	// not to fire: a check with nothing on its subject to look at passes on
	// every run forever.
	_, diags := dfcad.LoadGraph(dir)
	for _, diagnostic := range diags {
		fmt.Println(diagnostic.Message)
		fmt.Println("hint:", diagnostic.Hint)
	}

	// Output:
	// expected an assertion naming a check which applies to a node, found edge-endpoints-differ, which applies to edge
	// hint: an assertion is written on the thing the check examines, so edge-endpoints-differ is written on edge instead
}

func ExampleValidateAssertion() {
	const path = "entities/level-1.dfc"

	source := `(node site:S-101
  (kind Space)
  (type MeetingRoom)
  (assert boundary-loops-close (tolerance 0.005)))
`

	file, err := dfcad.Parse(path, strings.NewReader(source))
	if err != nil {
		fmt.Println(err)
		return
	}

	registry, _ := dfcad.LoadRegistry("testdata/registry/valid")

	// An assertion is validated against the check registry before anything runs
	// it: the check name is one the engine registers, and every parameter is
	// the sort of datum that check declares it takes.
	written := file.Nodes[0].Children[4]

	var diagnostics dfcad.Diagnostics
	diagnostics.Add(dfcad.ValidateAssertion(written, registry)...)

	if err := diagnostics.Render(os.Stdout, dfcad.Sources{path: []byte(source)}); err != nil {
		fmt.Println(err)
	}

	// Output:
	// entities/level-1.dfc:4:43: error: expected a declared tolerance name after the tolerance tag, found the number 0.005
	// 4 |   (assert boundary-loops-close (tolerance 0.005)))
	//   |                                           ^^^^^
	//   = hint: a tolerance is registry data rather than a number written where it is used: declare it with (tolerance <name> (value <magnitude> <unit>)) and name it here, so that how close is close enough is one decision in one place
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

	// And which step of the rule picked it. An answer which cannot say why it
	// is the answer is a bare number again: "the most accurate of two claims"
	// says which claim to go and read, where the value alone invites a
	// re-measurement nobody needed.
	fmt.Println(resolution.Reason())

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
	// accuracy
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
	fmt.Println(err, resolution.Ambiguous(), resolution.Reason())
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
	// <nil> true ambiguous
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

// ExampleBoundaries_Adjacent answers what borders what, which is read from the
// edges two regions share rather than from how close their outlines are.
func ExampleBoundaries_Adjacent() {
	root := "testdata/boundary/adjacent"

	registry, _ := dfcad.LoadRegistry(root)
	nodes, _ := dfcad.LoadNodes(root, registry)
	topology, _ := dfcad.LoadTopology(root, registry)

	boundaries, _ := dfcad.ResolveBoundaries(nodes, topology)

	room, _ := nodes.Node("site:S-A")

	// Every edge the two of them share is named. One is a partition and the
	// other is the doorway through it, which is the difference between what
	// separates two rooms and how anybody gets between them.
	for neighbour := range boundaries.Adjacent(room) {
		for _, edge := range neighbour.Via() {
			fmt.Printf("%s borders %s across %s, which is %s\n",
				room.ID(), neighbour.Node().ID(), edge.ID(), boundaries.Classified(edge).Classification())
		}
	}

	// Room C shares no edge with room A, so it is two steps away rather than
	// one, however close the two of them look on a plan.
	for neighbour := range boundaries.AdjacentTo(room, dfcad.Unbounded) {
		fmt.Printf("%s is %d away\n", neighbour.Node().ID(), neighbour.Depth())
	}

	// Output:
	// site:S-A borders site:S-B across geom:E-02, which is physical
	// site:S-A borders site:S-B across geom:E-03, which is virtual
	// site:S-B is 1 away
	// site:S-C is 2 away
}

// ExampleNodes_DescendantsTo walks containment with a bound, which is what makes
// a traversal of a model nobody has read an answer of a known size.
func ExampleNodes_DescendantsTo() {
	root := "testdata/node/containment"

	registry, _ := dfcad.LoadRegistry(root)
	nodes, _ := dfcad.LoadNodes(root, registry)

	site, _ := nodes.Node("site:S-01")

	// Level by level, so a bound of two is the two levels which were asked for.
	for contained := range nodes.DescendantsTo(site, 2) {
		fmt.Printf("%s %s at %d\n", contained.Relation(), contained.Node().ID(), contained.Depth())
	}

	// And the other relation, which the walk above never follows: the zones a
	// node is grouped into say nothing about where it is, so nothing inside the
	// site is reached through one.
	partition, _ := nodes.Node("site:E-01")
	for zone := range nodes.ZonesTo(partition, dfcad.Unbounded) {
		fmt.Printf("%s %s at %d\n", zone.Relation(), zone.Node().ID(), zone.Depth())
	}

	// Output:
	// containment site:B-01 at 1
	// containment site:L-01 at 2
	// membership site:Z-fire at 1
	// membership site:Z-therm at 1
	// membership site:Z-maint at 1
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

// ExampleTopology_MeasureRegion answers how big a room is from the boundary it
// references, rather than from a number written down beside it.
//
// An area recorded in the model is a second source of truth. It is right on the
// day somebody typed it and wrong the first time a wall moves, and nothing in the
// file says which of those it currently is. Computing it means the question
// cannot be answered wrongly: the corners are the only thing stated, and the
// answer is derived from them every time it is asked.
func ExampleTopology_MeasureRegion() {
	root := "testdata/measure/courtyard"

	registry, _ := dfcad.LoadRegistry(root)
	nodes, _ := dfcad.LoadNodes(root, registry)
	topology, _ := dfcad.LoadTopology(root, registry)
	claims, _ := dfcad.LoadClaims(root, registry)
	boundaries, _ := dfcad.ResolveBoundaries(nodes, topology)

	// Which predicate carries a position is vocabulary this repository owns, so
	// the positions are resolved here and handed in. Place fills the claim behind
	// each one at the same time, which is what lets the answer carry a budget.
	survey := dfcad.Survey{Tolerance: "boundary-closure", Registry: registry}
	for vertex := range topology.Vertices() {
		resolution, _ := claims.Resolve(vertex.ID(), "position", registry)
		survey.Place(vertex.ID(), resolution)
	}

	plate, _ := nodes.Node("site:S-01")

	measurement, _ := topology.MeasureRegion(plate, boundaries, survey)

	// Ten metres square, less a four-metre courtyard in the middle of it.
	// Nothing in the model says which of the two loops is the hole: the one
	// inside the other is, and it is taken away.
	area, _ := measurement.Area()
	length, _ := measurement.Length()
	fmt.Printf("%s: %g %s², bounded by %g %s of wall\n", measurement.Subject(), area, measurement.Unit(), length, measurement.Unit())

	// And how well that is known, which is the whole reason a position is a
	// claim: the tie to the control point is one error shared by every corner,
	// so it is counted once instead of eight times.
	combined, _ := measurement.Budget().Combined()
	dominant, _ := measurement.Budget().Dominant()
	fmt.Printf("corners known to %s, mostly %s\n", combined, dominant.Name)

	// Output:
	// site:S-01: 84 m², bounded by 56 m of wall
	// corners known to 0.013856406460551019 m (k = 1.0, ≈ 68%), mostly control:CP-3
}

// ExampleGraph_Measure asks how big something is without first working out which
// family the id names.
//
// It is the same measurement [Topology.MeasureRegion] gives above, reached
// through the one call a consumer actually wants: a room, its outline, one of its
// walls and one of its corners are all measurable, and which of the four
// measurements applies is a property of the model rather than of the question.
// [Graph.Corners] is the other half — the corners a survey has to carry for the
// answer to rest on all of them.
func ExampleGraph_Measure() {
	graph, _ := dfcad.LoadGraph("testdata/measure/courtyard")

	// Which predicate carries a position is vocabulary this repository owns, so
	// the positions are resolved here and handed in. Corners says which vertices
	// the answer needs, so nothing has to guess at the ones it rests on.
	plate, _ := graph.Entity("site:S-01")

	survey := dfcad.Survey{Tolerance: "boundary-closure", Registry: graph.Registry()}
	for vertex := range graph.Corners(plate) {
		resolution, _ := graph.Claims().Resolve(vertex.ID(), "position", graph.Registry())
		survey.Place(vertex.ID(), resolution)
	}

	measurement, _ := graph.Measure(plate, survey)
	fmt.Println(measurement)

	// The same call answers for a wall and for a corner. An edge encloses
	// nothing and a vertex has no extent, and neither comes back as a zero.
	wall, _ := graph.Entity("geom:E-01")
	corner, _ := graph.Entity("geom:V-01")

	for _, subject := range []dfcad.Entity{wall, corner} {
		measured, _ := graph.Measure(subject, survey)
		fmt.Println(measured)
	}

	// Output:
	// site:S-01: area 84.0 m², length 56.0 m, centroid (5.0 5.0 0.0), bounds (0.0 0.0 0.0) to (10.0 10.0 0.0)
	// geom:E-01: length 10.0 m, centroid (5.0 0.0 0.0), bounds (0.0 0.0 0.0) to (10.0 0.0 0.0)
	// geom:V-01: centroid (0.0 0.0 0.0), bounds (0.0 0.0 0.0) to (0.0 0.0 0.0)
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

func ExampleTx() {
	// A transaction writes, so this works on a copy of the fixture rather than
	// on the fixture.
	root, err := os.MkdirTemp("", "dfcad")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	if err := os.CopyFS(root, os.DirFS("testdata/graph/valid")); err != nil {
		fmt.Println(err)
		return
	}

	// Begin reads the whole model and holds the root until the transaction
	// finishes. A tree which does not already load is refused here, before
	// anything is asked of it, so a refusal later is about the change.
	tx, diags, err := dfcad.Begin(root)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, diagnostic := range diags {
		fmt.Println(diagnostic)
	}
	defer tx.Close()

	// Every step of the change except the writing, which is what a --dry-run
	// on a write command sets.
	tx.DryRun = true

	form, err := dfcad.Parse("", strings.NewReader(
		`(node site:S-103 (kind Space) (type MeetingRoom) (label "Meeting Room C") (geometry area) (frame frame:building) (within site:L-01))`,
	))
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := tx.Insert("entities/site.dfc", form.Nodes[0]); err != nil {
		fmt.Println(err)
		return
	}

	// Committing interprets the model as it would be once written. Nothing
	// reaches the disk unless that model loads.
	commit, refused, err := tx.Commit()
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, diagnostic := range refused {
		fmt.Println(diagnostic)
	}

	// What the change did to the model, and what it would have done to the
	// files.
	for _, effect := range commit.Effects() {
		fmt.Printf("%s %s %s\n", effect.Op, effect.Tag, effect.ID)
	}
	for _, file := range commit.Files {
		fmt.Printf("%s %s\n", filepath.Base(file.Path), file.Status)
	}

	// Output:
	// created node site:S-103
	// site.dfc rewritten
}

func ExampleTx_refused() {
	root, err := os.MkdirTemp("", "dfcad")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	if err := os.CopyFS(root, os.DirFS("testdata/graph/valid")); err != nil {
		fmt.Println(err)
		return
	}

	tx, _, err := dfcad.Begin(root)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tx.Close()

	// An id the model already holds. Nothing about the form itself is wrong,
	// which is why only interpreting the whole model finds it.
	form, err := dfcad.Parse("", strings.NewReader(
		`(node site:S-101 (kind Space) (type MeetingRoom) (geometry area) (frame frame:building))`,
	))
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := tx.Insert("entities/site.dfc", form.Nodes[0]); err != nil {
		fmt.Println(err)
		return
	}

	commit, refused, err := tx.Commit()
	if err != nil {
		fmt.Println(err)
		return
	}

	// The change is refused with the diagnostics the load would have raised,
	// and the tree is exactly as it was: no file was written, and there is
	// nothing to reconcile before trying again.
	for _, diagnostic := range refused {
		fmt.Println(diagnostic.Message)
		fmt.Println(diagnostic.Hint)
	}
	fmt.Println(len(commit.Files), "files changed")

	// Output:
	// expected an id nothing else holds, found site:S-101, which already names something in this model
	// an id is unique across the whole model, and is never issued again to a different thing
	// 0 files changed
}

// ExampleRegistry_Destination routes a new node to the file the registry says
// it belongs in, and shows what happens to one the rules do not cover.
func ExampleRegistry_Destination() {
	registry, diags := dfcad.LoadRegistry("testdata/graph/valid")
	for _, diagnostic := range diags {
		fmt.Println(diagnostic)
	}

	// A node the rules place: one rule matches it, and the destination says
	// which, so that "where did this go" and "why there" are one answer.
	destination, err := registry.Destination(dfcad.Subject{
		ID:   "site:S-104",
		Kind: dfcad.KindSpace,
		Type: "MeetingRoom",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(destination.Path, "by rule", destination.Rule)

	// A geometric node carries neither a kind nor a type, so it is routed by a
	// rule which matches on its namespace alone.
	geometry, err := registry.Destination(dfcad.Subject{ID: "geom:V-07"})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(geometry.Path, "by rule", geometry.Rule)

	// A node no rule covers is refused rather than filed somewhere plausible.
	// The refusal names it and every rule there was, because the fix is a rule
	// and a fix nobody is told to make is one nobody makes.
	_, err = registry.Destination(dfcad.Subject{
		ID:   "site:P-01",
		Kind: dfcad.KindElement,
		Type: "Partition",
	})

	var unplaced dfcad.RoutingError
	if errors.As(err, &unplaced) {
		fmt.Println("ambiguous:", unplaced.Ambiguous())
		for _, rule := range unplaced.Consulted {
			fmt.Println(" consulted", rule.Name, "->", rule.File)
		}
	}

	// Output:
	// entities/site.dfc by rule rooms
	// entities/geometry.dfc by rule geometry
	// ambiguous: false
	//  consulted corridors -> entities/circulation.dfc
	//  consulted geometry -> entities/geometry.dfc
	//  consulted rooms -> entities/site.dfc
}

// ExampleOverride takes a destination the caller named outright, which is what
// a write command's --file flag does with what it was given.
func ExampleOverride() {
	destination, err := dfcad.Override("entities/annexe.dfc")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(destination.Path, "overridden:", destination.Overridden, "rule:", destination.Rule == "")

	// A file no walk of the model reaches is refused: writing into one is a
	// change which appears to have been made and was not.
	_, err = dfcad.Override("notes.md")
	fmt.Println(err)
	fmt.Println(errors.Is(err, dfcad.ErrNotAnEntityFile))

	// Output:
	// entities/annexe.dfc overridden: true rule: true
	// notes.md: not an entity file
	// true
}

// ExampleTx_AddNode writes a new semantic node into the file the registry's
// routing rules choose for it.
func ExampleTx_AddNode() {
	root, err := os.MkdirTemp("", "dfcad")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	if err := os.CopyFS(root, os.DirFS("testdata/graph/valid")); err != nil {
		fmt.Println(err)
		return
	}

	tx, _, err := dfcad.Begin(root)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tx.Close()

	spec := dfcad.NodeSpec{
		ID:       "site:S-103",
		Kind:     dfcad.KindSpace,
		Type:     "MeetingRoom",
		Geometry: dfcad.GeometryArea,
		Frame:    "frame:building",
		Label:    "Meeting Room C",
	}

	// Where it goes is the registry's decision, asked the same way `dfcad
	// route` asks it.
	destination, err := tx.Graph().Registry().Destination(dfcad.Subject{
		ID:   spec.ID,
		Kind: spec.Kind,
		Type: spec.Type,
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	if err := tx.AddNode(spec, destination.Path); err != nil {
		fmt.Println(err)
		return
	}

	commit, refused, err := tx.Commit()
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, diagnostic := range refused {
		fmt.Println(diagnostic)
	}

	fmt.Println(destination.Path, "by rule", destination.Rule)
	for _, effect := range commit.Effects() {
		fmt.Printf("%s %s %s\n", effect.Op, effect.Tag, effect.ID)
	}

	// Output:
	// entities/site.dfc by rule rooms
	// created node site:S-103
}

// ExampleTx_AddNode_taken refuses an id something already holds, which a
// retired id is as much as a live one: retiring says the thing stopped
// existing, not that its name came free.
func ExampleTx_AddNode_taken() {
	root, err := os.MkdirTemp("", "dfcad")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	if err := os.CopyFS(root, os.DirFS("testdata/graph/valid")); err != nil {
		fmt.Println(err)
		return
	}

	tx, _, err := dfcad.Begin(root)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tx.Close()

	err = tx.AddNode(dfcad.NodeSpec{
		ID:       "site:S-101",
		Kind:     dfcad.KindSpace,
		Type:     "MeetingRoom",
		Geometry: dfcad.GeometryArea,
		Frame:    "frame:building",
	}, "entities/site.dfc")

	var taken dfcad.TakenIDError
	if errors.As(err, &taken) {
		fmt.Println(taken.ID, "already names", taken.What)
		fmt.Println("retired:", taken.Retired)
	}

	// Output:
	// site:S-101 already names a node
	// retired: false
}

// ExampleTx_Retire records that a thing stopped existing, and moves what
// referenced it onto the thing which replaced it.
func ExampleTx_Retire() {
	root, err := os.MkdirTemp("", "dfcad")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	if err := os.CopyFS(root, os.DirFS("testdata/graph/valid")); err != nil {
		fmt.Println(err)
		return
	}

	tx, _, err := dfcad.Begin(root)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tx.Close()

	// Without a replacement this is refused, naming the partition written
	// inside the room: a reference to something which says it stopped existing
	// is a question the model cannot answer.
	err = tx.Retire("site:S-101", dfcad.RetirementSpec{
		Date:         time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Reason:       "Knocked through into the room beside it.",
		SupersededBy: "site:S-102",
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	commit, refused, err := tx.Commit()
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, diagnostic := range refused {
		fmt.Println(diagnostic)
	}

	for _, effect := range commit.Effects() {
		fmt.Printf("%s %s %s\n", effect.Op, effect.Tag, effect.ID)
	}

	// The retired node is still a node the model holds, and says what happened
	// to it.
	graph, _ := dfcad.LoadGraph(root)

	node, _ := graph.Node("site:S-101")
	retirement, _ := node.Retirement()
	replacement, _ := retirement.SupersededBy()
	fmt.Println(node.Label(), "retired on", retirement.Date().Format(time.DateOnly), "for", replacement)

	// And the partition is inside the room which replaced it.
	partition, _ := graph.Node("site:E-01")
	within, _ := partition.Within()
	fmt.Println("site:E-01 is within", within)

	// Output:
	// modified node site:E-01
	// modified node site:S-101
	// Meeting Room B retired on 2026-06-01 for site:S-102
	// site:E-01 is within site:S-102
}

// ExampleGraph_References says what points at one thing, which is what a
// refusal to retire it reports.
func ExampleGraph_References() {
	graph, _ := dfcad.LoadGraph("testdata/graph/valid")

	for reference := range graph.References("site:S-101") {
		fmt.Println(reference.From, reference.Relation)
	}

	// Output:
	// site:E-01 within
}

// ExampleTx_AddClaim attaches a measured value to a thing, with the evidence
// that makes it a claim rather than a number.
func ExampleTx_AddClaim() {
	root, err := os.MkdirTemp("", "dfcad")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	if err := os.CopyFS(root, os.DirFS("testdata/graph/valid")); err != nil {
		fmt.Println(err)
		return
	}

	tx, _, err := dfcad.Begin(root)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tx.Close()

	// The partition is already measured twice, so a third measurement is a
	// third opinion. That is the normal case rather than an error, and the
	// notice says so rather than the change being refused.
	id, notices, err := tx.AddClaim(dfcad.ClaimSpec{
		Subject:   "site:E-01",
		Predicate: "width",
		Value:     dfcad.ScalarValue(0.103, "m"),
		Source:    "Fit-out check FC-2026-004, Acme Surveys",
		Method:    "method:total-station",
		Accuracy:  []dfcad.AccuracyTerm{{Kind: dfcad.TermIndependent, Magnitude: 0.003, Unit: "m"}},
		Date:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	if _, _, err := tx.Commit(); err != nil {
		fmt.Println(err)
		return
	}

	// A claim nothing references writes no id of its own.
	fmt.Printf("claim id %q\n", id)

	for _, notice := range notices {
		fmt.Println(notice.Kind, "with", len(notice.Competing), "claims already written")
	}

	// Output:
	// claim id ""
	// conflict with 2 claims already written
}

// ExampleTx_AddClaim_unrankable writes the least a claim may say. Leaving the
// accuracy out is permitted and is the one escape hatch the bare-scalar rule
// keeps open; what it produces is a claim which can never win resolution.
func ExampleTx_AddClaim_unrankable() {
	root, err := os.MkdirTemp("", "dfcad")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	if err := os.CopyFS(root, os.DirFS("testdata/graph/valid")); err != nil {
		fmt.Println(err)
		return
	}

	tx, _, err := dfcad.Begin(root)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tx.Close()

	_, notices, err := tx.AddClaim(dfcad.ClaimSpec{
		Subject:   "site:S-102",
		Predicate: "width",
		Value:     dfcad.ScalarValue(1.8, "m"),
		Source:    "Plan set A-101, sheet 3",
		Method:    "method:scaled-from-plan",
		Date:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	if _, _, err := tx.Commit(); err != nil {
		fmt.Println(err)
		return
	}

	for _, notice := range notices {
		fmt.Println(notice.Kind)
	}

	graph, _ := dfcad.LoadGraph(root)

	for claim := range graph.Claims().Under("site:S-102", "width") {
		fmt.Println("rankable:", claim.Rankable())
	}

	// Output:
	// unrankable
	// rankable: false
}

// ExampleTx_Supersede corrects a value. The new claim is written and the claim
// it replaces is retracted in its favour, in one change which lands completely
// or not at all — and the old claim keeps everything it said, because a
// correction is a record of why the number changed rather than a new number in
// place of the old one.
func ExampleTx_Supersede() {
	root, err := os.MkdirTemp("", "dfcad")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	if err := os.CopyFS(root, os.DirFS("testdata/graph/valid")); err != nil {
		fmt.Println(err)
		return
	}

	tx, _, err := dfcad.Begin(root)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tx.Close()

	// The claim being corrected is named by its subject and predicate, because
	// the one it replaces wrote no id — an id is required only of a claim
	// something references, and this one is about to be referenced for the
	// first time.
	id, _, err := tx.Supersede(dfcad.ClaimSpec{
		Subject:   "geom:V-01",
		Predicate: "position",
		Value:     dfcad.CoordinateValue([]float64{0.001, 0.002, 0}, "m"),
		Source:    "Interior control set IC-02, Acme Surveys",
		Method:    "method:total-station",
		Accuracy:  []dfcad.AccuracyTerm{{Kind: dfcad.TermIndependent, Magnitude: 0.002, Unit: "m"}},
		Date:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	if _, _, err := tx.Commit(); err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("minted", id)

	graph, _ := dfcad.LoadGraph(root)

	replacement, _ := graph.Claims().Claim(id)
	for retracted := range graph.Claims().Replaced(replacement) {
		was, _ := retracted.Value().Coordinate()
		fmt.Println(retracted.Rank(), "claim of", was, "from", retracted.Source())
	}

	// Output:
	// minted geom:V-01:position:1
	// deprecated claim of [0 0 0] from Interior control set IC-01, Acme Surveys
}

// ExampleTx_DeprecateClaim retracts a claim in favour of one already written.
// A replacement is required, which is the whole of what keeps deprecated from
// becoming a delete button.
func ExampleTx_DeprecateClaim() {
	root, err := os.MkdirTemp("", "dfcad")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	if err := os.CopyFS(root, os.DirFS("testdata/graph/valid")); err != nil {
		fmt.Println(err)
		return
	}

	tx, _, err := dfcad.Begin(root)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tx.Close()

	if _, err := tx.DeprecateClaim("survey:W-0002", ""); err != nil {
		fmt.Println(err)
	}

	notices, err := tx.DeprecateClaim("survey:W-0002", "survey:W-0003")
	if err != nil {
		fmt.Println(err)
		return
	}

	if _, _, err := tx.Commit(); err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("notices:", len(notices))

	graph, _ := dfcad.LoadGraph(root)
	fmt.Println("live claims of the width:", len(graph.Claims().Live("site:E-01", "width")))

	// Output:
	// expected the claim which replaces survey:W-0002, found none: a deprecated claim carries (superseded-by <claim-id>), which is what keeps deprecated from being a delete
	// notices: 0
	// live claims of the width: 1
}

// ExampleTx_AddVertex writes a corner together with where it is. The position
// is a claim like any other, carrying its evidence, its method, its accuracy
// and its date, so two surveys of one corner are two claims rather than a
// number somebody overwrote.
func ExampleTx_AddVertex() {
	root, err := os.MkdirTemp("", "dfcad")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	if err := os.CopyFS(root, os.DirFS("testdata/graph/valid")); err != nil {
		fmt.Println(err)
		return
	}

	tx, _, err := dfcad.Begin(root)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tx.Close()

	accuracy, err := dfcad.ParseAccuracyTerm("independent 0.004 m")
	if err != nil {
		fmt.Println(err)
		return
	}

	spec := dfcad.VertexSpec{
		ID:    "geom:V-07",
		Label: "Store, north-east corner",
		Frame: "frame:building",
		Position: dfcad.ClaimSpec{
			Predicate: "position",
			Value:     dfcad.CoordinateValue([]float64{4, 6, 0}, "m"),
			Source:    "Interior control set IC-01, Acme Surveys",
			Method:    "method:total-station",
			Accuracy:  []dfcad.AccuracyTerm{accuracy},
			Date:      time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC),
		},
	}

	// A geometric node declares neither a kind nor a type, so the rule which
	// files it matches on the namespace of its id and nothing else.
	destination, err := spec.Destination(tx.Graph().Registry(), "")
	if err != nil {
		fmt.Println(err)
		return
	}

	if err := tx.AddVertex(spec, destination.Path); err != nil {
		fmt.Println(err)
		return
	}

	if _, _, err := tx.Commit(); err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println(destination.Path, "by rule", destination.Rule)

	graph, _ := dfcad.LoadGraph(root)

	resolution, _ := graph.Claims().Resolve("geom:V-07", "position", graph.Registry())
	value, _ := resolution.Value()
	at, _ := value.Coordinate()

	fmt.Println("geom:V-07 is at", at, value.Unit())

	// Output:
	// entities/geometry.dfc by rule geometry
	// geom:V-07 is at [4 6 0] m
}

// ExampleTx_Scaffold writes a room from its corners: the vertices, the edges
// between them and the closed loop they form, in one change.
//
// The list is authored closed — its last corner names its first again — and a
// corner which lands on a vertex the model already holds reuses that vertex
// rather than writing a second one at the same point. Here the new store shares
// the room's south wall, so both of its northern corners and the edge between
// them are the ones already written: one node named by two loops, which cannot
// drift apart because there is nothing to drift.
func ExampleTx_Scaffold() {
	root, err := os.MkdirTemp("", "dfcad")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	if err := os.CopyFS(root, os.DirFS("testdata/graph/valid")); err != nil {
		fmt.Println(err)
		return
	}

	tx, _, err := dfcad.Begin(root)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tx.Close()

	accuracy, err := dfcad.ParseAccuracyTerm("independent 0.004 m")
	if err != nil {
		fmt.Println(err)
		return
	}

	metres := func(x, y, z float64) dfcad.Corner {
		return dfcad.Corner{Position: dfcad.CoordinateValue([]float64{x, y, z}, "m")}
	}

	built, notices, err := tx.Scaffold(dfcad.ScaffoldSpec{
		Namespace: "geom",
		Frame:     "frame:building",
		Label:     "Store boundary",
		Corners: []dfcad.Corner{
			metres(0, 3, 0), metres(4, 3, 0), metres(4, 6, 0), metres(0, 6, 0), metres(0, 3, 0),
		},
		Predicate: "position",
		Provenance: dfcad.ClaimSpec{
			Source:   "Interior control set IC-01, Acme Surveys",
			Method:   "method:total-station",
			Accuracy: []dfcad.AccuracyTerm{accuracy},
			Date:     time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC),
		},
		Tolerance: "boundary-closure",
		Snap:      true,
	}, "")
	if err != nil {
		fmt.Println(err)
		return
	}

	if _, _, err := tx.Commit(); err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("loop:", built.Loop)
	fmt.Println("created:", built.Created)
	fmt.Println("reused:", built.Reused)
	for _, snap := range built.Snaps {
		fmt.Printf("corner %d reuses %s, %v %s away\n", snap.Corner, snap.Vertex, snap.Distance, snap.Unit)
	}
	fmt.Println("notices:", len(notices))

	// The ring closes, judged against the tolerance the scaffold was given.
	graph, _ := dfcad.LoadGraph(root)

	positions := dfcad.Positions{}
	for vertex := range graph.Topology().Vertices() {
		resolution, _ := graph.Claims().Resolve(vertex.ID(), "position", graph.Registry())
		if value, ok := resolution.Value(); ok {
			positions[vertex.ID()] = value
		}
	}

	loop, _ := graph.Topology().Loop(built.Loop)
	assembly, _ := graph.Topology().Assemble(loop, positions, "boundary-closure", graph.Registry())

	fmt.Println("closed:", assembly.Closed())

	// Output:
	// loop: geom:loop-1
	// created: [geom:vertex-1 geom:vertex-2]
	// reused: [geom:E-03]
	// corner 1 reuses geom:V-04, 0 m away
	// corner 2 reuses geom:V-03, 0 m away
	// notices: 0
	// closed: true
}

// ExampleTx_Scaffold_unclosed refuses a corner list which does not return to
// where it started, naming the gap and its size.
//
// Closing one silently would leave the tool unable to tell an outline somebody
// finished from one they stopped typing halfway through, and the wall it
// invented would appear in no diagnostic anywhere.
func ExampleTx_Scaffold_unclosed() {
	root, err := os.MkdirTemp("", "dfcad")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	if err := os.CopyFS(root, os.DirFS("testdata/graph/valid")); err != nil {
		fmt.Println(err)
		return
	}

	tx, _, err := dfcad.Begin(root)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tx.Close()

	metres := func(x, y, z float64) dfcad.Corner {
		return dfcad.Corner{Position: dfcad.CoordinateValue([]float64{x, y, z}, "m")}
	}

	_, _, err = tx.Scaffold(dfcad.ScaffoldSpec{
		Namespace: "geom",
		Frame:     "frame:building",
		Corners: []dfcad.Corner{
			metres(0, 3, 0), metres(4, 3, 0), metres(4, 6, 0), metres(0, 6, 0),
		},
		Predicate: "position",
		Provenance: dfcad.ClaimSpec{
			Source: "Interior control set IC-01, Acme Surveys",
			Method: "method:total-station",
		},
		Tolerance: "boundary-closure",
		Snap:      true,
	}, "")

	var unclosed dfcad.UnclosedLoopError
	if errors.As(err, &unclosed) {
		fmt.Println(err)
		fmt.Println("the gap is", unclosed.Gap, unclosed.Unit)
	}

	// Output:
	// the corner list does not close: corner 4 is 3.0 m from corner 1, and the tolerance boundary-closure permits 0.005 m
	// the gap is 3 m
}

// ExampleTx_Apply applies a batch of edits as one change: the model is read
// once, every operation is applied to it in order, and what they produce
// together is validated once.
//
// The second operation names what the first one wrote, which is the pair a
// batch exists for — a node and the claim about it are one statement, and
// applying them as two commands would mean two loads and a moment in which the
// model holds a room nobody has measured.
func ExampleTx_Apply() {
	root, err := os.MkdirTemp("", "dfcad")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	if err := os.CopyFS(root, os.DirFS("testdata/graph/valid")); err != nil {
		fmt.Println(err)
		return
	}

	batch, err := dfcad.ParseBatch(strings.NewReader(`{
		"version": 1,
		"operations": [
			{"op": "add-node", "id": "site:S-103", "kind": "Space", "type": "MeetingRoom",
			 "geometry": "area", "frame": "frame:building", "label": "Meeting Room C"},
			{"op": "add-claim", "subject": "site:S-103", "predicate": "width",
			 "claim": {"value": "0.102", "unit": "m",
			           "source": "As-built check AB-2026-020, Acme Surveys",
			           "method": "method:total-station",
			           "accuracy": ["independent 0.003 m"],
			           "date": "2026-05-06"}}
		]
	}`))
	if err != nil {
		fmt.Println(err)
		return
	}

	tx, _, err := dfcad.Begin(root)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tx.Close()

	applied, err := tx.Apply(batch)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Nothing has been written yet. The batch is one change, and the change is
	// made by committing it.
	commit, refused, err := tx.Commit()
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, diagnostic := range refused {
		fmt.Println(diagnostic)
	}

	for _, operation := range applied {
		for _, effect := range operation.Effects {
			fmt.Printf("%d %s: %s %s %s\n",
				operation.Index, operation.Operation, effect.Op, effect.Tag, effect.ID)
		}
	}
	for _, file := range commit.Files {
		fmt.Println(filepath.Base(file.Path), file.Status)
	}

	// Output:
	// 1 add-node: created node site:S-103
	// 2 add-claim: modified node site:S-103
	// site.dfc rewritten
}

// ExampleParseBatch_refused reports every problem an operation file has at
// once, each naming the operation it is about by its place in the list.
//
// An author fixing a batch a tool generated should not have to reissue it once
// per mistake, which is the same rule a refused write is reported by.
func ExampleParseBatch_refused() {
	_, err := dfcad.ParseBatch(strings.NewReader(`{
		"operations": [
			{"op": "add-node", "kind": "Space"},
			{"op": "add-node", "id": "site:S-103", "edges": ["geom:E-01"]},
			{"op": "add-widget", "id": "site:S-104"}
		]
	}`))

	var refused dfcad.BatchError
	if errors.As(err, &refused) {
		for _, problem := range refused.Errs {
			fmt.Println(problem)
		}
	}

	// Every problem is reachable, so a caller branches on what is wrong rather
	// than on the message saying so.
	fmt.Println("no id was written:", errors.Is(err, dfcad.ErrNoID))

	// Output:
	// operation 1, add-node: a node is written with an id
	// operation 2, add-node: json: unknown field "edges"
	// operation 3, add-widget: unknown operation "add-widget": want one of add-node, add-vertex, add-edge, add-loop, scaffold-loop, set-label, retire, add-claim, supersede, deprecate-claim
	// no id was written: true
}

func ExampleReview() {
	// One model at two revisions: the merge base, and the change under review.
	base, _ := dfcad.LoadGraph("testdata/review/base")
	head, _ := dfcad.LoadGraph("testdata/review/head")

	// Nothing is wrong with either revision on its own — both load, and both
	// satisfy every rule they state. What needs an explanation is the
	// difference between them, which is a question no single revision can be
	// asked.
	for _, finding := range dfcad.Review(base, head, dfcad.DefaultPolicy(), nil) {
		fmt.Println(finding.Ruling, finding.Kind, finding.Subject)
		fmt.Println(finding.Message)
		fmt.Println(finding.Hint)
	}

	// Output:
	// warning boundary-moved-without-claim site:S-101
	// the boundary of site:S-101 moved: the position of geom:V-02 was rewritten from (4.0 0.0 0.0) m to (4.6 0.0 0.0) m inside the claim which already stated it, so nothing new was measured
	// a corner which moved was measured again, so write the measurement: `dfcad supersede geom:V-02 position ...` keeps what the first survey said beside what the second one found
}

func ExamplePolicy() {
	base, _ := dfcad.LoadGraph("testdata/review/base")
	head, _ := dfcad.LoadGraph("testdata/review/head")

	// A wall which moved because the room was surveyed again is a change
	// somebody meant. Saying so is a policy, stated once, rather than not
	// running the check.
	policy := dfcad.DefaultPolicy().With(dfcad.FindingBoundaryMoved, dfcad.RulingIgnored)

	for _, finding := range dfcad.Review(base, head, policy, nil) {
		// The finding is still here. A check which was switched off silently is
		// one nobody remembers is off, so what the policy acknowledged is
		// readable from the run which acknowledged it.
		fmt.Println(finding.Ruling, finding.Kind)
	}

	fmt.Println(policy.Ruling(dfcad.FindingIDDisappeared), "is still what an id which vanished means")

	// Output:
	// ignored boundary-moved-without-claim
	// failure is still what an id which vanished means
}

// ExampleTopology_RegionOf reads the area two rooms cover and overlays them,
// which is the question a setback or a clearance is asked as: not how big is
// this, but what is left of it once that is taken out.
//
// Nothing about what a region covers is stored. It is read back out of the
// loops bounding the node every time it is asked for, so an overlap cannot
// disagree with the geometry it was computed from.
func ExampleTopology_RegionOf() {
	root := "testdata/overlay/shapes"

	registry, _ := dfcad.LoadRegistry(root)
	nodes, _ := dfcad.LoadNodes(root, registry)
	topology, _ := dfcad.LoadTopology(root, registry)
	claims, _ := dfcad.LoadClaims(root, registry)
	boundaries, _ := dfcad.ResolveBoundaries(nodes, topology)

	survey := dfcad.Survey{Tolerance: "boundary-closure", Registry: registry}
	for vertex := range topology.Vertices() {
		resolution, _ := claims.Resolve(vertex.ID(), "position", registry)
		survey.Place(vertex.ID(), resolution)
	}

	first, _ := nodes.Node("site:S-01")
	second, _ := nodes.Node("site:S-02")

	room, _ := topology.RegionOf(first, boundaries, survey)
	store, _ := topology.RegionOf(second, boundaries, survey)

	shared, _ := room.Intersect(store)
	both, _ := room.Union(store)
	rest, _ := room.Difference(store)

	fmt.Println(shared)
	fmt.Println(both)
	fmt.Println(rest)

	// How the two sit against each other, which "not inside" does not say: each
	// of them reaches outside the other.
	containment, _ := room.Containment(store)
	fmt.Printf("%s and %s are %s\n", room.Subject(), store.Subject(), containment)

	// Output:
	// the region derived from site:S-01: area 4.0 m², 1 piece
	// the region derived from site:S-01: area 24.0 m², 1 piece
	// the region derived from site:S-01: area 8.0 m², 1 piece
	// site:S-01 and site:S-02 are overlapping
}

// ExampleRegion_Buffer offsets a floor plate outwards and inwards, which are the
// setback question and the clearance one and are the same construction run
// either way round.
//
// The plate has a courtyard in it, and both offsets do the right thing to it
// without anything having to say which of the two rings is the hole: growing the
// plate shrinks the courtyard, and shrinking the plate grows it.
func ExampleRegion_Buffer() {
	root := "testdata/overlay/shapes"

	registry, _ := dfcad.LoadRegistry(root)
	nodes, _ := dfcad.LoadNodes(root, registry)
	topology, _ := dfcad.LoadTopology(root, registry)
	claims, _ := dfcad.LoadClaims(root, registry)
	boundaries, _ := dfcad.ResolveBoundaries(nodes, topology)

	survey := dfcad.Survey{Tolerance: "boundary-closure", Registry: registry}
	for vertex := range topology.Vertices() {
		resolution, _ := claims.Resolve(vertex.ID(), "position", registry)
		survey.Place(vertex.ID(), resolution)
	}

	node, _ := nodes.Node("site:S-04")
	plate, _ := topology.RegionOf(node, boundaries, survey)

	fmt.Println(plate)

	for _, distance := range []float64{1, -1} {
		offset, _ := plate.Buffer(distance)

		// One piece with one hole either way round. Growing the plate by a metre
		// shrinks the courtyard by one, and shrinking the plate by a metre grows
		// the courtyard by one — which is the same offset applied to the whole of
		// the boundary rather than two rules about two rings.
		bounds, _ := offset.Bounds()
		fmt.Printf("offset by %g m: %.2f m², %d hole, reaching %s\n",
			distance, offset.Area(), len(offset.Pieces()[0].Holes()), bounds)
	}

	// An inward offset which eats a shape returns a region covering nothing,
	// which is the answer rather than a failure: a corridor 400 mm wide has
	// nothing 300 mm clear of both of its walls.
	corridor, _ := nodes.Node("site:S-07")
	thin, _ := topology.RegionOf(corridor, boundaries, survey)

	collapsed, _ := thin.Buffer(-0.3)
	fmt.Println(collapsed.Empty(), collapsed.Area())

	// Output:
	// site:S-04: area 84.0 m², 1 piece, 1 hole
	// offset by 1 m: 139.12 m², 1 hole, reaching (9.0 -1.0 0.0) to (21.0 11.0 0.0) m
	// offset by -1 m: 28.88 m², 1 hole, reaching (11.0 1.0 0.0) to (19.0 9.0 0.0) m
	// true 0
}

// A curved wall is kept as the curve it is, and measured from the circle rather
// than from a drawing of it.
func ExampleSurvey_Bend() {
	root := "testdata/measure/arcs"

	registry, _ := dfcad.LoadRegistry(root)
	nodes, _ := dfcad.LoadNodes(root, registry)
	topology, _ := dfcad.LoadTopology(root, registry)
	claims, _ := dfcad.LoadClaims(root, registry)
	boundaries, _ := dfcad.ResolveBoundaries(nodes, topology)

	survey := dfcad.Survey{Tolerance: "boundary-closure", Registry: registry}
	for vertex := range topology.Vertices() {
		resolution, _ := claims.Resolve(vertex.ID(), "position", registry)
		survey.Place(vertex.ID(), resolution)
	}

	// Which predicates carry an arc is vocabulary this repository owns too, so
	// the two behind one are resolved here and handed in. An edge nobody has
	// claimed a centre for is straight.
	for edge := range topology.Edges() {
		centre, _ := claims.Resolve(edge.ID(), "arc-centre", registry)
		through, _ := claims.Resolve(edge.ID(), "arc-through", registry)
		survey.Bend(edge.ID(), centre, through)
	}

	room, _ := nodes.Node("site:S-01")

	measurement, _ := topology.MeasureRegion(room, boundaries, survey)

	// Four metres square, plus the half disc its east wall bows out into. The
	// figure is the closed form and not a sum over chords, so it is right to
	// every digit rather than to whatever resolution somebody drew at.
	area, _ := measurement.Area()
	length, _ := measurement.Length()
	fmt.Printf("%s: %.6f %s², bounded by %.6f %s of wall\n",
		measurement.Subject(), area, measurement.Unit(), length, measurement.Unit())

	// Straight lines are wanted only where something actually needs them, and
	// then the tolerance is named and travels with the answer.
	bay, _ := topology.Edge("geom:E-02")
	drawn, _ := topology.TessellateEdge(bay, survey, "chord-deviation")

	fmt.Printf("%s drawn as %d segments, within %.6f %s of the curve, to %s\n",
		drawn.Subject(), len(drawn.Points())-1, drawn.Deviation(), drawn.Unit(), drawn.ChordTolerance().Name)

	// Output:
	// site:S-01: 22.283185 m², bounded by 18.283185 m of wall
	// geom:E-02 drawn as 16 segments, within 0.009631 m of the curve, to chord-deviation
}

// A boundary which curves is drawn to straight segments when something actually
// needs them — an export, a plan ring, a polygon — and the tolerance it was
// drawn to travels with it.
func ExampleTopology_TessellateRegion() {
	root := "testdata/measure/arcs"

	registry, _ := dfcad.LoadRegistry(root)
	nodes, _ := dfcad.LoadNodes(root, registry)
	topology, _ := dfcad.LoadTopology(root, registry)
	claims, _ := dfcad.LoadClaims(root, registry)
	boundaries, _ := dfcad.ResolveBoundaries(nodes, topology)

	survey := dfcad.Survey{Tolerance: "boundary-closure", Registry: registry}
	for vertex := range topology.Vertices() {
		resolution, _ := claims.Resolve(vertex.ID(), "position", registry)
		survey.Place(vertex.ID(), resolution)
	}

	for edge := range topology.Edges() {
		centre, _ := claims.Resolve(edge.ID(), "arc-centre", registry)
		through, _ := claims.Resolve(edge.ID(), "arc-through", registry)
		survey.Bend(edge.ID(), centre, through)
	}

	// A floor plate with a round courtyard in the middle of it. Nothing measures
	// it as it stands: which of two rings is inside the other is decided at
	// their corners, and a ring which bulges past a corner is not a question the
	// corners answer. Drawing the curve is what makes it answerable, and the
	// answer says how closely it was drawn.
	plate, _ := nodes.Node("site:S-31")

	drawn, diags := topology.TessellateRegion(plate, boundaries, survey, "chord-deviation")
	for _, diagnostic := range diags {
		fmt.Println(diagnostic)
	}

	piece := drawn.Pieces()[0]

	fmt.Printf("%s: %.4f %s², %d piece with %d hole\n",
		drawn.Subject(), drawn.Area(), drawn.Unit(), len(drawn.Pieces()), len(piece.Holes()))
	fmt.Printf("bounded by %d corners around %d segments, within %.6f %s of the boundary, to %s\n",
		len(piece.Outer()), len(piece.Holes()[0]), drawn.Deviation(), drawn.Unit(), drawn.ChordTolerance().Name)

	// What comes back is an ordinary region, so everything which takes one takes
	// this. A boundary which was never curved comes back from here exactly as
	// Topology.RegionOf reads it, which is what makes that one path rather than
	// two.
	bounds, _ := drawn.Region().Bounds()
	fmt.Println(bounds)

	// Output:
	// site:S-31: 87.5142 m², 1 piece with 1 hole
	// bounded by 4 corners around 32 segments, within 0.009631 m of the boundary, to chord-deviation
	// (30.0 0.0 0.0) to (40.0 10.0 0.0) m
}

// ExampleRegion_Segments attributes a boundary back to the model it came from:
// which edge produced each straight run of it, which way round the loop ran
// through that edge, and what produced a run no edge did.
//
// It is what an exporter needs and what a polygon on its own cannot say. A ring
// of coordinates can be drawn; it cannot say which segment is the party wall,
// which element backs it, or which claim was written about it. The pairing is
// known where the boundary is assembled, so it is reported from there rather
// than re-derived downstream by matching coordinates.
func ExampleRegion_Segments() {
	root := "testdata/overlay/shapes"

	registry, _ := dfcad.LoadRegistry(root)
	nodes, _ := dfcad.LoadNodes(root, registry)
	topology, _ := dfcad.LoadTopology(root, registry)
	claims, _ := dfcad.LoadClaims(root, registry)
	boundaries, _ := dfcad.ResolveBoundaries(nodes, topology)

	survey := dfcad.Survey{Tolerance: "boundary-closure", Registry: registry}
	for vertex := range topology.Vertices() {
		resolution, _ := claims.Resolve(vertex.ID(), "position", registry)
		survey.Place(vertex.ID(), resolution)
	}

	node, _ := nodes.Node("site:S-01")
	room, _ := topology.RegionOf(node, boundaries, survey)

	// Every run of the boundary of a region read from the model is an edge
	// somebody wrote, and says which ring of the boundary it belongs to and
	// which way round the loop ran through it.
	for _, segment := range room.Segments() {
		fmt.Printf("ring %d: %s\n", segment.Ring(), segment)
	}

	// An operation attributes none of its boundary, and says so rather than
	// naming the edge which nearly produced a run: an offset corner is where the
	// offset put it and no edge of the model runs there.
	offset, _ := room.Buffer(1)

	origins := map[dfcad.SegmentOrigin]int{}
	for _, segment := range offset.Segments() {
		origins[segment.Origin()]++
	}

	fmt.Printf("offset: %d runs, all %s, none of them an edge\n",
		origins[dfcad.SegmentOriginOperation], dfcad.SegmentOriginOperation)

	// Output:
	// ring 0: (0.0 0.0 0.0) to (4.0 0.0 0.0): edge geom:E-011, forwards
	// ring 0: (4.0 0.0 0.0) to (4.0 3.0 0.0): edge geom:E-012, forwards
	// ring 0: (4.0 3.0 0.0) to (0.0 3.0 0.0): edge geom:E-013, forwards
	// ring 0: (0.0 3.0 0.0) to (0.0 0.0 0.0): edge geom:E-014, forwards
	// offset: 36 runs, all operation, none of them an edge
}

// ExampleGraph_Derive computes the derived geometry of a whole model — what each
// thing covers, how big that is, where it is centred, how far it reaches and
// which regions it lies inside — and keeps the answer in a build output
// directory.
//
// None of it is ever written back into an entity file. The cache is keyed by the
// digest of the source tree the geometry was derived from, so a model which
// changed anywhere is a different key and a miss; there is no invalidation step
// to get wrong, because the key is the invalidation.
func ExampleGraph_Derive() {
	root := "testdata/derived/model"

	build, err := os.MkdirTemp("", "dfcad-build-")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(build)

	cache, err := dfcad.OpenCache(build)
	if err != nil {
		fmt.Println(err)
		return
	}

	graph, _ := dfcad.LoadGraph(root)

	against := dfcad.Derivation{
		Tolerance: "boundary-closure",
		Position:  "position",
		Cache:     cache,
	}

	prints, _ := graph.Derive(against)
	for print := range prints.All() {
		fmt.Println(print)
	}

	// The second run is the same answer, read rather than computed. Deleting the
	// whole of the build output directory would change what comes back by
	// nothing at all — only how long it took.
	again, _ := graph.Derive(against)

	fmt.Println(prints.Digest() == again.Digest(), cache.Stats().Hits, cache.Stats().Misses)

	// Output:
	// site:S-01: area 192.0 m², perimeter 72.0 m, centroid (10.0 5.0 0.0), 1 piece, 1 hole
	// site:S-02: area 8.0 m², perimeter 12.0 m, centroid (10.0 5.0 0.0), 1 piece
	// site:S-03: area 16.0 m², perimeter 16.0 m, centroid (4.0 4.0 0.0), 1 piece, within site:S-01
	// site:S-04: area 1.0 m², perimeter 4.0 m, centroid (3.5 3.5 0.0), 1 piece, within site:S-01, site:S-03
	// site:S-05: area 12.0 m², perimeter 14.0 m, centroid (2.0 1.5 0.0), 1 piece
	// true 1 1
}

// ExampleDigestOf shows what makes a stale cache hit unrepresentable: the key is
// a digest over the bytes of the source tree, so an edit anywhere in it — one
// coordinate, one new file — is a different key.
//
// It also shows what is deliberately not in it. A build output written beside
// the model is not an input to anything derived from the model, and a
// recomputation every time somebody's editor wrote a swap file would be a cache
// which never paid for itself.
func ExampleDigestOf() {
	root, err := os.MkdirTemp("", "dfcad-model-")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	entities := filepath.Join(root, "model.dfc")
	if err := os.WriteFile(entities, []byte("(node site:S-101 (label \"Meeting Room B\") (kind Space))\n"), 0o644); err != nil {
		fmt.Println(err)
		return
	}

	before, _ := dfcad.DigestOf(root)

	// Anything a walk does not read is not an input to a derivation.
	notes := filepath.Join(root, "README.md")
	if err := os.WriteFile(notes, []byte("nothing derives from this\n"), 0o644); err != nil {
		fmt.Println(err)
		return
	}

	unchanged, _ := dfcad.DigestOf(root)

	// One byte of an entity file is a different tree.
	if err := os.WriteFile(entities, []byte("(node site:S-101 (label \"Meeting Room C\") (kind Space))\n"), 0o644); err != nil {
		fmt.Println(err)
		return
	}

	after, _ := dfcad.DigestOf(root)

	fmt.Println(before == unchanged, before == after)

	// Output:
	// true false
}

// ExampleDerivationEpoch is the instant an exported artefact carries wherever
// its target format demands a creation time.
//
// It is derived from the tree rather than from a clock, so two exports of an
// unchanged model are byte-identical and the provenance the timestamp field
// pretends to carry is carried properly instead — by the digest.
func ExampleDerivationEpoch() {
	root, err := os.MkdirTemp("", "dfcad-model-")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)

	entities := filepath.Join(root, "model.dfc")
	if err := os.WriteFile(entities, []byte("(node site:S-101 (label \"Meeting Room B\") (kind Space))\n"), 0o644); err != nil {
		fmt.Println(err)
		return
	}

	digest, err := dfcad.DigestOf(root)
	if err != nil {
		fmt.Println(err)
		return
	}

	epoch := dfcad.DerivationEpoch(digest)

	// Each format takes the encoding it demands, and no exporter writes its own.
	fmt.Println(epoch.ISO8601())
	fmt.Println(epoch.STEP())
	fmt.Println(epoch.PDF())
	fmt.Println(epoch.Seconds())

	// A tree which could not be read still answers, so a refusal reaches its
	// diagnostic rather than panicking on the way there.
	fmt.Println(dfcad.DerivationEpoch(dfcad.Digest{}) == epoch)

	// Output:
	// 1970-01-01T00:00:00Z
	// 1970-01-01T00:00:00
	// D:19700101000000Z
	// 0
	// true
}

// ExampleTopology_BuildableOf derives what may be built on a plot from the
// plot's boundary and the setback claimed on each of its edges.
//
// Nothing in the model says what is buildable. The region is read back out of
// the corners, the edges and the claims every time it is asked for, so the day
// the frontage setback changes there is no second polygon left saying the old
// answer — which is the failure this derivation exists to make unrepresentable,
// because the shape it describes is the one a permanent structure gets placed
// against.
func ExampleTopology_BuildableOf() {
	root := "testdata/buildable/parcels"

	registry, _ := dfcad.LoadRegistry(root)
	nodes, _ := dfcad.LoadNodes(root, registry)
	topology, _ := dfcad.LoadTopology(root, registry)
	claims, _ := dfcad.LoadClaims(root, registry)
	boundaries, _ := dfcad.ResolveBoundaries(nodes, topology)

	survey := dfcad.Survey{Tolerance: "boundary-closure", Registry: registry}
	for vertex := range topology.Vertices() {
		resolution, _ := claims.Resolve(vertex.ID(), "position", registry)
		survey.Place(vertex.ID(), resolution)
	}

	setbacks := dfcad.Setbacks{Predicate: "setback", Claims: claims}

	// Six metres at the road, three at each flank and four at the rear. Which
	// edge is which is written on the edges rather than known here.
	plot, _ := nodes.Node("plan:P-01")

	sited, _ := topology.BuildableOf(plot, boundaries, survey, setbacks)
	fmt.Println(sited.Report())

	// How well the edge of it is known follows from the corners and from the
	// setbacks together, term by term, with the control point every corner was
	// shot from counted once.
	combined, _ := sited.Budget().Combined()
	dominant, _ := sited.Budget().Dominant()
	fmt.Printf("±%.3f %s, of which the largest term is %s\n",
		combined.Standard(), combined.Unit, dominant.Name)

	// A plot its own setbacks consume comes back covering nothing, with a
	// diagnostic saying so. It is the answer to the question rather than a
	// failure to answer it, so it is a warning and the region is still a region.
	infill, _ := nodes.Node("plan:P-02")

	consumed, diags := topology.BuildableOf(infill, boundaries, survey, setbacks)
	fmt.Println(consumed, diags[0].Severity)

	// Output:
	// plan:P-01: 240.0 m² buildable of 600.0 m²
	//   geom:E-101: 6.0 m
	//   geom:E-102: 3.0 m
	//   geom:E-103: 4.0 m
	//   geom:E-104: 3.0 m
	// ±0.023 m, of which the largest term is the setback of geom:E-101
	// nothing is buildable inside plan:P-02 warning
}

func ExampleTopology_FitWithin() {
	root := "testdata/siting/surveyed"

	registry, _ := dfcad.LoadRegistry(root)
	nodes, _ := dfcad.LoadNodes(root, registry)
	topology, _ := dfcad.LoadTopology(root, registry)
	claims, _ := dfcad.LoadClaims(root, registry)
	boundaries, _ := dfcad.ResolveBoundaries(nodes, topology)

	// The frames are joined to the claims which measure them, because the
	// relationship between two of them is a measurement and not a setting.
	frames, _ := dfcad.ResolveFrames(registry, claims)

	survey := dfcad.Survey{Tolerance: "boundary-closure", Registry: registry}
	for vertex := range topology.Vertices() {
		resolution, _ := claims.Resolve(vertex.ID(), "position", registry)
		survey.Place(vertex.ID(), resolution)
	}

	// The footprint was set out on the building's own grid and the plot was
	// surveyed on the site grid, so deciding this reads the georeference.
	footprint, _ := nodes.Node("plan:S-01")
	buildable, _ := nodes.Node("plan:B-01")

	answer, _ := topology.FitWithin(footprint, buildable, boundaries, survey, dfcad.Siting{Frames: frames})

	combined, _ := answer.Uncertainty()
	fmt.Printf("%s: %.2f m clear, ±%.4f m, carried across %s\n",
		answer.Verdict(), answer.Clearance(), combined.Standard(), answer.DeclaredIn())

	// The control point behind the interior corners is behind the boundary
	// survey and the georeference as well. It is one error however many inputs
	// reach it, so it appears in the sum once — and adds linearly rather than in
	// quadrature, because it does not cancel against itself.
	var shared dfcad.BudgetTerm
	for _, term := range answer.Budget().Terms() {
		if term.Shared() {
			shared = term
		}
	}

	fmt.Printf("%s %s: %.3f m from %d claims\n",
		shared.Kind, shared.Name, shared.Magnitude, len(shared.Contributors))

	// A clearance inside its own uncertainty is neither a pass nor a failure.
	// Ten millimetres of daylight is no answer at all when the answer is known
	// to twenty.
	tight, _ := nodes.Node("plan:S-03")

	marginal, diags := topology.FitWithin(tight, buildable, boundaries, survey, dfcad.Siting{Frames: frames})
	fmt.Println(marginal.Verdict(), marginal.Verdict().Decided(), diags[0].Severity)

	// Output:
	// fits: 4.00 m clear, ±0.0203 m, carried across frame:building
	// systematic control:CP-1: 0.008 m from 9 claims
	// might-fit false warning
}

func ExampleLoadObservations() {
	root := "testdata/observations/valid"

	registry, _ := dfcad.LoadRegistry(root)

	log, diags := dfcad.LoadObservations(root, registry)
	fmt.Println(log.Len(), "records,", len(diags), "diagnostics")

	// What the log resolves to is every observation no retirement names. The
	// retired one is still in the file and still readable; it is simply not
	// what the log currently says.
	for observation := range log.Current() {
		fmt.Printf("%s %s %.3f %s\n",
			observation.ID, observation.Fix, observation.HorizontalPrecision, observation.Session)
	}

	retired, _ := log.Observation("shot:2026-05-06-0003")
	retirement, _ := log.RetirementOf(retired.ID)

	fmt.Printf("%s: %.3f, retired on line %d: %s\n",
		retired.ID, retired.HorizontalPrecision, retirement.Line(), retirement.Reason)

	// Output:
	// 6 records, 0 diagnostics
	// shot:2026-05-06-0001 fix:rtk-fixed 0.012 session:2026-05-06-am
	// shot:2026-05-06-0002 fix:rtk-fixed 0.011 session:2026-05-06-am
	// shot:2026-05-06-0004 fix:rtk-fixed 0.013 session:2026-05-06-am
	// shot:2026-05-06-0005 fix:rtk-fixed 0.014 session:2026-05-06-pm
	// shot:2026-05-06-0003: 0.240, retired on line 8: float solution beside a fixed reshot of the same corner
}

func ExampleGraph_Observations() {
	// The model is loaded whole, and not one observation file is opened doing
	// it: what a load checks is that each linked file is there. That is the
	// whole point of keeping the records outside the entity files — an
	// afternoon with a rover is thousands of shots, and retrieving one corner
	// must not cost a season of field work.
	graph, diags := dfcad.LoadGraph("testdata/graph/observed")
	fmt.Println(len(diags), "diagnostics, and the files not yet read")

	corner, _ := graph.Entity("geom:V-01")

	// The links are part of the model, so saying where the evidence is costs
	// nothing.
	for _, link := range corner.ObservedIn() {
		fmt.Println("observed in", link.Path)
	}

	// Asking for the records is what opens the files. They are read once each
	// for the life of the graph, and the several files a thing links to are one
	// log: "earlier" is a question about the whole of what was read.
	log, problems := graph.Observations(corner)
	fmt.Println(log.Len(), "records of both forms,", len(problems), "problems")

	for observation := range log.Current() {
		fmt.Printf("%s %s %.3f\n", observation.ID, observation.Fix, observation.HorizontalPrecision)
	}

	// Output:
	// 0 diagnostics, and the files not yet read
	// observed in observations/2026-05-06-site-control.obs
	// observed in observations/2026-05-07-interior.obs
	// 6 records of both forms, 0 problems
	// shot:2026-05-06-0001 fix:rtk-fixed 0.012
	// shot:2026-05-06-0002 fix:rtk-fixed 0.011
	// shot:2026-05-07-0001 fix:observed 0.004
	// shot:2026-05-07-0002 fix:observed 0.004
}

// ExampleGraph_ObservationsWithin shows what derived membership buys: a region
// drawn today holding shots taken months before it existed, with nothing
// appended to an observation file to say so.
//
// The two models below are one survey drawn twice. Every observation file is
// byte for byte the same in both, and the bed links none of them at all — what
// differs is where a line is drawn, and the answer moves with it.
func ExampleGraph_ObservationsWithin() {
	against := dfcad.Derivation{Tolerance: "boundary-closure", Position: "position"}

	show := func(graph *dfcad.Graph, subject dfcad.ID) {
		members, _ := graph.ObservationsWithin(subject, against)

		for _, member := range members.Inside() {
			// A shot in a file the region cites is a shot *of* it, which is
			// stored. A shot merely in the place is written down nowhere.
			relation := "in"
			if member.Linked() {
				relation = "of"
			}

			fmt.Printf("%s holds %s, a shot %s it, %.3f m inside\n",
				subject, member.Observation().ID, relation, member.Clearance())
		}

		// A shot nearer the boundary than the survey can place it is reported
		// rather than assigned to a side. The band is the registry tolerance,
		// the shot's own precision and whatever a change of frame cost.
		for _, member := range members.Ambiguous() {
			doubt, _ := member.Doubt()

			fmt.Printf("%s cannot place %s: %.3f m from the boundary, known to %.3f m\n",
				subject, member.Observation().ID, math.Abs(member.Clearance()), doubt)
		}
	}

	garden, _ := dfcad.LoadGraph("testdata/membership/yard")
	show(garden, "site:S-yard")

	fmt.Println("--- a raised bed is carved out of the north-east corner ---")

	carved, _ := dfcad.LoadGraph("testdata/membership/carved")
	show(carved, "site:S-yard")
	show(carved, "site:S-bed")

	// Output:
	// site:S-yard holds shot:0001, a shot of it, 3.000 m inside
	// site:S-yard holds shot:0002, a shot in it, 3.000 m inside
	// site:S-yard holds shot:0003, a shot in it, 2.000 m inside
	// site:S-yard holds shot:0004, a shot in it, 3.000 m inside
	// site:S-yard holds shot:0006, a shot in it, 3.000 m inside
	// site:S-yard holds shot:0009, a shot in it, 2.000 m inside
	// site:S-yard cannot place shot:0007: 0.100 m from the boundary, known to 0.245 m
	// --- a raised bed is carved out of the north-east corner ---
	// site:S-yard holds shot:0001, a shot of it, 3.000 m inside
	// site:S-yard holds shot:0003, a shot in it, 2.000 m inside
	// site:S-yard holds shot:0006, a shot in it, 1.000 m inside
	// site:S-yard cannot place shot:0004: 0.000 m from the boundary, known to 0.017 m
	// site:S-yard cannot place shot:0007: 0.100 m from the boundary, known to 0.245 m
	// site:S-bed holds shot:0002, a shot in it, 3.000 m inside
	// site:S-bed holds shot:0009, a shot in it, 2.000 m inside
	// site:S-bed cannot place shot:0004: 0.000 m from the boundary, known to 0.017 m
}

// ExampleGraph_SurfaceWithin shows the ground answered for from the shots
// somebody took, rather than from a surface somebody drew.
//
// Nothing below is stored. The surface is derived from the observations inside
// the region every time it is asked, kept only under the build output directory,
// and written into no file a walk would read — so a shot taken tomorrow changes
// what the ground does here, and no file has to be edited to say so.
func ExampleGraph_SurfaceWithin() {
	graph, _ := dfcad.LoadGraph("testdata/surface/terrace")

	surface, _ := graph.SurfaceWithin("site:S-terrace", dfcad.SurfaceDerivation{
		Against: dfcad.Derivation{Tolerance: "boundary-closure", Position: "position"},
	})

	// How it was derived travels with it. Two interpolations of one set of
	// points are two different answers, and a grid of levels with no method on
	// it is one nobody can check.
	fmt.Println(surface)
	for _, parameter := range surface.Parameters() {
		fmt.Printf("  %s\n", parameter)
	}

	// The surface reaches as far as the shots and no further. A point beyond
	// the hull of them is reported as outside, never extrapolated: there is no
	// measurement out there, and a level continued past the last shot reads
	// exactly like a surveyed one.
	for _, at := range []dfcad.Point{{4, 4, 0}, {14, 8, 0}, {19, 11, 0}} {
		elevation, inside := surface.Elevation(at)
		if !inside {
			fmt.Printf("(%.0f, %.0f) is outside the surveyed ground\n", at[0], at[1])
			continue
		}

		fmt.Printf("(%.0f, %.0f) is at %.3f m ± %.3f m, from %v\n",
			at[0], at[1], elevation.Value(), elevation.Uncertainty(), elevation.From())
	}

	// And a surface can be traced back to the afternoon behind it.
	fmt.Printf("derived from %v\n", surface.Observations())

	// Output:
	// site:S-terrace: tin from 7 points, 8 facets, hull of 4 points
	//   method=tin
	//   tolerance=boundary-closure
	//   position=position
	//   minimum-points=3
	//   ambiguous=excluded
	//   roughness=unstated
	//   systematic=none
	// (4, 4) is at 100.120 m ± 0.014 m, from [shot:0001 shot:0005 shot:0006]
	// (14, 8) is at 100.540 m ± 0.015 m, from [shot:0003 shot:0005 shot:0007]
	// (19, 11) is outside the surveyed ground
	// derived from [shot:0001 shot:0002 shot:0003 shot:0004 shot:0005 shot:0006 shot:0007 shot:0008]
}

// ExampleSurface_Fall shows a decision made against a derived surface: how much
// the ground falls across a patio, and whether that is known well enough to
// decide anything by.
//
// The requirement is stated first and in the same units as the answer. A fall
// which has to be one in eighty over four metres is fifty millimetres of drop,
// and the survey has to resolve that well enough for the decision to turn on the
// patio rather than on the survey — here, five millimetres at one sigma.
//
// The part worth reading twice is the last line of the budget. Every shot of the
// patio was taken in one occupation on one base station, and whatever that base
// station is out by is in both ends of the fall by the same amount. It therefore
// cancels: the difference is known better than either level it is the difference
// of, and a caller who combined the two levels in quadrature would have counted
// that error twice over and gone off to re-survey ground which was never the
// problem.
func ExampleSurface_Fall() {
	graph, _ := dfcad.LoadGraph("testdata/surface/patio")

	surface, _ := graph.SurfaceWithin("site:S-patio", dfcad.SurfaceDerivation{
		Against: dfcad.Derivation{Tolerance: "boundary-closure", Position: "position"},

		// What the project says about the ground between the shots, and what an
		// afternoon on one base station is worth. Neither is in any file: the
		// first is a property of the paving and the second of the setup, and a
		// budget which assumed nought for either would be quietly optimistic.
		Roughness: 0.003,
		Systematic: []dfcad.SessionSystematic{
			{Session: "session:2026-06-03-am", Magnitude: 0.010},
		},
	})

	threshold, drain := dfcad.Point{3.5, 5, 0}, dfcad.Point{3.5, 1, 0}

	fall, inside := surface.Fall(threshold, drain)
	if !inside {
		return
	}

	fmt.Printf("the patio falls %.3f m over %.1f m, which is %.0f mm of drop\n",
		fall.Value(), fall.Run(), fall.Value()*1000)
	fmt.Printf("each level on its own is worth %.4f m\n", fall.From().Uncertainty())
	fmt.Printf("the fall between them is worth %.4f m\n", fall.Uncertainty())

	for _, term := range fall.Budget().Terms() {
		if term.Kind == dfcad.TermSystematic {
			fmt.Printf("%s: %.4f m\n", term.Name, term.Magnitude)
		}
	}

	// Which is the whole point of asking: the answer is measured against a
	// requirement somebody stated, and it does not meet it.
	fmt.Println("decides a fall known to 5 mm:", fall.Decides(0.005))
	fmt.Println("decides a fall known to 30 mm:", fall.Decides(0.030))

	// Output:
	// the patio falls 0.050 m over 4.0 m, which is 50 mm of drop
	// each level on its own is worth 0.0182 m
	// the fall between them is worth 0.0215 m
	// session:2026-06-03-am: 0.0000 m
	// decides a fall known to 5 mm: false
	// decides a fall known to 30 mm: true
}

func ExampleValidateAppendOnly() {
	read := func(name string) dfcad.ObservationSource {
		src, _ := os.ReadFile(filepath.Join("testdata", "observations", "append", name))
		return dfcad.ObservationSource{Path: name, Bytes: src}
	}

	base := read("base.obs")

	// Bytes at the end are the only legal change, so an append has nothing to
	// report however many records it adds.
	fmt.Println(len(dfcad.ValidateAppendOnly(base, read("appended.obs"))), "on an append")

	// A float solution quietly re-spelled as a fixed one is a correction of the
	// data with nothing in the file to say so, which is exactly what this
	// refuses.
	for _, diagnostic := range dfcad.ValidateAppendOnly(base, read("edited.obs")) {
		fmt.Println(diagnostic.Span.Start, diagnostic.Message)
		fmt.Println(diagnostic.Related[0].Span.Start, diagnostic.Related[0].Message)
	}

	// Output:
	// 0 on an append
	// edited.obs:6:1 expected line 6 to be what the earlier revision wrote, found it modified
	// base.obs:6:1 the earlier revision of line 6
}

func ExampleParseObservationTime() {
	// Z is +00:00 and is unambiguous, and an offset is carried as written.
	at, _ := dfcad.ParseObservationTime("2026-05-06T11:14:22+02:00")
	fmt.Println(at.UTC().Format(time.RFC3339))

	// A local time in a zone the file does not name denotes a different instant
	// depending on where it is read, so two records written that way cannot be
	// ordered.
	_, err := dfcad.ParseObservationTime("2026-05-06T09:14:22")

	var malformed dfcad.MalformedTimestampError
	if errors.As(err, &malformed) {
		fmt.Println(malformed.Reason, "-", malformed.Written)
	}

	// RFC 3339 spells an offset nobody knows -00:00, and a record whose instant
	// is unknown is not evidence of when anything was measured.
	_, err = dfcad.ParseObservationTime("2026-05-06T09:14:22-00:00")
	if errors.As(err, &malformed) {
		fmt.Println(malformed.Reason)
	}

	// Output:
	// 2026-05-06T09:14:22Z
	// no-offset - 2026-05-06T09:14:22
	// unknown-offset
}
