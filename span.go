// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	sexpr "github.com/z5labs/sexpr-go"
)

// Position is one point in one source file.
//
// Line and Column are 1-based. Column is a byte offset into its line rather
// than a count of characters, so a multi-byte rune advances it by its encoded
// width; that is the convention the underlying S-expression tokenizer reports
// and it is kept unchanged here. Offset is a 0-based byte offset into the
// whole file.
//
// Both spellings of the same point are carried because both are needed and
// neither is cheap to recover from the other once the source bytes are gone: a
// person reads path:line:column, and a tool slicing the source for a caret or a
// span of text wants the offset.
type Position struct {
	// Path is the file this position is in, exactly as the loader reached it.
	Path string `json:"path"`

	// Line is the 1-based line number.
	Line int `json:"line"`

	// Column is the 1-based byte offset into Line.
	Column int `json:"column"`

	// Offset is the 0-based byte offset into the file.
	Offset int `json:"offset"`
}

// String renders the position as path:line:column, which is the spelling every
// editor and every terminal already knows how to jump to.
func (p Position) String() string {
	return fmt.Sprintf("%s:%d:%d", p.Path, p.Line, p.Column)
}

// Span returns the empty span at the position, which is what something with no
// source text of its own — a token that is missing, the end of the file —
// points at.
func (p Position) Span() Span {
	return Span{Start: p, End: p}
}

// Span is the extent of the source text one node was written in.
//
// Start is the first byte of that text and End is one byte past its last, so
// for source bytes src the text is exactly src[Start.Offset:End.Offset] and its
// length is End.Offset-Start.Offset. A half-open span is what lets an empty
// extent be expressed at all, and it makes the arithmetic of underlining a
// range subtraction rather than a subtraction and a correction.
//
// In JSON it is a string rather than an object — see [Span.MarshalJSON] — and
// that spelling is the one the machine output contract writes.
type Span struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// String renders the span the way it is written in JSON: path:line:column for
// an empty span, and path:line:column-line:column for one with extent.
//
// The path is written once. A span never runs across two files — the extent of
// one node is text in one file — so repeating it on the far end would state
// the same thing twice.
func (s Span) String() string {
	start := fmt.Sprintf("%s:%d:%d", s.Start.Path, s.Start.Line, s.Start.Column)
	if s.Start.Line == s.End.Line && s.Start.Column == s.End.Column {
		return start
	}
	return fmt.Sprintf("%s-%d:%d", start, s.End.Line, s.End.Column)
}

// MarshalJSON writes the span as the text [Span.String] renders.
//
// It is a string rather than the two nested objects the fields would marshal
// to, because the object form is the single most expensive thing the engine
// writes: eight keys and two copies of the path for one location, repeated on
// every claim of every answer. docs/token-budget.md priced it at roughly half
// of what `dfcad get` costs and a third of `dfcad resolve`, for a reader that
// wants a file and a line.
//
// The byte offsets are not written. They are a convenience for a tool which is
// holding the source bytes, and a tool holding the bytes is one line index away
// from recovering them; every other reader — a person, an editor, an agent
// deciding whether to open the file — wants path:line:column and pays for the
// offsets on every span it never reads.
func (s Span) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// SpanTextError is a JSON span which is not in the form [Span.MarshalJSON]
// writes.
type SpanTextError struct {
	// Text is the string that could not be read as a span.
	Text string
}

// Error implements [error].
func (e SpanTextError) Error() string {
	return fmt.Sprintf(
		"expected a span written as path:line:column or path:line:column-line:column, found %q",
		e.Text,
	)
}

// UnmarshalJSON reads the text form back.
//
// What comes back carries the paths, lines and columns and leaves both offsets
// zero, because the text form does not write them. That is a documented
// property of the encoding rather than an oversight: a span is round-tripped
// through JSON to say where something is, and where something is has never been
// the offset.
func (s *Span) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}

	path, start, end, ok := parseSpanText(text)
	if !ok {
		return SpanTextError{Text: text}
	}

	*s = Span{
		Start: Position{Path: path, Line: start.line, Column: start.column},
		End:   Position{Path: path, Line: end.line, Column: end.column},
	}

	return nil
}

