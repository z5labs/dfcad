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

// adjacentRoot is the fixture of three rooms in a row, where room A and the
// corridor share two edges and the corridor and room C share one.
var adjacentRoot = filepath.Join("testdata", "boundary", "adjacent")

// textSpan is the span of the first text written after anchor in the file at
// path, which is how a test names a place in a fixture without writing its line
// and column down.
func textSpan(t *testing.T, path, anchor, text string) Span {
	t.Helper()

	src, err := os.ReadFile(path)
	require.NoError(t, err)

	from := strings.Index(string(src), anchor)
	require.GreaterOrEqual(t, from, 0, "the fixture no longer holds %q", anchor)
	at := strings.Index(string(src[from:]), text)
	require.GreaterOrEqual(t, at, 0, "the fixture no longer holds %q after %q", text, anchor)

	start := from + at
	return Span{Start: positionAt(path, src, start), End: positionAt(path, src, start+len(text))}
}

// positionAt is the position of the byte offset in src.
func positionAt(path string, src []byte, offset int) Position {
	line := 1 + strings.Count(string(src[:offset]), "\n")
	column := offset + 1
	if newline := strings.LastIndexByte(string(src[:offset]), '\n'); newline >= 0 {
		column = offset - newline
	}
	return Position{Path: path, Line: line, Column: column, Offset: offset}
}

// ownerIDs is the ids of the nodes an entity belongs to, in the order they came.
func ownerIDs(graph *Graph, entity Entity) []ID {
	var ids []ID
	for node := range graph.Owners(entity) {
		ids = append(ids, node.ID())
	}
	return ids
}

func TestGraphEnclosing(t *testing.T) {
	adjacent := filepath.Join(adjacentRoot, "model.dfc")
	registry := filepath.Join(adjacentRoot, "registry.dfc")
	valid := graphFixture("valid")
	site := filepath.Join(valid, "entities", "site.dfc")

	testCases := []struct {
		name     string
		root     string
		span     func(t *testing.T) Span
		expected ID
		owners   []ID
	}{
		{
			name:     "finds the loop a span inside it is written in, and the node it bounds",
			root:     adjacentRoot,
			span:     func(t *testing.T) Span { return textSpan(t, adjacent, "(loop geom:L-02", "geom:E-07") },
			expected: "geom:L-02",
			owners:   []ID{"site:S-B"},
		},
		{
			name: "finds an edge two regions share, and both regions",
			root: adjacentRoot,
			span: func(t *testing.T) Span {
				return textSpan(t, adjacent, "(edge geom:E-02", "(vertices geom:V-02 geom:V-03)")
			},
			expected: "geom:E-02",
			owners:   []ID{"site:S-A", "site:S-B"},
		},
		{
			name:     "finds an edge one region reaches, and that region",
			root:     adjacentRoot,
			span:     func(t *testing.T) Span { return textSpan(t, adjacent, "(edge geom:E-10", "geom:E-10") },
			expected: "geom:E-10",
			owners:   []ID{"site:S-C"},
		},
		{
			name:     "finds a vertex, and every region it is a corner of",
			root:     adjacentRoot,
			span:     func(t *testing.T) Span { return textSpan(t, adjacent, "(vertex geom:V-06", "(label") },
			expected: "geom:V-06",
			owners:   []ID{"site:S-B", "site:S-C"},
		},
		{
			name:     "finds the node a span on its own id names, and the node itself",
			root:     adjacentRoot,
			span:     func(t *testing.T) Span { return textSpan(t, adjacent, "(node site:S-C", "site:S-C") },
			expected: "site:S-C",
			owners:   []ID{"site:S-C"},
		},
		{
			name:     "finds the node a claim is written on",
			root:     valid,
			span:     func(t *testing.T) Span { return textSpan(t, site, "(node site:E-01", "(value 0.102 m)") },
			expected: "site:E-01",
			owners:   []ID{"site:E-01"},
		},
		{
			name: "finds the form a span covering the whole of it is written in",
			root: adjacentRoot,
			span: func(t *testing.T) Span {
				return textSpan(t, adjacent, "(vertex geom:V-04", `(vertex geom:V-04 (label "Room A, north-east corner") (frame frame:building))`)
			},
			expected: "geom:V-04",
			owners:   []ID{"site:S-A", "site:S-B"},
		},
		{
			name: "finds the form an empty span at its opening parenthesis points at",
			root: adjacentRoot,
			span: func(t *testing.T) Span {
				return textSpan(t, adjacent, "(loop geom:L-03", "(loop").Start.Span()
			},
			expected: "geom:L-03",
			owners:   []ID{"site:S-C"},
		},
		{
			name: "finds nothing for an empty span just past a form's end",
			root: adjacentRoot,
			span: func(t *testing.T) Span {
				return textSpan(t, adjacent, "(vertex geom:V-04", `(frame frame:building))`).End.Span()
			},
		},
		{
			name: "finds nothing for a span which runs past the form it starts in",
			root: adjacentRoot,
			span: func(t *testing.T) Span {
				return textSpan(t, adjacent, "(vertex geom:V-04", "(frame frame:building))\n\n(vertex geom:V-05")
			},
		},
		{
			name: "finds nothing for a span on a registry form",
			root: adjacentRoot,
			span: func(t *testing.T) Span { return textSpan(t, registry, "(type Corridor", "(kind Space)") },
		},
		{
			name: "finds nothing for a span in a comment between forms",
			root: adjacentRoot,
			span: func(t *testing.T) Span { return textSpan(t, adjacent, "; The one edge the corridor", "corridor") },
		},
		{
			name: "finds nothing for a span in a file the model does not hold",
			root: adjacentRoot,
			span: func(t *testing.T) Span {
				within := textSpan(t, adjacent, "(loop geom:L-02", "geom:E-07")
				within.Start.Path = filepath.Join(adjacentRoot, "elsewhere.dfc")
				within.End.Path = within.Start.Path
				return within
			},
		},
		{
			name: "finds the same entity for a span which carries no offsets, as one read back from JSON does",
			root: adjacentRoot,
			span: func(t *testing.T) Span {
				within := textSpan(t, adjacent, "(loop geom:L-02", "geom:E-07")
				within.Start.Offset, within.End.Offset = 0, 0
				return within
			},
			expected: "geom:L-02",
			owners:   []ID{"site:S-B"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			graph, _ := LoadGraph(testCase.root)
			require.NotNil(t, graph)

			entity, ok := graph.Enclosing(testCase.span(t))
			if testCase.expected == "" {
				assert.False(t, ok)
				assert.Nil(t, entity)
				return
			}

			require.True(t, ok)
			assert.Equal(t, testCase.expected, entity.ID())
			assert.Equal(t, testCase.owners, ownerIDs(graph, entity))
		})
	}
}

