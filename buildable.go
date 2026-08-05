// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"slices"
	"strings"
)

// Setbacks is where the setback distances a buildable region is derived from
// are read.
//
// It is the predicate and the claims and nothing else. Which edge is the front
// one, which the rear and which a side is not modelled here and never will be
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)): a
// setback is a claim written on the edge it applies to, so an authority which
// requires six metres at the front and three at the sides writes those numbers
// on those edges and the derivation applies each where it was written.
type Setbacks struct {
	// Predicate is the predicate an edge's setback distance is claimed under.
	//
	// There is no default. A derivation told no predicate would have to guess
	// which of a project's distances is the one which pushes a building back
	// from its boundary, and the guess would be wrong silently.
	Predicate string

	// Claims is the family the setback claims are read from, which is the one
	// [LoadClaims] loaded.
	Claims *Claims
}

// Setback is one edge's setback: how far the boundary is pushed back along that
// edge, and the claim which says so.
//
// The claim is carried rather than only the number, for the reason a
// [Resolution] carries one: "six metres" is a figure somebody has to go and
// check, and "six metres, from this claim, measured this way, this accurate" is
// the answer with the evidence already attached.
type Setback struct {
	// edge is the edge the setback was claimed on.
	edge *Edge

	// distance is how far back it pushes the boundary, in unit.
	distance float64
	unit     Unit

	// claim is the claim it was resolved from.
	claim *Claim
}

// Edge returns the edge the setback was claimed on.
func (s Setback) Edge() *Edge { return s.edge }

// Distance returns how far back the setback pushes the boundary, in
// [Setback.Unit].
func (s Setback) Distance() float64 { return s.distance }

// Unit returns the unit the distance is in, which is the linear unit of the
// region's frame.
func (s Setback) Unit() Unit { return s.unit }

// Claim returns the claim the distance was resolved from.
func (s Setback) Claim() *Claim { return s.claim }

// String renders the setback as a person reads it: the edge, the distance and
// the claim behind it.
func (s Setback) String() string {
	var id ID
	if s.edge != nil {
		id = s.edge.ID()
	}

	written := fmt.Sprintf("%s: %s%s", id, decimal(s.distance), unitSuffix(s.unit))
	if s.claim == nil {
		return written
	}

	if claimed, ok := s.claim.ID(); ok {
		return fmt.Sprintf("%s, from %s", written, claimed)
	}

	return written
}

// Buildable is the area left inside a boundary once every edge's setback has
// been taken off it.
//
// It is derived and never authored
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)), which
// matters more here than anywhere else in this package: a buildable region
// written down as a polygon of its own is a second statement of where a
// permanent structure may go, and the day a setback claim changes it is the
// wrong one. Nothing writes this back, so the region cannot disagree with the
// parcel or with the claims it was computed from — it is recomputed from both
// every time it is asked for.
//
// The setbacks which produced it come back with it. A region without them is a
// shape somebody has to reverse-engineer the rule out of; with them, the answer
// says which claim moved which edge and how far.
//
// The zero Buildable covers nothing and was derived from nothing, and every
// method below works on it.
type Buildable struct {
	// boundary is the region the setbacks were taken off.
	boundary Region

	// region is what is left, which is the answer.
	region Region

	// setbacks are the setbacks which were applied, one per edge of the
	// boundary and in the order the loops traverse them.
	setbacks []Setback
}

// Subject returns the id of the node the boundary was read from.
func (b Buildable) Subject() ID { return b.boundary.Subject() }

// Boundary returns the region the setbacks were taken off, which is the parcel
// as the model holds it.
//
// It is returned beside the answer because the two are read together: what is
// buildable is only meaningful against what the whole of the parcel is, and a
// caller which had to read the parcel a second time could read it against a
// different survey.
func (b Buildable) Boundary() Region { return b.boundary }

// Region returns the buildable area.
//
// It covers nothing where the setbacks meet in the middle, which is an answer
// rather than a failure and is what [Buildable.Empty] reports.
func (b Buildable) Region() Region { return b.region }

