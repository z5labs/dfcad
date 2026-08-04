// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sexpr "github.com/z5labs/sexpr-go"
)

// printed is the canonical printing of src, reported against path.
func printed(t *testing.T, path string, src []byte) string {
	t.Helper()

	file, err := Parse(path, bytes.NewReader(src))
	require.NoError(t, err)

	var out strings.Builder
	require.NoError(t, Print(&out, file))

	return out.String()
}

// printFixture is the canonical printing of testdata/print/name.dfc.
func printFixture(t *testing.T, name string) string {
	t.Helper()

	path := "testdata/print/" + name + ".dfc"

	src, err := os.ReadFile(path)
	require.NoError(t, err)

	return printed(t, path, src)
}

// wanted returns the canonical printing held in testdata/print/name.want,
// having first rewritten it from got when -update was passed.
//
// The fixtures are golden files rather than string literals in the table
// because canonical form is what a reviewer reads a diff of: a change to the
// printer shows up here as the bytes that changed, in a file that can be read
// as an entity file, rather than as a re-indented Go literal.
func wanted(t *testing.T, name string, got string) string {
	t.Helper()

	path := "testdata/print/" + name + ".want"
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

func TestPrint(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
	}{
		{
			name:    "orders the top-level forms by tag and then by name",
			fixture: "top-level-order",
		},
		{
			name:    "orders a form's children the way its table lists them, sorting the repeats",
			fixture: "child-order",
		},
		{
			name:    "puts the claims after the structural children and the assertions after the claims",
			fixture: "claims-and-assertions",
		},
		{
			name:    "leaves out a child which holds its default, however it was spelled",
			fixture: "defaults",
		},
		{
			name:    "leaves a sequence whose order is data in the order it was written",
			fixture: "sequences",
		},
		{
			name:    "moves a comment with the datum it annotates and leaves the text alone",
			fixture: "comments",
		},
		{
			name:    "breaks a form which runs past eighty columns and leaves a shorter one alone",
			fixture: "line-breaking",
		},
		{
			name:    "writes the shortest decimal which reads back as the same number",
			fixture: "numbers",
		},
		{
			name:    "escapes what a string has to escape and nothing else",
			fixture: "strings",
		},
		{
			name:    "normalises line endings, trailing whitespace and the final line break",
			fixture: "whitespace",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := printFixture(t, testCase.fixture)

			assert.Equal(t, wanted(t, testCase.fixture, got), got)
		})
	}
}

// corpus is every file the printing properties below are checked against: the
// inputs of the table above, the canonical printings they produce, the fixture
// corpus of the format's constructs, and the fixtures the validator already
// keeps — which between them hold every form of the format, and hold malformed
// input too.
//
// Printing something which does not load has to be as stable as printing
// something which does. A file somebody is part way through fixing is exactly
// the file they run fmt on.
//
// The invalid corpus is deliberately not here. Every file below has to parse,
// because a property about what printing preserves has nothing to say about a
// file there is no tree for; what those fixtures assert is in corpus_test.go.
func corpus(t *testing.T) []string {
	t.Helper()

	var paths []string
	for _, pattern := range []string{
		"testdata/print/*",
		"testdata/corpus/valid/*",
		"testdata/validate/*.dfc",
		"testdata/model/*/*.dfc",
		"testdata/registry/*/*.dfc",
	} {
		matched, err := filepath.Glob(pattern)
		require.NoError(t, err)
		paths = append(paths, matched...)
	}

	require.NotEmpty(t, paths)

	return paths
}

// TestPrintIsIdempotent is its own function because its assertion is a property
// of every input rather than a case of one behaviour: printing an
// already-canonical file produces the same bytes back.
//
// A printer which reorders on the second pass is one whose output no diff can
// be read from, and a canonical form which is not a fixed point is not one.
func TestPrintIsIdempotent(t *testing.T) {
	for _, path := range corpus(t) {
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(path)
			require.NoError(t, err)

			once := printed(t, path, src)
			twice := printed(t, path, []byte(once))

			assert.Equal(t, once, twice)
		})
	}
}

