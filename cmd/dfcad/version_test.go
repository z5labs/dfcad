// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// TestVersionReportsAnUnstampedBuild is the case a plain `go build` produces,
// and the case this test binary is: nothing stamped main.version or main.commit,
// so the command reports the placeholders and says they are placeholders.
//
// It is the case worth pinning because it is the one which can be run. A test
// cannot relink itself, so the stamped values are asserted by the CI job which
// runs the binary the standard pipeline built; what is asserted here is that the
// command works at all without a stamp, rather than reporting a blank version or
// failing on a binary nobody stamped.
func TestVersionReportsAnUnstampedBuild(t *testing.T) {
	t.Run("reports the placeholders and says they are placeholders", func(t *testing.T) {
		t.Chdir(t.TempDir())

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run([]string{"version"}, &stdout, &stderr), stderr.String())

		result := object(t, stdout.String())

		build, ok := result["build"].(map[string]any)
		require.True(t, ok, "build is not an object")

		assert.Equal(t, unstampedVersion, build["version"])
		assert.Equal(t, unstampedCommit, build["commit"])
		assert.Equal(t, false, build["stamped"])
	})

	t.Run("reports the version of each contract it implements", func(t *testing.T) {
		t.Chdir(t.TempDir())

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run([]string{"version"}, &stdout, &stderr), stderr.String())

		result := object(t, stdout.String())

		contracts, ok := result["contracts"].(map[string]any)
		require.True(t, ok, "contracts is not an object")

		assert.EqualValues(t, outputVersion, contracts["output"])
		assert.Equal(t, dfcad.SpecVersion, contracts["entity-format"])
	})

	t.Run("writes the contract version in the envelope as well as in the payload", func(t *testing.T) {
		t.Chdir(t.TempDir())

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run([]string{"version"}, &stdout, &stderr), stderr.String())

		result := object(t, stdout.String())

		// The envelope's number and the payload's are the same contract seen
		// twice: one is the version this object was written against, the other
		// is the version this build implements, and for an object this build
		// wrote they cannot differ. A caller reading either gets the same
		// answer.
		contracts := result["contracts"].(map[string]any)
		assert.Equal(t, result["version"], contracts["output"])
	})
}

// TestVersionIsStampedWhenTheLinkerSaysSo drives the reporting rather than the
// link line, which is the only part of stamping a test can reach: the values are
// package variables, so what can be checked here is what the command does with
// them once they hold something.
func TestVersionIsStampedWhenTheLinkerSaysSo(t *testing.T) {
	testCases := []struct {
		name            string
		version         string
		commit          string
		expectedStamped bool
	}{
		{
			name:            "reports a stamped build when both values came from the link line",
			version:         "v1.2.3",
			commit:          "abc1234",
			expectedStamped: true,
		},
		{
			name:            "reports an unstamped build when neither did",
			version:         unstampedVersion,
			commit:          unstampedCommit,
			expectedStamped: false,
		},
		{
			name:            "reports an unstamped build when only the version did",
			version:         "v1.2.3",
			commit:          unstampedCommit,
			expectedStamped: false,
		},
		{
			name:            "reports an unstamped build when only the commit did",
			version:         unstampedVersion,
			commit:          "abc1234",
			expectedStamped: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			restore(t, testCase.version, testCase.commit)

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run([]string{"version"}, &stdout, &stderr), stderr.String())

			result := object(t, stdout.String())
			build := result["build"].(map[string]any)

			assert.Equal(t, testCase.version, build["version"])
			assert.Equal(t, testCase.commit, build["commit"])
			assert.Equal(t, testCase.expectedStamped, build["stamped"])
		})
	}
}

// TestVersionRendersForAPerson is its own function because it asserts about
// stderr, which the contract walks require to leave stdout alone rather than to
// say anything in particular.
func TestVersionRendersForAPerson(t *testing.T) {
	t.Run("says the build is unstamped in the human rendering", func(t *testing.T) {
		t.Chdir(t.TempDir())

		var stdout, stderr bytes.Buffer
		args := []string{"version", "--format", formatHuman}
		require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

		report := stderr.String()
		assert.Contains(t, report, unstampedVersion)
		assert.Contains(t, report, unstampedCommit)
		assert.Contains(t, report, "unstamped")
		assert.Contains(t, report, dfcad.SpecVersion)
	})

	t.Run("does not call a stamped build unstamped", func(t *testing.T) {
		t.Chdir(t.TempDir())

		restore(t, "v1.2.3", "abc1234")

		var stdout, stderr bytes.Buffer
		args := []string{"version", "--format", formatHuman}
		require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

		report := stderr.String()
		assert.Contains(t, report, "v1.2.3")
		assert.Contains(t, report, "abc1234")
		assert.NotContains(t, report, "unstamped")
	})

	t.Run("says nothing about the build on stderr by default", func(t *testing.T) {
		t.Chdir(t.TempDir())

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run([]string{"version"}, &stdout, &stderr))

		assert.Empty(t, stderr.String())
	})
}

// TestVersionTakesNoArguments is its own function because it is about a failure
// path, which asserts an exit code and an empty stdout rather than the shape of
// a result.
func TestVersionTakesNoArguments(t *testing.T) {
	t.Run("rejects an argument rather than ignoring it", func(t *testing.T) {
		t.Chdir(t.TempDir())

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitUsage, run([]string{"version", "site:S-101"}, &stdout, &stderr))

		assert.Empty(t, stdout.String())
		assert.Contains(t, stderr.String(), "site:S-101")
	})
}

// restore sets the build stamp for the duration of one test and puts back what
// was there afterwards.
//
// The values are package variables because the linker writes them, which leaves
// a test no way to set them other than by assignment. Restoring is what keeps
// that from leaking into the tests which assert the unstamped case; the tests
// which use it do not run in parallel, for the same reason.
func restore(t *testing.T, setVersion, setCommit string) {
	t.Helper()

	wasVersion, wasCommit := version, commit
	t.Cleanup(func() { version, commit = wasVersion, wasCommit })

	version, commit = setVersion, setCommit
}
