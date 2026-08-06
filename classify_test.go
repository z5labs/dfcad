// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// classifiedEdge is one edge of a region's boundary flattened for comparison:
// its id, what it classified as, and the ids of the elements which back it.
//
// Ids rather than pointers, because the cases which matter here load the same
// model twice and a node loaded twice is two pointers to the same thing.
type classifiedEdge struct {
	edge           ID
	classification Classification
	backing        []ID
}

// classifyRegion is the boundary of one region, classified and flattened, in the
// order the query gave it.
func classifyRegion(boundaries *Boundaries, region *SemanticNode) []classifiedEdge {
	var classified []classifiedEdge

	for boundary := range boundaries.Classify(region) {
		var backing []ID
		for _, element := range boundary.Backing() {
			backing = append(backing, element.ID())
		}

		classified = append(classified, classifiedEdge{
			edge:           boundary.Edge().ID(),
			classification: boundary.Classification(),
			backing:        backing,
		})
	}

	return classified
}

// joinBoundaries loads one fixture and joins its two families, failing the test
// where the join reports anything.
func joinBoundaries(t *testing.T, name string) (boundaryModel, *Boundaries) {
	t.Helper()

	model := loadBoundaryModel(t, name)

	boundaries, diags := ResolveBoundaries(model.nodes, model.topology)
	require.Empty(t, renderBoundaryDiagnostics(t, diags), "the fixture joins clean")

	return model, boundaries
}

// TestClassifyBoundaryEdges is its own function because it asserts on a
// traversal rather than on a diagnostic: what separates a region from what is on
// the other side of each of its edges, computed from whether the edge names an
// element the model holds.
func TestClassifyBoundaryEdges(t *testing.T) {
	model, boundaries := joinBoundaries(t, "backed")

	room, ok := model.nodes.Node("site:S-101")
	require.True(t, ok)

	corridor, ok := model.nodes.Node("site:S-102")
	require.True(t, ok)

	t.Run("gives every edge of the region's boundary with its classification", func(t *testing.T) {
		assert.Equal(t, []classifiedEdge{
			{edge: "geom:E-01", classification: ClassificationVirtual},
			{edge: "geom:E-02", classification: ClassificationPhysical, backing: []ID{"site:W-14", "site:W-15"}},
			{edge: "geom:E-03", classification: ClassificationVirtual},
			{edge: "geom:E-04", classification: ClassificationVirtual},
		}, classifyRegion(boundaries, room))
	})

	t.Run("gives the same edge the same answer from the other region which reaches it", func(t *testing.T) {
		// The shared partition is one node with one identity, so what realises
		// it does not depend on which side is asking.
		assert.Equal(t, []classifiedEdge{
			{edge: "geom:E-05", classification: ClassificationPhysical, backing: []ID{"site:W-16"}},
			{edge: "geom:E-06", classification: ClassificationVirtual},
			{edge: "geom:E-07", classification: ClassificationVirtual},
			{edge: "geom:E-02", classification: ClassificationPhysical, backing: []ID{"site:W-14", "site:W-15"}},
		}, classifyRegion(boundaries, corridor))
	})

	t.Run("names the element backing a physical edge", func(t *testing.T) {
		edge, ok := model.topology.Edge("geom:E-05")
		require.True(t, ok)

		partition, ok := model.nodes.Node("site:W-16")
		require.True(t, ok)

		classified := boundaries.Classified(edge)

		assert.True(t, classified.Physical())
		assert.Equal(t, ClassificationPhysical, classified.Classification())

		// Not merely equal: the element the rest of the model reaches. An
		// element which happened to hold an equal id would satisfy an equality
		// assertion and would still be a different node.
		require.Len(t, classified.Backing(), 1)
		assert.Same(t, partition, classified.Backing()[0])
	})

	t.Run("names every element of an edge which references more than one", func(t *testing.T) {
		edge, ok := model.topology.Edge("geom:E-02")
		require.True(t, ok)

		classified := boundaries.Classified(edge)

		// One resolving reference is what makes the edge physical, and every
		// element it names is named: a stud wall with a glazed screen over it is
		// two things, and reporting one of them answers with half the wall.
		assert.True(t, classified.Physical())

		var backing []ID
		for _, element := range classified.Backing() {
			backing = append(backing, element.ID())
		}

		assert.Equal(t, []ID{"site:W-14", "site:W-15"}, backing)
	})

	t.Run("names nothing for an edge no element backs", func(t *testing.T) {
		edge, ok := model.topology.Edge("geom:E-01")
		require.True(t, ok)

		classified := boundaries.Classified(edge)

		assert.False(t, classified.Physical())
		assert.Equal(t, ClassificationVirtual, classified.Classification())
		assert.Empty(t, classified.Backing())
	})
}