// TestPrintRoundTripsTheTree checks the other half of the round trip: parsing
// canonical output gives back the tree it was printed from.
//
// It is asserted as a property rather than against expected output because a
// test which only compares an exact output string can pass while that output no
// longer reads back. A number which lost a digit, a string escaped so that it
// reads back as different text, a comment which swallowed the element after it
// — each of those produces stable, idempotent, wrong bytes, which every other
// test here would accept.
func TestPrintRoundTripsTheTree(t *testing.T) {
	for _, path := range corpus(t) {
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(path)
			require.NoError(t, err)

			file, err := Parse(path, bytes.NewReader(src))
			require.NoError(t, err)

			var out strings.Builder
			require.NoError(t, Print(&out, file))

			read, err := Parse(path, strings.NewReader(out.String()))
			require.NoError(t, err)

			assert.Equal(t, unpositioned(canonical(file)), unpositioned(treeOf(read)))
		})
	}
}

// treeOf is a loaded file as the datums the printer works in, which is what
// makes it comparable with what the printer was given to write.
func treeOf(file *File) *sexpr.File {
	out := &sexpr.File{}
	for _, node := range file.Nodes {
		out.Nodes = append(out.Nodes, node.Datum)
	}
	for _, comment := range file.Comments {
		out.Comments = append(out.Comments, &sexpr.Comment{Text: comment.Text})
	}

	return out
}

// TestPrintIsDeterministic checks that the same model prints the same way
// however many times it is loaded.
//
// Repeating it in one process is what catches the failure worth catching: Go
// randomises the order of a map range on every range, not once per process, so
// a canonical form which depended on one would come out differently here.
// Nothing else about printing can differ between two runs — the input is the
// same bytes and the output is a pure function of them.
func TestPrintIsDeterministic(t *testing.T) {
	for _, path := range corpus(t) {
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(path)
			require.NoError(t, err)

			first := printed(t, path, src)
			for range 8 {
				assert.Equal(t, first, printed(t, path, src))
			}
		})
	}
}

// TestPrintWhitespace checks the two rules about the shape of the output rather
// than its content, over every input at once: no line ends in whitespace, and
// the file ends with exactly one line feed.
func TestPrintWhitespace(t *testing.T) {
	for _, path := range corpus(t) {
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(path)
			require.NoError(t, err)

			got := printed(t, path, src)
			require.NotEmpty(t, got)

			assert.True(t, strings.HasSuffix(got, "\n"), "output ends with a line feed")
			assert.False(t, strings.HasSuffix(got, "\n\n"), "output ends with only one line feed")
			assert.NotContains(t, got, "\r", "output holds no carriage return")

			for i, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
				assert.Equal(t, strings.TrimRight(line, " \t"), line, "line %d has no trailing whitespace", i+1)
			}
		})
	}
}

// TestPrintNumbers pins the float formatting edges a fixture reads awkwardly:
// the values whose shortest round-tripping decimal is not the one somebody
// would guess, and the ones where reading the result back is the whole point.
func TestPrintNumbers(t *testing.T) {
	testCases := []struct {
		name    string
		written string
		want    string
	}{
		{
			name:    "drops a trailing zero which carries no meaning",
			written: "8.50",
			want:    "8.5",
		},
		{
			name:    "keeps a real which would otherwise read back as an integer a real",
			written: "100.0",
			want:    "100.0",
		},
		{
			name:    "writes the digits a shorter decimal would not read back",
			written: "0.30000000000000004",
			want:    "0.30000000000000004",
		},
		{
			name:    "writes the shorter of two spellings of one value",
			written: "3.0e2",
			want:    "300.0",
		},
		{
			name:    "uses an exponent where the exponent is shorter",
			written: "1000000000000000000000.0",
			want:    "1e+21",
		},
		{
			name:    "keeps the sign of a negative zero",
			written: "-0.0",
			want:    "-0.0",
		},
		{
			name:    "writes the largest finite value",
			written: "1.7976931348623157e308",
			want:    "1.7976931348623157e+308",
		},
		{
			name:    "writes the smallest subnormal",
			written: "5e-324",
			want:    "5e-324",
		},
		{
			name:    "writes an integer with neither a fraction nor an exponent",
			written: "1200",
			want:    "1200",
		},
		{
			name:    "writes the largest integer",
			written: "9223372036854775807",
			want:    "9223372036854775807",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := printed(t, "numbers.dfc", []byte("(reals "+testCase.written+")\n"))

			assert.Equal(t, "(reals "+testCase.want+")\n", got)

			// Whatever it was written as, it has to read back as the same
			// value, which is the only reason the shortest form is safe.
			assert.Equal(t, valueOf(t, testCase.written), valueOf(t, testCase.want))
		})
	}
}

