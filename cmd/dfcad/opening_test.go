// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// openingRegistry is the vocabulary of the model the story this came from
// measured: one wall, with a door at its foot and a window on a sill.
//
// The door and the window say they fill an opening in the element they are
// within, and that is the only thing marking them: nothing here reads that one
// is classified IfcDoor, and a type called anything at all which said the same
// would be cut into its wall the same way.
const openingRegistry = `(project
  (label "Opening repro")
  (description "One wall with a door and a window set in it.")
  (globalid-namespace "https://example.org/opening-repro"))

(namespace frame (description "Coordinate frames."))
(namespace geom (description "Geometric nodes."))
(namespace method (description "Measurement methods."))
(namespace site (description "Semantic nodes."))

(predicate position (unit usft) (shape coordinate) (dimension 3)
  (description "Where a vertex is."))
(predicate height (unit usft) (shape scalar) (description "How tall, from where it is drawn."))
(predicate thickness (unit usft) (shape scalar) (description "How thick a run is built."))
(predicate sill (unit usft) (shape scalar) (description "How far a body starts above its run."))

(tolerance corner (value 0.01 usft) (description "Two corners closer than this are one."))
(tolerance chord (value 0.05 usft) (description "How far a chord may fall from its curve."))

(frame frame:building (label "Building grid") (unit usft))

(type Lot (kind Site) (geometry absent) (description "A plot of land."))
(type House (kind Building) (geometry absent) (description "A house."))
(type Level (kind Storey) (geometry absent) (description "One floor of a house."))
(type Wall (kind Element) (geometry line) (description "A wall, as its centreline.")
  (classification "IFC4" "IfcWall"))
(type Door (kind Element) (geometry line) (description "A door, along its wall's centreline.")
  (classification "IFC4" "IfcDoor")
  (fills-opening #t))
(type Window (kind Element) (geometry line) (description "A window, drawn at its sill.")
  (classification "IFC4" "IfcWindow")
  (fills-opening #t))
`

// openingGeometry is the wall's run, the door's along it at its foot, and the
// window's along it at its sill.
//
// The window is drawn at the height it starts at, so this fixture stands
// without an offset claimed of anything; [withTheWindowOnItsWallsRun] is the
// same window drawn at the floor with its sill claimed instead.
const openingGeometry = `(vertex geom:V-W1 (frame frame:building)
  (position (value (0.0 0.0 0.0) usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25")))
(vertex geom:V-W2 (frame frame:building)
  (position (value (15.0 0.0 0.0) usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25")))
(edge geom:E-W (frame frame:building) (vertices geom:V-W1 geom:V-W2))
(loop geom:L-W (frame frame:building) (edges geom:E-W))

(vertex geom:V-D1 (frame frame:building)
  (position (value (5.0 0.0 0.0) usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25")))
(vertex geom:V-D2 (frame frame:building)
  (position (value (8.0 0.0 0.0) usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25")))
(edge geom:E-D (frame frame:building) (vertices geom:V-D1 geom:V-D2))
(loop geom:L-D (frame frame:building) (edges geom:E-D))

(vertex geom:V-N1 (frame frame:building)
  (position (value (10.0 0.0 3.0) usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25")))
(vertex geom:V-N2 (frame frame:building)
  (position (value (13.0 0.0 3.0) usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25")))
(edge geom:E-N (frame frame:building) (vertices geom:V-N1 geom:V-N2))
(loop geom:L-N (frame frame:building) (edges geom:E-N))
`

