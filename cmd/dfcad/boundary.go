// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"

	"github.com/z5labs/dfcad"
	"github.com/z5labs/dfcad/ifc"
)

// bounding is the space boundary relationships one space states: one per
// element backing an edge of its outline.
//
// This is the one place in the export where the engine already holds something
// a receiving system usually has to guess at. Two rooms either side of a
// partition reference one edge, that edge names the wall which realises it,
// and so the fact that the wall separates those two rooms is stated rather
// than recovered by comparing outlines and hoping the arithmetic agrees.
//
// One relationship is written per backing element rather than one per edge. An
// edge realised by a stud partition and the glazed screen above it is backed
// by two elements, and IfcRelSpaceBoundary names exactly one — so writing one
// of them would answer with half the wall.
func (e *exporter) bounding(node *dfcad.SemanticNode, drawn dfcad.RegionTessellation) []ifc.SpaceBoundary {
	var out []ifc.SpaceBoundary

	for classified := range e.graph.Boundaries().Classify(node) {
		edge := classified.Edge()

		// The switch is over the computed classification and nothing else,
		// which is the whole of how a boundary is decided here. Nothing in the
		// model says whether an edge is physical — there is no flag for it on
		// any form — so adding a wall moves a boundary from a stated gap to a
		// written relationship with no second edit
		// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
		switch classified.Classification() {
		case dfcad.ClassificationUnresolved:
			// The edge names elements the model does not hold, which
			// [dfcad.ResolveBoundaries] already reported as a load error — so
			// the command refuses the model before reaching this, and the arm
			// is unreachable through it. It is written out anyway because what
			// it must not do is the point: the edge is not virtual, it says
			// something backs it, and answering here as though it were would
			// be exactly the silent reclassification that load error exists to
			// prevent.
			continue

		case dfcad.ClassificationVirtual:
			if e.between(node, edge) {
				e.inexpressible(node, edge)
			}

		case dfcad.ClassificationPhysical:
			for _, element := range classified.Backing() {
				boundary, expressible := e.boundary(node, edge, element, drawn)
				if !expressible {
					continue
				}
				out = append(out, boundary)
			}
		}
	}

	return out
}

// between reports whether the edge is a boundary between node and something
// else, rather than an edge which only node reaches.
//
// It is what decides whether an unbacked edge is worth reporting, and the
// distinction is not a matter of degree. An edge two rooms reach is an
// adjacency this engine computed and holds: the two are next to each other,
// nothing is built between them, and IfcRelSpaceBoundary cannot say so — which
// is a gap in the artefact and has to be said. An edge one room reaches is a
// run of its outline the model has said nothing about at all; there is no
// boundary between two things to leave out, and reporting one per edge would
// put a warning on every wall of every plan-only model in the corpus.
//
// The reverse direction is written nowhere in the model and is exactly what
// [dfcad.Boundaries] indexes ([0001](docs/decisions/0001-two-node-families.md)).
func (e *exporter) between(node *dfcad.SemanticNode, edge *dfcad.Edge) bool {
	for region := range e.graph.Boundaries().Regions(edge) {
		if region != node {
			return true
		}
	}

	return false
}

// boundary is one space, one edge and one element which backs it, as the
// relationship between them.
func (e *exporter) boundary(
	node *dfcad.SemanticNode,
	edge *dfcad.Edge,
	element *dfcad.SemanticNode,
	drawn dfcad.RegionTessellation,
) (ifc.SpaceBoundary, bool) {
	// An element nothing spatial contains is written nowhere: IFC has no place
	// for a product outside the spatial structure. A boundary naming it would
	// be a reference to an object the file does not hold, so it is left out
	// and said, exactly as an unbacked edge is.
	if !e.written[element.ID()] {
		e.unheld(node, edge, element)
		return ifc.SpaceBoundary{}, false
	}

	return ifc.SpaceBoundary{
		GlobalID: e.identify(dfcad.ID(
			"ifc/boundary/" + node.ID() + "/" + edge.ID() + "/" + element.ID())),
		Name:     string(edge.ID()),
		Element:  e.identify(element.ID()),
		Physical: ifc.PhysicalBoundary,
		Internal: e.enclosure(node, element),
		// The curve is what the drawing produced for this edge, and is absent
		// on a run which drew nothing.
		Connection: connection(drawn, edge),
	}, true
}

// enclosure is whether the space is bounded by the element from inside the
// building or from outside it.
//
// It is read off the containment the model already states and off nothing
// else. A space and an element the model puts in one building have something
// built between them and the weather, so the boundary between them is
// internal; an element the model puts elsewhere — on the site rather than in a
// building, or in another building — is between the space and the outside.
//
// A space no building contains is an outdoor one: a yard, a plot, a terrace
// standing on a site. Every boundary of it is external, which is what the
// containment says and is the answer whatever backs its edges.
//
// Nothing here reads a type, a name or a geometry. Those would each be a
// second source for an answer the containment already gives, and the two would
// disagree the first time somebody moved a wall without renaming it.
func (e *exporter) enclosure(node, element *dfcad.SemanticNode) ifc.InternalOrExternal {
	within, held := e.enclosing(node)
	if !held {
		return ifc.BoundaryExternal
	}

	holding, held := e.enclosing(element)
	if !held || holding != within {
		return ifc.BoundaryExternal
	}

	return ifc.BoundaryInternal
}

