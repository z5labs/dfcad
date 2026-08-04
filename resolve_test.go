// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"math/rand/v2"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolutionSubject is the one thing every claim of these tests is about.
//
// Which subject it is does not matter, and a second one would only be a second
// place to look; what the tests vary is what is claimed about it.
const resolutionSubject ID = "site:S-101"

// claimSpec is one claim written for a resolution test: what resolution reads,
// and nothing else.
//
// The provenance a real claim carries — its source, its method — is left out
// because resolution deliberately does not consult it. Writing it here would
// suggest it did.
type claimSpec struct {
	// id is the claim's own id, which every claim of these tests writes so that
	// the answer can be named.
	id ID

	// subject is the thing it is about, which is [resolutionSubject] unless the
	// case is about more than one thing being claimed about.
	subject ID

	// predicate is the tag it was written under, which is `width` unless the
	// case is about asking under a different one.
	predicate string

	// value is the scalar it claims.
	value float64

	// terms are its accuracy, which is empty for an unrankable claim.
	terms []AccuracyTerm

	// date is the RFC 3339 full-date it was obtained on.
	date string

	// rank is its rank, which is [RankNormal] unless written.
	rank Rank

	// supersededBy is the id of the claim it names as its replacement, which
	// only a deprecated claim writes.
	supersededBy ID
}

// writtenClaims builds the claims of one test as though they had been written
// down the one file, in the order they are listed.
//
// Each claim gets a position of its own, which is what makes the order the
// candidates come back in a property of where they were written rather than of
// the order they were read: the tests below shuffle the built claims, and the
// positions travel with them.
func writtenClaims(specs ...claimSpec) []*Claim {
	out := make([]*Claim, 0, len(specs))

	for i, spec := range specs {
		predicate := spec.predicate
		if predicate == "" {
			predicate = "width"
		}

		subject := spec.subject
		if subject == "" {
			subject = resolutionSubject
		}

		rank := spec.rank
		if rank == "" {
			rank = RankNormal
		}

		claim := &Claim{
			id:           spec.id,
			subject:      subject,
			predicate:    predicate,
			value:        Value{shape: ShapeScalar, number: spec.value, unit: "m"},
			date:         day(spec.date),
			rank:         rank,
			supersededBy: spec.supersededBy,
			span:         writtenAt(i),
		}

		if len(spec.terms) > 0 {
			claim.accuracy = Accuracy{Terms: spec.terms, Span: writtenAt(i)}
			claim.hasAccuracy = true
		}

		out = append(out, claim)
	}

	return out
}

// writtenAt is the span of the claim written at the nth line of the notional
// file these tests read from.
func writtenAt(n int) Span {
	const path = "entities/level-1.dfc"

	return Span{
		Start: Position{Path: path, Line: n + 1, Column: 3, Offset: n * 100},
		End:   Position{Path: path, Line: n + 1, Column: 40, Offset: n*100 + 37},
	}
}

// day reads one RFC 3339 full-date, which is the one spelling of a date the
// format has.
func day(written string) time.Time {
	if written == "" {
		return time.Time{}
	}

	date, err := time.Parse(time.DateOnly, written)
	if err != nil {
		panic("resolve_test: " + written + " is not a date")
	}

	return date
}

// independent is one independent accuracy term in metres.
func independent(magnitude float64) AccuracyTerm {
	return independentIn(magnitude, "m")
}

// independentIn is one independent accuracy term in the unit it was written in.
func independentIn(magnitude float64, unit Unit) AccuracyTerm {
	return AccuracyTerm{Kind: TermIndependent, Magnitude: magnitude, Unit: unit}
}

// systematic is one systematic accuracy term in metres, shared through source.
func systematic(magnitude float64, source ID) AccuracyTerm {
	return AccuracyTerm{Kind: TermSystematic, Magnitude: magnitude, Unit: "m", Source: source}
}

// resolving indexes claims the way a load would, so that they can be resolved.
func resolving(claims []*Claim) *Claims {
	out := &Claims{
		byID:      make(map[ID]*Claim),
		bySubject: make(map[ID][]*Claim),
	}

	for _, claim := range claims {
		out.inOrder = append(out.inOrder, claim)
		if claim.id != "" {
			out.byID[claim.id] = claim
		}
		out.bySubject[claim.subject] = append(out.bySubject[claim.subject], claim)
	}

	// A load indexes the supersessions once it has read every file, and a
	// collection built without that index answers a traversal with nothing
	// rather than with what the claims say.
	out.link()

	return out
}