// openingEntities is the storey, the wall in it, and the door and the window
// within the wall.
const openingEntities = `(node site:S-01 (label "Lot 12") (kind Site) (type Lot))
(node site:B-01 (label "House") (kind Building) (type House) (within site:S-01))
(node site:L-01 (label "Main floor") (kind Storey) (type Level)
  (frame frame:building) (within site:B-01))

(node site:W-01 (label "Front wall") (kind Element) (type Wall)
  (geometry line) (frame frame:building) (within site:L-01) (boundary geom:L-W)
  (height (value 9.0 usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25"))
  (thickness (value 0.5 usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25")))

(node site:D-01 (label "Front door") (kind Element) (type Door)
  (geometry line) (frame frame:building) (within site:W-01) (boundary geom:L-D)
  (height (value 6.667 usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25"))
  (thickness (value 0.125 usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25")))

(node site:N-01 (label "Front window") (kind Element) (type Window)
  (geometry line) (frame frame:building) (within site:W-01) (boundary geom:L-N)
  (height (value 3.833 usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25"))
  (thickness (value 0.3 usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25")))
`

// openingModel is the fixture tree the opening export is run against.
func openingModel() map[string]string {
	return map[string]string{
		"registry.dfc": openingRegistry,
		"geometry.dfc": openingGeometry,
		"entities.dfc": openingEntities,
	}
}

// openingFlags is the vocabulary the fixture is read under, exactly as the
// story's measurement ran it.
func openingFlags() []string {
	return []string{
		"--position", "position", "--tolerance", "corner", "--chord", "chord",
		"--height", "height", "--thickness", "thickness",
	}
}

// exportOpenings runs export over a fixture and returns the artefact it wrote
// and what it said on stderr.
func exportOpenings(t *testing.T, files map[string]string, args ...string) (string, string) {
	t.Helper()

	result, _, stderr := exporting(t, exitSuccess, files, args...)
	require.True(t, result.Derived, stderr)

	return artefact(t, result), stderr
}

// openingEdit is the fixture with one piece of its text replaced, which has to
// be there to replace.
func openingEdit(t *testing.T, file, old, replacement string) map[string]string {
	t.Helper()

	files := openingModel()

	edited := strings.Replace(files[file], old, replacement, 1)
	require.NotEqual(t, files[file], edited, "the fixture holds %q", old)
	files[file] = edited

	return files
}

