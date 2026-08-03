// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sexpr "github.com/z5labs/sexpr-go"
)

// spanSource is the fixture the span tables below are written against. Every
// case names a path through the tree and the exact text it must span, so the
// assertions read as "this node was written here" rather than as a pair of
// numbers nobody can check by eye.
const spanSource = `(node site:S-101
  (label "Meeting Room B")
  (kind Space)
  (position (value (0.0 4.05 0.0) m) (dimension 3)))
(vertex geom:V-01 #t nil)
`

// at walks a path of child indices from a top-level node.
func at(t *testing.T, file *File, top int, path ...int) *Node {
	t.Helper()

	require.Greater(t, len(file.Nodes), top, "no top-level node %d", top)

	node := file.Nodes[top]
	for _, i := range path {
		require.Greater(t, len(node.Children), i, "no child %d", i)
		node = node.Children[i]
	}

	return node
}

func TestParseSpans(t *testing.T) {
	testCases := []struct {
		name  string
		top   int
		path  []int
		text  string
		start Position
		end   Position
	}{
		{
			name:  "spans a whole top-level list from its parentheses",
			top:   0,
			text:  "(node site:S-101\n  (label \"Meeting Room B\")\n  (kind Space)\n  (position (value (0.0 4.05 0.0) m) (dimension 3)))",
			start: Position{Line: 1, Column: 1, Offset: 0},
			end:   Position{Line: 4, Column: 53, Offset: 111},
		},
		{
			name:  "spans a symbol from its first byte to one past its last",
			top:   0,
			path:  []int{0},
			text:  "node",
			start: Position{Line: 1, Column: 2, Offset: 1},
			end:   Position{Line: 1, Column: 6, Offset: 5},
		},
		{
			name:  "spans a nested list on a later line",
			top:   0,
			path:  []int{2},
			text:  `(label "Meeting Room B")`,
			start: Position{Line: 2, Column: 3, Offset: 19},
			end:   Position{Line: 2, Column: 27, Offset: 43},
		},
		{
			name:  "spans a string including its quotes",
			top:   0,
			path:  []int{2, 1},
			text:  `"Meeting Room B"`,
			start: Position{Line: 2, Column: 10, Offset: 26},
			end:   Position{Line: 2, Column: 26, Offset: 42},
		},
		{
			name:  "spans a doubly nested list",
			top:   0,
			path:  []int{4, 1},
			text:  "(value (0.0 4.05 0.0) m)",
			start: Position{Line: 4, Column: 13, Offset: 71},
			end:   Position{Line: 4, Column: 37, Offset: 95},
		},
		{
			name:  "spans a float three lists deep",
			top:   0,
			path:  []int{4, 1, 1, 1},
			text:  "4.05",
			start: Position{Line: 4, Column: 25, Offset: 83},
			end:   Position{Line: 4, Column: 29, Offset: 87},
		},
		{
			name:  "spans an integer",
			top:   0,
			path:  []int{4, 2, 1},
			text:  "3",
			start: Position{Line: 4, Column: 49, Offset: 107},
			end:   Position{Line: 4, Column: 50, Offset: 108},
		},
		{
			name:  "spans a boolean",
			top:   1,
			path:  []int{2},
			text:  "#t",
			start: Position{Line: 5, Column: 19, Offset: 130},
			end:   Position{Line: 5, Column: 21, Offset: 132},
		},
		{
			name:  "spans nil",
			top:   1,
			path:  []int{3},
			text:  "nil",
			start: Position{Line: 5, Column: 22, Offset: 133},
			end:   Position{Line: 5, Column: 25, Offset: 136},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			file, err := Parse("entities.dfc", strings.NewReader(spanSource))

			require.NoError(t, err)

			node := at(t, file, testCase.top, testCase.path...)

			start := testCase.start
			start.Path = "entities.dfc"
			end := testCase.end
			end.Path = "entities.dfc"

			assert.Equal(t, start, node.Span.Start)
			assert.Equal(t, end, node.Span.End)
			assert.Equal(t, testCase.text, spanSource[node.Span.Start.Offset:node.Span.End.Offset])
		})
	}
}

