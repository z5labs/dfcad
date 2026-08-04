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
		expectedStderr string
	}{
		{
			name:           "prints usage to stderr and fails when given no arguments",
			args:           nil,
			expectedCode:   exitUsage,
			expectedStderr: usage(),
		},
		{
			name:           "prints usage to stderr and succeeds when asked for help",
			args:           []string{"--help"},
			expectedCode:   exitSuccess,
			expectedStderr: usage(),
		},
		{
			name:           "prints usage to stderr and succeeds for the short help flag",
			args:           []string{"-h"},
			expectedCode:   exitSuccess,
			expectedStderr: usage(),
		},
		{
			name:           "prints usage to stderr and succeeds for the help subcommand",
			args:           []string{"help"},
			expectedCode:   exitSuccess,
			expectedStderr: usage(),
		},
		{
			name:           "names the unknown command on stderr and fails",
			args:           []string{"resolev"},
			expectedCode:   exitUsage,
			expectedStderr: "dfcad: unknown command \"resolev\"\n\n" + usage(),
		},
		{
			name:           "takes the command from the first argument, not a later one",
			args:           []string{"resolev", "help"},
			expectedCode:   exitUsage,
			expectedStderr: "dfcad: unknown command \"resolev\"\n\n" + usage(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(testCase.args, &stdout, &stderr)

			require.Equal(t, testCase.expectedCode, code)

			// None of these produced a result, so none of them wrote anything
			// to the stream a caller pipes.
			assert.Empty(t, stdout.String())
			assert.Equal(t, testCase.expectedStderr, stderr.String())
		})
	}
}

// TestUsageListsEveryCommand is its own function because it is about the help
// staying in step with the command table rather than about one invocation.
func TestUsageListsEveryCommand(t *testing.T) {
	help := usage()

	for _, cmd := range commands {
		assert.Contains(t, help, cmd.name)
		assert.Contains(t, help, cmd.summary)
	}
}
