// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two spellings of one model the tests below format between.
const (
	asWritten = "(node site:S-102 (type Corridor) (kind Space))\n"
	asPrinted = "(node site:S-102 (kind Space) (type Corridor))\n"
)

// unparseable is a file with a form nothing closes.
const unparseable = "(node site:S-103\n"

// tree writes files into a new directory and returns its path.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))

		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	return dir
}

// contents is every file in the tree beneath dir, by its path relative to dir.
func contents(t *testing.T, dir string) map[string]string {
	t.Helper()

	out := make(map[string]string)
	require.NoError(t, filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		out[filepath.ToSlash(rel)] = string(src)
		return nil
	}))

	return out
}

// decoded is the one JSON object the command wrote to stdout, with every file
// named relative to dir.
//
// Decoding is where the contract is checked rather than where it is worked
// around: stdout has to hold exactly one object and nothing after it, which is
// what lets a caller pipe it into jq without filtering anything out first.
func decoded(t *testing.T, dir string, stdout string) fmtResult {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(stdout))

	var result fmtResult
	require.NoError(t, decoder.Decode(&result))

	_, err := decoder.Token()
	require.ErrorIs(t, err, io.EOF, "stdout holds more than one JSON value")

	for i, file := range result.Files {
		rel, err := filepath.Rel(dir, file.Path)
		require.NoError(t, err)

		result.Files[i].Path = filepath.ToSlash(rel)
	}

	return result
}

// statuses is each file the result reports, as "path status".
func statuses(result fmtResult) []string {
	out := make([]string, 0, len(result.Files))
	for _, file := range result.Files {
		out = append(out, file.Path+" "+file.Status)
	}
	return out
}

func TestRunFmt(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		files            map[string]string
		expectedCode     int
		expectedStatuses []string
		expectedContents map[string]string
	}{
		{
			name:             "rewrites a file which is not in canonical form",
			args:             []string{"fmt"},
			files:            map[string]string{"a.dfc": asWritten},
			expectedCode:     exitSuccess,
			expectedStatuses: []string{"a.dfc formatted"},
			expectedContents: map[string]string{"a.dfc": asPrinted},
		},
		{
			name:             "succeeds and writes nothing when every file is canonical",
			args:             []string{"fmt"},
			files:            map[string]string{"a.dfc": asPrinted},
			expectedCode:     exitSuccess,
			expectedStatuses: []string{"a.dfc unchanged"},
			expectedContents: map[string]string{"a.dfc": asPrinted},
		},
		{
			name:             "fails the check and writes nothing when a file would change",
			args:             []string{"fmt", "--check"},
			files:            map[string]string{"a.dfc": asWritten},
			expectedCode:     exitCheck,
			expectedStatuses: []string{"a.dfc unformatted"},
			expectedContents: map[string]string{"a.dfc": asWritten},
		},
		{
			name:             "passes the check when every file is canonical",
			args:             []string{"fmt", "--check"},
			files:            map[string]string{"a.dfc": asPrinted},
			expectedCode:     exitSuccess,
			expectedStatuses: []string{"a.dfc unchanged"},
			expectedContents: map[string]string{"a.dfc": asPrinted},
		},
		{
			name:             "writes nothing when it is asked for a diff",
			args:             []string{"fmt", "--diff"},
			files:            map[string]string{"a.dfc": asWritten},
			expectedCode:     exitCheck,
			expectedStatuses: []string{"a.dfc unformatted"},
			expectedContents: map[string]string{"a.dfc": asWritten},
		},
		{
			name:             "reports a file which could not be parsed as a load failure",
			args:             []string{"fmt"},
			files:            map[string]string{"a.dfc": unparseable},
			expectedCode:     exitLoad,
			expectedStatuses: []string{"a.dfc failed"},
			expectedContents: map[string]string{"a.dfc": unparseable},
		},
		{
			name:             "formats the files after one which could not be parsed",
			args:             []string{"fmt"},
			files:            map[string]string{"a.dfc": unparseable, "b.dfc": asWritten},
			expectedCode:     exitLoad,
			expectedStatuses: []string{"a.dfc failed", "b.dfc formatted"},
			expectedContents: map[string]string{"a.dfc": unparseable, "b.dfc": asPrinted},
		},
		{
			name:             "reports a file which could not be parsed over one which would change",
			args:             []string{"fmt", "--check"},
			files:            map[string]string{"a.dfc": unparseable, "b.dfc": asWritten},
			expectedCode:     exitLoad,
			expectedStatuses: []string{"a.dfc failed", "b.dfc unformatted"},
			expectedContents: map[string]string{"a.dfc": unparseable, "b.dfc": asWritten},
		},
		{
			name:             "reports an empty tree as no files at all",
			args:             []string{"fmt"},
			files:            map[string]string{"notes.md": asWritten},
			expectedCode:     exitSuccess,
			expectedStatuses: []string{},
			expectedContents: map[string]string{"notes.md": asWritten},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := tree(t, testCase.files)

			// The command defaults to the tree beneath the current directory,
			// which is what the arguments above leave it to do.
			t.Chdir(dir)

			var stdout, stderr bytes.Buffer

			code := run(testCase.args, &stdout, &stderr)

			require.Equal(t, testCase.expectedCode, code)

			result := decoded(t, ".", stdout.String())
			assert.Equal(t, outputVersion, result.Version)
			assert.Equal(t, "fmt", result.Command)
			assert.Equal(t, testCase.expectedStatuses, statuses(result))

			assert.Equal(t, testCase.expectedContents, contents(t, dir))
		})
	}
}

