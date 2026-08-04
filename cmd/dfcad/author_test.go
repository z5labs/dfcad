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

// authorModel is the model the write commands change: rooms the registry has a
// routing rule for, one of them written inside another so that retiring it has
// a referrer to report, and a campus nothing points at.
const authorModel = `(node site:S-101
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building))

(node site:S-102
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:S-101))

(node site:S-103
  (label "Meeting Room C")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building))

(node site:C-01
  (label "West campus")
  (kind Zone)
  (type Campus))
`

// authored is the fixture tree the write commands are run against. It shares
// the registry of the listing tests, because the routing rules a write follows
// are the ones `dfcad route` reports and there is no reason for two of them.
func authored() map[string]string {
	return map[string]string{"registry.dfc": listRegistry, "entities/site.dfc": authorModel}
}

// invoke runs one invocation against the model beneath root, requiring the exit
// code it was told to expect, and returns what it wrote to each stream.
//
// Every test here drives the command through [run] rather than through a
// subprocess, so this is the one place which knows that the root is a flag and
// that a failing run is worth quoting stderr for.
func invoke(t *testing.T, expectedCode int, root string, args ...string) (stdout, stderr string) {
	t.Helper()

	var out, report bytes.Buffer
	require.Equal(t, expectedCode, run(append(args, "--root", root), &out, &report), report.String())

	return out.String(), report.String()
}

// wrote runs one invocation against a fresh fixture, requiring it to have
// succeeded, and returns the result it wrote together with the model root.
func wrote(t *testing.T, args ...string) (writeResult, string) {
	t.Helper()

	root := tree(t, authored())
	stdout, _ := invoke(t, exitSuccess, root, args...)

	return listed[writeResult](t, stdout), root
}

// entities is the entity file of the fixture as it stands.
func entities(t *testing.T, root string) string {
	t.Helper()

	src, err := os.ReadFile(filepath.Join(root, "entities", "site.dfc"))
	require.NoError(t, err)

	return string(src)
}

// node is one node of the model beneath root, requiring the model to hold it.
func node(t *testing.T, root string, id dfcad.ID) *dfcad.SemanticNode {
	t.Helper()

	graph, diags := dfcad.LoadGraph(root)
	require.Empty(t, diags)

	found, ok := graph.Node(id)
	require.True(t, ok, "the model holds %s", id)

	return found
}

// refusal runs one invocation against a fresh fixture, requiring it to have
// been refused, and returns what it said on stderr.
//
// Nothing on stdout is half of what is being asserted: a run which produced no
// result writes no result object, so a caller piping stdout never has to tell a
// change which happened from one which did not.
func refusal(t *testing.T, args ...string) string {
	t.Helper()

	root := tree(t, authored())
	before := contents(t, root)

	stdout, stderr := invoke(t, exitUsage, root, args...)

	assert.Empty(t, stdout)
	assert.Equal(t, before, contents(t, root), "a refused change writes nothing")

	return stderr
}

func TestRunAddNode(t *testing.T) {
	testCases := []struct {
		name            string
		args            []string
		expectedPath    string
		expectedEffects []string
		expectedNode    string
	}{
		{
			name: "writes a node with every axis into the file the rules route it to",
			args: []string{
				"add-node", "--kind", "Space", "--type", "MeetingRoom",
				"--geometry", "area", "--frame", "frame:building",
				"--label", "Meeting Room D", "site:S-104",
			},
			expectedPath:    "entities/site.dfc",
			expectedEffects: []string{"created node site:S-104"},
			expectedNode: `(node
  site:S-104
  (label "Meeting Room D")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building))`,
		},
		{
			name:            "writes a node with no geometry, no frame and no label",
			args:            []string{"add-node", "--kind", "Zone", "--type", "Campus", "site:C-02"},
			expectedPath:    "entities/campuses.dfc",
			expectedEffects: []string{"created node site:C-02"},
			expectedNode:    `(node site:C-02 (kind Zone) (type Campus))`,
		},
		{
			name: "writes it where --file says instead of where the rules would",
			args: []string{
				"add-node", "--kind", "Zone", "--type", "Campus",
				"--file", "entities/site.dfc", "site:C-02",
			},
			expectedPath:    "entities/site.dfc",
			expectedEffects: []string{"created node site:C-02"},
			expectedNode:    `(node site:C-02 (kind Zone) (type Campus))`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, root := wrote(t, testCase.args...)

			assert.False(t, result.DryRun)
			assert.Equal(t, []string{testCase.expectedPath}, files(t, root, result))
			assert.Equal(t, testCase.expectedEffects, effects(result))

			written, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(testCase.expectedPath)))
			require.NoError(t, err)
			assert.Contains(t, string(written), testCase.expectedNode)
		})
	}
}

