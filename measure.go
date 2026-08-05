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

// Evidence is the claim each vertex's position was read from, keyed by the
// vertex's id.
//
// It is the other half of [Positions], supplied for the same reason: which
// predicate carries a position is vocabulary the consuming repository owns and
// not something the engine knows
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
// What it adds is the provenance a computed answer inherits. An area is only as
// well known as the corners it was computed from, and a budget accumulated from
// these claims is what lets the answer say so
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)).
//
// A vertex which is absent contributes nothing to the budget. The geometry is
// still computed — a position with no claim behind it is a caller which filled
// one map and not the other, which [Survey.Place] exists to prevent.
type Evidence map[ID]*Claim

// Survey is what a measurement is computed against: where the corners are, the
// claims which put them there, the tolerance corners are judged coincident
// against, and the registry the frames and their units are read from.
//
// The four travel together because every measurement needs all four, and
// because an answer computed against some of them could not say what it was
// computed against. A size is not a fact about a shape alone: it is a fact about
// a shape, read in a frame whose unit the registry declares, from positions
// which are claims, judged against a tolerance a project wrote down
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
//
// The zero Survey measures nothing and reports why, which is what a caller which
// has not loaded a registry yet gets rather than a panic.
type Survey struct {
	// Positions is where the vertices are.
	Positions Positions

	// Evidence is the claim each of those positions was read from.
	Evidence Evidence

	// Tolerance is the name — never a number — of the tolerance corners are
	// judged coincident and rings judged planar against.
	Tolerance string

	// Registry is the vocabulary the frames, their units and the tolerance are
	// read from.
	Registry *Registry
}

// Place records where one vertex is, from the resolution which decided it.
//
// It is what keeps [Survey.Positions] and [Survey.Evidence] in step. Both come
// from one resolution per vertex, and a caller filling them separately can leave
// a corner whose position is measured and whose provenance is not — which shows
// up much later as an answer with a narrower budget than the evidence supports.
//
// A resolution which resolved nothing places nothing, which is the ordinary
// state of a vertex nobody has surveyed yet.
func (s *Survey) Place(vertex ID, resolution Resolution) {
	value, ok := resolution.Value()
	if !ok {
		return
	}

	if s.Positions == nil {
		s.Positions = make(Positions)
	}
	s.Positions[vertex] = value

	claim, ok := resolution.Claim()
	if !ok {
		return
	}

	if s.Evidence == nil {
		s.Evidence = make(Evidence)
	}
	s.Evidence[vertex] = claim
}

// Box is an axis-aligned bounding box, in the unit of the frame it was computed
// in.
//
// It is axis-aligned to the frame's own axes and to nothing else. A tighter box
// at some other orientation is a different question with a different answer, and
// the one thing this box is good for — deciding quickly whether two things can
// possibly interact — needs the axes to be shared.
//
// The zero Box is the box of nothing, which is what a measurement with no
// bounding box carries.
type Box struct {
	// Min and Max are the corners of the box, component by component.
	Min, Max Point

	// Unit is the linear unit of the frame the box was computed in.
	Unit Unit
}

// Size returns the extent of the box along each axis.
func (b Box) Size() Point {
	return Point{b.Max[0] - b.Min[0], b.Max[1] - b.Min[1], b.Max[2] - b.Min[2]}
}

// Centre returns the middle of the box, which is not the centroid: a box knows
// where a shape reaches and nothing about where its area is.
func (b Box) Centre() Point {
	return Point{
		b.Min[0] + (b.Max[0]-b.Min[0])/2,
		b.Min[1] + (b.Max[1]-b.Min[1])/2,
		b.Min[2] + (b.Max[2]-b.Min[2])/2,
	}
}

// String writes both corners with the unit they are in.
func (b Box) String() string {
	return fmt.Sprintf("%s to %s%s", pointText(b.Min, 3), pointText(b.Max, 3), unitSuffix(b.Unit))
}

// Measurement is how big one thing is, computed from where its corners are.
//
// Nothing here is ever written back into a model. An area recorded beside the
// boundary it describes is a second source of truth which goes stale the first
// time a wall moves, and the whole reason this is computed on demand is that it
// cannot disagree with the geometry it is derived from
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// Every figure is in the unit of the frame the thing is declared in, which
// [Measurement.Unit] reports and nothing converts
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)). A length is in
// that unit and an area is in it squared.
//
// Each figure comes back with whether it could be computed at all, because
// "there is no answer" and "the answer is zero" are different states and a shape
// which does not close has the first. A figure which is not available is
// accompanied by a diagnostic saying why, and never by a number which looks
// plausible.
//
// The zero Measurement measures nothing and every method below works on it.
type Measurement struct {
	// subject is the id of the thing measured.
	subject ID

	// unit is the linear unit of the frame it was measured in, empty where the
	// registry declares no frame of that id.
	unit Unit

	// dimension is how many components the positions it was computed from were
	// written with, which is what a point in a message is printed with.
	dimension int

	// length is the extent of an edge, or the total length of the edges of a
	// loop or a region, and hasLength whether it could be computed.
	length    float64
	hasLength bool

	// area is the enclosed area, in unit squared, and hasArea whether it could be
	// computed. An edge encloses nothing and has neither.
	area    float64
	hasArea bool

	// centroid is the area centroid of a region or a loop, the midpoint of an
	// edge, and hasCentroid whether it could be computed.
	centroid    Point
	hasCentroid bool

	// bounds is the axis-aligned bounding box, and hasBounds whether it could be
	// computed.
	bounds    Box
	hasBounds bool

	// budget is the accumulated accuracy of the position claims every figure
	// above was computed from.
	budget Budget
}