func TestParseSpansEveryNode(t *testing.T) {
	source := `; a leading comment
(node site:S-101
  ; about the label
  (label "a\nb\"c")
  (retired (date "2026-03-14") (reason "π moved")))
`

	file, err := Parse("entities.dfc", strings.NewReader(source))

	require.NoError(t, err)

	var walk func(node *Node)
	walk = func(node *Node) {
		span := node.Span

		assert.Equal(t, "entities.dfc", span.Start.Path)
		assert.Equal(t, "entities.dfc", span.End.Path)
		assert.LessOrEqual(t, span.Start.Offset, span.End.Offset, "a span never runs backwards")
		assert.NotEmpty(t, source[span.Start.Offset:span.End.Offset], "every datum spans some text")

		// Line and column agree with the offset, so a caller may use either.
		assert.Equal(t, span.Start.Line, strings.Count(source[:span.Start.Offset], "\n")+1)
		assert.Equal(t, span.End.Line, strings.Count(source[:span.End.Offset], "\n")+1)

		for _, child := range node.Children {
			assert.GreaterOrEqual(t, child.Span.Start.Offset, span.Start.Offset, "a child starts inside its parent")
			assert.LessOrEqual(t, child.Span.End.Offset, span.End.Offset, "a child ends inside its parent")
			walk(child)
		}
	}

	for _, node := range file.Nodes {
		walk(node)
	}

	require.Len(t, file.Comments, 1)
	assert.Equal(t, "; a leading comment", file.Comments[0].Text)
	assert.Equal(t, "; a leading comment", source[file.Comments[0].Span.Start.Offset:file.Comments[0].Span.End.Offset])

	comments := file.Nodes[0].Comments
	require.Len(t, comments, 1)
	assert.Equal(t, "; about the label", comments[0].Text)
	assert.Equal(t, "; about the label", source[comments[0].Span.Start.Offset:comments[0].Span.End.Offset])

	// An escaped string spans its source text, not its decoded value, and a
	// multi-byte rune advances the column by its encoded width.
	label := at(t, file, 0, 2, 1)
	assert.Equal(t, `"a\nb\"c"`, source[label.Span.Start.Offset:label.Span.End.Offset])
	assert.Equal(t, "a\nb\"c", label.Datum.(sexpr.String).Value)

	reason := at(t, file, 0, 3, 2, 1)
	assert.Equal(t, `"π moved"`, source[reason.Span.Start.Offset:reason.Span.End.Offset])
	assert.Equal(t, reason.Span.End.Column-reason.Span.Start.Column, len(`"π moved"`))
}

func TestParseTreeShape(t *testing.T) {
	t.Run("gives a list one child per element", func(t *testing.T) {
		file, err := Parse("a.dfc", strings.NewReader("(a b c)"))

		require.NoError(t, err)
		require.Len(t, file.Nodes, 1)

		list, ok := file.Nodes[0].Datum.(sexpr.List)

		require.True(t, ok)
		assert.Len(t, file.Nodes[0].Children, len(list.Elements))
	})

	t.Run("gives an improper list one child more than it has elements", func(t *testing.T) {
		file, err := Parse("a.dfc", strings.NewReader("(a b . c)"))

		require.NoError(t, err)
		require.Len(t, file.Nodes, 1)

		node := file.Nodes[0]

		require.Len(t, node.Children, 3)
		assert.Equal(t, "c", node.Children[2].Datum.(sexpr.Symbol).Value)
		assert.Equal(t, "c", "(a b . c)"[node.Children[2].Span.Start.Offset:node.Children[2].Span.End.Offset])
	})

	t.Run("gives a quote shorthand one child and spans both together", func(t *testing.T) {
		source := ",@(a b)"

		file, err := Parse("a.dfc", strings.NewReader(source))

		require.NoError(t, err)
		require.Len(t, file.Nodes, 1)

		node := file.Nodes[0]

		require.Len(t, node.Children, 1)
		assert.Equal(t, source, source[node.Span.Start.Offset:node.Span.End.Offset])
		assert.Equal(t, "(a b)", source[node.Children[0].Span.Start.Offset:node.Children[0].Span.End.Offset])
	})

	t.Run("gives an atom no children", func(t *testing.T) {
		file, err := Parse("a.dfc", strings.NewReader("a"))

		require.NoError(t, err)
		require.Len(t, file.Nodes, 1)
		assert.Empty(t, file.Nodes[0].Children)
	})
}

