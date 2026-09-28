// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// traverseRegistry is the vocabulary the model below is judged against. It
// declares a type for every kind the hierarchy names, plus the two a zone takes.
const traverseRegistry = `(project
  (label "Traversal fixture")
  (globalid-namespace "https://example.org/models/traverse"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace site (description "Semantic nodes minted by this model."))

(frame frame:building (label "Building local grid") (unit m))

(type SiteBoundary
  (kind Site)
  (geometry area)
  (description "The extent of the land a project sits on."))

(type OfficeBuilding
  (kind Building)
  (geometry solid)
  (description "A building let as offices."))

(type Level
  (kind Storey)
  (geometry surface)
  (description "One floor of a building."))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings."))

(type Corridor
  (kind Space)
  (geometry area)
  (description "A circulation space between rooms."))

(type Partition
  (kind Element)
  (geometry line)
  (description "A non-loadbearing wall between two spaces."))

(type Compartment
  (kind Zone)
  (geometry area)
  (description "A group of things treated together, which may overlap another."))

(type CircuitGroup
  (kind Zone)
  (geometry absent)
  (description "A set of circuits fed from one board, which has no shape."))
`

// traverseModel is one hierarchy four levels deep, three zones which overlap
// it, two rooms which share a wall, and a loop and an edge nothing names.
//
// Every case a traversal has to keep apart is written here once: a wall which is
// inside one thing and a member of three, a room reachable from the site only
// through the two levels between them, a zone which is a member of another zone,
// one edge which two rooms both reach, and a shape which bounds nothing.
const traverseModel = `(node site:S-01
  (label "Riverside parcel")
  (kind Site)
  (type SiteBoundary)
  (geometry area)
  (frame frame:building))

(node site:B-01
  (label "Riverside House")
  (kind Building)
  (type OfficeBuilding)
  (geometry solid)
  (frame frame:building)
  (within site:S-01))

(node site:L-01
  (label "Level 1")
  (kind Storey)
  (type Level)
  (geometry surface)
  (frame frame:building)
  (within site:B-01))

(node site:S-101
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-01))

(node site:S-102
  (label "East Corridor")
  (kind Space)
  (type Corridor)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-02))

(node site:S-101a
  (label "Alcove off Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:S-101))

(node site:W-01
  (label "Stud partition, room B to corridor")
  (kind Element)
  (type Partition)
  (geometry line)
  (frame frame:building)
  (within site:L-01)
  (member-of site:Z-fire)
  (member-of site:Z-therm)
  (member-of site:Z-maint))

(node site:Z-fire
  (label "Fire compartment 1")
  (kind Zone)
  (type Compartment)
  (geometry area)
  (frame frame:building))

(node site:Z-therm
  (label "Thermal zone north")
  (kind Zone)
  (type Compartment)
  (geometry area)
  (frame frame:building))

(node site:Z-maint
  (label "Maintenance round A")
  (kind Zone)
  (type CircuitGroup)
  (member-of site:Z-therm))

(vertex geom:V-01 (label "Room B, north-west corner") (frame frame:building))

(vertex geom:V-02 (label "The shared wall, north end") (frame frame:building))

(vertex geom:V-03 (label "The shared wall, south end") (frame frame:building))

(vertex geom:V-04 (label "Room B, south-west corner") (frame frame:building))

(vertex geom:V-05 (label "Corridor, north-east corner") (frame frame:building))

(vertex geom:V-06 (label "Corridor, south-east corner") (frame frame:building))

(edge geom:E-01 (label "Room B, north opening") (frame frame:building) (vertices geom:V-01 geom:V-02))

(edge geom:E-02
  (label "Partition, room B to corridor")
  (frame frame:building)
  (vertices geom:V-02 geom:V-03)
  (backed-by site:W-01))

(edge geom:E-03 (label "Room B, south wall") (frame frame:building) (vertices geom:V-03 geom:V-04))

(edge geom:E-04 (label "Room B, west wall") (frame frame:building) (vertices geom:V-04 geom:V-01))

(edge geom:E-05 (label "Corridor, north wall") (frame frame:building) (vertices geom:V-02 geom:V-05))

(edge geom:E-06 (label "Corridor, east wall") (frame frame:building) (vertices geom:V-05 geom:V-06))

(edge geom:E-07 (label "Corridor, south wall") (frame frame:building) (vertices geom:V-06 geom:V-03))

(loop geom:L-01
  (label "Meeting Room B boundary")
  (frame frame:building)
  (edges geom:E-01 geom:E-02 geom:E-03 geom:E-04))

(loop geom:L-02
  (label "East Corridor boundary")
  (frame frame:building)
  (edges geom:E-05 geom:E-06 geom:E-07 geom:E-02))

(edge geom:E-08 (label "Setting-out line, bounding nothing") (frame frame:building) (vertices geom:V-01 geom:V-05))

(loop geom:L-03
  (label "Meeting Room B, a second ring nothing names")
  (frame frame:building)
  (edges geom:E-01 geom:E-02 geom:E-03 geom:E-04))
`

// traversable is the fixture tree traverse is run against.
func traversableModel() map[string]string {
	return map[string]string{"registry.dfc": traverseRegistry, "entities/site.dfc": traverseModel}
}

// walk runs one traversal against the fixture and decodes what it wrote.
func walk(t *testing.T, args ...string) traverseResult {
	t.Helper()

	return walkIn(t, tree(t, traversableModel()), args...)
}

// walkIn runs one traversal against the model rooted at dir and decodes what it
// wrote.
func walkIn(t *testing.T, dir string, args ...string) traverseResult {
	t.Helper()

	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run(append([]string{"traverse"}, args...), &stdout, &stderr), stderr.String())

	result := listed[traverseResult](t, stdout.String())
	assert.Equal(t, outputVersion, result.Version)
	assert.Equal(t, "traverse", result.Command)

	return result
}

// reached is each result as "id relation depth", which is every axis a
// traversal promises about a result in one readable line.
func reachedBy(result traverseResult) []string {
	out := make([]string, 0, len(result.Results))
	for _, entry := range result.Results {
		out = append(out, entry.ID+" "+entry.Relation+" "+strings.Repeat("+", entry.Depth))
	}
	return out
}

