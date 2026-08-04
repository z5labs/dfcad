// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"iter"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// relatedNodes loads the fixture whose two relations hold together, which is
// what the traversals are asked of.
func relatedNodes(t *testing.T) *Nodes {
	t.Helper()

	nodes, rendered := loadNodeFixture(t, "containment")
	require.Empty(t, rendered, "a model whose relations hold together loads clean")

	return nodes
}

// nodeOf reads one node of a load by its id, failing the test where the model
// holds none.
func nodeOf(t *testing.T, nodes *Nodes, id ID) *SemanticNode {
	t.Helper()

	node, ok := nodes.Node(id)
	require.True(t, ok, "the model holds the node %s", id)

	return node
}

// reached collects the ids a traversal yielded, having first required that
// every result was labelled with the relation which should have produced it.
//
// The label is checked here rather than in a test of its own because it is a
// property of every result of every traversal: a helper which dropped it while
// collecting the ids would leave each test below asserting the ids of a walk
// whose meaning it had just thrown away.
func reached(t *testing.T, results iter.Seq[Related], relation Relation) []ID {
	t.Helper()

	var ids []ID
	for result := range results {
		assert.Equal(t, relation, result.Relation())
		require.NotNil(t, result.Node())
		ids = append(ids, result.Node().ID())
	}

	return ids
}

// TestLoadNodesRelations reads the models in which one of the two relations
// does not hold together.
func TestLoadNodesRelations(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
	}{
		{
			name:    "names the rule and both claims when a node is written inside two things",
			fixture: "two-parents",
		},
		{
			name:    "names every node of a containment which never reaches a root",
			fixture: "containment-cycle",
		},
		{
			name:    "names the kind written, the kind it was written inside and where it does belong",
			fixture: "nesting-not-permitted",
		},
		{
			name:    "names the reference which resolves to nothing and the node making it",
			fixture: "dangling-relation",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, got := loadNodeFixture(t, testCase.fixture)

			assert.Equal(t, expectedNodeDiagnostics(t, testCase.fixture, got), got)
		})
	}
}

// TestNodesWithin walks one step outward along containment, which is the
// direction the reference is written in.
func TestNodesWithin(t *testing.T) {
	testCases := []struct {
		name     string
		node     ID
		expected ID
	}{
		{
			name:     "gives the node a containment names as its parent",
			node:     "site:B-01",
			expected: "site:S-01",
		},
		{
			name:     "gives a space the space it is written inside",
			node:     "site:S-101a",
			expected: "site:S-101",
		},
		{
			name:     "gives nothing for a node nothing contains",
			node:     "site:S-01",
			expected: "",
		},
		{
			name:     "gives nothing for a node which is only in zones",
			node:     "site:Z-maint",
			expected: "",
		},
	}

	nodes := relatedNodes(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			parent, ok := nodes.Within(nodeOf(t, nodes, testCase.node))

			if testCase.expected == "" {
				assert.False(t, ok)
				assert.Nil(t, parent.Node())
				return
			}

			require.True(t, ok)
			assert.Equal(t, testCase.expected, parent.Node().ID())
			assert.Equal(t, RelationContainment, parent.Relation())
		})
	}
}

// TestNodesContains walks one step inward along containment, which is the
// direction nothing is written in.
func TestNodesContains(t *testing.T) {
	testCases := []struct {
		name     string
		node     ID
		expected []ID
	}{
		{
			name:     "gives every node written directly inside one, in read order",
			node:     "site:L-01",
			expected: []ID{"site:S-101", "site:S-102", "site:E-01"},
		},
		{
			name:     "gives the nodes of two kinds a space holds",
			node:     "site:S-101",
			expected: []ID{"site:S-101a", "site:I-01"},
		},
		{
			name: "gives nothing for a node nothing is written inside",
			node: "site:S-102",
		},
		{
			name: "gives nothing for a zone, whose members are not its contents",
			node: "site:Z-fire",
		},
	}

	nodes := relatedNodes(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			contained := reached(t, nodes.Contains(nodeOf(t, nodes, testCase.node)), RelationContainment)

			assert.Equal(t, testCase.expected, contained)
		})
	}
}