// TestClassificationIsNeverStored is its own function because it asserts on the
// format rather than on a model: there is no flag anywhere in it which says
// whether a boundary is physical, so there is nothing which can disagree with
// the references it is computed from
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// Walking the form tables is what makes it a rule rather than a comment. A form
// which gained a child storing the answer would fail here, which is where
// somebody adding one is looking.
//
// The walk is over the entity forms and not over the registry ones. A boundary
// classification is a property of one edge of one region's boundary, so an
// entity form is the only place a file could state one — and the registry's
// `classification` of specification section 7.3 is a different word for a
// different thing entirely: how a scheme outside this model names a *type*, a
// pair of opaque strings which says nothing about any edge. Sweeping the
// registry in as well would make this test refuse that spelling for no reason
// but the letters in it.
func TestClassificationIsNeverStored(t *testing.T) {
	stores := []string{"physical", "virtual", "classification"}

	// Every child tag of every entity form, plus the reserved set the tables
	// produce, which is the whole of what an entity file may write.
	tags := slices.Sorted(maps.Keys(forms().reserved))

	seen := make(map[*form]bool)
	var walk func(f *form)
	walk = func(f *form) {
		if f == nil || seen[f] {
			return
		}
		seen[f] = true

		for _, c := range f.children {
			tags = append(tags, c.tag)
			walk(c.form)
		}
		walk(f.claims)
	}
	for _, entity := range []*form{nodeForm, vertexForm, edgeForm, loopForm} {
		walk(entity)
	}

	for _, tag := range tags {
		for _, word := range stores {
			assert.NotContains(t, tag, word, "no form carries a flag saying whether a boundary is physical")
		}
	}

	t.Run("computes an answer the file never says", func(t *testing.T) {
		model, boundaries := joinBoundaries(t, "backed")

		edge, ok := model.topology.Edge("geom:E-02")
		require.True(t, ok)

		require.Equal(t, ClassificationPhysical, boundaries.Classified(edge).Classification())

		source, err := os.ReadFile(filepath.Join(boundaryFixture("backed"), "model.dfc"))
		require.NoError(t, err)

		// The words appear in the file's opening comment, saying they are not in
		// it. Everything a reader is judged on is what is left with the comments
		// taken out.
		for _, line := range strings.Split(string(source), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), ";") {
				continue
			}

			for _, word := range stores {
				assert.NotContains(t, line, word, "the model says what backs an edge and never what that makes it")
			}
		}
	})
}

// TestAddingABackingElementFlipsTheClassification is its own function because
// the assertion is about two loads rather than one: the same model with one
// `(backed-by ...)` line and without it, which has to be the whole of the
// difference between a virtual boundary and a physical one.
//
// This is what "computed, never stored" buys. An author adds a wall, and the
// boundary it realises becomes physical with no edit to the edge's
// classification, to the region which reaches it, or to anything else.
func TestAddingABackingElementFlipsTheClassification(t *testing.T) {
	// The line as it is written, taken with the newline before it rather than
	// the one after, because it is the last child of its form and the bracket
	// which closes the edge is on the end of it.
	const added = "\n  (backed-by site:W-16)"

	before := loadBoundaryModelAt(t, withoutLine(t, boundaryFixture("backed"), added))
	after := loadBoundaryModel(t, "backed")

	corridor := func(model boundaryModel) (*Boundaries, *SemanticNode) {
		t.Helper()

		boundaries, diags := ResolveBoundaries(model.nodes, model.topology)
		require.Empty(t, renderBoundaryDiagnostics(t, diags), "both models join clean")

		node, ok := model.nodes.Node("site:S-102")
		require.True(t, ok)

		return boundaries, node
	}

	was := classifyRegion(corridor(before))
	is := classifyRegion(corridor(after))

	require.Len(t, was, len(is))

	t.Run("flips the edge the element was added to", func(t *testing.T) {
		assert.Equal(t, classifiedEdge{edge: "geom:E-05", classification: ClassificationVirtual}, was[0])
		assert.Equal(t, classifiedEdge{
			edge:           "geom:E-05",
			classification: ClassificationPhysical,
			backing:        []ID{"site:W-16"},
		}, is[0])
	})

	t.Run("leaves every other edge of the boundary as it was", func(t *testing.T) {
		assert.Equal(t, was[1:], is[1:])
	})
}

