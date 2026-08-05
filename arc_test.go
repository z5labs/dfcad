// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chordTolerance and coarseChordTolerance are the two chord tolerances the arc
// fixture's registry declares, named here so that a test says which decision it
// is drawing to rather than repeating a number the registry owns.
const (
	chordTolerance       = "chord-deviation"
	coarseChordTolerance = "coarse-chord-deviation"
)

// semicircle is the area a half disc of radius two encloses, which every figure
// below is written in terms of rather than as a decimal: an expectation written
// as 22.283185307179586 says nothing about where it came from.
var semicircle = 2 * math.Pi

func TestMeasureCurvedRegion(t *testing.T) {
	// The centroid of a half disc of radius two, measured from the centre of its
	// straight side. It is the standard four over three pi times the radius, and
	// it is written out because the whole point of the arithmetic under test is
	// that it agrees with the closed form rather than with a tessellation.
	bulge := 8 / (3 * math.Pi)

	testCases := []struct {
		name     string
		region   ID
		area     float64
		length   float64
		centroid Point
		bounds   Box
	}{
		{
			name:     "adds the half disc a wall bows out into",
			region:   "site:S-01",
			area:     16 + semicircle,
			length:   12 + 2*math.Pi,
			centroid: Point{(16*2 + semicircle*(4+bulge)) / (16 + semicircle), 2, 0},
			bounds:   Box{Min: Point{0, 0, 0}, Max: Point{6, 4, 0}, Unit: "m"},
		},
		{
			name:     "takes away the half disc a wall bows in from",
			region:   "site:S-11",
			area:     16 - semicircle,
			length:   12 + 2*math.Pi,
			centroid: Point{(16*12 - semicircle*(14-bulge)) / (16 - semicircle), 2, 0},
			bounds:   Box{Min: Point{10, 0, 0}, Max: Point{14, 4, 0}, Unit: "m"},
		},
		{
			name:     "measures a circle written as two arcs and no straight edge at all",
			region:   "site:S-21",
			area:     4 * math.Pi,
			length:   4 * math.Pi,
			centroid: Point{22, 0, 0},
			bounds:   Box{Min: Point{20, -2, 0}, Max: Point{24, 2, 0}, Unit: "m"},
		},
	}

	model := loadMeasuredModel(t, "arcs")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			measurement, diags := model.measure(t, testCase.region)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			area, ok := measurement.Area()
			require.True(t, ok)
			assert.InDelta(t, testCase.area, area, 1e-12)

			length, ok := measurement.Length()
			require.True(t, ok)
			assert.InDelta(t, testCase.length, length, 1e-12)

			centroid, ok := measurement.Centroid()
			require.True(t, ok)
			for axis := range centroid {
				assert.InDelta(t, testCase.centroid[axis], centroid[axis], 1e-12, "axis %d", axis)
			}

			bounds, ok := measurement.Bounds()
			require.True(t, ok)
			for axis := range 3 {
				assert.InDelta(t, testCase.bounds.Min[axis], bounds.Min[axis], 1e-12, "min of axis %d", axis)
				assert.InDelta(t, testCase.bounds.Max[axis], bounds.Max[axis], 1e-12, "max of axis %d", axis)
			}
			assert.Equal(t, Unit("m"), bounds.Unit)
		})
	}
}

