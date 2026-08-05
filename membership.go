// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
)

// Membership is one observation judged against the area one semantic node
// covers.
//
// It is derived and is written nowhere
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)). A shot
// is *in* a place as a consequence of two things — where it was taken and where
// the boundary runs — and neither of them is written on the observation record.
// Writing the answer down would guarantee it went stale the first time somebody
// moved the boundary, and the shot which is suddenly outside would go on saying
// it was inside in a file nobody had any reason to open.
//
// A shot *of* a thing is the opposite and is stored: the entity names the file
// the shot was written in, which is a deliberate statement that this evidence
// backs this thing. [Membership.Linked] is which of the two this result is, and
// the two are not interchangeable — a total station set up on a monument shoots
// it from a place which may be inside somebody else's garden.
//
// The zero Membership holds no observation and every method below works on it.
type Membership struct {
	// subject is the node the observation was judged against.
	subject ID

	// observation is the record judged, which is held rather than copied: it is
	// the log's, it is large, and every question about the shot itself — when,
	// what fix, whose session — is answered by reading it.
	observation *Observation

	// frame is the frame the judgement was made in, which is the subject's, and
	// unit that frame's linear unit.
	frame ID
	unit  Unit

	// at is the coordinate in that frame, which is the record's own where the
	// two frames are one and the carried coordinate where they are not.
	at      Point
	carried bool

	// offset is how far the shot lies out of the plane of the figure, which is
	// never a reason to exclude it and is always worth knowing.
	offset float64

	// clearance is how far the shot is from the nearest run of the boundary,
	// measured in the plane of the figure and positive inside it.
	clearance float64

	// doubt is how well that clearance is known: the stated tolerance plus the
	// combined standard uncertainty of the shot and of the transform which
	// carried it, per [Graph.ObservationsWithin].
	doubt float64

	// bounded is whether doubt could be arrived at at all. A transform whose
	// accuracy nothing states leaves it unbounded, which makes every shot
	// carried across it ambiguous rather than confidently anything.
	bounded bool

	// ambiguous is whether the shot is closer to the boundary than doubt.
	ambiguous bool

	// linked is whether the subject entity names the file this shot was written
	// in, which is the stored half of the relationship.
	linked bool

	// tolerance is the declared tolerance the boundary rule was applied
	// against, kept so that a result can say which decision decided it.
	tolerance Tolerance

	// budget is the accuracy of the transform the shot was carried across, and
	// holds no terms where it was not carried at all.
	budget Budget
}

// Subject returns the id of the node the observation was judged against.
func (m Membership) Subject() ID { return m.subject }

// Observation returns the record which was judged.
func (m Membership) Observation() *Observation { return m.observation }

// Frame returns the frame the judgement was made in, which is the subject's and
// not necessarily the one the record was written in.
func (m Membership) Frame() ID { return m.frame }

// Unit returns the linear unit of that frame, which every figure here is in.
func (m Membership) Unit() Unit { return m.unit }

// At returns the coordinate the judgement was made from, in [Membership.Frame].
//
// It is the record's own coordinate where the record was written in that frame,
// and the carried coordinate where it was not. Reading it rather than the
// record's is what a caller plotting the shot against the region wants: the two
// are the same position and are different numbers.
func (m Membership) At() Point { return m.at }

// Carried reports whether the shot was written in another frame and brought
// across to be judged.
//
// It is worth knowing on its own. A carried shot is judged through a chain of
// measured transforms, so its position is known less well than the record's own
// precision says, and [Membership.Budget] is what that cost.
func (m Membership) Carried() bool { return m.carried }

// Offset returns how far the shot lies out of the plane of the region, in
// [Membership.Unit].
//
// It is never negative and is never a reason a shot is excluded: membership is
// a question asked in plan, and a shot of the ground inside a garden is
// routinely a few tenths of a metre off the plane the boundary corners happen
// to define. What it is for is telling a shot on this storey from one two floors
// above it, which is a judgement about the model rather than about the geometry
// and is the caller's to make.
func (m Membership) Offset() float64 { return m.offset }

// Clearance returns how far the shot is from the nearest run of the boundary,
// measured in the plane of the region and in [Membership.Unit].
//
// It is positive inside the region and negative outside it, so an ambiguous
// shot says which side of the boundary it fell on as well as how near it was.
func (m Membership) Clearance() float64 { return m.clearance }

