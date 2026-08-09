// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"math"
	"slices"
)

// Positions is where the vertices of a model are: the resolved value of each
// vertex's position, keyed by the vertex's id.
//
// It is supplied rather than read, because which predicate carries a position is
// vocabulary the consuming repository owns and not something the engine knows
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)). A
// repository which spells it `position` resolves that predicate for every vertex
// and hands the result here; one which spells it something else spells it
// something else, and nothing in this file changes.
//
// A vertex which is absent is a vertex with no position anything could resolve —
// none was claimed, or the claims tie and the tie is unbroken. That is a state
// and not a failure: [Topology.Assemble] measures what it can and says so where
// it cannot, rather than inventing a coordinate to measure against.
type Positions map[ID]Value

// Step is one edge of an assembled loop, in the order the loop is traversed and
// in the direction the traversal runs through it.
//
// The direction is the step's and never the edge's. An edge is written once,
// with its two vertices ordered, and two loops either side of it traverse it in
// opposite directions — which is the whole point of the edge being one node
// rather than two. So a step says which vertex this traversal entered by and
// which it left by, and [Step.Reversed] reports when that is the opposite of the
// way the edge was written.
//
// The zero value is a step through no edge, which no assembly yields.
type Step struct {
	// edge is the edge traversed.
	edge *Edge

	// from and to are the vertices the traversal entered and left by.
	from, to ID

	// reversed reports whether that is the opposite of the order the edge was
	// written in.
	reversed bool
}

// Edge returns the edge the step runs through.
func (s Step) Edge() *Edge { return s.edge }

// From returns the vertex the traversal entered the edge by.
func (s Step) From() ID { return s.from }

// To returns the vertex the traversal left the edge by.
func (s Step) To() ID { return s.to }

// Reversed reports whether the traversal runs through the edge against the order
// its two vertices were written in.
//
// It is a property of this traversal and not of the edge. The edge is shared, and
// the room on the other side of it traverses it the other way.
func (s Step) Reversed() bool { return s.reversed }

// Assembly is one loop assembled into the ring its edges traverse, together with
// whether that ring closes and the named tolerance it was judged against.
//
// Closure is computed and never stored. Nothing in the format says whether a
// loop closes, adding an edge changes the answer with no other edit, and a
// recorded answer would be stale the moment a vertex moved
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// The tolerance travels with the result because the result depends on it. "This
// loop closes" is not a fact about the loop alone: it is a fact about the loop
// judged against a stated tolerance, and an answer which did not say which one
// could not be checked, reproduced or argued with
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
//
// The zero value is an assembly of no loop, which is what assembling a nil loop
// yields; every method below works on it.
type Assembly struct {
	// loop is the loop which was assembled.
	loop *Loop

	// steps are its edges in traversal order. A loop which did not assemble
	// cleanly still has the steps the traversal did take, because a caller
	// reporting on it wants to say how far it got.
	steps []Step

	// closed reports whether the ring closes.
	closed bool

	// open reports whether the edges were read as an open run rather than as a
	// ring, which is what [Topology.AssembleRun] does and [Topology.Assemble]
	// does not.
	open bool

	// tolerance is the declared tolerance closure was judged against. The zero
	// value belongs to an assembly whose tolerance name no registry file
	// declares, which is a diagnostic of its own.
	tolerance Tolerance
}

// Loop returns the loop which was assembled.
func (a Assembly) Loop() *Loop { return a.loop }

// Steps returns the loop's edges in the order the traversal ran through them,
// each with the direction it ran.
//
// A loop which did not close still has steps: they are how far the traversal
// got, which is what a caller reporting on the failure wants to show.
func (a Assembly) Steps() []Step { return slices.Clone(a.steps) }

// Closed reports whether the edges form a connected, simple cycle: each begins
// where the last ended, the last ends where the first began, and no corner joins
// more than two of them.
//
// Two corners which are different vertices count as one point when they are no
// further apart than the tolerance the assembly was judged against, which is what
// [Assembly.Tolerance] reports.
func (a Assembly) Closed() bool { return a.closed }