func TestMeasureCurvedEdge(t *testing.T) {
	testCases := []struct {
		name     string
		edge     ID
		length   float64
		centroid Point
		bounds   Box
	}{
		{
			name:   "measures the curve and not the chord across it",
			edge:   "geom:E-02",
			length: 2 * math.Pi,
			// The centroid of a half circle of wire, which is two over pi times
			// the radius from the centre — further in than the curve and further
			// out than the chord.
			centroid: Point{4 + 4/math.Pi, 2, 0},
			bounds:   Box{Min: Point{4, 0, 0}, Max: Point{6, 4, 0}, Unit: "m"},
		},
		{
			name:     "reaches where the curve reaches and not only where its ends do",
			edge:     "geom:E-12",
			length:   2 * math.Pi,
			centroid: Point{14 - 4/math.Pi, 2, 0},
			bounds:   Box{Min: Point{12, 0, 0}, Max: Point{14, 4, 0}, Unit: "m"},
		},
		{
			name:     "measures a straight edge from its two ends",
			edge:     "geom:E-01",
			length:   4,
			centroid: Point{2, 0, 0},
			bounds:   Box{Min: Point{0, 0, 0}, Max: Point{4, 0, 0}, Unit: "m"},
		},
	}

	model := loadMeasuredModel(t, "arcs")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			edge, ok := model.topology.Edge(testCase.edge)
			require.True(t, ok)

			measurement, diags := model.topology.MeasureEdge(edge, model.survey)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			length, ok := measurement.Length()
			require.True(t, ok)
			assert.InDelta(t, testCase.length, length, 1e-12)

			centroid, ok := measurement.Centroid()
			require.True(t, ok)
			for axis := range centroid {
				assert.InDelta(t, testCase.centroid[axis], centroid[axis], 1e-12, "axis %d", axis)
			}

			bounds, ok := measurement.Bounds()
			require.True(t, ok)
			for axis := range 3 {
				assert.InDelta(t, testCase.bounds.Min[axis], bounds.Min[axis], 1e-12, "min of axis %d", axis)
				assert.InDelta(t, testCase.bounds.Max[axis], bounds.Max[axis], 1e-12, "max of axis %d", axis)
			}
		})
	}
}

// TestAnArcIsNeverTessellatedByLoadingQueryingOrPrinting is its own function
// because what it asserts is an absence: nothing on the path from the file to an
// answer turns the curve into segments, so the curve is still there afterwards
// and the answer is the closed form rather than a sum over chords.
//
// The area is asserted to a part in a million million. A tessellation fine
// enough to reach that on a two-metre radius would need some tens of thousands
// of segments, so the assertion is not "close enough" — it is that no
// tessellation happened.
func TestAnArcIsNeverTessellatedByLoadingQueryingOrPrinting(t *testing.T) {
	model := loadMeasuredModel(t, "arcs")

	t.Run("loads the curved wall as one edge between two corners", func(t *testing.T) {
		loop, ok := model.topology.Loop("geom:L-01")
		require.True(t, ok)
		assert.Equal(t, []ID{"geom:E-01", "geom:E-02", "geom:E-03", "geom:E-04"}, loop.Edges(),
			"a bay window is one edge of the ring, not the eight a drawing of it would take")

		edge, ok := model.topology.Edge("geom:E-02")
		require.True(t, ok)

		start, end := edge.Vertices()
		assert.Equal(t, ID("geom:V-02"), start)
		assert.Equal(t, ID("geom:V-03"), end)
	})

	t.Run("answers from the circle rather than from a sum over chords", func(t *testing.T) {
		measurement, diags := model.measure(t, "site:S-01")
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		area, ok := measurement.Area()
		require.True(t, ok)
		assert.InDelta(t, 16+semicircle, area, 1e-12)
	})

	t.Run("measures the same after a print and a read back", func(t *testing.T) {
		// Round-tripping asserted as a property rather than against an expected
		// string: what has to survive the printer is the curve, and a test which
		// compared the text would pass on output which no longer read back.
		printed := loadMeasuredRoot(t, reprinted(t, filepath.Join("testdata", "measure", "arcs")))

		before, diags := model.measure(t, "site:S-01")
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		after, diags := printed.measure(t, "site:S-01")
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		assert.Equal(t, valuesOf(before), valuesOf(after),
			"a curve read and written back is the curve, not the segments somebody would have drawn it with")

		loop, ok := printed.topology.Loop("geom:L-01")
		require.True(t, ok)
		assert.Len(t, loop.Edges(), 4, "the printer wrote one edge for the bay window, not a fan of them")
	})
}

// reprinted writes every file of a tree back out through the printer and returns
// where it put them, which is what a round trip is loaded out of.
func reprinted(t *testing.T, root string) string {
	t.Helper()

	into := t.TempDir()

	entries, err := os.ReadDir(root)
	require.NoError(t, err)

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".dfc" {
			continue
		}

		file, err := LoadFile(filepath.Join(root, entry.Name()))
		require.NoError(t, err)

		var printed bytes.Buffer
		require.NoError(t, Print(&printed, file))

		require.NoError(t, os.WriteFile(filepath.Join(into, entry.Name()), printed.Bytes(), 0o644))
	}

	return into
}

