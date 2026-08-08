// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"math"
	"strings"
)

// Verdict is what a siting query concluded about whether something fits, judged
// against the uncertainty of its own answer.
//
// Three of the four are answers and the fourth is a refusal to give one. The
// distinction which matters is between [VerdictFits] and [VerdictMightFit]: a
// clearance of 40 mm is a comfortable fit where the answer is known to 5 mm and
// is no answer at all where it is known to 60 mm, and a result which reported
// both as "it fits" would be telling somebody to pour concrete on the strength
// of a number smaller than its own error bar.
type Verdict string

const (
	// VerdictFits is a proposal which clears the envelope by more than the
	// uncertainty of the clearance.
	VerdictFits Verdict = "fits"

	// VerdictMightFit is a proposal whose clearance is inside the uncertainty of
	// the clearance, in either direction.
	//
	// It is neither a pass nor a failure and it is not a rounding of one into
	// the other. What it says is that the model, as measured, cannot tell: the
	// answer is to go and measure the thing which dominates the budget, which is
	// what [Budget.Dominant] names.
	VerdictMightFit Verdict = "might-fit"

	// VerdictDoesNotFit is a proposal which misses the envelope by more than the
	// uncertainty of the clearance.
	VerdictDoesNotFit Verdict = "does-not-fit"

	// VerdictUnknown is a clearance whose uncertainty could not be computed, so
	// that there is nothing to judge it against.
	//
	// A claim which states no accuracy is the usual cause, and it is why this is
	// not folded into [VerdictMightFit]: "the model cannot tell" and "the model
	// was never asked" need different work done about them
	// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)).
	VerdictUnknown Verdict = "unknown"
)

// String returns the verdict as it is written.
func (v Verdict) String() string { return string(v) }

// Decided reports whether the verdict answers the question.
//
// [VerdictFits] and [VerdictDoesNotFit] do; the other two say that the question
// stands, for two different reasons.
func (v Verdict) Decided() bool { return v == VerdictFits || v == VerdictDoesNotFit }

// Siting is what a fit is judged against: the frames the answer is composed
// across, and the clearance the proposal is required to keep inside the
// envelope.
//
// It is a struct rather than two more parameters for the reason [Setbacks] is:
// the two travel together, and a query given one of them has not been asked a
// question yet.
type Siting struct {
	// Frames is the frame graph the proposal is carried across, which is the one
	// [ResolveFrames] produced.
	//
	// It is required only where the two things being compared are declared in
	// different frames, which is the case this whole query exists for. Where it
	// is needed and absent, the answer is a diagnostic rather than a comparison
	// of two frames' numbers because they are both numbers.
	Frames *Frames

	// Clearance is how much room the proposal is required to keep between
	// itself and the envelope's boundary, in the linear unit of the envelope's
	// frame.
	//
	// Nought is the ordinary case and means "inside it at all". A positive
	// distance is a maintenance strip, an access route, a fire appliance
	// standing: the proposal has to be that far inside as well as inside. There
	// is no default beyond nought, and a negative distance is refused — a
	// requirement to overhang the boundary is not a requirement.
	Clearance float64
}