// Subject returns the id of the thing which was measured.
func (m Measurement) Subject() ID { return m.subject }

// Unit returns the linear unit of the frame the thing was measured in, which is
// empty where the registry declares no frame of that id.
//
// Every figure is in it: a length in the unit, an area in the unit squared. A
// measurement is never converted into another one, so a figure and this unit
// together are the whole answer.
func (m Measurement) Unit() Unit { return m.unit }

// Length returns the extent of an edge, or the total length of the edges of a
// loop or a region, and whether it could be computed.
//
// For a closed loop that is its perimeter. For one which does not close it is
// still the total length of the edges written on it, which is what is true: the
// edges have lengths whether or not they form a ring.
func (m Measurement) Length() (float64, bool) { return m.length, m.hasLength }

// Area returns the enclosed area, in the square of [Measurement.Unit], and
// whether it could be computed.
//
// Nothing encloses an area unless it is a ring which closes, does not cross
// itself and lies in one plane. Each of those failing is a diagnostic and leaves
// this reporting that there is no answer, which is the point: a projection of a
// shape which is not planar, or the signed sum over one which crosses itself,
// are both numbers, and neither is an area.
func (m Measurement) Area() (float64, bool) { return m.area, m.hasArea }

// Centroid returns where the area of the thing is centred, and whether that
// could be computed.
//
// It is the area centroid and not the mean of the corners. A room with three
// corners bunched at one end has its area centred away from them, and the mean
// of the corners is not a point anybody asked about.
//
// For an edge it is the midpoint of the segment, which is the same definition
// one dimension down.
func (m Measurement) Centroid() (Point, bool) { return m.centroid, m.hasCentroid }

// Bounds returns the axis-aligned bounding box, and whether it could be
// computed.
//
// It needs only the corners, so it survives shapes which have no area: a ring
// with a gap in it still reaches as far as it reaches.
func (m Measurement) Bounds() (Box, bool) { return m.bounds, m.hasBounds }

// Budget returns the accumulated accuracy of the position claims the figures
// were computed from.
//
// It is in the frame's linear unit, and is the uncertainty of the corners rather
// than of the area — the sensitivity of an area to each of its corners is a
// per-corner quantity, and one number standing in for all of them would be
// exactly the plausible-looking wrong answer the rest of this file refuses to
// produce. What it does say is what the answer rests on: which claims, which
// shared terms among them counted once, and whether any corner stated no
// accuracy at all, which [Budget.Known] reports.
func (m Measurement) Budget() Budget { return m.budget }

// String writes the figures which were computed, with their units.
func (m Measurement) String() string {
	var parts []string

	if m.hasArea {
		parts = append(parts, fmt.Sprintf("area %s%s", decimal(m.area), squareSuffix(m.unit)))
	}
	if m.hasLength {
		parts = append(parts, fmt.Sprintf("length %s%s", decimal(m.length), unitSuffix(m.unit)))
	}
	if m.hasCentroid {
		parts = append(parts, fmt.Sprintf("centroid %s", pointText(m.centroid, m.printed())))
	}
	if m.hasBounds {
		parts = append(parts, fmt.Sprintf("bounds %s to %s",
			pointText(m.bounds.Min, m.printed()), pointText(m.bounds.Max, m.printed())))
	}

	if len(parts) == 0 {
		return fmt.Sprintf("%s: nothing measurable", m.subject)
	}

	return fmt.Sprintf("%s: %s", m.subject, strings.Join(parts, ", "))
}

// printed is how many components of a point to write, which is how many the
// positions were written with.
func (m Measurement) printed() int {
	if m.dimension < 1 || m.dimension > 3 {
		return 3
	}
	return m.dimension
}

// MeasureEdge measures one edge: how long it is, where its midpoint is and how
// far it reaches.
//
// Everything is in the unit of the edge's frame and nothing is converted. A
// position written in another unit is not read at all, which is reported rather
// than scaled ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
//
// An edge whose two ends are at the same point is a diagnostic and not a length
// of zero quietly returned. It gives a loop through it no direction, and the
// author who wrote it meant two corners.
func (t *Topology) MeasureEdge(edge *Edge, survey Survey) (Measurement, []Diagnostic) {
	if edge == nil {
		return Measurement{}, nil
	}

	m := t.measuring(edge.id, edge.frame, t.namedAt(edge.id, edge.span), survey)
	m.edge(edge)

	return m.result, m.diags
}