func TestTessellateEdge(t *testing.T) {
	testCases := []struct {
		name      string
		edge      ID
		tolerance string
		segments  int
	}{
		{
			name:      "draws a half circle finely enough to stay within a centimetre of it",
			edge:      "geom:E-02",
			tolerance: chordTolerance,
			segments:  16,
		},
		{
			name:      "draws the same half circle in fewer segments where half a metre will do",
			edge:      "geom:E-02",
			tolerance: coarseChordTolerance,
			segments:  3,
		},
		{
			name:      "leaves a straight edge as the one segment it already is",
			edge:      "geom:E-01",
			tolerance: chordTolerance,
			segments:  1,
		},
	}

	model := loadMeasuredModel(t, "arcs")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			edge, ok := model.topology.Edge(testCase.edge)
			require.True(t, ok)

			drawn, diags := model.topology.TessellateEdge(edge, model.survey, testCase.tolerance)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			assert.Equal(t, testCase.edge, drawn.Subject())
			assert.Equal(t, Unit("m"), drawn.Unit())
			assert.False(t, drawn.Closed(), "an edge is not a ring")
			assert.Len(t, drawn.Points(), testCase.segments+1)

			// The tolerance the drawing was made to is on the result, so what it
			// is good for can be read off it rather than remembered.
			tolerance := drawn.ChordTolerance()
			assert.Equal(t, testCase.tolerance, tolerance.Name)
			assert.Equal(t, Unit("m"), tolerance.Unit)

			assert.LessOrEqual(t, drawn.Deviation(), tolerance.Value)

			assert.Contains(t, drawn.String(), string(testCase.edge))
			assert.Contains(t, drawn.String(), testCase.tolerance)

			ends := model.survey.Positions
			assert.Equal(t, pointOf(t, ends[edgeStart(edge)]), drawn.Points()[0])
			assert.Equal(t, pointOf(t, ends[edgeEnd(edge)]), drawn.Points()[len(drawn.Points())-1])
		})
	}
}

// TestATessellationStaysWithinItsChordTolerance is its own function because it
// measures the deviation itself rather than reading the one the result reports.
//
// A result which recorded the deviation it was asked for and drew something else
// would pass every assertion which read that number back. So the segments are
// measured against the circle they stand in for: the furthest a chord of a
// circle falls from it is at its midpoint, and how far that is is the radius
// less the distance from the centre.
func TestATessellationStaysWithinItsChordTolerance(t *testing.T) {
	model := loadMeasuredModel(t, "arcs")

	for _, name := range []string{chordTolerance, coarseChordTolerance} {
		t.Run("follows the curve to within "+name, func(t *testing.T) {
			edge, ok := model.topology.Edge("geom:E-02")
			require.True(t, ok)

			drawn, diags := model.topology.TessellateEdge(edge, model.survey, name)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			centre, radius := Point{4, 2, 0}, 2.0

			var worst float64
			points := drawn.Points()
			for i := range len(points) - 1 {
				middle := pointScale(pointAdd(points[i], points[i+1]), 0.5)
				worst = math.Max(worst, math.Abs(radius-pointLength(pointSub(middle, centre))))
			}

			assert.Greater(t, worst, 0.0, "a curve drawn as straight segments does depart from it somewhere")
			assert.LessOrEqual(t, worst, drawn.ChordTolerance().Value,
				"no segment falls further from the curve than the tolerance it was drawn to")
			assert.InDelta(t, worst, drawn.Deviation(), 1e-12,
				"the deviation reported is the one the segments actually have")
		})
	}
}