// Fit is the answer to whether one thing sits inside another: the clearance
// between them, how well that clearance is known, and what that makes of the
// question.
//
// It is the query the rest of this engine exists to make answerable, and the
// only one which touches every part of it at once. The proposal is read out of
// the corners which were surveyed in its own frame, carried into the envelope's
// frame across the transform claims which relate the two, offset by the
// clearance it is required to keep, overlaid on the envelope, and measured. The
// accuracy of every claim any of those steps read is accumulated on the way
// through ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)), so that the
// clearance and its error bar are two halves of one answer rather than a number
// and a footnote.
//
// **The error budget is the point.** The georeference is one transform applied
// to every fact declared indoors, so its residual does not cancel between two
// indoor points and does not average away against an outdoor one. Combining it
// in quadrature with the random terms understates the answer, and understates
// it worst in exactly the case which motivates asking: a structure sited from
// interior geometry against an exterior boundary. So the budget adds the
// systematic terms linearly and counts each of them once however many inputs
// contributed it, which is [Budget]'s own arithmetic — and this query is where
// that arithmetic earns its keep, because a shared control point genuinely
// arrives from both sides of the comparison.
//
// Nothing here is written back to the model
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)). A fit
// is recomputed from the claims every time it is asked for, so it cannot
// disagree with a re-survey which has landed since somebody last asked.
//
// The zero Fit sited nothing, and every method below works on it.
type Fit struct {
	// declared is the frame the proposal was written in, which is the frame the
	// answer was carried out of. It is the envelope's frame where no carrying
	// was needed.
	declared ID

	// proposal is the thing being sited, expressed in the envelope's frame.
	proposal Region

	// envelope is the region it has to sit inside.
	envelope Region

	// needed is the proposal grown by the clearance it is required to keep,
	// which is the shape the envelope actually has to accommodate. It is the
	// proposal itself where nothing beyond fitting at all was required.
	needed Region

	// shared is what the proposal and the envelope have in common.
	shared Region

	// spill is what the proposal needs and the envelope does not offer, which
	// is where a refusal points.
	spill Region

	// required is the clearance the proposal was asked to keep, and clearance
	// what it actually has: positive where it sits inside the envelope,
	// negative where it does not.
	required  float64
	clearance float64

	// chord is the declared chord tolerance the curves of either outline were
	// drawn to on the way to the answer, and deviation how far the worst of
	// those segments falls from the curve it stands in for.
	//
	// One tolerance covers both, because both outlines are read from one survey
	// and a fit between two shapes drawn to different resolutions is a
	// comparison of two approximations nobody stated the relationship between.
	chord     Tolerance
	deviation float64

	// budget is the accumulated accuracy of every claim the answer was computed
	// from, on both sides and along the frame chain between them.
	budget Budget

	// verdict is what the clearance makes of the question once its own
	// uncertainty is taken into account.
	verdict Verdict
}

// Subject returns the id of the node which was sited.
func (f Fit) Subject() ID { return f.proposal.Subject() }

// Envelope returns the region the proposal had to sit inside, as the model
// holds it.
func (f Fit) Envelope() Region { return f.envelope }

// Proposal returns the thing which was sited, expressed in the envelope's
// frame.
//
// It is the carried region rather than the one which was read, because that is
// the one every number here was computed from. Handing back the region in its
// own frame would be handing back coordinates the answer is not in.
func (f Fit) Proposal() Region { return f.proposal }

// Needed returns the proposal grown by the clearance it was required to keep,
// which is the shape the envelope had to accommodate.
//
// It is the proposal itself where the required clearance was nought.
func (f Fit) Needed() Region { return f.needed }

// Shared returns what the proposal and the envelope have in common.
func (f Fit) Shared() Region { return f.shared }

// Spill returns what the proposal needs and the envelope does not offer.
//
// It covers nothing for a proposal which fits, and it is where a refusal points
// for one which does not: a fit answered only by "no" leaves somebody to work
// out which corner is over the line.
func (f Fit) Spill() Region { return f.spill }

// DeclaredIn returns the frame the proposal was written in.
func (f Fit) DeclaredIn() ID { return f.declared }

// Frame returns the frame the answer is expressed in, which is the envelope's.
func (f Fit) Frame() ID { return f.envelope.Frame() }

// Unit returns the linear unit of that frame, which every distance here is in
// and every area in the square of.
func (f Fit) Unit() Unit { return f.envelope.Unit() }

// ChordTolerance returns the declared tolerance the curves of either outline
// were drawn to on the way to the answer, and whether either of them bent at
// all.
//
// A fit is an overlay, so a curved outline has to become segments before there
// is a question to answer at all. That the answer says which tolerance is what
// separates a clearance measured against the wall from one measured against a
// chord which may sit a stated distance inside it.
func (f Fit) ChordTolerance() (Tolerance, bool) { return f.chord, f.chord.Name != "" }

