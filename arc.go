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

// Arc is the circle an edge bends along: where its centre is and a point the
// curve passes through between its two ends.
//
// It is a parameterisation and never a list of points. An arc approximated into
// segments while it was being authored cannot be recovered afterwards — every
// consumer inherits the resolution whoever drew it happened to pick, and none of
// them can tell that it happened. Keeping the curve a curve is what makes the
// approximation a decision somebody takes deliberately, at the moment something
// actually requires straight lines, and with a stated tolerance
// ([Topology.TessellateLoop]).
//
// It is supplied rather than read, for the reason [Positions] is: which
// predicate carries an arc centre is vocabulary the consuming repository owns
// and not something the engine knows
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)). A
// repository which spells the two `arc-centre` and `arc-through` resolves those
// predicates for the edges which bend and hands the result to [Survey.Bend]; one
// which spells them something else spells them something else, and nothing here
// changes. That is what the specification means by the predicate registry being
// the extension point for a non-straight edge: no form, no kind and nothing
// compiled in here learns a new name when an arc arrives.
//
// The two ends are the edge's own vertices and are never restated. An arc whose
// endpoints were written a second time would be a second source of truth for
// where a corner is, and the first move of a corner would put the two out of
// step ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// Both fields are positions, which is what lets an arc be claimed the way every
// other measurement is: each is written under a predicate the registry declares,
// in the frame's unit, with its own source, method, date and accuracy.
//
// The zero Arc bends nowhere and is reported as a degenerate arc rather than
// quietly measured as a straight edge.
type Arc struct {
	// Centre is the resolved position of the centre of the circle the edge
	// bends along, in the frame the edge is declared in and in that frame's
	// unit. A centre written in another unit is not read at all
	// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
	Centre Value

	// Through is the resolved position of a point the curve passes through
	// between its two ends, which is what says which of the two arcs is meant.
	//
	// A centre and two ends leave two arcs — the short way round and the long
	// way round — and in three dimensions they leave a plane undecided as well,
	// because ends diametrically opposite a centre lie on every plane through
	// the line between them. One point on the curve answers both at once and
	// answers them the way a surveyor would: it is somewhere on the wall, and it
	// was measured.
	//
	// It decides which arc and never where it is. The radius is read from the
	// centre and the start, the sweep from the centre and the two ends, so a
	// point on the curve half a millimetre out picks the same arc and moves no
	// figure — which is why the accuracy of the claim behind it is not
	// accumulated into an answer and the accuracy of the centre is. What it is
	// checked for is being on the circle at all, which a claim disagreeing with
	// the centre is not.
	Through Value
}

// Curvature is the arc each edge bends along, keyed by the edge's id.
//
// An edge which is absent is straight, which is what almost every edge is. That
// is a state rather than a default: nothing in the model says an edge is
// straight, and nothing needs to.
type Curvature map[ID]Arc

// Bend records that one edge bends along an arc, from the resolution which
// decided where its centre is.
//
// It is to [Curvature] what [Survey.Place] is to [Positions], and it is here for
// the same reason: the centre of an arc is a claim like any other, and a caller
// filling the curvature without the evidence behind it leaves an answer whose
// budget is narrower than what it rests on.
//
// The evidence recorded is the centre's, because the centre is what every figure
// computed from the arc rests on. [Arc.Through] says which of two arcs was meant
// and moves nothing, so accumulating its accuracy would widen a budget by a
// claim no figure was computed from.
//
// An edge either bends or does not, so an arc with only one of its two positions
// resolved bends nothing. That is not a curve half stated, it is a claim
// somebody has yet to write.
func (s *Survey) Bend(edge ID, centre, through Resolution) {
	middle, ok := through.Value()
	if !ok {
		return
	}

	value, ok := centre.Value()
	if !ok {
		return
	}

	if s.Curvature == nil {
		s.Curvature = make(Curvature)
	}
	s.Curvature[edge] = Arc{Centre: value, Through: middle}

	claim, ok := centre.Claim()
	if !ok {
		return
	}

	if s.Evidence == nil {
		s.Evidence = make(Evidence)
	}
	s.Evidence[edge] = claim
}