// TestATessellationIsDeterministic is its own function because what it asserts is
// an equality between two answers rather than either answer's value, and it is
// exact rather than within a tolerance.
//
// A drawing which differed between two runs by a bit in the last place would
// show up as a diff in whatever it was written into, and as an argument about
// which of the two runs was right.
func TestATessellationIsDeterministic(t *testing.T) {
	first := loadMeasuredModel(t, "arcs")
	second := loadMeasuredModel(t, "arcs")

	t.Run("draws the same edge the same way twice", func(t *testing.T) {
		edge, ok := first.topology.Edge("geom:E-02")
		require.True(t, ok)

		one, diags := first.topology.TessellateEdge(edge, first.survey, chordTolerance)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		other, diags := first.topology.TessellateEdge(edge, first.survey, chordTolerance)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		assert.Equal(t, one.Points(), other.Points())
		assert.Equal(t, one.Deviation(), other.Deviation())
	})

	t.Run("draws the same edge the same way after a second load", func(t *testing.T) {
		edge, ok := first.topology.Edge("geom:E-02")
		require.True(t, ok)

		reloaded, ok := second.topology.Edge("geom:E-02")
		require.True(t, ok)

		one, _ := first.topology.TessellateEdge(edge, first.survey, chordTolerance)
		other, _ := second.topology.TessellateEdge(reloaded, second.survey, chordTolerance)

		assert.Equal(t, one.Points(), other.Points())
	})
}

func TestTessellateLoop(t *testing.T) {
	model := loadMeasuredModel(t, "arcs")

	testCases := []struct {
		name   string
		loop   ID
		points int
	}{
		{
			name:   "draws the straight walls once each and the curved one as many times as it takes",
			loop:   "geom:L-01",
			points: 3 + 16,
		},
		{
			name:   "draws a ring of two arcs as two arcs",
			loop:   "geom:L-21",
			points: 32,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			loop, ok := model.topology.Loop(testCase.loop)
			require.True(t, ok)

			drawn, diags := model.topology.TessellateLoop(loop, model.survey, chordTolerance)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			assert.Equal(t, testCase.loop, drawn.Subject())
			assert.True(t, drawn.Closed(), "the fixture's rings close")
			assert.Len(t, drawn.Points(), testCase.points,
				"a ring does not repeat its first point at the end")
			assert.LessOrEqual(t, drawn.Deviation(), drawn.ChordTolerance().Value)
		})
	}
}

// TestATessellatedRingKeepsItsCorners is its own function because what it asserts
// is about where two drawn edges meet rather than about how closely either
// follows its own curve.
//
// Every arc contributes the corners it was surveyed at, so a ring drawn as
// segments still closes on the corners the model holds. An implementation which
// recomputed an end from the parameterisation would leave gaps a rounding wide,
// and the loop would then be reported as not closing against a model in which
// nothing is wrong.
func TestATessellatedRingKeepsItsCorners(t *testing.T) {
	model := loadMeasuredModel(t, "arcs")

	loop, ok := model.topology.Loop("geom:L-01")
	require.True(t, ok)

	drawn, diags := model.topology.TessellateLoop(loop, model.survey, chordTolerance)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	points := drawn.Points()

	for _, corner := range []ID{"geom:V-01", "geom:V-02", "geom:V-03", "geom:V-04"} {
		assert.Contains(t, points, pointOf(t, model.survey.Positions[corner]),
			"the drawn ring passes through %s exactly", corner)
	}
}

func TestDegenerateArcIsADiagnostic(t *testing.T) {
	testCases := []struct {
		name string
		edge ID
		arc  Arc
	}{
		{
			name: "reports an arc whose centre is on one of its ends",
			edge: "geom:E-02",
			arc:  arcAt(Point{4, 0, 0}, Point{6, 2, 0}),
		},
		{
			name: "reports an arc whose ends are not the same distance from its centre",
			edge: "geom:E-02",
			arc:  arcAt(Point{4, 2, 0}, Point{7, 2, 0}),
		},
		{
			name: "reports an arc whose point on the curve is in line with its centre and its start",
			edge: "geom:E-02",
			arc:  arcAt(Point{4, 2, 0}, Point{4, 4, 0}),
		},
		{
			name: "reports an arc whose point on the curve is past the end it reaches",
			edge: "geom:E-01",
			arc:  arcAt(Point{2, -1.5, 0}, Point{4.5, -1.5, 0}),
		},
		{
			name: "reports an arc which leaves the plane the ring lies in",
			edge: "geom:E-02",
			arc:  arcAt(Point{4, 2, 1}, Point{6, 2, 1}),
		},
		{
			name: "reports an arc with no centre stated at all",
			edge: "geom:E-02",
			arc:  Arc{},
		},
	}

	model := loadMeasuredModel(t, "arcs")

	var rendered string
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			edge, ok := model.topology.Edge(testCase.edge)
			require.True(t, ok)

			survey := model.survey
			survey.Curvature = Curvature{edge.ID(): testCase.arc}

			_, diags := model.topology.MeasureEdge(edge, survey)
			require.NotEmpty(t, diags, "a parameterisation which is not a curve is reported and not measured")

			for _, diag := range diags {
				assert.Equal(t, SeverityError, diag.Severity)
			}

			rendered += renderBoundaryDiagnostics(t, diags)
		})
	}

	assert.Equal(t, expectedArcDiagnostics(t, "degenerate.txt", rendered), rendered)
}