// Deviation returns how far the worst segment of that drawing falls from the
// curve it stands in for, in [Fit.Unit].
//
// It is a bound on the outlines and not an accuracy of the answer, so it is
// reported beside the margin rather than folded into [Fit.Budget]: a chord which
// may lie ten millimetres inside the wall is a decision the caller made, and an
// uncertainty is what nobody decided.
func (f Fit) Deviation() float64 { return f.deviation }

// Carried reports whether the proposal had to be brought across a frame chain
// to be compared with the envelope.
//
// It is what says whether a georeference is in the budget at all: two things
// declared in one frame are compared without reading a transform claim, and the
// answer is then as good as the positions and no worse.
func (f Fit) Carried() bool { return f.declared != f.envelope.Frame() }

// Required returns the clearance the proposal was asked to keep inside the
// envelope, in [Fit.Unit].
func (f Fit) Required() float64 { return f.required }

// Clearance returns how much room there actually is, in [Fit.Unit].
//
// It is positive where the proposal sits wholly inside the envelope, and is
// then the shortest distance between the two boundaries — the tightest point,
// which is the only one worth reporting. It is negative where the proposal does
// not sit inside: how far the part which is outside reaches past the boundary
// where the two overlap, and how far apart they are where the proposal is not
// over the envelope at all.
//
// Both signs are the same quantity read the same way round — how much room
// there is, with a deficit written as a negative amount of it — so a caller
// which compares the number to a threshold does not have to case-split on which
// situation the model is in.
func (f Fit) Clearance() float64 { return f.clearance }

// Margin returns how much the clearance beats its requirement by, which is
// [Fit.Clearance] less [Fit.Required].
//
// It is the quantity the verdict is decided on, and it reduces to the clearance
// itself where nothing beyond fitting at all was required.
func (f Fit) Margin() float64 { return f.clearance - f.required }

// Budget returns the accumulated accuracy of everything the answer was computed
// from: the position claims behind both sets of corners, and the transform
// claims of every frame the proposal was carried through.
//
// One budget rather than one per side is the whole arrangement. A control point
// which put the interior corners where they are and also fitted the
// georeference is one error appearing three times, and two budgets added
// together would count it twice while looking exactly like the honest answer
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)).
func (f Fit) Budget() Budget { return f.budget }

// Uncertainty returns the budget reduced to one standard uncertainty, and what
// stopped it where it could not be.
//
// It is what the clearance is judged against, so a fit whose uncertainty cannot
// be computed is [VerdictUnknown] rather than a fit.
func (f Fit) Uncertainty() (Uncertainty, error) { return f.budget.Combined() }

// Verdict returns what the clearance makes of the question once its own
// uncertainty is taken into account.
func (f Fit) Verdict() Verdict { return f.verdict }

// String renders the fit as a person reads it.
func (f Fit) String() string {
	if f.envelope.Subject() == "" {
		return "nothing was sited"
	}

	written := fmt.Sprintf("%s in %s: %s, clearance %s%s",
		sitedName(f.proposal), sitedName(f.envelope), f.verdict,
		decimal(f.clearance), unitSuffix(f.Unit()),
	)

	if f.required != 0 {
		written += fmt.Sprintf(" of %s%s required", decimal(f.required), unitSuffix(f.Unit()))
	}

	combined, err := f.Uncertainty()
	if err != nil {
		return written + ", uncertainty unknown"
	}

	return written + ", known to " + combined.String()
}

// Report is the fit rendered for a person: the summary, and one line per term
// of the budget beneath it.
//
// The terms are the actionable half. "It might fit" is a finding somebody has
// to do something about, and what to do is decided by which term dominates —
// re-run the georeference, or re-survey the corners, or accept the risk.
//
// It is here rather than in the command so that a library caller reporting the
// answer and the command reporting it write the same thing.
func (f Fit) Report() string {
	var out strings.Builder

	out.WriteString(f.String())

	for _, term := range f.budget.Terms() {
		fmt.Fprintf(&out, "\n  %s %s: %s%s from %s",
			term.Kind, term.Name, decimal(term.Magnitude), unitSuffix(term.Unit),
			plural(len(term.Contributors), "claim"),
		)
		if term.Shared() {
			out.WriteString(", counted once")
		}
	}

	for _, claim := range f.budget.Unknown() {
		out.WriteString("\n  unstated accuracy: " + claimName(claim))
	}

	return out.String()
}

