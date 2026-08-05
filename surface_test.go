// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The surface fixture is one model holding three pieces of ground, each of which
// puts the derivation in a different situation: one with enough shots, one with
// two, and one with four on a line.
const (
	terraceRegion = ID("site:S-terrace")
	stripRegion   = ID("site:S-strip")
	ridgeRegion   = ID("site:S-ridge")
)

// surfacePosition is the predicate the corners of the fixture are read from.
const surfacePosition = "position"

// terraceSession is the occupation every shot the terrace rests on was taken in,
// which is what makes a stated systematic term reach the whole surface at once.
const terraceSession = ID("session:2026-05-06-am")

// The shots of the terrace fall on this plane, and every level a test asserts is
// read off it rather than off a number somebody copied out of a run.
//
// A triangulation over points on a plane reproduces the plane exactly, which is
// what makes an expected elevation something a test can compute rather than
// record. A test which recorded them would pass just as happily against a
// surface which had stopped honouring its own shots, as long as it went on
// producing whatever it produced last time.
func terraceLevel(x, y float64) float64 { return 100 + 0.05*x - 0.02*y }

// loadSurfaceFixture loads the surface fixture, failing the test where the load
// reports anything.
func loadSurfaceFixture(t *testing.T) *Graph {
	t.Helper()

	graph, diags := LoadGraph(filepath.Join("testdata", "surface", "terrace"))
	require.NotNil(t, graph, "a load always yields a usable graph")
	require.Empty(t, renderGraphDiagnostics(t, diags), "the fixture loads clean")

	return graph
}

// surfaceAgainst is the geometry derivation every surface below is read against.
func surfaceAgainst(cache *Cache) Derivation {
	return Derivation{Tolerance: closureTolerance, Position: surfacePosition, Cache: cache}
}

// surfaceOf derives one surface, failing the test where deriving it reported
// anything.
func surfaceOf(t *testing.T, graph *Graph, subject ID, against SurfaceDerivation) Surface {
	t.Helper()

	against.Against = surfaceAgainst(against.Against.Cache)

	surface, diags := graph.SurfaceWithin(subject, against)
	require.Empty(t, renderGraphDiagnostics(t, diags), "%s derives a surface clean", subject)

	return surface
}

// resting is the identity of every shot a surface rests on, in the order they
// came back.
func resting(surface Surface) []string {
	out := make([]string, 0, surface.Len())
	for _, point := range surface.Points() {
		out = append(out, string(point.Observation()))
	}
	return out
}

// TestSurfaceWithin is the whole question in one table: given a region and a
// corpus, which shots does the ground surface rest on.
func TestSurfaceWithin(t *testing.T) {
	testCases := []struct {
		name       string
		derivation SurfaceDerivation
		points     []string
		facets     bool
	}{
		{
			name:   "rests on the shots inside the region, carrying in the ones written elsewhere",
			points: []string{"shot:0001", "shot:0002", "shot:0003", "shot:0004", "shot:0005", "shot:0006", "shot:0007"},
			facets: true,
		},
		{
			name:       "reaches the boundary when the shots it cannot place are asked for",
			derivation: SurfaceDerivation{Ambiguous: true},
			points: []string{
				"shot:0001", "shot:0002", "shot:0003", "shot:0004",
				"shot:0005", "shot:0006", "shot:0007", "shot:0011",
			},
			facets: true,
		},
		{
			name:       "weights every shot by distance rather than triangulating them",
			derivation: SurfaceDerivation{Method: SurfaceIDW},
			points:     []string{"shot:0001", "shot:0002", "shot:0003", "shot:0004", "shot:0005", "shot:0006", "shot:0007"},
			facets:     false,
		},
	}

	graph := loadSurfaceFixture(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			surface := surfaceOf(t, graph, terraceRegion, testCase.derivation)

			require.True(t, surface.Derived(), "the terrace holds enough shots to derive a surface")
			assert.Equal(t, testCase.points, resting(surface))
			assert.Equal(t, terraceRegion, surface.Subject())
			assert.Equal(t, ID("frame:site"), surface.Frame())
			assert.Equal(t, Unit("m"), surface.Unit())

			if testCase.facets {
				assert.NotEmpty(t, surface.Facets(), "a triangulation is its facets")
			} else {
				assert.Empty(t, surface.Facets(), "only a triangulation has facets")
			}
		})
	}
}

// TestSurfaceRestsOnNoRetiredShot is the one exclusion worth its own function:
// a retired record is not evidence, and a surface which rested on one would put
// a five metre error into the middle of the terrace.
func TestSurfaceRestsOnNoRetiredShot(t *testing.T) {
	graph := loadSurfaceFixture(t)

	surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

	assert.NotContains(t, resting(surface), "shot:0010", "a retired record is not evidence")
	assert.NotContains(t, surface.Observations(), ID("shot:0010"),
		"nor is it provenance: a surface which named it would say it had been used")
	assert.NotContains(t, resting(surface), "shot:0009", "a shot outside the region is not in it")
}

// TestSurfaceElevation is the query the whole thing exists for, asked at points
// the shots never covered.
//
// Every expectation is computed from the plane the fixture's shots lie on rather
// than recorded, so a surface which stopped interpolating its own points would
// fail here rather than pass against its own last output.
func TestSurfaceElevation(t *testing.T) {
	testCases := []struct {
		name   string
		at     Point
		inside bool
	}{
		{name: "answers between the shots", at: Point{4, 4, 0}, inside: true},
		{name: "answers at a shot with that shot's own level", at: Point{10, 6, 0}, inside: true},
		{name: "answers on a facet edge", at: Point{12, 7, 0}, inside: true},
		{name: "answers on the hull itself", at: Point{2, 2, 0}, inside: true},
		{name: "reports a point short of the first shot as outside", at: Point{0, 0, 0}},
		{name: "reports a point beyond the last shot as outside", at: Point{19, 11, 0}},
		{name: "reports a point in the next field as outside", at: Point{25, 3, 0}},
	}

	graph := loadSurfaceFixture(t)
	surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			elevation, inside := surface.Elevation(testCase.at)

			require.Equal(t, testCase.inside, inside)
			assert.Equal(t, testCase.inside, surface.Covers(testCase.at),
				"Covers and Elevation apply one rule at the edge")

			if !testCase.inside {
				assert.Zero(t, elevation, "a point outside is reported as outside and not answered for")
				return
			}

			assert.InDelta(t, terraceLevel(testCase.at[0], testCase.at[1]), elevation.Value(), 1e-9)
			assert.Equal(t, testCase.at[0], elevation.At()[0])
			assert.Equal(t, testCase.at[1], elevation.At()[1])
			assert.Equal(t, elevation.Value(), elevation.At()[2])
			assert.Equal(t, SurfaceTIN, elevation.Method())
			assert.Positive(t, elevation.Uncertainty(), "a level is known no better than the shots behind it")
			assert.NotEmpty(t, elevation.From(), "a level says which shots it came from")
			assert.Len(t, elevation.Weights(), len(elevation.From()))
		})
	}
}

// TestSurfaceReportsOutsideRatherThanExtrapolating is the rule at the edge said
// as a property rather than as a handful of points.
//
// The surface reaches as far as the shots and no further. Continuing the last
// facet's slope past the last shot would produce a level which reads exactly
// like a surveyed one and has no measurement under it at all.
func TestSurfaceReportsOutsideRatherThanExtrapolating(t *testing.T) {
	graph := loadSurfaceFixture(t)
	surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

	t.Run("answers nowhere beyond the hull of its shots", func(t *testing.T) {
		// A sweep of the whole region, which is larger than the shots cover. The
		// hull runs from (2, 2) to (18, 10) and the region from (0, 0) to
		// (20, 12), so the sweep crosses the edge on all four sides.
		for x := 0.0; x <= 20.0; x += 0.5 {
			for y := 0.0; y <= 12.0; y += 0.5 {
				at := Point{x, y, 0}

				elevation, inside := surface.Elevation(at)

				expected := x >= 2 && x <= 18 && y >= 2 && y <= 10
				require.Equal(t, expected, inside, "at %v", at)

				if inside {
					require.InDelta(t, terraceLevel(x, y), elevation.Value(), 1e-9, "at %v", at)
				}
			}
		}
	})

	t.Run("reaches further when the shots it cannot place are asked for", func(t *testing.T) {
		wider := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{Ambiguous: true})

		at := Point{10, 12, 0}

		_, narrow := surface.Elevation(at)
		assert.False(t, narrow, "the shot at the boundary is not one this surface rests on")

		elevation, reached := wider.Elevation(at)
		require.True(t, reached, "the ambiguous shot is what carries the surface to the boundary")
		assert.InDelta(t, terraceLevel(10, 12), elevation.Value(), 1e-9)
	})
}

