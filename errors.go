// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"

	sexpr "github.com/z5labs/sexpr-go"
)

// EncodingError reports a file that is not valid UTF-8.
//
// The position is the first offending byte rather than the file as a whole,
// because "this file is not UTF-8" is not something anybody can act on and
// "byte 0xff at line 12, column 3" is.
type EncodingError struct {
	// Position is the first byte that is not part of a valid encoding.
	Position Position

	// Byte is that byte.
	Byte byte
}

// Error implements the [error] interface.
func (e EncodingError) Error() string {
	return fmt.Sprintf("%s: invalid UTF-8: byte %#02x begins no valid encoding", e.Position, e.Byte)
}

// ByteOrderMarkError reports a file beginning with a UTF-8 byte order mark.
//
// A mark is a load error rather than something to skip. UTF-8 needs none, it
// is invisible in every editor that would have to remove it, and accepting one
// puts every byte offset in the file three bytes out for anything reading it
// outside the loader.
type ByteOrderMarkError struct {
	// Position is the first byte of the mark, which is the first byte of the
	// file.
	Position Position
}

// Error implements the [error] interface.
func (e ByteOrderMarkError) Error() string {
	return fmt.Sprintf("%s: file begins with a UTF-8 byte order mark, which must be removed", e.Position)
}

// ParseError reports a failure from the underlying S-expression tokenizer or
// parser, placed in a file.
//
// The underlying error already knows the line and the column; what it cannot
// know is which file it was reading or where that lands in the bytes. Err is
// kept rather than flattened into a message so that errors.As still reaches
// the tokenizer's and the parser's own types.
type ParseError struct {
	// Position is where the failure was reported.
	Position Position

	// Err is the failure the tokenizer or the parser reported.
	Err error
}

// Error implements the [error] interface.
func (e ParseError) Error() string {
	return fmt.Sprintf("%s: %v", e.Position, e.Err)
}

// Unwrap returns the underlying failure.
func (e ParseError) Unwrap() error {
	return e.Err
}

// parseError places an error from the underlying tokenizer or parser in a file.
//
// Every failure either of them reports carries a position, and the switch below
// is how each spells it. A failure that somehow carries none is reported at the
// start of the file: a position that is merely coarse is still a position, and
// one that is absent is a diagnostic nobody can follow.
func parseError(lines lineIndex, err error) ParseError {
	var pos sexpr.Pos

	switch err := err.(type) {
	case sexpr.InvalidEscapeError:
		pos = err.Pos
	case sexpr.InvalidNumberError:
		pos = err.Pos
	case sexpr.MaxDepthExceededError:
		pos = err.Pos
	case sexpr.NumberRangeError:
		pos = err.Pos
	case sexpr.UnexpectedCharacterError:
		pos = err.Pos
	case sexpr.UnexpectedEndOfTokensError:
		pos = err.Pos
	case sexpr.UnexpectedTokenError:
		pos = err.Actual.Pos
	case sexpr.UnterminatedCommentError:
		pos = err.Pos
	case sexpr.UnterminatedStringError:
		pos = err.Pos
	default:
		pos = sexpr.Pos{Line: 1, Column: 1}
	}

	return ParseError{Position: lines.position(pos), Err: err}
}