func TestRunExportCutsAnOpeningForEveryElementSetInAnother(t *testing.T) {
	got, _ := exportOpenings(t, openingModel(), openingFlags()...)

	assert.Equal(t, openingGolden(t, got), got,
		"the opening artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
}

// openingGolden is the recorded opening artefact, rewritten from got under
// -update.
func openingGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/openings.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

func TestRunExportOfOpeningsIsAFunctionOfTheModel(t *testing.T) {
	first, _ := exportOpenings(t, openingModel(), openingFlags()...)

	for range 4 {
		again, _ := exportOpenings(t, openingModel(), openingFlags()...)
		assert.Equal(t, first, again)
	}
}

// namedIn is the reference of the one instance whose Name is the id given.
func namedIn(t *testing.T, source, id string) string {
	t.Helper()

	for at, held := range parsed(t, source) {
		if strings.HasPrefix(held.keyword, "IFCREL") || len(held.attributes) < 3 {
			continue
		}
		if held.attributes[2] == "'"+id+"'" {
			return at
		}
	}

	t.Fatalf("the file holds an object named %s", id)

	return ""
}

// relatedBy is every instance of one relationship entity, as the two references
// it relates: the relating one and the related one, or the list of them.
func relatedBy(t *testing.T, source, keyword string) [][2]string {
	t.Helper()

	var out [][2]string
	for _, held := range parsed(t, source) {
		if held.keyword == keyword {
			out = append(out, [2]string{held.attributes[4], held.attributes[5]})
		}
	}

	return out
}

// voidOf is the opening a filling fills and the element that opening voids,
// found by following the two relationships from the filling.
func voidOf(t *testing.T, source, filler string) (string, string) {
	t.Helper()

	at := namedIn(t, source, filler)

	opening := ""
	for _, fills := range relatedBy(t, source, "IFCRELFILLSELEMENT") {
		if fills[1] == at {
			require.Empty(t, opening, "%s fills one opening", filler)
			opening = fills[0]
		}
	}
	require.NotEmpty(t, opening, "%s fills an opening", filler)

	host := ""
	for _, voids := range relatedBy(t, source, "IFCRELVOIDSELEMENT") {
		if voids[1] == opening {
			require.Empty(t, host, "one element is voided by the opening %s fills", filler)
			host = voids[0]
		}
	}
	require.NotEmpty(t, host, "the opening %s fills voids something", filler)

	return opening, host
}

// containedIn is every reference contained in the spatial structure.
func containedIn(t *testing.T, source string) []string {
	t.Helper()

	var out []string
	for _, contains := range relatedBy(t, source, "IFCRELCONTAINEDINSPATIALSTRUCTURE") {
		out = append(out, split(strings.TrimSuffix(strings.TrimPrefix(contains[0], "("), ")"))...)
	}

	return out
}

func TestRunExportRelatesAFillingToTheElementItIsWithin(t *testing.T) {
	source, _ := exportOpenings(t, openingModel(), openingFlags()...)

	wall := namedIn(t, source, "site:W-01")

	for _, filler := range []string{"site:D-01", "site:N-01"} {
		t.Run("voids the wall with the opening "+filler+" fills", func(t *testing.T) {
			opening, host := voidOf(t, source, filler)

			assert.Equal(t, "IFCOPENINGELEMENT", instance(t, source, opening).keyword)
			assert.Equal(t, wall, host)
		})

		t.Run("keeps "+filler+" contained in its storey", func(t *testing.T) {
			assert.Contains(t, containedIn(t, source), namedIn(t, source, filler))
		})
	}

	t.Run("does not contain an opening in the spatial structure", func(t *testing.T) {
		for _, filler := range []string{"site:D-01", "site:N-01"} {
			opening, _ := voidOf(t, source, filler)
			assert.NotContains(t, containedIn(t, source), opening)
		}
	})

	t.Run("aggregates nothing into the wall its fillings stand in", func(t *testing.T) {
		for _, aggregates := range relatedBy(t, source, "IFCRELAGGREGATES") {
			assert.NotEqual(t, wall, aggregates[0])
		}
	})
}

// box is the extent of a set of swept solids, in the coordinates of the file,
// and the volume they sweep between them.
type box struct {
	low, high [3]float64
	volume    float64
}

// sweptBox resolves the body of one product or opening back out of the file:
// every profile's corners, moved by the position its solid is swept from and
// by the placement chain the object hangs off.
func sweptBox(t *testing.T, source, at string) box {
	t.Helper()

	held := instance(t, source, at)
	require.GreaterOrEqual(t, len(held.attributes), 7)

	lift := elevationOf(t, source, held.attributes[5])

	out := box{
		low:  [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)},
		high: [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)},
	}

	bodies := solids(t, source, held.attributes[6])
	require.NotEmpty(t, bodies, "%s carries a body", at)

	for _, solid := range bodies {
		profile := instance(t, source, solid.attributes[0])
		require.Equal(t, "IFCARBITRARYCLOSEDPROFILEDEF", profile.keyword)

		line := instance(t, source, profile.attributes[2])
		require.Equal(t, "IFCPOLYLINE", line.keyword)

		var corners [][2]float64
		for _, point := range split(strings.TrimSuffix(strings.TrimPrefix(line.attributes[0], "("), ")")) {
			written := split(strings.TrimSuffix(strings.TrimPrefix(instance(t, source, point).attributes[0], "("), ")"))
			require.Len(t, written, 2)

			corner := [2]float64{real(t, written[0]), real(t, written[1])}
			corners = append(corners, corner)

			for axis := range 2 {
				out.low[axis] = math.Min(out.low[axis], corner[axis])
				out.high[axis] = math.Max(out.high[axis], corner[axis])
			}
		}

		bottom := lift + elevationOf(t, source, solid.attributes[1])
		depth := real(t, solid.attributes[3])

		out.low[2] = math.Min(out.low[2], bottom)
		out.high[2] = math.Max(out.high[2], bottom+depth)

		// The shoelace over the closed ring: the profile's area, which the
		// depth sweeps into the solid's volume.
		area := 0.0
		for i := 0; i+1 < len(corners); i++ {
			area += corners[i][0]*corners[i+1][1] - corners[i+1][0]*corners[i][1]
		}
		out.volume += math.Abs(area) / 2 * depth
	}

	return out
}

