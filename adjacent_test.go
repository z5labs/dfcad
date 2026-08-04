// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// neighbour is one result of an adjacency walk flattened for comparison: the
// region reached, how far away it is, and the edges it was reached through.
//
// Ids rather than pointers, for the reason the classification tests use them:
// what is asserted is which things the walk reached, and a fixture loaded twice
// is two pointers to the same model.
type neighbour struct {
	node  ID
	depth int
	via   []ID
}

// bordering collects an adjacency walk, having first required that every result
// was labelled with the relation which produced it.
//
// The label is checked here rather than in a test of its own because it is a
// property of every result: a helper which dropped it while collecting the ids
// would leave each case below asserting the ids of a walk whose meaning it had
// just thrown away.
func bordering(t *testing.T, results iter.Seq[Adjacent]) []neighbour {
	t.Helper()

	var out []neighbour
	for result := range results {
		assert.Equal(t, RelationAdjacency, result.Relation())
		require.NotNil(t, result.Node())

		var via []ID
		for _, edge := range result.Via() {
			via = append(via, edge.ID())
		}

		out = append(out, neighbour{node: result.Node().ID(), depth: result.Depth(), via: via})
	}

	return out
}

// TestBoundariesAdjacent walks one step across shared boundary, which is the
// question "what borders this".
func TestBoundariesAdjacent(t *testing.T) {
	testCases := []struct {
		name     string
		region   ID
		expected []neighbour
	}{
		{
			// Two edges, one region. What is on the other side of a wall and
			// what is on the other side of the doorway through it are the same
			// room, and reporting it twice would be counting the ways in.
			name:     "gives the region on the other side of every edge, once, with the edges it shares",
			region:   "site:S-A",
			expected: []neighbour{{node: "site:S-B", depth: 1, via: []ID{"geom:E-02", "geom:E-03"}}},
		},
		{
			name:   "gives both neighbours of a region between two others, in boundary order",
			region: "site:S-B",
			expected: []neighbour{
				{node: "site:S-C", depth: 1, via: []ID{"geom:E-07"}},
				{node: "site:S-A", depth: 1, via: []ID{"geom:E-03", "geom:E-02"}},
			},
		},
		{
			name:     "gives one neighbour for a region at the end of the row",
			region:   "site:S-C",
			expected: []neighbour{{node: "site:S-B", depth: 1, via: []ID{"geom:E-07"}}},
		},
		{
			name:   "gives nothing for a node with no boundary of its own",
			region: "site:W-01",
		},
	}

	model, boundaries := joinBoundaries(t, "adjacent")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			region, ok := model.nodes.Node(testCase.region)
			require.True(t, ok)

			got := bordering(t, boundaries.Adjacent(region))

			assert.Equal(t, testCase.expected, got)
			assert.NotContains(t, got, testCase.region, "a region is not adjacent to itself")
		})
	}
}

// TestBoundariesAdjacentTo follows adjacency outward, bounded.
//
// Room A and room C are two steps apart. Nothing about the plan says so — they
// share no edge, and the corridor between them is what relates them at all — so
// a walk which reported them as neighbours would be answering from how close two
// outlines look rather than from what the model says.
func TestBoundariesAdjacentTo(t *testing.T) {
	testCases := []struct {
		name     string
		region   ID
		depth    int
		expected []neighbour
	}{
		{
			name:   "gives nothing at a depth of no steps at all",
			region: "site:S-A",
			depth:  0,
		},
		{
			name:     "gives what borders the region at one step",
			region:   "site:S-A",
			depth:    1,
			expected: []neighbour{{node: "site:S-B", depth: 1, via: []ID{"geom:E-02", "geom:E-03"}}},
		},
		{
			name:   "adds what borders that at two",
			region: "site:S-A",
			depth:  2,
			expected: []neighbour{
				{node: "site:S-B", depth: 1, via: []ID{"geom:E-02", "geom:E-03"}},
				{node: "site:S-C", depth: 2, via: []ID{"geom:E-07"}},
			},
		},
		{
			// The row is three rooms long, so a walk with no bound stops where
			// the model does rather than where the flag does.
			name:   "reaches the whole connected row when it is given no bound",
			region: "site:S-A",
			depth:  Unbounded,
			expected: []neighbour{
				{node: "site:S-B", depth: 1, via: []ID{"geom:E-02", "geom:E-03"}},
				{node: "site:S-C", depth: 2, via: []ID{"geom:E-07"}},
			},
		},
		{
			name:   "walks the other way from the far end of the row",
			region: "site:S-C",
			depth:  Unbounded,
			expected: []neighbour{
				{node: "site:S-B", depth: 1, via: []ID{"geom:E-07"}},
				{node: "site:S-A", depth: 2, via: []ID{"geom:E-03", "geom:E-02"}},
			},
		},
	}

	model, boundaries := joinBoundaries(t, "adjacent")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			region, ok := model.nodes.Node(testCase.region)
			require.True(t, ok)

			assert.Equal(t, testCase.expected, bordering(t, boundaries.AdjacentTo(region, testCase.depth)))
		})
	}
}

