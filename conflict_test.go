// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// conflictSpec is one entry of the register as a test reads it: which pair it is
// about, which claims are competing, and what resolution made of them.
//
// The claims are named by their ids rather than compared as pointers because a
// failure has to say which claim was missing, and a pointer says only that one
// was.
type conflictSpec struct {
	subject   ID
	predicate string
	claims    []ID
	resolved  ID
	ambiguous bool
}

// registerOf walks the conflict register of claims indexed the way a load would
// index them.
func registerOf(claims []*Claim) []conflictSpec {
	var out []conflictSpec
	for conflict := range resolving(claims).Conflicts() {
		out = append(out, entryOf(conflict))
	}
	return out
}

// entryOf reads one entry into the shape a test asserts on.
func entryOf(conflict Conflict) conflictSpec {
	spec := conflictSpec{
		subject:   conflict.Subject(),
		predicate: conflict.Predicate(),
		claims:    idsOf(conflict.Claims()),
		ambiguous: conflict.Ambiguous(),
	}

	if claim, ok := conflict.Resolution().Claim(); ok {
		spec.resolved, _ = claim.ID()
	}

	return spec
}

func TestClaimsConflicts(t *testing.T) {
	testCases := []struct {
		name     string
		claims   []*Claim
		expected []conflictSpec
	}{
		{
			name: "reports a pair whose claims disagree and names the one which wins",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.05)}, date: "2026-01-09"},
				claimSpec{id: "survey:C-02", value: 8.53, terms: []AccuracyTerm{independent(0.003)}, date: "2026-05-06"},
			),
			expected: []conflictSpec{
				{
					subject:   resolutionSubject,
					predicate: "width",
					claims:    []ID{"survey:C-01", "survey:C-02"},
					resolved:  "survey:C-02",
				},
			},
		},
		{
			name: "reports a pair nothing separates as ambiguous rather than picking one",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.004)}, date: "2026-05-06"},
				claimSpec{id: "survey:C-02", value: 8.53, terms: []AccuracyTerm{independent(0.004)}, date: "2026-05-06"},
			),
			expected: []conflictSpec{
				{
					subject:   resolutionSubject,
					predicate: "width",
					claims:    []ID{"survey:C-01", "survey:C-02"},
					ambiguous: true,
				},
			},
		},
		{
			name: "reports nothing about a pair whose second claim has been deprecated",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.05)}, date: "2026-01-09"},
				claimSpec{
					id:    "survey:C-02",
					value: 8.53,
					terms: []AccuracyTerm{independent(0.003)},
					date:  "2026-05-06",
					rank:  RankDeprecated,
				},
			),
		},
		{
			name: "reports nothing about a pair every claim of which has been deprecated",
			claims: writtenClaims(
				claimSpec{
					id:    "survey:C-01",
					value: 8.5,
					terms: []AccuracyTerm{independent(0.05)},
					date:  "2026-01-09",
					rank:  RankDeprecated,
				},
				claimSpec{
					id:    "survey:C-02",
					value: 8.53,
					terms: []AccuracyTerm{independent(0.003)},
					date:  "2026-05-06",
					rank:  RankDeprecated,
				},
			),
		},
		{
			name: "reports nothing about a pair only one claim was written on",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.05)}, date: "2026-01-09"},
			),
		},
		{
			name:   "reports nothing about a model nothing is claimed in",
			claims: nil,
		},
		{
			name: "carries the claim which cannot win beside the one which does",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.05)}, date: "2026-01-09"},
				claimSpec{id: "survey:C-02", value: 8.53, date: "2026-05-06"},
			),
			expected: []conflictSpec{
				{
					subject:   resolutionSubject,
					predicate: "width",
					claims:    []ID{"survey:C-01", "survey:C-02"},
					resolved:  "survey:C-01",
				},
			},
		},
		{
			name: "reports a pair nothing rankable was said about as ambiguous",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, date: "2026-01-09"},
				claimSpec{id: "survey:C-02", value: 8.53, date: "2026-05-06"},
			),
			expected: []conflictSpec{
				{
					subject:   resolutionSubject,
					predicate: "width",
					claims:    []ID{"survey:C-01", "survey:C-02"},
					ambiguous: true,
				},
			},
		},
		{
			name: "keeps two claims under different predicates of one subject apart",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", predicate: "height", value: 2.7, date: "2026-01-09"},
				claimSpec{id: "survey:C-02", value: 8.53, date: "2026-05-06"},
			),
		},
		{
			name: "keeps one predicate claimed on two subjects apart",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", subject: "site:S-102", value: 8.5, date: "2026-01-09"},
				claimSpec{id: "survey:C-02", value: 8.53, date: "2026-05-06"},
			),
		},
		{
			name: "orders entries by subject and then by predicate",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", subject: "site:S-102", value: 8.5, date: "2026-01-09"},
				claimSpec{id: "survey:C-02", subject: "site:S-102", value: 8.53, date: "2026-05-06"},
				claimSpec{id: "survey:C-03", predicate: "height", value: 2.7, date: "2026-01-09"},
				claimSpec{id: "survey:C-04", predicate: "height", value: 2.74, date: "2026-05-06"},
				claimSpec{id: "survey:C-05", value: 8.5, date: "2026-01-09"},
				claimSpec{id: "survey:C-06", value: 8.53, date: "2026-05-06"},
			),
			expected: []conflictSpec{
				{
					subject:   "site:S-101",
					predicate: "height",
					claims:    []ID{"survey:C-03", "survey:C-04"},
					ambiguous: true,
				},
				{
					subject:   "site:S-101",
					predicate: "width",
					claims:    []ID{"survey:C-05", "survey:C-06"},
					ambiguous: true,
				},
				{
					subject:   "site:S-102",
					predicate: "width",
					claims:    []ID{"survey:C-01", "survey:C-02"},
					ambiguous: true,
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := registerOf(testCase.claims)

			assert.Equal(t, testCase.expected, got)

			// A pair with more than one live claim either has a best claim or
			// does not, and the entry says which. A third answer would leave a
			// caller with a disagreement it could not report.
			for conflict := range resolving(testCase.claims).Conflicts() {
				assert.NotEqual(t, conflict.Resolved(), conflict.Ambiguous(),
					"%s %s is neither resolved nor ambiguous", conflict.Subject(), conflict.Predicate())
			}
		})
	}
}