// TestSurfaceHonoursEveryShotItRestsOn is the property a triangulation has and a
// weighted mean does not, which is the reason both exist.
func TestSurfaceHonoursEveryShotItRestsOn(t *testing.T) {
	graph := loadSurfaceFixture(t)

	t.Run("returns each shot's own level at that shot", func(t *testing.T) {
		surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

		for _, point := range surface.Points() {
			elevation, inside := surface.Elevation(point.At())

			require.True(t, inside, "%s is a point of the surface", point.Observation())
			assert.InDelta(t, point.Elevation(), elevation.Value(), 1e-9,
				"%s is the level the surface has there", point.Observation())
		}
	})

	t.Run("weights a level away from the shots differently under each method", func(t *testing.T) {
		tin := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})
		idw := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{Method: SurfaceIDW})

		at := Point{4, 4, 0}

		byFacet, inside := tin.Elevation(at)
		require.True(t, inside)
		byDistance, inside := idw.Elevation(at)
		require.True(t, inside)

		assert.NotEqual(t, byFacet.Value(), byDistance.Value(),
			"two interpolations of one set of points are two different answers, which is why the method travels "+
				"with the surface")
	})
}

// TestSurfaceRecordsHowItWasDerived is the criterion the issue puts first: a
// surface handed on without its method is a grid of numbers nobody can check.
func TestSurfaceRecordsHowItWasDerived(t *testing.T) {
	testCases := []struct {
		name       string
		derivation SurfaceDerivation
		expected   []SurfaceParameter
	}{
		{
			name: "records the defaults it filled in",
			expected: []SurfaceParameter{
				{Name: "method", Value: "tin"},
				{Name: "tolerance", Value: closureTolerance},
				{Name: "position", Value: surfacePosition},
				{Name: "minimum-points", Value: "3"},
				{Name: "ambiguous", Value: "excluded"},
				{Name: "roughness", Value: "unstated"},
				{Name: "systematic", Value: "none"},
			},
		},
		{
			name:       "records the choice about shots it could not place",
			derivation: SurfaceDerivation{Ambiguous: true},
			expected: []SurfaceParameter{
				{Name: "method", Value: "tin"},
				{Name: "tolerance", Value: closureTolerance},
				{Name: "position", Value: surfacePosition},
				{Name: "minimum-points", Value: "3"},
				{Name: "ambiguous", Value: "included"},
				{Name: "roughness", Value: "unstated"},
				{Name: "systematic", Value: "none"},
			},
		},
		{
			name: "records what was said about the ground and the afternoons behind it",
			derivation: SurfaceDerivation{
				Roughness: 0.003,
				Systematic: []SessionSystematic{
					{Session: terraceSession, Magnitude: 0.010},
					{Session: "session:2026-05-07-am", Magnitude: 0.012},
				},
			},
			expected: []SurfaceParameter{
				{Name: "method", Value: "tin"},
				{Name: "tolerance", Value: closureTolerance},
				{Name: "position", Value: surfacePosition},
				{Name: "minimum-points", Value: "3"},
				{Name: "ambiguous", Value: "excluded"},
				{Name: "roughness", Value: "0.003"},
				{Name: "systematic", Value: "session:2026-05-06-am@0.01,session:2026-05-07-am@0.012"},
			},
		},
		{
			name:       "records the parameters the method it was asked for reads",
			derivation: SurfaceDerivation{Method: SurfaceIDW, Power: 3, Neighbours: 4},
			expected: []SurfaceParameter{
				{Name: "method", Value: "idw"},
				{Name: "tolerance", Value: closureTolerance},
				{Name: "position", Value: surfacePosition},
				{Name: "minimum-points", Value: "3"},
				{Name: "ambiguous", Value: "excluded"},
				{Name: "roughness", Value: "unstated"},
				{Name: "systematic", Value: "none"},
				{Name: "power", Value: "3.0"},
				{Name: "neighbours", Value: "4"},
			},
		},
	}

	graph := loadSurfaceFixture(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			surface := surfaceOf(t, graph, terraceRegion, testCase.derivation)

			assert.Equal(t, testCase.expected, surface.Parameters())
			assert.Equal(t, testCase.expected[0].Value, string(surface.Method()))
		})
	}
}

// TestSurfaceParametersDecideTheAnswer is why the parameters are recorded at
// all: change one and the surface changes.
func TestSurfaceParametersDecideTheAnswer(t *testing.T) {
	graph := loadSurfaceFixture(t)

	t.Run("weights fewer shots when fewer neighbours are asked for", func(t *testing.T) {
		all := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{Method: SurfaceIDW})
		three := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{Method: SurfaceIDW, Neighbours: 3})

		at := Point{7, 5, 0}

		wide, inside := all.Elevation(at)
		require.True(t, inside)
		narrow, inside := three.Elevation(at)
		require.True(t, inside)

		assert.Len(t, wide.From(), all.Len())
		assert.Len(t, narrow.From(), 3)
		assert.NotEqual(t, wide.Value(), narrow.Value())
	})

	t.Run("weights the near shots harder under a higher power", func(t *testing.T) {
		square := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{Method: SurfaceIDW})
		cube := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{Method: SurfaceIDW, Power: 3})

		at := Point{7, 5, 0}

		gentle, inside := square.Elevation(at)
		require.True(t, inside)
		steep, inside := cube.Elevation(at)
		require.True(t, inside)

		assert.NotEqual(t, gentle.Value(), steep.Value())
	})
}

// TestSurfaceRefusesTooFewPoints is the diagnostic the issue asks for by name: it
// has to say how many were found and how many it takes.
func TestSurfaceRefusesTooFewPoints(t *testing.T) {
	graph := loadSurfaceFixture(t)

	surface, diags := graph.SurfaceWithin(stripRegion, SurfaceDerivation{Against: surfaceAgainst(nil)})

	require.Len(t, diags, 1)
	assert.Equal(t, SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, "at least 3 distinct observation points")
	assert.Contains(t, diags[0].Message, "found 2 points")
	assert.Contains(t, diags[0].Message, string(stripRegion))
	assert.NotEmpty(t, diags[0].Span.Start.Path, "a diagnostic which cannot say where is a bug in the reporting")

	assert.False(t, surface.Derived(), "two points bound no area")
	assert.Equal(t, []string{"shot:0101", "shot:0102"}, resting(surface),
		"what was found comes back with the diagnostic, because it is the next thing to look at")

	_, inside := surface.Elevation(Point{50, 6, 0})
	assert.False(t, inside, "a surface which was not derived answers nowhere")
}

// TestSurfaceRefusesPointsOnOneLine is the same refusal for the corpus which has
// enough shots and still bounds nothing.
func TestSurfaceRefusesPointsOnOneLine(t *testing.T) {
	graph := loadSurfaceFixture(t)

	surface, diags := graph.SurfaceWithin(ridgeRegion, SurfaceDerivation{Against: surfaceAgainst(nil)})

	require.Len(t, diags, 1)
	assert.Equal(t, SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, "4 distinct observation points")
	assert.Contains(t, diags[0].Message, "lie on one line")

	assert.False(t, surface.Derived(), "points on a line say nothing about which way the ground falls off it")
	assert.Empty(t, surface.Hull())
	assert.Empty(t, surface.Facets())
}

// TestSurfaceRefusesAMethodItDoesNotImplement keeps the closed set closed. How a
// surface is interpolated is an algorithm this package holds, not registry data,
// so a name nothing implements is nobody's surface.
func TestSurfaceRefusesAMethodItDoesNotImplement(t *testing.T) {
	graph := loadSurfaceFixture(t)

	surface, diags := graph.SurfaceWithin(terraceRegion, SurfaceDerivation{
		Against: surfaceAgainst(nil),
		Method:  "kriging",
	})

	require.Len(t, diags, 1)
	assert.Equal(t, SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, "kriging")
	assert.Contains(t, diags[0].Message, "tin, idw")
	assert.False(t, surface.Derived())

	assert.True(t, SurfaceTIN.Known())
	assert.True(t, SurfaceIDW.Known())
	assert.False(t, SurfaceMethod("kriging").Known())
	assert.Equal(t, []SurfaceMethod{SurfaceTIN, SurfaceIDW}, SurfaceMethods())
}

// TestSurfaceMergesShotsOfOneMark is what stops a mark shot twice becoming a
// facet of no area, and what stops which of the two answers depending on the
// order the corpus was walked in.
func TestSurfaceMergesShotsOfOneMark(t *testing.T) {
	graph := loadSurfaceFixture(t)
	surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

	var merged SurfacePoint
	for _, point := range surface.Points() {
		if len(point.Coincident()) > 0 {
			merged = point
		}
	}

	require.Equal(t, ID("shot:0005"), merged.Observation(), "the better measured shot stands for the mark")
	assert.Equal(t, []ID{"shot:0008"}, merged.Coincident())
	assert.InDelta(t, 0.021, merged.Uncertainty(), 1e-12, "the level is the good shot's, not a mean of the two")

	assert.NotContains(t, resting(surface), "shot:0008", "a mark shot twice is one point")
	assert.Contains(t, surface.Observations(), ID("shot:0008"),
		"the shot which did not win is still evidence the surface was derived from")
}

