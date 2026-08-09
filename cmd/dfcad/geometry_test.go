// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// surveyed is the provenance flags every position these tests write carries. It
// is the same evidence for all of them, because what is under test is the
// geometry rather than the claim.
func surveyed() []string {
	return []string{
		"--source", "Interior control set IC-01, Acme Surveys",
		"--method", "method:total-station",
		"--accuracy", "independent 0.004 m",
		"--date", "2026-02-18",
		"--unit", "m",
	}
}

// scaffold is a scaffold-loop invocation of the corners given, closed by naming
// the first again.
func scaffold(corners ...string) []string {
	args := []string{
		"scaffold-loop",
		"--frame", "frame:building",
		"--namespace", "geom",
		"--predicate", "position",
		"--tolerance", "coincident",
	}
	args = append(args, surveyed()...)

	for _, corner := range append(corners, corners[0]) {
		args = append(args, "--corner", corner)
	}

	return args
}

// unclosed is a scaffold whose corner list stops at the last corner rather than
// naming the first again, which is the mistake the closure check exists for.
func unclosed() []string {
	args := scaffold("10 0 0", "14 0 0", "14 3 0", "10 3 0")

	return args[:len(args)-2]
}

// laidOut runs one scaffold against a fresh fixture, requiring it to have
// succeeded, and returns the result together with the model root.
func laidOut(t *testing.T, args ...string) (scaffoldResult, string) {
	t.Helper()

	root := tree(t, model())
	stdout, _ := invoke(t, exitSuccess, root, args...)

	return listed[scaffoldResult](t, stdout), root
}

// geometry is one geometric node of the model beneath root, requiring the model
// to hold it.
func geometry(t *testing.T, root string, id dfcad.ID) dfcad.Entity {
	t.Helper()

	graph, diags := dfcad.LoadGraph(root)
	require.Empty(t, diags)

	found, ok := graph.Entity(id)
	require.True(t, ok, "the model holds %s", id)

	return found
}

func TestRunAddVertex(t *testing.T) {
	testCases := []struct {
		name            string
		args            []string
		expectedEffects []string
		expectedWritten string
	}{
		{
			name: "writes a corner with where it is and how that is known",
			args: append([]string{
				"add-vertex",
				"--frame", "frame:building",
				"--label", "Room B, north corner",
				"--predicate", "position",
				"--value", "8.0 0.0 0.0",
			}, append(surveyed(), "geom:V-05")...),
			expectedEffects: []string{"created geom:V-05"},
			expectedWritten: `(vertex
  geom:V-05
  (label "Room B, north corner")
  (frame frame:building)
  (position
    (value (8.0 0.0 0.0) m)`,
		},
		{
			name: "writes a corner nobody has yet surveyed, which claims nothing about where it is",
			args: []string{"add-vertex", "--frame", "frame:building", "geom:V-05"},

			expectedEffects: []string{"created geom:V-05"},
			expectedWritten: `(vertex geom:V-05 (frame frame:building))`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, model())
			stdout, _ := invoke(t, exitSuccess, root, testCase.args...)

			result := listed[writeResult](t, stdout)

			assert.Equal(t, []string{"entities/geometry.dfc"}, files(t, root, result.Commit))
			assert.Equal(t, testCase.expectedEffects, spelledEffects(result.Effects(), false))
			assert.Contains(t, contents(t, root)["entities/geometry.dfc"], testCase.expectedWritten)

			vertex, ok := geometry(t, root, "geom:V-05").(*dfcad.Vertex)
			require.True(t, ok, "the model reads it back as a vertex")
			assert.Equal(t, dfcad.ID("frame:building"), vertex.Frame())
		})
	}
}