func TestRunTraverse(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "gives what a node holds one level in",
			args:     []string{queryContains, "site:L-01"},
			expected: []string{"site:S-101 containment +", "site:S-102 containment +", "site:W-01 containment +"},
		},
		{
			name: "walks containment as far as the depth it was given",
			args: []string{queryContains, "--depth", "2", "site:S-01"},
			expected: []string{
				"site:B-01 containment +",
				"site:L-01 containment ++",
			},
		},
		{
			name: "walks containment as far as the model goes when it is asked to",
			args: []string{queryContains, "--depth", depthAll, "site:S-01"},
			expected: []string{
				"site:B-01 containment +",
				"site:L-01 containment ++",
				"site:S-101 containment +++",
				"site:S-102 containment +++",
				"site:W-01 containment +++",
				"site:S-101a containment ++++",
			},
		},
		{
			name:     "gives the node a thing sits in",
			args:     []string{queryContainedBy, "site:S-101a"},
			expected: []string{"site:S-101 containment +"},
		},
		{
			name: "walks containment out to the root",
			args: []string{queryContainedBy, "--depth", depthAll, "site:S-101a"},
			expected: []string{
				"site:S-101 containment +",
				"site:L-01 containment ++",
				"site:B-01 containment +++",
				"site:S-01 containment ++++",
			},
		},
		{
			name: "gives every zone a node is a member of",
			args: []string{queryMembersOf, "site:W-01"},
			expected: []string{
				"site:Z-fire membership +",
				"site:Z-maint membership +",
				"site:Z-therm membership +",
			},
		},
		{
			// The thermal zone is named by the wall and again by the maintenance
			// round, which is a member of it. That is one zone, reached at the
			// fewer steps, and reporting it twice would be counting the ways in.
			name: "gives a zone reachable two ways once",
			args: []string{queryMembersOf, "--depth", depthAll, "site:W-01"},
			expected: []string{
				"site:Z-fire membership +",
				"site:Z-maint membership +",
				"site:Z-therm membership +",
			},
		},
		{
			name:     "gives the zone a zone is itself a member of",
			args:     []string{queryMembersOf, "site:Z-maint"},
			expected: []string{"site:Z-therm membership +"},
		},
		{
			name:     "gives every node which named a zone",
			args:     []string{queryMembers, "site:Z-fire"},
			expected: []string{"site:W-01 membership +"},
		},
		{
			// The maintenance round is a zone written into the thermal zone, so it
			// is one of the thermal zone's members, whatever else it groups.
			name:     "gives a zone grouped into a zone as one of its members",
			args:     []string{queryMembers, "site:Z-therm"},
			expected: []string{"site:W-01 membership +", "site:Z-maint membership +"},
		},
		{
			// The wall is a member of the thermal zone and again of the
			// maintenance round inside it. That is one member, at the fewer steps.
			name:     "gives a member reachable two ways once",
			args:     []string{queryMembers, "--depth", depthAll, "site:Z-therm"},
			expected: []string{"site:W-01 membership +", "site:Z-maint membership +"},
		},
		{
			name:     "gives nothing for a node nothing names in a member-of",
			args:     []string{queryMembers, "--depth", depthAll, "site:W-01"},
			expected: []string{},
		},
		{
			name:     "narrows the members to one kind without narrowing the walk",
			args:     []string{queryMembers, "--depth", depthAll, "--kind", "Zone", "site:Z-therm"},
			expected: []string{"site:Z-maint membership +"},
		},
		{
			name:     "narrows the members to one type without narrowing the walk",
			args:     []string{queryMembers, "--depth", depthAll, "--type", "Partition", "site:Z-therm"},
			expected: []string{"site:W-01 membership +"},
		},
		{
			// In the order the loop traverses them rather than in id order: that
			// order is the ring itself.
			name: "gives the edges a boundary is assembled from, in the order the loop traverses them",
			args: []string{queryBoundaryOf, "site:S-101"},
			expected: []string{
				"geom:E-01 boundary +",
				"geom:E-02 boundary +",
				"geom:E-03 boundary +",
				"geom:E-04 boundary +",
			},
		},
		{
			name:     "gives the node which names a loop as its boundary",
			args:     []string{queryBounds, "geom:L-01"},
			expected: []string{"site:S-101 boundary +"},
		},
		{
			name:     "gives both rooms either side of a shared edge, in id order",
			args:     []string{queryBounds, "geom:E-02"},
			expected: []string{"site:S-101 boundary +", "site:S-102 boundary +"},
		},
		{
			name:     "gives the one room an unshared edge bounds",
			args:     []string{queryBounds, "geom:E-01"},
			expected: []string{"site:S-101 boundary +"},
		},
		{
			name:     "gives nothing for a loop no node names",
			args:     []string{queryBounds, "geom:L-03"},
			expected: []string{},
		},
		{
			name:     "gives nothing for an edge no named loop reaches",
			args:     []string{queryBounds, "geom:E-08"},
			expected: []string{},
		},
		{
			name:     "narrows what an edge bounds to one kind",
			args:     []string{queryBounds, "--kind", "Space", "geom:E-02"},
			expected: []string{"site:S-101 boundary +", "site:S-102 boundary +"},
		},
		{
			name:     "narrows what an edge bounds to one type",
			args:     []string{queryBounds, "--type", "Corridor", "geom:E-02"},
			expected: []string{"site:S-102 boundary +"},
		},
		{
			name:     "gives the room on the other side of a shared wall",
			args:     []string{queryAdjacentTo, "site:S-101"},
			expected: []string{"site:S-102 adjacency +"},
		},
		{
			name:     "gives nothing for a node nothing shares an edge with",
			args:     []string{queryAdjacentTo, "site:S-101a"},
			expected: []string{},
		},
		{
			name:     "narrows the results to one kind without narrowing the walk",
			args:     []string{queryContains, "--depth", depthAll, "--kind", "Space", "site:S-01"},
			expected: []string{"site:S-101 containment +++", "site:S-102 containment +++", "site:S-101a containment ++++"},
		},
		{
			name:     "narrows the results to one type without narrowing the walk",
			args:     []string{queryContains, "--depth", depthAll, "--type", "MeetingRoom", "site:S-01"},
			expected: []string{"site:S-101 containment +++", "site:S-101a containment ++++"},
		},
		{
			name:     "narrows on both filters at once",
			args:     []string{queryContains, "--depth", depthAll, "--kind", "Zone", "--type", "MeetingRoom", "site:S-01"},
			expected: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := walk(t, testCase.args...)

			assert.Equal(t, testCase.expected, reachedBy(result))
		})
	}
}

// TestTraverseReportsWhatItWasAsked is its own function because it asserts on
// the head of the answer rather than on the walk: a stored result has to say
// what question produced it, and whether the walk stopped where the model ran
// out or where the bound did.
func TestTraverseReportsWhatItWasAsked(t *testing.T) {
	bounded := walk(t, queryContains, "--depth", "2", "site:S-01")

	assert.Equal(t, "site:S-01", bounded.Subject)
	assert.Equal(t, queryContains, bounded.Query)
	assert.Equal(t, 2, bounded.Depth)

	unbounded := walk(t, queryContains, "--depth", depthAll, "site:S-01")

	assert.Equal(t, dfcad.Unbounded, unbounded.Depth)

	// The default is a bound rather than the whole model, which is what makes a
	// traversal of a model nobody has read an answer of a known size.
	assert.Equal(t, 1, walk(t, queryContains, "site:S-01").Depth)
}

