// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// pointFixture is the model the measure of a point is asked over: site:PNL-01
// is a node drawn as a point, so nothing in measuring it reads a tolerance.
const pointFixture = "../../testdata/measure/located"

// tolerated is an invocation of every command which takes a tolerance by name,
// over the siting fixture, in the vocabulary it declares: "boundary-closure" is
// the one tolerance its registry holds.
//
// Each is a run which answers exit 0 as written, so a refusal of one of them
// with a name swapped is the name and nothing else.
func tolerated(t *testing.T) map[string][]string {
	t.Helper()

	out := t.TempDir()

	return map[string][]string{
		"measure":    {"measure", "--position", "position", "plan:S-01"},
		"tessellate": {"tessellate", "--position", "position", "plan:S-01"},
		"plan":       {"plan", "--annotate", "position", "--position", "position", "plan:P-01"},
		"site":       {"site", "--within", "plan:P-01", "--position", "position", "plan:S-01"},
		"buildable":  {"buildable", "--setback", "setback", "--position", "position", "plan:P-01"},
		"export":     {"export", "--out", filepath.Join(out, "model.ifc"), "--position", "position"},
		"export-map": {"export-map", "--out", filepath.Join(out, "model.gml"), "--position", "position"},
	}
}

// withTolerances is the invocation with its tolerance and chord tolerance named.
func withTolerances(args []string, tolerance, chord string) []string {
	return append(append([]string{}, args...), "--tolerance", tolerance, "--chord", chord)
}

// TestTolerancesTheRegistryDoesNotDeclareAreUsageErrors walks every command
// which takes a tolerance by name, with each of its two tolerance flags naming
// one the registry does not declare.
func TestTolerancesTheRegistryDoesNotDeclareAreUsageErrors(t *testing.T) {
	testCases := []struct {
		name      string
		tolerance string
		chord     string
	}{
		{
			name:      "refuses a --tolerance the registry does not declare",
			tolerance: "no-such",
			chord:     "boundary-closure",
		},
		{
			name:      "refuses a --chord the registry does not declare",
			tolerance: "boundary-closure",
			chord:     "no-such",
		},
	}

	for command, args := range tolerated(t) {
		for _, testCase := range testCases {
			t.Run(command+" "+testCase.name, func(t *testing.T) {
				stdout, stderr := invoke(t, exitUsage, surveyedFixture, withTolerances(args, testCase.tolerance, testCase.chord)...)

				expected := dfcad.UnknownAxisError{
					Axis:      "tolerance",
					Value:     "no-such",
					Permitted: []string{"boundary-closure"},
				}

				assert.Empty(t, stdout)
				assert.Equal(t, "dfcad "+command+": "+expected.Error()+"\n", stderr, "one error, and nothing said about the model")
			})
		}
	}
}

// TestDeclaredTolerancesAnswerAsTheyDid runs the same invocations with every
// name declared, so that the refusal above is the name and not the command.
func TestDeclaredTolerancesAnswerAsTheyDid(t *testing.T) {
	for command, args := range tolerated(t) {
		t.Run(command+" answers a tolerance the registry declares", func(t *testing.T) {
			stdout, _ := invoke(t, exitSuccess, surveyedFixture, withTolerances(args, "boundary-closure", "boundary-closure")...)

			result := object(t, stdout)
			assert.Equal(t, command, result["command"])
		})
	}
}

// TestUndeclaredTolerancesNothingWouldHaveReadAreRefused covers the runs which
// answered an undeclared name with exit 0, because nothing in them read it: the
// name is refused whether or not anything would have.
func TestUndeclaredTolerancesNothingWouldHaveReadAreRefused(t *testing.T) {
	testCases := []struct {
		name    string
		root    string
		args    []string
		command string
	}{
		{
			name:    "refuses the measure of a point",
			root:    pointFixture,
			args:    []string{"measure", "--position", "position", "--tolerance", "no-such", "site:PNL-01"},
			command: "measure",
		},
		{
			name:    "refuses a --chord over a region with no curves",
			root:    surveyedFixture,
			args:    []string{"measure", "--position", "position", "--tolerance", "boundary-closure", "--chord", "no-such", "plan:S-01"},
			command: "measure",
		},
		{
			name:    "refuses the plan of a place containing nothing",
			root:    surveyedFixture,
			args:    []string{"plan", "--annotate", "position", "--position", "position", "--tolerance", "no-such", "plan:S-01"},
			command: "plan",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stdout, stderr := invoke(t, exitUsage, testCase.root, testCase.args...)

			assert.Empty(t, stdout)
			assert.Contains(t, stderr, "dfcad "+testCase.command+": ")
			assert.Equal(t, 1, bytes.Count([]byte(stderr), []byte("\n")), "one error, and no diagnostic")
		})
	}
}

