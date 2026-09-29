// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// rerouted is the batch which reroutes the store's outline through a corner a
// metre beyond its north side: a vertex and the two edges to it written, and the
// ring restated through them in place of the side they replace.
const rerouted = `{"operations": [
  {"op": "add-vertex", "id": "geom:V-30", "frame": "frame:building", "file": "model.dfc", "predicate": "position",
   "claim": {"value": "11.0 3.0 0.0", "unit": "m", "source": "Interior control set IC-01, Acme Surveys",
             "method": "method:total-station", "accuracy": ["independent 0.004 m"], "date": "2026-02-18"}},
  {"op": "add-edge", "id": "geom:E-30", "frame": "frame:building", "file": "model.dfc", "start": "geom:V-11", "end": "geom:V-30"},
  {"op": "add-edge", "id": "geom:E-31", "frame": "frame:building", "file": "model.dfc", "start": "geom:V-30", "end": "geom:V-12"},
  {"op": "set-edges", "id": "geom:L-13", "edges": ["geom:E-13", "geom:E-14", "geom:E-30", "geom:E-31", "geom:E-16"]}
]}`

// unclosing is the batch which leaves the store's north side out of its ring,
// the mutation a harness proving boundary-loops-close can fail writes.
const unclosing = `{"operations": [
  {"op": "set-edges", "id": "geom:L-13", "edges": ["geom:E-13", "geom:E-14", "geom:E-15"]}
]}`

// ringOf is the edges of the loop id in the model beneath root, requiring the
// model to load and to hold it.
func ringOf(t *testing.T, root string, id dfcad.ID) []dfcad.ID {
	t.Helper()

	graph, diags := dfcad.LoadGraph(root)
	require.Empty(t, diags)

	loop, ok := graph.Topology().Loop(id)
	require.True(t, ok, "the model holds %s", id)

	return loop.Edges()
}

// storeArea is what `dfcad measure` answers for the store's area in the model
// beneath root.
func storeArea(t *testing.T, root string) float64 {
	t.Helper()

	stdout, _ := invoke(t, exitSuccess, root,
		"measure", "--position", "position", "--tolerance", "boundary-closure", "site:S-901")
	result := listed[measureResult](t, stdout)
	require.NotNil(t, result.Area, "the store encloses an area")

	return result.Area.Value
}

// violatesClosure reports whether a check of the model beneath root reports
// boundary-loops-close failing on the store's outline, requiring the exit code
// given.
func violatesClosure(t *testing.T, expectedCode int, root string, args ...string) bool {
	t.Helper()

	stdout, _ := invoke(t, expectedCode, root, append([]string{"check"}, args...)...)
	result := listed[checkResult](t, stdout)

	return slices.ContainsFunc(result.Violations, func(violation dfcad.Violation) bool {
		return violation.Instance == "geom:L-13" && violation.Check == "boundary-loops-close"
	})
}

func TestRunSetEdges(t *testing.T) {
	testCases := []struct {
		name  string
		edges []dfcad.ID
	}{
		{
			name:  "restates a ring walked from another of its edges",
			edges: []dfcad.ID{"geom:E-14", "geom:E-15", "geom:E-16", "geom:E-13"},
		},
		{
			name:  "restates a ring walked the other way round, never sorting it",
			edges: []dfcad.ID{"geom:E-16", "geom:E-15", "geom:E-14", "geom:E-13"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, satisfiedFixture)

			args := []string{"set-edges"}
			for _, edge := range testCase.edges {
				args = append(args, "--edge", string(edge))
			}
			args = append(args, "geom:L-13")

			stdout, _ := invoke(t, exitSuccess, root, args...)
			result := listed[writeResult](t, stdout)

			assert.Equal(t, []string{"model.dfc"}, files(t, root, result.Commit))
			assert.Equal(t, []string{"modified loop geom:L-13"}, effects(result))

			assert.Equal(t, testCase.edges, ringOf(t, root, "geom:L-13"))
			assert.InDelta(t, 4.0, storeArea(t, root), 1e-9, "the same ring encloses the same room")
		})
	}
}

// TestRunSetEdgesReroutesAnOutline is the change the command exists for: the
// store's outline moved through a corner the same batch writes, which the room
// bounded by it measures with no edit of its own.
func TestRunSetEdgesReroutesAnOutline(t *testing.T) {
	root := copied(t, satisfiedFixture)
	assert.InDelta(t, 4.0, storeArea(t, root), 1e-9, "the store is two metres square")

	invoke(t, exitSuccess, root, "apply", operationFile(t, root, rerouted))

	assert.Equal(t,
		[]dfcad.ID{"geom:E-13", "geom:E-14", "geom:E-30", "geom:E-31", "geom:E-16"},
		ringOf(t, root, "geom:L-13"),
	)

	// The square and the triangle the new corner adds above its north side.
	assert.InDelta(t, 5.0, storeArea(t, root), 1e-9)
}

