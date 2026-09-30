// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// claimedFixture is the checked-in model whose rules require a claim, which is
// the rule an assertion on a vertex nobody has placed fails.
const claimedFixture = "../../testdata/checks/claimed"

// asserted is every assertion bound on one thing of the model beneath root, as
// the entity format writes it without its parentheses.
func asserted(t *testing.T, root string, id dfcad.ID) []string {
	t.Helper()

	graph, diags := dfcad.LoadGraph(root)
	require.Empty(t, diags)

	entity, ok := graph.Entity(id)
	require.True(t, ok, "the model holds %s", id)

	var out []string
	for _, binding := range graph.Assertions(entity) {
		out = append(out, binding.Declared.String())
	}
	return out
}

func TestRunAddAssertion(t *testing.T) {
	testCases := []struct {
		name            string
		fixture         string
		args            []string
		subject         dfcad.ID
		expectedEffects []string
		expected        string
	}{
		{
			name:    "writes a check with its parameters on a loop",
			fixture: satisfiedFixture,
			args: []string{
				"add-assertion",
				"--parameter", "tolerance boundary-closure",
				"--parameter", "position position",
				"geom:L-10", "boundary-loops-close",
			},
			subject:         "geom:L-10",
			expectedEffects: []string{"modified loop geom:L-10"},
			expected:        "boundary-loops-close (position position) (tolerance boundary-closure)",
		},
		{
			name:            "writes a check which takes no parameter on an edge",
			fixture:         satisfiedFixture,
			args:            []string{"add-assertion", "geom:E-01", "edge-backing-resolves"},
			subject:         "geom:E-01",
			expectedEffects: []string{"modified edge geom:E-01"},
			expected:        "edge-backing-resolves",
		},
		{
			name:            "writes a check on a node",
			fixture:         claimedFixture,
			args:            []string{"add-assertion", "--parameter", "predicate width", "site:S-104", "required-claim"},
			subject:         "site:S-104",
			expectedEffects: []string{"modified node site:S-104"},
			expected:        "required-claim (predicate width)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, testCase.fixture)

			stdout, _ := invoke(t, exitSuccess, root, testCase.args...)
			result := listed[writeResult](t, stdout)

			assert.Equal(t, "add-assertion", result.Command)
			assert.False(t, result.DryRun)
			assert.Equal(t, testCase.expectedEffects, spelledEffects(result.Effects(), true))
			assert.Contains(t, asserted(t, root, testCase.subject), testCase.expected)
		})
	}
}

// TestRunAddAssertionWritesARuleCheckRuns checks the assertion as the gate reads
// it: once written, `check` runs it on the thing it was written on, and says
// whether the thing satisfies it.
func TestRunAddAssertionWritesARuleCheckRuns(t *testing.T) {
	root := copied(t, satisfiedFixture)

	invoke(t, exitSuccess, root,
		"add-assertion",
		"--parameter", "tolerance boundary-closure",
		"--parameter", "position position",
		"geom:L-10", "boundary-loops-close",
	)

	stdout, _ := invoke(t, exitSuccess, root, "check", "--subject", "geom:L-10", "--list")
	listing := listed[checkResult](t, stdout)

	assert.True(t, slices.ContainsFunc(rules(listing), func(rule string) bool {
		return strings.HasPrefix(rule, "geom:L-10 boundary-loops-close")
	}), "the rule is one check would run: %v", rules(listing))

	stdout, _ = invoke(t, exitSuccess, root, "check", "--subject", "geom:L-10")
	assert.Empty(t, listed[checkResult](t, stdout).Violations)
}

// TestApplyWritesAnAssertionOnWhatTheSameBatchWrote checks the batch half of the
// command: a vertex added without a position and a rule requiring one are one
// batch, and the rule then fails where it was written.
func TestApplyWritesAnAssertionOnWhatTheSameBatchWrote(t *testing.T) {
	root := copied(t, claimedFixture)

	stdout, _ := invoke(t, exitSuccess, root, "apply", operationFile(t, root, `{"operations": [
		{"op": "add-vertex", "id": "geom:V-05", "frame": "frame:building", "file": "model.dfc"},
		{"op": "add-assertion", "subject": "geom:V-05", "check": "required-claim",
		 "parameters": ["predicate position"]}
	]}`))

	result := listed[applyResult](t, stdout)
	require.Len(t, result.Operations, 2)
	assert.Equal(t, "add-assertion", result.Operations[1].Op)

	assert.Contains(t, asserted(t, root, "geom:V-05"), "required-claim (predicate position)")

	stdout, _ = invoke(t, exitCheck, root, "check", "--subject", "geom:V-05")
	failed := listed[checkResult](t, stdout)

	require.Len(t, failed.Violations, 1)
	assert.Equal(t, "required-claim", failed.Violations[0].Check)
	assert.Equal(t, dfcad.ID("geom:V-05"), failed.Violations[0].Instance)
}