// Doubt returns how well [Membership.Clearance] is known, in [Membership.Unit],
// and whether it could be arrived at at all.
//
// It is the band [Graph.ObservationsWithin] judges the boundary against, and it
// is three things added: the declared tolerance, the shot's own horizontal
// precision and the standard uncertainty of the transform which carried it. A
// false second return is a transform whose accuracy nothing states, which
// bounds nothing and makes the shot ambiguous whatever its clearance.
func (m Membership) Doubt() (float64, bool) { return m.doubt, m.bounded }

// Ambiguous reports whether the shot is nearer the boundary than it is known
// to.
//
// Every result [Graph.ObservationsWithin] returns is either confidently inside
// or ambiguous, and the two are separate slices of a [Members] rather than a
// flag on one, so that a caller cannot silently assign a shot the model cannot
// place. This is here for the caller which has one result in hand.
func (m Membership) Ambiguous() bool { return m.ambiguous }

// Linked reports whether the subject entity names the observation file this
// shot was written in.
//
// That link is stored, is what the entity format writes as `observed-in`, and is
// what makes a shot one *of* the subject: somebody decided this evidence backs
// this thing. A result with it false is a shot merely *in* the place, which
// nothing wrote down anywhere and which is true only because of where the shot
// and the boundary are.
//
// It is the subject's own links which decide it, and not those of the corners
// bounding it. A shot cited by the corner is evidence about the corner.
func (m Membership) Linked() bool { return m.linked }

// Tolerance returns the declared tolerance the boundary rule was applied
// against.
func (m Membership) Tolerance() Tolerance { return m.tolerance }

// Budget returns the accuracy of the transform the shot was carried across,
// which holds no terms where it was not carried at all.
func (m Membership) Budget() Budget { return m.budget }

// String writes where the shot fell and how well that is known.
//
// The three renderings are the three states, and the unbounded one is spelled
// out rather than folded into "near the boundary": a shot four metres inside a
// region on a fit nobody stated the accuracy of is not near anything, and saying
// it was would be the one reading which sends somebody out to remeasure the
// wrong thing.
func (m Membership) String() string {
	if m.observation == nil {
		return "no observation"
	}

	var out strings.Builder

	out.WriteString(string(m.observation.ID))

	switch {
	case !m.bounded:
		out.WriteString(" cannot be placed against ")
		out.WriteString(string(m.subject))
		out.WriteString(": it is ")
		out.WriteString(decimal(math.Abs(m.clearance)))
		out.WriteString(unitSuffix(m.unit))
		out.WriteString(side(m.clearance))
		out.WriteString(", by an amount nothing bounds")

	case m.ambiguous:
		out.WriteString(" is within ")
		out.WriteString(decimal(math.Abs(m.clearance)))
		out.WriteString(unitSuffix(m.unit))
		out.WriteString(" of the boundary of ")
		out.WriteString(string(m.subject))
		out.WriteString(", known to ")
		out.WriteString(decimal(m.doubt))
		out.WriteString(unitSuffix(m.unit))

	default:
		out.WriteString(" is ")
		out.WriteString(decimal(m.clearance))
		out.WriteString(unitSuffix(m.unit))
		out.WriteString(" inside ")
		out.WriteString(string(m.subject))
		out.WriteString(", known to ")
		out.WriteString(decimal(m.doubt))
		out.WriteString(unitSuffix(m.unit))
	}

	if m.linked {
		out.WriteString("; it is a shot of it")
	}

	return out.String()
}

// side is which side of the boundary a clearance puts a shot on, written the way
// a sentence which has already stated the distance needs it.
func side(clearance float64) string {
	if clearance < 0 {
		return " outside it"
	}
	return " inside it"
}

// Members is which observations fall inside the area one semantic node covers.
//
// The two answers are two slices rather than one carrying a flag, which is the
// whole point of the type: a shot the model cannot place is not assignable by a
// caller who forgot to look. Ranging over [Members.Inside] is the ordinary
// question — which shots are in this region — and it cannot pick up a shot which
// is only nearly in it.
//
// The zero Members holds nothing and every method below works on it.
type Members struct {
	// subject is the node the observations were judged against.
	subject ID

	// tolerance is the declared tolerance the boundary rule was applied
	// against.
	tolerance Tolerance

	// inside are the shots further inside the boundary than they are known to,
	// and ambiguous those nearer it than that.
	inside    []Membership
	ambiguous []Membership
}

// Subject returns the id of the node the observations were judged against.
func (m Members) Subject() ID { return m.subject }