// TestNodesAncestors follows containment to the root, which is what answers
// which building a thing is in without the caller writing the loop.
func TestNodesAncestors(t *testing.T) {
	testCases := []struct {
		name     string
		node     ID
		expected []ID
	}{
		{
			name:     "gives every node above one, nearest first",
			node:     "site:S-101a",
			expected: []ID{"site:S-101", "site:L-01", "site:B-01", "site:S-01"},
		},
		{
			name:     "stops at the node nothing contains",
			node:     "site:B-01",
			expected: []ID{"site:S-01"},
		},
		{
			name: "gives nothing for a root",
			node: "site:S-01",
		},
		{
			name: "gives nothing for a node in zones only",
			node: "site:Z-maint",
		},
	}

	nodes := relatedNodes(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ancestors := reached(t, nodes.Ancestors(nodeOf(t, nodes, testCase.node)), RelationContainment)

			assert.Equal(t, testCase.expected, ancestors)
		})
	}
}

// TestNodesDescendants follows containment all the way down.
func TestNodesDescendants(t *testing.T) {
	testCases := []struct {
		name     string
		node     ID
		expected []ID
	}{
		{
			name: "gives everything below a node, depth first",
			node: "site:S-01",
			expected: []ID{
				"site:B-01", "site:L-01",
				"site:S-101", "site:S-101a", "site:I-01",
				"site:S-102", "site:E-01",
			},
		},
		{
			name:     "gives the nodes below a space",
			node:     "site:S-101",
			expected: []ID{"site:S-101a", "site:I-01"},
		},
		{
			name: "gives nothing for a leaf",
			node: "site:I-01",
		},
	}

	nodes := relatedNodes(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			descendants := reached(t, nodes.Descendants(nodeOf(t, nodes, testCase.node)), RelationContainment)

			assert.Equal(t, testCase.expected, descendants)
		})
	}
}

// TestNodesZones reads membership from the member's side, which is the side it
// is written on.
func TestNodesZones(t *testing.T) {
	testCases := []struct {
		name     string
		node     ID
		expected []ID
	}{
		{
			name:     "gives every zone a node belongs to, in written order",
			node:     "site:E-01",
			expected: []ID{"site:Z-fire", "site:Z-therm", "site:Z-maint"},
		},
		{
			name:     "gives the zone a zone is itself a member of",
			node:     "site:Z-maint",
			expected: []ID{"site:Z-therm"},
		},
		{
			name: "gives nothing for a node in no zone",
			node: "site:S-101",
		},
	}

	nodes := relatedNodes(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			zones := reached(t, nodes.Zones(nodeOf(t, nodes, testCase.node)), RelationMembership)

			assert.Equal(t, testCase.expected, zones)
		})
	}
}

// TestNodesMembers reads membership from the zone's side, which is the side
// nothing is written on.
func TestNodesMembers(t *testing.T) {
	testCases := []struct {
		name     string
		zone     ID
		expected []ID
	}{
		{
			name:     "gives every node which named the zone, in read order",
			zone:     "site:Z-therm",
			expected: []ID{"site:E-01", "site:Z-maint"},
		},
		{
			name:     "gives the one member of a zone",
			zone:     "site:Z-fire",
			expected: []ID{"site:E-01"},
		},
		{
			name: "gives nothing for a zone nothing named",
			zone: "site:C-01",
		},
	}

	nodes := relatedNodes(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			members := reached(t, nodes.Members(nodeOf(t, nodes, testCase.zone)), RelationMembership)

			assert.Equal(t, testCase.expected, members)
		})
	}
}

