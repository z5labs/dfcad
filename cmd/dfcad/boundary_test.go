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
)

// boundaryRegistry is the vocabulary the bounded fixture is authored against.
//
// It is the drawing vocabulary plus two element types: one which names an IFC
// entity of its own, and one which does not and so reaches the file as a
// proxy. Both back an edge, because what a boundary relationship needs of an
// element is that it be in the file and not that it be a wall.
const boundaryRegistry = `(project
  (label "Riverside example")
  (description "The model the bounded export fixture is derived from.")
  (globalid-namespace "https://example.org/models/riverside"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))

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

(type PartitionWall
  (kind Element)
  (geometry absent)
  (description "A wall dividing two rooms.")
  (classification "IFC4" "IfcWall"))

(type Hoarding
  (kind Element)
  (geometry absent)
  (description "A screen standing on the plot, which no IFC entity names."))
`

// boundaryGeometry is three rooms in a row, sharing the two edges between
// them.
//
// The sharing is the whole fixture. One edge is shared and backed, one is
// shared and backed by nothing, one is backed by something the file will not
// hold, and one is backed by nothing and shared with nothing — which are the
// four answers a space boundary export has.
const boundaryGeometry = `
(vertex geom:V-A (frame frame:building) (position (value (0.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-B (frame frame:building) (position (value (4.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-C (frame frame:building) (position (value (4.0 3.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-D (frame frame:building) (position (value (0.0 3.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-E (frame frame:building) (position (value (8.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-F (frame frame:building) (position (value (8.0 3.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-G (frame frame:building) (position (value (12.0 0.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-H (frame frame:building) (position (value (12.0 3.0 0.0) m) (source "Interior control set IC-1") (method method:total-station) (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-AB
  (frame frame:building)
  (vertices geom:V-A geom:V-B)
  (backed-by site:W-plot))
(edge geom:E-BC
  (frame frame:building)
  (vertices geom:V-B geom:V-C)
  (backed-by site:W-party))
(edge geom:E-CD (frame frame:building) (vertices geom:V-C geom:V-D))
(edge geom:E-DA
  (frame frame:building)
  (vertices geom:V-D geom:V-A)
  (backed-by site:W-loose))

(edge geom:E-BE (frame frame:building) (vertices geom:V-B geom:V-E))
(edge geom:E-EF (frame frame:building) (vertices geom:V-E geom:V-F))
(edge geom:E-FC (frame frame:building) (vertices geom:V-F geom:V-C))

(edge geom:E-EG (frame frame:building) (vertices geom:V-E geom:V-G))
(edge geom:E-GH (frame frame:building) (vertices geom:V-G geom:V-H))
(edge geom:E-HF (frame frame:building) (vertices geom:V-H geom:V-F))

(loop geom:L-301 (frame frame:building) (edges geom:E-AB geom:E-BC geom:E-CD geom:E-DA))
(loop geom:L-302 (frame frame:building) (edges geom:E-BE geom:E-EF geom:E-FC geom:E-BC))
(loop geom:L-303 (frame frame:building) (edges geom:E-EG geom:E-GH geom:E-HF geom:E-EF))
`

// boundaryEntities is the spatial structure those rooms hang off, and the
// three elements the edges name.
//
// The three elements are where internal and external are decided, and each is
// placed by its containment and by nothing else. The partition stands in the
// storey, so it is in the same building as the rooms it separates; the
// hoarding stands on the plot, so it is between a room and the outside; and
// the loose one stands nowhere, which is a thing IFC has no place for.
const boundaryEntities = `(node site:P-01
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

(node site:W-party
  (label "Rooms A and B party wall")
  (kind Element)
  (type PartitionWall)
  (within site:L-01))

(node site:W-plot
  (label "Plot hoarding")
  (kind Element)
  (type Hoarding)
  (within site:P-01))

(node site:W-loose
  (label "Screen nobody has placed")
  (kind Element)
  (type Hoarding))

(node site:S-301
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-301))

(node site:S-302
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-302))

(node site:S-303
  (label "Meeting Room C")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-303))
`