func TestRunAddEdge(t *testing.T) {
	root := tree(t, model())

	stdout, _ := invoke(t, exitSuccess, root,
		"add-edge",
		"--frame", "frame:building",
		"--label", "Room B, west wall",
		"--start", "geom:V-04", "--end", "geom:V-01",
		"geom:E-04",
	)

	result := listed[writeResult](t, stdout)
	assert.Equal(t, []string{"created geom:E-04"}, spelledEffects(result.Effects(), false))

	edge, ok := geometry(t, root, "geom:E-04").(*dfcad.Edge)
	require.True(t, ok)

	start, end := edge.Vertices()
	assert.Equal(t, dfcad.ID("geom:V-04"), start)
	assert.Equal(t, dfcad.ID("geom:V-01"), end)
}

func TestRunAddLoop(t *testing.T) {
	root := tree(t, model())

	invoke(t, exitSuccess, root,
		"add-edge", "--frame", "frame:building",
		"--start", "geom:V-04", "--end", "geom:V-01", "geom:E-04",
	)

	stdout, _ := invoke(t, exitSuccess, root,
		"add-loop",
		"--frame", "frame:building",
		"--label", "Room B boundary",
		"--edge", "geom:E-01", "--edge", "geom:E-02",
		"--edge", "geom:E-03", "--edge", "geom:E-04",
		"geom:L-01",
	)

	result := listed[writeResult](t, stdout)
	assert.Equal(t, []string{"created geom:L-01"}, spelledEffects(result.Effects(), false))

	loop, ok := geometry(t, root, "geom:L-01").(*dfcad.Loop)
	require.True(t, ok)

	// The order is the order the ring is walked, kept exactly as it was written.
	assert.Equal(t,
		[]dfcad.ID{"geom:E-01", "geom:E-02", "geom:E-03", "geom:E-04"},
		loop.Edges(),
	)
}

func TestGeometryCommandsRefuseWhatTheyCannotWrite(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedInStderr []string
	}{
		{
			name:             "refuses a geometric node written in no frame at all",
			args:             []string{"add-vertex", "geom:V-05"},
			expectedInStderr: []string{"exactly one frame"},
		},
		{
			name:             "refuses a frame the registry does not declare",
			args:             []string{"add-vertex", "--frame", "frame:mars", "geom:V-05"},
			expectedInStderr: []string{"frame:mars", "frame:building"},
		},
		{
			name:             "refuses an id namespace nobody declared",
			args:             []string{"add-vertex", "--frame", "frame:building", "shape:V-05"},
			expectedInStderr: []string{"shape", "geom"},
		},
		{
			name:             "refuses an id something in the model already holds",
			args:             []string{"add-vertex", "--frame", "frame:building", "geom:V-01"},
			expectedInStderr: []string{"geom:V-01", "already names"},
		},
		{
			name: "refuses an edge which runs from a vertex to itself",
			args: []string{
				"add-edge", "--frame", "frame:building",
				"--start", "geom:V-01", "--end", "geom:V-01", "geom:E-04",
			},
			expectedInStderr: []string{"geom:V-01", "to itself"},
		},
		{
			name: "refuses an endpoint which names nothing in the model",
			args: []string{
				"add-edge", "--frame", "frame:building",
				"--start", "geom:V-01", "--end", "geom:V-99", "geom:E-04",
			},
			expectedInStderr: []string{"geom:V-99", "nothing answers"},
		},
		{
			name: "refuses an endpoint which names something that is not a vertex",
			args: []string{
				"add-edge", "--frame", "frame:building",
				"--start", "geom:V-01", "--end", "geom:E-02", "geom:E-04",
			},
			expectedInStderr: []string{"geom:E-02", "vertex was required"},
		},
		{
			name:             "refuses a loop with no edges to traverse",
			args:             []string{"add-loop", "--frame", "frame:building", "geom:L-01"},
			expectedInStderr: []string{"one or more edges"},
		},
		{
			name: "refuses a loop naming something that is not an edge",
			args: []string{
				"add-loop", "--frame", "frame:building",
				"--edge", "geom:E-01", "--edge", "geom:V-02", "geom:L-01",
			},
			expectedInStderr: []string{"geom:V-02", "edge was required"},
		},
		{
			name:             "refuses a scaffold with no corners at all",
			args:             []string{"scaffold-loop", "--frame", "frame:building", "--namespace", "geom"},
			expectedInStderr: []string{"--corner"},
		},
		{
			name:             "refuses a scaffold with arguments it does not take",
			args:             append(scaffold("0 0 0", "4 0 0", "4 3 0"), "geom:L-01"),
			expectedInStderr: []string{"geom:L-01"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, model())
			before := contents(t, root)

			stdout, stderr := invoke(t, exitUsage, root, testCase.args...)

			assert.Empty(t, stdout, "a run which produced no result writes no result object")
			assert.Equal(t, before, contents(t, root), "a refused change writes nothing")

			for _, expected := range testCase.expectedInStderr {
				assert.Contains(t, stderr, expected)
			}
		})
	}
}