// Tolerance returns the declared tolerance the boundary rule was applied
// against.
func (m Members) Tolerance() Tolerance { return m.tolerance }

// Inside returns the shots which fall inside the region by more than they are
// known to, in record identity order.
func (m Members) Inside() []Membership { return slices.Clone(m.inside) }

// Ambiguous returns the shots nearer the boundary than they are known to, in
// record identity order.
//
// They are reported rather than assigned. A shot 20 mm outside a boundary,
// taken with a float solution good to 240 mm, is not outside the region: it is a
// shot nobody can place, and the honest answer is to say so and let whoever
// cares reshoot it.
func (m Members) Ambiguous() []Membership { return slices.Clone(m.ambiguous) }

// Len reports how many shots were placed at all, inside and ambiguous together.
func (m Members) Len() int { return len(m.inside) + len(m.ambiguous) }

// String writes how many fell each way.
func (m Members) String() string {
	return fmt.Sprintf("%s holds %d observations, with %d too near its boundary to place",
		m.subject, len(m.inside), len(m.ambiguous))
}

// ObservationsWithin returns the observations of the model which fall inside the
// area a semantic node covers.
//
// **Nothing here is stored and nothing here is read from the model.** Which
// shots are in a region is computed from their coordinates and the region's
// boundary every time it is asked, so carving a new region out of the back
// garden gives it the shots inside it with no edit to an observation file
// anywhere — and moving that boundary again moves the answer with it
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// The figure comes from [Graph.Derive], so a membership query over a model whose
// derived geometry is already cached pays for the point tests and not for
// reading the boundary again. against is that derivation: the named tolerance
// and the named position predicate, and the cache to look in.
//
// # The rule at the boundary
//
// A shot is inside when it lies inside the figure by further than it is known
// to, and ambiguous when it is nearer the boundary than that. What it is known
// to is three things added:
//
//   - the tolerance named by against, which is how close two corners are one
//     corner and so how well the boundary itself is placed
//     ([0012](docs/decisions/0012-tolerances-are-registry-data.md));
//   - the shot's own horizontal precision, at one standard deviation
//     ([0006](docs/decisions/0006-accuracy-is-one-sigma.md));
//   - the combined standard uncertainty of the transform, where the shot was
//     carried in from another frame.
//
// The tolerance is added rather than combined in quadrature with the other two
// because it is not an error: it is a distance the project has stated it does
// not distinguish from zero, and a stated indifference does not cancel against a
// measurement. The two statistical terms are combined with each other in
// quadrature, being independent one-sigma figures.
//
// A shot which is neither inside nor ambiguous is outside, and is not a result.
//
// # Frames
//
// A record written in another frame is carried into the subject's frame before
// it is tested, through the chain of measured transforms between them, and the
// accuracy of that chain is accumulated into [Membership.Budget] and into the
// band above. A frame with no chain to the subject's is reported once, naming
// the frame and how many records were written in it, rather than once per record
// or not at all.
//
// A transform whose accuracy nothing states bounds nothing. A shot carried
// across one is never confidently inside anything: it comes back reported rather
// than assigned, with [Membership.Doubt] saying that no band could be arrived
// at. Treating it as exact instead is what would make a cross-frame membership
// look better than the survey behind it.
//
// Such a shot still has to be filtered on something, or a coordinate on the far
// side of the county would be reported as one this region cannot place. What it
// is filtered on is the tolerance and the shot's own precision — the two terms
// which *were* stated — so it is reported where it lands near the region and is
// no result where it plainly does not.
//
// # What is judged
//
// Every record of every observation file the model links to, minus those a
// retirement names: a retired shot is not evidence and is not a member of
// anywhere. **This reads those files**, which is the one thing
// [Graph.Observations] exists to defer; a command which never asks this question
// never opens one.
//
// Results are ordered by record identity, then by where the record was written,
// which is deterministic and independent of the order the model was walked in.
func (g *Graph) ObservationsWithin(subject ID, against Derivation) (Members, []Diagnostic) {
	if g == nil {
		return Members{}, nil
	}

	node, held := g.nodes.Node(subject)
	if !held {
		return Members{}, []Diagnostic{{
			Severity: SeverityError,
			Span:     Span{Start: Position{Path: g.root}},
			Message:  fmt.Sprintf("expected a region to place observations in, found no semantic node %s", subject),
			Hint: "membership is asked of a semantic node which covers area; a corner, an edge and a loop are " +
				"geometry a region is bounded by rather than places a shot can be in",
		}}
	}

	tolerance, declared := g.registry.Tolerance(against.Tolerance)
	if !declared {
		return Members{}, []Diagnostic{g.registry.Undeclared(SortTolerance, against.Tolerance, g.named(node))}
	}

	prints, diags := g.Derive(against)

	print, derived := prints.Of(subject)
	if !derived {
		return Members{}, append(diags, Diagnostic{
			Severity: SeverityError,
			Span:     g.named(node),
			Message: fmt.Sprintf(
				"expected a region to place observations in, found that %s covers no area", subject,
			),
			Hint: "a region is read from the loops bounding a node; a node which references none covers nothing, " +
				"and where one could not be read the reason is on a diagnostic of its own",
		})
	}

	if tolerance.Unit != print.unit {
		return Members{}, append(diags, Diagnostic{
			Severity: SeverityError,
			Span:     g.named(node),
			Message: fmt.Sprintf(
				"expected the tolerance %s to be declared in %s, which the frame %s of %s is in, found %s",
				tolerance.Name, spellUnit(print.unit), print.frame, subject, spellUnit(tolerance.Unit),
			),
			Hint: "nothing here converts a unit: a tolerance in metres applied to a figure in millimetres is out by " +
				"a thousand, and a shot would be placed either way",
			Related: []RelatedLocation{
				{Span: tolerance.Span, Message: "the tolerance is declared here"},
			},
		})
	}

	caught, planar := catchmentOf(print.pieces)
	if !planar {
		return Members{}, append(diags, Diagnostic{
			Severity: SeverityError,
			Span:     g.named(node),
			Message: fmt.Sprintf(
				"expected the figure of %s to lie in a plane observations could be tested against, found that it lies in none",
				subject,
			),
			Hint: "a region whose corners enclose no area has no plane to project a shot onto; the geometry it was " +
				"read from is what to look at",
		})
	}

	log, logDiags := g.AllObservations()
	diags = append(diags, logDiags...)

	members := Members{subject: subject, tolerance: tolerance}

	across := &carrier{frames: g.frames, into: print.frame, unit: print.unit}

	// Which records the subject cites is asked once rather than once per shot.
	// It is the same answer for every record of the corpus, and reading it
	// inside the loop would resolve the subject's link set thousands of times to
	// arrive at it thousands of times.
	cited := g.cites(node)

	for observation := range log.Current() {
		at, budget, sigma, bounded, ok := across.carry(observation)
		if !ok {
			continue
		}

		doubt := tolerance.Value + math.Hypot(observation.HorizontalPrecision, sigma)

		point := caught.basis.project(at)
		if caught.beyond(point, doubt) {
			continue
		}

		clearance := caught.clearance(point)
		if bounded && clearance < -doubt {
			continue
		}

		member := Membership{
			subject:     subject,
			observation: observation,
			frame:       print.frame,
			unit:        print.unit,
			at:          at,
			carried:     observation.Frame != print.frame,
			offset:      caught.basis.distance(at),
			clearance:   clearance,
			doubt:       doubt,
			bounded:     bounded,
			ambiguous:   !bounded || math.Abs(clearance) <= doubt,
			linked:      cited[observation.ID],
			tolerance:   tolerance,
			budget:      budget,
		}

		if member.ambiguous {
			members.ambiguous = append(members.ambiguous, member)
			continue
		}

		members.inside = append(members.inside, member)
	}

	slices.SortFunc(members.inside, compareMembership)
	slices.SortFunc(members.ambiguous, compareMembership)

	return members, append(diags, across.refusals()...)
}