// TestTraverseOfASharedWall is its own function because the assertion is about
// one edge read from both sides: two rooms either side of a partition reference
// one edge with one identity, and every query which reaches it has to say the
// same thing about it.
func TestTraverseOfASharedWall(t *testing.T) {
	room := walk(t, queryBoundaryOf, "site:S-101")
	corridor := walk(t, queryBoundaryOf, "site:S-102")

	shared := func(result traverseResult) traversed {
		t.Helper()

		for _, entry := range result.Results {
			if entry.ID == "geom:E-02" {
				return entry
			}
		}

		t.Fatalf("both rooms reach the shared edge")
		return traversed{}
	}

	t.Run("says which edges are walls and which are openings", func(t *testing.T) {
		// Nothing in the model says physical or virtual. What backs each edge is
		// written, and the classification is read from that, so adding a wall
		// changes this answer with no edit which says so.
		assert.Equal(t, []string{"virtual", "physical", "virtual", "virtual"}, classifications(room))
		assert.Equal(t, []string{"virtual", "virtual", "virtual", "physical"}, classifications(corridor))
	})

	t.Run("names the element which realises the shared edge, from both sides", func(t *testing.T) {
		assert.Equal(t, string(dfcad.ClassificationPhysical), shared(room).Classification)
		assert.Equal(t, []string{"site:W-01"}, shared(room).Backing)

		assert.Equal(t, shared(room).Classification, shared(corridor).Classification)
		assert.Equal(t, shared(room).Backing, shared(corridor).Backing)
	})

	t.Run("names the type of that element, from both sides", func(t *testing.T) {
		assert.Equal(t, []string{"Partition"}, shared(room).BackingTypes)
		assert.Equal(t, []string{"Partition"}, shared(corridor).BackingTypes)
	})

	t.Run("makes the two rooms adjacent across that edge and no other", func(t *testing.T) {
		neighbours := walk(t, queryAdjacentTo, "site:S-101")

		require.Len(t, neighbours.Results, 1)
		assert.Equal(t, "site:S-102", neighbours.Results[0].ID)
		assert.Equal(t, string(dfcad.RelationAdjacency), neighbours.Results[0].Relation)
		assert.Equal(t, []string{"geom:E-02"}, neighbours.Results[0].Via)

		// And the wall itself is adjacent to nothing. It is what backs an edge
		// rather than something with a boundary of its own.
		assert.Empty(t, walk(t, queryAdjacentTo, "--depth", depthAll, "site:W-01").Results)
	})
}

// classifications is what each result of a boundary walk classified as.
func classifications(result traverseResult) []string {
	out := make([]string, 0, len(result.Results))
	for _, entry := range result.Results {
		out = append(out, entry.Classification)
	}
	return out
}

// TestTraverseNeverConflatesTheTwoRelations is its own function because it is
// the reason every result carries the relation which produced it: the wall is
// inside one thing and a member of three, and a walk which blurred the two would
// answer either question with the other one's results.
func TestTraverseNeverConflatesTheTwoRelations(t *testing.T) {
	zones := walk(t, queryMembersOf, "--depth", depthAll, "site:W-01")
	parents := walk(t, queryContainedBy, "--depth", depthAll, "site:W-01")

	for _, entry := range zones.Results {
		assert.Equal(t, string(dfcad.RelationMembership), entry.Relation)
		assert.Equal(t, "Zone", entry.Kind)
	}
	for _, entry := range parents.Results {
		assert.Equal(t, string(dfcad.RelationContainment), entry.Relation)
		assert.NotEqual(t, "Zone", entry.Kind)
	}

	assert.Equal(t, []string{"site:Z-fire", "site:Z-maint", "site:Z-therm"}, reachedIDs(zones))
	assert.Equal(t, []string{"site:L-01", "site:B-01", "site:S-01"}, reachedIDs(parents))

	// And the zones hold nothing, however many members they have: a member is
	// not a thing inside.
	assert.Empty(t, walk(t, queryContains, "--depth", depthAll, "site:Z-fire").Results)

	// The other direction of membership keeps to it as well. The storey holds
	// the wall, and the zones group it: the storey groups nothing, and what the
	// zones group is not what they hold.
	members := walk(t, queryMembers, "--depth", depthAll, "site:Z-therm")
	for _, entry := range members.Results {
		assert.Equal(t, string(dfcad.RelationMembership), entry.Relation)
	}
	assert.Equal(t, []string{"site:W-01", "site:Z-maint"}, reachedIDs(members))
	assert.Empty(t, walk(t, queryMembers, "--depth", depthAll, "site:L-01").Results)
}

// nestedZones is a model whose membership nests two deep, with a room inside a
// member which is a member of nothing itself.
//
// It is its own model rather than more of the one above because that one's
// nested zone groups nothing its parent does not group directly, so no member of
// it is ever further than one step away.
const nestedZones = `(node site:Z-A
  (label "Fire compartment")
  (kind Zone)
  (type Compartment)
  (geometry area)
  (frame frame:building))

(node site:Z-B
  (label "Smoke zone inside the compartment")
  (kind Zone)
  (type Compartment)
  (geometry area)
  (frame frame:building)
  (member-of site:Z-A))

(node site:S-201
  (label "Meeting Room C")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (member-of site:Z-B))

(node site:S-201a
  (label "Alcove off Meeting Room C")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:S-201))
`

// TestTraverseMembersFollowsNestedMembership is its own function because it is
// asked of its own model: the members of a zone, then the members of those
// which are zones, and never what sits inside a member.
func TestTraverseMembersFollowsNestedMembership(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "gives only the zone's own members at one step",
			args:     []string{queryMembers, "site:Z-A"},
			expected: []string{"site:Z-B membership +"},
		},
		{
			name:     "gives the members of a member which is a zone at the next step",
			args:     []string{queryMembers, "--depth", "2", "site:Z-A"},
			expected: []string{"site:Z-B membership +", "site:S-201 membership ++"},
		},
		{
			// The alcove is inside a member and wrote no member-of of its own, so
			// no depth reaches it.
			name:     "never reaches what sits inside a member",
			args:     []string{queryMembers, "--depth", depthAll, "site:Z-A"},
			expected: []string{"site:Z-B membership +", "site:S-201 membership ++"},
		},
		{
			name:     "narrows what is reported without narrowing what is walked",
			args:     []string{queryMembers, "--depth", depthAll, "--kind", "Space", "site:Z-A"},
			expected: []string{"site:S-201 membership ++"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := tree(t, map[string]string{"registry.dfc": traverseRegistry, "entities/site.dfc": nestedZones})

			result := walkIn(t, dir, testCase.args...)

			assert.Equal(t, testCase.expected, reachedBy(result))
		})
	}
}