// Open reports whether the edges were read as an open run rather than as a
// ring.
//
// It is a fact about the reading and not about the edges. A run whose two ends
// happen to meet is both open and closed — it was read as a chain, and the chain
// came back to where it began — and a caller drawing it has to know which of the
// two questions it is holding the answer to.
func (a Assembly) Open() bool { return a.open }

// Tolerance returns the declared tolerance closure was judged against.
//
// Its zero value belongs to an assembly whose tolerance was not declared, which
// is reported as a diagnostic and leaves closure decided by vertex identity
// alone. There is no default and no fallback
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
func (a Assembly) Tolerance() Tolerance { return a.tolerance }

// Assemble reads a loop as the ring its edges traverse and reports whether that
// ring closes, judged against the named tolerance.
//
// The order of the loop's edges is the order it is traversed, and the edges
// themselves are directed, so assembling one means deciding which way round each
// edge is entered: the ring is what says that, and an edge shared by two regions
// is traversed one way by one of them and the other way by the other.
//
// tolerance is a name from the tolerance registry and never a number. A name
// nothing declares is a diagnostic, and closure is then decided by vertex
// identity alone; there is no default and no fallback
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)). Which tolerance
// produced the answer comes back on the assembly, because an answer computed
// against a tolerance which does not say which one cannot be reproduced.
//
// positions is where the vertices are, which the caller resolves under whichever
// predicate its registry declares for a position. It is what a gap is measured
// with. A vertex with no resolved position is not a failure: the gap is reported
// as one whose size could not be measured, which is what is true.
//
// Everything is measured in the unit of the loop's frame, and nothing is
// converted. A frame has exactly one linear unit
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)), so a position
// written in another unit, or a tolerance declared in one, is reported rather
// than silently scaled.
//
// The three ways a loop fails are three diagnostics, because they are three
// different mistakes with three different fixes:
//
//   - A gap. Two edges which should meet do not, and nothing brings them within
//     the tolerance. The diagnostic names both corners and how far apart they
//     are, so the author knows whether this is a typo or a survey which moved.
//   - An order. The edges do form one ring, but not in the order they are
//     written. Nothing is missing and nothing has to move; the list has to be
//     rewritten in the order the ring is walked.
//   - A shape which is not a simple cycle. An edge written twice, a corner where
//     three edges meet, or edges which form more than one ring. No reordering
//     fixes any of them, which is why calling them a gap would send the author
//     looking in the wrong place.
//
// Diagnostics come back in the order the pass found them. Collecting them into a
// [Diagnostics] is what puts them in reporting order.
func (t *Topology) Assemble(loop *Loop, positions Positions, tolerance string, registry *Registry) (Assembly, []Diagnostic) {
	return t.assemble(loop, positions, tolerance, registry, false)
}

// AssembleRun reads a loop as the open run of edges its traversal walks, and is
// [Topology.Assemble] with the one requirement a run does not have: that the
// walk returns to the corner it began at.
//
// A door, a window, a railing, a wall run and a duct are each an open chain of
// edges rather than a region, which is what the geometry form `line` declares.
// The edges are written the way a loop's are — ordered, in one frame, resolving
// to edges this model holds — because a run is the same authoring and not a
// second one: a chain and a ring differ in whether the last edge ends where the
// first began, and in nothing else.
//
// So every refusal [Topology.Assemble] makes is made here too. An edge written
// twice doubles back, a corner where three edges meet is a branch, and edges in
// two disconnected pieces are two runs; none of the three is fixed by closing
// anything, and each is refused with the same distinction drawn between it and
// the others. What is not refused is the loose end: a chain has exactly two of
// them, and having them is what a run *is* rather than a mistake in one.
//
// Order is still the order the run is walked, and edges written in some other
// order are reported as being in the wrong order rather than as a gap — the same
// diagnostic a ring gets, for the same reason. A run whose ends happen to meet
// is a ring somebody drew as a chain: it comes back closed as well as open, and
// nothing is refused for it.
//
// [Assembly.Open] reports which of the two readings produced an assembly, and
// [Assembly.Closed] whether the walk came back to where it started.
func (t *Topology) AssembleRun(loop *Loop, positions Positions, tolerance string, registry *Registry) (Assembly, []Diagnostic) {
	return t.assemble(loop, positions, tolerance, registry, true)
}

