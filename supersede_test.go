// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supersededClaims loads the fixture whose supersessions all hold together,
// which is what the traversals are asked of.
func supersededClaims(t *testing.T) *Claims {
	t.Helper()

	claims, rendered := loadClaimFixture(t, "supersession")
	require.Empty(t, rendered, "a supersession which holds together loads clean")

	return claims
}

// claimOf reads one claim of a load by its id, failing the test where the model
// holds none.
func claimOf(t *testing.T, claims *Claims, id ID) *Claim {
	t.Helper()

	claim, ok := claims.Claim(id)
	require.True(t, ok, "the model holds the claim %s", id)

	return claim
}

// TestClaimsReplacement walks one step forward along a supersession, which is
// the direction the reference is written in.
func TestClaimsReplacement(t *testing.T) {
	testCases := []struct {
		name     string
		claim    ID
		expected ID
	}{
		{
			name:     "gives the claim a deprecation names as its replacement",
			claim:    "survey:C-0100",
			expected: "survey:C-0104",
		},
		{
			name:     "gives the next link where the replacement was itself replaced",
			claim:    "survey:C-0104",
			expected: "survey:C-0181",
		},
		{
			name:  "gives nothing for a claim which is still asserted",
			claim: "survey:C-0181",
		},
	}

	claims := supersededClaims(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			replacement, ok := claims.Replacement(claimOf(t, claims, testCase.claim))

			if testCase.expected == "" {
				assert.False(t, ok)
				assert.Nil(t, replacement)
				return
			}

			require.True(t, ok)
			id, _ := replacement.ID()
			assert.Equal(t, testCase.expected, id)
		})
	}
}

// TestClaimsReplaced walks one step back along a supersession, which is the
// direction nothing is written in.
func TestClaimsReplaced(t *testing.T) {
	testCases := []struct {
		name     string
		claim    ID
		expected []ID
	}{
		{
			name:     "gives the claim which named this one as its replacement",
			claim:    "survey:C-0104",
			expected: []ID{"survey:C-0100"},
		},
		{
			name:     "gives every claim one replacement stands in place of, in written order",
			claim:    "survey:C-0212",
			expected: []ID{"survey:C-0210", "survey:C-0211"},
		},
		{
			name:  "gives nothing for a claim which replaced nothing",
			claim: "survey:C-0100",
		},
	}

	claims := supersededClaims(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			replaced := slices.Collect(claims.Replaced(claimOf(t, claims, testCase.claim)))

			assert.Equal(t, testCase.expected, idsOf(replaced))
		})
	}
}

// TestClaimsCurrentWalksToWhatIsAsserted checks the forward walk of a chain
// three links long, which is the whole of tracing a value read out of an old
// report forward to what the model says now.
func TestClaimsCurrentWalksToWhatIsAsserted(t *testing.T) {
	testCases := []struct {
		name     string
		claim    ID
		expected ID
	}{
		{
			name:     "walks a three-link chain to the claim which is still asserted",
			claim:    "survey:C-0100",
			expected: "survey:C-0181",
		},
		{
			name:     "walks the last deprecated link to the same claim",
			claim:    "survey:C-0104",
			expected: "survey:C-0181",
		},
		{
			name:     "gives back a claim nothing replaced",
			claim:    "survey:C-0181",
			expected: "survey:C-0181",
		},
	}

	claims := supersededClaims(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			current, ok := claims.Current(claimOf(t, claims, testCase.claim))

			require.True(t, ok)
			id, _ := current.ID()
			assert.Equal(t, testCase.expected, id)
		})
	}
}

// TestClaimsHistoryWalksBackThroughEverythingReplaced is the other direction of
// the same chain: from the value the model asserts, back through every claim it
// stands in place of.
func TestClaimsHistoryWalksBackThroughEverythingReplaced(t *testing.T) {
	testCases := []struct {
		name     string
		claim    ID
		expected []ID
	}{
		{
			name:     "walks a three-link chain back through both claims it replaced",
			claim:    "survey:C-0181",
			expected: []ID{"survey:C-0104", "survey:C-0100"},
		},
		{
			name:     "walks back through two claims one replacement stands in place of",
			claim:    "survey:C-0212",
			expected: []ID{"survey:C-0210", "survey:C-0211"},
		},
		{
			name:  "gives nothing for the claim at the far end of the chain",
			claim: "survey:C-0100",
		},
	}

	claims := supersededClaims(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			history := slices.Collect(claims.History(claimOf(t, claims, testCase.claim)))

			assert.Equal(t, testCase.expected, idsOf(history))
		})
	}
}

