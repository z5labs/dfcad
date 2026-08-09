// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// mapRegistry is the vocabulary the fixture below is judged against.
//
// The two frames are the point of it. The site grid is the root, it is the one
// carrying the coordinate reference system, and its coordinates are the size a
// projected system's are; the building grid hangs off it by a measured
// transform and is authored at its own origin, the way a setting-out drawing
// is. Everything the map export does to a coordinate is that transform, and a
// fixture with one frame would never have shown it.
const mapRegistry = `(project
  (label "Riverside example")
  (description "The model the map export fixture is drawn from.")
  (globalid-namespace "https://example.org/models/riverside"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
(namespace survey (description "Claim ids issued by Acme Surveys."))

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

(frame frame:site-grid
  (label "Site survey grid")
  (unit m)
  (crs "EPSG:6543"))

(frame frame:building
  (label "Building local grid")
  (unit m)
  (parent frame:site-grid)
  (transform survey:C-0001)
  (frame-transform
    (id survey:C-0001)
    (value
      (transform
        (translation 3502104.0 552004.0 0.0)
        (rotation 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0)
        (scale 1.0)))
    (source "Setting-out record SO-2026-014, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-02")))

(tolerance corner
  (value 0.005 m)
  (description "How close two corners have to be to be one corner."))

(tolerance facet
  (value 0.1 m)
  (description "How far a segment standing in for a curve may fall from it."))

(type Parcel
  (kind Site)
  (geometry area)
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
  (description "An enclosed room used for meetings."))
`

// mapSiteGeometry is the plot, outlined on the site grid in that grid's own
// coordinates, with a courtyard taken out of it.
//
// The courtyard is what says holes survive: a plot with a hole in it drawn as
// a solid block is a plan which shows land as built on that nobody built on.
const mapSiteGeometry = `(vertex geom:V-01 (frame frame:site-grid) (position (value (3502100.0 552000.0 0.0) m) (source "Boundary survey BS-2026-004") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-03-11")))
(vertex geom:V-02 (frame frame:site-grid) (position (value (3502140.0 552000.0 0.0) m) (source "Boundary survey BS-2026-004") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-03-11")))
(vertex geom:V-03 (frame frame:site-grid) (position (value (3502140.0 552024.0 0.0) m) (source "Boundary survey BS-2026-004") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-03-11")))
(vertex geom:V-04 (frame frame:site-grid) (position (value (3502100.0 552024.0 0.0) m) (source "Boundary survey BS-2026-004") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-03-11")))

(edge geom:E-01 (frame frame:site-grid) (vertices geom:V-01 geom:V-02))
(edge geom:E-02 (frame frame:site-grid) (vertices geom:V-02 geom:V-03))
(edge geom:E-03 (frame frame:site-grid) (vertices geom:V-03 geom:V-04))
(edge geom:E-04 (frame frame:site-grid) (vertices geom:V-04 geom:V-01))

(loop geom:L-01 (frame frame:site-grid) (edges geom:E-01 geom:E-02 geom:E-03 geom:E-04))

(vertex geom:V-05 (frame frame:site-grid) (position (value (3502120.0 552014.0 0.0) m) (source "Boundary survey BS-2026-004") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-03-11")))
(vertex geom:V-06 (frame frame:site-grid) (position (value (3502128.0 552014.0 0.0) m) (source "Boundary survey BS-2026-004") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-03-11")))
(vertex geom:V-07 (frame frame:site-grid) (position (value (3502128.0 552020.0 0.0) m) (source "Boundary survey BS-2026-004") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-03-11")))
(vertex geom:V-08 (frame frame:site-grid) (position (value (3502120.0 552020.0 0.0) m) (source "Boundary survey BS-2026-004") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-03-11")))

(edge geom:E-05 (frame frame:site-grid) (vertices geom:V-05 geom:V-06))
(edge geom:E-06 (frame frame:site-grid) (vertices geom:V-06 geom:V-07))
(edge geom:E-07 (frame frame:site-grid) (vertices geom:V-07 geom:V-08))
(edge geom:E-08 (frame frame:site-grid) (vertices geom:V-08 geom:V-05))

(loop geom:L-02 (frame frame:site-grid) (edges geom:E-05 geom:E-06 geom:E-07 geom:E-08))
`

