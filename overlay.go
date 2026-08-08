// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// Containment is how one region sits against another.
//
// It is the answer to a fitting question — is the desk in the room, does the
// setback reach the boundary, do these two zones overlap — and it is one of six
// states rather than a yes and a no, because "not inside" covers a region which
// is nowhere near, one which straddles the boundary and one which is inside and
// touching. Those need different work done about them.
type Containment string

const (
	// ContainmentDisjoint is two regions with nothing in common: no shared area
	// and no shared boundary.
	ContainmentDisjoint Containment = "disjoint"

	// ContainmentTouching is two regions which meet along a boundary or at a
	// corner and enclose no area between them.
	//
	// Two rooms either side of a party wall are this, and so are two plots
	// sharing a fence line. It is deliberately not "overlapping": nothing is in
	// both, and a clearance computed as though something were would report a
	// conflict where the model has none.
	ContainmentTouching Containment = "touching"

	// ContainmentOverlapping is two regions which share area, with each
	// reaching outside the other.
	ContainmentOverlapping Containment = "overlapping"

	// ContainmentInside is the other region entirely within this one.
	//
	// Touching the inside of this one's boundary does not stop it being inside:
	// a room whose wall is the building's wall is in the building. What takes it
	// out is reaching across the boundary, which is [ContainmentOverlapping].
	ContainmentInside Containment = "inside"

	// ContainmentSurrounds is this region entirely within the other one, which
	// is [ContainmentInside] read the other way round.
	ContainmentSurrounds Containment = "surrounds"

	// ContainmentCoincident is two regions covering the same area.
	ContainmentCoincident Containment = "coincident"
)

// String returns the containment as it is written.
func (c Containment) String() string { return string(c) }

// Piece is one connected part of a region: a ring bounding it and the rings
// taken out of it.
//
// Both are needed and neither is derivable from the other. An operation over
// two rooms can leave two pieces which do not touch — a corridor cut in half by
// a lift core — and it can leave one piece with a courtyard in it. A result
// type which could only say "these points" would have to pick one of those to
// be unable to represent.
//
// The rings are closed without repeating their first point at the end, the
// outer one runs anticlockwise seen from the region's own normal and the holes
// run clockwise.
type Piece struct {
	// outer is the ring bounding the piece.
	outer []Point

	// holes are the rings taken out of it.
	holes [][]Point

	// area is what it encloses once the holes are taken away.
	area float64
}

// Outer returns the ring bounding the piece.
func (p Piece) Outer() []Point { return slices.Clone(p.outer) }

// Holes returns the rings taken out of the piece.
func (p Piece) Holes() [][]Point {
	holes := make([][]Point, 0, len(p.holes))
	for _, hole := range p.holes {
		holes = append(holes, slices.Clone(hole))
	}
	return holes
}

// Area returns what the piece encloses once its holes are taken away, in the
// square of the region's unit.
func (p Piece) Area() float64 { return p.area }

// Region is the area a thing covers, as a plane figure operations can be run
// over.
//
// It is derived from the graph and never written back to it
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)): a
// region is read out of the loops bounding a node every time it is asked for,
// so an offset or an overlay cannot disagree with the geometry it was computed
// from.
//
// Everything about it is in one frame and one unit, and nothing here converts
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)). Two regions in
// different frames are refused rather than combined; [Region.In] is how one is
// brought into the other's frame, and it is a step a caller takes deliberately
// because the transform between two frames is a measurement with an accuracy of
// its own and not a detail an operation should apply behind a caller's back.
//
// Every operation carries the accuracy of the position claims the operands were
// read from ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)), so an
// overlap comes back knowing how well the corners which decided it were known.
//
// The zero Region covers nothing, is in no frame, and every operation over it
// reports that rather than answering.
type Region struct {
	// subject is the id of the node the region was read from, and derived
	// whether operations have been run over it since.
	subject ID
	derived bool

	// span is where a diagnostic about the region as a whole points.
	span Span

	// frame is the frame it is declared in and unit that frame's linear unit.
	frame ID
	unit  Unit

	// tolerance is the declared tolerance coincidence and snapping are judged
	// against. Whether it could be applied here at all is ready below, because
	// there is nothing to operate on without it.
	tolerance Tolerance

	// basis is the plane the figure lies in and the orthonormal axes of that
	// plane which every operation is computed in.
	basis plane

	// ready is whether there is a figure to operate on: a frame with a unit, a
	// tolerance declared in that unit, and a plane read off geometry which
	// loaded.
	ready bool

	// dimension is how many components the positions it was read from were
	// written with.
	dimension int

	// pieces are what it covers.
	pieces []Piece

	// segments are the straight runs of the boundary it was read from, in the
	// order the loops traverse them, each paired with the edge it was written
	// as.
	//
	// They are what a derivation which has to treat one edge differently from
	// another is computed from — a setback per edge is the case this exists for
	// — and they are what [Region.Segments] reports.
	//
	// A region an operation produced carries none, and that includes
	// [Region.In]. The boundary of an intersection runs partly along each
	// operand and partly along where they cross, so attributing any of it to an
	// edge somebody wrote would be a lie the next operation would act on; and a
	// region carried into another frame has corners in that frame and edges
	// whose coordinates were never in it, which is a pair that would drift the
	// moment either was read on its own. Neither is left unanswered:
	// [Region.Segments] reports the runs of such a boundary as
	// [SegmentOriginOperation], which says an operation produced them rather
	// than naming an edge which did not.
	segments []BoundarySegment

	// drawn is what the rings which bend were drawn to on the way to reading
	// it, and how far the worst of those segments falls from the curve it
	// stands in for.
	//
	// It is zero for a region with nothing curved in it and for one read
	// without a chord tolerance at all, which are the same value for the same
	// reason: nothing was approximated, so there is nothing to declare.
	drawn drawing

	// budget is the accumulated accuracy of the position claims behind it.
	budget Budget
}

// SegmentOrigin is what produced one straight run of a region's boundary.
//
// It is what stops an attribution being read as more than it is. A run which is
// an edge and a run which is one chord of the drawing of an arc both name an
// edge, and a consumer which carried the second back into the model as though it
// were the first would be writing a straight wall where a curved one is.
type SegmentOrigin string

