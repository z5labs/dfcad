// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"cmp"
	"fmt"
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

	// reason is which step of the rule produced this outcome. It is empty on
	// the zero Resolution, which [Resolution.Reason] reports as
	// [ReasonUnclaimed].
	reason Reason
}

// Reason is which step of the resolution rule produced an outcome.
//
// It exists because an answer which cannot say why it is the answer is a number
// again. "24.2 m²" and "24.2 m², because it is the most accurate of three
// claims" are different things to act on: the first invites a re-measurement
// nobody needed, and the second says which claim to go and read.
//
// The set is closed and mirrors [Claims.Resolve]'s rule step for step, so a
// caller branching on it is branching on the rule rather than on a message.
type Reason string

// The reasons, one per step of the rule.
const (
	// ReasonUnclaimed is a subject with no live claim under the predicate at
	// all. It is what the zero [Resolution] reports, and it is not the same as
	// an unranked one: nothing was said, rather than nothing rankable.
	ReasonUnclaimed Reason = "unclaimed"

	// ReasonOnly is the one live claim under the predicate, which wins by there
	// being nothing to compare it against.
	ReasonOnly Reason = "only"

	// ReasonAccuracy is a claim whose accuracy is smaller than that of every
	// claim it could be compared against, which is the criterion.
	ReasonAccuracy Reason = "accuracy"

	// ReasonRecency is a claim which tied on accuracy and carried the later
	// date. Recency is the tiebreaker and never the criterion, which is why it
	// is a reason of its own rather than folded into the one above.
	ReasonRecency Reason = "recency"

	// ReasonUnranked is the one live claim under a predicate nothing rankable
	// was said about. It is still what the model says and is still the answer;
	// what it is not is an answer the rule chose.
	ReasonUnranked Reason = "unranked"

	// ReasonAmbiguous is more than one claim the rule cannot separate, whether
	// because they are equally accurate and equally recent, because their
	// accuracies are in units nothing converts between, or because nothing
	// rankable was said about any of them.
	ReasonAmbiguous Reason = "ambiguous"
)

// decided reports whether the reason is one which picked a claim.
func (r Reason) decided() bool {
	switch r {
	case ReasonOnly, ReasonAccuracy, ReasonRecency:
		return true
	}
	return false
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

// Reason returns which step of the rule produced this outcome.
//
// The zero [Resolution] reports [ReasonUnclaimed], which is what a subject
// nothing has been claimed about resolves to. Every other reason is written by
// the step which produced it, so the reason and the outcome cannot drift apart:
// [Resolution.Resolved] is true exactly for the three reasons which picked a
// claim, and [Resolution.Ambiguous] is true exactly for [ReasonAmbiguous].
func (r Resolution) Reason() Reason {
	if r.reason == "" {
		return ReasonUnclaimed
	}
	return r.reason
}

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

	candidates, reason := narrow(live)
	slices.SortStableFunc(candidates, compareClaims)

	resolution.candidates = candidates
	resolution.reason = reason
	if reason.decided() && len(candidates) == 1 {
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
// reports which step of the rule got it there.
//
// The reason is produced here rather than reconstructed by a caller comparing
// the candidates afterwards, because reconstructing it means re-implementing the
// rule: which of two equally accurate claims is more recent is the same question
// this function already answered, and a second answer to it is free to disagree
// with the first.
//
// A reason of [ReasonUnranked] or [ReasonAmbiguous] is the case where no claim
// won: every claim which could still be it comes back, and none of them is the
// answer. That is deliberate — an unrankable claim is still what the model says,
// and hiding it would leave a caller unable to tell a subject with an unmeasured
// claim from one with no claim at all.
func narrow(live []*Claim) (candidates []*Claim, reason Reason) {
	var rankable []ranking
	for _, claim := range live {
		// One claim is one budget. Ranking by the same arithmetic which
		// accumulates a derived answer's budget is what keeps the two from
		// drifting apart: a claim unrankable here is one whose accuracy could
		// not be combined there either, for the same reason.
		var budget Budget
		budget.Add(claim)

		combined, err := budget.Combined()
		if err != nil {
			continue
		}

		rankable = append(rankable, ranking{
			claim:     claim,
			magnitude: combined.Magnitude,
			unit:      combined.Unit,
		})
	}

	if len(rankable) == 0 {
		// One unrankable claim standing alone is not ambiguity. There is
		// nothing to be ambiguous between, and the answer is simply the one
		// thing anybody said.
		if len(live) == 1 {
			return slices.Clone(live), ReasonUnranked
		}
		return slices.Clone(live), ReasonAmbiguous
	}

	unbeaten := smallest(rankable)

	// Magnitudes in different units were never compared against each other, so
	// recency does not get to decide between them. Accuracy comes first, and a
	// tiebreak applied where the criterion could not be is that ordering
	// inverted by an implementation detail.
	if mixedUnits(unbeaten) {
		return claimsOf(unbeaten), ReasonAmbiguous
	}

	recent := mostRecent(unbeaten)
	if len(recent) > 1 {
		return claimsOf(recent), ReasonAmbiguous
	}

	switch {
	case len(live) == 1:
		// Nothing to compare it against, so no step of the rule chose it.
		return claimsOf(recent), ReasonOnly
	case len(unbeaten) > 1:
		// Accuracy left more than one standing, so the date is what separated
		// them.
		return claimsOf(recent), ReasonRecency
	default:
		return claimsOf(recent), ReasonAccuracy
	}
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
