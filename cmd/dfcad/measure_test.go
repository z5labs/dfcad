// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// measuring is the vocabulary every invocation below is asked in: which
// predicate carries a position, and how close two corners are one corner.
func measuring(args ...string) []string {
	return append([]string{
		"measure",
		"--position", "position",
		"--tolerance", "coincident",
	}, args...)
}

// computed runs measure against a fixture and returns what it wrote, requiring
// the run to have exited with the code given.
func computed(t *testing.T, expectedCode int, files map[string]string, args ...string) (measureResult, string) {
	t.Helper()

	root := tree(t, files)
	stdout, stderr := invoke(t, expectedCode, root, measuring(args...)...)

	return listed[measureResult](t, stdout), stderr
}

// crossed is the fixture with the plot's two far corners swapped, which leaves
// the same four edges traversing a ring which crosses over itself.
//
// It is written as a swap rather than as a fifth corner because the ring has to
// stay closed and stay in one plane: what is under test is a shape which is not
// a shape, and a ring which merely failed to close would be refused a step
// earlier for a different reason.
func crossed() map[string]string {
	const (
		northEast = "(value (40.0 12.0 0.0) m)"
		northWest = "(value (20.0 12.0 0.0) m)"
		swapping  = "(value (swapping) m)"
	)

	files := model()

	// Through a placeholder, because a swap written as two replacements in a row
	// would put the first corner back where the second came from.
	written := files["entities/geometry.dfc"]
	written = strings.Replace(written, northEast, swapping, 1)
	written = strings.Replace(written, northWest, northEast, 1)
	written = strings.Replace(written, swapping, northWest, 1)

	files["entities/geometry.dfc"] = written

	return files
}

func TestRunMeasure(t *testing.T) {
	result, _ := computed(t, exitSuccess, model(), "site:P-01")

	assert.Equal(t, "measure", result.Command)
	assert.Equal(t, "site:P-01", result.Subject)
	assert.Equal(t, familyNode, result.Family)
	assert.True(t, result.Derived)
	assert.Equal(t, "frame:building", result.Frame)
	assert.Equal(t, "m", result.Unit)

	require.NotNil(t, result.Tolerance)
	assert.Equal(t, "coincident", result.Tolerance.Name)

	// Twenty by twelve, on the corners the boundary survey put there.
	require.NotNil(t, result.Area)
	assert.InDelta(t, 240.0, result.Area.Value, 1e-9)
	assert.Equal(t, "m²", result.Area.Unit, "an area is in the square of the frame's unit")

	require.NotNil(t, result.Length)
	assert.InDelta(t, 64.0, result.Length.Value, 1e-9)
	assert.Equal(t, "m", result.Length.Unit)

	require.NotNil(t, result.Centroid)
	assert.Equal(t, []float64{30, 6, 0}, result.Centroid.At)
	assert.Equal(t, "m", result.Centroid.Unit)

	require.NotNil(t, result.Bounds)
	assert.Equal(t, []float64{20, 0, 0}, result.Bounds.Min)
	assert.Equal(t, []float64{40, 12, 0}, result.Bounds.Max)
	assert.Equal(t, "m", result.Bounds.Unit)

	// The answer is only as good as the corners it was read from, and it says so.
	require.NotNil(t, result.Budget)
	assert.Len(t, result.Budget.Terms, 4, "one term per surveyed corner, none of them shared")
	require.NotNil(t, result.Budget.Combined)
	assert.Positive(t, result.Budget.Combined.Magnitude)
	assert.Empty(t, result.Budget.From, "this budget is a computation rather than a route between frames")
}

