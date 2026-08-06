// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// object is the one JSON object on stdout.
//
// It requires that stdout holds exactly one JSON value and nothing after it,
// which is the whole of the machine contract as a caller experiences it: pipe
// it into jq, with no filtering, and it works.
func object(t *testing.T, stdout string) map[string]any {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(stdout))

	var result map[string]any
	require.NoError(t, decoder.Decode(&result), "stdout is not one JSON object")

	_, err := decoder.Token()
	require.ErrorIs(t, err, io.EOF, "stdout holds more than one JSON value")

	return result
}

// samples is a well-formed invocation of each command — the arguments written
// after its name — against the model [model] holds.
//
// It is a table rather than an argument the walks below invent because the
// commands do not take the same arguments: a listing of a whole model takes
// none, and a retrieval of one thing takes the id of the thing. Every command
// has an entry, and [sample] fails the test when one does not, so a command
// added later cannot quietly fall out of the walks which check the contract
// holds for every one of them.
var samples = map[string][]string{
	// version reads no model, so the fixture the walks build around is beside
	// the point for it and it takes no arguments either way.
	"version":        {},
	"fmt":            {},
	"list-types":     {},
	"list-instances": {},
	// A geometric listing is asked in the project's own words for which
	// predicate the nodes carry, so the predicate is named rather than
	// defaulted.
	"list-geometry": {"--predicate", "position"},
	"get":           {"site:S-101"},
	"resolve":       {"site:S-101", "area"},
	"traverse":      {"contains", "site:S-101"},
	"claims":        {"site:S-101"},
	"conflicts":     {},
	"check":         {},
	// A measurement is read out of the corners, so it is asked in the project's
	// own words for what a position is and how close two corners are one corner.
	"measure": {
		"--position", "position",
		"--tolerance", "coincident",
		"site:S-103",
	},
	// A drawing says how closely it follows the curve, so the chord tolerance
	// is named beside the two a boundary is read with. The fixture's parcel has
	// nothing curved in it, which is drawn to itself.
	"tessellate": {
		"--position", "position",
		"--tolerance", "coincident",
		"--chord", "chord-deviation",
		"site:P-01",
	},
	// A derivation is asked in the project's own vocabulary, so the three
	// predicates and the tolerance are named rather than defaulted.
	"buildable": {
		"--setback", "setback",
		"--position", "position",
		"--tolerance", "coincident",
		"site:P-01",
	},
	// A fit is between two things declared in two frames, asked in the
	// project's own words for what a position is and how close two corners are
	// one corner.
	"site": {
		"--within", "site:P-01",
		"--position", "position",
		"--tolerance", "coincident",
		"site:S-103",
	},
	// A plan is asked in the project's own words for what a position is, how
	// close two corners are one corner, and which measurements belong on a
	// sheet. The fixture's plot contains nothing, which is the empty answer
	// this exercises here and the populated one plan_test.go's own model does.
	"plan": {
		"--annotate", "setback",
		"--position", "position",
		"--tolerance", "coincident",
		"site:P-01",
	},
	// A review needs a revision to compare against, and the fixture tree is not
	// a repository. Comparing the model with itself is the invocation which
	// exercises the contract without one, and it finds nothing, which is what a
	// walk asserting a successful run needs.
	"review": {"--base-root", "."},
	"route":  {"--kind", "Space", "--type", "MeetingRoom", "site:S-104"},
	"apply":  {"batch.json"},
	// An export takes no arguments at all: what it writes is the whole model,
	// and where it writes it is derived from the digest of that model.
	"export": {},
	"add-node": {
		"--kind", "Space", "--type", "MeetingRoom",
		"--geometry", "area", "--frame", "frame:building",
		"site:S-104",
	},
	"add-vertex": {
		"--frame", "frame:building",
		"--predicate", "position",
		"--value", "8.0 0.0 0.0", "--unit", "m",
		"--source", "Interior control set IC-01, Acme Surveys",
		"--method", "method:total-station",
		"--accuracy", "independent 0.004 m",
		"--date", "2026-02-18",
		"geom:V-05",
	},
	"add-edge": {
		"--frame", "frame:building",
		"--start", "geom:V-04", "--end", "geom:V-01",
		"geom:E-04",
	},
	"add-loop": {
		"--frame", "frame:building",
		"--edge", "geom:E-01", "--edge", "geom:E-02", "--edge", "geom:E-03",
		"geom:L-01",
	},
	"scaffold-loop": {
		"--frame", "frame:building",
		"--namespace", "geom",
		"--predicate", "position",
		"--tolerance", "coincident",
		"--unit", "m",
		"--source", "Interior control set IC-02, Acme Surveys",
		"--method", "method:total-station",
		"--accuracy", "independent 0.004 m",
		"--date", "2026-02-18",
		"--corner", "10.0 0.0 0.0",
		"--corner", "14.0 0.0 0.0",
		"--corner", "14.0 3.0 0.0",
		"--corner", "10.0 3.0 0.0",
		"--corner", "10.0 0.0 0.0",
	},
	// A system nothing in the fixture already carries, so the sample is a change
	// rather than a refusal: the fixture's types are classified in IFC4, and a
	// second scheme beside it is the ordinary case this command exists for.
	"classify-type": {"MeetingRoom", "OmniClass", "13-51 31 00"},
	"set-label":     {"site:S-101", "Meeting Room One"},
	"retire":        {"--reason", "The floor plate it was on was never built.", "site:S-103"},
	"add-claim": {
		"--value", "18.0", "--unit", "m2",
		"--source", "As-built check AB-2026-012, Acme Surveys",
		"--method", "method:total-station",
		"--accuracy", "independent 0.05 m2",
		"--date", "2026-05-06",
		"site:S-102", "area",
	},
	"supersede": {
		"--value", "24.5", "--unit", "m2",
		"--source", "Re-measure RM-2026-002, Acme Surveys",
		"--method", "method:total-station",
		"--accuracy", "independent 0.02 m2",
		"--date", "2026-06-01",
		"site:S-101", "area",
	},
	"deprecate-claim": {"site:M-0001", "--superseded-by", "site:M-0002"},
}