// TestGraphEnclosingFindsEveryEntityByItsOwnSpan holds the lookup to the load
// as a property: every entity the model holds is what a span inside its own
// form finds, and an empty span at each form's first byte finds it too.
func TestGraphEnclosingFindsEveryEntityByItsOwnSpan(t *testing.T) {
	for _, root := range []string{adjacentRoot, graphFixture("valid"), filepath.Join("testdata", "siting", "surveyed")} {
		t.Run("finds every entity of "+root+" by a span inside it", func(t *testing.T) {
			graph, _ := LoadGraph(root)
			require.NotNil(t, graph)

			for entity := range graph.entities() {
				found, ok := graph.Enclosing(entity.Span())
				if assert.True(t, ok, "%s is enclosed by its own form", entity.ID()) {
					assert.Same(t, entity, found)
				}

				found, ok = graph.Enclosing(entity.Span().Start.Span())
				if assert.True(t, ok, "%s is enclosed at its first byte", entity.ID()) {
					assert.Same(t, entity, found)
				}
			}
		})
	}
}

// TestGraphOwnersIsTheReverseOfBoundaryOf holds [Graph.Owners] to the forward
// direction it reverses, over every loop, edge and vertex of the adjacency
// fixture: the nodes a span inside one of them names are exactly the nodes
// whose boundary lists it — the loops a node is bounded by, and the edges and
// vertices `traverse boundary-of` walks.
func TestGraphOwnersIsTheReverseOfBoundaryOf(t *testing.T) {
	graph, _ := LoadGraph(adjacentRoot)
	require.NotNil(t, graph)

	// reaching is every node whose boundary, as listed, includes the shape.
	reaching := func(listed func(*SemanticNode) []Entity, shape Entity) []ID {
		var ids []ID
		for node := range graph.Nodes().All() {
			if slices.Contains(listed(node), shape) {
				ids = append(ids, node.ID())
			}
		}
		slices.Sort(ids)
		return ids
	}

	// named is the nodes a span inside the shape's form names.
	named := func(shape Entity) []ID {
		within := shape.Span()
		within.Start.Column++

		found, ok := graph.Enclosing(within)
		require.True(t, ok, "%s is enclosed by its own form", shape.ID())
		require.Same(t, shape, found)

		ids := ownerIDs(graph, found)
		slices.Sort(ids)
		return ids
	}

	t.Run("names every node whose boundary lists an edge, and no other", func(t *testing.T) {
		listed := func(node *SemanticNode) []Entity { return slices.Collect(asEntities(graph.Edges(node))) }

		for edge := range graph.Topology().Edges() {
			assert.Equal(t, reaching(listed, edge), named(edge), "%s", edge.ID())
		}
	})

	t.Run("names every node which names a loop, and no other", func(t *testing.T) {
		listed := func(node *SemanticNode) []Entity { return slices.Collect(asEntities(graph.Loops(node))) }

		for loop := range graph.Topology().Loops() {
			assert.Equal(t, reaching(listed, loop), named(loop), "%s", loop.ID())
		}
	})

	t.Run("names every node whose boundary reaches a vertex, and no other", func(t *testing.T) {
		listed := func(node *SemanticNode) []Entity { return slices.Collect(asEntities(graph.Vertices(node))) }

		for vertex := range graph.Topology().Vertices() {
			assert.Equal(t, reaching(listed, vertex), named(vertex), "%s", vertex.ID())
		}
	})
}

// TestGraphOwnersOfNothing is its own function because it asserts over what is
// not in the model at all rather than over a span.
func TestGraphOwnersOfNothing(t *testing.T) {
	graph, _ := LoadGraph(adjacentRoot)
	require.NotNil(t, graph)

	testCases := []struct {
		name   string
		entity Entity
	}{
		{name: "yields nothing for no entity", entity: nil},
		{name: "yields nothing for a nil node", entity: (*SemanticNode)(nil)},
		{name: "yields nothing for a vertex the model does not hold", entity: &Vertex{}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Empty(t, ownerIDs(graph, testCase.entity))
		})
	}

	t.Run("yields nothing on a nil graph", func(t *testing.T) {
		var nothing *Graph
		assert.Empty(t, ownerIDs(nothing, &Loop{}))

		_, ok := nothing.Enclosing(Span{})
		assert.False(t, ok)
	})
}