// MeasureLoop measures one loop: its area, its perimeter, its centroid and its
// bounding box.
//
// The loop is assembled first, because a size is a property of the ring its
// edges traverse and not of the list they were written in. Every diagnostic
// [Topology.Assemble] raises comes back from here, and a loop which does not
// assemble into one closed ring has no area — which is reported as no answer
// rather than as the signed sum over whatever the edges did form.
//
// Three more things stop an area being computable, each its own diagnostic
// because each is a different mistake: corners which do not lie in one plane, a
// ring which crosses itself, and a ring whose corners are collinear and so
// enclose nothing.
func (t *Topology) MeasureLoop(loop *Loop, survey Survey) (Measurement, []Diagnostic) {
	if loop == nil {
		return Measurement{}, nil
	}

	m := t.measuring(loop.id, loop.frame, t.namedAt(loop.id, loop.span), survey)

	if ring, ok := m.ring(loop); ok {
		m.single(ring)
	}

	return m.result, m.diags
}

// MeasureRegion measures a semantic node from the loops which bound it: the area
// it encloses, the total length of its boundary, its centroid and its bounding
// box.
//
// The region holds no coordinate of its own. It references loops, the loops
// reference edges and the edges reference vertices, so a measurement of it is
// computed the whole way down every time it is asked — which is why it cannot
// disagree with the geometry it describes
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// A region bounded by more than one ring is measured by nesting. A ring inside
// another is a hole and its area is taken away; a ring inside that hole is an
// island and is added back. That is the even-odd rule, and it is what makes a
// courtyard subtract without anything in the model having to say which loop is
// the outside one.
//
// A region which references no loop is not malformed — a circuit group and a
// warranty have no outline — and measures nothing.
func (t *Topology) MeasureRegion(region *SemanticNode, boundaries *Boundaries, survey Survey) (Measurement, []Diagnostic) {
	if region == nil {
		return Measurement{}, nil
	}

	loops := slices.Collect(boundaries.Loops(region))

	frame := region.frame
	if frame == "" && len(loops) > 0 {
		frame = loops[0].frame
	}

	m := t.measuring(region.id, frame, region.span, survey)

	var rings []*outline
	for _, loop := range loops {
		assembled, ok := m.ring(loop)
		if !ok {
			continue
		}
		rings = append(rings, assembled)
	}

	if len(rings) != len(loops) {
		// A region is measured from all of its rings or from none of them.
		// Summing the ones which worked would report a courtyard building as
		// solid, with no figure saying which part of it was left out.
		m.lengthOf(rings)
		m.boundsOf(rings)
		return m.result, m.diags
	}

	m.combine(region, rings)

	return m.result, m.diags
}

// measuring starts a measurement of one subject in one frame.
func (t *Topology) measuring(subject, frame ID, at Span, survey Survey) *measurer {
	unit := frameUnit(survey.Registry, frame)

	m := &measurer{
		topology: t,
		survey:   survey,
		subject:  subject,
		span:     at,
		unit:     unit,
		result:   Measurement{subject: subject, unit: unit},
	}

	m.tolerance, m.declared = survey.Registry.Tolerance(survey.Tolerance)

	return m
}

// measurer measures one thing.
type measurer struct {
	reader

	// topology is what ids are resolved against.
	topology *Topology

	// survey is what the measurement is computed against.
	survey Survey

	// subject is the id of the thing being measured, and span where a
	// diagnostic about it as a whole points.
	subject ID
	span    Span

	// unit is the linear unit of the subject's frame, empty where the registry
	// declares no frame of that id, which leaves everything unmeasurable.
	unit Unit

	// tolerance is the declared tolerance corners are judged coincident and
	// rings judged planar against, and declared whether the registry declares it
	// at all.
	tolerance Tolerance
	declared  bool

	// result is what is being built.
	result Measurement
}

// applicable reports whether the tolerance can be applied to this subject at
// all: that the registry declares it, that the frame is one the registry
// declares, and that the two are in the same unit.
//
// It is the same predicate [assembler.applicable] is, for the same reason.
// Judging a ring planar against a tolerance in millimetres in a frame written in
// metres is off by a thousand, and a check which quietly did it would pass every
// shape put to it.
func (m *measurer) applicable() bool {
	return m.declared && m.unit != "" && m.tolerance.Unit == m.unit
}

