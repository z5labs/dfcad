// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// answerRegistry is the vocabulary the model below is judged against.
//
// The two frames are a chain rather than one frame, because a cross-frame answer
// is the case where the accuracy of the answer is not the accuracy of the claim
// it came from. The fit between them shares a control point with the position
// claim below, which is what says a shared term is counted once rather than
// twice.
//
// `bearing` is declared strict and `heading` is the same shape of claim with the
// flag left off, so what separates a finding from a failure is the registry and
// nothing about the claims.
const answerRegistry = `(project
  (label "Answer fixture")
  (globalid-namespace "https://example.org/models/answer"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
(namespace survey
  (description "Claim ids and control points issued by Acme Surveys."))

(type Campus
  (kind Zone)
  (geometry absent)
  (description "A group of things administered together, which has no shape."))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings."))

(predicate area
  (unit m2)
  (shape scalar)
  (description "How much floor a space has."))

(predicate bearing
  (unit deg)
  (shape scalar)
  (strict #t)
  (description "Which way the thing faces, clockwise from grid north."))

(predicate heading
  (unit deg)
  (shape scalar)
  (description "Which way the thing faces, to nobody's great concern."))

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

(predicate frame-transform
  (shape transform)
  (description "The rigid transform from a frame to its parent."))

(frame frame:site-grid (label "Site survey grid") (unit m))

(frame frame:building
  (label "Building local grid")
  (unit m)
  (parent frame:site-grid)
  (transform survey:C-0001)
  (frame-transform
    (id survey:C-0001)
    (value
      (transform
        (translation 100.0 200.0 0.0)
        (rotation 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0)
        (scale 1.0)))
    (source "Setting-out record SO-2026-014, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m) (systematic 0.008 m survey:CP-3))
    (date "2026-03-02")))
`

// answerModel is the semantic family, written so that every outcome a
// resolution can have is somewhere in it.
//
// The campus is claimed about at all, which is the subject nobody has measured.
// Room A is where the rule decides on accuracy, ties on both, and is asked under
// a predicate nothing rankable was said about. Room B is where accuracy ties and
// the date separates them. Room C has one live claim under one predicate and two
// unrankable ones under another.
const answerModel = `(node site:Z-01
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
  (bearing
    (id survey:B-0001)
    (value 91.0 deg)
    (source "Setting-out record SO-2026-014, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 deg))
    (date "2026-03-02"))
  (bearing
    (id survey:B-0002)
    (value 91.2 deg)
    (source "Setting-out check SO-2026-021, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 deg))
    (date "2026-03-02"))
  (heading
    (id survey:B-0003)
    (value 91.0 deg)
    (source "Setting-out record SO-2026-014, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 deg))
    (date "2026-03-02"))
  (heading
    (id survey:B-0004)
    (value 91.2 deg)
    (source "Setting-out check SO-2026-021, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 deg))
    (date "2026-03-02"))
  (note
    (id survey:N-0001)
    (value "Booked through the front desk.")
    (source "Facilities handbook, 2026 edition")
    (method method:assumed)
    (date "2026-02-01")))

(node site:S-102
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (area
    (id survey:A-0004)
    (value 18.0 m2)
    (source "Plan set A-101, sheet 3")
    (method method:scaled-from-plan)
    (accuracy (independent 0.05 m2))
    (date "2026-01-09"))
  (area
    (id survey:A-0005)
    (value 18.4 m2)
    (source "As-built check AB-2026-010, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-05-06")))

(node site:S-103
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
`

// answerGeometry is a vertex in the building frame, which is what a cross-frame
// answer is asked about.
//
// Its winning claim shares the control point survey:CP-3 with the fit which
// relates the two frames, so an honest budget of a position expressed in the
// site grid counts that control point once.
const answerGeometry = `(vertex geom:V-01
  (label "Room A, north-west corner")
  (frame frame:building)
  (position
    (id survey:P-0001)
    (value (3.0 4.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m) (systematic 0.008 m survey:CP-3))
    (date "2026-02-18"))
  (position
    (id survey:P-0002)
    (value (3.01 4.0 0.0) m)
    (source "Interior control set IC-02, Acme Surveys")
    (method method:tape)
    (accuracy (independent 0.01 m))
    (date "2026-03-18")))
`

// answerable is the fixture tree resolve is run against.
func answerable() map[string]string {
	return map[string]string{
		"registry.dfc":          answerRegistry,
		"entities/site.dfc":     answerModel,
		"entities/geometry.dfc": answerGeometry,
	}
}