// TestRunFmtPaths is its own function because it varies what is named on the
// way in rather than what happens to one tree.
func TestRunFmtPaths(t *testing.T) {
	files := map[string]string{
		"a.dfc":      asWritten,
		"notes.md":   asWritten,
		"site/b.dfc": asWritten,
		"site/c.dfc": asWritten,
	}

	testCases := []struct {
		name             string
		paths            []string
		expectedStatuses []string
	}{
		{
			name:             "formats the current tree when no path is given",
			paths:            nil,
			expectedStatuses: []string{"a.dfc formatted", "site/b.dfc formatted", "site/c.dfc formatted"},
		},
		{
			name:             "formats a single file",
			paths:            []string{"a.dfc"},
			expectedStatuses: []string{"a.dfc formatted"},
		},
		{
			name:             "formats a single directory",
			paths:            []string{"site"},
			expectedStatuses: []string{"site/b.dfc formatted", "site/c.dfc formatted"},
		},
		{
			name:             "formats several files and directories together",
			paths:            []string{"a.dfc", "site"},
			expectedStatuses: []string{"a.dfc formatted", "site/b.dfc formatted", "site/c.dfc formatted"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := tree(t, files)
			t.Chdir(dir)

			var stdout, stderr bytes.Buffer

			code := run(append([]string{"fmt"}, testCase.paths...), &stdout, &stderr)

			require.Equal(t, exitSuccess, code)
			assert.Equal(t, testCase.expectedStatuses, statuses(decoded(t, ".", stdout.String())))
		})
	}
}

// TestRunFmtReportsToStderr is its own function because it is about which
// stream each of the two renderings goes to, which is the part of the output
// contract a caller piping stdout depends on.
func TestRunFmtReportsToStderr(t *testing.T) {
	dir := tree(t, map[string]string{"a.dfc": asWritten, "b.dfc": unparseable})
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer

	code := run([]string{"fmt", "--diff"}, &stdout, &stderr)

	require.Equal(t, exitLoad, code)

	// The diagnostic and the diff are for a person, and neither may reach the
	// stream a caller pipes.
	report := stderr.String()
	assert.Contains(t, report, "a.dfc: not in canonical form")
	assert.Contains(t, report, "--- a.dfc.orig")
	assert.Contains(t, report, "+(node site:S-102 (kind Space) (type Corridor))")
	assert.Contains(t, report, "b.dfc:1:7: error:")

	// The machine-readable form of the diagnostic is on stdout, and is not
	// derived by parsing what was written to stderr.
	result := decoded(t, ".", stdout.String())
	require.Len(t, result.Files, 2)

	assert.Equal(t, statusUnformatted, result.Files[0].Status)
	assert.Empty(t, result.Files[0].Diagnostics)

	require.Len(t, result.Files[1].Diagnostics, 1)
	assert.Equal(t, statusFailed, result.Files[1].Status)
	assert.Equal(t, 1, result.Files[1].Diagnostics[0].Span.Start.Line)
	assert.Equal(t, 7, result.Files[1].Diagnostics[0].Span.Start.Column)
}

