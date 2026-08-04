// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// frameRoundTripTolerance is how far a point transformed into another frame and
// back may land from where it started, in the unit it started in.
//
// It is stated rather than assumed, and it is a property of the arithmetic
// rather than of the model: a route through two frames applies a matrix inverse
// and two divisions, and each of them rounds. A metre-scale coordinate carries
// about fifteen significant figures, so a nanometre is several orders of
// magnitude larger than the rounding and several smaller than anything anybody
// surveys — which makes a failure here a mistake in the composition rather than
// a change in the last bit.
const frameRoundTripTolerance = 1e-9

// frameFixture is the root of one fixture model: a registry declaring frames,
// and the claims which measure them against their parents.
func frameFixture(name string) string { return filepath.Join("testdata", "frame", name) }

// resolveFrames resolves the frames of a fixture and renders the diagnostics of
// the pass the way the command line interface would.
//
// The registry's and the claims' own diagnostics are asserted empty rather than
// rendered: every fixture here declares a registry and claims which load clean,
// so what the golden beside it holds is what this pass had to say and nothing
// else.
func resolveFrames(t *testing.T, root string) (*Frames, string) {
	t.Helper()

	registry, registryDiags := LoadRegistry(root)
	require.Empty(t, registryDiags, "the fixture registry loads clean")

	claims, claimDiags := LoadClaims(root, registry)
	require.Empty(t, claimDiags, "the fixture's claims load clean")

	frames, diags := ResolveFrames(registry, claims)

	var collected Diagnostics
	collected.Add(diags...)

	var rendered strings.Builder
	require.NoError(t, collected.Render(&rendered, FileSources{}))

	return frames, rendered.String()
}

// resolveFramesRegardless is [resolveFrames] against a model which does not load
// clean, which is what the error paths need: a cycle in the parent chain, a
// second root frame and a unit which is no linear unit are each a load error,
// and each is also a question a caller may still ask the graph afterwards.
func resolveFramesRegardless(t *testing.T, root string) *Frames {
	t.Helper()

	registry, _ := LoadRegistry(root)
	claims, _ := LoadClaims(root, registry)

	frames, _ := ResolveFrames(registry, claims)

	return frames
}

// expectedFrameDiagnostics returns the rendering held beside the fixture, having
// first rewritten it from got when -update was passed.
func expectedFrameDiagnostics(t *testing.T, name string, got string) string {
	t.Helper()

	path := filepath.Join(frameFixture(name), "diagnostics.txt")
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// assertPoint compares two positions component by component, within a stated
// tolerance.
//
// The components are compared in order and never sorted: they are the axes of
// the frame the position is expressed in, in the order that frame gives them.
func assertPoint(t *testing.T, want, got Point, tolerance float64) {
	t.Helper()

	for axis := range want {
		assert.InDelta(t, want[axis], got[axis], tolerance, "component %d of %v", axis, got)
	}
}

func TestResolveFrames(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
	}{
		{
			name:    "names both ends when a frame's transform names a claim which is not a transform",
			fixture: "not-a-transform",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, got := resolveFrames(t, frameFixture(testCase.fixture))

			assert.Equal(t, expectedFrameDiagnostics(t, testCase.fixture, got), got)
		})
	}
}

