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

	root := filepath.Join("testdata", "measure", name)

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
