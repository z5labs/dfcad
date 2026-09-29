// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// intoSetback is the batch discussion #304's consumer proves a check can fail
// with: geom:V-03, the north-east corner of the fixture's level, moved from
// six metres east to nine, which draws Room B two metres into the setback strip
// that begins at eight.
const intoSetback = `{"version": 1, "operations": [
  {"op": "supersede", "subject": "geom:V-03", "predicate": "position",
   "claim": {"value": "9.0 4.0 0.0", "unit": "m", "source": "Interior control set IC-01, Acme Surveys",
             "method": "method:total-station", "accuracy": ["independent 0.004 m"], "date": "2026-02-18"}}
]}
`

// blockBBack is a batch over the siting fixture which sets Block B's
// south-west corner out ten metres further west than it was surveyed, so the
// footprint it bounds is a different shape and sits somewhere else on the plot.
const blockBBack = `{"version": 1, "operations": [
  {"op": "supersede", "subject": "geom:V-21", "predicate": "position",
   "claim": {"value": "10.0 0.0 0.0", "unit": "m", "source": "Interior control set IC-2026-03, Acme Surveys",
             "method": "method:total-station",
             "accuracy": ["independent 0.003 m", "systematic 0.008 m control:CP-1"],
             "date": "2026-04-08"}}
]}
`

// relabelled is a batch over the command fixture which every read can answer
// over: it changes a byte of the tree, so the digest of the tree it would
// produce is not the digest of the one on disk, and nothing any sample asks
// about stops being there.
const relabelled = `{"version": 1, "operations": [
  {"op": "set-label", "id": "site:S-101", "label": "Meeting Room One"}
]}
`

// satisfied and surveyed are the checked-in fixtures the round trip runs over.
const (
	satisfiedFixture = "../../testdata/checks/satisfied"
	surveyedFixture  = "../../testdata/siting/surveyed"
)

// assumedMember matches the "assumed" member of an object, which is flat, so
// that an answer under --assume can be compared byte for byte with the one the
// same read gives once the batch is applied.
var assumedMember = regexp.MustCompile(`,"assumed":\{[^{}]*\}`)

// copied is a copy of the fixture tree at dir, in a directory of its own.
func copied(t *testing.T, dir string) string {
	t.Helper()

	root := filepath.Join(t.TempDir(), "model")
	require.NoError(t, os.CopyFS(root, os.DirFS(dir)))

	return root
}

// built is every build output beneath root's .dfcad directory, by its path
// relative to that directory, and empty where the run built nothing.
func built(t *testing.T, root string) map[string]string {
	t.Helper()

	dir := filepath.Join(root, ".dfcad")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return map[string]string{}
	}

	return contents(t, dir)
}

// batchFile writes a batch outside every model root and returns its path, so
// that nothing about the batch is part of the tree a digest is taken over.
func batchFile(t *testing.T, written string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "batch.json")
	require.NoError(t, os.WriteFile(path, []byte(written), 0o600))

	return path
}

// runFrom runs one invocation from inside root, with the model root left at its
// default, and returns its exit code and what it wrote to each stream.
//
// The root is the working directory rather than a flag so that every path the
// run reports — a diagnostic's span, an artefact it wrote — is the same
// whichever copy of a fixture it ran over, which is what lets two runs over two
// copies be compared byte for byte.
func runFrom(t *testing.T, root string, stdin io.Reader, args ...string) (int, string, string) {
	t.Helper()

	t.Chdir(root)

	var stdout, stderr bytes.Buffer
	code := runOn(args, stdin, &stdout, &stderr)

	return code, stdout.String(), stderr.String()
}

