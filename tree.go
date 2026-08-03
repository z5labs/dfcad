// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	sexpr "github.com/z5labs/sexpr-go"
)

// File is one loaded source file: the datums it holds, each carrying the span
// of source text it was written in.
//
// A File says nothing about what those datums mean. Whether a top-level datum
// is a well-formed entity form, whether its tag is one the format knows and
// whether its children are the right ones is decided by validation, on this
// tree, and not here.
type File struct {
	// Path is the file these datums were read from, exactly as the loader
	// reached it. It is the Path of every Position in the file.
	Path string

	// Nodes are the top-level datums, in the order they were written. A file
	// holding no datums has none, which is legal and contributes nothing.
	Nodes []*Node

	// Comments are the comments written at the top level of the file, in the
	// order they were written. A comment written inside a list belongs to that
	// list rather than to the file.
	Comments []*Comment
}

// Node is one datum together with the span of source text it was written in.
//
// Every node of every loaded file has a span, which is the whole point of the
// type: a problem found three layers up in the model can still be reported
// against the file, line and column somebody wrote.
type Node struct {
	// Datum is the datum itself, as the underlying S-expression parser
	// produced it. A type switch over it says what the node is and, for an
	// atom, what its decoded value is.
	//
	// Datum carries its own children too, unspanned. Children below is the
	// same sequence, spanned; the two are always the same length and in the
	// same order.
	Datum sexpr.Node

	// Span is the extent of the source text Datum was written in.
	Span Span

	// Children are the datums written inside this one, in source order. An
	// atom has none. A list has one child per element, followed by one more
	// for its tail where it has one. A quote shorthand has exactly one.
	Children []*Node

	// Comments are the comments written directly inside this node, in the
	// order they were written. Only a list can hold any, and they belong to
	// the list rather than to any one of its children.
	Comments []*Comment
}

// Comment is one comment together with the span of source text it was written
// in. Text is the comment exactly as it appeared, delimiters included.
type Comment struct {
	Text string
	Span Span
}
