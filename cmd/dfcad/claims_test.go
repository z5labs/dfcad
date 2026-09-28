// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"cmp"
	"errors"
	"slices"
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
// holds a winning claim which wrote no id of its own. Room D has been retired, and
// the area claimed of it before it was is still a claim the model holds.
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

(node site:S-105
  (label "Meeting Room D")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (area
    (id survey:A-0008)
    (value 12.5 m2)
    (source "Plan set A-101, sheet 3")
    (method method:scaled-from-plan)
    (accuracy (independent 0.5 m2))
    (date "2026-01-09"))
  (retired
    (date "2026-04-02")
    (reason "Merged into Meeting Room A.")
    (superseded-by site:S-101)))
`

// auditGeometry is a vertex two control sets disagree about, which is what says
// that the register is about claims rather than about semantic nodes, and an
// edge and a loop which each carry one claim, so that every family a claim can
// be written on is somewhere in the audit fixture.
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

(vertex geom:V-02 (frame frame:building))

(vertex geom:V-03 (frame frame:building))

(edge geom:E-01
  (frame frame:building)
  (vertices geom:V-01 geom:V-02)
  (note
    (id survey:N-0002)
    (value "Runs along the glazing.")
    (source "Site walk SW-01")
    (method method:assumed)
    (date "2026-02-01")))

(edge geom:E-02 (frame frame:building) (vertices geom:V-02 geom:V-03))

(edge geom:E-03 (frame frame:building) (vertices geom:V-03 geom:V-01))

(loop geom:L-01
  (frame frame:building)
  (edges geom:E-01 geom:E-02 geom:E-03)
  (note
    (id survey:N-0003)
    (value "The corner the fire strategy calls the east lobby.")
    (source "Fire strategy FS-02")
    (method method:assumed)
    (date "2026-03-01")))
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

// entries is the claim object of each row, which is what the assertions shared
// with the other audit views are written against.
func entries(rows []claimRow) []claimEntry {
	out := make([]claimEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.claimEntry)
	}
	return out
}

// rows is each row as "subject predicate id state", which is what says which
// claims a listing of more than one subject returned, and in which order.
func rows(claims []claimRow) []string {
	out := make([]string, 0, len(claims))
	for _, claim := range claims {
		out = append(out, strings.Join(strings.Fields(claim.Subject+" "+claim.Predicate+" "+claim.ID+" "+claim.Resolution), " "))
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
			assert.Equal(t, testCase.expected, resolutions(entries(result.Claims)))
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
	}, resolutions(entries(claimed(t, "site:S-103").Claims)))
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
			name: "rejects a second type the registry does not declare",
			args: []string{"conflicts", "--type", "MeetingRoom", "--type", "MeetingRom"},
			expectedStderr: "dfcad conflicts: " +
				UnknownTypeError{
					Type:     "MeetingRom",
					Declared: []string{"Campus", "Corridor", "MeetingRoom"},
				}.Error() + "\n",
		},
		{
			name: "rejects a second predicate the registry does not declare",
			args: []string{"conflicts", "--predicate", "area", "--predicate", "aera"},
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

	machine, machineReport = report(t, "claims")
	human, humanReport = report(t, "claims", "--format", formatHuman)
	loud, loudReport = report(t, "claims", "--format", formatHuman, "-v")

	assert.Equal(t, machine, human)
	assert.Equal(t, machine, loud)

	assert.Empty(t, machineReport)
	assert.Contains(t, humanReport, "18 claims of 8 subjects under 5 predicates, 2 retracted")
	assert.NotContains(t, humanReport, "site:S-101 area:")
	assert.Contains(t, loudReport, "site:S-101 area: 24.2 m2 by method:total-station on 2026-05-06, current")
	assert.Contains(t, loudReport, "geom:E-01 note: ")

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

// conflictOrder is the documented order of the register: by subject, and then
// by predicate.
func conflictOrder(a, b map[string]any) int {
	return cmp.Or(
		strings.Compare(a["subject"].(string), b["subject"].(string)),
		strings.Compare(a["predicate"].(string), b["predicate"].(string)),
	)
}

// TestConflictsFiltersWrittenTwiceAnswerTheUnion is the property both of the
// register's naming filters promise: within one flag a pair is listed when it
// satisfies any of the values.
//
// It runs against the audit fixture, written to a temporary directory, rather
// than against the budget model, which states nothing twice: here two types and
// three predicates each have pairs in dispute.
func TestConflictsFiltersWrittenTwiceAnswerTheUnion(t *testing.T) {
	testCases := []struct {
		name   string
		args   []string
		flag   string
		first  string
		second string
	}{
		{
			name:   "lists the pairs whose subject declares either type",
			args:   []string{"conflicts"},
			flag:   "type",
			first:  "MeetingRoom",
			second: "Corridor",
		},
		{
			name:   "lists the pairs written under either predicate",
			args:   []string{"conflicts"},
			flag:   "predicate",
			first:  "area",
			second: "height",
		},
		{
			// Two predicates written on one subject: the subject's pairs are
			// each listed once, in predicate order, rather than grouped by the
			// value which selected them.
			name:   "lists the pairs under either predicate beside another filter",
			args:   []string{"conflicts", "--type", "MeetingRoom"},
			flag:   "predicate",
			first:  "height",
			second: "area",
		},
		{
			name:   "lists the pairs under either predicate among the ones resolution cannot decide",
			args:   []string{"conflicts", "--ambiguous"},
			flag:   "predicate",
			first:  "occupancy",
			second: "height",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assertFilterIsAUnion(t, tree(t, auditable()), testCase.args,
				testCase.flag, testCase.first, testCase.second, "conflicts", conflictOrder)
		})
	}
}

// TestRunClaimsAndConflictsCarryEachClaimsAccuracyCombined checks that the
// claim objects claims and conflicts write carry the figure each accuracy
// reduces to, exactly as get writes it: they share the claim object, so one
// rendering of the figure is one rendering everywhere.
func TestRunClaimsAndConflictsCarryEachClaimsAccuracyCombined(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		claims   func(t *testing.T, stdout string) []claimEntry
		expected int
	}{
		{
			name: "on every claim claims returns",
			args: []string{"claims", "site:S-120"},
			claims: func(t *testing.T, stdout string) []claimEntry {
				return entries(listed[claimsResult](t, stdout).Claims)
			},
			expected: 6,
		},
		{
			name: "on every competing claim of a conflict",
			args: []string{"conflicts"},
			claims: func(t *testing.T, stdout string) []claimEntry {
				var out []claimEntry
				for _, conflict := range listed[conflictsResult](t, stdout).Conflicts {
					if conflict.Subject == "site:S-120" {
						out = append(out, conflict.Claims...)
					}
				}
				return out
			},
			expected: 5,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, withCombined(auditable(), "")))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run(testCase.args, &stdout, &stderr), stderr.String())

			assert.NotContains(t, stderr.String(), "error:", "the fixture loads with no more than its warning")

			claims := testCase.claims(t, stdout.String())
			assert.Equal(t, testCase.expected, assertCombined(t, claims), "how many of the claims were checked")
		})
	}
}

// everyClaim is the whole audit fixture as claims with no subject lists it: in
// subject id order, then predicate order, then the order each was written.
func everyClaim() []string {
	return []string{
		"geom:E-01 note survey:N-0002 " + resolutionUnranked,
		"geom:L-01 note survey:N-0003 " + resolutionUnranked,
		"geom:V-01 position survey:P-0001 " + resolutionCurrent,
		"geom:V-01 position survey:P-0002 " + resolutionOutranked,
		"site:S-101 area survey:A-0001 " + resolutionRetracted,
		"site:S-101 area survey:A-0002 " + resolutionCurrent,
		"site:S-101 area survey:A-0003 " + resolutionOutranked,
		"site:S-101 height survey:H-0001 " + resolutionTied,
		"site:S-101 height survey:H-0002 " + resolutionTied,
		"site:S-101 note survey:N-0001 " + resolutionUnranked,
		"site:S-102 area survey:A-0004 " + resolutionCurrent,
		"site:S-102 occupancy survey:O-0001 " + resolutionTied,
		"site:S-102 occupancy survey:O-0002 " + resolutionTied,
		"site:S-103 area survey:A-0005 " + resolutionRetracted,
		"site:S-103 area survey:A-0006 " + resolutionCurrent,
		"site:S-104 area " + resolutionCurrent,
		"site:S-104 area survey:A-0007 " + resolutionOutranked,
		"site:S-105 area survey:A-0008 " + resolutionCurrent,
	}
}

// only is the rows of [everyClaim] which satisfy keep, in the order they come.
func only(keep func(row string) bool) []string {
	out := make([]string, 0)
	for _, row := range everyClaim() {
		if keep(row) {
			out = append(out, row)
		}
	}
	return out
}

// under keeps the rows written under one of the predicates.
func under(predicates ...string) func(string) bool {
	return func(row string) bool {
		return slices.Contains(predicates, strings.Fields(row)[1])
	}
}

// writtenOn keeps the rows written on one of the subjects.
func writtenOn(subjects ...string) func(string) bool {
	return func(row string) bool {
		return slices.Contains(subjects, strings.Fields(row)[0])
	}
}

func TestRunClaimsWithNoSubject(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "lists every claim on every subject, live and retracted",
			args:     nil,
			expected: everyClaim(),
		},
		{
			name:     "narrows to one predicate",
			args:     []string{"--predicate", "area"},
			expected: only(under("area")),
		},
		{
			name:     "lists the claims under either of two predicates",
			args:     []string{"--predicate", "height", "--predicate", "area"},
			expected: only(under("area", "height")),
		},
		{
			name:     "narrows to the subjects of one type",
			args:     []string{"--type", "MeetingRoom"},
			expected: only(writtenOn("site:S-101", "site:S-103", "site:S-104", "site:S-105")),
		},
		{
			name:     "lists the claims on subjects of either of two types",
			args:     []string{"--type", "Corridor", "--type", "MeetingRoom"},
			expected: only(writtenOn("site:S-101", "site:S-102", "site:S-103", "site:S-104", "site:S-105")),
		},
		{
			name:     "narrows to one family",
			args:     []string{"--family", "vertex"},
			expected: only(writtenOn("geom:V-01")),
		},
		{
			name:     "lists the claims on subjects of either of two families",
			args:     []string{"--family", "loop", "--family", "edge"},
			expected: only(writtenOn("geom:E-01", "geom:L-01")),
		},
		{
			name:     "narrows to the semantic nodes",
			args:     []string{"--family", "node", "--predicate", "note"},
			expected: []string{"site:S-101 note survey:N-0001 " + resolutionUnranked},
		},
		{
			name: "combines the filters",
			args: []string{"--type", "MeetingRoom", "--predicate", "area", "--family", "node"},
			expected: only(func(row string) bool {
				return under("area")(row) && writtenOn("site:S-101", "site:S-103", "site:S-104", "site:S-105")(row)
			}),
		},
		{
			name:     "accepts a type beside a family list which holds node",
			args:     []string{"--type", "Corridor", "--family", "vertex", "--family", "node"},
			expected: only(writtenOn("site:S-102")),
		},
		{
			name:     "answers nothing for a declared predicate nothing on the family is claimed under",
			args:     []string{"--predicate", "position", "--family", "node"},
			expected: []string{},
		},
		{
			name:     "answers nothing for a declared type whose instances carry no claim",
			args:     []string{"--type", "Campus"},
			expected: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := claimed(t, testCase.args...)

			assert.Empty(t, result.Subject)
			assert.Equal(t, testCase.expected, rows(result.Claims))
		})
	}
}

// TestRunClaimsFiltersASubject is its own function because the filters narrow
// one subject's claims as well as every subject's, and the positional predicate
// is one more value of --predicate rather than a second filter beside it.
func TestRunClaimsFiltersASubject(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "narrows one subject's claims to a predicate given by flag",
			args:     []string{"site:S-101", "--predicate", "height"},
			expected: only(func(row string) bool { return writtenOn("site:S-101")(row) && under("height")(row) }),
		},
		{
			name:     "counts the positional predicate as one value of the flag",
			args:     []string{"site:S-101", "note", "--predicate", "area"},
			expected: only(func(row string) bool { return writtenOn("site:S-101")(row) && under("area", "note")(row) }),
		},
		{
			name:     "answers nothing when the subject is not of the family asked for",
			args:     []string{"site:S-101", "--family", "vertex"},
			expected: []string{},
		},
		{
			name:     "answers nothing when the subject does not declare the type asked for",
			args:     []string{"site:S-101", "--type", "Corridor"},
			expected: []string{},
		},
		{
			name:     "answers a geometric subject's claims under its own family",
			args:     []string{"geom:L-01", "--family", "loop"},
			expected: only(writtenOn("geom:L-01")),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := claimed(t, testCase.args...)

			assert.Equal(t, testCase.args[0], result.Subject)
			assert.Equal(t, testCase.expected, rows(result.Claims))
		})
	}
}

// TestRunClaimsSaysWhatEachClaimIsWrittenOn is its own function because it
// asserts about the subject's fields on each row rather than about which rows
// came back: which fields are written, and which are absent.
func TestRunClaimsSaysWhatEachClaimIsWrittenOn(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected map[string]any
	}{
		{
			name:     "names a node's family and type",
			args:     []string{"--predicate", "occupancy"},
			expected: map[string]any{"subject": "site:S-102", "family": "node", "type": "Corridor"},
		},
		{
			name:     "marks a claim on a retired node",
			args:     []string{"site:S-105"},
			expected: map[string]any{"subject": "site:S-105", "family": "node", "type": "MeetingRoom", "retired": true},
		},
		{
			name:     "names a vertex's family and no type",
			args:     []string{"geom:V-01"},
			expected: map[string]any{"subject": "geom:V-01", "family": "vertex"},
		},
		{
			name:     "names an edge's family and no type",
			args:     []string{"--family", "edge"},
			expected: map[string]any{"subject": "geom:E-01", "family": "edge"},
		},
		{
			name:     "names a loop's family and no type",
			args:     []string{"--family", "loop"},
			expected: map[string]any{"subject": "geom:L-01", "family": "loop"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, auditable()))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run(append([]string{"claims"}, testCase.args...), &stdout, &stderr), stderr.String())

			claims := entriesOf(t, object(t, stdout.String()), "claims")
			require.NotEmpty(t, claims)

			for _, claim := range claims {
				row := claim.(map[string]any)
				for _, key := range []string{"subject", "family", "type", "retired"} {
					expected, ok := testCase.expected[key]
					if !ok {
						assert.NotContains(t, row, key)
						continue
					}
					assert.Equal(t, expected, row[key], key)
				}
			}
		})
	}
}

// TestRunClaimsListsARetiredNodeAndARetractedClaim is its own function because
// both are things a listing of what the model currently holds leaves out, and
// the audit view is the one which does not.
func TestRunClaimsListsARetiredNodeAndARetractedClaim(t *testing.T) {
	claims := claimed(t, "--predicate", "area").Claims

	var retired, retracted []string
	for _, claim := range claims {
		if claim.Retired {
			retired = append(retired, claim.Subject+" "+claim.ID)
		}
		if claim.Resolution == resolutionRetracted {
			retracted = append(retracted, claim.Subject+" "+claim.ID)
			assert.NotEmpty(t, claim.SupersededBy, "a retraction names what replaced it")
		}
	}

	assert.Equal(t, []string{"site:S-105 survey:A-0008"}, retired)
	assert.Equal(t, []string{"site:S-101 survey:A-0001", "site:S-103 survey:A-0005"}, retracted)
}

// TestRunClaimsOverEverySubjectIsEachSubjectInTurn is the property the listing
// with no subject is defined by: for every thing the model holds, the rows
// written on it are exactly the rows "claims <id>" returns, in the same order.
//
// It walks every node, vertex, edge and loop of the fixture rather than the
// subjects the listing happens to name, so that a subject whose claims the
// listing dropped altogether is caught as well as one it reordered.
func TestRunClaimsOverEverySubjectIsEachSubjectInTurn(t *testing.T) {
	graph, _ := dfcad.LoadGraph(tree(t, auditable()))

	var subjects []string
	for node := range graph.Nodes().All() {
		subjects = append(subjects, string(node.ID()))
	}
	for vertex := range graph.Topology().Vertices() {
		subjects = append(subjects, string(vertex.ID()))
	}
	for edge := range graph.Topology().Edges() {
		subjects = append(subjects, string(edge.ID()))
	}
	for loop := range graph.Topology().Loops() {
		subjects = append(subjects, string(loop.ID()))
	}
	require.Len(t, subjects, 13, "the fixture holds six nodes, three vertices, three edges and a loop")

	every := claimed(t).Claims

	for _, subject := range subjects {
		t.Run(subject+" is listed as claims "+subject+" lists it", func(t *testing.T) {
			var listed []claimRow
			for _, row := range every {
				if row.Subject == subject {
					listed = append(listed, row)
				}
			}

			alone := claimed(t, subject).Claims

			assert.Equal(t, alone, append(make([]claimRow, 0), listed...))
		})
	}
}

// claimOrder is the documented order of a listing of every subject's claims:
// by subject, then by predicate, then by where each was written. Where it was
// written is compared by its place in the unfiltered listing, which is that
// order already, because a span is not orderable as the string it is written as.
//
// The listing is read from the tree the union is asserted over, because a span
// names the file it is in: a place looked up by the span of another tree finds
// nothing, and every claim under one predicate would compare equal.
func claimOrder(t *testing.T, root string) func(a, b map[string]any) int {
	t.Helper()

	place := make(map[string]int)
	for i, claim := range entriesOf(t, answerOf(t, root, "claims"), "claims") {
		place[claim.(map[string]any)["span"].(string)] = i
	}

	return func(a, b map[string]any) int {
		return cmp.Or(
			strings.Compare(a["subject"].(string), b["subject"].(string)),
			strings.Compare(a["predicate"].(string), b["predicate"].(string)),
			cmp.Compare(place[a["span"].(string)], place[b["span"].(string)]),
		)
	}
}

// TestClaimsFiltersWrittenTwiceAnswerTheUnion is the property each of claims'
// filters promises: within one flag a claim is listed when it satisfies any of
// the values.
func TestClaimsFiltersWrittenTwiceAnswerTheUnion(t *testing.T) {
	testCases := []struct {
		name   string
		args   []string
		flag   string
		first  string
		second string
	}{
		{
			name:   "lists the claims under either predicate",
			args:   []string{"claims"},
			flag:   "predicate",
			first:  "height",
			second: "area",
		},
		{
			name:   "lists the claims on a subject declaring either type",
			args:   []string{"claims"},
			flag:   "type",
			first:  "MeetingRoom",
			second: "Corridor",
		},
		{
			name:   "lists the claims on a subject of either family",
			args:   []string{"claims"},
			flag:   "family",
			first:  "vertex",
			second: "node",
		},
		{
			name:   "lists one subject's claims under either predicate",
			args:   []string{"claims", "site:S-101"},
			flag:   "predicate",
			first:  "note",
			second: "area",
		},
		{
			name:   "lists the claims under either predicate beside another filter",
			args:   []string{"claims", "--family", "node"},
			flag:   "predicate",
			first:  "occupancy",
			second: "height",
		},
		{
			name:   "lists the claims obtained by either method",
			args:   []string{"claims"},
			flag:   "method",
			first:  "method:tape",
			second: "method:scaled-from-plan",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, auditable())

			assertFilterIsAUnion(t, root, testCase.args,
				testCase.flag, testCase.first, testCase.second, "claims", claimOrder(t, root))
		})
	}
}

// TestRunClaimsRefusesAFilterNamingNothing walks the ways a filter can name
// something the model has no such thing of. Each is a usage error rather than an
// empty answer, and stdout stays empty because the run produced no result.
func TestRunClaimsRefusesAFilterNamingNothing(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected error
	}{
		{
			name:     "rejects a predicate the registry does not declare",
			args:     []string{"--predicate", "aera"},
			expected: UnknownPredicateError{Predicate: "aera", Declared: auditPredicates()},
		},
		{
			name:     "rejects a second predicate the registry does not declare",
			args:     []string{"--predicate", "area", "--predicate", "hieght"},
			expected: UnknownPredicateError{Predicate: "hieght", Declared: auditPredicates()},
		},
		{
			name:     "rejects a flag's predicate beside a subject",
			args:     []string{"site:S-101", "area", "--predicate", "aera"},
			expected: UnknownPredicateError{Predicate: "aera", Declared: auditPredicates()},
		},
		{
			name: "rejects a type the registry does not declare",
			args: []string{"--type", "MeetingRom"},
			expected: UnknownTypeError{
				Type:     "MeetingRom",
				Declared: []string{"Campus", "Corridor", "MeetingRoom"},
			},
		},
		{
			name:     "rejects a family which is none of the four",
			args:     []string{"--family", "vertices"},
			expected: UnknownFamilyError{Family: "vertices", Known: claimFamilies},
		},
		{
			name:     "rejects a second family which is none of the four",
			args:     []string{"--family", "node", "--family", "nodes"},
			expected: UnknownFamilyError{Family: "nodes", Known: claimFamilies},
		},
		{
			name:     "refuses a type beside families none of which is node",
			args:     []string{"--type", "MeetingRoom", "--family", "vertex", "--family", "loop"},
			expected: ErrTypeNeedsNodeFamily,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, auditable()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitUsage, run(append([]string{"claims"}, testCase.args...), &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Equal(t, "dfcad claims: "+testCase.expected.Error()+"\n", stderr.String())
		})
	}
}

// TestCheckClaimFamilies asserts the refusals which need no model on the error
// values themselves, so that what a caller branches on is the type and its
// fields rather than a message.
func TestCheckClaimFamilies(t *testing.T) {
	t.Run("reports the first family which is none of the four, with the four", func(t *testing.T) {
		err := checkClaimFamilies(nil, []string{"edge", "vertexes", "loops"})

		var unknown UnknownFamilyError
		require.ErrorAs(t, err, &unknown)
		assert.Equal(t, "vertexes", unknown.Family)
		assert.Equal(t, []string{familyNode, familyVertex, familyEdge, familyLoop}, unknown.Known)
	})

	t.Run("refuses a type no family asked for can declare", func(t *testing.T) {
		err := checkClaimFamilies([]string{"MeetingRoom"}, []string{"vertex", "edge", "loop"})

		assert.ErrorIs(t, err, ErrTypeNeedsNodeFamily)
	})

	t.Run("accepts a type beside no family, or beside node", func(t *testing.T) {
		assert.NoError(t, checkClaimFamilies([]string{"MeetingRoom"}, nil))
		assert.NoError(t, checkClaimFamilies([]string{"MeetingRoom"}, []string{"edge", "node"}))
		assert.NoError(t, checkClaimFamilies(nil, claimFamilies))
	})
}

// TestRunClaimsAnswersAModelWithNoClaims is its own function because it runs
// against a model of its own: one which declares predicates and claims nothing
// under any of them, which is an empty list rather than a failure.
func TestRunClaimsAnswersAModelWithNoClaims(t *testing.T) {
	t.Chdir(tree(t, map[string]string{
		"registry.dfc": auditRegistry,
		"entities/site.dfc": `(node site:Z-01
  (label "Riverside campus")
  (kind Zone)
  (type Campus))
