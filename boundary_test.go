// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// boundaryFixture is the root of one fixture model: a registry, and whichever of
// the two families the case is about, judged against it.
func boundaryFixture(name string) string { return filepath.Join("testdata", "boundary", name) }

// boundaryModel is one fixture loaded: the vocabulary it declares, both families
// of it, and where its vertices are.
//
// The four are loaded together because the pass under test is the one which
// needs all four. Everything below the join reads one family and can be tested
// against one; a boundary is written on a semantic node, names a member of the
// geometric family and is measured with claims, and a fixture which held less
// than that could not exercise it.
type boundaryModel struct {
	registry  *Registry
	nodes     *Nodes
	topology  *Topology
	positions Positions
}

// loadBoundaryModel loads one fixture model, failing the test where any of the
// passes beneath the join reports anything.
//
// Their diagnostics are asserted empty rather than rendered: every fixture here
// loads clean in the registry, in both families and in its claims, so whatever
// the golden beside it holds is what the pass under test had to say and nothing
// else.
func loadBoundaryModel(t *testing.T, name string) boundaryModel {
	t.Helper()

	return loadBoundaryModelAt(t, boundaryFixture(name))
}

// loadBoundaryModelAt is [loadBoundaryModel] against a root of its own, for the
// cases which build the model they load rather than reading one from testdata.
func loadBoundaryModelAt(t *testing.T, root string) boundaryModel {
	t.Helper()

	registry, registryDiags := LoadRegistry(root)
	require.Empty(t, registryDiags, "the fixture registry loads clean")

	nodes, nodeDiags := LoadNodes(root, registry)
	require.Empty(t, nodeDiags, "the fixture's semantic family loads clean")

	topology, topologyDiags := LoadTopology(root, registry)
	require.Empty(t, topologyDiags, "the fixture's geometric family loads clean")

	return boundaryModel{
		registry:  registry,
		nodes:     nodes,
		topology:  topology,
		positions: positionsIn(t, root, registry, topology),
	}
}

// positionsIn resolves where every vertex of a fixture is.
//
// The predicate is named here and not in the engine, which is the whole of why
// [Positions] is passed in rather than read. This fixture's registry spells a
// vertex's position `position`; a consuming repository which spelled it
// something else would write that name here and change nothing else.
func positionsIn(t *testing.T, root string, registry *Registry, topology *Topology) Positions {
	t.Helper()

	claims, diags := LoadClaims(root, registry)
	require.Empty(t, diags, "the fixture's claims load clean")

	positions := make(Positions)
	for vertex := range topology.Vertices() {
		resolution, err := claims.Resolve(vertex.ID(), "position", registry)
		require.NoError(t, err)

		if value, ok := resolution.Value(); ok {
			positions[vertex.ID()] = value
		}
	}

	return positions
}

// renderBoundaryDiagnostics renders diagnostics the way the command line
// interface would, which is what the golden beside a fixture holds.
func renderBoundaryDiagnostics(t *testing.T, diags []Diagnostic) string {
	t.Helper()

	var collected Diagnostics
	collected.Add(diags...)

	var rendered strings.Builder
	require.NoError(t, collected.Render(&rendered, FileSources{}))

	return rendered.String()
}

// expectedBoundaryDiagnostics returns the rendering held beside the fixture,
// having first rewritten it from got when -update was passed.
func expectedBoundaryDiagnostics(t *testing.T, name string, got string) string {
	t.Helper()

	path := filepath.Join(boundaryFixture(name), "diagnostics.txt")
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

func TestResolveBoundaries(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
	}{
		{
			name:    "names a boundary which reaches nothing, one which reaches the wrong sort of node, and one written twice",
			fixture: "dangling-boundary",
		},
		{
			name:    "names a backing which reaches nothing, one which reaches the wrong sort of node, and one written twice",
			fixture: "dangling-backing",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			model := loadBoundaryModel(t, testCase.fixture)

			_, diags := ResolveBoundaries(model.nodes, model.topology)
			got := renderBoundaryDiagnostics(t, diags)

			assert.Equal(t, expectedBoundaryDiagnostics(t, testCase.fixture, got), got)
		})
	}
}

// TestResolveBoundariesSharesOneEdgeBetweenTwoRegions is its own function
// because the assertion is about identity rather than about a diagnostic: the
// two rooms either side of a partition have to reach the same Go value for it,
// and every question asked of that edge has to give both rooms back.
//
// This is the whole reason a semantic node references a loop instead of holding
// coordinates. Two copies of a wall can differ by a millimetre; one node cannot
// differ from itself, so a sliver gap between the rooms is not a state the model
// can express at all.
func TestResolveBoundariesSharesOneEdgeBetweenTwoRegions(t *testing.T) {
	model := loadBoundaryModel(t, "valid")

	boundaries, diags := ResolveBoundaries(model.nodes, model.topology)
	require.Empty(t, renderBoundaryDiagnostics(t, diags), "the valid fixture joins clean")

	room, ok := model.nodes.Node("site:S-101")
	require.True(t, ok)

	corridor, ok := model.nodes.Node("site:S-102")
	require.True(t, ok)

	shared, ok := model.topology.Edge("geom:E-02")
	require.True(t, ok)

	t.Run("reaches one edge from both of the regions which reference it", func(t *testing.T) {
		fromRoom := slices.Collect(boundaries.Edges(room))
		fromCorridor := slices.Collect(boundaries.Edges(corridor))

		require.Contains(t, fromRoom, shared)
		require.Contains(t, fromCorridor, shared)

		// Not merely equal: the same pointer, reached both ways. Two edges which
		// happened to hold equal ids and equal endpoints would satisfy an
		// equality assertion and would still be two walls.
		assert.Same(t, shared, fromRoom[slices.Index(fromRoom, shared)])
		assert.Same(t, shared, fromCorridor[slices.Index(fromCorridor, shared)])
	})

	t.Run("names both regions when asked from the edge", func(t *testing.T) {
		assert.Equal(t, []*SemanticNode{room, corridor}, slices.Collect(boundaries.Regions(shared)))
	})

	t.Run("names one region from an edge only one of them reaches", func(t *testing.T) {
		west, ok := model.topology.Edge("geom:E-04")
		require.True(t, ok)

		assert.Equal(t, []*SemanticNode{room}, slices.Collect(boundaries.Regions(west)))
	})
}

