// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// auditRegistry is the vocabulary the model below is judged against.
const auditRegistry = `(project
  (label "Audit fixture")
  (globalid-namespace "https://example.org/models/audit"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
(namespace survey (description "Claim ids issued by Acme Surveys."))

(frame frame:building (label "Building local grid") (unit m))

(type Campus
  (kind Zone)
  (geometry absent)
  (description "A group of things administered together, which has no shape."))

(type Corridor
  (kind Space)
  (geometry area)
  (description "A circulation space between rooms."))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings."))

(predicate area
  (unit m2)
  (shape scalar)
  (description "How much floor a space has."))

(predicate height
  (unit m)
  (shape scalar)
  (description "How far it is from the floor to the ceiling."))

(predicate note
  (shape text)
  (description "Something worth writing down about the thing."))

(predicate occupancy
  (shape scalar)
  (description "How many people the space seats."))

(predicate position
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "The location of a vertex in its frame."))
`

// auditModel is the semantic family, written so that every state a claim can be
// left in by resolution is somewhere in it.
//
// The campus is claimed about at all, so that a subject with an empty answer is
// covered. Room A holds a retracted area claim beside the two live ones which
// disagree, and two equally accurate heights which nothing separates. The
// corridor holds two occupancy claims neither of which carries an accuracy, so
// nothing about them can be ranked and both are equally current. Room B holds a
// retraction and a replacement, which is one live claim and therefore no
// disagreement at all — the one way of silencing a conflict there is. Room C
// holds a winning claim which wrote no id of its own.
const auditModel = `(node site:Z-01
  (label "Riverside campus")
  (kind Zone)
  (type Campus))

(node site:S-101
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (area
    (id survey:A-0001)
    (value 23.0 m2)
    (source "Plan set A-101, sheet 3")
    (method method:scaled-from-plan)
    (accuracy (independent 0.5 m2))
    (date "2026-01-09")
    (rank deprecated)
    (superseded-by survey:A-0002))
  (area
    (id survey:A-0002)
    (value 24.2 m2)
    (source "As-built check AB-2026-009, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-05-06"))
  (area
    (id survey:A-0003)
    (value 24.0 m2)
    (source "Fit-out check FC-2026-002, Acme Surveys")
    (method method:tape)
    (accuracy (independent 0.2 m2))
    (date "2026-05-11"))
  (height
    (id survey:H-0001)
    (value 2.7 m)
    (source "Section A-A, sheet 5")
    (method method:tape)
    (accuracy (independent 0.01 m))
    (date "2026-04-01"))
  (height
    (id survey:H-0002)
    (value 2.71 m)
    (source "Fit-out check FC-2026-002, Acme Surveys")
    (method method:tape)
    (accuracy (independent 0.01 m))
    (date "2026-04-01"))
  (note
    (id survey:N-0001)
    (value "Booked through the front desk.")
    (source "Facilities handbook, 2026 edition")
    (method method:assumed)
    (date "2026-02-01")))

(node site:S-102
  (label "Corridor 1")
  (kind Space)
  (type Corridor)
  (geometry area)
  (frame frame:building)
  (area
    (id survey:A-0004)
    (value 11.5 m2)
    (source "Plan set A-101, sheet 3")
    (method method:scaled-from-plan)
    (accuracy (independent 0.5 m2))
    (date "2026-01-09"))
  (occupancy
    (id survey:O-0001)
    (value 8.0)
    (source "Fire strategy FS-01")
    (method method:assumed)
    (date "2026-02-01"))
  (occupancy
    (id survey:O-0002)
    (value 6.0)
    (source "Fire strategy FS-02")
    (method method:assumed)
    (date "2026-03-01")))

(node site:S-103
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (area
    (id survey:A-0005)
    (value 18.0 m2)
    (source "Plan set A-101, sheet 3")
    (method method:scaled-from-plan)
    (accuracy (independent 0.5 m2))
    (date "2026-01-09")
    (rank deprecated)
    (superseded-by survey:A-0006))
  (area
    (id survey:A-0006)
    (value 18.4 m2)
    (source "As-built check AB-2026-010, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-05-06")))

(node site:S-104
  (label "Meeting Room C")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (area
    (value 31.2 m2)
    (source "As-built check AB-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.01 m2))
    (date "2026-06-01"))
  (area
    (id survey:A-0007)
    (value 31.0 m2)
    (source "Plan set A-101, sheet 4")
    (method method:scaled-from-plan)
    (accuracy (independent 0.3 m2))
    (date "2026-06-01")))
`

