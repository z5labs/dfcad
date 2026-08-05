// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// recorded runs one invocation against a fresh copy of the shared fixture,
// requiring it to have succeeded, and returns the result it wrote, what it said
// on stderr, and the model root.
func recorded(t *testing.T, args ...string) (claimResult, string, string) {
	t.Helper()

	root := tree(t, model())
	stdout, stderr := invoke(t, exitSuccess, root, args...)

	return listed[claimResult](t, stdout), stderr, root
}

// refusedClaim runs one invocation against a fresh copy of the shared fixture,
// requiring it to have been refused, and returns what it said on stderr.
//
// Nothing on stdout is half of what is asserted, as it is for the node commands:
// a run which produced no result writes no result object, so a caller piping
// stdout never has to tell a change which happened from one which did not.
func refusedClaim(t *testing.T, args ...string) string {
	t.Helper()

	root := tree(t, model())
	before := contents(t, root)

	stdout, stderr := invoke(t, exitUsage, root, args...)

	assert.Empty(t, stdout)
	assert.Equal(t, before, contents(t, root), "a refused change writes nothing")

	return stderr
}

// claimsWritten is every claim the model beneath root holds on a subject under a
// predicate, in written order.
func claimsWritten(t *testing.T, root string, subject dfcad.ID, predicate string) []*dfcad.Claim {
	t.Helper()

	graph, diags := dfcad.LoadGraph(root)
	require.Empty(t, diags)

	return slices.Collect(graph.Claims().Under(subject, predicate))
}

// claimByID is the claim the model beneath root holds under an id, requiring it
// to hold one.
//
// Claims are looked up rather than indexed because canonical form sorts the
// children of every form: which claim of a subject is written first is a
// property of the printing rather than of the change, and a test which read it
// as an order would break the first time a value moved past its neighbour.
func claimByID(t *testing.T, root string, id dfcad.ID) *dfcad.Claim {
	t.Helper()

	graph, diags := dfcad.LoadGraph(root)
	require.Empty(t, diags)

	claim, ok := graph.Claims().Claim(id)
	require.True(t, ok, "the model holds a claim under %s", id)

	return claim
}

// scalarOf is a claim's value as the real number it is, requiring it to be one.
func scalarOf(t *testing.T, claim *dfcad.Claim) float64 {
	t.Helper()

	value, ok := claim.Value().Scalar()
	require.True(t, ok, "the claim carries a scalar value")

	return value
}

// aFullClaim is every axis of a claim written out, which is the invocation the
// cases below start from and change the one flag they are about.
func aFullClaim(subject, predicate string) []string {
	return []string{
		"--value", "18.4",
		"--unit", "m2",
		"--source", "As-built check AB-2026-012, Acme Surveys",
		"--method", "method:total-station",
		"--accuracy", "independent 0.05 m2",
		"--accuracy", "systematic 0.002 m2 method:total-station",
		"--date", "2026-05-06",
		subject, predicate,
	}
}

// TestRunAddClaimWritesAFullClaim checks the whole of what an author gets for
// the whole of what they wrote: every axis reaches the file, and the result
// object says what was written.
func TestRunAddClaimWritesAFullClaim(t *testing.T) {
	result, _, root := recorded(t, append([]string{"add-claim"}, aFullClaim("site:S-102", "area")...)...)

	assert.False(t, result.DryRun)
	assert.Equal(t, []string{"entities/site.dfc"}, files(t, root, result.Commit))
	assert.True(t, result.Rankable)
	assert.Empty(t, result.Notices, "a rankable claim on a pair nothing states says nothing")

	claims := claimsWritten(t, root, "site:S-102", "area")
	require.Len(t, claims, 1)

	assert.Equal(t, 18.4, scalarOf(t, claims[0]))
	assert.Equal(t, dfcad.Unit("m2"), claims[0].Value().Unit())
	assert.Equal(t, "As-built check AB-2026-012, Acme Surveys", claims[0].Source())
	assert.Equal(t, dfcad.ID("method:total-station"), claims[0].Method())
	assert.Equal(t, "2026-05-06", claims[0].Date().Format("2006-01-02"))
	assert.Equal(t, dfcad.RankNormal, claims[0].Rank())

	accuracy, ok := claims[0].Accuracy()
	require.True(t, ok)
	require.Len(t, accuracy.Terms, 2)
	assert.Equal(t, dfcad.TermIndependent, accuracy.Terms[0].Kind)
	assert.Equal(t, dfcad.TermSystematic, accuracy.Terms[1].Kind)
	assert.Equal(t, dfcad.ID("method:total-station"), accuracy.Terms[1].Source)

	// A claim nothing references writes no id, which is the ordinary case.
	assert.Empty(t, result.Claim)

	id, ok := claims[0].ID()
	assert.False(t, ok)
	assert.Empty(t, id)
}