// TestRunScaffoldLoopWritesAFreshRoom is the ordinary case: a coordinate list
// somewhere nothing already is, which comes back as a closed ring of shared
// vertices and edges nobody had to name.
func TestRunScaffoldLoopWritesAFreshRoom(t *testing.T) {
	result, root := laidOut(t, scaffold("10 0 0", "14 0 0", "14 3 0", "10 3 0")...)

	assert.Equal(t, "geom:loop-1", result.Loop)
	assert.Len(t, result.Vertices, 4, "the closing corner is the first corner again, not a fifth")
	assert.Equal(t, result.Vertices, result.Created)
	assert.Len(t, result.Edges, 4)
	assert.Empty(t, result.Reused)
	assert.Empty(t, result.Snaps)
	assert.Equal(t, toleranceEntry{Name: "coincident", Value: 0.005, Unit: "m"}, result.Tolerance)
	assert.Empty(t, result.Notices, "the corners carry an accuracy, so nothing is unrankable")

	loop, ok := geometry(t, root, "geom:loop-1").(*dfcad.Loop)
	require.True(t, ok)
	assert.Equal(t, result.Edges, spelled(loop.Edges()))
}

// TestRunScaffoldLoopSharesTheWallItLandsOn is its own function because it is
// the behaviour the command exists for, and because what it asserts is a reuse
// rather than a room: one edge named by two loops is what makes a partition one
// wall rather than two which can drift apart.
func TestRunScaffoldLoopSharesTheWallItLandsOn(t *testing.T) {
	root := tree(t, model())

	// The fixture's room B runs from the origin to (4, 3). The corridor east of
	// it shares that wall, two millimetres out — which the tolerance calls one
	// point.
	stdout, stderr := invoke(t, exitSuccess, root,
		scaffold("4.002 0 0", "8 0 0", "8 3 0", "4.002 3 0")...)

	result := listed[scaffoldResult](t, stdout)

	require.Len(t, result.Snaps, 2, "both corners of the shared wall land on a vertex which is there")
	assert.Equal(t, []string{"geom:V-02", "geom:V-03"}, []string{result.Snaps[0].Vertex, result.Snaps[1].Vertex})
	assert.Equal(t, []int{1, 4}, []int{result.Snaps[0].Corner, result.Snaps[1].Corner})

	for _, snap := range result.Snaps {
		assert.InDelta(t, 0.002, snap.Distance, 1e-9, "the distance snapped is reported")
		assert.Equal(t, "m", snap.Unit)
		assert.True(t, snap.Reused)
	}

	assert.Equal(t, []string{"geom:E-02"}, result.Reused, "and the wall between them is reused too")
	assert.Len(t, result.Created, 2, "only the two corners away from the shared wall are new")

	// A surprising reuse is visible rather than silent.
	assert.Contains(t, stderr, "corner 1 reuses geom:V-02")
	assert.Contains(t, stderr, "within the tolerance coincident")

	// One edge, named by both loops.
	loop, ok := geometry(t, root, dfcad.ID(result.Loop)).(*dfcad.Loop)
	require.True(t, ok)
	assert.Contains(t, loop.Edges(), dfcad.ID("geom:E-02"))
}

