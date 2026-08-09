// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// measured is one fixture loaded and ready to be measured: the vocabulary it
// declares, both families joined, and the survey every figure is computed
// against.
type measuredModel struct {
	registry   *Registry
	nodes      *Nodes
	topology   *Topology
	boundaries *Boundaries
	survey     Survey
}

// loadMeasuredModel loads one fixture, failing the test where any pass beneath
// the one under test reports anything.
//
// Every fixture here loads clean. What a golden beside one holds is therefore
// what the measurement had to say, and nothing else — a fixture whose geometry
// did not load would be testing the loader.
func loadMeasuredModel(t *testing.T, name string) measuredModel {
	t.Helper()

	return loadMeasuredRoot(t, filepath.Join("testdata", "measure", name))
}

// loadMeasuredRoot is [loadMeasuredModel] over a tree somewhere other than
// beside this file, which is what lets a round trip be loaded back out of what
// the printer wrote.
func loadMeasuredRoot(t *testing.T, root string) measuredModel {
	t.Helper()

	registry, registryDiags := LoadRegistry(root)
	require.Empty(t, registryDiags, "the fixture registry loads clean")

	nodes, nodeDiags := LoadNodes(root, registry)
	require.Empty(t, nodeDiags, "the fixture's semantic family loads clean")

	topology, topologyDiags := LoadTopology(root, registry)
	require.Empty(t, topologyDiags, "the fixture's geometric family loads clean")

	claims, claimDiags := LoadClaims(root, registry)
	require.Empty(t, claimDiags, "the fixture's claims load clean")

	boundaries, boundaryDiags := ResolveBoundaries(nodes, topology)
	require.Empty(t, renderBoundaryDiagnostics(t, boundaryDiags), "the fixture's two families join clean")

	// Which predicate carries a position is vocabulary this repository owns, so
	// it is named here and nowhere in the engine. Place is what keeps the
	// positions and the claims behind them in step.
	survey := Survey{Tolerance: closureTolerance, Registry: registry}
	for vertex := range topology.Vertices() {
		resolution, err := claims.Resolve(vertex.ID(), "position", registry)
		require.NoError(t, err)

		survey.Place(vertex.ID(), resolution)
	}

	// An arc is registry data in exactly the way a position is. This repository
	// spells the two claims behind one `arc-centre` and `arc-through`, resolves
	// them for the edges which carry them and hands the result over; a fixture
	// whose registry declares neither has no curved edges to read.
	if _, declared := registry.Predicate("arc-centre"); declared {
		for edge := range topology.Edges() {
			centre, err := claims.Resolve(edge.ID(), "arc-centre", registry)
			require.NoError(t, err)

			through, err := claims.Resolve(edge.ID(), "arc-through", registry)
			require.NoError(t, err)

			survey.Bend(edge.ID(), centre, through)
		}
	}

	return measuredModel{
		registry:   registry,
		nodes:      nodes,
		topology:   topology,
		boundaries: boundaries,
		survey:     survey,
	}
}

// measure measures one region of a fixture, failing the test where the fixture
// holds no node of that id.
func (m measuredModel) measure(t *testing.T, id ID) (Measurement, []Diagnostic) {
	t.Helper()

	region, ok := m.nodes.Node(id)
	require.True(t, ok, "the fixture holds a node %s", id)

	return m.topology.MeasureRegion(region, m.boundaries, m.survey)
}

// measureAll measures every loop and then every edge of a fixture, in the order
// the walk read them, and renders what the measurements had to say.
//
// Everything is measured rather than one named thing, for the reason
// [assembleAll] measures every loop: a fixture holding three shapes which are
// not shapes should report on three of them, and a helper which had to be told
// which to look at would quietly stop testing whichever nobody remembered.
func measureAll(t *testing.T, name string) string {
	t.Helper()

	model := loadMeasuredModel(t, name)

	var diags []Diagnostic

	for loop := range model.topology.Loops() {
		_, found := model.topology.MeasureLoop(loop, model.survey)
		diags = append(diags, found...)
	}

	for edge := range model.topology.Edges() {
		_, found := model.topology.MeasureEdge(edge, model.survey)
		diags = append(diags, found...)
	}

	return renderBoundaryDiagnostics(t, diags)
}

// measureRegions measures every semantic node of a fixture, in the order the
// walk read them, and renders what the measurements had to say.
//
// It is separate from [measureAll] rather than folded into it because a region
// is measured through its loops: a helper which did both would report every
// loop's diagnostic twice, once in its own right and once through the region
// which references it.
func measureRegions(t *testing.T, name string) string {
	t.Helper()

	model := loadMeasuredModel(t, name)

	var diags []Diagnostic
	for node := range model.nodes.All() {
		_, found := model.topology.MeasureRegion(node, model.boundaries, model.survey)
		diags = append(diags, found...)
	}

	return renderBoundaryDiagnostics(t, diags)
}

