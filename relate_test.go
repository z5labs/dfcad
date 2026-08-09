// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTxRelate(t *testing.T) {
	testCases := []struct {
		name             string
		id               ID
		spec             RelationSpec
		expectedWithin   ID
		expectedMemberOf []ID
		expectedBoundary []ID
	}{
		{
			name:           "writes what strictly contains a node",
			id:             "site:B-02",
			spec:           RelationSpec{Within: "site:P-01"},
			expectedWithin: "site:P-01",
		},
		{
			name:             "writes a zone the node is grouped into",
			id:               "site:B-01",
			spec:             RelationSpec{MemberOf: []ID{"site:Z-01"}},
			expectedWithin:   "site:P-01",
			expectedMemberOf: []ID{"site:Z-01"},
		},
		{
			// The order the zones come back in is canonical form's rather
			// than the invocation's: the printer sorts the children of every
			// form, so a node grouped into two zones reads the same whichever
			// order they were named in.
			name:             "writes every zone it was given",
			id:               "site:B-01",
			spec:             RelationSpec{MemberOf: []ID{"site:Z-02", "site:Z-01"}},
			expectedWithin:   "site:P-01",
			expectedMemberOf: []ID{"site:Z-01", "site:Z-02"},
		},
		{
			name:             "adds a zone beside the ones the node already declares",
			id:               "site:L-01",
			spec:             RelationSpec{MemberOf: []ID{"site:Z-02"}},
			expectedWithin:   "site:B-01",
			expectedMemberOf: []ID{"site:Z-01", "site:Z-02"},
		},
		{
			name:             "replaces the one parent rather than writing a second beside it",
			id:               "site:L-01",
			spec:             RelationSpec{Within: "site:B-02"},
			expectedWithin:   "site:B-02",
			expectedMemberOf: []ID{"site:Z-01"},
		},
		{
			name: "writes all three relations in one change",
			id:   "site:B-02",
			spec: RelationSpec{
				Within:   "site:P-01",
				MemberOf: []ID{"site:Z-01"},
				Boundary: []ID{"geom:L-01"},
			},
			expectedWithin:   "site:P-01",
			expectedMemberOf: []ID{"site:Z-01"},
			expectedBoundary: []ID{"geom:L-01"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := relateFixture(t)

			graph := authored(t, root, func(tx *Tx) error {
				return tx.Relate(testCase.id, testCase.spec)
			})

			node, ok := graph.Node(testCase.id)
			require.True(t, ok, "the model holds %s", testCase.id)

			within, _ := node.Within()
			assert.Equal(t, testCase.expectedWithin, within)
			assert.Equal(t, testCase.expectedMemberOf, orNone(node.MemberOf()))
			assert.Equal(t, testCase.expectedBoundary, orNone(node.Boundaries()))
		})
	}
}

func TestTxRelateRefusesTheInvocation(t *testing.T) {
	testCases := []struct {
		name     string
		id       ID
		spec     RelationSpec
		expected func(*testing.T, error)
	}{
		{
			name: "a relation which relates the node to nothing",
			id:   "site:B-01",
			spec: RelationSpec{},
			expected: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrNoRelation)
			},
		},
		{
			name: "an id nothing in the model answers to",
			id:   "site:B-09",
			spec: RelationSpec{Within: "site:P-01"},
			expected: func(t *testing.T, err error) {
				var unknown UnknownEntityError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, ID("site:B-09"), unknown.ID)
			},
		},
		{
			name: "an id naming geometry, which carries none of the three",
			id:   "geom:L-01",
			spec: RelationSpec{Within: "site:P-01"},
			expected: func(t *testing.T, err error) {
				var family NotOfFamilyError
				require.ErrorAs(t, err, &family)
				assert.Equal(t, nodeTag, family.Want)
				assert.Equal(t, loopTag, family.Got)
			},
		},
		{
			name: "a zone named as no id at all",
			id:   "site:B-01",
			spec: RelationSpec{MemberOf: []ID{""}},
			expected: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrNoID)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := relateFixture(t)

			err := rejected(t, root, func(tx *Tx) error {
				return tx.Relate(testCase.id, testCase.spec)
			})

			testCase.expected(t, err)
		})
	}
}

// TestTxRelateIsRefusedByTheModelItWouldProduce checks that a relation the
// model cannot hold is refused at the commit, with the diagnostics a load of
// the result would have raised, rather than by a second copy of the hierarchy
// rules in the write path.
//
// It is the whole reason [Tx.Relate] resolves nothing: a containment authored
// by hand and one written by a command are the same mistake, and an author
// reading two different sentences about it has to learn which layer answered.
func TestTxRelateIsRefusedByTheModelItWouldProduce(t *testing.T) {
	testCases := []struct {
		name     string
		id       ID
		spec     RelationSpec
		expected string
	}{
		{
			name:     "a parent nothing in the model holds",
			id:       "site:B-02",
			spec:     RelationSpec{Within: "site:P-09"},
			expected: "expected a node id something in this model holds, found site:P-09",
		},
		{
			name:     "a parent the hierarchy does not permit",
			id:       "site:B-02",
			spec:     RelationSpec{Within: "site:L-01"},
			expected: "expected a kind the hierarchy permits to contain a Building",
		},
		{
			name:     "a membership naming something which is not a Zone",
			id:       "site:B-02",
			spec:     RelationSpec{MemberOf: []ID{"site:P-01"}},
			expected: "expected a node of kind Zone",
		},
		{
			name:     "a zone the node already names",
			id:       "site:L-01",
			spec:     RelationSpec{MemberOf: []ID{"site:Z-01"}},
			expected: "expected a zone site:L-01 does not already name, found site:Z-01 a second time",
		},
		{
			name:     "a boundary naming something which is not a loop",
			id:       "site:B-02",
			spec:     RelationSpec{Boundary: []ID{"geom:E-01"}},
			expected: "geom:E-01",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := relateFixture(t)
			before := contents(t, root)

			tx := begin(t, root)
			require.NoError(t, tx.Relate(testCase.id, testCase.spec))

			out, diags, err := tx.Commit()
			require.NoError(t, err)
			assert.Empty(t, out.Files, "a refused change describes nothing")
			assert.Equal(t, before, contents(t, root), "a refused change writes nothing")

			var collected Diagnostics
			collected.Add(diags...)
			require.True(t, collected.HasErrors(), "the change was refused")

			assert.True(t, slices.ContainsFunc(diags, func(diagnostic Diagnostic) bool {
				return strings.Contains(diagnostic.Message, testCase.expected)
			}), "the diagnostics say what the relation produced: %v", diags)
		})
	}
}