// The three things which produce a run of a boundary.
const (
	// SegmentOriginEdge is a run which is an edge, corner to corner as it was
	// written. It is the case an attribution is wanted for: the segment is the
	// edge, and a claim written on the edge is a claim about the segment.
	SegmentOriginEdge SegmentOrigin = "edge"

	// SegmentOriginArc is a chord standing in for part of the arc an edge bends
	// along. It names that edge, which is what makes a drawn boundary
	// attributable at all, and it is not the edge: the run is as close to the
	// curve as the chord tolerance the drawing was made to allowed, and no
	// closer.
	SegmentOriginArc SegmentOrigin = "arc"

	// SegmentOriginOperation is a run an operation produced, which no authored
	// edge is behind. An offset, a union and a region carried into another frame
	// are all this: the boundary is where the operation put it, and naming an
	// edge for it would be a lie the next derivation acts on.
	SegmentOriginOperation SegmentOrigin = "operation"
)

// String returns the origin as it is written.
func (o SegmentOrigin) String() string { return string(o) }

// BoundarySegment is one straight run of a region's boundary: which ring of the
// boundary it belongs to, the two corners it runs between, and what produced it.
//
// It is what an exporter attributes a ring back to the model with. Without it a
// polygon is anonymous coordinates and the correspondence has to be re-derived
// by matching them, which is exactly the re-derivation this engine exists to
// prevent: the pairing is known where the boundary is assembled, so it is
// reported from there rather than reconstructed downstream.
//
// The corners are held as they were read rather than projected into the
// region's plane, for the reason [Region.figure] projects rather than caching a
// projection: one projection, done where it is used, cannot fall out of step
// with the figure it has to line up with.
//
// The zero value is a run from nowhere to nowhere which no edge produced, and
// [BoundarySegment.Origin] reports it as [SegmentOriginOperation].
type BoundarySegment struct {
	// ring is which ring of the boundary the run belongs to, counted from zero
	// in the order the rings come back.
	ring int

	// edge is the edge the run was written as, or which it stands in for, and
	// nil for a run an operation produced.
	edge *Edge

	// bent is whether the run is a chord of the drawing of an arc rather than
	// the edge itself, which is what [BoundarySegment.Origin] tells the two
	// apart by.
	bent bool

	// reversed is whether the run goes against the order the edge was written,
	// which the traversal decides and not the edge.
	reversed bool

	// from and to are the corners it runs between, in the order the loop
	// traversed them.
	from Point
	to   Point
}

// Ring returns which ring of the boundary the run belongs to, counted from zero
// in the order the rings come back.
//
// It is what makes a flat list of runs a boundary: a plate with a courtyard in
// it is two rings, and a consumer which could not tell where one ended and the
// next began would close the outer ring through the hole.
func (s BoundarySegment) Ring() int { return s.ring }

// Edge returns the edge the run was written as, or whose arc it stands in for.
//
// It is nil for a run an operation produced, which
// [BoundarySegment.Origin] reports as [SegmentOriginOperation].
func (s BoundarySegment) Edge() *Edge { return s.edge }

// From returns the corner the run leaves, and [BoundarySegment.To] the one it
// arrives at, in the order the loop traversed them.
func (s BoundarySegment) From() Point { return s.from }

// To returns the corner the run arrives at.
func (s BoundarySegment) To() Point { return s.to }

// Reversed reports whether the run goes against the order the edge was written.
//
// The direction is stated because a loop traverses an edge in either order and
// the answer differs: two rooms either side of a party wall name one edge, and
// the boundary of the second runs through it the other way round. A caller
// which read the edge's own vertices and assumed the run followed them would
// draw one of the two rooms inside out.
//
// It is false for a run an operation produced, which no edge was written for.
func (s BoundarySegment) Reversed() bool { return s.reversed }

// Origin reports what produced the run.
//
// It is computed from what the segment holds rather than stored beside it, for
// the reason [BoundaryEdge.Classification] is: one rule, and nowhere for a
// second copy of the answer to go stale
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
func (s BoundarySegment) Origin() SegmentOrigin {
	switch {
	case s.edge == nil:
		return SegmentOriginOperation
	case s.bent:
		return SegmentOriginArc
	default:
		return SegmentOriginEdge
	}
}

// String renders the run as a person reads it: where it goes, what produced it
// and which way round it ran through the edge.
func (s BoundarySegment) String() string {
	where := fmt.Sprintf("%s to %s", pointText(s.from, 3), pointText(s.to, 3))

	if s.edge == nil {
		return fmt.Sprintf("%s: %s", where, SegmentOriginOperation)
	}

	direction := "forwards"
	if s.reversed {
		direction = "reversed"
	}

	return fmt.Sprintf("%s: %s %s, %s", where, s.Origin(), s.edge.ID(), direction)
}

// RegionOf reads the area a semantic node covers out of the loops bounding it.
//
// It is the entry point to every operation in this file, and it refuses more
// than it accepts. A ring which does not close, one whose corners are not in
// one plane, one which crosses itself and one whose corners are collinear all
// come back as diagnostics and no region — the same diagnostics
// [Topology.MeasureRegion] raises, because they are the same defects and an
// offset computed over any of them would be a shape which looks like an answer.
//
// The tolerance named by the survey has to be one the registry declares in the
// unit of the node's frame. Coincidence is judged against it and offsets are
// drawn to it, so there is no default and no fallback
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
//
// A boundary which bends is refused unless the survey names a chord tolerance
// for it ([Survey.Chord]). An overlay is computed over straight segments, so a
// curve has to become segments somewhere, and where that happens is a decision
// the caller takes and states rather than one this makes on their behalf: a
// survey which names a chord has decided to draw the curve and has said how
// closely, and [Region.ChordTolerance] and [Region.Deviation] carry that
// decision out with the answer. A survey which names none is refused, naming the
// loop which bends.
//
// A node which references no loop is not malformed — a circuit group covers no
// area — and comes back as a region which covers nothing.
func (t *Topology) RegionOf(node *SemanticNode, boundaries *Boundaries, survey Survey) (Region, []Diagnostic) {
	if node == nil {
		return Region{}, nil
	}

	region, drew, diags := t.regionOf(node, boundaries, survey, survey.Chord, survey.Chord != "")
	if drew.drew {
		region.drawn = drew
	}

	return region, diags
}

// drawing is what a region's rings were drawn to on the way to reading it: the
// chord tolerance named on the invocation and the furthest any segment of the
// result falls from the curve it stands in for.
//
// Both are zero for a region read without a name, which is [Topology.RegionOf]:
// nothing was drawn there, because a ring which bends is refused rather than
// approximated.
type drawing struct {
	tolerance Tolerance
	deviation float64

	// drew reports whether a curve was actually drawn, which is a different
	// question from whether a drawing was asked for: a region with nothing
	// curved in it is drawn to itself, and a tolerance reported for it would
	// say a boundary had been approximated which was not.
	drew bool
}

