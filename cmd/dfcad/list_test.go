// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// listRegistry is the vocabulary the model below is judged against.
//
// Fitting declares its kinds and its geometry forms out of specification order
// and permits absence as well, which is what says whether a listing reports the
// axes the registry permits or the order somebody happened to type them in.
const listRegistry = `(project
  (label "Listing fixture")
  (globalid-namespace "https://example.org/models/list"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))

(frame frame:site-grid (label "Site survey grid") (unit m))

(frame frame:building
  (label "Building local grid")
  (unit m)
  (parent frame:site-grid)
  (transform site:C-0001)
  (frame-transform
    (id site:C-0001)
    (value
      (transform
        (translation 100.0 200.0 0.0)
        (rotation 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0)
        (scale 1.0)))
    (source "Setting-out record SO-2026-014, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-02")))

(type Campus
  (kind Zone)
  (geometry absent)
  (description "A group of things administered together, which has no shape."))

(type Fitting
  (kind Interface)
  (kind Element)
  (geometry surface)
  (geometry line)
  (geometry absent)
  (description "Something fitted between two spaces."))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings."))

(type OfficeBuilding
  (kind Building)
  (geometry solid)
  (description "A building let as offices."))

(predicate area
  (unit m2)
  (shape scalar)
  (description "How much floor a space has."))

(predicate frame-transform
  (shape transform)
  (description "The rigid transform from a frame to its parent."))

(route buildings
  (kind Building)
  (type OfficeBuilding)
  (file "entities/buildings.dfc"))

(route campuses (kind Zone) (type Campus) (file "entities/campuses.dfc"))

(route rooms
  (kind Space)
  (type MeetingRoom)
  (file "entities/site.dfc")
  (description "Meeting rooms, beside the building they are in."))
`

// listModel is written with its nodes out of id order, so that a listing which
// reported the walk order rather than the id order would say so.
//
// Room A carries a claim which writes no id of its own, which is the ordinary
// case — an id is required only of a claim something references — so that the
// walks over every command reach a question `dfcad resolve` can answer and a
// correction which has to mint an id has something to correct.
//
// Room C carries two, both named, which disagree. That is a state a model is
// routinely in and it is the one a retraction is issued against: naming a claim
// on a command line means naming it by the id it wrote, so the fixture has to
// hold claims which wrote one.
const listModel = `(node site:S-102
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building))

(node site:B-01
  (label "Block A")
  (kind Building)
  (type OfficeBuilding)
  (geometry solid)
  (frame frame:site-grid))

(node site:S-101
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (area
    (value 24.2 m2)
    (source "As-built check AB-2026-009, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-05-06")))

(node site:S-103
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:site-grid)
  (area
    (id site:M-0001)
    (value 31.0 m2)
    (source "Design drawing DR-2026-004, Acme Architects")
    (method method:estimate)
    (accuracy (independent 0.5 m2))
    (date "2026-01-12"))
  (area
    (id site:M-0002)
    (value 31.4 m2)
    (source "As-built check AB-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-05-06")))

(node site:C-01
  (label "West campus")
  (kind Zone)
  (type Campus))
`

// model is the fixture tree both commands are run against.
func model() map[string]string {
	return map[string]string{"registry.dfc": listRegistry, "entities/site.dfc": listModel}
}

// listed decodes the one JSON object on stdout into T.
//
// It requires that stdout holds exactly one JSON value and nothing after it,
// because that is the contract as a caller experiences it rather than a detail
// of this test.
func listed[T any](t *testing.T, stdout string) T {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(stdout))

	var result T
	require.NoError(t, decoder.Decode(&result))

	_, err := decoder.Token()
	require.ErrorIs(t, err, io.EOF, "stdout holds more than one JSON value")

	return result
}

// names is each listed type as "name kinds geometries instances", which is
// every axis the discovery path promises in one readable line.
func names(result listTypesResult) []string {
	out := make([]string, 0, len(result.Types))
	for _, declared := range result.Types {
		geometries := declared.Geometries
		if declared.Absent {
			geometries = append(slices.Clone(geometries), "absent")
		}
		out = append(out, strings.Join(declared.Kinds, "+")+" "+strings.Join(geometries, "+")+" "+
			declared.Name+" "+plural(declared.Instances, "instance"))
	}
	return out
}

// ids is the id of each listed instance.
func ids(result listInstancesResult) []string {
	out := make([]string, 0, len(result.Instances))
	for _, instance := range result.Instances {
		out = append(out, instance.ID)
	}
	return out
}