// TestBoundariesEnumeratesWhatARegionDependsOn is its own function because it
// asserts on a traversal rather than on a diagnostic: what a region is built
// from, at each of the three levels the geometric family has.
func TestBoundariesEnumeratesWhatARegionDependsOn(t *testing.T) {
	model := loadBoundaryModel(t, "valid")

	boundaries, diags := ResolveBoundaries(model.nodes, model.topology)
	require.Empty(t, renderBoundaryDiagnostics(t, diags), "the valid fixture joins clean")

	room, ok := model.nodes.Node("site:S-101")
	require.True(t, ok)

	t.Run("gives the loops the region references, in the order it wrote them", func(t *testing.T) {
		var ids []ID
		for loop := range boundaries.Loops(room) {
			ids = append(ids, loop.ID())
		}

		assert.Equal(t, []ID{"geom:L-01"}, ids)
	})

	t.Run("gives the edges the region's boundary is assembled from", func(t *testing.T) {
		var ids []ID
		for edge := range boundaries.Edges(room) {
			ids = append(ids, edge.ID())
		}

		assert.Equal(t, []ID{"geom:E-01", "geom:E-02", "geom:E-03", "geom:E-04"}, ids)
	})

	t.Run("gives each corner once, however many of the region's edges meet there", func(t *testing.T) {
		var ids []ID
		for vertex := range boundaries.Vertices(room) {
			ids = append(ids, vertex.ID())
		}

		assert.Equal(t, []ID{"geom:V-01", "geom:V-02", "geom:V-03", "geom:V-04"}, ids)
	})

	t.Run("gives the regions a loop bounds", func(t *testing.T) {
		loop, ok := model.topology.Loop("geom:L-01")
		require.True(t, ok)

		assert.Equal(t, []*SemanticNode{room}, slices.Collect(boundaries.Bounded(loop)))
	})

	t.Run("gives nothing for a node which references no loop", func(t *testing.T) {
		unbounded := &SemanticNode{id: "site:S-999"}

		assert.Empty(t, slices.Collect(boundaries.Loops(unbounded)))
		assert.Empty(t, slices.Collect(boundaries.Edges(unbounded)))
		assert.Empty(t, slices.Collect(boundaries.Vertices(unbounded)))
	})
}

// TestBoundariesOfNothingAnswerNothing is its own function because it is about a
// receiver rather than about a model: every traversal has to work on a zero
// value and on a nil, which is what lets a caller which did not read the
// diagnostics of a failed load ask questions of it rather than panic.
func TestBoundariesOfNothingAnswerNothing(t *testing.T) {
	testCases := []struct {
		name       string
		boundaries *Boundaries
	}{
		{name: "answers nothing when it is nil", boundaries: nil},
		{name: "answers nothing when nothing was resolved", boundaries: &Boundaries{}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			node := &SemanticNode{id: "site:S-101"}

			assert.Empty(t, slices.Collect(testCase.boundaries.Loops(node)))
			assert.Empty(t, slices.Collect(testCase.boundaries.Edges(node)))
			assert.Empty(t, slices.Collect(testCase.boundaries.Vertices(node)))
			assert.Empty(t, slices.Collect(testCase.boundaries.Bounded(&Loop{})))
			assert.Empty(t, slices.Collect(testCase.boundaries.Regions(&Edge{})))
			assert.Empty(t, slices.Collect(testCase.boundaries.Classify(node)))

			// An edge which named nothing is backed by nothing, whoever is
			// asked. A join which resolved no reference has no answer of its own
			// to give, and the edge's own emptiness is the whole of it.
			assert.Equal(t, ClassificationVirtual, testCase.boundaries.Classified(&Edge{}).Classification())
		})
	}
}

// TestSemanticNodeReferencesALoopRatherThanCoordinates is its own function
// because it asserts on what the loaded node carries rather than on what a
// traversal reaches: the ids as they were written, with a loop named twice held
// once.
func TestSemanticNodeReferencesALoopRatherThanCoordinates(t *testing.T) {
	model := loadBoundaryModel(t, "dangling-boundary")

	room, ok := model.nodes.Node("site:S-101")
	require.True(t, ok)

	// Written four times, with one of them a repeat. What a caller reads is the
	// set of loops the node references, and the repeat is a diagnostic rather
	// than a second reference to follow.
	assert.Equal(t, []ID{"geom:L-01", "geom:V-01", "geom:L-99"}, room.Boundaries())
}