// assumedOf is the "assumed" member of an answer, which is required to be
// there.
func assumedOf(t *testing.T, stdout string) assumedBatch {
	t.Helper()

	var result struct {
		Assumed *assumedBatch `json:"assumed"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result), stdout)
	require.NotNil(t, result.Assumed, "an answer under --assume says it is hypothetical")

	return *result.Assumed
}

// TestEveryReadTakesAssume walks every subcommand and requires the ones which
// read the model to answer over the model a batch would produce, and every
// other one to refuse the flag rather than ignore it.
//
// It walks [commands] rather than naming them, for the reason the dry-run walk
// does: a read added later takes the flag because it is a read.
func TestEveryReadTakesAssume(t *testing.T) {
	for _, cmd := range commands {
		if !cmd.reads {
			t.Run(cmd.name+" does not take --assume, because it is not a read", func(t *testing.T) {
				t.Chdir(t.TempDir())

				var stdout, stderr bytes.Buffer
				require.Equal(t, exitUsage, run([]string{cmd.name, "--assume", "batch.json"}, &stdout, &stderr))

				assert.Empty(t, stdout.String())
				assert.False(t, strings.Contains(cmd.usage, readFlagsHelp), "and its help does not offer it")
			})

			continue
		}

		t.Run(cmd.name+" answers over the model the batch would produce", func(t *testing.T) {
			root := tree(t, model())
			path := batchFile(t, relabelled)
			before := contents(t, root)

			base, err := dfcad.DigestOf(root)
			require.NoError(t, err)

			stdout, _ := invoke(t, exitSuccess, root, sample(t, cmd, "--assume", path)...)

			prefix := `{"version":2,"command":"` + cmd.name + `","assumed":{`
			assert.True(t, strings.HasPrefix(stdout, prefix), "assumed is written after the envelope: %s", stdout)

			assumed := assumedOf(t, stdout)
			assert.Equal(t, path, assumed.Batch)
			assert.Equal(t, 1, assumed.Operations)
			assert.Equal(t, base.String(), assumed.Base)
			assert.NotEqual(t, assumed.Base, assumed.Digest, "the batch changed a byte, so the tree it would produce is another tree")

			if digest, held := object(t, stdout)["digest"]; held {
				assert.Equal(t, assumed.Digest, digest, "a payload's digest is the tree the batch would produce")
			}

			authored := contents(t, root)
			for name := range authored {
				if strings.HasPrefix(name, ".dfcad/") {
					delete(authored, name)
				}
			}
			assert.Equal(t, before, authored, "nothing authored is written")

			assert.True(t, strings.Contains(cmd.usage, readFlagsHelp), "and its help says so")
		})

		t.Run(cmd.name+" says nothing about a batch it was not given", func(t *testing.T) {
			root := tree(t, model())

			stdout, _ := invoke(t, exitSuccess, root, sample(t, cmd)...)

			assert.NotContains(t, object(t, stdout), "assumed")
		})
	}
}

// TestAReadUnderAssumeAnswersAsApplyThenRead is the defining property of the
// flag: over every batch apply accepts, a read given it writes what the same
// read writes once apply has written the batch, apart from "assumed", and exits
// with the same code.
func TestAReadUnderAssumeAnswersAsApplyThenRead(t *testing.T) {
	testCases := []struct {
		name     string
		fixture  string
		batch    string
		args     []string
		artefact bool
	}{
		{
			name:    "check fails where the batch draws a room into the setback",
			fixture: satisfiedFixture,
			batch:   intoSetback,
			args:    []string{"check"},
		},
		{
			name:    "resolve answers the position the batch states",
			fixture: satisfiedFixture,
			batch:   intoSetback,
			args:    []string{"resolve", "geom:V-03", "position"},
		},
		{
			name:    "measure measures the room the batch reshaped",
			fixture: satisfiedFixture,
			batch:   intoSetback,
			args:    []string{"measure", "--position", "position", "--tolerance", "boundary-closure", "site:S-102"},
		},
		{
			name:    "site sites the room the batch reshaped",
			fixture: satisfiedFixture,
			batch:   intoSetback,
			args:    []string{"site", "--within", "site:L-01", "--position", "position", "--tolerance", "boundary-closure", "site:S-102"},
		},
		{
			name:     "export writes the model the batch would produce",
			fixture:  satisfiedFixture,
			batch:    intoSetback,
			args:     []string{"export"},
			artefact: true,
		},
		{
			name:    "check answers over the siting fixture with a footprint moved",
			fixture: surveyedFixture,
			batch:   blockBBack,
			args:    []string{"check"},
		},
		{
			name:    "resolve answers the corner the batch moved",
			fixture: surveyedFixture,
			batch:   blockBBack,
			args:    []string{"resolve", "geom:V-21", "position"},
		},
		{
			name:    "measure measures the footprint the batch reshaped",
			fixture: surveyedFixture,
			batch:   blockBBack,
			args:    []string{"measure", "--position", "position", "--tolerance", "boundary-closure", "plan:S-02"},
		},
		{
			name:    "site sites the footprint the batch reshaped",
			fixture: surveyedFixture,
			batch:   blockBBack,
			args:    []string{"site", "--within", "plan:P-01", "--position", "position", "--tolerance", "boundary-closure", "plan:S-02"},
		},
		{
			name:     "export writes the siting model the batch would produce",
			fixture:  surveyedFixture,
			batch:    blockBBack,
			args:     []string{"export"},
			artefact: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			path := batchFile(t, testCase.batch)

			// Both copied before either run, because a run moves the working
			// directory the fixture's path is relative to.
			hypothetical := copied(t, testCase.fixture)
			actual := copied(t, testCase.fixture)

			assumedCode, assumedOut, assumedErr := runFrom(t, hypothetical, nil,
				append(append([]string{}, testCase.args...), "--assume", path)...)

			code, _, applyErr := runFrom(t, actual, nil, "apply", path)
			require.Equal(t, exitSuccess, code, applyErr)
			appliedCode, appliedOut, appliedErr := runFrom(t, actual, nil, testCase.args...)

			require.NotEmpty(t, appliedOut, appliedErr)
			assert.Equal(t, appliedCode, assumedCode, assumedErr)
			assert.Equal(t, appliedOut, assumedMember.ReplaceAllString(assumedOut, ""))

			digest, err := dfcad.DigestOf(actual)
			require.NoError(t, err)
			assert.Equal(t, digest.String(), assumedOf(t, assumedOut).Digest,
				"the digest is the one the tree has once the batch is written")

			// An artefact is keyed by that digest, so the hypothetical one is the
			// one the applied tree would be served from, byte for byte.
			assert.Equal(t, built(t, actual), built(t, hypothetical))
			if testCase.artefact {
				assert.NotEmpty(t, built(t, hypothetical), "the artefact is written beneath the digest")
			}
		})
	}
}

// TestAssumeAnswersTheReproduction is discussion #304's reproduction, run
// without copying the fixture: the check fails, over a model nobody wrote, and
// the fixture is exactly as it was.
func TestAssumeAnswersTheReproduction(t *testing.T) {
	root := copied(t, satisfiedFixture)
	path := batchFile(t, intoSetback)
	before := contents(t, root)

	base, err := dfcad.DigestOf(root)
	require.NoError(t, err)

	// A transaction holds the root throughout, which a read under the flag
	// neither takes nor waits for.
	require.NoError(t, os.WriteFile(filepath.Join(root, ".dfcad.lock"), nil, 0o600))
	before[".dfcad.lock"] = ""

	stdout, _ := invoke(t, exitCheck, root, "check", "--assume", path)

	var result struct {
		Assumed    assumedBatch `json:"assumed"`
		Violations []struct {
			Check    string `json:"check"`
			Instance string `json:"instance"`
		} `json:"violations"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))

	require.Len(t, result.Violations, 1)
	assert.Equal(t, "stays-clear-of-zone", result.Violations[0].Check)
	assert.Equal(t, "site:S-102", result.Violations[0].Instance)

	assert.Equal(t, 1, result.Assumed.Operations)
	assert.Equal(t, base.String(), result.Assumed.Base, "base is the tree that was read")

	assert.Equal(t, before, contents(t, root), "the tree is byte-identical afterwards")
}

// TestAssumeReadsTheBatchFromStandardInput checks that `-` names standard
// input, as it does for apply, and that the answer says so.
func TestAssumeReadsTheBatchFromStandardInput(t *testing.T) {
	root := copied(t, satisfiedFixture)

	named, _ := invoke(t, exitCheck, root, "check", "--assume", batchFile(t, intoSetback))
	piped, _ := piped(t, exitCheck, root, intoSetback, "check", "--assume", "-")

	assert.Equal(t, stdinPath, assumedOf(t, piped).Batch)
	assert.Equal(t, assumedMember.ReplaceAllString(named, ""), assumedMember.ReplaceAllString(piped, ""))
}

// TestAssumeIsRefusedAsApplyRefusesIt checks that a batch is refused under
// --assume exactly as `apply --dry-run` refuses it — the same exit code, the
// same stderr and the same stdout, under the command that was run — and that
// the read does not run: not even a discovery read or check, which answer
// through a refused tree without the flag.
func TestAssumeIsRefusedAsApplyRefusesIt(t *testing.T) {
	testCases := []struct {
		name         string
		files        func(t *testing.T) map[string]string
		batch        string
		expectedCode int
		refusal      bool
	}{
		{
			name:         "a file which is not a batch",
			files:        func(*testing.T) map[string]string { return model() },
			batch:        `{"version": 1, "operations": [{"op": "no-such-operation"}]}`,
			expectedCode: exitLoad,
		},
		{
			name:         "a file which is not JSON",
			files:        func(*testing.T) map[string]string { return model() },
			batch:        `(node site:S-104)`,
			expectedCode: exitLoad,
		},
		{
			name:         "an operation the model refuses",
			files:        func(*testing.T) map[string]string { return model() },
			batch:        `{"version": 1, "operations": [{"op": "set-label", "id": "site:S-999", "label": "Nowhere"}]}`,
			expectedCode: exitUsage,
		},
		{
			name:         "a result which would not load",
			files:        func(*testing.T) map[string]string { return model() },
			batch:        `{"version": 1, "operations": [{"op": "relate", "id": "site:S-103", "within": "site:S-909"}]}`,
			expectedCode: exitLoad,
			refusal:      true,
		},
		{
			name:         "a base tree which does not load",
			files:        unloadable,
			batch:        relabelled,
			expectedCode: exitLoad,
			refusal:      true,
		},
	}

	reads := []string{"check", "list-instances", "measure"}

	for _, testCase := range testCases {
		for _, read := range reads {
			t.Run(read+" refuses "+testCase.name, func(t *testing.T) {
				root := tree(t, testCase.files(t))
				path := batchFile(t, testCase.batch)
				before := contents(t, root)

				dryRunOut, dryRunErr := invoke(t, testCase.expectedCode, root, "apply", "--dry-run", path)

				cmd, ok := lookup(read)
				require.True(t, ok)
				stdout, stderr := invoke(t, testCase.expectedCode, root, sample(t, cmd, "--assume", path)...)

				if testCase.refusal {
					refusedObject(t, stdout, read)
				} else {
					assert.Empty(t, stdout, "a batch refused before a load writes nothing")
				}

				assert.Equal(t,
					strings.Replace(dryRunOut, `"command":"apply"`, `"command":"`+read+`"`, 1),
					stdout,
					"stdout is what apply --dry-run writes, under the command that was run",
				)
				assert.Equal(t,
					strings.ReplaceAll(dryRunErr, "dfcad apply:", "dfcad "+read+":"),
					stderr,
					"and stderr is what it writes, in its words",
				)

				assert.Equal(t, before, contents(t, root), "a refused batch writes nothing")
			})
		}
	}

	t.Run("a file which cannot be read", func(t *testing.T) {
		root := tree(t, model())

		stdout, stderr := invoke(t, exitLoad, root, "check", "--assume", "absent.json")

		assert.Empty(t, stdout)
		assert.Contains(t, stderr, "absent.json")
	})
}

// TestAssumeIsAUsageErrorWhereItCannotMeanAnything checks the invocations of
// the flag which are wrong whatever the model holds.
func TestAssumeIsAUsageErrorWhereItCannotMeanAnything(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "a flag which names no file",
			args: []string{"check", "--assume="},
		},
		{
			name: "a flag written twice",
			args: []string{"check", "--assume", "a.json", "--assume", "b.json"},
		},
		{
			name: "the ids and the batch both on standard input",
			args: []string{"get", "--assume", "-", "-"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, model())

			stdout, stderr := piped(t, exitUsage, root, "site:S-101\n", testCase.args...)

			assert.Empty(t, stdout)
			assert.NotEmpty(t, stderr)
		})
	}
}