// TestRunAddClaimWritesTheLeastAClaimMaySay is its own function because leaving
// the accuracy out is a different shape of behaviour: it succeeds, it is
// reported, and what it produces is a claim which can never win resolution.
func TestRunAddClaimWritesTheLeastAClaimMaySay(t *testing.T) {
	result, report, root := recorded(t,
		"add-claim", "--value", "18.4", "--unit", "m2",
		"--source", "As-built check AB-2026-012, Acme Surveys",
		"--method", "method:total-station",
		"--date", "2026-05-06",
		"site:S-102", "area",
	)

	assert.False(t, result.Rankable)

	require.Len(t, result.Notices, 1)
	assert.Equal(t, string(dfcad.NoticeUnrankable), result.Notices[0].Kind)
	assert.Equal(t, "site:S-102", result.Notices[0].Subject)
	assert.Equal(t, "area", result.Notices[0].Predicate)
	assert.Contains(t, result.Notices[0].Message, "unrankable")

	// The warning is on stderr as well as in the object, because a person
	// running this is the one who has to decide whether they meant it.
	assert.Contains(t, report, "unrankable")

	claims := claimsWritten(t, root, "site:S-102", "area")
	require.Len(t, claims, 1)
	assert.False(t, claims[0].Rankable(), "the claim records that it is unrankable")
}

// TestRunAddClaimReportsTheConflictItCreated is its own function because the
// claim was written and something is still being said about the model: a second
// claim on one pair is a disagreement, and the disagreement is the answer.
func TestRunAddClaimReportsTheConflictItCreated(t *testing.T) {
	result, report, root := recorded(t, append([]string{"add-claim"}, aFullClaim("site:S-101", "area")...)...)

	require.Len(t, result.Notices, 1)
	assert.Equal(t, string(dfcad.NoticeConflict), result.Notices[0].Kind)

	// The competing claim is named rather than counted: the next thing anybody
	// does with a conflict is read what the other side says and where it was
	// written.
	require.Len(t, result.Notices[0].Competing, 1)
	assert.Equal(t, 24.2, *result.Notices[0].Competing[0].Value.Scalar)
	assert.Equal(t, "As-built check AB-2026-009, Acme Surveys", result.Notices[0].Competing[0].Source)
	assert.NotEmpty(t, result.Notices[0].Competing[0].Span.Start.Path)

	assert.Contains(t, report, "conflict")

	// Both claims are there. Repeating a predicate is the normal case, and the
	// disagreement between two measurements is the most valuable thing in the
	// file.
	assert.Len(t, claimsWritten(t, root, "site:S-101", "area"), 2)
}