// TestSurfaceTracesBackToItsShots is the provenance criterion: a level read off
// the surface can be followed back to the afternoon somebody stood there.
func TestSurfaceTracesBackToItsShots(t *testing.T) {
	graph := loadSurfaceFixture(t)
	surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

	t.Run("names every record it was derived from, sorted", func(t *testing.T) {
		expected := []ID{
			"shot:0001", "shot:0002", "shot:0003", "shot:0004",
			"shot:0005", "shot:0006", "shot:0007", "shot:0008",
		}

		assert.Equal(t, expected, surface.Observations())
	})

	t.Run("names the occupation behind each point", func(t *testing.T) {
		for _, point := range surface.Points() {
			assert.Equal(t, ID("session:2026-05-06-am"), point.Session(), "%s", point.Observation())
		}
	})

	t.Run("says which of them was carried in from another frame", func(t *testing.T) {
		var carried []ID
		for _, point := range surface.Points() {
			if point.Carried() {
				carried = append(carried, point.Observation())
			}
		}

		require.Equal(t, []ID{"shot:0007"}, carried)
	})

	t.Run("names the shots behind one level", func(t *testing.T) {
		elevation, inside := surface.Elevation(Point{4, 4, 0})
		require.True(t, inside)

		for _, id := range elevation.From() {
			assert.Contains(t, surface.Observations(), id, "a level comes from shots the surface rests on")
		}

		var total float64
		for _, weight := range elevation.Weights() {
			total += weight
		}
		assert.InDelta(t, 1.0, total, 1e-12, "the weights of an interpolation sum to one")
	})
}

// TestSurfaceCostsWhatTheShotsCost is the uncertainty of a level, which is
// propagated from the shots and from the transforms which carried them.
func TestSurfaceCostsWhatTheShotsCost(t *testing.T) {
	graph := loadSurfaceFixture(t)
	surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

	t.Run("charges a carried shot for the transform which carried it", func(t *testing.T) {
		var carried, own SurfacePoint
		for _, point := range surface.Points() {
			if point.Carried() {
				carried = point
				continue
			}
			if point.Observation() == "shot:0001" {
				own = point
			}
		}

		require.NotEmpty(t, carried.Observation())
		assert.Greater(t, carried.Uncertainty(), own.Uncertainty(),
			"a shot brought across a measured transform is known no better than the transform")
	})

	// The bound is the *worst* contributing shot and not the best. Combining
	// independent one-sigma figures under weights which sum to one gives
	// sqrt(sum (w·s)²), which is at most sqrt(sum w²)·max(s) and so never worse
	// than the worst shot behind it — but it is routinely worse than the best,
	// because a weight on a poorly measured shot carries that shot's doubt into
	// the answer. A level interpolated half from a shot known to a millimetre
	// and half from one known to a decimetre is not known to a millimetre.
	t.Run("is no worse than the worst shot behind it", func(t *testing.T) {
		sigmas := make(map[ID]float64, surface.Len())
		for _, point := range surface.Points() {
			sigmas[point.Observation()] = point.Uncertainty()
		}

		for x := 3.0; x <= 17.0; x += 1.0 {
			for y := 3.0; y <= 9.0; y += 1.0 {
				at := Point{x, y, 0}

				elevation, inside := surface.Elevation(at)
				require.True(t, inside, "at %v", at)

				worst := 0.0
				for _, id := range elevation.From() {
					sigma, behind := sigmas[id]
					require.True(t, behind, "%s is a shot the surface rests on", id)
					worst = math.Max(worst, sigma)
				}

				require.Positive(t, elevation.Uncertainty(), "at %v", at)
				require.LessOrEqual(t, elevation.Uncertainty(), worst, "at %v", at)
			}
		}
	})

	t.Run("is better than the worst where more than one shot carries weight", func(t *testing.T) {
		elevation, inside := surface.Elevation(Point{4, 4, 0})
		require.True(t, inside)

		var carrying int
		for _, weight := range elevation.Weights() {
			if weight > 0 {
				carrying++
			}
		}
		require.Greater(t, carrying, 1, "the point is between shots rather than on one")

		sigmas := make(map[ID]float64, surface.Len())
		for _, point := range surface.Points() {
			sigmas[point.Observation()] = point.Uncertainty()
		}

		worst := 0.0
		for _, id := range elevation.From() {
			worst = math.Max(worst, sigmas[id])
		}

		assert.Less(t, elevation.Uncertainty(), worst,
			"averaging independent shots buys something, which is why the weights are propagated rather than the "+
				"worst figure taken")
	})
}

// TestSurfaceIsDeterministic is the criterion which cannot be shown by one
// derivation: the same points and parameters give the same surface every time.
//
// The graph is loaded afresh each round rather than reused, because a map
// iteration order which leaked into the answer would be fixed for the lifetime
// of one load and would make a test which derived twice from one graph pass
// while the property failed.
func TestSurfaceIsDeterministic(t *testing.T) {
	t.Run("gives the same surface on every derivation", func(t *testing.T) {
		first := surfaceOf(t, loadSurfaceFixture(t), terraceRegion, SurfaceDerivation{})

		for round := 0; round < 8; round++ {
			again := surfaceOf(t, loadSurfaceFixture(t), terraceRegion, SurfaceDerivation{})

			require.Equal(t, first.Points(), again.Points())
			require.Equal(t, first.Facets(), again.Facets())
			require.Equal(t, first.Hull(), again.Hull())
			require.Equal(t, first.Observations(), again.Observations())
			require.Equal(t, first.Parameters(), again.Parameters())
			require.Equal(t, first.Digest(), again.Digest())
		}
	})

	t.Run("gives the same level on every query", func(t *testing.T) {
		surface := surfaceOf(t, loadSurfaceFixture(t), terraceRegion, SurfaceDerivation{Method: SurfaceIDW})

		at := Point{9, 7, 0}

		first, inside := surface.Elevation(at)
		require.True(t, inside)

		for round := 0; round < 8; round++ {
			again, inside := surface.Elevation(at)

			require.True(t, inside)
			require.Equal(t, first, again)
		}
	})
}

// TestSurfaceIsABuildOutput is the criterion which is about files rather than
// about values: a surface is derived, kept under the build output directory, and
// written into no source.
//
// It compares the bytes of the model rather than reloading it and comparing what
// it reads back. A test which did the latter would pass just as happily if a
// line had been rewritten into an equivalent one, and "never written into
// source" is a claim about the files.
func TestSurfaceIsABuildOutput(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.CopyFS(root, os.DirFS(filepath.Join("testdata", "surface", "terrace"))))

	before := treeContents(t, root)

	cache, err := OpenCache(CacheDir(root))
	require.NoError(t, err)

	graph, diags := LoadGraph(root)
	require.Empty(t, renderGraphDiagnostics(t, diags))

	surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{Against: Derivation{Cache: cache}})
	require.True(t, surface.Derived())

	t.Run("writes nothing into the model it was derived from", func(t *testing.T) {
		for path, content := range treeContents(t, root) {
			if strings.HasPrefix(path, BuildDir+"/") {
				continue
			}
			assert.Equal(t, before[path], content, "%s", path)
		}

		for path := range before {
			assert.Contains(t, treeContents(t, root), path, "%s was removed", path)
		}
	})

	t.Run("keeps what it derived under the build output directory", func(t *testing.T) {
		entry := filepath.Join(CacheDir(root), surface.Digest().String())

		info, err := os.Stat(entry)
		require.NoError(t, err)
		assert.True(t, info.IsDir(), "an entry is written under the digest it was derived from")

		assert.Equal(t, filepath.Join(root, BuildDir, "cache"), CacheDir(root))
	})

	t.Run("derives the same surface with the build output thrown away", func(t *testing.T) {
		require.NoError(t, os.RemoveAll(filepath.Join(root, BuildDir)))

		bare, diags := LoadGraph(root)
		require.Empty(t, renderGraphDiagnostics(t, diags))

		again := surfaceOf(t, bare, terraceRegion, SurfaceDerivation{})

		assert.Equal(t, surface.Points(), again.Points())
		assert.Equal(t, surface.Facets(), again.Facets())
		assert.Equal(t, surface.Hull(), again.Hull())
	})
}