// TestRunMeasureAnswersForEveryFamily is its own function because the id is the
// whole of the dispatch: what a measurement is depends on which family holds the
// id, and there is no flag saying which.
func TestRunMeasureAnswersForEveryFamily(t *testing.T) {
	testCases := []struct {
		name             string
		id               string
		expectedFamily   string
		expectedArea     float64
		expectsArea      bool
		expectedLength   float64
		expectsLength    bool
		expectedCentroid []float64
	}{
		{
			name:             "measures a semantic node through the loops which bound it",
			id:               "site:P-01",
			expectedFamily:   familyNode,
			expectedArea:     240.0,
			expectsArea:      true,
			expectedLength:   64.0,
			expectsLength:    true,
			expectedCentroid: []float64{30, 6, 0},
		},
		{
			name:             "measures a loop through the ring its edges traverse",
			id:               "geom:L-11",
			expectedFamily:   familyLoop,
			expectedArea:     240.0,
			expectsArea:      true,
			expectedLength:   64.0,
			expectsLength:    true,
			expectedCentroid: []float64{30, 6, 0},
		},
		{
			name:             "measures an edge from its two ends",
			id:               "geom:E-11",
			expectedFamily:   familyEdge,
			expectsArea:      false,
			expectedLength:   20.0,
			expectsLength:    true,
			expectedCentroid: []float64{30, 0, 0},
		},
		{
			name:             "measures a vertex from where it is",
			id:               "geom:V-11",
			expectedFamily:   familyVertex,
			expectsArea:      false,
			expectsLength:    false,
			expectedCentroid: []float64{20, 0, 0},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, _ := computed(t, exitSuccess, model(), testCase.id)

			assert.Equal(t, testCase.id, result.Subject)
			assert.Equal(t, testCase.expectedFamily, result.Family)
			assert.True(t, result.Derived)

			if testCase.expectsArea {
				require.NotNil(t, result.Area)
				assert.InDelta(t, testCase.expectedArea, result.Area.Value, 1e-9)
			} else {
				assert.Nil(t, result.Area, "a line encloses nothing, and there is no answer rather than a zero")
			}

			if testCase.expectsLength {
				require.NotNil(t, result.Length)
				assert.InDelta(t, testCase.expectedLength, result.Length.Value, 1e-9)
			} else {
				assert.Nil(t, result.Length, "a point has no extent, and there is no answer rather than a zero")
			}

			require.NotNil(t, result.Centroid)
			assert.Equal(t, testCase.expectedCentroid, result.Centroid.At)
		})
	}
}

// TestRunMeasureReportsTheDigestItWasComputedAgainst is its own function because
// it is about the provenance of a computation rather than about a figure: a
// derived value which cannot say which tree it came from is one nobody can check
// against the tree in front of them.
func TestRunMeasureReportsTheDigestItWasComputedAgainst(t *testing.T) {
	files := model()
	root := tree(t, files)

	stdout, _ := invoke(t, exitSuccess, root, measuring("site:P-01")...)
	result := listed[measureResult](t, stdout)

	t.Run("names the digest of the source tree", func(t *testing.T) {
		digest, err := dfcad.DigestOf(root)
		require.NoError(t, err)
		require.True(t, digest.Known())

		assert.Equal(t, digest.String(), result.Digest)
	})

	t.Run("names a different one for a tree which moved", func(t *testing.T) {
		moved := model()
		moved["entities/geometry.dfc"] = strings.Replace(
			moved["entities/geometry.dfc"], "(value (40.0 0.0 0.0) m)", "(value (41.0 0.0 0.0) m)", 1)

		after, _ := computed(t, exitSuccess, moved, "site:P-01")

		assert.NotEqual(t, result.Digest, after.Digest)
		assert.NotEqual(t, result.Area.Value, after.Area.Value,
			"the answer moved with the tree, which is what the digest is of")
	})
}

// TestRunMeasureIsDeterministic is its own function because it asserts about two
// runs rather than about one: the same model has to produce the same bytes, or
// nothing downstream can diff an answer.
func TestRunMeasureIsDeterministic(t *testing.T) {
	root := tree(t, model())

	first, _ := invoke(t, exitSuccess, root, measuring("site:P-01")...)
	second, _ := invoke(t, exitSuccess, root, measuring("site:P-01")...)

	assert.Equal(t, first, second)
}