// TestRunScaffoldLoopWritesANewCornerOutsideTheTolerance is the other half of
// the same measurement: a near miss has to come back as a corner of its own, or
// the tolerance means nothing.
func TestRunScaffoldLoopWritesANewCornerOutsideTheTolerance(t *testing.T) {
	// Six millimetres, against a tolerance of five.
	result, _ := laidOut(t, scaffold("4.006 0 0", "8 0 0", "8 3 0", "4.006 3 0")...)

	assert.Empty(t, result.Snaps, "a corner outside the tolerance lands on nothing")
	assert.Empty(t, result.Reused)
	assert.Len(t, result.Created, 4)
	assert.NotContains(t, result.Vertices, "geom:V-02")
}

// TestRunScaffoldLoopWarnsAboutADuplicateItWasToldToWrite is its own function
// because the assertion is about a warning rather than about a result: a run
// which wrote a vertex two millimetres from an existing one has to say so, or
// the sliver arrives with nothing pointing at it.
func TestRunScaffoldLoopWarnsAboutADuplicateItWasToldToWrite(t *testing.T) {
	root := tree(t, model())

	args := append(scaffold("4.002 0 0", "8 0 0", "8 3 0", "4.002 3 0"), "--no-snap")
	stdout, stderr := invoke(t, exitSuccess, root, args...)

	result := listed[scaffoldResult](t, stdout)

	require.Len(t, result.Snaps, 2)
	for _, snap := range result.Snaps {
		assert.False(t, snap.Reused, "the coincidence is reported and the duplicate is written")
	}

	assert.Len(t, result.Created, 4)
	assert.Empty(t, result.Reused, "with no shared corners there is no shared edge either")

	assert.Contains(t, stderr, "warning: corner 1 is")
	assert.Contains(t, stderr, "from geom:V-02")
	assert.Contains(t, stderr, "snapping is off")
	assert.InDelta(t, 0.002, result.Snaps[0].Distance, 1e-9, "and the distance is on stdout to be read exactly")
}

func TestRunScaffoldLoopRefusesAListItCannotMakeARingOf(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedInStderr []string
	}{
		{
			name: "refuses a coordinate list which does not return to where it started",
			args: unclosed(),
			expectedInStderr: []string{
				"does not close", "corner 4 is 3.0 m from corner 1", "coincident",
			},
		},
		{
			name:             "refuses a list too short to describe a ring at all",
			args:             scaffold("10 0 0", "14 0 0"),
			expectedInStderr: []string{"describe no loop"},
		},
		{
			name:             "refuses a list which visits one of its corners twice",
			args:             scaffold("10 0 0", "14 0 0", "10 0 0", "10 3 0"),
			expectedInStderr: []string{"corners 1 and 3", "visits each of its corners once"},
		},
		{
			name:             "refuses a corner of a shape the predicate does not declare",
			args:             scaffold("10 0", "14 0 0", "14 3 0", "10 3 0"),
			expectedInStderr: []string{"coordinate value", "10 0"},
		},
		{
			name: "refuses a list which visits one of its corners twice with snapping off",
			args: append(scaffold("10 0 0", "14 0 0", "10 0 0", "10 3 0"), "--no-snap"),
			expectedInStderr: []string{
				"corners 1 and 3", "visits each of its corners once",
			},
		},
		{
			// The refusal is the engine's words rather than a second set of
			// them here, and it names the predicates there are.
			name: "refuses a predicate the registry does not declare",
			args: append(scaffold("10 0 0", "14 0 0", "14 3 0", "10 3 0"),
				"--predicate", "where"),
			expectedInStderr: []string{"where", "position"},
		},
		{
			name: "refuses a tolerance the registry does not declare",
			args: append(scaffold("10 0 0", "14 0 0", "14 3 0", "10 3 0"),
				"--tolerance", "close-enough"),
			expectedInStderr: []string{"close-enough", "coincident"},
		},
		{
			name: "refuses a namespace nobody declared to mint into",
			args: append(scaffold("10 0 0", "14 0 0", "14 3 0", "10 3 0"),
				"--namespace", "shape"),
			expectedInStderr: []string{"shape", "geom"},
		},
		{
			name: "refuses a position claim with no evidence behind it",
			args: append(scaffold("10 0 0", "14 0 0", "14 3 0", "10 3 0"),
				"--source", ""),
			expectedInStderr: []string{"source"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, model())
			before := contents(t, root)

			stdout, stderr := invoke(t, exitUsage, root, testCase.args...)

			assert.Empty(t, stdout)
			assert.Equal(t, before, contents(t, root), "a refused scaffold writes nothing")

			for _, expected := range testCase.expectedInStderr {
				assert.Contains(t, stderr, expected)
			}
		})
	}
}