// idsOf names claims by their ids, which is what a test asserts on: comparing
// pointers says which claims came back but not which ones a failure was missing.
func idsOf(claims []*Claim) []ID {
	var out []ID
	for _, claim := range claims {
		id, _ := claim.ID()
		out = append(out, id)
	}
	return out
}

func TestClaimsResolve(t *testing.T) {
	testCases := []struct {
		name       string
		claims     []*Claim
		resolved   ID
		ambiguous  bool
		candidates []ID
		reason     Reason
	}{
		{
			name: "picks the claim whose accuracy is smallest",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.05)}, date: "2026-01-09"},
				claimSpec{id: "survey:C-02", value: 8.53, terms: []AccuracyTerm{independent(0.003)}, date: "2026-05-06"},
			),
			resolved:   "survey:C-02",
			candidates: []ID{"survey:C-02"},
			reason:     ReasonAccuracy,
		},
		{
			name: "prefers the more accurate claim over the more recent one",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.53, terms: []AccuracyTerm{independent(0.003)}, date: "2023-06-02"},
				claimSpec{id: "survey:C-02", value: 8.5, terms: []AccuracyTerm{independent(0.05)}, date: "2024-11-19"},
			),
			resolved:   "survey:C-01",
			candidates: []ID{"survey:C-01"},
			reason:     ReasonAccuracy,
		},
		{
			name: "breaks an accuracy tie by the most recent date",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.004)}, date: "2026-01-09"},
				claimSpec{id: "survey:C-02", value: 8.53, terms: []AccuracyTerm{independent(0.004)}, date: "2026-05-06"},
			),
			resolved:   "survey:C-02",
			candidates: []ID{"survey:C-02"},
			reason:     ReasonRecency,
		},
		{
			name: "reports both claims where neither accuracy nor date separates them",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.004)}, date: "2026-05-06"},
				claimSpec{id: "survey:C-02", value: 8.53, terms: []AccuracyTerm{independent(0.004)}, date: "2026-05-06"},
			),
			ambiguous:  true,
			candidates: []ID{"survey:C-01", "survey:C-02"},
			reason:     ReasonAmbiguous,
		},
		{
			name: "passes over a deprecated claim however good it is",
			claims: writtenClaims(
				claimSpec{
					id:    "survey:C-01",
					value: 8.5,
					terms: []AccuracyTerm{independent(0.001)},
					date:  "2026-05-06",
					rank:  RankDeprecated,
				},
				claimSpec{id: "survey:C-02", value: 8.53, terms: []AccuracyTerm{independent(0.05)}, date: "2026-01-09"},
			),
			resolved:   "survey:C-02",
			candidates: []ID{"survey:C-02"},
			reason:     ReasonOnly,
		},
		{
			name: "resolves nothing where every claim has been deprecated",
			claims: writtenClaims(
				claimSpec{
					id:    "survey:C-01",
					value: 8.5,
					terms: []AccuracyTerm{independent(0.004)},
					date:  "2026-05-06",
					rank:  RankDeprecated,
				},
			),
			reason: ReasonUnclaimed,
		},
		{
			name: "returns the unrankable claims as candidates where nothing rankable exists",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, date: "2026-01-09"},
				claimSpec{id: "survey:C-02", value: 8.53, date: "2026-05-06"},
			),
			ambiguous:  true,
			candidates: []ID{"survey:C-01", "survey:C-02"},
			reason:     ReasonAmbiguous,
		},
		{
			name: "returns one unrankable claim as a candidate without letting it win",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, date: "2026-01-09"},
			),
			candidates: []ID{"survey:C-01"},
			reason:     ReasonUnranked,
		},
		{
			name: "keeps an unrankable claim from winning against a rankable one",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.05)}, date: "2023-06-02"},
				claimSpec{id: "survey:C-02", value: 8.53, date: "2026-05-06"},
			),
			resolved:   "survey:C-01",
			candidates: []ID{"survey:C-01"},
			reason:     ReasonAccuracy,
		},
		{
			name: "ranks a claim by its independent and systematic terms together",
			claims: writtenClaims(
				// One term of 0.02 against 0.01 and 0.03 combined, which is
				// 0.0316: the claim with the smaller independent term is the
				// worse of the two once the term it shares is counted.
				claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.02)}, date: "2026-01-09"},
				claimSpec{
					id:    "survey:C-02",
					value: 8.53,
					terms: []AccuracyTerm{independent(0.01), systematic(0.03, "survey:CP-3")},
					date:  "2026-05-06",
				},
			),
			resolved:   "survey:C-01",
			candidates: []ID{"survey:C-01"},
			reason:     ReasonAccuracy,
		},
		{
			name: "leaves a claim whose accuracy mixes units unrankable rather than converting it",
			claims: writtenClaims(
				claimSpec{
					id:    "survey:C-01",
					value: 8.5,
					terms: []AccuracyTerm{independent(0.001), independentIn(3.0, "mm")},
					date:  "2026-05-06",
				},
				claimSpec{id: "survey:C-02", value: 8.53, terms: []AccuracyTerm{independent(0.05)}, date: "2026-01-09"},
			),
			resolved:   "survey:C-02",
			candidates: []ID{"survey:C-02"},
			reason:     ReasonAccuracy,
		},
		{
			name: "reports accuracies in different units as tied rather than converting between them",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.05)}, date: "2026-01-09"},
				claimSpec{
					id:    "survey:C-02",
					value: 8.53,
					terms: []AccuracyTerm{independentIn(3.0, "mm")},
					date:  "2026-05-06",
				},
			),
			ambiguous:  true,
			candidates: []ID{"survey:C-01", "survey:C-02"},
			reason:     ReasonAmbiguous,
		},
		{
			name:   "resolves nothing about a subject nothing claims",
			claims: nil,
			reason: ReasonUnclaimed,
		},
		{
			name: "considers only the claims written under the predicate asked for",
			claims: writtenClaims(
				claimSpec{
					id:        "survey:C-01",
					predicate: "height",
					value:     2.7,
					terms:     []AccuracyTerm{independent(0.001)},
					date:      "2026-05-06",
				},
				claimSpec{id: "survey:C-02", value: 8.53, terms: []AccuracyTerm{independent(0.05)}, date: "2026-01-09"},
			),
			resolved:   "survey:C-02",
			candidates: []ID{"survey:C-02"},
			reason:     ReasonOnly,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := resolving(testCase.claims).Resolve(resolutionSubject, "width", nil)
			require.NoError(t, err)

			assert.Equal(t, resolutionSubject, got.Subject())
			assert.Equal(t, "width", got.Predicate())

			assert.Equal(t, testCase.resolved != "", got.Resolved())
			assert.Equal(t, testCase.ambiguous, got.Ambiguous())
			assert.Equal(t, testCase.candidates, idsOf(got.Candidates()))

			// The reason is which step of the rule produced the outcome, and
			// it is asserted beside the outcome so that the two cannot drift:
			// a resolution which says it won on accuracy and did not is worse
			// than one which says nothing.
			assert.Equal(t, testCase.reason, got.Reason())

			claim, resolved := got.Claim()
			require.Equal(t, testCase.resolved != "", resolved)
			if !resolved {
				assert.Nil(t, claim)

				_, traced := got.ClaimID()
				assert.False(t, traced)

				_, valued := got.Value()
				assert.False(t, valued)

				return
			}

			// The answer names the claim it came from, so that a number can be
			// taken back to the evidence for it without a second lookup.
			traced, wrote := got.ClaimID()
			require.True(t, wrote)
			assert.Equal(t, testCase.resolved, traced)

			id, _ := claim.ID()
			assert.Equal(t, testCase.resolved, id)

			value, valued := got.Value()
			require.True(t, valued)
			assert.Equal(t, claim.Value(), value)
		})
	}
}