// regionOf reads the area a node covers, drawing every ring which bends to the
// chord tolerance named and refusing one where no drawing was asked for.
//
// Not drawing is [Topology.RegionOf]: an overlay is computed over straight
// segments, and picking a resolution for somebody's boundary on the way to
// answering a question about it is the accident an arc kept as an arc exists to
// prevent. Drawing is [Topology.TessellateRegion], where a caller has decided to
// draw the curve and has said how closely — and a caller which asked to draw and
// named nothing to draw to is refused by [measurer.chord] like any other name
// the registry does not declare, rather than quietly falling back to the
// refusing case.
//
// Everything else is the same pass, which is what makes the two interchangeable:
// the same refusals, the same even-odd nesting, the same orientation and the
// same assembly into pieces. A region with nothing curved in it comes back
// identical whichever way it was asked for, down to the boundary segments.
func (t *Topology) regionOf(
	node *SemanticNode,
	boundaries *Boundaries,
	survey Survey,
	chord string,
	draw bool,
) (Region, drawing, []Diagnostic) {
	var drew drawing

	loops := slices.Collect(boundaries.Loops(node))

	frame := node.frame
	if frame == "" && len(loops) > 0 {
		frame = loops[0].frame
	}

	m := t.measuring(node.id, frame, node.span, survey)

	region := Region{
		subject:   node.id,
		span:      m.span,
		frame:     frame,
		unit:      m.unit,
		tolerance: m.tolerance,
	}

	if len(loops) == 0 {
		// A node which references no loop covers no area and has no curve in it
		// to draw, so the chord tolerance is not read at all: refusing a circuit
		// group for the name of a tolerance nothing here would have applied
		// would be a diagnostic about a model in which nothing is wrong.
		return region, drew, m.diags
	}

	if draw {
		tolerance, ok := m.chord(chord, t.namedAt(node.id, node.span))
		if !ok {
			return region, drew, m.diags
		}
		drew.tolerance = tolerance
	}

	var rings []*outline
	for _, loop := range loops {
		assembled, ok := m.ring(loop)
		if !ok {
			continue
		}
		rings = append(rings, assembled)
	}

	if len(rings) != len(loops) {
		// A region is read from all of its rings or from none of them, for the
		// reason it is measured from all of them or none: an operation over the
		// rings which happened to assemble is an answer about a shape with a
		// piece missing, and nothing in the result says which piece.
		return region, drew, m.diags
	}

	if !m.applicable() {
		m.add(m.untolerated(node))
		return region, drew, m.diags
	}

	var bent bool
	for _, one := range rings {
		if !one.hasArea {
			// Why is already on a diagnostic from the ring itself, in the
			// vocabulary of the shape rather than of the arithmetic over it.
			return region, drew, m.diags
		}

		if !one.curved {
			continue
		}

		if !draw {
			m.add(m.undrawn(node, one))
			return region, drew, m.diags
		}

		bent = true
	}

	drew.drew = bent

	if !m.coplanar(node, rings) {
		return region, drew, m.diags
	}

	basis, ok := planeOf(rings[0].normal, rings[0].points[0])
	if !ok {
		return region, drew, m.diags
	}

	var drawn []BoundarySegment

	figure := make([]contour, 0, len(rings))
	for i, one := range rings {
		points := one.points

		if bent {
			ring, starts, deviation, ok := m.drawnRing(one, drew.tolerance)
			if !ok {
				return region, drawing{tolerance: drew.tolerance}, m.diags
			}

			points = ring
			drawn = append(drawn, drawnSegmentsOf(one, i, ring, starts)...)
			drew.deviation = math.Max(drew.deviation, deviation)
		}

		projected := make(contour, 0, len(points))
		for _, point := range points {
			projected = append(projected, basis.project(point))
		}

		figure = append(figure, projected)
	}

	// A ring inside an odd number of others is a hole and is written clockwise,
	// which is the same even-odd rule a measurement takes its area away by.
	// Nothing in the model declares which ring is the outside one, and this is
	// where that is worked out rather than asked for.
	for i, depth := range m.nestings(figure, rings, bent) {
		figure[i] = oriented(figure[i], depth%2 == 0)
	}

	region.basis = basis
	region.ready = true
	region.dimension = rings[0].dimension
	region.budget = m.result.budget
	region.pieces = piecesOf(overlay(figure, nil, m.tolerance.Value, coveredAlone), basis)

	// The straight runs are the edges they were written as, and a run of a drawn
	// arc is a chord standing in for part of the edge it was written as rather
	// than that edge. Both name the edge and [BoundarySegment.Origin] is what
	// keeps them apart, which is what lets a drawn boundary be attributed at all
	// without the chord being read back as a wall somebody wrote straight. A
	// region with nothing curved in it comes back the same either way, which is
	// what makes [Topology.RegionOf] and [Topology.TessellateRegion] the same
	// value.
	region.segments = drawn
	if !bent {
		region.segments = segmentsOf(rings)
	}

	return region, drew, m.diags
}

// nestings counts, for each ring of a region, how many of the others hold it.
//
// Where nothing bent the count is taken at the corners, which is
// [measurer.nesting] and is the same count [Topology.MeasureRegion] nests by.
// Where something bent it is taken at the segments the curve was drawn as, and
// that is the whole reason a curved region has to be drawn before it can be
// nested at all: a courtyard whose wall bows out past a corner of the plate
// around it is inside the plate and outside the polygon of its chords, so a
// count taken at the corners would flip on which side of a bulge a corner
// happened to fall — a region wrong by a whole ring rather than by a sag, which
// is what [measurer.straight] refuses rather than approximates.
func (m *measurer) nestings(figure []contour, rings []*outline, bent bool) []int {
	depths := make([]int, len(figure))

	for i := range figure {
		if !bent {
			depths[i] = m.nesting(rings, i)
			continue
		}

		for j, other := range figure {
			if j != i && len(figure[i]) > 0 && other.holds(figure[i][0]) {
				depths[i]++
			}
		}
	}

	return depths
}

// segmentsOf is the straight runs of every assembled ring, in the order the
// traversals ran through them.
//
// The run at an index leaves the corner at that index and arrives at the next
// one, which is how an assembled ring is held: the edge at an index is the one
// the traversal left that corner by, and the ring closes back onto its first
// corner. Reading the pair from the same index rather than from the edge's own
// written direction is what makes a ring traversed backwards come out running
// the way the ring does.
func segmentsOf(rings []*outline) []BoundarySegment {
	var segments []BoundarySegment

	for index, one := range rings {
		if len(one.points) != len(one.edges) || len(one.reversed) != len(one.edges) {
			continue
		}

		for i, edge := range one.edges {
			segments = append(segments, BoundarySegment{
				ring:     index,
				edge:     edge,
				reversed: one.reversed[i],
				from:     one.points[i],
				to:       one.points[(i+1)%len(one.points)],
			})
		}
	}

	return segments
}