// Tessellation is a curve replaced by the straight segments which stand in for
// it, together with the chord tolerance that was decided to.
//
// The tolerance travels with the result because the result is meaningless
// without it. A list of points which does not say how closely it follows the
// curve it came from is exactly the approximation this package refuses to bake
// into a model: nobody downstream can tell whether it is good enough for what
// they are about to do with it, and nobody can reproduce it.
//
// Nothing here is ever written back
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)). A
// tessellation is computed at the moment something needs straight lines and is
// thrown away after; the arc in the model is untouched and stays exact.
//
// The zero Tessellation holds no points, which is what tessellating nothing
// yields; every method below works on it.
type Tessellation struct {
	// subject is the id of the edge or loop which was tessellated.
	subject ID

	// unit is the linear unit of its frame, which the points and the deviation
	// are in.
	unit Unit

	// tolerance is the declared chord tolerance it was drawn to.
	tolerance Tolerance

	// points are the ends of the segments, in traversal order. A closed ring
	// does not repeat its first point at the end.
	points []Point

	// closed reports whether the points form a ring.
	closed bool

	// deviation is the furthest any of those segments falls from the curve it
	// stands in for, which is at most the tolerance.
	deviation float64

	// dimension is how many components the positions behind it were written
	// with, which is how many a point in a message is printed with.
	dimension int
}

// Subject returns the id of the edge or loop which was tessellated.
func (t Tessellation) Subject() ID { return t.subject }

// Unit returns the linear unit of the frame it was computed in, which its points
// and its deviation are in.
func (t Tessellation) Unit() Unit { return t.unit }

// ChordTolerance returns the declared tolerance the curve was drawn to: how far
// a segment standing in for the curve was allowed to fall from it.
//
// It is a tolerance the registry declares and never a number this package chose
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)). How closely a
// curve has to be followed is a decision a project makes — a millimetre for a
// setting-out drawing, a hundred millimetres for an area take-off — and a
// default compiled in here would be the engine making it for them.
func (t Tessellation) ChordTolerance() Tolerance { return t.tolerance }

// Points returns the ends of the segments, in traversal order.
//
// A ring does not repeat its first point at the end, which [Tessellation.Closed]
// is what says. Every arc contributes its own two ends exactly as they were
// surveyed, so two tessellations which meet at a corner meet at the corner and
// not near it.
func (t Tessellation) Points() []Point { return slices.Clone(t.points) }

// Closed reports whether the points form a ring, which they do when the loop
// they came from closed.
func (t Tessellation) Closed() bool { return t.closed }

// Deviation returns the furthest any segment falls from the curve it stands in
// for, in [Tessellation.Unit].
//
// It is what was actually achieved rather than what was asked for, and it is
// always within [Tessellation.ChordTolerance]. The two differ because a curve is
// divided into a whole number of segments: an arc which needs two and a bit
// segments gets three, and follows the curve more closely than it had to.
func (t Tessellation) Deviation() float64 { return t.deviation }

// String writes how many segments the curve became and how closely they follow
// it.
func (t Tessellation) String() string {
	if len(t.points) == 0 {
		return fmt.Sprintf("%s: nothing tessellated", t.subject)
	}

	segments := len(t.points) - 1
	if t.closed {
		segments = len(t.points)
	}

	parts := []string{
		plural(segments, "segment"),
		fmt.Sprintf("within %s%s of the curve", decimal(t.deviation), unitSuffix(t.unit)),
	}
	if t.tolerance.Name != "" {
		parts = append(parts, fmt.Sprintf("drawn to %s", t.tolerance.Name))
	}

	return fmt.Sprintf("%s: %s", t.subject, strings.Join(parts, ", "))
}