// enclosing is the building one node sits in, and whether it sits in one.
//
// The walk is up the containment chain rather than one step, for the reason
// [exporter.spatialParent]'s is: a wall inside a room inside a storey is in
// the building the storey is in. It is bounded by the number of nodes, which
// is what stops a containment cycle turning into a hang.
func (e *exporter) enclosing(node *dfcad.SemanticNode) (dfcad.ID, bool) {
	seen := 0

	for {
		if node.Kind() == dfcad.KindBuilding {
			return node.ID(), true
		}

		within, ok := node.Within()
		if !ok {
			return "", false
		}

		parent, held := e.graph.Node(within)
		if !held || parent.Retired() {
			return "", false
		}

		node = parent

		seen++
		if seen > e.graph.Nodes().Len() {
			return "", false
		}
	}
}

// connection is the curve where a space meets the element backing one of its
// edges, and is nil where the run drew nothing.
//
// It is cut out of the drawing the footprint was made from rather than built
// from the edge's own vertices, which is what the attribution
// ([dfcad.BoundarySegment]) is for: the runs of the outline this edge produced
// are already known where the boundary was assembled, and re-deriving them by
// matching coordinates is exactly the re-derivation this engine exists to
// prevent.
//
// A curved edge contributes the chords which stand in for its arc, which is
// the same drawing the footprint beside it carries and is stated to the same
// chord tolerance. The alternative — the straight line between the edge's two
// ends — would be an approximation this command chose, sitting in a file whose
// outline disagrees with it.
//
// The curve is two dimensional for the reason the footprint is: it is
// expressed in the plane the space is drawn in, and the elevation is carried
// by the placement rather than repeated on every point.
func connection(drawn dfcad.RegionTessellation, edge *dfcad.Edge) *ifc.ConnectionCurve {
	var points []ifc.Point2D

	for _, segment := range drawn.Segments() {
		if segment.Edge() != edge {
			continue
		}

		from, to := flat(segment.From()), flat(segment.To())
		if len(points) == 0 || points[len(points)-1] != from {
			points = append(points, from)
		}
		points = append(points, to)
	}

	// A run which drew nothing, and an edge the drawing attributed nothing to,
	// both come back with no curve at all. The attribute is optional, so a
	// boundary without one is a boundary stated logically — which is what a
	// topological engine should prefer anyway.
	if len(points) < 2 {
		return nil
	}

	return &ifc.ConnectionCurve{OnRelating: ifc.Polyline{Points: points}}
}

// flat is one corner of the drawing in the plane the space is drawn in.
func flat(at dfcad.Point) ifc.Point2D {
	return ifc.Point2D{X: at[0], Y: at[1]}
}

// inexpressible reports a boundary IfcRelSpaceBoundary cannot carry: an edge
// with nothing built along it.
//
// The relationship's related building element is mandatory, so a boundary
// between two rooms with nothing between them has no relationship to be
// written as. IFC's own answer to that is an IfcVirtualElement — an element
// which is not there — and writing one would be this command putting a thing
// into the model that the model does not hold, which is the one thing an
// export must never do
// ([0020](docs/decisions/0020-export-is-a-boundary-and-the-closed-set-is-what-crosses-it.md)).
//
// So it is left out, and saying so is the point. A warning rather than an
// error, because the file is still correct and is still worth having: what it
// is not is complete, and a gap somebody is told about is one they can act on,
// where a gap they find by counting walls in the receiving system is not.
func (e *exporter) inexpressible(node *dfcad.SemanticNode, edge *dfcad.Edge) {
	e.warn(node, edge, fmt.Sprintf(
		"expected an element backing every edge of %s to write it as a space boundary, found %s, which nothing backs: "+
			"the relationship names the element between the two sides and there is none",
		node.ID(), edge.ID()),
		"name what realises the edge, written (backed-by <element-id>) on it; a boundary with nothing built along it is "+
			"an open line between two rooms, which this schema has no relationship for")
}

// unheld reports a boundary whose backing element is not in the file.
//
// It is the same gap as an unbacked edge seen from the other side: the model
// says what separates the two rooms, and the file cannot say it, because an
// element outside the spatial structure is written nowhere for a relationship
// to point at.
func (e *exporter) unheld(node *dfcad.SemanticNode, edge *dfcad.Edge, element *dfcad.SemanticNode) {
	e.warn(node, edge, fmt.Sprintf(
		"expected the element backing %s of %s to be one this file holds, found %s, which nothing spatial contains",
		edge.ID(), node.ID(), element.ID()),
		"contain the element in the space, storey or building it stands in, written (within <node-id>); IFC has no "+
			"place for a product outside the spatial structure and so none for a boundary naming one")
}

// warn records something this format cannot carry about one space's boundary,
// pointing at the space and at the edge it is about.
//
// Both ends are named because either may be the thing to fix: the edge is
// where a backing is written, and the space is what the reader of the file
// will notice is short of a wall.
func (e *exporter) warn(node *dfcad.SemanticNode, edge *dfcad.Edge, message, hint string) {
	e.diags = append(e.diags, dfcad.Diagnostic{
		Severity: dfcad.SeverityWarning,
		Span:     node.Span(),
		Message:  message,
		Hint:     hint,
		Related: []dfcad.RelatedLocation{
			{Span: edge.Span(), Message: "the edge it is written on is declared here"},
		},
	})
}
