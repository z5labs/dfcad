// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// backedFixture is the model set-backing changes: two rooms sharing a partition,
// with a virtual edge, an edge backed by one element and an edge backed by two.
const backedFixture = "../../testdata/boundary/backed"

// backedEdge is one edge of the model beneath root and its classification,
// requiring the model to load and to hold it.
func backedEdge(t *testing.T, root string, id dfcad.ID) (*dfcad.Edge, dfcad.Classification) {
	t.Helper()

	graph, diags := dfcad.LoadGraph(root)
	require.Empty(t, diags)

	edge, ok := graph.Topology().Edge(id)
	require.True(t, ok, "the model holds %s", id)

	return edge, graph.Classified(edge).Classification()
}

func TestRunSetBacking(t *testing.T) {
	testCases := []struct {
		name                   string
		args                   []string
		edge                   dfcad.ID
		expectedBacking        []dfcad.ID
		expectedClassification dfcad.Classification
	}{
		{
			name:                   "backs a virtual edge with the wall built after it was drawn",
			args:                   []string{"set-backing", "--backed-by", "site:W-14", "geom:E-01"},
			edge:                   "geom:E-01",
			expectedBacking:        []dfcad.ID{"site:W-14"},
			expectedClassification: dfcad.ClassificationPhysical,
		},
		{
			name:                   "makes the edge of a demolished wall virtual",
			args:                   []string{"set-backing", "--virtual", "geom:E-05"},
			edge:                   "geom:E-05",
			expectedClassification: dfcad.ClassificationVirtual,
		},
		{
			name:                   "replaces the wall an edge is backed by with another",
			args:                   []string{"set-backing", "--backed-by", "site:W-15", "geom:E-05"},
			edge:                   "geom:E-05",
			expectedBacking:        []dfcad.ID{"site:W-15"},
			expectedClassification: dfcad.ClassificationPhysical,
		},
		{
			name: "states every element when more than one realises the edge",
			args: []string{
				"set-backing", "--backed-by", "site:W-16", "--backed-by", "site:W-14", "geom:E-05",
			},
			edge:                   "geom:E-05",
			expectedBacking:        []dfcad.ID{"site:W-14", "site:W-16"},
			expectedClassification: dfcad.ClassificationPhysical,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, backedFixture)

			stdout, _ := invoke(t, exitSuccess, root, testCase.args...)
			result := listed[writeResult](t, stdout)

			assert.Equal(t, []string{"model.dfc"}, files(t, root, result.Commit))
			assert.Equal(t, []string{"modified edge " + string(testCase.edge)}, effects(result))

			edge, classification := backedEdge(t, root, testCase.edge)
			assert.ElementsMatch(t, testCase.expectedBacking, edge.BackedBy())
			assert.Equal(t, testCase.expectedClassification, classification)
		})
	}
}

// TestRunSetBackingRefusesTheInvocation checks that what is wrong with the
// invocation is a usage error, exit 3, with nothing on stdout and nothing
// written. Which errors the engine gives is walked in the engine's own tests;
// what is checked here is what only this layer decides.
func TestRunSetBackingRefusesTheInvocation(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedMentions []string
	}{
		{
			name:             "refuses an id nothing holds, naming the nearest",
			args:             []string{"set-backing", "--virtual", "geom:E-O5"},
			expectedMentions: []string{"geom:E-O5", "geom:E-05"},
		},
		{
			name:             "refuses an id naming something which is not an edge",
			args:             []string{"set-backing", "--virtual", "geom:V-01"},
			expectedMentions: []string{"geom:V-01", "vertex", "edge"},
		},
		{
			name:             "refuses both --backed-by and --virtual",
			args:             []string{"set-backing", "--virtual", "--backed-by", "site:W-14", "geom:E-05"},
			expectedMentions: []string{"virtual", "site:W-14", "Usage"},
		},
		{
			name:             "refuses neither --backed-by nor --virtual",
			args:             []string{"set-backing", "geom:E-05"},
			expectedMentions: []string{"virtual", "Usage"},
		},
		{
			name:             "refuses an invocation naming no edge",
			args:             []string{"set-backing", "--virtual"},
			expectedMentions: []string{"Usage"},
		},
		{
			name:             "refuses an element which is not an id at all",
			args:             []string{"set-backing", "--backed-by", "W-14", "geom:E-05"},
			expectedMentions: []string{"W-14"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, backedFixture)
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

// TestRunSetBackingIsRefusedByTheModelItWouldProduce checks that a backing
// naming no Element is refused by the load of the result — exit 2, the refusal
// on stdout — with the diagnostic the same mistake typed into a file gets
// (specification section 6.3).
func TestRunSetBackingIsRefusedByTheModelItWouldProduce(t *testing.T) {
	testCases := []struct {
		name             string
		backing          string
		expectedMentions []string
	}{
		{
			name:             "an element nothing holds",
			backing:          "site:W-99",
			expectedMentions: []string{"site:W-99", "names no node"},
		},
		{
			name:             "a node which is not an Element",
			backing:          "site:S-101",
			expectedMentions: []string{"site:S-101", "Element"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, backedFixture)
			before := contents(t, root)

			stdout, stderr := invoke(t, exitLoad, root, "set-backing", "--backed-by", testCase.backing, "geom:E-01")

			assert.Equal(t, true, object(t, stdout)["refused"])
			assert.Equal(t, before, contents(t, root), "a refused change writes nothing")

			for _, mention := range testCase.expectedMentions {
				assert.Contains(t, stderr, mention)
			}
		})
	}
}

// TestRunSetBackingThenRetireDemolishesAWall is the demolition the command
// exists for, as its help describes it: the edge stops naming the wall, and the
// wall is retired as a second change.
func TestRunSetBackingThenRetireDemolishesAWall(t *testing.T) {
	root := copied(t, backedFixture)

	// Before the edge stops naming it, the wall cannot be retired without a
	// replacement.
	invoke(t, exitUsage, root, "retire", "--reason", "Partition demolished", "site:W-16")

	invoke(t, exitSuccess, root, "set-backing", "--virtual", "geom:E-05")
	invoke(t, exitSuccess, root, "retire", "--reason", "Partition demolished", "site:W-16")

	graph, diags := dfcad.LoadGraph(root)
	require.Empty(t, diags)

	wall, ok := graph.Node("site:W-16")
	require.True(t, ok)

	retirement, ok := wall.Retirement()
	require.True(t, ok)
	assert.Equal(t, "Partition demolished", retirement.Reason())

	_, classification := backedEdge(t, root, "geom:E-05")
	assert.Equal(t, dfcad.ClassificationVirtual, classification)
}

// TestHelpNamesSetBackingAsHowTheWallIsAddedLater checks that the help which
// promises an edge's backing can change says which command changes it, and
// that set-backing's own help says how a demolished element is retired.
func TestHelpNamesSetBackingAsHowTheWallIsAddedLater(t *testing.T) {
	_, addEdge := invoke(t, exitSuccess, t.TempDir(), "add-edge", "-h")
	assert.Contains(t, addEdge, "dfcad set-backing")

	_, setBacking := invoke(t, exitSuccess, t.TempDir(), "set-backing", "-h")
	assert.Contains(t, setBacking, "set-backing --virtual")
	assert.Contains(t, setBacking, "dfcad retire")
	assert.Contains(t, setBacking, "as the change found it", "the help says why the retirement is a second change")
}