// TestRunMeasureOfSomethingWithNoOutline is its own function because it is the
// successful run which answers with nothing: a node which references no loop is
// not malformed, and "derived with no figures" is what tells it apart from a
// boundary which could not be read.
func TestRunMeasureOfSomethingWithNoOutline(t *testing.T) {
	result, _ := computed(t, exitSuccess, model(), "site:S-101")

	assert.True(t, result.Derived, "there was nothing wrong with the question")
	assert.Nil(t, result.Area)
	assert.Nil(t, result.Length)
	assert.NotEmpty(t, result.Digest, "and it still says which tree it read")

	// A budget with no terms, no combined figure and no reason for there being
	// none would read as an answer known exactly. Nothing was computed from any
	// claim, so nothing is reported about how well it is known.
	assert.Nil(t, result.Budget)
	assert.NotContains(t, stdoutOf(t, model(), "site:S-101"), `"budget"`)
}

// stdoutOf is what a successful measurement of one id wrote, as the bytes a
// caller reads rather than as the object they decode to.
func stdoutOf(t *testing.T, files map[string]string, args ...string) string {
	t.Helper()

	stdout, _ := invoke(t, exitSuccess, tree(t, files), measuring(args...)...)

	return stdout
}

// TestRunMeasureRefusesAShapeWhichIsNotOne is its own function because every case
// in it exits with a check failure and carries no figures.
func TestRunMeasureRefusesAShapeWhichIsNotOne(t *testing.T) {
	testCases := []struct {
		name             string
		files            map[string]string
		id               string
		expectedInStderr []string
	}{
		{
			name:             "refuses a ring which crosses over itself",
			files:            crossed(),
			id:               "site:P-01",
			expectedInStderr: []string{"geom:L-11", "crosses"},
		},
		{
			name: "names the corner nothing says the position of",
			files: func() map[string]string {
				files := model()
				files["entities/geometry.dfc"] = strings.Replace(
					files["entities/geometry.dfc"], `(position
    (value (20.0 0.0 0.0) m)
    (source "Boundary survey BS-2026-004, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-11"))`, "", 1)
				return files
			}(),
			id:               "site:P-01",
			expectedInStderr: []string{"geom:V-11"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, stderr := computed(t, exitCheck, testCase.files, testCase.id)

			assert.False(t, result.Derived)
			assert.Equal(t, testCase.id, result.Subject, "a refusal still says which question it answers")
			assert.NotEmpty(t, result.Digest, "and which tree it was asked of")
			assert.Nil(t, result.Area, "no plausible-looking number came back instead")
			assert.Nil(t, result.Length)
			assert.Nil(t, result.Centroid)
			assert.Nil(t, result.Bounds)

			for _, expected := range testCase.expectedInStderr {
				assert.Contains(t, stderr, expected)
			}
		})
	}
}

