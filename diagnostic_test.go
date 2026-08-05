// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateGolden rewrites every golden file under testdata from what this
// package produced, so that a deliberate change to a rendering or to canonical
// form is a diff to review rather than a set of string literals to retype.
//
//	go test . -update
//
// It is one flag rather than one per directory because a change to the printer
// or to the renderer reaches all of them at once, and regenerating half a
// corpus leaves a tree nobody can read a diff of. CI regenerates and then
// requires the tree to be unchanged, so a golden which was edited by hand — or
// left behind by a change nobody regenerated — fails there.
var updateGolden = flag.Bool("update", false, "rewrite the golden files under testdata")

// diagnosticFixture is the source every rendering fixture points into. Paths
// are written with forward slashes because they are printed into the golden
// files, which are compared byte for byte.
const diagnosticFixture = "testdata/diagnostics/level-1.dfc"

// loadFixture returns the fixture's source, its loaded tree, and the source
// map the renderer quotes it from.
func loadFixture(t *testing.T) ([]byte, *File, Sources) {
	t.Helper()

	src, err := os.ReadFile(diagnosticFixture)
	require.NoError(t, err)

	file, err := Parse(diagnosticFixture, bytes.NewReader(src))
	require.NoError(t, err)

	return src, file, Sources{diagnosticFixture: src}
}

