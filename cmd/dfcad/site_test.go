// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// siting is the question every invocation below asks: whether Room C, outlined
// on the site grid, fits inside the plot outlined on the building's.
//
// The two are in different frames on purpose. That is what makes the answer
// depend on the claim which measures the two grids against each other, and what
// puts that claim's accuracy in the budget.
func siting(args ...string) []string {
	return append([]string{
		"site",
		"--within", "site:P-01",
		"--position", "position",
		"--tolerance", "coincident",
	}, args...)
}

// fitted runs site against a fixture and returns what it wrote, requiring the
// run to have exited with the code given.
func fitted(t *testing.T, expectedCode int, args ...string) (siteResult, string) {
	t.Helper()

	root := tree(t, model())
	stdout, stderr := invoke(t, expectedCode, root, siting(args...)...)

	return listed[siteResult](t, stdout), stderr
}

func TestRunSite(t *testing.T) {
	result, _ := fitted(t, exitSuccess, "site:S-103")

	assert.Equal(t, "site", result.Command)
	assert.Equal(t, "site:S-103", result.Subject)
	assert.Equal(t, "site:P-01", result.Within)
	assert.True(t, result.Sited)

	// The answer is in the envelope's frame; the subject was written in another
	// one, which is what says a transform was read to get here.
	assert.Equal(t, "frame:building", result.Frame)
	assert.Equal(t, "frame:site-grid", result.DeclaredIn)
	assert.True(t, result.Carried)
	assert.Equal(t, "m", result.Unit)

	require.NotNil(t, result.Tolerance)
	assert.Equal(t, "coincident", result.Tolerance.Name)

	// Eight by six, carried onto a plot twenty by twelve, three metres inside
	// its nearest edge.
	require.NotNil(t, result.Clearance)
	assert.Equal(t, 0.0, result.Clearance.Required)
	assert.InDelta(t, 3.0, result.Clearance.Actual, 0.001)
	assert.InDelta(t, 3.0, result.Clearance.Margin, 0.001)
	assert.Equal(t, "m", result.Clearance.Unit)

	// Nine claims of four millimetres each, none of them shared, which is twelve
	// millimetres in quadrature.
	require.NotNil(t, result.Clearance.Uncertainty)
	assert.InDelta(t, 0.012, result.Clearance.Uncertainty.Magnitude, 1e-9)
	assert.Equal(t, 1.0, result.Clearance.Uncertainty.CoverageFactor)

	assert.Equal(t, "fits", result.Verdict)
	assert.True(t, result.Decided)

	// Every region the answer was composed from comes back, including the two
	// which cover nothing: an absent region reads as one which was never
	// computed.
	require.NotNil(t, result.Envelope)
	require.NotNil(t, result.Proposal)
	require.NotNil(t, result.Needed)
	require.NotNil(t, result.Shared)
	require.NotNil(t, result.Spill)

	assert.InDelta(t, 240.0, result.Envelope.Area, 0.25)
	assert.InDelta(t, 48.0, result.Proposal.Area, 0.25)
	assert.InDelta(t, 48.0, result.Needed.Area, 0.25, "nothing beyond fitting at all was required")
	assert.InDelta(t, 48.0, result.Shared.Area, 0.25)
	assert.True(t, result.Spill.Empty)

	// Every term names the claims behind it, which is what makes the budget
	// something to act on rather than a number.
	require.NotNil(t, result.Budget)
	require.NotEmpty(t, result.Budget.Terms)
	for _, term := range result.Budget.Terms {
		assert.NotEmpty(t, term.Name)
		assert.NotEmpty(t, term.Contributors)
	}
	assert.Empty(t, result.Budget.From, "this budget is a computation rather than a route between frames")
}

// TestRunSiteReportsTheDigestItWasComputedAgainst is its own function because it
// is about the provenance of a computation rather than about a fit: a derived
// value which cannot say which tree it came from is one nobody can check against
// the tree in front of them
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
func TestRunSiteReportsTheDigestItWasComputedAgainst(t *testing.T) {
	files := model()
	root := tree(t, files)

	stdout, _ := invoke(t, exitSuccess, root, siting("site:S-103")...)
	result := listed[siteResult](t, stdout)

	t.Run("names the digest of the source tree", func(t *testing.T) {
		digest, err := dfcad.DigestOf(root)
		require.NoError(t, err)
		require.True(t, digest.Known())

		assert.Equal(t, digest.String(), result.Digest)
	})

	t.Run("names a different one for a tree which moved", func(t *testing.T) {
		// The room is carried a metre across the plot, which moves the room's
		// nearest edge and so the clearance the verdict is decided on.
		moved := model()
		moved["entities/geometry.dfc"] = strings.NewReplacer(
			"203.0 0.0) m)", "204.0 0.0) m)",
			"209.0 0.0) m)", "210.0 0.0) m)",
		).Replace(moved["entities/geometry.dfc"])

		stdout, _ := invoke(t, exitSuccess, tree(t, moved), siting("site:S-103")...)
		after := listed[siteResult](t, stdout)

		assert.NotEqual(t, result.Digest, after.Digest)
		require.NotNil(t, after.Clearance)
		assert.NotEqual(t, result.Clearance.Actual, after.Clearance.Actual,
			"the answer moved with the tree, which is what the digest is of")
	})
}