// TestRunScaffoldLoopReportsWhatADryRunWouldDo checks the criterion a dry run
// exists to satisfy for this command: every node which would be created and
// every snap which would happen, before anything is committed to.
func TestRunScaffoldLoopReportsWhatADryRunWouldDo(t *testing.T) {
	root := tree(t, model())
	before := contents(t, root)

	args := append(scaffold("4.002 0 0", "8 0 0", "8 3 0", "4.002 3 0"), "--dry-run")
	stdout, _ := invoke(t, exitSuccess, root, args...)

	result := listed[scaffoldResult](t, stdout)

	assert.True(t, result.DryRun)
	assert.Equal(t, before, contents(t, root), "a dry run writes nothing")

	assert.Equal(t, "geom:loop-1", result.Loop)
	assert.Len(t, result.Created, 2)
	assert.Len(t, result.Snaps, 2, "every snap which would happen")
	assert.Equal(t, []string{"geom:E-02"}, result.Reused)
	assert.NotEmpty(t, result.Files[0].Diff, "and the diff of every file it would write")
}

// TestScaffoldedGeometryIsWrittenInCanonicalForm checks the property every write
// command's output is held to: what it leaves behind already satisfies
// `fmt --check`, so a scaffold is a shortcut rather than a second dialect.
func TestScaffoldedGeometryIsWrittenInCanonicalForm(t *testing.T) {
	result, root := laidOut(t, scaffold("10 0 0", "14 0 0", "14 3 0", "10 3 0")...)

	require.NotEmpty(t, result.Written())

	// Only the files the change touched: a write command is not a formatter, so
	// the rest of the fixture is left exactly as it was written.
	var stdout, stderr strings.Builder
	args := []string{"fmt", "--check", "--root", root, "entities/geometry.dfc"}
	require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())
}

// TestRunScaffoldLoopBindsTheLoopToWhatItBounds is the half of laying out a
// room a scaffold could not previously state: the outline is minted and the
// room says the outline is its, in one change.
func TestRunScaffoldLoopBindsTheLoopToWhatItBounds(t *testing.T) {
	args := append(scaffold("10 0 0", "14 0 0", "14 3 0", "10 3 0"), "--bounds", "site:S-102")

	result, root := laidOut(t, args...)

	assert.Equal(t, "site:S-102", result.Bounds)
	assert.Equal(t, []dfcad.ID{dfcad.ID(result.Loop)}, node(t, root, "site:S-102").Boundaries())
}

func TestRunScaffoldLoopRefusesToBindALoopToSomethingWhichCarriesNone(t *testing.T) {
	root := tree(t, model())
	before := contents(t, root)

	args := append(scaffold("10 0 0", "14 0 0", "14 3 0", "10 3 0"), "--bounds", "geom:V-01")

	stdout, stderr := invoke(t, exitUsage, root, args...)

	assert.Empty(t, stdout)
	assert.Equal(t, before, contents(t, root), "a refused scaffold writes nothing")
	assert.Contains(t, stderr, "geom:V-01 names a vertex, and a node was required")
}

