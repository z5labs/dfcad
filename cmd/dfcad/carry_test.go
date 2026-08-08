// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// carriedRegistry is the vocabulary the fixture below is authored against, and
// the arrangement is the whole point of it.
//
// The root is a survey grid whose coordinates are the size a projected
// system's are, and it is the frame carrying the coordinate reference system.
// Two plan grids hang off it by measured transforms, one per level, and every
// room is drawn at its own level's origin — which is how a building authored a
// plan at a time is written, and is the arrangement under which an export that
// wrote the corners as authored placed every room at the system's origin.
const carriedRegistry = `(project
  (label "Riverside example")
  (description "The sited two-storey model the carried export fixture is derived from.")
  (globalid-namespace "https://example.org/models/riverside"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))

(predicate crs
  (shape text)
  (claim-bearing #f)
  (description "The projected coordinate reference system the chain is rooted at."))

(predicate frame-transform
  (shape transform)
  (description "The rigid transform from a frame to its parent."))

(predicate position
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "The location of a vertex in its frame."))

(predicate arc-centre
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "The centre of the circle a curved edge runs along."))

(predicate arc-through
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "A point the curve of a curved edge passes through."))

(predicate clear-height
  (unit m)
  (shape scalar)
  (description "Floor to soffit height of a space."))

(tolerance corner
  (value 0.005 m)
  (description "How close two corners have to be to be one corner."))

(tolerance facet
  (value 0.1 m)
  (description "How far a segment standing in for a curve may fall from it."))

(frame frame:site-grid
  (label "Site survey grid")
  (unit m)
  (crs "EPSG:6543"))

(frame frame:plan-ground
  (label "Main floor plan grid")
  (unit m)
  (parent frame:site-grid)
  (transform site:C-0001)
  (frame-transform
    (id site:C-0001)
    (value
      (transform
        (translation 3502100.0 552000.0 0.0)
        (rotation 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0)
        (scale 1.0)))
    (source "Setting-out record SO-2026-014, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-02")))

(frame frame:plan-upstairs
  (label "Upstairs plan grid")
  (unit m)
  (parent frame:plan-ground)
  (transform site:C-0002)
  (frame-transform
    (id site:C-0002)
    (value
      (transform
        (translation 0.0 0.0 3.0)
        (rotation 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0)
        (scale 1.0)))
    (source "Setting-out record SO-2026-014, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-02")))

(type Parcel
  (kind Site)
  (geometry absent)
  (description "A plot of land."))

(type OfficeBuilding
  (kind Building)
  (geometry absent)
  (description "A building let as offices."))

(type Level
  (kind Storey)
  (geometry absent)
  (description "One floor of a building."))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings.")
  (classification "IFC4" "IfcSpace"))
`

// carriedGeometry is one room per level, each drawn at nought in the plan frame
// of the level it stands on.
//
// Not one corner of it is a coordinate of the system the model is sited in.
// Every number the artefact carries is therefore one this export worked out by
// walking the chain, which is what makes the fixture worth having.
const carriedGeometry = `
(vertex geom:V-401-A (frame frame:plan-ground) (position (value (0.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-401-B (frame frame:plan-ground) (position (value (4.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-401-C (frame frame:plan-ground) (position (value (4.0 3.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-401-D (frame frame:plan-ground) (position (value (0.0 3.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-401-AB (frame frame:plan-ground) (vertices geom:V-401-A geom:V-401-B))
(edge geom:E-401-BC (frame frame:plan-ground) (vertices geom:V-401-B geom:V-401-C))
(edge geom:E-401-CD (frame frame:plan-ground) (vertices geom:V-401-C geom:V-401-D))
(edge geom:E-401-DA (frame frame:plan-ground) (vertices geom:V-401-D geom:V-401-A))

(loop geom:L-401 (frame frame:plan-ground) (edges geom:E-401-AB geom:E-401-BC geom:E-401-CD geom:E-401-DA))

(vertex geom:V-402-A (frame frame:plan-upstairs) (position (value (0.0 0.0 0.0) m) (source "Interior control set IC-2") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-402-B (frame frame:plan-upstairs) (position (value (4.0 0.0 0.0) m) (source "Interior control set IC-2") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-402-C (frame frame:plan-upstairs) (position (value (4.0 3.0 0.0) m) (source "Interior control set IC-2") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-402-D (frame frame:plan-upstairs) (position (value (0.0 3.0 0.0) m) (source "Interior control set IC-2") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-402-AB (frame frame:plan-upstairs) (vertices geom:V-402-A geom:V-402-B))
(edge geom:E-402-BC (frame frame:plan-upstairs) (vertices geom:V-402-B geom:V-402-C))
(edge geom:E-402-CD (frame frame:plan-upstairs) (vertices geom:V-402-C geom:V-402-D))
(edge geom:E-402-DA (frame frame:plan-upstairs) (vertices geom:V-402-D geom:V-402-A))

(loop geom:L-402 (frame frame:plan-upstairs) (edges geom:E-402-AB geom:E-402-BC geom:E-402-CD geom:E-402-DA))
`