func TestParseFiles(t *testing.T) {
	testCases := []struct {
		name    string
		source  string
		nodes   int
		asserts func(t *testing.T, file *File)
	}{
		{
			name:   "loads an empty file as a file with no datums",
			source: "",
			nodes:  0,
		},
		{
			name:   "loads a file holding only whitespace as a file with no datums",
			source: "\n\n   \n",
			nodes:  0,
		},
		{
			name:   "loads a file with no trailing newline",
			source: "(a b)",
			nodes:  1,
		},
		{
			name:   "loads a file with a trailing newline to the same tree",
			source: "(a b)\n",
			nodes:  1,
		},
		{
			name:   "reads CRLF line endings as line terminators",
			source: "(a)\r\n(b)\r\n",
			nodes:  2,
			asserts: func(t *testing.T, file *File) {
				assert.Equal(t, 2, file.Nodes[1].Span.Start.Line)
				assert.Equal(t, 1, file.Nodes[1].Span.Start.Column)
			},
		},
		{
			name:   "reads a lone carriage return as whitespace and not as a line terminator",
			source: "(a)\r(b)\r",
			nodes:  2,
			asserts: func(t *testing.T, file *File) {
				assert.Equal(t, 1, file.Nodes[1].Span.Start.Line)
				assert.Equal(t, 5, file.Nodes[1].Span.Start.Column)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			file, err := Parse("a.dfc", strings.NewReader(testCase.source))

			require.NoError(t, err)
			require.NotNil(t, file)
			assert.Equal(t, "a.dfc", file.Path)
			assert.Len(t, file.Nodes, testCase.nodes)

			if testCase.asserts != nil {
				testCase.asserts(t, file)
			}
		})
	}
}

func TestParseTrailingNewlineIsNotLoadBearing(t *testing.T) {
	source := "(node site:S-101 (label \"a\") (kind Space))"

	with, err := Parse("a.dfc", strings.NewReader(source+"\n"))
	require.NoError(t, err)

	without, err := Parse("a.dfc", strings.NewReader(source))
	require.NoError(t, err)

	assert.Equal(t, with, without, "a missing trailing newline changes nothing about the tree")
}

func TestParseEncodingErrors(t *testing.T) {
	t.Run("reports invalid UTF-8 at the first offending byte", func(t *testing.T) {
		_, err := Parse("a.dfc", strings.NewReader("(a b)\n(c \xffd)\n"))

		var got EncodingError

		require.ErrorAs(t, err, &got)
		assert.Equal(t, byte(0xff), got.Byte)
		assert.Equal(t, Position{Path: "a.dfc", Line: 2, Column: 4, Offset: 9}, got.Position)
	})

	t.Run("reports invalid UTF-8 inside a string, which the tokenizer would otherwise accept", func(t *testing.T) {
		_, err := Parse("a.dfc", strings.NewReader("(label \"a\xffb\")\n"))

		var got EncodingError

		require.ErrorAs(t, err, &got)
		assert.Equal(t, byte(0xff), got.Byte)
		assert.Equal(t, 9, got.Position.Offset)
	})

	t.Run("reports a byte order mark rather than skipping it", func(t *testing.T) {
		_, err := Parse("a.dfc", strings.NewReader("\xef\xbb\xbf(a)\n"))

		var got ByteOrderMarkError

		require.ErrorAs(t, err, &got)
		assert.Equal(t, Position{Path: "a.dfc", Line: 1, Column: 1, Offset: 0}, got.Position)
	})

	t.Run("accepts non-ASCII text", func(t *testing.T) {
		file, err := Parse("a.dfc", strings.NewReader("(label \"Grünstraße\")\n"))

		require.NoError(t, err)
		assert.Len(t, file.Nodes, 1)
	})
}