// valueOf parses one number and returns the datum it decodes to.
func valueOf(t *testing.T, written string) sexpr.Node {
	t.Helper()

	file, err := Parse("numbers.dfc", strings.NewReader("(reals "+written+")\n"))
	require.NoError(t, err)

	return unpositioned(treeOf(file)).Nodes[0].(sexpr.List).Elements[1]
}

// TestPrintStrings pins the escaping of the characters a fixture cannot hold
// legibly, and checks that every one of them reads back as the text it was
// printed from.
func TestPrintStrings(t *testing.T) {
	testCases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "escapes a quote and a backslash",
			text: `a "quoted" word and a back\slash`,
			want: `"a \"quoted\" word and a back\\slash"`,
		},
		{
			name: "escapes the control characters which have a short spelling",
			text: "\n\r\t\b\f",
			want: `"\n\r\t\b\f"`,
		},
		{
			name: "escapes every other control character in upper case hex",
			text: "\x00\x1f\x7f\u0085",
			want: `"\u0000\u001F\u007F\u0085"`,
		},
		{
			name: "writes a letter which is not ASCII as itself",
			text: "un mètre, 一メートル, ǅ",
			want: `"un mètre, 一メートル, ǅ"`,
		},
		{
			name: "writes nothing for the empty string",
			text: "",
			want: `""`,
		},
		{
			// The loader rejects a byte which begins no valid encoding, so a
			// replacement character in a loaded file is one somebody wrote. It
			// is escaped rather than written as itself, which is what the
			// underlying printer does and so what the sort key has to agree
			// with; the point of the case is that it still reads back.
			name: "escapes a replacement character somebody wrote",
			text: "\uFFFD",
			want: `"\uFFFD"`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			file := &File{Nodes: []*Node{{Datum: sexpr.String{Value: testCase.text}}}}

			var out strings.Builder
			require.NoError(t, Print(&out, file))

			assert.Equal(t, testCase.want+"\n", out.String())

			// The escape is only correct if it reads back, which is the whole
			// of what "escaping exactly what must be escaped" means.
			read, err := Parse("strings.dfc", strings.NewReader(out.String()))
			require.NoError(t, err)
			require.Len(t, read.Nodes, 1)

			datum, ok := read.Nodes[0].Datum.(sexpr.String)
			require.True(t, ok)
			assert.Equal(t, testCase.text, datum.Value)
		})
	}
}

// TestPrintEmptyInput is its own function because a file with no forms in it
// has no canonical printing to compare against — the assertion is about how
// little comes out, not about which bytes.
func TestPrintEmptyInput(t *testing.T) {
	testCases := []struct {
		name   string
		file   *File
		source string
		want   string
	}{
		{
			name: "writes nothing for a file which holds no forms",
			file: &File{Path: "entities/level-1.dfc"},
		},
		{
			name: "writes nothing for a file which was never loaded",
			file: nil,
		},
		{
			name:   "writes back a file which holds comments and nothing else",
			source: "; A file nobody has written a form into yet.\n",
			want:   "; A file nobody has written a form into yet.\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			file := testCase.file
			if testCase.source != "" {
				var err error
				file, err = Parse("entities/level-1.dfc", strings.NewReader(testCase.source))
				require.NoError(t, err)
			}

			var out strings.Builder

			require.NoError(t, Print(&out, file))
			assert.Equal(t, testCase.want, out.String())
		})
	}
}