// TestRunScaffoldLoopMintsUnderTheMarksItWasGiven is what keeps a generated
// batch from having to be rewritten id by id to match the scheme the consuming
// repository already names its geometry in.
func TestRunScaffoldLoopMintsUnderTheMarksItWasGiven(t *testing.T) {
	args := append(scaffold("10 0 0", "14 0 0", "14 3 0", "10 3 0"),
		"--vertex-mark", "V", "--edge-mark", "E", "--loop-mark", "L")

	result, root := laidOut(t, args...)

	// The fixture already holds geom:V-01 upward, so the lowest free ordinal is
	// what is taken: a mark names, and it does not reserve a range.
	assert.Equal(t, []string{"geom:V-1", "geom:V-2", "geom:V-3", "geom:V-4"}, result.Created)
	assert.Equal(t, []string{"geom:E-1", "geom:E-2", "geom:E-3", "geom:E-4"}, result.Edges)
	assert.Equal(t, "geom:L-1", result.Loop)

	_, ok := geometry(t, root, "geom:L-1").(*dfcad.Loop)
	assert.True(t, ok, "what a mark names a loop is still a loop")
}

// TestRunScaffoldLoopReadsCornersInTheDeclaredUnit checks that --unit is no
// longer required by a command which cannot read a corner in any other unit.
//
// A corner is a coordinate in a frame rather than a value somebody chose a unit
// for, so the one unit it may legally be in is the one the position predicate
// declares — and requiring it to be written was requiring a flag whose only
// permitted value was already known.
func TestRunScaffoldLoopReadsCornersInTheDeclaredUnit(t *testing.T) {
	args := []string{
		"scaffold-loop",
		"--frame", "frame:building",
		"--namespace", "geom",
		"--predicate", "position",
		"--tolerance", "coincident",
		"--source", "Interior control set IC-01, Acme Surveys",
		"--method", "method:total-station",
		"--accuracy", "independent 0.004 m",
		"--date", "2026-02-18",
	}

	for _, corner := range []string{"10 0 0", "14 0 0", "14 3 0", "10 3 0", "10 0 0"} {
		args = append(args, "--corner", corner)
	}

	result, root := laidOut(t, args...)

	require.Len(t, result.Created, 4)

	vertex, ok := geometry(t, root, dfcad.ID(result.Created[0])).(*dfcad.Vertex)
	require.True(t, ok)

	graph, diags := dfcad.LoadGraph(root)
	require.Empty(t, diags)

	resolution, err := graph.Claims().Resolve(vertex.ID(), "position", graph.Registry())
	require.NoError(t, err)

	value, ok := resolution.Value()
	require.True(t, ok)
	assert.Equal(t, dfcad.Unit("m"), value.Unit(), "the corner was read in the unit the predicate declares")
}

// TestRunScaffoldLoopStillRefusesAUnitOtherThanTheDeclaredOne is the other half
// of the rule above: defaulting the unit does not weaken the check that a
// corner list typed in the wrong one is refused.
func TestRunScaffoldLoopStillRefusesAUnitOtherThanTheDeclaredOne(t *testing.T) {
	root := tree(t, model())

	args := []string{
		"scaffold-loop",
		"--frame", "frame:building",
		"--namespace", "geom",
		"--predicate", "position",
		"--tolerance", "coincident",
		"--unit", "mm",
		"--source", "Interior control set IC-01, Acme Surveys",
		"--method", "method:total-station",
	}

	for _, corner := range []string{"10 0 0", "14 0 0", "14 3 0", "10 3 0", "10 0 0"} {
		args = append(args, "--corner", corner)
	}

	_, stderr := invoke(t, exitUsage, root, args...)

	assert.Contains(t, stderr, "position declares m")
}
