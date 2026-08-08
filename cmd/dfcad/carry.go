// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"

	"github.com/z5labs/dfcad"
)

// plan is one piece of a drawing as this export writes it: the ring bounding
// it and the rings taken out of it, both expressed in the frame the file's
// coordinates are in.
//
// It stands in for [dfcad.Piece] rather than reusing it because the corners
// have moved and the piece has not: the nesting of a hole inside its outer
// ring is a fact the region worked out under its own tolerance, and carrying
// the corners across a rigid transform cannot change which ring is inside
// which. Rebuilding a region out of them would re-derive that nesting, which
// is both a second answer to a settled question and a re-ordering of rings the
// artefact is keyed on ([0021](docs/decisions/0021-an-export-is-a-build-output-keyed-by-its-source-digest.md)).
type plan struct {
	// outer is the ring bounding the piece.
	outer []dfcad.Point

	// holes are the rings taken out of it, in the order the region held them.
	holes [][]dfcad.Point
}

// planRings are the rings of a set of pieces, the outer ring of each followed
// by the rings taken out of it — the same order [rings] gives for the pieces
// they were carried from, so what is judged level is what is written.
func planRings(plans []plan) [][]dfcad.Point {
	out := make([][]dfcad.Point, 0, len(plans))

	for _, piece := range plans {
		out = append(out, piece.outer)
		out = append(out, piece.holes...)
	}

	return out
}

// carriage is the transform which expresses one drawing in the frame the
// file's coordinates are written in.
//
// It is a value rather than a call per corner because the answer to "can this
// drawing be carried at all" is a property of the pair of frames and not of
// any one of its corners: asked once, it is one refusal naming the frame
// rather than a refusal per vertex of a shape which was never going to be
// written.
type carriage struct {
	frames *dfcad.Frames

	// from is the frame the drawing was authored in, and to the frame the file
	// is written in.
	from, to dfcad.ID
}

// still reports whether the drawing is already in the frame the file is
// written in, which is the whole of a model authored in one frame.
func (c carriage) still() bool { return c.from == c.to }

// point is one corner carried across.
func (c carriage) point(at dfcad.Point) (dfcad.Point, error) {
	if c.still() {
		return at, nil
	}

	return c.frames.TransformPoint(at, c.from, c.to)
}

// ring is a run of corners carried across.
//
// A corner which cannot be carried refuses the whole run, for the reason
// [dfcad.Region.In] gives: a transform which cannot be applied to one corner of
// a shape cannot be applied to the shape, and carrying the corners it reached
// and leaving the rest would write a shape the model does not hold.
func (c carriage) ring(ring []dfcad.Point) ([]dfcad.Point, error) {
	if c.still() {
		return ring, nil
	}

	out := make([]dfcad.Point, 0, len(ring))

	for _, at := range ring {
		carried, err := c.point(at)
		if err != nil {
			return nil, err
		}
		out = append(out, carried)
	}

	return out, nil
}

// carrying is how one node's drawing reaches the frame the file's coordinates
// are written in, and whether it reaches it at all.
//
// The frames are asked before a corner is moved, so a chain which is not there
// is one diagnostic naming the frame rather than a shape quietly written where
// the model does not put it. That silence is what this exists to end: an
// export which drew the corners as authored and said nothing produced a
// georeferenced file placing a room a couple of million feet from where the
// model puts it, with nothing anywhere saying so
// ([0024](docs/decisions/0024-every-coordinate-in-an-export-is-written-in-the-root-frame.md)).
func (e *exporter) carrying(node *dfcad.SemanticNode, from dfcad.ID) (carriage, bool) {
	if !e.rooted {
		e.refuse(node, fmt.Sprintf(
			"expected the frames of this model to reach one root to express the geometry of %s in, found none: it "+
				"is drawn on %s and nothing relates that frame to a datum", node.ID(), from),
			"every coordinate in the file is written in the coordinates of the frame the chain is rooted at, which "+
				"is the frame a coordinate reference system describes; a model with no root has no such coordinates")
		return carriage{}, false
	}

	carry := carriage{frames: e.graph.Frames(), from: from, to: e.root}
	if carry.still() {
		return carry, true
	}

	// The origin rather than a corner of the shape. What is being asked is
	// whether the two frames are related, which is a question about the chain
	// and has the same answer at every point of it.
	if _, err := carry.point(dfcad.Point{}); err != nil {
		e.uncarried(node, from, err)
		return carriage{}, false
	}

	return carry, true
}