// assemble is the walk both readings share, with open saying whether the ends
// are required to meet.
//
// It is one pass rather than two because a run and a ring are one authoring:
// every refusal but closure is the same refusal, and a second copy of it would
// be a second answer to "is this a branch" the day either learned something the
// other did not.
func (t *Topology) assemble(
	loop *Loop,
	positions Positions,
	tolerance string,
	registry *Registry,
	open bool,
) (Assembly, []Diagnostic) {
	if loop == nil {
		return Assembly{}, nil
	}

	a := &assembler{
		topology:  t,
		positions: positions,
		assembly:  Assembly{loop: loop, open: open},
		open:      open,
		unit:      frameUnit(registry, loop.frame),
	}

	a.assembly.tolerance, a.declared = registry.Tolerance(tolerance)
	if !a.declared {
		a.add(registry.Undeclared(SortTolerance, tolerance, t.namedAt(loop.id, loop.span)))
	}
	a.mismatched()

	// Each step below is reached only where the one before it left something
	// worth judging. A loop with an edge nobody wrote has no ring to walk, and a
	// loop which traverses one edge twice is not a ring at all — reporting where
	// its corners fail to meet on top of that is one mistake told twice, in the
	// vocabulary of a different one.
	if !a.resolve() {
		return a.assembly, a.diags
	}

	if !a.simple() {
		return a.assembly, a.diags
	}

	a.merge()

	if !a.ring() {
		return a.assembly, a.diags
	}

	a.traverse()

	return a.assembly, a.diags
}

// assembler assembles one loop.
type assembler struct {
	reader

	// topology is what the loop's edge ids are resolved against.
	topology *Topology

	// positions is where the vertices are.
	positions Positions

	// assembly is what is being built.
	assembly Assembly

	// declared reports whether the tolerance name was one the registry
	// declares.
	declared bool

	// open reports whether the edges are being read as an open run, in which
	// the two loose ends are what the shape is rather than a gap in it.
	open bool

	// unit is the unit of the loop's frame, which everything is measured in.
	// Empty where the frame is not one the registry declares, which leaves every
	// gap unmeasurable.
	unit Unit

	// edges are the loop's edges, resolved, in the order it wrote them.
	edges []*Edge

	// points is the corners of the loop, with vertices no further apart than the
	// tolerance merged into one. It is what "the same corner" means here.
	points *merged

	// coincident are the pairs of distinct vertices the tolerance merged, in the
	// order they were found, so that connectivity can be rebuilt over the same
	// corners.
	coincident [][2]ID
}

// loopName names the loop for a diagnostic.
func (a *assembler) loopName() string {
	return geometricName(loopTag, a.assembly.loop.id)
}

// shape is the word a diagnostic calls the edges by: a loop read as a ring is a
// loop, and one read as an open chain is a run.
//
// It is one word rather than a message per reading because the mistakes are the
// same mistakes. An edge written twice is an edge written twice whichever way
// the edges were going to be walked, and telling somebody about it in a
// vocabulary their file does not use is how a diagnostic sends them looking for
// a ring they never wrote.
func (a *assembler) shape() string {
	if a.open {
		return "run"
	}
	return "loop"
}