// TestCurvedRingsAreRefusedRatherThanApproximated is its own function because
// every case in it is an answer deliberately not given: an operation which would
// have had to draw a curve to answer says so instead of drawing one.
func TestCurvedRingsAreRefusedRatherThanApproximated(t *testing.T) {
	model := loadMeasuredModel(t, "arcs")

	t.Run("refuses to nest one ring inside another where either bends", func(t *testing.T) {
		measurement, diags := model.measure(t, "site:S-31")
		require.NotEmpty(t, diags)

		_, ok := measurement.Area()
		assert.False(t, ok, "no area, rather than one computed from the chords")
	})

	t.Run("refuses to read a curved boundary as a plane figure to overlay", func(t *testing.T) {
		node, ok := model.nodes.Node("site:S-01")
		require.True(t, ok)

		region, diags := model.topology.RegionOf(node, model.boundaries, model.survey)
		require.NotEmpty(t, diags)

		assert.True(t, region.Empty(), "no figure, rather than one drawn to a resolution nobody chose")
	})

	rendered := measureRegions(t, "arcs")
	assert.Equal(t, expectedMeasureDiagnostics(t, "arcs", rendered), rendered)
}

// TestTessellationRefusesAToleranceNothingCouldBeDrawnTo is its own function
// because the refusal is about the number of points rather than about the
// tolerance being unusable: the tolerance is declared, is in the right unit and
// is a perfectly good number, and following the curve that closely would still
// take more segments than anything behind it supports.
func TestTessellationRefusesAToleranceNothingCouldBeDrawnTo(t *testing.T) {
	model := loadMeasuredModel(t, "arcs")

	edge, ok := model.topology.Edge("geom:E-02")
	require.True(t, ok)

	drawn, diags := model.topology.TessellateEdge(edge, model.survey, "hair-chord-deviation")
	require.NotEmpty(t, diags)

	assert.Empty(t, drawn.Points(), "no points, rather than a million of them")
}

func TestTessellationRefusesAToleranceItCannotApply(t *testing.T) {
	testCases := []struct {
		name      string
		tolerance string
	}{
		{name: "refuses a chord tolerance the registry does not declare", tolerance: "no-such-tolerance"},
		{name: "refuses a chord tolerance which is not in the unit of the frame", tolerance: closureTolerance},
	}

	model := loadMeasuredModel(t, "arcs")

	edge, ok := model.topology.Edge("geom:E-02")
	require.True(t, ok)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := model.registry
			if testCase.tolerance == closureTolerance {
				// The declared closure tolerance is in the frame's unit, so the
				// case which needs one which is not asks for a frame with no
				// unit at all: both are the same refusal, that the number and
				// the geometry are not comparable.
				registry = nil
			}

			survey := model.survey
			survey.Registry = registry

			drawn, diags := model.topology.TessellateEdge(edge, survey, testCase.tolerance)
			require.NotEmpty(t, diags)

			assert.Empty(t, drawn.Points(), "no points, rather than points drawn to a number nobody declared")
		})
	}
}

// arcAt is an arc stated by two literal positions in metres, which is what a
// test which is about the geometry rather than about the claims behind it needs.
func arcAt(centre, through Point) Arc {
	return Arc{Centre: coordinateAt(centre), Through: coordinateAt(through)}
}

// coordinateAt is one literal position in metres.
func coordinateAt(point Point) Value {
	return Value{shape: ShapeCoordinate, components: []float64{point[0], point[1], point[2]}, unit: "m"}
}

// pointOf is a resolved position as a point, failing the test where it is not a
// coordinate.
func pointOf(t *testing.T, value Value) Point {
	t.Helper()

	written, ok := value.Coordinate()
	require.True(t, ok)

	return asPoint(written)
}

