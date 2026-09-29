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

// backedFixture is the model the backing tests change: two rooms sharing a
// partition, with a virtual edge, an edge backed by one element and an edge
// backed by two.
const backedFixture = "testdata/boundary/backed"

// edgeOf is one edge of graph, requiring the model to hold it.
func edgeOf(t *testing.T, graph *Graph, id ID) *Edge {
	t.Helper()

	edge, ok := graph.Topology().Edge(id)
	require.True(t, ok, "the model holds %s", id)

	return edge
}

func TestTxSetBacking(t *testing.T) {
	testCases := []struct {
		name                   string
		edge                   ID
		spec                   BackingSpec
		expectedBacking        []ID
		expectedClassification Classification
	}{
		{
			name:                   "backs a virtual edge with an element, which makes it physical",
			edge:                   "geom:E-01",
			spec:                   BackingSpec{BackedBy: []ID{"site:W-14"}},
			expectedBacking:        []ID{"site:W-14"},
			expectedClassification: ClassificationPhysical,
		},
		{
			name:                   "makes a backed edge virtual",
			edge:                   "geom:E-05",
			spec:                   BackingSpec{Virtual: true},
			expectedClassification: ClassificationVirtual,
		},
		{
			name:                   "replaces the element an edge is backed by with another",
			edge:                   "geom:E-05",
			spec:                   BackingSpec{BackedBy: []ID{"site:W-15"}},
			expectedBacking:        []ID{"site:W-15"},
			expectedClassification: ClassificationPhysical,
		},
		{
			name:                   "replaces every element of an edge backed by two",
			edge:                   "geom:E-02",
			spec:                   BackingSpec{BackedBy: []ID{"site:W-16"}},
			expectedBacking:        []ID{"site:W-16"},
			expectedClassification: ClassificationPhysical,
		},
		{
			name:                   "backs an edge with more than one element",
			edge:                   "geom:E-05",
			spec:                   BackingSpec{BackedBy: []ID{"site:W-16", "site:W-14"}},
			expectedBacking:        []ID{"site:W-14", "site:W-16"},
			expectedClassification: ClassificationPhysical,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, backedFixture)

			before, diags := LoadGraph(root)
			require.Empty(t, diags)
			original := edgeOf(t, before, testCase.edge)

			graph := authored(t, root, func(tx *Tx) error {
				return tx.SetBacking(testCase.edge, testCase.spec)
			})

			edge := edgeOf(t, graph, testCase.edge)

			// The round trip: what the model reads back is the set written,
			// whatever order it was given in.
			assert.ElementsMatch(t, testCase.expectedBacking, edge.BackedBy())

			// And the classification is computed from it, with no other edit.
			assert.Equal(t, testCase.expectedClassification, graph.Classified(edge).Classification())

			// Nothing else about the edge moved.
			start, end := edge.Vertices()
			originalStart, originalEnd := original.Vertices()
			assert.Equal(t, originalStart, start)
			assert.Equal(t, originalEnd, end)
			assert.Equal(t, original.Frame(), edge.Frame())
			assert.Equal(t, original.Label(), edge.Label())
		})
	}
}

// TestTxSetBackingRoundTrips checks the backing as a property of the file it
// lands in rather than as a literal: the file is written in canonical form, so
// reading it and printing it again gives back the bytes on disk, and the
// backings print in canonical order whichever order they were given in.
func TestTxSetBackingRoundTrips(t *testing.T) {
	root := copied(t, backedFixture)

	authored(t, root, func(tx *Tx) error {
		return tx.SetBacking("geom:E-01", BackingSpec{BackedBy: []ID{"site:W-15", "site:W-14"}})
	})

	path := filepath.Join(root, "model.dfc")
	src, err := os.ReadFile(path)
	require.NoError(t, err)

	file, err := Parse(path, bytes.NewReader(src))
	require.NoError(t, err)

	var printed bytes.Buffer
	require.NoError(t, Print(&printed, file))
	assert.Equal(t, string(src), printed.String(), "the file is written in canonical form")

	assert.Contains(t, string(src), `(edge
  geom:E-01
  (label "Room B, north opening")
  (frame frame:building)
  (vertices geom:V-01 geom:V-02)
  (backed-by site:W-14)
  (backed-by site:W-15))`)
}