// TestRunAddAssertionRefusesTheAssertion checks what is wrong with the
// assertion itself: each is a usage error answered before anything is written.
func TestRunAddAssertionRefusesTheAssertion(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "a subject nothing holds",
			args:     []string{"add-assertion", "geom:E-99", "edge-backing-resolves"},
			expected: "geom:E-99",
		},
		{
			name:     "a frame, which carries no assertion",
			args:     []string{"add-assertion", "--parameter", "predicate position", "frame:building", "required-claim"},
			expected: "a frame",
		},
		{
			name:     "a check the engine does not register, naming the ones it does",
			args:     []string{"add-assertion", "geom:L-10", "loops-close"},
			expected: "edge-backing-resolves",
		},
		{
			name: "a parameter the check does not take",
			args: []string{
				"add-assertion",
				"--parameter", "tolerance boundary-closure", "--parameter", "limit frame-budget",
				"geom:L-10", "boundary-loops-close",
			},
			expected: "(limit ...)",
		},
		{
			name: "a parameter the check requires and which is missing",
			args: []string{
				"add-assertion",
				"--parameter", "tolerance boundary-closure", "--parameter", "position position",
				"site:S-101", "stays-clear-of-zone",
			},
			expected: "(zone ...)",
		},
		{
			name:     "a value not of the sort the check declares",
			args:     []string{"add-assertion", "--parameter", "tolerance 0.005", "geom:L-10", "boundary-loops-close"},
			expected: "a declared tolerance name",
		},
		{
			name:     "text which is not one parameter",
			args:     []string{"add-assertion", "--parameter", "tolerance a) (b", "geom:L-10", "boundary-loops-close"},
			expected: "tolerance a) (b",
		},
		{
			name:     "a subject with no check to apply",
			args:     []string{"add-assertion", "geom:L-10"},
			expected: ErrMissingCheck.Error(),
		},
		{
			name:     "a third argument",
			args:     []string{"add-assertion", "geom:E-01", "edge-backing-resolves", "again"},
			expected: "again",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, satisfiedFixture)
			before := contents(t, root)

			stdout, stderr := invoke(t, exitUsage, root, testCase.args...)

			assert.Empty(t, stdout)
			assert.Contains(t, stderr, testCase.expected)
			assert.Equal(t, before, contents(t, root), "a refused change writes nothing")
		})
	}
}

// TestRunAddAssertionIsRefusedByTheModelItWouldProduce checks what is wrong with
// the assertion in this model: each comes back as the diagnostic a load of the
// result would have raised, with the load failure exit code.
func TestRunAddAssertionIsRefusedByTheModelItWouldProduce(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		batch    string
		expected string
	}{
		{
			name:     "a check which cannot examine the subject's form",
			args:     []string{"add-assertion", "site:S-101", "edge-backing-resolves"},
			batch:    `{"operations": [{"op": "add-assertion", "subject": "site:S-101", "check": "edge-backing-resolves"}]}`,
			expected: "expected an assertion naming a check which applies to a node, found edge-backing-resolves",
		},
		{
			name: "a check which cannot examine the subject's geometry",
			args: []string{
				"add-assertion",
				"--parameter", "zone site:Z-90",
				"--parameter", "tolerance boundary-closure",
				"--parameter", "position position",
				"site:A-01", "stays-clear-of-zone",
			},
			batch: `{"operations": [{"op": "add-assertion", "subject": "site:A-01", "check": "stays-clear-of-zone",
				"parameters": ["zone site:Z-90", "tolerance boundary-closure", "position position"]}]}`,
			expected: "applies to the geometry site:A-01 has",
		},
		{
			name: "a parameter naming an id nothing holds",
			args: []string{
				"add-assertion",
				"--parameter", "zone site:Z-99",
				"--parameter", "tolerance boundary-closure",
				"--parameter", "position position",
				"site:S-101", "stays-clear-of-zone",
			},
			batch: `{"operations": [{"op": "add-assertion", "subject": "site:S-101", "check": "stays-clear-of-zone",
				"parameters": ["zone site:Z-99", "tolerance boundary-closure", "position position"]}]}`,
			expected: "found site:Z-99, which nothing answers to",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, satisfiedFixture)
			before := contents(t, root)

			stdout, stderr := invoke(t, exitLoad, root, testCase.args...)

			refusedObject(t, stdout, "add-assertion")
			assertRoundTripsOver(t, stdout, stderr, proposed(t, root, testCase.batch))
			assert.Equal(t, before, contents(t, root), "a refused change writes nothing")
			assert.Contains(t, stderr, testCase.expected)
		})
	}
}
