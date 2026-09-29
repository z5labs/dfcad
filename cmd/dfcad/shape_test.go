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

// shapeRegistry is the vocabulary the drawn fixture below is authored against.
//
// Everything a shape is read under is in it and nothing is compiled in: the
// predicate a corner's position is claimed under, the predicate a room's
// height is claimed under, the two an arc is written in, and the two
// tolerances — how close two corners have to be to be one corner, and how far
// a segment standing in for a curve may fall from it.
const shapeRegistry = `(project
  (label "Riverside example")
  (description "The model the drawn export fixture is derived from.")
  (globalid-namespace "https://example.org/models/riverside"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
(namespace survey (description "Claim ids issued by Acme Surveys."))

(frame frame:building (label "Building local grid") (unit m))

(tolerance corner
  (value 0.005 m)
  (description "How close two corners have to be to be one corner."))

(tolerance facet
  (value 0.1 m)
  (description "How far a segment standing in for a curve may fall from it."))

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

(type Parcel
  (kind Site)
  (geometry absent)
  (description "A plot of land.")
  (classification "IFC4" "IfcSite"))

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

// shapeGeometry is the outline of every room in the fixture: the corners, the
// edges between them, and the rings each room is bounded by.
//
// The four rooms are the four cases a drawn export has to answer: a plain
// rectangle, a second one nothing states the height of, one with a courtyard
// taken out of it, and one whose east wall bows outwards along an arc.
const shapeGeometry = `
(vertex geom:V-201-A (frame frame:building) (position (value (0.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-201-B (frame frame:building) (position (value (4.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-201-C (frame frame:building) (position (value (4.0 3.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-201-D (frame frame:building) (position (value (0.0 3.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-201-AB (frame frame:building) (vertices geom:V-201-A geom:V-201-B))
(edge geom:E-201-BC (frame frame:building) (vertices geom:V-201-B geom:V-201-C))
(edge geom:E-201-CD (frame frame:building) (vertices geom:V-201-C geom:V-201-D))
(edge geom:E-201-DA (frame frame:building) (vertices geom:V-201-D geom:V-201-A))

(loop geom:L-201 (frame frame:building) (edges geom:E-201-AB geom:E-201-BC geom:E-201-CD geom:E-201-DA))

(vertex geom:V-202-A (frame frame:building) (position (value (6.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-202-B (frame frame:building) (position (value (10.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-202-C (frame frame:building) (position (value (10.0 3.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-202-D (frame frame:building) (position (value (6.0 3.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-202-AB (frame frame:building) (vertices geom:V-202-A geom:V-202-B))
(edge geom:E-202-BC (frame frame:building) (vertices geom:V-202-B geom:V-202-C))
(edge geom:E-202-CD (frame frame:building) (vertices geom:V-202-C geom:V-202-D))
(edge geom:E-202-DA (frame frame:building) (vertices geom:V-202-D geom:V-202-A))

(loop geom:L-202 (frame frame:building) (edges geom:E-202-AB geom:E-202-BC geom:E-202-CD geom:E-202-DA))

(vertex geom:V-203-A (frame frame:building) (position (value (12.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-203-B (frame frame:building) (position (value (20.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-203-C (frame frame:building) (position (value (20.0 8.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-203-D (frame frame:building) (position (value (12.0 8.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-203-E (frame frame:building) (position (value (14.0 2.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-203-F (frame frame:building) (position (value (18.0 2.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-203-G (frame frame:building) (position (value (18.0 6.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-203-H (frame frame:building) (position (value (14.0 6.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-203-AB (frame frame:building) (vertices geom:V-203-A geom:V-203-B))
(edge geom:E-203-BC (frame frame:building) (vertices geom:V-203-B geom:V-203-C))
(edge geom:E-203-CD (frame frame:building) (vertices geom:V-203-C geom:V-203-D))
(edge geom:E-203-DA (frame frame:building) (vertices geom:V-203-D geom:V-203-A))
(edge geom:E-203-EF (frame frame:building) (vertices geom:V-203-E geom:V-203-F))
(edge geom:E-203-FG (frame frame:building) (vertices geom:V-203-F geom:V-203-G))
(edge geom:E-203-GH (frame frame:building) (vertices geom:V-203-G geom:V-203-H))
(edge geom:E-203-HE (frame frame:building) (vertices geom:V-203-H geom:V-203-E))

(loop geom:L-203 (frame frame:building) (edges geom:E-203-AB geom:E-203-BC geom:E-203-CD geom:E-203-DA))
(loop geom:L-203H (frame frame:building) (edges geom:E-203-EF geom:E-203-FG geom:E-203-GH geom:E-203-HE))

(vertex geom:V-204-A (frame frame:building) (position (value (0.0 10.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-204-B (frame frame:building) (position (value (4.0 10.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-204-C (frame frame:building) (position (value (4.0 14.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-204-D (frame frame:building) (position (value (0.0 14.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-204-AB (frame frame:building) (vertices geom:V-204-A geom:V-204-B))
(edge geom:E-204-BC
  (frame frame:building)
  (vertices geom:V-204-B geom:V-204-C)
  (arc-centre
    (value (2.0 12.0 0.0) m)
    (source "Setting-out record SO-2026-014")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18"))
  (arc-through
    (value (4.82842712474619 12.0 0.0) m)
    (source "Setting-out record SO-2026-014")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))
(edge geom:E-204-CD (frame frame:building) (vertices geom:V-204-C geom:V-204-D))
(edge geom:E-204-DA (frame frame:building) (vertices geom:V-204-D geom:V-204-A))

(loop geom:L-204 (frame frame:building) (edges geom:E-204-AB geom:E-204-BC geom:E-204-CD geom:E-204-DA))
`

// shapeEntities is the spatial structure the rooms above hang off, and the
// heights three of them are claimed to have.
//
// The three claims are deliberately unalike. One is surveyed, ranked and
// carries an id; one is taken off a drawing and states no accuracy at all,
// which the resolution rule reports as unranked; and one is nothing, because
// Meeting Room B's height has never been measured. Telling those apart from
// the exported file is what the property set beside each body is for.
const shapeEntities = `(node site:P-01
  (label "Plot one")
  (kind Site)
  (type Parcel))

(node site:B-01
  (label "Block A")
  (kind Building)
  (type OfficeBuilding)
  (within site:P-01))

(node site:L-01
  (label "Level one")
  (kind Storey)
  (type Level)
  (within site:B-01))

(node site:S-201
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-201)
  (clear-height
    (id survey:H-201)
    (value 2.7 m)
    (source "As-built check AB-2026-019, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.006 m))
    (date "2026-05-06")))

(node site:S-202
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-202))

(node site:S-203
  (label "Meeting Room C")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-203)
  (boundary geom:L-203H)
  (clear-height
    (value 3.0 m)
    (source "Fit-out drawing FD-2026-011")
    (method method:drawing-take-off)
    (date "2026-03-02")))

(node site:S-204
  (label "Meeting Room D")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-204)
  (clear-height
    (value 2.4 m)
    (source "As-built check AB-2026-019, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.006 m))
    (date "2026-05-06")))
`

// shapeModel is the fixture tree the drawn export is run against.
func shapeModel() map[string]string {
	return map[string]string{
		"registry.dfc":          shapeRegistry,
		"geometry/level-01.dfc": shapeGeometry,
		"entities/site.dfc":     shapeEntities,
	}
}

// drawingFlags is the vocabulary every drawn run below reads the model under.
func drawingFlags() []string {
	return []string{
		"--position", "position",
		"--tolerance", "corner",
		"--chord", "facet",
		"--arc-centre", "arc-centre",
		"--arc-through", "arc-through",
	}
}

// exportDrawn runs export over the fixture with the flags given on top of that
// vocabulary, and returns the artefact it wrote.
func exportDrawn(t *testing.T, args ...string) string {
	t.Helper()

	result, _, _ := exporting(t, exitSuccess, shapeModel(), append(drawingFlags(), args...)...)

	return artefact(t, result)
}

func TestRunExportDrawsEverySpaceItCan(t *testing.T) {
	got := exportDrawn(t, "--height", "clear-height")

	assert.Equal(t, shapeGolden(t, got), got,
		"the drawn artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
}

// shapeGolden is the recorded drawn artefact, rewritten from got under
// -update.
func shapeGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/shapes.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

func TestRunExportWritesTheOutlineTheModelStates(t *testing.T) {
	source := exportDrawn(t, "--height", "clear-height")

	testCases := []struct {
		name     string
		expected string
	}{
		{
			name:     "declares a plan view for the outlines",
			expected: "IFCGEOMETRICREPRESENTATIONSUBCONTEXT('FootPrint','Model',*,*,*,*,",
		},
		{
			name:     "declares a model view for the solids",
			expected: "IFCGEOMETRICREPRESENTATIONSUBCONTEXT('Body','Model',*,*,*,*,",
		},
		{
			name:     "writes the outline as a two dimensional curve",
			expected: "'FootPrint','Curve2D'",
		},
		{
			name:     "writes the body as a swept solid",
			expected: "'Body','SweptSolid'",
		},
		{
			name:     "sweeps a profile rather than drawing a mesh",
			expected: "IFCEXTRUDEDAREASOLID(",
		},
		{
			name:     "takes the courtyard out of the profile as an inner curve",
			expected: "IFCARBITRARYPROFILEDEFWITHVOIDS(.AREA.,",
		},
		{
			name:     "leaves a room with no hole in it as a plain closed profile",
			expected: "IFCARBITRARYCLOSEDPROFILEDEF(.AREA.,",
		},
		{
			name:     "records where the height behind a body came from",
			expected: "'dfcad_HeightProvenance'",
		},
		{
			name:     "names the predicate the height was claimed under",
			expected: "IFCPROPERTYSINGLEVALUE('Predicate',$,IFCTEXT('clear-height'),$)",
		},
		{
			name:     "carries the source of a surveyed height into the file",
			expected: "IFCTEXT('As-built check AB-2026-019, Acme Surveys')",
		},
		{
			name:     "carries the accuracy the height is known to",
			expected: "IFCTEXT('independent 0.006 m')",
		},
		{
			name:     "says which step of the resolution rule chose the claim",
			expected: "IFCPROPERTYSINGLEVALUE('Reason',$,IFCTEXT('only'),$)",
		},
		{
			name:     "says so where nothing rankable was claimed of the height",
			expected: "IFCPROPERTYSINGLEVALUE('Reason',$,IFCTEXT('unranked'),$)",
		},
		{
			name:     "attaches each property set to the space it was read from",
			expected: "IFCRELDEFINESBYPROPERTIES(",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Contains(t, source, testCase.expected)
		})
	}
}

// TestRunExportWithoutTheDrawingVocabularyWritesNoShapeAtAll is its own
// function because it is about a whole file rather than about a line of one:
// a spatial export is a correct IFC file and is what this command wrote before
// it could draw anything, so naming none of the geometry flags has to leave it
// exactly as it was.
func TestRunExportWithoutTheDrawingVocabularyWritesNoShapeAtAll(t *testing.T) {
	result, _, _ := exporting(t, exitSuccess, shapeModel())
	source := artefact(t, result)

	for _, absent := range []string{
		"IFCPRODUCTDEFINITIONSHAPE",
		"IFCSHAPEREPRESENTATION",
		"IFCGEOMETRICREPRESENTATIONSUBCONTEXT",
		"IFCPOLYLINE",
		"IFCEXTRUDEDAREASOLID",
		"IFCPROPERTYSET",
	} {
		assert.NotContains(t, source, absent)
	}
}

// TestRunExportWithoutAHeightPredicateWritesFootprintsOnly is its own function
// because it is the story's central case: a model of plan polygons and no
// heights exports as the outlines it actually states, rather than failing or
// inventing a height nobody claimed.
func TestRunExportWithoutAHeightPredicateWritesFootprintsOnly(t *testing.T) {
	source := exportDrawn(t)

	assert.Contains(t, source, "'FootPrint','Curve2D'")
	assert.NotContains(t, source, "'Body','SweptSolid'")
	assert.NotContains(t, source, "IFCEXTRUDEDAREASOLID")
	assert.NotContains(t, source, "IFCPROPERTYSET")
}

// TestRunExportWritesNoBodyForASpaceNothingClaimsAHeightOf is its own function
// because what it asserts is a difference between two rooms of one file rather
// than a property of the file: the count of solids is one short of the count of
// outlines, and the room missing one is the room nobody has measured.
func TestRunExportWritesNoBodyForASpaceNothingClaimsAHeightOf(t *testing.T) {
	source := exportDrawn(t, "--height", "clear-height")

	assert.Equal(t, 4, strings.Count(source, "'FootPrint','Curve2D'"),
		"every space carries the outline its model states")
	assert.Equal(t, 3, strings.Count(source, "'Body','SweptSolid'"),
		"the room nothing claims a height of carries no body")
	assert.Equal(t, 3, strings.Count(source, "IFCPROPERTYSET("),
		"a body's provenance travels with it and nothing else carries one")
}

// TestRunExportDrawsACurvedBoundaryToTheChordToleranceItWasGiven is its own
// function because what it asserts is a relation between two runs: a boundary
// which bends reaches the file as more segments the closer it is asked to
// follow the curve, and never as the straight line between its ends.
func TestRunExportDrawsACurvedBoundaryToTheChordToleranceItWasGiven(t *testing.T) {
	coarse := exportDrawn(t, "--height", "clear-height")

	fine, _, _ := exporting(t, exitSuccess,
		withTolerance(t, "facet", "0.01"),
		append(drawingFlags(), "--height", "clear-height")...)

	assert.Greater(t, points(t, artefact(t, fine)), points(t, coarse),
		"a tighter chord tolerance draws the curve with more segments")
}

// TestRunExportReadsAnArcAsStraightWhereTheRunNamesNoArcVocabulary is its own
// function because it is about the vocabulary rather than about the drawing:
// an edge is straight unless the model says otherwise in words the run named,
// which is what every other command here does with an arc.
func TestRunExportReadsAnArcAsStraightWhereTheRunNamesNoArcVocabulary(t *testing.T) {
	bent := exportDrawn(t, "--height", "clear-height")

	straight, _, _ := exporting(t, exitSuccess, shapeModel(),
		"--position", "position", "--tolerance", "corner", "--chord", "facet")

	assert.Less(t, points(t, artefact(t, straight)), points(t, bent),
		"an arc nobody named the vocabulary of is the chord between its ends")
}

// points is how many cartesian points an artefact holds, which is the size of
// the drawing in it.
func points(t *testing.T, source string) int {
	t.Helper()

	return strings.Count(source, "=IFCCARTESIANPOINT(")
}

// withTolerance is the fixture with one tolerance redeclared at another value.
func withTolerance(t *testing.T, name, value string) map[string]string {
	t.Helper()

	files := shapeModel()

	replaced := strings.Replace(files["registry.dfc"],
		"(tolerance "+name+"\n  (value 0.1 m)",
		"(tolerance "+name+"\n  (value "+value+" m)", 1)

	require.NotEqual(t, files["registry.dfc"], replaced, "the fixture declares the tolerance %s", name)
	files["registry.dfc"] = replaced

	return files
}

func TestRunExportRefusesAShapeItCannotDraw(t *testing.T) {
	testCases := []struct {
		name     string
		files    func(t *testing.T) map[string]string
		expected string
	}{
		{
			name: "a height of nought, which is no solid at all",
			files: func(t *testing.T) map[string]string {
				return replacing(t, edit{"entities/site.dfc", "(value 2.7 m)", "(value 0.0 m)"})
			},
			expected: "positive distance",
		},
		{
			name: "a height below nought",
			files: func(t *testing.T) map[string]string {
				return replacing(t, edit{"entities/site.dfc", "(value 2.7 m)", "(value -2.7 m)"})
			},
			expected: "positive distance",
		},
		{
			name: "a height whose predicate is declared in another unit than the frame",
			files: func(t *testing.T) map[string]string {
				return replacing(t,
					edit{"registry.dfc", "(predicate clear-height\n  (unit m)", "(predicate clear-height\n  (unit mm)"},
					edit{"entities/site.dfc", "(value 2.7 m)", "(value 2700.0 mm)"},
					edit{"entities/site.dfc", "(value 3.0 m)", "(value 3000.0 mm)"},
					edit{"entities/site.dfc", "(value 2.4 m)", "(value 2400.0 mm)"},
				)
			},
			expected: "which is the unit of the frame",
		},
		{
			name: "a boundary which lies in one plane but not a level one",
			files: func(t *testing.T) map[string]string {
				return replacing(t,
					edit{"geometry/level-01.dfc", "(value (4.0 3.0 0.0) m)", "(value (4.0 3.0 1.5) m)"},
					edit{"geometry/level-01.dfc", "(value (0.0 3.0 0.0) m)", "(value (0.0 3.0 1.5) m)"},
				)
			},
			expected: "one level",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, _, stderr := exporting(t, exitCheck, testCase.files(t),
				append(drawingFlags(), "--height", "clear-height")...)

			assert.False(t, result.Derived)
			assert.Empty(t, result.Files, "an artefact is all or nothing, and nothing was produced")
			assert.Contains(t, stderr, testCase.expected)
		})
	}
}

// edit is one substring of one fixture file, and what it becomes.
type edit struct {
	file string
	from string
	to   string
}

// replacing is the fixture with the edits given applied, which is how a case
// above states the single thing it changes.
func replacing(t *testing.T, edits ...edit) map[string]string {
	t.Helper()

	files := shapeModel()

	for _, one := range edits {
		replaced := strings.Replace(files[one.file], one.from, one.to, 1)
		require.NotEqual(t, files[one.file], replaced, "%s holds %q", one.file, one.from)
		files[one.file] = replaced
	}

	return files
}

// TestRunExportRefusesTwoEquallyCurrentHeights is its own function because
// what it exercises is the resolution rule rather than a value: two claims
// neither of which supersedes the other is a room with two heights, and a
// solid swept through one of them would be a shape the file gives no reason
// for.
func TestRunExportRefusesTwoEquallyCurrentHeights(t *testing.T) {
	files := replacing(t, edit{"entities/site.dfc",
		`  (clear-height
    (id survey:H-201)
    (value 2.7 m)`,
		`  (clear-height
    (value 2.9 m)
    (source "Fit-out drawing FD-2026-011")
    (method method:drawing-take-off)
    (accuracy (independent 0.006 m))
    (date "2026-05-06"))
  (clear-height
    (id survey:H-201)
    (value 2.7 m)`})

	result, _, stderr := exporting(t, exitCheck, files,
		append(drawingFlags(), "--height", "clear-height")...)

	assert.False(t, result.Derived)
	assert.Contains(t, stderr, "cannot separate")
}

func TestRunExportRefusesAnIncompleteDrawingVocabulary(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "a position with no tolerance and no chord tolerance",
			args:     []string{"--position", "position"},
			expected: "--tolerance",
		},
		{
			name:     "a chord tolerance with nothing to draw",
			args:     []string{"--chord", "facet"},
			expected: "--position",
		},
		{
			name:     "a height with no boundary to sweep",
			args:     []string{"--height", "clear-height"},
			expected: "swept upwards",
		},
		{
			name:     "a thickness with no run to widen",
			args:     []string{"--thickness", "nominal-thickness"},
			expected: "--thickness",
		},
		{
			name:     "an arc vocabulary with no boundary to bend",
			args:     []string{"--arc-centre", "arc-centre", "--arc-through", "arc-through"},
			expected: "--position",
		},
		{
			name: "an arc centre with no point on the curve",
			args: []string{
				"--position", "position", "--tolerance", "corner", "--chord", "facet",
				"--arc-centre", "arc-centre",
			},
			expected: "--arc-through",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, shapeModel())

			stdout, stderr := invoke(t, exitUsage, root, append([]string{"export"}, testCase.args...)...)

			assert.Empty(t, stdout)
			assert.Contains(t, stderr, testCase.expected)
		})
	}
}

// TestRunExportNamesTheClaimBehindARefusedHeight is its own function because
// what it asserts is which claim a diagnostic sends somebody to: a room may
// carry several heights, and a refusal which named only the room would leave
// the edit to be guessed at.
func TestRunExportNamesTheClaimBehindARefusedHeight(t *testing.T) {
	files := replacing(t, edit{"entities/site.dfc", "(value 2.7 m)", "(value 0.0 m)"})

	_, _, stderr := exporting(t, exitCheck, files,
		append(drawingFlags(), "--height", "clear-height")...)

	assert.Contains(t, stderr, "survey:H-201", "the refusal names the claim it is about")
	assert.Contains(t, stderr, "site:S-201", "and the room the claim was written on")
	assert.Contains(t, stderr, "clear-height", "and the predicate it was claimed under")
}

// elementRegistry is the drawn fixture's vocabulary with the elements this
// project builds added to it.
//
// The four types are the four answers a drawn product has. One is an area with
// a height over it, which is a room's shape on something which is not a room;
// two are runs, which is what a partition and a railing are; and one is
// classified as an entity no product can be written as, which is the mistake
// the export has to say something about rather than write around.
const elementRegistry = shapeRegistry + `
(predicate nominal-thickness
  (unit m)
  (shape scalar)
  (description "How thick a thing built as a run is."))

(type Countertop
  (kind Element)
  (geometry area)
  (description "A worktop fitted along a wall.")
  (classification "IFC4" "IfcFurnishingElement"))

(type Partition
  (kind Element)
  (geometry line)
  (description "A non-loadbearing wall between spaces.")
  (classification "IFC4" "IfcWall"))

(type Railing
  (kind Element)
  (geometry line)
  (description "A guard along the open side of a stair.")
  (classification "IFC4" "IfcRailing"))

(type Threshold
  (kind Interface)
  (geometry absent)
  (description "Where two rooms meet.")
  (classification "IFC4" "IfcRelSpaceBoundary"))
`

// elementGeometry is the outline of the countertop and the runs of the
// partition and the railing.
//
// The partition is two segments meeting at a corner and the railing is one, so
// the fixture holds both the case a joint has to be answered for and the case
// there is no joint at all.
const elementGeometry = `
(vertex geom:V-K-A (frame frame:building) (position (value (0.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-K-B (frame frame:building) (position (value (2.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-K-C (frame frame:building) (position (value (2.0 0.6 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-K-D (frame frame:building) (position (value (0.0 0.6 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-K-AB (frame frame:building) (vertices geom:V-K-A geom:V-K-B))
(edge geom:E-K-BC (frame frame:building) (vertices geom:V-K-B geom:V-K-C))
(edge geom:E-K-CD (frame frame:building) (vertices geom:V-K-C geom:V-K-D))
(edge geom:E-K-DA (frame frame:building) (vertices geom:V-K-D geom:V-K-A))

(loop geom:L-K (frame frame:building) (edges geom:E-K-AB geom:E-K-BC geom:E-K-CD geom:E-K-DA))

(vertex geom:V-W-A (frame frame:building) (position (value (0.0 3.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-W-B (frame frame:building) (position (value (4.0 3.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-W-C (frame frame:building) (position (value (4.0 6.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-W-AB (frame frame:building) (vertices geom:V-W-A geom:V-W-B))
(edge geom:E-W-BC (frame frame:building) (vertices geom:V-W-B geom:V-W-C))

(loop geom:L-W (frame frame:building) (edges geom:E-W-AB geom:E-W-BC))

(vertex geom:V-R-A (frame frame:building) (position (value (6.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-R-B (frame frame:building) (position (value (6.0 4.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-R-AB (frame frame:building) (vertices geom:V-R-A geom:V-R-B))

(loop geom:L-R (frame frame:building) (edges geom:E-R-AB))
`

// elementEntities is a storey holding three elements and no space at all.
//
// That is the whole point of it. The consumer this story came from authors
// stairs, railings and counters, and what it got back was a file in which every
// one of them existed, was classified correctly, was placed correctly and had
// no shape — so a model with nothing but elements in it has to come out with
// geometry in it or the export has not moved.
const elementEntities = `(node site:P-01
  (label "Plot one")
  (kind Site)
  (type Parcel))

(node site:B-01
  (label "Block A")
  (kind Building)
  (type OfficeBuilding)
  (within site:P-01))

(node site:L-01
  (label "Level one")
  (kind Storey)
  (type Level)
  (within site:B-01))

(node site:K-01
  (label "Kitchen counter")
  (kind Element)
  (type Countertop)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-K)
  (clear-height
    (id survey:H-K-01)
    (value 0.9 m)
    (source "As-built check AB-2026-019, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.006 m))
    (date "2026-05-06")))

(node site:W-01
  (label "Kitchen partition")
  (kind Element)
  (type Partition)
  (geometry line)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-W)
  (clear-height
    (value 2.7 m)
    (source "As-built check AB-2026-019, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.006 m))
    (date "2026-05-06"))
  (nominal-thickness
    (value 0.1 m)
    (source "Fit-out drawing FD-2026-011")
    (method method:drawing-take-off)
    (date "2026-03-02")))

(node site:R-01
  (label "Stair railing")
  (kind Element)
  (type Railing)
  (geometry line)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-R)
  (clear-height
    (value 1.1 m)
    (source "Fit-out drawing FD-2026-011")
    (method method:drawing-take-off)
    (date "2026-03-02"))
  (nominal-thickness
    (value 0.05 m)
    (source "Fit-out drawing FD-2026-011")
    (method method:drawing-take-off)
    (date "2026-03-02")))
`

// elementModel is the fixture tree the drawn element export is run against.
func elementModel() map[string]string {
	return map[string]string{
		"registry.dfc":          elementRegistry,
		"geometry/level-01.dfc": elementGeometry,
		"entities/site.dfc":     elementEntities,
	}
}

// bodyFlags is the drawing vocabulary with both of the predicates a body is
// built from, which is what a run drawing elements gives.
func bodyFlags() []string {
	return append(drawingFlags(), "--height", "clear-height", "--thickness", "nominal-thickness")
}

// exportElements runs export over the element fixture and returns the artefact
// it wrote.
func exportElements(t *testing.T, args ...string) string {
	t.Helper()

	result, _, _ := exporting(t, exitSuccess, elementModel(), append(bodyFlags(), args...)...)

	return artefact(t, result)
}

func TestRunExportDrawsTheElementsOfAModelWithNoSpaceInIt(t *testing.T) {
	got := exportElements(t)

	assert.Equal(t, elementGolden(t, got), got,
		"the drawn element artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
}

// elementGolden is the recorded element artefact, rewritten from got under
// -update.
func elementGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/elements.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

func TestRunExportGivesAnElementTheShapeItsBoundaryStates(t *testing.T) {
	source := exportElements(t)

	testCases := []struct {
		name     string
		expected string
	}{
		{
			name:     "writes a countertop as the furnishing element it is classified as",
			expected: "IFCFURNISHINGELEMENT(",
		},
		{
			name:     "sweeps the countertop's outline exactly as it sweeps a room's",
			expected: "IFCARBITRARYCLOSEDPROFILEDEF(.AREA.,",
		},
		{
			name:     "writes the partition as the wall it is classified as",
			expected: "IFCWALL(",
		},
		{
			name:     "writes the railing as the railing it is classified as",
			expected: "IFCRAILING(",
		},
		{
			name:     "records where the thickness a run was widened by came from",
			expected: "'dfcad_ThicknessProvenance'",
		},
		{
			name:     "names the predicate the thickness was claimed under",
			expected: "IFCPROPERTYSINGLEVALUE('Predicate',$,IFCTEXT('nominal-thickness'),$)",
		},
		{
			name:     "writes the thickness beside the body it widened",
			expected: "IFCPROPERTYSINGLEVALUE('Thickness',$,IFCTEXT('0.1'),$)",
		},
		{
			name:     "records where the height a body was swept through came from",
			expected: "'dfcad_HeightProvenance'",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Contains(t, source, testCase.expected)
		})
	}
}

// TestRunExportGivesEveryElementOfASpacelessModelABody is its own function
// because it is the story's acceptance in one assertion: the file a consumer
// which authors nothing but elements gets back has solids in it, and no
// IfcSpace for those solids to have come from.
func TestRunExportGivesEveryElementOfASpacelessModelABody(t *testing.T) {
	source := exportElements(t)

	assert.NotContains(t, source, "IFCSPACE(", "the model holds no room at all")

	// The countertop's one solid, the partition's two — one per straight
	// segment of its run — and the railing's one.
	assert.Equal(t, 4, strings.Count(source, "IFCEXTRUDEDAREASOLID("),
		"every element the model measured is swept into a solid")
	assert.Equal(t, 3, strings.Count(source, "'Body','SweptSolid'"),
		"and each carries that solid as a body of its own")
}

// TestRunExportWidensARunByTheThicknessClaimedOfIt is its own function because
// what it asserts is a number rather than a shape: the run is widened half the
// claimed thickness either side of the centreline, which is what makes the
// claim the width of the wall rather than half of it.
func TestRunExportWidensARunByTheThicknessClaimedOfIt(t *testing.T) {
	source := exportElements(t)

	// The railing runs north from (6,0) to (6,4) and is claimed to be 0.05
	// thick, so its plan is the strip from x=5.975 to x=6.025.
	assert.Contains(t, source, "IFCCARTESIANPOINT((5.975,0.))")
	assert.Contains(t, source, "IFCCARTESIANPOINT((6.025,4.))")
}

// TestRunExportWritesNoBodyForARunNothingClaimsAThicknessOf is its own function
// because what it asserts is a difference between two runs of one command: a
// centreline with no width claimed is not a solid, and the file says so by
// holding no shape for it rather than by holding a hairline nobody can select.
func TestRunExportWritesNoBodyForARunNothingClaimsAThicknessOf(t *testing.T) {
	result, _, _ := exporting(t, exitSuccess, elementModel(),
		append(drawingFlags(), "--height", "clear-height")...)

	source := artefact(t, result)

	assert.Contains(t, source, "IFCWALL(", "the partition is still in the file")
	assert.Equal(t, 1, strings.Count(source, "IFCEXTRUDEDAREASOLID("),
		"the countertop is drawn from its own outline and the two runs are not drawn at all")
}

// TestRunExportRefusesABodyClaimedOfSomethingNoEntityCanCarryOne is its own
// function because it is about a disagreement rather than about a value: the
// claim says a body was meant and the classification says it was meant on a
// relationship, and writing either of them as if the other were absent is the
// silence this refusal replaces.
func TestRunExportRefusesABodyClaimedOfSomethingNoEntityCanCarryOne(t *testing.T) {
	files := elementModel()
	files["entities/site.dfc"] += `
(node site:T-01
  (label "Rooms A and B, shared wall")
  (kind Interface)
  (type Threshold)
  (within site:L-01)
  (clear-height
    (id survey:H-T-01)
    (value 2.7 m)
    (source "Fit-out drawing FD-2026-011")
    (method method:drawing-take-off)
    (date "2026-03-02")))
`

	result, _, stderr := exporting(t, exitCheck, files, bodyFlags()...)

	assert.False(t, result.Derived)
	assert.Empty(t, result.Files, "an artefact is all or nothing, and nothing was produced")
	assert.Contains(t, stderr, "survey:H-T-01", "the refusal names the claim it is about")
	assert.Contains(t, stderr, "IfcRelSpaceBoundary", "and the entity which cannot carry the body")
}

// TestRunExportWritesAnUnmeasuredInterfaceAsAProxyStill is its own function
// because it is the other half of the refusal above: the fallback to a proxy is
// what a classification this writer cannot use has always meant, and it stays
// that for everything nobody claimed a body of.
func TestRunExportWritesAnUnmeasuredInterfaceAsAProxyStill(t *testing.T) {
	files := elementModel()
	files["entities/site.dfc"] += `
(node site:T-01
  (label "Rooms A and B, shared wall")
  (kind Interface)
  (type Threshold)
  (within site:L-01))
`

	result, _, _ := exporting(t, exitSuccess, files, bodyFlags()...)

	assert.Contains(t, artefact(t, result), "IFCBUILDINGELEMENTPROXY('")
}

// TestRunExportOfElementsIsAFunctionOfTheModel is the determinism property over
// a drawn element export, which ADR 0021 makes the whole artefact turn on: the
// same tree exports to the same bytes, and a second run reports the file it
// found as unchanged.
func TestRunExportOfElementsIsAFunctionOfTheModel(t *testing.T) {
	first := exportElements(t)

	for range 4 {
		assert.Equal(t, first, exportElements(t))
	}
}

// lineModel is the element fixture with the node bounded by rings taken out of
// it, so that what is left is drawn as lines and nothing else.
//
// It is what tells a refusal made on the run path from one the region path made
// first: an area node in the tree answers for the tolerance on its own, and a
// model of nothing but runs has nobody to answer for it but the run.
func lineModel(t *testing.T) map[string]string {
	t.Helper()

	files := elementModel()

	entities := files["entities/site.dfc"]
	from := strings.Index(entities, "(node site:K-01")
	to := strings.Index(entities, "(node site:W-01")
	require.Positive(t, from, "the fixture holds the countertop")
	require.Greater(t, to, from, "and the partition after it")

	files["entities/site.dfc"] = entities[:from] + entities[to:]

	return files
}

// TestRunExportRefusesARunItHasNoDeclaredToleranceToJudge is its own function
// because it is about a name rather than about a shape: there is no default
// tolerance and there never will be, so a run named against one the registry
// does not declare is refused rather than judged against nothing.
//
// It is refused as the invocation it is, before anything is derived — a usage
// error naming the tolerances the registry does declare — rather than as a
// diagnostic at the run which happened to read the name first: the mistake is
// on the command line, and nothing is wrong with site:W-01.
func TestRunExportRefusesARunItHasNoDeclaredToleranceToJudge(t *testing.T) {
	result, _, stderr := exporting(t, exitUsage, lineModel(t),
		"--position", "position", "--tolerance", "nonesuch", "--chord", "facet",
		"--height", "clear-height", "--thickness", "nominal-thickness")

	expected := dfcad.UnknownAxisError{Axis: "tolerance", Value: "nonesuch", Permitted: []string{"corner", "facet"}}

	assert.Equal(t, exportResult{}, result, "a usage error writes nothing on stdout")
	assert.Equal(t, "dfcad export: "+expected.Error()+"\n", stderr, "the refusal names the tolerance nobody declared, and nothing about the model")
}

// TestRunExportNamesTheQuantityBehindAnAmbiguousClaim is its own function
// because what it asserts is the words a diagnostic uses: a reader sent to fix
// two equally current claims is told which quantity they are of, and the two
// quantities are not one word with an "s" put on the end of it.
func TestRunExportNamesTheQuantityBehindAnAmbiguousClaim(t *testing.T) {
	testCases := []struct {
		name     string
		twice    edit
		expected string
	}{
		{
			name: "two equally current heights",
			twice: edit{"entities/site.dfc", `  (clear-height
    (value 1.1 m)`, `  (clear-height
    (value 1.3 m)
    (source "Fit-out drawing FD-2026-011")
    (method method:drawing-take-off)
    (date "2026-03-02"))
  (clear-height
    (value 1.1 m)`},
			expected: "equally current heights",
		},
		{
			name: "two equally current thicknesses",
			twice: edit{"entities/site.dfc", `  (nominal-thickness
    (value 0.05 m)`, `  (nominal-thickness
    (value 0.08 m)
    (source "Fit-out drawing FD-2026-011")
    (method method:drawing-take-off)
    (date "2026-03-02"))
  (nominal-thickness
    (value 0.05 m)`},
			expected: "equally current thicknesses",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			files := elementModel()

			replaced := strings.Replace(files[testCase.twice.file], testCase.twice.from, testCase.twice.to, 1)
			require.NotEqual(t, files[testCase.twice.file], replaced,
				"%s holds %q", testCase.twice.file, testCase.twice.from)
			files[testCase.twice.file] = replaced

			_, _, stderr := exporting(t, exitCheck, files, bodyFlags()...)

			assert.Contains(t, stderr, "cannot separate")
			assert.Contains(t, stderr, testCase.expected)
		})
	}
}

// TestRunExportGivesAnOpenRunARepresentationOnTheSameTermsAnAreaGetsOne is its
// own function because what it asserts is a symmetry between two shapes rather
// than a property of either.
//
// The partition's boundary is an open chain — two edges which begin at one
// corner and end at another, never returning — and the countertop's is a ring.
// Both reach the file with a footprint and a body, and neither is asked for
// anything the other is not: an outline, and the claims which turn the outline
// into a solid.
func TestRunExportGivesAnOpenRunARepresentationOnTheSameTermsAnAreaGetsOne(t *testing.T) {
	source := exportElements(t)

	for _, subject := range []string{"Kitchen counter", "Kitchen partition", "Stair railing"} {
		assert.Contains(t, source, subject, "the model states a shape for %s", subject)
	}

	assert.Equal(t, 3, strings.Count(source, "'FootPrint','Curve2D'"),
		"the ring and the two runs each carry the outline the model states")
	assert.Equal(t, 3, strings.Count(source, "'Body','SweptSolid'"),
		"and each carries the body the claims about it make of that outline")
}

// withALocatedElement is the element fixture with a panel added to the storey:
// a node whose declared geometry is `point`, placed by a claim on itself.
func withALocatedElement() map[string]string {
	files := elementModel()

	files["registry.dfc"] += `
(type Panel
  (kind Element)
  (geometry point)
  (description "A distribution board, recorded at the point it was set out at."))
`

	files["entities/site.dfc"] += `
(node site:PNL-01
  (label "Distribution panel 1")
  (kind Element)
  (type Panel)
  (geometry point)
  (frame frame:building)
  (within site:L-01)
  (position
    (value (2.5 1.5 1.2) m)
    (source "Services set-out SS-2026-007")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-02")))
`

	return files
}

// TestRunExportPlacesALocatedElementWhereTheModelPutsIt is its own function
// because what it asserts is a placement rather than a representation, which is
// a thing nothing else in this file is about.
//
// A panel used to reach the file at its container's origin. Every device on a
// level stood in one corner of it, the file was valid, the objects were
// classified correctly, and nothing anywhere said that the coordinates the
// model held had not been written — which is a failure a receiving system
// renders rather than reports.
func TestRunExportPlacesALocatedElementWhereTheModelPutsIt(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, withALocatedElement(), bodyFlags()...)
	require.True(t, result.Derived, stderr)

	source := artefact(t, result)

	assert.Contains(t, source, "Distribution panel 1")
	assert.Contains(t, source, "IFCCARTESIANPOINT((2.5,1.5,1.2))",
		"the product stands where the model puts it rather than at its container's origin")

	t.Run("writes no representation for it", func(t *testing.T) {
		// A panel has a position and no extent. A rectangle invented for one
		// would be dimensions nobody measured, so the placement is the whole of
		// what reaches the file: the field after the placement reference is the
		// representation, and it is empty.
		product := regexp.MustCompile(
			`IFCBUILDINGELEMENTPROXY\('[^']+',\$,'site:PNL-01','Distribution panel 1','Panel',#(\d+),\$,`)

		matched := product.FindStringSubmatch(source)
		require.Len(t, matched, 2, "the panel is written as a product with no representation")

		// And the placement it names is the one standing at the coordinate,
		// relative to the storey rather than at the storey's own origin.
		placement := regexp.MustCompile(`#` + matched[1] + `=IFCLOCALPLACEMENT\(#\d+,#(\d+)\);`)
		axis := placement.FindStringSubmatch(source)
		require.Len(t, axis, 2)

		assert.Regexp(t, `#`+axis[1]+`=IFCAXIS2PLACEMENT3D\(#(\d+),`, source)
	})

	t.Run("leaves the elements it already drew exactly as they were", func(t *testing.T) {
		assert.Equal(t, 3, strings.Count(source, "'FootPrint','Curve2D'"))
		assert.Equal(t, 3, strings.Count(source, "'Body','SweptSolid'"))
	})
}

// TestRunExportRefusesALocatedElementNothingPlaces is its own function because
// it is about the refusal rather than the placement: a device the model
// declares and puts nowhere is written at its container's origin by anything
// which does not say so, and a panel in the corner of its storey looks exactly
// like a panel somebody set out.
func TestRunExportRefusesALocatedElementNothingPlaces(t *testing.T) {
	files := withALocatedElement()
	files["entities/site.dfc"] += `
(node site:PNL-02
  (label "Distribution panel 2, not yet set out")
  (kind Element)
  (type Panel)
  (geometry point)
  (frame frame:building)
  (within site:L-01))
`

	_, _, stderr := exporting(t, exitCheck, files, bodyFlags()...)

	assert.Contains(t, stderr, "site:PNL-02")
}

// TestRunExportDrawsABodyOnADoor is its own function because it is the other
// side of the refusal above, and it is the case which used to fall between the
// two: a door is a thing somebody measures the height of, and until this writer
// held IfcDoor's attribute list a model which measured one was refused for
// having claimed a body of something that could not carry it.
//
// The fixture is the partition reclassified, so what changes between this and
// the golden beside it is one line of the registry and nothing else.
func TestRunExportDrawsABodyOnADoor(t *testing.T) {
	files := elementModel()
	files["registry.dfc"] = strings.Replace(elementRegistry, `"IfcWall"`, `"IfcDoor"`, 1)

	result, _, stderr := exporting(t, exitSuccess, files, bodyFlags()...)
	source := artefact(t, result)

	assert.True(t, result.Derived)
	assert.Empty(t, result.Classifications, "a door is an entity this writer holds an attribute list for")
	assert.NotContains(t, stderr, "IfcDoor", "so nothing is said about it")

	assert.Contains(t, source, "IFCDOOR('")
	assert.NotContains(t, source, "IFCBUILDINGELEMENTPROXY", "nothing in the fixture falls back now")

	t.Run("carries the body the height claim asked for", func(t *testing.T) {
		for at, held := range parsed(t, source) {
			if held.keyword != "IFCDOOR" {
				continue
			}

			require.Len(t, held.attributes, 13, at)
			assert.NotEqual(t, "$", held.attributes[6], "Representation")

			return
		}

		require.Fail(t, "the file holds a door")
	})
}

// offsetRegistry is the vocabulary of the smallest model a body's offset shows
// on: a wall drawn as a run through two jambs, a window drawn as the run between
// them, a room whose floor steps down from the level it is drawn at, and a slab
// stepped down with it.
//
// It is the reproduction the story was filed with, in US survey feet, with the
// room and the slab added so that an area and a line are both moved, and so
// that an offset below the boundary's level is exercised beside one above it.
const offsetRegistry = `(project
  (label "Body offset repro")
  (description "A window set in a wall, with a claim that puts its bottom above the floor.")
  (globalid-namespace "https://example.org/models/sill-repro"))

(namespace claim (description "Claim ids issued on this project."))
(namespace frame (description "Coordinate frames."))
(namespace geom (description "Geometric nodes."))
(namespace method (description "How a value was obtained."))
(namespace site (description "Semantic nodes."))

(frame frame:plan (label "Plan grid") (unit usft))

(tolerance corner (value 0.01 usft) (description "How close two corners are one corner."))

(tolerance facet (value 0.05 usft) (description "How far a chord may fall from its curve."))

(predicate position (unit usft) (shape coordinate) (dimension 3) (description "Where a corner is."))

(predicate height (unit usft) (shape scalar) (description "How tall a thing is, from its base."))

(predicate thickness (unit usft) (shape scalar) (description "How thick a run is."))

(predicate sill (unit usft) (shape scalar) (description "How far a thing's base is above its floor."))

(type House (kind Building) (geometry absent) (description "A dwelling."))

(type Level (kind Storey) (geometry absent) (description "One floor."))

(type Room (kind Space) (geometry area) (description "A room.")
  (classification "IFC4" "IfcSpace"))

(type Slab (kind Element) (geometry area) (description "A floor slab.")
  (classification "IFC4" "IfcSlab"))

(type Wall (kind Element) (geometry line) (description "A wall drawn as its centreline.")
  (classification "IFC4" "IfcWall"))

(type Window (kind Element) (geometry line) (description "A window drawn along its wall's run.")
  (classification "IFC4" "IfcWindow"))
`

// offsetGeometry is the wall's run through the two jambs the window shares with
// it, and the outline of a room behind the wall.
//
// Every corner is at nought. That is the point of the fixture: the window's
// jambs are corners of the wall's run and are written once, and nothing in the
// coordinates says the window stands anywhere but on the floor.
const offsetGeometry = `
(vertex geom:A (frame frame:plan) (position (value (0.0 0.0 0.0) usft) (source "Plan") (method method:take-off) (accuracy (independent 0.01 usft)) (date "2026-09-25")))
(vertex geom:J1 (frame frame:plan) (position (value (8.0 0.0 0.0) usft) (source "Plan") (method method:take-off) (accuracy (independent 0.01 usft)) (date "2026-09-25")))
(vertex geom:J2 (frame frame:plan) (position (value (11.0 0.0 0.0) usft) (source "Plan") (method method:take-off) (accuracy (independent 0.01 usft)) (date "2026-09-25")))
(vertex geom:B (frame frame:plan) (position (value (20.0 0.0 0.0) usft) (source "Plan") (method method:take-off) (accuracy (independent 0.01 usft)) (date "2026-09-25")))

(edge geom:A-J1 (frame frame:plan) (vertices geom:A geom:J1))
(edge geom:J1-J2 (frame frame:plan) (vertices geom:J1 geom:J2))
(edge geom:J2-B (frame frame:plan) (vertices geom:J2 geom:B))

(loop geom:WALL (frame frame:plan) (edges geom:A-J1 geom:J1-J2 geom:J2-B))
(loop geom:WINDOW (frame frame:plan) (edges geom:J1-J2))

(vertex geom:R1 (frame frame:plan) (position (value (0.0 1.0 0.0) usft) (source "Plan") (method method:take-off) (accuracy (independent 0.01 usft)) (date "2026-09-25")))
(vertex geom:R2 (frame frame:plan) (position (value (20.0 1.0 0.0) usft) (source "Plan") (method method:take-off) (accuracy (independent 0.01 usft)) (date "2026-09-25")))
(vertex geom:R3 (frame frame:plan) (position (value (20.0 10.0 0.0) usft) (source "Plan") (method method:take-off) (accuracy (independent 0.01 usft)) (date "2026-09-25")))
(vertex geom:R4 (frame frame:plan) (position (value (0.0 10.0 0.0) usft) (source "Plan") (method method:take-off) (accuracy (independent 0.01 usft)) (date "2026-09-25")))

(edge geom:R1-R2 (frame frame:plan) (vertices geom:R1 geom:R2))
(edge geom:R2-R3 (frame frame:plan) (vertices geom:R2 geom:R3))
(edge geom:R3-R4 (frame frame:plan) (vertices geom:R3 geom:R4))
(edge geom:R4-R1 (frame frame:plan) (vertices geom:R4 geom:R1))

(loop geom:ROOM (frame frame:plan) (edges geom:R1-R2 geom:R2-R3 geom:R3-R4 geom:R4-R1))
`

// offsetEntities is the storey the wall, the window, the room and the slab
// stand in.
//
// The wall claims no offset and starts where its run lies. The window claims a
// sill above it, the room a floor below it, and the slab a base further below
// again — so one run of the export moves bodies up, moves them down and leaves
// one where it was.
const offsetEntities = `(node site:B (label "House") (kind Building) (type House))

(node site:L (label "Main floor") (kind Storey) (type Level) (within site:B))

(node site:W (label "North wall") (kind Element) (type Wall) (geometry line) (within site:L)
  (boundary geom:WALL)
  (height (value 9.0 usft) (source "Section") (method method:take-off) (date "2026-09-25"))
  (thickness (value 0.5 usft) (source "Section") (method method:take-off) (date "2026-09-25")))

(node site:N (label "North window") (kind Element) (type Window) (geometry line) (within site:L)
  (boundary geom:WINDOW)
  (height (value 3.0 usft) (source "Window schedule") (method method:take-off) (date "2026-09-25"))
  (thickness (value 0.5 usft) (source "Section") (method method:take-off) (date "2026-09-25"))
  (sill (value 4.0 usft) (source "Elevation") (method method:take-off) (date "2026-09-25")))

(node site:R (label "Sunken den") (kind Space) (type Room) (geometry area) (within site:L)
  (boundary geom:ROOM)
  (height (value 9.0 usft) (source "Section") (method method:take-off) (date "2026-09-25"))
  (sill
    (id claim:SILL-R)
    (value -0.667 usft)
    (source "Section S-3")
    (method method:level)
    (accuracy (independent 0.01 usft))
    (date "2026-09-25")))

(node site:S (label "Den slab") (kind Element) (type Slab) (geometry area) (within site:L)
  (boundary geom:ROOM)
  (height (value 0.5 usft) (source "Section") (method method:take-off) (date "2026-09-25"))
  (sill (value -1.167 usft) (source "Section S-3") (method method:take-off) (date "2026-09-25")))
`

// offsetModel is the fixture tree the offset export is run against.
func offsetModel() map[string]string {
	return map[string]string{
		"registry.dfc":      offsetRegistry,
		"geometry/plan.dfc": offsetGeometry,
		"entities/site.dfc": offsetEntities,
	}
}

// offsetFlags is the vocabulary the fixture above is read under, with the
// predicate a body's offset is claimed under named as the story's consumer
// names it.
func offsetFlags() []string {
	return []string{
		"--position", "position", "--tolerance", "corner", "--chord", "facet",
		"--height", "height", "--thickness", "thickness", "--offset", "sill",
	}
}

// exportOffset runs export over a fixture under the vocabulary given and
// returns the artefact it wrote.
func exportOffset(t *testing.T, files map[string]string, args ...string) string {
	t.Helper()

	result, _, stderr := exporting(t, exitSuccess, files, args...)
	require.True(t, result.Derived, stderr)

	return artefact(t, result)
}

func TestRunExportOffsetsABodyByTheClaimMadeOfIt(t *testing.T) {
	got := exportOffset(t, offsetModel(), offsetFlags()...)

	assert.Equal(t, offsetGolden(t, got), got,
		"the offset artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
}

// offsetGolden is the recorded offset artefact, rewritten from got under
// -update.
func offsetGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/offsets.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// withStoreyElevation is the offset fixture with its storey written at an
// elevation: the plan grid every corner is drawn on is measured the given
// height above a root frame, and the storey declares it.
func withStoreyElevation(t *testing.T, lift string) map[string]string {
	t.Helper()

	files := offsetModel()

	registry := strings.Replace(files["registry.dfc"],
		`(frame frame:plan (label "Plan grid") (unit usft))`,
		`(predicate frame-transform (shape transform) (description "The rigid transform from a frame to its parent."))

(frame frame:site (label "Site datum") (unit usft))

(frame frame:plan
  (label "Plan grid")
  (unit usft)
  (parent frame:site)
  (transform claim:T-PLAN)
  (frame-transform
    (id claim:T-PLAN)
    (value
      (transform
        (translation 0.0 0.0 `+lift+`)
        (rotation 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0)
        (scale 1.0)))
    (source "Setting-out record")
    (method method:level)
    (accuracy (independent 0.01 usft))
    (date "2026-09-25")))`, 1)
	require.NotEqual(t, files["registry.dfc"], registry, "the fixture declares the plan grid")
	files["registry.dfc"] = registry

	entities := strings.Replace(files["entities/site.dfc"],
		`(type Level) (within site:B)`, `(type Level) (frame frame:plan) (within site:B)`, 1)
	require.NotEqual(t, files["entities/site.dfc"], entities, "the fixture declares the storey")
	files["entities/site.dfc"] = entities

	return files
}

func TestRunExportStartsABodyAtItsBoundarysLevelPlusItsOffset(t *testing.T) {
	testCases := []struct {
		name     string
		files    func(t *testing.T) map[string]string
		subject  string
		expected span
	}{
		{
			name:     "starts a window at the sill claimed of it",
			files:    func(*testing.T) map[string]string { return offsetModel() },
			subject:  "site:N",
			expected: span{low: 4, high: 7},
		},
		{
			name:     "leaves the wall the window is set in where its run lies",
			files:    func(*testing.T) map[string]string { return offsetModel() },
			subject:  "site:W",
			expected: span{low: 0, high: 9},
		},
		{
			name:     "starts a room below its outline where the offset claimed of it is negative",
			files:    func(*testing.T) map[string]string { return offsetModel() },
			subject:  "site:R",
			expected: span{low: -0.667, high: 8.333},
		},
		{
			name:     "moves an element drawn as an area exactly as it moves one drawn as a line",
			files:    func(*testing.T) map[string]string { return offsetModel() },
			subject:  "site:S",
			expected: span{low: -1.167, high: -0.667},
		},
		{
			name:     "composes the offset with the elevation the storey's frame chain puts it at",
			files:    func(t *testing.T) map[string]string { return withStoreyElevation(t, "2.5") },
			subject:  "site:N",
			expected: span{low: 6.5, high: 9.5},
		},
		{
			name:     "composes a negative offset with the storey's elevation too",
			files:    func(t *testing.T) map[string]string { return withStoreyElevation(t, "2.5") },
			subject:  "site:R",
			expected: span{low: 1.833, high: 10.833},
		},
		{
			name: "starts a body where its boundary lies when the offset claimed is nought",
			files: func(t *testing.T) map[string]string {
				files := offsetModel()
				files["entities/site.dfc"] = strings.Replace(files["entities/site.dfc"],
					"(sill (value 4.0 usft)", "(sill (value 0.0 usft)", 1)
				return files
			},
			subject:  "site:N",
			expected: span{low: 0, high: 3},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			source := exportOffset(t, testCase.files(t), offsetFlags()...)

			got := bodyOf(t, source, testCase.subject)

			assert.InDelta(t, testCase.expected.low, got.low, 1e-9, "the base of the body")
			assert.InDelta(t, testCase.expected.high, got.high, 1e-9, "the top of the body")
		})
	}
}

// TestRunExportSweepsEveryBodyFromItsBoundaryWhereTheRunNamesNoOffset is its
// own function because it compares two runs rather than reading one: a run
// naming no offset predicate reads no offset claim, so a model full of them
// exports exactly as it did before one could be read.
func TestRunExportSweepsEveryBodyFromItsBoundaryWhereTheRunNamesNoOffset(t *testing.T) {
	flags := offsetFlags()
	unmoved := exportOffset(t, offsetModel(), flags[:len(flags)-2]...)

	for _, subject := range []string{"site:N", "site:W", "site:R", "site:S"} {
		assert.Zero(t, bodyOf(t, unmoved, subject).low, "%s starts where its boundary lies", subject)
	}

	assert.NotContains(t, unmoved, "dfcad_OffsetProvenance", "and no offset is recorded, because none was read")
}

// TestRunExportLeavesTheFootprintWhereTheModelStatesIt is its own function
// because what it compares is two drawings of one model: the offset moves the
// sweep and nothing else, so every two dimensional point the plan is written
// with is the same whether or not the run read it.
func TestRunExportLeavesTheFootprintWhereTheModelStatesIt(t *testing.T) {
	flags := offsetFlags()

	moved := exportOffset(t, offsetModel(), flags...)
	unmoved := exportOffset(t, offsetModel(), flags[:len(flags)-2]...)

	plan := regexp.MustCompile(`IFCCARTESIANPOINT\(\([^,()]+,[^,()]+\)\)`)

	assert.Equal(t, plan.FindAllString(unmoved, -1), plan.FindAllString(moved, -1),
		"the plan the model states is written identically")
	assert.Equal(t, strings.Count(unmoved, "'FootPrint','Curve2D'"), strings.Count(moved, "'FootPrint','Curve2D'"))
}

func TestRunExportRecordsTheClaimABodyWasOffsetBy(t *testing.T) {
	source := exportOffset(t, offsetModel(), offsetFlags()...)

	testCases := []struct {
		name     string
		expected string
	}{
		{
			name:     "records the offset in a set of its own",
			expected: "'dfcad_OffsetProvenance'",
		},
		{
			name:     "names the predicate the offset was claimed under",
			expected: "IFCPROPERTYSINGLEVALUE('Predicate',$,IFCTEXT('sill'),$)",
		},
		{
			name:     "writes a sill above the floor as the figure it was claimed as",
			expected: "IFCPROPERTYSINGLEVALUE('Offset',$,IFCTEXT('4'),$)",
		},
		{
			name:     "writes a step down with its sign",
			expected: "IFCPROPERTYSINGLEVALUE('Offset',$,IFCTEXT('-0.667'),$)",
		},
		{
			name:     "writes the unit the offset is in",
			expected: "IFCPROPERTYSINGLEVALUE('Unit',$,IFCTEXT('usft'),$)",
		},
		{
			name:     "carries the source the offset was read off",
			expected: "IFCPROPERTYSINGLEVALUE('Source',$,IFCTEXT('Elevation'),$)",
		},
		{
			name:     "carries the method it was read by",
			expected: "IFCPROPERTYSINGLEVALUE('Method',$,IFCTEXT('method:level'),$)",
		},
		{
			name:     "carries the accuracy it is known to",
			expected: "IFCPROPERTYSINGLEVALUE('Accuracy',$,IFCTEXT('independent 0.01 usft'),$)",
		},
		{
			name:     "carries the date it was read",
			expected: "IFCPROPERTYSINGLEVALUE('Date',$,IFCTEXT('2026-09-25'),$)",
		},
		{
			name:     "names the claim it was resolved from",
			expected: "IFCPROPERTYSINGLEVALUE('Claim',$,IFCTEXT('claim:SILL-R'),$)",
		},
		{
			name:     "says which step of the resolution rule chose it",
			expected: "IFCPROPERTYSINGLEVALUE('Reason',$,IFCTEXT('unranked'),$)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Contains(t, source, testCase.expected)
		})
	}

	t.Run("writes one offset set per body a claim moved", func(t *testing.T) {
		assert.Equal(t, 3, strings.Count(source, "'dfcad_OffsetProvenance'"),
			"the window, the room and the slab claim an offset, and the wall claims none")
	})
}

func TestRunExportRefusesAnOffsetWhichIsNotADistance(t *testing.T) {
	testCases := []struct {
		name     string
		edits    []edit
		expected string
	}{
		{
			name: "an offset of another shape than one number",
			edits: []edit{
				{"registry.dfc", "(predicate sill (unit usft) (shape scalar)",
					"(predicate sill (unit usft) (shape coordinate) (dimension 3)"},
				{"entities/site.dfc", "(value -0.667 usft)", "(value (0.0 0.0 -0.667) usft)"},
				{"entities/site.dfc", "(value 4.0 usft)", "(value (0.0 0.0 4.0) usft)"},
				{"entities/site.dfc", "(value -1.167 usft)", "(value (0.0 0.0 -1.167) usft)"},
			},
			expected: "found a value of another shape",
		},
		{
			name: "an offset in another unit than the frame its boundary is drawn in",
			edits: []edit{
				{"registry.dfc", "(predicate sill (unit usft)", "(predicate sill (unit ft)"},
				{"entities/site.dfc", "(value -0.667 usft)", "(value -0.667 ft)"},
				{"entities/site.dfc", "(value 4.0 usft)", "(value 4.0 ft)"},
				{"entities/site.dfc", "(value -1.167 usft)", "(value -1.167 ft)"},
			},
			expected: "which is the unit of the frame",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			files := offsetModel()
			for _, one := range testCase.edits {
				replaced := strings.Replace(files[one.file], one.from, one.to, 1)
				require.NotEqual(t, files[one.file], replaced, "%s holds %q", one.file, one.from)
				files[one.file] = replaced
			}

			result, _, stderr := exporting(t, exitCheck, files, offsetFlags()...)

			assert.False(t, result.Derived)
			assert.Empty(t, result.Files, "an artefact is all or nothing, and nothing was produced")
			assert.Contains(t, stderr, testCase.expected)
			assert.Contains(t, stderr, "claim:SILL-R", "the refusal names the claim it is about")
			assert.Contains(t, stderr, "the sill of site:R", "and the predicate and the node it was claimed of")
		})
	}
}

// TestRunExportRefusesTwoEquallyCurrentOffsets is its own function because
// what it exercises is the resolution rule rather than a value: a window with
// two sills is not a window this command may stand at either of them.
func TestRunExportRefusesTwoEquallyCurrentOffsets(t *testing.T) {
	files := offsetModel()
	files["entities/site.dfc"] = strings.Replace(files["entities/site.dfc"],
		`  (sill (value 4.0 usft)`,
		`  (sill (value 3.5 usft) (source "Window schedule") (method method:take-off) (date "2026-09-25"))
  (sill (value 4.0 usft)`, 1)

	result, _, stderr := exporting(t, exitCheck, files, offsetFlags()...)

	assert.False(t, result.Derived)
	assert.Contains(t, stderr, "equally current offsets")
	assert.Contains(t, stderr, "site:N")
}

// TestRunExportRefusesAnOffsetWithNothingToMove is its own function because it
// is a usage error rather than a model's: an offset moves a body, and a run
// which sweeps none has nothing for it to move.
func TestRunExportRefusesAnOffsetWithNothingToMove(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name: "an offset with no height to sweep a body through",
			args: []string{
				"--position", "position", "--tolerance", "corner", "--chord", "facet", "--offset", "sill",
			},
			expected: "--height",
		},
		{
			name:     "an offset with no boundary to sweep",
			args:     []string{"--offset", "sill"},
			expected: "--offset",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, offsetModel())

			stdout, stderr := invoke(t, exitUsage, root, append([]string{"export"}, testCase.args...)...)

			assert.Empty(t, stdout)
			assert.Contains(t, stderr, testCase.expected)
		})
	}
}

func TestOffsetVocabularyOf(t *testing.T) {
	testCases := []struct {
		name     string
		drawn    shapes
		expected string
	}{
		{
			name:  "accepts a run naming no offset",
			drawn: shapes{height: "height"},
		},
		{
			name:  "accepts an offset beside the height it moves",
			drawn: shapes{height: "height", offset: "sill"},
		},
		{
			name:     "refuses an offset with no height, naming it",
			drawn:    shapes{offset: "sill"},
			expected: "sill",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := offsetVocabularyOf(testCase.drawn)

			if testCase.expected == "" {
				require.NoError(t, err)
				return
			}

			var got UnsweptOffsetError
			require.ErrorAs(t, err, &got)
			assert.Equal(t, testCase.expected, got.Offset)
		})
	}
}

// TestRunExportOfOffsetsIsAFunctionOfTheModel is the determinism property over
// an offset export
// ([0021](docs/decisions/0021-an-export-is-a-build-output-keyed-by-its-source-digest.md)).
func TestRunExportOfOffsetsIsAFunctionOfTheModel(t *testing.T) {
	first := exportOffset(t, offsetModel(), offsetFlags()...)

	for range 4 {
		assert.Equal(t, first, exportOffset(t, offsetModel(), offsetFlags()...))
	}
}

// bodyOf is the z range one product's body occupies above the file's datum,
// read back through the whole chain of placements it hangs off.
func bodyOf(t *testing.T, source, id string) span {
	t.Helper()

	for _, held := range parsed(t, source) {
		if strings.HasPrefix(held.keyword, "IFCREL") || len(held.attributes) < 7 ||
			held.attributes[2] != "'"+id+"'" {
			continue
		}

		low := elevationOf(t, source, held.attributes[5])

		var out span
		first := true

		for _, solid := range solids(t, source, held.attributes[6]) {
			bottom := low + elevationOf(t, source, solid.attributes[1])
			top := bottom + real(t, solid.attributes[3])

			if first {
				out, first = span{low: bottom, high: top}, false
				continue
			}
			out.low = min(out.low, bottom)
			out.high = max(out.high, top)
		}

		require.False(t, first, "%s carries a body", id)

		return out
	}

	t.Fatalf("the file holds a product named %s", id)

	return span{}
}