// expectedMeasureDiagnostics returns the rendering held beside the fixture,
// having first rewritten it from got when -update was passed.
func expectedMeasureDiagnostics(t *testing.T, name string, got string) string {
	t.Helper()

	path := filepath.Join("testdata", "measure", name, "diagnostics.txt")
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

func TestMeasureRegion(t *testing.T) {
	testCases := []struct {
		name     string
		region   ID
		area     float64
		length   float64
		centroid Point
		bounds   Box
	}{
		{
			name:     "measures a square from its four corners",
			region:   "site:S-01",
			area:     12,
			length:   14,
			centroid: Point{2, 1.5, 0},
			bounds:   Box{Min: Point{0, 0, 0}, Max: Point{4, 3, 0}, Unit: "m"},
		},
		{
			name:     "measures a triangle, whose centroid is where its three corners average",
			region:   "site:S-11",
			area:     18,
			length:   12 + math.Sqrt(72),
			centroid: Point{12, 2, 0},
			bounds:   Box{Min: Point{10, 0, 0}, Max: Point{16, 6, 0}, Unit: "m"},
		},
		{
			name:     "measures an L-shape, whose centroid is in neither of the rectangles it is made of",
			region:   "site:S-21",
			area:     16,
			length:   26,
			centroid: Point{22.5, 1.75, 0},
			bounds:   Box{Min: Point{20, 0, 0}, Max: Point{28, 5, 0}, Unit: "m"},
		},
		{
			name:     "measures a concave shape, whose centroid is not inside its bounding box centre",
			region:   "site:S-31",
			area:     10,
			length:   12 + 2*math.Sqrt(13),
			centroid: Point{32, 1.4, 0},
			bounds:   Box{Min: Point{30, 0, 0}, Max: Point{34, 4, 0}, Unit: "m"},
		},
	}

	model := loadMeasuredModel(t, "shapes")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			region, ok := model.nodes.Node(testCase.region)
			require.True(t, ok)

			measurement, diags := model.topology.MeasureRegion(region, model.boundaries, model.survey)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			area, ok := measurement.Area()
			require.True(t, ok)
			assert.InDelta(t, testCase.area, area, 1e-9)

			length, ok := measurement.Length()
			require.True(t, ok)
			assert.InDelta(t, testCase.length, length, 1e-9)

			centroid, ok := measurement.Centroid()
			require.True(t, ok)
			for axis := range centroid {
				assert.InDelta(t, testCase.centroid[axis], centroid[axis], 1e-9, "axis %d", axis)
			}

			bounds, ok := measurement.Bounds()
			require.True(t, ok)
			assert.Equal(t, testCase.bounds, bounds)

			// Every figure above is in the unit of the frame the region is
			// declared in, and an area is in the square of it. Nothing here
			// converts, so the unit and the figure together are the answer.
			assert.Equal(t, Unit("m"), measurement.Unit())
			assert.Equal(t, testCase.region, measurement.Subject())
		})
	}
}

// TestMeasureIsIndependentOfWhereTheRingWasWrittenFrom is its own function
// because what it asserts is an equality between two answers rather than either
// answer's value: the same four edges, written backwards and written from a
// different one of them, have to measure the same to the last bit.
//
// Not within a tolerance. A size which depended on where somebody began writing
// the loop down would be a size which changed when the file was tidied, and a
// difference small enough to be invisible in a test is exactly the one which
// makes two runs of a report disagree.
func TestMeasureIsIndependentOfWhereTheRingWasWrittenFrom(t *testing.T) {
	model := loadMeasuredModel(t, "shapes")

	written, _ := model.measure(t, "site:S-01")

	testCases := []struct {
		name   string
		region ID
	}{
		{name: "measures the same ring written the other way round the same", region: "site:S-02"},
		{name: "measures the same ring written from another of its edges the same", region: "site:S-03"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			other, diags := model.measure(t, testCase.region)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			assert.Equal(t, valuesOf(written), valuesOf(other))
		})
	}
}

// valuesOf is every figure of a measurement, without the subject it was computed
// for, which is what two measurements of one shape written two ways differ in
// and are allowed to.
func valuesOf(m Measurement) []any {
	area, hasArea := m.Area()
	length, hasLength := m.Length()
	centroid, hasCentroid := m.Centroid()
	bounds, hasBounds := m.Bounds()

	return []any{area, hasArea, length, hasLength, centroid, hasCentroid, bounds, hasBounds, m.Unit()}
}

// TestMeasureKeepsItsPrecisionFarFromTheOrigin is its own function because the
// assertion is exact equality against a hand-computed value rather than a
// tolerance: on a projected grid the interesting failure is a room which comes
// out as 11.999999999 square metres, and a test which allowed a delta would pass
// on it.
func TestMeasureKeepsItsPrecisionFarFromTheOrigin(t *testing.T) {
	model := loadMeasuredModel(t, "far-from-the-origin")

	measurement, diags := model.measure(t, "site:S-01")
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	area, ok := measurement.Area()
	require.True(t, ok)
	assert.Equal(t, 12.0, area, "four by three is twelve, seven digits from the origin as much as at it")

	length, ok := measurement.Length()
	require.True(t, ok)
	assert.Equal(t, 14.0, length)

	centroid, ok := measurement.Centroid()
	require.True(t, ok)
	assert.Equal(t, Point{1234569.25, 7654323.0, 0}, centroid)

	bounds, ok := measurement.Bounds()
	require.True(t, ok)
	assert.Equal(t, Box{
		Min:  Point{1234567.25, 7654321.5, 0},
		Max:  Point{1234571.25, 7654324.5, 0},
		Unit: "m",
	}, bounds)
}