func TestRunFmtReportsAFileItCannotRead(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer

	code := run([]string{"fmt", "missing.dfc"}, &stdout, &stderr)

	require.Equal(t, exitLoad, code)

	result := decoded(t, ".", stdout.String())
	require.Len(t, result.Files, 1)
	assert.Equal(t, statusFailed, result.Files[0].Status)
	assert.NotEmpty(t, result.Files[0].Error)
	assert.Empty(t, result.Files[0].Diagnostics)

	assert.Contains(t, stderr.String(), "missing.dfc")
}

func TestRunFmtIsIdempotent(t *testing.T) {
	dir := tree(t, map[string]string{"a.dfc": asWritten})
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"fmt"}, &stdout, &stderr))

	first := contents(t, dir)

	stdout.Reset()
	stderr.Reset()
	require.Equal(t, exitSuccess, run([]string{"fmt"}, &stdout, &stderr))

	assert.Equal(t, []string{"a.dfc unchanged"}, statuses(decoded(t, ".", stdout.String())))
	assert.Equal(t, first, contents(t, dir))

	// A second run is also a passing check, which is what makes fmt --check in
	// CI a gate rather than a coin toss.
	stdout.Reset()
	stderr.Reset()
	assert.Equal(t, exitSuccess, run([]string{"fmt", "--check"}, &stdout, &stderr))
}

func TestRunFmtUsage(t *testing.T) {
	testCases := []struct {
		name           string
		args           []string
		expectedCode   int
		expectedStderr string
	}{
		{
			name:           "prints the fmt usage to stderr and succeeds when asked for help",
			args:           []string{"fmt", "-h"},
			expectedCode:   exitSuccess,
			expectedStderr: fmtUsage,
		},
		{
			name:           "names the unknown flag on stderr and reports a usage error",
			args:           []string{"fmt", "--rewrite"},
			expectedCode:   exitUsage,
			expectedStderr: "dfcad fmt: flag provided but not defined: -rewrite\n\n" + fmtUsage,
		},
		{
			name:           "names the unknown format on stderr and reports a usage error",
			args:           []string{"fmt", "--format", "yaml"},
			expectedCode:   exitUsage,
			expectedStderr: "dfcad fmt: " + UnknownFormatError{Format: "yaml", Known: formats}.Error() + "\n\n" + fmtUsage,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			var stdout, stderr bytes.Buffer

			code := run(testCase.args, &stdout, &stderr)

			require.Equal(t, testCase.expectedCode, code)

			// Help and a wrong invocation both produce no result, so neither
			// writes anything to the stream a caller pipes.
			assert.Empty(t, stdout.String())
			assert.Equal(t, testCase.expectedStderr, stderr.String())
		})
	}
}

// TestRunFmtOutputIsDeterministic checks that two runs over the same tree
// write byte-identical results, which is what makes diffing two runs mean
// something.
func TestRunFmtOutputIsDeterministic(t *testing.T) {
	files := map[string]string{
		"a.dfc":      asWritten,
		"site/b.dfc": unparseable,
		"site/c.dfc": asPrinted,
	}

	var results []string
	for range 2 {
		dir := tree(t, files)
		t.Chdir(dir)

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitLoad, run([]string{"fmt", "--check"}, &stdout, &stderr))

		// The temporary directory differs between the two runs and is the one
		// thing in the output that legitimately does.
		results = append(results, strings.ReplaceAll(stdout.String(), dir, "<dir>"))
	}

	assert.Equal(t, results[0], results[1])
}

// TestFmtErrorsAreNotSwallowed checks that a stdout which cannot be written
// reports a failure rather than an unexplained success.
func TestFmtErrorsAreNotSwallowed(t *testing.T) {
	dir := tree(t, map[string]string{"a.dfc": asPrinted})
	t.Chdir(dir)

	var stderr bytes.Buffer

	code := run([]string{"fmt"}, brokenWriter{}, &stderr)

	assert.Equal(t, exitLoad, code)
	assert.Contains(t, stderr.String(), "dfcad fmt:")
}

// errBrokenWriter is what a [brokenWriter] fails with.
var errBrokenWriter = errors.New("broken")

// brokenWriter is a stdout that cannot be written to.
type brokenWriter struct{}

// Write implements [io.Writer].
func (brokenWriter) Write([]byte) (int, error) {
	return 0, errBrokenWriter
}