func TestTxRelateOnAFinishedTransaction(t *testing.T) {
	tx := begin(t, relateFixture(t))

	_, _, err := tx.Commit()
	require.NoError(t, err)

	assert.ErrorIs(t, tx.Relate("site:B-02", RelationSpec{Within: "site:P-01"}), ErrFinished)
}

// TestTxRelateKeepsTheCommentsAroundIt checks that writing a relation onto a
// node leaves everything the form carried which the relation did not touch.
//
// A write command deleting a comment nobody asked it to touch is the failure
// this guards, and it is not visible in the model the change produces: the
// comment means nothing to the engine and everything to whoever wrote it.
func TestTxRelateKeepsTheCommentsAroundIt(t *testing.T) {
	root := relateFixture(t)

	authored(t, root, func(tx *Tx) error {
		return tx.Relate("site:B-02", RelationSpec{Within: "site:P-01"})
	})

	assert.Contains(t, contents(t, root)["entities/site.dfc"], "; The block nobody has placed yet.")
}

func TestRelationSpecRelates(t *testing.T) {
	testCases := []struct {
		name     string
		spec     RelationSpec
		expected bool
	}{
		{name: "says nothing when it names nothing", spec: RelationSpec{}},
		{name: "says something for a containment", spec: RelationSpec{Within: "site:P-01"}, expected: true},
		{name: "says something for a membership", spec: RelationSpec{MemberOf: []ID{"site:Z-01"}}, expected: true},
		{name: "says something for a boundary", spec: RelationSpec{Boundary: []ID{"geom:L-01"}}, expected: true},
		{name: "says nothing for empty lists", spec: RelationSpec{MemberOf: []ID{}, Boundary: []ID{}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, testCase.spec.relates())
		})
	}
}

// relateFixture writes the model the relation tests change and returns its
// root.
//
// It is the authoring fixture with a second zone, a block written within
// nothing and a loop to bound something with, because what is under test is the
// references a node makes rather than the axes it declares.
func relateFixture(t *testing.T) string {
	t.Helper()

	return tree(t, map[string]string{
		"registry.dfc":          relateRegistry,
		"entities/site.dfc":     relateModel,
		"entities/geometry.dfc": relateGeometry,
	})
}

const (
	relateRegistry = relateNamespaces + `
(frame frame:site-grid (label "Site survey grid") (unit m))

(predicate position
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "The location of a vertex in its frame."))

(type Campus
  (kind Zone)
  (geometry absent)
  (description "A group of things administered together."))

(type Plot
  (kind Site)
  (geometry area)
  (description "The ground a project is built on."))

(type OfficeBuilding
  (kind Building)
  (geometry solid)
  (description "A building let as offices."))

(type Level
  (kind Storey)
  (geometry area)
  (description "One floor plate of a building."))
`

	relateNamespaces = `(project
  (label "Relation fixture")
  (globalid-namespace "https://example.org/models/relate"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
`

	relateModel = `(node site:P-01
  (label "Riverside plot")
  (kind Site)
  (type Plot)
  (geometry area)
  (frame frame:site-grid))

(node site:B-01
  (label "Block A")
  (kind Building)
  (type OfficeBuilding)
  (geometry solid)
  (frame frame:site-grid)
  (within site:P-01))

; The block nobody has placed yet.
(node site:B-02
  (label "Block B")
  (kind Building)
  (type OfficeBuilding)
  (geometry solid)
  (frame frame:site-grid))

(node site:L-01
  (label "Level 1")
  (kind Storey)
  (type Level)
  (geometry area)
  (frame frame:site-grid)
  (within site:B-01)
  (member-of site:Z-01))

(node site:Z-01
  (label "West campus")
  (kind Zone)
  (type Campus))

(node site:Z-02
  (label "East campus")
  (kind Zone)
  (type Campus))
`

	relateGeometry = `(vertex geom:V-01 (frame frame:site-grid)
  (position (value (0.0 0.0 0.0) m) (source "IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))

(vertex geom:V-02 (frame frame:site-grid)
  (position (value (4.0 0.0 0.0) m) (source "IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))

(vertex geom:V-03 (frame frame:site-grid)
  (position (value (4.0 3.0 0.0) m) (source "IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-01 (frame frame:site-grid) (vertices geom:V-01 geom:V-02))
(edge geom:E-02 (frame frame:site-grid) (vertices geom:V-02 geom:V-03))
(edge geom:E-03 (frame frame:site-grid) (vertices geom:V-03 geom:V-01))

(loop geom:L-01 (frame frame:site-grid) (edges geom:E-01 geom:E-02 geom:E-03))
`
)

// orNone is a list of ids as a test compares it, which is nil for an empty one
// so that "wrote none" and "wrote an empty list" read as one expectation.
func orNone(ids []ID) []ID {
	if len(ids) == 0 {
		return nil
	}
	return ids
}