// TestMeasureSubtractsARingInsideAnother is its own function because the
// behaviour it asserts belongs to a region with more than one ring, which the
// single-ring table above has no case for.
func TestMeasureSubtractsARingInsideAnother(t *testing.T) {
	model := loadMeasuredModel(t, "courtyard")

	measurement, diags := model.measure(t, "site:S-01")
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	t.Run("takes the courtyard away from the floor plate", func(t *testing.T) {
		area, ok := measurement.Area()
		require.True(t, ok)
		assert.InDelta(t, 84.0, area, 1e-9, "a hundred square metres less a sixteen-metre courtyard")
	})

	t.Run("counts the boundary of both rings", func(t *testing.T) {
		length, ok := measurement.Length()
		require.True(t, ok)
		assert.InDelta(t, 56.0, length, 1e-9, "forty metres of perimeter wall and sixteen of courtyard wall")
	})

	t.Run("weights the centroid by the area which is left", func(t *testing.T) {
		centroid, ok := measurement.Centroid()
		require.True(t, ok)
		assert.InDelta(t, 5.0, centroid[0], 1e-9)
		assert.InDelta(t, 5.0, centroid[1], 1e-9)
	})

	t.Run("bounds the whole of it, hole and all", func(t *testing.T) {
		bounds, ok := measurement.Bounds()
		require.True(t, ok)
		assert.Equal(t, Box{Min: Point{0, 0, 0}, Max: Point{10, 10, 0}, Unit: "m"}, bounds)
		assert.Equal(t, Point{10, 10, 0}, bounds.Size())
		assert.Equal(t, Point{5, 5, 0}, bounds.Centre())
	})
}

func TestMeasureRefusesAShapeWhichIsNotOne(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
	}{
		{
			name:    "names a ring whose corners lie on one line, one which never closes, one which passes through a corner twice, and an edge with no extent",
			fixture: "degenerate",
		},
		{
			name:    "names the point two walls of one room cross at",
			fixture: "self-intersecting",
		},
		{
			name:    "names the corner which is out of the plane of the others, and how far",
			fixture: "not-planar",
		},
		{
			name:    "names the corner nobody has surveyed rather than measuring around it",
			fixture: "unmeasurable",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := measureAll(t, testCase.fixture)

			assert.Equal(t, expectedMeasureDiagnostics(t, testCase.fixture, got), got)
		})
	}
}

// TestMeasureWithoutAnAreaSaysSoRatherThanSayingZero is its own function because
// the assertion is the absence of a figure rather than a diagnostic: what makes
// a refusal a refusal is that nothing plausible comes back with it.
func TestMeasureWithoutAnAreaSaysSoRatherThanSayingZero(t *testing.T) {
	testCases := []struct {
		name      string
		fixture   string
		region    ID
		hasLength bool
		hasBounds bool
	}{
		{
			name:      "gives no area for a ring whose corners are collinear, and still gives its length",
			fixture:   "degenerate",
			region:    "site:S-01",
			hasLength: true,
			hasBounds: true,
		},
		{
			name:      "gives no area for a ring which never closes, and still gives the length of its edges",
			fixture:   "degenerate",
			region:    "site:S-11",
			hasLength: true,
			hasBounds: true,
		},
		{
			name:      "gives nothing at all for a ring which passes through one corner twice",
			fixture:   "degenerate",
			region:    "site:S-31",
			hasLength: false,
			hasBounds: false,
		},
		{
			name:      "gives no area for a ring which crosses itself",
			fixture:   "self-intersecting",
			region:    "site:S-01",
			hasLength: true,
			hasBounds: true,
		},
		{
			name:      "gives no area for corners which are not in one plane",
			fixture:   "not-planar",
			region:    "site:S-01",
			hasLength: true,
			hasBounds: true,
		},
		{
			name:      "gives nothing at all for a ring with a corner nobody has surveyed",
			fixture:   "unmeasurable",
			region:    "site:S-01",
			hasLength: false,
			hasBounds: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			model := loadMeasuredModel(t, testCase.fixture)

			measurement, diags := model.measure(t, testCase.region)
			assert.NotEmpty(t, diags, "a refusal says why")

			area, ok := measurement.Area()
			assert.False(t, ok)
			assert.Zero(t, area)

			_, ok = measurement.Centroid()
			assert.False(t, ok)

			_, ok = measurement.Length()
			assert.Equal(t, testCase.hasLength, ok)

			_, ok = measurement.Bounds()
			assert.Equal(t, testCase.hasBounds, ok)
		})
	}
}

