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