// FitWithin answers whether one node's area sits inside another's, across
// whatever frame chain relates the two, and says how well the answer is known.
//
// It is one composition of steps which exist separately, and the composition is
// the story: the envelope and the proposal are read with [Topology.RegionOf],
// the proposal is carried into the envelope's frame with [Region.In], grown by
// the required clearance with [Region.Buffer], overlaid on the envelope with
// [Region.Intersect] and [Region.Difference], and measured. Every one of those
// accumulates the accuracy of what it read, so the budget which comes out is
// the budget of the answer and not of any one step.
//
// **Everything the steps refuse, this refuses.** A ring which does not close, a
// tolerance the registry declares in the wrong unit, two frames with no measured
// chain between them, a clearance shorter than the distance two corners are one
// corner — each is the diagnostic the step which found it writes, pointing where
// the edit is. Nothing here re-words them, and nothing here works around one.
//
// A proposal and an envelope declared in one frame are compared without reading
// a transform at all, and the budget then holds no georeference term because
// none was applied. That is the difference the whole arrangement is about: a
// cross-frame answer costs what the georeference costs, and it says so.
//
// The two nodes may be the same node, which fits inside itself with a clearance
// of nought — an answer rather than a mistake, and one whose uncertainty is the
// accuracy of its own corners.
//
// Diagnostics are collected rather than stopped at, and a fit which was computed
// and cannot be decided comes back with [VerdictUnknown] and a warning saying
// why, rather than as a failure: "this could not be worked out" and "this was
// worked out and the model cannot tell" are different answers.
func (t *Topology) FitWithin(
	proposal, envelope *SemanticNode,
	boundaries *Boundaries,
	survey Survey,
	siting Siting,
) (Fit, []Diagnostic) {
	within, diags := t.RegionOf(envelope, boundaries, survey)

	sited, found := t.RegionOf(proposal, boundaries, survey)
	diags = append(diags, found...)

	for _, one := range []Region{within, sited} {
		if one.ready {
			continue
		}
		return Fit{}, append(diags, unsitable(one))
	}

	if siting.Clearance < 0 {
		return Fit{}, append(diags, overhanging(within, siting.Clearance))
	}

	carried := sited
	if sited.frame != within.frame {
		var refused []Diagnostic
		if carried, refused = sited.In(within.frame, siting.Frames); len(refused) > 0 {
			return Fit{}, append(diags, refused...)
		}
	}

	needed := carried
	if siting.Clearance > 0 {
		var refused []Diagnostic
		if needed, refused = carried.Buffer(siting.Clearance); len(refused) > 0 {
			return Fit{}, append(diags, refused...)
		}
	}

	spill, refused := needed.Difference(within)
	if len(refused) > 0 {
		return Fit{}, append(diags, refused...)
	}

	shared, refused := carried.Intersect(within)
	if len(refused) > 0 {
		return Fit{}, append(diags, refused...)
	}

	// What lies outside the envelope is asked of the proposal itself rather than
	// of the shape it needs around it, because the clearance reported is the
	// room there is and not the room there is once the requirement has already
	// been taken off it. Where nothing was required the two are the same overlay
	// and running it twice would be a second chance to round differently.
	beyond := spill
	if siting.Clearance > 0 {
		if beyond, refused = carried.Difference(within); len(refused) > 0 {
			return Fit{}, append(diags, refused...)
		}
	}

	result := Fit{
		declared:  sited.frame,
		proposal:  carried,
		envelope:  within,
		needed:    needed,
		shared:    shared,
		spill:     spill,
		required:  siting.Clearance,
		clearance: clearanceOf(carried, within, shared, beyond),
	}

	// The tolerance the outlines were drawn to, taken from whichever of them
	// bent, and the worst deviation either of them achieved. Both were read
	// from one survey, so a run which drew either drew both to the same name.
	if tolerance, drawn := within.ChordTolerance(); drawn {
		result.chord = tolerance
	} else if tolerance, drawn := sited.ChordTolerance(); drawn {
		result.chord = tolerance
	}
	result.deviation = math.Max(within.Deviation(), sited.Deviation())

	result.budget.Merge(carried.budget)
	result.budget.Merge(within.budget)

	verdict, reported, worth := result.decide()
	result.verdict = verdict
	if worth {
		diags = append(diags, reported)
	}

	return result, diags
}