// TestRunMeasureDoesNotStandInForResolve is its own function because it is about
// two commands rather than one: a claimed measurement and a computed one are
// different answers, and the moment either silently substitutes for the other the
// disagreement between them stops being visible.
func TestRunMeasureDoesNotStandInForResolve(t *testing.T) {
	root := tree(t, model())

	t.Run("computes an area for a thing whose claimed area disagrees with it", func(t *testing.T) {
		stdout, _ := invoke(t, exitSuccess, root, measuring("site:S-103")...)
		measured := listed[measureResult](t, stdout)

		require.NotNil(t, measured.Area)
		assert.InDelta(t, 48.0, measured.Area.Value, 1e-9, "eight by six, off the corners")

		// The same node claims thirty-one square metres, from two surveys of
		// which resolution prefers the more accurate. It still says so, and says
		// nothing at all about where the corners are.
		stdout, _ = invoke(t, exitSuccess, root, "resolve", "site:S-103", "area")
		resolved := listed[resolveResult](t, stdout)

		assert.Equal(t, "resolve", resolved.Command)
		require.NotNil(t, resolved.Value)
		require.NotNil(t, resolved.Value.Scalar)
		assert.InDelta(t, 31.4, *resolved.Value.Scalar, 1e-9,
			"which is not what the corners come to, and both answers stay visible")
	})

	t.Run("does not answer from a claim where the geometry says nothing", func(t *testing.T) {
		// Room A claims an area and has no outline at all. Measurement reports
		// that there is nothing to measure rather than reaching for the claim.
		stdout, _ := invoke(t, exitSuccess, root, measuring("site:S-101")...)
		measured := listed[measureResult](t, stdout)

		assert.True(t, measured.Derived)
		assert.Nil(t, measured.Area)

		stdout, _ = invoke(t, exitSuccess, root, "resolve", "site:S-101", "area")
		resolved := listed[resolveResult](t, stdout)

		require.NotNil(t, resolved.Value)
		require.NotNil(t, resolved.Value.Scalar)
		assert.InDelta(t, 24.2, *resolved.Value.Scalar, 1e-9, "and resolution still answers what was claimed")
	})
}

// TestRunMeasureRefusesAnInvocationWithoutTheVocabulary is its own function
// because it is about what the run was asked rather than about what the model
// says.
func TestRunMeasureRefusesAnInvocationWithoutTheVocabulary(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedInStderr []string
	}{
		{
			name:             "names every flag it was not given at once",
			args:             []string{"measure", "site:P-01"},
			expectedInStderr: []string{"--position", "--tolerance"},
		},
		{
			name:             "names the one flag it was not given",
			args:             []string{"measure", "--position", "position", "site:P-01"},
			expectedInStderr: []string{"--tolerance"},
		},
		{
			name:             "refuses a run with nothing to measure",
			args:             measuring(),
			expectedInStderr: []string{ErrMissingSubject.Error()},
		},
		{
			name:             "refuses more ids than it can answer about",
			args:             measuring("site:P-01", "site:S-103"),
			expectedInStderr: []string{"site:S-103"},
		},
		{
			name:             "refuses an id nothing in the model holds",
			args:             measuring("site:P-99"),
			expectedInStderr: []string{"site:P-99"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, model())
			stdout, stderr := invoke(t, exitUsage, root, testCase.args...)

			assert.Empty(t, stdout, "a run which asked no question writes no result")
			for _, expected := range testCase.expectedInStderr {
				assert.Contains(t, stderr, expected)
			}
		})
	}
}

// TestRunMeasureHumanOutputNeverChangesStdout is its own function because it is
// about the one property the format flag must not have.
func TestRunMeasureHumanOutputNeverChangesStdout(t *testing.T) {
	root := tree(t, model())

	machine, machineReport := invoke(t, exitSuccess, root, measuring("site:P-01")...)
	human, humanReport := invoke(t, exitSuccess, root, measuring("site:P-01", "--format", formatHuman)...)
	loud, loudReport := invoke(t, exitSuccess, root, measuring("site:P-01", "--format", formatHuman, "-v")...)

	assert.Equal(t, machine, human)
	assert.Equal(t, machine, loud)

	assert.Empty(t, machineReport)
	assert.Contains(t, humanReport, "site:P-01: area 240.0 m², length 64.0 m")
	assert.NotContains(t, humanReport, "corners known to")
	assert.Contains(t, loudReport, "corners known to")
	assert.Contains(t, loudReport, "independent 0.004 m")
}

// curvedMeasurement runs measure over the curved fixture with the vocabulary the
// case names, rather than through [computed], which fixes the position and
// tolerance of the straight fixture's registry.
func curvedMeasurement(t *testing.T, expectedCode int, files map[string]string, args ...string) (measureResult, string) {
	t.Helper()

	root := tree(t, files)
	stdout, stderr := invoke(t, expectedCode, root, append([]string{
		"measure",
		"--position", "position",
		"--tolerance", "coincident",
	}, args...)...)

	return listed[measureResult](t, stdout), stderr
}