// TestClaimsConflictsCarryTheEvidence reads one entry the way something
// reporting the register would: the competing claims arrive with what was
// claimed, how well it was known, when it was obtained and where it came from,
// so that the disagreement can be described without going back to the model.
func TestClaimsConflictsCarryTheEvidence(t *testing.T) {
	claims := writtenClaims(
		claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.05)}, date: "2026-01-09"},
		claimSpec{id: "survey:C-02", value: 8.53, terms: []AccuracyTerm{independent(0.003)}, date: "2026-05-06"},
	)
	claims[0].source = "Plan set A-101, sheet 3"
	claims[0].method = "method:scaled-from-plan"
	claims[1].source = "As-built check AB-2026-009, Acme Surveys"
	claims[1].method = "method:total-station"

	register := slices.Collect(resolving(claims).Conflicts())
	require.Len(t, register, 1)

	competing := register[0].Claims()
	require.Len(t, competing, 2)

	expected := []struct {
		value    float64
		accuracy float64
		date     string
		source   string
		method   ID
	}{
		{value: 8.5, accuracy: 0.05, date: "2026-01-09", source: "Plan set A-101, sheet 3", method: "method:scaled-from-plan"},
		{
			value:    8.53,
			accuracy: 0.003,
			date:     "2026-05-06",
			source:   "As-built check AB-2026-009, Acme Surveys",
			method:   "method:total-station",
		},
	}

	for i, claim := range competing {
		value, isScalar := claim.Value().Scalar()
		require.True(t, isScalar)
		assert.Equal(t, expected[i].value, value)
		assert.Equal(t, Unit("m"), claim.Value().Unit())

		accuracy, ranked := claim.Accuracy()
		require.True(t, ranked)
		require.Len(t, accuracy.Terms, 1)
		assert.Equal(t, expected[i].accuracy, accuracy.Terms[0].Magnitude)

		assert.Equal(t, day(expected[i].date), claim.Date())
		assert.Equal(t, expected[i].source, claim.Source())
		assert.Equal(t, expected[i].method, claim.Method())

		// Every claim also says where it was written, which is what a report
		// points a reader at when the claim wrote no id.
		assert.NotZero(t, claim.Span().Start.Line)
	}

	// Re-ordering the claims of an entry re-orders nothing in the model, which
	// is what makes the entry a value a caller can keep.
	slices.Reverse(competing)
	assert.Equal(t, []ID{"survey:C-01", "survey:C-02"}, idsOf(register[0].Claims()))
}