// mapRoomGeometry is two rooms, outlined on the building grid at that grid's
// own origin.
//
// The second bows outwards along an arc, which is what says the chord
// tolerance reaches this command too: a curved wall written as the straight
// line between its ends is a plan which is wrong by however far the wall bows.
const mapRoomGeometry = `(vertex geom:V-11 (frame frame:building) (position (value (0.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-12 (frame frame:building) (position (value (4.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-13 (frame frame:building) (position (value (4.0 3.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-14 (frame frame:building) (position (value (0.0 3.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-11 (frame frame:building) (vertices geom:V-11 geom:V-12))
(edge geom:E-12 (frame frame:building) (vertices geom:V-12 geom:V-13))
(edge geom:E-13 (frame frame:building) (vertices geom:V-13 geom:V-14))
(edge geom:E-14 (frame frame:building) (vertices geom:V-14 geom:V-11))

(loop geom:L-11 (frame frame:building) (edges geom:E-11 geom:E-12 geom:E-13 geom:E-14))

(vertex geom:V-21 (frame frame:building) (position (value (6.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-22 (frame frame:building) (position (value (10.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-23 (frame frame:building) (position (value (10.0 4.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-24 (frame frame:building) (position (value (6.0 4.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-21 (frame frame:building) (vertices geom:V-21 geom:V-22))
(edge geom:E-22
  (frame frame:building)
  (vertices geom:V-22 geom:V-23)
  (arc-centre
    (value (10.0 2.0 0.0) m)
    (source "Setting-out record SO-2026-014")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18"))
  (arc-through
    (value (12.0 2.0 0.0) m)
    (source "Setting-out record SO-2026-014")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))
(edge geom:E-23 (frame frame:building) (vertices geom:V-23 geom:V-24))
(edge geom:E-24 (frame frame:building) (vertices geom:V-24 geom:V-21))

(loop geom:L-21 (frame frame:building) (edges geom:E-21 geom:E-22 geom:E-23 geom:E-24))
`

// mapEntities is the spatial structure the outlines above hang off.
//
// The building carries no outline at all, which is the ordinary case and is
// what says a map holds the things the model drew rather than one feature per
// node. The retired room is here because a thing which stopped existing must
// not reach a plan as a live one.
const mapEntities = `(node site:P-01
  (label "Plot one")
  (kind Site)
  (type Parcel)
  (geometry area)
  (frame frame:site-grid)
  (boundary geom:L-01)
  (boundary geom:L-02))

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

(node site:S-101
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-11))

(node site:S-102
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-21))

(node site:S-199
  (label "The old store")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-11)
  (retired
    (date "2026-04-02")
    (reason "Merged into Meeting Room A.")
    (superseded-by site:S-101)))
`

// mapModel is the fixture tree every test below is run against.
func mapModel() map[string]string {
	return map[string]string{
		"registry.dfc":          mapRegistry,
		"geometry/site.dfc":     mapSiteGeometry,
		"geometry/level-01.dfc": mapRoomGeometry,
		"entities/site.dfc":     mapEntities,
	}
}

// mapFlags is the vocabulary every run below reads the model under.
func mapFlags() []string {
	return []string{
		"--position", "position",
		"--tolerance", "corner",
		"--chord", "facet",
		"--arc-centre", "arc-centre",
		"--arc-through", "arc-through",
		"--crs", "crs",
	}
}

// mapping runs export-map against a fixture and returns what it wrote,
// requiring the run to have exited with the code given.
func mapping(t *testing.T, expectedCode int, files map[string]string, args ...string) (exportMapResult, string, string) {
	t.Helper()

	root := tree(t, files)
	stdout, stderr := invoke(t, expectedCode, root, append([]string{"export-map"}, args...)...)

	if strings.TrimSpace(stdout) == "" {
		return exportMapResult{}, root, stderr
	}

	return listed[exportMapResult](t, stdout), root, stderr
}