// auditGeometry is a vertex two control sets disagree about, which is what says
// that the register is about claims rather than about semantic nodes.
const auditGeometry = `(vertex geom:V-01
  (label "Room A, north-west corner")
  (frame frame:building)
  (position
    (id survey:P-0001)
    (value (0.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18"))
  (position
    (id survey:P-0002)
    (value (0.01 0.0 0.0) m)
    (source "Interior control set IC-02, Acme Surveys")
    (method method:tape)
    (accuracy (independent 0.01 m))
    (date "2026-03-18")))
`

// auditable is the fixture tree the two audit commands are run against.
func auditable() map[string]string {
	return map[string]string{
		"registry.dfc":          auditRegistry,
		"entities/site.dfc":     auditModel,
		"entities/geometry.dfc": auditGeometry,
	}
}

// claimed runs claims over the fixture and decodes what reached stdout.
func claimed(t *testing.T, args ...string) claimsResult {
	t.Helper()

	t.Chdir(tree(t, auditable()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run(append([]string{"claims"}, args...), &stdout, &stderr), stderr.String())

	// Nothing is wrong with the fixture, so nothing is on stderr. It is asserted
	// rather than assumed because a model which quietly stopped loading would
	// still answer every assertion below.
	require.Empty(t, stderr.String())

	result := listed[claimsResult](t, stdout.String())
	assert.Equal(t, outputVersion, result.Version)
	assert.Equal(t, "claims", result.Command)

	return result
}

// conflicting runs conflicts over the fixture and decodes what reached stdout.
func conflicting(t *testing.T, args ...string) []conflictEntry {
	t.Helper()

	t.Chdir(tree(t, auditable()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run(append([]string{"conflicts"}, args...), &stdout, &stderr), stderr.String())

	require.Empty(t, stderr.String())

	result := listed[conflictsResult](t, stdout.String())
	assert.Equal(t, outputVersion, result.Version)
	assert.Equal(t, "conflicts", result.Command)

	return result.Conflicts
}

// resolutions is each claim as "predicate id state", which is what says which
// claims came back, in which order, and what resolution made of each.
func resolutions(claims []claimEntry) []string {
	out := make([]string, 0, len(claims))
	for _, claim := range claims {
		out = append(out, strings.TrimSpace(claim.Predicate+" "+claim.ID)+" "+claim.Resolution)
	}
	return out
}

// pairs is each conflict as "subject predicate", which is what says which pairs
// came back and in which order.
func pairs(conflicts []conflictEntry) []string {
	out := make([]string, 0, len(conflicts))
	for _, conflict := range conflicts {
		out = append(out, conflict.Subject+" "+conflict.Predicate)
	}
	return out
}

func TestRunClaims(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name: "returns every claim on the subject, the retracted ones among them",
			args: []string{"site:S-101"},
			expected: []string{
				"area survey:A-0001 " + resolutionRetracted,
				"area survey:A-0002 " + resolutionCurrent,
				"area survey:A-0003 " + resolutionOutranked,
				"height survey:H-0001 " + resolutionTied,
				"height survey:H-0002 " + resolutionTied,
				"note survey:N-0001 " + resolutionUnranked,
			},
		},
		{
			name: "narrows to one predicate when one is named",
			args: []string{"site:S-101", "area"},
			expected: []string{
				"area survey:A-0001 " + resolutionRetracted,
				"area survey:A-0002 " + resolutionCurrent,
				"area survey:A-0003 " + resolutionOutranked,
			},
		},
		{
			name:     "reports a thing nothing is claimed about as no claims at all",
			args:     []string{"site:Z-01"},
			expected: []string{},
		},
		{
			name:     "reports a predicate nothing is claimed under as no claims at all",
			args:     []string{"site:Z-01", "area"},
			expected: []string{},
		},
		{
			name: "reports the claims written on a geometric node",
			args: []string{"geom:V-01"},
			expected: []string{
				"position survey:P-0001 " + resolutionCurrent,
				"position survey:P-0002 " + resolutionOutranked,
			},
		},
		{
			name: "marks a claim which wrote no id of its own",
			args: []string{"site:S-104"},
			expected: []string{
				"area " + resolutionCurrent,
				"area survey:A-0007 " + resolutionOutranked,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := claimed(t, testCase.args...)

			assert.Equal(t, testCase.args[0], result.Subject)
			assert.Equal(t, testCase.expected, resolutions(result.Claims))
		})
	}
}

