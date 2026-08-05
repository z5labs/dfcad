// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			assert.Empty(t, result.Verdict)
			assert.Nil(t, result.Clearance)
		})
	}
}
