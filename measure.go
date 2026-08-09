// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"fmt"
	"iter"
	"math"
	"slices"
	"strings"
)

// Evidence is the claim each position a measurement was computed from was read
// from, keyed by the id of the thing that position belongs to: a vertex's own id
// for where the corner is, and an edge's for where the centre of the arc it
// bends along is.
//
// It is the other half of [Positions] and of [Curvature], supplied for the same
// reason: which predicate carries a position is vocabulary the consuming
// repository owns and not something the engine knows
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
// What it adds is the provenance a computed answer inherits. An area is only as
// well known as the corners and the centres it was computed from, and a budget
// accumulated from these claims is what lets the answer say so
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)).
//
// An id which is absent contributes nothing to the budget. The geometry is
// still computed — a position with no claim behind it is a caller which filled
// one map and not the other, which [Survey.Place] and [Survey.Bend] exist to
// prevent.
type Evidence map[ID]*Claim

// Survey is what a measurement is computed against: where the corners are, the
// claims which put them there, the tolerance corners are judged coincident
// against, and the registry the frames and their units are read from.
//
// They travel together because every measurement needs them, and because an
// answer computed against some of them could not say what it was computed
// against. A size is not a fact about a shape alone: it is a fact about
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

	// Curvature is the arc each edge which bends bends along. An edge which is
	// absent is straight.
	Curvature Curvature

	// Tolerance is the name — never a number — of the tolerance corners are
	// judged coincident and rings judged planar against.
	Tolerance string

	// Chord is the name — never a number — of the tolerance a curve is drawn
	// to where an answer cannot be reached without straight segments: an
	// overlay, a setback, a fit, and the nesting of several rings one of which
	// bends.
	//
	// Empty is a survey which refuses such a question rather than answering it
	// at a resolution nobody named, which is what every caller which named no
	// chord gets and what the engine did before there was a name to give. It is
	// never a fallback and never a default
	// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)): how closely
	// a curve has to be followed is a decision a project makes, and a figure
	// computed to a resolution the engine picked would be an approximation
	// nobody could judge.
	//
	// Nothing which can be computed from the arc itself reads it. An area, a
	// length, a centroid and a bounding box are computed from the
	// parameterisation, so naming a chord does not move them and not naming one
	// does not stop them.
	Chord string

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

	// chord is the declared chord tolerance the rings were drawn to on the way
	// to the answer, and deviation how far the worst of those segments fell
	// from the curve it stood in for.
	//
	// Both are zero for a measurement no curve was drawn for, which is every
	// measurement of a straight shape and every measurement of a single curved
	// ring: the figures come from the arc itself, and nothing was approximated
	// to reach them.
	chord     Tolerance
	deviation float64

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

// ChordTolerance returns the declared tolerance the rings were drawn to on the
// way to the answer, and whether anything was drawn at all.
//
// Almost nothing is. Every figure a measurement reports is computed from the arc
// itself, so the only thing a drawing is ever needed for is deciding which of
// several rings holds which — the even-odd nesting, which is taken at the
// corners and cannot be taken at the corners of a ring which bulges past one
// ([measurer.depths]). A measurement of a straight shape and a measurement of a
// single curved ring both come back with nothing drawn.
//
// Where something was drawn, this and [Measurement.Deviation] are what say so.
// An answer which silently chose a resolution for somebody's boundary is the
// approximation an arc kept as an arc exists to prevent, and the figures are no
// use to a caller who cannot tell one from the other.
func (m Measurement) ChordTolerance() (Tolerance, bool) { return m.chord, m.chord.Name != "" }

// Deviation returns how far the worst segment of that drawing falls from the
// curve it stands in for, in the frame's linear unit.
//
// It is what was achieved rather than what was asked for, and it is always
// within [Measurement.ChordTolerance]: a curve is divided into a whole number of
// segments, so an arc which needs two and a bit gets three and follows the curve
// more closely than it had to.
//
// It bounds the nesting and not the figures. The area of a curved ring is the
// area of the arc, exact whatever this says; what the drawing decided is which
// rings were taken away from which.
func (m Measurement) Deviation() float64 { return m.deviation }

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

// Report writes the figures and, under them, the accuracy they rest on: the
// combined uncertainty of the corners where there is one, and every term which
// went into it.
//
// It is the detail under [Measurement.String] rather than a second rendering of
// the same thing, and it is here rather than in whatever prints it so that a
// caller reporting a measurement and a command reporting one write the same
// words. The terms are what make the figure actionable — "±0.006 m" is a number
// nobody can act on, and "the georeference is most of it, and three corners
// share it" says what to re-measure ([Budget]).
//
// The uncertainty is of the corners and not of the area, exactly as
// [Measurement.Budget] says. Nothing here reduces it to a tolerance on a figure.
func (m Measurement) Report() string {
	var out strings.Builder

	out.WriteString(m.String())

	combined, err := m.budget.Combined()
	switch {
	case err == nil:
		fmt.Fprintf(&out, "\n  corners known to %s", combined)
	case errors.Is(err, ErrEmptyBudget):
		// A measurement computed from claims which stated no accuracy is one
		// thing; one computed from no claims at all is another, and a report
		// which said nothing here would read as the first.
		out.WriteString("\n  nothing states how well the corners are known")
	default:
		fmt.Fprintf(&out, "\n  the corners cannot be combined into one figure: %v", err)
	}

	for _, term := range m.budget.Terms() {
		out.WriteString("\n    ")
		out.WriteString(term.String())
	}

	return out.String()
}

