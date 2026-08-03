// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"fmt"
	"sort"

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
type Span struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
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