// TestRunClaimsMarksARetractedClaimWithWhatReplacedIt is its own function
// because a retracted claim is not the same shape of answer as a live one: it
// says which claim replaced it, and that reference is what makes the history
// walkable forward without a second call.
func TestRunClaimsMarksARetractedClaimWithWhatReplacedIt(t *testing.T) {
	claims := claimed(t, "site:S-101", "area").Claims

	require.NotEmpty(t, claims)
	retracted := claims[0]

	assert.Equal(t, "survey:A-0001", retracted.ID)
	assert.Equal(t, string(dfcad.RankDeprecated), retracted.Rank)
	assert.Equal(t, "survey:A-0002", retracted.SupersededBy)
	assert.Equal(t, resolutionRetracted, retracted.Resolution)

	// The claim which replaced it says nothing about being a replacement, which
	// is what makes the reference one-directional and followable forward.
	assert.Empty(t, claims[1].SupersededBy)
}

// TestRunClaimsReportsTheEvidenceForEachClaim is its own function because it
// asserts about the whole of one claim rather than about which claims came
// back. A value which arrived without where it came from, how it was obtained
// and how good it is would be the bare number the format exists to stop.
func TestRunClaimsReportsTheEvidenceForEachClaim(t *testing.T) {
	claims := claimed(t, "site:S-101", "area").Claims

	require.Len(t, claims, 3)
	current := claims[1]

	assert.Equal(t, "survey:A-0002", current.ID)
	assert.Equal(t, "area", current.Predicate)
	assert.Equal(t, "As-built check AB-2026-009, Acme Surveys", current.Source)
	assert.Equal(t, "method:total-station", current.Method)
	assert.Equal(t, "2026-05-06", current.Date)
	assert.Equal(t, string(dfcad.RankNormal), current.Rank)
	assert.Equal(t, []accuracyTerm{
		{Kind: string(dfcad.TermIndependent), Magnitude: 0.05, Unit: "m2"},
	}, current.Accuracy)

	require.NotNil(t, current.Value.Scalar)
	assert.Equal(t, string(dfcad.ShapeScalar), current.Value.Shape)
	assert.Equal(t, "m2", current.Value.Unit)
	assert.InDelta(t, 24.2, *current.Value.Scalar, 1e-9)

	// The claim carries where it was written, which is what sends a reader to
	// the file rather than to a search.
	assert.Contains(t, current.Span.Start.Path, "site.dfc")
	assert.Positive(t, current.Span.Start.Line)
}

func TestRunConflicts(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name: "returns every pair the model states more than once",
			args: nil,
			expected: []string{
				"geom:V-01 position",
				"site:S-101 area",
				"site:S-101 height",
				"site:S-102 occupancy",
				"site:S-104 area",
			},
		},
		{
			name:     "narrows to one predicate",
			args:     []string{"--predicate", "area"},
			expected: []string{"site:S-101 area", "site:S-104 area"},
		},
		{
			name: "narrows to the subjects of one type",
			args: []string{"--type", "MeetingRoom"},
			expected: []string{
				"site:S-101 area",
				"site:S-101 height",
				"site:S-104 area",
			},
		},
		{
			name:     "narrows to the pairs resolution cannot decide",
			args:     []string{"--ambiguous"},
			expected: []string{"site:S-101 height", "site:S-102 occupancy"},
		},
		{
			name: "narrows to the pairs resolution can decide",
			args: []string{"--resolved"},
			expected: []string{
				"geom:V-01 position",
				"site:S-101 area",
				"site:S-104 area",
			},
		},
		{
			name:     "combines the filters",
			args:     []string{"--type", "MeetingRoom", "--predicate", "area", "--resolved"},
			expected: []string{"site:S-101 area", "site:S-104 area"},
		},
		{
			name:     "answers nothing when no pair satisfies every filter",
			args:     []string{"--type", "Corridor", "--predicate", "height"},
			expected: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, pairs(conflicting(t, testCase.args...)))
		})
	}
}