// edgeStart and edgeEnd are the two vertices of an edge, which the tests above
// read to assert that a drawing begins and ends at the surveyed corners.
func edgeStart(edge *Edge) ID {
	start, _ := edge.Vertices()
	return start
}

func edgeEnd(edge *Edge) ID {
	_, end := edge.Vertices()
	return end
}

// expectedArcDiagnostics returns the rendering held beside the arc fixture,
// having first rewritten it from got when -update was passed.
func expectedArcDiagnostics(t *testing.T, name string, got string) string {
	t.Helper()

	path := filepath.Join("testdata", "measure", "arcs", name)
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestACurveMeasuresTheSameTraversedEitherWay is its own function because what it
// asserts is an equality between two answers rather than either answer's value,
// and it is exact rather than within a tolerance.
//
// An edge is written once and the rooms either side of it traverse it in
// opposite directions, so the bulge has to belong to the wall rather than to the
// traversal. An implementation which read the arc the way the edge was written
// and not the way the ring runs would have the bay window bow into one of the two
// rooms.
func TestACurveMeasuresTheSameTraversedEitherWay(t *testing.T) {
	model := loadMeasuredModel(t, "arcs")

	written, diags := model.measure(t, "site:S-01")
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	backwards, diags := model.measure(t, "site:S-02")
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	assert.Equal(t, valuesOf(written), valuesOf(backwards))
}

// TestACurveWhichCrossesTheRestOfTheRingIsADiagnostic is its own function because
// the crossing is between a curve and the edges around it rather than between two
// straight ones, which is a different pass with a different set of assertions.
//
// The arc it puts on the south wall bows so far up that the wall leaves the room
// through the other three. Nothing about the chord across it says so — the chord
// is where it always was, along the bottom — so a ring judged at its chords would
// call this shape sound.
func TestACurveWhichCrossesTheRestOfTheRingIsADiagnostic(t *testing.T) {
	model := loadMeasuredModel(t, "arcs")

	loop, ok := model.topology.Loop("geom:L-01")
	require.True(t, ok)

	// The circle through the two ends of the south wall whose top is above the
	// north one: a bulge which reaches out of the room it is a wall of.
	centre, radius := Point{2, 1.75, 0}, math.Sqrt(7.0625)

	survey := model.survey
	survey.Curvature = Curvature{"geom:E-01": arcAt(centre, Point{2, centre[1] + radius, 0})}

	measurement, diags := model.topology.MeasureLoop(loop, survey)
	require.NotEmpty(t, diags)

	_, ok = measurement.Area()
	assert.False(t, ok, "no area, rather than the signed sum over a ring which crosses itself")

	rendered := renderBoundaryDiagnostics(t, diags)
	assert.Equal(t, expectedArcDiagnostics(t, "crossing.txt", rendered), rendered)
}

// TestTessellateAnOpenTraversal is its own function because a traversal which
// does not close has an end no step of it begins at, which a closed ring does
// not: the drawing has to reach that corner rather than stop a whole edge short
// of it, and none of the assertions above would notice if it did.
func TestTessellateAnOpenTraversal(t *testing.T) {
	model := loadMeasuredModel(t, "arcs")

	loop, ok := model.topology.Loop("geom:L-51")
	require.True(t, ok)

	// The gap is reported by the pass whose job that is, and the segments as far
	// as the traversal got are still what a caller drawing the failure wants.
	drawn, diags := model.topology.TessellateLoop(loop, model.survey, chordTolerance)
	require.NotEmpty(t, diags, "a ring which does not close says so")

	assert.False(t, drawn.Closed())

	points := drawn.Points()
	require.Len(t, points, 1+16+1, "one corner, the curve, and the corner the traversal stopped at")

	assert.Equal(t, pointOf(t, model.survey.Positions["geom:V-51"]), points[0])
	assert.Equal(t, pointOf(t, model.survey.Positions["geom:V-52"]), points[1])
	assert.Equal(t, pointOf(t, model.survey.Positions["geom:V-53"]), points[len(points)-1])

	assert.NotEqual(t, points[len(points)-2], points[len(points)-1],
		"the last segment has an extent; the end is not the corner before it written twice")
}