// TestMeasureRefusesRingsWhichShareAnOrientationAndNotAPlane is its own function
// because it is measured at the region rather than at the loop: two rings are
// only nested where they lie in one plane, and the case which makes that worth
// checking — two storeys, whose plates face the same way — is one no single loop
// can see.
func TestMeasureRefusesRingsWhichShareAnOrientationAndNotAPlane(t *testing.T) {
	got := measureRegions(t, "two-planes")
	assert.Equal(t, expectedMeasureDiagnostics(t, "two-planes", got), got)

	model := loadMeasuredModel(t, "two-planes")

	measurement, _ := model.measure(t, "site:S-01")

	// Seen from above the upper plate is inside the lower one. Nesting them
	// would report a hundred square metres less sixteen, and the sixteen are on
	// another storey.
	_, ok := measurement.Area()
	assert.False(t, ok)
	_, ok = measurement.Centroid()
	assert.False(t, ok)
}

func TestMeasureEdge(t *testing.T) {
	model := loadMeasuredModel(t, "shapes")

	t.Run("gives the length, the midpoint and the box of an edge", func(t *testing.T) {
		edge, ok := model.topology.Edge("geom:E-02")
		require.True(t, ok)

		measurement, diags := model.topology.MeasureEdge(edge, model.survey)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		length, ok := measurement.Length()
		require.True(t, ok)
		assert.Equal(t, 3.0, length)

		centroid, ok := measurement.Centroid()
		require.True(t, ok)
		assert.Equal(t, Point{4, 1.5, 0}, centroid)

		bounds, ok := measurement.Bounds()
		require.True(t, ok)
		assert.Equal(t, Box{Min: Point{4, 0, 0}, Max: Point{4, 3, 0}, Unit: "m"}, bounds)

		// An edge is a line. It encloses nothing, and there is no answer rather
		// than an area of zero.
		_, ok = measurement.Area()
		assert.False(t, ok)
	})

	t.Run("gives the length of a diagonal edge", func(t *testing.T) {
		edge, ok := model.topology.Edge("geom:E-12")
		require.True(t, ok)

		measurement, diags := model.topology.MeasureEdge(edge, model.survey)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		length, ok := measurement.Length()
		require.True(t, ok)
		assert.InDelta(t, math.Sqrt(72), length, 1e-9)
	})
}

func TestMeasureVertex(t *testing.T) {
	model := loadMeasuredModel(t, "shapes")

	t.Run("gives where a corner is and how far it reaches", func(t *testing.T) {
		vertex, ok := model.topology.Vertex("geom:V-03")
		require.True(t, ok)

		measurement, diags := model.topology.MeasureVertex(vertex, model.survey)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		assert.Equal(t, ID("geom:V-03"), measurement.Subject())
		assert.Equal(t, Unit("m"), measurement.Unit())

		centroid, ok := measurement.Centroid()
		require.True(t, ok)
		assert.Equal(t, Point{4, 3, 0}, centroid)

		bounds, ok := measurement.Bounds()
		require.True(t, ok)
		assert.Equal(t, Box{Min: Point{4, 3, 0}, Max: Point{4, 3, 0}, Unit: "m"}, bounds,
			"a point reaches exactly as far as itself")
	})

	t.Run("gives a corner no length and no area rather than zeroes", func(t *testing.T) {
		vertex, ok := model.topology.Vertex("geom:V-03")
		require.True(t, ok)

		measurement, _ := model.topology.MeasureVertex(vertex, model.survey)

		_, ok = measurement.Length()
		assert.False(t, ok)
		_, ok = measurement.Area()
		assert.False(t, ok)
	})

	t.Run("carries the accuracy of the claim which put it there", func(t *testing.T) {
		vertex, ok := model.topology.Vertex("geom:V-03")
		require.True(t, ok)

		measurement, _ := model.topology.MeasureVertex(vertex, model.survey)

		assert.True(t, measurement.Budget().Known())
		assert.NotEmpty(t, measurement.Budget().Terms())
	})

	t.Run("measures nothing at all", func(t *testing.T) {
		measurement, diags := model.topology.MeasureVertex(nil, model.survey)

		assert.Empty(t, diags)
		assert.Equal(t, ID(""), measurement.Subject())
	})
}

// TestMeasureVertexNobodyHasSurveyed is its own function because it asserts about
// a refusal rather than about a figure: a corner nobody has claimed a position for
// is unknown, and answering it with the origin would be the plausible-looking
// number every other refusal here exists to avoid.
func TestMeasureVertexNobodyHasSurveyed(t *testing.T) {
	model := loadMeasuredModel(t, "shapes")

	vertex, ok := model.topology.Vertex("geom:V-03")
	require.True(t, ok)

	unsurveyed := Survey{Tolerance: closureTolerance, Registry: model.registry}

	measurement, diags := model.topology.MeasureVertex(vertex, unsurveyed)

	require.Len(t, diags, 1)
	assert.Equal(t, SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, "geom:V-03")

	_, computed := measurement.Centroid()
	assert.False(t, computed)
	_, computed = measurement.Bounds()
	assert.False(t, computed)
}