// TestRunConflictsShowsTheCompetingClaims is its own function because it asserts
// about the inside of one entry rather than about which entries came back: an
// entry which reported only that there was a disagreement would be a second
// lookup per line of the register.
func TestRunConflictsShowsTheCompetingClaims(t *testing.T) {
	testCases := []struct {
		name              string
		subject           string
		expectedType      string
		expectedAmbiguous bool
		expectedCurrent   string
		expectedClaims    []string
	}{
		{
			name:            "shows which claim resolution picks",
			subject:         "site:S-101 area",
			expectedType:    "MeetingRoom",
			expectedCurrent: "survey:A-0002",
			expectedClaims: []string{
				"area survey:A-0002 " + resolutionCurrent,
				"area survey:A-0003 " + resolutionOutranked,
			},
		},
		{
			name:              "shows a pair nothing separates as ambiguous",
			subject:           "site:S-101 height",
			expectedType:      "MeetingRoom",
			expectedAmbiguous: true,
			expectedClaims: []string{
				"height survey:H-0001 " + resolutionTied,
				"height survey:H-0002 " + resolutionTied,
			},
		},
		{
			name:              "shows a pair nothing rankable was said about as ambiguous",
			subject:           "site:S-102 occupancy",
			expectedType:      "Corridor",
			expectedAmbiguous: true,
			expectedClaims: []string{
				"occupancy survey:O-0001 " + resolutionTied,
				"occupancy survey:O-0002 " + resolutionTied,
			},
		},
		{
			name:            "shows a pair on a geometric subject, which declares no type",
			subject:         "geom:V-01 position",
			expectedCurrent: "survey:P-0001",
			expectedClaims: []string{
				"position survey:P-0001 " + resolutionCurrent,
				"position survey:P-0002 " + resolutionOutranked,
			},
		},
		{
			name:         "leaves the winning id out when the claim which won wrote none",
			subject:      "site:S-104 area",
			expectedType: "MeetingRoom",
			expectedClaims: []string{
				"area " + resolutionCurrent,
				"area survey:A-0007 " + resolutionOutranked,
			},
		},
	}

	conflicts := conflicting(t)

	byPair := make(map[string]conflictEntry, len(conflicts))
	for _, conflict := range conflicts {
		byPair[conflict.Subject+" "+conflict.Predicate] = conflict
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			conflict, ok := byPair[testCase.subject]
			require.True(t, ok, "the register holds %s", testCase.subject)

			assert.Equal(t, testCase.expectedType, conflict.Type)
			assert.Equal(t, testCase.expectedAmbiguous, conflict.Ambiguous)
			assert.Equal(t, testCase.expectedCurrent, conflict.Current)
			assert.Equal(t, testCase.expectedClaims, resolutions(conflict.Claims))
		})
	}
}

// TestRunConflictsLeavesRetractedClaimsOutOfTheRegister is its own function
// because it is about what is not in the answer. Deprecating a claim is the one
// way of silencing a conflict the format has, and it requires asserting in the
// file that the claim is wrong.
func TestRunConflictsLeavesRetractedClaimsOutOfTheRegister(t *testing.T) {
	// Room B holds two area claims, one of them retracted, and so is not in
	// dispute with itself.
	assert.NotContains(t, pairs(conflicting(t)), "site:S-103 area")

	// The retracted claim is still a claim the model holds, which is what the
	// audit view of the subject says.
	assert.Equal(t, []string{
		"area survey:A-0005 " + resolutionRetracted,
		"area survey:A-0006 " + resolutionCurrent,
	}, resolutions(claimed(t, "site:S-103").Claims))
}

// TestConflictsAreAFindingRatherThanAFailure is its own function because it is
// the property both commands exist to have: a model which disagrees with itself
// is a model somebody has to look at, not a run which failed.
func TestConflictsAreAFindingRatherThanAFailure(t *testing.T) {
	testCases := [][]string{
		{"conflicts"},
		{"conflicts", "--ambiguous"},
		{"claims", "site:S-101"},
		{"claims", "site:S-101", "height"},
	}

	for _, args := range testCases {
		t.Run(strings.Join(args, " ")+" exits zero however much the model disagrees", func(t *testing.T) {
			t.Chdir(tree(t, auditable()))

			var stdout, stderr bytes.Buffer

			assert.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())
			assert.NotEmpty(t, stdout.String())
		})
	}
}

