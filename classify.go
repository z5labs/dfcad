// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"iter"
	"slices"
)

// backedByChild is the child an edge writes to name an element which physically
// realises it, per specification section 6.3.
const backedByChild = "backed-by"

// backedByHint is what a diagnostic about a `backed-by` carries: how the
// reference is written, which is what the author needs where the id names the
// wrong thing.
const backedByHint = "an edge names what physically realises it, written (backed-by <element-id>) and naming a semantic " +
	"node of kind Element; an edge which names none is a virtual boundary"

// backingReference is one `(backed-by <element-id>)` as it was written: which
// edge wrote it, which id it names, where that id was written, and where the
// edge itself is named.
//
// The spans are what the diagnostics point at, and an [Edge] carries none of
// them: it holds the ids of the elements which back it, which is what a caller
// reads, rather than the positions the children were written at.
type backingReference struct {
	// edge is the edge which wrote it.
	edge *Edge

	// element is the id it names.
	element ID

	// at is where that id was written, which is what a diagnostic about the
	// element it names points at.
	at Span

	// where is where the edge itself is named, which is the other end such a
	// diagnostic points back at.
	where Span
}

// Classification is what an edge of a region's boundary separates it by: an
// element which physically realises the edge, or nothing at all.
//
// It is computed from the model every time it is asked and is stored nowhere.
// Nothing in the format says which an edge is — there is no flag for it on any
// form — so adding a wall makes the boundary physical with no second edit and
// the answer cannot drift away from what the model says
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
type Classification string

// The three answers the classification has.
//
// Two of them are the question — an edge is backed by something real, or it is
// the open line between a foyer and a dining room. The third is what an edge
// which names a backing element the model does not hold gets: that is a load
// error, and reporting it as virtual would be the silent reclassification the
// error exists to prevent.
const (
	// ClassificationVirtual is an edge which names no backing element. The open
	// line between a foyer and a dining room is one, and it is as ordinary as a
	// wall.
	ClassificationVirtual Classification = "virtual"

	// ClassificationPhysical is an edge at least one of whose backing elements
	// resolved. [BoundaryEdge.Backing] names them.
	ClassificationPhysical Classification = "physical"

	// ClassificationUnresolved is an edge which names backing elements and
	// reached none of them. The edge is not virtual — it says something backs
	// it — and it is not physical either, because nothing in the model is what it
	// names. [ResolveBoundaries] reports each of its references as a load error.
	ClassificationUnresolved Classification = "unresolved"
)

// BoundaryEdge is one edge of a region's boundary, classified.
//
// It pairs the edge with the elements which physically realise it, which is
// everything the classification is computed from. The classification itself is
// not a field: it is [BoundaryEdge.Classification], computed from the ids the
// edge wrote and the elements they reached, so that there is one rule and
// nowhere for a second copy of the answer to go stale
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// The zero value is no edge, which classifies as virtual: an edge which names
// nothing is backed by nothing.
type BoundaryEdge struct {
	// edge is the edge which was classified.
	edge *Edge

	// backing is the elements it named which resolved, in the order it named
	// them.
	backing []*SemanticNode
}

// Edge returns the edge which was classified.
func (b BoundaryEdge) Edge() *Edge { return b.edge }

// Backing returns the elements which physically realise the edge, in the order
// the edge named them.
//
// Every one of them is named rather than only the first. An edge realised by a
// stud partition and the glazed screen above it is backed by two elements, both
// of which are what somebody asking what separates the rooms is asking about,
// and picking one of them would answer with half the wall.
//
// It is empty for a virtual edge, and empty for one whose references did not
// resolve — which [BoundaryEdge.Classification] tells apart.
func (b BoundaryEdge) Backing() []*SemanticNode { return slices.Clone(b.backing) }

// Classification reports whether the edge is backed by a real element.
//
// The rule is the whole of it, and is specification section 6.3: an edge with at
// least one resolving `backed-by` is physical, one which wrote none is virtual,
// and one which wrote some and reached none of them is unresolved. Nothing is
// read from a stored flag, because there is none to read.
//
// An edge which named two elements and reached one of them is physical, because
// something real does back it. The reference which failed is still a load error
// and is still reported; what it is not is a reason to answer a question the
// model did settle with the answer to one it did not.
func (b BoundaryEdge) Classification() Classification {
	var written int
	if b.edge != nil {
		written = len(b.edge.backing)
	}

	switch {
	case written == 0:
		return ClassificationVirtual
	case len(b.backing) == 0:
		return ClassificationUnresolved
	default:
		return ClassificationPhysical
	}
}

// Physical reports whether the edge is backed by at least one element the model
// holds, which is [ClassificationPhysical].
func (b BoundaryEdge) Physical() bool { return b.Classification() == ClassificationPhysical }