// golden returns the expected rendering held in testdata/diagnostics/name,
// having first rewritten it from got when -update was passed.
func golden(t *testing.T, name string, got string) string {
	t.Helper()

	path := "testdata/diagnostics/" + name
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

func TestDiagnosticRender(t *testing.T) {
	testCases := []struct {
		name   string
		golden string
		build  func(t *testing.T, src []byte, file *File) Diagnostic
	}{
		{
			name:   "underlines the offending text and prints the hint beneath it",
			golden: "single-line.txt",
			build: func(t *testing.T, src []byte, file *File) Diagnostic {
				return Diagnostic{
					Severity: SeverityError,
					Span:     at(t, file, 0, 2, 1).Span,
					Message:  "expected a label of at most 64 characters, found one of 80",
					Hint:     "a longer name belongs in a claim under a registered predicate",
				}
			},
		},
		{
			name:   "quotes every line a span covers",
			golden: "multi-line.txt",
			build: func(t *testing.T, src []byte, file *File) Diagnostic {
				return Diagnostic{
					Severity: SeverityError,
					Span:     at(t, file, 0).Span,
					Message:  "expected a (type ...) child of every node form, found none",
				}
			},
		},
		{
			name:   "elides the middle of a span too long to quote whole",
			golden: "elided.txt",
			build: func(t *testing.T, src []byte, file *File) Diagnostic {
				return Diagnostic{
					Severity: SeverityError,
					Span:     at(t, file, 1).Span,
					Message:  "expected at most one (position ...) child, found two",
				}
			},
		},
		{
			name:   "points at both places a redefinition involves",
			golden: "related.txt",
			build: func(t *testing.T, src []byte, file *File) Diagnostic {
				return Diagnostic{
					Severity: SeverityError,
					Span:     at(t, file, 1, 1).Span,
					Message:  "expected an unused id, found site:S-101, which is already defined",
					Hint:     "an id is immutable and unique across the whole model; a second reading of one node is a claim on it",
					Related: []RelatedLocation{
						{
							Span:    at(t, file, 0, 1).Span,
							Message: "first defined here",
						},
					},
				}
			},
		},
		{
			name:   "points at the end of the file when the file ends too early",
			golden: "end-of-file.txt",
			build: func(t *testing.T, src []byte, file *File) Diagnostic {
				lines := newLineIndex(diagnosticFixture, src)

				return Diagnostic{
					Severity: SeverityError,
					Span:     lines.at(len(src)).Span(),
					Message:  "expected a closing parenthesis, found the end of the file",
				}
			},
		},
		{
			name:   "renders a warning the same way it renders an error",
			golden: "warning.txt",
			build: func(t *testing.T, src []byte, file *File) Diagnostic {
				return Diagnostic{
					Severity: SeverityWarning,
					Span:     at(t, file, 1, 6).Span,
					Message:  "expected a unit after the value, found none",
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			src, file, sources := loadFixture(t)

			var buf bytes.Buffer
			require.NoError(t, testCase.build(t, src, file).Render(&buf, sources))

			assert.Equal(t, golden(t, testCase.golden, buf.String()), buf.String())
		})
	}
}

func TestDiagnosticRenderWithoutSource(t *testing.T) {
	testCases := []struct {
		name     string
		sources  SourceMap
		expected string
	}{
		{
			name:    "writes the headers alone when there is no source at all",
			sources: nil,
			expected: "level-1.dfc:1:7: error: expected an unused id, found site:S-101\n" +
				" = hint: an id is unique across the whole model\n" +
				"level-1.dfc:6:7: note: first defined here\n",
		},
		{
			name:    "writes the headers alone when the source map does not have the file",
			sources: Sources{"other.dfc": []byte("(node site:S-102)\n")},
			expected: "level-1.dfc:1:7: error: expected an unused id, found site:S-101\n" +
				" = hint: an id is unique across the whole model\n" +
				"level-1.dfc:6:7: note: first defined here\n",
		},
	}

	diagnostic := Diagnostic{
		Severity: SeverityError,
		Span: Span{
			Start: Position{Path: "level-1.dfc", Line: 1, Column: 7, Offset: 6},
			End:   Position{Path: "level-1.dfc", Line: 1, Column: 17, Offset: 16},
		},
		Message: "expected an unused id, found site:S-101",
		Hint:    "an id is unique across the whole model",
		Related: []RelatedLocation{
			{
				Span: Span{
					Start: Position{Path: "level-1.dfc", Line: 6, Column: 7, Offset: 118},
					End:   Position{Path: "level-1.dfc", Line: 6, Column: 17, Offset: 128},
				},
				Message: "first defined here",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, diagnostic.Render(&buf, testCase.sources))

			assert.Equal(t, testCase.expected, buf.String())
		})
	}
}

// TestDiagnosticRenderAlignment is its own table because it asserts on where
// the carets land rather than on a whole rendering: a column is only right
// relative to the line above it, so these cases are read as a pair of lines.
func TestDiagnosticRenderAlignment(t *testing.T) {
	testCases := []struct {
		name     string
		source   string
		text     string
		expected string
	}{
		{
			name:     "pads by rune rather than by byte so multi-byte text does not shift the carets",
			source:   "(label \"réunion\" m)\n",
			text:     "m",
			expected: "1 | (label \"réunion\" m)\n  |                  ^\n",
		},
		{
			name:     "repeats a tab in the padding so the caret keeps the column the tab gave it",
			source:   "(label\t\"B\"\tm)\n",
			text:     "m",
			expected: "1 | (label\t\"B\"\tm)\n  |       \t   \t^\n",
		},
		{
			name:     "underlines every rune of the span and no more",
			source:   "(label \"réunion\" m)\n",
			text:     "\"réunion\"",
			expected: "1 | (label \"réunion\" m)\n  |        ^^^^^^^^^\n",
		},
		{
			name:     "underlines a single caret where the span is empty",
			source:   "(label \"B\")\n",
			text:     "",
			expected: "1 | (label \"B\")\n  | ^\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			const path = "alignment.dfc"

			start := strings.Index(testCase.source, testCase.text)
			require.GreaterOrEqual(t, start, 0)

			lines := newLineIndex(path, []byte(testCase.source))
			diagnostic := Diagnostic{
				Severity: SeverityError,
				Span:     Span{Start: lines.at(start), End: lines.at(start + len(testCase.text))},
				Message:  "expected a unit, found none",
			}

			var buf bytes.Buffer
			require.NoError(t, diagnostic.Render(&buf, Sources{path: []byte(testCase.source)}))

			_, quoted, found := strings.Cut(buf.String(), "\n")
			require.True(t, found)
			assert.Equal(t, testCase.expected, quoted)
		})
	}
}

func TestDiagnosticString(t *testing.T) {
	testCases := []struct {
		name       string
		diagnostic Diagnostic
		expected   string
	}{
		{
			name: "renders the position, the severity and the message on one line",
			diagnostic: Diagnostic{
				Severity: SeverityError,
				Span:     Position{Path: "level-1.dfc", Line: 4, Column: 13}.Span(),
				Message:  "expected a unit after the value, found )",
			},
			expected: "level-1.dfc:4:13: error: expected a unit after the value, found )",
		},
		{
			name: "leaves the hint out of the one-line rendering",
			diagnostic: Diagnostic{
				Severity: SeverityWarning,
				Span:     Position{Path: "level-1.dfc", Line: 9, Column: 3}.Span(),
				Message:  "expected a registered predicate, found area",
				Hint:     "predicates are registry data",
			},
			expected: "level-1.dfc:9:3: warning: expected a registered predicate, found area",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, testCase.diagnostic.String())
		})
	}
}