// TestAFitOfATreeWithNoDigestWritesNoDigestField is its own function because it
// asserts about the bytes rather than about a figure: a digest of nothing would
// key an answer to a tree nobody could produce, so where there is none the field
// is absent rather than empty.
func TestAFitOfATreeWithNoDigestWritesNoDigestField(t *testing.T) {
	// A graph assembled from no tree at all has no digest, which is the state a
	// model that was never read from disk leaves behind.
	result := reportSite(command{name: "site"}, nil, "site:S-103", "site:P-01", dfcad.Fit{}, nil, false)

	assert.Empty(t, result.Digest)

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), `"digest"`)
}

// TestRunSiteAnswersNoAsASuccessfulRun is its own function because it asserts
// about a run which answered and whose answer is no, which is a different shape
// of behaviour from one which could not answer.
func TestRunSiteAnswersNoAsASuccessfulRun(t *testing.T) {
	result, _ := fitted(t, exitSuccess, "--clearance", "3.5", "site:S-103")

	assert.True(t, result.Sited)
	assert.Equal(t, "does-not-fit", result.Verdict)
	assert.True(t, result.Decided)

	require.NotNil(t, result.Clearance)
	assert.Equal(t, 3.5, result.Clearance.Required)
	assert.InDelta(t, 3.0, result.Clearance.Actual, 0.001)
	assert.InDelta(t, -0.5, result.Clearance.Margin, 0.001)

	// The strip it needs and cannot have is where a refusal points, rather than
	// leaving somebody to work out which side is over the line.
	require.NotNil(t, result.Spill)
	assert.False(t, result.Spill.Empty)
	assert.Positive(t, result.Spill.Area)

	require.NotNil(t, result.Needed)
	assert.Greater(t, result.Needed.Area, result.Proposal.Area)
}

// TestRunSiteWithholdsAVerdictInsideTheUncertainty is its own function because
// it asserts about the one answer which is neither yes nor no.
func TestRunSiteWithholdsAVerdictInsideTheUncertainty(t *testing.T) {
	result, stderr := fitted(t, exitSuccess, "--clearance", "3.0", "site:S-103")

	assert.True(t, result.Sited)
	assert.Equal(t, "might-fit", result.Verdict)
	assert.False(t, result.Decided, "a margin inside its own error bar decides nothing")

	require.NotNil(t, result.Clearance)
	assert.InDelta(t, 0.0, result.Clearance.Margin, 0.001)

	// It is said out loud on stderr as well, because it is the finding somebody
	// has to do something about and a caller reading the terminal would
	// otherwise have to notice a word in a JSON object.
	assert.Contains(t, stderr, "might fit inside site:P-01")
}

// TestRunSiteReportsForAPerson is its own function because it asserts about
// stderr rather than about the contract on stdout.
func TestRunSiteReportsForAPerson(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedInStderr []string
	}{
		{
			name:             "says nothing readable in the default format",
			args:             []string{"site:S-103"},
			expectedInStderr: nil,
		},
		{
			name:             "summarises the answer for a person",
			args:             []string{"--format", "human", "site:S-103"},
			expectedInStderr: []string{"site:S-103 in site:P-01: fits", "clearance 3.0 m", "k = 1.0"},
		},
		{
			name: "breaks the budget out term by term when asked for more",
			args: []string{"--format", "human", "-v", "site:S-103"},
			expectedInStderr: []string{
				"site:S-103 in site:P-01: fits",
				"independent site:C-0001",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, stderr := fitted(t, exitSuccess, testCase.args...)

			if len(testCase.expectedInStderr) == 0 {
				assert.Empty(t, strings.TrimSpace(stderr))
				return
			}

			for _, expected := range testCase.expectedInStderr {
				assert.Contains(t, stderr, expected)
			}
		})
	}
}