func TestRunExportCutsTheOpeningThroughTheWholeOfItsHost(t *testing.T) {
	source, _ := exportOpenings(t, openingModel(), openingFlags()...)

	testCases := []struct {
		name     string
		filler   string
		expected box
	}{
		{
			name:   "cuts the door's opening through the wall's thickness, not the door's, from the floor",
			filler: "site:D-01",
			expected: box{
				low:    [3]float64{5, -0.25, 0},
				high:   [3]float64{8, 0.25, 6.667},
				volume: 10.0005,
			},
		},
		{
			name:   "cuts the window's opening from its sill through its own height",
			filler: "site:N-01",
			expected: box{
				low:    [3]float64{10, -0.25, 3},
				high:   [3]float64{13, 0.25, 6.833},
				volume: 5.7495,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			opening, _ := voidOf(t, source, testCase.filler)

			got := sweptBox(t, source, opening)

			for axis := range 3 {
				assert.InDelta(t, testCase.expected.low[axis], got.low[axis], 1e-9, "low %d", axis)
				assert.InDelta(t, testCase.expected.high[axis], got.high[axis], 1e-9, "high %d", axis)
			}
			assert.InDelta(t, testCase.expected.volume, got.volume, 1e-9)
		})
	}

	t.Run("leaves the wall 51.75 once both are taken out of it", func(t *testing.T) {
		wall := sweptBox(t, source, namedIn(t, source, "site:W-01"))
		door, _ := voidOf(t, source, "site:D-01")
		window, _ := voidOf(t, source, "site:N-01")

		// Both openings lie wholly inside the wall — the extents above say so
		// — so what a reader subtracting them is left with is the difference.
		cut := wall.volume - sweptBox(t, source, door).volume - sweptBox(t, source, window).volume

		assert.InDelta(t, 67.5, wall.volume, 1e-9)
		assert.InDelta(t, 51.75, cut, 1e-9)
	})
}

// withTheWindowOnItsWallsRun is the fixture with the window drawn where its
// wall's run lies and its sill claimed of it instead, which is how a model
// authored a run at a time draws one.
func withTheWindowOnItsWallsRun(t *testing.T) map[string]string {
	t.Helper()

	files := openingEdit(t, "geometry.dfc", "(10.0 0.0 3.0)", "(10.0 0.0 0.0)")

	geometry := strings.Replace(files["geometry.dfc"], "(13.0 0.0 3.0)", "(13.0 0.0 0.0)", 1)
	require.NotEqual(t, files["geometry.dfc"], geometry)
	files["geometry.dfc"] = geometry

	entities := strings.Replace(files["entities.dfc"], `(thickness (value 0.3 usft)`,
		`(sill (value 3.0 usft) (source "Elevation") (method method:tape) (date "2026-09-25"))
  (thickness (value 0.3 usft)`, 1)
	require.NotEqual(t, files["entities.dfc"], entities)
	files["entities.dfc"] = entities

	return files
}

func TestRunExportCutsAnOpeningFromTheBaseTheFillingsOffsetMovesItTo(t *testing.T) {
	source, _ := exportOpenings(t, withTheWindowOnItsWallsRun(t), append(openingFlags(), "--offset", "sill")...)

	opening, _ := voidOf(t, source, "site:N-01")

	got := sweptBox(t, source, opening)

	assert.InDelta(t, 3, got.low[2], 1e-9)
	assert.InDelta(t, 6.833, got.high[2], 1e-9)
}

