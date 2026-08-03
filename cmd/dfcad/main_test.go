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
)

func TestRun(t *testing.T) {
	testCases := []struct {
		name         string
		args         []string
		expectedCode int
		stdoutUsage  bool
		stderrUsage  bool
	}{
		{
			name:         "prints usage to stderr and fails when given no arguments",
			args:         nil,
			expectedCode: exitUsage,
			stderrUsage:  true,
		},
		{
			name:         "prints usage to stdout and succeeds when asked for help",
			args:         []string{"--help"},
			expectedCode: exitSuccess,
			stdoutUsage:  true,
		},
		{
			name:         "prints usage to stdout and succeeds for the short help flag",
			args:         []string{"-h"},
			expectedCode: exitSuccess,
			stdoutUsage:  true,
		},
		{
			name:         "prints usage to stdout and succeeds for the help subcommand",
			args:         []string{"help"},
			expectedCode: exitSuccess,
			stdoutUsage:  true,
		},
		{
			name:         "reports an unknown command on stderr and fails",
			args:         []string{"nope"},
			expectedCode: exitUsage,
			stderrUsage:  true,
		},
		{
			name:         "ignores trailing arguments when deciding the command",
			args:         []string{"nope", "--flag", "value"},
			expectedCode: exitUsage,
			stderrUsage:  true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(testCase.args, &stdout, &stderr)

			require.Equal(t, testCase.expectedCode, code)

			if testCase.stdoutUsage {
				assert.Contains(t, stdout.String(), usage)
			} else {
				assert.Empty(t, stdout.String())
			}

			if testCase.stderrUsage {
				assert.Contains(t, stderr.String(), usage)
			} else {
				assert.Empty(t, stderr.String())
			}
		})
	}
}

func TestRunNamesTheUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"resolev"}, &stdout, &stderr)

	require.Equal(t, exitUsage, code)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "resolev")
}