// TestClaimsResolveIsIndependentOfOrder is the assertion the whole layer rests
// on: the answer is a function of what the claims carry, and of nothing about
// how they were loaded.
//
// Resolution exists whether or not it is written down, and unwritten it becomes
// whatever the loader did — which is file order, which changes the day somebody
// sorts a directory. Shuffling the claims is the cheapest available proof that
// it did not become that here. It covers the candidate ordering as well as the
// winner, because an ambiguous answer whose candidates came back in load order
// would be a diff which changed for no reason.
func TestClaimsResolveIsIndependentOfOrder(t *testing.T) {
	testCases := []struct {
		name   string
		claims []*Claim
	}{
		{
			name: "with one claim winning outright",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.05)}, date: "2026-01-09"},
				claimSpec{id: "survey:C-02", value: 8.53, terms: []AccuracyTerm{independent(0.003)}, date: "2026-05-06"},
				claimSpec{id: "survey:C-03", value: 8.51, terms: []AccuracyTerm{independent(0.02)}, date: "2026-03-14"},
				claimSpec{id: "survey:C-04", value: 8.6, date: "2026-04-02"},
				claimSpec{
					id:    "survey:C-05",
					value: 8.49,
					terms: []AccuracyTerm{independent(0.001)},
					date:  "2026-05-06",
					rank:  RankDeprecated,
				},
			),
		},
		{
			name: "with four claims tied at the top",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.004)}, date: "2026-05-06"},
				claimSpec{id: "survey:C-02", value: 8.53, terms: []AccuracyTerm{independent(0.004)}, date: "2026-05-06"},
				claimSpec{id: "survey:C-03", value: 8.51, terms: []AccuracyTerm{independent(0.004)}, date: "2026-05-06"},
				claimSpec{id: "survey:C-04", value: 8.52, terms: []AccuracyTerm{independent(0.004)}, date: "2026-05-06"},
				claimSpec{id: "survey:C-05", value: 8.6, terms: []AccuracyTerm{independent(0.05)}, date: "2026-06-01"},
			),
		},
		{
			name: "with nothing rankable said at all",
			claims: writtenClaims(
				claimSpec{id: "survey:C-01", value: 8.5, date: "2026-01-09"},
				claimSpec{id: "survey:C-02", value: 8.53, date: "2026-05-06"},
				claimSpec{id: "survey:C-03", value: 8.51, date: "2026-03-14"},
			),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			first, err := resolving(testCase.claims).Resolve(resolutionSubject, "width", nil)
			require.NoError(t, err)

			// A fixed seed, so that a failure is reproducible: what is being
			// asserted is that every order gives one answer, and a test which
			// could not be run again on the order which broke it would report
			// that badly.
			shuffle := rand.New(rand.NewPCG(0x64666361, 0x64))

			for range 200 {
				shuffled := slices.Clone(testCase.claims)
				shuffle.Shuffle(len(shuffled), func(i, j int) {
					shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
				})

				got, err := resolving(shuffled).Resolve(resolutionSubject, "width", nil)
				require.NoError(t, err)

				assert.Equal(t, first, got, "shuffled %v", idsOf(shuffled))
			}
		})
	}
}

