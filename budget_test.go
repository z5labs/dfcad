// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// budgetTolerance is how far a combined figure may land from the arithmetic the
// test states, in the unit the terms were written in.
//
// It is stated rather than assumed. Combining terms is a sum of squares and a
// square root, each of which rounds once, and a picometre is several orders of
// magnitude larger than that rounding and several smaller than anything anybody
// surveys — which makes a failure here a mistake in the combination rather than
// a change in the last bit.
const budgetTolerance = 1e-12

// budgetOf accumulates the claims of a set of specifications into one budget,
// the way a computation reading them all would.
func budgetOf(specs ...claimSpec) Budget {
	var budget Budget
	budget.Add(writtenClaims(specs...)...)
	return budget
}

// measured is one claim of a budget test: an id, so that a term can be
// attributed to it by name, and the terms it states.
func measured(id ID, terms ...AccuracyTerm) claimSpec {
	return claimSpec{id: id, value: 8.5, terms: terms, date: "2026-05-06"}
}

// unmeasured is one claim which says nothing about how well its value is known,
// which is what taints a budget.
func unmeasured(id ID) claimSpec {
	return claimSpec{id: id, value: 8.5, date: "2026-05-06"}
}

func TestBudgetCombined(t *testing.T) {
	testCases := []struct {
		name     string
		specs    []claimSpec
		expected float64
	}{
		{
			name: "combines independent terms in quadrature",
			specs: []claimSpec{
				measured("survey:C-01", independent(0.003)),
				measured("survey:C-02", independent(0.004)),
			},
			// √(0.003² + 0.004²)
			expected: 0.005,
		},
		{
			name: "combines the independent and the systematic contributions in quadrature",
			specs: []claimSpec{
				measured("survey:C-01", independent(0.004), systematic(0.003, "survey:CP-3")),
			},
			// √(0.004² + 0.003²)
			expected: 0.005,
		},
		{
			name: "counts a systematic term shared by two claims once",
			specs: []claimSpec{
				measured("survey:C-01", independent(0.004), systematic(0.008, "survey:CP-3")),
				measured("survey:C-02", independent(0.003), systematic(0.008, "survey:CP-3")),
			},
			// √(0.004² + 0.003² + 0.008²), and not √(0.004² + 0.003² + 0.016²).
			expected: math.Sqrt(89e-6),
		},
		{
			name: "counts systematic terms naming different sources separately",
			specs: []claimSpec{
				measured("survey:C-01", systematic(0.008, "survey:CP-3")),
				measured("survey:C-02", systematic(0.006, "survey:CP-7")),
			},
			// (0.008 + 0.006), squared and rooted, which is the sum itself.
			expected: 0.014,
		},
		{
			name: "counts one claim once however many times it is accumulated",
			specs: []claimSpec{
				measured("survey:C-01", independent(0.003)),
				measured("survey:C-01", independent(0.003)),
			},
			expected: 0.003,
		},
		{
			name: "takes the wider of two magnitudes written for one shared term",
			specs: []claimSpec{
				measured("survey:C-01", systematic(0.008, "survey:CP-3")),
				measured("survey:C-02", systematic(0.011, "survey:CP-3")),
			},
			expected: 0.011,
		},
		{
			name: "reads a magnitude written negative as the width it is",
			specs: []claimSpec{
				measured("survey:C-01", independent(-0.005)),
			},
			expected: 0.005,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			budget := budgetOf(testCase.specs...)

			combined, err := budget.Combined()

			require.NoError(t, err)
			assert.InDelta(t, testCase.expected, combined.Magnitude, budgetTolerance)
			assert.Equal(t, UnitMetre, combined.Unit)
			assert.Equal(t, 1.0, combined.CoverageFactor, "the storage convention is one standard deviation")
		})
	}
}