// TestRunSiteRefusals is its own function because every case in it comes back
// with no answer at all.
func TestRunSiteRefusals(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedCode     int
		expectedInStderr []string
	}{
		{
			name:             "refuses a run which did not say what to fit inside",
			args:             []string{"site", "--position", "position", "--tolerance", "coincident", "site:S-103"},
			expectedCode:     exitUsage,
			expectedInStderr: []string{"--within"},
		},
		{
			name:             "names every vocabulary flag a run did not give",
			args:             []string{"site", "--within", "site:P-01", "site:S-103"},
			expectedCode:     exitUsage,
			expectedInStderr: []string{"--position", "--tolerance"},
		},
		{
			name:             "refuses a run which named nothing to site",
			args:             siting(),
			expectedCode:     exitUsage,
			expectedInStderr: []string{"expected the id"},
		},
		{
			name:             "refuses more than one subject",
			args:             siting("site:S-103", "site:S-102"),
			expectedCode:     exitUsage,
			expectedInStderr: []string{"site:S-102"},
		},
		{
			name:             "names an id the model does not declare",
			args:             siting("site:S-999"),
			expectedCode:     exitUsage,
			expectedInStderr: []string{"site:S-999"},
		},
		{
			name:             "refuses a subject which bounds no area",
			args:             siting("site:S-102"),
			expectedCode:     exitCheck,
			expectedInStderr: []string{"site:S-102"},
		},
		{
			name:             "refuses a clearance shorter than the tolerance corners are judged against",
			args:             siting("--clearance", "0.001", "site:S-103"),
			expectedCode:     exitCheck,
			expectedInStderr: []string{"coincident"},
		},
		{
			name:             "refuses a clearance written as a distance outwards",
			args:             siting("--clearance", "-1", "site:S-103"),
			expectedCode:     exitCheck,
			expectedInStderr: []string{"to be a distance"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, model())

			stdout, stderr := invoke(t, testCase.expectedCode, root, testCase.args...)

			for _, expected := range testCase.expectedInStderr {
				assert.Contains(t, stderr, expected)
			}

			if testCase.expectedCode == exitUsage {
				assert.Empty(t, stdout, "a run which never asked a question has no result")
				return
			}

			// A question which was asked and could not be answered still writes
			// the object, so that a caller reads why from the diagnostics rather
			// than from an empty stream.
			result := listed[siteResult](t, stdout)
			assert.False(t, result.Sited)
			assert.NotEmpty(t, result.Digest, "and which tree it was asked of")
			assert.Empty(t, result.Verdict)
			assert.Nil(t, result.Clearance)
		})
	}
}

// curvedFit runs site over the curved fixture, where the pavilion sits in the
// bulge of the plot's frontage: inside the arc everywhere and outside the chord
// between its two ends everywhere.
func curvedFit(t *testing.T, expectedCode int, args ...string) (siteResult, string) {
	t.Helper()

	root := tree(t, curved())
	stdout, stderr := invoke(t, expectedCode, root, siting(args...)...)

	return listed[siteResult](t, stdout), stderr
}

// TestRunSiteReadsACurvedEnvelopeOrSaysItDidNot is its own function because the
// two halves of it are one behaviour and they give opposite verdicts: the same
// pavilion on the same plot fits or does not fit according to whether the run
// read the boundary or the chord across it, which is the whole reason a chorded
// answer cannot be a silent one.
func TestRunSiteReadsACurvedEnvelopeOrSaysItDidNot(t *testing.T) {
	curving := []string{"--arc-centre", "arc-centre", "--arc-through", "arc-through"}

	t.Run("decides against the chord and says which edge it chorded", func(t *testing.T) {
		result, stderr := curvedFit(t, exitSuccess, "site:S-31")

		require.True(t, result.Sited)
		assert.Equal(t, "does-not-fit", result.Verdict, "the pavilion is outside the chord")

		require.Len(t, result.Chorded, 1)
		assert.Equal(t, "geom:E-21", result.Chorded[0].Edge)
		assert.Equal(t, []string{"arc-centre", "arc-through"}, result.Chorded[0].Predicates)

		assert.Nil(t, result.Chord, "nothing bent, because nothing read the bend")
		assert.Contains(t, stderr, "geom:E-21")
	})

	t.Run("refuses to overlay the frontage until a chord tolerance names how closely", func(t *testing.T) {
		result, stderr := curvedFit(t, exitCheck, append(curving, "site:S-31")...)

		assert.False(t, result.Sited, "no verdict, rather than one decided against a shape nobody chose")
		assert.Contains(t, stderr, "no chord tolerance named")
	})

	t.Run("decides against the frontage where the whole vocabulary is named", func(t *testing.T) {
		result, _ := curvedFit(t, exitSuccess,
			append(curving, "--chord", "chord-deviation", "site:S-31")...)

		require.True(t, result.Sited)
		assert.Equal(t, "fits", result.Verdict, "the pavilion is inside the arc the chord cut off")

		require.NotNil(t, result.Chord)
		assert.Equal(t, "chord-deviation", result.Chord.Name)

		require.NotNil(t, result.Deviation)
		assert.Positive(t, result.Deviation.Value)
		assert.LessOrEqual(t, result.Deviation.Value, result.Chord.Value)
		assert.Equal(t, "m", result.Deviation.Unit)

		assert.Empty(t, result.Chorded)
	})

	t.Run("says nothing about curves for a model which claims none", func(t *testing.T) {
		result, _ := fitted(t, exitSuccess, "site:S-103")

		assert.Empty(t, result.Chorded)
		assert.Nil(t, result.Chord)
		assert.Nil(t, result.Deviation)
	})
}