// sample is the invocation of cmd against the fixture model, command name and
// arguments together.
func sample(t *testing.T, cmd command, flags ...string) []string {
	t.Helper()

	arguments, ok := samples[cmd.name]
	require.Truef(t, ok, "add %s to samples so the contract walks reach it", cmd.name)

	return append(append([]string{cmd.name}, flags...), arguments...)
}

// TestStdoutIsTheMachineContract walks every command and asserts, on the
// paths that produce a result and on the paths that do not, that stdout is
// either one JSON object carrying the version and the command, or empty.
//
// It walks [commands] rather than naming them so that a command added later
// is covered by this the day it is added, which is the only way a contract
// like this stays true.
func TestStdoutIsTheMachineContract(t *testing.T) {
	for _, cmd := range commands {
		t.Run(cmd.name+" writes one versioned object naming itself", func(t *testing.T) {
			t.Chdir(tree(t, model()))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run(sample(t, cmd), &stdout, &stderr), stderr.String())

			result := object(t, stdout.String())
			assert.EqualValues(t, outputVersion, result["version"])
			assert.Equal(t, cmd.name, result["command"])
		})

		t.Run(cmd.name+" writes nothing to stdout when asked for help", func(t *testing.T) {
			t.Chdir(t.TempDir())

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run([]string{cmd.name, "-h"}, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.NotEmpty(t, stderr.String())
		})

		t.Run(cmd.name+" writes nothing to stdout for a malformed flag", func(t *testing.T) {
			t.Chdir(t.TempDir())

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitUsage, run([]string{cmd.name, "--no-such-flag"}, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.NotEmpty(t, stderr.String())
		})
	}

	// The invocations that never reach a command at all.
	testCases := []struct {
		name         string
		args         []string
		expectedCode int
	}{
		{
			name:         "writes nothing to stdout when given no arguments",
			args:         nil,
			expectedCode: exitUsage,
		},
		{
			name:         "writes nothing to stdout when asked for the top-level help",
			args:         []string{"--help"},
			expectedCode: exitSuccess,
		},
		{
			name:         "writes nothing to stdout for an unknown command",
			args:         []string{"resolev"},
			expectedCode: exitUsage,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			var stdout, stderr bytes.Buffer

			require.Equal(t, testCase.expectedCode, run(testCase.args, &stdout, &stderr))
			assert.Empty(t, stdout.String())
			assert.NotEmpty(t, stderr.String())
		})
	}
}