// Setbacks returns the setbacks which were applied, one per edge of the
// boundary and in the order the loops traverse it.
func (b Buildable) Setbacks() []Setback { return slices.Clone(b.setbacks) }

// Area returns how much is buildable, in the square of the region's unit.
func (b Buildable) Area() float64 { return b.region.Area() }

// Empty reports whether nothing is buildable.
func (b Buildable) Empty() bool { return b.region.Empty() }

// Budget returns the accumulated accuracy of everything the answer was computed
// from: the position claims which put the boundary's corners where they are,
// and the setback claims which pushed each edge back.
//
// Both families are in one budget rather than reported separately because the
// question the budget answers is about the answer and not about its inputs —
// how well is the edge of the buildable area known — and a control point shared
// between a corner and a setback survey is counted once there and twice in two
// budgets added together ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)).
func (b Buildable) Budget() Budget { return b.region.Budget() }

// String renders the buildable region as a person reads it.
func (b Buildable) String() string {
	if b.boundary.Subject() == "" {
		return "nothing buildable was derived"
	}

	if b.Empty() {
		return fmt.Sprintf("nothing is buildable inside %s", b.boundary.Subject())
	}

	return fmt.Sprintf("%s: %s%s buildable of %s%s",
		b.boundary.Subject(),
		decimal(b.Area()), squareSuffix(b.region.Unit()),
		decimal(b.boundary.Area()), squareSuffix(b.boundary.Unit()),
	)
}

// BuildableOf derives the area left buildable inside a node's boundary once
// every edge's setback has been taken off it.
//
// The boundary is read with [Topology.RegionOf], so everything that refuses a
// region refuses this: a ring which does not close, corners which are not in one
// plane, a boundary which bends, a tolerance the registry does not declare in
// the frame's unit. The setback of each edge is resolved from the claims written
// on that edge, under the predicate [Setbacks] names, by the ordinary resolution
// rule — so the most accurate current claim wins and the evidence comes back
// with the answer.
//
// Every edge needs one. An edge with no live claim under the predicate is a
// diagnostic naming that edge and no region, because the alternative is to read
// the silence as nought and site a building up against a boundary nobody
// intended it to touch. A setback of nought is a thing somebody can write down,
// and writing it down is how it is said.
//
// The construction is [Region.Buffer]'s, applied per edge: the strip within the
// setback of an edge is taken off the parcel, and what is left is what is at
// least that far inside every edge. Corners are rounded to the declared
// tolerance the same way, so a front setback and a side setback meeting at a
// corner cannot disagree about where the corner of the buildable area is. What
// is never produced is the inside-out shape offsetting each edge on its own
// gives when the offsets cross over each other: setbacks which meet in the
// middle leave a region covering nothing, reported as a warning rather than as
// a failure, because "nothing is buildable here" is the answer to the question
// and a permit application is exactly where it needs to be legible.
//
// Diagnostics are collected rather than stopped at: a parcel missing three
// setback claims reports all three, so fixing it is one edit rather than three
// rounds of the same guess.
func (t *Topology) BuildableOf(
	node *SemanticNode,
	boundaries *Boundaries,
	survey Survey,
	setbacks Setbacks,
) (Buildable, []Diagnostic) {
	boundary, diags := t.RegionOf(node, boundaries, survey)

	if !boundary.ready {
		return Buildable{}, append(diags, Diagnostic{
			Severity: SeverityError,
			Span:     boundary.span,
			Message: fmt.Sprintf(
				"expected a boundary to derive a buildable region from, found that %s covers no area which could be set back",
				boundary.name(),
			),
			Hint: "a buildable region is the parcel less the strip along each of its edges; a node with no outline " +
				"bounds no parcel, and where an outline could not be read the reason is on a diagnostic of its own",
		})
	}

	applied, refused := boundary.setbacksOf(t, setbacks)
	if len(refused) > 0 {
		return Buildable{}, append(diags, refused...)
	}

	result := Buildable{boundary: boundary, region: boundary.setBack(applied), setbacks: applied}

	if result.Empty() {
		diags = append(diags, boundary.consumed(applied))
	}

	return result, diags
}