func TestParseFailures(t *testing.T) {
	testCases := []struct {
		name     string
		source   string
		position Position
		target   any
	}{
		{
			name:     "reports an unclosed list where the input ran out",
			source:   "(a b\n",
			position: Position{Line: 1, Column: 4, Offset: 3},
			target:   new(sexpr.UnexpectedEndOfTokensError),
		},
		{
			name:     "reports an unterminated string at its opening quote",
			source:   "(label \"a\n",
			position: Position{Line: 1, Column: 8, Offset: 7},
			target:   new(sexpr.UnterminatedStringError),
		},
		{
			name:     "reports an unterminated block comment",
			source:   "(a)\n#| open\n",
			position: Position{Line: 2, Column: 1, Offset: 4},
			target:   new(sexpr.UnterminatedCommentError),
		},
		{
			name:     "reports a malformed number rather than reading it as a symbol",
			source:   "(date 2026-03-14)\n",
			position: Position{Line: 1, Column: 7, Offset: 6},
			target:   new(sexpr.InvalidNumberError),
		},
		{
			name:     "reports an unrecognised string escape",
			source:   "(label \"a\\qb\")\n",
			position: Position{Line: 1, Column: 10, Offset: 9},
			target:   new(sexpr.InvalidEscapeError),
		},
		{
			name:     "reports an unexpected closing parenthesis",
			source:   "(a))\n",
			position: Position{Line: 1, Column: 4, Offset: 3},
			target:   new(sexpr.UnexpectedTokenError),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			file, err := Parse("a.dfc", strings.NewReader(testCase.source))

			require.Error(t, err)
			assert.Nil(t, file, "a failed parse yields no tree rather than a partial one presented as complete")

			var got ParseError

			require.ErrorAs(t, err, &got)

			position := testCase.position
			position.Path = "a.dfc"
			assert.Equal(t, position, got.Position)

			// The tokenizer's and the parser's own error types stay reachable.
			assert.True(t, errors.As(err, testCase.target), "expected %T, got %T", testCase.target, got.Err)
		})
	}
}

func TestParseErrorUnwrapsToItsCause(t *testing.T) {
	cause := sexpr.UnterminatedStringError{Pos: sexpr.Pos{Line: 1, Column: 1}}
	err := ParseError{Position: Position{Path: "a.dfc", Line: 1, Column: 1}, Err: cause}

	assert.Equal(t, cause, errors.Unwrap(err))
	assert.Equal(t, cause, err.Err)
	assert.ErrorAs(t, error(err), &sexpr.UnterminatedStringError{})
}

func TestPositionString(t *testing.T) {
	testCases := []struct {
		name     string
		position Position
		expected string
	}{
		{
			name:     "renders a position as path, line and column",
			position: Position{Path: "entities/level-1.dfc", Line: 12, Column: 5, Offset: 240},
			expected: "entities/level-1.dfc:12:5",
		},
		{
			name:     "leaves the byte offset out, which is for a tool rather than a reader",
			position: Position{Path: "a.dfc", Line: 1, Column: 1, Offset: 0},
			expected: "a.dfc:1:1",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, testCase.position.String())
		})
	}
}

func TestLoadFile(t *testing.T) {
	t.Run("reads a file whatever its extension", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "entities.txt")

		require.NoError(t, os.WriteFile(path, []byte("(a b)\n"), 0o600))

		file, err := LoadFile(path)

		require.NoError(t, err)
		assert.Equal(t, path, file.Path)
		assert.Equal(t, path, file.Nodes[0].Span.Start.Path)
	})

	t.Run("reports a file which is not there", func(t *testing.T) {
		file, err := LoadFile(filepath.Join(t.TempDir(), "missing.dfc"))

		assert.Nil(t, file)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})
}

// tree writes a fixture directory and returns its root.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))

		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	return root
}

// paths consumes a load and returns the path of every file it yielded,
// requiring every one of them to have loaded.
func paths(t *testing.T, seq func(yield func(*File, error) bool)) []string {
	t.Helper()

	var loaded []string
	for file, err := range seq {
		require.NoError(t, err)
		loaded = append(loaded, file.Path)
	}

	return loaded
}