// answering runs resolve over the fixture and decodes what reached stdout.
//
// The exit code is part of what is being asserted rather than a detail of the
// helper, so it is given by the caller: an ambiguity which answered zero would
// otherwise pass every assertion below it.
func answering(t *testing.T, expectedCode int, args ...string) resolveResult {
	t.Helper()

	t.Chdir(tree(t, answerable()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, expectedCode, run(append([]string{"resolve"}, args...), &stdout, &stderr), stderr.String())

	// Nothing is wrong with the fixture, so nothing is on stderr. It is asserted
	// rather than assumed because a model which quietly stopped loading would
	// still answer every assertion below.
	require.Empty(t, stderr.String())

	result := listed[resolveResult](t, stdout.String())
	assert.Equal(t, outputVersion, result.Version)
	assert.Equal(t, "resolve", result.Command)

	return result
}

// claimed names each entry by its claim id and what resolution made of it, which
// is what says which claims came back and how each was treated.
func considered(claims []claimEntry) []string {
	var out []string
	for _, claim := range claims {
		out = append(out, claim.ID+" "+claim.Resolution)
	}
	return out
}

func TestRunResolve(t *testing.T) {
	testCases := []struct {
		name               string
		args               []string
		expectedCode       int
		expectedOutcome    string
		expectedReason     string
		expectedStrict     bool
		expectedValue      string
		expectedClaim      string
		expectedCandidates []string
	}{
		{
			name:            "answers with the claim whose accuracy is smallest",
			args:            []string{"site:S-101", "area"},
			expectedCode:    exitSuccess,
			expectedOutcome: outcomeResolved,
			expectedReason:  "accuracy",
			expectedValue:   "24.2 m2",
			expectedClaim:   "survey:A-0002",
		},
		{
			name:            "answers with the more recent of two claims which tie on accuracy",
			args:            []string{"site:S-102", "area"},
			expectedCode:    exitSuccess,
			expectedOutcome: outcomeResolved,
			expectedReason:  "recency",
			expectedValue:   "18.4 m2",
			expectedClaim:   "survey:A-0005",
		},
		{
			name:            "answers with the one live claim under the predicate",
			args:            []string{"site:S-103", "area"},
			expectedCode:    exitSuccess,
			expectedOutcome: outcomeResolved,
			expectedReason:  "only",
			expectedValue:   "31.2 m2",
		},
		{
			name:            "answers from an unrankable claim and marks it as one",
			args:            []string{"site:S-101", "note"},
			expectedCode:    exitSuccess,
			expectedOutcome: outcomeUnranked,
			expectedReason:  "unranked",
			expectedValue:   `"Booked through the front desk."`,
			expectedClaim:   "survey:N-0001",
		},
		{
			name:               "returns every tied claim rather than picking one",
			args:               []string{"site:S-101", "height"},
			expectedCode:       exitAmbiguous,
			expectedOutcome:    outcomeAmbiguous,
			expectedReason:     "ambiguous",
			expectedCandidates: []string{"survey:H-0001 tied", "survey:H-0002 tied"},
		},
		{
			name:               "returns every claim nothing rankable was said about",
			args:               []string{"site:S-103", "occupancy"},
			expectedCode:       exitAmbiguous,
			expectedOutcome:    outcomeAmbiguous,
			expectedReason:     "ambiguous",
			expectedCandidates: []string{"survey:O-0001 tied", "survey:O-0002 tied"},
		},
		{
			name:               "fails where the ambiguous predicate is declared strict",
			args:               []string{"site:S-101", "bearing"},
			expectedCode:       exitStrict,
			expectedOutcome:    outcomeAmbiguous,
			expectedReason:     "ambiguous",
			expectedStrict:     true,
			expectedCandidates: []string{"survey:B-0001 tied", "survey:B-0002 tied"},
		},
		{
			name:               "reports the same ambiguity where the predicate is not strict",
			args:               []string{"site:S-101", "heading"},
			expectedCode:       exitAmbiguous,
			expectedOutcome:    outcomeAmbiguous,
			expectedReason:     "ambiguous",
			expectedCandidates: []string{"survey:B-0003 tied", "survey:B-0004 tied"},
		},
		{
			name:            "reports a thing nothing is claimed about under the predicate",
			args:            []string{"site:Z-01", "area"},
			expectedCode:    exitCheck,
			expectedOutcome: outcomeUnclaimed,
			expectedReason:  "unclaimed",
		},
		{
			name:            "reports a predicate whose every claim has been retracted",
			args:            []string{"site:S-102", "height"},
			expectedCode:    exitCheck,
			expectedOutcome: outcomeUnclaimed,
			expectedReason:  "unclaimed",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := answering(t, testCase.expectedCode, testCase.args...)

			assert.Equal(t, testCase.args[0], result.Subject)
			assert.Equal(t, testCase.args[1], result.Predicate)
			assert.Equal(t, testCase.expectedOutcome, result.Outcome)
			assert.Equal(t, testCase.expectedReason, result.Reason)
			assert.Equal(t, testCase.expectedStrict, result.Strict)
			assert.Equal(t, testCase.expectedCandidates, considered(result.Candidates))

			// The audit trail is never in the default answer, whatever came of
			// the resolution. --evidence is what asks for it.
			assert.Nil(t, result.Claim)

			if testCase.expectedValue == "" {
				assert.Nil(t, result.Value)
				assert.Empty(t, result.ClaimID)
				return
			}

			// The value comes back with the unit it is in, because a number
			// without one is the thing the whole format exists to stop being
			// handed back on its own.
			require.NotNil(t, result.Value)
			assert.Equal(t, testCase.expectedValue, spellClaimValue(*result.Value))

			// And with the id of the claim it came from, so that the answer can
			// be taken back to what it came from without a second call. The
			// rest of that claim is a second question and is behind --evidence.
			assert.Equal(t, testCase.expectedClaim, result.ClaimID)
		})
	}
}