`,
	}))

	for _, args := range [][]string{{"claims"}, {"claims", "--predicate", "area"}} {
		t.Run(strings.Join(args, " ")+" answers an empty list", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

			result := object(t, stdout.String())
			assert.Equal(t, []any{}, result["claims"])
			assert.NotContains(t, result, "subject")
		})
	}
}

// TestRunClaimsAnswersAModelTheLoadRefused is its own function because it is
// about the load rather than the listing: every subject's claims are still what
// the model holds when the load refused it, and the answer says it was refused.
func TestRunClaimsAnswersAModelTheLoadRefused(t *testing.T) {
	files := auditable()
	files["entities/broken.dfc"] = unparseable

	t.Chdir(tree(t, files))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"claims"}, &stdout, &stderr))

	result := listed[claimsResult](t, stdout.String())
	assert.True(t, result.Refused)
	assert.Equal(t, everyClaim(), rows(result.Claims))
	assert.Contains(t, stderr.String(), "broken.dfc:1:")
}

// TestRunClaimsOverASurveyedPlot runs the listing with no subject over the
// siting fixture's surveyed plot, which is the question the listing was asked
// for: every corner's position, with its method and accuracy, in one call.
//
// It is also where the frame exclusion is exercised, because the plot's
// registry places each of its frames with a claim written on the frame.
func TestRunClaimsOverASurveyedPlot(t *testing.T) {
	const root = "../../testdata/siting/surveyed"

	run := func(t *testing.T, args ...string) claimsResult {
		t.Helper()

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run(append([]string{"claims", "--root", root}, args...), &stdout, &stderr), stderr.String())

		return listed[claimsResult](t, stdout.String())
	}

	t.Run("lists every surveyed corner's position with its method and accuracy", func(t *testing.T) {
		claims := run(t, "--predicate", "position", "--family", "vertex").Claims

		require.Len(t, claims, 24)
		for _, claim := range claims {
			assert.Equal(t, familyVertex, claim.Family)
			assert.Equal(t, "position", claim.Predicate)
			assert.NotEmpty(t, claim.Method, claim.Subject)
			assert.NotEmpty(t, claim.Accuracy, claim.Subject)
		}
		assert.Equal(t, "geom:V-01", claims[0].Subject)
		assert.IsIncreasing(t, subjectsOf(claims))
	})

	t.Run("leaves out the claims written on a frame", func(t *testing.T) {
		graph, _ := dfcad.LoadGraph(root)

		var onFrames int
		for claim := range graph.Claims().All() {
			if strings.HasPrefix(string(claim.Subject()), "frame:") {
				onFrames++
			}
		}
		require.Positive(t, onFrames, "the plot no longer writes a claim on a frame, so this says nothing")

		for _, claim := range run(t).Claims {
			assert.False(t, strings.HasPrefix(claim.Subject, "frame:"), claim.Subject)
		}
		assert.Empty(t, run(t, "--predicate", "frame-transform").Claims)
	})
}

// subjectsOf is the subject of each row, which is what an order by subject is
// asserted on.
func subjectsOf(claims []claimRow) []string {
	out := make([]string, 0, len(claims))
	for _, claim := range claims {
		out = append(out, claim.Subject)
	}
	return out
}

// methods is the method each row of [everyClaim] was obtained by, which is what
// the fixture wrote on each claim.
var methods = map[string]string{
	"geom:E-01 note survey:N-0002":       "method:assumed",
	"geom:L-01 note survey:N-0003":       "method:assumed",
	"geom:V-01 position survey:P-0001":   "method:total-station",
	"geom:V-01 position survey:P-0002":   "method:tape",
	"site:S-101 area survey:A-0001":      "method:scaled-from-plan",
	"site:S-101 area survey:A-0002":      "method:total-station",
	"site:S-101 area survey:A-0003":      "method:tape",
	"site:S-101 height survey:H-0001":    "method:tape",
	"site:S-101 height survey:H-0002":    "method:tape",
	"site:S-101 note survey:N-0001":      "method:assumed",
	"site:S-102 area survey:A-0004":      "method:scaled-from-plan",
	"site:S-102 occupancy survey:O-0001": "method:assumed",
	"site:S-102 occupancy survey:O-0002": "method:assumed",
	"site:S-103 area survey:A-0005":      "method:scaled-from-plan",
	"site:S-103 area survey:A-0006":      "method:total-station",
	"site:S-104 area":                    "method:total-station",
	"site:S-104 area survey:A-0007":      "method:scaled-from-plan",
	"site:S-105 area survey:A-0008":      "method:scaled-from-plan",
}

// obtainedBy keeps the rows obtained by one of the methods.
func obtainedBy(wanted ...string) func(string) bool {
	return func(row string) bool {
		fields := strings.Fields(row)
		method, ok := methods[strings.Join(fields[:len(fields)-1], " ")]
		return ok && slices.Contains(wanted, method)
	}
}

func TestRunClaimsFiltersByMethod(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "lists only the claims obtained by one method",
			args:     []string{"--method", "method:total-station"},
			expected: only(obtainedBy("method:total-station")),
		},
		{
			name:     "lists the claims obtained by either of two methods",
			args:     []string{"--method", "method:tape", "--method", "method:assumed"},
			expected: only(obtainedBy("method:tape", "method:assumed")),
		},
		{
			name: "combines with a predicate",
			args: []string{"--method", "method:scaled-from-plan", "--predicate", "area"},
			expected: only(func(row string) bool {
				return obtainedBy("method:scaled-from-plan")(row) && under("area")(row)
			}),
		},
		{
			name:     "combines with a family",
			args:     []string{"--method", "method:tape", "--family", "vertex"},
			expected: []string{"geom:V-01 position survey:P-0002 " + resolutionOutranked},
		},
		{
			name: "combines with a type",
			args: []string{"--method", "method:assumed", "--type", "Corridor"},
			expected: []string{
				"site:S-102 occupancy survey:O-0001 " + resolutionTied,
				"site:S-102 occupancy survey:O-0002 " + resolutionTied,
			},
		},
		{
			name:     "narrows one subject's claims",
			args:     []string{"site:S-101", "--method", "method:tape"},
			expected: only(func(row string) bool { return writtenOn("site:S-101")(row) && obtainedBy("method:tape")(row) }),
		},
		{
			name:     "narrows one subject's claims under its positional predicate",
			args:     []string{"site:S-101", "area", "--method", "method:tape"},
			expected: []string{"site:S-101 area survey:A-0003 " + resolutionOutranked},
		},
		{
			name:     "answers nothing, without a warning, for a method the other filters exclude",
			args:     []string{"--method", "method:assumed", "--family", "vertex"},
			expected: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// claimed asserts that stderr is empty, which is what says a method
			// some claim names is never warned about.
			result := claimed(t, testCase.args...)

			assert.Equal(t, testCase.expected, rows(result.Claims))
			for _, row := range result.Claims {
				assert.Contains(t, testCase.args, row.Method)
			}
		})
	}
}

// TestRunClaimsByMethodIsTheListingNarrowed is the filter as a property: for
// every method, the rows it lists are exactly the rows of the unfiltered
// listing which name it, in the same order and with every field the same.
func TestRunClaimsByMethodIsTheListingNarrowed(t *testing.T) {
	every := claimed(t).Claims

	named := make([]string, 0)
	for _, row := range every {
		if !slices.Contains(named, row.Method) {
			named = append(named, row.Method)
		}
	}
	require.Len(t, named, 4, "the fixture no longer writes the four methods this walks")

	for _, method := range named {
		t.Run("--method "+method+" is the listing's rows obtained by it", func(t *testing.T) {
			expected := make([]claimRow, 0)
			for _, row := range every {
				if row.Method == method {
					expected = append(expected, row)
				}
			}

			assert.Equal(t, expected, claimed(t, "--method", method).Claims)
		})
	}

	t.Run("without --method the listing is every claim", func(t *testing.T) {
		assert.Equal(t, everyClaim(), rows(every))
	})
}

// TestRunClaimsRefusesAMethodItCannotCheck walks the two ways a method can be
// refused. A method is an id rather than a member of a known set, so what can
// be refused is its grammar and its namespace, and nothing else.
func TestRunClaimsRefusesAMethodItCannotCheck(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected error
	}{
		{
			name:     "rejects a method which is not an id",
			args:     []string{"--method", "total-station"},
			expected: dfcad.MalformedIDError{Written: "total-station", Reason: dfcad.IDUnqualified},
		},
		{
			name:     "rejects a second method which is not an id",
			args:     []string{"geom:V-01", "--method", "method:tape", "--method", "method:"},
			expected: dfcad.MalformedIDError{Written: "method:", Reason: dfcad.IDEmptyLocal},
		},
		{
			name: "rejects a method in a namespace the registry does not declare",
			args: []string{"--method", "methods:tape"},
			expected: dfcad.UnknownAxisError{
				Axis:      "namespace",
				Value:     "methods",
				Permitted: []string{"frame", "geom", "method", "site", "survey"},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, auditable()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitUsage, run(append([]string{"claims"}, testCase.args...), &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Equal(t, "dfcad claims: "+testCase.expected.Error()+"\n", stderr.String())
		})
	}
}

// TestParseMethods asserts the refusal of a method which is not an id on the
// error value, so that what a caller branches on is its type and its fields.
func TestParseMethods(t *testing.T) {
	t.Run("reports the first method which is not an id, with the rule it broke", func(t *testing.T) {
		_, err := parseMethods([]string{"method:tape", "tape", "also wrong"})

		var got dfcad.MalformedIDError
		require.True(t, errors.As(err, &got), "expected MalformedIDError, got %T", err)
		assert.Equal(t, "tape", got.Written)
		assert.Equal(t, dfcad.IDUnqualified, got.Reason)
	})

	t.Run("keeps every method in the order written", func(t *testing.T) {
		got, err := parseMethods([]string{"method:tape", "method:assumed"})

		require.NoError(t, err)
		assert.Equal(t, []string{"method:tape", "method:assumed"}, got)
	})
}

// TestCheckMethodNamespaces asserts the refusal of an undeclared namespace on
// the error value, which is the one the write path gives for the same mistake.
func TestCheckMethodNamespaces(t *testing.T) {
	graph, _ := dfcad.LoadGraph(tree(t, auditable()))

	t.Run("reports the first namespace the registry does not declare", func(t *testing.T) {
		err := checkMethodNamespaces(graph.Registry(), []string{"method:tape", "instrument:tape", "other:x"})

		var got dfcad.UnknownAxisError
		require.True(t, errors.As(err, &got), "expected UnknownAxisError, got %T", err)
		assert.Equal(t, "namespace", got.Axis)
		assert.Equal(t, "instrument", got.Value)
		assert.Contains(t, got.Permitted, "method")
	})

	t.Run("accepts a method nothing names in a declared namespace", func(t *testing.T) {
		assert.NoError(t, checkMethodNamespaces(graph.Registry(), []string{"method:laser-scan"}))
	})
}

// TestRunClaimsWarnsOfAMethodNothingNames is its own function because what it
// asserts is on stderr, in every format, and that stdout is untouched by it.
func TestRunClaimsWarnsOfAMethodNothingNames(t *testing.T) {
	// The answer the warning must not change: an empty listing, over the same
	// model and of the same subject, that no method filter produced.
	empty := func(t *testing.T, format, subject string) string {
		t.Helper()

		args := []string{"claims", "--format", format, "--predicate", "position", "--family", "node"}
		if subject != "" {
			args = []string{"claims", "--format", format, subject, "--family", "vertex"}
		}

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run(args, &stdout, &stderr))
		assert.NotContains(t, stderr.String(), "warning")

		return stdout.String()
	}

	testCases := []struct {
		name   string
		args   []string
		warned []string
	}{
		{
			name:   "warns of a method no claim names",
			args:   []string{"--method", "method:laser-scan"},
			warned: []string{"method:laser-scan"},
		},
		{
			name:   "warns once of a method written twice",
			args:   []string{"--method", "method:laser-scan", "--method", "method:laser-scan"},
			warned: []string{"method:laser-scan"},
		},
		{
			name:   "warns of each method no claim names, and of no other",
			args:   []string{"--method", "method:gnss", "--method", "method:tape", "--method", "method:lidar", "--family", "loop"},
			warned: []string{"method:gnss", "method:lidar"},
		},
		{
			name:   "warns beside a subject",
			args:   []string{"site:S-101", "--method", "method:laser-scan"},
			warned: []string{"method:laser-scan"},
		},
	}

	for _, format := range formats {
		for _, testCase := range testCases {
			t.Run(testCase.name+" under --format "+format, func(t *testing.T) {
				t.Chdir(tree(t, auditable()))

				var stdout, stderr bytes.Buffer
				args := append([]string{"claims", "--format", format}, testCase.args...)
				require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

				result := listed[claimsResult](t, stdout.String())
				assert.Empty(t, result.Claims)
				assert.Equal(t, empty(t, format, result.Subject), stdout.String())

				var warnings []string
				for _, line := range strings.Split(stderr.String(), "\n") {
					if strings.HasPrefix(line, "dfcad claims: warning: ") {
						warnings = append(warnings, line)
					}
				}
				require.Len(t, warnings, len(testCase.warned), stderr.String())
				for i, method := range testCase.warned {
					assert.Contains(t, warnings[i], method)
				}
			})
		}
	}
}

// TestUnnamedMethods asserts which methods are warned of on the values
// themselves: every claim in the model counts as naming its method, whatever
// the listing's other filters leave.
func TestUnnamedMethods(t *testing.T) {
	graph, _ := dfcad.LoadGraph(tree(t, auditable()))

	testCases := []struct {
		name     string
		methods  []string
		expected []string
	}{
		{
			name:     "names nothing when no method was asked for",
			methods:  nil,
			expected: nil,
		},
		{
			name:     "names nothing when every method is on some claim",
			methods:  []string{"method:tape", "method:assumed", "method:scaled-from-plan", "method:total-station"},
			expected: nil,
		},
		{
			name:     "names each method no claim names, once, in the order asked",
			methods:  []string{"method:lidar", "method:tape", "method:gnss", "method:lidar"},
			expected: []string{"method:lidar", "method:gnss"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, unnamedMethods(graph, testCase.methods))
		})
	}
}

// stateNoAccuracy is the rows of [everyClaim] whose claim states no accuracy,
// which in the audit fixture is every claim resolution cannot rank: the notes
// on room A, the edge and the loop, and the corridor's two occupancy claims.
var stateNoAccuracy = []string{
	"geom:E-01 note survey:N-0002",
	"geom:L-01 note survey:N-0003",
	"site:S-101 note survey:N-0001",
	"site:S-102 occupancy survey:O-0001",
	"site:S-102 occupancy survey:O-0002",
}

// unrankable keeps the rows whose claim states no accuracy.
func unrankable(row string) bool {
	fields := strings.Fields(row)
	return slices.Contains(stateNoAccuracy, strings.Join(fields[:len(fields)-1], " "))
}

func TestRunClaimsFiltersUnrankable(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "lists only the claims which state no accuracy",
			args:     []string{"--unrankable"},
			expected: only(unrankable),
		},
		{
			name: "combines with a predicate",
			args: []string{"--unrankable", "--predicate", "occupancy"},
			expected: []string{
				"site:S-102 occupancy survey:O-0001 " + resolutionTied,
				"site:S-102 occupancy survey:O-0002 " + resolutionTied,
			},
		},
		{
			name:     "combines with a predicate every claim under which is rankable",
			args:     []string{"--unrankable", "--predicate", "area"},
			expected: []string{},
		},
		{
			name:     "combines with a family",
			args:     []string{"--unrankable", "--family", "edge", "--family", "loop"},
			expected: only(func(row string) bool { return unrankable(row) && writtenOn("geom:E-01", "geom:L-01")(row) }),
		},
		{
			name:     "combines with the node family",
			args:     []string{"--family", "node", "--unrankable"},
			expected: only(func(row string) bool { return unrankable(row) && strings.HasPrefix(row, "site:") }),
		},
		{
			name:     "combines with a type",
			args:     []string{"--unrankable", "--type", "MeetingRoom"},
			expected: []string{"site:S-101 note survey:N-0001 " + resolutionUnranked},
		},
		{
			name:     "combines with a method",
			args:     []string{"--unrankable", "--method", "method:assumed"},
			expected: only(unrankable),
		},
		{
			name:     "combines with a method no unrankable claim was obtained by",
			args:     []string{"--unrankable", "--method", "method:tape"},
			expected: []string{},
		},
		{
			name:     "narrows one subject's claims",
			args:     []string{"site:S-101", "--unrankable"},
			expected: []string{"site:S-101 note survey:N-0001 " + resolutionUnranked},
		},
		{
			name:     "narrows one subject's claims under its positional predicate",
			args:     []string{"site:S-102", "occupancy", "--unrankable"},
			expected: only(func(row string) bool { return unrankable(row) && writtenOn("site:S-102")(row) }),
		},
		{
			name:     "answers nothing for a subject whose every claim states an accuracy",
			args:     []string{"geom:V-01", "--unrankable"},
			expected: []string{},
		},
		{
			name:     "answers nothing for a subject nothing is claimed about",
			args:     []string{"site:Z-01", "--unrankable"},
			expected: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := claimed(t, testCase.args...)

			assert.Equal(t, testCase.expected, rows(result.Claims))
			for _, row := range result.Claims {
				assert.Empty(t, row.Accuracy)
				assert.Nil(t, row.Combined)
			}
		})
	}
}

// TestRunClaimsUnrankableIsTheListingNarrowed is the filter as a property: the
// rows --unrankable lists are exactly the rows of the unfiltered listing which
// carry no accuracy, in the same order and with every field the same.
func TestRunClaimsUnrankableIsTheListingNarrowed(t *testing.T) {
	every := claimed(t).Claims

	t.Run("--unrankable is the listing's rows with no accuracy", func(t *testing.T) {
		expected := make([]claimRow, 0)
		for _, row := range every {
			if len(row.Accuracy) == 0 {
				expected = append(expected, row)
			}
		}
		require.NotEmpty(t, expected, "the fixture no longer writes a claim with no accuracy")

		assert.Equal(t, expected, claimed(t, "--unrankable").Claims)
	})

	t.Run("each subject's --unrankable is that subject's rows with no accuracy", func(t *testing.T) {
		for _, subject := range subjectsOf(every) {
			expected := make([]claimRow, 0)
			for _, row := range claimed(t, subject).Claims {
				if len(row.Accuracy) == 0 {
					expected = append(expected, row)
				}
			}

			assert.Equal(t, expected, claimed(t, subject, "--unrankable").Claims, subject)
		}
	})

	t.Run("without --unrankable the listing is every claim", func(t *testing.T) {
		assert.Equal(t, everyClaim(), rows(every))
	})
}

// retractedUnrankable is a room whose height was first written with no
// accuracy, then retracted in favour of one which states one.
const retractedUnrankable = `
(node site:S-106
  (label "Meeting Room E")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (height
    (id survey:H-0106)
    (value 2.6 m)
    (source "Facilities handbook, 2026 edition")
    (method method:assumed)
    (date "2026-01-09")
    (rank deprecated)
    (superseded-by survey:H-0107))
  (height
    (id survey:H-0107)
    (value 2.65 m)
    (source "Section A-A, sheet 5")
    (method method:tape)
    (accuracy (independent 0.01 m))
    (date "2026-04-01")))