// TessellateEdge replaces one edge with the straight segments which stand in for
// it, no further from it than the named chord tolerance allows.
//
// It is the only way an arc becomes segments, and it is explicit for that
// reason. Nothing in this package tessellates on the way to an answer: a length,
// an area, a centroid and a bounding box are all computed from the arc itself
// ([Topology.MeasureEdge]), so the resolution of a drawing never leaks into a
// figure somebody reports.
//
// A straight edge tessellates to its two ends and deviates from itself by
// nothing, which is what makes a ring of straight edges and a ring with one arc
// in it the same kind of result to a caller.
//
// The tolerance is named and the registry declares it, in the unit of the edge's
// frame. There is no default and no fallback
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
//
// The same edge and the same tolerance produce the same points every time, to
// the last bit. A tessellation which drifted between two runs would show up as a
// diff in whatever it was written into and as an argument about which run was
// right.
func (t *Topology) TessellateEdge(edge *Edge, survey Survey, chord string) (Tessellation, []Diagnostic) {
	if edge == nil {
		return Tessellation{}, nil
	}

	m := t.measuring(edge.id, edge.frame, t.namedAt(edge.id, edge.span), survey)

	tolerance, ok := m.chord(chord, m.edgeAt(edge))
	if !ok {
		return Tessellation{}, m.diags
	}

	from, to, dimension, ok := m.ends(edge)
	if !ok {
		return Tessellation{}, m.diags
	}

	drawn := Tessellation{
		subject:   edge.id,
		unit:      m.unit,
		tolerance: tolerance,
		points:    []Point{from, to},
		dimension: dimension,
	}

	curve, bends, ok := m.bend(edge, from, to, false)
	if !ok {
		return Tessellation{}, m.diags
	}

	if bends && !m.draw(curve, tolerance, &drawn) {
		return Tessellation{}, m.diags
	}

	return drawn, m.diags
}

// TessellateLoop replaces a loop with the ring of straight segments which stands
// in for it, no further from it than the named chord tolerance allows.
//
// The loop is assembled first, so the segments come back in the order the ring
// is traversed and in the direction the traversal runs — an arc written once and
// traversed by the room on either side of it tessellates the other way round for
// the other room, and its two ends are still the two corners. Every diagnostic
// [Topology.Assemble] raises comes back from here.
//
// A ring which did not close is still tessellated, and [Tessellation.Closed]
// says so. The gap is already reported by the pass whose job that is, and the
// segments as far as the traversal got are what a caller drawing the failure
// wants to show.
//
// [Topology.TessellateEdge] says why this is explicit, what the named tolerance
// is, and why the same input gives the same output every time.
func (t *Topology) TessellateLoop(loop *Loop, survey Survey, chord string) (Tessellation, []Diagnostic) {
	if loop == nil {
		return Tessellation{}, nil
	}

	m := t.measuring(loop.id, loop.frame, t.namedAt(loop.id, loop.span), survey)

	tolerance, ok := m.chord(chord, m.topology.namedAt(loop.id, loop.span))
	if !ok {
		return Tessellation{}, m.diags
	}

	out, ok := m.ring(loop)
	if !ok {
		return Tessellation{}, m.diags
	}

	drawn := Tessellation{
		subject:   loop.id,
		unit:      m.unit,
		tolerance: tolerance,
		closed:    out.closed,
		dimension: out.dimension,
	}

	for i, point := range out.points {
		curve := out.bends[i]
		if curve == nil {
			drawn.points = append(drawn.points, point)
			continue
		}

		segment := Tessellation{}
		if !m.draw(curve, tolerance, &segment) {
			return Tessellation{}, m.diags
		}

		// The last point of one arc is the first corner of the next step, so it
		// is written once, by the step it begins.
		drawn.points = append(drawn.points, segment.points[:len(segment.points)-1]...)
		drawn.deviation = math.Max(drawn.deviation, segment.deviation)
	}

	if out.closed {
		return drawn, m.diags
	}

	// An open traversal ends where its last step ended, which is a corner no
	// step of the ring begins at and so one nothing above has written.
	if !out.finished {
		m.add(Diagnostic{
			Severity: SeverityError,
			Span:     m.topology.namedAt(loop.id, loop.span),
			Message: fmt.Sprintf(
				"expected a position for the corner the traversal of the loop %s ends at, found none for %s",
				geometricName(loopTag, loop.id), out.last,
			),
			Hint: m.positionHint(),
		})
		return Tessellation{}, m.diags
	}

	drawn.points = append(drawn.points, out.finish)

	return drawn, m.diags
}

