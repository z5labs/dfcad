// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two spellings of one model the tests below format between: what somebody
// wrote, with the children in the order they thought of them, and the one
// canonical printing of the same thing.
const (
	asWritten = "(node site:S-102 (type Corridor) (kind Space))\n"
	asPrinted = "(node site:S-102 (kind Space) (type Corridor))\n"
)

// unparseable is a file with a form nothing closes, which is what a file
// somebody is part way through editing looks like.
const unparseable = "(node site:S-103\n"

// outcomes is what formatting did, one line per file, named relative to dir so
// that the expectation does not hold a temporary directory nobody can predict.
func outcomes(t *testing.T, dir string, formatted []Formatted) []string {
	t.Helper()

	out := make([]string, 0, len(formatted))
	for _, file := range formatted {
		rel, err := filepath.Rel(dir, file.Path)
		require.NoError(t, err)

		var state string
		switch {
		case file.Failed():
			state = "failed"
		case file.Written:
			state = "rewritten"
		case file.Changed:
			state = "changed"
		default:
			state = "unchanged"
		}

		out = append(out, filepath.ToSlash(rel)+" "+state)
	}

	return out
}

// contents is every file in the tree beneath dir, by its path relative to dir.
//
// It holds every file rather than the ones the test asked about, so that a
// temporary file left behind by a replacement fails the test which expected
// the replacement to be the only thing that happened.
func contents(t *testing.T, dir string) map[string]string {
	t.Helper()

	out := make(map[string]string)
	require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
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

func TestFormat(t *testing.T) {
	testCases := []struct {
		name             string
		formatter        Formatter
		files            map[string]string
		expectedOutcomes []string
		expectedContents map[string]string
	}{
		{
			name:             "rewrites a file which is not in canonical form",
			formatter:        Formatter{Rewrite: true},
			files:            map[string]string{"a.dfc": asWritten},
			expectedOutcomes: []string{"a.dfc rewritten"},
			expectedContents: map[string]string{"a.dfc": asPrinted},
		},
		{
			name:             "leaves a file which is already in canonical form alone",
			formatter:        Formatter{Rewrite: true},
			files:            map[string]string{"a.dfc": asPrinted},
			expectedOutcomes: []string{"a.dfc unchanged"},
			expectedContents: map[string]string{"a.dfc": asPrinted},
		},
		{
			name:             "reports what would change and writes nothing when it is only checking",
			formatter:        Formatter{},
			files:            map[string]string{"a.dfc": asWritten},
			expectedOutcomes: []string{"a.dfc changed"},
			expectedContents: map[string]string{"a.dfc": asWritten},
		},
		{
			name:             "writes nothing when it is asked for a diff",
			formatter:        Formatter{Diff: true},
			files:            map[string]string{"a.dfc": asWritten},
			expectedOutcomes: []string{"a.dfc changed"},
			expectedContents: map[string]string{"a.dfc": asWritten},
		},
		{
			name:             "adds the line feed a file which ends without one is missing",
			formatter:        Formatter{Rewrite: true},
			files:            map[string]string{"a.dfc": strings.TrimSuffix(asPrinted, "\n")},
			expectedOutcomes: []string{"a.dfc rewritten"},
			expectedContents: map[string]string{"a.dfc": asPrinted},
		},
		{
			name:             "leaves a file which does not parse exactly as it was",
			formatter:        Formatter{Rewrite: true},
			files:            map[string]string{"a.dfc": unparseable},
			expectedOutcomes: []string{"a.dfc failed"},
			expectedContents: map[string]string{"a.dfc": unparseable},
		},
		{
			name:      "formats the files after one which does not parse",
			formatter: Formatter{Rewrite: true},
			files: map[string]string{
				"a.dfc": asWritten,
				"b.dfc": unparseable,
				"c.dfc": asWritten,
			},
			expectedOutcomes: []string{"a.dfc rewritten", "b.dfc failed", "c.dfc rewritten"},
			expectedContents: map[string]string{
				"a.dfc": asPrinted,
				"b.dfc": unparseable,
				"c.dfc": asPrinted,
			},
		},
		{
			name:      "walks into subdirectories and ignores what is not an entity file",
			formatter: Formatter{Rewrite: true},
			files: map[string]string{
				"a.dfc":         asWritten,
				"notes.md":      asWritten,
				"site/b.dfc":    asWritten,
				"site/deep.DFC": asWritten,
			},
			expectedOutcomes: []string{"a.dfc rewritten", "site/b.dfc rewritten"},
			expectedContents: map[string]string{
				"a.dfc":         asPrinted,
				"notes.md":      asWritten,
				"site/b.dfc":    asPrinted,
				"site/deep.DFC": asWritten,
			},
		},
		{
			name:             "reports a directory holding no entity file as nothing at all",
			formatter:        Formatter{Rewrite: true},
			files:            map[string]string{"notes.md": asWritten},
			expectedOutcomes: []string{},
			expectedContents: map[string]string{"notes.md": asWritten},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := tree(t, testCase.files)

			formatted := testCase.formatter.Format(dir)

			assert.Equal(t, testCase.expectedOutcomes, outcomes(t, dir, formatted))
			assert.Equal(t, testCase.expectedContents, contents(t, dir))
		})
	}
}