// TestFramesReadTheChainAndTheMeasurement is its own function because it is
// about what the graph holds rather than about a diagnostic: the chain a frame
// reaches the root through, and the evidence for each step of it.
//
// The measurement is the point of the arrangement. A georeference is a fit, with
// a residual, produced by a method on a date, and reading it back as a claim is
// what lets a cross-frame answer say how well the relationship it used is known.
func TestFramesReadTheChainAndTheMeasurement(t *testing.T) {
	frames, rendered := resolveFrames(t, frameFixture("valid"))
	require.Empty(t, rendered, "the valid fixture resolves clean")

	t.Run("gives the root frame every chain is rooted at", func(t *testing.T) {
		root, ok := frames.Root()

		require.True(t, ok)
		assert.Equal(t, ID("frame:survey-grid"), root.ID)
		assert.Equal(t, UnitMetre, root.Unit)
	})

	t.Run("walks the chain from a frame to the root, beginning with the frame", func(t *testing.T) {
		var chain []ID
		for frame := range frames.Chain("frame:building") {
			chain = append(chain, frame.ID)
		}

		assert.Equal(t, []ID{"frame:building", "frame:site", "frame:survey-grid"}, chain)
	})

	t.Run("gives the root itself as a chain of one", func(t *testing.T) {
		var chain []ID
		for frame := range frames.Chain("frame:survey-grid") {
			chain = append(chain, frame.ID)
		}

		assert.Equal(t, []ID{"frame:survey-grid"}, chain)
	})

	t.Run("walks nothing for a frame no registry file declares", func(t *testing.T) {
		assert.Empty(t, slices.Collect(frames.Chain("frame:nowhere")))
	})

	t.Run("gives the claim which measured a frame against its parent", func(t *testing.T) {
		claim, ok := frames.Measurement("frame:site")
		require.True(t, ok)

		id, written := claim.ID()
		require.True(t, written)
		assert.Equal(t, ID("survey:C-0001"), id)

		assert.Equal(t, "Georeferencing report GR-2026-002, Acme Surveys", claim.Source())
		assert.Equal(t, ID("method:gnss-static"), claim.Method())
		assert.Equal(t, "2026-02-11", claim.Date().Format(dateLayout))

		accuracy, rankable := claim.Accuracy()
		require.True(t, rankable)
		assert.Equal(t, []AccuracyTerm{
			{Kind: TermIndependent, Magnitude: 0.012, Unit: UnitMetre, Span: accuracy.Terms[0].Span},
			{Kind: TermSystematic, Magnitude: 0.008, Unit: UnitMetre, Source: "survey:CP-3", Span: accuracy.Terms[1].Span},
		}, accuracy.Terms)
	})

	t.Run("gives the transform the claim carries", func(t *testing.T) {
		transform, ok := frames.Transform("frame:site")

		require.True(t, ok)
		assert.Equal(t, [3]float64{100.0, 200.0, 0.0}, transform.Translation)
		assert.Equal(t, 1.0, transform.Scale)
	})

	t.Run("gives no measurement for the root, which is what makes it the root", func(t *testing.T) {
		_, ok := frames.Measurement("frame:survey-grid")

		assert.False(t, ok)
	})
}

// TestFramesComposeAlongTheChain is its own function because the assertion is a
// position rather than a diagnostic: what one point is, read from each of the
// frames the model declares.
//
// A shape lives in exactly one frame and is transformed on demand, which is
// exactly this. The building is in millimetres and everything above it in
// metres, and the conversion between them is applied by the frames' own declared
// units rather than by anything written on the point.
func TestFramesComposeAlongTheChain(t *testing.T) {
	frames, rendered := resolveFrames(t, frameFixture("valid"))
	require.Empty(t, rendered, "the valid fixture resolves clean")

	testCases := []struct {
		name     string
		point    Point
		from     ID
		to       ID
		expected Point
	}{
		{
			name:     "converts the unit and applies the transform one level up",
			point:    Point{3000.0, 4000.0, 0.0},
			from:     "frame:building",
			to:       "frame:site",
			expected: Point{13.0, 24.0, 0.0},
		},
		{
			name:     "composes both transforms two levels up",
			point:    Point{3000.0, 4000.0, 0.0},
			from:     "frame:building",
			to:       "frame:survey-grid",
			expected: Point{113.0, 224.0, 0.0},
		},
		{
			name:     "runs the same chain backwards, into the unit of the frame it arrives in",
			point:    Point{113.0, 224.0, 0.0},
			from:     "frame:survey-grid",
			to:       "frame:building",
			expected: Point{3000.0, 4000.0, 0.0},
		},
		{
			name:     "composes three transforms three levels up",
			point:    Point{2000.0, 3500.0, 0.0},
			from:     "frame:room",
			to:       "frame:survey-grid",
			expected: Point{113.0, 224.0, 0.0},
		},
		{
			name:     "runs the three-level chain backwards",
			point:    Point{113.0, 224.0, 0.0},
			from:     "frame:survey-grid",
			to:       "frame:room",
			expected: Point{2000.0, 3500.0, 0.0},
		},
		{
			name:     "goes up one chain and down the other between two frames of one parent",
			point:    Point{3000.0, 4000.0, 0.0},
			from:     "frame:building",
			to:       "frame:annex",
			expected: Point{-26.0, -13.0, 0.0},
		},
		{
			name:     "goes back between the same two, the other way round",
			point:    Point{-26.0, -13.0, 0.0},
			from:     "frame:annex",
			to:       "frame:building",
			expected: Point{3000.0, 4000.0, 0.0},
		},
		{
			name:     "leaves a point in the frame it is already in exactly as it was",
			point:    Point{3000.0, 4000.0, 0.0},
			from:     "frame:building",
			to:       "frame:building",
			expected: Point{3000.0, 4000.0, 0.0},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := frames.TransformPoint(testCase.point, testCase.from, testCase.to)

			require.NoError(t, err)
			assertPoint(t, testCase.expected, got, frameRoundTripTolerance)
		})
	}
}