// mismatched reports a tolerance which cannot be applied to this loop because it
// is declared in a different unit from the loop's frame.
//
// Nothing here converts between units
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)). A tolerance in
// millimetres applied to a frame in metres by scaling one of them silently is
// exactly the mistake that decision exists to prevent, and one applied without
// scaling would be a thousand times the tolerance somebody wrote down.
func (a *assembler) mismatched() {
	if !a.declared || a.unit == "" || a.assembly.tolerance.Unit == a.unit {
		return
	}

	a.add(Diagnostic{
		Severity: SeverityError,
		Span:     a.topology.namedAt(a.assembly.loop.id, a.assembly.loop.span),
		Message: fmt.Sprintf(
			"expected the tolerance %s in %s, which is the unit of the frame %s, found it in %s",
			a.assembly.tolerance.Name, a.unit, a.assembly.loop.frame, a.assembly.tolerance.Unit,
		),
		Hint: "nothing converts between units; a tolerance judging a loop in one frame is declared in that frame's unit",
		Related: []RelatedLocation{
			{Span: a.assembly.tolerance.Span, Message: "the tolerance is declared here"},
		},
	})
}

// resolve reads the loop's edge ids as edges, reporting whether every one of
// them reached an edge with two corners to walk between.
//
// An id naming no edge, one naming something which is not an edge, and an
// endpoint which was not an id are each already a diagnostic from the pass which
// read the family. There is no ring to assemble without them and nothing to add
// by saying so a second time.
func (a *assembler) resolve() bool {
	loop := a.assembly.loop
	if len(loop.edges) == 0 {
		return false
	}

	a.edges = make([]*Edge, 0, len(loop.edges))
	for _, id := range loop.edges {
		edge, ok := a.topology.Edge(id)
		if !ok || edge.start == "" || edge.end == "" {
			return false
		}
		a.edges = append(a.edges, edge)
	}

	return true
}

// simple reports whether the loop traverses each of its edges once, reporting
// the first edge it traverses twice.
//
// It is checked before anything about corners, because a ring which doubles back
// along an edge has corners which meet three ways as a consequence, and the
// consequence is not the mistake.
func (a *assembler) simple() bool {
	first := make(map[ID]int, len(a.edges))

	for i, edge := range a.edges {
		at, seen := first[edge.id]
		if !seen {
			first[edge.id] = i
			continue
		}

		hint := "a loop is a simple cycle: it runs through each of its edges once, so an edge written twice is a ring " +
			"which doubles back along itself"
		if a.open {
			hint = "a run is a simple chain: it runs through each of its edges once, so an edge written twice is a " +
				"chain which doubles back along itself"
		}

		a.add(Diagnostic{
			Severity: SeverityError,
			Span:     a.assembly.loop.at[i],
			Message: fmt.Sprintf(
				"expected an edge the %s %s does not already traverse, found %s a second time",
				a.shape(), a.loopName(), edge.id,
			),
			Hint:    hint,
			Related: []RelatedLocation{{Span: a.assembly.loop.at[at], Message: "first traversed here"}},
		})
		return false
	}

	return true
}

// merge decides which of the loop's vertices are the same corner.
//
// Two vertices are the same corner when they are the same vertex, and also when
// they are no further apart than the declared tolerance. The second is what the
// tolerance is for: a corner surveyed twice by two crews is two vertices four
// millimetres apart, and a model which called that an open loop would report a
// gap on every honest building in it.
//
// Nothing is merged where the tolerance was not declared, was declared in
// another unit, or where either position is unresolved. In each of those the
// answer is unknown rather than false, and merging on an unknown would close a
// loop nobody measured.
func (a *assembler) merge() {
	ids := make([]ID, 0, 2*len(a.edges))
	for _, edge := range a.edges {
		ids = append(ids, edge.start, edge.end)
	}

	a.points = newMerged(ids)

	if !a.applicable() {
		return
	}

	distinct := make([]ID, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(distinct, id) {
			distinct = append(distinct, id)
		}
	}

	for i, first := range distinct {
		for _, second := range distinct[i+1:] {
			gap, ok := a.distance(first, second)
			if !ok || gap > a.assembly.tolerance.Value {
				continue
			}

			a.coincident = append(a.coincident, [2]ID{first, second})
			a.points.union(first, second)
		}
	}
}