// TestTraverseMembersOverTheBudgetModel is its own function because it is asked
// of the representative model rather than of the traversal fixture: every space
// of level 1 names its zone, whatever its type, and every one of them is a
// member — the service riser and the corridor as much as the offices.
func TestTraverseMembersOverTheBudgetModel(t *testing.T) {
	root, err := filepath.Abs(budgetRoot)
	require.NoError(t, err)

	result := walkIn(t, root, queryMembers, "site:Z-01")

	expected := make([]string, 0, 13)
	for n := 101; n <= 113; n++ {
		expected = append(expected, "site:S-"+strconv.Itoa(n)+" membership +")
	}

	assert.Equal(t, expected, reachedBy(result))

	types := make(map[string]string, len(result.Results))
	for _, entry := range result.Results {
		types[entry.ID] = entry.Type
	}
	assert.Equal(t, "ServiceRiser", types["site:S-101"])
	assert.Equal(t, "Corridor", types["site:S-113"])
}

// TestTraverseMembershipReadsTheSameBothWays holds the two directions of
// membership to each other over every node and every zone of both models: a zone
// is among what members-of gives for a node at one step exactly when that node
// is among what members gives for the zone. A table of expected literals would
// check the pairs somebody thought of; this checks all of them.
func TestTraverseMembershipReadsTheSameBothWays(t *testing.T) {
	budget, err := filepath.Abs(budgetRoot)
	require.NoError(t, err)

	models := []struct {
		name string
		root func(t *testing.T) string
	}{
		{name: "the traversal fixture", root: func(t *testing.T) string { return tree(t, traversableModel()) }},
		{name: "the budget model", root: func(*testing.T) string { return budget }},
	}

	for _, model := range models {
		t.Run("over "+model.name+" a zone of a node has that node as a member", func(t *testing.T) {
			root := model.root(t)

			graph, _ := dfcad.LoadGraph(root)
			require.NotNil(t, graph)

			var nodes, zones []string
			for node := range graph.Nodes().All() {
				nodes = append(nodes, string(node.ID()))
				if node.Kind() == dfcad.KindZone {
					zones = append(zones, string(node.ID()))
				}
			}
			require.NotEmpty(t, zones)

			membersOf := make(map[string][]string, len(nodes))
			for _, node := range nodes {
				membersOf[node] = reachedIDs(walkIn(t, root, queryMembersOf, node))
			}

			for _, zone := range zones {
				members := reachedIDs(walkIn(t, root, queryMembers, zone))

				for _, node := range nodes {
					assert.Equal(t,
						slices.Contains(membersOf[node], zone),
						slices.Contains(members, node),
						"%s among the zones of %s, and %s among the members of %s", zone, node, node, zone,
					)
				}
			}
		})
	}
}

// reachedIDs is the id of each result, in the order the answer reports them.
func reachedIDs(result traverseResult) []string {
	out := make([]string, 0, len(result.Results))
	for _, entry := range result.Results {
		out = append(out, entry.ID)
	}
	return out
}

// TestTraverseIsDeterministic is its own function because the property is about
// two runs rather than one: the same model and the same question produce the
// same bytes, which is what makes a diff between two results mean something.
func TestTraverseIsDeterministic(t *testing.T) {
	once := func(t *testing.T) string {
		t.Helper()

		t.Chdir(tree(t, traversableModel()))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run(
			[]string{"traverse", queryContains, "--depth", depthAll, "site:S-01"}, &stdout, &stderr,
		), stderr.String())

		return stdout.String()
	}

	assert.Equal(t, once(t), once(t))
}

// TestTraverseUsageErrors walks the invocations which name something that is not
// there, or ask a question the relation has no answer to.
func TestTraverseUsageErrors(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "reports a traverse with no query at all",
			args:     []string{"traverse"},
			expected: "found no argument",
		},
		{
			name:     "reports a traverse with a query and nothing to ask it of",
			args:     []string{"traverse", queryContains},
			expected: "found only the query",
		},
		{
			name:     "reports an argument too many",
			args:     []string{"traverse", queryContains, "site:S-01", "site:B-01"},
			expected: "site:B-01",
		},
		{
			name:     "reports a query which names none of the relations",
			args:     []string{"traverse", "borders", "site:S-01"},
			expected: "borders",
		},
		{
			name:     "reports an id nothing in the model holds, and the nearest there is",
			args:     []string{"traverse", queryContains, "site:S-O1"},
			expected: "did you mean site:S-01?",
		},
		{
			name:     "reports an argument which is not an id at all",
			args:     []string{"traverse", queryContains, "not an id"},
			expected: "not an id",
		},
		{
			name:     "reports an id which names a shape rather than a thing",
			args:     []string{"traverse", queryContains, "geom:L-01"},
			expected: "geom:L-01",
		},
		{
			name:     "reports a node asked what it bounds",
			args:     []string{"traverse", queryBounds, "site:S-101"},
			expected: "bounds takes a loop or an edge",
		},
		{
			name:     "reports a vertex asked what it bounds",
			args:     []string{"traverse", queryBounds, "geom:V-01"},
			expected: "geom:V-01",
		},
		{
			name:     "refuses a depth beside the query which is one step from a shape by definition",
			args:     []string{"traverse", queryBounds, "--depth", "2", "geom:L-01"},
			expected: "--depth says nothing under bounds",
		},
		{
			name:     "refuses every depth beside it, all included",
			args:     []string{"traverse", queryBounds, "--depth", depthAll, "geom:E-02"},
			expected: "--depth says nothing under bounds",
		},
		{
			name:     "reports a kind which is none of the kinds",
			args:     []string{"traverse", queryContains, "--kind", "Storeys", "site:S-01"},
			expected: "Storeys",
		},
		{
			name:     "reports a type the registry does not declare",
			args:     []string{"traverse", queryContains, "--type", "BoardRoom", "site:S-01"},
			expected: "BoardRoom",
		},
		{
			name:     "reports a depth which is not a count of steps",
			args:     []string{"traverse", queryContains, "--depth", "deep", "site:S-01"},
			expected: "deep",
		},
		{
			name:     "reports a depth of no steps at all",
			args:     []string{"traverse", queryContains, "--depth", "0", "site:S-01"},
			expected: "0",
		},
		{
			name:     "refuses a depth beside the query which is one step by definition",
			args:     []string{"traverse", queryBoundaryOf, "--depth", "2", "site:S-101"},
			expected: "--depth says nothing under boundary-of",
		},
		{
			name:     "refuses a kind beside the query whose results declare none",
			args:     []string{"traverse", queryBoundaryOf, "--kind", "Space", "site:S-101"},
			expected: "--kind says nothing under boundary-of",
		},
		{
			name:     "refuses a type beside the query whose results declare none",
			args:     []string{"traverse", queryBoundaryOf, "--type", "MeetingRoom", "site:S-101"},
			expected: "--type says nothing under boundary-of",
		},
		{
			name:     "reports a second kind which is none of the kinds",
			args:     []string{"traverse", queryContains, "--kind", "Space", "--kind", "Storeys", "site:S-01"},
			expected: "Storeys",
		},
		{
			name:     "reports a second type the registry does not declare",
			args:     []string{"traverse", queryContains, "--type", "MeetingRoom", "--type", "BoardRoom", "site:S-01"},
			expected: "BoardRoom",
		},
		{
			name: "refuses a kind written twice beside the query whose results declare none",
			args: []string{
				"traverse", queryBoundaryOf, "--kind", "Space", "--kind", "Element", "site:S-101",
			},
			expected: "--kind says nothing under boundary-of",
		},
		{
			name: "refuses a type written twice beside the query whose results declare none",
			args: []string{
				"traverse", queryBoundaryOf, "--type", "MeetingRoom", "--type", "MeetingRoom", "site:S-101",
			},
			expected: "--type says nothing under boundary-of",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, traversableModel()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitUsage, run(testCase.args, &stdout, &stderr))

			assert.Empty(t, stdout.String(), "a run which produced no result writes no result object")
			assert.Contains(t, stderr.String(), testCase.expected)
		})
	}
}

