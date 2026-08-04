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
