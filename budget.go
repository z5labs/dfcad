// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

// ErrEmptyBudget reports that a budget holds no term, and so has no figure to
// combine.
//
// It is not the same as an uncertainty of zero. A computation which read no
// claim has no measured input, and answering it with an exact zero would be
// asserting that its answer is perfect — which is the optimism the whole of
// this file exists to refuse.
var ErrEmptyBudget = errors.New("expected at least one accuracy term in the budget, found none")

// UnknownAccuracyError reports that a budget cannot be combined because one or
// more of the claims it accumulated states no accuracy.
//
// An unstated accuracy is unknown and not zero. Treating it as zero would let a
// claim nobody measured pass silently through a clearance computation and come
// out the far side looking like the most accurate input it had, so an unknown
// term taints the budget instead: no combined figure comes out of it at all
// until the claim it came from says how well it is known
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)).
//
// The claims are carried rather than counted because the caller's next move is
// to say which ones and where they were written, and a message would have to be
// taken apart again to do that.
type UnknownAccuracyError struct {
	// Claims are the claims which carried no usable accuracy, in the order they
	// were accumulated.
	Claims []*Claim
}

// Error implements the [error] interface.
func (e UnknownAccuracyError) Error() string {
	return fmt.Sprintf(
		"expected an accuracy on every claim the answer was computed from, found %s with none: an unstated accuracy is unknown rather than zero",
		count(len(e.Claims)),
	)
}

// MixedUnitsError reports that a budget's terms are not all expressed in one
// unit.
//
// Nothing here converts between units
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)). Adding a figure in
// millimetres to one in metres to get a budget would be exactly the silent
// conversion the rest of the engine refuses, so a budget whose terms disagree
// combines to nothing and says which units it was asked to reconcile.
type MixedUnitsError struct {
	// Units are the units the terms were written in, each once, in the order
	// they were accumulated.
	Units []Unit
}

// Error implements the [error] interface.
func (e MixedUnitsError) Error() string {
	return fmt.Sprintf(
		"expected every term of the budget in one unit, found %s: nothing converts between them",
		join(spellings(e.Units), "and"),
	)
}

// CoverageFactorError reports a coverage factor an uncertainty cannot be stated
// at.
//
// A coverage factor multiplies a standard uncertainty, so it has to be a finite
// number greater than zero. Zero would report every answer as exact and a
// negative one would report a width as a deficit.
type CoverageFactorError struct {
	// Factor is the factor that was asked for.
	Factor float64
}

// Error implements the [error] interface.
func (e CoverageFactorError) Error() string {
	return fmt.Sprintf(
		"expected a coverage factor greater than zero, found %s",
		decimal(e.Factor),
	)
}

// Uncertainty is one combined figure together with the coverage it is stated
// at.
//
// The coverage factor travels with the figure rather than beside it because a
// width with no factor attached is the ambiguity decision record
// [0006](docs/decisions/0006-accuracy-is-one-sigma.md) exists to remove: a
// half-width bound, a 1σ uncertainty and a 95% interval differ by a factor of
// two, and nothing in the bare number tells them apart. [Uncertainty.String]
// therefore always writes the factor, and there is no rendering here which
// leaves it out.
//
// Storage is always 1σ. An [Uncertainty] whose CoverageFactor is not one is a
// presentation of a budget and is never written back into a model
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
type Uncertainty struct {
	// Magnitude is the combined figure, at CoverageFactor standard deviations.
	Magnitude float64

	// Unit is the unit the figure is expressed in, which is the unit every term
	// of the budget was written in.
	Unit Unit

	// CoverageFactor is k: how many standard uncertainties Magnitude is. It is
	// one for the figure [Budget.Combined] produces, which is the storage
	// convention.
	CoverageFactor float64
}

// Standard returns the figure at one standard deviation, whatever coverage this
// one is stated at.
//
// It is what widening is computed from, so that widening an already widened
// figure states the coverage asked for rather than compounding two factors.
func (u Uncertainty) Standard() float64 {
	if !(u.CoverageFactor > 0) {
		return u.Magnitude
	}
	return u.Magnitude / u.CoverageFactor
}