// TestStdoutIsOneObjectOnAFailurePath is its own function because it asserts
// about a run that failed and still answered, which is a different shape of
// behaviour from the empty-stdout paths above: a check failure and a file that
// did not parse are results, and a result is always on stdout.
func TestStdoutIsOneObjectOnAFailurePath(t *testing.T) {
	testCases := []struct {
		name         string
		files        map[string]string
		args         []string
		expectedCode int
	}{
		{
			name:         "reports a file which is not in canonical form as a result",
			files:        map[string]string{"a.dfc": asWritten},
			args:         []string{"fmt", "--check"},
			expectedCode: exitCheck,
		},
		{
			name:         "reports a file which did not parse as a result",
			files:        map[string]string{"a.dfc": unparseable},
			args:         []string{"fmt"},
			expectedCode: exitLoad,
		},
		{
			name:         "reports a path which is not there as a result",
			files:        map[string]string{"a.dfc": asPrinted},
			args:         []string{"fmt", "missing.dfc"},
			expectedCode: exitLoad,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, testCase.files))

			var stdout, stderr bytes.Buffer

			require.Equal(t, testCase.expectedCode, run(testCase.args, &stdout, &stderr))

			result := object(t, stdout.String())
			assert.EqualValues(t, outputVersion, result["version"])
			assert.Equal(t, "fmt", result["command"])
		})
	}
}

// TestStdoutIsEmptyWhenTheInputCannotBeLoaded walks every command with a model
// root that is not there and one that is not a directory.
//
// A command that could not read its input has no result, and writing a partial
// or empty result object would say it did. Stdout stays untouched, and the
// exit code is what tells the caller which of the four things happened.
func TestStdoutIsEmptyWhenTheInputCannotBeLoaded(t *testing.T) {
	for _, cmd := range commands {
		t.Run(cmd.name+" writes nothing when the model root is not there", func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "missing")

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitLoad, run([]string{cmd.name, "--root", root}, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), root)
		})

		t.Run(cmd.name+" writes nothing when the model root is not a directory", func(t *testing.T) {
			root := filepath.Join(tree(t, map[string]string{"a.dfc": asPrinted}), "a.dfc")

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitLoad, run([]string{cmd.name, "--root", root}, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), root)
		})

		t.Run(cmd.name+" writes nothing when the model root cannot be read", func(t *testing.T) {
			root := unreadable(t)

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitLoad, run([]string{cmd.name, "--root", root}, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), root)
		})
	}
}

// unreadable is a directory that is there and cannot be read.
//
// A directory can be searchable without being readable, which stat cannot tell
// apart from an ordinary one — so this is the case that says whether the root
// check asks the question it claims to.
func unreadable(t *testing.T) string {
	t.Helper()

	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode, so there is nothing to observe")
	}

	dir := tree(t, map[string]string{"a.dfc": asPrinted})

	// Searchable but not readable: a path beneath it can still be opened by
	// name, and the directory itself cannot be listed.
	require.NoError(t, os.Chmod(dir, 0o100))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	return dir
}

