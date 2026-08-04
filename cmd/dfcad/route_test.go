// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routed runs route against the fixture model and returns the result, requiring
// the run to have succeeded.
func routed(t *testing.T, args ...string) routeResult {
	t.Helper()

	t.Chdir(tree(t, model()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run(append([]string{"route"}, args...), &stdout, &stderr), stderr.String())

	return listed[routeResult](t, stdout.String())
}

func TestRunRoute(t *testing.T) {
	testCases := []struct {
		name        string
		args        []string
		expected    routedDestination
		expectedFor routedSubject
	}{
		{
			name: "reports the file a matched rule chose, and that the model already holds it",
			args: []string{"--kind", "Space", "--type", "MeetingRoom", "site:S-104"},
			expected: routedDestination{
				Path:   "entities/site.dfc",
				Rule:   "rooms",
				Exists: true,
			},
			expectedFor: routedSubject{ID: "site:S-104", Kind: "Space", Type: "MeetingRoom"},
		},
		{
			name: "reports a destination the model does not hold yet, which the write will create",
			args: []string{"--kind", "Zone", "--type", "Campus", "site:C-02"},
			expected: routedDestination{
				Path:   "entities/campuses.dfc",
				Rule:   "campuses",
				Exists: false,
			},
			expectedFor: routedSubject{ID: "site:C-02", Kind: "Zone", Type: "Campus"},
		},
		{
			name: "takes an explicit --file over the rules, naming no rule for it",
			args: []string{"--kind", "Space", "--type", "MeetingRoom", "--file", "entities/annexe.dfc", "site:S-104"},
			expected: routedDestination{
				Path:       "entities/annexe.dfc",
				Overridden: true,
				Exists:     false,
			},
			expectedFor: routedSubject{ID: "site:S-104", Kind: "Space", Type: "MeetingRoom"},
		},
		{
			name: "overrides to a file the model already holds",
			args: []string{"--kind", "Zone", "--type", "Campus", "--file", "entities/site.dfc", "site:C-02"},
			expected: routedDestination{
				Path:       "entities/site.dfc",
				Overridden: true,
				Exists:     true,
			},
			expectedFor: routedSubject{ID: "site:C-02", Kind: "Zone", Type: "Campus"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := routed(t, testCase.args...)

			assert.Equal(t, testCase.expected, result.Destination)
			assert.Equal(t, testCase.expectedFor, result.Subject)
		})
	}
}

// TestRunRouteRefusesAnInvocationItCannotAnswer is its own function because it
// asserts about an exit code and a message on stderr rather than about a result:
// a routing that does not decide writes no result object at all.
func TestRunRouteRefusesAnInvocationItCannotAnswer(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedInStderr []string
	}{
		{
			name:             "refuses a node no rule matches, naming it and every rule consulted",
			args:             []string{"--kind", "Interface", "--type", "Fitting", "site:F-01"},
			expectedInStderr: []string{"site:F-01", "buildings", "campuses", "rooms"},
		},
		{
			name:             "refuses a geometric node no rule matches, which has neither axis to match on",
			args:             []string{"geom:V-01"},
			expectedInStderr: []string{"geom:V-01", "no kind and no type"},
		},
		{
			name:             "refuses an invocation which names no node",
			args:             nil,
			expectedInStderr: []string{"no id"},
		},
		{
			name:             "refuses an argument which is not an id",
			args:             []string{"--kind", "Space", "--type", "MeetingRoom", "S-104"},
			expectedInStderr: []string{"S-104", "namespace:local"},
		},
		{
			name:             "refuses a kind which is none of the seven",
			args:             []string{"--kind", "Room", "--type", "MeetingRoom", "site:S-104"},
			expectedInStderr: []string{"Room"},
		},
		{
			name:             "refuses a type the registry does not declare",
			args:             []string{"--kind", "Space", "--type", "Corridor", "site:S-104"},
			expectedInStderr: []string{"Corridor", "list-types"},
		},
		{
			name:             "refuses an override no walk of the model would read back",
			args:             []string{"--kind", "Space", "--type", "MeetingRoom", "--file", "notes.md", "site:S-104"},
			expectedInStderr: []string{"notes.md", "not an entity file"},
		},
		{
			name:             "refuses an override which climbs out of the model root",
			args:             []string{"--kind", "Space", "--type", "MeetingRoom", "--file", "../elsewhere.dfc", "site:S-104"},
			expectedInStderr: []string{"elsewhere.dfc", "not beneath the model root"},
		},
		{
			name:             "refuses more arguments than it takes",
			args:             []string{"site:S-104", "site:S-105"},
			expectedInStderr: []string{"site:S-105"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, model()))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitUsage, run(append([]string{"route"}, testCase.args...), &stdout, &stderr))

			assert.Empty(t, stdout.String(), "a run which produced no result writes no result object")
			for _, expected := range testCase.expectedInStderr {
				assert.Contains(t, stderr.String(), expected)
			}
		})
	}
}

// TestRunRouteRefusesAnAmbiguousRouting is its own function because it needs a
// registry the other tests deliberately do not have: one whose rules overlap.
//
// Overlap is a mistake in the registry rather than in the invocation, and the
// refusal has to say so — a tool which picked the most specific rule, or the
// first one written, would file the node somewhere nothing the author wrote
// points at.
func TestRunRouteRefusesAnAmbiguousRouting(t *testing.T) {
	files := model()
	files["registry-extra.dfc"] = `(route everything (file "entities/misc.dfc"))` + "\n"

	t.Chdir(tree(t, files))

	var stdout, stderr bytes.Buffer
	args := []string{"route", "--kind", "Space", "--type", "MeetingRoom", "site:S-104"}
	require.Equal(t, exitUsage, run(args, &stdout, &stderr))

	assert.Empty(t, stdout.String())

	report := stderr.String()
	assert.Contains(t, report, "site:S-104")
	assert.Contains(t, report, "more than one routing rule")
	assert.Contains(t, report, "everything")
	assert.Contains(t, report, "rooms")
}

// TestRunRouteWritesNothing checks the property the command's whole reason for
// existing rests on: it reports where a node would go and changes nothing, so
// that an author can ask before committing to an answer.
func TestRunRouteWritesNothing(t *testing.T) {
	dir := tree(t, model())
	before := contents(t, dir)

	var stdout, stderr bytes.Buffer
	args := []string{"route", "--root", dir, "--kind", "Zone", "--type", "Campus", "site:C-02"}
	require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

	assert.Equal(t, before, contents(t, dir))
}

// TestRunRouteRendersItsAnswerForAPerson checks that the human format says the
// same thing the result object does, on stderr, without changing stdout.
func TestRunRouteRendersItsAnswerForAPerson(t *testing.T) {
	t.Chdir(tree(t, model()))

	var quiet, quietReport bytes.Buffer
	args := []string{"route", "--kind", "Space", "--type", "MeetingRoom", "site:S-104"}
	require.Equal(t, exitSuccess, run(args, &quiet, &quietReport), quietReport.String())

	var loud, loudReport bytes.Buffer
	require.Equal(t, exitSuccess, run(append(args, "--format", formatHuman), &loud, &loudReport), loudReport.String())

	assert.Equal(t, quiet.String(), loud.String(), "the format never changes stdout")
	assert.Empty(t, quietReport.String())
	assert.Contains(t, loudReport.String(), "site:S-104 -> entities/site.dfc (rule rooms, an existing file)")
}