// carriedEntities is a building of two levels standing on a sited plot, each
// level declared in the plan frame its rooms were drawn in.
const carriedEntities = `(node site:P-01
  (label "Plot one")
  (kind Site)
  (type Parcel))

(node site:B-01
  (label "Block A")
  (kind Building)
  (type OfficeBuilding)
  (within site:P-01))

(node site:L-01
  (label "Main floor")
  (kind Storey)
  (type Level)
  (frame frame:plan-ground)
  (within site:B-01))

(node site:L-02
  (label "Upstairs")
  (kind Storey)
  (type Level)
  (frame frame:plan-upstairs)
  (within site:B-01))

(node site:S-01
  (label "Main floor meeting room")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:plan-ground)
  (within site:L-01)
  (boundary geom:L-401)
  (clear-height
    (value 2.7 m)
    (source "As-built check AB-2026-019, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.006 m))
    (date "2026-05-06")))

(node site:S-02
  (label "Upstairs meeting room")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:plan-upstairs)
  (within site:L-02)
  (boundary geom:L-402)
  (clear-height
    (value 2.4 m)
    (source "As-built check AB-2026-019, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.006 m))
    (date "2026-05-06")))
`

// carriedModel is the sited two-storey fixture tree both exports are run
// against.
func carriedModel() map[string]string {
	return map[string]string{
		"registry.dfc":        carriedRegistry,
		"geometry/levels.dfc": carriedGeometry,
		"entities/site.dfc":   carriedEntities,
	}
}

// carriedFlags is the vocabulary both exports read the fixture under, which is
// one list on purpose: the two commands disagreeing about where a model is was
// the fault, and a test which gave each its own vocabulary could reintroduce it
// without saying so.
func carriedFlags() []string {
	return []string{
		"--position", "position",
		"--tolerance", "corner",
		"--chord", "facet",
		"--arc-centre", "arc-centre",
		"--arc-through", "arc-through",
		"--crs", "crs",
	}
}

// exportCarried is the sited fixture exported with everything drawn.
func exportCarried(t *testing.T, files map[string]string) string {
	t.Helper()

	result, _, _ := exporting(t, exitSuccess, files, append(carriedFlags(), "--height", "clear-height")...)

	return artefact(t, result)
}