// carriedPieces is a set of pieces of a drawing expressed in the frame the
// file's coordinates are written in.
func (e *exporter) carriedPieces(
	node *dfcad.SemanticNode,
	carry carriage,
	pieces []dfcad.Piece,
) ([]plan, bool) {
	out := make([]plan, 0, len(pieces))

	for _, piece := range pieces {
		outer, err := carry.ring(piece.Outer())
		if err != nil {
			e.uncarried(node, carry.from, err)
			return nil, false
		}

		carried := plan{outer: outer}

		for _, hole := range piece.Holes() {
			moved, err := carry.ring(hole)
			if err != nil {
				e.uncarried(node, carry.from, err)
				return nil, false
			}
			carried.holes = append(carried.holes, moved)
		}

		out = append(out, carried)
	}

	return out, true
}

// carriedRing is one run of a drawing expressed in the frame the file's
// coordinates are written in.
func (e *exporter) carriedRing(
	node *dfcad.SemanticNode,
	carry carriage,
	ring []dfcad.Point,
) ([]dfcad.Point, bool) {
	moved, err := carry.ring(ring)
	if err != nil {
		e.uncarried(node, carry.from, err)
		return nil, false
	}

	return moved, true
}

// sited is where the coordinates this file holds sit in the system the model
// names, which is what IfcMapConversion states.
//
// It is the origin of the frame the coordinates were written in, expressed in
// the frame the system was read off. Both are the root — the geometry is
// carried into it and a system written on any other frame is refused — so the
// answer is nothing, and the point of asking is that nothing is an answer here
// rather than an assumption. An export which wrote the identity without asking
// is what this story is about: the coordinates had not been carried, the
// conversion said they had, and the file placed a room two million feet from
// the model.
//
// The refusal below is unreachable through the command as it stands, for the
// same reason. It is written anyway because what it must not do is the point:
// an export which one day wrote its coordinates in some other frame would
// otherwise mislay the offset silently, which is the exact failure this whole
// record exists to end.
func (e *exporter) sited(placed *recordedCRS) dfcad.Point {
	if placed == nil || !e.rooted {
		return dfcad.Point{}
	}

	at, err := e.graph.Frames().TransformPoint(dfcad.Point{}, e.root, placed.Frame)
	if err != nil {
		e.refuseAt(placed.Span, fmt.Sprintf(
			"expected to express the origin of the frame %s in %s, which names the coordinate reference system, "+
				"found that the two frames are not related: %s", e.root, placed.Frame, err),
			"the conversion into a projected system states where this file's coordinates sit in it; where the two "+
				"frames are unrelated there is no such offset, and writing nought would say they coincide")
		return dfcad.Point{}
	}

	return at
}

// uncarried refuses a drawing the chain of measured transforms does not reach.
//
// It names the frame the drawing was authored in rather than only the node,
// because the frame is what has to be fixed: a shape is drawn where its
// vertices were measured, and a chain which does not reach the root is a
// missing transform between two frames and not a mistake in any shape drawn on
// either of them.
func (e *exporter) uncarried(node *dfcad.SemanticNode, from dfcad.ID, err error) {
	e.refuse(node, fmt.Sprintf(
		"expected to express the geometry of %s in the frame %s to write it, found that it is drawn on %s and the "+
			"two frames are not related: %s", node.ID(), e.root, from, err),
		"two frames are related by the chain of measured transforms between them; where there is no chain there is "+
			"no answer, and a coordinate carried across unchanged would be in neither frame")
}