func TestRunSiteRefusesHalfTheArcVocabulary(t *testing.T) {
	root := tree(t, curved())

	_, stderr := invoke(t, exitUsage, root, siting("--arc-through", "arc-through", "site:S-31")...)

	assert.Contains(t, stderr, "--arc-centre")
}

// setBack is siting with the envelope's setbacks taken off it: the question a
// plot is usually asked, which is not whether a room fits on it but whether it
// fits what the plot allows.
func setBack(args ...string) []string {
	return siting(append([]string{"--setback", "setback"}, args...)...)
}

// TestRunSiteInsideWhatTheSetbacksLeave is its own function because the object
// it asserts about carries fields a fit against the outline does not.
func TestRunSiteInsideWhatTheSetbacksLeave(t *testing.T) {
	root := tree(t, model())

	stdout, _ := invoke(t, exitSuccess, root, setBack("site:S-103")...)
	result := listed[siteResult](t, stdout)

	require.True(t, result.Sited)

	// Three metres inside the plot, and two metres into the five the consent
	// keeps clear of the road: inside the outline and outside what it allows.
	assert.Equal(t, "does-not-fit", result.Verdict)
	assert.True(t, result.Decided)
	require.NotNil(t, result.Clearance)
	assert.InDelta(t, -2.0, result.Clearance.Actual, 0.001)
	assert.True(t, result.Carried, "the room is on the site grid and the plot on the building's")

	// The envelope is what the setbacks leave, and the plot it was derived from
	// comes back beside it: twenty by twelve, less five, three and two and two.
	require.NotNil(t, result.Parcel)
	require.NotNil(t, result.Envelope)
	assert.InDelta(t, 240.0, result.Parcel.Area, 0.25)
	assert.InDelta(t, 64.0, result.Envelope.Area, 0.25)
	assert.False(t, result.Envelope.Empty)
	assert.NotEmpty(t, result.Parcel.Boundary, "the plot is read from the model, so its runs name their edges")

	// Which setbacks were applied, each off the edge which claims it.
	require.Len(t, result.Setbacks, 4)
	for i, expected := range []struct {
		edge     string
		distance float64
	}{
		{"geom:E-11", 5}, {"geom:E-12", 2}, {"geom:E-13", 3}, {"geom:E-14", 2},
	} {
		assert.Equal(t, expected.edge, result.Setbacks[i].Edge)
		assert.Equal(t, expected.distance, result.Setbacks[i].Distance)
		assert.Contains(t, result.Setbacks[i].Source, "Planning consent PC-2026-014")
	}

	// One budget, over the setback claims as well as both sets of corners and
	// the georeference.
	require.NotNil(t, result.Budget)
	var setbackTerms int
	for _, term := range result.Budget.Terms {
		if strings.HasPrefix(term.Name, "the setback of ") {
			setbackTerms++
		}
	}
	assert.Equal(t, 4, setbackTerms)
	require.NotNil(t, result.Clearance.Uncertainty)
	assert.Equal(t, result.Budget.Combined.Magnitude, result.Clearance.Uncertainty.Magnitude)

	digest, err := dfcad.DigestOf(root)
	require.NoError(t, err)
	assert.Equal(t, digest.String(), result.Digest)
}