// TestRunAddNodeRefusesWhatTheRegistryDoesNotDeclare checks that a refusal from
// the engine reaches the caller as a usage error naming what would have been
// permitted.
//
// Which values each axis permits is the engine's rule and is walked there, in
// TestTxAddNodeChecksEveryAxisAgainstTheRegistry. What is checked here is what
// only this layer decides: that the refusal is exit 3 with nothing on stdout,
// and that an axis which names nothing is answered before the routing rules are
// consulted rather than as a node no rule places.
func TestRunAddNodeRefusesWhatTheRegistryDoesNotDeclare(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedMentions []string
	}{
		{
			name: "refuses a type nothing declares, naming the declared ones",
			args: []string{
				"add-node", "--kind", "Space", "--type", "BoardRoom",
				"--geometry", "area", "site:S-104",
			},
			expectedMentions: []string{"BoardRoom", "MeetingRoom", "Campus"},
		},
		{
			name: "refuses a kind which is not one of the seven, naming them",
			args: []string{
				"add-node", "--kind", "Room", "--type", "MeetingRoom",
				"--geometry", "area", "site:S-104",
			},
			expectedMentions: []string{"Room", "Space", "Interface"},
		},
		{
			name:             "refuses an argument which is not an id at all",
			args:             []string{"add-node", "--kind", "Zone", "--type", "Campus", "S-104"},
			expectedMentions: []string{"S-104"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			report := refusal(t, testCase.args...)

			for _, mention := range testCase.expectedMentions {
				assert.Contains(t, report, mention)
			}
		})
	}
}

// TestRunAddNodeRefusesATakenID is its own function because the assertion is
// about where the thing which already holds the id is defined, which none of
// the axis refusals says anything about.
func TestRunAddNodeRefusesATakenID(t *testing.T) {
	report := refusal(t,
		"add-node", "--kind", "Space", "--type", "MeetingRoom",
		"--geometry", "area", "--frame", "frame:building", "site:S-101",
	)

	assert.Contains(t, report, "site:S-101")
	assert.Contains(t, report, filepath.Join("entities", "site.dfc"))
}

// TestRunAddNodeNeverReissuesARetiredID checks the half of
// [0002](../../docs/decisions/0002-immutable-id-mutable-label.md) which says an
// id is not freed by the thing it named ceasing to exist.
func TestRunAddNodeNeverReissuesARetiredID(t *testing.T) {
	root := tree(t, authored())

	invoke(t, exitSuccess, root, "retire", "--reason", "Never built.", "site:C-01")

	stdout, stderr := invoke(t, exitUsage, root, "add-node", "--kind", "Zone", "--type", "Campus", "site:C-01")

	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "site:C-01")
	assert.Contains(t, stderr, "retired")
}

func TestRunSetLabel(t *testing.T) {
	testCases := []struct {
		name          string
		args          []string
		expectedLabel string
	}{
		{
			name:          "changes the label",
			args:          []string{"set-label", "site:S-101", "Board Room"},
			expectedLabel: "Board Room",
		},
		{
			name:          "removes the label when it is set to nothing",
			args:          []string{"set-label", "site:S-101", ""},
			expectedLabel: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, root := wrote(t, testCase.args...)

			assert.Equal(t, []string{"entities/site.dfc"}, files(t, root, result))
			assert.Equal(t, []string{"modified node site:S-101"}, effects(result))

			renamed := node(t, root, "site:S-101")
			assert.Equal(t, testCase.expectedLabel, renamed.Label())

			// Identity, everything derived from it and every other axis of the
			// node are what they were: a rename is a rename.
			assert.Equal(t, dfcad.ID("site:S-101"), renamed.ID())
			assert.Equal(t, dfcad.KindSpace, renamed.Kind())
			assert.Equal(t, "MeetingRoom", renamed.Type())

			// And so is the reference written to it, which is what would break
			// if a rename were a re-identification.
			assert.Contains(t, entities(t, root), "(within site:S-101)")
		})
	}
}

func TestRunSetLabelRefusesWhatItCannotRename(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedMentions []string
	}{
		{
			name:             "refuses an id nothing answers to, naming the nearest",
			args:             []string{"set-label", "site:S-1O1", "Board Room"},
			expectedMentions: []string{"site:S-1O1", "site:S-101"},
		},
		{
			name:             "refuses an invocation with no label to set",
			args:             []string{"set-label", "site:S-101"},
			expectedMentions: []string{"label"},
		},
		{
			name:             "refuses an argument too many",
			args:             []string{"set-label", "site:S-101", "Board Room", "and another"},
			expectedMentions: []string{"and another"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			report := refusal(t, testCase.args...)

			for _, mention := range testCase.expectedMentions {
				assert.Contains(t, report, mention)
			}
		})
	}
}