// TestMeasureLoopLengthIsTheSameAsItsEdgesMeasuredOneByOne is its own function
// because it is a property rather than a value: a perimeter which disagreed with
// the walls it is made of would be two answers to one question.
func TestMeasureLoopLengthIsTheSameAsItsEdgesMeasuredOneByOne(t *testing.T) {
	model := loadMeasuredModel(t, "shapes")

	loop, ok := model.topology.Loop("geom:L-21")
	require.True(t, ok)

	measurement, diags := model.topology.MeasureLoop(loop, model.survey)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	perimeter, ok := measurement.Length()
	require.True(t, ok)

	var summed float64
	for _, id := range loop.Edges() {
		edge, ok := model.topology.Edge(id)
		require.True(t, ok)

		one, diags := model.topology.MeasureEdge(edge, model.survey)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		length, ok := one.Length()
		require.True(t, ok)
		summed += length
	}

	assert.InDelta(t, summed, perimeter, 1e-9)
}

// TestMeasurementCarriesTheAccuracyOfThePositionsItWasComputedFrom is its own
// function because the assertion is the provenance of the answer rather than the
// answer: an area with no error budget is a number nobody can act on, and the
// budget is what says which claims it rests on and which of their errors are
// shared.
func TestMeasurementCarriesTheAccuracyOfThePositionsItWasComputedFrom(t *testing.T) {
	model := loadMeasuredModel(t, "shapes")

	measurement, diags := model.measure(t, "site:S-01")
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	budget := measurement.Budget()

	t.Run("knows the accuracy of every corner it was computed from", func(t *testing.T) {
		assert.True(t, budget.Known())
		assert.Empty(t, budget.Unknown())
	})

	t.Run("counts the term the four corners share once", func(t *testing.T) {
		var shared []BudgetTerm
		for _, term := range budget.Terms() {
			if term.Kind == TermSystematic {
				shared = append(shared, term)
			}
		}

		require.Len(t, shared, 1, "one control point, reached through four corners")
		assert.Equal(t, ID("control:CP-3"), shared[0].Source)
		assert.True(t, shared[0].Shared())
		assert.Len(t, shared[0].Contributors, 4)
	})

	t.Run("combines the independent terms in quadrature and the shared one linearly", func(t *testing.T) {
		combined, err := budget.Combined()
		require.NoError(t, err)

		// Four corners at four millimetres each, plus one eight-millimetre tie
		// to the control which does not average away.
		assert.InDelta(t, math.Sqrt(4*0.004*0.004+0.008*0.008), combined.Magnitude, 1e-12)
		assert.Equal(t, Unit("m"), combined.Unit)
		assert.Equal(t, 1.0, combined.CoverageFactor)
	})
}

// TestMeasureRefusesCornersOfDifferentShapes is its own function because the
// positions are built here rather than loaded: a registry declaring a
// three-component position refuses a two-component one at load, so the only way
// to reach this is to hand a measurement the mismatch directly, which is exactly
// what a caller resolving under two predicates would do.
func TestMeasureRefusesCornersOfDifferentShapes(t *testing.T) {
	model := loadMeasuredModel(t, "shapes")

	flattened := Survey{
		Positions: make(Positions),
		Tolerance: closureTolerance,
		Registry:  model.registry,
	}
	for id, value := range model.survey.Positions {
		flattened.Positions[id] = value
	}

	// One corner of the square, written with the height left off.
	flattened.Positions["geom:V-03"] = Value{shape: ShapeCoordinate, components: []float64{4, 3}, unit: "m"}

	loop, ok := model.topology.Loop("geom:L-01")
	require.True(t, ok)

	measurement, diags := model.topology.MeasureLoop(loop, flattened)

	require.Len(t, diags, 1)
	assert.Equal(t, SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, "geom:V-03")
	assert.Contains(t, diags[0].Hint, "not the same one with a zero left out")

	_, ok = measurement.Area()
	assert.False(t, ok)
}

// TestMeasureWithoutADeclaredFrame is its own function because nothing is
// measured at all: a frame no registry file declares has no unit, and a figure
// in no unit is not an answer.
func TestMeasureWithoutADeclaredFrame(t *testing.T) {
	model := loadMeasuredModel(t, "shapes")

	loop, ok := model.topology.Loop("geom:L-01")
	require.True(t, ok)

	measurement, diags := model.topology.MeasureLoop(loop, Survey{
		Positions: model.survey.Positions,
		Tolerance: closureTolerance,
	})

	require.NotEmpty(t, diags)
	assert.Contains(t, diags[len(diags)-1].Hint, "not one the registry declares")

	assert.Equal(t, Unit(""), measurement.Unit())
	_, ok = measurement.Area()
	assert.False(t, ok)
}