// TestRunMeasureReadsACurvedEdgeOrSaysItDidNot is its own function because the
// two halves of it are one behaviour: a run which names the vocabulary measures
// the wall, and a run which does not measures the chord and says so. Either
// answer on its own is the failure.
func TestRunMeasureReadsACurvedEdgeOrSaysItDidNot(t *testing.T) {
	// The plot's road frontage bows out along an arc of radius twenty-six over a
	// chord of twenty. Read as the chord it is a twenty by twelve rectangle;
	// read as the arc it is that plus the circular segment the bow encloses,
	// which is written as the closed form rather than as a decimal: an
	// expectation written as 26.8 says nothing about where it came from.
	const chorded = 240.0
	bulge := frontageBulge()

	t.Run("measures the chord and says which edge it chorded", func(t *testing.T) {
		result, stderr := curvedMeasurement(t, exitSuccess, curved(), "site:P-01")

		require.NotNil(t, result.Area)
		assert.InDelta(t, chorded, result.Area.Value, 1e-9)

		require.Len(t, result.Chorded, 1, "one of the plot's four edges states a curve")
		assert.Equal(t, "geom:E-21", result.Chorded[0].Edge)
		assert.Equal(t, []string{"arc-centre", "arc-through"}, result.Chorded[0].Predicates,
			"the predicates to name, which is what makes the report actionable")
		assert.NotEmpty(t, result.Chorded[0].Span.String())

		assert.Contains(t, stderr, "geom:E-21", "and a person is told the same thing")
	})

	t.Run("measures the arc where the vocabulary is named", func(t *testing.T) {
		result, _ := curvedMeasurement(t, exitSuccess, curved(),
			"--arc-centre", "arc-centre", "--arc-through", "arc-through", "site:P-01")

		require.NotNil(t, result.Area)
		assert.InDelta(t, chorded+bulge, result.Area.Value, 1e-9,
			"the figure is the closed form of the arc and not of any drawing of it")

		assert.Empty(t, result.Chorded, "there is nothing left unread to report")
		assert.Nil(t, result.Chord, "nothing was drawn to reach the figure")
		assert.Nil(t, result.Deviation)
	})

	t.Run("says nothing about curves for a model which claims none", func(t *testing.T) {
		result, stderr := computed(t, exitSuccess, model(), "site:P-01")

		assert.Empty(t, result.Chorded, "a plot of straight edges has no curve to go unread")
		assert.Nil(t, result.Chord)
		assert.Nil(t, result.Deviation)
		assert.NotContains(t, stderr, "curved edge")
	})
}

// TestRunMeasureNestsCurvedRingsAtAChordItIsGiven is its own function because
// the chord does something different here from anywhere else: it decides the
// nesting and never the figure, and the figure it does not decide is the one
// worth asserting.
func TestRunMeasureNestsCurvedRingsAtAChordItIsGiven(t *testing.T) {
	curving := []string{"--arc-centre", "arc-centre", "--arc-through", "arc-through"}

	t.Run("refuses a region of several rings one of which bends", func(t *testing.T) {
		result, stderr := curvedMeasurement(t, exitCheck, curved(), append(curving, "site:S-01")...)

		assert.False(t, result.Derived)
		assert.Contains(t, stderr, "no chord tolerance named")
	})

	t.Run("nests them where a chord is named and still measures the arcs", func(t *testing.T) {
		result, _ := curvedMeasurement(t, exitSuccess, curved(),
			append(curving, "--chord", "chord-deviation", "site:S-01")...)

		require.NotNil(t, result.Area)

		// A ten metre plate with a round courtyard of radius two taken out of
		// the middle of it.
		assert.InDelta(t, 100-4*math.Pi, result.Area.Value, 1e-9)

		require.NotNil(t, result.Chord)
		assert.Equal(t, "chord-deviation", result.Chord.Name)

		require.NotNil(t, result.Deviation)
		assert.Positive(t, result.Deviation.Value)
		assert.LessOrEqual(t, result.Deviation.Value, result.Chord.Value)
		assert.Equal(t, "m", result.Deviation.Unit)
	})
}