// unsitable reports a region there is no figure to site or to site inside.
func unsitable(one Region) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Span:     one.span,
		Message: fmt.Sprintf(
			"expected two regions to decide a fit between, found that %s covers no area which could be sited",
			one.name(),
		),
		Hint: "a region is read from the loops bounding a node, against a tolerance the registry declares in the " +
			"unit of its frame; where that could not be done the reason is on a diagnostic of its own",
	}
}

// overhanging reports a required clearance written as a negative distance.
//
// It is refused rather than read as permission to overhang, because the two
// readings differ by the sign of a number nobody would look at twice: a
// requirement is a distance the proposal has to keep, and a proposal which is
// allowed over the line is one whose envelope is somewhere else.
func overhanging(within Region, clearance float64) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Span:     within.span,
		Message: fmt.Sprintf(
			"expected the clearance to keep inside %s to be a distance, found %s",
			within.name(), decimal(clearance)+unitSuffix(within.unit),
		),
		Hint: "a required clearance is room kept between a proposal and a boundary and is never room taken across " +
			"it; a proposal allowed to overhang is one whose envelope is a different region",
	}
}

// decide is what the clearance makes of the question once its own uncertainty
// is taken into account, together with whatever is worth saying about it.
//
// The comparison is against one standard uncertainty rather than a widened one.
// Widening is a presentation of a budget and belongs to whoever is presenting it
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)); what this decides is
// the narrowest honest question — whether the margin is bigger than the noise
// at all — and a caller wanting 95% confidence widens the figure
// [Fit.Uncertainty] hands back and compares it themselves.
func (f Fit) decide() (Verdict, Diagnostic, bool) {
	combined, err := f.Uncertainty()
	if err != nil {
		return VerdictUnknown, Diagnostic{
			Severity: SeverityWarning,
			Span:     f.proposal.span,
			Message: fmt.Sprintf(
				"whether %s fits inside %s cannot be decided: %s",
				sitedName(f.proposal), sitedName(f.envelope), err,
			),
			Hint: "a clearance is a fit only against the uncertainty of the clearance; the margin is still reported, " +
				"and what it is worth follows from the accuracy of the claims behind it",
		}, true
	}

	// A budget whose terms are all in one unit which is not the unit the shapes
	// are in combines perfectly well and cannot be compared with anything here.
	// Nothing converts ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)),
	// so the margin is reported and the verdict withheld.
	if combined.Unit != f.envelope.unit {
		return VerdictUnknown, Diagnostic{
			Severity: SeverityWarning,
			Span:     f.proposal.span,
			Message: fmt.Sprintf(
				"whether %s fits inside %s cannot be decided: the clearance is in %s and its uncertainty is %s",
				sitedName(f.proposal), sitedName(f.envelope), spellUnit(f.envelope.unit), combined,
			),
			Hint: "nothing here converts between units: a margin in metres judged against an uncertainty in " +
				"millimetres is out by a thousand, and the answer would look like a verdict either way",
		}, true
	}

	margin := f.Margin()

	switch {
	case margin > combined.Magnitude:
		return VerdictFits, Diagnostic{}, false
	case margin < -combined.Magnitude:
		return VerdictDoesNotFit, Diagnostic{}, false
	}

	// The one answer nobody reads off a number on its own, so it is said out
	// loud. A margin inside its own error bar is not a near miss to be rounded
	// either way: it is the model reporting that it was not measured well enough
	// to answer the question it was asked.
	return VerdictMightFit, Diagnostic{
		Severity: SeverityWarning,
		Span:     f.proposal.span,
		Message: fmt.Sprintf(
			"%s might fit inside %s: a margin of %s%s against an uncertainty of %s",
			sitedName(f.proposal), sitedName(f.envelope),
			decimal(margin), unitSuffix(f.envelope.unit), combined,
		),
		Hint: "the margin is inside the uncertainty of the answer, so the model cannot tell; re-measuring whatever " +
			"dominates the budget is what decides it",
	}, true
}