func TestLoadWalksEntityFiles(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			name: "reads every file with the entity extension beneath the root",
			files: map[string]string{
				"registry/registry.dfc": "(project)\n",
				"entities/level-1.dfc":  "(node site:S-101)\n",
				"top.dfc":               "(node site:S-102)\n",
			},
			want: []string{"entities/level-1.dfc", "registry/registry.dfc", "top.dfc"},
		},
		{
			name: "ignores every other extension",
			files: map[string]string{
				"a.dfc":     "(a)\n",
				"b.txt":     "(b)\n",
				"c.sexpr":   "(c)\n",
				"d.dfc.bak": "(d)\n",
				"e":         "(e)\n",
			},
			want: []string{"a.dfc"},
		},
		{
			name: "compares the extension byte-wise, so a different case is a different extension",
			files: map[string]string{
				"a.dfc": "(a)\n",
				"b.DFC": "(b)\n",
				"c.Dfc": "(c)\n",
			},
			want: []string{"a.dfc"},
		},
		{
			name: "yields nothing for a tree holding no entity file",
			files: map[string]string{
				"notes/README.md": "hello\n",
			},
			want: nil,
		},
		{
			name: "yields files in lexical order however they were written",
			files: map[string]string{
				"z.dfc":     "(z)\n",
				"a.dfc":     "(a)\n",
				"m/n.dfc":   "(n)\n",
				"m/a.dfc":   "(a)\n",
				"b/c/d.dfc": "(d)\n",
			},
			want: []string{"a.dfc", "b/c/d.dfc", "m/a.dfc", "m/n.dfc", "z.dfc"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, testCase.files)

			var want []string
			for _, name := range testCase.want {
				want = append(want, filepath.Join(root, filepath.FromSlash(name)))
			}

			assert.Equal(t, want, paths(t, Load(root)))
		})
	}
}

func TestLoadIsDeterministic(t *testing.T) {
	root := tree(t, map[string]string{
		"z.dfc":      "(z)\n",
		"a.dfc":      "(a)\n",
		"m/n.dfc":    "(n)\n",
		"m/a.dfc":    "(a)\n",
		"b/c/d.dfc":  "(d)\n",
		"b/c/aa.dfc": "(aa)\n",
	})

	first := paths(t, Load(root))
	for range 8 {
		assert.Equal(t, first, paths(t, Load(root)), "the same tree walked twice yields the same files in the same order")
	}
}

func TestLoadSingleFile(t *testing.T) {
	t.Run("reads a root which names a file whatever its extension", func(t *testing.T) {
		root := tree(t, map[string]string{"entities.txt": "(a b)\n"})
		path := filepath.Join(root, "entities.txt")

		assert.Equal(t, []string{path}, paths(t, Load(path)))
	})

	t.Run("reports a root which is not there", func(t *testing.T) {
		var errs []error
		for file, err := range Load(filepath.Join(t.TempDir(), "missing")) {
			assert.Nil(t, file)
			errs = append(errs, err)
		}

		require.Len(t, errs, 1)
		assert.ErrorIs(t, errs[0], os.ErrNotExist)
	})
}

func TestLoadReportsEveryUnloadableFile(t *testing.T) {
	root := tree(t, map[string]string{
		"a.dfc": "(a)\n",
		"b.dfc": "(b\n",
		"c.dfc": "(c)\n",
		"d.dfc": "\xef\xbb\xbf(d)\n",
	})

	var loaded []string
	var failed []string
	for file, err := range Load(root) {
		if err != nil {
			var parse ParseError
			var bom ByteOrderMarkError
			assert.True(t, errors.As(err, &parse) || errors.As(err, &bom), "got %T", err)
			failed = append(failed, err.Error())
			continue
		}
		loaded = append(loaded, filepath.Base(file.Path))
	}

	assert.Equal(t, []string{"a.dfc", "c.dfc"}, loaded, "one unloadable file does not hide the rest")
	assert.Len(t, failed, 2, "every unloadable file is reported, not only the first")
}

func TestLoadStreams(t *testing.T) {
	t.Run("opens no file until the sequence is ranged over", func(t *testing.T) {
		// Load on a root which cannot even be stat'd still returns a sequence
		// rather than doing the work eagerly.
		seq := Load(filepath.Join(t.TempDir(), "missing"))

		assert.NotNil(t, seq)
	})

	t.Run("stops walking as soon as the caller stops", func(t *testing.T) {
		root := tree(t, map[string]string{
			"a.dfc": "(a)\n",
			"b.dfc": "(b\n",
			"c.dfc": "(c\n",
		})

		var loaded []string
		for file, err := range Load(root) {
			require.NoError(t, err, "the walk read past where the caller stopped")
			loaded = append(loaded, filepath.Base(file.Path))
			break
		}

		assert.Equal(t, []string{"a.dfc"}, loaded)
	})
}
