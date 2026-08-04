// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"errors"
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
// it, and two rooms which share a wall.
//
// Every case a traversal has to keep apart is written here once: a wall which is
// inside one thing and a member of three, a room reachable from the site only
// through the two levels between them, a zone which is a member of another zone,
// and one edge which two rooms both reach.
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
`

// traversable is the fixture tree traverse is run against.
func traversableModel() map[string]string {
	return map[string]string{"registry.dfc": traverseRegistry, "entities/site.dfc": traverseModel}
}

// walk runs one traversal against the fixture and decodes what it wrote.
func walk(t *testing.T, args ...string) traverseResult {
	t.Helper()

	t.Chdir(tree(t, traversableModel()))

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
		queryContains, queryContainedBy, queryMembersOf, queryBoundaryOf, queryAdjacentTo,
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

	// Two subjects, because no one thing is in every relation: the room has an
	// outline and a neighbour, and the wall which separates it is what the zones
	// are written on.
	for _, subject := range []string{"site:S-101", "site:W-01"} {
		for _, asked := range queries {
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

	// Every other query honours all three, and none of them is refused when it
	// was not written.
	for _, asked := range queries {
		assert.NoError(t, checkFlags(asked, nil))

		if asked.name == queryBoundaryOf {
			continue
		}
		assert.NoError(t, checkFlags(asked, map[string]bool{flagDepth: true, flagKind: true, flagType: true}))
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

// TestNotTraversableNamesTheFamily checks that an id which names a shape is
// reported as what it is rather than as an id nothing holds, which is a
// different mistake with a different fix.
func TestNotTraversableNamesTheFamily(t *testing.T) {
	t.Chdir(tree(t, traversableModel()))

	graph, _ := dfcad.LoadGraph(".")

	testCases := []struct {
		name           string
		id             dfcad.ID
		expectedFamily string
	}{
		{
			name:           "reports a vertex",
			id:             "geom:V-01",
			expectedFamily: familyVertex,
		},
		{
			name:           "reports an edge",
			id:             "geom:E-01",
			expectedFamily: familyEdge,
		},
		{
			name:           "reports a loop",
			id:             "geom:L-01",
			expectedFamily: familyLoop,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := traversable(graph, testCase.id)

			var refused NotTraversableError
			require.ErrorAs(t, err, &refused)
			assert.Equal(t, string(testCase.id), refused.ID)
			assert.Equal(t, testCase.expectedFamily, refused.Family)
		})
	}

	t.Run("reports an id nothing holds as an unknown id rather than as a family", func(t *testing.T) {
		_, err := traversable(graph, "site:S-999")

		var unknown UnknownIDError
		require.ErrorAs(t, err, &unknown)
		assert.False(t, errors.As(err, &NotTraversableError{}))
	})

	t.Run("gives the node itself for an id a semantic node holds", func(t *testing.T) {
		node, err := traversable(graph, "site:S-101")

		require.NoError(t, err)
		require.NotNil(t, node)
		assert.Equal(t, dfcad.ID("site:S-101"), node.ID())
	})
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