func TestRunRetire(t *testing.T) {
	result, root := wrote(t, "retire", "--reason", "Never built.", "--date", "2026-06-01", "site:C-01")

	assert.Equal(t, []string{"entities/site.dfc"}, files(t, root, result))
	assert.Equal(t, []string{"modified node site:C-01"}, effects(result))

	retirement, ok := node(t, root, "site:C-01").Retirement()
	require.True(t, ok)
	assert.Equal(t, "Never built.", retirement.Reason())
	assert.Equal(t, "2026-06-01", retirement.Date().Format("2006-01-02"))

	replacement, ok := retirement.SupersededBy()
	assert.False(t, ok)
	assert.Empty(t, replacement)
}

// TestRunRetireRefusesANodeStillReferenced is the refusal which keeps the model
// from holding a reference to something which says it stopped existing.
func TestRunRetireRefusesANodeStillReferenced(t *testing.T) {
	report := refusal(t, "retire", "--reason", "Knocked through.", "site:S-101")

	assert.Contains(t, report, "site:S-101")
	assert.Contains(t, report, "site:S-102", "the refusal names every referrer")
	assert.Contains(t, report, "within")
}

// TestRunRetireRedirectsTheReferencesToTheReplacement is the other half of the
// refusal above: a replacement is what makes those references answerable, so
// the change which supplies one moves them.
func TestRunRetireRedirectsTheReferencesToTheReplacement(t *testing.T) {
	result, root := wrote(t,
		"retire", "--reason", "Knocked through.", "--replacement", "site:S-103",
		"--date", "2026-06-01", "site:S-101",
	)

	assert.Equal(t, []string{"modified node site:S-102", "modified node site:S-101"}, effects(result))

	retirement, ok := node(t, root, "site:S-101").Retirement()
	require.True(t, ok)

	replacement, ok := retirement.SupersededBy()
	require.True(t, ok)
	assert.Equal(t, dfcad.ID("site:S-103"), replacement)

	within, ok := node(t, root, "site:S-102").Within()
	require.True(t, ok)
	assert.Equal(t, dfcad.ID("site:S-103"), within, "the reference moved to what replaced it")
}

func TestRunRetireRefusesWhatItCannotRetire(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedMentions []string
	}{
		{
			name:             "refuses a retirement which says nothing about why",
			args:             []string{"retire", "site:C-01"},
			expectedMentions: []string{"reason"},
		},
		{
			name:             "refuses an id nothing answers to",
			args:             []string{"retire", "--reason", "Never built.", "site:C-99"},
			expectedMentions: []string{"site:C-99"},
		},
		{
			name: "refuses a date which is not one",
			args: []string{
				"retire", "--reason", "Never built.",
				"--date", "the first of June", "site:C-01",
			},
			expectedMentions: []string{"the first of June", "YYYY-MM-DD"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			report := refusal(t, testCase.args...)

			for _, mention := range testCase.expectedMentions {
				assert.Contains(t, report, mention)
			}
		})
	}
}

// TestEveryWriteCommandTakesADryRun walks every subcommand and requires the
// ones which change the model to do everything except the writing, and the ones
// which do not to refuse the flag rather than ignore it.
//
// It walks [commands] rather than naming them, for the reason the contract
// walks do: a dry run which held for the command somebody remembered to test
// and not for the one added afterwards is exactly the failure this flag cannot
// have.
func TestEveryWriteCommandTakesADryRun(t *testing.T) {
	for _, cmd := range commands {
		if !cmd.writes {
			t.Run(cmd.name+" does not take a dry run, because it writes nothing", func(t *testing.T) {
				t.Chdir(t.TempDir())

				var stdout, stderr bytes.Buffer
				require.Equal(t, exitUsage, run([]string{cmd.name, "--dry-run"}, &stdout, &stderr))

				assert.Empty(t, stdout.String())
			})

			continue
		}

		t.Run(cmd.name+" describes the change and writes nothing", func(t *testing.T) {
			root := tree(t, model())
			before := contents(t, root)

			stdout, _ := invoke(t, exitSuccess, root, sample(t, cmd, "--dry-run")...)

			result := listed[writeResult](t, stdout)

			assert.True(t, result.DryRun)
			assert.NotEmpty(t, result.Files, "a dry run says what would have changed")
			assert.NotEmpty(t, result.Files[0].Diff, "and carries the diff of every file")
			assert.Empty(t, result.Written(), "and wrote nothing")

			assert.Equal(t, before, contents(t, root), "the tree is untouched")
		})
	}
}