// TestFramesBudgetTheRouteTheyComposeAlong is its own function because the
// assertion is an error budget rather than a position: how well the
// relationship a cross-frame answer was computed through is known.
//
// The route from the room to the annex passes through three fits, two of which
// were made against the same control point. Counting survey:CP-3 once however
// many frames it is reached through is the whole difference between an honest
// cross-frame budget and an optimistic one.
func TestFramesBudgetTheRouteTheyComposeAlong(t *testing.T) {
	frames, rendered := resolveFrames(t, frameFixture("valid"))
	require.Empty(t, rendered, "the valid fixture resolves clean")

	t.Run("carries a term for every fit the route passes through", func(t *testing.T) {
		budget, err := frames.TransformBudget("frame:room", "frame:annex")
		require.NoError(t, err)

		terms := budget.Terms()
		require.Len(t, terms, 4, "three independent fits and one shared control point")

		var names []string
		for _, term := range terms {
			names = append(names, term.Name)
		}
		assert.Equal(t,
			[]string{"survey:C-0004", "survey:C-0002", "survey:CP-3", "survey:C-0003"},
			names,
			"in the order the route reads them, up one chain and down the other")
	})

	t.Run("counts the shared control point once and names both fits which brought it", func(t *testing.T) {
		budget, err := frames.TransformBudget("frame:room", "frame:annex")
		require.NoError(t, err)

		shared := budget.Terms()[2]
		require.Equal(t, TermSystematic, shared.Kind)
		assert.Equal(t, ID("survey:CP-3"), shared.Source)
		assert.Equal(t, 0.008, shared.Magnitude)

		var contributors []ID
		for _, claim := range shared.Contributors {
			id, _ := claim.ID()
			contributors = append(contributors, id)
		}
		assert.Equal(t, []ID{"survey:C-0002", "survey:C-0003"}, contributors)
	})

	t.Run("combines to the figure the linear rule gives rather than the quadrature one", func(t *testing.T) {
		budget, err := frames.TransformBudget("frame:room", "frame:annex")
		require.NoError(t, err)

		combined, err := budget.Combined()
		require.NoError(t, err)

		// √(0.002² + 0.004² + 0.006² + 0.008²), the shared 0.008 m appearing
		// once. Read as two independent terms it would come to √(184e-6), and
		// counted twice over it would come to √(312e-6).
		assert.InDelta(t, math.Sqrt(120e-6), combined.Magnitude, frameRoundTripTolerance)
		assert.Equal(t, UnitMetre, combined.Unit)
		assert.Equal(t, 1.0, combined.CoverageFactor)
	})

	t.Run("says which term dominates the budget", func(t *testing.T) {
		budget, err := frames.TransformBudget("frame:room", "frame:annex")
		require.NoError(t, err)

		dominant, ok := budget.Dominant()

		require.True(t, ok)
		assert.Equal(t, "survey:CP-3", dominant.Name)
	})

	t.Run("budgets a one-level route from the one fit it applies", func(t *testing.T) {
		budget, err := frames.TransformBudget("frame:building", "frame:site")
		require.NoError(t, err)

		combined, err := budget.Combined()
		require.NoError(t, err)

		// √(0.004² + 0.008²), which is the building's own fit and nothing else.
		assert.InDelta(t, math.Sqrt(80e-6), combined.Magnitude, frameRoundTripTolerance)
	})

	t.Run("budgets the same route the same way run backwards", func(t *testing.T) {
		there, err := frames.TransformBudget("frame:room", "frame:annex")
		require.NoError(t, err)
		back, err := frames.TransformBudget("frame:annex", "frame:room")
		require.NoError(t, err)

		forwards, err := there.Combined()
		require.NoError(t, err)
		backwards, err := back.Combined()
		require.NoError(t, err)

		assert.InDelta(t, forwards.Magnitude, backwards.Magnitude, frameRoundTripTolerance)
	})

	t.Run("budgets nothing for a frame against itself, which applies no transform", func(t *testing.T) {
		budget, err := frames.TransformBudget("frame:building", "frame:building")
		require.NoError(t, err)

		assert.Empty(t, budget.Terms())

		_, err = budget.Combined()
		assert.ErrorIs(t, err, ErrEmptyBudget)
	})
}