// TestSystematicTermsAddLinearlyRatherThanInQuadrature is its own function
// because it is the one assertion the arithmetic exists for, and it is worth
// reading as arithmetic rather than as a row of a table.
//
// Two systematic terms of 0.03 m and 0.04 m are the same two numbers whichever
// rule combines them. In quadrature they come to 0.05 m; added linearly they
// come to 0.07 m. Forty percent is not a rounding difference — it is the
// difference between a clearance which passes and one which does not — and the
// linear answer is the one a shared error actually produces, because two facts
// located from the same control point share that control point's error entirely
// rather than partly cancelling it.
func TestSystematicTermsAddLinearlyRatherThanInQuadrature(t *testing.T) {
	budget := budgetOf(
		measured("survey:C-01", systematic(0.03, "survey:CP-3")),
		measured("survey:C-02", systematic(0.04, "survey:CP-7")),
	)

	combined, err := budget.Combined()
	require.NoError(t, err)

	const linear = 0.03 + 0.04
	quadrature := math.Sqrt(0.03*0.03 + 0.04*0.04)

	assert.InDelta(t, linear, combined.Magnitude, budgetTolerance)
	assert.Greater(t, combined.Magnitude, quadrature,
		"combining shared terms in quadrature would understate the budget by %g m", linear-quadrature)
}

// TestBudgetTermsAreNamedAndAttributed is its own function because it asserts
// what a budget carries rather than what it computes: every term named, and
// every term traceable to the claim which put it there.
//
// A combined figure with no itemisation is an answer nobody can act on. "The
// control point is most of your budget" says what to re-measure; "±0.01 m" does
// not.
func TestBudgetTermsAreNamedAndAttributed(t *testing.T) {
	claims := writtenClaims(
		measured("survey:C-01", independent(0.004), systematic(0.008, "survey:CP-3")),
		measured("survey:C-02", independent(0.003), systematic(0.008, "survey:CP-3")),
	)

	var budget Budget
	budget.Add(claims...)

	terms := budget.Terms()
	require.Len(t, terms, 3, "the shared term is one term and not two")

	t.Run("names an independent term for the claim which carried it", func(t *testing.T) {
		assert.Equal(t, TermIndependent, terms[0].Kind)
		assert.Equal(t, "survey:C-01", terms[0].Name)
		assert.Equal(t, 0.004, terms[0].Magnitude)
		assert.Equal(t, []*Claim{claims[0]}, terms[0].Contributors)
		assert.False(t, terms[0].Shared())
	})

	t.Run("names a systematic term for the source it is shared with", func(t *testing.T) {
		assert.Equal(t, TermSystematic, terms[1].Kind)
		assert.Equal(t, "survey:CP-3", terms[1].Name)
		assert.Equal(t, ID("survey:CP-3"), terms[1].Source)
	})

	t.Run("attributes a shared term to every claim which contributed it", func(t *testing.T) {
		assert.Equal(t, []*Claim{claims[0], claims[1]}, terms[1].Contributors)
		assert.True(t, terms[1].Shared())
	})

	t.Run("keeps the second claim's independent term as a term of its own", func(t *testing.T) {
		assert.Equal(t, TermIndependent, terms[2].Kind)
		assert.Equal(t, "survey:C-02", terms[2].Name)
		assert.Equal(t, []*Claim{claims[1]}, terms[2].Contributors)
	})

	t.Run("says which term dominates the budget", func(t *testing.T) {
		dominant, ok := budget.Dominant()

		require.True(t, ok)
		assert.Equal(t, "survey:CP-3", dominant.Name)
	})

	t.Run("copies the terms it hands out, down to the claims attributed to each", func(t *testing.T) {
		terms[0].Magnitude = 99.0
		terms[1].Contributors = append(terms[1].Contributors[:1], nil)

		fresh := budget.Terms()
		assert.Equal(t, 0.004, fresh[0].Magnitude)
		assert.Equal(t, []*Claim{claims[0], claims[1]}, fresh[1].Contributors)
	})
}