// drawnSegmentsOf is the straight runs of one drawn ring, each naming the edge
// it was written as or whose arc it stands in for.
//
// The drawing is per step of the traversal — starts says where in the ring each
// step's points begin — so a chord belongs to the step it was drawn for and to
// no other. That is what keeps a curve attributable: sixteen chords of one arc
// all name the edge which bends along it, and none of them is mistaken for the
// straight edge on either side of it.
//
// The ring closes, so the last run of the last step arrives back at the first
// corner. Only a closed ring reaches here: a ring which does not close encloses
// no area, and [Topology.regionOf] has already refused it.
func drawnSegmentsOf(one *outline, index int, points []Point, starts []int) []BoundarySegment {
	if len(points) == 0 || len(starts) != len(one.edges) || len(one.reversed) != len(one.edges) {
		return nil
	}

	var segments []BoundarySegment

	for i, edge := range one.edges {
		end := len(points)
		if i+1 < len(starts) {
			end = starts[i+1]
		}

		for at := starts[i]; at < end; at++ {
			segments = append(segments, BoundarySegment{
				ring:     index,
				edge:     edge,
				bent:     one.bends[i] != nil,
				reversed: one.reversed[i],
				from:     points[at],
				to:       points[(at+1)%len(points)],
			})
		}
	}

	return segments
}

// untolerated reports a region whose tolerance cannot be applied to it, which is
// the one thing this file will not proceed without.
func (m *measurer) untolerated(node *SemanticNode) Diagnostic {
	found := fmt.Sprintf("the registry declares no tolerance %q", m.survey.Tolerance)
	switch {
	case m.unit == "":
		found = fmt.Sprintf("the frame %s is not one the registry declares, so nothing here has a unit", m.subject)
	case m.declared:
		found = fmt.Sprintf("the tolerance %s is in %s and the frame is in %s",
			m.tolerance.Name, m.tolerance.Unit, m.unit)
	}

	return Diagnostic{
		Severity: SeverityError,
		Span:     m.topology.namedAt(node.id, node.span),
		Message: fmt.Sprintf(
			"expected a tolerance in the unit of the frame to compute the area %s covers, found that %s",
			nodeName(node), found,
		),
		Hint: "an overlay judges two corners coincident and draws an offset to a distance the project wrote down; " +
			"there is no default for it, and one compiled in here would be the engine deciding how close is close enough",
	}
}

// undrawn reports a region bounded by a ring which bends, which every operation
// in this file is over straight segments and so will not read.
//
// It is a refusal and not a limitation being worked around. An overlay walks
// segments, so reading a curved boundary here would mean tessellating it — and
// choosing a resolution for somebody's boundary in the middle of answering a
// question about whether two rooms overlap is exactly the accident an arc kept
// as an arc exists to prevent. The caller draws the curve deliberately, to a
// tolerance they name, and knows they have.
func (m *measurer) undrawn(node *SemanticNode, one *outline) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Span:     m.topology.namedAt(node.id, node.span),
		Message: fmt.Sprintf(
			"expected either every loop bounding %s to be straight or a chord tolerance to draw them to, found that %s "+
				"bends along an arc and no chord tolerance named",
			nodeName(node), geometricName(loopTag, one.loop.id),
		),
		Hint: "an overlay is computed over straight segments, so the curve has to become segments somewhere; name the " +
			"tolerance it may be drawn to and the answer carries what it was drawn to, rather than having a " +
			"resolution chosen for you here",
	}
}

// Subject returns the id of the node the region was read from.
func (r Region) Subject() ID { return r.subject }

// Frame returns the frame the region is declared in.
func (r Region) Frame() ID { return r.frame }

// Unit returns the linear unit of that frame, which every figure here is in and
// which an area is in the square of.
func (r Region) Unit() Unit { return r.unit }

// Tolerance returns the declared tolerance the region's coincidence and
// snapping are judged against.
func (r Region) Tolerance() Tolerance { return r.tolerance }

// ChordTolerance returns the declared tolerance the rings which bend were drawn
// to on the way to reading the region, and whether any of them was drawn at all.
//
// It travels with the region because the region is an approximation of the
// boundary wherever it is true, and an outline which does not say how closely it
// follows the curve it came from is one nobody downstream can judge. A region
// with nothing curved in it is not an approximation of anything and says so by
// reporting none.
func (r Region) ChordTolerance() (Tolerance, bool) {
	return r.drawn.tolerance, r.drawn.tolerance.Name != ""
}

// Deviation returns how far the worst segment of that drawing falls from the
// curve it stands in for, in [Region.Unit].
//
// It is what was achieved rather than what was asked for, and is always within
// [Region.ChordTolerance]: a curve is divided into a whole number of segments, so
// an arc which needs two and a bit gets three and follows the curve more closely
// than it had to.
func (r Region) Deviation() float64 { return r.drawn.deviation }

// Pieces returns the connected parts the region covers.
//
// There may be none, which is a region covering nothing rather than an error —
// an inward offset which collapsed a room comes back this way, and so does the
// intersection of two rooms which do not meet.
func (r Region) Pieces() []Piece { return slices.Clone(r.pieces) }

// Empty reports whether the region covers nothing.
func (r Region) Empty() bool { return len(r.pieces) == 0 }

// Segments returns the straight runs of the region's boundary, in the order the
// rings are traversed, each saying what produced it.
//
// It is the pairing the boundary was assembled from rather than a
// correspondence worked back out of coordinates. A run of a region read from a
// model is the edge it was written as, or a chord of the drawing of that edge's
// arc, and [BoundarySegment.Reversed] says which way round the loop ran through
// it. That is what lets a ring be attributed back to the model it came from: a
// party wall can be named, an element backing an edge can be carried onto the
// segment that edge produced, and a claim written on an edge reaches the run it
// is about.
//
// A region an operation produced attributes none of its boundary, and says so:
// every run of it comes back as [SegmentOriginOperation] with no edge. The
// boundary of an intersection runs partly along each operand and partly along
// where they cross, and there is no edge which is the second kind — so the
// answer is that an operation put it there, and not the nearest edge which
// nearly did.
//
// The runs are derived from the region every time they are asked for and are
// stored nowhere else
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
func (r Region) Segments() []BoundarySegment {
	if r.derived {
		return r.produced()
	}

	return slices.Clone(r.segments)
}