func TestRunListTypes(t *testing.T) {
	testCases := []struct {
		name          string
		files         map[string]string
		expectedTypes []string
	}{
		{
			name:  "reports every declared type with its axes and its instance count",
			files: model(),
			expectedTypes: []string{
				"Zone absent Campus 1 instance",
				"Element+Interface line+surface+absent Fitting 0 instances",
				"Space area MeetingRoom 3 instances",
				"Building solid OfficeBuilding 1 instance",
			},
		},
		{
			name:          "reports an empty model as no types at all",
			files:         map[string]string{"notes.md": "nothing to see"},
			expectedTypes: []string{},
		},
		{
			name:          "reports a registry which declares no type as no types at all",
			files:         map[string]string{"registry.dfc": "(project (globalid-namespace \"https://example.org/e\"))\n"},
			expectedTypes: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, testCase.files))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitSuccess, run([]string{"list-types"}, &stdout, &stderr), stderr.String())

			result := listed[listTypesResult](t, stdout.String())
			assert.Equal(t, outputVersion, result.Version)
			assert.Equal(t, "list-types", result.Command)
			assert.Equal(t, testCase.expectedTypes, names(result))
		})
	}
}

// TestRunListTypesOrdersByName is its own function because it asserts about the
// order of the whole list rather than about what one entry says.
func TestRunListTypesOrdersByName(t *testing.T) {
	t.Chdir(tree(t, model()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"list-types"}, &stdout, &stderr), stderr.String())

	result := listed[listTypesResult](t, stdout.String())

	ordered := make([]string, 0, len(result.Types))
	for _, declared := range result.Types {
		ordered = append(ordered, declared.Name)
	}

	assert.Equal(t, []string{"Campus", "Fitting", "MeetingRoom", "OfficeBuilding"}, ordered)
	assert.True(t, sortedStrings(ordered))
}

// sortedStrings reports whether items are in ascending order.
func sortedStrings(items []string) bool {
	for i := 1; i < len(items); i++ {
		if items[i-1] > items[i] {
			return false
		}
	}
	return true
}

func TestRunListInstances(t *testing.T) {
	testCases := []struct {
		name        string
		args        []string
		expectedIDs []string
	}{
		{
			name:        "reports every instance when no type is named",
			args:        []string{"list-instances"},
			expectedIDs: []string{"site:B-01", "site:C-01", "site:S-101", "site:S-102", "site:S-103"},
		},
		{
			name:        "reports the instances of one type",
			args:        []string{"list-instances", "MeetingRoom"},
			expectedIDs: []string{"site:S-101", "site:S-102", "site:S-103"},
		},
		{
			name:        "reports a declared type nothing instantiates as no instances",
			args:        []string{"list-instances", "Fitting"},
			expectedIDs: []string{},
		},
		{
			name:        "filters by kind",
			args:        []string{"list-instances", "--kind", "Space"},
			expectedIDs: []string{"site:S-101", "site:S-102", "site:S-103"},
		},
		{
			name:        "filters by frame",
			args:        []string{"list-instances", "--frame", "frame:building"},
			expectedIDs: []string{"site:S-101", "site:S-102"},
		},
		{
			name:        "combines a type with a frame",
			args:        []string{"list-instances", "MeetingRoom", "--frame", "frame:site-grid"},
			expectedIDs: []string{"site:S-103"},
		},
		{
			name:        "combines a kind with a frame",
			args:        []string{"list-instances", "--kind", "Building", "--frame", "frame:site-grid"},
			expectedIDs: []string{"site:B-01"},
		},
		{
			name:        "reports nothing when the filters agree on nothing",
			args:        []string{"list-instances", "Campus", "--kind", "Space"},
			expectedIDs: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, model()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitSuccess, run(testCase.args, &stdout, &stderr), stderr.String())

			result := listed[listInstancesResult](t, stdout.String())
			assert.Equal(t, outputVersion, result.Version)
			assert.Equal(t, "list-instances", result.Command)
			assert.Equal(t, testCase.expectedIDs, ids(result))
		})
	}
}

