// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// refusedObject requires that stdout is the object a run writes where a load
// refused what it read and the command has no answer to give through that —
// the envelope, "refused" true and the diagnostics, and no other key — and
// returns the diagnostics it carries.
//
// It asserts the keys rather than the fields it knows of, because what the
// object leaves out is the point: a subject, a digest, a file or a dry run
// would each describe an answer the run did not give.
func refusedObject(t *testing.T, stdout, command string) []dfcad.Diagnostic {
	t.Helper()

	result := object(t, stdout)

	keys := objectKeys(t, json.RawMessage(stdout))
	expected := []string{"version", "command", "refused", "diagnostics"}
	if _, held := result["diagnostics-suppressed"]; held {
		expected = []string{"version", "command", "refused", "diagnostics-suppressed", "diagnostics"}
	}
	assert.Equal(t, expected, keys, "a refused run writes the envelope, refused and its diagnostics, and nothing else")

	assert.Equal(t, float64(outputVersion), result["version"])
	assert.Equal(t, command, result["command"])
	assert.Equal(t, true, result["refused"])

	diagnostics, _ := carried(t, stdout)
	assert.True(t, hasError(diagnostics), "a load is refused by an error, and the object carries it")

	return diagnostics
}

// hasError reports whether any of the diagnostics is an error.
func hasError(diagnostics []dfcad.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == dfcad.SeverityError {
			return true
		}
	}
	return false
}

// answersThrough is every command which answers through a load the model refused,
// reporting that it did in its answer object rather than in its exit code.
//
// It is the set docs/machine-output.md names under "Diagnostics and the exit
// code of a read". Every other command which reads the model exits 2 on such a
// tree, as `check` does.
var answersThrough = map[string]bool{
	"list-types":      true,
	"list-predicates": true,
	"list-tolerances": true,
	"list-frames":     true,
	"list-instances":  true,
	"list-geometry":   true,
	"get":             true,
	"traverse":        true,
	"claims":          true,
	"conflicts":       true,
}

// readsNoModel is every command which does not load the model beneath the
// root, so a load it refuses says nothing about it.
var readsNoModel = map[string]bool{
	"version": true,
	"fmt":     true,
}

// unloadable is the fixture model with a registry which declares frames in a
// namespace nothing declares.
//
// That is a load error — `dfcad check` refuses the tree for it — but not one
// which stops anything else in the model being read: the frames are still
// there, so a derivation over them still computes a figure. Which is exactly
// what makes it the case worth testing. A tree nothing could be read out of
// exits 2 from every command already.
func unloadable(t *testing.T) map[string]string {
	t.Helper()

	files := model()

	const declared = `(namespace frame (description "Coordinate frames declared by this model."))` + "\n"
	require.Contains(t, files["registry.dfc"], declared, "the fixture registry no longer declares the frame namespace this removes")
	files["registry.dfc"] = strings.Replace(files["registry.dfc"], declared, "", 1)

	return files
}

// TestEveryCommandWhichReadsTheModelSaysTheLoadRefusedIt walks every command
// over a tree whose load reports an error, and asserts that none of them
// answers as though it had loaded: a discovery read answers with "refused" true
// in its object, `check` exits 2 with its whole report, and every other command
// exits 2 writing the envelope, "refused" true and the diagnostics which
// refused the model — and nothing else.
//
// It walks [commands] rather than naming them so that a command added later has
// to be placed on one side of that line the day it is added.
func TestEveryCommandWhichReadsTheModelSaysTheLoadRefusedIt(t *testing.T) {
	for _, cmd := range commands {
		if readsNoModel[cmd.name] {
			continue
		}

		if answersThrough[cmd.name] {
			t.Run(cmd.name+" answers and says the load refused the model", func(t *testing.T) {
				t.Chdir(tree(t, unloadable(t)))

				var stdout, stderr bytes.Buffer
				require.Equal(t, exitSuccess, run(sample(t, cmd), &stdout, &stderr), stderr.String())

				result := object(t, stdout.String())
				assert.Equal(t, true, result["refused"])
				assert.Contains(t, stderr.String(), "registry.dfc:", "the diagnostic the load reported is rendered for whoever wrote the file")
			})

			continue
		}

		if cmd.name == "check" {
			t.Run(cmd.name+" exits as a load failure and reports the refusal as its answer", func(t *testing.T) {
				t.Chdir(tree(t, unloadable(t)))

				var stdout, stderr bytes.Buffer
				require.Equal(t, exitLoad, run(sample(t, cmd), &stdout, &stderr), stderr.String())

				result := object(t, stdout.String())
				assert.Equal(t, true, result["refused"])
				assert.Contains(t, result, "summary", "check writes its whole object over a refused model")
			})

			continue
		}

		t.Run(cmd.name+" exits as a load failure and says why on stdout", func(t *testing.T) {
			t.Chdir(tree(t, unloadable(t)))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitLoad, run(sample(t, cmd), &stdout, &stderr), stderr.String())

			diagnostics := refusedObject(t, stdout.String(), cmd.name)
			assert.True(t, spannedIn(diagnostics, "registry.dfc"), "the load's error names the registry it is about")
			assert.Contains(t, stderr.String(), "registry.dfc:", "the diagnostic the load reported is rendered for whoever wrote the file")

			assertRoundTrips(t, stdout.String(), stderr.String())
		})
	}
}