// TestBudgetNamesAClaimWhichWroteNoID is its own function because a claim id is
// optional, and a term named by an empty string is a term nobody can trace.
func TestBudgetNamesAClaimWhichWroteNoID(t *testing.T) {
	budget := budgetOf(claimSpec{value: 8.5, terms: []AccuracyTerm{independent(0.004)}, date: "2026-05-06"})

	terms := budget.Terms()

	require.Len(t, terms, 1)
	assert.Equal(t, "the width of site:S-101", terms[0].Name)
}

// TestBudgetTaintsOnAnUnstatedAccuracy is its own function because it asserts
// an absence rather than a figure: the point is that no number comes out.
//
// A claim which says nothing about how well its value is known is unknown and
// not zero. Reading it as zero would let it pass through a clearance
// computation and come out looking like the most accurate input the
// computation had.
func TestBudgetTaintsOnAnUnstatedAccuracy(t *testing.T) {
	claims := writtenClaims(
		measured("survey:C-01", independent(0.004)),
		unmeasured("survey:C-02"),
	)

	var budget Budget
	budget.Add(claims...)

	t.Run("reports the budget as not known", func(t *testing.T) {
		assert.False(t, budget.Known())
		assert.Equal(t, []*Claim{claims[1]}, budget.Unknown())
	})

	t.Run("still carries the terms it could read", func(t *testing.T) {
		require.Len(t, budget.Terms(), 1)
		assert.Equal(t, "survey:C-01", budget.Terms()[0].Name)
	})

	t.Run("combines to nothing, naming the claim which tainted it", func(t *testing.T) {
		_, err := budget.Combined()

		var unknown UnknownAccuracyError
		require.ErrorAs(t, err, &unknown)
		assert.Equal(t, []*Claim{claims[1]}, unknown.Claims)
	})

	t.Run("does not treat the unstated accuracy as zero", func(t *testing.T) {
		var measured Budget
		measured.Add(claims[0])

		combined, err := measured.Combined()
		require.NoError(t, err)

		// Were the unmeasured claim read as an exact input, the tainted budget
		// would combine to exactly this and nothing would say otherwise.
		assert.InDelta(t, 0.004, combined.Magnitude, budgetTolerance)
		assert.False(t, budget.Known())
	})
}

// TestBudgetTaintsOnAnAccuracyItCouldOnlyPartlyRead is its own function because
// the claims it needs cannot be written by [writtenClaims]: the loader reports
// each of these as a diagnostic, so a claim carrying one only reaches the
// arithmetic where something above went wrong.
func TestBudgetTaintsOnAnAccuracyItCouldOnlyPartlyRead(t *testing.T) {
	testCases := []struct {
		name  string
		terms []AccuracyTerm
	}{
		{
			name:  "an accuracy holding no term at all",
			terms: nil,
		},
		{
			name:  "a magnitude which is not a number",
			terms: []AccuracyTerm{independent(math.NaN())},
		},
		{
			name:  "a magnitude with no finite width",
			terms: []AccuracyTerm{independent(math.Inf(1))},
		},
		{
			name:  "a term of no kind the closed set has a member for",
			terms: []AccuracyTerm{{Kind: "estimated", Magnitude: 0.004, Unit: UnitMetre}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			claim := &Claim{
				id:          "survey:C-01",
				subject:     resolutionSubject,
				predicate:   "width",
				accuracy:    Accuracy{Terms: testCase.terms},
				hasAccuracy: true,
			}

			var budget Budget
			budget.Add(claim)

			assert.False(t, budget.Known())
			assert.Equal(t, []*Claim{claim}, budget.Unknown())

			_, err := budget.Combined()

			var unknown UnknownAccuracyError
			assert.ErrorAs(t, err, &unknown)
		})
	}
}