func TestRunExportAggregatesAnElementWithinAnotherWhoseTypeFillsNothing(t *testing.T) {
	files := openingEdit(t, "registry.dfc",
		`(classification "IFC4" "IfcDoor")
  (fills-opening #t)`, `(classification "IFC4" "IfcDoor")`)

	source, _ := exportOpenings(t, files, openingFlags()...)

	wall := namedIn(t, source, "site:W-01")
	door := namedIn(t, source, "site:D-01")

	t.Run("aggregates it into the element it is within", func(t *testing.T) {
		var parts []string
		for _, aggregates := range relatedBy(t, source, "IFCRELAGGREGATES") {
			if aggregates[0] == wall {
				parts = append(parts, split(strings.TrimSuffix(strings.TrimPrefix(aggregates[1], "("), ")"))...)
			}
		}

		assert.Equal(t, []string{door}, parts)
	})

	t.Run("does not contain it in the storey as well", func(t *testing.T) {
		assert.NotContains(t, containedIn(t, source), door)
		assert.Contains(t, containedIn(t, source), wall)
	})

	t.Run("cuts no opening for it", func(t *testing.T) {
		for _, fills := range relatedBy(t, source, "IFCRELFILLSELEMENT") {
			assert.NotEqual(t, door, fills[1])
		}
	})

	t.Run("still draws it where the model puts it", func(t *testing.T) {
		got := sweptBox(t, source, door)
		assert.InDelta(t, 6.667, got.high[2], 1e-9)
	})

	t.Run("still cuts the window, whose type says it fills one", func(t *testing.T) {
		_, host := voidOf(t, source, "site:N-01")
		assert.Equal(t, wall, host)
	})
}

// TestRunExportReadsWhatMarksAFillingFromTheModelAlone is the constraint the
// mechanism was designed under, asserted rather than intended: a type which
// says it fills an opening is cut into its host whatever it is classified as,
// and one classified as a door which says nothing is not.
func TestRunExportReadsWhatMarksAFillingFromTheModelAlone(t *testing.T) {
	testCases := []struct {
		name     string
		files    func(t *testing.T) map[string]string
		filler   string
		expected bool
	}{
		{
			name: "cuts an opening for a type classified as nothing in particular",
			files: func(t *testing.T) map[string]string {
				return openingEdit(t, "registry.dfc", `(classification "IFC4" "IfcDoor")`,
					`(classification "IFC4" "IfcBuildingElementProxy")`)
			},
			filler:   "site:D-01",
			expected: true,
		},
		{
			name: "cuts none for a type classified as a window which does not say it fills one",
			files: func(t *testing.T) map[string]string {
				return openingEdit(t, "registry.dfc", `(classification "IFC4" "IfcWindow")
  (fills-opening #t)`, `(classification "IFC4" "IfcWindow")`)
			},
			filler:   "site:N-01",
			expected: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			source, _ := exportOpenings(t, testCase.files(t), openingFlags()...)

			at := namedIn(t, source, testCase.filler)

			fills := false
			for _, relationship := range relatedBy(t, source, "IFCRELFILLSELEMENT") {
				fills = fills || relationship[1] == at
			}

			assert.Equal(t, testCase.expected, fills)
		})
	}
}

func TestRunExportRefusesAFillingOffItsHostsRun(t *testing.T) {
	testCases := []struct {
		name  string
		files func(t *testing.T) map[string]string
	}{
		{
			name: "a door drawn beside its wall rather than along it",
			files: func(t *testing.T) map[string]string {
				files := openingEdit(t, "geometry.dfc", "(5.0 0.0 0.0)", "(5.0 1.0 0.0)")
				files["geometry.dfc"] = strings.Replace(files["geometry.dfc"], "(8.0 0.0 0.0)", "(8.0 1.0 0.0)", 1)
				return files
			},
		},
		{
			name: "a door running past the end of its wall",
			files: func(t *testing.T) map[string]string {
				return openingEdit(t, "geometry.dfc", "(8.0 0.0 0.0)", "(18.0 0.0 0.0)")
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, _, stderr := exporting(t, exitCheck, testCase.files(t), openingFlags()...)

			assert.False(t, result.Derived)
			assert.Empty(t, result.Files)
			assert.Contains(t, stderr, "site:D-01")
			assert.Contains(t, stderr, "site:W-01")
		})
	}
}

