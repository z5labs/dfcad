// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"cmp"
	"fmt"
	"math"
	"slices"
)

// Resolution is the answer to "which claim about this thing is current", and
// the evidence for that answer.
//
// A resolution is a value rather than a claim because there are three outcomes
// and only one of them is a claim. One claim can win outright; several can be
// equally current, which is ambiguity and is reported rather than broken by
// picking one; and a subject can have nothing rankable said about it, which is
// an unranked answer rather than an absent one. The zero Resolution is what a
// subject nothing has been claimed about resolves to, and every method below
// works on it.
type Resolution struct {
	// subject is the thing the question was asked about.
	subject ID

	// predicate is the predicate it was asked under.
	predicate string

	// claim is the claim which won, and is nil when none did.
	claim *Claim

	// candidates are the claims which could still be the answer: the winner
	// alone where there is one, every tied claim where the rule could not
	// separate them, and every live claim where nothing rankable was said.
	//
	// They are held in source order — file, then position within it — which is
	// a property of where they were written rather than of the order they were
	// read.
	candidates []*Claim
}

// Subject returns the id of the thing the resolution is about.
func (r Resolution) Subject() ID { return r.subject }

// Predicate returns the predicate the resolution was asked under.
func (r Resolution) Predicate() string { return r.predicate }

// Claim returns the claim which won, and whether one did.
//
// The claim itself is the answer rather than only its value, because the point
// of the model is that a number arrives with the evidence for it: what won and
// why is one lookup, and [Resolution.ClaimID] is the short form of it.
func (r Resolution) Claim() (*Claim, bool) { return r.claim, r.claim != nil }

// ClaimID returns the id of the claim the answer came from, and whether the
// winning claim wrote one.
//
// A claim id is optional, so a resolved answer can be traced back to a claim
// which has no name of its own. That is what the second result distinguishes,
// and [Resolution.Claim] then carries the span which names it instead.
func (r Resolution) ClaimID() (ID, bool) {
	if r.claim == nil {
		return "", false
	}
	return r.claim.ID()
}

// Value returns the value of the claim which won, and whether one did.
func (r Resolution) Value() (Value, bool) {
	if r.claim == nil {
		return Value{}, false
	}
	return r.claim.Value(), true
}

// Resolved reports whether one claim won outright.
func (r Resolution) Resolved() bool { return r.claim != nil }

// Ambiguous reports whether more than one claim is equally current.
//
// Ambiguity is a state of the claims and not a failure of the engine: two
// measurements of one thing, equally good and equally recent, genuinely do not
// decide between themselves, and the honest answer is both of them. A predicate
// the registry declares strict escalates it to an [AmbiguousResolutionError]
// instead, because for some quantities no answer is safer than an arbitrary one.
//
// One unrankable claim standing alone is not ambiguity. There is nothing to be
// ambiguous between; the answer is simply unranked, which [Resolution.Resolved]
// reports and [Resolution.Candidates] names.
func (r Resolution) Ambiguous() bool { return r.claim == nil && len(r.candidates) > 1 }

// Candidates returns the claims which could still be the answer, in source
// order.
//
// An ambiguous resolution returns every tied claim rather than an arbitrary
// pick, because narrowing four claims to two is most of the work of deciding
// between them and a caller which is shown one of the two cannot tell that the
// other exists.
//
// The claims are a copy of the resolution's own slice, so re-ordering them
// re-orders nothing in the model.
func (r Resolution) Candidates() []*Claim { return slices.Clone(r.candidates) }

// AmbiguousResolutionError reports that a predicate the registry declares
// strict resolved to more than one equally current claim.
//
// It carries the tied claims rather than a count of them because the caller's
// next move is to say which ones, where they were written and what they say —
// a message would have to be parsed back apart to do any of that.
type AmbiguousResolutionError struct {
	// Subject is the thing the question was asked about.
	Subject ID

	// Predicate is the strict predicate it was asked under.
	Predicate string

	// Candidates are the tied claims, in source order.
	Candidates []*Claim
}

// Error implements the [error] interface.
func (e AmbiguousResolutionError) Error() string {
	return fmt.Sprintf(
		"expected one current %s of %s, found %s equally current: %s is declared strict",
		e.Predicate, e.Subject, count(len(e.Candidates)), e.Predicate,
	)
}