// outline is one loop assembled and reduced to arithmetic: the corners it passes
// through, where they are, and what that makes it.
//
// The points are held twice, absolutely and relative to the minimum corner of
// the bounding box. The relative ones are what every sum is computed from, which
// is what keeps a building surveyed on a national grid from losing precision: a
// coordinate in the millions has fifteen significant figures of which the first
// seven are the same for every corner, and subtracting them before multiplying
// is what stops the difference between two of them being computed in the last
// two digits of a large number.
//
// The origin is the bounding box minimum rather than the first corner because a
// measurement must not depend on where a traversal started. The bounding box is
// a property of the set of corners and is the same whichever edge the ring is
// written from and whichever way round it runs.
type outline struct {
	// loop is the loop this was assembled from.
	loop *Loop

	// corners are the vertices of the ring, in traversal order, each once.
	corners []ID

	// points are where those corners are.
	points []Point

	// origin is the bounding box minimum, which shifted is measured from.
	origin Point

	// shifted are the points relative to origin.
	shifted []Point

	// normal is the Newell normal of the shifted points: twice the vector area
	// of the ring, whose direction is the plane's and whose magnitude is twice
	// the enclosed area.
	normal Point

	// magnitude is the length of normal, which is twice the area.
	magnitude float64

	// bounds is the axis-aligned bounding box of the points.
	bounds Box

	// perimeter is the total length of the ring's segments.
	perimeter float64

	// area is the enclosed area, and hasArea whether the ring is one which
	// encloses one at all.
	area    float64
	hasArea bool

	// centroid is the area centroid of the ring.
	centroid Point

	// axis is the axis of the normal's largest component, which is the one to
	// drop to project the ring onto a plane it does not collapse in.
	axis int

	// dimension is how many components its positions were written with.
	dimension int
}

// ring assembles a loop and reduces it to its corners, reporting where that
// could not be done at all.
//
// The second result says whether there is a ring to measure, which is a
// different question from whether it encloses an area: a ring whose corners are
// collinear is a ring, and reporting it as one is what lets the area diagnostic
// be about the shape rather than about the reading.
func (m *measurer) ring(loop *Loop) (*outline, bool) {
	assembly, diags := m.topology.Assemble(loop, m.survey.Positions, m.survey.Tolerance, m.survey.Registry)
	m.add(diags...)

	steps := assembly.Steps()
	if len(steps) == 0 {
		return nil, false
	}

	out := &outline{loop: loop}
	for _, step := range steps {
		out.corners = append(out.corners, step.From())
	}

	if !m.locate(loop, out) {
		return nil, false
	}

	out.measure()
	out.bounds.Unit = m.unit
	m.contribute(out.corners)

	if !assembly.Closed() {
		// The gap is already reported, by the pass whose job it is and with the
		// distance across it. What follows from it — that there is no area — is
		// the same mistake, and saying it again in this file's vocabulary would
		// send whoever reads it looking for a second thing to fix.
		out.hasArea = false
		return out, true
	}

	m.degenerate(out)

	return out, true
}

// locate reads where every corner of a ring is, reporting the corners which have
// no position to read and the ones written with a different number of components
// from the others.
//
// Both are reported once per ring, naming every corner they are about. A loop
// nobody has surveyed would otherwise produce one diagnostic per corner, saying
// the same thing about a model in which one thing is missing.
func (m *measurer) locate(loop *Loop, out *outline) bool {
	var missing []string
	var components [][]float64

	for _, corner := range out.corners {
		written, ok := m.at(corner)
		if !ok {
			missing = append(missing, string(corner))
			continue
		}
		components = append(components, written)
	}

	if len(missing) > 0 {
		m.add(Diagnostic{
			Severity: SeverityError,
			Span:     m.topology.namedAt(loop.id, loop.span),
			Message: fmt.Sprintf(
				"expected a position for every corner of the loop %s, found none for %s",
				geometricName(loopTag, loop.id), join(missing, "and"),
			),
			Hint: m.positionHint(),
		})
		return false
	}

	dimension := len(components[0])
	for i, written := range components {
		if len(written) == dimension {
			continue
		}

		m.add(Diagnostic{
			Severity: SeverityError,
			Span:     m.topology.namedAt(out.corners[i], loop.span),
			Message: fmt.Sprintf(
				"expected every corner of the loop %s to be written with %d components, found %s with %d",
				geometricName(loopTag, loop.id), dimension, out.corners[i], len(written),
			),
			Hint: "nothing here is padded: a corner written with fewer components than the others is a position in a " +
				"different space, not the same one with a zero left out",
		})
		return false
	}

	if dimension < 2 {
		m.add(Diagnostic{
			Severity: SeverityError,
			Span:     m.topology.namedAt(loop.id, loop.span),
			Message: fmt.Sprintf(
				"expected the corners of the loop %s to be written with at least two components, found %d",
				geometricName(loopTag, loop.id), dimension,
			),
			Hint: "a ring is a plane figure; corners written along one axis enclose nothing to measure",
		})
		return false
	}

	out.dimension = dimension
	for _, written := range components {
		out.points = append(out.points, asPoint(written))
	}

	return true
}

// positionHint says what a position has to be for anything to be measured from
// it, naming the unit where there is one to name.
func (m *measurer) positionHint() string {
	hint := "a position is read in the unit of its frame and nothing is converted"
	if m.unit == "" {
		return hint + "; this frame is not one the registry declares, so nothing here has a unit to be read in"
	}
	return fmt.Sprintf("%s; this frame is in %s, so a position written in another unit is not read at all", hint, m.unit)
}