// mapArtefact is the bytes of the one file a result names.
func mapArtefact(t *testing.T, result exportMapResult) string {
	t.Helper()

	require.Len(t, result.Files, 1)

	src, err := os.ReadFile(result.Files[0].Path)
	require.NoError(t, err)

	return string(src)
}

// drawnMapOf runs the fixture with the standard vocabulary and returns the
// document.
func drawnMapOf(t *testing.T, args ...string) string {
	t.Helper()

	result, _, _ := mapping(t, exitSuccess, mapModel(), append(mapFlags(), args...)...)

	return mapArtefact(t, result)
}

func TestRunExportMap(t *testing.T) {
	result, root, _ := mapping(t, exitSuccess, mapModel(), mapFlags()...)

	assert.Equal(t, "export-map", result.Command)
	assert.True(t, result.Derived)
	assert.Equal(t, "GML 3.2.1", result.Schema)

	t.Run("names the digest of the source tree the artefact was derived from", func(t *testing.T) {
		digest, err := dfcad.DigestOf(root)
		require.NoError(t, err)

		assert.Equal(t, digest.String(), result.Digest)
	})

	t.Run("writes one file, beneath the build directory, keyed by that digest", func(t *testing.T) {
		require.Len(t, result.Files, 1)

		assert.Equal(t, statusWritten, result.Files[0].Status)
		assert.Equal(t,
			filepath.Join(root, dfcad.BuildDir, "export", result.Digest, "model.gml"),
			result.Files[0].Path)

		assert.FileExists(t, result.Files[0].Path)
	})

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		got := mapArtefact(t, result)

		assert.Equal(t, mapGolden(t, got), got,
			"the exported document is stale; regenerate it with: go test ./cmd/dfcad -update")
	})
}

// mapGolden is the recorded document, rewritten from got under -update.
func mapGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export-map/model.gml"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestRunExportMapIsByteIdenticalForAnUnchangedTree is its own function
// because it is about two runs rather than about one: the golden above says
// what the bytes are, and this says that nothing outside the model reaches
// them.
func TestRunExportMapIsByteIdenticalForAnUnchangedTree(t *testing.T) {
	first, _, _ := mapping(t, exitSuccess, mapModel(), mapFlags()...)
	second, _, _ := mapping(t, exitSuccess, mapModel(), mapFlags()...)

	assert.Equal(t, first.Digest, second.Digest)
	assert.Equal(t, mapArtefact(t, first), mapArtefact(t, second))
}

// TestRunExportMapLeavesADocumentItWouldHaveRewrittenAlone is its own function
// because it is about the second run over one tree, which is the cache hit the
// digest key buys.
func TestRunExportMapLeavesADocumentItWouldHaveRewrittenAlone(t *testing.T) {
	root := tree(t, mapModel())

	stdout, _ := invoke(t, exitSuccess, root, append([]string{"export-map"}, mapFlags()...)...)
	first := listed[exportMapResult](t, stdout)
	require.Len(t, first.Files, 1)
	require.Equal(t, statusWritten, first.Files[0].Status)

	stdout, _ = invoke(t, exitSuccess, root, append([]string{"export-map"}, mapFlags()...)...)
	second := listed[exportMapResult](t, stdout)

	require.Len(t, second.Files, 1)
	assert.Equal(t, statusUnchanged, second.Files[0].Status)
	assert.Equal(t, first.Files[0].Path, second.Files[0].Path)
}

// TestRunExportMapNeverWritesToTheModelRoot is its own function because what
// it asserts is about the authored tree rather than about the artefact.
func TestRunExportMapNeverWritesToTheModelRoot(t *testing.T) {
	root := tree(t, mapModel())

	before := authoredFiles(t, root)

	invoke(t, exitSuccess, root, append([]string{"export-map"}, mapFlags()...)...)

	assert.Equal(t, before, authoredFiles(t, root))
}

