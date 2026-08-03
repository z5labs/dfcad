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
		name           string
		args           []string
		expectedCode   int
		expectedStdout string
		expectedStderr string
	}{
		{
			name:           "prints usage to stderr and fails when given no arguments",
			args:           nil,
			expectedCode:   exitUsage,
			expectedStderr: usage,
		},
		{
			name:           "prints usage to stdout and succeeds when asked for help",
			args:           []string{"--help"},
			expectedCode:   exitSuccess,
			expectedStdout: usage,
		},
		{
			name:           "prints usage to stdout and succeeds for the short help flag",
			args:           []string{"-h"},
			expectedCode:   exitSuccess,
			expectedStdout: usage,
		},
		{
			name:           "prints usage to stdout and succeeds for the help subcommand",
			args:           []string{"help"},
			expectedCode:   exitSuccess,
			expectedStdout: usage,
		},
		{
			name:           "names the unknown command on stderr and fails",
			args:           []string{"resolev"},
			expectedCode:   exitUsage,
			expectedStderr: "dfcad: unknown command \"resolev\"\n\n" + usage,
		},
		{
			name:           "takes the command from the first argument, not a later one",
			args:           []string{"resolev", "help"},
			expectedCode:   exitUsage,
			expectedStderr: "dfcad: unknown command \"resolev\"\n\n" + usage,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(testCase.args, &stdout, &stderr)

			require.Equal(t, testCase.expectedCode, code)
			assert.Equal(t, testCase.expectedStdout, stdout.String())
			assert.Equal(t, testCase.expectedStderr, stderr.String())
		})
	}
}