// TestMeasureNothing is its own function because a nil is not a thing with
// something wrong with it: there is nothing to measure and nothing to report it
// against.
func TestMeasureNothing(t *testing.T) {
	model := loadMeasuredModel(t, "shapes")

	t.Run("measures no edge", func(t *testing.T) {
		measurement, diags := model.topology.MeasureEdge(nil, model.survey)

		assert.Empty(t, diags)
		assert.Equal(t, Measurement{}, measurement)
	})

	t.Run("measures no loop", func(t *testing.T) {
		measurement, diags := model.topology.MeasureLoop(nil, model.survey)

		assert.Empty(t, diags)
		assert.Equal(t, Measurement{}, measurement)
	})

	t.Run("measures no region", func(t *testing.T) {
		measurement, diags := model.topology.MeasureRegion(nil, model.boundaries, model.survey)

		assert.Empty(t, diags)
		assert.Equal(t, Measurement{}, measurement)
	})

	t.Run("measures a region which is bounded by nothing", func(t *testing.T) {
		unbounded := &SemanticNode{id: "site:S-999"}

		measurement, diags := model.topology.MeasureRegion(unbounded, model.boundaries, model.survey)

		assert.Empty(t, diags)
		assert.Equal(t, ID("site:S-999"), measurement.Subject())

		_, ok := measurement.Area()
		assert.False(t, ok)
		_, ok = measurement.Length()
		assert.False(t, ok)
	})
}

// measuredGraph is one fixture loaded whole, with the survey a measurement of
// anything in it is computed against.
//
// It is the whole model rather than the four passes [loadMeasuredModel] joins by
// hand, because that is what [Graph.Measure] takes and what a caller which did
// `go get` holds. The survey is placed over every corner the model has, which is
// what a caller measuring more than one thing would do; [Graph.Corners] is what
// says which of them any one answer needs.
func measuredGraph(t *testing.T, name string) (*Graph, Survey) {
	t.Helper()

	graph, diags := LoadGraph(filepath.Join("testdata", "measure", name))
	require.Empty(t, renderBoundaryDiagnostics(t, diags), "the fixture loads clean")

	survey := Survey{Tolerance: closureTolerance, Registry: graph.Registry()}
	for vertex := range graph.Topology().Vertices() {
		resolution, err := graph.Claims().Resolve(vertex.ID(), "position", graph.Registry())
		require.NoError(t, err)

		survey.Place(vertex.ID(), resolution)
	}

	return graph, survey
}

func TestGraphMeasure(t *testing.T) {
	graph, survey := measuredGraph(t, "shapes")

	testCases := []struct {
		name             string
		id               ID
		expectedArea     float64
		expectedLength   float64
		expectsArea      bool
		expectsLength    bool
		expectedCentroid Point
	}{
		{
			name:             "measures a semantic node through the loops which bound it",
			id:               "site:S-01",
			expectedArea:     12.0,
			expectsArea:      true,
			expectedLength:   14.0,
			expectsLength:    true,
			expectedCentroid: Point{2, 1.5, 0},
		},
		{
			name:             "measures a loop through the ring its edges traverse",
			id:               "geom:L-01",
			expectedArea:     12.0,
			expectsArea:      true,
			expectedLength:   14.0,
			expectsLength:    true,
			expectedCentroid: Point{2, 1.5, 0},
		},
		{
			name:             "measures an edge from its two ends",
			id:               "geom:E-02",
			expectsArea:      false,
			expectedLength:   3.0,
			expectsLength:    true,
			expectedCentroid: Point{4, 1.5, 0},
		},
		{
			name:             "measures a vertex from where it is",
			id:               "geom:V-03",
			expectsArea:      false,
			expectsLength:    false,
			expectedCentroid: Point{4, 3, 0},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			entity, held := graph.Entity(testCase.id)
			require.True(t, held, "the fixture holds %s", testCase.id)

			measurement, diags := graph.Measure(entity, survey)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			assert.Equal(t, testCase.id, measurement.Subject())
			assert.Equal(t, Unit("m"), measurement.Unit())

			area, computed := measurement.Area()
			assert.Equal(t, testCase.expectsArea, computed)
			if testCase.expectsArea {
				assert.InDelta(t, testCase.expectedArea, area, 1e-9)
			}

			length, computed := measurement.Length()
			assert.Equal(t, testCase.expectsLength, computed)
			if testCase.expectsLength {
				assert.InDelta(t, testCase.expectedLength, length, 1e-9)
			}

			centroid, computed := measurement.Centroid()
			require.True(t, computed)
			assert.Equal(t, testCase.expectedCentroid, centroid)
		})
	}
}

// TestGraphMeasureAgreesWithMeasuringEachFamilyDirectly is its own function
// because it is a property rather than a value: the dispatch exists so that
// nobody writes it again, and it earns that only while it answers exactly what
// the four calls beneath it answer.
func TestGraphMeasureAgreesWithMeasuringEachFamilyDirectly(t *testing.T) {
	graph, survey := measuredGraph(t, "shapes")

	region, held := graph.Node("site:S-01")
	require.True(t, held)

	loop, held := graph.Topology().Loop("geom:L-01")
	require.True(t, held)

	edge, held := graph.Topology().Edge("geom:E-02")
	require.True(t, held)

	vertex, held := graph.Topology().Vertex("geom:V-03")
	require.True(t, held)

	directly := []Measurement{
		must(graph.Topology().MeasureRegion(region, graph.Boundaries(), survey)),
		must(graph.Topology().MeasureLoop(loop, survey)),
		must(graph.Topology().MeasureEdge(edge, survey)),
		must(graph.Topology().MeasureVertex(vertex, survey)),
	}

	dispatched := []Measurement{
		must(graph.Measure(region, survey)),
		must(graph.Measure(loop, survey)),
		must(graph.Measure(edge, survey)),
		must(graph.Measure(vertex, survey)),
	}

	assert.Equal(t, directly, dispatched)
}