func TestRunExportMapWritesEveryRegionTheModelDrew(t *testing.T) {
	source := drawnMapOf(t)

	testCases := []struct {
		name     string
		expected string
	}{
		{
			name:     "writes the plot, which is outlined on the root frame itself",
			expected: "<dfcad:id>site:P-01</dfcad:id>",
		},
		{
			name:     "writes a room, which is outlined on a frame measured against it",
			expected: "<dfcad:id>site:S-101</dfcad:id>",
		},
		{
			name:     "carries the label a reader shows the feature under",
			expected: "<dfcad:label>Meeting Room A</dfcad:label>",
		},
		{
			name:     "carries the kind, so a plan can be styled by it",
			expected: "<dfcad:kind>Site</dfcad:kind>",
		},
		{
			name:     "carries the type the project declared",
			expected: "<dfcad:type>MeetingRoom</dfcad:type>",
		},
		{
			name:     "carries what contains it, so the features can be grouped",
			expected: "<dfcad:within>site:L-01</dfcad:within>",
		},
		{
			name:     "carries the frame the outline was declared in",
			expected: "<dfcad:frame>frame:building</dfcad:frame>",
		},
		{
			name:     "keeps the courtyard as a hole rather than as a second feature",
			expected: "<gml:interior>",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Contains(t, source, testCase.expected)
		})
	}

	t.Run("leaves out a node the model gave no outline to", func(t *testing.T) {
		assert.NotContains(t, source, "site:B-01</dfcad:id>")
	})

	t.Run("leaves out a node which stopped existing", func(t *testing.T) {
		assert.NotContains(t, source, "site:S-199")
	})
}

// TestRunExportMapExpressesEveryRegionInTheRootFrame is its own function
// because it is the assertion the whole command exists for: the coordinates in
// the document are the root frame's, and the only thing which moved them there
// is the chain of measured transforms the model already states.
func TestRunExportMapExpressesEveryRegionInTheRootFrame(t *testing.T) {
	source := drawnMapOf(t)

	t.Run("writes the plot in the coordinates it was authored in", func(t *testing.T) {
		assert.Contains(t, source, "3502100 552024 3502100 552000 3502140 552000 3502140 552024 3502100 552024")
	})

	t.Run("carries a room outlined on another frame across by that frame's transform", func(t *testing.T) {
		// The room is authored at the building grid's own origin, and the
		// transform to the site grid is a translation of 3502104 552004.
		assert.Contains(t, source, "3502104 552007 3502104 552004 3502108 552004 3502108 552007 3502104 552007")
	})

	t.Run("writes no coordinate in the frame it was authored in", func(t *testing.T) {
		assert.NotContains(t, source, "<gml:posList>0 0 4 0")
	})
}

// TestRunExportMapDrawsACurvedBoundaryToTheChordToleranceItWasGiven is its own
// function because what it asserts is a count rather than a value: a curve
// reaches the file as the segments it was drawn to, and a run which read no
// chord tolerance would write the two ends and nothing between them.
func TestRunExportMapDrawsACurvedBoundaryToTheChordToleranceItWasGiven(t *testing.T) {
	coarse := positionsOf(t, drawnMapOf(t))
	fine := positionsOf(t, drawnMapOfTolerance(t, "facet", "0.01"))

	assert.Greater(t, fine, coarse,
		"a tighter chord tolerance is more segments along the same curve")
}

// drawnMapOfTolerance is the fixture with one tolerance rewritten, exported.
func drawnMapOfTolerance(t *testing.T, name, value string) string {
	t.Helper()

	files := mapModel()
	files["registry.dfc"] = strings.Replace(files["registry.dfc"],
		"(tolerance "+name+"\n  (value 0.1 m)",
		"(tolerance "+name+"\n  (value "+value+" m)", 1)

	require.NotEqual(t, mapModel()["registry.dfc"], files["registry.dfc"], "the tolerance was rewritten")

	result, _, _ := mapping(t, exitSuccess, files, mapFlags()...)

	return mapArtefact(t, result)
}