// maxChordDivisions is the most segments one arc is allowed to become.
//
// It is not a resolution and it is not a tolerance. How closely a curve is
// followed is the declared chord tolerance's to say and [bend.divisions] reads
// it from there; this is the point at which the answer stops being a drawing and
// becomes a refusal, so that a tolerance finer than any coordinate in the model
// is reported rather than turned into a million points nobody can use.
const maxChordDivisions = 1 << 16

// chord reads the named chord tolerance, reporting a name the registry does not
// declare and one declared in a unit the frame is not in.
func (m *measurer) chord(name string, at Span) (Tolerance, bool) {
	tolerance, declared := m.survey.Registry.Tolerance(name)

	switch {
	case m.unit == "":
		m.add(m.untessellated(at, fmt.Sprintf(
			"the frame the %s is declared in is not one the registry declares, so nothing here has a unit",
			subjectOf(m.subject))))
	case !declared:
		m.add(m.untessellated(at, fmt.Sprintf("the registry declares no tolerance %q", name)))
	case tolerance.Unit != m.unit:
		m.add(m.untessellated(at, fmt.Sprintf("the tolerance %s is in %s and the frame is in %s",
			tolerance.Name, tolerance.Unit, m.unit)))
	case tolerance.Value <= 0:
		m.add(m.untessellated(at, fmt.Sprintf("the tolerance %s is %s %s",
			tolerance.Name, decimal(tolerance.Value), tolerance.Unit)))
	default:
		return tolerance, true
	}

	return Tolerance{}, false
}

// untessellated reports a curve which cannot be drawn because the tolerance to
// draw it to cannot be applied.
func (m *measurer) untessellated(at Span, found string) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Span:     at,
		Message: fmt.Sprintf(
			"expected a chord tolerance in the unit of the frame to tessellate %s, found that %s",
			m.subject, found,
		),
		Hint: "how closely a curve has to be followed is a decision a project writes down; there is no default for it, " +
			"and one compiled in here would be the engine choosing the resolution of somebody else's drawing",
	}
}

// draw fills a tessellation with the segments one arc becomes, reporting an arc
// a tolerance would take more segments to follow than anything can use.
func (m *measurer) draw(curve *bend, tolerance Tolerance, into *Tessellation) bool {
	divisions := curve.divisions(tolerance.Value)
	if divisions > maxChordDivisions {
		m.add(Diagnostic{
			Severity: SeverityError,
			Span:     m.edgeAt(curve.edge),
			Message: fmt.Sprintf(
				"expected the arc %s to be drawable to the tolerance %s, which is %s %s, found that following it that "+
					"closely takes more than %d segments",
				geometricName(edgeTag, curve.edge.id), tolerance.Name,
				decimal(tolerance.Value), tolerance.Unit, maxChordDivisions,
			),
			Hint: "a tolerance far finer than the coordinates the arc was surveyed to draws a curve to a resolution " +
				"nothing behind it supports; the number of points it takes is the symptom and not the problem",
			Related: []RelatedLocation{
				{Span: tolerance.Span, Message: "the tolerance is declared here"},
			},
		})
		return false
	}

	into.points = curve.points(divisions)
	into.deviation = curve.deviation(divisions)

	return true
}

// bend is one edge's arc reduced to arithmetic: the circle it runs on, the two
// axes of its plane, how far it sweeps and which way.
//
// Everything about it is exact. A point on it is read from the parameterisation
// rather than interpolated between segments, which is what lets a length, an
// area and a bounding box be computed without ever drawing the curve.
//
// The sweep runs from u toward v, so a point at parameter t is centre plus the
// radius times cos t along u plus sin t along v, for t from zero to the sweep.
// The normal is u cross v, which is the direction the sweep runs anticlockwise
// about and which the traversal reverses when it runs the edge backwards.
type bend struct {
	// edge is the edge which bends, and at where a diagnostic about the arc
	// points.
	edge *Edge
	span Span

	// start and end are the two ends as they were surveyed, which the arc runs
	// between and which nothing here recomputes.
	start, end Point

	// centre is the centre of the circle and radius its radius.
	centre Point
	radius float64

	// u is the unit direction from the centre to the start, and v the unit
	// direction a quarter turn along the sweep from it.
	u, v Point

	// normal is the unit normal of the arc's plane, oriented so the sweep runs
	// anticlockwise about it.
	normal Point

	// sweep is how far the arc turns, in radians, and is greater than zero and
	// less than a full turn.
	sweep float64
}