// TestFormatPaths is its own function because it varies what is named on the
// way in rather than what happens to one tree, so what it asserts on is which
// files were reached and not what was put in them.
func TestFormatPaths(t *testing.T) {
	files := map[string]string{
		"a.dfc":      asWritten,
		"notes.md":   asWritten,
		"site/b.dfc": asWritten,
		"site/c.dfc": asWritten,
	}

	testCases := []struct {
		name             string
		paths            []string
		expectedOutcomes []string
	}{
		{
			name:             "formats a single file",
			paths:            []string{"a.dfc"},
			expectedOutcomes: []string{"a.dfc rewritten"},
		},
		{
			name:             "formats a file named explicitly whatever its extension",
			paths:            []string{"notes.md"},
			expectedOutcomes: []string{"notes.md rewritten"},
		},
		{
			name:             "formats a directory",
			paths:            []string{"site"},
			expectedOutcomes: []string{"site/b.dfc rewritten", "site/c.dfc rewritten"},
		},
		{
			name:             "formats several files",
			paths:            []string{"a.dfc", "site/c.dfc"},
			expectedOutcomes: []string{"a.dfc rewritten", "site/c.dfc rewritten"},
		},
		{
			name:             "formats several directories and files together",
			paths:            []string{"site", "a.dfc"},
			expectedOutcomes: []string{"site/b.dfc rewritten", "site/c.dfc rewritten", "a.dfc rewritten"},
		},
		{
			name:             "formats a file named twice only once",
			paths:            []string{"a.dfc", "./a.dfc"},
			expectedOutcomes: []string{"a.dfc rewritten"},
		},
		{
			name:             "formats a file named as well as walked into only once",
			paths:            []string{"site", "site/b.dfc"},
			expectedOutcomes: []string{"site/b.dfc rewritten", "site/c.dfc rewritten"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := tree(t, files)

			paths := make([]string, 0, len(testCase.paths))
			for _, path := range testCase.paths {
				paths = append(paths, filepath.Join(dir, filepath.FromSlash(path)))
			}

			formatted := Formatter{Rewrite: true}.Format(paths...)

			assert.Equal(t, testCase.expectedOutcomes, outcomes(t, dir, formatted))
		})
	}
}

func TestFormatDiagnostics(t *testing.T) {
	testCases := []struct {
		name            string
		source          string
		expectedLine    int
		expectedColumn  int
		expectedMessage string
	}{
		{
			name:            "reports where a form was left unclosed",
			source:          unparseable,
			expectedLine:    1,
			expectedColumn:  7,
			expectedMessage: "unexpected end of tokens at line 1, column 7, expected one of: RParen",
		},
		{
			name:            "reports a file which begins with a byte order mark",
			source:          "\ufeff" + asPrinted,
			expectedLine:    1,
			expectedColumn:  1,
			expectedMessage: "file begins with a UTF-8 byte order mark, which must be removed",
		},
		{
			name:            "reports the first byte which is not valid UTF-8",
			source:          "(node site:S-102 (label \"\xff\"))\n",
			expectedLine:    1,
			expectedColumn:  26,
			expectedMessage: "invalid UTF-8: byte 0xff begins no valid encoding",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := tree(t, map[string]string{"a.dfc": testCase.source})
			path := filepath.Join(dir, "a.dfc")

			formatted := Formatter{Rewrite: true}.Format(path)

			require.Len(t, formatted, 1)
			require.True(t, formatted[0].Failed())
			require.Len(t, formatted[0].Diagnostics, 1)

			diagnostic := formatted[0].Diagnostics[0]
			assert.Equal(t, SeverityError, diagnostic.Severity)
			assert.Equal(t, path, diagnostic.Span.Start.Path)
			assert.Equal(t, testCase.expectedLine, diagnostic.Span.Start.Line)
			assert.Equal(t, testCase.expectedColumn, diagnostic.Span.Start.Column)
			assert.Equal(t, testCase.expectedMessage, diagnostic.Message)

			// Nothing is written to a file that could not be read back.
			assert.Equal(t, testCase.source, contents(t, dir)["a.dfc"])
		})
	}
}