// TestTraverseBoundaryOfKeepsItsDefaults is its own function because it is the
// other half of the refusals above: a flag which was never written is not a
// flag which says nothing, so the query runs on its defaults.
func TestTraverseBoundaryOfKeepsItsDefaults(t *testing.T) {
	result := walk(t, queryBoundaryOf, "site:S-101")

	assert.Len(t, result.Results, 4)
	assert.Equal(t, 1, result.Depth)
}

// TestUnknownQueryNamesWhatItWanted checks that the error carries the queries
// there are, so a caller does not have to read the message to find them.
func TestUnknownQueryNamesWhatItWanted(t *testing.T) {
	_, ok := queryNamed("borders")
	require.False(t, ok)

	err := UnknownQueryError{Query: "borders", Known: queryNames()}

	assert.Equal(t, queryNames(), err.Known)
	assert.Equal(t, []string{
		queryContains, queryContainedBy, queryMembersOf, queryMembers, queryBoundaryOf, queryBounds, queryAdjacentTo,
	}, err.Known)

	for _, name := range queryNames() {
		asked, found := queryNamed(name)
		require.True(t, found)
		assert.Equal(t, name, asked.name)
		assert.NotNil(t, asked.walk)
	}
}

// TestEveryResultSaysWhichRelationReachedIt walks every query rather than naming
// them, because the property is one of all of them: a result which cannot say
// whether it means enclosure, grouping, an outline or a shared wall is a result
// which will eventually be read as the wrong one of the four.
func TestEveryResultSaysWhichRelationReachedIt(t *testing.T) {
	known := []string{
		string(dfcad.RelationContainment),
		string(dfcad.RelationMembership),
		string(dfcad.RelationBoundary),
		string(dfcad.RelationAdjacency),
	}

	// Five subjects, because no one thing is in every relation: the room has an
	// outline and a neighbour, the wall which separates it is what the zones are
	// written on, the zone is what the wall is a member of, and the loop and the
	// shared edge are what the room's outline is assembled from.
	subjects := []struct {
		id     string
		family string
	}{
		{id: "site:S-101", family: familyNode},
		{id: "site:W-01", family: familyNode},
		{id: "site:Z-therm", family: familyNode},
		{id: "geom:L-01", family: familyLoop},
		{id: "geom:E-02", family: familyEdge},
	}

	for _, asked := range queries {
		ran := 0

		for _, walked := range subjects {
			if !slices.Contains(asked.takes, walked.family) {
				continue
			}
			ran++

			subject := walked.id
			t.Run(asked.name+" of "+subject+" says which relation reached each result", func(t *testing.T) {
				args := []string{asked.name, subject}
				if asked.deep {
					args = []string{asked.name, "--depth", depthAll, subject}
				}

				result := walk(t, args...)

				assert.Equal(t, asked.name, result.Query)
				for _, entry := range result.Results {
					assert.Contains(t, known, entry.Relation)
					assert.Positive(t, entry.Depth, "a result is at least one step from what was asked about")
					assert.NotEmpty(t, entry.Family)
					assert.NotEqual(t, subject, entry.ID, "nothing is its own relative")
				}
			})
		}

		assert.Positive(t, ran, "%s is asked of at least one subject it takes", asked.name)
	}
}

// TestFlagNotApplicableCarriesWhichAndWhy checks the refusals in a form a
// caller can branch on rather than only in a message.
func TestFlagNotApplicableCarriesWhichAndWhy(t *testing.T) {
	boundary, ok := queryNamed(queryBoundaryOf)
	require.True(t, ok)

	testCases := []struct {
		name         string
		given        map[string]bool
		expectedFlag string
	}{
		{
			name:         "refuses a depth",
			given:        map[string]bool{flagDepth: true},
			expectedFlag: flagDepth,
		},
		{
			name:         "refuses a kind",
			given:        map[string]bool{flagKind: true},
			expectedFlag: flagKind,
		},
		{
			name:         "refuses a type",
			given:        map[string]bool{flagType: true},
			expectedFlag: flagType,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := checkFlags(boundary, testCase.given)

			var refused FlagNotApplicableError
			require.ErrorAs(t, err, &refused)
			assert.Equal(t, testCase.expectedFlag, refused.Flag)
			assert.Equal(t, queryBoundaryOf, refused.Query)
			assert.NotEmpty(t, refused.Reason)
		})
	}

	t.Run("refuses a depth beside bounds and honours both filters", func(t *testing.T) {
		bounds, ok := queryNamed(queryBounds)
		require.True(t, ok)

		var refused FlagNotApplicableError
		require.ErrorAs(t, checkFlags(bounds, map[string]bool{flagDepth: true}), &refused)
		assert.Equal(t, flagDepth, refused.Flag)
		assert.Equal(t, queryBounds, refused.Query)
		assert.Equal(t, depthNotApplicable, refused.Reason, "for the reason boundary-of gives")

		assert.NoError(t, checkFlags(bounds, map[string]bool{flagKind: true, flagType: true}))
	})

	// Every query refuses exactly what it declares it cannot honour, and none of
	// them is refused when it was not written.
	for _, asked := range queries {
		assert.NoError(t, checkFlags(asked, nil))

		if asked.deep {
			assert.NoError(t, checkFlags(asked, map[string]bool{flagDepth: true}), asked.name)
		} else {
			assert.Error(t, checkFlags(asked, map[string]bool{flagDepth: true}), asked.name)
		}

		if asked.grouped {
			assert.NoError(t, checkFlags(asked, map[string]bool{flagKind: true, flagType: true}), asked.name)
		} else {
			assert.Error(t, checkFlags(asked, map[string]bool{flagKind: true}), asked.name)
			assert.Error(t, checkFlags(asked, map[string]bool{flagType: true}), asked.name)
		}
	}
}