`

// TestRunClaimsUnrankableListsARetractedClaim is its own function because it
// runs over the audit fixture with a room added, which every other listing
// test would have to account for: the unrankable claim here is retracted, and
// it is listed, marked as the retraction it is.
func TestRunClaimsUnrankableListsARetractedClaim(t *testing.T) {
	files := auditable()
	files["entities/site.dfc"] += retractedUnrankable
	t.Chdir(tree(t, files))

	listing := func(t *testing.T, args ...string) []claimRow {
		t.Helper()

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run(append([]string{"claims"}, args...), &stdout, &stderr), stderr.String())
		require.Empty(t, stderr.String())

		return listed[claimsResult](t, stdout.String()).Claims
	}

	t.Run("lists the retracted claim, marked retracted, and not its rankable replacement", func(t *testing.T) {
		claims := listing(t, "site:S-106", "--unrankable")

		require.Len(t, claims, 1)
		assert.Equal(t, "survey:H-0106", claims[0].ID)
		assert.Equal(t, resolutionRetracted, claims[0].Resolution)
		assert.Equal(t, string(dfcad.RankDeprecated), claims[0].Rank)
		assert.Equal(t, "survey:H-0107", claims[0].SupersededBy)
	})

	t.Run("lists it among the live unrankable claims of the whole model", func(t *testing.T) {
		assert.Equal(t, []string{
			"geom:E-01 note survey:N-0002 " + resolutionUnranked,
			"geom:L-01 note survey:N-0003 " + resolutionUnranked,
			"site:S-101 note survey:N-0001 " + resolutionUnranked,
			"site:S-102 occupancy survey:O-0001 " + resolutionTied,
			"site:S-102 occupancy survey:O-0002 " + resolutionTied,
			"site:S-106 height survey:H-0106 " + resolutionRetracted,
		}, rows(listing(t, "--unrankable")))
	})
}

// TestRunClaimsUnrankableListsAnAccuracyInMixedUnits is its own function
// because its fixture loads with a warning, which [claimed] asserts is absent.
// A claim whose accuracy terms are not all in one unit states an accuracy and
// is still unrankable (specification section 6.5): the flag selects the state
// resolution reports, and that claim is in it.
func TestRunClaimsUnrankableListsAnAccuracyInMixedUnits(t *testing.T) {
	files := auditable()
	files["entities/site.dfc"] += mixedAccuracyModel
	t.Chdir(tree(t, files))

	listing := func(t *testing.T, args ...string) []claimRow {
		t.Helper()

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run(append([]string{"claims"}, args...), &stdout, &stderr), stderr.String())

		return listed[claimsResult](t, stdout.String()).Claims
	}

	t.Run("lists the claims whose terms are in more than one unit, with their units", func(t *testing.T) {
		claims := listing(t, "--unrankable", "--predicate", "height")

		assert.Equal(t, []string{
			"site:S-109 height survey:H-0109 " + resolutionUnranked,
			"site:S-110 height survey:H-0111 " + resolutionOutranked,
		}, rows(claims))
		for _, claim := range claims {
			assert.NotEmpty(t, claim.Accuracy)
			assert.Nil(t, claim.Combined)
			assert.Equal(t, []string{"mm", "m"}, claim.Units)
		}
	})

	t.Run("is exactly the listing's rows with no combined figure", func(t *testing.T) {
		expected := make([]claimRow, 0)
		for _, row := range listing(t) {
			if row.Combined == nil {
				expected = append(expected, row)
			}
		}

		assert.Equal(t, expected, listing(t, "--unrankable"))
	})
}

// TestRunClaimsUnrankableOverAModelWhereEveryClaimStatesAnAccuracy is its own
// function because it runs over a model of its own: nothing in it is
// unrankable, and that is an empty answer rather than a failure.
func TestRunClaimsUnrankableOverAModelWhereEveryClaimStatesAnAccuracy(t *testing.T) {
	t.Chdir(tree(t, map[string]string{
		"registry.dfc": auditRegistry,
		"entities/site.dfc": `(node site:S-103
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (area
    (id survey:A-0006)
    (value 18.4 m2)
    (source "As-built check AB-2026-010, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-05-06")))