// compareMembership orders two results by the identity of the record behind
// them, and then by where that record was written.
//
// The identity alone would be enough for a log the validator accepts, because a
// record identity is minted once. The span is the tie-break for the log it does
// not: two records under one identity is a diagnostic, and an order which
// depended on which of them a map happened to yield first would make the output
// of a run differ from the output of the same run.
func compareMembership(a, b Membership) int {
	if order := strings.Compare(string(a.observation.ID), string(b.observation.ID)); order != 0 {
		return order
	}
	return comparePositions(a.observation.Span.Start, b.observation.Span.Start)
}

// cites is the identities of the records written in the observation files an
// entity names, which is what makes each of them a shot *of* it.
//
// It answers by identity rather than by path. The path a record was read from is
// the path this run reached the file by, and the path an entity wrote is
// relative to the model root; comparing the two means reconstructing one from
// the other, and a comparison which is nearly right is a link which is silently
// missed. What the entity links resolves to a set of records, and a record is
// either in that set or it is not.
//
// An entity which links nothing yields nothing, which reads the same as a set
// holding no record and costs no map.
func (g *Graph) cites(entity Entity) map[ID]bool {
	if len(entity.ObservedIn()) == 0 {
		return nil
	}

	log, _ := g.Observations(entity)

	cited := make(map[ID]bool, log.Len())
	for observation := range log.Observations() {
		cited[observation.ID] = true
	}

	return cited
}