// must is the measurement of a call which reported nothing, which is every call
// above: the fixture measures clean, and a diagnostic here would be a fixture
// which changed rather than a dispatch which disagreed.
func must(measurement Measurement, _ []Diagnostic) Measurement { return measurement }

// TestGraphMeasureOfSomethingWithNoFamily is its own function because it asserts
// about the absence of a question rather than about a refusal to answer one.
func TestGraphMeasureOfSomethingWithNoFamily(t *testing.T) {
	graph, survey := measuredGraph(t, "shapes")

	measurement, diags := graph.Measure(nil, survey)

	assert.Empty(t, diags)
	assert.Equal(t, ID(""), measurement.Subject())

	var absent *Graph
	measurement, diags = absent.Measure(nil, survey)

	assert.Empty(t, diags)
	assert.Equal(t, ID(""), measurement.Subject())
}

func TestGraphCorners(t *testing.T) {
	graph, _ := measuredGraph(t, "shapes")

	testCases := []struct {
		name             string
		id               ID
		expectedVertices []ID
	}{
		{
			name:             "reaches a region's corners through its loops",
			id:               "site:S-01",
			expectedVertices: []ID{"geom:V-01", "geom:V-02", "geom:V-03", "geom:V-04"},
		},
		{
			name:             "reaches a loop's corners through the edges it names",
			id:               "geom:L-01",
			expectedVertices: []ID{"geom:V-01", "geom:V-02", "geom:V-03", "geom:V-04"},
		},
		{
			name:             "reaches an edge's two ends",
			id:               "geom:E-02",
			expectedVertices: []ID{"geom:V-02", "geom:V-03"},
		},
		{
			name:             "reaches a corner itself",
			id:               "geom:V-03",
			expectedVertices: []ID{"geom:V-03"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			entity, held := graph.Entity(testCase.id)
			require.True(t, held, "the fixture holds %s", testCase.id)

			var reached []ID
			for vertex := range graph.Corners(entity) {
				reached = append(reached, vertex.ID())
			}

			assert.Equal(t, testCase.expectedVertices, reached)
		})
	}

	t.Run("reaches nothing through something of no family", func(t *testing.T) {
		var reached []ID
		for vertex := range graph.Corners(nil) {
			reached = append(reached, vertex.ID())
		}

		assert.Empty(t, reached)
	})
}

// TestGraphCornersIsWhatAMeasurementNeeds is its own function because it is the
// contract between the two calls rather than a property of either: a survey
// placed over exactly these corners has to measure the same as one placed over
// the whole model, or the pair is an invitation to under-survey an answer.
func TestGraphCornersIsWhatAMeasurementNeeds(t *testing.T) {
	graph, whole := measuredGraph(t, "shapes")

	for _, id := range []ID{"site:S-01", "geom:L-01", "geom:E-02", "geom:V-03"} {
		t.Run("measures "+string(id)+" from its own corners alone", func(t *testing.T) {
			entity, held := graph.Entity(id)
			require.True(t, held)

			only := Survey{Tolerance: closureTolerance, Registry: graph.Registry()}
			for vertex := range graph.Corners(entity) {
				resolution, err := graph.Claims().Resolve(vertex.ID(), "position", graph.Registry())
				require.NoError(t, err)

				only.Place(vertex.ID(), resolution)
			}

			narrow, diags := graph.Measure(entity, only)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			broad, diags := graph.Measure(entity, whole)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			assert.Equal(t, broad, narrow)
		})
	}
}

// TestMeasurementReport is its own function because it renders the accuracy under
// the figures rather than the figures: a report which said how big something is
// and not how well that is known is the bare number the claim model refuses.
func TestMeasurementReport(t *testing.T) {
	model := loadMeasuredModel(t, "shapes")

	t.Run("writes the figures and the terms behind them", func(t *testing.T) {
		measurement, _ := model.measure(t, "site:S-01")

		report := measurement.Report()

		assert.Contains(t, report, measurement.String(), "the summary is the first line of the detail")
		assert.Contains(t, report, "corners known to")
		assert.Contains(t, report, "systematic 0.008 m shared with control:CP-3, counted once over 4 claims")
		assert.Contains(t, report, "independent 0.004 m")
	})

	t.Run("says when nothing states how well the corners are known", func(t *testing.T) {
		var measurement Measurement

		assert.Contains(t, measurement.Report(), "nothing states how well the corners are known")
	})
}

