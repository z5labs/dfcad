// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// roomAndItsArea is the batch the tests below apply: a node, and the claim
// about the node, which is the pair a batch exists for.
//
// Applied as two commands it is two loads and a moment in which the model holds
// a room whose area nobody has stated. Applied as one it is one statement.
const roomAndItsArea = `{
  "version": 1,
  "operations": [
    {"op": "add-node", "id": "site:S-104", "kind": "Space", "type": "MeetingRoom",
     "geometry": "area", "frame": "frame:building", "label": "Meeting Room D"},
    {"op": "add-claim", "subject": "site:S-104", "predicate": "area",
     "claim": {"value": "18.0", "unit": "m2", "source": "As-built check AB-2026-012, Acme Surveys",
               "method": "method:total-station", "accuracy": ["independent 0.05 m2"],
               "date": "2026-05-06"}}
  ]
}
`

// operationFile writes an operation file into the model root and returns the
// path to name on the command line, which is relative to the root as every path
// this interface takes is.
func operationFile(t *testing.T, root, written string) string {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(root, "ops.json"), []byte(written), 0o600))

	return "ops.json"
}

// piped runs one invocation with a batch on standard input rather than in a
// file, requiring the exit code it was told to expect.
//
// It drives [runOn] rather than [run] so that the input is the test's, which is
// the whole of what makes the piped path testable without a subprocess.
func piped(t *testing.T, expectedCode int, root, written string, args ...string) (stdout, stderr string) {
	t.Helper()

	var out, report bytes.Buffer

	code := runOn(append(args, "--root", root), strings.NewReader(written), &out, &report)
	require.Equal(t, expectedCode, code, report.String())

	return out.String(), report.String()
}

// applied is the result of an apply which succeeded, together with the model
// root it was applied to.
func applied(t *testing.T, files map[string]string, written string, args ...string) (applyResult, string) {
	t.Helper()

	root := tree(t, files)
	path := operationFile(t, root, written)

	stdout, _ := invoke(t, exitSuccess, root, append([]string{"apply", path}, args...)...)

	return listed[applyResult](t, stdout), root
}

// refusedBatch runs an apply which was refused, requiring the tree to be exactly
// what it was and stdout to be empty.
//
// Both are the whole of what all-or-nothing means from the outside: a caller
// piping stdout never has to tell a batch which landed from one which did not,
// and there is no partial state to reconcile.
func refusedBatch(t *testing.T, files map[string]string, expectedCode int, written string, args ...string) string {
	t.Helper()

	root := tree(t, files)
	path := operationFile(t, root, written)

	before := contents(t, root)

	stdout, stderr := invoke(t, expectedCode, root, append([]string{"apply", path}, args...)...)

	assert.Empty(t, stdout)
	assert.Equal(t, before, contents(t, root), "a refused batch writes nothing at all")

	return stderr
}

func TestRunApply(t *testing.T) {
	testCases := []struct {
		name              string
		written           string
		expectedOps       []string
		expectedEffects   [][]string
		expectedTotals    totals
		expectedFileCount int
	}{
		{
			name:        "applies a node and the claim about it in one change",
			written:     roomAndItsArea,
			expectedOps: []string{"add-node", "add-claim"},
			expectedEffects: [][]string{
				{"created node site:S-104"},
				{"modified node site:S-104"},
			},
			expectedTotals:    totals{Operations: 2, Created: 1, Modified: 1},
			expectedFileCount: 1,
		},
		{
			name: "applies an edge between two corners the same batch wrote",
			written: `{"operations": [
				{"op": "add-vertex", "id": "geom:V-07", "frame": "frame:building"},
				{"op": "add-vertex", "id": "geom:V-08", "frame": "frame:building"},
				{"op": "add-edge", "id": "geom:E-08", "frame": "frame:building",
				 "start": "geom:V-07", "end": "geom:V-08"}
			]}`,
			expectedOps: []string{"add-vertex", "add-vertex", "add-edge"},
			expectedEffects: [][]string{
				{"created vertex geom:V-07"},
				{"created vertex geom:V-08"},
				{"created edge geom:E-08"},
			},
			expectedTotals:    totals{Operations: 3, Created: 3},
			expectedFileCount: 1,
		},
		{
			name: "applies a batch which touches more than one file",
			written: `{"operations": [
				{"op": "add-node", "id": "site:S-104", "kind": "Space", "type": "MeetingRoom",
				 "geometry": "area", "frame": "frame:building"},
				{"op": "add-vertex", "id": "geom:V-07", "frame": "frame:building"},
				{"op": "set-label", "id": "site:S-102", "label": "Meeting Room B, renamed"}
			]}`,
			expectedOps: []string{"add-node", "add-vertex", "set-label"},
			expectedEffects: [][]string{
				{"created node site:S-104"},
				{"created vertex geom:V-07"},
				{"modified node site:S-102"},
			},
			expectedTotals:    totals{Operations: 3, Created: 2, Modified: 1},
			expectedFileCount: 2,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, _ := applied(t, model(), testCase.written)

			assert.Equal(t, outputVersion, result.Version)
			assert.Equal(t, "apply", result.Command)
			assert.False(t, result.DryRun)
			assert.Len(t, result.Files, testCase.expectedFileCount)
			assert.Equal(t, testCase.expectedTotals, result.Totals)

			require.Len(t, result.Operations, len(testCase.expectedOps))

			for at, operation := range result.Operations {
				assert.Equal(t, at+1, operation.Index, "an operation is named by its place in the batch")
				assert.Equal(t, testCase.expectedOps[at], operation.Op)
				assert.Equal(t, testCase.expectedEffects[at], spelledEffects(operation.Effects, true))
			}
		})
	}
}