// TestClaimsConflictsIsIndependentOfOrder says the register is a function of
// what the claims carry and of nothing about how they were loaded.
//
// It matters more here than it does for one resolution: a register is read as a
// list, and a list whose order came from the walk would diff against the last
// run every time a file was renamed, which is how a real change stops being
// visible in the noise.
func TestClaimsConflictsIsIndependentOfOrder(t *testing.T) {
	claims := writtenClaims(
		claimSpec{id: "survey:C-01", subject: "site:S-102", value: 8.5, terms: []AccuracyTerm{independent(0.05)}, date: "2026-01-09"},
		claimSpec{id: "survey:C-02", subject: "site:S-102", value: 8.53, terms: []AccuracyTerm{independent(0.003)}, date: "2026-05-06"},
		claimSpec{id: "survey:C-03", predicate: "height", value: 2.7, terms: []AccuracyTerm{independent(0.004)}, date: "2026-05-06"},
		claimSpec{id: "survey:C-04", predicate: "height", value: 2.74, terms: []AccuracyTerm{independent(0.004)}, date: "2026-05-06"},
		claimSpec{id: "survey:C-05", value: 8.5, date: "2026-01-09"},
		claimSpec{id: "survey:C-06", value: 8.53, terms: []AccuracyTerm{independent(0.02)}, date: "2026-05-06"},
		claimSpec{
			id:    "survey:C-07",
			value: 8.49,
			terms: []AccuracyTerm{independent(0.001)},
			date:  "2026-06-01",
			rank:  RankDeprecated,
		},
	)

	first := registerOf(claims)
	require.Len(t, first, 3)

	// A fixed seed, so that a failure is reproducible: what is being asserted
	// is that every order gives one register, and a test which could not be run
	// again on the order which broke it would report that badly.
	shuffle := rand.New(rand.NewPCG(0x64666361, 0x64))

	for range 200 {
		shuffled := slices.Clone(claims)
		shuffle.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})

		assert.Equal(t, first, registerOf(shuffled), "shuffled %v", idsOf(shuffled))
	}
}

