// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rerouted is the batch which reroutes the store's outline through a corner a
// metre beyond its north side: a vertex and the two edges to it written, and the
// ring restated through them in place of the side they replace. It is the
// example the operation file documents, applied as written.
const rerouted = `{"operations": [
  {"op": "add-vertex", "id": "geom:V-30", "frame": "frame:building", "file": "model.dfc", "predicate": "position",
   "claim": {"value": "11.0 3.0 0.0", "unit": "m", "source": "Interior control set IC-01, Acme Surveys",
             "method": "method:total-station", "accuracy": ["independent 0.004 m"], "date": "2026-02-18"}},
  {"op": "add-edge", "id": "geom:E-30", "frame": "frame:building", "file": "model.dfc", "start": "geom:V-11", "end": "geom:V-30"},
  {"op": "add-edge", "id": "geom:E-31", "frame": "frame:building", "file": "model.dfc", "start": "geom:V-30", "end": "geom:V-12"},
  {"op": "set-edges", "id": "geom:L-13", "edges": ["geom:E-13", "geom:E-14", "geom:E-30", "geom:E-31", "geom:E-16"]}
]}`

// loopOf is one loop of graph, requiring the model to hold it.
func loopOf(t *testing.T, graph *Graph, id ID) *Loop {
	t.Helper()

	loop, ok := graph.Topology().Loop(id)
	require.True(t, ok, "the model holds %s", id)

	return loop
}

// areaOf is the area the region id encloses in the model beneath root, measured
// as `dfcad measure` measures it, requiring the model to load clean.
func areaOf(t *testing.T, root string, id ID) float64 {
	t.Helper()

	model := loadMeasuredRoot(t, root)

	region, ok := model.nodes.Node(id)
	require.True(t, ok, "the model holds %s", id)

	measurement, diags := model.topology.MeasureRegion(region, model.boundaries, model.survey)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	area, computed := measurement.Area()
	require.True(t, computed, "%s encloses an area", id)

	return area
}

// assertSameAssertions checks that got carries the checks original did. The
// parameters are compared by check and count rather than spelling, because a
// fixture not written in canonical form has them reordered when it is printed.
func assertSameAssertions(t *testing.T, original, got *Loop) {
	t.Helper()

	require.Len(t, got.Assertions(), len(original.Assertions()))
	for at, assertion := range original.Assertions() {
		assert.Equal(t, assertion.Check, got.Assertions()[at].Check)
		assert.Len(t, got.Assertions()[at].Parameters, len(assertion.Parameters))
	}
}

func TestTxSetEdges(t *testing.T) {
	testCases := []struct {
		name  string
		loop  ID
		edges []ID
	}{
		{
			name:  "restates a ring walked from another of its edges",
			loop:  "geom:L-13",
			edges: []ID{"geom:E-14", "geom:E-15", "geom:E-16", "geom:E-13"},
		},
		{
			name:  "restates a ring walked the other way round",
			loop:  "geom:L-13",
			edges: []ID{"geom:E-16", "geom:E-15", "geom:E-14", "geom:E-13"},
		},
		{
			name:  "restates a ring as the one it already was",
			loop:  "geom:L-13",
			edges: []ID{"geom:E-13", "geom:E-14", "geom:E-15", "geom:E-16"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, satisfiedFixture)

			before, diags := LoadGraph(root)
			require.Empty(t, diags)
			original := loopOf(t, before, testCase.loop)

			graph := authored(t, root, func(tx *Tx) error {
				return tx.SetEdges(testCase.loop, testCase.edges)
			})

			loop := loopOf(t, graph, testCase.loop)

			// The round trip: what the model reads back is the list written, in
			// the order it was written.
			assert.Equal(t, testCase.edges, loop.Edges())

			// Nothing else about the loop moved.
			assert.Equal(t, original.Label(), loop.Label())
			assert.Equal(t, original.Frame(), loop.Frame())
			assertSameAssertions(t, original, loop)

			// The node bounded by it still names it, and reads the ring the
			// change wrote: the store measures what the ring encloses.
			store, ok := graph.Node("site:S-901")
			require.True(t, ok)
			assert.Equal(t, []ID{testCase.loop}, store.Boundaries())
			assert.InDelta(t, 4.0, areaOf(t, root, "site:S-901"), 1e-9)
		})
	}
}