// TestEveryDiscoveryReadSaysTheModelLoaded is its own function because it is
// the other half of the field: "refused" is written on every run, false over a
// model which loads, so a caller can read it without first asking whether it
// is there.
func TestEveryDiscoveryReadSaysTheModelLoaded(t *testing.T) {
	for _, cmd := range commands {
		if !answersThrough[cmd.name] {
			continue
		}

		t.Run(cmd.name+" says the load did not refuse the model", func(t *testing.T) {
			t.Chdir(tree(t, model()))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run(sample(t, cmd), &stdout, &stderr), stderr.String())

			result := object(t, stdout.String())
			assert.Equal(t, false, result["refused"])
		})
	}
}

// TestAQueryOverATreeTheLoadRefuses is the reproduction the story was filed
// with: a registry declaring a frame in a namespace nothing declares, which
// `check` refused and measure, resolve and conflicts answered over anyway, each
// exiting 0.
func TestAQueryOverATreeTheLoadRefuses(t *testing.T) {
	testCases := []struct {
		name         string
		args         []string
		expectedCode int
		answers      bool
	}{
		{
			name: "measure exits as a load failure rather than measuring",
			args: []string{
				"measure",
				"--position", "position",
				"--tolerance", "coincident",
				"site:S-103",
			},
			expectedCode: exitLoad,
		},
		{
			name:         "resolve exits as a load failure rather than resolving",
			args:         []string{"resolve", "site:S-101", "area"},
			expectedCode: exitLoad,
		},
		{
			name:         "check exits as a load failure, which is what the others now agree with",
			args:         []string{"check"},
			expectedCode: exitLoad,
			answers:      true,
		},
		{
			name:         "conflicts answers and says the load refused the model",
			args:         []string{"conflicts"},
			expectedCode: exitSuccess,
			answers:      true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, unloadable(t)))

			var stdout, stderr bytes.Buffer
			require.Equal(t, testCase.expectedCode, run(testCase.args, &stdout, &stderr), stderr.String())

			assert.Contains(t, stderr.String(), "registry.dfc:", "the diagnostic the load reported is rendered for whoever wrote the file")

			if !testCase.answers {
				refusedObject(t, stdout.String(), testCase.args[0])
				return
			}

			result := object(t, stdout.String())
			assert.Equal(t, true, result["refused"])
		})
	}
}

// TestARunWithNoDiagnosticToGiveStillWritesNothing holds the other side of the
// refusal: a run which exits 2 for an error rather than for a diagnostic has no
// refusal to write, and stdout stays empty. What those runs have to say is on
// stderr as a `dfcad <cmd>:` line, which is not a diagnostic and is not in any
// object.
//
// The annotation case is the one that could be mistaken for a refusal: the run
// renders its findings as errors, and then fails to write the file it was told
// to. Its exit 2 is the write, not the findings.
func TestARunWithNoDiagnosticToGiveStillWritesNothing(t *testing.T) {
	testCases := []struct {
		name          string
		args          func(t *testing.T) []string
		rendersErrors bool
	}{
		{
			name: "a model root held by another transaction",
			args: func(t *testing.T) []string {
				root := tree(t, authored())

				tx, diags, err := dfcad.Begin(root)
				require.NoError(t, err)
				require.Empty(t, diags)
				t.Cleanup(func() { _ = tx.Close() })

				return []string{"set-label", "--root", root, "site:S-101", "Board Room"}
			},
		},
		{
			name: "an operation file which could not be read at all",
			args: func(t *testing.T) []string {
				root := tree(t, authored())
				return []string{"apply", "--root", root, "missing.json"}
			},
		},
		{
			name: "a review whose summary could not be written, over findings rendered as errors",
			args: func(t *testing.T) []string {
				base := tree(t, model())
				head := tree(t, map[string]string{
					"registry.dfc":          listRegistry,
					"entities/site.dfc":     withoutTheCampus(t),
					"entities/geometry.dfc": listGeometry,
					"entities/parcels.dfc":  listParcels,
				})
				unwritable := filepath.Join(t.TempDir(), "missing", "step-summary.md")

				return []string{"review", "--root", head, "--base-root", base, "--annotate", unwritable}
			},
			rendersErrors: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			require.Equal(t, exitLoad, run(testCase.args(t), &stdout, &stderr), stderr.String())

			assert.Empty(t, stdout.String(), "an error is not a refusal, and a run which has only an error writes no object")
			assert.NotEmpty(t, stderr.String())

			if testCase.rendersErrors {
				assert.Contains(t, stderr.String(), ": error: ", "the run rendered a diagnostic which is an error, and still refused nothing")
			}
		})
	}
}