// TestRunClaimsAndConflictsRejectWhatTheModelDoesNotHold walks the ways an
// invocation can name something that is not there. Each is a usage error rather
// than an empty answer, and stdout stays empty because the run produced no
// result.
func TestRunClaimsAndConflictsRejectWhatTheModelDoesNotHold(t *testing.T) {
	testCases := []struct {
		name           string
		args           []string
		expectedStderr string
	}{
		{
			name: "names the nearest id when one is close enough to be the one meant",
			args: []string{"claims", "site:S-1O1"},
			expectedStderr: "dfcad claims: " +
				UnknownIDError{ID: "site:S-1O1", Nearest: "site:S-101"}.Error() + "\n",
		},
		{
			name: "reports an argument which is not an id at all",
			args: []string{"claims", "S-101"},
			expectedStderr: "dfcad claims: " +
				dfcad.MalformedIDError{Written: "S-101", Reason: dfcad.IDUnqualified}.Error() + "\n",
		},
		{
			name: "rejects a predicate the registry does not declare",
			args: []string{"claims", "site:S-101", "widht"},
			expectedStderr: "dfcad claims: " +
				UnknownPredicateError{Predicate: "widht", Declared: auditPredicates()}.Error() + "\n",
		},
		{
			name:           "reports a claims with no id at all",
			args:           []string{"claims"},
			expectedStderr: "dfcad claims: " + ErrMissingID.Error() + "\n\n" + claimsUsage,
		},
		{
			name: "rejects a third argument",
			args: []string{"claims", "site:S-101", "area", "height"},
			expectedStderr: "dfcad claims: " +
				UnexpectedArgumentsError{Extra: []string{"height"}}.Error() + "\n\n" + claimsUsage,
		},
		{
			name: "rejects a type the registry does not declare",
			args: []string{"conflicts", "--type", "MeetingRom"},
			expectedStderr: "dfcad conflicts: " +
				UnknownTypeError{
					Type:     "MeetingRom",
					Declared: []string{"Campus", "Corridor", "MeetingRoom"},
				}.Error() + "\n",
		},
		{
			name: "rejects a predicate the registry does not declare",
			args: []string{"conflicts", "--predicate", "aera"},
			expectedStderr: "dfcad conflicts: " +
				UnknownPredicateError{Predicate: "aera", Declared: auditPredicates()}.Error() + "\n",
		},
		{
			name:           "refuses the two halves of the register together",
			args:           []string{"conflicts", "--ambiguous", "--resolved"},
			expectedStderr: "dfcad conflicts: " + ErrAmbiguousAndResolved.Error() + "\n",
		},
		{
			name: "rejects an argument to a command which takes none",
			args: []string{"conflicts", "site:S-101"},
			expectedStderr: "dfcad conflicts: " +
				UnexpectedArgumentsError{Extra: []string{"site:S-101"}}.Error() + "\n\n" + conflictsUsage,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, auditable()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitUsage, run(testCase.args, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Equal(t, testCase.expectedStderr, stderr.String())
		})
	}
}

// auditPredicates is the predicates the fixture registry declares, in the order
// an error naming one of them lists them.
func auditPredicates() []string {
	return []string{"area", "height", "note", "occupancy", "position"}
}

// TestUnknownPredicateErrorSaysWhatThereWas checks that the error carries the
// declared set for a caller to branch on, rather than only spelling it into a
// message a caller would have to parse back apart.
func TestUnknownPredicateErrorSaysWhatThereWas(t *testing.T) {
	some := UnknownPredicateError{Predicate: "widht", Declared: []string{"area", "width"}}
	assert.Contains(t, some.Error(), "width")

	// A model which declares no predicate at all says so, rather than offering
	// an empty list of alternatives.
	none := UnknownPredicateError{Predicate: "width"}
	assert.Contains(t, none.Error(), "no predicate at all")
}

// TestCheckPredicateAcceptsEveryDeclaredPredicate is the other half of the
// rejection table: every predicate the registry declares passes, and so does no
// predicate at all, so the check is not simply refusing everything.
func TestCheckPredicateAcceptsEveryDeclaredPredicate(t *testing.T) {
	graph, _ := dfcad.LoadGraph(tree(t, auditable()))

	assert.NoError(t, checkPredicate(graph.Registry(), ""))
	for _, predicate := range auditPredicates() {
		assert.NoError(t, checkPredicate(graph.Registry(), predicate), predicate)
	}
}

// TestRunClaimsAndConflictsStillAnswerOnAModelWithDiagnostics is its own
// function because it is about a run over a model which is not sound. What was
// asked for is still what the model holds, the diagnostics still reach whoever
// wrote the file, and whether the model is sound is what `dfcad check` answers.
func TestRunClaimsAndConflictsStillAnswerOnAModelWithDiagnostics(t *testing.T) {
	files := auditable()
	files["entities/broken.dfc"] = unparseable

	for _, args := range [][]string{{"claims", "site:S-101"}, {"conflicts"}} {
		t.Run(args[0]+" answers anyway", func(t *testing.T) {
			t.Chdir(tree(t, files))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run(args, &stdout, &stderr))

			assert.NotEmpty(t, object(t, stdout.String()))
			assert.Contains(t, stderr.String(), "broken.dfc:1:")
		})
	}
}