// produced is the boundary of a region nothing authored: the rings an operation
// left, run corner to corner, each saying that an operation is what produced
// it.
//
// The rings are taken in the order they come back — each piece's outer ring and
// then the rings taken out of it — which is the order [Region.Pieces] reports
// them in, so a caller reading both sees one boundary described twice rather
// than two orderings to reconcile.
func (r Region) produced() []BoundarySegment {
	var segments []BoundarySegment
	var index int

	for _, piece := range r.pieces {
		rings := make([][]Point, 0, 1+len(piece.holes))
		rings = append(rings, piece.outer)
		rings = append(rings, piece.holes...)

		for _, ring := range rings {
			for i, from := range ring {
				segments = append(segments, BoundarySegment{
					ring: index,
					from: from,
					to:   ring[(i+1)%len(ring)],
				})
			}
			index++
		}
	}

	return segments
}

// Area returns the total area covered, holes taken away, in the square of
// [Region.Unit].
func (r Region) Area() float64 {
	var total float64
	for _, piece := range r.pieces {
		total += piece.area
	}
	return total
}

// Bounds returns the axis-aligned bounding box of everything the region covers,
// and whether there was anything to bound.
func (r Region) Bounds() (Box, bool) {
	var points []Point
	for _, piece := range r.pieces {
		points = append(points, piece.outer...)
	}

	if len(points) == 0 {
		return Box{}, false
	}

	box := boxOf(points)
	box.Unit = r.unit

	return box, true
}

// Budget returns the accumulated accuracy of the position claims every operand
// behind the region was read from.
//
// It accumulates across operations: the intersection of two rooms carries the
// claims of both rooms' corners, and a region brought into another frame
// carries the accuracy of the transform which brought it there. A term shared
// between them — one georeference fit behind every indoor corner — is counted
// once, which is [Budget]'s own arithmetic.
func (r Region) Budget() Budget { return r.budget }

// String writes what the region covers, with its unit.
func (r Region) String() string {
	if r.Empty() {
		return fmt.Sprintf("%s: covers nothing", r.name())
	}

	var holes int
	for _, piece := range r.pieces {
		holes += len(piece.holes)
	}

	parts := []string{fmt.Sprintf("area %s%s", decimal(r.Area()), squareSuffix(r.unit))}
	parts = append(parts, plural(len(r.pieces), "piece"))
	if holes > 0 {
		parts = append(parts, plural(holes, "hole"))
	}

	return fmt.Sprintf("%s: %s", r.name(), strings.Join(parts, ", "))
}

// name is how a diagnostic and a rendering name the region.
func (r Region) name() string {
	switch {
	case r.subject == "":
		return "the region"
	case r.derived:
		return fmt.Sprintf("the region derived from %s", r.subject)
	default:
		return string(r.subject)
	}
}

// Union returns everything either region covers.
//
// It is commutative, and not merely in the sense that the areas agree: the
// pieces come back in the same order with their rings written from the same
// corner whichever operand it was called on, because an answer which depended
// on that would be one a caller had to normalise before comparing.
func (r Region) Union(other Region) (Region, []Diagnostic) {
	return r.combine(other, "union", coveredByEither)
}

// Intersect returns everything both regions cover.
//
// Two regions which meet only along a boundary intersect in nothing: a shared
// wall line has no area, and reporting it as an overlap would turn every pair
// of adjacent rooms into a conflict. [Region.Containment] is what tells that
// case apart from two regions which are nowhere near each other.
//
// It is commutative in the same sense [Region.Union] is.
func (r Region) Intersect(other Region) (Region, []Diagnostic) {
	return r.combine(other, "intersection", coveredByBoth)
}

// Difference returns everything this region covers and the other does not.
//
// It is not commutative and does not pretend to be: a room less a desk is a
// room with a gap in it and a desk less a room is nothing.
func (r Region) Difference(other Region) (Region, []Diagnostic) {
	return r.combine(other, "difference", coveredByFirstAlone)
}

// coveredAlone, coveredByEither, coveredByBoth and coveredByFirstAlone are the
// four rules an overlay is walked with.
//
// Each is asked, about one side of one stretch of boundary, whether the answer
// covers that side given which operands do. That is the whole difference
// between the operations in this file: one subdivision, four one-line rules
// over it, and no operation with an algorithm of its own to be separately wrong.
func coveredAlone(a, _ bool) bool        { return a }
func coveredByEither(a, b bool) bool     { return a || b }
func coveredByBoth(a, b bool) bool       { return a && b }
func coveredByFirstAlone(a, b bool) bool { return a && !b }

// combine runs one operation over two regions, having first established that
// the two are operands one operation can be run over at all.
func (r Region) combine(other Region, what string, covered func(a, b bool) bool) (Region, []Diagnostic) {
	if diags := r.comparable(other, what); len(diags) > 0 {
		return Region{}, diags
	}

	result := r.derive()
	result.budget.Merge(other.budget)
	result.pieces = piecesOf(
		overlay(r.figure(r.basis), other.figure(r.basis), r.tolerance.Value, covered),
		r.basis,
	)

	return result, nil
}