// Widen returns the same uncertainty stated at the given coverage factor.
//
// It is computed from [Uncertainty.Standard] rather than from Magnitude, so
// widening a figure already stated at k = 2 to k = 3 gives three standard
// uncertainties and not six. A factor which is not a finite number greater than
// zero is a [CoverageFactorError] rather than a figure nobody can act on.
func (u Uncertainty) Widen(factor float64) (Uncertainty, error) {
	if !(factor > 0) || math.IsInf(factor, 0) {
		return Uncertainty{}, CoverageFactorError{Factor: factor}
	}

	return Uncertainty{
		Magnitude:      u.Standard() * factor,
		Unit:           u.Unit,
		CoverageFactor: factor,
	}, nil
}

// Confidence returns the approximate confidence of the coverage factor for a
// normal distribution, and whether the factor is one of the three which has a
// customary spelling.
//
// Only the customary factors are spelled. Reporting "≈ 86.6%" for k = 1.5 would
// be arithmetic on an assumption about the distribution that nothing in the
// model states, and the factor itself is the honest thing to print.
func (u Uncertainty) Confidence() (string, bool) {
	switch u.CoverageFactor {
	case 1:
		return "≈ 68%", true
	case 2:
		return "≈ 95%", true
	case 3:
		return "≈ 99.7%", true
	}
	return "", false
}

// String writes the figure with its unit and its coverage factor.
//
// The factor is never left out, which is what makes this safe to print
// anywhere: a widened number without its factor attached is a number which
// means whatever the reader assumed.
func (u Uncertainty) String() string {
	var out strings.Builder

	out.WriteString(decimal(u.Magnitude))
	if u.Unit != "" {
		out.WriteString(" ")
		out.WriteString(string(u.Unit))
	}

	out.WriteString(" (k = ")
	out.WriteString(decimal(u.CoverageFactor))
	if confidence, spelled := u.Confidence(); spelled {
		out.WriteString(", ")
		out.WriteString(confidence)
	}
	out.WriteString(")")

	return out.String()
}

// BudgetTerm is one term of an accumulated budget: an [AccuracyTerm] together
// with what it is called and which claims put it there.
//
// The attribution is the reason a budget is a list of terms rather than a
// number. "±0.06 m" is an answer nobody can act on; "the control point is 80%
// of it, and it came from this claim" says what to re-measure.
type BudgetTerm struct {
	// Kind is which of the two kinds of error this term is, and so how it
	// combines: independent terms in quadrature, systematic terms linearly.
	Kind TermKind

	// Name is what to call the term in a report.
	//
	// For a systematic term it is the term id, which is the thing the error is
	// shared with. For an independent term there is no id in the format — the
	// error belongs to the one measurement — so it is the name of the claim it
	// came from.
	Name string

	// Magnitude is the one-sigma figure, in Unit, exactly as the claim wrote it.
	// Combining takes its absolute value; nothing here rewrites what was
	// written.
	//
	// Where one systematic term was contributed with two magnitudes, this is the
	// larger of them. A budget which cannot tell which fit of a shared error is
	// the right one reports the wider, because a check which fails on a wide
	// budget prompts an investigation and one which passes on a narrow one does
	// not.
	Magnitude float64

	// Unit is the unit the magnitude is expressed in, as it was written.
	Unit Unit

	// Source is the id the systematic error is shared with, and is empty for an
	// independent term.
	Source ID

	// Contributors are the claims which carried this term, in the order they
	// were accumulated.
	//
	// A systematic term shared by several inputs of one computation is one term
	// with several contributors, which is what "counted once" looks like from
	// the reporting side: the magnitude appears in the arithmetic once, and
	// every claim which put it there is still named. Each is named once, so one
	// claim which wrote the same term id twice does not read as two
	// measurements sharing it.
	Contributors []*Claim
}