// positionsOf is how many ordinates the document holds, which stands in for
// how finely it was drawn.
func positionsOf(t *testing.T, source string) int {
	t.Helper()

	var ordinates int

	for _, line := range strings.Split(source, "\n") {
		_, list, found := strings.Cut(line, "<gml:posList>")
		if !found {
			continue
		}
		list, _, _ = strings.Cut(list, "</gml:posList>")
		ordinates += len(strings.Fields(list))
	}

	require.NotZero(t, ordinates, "the document holds coordinates")

	return ordinates
}

// mapBowedWall is the one edge of the fixture which states a curve, quoted so
// that a model claiming none can be made by writing it as an ordinary edge.
const mapBowedWall = `(edge geom:E-22
  (frame frame:building)
  (vertices geom:V-22 geom:V-23)
  (arc-centre
    (value (10.0 2.0 0.0) m)
    (source "Setting-out record SO-2026-014")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18"))
  (arc-through
    (value (12.0 2.0 0.0) m)
    (source "Setting-out record SO-2026-014")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))`

// mapUncurved is the fixture with that wall written as the ordinary edge it
// would be if nobody had surveyed the bow, which is a model carrying no arc
// claim anywhere.
func mapUncurved(t *testing.T) map[string]string {
	t.Helper()

	straight := strings.Replace(mapRoomGeometry, mapBowedWall,
		`(edge geom:E-22 (frame frame:building) (vertices geom:V-22 geom:V-23))`, 1)

	require.NotEqual(t, mapRoomGeometry, straight, "the quoted wall still matches the fixture")

	files := mapModel()
	files["geometry/level-01.dfc"] = straight

	return files
}

// mapFlagsWithoutArcs is the same vocabulary with the two predicates an arc is
// written under left out, which is how a caller who does not know the model
// carries curves runs this command.
func mapFlagsWithoutArcs() []string {
	return []string{
		"--position", "position",
		"--tolerance", "corner",
		"--chord", "facet",
		"--crs", "crs",
	}
}

// TestRunExportMapSaysWhatItDrewTheDocumentTo is its own function because what
// it asserts is a property of the artefact rather than of any feature in it.
//
// A GML document is positions. A reader holding one cannot tell a ring which
// follows its curve to a tenth of a metre from one drawn coarsely, so a map
// which does not say what it was drawn to is one no downstream check can judge.
func TestRunExportMapSaysWhatItDrewTheDocumentTo(t *testing.T) {
	result, _, _ := mapping(t, exitSuccess, mapModel(), mapFlags()...)

	require.True(t, result.Derived)

	require.NotNil(t, result.Chord)
	assert.Equal(t, "facet", result.Chord.Name)
	assert.Equal(t, 0.1, result.Chord.Value)
	assert.Equal(t, "m", result.Chord.Unit)

	require.NotNil(t, result.Deviation)
	assert.Positive(t, result.Deviation.Value, "the fixture holds a curve, which was approximated")
	assert.LessOrEqual(t, result.Deviation.Value, result.Chord.Value)
	assert.Equal(t, "m", result.Deviation.Unit)

	assert.Empty(t, result.Chorded, "every curve in the fixture was read")
}