// ring reports whether the loop's edges form exactly one ring, reporting the
// shapes which are not one.
//
// A ring is a connected graph in which every corner meets exactly two edges.
// Both halves are checked, and each is its own diagnostic: a corner where three
// edges meet is a branch, and edges which are all in pairs but in two separate
// rings are two rings. Neither is fixed by reordering the list, which is what
// keeps them apart from the order diagnostic.
//
// A corner met by one edge is not reported here. That is a chain with two loose
// ends, which is a gap, and [assembler.traverse] reports it as one with the
// distance across it — which is the number the author needs and this cannot say.
// Read as a run those loose ends are the shape rather than a gap, and nothing
// reports them at all.
func (a *assembler) ring() bool {
	// The corners are collected in the order the edges reach them so that what
	// is reported is a property of the loop rather than of a map's iteration.
	var corners []ID
	degree := make(map[ID]int, 2*len(a.edges))

	for _, edge := range a.edges {
		for _, id := range []ID{edge.start, edge.end} {
			corner := a.points.find(id)
			if degree[corner] == 0 {
				corners = append(corners, corner)
			}
			degree[corner]++
		}
	}

	for _, corner := range corners {
		if degree[corner] <= 2 {
			continue
		}

		a.add(a.branching(corner, degree[corner]))
		return false
	}

	if rings := a.rings(corners); rings > 1 {
		found, hint := "ring", "a loop is one simple cycle; a region bounded by more than one ring references one "+
			"loop per ring, written (boundary <loop-id>) once for each"
		if a.open {
			found, hint = "chain", "a run is one connected chain; a node drawn as a line whose shape is in more than "+
				"one piece references one loop per piece, written (boundary <loop-id>) once for each"
		}

		a.add(Diagnostic{
			Severity: SeverityError,
			Span:     a.topology.namedAt(a.assembly.loop.id, a.assembly.loop.span),
			Message: fmt.Sprintf(
				"expected the edges of the %s %s to form one %s, found %d separate ones",
				a.shape(), a.loopName(), found, rings,
			),
			Hint: hint,
		})
		return false
	}

	return true
}

// branching reports one corner where more than two of the loop's edges meet.
func (a *assembler) branching(corner ID, degree int) Diagnostic {
	var related []RelatedLocation
	for i, edge := range a.edges {
		if a.points.find(edge.start) != corner && a.points.find(edge.end) != corner {
			continue
		}
		related = append(related, RelatedLocation{
			Span:    a.assembly.loop.at[i],
			Message: fmt.Sprintf("%s meets it here", edge.id),
		})
	}

	want, hint := "two", "a loop is a simple cycle: each corner joins the edge the traversal arrives by and the one "+
		"it leaves by, so a corner joining a third is a branch and not a ring"
	if a.open {
		want, hint = "at most two", "a run is a simple chain: each corner but its two ends joins the edge the "+
			"traversal arrives by and the one it leaves by, so a corner joining a third is a branch and not a chain"
	}

	return Diagnostic{
		Severity: SeverityError,
		Span:     a.topology.namedAt(a.assembly.loop.id, a.assembly.loop.span),
		Message: fmt.Sprintf(
			"expected every corner of the %s %s to meet %s of its edges, found %s, where %d meet",
			a.shape(), a.loopName(), want, a.cornerName(corner), degree,
		),
		Hint:    hint,
		Related: related,
	}
}

// rings counts the connected components the loop's edges form over its corners.
func (a *assembler) rings(corners []ID) int {
	connected := newMerged(corners)
	for _, pair := range a.coincident {
		connected.union(a.points.find(pair[0]), a.points.find(pair[1]))
	}
	for _, edge := range a.edges {
		connected.union(a.points.find(edge.start), a.points.find(edge.end))
	}

	var roots []ID
	for _, corner := range corners {
		root := connected.find(corner)
		if !slices.Contains(roots, root) {
			roots = append(roots, root)
		}
	}

	return len(roots)
}

