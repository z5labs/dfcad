// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
// answers as though it had loaded: a derivation, a gate or a write exits 2, and
// a discovery read answers with "refused" true in its object.
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

		t.Run(cmd.name+" exits as a load failure", func(t *testing.T) {
			t.Chdir(tree(t, unloadable(t)))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitLoad, run(sample(t, cmd), &stdout, &stderr), stderr.String())

			// Nothing, or — for a gate whose report of a refused model is
			// itself the answer — an object which says the load refused it.
			if stdout.Len() > 0 {
				result := object(t, stdout.String())
				assert.Equal(t, true, result["refused"])
			}
			assert.Contains(t, stderr.String(), "registry.dfc:", "the diagnostic the load reported is rendered for whoever wrote the file")
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
		refused      bool
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
			refused:      true,
		},
		{
			name:         "conflicts answers and says the load refused the model",
			args:         []string{"conflicts"},
			expectedCode: exitSuccess,
			refused:      true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, unloadable(t)))

			var stdout, stderr bytes.Buffer
			require.Equal(t, testCase.expectedCode, run(testCase.args, &stdout, &stderr), stderr.String())

			assert.Contains(t, stderr.String(), "registry.dfc:", "the diagnostic the load reported is rendered for whoever wrote the file")

			if !testCase.refused {
				assert.Empty(t, stdout.String(), "a load failure answers nothing")
				return
			}

			result := object(t, stdout.String())
			assert.Equal(t, true, result["refused"])
		})
	}
}