// lineColumn is one end of a parsed span text.
type lineColumn struct {
	line   int
	column int
}

// parseSpanText splits the text form into the path and its two ends.
//
// It reads from the right rather than from the left, because a path holds
// colons on Windows and dashes everywhere, and only the numbers at the end are
// unambiguous.
func parseSpanText(text string) (path string, start, end lineColumn, ok bool) {
	rest := text

	if head, tail, found := cutLast(rest, "-"); found {
		if line, column, valid := splitLineColumn(tail); valid {
			end, rest = lineColumn{line: line, column: column}, head
		}
	}

	head, tail, found := cutLast(rest, ":")
	if !found {
		return "", start, end, false
	}
	column, err := strconv.Atoi(tail)
	if err != nil || column < 1 {
		return "", start, end, false
	}

	path, tail, found = cutLast(head, ":")
	if !found || path == "" {
		return "", start, end, false
	}
	line, err := strconv.Atoi(tail)
	if err != nil || line < 1 {
		return "", start, end, false
	}

	start = lineColumn{line: line, column: column}
	if end == (lineColumn{}) {
		end = start
	}

	return path, start, end, true
}

// splitLineColumn reads a bare line:column pair.
func splitLineColumn(text string) (line, column int, ok bool) {
	head, tail, found := strings.Cut(text, ":")
	if !found {
		return 0, 0, false
	}

	line, err := strconv.Atoi(head)
	if err != nil || line < 1 {
		return 0, 0, false
	}

	column, err = strconv.Atoi(tail)
	if err != nil || column < 1 {
		return 0, 0, false
	}

	return line, column, true
}

// cutLast is [strings.Cut] around the last occurrence of sep rather than the
// first.
func cutLast(text, sep string) (before, after string, found bool) {
	i := strings.LastIndex(text, sep)
	if i < 0 {
		return text, "", false
	}
	return text[:i], text[i+len(sep):], true
}

// lineIndex answers where a byte offset is, and where a position the underlying
// parser reported lands in the bytes.
//
// The parser reports a line and a column and never an offset, and the loader
// promises both, so one table of line starts sits between them.
type lineIndex struct {
	path string

	// starts holds the byte offset of the first byte of each line. Line n is
	// starts[n-1], so the slice is never empty: an empty file has one line.
	starts []int

	// length is the size of the file in bytes, and therefore the one offset
	// past its last byte.
	length int
}

// newLineIndex builds the index for src.
//
// Only a line feed starts a new line. A lone carriage return is whitespace and
// is not a terminator, which is what the specification says and what the
// underlying tokenizer does, so a file written with them reports every position
// on line 1 in both places rather than in only one of them.
func newLineIndex(path string, src []byte) lineIndex {
	starts := make([]int, 1, bytes.Count(src, []byte{'\n'})+1)
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return lineIndex{path: path, starts: starts, length: len(src)}
}

// at returns the Position of a byte offset. An offset outside the file is
// clamped into it, so a Position always names somewhere the file actually has.
func (x lineIndex) at(offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > x.length {
		offset = x.length
	}

	// Search yields the number of line starts at or before offset, which is the
	// 1-based line number.
	line := sort.Search(len(x.starts), func(i int) bool { return x.starts[i] > offset })

	return Position{
		Path:   x.path,
		Line:   line,
		Column: offset - x.starts[line-1] + 1,
		Offset: offset,
	}
}

// offsetOf returns the byte offset of a position the underlying parser
// reported, clamped into the file.
func (x lineIndex) offsetOf(pos sexpr.Pos) int {
	if pos.Line < 1 {
		return 0
	}
	if pos.Line > len(x.starts) {
		return x.length
	}
	return x.starts[pos.Line-1] + pos.Column - 1
}

// position converts a position the underlying parser reported into a full one.
//
// The line and column are recomputed from the offset rather than copied, so a
// position the parser reported outside the file comes back naming a real place
// instead of one that cannot be pointed at.
func (x lineIndex) position(pos sexpr.Pos) Position {
	return x.at(x.offsetOf(pos))
}