// TestEveryCommandDescribesTheContractAndExitsZero walks every command's help,
// and the top-level help, and requires each to say what the streams are and
// what the exit codes mean.
func TestEveryCommandDescribesTheContractAndExitsZero(t *testing.T) {
	helps := map[string][]string{
		"dfcad": {"--help"},
	}
	for _, cmd := range commands {
		helps["dfcad "+cmd.name] = []string{cmd.name, "--help"}
	}

	for name, args := range helps {
		t.Run(name+" --help describes the contract and exits zero", func(t *testing.T) {
			t.Chdir(t.TempDir())

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitSuccess, run(args, &stdout, &stderr))
			assert.Empty(t, stdout.String())

			help := stderr.String()
			assert.Contains(t, help, outputContractHelp)
			assert.Contains(t, help, globalFlagsHelp)
		})
	}
}

// TestEveryCommandTakesTheGlobalFlags walks every command and requires it to
// accept each global flag and to reject a bad value for it the same way, which
// is what "defined once" has to mean from the outside.
func TestEveryCommandTakesTheGlobalFlags(t *testing.T) {
	for _, cmd := range commands {
		t.Run(cmd.name+" accepts every global flag", func(t *testing.T) {
			dir := tree(t, model())

			var stdout, stderr bytes.Buffer

			args := sample(t, cmd, "--root", dir, "--format", formatHuman, "-v", "-v")
			require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

			assert.NotEmpty(t, object(t, stdout.String()))
		})

		t.Run(cmd.name+" rejects a format that names no format", func(t *testing.T) {
			t.Chdir(t.TempDir())

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitUsage, run([]string{cmd.name, "--format", "yaml"}, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), "yaml")
		})

		t.Run(cmd.name+" rejects a verbosity that names no level", func(t *testing.T) {
			t.Chdir(t.TempDir())

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitUsage, run([]string{cmd.name, "--verbose=loud"}, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), "loud")
		})
	}
}

// TestHumanOutputNeverChangesStdout is its own function because it is about
// the one property the format flag must not have: whichever format was asked
// for, and however loud the run was told to be, stdout is the same bytes.
func TestHumanOutputNeverChangesStdout(t *testing.T) {
	files := map[string]string{"a.dfc": asWritten, "b.dfc": asPrinted}

	quiet := func(t *testing.T, args ...string) (string, string) {
		t.Helper()

		t.Chdir(tree(t, files))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitCheck, run(append([]string{"fmt", "--check"}, args...), &stdout, &stderr))

		return stdout.String(), stderr.String()
	}

	machine, machineReport := quiet(t)
	human, humanReport := quiet(t, "--format", formatHuman)
	loud, loudReport := quiet(t, "-v")
	both, bothReport := quiet(t, "--format", formatHuman, "-v")

	assert.Equal(t, machine, human)
	assert.Equal(t, machine, loud)
	assert.Equal(t, machine, both)

	// The human rendering is behind the flag, is on stderr, and says what the
	// run found. The default says only what is wrong with the input.
	assert.Contains(t, humanReport, "2 files: 1 unchanged, 0 formatted, 1 unformatted, 0 failed")
	assert.NotContains(t, machineReport, "2 files:")

	// Verbosity is progress rather than result, so it says nothing about what
	// was found on its own, and adds the detail of it when the run was also
	// asked to render its result.
	assert.Contains(t, loudReport, "searching")
	assert.NotContains(t, machineReport, "searching")
	assert.NotContains(t, humanReport, "b.dfc: unchanged")
	assert.Contains(t, bothReport, "b.dfc: unchanged")
}

// TestResolvesPathsAgainstTheModelRoot is its own function because it is about
// what --root does to the arguments rather than about what reaches stdout.
func TestResolvesPathsAgainstTheModelRoot(t *testing.T) {
	dir := tree(t, map[string]string{"site/a.dfc": asWritten})

	// The run is started somewhere with nothing in it, so anything it formats
	// it found through the root rather than through the working directory.
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"fmt", "--root", dir, "site"}, &stdout, &stderr))

	assert.Equal(t, []string{"site/a.dfc formatted"}, statuses(decoded(t, dir, stdout.String())))
	assert.Equal(t, map[string]string{"site/a.dfc": asPrinted}, contents(t, dir))
}