// TestRunListInstancesReportsIDAndLabel is its own function because it asserts
// about the whole of one entry rather than about which entries came back.
func TestRunListInstancesReportsIDAndLabel(t *testing.T) {
	t.Chdir(tree(t, model()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"list-instances", "MeetingRoom"}, &stdout, &stderr), stderr.String())

	result := listed[listInstancesResult](t, stdout.String())

	assert.Equal(t, []listedInstance{
		{ID: "site:S-101", Label: "Meeting Room A", Type: "MeetingRoom", Kind: "Space", Frame: "frame:building"},
		{ID: "site:S-102", Label: "Meeting Room B", Type: "MeetingRoom", Kind: "Space", Frame: "frame:building"},

		// A node which was never labelled reports no label rather than an
		// invented one, and the field is absent from the object entirely.
		{ID: "site:S-103", Type: "MeetingRoom", Kind: "Space", Frame: "frame:site-grid"},
	}, result.Instances)

	assert.NotContains(t, stdout.String(), `"label":""`)
}

// TestRunListInstancesOnAnEmptyModel is its own function because an empty model
// declares no type, so every filter that could be given to it names something
// undeclared and the only invocation left is the bare one.
func TestRunListInstancesOnAnEmptyModel(t *testing.T) {
	t.Chdir(tree(t, map[string]string{"notes.md": "nothing to see"}))

	var stdout, stderr bytes.Buffer

	require.Equal(t, exitSuccess, run([]string{"list-instances"}, &stdout, &stderr), stderr.String())

	result := listed[listInstancesResult](t, stdout.String())
	assert.Equal(t, "list-instances", result.Command)
	assert.Equal(t, []listedInstance{}, result.Instances)

	// An empty collection is a list rather than a null, so a caller indexing it
	// needs no special case for the model nobody has written yet.
	assert.Contains(t, stdout.String(), `"instances":[]`)
}

// TestRunListRejectsWhatTheModelDoesNotDeclare walks the three names a caller
// can get wrong.
//
// Each is a usage error rather than an empty list: a name nobody declared and a
// name nothing instantiates are different answers, and stdout stays empty
// because the run produced no result.
func TestRunListRejectsWhatTheModelDoesNotDeclare(t *testing.T) {
	declaredTypes := []string{"Campus", "Fitting", "MeetingRoom", "OfficeBuilding"}
	declaredFrames := []string{"frame:building", "frame:site-grid"}

	testCases := []struct {
		name           string
		args           []string
		expectedStderr string
	}{
		{
			name: "names an undeclared type and points at list-types",
			args: []string{"list-instances", "MeetingRoomm"},
			expectedStderr: "dfcad list-instances: " +
				UnknownTypeError{Type: "MeetingRoomm", Declared: declaredTypes}.Error() + "\n",
		},
		{
			name: "names a kind which is not one of the seven",
			args: []string{"list-instances", "--kind", "Room"},
			expectedStderr: "dfcad list-instances: " +
				UnknownKindError{Kind: "Room", Known: dfcad.Kinds()}.Error() + "\n",
		},
		{
			name: "names an undeclared frame",
			args: []string{"list-instances", "--frame", "frame:annex"},
			expectedStderr: "dfcad list-instances: " +
				UnknownFrameError{Frame: "frame:annex", Declared: declaredFrames}.Error() + "\n",
		},
		{
			name: "rejects a second type argument",
			args: []string{"list-instances", "MeetingRoom", "Campus"},
			expectedStderr: "dfcad list-instances: " +
				UnexpectedArgumentsError{Extra: []string{"Campus"}}.Error() + "\n\n" + listInstancesUsage,
		},
		{
			name: "rejects an argument to list-types, which takes none",
			args: []string{"list-types", "MeetingRoom"},
			expectedStderr: "dfcad list-types: " +
				UnexpectedArgumentsError{Extra: []string{"MeetingRoom"}}.Error() + "\n\n" + listTypesUsage,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, model()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitUsage, run(testCase.args, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Equal(t, testCase.expectedStderr, stderr.String())
		})
	}
}

// TestUnknownTypeErrorSaysWhereToLook checks that the error carries the
// declared set for a caller to branch on, and points a person at the call which
// lists it rather than printing a registry into a message.
func TestUnknownTypeErrorSaysWhereToLook(t *testing.T) {
	testCases := []struct {
		name     string
		declared []string
	}{
		{
			name:     "points at list-types when the registry declares some",
			declared: []string{"Campus", "MeetingRoom"},
		},
		{
			name:     "says the registry is empty when it declares none",
			declared: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := checkFilters(registryOf(t, testCase.declared), "Nope", "", "")

			var unknown UnknownTypeError
			require.ErrorAs(t, err, &unknown)
			assert.Equal(t, "Nope", unknown.Type)
			assert.Equal(t, testCase.declared, unknown.Declared)
			assert.Contains(t, unknown.Error(), "list-types")
		})
	}
}