func TestRunAddClaimRefusesWhatTheRegistryDoesNotDeclare(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedMentions []string
	}{
		{
			name:             "refuses a predicate nothing declares, naming the declared ones",
			args:             []string{"add-claim", "--value", "18.4", "--unit", "m2", "site:S-102", "aera"},
			expectedMentions: []string{"aera", "area"},
		},
		{
			name: "refuses a value of another shape than the predicate declares",
			args: []string{
				"add-claim", "--value", "about eighteen", "--unit", "m2",
				"--source", "A guess", "--method", "method:estimate",
				"site:S-102", "area",
			},
			expectedMentions: []string{"about eighteen", "scalar"},
		},
		{
			name: "refuses a unit other than the one the predicate declares",
			args: []string{
				"add-claim", "--value", "18.4", "--unit", "ft",
				"--source", "A guess", "--method", "method:estimate",
				"site:S-102", "area",
			},
			expectedMentions: []string{"ft", "m2"},
		},
		{
			name: "refuses a claim with nothing evidencing it",
			args: []string{
				"add-claim", "--value", "18.4", "--unit", "m2",
				"--method", "method:estimate", "site:S-102", "area",
			},
			expectedMentions: []string{"source"},
		},
		{
			name: "refuses a claim which does not say how the value was obtained",
			args: []string{
				"add-claim", "--value", "18.4", "--unit", "m2",
				"--source", "A guess", "site:S-102", "area",
			},
			expectedMentions: []string{"method"},
		},
		{
			name: "refuses a claim with nothing claimed",
			args: []string{
				"add-claim", "--source", "A guess", "--method", "method:estimate",
				"site:S-102", "area",
			},
			expectedMentions: []string{"--value"},
		},
		{
			name: "refuses an accuracy term which is not one",
			args: []string{
				"add-claim", "--value", "18.4", "--unit", "m2",
				"--source", "A guess", "--method", "method:estimate",
				"--accuracy", "roughly", "site:S-102", "area",
			},
			expectedMentions: []string{"roughly", "independent", "systematic"},
		},
		{
			name: "refuses a subject nothing answers to, naming the nearest",
			args: []string{
				"add-claim", "--value", "18.4", "--unit", "m2",
				"--source", "A guess", "--method", "method:estimate",
				"site:S-1O2", "area",
			},
			expectedMentions: []string{"site:S-1O2", "site:S-102"},
		},
		{
			name:             "refuses an invocation naming no predicate",
			args:             []string{"add-claim", "--value", "18.4", "site:S-102"},
			expectedMentions: []string{"predicate"},
		},
		{
			name: "refuses an argument too many",
			args: []string{
				"add-claim", "--value", "18.4", "--unit", "m2",
				"--source", "A guess", "--method", "method:estimate",
				"site:S-102", "area", "and another",
			},
			expectedMentions: []string{"and another"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			report := refusedClaim(t, testCase.args...)

			for _, mention := range testCase.expectedMentions {
				assert.Contains(t, report, mention)
			}
		})
	}
}

// TestRunSupersedeCorrectsAValueInOneTransaction walks the whole of a
// correction: the new claim is written with an id, the old one is retracted in
// its favour, and everything the old one said is still there.
func TestRunSupersedeCorrectsAValueInOneTransaction(t *testing.T) {
	result, _, root := recorded(t,
		"supersede", "--value", "24.6", "--unit", "m2",
		"--source", "Re-measure RM-2026-002, Acme Surveys",
		"--method", "method:total-station",
		"--accuracy", "independent 0.02 m2",
		"--date", "2026-06-01",
		"site:S-101", "area",
	)

	// The id is generated because the retraction references the new claim, and
	// its format is the one the help states.
	assert.Equal(t, "site:S-101:area:1", result.Claim)
	assert.True(t, result.Rankable)

	// The claim which was corrected wrote no id of its own, which is why it
	// could not have been named by one.
	assert.Empty(t, result.Replaced)

	assert.Len(t, claimsWritten(t, root, "site:S-101", "area"), 2)

	// The claim which was written says the new value.
	replacement := claimByID(t, root, "site:S-101:area:1")
	assert.Equal(t, 24.6, scalarOf(t, replacement))
	assert.Equal(t, dfcad.RankNormal, replacement.Rank())

	// The claim it corrected is retracted in its favour, and says everything it
	// said before: correction is supersession, never an edit.
	graph, diags := dfcad.LoadGraph(root)
	require.Empty(t, diags)

	retracted := slices.Collect(graph.Claims().Replaced(replacement))
	require.Len(t, retracted, 1)

	assert.Equal(t, 24.2, scalarOf(t, retracted[0]))
	assert.Equal(t, dfcad.RankDeprecated, retracted[0].Rank())
	assert.Equal(t, "As-built check AB-2026-009, Acme Surveys", retracted[0].Source(),
		"the retracted claim says exactly what it said")

	// One live claim is left, so the correction resolved the pair rather than
	// doubling it.
	assert.Len(t, graph.Claims().Live("site:S-101", "area"), 1)
}