// TestRelationsAreNeverEachOther is its own function because it is the whole
// reason the two relations are separate rather than a variation on one of them:
// nothing about where a node is says which groups it is in, and nothing about
// its groups says where it is.
//
// The partition is the case which makes the confusion tempting. It is inside one
// storey and belongs to three zones which overlap it, and a model which blurred
// the two would answer "which zones is this in" with the storey, or "what is in
// this storey" with the zones.
func TestRelationsAreNeverEachOther(t *testing.T) {
	nodes := relatedNodes(t)

	partition := nodeOf(t, nodes, "site:E-01")
	storey := nodeOf(t, nodes, "site:L-01")
	fire := nodeOf(t, nodes, "site:Z-fire")

	t.Run("membership never implies containment", func(t *testing.T) {
		// The partition is a member of three zones and inside none of them.
		zones := reached(t, nodes.Zones(partition), RelationMembership)
		require.Len(t, zones, 3)

		parent, ok := nodes.Within(partition)
		require.True(t, ok)
		assert.Equal(t, ID("site:L-01"), parent.Node().ID())
		assert.NotContains(t, zones, parent.Node().ID())

		// And the zone holding it contains nothing, however many members it has.
		assert.Empty(t, reached(t, nodes.Contains(fire), RelationContainment))
		assert.Empty(t, reached(t, nodes.Descendants(fire), RelationContainment))
		assert.Equal(t, []ID{"site:E-01"}, reached(t, nodes.Members(fire), RelationMembership))
	})

	t.Run("containment never implies zone membership", func(t *testing.T) {
		// The storey holds the partition and is a member of nothing, and the
		// partition's zones are not among what the storey contains.
		assert.Empty(t, reached(t, nodes.Zones(storey), RelationMembership))
		assert.Empty(t, reached(t, nodes.Members(storey), RelationMembership))

		contained := reached(t, nodes.Contains(storey), RelationContainment)
		assert.Contains(t, contained, ID("site:E-01"))
		assert.NotContains(t, contained, ID("site:Z-fire"))

		// Nothing inside the storey inherits the partition's zones either.
		for _, id := range contained {
			if id == "site:E-01" {
				continue
			}
			assert.Empty(t, reached(t, nodes.Zones(nodeOf(t, nodes, id)), RelationMembership))
		}
	})
}

// TestNodeInNeitherRelation is its own function because it is the state the two
// relations exist to leave ordinary rather than a variation on either of them.
//
// A lighting circuit group is inside nothing and grouped with nothing, and a
// model which could not hold one would be a model in which every such thing had
// to be given a place it does not have.
func TestNodeInNeitherRelation(t *testing.T) {
	nodes := relatedNodes(t)

	node := nodeOf(t, nodes, "site:C-01")

	within, hasWithin := node.Within()
	assert.False(t, hasWithin)
	assert.Empty(t, within)
	assert.Empty(t, node.MemberOf())

	parent, ok := nodes.Within(node)
	assert.False(t, ok)
	assert.Nil(t, parent.Node())

	assert.Empty(t, reached(t, nodes.Ancestors(node), RelationContainment))
	assert.Empty(t, reached(t, nodes.Contains(node), RelationContainment))
	assert.Empty(t, reached(t, nodes.Descendants(node), RelationContainment))
	assert.Empty(t, reached(t, nodes.Zones(node), RelationMembership))
	assert.Empty(t, reached(t, nodes.Members(node), RelationMembership))

	// The node beside it in the same file writes both, so absence here is the
	// node's and not the loader's.
	beside := nodeOf(t, nodes, "site:E-01")

	_, hasWithin = beside.Within()
	assert.True(t, hasWithin)
	assert.NotEmpty(t, beside.MemberOf())
}

// TestSemanticNodeReferencesAsWritten reads the two relations back as ids,
// which is what a caller holding a node rather than the model sees.
func TestSemanticNodeReferencesAsWritten(t *testing.T) {
	nodes := relatedNodes(t)

	partition := nodeOf(t, nodes, "site:E-01")

	within, hasWithin := partition.Within()
	require.True(t, hasWithin)
	assert.Equal(t, ID("site:L-01"), within)

	assert.Equal(t, []ID{"site:Z-fire", "site:Z-therm", "site:Z-maint"}, partition.MemberOf())

	// The slice is the node's own read-only state, so re-ordering what comes
	// back re-orders nothing.
	slices.Reverse(partition.MemberOf())
	assert.Equal(t, []ID{"site:Z-fire", "site:Z-therm", "site:Z-maint"}, partition.MemberOf())
}