// TestRunApplyWritesTheModelTheBatchDescribes is its own function because it
// asserts about the tree rather than about the result object: what a batch
// reports and what it wrote have to be the same thing.
func TestRunApplyWritesTheModelTheBatchDescribes(t *testing.T) {
	_, root := applied(t, model(), roomAndItsArea)

	written := node(t, root, "site:S-104")

	assert.Equal(t, "Meeting Room D", written.Label())
	assert.Equal(t, "MeetingRoom", written.Type())

	// The claim was written on a node the same batch created, which is the
	// dependency a batch exists to make ordinary.
	report, _ := invoke(t, exitSuccess, root, "resolve", "site:S-104", "area")
	assert.Contains(t, report, `"scalar":18`)
}

// TestRunApplyRefusesTheWholeBatchWhenTheLastOperationFails is its own function
// because what it asserts is a property of the tree rather than of an answer:
// after a batch whose last operation cannot be applied, every file is
// byte-identical to what it was.
func TestRunApplyRefusesTheWholeBatchWhenTheLastOperationFails(t *testing.T) {
	stderr := refusedBatch(t, model(), exitUsage, `{"operations": [
		{"op": "add-node", "id": "site:S-104", "kind": "Space", "type": "MeetingRoom",
		 "geometry": "area", "frame": "frame:building"},
		{"op": "add-vertex", "id": "geom:V-07", "frame": "frame:building"},
		{"op": "add-node", "id": "site:S-101", "kind": "Space", "type": "MeetingRoom",
		 "geometry": "area", "frame": "frame:building"}
	]}`)

	// The refusal names which operation failed and why, which is what makes a
	// batch of fifty fixable in one pass rather than by bisection.
	assert.Contains(t, stderr, "operation 3")
	assert.Contains(t, stderr, "add-node")
	assert.Contains(t, stderr, "site:S-101")
}

// TestRunApplyRefusesABatchWhoseModelWouldNotLoad is its own function because
// the refusal comes from the model the batch produces rather than from any one
// operation: it is the end state which is validated, once.
func TestRunApplyRefusesABatchWhoseModelWouldNotLoad(t *testing.T) {
	// Every operation is one the engine accepts: the node is new, and the
	// retirement names a replacement for the references it has to redirect.
	// What the two produce together is a node contained by itself, which is a
	// model that does not load — and the batch is refused by it.
	stderr := refusedBatch(t, authored(), exitLoad, `{"operations": [
		{"op": "add-node", "id": "site:S-104", "kind": "Space", "type": "MeetingRoom",
		 "geometry": "area", "frame": "frame:building"},
		{"op": "retire", "id": "site:S-101", "reason": "Merged into Meeting Room B.",
		 "replacement": "site:S-102"}
	]}`)

	assert.Contains(t, stderr, "site:S-102")
}

func TestRunApplyRefusesAFileWhichIsNotABatch(t *testing.T) {
	testCases := []struct {
		name             string
		written          string
		expectedMentions []string
	}{
		{
			name:             "refuses a file which is not JSON at all",
			written:          "(operations)",
			expectedMentions: []string{"ops.json"},
		},
		{
			name:             "refuses an operation nothing declares",
			written:          `{"operations": [{"op": "add-widget", "id": "site:S-104"}]}`,
			expectedMentions: []string{"operation 1", "add-widget", "add-node"},
		},
		{
			name: "refuses a member the operation does not read",
			written: `{"operations": [
				{"op": "add-node", "id": "site:S-104", "edges": ["geom:E-01"]}
			]}`,
			expectedMentions: []string{"operation 1", "edges"},
		},
		{
			name:             "refuses a batch with no operation in it",
			written:          `{"operations": []}`,
			expectedMentions: []string{"one or more operations"},
		},
		{
			name:             "refuses a file written against a version it does not read",
			written:          `{"version": 2, "operations": []}`,
			expectedMentions: []string{"version 2"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stderr := refusedBatch(t, model(), exitLoad, testCase.written)

			for _, mention := range testCase.expectedMentions {
				assert.Contains(t, stderr, mention)
			}
		})
	}
}

