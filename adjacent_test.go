// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"cmp"
	"iter"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// neighbour is one result of an adjacency walk flattened for comparison: the
// region reached, how far away it is, the region it was reached from, and the
// edges it was reached through.
//
// Ids rather than pointers, for the reason the classification tests use them:
// what is asserted is which things the walk reached, and a fixture loaded twice
// is two pointers to the same model.
type neighbour struct {
	node  ID
	depth int
	from  ID
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
		require.NotNil(t, result.From(), "every result names the region it was reached from")

		var via []ID
		for _, edge := range result.Via() {
			via = append(via, edge.ID())
		}

		out = append(out, neighbour{node: result.Node().ID(), depth: result.Depth(), from: result.From().ID(), via: via})
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
			expected: []neighbour{{node: "site:S-B", depth: 1, from: "site:S-A", via: []ID{"geom:E-02", "geom:E-03"}}},
		},
		{
			name:   "gives both neighbours of a region between two others, in boundary order",
			region: "site:S-B",
			expected: []neighbour{
				{node: "site:S-C", depth: 1, from: "site:S-B", via: []ID{"geom:E-07"}},
				{node: "site:S-A", depth: 1, from: "site:S-B", via: []ID{"geom:E-03", "geom:E-02"}},
			},
		},
		{
			name:     "gives one neighbour for a region at the end of the row",
			region:   "site:S-C",
			expected: []neighbour{{node: "site:S-B", depth: 1, from: "site:S-C", via: []ID{"geom:E-07"}}},
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
			expected: []neighbour{{node: "site:S-B", depth: 1, from: "site:S-A", via: []ID{"geom:E-02", "geom:E-03"}}},
		},
		{
			name:   "adds what borders that at two",
			region: "site:S-A",
			depth:  2,
			expected: []neighbour{
				{node: "site:S-B", depth: 1, from: "site:S-A", via: []ID{"geom:E-02", "geom:E-03"}},
				{node: "site:S-C", depth: 2, from: "site:S-B", via: []ID{"geom:E-07"}},
			},
		},
		{
			// The row is three rooms long, so a walk with no bound stops where
			// the model does rather than where the flag does.
			name:   "reaches the whole connected row when it is given no bound",
			region: "site:S-A",
			depth:  Unbounded,
			expected: []neighbour{
				{node: "site:S-B", depth: 1, from: "site:S-A", via: []ID{"geom:E-02", "geom:E-03"}},
				{node: "site:S-C", depth: 2, from: "site:S-B", via: []ID{"geom:E-07"}},
			},
		},
		{
			name:   "walks the other way from the far end of the row",
			region: "site:S-C",
			depth:  Unbounded,
			expected: []neighbour{
				{node: "site:S-B", depth: 1, from: "site:S-C", via: []ID{"geom:E-07"}},
				{node: "site:S-A", depth: 2, from: "site:S-B", via: []ID{"geom:E-03", "geom:E-02"}},
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

// TestBoundariesAdjacentWalk walks adjacency across only the edges a filter
// allows, one filter per case.
//
// Room A and the corridor share a partition and the doorway through it, which is
// drawn as an edge nothing backs; the corridor and room C share only a
// partition. So the doorway is a way through when virtual edges may be crossed,
// and the partitions are when that type may be.
func TestBoundariesAdjacentWalk(t *testing.T) {
	testCases := []struct {
		name     string
		region   ID
		filter   AdjacencyFilter
		expected []neighbour
	}{
		{
			name:   "crosses every shared edge when the filter is empty",
			region: "site:S-A",
			filter: AdjacencyFilter{},
			expected: []neighbour{
				{node: "site:S-B", depth: 1, from: "site:S-A", via: []ID{"geom:E-02", "geom:E-03"}},
				{node: "site:S-C", depth: 2, from: "site:S-B", via: []ID{"geom:E-07"}},
			},
		},
		{
			name:     "crosses the doorway and not the partitions when only virtual edges may be crossed",
			region:   "site:S-A",
			filter:   AdjacencyFilter{CrossVirtual: true},
			expected: []neighbour{{node: "site:S-B", depth: 1, from: "site:S-A", via: []ID{"geom:E-03"}}},
		},
		{
			name:   "crosses the partitions and not the doorway when only partitions may be crossed",
			region: "site:S-A",
			filter: AdjacencyFilter{CrossTypes: []string{"Partition"}},
			expected: []neighbour{
				{node: "site:S-B", depth: 1, from: "site:S-A", via: []ID{"geom:E-02"}},
				{node: "site:S-C", depth: 2, from: "site:S-B", via: []ID{"geom:E-07"}},
			},
		},
		{
			name:   "crosses what either field allows when both are given",
			region: "site:S-A",
			filter: AdjacencyFilter{CrossVirtual: true, CrossTypes: []string{"Partition"}},
			expected: []neighbour{
				{node: "site:S-B", depth: 1, from: "site:S-A", via: []ID{"geom:E-02", "geom:E-03"}},
				{node: "site:S-C", depth: 2, from: "site:S-B", via: []ID{"geom:E-07"}},
			},
		},
		{
			name:   "crosses what any of several types allows",
			region: "site:S-C",
			filter: AdjacencyFilter{CrossTypes: []string{"Doorway", "Partition"}},
			expected: []neighbour{
				{node: "site:S-B", depth: 1, from: "site:S-C", via: []ID{"geom:E-07"}},
				{node: "site:S-A", depth: 2, from: "site:S-B", via: []ID{"geom:E-02"}},
			},
		},
		{
			name:   "reaches nothing when no shared edge is backed by a type it names",
			region: "site:S-A",
			filter: AdjacencyFilter{CrossTypes: []string{"Doorway"}},
		},
		{
			// Room C shares only a partition with the corridor, so a walk which
			// may cross only virtual edges is walled in.
			name:   "does not reach a region which shares only edges it may not cross",
			region: "site:S-C",
			filter: AdjacencyFilter{CrossVirtual: true},
		},
	}

	model, boundaries := joinBoundaries(t, "adjacent")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			region, ok := model.nodes.Node(testCase.region)
			require.True(t, ok)

			got := bordering(t, boundaries.AdjacentWalk(region, Unbounded, testCase.filter))

			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestAdjacentToIsAnUnfilteredWalk is the other half of the table above: with
// nothing given, a walk crosses every shared edge and answers exactly what
// AdjacentTo always has, from every region and at every depth.
func TestAdjacentToIsAnUnfilteredWalk(t *testing.T) {
	model, boundaries := joinBoundaries(t, "adjacent")

	for _, id := range []ID{"site:S-A", "site:S-B", "site:S-C", "site:W-01"} {
		region, ok := model.nodes.Node(id)
		require.True(t, ok)

		for _, depth := range []int{0, 1, 2, Unbounded} {
			assert.Equal(t,
				bordering(t, boundaries.AdjacentTo(region, depth)),
				bordering(t, boundaries.AdjacentWalk(region, depth, AdjacencyFilter{})),
				"%s at %d", id, depth,
			)
		}
	}
}

// TestAdjacencyFilterMatches decides one edge at a time, which is where the
// unresolved edge is reachable: no fixture which joins clean holds one.
func TestAdjacencyFilterMatches(t *testing.T) {
	partition := &SemanticNode{id: "site:W-01", kind: KindElement, declaredType: "Partition"}
	doorway := &SemanticNode{id: "site:D-01", kind: KindElement, declaredType: "Doorway"}

	virtual := BoundaryEdge{edge: &Edge{}}
	wall := BoundaryEdge{edge: &Edge{backing: []ID{"site:W-01"}}, backing: []*SemanticNode{partition}}
	opening := BoundaryEdge{
		edge:    &Edge{backing: []ID{"site:D-01", "site:W-01"}},
		backing: []*SemanticNode{doorway, partition},
	}
	unresolved := BoundaryEdge{edge: &Edge{backing: []ID{"site:W-99"}}}

	require.Equal(t, ClassificationVirtual, virtual.Classification())
	require.Equal(t, ClassificationPhysical, wall.Classification())
	require.Equal(t, ClassificationPhysical, opening.Classification())
	require.Equal(t, ClassificationUnresolved, unresolved.Classification())

	testCases := []struct {
		name     string
		filter   AdjacencyFilter
		edge     BoundaryEdge
		expected bool
	}{
		{name: "an empty filter crosses a virtual edge", filter: AdjacencyFilter{}, edge: virtual, expected: true},
		{name: "an empty filter crosses a physical edge", filter: AdjacencyFilter{}, edge: wall, expected: true},
		{name: "an empty filter crosses an unresolved edge", filter: AdjacencyFilter{}, edge: unresolved, expected: true},
		{name: "crosses a virtual edge where virtual edges may be crossed", filter: AdjacencyFilter{CrossVirtual: true}, edge: virtual, expected: true},
		{name: "does not cross a physical edge where only virtual edges may be", filter: AdjacencyFilter{CrossVirtual: true}, edge: wall, expected: false},
		{name: "does not cross a virtual edge where only types may be", filter: AdjacencyFilter{CrossTypes: []string{"Doorway"}}, edge: virtual, expected: false},
		{name: "crosses an edge one of whose backing elements declares a named type", filter: AdjacencyFilter{CrossTypes: []string{"Doorway"}}, edge: opening, expected: true},
		{name: "does not cross an edge none of whose backing elements declares a named type", filter: AdjacencyFilter{CrossTypes: []string{"Doorway"}}, edge: wall, expected: false},
		{name: "crosses an edge backed by any of several named types", filter: AdjacencyFilter{CrossTypes: []string{"Window", "Partition"}}, edge: wall, expected: true},
		{name: "never crosses an unresolved edge under a type filter", filter: AdjacencyFilter{CrossTypes: []string{"Partition"}}, edge: unresolved, expected: false},
		{name: "never crosses an unresolved edge under a virtual filter", filter: AdjacencyFilter{CrossVirtual: true}, edge: unresolved, expected: false},
		{name: "never crosses an unresolved edge under both", filter: AdjacencyFilter{CrossVirtual: true, CrossTypes: []string{"Partition"}}, edge: unresolved, expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, testCase.filter.Matches(testCase.edge))
		})
	}
}

// TestAdjacentWalkCrossesOnlyWhatItWasTold is the property the filter promises,
// over every region of the fixture and every filter the table uses: each edge a
// result was reached through is one the filter allows, and under a type filter
// that means one backed by an element declaring a named type.
func TestAdjacentWalkCrossesOnlyWhatItWasTold(t *testing.T) {
	model, boundaries := joinBoundaries(t, "adjacent")

	filters := []AdjacencyFilter{
		{CrossVirtual: true},
		{CrossTypes: []string{"Partition"}},
		{CrossTypes: []string{"Doorway"}},
		{CrossVirtual: true, CrossTypes: []string{"Partition"}},
	}

	walked := 0
	for region := range model.nodes.All() {
		for _, filter := range filters {
			for result := range boundaries.AdjacentWalk(region, Unbounded, filter) {
				walked++
				require.NotEmpty(t, result.Via(), "%s was reached across something", result.Node().ID())

				for _, edge := range result.Via() {
					classified := boundaries.Classified(edge)
					assert.True(t, filter.Matches(classified), "%s may be crossed under %+v", edge.ID(), filter)

					if !filter.CrossVirtual {
						assert.True(t, slices.ContainsFunc(classified.Backing(), func(element *SemanticNode) bool {
							return slices.Contains(filter.CrossTypes, element.Type())
						}), "%s is backed by one of %v", edge.ID(), filter.CrossTypes)
					}
				}
			}
		}
	}

	require.NotZero(t, walked, "the filters reach something")
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

// tieRegistry, tieGeometry and the tie rooms are a plan in which a room two
// steps from the subject is bordered by two rooms one step from it. Room C
// shares geom:E-AC with room A and geom:E-BC with room B, and A and B both
// share geom:E-1 with the start room, so either could be the one C is reached
// from.
const (
	tieRegistry = `(project (label "Tie fixture") (globalid-namespace "https://example.org/models/tie"))
(namespace frame (description "Frames."))
(namespace geom (description "Geometric nodes."))
(namespace site (description "Semantic nodes."))
(frame frame:b (label "Grid") (unit m))
(type Room (kind Space) (geometry area) (description "A room."))
`

	tieGeometry = `(vertex geom:V-1 (frame frame:b))
(vertex geom:V-2 (frame frame:b))
(edge geom:E-1 (frame frame:b) (vertices geom:V-1 geom:V-2))
(edge geom:E-S (frame frame:b) (vertices geom:V-2 geom:V-1))
(edge geom:E-AC (frame frame:b) (vertices geom:V-2 geom:V-1))
(edge geom:E-BC (frame frame:b) (vertices geom:V-2 geom:V-1))
(loop geom:L-S (frame frame:b) (edges geom:E-1 geom:E-S))
(loop geom:L-A (frame frame:b) (edges geom:E-1 geom:E-AC))
(loop geom:L-B (frame frame:b) (edges geom:E-1 geom:E-BC))
(loop geom:L-C (frame frame:b) (edges geom:E-AC geom:E-BC))
`

	tieStart = `(node site:R-S (label "Start") (kind Space) (type Room) (geometry area) (frame frame:b) (boundary geom:L-S))
`
	tieFar = `(node site:R-C (label "Far") (kind Space) (type Room) (geometry area) (frame frame:b) (boundary geom:L-C))
`
	tieA = `(node site:R-A (label "A") (kind Space) (type Room) (geometry area) (frame frame:b) (boundary geom:L-A))
`
	tieB = `(node site:R-B (label "B") (kind Space) (type Room) (geometry area) (frame frame:b) (boundary geom:L-B))
`
)

// TestAdjacencyDoesNotDependOnWhichFileANodeIsIn is its own function because it
// asserts across two models rather than within one: the same rooms written into
// different files are the same plan, and a walk over them reaches every room
// from the same neighbour through the same edges.
//
// Moving room A into a file read after room B's is what used to change the
// answer, because the walk expanded rooms in the order it had read them.
func TestAdjacencyDoesNotDependOnWhichFileANodeIsIn(t *testing.T) {
	layouts := []struct {
		name  string
		files map[string]string
	}{
		{
			name: "with room A read before room B",
			files: map[string]string{
				"registry.dfc":          tieRegistry,
				"entities/geometry.dfc": tieGeometry,
				"entities/a.dfc":        tieStart + tieFar + tieA,
				"entities/b.dfc":        tieB,
			},
		},
		{
			name: "with room A read after room B",
			files: map[string]string{
				"registry.dfc":          tieRegistry,
				"entities/geometry.dfc": tieGeometry,
				"entities/a.dfc":        tieStart + tieFar,
				"entities/b.dfc":        tieB,
				"entities/z.dfc":        tieA,
			},
		},
	}

	// Room C is reached from room A, the neighbour with the smaller id, and so
	// through the edge it shares with room A.
	expected := []neighbour{
		{node: "site:R-A", depth: 1, from: "site:R-S", via: []ID{"geom:E-1"}},
		{node: "site:R-B", depth: 1, from: "site:R-S", via: []ID{"geom:E-1"}},
		{node: "site:R-C", depth: 2, from: "site:R-A", via: []ID{"geom:E-AC"}},
	}

	for _, layout := range layouts {
		t.Run("reaches a room from the neighbour with the smallest id "+layout.name, func(t *testing.T) {
			root := tree(t, layout.files)

			registry, diags := LoadRegistry(root)
			require.Empty(t, diags)

			nodes, diags := LoadNodes(root, registry)
			require.Empty(t, diags)

			topology, diags := LoadTopology(root, registry)
			require.Empty(t, diags)

			boundaries, diags := ResolveBoundaries(nodes, topology)
			require.Empty(t, diags)

			start, ok := nodes.Node("site:R-S")
			require.True(t, ok)

			// Compared in depth and then id order, the order the answer is
			// reported in. The order the walk yields a level in is the order
			// it discovers it, which is not what this asserts: which node a
			// result was reached from, and so its via, is.
			got := bordering(t, boundaries.AdjacentTo(start, Unbounded))
			slices.SortStableFunc(got, func(x, y neighbour) int {
				return cmp.Or(cmp.Compare(x.depth, y.depth), strings.Compare(string(x.node), string(y.node)))
			})

			assert.Equal(t, expected, got)
		})
	}
}

// The lot model: two rooms which share no edge, and a lot whose outline is drawn
// along the outside of both. Every edge is virtual — nothing backs any of them —
// so crossing virtual edges crosses all of them.
//
// A walk which may enter anything steps from room A onto the lot and from the lot
// into room B, which is how a site, zone or storey outline drawn with the same
// edges as the rooms along its side joins rooms that are not next to each other.
const (
	lotRegistry = `; registry.dfc
(project (label "Leak fixture") (globalid-namespace "https://example.org/models/leak"))
(namespace frame (description "Frames."))
(namespace geom (description "Geometric nodes."))
(namespace site (description "Semantic nodes."))
(frame frame:b (label "Grid") (unit m))
(type Room (kind Space) (geometry area) (description "A room."))
(type Lot (kind Site) (geometry area) (description "The land the house stands on."))
`

	lotModel = `; entities/model.dfc
(node site:LOT (label "Lot") (kind Site) (type Lot) (geometry area) (frame frame:b) (boundary geom:L-LOT))
(node site:R-A (label "Room A") (kind Space) (type Room) (geometry area) (frame frame:b) (boundary geom:L-A))
(node site:R-B (label "Room B") (kind Space) (type Room) (geometry area) (frame frame:b) (boundary geom:L-B))
(vertex geom:V-1 (frame frame:b))
(vertex geom:V-2 (frame frame:b))
(edge geom:E-A-OUT (frame frame:b) (vertices geom:V-1 geom:V-2))
(edge geom:E-A-IN (frame frame:b) (vertices geom:V-2 geom:V-1))
(edge geom:E-B-OUT (frame frame:b) (vertices geom:V-1 geom:V-2))
(edge geom:E-B-IN (frame frame:b) (vertices geom:V-2 geom:V-1))
(loop geom:L-A (frame frame:b) (edges geom:E-A-OUT geom:E-A-IN))
(loop geom:L-B (frame frame:b) (edges geom:E-B-OUT geom:E-B-IN))
(loop geom:L-LOT (frame frame:b) (edges geom:E-A-OUT geom:E-B-OUT))
`

	// lotHallType and lotHall add a hall of a type of its own between the two
	// rooms, sharing one edge with each, so that a walk which enters only spaces
	// still has somewhere to go.
	lotHallType = `(type Hall (kind Space) (geometry area) (description "A hall."))
`
	lotHall = `(node site:R-H (label "Hall") (kind Space) (type Hall) (geometry area) (frame frame:b) (boundary geom:L-H))
(loop geom:L-H (frame frame:b) (edges geom:E-A-IN geom:E-B-IN))
`
)

// lotFiles is the lot model as the issue reproducing the leak writes it.
func lotFiles() map[string]string {
	return map[string]string{"registry.dfc": lotRegistry, "entities/model.dfc": lotModel}
}

// lotWithHallFiles is the lot model with a hall between the two rooms.
func lotWithHallFiles() map[string]string {
	return map[string]string{
		"registry.dfc":       lotRegistry + lotHallType,
		"entities/model.dfc": lotModel,
		"entities/hall.dfc":  lotHall,
	}
}

// joinTree loads the model a tree of files holds and joins its boundaries,
// requiring every stage to be clean.
func joinTree(t *testing.T, files map[string]string) (*Nodes, *Boundaries) {
	t.Helper()

	root := tree(t, files)

	registry, diags := LoadRegistry(root)
	require.Empty(t, diags)

	nodes, diags := LoadNodes(root, registry)
	require.Empty(t, diags)

	topology, diags := LoadTopology(root, registry)
	require.Empty(t, diags)

	boundaries, diags := ResolveBoundaries(nodes, topology)
	require.Empty(t, diags)

	return nodes, boundaries
}

// TestBoundariesAdjacentWalkEntersOnlyWhatItWasTold walks adjacency into only the
// regions a filter's walk fields allow, one filter per case.
func TestBoundariesAdjacentWalkEntersOnlyWhatItWasTold(t *testing.T) {
	testCases := []struct {
		name     string
		files    map[string]string
		region   ID
		filter   AdjacencyFilter
		expected []neighbour
	}{
		{
			name:   "walks through the lot into the room past it when no walk field is given",
			files:  lotFiles(),
			region: "site:R-A",
			filter: AdjacencyFilter{},
			expected: []neighbour{
				{node: "site:LOT", depth: 1, from: "site:R-A", via: []ID{"geom:E-A-OUT"}},
				{node: "site:R-B", depth: 2, from: "site:LOT", via: []ID{"geom:E-B-OUT"}},
			},
		},
		{
			name:   "does not walk through the lot when it enters only spaces",
			files:  lotFiles(),
			region: "site:R-A",
			filter: AdjacencyFilter{WalkKinds: []Kind{KindSpace}},
		},
		{
			name:     "enters the lot and not the room past it when it enters only sites",
			files:    lotFiles(),
			region:   "site:R-A",
			filter:   AdjacencyFilter{WalkKinds: []Kind{KindSite}},
			expected: []neighbour{{node: "site:LOT", depth: 1, from: "site:R-A", via: []ID{"geom:E-A-OUT"}}},
		},
		{
			name:   "enters what any of several kinds allows",
			files:  lotFiles(),
			region: "site:R-A",
			filter: AdjacencyFilter{WalkKinds: []Kind{KindSpace, KindSite}},
			expected: []neighbour{
				{node: "site:LOT", depth: 1, from: "site:R-A", via: []ID{"geom:E-A-OUT"}},
				{node: "site:R-B", depth: 2, from: "site:LOT", via: []ID{"geom:E-B-OUT"}},
			},
		},
		{
			name:     "enters only regions of a type it names",
			files:    lotFiles(),
			region:   "site:R-A",
			filter:   AdjacencyFilter{WalkTypes: []string{"Lot"}},
			expected: []neighbour{{node: "site:LOT", depth: 1, from: "site:R-A", via: []ID{"geom:E-A-OUT"}}},
		},
		{
			name:   "does not walk through the lot when it enters only rooms",
			files:  lotFiles(),
			region: "site:R-A",
			filter: AdjacencyFilter{WalkTypes: []string{"Room"}},
		},
		{
			// The lot is a site and not a room, and the room past it is never
			// reached: every walk field given has to be satisfied, not one.
			name:   "enters only a region which satisfies every walk field given",
			files:  lotFiles(),
			region: "site:R-A",
			filter: AdjacencyFilter{WalkKinds: []Kind{KindSite}, WalkTypes: []string{"Room"}},
		},
		{
			name:   "starts from the subject whatever the subject declares",
			files:  lotFiles(),
			region: "site:LOT",
			filter: AdjacencyFilter{WalkKinds: []Kind{KindSpace}},
			expected: []neighbour{
				{node: "site:R-A", depth: 1, from: "site:LOT", via: []ID{"geom:E-A-OUT"}},
				{node: "site:R-B", depth: 1, from: "site:LOT", via: []ID{"geom:E-B-OUT"}},
			},
		},
		{
			name:   "does not walk through the lot across edges nothing backs when it enters only spaces",
			files:  lotFiles(),
			region: "site:R-A",
			filter: AdjacencyFilter{CrossVirtual: true, WalkKinds: []Kind{KindSpace}},
		},
		{
			name:   "walks through the lot across edges nothing backs when no walk field is given",
			files:  lotFiles(),
			region: "site:R-A",
			filter: AdjacencyFilter{CrossVirtual: true},
			expected: []neighbour{
				{node: "site:LOT", depth: 1, from: "site:R-A", via: []ID{"geom:E-A-OUT"}},
				{node: "site:R-B", depth: 2, from: "site:LOT", via: []ID{"geom:E-B-OUT"}},
			},
		},
		{
			name:   "walks on through the spaces it enters",
			files:  lotWithHallFiles(),
			region: "site:R-A",
			filter: AdjacencyFilter{CrossVirtual: true, WalkKinds: []Kind{KindSpace}},
			expected: []neighbour{
				{node: "site:R-H", depth: 1, from: "site:R-A", via: []ID{"geom:E-A-IN"}},
				{node: "site:R-B", depth: 2, from: "site:R-H", via: []ID{"geom:E-B-IN"}},
			},
		},
		{
			name:   "does not walk through a space of a type it does not name",
			files:  lotWithHallFiles(),
			region: "site:R-A",
			filter: AdjacencyFilter{WalkTypes: []string{"Room"}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			nodes, boundaries := joinTree(t, testCase.files)

			region, ok := nodes.Node(testCase.region)
			require.True(t, ok)

			got := bordering(t, boundaries.AdjacentWalk(region, Unbounded, testCase.filter))
			slices.SortStableFunc(got, func(x, y neighbour) int {
				return cmp.Or(cmp.Compare(x.depth, y.depth), strings.Compare(string(x.node), string(y.node)))
			})

			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestAdjacencyFilterEnters decides one region at a time.
func TestAdjacencyFilterEnters(t *testing.T) {
	room := &SemanticNode{id: "site:R-A", kind: KindSpace, declaredType: "Room"}
	lot := &SemanticNode{id: "site:LOT", kind: KindSite, declaredType: "Lot"}

	testCases := []struct {
		name     string
		filter   AdjacencyFilter
		region   *SemanticNode
		expected bool
	}{
		{name: "an empty filter enters any region", filter: AdjacencyFilter{}, region: lot, expected: true},
		{name: "a crossing filter enters any region", filter: AdjacencyFilter{CrossVirtual: true, CrossTypes: []string{"Doorway"}}, region: lot, expected: true},
		{name: "enters a region of a named kind", filter: AdjacencyFilter{WalkKinds: []Kind{KindSpace}}, region: room, expected: true},
		{name: "does not enter a region of a kind it does not name", filter: AdjacencyFilter{WalkKinds: []Kind{KindSpace}}, region: lot, expected: false},
		{name: "enters a region of any of several named kinds", filter: AdjacencyFilter{WalkKinds: []Kind{KindSpace, KindSite}}, region: lot, expected: true},
		{name: "enters a region of a named type", filter: AdjacencyFilter{WalkTypes: []string{"Lot"}}, region: lot, expected: true},
		{name: "does not enter a region of a type it does not name", filter: AdjacencyFilter{WalkTypes: []string{"Lot"}}, region: room, expected: false},
		{name: "enters a region which satisfies both walk fields", filter: AdjacencyFilter{WalkKinds: []Kind{KindSpace}, WalkTypes: []string{"Room"}}, region: room, expected: true},
		{name: "does not enter a region which satisfies only the kind", filter: AdjacencyFilter{WalkKinds: []Kind{KindSpace}, WalkTypes: []string{"Lot"}}, region: room, expected: false},
		{name: "does not enter a region which satisfies only the type", filter: AdjacencyFilter{WalkKinds: []Kind{KindSite}, WalkTypes: []string{"Room"}}, region: room, expected: false},
		{name: "never enters no region at all", filter: AdjacencyFilter{}, region: nil, expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, testCase.filter.Enters(testCase.region))
		})
	}
}

// TestWalkFieldsDoNotDecideCrossing is the other half of the table above: the walk
// fields say which regions are entered, and a filter given only them still
// crosses every edge.
func TestWalkFieldsDoNotDecideCrossing(t *testing.T) {
	partition := &SemanticNode{id: "site:W-01", kind: KindElement, declaredType: "Partition"}

	virtual := BoundaryEdge{edge: &Edge{}}
	wall := BoundaryEdge{edge: &Edge{backing: []ID{"site:W-01"}}, backing: []*SemanticNode{partition}}
	unresolved := BoundaryEdge{edge: &Edge{backing: []ID{"site:W-99"}}}

	filter := AdjacencyFilter{WalkKinds: []Kind{KindSpace}, WalkTypes: []string{"Room"}}

	for _, edge := range []BoundaryEdge{virtual, wall, unresolved} {
		assert.True(t, filter.Matches(edge), "%s may be crossed", edge.Classification())
	}
}

// TestAdjacentWalkEntersOnlyWhatItWasTold is the property the walk fields
// promise, over every region of both lot models and every filter the table uses:
// every result, and every region a result was reached from other than the one the
// walk started from, satisfies the walk fields.
func TestAdjacentWalkEntersOnlyWhatItWasTold(t *testing.T) {
	filters := []AdjacencyFilter{
		{WalkKinds: []Kind{KindSpace}},
		{WalkKinds: []Kind{KindSite}},
		{WalkKinds: []Kind{KindSpace, KindSite}},
		{WalkTypes: []string{"Room"}},
		{WalkTypes: []string{"Room", "Hall"}},
		{WalkKinds: []Kind{KindSpace}, WalkTypes: []string{"Hall"}},
		{CrossVirtual: true, WalkKinds: []Kind{KindSpace}},
	}

	walked := 0
	for _, files := range []map[string]string{lotFiles(), lotWithHallFiles()} {
		nodes, boundaries := joinTree(t, files)

		for region := range nodes.All() {
			for _, filter := range filters {
				for result := range boundaries.AdjacentWalk(region, Unbounded, filter) {
					walked++

					assert.True(t, filter.Enters(result.Node()), "%s is entered under %+v", result.Node().ID(), filter)
					if result.From() != region {
						assert.True(t, filter.Enters(result.From()), "%s is entered under %+v", result.From().ID(), filter)
					}
				}
			}
		}
	}

	require.NotZero(t, walked, "the filters reach something")
}