// sitedName is how a fit names one of its two sides.
//
// It is the node the region was read from, and it stays that way after the
// region has been carried into another frame and offset: a report which called
// the proposal "the region derived from plan:S-01" would be naming the
// arithmetic rather than the thing somebody is deciding about. A region which
// came from no node at all falls back to what [Region.name] calls it.
func sitedName(region Region) string {
	if subject := region.Subject(); subject != "" {
		return string(subject)
	}
	return region.name()
}

// clearanceOf is how much room a proposal has inside an envelope, signed so
// that a deficit is a negative amount of room.
//
// Three situations and one quantity. A proposal wholly inside has the distance
// from its boundary to the envelope's, taken at the tightest point, because a
// clearance which reported the average or the loosest point would pass a
// structure which touches at one corner. A proposal partly outside has how far
// the part outside reaches past the boundary, which is what it would have to be
// brought in by. A proposal nowhere near has how far away it is, because there
// is no crossing to measure the depth of and its distance is what somebody has
// to close.
func clearanceOf(proposal, envelope, shared, beyond Region) float64 {
	switch {
	case beyond.Empty():
		return proposal.gap(envelope)
	case shared.Empty():
		return -proposal.gap(envelope)
	default:
		return -envelope.overrun(beyond)
	}
}

// gap is the shortest distance between two regions' boundaries, in the plane
// they share.
//
// Every pair of segments is compared, which is what catches two shapes whose
// nearest approach is a corner against the middle of a wall as well as two
// running parallel. It is the same comparison [Region.meets] makes and is
// computed the same way, so a fit which reports a clearance of nought and a
// containment which reports touching cannot disagree.
//
// A region with no ring is a distance to nothing, which is nought rather than
// an infinity a caller would have to test for before printing it.
func (r Region) gap(other Region) float64 {
	mine := r.figure(r.basis)
	theirs := other.figure(r.basis)

	if len(mine) == 0 || len(theirs) == 0 {
		return 0
	}

	nearest := math.Inf(1)

	for _, ring := range mine {
		for i, from := range ring {
			one := segment{a: from, b: ring[(i+1)%len(ring)]}

			for _, against := range theirs {
				for j, start := range against {
					two := segment{a: start, b: against[(j+1)%len(against)]}
					nearest = math.Min(nearest, apart(one, two))
				}
			}
		}
	}

	return nearest
}

// overrun is how far the deepest corner of a region lies from this region's
// boundary.
//
// It is asked of the part of a proposal which is outside the envelope, where it
// is the depth of the incursion: the corners of that part which lie on the
// envelope's own boundary are nought away from it, and the ones which came from
// the proposal are as far past it as the proposal reaches. The deepest is the
// distance the proposal has to be brought in by, and taking the deepest rather
// than the average is what stops a structure a metre over the line at one corner
// reading as a comfortable few centimetres.
func (r Region) overrun(outside Region) float64 {
	_, depth := r.deepest(outside)
	return depth
}

// deepest is the corner of a region which lies furthest from this region's
// boundary, and how far from it that corner lies.
//
// It is [Region.overrun] with the place kept as well as the distance, for a
// report which has to say where a thing reaches past a boundary and not only by
// how much. The two are one walk rather than two, so a report naming a corner
// and a figure cannot name a corner the figure was not measured at.
func (r Region) deepest(outside Region) (Point, float64) {
	boundary := r.figure(r.basis)
	if len(boundary) == 0 {
		return Point{}, 0
	}

	var (
		at      Point
		deepest float64
	)

	for _, piece := range outside.pieces {
		for _, ring := range append([][]Point{piece.outer}, piece.holes...) {
			for _, point := range ring {
				if nearest := nearestTo(r.basis.project(point), boundary); nearest > deepest {
					at, deepest = point, nearest
				}
			}
		}
	}

	return at, deepest
}