// comparable reports why two regions are not operands of one operation, and
// nothing where they are.
//
// Three things have to hold and each is refused rather than worked around. Both
// have to be regions there is a figure to operate on. They have to be in one
// frame, because a coordinate means nothing without the frame it was written in
// and combining two frames' numbers because they are both numbers is the one
// mistake here which produces an answer rather than a failure. And they have to
// lie in one plane, because two floor plates on different storeys are inside
// each other seen from above and are not inside each other.
func (r Region) comparable(other Region, what string) []Diagnostic {
	for _, one := range []Region{r, other} {
		if one.ready {
			continue
		}
		return []Diagnostic{{
			Severity: SeverityError,
			Span:     r.at(one),
			Message: fmt.Sprintf(
				"expected two regions to take the %s of, found that %s covers no area which could be operated on",
				what, one.name(),
			),
			Hint: "a region is read from the loops bounding a node, against a tolerance the registry declares in the " +
				"unit of its frame; where that could not be done the reason is on a diagnostic of its own",
		}}
	}

	if r.frame != other.frame {
		return []Diagnostic{{
			Severity: SeverityError,
			Span:     r.span,
			Message: fmt.Sprintf(
				"expected both operands of the %s to be declared in one frame, found %s in %s and %s in %s",
				what, r.name(), r.frame, other.name(), other.frame,
			),
			Hint: "nothing here converts between frames on its own: the transform between two of them is a measurement " +
				"with an accuracy of its own, so bring one into the other with In and the accuracy comes with it",
		}}
	}

	if r.tolerance.Name != other.tolerance.Name || r.tolerance.Value != other.tolerance.Value {
		return []Diagnostic{{
			Severity: SeverityError,
			Span:     r.span,
			Message: fmt.Sprintf(
				"expected both operands of the %s to be judged against one tolerance, found %s against %s %s and %s against %s %s",
				what, r.name(), decimal(r.tolerance.Value), r.tolerance.Unit,
				other.name(), decimal(other.tolerance.Value), other.tolerance.Unit,
			),
			Hint: "how close two corners have to be to be one corner decides where the boundary of the answer runs; two " +
				"operands judged against different distances would each be right about a different shape",
			Related: []RelatedLocation{
				{Span: r.tolerance.Span, Message: "one tolerance is declared here"},
				{Span: other.tolerance.Span, Message: "the other is declared here"},
			},
		}}
	}

	for _, piece := range other.pieces {
		for _, point := range piece.outer {
			out := r.basis.distance(point)
			if out <= r.tolerance.Value {
				continue
			}

			return []Diagnostic{{
				Severity: SeverityError,
				Span:     r.span,
				Message: fmt.Sprintf(
					"expected both operands of the %s to lie in one plane within the tolerance %s, which is %s %s, found %s %s %s out of the plane of %s",
					what, r.tolerance.Name, decimal(r.tolerance.Value), r.tolerance.Unit,
					other.name(), decimal(out), r.unit, r.name(),
				),
				Hint: "an overlay is a plane figure over a plane figure; two shapes on different storeys are inside each " +
					"other seen from above and are not inside each other",
				Related: []RelatedLocation{
					{Span: r.tolerance.Span, Message: "the tolerance is declared here"},
				},
			}}
		}
	}

	return nil
}

// at is where a diagnostic about an operand of an operation points: at that
// operand where it was written down anywhere, and at the region the operation
// was asked of otherwise.
//
// Which operand a refusal is about is the actionable half of it. A region read
// from a node carries the span of the node, so a refusal about the second
// operand points at the second operand; one derived from an operation carries
// the span of what it was derived from, and a zero region carries none at all,
// which is when this falls back to the receiver.
func (r Region) at(one Region) Span {
	if one.span != (Span{}) {
		return one.span
	}
	return r.span
}

// derive is the region an operation over this one produces, before what it
// covers is filled in.
func (r Region) derive() Region {
	result := r
	result.derived = true
	result.pieces = nil
	result.segments = nil

	result.budget = Budget{}
	result.budget.Merge(r.budget)

	return result
}

// figure is what the region covers, projected into a plane's axes and oriented
// so that what is inside it runs anticlockwise and its holes run clockwise.
//
// The orientation is imposed from what each ring is rather than read back off
// the projection. Two regions in one plane project into the same axes, but a
// ring reduced to a sliver by an operand's own rounding could come back with an
// area of either sign, and a hole which came back anticlockwise would be filled
// in rather than taken out.
func (r Region) figure(basis plane) []contour {
	var figure []contour

	for _, piece := range r.pieces {
		figure = append(figure, oriented(project(piece.outer, basis), true))
		for _, hole := range piece.holes {
			figure = append(figure, oriented(project(hole, basis), false))
		}
	}

	return figure
}

// Buffer returns everything within a distance of the region, or everything far
// enough inside it, according to the sign of the distance.
//
// An outward offset — a positive distance — is the setback question: everything
// within three metres of this boundary. An inward offset is the clearance one:
// everything at least three metres inside it. Both are one construction, which
// is why they cannot disagree at the corners: the boundary is thickened by the
// distance, and an outward offset adds that thickening to the region while an
// inward one takes it away.
//
// An inward offset which eats the region returns a region covering nothing.
// That is the answer — a corridor 1.6 m wide has nothing 1 m clear of both
// walls — and it is why the collapse is not reported as a failure. What is
// never returned is the inside-out shape the arithmetic of offsetting each edge
// on its own produces when the edges cross over each other.
//
// Corners are rounded, and the rounding follows the circle to within the
// declared tolerance rather than to a segment count written down here.
//
// An offset smaller than that tolerance is refused. It is a distance the
// project has said it cannot tell from zero, and an answer to it would be a
// boundary moved by less than the uncertainty of where the boundary is.
func (r Region) Buffer(distance float64) (Region, []Diagnostic) {
	if !r.ready {
		return Region{}, []Diagnostic{{
			Severity: SeverityError,
			Span:     r.span,
			Message: fmt.Sprintf(
				"expected a region to offset, found that %s covers no area which could be operated on", r.name(),
			),
			Hint: "a region is read from the loops bounding a node, against a tolerance the registry declares in the " +
				"unit of its frame; where that could not be done the reason is on a diagnostic of its own",
		}}
	}

	if distance == 0 {
		// Nothing to draw and nothing to overlay: a region offset by nothing is
		// the region, and running it through the arrangement to say so would be
		// a chance for it to come back very slightly different.
		unchanged := r.derive()
		unchanged.pieces = r.Pieces()

		return unchanged, nil
	}

	radius := math.Abs(distance)
	if radius < r.tolerance.Value {
		return Region{}, []Diagnostic{{
			Severity: SeverityError,
			Span:     r.span,
			Message: fmt.Sprintf(
				"expected an offset of %s to be further than the tolerance %s, which is %s %s",
				decimal(distance)+unitSuffix(r.unit), r.tolerance.Name, decimal(r.tolerance.Value), r.tolerance.Unit,
			),
			Hint: "an offset shorter than the distance two corners are the same corner within moves a boundary by less " +
				"than the model knows where it is; the answer would be the tolerance rather than the offset",
			Related: []RelatedLocation{
				{Span: r.tolerance.Span, Message: "the tolerance is declared here"},
			},
		}}
	}

	figure := r.figure(r.basis)
	thickened := thicken(figure, radius, r.tolerance.Value)

	result := r.derive()

	if distance > 0 {
		result.pieces = piecesOf(
			overlay(slices.Concat(figure, thickened), nil, r.tolerance.Value, coveredAlone), r.basis)
		return result, nil
	}

	result.pieces = piecesOf(
		overlay(figure, thickened, r.tolerance.Value, coveredByFirstAlone), r.basis)

	return result, nil
}

// thicken is everything within a radius of a figure's boundary: a slab either
// side of every segment and a disc at every corner.
//
// One construction serves both directions of an offset. Everything within the
// radius of the region is the region with this added to it, and everything that
// far inside the region is the region with this taken away — which is what
// makes an inward offset collapse a shape narrower than twice the radius to
// nothing instead of turning it inside out.
func thicken(figure []contour, radius, tolerance float64) []contour {
	var thickened []contour

	for _, ring := range figure {
		for i, point := range ring {
			if piece, ok := slab(point, ring[(i+1)%len(ring)], radius); ok {
				thickened = append(thickened, piece)
			}
			thickened = append(thickened, disc(point, radius, tolerance))
		}
	}

	return thickened
}