// TestRunSiteInsideWhatTheSetbacksLeaveFollowsTheClaim is its own function
// because it asserts about two runs over two trees: moving one setback moves
// the answer and the digest it is keyed by, and nothing else.
func TestRunSiteInsideWhatTheSetbacksLeaveFollowsTheClaim(t *testing.T) {
	stdout, _ := invoke(t, exitSuccess, tree(t, model()), setBack("site:S-103")...)
	before := listed[siteResult](t, stdout)

	moved := model()
	moved["entities/geometry.dfc"] = strings.Replace(
		moved["entities/geometry.dfc"], "(value 5.0 m)", "(value 4.0 m)", 1)

	stdout, _ = invoke(t, exitSuccess, tree(t, moved), setBack("site:S-103")...)
	after := listed[siteResult](t, stdout)

	assert.NotEqual(t, before.Digest, after.Digest)
	require.NotNil(t, after.Clearance)
	assert.InDelta(t, -1.0, after.Clearance.Actual, 0.001, "a metre less frontage is a metre less deficit")
	assert.InDelta(t, before.Parcel.Area, after.Parcel.Area, 0.25, "and the plot itself did not move")

	// The same tree asked twice is the same bytes, which is what makes the
	// digest a key rather than a label.
	root := tree(t, model())
	first, _ := invoke(t, exitSuccess, root, setBack("site:S-103")...)
	again, _ := invoke(t, exitSuccess, root, setBack("site:S-103")...)
	assert.Equal(t, first, again)
	assert.Equal(t, before.Digest, listed[siteResult](t, again).Digest)
}

// TestRunSiteWhenTheSetbacksLeaveNothing is its own function because the answer
// carries no clearance, which is a different set of assertions from a fit with
// one.
func TestRunSiteWhenTheSetbacksLeaveNothing(t *testing.T) {
	consumed := model()
	consumed["entities/geometry.dfc"] = strings.Replace(
		consumed["entities/geometry.dfc"], "(value 5.0 m)", "(value 10.0 m)", 1)

	stdout, stderr := invoke(t, exitSuccess, tree(t, consumed), setBack("--format", "human", "site:S-103")...)
	result := listed[siteResult](t, stdout)

	assert.True(t, result.Sited, "nothing fits an empty region, which is an answer")
	assert.Equal(t, "does-not-fit", result.Verdict)
	assert.True(t, result.Decided)

	require.NotNil(t, result.Envelope)
	assert.True(t, result.Envelope.Empty, "the answer says the region was empty")
	assert.Nil(t, result.Clearance, "there is no boundary to measure a clearance to")
	assert.Len(t, result.Setbacks, 4)

	assert.Contains(t, stderr, "leave nothing buildable")
	assert.Contains(t, stderr, "site:S-103 in what the setbacks of site:P-01 leave buildable: does-not-fit")
}

// TestRunSiteInsideWhatTheSetbacksLeaveRefusals is its own function because
// every case in it comes back with no answer at all.
func TestRunSiteInsideWhatTheSetbacksLeaveRefusals(t *testing.T) {
	testCases := []struct {
		name             string
		edit             func(map[string]string)
		args             []string
		expectedInStderr []string
	}{
		{
			name: "names the edge a setback was needed for rather than reading the silence as nought",
			edit: func(files map[string]string) {
				files["entities/geometry.dfc"] = strings.Replace(
					files["entities/geometry.dfc"], `(setback
    (value 3.0 m)
    (source "Planning consent PC-2026-014, condition 3")
    (method method:statutory-instrument)
    (accuracy (independent 0.01 m))
    (date "2026-04-02"))`, "", 1)
			},
			args:             setBack("site:S-103"),
			expectedInStderr: []string{"geom:E-13", "found none on"},
		},
		{
			name:             "names every edge a predicate nobody claims leaves unset",
			edit:             func(map[string]string) {},
			args:             siting("--setback", "frontage", "site:S-103"),
			expectedInStderr: []string{"geom:E-11", "geom:E-12", "geom:E-13", "geom:E-14"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			files := model()
			testCase.edit(files)

			stdout, stderr := invoke(t, exitCheck, tree(t, files), testCase.args...)

			for _, expected := range testCase.expectedInStderr {
				assert.Contains(t, stderr, expected)
			}

			result := listed[siteResult](t, stdout)
			assert.False(t, result.Sited)
			assert.Empty(t, result.Verdict)
			assert.Nil(t, result.Clearance)
			assert.Empty(t, result.Setbacks)
		})
	}
}

// TestRunSiteWithoutSetbacksWritesNoSetbackFields is its own function because it
// asserts about the bytes: a run which named no setback predicate is the run it
// always was, and a key it never wrote is not one it starts writing.
func TestRunSiteWithoutSetbacksWritesNoSetbackFields(t *testing.T) {
	for _, args := range [][]string{
		siting("site:S-103"),
		siting("--clearance", "1", "site:S-103"),
	} {
		stdout, _ := invoke(t, exitSuccess, tree(t, model()), args...)

		assert.NotContains(t, stdout, `"setbacks"`)
		assert.NotContains(t, stdout, `"parcel"`)
	}
}