// TestClaimsConflictsAreComputedRatherThanStored is the point of the register
// being a traversal.
//
// A stored flag is current until somebody forgets to clear it, and the way it
// gets forgotten is never deliberate. Nothing in the format writes one, nothing
// loaded from a file holds one, and asking twice reads the claims twice — so
// the register can only go quiet when the claims themselves stop disagreeing.
func TestClaimsConflictsAreComputedRatherThanStored(t *testing.T) {
	t.Run("no form of the format carries a conflict of its own", func(t *testing.T) {
		for tag := range forms().permittedIn {
			assert.NotContains(t, strings.ToLower(tag), "conflict",
				"the format knows a %s tag, which would be a conflict somebody has to maintain", tag)
		}
	})

	t.Run("nothing loaded from a file holds a conflict field", func(t *testing.T) {
		loaded := []reflect.Type{
			reflect.TypeOf(Claim{}),
			reflect.TypeOf(Claims{}),
			reflect.TypeOf(Value{}),
			reflect.TypeOf(Accuracy{}),
			reflect.TypeOf(SemanticNode{}),
			reflect.TypeOf(Nodes{}),
			reflect.TypeOf(Resolution{}),
		}

		for _, typ := range loaded {
			for i := range typ.NumField() {
				assert.NotContains(t, strings.ToLower(typ.Field(i).Name), "conflict",
					"%s holds a %s field, which is a register nothing recomputes", typ.Name(), typ.Field(i).Name)
			}
		}
	})

	t.Run("reads the claims again rather than answering from the first walk", func(t *testing.T) {
		claims := writtenClaims(
			claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.05)}, date: "2026-01-09"},
			claimSpec{id: "survey:C-02", value: 8.53, terms: []AccuracyTerm{independent(0.003)}, date: "2026-05-06"},
		)
		indexed := resolving(claims)

		require.Len(t, slices.Collect(indexed.Conflicts()), 1)

		// Deprecating the claim which lost is the one thing the format offers
		// for silencing a conflict, and it is a falsifiable statement about the
		// claim rather than a flag on the register.
		claims[0].rank = RankDeprecated

		assert.Empty(t, slices.Collect(indexed.Conflicts()))
	})
}

// TestClaimsConflictsLoadedModel walks the register of a loaded model, which is
// what says it reads what a file writes rather than what a test built.
func TestClaimsConflictsLoadedModel(t *testing.T) {
	t.Run("reports the disagreement a clean model holds without calling it a fault", func(t *testing.T) {
		registry := mustLoadRegistry(t, claimFixture("valid"))

		claims, diags := LoadClaims(claimFixture("valid"), registry)

		// The fixture states the room's width twice and disagrees with itself,
		// and it loads clean: a conflict is a finding rather than a failure.
		require.Empty(t, diags, "the valid fixture loads clean")

		register := slices.Collect(claims.Conflicts())
		require.Len(t, register, 1)

		assert.Equal(t, conflictSpec{
			subject:   "site:S-101",
			predicate: "width",
			claims:    []ID{"survey:C-0210", ""},
			resolved:  "",
		}, entryOf(register[0]))

		// The winning claim wrote no id of its own, which is the ordinary case;
		// what names it is where it was written.
		winner, resolved := register[0].Resolution().Claim()
		require.True(t, resolved)
		assert.Equal(t, "As-built check AB-2026-009, Acme Surveys", winner.Source())

		// The vertex is claimed about twice as well, and one of the two is
		// deprecated — so it is not in the register, which is the whole of how a
		// conflict is silenced.
		assert.Equal(t, 2, len(slices.Collect(claims.Of("geom:V-02"))))
	})

	t.Run("reports a strict predicate's ambiguity rather than failing on it", func(t *testing.T) {
		registry := mustLoadRegistry(t, claimFixture("strict"))

		claims, diags := LoadClaims(claimFixture("strict"), registry)
		require.Empty(t, diags, "the strict fixture loads clean")

		var got []conflictSpec
		for conflict := range claims.Conflicts() {
			got = append(got, entryOf(conflict))
		}

		// `bearing` is declared strict, which makes asking Resolve for one
		// number a failure. Asking what the model says is not: the register is
		// where that ambiguity is meant to be visible.
		assert.Equal(t, []conflictSpec{
			{
				subject:   "site:S-101",
				predicate: "bearing",
				claims:    []ID{"survey:C-0310", "survey:C-0311"},
				ambiguous: true,
			},
			{
				subject:   "site:S-101",
				predicate: "height",
				claims:    []ID{"survey:C-0314", "survey:C-0315"},
				resolved:  "survey:C-0315",
			},
			{
				subject:   "site:S-101",
				predicate: "width",
				claims:    []ID{"survey:C-0312", "survey:C-0313"},
				ambiguous: true,
			},
		}, got)

		_, err := claims.Resolve("site:S-101", "bearing", registry)
		require.Error(t, err, "the same ambiguity is a failure where a caller asked for one number")
	})
}