// printed is how many components of a point to write, which is how many the
// positions were written with.
func (m Measurement) printed() int {
	if m.dimension < 1 || m.dimension > 3 {
		return 3
	}
	return m.dimension
}

// MeasureVertex measures one corner: where it is, and how far it reaches, which
// for a point is the same answer twice.
//
// A corner has no extent, so it has no length and encloses no area, and both
// come back as no answer rather than as a zero. Its centroid is the point
// itself, which is [Measurement.Centroid]'s own definition one dimension further
// down than the midpoint of an edge, and its bounding box is the box of zero
// extent there. Reporting both is what lets a caller ask how far anything
// reaches without first asking which family it named.
//
// The position is read in the unit of the vertex's frame and nothing is
// converted, exactly as it is for an edge
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)). A corner nobody
// has surveyed yet is a diagnostic and no figures, because a position which was
// never claimed is unknown rather than the origin.
func (t *Topology) MeasureVertex(vertex *Vertex, survey Survey) (Measurement, []Diagnostic) {
	if vertex == nil {
		return Measurement{}, nil
	}

	m := t.measuring(vertex.id, vertex.frame, t.namedAt(vertex.id, vertex.span), survey)
	m.vertex(vertex)

	return m.result, m.diags
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
// A node which declares the geometry form `line` is read as open runs instead.
// Its loops are the same authoring — ordered edges, refused for a branch, a
// doubled edge or a shape in two pieces — and are walked as chains rather than
// as rings, so what comes back is how far the run reaches and the box around it,
// with no area and no centroid. A door, a railing and a wall run are each that
// shape, and requiring one of them to close would be requiring it to be
// something else.
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

	open := drawnAsLine(region)

	var rings []*outline
	for _, loop := range loops {
		assembled, ok := m.assembled(loop, open)
		if !ok {
			continue
		}
		rings = append(rings, assembled)
	}

	if len(rings) != len(loops) {
		// A region is measured from all of its rings or from none of them, and
		// that holds for every figure and not only for the area. A length summed
		// over the rings which happened to assemble is a boundary with a piece
		// missing and no figure saying which piece; a box drawn round them is a
		// region which reaches further than it says it does. Both are wrong in
		// the direction which reads as an answer, so neither is reported. Why is
		// already on the diagnostics from the ring which did not assemble.
		return m.result, m.diags
	}

	if open {
		m.along(rings)
		return m.result, m.diags
	}

	m.combine(region, rings)

	return m.result, m.diags
}

// drawnAsLine reports whether a node declares the geometry form which is an open
// run of edges rather than a region.
//
// It is one predicate rather than the comparison written wherever it is needed,
// because the two places which read it — the measurement and the region — have
// to agree. A node measured as a chain and read as a ring would be one shape
// with two answers, and the second of them would be a gap reported against a
// door for not being a room.
func drawnAsLine(node *SemanticNode) bool {
	if node == nil {
		return false
	}

	geometry, _ := node.Geometry()

	return geometry == GeometryLine
}

// Measure measures whatever an id named, whichever family holds it: a semantic
// node from the loops which bound it, a loop from the ring its edges traverse, an
// edge from its two ends, a vertex from where it is.
//
// It is the whole of the dimensional question in one call, and it is here rather
// than left to each caller because the alternative is that every consuming
// repository writes the same switch. A caller which dispatched for itself would
// have to know that a region is measured through its boundaries and a loop is
// not, that a node with no outline measures nothing rather than nothing being
// wrong, and which of the four calls takes which argument — and a second
// implementation of that is a second set of answers the day one of them is
// missed.
//
// The survey is the caller's, because which predicate carries a position and
// which tolerance corners are judged coincident against are the consuming
// repository's vocabulary and never the engine's
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
// [Graph.Corners] is what says which corners have to be in it.
//
// Nothing is written back. Every figure is recomputed from the claims each time
// it is asked for, which is what makes it unable to disagree with the geometry it
// describes ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// An entity of no family — a nil, or a type from outside this package — measures
// nothing and reports nothing, because there is no question there to refuse.
func (g *Graph) Measure(entity Entity, survey Survey) (Measurement, []Diagnostic) {
	if g == nil {
		return Measurement{}, nil
	}

	switch subject := entity.(type) {
	case *SemanticNode:
		return g.Topology().MeasureRegion(subject, g.Boundaries(), survey)
	case *Loop:
		return g.Topology().MeasureLoop(subject, survey)
	case *Edge:
		return g.Topology().MeasureEdge(subject, survey)
	case *Vertex:
		return g.Topology().MeasureVertex(subject, survey)
	}

	return Measurement{}, nil
}

// Corners iterates the vertices a measurement of entity is computed from, each
// yielded once and in the order the thing reaches them.
//
// It is the other half of [Graph.Measure]: a survey which is missing one of these
// is a measurement which cannot be made, and one built by hand from a guess at
// which corners matter is the way that happens. A caller places a position for
// each of these with [Survey.Place] and has surveyed exactly what the answer
// needs.
//
// A semantic node reaches them through its loops and its edges, which is
// [Boundaries.Vertices]; a loop reaches them through the edges it names, in the
// order it names them and start before end; an edge is its two ends; a vertex is
// itself. An entity of no family reaches none.
func (g *Graph) Corners(entity Entity) iter.Seq[*Vertex] {
	if g == nil {
		return sequence[*Vertex](nil)
	}

	switch subject := entity.(type) {
	case *SemanticNode:
		return g.Boundaries().Vertices(subject)
	case *Loop:
		return sequence(g.Topology().cornersOf(subject.Edges()))
	case *Edge:
		start, end := subject.Vertices()
		return sequence(g.Topology().cornersOf([]ID{}, start, end))
	case *Vertex:
		return sequence([]*Vertex{subject})
	}

	return sequence[*Vertex](nil)
}