// TestRunSetEdgesWritesARingWhichDoesNotClose checks that closure is judged by
// check and not by the write, as it is for add-loop: the ring is written, exit 0,
// and check over the result reports the loop's boundary-loops-close failing.
func TestRunSetEdgesWritesARingWhichDoesNotClose(t *testing.T) {
	root := copied(t, satisfiedFixture)
	require.False(t, violatesClosure(t, exitSuccess, root), "the store closes before the change")

	invoke(t, exitSuccess, root,
		"set-edges", "--edge", "geom:E-13", "--edge", "geom:E-14", "--edge", "geom:E-15", "geom:L-13")

	assert.Equal(t, []dfcad.ID{"geom:E-13", "geom:E-14", "geom:E-15"}, ringOf(t, root, "geom:L-13"))
	assert.True(t, violatesClosure(t, exitCheck, root), "check reports the ring which does not close")
}

// TestRunSetEdgesWritesARingWhichDoesNotCloseUnderAssume is the same mutation
// asked of check without writing it, which is how a harness proves the check can
// fail without touching the model.
func TestRunSetEdgesWritesARingWhichDoesNotCloseUnderAssume(t *testing.T) {
	root := copied(t, satisfiedFixture)
	path := operationFile(t, root, unclosing)
	before := contents(t, root)

	assert.True(t, violatesClosure(t, exitCheck, root, "--assume", path),
		"check over the assumed batch reports the ring which does not close")
	assert.Equal(t, before, contents(t, root), "an assumed batch writes nothing")
	assert.False(t, violatesClosure(t, exitSuccess, root), "the model on disk still closes")
}

// TestRunSetEdgesRefusesTheInvocation checks that what is wrong with the
// invocation is a usage error, exit 3, with nothing on stdout and nothing
// written. Which errors the engine gives is walked in the engine's own tests;
// what is checked here is what only this layer decides.
func TestRunSetEdgesRefusesTheInvocation(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedMentions []string
	}{
		{
			name:             "refuses an id nothing holds, naming the nearest",
			args:             []string{"set-edges", "--edge", "geom:E-13", "geom:L-I3"},
			expectedMentions: []string{"geom:L-I3", "geom:L-13"},
		},
		{
			name:             "refuses an id naming something which is not a loop",
			args:             []string{"set-edges", "--edge", "geom:E-13", "geom:E-14"},
			expectedMentions: []string{"geom:E-14", "edge", "loop"},
		},
		{
			name:             "refuses an edge nothing holds",
			args:             []string{"set-edges", "--edge", "geom:E-13", "--edge", "geom:E-99", "geom:L-13"},
			expectedMentions: []string{"geom:E-99"},
		},
		{
			name:             "refuses an edge id naming a vertex",
			args:             []string{"set-edges", "--edge", "geom:E-13", "--edge", "geom:V-11", "geom:L-13"},
			expectedMentions: []string{"geom:V-11", "vertex", "edge"},
		},
		{
			name:             "refuses no --edge at all",
			args:             []string{"set-edges", "geom:L-13"},
			expectedMentions: []string{"edges", "Usage"},
		},
		{
			name:             "refuses an invocation naming no loop",
			args:             []string{"set-edges", "--edge", "geom:E-13"},
			expectedMentions: []string{"Usage"},
		},
		{
			name:             "refuses an edge which is not an id at all",
			args:             []string{"set-edges", "--edge", "E-13", "geom:L-13"},
			expectedMentions: []string{"E-13"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, satisfiedFixture)
			before := contents(t, root)

			stdout, stderr := invoke(t, exitUsage, root, testCase.args...)

			assert.Empty(t, stdout)
			assert.Equal(t, before, contents(t, root), "a refused change writes nothing")

			for _, mention := range testCase.expectedMentions {
				assert.Contains(t, stderr, mention)
			}
		})
	}
}

// TestRunSetEdgesIsRefusedByTheModelItWouldProduce checks that a ring naming an
// edge in another frame is refused by the load of the result — exit 2, the
// refusal on stdout — with the diagnostic the same ring typed into a file gets
// (specification sections 6.4 and 7.5.1).
func TestRunSetEdgesIsRefusedByTheModelItWouldProduce(t *testing.T) {
	root := copied(t, satisfiedFixture)
	path := operationFile(t, root, `{"operations": [
  {"op": "add-vertex", "id": "geom:V-40", "frame": "frame:annex", "file": "model.dfc"},
  {"op": "add-vertex", "id": "geom:V-41", "frame": "frame:annex", "file": "model.dfc"},
  {"op": "add-edge", "id": "geom:E-40", "frame": "frame:annex", "file": "model.dfc", "start": "geom:V-40", "end": "geom:V-41"},
  {"op": "set-edges", "id": "geom:L-13", "edges": ["geom:E-13", "geom:E-14", "geom:E-40", "geom:E-16"]}
]}`)
	before := contents(t, root)

	stdout, stderr := invoke(t, exitLoad, root, "apply", path)

	assert.Equal(t, true, object(t, stdout)["refused"])
	assert.Equal(t, before, contents(t, root), "a refused change writes nothing")
	assert.Contains(t, stderr, "geom:E-40")
	assert.Contains(t, stderr, "frame:annex")
}

// TestHelpSaysClosureIsJudgedByCheck checks that the help says where a ring
// which does not close is reported, since the write does not refuse it.
func TestHelpSaysClosureIsJudgedByCheck(t *testing.T) {
	_, help := invoke(t, exitSuccess, t.TempDir(), "set-edges", "-h")
	assert.Contains(t, help, "dfcad check")
	assert.Contains(t, help, "boundary-loops-close")
}