// TestClaimsResolveStrict reads the one thing the registry decides about a
// resolution.
//
// The fixture's `bearing` and `width` claims are tied in exactly the same way,
// and the predicates differ only in the flag, so what separates a report from a
// failure here is the registry and nothing about the claims.
func TestClaimsResolveStrict(t *testing.T) {
	registry := mustLoadRegistry(t, claimFixture("strict"))

	claims, diags := LoadClaims(claimFixture("strict"), registry)
	require.Empty(t, diags, "the strict fixture loads clean")

	t.Run("fails where a predicate the registry declares strict is ambiguous", func(t *testing.T) {
		resolution, err := claims.Resolve(resolutionSubject, "bearing", registry)

		var ambiguous AmbiguousResolutionError
		require.ErrorAs(t, err, &ambiguous)
		assert.Equal(t, resolutionSubject, ambiguous.Subject)
		assert.Equal(t, "bearing", ambiguous.Predicate)
		assert.Equal(t, []ID{"survey:C-0310", "survey:C-0311"}, idsOf(ambiguous.Candidates))

		// The resolution comes back beside the error, because a caller
		// reporting the failure wants the tied claims and not only the fact
		// that there were some.
		assert.True(t, resolution.Ambiguous())
		assert.False(t, resolution.Resolved())
		assert.Equal(t, []ID{"survey:C-0310", "survey:C-0311"}, idsOf(resolution.Candidates()))
	})

	t.Run("reports the same ambiguity where the predicate is not strict", func(t *testing.T) {
		resolution, err := claims.Resolve(resolutionSubject, "width", registry)

		require.NoError(t, err)
		assert.True(t, resolution.Ambiguous())
		assert.Equal(t, []ID{"survey:C-0312", "survey:C-0313"}, idsOf(resolution.Candidates()))
	})

	t.Run("answers a strict predicate which resolves", func(t *testing.T) {
		resolution, err := claims.Resolve(resolutionSubject, "height", registry)

		require.NoError(t, err)
		require.True(t, resolution.Resolved())

		id, wrote := resolution.ClaimID()
		require.True(t, wrote)
		assert.Equal(t, ID("survey:C-0315"), id)
	})
}