// TestTxSetEdgesReroutesAnOutline is the change the operation exists for: an
// outline moved through a corner the same change writes, which every node
// bounded by the loop reads with no edit of its own.
func TestTxSetEdgesReroutesAnOutline(t *testing.T) {
	root := copied(t, satisfiedFixture)
	assert.InDelta(t, 4.0, areaOf(t, root, "site:S-901"), 1e-9, "the store is two metres square")

	graph := authored(t, root, func(tx *Tx) error {
		_, err := tx.Apply(batched(t, rerouted))
		return err
	})

	assert.Equal(t,
		[]ID{"geom:E-13", "geom:E-14", "geom:E-30", "geom:E-31", "geom:E-16"},
		loopOf(t, graph, "geom:L-13").Edges(),
	)

	// The square and the triangle the new corner adds above its north side:
	// two by two, and half of two by one.
	assert.InDelta(t, 5.0, areaOf(t, root, "site:S-901"), 1e-9)
}

// TestTxSetEdgesRoundTrips checks the ring as a property of the file it lands
// in rather than as a literal: the file is written in canonical form, so reading
// it and printing it again gives back the bytes on disk, and the edges print in
// the order they were given rather than in a sorted one.
func TestTxSetEdgesRoundTrips(t *testing.T) {
	root := copied(t, satisfiedFixture)

	authored(t, root, func(tx *Tx) error {
		return tx.SetEdges("geom:L-13", []ID{"geom:E-16", "geom:E-15", "geom:E-14", "geom:E-13"})
	})

	path := filepath.Join(root, "model.dfc")
	src, err := os.ReadFile(path)
	require.NoError(t, err)

	file, err := Parse(path, bytes.NewReader(src))
	require.NoError(t, err)

	var printed bytes.Buffer
	require.NoError(t, Print(&printed, file))
	assert.Equal(t, string(src), printed.String(), "the file is written in canonical form")

	assert.Contains(t, string(src), "(edges geom:E-16 geom:E-15 geom:E-14 geom:E-13)")

	graph, diags := LoadGraph(root)
	require.Empty(t, diags)
	assert.Equal(t,
		[]ID{"geom:E-16", "geom:E-15", "geom:E-14", "geom:E-13"},
		loopOf(t, graph, "geom:L-13").Edges(),
	)
}

// TestTxSetEdgesLeavesTheRestOfTheLoop is its own function because it needs a
// loop carrying what the fixture's loops do not — a comment inside the form and
// inside its ring — each of which the change has no business touching.
func TestTxSetEdgesLeavesTheRestOfTheLoop(t *testing.T) {
	root := copied(t, satisfiedFixture)

	model := filepath.Join(root, "model.dfc")
	written, err := os.ReadFile(model)
	require.NoError(t, err)
	edited := strings.Replace(string(written), `  (edges geom:E-13 geom:E-14 geom:E-15 geom:E-16)`, `  ; Surveyed again after the 2026 refit.
  (edges geom:E-13 geom:E-14 geom:E-15 geom:E-16)`, 1)
	require.NotEqual(t, string(written), edited, "the fixture has the loop this test edits")
	require.NoError(t, os.WriteFile(model, []byte(edited), 0o644))

	before, diags := LoadGraph(root)
	require.Empty(t, diags, "the edited fixture loads")
	original := loopOf(t, before, "geom:L-13")
	require.NotEmpty(t, original.Assertions())

	graph := authored(t, root, func(tx *Tx) error {
		return tx.SetEdges("geom:L-13", []ID{"geom:E-14", "geom:E-15", "geom:E-16", "geom:E-13"})
	})

	loop := loopOf(t, graph, "geom:L-13")
	assert.Equal(t, original.Label(), loop.Label())
	assertSameAssertions(t, original, loop)

	src, err := os.ReadFile(model)
	require.NoError(t, err)
	assert.Contains(t, string(src), "; Surveyed again after the 2026 refit.")
}