// TestTraversalDepth is its own function because it is about the flag rather
// than about a walk: the spellings it takes, and the ones it refuses.
func TestTraversalDepth(t *testing.T) {
	testCases := []struct {
		name     string
		value    string
		expected traversalDepth
	}{
		{
			name:     "takes a count of steps",
			value:    "3",
			expected: 3,
		},
		{
			name:     "takes one step",
			value:    "1",
			expected: 1,
		},
		{
			name:     "takes the word which means as far as the model goes",
			value:    depthAll,
			expected: dfcad.Unbounded,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			depth := traversalDepth(1)

			require.NoError(t, depth.Set(testCase.value))
			assert.Equal(t, testCase.expected, depth)
			assert.Equal(t, testCase.value, depth.String())
		})
	}
}

func TestTraversalDepthRejectsWhatIsNotADepth(t *testing.T) {
	testCases := []string{"0", "-1", "1.5", "deep", "", "every"}

	for _, value := range testCases {
		t.Run("rejects "+value, func(t *testing.T) {
			depth := traversalDepth(1)

			err := depth.Set(value)

			var invalid InvalidDepthError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, value, invalid.Value)
			assert.Equal(t, traversalDepth(1), depth, "a refused depth leaves the default where it was")
		})
	}
}

// TestNotTraversableNamesTheFamily checks that an id of a family the query does
// not walk from is reported as what it is, and as what the query takes, rather
// than as an id nothing holds, which is a different mistake with a different fix.
func TestNotTraversableNamesTheFamily(t *testing.T) {
	t.Chdir(tree(t, traversableModel()))

	graph, _ := dfcad.LoadGraph(".")

	// One id of each family, so that every query is asked of every family and
	// the rule each query declares is checked by walking the list.
	subjects := []struct {
		id     dfcad.ID
		family string
	}{
		{id: "site:S-101", family: familyNode},
		{id: "geom:V-01", family: familyVertex},
		{id: "geom:E-01", family: familyEdge},
		{id: "geom:L-01", family: familyLoop},
	}

	for _, asked := range queries {
		for _, subject := range subjects {
			takes := slices.Contains(asked.takes, subject.family)

			name := asked.name + " refuses " + article(subject.family) + " " + subject.family
			if takes {
				name = asked.name + " walks from " + article(subject.family) + " " + subject.family
			}

			t.Run(name, func(t *testing.T) {
				entity, err := walkable(graph, subject.id, asked)

				if takes {
					require.NoError(t, err)
					assert.Equal(t, subject.id, entity.ID())
					return
				}

				var refused NotTraversableError
				require.ErrorAs(t, err, &refused)
				assert.Equal(t, string(subject.id), refused.ID)
				assert.Equal(t, subject.family, refused.Family)
				assert.Equal(t, asked.name, refused.Query)
				assert.Equal(t, asked.takes, refused.Takes)
			})
		}
	}

	t.Run("bounds takes a loop and an edge and nothing else", func(t *testing.T) {
		bounds, ok := queryNamed(queryBounds)
		require.True(t, ok)

		assert.ElementsMatch(t, []string{familyLoop, familyEdge}, bounds.takes)
	})

	t.Run("every other query takes a semantic node and nothing else", func(t *testing.T) {
		for _, asked := range queries {
			if asked.name == queryBounds {
				continue
			}
			assert.Equal(t, []string{familyNode}, asked.takes, asked.name)
		}
	})

	t.Run("reports an id nothing holds as an unknown id rather than as a family", func(t *testing.T) {
		_, err := traversable(graph, "site:S-999")

		var unknown UnknownIDError
		require.ErrorAs(t, err, &unknown)
		assert.False(t, errors.As(err, &NotTraversableError{}))
	})

	t.Run("reports a shape given to a command which walks from a node, naming no query", func(t *testing.T) {
		_, err := traversable(graph, "geom:L-01")

		var refused NotTraversableError
		require.ErrorAs(t, err, &refused)
		assert.Equal(t, "geom:L-01", refused.ID)
		assert.Equal(t, familyLoop, refused.Family)
		assert.Empty(t, refused.Query)
		assert.Equal(t, []string{familyNode}, refused.Takes)
	})

	t.Run("gives the node itself for an id a semantic node holds", func(t *testing.T) {
		node, err := traversable(graph, "site:S-101")

		require.NoError(t, err)
		require.NotNil(t, node)
		assert.Equal(t, dfcad.ID("site:S-101"), node.ID())
	})
}

// TestTraverseBoundsReadsTheSameAsBoundaryOf holds bounds to the two
// directions it reverses, over every node, edge and loop of the budget model: an
// edge is in boundary-of a node exactly when that node is in bounds of the edge,
// and a loop is among the boundaries get gives for a node exactly when that node
// is in bounds of the loop. A table of expected literals would check the pairs
// somebody thought of; this checks all of them.
func TestTraverseBoundsReadsTheSameAsBoundaryOf(t *testing.T) {
	root, err := filepath.Abs(budgetRoot)
	require.NoError(t, err)

	graph, _ := dfcad.LoadGraph(root)
	require.NotNil(t, graph)

	boundaryOf := make(map[string][]string)
	boundaries := make(map[string][]string)
	for node := range graph.Nodes().All() {
		id := string(node.ID())
		boundaryOf[id] = reachedIDs(walkIn(t, root, queryBoundaryOf, id))
		boundaries[id] = fetched(t, root, id).Boundaries
	}

	bounds := make(map[string][]string)
	for edge := range graph.Topology().Edges() {
		id := string(edge.ID())
		bounds[id] = reachedIDs(walkIn(t, root, queryBounds, id))
	}
	for loop := range graph.Topology().Loops() {
		id := string(loop.ID())
		bounds[id] = reachedIDs(walkIn(t, root, queryBounds, id))
	}

	t.Run("gives every node whose boundary reaches an edge, and no other", func(t *testing.T) {
		for edge := range graph.Topology().Edges() {
			shape := string(edge.ID())
			for node, edges := range boundaryOf {
				assert.Equal(t,
					slices.Contains(edges, shape),
					slices.Contains(bounds[shape], node),
					"%s in the boundary of %s, and %s among what %s bounds", shape, node, node, shape,
				)
			}
		}
	})

	t.Run("gives every node which names a loop, and no other", func(t *testing.T) {
		for loop := range graph.Topology().Loops() {
			shape := string(loop.ID())
			for node, loops := range boundaries {
				assert.Equal(t,
					slices.Contains(loops, shape),
					slices.Contains(bounds[shape], node),
					"%s among the boundaries of %s, and %s among what %s bounds", shape, node, node, shape,
				)
			}
		}
	})

	t.Run("reads the answer the story measured", func(t *testing.T) {
		assert.Equal(t, []string{"site:S-101"}, bounds["geom:L-101"])
		assert.Equal(t, []string{"site:S-101", "site:S-113"}, bounds["geom:E-1-A2-B2"])
	})
}