// TestBackingWhichDoesNotResolveIsNotVirtual is its own function because it
// asserts on a classification and a diagnostic together: an edge which says
// something backs it and names nothing the model holds is a load error, and the
// query says so rather than answering "virtual" and losing the question.
func TestBackingWhichDoesNotResolveIsNotVirtual(t *testing.T) {
	model := loadBoundaryModel(t, "dangling-backing")

	boundaries, diags := ResolveBoundaries(model.nodes, model.topology)
	require.NotEmpty(t, diags, "every reference which failed is reported")

	for _, diag := range diags {
		assert.Equal(t, SeverityError, diag.Severity, "an unresolved backing is a load error")
	}

	testCases := []struct {
		name    string
		edge    ID
		want    Classification
		backing []ID
	}{
		{
			// Something real does back it, so the question the model settled is
			// answered from what it settled. The references which failed are
			// reported, and none of them makes the wall which is there stop being
			// there.
			name:    "stays physical where one of the elements the edge names is reached",
			edge:    "geom:E-01",
			want:    ClassificationPhysical,
			backing: []ID{"site:W-14"},
		},
		{
			name: "is neither physical nor virtual where none of them is reached",
			edge: "geom:E-02",
			want: ClassificationUnresolved,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			edge, ok := model.topology.Edge(testCase.edge)
			require.True(t, ok)

			classified := boundaries.Classified(edge)

			assert.Equal(t, testCase.want, classified.Classification())
			assert.NotEqual(t, ClassificationVirtual, classified.Classification())

			var backing []ID
			for _, element := range classified.Backing() {
				backing = append(backing, element.ID())
			}

			assert.Equal(t, testCase.backing, backing)
		})
	}
}

// TestEdgeReferencesAnElementRatherThanAFlag is its own function because it
// asserts on what the loaded edge carries rather than on what a query computes:
// the ids as they were written, with an element named twice held once.
func TestEdgeReferencesAnElementRatherThanAFlag(t *testing.T) {
	model := loadBoundaryModel(t, "dangling-backing")

	edge, ok := model.topology.Edge("geom:E-01")
	require.True(t, ok)

	// Written four times, with one of them a repeat. What a caller reads is the
	// set of elements the edge references, and the repeat is a diagnostic rather
	// than a second thing holding the wall up.
	assert.Equal(t, []ID{"site:W-14", "site:S-101", "site:W-99"}, edge.BackedBy())
}

// withoutLine copies a fixture into a directory of its own with one line
// deleted, and returns the root of the copy.
//
// The line has to appear exactly once, so that what the copy differs from the
// fixture by is that one line and nothing else. A case built on "the same model
// without this" is only worth anything if that is genuinely all it is.
func withoutLine(t *testing.T, root string, line string) string {
	t.Helper()

	entries, err := os.ReadDir(root)
	require.NoError(t, err)

	copied := t.TempDir()

	var deleted int
	for _, entry := range entries {
		source, err := os.ReadFile(filepath.Join(root, entry.Name()))
		require.NoError(t, err)

		text := string(source)
		deleted += strings.Count(text, line)

		require.NoError(t, os.WriteFile(
			filepath.Join(copied, entry.Name()),
			[]byte(strings.Replace(text, line, "", 1)),
			0o644,
		))
	}

	require.Equal(t, 1, deleted, "the line the copy is missing is written exactly once")

	return copied
}