// TestFramesRefuseABudgetForARouteTheyCannotWalk is its own function because it
// asserts the errors of a different method: a budget for a route which does not
// exist is refused the same way the position along it is.
func TestFramesRefuseABudgetForARouteTheyCannotWalk(t *testing.T) {
	t.Run("names the frame nothing declares", func(t *testing.T) {
		frames, rendered := resolveFrames(t, frameFixture("valid"))
		require.Empty(t, rendered)

		_, err := frames.TransformBudget("frame:nowhere", "frame:site")

		var undeclared UndeclaredFrameError
		require.ErrorAs(t, err, &undeclared)
		assert.Equal(t, ID("frame:nowhere"), undeclared.Frame)
	})

	t.Run("names both ends of a relationship nothing measured", func(t *testing.T) {
		frames, rendered := resolveFrames(t, frameFixture("not-a-transform"))
		require.NotEmpty(t, rendered, "the fixture reports the claim which is not a transform")

		_, err := frames.TransformBudget("frame:building", "frame:survey-grid")

		var unmeasured UnmeasuredFrameError
		require.ErrorAs(t, err, &unmeasured)
		assert.Equal(t, ID("frame:building"), unmeasured.Frame)
		assert.Equal(t, ID("frame:survey-grid"), unmeasured.Parent)
	})

	t.Run("names both frames when their chains reach no common frame", func(t *testing.T) {
		frames := resolveFramesRegardless(t, registryFixture("roots"))

		_, err := frames.TransformBudget("frame:building", "frame:state-plane")

		var unrelated UnrelatedFramesError
		require.ErrorAs(t, err, &unrelated)
		assert.Equal(t, ID("frame:building"), unrelated.From)
		assert.Equal(t, ID("frame:state-plane"), unrelated.To)
	})

	t.Run("stops at the frame a cyclic chain re-enters rather than walking it forever", func(t *testing.T) {
		frames := resolveFramesRegardless(t, registryFixture("cycle"))

		_, err := frames.TransformBudget("frame:a", "frame:c")

		var cycle FrameCycleError
		require.ErrorAs(t, err, &cycle)
		assert.Equal(t, ID("frame:a"), cycle.Frame)
	})
}