// Resolve decides which claim about one subject under one predicate is
// current, per decision record
// [0007](docs/decisions/0007-rank-is-closed.md).
//
// The rule is stated once, here, and is the whole of it:
//
//   - A deprecated claim is not considered. It is retracted rather than
//     out-ranked, and resolution never sees it.
//   - The smallest accuracy wins. A claim with no accuracy, or one whose terms
//     are not in one unit, is unrankable and cannot win.
//   - Where accuracies tie, the most recent date wins. Recency is the
//     tiebreaker and never the criterion: a dimension scaled off a plan last
//     month does not beat a survey shot taken last year.
//   - Where neither separates the candidates, the answer is ambiguous, and
//     every tied claim comes back.
//   - Where nothing rankable was said, every live claim comes back as a
//     candidate and nothing wins.
//
// Nothing else takes part. There is no preferred rank, no numeric priority and
// no override, so two runs over the same claims give the same answer whichever
// file each was read from and in whatever order the walk reached them. Nothing
// in this path iterates a map, and the answer is a function of what the claims
// carry rather than of how they were loaded.
//
// registry decides one thing: whether the predicate is declared strict, which
// escalates an ambiguous resolution from a report to an
// [AmbiguousResolutionError]. The resolution comes back beside the error
// whatever it says, because a caller reporting the failure wants the tied
// claims and not just the fact that there were some. A nil registry declares
// nothing strict.
//
// Comparison of magnitudes is exact. Two accuracies which differ in the last
// bit are two different accuracies, and an epsilon which called them equal would
// be a tolerance — which is registry data the consuming repository owns
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)), not a constant
// hidden here.
func (c *Claims) Resolve(subject ID, predicate string, registry *Registry) (Resolution, error) {
	resolution := c.resolve(subject, predicate)

	if resolution.Ambiguous() && isStrict(registry, predicate) {
		return resolution, AmbiguousResolutionError{
			Subject:    subject,
			Predicate:  predicate,
			Candidates: resolution.Candidates(),
		}
	}

	return resolution, nil
}

// resolve applies the rule and reports what it found.
//
// It is the whole of resolution apart from what the registry says, which is why
// it cannot fail: an ambiguity is a state of the claims, and only a predicate
// declared strict turns that state into an error. The conflict register asks
// for the state and never for the error, so it calls this rather than
// [Claims.Resolve] with a discarded one.
func (c *Claims) resolve(subject ID, predicate string) Resolution {
	var live []*Claim
	for claim := range c.Under(subject, predicate) {
		if claim.Rank() == RankDeprecated {
			continue
		}
		live = append(live, claim)
	}

	return resolutionOf(subject, predicate, live)
}

// resolutionOf applies the rule to the live claims of one pair, which the
// caller has already separated from the deprecated ones.
//
// It is split from [Claims.resolve] for the conflict register, which has the
// live claims of every pair in hand by the time it wants a resolution of each.
// Asking resolve for them again would re-read the claims of a subject once per
// predicate written on it, which is a scan the walk has already done.
func resolutionOf(subject ID, predicate string, live []*Claim) Resolution {
	resolution := Resolution{subject: subject, predicate: predicate}

	if len(live) == 0 {
		return resolution
	}

	candidates, ranked := narrow(live)
	slices.SortStableFunc(candidates, compareClaims)

	resolution.candidates = candidates
	if ranked && len(candidates) == 1 {
		resolution.claim = candidates[0]
	}

	return resolution
}

// ranking is one live claim together with the figure it is ranked by.
type ranking struct {
	// claim is the claim being ranked.
	claim *Claim

	// magnitude is its accuracy combined into one figure.
	magnitude float64

	// unit is the unit that figure is expressed in. Two magnitudes in different
	// units are not comparable, because nothing here converts.
	unit Unit
}

// narrow reduces the live claims to those which could still be the answer, and
// reports whether it ranked them.
//
// A false second result is the case where nothing could be ranked: every live
// claim comes back, and none of them wins. That is deliberate — an unrankable
// claim is still what the model says, and hiding it would leave a caller unable
// to tell a subject with an unmeasured claim from one with no claim at all.
func narrow(live []*Claim) (candidates []*Claim, ranked bool) {
	var rankable []ranking
	for _, claim := range live {
		accuracy, ok := claim.Accuracy()
		if !ok {
			continue
		}

		magnitude, unit, ok := combined(accuracy)
		if !ok {
			continue
		}

		rankable = append(rankable, ranking{claim: claim, magnitude: magnitude, unit: unit})
	}

	if len(rankable) == 0 {
		return slices.Clone(live), false
	}

	unbeaten := smallest(rankable)

	// Magnitudes in different units were never compared against each other, so
	// recency does not get to decide between them. Accuracy comes first, and a
	// tiebreak applied where the criterion could not be is that ordering
	// inverted by an implementation detail.
	if mixedUnits(unbeaten) {
		return claimsOf(unbeaten), true
	}

	return claimsOf(mostRecent(unbeaten)), true
}