// Shared reports whether more than one claim contributed this term.
func (t BudgetTerm) Shared() bool { return len(t.Contributors) > 1 }

// String writes the term for a person: which kind of error it is, how big it is,
// what it is shared with, and how many inputs put it there.
//
// The count of contributors is written for a shared term and left out for a term
// one claim carried, because it is the whole of what "counted once" looks like
// from the reporting side: a figure which appears in the arithmetic once and in
// three of the inputs is the case a budget is easiest to get wrong about, and it
// should be readable without going to the machine payload for it.
func (t BudgetTerm) String() string {
	var out strings.Builder

	out.WriteString(string(t.Kind))
	out.WriteString(" ")
	out.WriteString(decimal(t.Magnitude))
	if t.Unit != "" {
		out.WriteString(" ")
		out.WriteString(string(t.Unit))
	}

	if t.Source != "" {
		out.WriteString(" shared with ")
		out.WriteString(string(t.Source))
	}

	if t.Shared() {
		fmt.Fprintf(&out, ", counted once over %d claims", len(t.Contributors))
	}

	return out.String()
}

// clone is the term with contributors of its own, which is what a reader hands
// out: a term whose slice is the budget's own would let an append reach back
// into the budget it came from.
func (t BudgetTerm) clone() BudgetTerm {
	t.Contributors = slices.Clone(t.Contributors)
	return t
}

// Budget is the accumulated uncertainty of one computed answer, broken out by
// contributing term.
//
// A derived quantity — the distance between two surveyed corners, the clearance
// between a wall face and a setback line — is only as trustworthy as the
// arithmetic which combined the uncertainties of its inputs, and two things make
// that arithmetic go wrong silently
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)):
//
//   - Combining everything in quadrature is right only for terms which are
//     independent. A term shared between the inputs does not partially cancel
//     and does not average away, so it adds linearly.
//   - A shared term counted once per input inflates the budget instead, and the
//     usual response to that is to widen a tolerance rather than to fix the
//     arithmetic.
//
// So a budget accumulates claims rather than numbers. Two systematic terms are
// the same term when they name the same term id — not when their magnitudes
// happen to match — and the same term reached through two inputs is counted
// once. Adding one claim twice adds its terms once, for the same reason: one
// measurement is one error however many times a computation reads it.
//
// The zero Budget holds nothing and every method below works on it. A budget is
// accumulated with [Budget.Add] and then read; the readers copy what they
// return, so nothing a caller does to the result reaches back into the budget.
type Budget struct {
	// terms are the accumulated terms, in the order they were first
	// contributed. The order is a property of the walk which accumulated them,
	// which is itself deterministic, and nothing here sorts or indexes by a map.
	terms []BudgetTerm

	// inputs is every claim accumulated, each once, in the order they arrived.
	// It is what makes [Budget.Add] idempotent per claim.
	inputs []*Claim

	// unknown are the accumulated claims which stated no usable accuracy. One of
	// them taints the whole budget.
	unknown []*Claim
}

// Add accumulates the accuracy of each claim into the budget.
//
// A nil claim is not an input and contributes nothing. A claim already
// accumulated contributes nothing further: it is one measurement, and reading
// it twice does not make its error two errors.
//
// A claim which states no accuracy — or one whose accuracy the loader could
// only read in part, leaving a magnitude which is not a finite number or a term
// of no known kind — is accumulated as unknown, and taints the budget until it
// is fixed.
func (b *Budget) Add(claims ...*Claim) {
	for _, claim := range claims {
		if claim == nil || slices.ContainsFunc(b.inputs, func(in *Claim) bool { return sameClaim(in, claim) }) {
			continue
		}
		b.inputs = append(b.inputs, claim)

		accuracy, stated := claim.Accuracy()
		if !stated || !usable(accuracy) {
			b.unknown = append(b.unknown, claim)
			continue
		}

		for _, term := range accuracy.Terms {
			b.contribute(claim, term)
		}
	}
}