// TestFramesRoundTripAPointThroughEveryFrame is its own function because it is a
// property rather than a case: a position taken into another frame and brought
// back is the position it started as, whichever frame it went through.
//
// It is asserted as a property for the reason the printer's round trip is. A
// test which only compared a transformed point against a recorded number can
// pass while the way back no longer arrives anywhere near where it left, and a
// model whose georeference does not invert is one where two answers about the
// same corner disagree by however far the composition drifted.
func TestFramesRoundTripAPointThroughEveryFrame(t *testing.T) {
	frames, rendered := resolveFrames(t, frameFixture("valid"))
	require.Empty(t, rendered, "the valid fixture resolves clean")

	// A position with a fraction on every axis, so that a route which dropped or
	// swapped one of them arrives somewhere else rather than somewhere equal.
	start := Point{1234.5, -678.25, 42.125}

	for _, through := range []ID{"frame:survey-grid", "frame:site", "frame:annex", "frame:building", "frame:room"} {
		t.Run("returns the position it started at, through "+string(through), func(t *testing.T) {
			there, err := frames.TransformPoint(start, "frame:room", through)
			require.NoError(t, err)

			back, err := frames.TransformPoint(there, through, "frame:room")
			require.NoError(t, err)

			assertPoint(t, start, back, frameRoundTripTolerance)
		})
	}
}

// TestFramesRefuseARouteTheyCannotWalk is its own function because every case is
// an error and a field of it rather than a position.
//
// None of them is answered with a position which is merely approximate. A frame
// nothing declares, a chain which cycles, a relationship nobody measured and two
// frames which reach no common ancestor are each a question with no answer, and
// returning a point anyway would be inventing one.
func TestFramesRefuseARouteTheyCannotWalk(t *testing.T) {
	t.Run("names the frame nothing declares", func(t *testing.T) {
		frames, rendered := resolveFrames(t, frameFixture("valid"))
		require.Empty(t, rendered)

		_, err := frames.TransformPoint(Point{}, "frame:nowhere", "frame:site")

		var undeclared UndeclaredFrameError
		require.ErrorAs(t, err, &undeclared)
		assert.Equal(t, ID("frame:nowhere"), undeclared.Frame)
	})

	t.Run("names the frame nothing declares when it is the one arrived at", func(t *testing.T) {
		frames, rendered := resolveFrames(t, frameFixture("valid"))
		require.Empty(t, rendered)

		_, err := frames.TransformPoint(Point{}, "frame:site", "frame:nowhere")

		var undeclared UndeclaredFrameError
		require.ErrorAs(t, err, &undeclared)
		assert.Equal(t, ID("frame:nowhere"), undeclared.Frame)
	})

	t.Run("names both ends of a relationship nothing measured", func(t *testing.T) {
		frames, rendered := resolveFrames(t, frameFixture("not-a-transform"))
		require.NotEmpty(t, rendered, "the fixture reports the claim which is not a transform")

		_, err := frames.TransformPoint(Point{}, "frame:building", "frame:survey-grid")

		var unmeasured UnmeasuredFrameError
		require.ErrorAs(t, err, &unmeasured)
		assert.Equal(t, ID("frame:building"), unmeasured.Frame)
		assert.Equal(t, ID("frame:survey-grid"), unmeasured.Parent)
	})

	t.Run("stops at the frame a cyclic chain re-enters rather than walking it forever", func(t *testing.T) {
		frames := resolveFramesRegardless(t, registryFixture("cycle"))

		_, err := frames.TransformPoint(Point{}, "frame:a", "frame:c")

		var cycle FrameCycleError
		require.ErrorAs(t, err, &cycle)
		assert.Equal(t, ID("frame:a"), cycle.Frame)
	})

	t.Run("names both frames when their chains reach no common frame", func(t *testing.T) {
		frames := resolveFramesRegardless(t, registryFixture("roots"))

		_, err := frames.TransformPoint(Point{}, "frame:building", "frame:state-plane")

		var unrelated UnrelatedFramesError
		require.ErrorAs(t, err, &unrelated)
		assert.Equal(t, ID("frame:building"), unrelated.From)
		assert.Equal(t, ID("frame:state-plane"), unrelated.To)
	})

	t.Run("names the frame whose unit is no linear unit rather than assuming metres", func(t *testing.T) {
		frames := resolveFramesRegardless(t, registryFixture("unknown-unit"))

		_, err := frames.TransformPoint(Point{}, "frame:building", "frame:survey-grid")

		var unknown UnknownUnitError
		require.ErrorAs(t, err, &unknown)
		assert.Equal(t, ID("frame:building"), unknown.Frame)
	})
}