// TestRunClaimsAndConflictsOutputIsDeterministic checks that two runs over the
// same model write byte-identical results, which is what makes diffing two runs
// mean something and is the whole of what "stably ordered" has to mean from the
// outside.
func TestRunClaimsAndConflictsOutputIsDeterministic(t *testing.T) {
	for _, args := range [][]string{
		{"claims", "site:S-101"},
		{"claims", "site:S-101", "area"},
		{"claims", "geom:V-01"},
		{"conflicts"},
		{"conflicts", "--type", "MeetingRoom"},
		{"conflicts", "--ambiguous"},
	} {
		t.Run(strings.Join(args, " ")+" writes the same bytes twice", func(t *testing.T) {
			var results []string
			for range 2 {
				t.Chdir(tree(t, auditable()))

				var stdout, stderr bytes.Buffer
				require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

				results = append(results, stdout.String())
			}

			assert.Equal(t, results[0], results[1])
		})
	}
}

// TestRunClaimsAndConflictsHumanOutputNeverChangesStdout is its own function
// because it is about the one property the format flag must not have: whichever
// format was asked for, and however loud the run was told to be, stdout is the
// same bytes.
func TestRunClaimsAndConflictsHumanOutputNeverChangesStdout(t *testing.T) {
	report := func(t *testing.T, args ...string) (string, string) {
		t.Helper()

		t.Chdir(tree(t, auditable()))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

		return stdout.String(), stderr.String()
	}

	machine, machineReport := report(t, "claims", "site:S-101")
	human, humanReport := report(t, "claims", "site:S-101", "--format", formatHuman)
	loud, loudReport := report(t, "claims", "site:S-101", "--format", formatHuman, "-v")

	assert.Equal(t, machine, human)
	assert.Equal(t, machine, loud)

	assert.Empty(t, machineReport)
	assert.Contains(t, humanReport, "6 claims of site:S-101 under 3 predicates, 1 retracted")
	assert.NotContains(t, humanReport, "area: 24.2 m2")
	assert.Contains(t, loudReport, "area: 24.2 m2 by method:total-station on 2026-05-06, current")
	assert.Contains(t, loudReport, "area: 23 m2 by method:scaled-from-plan on 2026-01-09, retracted, deprecated")

	machine, machineReport = report(t, "conflicts")
	human, humanReport = report(t, "conflicts", "--format", formatHuman)
	loud, loudReport = report(t, "conflicts", "--format", formatHuman, "-v")

	assert.Equal(t, machine, human)
	assert.Equal(t, machine, loud)

	assert.Empty(t, machineReport)
	assert.Contains(t, humanReport, "5 conflicts across 4 subjects, 2 ambiguous")
	assert.NotContains(t, humanReport, "site:S-101 area:")
	assert.Contains(t, loudReport, "site:S-101 area: 2 claims, resolved to survey:A-0002")
	assert.Contains(t, loudReport, "site:S-101 height: 2 claims, ambiguous")
	assert.Contains(t, loudReport, "site:S-104 area: 2 claims, resolved")
}

// TestRunClaimsAndConflictsUsage checks that help goes to stderr and exits zero,
// which is the half of the contract that keeps prose off the stream a caller
// pipes.
func TestRunClaimsAndConflictsUsage(t *testing.T) {
	testCases := map[string]string{
		"claims":    claimsUsage,
		"conflicts": conflictsUsage,
	}

	for name, expected := range testCases {
		t.Run(name+" prints its usage to stderr and exits zero", func(t *testing.T) {
			t.Chdir(t.TempDir())

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitSuccess, run([]string{name, "-h"}, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Equal(t, expected, stderr.String())
		})
	}
}

// TestClaimsAndConflictsErrorsAreNotSwallowed checks that a stdout which cannot
// be written reports a failure rather than an unexplained success.
func TestClaimsAndConflictsErrorsAreNotSwallowed(t *testing.T) {
	for _, args := range [][]string{{"claims", "site:S-101"}, {"conflicts"}} {
		t.Run(args[0]+" reports a stdout it cannot write", func(t *testing.T) {
			t.Chdir(tree(t, auditable()))

			var stderr bytes.Buffer

			assert.Equal(t, exitLoad, run(args, brokenWriter{}, &stderr))
			assert.Contains(t, stderr.String(), "dfcad "+args[0]+":")
		})
	}
}