func TestRunExportGivesEveryOpeningAnIdentifierOfItsOwn(t *testing.T) {
	result, _, _ := exporting(t, exitSuccess, openingModel(), append(openingFlags(), "--evidence")...)
	source := artefact(t, result)

	const url = "https://example.org/opening-repro"

	for _, filler := range []string{"site:D-01", "site:N-01"} {
		t.Run("derives the opening "+filler+" fills from its id and the pinned URL", func(t *testing.T) {
			opening, _ := voidOf(t, source, filler)

			derived := dfcad.DeriveGlobalID(url, dfcad.ID("ifc/opening/"+filler))

			assert.Equal(t, "'"+string(derived)+"'", instance(t, source, opening).attributes[0])
			assert.NotEqual(t, dfcad.DeriveGlobalID(url, dfcad.ID(filler)), derived)
		})
	}

	t.Run("accounts for every one of them in the manifest", func(t *testing.T) {
		var names []string
		for _, identifier := range result.Identifiers {
			names = append(names, identifier.ID)
		}

		for _, name := range []string{
			"ifc/opening/site:D-01", "ifc/voids/site:D-01", "ifc/fills/site:D-01",
			"ifc/opening/site:N-01", "ifc/voids/site:N-01", "ifc/fills/site:N-01",
		} {
			assert.Contains(t, names, name)
		}
	})
}

// TestRunExportRelatesAFillingWithoutDrawingAnything is its own function
// because it is the relationship half of the story on its own: a run which
// draws nothing still says which element each door stands in, and says it
// without a void to cut.
func TestRunExportRelatesAFillingWithoutDrawingAnything(t *testing.T) {
	source, _ := exportOpenings(t, openingModel())

	for _, filler := range []string{"site:D-01", "site:N-01"} {
		opening, host := voidOf(t, source, filler)

		assert.Equal(t, namedIn(t, source, "site:W-01"), host)
		assert.Equal(t, "$", instance(t, source, opening).attributes[6], "an opening nothing drew has no shape")
	}
}

func TestRunExportSaysSoWhereAnOpeningHasNothingToBeCutTo(t *testing.T) {
	files := openingEdit(t, "entities.dfc", `(height (value 6.667 usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25"))
`, "")

	source, stderr := exportOpenings(t, files, openingFlags()...)

	t.Run("warns naming the filling and its host", func(t *testing.T) {
		assert.Contains(t, stderr, "site:D-01")
		assert.Contains(t, stderr, "site:W-01")
	})

	t.Run("still relates the two, without a shape", func(t *testing.T) {
		opening, host := voidOf(t, source, "site:D-01")

		assert.Equal(t, namedIn(t, source, "site:W-01"), host)
		assert.Equal(t, "$", instance(t, source, opening).attributes[6])
	})
}

func TestRunExportCutsNothingOutOfAHostWithNoBody(t *testing.T) {
	files := openingEdit(t, "entities.dfc", `(height (value 9.0 usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25"))
`, "")

	source, stderr := exportOpenings(t, files, openingFlags()...)

	for _, filler := range []string{"site:D-01", "site:N-01"} {
		opening, _ := voidOf(t, source, filler)
		assert.Equal(t, "$", instance(t, source, opening).attributes[6])
	}

	assert.NotContains(t, stderr, "warning", "a wall with no solid has nothing missing from it")
}