// TestFormatIsIdempotent checks the property the whole of formatting rests on:
// canonical form is a fixed point, so a second pass over what a first one
// wrote changes nothing.
//
// It runs over every fixture the printer is tested against rather than over a
// handful of literals, because a file the second pass would change is a file
// the tool and somebody's editor would fight over, and the fixtures are where
// the awkward cases already are.
func TestFormatIsIdempotent(t *testing.T) {
	for _, source := range corpus(t) {
		t.Run(source, func(t *testing.T) {
			src, err := os.ReadFile(source)
			require.NoError(t, err)

			dir := tree(t, map[string]string{"a.dfc": string(src)})
			path := filepath.Join(dir, "a.dfc")

			first := Formatter{Rewrite: true}.Format(path)
			require.Len(t, first, 1)
			if first[0].Failed() {
				// Some of the corpus deliberately does not parse. Formatting
				// leaves those alone, which the table above checks; there is
				// no canonical form of one for anything to be a fixed point
				// of.
				return
			}

			after, err := os.ReadFile(path)
			require.NoError(t, err)

			second := Formatter{Rewrite: true, Diff: true}.Format(path)
			require.Len(t, second, 1)

			assert.False(t, second[0].Changed, "a second pass would change the file:\n%s", second[0].Diff)
			assert.False(t, second[0].Written)

			again, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(after), string(again))
		})
	}
}

// TestFormatWritesWhatPrintWrites keeps formatting and printing in step: what
// lands on disk is exactly what a caller printing the same file would get, so
// a file the command line interface wrote and a file put through fmt cannot
// differ.
func TestFormatWritesWhatPrintWrites(t *testing.T) {
	for _, source := range corpus(t) {
		t.Run(source, func(t *testing.T) {
			src, err := os.ReadFile(source)
			require.NoError(t, err)

			dir := tree(t, map[string]string{"a.dfc": string(src)})
			path := filepath.Join(dir, "a.dfc")

			file, parsed := Parse(path, bytes.NewReader(src))

			formatted := Formatter{Rewrite: true}.Format(path)
			require.Len(t, formatted, 1)

			if parsed != nil {
				assert.True(t, formatted[0].Failed())
				return
			}

			var want strings.Builder
			require.NoError(t, Print(&want, file))

			got, err := os.ReadFile(path)
			require.NoError(t, err)

			assert.Equal(t, want.String(), string(got))
		})
	}
}

// TestFormatReplacesAtomically is its own function because it is about the
// mechanics of the replacement rather than about which files were formatted.
func TestFormatReplacesAtomically(t *testing.T) {
	dir := tree(t, map[string]string{"a.dfc": asWritten})
	path := filepath.Join(dir, "a.dfc")
	require.NoError(t, os.Chmod(path, 0o640))

	formatted := Formatter{Rewrite: true}.Format(path)

	require.Len(t, formatted, 1)
	require.True(t, formatted[0].Written)

	// The replacement is a rename over the target, so once it is done nothing
	// else may be left in the directory.
	assert.Equal(t, map[string]string{"a.dfc": asPrinted}, contents(t, dir))

	// A temporary file is created private to its owner. The file it replaces
	// was not, and the one replacing it must not be either.
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o640), info.Mode().Perm())
}

func TestFormatLeavesTheFileAloneWhenItCannotBeReplaced(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the superuser writes into a directory it has no write permission on")
	}

	dir := tree(t, map[string]string{"a.dfc": asWritten})
	path := filepath.Join(dir, "a.dfc")

	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	formatted := Formatter{Rewrite: true}.Format(path)

	require.Len(t, formatted, 1)
	assert.True(t, formatted[0].Failed())
	assert.True(t, formatted[0].Changed)
	assert.False(t, formatted[0].Written)

	var failure WriteError
	require.ErrorAs(t, formatted[0].Err, &failure)
	assert.Equal(t, path, failure.Path)
	assert.ErrorIs(t, failure, fs.ErrPermission)

	src, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, asWritten, string(src))
}

func TestFormatReportsAPathItCannotReach(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.dfc")

	formatted := Formatter{Rewrite: true}.Format(path)

	require.Len(t, formatted, 1)
	assert.Equal(t, path, formatted[0].Path)
	assert.True(t, formatted[0].Failed())
	assert.ErrorIs(t, formatted[0].Err, fs.ErrNotExist)
	assert.Empty(t, formatted[0].Diagnostics)
}