// bend reads the arc one edge bends along, reporting every way the stated
// parameterisation fails to describe one.
//
// The second result says whether the edge bends at all, which is a different
// question from whether it could be read: an edge with no arc is straight and is
// not a failure, and an edge whose arc is degenerate is a failure and is not
// straight.
//
// The traversal direction is the bend's and not the edge's. An edge is written
// once, with its ends ordered, and the ring on the other side of it runs through
// it backwards — which sweeps the same arc about the opposite normal, and is
// what makes the same bulge add to one ring's area and take away from the
// other's.
func (m *measurer) bend(edge *Edge, from, to Point, reversed bool) (*bend, bool, bool) {
	arc, ok := m.survey.Curvature[edge.id]
	if !ok {
		return nil, false, true
	}

	at := m.edgeAt(edge)

	centre, ok := m.position(arc.Centre)
	if !ok {
		m.degenerateArc(edge, at, "no centre it could read in the unit of the frame",
			"a position is read in the unit of its frame and nothing is converted; a centre written in another unit "+
				"is not read at all")
		return nil, true, false
	}

	middle, ok := m.position(arc.Through)
	if !ok {
		m.degenerateArc(edge, at, "no point on it it could read in the unit of the frame",
			"a centre and two ends leave two arcs, the short way round and the long way round; a point the curve "+
				"passes through is what says which of them was meant")
		return nil, true, false
	}

	curve := &bend{edge: edge, span: at, start: from, end: to, centre: centre}

	first := pointSub(from, centre)
	between := pointSub(middle, centre)
	last := pointSub(to, centre)

	curve.radius = pointLength(first)
	if curve.radius == 0 {
		m.degenerateArc(edge, at, fmt.Sprintf("a radius of zero: its centre is at %s, where %s is",
			pointText(centre, m.result.printed()), edge.start),
			"an arc of no radius is a point; two ends and a centre on top of one of them describe nothing to follow")
		return nil, true, false
	}

	facing := pointCross(first, between)
	if pointLength(facing) == 0 {
		m.degenerateArc(edge, at, fmt.Sprintf("the point it passes through, %s, in line with its centre and %s",
			pointText(middle, m.result.printed()), edge.start),
			"three points in a line lie on every plane through it; a point on the curve says which plane the arc is "+
				"in only where it is off the line its centre and its start make")
		return nil, true, false
	}

	curve.normal = pointScale(facing, 1/pointLength(facing))
	curve.u = pointScale(first, 1/curve.radius)
	curve.v = pointCross(curve.normal, curve.u)

	if !m.circular(curve, middle, between, last) {
		return nil, true, false
	}

	curve.sweep = curve.turn(last)
	if curve.sweep == 0 {
		m.degenerateArc(edge, at, fmt.Sprintf("its two ends at the same point on the circle, %s",
			pointText(from, m.result.printed())),
			"an arc which begins and ends in one place sweeps either nothing or the whole circle, and the "+
				"parameterisation does not say which; two ends surveyed to one coordinate are one end")
		return nil, true, false
	}

	if curve.turn(between) > curve.sweep {
		m.degenerateArc(edge, at, fmt.Sprintf("the point it passes through, %s, past the end it reaches at %s",
			pointText(middle, m.result.printed()), edge.end),
			"a point on the curve is between the two ends, on the arc which is the edge; one beyond the far end is on "+
				"the same circle and not on this arc of it")
		return nil, true, false
	}

	if reversed {
		// The same circle swept the other way. Reversing the direction and the
		// normal together leaves the sweep exactly as it was, which is what
		// makes a shared wall bulge the same way for the rooms either side of
		// it and contribute the opposite way to their two areas.
		curve.start, curve.end = to, from
		curve.normal = pointScale(curve.normal, -1)
		curve.u = pointScale(last, 1/pointLength(last))
		curve.v = pointCross(curve.normal, curve.u)
	}

	return curve, true, true
}