// TestClaimsCurrentWithoutAChainWhichEnds is its own function because it asks a
// different question of a different model: not which claim the walk reaches,
// but that it reaches none and says so.
//
// Every model below is one a load reports on, and every one of them is still
// returned — a caller reporting on a model wants to say what is in it as well
// as what is wrong with it, and it is these traversals which would otherwise
// spin on one.
func TestClaimsCurrentWithoutAChainWhichEnds(t *testing.T) {
	testCases := []struct {
		name   string
		claims []*Claim
	}{
		{
			name: "reports no current claim where the replacement is one the model does not hold",
			claims: writtenClaims(
				claimSpec{id: "survey:C-0100", value: 8.5, rank: RankDeprecated, supersededBy: "survey:C-0212"},
			),
		},
		{
			name: "reports no current claim where the deprecation named nothing",
			claims: writtenClaims(
				claimSpec{id: "survey:C-0100", value: 8.5, rank: RankDeprecated},
			),
		},
		{
			name: "reports no current claim where the chain is a cycle",
			claims: writtenClaims(
				claimSpec{id: "survey:C-0100", value: 8.5, rank: RankDeprecated, supersededBy: "survey:C-0104"},
				claimSpec{id: "survey:C-0104", value: 8.6, rank: RankDeprecated, supersededBy: "survey:C-0100"},
			),
		},
		{
			name: "reports no current claim where the claim replaced itself",
			claims: writtenClaims(
				claimSpec{id: "survey:C-0100", value: 8.5, rank: RankDeprecated, supersededBy: "survey:C-0100"},
			),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			claims := resolving(testCase.claims)

			current, ok := claims.Current(testCase.claims[0])

			assert.False(t, ok)
			assert.Nil(t, current)
		})
	}
}

// TestSupersessionFollowsOnlyARetraction is its own function because it is
// about a model which is wrong rather than about walking one which is not: a
// claim which is still asserted named a replacement anyway, which specification
// section 6.5 forbids.
//
// The traversals do not read it. A live claim has not been replaced, so
// following the reference would let a file put one asserted claim behind
// another by writing a child the format does not permit it — and the walk
// forwards and the walk backwards would then have to agree about an edge
// neither of them should see.
func TestSupersessionFollowsOnlyARetraction(t *testing.T) {
	written := writtenClaims(
		claimSpec{id: "survey:C-0210", value: 8.5, supersededBy: "survey:C-0212"},
		claimSpec{id: "survey:C-0212", value: 8.53},
	)

	claims := resolving(written)

	replacement, ok := claims.Replacement(written[0])
	assert.False(t, ok, "a claim which is still asserted has no replacement")
	assert.Nil(t, replacement)

	current, ok := claims.Current(written[0])
	require.True(t, ok, "a claim which is still asserted is its own current claim")
	assert.Same(t, written[0], current)

	assert.Empty(t, slices.Collect(claims.Replaced(written[1])), "a live claim is nothing's predecessor")
	assert.Empty(t, slices.Collect(claims.History(written[1])))
}

// TestSupersessionFollowsOnlyACompleteRetraction is the other half of the same
// rule, from the other direction: a deprecation which named nothing is not a
// link either, so the claim it was written on is in no chain.
func TestSupersessionFollowsOnlyACompleteRetraction(t *testing.T) {
	written := writtenClaims(
		claimSpec{id: "survey:C-0210", value: 8.5, rank: RankDeprecated},
	)

	claims := resolving(written)

	replacement, ok := claims.Replacement(written[0])
	assert.False(t, ok)
	assert.Nil(t, replacement)
}

// TestClaimsHistoryTerminatesOnACycle is its own function for the reason above:
// what it asserts is that the walk ends, and what it comes back with is
// secondary.
func TestClaimsHistoryTerminatesOnACycle(t *testing.T) {
	written := writtenClaims(
		claimSpec{id: "survey:C-0100", value: 8.5, rank: RankDeprecated, supersededBy: "survey:C-0104"},
		claimSpec{id: "survey:C-0104", value: 8.6, rank: RankDeprecated, supersededBy: "survey:C-0100"},
	)

	claims := resolving(written)

	assert.Equal(t, []ID{"survey:C-0104"}, idsOf(slices.Collect(claims.History(written[0]))))
	assert.Equal(t, []ID{"survey:C-0100"}, idsOf(slices.Collect(claims.History(written[1]))))
}