func TestTxSetEdgesRefusesTheInvocation(t *testing.T) {
	testCases := []struct {
		name   string
		loop   ID
		edges  []ID
		assert func(t *testing.T, err error)
	}{
		{
			name:  "refuses an id nothing holds, naming the nearest",
			loop:  "geom:L-I3",
			edges: []ID{"geom:E-13"},
			assert: func(t *testing.T, err error) {
				var unknown UnknownEntityError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, ID("geom:L-I3"), unknown.ID)
				assert.Equal(t, ID("geom:L-13"), unknown.Nearest)
			},
		},
		{
			name:  "refuses an id naming an edge",
			loop:  "geom:E-13",
			edges: []ID{"geom:E-13"},
			assert: func(t *testing.T, err error) {
				var family NotOfFamilyError
				require.ErrorAs(t, err, &family)
				assert.Equal(t, ID("geom:E-13"), family.ID)
				assert.Equal(t, loopTag, family.Want)
				assert.Equal(t, edgeTag, family.Got)
			},
		},
		{
			name:  "refuses an id naming a semantic node",
			loop:  "site:S-901",
			edges: []ID{"geom:E-13"},
			assert: func(t *testing.T, err error) {
				var family NotOfFamilyError
				require.ErrorAs(t, err, &family)
				assert.Equal(t, ID("site:S-901"), family.ID)
				assert.Equal(t, loopTag, family.Want)
				assert.Equal(t, nodeTag, family.Got)
			},
		},
		{
			name:  "refuses an edge nothing holds, naming the nearest",
			loop:  "geom:L-13",
			edges: []ID{"geom:E-13", "geom:E-14", "geom:E-155", "geom:E-16"},
			assert: func(t *testing.T, err error) {
				var unknown UnknownEntityError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, ID("geom:E-155"), unknown.ID)
				assert.Equal(t, ID("geom:E-15"), unknown.Nearest)
			},
		},
		{
			name:  "refuses an edge id naming a vertex",
			loop:  "geom:L-13",
			edges: []ID{"geom:E-13", "geom:V-11", "geom:E-16"},
			assert: func(t *testing.T, err error) {
				var family NotOfFamilyError
				require.ErrorAs(t, err, &family)
				assert.Equal(t, ID("geom:V-11"), family.ID)
				assert.Equal(t, edgeTag, family.Want)
				assert.Equal(t, vertexTag, family.Got)
			},
		},
		{
			name:  "refuses an edge id naming a loop",
			loop:  "geom:L-13",
			edges: []ID{"geom:L-14"},
			assert: func(t *testing.T, err error) {
				var family NotOfFamilyError
				require.ErrorAs(t, err, &family)
				assert.Equal(t, ID("geom:L-14"), family.ID)
				assert.Equal(t, edgeTag, family.Want)
				assert.Equal(t, loopTag, family.Got)
			},
		},
		{
			name:  "refuses an edge id in a namespace the registry does not declare",
			loop:  "geom:L-13",
			edges: []ID{"shape:E-13"},
			assert: func(t *testing.T, err error) {
				var unknown UnknownAxisError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, "shape", unknown.Value)
				assert.Contains(t, unknown.Permitted, "geom")
			},
		},
		{
			name:  "refuses an empty list",
			loop:  "geom:L-13",
			edges: []ID{},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrNoEdges)
			},
		},
		{
			name:  "refuses no list at all",
			loop:  "geom:L-13",
			edges: nil,
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrNoEdges)
			},
		},
		{
			name:  "refuses an empty id",
			loop:  "",
			edges: []ID{"geom:E-13"},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrNoID)
			},
		},
		{
			name:  "refuses an edge named as the empty id",
			loop:  "geom:L-13",
			edges: []ID{"geom:E-13", ""},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrNoID)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, satisfiedFixture)

			err := rejected(t, root, func(tx *Tx) error {
				return tx.SetEdges(testCase.loop, testCase.edges)
			})

			testCase.assert(t, err)
		})
	}
}

// TestTxSetEdgesWritesARingWhichDoesNotClose checks that closure is not judged
// at commit, as it is not for [Tx.AddLoop]: the load has no closure judgment,
// and one at commit would need a registry tolerance the write path has no way to
// choose. The ring is written, and the loop's own boundary-loops-close assertion
// is what reports it — which is the mutation a harness proving that check can
// fail writes.
func TestTxSetEdgesWritesARingWhichDoesNotClose(t *testing.T) {
	testCases := []struct {
		name  string
		edges []ID
	}{
		{
			name:  "writes a ring with a side left out",
			edges: []ID{"geom:E-13", "geom:E-14", "geom:E-15"},
		},
		{
			name:  "writes a ring in two pieces",
			edges: []ID{"geom:E-13", "geom:E-14", "geom:E-15", "geom:E-16", "geom:E-17", "geom:E-18", "geom:E-19", "geom:E-20"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, satisfiedFixture)

			before, diags := LoadGraph(root)
			require.Empty(t, diags)
			require.False(t, slices.ContainsFunc(before.CheckAssertions(), violatesClosure("geom:L-13")),
				"the store closes before the change")

			graph := authored(t, root, func(tx *Tx) error {
				return tx.SetEdges("geom:L-13", testCase.edges)
			})

			assert.Equal(t, testCase.edges, loopOf(t, graph, "geom:L-13").Edges())
			assert.True(t, slices.ContainsFunc(graph.CheckAssertions(), violatesClosure("geom:L-13")),
				"the loop's boundary-loops-close assertion reports the ring")
		})
	}
}