// TestAdjacencyIsTheSharedEdge is its own function because it asserts on the
// wall rather than on either room: the edge two rooms share is one node reached
// from both sides, which is what makes the relation a fact about the model
// rather than a comparison of two outlines.
func TestAdjacencyIsTheSharedEdge(t *testing.T) {
	model, boundaries := joinBoundaries(t, "adjacent")

	room, ok := model.nodes.Node("site:S-A")
	require.True(t, ok)

	corridor, ok := model.nodes.Node("site:S-B")
	require.True(t, ok)

	partition, ok := model.topology.Edge("geom:E-02")
	require.True(t, ok)

	doorway, ok := model.topology.Edge("geom:E-03")
	require.True(t, ok)

	t.Run("reaches the neighbour through the edges both boundaries reach", func(t *testing.T) {
		var found []Adjacent
		for result := range boundaries.Adjacent(room) {
			found = append(found, result)
		}

		require.Len(t, found, 1)
		assert.Same(t, corridor, found[0].Node())

		// Not merely equal ids: the edges the rest of the model reaches. Two
		// copies of one coordinate would satisfy an equality assertion and would
		// still be two walls.
		require.Len(t, found[0].Via(), 2)
		assert.Same(t, partition, found[0].Via()[0])
		assert.Same(t, doorway, found[0].Via()[1])

		// And the edges are the ones the other side reaches too, which is the
		// whole of what adjacency is.
		assert.Contains(t, edgesOf(boundaries, corridor), partition)
		assert.Contains(t, edgesOf(boundaries, room), partition)
	})

	t.Run("says what separates the neighbour from the region", func(t *testing.T) {
		// One shared edge is a wall and the other is the way through it. The
		// classification comes from what backs each edge, so "these rooms are
		// adjacent" and "a partition is between them" are one answer rather than
		// two which can disagree.
		assert.Equal(t, ClassificationPhysical, boundaries.Classified(partition).Classification())
		assert.Equal(t, ClassificationVirtual, boundaries.Classified(doorway).Classification())

		require.Len(t, boundaries.Classified(partition).Backing(), 1)
		assert.Equal(t, ID("site:W-01"), boundaries.Classified(partition).Backing()[0].ID())
	})

	t.Run("is not adjacency between the elements which back the edges", func(t *testing.T) {
		// The wall is a node of the semantic family like the rooms are, and it
		// has no boundary of its own. What backs an edge is not what borders
		// anything.
		wall, ok := model.nodes.Node("site:W-01")
		require.True(t, ok)

		assert.Empty(t, bordering(t, boundaries.AdjacentTo(wall, Unbounded)))
	})
}

// TestAdjacencyOfAnUnjoinedModel is its own function because it asserts about a
// Boundaries which resolved nothing rather than about a model: every traversal
// works on the zero value, so a caller reporting on a model whose boundaries
// have not been joined reports nothing rather than crashing.
func TestAdjacencyOfAnUnjoinedModel(t *testing.T) {
	model, _ := joinBoundaries(t, "adjacent")

	room, ok := model.nodes.Node("site:S-A")
	require.True(t, ok)

	var none *Boundaries

	assert.Empty(t, bordering(t, none.Adjacent(room)))
	assert.Empty(t, bordering(t, none.AdjacentTo(room, Unbounded)))
	assert.Empty(t, bordering(t, (&Boundaries{}).Adjacent(room)))
	assert.Empty(t, bordering(t, (&Boundaries{}).Adjacent(nil)))
}

// edgesOf is the edges one region's boundary is assembled from.
func edgesOf(boundaries *Boundaries, region *SemanticNode) []*Edge {
	var out []*Edge
	for edge := range boundaries.Edges(region) {
		out = append(out, edge)
	}
	return out
}