// TestInlineRenderingIsWhatThePrinterWrites checks the rendering the ordering
// rule sorts by against the printer that eventually writes the same datum.
//
// The two are spelled separately — the sort key ignores the column limit and
// the comments, so it always exists — and this is what stops them drifting. A
// sort key which spelled a number or a string differently from the output would
// order a file by something nobody can see in it.
func TestInlineRenderingIsWhatThePrinterWrites(t *testing.T) {
	sources := []string{
		`(node site:S-101 (label "Meeting Room B") (kind Space))`,
		`(reals 8.50 -0.0 1e21 0.30480000000000002)`,
		`(counts 0 -9223372036854775808)`,
		`(flags #true #false nil)`,
		`(escapes "a \"quoted\" word" "a\ttab")`,
		`(quotes 'a ,b ,@c ` + "`d)",
		`(nested (a (b c)) (d))`,
		`(dotted a . b)`,
		`()`,
	}

	for _, source := range sources {
		t.Run(source, func(t *testing.T) {
			file, err := Parse("inline.dfc", strings.NewReader(source+"\n"))
			require.NoError(t, err)

			var out strings.Builder
			require.NoError(t, Print(&out, file))
			require.LessOrEqual(t, len(strings.TrimSuffix(out.String(), "\n")), sexpr.MaxLineWidth, "the case fits on one line")

			groups := attach(file.Comments, file.Nodes)
			items, _ := arrange(file.Nodes, groups, topLevelForm, byTag)
			require.Len(t, items, 1)

			assert.Equal(t, out.String(), items[0].inline+"\n")
		})
	}
}

// unpositioned is a tree with every position stripped out of it.
//
// It is what makes the tree the printer wrote comparable with the tree parsing
// its output gives back. The printer's positions are synthetic and the parser's
// are where things landed on the page; a position is the one part of a tree a
// round trip is not required to preserve.
func unpositioned(file *sexpr.File) *sexpr.File {
	out := &sexpr.File{Comments: unpositionedComments(file.Comments)}
	for _, node := range file.Nodes {
		out.Nodes = append(out.Nodes, unpositionedDatum(node))
	}

	return out
}

func unpositionedComments(comments []*sexpr.Comment) []*sexpr.Comment {
	if len(comments) == 0 {
		return nil
	}

	out := make([]*sexpr.Comment, 0, len(comments))
	for _, comment := range comments {
		out = append(out, &sexpr.Comment{Text: comment.Text})
	}
	return out
}

func unpositionedDatum(n sexpr.Node) sexpr.Node {
	switch datum := n.(type) {
	case sexpr.Symbol:
		datum.Pos = sexpr.Pos{}
		return datum
	case sexpr.String:
		datum.Pos = sexpr.Pos{}
		return datum
	case sexpr.Int:
		datum.Pos = sexpr.Pos{}
		return datum
	case sexpr.Float:
		datum.Pos = sexpr.Pos{}
		return datum
	case sexpr.Bool:
		datum.Pos = sexpr.Pos{}
		return datum
	case sexpr.Nil:
		datum.Pos = sexpr.Pos{}
		return datum
	case sexpr.Quote:
		return sexpr.Quote{Kind: datum.Kind, Datum: unpositionedDatum(datum.Datum)}
	case sexpr.List:
		out := sexpr.List{Comments: unpositionedComments(datum.Comments)}
		for _, element := range datum.Elements {
			out.Elements = append(out.Elements, unpositionedDatum(element))
		}
		if datum.Tail != nil {
			out.Tail = unpositionedDatum(datum.Tail)
		}
		return out
	}

	return n
}