// setbacksOf resolves the setback of every edge of the boundary, in the order
// the loops traverse them, and reports every edge whose setback could not be
// read.
//
// One edge is resolved once however many runs of the boundary it accounts for.
// An edge written into two loops of one node is one edge with one setback on it,
// and reporting it twice would be one missing claim read as two.
func (r Region) setbacksOf(t *Topology, setbacks Setbacks) ([]Setback, []Diagnostic) {
	if setbacks.Predicate == "" || setbacks.Claims == nil {
		found := "no claims to read them from"
		if setbacks.Predicate == "" {
			found = "no predicate named"
		}

		return nil, []Diagnostic{{
			Severity: SeverityError,
			Span:     r.span,
			Message: fmt.Sprintf(
				"expected the predicate an edge's setback is claimed under to derive the buildable region of %s, found %s",
				r.name(), found,
			),
			Hint: "a setback is a claim on the edge it applies to, under a predicate the registry declares; naming " +
				"that predicate is the caller's, because which of a project's distances pushes a building back from " +
				"its boundary is project vocabulary and there is no default for it here",
		}}
	}

	var applied []Setback
	var refused []Diagnostic

	seen := make(map[ID]bool, len(r.segments))

	for _, segment := range r.segments {
		if segment.edge == nil || seen[segment.edge.ID()] {
			continue
		}
		seen[segment.edge.ID()] = true

		setback, diagnostic, ok := r.setbackOf(t, segment.edge, setbacks)
		if !ok {
			refused = append(refused, diagnostic)
			continue
		}

		applied = append(applied, setback)
	}

	return applied, refused
}

// setbackOf resolves one edge's setback, reporting what stopped it where
// nothing could be read.
func (r Region) setbackOf(t *Topology, edge *Edge, setbacks Setbacks) (Setback, Diagnostic, bool) {
	at := t.namedAt(edge.ID(), edge.span)

	// The registry is not asked, because what it decides here is whether an
	// ambiguity is an error, and this refuses one whether the predicate was
	// declared strict or not: two current answers to how far back a building
	// goes is not a question a derivation may pick from.
	resolution, _ := setbacks.Claims.Resolve(edge.ID(), setbacks.Predicate, nil)

	if resolution.Ambiguous() {
		return Setback{}, r.contested(edge, at, setbacks, resolution), false
	}

	claim, resolved := resolution.Claim()
	if !resolved {
		return Setback{}, r.unclaimed(edge, at, setbacks), false
	}

	value := claim.Value()

	distance, scalar := value.Scalar()
	if !scalar {
		return Setback{}, Diagnostic{
			Severity: SeverityError,
			Span:     value.Span(),
			Message: fmt.Sprintf(
				"expected the %s of %s to be a distance, found %s",
				setbacks.Predicate, geometricName(edgeTag, edge.ID()), spellShape(value.Shape()),
			),
			Hint: "a setback is how far back from an edge something has to sit, which is one number; the predicate it " +
				"is claimed under is declared with a scalar value",
			Related: []RelatedLocation{{Span: at, Message: "the edge it was claimed on is written here"}},
		}, false
	}

	if value.Unit() != r.unit {
		return Setback{}, Diagnostic{
			Severity: SeverityError,
			Span:     value.Span(),
			Message: fmt.Sprintf(
				"expected the %s of %s to be in %s, which is the unit of the frame %s is in, found %s",
				setbacks.Predicate, geometricName(edgeTag, edge.ID()), r.unit, r.name(), spellClaimedUnit(value.Unit()),
			),
			Hint: "nothing here converts between units: a frame declares one linear unit, and a distance taken off a " +
				"boundary has to be written in the unit the boundary is",
			Related: []RelatedLocation{{Span: at, Message: "the edge it was claimed on is written here"}},
		}, false
	}

	if distance < 0 {
		return Setback{}, Diagnostic{
			Severity: SeverityError,
			Span:     value.Span(),
			Message: fmt.Sprintf(
				"expected the %s of %s to be a distance inwards from the boundary, found %s",
				setbacks.Predicate, geometricName(edgeTag, edge.ID()), decimal(distance)+unitSuffix(value.Unit()),
			),
			Hint: "a setback takes area away from a parcel and never adds any; a boundary which is meant to be further " +
				"out is a boundary somebody has to move",
			Related: []RelatedLocation{{Span: at, Message: "the edge it was claimed on is written here"}},
		}, false
	}

	if distance > 0 && distance < r.tolerance.Value {
		return Setback{}, Diagnostic{
			Severity: SeverityError,
			Span:     value.Span(),
			Message: fmt.Sprintf(
				"expected the %s of %s to be further than the tolerance %s, which is %s %s, found %s",
				setbacks.Predicate, geometricName(edgeTag, edge.ID()), r.tolerance.Name,
				decimal(r.tolerance.Value), r.tolerance.Unit, decimal(distance)+unitSuffix(value.Unit()),
			),
			Hint: "a setback shorter than the distance two corners are the same corner within moves the boundary by " +
				"less than the model knows where it is; a setback of nought is written as nought and applies nothing",
			Related: []RelatedLocation{
				{Span: r.tolerance.Span, Message: "the tolerance is declared here"},
				{Span: at, Message: "the edge it was claimed on is written here"},
			},
		}, false
	}

	return Setback{edge: edge, distance: distance, unit: value.Unit(), claim: claim}, Diagnostic{}, true
}