// at is a vertex's position in the unit of the subject's frame, and whether it
// has one there.
func (m *measurer) at(vertex ID) ([]float64, bool) {
	value, ok := m.survey.Positions[vertex]
	if !ok || m.unit == "" || value.Unit() != m.unit {
		return nil, false
	}
	return value.Coordinate()
}

// contribute accumulates the accuracy of the claims the corners' positions were
// read from.
//
// A term shared between two corners — a georeference fit applied to every indoor
// fact alike — is counted once, which is [Budget]'s own arithmetic and the whole
// reason the claims are accumulated rather than their numbers
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)).
func (m *measurer) contribute(corners []ID) {
	for _, corner := range corners {
		if claim, ok := m.survey.Evidence[corner]; ok {
			m.result.budget.Add(claim)
		}
	}
}

// degenerate reports the shapes which are rings and enclose nothing an area can
// be computed of, and marks the ring as having no area where one of them holds.
//
// Each is a different mistake with a different fix, so each is its own
// diagnostic. A ring whose corners are not in one plane has to be re-surveyed or
// re-modelled; one which crosses itself has an edge naming the wrong corner; one
// whose corners are collinear is not a shape at all.
//
// A ring which passes through one corner twice is not among them. Two vertices
// no further apart than the tolerance are one corner, and a ring which reaches
// one of them twice is a corner where four edges meet — which
// [Topology.Assemble] has already reported as the branch it is, in the
// vocabulary of the ring rather than of the arithmetic over it.
func (m *measurer) degenerate(out *outline) {
	if !m.planar(out) {
		out.hasArea = false
		return
	}

	if m.crossing(out) {
		out.hasArea = false
		return
	}

	if out.hasArea && out.area == 0 {
		out.hasArea = false
		m.add(Diagnostic{
			Severity: SeverityError,
			Span:     m.topology.namedAt(out.loop.id, out.loop.span),
			Message: fmt.Sprintf(
				"expected the loop %s to enclose an area, found one of zero: its corners all lie on one line",
				geometricName(loopTag, out.loop.id),
			),
			Hint: "an area of zero here is the arithmetic being right about a shape which is wrong; a ring needs three " +
				"corners which are not collinear before it encloses anything",
		})
	}
}

// planar reports whether every corner of the ring lies in one plane, naming the
// corner furthest out of it where they do not.
//
// It is judged against the same declared tolerance closure is, and is not judged
// at all where that tolerance could not be applied — there is no default and no
// fallback, and a plane decided by a constant compiled in here would be this
// package inventing the one number the project is supposed to have written down
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
func (m *measurer) planar(out *outline) bool {
	if !m.applicable() || out.magnitude == 0 {
		return true
	}

	unit := pointScale(out.normal, 1/out.magnitude)
	plane := pointDot(out.shifted[0], unit)

	worst, at := 0.0, 0
	for i, point := range out.shifted {
		if out := math.Abs(pointDot(point, unit) - plane); out > worst {
			worst, at = out, i
		}
	}

	if worst <= m.tolerance.Value {
		return true
	}

	m.add(Diagnostic{
		Severity: SeverityError,
		Span:     m.topology.namedAt(out.corners[at], out.loop.span),
		Message: fmt.Sprintf(
			"expected every corner of the loop %s to lie in one plane within the tolerance %s, which is %s %s, found %s %s %s out of it",
			geometricName(loopTag, out.loop.id), m.tolerance.Name, decimal(m.tolerance.Value), m.tolerance.Unit,
			out.corners[at], decimal(worst), m.unit,
		),
		Hint: "an area is a property of a plane figure; the number a projection of a ring which is not planar would give " +
			"is smaller than the shape and is not the area of anything",
		Related: []RelatedLocation{
			{Span: m.tolerance.Span, Message: "the tolerance is declared here"},
		},
	})

	return false
}

// crossing reports every place the ring crosses itself, naming where the
// crossing is.
//
// The point is what makes it actionable. "This loop crosses itself" sends
// whoever reads it round the whole boundary; a coordinate sends them to the two
// walls which meet where no wall should.
//
// Segments which share a corner are not compared. Two walls meeting at a corner
// touch there by construction, and calling that a crossing would report every
// ring ever written.
func (m *measurer) crossing(out *outline) bool {
	var found bool

	count := len(out.shifted)
	for i := range count {
		for j := i + 1; j < count; j++ {
			if adjacent(i, j, count) {
				continue
			}

			at, crosses := intersection(
				out.shifted[i], out.shifted[(i+1)%count],
				out.shifted[j], out.shifted[(j+1)%count],
				out.axis,
			)
			if !crosses {
				continue
			}

			found = true
			m.add(Diagnostic{
				Severity: SeverityError,
				Span:     m.topology.namedAt(out.loop.id, out.loop.span),
				Message: fmt.Sprintf(
					"expected the loop %s not to cross itself, found the segment %s to %s crossing %s to %s at %s%s",
					geometricName(loopTag, out.loop.id),
					out.corners[i], out.corners[(i+1)%count],
					out.corners[j], out.corners[(j+1)%count],
					pointText(pointAdd(out.origin, at), out.dimension), unitSuffix(m.unit),
				),
				Hint: "a ring which crosses itself encloses no one region; the signed sum over it counts the parts either " +
					"side of the crossing against each other, which is a number and is not an area",
			})
		}
	}

	return found
}