// TestRunSupersedeIsAllOrNothing is its own function because it is about a
// change which was refused after part of it had been applied in memory: the
// retraction cannot land without the claim which replaces it.
func TestRunSupersedeIsAllOrNothing(t *testing.T) {
	files := model()
	files["entities/broken.dfc"] = "(node site:S-104\n  (kind Space)\n  (type MeetingRoom)\n  (geometry area)\n  (within site:S-999))\n"

	root := tree(t, files)
	before := contents(t, root)

	stdout, stderr := invoke(t, exitLoad, root,
		"supersede", "--value", "24.6", "--unit", "m2",
		"--source", "Re-measure RM-2026-002, Acme Surveys",
		"--method", "method:total-station",
		"site:S-101", "area",
	)

	assert.Empty(t, stdout, "a run which produced no result writes no result object")
	assert.Contains(t, stderr, "site:S-999")
	assert.Equal(t, before, contents(t, root), "neither half of the correction was written")
}

func TestRunSupersedeRefusesWhatItCannotCorrect(t *testing.T) {
	testCases := []struct {
		name             string
		subject          string
		expectedMentions []string
	}{
		{
			name:             "refuses a pair the model says nothing about",
			subject:          "site:S-102",
			expectedMentions: []string{"site:S-102", "area", "added"},
		},
		{
			name:             "refuses a pair stated twice, naming the competing claims",
			subject:          "site:S-103",
			expectedMentions: []string{"site:M-0001", "site:M-0002"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			report := refusedClaim(t,
				"supersede", "--value", "24.6", "--unit", "m2",
				"--source", "Re-measure RM-2026-002, Acme Surveys",
				"--method", "method:total-station",
				testCase.subject, "area",
			)

			for _, mention := range testCase.expectedMentions {
				assert.Contains(t, report, mention)
			}
		})
	}
}

// TestRunDeprecateClaim walks the explicit retraction: a claim named by the id
// it wrote, retracted in favour of one named the same way.
func TestRunDeprecateClaim(t *testing.T) {
	result, _, root := recorded(t, "deprecate-claim", "site:M-0001", "--superseded-by", "site:M-0002")

	assert.Equal(t, "site:M-0001", result.Replaced)
	assert.Empty(t, result.Notices, "a pair with a live claim left has a resolvable value")

	assert.Len(t, claimsWritten(t, root, "site:S-103", "area"), 2)

	retracted := claimByID(t, root, "site:M-0001")
	assert.Equal(t, dfcad.RankDeprecated, retracted.Rank())
	assert.Equal(t, 31.0, scalarOf(t, retracted), "retracting is not editing")
	assert.Equal(t, "Design drawing DR-2026-004, Acme Architects", retracted.Source())

	replacement, ok := retracted.SupersededBy()
	require.True(t, ok)
	assert.Equal(t, dfcad.ID("site:M-0002"), replacement)

	assert.Equal(t, dfcad.RankNormal, claimByID(t, root, "site:M-0002").Rank())
}