// TestSurfaceParticipatesInTheDerivedCache is the last criterion: the result is
// keyed by the source digest, so a surface cannot be served for a tree it was
// not derived from.
func TestSurfaceParticipatesInTheDerivedCache(t *testing.T) {
	graph := loadSurfaceFixture(t)

	t.Run("serves the second derivation from the cache", func(t *testing.T) {
		cache, err := OpenCache(t.TempDir())
		require.NoError(t, err)

		first := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{Against: Derivation{Cache: cache}})
		stored := cache.Stats().Stores

		second := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{Against: Derivation{Cache: cache}})

		assert.Equal(t, 1, cache.Stats().Hits, "a surface already derived is not derived again")
		assert.Equal(t, stored, cache.Stats().Stores, "nor is it stored again")

		assert.Equal(t, first.Points(), second.Points())
		assert.Equal(t, first.Facets(), second.Facets())
		assert.Equal(t, first.Hull(), second.Hull())
		assert.Equal(t, first.Parameters(), second.Parameters())
		assert.Equal(t, first.Observations(), second.Observations())
		assert.Equal(t, first.Digest(), second.Digest())
		assert.Equal(t, first.Frame(), second.Frame())
		assert.Equal(t, first.Unit(), second.Unit())

		at := Point{7, 5, 0}
		want, inside := first.Elevation(at)
		require.True(t, inside)
		got, inside := second.Elevation(at)
		require.True(t, inside)
		assert.Equal(t, want, got, "a surface read back out of the cache answers identically")
	})

	// A cache which stored the combined figure per point would answer a fall as
	// though the base station behind both ends were two different base stations.
	// So what is stored is the independent part and the shared terms apart from
	// it, and this is what says they came back apart.
	t.Run("carries the terms a level is made of back out of the cache", func(t *testing.T) {
		cache, err := OpenCache(t.TempDir())
		require.NoError(t, err)

		derivation := SurfaceDerivation{
			Against:    Derivation{Cache: cache},
			Roughness:  0.003,
			Systematic: []SessionSystematic{{Session: terraceSession, Magnitude: 0.010}},
		}

		first := surfaceOf(t, graph, terraceRegion, derivation)
		second := surfaceOf(t, graph, terraceRegion, derivation)

		require.Equal(t, 1, cache.Stats().Hits, "the second derivation is the cached one")

		assert.Equal(t, first.Points(), second.Points())
		assert.Equal(t, first.Roughness(), second.Roughness())

		at := Point{7, 5, 0}

		want, inside := first.Elevation(at)
		require.True(t, inside)
		got, inside := second.Elevation(at)
		require.True(t, inside)

		assert.Equal(t, want, got)
		assert.True(t, got.Complete(), "the roughness came back with it")

		term, held := surfaceTermNamed(got.Budget(), string(terraceSession))
		require.True(t, held, "the afternoon behind the shots came back as a term of its own")
		assert.InDelta(t, 0.010, term.Magnitude, 1e-12)
	})

	t.Run("keys a surface by every parameter it was derived under", func(t *testing.T) {
		dir := t.TempDir()

		cache, err := OpenCache(dir)
		require.NoError(t, err)

		derivations := []SurfaceDerivation{
			{},
			{Ambiguous: true},
			{Method: SurfaceIDW},
			{Method: SurfaceIDW, Power: 3},
			{Method: SurfaceIDW, Neighbours: 4},
			{Roughness: 0.003},
			{Systematic: []SessionSystematic{{Session: terraceSession, Magnitude: 0.010}}},
		}

		var digest Digest
		for _, derivation := range derivations {
			derivation.Against = Derivation{Cache: cache}
			digest = surfaceOf(t, graph, terraceRegion, derivation).Digest()
		}

		entries, err := filepath.Glob(filepath.Join(dir, digest.String(), "surface-*"))
		require.NoError(t, err)

		assert.Len(t, entries, len(derivations),
			"a surface derived under different parameters is a different answer, and answers a different question "+
				"under a name of its own")
	})

	t.Run("keys a surface by the region it covers", func(t *testing.T) {
		cache, err := OpenCache(t.TempDir())
		require.NoError(t, err)

		key := func(subject ID) SurfaceKey {
			return SurfaceKey{
				Digest:     mustDigest(t, graph),
				Subject:    subject,
				Method:     SurfaceTIN,
				Parameters: surfaceParameterText(SurfaceDerivation{Against: surfaceAgainst(nil)}.Parameters()),
			}
		}

		surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{Against: Derivation{Cache: cache}})

		read, held := cache.LookupSurface(key(terraceRegion))
		require.True(t, held)
		assert.Equal(t, surface.Points(), read.Points())

		_, held = cache.LookupSurface(key(stripRegion))
		assert.False(t, held, "one region's surface is not another's")
	})

	t.Run("cannot serve a surface derived from another tree", func(t *testing.T) {
		cache, err := OpenCache(t.TempDir())
		require.NoError(t, err)

		surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{Against: Derivation{Cache: cache}})

		stale := SurfaceKey{
			Digest:     Digest{},
			Subject:    terraceRegion,
			Method:     SurfaceTIN,
			Parameters: surfaceParameterText(SurfaceDerivation{Against: surfaceAgainst(nil)}.Parameters()),
		}

		require.NoError(t, cache.StoreSurface(stale, surface), "a key with no digest stores nothing and says so")

		_, held := cache.LookupSurface(stale)
		assert.False(t, held, "nothing pins an entry under an unknown digest, so none is written")
	})

	t.Run("discards an entry which does not verify", func(t *testing.T) {
		dir := t.TempDir()

		cache, err := OpenCache(dir)
		require.NoError(t, err)

		surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{Against: Derivation{Cache: cache}})

		key := SurfaceKey{
			Digest:     surface.Digest(),
			Subject:    terraceRegion,
			Method:     SurfaceTIN,
			Parameters: surfaceParameterText(SurfaceDerivation{Against: surfaceAgainst(nil)}.Parameters()),
		}

		path := filepath.Join(dir, key.Digest.String(), key.entry())
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, append(content, '}'), 0o644))

		_, held := cache.LookupSurface(key)
		assert.False(t, held, "a corrupt build output is a recomputation, never a failed run")
		assert.Equal(t, 1, cache.Stats().Discards)

		_, err = os.Stat(path)
		assert.ErrorIs(t, err, fs.ErrNotExist, "what did not verify is thrown away")

		again := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{Against: Derivation{Cache: cache}})
		assert.Equal(t, surface.Points(), again.Points(), "and the answer is unchanged")
	})

	t.Run("does not cache a derivation which reported something", func(t *testing.T) {
		cache, err := OpenCache(t.TempDir())
		require.NoError(t, err)

		for round := 0; round < 2; round++ {
			_, diags := graph.SurfaceWithin(stripRegion, SurfaceDerivation{
				Against: surfaceAgainst(cache),
			})
			require.NotEmpty(t, diags, "the diagnostics of a run never depend on what a cache holds")
		}
	})

	t.Run("works against no cache at all", func(t *testing.T) {
		with := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})
		without := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

		assert.Equal(t, with.Points(), without.Points())
	})
}

// mustDigest is the digest of a graph, which every fixture loaded from disk has.
func mustDigest(t *testing.T, graph *Graph) Digest {
	t.Helper()

	digest, known := graph.Digest()
	require.True(t, known, "a graph loaded from a readable tree has a digest")

	return digest
}

// TestSurfaceKeyIsOneNamePerAnswer keeps the two kinds of entry apart. A surface
// and a set of footprints live under one digest, and a name collision between
// them would serve one for the other.
func TestSurfaceKeyIsOneNamePerAnswer(t *testing.T) {
	digest, err := DigestOf(filepath.Join("testdata", "surface", "terrace"))
	require.NoError(t, err)

	t.Run("gives one name per set of parameters", func(t *testing.T) {
		names := map[string]bool{}

		for _, key := range []SurfaceKey{
			{Digest: digest, Subject: terraceRegion, Method: SurfaceTIN, Parameters: "a=1 b=2"},
			{Digest: digest, Subject: terraceRegion, Method: SurfaceTIN, Parameters: "a=1 b=3"},
			{Digest: digest, Subject: terraceRegion, Method: SurfaceIDW, Parameters: "a=1 b=2"},
			{Digest: digest, Subject: stripRegion, Method: SurfaceTIN, Parameters: "a=1 b=2"},
			{Digest: digest, Subject: terraceRegion, Method: SurfaceTIN, Parameters: "a=12 b=2"},
			{Digest: digest, Subject: terraceRegion, Method: SurfaceTIN, Parameters: "a=1 b2=2"},
		} {
			require.False(t, names[key.entry()], "%s collides", key)
			names[key.entry()] = true
		}
	})

	t.Run("is told apart from a set of footprints under the same digest", func(t *testing.T) {
		surface := SurfaceKey{Digest: digest, Subject: terraceRegion, Method: SurfaceTIN, Parameters: "a=1"}
		prints := Key{Digest: digest, Tolerance: closureTolerance, Position: surfacePosition}

		assert.NotEqual(t, prints.entry(), surface.entry())
		assert.True(t, strings.HasPrefix(surface.entry(), "surface-"),
			"a build output directory somebody is reading by hand says what each entry is")
	})

	t.Run("reads as the question it answers", func(t *testing.T) {
		key := SurfaceKey{Digest: digest, Subject: terraceRegion, Method: SurfaceTIN, Parameters: "method=tin"}

		assert.Contains(t, key.String(), digest.String())
		assert.Contains(t, key.String(), string(terraceRegion))
		assert.Contains(t, key.String(), "method=tin")
	})
}

// TestSurfaceReadsBackAsItWasWritten is the round trip as a property: a surface
// encoded into an entry and decoded out of it is the surface which went in.
func TestSurfaceReadsBackAsItWasWritten(t *testing.T) {
	graph := loadSurfaceFixture(t)

	testCases := []struct {
		name       string
		derivation SurfaceDerivation
	}{
		{name: "a triangulation"},
		{name: "a triangulation reaching the boundary", derivation: SurfaceDerivation{Ambiguous: true}},
		{name: "a weighted mean", derivation: SurfaceDerivation{Method: SurfaceIDW, Power: 3, Neighbours: 4}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			surface := surfaceOf(t, graph, terraceRegion, testCase.derivation)

			key := SurfaceKey{
				Digest:     surface.Digest(),
				Subject:    surface.Subject(),
				Method:     surface.Method(),
				Parameters: surfaceParameterText(surface.Parameters()),
			}

			payload, err := encodeSurfaceEntry(key, surface)
			require.NoError(t, err)

			read, ok := decodeSurfaceEntry(payload, key)
			require.True(t, ok)

			assert.Equal(t, surface, read)

			for x := 3.0; x <= 17.0; x += 1.0 {
				for y := 3.0; y <= 9.0; y += 1.0 {
					at := Point{x, y, 0}

					want, inside := surface.Elevation(at)
					got, alsoInside := read.Elevation(at)

					require.Equal(t, inside, alsoInside, "at %v", at)
					require.Equal(t, want, got, "at %v", at)
				}
			}
		})
	}
}