// unclaimed reports an edge nothing says the setback of.
//
// It names the edge rather than the parcel, because the edit is on the edge, and
// it is an error rather than a nought applied quietly: the whole of this
// derivation is that the buildable region cannot disagree with the setbacks, and
// a missing claim read as nought is a disagreement with a rule nobody wrote
// down.
func (r Region) unclaimed(edge *Edge, at Span, setbacks Setbacks) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Span:     at,
		Message: fmt.Sprintf(
			"expected a %s claimed on every edge bounding %s to derive its buildable region, found none on %s",
			setbacks.Predicate, r.name(), geometricName(edgeTag, edge.ID()),
		),
		Hint: fmt.Sprintf(
			"write it on the edge as a claim with its source, its method and its accuracy, the way every other "+
				"measured value is written; an edge nothing sets back is written (%s (value 0 %s) ...) and says so",
			setbacks.Predicate, r.unit,
		),
	}
}

// contested reports an edge whose setback more than one claim is equally
// current about.
//
// It is refused rather than broken by picking one, for the reason resolution
// refuses it: two equally good statements of how far back a building goes do not
// decide between themselves, and the region drawn from either of them would look
// exactly like the region drawn from the other.
func (r Region) contested(edge *Edge, at Span, setbacks Setbacks, resolution Resolution) Diagnostic {
	candidates := resolution.Candidates()

	written := make([]string, 0, len(candidates))
	related := make([]RelatedLocation, 0, len(candidates))
	for _, candidate := range candidates {
		value := candidate.Value()
		written = append(written, decimal(scalarOf(value))+unitSuffix(value.Unit()))
		related = append(related, RelatedLocation{Span: candidate.Span(), Message: "one of them is written here"})
	}

	return Diagnostic{
		Severity: SeverityError,
		Span:     at,
		Message: fmt.Sprintf(
			"expected one current %s of %s to derive the buildable region of %s, found %s equally current: %s",
			setbacks.Predicate, geometricName(edgeTag, edge.ID()), r.name(),
			count(len(candidates)), join(written, "and"),
		),
		Hint: "an ambiguity is never broken by picking one; retract the claim which no longer applies, or state the " +
			"accuracy which separates them",
		Related: related,
	}
}