// TestRunResolveKeepsTheAuditTrailBehindAFlag is its own function because it
// asks a different question from the answer: not how big the room is, but who
// said so and where they wrote it down.
//
// The two are separated because the second was three quarters of what this
// command cost, and is wanted on a small minority of the calls that pay for it.
// See docs/decisions/0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md.
func TestRunResolveKeepsTheAuditTrailBehindAFlag(t *testing.T) {
	t.Run("leaves the claim out of the answer", func(t *testing.T) {
		result := answering(t, exitSuccess, "site:S-101", "area")

		assert.Nil(t, result.Claim)
		assert.Equal(t, "survey:A-0002", result.ClaimID)
	})

	t.Run("reports the claim in full when it is asked for", func(t *testing.T) {
		result := answering(t, exitSuccess, "--evidence", "site:S-101", "area")

		require.NotNil(t, result.Claim)
		assert.Equal(t, "survey:A-0002", result.Claim.ID)
		assert.NotEmpty(t, result.Claim.Source)
		assert.Equal(t, "2026-05-06", result.Claim.Date)
		assert.Equal(t, "method:total-station", result.Claim.Method)
		assert.NotEmpty(t, result.Claim.Span.Start.Path)

		// The answer is unchanged by having asked. --evidence adds a field; it
		// does not report a different resolution.
		assert.Equal(t, "survey:A-0002", result.ClaimID)
		require.NotNil(t, result.Value)
		assert.Equal(t, "24.2 m2", spellClaimValue(*result.Value))
	})
}

// TestRunResolveCarriesTheAccuracyOfItsAnswer is its own function because it is
// about what travels beside the number rather than about which number came back.
//
// A resolution which reported the value and dropped how well it is known would
// be handing back exactly the bare number the format exists to prevent — which
// is why the accuracy is beside the value in the default answer rather than
// inside the audit trail --evidence asks for.
func TestRunResolveCarriesTheAccuracyOfItsAnswer(t *testing.T) {
	result := answering(t, exitSuccess, "site:S-101", "area")

	require.Len(t, result.Accuracy, 1)

	assert.Equal(t, "independent", result.Accuracy[0].Kind)
	assert.InDelta(t, 0.05, result.Accuracy[0].Magnitude, 0)
	assert.Equal(t, "m2", result.Accuracy[0].Unit)
}

// TestRunResolveNamesAnAnswerWhoseClaimWroteNoID is its own function because it
// is the other end of traceability: a claim id is optional, and an answer which
// came from a claim with no name of its own is traced by where it was written,
// which is what --evidence reports.
func TestRunResolveNamesAnAnswerWhoseClaimWroteNoID(t *testing.T) {
	result := answering(t, exitSuccess, "site:S-103", "area")

	assert.Empty(t, result.ClaimID)

	evidenced := answering(t, exitSuccess, "--evidence", "site:S-103", "area")

	require.NotNil(t, evidenced.Claim)
	assert.Empty(t, evidenced.Claim.ID)
	assert.Equal(t, "entities/site.dfc", evidenced.Claim.Span.Start.Path)
	assert.Positive(t, evidenced.Claim.Span.Start.Line)
}

