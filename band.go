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

// BandSource is what stated one of the accuracies a band was widened by.
//
// The set is closed because it is read by whatever acts on the answer, and a
// phrase composed at each call site would be a vocabulary nothing could match
// on. Which of them a check reports is decided by what the check compares, and
// nothing else about a term says which side of the comparison it came from.
type BandSource string

// The sides of a comparison which state an accuracy of their own.
const (
	// BandFromClaim is the accuracy the claim under comparison states of
	// itself: the area somebody wrote down, and how well they say they know it.
	BandFromClaim BandSource = "claim"

	// BandFromCorners is the accumulated accuracy of the position claims the
	// subject's own shape is read from.
	BandFromCorners BandSource = "corners"

	// BandFromContainer is the same for the shape the subject is judged
	// against, where the check compares two shapes rather than a shape and a
	// number.
	BandFromContainer BandSource = "container"
)

// BandTerm is one accuracy which went into a band.
//
// It is reported rather than folded into the total because the total is not
// actionable on its own. A band eight times the tolerance it was declared with
// is either a tape nobody has calibrated or a figure whose author stated an
// honest uncertainty, and the two are fixed in different places — so a reader
// has to be told which side of the comparison the width came from and how much
// of it each side is.
type BandTerm struct {
	// Source is which side of the comparison stated it.
	Source BandSource `json:"source"`

	// Sigma is the one standard uncertainty that side states, in Unit. It is
	// one sigma because every accuracy in a model is
	// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)).
	Sigma float64 `json:"sigma"`

	// Unit is what Sigma is in, which is not always the band's own unit: an
	// area is compared in the square of a length and the corners behind it are
	// surveyed in the length, which is the whole reason Sensitivity exists.
	Unit Unit `json:"unit"`

	// Sensitivity is how far the compared figure moves per unit of Sigma, which
	// carries the term into the band's unit. It is one where the two are already
	// in the same unit, and the length of the boundary where a corner
	// displacement is carried across to the area it encloses: displacing a
	// boundary of length P by δ changes the area it encloses by about P·δ. It is
	// a first-order sensitivity and is reported as one.
	Sensitivity float64 `json:"sensitivity"`

	// Contribution is Sigma × Sensitivity, in the band's unit, which is the
	// figure combined into the band. It is written rather than left to be
	// multiplied out because it is the number a reader compares against the
	// declared tolerance, and a reader doing the multiplication is a reader who
	// can do it wrong.
	Contribution float64 `json:"contribution"`
}

// bandTerm is one term with its contribution worked out, which is the only way
// one is made: a term whose Contribution disagreed with its Sigma and its
// Sensitivity would be a report of arithmetic nobody did.
func bandTerm(source BandSource, sigma float64, unit Unit, sensitivity float64) BandTerm {
	return BandTerm{
		Source:       source,
		Sigma:        sigma,
		Unit:         unit,
		Sensitivity:  sensitivity,
		Contribution: sigma * sensitivity,
	}
}

// Band is what a check decided a comparison against, where the tolerance it was
// declared with is the floor under the answer rather than the whole of it.
//
// Widening is right and is not in question: a claim cannot be held to a
// precision the geometry does not have, and two figures which differ by less
// than their combined uncertainty do not disagree. What a band is for is saying
// so. Left unreported, the number the registry states is not the number the
// check applies and nothing says which is which — so a rule written as "the
// boundary agrees with the appraisal to within half a square foot" can be
// satisfied by a comparison made against eight, and the run which passed it
// reads exactly like one which did not need to widen anything.
//
// That is the failure this reports: not a wrong answer, but an acceptance
// criterion nobody can falsify. It is reported on passes as well as failures for
// the same reason — a pass is the answer whose band nothing else in the output
// discloses.
//
// A check which decides against the tolerance it was given and nothing else
// reports no band. There is nothing to disclose: the declared figure is the
// applied one, and it is already written in the rule.
type Band struct {
	// Tolerance is the name of the declared tolerance the rule was given, which
	// is where a reader goes to change the floor.
	Tolerance string `json:"tolerance"`

	// Floor is that tolerance's value: the narrowest figure this comparison
	// could have been decided against.
	Floor float64 `json:"floor"`

	// Applied is the figure the difference was actually compared against, which
	// is Floor or the terms combined where that is wider.
	Applied float64 `json:"applied"`

	// Unit is what Floor, Applied and Difference are in. It is the unit of what
	// is compared, so it is the square of the frame's linear unit for an area
	// and the linear unit itself for a length.
	Unit Unit `json:"unit"`

	// Difference is the magnitude of the gap the band was applied to: how far
	// apart the two figures are, with no sign, because which of them is larger
	// is the failure's to say and is not what the band decided.
	Difference float64 `json:"difference"`

	// Widened reports whether Applied is wider than Floor, which is whether the
	// check decided against a figure nobody wrote down.
	Widened bool `json:"widened"`

	// Decisive reports whether the widening is what decided the answer: the
	// difference is inside Applied and outside Floor, so the same comparison
	// made against the declared tolerance alone would have gone the other way.
	//
	// It is what tells a pass which needed the widening from a pass within the
	// tolerance as written, and those are two different states of a model. The
	// first is a criterion the survey is not accurate enough to test; the second
	// is a criterion which held.
	Decisive bool `json:"decisive"`

	// Terms are the accuracies combined into Applied, one per side of the
	// comparison which stated one, in the order the check reports them. It is
	// empty where neither side stated one and the floor is the whole of the
	// test.
	Terms []BandTerm `json:"terms,omitempty"`
}