// single records a measurement made from one ring.
func (m *measurer) single(out *outline) {
	m.result.dimension = out.dimension
	m.result.length, m.result.hasLength = out.perimeter, true
	m.result.bounds, m.result.hasBounds = out.bounds, true

	if !out.hasArea {
		return
	}

	m.result.area, m.result.hasArea = out.area, true
	m.result.centroid, m.result.hasCentroid = out.centroid, true
}

// combine records a measurement made from every ring bounding a region, with a
// ring nested inside another taken away rather than added.
func (m *measurer) combine(region *SemanticNode, rings []*outline) {
	m.lengthOf(rings)
	m.boundsOf(rings)

	if len(rings) == 0 {
		return
	}

	if len(rings) == 1 {
		// One ring is not a nesting. Going through the even-odd rule for it
		// would be the same arithmetic with a step which can only ever add.
		m.single(rings[0])
		return
	}

	if !m.coplanar(region, rings) {
		return
	}

	var area float64
	var weighted Point

	for i, one := range rings {
		if !one.hasArea {
			return
		}

		sign := 1.0
		if m.nesting(rings, i)%2 == 1 {
			sign = -1
		}

		area += sign * one.area
		weighted = pointAdd(weighted, pointScale(one.centroid, sign*one.area))
	}

	if area <= 0 {
		// Every ring is a hole, or the holes are bigger than what holds them.
		// Either way the rings are not the boundary of one region, and a
		// centroid computed by dividing by this is a point outside the shape.
		m.add(Diagnostic{
			Severity: SeverityError,
			Span:     m.span,
			Message: fmt.Sprintf(
				"expected the loops bounding %s to enclose an area, found %s%s once the rings nested inside others were taken away",
				nodeName(region), decimal(area), squareSuffix(m.unit),
			),
			Hint: "a ring inside another is a hole and is taken away; a region whose holes are as big as the rings " +
				"holding them is not one region's boundary",
		})
		return
	}

	m.result.area, m.result.hasArea = area, true
	m.result.centroid, m.result.hasCentroid = pointScale(weighted, 1/area), true
}

// lengthOf records the total length of every ring's segments, which is the
// length of the whole of a region's boundary.
func (m *measurer) lengthOf(rings []*outline) {
	if len(rings) == 0 {
		return
	}

	var total float64
	for _, one := range rings {
		total += one.perimeter
		m.result.dimension = one.dimension
	}

	m.result.length, m.result.hasLength = total, true
}

// boundsOf records the box which holds every ring.
func (m *measurer) boundsOf(rings []*outline) {
	if len(rings) == 0 {
		return
	}

	bounds := rings[0].bounds
	for _, one := range rings[1:] {
		for axis := range 3 {
			bounds.Min[axis] = math.Min(bounds.Min[axis], one.bounds.Min[axis])
			bounds.Max[axis] = math.Max(bounds.Max[axis], one.bounds.Max[axis])
		}
	}

	m.result.bounds, m.result.hasBounds = bounds, true
}

// coplanar reports whether every ring of a region lies in the same plane of the
// frame, naming the first which does not.
//
// Nesting is only meaningful between rings which share a plane. A courtyard is a
// hole in a floor plate because it is drawn in the plate; a ring on another storey
// is a different shape which happens to be inside this one seen from above, and
// subtracting it would report a building with a floor missing.
func (m *measurer) coplanar(region *SemanticNode, rings []*outline) bool {
	for _, one := range rings[1:] {
		if one.axis == rings[0].axis {
			continue
		}

		m.add(Diagnostic{
			Severity: SeverityError,
			Span:     m.span,
			Message: fmt.Sprintf(
				"expected every loop bounding %s to lie in one plane, found %s in another than %s",
				nodeName(region), geometricName(loopTag, one.loop.id), geometricName(loopTag, rings[0].loop.id),
			),
			Hint: "a region bounded by more than one ring is measured by nesting, and a ring inside another is only a " +
				"hole in it where the two share a plane",
		})
		return false
	}

	return true
}

// nesting counts how many of a region's other rings hold the one at index.
//
// The count and not merely whether it is held: a ring inside a hole is an island
// and is added back, which is the even-odd rule and is what makes a courtyard
// with a pavilion in it come out right.
func (m *measurer) nesting(rings []*outline, index int) int {
	var depth int

	for i, other := range rings {
		if i == index {
			continue
		}
		if other.holds(rings[index].shiftedTo(other)) {
			depth++
		}
	}

	return depth
}