// TestSurfaceEntryRefusesWhatWouldPanic is the check a build output has to pass
// before a run trusts an index it read out of a file.
func TestSurfaceEntryRefusesWhatWouldPanic(t *testing.T) {
	testCases := []struct {
		name  string
		entry surfaceEntry
	}{
		{
			name: "a facet naming a point it does not hold",
			entry: surfaceEntry{
				Points: []surfacePointRecord{{Observation: "shot:0001"}},
				Facets: []SurfaceFacet{{0, 1, 2}},
			},
		},
		{
			name: "a hull naming a point it does not hold",
			entry: surfaceEntry{
				Points: []surfacePointRecord{{Observation: "shot:0001"}},
				Hull:   []int{0, 4},
			},
		},
		{
			name: "a facet naming a point before the first",
			entry: surfaceEntry{
				Points: []surfacePointRecord{{Observation: "shot:0001"}},
				Facets: []SurfaceFacet{{0, 0, -1}},
			},
		},
		{
			name: "a point with no record behind it",
			entry: surfaceEntry{
				Points: []surfacePointRecord{{Observation: ""}},
			},
		},
	}

	key := SurfaceKey{Digest: Digest{}, Subject: terraceRegion, Method: SurfaceTIN, Parameters: ""}

	for _, testCase := range testCases {
		t.Run("discards "+testCase.name, func(t *testing.T) {
			testCase.entry.Version = cacheVersion
			testCase.entry.Subject = key.Subject
			testCase.entry.Method = key.Method

			payload, err := json.Marshal(testCase.entry)
			require.NoError(t, err)

			_, ok := decodeSurfaceEntry(payload, key)
			assert.False(t, ok, "an entry a run would fault on is not an answer")
		})
	}

	t.Run("discards an entry written by another version", func(t *testing.T) {
		payload, err := json.Marshal(surfaceEntry{
			Version: cacheVersion + 1,
			Subject: key.Subject,
			Method:  key.Method,
		})
		require.NoError(t, err)

		_, ok := decodeSurfaceEntry(payload, key)
		assert.False(t, ok)
	})

	t.Run("discards an entry written under other parameters", func(t *testing.T) {
		payload, err := json.Marshal(surfaceEntry{
			Version:    cacheVersion,
			Subject:    key.Subject,
			Method:     key.Method,
			Parameters: []SurfaceParameter{{Name: "method", Value: "tin"}},
		})
		require.NoError(t, err)

		_, ok := decodeSurfaceEntry(payload, key)
		assert.False(t, ok, "an entry whose parameters are not the ones asked for answers another question")
	})
}

// TestZeroSurfaceWorks is the contract every type in this package keeps: a
// caller which was handed nothing can still ask it anything.
func TestZeroSurfaceWorks(t *testing.T) {
	t.Run("answers every question about a surface which was never derived", func(t *testing.T) {
		var surface Surface

		assert.False(t, surface.Derived())
		assert.Zero(t, surface.Subject())
		assert.Zero(t, surface.Frame())
		assert.Zero(t, surface.Unit())
		assert.Zero(t, surface.Method())
		assert.Zero(t, surface.Len())
		assert.Empty(t, surface.Points())
		assert.Empty(t, surface.Facets())
		assert.Empty(t, surface.Hull())
		assert.Empty(t, surface.Parameters())
		assert.Empty(t, surface.Observations())
		assert.False(t, surface.Covers(Point{}))
		assert.Contains(t, surface.String(), "no surface")

		elevation, inside := surface.Elevation(Point{})
		assert.False(t, inside)
		assert.Zero(t, elevation.Value())
		assert.Zero(t, elevation.Uncertainty())
		assert.Empty(t, elevation.From())
		assert.Empty(t, elevation.Weights())
		assert.Contains(t, elevation.String(), "no elevation")
	})

	t.Run("answers about a point which came from no shot", func(t *testing.T) {
		var point SurfacePoint

		assert.Zero(t, point.Observation())
		assert.Zero(t, point.Session())
		assert.Zero(t, point.At())
		assert.Zero(t, point.Elevation())
		assert.Zero(t, point.Uncertainty())
		assert.False(t, point.Carried())
		assert.False(t, point.Ambiguous())
		assert.Empty(t, point.Coincident())
		assert.Contains(t, point.String(), "no point")
	})

	t.Run("derives nothing from no graph", func(t *testing.T) {
		var graph *Graph

		surface, diags := graph.SurfaceWithin(terraceRegion, SurfaceDerivation{})

		assert.Empty(t, diags)
		assert.False(t, surface.Derived())
	})
}

// TestSurfaceReadsAsASentence keeps the human rendering honest.
func TestSurfaceReadsAsASentence(t *testing.T) {
	graph := loadSurfaceFixture(t)

	t.Run("says what the surface is and what it rests on", func(t *testing.T) {
		surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

		written := surface.String()

		assert.Contains(t, written, string(terraceRegion))
		assert.Contains(t, written, "tin")
		assert.Contains(t, written, "7 points")
		assert.Contains(t, written, "facets")
	})

	t.Run("says what a level is and where it came from", func(t *testing.T) {
		surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

		elevation, inside := surface.Elevation(Point{4, 4, 0})
		require.True(t, inside)

		written := elevation.String()

		assert.Contains(t, written, "tin")
		assert.Contains(t, written, "shot:0001")
	})

	t.Run("says a parameter as name and value", func(t *testing.T) {
		assert.Equal(t, "power=2.0", SurfaceParameter{Name: "power", Value: "2.0"}.String())
	})

	t.Run("says a point as its record and where it is", func(t *testing.T) {
		surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

		assert.Contains(t, surface.Points()[0].String(), "shot:0001")
	})
}

// TestSurfaceGeometryIsCanonical is the arithmetic underneath the determinism
// criterion, exercised without a model behind it so that a failure says which
// half is wrong.
func TestSurfaceGeometryIsCanonical(t *testing.T) {
	t.Run("gives one hull whichever order the points arrive in", func(t *testing.T) {
		square := []vec{{0, 0}, {4, 0}, {4, 4}, {0, 4}, {2, 2}}

		hull := convexHull(square)
		require.Len(t, hull, 4, "the interior point is not on the hull")

		shuffled := []vec{{2, 2}, {0, 4}, {4, 4}, {4, 0}, {0, 0}}

		var one, other []vec
		for _, index := range hull {
			one = append(one, square[index])
		}
		for _, index := range convexHull(shuffled) {
			other = append(other, shuffled[index])
		}

		assert.ElementsMatch(t, one, other)
	})

	t.Run("leaves a collinear run with no hull", func(t *testing.T) {
		assert.Empty(t, convexHull([]vec{{0, 0}, {1, 0}, {2, 0}, {3, 0}}))
		assert.Empty(t, convexHull([]vec{{0, 0}, {1, 1}}))
	})

	t.Run("triangulates a square into two facets", func(t *testing.T) {
		facets := triangulate([]vec{{0, 0}, {4, 0}, {4, 4}, {0, 4}})

		assert.Len(t, facets, 2)

		covered := map[int]bool{}
		for _, facet := range facets {
			for _, index := range facet {
				covered[index] = true
			}
			assert.Equal(t, facet[0], min(facet[0], min(facet[1], facet[2])),
				"a facet is rotated to its lowest corner, so one triangle is one triple")
		}
		assert.Len(t, covered, 4, "every point is a corner of some facet")
	})

	t.Run("covers the hull of the points it triangulates", func(t *testing.T) {
		points := []vec{{0, 0}, {10, 0}, {10, 8}, {0, 8}, {4, 3}, {7, 6}, {2, 6}}

		facets := triangulate(points)
		require.NotEmpty(t, facets)

		var area float64
		for _, facet := range facets {
			area += cross(points[facet[0]], points[facet[1]], points[facet[2]]) / 2
		}

		assert.InDelta(t, 80.0, area, 1e-9,
			"the facets of a Delaunay triangulation tile its hull exactly, and each is counter-clockwise")
	})

	t.Run("triangulates nothing it cannot", func(t *testing.T) {
		assert.Empty(t, triangulate([]vec{{0, 0}, {1, 1}}))
		assert.Empty(t, triangulate([]vec{{1, 1}, {1, 1}, {1, 1}}))
	})
}

// The patio fixture is two pieces of ground of the same shape and the same
// survey density, picked up by two instruments. It is the gate: what a fall read
// off a derived surface is worth against a decision somebody has to make.
const (
	roverPatio    = ID("site:S-patio")
	levelledPatio = ID("site:S-patio-levelled")

	roverSession    = ID("session:2026-06-03-am")
	levelledSession = ID("session:2026-06-04-am")
)

// The shots of both patios fall on this plane: the ground drops one in eighty
// away from the house wall along y = 6, which is what the patio was specified at.
func patioLevel(y float64) float64 { return 100 + 0.0125*y }

// loadPatioFixture loads the patio fixture, failing the test where the load
// reports anything.
func loadPatioFixture(t *testing.T) *Graph {
	t.Helper()

	graph, diags := LoadGraph(filepath.Join("testdata", "surface", "patio"))
	require.NotNil(t, graph, "a load always yields a usable graph")
	require.Empty(t, renderGraphDiagnostics(t, diags), "the fixture loads clean")

	return graph
}

