// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"flag"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declaredFlags is the flag set cmd builds, as it stood before it parsed
// anything, found by asking the command for its help.
//
// It is taken from the command rather than listed here so that a flag a
// command declares later is walked the day it is declared.
func declaredFlags(t *testing.T, cmd command) *flag.FlagSet {
	t.Helper()

	var declared *flag.FlagSet
	inspectFlags = func(_ command, flags *flag.FlagSet) { declared = flags }
	defer func() { inspectFlags = nil }()

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{cmd.name, "--help"}, &stdout, &stderr))
	require.NotNil(t, declared, "%s never reached parse", cmd.name)

	return declared
}

// refused runs args, requires the run to be a usage error with nothing on
// stdout, and returns the error parsing args against the command's own flag
// set gives, which is what the run reported on stderr.
func refused(t *testing.T, args []string) error {
	t.Helper()
	t.Chdir(t.TempDir())

	cmd, ok := lookup(args[0])
	require.True(t, ok, "no command %s", args[0])
	declared := declaredFlags(t, cmd)

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitUsage, run(args, &stdout, &stderr), stderr.String())
	assert.Empty(t, stdout.String())

	_, err := parseFlags(declared, args[1:])
	return err
}

// repeatsByDesign reports whether a flag's value is one of the two which give
// a second occurrence a meaning: the value every filter, --annotate, --corner
// and the other list-building flags are held in, and the -v counter.
func repeatsByDesign(value flag.Value) bool {
	switch value.(type) {
	case *repeated, *verbosity:
		return true
	default:
		return false
	}
}

// isBool reports whether a flag stands alone, without a value after it.
func isBool(value flag.Value) bool {
	boolean, ok := value.(interface{ IsBoolFlag() bool })
	return ok && boolean.IsBoolFlag()
}

// TestEveryCommandRefusesASingleValuedFlagWrittenTwice walks every command
// and every flag it declares which takes one value, and requires writing that
// flag twice to be a usage error naming it and both values — whether or not
// the two values agree.
func TestEveryCommandRefusesASingleValuedFlagWrittenTwice(t *testing.T) {
	for _, cmd := range commands {
		declared := declaredFlags(t, cmd)

		var walked []string
		declared.VisitAll(func(f *flag.Flag) {
			if repeatsByDesign(f.Value) {
				return
			}
			walked = append(walked, f.Name)

			testCases := []struct {
				name     string
				args     []string
				expected []string
			}{
				{
					name:     "refuses two values",
					args:     []string{"--" + f.Name, "1", "--" + f.Name, "2"},
					expected: []string{"1", "2"},
				},
				{
					name:     "refuses one value written twice",
					args:     []string{"--" + f.Name, "1", "--" + f.Name, "1"},
					expected: []string{"1", "1"},
				},
			}
			if isBool(f.Value) {
				testCases = []struct {
					name     string
					args     []string
					expected []string
				}{
					{
						name:     "refuses two values",
						args:     []string{"--" + f.Name, "--" + f.Name + "=false"},
						expected: []string{"true", "false"},
					},
					{
						name:     "refuses one value written twice",
						args:     []string{"--" + f.Name, "--" + f.Name},
						expected: []string{"true", "true"},
					},
				}
			}

			for _, testCase := range testCases {
				t.Run(cmd.name+" --"+f.Name+" "+testCase.name, func(t *testing.T) {
					err := refused(t, append([]string{cmd.name}, testCase.args...))

					var got RepeatedFlagError
					require.ErrorAs(t, err, &got)
					assert.Equal(t, f.Name, got.Flag)
					assert.Equal(t, testCase.expected, got.Values)
				})
			}
		})

		// The globals are on every command, so a walk which did not reach
		// them walked nothing.
		assert.Subset(t, walked, []string{"root", "format", "entity-format"}, cmd.name)
	}
}

// TestRepeatedFlagsAreEachRefused covers the refusals a walk of one flag at a
// time does not reach: the reproduction that turned the rule up, and a run
// that repeats more than one flag.
func TestRepeatedFlagsAreEachRefused(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected []RepeatedFlagError
	}{
		{
			name: "refuses a coordinate asked for in two frames",
			args: []string{
				"resolve", "geom:V-11", "position",
				"--frame", "frame:annex", "--frame", "frame:site",
			},
			expected: []RepeatedFlagError{
				{Flag: "frame", Values: []string{"frame:annex", "frame:site"}},
			},
		},
		{
			name: "names every value however many were written",
			args: []string{"traverse", "contains", "site:B-01", "--depth", "1", "--depth", "2", "--depth", "all"},
			expected: []RepeatedFlagError{
				{Flag: flagDepth, Values: []string{"1", "2", depthAll}},
			},
		},
		{
			name: "refuses a repeat it would have refused as a value",
			args: []string{"traverse", "contains", "site:B-01", "--depth", "1", "--depth", "0"},
			expected: []RepeatedFlagError{
				{Flag: flagDepth, Values: []string{"1", "0"}},
			},
		},
		{
			name: "names every flag which was repeated, in the order of their names",
			args: []string{
				"get", "site:S-101",
				"--root", "a", "--claims", "full", "--root", "b", "--claims", "none",
			},
			expected: []RepeatedFlagError{
				{Flag: "claims", Values: []string{"full", "none"}},
				{Flag: "root", Values: []string{"a", "b"}},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := refused(t, testCase.args)
			require.Error(t, err)

			joined, ok := err.(interface{ Unwrap() []error })
			require.True(t, ok, "want every repeat, got %T", err)

			var got []RepeatedFlagError
			for _, each := range joined.Unwrap() {
				var repeat RepeatedFlagError
				require.ErrorAs(t, each, &repeat)
				got = append(got, repeat)
			}
			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestFlagsWhichRepeatByDesignStillAnswer runs the flags a second occurrence
// means something for, written more than once, and requires each run to
// answer.
func TestFlagsWhichRepeatByDesignStillAnswer(t *testing.T) {
	plan, ok := lookup("plan")
	require.True(t, ok)
	get, ok := lookup("get")
	require.True(t, ok)

	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "annotates a plan with every predicate written",
			args: sample(t, plan, "--"+flagAnnotate, "setback"),
		},
		{
			name: "counts -v written twice",
			args: sample(t, get, "-v", "-v"),
		},
		{
			name: "counts --verbose written beside -v",
			args: sample(t, get, "-v", "--verbose"),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := tree(t, model())

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run(append(testCase.args, "--root", dir), &stdout, &stderr), stderr.String())

			assert.NotEmpty(t, object(t, stdout.String()))
		})
	}
}

// TestAFilterWrittenTwiceIsSatisfiedByEither runs the reproduction a filter
// used to drop a value on, and requires both values to be answered.
func TestAFilterWrittenTwiceIsSatisfiedByEither(t *testing.T) {
	var stdout, stderr bytes.Buffer

	args := []string{
		"traverse", queryContains, "site:B-01", "--depth", depthAll,
		"--" + flagType, "Office", "--" + flagType, "MeetingRoom",
		"--root", budgetRoot,
	}
	require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

	results, ok := object(t, stdout.String())["results"].([]any)
	require.True(t, ok, "results is not an array")

	types := map[string]bool{}
	for _, result := range results {
		entry, ok := result.(map[string]any)
		require.True(t, ok)
		declared, _ := entry["type"].(string)
		types[declared] = true
	}
	assert.Equal(t, map[string]bool{"Office": true, "MeetingRoom": true}, types)
}