// TestRunResolveAuditsEveryCandidate is its own function because --candidates
// asks a different question from the answer: not what the model says, but what
// the rule weighed to say it.
func TestRunResolveAuditsEveryCandidate(t *testing.T) {
	t.Run("reports every live claim beside the winner", func(t *testing.T) {
		result := answering(t, exitSuccess, "--candidates", "site:S-101", "area")

		// The winner is named the way it is without the flag, and is among the
		// candidates in full and marked current. Reporting it twice over would
		// be the audit view answering the question as well.
		assert.Equal(t, "survey:A-0002", result.ClaimID)
		assert.Nil(t, result.Claim)

		// The retracted claim is not among them. It was never a candidate, so
		// reporting it here would say the rule weighed something it never saw.
		assert.Equal(t,
			[]string{"survey:A-0002 current", "survey:A-0003 outranked"},
			considered(result.Candidates),
		)
	})

	t.Run("reports every candidate of a resolution which chose nothing", func(t *testing.T) {
		result := answering(t, exitAmbiguous, "--candidates", "site:S-101", "height")

		assert.Equal(t,
			[]string{"survey:H-0001 tied", "survey:H-0002 tied"},
			considered(result.Candidates),
		)
	})

	t.Run("reports nothing extra where nothing was claimed", func(t *testing.T) {
		result := answering(t, exitCheck, "--candidates", "site:Z-01", "area")

		assert.Empty(t, result.Candidates)
	})
}

// TestRunResolveAcrossFrames is its own function because a cross-frame answer
// asserts about two things an ordinary one does not: the value moved, and the
// error of the answer is no longer the accuracy of the claim it came from.
func TestRunResolveAcrossFrames(t *testing.T) {
	t.Run("expresses the answer in the frame it was asked for", func(t *testing.T) {
		result := answering(t, exitSuccess, "--frame", "frame:site-grid", "geom:V-01", "position")

		require.NotNil(t, result.Value)
		assert.Equal(t, []float64{103.0, 204.0, 0.0}, result.Value.Coordinate)

		// A frame declares exactly one linear unit, so a position expressed in
		// it is in that unit rather than in the one the claim was written in.
		assert.Equal(t, "m", result.Value.Unit)
		assert.Equal(t, "frame:site-grid", result.Frame)
	})

	t.Run("reports the accumulated error broken out by term", func(t *testing.T) {
		result := answering(t, exitSuccess, "--frame", "frame:site-grid", "geom:V-01", "position")

		require.NotNil(t, result.Budget)
		assert.Equal(t, "frame:building", result.Budget.From)
		assert.Equal(t, "frame:site-grid", result.Budget.To)

		// The fit which relates the frames and the measurement itself are both
		// terms of the answer, and the control point they share is one term
		// with two contributors rather than two terms of the same magnitude.
		assert.Equal(t, []budgetTerm{
			{
				Kind:         "independent",
				Name:         "survey:C-0001",
				Magnitude:    0.004,
				Unit:         "m",
				Contributors: []string{"survey:C-0001"},
			},
			{
				Kind:         "systematic",
				Name:         "survey:CP-3",
				Magnitude:    0.008,
				Unit:         "m",
				Source:       "survey:CP-3",
				Contributors: []string{"survey:C-0001", "survey:P-0001"},
			},
			{
				Kind:         "independent",
				Name:         "survey:P-0001",
				Magnitude:    0.004,
				Unit:         "m",
				Contributors: []string{"survey:P-0001"},
			},
		}, result.Budget.Terms)

		// Independent terms in quadrature, the shared one linearly, and the two
		// together in quadrature: √(0.004² + 0.004² + 0.008²).
		require.NotNil(t, result.Budget.Combined)
		assert.InDelta(t, 0.0097980, result.Budget.Combined.Magnitude, 1e-7)
		assert.Equal(t, "m", result.Budget.Combined.Unit)
		assert.InDelta(t, 1.0, result.Budget.Combined.CoverageFactor, 0)

		assert.Empty(t, result.Budget.Unknown)
		assert.Empty(t, result.Budget.Units)
	})

	t.Run("applies no transform and reports no budget for the frame it is already in", func(t *testing.T) {
		result := answering(t, exitSuccess, "--frame", "frame:building", "geom:V-01", "position")

		require.NotNil(t, result.Value)
		assert.Equal(t, []float64{3.0, 4.0, 0.0}, result.Value.Coordinate)
		assert.Equal(t, "frame:building", result.Frame)

		// Nothing was transformed, so there is nothing to be uncertain about
		// beyond the claim, whose own accuracy is already beside it.
		assert.Nil(t, result.Budget)
	})

	t.Run("reports the frame a value is in without being asked", func(t *testing.T) {
		result := answering(t, exitSuccess, "geom:V-01", "position")

		assert.Equal(t, "frame:building", result.Frame)
		assert.Nil(t, result.Budget)
	})

	t.Run("reports no frame beside a value which is in none", func(t *testing.T) {
		result := answering(t, exitSuccess, "site:S-101", "area")

		assert.Empty(t, result.Frame)
	})
}