// TestTxSetBackingLeavesTheRestOfTheEdge is its own function because it needs an
// edge carrying what the fixture's edges do not: a claim, an assertion and a
// comment, each of which the change has no business touching.
func TestTxSetBackingLeavesTheRestOfTheEdge(t *testing.T) {
	root := copied(t, backedFixture)

	registry := filepath.Join(root, "registry.dfc")
	declared, err := os.ReadFile(registry)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(registry, append(declared, []byte(`
(namespace method (description "Measurement methods."))

(predicate clearance
  (unit m)
  (shape scalar)
  (description "The clear height beneath a thing."))
`)...), 0o644))

	model := filepath.Join(root, "model.dfc")
	written, err := os.ReadFile(model)
	require.NoError(t, err)
	edited := strings.Replace(string(written), `  (backed-by site:W-16))`, `  ; The wall came down in the 2026 refit.
  (backed-by site:W-16)
  (clearance
    (value 2.4 m)
    (source "As-built check AB-2026-009, Acme Surveys")
    (method method:tape)
    (accuracy (independent 0.008 m))
    (date "2026-05-06"))
  (assert edge-backing-resolves))`, 1)
	require.NotEqual(t, string(written), edited, "the fixture has the edge this test edits")
	require.NoError(t, os.WriteFile(model, []byte(edited), 0o644))

	before, diags := LoadGraph(root)
	require.Empty(t, diags, "the edited fixture loads")
	original := edgeOf(t, before, "geom:E-05")
	originalClaims := slices.Collect(before.Claims().Of("geom:E-05"))
	require.NotEmpty(t, originalClaims)

	graph := authored(t, root, func(tx *Tx) error {
		return tx.SetBacking("geom:E-05", BackingSpec{Virtual: true})
	})

	edge := edgeOf(t, graph, "geom:E-05")
	assert.Empty(t, edge.BackedBy())
	assert.Equal(t, original.Assertions()[0].Check, edge.Assertions()[0].Check)
	assert.Len(t, edge.Assertions(), len(original.Assertions()))

	claims := slices.Collect(graph.Claims().Of("geom:E-05"))
	require.Len(t, claims, len(originalClaims))
	for at, claim := range claims {
		assert.Equal(t, originalClaims[at].Predicate(), claim.Predicate())
		assert.Equal(t, originalClaims[at].Source(), claim.Source())
	}

	src, err := os.ReadFile(model)
	require.NoError(t, err)
	assert.Contains(t, string(src), "; The wall came down in the 2026 refit.")
}

func TestTxSetBackingRefusesTheInvocation(t *testing.T) {
	testCases := []struct {
		name   string
		edge   ID
		spec   BackingSpec
		assert func(t *testing.T, err error)
	}{
		{
			name: "refuses an id nothing holds, naming the nearest",
			edge: "geom:E-O5",
			spec: BackingSpec{Virtual: true},
			assert: func(t *testing.T, err error) {
				var unknown UnknownEntityError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, ID("geom:E-O5"), unknown.ID)
				assert.Equal(t, ID("geom:E-05"), unknown.Nearest)
			},
		},
		{
			name: "refuses an id naming a vertex",
			edge: "geom:V-01",
			spec: BackingSpec{Virtual: true},
			assert: func(t *testing.T, err error) {
				var family NotOfFamilyError
				require.ErrorAs(t, err, &family)
				assert.Equal(t, ID("geom:V-01"), family.ID)
				assert.Equal(t, edgeTag, family.Want)
				assert.Equal(t, vertexTag, family.Got)
			},
		},
		{
			name: "refuses an id naming a semantic node",
			edge: "site:W-16",
			spec: BackingSpec{Virtual: true},
			assert: func(t *testing.T, err error) {
				var family NotOfFamilyError
				require.ErrorAs(t, err, &family)
				assert.Equal(t, ID("site:W-16"), family.ID)
				assert.Equal(t, edgeTag, family.Want)
				assert.Equal(t, nodeTag, family.Got)
			},
		},
		{
			name: "refuses a backing which is both virtual and backed",
			edge: "geom:E-05",
			spec: BackingSpec{BackedBy: []ID{"site:W-14"}, Virtual: true},
			assert: func(t *testing.T, err error) {
				var conflicting ConflictingBackingError
				require.ErrorAs(t, err, &conflicting)
				assert.Equal(t, ID("geom:E-05"), conflicting.ID)
				assert.Equal(t, []ID{"site:W-14"}, conflicting.BackedBy)
			},
		},
		{
			name: "refuses a backing which states neither",
			edge: "geom:E-05",
			spec: BackingSpec{},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrNoBacking)
			},
		},
		{
			name: "refuses an empty id",
			edge: "",
			spec: BackingSpec{Virtual: true},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrNoID)
			},
		},
		{
			name: "refuses an element named as the empty id",
			edge: "geom:E-05",
			spec: BackingSpec{BackedBy: []ID{"site:W-14", ""}},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrNoID)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, backedFixture)

			err := rejected(t, root, func(tx *Tx) error {
				return tx.SetBacking(testCase.edge, testCase.spec)
			})

			testCase.assert(t, err)
		})
	}
}