// banded is a band with the terms combined in and what they decided worked out.
//
// Every band is made through here, so that Applied, Widened and Decisive are
// one arithmetic rather than three which have to be kept agreeing. The terms are
// combined in quadrature, as separate measurements of one quantity: a side which
// stated no accuracy contributes no term rather than a zero, because an unstated
// accuracy is unknown rather than zero
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)) and a zero term would
// read as a side which was measured perfectly.
func banded(declared Tolerance, difference float64, terms ...BandTerm) Band {
	band := Band{
		Tolerance:  declared.Name,
		Floor:      declared.Value,
		Applied:    declared.Value,
		Unit:       declared.Unit,
		Difference: math.Abs(difference),
		Terms:      terms,
	}

	var squares float64
	for _, term := range terms {
		squares += term.Contribution * term.Contribution
	}

	if combined := math.Sqrt(squares); combined > band.Applied {
		band.Applied = combined
	}

	band.Widened = band.Applied > band.Floor
	band.Decisive = band.Widened && band.Difference > band.Floor && band.Difference <= band.Applied

	return band
}

// holds reports whether the difference is inside the band, which is the whole of
// what a band decides.
func (b Band) holds() bool { return b.Difference <= b.Applied }

// against is how a hint names the figure the comparison was decided against.
//
// It names the applied figure and not the declared one, and where the two differ
// it says what widened it. A hint naming only the tolerance sends a reader to
// tighten a number which decided nothing; one naming only the applied figure
// leaves them with a number appearing nowhere in the model and nothing to change
// about it. Both, in that order, is the only rendering which leads somewhere.
func (b Band) against() string {
	// A widened band always has a term, because nothing else can widen one. The
	// second half of the condition is what keeps the sentence readable rather
	// than trailing off after "widened by", if that ever stops being true.
	if !b.Widened || len(b.Terms) == 0 {
		return fmt.Sprintf("the tolerance %s, which is %s %s", b.Tolerance, decimal(b.Floor), b.Unit)
	}

	return fmt.Sprintf(
		"%s %s: the tolerance %s, which is %s %s, widened by %s",
		decimal(b.Applied), b.Unit,
		b.Tolerance, decimal(b.Floor), b.Unit,
		b.widenedBy(),
	)
}

// widenedBy is what the terms did, read as a phrase: how well each side is
// known, and what carried a term into the unit the comparison is made in.
//
// A term whose sensitivity is one is stated without it. Saying that a length was
// carried across to a length by multiplying by one is arithmetic nobody needs
// read back to them, and it would bury the one sensitivity which does decide
// something — the perimeter an area's band is scaled by, which is what makes an
// area's band the multiple of its tolerance that it is.
func (b Band) widenedBy() string {
	written := make([]string, 0, len(b.Terms))
	for _, term := range b.Terms {
		if term.Sensitivity == 1 {
			written = append(written, fmt.Sprintf("%s (%s %s)", term.Source.phrase(), decimal(term.Sigma), term.Unit))
			continue
		}

		written = append(written, fmt.Sprintf(
			"%s (%s %s over a boundary of %s %s, which is %s %s)",
			term.Source.phrase(), decimal(term.Sigma), term.Unit,
			decimal(term.Sensitivity), term.Unit,
			decimal(term.Contribution), b.Unit,
		))
	}

	return strings.Join(written, " and ")
}

// phrase is what a hint calls a side of the comparison which stated an accuracy.
func (s BandSource) phrase() string {
	switch s {
	case BandFromClaim:
		return "how well the claim says it is known"
	case BandFromCorners:
		return "how well its corners are surveyed"
	case BandFromContainer:
		return "how well the corners it is judged against are surveyed"
	}
	return string(s)
}