// boundaryModel is the fixture tree every test below is run against.
func boundaryModel() map[string]string {
	return map[string]string{
		"registry.dfc":          boundaryRegistry,
		"geometry/level-01.dfc": boundaryGeometry,
		"entities/site.dfc":     boundaryEntities,
	}
}

// boundaryFlags is the vocabulary a drawn run over that fixture reads it
// under.
func boundaryFlags() []string {
	return []string{"--position", "position", "--tolerance", "corner", "--chord", "facet"}
}

// exportBounded runs export over the fixture and returns the artefact and what
// it said on the way.
func exportBounded(t *testing.T, args ...string) (string, string) {
	t.Helper()

	result, _, stderr := exporting(t, exitSuccess, boundaryModel(), args...)

	return artefact(t, result), stderr
}

func TestRunExportWritesEveryBoundaryAnElementBacks(t *testing.T) {
	source, _ := exportBounded(t, boundaryFlags()...)

	testCases := []struct {
		name     string
		expected string
	}{
		{
			name:     "writes the relationship rather than leaving the sharing to be recovered",
			expected: "IFCRELSPACEBOUNDARY('",
		},
		{
			name:     "names the edge the boundary was read from",
			expected: "'geom:E-BC',$,",
		},
		{
			name:     "carries the computed classification through as the schema enumeration",
			expected: ".PHYSICAL.,.INTERNAL.);",
		},
		{
			name:     "says external where the containment puts the element outside the building",
			expected: ".PHYSICAL.,.EXTERNAL.);",
		},
		{
			name:     "cuts the connection curve out of the drawing rather than the edge's own ends",
			expected: "IFCCONNECTIONCURVEGEOMETRY(",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Contains(t, source, testCase.expected)
		})
	}

	t.Run("writes one relationship per space and element which back an edge of it", func(t *testing.T) {
		// The party wall is reached from both rooms it separates, which is two
		// relationships and not one: a boundary is stated of a space, and each
		// of the two is bounded by that wall.
		assert.Equal(t, 3, strings.Count(source, "=IFCRELSPACEBOUNDARY("))
	})

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		assert.Equal(t, boundaryGolden(t, source), source,
			"the bounded artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})
}

// boundaryGolden is the recorded bounded artefact, rewritten from got under
// -update.
func boundaryGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/boundaries.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestRunExportReportsABoundaryTheSchemaCannotExpress is its own function
// because what it asserts is a diagnostic beside a file which was still
// written, rather than a line of that file: the export succeeds, and the two
// boundaries it could not carry are named on the way.
func TestRunExportReportsABoundaryTheSchemaCannotExpress(t *testing.T) {
	source, stderr := exportBounded(t, boundaryFlags()...)

	t.Run("names the space and the edge where nothing backs a shared edge", func(t *testing.T) {
		assert.Contains(t, stderr, "geom:E-EF")
		assert.Contains(t, stderr, "site:S-302")
		assert.Contains(t, stderr, "site:S-303")
		assert.Contains(t, stderr, "which nothing backs")
	})

	t.Run("names the space and the edge where the element is not in the file", func(t *testing.T) {
		assert.Contains(t, stderr, "geom:E-DA")
		assert.Contains(t, stderr, "site:W-loose")
		assert.Contains(t, stderr, "nothing spatial contains")
	})

	t.Run("writes nothing for either of them", func(t *testing.T) {
		assert.NotContains(t, source, "'geom:E-EF'")
		assert.NotContains(t, source, "'geom:E-DA'")
	})

	t.Run("says nothing about an edge which bounds one room and nothing else", func(t *testing.T) {
		// Nothing was ever stated about what runs along it, so there is no
		// boundary between two things to leave out — and a warning per wall of
		// every plan-only model is noise nobody can act on.
		assert.NotContains(t, stderr, "geom:E-CD")
	})

	t.Run("still produces the artefact, because a stated gap is not a refusal", func(t *testing.T) {
		assert.Contains(t, source, "IFCRELSPACEBOUNDARY('")
	})
}