// registryOf is a registry declaring one type per name and nothing else.
func registryOf(t *testing.T, types []string) *dfcad.Registry {
	t.Helper()

	var src strings.Builder
	src.WriteString("(project (globalid-namespace \"https://example.org/models/filters\"))\n")
	for _, name := range types {
		src.WriteString("(type " + name + " (kind Space) (geometry area) (description \"One type.\"))\n")
	}

	dir := tree(t, map[string]string{"registry.dfc": src.String()})

	graph, diags := dfcad.LoadGraph(dir)
	require.Empty(t, diags)

	return graph.Registry()
}

// TestCheckFiltersAcceptsWhatTheModelDeclares is the other half of the
// rejection table: every name the model does declare passes, so the check is
// not simply refusing everything.
func TestCheckFiltersAcceptsWhatTheModelDeclares(t *testing.T) {
	dir := tree(t, model())

	graph, diags := dfcad.LoadGraph(dir)
	require.Empty(t, diags)

	registry := graph.Registry()

	assert.NoError(t, checkFilters(registry, "", "", ""))
	assert.NoError(t, checkFilters(registry, "MeetingRoom", "Space", "frame:building"))

	for _, kind := range dfcad.Kinds() {
		assert.NoError(t, checkFilters(registry, "", string(kind), ""))
	}
}

// TestParseEndsTheFlagsAtADoubleDash is its own function because it is about
// the shared parsing rather than about either listing: resuming after an
// argument must not resume past a `--`, which is the one spelling there is for
// a path or a name that begins with a dash.
func TestParseEndsTheFlagsAtADoubleDash(t *testing.T) {
	testCases := []struct {
		name               string
		args               []string
		expectedPositional []string
	}{
		{
			name:               "takes everything after a double dash as an argument",
			args:               []string{"--", "-a.dfc", "-b.dfc"},
			expectedPositional: []string{"-a.dfc", "-b.dfc"},
		},
		{
			name:               "takes a flag written after a double dash as an argument",
			args:               []string{"--", "MeetingRoom", "--kind", "Space"},
			expectedPositional: []string{"MeetingRoom", "--kind", "Space"},
		},
		{
			name:               "still reads the flags written before it",
			args:               []string{"--format", formatHuman, "--", "-a.dfc"},
			expectedPositional: []string{"-a.dfc"},
		},
		{
			name:               "ends the flags after an argument as well",
			args:               []string{"a.dfc", "--", "-b.dfc"},
			expectedPositional: []string{"a.dfc", "-b.dfc"},
		},
		{
			name:               "reads a lone dash as an argument rather than as a terminator",
			args:               []string{"-", "--format", formatHuman},
			expectedPositional: []string{"-"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()

			cmd, ok := lookup("fmt")
			require.True(t, ok)

			globals := &globals{}
			flags := newFlagSet(cmd, globals)

			var stderr bytes.Buffer

			positional, code, done := parse(cmd, flags, globals,
				append([]string{"--root", dir}, testCase.args...), &stderr)

			require.False(t, done, stderr.String())
			require.Equal(t, exitSuccess, code)
			assert.Equal(t, testCase.expectedPositional, positional)
		})
	}
}

// TestRunListStillAnswersOnAModelWithDiagnostics is its own function because it
// is about a run over a model which is not sound: the listing is still a
// listing of what is there, the diagnostics still reach the person who wrote
// the file, and the exit code does not answer a question these commands were
// not asked. A discovery call which refuses to describe a tree until the tree
// is finished is one nobody reaches for while writing it.
func TestRunListStillAnswersOnAModelWithDiagnostics(t *testing.T) {
	files := model()
	files["entities/broken.dfc"] = unparseable

	for _, args := range [][]string{{"list-types"}, {"list-instances"}} {
		t.Run(args[0]+" lists what loaded and reports the rest on stderr", func(t *testing.T) {
			t.Chdir(tree(t, files))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitSuccess, run(args, &stdout, &stderr))

			// What loaded is a real answer about what loaded.
			result := object(t, stdout.String())
			assert.Equal(t, args[0], result["command"])
			assert.NotEmpty(t, result)

			// The diagnostic is for whoever wrote the file, so it is on stderr
			// and never on the stream a caller pipes.
			assert.Contains(t, stderr.String(), "broken.dfc:1:")
		})
	}
}