// Bounding iterates the edges a measurement of entity is computed across, each
// yielded once and in the order the thing reaches them.
//
// It is [Graph.Corners] for the other family, and it exists for the same reason:
// an edge is where a curve is written, so a caller resolving the arcs of a shape
// has to reach exactly the edges the answer will be read across. One built by
// hand from a guess is a shape read partly as arcs and partly as the straight
// lines between their ends, and nothing in the answer would say which was which.
//
// A semantic node reaches them through its loops, which is [Boundaries.Edges]; a
// loop reaches them through the ids it names, in the order it names them; an
// edge is itself; a vertex reaches none, because a corner has no side to bend.
// An entity of no family reaches none.
func (g *Graph) Bounding(entity Entity) iter.Seq[*Edge] {
	if g == nil {
		return sequence[*Edge](nil)
	}

	switch subject := entity.(type) {
	case *SemanticNode:
		return g.Boundaries().Edges(subject)
	case *Loop:
		return sequence(g.Topology().edgesOf(subject.Edges()))
	case *Edge:
		return sequence([]*Edge{subject})
	case *Vertex:
		return sequence[*Edge](nil)
	}

	return sequence[*Edge](nil)
}

// cornersOf is the vertices reached through a list of edges and then through a
// list of vertices named directly, each held once and in the order they were
// reached.
//
// An id which names nothing this model holds is passed over rather than reported.
// A loop naming an edge which is not there, and an edge naming a corner which is
// not, are load errors already reported against the form which wrote them, and a
// second diagnostic from a survey being assembled would say the same thing in the
// vocabulary of the arithmetic instead of the file.
//
// What has been reached is tracked in a set rather than by scanning what has been
// collected, so a ring of a thousand corners costs a thousand lookups and not half
// a million comparisons. The order is still the order they were reached in: the
// set decides whether to append and never what comes back.
func (t *Topology) cornersOf(edges []ID, vertices ...ID) []*Vertex {
	var out []*Vertex

	reached := make(map[ID]struct{}, len(edges)*2+len(vertices))

	held := func(id ID) {
		if _, seen := reached[id]; seen {
			return
		}

		vertex, ok := t.Vertex(id)
		if !ok {
			return
		}

		reached[id] = struct{}{}
		out = append(out, vertex)
	}

	for _, id := range edges {
		edge, ok := t.Edge(id)
		if !ok {
			continue
		}
		start, end := edge.Vertices()
		held(start)
		held(end)
	}

	for _, id := range vertices {
		held(id)
	}

	return out
}

// edgesOf is the edges a list of ids names, each held once and in the order they
// were named.
//
// An id which names nothing this model holds is passed over rather than
// reported, for the reason [Topology.cornersOf] passes one over: a loop naming
// an edge which is not there is a load error already reported against the form
// which wrote it.
func (t *Topology) edgesOf(ids []ID) []*Edge {
	out := make([]*Edge, 0, len(ids))

	reached := make(map[ID]struct{}, len(ids))

	for _, id := range ids {
		if _, seen := reached[id]; seen {
			continue
		}

		edge, ok := t.Edge(id)
		if !ok {
			continue
		}

		reached[id] = struct{}{}
		out = append(out, edge)
	}

	return out
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

	// edges are the edges the traversal ran through, one per corner and in the
	// same order: the edge at an index leaves the corner at that index.
	edges []*Edge

	// reversed is whether the traversal ran through each of those edges against
	// the order it was written, one per edge and in the same order.
	//
	// It is the traversal's answer and not the edge's. Two rings either side of
	// one edge run through it opposite ways, so a run of a boundary which did
	// not carry this could only be read back by comparing its corners with the
	// edge's own vertices — which is the re-derivation [BoundarySegment] exists
	// to make unnecessary.
	reversed []bool

	// bends are the arcs those edges bend along, one per edge and in the same
	// order, nil wherever an edge is straight — which is what almost every edge
	// is. Each is oriented the way the traversal ran through it and not the way
	// the edge was written, so a bulge shared by two rings adds to one area and
	// takes away from the other with nothing having to decide which.
	bends []*bend

	// curved is whether any of them is an arc, which is what decides whether a
	// figure has to account for the area between a chord and the curve over it.
	curved bool

	// closed is whether the ring closes, which a tessellation of it reports and
	// an area needs.
	closed bool

	// open is whether the edges were read as an open run rather than as a ring,
	// which is what a node drawn as a line declares.
	//
	// It changes two things and nothing else. points carries one corner more
	// than there are edges, because a run ends at a corner no step leaves; and
	// there is no area, no centroid and no plane, because a chain encloses
	// nothing. Everything else — the bounds, the length along it, the accuracy
	// behind its corners and the edge each straight run was written as — is read
	// exactly as a ring's is.
	open bool

	// last is the corner the traversal ended at, finish where that is and
	// finished whether there was a position to read it at.
	//
	// It is the traversal's own end and not the last edge's, which the two
	// differ in wherever the ring runs through that edge backwards. A ring which
	// closes ends at the corner it began at, which is already the first of
	// points; one which does not ends somewhere no corner of the ring names, and
	// a tessellation which left that out would stop a whole edge short of where
	// the traversal got to.
	last     ID
	finish   Point
	finished bool

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
	return m.assembled(loop, false)
}