// breakage is one seam of the ring which did not join: where the traversal had
// reached, the corner the next edge begins at, and which edge of the loop that
// is.
type breakage struct {
	// index is the position in the loop's edges of the edge which did not join,
	// which is what a diagnostic about the seam underlines. The closing seam is
	// index 0, because what did not join there is the first edge.
	index int

	// from is the corner the traversal had reached, and to the corner the next
	// edge begins at.
	from, to ID
}

// traverse walks the loop's edges in the order they were written, reporting
// where the walk does not join.
//
// The walk decides the direction of each edge as it goes: an edge is entered by
// whichever of its two vertices the traversal has reached, and left by the other.
// The first edge's direction is decided by the second, because a ring has to
// start somewhere and the way round it runs is what the rest of it says.
//
// A break does not end the walk. The traversal restarts at the next edge in the
// order it was written, so a loop with two gaps is reported as a loop with two
// gaps rather than as one which has to be fixed twice to find out.
func (a *assembler) traverse() {
	first := a.edges[0]

	from, to := first.start, first.end
	if len(a.edges) > 1 && !a.joins(to, a.edges[1]) && a.joins(from, a.edges[1]) {
		from, to = to, from
	}

	steps := make([]Step, 0, len(a.edges))
	steps = append(steps, Step{edge: first, from: from, to: to, reversed: from != first.start})

	var breaks []breakage

	for i, edge := range a.edges[1:] {
		switch {
		case a.same(to, edge.start):
			from, to = edge.start, edge.end
		case a.same(to, edge.end):
			from, to = edge.end, edge.start
		default:
			near, far := a.nearer(to, edge)
			breaks = append(breaks, breakage{index: i + 1, from: to, to: near})
			from, to = near, far
		}

		steps = append(steps, Step{edge: edge, from: from, to: to, reversed: from != edge.start})
	}

	// The closing seam is a break for a ring and the far end of the chain for a
	// run. Whether the walk came back to where it started is recorded either
	// way, because a run whose ends happen to meet is a fact about the run and
	// not about the reading of it.
	joined := a.same(to, steps[0].from)
	if !joined && !a.open {
		breaks = append(breaks, breakage{index: 0, from: to, to: steps[0].from})
	}

	a.assembly.steps = steps
	a.assembly.closed = len(breaks) == 0 && joined

	if len(breaks) == 0 {
		return
	}

	// Reaching here with a ring means the edges do form one, because
	// [assembler.ring] has already rejected every shape which is not one. So the
	// walk failed on the order alone, which is one fact about the loop and is
	// reported once rather than once per seam.
	//
	// A run reaches here for the same reason and always: [assembler.ring] has
	// left it one connected piece with no corner meeting three edges, which is a
	// chain or a cycle, and either of those walked in the order it is written
	// joins at every seam. So a seam which did not join is the order, and
	// reporting a gap across it would send the author to move a corner which is
	// where they put it.
	if a.open || a.isRing() {
		a.add(a.disordered(breaks[0]))
		return
	}

	for _, seam := range breaks {
		a.add(a.gap(seam))
	}
}

// isRing reports whether the loop's corners each meet exactly two of its edges,
// which — everything else [assembler.ring] rejects having been rejected — is
// what makes the edges one ring written in the wrong order rather than a chain
// with a gap in it.
func (a *assembler) isRing() bool {
	degree := make(map[ID]int, 2*len(a.edges))
	for _, edge := range a.edges {
		degree[a.points.find(edge.start)]++
		degree[a.points.find(edge.end)]++
	}

	for _, count := range degree {
		if count != 2 {
			return false
		}
	}

	return true
}