// TestRunListInstancesAcceptsFlagsAfterTheType is its own function because it
// is about how the arguments parse rather than about what came back. A flag
// written after the type has to narrow the listing rather than be handed back
// unread, because a filter which is silently ignored is worse than one which
// does not exist.
func TestRunListInstancesAcceptsFlagsAfterTheType(t *testing.T) {
	testCases := [][]string{
		{"list-instances", "--frame", "frame:site-grid", "MeetingRoom"},
		{"list-instances", "MeetingRoom", "--frame", "frame:site-grid"},
		{"list-instances", "MeetingRoom", "--frame=frame:site-grid"},
	}

	for _, args := range testCases {
		t.Run(strings.Join(args[1:], " ")+" lists the same instances", func(t *testing.T) {
			t.Chdir(tree(t, model()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())
			assert.Equal(t, []string{"site:S-103"}, ids(listed[listInstancesResult](t, stdout.String())))
		})
	}
}

// TestRunListOutputIsDeterministic checks that two runs over the same model
// write byte-identical results, which is what makes diffing two runs mean
// something.
func TestRunListOutputIsDeterministic(t *testing.T) {
	for _, args := range [][]string{{"list-types"}, {"list-instances"}, {"list-instances", "MeetingRoom"}} {
		t.Run(strings.Join(args, " ")+" writes the same bytes twice", func(t *testing.T) {
			var results []string
			for range 2 {
				t.Chdir(tree(t, model()))

				var stdout, stderr bytes.Buffer
				require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

				results = append(results, stdout.String())
			}

			assert.Equal(t, results[0], results[1])
		})
	}
}

// TestRunListHumanOutputNeverChangesStdout is its own function because it is
// about the one property the format flag must not have: whichever format was
// asked for, and however loud the run was told to be, stdout is the same bytes.
func TestRunListHumanOutputNeverChangesStdout(t *testing.T) {
	listing := func(t *testing.T, args ...string) (string, string) {
		t.Helper()

		t.Chdir(tree(t, model()))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

		return stdout.String(), stderr.String()
	}

	machine, machineReport := listing(t, "list-types")
	human, humanReport := listing(t, "list-types", "--format", formatHuman)
	both, bothReport := listing(t, "list-types", "--format", formatHuman, "-v")

	assert.Equal(t, machine, human)
	assert.Equal(t, machine, both)

	// The summary is behind the format flag; the detail behind it is behind the
	// verbosity flag, because the detail is already the result on stdout.
	assert.Empty(t, machineReport)
	assert.Contains(t, humanReport, "4 types, 5 instances")
	assert.NotContains(t, humanReport, "MeetingRoom: kind Space")
	assert.Contains(t, bothReport, "MeetingRoom: kind Space, geometry area, 3 instances")
	assert.Contains(t, bothReport, "Fitting: kind Element, Interface, geometry line, surface, none at all, 0 instances")

	instances, instancesReport := listing(t, "list-instances", "--format", formatHuman, "-v")
	assert.Contains(t, instancesReport, "5 instances of 3 types")
	assert.Contains(t, instancesReport, "site:S-101: Meeting Room A, Space MeetingRoom")
	assert.Contains(t, instancesReport, "site:S-103: (no label), Space MeetingRoom")
	assert.NotEmpty(t, listed[listInstancesResult](t, instances).Instances)
}

// TestRunListUsage checks that help goes to stderr and exits zero, which is the
// half of the contract that keeps prose off the stream a caller pipes.
func TestRunListUsage(t *testing.T) {
	testCases := []struct {
		name           string
		args           []string
		expectedStderr string
	}{
		{
			name:           "prints the list-types usage to stderr and succeeds",
			args:           []string{"list-types", "-h"},
			expectedStderr: listTypesUsage,
		},
		{
			name:           "prints the list-instances usage to stderr and succeeds",
			args:           []string{"list-instances", "-h"},
			expectedStderr: listInstancesUsage,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitSuccess, run(testCase.args, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Equal(t, testCase.expectedStderr, stderr.String())
		})
	}
}

// TestListErrorsAreNotSwallowed checks that a stdout which cannot be written
// reports a failure rather than an unexplained success.
func TestListErrorsAreNotSwallowed(t *testing.T) {
	for _, args := range [][]string{{"list-types"}, {"list-instances"}} {
		t.Run(args[0]+" reports a stdout it cannot write", func(t *testing.T) {
			t.Chdir(tree(t, model()))

			var stderr bytes.Buffer

			assert.Equal(t, exitLoad, run(args, brokenWriter{}, &stderr))
			assert.Contains(t, stderr.String(), "dfcad "+args[0]+":")
		})
	}
}