func TestRunMeasureRefusesHalfTheArcVocabulary(t *testing.T) {
	testCases := []struct {
		name string
		args []string
		says string
	}{
		{
			name: "a centre with no point on the curve",
			args: []string{"--arc-centre", "arc-centre", "site:P-01"},
			says: "--arc-through",
		},
		{
			name: "a point on the curve with no centre",
			args: []string{"--arc-through", "arc-through", "site:P-01"},
			says: "--arc-centre",
		},
	}

	root := tree(t, curved())

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, stderr := invoke(t, exitUsage, root, append([]string{
				"measure", "--position", "position", "--tolerance", "coincident",
			}, testCase.args...)...)

			assert.Contains(t, stderr, testCase.says)
		})
	}
}

// TestRunMeasureNamesAPredicateOnceHoweverOftenItIsClaimed is its own function
// because what it asserts is a property of the list rather than a figure: a
// curve re-surveyed is two claims under one predicate, and an author told to
// name that predicate twice would be told to fix a model in which nothing is
// wrong.
//
// The retracted claim is the case which makes it worth asserting. Resolution
// never considers one, so a report which counted it would be reporting a
// statement nobody is making.
func TestRunMeasureNamesAPredicateOnceHoweverOftenItIsClaimed(t *testing.T) {
	const surveyed = `  (arc-centre
    (value (16.0 2.0 0.0) m)
    (source "Setting-out report SO-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-19"))`

	// The first setting-out of the bay, retracted in favour of the as-built
	// check which replaced it.
	const resurveyed = `  (arc-centre
    (id site:M-0101)
    (value (16.0 2.05 0.0) m)
    (source "Setting-out report SO-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-19")
    (rank deprecated)
    (superseded-by site:M-0102))
  (arc-centre
    (id site:M-0102)
    (value (16.0 2.0 0.0) m)
    (source "As-built check AB-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-05-06"))`

	files := curved()

	written := files["entities/geometry.dfc"]
	require.Contains(t, written, surveyed)
	files["entities/geometry.dfc"] = strings.Replace(written, surveyed, resurveyed, 1)

	result, _ := curvedMeasurement(t, exitSuccess, files, "site:S-41")

	require.Len(t, result.Chorded, 1)
	assert.Equal(t, []string{"arc-centre", "arc-through"}, result.Chorded[0].Predicates,
		"one entry per predicate, however many claims are written under it")
}

// TestRunMeasureOfAnOpenRun is its own function because the figures it asserts
// are the ones a chain has: it reaches a length and it encloses nothing, and
// both of those are an answer rather than a refusal.
func TestRunMeasureOfAnOpenRun(t *testing.T) {
	root := tree(t, planFixture())
	stdout, stderr := invoke(t, exitSuccess, root, measuring("site:D-01")...)

	result := listed[measureResult](t, stdout)

	assert.True(t, result.Derived, stderr)
	assert.Equal(t, "site:D-01", result.Subject)
	assert.Equal(t, familyNode, result.Family)

	require.NotNil(t, result.Length)
	assert.InDelta(t, 1.6, result.Length.Value, 1e-9, "0.9 through the opening and 0.7 beyond it")
	assert.Equal(t, "m", result.Length.Unit)

	// And no area. A door is not a room, and reporting nought square metres for
	// one would be a figure about a shape nobody drew.
	assert.Nil(t, result.Area)
	assert.Nil(t, result.Centroid)

	require.NotNil(t, result.Bounds)
}