// disordered reports edges which form one ring, or one chain, but are not
// written in the order it is walked.
func (a *assembler) disordered(seam breakage) Diagnostic {
	arrived := a.edges[len(a.edges)-1]
	if seam.index > 0 {
		arrived = a.edges[seam.index-1]
	}

	hint := "these edges do form one ring, but not in this order; the order of a loop's edges is the order the loop " +
		"is traversed, and is never sorted"
	if a.open {
		hint = "these edges do form one chain, but not in this order; the order of a run's edges is the order the " +
			"run is walked, and is never sorted"
	}

	return Diagnostic{
		Severity: SeverityError,
		Span:     a.assembly.loop.at[seam.index],
		Message: fmt.Sprintf(
			"expected the edges of the %s %s in the order it is traversed, found %s, which does not begin at %s, where %s ended",
			a.shape(), a.loopName(), a.edges[seam.index].id, a.cornerName(seam.from), arrived.id,
		),
		Hint: hint,
		Related: []RelatedLocation{
			{Span: a.topology.namedAt(seam.from, Span{}), Message: "the corner the traversal had reached"},
		},
	}
}

// gap reports one seam of the ring which does not join, with how far apart the
// two corners are.
//
// The size is what makes the diagnostic actionable. Four millimetres is a survey
// which moved and a tolerance which wants looking at; four metres is an edge
// naming the wrong corner. Both read as "the loop does not close" without it.
func (a *assembler) gap(seam breakage) Diagnostic {
	what := fmt.Sprintf(
		"expected the loop %s to close, found a gap between %s and %s whose size could not be measured",
		a.loopName(), seam.from, seam.to,
	)
	if gap, ok := a.distance(seam.from, seam.to); ok {
		what = fmt.Sprintf(
			"expected the loop %s to close, found a gap of %s %s between %s and %s",
			a.loopName(), decimal(gap), a.unit, seam.from, seam.to,
		)
	}

	return Diagnostic{
		Severity: SeverityError,
		Span:     a.assembly.loop.at[seam.index],
		Message:  what,
		Hint:     a.closureHint(),
		Related: []RelatedLocation{
			{Span: a.topology.namedAt(seam.from, Span{}), Message: "the corner the traversal had reached"},
			{Span: a.topology.namedAt(seam.to, Span{}), Message: "the corner the next edge begins at"},
		},
	}
}

// closureHint says what closing means, and what the loop was judged against
// where it was judged against anything.
//
// The tolerance is named only where it was applied. A hint which offered it
// whenever the registry declared one would tell somebody whose frame is
// undeclared, or whose tolerance is in another unit, that two corners within
// five millimetres count as one — when nothing was measured and no two corners
// were merged. That is worse than a shorter hint: it sends them to check a
// number which had no part in the answer.
func (a *assembler) closureHint() string {
	hint := "a loop is a ring: each edge begins at the corner the last one ended at, and the last ends where the first began"
	if !a.applicable() {
		return hint
	}

	return fmt.Sprintf(
		"%s; two corners no further apart than the tolerance %s, which is %s %s, count as one",
		hint, a.assembly.tolerance.Name, decimal(a.assembly.tolerance.Value), a.assembly.tolerance.Unit,
	)
}

// applicable reports whether the tolerance could be applied to this loop at all:
// that the registry declares it, that the loop's frame is one the registry
// declares, and that the two are in the same unit.
//
// It is one predicate rather than a condition repeated at each place which needs
// it, because those places have to agree. A hint which says a tolerance decided
// the answer where no corner was merged against it is as wrong as merging
// against a tolerance in another unit would be, and the two going out of step is
// exactly what a second copy of the condition would eventually do.
func (a *assembler) applicable() bool {
	return a.declared && a.unit != "" && a.assembly.tolerance.Unit == a.unit
}

// cornerName names a corner for a diagnostic, listing every vertex the tolerance
// merged into it where it merged any.
//
// A corner which is two vertices judged coincident is named as both, because
// naming one of them would send the author to a vertex which is not the one they
// would have to move.
func (a *assembler) cornerName(corner ID) string {
	var names []string
	for _, edge := range a.edges {
		for _, id := range []ID{edge.start, edge.end} {
			if a.points.find(id) != corner || slices.Contains(names, string(id)) {
				continue
			}
			names = append(names, string(id))
		}
	}

	if len(names) == 0 {
		return string(corner)
	}

	return join(names, "and")
}