// TestRunDeprecateClaimReportsASubjectLeftWithNoValue is its own function
// because the assertion is about what the model stops saying rather than about
// the claim which was retracted.
func TestRunDeprecateClaimReportsASubjectLeftWithNoValue(t *testing.T) {
	// A room whose one measurement was superseded by a measurement of another
	// room is the shape of this: the claim which stands in its place is a real
	// claim, and it is not one of the retracted claim's own pair.
	files := model()
	files["entities/annexe.dfc"] = `(node site:S-201
  (label "Meeting Room D")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:site-grid)
  (area
    (id site:M-0003)
    (value 12.0 m2)
    (source "Design drawing DR-2026-004, Acme Architects")
    (method method:estimate)
    (accuracy (independent 0.5 m2))
    (date "2026-01-12")))
`

	root := tree(t, files)

	stdout, stderr := invoke(t, exitSuccess, root,
		"deprecate-claim", "site:M-0003", "--superseded-by", "site:M-0002")

	result := listed[claimResult](t, stdout)

	require.Len(t, result.Notices, 1)
	assert.Equal(t, string(dfcad.NoticeUnresolvable), result.Notices[0].Kind)
	assert.Equal(t, "site:S-201", result.Notices[0].Subject)
	assert.Equal(t, "area", result.Notices[0].Predicate)

	assert.Contains(t, stderr, "no resolvable value")

	graph, diags := dfcad.LoadGraph(root)
	require.Empty(t, diags)
	assert.Empty(t, graph.Claims().Live("site:S-201", "area"))
}

func TestRunDeprecateClaimRefusesWithoutAValidReplacement(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedMentions []string
	}{
		{
			name:             "refuses a deprecation which names nothing to stand in its place",
			args:             []string{"deprecate-claim", "site:M-0001"},
			expectedMentions: []string{"site:M-0001", "superseded-by"},
		},
		{
			name:             "refuses a replacement which names no claim",
			args:             []string{"deprecate-claim", "site:M-0001", "--superseded-by", "site:M-0009"},
			expectedMentions: []string{"site:M-0009"},
		},
		{
			name:             "refuses a claim named as its own replacement",
			args:             []string{"deprecate-claim", "site:M-0001", "--superseded-by", "site:M-0001"},
			expectedMentions: []string{"site:M-0001", "itself"},
		},
		{
			name:             "refuses a claim id nothing carries",
			args:             []string{"deprecate-claim", "site:M-0009", "--superseded-by", "site:M-0002"},
			expectedMentions: []string{"site:M-0009"},
		},
		{
			name:             "refuses an argument which is not an id at all",
			args:             []string{"deprecate-claim", "M-0001", "--superseded-by", "site:M-0002"},
			expectedMentions: []string{"M-0001"},
		},
		{
			name:             "refuses an invocation naming no claim",
			args:             []string{"deprecate-claim", "--superseded-by", "site:M-0002"},
			expectedMentions: []string{"claim"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			report := refusedClaim(t, testCase.args...)

			for _, mention := range testCase.expectedMentions {
				assert.Contains(t, report, mention)
			}
		})
	}
}

// TestRunDeprecateClaimRefusesARetractionOfARetraction is its own function
// because the state it refuses is one the model is already in rather than one
// the arguments describe.
func TestRunDeprecateClaimRefusesARetractionOfARetraction(t *testing.T) {
	root := tree(t, model())

	invoke(t, exitSuccess, root, "deprecate-claim", "site:M-0001", "--superseded-by", "site:M-0002")

	stdout, stderr := invoke(t, exitUsage, root,
		"deprecate-claim", "site:M-0001", "--superseded-by", "site:M-0002")

	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "site:M-0001")
	assert.Contains(t, stderr, "already deprecated")
}

// TestNoCommandEditsAClaimValueInPlace is the assertion behind
// [0009](../../docs/decisions/0009-derived-values-are-never-written-back.md):
// correction is supersession, and there is no invocation of this interface which
// writes over what a claim said.
//
// It walks [commands] rather than naming the ones which touch claims, for the
// reason the contract walks do: a command added later which edited a value would
// be caught by this the day it was added, which is the only way a rule like this
// stays true.
func TestNoCommandEditsAClaimValueInPlace(t *testing.T) {
	for _, cmd := range commands {
		if !cmd.writes {
			continue
		}

		t.Run(cmd.name+" leaves every claim already written saying what it said", func(t *testing.T) {
			root := tree(t, model())

			was := stated(t, root)

			invoke(t, exitSuccess, root, sample(t, cmd)...)

			for written := range stated(t, root) {
				delete(was, written)
			}

			assert.Empty(t, was, "every claim which was there still says exactly what it said")
		})
	}
}