// TestRunResolveRejectsWhatTheModelDoesNotHold walks the invocations which named
// something that is not there.
//
// Each is a usage error rather than an empty answer, and each says which of them
// it was: a caller which cannot tell a misspelled id from an unmeasured thing
// retries the misspelling forever.
func TestRunResolveRejectsWhatTheModelDoesNotHold(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedMentions []string
	}{
		{
			name:             "refuses an invocation with nothing to resolve about",
			args:             nil,
			expectedMentions: []string{"found no argument"},
		},
		{
			name:             "refuses an invocation with no predicate to resolve under",
			args:             []string{"site:S-101"},
			expectedMentions: []string{"found only the id"},
		},
		{
			name:             "refuses more arguments than it takes",
			args:             []string{"site:S-101", "area", "m2"},
			expectedMentions: []string{"m2"},
		},
		{
			name:             "names the nearest id for one nothing holds",
			args:             []string{"site:S-1O1", "area"},
			expectedMentions: []string{"site:S-1O1", "site:S-101"},
		},
		{
			name:             "refuses a predicate the registry does not declare",
			args:             []string{"site:S-101", "widht"},
			expectedMentions: []string{"widht", "area"},
		},
		{
			name:             "refuses a frame the registry does not declare",
			args:             []string{"--frame", "frame:nowhere", "geom:V-01", "position"},
			expectedMentions: []string{"frame:nowhere", "frame:building"},
		},
		{
			name:             "refuses to express a value which is not a position in another frame",
			args:             []string{"--frame", "frame:site-grid", "site:S-101", "area"},
			expectedMentions: []string{"area", "scalar"},
		},
		{
			name:             "refuses to express a value of a thing which is written in no frame",
			args:             []string{"--frame", "frame:site-grid", "site:Z-01", "position"},
			expectedMentions: []string{"site:Z-01", "declares no frame"},
		},
		{
			// The flag is refused on a question it could never have answered
			// rather than quietly doing nothing on the way to reporting the
			// ambiguity, because a flag which is silently ignored is worse than
			// one which does not exist.
			name:             "refuses the frame flag before reporting an ambiguity it could not have answered",
			args:             []string{"--frame", "frame:site-grid", "site:S-101", "height"},
			expectedMentions: []string{"height", "scalar"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, answerable()))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitUsage, run(append([]string{"resolve"}, testCase.args...), &stdout, &stderr))

			// A run which produced no result writes no result object, and never
			// writes prose instead of one.
			assert.Empty(t, stdout.String())
			for _, mention := range testCase.expectedMentions {
				assert.Contains(t, stderr.String(), mention)
			}
		})
	}
}

// TestRunResolveDistinguishesAnUnknownSubjectFromAnUnmeasuredOne is its own
// function because it is the distinction the whole diagnostic side of this
// command turns on, and it is one assertion about two runs rather than a case in
// a table.
func TestRunResolveDistinguishesAnUnknownSubjectFromAnUnmeasuredOne(t *testing.T) {
	attempt := func(t *testing.T, args ...string) (int, string, string) {
		t.Helper()

		t.Chdir(tree(t, answerable()))

		var stdout, stderr bytes.Buffer
		code := run(append([]string{"resolve"}, args...), &stdout, &stderr)

		return code, stdout.String(), stderr.String()
	}

	unknown, unknownOut, unknownErr := attempt(t, "site:S-999", "area")
	unmeasured, unmeasuredOut, unmeasuredErr := attempt(t, "site:Z-01", "area")

	// An id nothing holds is the invocation being wrong: nothing ran, so there
	// is no result, and the diagnostic is about the id.
	assert.Equal(t, exitUsage, unknown)
	assert.Empty(t, unknownOut)
	assert.Contains(t, unknownErr, "unknown id site:S-999")

	// A thing nobody has measured is the model answering, and the answer is
	// that it says nothing. It is a result, so it is on stdout.
	assert.Equal(t, exitCheck, unmeasured)
	assert.NotEmpty(t, unmeasuredOut)
	assert.Equal(t, outcomeUnclaimed, listed[resolveResult](t, unmeasuredOut).Outcome)
	assert.Empty(t, unmeasuredErr)
}