// surveyedWith is the siting fixture with each of extra appended to the file of
// the same name, or written beside it where there is none.
func surveyedWith(t *testing.T, extra map[string]string) string {
	t.Helper()

	files := make(map[string]string)
	for _, name := range []string{"model.dfc", "registry.dfc"} {
		src, err := os.ReadFile(filepath.Join(surveyedFixture, name))
		require.NoError(t, err)
		files[name] = string(src)
	}

	for name, content := range extra {
		files[name] += "\n" + content
	}

	return tree(t, files)
}

// TestDeclaredTolerancesInTheWrongUnitAreReportedWhereTheyAreRead is its own
// function because what it asserts is the other side of the refusal: a name
// the registry declares is a question about the model, answered at each node.
func TestDeclaredTolerancesInTheWrongUnitAreReportedWhereTheyAreRead(t *testing.T) {
	root := surveyedWith(t, map[string]string{
		"registry.dfc": `(tolerance feet-closure
  (value 0.02 ft)
  (description "A closure tolerance in a unit no frame of this model is in."))`,
	})

	stdout, stderr := invoke(t, exitCheck, root, "measure", "--position", "position", "--tolerance", "feet-closure", "plan:S-01")

	result := listed[measureResult](t, stdout)
	assert.False(t, result.Derived)
	assert.Contains(t, stderr, ": error: ")
}

// TestUndeclaredTolerancesOverAModelTheLoadRefused is its own function because
// the precedence it asserts is between two refusals: a tree which does not load
// is exit 2 whatever the invocation names, as #250 established.
func TestUndeclaredTolerancesOverAModelTheLoadRefused(t *testing.T) {
	root := surveyedWith(t, map[string]string{
		"broken.dfc": "(node plan:X-01",
	})

	for command, args := range tolerated(t) {
		t.Run(command+" exits as a load failure", func(t *testing.T) {
			invoke(t, exitLoad, root, withTolerances(args, "no-such", "no-such")...)
		})
	}
}

// TestDeclaredTolerances asserts the refusal on the error value, so that what a
// caller branches on is its type and its fields.
func TestDeclaredTolerances(t *testing.T) {
	graph, _ := dfcad.LoadGraph(surveyedFixture)
	registry := graph.Registry()

	testCases := []struct {
		name     string
		names    []string
		expected string
	}{
		{
			name:     "reports a tolerance the registry does not declare",
			names:    []string{"no-such"},
			expected: "no-such",
		},
		{
			name:     "reports the first of several it does not declare",
			names:    []string{"boundary-closure", "first", "second"},
			expected: "first",
		},
		{
			name:     "reports an undeclared chord beside a declared tolerance",
			names:    []string{"boundary-closure", "no-such"},
			expected: "no-such",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := declaredTolerances(registry, testCase.names...)

			var got dfcad.UnknownAxisError
			require.True(t, errors.As(err, &got), "expected UnknownAxisError, got %T", err)
			assert.Equal(t, "tolerance", got.Axis)
			assert.Equal(t, testCase.expected, got.Value)
			assert.Equal(t, registry.Names(dfcad.SortTolerance), got.Permitted)
			assert.Equal(t, []string{"boundary-closure"}, got.Permitted)
		})
	}

	t.Run("accepts every name the registry declares", func(t *testing.T) {
		assert.NoError(t, declaredTolerances(registry, "boundary-closure", "boundary-closure"))
	})

	t.Run("does not check a name which was not given", func(t *testing.T) {
		assert.NoError(t, declaredTolerances(registry, "", ""))
	})
}