// TestRootErrorCarriesItsCause checks that a root which cannot be opened
// reports why in a form a caller can branch on rather than only in a message.
func TestRootErrorCarriesItsCause(t *testing.T) {
	testCases := []struct {
		name            string
		root            func(t *testing.T) string
		expectedCause   error
		expectedMissing bool
	}{
		{
			name: "reports a root which is not there",
			root: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "missing")
			},
			expectedCause: fs.ErrNotExist,
		},
		{
			name: "reports a root which is not a directory",
			root: func(t *testing.T) string {
				return filepath.Join(tree(t, map[string]string{"a.dfc": asPrinted}), "a.dfc")
			},
			expectedCause: ErrNotADirectory,
		},
		{
			name:          "reports a root which cannot be read",
			root:          unreadable,
			expectedCause: fs.ErrPermission,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := testCase.root(t)

			err := (&globals{Root: root}).open()

			var opened RootError
			require.ErrorAs(t, err, &opened)
			assert.Equal(t, root, opened.Path)
			assert.ErrorIs(t, err, testCase.expectedCause)
		})
	}
}

func TestVerbosity(t *testing.T) {
	testCases := []struct {
		name     string
		values   []string
		expected verbosity
	}{
		{
			name:     "starts quiet",
			values:   nil,
			expected: verbosityQuiet,
		},
		{
			name:     "counts a bare -v as one level",
			values:   []string{"true"},
			expected: verbosityProgress,
		},
		{
			name:     "counts a repeated -v",
			values:   []string{"true", "true", "true"},
			expected: 3,
		},
		{
			name:     "takes a level outright",
			values:   []string{"2"},
			expected: 2,
		},
		{
			name:     "goes quiet again when switched off",
			values:   []string{"true", "false"},
			expected: verbosityQuiet,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var level verbosity

			for _, value := range testCase.values {
				require.NoError(t, level.Set(value))
			}

			assert.Equal(t, testCase.expected, level)
		})
	}
}

func TestVerbosityRejectsWhatIsNotALevel(t *testing.T) {
	testCases := []string{"loud", "-1", "1.5", ""}

	for _, value := range testCases {
		t.Run("rejects "+value, func(t *testing.T) {
			var level verbosity

			err := level.Set(value)

			var invalid InvalidVerbosityError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, value, invalid.Value)
		})
	}
}

// TestUnknownFormatNamesWhatItWanted checks that the error carries the formats
// there are, so that a caller does not have to read the message to find them.
func TestUnknownFormatNamesWhatItWanted(t *testing.T) {
	err := (&globals{Format: "yaml"}).validate()

	var unknown UnknownFormatError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, "yaml", unknown.Format)
	assert.Equal(t, formats, unknown.Known)

	for _, format := range formats {
		assert.NoError(t, (&globals{Format: format}).validate())
	}
}

// TestEmitWritesOneObjectAndNothingElse checks the encoder settings the whole
// contract rests on: one value, terminated, with nothing escaped that a
// pipeline would have to undo.
func TestEmitWritesOneObjectAndNothingElse(t *testing.T) {
	var stdout bytes.Buffer

	require.NoError(t, emit(&stdout, fmtResult{
		envelope: newEnvelope("fmt"),
		Files:    []fmtFile{{Path: "a&b<c>.dfc", Status: statusUnchanged}},
	}))

	assert.Equal(t,
		`{"version":2,"command":"fmt","files":[{"path":"a&b<c>.dfc","status":"unchanged"}]}`+"\n",
		stdout.String(),
	)

	assert.NotEmpty(t, object(t, stdout.String()))
}

// TestEmitReportsAStdoutItCannotWrite checks that a stdout which cannot be
// written is an error rather than a silent success — an empty stdout and a
// zero exit code would say the run produced no result rather than that it
// could not deliver one.
func TestEmitReportsAStdoutItCannotWrite(t *testing.T) {
	err := emit(brokenWriter{}, fmtResult{envelope: newEnvelope("fmt")})

	assert.ErrorIs(t, err, errBrokenWriter)
}