// TestMeasurementOfNothingReadsBack is its own function because it is about a
// receiver rather than a model: every accessor has to work on the zero value, so
// that a caller which did not read the diagnostics of a refused measurement gets
// an answer of "there is none" rather than a panic.
func TestMeasurementOfNothingReadsBack(t *testing.T) {
	var measurement Measurement

	assert.Equal(t, ID(""), measurement.Subject())
	assert.Equal(t, Unit(""), measurement.Unit())
	assert.Equal(t, ": nothing measurable", measurement.String())

	_, ok := measurement.Area()
	assert.False(t, ok)
	_, ok = measurement.Length()
	assert.False(t, ok)
	_, ok = measurement.Centroid()
	assert.False(t, ok)
	_, ok = measurement.Bounds()
	assert.False(t, ok)

	assert.True(t, measurement.Budget().Known(), "a budget with nothing in it hides nothing")
	assert.Equal(t, "(0.0 0.0 0.0) to (0.0 0.0 0.0)", Box{}.String())
}

// TestMeasurementString is its own function because the rendering is the one
// place a figure and its unit are written together, and an area written in
// metres rather than square metres is the kind of wrong nobody notices.
func TestMeasurementString(t *testing.T) {
	model := loadMeasuredModel(t, "shapes")

	measurement, _ := model.measure(t, "site:S-01")

	assert.Equal(t,
		"site:S-01: area 12.0 m², length 14.0 m, centroid (2.0 1.5 0.0), bounds (0.0 0.0 0.0) to (4.0 3.0 0.0)",
		measurement.String())
}

// TestMeasureRegionOfAnOpenRun is its own function because what a run measures
// is a different set of figures from what a region measures, not a variation on
// them: there is a length and a box and there is deliberately no area.
func TestMeasureRegionOfAnOpenRun(t *testing.T) {
	model := loadMeasuredRoot(t, boundaryFixture("run"))

	t.Run("measures how far a run of two edges reaches", func(t *testing.T) {
		railing, ok := model.nodes.Node("site:D-02")
		require.True(t, ok)

		measurement, diags := model.topology.MeasureRegion(railing, model.boundaries, model.survey)
		require.Empty(t, renderBoundaryDiagnostics(t, diags), "a run is not a loop with a gap in it")

		length, computed := measurement.Length()
		require.True(t, computed)
		assert.InDelta(t, 4.0, length, 1e-9, "two metres out and two metres along")

		// And no closing side. A perimeter would have counted the way back from
		// the free end to the corner it started at, which is a side nobody drew.
		_, hasArea := measurement.Area()
		assert.False(t, hasArea, "a chain encloses nothing")

		_, hasCentroid := measurement.Centroid()
		assert.False(t, hasCentroid, "there is no area to take the centroid of")

		bounds, hasBounds := measurement.Bounds()
		require.True(t, hasBounds)
		assert.Equal(t, Point{4, 3, 0}, bounds.Min)
		assert.Equal(t, Point{6, 5, 0}, bounds.Max)
	})

	t.Run("measures a run of one edge", func(t *testing.T) {
		doorway, ok := model.nodes.Node("site:D-01")
		require.True(t, ok)

		measurement, diags := model.topology.MeasureRegion(doorway, model.boundaries, model.survey)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		length, computed := measurement.Length()
		require.True(t, computed)
		assert.InDelta(t, 0.9, length, 1e-9)
	})

	t.Run("carries the accuracy of the corners which put the run where it is", func(t *testing.T) {
		railing, ok := model.nodes.Node("site:D-02")
		require.True(t, ok)

		measurement, _ := model.topology.MeasureRegion(railing, model.boundaries, model.survey)

		assert.NotEmpty(t, measurement.Budget().Terms(), "the run is as well known as its corners are")
	})

	t.Run("still measures the room beside it as a region", func(t *testing.T) {
		room, ok := model.nodes.Node("site:S-101")
		require.True(t, ok)

		measurement, diags := model.topology.MeasureRegion(room, model.boundaries, model.survey)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		area, hasArea := measurement.Area()
		require.True(t, hasArea)
		assert.InDelta(t, 12.0, area, 1e-9)
	})
}

// TestMeasureRegionOfAnOpenRunMovesWithTheCornerItShares is its own function
// because the assertion is about two shapes at once: the run and the wall it
// begins at are one vertex, so surveying that vertex somewhere else moves both.
func TestMeasureRegionOfAnOpenRunMovesWithTheCornerItShares(t *testing.T) {
	model := loadMeasuredRoot(t, boundaryFixture("run"))

	railing, ok := model.nodes.Node("site:D-02")
	require.True(t, ok)

	before, diags := model.topology.MeasureRegion(railing, model.boundaries, model.survey)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	length, computed := before.Length()
	require.True(t, computed)
	require.InDelta(t, 4.0, length, 1e-9)

	// The room's south-east corner moves a metre east. Nothing about the
	// railing is edited, and the railing is a metre shorter, because it names
	// that corner rather than restating where it was.
	moved := model.survey
	moved.Positions = Positions{}
	for id, value := range model.survey.Positions {
		moved.Positions[id] = value
	}
	moved.Positions["geom:V-03"] = CoordinateValue([]float64{5, 3, 0}, "m")

	after, diags := model.topology.MeasureRegion(railing, model.boundaries, moved)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	length, computed = after.Length()
	require.True(t, computed)
	assert.InDelta(t, 3.0, length, 1e-9, "the run followed the corner the wall moved")
}