// turn is how far round the sweep a point on the circle is, from nothing at the
// start to a whole turn back at it.
func (b *bend) turn(offset Point) float64 {
	angle := math.Atan2(pointDot(offset, b.v), pointDot(offset, b.u))
	if angle < 0 {
		angle += 2 * math.Pi
	}
	return angle
}

// position reads a claimed position in the unit of the subject's frame, and
// whether it has one there.
func (m *measurer) position(value Value) (Point, bool) {
	written, ok := value.Coordinate()
	if !ok || m.unit == "" || value.Unit() != m.unit {
		return Point{}, false
	}
	return asPoint(written), true
}

// circular reports whether the stated centre is the centre of one circle all
// three of the ends and the point on the curve lie on, and whether they lie in
// one plane.
//
// It is judged against the declared tolerance and is not judged at all where that
// tolerance could not be applied, for the reason [measurer.planar] is: there is
// no default and no fallback
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
func (m *measurer) circular(curve *bend, middle, between, last Point) bool {
	if !m.applicable() {
		return true
	}

	away := math.Abs(pointDot(last, curve.normal))
	if away > m.tolerance.Value {
		m.degenerateArc(curve.edge, curve.span, fmt.Sprintf(
			"%s lying %s %s out of the plane its centre, %s and the point it passes through make",
			curve.edge.end, decimal(away), m.unit, curve.edge.start),
			"an arc lies in one plane; an end out of it is a centre, a point on the curve and a corner which do not "+
				"describe one circle between them")
		return false
	}

	for _, off := range [...]struct {
		what  string
		where Point
		by    float64
	}{
		{string(curve.edge.end), last, math.Abs(pointLength(last) - curve.radius)},
		{fmt.Sprintf("the point it passes through, %s", pointText(middle, m.result.printed())),
			between, math.Abs(pointLength(between) - curve.radius)},
	} {
		if off.by <= m.tolerance.Value {
			continue
		}

		m.degenerateArc(curve.edge, curve.span, fmt.Sprintf(
			"%s lying %s %s off the circle of radius %s %s about it",
			off.what, decimal(off.by), m.unit, decimal(curve.radius), m.unit),
			"the centre of an arc is the same distance from both of its ends and from every point on it; one which is "+
				"not is not the centre of a circle they lie on")
		return false
	}

	return true
}

// degenerateArc reports an arc which does not describe a curve, saying what was
// found in place of one.
func (m *measurer) degenerateArc(edge *Edge, at Span, found, hint string) {
	related := []RelatedLocation{{Span: edge.span, Message: "the edge is written here"}}
	if m.declared {
		related = append(related, RelatedLocation{Span: m.tolerance.Span, Message: "the tolerance is declared here"})
	}

	m.add(Diagnostic{
		Severity: SeverityError,
		Span:     at,
		Message: fmt.Sprintf("expected the arc the edge %s bends along to describe a curve, found %s",
			geometricName(edgeTag, edge.id), found),
		Hint:    hint,
		Related: related,
	})
}

// subjectOf names what is being tessellated where a message has to say so.
func subjectOf(id ID) string {
	if id == "" {
		return "curve"
	}
	return string(id)
}

// at is the point at one parameter of the sweep, which is exact and is never
// interpolated between segments.
func (b *bend) at(t float64) Point {
	return pointAdd(b.centre, pointScale(
		pointAdd(pointScale(b.u, math.Cos(t)), pointScale(b.v, math.Sin(t))), b.radius))
}

// length is how far the arc runs, which is its radius times its sweep.
func (b *bend) length() float64 { return b.radius * b.sweep }

// centroid is where the arc's length is centred, which is on the bisector of its
// sweep and inside the circle rather than on it.
func (b *bend) centroid() Point {
	half := b.sweep / 2
	return pointAdd(b.centre, pointScale(b.bisector(), b.radius*math.Sin(half)/half))
}

// bisector is the unit direction from the centre to the middle of the arc.
func (b *bend) bisector() Point {
	half := b.sweep / 2
	return pointAdd(pointScale(b.u, math.Cos(half)), pointScale(b.v, math.Sin(half)))
}