// carrier brings observations into one frame, remembering what each frame it
// has seen cost and what it could not do at all.
//
// The route between two frames and the accuracy of that route are the same
// answer for every record written in one frame, and a corpus is thousands of
// records written in a handful of them. Working it out per record would walk the
// frame tree once per shot; working it out per frame walks it once per frame.
type carrier struct {
	// frames is the resolved frame tree, into the frame every answer is
	// expressed in, and unit that frame's linear unit.
	frames *Frames
	into   ID
	unit   Unit

	// known is what each frame seen so far costs to come from.
	known map[ID]carriage

	// refused is the frames which could not be reached from, each with the
	// first record written in it and how many there were.
	refused map[ID]*refusal
}

// carriage is what coming from one frame costs: the budget of the route, the
// standard uncertainty that combines to, and whether it combines to one at all.
type carriage struct {
	budget  Budget
	sigma   float64
	bounded bool
}

// refusal is a frame nothing relates to the one being judged in, with the
// evidence a diagnostic about it needs.
type refusal struct {
	first *Observation
	count int
	err   error
}

// carry expresses one record's coordinate in the carrier's frame, with what the
// carrying cost.
//
// The last return is whether the record can be judged at all. A record in a
// frame no chain of measured transforms reaches is not outside the region and is
// not ambiguously in it: it is a coordinate with no relationship to the boundary
// whatsoever, and it is reported as that rather than placed.
func (c *carrier) carry(observation *Observation) (Point, Budget, float64, bool, bool) {
	if observation.Frame == c.into {
		// A record already in the frame is not transformed, so there is no
		// transform to be uncertain about. Its own precision is the caller's to
		// add and is not read here.
		return observation.Coordinate, Budget{}, 0, true, true
	}

	if c.known == nil {
		c.known = make(map[ID]carriage)
	}

	cost, seen := c.known[observation.Frame]
	if !seen {
		budget, err := c.frames.TransformBudget(observation.Frame, c.into)
		if err != nil {
			c.refuse(observation, err)
			return Point{}, Budget{}, 0, false, false
		}

		cost = carriage{budget: budget}
		if uncertainty, err := budget.Combined(); err == nil && uncertainty.Unit == c.unit {
			cost.sigma, cost.bounded = uncertainty.Standard(), true
		}

		c.known[observation.Frame] = cost
	}

	at, err := c.frames.TransformPoint(observation.Coordinate, observation.Frame, c.into)
	if err != nil {
		c.refuse(observation, err)
		return Point{}, Budget{}, 0, false, false
	}

	return at, cost.budget, cost.sigma, cost.bounded, true
}

// refuse records that a frame could not be carried from, keeping the first
// record which was written in it.
func (c *carrier) refuse(observation *Observation, err error) {
	if c.refused == nil {
		c.refused = make(map[ID]*refusal)
	}

	if held, seen := c.refused[observation.Frame]; seen {
		held.count++
		return
	}

	c.refused[observation.Frame] = &refusal{first: observation, count: 1, err: err}
}

// refusals is one diagnostic per frame which could not be carried from, in
// frame order.
//
// One per frame and not one per record. A season of field work written in a
// frame nobody has tied in yet is one mistake, and ten thousand diagnostics
// about it would bury every other thing wrong with the model — while saying
// nothing ten thousand times which the first did not say once. The count is on
// the message because how much evidence is stranded is the part which decides
// whether it is worth tying the frame in.
func (c *carrier) refusals() []Diagnostic {
	if len(c.refused) == 0 {
		return nil
	}

	var diags []Diagnostic

	for _, frame := range slices.Sorted(maps.Keys(c.refused)) {
		held := c.refused[frame]

		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Span:     held.first.Span,
			Message: fmt.Sprintf(
				"expected to place %s written in %s, found that it is not related to %s: %s",
				plural(held.count, "observation record"), frame, c.into, held.err,
			),
			Hint: "two frames are related by the chain of measured transforms between them; where there is no chain " +
				"a coordinate in one says nothing at all about a boundary in the other",
		})
	}

	return diags
}