// stated is what every claim of the model beneath root says, keyed by
// everything about it a change is forbidden to touch.
//
// The rank is deliberately not in the key. A retraction is the one thing a
// change may write onto a claim which was already there, and it says the claim
// was withdrawn rather than that it said something else.
func stated(t *testing.T, root string) map[string]struct{} {
	t.Helper()

	graph, diags := dfcad.LoadGraph(root)
	require.Empty(t, diags)

	out := make(map[string]struct{})

	for claim := range graph.Claims().All() {
		entry := entryOf(claim, "")
		entry.Rank, entry.SupersededBy, entry.Span = "", "", dfcad.Span{}

		out[strings.Join([]string{
			string(claim.Subject()),
			entry.Predicate,
			entry.Value.Shape,
			entry.Value.Unit,
			spellScalar(entry.Value),
			entry.Source,
			entry.Method,
			entry.Date,
		}, "|")] = struct{}{}
	}

	return out
}

// spellScalar is a claim's value as a string, for the key above.
//
// It reads the one shape the shared fixture uses and says nothing about the
// others, which is enough: the shape and the unit are already in the key beside
// it, so a value which changed shape differs there.
func spellScalar(value claimValue) string {
	if value.Scalar == nil {
		return ""
	}
	return strconv.FormatFloat(*value.Scalar, 'g', -1, 64)
}

// TestClaimCommandsReportForAPerson checks that the human rendering says what
// changed without changing what a caller piping stdout reads.
func TestClaimCommandsReportForAPerson(t *testing.T) {
	root := tree(t, model())

	quiet := func(t *testing.T, args ...string) (string, string) {
		t.Helper()

		return invoke(t, exitSuccess, root, append([]string{
			"add-claim", "--value", "18.4", "--unit", "m2",
			"--source", "As-built check AB-2026-012, Acme Surveys",
			"--method", "method:total-station",
			"--date", "2026-05-06", "--dry-run",
			"site:S-102", "area",
		}, args...)...)
	}

	machine, machineReport := quiet(t)
	human, humanReport := quiet(t, "--format", formatHuman)

	assert.Equal(t, machine, human)

	// The notice is on stderr in both, because what a change turned out to mean
	// is not a rendering somebody opted into.
	assert.Contains(t, machineReport, "unrankable")
	assert.Contains(t, humanReport, "unrankable")

	assert.NotContains(t, machineReport, "would write 1 file")
	assert.Contains(t, humanReport, "would write 1 file")
}

// TestClaimResultsCarryAnEmptyNoticeList checks the half of the machine
// contract a caller indexing the object depends on: a change with nothing to say
// writes an empty list rather than a null.
func TestClaimResultsCarryAnEmptyNoticeList(t *testing.T) {
	root := tree(t, model())

	stdout, _ := invoke(t, exitSuccess, root, append([]string{"add-claim"}, aFullClaim("site:S-102", "area")...)...)

	assert.Contains(t, stdout, `"notices":[]`)
}

func TestOptionalFlagTellsAnEmptyValueFromNoValue(t *testing.T) {
	testCases := []struct {
		name          string
		values        []string
		expectedValue string
		expectedSet   bool
	}{
		{
			name:        "is unset when it was never written",
			values:      nil,
			expectedSet: false,
		},
		{
			name:        "is set when it was written empty",
			values:      []string{""},
			expectedSet: true,
		},
		{
			name:          "keeps the last value it was written with",
			values:        []string{"first", "second"},
			expectedValue: "second",
			expectedSet:   true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var written optional

			for _, value := range testCase.values {
				require.NoError(t, written.Set(value))
			}

			assert.Equal(t, testCase.expectedValue, written.value)
			assert.Equal(t, testCase.expectedSet, written.set)
		})
	}
}

func TestRepeatedFlagKeepsEveryValueInOrder(t *testing.T) {
	var written repeated

	require.NoError(t, written.Set("independent 0.05 m2"))
	require.NoError(t, written.Set("systematic 0.002 m2 method:total-station"))

	assert.Equal(t, repeated{"independent 0.05 m2", "systematic 0.002 m2 method:total-station"}, written)
	assert.Contains(t, written.String(), "systematic")
}