func TestRunExportCarriesGeometryAuthoredOnAChildFrameIntoTheRootFrame(t *testing.T) {
	got := exportCarried(t, carriedModel())

	assert.Equal(t, carriedGolden(t, got), got,
		"the sited artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
}

// carriedGolden is the recorded sited artefact, rewritten from got under
// -update.
func carriedGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/carried.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestRunExportWritesTheCornersTheFrameChainPutsThemAt is its own function
// because it is the story's central claim and it is about the numbers rather
// than about the file: a room drawn at its level's origin reaches the artefact
// at the coordinates the chain of measured transforms puts it at, and none of
// those coordinates is one anybody typed.
func TestRunExportWritesTheCornersTheFrameChainPutsThemAt(t *testing.T) {
	source := exportCarried(t, carriedModel())

	expected := [][2]float64{
		{3502100, 552000},
		{3502104, 552000},
		{3502104, 552003},
		{3502100, 552003},
	}

	corners := cornersOf(t, source)

	for _, at := range expected {
		assert.Contains(t, corners, at,
			"a corner of the sited plan reaches the artefact where the chain puts it")
	}

	assert.NotContains(t, corners, [2]float64{0, 0},
		"a corner left in the frame it was authored in is a room at the system's origin")
}

// TestRunExportAndExportMapAgreeOnWhereAModelIs is its own function because
// what it asserts is about two commands rather than about either of them. Both
// read one model under one vocabulary, and the corners in the two files are the
// same corners; the fault this story is about was the two disagreeing silently,
// which no assertion about either file on its own could have caught.
func TestRunExportAndExportMapAgreeOnWhereAModelIs(t *testing.T) {
	built := exportCarried(t, carriedModel())

	drawn, _, _ := mapping(t, exitSuccess, carriedModel(), carriedFlags()...)
	plan := mapArtefact(t, drawn)

	for _, at := range mapCornersOf(t, plan) {
		assert.Contains(t, cornersOf(t, built), at,
			"a corner the map places on the earth is the corner the model file holds")
	}
}

// TestRunExportWritesTheMapConversionTheCarryingLeaves is its own function
// because the conversion is a statement about the coordinates beside it. It is
// the identity, and it is the identity because the coordinates were carried
// into the frame the system is named on — not because a writer decided they
// must already be there.
func TestRunExportWritesTheMapConversionTheCarryingLeaves(t *testing.T) {
	source := exportCarried(t, carriedModel())

	require.Contains(t, source, "IFCPROJECTEDCRS('EPSG:6543',")

	for _, line := range strings.Split(source, "\n") {
		if !strings.Contains(line, "IFCMAPCONVERSION(") {
			continue
		}

		assert.Regexp(t, `IFCMAPCONVERSION\(#\d+,#\d+,0\.,0\.,0\.,\$,\$,\$\);$`, line,
			"nothing remains between the file's coordinates and the system once they are carried")

		return
	}

	t.Fatal("the artefact holds a map conversion")
}

// TestRunExportLeavesNoRoomStackedInsideAnotherOnASitedModel is its own
// function because carrying the corners and lifting the storey are two ways of
// moving the same room, and doing both is how a level ends up at twice its own
// height. The z ranges of the two levels meet exactly once.
func TestRunExportLeavesNoRoomStackedInsideAnotherOnASitedModel(t *testing.T) {
	source := exportCarried(t, carriedModel())

	ground := extent(t, source, "site:L-01")
	upstairs := extent(t, source, "site:L-02")

	assert.Equal(t, span{low: 0, high: 2.7}, ground,
		"the main floor stands on the building's datum and is as tall as it was measured")
	assert.Equal(t, span{low: 3, high: 5.4}, upstairs,
		"the upstairs stands at the lift its frame states, once and not twice")
}

// TestRunExportIsByteIdenticalForAnUnchangedSitedTree is its own function
// because carrying a corner is arithmetic done per run, and arithmetic done per
// run is where a number which is a property of the run rather than of the model
// gets in
// ([0021](docs/decisions/0021-an-export-is-a-build-output-keyed-by-its-source-digest.md)).
func TestRunExportIsByteIdenticalForAnUnchangedSitedTree(t *testing.T) {
	first := exportCarried(t, carriedModel())

	for range 4 {
		assert.Equal(t, first, exportCarried(t, carriedModel()))
	}
}

// TestRunExportRefusesGeometryTheFrameChainDoesNotReach is its own function
// because its assertions are a refusal's rather than a file's: silence is the
// failure mode this whole story is about, so a room whose frame reaches no
// datum writes no file at all rather than a file with the room at the system's
// origin.
func TestRunExportRefusesGeometryTheFrameChainDoesNotReach(t *testing.T) {
	files := carriedModel()
	for _, name := range []string{"geometry/levels.dfc", "entities/site.dfc"} {
		files[name] = strings.ReplaceAll(files[name], "frame:plan-upstairs", "frame:plan-attic")
	}

	result, root, stderr := exporting(t, exitCheck, files,
		append(carriedFlags(), "--height", "clear-height")...)

	assert.False(t, result.Derived)
	assert.Empty(t, result.Files)

	assert.Contains(t, stderr, "frame:plan-attic", "a refusal names the frame it could not walk")
	assert.Contains(t, stderr, "site:S-02", "and the node which was drawn on it")

	assert.NoDirExists(t, filepath.Join(root, dfcad.BuildDir, "export"),
		"an artefact is all or nothing, and nothing was produced")
}

// TestExporterCarryingRefusesWhatItCannotCarry is its own function because it
// is about the guard rather than about a run. The engine refuses most of these
// models earlier — a shape drawn on a frame the registry does not declare has
// no unit to be tessellated under — so the arms below are what stands between a
// model those gates let through and a file which says nothing about it.
func TestExporterCarryingRefusesWhatItCannotCarry(t *testing.T) {
	graph, diags := dfcad.LoadGraph(tree(t, carriedModel()))
	require.Empty(t, diags)

	node, held := graph.Node("site:S-02")
	require.True(t, held)

	testCases := []struct {
		name     string
		root     dfcad.ID
		rooted   bool
		from     dfcad.ID
		expected []string
	}{
		{
			name:     "refuses a model whose frames reach no root at all",
			from:     "frame:plan-upstairs",
			expected: []string{"site:S-02", "frame:plan-upstairs", "found none"},
		},
		{
			name:     "refuses a frame the chain of measured transforms does not relate to the root",
			root:     "frame:site-grid",
			rooted:   true,
			from:     "frame:plan-attic",
			expected: []string{"site:S-02", "frame:plan-attic", "frame:site-grid"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			out := &exporter{graph: graph, root: testCase.root, rooted: testCase.rooted}

			carry, carried := out.carrying(node, testCase.from)

			assert.False(t, carried)
			assert.Equal(t, carriage{}, carry, "a refusal carries nothing")

			require.Len(t, out.diags, 1, "one refusal names the frame, and not one per corner")
			assert.Equal(t, dfcad.SeverityError, out.diags[0].Severity)

			for _, expected := range testCase.expected {
				assert.Contains(t, out.diags[0].Message, expected)
			}
		})
	}
}

// TestExporterCarryingMovesOnlyWhatWasAuthoredElsewhere is its own function
// because what it asserts is the other half of the same guard, and the two
// answers have nothing in common: a drawing already in the root frame is
// handed back untouched, which is what keeps a model authored in one frame
// byte for byte the file it was.
func TestExporterCarryingMovesOnlyWhatWasAuthoredElsewhere(t *testing.T) {
	graph, diags := dfcad.LoadGraph(tree(t, carriedModel()))
	require.Empty(t, diags)

	node, held := graph.Node("site:S-02")
	require.True(t, held)

	out := &exporter{graph: graph, root: "frame:site-grid", rooted: true}

	t.Run("leaves a drawing already in the root frame still", func(t *testing.T) {
		carry, carried := out.carrying(node, "frame:site-grid")

		require.True(t, carried)
		assert.True(t, carry.still())

		at, err := carry.point(dfcad.Point{4, 3, 0})
		require.NoError(t, err)
		assert.Equal(t, dfcad.Point{4, 3, 0}, at)
	})

	t.Run("carries a drawing authored on a child frame by the transforms between them", func(t *testing.T) {
		carry, carried := out.carrying(node, "frame:plan-upstairs")

		require.True(t, carried)
		assert.False(t, carry.still())

		at, err := carry.point(dfcad.Point{4, 3, 0})
		require.NoError(t, err)
		assert.Equal(t, dfcad.Point{3502104, 552003, 3}, at)
	})

	assert.Empty(t, out.diags, "a chain which walks says nothing")
}

// cornerPoint is a two dimensional cartesian point of a STEP file.
var cornerPoint = regexp.MustCompile(`IFCCARTESIANPOINT\(\((-?[\d.]+),(-?[\d.]+)\)\)`)

// cornersOf is every corner a model file draws in plan, as the pairs of numbers
// it wrote them as.
//
// It reads the two dimensional points only. A three dimensional one is a
// placement rather than a corner of an outline, and the elevation those carry
// is what the storey test beside this reads.
func cornersOf(t *testing.T, source string) [][2]float64 {
	t.Helper()

	var out [][2]float64

	for _, found := range cornerPoint.FindAllStringSubmatch(source, -1) {
		out = append(out, [2]float64{real(t, found[1]), real(t, found[2])})
	}

	require.NotEmpty(t, out, "the artefact draws something in plan")

	return out
}

// mapPositionList is one run of ordinates of a GML geometry.
var mapPositionList = regexp.MustCompile(`<gml:posList>([^<]+)</gml:posList>`)

// mapCornersOf is every corner a map document draws, as pairs of ordinates in
// the easting-then-northing order that format writes.
func mapCornersOf(t *testing.T, source string) [][2]float64 {
	t.Helper()

	var out [][2]float64

	for _, found := range mapPositionList.FindAllStringSubmatch(source, -1) {
		ordinates := strings.Fields(found[1])
		require.Zero(t, len(ordinates)%2, "a position list holds whole positions")

		for i := 0; i+1 < len(ordinates); i += 2 {
			out = append(out, [2]float64{real(t, ordinates[i]), real(t, ordinates[i+1])})
		}
	}

	require.NotEmpty(t, out, "the document draws something")

	return out
}