// consumed reports a parcel its own setbacks leave nothing of.
//
// It is a warning and not an error. Nothing is wrong with the model — the
// parcel is the parcel and the setbacks are the setbacks — and what it says is
// the answer to the question: a plot 8 m deep with a 5 m setback front and back
// has nowhere on it a building may go. Reporting it as a failure would leave a
// caller unable to tell "this cannot be worked out" from "this was worked out
// and the answer is none of it".
func (r Region) consumed(applied []Setback) Diagnostic {
	return Diagnostic{
		Severity: SeverityWarning,
		Span:     r.span,
		Message: fmt.Sprintf(
			"the setbacks of %s leave nothing buildable: %s%s of parcel, set back %s along its %s",
			r.name(), decimal(r.Area()), squareSuffix(r.unit), spellSetbacks(applied), plural(len(applied), "edge"),
		),
		Hint: "this is the answer rather than a failure — the setbacks meet in the middle — and it is reported so " +
			"that an empty region cannot be read as one which was never computed",
	}
}

// spellSetbacks is how wide a set of setbacks is, for a message which is about
// all of them at once: the one distance where they agree, and the range they
// cover where they do not.
func spellSetbacks(applied []Setback) string {
	if len(applied) == 0 {
		return "nothing"
	}

	smallest, largest := applied[0], applied[0]
	for _, setback := range applied[1:] {
		if setback.distance < smallest.distance {
			smallest = setback
		}
		if setback.distance > largest.distance {
			largest = setback
		}
	}

	if smallest.distance == largest.distance {
		return decimal(largest.distance) + unitSuffix(largest.unit)
	}

	return fmt.Sprintf("%s to %s",
		decimal(smallest.distance)+unitSuffix(smallest.unit),
		decimal(largest.distance)+unitSuffix(largest.unit),
	)
}

// setBack is the region with the strip along each edge taken off it.
//
// It is one overlay rather than one per edge. Taking each strip off in turn
// would be a sequence of results each rounded to the arrangement's own
// tolerance, and the corner where two of them meet would be decided by the order
// the edges happened to be traversed in.
func (r Region) setBack(applied []Setback) Region {
	distances := make(map[ID]float64, len(applied))
	for _, setback := range applied {
		distances[setback.edge.ID()] = setback.distance
	}

	var strips []contour

	for _, segment := range r.segments {
		if segment.edge == nil {
			continue
		}

		distance, ok := distances[segment.edge.ID()]
		if !ok || distance == 0 {
			// A setback of nought moves nothing, and drawing a strip of no width
			// to say so would put a degenerate ring into the arrangement.
			continue
		}

		from, to := r.basis.project(segment.from), r.basis.project(segment.to)

		if strip, ok := slab(from, to, distance); ok {
			strips = append(strips, strip)
		}

		// The discs are what a corner of the buildable region is rounded by, and
		// they are the same construction [Region.Buffer] rounds a corner with:
		// everything within the setback of the edge, which at the ends of the
		// edge is everything within the setback of its last corner.
		strips = append(strips, disc(from, distance, r.tolerance.Value), disc(to, distance, r.tolerance.Value))
	}

	result := r.derive()
	result.budget = Budget{}
	result.budget.Merge(r.budget)
	for _, setback := range applied {
		result.budget.Add(setback.claim)
	}

	result.pieces = piecesOf(
		overlay(r.figure(r.basis), strips, r.tolerance.Value, coveredByFirstAlone), r.basis)

	return result
}

// scalarOf is a value's number, and nought for a value which is not one.
//
// It is only reached from a diagnostic which is already reporting that the
// claims disagree, where a value of the wrong shape is a second problem the same
// edit fixes and not one this rendering should refuse to describe.
func scalarOf(value Value) float64 {
	number, _ := value.Scalar()
	return number
}

// spellClaimedUnit names the unit a claim wrote, for a diagnostic about a claim
// whose unit is not the one the frame is in.
//
// A value written with no unit is spelled as having none rather than as an empty
// string, which is what a message reading `found ` would leave behind.
func spellClaimedUnit(unit Unit) string {
	if unit == "" {
		return "one written with no unit"
	}
	return string(unit)
}

// Report is the buildable region rendered for a person: the summary, and one
// line per setback beneath it.
//
// It is here rather than in the command so that a library caller reporting the
// answer and the command reporting it write the same thing.
func (b Buildable) Report() string {
	var out strings.Builder

	out.WriteString(b.String())
	for _, setback := range b.setbacks {
		out.WriteString("\n  ")
		out.WriteString(setback.String())
	}

	return out.String()
}