// TestRunExportMapReadsACurvedEdgeOrSaysItDidNot is its own function because
// the halves of it are one behaviour, and because this command is the one whose
// product is a file somebody keeps: a feature drawn straight through a curve
// nothing read is a boundary in the wrong place in that file, and a deviation
// of nothing beside a named chord tolerance is this command saying it is in the
// right one.
func TestRunExportMapReadsACurvedEdgeOrSaysItDidNot(t *testing.T) {
	t.Run("names the edge it chorded and reports no deviation from a curve it never read", func(t *testing.T) {
		result, _, stderr := mapping(t, exitSuccess, mapModel(), mapFlagsWithoutArcs()...)

		require.True(t, result.Derived, "a chorded map is still a map, and is still written")

		require.Len(t, result.Chorded, 1, "one edge of the fixture states a curve")
		assert.Equal(t, "geom:E-22", result.Chorded[0].Edge)
		assert.Equal(t, []string{"arc-centre", "arc-through"}, result.Chorded[0].Predicates,
			"the predicates to name, which is what makes the report actionable")
		assert.NotEmpty(t, result.Chorded[0].Span.String())

		require.NotNil(t, result.Chord, "what was asked for is still what was asked for")
		assert.Equal(t, "facet", result.Chord.Name)

		assert.Nil(t, result.Deviation,
			"the bowed wall was run straight through, so nothing here achieved anything against it")

		assert.Contains(t, stderr, "geom:E-22", "and a person is told the same thing")
	})

	t.Run("says nothing about curves for a model which claims none", func(t *testing.T) {
		result, _, stderr := mapping(t, exitSuccess, mapUncurved(t), mapFlags()...)

		assert.Empty(t, result.Chorded)

		// A model carrying no arc claim gets the answer it always got: four
		// straight walls were followed exactly, and that zero is true.
		require.NotNil(t, result.Chord)
		require.NotNil(t, result.Deviation)
		assert.Zero(t, result.Deviation.Value)

		assert.NotContains(t, stderr, "curved edge")
	})

	t.Run("names it on a run which refused the georeference and wrote nothing", func(t *testing.T) {
		// A curve nothing read is a fact about the model rather than about the
		// file, so a run which also cannot say where the model is has two
		// things wrong with it. Reporting one of them sends the author back to
		// fix the registry, run again, and only then find the wall.
		files := mapModel()
		files["registry.dfc"] = strings.Replace(files["registry.dfc"],
			`(parent frame:site-grid)`,
			`(parent frame:site-grid)
  (crs "EPSG:6543")`, 1)

		result, _, stderr := mapping(t, exitCheck, files, mapFlagsWithoutArcs()...)

		require.False(t, result.Derived)
		assert.Empty(t, result.Files)

		require.Len(t, result.Chorded, 1)
		assert.Equal(t, "geom:E-22", result.Chorded[0].Edge)

		assert.Contains(t, stderr, "geom:E-22")
		assert.Contains(t, stderr, "frame:building", "and the georeference is still refused")

		assert.Nil(t, result.Chord, "nothing was drawn, so there is no tolerance it was drawn to")
		assert.Nil(t, result.Deviation)
	})

	t.Run("reports an edge two features share once", func(t *testing.T) {
		result, _, _ := mapping(t, exitSuccess, mapModel(), mapFlagsWithoutArcs()...)

		seen := map[string]int{}
		for _, entry := range result.Chorded {
			seen[entry.Edge]++
		}

		for edge, count := range seen {
			assert.Equal(t, 1, count, "%s is one edge however many regions reach it", edge)
		}
	})
}

// TestRunExportMapNamesTheCoordinateReferenceSystemOnEveryGeometry is its own
// function because the identifier reaching the file is the whole story: a
// document whose coordinates name no system is one somebody places by
// guessing.
func TestRunExportMapNamesTheCoordinateReferenceSystemOnEveryGeometry(t *testing.T) {
	source := drawnMapOf(t)

	named := strings.Count(source, `srsName="EPSG:6543"`)
	geometries := strings.Count(source, "<gml:MultiSurface")

	assert.Equal(t, geometries+1, named,
		"every geometry names the system, and so does the envelope over them")
}

// TestRunExportMapWithoutACRSPredicateWritesTheFileAndSaysWhatIsMissing is its
// own function because it is a run which succeeds and warns, which is neither
// of the shapes the tests above assert.
func TestRunExportMapWithoutACRSPredicateWritesTheFileAndSaysWhatIsMissing(t *testing.T) {
	flags := []string{
		"--position", "position",
		"--tolerance", "corner",
		"--chord", "facet",
	}

	result, _, stderr := mapping(t, exitSuccess, mapModel(), flags...)

	assert.True(t, result.Derived)
	assert.NotContains(t, mapArtefact(t, result), "srsName")
	assert.Contains(t, stderr, "frame:site-grid")
	assert.Contains(t, stderr, "--crs")
}