// violatesClosure reports a violation of boundary-loops-close on loop.
func violatesClosure(loop ID) func(Violation) bool {
	return func(violation Violation) bool {
		return violation.Instance == loop && violation.Check == "boundary-loops-close"
	}
}

// TestTxSetEdgesIsRefusedByTheModelItWouldProduce checks that whether the edges
// are in the loop's frame is judged by the load of the result rather than by a
// second copy of the rules here, so that the same ring typed into a file by hand
// is refused in the same words (specification sections 6.4 and 7.5.1).
func TestTxSetEdgesIsRefusedByTheModelItWouldProduce(t *testing.T) {
	root := copied(t, satisfiedFixture)
	before := contents(t, root)

	tx := begin(t, root)

	// Two corners and an edge in the annex's frame, written by the same change
	// and so resolving as edges; the ring naming one is in two frames at once.
	for _, vertex := range []ID{"geom:V-40", "geom:V-41"} {
		require.NoError(t, tx.AddVertex(VertexSpec{ID: vertex, Frame: "frame:annex"}, "model.dfc"))
	}
	annex := EdgeSpec{ID: "geom:E-40", Frame: "frame:annex", Start: "geom:V-40", End: "geom:V-41"}
	require.NoError(t, tx.AddEdge(annex, "model.dfc"))
	require.NoError(t, tx.SetEdges("geom:L-13", []ID{"geom:E-13", "geom:E-14", "geom:E-40", "geom:E-16"}))

	out, diags, err := tx.Commit()
	require.NoError(t, err)
	assert.Empty(t, out.Files, "a refused change describes nothing")
	assert.Equal(t, before, contents(t, root), "a refused change writes nothing")

	var collected Diagnostics
	collected.Add(diags...)
	require.True(t, collected.HasErrors(), "the change was refused")

	assert.True(t, slices.ContainsFunc(diags, func(diagnostic Diagnostic) bool {
		return strings.Contains(diagnostic.Message, "geom:E-40") && strings.Contains(diagnostic.Message, "frame:annex")
	}), "the diagnostics name the edge in the other frame: %v", messages(diags))
}

func TestTxSetEdgesOnAFinishedTransaction(t *testing.T) {
	tx := begin(t, copied(t, satisfiedFixture))

	_, _, err := tx.Commit()
	require.NoError(t, err)

	assert.ErrorIs(t, tx.SetEdges("geom:L-13", []ID{"geom:E-13"}), ErrFinished)
}

// TestTxSetEdgesNamesWhatTheSameChangeWrote checks that edges and a loop written
// earlier in the same change count, which is what lets a batch draw an edge and
// route a ring through it, or write a loop and correct it.
func TestTxSetEdgesNamesWhatTheSameChangeWrote(t *testing.T) {
	root := copied(t, satisfiedFixture)

	graph := authored(t, root, func(tx *Tx) error {
		// The store's diagonal, and a triangle through it which the change
		// writes and then restates walked the other way.
		diagonal := EdgeSpec{ID: "geom:E-30", Frame: "frame:building", Start: "geom:V-11", End: "geom:V-09"}
		if err := tx.AddEdge(diagonal, "model.dfc"); err != nil {
			return err
		}

		loop := LoopSpec{
			ID:    "geom:L-30",
			Frame: "frame:building",
			Edges: []ID{"geom:E-13", "geom:E-14", "geom:E-30"},
		}
		if err := tx.AddLoop(loop, "model.dfc"); err != nil {
			return err
		}

		return tx.SetEdges("geom:L-30", []ID{"geom:E-30", "geom:E-14", "geom:E-13"})
	})

	assert.Equal(t, []ID{"geom:E-30", "geom:E-14", "geom:E-13"}, loopOf(t, graph, "geom:L-30").Edges())
}

// TestTxApplyRefusesASetEdgesBuiltWithoutItsRing checks a batch built in code
// rather than read from a file, which [ParseBatch] never checked: the absent
// ring is refused as the parse would have refused it.
func TestTxApplyRefusesASetEdgesBuiltWithoutItsRing(t *testing.T) {
	root := copied(t, satisfiedFixture)

	err := rejected(t, root, func(tx *Tx) error {
		_, err := tx.Apply(Batch{Version: BatchVersion, Operations: []Operation{&SetEdgesOperation{ID: "geom:L-13"}}})
		return err
	})

	var problem OperationError
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, 1, problem.Index)
	assert.Equal(t, "set-edges", problem.Operation)
	assert.ErrorIs(t, err, ErrNoEdges)
}