// Containment returns how another region sits against this one.
//
// It is computed from the operations above rather than beside them, so it
// cannot disagree with them: what the two share, what this one has that the
// other does not and what the other has that this one does not are three
// overlays, and which of the six states holds follows from which of them cover
// anything.
//
// Boundary-touching is defined rather than left to fall out. Two regions which
// meet along a boundary and enclose nothing between them are
// [ContainmentTouching] and never [ContainmentOverlapping] — a party wall is
// not an overlap. A region inside another and touching it from the inside is
// [ContainmentInside], because touching the inside of a boundary is not
// crossing it. Both are judged at the declared tolerance, so two corners closer
// together than the project can survey them are the same corner here too.
func (r Region) Containment(other Region) (Containment, []Diagnostic) {
	shared, diags := r.Intersect(other)
	if len(diags) > 0 {
		return "", diags
	}

	if shared.Empty() {
		if r.meets(other) {
			return ContainmentTouching, nil
		}
		return ContainmentDisjoint, nil
	}

	beyond, diags := r.Difference(other)
	if len(diags) > 0 {
		return "", diags
	}

	within, diags := other.Difference(r)
	if len(diags) > 0 {
		return "", diags
	}

	switch {
	case beyond.Empty() && within.Empty():
		return ContainmentCoincident, nil
	case within.Empty():
		return ContainmentInside, nil
	case beyond.Empty():
		return ContainmentSurrounds, nil
	default:
		return ContainmentOverlapping, nil
	}
}

// meets reports whether two regions' boundaries come within the declared
// tolerance of each other anywhere.
//
// It is asked only of two regions which share no area, where it is what tells
// touching from disjoint. Every pair of segments is compared, which is what
// catches two shapes meeting at one corner as well as two sharing a whole wall.
func (r Region) meets(other Region) bool {
	mine := r.figure(r.basis)
	theirs := other.figure(r.basis)

	for _, ring := range mine {
		for i, from := range ring {
			one := segment{a: from, b: ring[(i+1)%len(ring)]}

			for _, against := range theirs {
				for j, start := range against {
					two := segment{a: start, b: against[(j+1)%len(against)]}
					if apart(one, two) <= r.tolerance.Value {
						return true
					}
				}
			}
		}
	}

	return false
}

// apart is how far two segments are from each other, which is zero where they
// cross.
func apart(one, other segment) float64 {
	if _, ok := meeting(one, other); ok {
		return 0
	}

	return min(
		toSegment(one.a, other), toSegment(one.b, other),
		toSegment(other.a, one), toSegment(other.b, one),
	)
}

// toSegment is how far a point is from a segment.
func toSegment(point vec, one segment) float64 {
	direction := one.b.sub(one.a)

	span := direction.dot(direction)
	if span == 0 {
		return point.sub(one.a).length()
	}

	at := math.Min(math.Max(point.sub(one.a).dot(direction)/span, 0), 1)

	return point.sub(one.a.add(direction.scale(at))).length()
}

// In returns the region expressed in another frame.
//
// It is the explicit step operations refuse to take on a caller's behalf. Two
// regions in different frames are not combined until one of them has been
// brought into the other's frame here, because the transform between two frames
// is a measurement with an accuracy of its own
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)) — and an operation
// which applied it quietly would produce an answer whose budget was narrower
// than what it rests on.
//
// The accuracy of the transform is accumulated into the result's budget, so a
// clearance computed after a change of frame knows what the change of frame
// cost it.
//
// The target frame has to be in the unit the tolerance was declared in. A
// region moved into a frame written in millimetres is judged against a
// tolerance in metres by a factor of a thousand, and there is no conversion
// here which would make that right
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
func (r Region) In(target ID, frames *Frames) (Region, []Diagnostic) {
	if frames == nil {
		return Region{}, []Diagnostic{{
			Severity: SeverityError,
			Span:     r.span,
			Message: fmt.Sprintf(
				"expected the frames of the model to express %s in the frame %s, found none resolved",
				r.name(), target,
			),
			Hint: "the transform between two frames is a claim like every other measurement; ResolveFrames is what " +
				"reads those claims, and without them there is no relationship between the two frames to apply",
		}}
	}

	if !r.ready {
		return Region{}, []Diagnostic{{
			Severity: SeverityError,
			Span:     r.span,
			Message: fmt.Sprintf(
				"expected a region to express in the frame %s, found that %s covers no area which could be transformed",
				target, r.name(),
			),
			Hint: "a region is read from the loops bounding a node, against a tolerance the registry declares in the " +
				"unit of its frame; where that could not be done the reason is on a diagnostic of its own",
		}}
	}

	unit := frameUnit(frames.registry, target)
	if unit != r.tolerance.Unit {
		return Region{}, []Diagnostic{{
			Severity: SeverityError,
			Span:     r.span,
			Message: fmt.Sprintf(
				"expected the frame %s to be in %s, which the tolerance %s is declared in, found %s",
				target, r.tolerance.Unit, r.tolerance.Name, spellUnit(unit),
			),
			Hint: "nothing here converts a unit: a tolerance in metres applied to a figure in millimetres is out by a " +
				"thousand, and the answer would look like a shape either way",
			Related: []RelatedLocation{
				{Span: r.tolerance.Span, Message: "the tolerance is declared here"},
			},
		}}
	}

	budget, err := frames.TransformBudget(r.frame, target)
	if err != nil {
		return Region{}, []Diagnostic{{
			Severity: SeverityError,
			Span:     r.span,
			Message: fmt.Sprintf(
				"expected to express %s in the frame %s, found that the two frames are not related: %s",
				r.name(), target, err,
			),
			Hint: "two frames are related by the chain of measured transforms between them; where there is no chain " +
				"there is no answer, and a coordinate carried across unchanged would be in neither frame",
		}}
	}

	var moved [][]Point
	var roles []bool

	for _, piece := range r.pieces {
		for i, ring := range append([][]Point{piece.outer}, piece.holes...) {
			carried := make([]Point, 0, len(ring))
			for _, point := range ring {
				at, err := frames.TransformPoint(point, r.frame, target)
				if err != nil {
					return Region{}, []Diagnostic{{
						Severity: SeverityError,
						Span:     r.span,
						Message: fmt.Sprintf(
							"expected to express %s in the frame %s, found that %s could not be carried across: %s",
							r.name(), target, pointText(point, r.printed()), err,
						),
						Hint: "a transform which cannot be applied to one corner of a shape cannot be applied to the " +
							"shape; nothing here carries the corners it could and leaves the rest",
					}}
				}
				carried = append(carried, at)
			}

			moved = append(moved, carried)
			roles = append(roles, i == 0)
		}
	}

	result := r.derive()
	result.frame = target
	result.unit = unit
	result.budget.Merge(budget)

	if len(moved) == 0 {
		return result, nil
	}

	basis, ok := planeOf(normalOf(moved[0]), moved[0][0])
	if !ok {
		// The rings went in enclosing an area and came out enclosing none, which
		// a rigid transform cannot do and a degenerate one can. Reporting it as a
		// region covering nothing would turn a transform which collapsed the
		// model into an answer, and that answer would be the same one a clearance
		// which genuinely has nothing in it gives.
		return Region{}, []Diagnostic{{
			Severity: SeverityError,
			Span:     r.span,
			Message: fmt.Sprintf(
				"expected %s to still enclose an area once expressed in the frame %s, found that it lies in no plane",
				r.name(), target,
			),
			Hint: "a transform between two frames is rigid up to a scale, so a shape which comes out of one flattened " +
				"is a transform which is not one; the region it would come back as covers nothing and is not this shape",
		}}
	}

	figure := make([]contour, 0, len(moved))
	for i, ring := range moved {
		figure = append(figure, oriented(project(ring, basis), roles[i]))
	}

	result.basis = basis
	result.pieces = piecesOf(overlay(figure, nil, r.tolerance.Value, coveredAlone), basis)

	return result, nil
}