// TestClaimsResolveLoadedModel resolves the claims of a loaded model, which is
// what says the rule reads what a file writes rather than what a test built.
//
// It is also where the two ends of traceability meet: the winning width claim
// wrote no id of its own, and the winning position claim did, so an answer can
// be traced back to a named claim or to the place one was written.
func TestClaimsResolveLoadedModel(t *testing.T) {
	registry := mustLoadRegistry(t, claimFixture("valid"))

	claims, diags := LoadClaims(claimFixture("valid"), registry)
	require.Empty(t, diags, "the valid fixture loads clean")

	t.Run("resolves to the as-built measurement rather than the plan it was scaled off", func(t *testing.T) {
		resolution, err := claims.Resolve("site:S-101", "width", registry)
		require.NoError(t, err)
		require.True(t, resolution.Resolved())

		value, ok := resolution.Value()
		require.True(t, ok)

		width, isScalar := value.Scalar()
		require.True(t, isScalar)
		assert.Equal(t, 8.53, width)

		// The claim which won wrote no id, which is the ordinary case: a claim
		// needs a name only where something references it. The answer is still
		// traceable, through the claim it came from and the span on it.
		_, wrote := resolution.ClaimID()
		assert.False(t, wrote)

		claim, _ := resolution.Claim()
		assert.Equal(t, "As-built check AB-2026-009, Acme Surveys", claim.Source())
		assert.Equal(t, claimFixture("valid"), filepath.Dir(claim.Span().Start.Path))
	})

	t.Run("names the claim which won where it wrote an id", func(t *testing.T) {
		resolution, err := claims.Resolve("geom:V-02", "position", registry)
		require.NoError(t, err)
		require.True(t, resolution.Resolved())

		// The deprecated shot is not considered, so the one which replaced it
		// is the only candidate rather than the better of two.
		id, wrote := resolution.ClaimID()
		require.True(t, wrote)
		assert.Equal(t, ID("survey:C-0181"), id)
		assert.Equal(t, []ID{"survey:C-0181"}, idsOf(resolution.Candidates()))
	})

	t.Run("answers an unrankable claim as a candidate rather than as a value", func(t *testing.T) {
		resolution, err := claims.Resolve("site:S-101", "occupancy", registry)
		require.NoError(t, err)

		assert.False(t, resolution.Resolved())
		assert.False(t, resolution.Ambiguous())
		require.Len(t, resolution.Candidates(), 1)
		assert.Equal(t, ID("method:assumed"), resolution.Candidates()[0].Method())
	})

	t.Run("resolves nothing about a predicate the model says nothing under", func(t *testing.T) {
		resolution, err := claims.Resolve("site:S-101", "position", registry)

		require.NoError(t, err)
		assert.False(t, resolution.Resolved())
		assert.False(t, resolution.Ambiguous())
		assert.Empty(t, resolution.Candidates())
	})
}

// TestResolutionCandidatesAreACopy checks that the candidates a resolution hands
// out are the caller's, for the reason a coordinate's components are: sorting
// them is a thing a caller does, and it must sort nothing in the answer.
func TestResolutionCandidatesAreACopy(t *testing.T) {
	claims := resolving(writtenClaims(
		claimSpec{id: "survey:C-01", value: 8.5, terms: []AccuracyTerm{independent(0.004)}, date: "2026-05-06"},
		claimSpec{id: "survey:C-02", value: 8.53, terms: []AccuracyTerm{independent(0.004)}, date: "2026-05-06"},
	))

	resolution, err := claims.Resolve(resolutionSubject, "width", nil)
	require.NoError(t, err)

	candidates := resolution.Candidates()
	require.Len(t, candidates, 2)
	slices.Reverse(candidates)

	assert.Equal(t, []ID{"survey:C-01", "survey:C-02"}, idsOf(resolution.Candidates()))
}