// TestRunExportWritesBoundariesWithoutDrawingAnything is its own function
// because it is the story's central case: a boundary is a relationship rather
// than a geometry, so a run which draws nothing still says which element
// bounds which room, and simply says it without a curve.
func TestRunExportWritesBoundariesWithoutDrawingAnything(t *testing.T) {
	source, _ := exportBounded(t)

	assert.Contains(t, source, "IFCRELSPACEBOUNDARY('")
	assert.Contains(t, source, ".PHYSICAL.,.INTERNAL.);")

	assert.NotContains(t, source, "IFCCONNECTIONCURVEGEOMETRY",
		"connection geometry is emitted where the attribution is there and omitted otherwise")
	assert.NotContains(t, source, "IFCPOLYLINE",
		"a run which drew nothing draws nothing here either")
}

// TestRunExportDrawsAConnectionCurveFromTheSegmentsTheEdgeProduced is its own
// function because what it asserts is a relation between the curve and the
// outline beside it: the connection geometry is the run of the footprint that
// edge produced, so it is made of points the footprint already holds rather
// than of an approximation drawn beside it.
func TestRunExportDrawsAConnectionCurveFromTheSegmentsTheEdgeProduced(t *testing.T) {
	source, _ := exportBounded(t, boundaryFlags()...)

	// The party wall runs from (4,0) to (4,3), which are two corners of the
	// outline of each of the rooms it separates.
	assert.Contains(t, source, "IFCCARTESIANPOINT((4.,0.))")
	assert.Contains(t, source, "IFCCARTESIANPOINT((4.,3.))")

	assert.Equal(t, 3, strings.Count(source, "=IFCCONNECTIONCURVEGEOMETRY("),
		"a curve is written for each boundary the drawing attributed a run to")

	// The two rooms traverse the shared edge in opposite directions, and each
	// one's curve runs the way its own outline does. A curve built from the
	// edge's own vertices instead would run one of the two rooms backwards.
	corners := partyCorners(t, source)
	require.Len(t, corners, 2)

	assert.Contains(t, source, "IFCPOLYLINE((#"+corners[0]+",#"+corners[1]+"));")
	assert.Contains(t, source, "IFCPOLYLINE((#"+corners[1]+",#"+corners[0]+"));",
		"the second room's curve runs against the first's")
}

// partyCorners is the two instance numbers of the corners the party wall runs
// between, ascending.
//
// They are read out of the file rather than written down here, because the
// numbering is the traversal: this test is about which way round the two
// curves run and not about which numbers that traversal happened to reach
// those corners at.
func partyCorners(t *testing.T, source string) []string {
	t.Helper()

	var out []string

	for _, line := range strings.Split(source, "\n") {
		number, rest, found := strings.Cut(strings.TrimPrefix(line, "#"), "=")
		if !found || !strings.HasPrefix(rest, "IFCCARTESIANPOINT((4.,") {
			continue
		}
		out = append(out, number)
	}

	return out
}

// TestRunExportLeavesAnUnresolvedBackingAsTheLoadErrorItAlreadyIs is its own
// function because it is about which pass answers: an edge naming an element
// the model does not hold is refused when the model is read, and the exporter
// is never reached to have an opinion about it.
func TestRunExportLeavesAnUnresolvedBackingAsTheLoadErrorItAlreadyIs(t *testing.T) {
	files := boundaryModel()
	files["geometry/level-01.dfc"] = strings.Replace(files["geometry/level-01.dfc"],
		"(backed-by site:W-party)", "(backed-by site:W-nothing)", 1)

	root := tree(t, files)
	stdout, stderr := invoke(t, exitLoad, root, append([]string{"export"}, boundaryFlags()...)...)

	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "site:W-nothing")

	// The edge said something backs it. Reporting it as a boundary with
	// nothing along it would be the silent reclassification the load error
	// exists to prevent.
	assert.NotContains(t, stderr, "which nothing backs")
}

// TestRunExportBoundsNothingWhereNoEdgeNamesAnElement is its own function
// because what it asserts is an absence over a whole file: a model which
// states no backing at all exports exactly as it did before there were
// boundaries to write.
func TestRunExportBoundsNothingWhereNoEdgeNamesAnElement(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, shapeModel(), drawingFlags()...)
	source := artefact(t, result)

	assert.NotContains(t, source, "IFCRELSPACEBOUNDARY")
	assert.NotContains(t, source, "IFCCONNECTIONCURVEGEOMETRY")
	assert.NotContains(t, stderr, "which nothing backs")
}