// catchment is the figure a region catches shots with: the plane it lies in, its
// rings projected into that plane's axes, and the box which bounds them.
//
// It is built from a [Footprint] rather than from a [Region] because the
// footprint is what the derived cache holds, and reading the boundary again to
// get a region would pay for exactly what the cache exists to have paid for
// already.
type catchment struct {
	// basis is the plane the figure lies in and the axes every test is computed
	// in.
	basis plane

	// pieces are its connected parts, each an outer ring with the rings taken
	// out of it, projected into those axes and oriented.
	pieces []catchmentPiece

	// min and max bound every ring, in the same axes. They are what makes a
	// corpus affordable: a shot beyond them by further than it is known to is
	// outside, and saying so costs two comparisons rather than a walk of every
	// run of the boundary.
	min, max vec
}

// catchmentPiece is one connected part of a figure, in a plane's own axes.
type catchmentPiece struct {
	outer contour
	holes []contour
}

// catchmentOf projects a figure into the plane it lies in, and reports whether
// it lies in one at all.
func catchmentOf(pieces []Piece) (catchment, bool) {
	if len(pieces) == 0 {
		return catchment{}, false
	}

	basis, planar := planeOf(normalOf(pieces[0].outer), pieces[0].outer[0])
	if !planar {
		return catchment{}, false
	}

	caught := catchment{basis: basis}

	first := true
	stretch := func(ring contour) {
		for _, point := range ring {
			if first {
				caught.min, caught.max, first = point, point, false
				continue
			}
			caught.min = vec{math.Min(caught.min.X, point.X), math.Min(caught.min.Y, point.Y)}
			caught.max = vec{math.Max(caught.max.X, point.X), math.Max(caught.max.Y, point.Y)}
		}
	}

	for _, piece := range pieces {
		outer := oriented(project(piece.outer, basis), true)
		stretch(outer)

		part := catchmentPiece{outer: outer}
		for _, hole := range piece.holes {
			// A hole is projected and oriented and is not stretched into the
			// box: it is inside the ring which was, so it can only make the box
			// no larger, and a box which grew by one would be a box that ring
			// did not bound.
			part.holes = append(part.holes, oriented(project(hole, basis), false))
		}

		caught.pieces = append(caught.pieces, part)
	}

	return caught, !first
}

// beyond reports whether a point is outside the box bounding the figure by
// further than doubt, which is the one answer no test of the rings could
// overturn.
func (c catchment) beyond(point vec, doubt float64) bool {
	return point.X < c.min.X-doubt || point.X > c.max.X+doubt ||
		point.Y < c.min.Y-doubt || point.Y > c.max.Y+doubt
}

// holds reports whether a point falls inside the figure: inside the outer ring
// of some piece and inside none of that piece's holes.
//
// The pieces are kept apart rather than being one list of rings so that a hole
// of one piece cannot take a bite out of another. Two rooms either side of a
// courtyard are one figure of two pieces, and the courtyard belongs to the piece
// it was cut from.
func (c catchment) holds(point vec) bool {
	for _, piece := range c.pieces {
		if !piece.outer.holds(point) {
			continue
		}

		within := true
		for _, hole := range piece.holes {
			if hole.holds(point) {
				within = false
				break
			}
		}

		if within {
			return true
		}
	}

	return false
}

// clearance is how far a point is from the nearest run of the boundary,
// positive inside the figure and negative outside it.
//
// Both halves are the same distance and only the sign is a question about
// where the point is. A hole's boundary counts as boundary, so a shot in a
// courtyard is outside the plate around it by how far it is from the courtyard's
// edge rather than by how far it is from the plate's.
func (c catchment) clearance(point vec) float64 {
	nearest := math.Inf(1)

	along := func(ring contour) {
		for i, from := range ring {
			nearest = math.Min(nearest, toSegment(point, segment{a: from, b: ring[(i+1)%len(ring)]}))
		}
	}

	for _, piece := range c.pieces {
		along(piece.outer)
		for _, hole := range piece.holes {
			along(hole)
		}
	}

	if math.IsInf(nearest, 1) {
		return 0
	}

	if !c.holds(point) {
		return -nearest
	}

	return nearest
}