// allIndependent is every figure treated as an independent error.
//
// It is written out here because it is what several of the answers below must
// *not* equal: a budget which combined a shared term this way would understate a
// level and overstate a difference of two levels, and a test which asserted only
// the formula the code implements would pass either way.
func allIndependent(values ...float64) float64 {
	var squares float64
	for _, value := range values {
		squares += value * value
	}
	return math.Sqrt(squares)
}

// surfaceTermNamed is the budget term of that name, and whether the budget holds
// one.
func surfaceTermNamed(budget SurfaceBudget, name string) (SurfaceTerm, bool) {
	for _, term := range budget.Terms() {
		if term.Name == name {
			return term, true
		}
	}
	return SurfaceTerm{}, false
}

// weightOf is what one record counted for in an interpolation.
func weightOf(elevation Elevation, id ID) float64 {
	for i, from := range elevation.From() {
		if from == id {
			return elevation.Weights()[i]
		}
	}
	return 0
}

// TestSurfacePropagatesAccuracyThroughASurface is the first criterion of the
// gate: a level nobody can say how well they know is a level nobody can decide
// anything against.
func TestSurfacePropagatesAccuracyThroughASurface(t *testing.T) {
	graph := loadSurfaceFixture(t)

	t.Run("answers every level with an accuracy and the terms behind it", func(t *testing.T) {
		surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

		resting := map[ID]bool{}
		for _, point := range surface.Points() {
			resting[point.Observation()] = true
		}

		for x := 3.0; x <= 17.0; x += 2.0 {
			for y := 3.0; y <= 9.0; y += 2.0 {
				at := Point{x, y, 0}

				elevation, inside := surface.Elevation(at)
				require.True(t, inside, "at %v", at)
				require.Positive(t, elevation.Uncertainty(), "at %v", at)

				terms := elevation.Budget().Terms()
				require.NotEmpty(t, terms, "at %v", at)

				for _, term := range terms {
					for _, from := range term.From {
						assert.True(t, resting[from], "%s is a shot the surface rests on", from)
					}
				}
			}
		}
	})

	// Nothing is shared at (4, 4): the three shots behind it were all written in
	// the surface's own frame, so no transform was applied and no control point
	// is behind two of them at once.
	t.Run("combines the shots in quadrature where they share nothing", func(t *testing.T) {
		surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

		elevation, inside := surface.Elevation(Point{4, 4, 0})
		require.True(t, inside)

		independent := make([]float64, 0, len(elevation.From()))
		for i, from := range elevation.From() {
			for _, point := range surface.Points() {
				if point.Observation() == from {
					independent = append(independent, elevation.Weights()[i]*point.Independent())
				}
			}
		}
		require.Len(t, independent, len(elevation.From()))

		assert.InDelta(t, allIndependent(independent...), elevation.Uncertainty(), 1e-12)
		assert.Equal(t, UnitMetre, elevation.Budget().Unit(), "a figure with no unit on it means whatever the reader assumed")
		assert.Equal(t, 1.0, elevation.Budget().Combined().CoverageFactor, "storage is always one sigma")
	})

	t.Run("adds a term the whole afternoon shares linearly instead", func(t *testing.T) {
		const shared = 0.010

		surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{
			Systematic: []SessionSystematic{{Session: terraceSession, Magnitude: shared}},
		})
		plain := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

		at := Point{4, 4, 0}

		elevation, inside := surface.Elevation(at)
		require.True(t, inside)

		without, inside := plain.Elevation(at)
		require.True(t, inside)

		assert.InDelta(t, allIndependent(without.Uncertainty(), shared), elevation.Uncertainty(), 1e-12,
			"the shared term enters once at its full magnitude, whatever the weights")

		// The weights sum to one, so treating the three contributions as three
		// independent errors of a tenth of that magnitude each would divide the
		// shared term by the square root of three. That is the mistake the whole
		// arrangement exists to stop.
		naive := make([]float64, 0, len(elevation.Weights()))
		naive = append(naive, without.Uncertainty())
		for _, weight := range elevation.Weights() {
			naive = append(naive, weight*shared)
		}

		assert.Greater(t, elevation.Uncertainty(), allIndependent(naive...),
			"a systematic error does not partially cancel and does not average away")
	})

	t.Run("counts a shared term once however many shots carried it", func(t *testing.T) {
		const shared = 0.010

		surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{
			Systematic: []SessionSystematic{{Session: terraceSession, Magnitude: shared}},
		})

		elevation, inside := surface.Elevation(Point{4, 4, 0})
		require.True(t, inside)

		var systematic []SurfaceTerm
		for _, term := range elevation.Budget().Terms() {
			if term.Source == terraceSession {
				systematic = append(systematic, term)
			}
		}

		require.Len(t, systematic, 1, "one afternoon is one error, not one per shot")
		assert.Equal(t, TermSystematic, systematic[0].Kind)
		assert.True(t, systematic[0].Shared(), "every shot behind the level carried it")
		assert.ElementsMatch(t, elevation.From(), systematic[0].From)
		assert.InDelta(t, shared, systematic[0].Magnitude, 1e-12,
			"the weights sum to one, so the term arrives whole")
	})

	t.Run("keeps a transform's control apart from its own scatter", func(t *testing.T) {
		surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{})

		var carried SurfacePoint
		for _, point := range surface.Points() {
			if point.Carried() {
				carried = point
			}
		}
		require.Equal(t, ID("shot:0007"), carried.Observation())

		// The fit which carried it states 0.006 m of scatter and 0.008 m at
		// control:CP-3. The first is this shot's alone and combines in
		// quadrature with the instrument's 0.021 m; the second is shared with
		// everything else that transform ever carried, so it is kept as a term.
		assert.InDelta(t, allIndependent(0.021, 0.006), carried.Independent(), 1e-12)

		require.Len(t, carried.Systematic(), 1)
		assert.Equal(t, ID("control:CP-3"), carried.Systematic()[0].Source)
		assert.InDelta(t, 0.008, carried.Systematic()[0].Magnitude, 1e-12)

		assert.InDelta(t, allIndependent(0.021, 0.006, 0.008), carried.Uncertainty(), 1e-12,
			"one systematic term combines with the independent part the way a budget combines them")
	})

	t.Run("states the shared terms of a derivation in its parameters", func(t *testing.T) {
		surface := surfaceOf(t, graph, terraceRegion, SurfaceDerivation{
			Systematic: []SessionSystematic{
				{Session: terraceSession, Magnitude: 0.004},
				{Session: terraceSession, Magnitude: 0.010},
				{Session: "session:nothing-stated", Magnitude: 0},
			},
		})

		parameter, held := surfaceParameter(surface, "systematic")
		require.True(t, held)
		assert.Equal(t, "session:2026-05-06-am@0.01", parameter,
			"a session stated twice is the wider of the two, and one stated with nothing is not stated")
	})
}

// surfaceParameter is what one parameter of a derived surface was, and whether
// it was recorded at all.
func surfaceParameter(surface Surface, name string) (string, bool) {
	for _, parameter := range surface.Parameters() {
		if parameter.Name == name {
			return parameter.Value, true
		}
	}
	return "", false
}

// TestSurfaceAccuracyDegradesWithDistance is the stated rule for the ground
// between the shots: the interpolation is charged the stated roughness per unit
// of distance from the nearest of them, and nothing at a shot.
func TestSurfaceAccuracyDegradesWithDistance(t *testing.T) {
	const roughness = 0.003

	graph := loadPatioFixture(t)

	t.Run("says the ground between the shots is not in the figure unless somebody states it", func(t *testing.T) {
		surface := surfaceOf(t, graph, roverPatio, SurfaceDerivation{})

		elevation, inside := surface.Elevation(Point{2.75, 3.625, 0})
		require.True(t, inside)

		assert.False(t, elevation.Complete(), "nothing was said about how far the ground departs from a plane")
		assert.False(t, elevation.Budget().Complete())

		_, held := surfaceTermNamed(elevation.Budget(), interpolationTerm)
		assert.False(t, held, "a term nobody stated is not a term of nought")

		assert.Positive(t, elevation.Nearest(), "how far from the evidence it is, is still answerable")
	})

	t.Run("charges the stated roughness per unit of distance from the nearest shot", func(t *testing.T) {
		testCases := []struct {
			name    string
			at      Point
			nearest float64
		}{
			{name: "nothing at a shot", at: Point{2, 3, 0}, nearest: 0},
			{name: "a quarter of a metre from one", at: Point{2, 3.25, 0}, nearest: 0.25},
			{name: "midway between two columns", at: Point{2.75, 3, 0}, nearest: 0.75},
			{name: "in the middle of a cell", at: Point{2.75, 3.625, 0}, nearest: math.Hypot(0.75, 0.625)},
		}

		surface := surfaceOf(t, graph, roverPatio, SurfaceDerivation{Roughness: roughness})

		for _, testCase := range testCases {
			t.Run(testCase.name, func(t *testing.T) {
				elevation, inside := surface.Elevation(testCase.at)
				require.True(t, inside)

				assert.True(t, elevation.Complete())
				assert.InDelta(t, testCase.nearest, elevation.Nearest(), 1e-12)

				term, held := surfaceTermNamed(elevation.Budget(), interpolationTerm)
				if testCase.nearest == 0 {
					assert.False(t, held, "a level asked for at a shot is that shot's level")
					return
				}

				require.True(t, held)
				assert.Equal(t, TermIndependent, term.Kind)
				assert.InDelta(t, roughness*testCase.nearest, term.Magnitude, 1e-12)
			})
		}
	})

	t.Run("is worse the further the question is asked from the evidence", func(t *testing.T) {
		surface := surfaceOf(t, graph, roverPatio, SurfaceDerivation{Roughness: roughness})

		var last float64
		for _, at := range []Point{{2, 3, 0}, {2, 3.125, 0}, {2, 3.25, 0}, {2.5, 3.375, 0}, {2.75, 3.625, 0}} {
			elevation, inside := surface.Elevation(at)
			require.True(t, inside, "at %v", at)

			assert.GreaterOrEqual(t, elevation.Nearest(), last, "at %v", at)
			last = elevation.Nearest()
		}
	})

	t.Run("adds it to the shots rather than instead of them", func(t *testing.T) {
		at := Point{2.75, 3.625, 0}

		with := surfaceOf(t, graph, roverPatio, SurfaceDerivation{Roughness: roughness})
		without := surfaceOf(t, graph, roverPatio, SurfaceDerivation{})

		stated, inside := with.Elevation(at)
		require.True(t, inside)

		bare, inside := without.Elevation(at)
		require.True(t, inside)

		assert.InDelta(t, bare.Value(), stated.Value(), 1e-12, "how well the ground is known is not where it is")
		assert.InDelta(t,
			allIndependent(bare.Uncertainty(), roughness*stated.Nearest()),
			stated.Uncertainty(), 1e-12,
		)
	})
}