// Merge accumulates every claim another budget was built from.
//
// It is how a computation over computations keeps a shared term counted once:
// re-accumulating the claims rather than adding the two combined figures is
// what lets the same control point reached through two derived answers meet
// itself in one budget.
func (b *Budget) Merge(other Budget) { b.Add(other.inputs...) }

// contribute adds one written term to the budget, merging it with the term it
// is the same as where there is one.
//
// Sameness is the term id, and only for a systematic term: two independent
// terms of the same magnitude are two different errors which happen to be
// equally large, and folding them together would understate the answer.
//
// The unit takes part in the match so that one term id written in two units
// stays two terms. It is not a conversion this could do — nothing here converts
// — and merging them would swallow the disagreement that [Budget.Combined]
// exists to report.
func (b *Budget) contribute(claim *Claim, term AccuracyTerm) {
	if term.Kind == TermSystematic && term.Source != "" {
		for i := range b.terms {
			same := b.terms[i].Kind == TermSystematic &&
				b.terms[i].Source == term.Source &&
				b.terms[i].Unit == term.Unit
			if !same {
				continue
			}

			// One claim which wrote the same term id twice is still one claim
			// contributing it. Recording it twice would make the term read as
			// shared between two measurements when only one carried it.
			contributors := b.terms[i].Contributors
			if !slices.ContainsFunc(contributors, func(in *Claim) bool { return sameClaim(in, claim) }) {
				b.terms[i].Contributors = append(contributors, claim)
			}

			if math.Abs(term.Magnitude) > math.Abs(b.terms[i].Magnitude) {
				b.terms[i].Magnitude = term.Magnitude
			}
			return
		}
	}

	b.terms = append(b.terms, BudgetTerm{
		Kind:         term.Kind,
		Name:         termName(claim, term),
		Magnitude:    term.Magnitude,
		Unit:         term.Unit,
		Source:       term.Source,
		Contributors: []*Claim{claim},
	})
}

// Terms returns the accumulated terms, in the order they were first
// contributed.
//
// The terms are a copy, down to the claims attributed to each, so neither
// re-ordering them nor appending to one's contributors reaches back into the
// budget.
func (b Budget) Terms() []BudgetTerm {
	out := make([]BudgetTerm, 0, len(b.terms))
	for _, term := range b.terms {
		out = append(out, term.clone())
	}
	return out
}

// Unknown returns the accumulated claims which stated no usable accuracy, in
// the order they arrived.
//
// It is empty for a budget every input of which said how well it is known,
// which is what [Budget.Known] reports.
func (b Budget) Unknown() []*Claim { return slices.Clone(b.unknown) }

// Known reports whether every claim the budget accumulated stated an accuracy.
//
// A false answer is the taint: the budget still holds and reports the terms it
// did read, and [Budget.Combined] refuses to reduce them to a figure, because a
// figure computed from some of the inputs would be narrower than the truth
// while looking exactly like it.
func (b Budget) Known() bool { return len(b.unknown) == 0 }

// Dominant returns the term contributing most to the combined figure, and
// whether the budget holds one to return.
//
// The comparison is of each term's own contribution — a systematic term's
// magnitude against an independent one's, both at 1σ — which is what makes "the
// control point is most of your budget" answerable. Where two terms tie, the
// one contributed first wins, so the answer does not depend on the order the
// walk happened to reach equal claims in.
func (b Budget) Dominant() (BudgetTerm, bool) {
	if len(b.terms) == 0 {
		return BudgetTerm{}, false
	}

	dominant := b.terms[0]
	for _, term := range b.terms[1:] {
		if math.Abs(term.Magnitude) > math.Abs(dominant.Magnitude) {
			dominant = term
		}
	}
	return dominant.clone(), true
}