func TestDiagnosticJSON(t *testing.T) {
	testCases := []struct {
		name       string
		diagnostic Diagnostic
		expected   string
	}{
		{
			name: "carries every field the human rendering shows",
			diagnostic: Diagnostic{
				Severity: SeverityError,
				Span: Span{
					Start: Position{Path: "level-1.dfc", Line: 6, Column: 7, Offset: 118},
					End:   Position{Path: "level-1.dfc", Line: 6, Column: 17, Offset: 128},
				},
				Message: "expected an unused id, found site:S-101",
				Hint:    "an id is unique across the whole model",
				Related: []RelatedLocation{
					{
						Span: Span{
							Start: Position{Path: "level-1.dfc", Line: 1, Column: 7, Offset: 6},
							End:   Position{Path: "level-1.dfc", Line: 1, Column: 17, Offset: 16},
						},
						Message: "first defined here",
					},
				},
			},
			expected: `{"severity":"error","span":"level-1.dfc:6:7-6:17",` +
				`"message":"expected an unused id, found site:S-101",` +
				`"hint":"an id is unique across the whole model",` +
				`"related":[{"span":"level-1.dfc:1:7-1:17","message":"first defined here"}]}`,
		},
		{
			name: "leaves the optional fields out when they are unset",
			diagnostic: Diagnostic{
				Severity: SeverityWarning,
				Span:     Position{Path: "level-1.dfc", Line: 9, Column: 3, Offset: 150}.Span(),
				Message:  "expected a unit after the value, found none",
			},
			expected: `{"severity":"warning","span":"level-1.dfc:9:3",` +
				`"message":"expected a unit after the value, found none"}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			encoded, err := json.Marshal(testCase.diagnostic)
			require.NoError(t, err)
			assert.Equal(t, testCase.expected, string(encoded))

			// The machine form is the same data, not a rendering of it: what it
			// carries has to read back as the diagnostic it came from — every
			// part of it a reader is sent to the source with. The byte offsets
			// are the documented exception, because the span's text form does
			// not write them; see [Span.MarshalJSON].
			var decoded Diagnostic
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			assert.Equal(t, located(testCase.diagnostic), decoded)
		})
	}
}

// located is a diagnostic as it comes back through JSON: the same diagnostic
// with the byte offsets the text form does not carry cleared, so that what the
// round trip is asserted against is the encoding's documented behaviour rather
// than an encoding it does not have.
func located(diagnostic Diagnostic) Diagnostic {
	out := diagnostic
	out.Span = unoffset(diagnostic.Span)

	out.Related = nil
	for _, related := range diagnostic.Related {
		related.Span = unoffset(related.Span)
		out.Related = append(out.Related, related)
	}

	return out
}

// unoffset is a span with both byte offsets cleared.
func unoffset(span Span) Span {
	span.Start.Offset, span.End.Offset = 0, 0
	return span
}

func TestDiagnosticsOrdering(t *testing.T) {
	// Positions are written out of order on purpose: what is asserted is that
	// reporting order does not depend on the order problems were found in.
	testCases := []struct {
		name     string
		added    []Diagnostic
		expected []string
	}{
		{
			name: "orders by file before position",
			added: []Diagnostic{
				{Severity: SeverityError, Span: Position{Path: "b.dfc", Line: 1, Column: 1}.Span(), Message: "second file"},
				{Severity: SeverityError, Span: Position{Path: "a.dfc", Line: 9, Column: 1}.Span(), Message: "first file"},
			},
			expected: []string{"first file", "second file"},
		},
		{
			name: "orders by line and then by column within one file",
			added: []Diagnostic{
				{Severity: SeverityError, Span: Position{Path: "a.dfc", Line: 2, Column: 9}.Span(), Message: "later column"},
				{Severity: SeverityError, Span: Position{Path: "a.dfc", Line: 9, Column: 1}.Span(), Message: "later line"},
				{Severity: SeverityError, Span: Position{Path: "a.dfc", Line: 2, Column: 1}.Span(), Message: "earlier column"},
			},
			expected: []string{"earlier column", "later column", "later line"},
		},
		{
			name: "orders two diagnostics at one position by severity and then by message",
			added: []Diagnostic{
				{Severity: SeverityWarning, Span: Position{Path: "a.dfc", Line: 1, Column: 1}.Span(), Message: "a warning"},
				{Severity: SeverityError, Span: Position{Path: "a.dfc", Line: 1, Column: 1}.Span(), Message: "second error"},
				{Severity: SeverityError, Span: Position{Path: "a.dfc", Line: 1, Column: 1}.Span(), Message: "first error"},
			},
			expected: []string{"first error", "second error", "a warning"},
		},
		{
			name: "orders a shorter span before a longer one starting at the same place",
			added: []Diagnostic{
				{
					Severity: SeverityError,
					Span: Span{
						Start: Position{Path: "a.dfc", Line: 1, Column: 1},
						End:   Position{Path: "a.dfc", Line: 4, Column: 2},
					},
					Message: "the whole form",
				},
				{
					Severity: SeverityError,
					Span: Span{
						Start: Position{Path: "a.dfc", Line: 1, Column: 1},
						End:   Position{Path: "a.dfc", Line: 1, Column: 5},
					},
					Message: "its first token",
				},
			},
			expected: []string{"its first token", "the whole form"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var diagnostics Diagnostics
			diagnostics.Add(testCase.added...)

			var messages []string
			for _, diagnostic := range diagnostics.All() {
				messages = append(messages, diagnostic.Message)
			}

			assert.Equal(t, testCase.expected, messages)

			// Ordering is a property of the set rather than of one call: a
			// second reading has to give back the same sequence.
			assert.Equal(t, diagnostics.All(), diagnostics.All())
		})
	}
}

func TestDiagnosticsLimit(t *testing.T) {
	testCases := []struct {
		name       string
		limit      int
		added      int
		reported   int
		suppressed int
	}{
		{
			name:       "retains everything below the default limit",
			added:      10,
			reported:   10,
			suppressed: 0,
		},
		{
			name:       "suppresses everything past the default limit",
			added:      DefaultDiagnosticLimit + 7,
			reported:   DefaultDiagnosticLimit,
			suppressed: 7,
		},
		{
			name:       "honours a limit the caller set",
			limit:      3,
			added:      5,
			reported:   3,
			suppressed: 2,
		},
		{
			name:       "retains everything when the limit is negative",
			limit:      -1,
			added:      DefaultDiagnosticLimit + 7,
			reported:   DefaultDiagnosticLimit + 7,
			suppressed: 0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			diagnostics := Diagnostics{Limit: testCase.limit}
			for i := range testCase.added {
				diagnostics.Add(Diagnostic{
					Severity: SeverityError,
					Span:     Position{Path: "a.dfc", Line: i + 1, Column: 1}.Span(),
					Message:  "expected a form, found none",
				})
			}

			assert.Equal(t, testCase.reported, diagnostics.Len())
			assert.Equal(t, testCase.reported, len(diagnostics.All()))
			assert.Equal(t, testCase.suppressed, diagnostics.Suppressed())
		})
	}
}

func TestDiagnosticsRender(t *testing.T) {
	testCases := []struct {
		name     string
		limit    int
		added    []Diagnostic
		expected string
	}{
		{
			name:     "writes nothing at all when there is nothing to report",
			expected: "",
		},
		{
			name: "writes every diagnostic in reporting order",
			added: []Diagnostic{
				{Severity: SeverityWarning, Span: Position{Path: "b.dfc", Line: 1, Column: 1}.Span(), Message: "expected a unit, found none"},
				{Severity: SeverityError, Span: Position{Path: "a.dfc", Line: 2, Column: 3}.Span(), Message: "expected a form, found an atom"},
			},
			expected: "a.dfc:2:3: error: expected a form, found an atom\n" +
				"b.dfc:1:1: warning: expected a unit, found none\n",
		},
		{
			name:  "says how many diagnostics the limit suppressed",
			limit: 2,
			added: []Diagnostic{
				{Severity: SeverityError, Span: Position{Path: "a.dfc", Line: 1, Column: 1}.Span(), Message: "first"},
				{Severity: SeverityError, Span: Position{Path: "a.dfc", Line: 2, Column: 1}.Span(), Message: "second"},
				{Severity: SeverityError, Span: Position{Path: "a.dfc", Line: 3, Column: 1}.Span(), Message: "third"},
				{Severity: SeverityError, Span: Position{Path: "a.dfc", Line: 4, Column: 1}.Span(), Message: "fourth"},
			},
			expected: "a.dfc:1:1: error: first\n" +
				"a.dfc:2:1: error: second\n" +
				"2 more diagnostics suppressed by the limit of 2\n",
		},
		{
			name:  "says it in the singular when the limit suppressed one",
			limit: 1,
			added: []Diagnostic{
				{Severity: SeverityError, Span: Position{Path: "a.dfc", Line: 1, Column: 1}.Span(), Message: "first"},
				{Severity: SeverityError, Span: Position{Path: "a.dfc", Line: 2, Column: 1}.Span(), Message: "second"},
			},
			expected: "a.dfc:1:1: error: first\n" +
				"1 more diagnostic suppressed by the limit of 1\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			diagnostics := Diagnostics{Limit: testCase.limit}
			diagnostics.Add(testCase.added...)

			var buf bytes.Buffer
			require.NoError(t, diagnostics.Render(&buf, nil))

			assert.Equal(t, testCase.expected, buf.String())
		})
	}
}

func TestDiagnosticsHasErrors(t *testing.T) {
	testCases := []struct {
		name     string
		added    []Diagnostic
		expected bool
	}{
		{
			name:     "reports no errors when nothing was collected",
			expected: false,
		},
		{
			name: "reports no errors when everything collected is a warning",
			added: []Diagnostic{
				{Severity: SeverityWarning, Span: Position{Path: "a.dfc", Line: 1, Column: 1}.Span(), Message: "a warning"},
			},
			expected: false,
		},
		{
			name: "reports an error when one was collected among warnings",
			added: []Diagnostic{
				{Severity: SeverityWarning, Span: Position{Path: "a.dfc", Line: 1, Column: 1}.Span(), Message: "a warning"},
				{Severity: SeverityError, Span: Position{Path: "a.dfc", Line: 2, Column: 1}.Span(), Message: "an error"},
			},
			expected: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var diagnostics Diagnostics
			diagnostics.Add(testCase.added...)

			assert.Equal(t, testCase.expected, diagnostics.HasErrors())
		})
	}
}

// TestDiagnosticsCollectInOnePass is the property the whole type exists for:
// one pass over input reports every independent problem, rather than the first
// one and nothing about the rest.
func TestDiagnosticsCollectInOnePass(t *testing.T) {
	_, file, sources := loadFixture(t)

	var diagnostics Diagnostics
	for i, node := range file.Nodes {
		diagnostics.Add(Diagnostic{
			Severity: SeverityError,
			Span:     node.Children[1].Span,
			Message:  "expected a (type ...) child of every node form, found none",
			Related: []RelatedLocation{
				{Span: node.Span, Message: "in this form"},
			},
		})

		require.Equal(t, i+1, diagnostics.Len())
	}

	require.Equal(t, len(file.Nodes), diagnostics.Len())
	require.Greater(t, diagnostics.Len(), 1)

	var buf bytes.Buffer
	require.NoError(t, diagnostics.Render(&buf, sources))

	for _, node := range file.Nodes {
		assert.Contains(t, buf.String(), node.Children[1].Span.Start.String())
	}
}

// TestDiagnosticRenderCarriageReturn pins the rendering of a file written with
// CRLF line endings, where the last byte of every line is a carriage return
// that no reader can see.
//
// Positions stay byte-based over the source as written — a carriage return is
// whitespace and not a terminator, so it is part of the line it ends and
// columns count it. What is quoted leaves it out: writing it would return the
// cursor to the start of the line and overwrite the whole quotation. Both hold
// at once because a carriage return is only ever the last byte of its line, so
// the only column it can be given is one past the text, which is where a caret
// for something missing from the end of a line belongs anyway.
func TestDiagnosticRenderCarriageReturn(t *testing.T) {
	const path = "crlf.dfc"
	const source = "(node site:S-101\r\n  (label \"B\")\r\n"

	lines := newLineIndex(path, []byte(source))

	testCases := []struct {
		name     string
		span     Span
		expected string
	}{
		{
			name:     "quotes the line without the carriage return which ends it",
			span:     Span{Start: lines.at(6), End: lines.at(16)},
			expected: "1 | (node site:S-101\n  |       ^^^^^^^^^^\n",
		},
		{
			name:     "puts the caret one past the text for a position at the line terminator",
			span:     lines.at(16).Span(),
			expected: "1 | (node site:S-101\n  |                 ^\n",
		},
		{
			name:     "underlines a span covering nothing but the carriage return",
			span:     Span{Start: lines.at(16), End: lines.at(17)},
			expected: "1 | (node site:S-101\n  |                 ^\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			diagnostic := Diagnostic{
				Severity: SeverityError,
				Span:     testCase.span,
				Message:  "expected a closing parenthesis, found the end of the line",
			}

			var buf bytes.Buffer
			require.NoError(t, diagnostic.Render(&buf, Sources{path: []byte(source)}))

			header, quoted, found := strings.Cut(buf.String(), "\n")
			require.True(t, found)

			assert.Equal(t, testCase.span.Start.String(), strings.SplitN(header, ": ", 2)[0])
			assert.Equal(t, testCase.expected, quoted)
		})
	}
}
