// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// areaAgreement is the declared tolerance the cases below widen, in the shape a
// registry states one: half a square foot of agreement between a figure somebody
// wrote down and the shape it describes.
func areaAgreement() Tolerance {
	return Tolerance{Name: "area-agreement", Value: 0.5, Unit: "usft2"}
}

func TestBanded(t *testing.T) {
	testCases := []struct {
		name       string
		declared   Tolerance
		difference float64
		terms      []BandTerm
		expected   Band
	}{
		{
			name:       "applies the declared tolerance where neither side states an accuracy",
			declared:   areaAgreement(),
			difference: 0.2,
			expected: Band{
				Tolerance:  "area-agreement",
				Floor:      0.5,
				Applied:    0.5,
				Unit:       "usft2",
				Difference: 0.2,
			},
		},
		{
			name:       "keeps the declared tolerance where the terms combine to less than it",
			declared:   areaAgreement(),
			difference: 0.2,
			terms: []BandTerm{
				bandTerm(BandFromClaim, 0.3, "usft2", 1),
			},
			expected: Band{
				Tolerance:  "area-agreement",
				Floor:      0.5,
				Applied:    0.5,
				Unit:       "usft2",
				Difference: 0.2,
				Terms: []BandTerm{
					{Source: BandFromClaim, Sigma: 0.3, Unit: "usft2", Sensitivity: 1, Contribution: 0.3},
				},
			},
		},
		{
			name:       "applies the terms where they combine to more than the declared tolerance",
			declared:   areaAgreement(),
			difference: 0.2,
			terms: []BandTerm{
				bandTerm(BandFromClaim, 3, "usft2", 1),
				bandTerm(BandFromCorners, 0.8, "usft", 5),
			},
			expected: Band{
				Tolerance:  "area-agreement",
				Floor:      0.5,
				Applied:    5,
				Unit:       "usft2",
				Difference: 0.2,
				Widened:    true,
				Terms: []BandTerm{
					{Source: BandFromClaim, Sigma: 3, Unit: "usft2", Sensitivity: 1, Contribution: 3},
					{Source: BandFromCorners, Sigma: 0.8, Unit: "usft", Sensitivity: 5, Contribution: 4},
				},
			},
		},
		{
			name:       "calls the widening decisive where the difference is outside the tolerance and inside the band",
			declared:   areaAgreement(),
			difference: 4,
			terms: []BandTerm{
				bandTerm(BandFromClaim, 3, "usft2", 1),
				bandTerm(BandFromCorners, 0.8, "usft", 5),
			},
			expected: Band{
				Tolerance:  "area-agreement",
				Floor:      0.5,
				Applied:    5,
				Unit:       "usft2",
				Difference: 4,
				Widened:    true,
				Decisive:   true,
				Terms: []BandTerm{
					{Source: BandFromClaim, Sigma: 3, Unit: "usft2", Sensitivity: 1, Contribution: 3},
					{Source: BandFromCorners, Sigma: 0.8, Unit: "usft", Sensitivity: 5, Contribution: 4},
				},
			},
		},
		{
			name:       "does not call the widening decisive where the difference is inside the tolerance as written",
			declared:   areaAgreement(),
			difference: 0.4,
			terms: []BandTerm{
				bandTerm(BandFromClaim, 3, "usft2", 1),
				bandTerm(BandFromCorners, 0.8, "usft", 5),
			},
			expected: Band{
				Tolerance:  "area-agreement",
				Floor:      0.5,
				Applied:    5,
				Unit:       "usft2",
				Difference: 0.4,
				Widened:    true,
				Terms: []BandTerm{
					{Source: BandFromClaim, Sigma: 3, Unit: "usft2", Sensitivity: 1, Contribution: 3},
					{Source: BandFromCorners, Sigma: 0.8, Unit: "usft", Sensitivity: 5, Contribution: 4},
				},
			},
		},
		{
			name:       "does not call the widening decisive where the difference is outside the band as well",
			declared:   areaAgreement(),
			difference: 8,
			terms: []BandTerm{
				bandTerm(BandFromClaim, 3, "usft2", 1),
				bandTerm(BandFromCorners, 0.8, "usft", 5),
			},
			expected: Band{
				Tolerance:  "area-agreement",
				Floor:      0.5,
				Applied:    5,
				Unit:       "usft2",
				Difference: 8,
				Widened:    true,
				Terms: []BandTerm{
					{Source: BandFromClaim, Sigma: 3, Unit: "usft2", Sensitivity: 1, Contribution: 3},
					{Source: BandFromCorners, Sigma: 0.8, Unit: "usft", Sensitivity: 5, Contribution: 4},
				},
			},
		},
		{
			name:       "reports the gap as a magnitude, whichever way it runs",
			declared:   areaAgreement(),
			difference: -0.2,
			expected: Band{
				Tolerance:  "area-agreement",
				Floor:      0.5,
				Applied:    0.5,
				Unit:       "usft2",
				Difference: 0.2,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := banded(testCase.declared, testCase.difference, testCase.terms...)

			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestABandDecidesTheAnswerItReports covers the one thing a band is allowed to
// be read for: whether the difference is inside it.
//
// It is its own function because it is a different assertion from the shape of
// the band above — the arithmetic and the verdict it supports are separately
// wrong-able, and a table which returned both would stop saying which of them a
// failure is about.
func TestABandDecidesTheAnswerItReports(t *testing.T) {
	testCases := []struct {
		name       string
		difference float64
		expected   bool
	}{
		{name: "holds where the difference is inside the band", difference: 4.9, expected: true},
		{name: "holds where the difference is exactly the band", difference: 5, expected: true},
		{name: "does not hold where the difference is outside it", difference: 5.1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			band := banded(areaAgreement(), testCase.difference,
				bandTerm(BandFromClaim, 3, "usft2", 1),
				bandTerm(BandFromCorners, 0.8, "usft", 5),
			)

			assert.Equal(t, testCase.expected, band.holds())
		})
	}
}

// TestABandNamesWhatWidenedIt covers the sentence a hint reads a band as.
//
// The applied figure alone is a number appearing nowhere in the model, and the
// declared tolerance alone sends a reader to tighten something which decided
// nothing. What the rendering has to do is lead from the one to the other.
func TestABandNamesWhatWidenedIt(t *testing.T) {
	testCases := []struct {
		name     string
		band     Band
		expected string
	}{
		{
			name:     "names the tolerance alone where nothing widened it",
			band:     banded(areaAgreement(), 0.2),
			expected: "the tolerance area-agreement, which is 0.5 usft2",
		},
		{
			name:     "names the tolerance alone where the terms did not reach it",
			band:     banded(areaAgreement(), 0.2, bandTerm(BandFromClaim, 0.3, "usft2", 1)),
			expected: "the tolerance area-agreement, which is 0.5 usft2",
		},
		{
			name: "names the applied figure, the tolerance under it and how well the claim is known",
			band: banded(areaAgreement(), 0.2, bandTerm(BandFromClaim, 2, "usft2", 1)),
			expected: "2.0 usft2: the tolerance area-agreement, which is 0.5 usft2, widened by how well the " +
				"claim says it is known (2.0 usft2)",
		},
		{
			name: "carries a corner accuracy across by the boundary it is scaled along",
			band: banded(areaAgreement(), 0.2, bandTerm(BandFromCorners, 0.02, "usft", 136.84)),
			expected: "2.7368 usft2: the tolerance area-agreement, which is 0.5 usft2, widened by how well its " +
				"corners are surveyed (0.02 usft over a boundary of 136.84 usft, which is 2.7368 usft2)",
		},
		{
			name: "names both sides where both stated one",
			band: banded(
				Tolerance{Name: "boundary-closure", Value: 0.005, Unit: "m"}, 0.02,
				bandTerm(BandFromCorners, 0.008, "m", 1),
				bandTerm(BandFromContainer, 0.008, "m", 1),
			),
			expected: "0.01131370849898476 m: the tolerance boundary-closure, which is 0.005 m, widened by how " +
				"well its corners are surveyed (0.008 m) and how well the corners it is judged against are " +
				"surveyed (0.008 m)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, testCase.band.against())
		})
	}
}