// TestSurfaceFallCarriesItsOwnBudget is the criterion the whole arrangement is
// for: a difference of two levels is not two levels combined, because what the
// two share is in both of them by the same amount and so is not in the
// difference at all.
func TestSurfaceFallCarriesItsOwnBudget(t *testing.T) {
	const (
		roughness = 0.003
		shared    = 0.010
	)

	graph := loadPatioFixture(t)

	derivation := SurfaceDerivation{
		Roughness:  roughness,
		Systematic: []SessionSystematic{{Session: roverSession, Magnitude: shared}},
	}

	surface := surfaceOf(t, graph, roverPatio, derivation)

	threshold, drain := Point{3.5, 5, 0}, Point{3.5, 1, 0}

	t.Run("answers the drop, the run and the grade", func(t *testing.T) {
		fall, ok := surface.Fall(threshold, drain)
		require.True(t, ok)

		assert.InDelta(t, patioLevel(5)-patioLevel(1), fall.Value(), 1e-9, "fifty millimetres over four metres")
		assert.InDelta(t, 4.0, fall.Run(), 1e-12)
		assert.InDelta(t, 1.0/80.0, fall.Gradient(), 1e-9)
		assert.Contains(t, fall.String(), "1 in 80.0")
	})

	t.Run("reads the other way round as a rise", func(t *testing.T) {
		fall, ok := surface.Fall(drain, threshold)
		require.True(t, ok)

		assert.InDelta(t, patioLevel(1)-patioLevel(5), fall.Value(), 1e-9)
		assert.Negative(t, fall.Gradient())
		assert.Contains(t, fall.String(), "rises")
	})

	t.Run("cancels the term the whole afternoon shares", func(t *testing.T) {
		fall, ok := surface.Fall(threshold, drain)
		require.True(t, ok)

		term, held := surfaceTermNamed(fall.Budget(), string(roverSession))
		require.True(t, held, "the term is reported rather than dropped: that it cancels is the answer")
		assert.Equal(t, TermSystematic, term.Kind)
		assert.True(t, term.Shared())
		assert.Zero(t, term.Magnitude)
	})

	// The two ends rest on six different shots, so the independent part of the
	// difference is larger than either level's. What cancels is the shared part —
	// and where the shared part is what a level is mostly made of, which is the
	// usual state of a survey run off one benchmark, the difference is known
	// several times better than either end of it.
	t.Run("answers a fall a level nobody would decide against is buried in", func(t *testing.T) {
		buried := surfaceOf(t, graph, roverPatio, SurfaceDerivation{
			Roughness:  roughness,
			Systematic: []SessionSystematic{{Session: roverSession, Magnitude: 0.030}},
		})

		fall, ok := buried.Fall(threshold, drain)
		require.True(t, ok)

		require.Greater(t, fall.From().Uncertainty(), 0.030, "the benchmark is most of what a level is worth")

		assert.Less(t, fall.Uncertainty(), fall.From().Uncertainty(),
			"the difference is known better than either level it is the difference of")
		assert.Less(t, fall.Uncertainty(), fall.To().Uncertainty())

		unburied, ok := surface.Fall(threshold, drain)
		require.True(t, ok)
		assert.InDelta(t, unburied.Uncertainty(), fall.Uncertainty(), 1e-12,
			"a term which cancels changes the fall by nothing at all, whatever it is worth")
	})

	t.Run("is not the two levels combined in quadrature", func(t *testing.T) {
		fall, ok := surface.Fall(threshold, drain)
		require.True(t, ok)

		naive := allIndependent(fall.From().Uncertainty(), fall.To().Uncertainty())

		assert.Less(t, fall.Uncertainty(), naive,
			"combining two levels off one surface counts everything they share twice")
	})

	t.Run("partially cancels a shot which backs both ends", func(t *testing.T) {
		near, far := Point{2.2, 3.2, 0}, Point{2.4, 3.4, 0}

		fall, ok := surface.Fall(near, far)
		require.True(t, ok)

		var both []ID
		for _, from := range fall.From().From() {
			if slices.Contains(fall.To().From(), from) {
				both = append(both, from)
			}
		}
		require.NotEmpty(t, both, "the two ends are close enough to share a facet")

		for _, id := range both {
			term, held := surfaceTermNamed(fall.Budget(), string(id))
			require.True(t, held, "%s", id)

			difference := math.Abs(weightOf(fall.To(), id) - weightOf(fall.From(), id))
			assert.InDelta(t, difference*0.021, term.Magnitude, 1e-12,
				"a shot behind both ends enters once, at the difference of its two weights")
			assert.Equal(t, []ID{id}, term.From, "one shot is one term however many ends it backs")
		}
	})

	t.Run("charges the ground between the shots at each end separately", func(t *testing.T) {
		fall, ok := surface.Fall(threshold, drain)
		require.True(t, ok)

		var interpolation []SurfaceTerm
		for _, term := range fall.Budget().Terms() {
			if term.Name == interpolationTerm {
				interpolation = append(interpolation, term)
			}
		}

		require.Len(t, interpolation, 2,
			"how far the ground departs from the interpolation here says nothing about four metres away")
		for _, term := range interpolation {
			assert.Equal(t, TermIndependent, term.Kind)
			assert.InDelta(t, roughness*0.5, term.Magnitude, 1e-12)
		}
	})

	t.Run("refuses a fall with an end beyond the surveyed ground", func(t *testing.T) {
		_, ok := surface.Fall(threshold, Point{30, 1, 0})
		assert.False(t, ok, "there is no measurement out there")

		_, ok = surface.Fall(Point{-30, 1, 0}, drain)
		assert.False(t, ok)

		_, ok = Surface{}.Fall(threshold, drain)
		assert.False(t, ok)
	})

	t.Run("decides nothing against a budget which leaves the ground out", func(t *testing.T) {
		bare := surfaceOf(t, graph, roverPatio, SurfaceDerivation{
			Systematic: derivation.Systematic,
		})

		fall, ok := bare.Fall(threshold, drain)
		require.True(t, ok)

		require.Less(t, fall.Uncertainty(), 0.030, "the figure is well inside a requirement of thirty millimetres")
		assert.False(t, fall.Decides(0.030), "a floor which passes a requirement passes it by leaving something out")

		stated, ok := surface.Fall(threshold, drain)
		require.True(t, ok)
		assert.True(t, stated.Decides(0.030))
		assert.False(t, stated.Decides(0), "a requirement which is not a figure decides nothing")
	})

	t.Run("says nothing about a pair of points nobody can be answered for", func(t *testing.T) {
		assert.Equal(t, "no fall", Fall{}.String())
		assert.Equal(t, "no fall", Fall{}.Report())
		assert.Zero(t, Fall{}.Gradient())
		assert.Zero(t, Fall{}.Uncertainty())
	})
}

// The decision the patio fixture was surveyed for, and what it requires of the
// answer.
//
// **Every figure here was settled before a surface was derived.** The fixture's
// registry says so, and [docs/surface-accuracy-gate.md] states the reasoning: a
// patio which does not fall away from the house does not drain, one which falls
// too steeply is unpleasant to stand on, and the two limits are fifty
// millimetres apart over the run the decision is made across. A requirement
// settled afterwards is not a requirement — it is a description of whatever the
// survey happened to achieve.
const (
	// patioGrade is the fall the patio was specified at: one in eighty away
	// from the house, which over the run below is fifty millimetres.
	patioGrade = 1.0 / 80.0

	// patioRun is how far the decision is made over: threshold to drain edge.
	patioRun = 4.0

	// patioRequired is what the fall has to be known to, as a standard
	// uncertainty in metres. Five millimetres at one sigma is ten at k = 2,
	// which is a fifth of the gap between the two grades the decision is
	// between — small enough that the decision turns on the patio rather than
	// on the survey.
	patioRequired = 0.005

	// patioCoverage is what the requirement is restated at when it is reported
	// to somebody who has to act on it.
	patioCoverage = 2.0

	// patioRoughness is how far a laid patio was stated to depart from a plane,
	// one sigma per metre of distance from the nearest shot.
	patioRoughness = 0.003

	// patioSpacing is the grid both patios were surveyed on.
	patioSpacingX = 1.5
	patioSpacingY = 1.25
)