// joins reports whether an edge has a corner the traversal could enter it by,
// having reached the given one.
func (a *assembler) joins(corner ID, edge *Edge) bool {
	return a.same(corner, edge.start) || a.same(corner, edge.end)
}

// same reports whether two vertices are the same corner of this loop.
func (a *assembler) same(first, second ID) bool {
	return a.points.find(first) == a.points.find(second)
}

// nearer orders an edge's two vertices by which of them is closer to the corner
// the traversal has reached, so that a gap is reported across the shorter of the
// two ways it could be closed.
//
// Where neither distance can be measured the edge is taken in the order it was
// written, which is the order the author would read it in.
func (a *assembler) nearer(corner ID, edge *Edge) (near, far ID) {
	near, far = edge.start, edge.end

	toStart, measured := a.distance(corner, near)
	if !measured {
		return near, far
	}

	if toEnd, measured := a.distance(corner, far); measured && toEnd < toStart {
		return far, near
	}

	return near, far
}

// distance is how far apart two vertices are, in the unit of the loop's frame,
// and whether that could be measured at all.
//
// It is measurable when both vertices have a resolved position, both are written
// in the frame's unit, and both have the same number of components. Nothing is
// converted and nothing is padded: a position in millimetres compared against one
// in metres, or a two-component position against a three-component one, is a
// question with no answer rather than one with a plausible wrong one
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
func (a *assembler) distance(first, second ID) (float64, bool) {
	if first == second {
		return 0, true
	}

	from, ok := a.at(first)
	if !ok {
		return 0, false
	}

	to, ok := a.at(second)
	if !ok || len(to) != len(from) {
		return 0, false
	}

	var squares float64
	for i := range from {
		delta := from[i] - to[i]
		squares += delta * delta
	}

	gap := math.Sqrt(squares)
	if math.IsNaN(gap) || math.IsInf(gap, 0) {
		return 0, false
	}

	return gap, true
}

// at is a vertex's position in the unit of the loop's frame, and whether it has
// one there.
func (a *assembler) at(vertex ID) ([]float64, bool) {
	value, ok := a.positions[vertex]
	if !ok || a.unit == "" || value.Unit() != a.unit {
		return nil, false
	}
	return value.Coordinate()
}

// frameUnit is the one linear unit of a frame, empty where the registry declares
// no frame of that id.
func frameUnit(registry *Registry, frame ID) Unit {
	declared, ok := registry.Frame(frame)
	if !ok {
		return ""
	}
	return declared.Unit
}

// namedAt is where the id of a geometric node was written, falling back to the
// given span where the model holds no node of that id.
func (t *Topology) namedAt(id ID, fallback Span) Span {
	if declared, ok := t.definitionOf(id); ok {
		return declared.at
	}
	return fallback
}

// merged is a partition of vertex ids into the corners they are the same as.
//
// It is a disjoint-set forest because that is what the question is: coincidence
// is transitive across a chain of vertices each within the tolerance of the next,
// and a map of pairs would answer "is this the same corner" differently depending
// on which of the pair was asked about.
type merged struct {
	// parent is each id's parent in the forest. An id which is its own parent is
	// the representative of its corner.
	parent map[ID]ID
}

// newMerged starts every id in a corner of its own.
func newMerged(ids []ID) *merged {
	m := &merged{parent: make(map[ID]ID, len(ids))}
	for _, id := range ids {
		if _, ok := m.parent[id]; !ok {
			m.parent[id] = id
		}
	}
	return m
}

// find returns the representative of the corner an id belongs to, compressing
// the path it walked.
func (m *merged) find(id ID) ID {
	if _, ok := m.parent[id]; !ok {
		return id
	}

	root := id
	for m.parent[root] != root {
		root = m.parent[root]
	}

	for m.parent[id] != root {
		m.parent[id], id = root, m.parent[id]
	}

	return root
}

// union puts two ids in one corner.
func (m *merged) union(first, second ID) {
	one, other := m.find(first), m.find(second)
	if one == other {
		return
	}
	m.parent[other] = one
}