// TestTraverseBoundaryOfNamesTheTypeOfWhatBacksAnEdge checks the type written
// beside each backing element over the budget model, where one edge is backed by
// a doorway and the wall it is cut into, and telling the two apart is the point.
func TestTraverseBoundaryOfNamesTheTypeOfWhatBacksAnEdge(t *testing.T) {
	root, err := filepath.Abs(budgetRoot)
	require.NoError(t, err)

	testCases := []struct {
		name          string
		subject       string
		edge          string
		expected      []string
		expectedTypes []string
	}{
		{
			name:          "names a doorway and the wall it is cut into by their types",
			subject:       "site:S-102",
			edge:          "geom:E-1-B2-C2",
			expected:      []string{"site:D-101", "site:W-111"},
			expectedTypes: []string{"Doorway", "Partition"},
		},
		{
			name:          "names a wall alone by its type",
			subject:       "site:S-102",
			edge:          "geom:E-1-C1-C2",
			expected:      []string{"site:W-102"},
			expectedTypes: []string{"Partition"},
		},
		{
			name:    "writes neither for a virtual edge",
			subject: "site:S-102",
			edge:    "geom:E-1-B1-C1",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := walkIn(t, root, queryBoundaryOf, testCase.subject)

			entry, ok := resultFor(result, testCase.edge)
			require.True(t, ok, "%s is in the boundary of %s", testCase.edge, testCase.subject)

			assert.Equal(t, testCase.expected, entry.Backing)
			assert.Equal(t, testCase.expectedTypes, entry.BackingTypes)
		})
	}
}

// TestTraverseBackingTypesAreTheTypesGetGives holds backing-types to get over
// every edge of every space's boundary in the budget model: the two arrays are
// the same length, and each type is the one get gives for the element at that
// position. A table of expected literals would check the edges somebody thought
// of; this checks all of them.
func TestTraverseBackingTypesAreTheTypesGetGives(t *testing.T) {
	root, err := filepath.Abs(budgetRoot)
	require.NoError(t, err)

	graph, _ := dfcad.LoadGraph(root)
	require.NotNil(t, graph)

	types := make(map[string]string)
	backed := 0
	for node := range graph.Nodes().All() {
		if node.Kind() != dfcad.KindSpace {
			continue
		}

		for _, entry := range walkIn(t, root, queryBoundaryOf, string(node.ID())).Results {
			require.Len(t, entry.BackingTypes, len(entry.Backing), "%s of %s", entry.ID, node.ID())

			for i, element := range entry.Backing {
				if _, ok := types[element]; !ok {
					types[element] = fetched(t, root, element).Type
				}
				assert.Equal(t, types[element], entry.BackingTypes[i], "%s backing %s", element, entry.ID)
				backed++
			}
		}
	}

	require.NotZero(t, backed, "the budget model has walls")
}

// TestTraverseBackingTypesAreWrittenExactlyWhereBackingIs is its own function
// because it asserts on the bytes rather than the decoded result: a field which
// is absent and a field which is empty decode to the same nil slice, and only one
// of them is what the contract promises. It also holds the key order, which is
// the order the fields are declared in.
func TestTraverseBackingTypesAreWrittenExactlyWhereBackingIs(t *testing.T) {
	testCases := []struct {
		name     string
		files    map[string]string
		subject  string
		refused  bool
		expected map[string][]string
	}{
		{
			name:    "writes them for a physical edge and not for a virtual one",
			files:   traversableModel(),
			subject: "site:S-101",
			expected: map[string][]string{
				"geom:E-01": nil,
				"geom:E-02": {"Partition"},
				"geom:E-03": nil,
				"geom:E-04": nil,
			},
		},
		{
			name:    "writes neither for an edge whose backing element does not resolve",
			refused: true,
			files: map[string]string{
				"registry.dfc": traverseRegistry,
				"entities/site.dfc": strings.Replace(traverseModel,
					"(backed-by site:W-01)", "(backed-by site:W-99)", 1),
			},
			subject: "site:S-101",
			expected: map[string][]string{
				"geom:E-01": nil,
				"geom:E-02": nil,
				"geom:E-03": nil,
				"geom:E-04": nil,
			},
		},
		{
			// Only a model the load refused holds an element which declares no
			// type: one with no (type ...) at all is not held, and one whose type
			// could not be read is held without one. The answer is read through
			// the refusal all the same.
			name: "writes an empty type for an element which declares none, keeping the two aligned",
			files: map[string]string{
				"registry.dfc": traverseRegistry,
				"entities/site.dfc": strings.Replace(traverseModel,
					"  (type Partition)\n", "  (type \"Partition\")\n", 1),
			},
			refused: true,
			subject: "site:S-101",
			expected: map[string][]string{
				"geom:E-01": nil,
				"geom:E-02": {""},
				"geom:E-03": nil,
				"geom:E-04": nil,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, testCase.files))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run([]string{"traverse", queryBoundaryOf, testCase.subject}, &stdout, &stderr), stderr.String())

			var raw struct {
				Refused bool              `json:"refused"`
				Results []json.RawMessage `json:"results"`
			}
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &raw))
			require.Equal(t, testCase.refused, raw.Refused, stderr.String())
			require.Len(t, raw.Results, len(testCase.expected))

			for _, message := range raw.Results {
				keys := objectKeys(t, message)

				var entry traversed
				require.NoError(t, json.Unmarshal(message, &entry))

				expected, ok := testCase.expected[entry.ID]
				require.True(t, ok, "%s is expected in the boundary", entry.ID)

				assert.Equal(t, expected, entry.BackingTypes, entry.ID)
				assert.Equal(t, slices.Contains(keys, "backing"), slices.Contains(keys, "backing-types"),
					"%s writes backing-types exactly where it writes backing", entry.ID)
				assert.NotContains(t, keys, "backing-kinds", entry.ID)

				if expected != nil {
					assert.Equal(t, []string{"backing", "backing-types", "span"}, keys[len(keys)-3:], entry.ID)
				}
			}
		})
	}
}

// objectKeys is the keys of one JSON object, in the order they were written.
func objectKeys(t *testing.T, message json.RawMessage) []string {
	t.Helper()

	decoder := json.NewDecoder(bytes.NewReader(message))

	open, err := decoder.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('{'), open)

	var keys []string
	for decoder.More() {
		key, err := decoder.Token()
		require.NoError(t, err)
		keys = append(keys, key.(string))

		var skipped json.RawMessage
		require.NoError(t, decoder.Decode(&skipped))
	}

	closing, err := decoder.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('}'), closing)

	return keys
}

// resultFor is the result a walk reached under one id.
func resultFor(result traverseResult, id string) (traversed, bool) {
	for _, entry := range result.Results {
		if entry.ID == id {
			return entry, true
		}
	}
	return traversed{}, false
}