// TestTxSetBackingIsRefusedByTheModelItWouldProduce checks that the elements are
// resolved by the load of the result rather than by a second copy of the rules
// here, so that the same mistake typed into a file by hand is refused in the
// same words (specification section 6.3).
func TestTxSetBackingIsRefusedByTheModelItWouldProduce(t *testing.T) {
	testCases := []struct {
		name     string
		backing  []ID
		expected string
	}{
		{
			name:     "an element nothing in the model holds",
			backing:  []ID{"site:W-99"},
			expected: "found site:W-99, which names no node",
		},
		{
			name:     "an element which is geometry",
			backing:  []ID{"geom:V-01"},
			expected: "expected an element id, found geom:V-01, which is a vertex",
		},
		{
			name:     "a node which is not an Element",
			backing:  []ID{"site:S-101"},
			expected: "expected a node of kind Element, found site:S-101",
		},
		{
			name:     "an element named twice",
			backing:  []ID{"site:W-14", "site:W-14"},
			expected: "found site:W-14 a second time",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, backedFixture)
			before := contents(t, root)

			tx := begin(t, root)
			require.NoError(t, tx.SetBacking("geom:E-01", BackingSpec{BackedBy: testCase.backing}))

			out, diags, err := tx.Commit()
			require.NoError(t, err)
			assert.Empty(t, out.Files, "a refused change describes nothing")
			assert.Equal(t, before, contents(t, root), "a refused change writes nothing")

			var collected Diagnostics
			collected.Add(diags...)
			require.True(t, collected.HasErrors(), "the change was refused")

			assert.True(t, slices.ContainsFunc(diags, func(diagnostic Diagnostic) bool {
				return strings.Contains(diagnostic.Message, testCase.expected)
			}), "the diagnostics say what the backing produced: %v", diags)
		})
	}
}

// TestTxSetBackingThenRetireDemolishesAnElement is the demolition the command
// exists for: the edge stops naming the wall, and then the wall is retired, as
// two changes.
func TestTxSetBackingThenRetireDemolishesAnElement(t *testing.T) {
	root := copied(t, backedFixture)

	authored(t, root, func(tx *Tx) error {
		return tx.SetBacking("geom:E-05", BackingSpec{Virtual: true})
	})

	graph := authored(t, root, func(tx *Tx) error {
		return tx.Retire("site:W-16", RetirementSpec{Reason: "Partition demolished"})
	})

	wall, ok := graph.Node("site:W-16")
	require.True(t, ok, "retiring is not deleting")

	retirement, ok := wall.Retirement()
	require.True(t, ok)
	assert.Equal(t, "Partition demolished", retirement.Reason())

	assert.Equal(t, ClassificationVirtual, graph.Classified(edgeOf(t, graph, "geom:E-05")).Classification())
}

// TestTxRetireReadsTheReferencesTheChangeFound is the reason the demolition is
// two changes rather than one: a retirement reads the references of the model
// as the change found it, so an edge made virtual earlier in the same change
// still names the element being retired.
func TestTxRetireReadsTheReferencesTheChangeFound(t *testing.T) {
	root := copied(t, backedFixture)

	err := rejected(t, root, func(tx *Tx) error {
		if err := tx.SetBacking("geom:E-05", BackingSpec{Virtual: true}); err != nil {
			return err
		}
		return tx.Retire("site:W-16", RetirementSpec{Reason: "Partition demolished"})
	})

	var referenced ReferencedError
	require.ErrorAs(t, err, &referenced)
	assert.Equal(t, ID("site:W-16"), referenced.ID)
}

func TestTxSetBackingOnAFinishedTransaction(t *testing.T) {
	tx := begin(t, copied(t, backedFixture))

	_, _, err := tx.Commit()
	require.NoError(t, err)

	assert.ErrorIs(t, tx.SetBacking("geom:E-05", BackingSpec{Virtual: true}), ErrFinished)
}

// TestTxSetBackingNamesAnEdgeTheSameChangeWrote checks that an edge written
// earlier in the same change counts, which is what lets a batch draw an edge
// and then back it.
func TestTxSetBackingNamesAnEdgeTheSameChangeWrote(t *testing.T) {
	root := copied(t, backedFixture)

	graph := authored(t, root, func(tx *Tx) error {
		spec := EdgeSpec{ID: "geom:E-08", Frame: "frame:building", Start: "geom:V-05", End: "geom:V-01"}
		if err := tx.AddEdge(spec, "model.dfc"); err != nil {
			return err
		}
		return tx.SetBacking("geom:E-08", BackingSpec{BackedBy: []ID{"site:W-16"}})
	})

	assert.Equal(t, []ID{"site:W-16"}, edgeOf(t, graph, "geom:E-08").BackedBy())
}