// TestClaimsHistoryStopsEarly checks that a caller which breaks out of the walk
// stops it, which is the whole reason the traversal is a sequence rather than a
// slice.
func TestClaimsHistoryStopsEarly(t *testing.T) {
	claims := supersededClaims(t)

	var walked []ID
	for claim := range claims.History(claimOf(t, claims, "survey:C-0181")) {
		id, _ := claim.ID()
		walked = append(walked, id)
		break
	}

	assert.Equal(t, []ID{"survey:C-0104"}, walked)
}

// TestDeprecatedClaimsAreExcludedFromResolutionAndStillRetrievable is the
// property the whole of supersession rests on: a claim which was corrected is
// out of the answer and still in the file.
//
// Deleting it would be the alternative, and deleting it destroys the record of
// why the number changed — which is the thing the model exists to hold.
func TestDeprecatedClaimsAreExcludedFromResolutionAndStillRetrievable(t *testing.T) {
	claims := supersededClaims(t)

	registry := mustLoadRegistry(t, claimFixture("supersession"))

	resolution, err := claims.Resolve("geom:V-02", "position", registry)
	require.NoError(t, err)

	winner, resolved := resolution.Claim()
	require.True(t, resolved)

	id, _ := winner.ID()
	assert.Equal(t, ID("survey:C-0181"), id)
	assert.Equal(t, []ID{"survey:C-0181"}, idsOf(resolution.Candidates()))

	// Every claim of the chain is still read back, by its own id and among the
	// claims written on the vertex, in the order they were written.
	assert.Equal(t, []ID{"survey:C-0100", "survey:C-0104", "survey:C-0181"},
		idsOf(slices.Collect(claims.Under("geom:V-02", "position"))))

	for _, id := range []ID{"survey:C-0100", "survey:C-0104"} {
		claim := claimOf(t, claims, id)

		assert.Equal(t, RankDeprecated, claim.Rank())

		superseded, isSuperseded := claim.SupersededBy()
		assert.True(t, isSuperseded)
		assert.NotEmpty(t, superseded)
	}
}

// TestLoadClaimsReportsAnUnreadableReplacementOnce checks that a deprecation
// whose replacement is not an id is reported as that and not also as a
// deprecation which named nothing.
//
// The two would be one mistake written twice. The author wrote the child, and
// what is wrong with it is what was put inside it — so the diagnostic is the
// one which says a claim id was expected there, and telling them in the next
// line that they left the child out is a sentence about a file nobody wrote.
func TestLoadClaimsReportsAnUnreadableReplacementOnce(t *testing.T) {
	const registry = `(project (globalid-namespace "https://example.org/models/supersession"))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
(namespace survey (description "Claim ids issued by Acme Surveys."))
(predicate width (unit m) (shape scalar) (description "How wide the thing is."))
`

	const written = `(node site:S-101
  (width
    (id survey:C-0210)
    (value 8.5 m)
    (source "Plan set A-101, sheet 3")
    (method method:total-station)
    (date "2026-01-09")
    (rank deprecated)
    (superseded-by "survey:C-0212")))
`

	claims, diags := loadClaimModel(t, registry, written)

	require.Len(t, diags, 1)
	assert.Equal(t, `expected a claim id, found the string "survey:C-0212"`, diags[0].Message)

	// The claim is still read, and it is in no chain: nothing readable stands
	// in its place, so the walk forward reaches no current claim.
	claim, ok := claims.Claim("survey:C-0210")
	require.True(t, ok)

	current, ok := claims.Current(claim)
	assert.False(t, ok)
	assert.Nil(t, current)
}

// TestSupersessionTraversalsOnTheZeroValue checks that the traversals answer on
// the collection a tree holding no claim yields, which is what a consuming
// repository whose entities have not been written yet loads.
func TestSupersessionTraversalsOnTheZeroValue(t *testing.T) {
	var claims *Claims

	replacement, ok := claims.Replacement(nil)
	assert.False(t, ok)
	assert.Nil(t, replacement)

	current, ok := claims.Current(nil)
	assert.False(t, ok)
	assert.Nil(t, current)

	assert.Empty(t, slices.Collect(claims.Replaced(nil)))
	assert.Empty(t, slices.Collect(claims.History(nil)))

	empty := &Claims{}

	assert.Empty(t, slices.Collect(empty.Replaced(&Claim{id: "survey:C-0100"})))
	assert.Empty(t, slices.Collect(empty.History(&Claim{id: "survey:C-0100"})))
}