// TestRunApplyReportsEveryProblemOfTheFileAtOnce is its own function because it
// asserts about the set of refusals rather than about one of them: an author
// fixing a batch should not have to reissue it once per mistake.
func TestRunApplyReportsEveryProblemOfTheFileAtOnce(t *testing.T) {
	stderr := refusedBatch(t, model(), exitLoad, `{"operations": [
		{"op": "add-node"},
		{"op": "add-node", "id": "site:S-104", "kind": "Space", "type": "MeetingRoom"},
		{"op": "retire", "id": "site:S-101"}
	]}`)

	assert.Contains(t, stderr, "operation 1")
	assert.Contains(t, stderr, "operation 3")
	assert.NotContains(t, stderr, "operation 2")
}

// TestRunApplyReadsABatchFromStandardInput is its own function because it is
// about where the batch came from rather than about what it did: a generated
// batch is piped in rather than written to a file somebody then has to clean up.
func TestRunApplyReadsABatchFromStandardInput(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "reads standard input when no file is named",
			args: []string{"apply"},
		},
		{
			name: "reads standard input when it is named",
			args: []string{"apply", "-"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, model())

			stdout, _ := piped(t, exitSuccess, root, roomAndItsArea, testCase.args...)

			result := listed[applyResult](t, stdout)
			assert.Equal(t, totals{Operations: 2, Created: 1, Modified: 1}, result.Totals)

			assert.Equal(t, "Meeting Room D", node(t, root, "site:S-104").Label())
		})
	}
}

// TestRunApplyRefusesMoreThanOneOperationFile checks the one shape of
// invocation which is wrong rather than refused: a batch is one change, so it
// is one file.
func TestRunApplyRefusesMoreThanOneOperationFile(t *testing.T) {
	root := tree(t, model())
	operationFile(t, root, roomAndItsArea)

	stdout, stderr := invoke(t, exitUsage, root, "apply", "ops.json", "more.json")

	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "one operation file")
}

// TestRunApplyReportsAFileWhichIsNotThere checks that a named file which cannot
// be opened is a load failure naming it, rather than an empty batch.
func TestRunApplyReportsAFileWhichIsNotThere(t *testing.T) {
	root := tree(t, model())

	stdout, stderr := invoke(t, exitLoad, root, "apply", "missing.json")

	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "missing.json")
}

// TestRunApplyDryRunReportsEveryOperationAndWritesNothing is its own function
// because it asserts about a run which deliberately did not write: the diff of
// every file and the effect of every operation are the whole of what makes a dry
// run worth doing.
func TestRunApplyDryRunReportsEveryOperationAndWritesNothing(t *testing.T) {
	root := tree(t, model())
	path := operationFile(t, root, roomAndItsArea)

	before := contents(t, root)

	stdout, _ := invoke(t, exitSuccess, root, "apply", "--dry-run", path)

	result := listed[applyResult](t, stdout)

	assert.True(t, result.DryRun)
	assert.Equal(t, totals{Operations: 2, Created: 1, Modified: 1}, result.Totals)
	require.Len(t, result.Files, 1)
	assert.NotEmpty(t, result.Files[0].Diff, "a dry run which does not say what would change has reported nothing")

	require.Len(t, result.Operations, 2)
	assert.Equal(t, []string{"created node site:S-104"}, spelledEffects(result.Operations[0].Effects, true))

	assert.Equal(t, before, contents(t, root), "a dry run writes nothing")
}

// TestRunApplyReportsWhatTheChangeHadToSay checks that a batch reports the
// notices its operations produced, which is what a caller reads to find out
// that a claim it wrote is unrankable or now competes with another.
func TestRunApplyReportsWhatTheChangeHadToSay(t *testing.T) {
	result, _ := applied(t, model(), `{"operations": [
		{"op": "add-claim", "subject": "site:S-101", "predicate": "area",
		 "claim": {"value": "24.4", "unit": "m2", "source": "Re-measure RM-2026-004",
		           "method": "method:total-station"}}
	]}`)

	require.Len(t, result.Operations, 1)

	kinds := make([]string, 0, len(result.Notices))
	for _, notice := range result.Notices {
		kinds = append(kinds, notice.Kind)
	}

	assert.Equal(t, []string{"unrankable", "conflict"}, kinds)
	assert.Equal(t, kinds, spelledKinds(result.Operations[0].Notices))
}

// TestEveryWriteCommandIsAnOperation walks the commands which change the model
// and requires the operation file to carry each of them.
//
// It walks [commands] rather than naming them for the reason the contract walks
// do: a write command added later has to be writable in a batch the day it is
// added, or an author driving the interface with a file loses access to it
// without anything saying so.
func TestEveryWriteCommandIsAnOperation(t *testing.T) {
	written := make([]string, 0, len(commands))

	for _, cmd := range commands {
		// Applying a batch is not itself an operation of one: a batch inside a
		// batch is the same change written twice.
		if cmd.writes && cmd.name != "apply" {
			written = append(written, cmd.name)
		}
	}

	assert.ElementsMatch(t, written, dfcad.Operations())
}

// spelledKinds is the kind of each notice, which is what a caller branches on.
func spelledKinds(notices []noticeEntry) []string {
	out := make([]string, 0, len(notices))
	for _, notice := range notices {
		out = append(out, notice.Kind)
	}
	return out
}