// smallest returns the rankings nothing comparable beats.
//
// It is a maximal set rather than a minimum because two magnitudes in different
// units do not compare: each is the smallest of what it can be measured
// against, and both survive. Where every ranking shares one unit — which is the
// ordinary case, since the predicate declares the unit its values are written
// in — this is exactly the claims holding the minimum.
func smallest(rankable []ranking) []ranking {
	var unbeaten []ranking
	for _, candidate := range rankable {
		beaten := false
		for _, other := range rankable {
			if other.unit == candidate.unit && other.magnitude < candidate.magnitude {
				beaten = true
				break
			}
		}
		if !beaten {
			unbeaten = append(unbeaten, candidate)
		}
	}
	return unbeaten
}

// mixedUnits reports whether the rankings are expressed in more than one unit.
func mixedUnits(rankings []ranking) bool {
	for _, r := range rankings[1:] {
		if r.unit != rankings[0].unit {
			return true
		}
	}
	return false
}

// mostRecent returns the rankings whose claims carry the latest date.
func mostRecent(rankings []ranking) []ranking {
	latest := rankings[0].claim.Date()
	for _, r := range rankings[1:] {
		if r.claim.Date().After(latest) {
			latest = r.claim.Date()
		}
	}

	var out []ranking
	for _, r := range rankings {
		if r.claim.Date().Equal(latest) {
			out = append(out, r)
		}
	}
	return out
}

// claimsOf is the claims of a set of rankings, in the order the rankings are in.
func claimsOf(rankings []ranking) []*Claim {
	out := make([]*Claim, 0, len(rankings))
	for _, r := range rankings {
		out = append(out, r.claim)
	}
	return out
}

// combined reduces an accuracy to the one figure resolution ranks by, the unit
// it is expressed in, and whether it could be reduced at all.
//
// The terms combine the way specification section 6.6.5 says they do:
// independent terms in quadrature, systematic terms linearly, and the two
// together in quadrature. Every magnitude is already one standard deviation
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)), so the result is too.
//
// Terms written in more than one unit do not combine. Nothing here converts
// between units ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)), and
// adding a figure in millimetres to one in metres to get a rank would be the
// silent conversion the rest of the engine refuses — so the claim is unrankable
// instead, which is a state the caller can see.
func combined(accuracy Accuracy) (float64, Unit, bool) {
	if len(accuracy.Terms) == 0 {
		return 0, "", false
	}

	unit := accuracy.Terms[0].Unit

	// squares is the independent terms in quadrature; shared is the systematic
	// ones, which add linearly because they are the same error appearing twice
	// rather than two errors which might cancel.
	var squares, shared float64

	for _, term := range accuracy.Terms {
		if term.Unit != unit {
			return 0, "", false
		}

		magnitude := math.Abs(term.Magnitude)
		if math.IsNaN(magnitude) || math.IsInf(magnitude, 0) {
			return 0, "", false
		}

		switch term.Kind {
		case TermIndependent:
			squares += magnitude * magnitude
		case TermSystematic:
			shared += magnitude
		default:
			return 0, "", false
		}
	}

	return math.Sqrt(squares + shared*shared), unit, true
}

// isStrict reports whether the registry declares the predicate strict.
func isStrict(registry *Registry, predicate string) bool {
	declared, ok := registry.Predicate(predicate)
	return ok && declared.Strict
}

// compareClaims orders two claims by where they were written, and then by their
// own ids.
//
// Ordering candidates by their source position rather than by the order they
// were loaded is what makes the result of a resolution a value: shuffling the
// files, the walk or the claims of one subject leaves the same claims in the
// same order, so two runs produce output which diffs to nothing.
func compareClaims(a, b *Claim) int {
	return cmp.Or(
		comparePositions(a.Span().Start, b.Span().Start),
		comparePositions(a.Span().End, b.Span().End),
		cmp.Compare(a.id, b.id),
	)
}