// TestWriteIsRefusedByTheModelItWouldProduce checks that a change is judged by
// the model it leaves behind rather than by the rules the command itself knows.
func TestWriteIsRefusedByTheModelItWouldProduce(t *testing.T) {
	files := authored()
	files["entities/site.dfc"] += "\n(node site:S-105\n  (kind Space)\n  (type MeetingRoom)\n  (geometry area)\n  (frame frame:building)\n  (within site:S-999))\n"

	root := tree(t, files)

	stdout, stderr := invoke(t, exitLoad, root, "set-label", "site:S-101", "Board Room")

	assert.Empty(t, stdout, "a run which produced no result writes no result object")
	assert.Contains(t, stderr, "site:S-999")
}

// TestRetiredNodesAreLeftOutOfListingsUnlessAskedFor is the query half of
// retirement: the id goes on resolving, and the listing stops offering it.
func TestRetiredNodesAreLeftOutOfListingsUnlessAskedFor(t *testing.T) {
	root := tree(t, authored())

	invoke(t, exitSuccess, root, "retire", "--reason", "Never built.", "--date", "2026-06-01", "site:C-01")

	listing := func(t *testing.T, args ...string) listInstancesResult {
		t.Helper()

		stdout, _ := invoke(t, exitSuccess, root, append([]string{"list-instances"}, args...)...)

		return listed[listInstancesResult](t, stdout)
	}

	assert.Equal(t, []string{"site:S-101", "site:S-102", "site:S-103"}, ids(listing(t)))
	assert.Equal(t,
		[]string{"site:C-01", "site:S-101", "site:S-102", "site:S-103"},
		ids(listing(t, "--retired")),
	)

	// Asked for, they come back marked, so a caller reading a mixed listing can
	// tell which is which without asking about each of them.
	for _, instance := range listing(t, "--retired").Instances {
		assert.Equal(t, instance.ID == "site:C-01", instance.Retired)
	}
}

// TestGetAnswersForARetiredNode is the other half of it: a reference written
// years ago resolves to the node which says what happened to it.
func TestGetAnswersForARetiredNode(t *testing.T) {
	root := tree(t, authored())

	invoke(t, exitSuccess, root,
		"retire", "--reason", "Knocked through.", "--replacement", "site:S-103",
		"--date", "2026-06-01", "site:S-101",
	)

	stdout, _ := invoke(t, exitSuccess, root, "get", "site:S-101")

	result := listed[getResult](t, stdout)
	require.NotNil(t, result.Entity.Retired, "a retrieval by id says what happened to it")
	assert.Equal(t, "2026-06-01", result.Entity.Retired.Date)
	assert.Equal(t, "Knocked through.", result.Entity.Retired.Reason)
	assert.Equal(t, "site:S-103", result.Entity.Retired.SupersededBy)

	// It is still the node it was: retiring is not deleting.
	assert.Equal(t, "Meeting Room A", result.Entity.Label)
}

// TestWriteReportsForAPerson checks that the human rendering says what changed
// without changing what a caller piping stdout reads.
func TestWriteReportsForAPerson(t *testing.T) {
	root := tree(t, authored())

	quiet := func(t *testing.T, args ...string) (string, string) {
		t.Helper()

		return invoke(t, exitSuccess, root,
			append([]string{"set-label", "site:S-101", "Board Room", "--dry-run"}, args...)...)
	}

	machine, machineReport := quiet(t)
	human, humanReport := quiet(t, "--format", formatHuman)
	loud, loudReport := quiet(t, "--format", formatHuman, "-v")

	assert.Equal(t, machine, human)
	assert.Equal(t, machine, loud)

	assert.Empty(t, machineReport)
	assert.Contains(t, humanReport, "would write 1 file: modified site:S-101")
	assert.Contains(t, loudReport, "rewritten")
}

// files names each file of a result and nothing else, so that an expectation
// does not hold a temporary directory nobody can predict.
func files(t *testing.T, root string, result writeResult) []string {
	t.Helper()

	out := make([]string, 0, len(result.Files))
	for _, file := range result.Files {
		rel, err := filepath.Rel(root, file.Path)
		require.NoError(t, err)

		out = append(out, filepath.ToSlash(rel))
	}

	return out
}

// effects is what a result says the change did to the model, in the order it
// did it.
func effects(result writeResult) []string {
	out := make([]string, 0)
	for _, effect := range result.Effects() {
		out = append(out, strings.TrimSpace(string(effect.Op)+" "+effect.Tag+" "+string(effect.ID)))
	}

	return out
}