// bounds is the box the arc reaches, which is the box of its two ends widened
// wherever the curve turns further out than either of them.
//
// The turning points are where the arc runs parallel to an axis, which is a
// pair of parameters per axis read straight off the two axes of the plane. That
// is what makes this exact: a box drawn round a tessellation is a box round the
// chords, and is smaller than the shape by whatever the sag was.
func (b *bend) bounds() Box {
	box := boxOf([]Point{b.start, b.end})

	for axis := range 3 {
		base := math.Atan2(b.v[axis], b.u[axis])

		for _, t := range [2]float64{base, base + math.Pi} {
			t = math.Mod(t, 2*math.Pi)
			if t < 0 {
				t += 2 * math.Pi
			}
			if t > b.sweep {
				continue
			}

			reach := b.at(t)
			box.Min[axis] = math.Min(box.Min[axis], reach[axis])
			box.Max[axis] = math.Max(box.Max[axis], reach[axis])
		}
	}

	return box
}

// area is twice the vector area of the circular segment the arc cuts off from
// its chord, which is what a ring's own vector area is short by wherever it
// bends.
//
// It is twice it, and points along the arc's own normal, so that it adds to the
// Newell sum over the ring's corners term for term: a bulge which runs the way
// the ring does adds to the area and one which runs against it takes away, with
// nothing having to decide which is which.
func (b *bend) area() Point {
	return pointScale(b.normal, b.radius*b.radius*(b.sweep-math.Sin(b.sweep)))
}

// segment is where the area of that circular segment is centred, and whether
// there is any area there to centre.
//
// A sweep small enough that its segment has no area in floating point has no
// centroid either, and the point of saying so is that the arithmetic which would
// find one divides by that area.
func (b *bend) segment() (Point, bool) {
	denominator := b.sweep - math.Sin(b.sweep)
	if denominator <= 0 {
		return Point{}, false
	}

	half := math.Sin(b.sweep / 2)

	return pointAdd(b.centre, pointScale(b.bisector(), 4*b.radius*half*half*half/(3*denominator))), true
}

// divisions is how many segments the arc becomes to follow it no further away
// than a chord tolerance allows.
//
// The furthest a chord subtending an angle falls inside the circle is the radius
// less the radius times the cosine of half that angle, so the widest angle a
// tolerance allows is twice the arc cosine of one less the tolerance over the
// radius. The sweep is divided into a whole number of those, which is why the
// deviation achieved is at most the one asked for and usually less.
//
// It is arithmetic on the arc and the tolerance and on nothing else, which is
// what makes the same arc drawn to the same tolerance give the same points every
// time.
func (b *bend) divisions(tolerance float64) int {
	ratio := 1 - tolerance/b.radius
	if ratio <= -1 {
		// A tolerance as wide as the circle: no chord of it can fall further
		// away than that, so one segment follows the arc closely enough.
		return 1
	}

	widest := 2 * math.Acos(ratio)
	if widest <= 0 {
		return maxChordDivisions + 1
	}

	return max(int(math.Ceil(b.sweep/widest)), 1)
}

// deviation is how far the segments of a division actually fall from the arc,
// which is the sag of one of them.
func (b *bend) deviation(divisions int) float64 {
	return b.radius * (1 - math.Cos(b.sweep/float64(2*divisions)))
}

// points is the arc drawn as a whole number of segments, from its start to its
// end.
//
// The two ends are the surveyed corners themselves and are not recomputed from
// the parameterisation. A ring whose arcs each began a rounding away from the
// corner they share would not close, and the gap would be reported against a
// model in which nothing is wrong.
func (b *bend) points(divisions int) []Point {
	points := make([]Point, 0, divisions+1)
	points = append(points, b.start)

	for i := 1; i < divisions; i++ {
		points = append(points, b.at(b.sweep*float64(i)/float64(divisions)))
	}

	return append(points, b.end)
}

// holds reports whether a point on the arc's circle is within its sweep.
func (b *bend) holds(point Point) bool {
	return b.turn(pointSub(point, b.centre)) <= b.sweep
}
