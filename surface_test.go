// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"encoding/json"
	"io/fs"
	"math"
	"os"
	"path/filepath"
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