`,
	}))

	for _, args := range [][]string{{"claims", "--unrankable"}, {"claims", "site:S-103", "--unrankable"}} {
		t.Run(strings.Join(args, " ")+" answers an empty list", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

			result := object(t, stdout.String())
			assert.Equal(t, []any{}, result["claims"])
			assert.EqualValues(t, outputVersion, result["version"])
		})
	}
}

// claimedFramesRegistry is a registry whose claims are written on frames, for
// claims and resolve asked about a frame id. It is modelled on
// testdata/checks/grid/affirmed: a root frame carrying its coordinate reference
// system as a plain value and its ground-to-grid factor as claims, and a child
// fitted to it by a transform.
//
// The root carries more than the fixture does. Two live factors say the same
// thing with nothing rankable behind either, so resolution cannot choose, and a
// third is deprecated in favour of one of them. Two convergence angles under a
// strict predicate are equally accurate and equally recent, which is the
// ambiguity strictness turns into a failure.
const claimedFramesRegistry = `(project
  (label "Claims on frames fixture")
  (globalid-namespace "https://example.org/models/claims-frames"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace survey (description "Claim ids issued by Acme Surveys."))

(predicate frame-transform
  (shape transform)
  (description "The rigid transform from a frame to its parent."))

(predicate crs
  (shape text)
  (claim-bearing #f)
  (description "The projected coordinate reference system the chain is rooted at."))

(predicate ground-to-grid
  (shape scalar)
  (description "The combined ground-to-grid factor."))

(predicate grid-convergence
  (unit deg)
  (shape scalar)
  (strict #t)
  (description "The angle between grid north and true north at the site."))

(frame frame:survey-grid
  (label "Site survey grid")
  (unit m)
  (crs "EPSG:25831")
  (ground-to-grid
    (id survey:C-0009)
    (value 1.0002)
    (source "Desk estimate, Acme Surveys")
    (method method:assumed)
    (date "2025-11-02")
    (rank deprecated)
    (superseded-by survey:C-0010))
  (ground-to-grid
    (id survey:C-0010)
    (value 1.0)
    (source "Georeferencing report GR-2026-002, Acme Surveys, section 4: combined factor")
    (method method:gnss-static)
    (date "2026-02-11"))
  (ground-to-grid
    (id survey:C-0011)
    (value 0.99992)
    (source "Georeferencing report GR-2026-002, Acme Surveys, section 5: check factor")
    (method method:gnss-static)
    (date "2026-02-11"))
  (grid-convergence
    (id survey:G-0001)
    (value 0.52 deg)
    (source "Georeferencing report GR-2026-002, Acme Surveys, section 6")
    (method method:gnss-static)
    (accuracy (independent 0.01 deg))
    (date "2026-02-11"))
  (grid-convergence
    (id survey:G-0002)
    (value 0.55 deg)
    (source "Convergence check CC-2026-001, Acme Surveys")
    (method method:gnss-static)
    (accuracy (independent 0.01 deg))
    (date "2026-02-11")))

(frame frame:site
  (label "Site setting-out grid")
  (unit m)
  (parent frame:survey-grid)
  (transform survey:C-0001)
  (frame-transform
    (id survey:C-0001)
    (value
      (transform
        (translation 100.0 200.0 0.0)
        (rotation 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0)
        (scale 1.0)))
    (source "Georeferencing report GR-2026-002, Acme Surveys")
    (method method:gnss-static)
    (accuracy (independent 0.012 m))
    (date "2026-02-11")))
`

// claimedFrames is the frame fixture claims and resolve are asked about.
func claimedFrames() map[string]string {
	return map[string]string{"registry.dfc": claimedFramesRegistry}
}

func TestRunClaimsAnswersAFrameID(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "lists the one claim written on a frame",
			args:     []string{"frame:site"},
			expected: []string{"frame:site frame-transform survey:C-0001 current"},
		},
		{
			name: "lists every claim on a frame, live and retracted, each with its resolution",
			args: []string{"frame:survey-grid"},
			expected: []string{
				"frame:survey-grid grid-convergence survey:G-0001 tied",
				"frame:survey-grid grid-convergence survey:G-0002 tied",
				"frame:survey-grid ground-to-grid survey:C-0009 retracted",
				"frame:survey-grid ground-to-grid survey:C-0010 tied",
				"frame:survey-grid ground-to-grid survey:C-0011 tied",
			},
		},
		{
			name: "narrows a frame to the predicate written after its id",
			args: []string{"frame:survey-grid", "ground-to-grid"},
			expected: []string{
				"frame:survey-grid ground-to-grid survey:C-0009 retracted",
				"frame:survey-grid ground-to-grid survey:C-0010 tied",
				"frame:survey-grid ground-to-grid survey:C-0011 tied",
			},
		},
		{
			name: "marks the claims under a strict predicate on a frame tied as under any other",
			args: []string{"frame:survey-grid", "--predicate", "grid-convergence"},
			expected: []string{
				"frame:survey-grid grid-convergence survey:G-0001 tied",
				"frame:survey-grid grid-convergence survey:G-0002 tied",
			},
		},
		{
			name:     "reports no plain value written on a frame, which is not a claim",
			args:     []string{"frame:survey-grid", "crs"},
			expected: []string{},
		},
		{
			name:     "lists nothing on a frame for a family filter the frame is not",
			args:     []string{"frame:site", "--family", familyNode},
			expected: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stdout, stderr := invoke(t, exitSuccess, tree(t, claimedFrames()), append([]string{"claims"}, testCase.args...)...)
			assert.Empty(t, stderr)

			result := listed[claimsResult](t, stdout)
			assert.Equal(t, outputVersion, result.Version)
			assert.Equal(t, testCase.args[0], result.Subject)
			assert.Equal(t, testCase.expected, rows(result.Claims))

			for _, row := range result.Claims {
				assert.Equal(t, familyFrame, row.Family)
				assert.Empty(t, row.Type, "a frame declares no type")
				assert.False(t, row.Retired)
			}
		})
	}
}

// TestRunClaimsMarksARetractedClaimOnAFrameWithWhatReplacedIt checks the one
// field a retraction adds, on a frame as on a node: the claim which replaced it,
// so the retraction is followable forward without a second call.
func TestRunClaimsMarksARetractedClaimOnAFrameWithWhatReplacedIt(t *testing.T) {
	stdout, _ := invoke(t, exitSuccess, tree(t, claimedFrames()), "claims", "frame:survey-grid", "ground-to-grid")

	claims := listed[claimsResult](t, stdout).Claims
	require.NotEmpty(t, claims)

	retracted := claims[0]
	assert.Equal(t, "survey:C-0009", retracted.ID)
	assert.Equal(t, resolutionRetracted, retracted.Resolution)
	assert.Equal(t, "deprecated", retracted.Rank)
	assert.Equal(t, "survey:C-0010", retracted.SupersededBy)
}

// TestRunClaimsOfAFrameIsWhatGetReportsOfIt is the property which holds of a
// node: the live claims the audit view lists on a subject are exactly the claims
// get reports on it, in the same order, and the audit view adds only what
// resolution made of each.
func TestRunClaimsOfAFrameIsWhatGetReportsOfIt(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
	}{
		{name: "holds of every frame of the claims fixture", files: claimedFrames()},
		{name: "holds of every frame of the get fixture", files: framesTree()},
		{name: "holds of every frame of the bare fixture", files: map[string]string{"registry.dfc": bareFrameRegistry}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, testCase.files)

			registry, diags := dfcad.LoadRegistry(root)
			require.Empty(t, diags)

			var declared int
			for frame := range registry.Frames() {
				declared++

				audit, _ := invoke(t, exitSuccess, root, "claims", string(frame.ID))
				retrieval, _ := invoke(t, exitSuccess, root, "get", string(frame.ID), "--claims", claimsFull)

				live := make([]claimEntry, 0)
				for _, row := range listed[claimsResult](t, audit).Claims {
					if row.Resolution == resolutionRetracted {
						continue
					}
					row.Resolution = ""
					live = append(live, row.claimEntry)
				}

				assert.Equal(t, listed[getResult](t, retrieval).Entity.Claims, live, frame.ID)
			}
			require.Positive(t, declared, "the fixture declares a frame")
		})
	}
}