// TestFramesRefuseATransformTheyCannotRunBackwards is its own function because
// the case cannot be reached from a model which measures anything: a fit which
// collapsed a frame onto a plane is legal to write down and has no way back, and
// the graph is built here rather than read so that the degenerate transform is
// the only thing in it.
func TestFramesRefuseATransformTheyCannotRunBackwards(t *testing.T) {
	testCases := []struct {
		name      string
		transform Transform
	}{
		{
			name: "refuses a rotation of zero determinant",
			transform: Transform{
				Rotation: [9]float64{1.0, 0.0, 0.0, 1.0, 0.0, 0.0, 0.0, 0.0, 1.0},
				Scale:    1.0,
			},
		},
		{
			name: "refuses a scale of zero",
			transform: Transform{
				Rotation: [9]float64{1.0, 0.0, 0.0, 0.0, 1.0, 0.0, 0.0, 0.0, 1.0},
				Scale:    0.0,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			frames := framesOf(testCase.transform)

			// Forwards is a position like any other: a transform which flattens a
			// frame maps every point in it somewhere.
			_, err := frames.TransformPoint(Point{1.0, 2.0, 3.0}, "frame:building", "frame:survey-grid")
			require.NoError(t, err)

			_, err = frames.TransformPoint(Point{1.0, 2.0, 3.0}, "frame:survey-grid", "frame:building")

			var singular SingularTransformError
			require.ErrorAs(t, err, &singular)
			assert.Equal(t, ID("frame:building"), singular.Frame)
			assert.Equal(t, ID("frame:survey-grid"), singular.Parent)
		})
	}
}

// framesOf is a two-frame graph measured by one transform, built rather than
// read, for the cases a loadable model cannot reach.
func framesOf(transform Transform) *Frames {
	registry := &Registry{frames: map[ID]Frame{
		"frame:survey-grid": {ID: "frame:survey-grid", Unit: UnitMetre},
		"frame:building": {
			ID:        "frame:building",
			Unit:      UnitMetre,
			Parent:    "frame:survey-grid",
			Transform: "survey:C-0031",
		},
	}}

	return &Frames{
		registry: registry,
		root:     "frame:survey-grid",
		measured: map[ID]*Claim{
			"frame:building": {value: Value{shape: ShapeTransform, transform: transform}},
		},
	}
}

// TestFramesOfNothingAnswerNothing is its own function because it is about a
// receiver rather than about a model: every question has to work on a zero value
// and on a nil, which is what lets a caller which did not read the diagnostics
// of a failed load ask them rather than panic.
func TestFramesOfNothingAnswerNothing(t *testing.T) {
	testCases := []struct {
		name   string
		frames *Frames
	}{
		{name: "answers nothing when it is nil", frames: nil},
		{name: "answers nothing when nothing was resolved", frames: &Frames{}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, hasRoot := testCase.frames.Root()
			assert.False(t, hasRoot)

			assert.Empty(t, slices.Collect(testCase.frames.Chain("frame:building")))

			_, measured := testCase.frames.Measurement("frame:building")
			assert.False(t, measured)

			_, transformed := testCase.frames.Transform("frame:building")
			assert.False(t, transformed)

			_, err := testCase.frames.TransformPoint(Point{}, "frame:building", "frame:survey-grid")
			assert.True(t, errors.As(err, &UndeclaredFrameError{}))

			_, err = testCase.frames.TransformBudget("frame:building", "frame:survey-grid")
			assert.True(t, errors.As(err, &UndeclaredFrameError{}))
		})
	}
}