// Classified returns edge with its classification and the elements which back
// it.
//
// The answer is computed here and is held nowhere: ask it again after adding an
// element and it is a different answer, which is the point
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// An edge this join never resolved — one from another model, or one asked of a
// [Boundaries] which resolved nothing — classifies from what the edge itself
// wrote. An edge which named no element is virtual; one which named any is
// unresolved, because nothing here reached them.
func (b *Boundaries) Classified(edge *Edge) BoundaryEdge {
	return BoundaryEdge{edge: edge, backing: b.index().backing[edge]}
}

// Classify iterates the edges of region's boundary, each classified, in the
// order [Boundaries.Edges] gives them and each yielded once.
//
// This is the question "what actually separates these two rooms", asked of a
// region rather than of an edge. The answer is computed from whether each edge
// names an element the model holds, so it cannot disagree with the model: adding
// a wall moves an edge from virtual to physical with no edit to the region at
// all.
//
// The order is the order the region's loops traverse its edges, which is
// deterministic, so anything built from it diffs against the last run's.
func (b *Boundaries) Classify(region *SemanticNode) iter.Seq[BoundaryEdge] {
	return func(yield func(BoundaryEdge) bool) {
		for _, edge := range b.index().edges[region] {
			if !yield(b.Classified(edge)) {
				return
			}
		}
	}
}

// back checks every `backed-by` the geometric family wrote and indexes the ones
// which reach an element.
func (l *boundaryLoader) back() {
	if l.topology == nil {
		return
	}

	// named is where each edge first named each element, which is what a
	// repeated backing points its reader back at.
	type naming struct {
		edge    *Edge
		element ID
	}
	named := make(map[naming]Span, len(l.topology.backings))

	for _, written := range l.topology.backings {
		if first, ok := named[naming{edge: written.edge, element: written.element}]; ok {
			l.add(Diagnostic{
				Severity: SeverityError,
				Span:     written.at,
				Message: fmt.Sprintf(
					"expected an element %s does not already name, found %s a second time",
					geometricName(edgeTag, written.edge.id), written.element,
				),
				Hint:    "a backing is a reference and not a count; naming an element twice says exactly what naming it once says",
				Related: []RelatedLocation{{Span: first, Message: "first named here"}},
			})
			continue
		}
		named[naming{edge: written.edge, element: written.element}] = written.at

		element, ok := l.element(written)
		if !ok {
			continue
		}

		l.boundaries.backing[written.edge] = append(l.boundaries.backing[written.edge], element)
	}
}

// element resolves one `backed-by` to the element it names, reporting the ways
// it fails to reach one.
//
// The three are one diagnostic with three messages rather than three
// diagnostics because they are one mistake: the reference does not name an
// element. Which of the three it is decides what the author is told to look at —
// a name nothing holds is a typo or a deletion, a name the geometric family
// holds is a reference to a shape rather than to a thing, and a node of the
// wrong kind is a reference to the wrong node.
//
// A node whose kind could not be read is taken as the element it says it is.
// That it wrote no kind is already a diagnostic naming it, and a second one
// saying the edge which references it is not backed by an Element reports one
// mistake twice, in the vocabulary of a different one.
func (l *boundaryLoader) element(written backingReference) (*SemanticNode, bool) {
	diag := Diagnostic{
		Severity: SeverityError,
		Span:     written.at,
		Hint:     backedByHint,
		Related:  []RelatedLocation{{Span: written.where, Message: "the edge it is written on is named here"}},
	}

	if declared, ok := l.topology.definitionOf(written.element); ok {
		diag.Message = fmt.Sprintf(
			"expected an element id, found %s, which is %s %s",
			written.element, article(declared.tag), declared.tag,
		)
		diag.Related = append([]RelatedLocation{
			{Span: declared.at, Message: fmt.Sprintf("the %s it names is written here", declared.tag)},
		}, diag.Related...)

		l.add(diag)
		return nil, false
	}

	element, ok := l.nodes.Node(written.element)
	if !ok {
		diag.Message = fmt.Sprintf(
			"expected an element id something in this model holds, found %s, which names no node",
			written.element,
		)

		l.add(diag)
		return nil, false
	}

	if element.kind == "" || element.kind == KindElement {
		return element, true
	}

	diag.Message = fmt.Sprintf(
		"expected a node of kind %s, found %s, which is %s",
		KindElement, written.element, kindName(element.kind),
	)
	diag.Related = append([]RelatedLocation{
		{Span: l.nodes.named(element), Message: fmt.Sprintf("the %s named as the backing element is written here", element.kind)},
	}, diag.Related...)

	l.add(diag)
	return nil, false
}