// TestTraversalsShowWhatTheFileSays is its own function because it is about a
// model which did not load clean rather than a variation on one which did.
//
// A reference which resolves is walked whatever else is wrong with it. A
// membership naming a storey and a containment the hierarchy forbids are both
// load errors, and both are still what somebody wrote: a traversal which
// quietly dropped either would disagree with the source the diagnostic is
// asking them to fix, and would do it silently.
//
// Both ends of one edge report it, which is the other half of the rule. An edge
// visible from one side and not the other is worse than an edge visible from
// neither, because the two answers then disagree about one model.
func TestTraversalsShowWhatTheFileSays(t *testing.T) {
	nodes, rendered := loadNodeFixture(t, "dangling-relation")
	require.NotEmpty(t, rendered, "the fixture is a model which does not load clean")

	storey := nodeOf(t, nodes, "site:L-01")
	door := nodeOf(t, nodes, "site:I-01")

	t.Run("walks a membership naming something which is not a zone", func(t *testing.T) {
		assert.Equal(t, []ID{"site:L-01"}, reached(t, nodes.Zones(door), RelationMembership))
		assert.Equal(t, []ID{"site:I-01"}, reached(t, nodes.Members(storey), RelationMembership))
	})

	t.Run("keeps the relation of an edge which is wrong", func(t *testing.T) {
		// The storey holds the partition by containment and the door by
		// membership. The two are the same node pair away from each other and
		// are never merged, however wrong the second one is.
		assert.Equal(t, []ID{"site:E-01"}, reached(t, nodes.Contains(storey), RelationContainment))
		assert.Equal(t, []ID{"site:I-01"}, reached(t, nodes.Members(storey), RelationMembership))
	})

	t.Run("does not walk a reference which names no node", func(t *testing.T) {
		room := nodeOf(t, nodes, "site:S-101")

		within, hasWithin := room.Within()
		assert.True(t, hasWithin, "the node wrote a parent")
		assert.Equal(t, ID("site:L-99"), within, "and it is read back as written")

		// There is nothing for the traversal to yield, so it yields nothing.
		_, ok := nodes.Within(room)
		assert.False(t, ok)
		assert.Empty(t, reached(t, nodes.Ancestors(room), RelationContainment))
	})
}

// TestNests reads the hierarchy the engine compiles in.
func TestNests(t *testing.T) {
	testCases := []struct {
		name     string
		child    Kind
		parent   Kind
		expected bool
	}{
		{
			name:     "permits a building inside a site",
			child:    KindBuilding,
			parent:   KindSite,
			expected: true,
		},
		{
			name:     "permits a storey inside a building",
			child:    KindStorey,
			parent:   KindBuilding,
			expected: true,
		},
		{
			name:     "permits a space inside a storey",
			child:    KindSpace,
			parent:   KindStorey,
			expected: true,
		},
		{
			name:     "permits a space inside a space",
			child:    KindSpace,
			parent:   KindSpace,
			expected: true,
		},
		{
			name:     "permits an element inside a space",
			child:    KindElement,
			parent:   KindSpace,
			expected: true,
		},
		{
			name:     "permits an interface inside a space",
			child:    KindInterface,
			parent:   KindSpace,
			expected: true,
		},
		{
			name:   "refuses the hierarchy upside down",
			child:  KindStorey,
			parent: KindSpace,
		},
		{
			name:   "refuses anything inside a zone",
			child:  KindElement,
			parent: KindZone,
		},
		{
			name:   "refuses a zone inside anything",
			child:  KindZone,
			parent: KindSite,
		},
		{
			name:   "refuses a site inside anything",
			child:  KindSite,
			parent: KindSite,
		},
		{
			name:   "refuses an interface inside an interface",
			child:  KindInterface,
			parent: KindInterface,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, Nests(testCase.child, testCase.parent))
		})
	}

	t.Run("says something about every kind the engine compiles in", func(t *testing.T) {
		for _, kind := range Kinds() {
			_, ok := nests[kind]
			assert.True(t, ok, "the hierarchy places %s", kind)
		}
	})
}