// patioThreshold and patioDrain are the two points the decision is made between
// on the rover patio; the levelled one is the same ground ten metres east.
var (
	patioThreshold = Point{3.5, 5, 0}
	patioDrain     = Point{3.5, 1, 0}
)

// patioSurvey is one of the two surveys of the same ground.
type patioSurvey struct {
	subject    ID
	instrument string
	precision  float64
	session    ID
	systematic float64
	east       float64
}

// surfaceGolden returns the record held in testdata/surface/patio/name, having
// first rewritten it from got when -update was passed.
func surfaceGolden(t *testing.T, name string, got string) string {
	t.Helper()

	path := filepath.Join("testdata", "surface", "patio", name)
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestPatioFallGate is the gate: the surface put to a decision with a stated
// accuracy requirement, and the achieved accuracy measured against it.
//
// The mechanism being right does not make the answer usable, and this is where
// that is found out. What comes back is recorded in the repository as
// testdata/surface/patio/decision.txt, so the finding is a file somebody can
// read and a diff somebody has to explain rather than a number in a transcript.
func TestPatioFallGate(t *testing.T) {
	surveys := []patioSurvey{
		{
			subject:    roverPatio,
			instrument: "gnss-rtk",
			precision:  0.021,
			session:    roverSession,
			systematic: 0.010,
		},
		{
			subject:    levelledPatio,
			instrument: "levelling",
			precision:  0.002,
			session:    levelledSession,
			systematic: 0.003,
			east:       10,
		},
	}

	graph := loadPatioFixture(t)

	measured := make(map[ID]Fall, len(surveys))

	var out strings.Builder

	requirement := Uncertainty{Magnitude: patioRequired, Unit: UnitMetre, CoverageFactor: 1}
	widened, err := requirement.Widen(patioCoverage)
	require.NoError(t, err)

	out.WriteString("The decision, stated before any surface was derived\n")
	fmt.Fprintf(&out, "  a patio must fall at least %s away from the house or it does not drain\n",
		gradeText(patioGrade))
	fmt.Fprintf(&out, "  the fall is decided over %.3f m of run, threshold to drain edge\n", patioRun)
	fmt.Fprintf(&out, "  it must be known to %s, which is %s\n", figure(requirement), figure(widened))
	fmt.Fprintf(&out, "  the ground departs from a laid plane by %.4f m per metre from the nearest shot\n",
		patioRoughness)

	for _, survey := range surveys {
		surface := surfaceOf(t, graph, survey.subject, SurfaceDerivation{
			Roughness:  patioRoughness,
			Systematic: []SessionSystematic{{Session: survey.session, Magnitude: survey.systematic}},
		})

		east := func(at Point) Point { return Point{at[0] + survey.east, at[1], at[2]} }

		fall, ok := surface.Fall(east(patioThreshold), east(patioDrain))
		require.True(t, ok, "%s", survey.subject)

		measured[survey.subject] = fall

		// The shots on their own: the answer a survey of infinite density would
		// give with this instrument, which is what says whether density is the
		// constraint at all.
		var shots []float64
		for _, term := range fall.Budget().Terms() {
			if term.Name != interpolationTerm {
				shots = append(shots, term.Magnitude)
			}
		}
		alone := allIndependent(shots...)

		widenedFall, err := fall.Budget().Combined().Widen(patioCoverage)
		require.NoError(t, err)

		fmt.Fprintf(&out, "\n%s — %s, %s on a %.3f m by %.3f m grid, vertical precision %.3f m\n",
			survey.subject, survey.instrument, plural(surface.Len(), "shot"),
			patioSpacingX, patioSpacingY, survey.precision)

		fmt.Fprintf(&out, "  the fall is %.4f m over %.3f m of run, which is %s\n",
			fall.Value(), fall.Run(), gradeText(fall.Gradient()))
		fmt.Fprintf(&out, "  known to %s, which is %s\n", figure(fall.Budget().Combined()), figure(widenedFall))
		fmt.Fprintf(&out, "  each end on its own is worth %.4f m\n", fall.From().Uncertainty())

		for _, term := range fall.Budget().Terms() {
			line := fmt.Sprintf("    %-48s %.4f m %s", termText(term), term.Magnitude, remarkText(term))
			fmt.Fprintf(&out, "%s\n", strings.TrimRight(line, " "))
		}

		fmt.Fprintf(&out, "  achieved %.4f m against a requirement of %.4f m, %.1f times it\n",
			fall.Uncertainty(), patioRequired, fall.Uncertainty()/patioRequired)
		fmt.Fprintf(&out, "  verdict: %s\n", decidesText(fall.Decides(patioRequired)))
		fmt.Fprintf(&out, "  the shots alone are worth %.4f m, so a denser survey %s\n",
			alone, reachText(alone <= patioRequired))

		if alone <= patioRequired {
			// How far from a shot the question may be asked before the ground
			// between them uses up what the shots left of the budget, and the
			// grid spacing which keeps every question inside that.
			room := math.Sqrt((patioRequired*patioRequired - alone*alone) / 2)
			reach := room / patioRoughness

			fmt.Fprintf(&out, "  it holds out to %.3f m from the nearest shot, a grid of %.3f m\n",
				reach, reach*math.Sqrt2)

			worst := math.Hypot(patioSpacingX/2, patioSpacingY/2)
			assert.Less(t, worst, reach,
				"the grid surveyed keeps every point of the patio inside the reach the budget allows")
		}
	}

	assert.Equal(t, surfaceGolden(t, "decision.txt", out.String()), out.String(),
		"the outcome of the gate is recorded in the repository, and a change to it is a diff to explain")

	rover, levelled := measured[roverPatio], measured[levelledPatio]

	t.Run("misses the requirement at the density and instrument surveyed", func(t *testing.T) {
		assert.False(t, rover.Decides(patioRequired))
		assert.Greater(t, rover.Uncertainty(), patioRequired)
	})

	t.Run("misses it because of the instrument and not the density", func(t *testing.T) {
		term, held := rover.Budget().Dominant()
		require.True(t, held)

		assert.Equal(t, TermIndependent, term.Kind)
		assert.NotEqual(t, interpolationTerm, term.Name,
			"the ground between the shots is a millimetre and a half of a twenty-one millimetre budget")

		var shots []float64
		for _, term := range rover.Budget().Terms() {
			if term.Name != interpolationTerm {
				shots = append(shots, term.Magnitude)
			}
		}

		assert.Greater(t, allIndependent(shots...), patioRequired,
			"the shots alone miss it, so no density of them would meet it")
	})

	t.Run("meets it on the same ground at the same density, levelled", func(t *testing.T) {
		assert.True(t, levelled.Decides(patioRequired))
		assert.Less(t, levelled.Uncertainty(), patioRequired)

		term, held := levelled.Budget().Dominant()
		require.True(t, held)
		assert.Equal(t, interpolationTerm, term.Name,
			"with an instrument this good the ground between the shots is what the budget is made of, "+
				"which is where density starts to be the question")
	})

	t.Run("answers the same fall either way", func(t *testing.T) {
		assert.InDelta(t, rover.Value(), levelled.Value(), 1e-9,
			"the two patios are the same ground: what differs between the answers is what they are worth")
		assert.InDelta(t, patioGrade*patioRun, rover.Value(), 1e-9)
	})
}

// figure writes an uncertainty for the record: rounded to a tenth of a
// millimetre, with the coverage factor it is stated at.
//
// It is rounded and [Uncertainty.String] is not, on purpose. This is a record
// checked into the repository and compared byte for byte, and the last bits of a
// float are where a compiler is free to fuse a multiply and an add on one
// architecture and not on another. The figures the tests assert on are the full
// ones; what is written down is the figure somebody acts on.
func figure(uncertainty Uncertainty) string {
	written := fmt.Sprintf("%.4f %s (k = %.0f", uncertainty.Magnitude, uncertainty.Unit, uncertainty.CoverageFactor)
	if confidence, spelled := uncertainty.Confidence(); spelled {
		written += ", " + confidence
	}
	return written + ")"
}

// termText is one budget term named for the record, without its magnitude, which
// is written beside it to a fixed width.
func termText(term SurfaceTerm) string {
	name := term.Name
	if name == interpolationTerm && len(term.From) > 0 {
		name += " near " + string(term.From[0])
	}
	return string(term.Kind) + " " + name
}

// remarkText is what a term of nought has to say for itself.
func remarkText(term SurfaceTerm) string {
	switch {
	case term.Magnitude != 0:
		return ""
	case term.Kind == TermSystematic:
		return "(cancels: it is in both ends by the same amount)"
	default:
		return "(carries no weight here)"
	}
}

// decidesText writes a verdict about a requirement the way the record reads it.
func decidesText(decides bool) string {
	if decides {
		return "DECIDES"
	}
	return "DOES NOT DECIDE"
}

// reachText writes whether taking more shots is worth anything.
func reachText(reaches bool) string {
	if reaches {
		return "reaches it"
	}
	return "cannot reach it"
}