// printed is how many components of a point a message about this region writes.
func (r Region) printed() int {
	if r.dimension < 1 || r.dimension > 3 {
		return 3
	}
	return r.dimension
}

// spellUnit is how a diagnostic names the unit of a frame, naming that there is
// none where the registry declares no such frame.
func spellUnit(unit Unit) string {
	if unit == "" {
		return "a frame the registry does not declare, which has no unit"
	}
	return string(unit)
}

// plane is the plane a region lies in, and the two axes in it every operation
// over the region is computed in.
//
// The axes are orthonormal, so a distance in them is a distance in the model
// and an offset is the same distance all the way round a shape — which
// projecting onto one of the frame's own planes would not give for a ramp or a
// pitched roof, where one direction comes back foreshortened.
//
// Every part of it is a function of the plane and of nothing else. The normal
// is turned to face the same way whichever direction a ring was written in, the
// origin is the point of the plane closest to the frame's origin, and the first
// axis is built from whichever of the frame's axes the plane leans on least. So
// two regions in one plane are projected into exactly the same axes, and an
// overlay of them does not depend on which was the operand it was read from.
type plane struct {
	// origin is the point of the plane every projection is measured from.
	origin Point

	// normal is the unit normal, facing the direction its largest component is
	// positive in.
	normal Point

	// e1 and e2 are the orthonormal axes of the plane, with e1 crossed into e2
	// giving the normal.
	e1, e2 Point
}

// planeOf is the plane of a vector area through a point, and whether there is
// one: a figure with no vector area lies in no plane this can name.
func planeOf(area Point, through Point) (plane, bool) {
	length := pointLength(area)
	if length == 0 {
		return plane{}, false
	}

	normal := pointScale(area, 1/length)
	if normal[dominant(normal)] < 0 {
		// Which way a ring was written decides which way its vector area points
		// and must decide nothing else. Turning the normal to face the way its
		// largest component is positive is a choice made by the plane rather
		// than by the traversal, and the largest component is the one no
		// rounding is going to change the sign of.
		normal = pointScale(normal, -1)
	}

	axis := 0
	for i := 1; i < len(normal); i++ {
		if math.Abs(normal[i]) < math.Abs(normal[axis]) {
			axis = i
		}
	}

	var leaning Point
	leaning[axis] = 1

	first := pointCross(leaning, normal)
	first = pointScale(first, 1/pointLength(first))

	return plane{
		origin: pointScale(normal, pointDot(normal, through)),
		normal: normal,
		e1:     first,
		e2:     pointCross(normal, first),
	}, true
}

// project reads a point in the model as a point in the plane's own axes.
func (p plane) project(point Point) vec {
	from := pointSub(point, p.origin)
	return vec{pointDot(from, p.e1), pointDot(from, p.e2)}
}

// lift reads a point in the plane's own axes back as a point in the model.
func (p plane) lift(point vec) Point {
	return pointAdd(p.origin, pointAdd(pointScale(p.e1, point.X), pointScale(p.e2, point.Y)))
}

// distance is how far a point is out of the plane.
func (p plane) distance(point Point) float64 {
	return math.Abs(pointDot(pointSub(point, p.origin), p.normal))
}

// normalOf is the vector area of a ring of points, whose direction is the
// plane's and whose magnitude is twice the area.
func normalOf(ring []Point) Point {
	var area Point

	for i, point := range ring {
		next := ring[(i+1)%len(ring)]
		area = pointAdd(area, pointCross(pointSub(point, ring[0]), pointSub(next, ring[0])))
	}

	return pointScale(area, 0.5)
}

// project reads a ring of points in the model as a ring in a plane's own axes.
func project(ring []Point, basis plane) contour {
	projected := make(contour, 0, len(ring))
	for _, point := range ring {
		projected = append(projected, basis.project(point))
	}
	return projected
}

// oriented is a ring written the way round its role calls for: anticlockwise
// where it bounds what is inside it and clockwise where it bounds a hole.
func oriented(ring contour, outer bool) contour {
	if (ring.signedArea() > 0) == outer {
		return ring
	}
	return ring.reversed()
}

// piecesOf lifts the rings of an overlay back into the model and groups them
// into the pieces they bound.
func piecesOf(rings []contour, basis plane) []Piece {
	outers, holes := nested(rings)

	pieces := make([]Piece, 0, len(outers))
	for i, outer := range outers {
		piece := Piece{outer: raise(outer, basis), area: outer.signedArea()}

		for _, hole := range holes[i] {
			piece.holes = append(piece.holes, raise(hole, basis))
			piece.area += hole.signedArea()
		}

		pieces = append(pieces, piece)
	}

	if len(pieces) == 0 {
		return nil
	}

	return pieces
}

// raise lifts a ring in a plane's own axes back into the model.
func raise(ring contour, basis plane) []Point {
	raised := make([]Point, 0, len(ring))
	for _, point := range ring {
		raised = append(raised, basis.lift(point))
	}
	return raised
}