// TestRunResolveRendersItsAnswerForAPerson is its own function because it is
// about the stream a person reads rather than about the one a caller pipes: the
// reason has to be readable as a sentence and not only as a word in an object.
func TestRunResolveRendersItsAnswerForAPerson(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedCode     int
		expectedMentions []string
	}{
		{
			name:             "says which claim won and why",
			args:             []string{"site:S-101", "area"},
			expectedCode:     exitSuccess,
			expectedMentions: []string{"24.2 m2", "accuracy is the smallest", "survey:A-0002"},
		},
		{
			name:             "says that the date broke a tie on accuracy",
			args:             []string{"site:S-102", "area"},
			expectedCode:     exitSuccess,
			expectedMentions: []string{"18.4 m2", "most recent", "survey:A-0005"},
		},
		{
			name:             "says that nothing rankable was said",
			args:             []string{"site:S-101", "note"},
			expectedCode:     exitSuccess,
			expectedMentions: []string{"nothing rankable was said"},
		},
		{
			name:             "says how many claims nothing separates",
			args:             []string{"site:S-101", "height"},
			expectedCode:     exitAmbiguous,
			expectedMentions: []string{"nothing separates 2 claims"},
		},
		{
			name:             "says that the predicate nothing separates is strict",
			args:             []string{"site:S-101", "bearing"},
			expectedCode:     exitStrict,
			expectedMentions: []string{"declared strict"},
		},
		{
			name:             "says that nothing live is written under the predicate",
			args:             []string{"site:Z-01", "area"},
			expectedCode:     exitCheck,
			expectedMentions: []string{"nothing live is written"},
		},
		{
			name:             "says what the answer cost in accuracy",
			args:             []string{"--frame", "frame:site-grid", "geom:V-01", "position"},
			expectedCode:     exitSuccess,
			expectedMentions: []string{"(103 204 0) m", "±0.0097979"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, answerable()))

			var stdout, stderr bytes.Buffer

			args := append([]string{"resolve", "--format", formatHuman}, testCase.args...)
			require.Equal(t, testCase.expectedCode, run(args, &stdout, &stderr))

			for _, mention := range testCase.expectedMentions {
				assert.Contains(t, stderr.String(), mention)
			}
		})
	}
}

// TestRunResolveHumanOutputNeverChangesStdout is its own function because it is
// the one property the format flag must not have, asserted on the command whose
// human rendering says the most.
func TestRunResolveHumanOutputNeverChangesStdout(t *testing.T) {
	answer := func(t *testing.T, args ...string) string {
		t.Helper()

		t.Chdir(tree(t, answerable()))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess,
			run(append([]string{"resolve", "site:S-101", "area"}, args...), &stdout, &stderr))

		return stdout.String()
	}

	machine := answer(t)

	assert.Equal(t, machine, answer(t, "--format", formatHuman))
	assert.Equal(t, machine, answer(t, "-v"))
	assert.Equal(t, machine, answer(t, "--format", formatHuman, "-v", "-v"))
}

// TestRunResolveReportsAFrameTheModelCannotRelate is its own function because it
// is a load failure rather than an answer: the model does not say how the two
// frames relate, and a position computed anyway would be an invented
// georeference.
func TestRunResolveReportsAFrameTheModelCannotRelate(t *testing.T) {
	files := answerable()

	// The annexe declares a parent and no fit against it, which is a
	// relationship nobody measured rather than one measured as the identity.
	files["registry.dfc"] += "\n(frame frame:annexe (label \"Annexe grid\") (unit m) (parent frame:site-grid))\n"

	t.Chdir(tree(t, files))

	var stdout, stderr bytes.Buffer
	code := run([]string{"resolve", "--frame", "frame:annexe", "geom:V-01", "position"}, &stdout, &stderr)

	require.Equal(t, exitLoad, code, stderr.String())
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "frame:annexe")
}