// Combined reduces the budget to one standard uncertainty.
//
// The terms combine the way specification section 6.6.5 says they do:
// independent terms in quadrature, systematic terms linearly, and the two
// together in quadrature.
//
//	u = √( Σ uᵢ² + ( Σ |sⱼ| )² )
//
// Every magnitude is already one standard deviation, so the result is too, and
// the [Uncertainty] which comes back says so. Nothing widened is produced here
// and nothing widened is ever stored; widening is [Uncertainty.Widen], which
// attaches the factor it widened by.
//
// Three things stop it, each a value a caller can inspect: a claim which stated
// no accuracy ([UnknownAccuracyError]), terms which are not all in one unit
// ([MixedUnitsError]), and a budget holding no term at all ([ErrEmptyBudget]).
// The unknown claim is reported first, because a budget which is both tainted
// and mixed is tainted whatever the units say.
func (b Budget) Combined() (Uncertainty, error) {
	if !b.Known() {
		return Uncertainty{}, UnknownAccuracyError{Claims: b.Unknown()}
	}

	if len(b.terms) == 0 {
		return Uncertainty{}, ErrEmptyBudget
	}

	unit := b.terms[0].Unit

	// squares is the independent terms in quadrature; shared is the systematic
	// ones, which add linearly because they are the same error appearing twice
	// rather than two errors which might cancel.
	var squares, shared float64

	for _, term := range b.terms {
		if term.Unit != unit {
			return Uncertainty{}, MixedUnitsError{Units: budgetUnits(b.terms)}
		}

		magnitude := math.Abs(term.Magnitude)
		switch term.Kind {
		case TermIndependent:
			squares += magnitude * magnitude
		case TermSystematic:
			shared += magnitude
		}
	}

	return Uncertainty{
		Magnitude:      math.Sqrt(squares + shared*shared),
		Unit:           unit,
		CoverageFactor: 1,
	}, nil
}

// sameClaim reports whether two claims are the same input to a computation.
//
// The same claim reached twice is usually the same pointer, because a
// computation reads its inputs out of one loaded model. It need not be — a
// claim can be read out of two indexes of the same model — so a written id
// counts as well, ids being unique across a model by construction
// ([0002](docs/decisions/0002-immutable-id-mutable-label.md)).
//
// A claim which wrote no id is the same input only as itself. There is nothing
// else to compare, and calling two anonymous claims the same because they say
// the same thing would fold two independent measurements into one.
func sameClaim(a, b *Claim) bool {
	if a == b {
		return true
	}

	first, written := a.ID()
	second, alsoWritten := b.ID()

	return written && alsoWritten && first == second
}

// usable reports whether every term of an accuracy is one the arithmetic can
// use.
//
// A magnitude which is not a finite number, and a term of a kind the closed set
// has no member for, are both things the loader reports as diagnostics. Reaching
// here they mean the accuracy was only read in part, and part of an accuracy is
// not an accuracy: the claim is unknown rather than accurate to whatever
// survived.
func usable(accuracy Accuracy) bool {
	if len(accuracy.Terms) == 0 {
		return false
	}

	for _, term := range accuracy.Terms {
		if term.Kind != TermIndependent && term.Kind != TermSystematic {
			return false
		}
		if math.IsNaN(term.Magnitude) || math.IsInf(term.Magnitude, 0) {
			return false
		}
	}

	return true
}

// termName is what a term is called in a report.
//
// A systematic term is named by the thing it is shared with, which is the whole
// point of naming it: two budgets mentioning survey:CP-3 are talking about the
// same control point. An independent term belongs to the one measurement which
// carried it and has no id of its own, so it is named for the claim instead —
// by the same spelling a diagnostic names a claim with, so that a budget and a
// diagnostic about the same claim call it the same thing.
func termName(claim *Claim, term AccuracyTerm) string {
	if term.Kind == TermSystematic && term.Source != "" {
		return string(term.Source)
	}
	return claimName(claim)
}

// budgetUnits is the units of a set of terms, each once, in the order they were
// contributed.
func budgetUnits(terms []BudgetTerm) []Unit {
	var units []Unit
	for _, term := range terms {
		if !slices.Contains(units, term.Unit) {
			units = append(units, term.Unit)
		}
	}
	return units
}
