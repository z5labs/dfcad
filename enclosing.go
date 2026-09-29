// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"cmp"
	"iter"
	"slices"
)

// Enclosing returns the entity whose declaring form encloses span — the node,
// vertex, edge or loop written around it — and whether the model holds one.
//
// It is the step from where a diagnostic points to what that place is. A
// diagnostic carries a span because every diagnostic has to say where it is;
// what it is about is then a fact about the model rather than something each
// place which builds one has to remember to write down. A span inside a claim is
// inside the node the claim is written on, because a claim is written inside
// its subject's form.
//
// A span is matched by its path and its lines and columns, and never by its
// byte offsets, so a span read back out of JSON — which carries no offsets —
// finds the same entity as the one the diagnostic was built with. A span
// enclosed by a form starts at or after the form's first byte and ends at or
// before one past its last; an empty span, which points at a place rather than
// covering one, has to be before the form's end.
//
// Nothing is returned for a span in a registry file, since a registry declares
// vocabulary rather than things; for a span between forms or in a comment; and
// for a span in a file the model does not hold.
func (g *Graph) Enclosing(span Span) (Entity, bool) {
	if g == nil {
		return nil, false
	}

	forms := g.forms[span.Start.Path]

	// The forms of one file do not overlap, so the only candidate is the last
	// one which starts at or before the span does.
	after, _ := slices.BinarySearchFunc(forms, span.Start, func(form Entity, at Position) int {
		if compareLineColumn(form.Span().Start, at) <= 0 {
			return -1
		}
		return 1
	})
	if after == 0 {
		return nil, false
	}

	form := forms[after-1]
	if !formEncloses(form.Span(), span) {
		return nil, false
	}

	return form, true
}

// Owners iterates the semantic nodes entity belongs to, which is what a caller
// acting on a diagnostic about it usually acts on: the room missing from the
// sheet rather than the ring it could not draw.
//
//   - A node belongs to itself.
//   - A loop belongs to every node it bounds ([Graph.Bounded]).
//   - An edge belongs to every node whose boundary it is part of
//     ([Graph.Regions]), which is both rooms either side of a shared wall.
//   - A vertex belongs to every node whose boundary's vertices include it — the
//     reverse of [Graph.Vertices], indexed when the families are joined.
//
// The loop and the edge read the same index `traverse bounds` does, so what a
// diagnostic names and what that query answers cannot disagree. Each list is in
// the order the load read the nodes; a thing which bounds nothing belongs to
// nothing, and yields nothing.
func (g *Graph) Owners(entity Entity) iter.Seq[*SemanticNode] {
	if g == nil {
		return sequence[*SemanticNode](nil)
	}

	switch entity := entity.(type) {
	case *SemanticNode:
		if entity == nil {
			return sequence[*SemanticNode](nil)
		}
		return sequence([]*SemanticNode{entity})
	case *Loop:
		return g.Bounded(entity)
	case *Edge:
		return g.Regions(entity)
	case *Vertex:
		return g.Boundaries().corners(entity)
	}

	return sequence[*SemanticNode](nil)
}

// indexForms groups entities by the file each was written in, each file's
// forms ordered by where they start.
func indexForms(entities iter.Seq[Entity]) map[string][]Entity {
	forms := make(map[string][]Entity)
	for entity := range entities {
		path := entity.Span().Start.Path
		if path == "" {
			continue
		}
		forms[path] = append(forms[path], entity)
	}

	for _, inFile := range forms {
		slices.SortStableFunc(inFile, func(a, b Entity) int {
			return compareLineColumn(a.Span().Start, b.Span().Start)
		})
	}

	return forms
}

// formEncloses reports whether span lies within form, by line and column.
func formEncloses(form, span Span) bool {
	if compareLineColumn(span.Start, form.Start) < 0 {
		return false
	}

	if compareLineColumn(span.Start, span.End) == 0 {
		return compareLineColumn(span.Start, form.End) < 0
	}

	return compareLineColumn(span.End, form.End) <= 0
}

// compareLineColumn orders two positions in one file by line and then column,
// ignoring the byte offset, which a span read back out of JSON does not carry.
func compareLineColumn(a, b Position) int {
	return cmp.Or(cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column))
}