// measure computes everything a ring's corners decide.
func (r *outline) measure() {
	r.bounds = boxOf(r.points)
	r.origin = r.bounds.Min

	r.shifted = make([]Point, 0, len(r.points))
	for _, point := range r.points {
		r.shifted = append(r.shifted, pointSub(point, r.origin))
	}

	count := len(r.shifted)
	for i, point := range r.shifted {
		next := r.shifted[(i+1)%count]

		r.perimeter += pointLength(pointSub(next, point))
		r.normal = pointAdd(r.normal, pointCross(point, next))
	}

	r.magnitude = pointLength(r.normal)
	r.axis = dominant(r.normal)

	if r.magnitude == 0 {
		// A ring with no vector area has no plane to read a projection axis
		// from, and it is exactly the ring which most needs one: a shape whose
		// two halves cancel is the reason the crossing has to be looked for. The
		// axis it reaches least far along is the one which cannot collapse it.
		r.axis = flattest(r.bounds)
		r.area, r.hasArea = 0, true
		r.centroid = r.bounds.Centre()
		return
	}

	r.area, r.hasArea = r.magnitude/2, true

	// The area centroid of a plane polygon, written so that it holds whichever
	// plane the ring lies in: each segment contributes the midpoint of the
	// triangle it makes with the origin, weighted by that triangle's signed area
	// along the ring's own normal. Reversing the traversal negates the normal and
	// every cross product with it, so the weights are unchanged and the answer
	// does not depend on which way round the ring was written.
	var weighted Point
	for i, point := range r.shifted {
		next := r.shifted[(i+1)%count]
		weight := pointDot(pointCross(point, next), r.normal)
		weighted = pointAdd(weighted, pointScale(pointAdd(point, next), weight))
	}

	r.centroid = pointAdd(r.origin, pointScale(weighted, 1/(3*r.magnitude*r.magnitude)))
}

// shiftedTo is one of the ring's corners expressed relative to another ring's
// origin, which is the point a containment test against that ring is made with.
func (r *outline) shiftedTo(other *outline) Point {
	return pointSub(r.points[0], other.origin)
}

// holds reports whether a point projected onto the ring's plane falls inside it,
// by counting the crossings of a ray cast from it.
//
// The ring is projected by dropping the axis its normal is largest along, which
// is the one projection of it which cannot collapse it to a line.
func (r *outline) holds(point Point) bool {
	first, second := axes(r.axis)

	x, y := point[first], point[second]

	var inside bool
	count := len(r.shifted)

	for i := range count {
		a, b := r.shifted[i], r.shifted[(i+1)%count]

		ax, ay := a[first], a[second]
		bx, by := b[first], b[second]

		if (ay > y) == (by > y) {
			continue
		}

		if x < ax+(y-ay)/(by-ay)*(bx-ax) {
			inside = !inside
		}
	}

	return inside
}

// edge measures one edge, reporting an edge whose ends are at one point.
func (m *measurer) edge(edge *Edge) {
	var missing []string

	from, ok := m.at(edge.start)
	if !ok {
		missing = append(missing, string(edge.start))
	}

	to, ok := m.at(edge.end)
	if !ok {
		missing = append(missing, string(edge.end))
	}

	if len(missing) > 0 {
		m.add(Diagnostic{
			Severity: SeverityError,
			Span:     m.edgeAt(edge),
			Message: fmt.Sprintf(
				"expected a position for both ends of the edge %s, found none for %s",
				geometricName(edgeTag, edge.id), join(missing, "and"),
			),
			Hint: m.positionHint(),
		})
		return
	}

	if len(from) != len(to) {
		m.add(Diagnostic{
			Severity: SeverityError,
			Span:     m.edgeAt(edge),
			Message: fmt.Sprintf(
				"expected both ends of the edge %s to be written with the same number of components, found %s with %d and %s with %d",
				geometricName(edgeTag, edge.id), edge.start, len(from), edge.end, len(to),
			),
			Hint: "nothing here is padded: a position written with fewer components than the other end is a position in " +
				"a different space, not the same one with a zero left out",
		})
		return
	}

	start, end := asPoint(from), asPoint(to)

	m.result.dimension = len(from)
	m.result.length, m.result.hasLength = pointLength(pointSub(end, start)), true
	m.result.centroid, m.result.hasCentroid = pointScale(pointAdd(start, end), 0.5), true
	m.result.bounds, m.result.hasBounds = boxOf([]Point{start, end}), true
	m.result.bounds.Unit = m.unit

	m.contribute([]ID{edge.start, edge.end})

	if m.result.length != 0 {
		return
	}

	m.add(Diagnostic{
		Severity: SeverityError,
		Span:     m.edgeAt(edge),
		Message: fmt.Sprintf(
			"expected the edge %s to have an extent, found %s and %s at the same point, %s%s",
			geometricName(edgeTag, edge.id), edge.start, edge.end, pointText(start, len(from)), unitSuffix(m.unit),
		),
		Hint: "an edge with no extent gives a loop through it no direction; two corners surveyed to one coordinate are " +
			"one corner",
	})
}