// TestRunExportMapRefusesAGeoreferenceItCannotWrite is its own function
// because it asserts about a run which produced nothing: an artefact is all or
// nothing, so a model which cannot say where it is writes no file at all.
func TestRunExportMapRefusesAGeoreferenceItCannotWrite(t *testing.T) {
	files := mapModel()
	files["registry.dfc"] = strings.Replace(files["registry.dfc"],
		`(parent frame:site-grid)`,
		`(parent frame:site-grid)
  (crs "EPSG:6543")`, 1)

	result, root, stderr := mapping(t, exitCheck, files, mapFlags()...)

	assert.False(t, result.Derived)
	assert.Empty(t, result.Files)
	assert.Contains(t, stderr, "frame:building")

	assert.NoDirExists(t, filepath.Join(root, dfcad.BuildDir))
}

// TestRunExportMapRefusesABoundaryWhichIsNotLevel is its own function because
// the refusal is about the shape rather than about the georeference, and
// because it is the one place this command chooses not to project something.
func TestRunExportMapRefusesABoundaryWhichIsNotLevel(t *testing.T) {
	// The room is tilted rather than folded: two of its four corners are
	// lifted, so the ring is still planar and is still a ring, and the only
	// thing wrong with it is that its plane is not horizontal. A corner moved
	// on its own would be refused by the assembly before this command saw it,
	// which is a different refusal about a different mistake.
	files := mapModel()
	rooms := files["geometry/level-01.dfc"]
	rooms = strings.Replace(rooms, `(value (4.0 3.0 0.0) m)`, `(value (4.0 3.0 1.0) m)`, 1)
	rooms = strings.Replace(rooms, `(value (0.0 3.0 0.0) m)`, `(value (0.0 3.0 1.0) m)`, 1)
	files["geometry/level-01.dfc"] = rooms

	result, _, stderr := mapping(t, exitCheck, files, mapFlags()...)

	assert.False(t, result.Derived)
	assert.Empty(t, result.Files)
	assert.Contains(t, stderr, "site:S-101")
	assert.Contains(t, stderr, "a map is a plan")
}

// TestRunExportMapRequiresTheVocabularyAnOutlineIsReadUnder is its own
// function because it is a usage error rather than a diagnostic: it is a fact
// about the invocation, and it is decidable before a byte of the model is
// read.
func TestRunExportMapRequiresTheVocabularyAnOutlineIsReadUnder(t *testing.T) {
	testCases := []struct {
		name    string
		args    []string
		missing []string
	}{
		{
			name:    "names none of it",
			args:    nil,
			missing: []string{"--position", "--tolerance", "--chord"},
		},
		{
			name:    "names the predicate a corner is claimed under and nothing else",
			args:    []string{"--position", "position"},
			missing: []string{"--tolerance", "--chord"},
		},
		{
			name:    "names everything but the chord tolerance",
			args:    []string{"--position", "position", "--tolerance", "corner"},
			missing: []string{"--chord"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, root, stderr := mapping(t, exitUsage, mapModel(), testCase.args...)

			for _, flag := range testCase.missing {
				assert.Contains(t, stderr, flag)
			}

			assert.NoDirExists(t, filepath.Join(root, dfcad.BuildDir))
		})
	}
}

// TestRunExportMapRefusesADestinationInsideTheAuthoredTree is its own function
// because it is the other usage error, and because what it protects is the
// model rather than the artefact.
func TestRunExportMapRefusesADestinationInsideTheAuthoredTree(t *testing.T) {
	_, root, stderr := mapping(t, exitUsage, mapModel(),
		append(mapFlags(), "--out", "entities/model.gml")...)

	assert.Contains(t, stderr, "entities/model.gml")
	assert.FileExists(t, filepath.Join(root, "entities", "site.dfc"))
	assert.NoFileExists(t, filepath.Join(root, "entities", "model.gml"))
}

// TestRunExportMapWritesWhereItWasTold is its own function because --out is
// the one thing about this command a build script sets, and because a
// destination outside the tree is the ordinary use of it.
func TestRunExportMapWritesWhereItWasTold(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "plan.gml")

	result, _, _ := mapping(t, exitSuccess, mapModel(),
		append(mapFlags(), "--out", destination)...)

	require.Len(t, result.Files, 1)
	assert.Equal(t, destination, result.Files[0].Path)
	assert.FileExists(t, destination)
}