// fetched is what get gives for one id of the model rooted at dir.
func fetched(t *testing.T, dir, id string) getEntity {
	t.Helper()

	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"get", id}, &stdout, &stderr), stderr.String())

	return listed[getResult](t, stdout.String()).Entity
}

// TestTraverseRendersForAPerson checks that the human rendering says what was
// found without changing what a caller reads on stdout.
func TestTraverseRendersForAPerson(t *testing.T) {
	dir := tree(t, traversableModel())

	quiet := func(t *testing.T, args ...string) (string, string) {
		t.Helper()

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run(append([]string{
			"traverse", "--root", dir, queryContains, "--depth", depthAll, "site:S-01",
		}, args...), &stdout, &stderr), stderr.String())

		return stdout.String(), stderr.String()
	}

	machine, machineReport := quiet(t)
	human, humanReport := quiet(t, "--format", formatHuman)
	both, bothReport := quiet(t, "--format", formatHuman, "-v")

	assert.Equal(t, machine, human)
	assert.Equal(t, machine, both)

	assert.Contains(t, humanReport, "contains site:S-01: 6 results, deepest at 4")
	assert.NotContains(t, machineReport, "6 results")

	// Verbosity adds the detail behind the summary, and only where the run was
	// also asked to render its result.
	assert.NotContains(t, humanReport, "site:S-101a: containment at 4")
	assert.Contains(t, bothReport, "site:S-101a: containment at 4")
}

// traversalOrder is the documented order of a traversal which groups: depth
// first, and then id.
func traversalOrder(a, b map[string]any) int {
	return cmp.Or(cmp.Compare(a["depth"].(float64), b["depth"].(float64)), byID(a, b))
}

// TestTraverseFiltersWrittenTwiceAnswerTheUnion is the property both of
// traverse's filters promise: within one flag a result is reported when it
// satisfies any of the values, and the walk beneath them is the same walk.
func TestTraverseFiltersWrittenTwiceAnswerTheUnion(t *testing.T) {
	budget, err := filepath.Abs(budgetRoot)
	require.NoError(t, err)

	testCases := []struct {
		name   string
		args   []string
		flag   string
		first  string
		second string
	}{
		{
			name:   "reports the results of either kind",
			args:   []string{"traverse", queryContains, "site:B-01", "--depth", "all"},
			flag:   flagKind,
			first:  "Space",
			second: "Element",
		},
		{
			// The case the story reproduced: before a repeat was honoured this
			// answered with the meeting rooms alone.
			name:   "reports the results of either type",
			args:   []string{"traverse", queryContains, "site:B-01", "--depth", "all"},
			flag:   flagType,
			first:  "Office",
			second: "MeetingRoom",
		},
		{
			name:   "reports the results of either type one step from a storey",
			args:   []string{"traverse", queryContains, "site:L-01"},
			flag:   flagType,
			first:  "Office",
			second: "Corridor",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assertFilterIsAUnion(t, budget, testCase.args,
				testCase.flag, testCase.first, testCase.second, "results", traversalOrder)
		})
	}
}

// tieFiles is a plan in which a room two steps from site:R-S is bordered by two
// rooms one step from it, with room A written in whichever file is named. Room
// C shares geom:E-AC with room A and geom:E-BC with room B.
func tieFiles(roomAIn string) map[string]string {
	files := map[string]string{
		"registry.dfc": `(project (label "Tie fixture") (globalid-namespace "https://example.org/models/tie"))
(namespace frame (description "Frames."))
(namespace geom (description "Geometric nodes."))
(namespace site (description "Semantic nodes."))
(frame frame:b (label "Grid") (unit m))
(type Room (kind Space) (geometry area) (description "A room."))
`,
		"entities/geometry.dfc": `(vertex geom:V-1 (frame frame:b))
(vertex geom:V-2 (frame frame:b))
(edge geom:E-1 (frame frame:b) (vertices geom:V-1 geom:V-2))
(edge geom:E-S (frame frame:b) (vertices geom:V-2 geom:V-1))
(edge geom:E-AC (frame frame:b) (vertices geom:V-2 geom:V-1))
(edge geom:E-BC (frame frame:b) (vertices geom:V-2 geom:V-1))
(loop geom:L-S (frame frame:b) (edges geom:E-1 geom:E-S))
(loop geom:L-A (frame frame:b) (edges geom:E-1 geom:E-AC))
(loop geom:L-B (frame frame:b) (edges geom:E-1 geom:E-BC))
(loop geom:L-C (frame frame:b) (edges geom:E-AC geom:E-BC))
`,
		"entities/a.dfc": `(node site:R-S (label "Start") (kind Space) (type Room) (geometry area) (frame frame:b) (boundary geom:L-S))
(node site:R-C (label "Far") (kind Space) (type Room) (geometry area) (frame frame:b) (boundary geom:L-C))
`,
		"entities/b.dfc": `(node site:R-B (label "B") (kind Space) (type Room) (geometry area) (frame frame:b) (boundary geom:L-B))
`,
	}

	files[roomAIn] += `(node site:R-A (label "A") (kind Space) (type Room) (geometry area) (frame frame:b) (boundary geom:L-A))
`

	return files
}

// TestTraverseAdjacencyIsTheSameWhicheverFileANodeIsIn is its own function
// because it compares two models rather than asserting one: moving a room into
// another file changes nothing about the plan, so it changes nothing about the
// bytes an adjacency walk writes — including which neighbour a room two steps
// away was reached from, and so the edges its via names.
func TestTraverseAdjacencyIsTheSameWhicheverFileANodeIsIn(t *testing.T) {
	once := func(t *testing.T, roomAIn string) string {
		t.Helper()

		t.Chdir(tree(t, tieFiles(roomAIn)))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run(
			[]string{"traverse", queryAdjacentTo, "--depth", depthAll, "site:R-S"}, &stdout, &stderr,
		), stderr.String())

		return stdout.String()
	}

	before := once(t, "entities/a.dfc")
	after := once(t, "entities/z.dfc")

	// Every byte but the moved room's span, which says where it is written and
	// so is the one thing that is meant to move with it.
	assert.Equal(t, withoutSpans(t, before), withoutSpans(t, after))

	far, ok := resultFor(listed[traverseResult](t, before), "site:R-C")
	require.True(t, ok)
	assert.Equal(t, []string{"geom:E-AC"}, far.Via, "reached from site:R-A, the nearer room with the smaller id")
}

// spanOf is a span as traverse writes one, with the comma which separates it
// from the key before it.
var spanOf = regexp.MustCompile(`,"span":"[^"]*"`)

// withoutSpans is what traverse wrote with every span removed and every other
// byte left where it was, so two of them are equal exactly when everything
// around the spans is.
func withoutSpans(t *testing.T, stdout string) string {
	t.Helper()

	require.Regexp(t, spanOf, stdout, "the answer carries spans to remove")

	return spanOf.ReplaceAllString(stdout, "")
}