// assembled reads a loop as a ring or, where open, as the open chain its edges
// walk — which is what a node declaring the geometry form `line` is drawn as.
//
// The open reading is [measurer.ring] with the closure requirement dropped and
// nothing else changed: the same edges, the same corners, the same arcs, the
// same accuracy, and the same refusal of a branch, a doubled edge or a shape in
// two pieces. A chain encloses nothing, so what comes back has a length and a
// bounding box and no area — which is not a failure to measure it but the whole
// of what there is to measure.
//
// It takes the reading as an argument rather than being two methods because its
// callers take it as one: which of the two a node gets is its declared geometry,
// which is read once and threaded through every loop bounding it.
func (m *measurer) assembled(loop *Loop, open bool) (*outline, bool) {
	assemble := m.topology.Assemble
	if open {
		assemble = m.topology.AssembleRun
	}

	assembly, diags := assemble(loop, m.survey.Positions, m.survey.Tolerance, m.survey.Registry)
	m.add(diags...)

	steps := assembly.Steps()
	if len(steps) == 0 {
		return nil, false
	}

	out := &outline{loop: loop, open: open, closed: assembly.Closed(), bends: make([]*bend, len(steps))}
	for _, step := range steps {
		out.corners = append(out.corners, step.From())
		out.edges = append(out.edges, step.Edge())
		out.reversed = append(out.reversed, step.Reversed())
	}

	// The end of the walk is read before the corners are, because a run ends at
	// a corner no step leaves and a run with no position for it is a run which
	// cannot be drawn — which is [measurer.locate]'s diagnostic and not a corner
	// quietly left out of the figure.
	out.last = steps[len(steps)-1].To()

	if !m.locate(loop, out) {
		return nil, false
	}

	if written, ok := m.at(out.last); ok {
		out.finish, out.finished = asPoint(written), true
	}

	if !m.curves(out, steps) {
		return nil, false
	}

	out.measure()
	out.bounds.Unit = m.unit
	m.contribute(out.walked())
	m.contribute(edgeIDs(out.edges))

	if open || !assembly.Closed() {
		// The gap is already reported, by the pass whose job it is and with the
		// distance across it. What follows from it — that there is no area — is
		// the same mistake, and saying it again in this file's vocabulary would
		// send whoever reads it looking for a second thing to fix. A run has no
		// area for a different reason and no diagnostic at all: a chain is not a
		// region, and saying so would be reporting a model in which nothing is
		// wrong.
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
//
// The corners are the ones the walk passes through, which for an open run is one
// more than there are edges: a chain ends somewhere no step leaves, and a corner
// left out of this would be a run drawn a whole edge short with nothing saying
// so.
func (m *measurer) locate(loop *Loop, out *outline) bool {
	var missing []string
	var components [][]float64

	walked := out.walked()

	for _, corner := range walked {
		written, ok := m.at(corner)
		if !ok {
			missing = append(missing, string(corner))
			continue
		}
		components = append(components, written)
	}

	shape := "loop"
	if out.open {
		shape = "run"
	}

	if len(missing) > 0 {
		m.add(Diagnostic{
			Severity: SeverityError,
			Span:     m.topology.namedAt(loop.id, loop.span),
			Message: fmt.Sprintf(
				"expected a position for every corner of the %s %s, found none for %s",
				shape, geometricName(loopTag, loop.id), join(missing, "and"),
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
			Span:     m.topology.namedAt(walked[i], loop.span),
			Message: fmt.Sprintf(
				"expected every corner of the %s %s to be written with %d components, found %s with %d",
				shape, geometricName(loopTag, loop.id), dimension, walked[i], len(written),
			),
			Hint: "nothing here is padded: a corner written with fewer components than the others is a position in a " +
				"different space, not the same one with a zero left out",
		})
		return false
	}

	if dimension < 2 {
		hint := "a ring is a plane figure; corners written along one axis enclose nothing to measure"
		if out.open {
			hint = "a run is drawn in a plane; corners written with one component are positions along a line and not " +
				"anywhere a sheet can put them"
		}

		m.add(Diagnostic{
			Severity: SeverityError,
			Span:     m.topology.namedAt(loop.id, loop.span),
			Message: fmt.Sprintf(
				"expected the corners of the %s %s to be written with at least two components, found %d",
				shape, geometricName(loopTag, loop.id), dimension,
			),
			Hint: hint,
		})
		return false
	}

	out.dimension = dimension
	m.result.dimension = dimension
	for _, written := range components {
		out.points = append(out.points, asPoint(written))
	}

	return true
}

// curves reads the arc every edge of a ring which bends bends along, oriented
// the way the traversal ran through it.
//
// An edge with no arc is left as the straight edge it is, and its two ends are
// never read here — a ring of straight edges is measured from exactly the
// corners it always was, and nothing about this pass touches it.
func (m *measurer) curves(out *outline, steps []Step) bool {
	for i, step := range steps {
		edge := step.Edge()
		if _, ok := m.survey.Curvature[edge.ID()]; !ok {
			continue
		}

		from, to, _, ok := m.ends(edge)
		if !ok {
			return false
		}

		curve, _, ok := m.bend(edge, from, to, step.Reversed())
		if !ok {
			return false
		}

		out.bends[i] = curve
		out.curved = true
	}

	return true
}

// edgeIDs is the ids of a ring's edges, which is what the claims behind their
// arcs are keyed by.
func edgeIDs(edges []*Edge) []ID {
	ids := make([]ID, 0, len(edges))
	for _, edge := range edges {
		ids = append(ids, edge.ID())
	}
	return ids
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
		return m.coplanarBends(out, unit, plane)
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

// coplanarBends reports every arc of a ring which is not in the ring's plane,
// which is an arc whose centre is out of it.
//
// The two ends of an arc are corners of the ring and have already been measured
// against the plane by [measurer.planar]; the centre is the only other point the
// parameterisation states, and it is what tilts the curve out of the shape when
// it is wrong. A ring whose corners lie in one plane and whose bulge leaves it
// is not a plane figure, and the area a projection of it would give is not the
// area of anything.
func (m *measurer) coplanarBends(out *outline, unit Point, plane float64) bool {
	ok := true

	for i, curve := range out.bends {
		if curve == nil {
			continue
		}

		away := math.Abs(pointDot(pointSub(curve.centre, out.origin), unit) - plane)
		if away <= m.tolerance.Value {
			continue
		}

		ok = false
		m.add(Diagnostic{
			Severity: SeverityError,
			Span:     curve.span,
			Message: fmt.Sprintf(
				"expected the arc the edge %s bends along to lie in the plane of the loop %s within the tolerance %s, "+
					"which is %s %s, found its centre %s %s out of it",
				geometricName(edgeTag, out.edges[i].id), geometricName(loopTag, out.loop.id),
				m.tolerance.Name, decimal(m.tolerance.Value), m.tolerance.Unit, decimal(away), m.unit,
			),
			Hint: "an arc in a plane of its own is a curve leaving the shape and coming back to it; a ring is a plane " +
				"figure, and its bulges are in the same plane its corners are",
			Related: []RelatedLocation{
				{Span: m.tolerance.Span, Message: "the tolerance is declared here"},
			},
		})
	}

	return ok
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

			if out.bends[i] != nil || out.bends[j] != nil {
				// A chord is not the edge. Two curves whose chords cross may
				// not, and two which cross may have chords which do not, so a
				// pair with an arc in it is judged as the arc it is by
				// [measurer.crossingBends] rather than twice and inconsistently.
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

	if m.crossingBends(out) {
		found = true
	}

	return found
}

// crossingBends reports every place an arc of a ring crosses another of its
// edges, naming where the crossing is.
//
// It is the same question [measurer.crossing] answers for two straight edges and
// it is asked of the curves themselves: two circles in one plane meet in at most
// two points and a circle meets a straight edge in at most two, and each of those
// is a crossing exactly when it lies within both sweeps.
//
// Two edges which meet only where they share a corner are not crossing there,
// and neither is a curve which runs up against another and turns away without
// passing through it. Both are judged against the declared tolerance, and so this
// is not judged at all where that tolerance could not be applied, for the reason
// [measurer.planar] is not
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
func (m *measurer) crossingBends(out *outline) bool {
	if !out.curved || !m.applicable() || out.magnitude == 0 {
		return false
	}

	normal := pointScale(out.normal, 1/out.magnitude)

	var found bool

	count := len(out.shifted)
	for i := range count {
		for j := i + 1; j < count; j++ {
			if out.bends[i] == nil && out.bends[j] == nil {
				continue
			}

			for _, at := range m.meetings(out, normal, i, j) {
				found = true
				m.add(Diagnostic{
					Severity: SeverityError,
					Span:     m.topology.namedAt(out.loop.id, out.loop.span),
					Message: fmt.Sprintf(
						"expected the loop %s not to cross itself, found the edge %s crossing %s at %s%s",
						geometricName(loopTag, out.loop.id),
						geometricName(edgeTag, out.edges[i].id), geometricName(edgeTag, out.edges[j].id),
						pointText(at, out.dimension), unitSuffix(m.unit),
					),
					Hint: "a ring which crosses itself encloses no one region; the signed sum over it counts the parts " +
						"either side of the crossing against each other, which is a number and is not an area",
				})
			}
		}
	}

	return found
}

// meetings is every point at which two edges of a ring cross, where at least one
// of them is an arc.
//
// A point the two edges share as a corner is not one of them, and neither is a
// pair of points no further apart than the tolerance: two curves which touch and
// turn away meet the arithmetic at one point twice, and reporting that as a
// crossing would report every fillet ever drawn.
func (m *measurer) meetings(out *outline, normal Point, i, j int) []Point {
	count := len(out.shifted)

	first, second := out.bends[i], out.bends[j]

	var candidates []Point
	switch {
	case first != nil && second != nil:
		candidates = circlesMeeting(first, second, normal)
	case first != nil:
		candidates = circleMeetingSegment(first, out.points[j], out.points[(j+1)%count], m.tolerance.Value)
	default:
		candidates = circleMeetingSegment(second, out.points[i], out.points[(i+1)%count], m.tolerance.Value)
	}

	if len(candidates) == 2 && pointLength(pointSub(candidates[0], candidates[1])) <= m.tolerance.Value {
		return nil
	}

	var shared []Point
	for _, one := range [...]int{i, (i + 1) % count} {
		for _, other := range [...]int{j, (j + 1) % count} {
			if one == other {
				shared = append(shared, out.points[one])
			}
		}
	}

	var crossings []Point
	for _, at := range candidates {
		if !onBoth(at, first, second) || near(at, shared, m.tolerance.Value) {
			continue
		}
		crossings = append(crossings, at)
	}

	return crossings
}

// onBoth reports whether a point lies within the sweep of whichever of two edges
// are arcs, which is what turns a meeting of two circles into a meeting of two
// arcs.
func onBoth(at Point, first, second *bend) bool {
	if first != nil && !first.holds(at) {
		return false
	}
	return second == nil || second.holds(at)
}

// near reports whether a point is no further than the tolerance from any of
// them.
func near(at Point, points []Point, tolerance float64) bool {
	for _, point := range points {
		if pointLength(pointSub(at, point)) <= tolerance {
			return true
		}
	}
	return false
}

// circlesMeeting is where the circles two coplanar arcs run on meet, which is at
// most two points.
//
// Two circles about one centre are one circle or are nowhere near each other,
// and neither is a crossing: arcs of one circle which overlap retrace the ring
// rather than cross it, which shows up as the vector area they cancel.
func circlesMeeting(first, second *bend, normal Point) []Point {
	between := pointSub(second.centre, first.centre)

	apart := pointLength(between)
	if apart == 0 || apart > first.radius+second.radius || apart < math.Abs(first.radius-second.radius) {
		return nil
	}

	along := (first.radius*first.radius - second.radius*second.radius + apart*apart) / (2 * apart)

	square := first.radius*first.radius - along*along
	if square <= 0 {
		return nil
	}

	across := math.Sqrt(square)
	foot := pointAdd(first.centre, pointScale(between, along/apart))
	sideways := pointScale(pointCross(normal, between), across/apart)

	return []Point{pointAdd(foot, sideways), pointSub(foot, sideways)}
}

// circleMeetingSegment is where the circle an arc runs on meets a straight edge,
// which is at most two points.
//
// The ends of the segment are inside it rather than outside. A curve which
// reaches another edge exactly at its corner is where the arithmetic lands on
// the boundary and a rounding decides which side of it, and a crossing missed
// because of one is a ring called sound which is not.
func circleMeetingSegment(curve *bend, from, to Point, tolerance float64) []Point {
	along := pointSub(to, from)
	offset := pointSub(from, curve.centre)

	square := pointDot(along, along)
	if square == 0 {
		return nil
	}

	margin := tolerance / math.Sqrt(square)

	linear := 2 * pointDot(offset, along)
	constant := pointDot(offset, offset) - curve.radius*curve.radius

	discriminant := linear*linear - 4*square*constant
	if discriminant <= 0 {
		return nil
	}

	root := math.Sqrt(discriminant)

	var meetings []Point
	for _, at := range [2]float64{(-linear - root) / (2 * square), (-linear + root) / (2 * square)} {
		if at < -margin || at > 1+margin {
			continue
		}
		meetings = append(meetings, pointAdd(from, pointScale(along, at)))
	}

	return meetings
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

	depths, nested := m.depths(region, rings)
	if !nested {
		return
	}

	var area float64
	var weighted Point

	for i, one := range rings {
		if !one.hasArea {
			return
		}

		sign := 1.0
		if depths[i]%2 == 1 {
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

// depths counts, for each ring of a region, how many of the others hold it,
// which is what the even-odd rule takes a ring's area away by.
//
// Where nothing bends the count is taken at the corners, which is
// [measurer.nesting] and is the count this package has always nested by. A
// region of straight rings is measured identically whether or not the survey
// names a chord, down to the last figure: nothing is drawn for it and nothing
// reads the name.
//
// Where something bends the count cannot be taken at the corners at all. Nesting
// decides whether a ring is a hole by casting a ray from one ring at another,
// and that ray is cast at the chords: a courtyard whose wall bows out past a
// corner of the plate around it is inside the plate and outside its chords, so
// the answer would flip on which side of a bulge a corner happened to fall — an
// area wrong by a whole ring rather than by a sag.
//
// So the rings are drawn and the count is taken at the segments, to the chord
// tolerance the survey names. A survey which names none is refused rather than
// drawn at a resolution nobody chose, which is the same refusal this made before
// there was a name to give — and the refusal now says which flag answers it.
//
// The drawing decides the nesting and nothing else. Every area summed afterwards
// is the area of the arc, computed from the parameterisation and exact whatever
// the chord tolerance was; what the chord bounds is which rings were taken away
// from which, and [Measurement.Deviation] reports what it came to.
func (m *measurer) depths(region *SemanticNode, rings []*outline) ([]int, bool) {
	var bent bool
	for _, one := range rings {
		if one.curved {
			bent = true
			break
		}
	}

	if !bent {
		depths := make([]int, len(rings))
		for i := range rings {
			depths[i] = m.nesting(rings, i)
		}
		return depths, true
	}

	if m.survey.Chord == "" {
		m.add(m.unnestable(region, rings))
		return nil, false
	}

	tolerance, ok := m.chord(m.survey.Chord, m.span)
	if !ok {
		return nil, false
	}

	basis, ok := planeOf(rings[0].normal, rings[0].points[0])
	if !ok {
		return nil, false
	}

	figure := make([]contour, 0, len(rings))
	for _, one := range rings {
		points, _, deviation, ok := m.drawnRing(one, tolerance)
		if !ok {
			return nil, false
		}

		projected := make(contour, 0, len(points))
		for _, point := range points {
			projected = append(projected, basis.project(point))
		}

		figure = append(figure, projected)
		m.result.deviation = math.Max(m.result.deviation, deviation)
	}

	m.result.chord = tolerance

	depths := make([]int, len(figure))
	for i := range figure {
		for j, other := range figure {
			if j != i && len(figure[i]) > 0 && other.holds(figure[i][0]) {
				depths[i]++
			}
		}
	}

	return depths, true
}

// unnestable reports a region of several rings, one of which bends, which no
// chord tolerance was named to nest at the segments.
func (m *measurer) unnestable(region *SemanticNode, rings []*outline) Diagnostic {
	var bends *outline
	for _, one := range rings {
		if one.curved {
			bends = one
			break
		}
	}

	return Diagnostic{
		Severity: SeverityError,
		Span:     m.span,
		Message: fmt.Sprintf(
			"expected either every loop bounding %s to be straight or a chord tolerance to nest them at, found that %s "+
				"bends along an arc and no chord tolerance named",
			nodeName(region), geometricName(loopTag, bends.loop.id),
		),
		Hint: "a ring inside another is a hole and is taken away, and which ring is inside which is decided at the " +
			"corners; a bulge which reaches past one is a whole ring counted the wrong way rather than a sag; name " +
			"the tolerance the curve may be drawn to so the nesting is decided at the segments instead",
	}
}

// along records a measurement made from the open runs a node drawn as a line is
// assembled from: how far they reach in total, and the box which holds them.
//
// It is [measurer.combine] for a shape which encloses nothing, and it is a
// separate pass rather than a flag through that one because there is nothing of
// that one to run. Nesting, coplanarity and the even-odd rule are all questions
// about which ring is inside which, and a chain is inside nothing; a run whose
// two ends happen to meet is still a run, and calling it a region because it
// closed would give a door an area nobody drew.
func (m *measurer) along(rings []*outline) {
	m.lengthOf(rings)
	m.boundsOf(rings)
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
		bounds = widened(bounds, one.bounds)
	}

	m.result.bounds, m.result.hasBounds = bounds, true
}

// widened is the box which holds both, keeping the unit of the first.
func widened(box, other Box) Box {
	for axis := range 3 {
		box.Min[axis] = math.Min(box.Min[axis], other.Min[axis])
		box.Max[axis] = math.Max(box.Max[axis], other.Max[axis])
	}
	return box
}

// coplanar reports whether every ring of a region lies in one and the same
// plane, naming the first which does not and how far out of it that puts it.
//
// Nesting is only meaningful between rings which share a plane. A courtyard is a
// hole in a floor plate because it is drawn in the plate; a ring on the storey
// above is a different shape which merely happens to be inside this one seen
// from above, and subtracting it would report a building with a floor missing.
//
// Sharing an orientation is not sharing a plane, and this is the case where the
// difference bites: every floor plate of every storey has a normal along the
// same axis, so a test which compared only that would call two storeys coplanar
// and quietly nest one in the other. So the orientation is checked because the
// projection nesting is computed in needs it, and then every corner of every
// other ring is measured against the first ring's plane — one distance, in the
// frame's own unit, which catches a ring tilted away and a ring three metres
// above alike.
//
// The plane itself is judged only where the declared tolerance can be applied,
// for the reason [measurer.planar] is: there is no default and no fallback
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
func (m *measurer) coplanar(region *SemanticNode, rings []*outline) bool {
	for _, one := range rings[1:] {
		if one.axis != rings[0].axis {
			m.add(m.twoPlanes(region, rings[0], one, "which faces another way", ""))
			return false
		}
	}

	if !m.applicable() || rings[0].magnitude == 0 {
		return true
	}

	unit := pointScale(rings[0].normal, 1/rings[0].magnitude)
	anchor := rings[0].points[0]

	for _, one := range rings[1:] {
		worst := 0.0
		for _, point := range one.points {
			// The difference is taken before the dot product, so a plate on a
			// projected grid is measured through a three and not through the
			// last digits of two seven-figure numbers.
			if out := math.Abs(pointDot(pointSub(point, anchor), unit)); out > worst {
				worst = out
			}
		}

		if worst <= m.tolerance.Value {
			continue
		}

		m.add(m.twoPlanes(region, rings[0], one,
			fmt.Sprintf("which is %s %s out of its plane", decimal(worst), m.unit),
			fmt.Sprintf(", within the tolerance %s, which is %s %s",
				m.tolerance.Name, decimal(m.tolerance.Value), m.tolerance.Unit)))
		return false
	}

	return true
}

// twoPlanes reports one ring of a region which is not in the plane of the first.
func (m *measurer) twoPlanes(region *SemanticNode, first, other *outline, found, judged string) Diagnostic {
	var related []RelatedLocation
	if judged != "" {
		related = append(related, RelatedLocation{Span: m.tolerance.Span, Message: "the tolerance is declared here"})
	}

	return Diagnostic{
		Severity: SeverityError,
		Span:     m.span,
		Message: fmt.Sprintf(
			"expected every loop bounding %s to lie in the plane of %s%s, found %s, %s",
			nodeName(region), geometricName(loopTag, first.loop.id), judged,
			geometricName(loopTag, other.loop.id), found,
		),
		Hint: "a region bounded by more than one ring is measured by nesting, and a ring inside another is only a hole " +
			"in it where the two share a plane; two rings which face the same way on different storeys are two regions",
		Related: related,
	}
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

// walked is the corners the traversal passes through, in the order it reaches
// them: the corner each step leaves, and — for an open run, which does not come
// back to where it began — the corner the last step arrives at.
//
// It is what a ring's corners and a run's corners have in common, and it is a
// method rather than a second slice on the outline so that the pairing of a
// corner with the edge leaving it stays exact. corners, edges, reversed and
// bends are one per step and index each other; this is one longer for a run and
// indexes nothing but points.
func (r *outline) walked() []ID {
	if !r.open || r.last == "" {
		return r.corners
	}

	return append(slices.Clone(r.corners), r.last)
}

// measure computes everything a ring's corners and its arcs decide.
//
// The arcs are accounted for exactly and are never drawn. A ring which bends is
// its polygon of chords plus, for every arc, the circular segment between that
// chord and the curve over it — a length, a vector area and a centroid each
// computed from the parameterisation. So the sag of a curve is in the answer at
// full precision rather than at whatever resolution somebody would have
// tessellated it to.
//
// An open run stops after the length and the bounds. There is one segment per
// edge and never one back from the last corner to the first, so the length is
// how far the run reaches rather than a perimeter with an imaginary side in it;
// and a chain has no vector area, so there is no plane, no area and no centroid
// to compute from one. Each of those is absent rather than zero, which is what
// [Measurement.Area] reporting false says.
func (r *outline) measure() {
	r.bounds = boxOf(r.points)
	for _, curve := range r.bends {
		if curve != nil {
			r.bounds = widened(r.bounds, curve.bounds())
		}
	}

	r.origin = r.bounds.Min

	r.shifted = make([]Point, 0, len(r.points))
	for _, point := range r.points {
		r.shifted = append(r.shifted, pointSub(point, r.origin))
	}

	// segments is twice the vector area of every circular segment the ring's
	// arcs cut off from their chords, which is what the Newell sum over the
	// corners is short by. Each points along its own arc's normal, so a bulge
	// running the way the ring does adds and one running against it takes away.
	var segments Point

	// One segment per edge. A ring has as many corners as edges and the last
	// segment wraps back onto the first corner; a run has one corner more and
	// the last segment arrives at it, so the same index runs both without a
	// closing side being invented for the chain.
	count := len(r.shifted)
	for i := range r.bends {
		point, next := r.shifted[i], r.shifted[(i+1)%count]

		if curve := r.bends[i]; curve != nil {
			r.perimeter += curve.length()
			segments = pointAdd(segments, curve.area())
		} else {
			r.perimeter += pointLength(pointSub(next, point))
		}

		r.normal = pointAdd(r.normal, pointCross(point, next))
	}

	if r.open {
		// A chain encloses nothing, so there is no plane to read a projection
		// from and no area to take a centroid of. The axis is still set, from
		// the box rather than from a normal, so that anything which asks the run
		// which axis to drop gets the one which cannot collapse it.
		r.normal, r.magnitude = Point{}, 0
		r.axis = flattest(r.bounds)
		return
	}

	r.normal = pointAdd(r.normal, segments)
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

	// Each circular segment is a piece of the shape in its own right, with its
	// own area and its own centroid, and the two are combined the way the rings
	// of a region are: the areas are signed along the ring's normal, so a bulge
	// which runs against the ring pulls the centroid the other way without
	// anything having to say that it is a bite rather than a bulge.
	var bulges Point
	for _, curve := range r.bends {
		if curve == nil {
			continue
		}

		centre, ok := curve.segment()
		if !ok {
			continue
		}

		signed := pointDot(curve.area(), r.normal) / (2 * r.magnitude)
		bulges = pointAdd(bulges, pointScale(pointSub(centre, r.origin), signed))
	}

	r.centroid = pointAdd(r.origin, pointAdd(
		pointScale(weighted, 1/(3*r.magnitude*r.magnitude)),
		pointScale(bulges, 2/r.magnitude),
	))
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

// vertex measures one corner, reporting a corner nothing says the position of.
func (m *measurer) vertex(vertex *Vertex) {
	components, ok := m.at(vertex.id)
	if !ok {
		m.add(Diagnostic{
			Severity: SeverityError,
			Span:     m.topology.namedAt(vertex.id, vertex.span),
			Message: fmt.Sprintf(
				"expected a position for the vertex %s, found none to read",
				geometricName(vertexTag, vertex.id),
			),
			Hint: m.positionHint(),
		})
		return
	}

	m.result.dimension = len(components)
	at := asPoint(components)

	m.contribute([]ID{vertex.id})

	m.result.centroid, m.result.hasCentroid = at, true
	m.result.bounds, m.result.hasBounds = boxOf([]Point{at}), true
	m.result.bounds.Unit = m.unit
}

// edge measures one edge, reporting an edge whose ends are at one point.
//
// An edge which bends is measured from its arc and never from its chord: its
// length is the length of the curve, its centroid is where the length of the
// curve is centred, and its box is the box the curve reaches rather than the one
// its two ends do.
func (m *measurer) edge(edge *Edge) {
	start, end, dimension, ok := m.ends(edge)
	if !ok {
		return
	}

	curve, bends, ok := m.bend(edge, start, end, false)
	if !ok {
		return
	}

	m.contribute([]ID{edge.start, edge.end, edge.id})

	if bends {
		m.result.length, m.result.hasLength = curve.length(), true
		m.result.centroid, m.result.hasCentroid = curve.centroid(), true
		m.result.bounds, m.result.hasBounds = curve.bounds(), true
		m.result.bounds.Unit = m.unit
		return
	}

	m.result.length, m.result.hasLength = pointLength(pointSub(end, start)), true
	m.result.centroid, m.result.hasCentroid = pointScale(pointAdd(start, end), 0.5), true
	m.result.bounds, m.result.hasBounds = boxOf([]Point{start, end}), true
	m.result.bounds.Unit = m.unit

	if m.result.length != 0 {
		return
	}

	m.add(Diagnostic{
		Severity: SeverityError,
		Span:     m.edgeAt(edge),
		Message: fmt.Sprintf(
			"expected the edge %s to have an extent, found %s and %s at the same point, %s%s",
			geometricName(edgeTag, edge.id), edge.start, edge.end, pointText(start, dimension), unitSuffix(m.unit),
		),
		Hint: "an edge with no extent gives a loop through it no direction; two corners surveyed to one coordinate are " +
			"one corner",
	})
}

// ends reads where an edge's two ends are, reporting an end with no position to
// read and a pair written with different numbers of components.
func (m *measurer) ends(edge *Edge) (Point, Point, int, bool) {
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
		return Point{}, Point{}, 0, false
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
		return Point{}, Point{}, 0, false
	}

	m.result.dimension = len(from)

	return asPoint(from), asPoint(to), len(from), true
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