// TestBudgetRefusesTermsInMoreThanOneUnit is its own function because it
// asserts a refusal rather than a figure. Nothing here converts, so a budget
// mixing millimetres and metres has no answer rather than a silently converted
// one.
func TestBudgetRefusesTermsInMoreThanOneUnit(t *testing.T) {
	budget := budgetOf(
		measured("survey:C-01", independent(0.004)),
		measured("survey:C-02", independentIn(3.0, "mm")),
	)

	_, err := budget.Combined()

	var mixed MixedUnitsError
	require.ErrorAs(t, err, &mixed)
	assert.Equal(t, []Unit{UnitMetre, "mm"}, mixed.Units)
}

// TestBudgetKeepsOneTermIDInTwoUnitsApart is its own function because it is the
// one case where the deduplication rule and the unit rule meet: the same source
// written in two units is not one term, because folding it into one would
// swallow the disagreement.
func TestBudgetKeepsOneTermIDInTwoUnitsApart(t *testing.T) {
	budget := budgetOf(
		measured("survey:C-01", systematic(0.008, "survey:CP-3")),
		measured("survey:C-02", AccuracyTerm{
			Kind:      TermSystematic,
			Magnitude: 8.0,
			Unit:      "mm",
			Source:    "survey:CP-3",
		}),
	)

	require.Len(t, budget.Terms(), 2)

	_, err := budget.Combined()

	var mixed MixedUnitsError
	assert.ErrorAs(t, err, &mixed)
}

// TestBudgetCombinesNothingWithoutATerm is its own function because an empty
// budget is not an uncertainty of zero. A computation which read no claim has
// no measured input, and answering it exactly would be the optimism the whole
// arrangement refuses.
func TestBudgetCombinesNothingWithoutATerm(t *testing.T) {
	var budget Budget

	t.Run("reports an empty budget as known, because nothing tainted it", func(t *testing.T) {
		assert.True(t, budget.Known())
		assert.Empty(t, budget.Terms())
	})

	t.Run("combines to nothing rather than to zero", func(t *testing.T) {
		_, err := budget.Combined()

		assert.ErrorIs(t, err, ErrEmptyBudget)
	})

	t.Run("has no dominant term", func(t *testing.T) {
		_, ok := budget.Dominant()

		assert.False(t, ok)
	})

	t.Run("accumulates nothing from a claim which is not there", func(t *testing.T) {
		budget.Add(nil)

		assert.True(t, budget.Known())
		assert.Empty(t, budget.Terms())
	})
}

// TestBudgetMerge is its own function because merging is what makes a
// computation over computations count a shared term once: the two budgets each
// carry survey:CP-3, and the merged one carries it exactly once.
func TestBudgetMerge(t *testing.T) {
	left := budgetOf(measured("survey:C-01", independent(0.004), systematic(0.008, "survey:CP-3")))
	right := budgetOf(measured("survey:C-02", independent(0.003), systematic(0.008, "survey:CP-3")))

	var merged Budget
	merged.Merge(left)
	merged.Merge(right)

	require.Len(t, merged.Terms(), 3)

	combined, err := merged.Combined()
	require.NoError(t, err)

	// √(0.004² + 0.003² + 0.008²), which is what the two budgets come to
	// together rather than the quadrature of what each came to alone.
	assert.InDelta(t, math.Sqrt(89e-6), combined.Magnitude, budgetTolerance)

	t.Run("carries a taint across the merge", func(t *testing.T) {
		tainted := budgetOf(unmeasured("survey:C-03"))

		var out Budget
		out.Merge(left)
		out.Merge(tainted)

		assert.False(t, out.Known())
	})

	t.Run("merging a budget twice accumulates its claims once", func(t *testing.T) {
		var twice Budget
		twice.Merge(left)
		twice.Merge(left)

		assert.Equal(t, left.Terms(), twice.Terms())
	})
}