// edgeAt is where a diagnostic about an edge as a whole points.
func (m *measurer) edgeAt(edge *Edge) Span {
	return m.topology.namedAt(edge.id, edge.span)
}

// boxOf is the axis-aligned box which holds every point.
func boxOf(points []Point) Box {
	if len(points) == 0 {
		return Box{}
	}

	box := Box{Min: points[0], Max: points[0]}
	for _, point := range points[1:] {
		for axis := range 3 {
			box.Min[axis] = math.Min(box.Min[axis], point[axis])
			box.Max[axis] = math.Max(box.Max[axis], point[axis])
		}
	}

	return box
}

// asPoint reads written components as a point, leaving the components which were
// not written at zero.
//
// Padding here is not the padding [measurer.locate] refuses. Every corner of one
// ring has been checked to carry the same number of components as the others, so
// what is added is a shared zero axis which no difference between two of them
// ever reaches, rather than an axis one corner stated and another did not.
func asPoint(components []float64) Point {
	var point Point
	for i := range min(len(components), len(point)) {
		point[i] = components[i]
	}
	return point
}

// adjacent reports whether two segments of a ring of count segments share a
// corner, which every consecutive pair does and the first and last do.
func adjacent(i, j, count int) bool {
	return j == i+1 || (i == 0 && j == count-1)
}

// dominant is the axis a vector is largest along, which is the axis to drop to
// project a ring onto a plane it does not collapse in.
//
// Ties go to the lower axis, so that the projection is decided by the vector and
// never by the order anything was walked in.
func dominant(v Point) int {
	axis := 0
	for i := 1; i < len(v); i++ {
		if math.Abs(v[i]) > math.Abs(v[axis]) {
			axis = i
		}
	}
	return axis
}

// flattest is the axis a box reaches least far along, which is the axis to drop
// to project a shape which has no plane of its own.
//
// Ties go to the lower axis, for the reason [dominant]'s do: the projection is
// decided by the shape and never by the order anything was walked in.
func flattest(box Box) int {
	size := box.Size()

	axis := 0
	for i := 1; i < len(size); i++ {
		if size[i] < size[axis] {
			axis = i
		}
	}
	return axis
}

// axes are the two axes which remain when one is dropped, in ascending order.
func axes(dropped int) (int, int) {
	switch dropped {
	case 0:
		return 1, 2
	case 1:
		return 0, 2
	default:
		return 0, 1
	}
}

// intersection reports where two segments cross, projected onto the plane which
// drops the given axis, and whether they cross at all.
//
// A crossing is a point strictly inside both segments or an end of one strictly
// inside the other. Two segments which merely share an end are not crossing —
// that is a corner — and the caller has already excluded the pairs which do.
func intersection(a, b, c, d Point, dropped int) (Point, bool) {
	first, second := axes(dropped)

	ax, ay := a[first], a[second]
	bx, by := b[first], b[second]
	cx, cy := c[first], c[second]
	dx, dy := d[first], d[second]

	denominator := (bx-ax)*(dy-cy) - (by-ay)*(dx-cx)
	if denominator == 0 {
		return Point{}, false
	}

	t := ((cx-ax)*(dy-cy) - (cy-ay)*(dx-cx)) / denominator
	u := ((cx-ax)*(by-ay) - (cy-ay)*(bx-ax)) / denominator

	if t < 0 || t > 1 || u < 0 || u > 1 {
		return Point{}, false
	}

	return pointAdd(a, pointScale(pointSub(b, a), t)), true
}

// pointAdd adds two points component by component.
func pointAdd(a, b Point) Point {
	return Point{a[0] + b[0], a[1] + b[1], a[2] + b[2]}
}

// pointSub subtracts one point from another component by component.
func pointSub(a, b Point) Point {
	return Point{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

// pointScale multiplies every component of a point by a factor.
func pointScale(a Point, factor float64) Point {
	return Point{a[0] * factor, a[1] * factor, a[2] * factor}
}

// pointDot is the dot product of two points read as vectors.
func pointDot(a, b Point) float64 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}

// pointCross is the cross product of two points read as vectors.
func pointCross(a, b Point) Point {
	return Point{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

// pointLength is the length of a point read as a vector.
func pointLength(a Point) float64 {
	return math.Sqrt(pointDot(a, a))
}

// pointText writes the first n components of a point the way a coordinate is
// written in a file.
func pointText(point Point, n int) string {
	if n < 1 || n > len(point) {
		n = len(point)
	}

	written := make([]string, 0, n)
	for _, component := range point[:n] {
		written = append(written, decimal(component))
	}

	return "(" + strings.Join(written, " ") + ")"
}

// unitSuffix writes a unit after a figure, and nothing where there is no unit to
// write.
func unitSuffix(unit Unit) string {
	if unit == "" {
		return ""
	}
	return " " + string(unit)
}

// squareSuffix writes a unit squared after an area, which is what an area is in.
func squareSuffix(unit Unit) string {
	if unit == "" {
		return ""
	}
	return " " + string(unit) + "²"
}