// TestUncertaintyStatesItsCoverageFactor is its own function because it is
// about how a figure is reported rather than about how it was computed. A
// width with no factor attached means whatever the reader assumed, and the
// three readings differ by a factor of two.
func TestUncertaintyStatesItsCoverageFactor(t *testing.T) {
	standard := Uncertainty{Magnitude: 0.005, Unit: UnitMetre, CoverageFactor: 1}

	t.Run("writes the factor beside every figure", func(t *testing.T) {
		assert.Equal(t, "0.005 m (k = 1.0, ≈ 68%)", standard.String())
	})

	t.Run("widens to the factor asked for and says so", func(t *testing.T) {
		widened, err := standard.Widen(2)

		require.NoError(t, err)
		assert.InDelta(t, 0.01, widened.Magnitude, budgetTolerance)
		assert.Equal(t, 2.0, widened.CoverageFactor)
		assert.Equal(t, "0.01 m (k = 2.0, ≈ 95%)", widened.String())
	})

	t.Run("widens from the standard figure rather than compounding two factors", func(t *testing.T) {
		widened, err := standard.Widen(2)
		require.NoError(t, err)

		again, err := widened.Widen(3)
		require.NoError(t, err)

		assert.InDelta(t, 0.015, again.Magnitude, budgetTolerance)
		assert.InDelta(t, 0.005, again.Standard(), budgetTolerance)
	})

	t.Run("prints the factor alone where the confidence has no customary spelling", func(t *testing.T) {
		widened, err := standard.Widen(1.5)

		require.NoError(t, err)
		_, spelled := widened.Confidence()
		assert.False(t, spelled)
		assert.Equal(t, "0.0075 m (k = 1.5)", widened.String())
	})

	t.Run("spells the three customary factors", func(t *testing.T) {
		for factor, expected := range map[float64]string{1: "≈ 68%", 2: "≈ 95%", 3: "≈ 99.7%"} {
			widened, err := standard.Widen(factor)
			require.NoError(t, err)

			confidence, spelled := widened.Confidence()
			require.True(t, spelled)
			assert.Equal(t, expected, confidence)
		}
	})
}

func TestUncertaintyWidenRefusesAFactorNothingCanBeStatedAt(t *testing.T) {
	testCases := []struct {
		name   string
		factor float64
	}{
		{name: "a factor of zero, which would report every answer as exact", factor: 0},
		{name: "a negative factor, which would report a width as a deficit", factor: -2},
		{name: "a factor which is not a number", factor: math.NaN()},
		{name: "a factor with no finite width", factor: math.Inf(1)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			standard := Uncertainty{Magnitude: 0.005, Unit: UnitMetre, CoverageFactor: 1}

			_, err := standard.Widen(testCase.factor)

			var coverage CoverageFactorError
			require.ErrorAs(t, err, &coverage)
			assert.Equal(t, math.IsNaN(testCase.factor), math.IsNaN(coverage.Factor))
			if !math.IsNaN(testCase.factor) {
				assert.Equal(t, testCase.factor, coverage.Factor)
			}
		})
	}
}

// TestBudgetRanksTheSameWayResolutionDoes is its own function because the two
// arithmetics are one: a claim resolution calls unrankable is one whose budget
// does not combine, and a second implementation of the rule is a second place
// for it to drift.
func TestBudgetRanksTheSameWayResolutionDoes(t *testing.T) {
	claims := writtenClaims(
		measured("survey:C-01", independent(0.004), systematic(0.003, "survey:CP-3")),
		measured("survey:C-02", independent(0.006)),
	)

	resolution := resolutionOf(resolutionSubject, "width", claims)

	winner, ok := resolution.Claim()
	require.True(t, ok)

	id, _ := winner.ID()
	assert.Equal(t, ID("survey:C-01"), id, "0.005 m beats 0.006 m")

	var budget Budget
	budget.Add(winner)

	combined, err := budget.Combined()
	require.NoError(t, err)
	assert.InDelta(t, 0.005, combined.Magnitude, budgetTolerance)
}

// TestErrEmptyBudgetIsASentinel guards the one error here which carries
// nothing, so that a caller reaching for it with errors.Is keeps reaching it.
func TestErrEmptyBudgetIsASentinel(t *testing.T) {
	var budget Budget

	_, err := budget.Combined()

	require.True(t, errors.Is(err, ErrEmptyBudget))
}
