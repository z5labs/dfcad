// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// planRegistry is the vocabulary the storey below is read and annotated with.
//
// `caption` is claim-bearing and text-shaped, which is what makes what a sheet
// prints against a room a claim rather than a label: it was stated by somebody,
// from a source, on a date, and a value written bare could say none of that.
const planRegistry = `(project
  (label "Plan fixture")
  (globalid-namespace "https://example.org/models/plan"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))

(frame frame:building (label "Building local grid") (unit m))

(type Campus (kind Zone) (geometry absent) (description "Things administered together."))
(type OfficeBuilding (kind Building) (geometry solid) (description "A building let as offices."))
(type OfficeStorey (kind Storey) (geometry solid) (description "One floor plate of a building."))
(type MeetingRoom (kind Space) (geometry area) (description "An enclosed room used for meetings."))
(type Doorway (kind Element) (geometry line) (description "An opening formed through a partition."))

(predicate area (unit m2) (shape scalar) (description "How much floor a space has."))
(predicate caption (shape text) (description "What a sheet prints against a thing."))
(predicate position (unit m) (shape coordinate) (dimension 3)
  (description "The location of a vertex in its frame."))
(predicate wall-length (unit m) (shape scalar) (description "How far a run of wall reaches."))

(tolerance coincident (value 0.005 m)
  (description "How far apart two corners may be and still be one point."))
`

// planGeometry is two rooms sharing a party wall.
//
// The shared edge is measured twice and never reconciled, which is the case the
// answer has to report rather than decide: both statements are live, and which
// of them a sheet prints is the sheet's.
const planGeometry = `(vertex geom:V-01 (frame frame:building)
  (position (value (0.0 0.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-02 (frame frame:building)
  (position (value (4.0 0.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-03 (frame frame:building)
  (position (value (4.0 3.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-04 (frame frame:building)
  (position (value (0.0 3.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-05 (frame frame:building)
  (position (value (8.0 0.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-06 (frame frame:building)
  (position (value (8.0 3.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-01 (frame frame:building) (vertices geom:V-01 geom:V-02)
  (wall-length (value 4.0 m) (source "Set-out drawing SD-2026-001") (method method:tape)
    (accuracy (independent 0.01 m)) (date "2026-03-01")))

(edge geom:E-02 (frame frame:building) (vertices geom:V-02 geom:V-03)
  (wall-length (id site:M-0001) (value 3.0 m) (source "Set-out drawing SD-2026-001") (method method:tape)
    (accuracy (independent 0.01 m)) (date "2026-03-01"))
  (wall-length (id site:M-0002) (value 3.02 m) (source "As-built check AB-2026-002") (method method:total-station)
    (accuracy (independent 0.01 m)) (date "2026-05-06")))

(edge geom:E-03 (frame frame:building) (vertices geom:V-03 geom:V-04))
(edge geom:E-04 (frame frame:building) (vertices geom:V-04 geom:V-01))
(edge geom:E-05 (frame frame:building) (vertices geom:V-02 geom:V-05))
(edge geom:E-06 (frame frame:building) (vertices geom:V-05 geom:V-06))
(edge geom:E-07 (frame frame:building) (vertices geom:V-06 geom:V-03))

(loop geom:L-01 (frame frame:building) (edges geom:E-01 geom:E-02 geom:E-03 geom:E-04))
(loop geom:L-02 (frame frame:building) (edges geom:E-05 geom:E-06 geom:E-07 geom:E-02))
`

// planEntities is the storey, what it holds, an empty storey beside it and a
// zone which is not a place at all.
const planEntities = `(node site:B-01
  (label "Block A")
  (kind Building)
  (type OfficeBuilding)
  (geometry solid)
  (frame frame:building))

(node site:L-01
  (label "Level 1")
  (kind Storey)
  (type OfficeStorey)
  (geometry solid)
  (frame frame:building)
  (within site:B-01))

; A storey nobody has outlined yet. It is an empty answer rather than a
; refusal: that is what it looks like in plan.
(node site:L-02
  (label "Level 2")
  (kind Storey)
  (type OfficeStorey)
  (geometry solid)
  (frame frame:building)
  (within site:B-01))

(node site:R-01
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-01)
  (area
    (value 12.0 m2)
    (source "As-built check AB-2026-002")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-05-06"))
  (caption
    (value "Meeting Room A")
    (source "Room schedule RS-2026-001")
    (method method:schedule)
    (date "2026-03-01"))
  (caption
    (value "MR-A")
    (source "Fit-out drawing FD-2026-004")
    (method method:schedule)
    (date "2026-04-02")))

(node site:R-02
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-02))

(node site:D-01
  (label "Doorway into Room A")
  (kind Element)
  (type Doorway)
  (geometry line)
  (frame frame:building)
  (within site:L-01))

(node site:Z-01
  (label "Level 1 occupancy zone")
  (kind Zone)
  (type Campus))
`

// planFixture is the tree the runs below are made against.
func planFixture() map[string]string {
	return map[string]string{
		"registry.dfc":          planRegistry,
		"entities/model.dfc":    planEntities,
		"entities/geometry.dfc": planGeometry,
	}
}

// planned runs plan over the fixture with the arguments given, requiring the
// exit code asked for, and returns the object on stdout and what went to
// stderr.
func planned(t *testing.T, expectedExit int, args ...string) (planResult, string) {
	t.Helper()

	t.Chdir(tree(t, planFixture()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, expectedExit, run(append([]string{"plan"}, args...), &stdout, &stderr), stderr.String())

	if stdout.Len() == 0 {
		return planResult{}, stderr.String()
	}

	return listed[planResult](t, stdout.String()), stderr.String()
}

// wholeStorey is the ordinary invocation: the level, read with the project's
// own words, annotated with every predicate a sheet of it would print.
func wholeStorey(id string) []string {
	return []string{
		"--annotate", "area",
		"--annotate", "caption",
		"--annotate", "wall-length",
		"--position", "position",
		"--tolerance", "coincident",
		id,
	}
}

// outlined is the id of each outline, which is what a case asserting about which
// nodes were reported and in what order reads.
func outlined(result planResult) []string {
	out := make([]string, 0, len(result.Outlines))
	for _, outline := range result.Outlines {
		out = append(out, outline.Node)
	}
	return out
}

// anchored is every claim of one outline as "predicate @ kind id", which is the
// pairing the answer exists to carry.
func anchored(result planResult, node string) []string {
	var out []string

	for _, outline := range result.Outlines {
		if outline.Node != node {
			continue
		}
		for _, annotation := range outline.Annotations {
			out = append(out, annotation.Predicate+" @ "+annotation.Anchor.Kind+" "+annotation.Anchor.ID)
		}
	}

	return out
}

func TestRunPlan(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedOutlines []string
	}{
		{
			name:             "reports every ring the storey contains, in id order",
			args:             wholeStorey("site:L-01"),
			expectedOutlines: []string{"site:R-01", "site:R-02"},
		},
		{
			name:             "reports a storey nobody has outlined as no rings at all",
			args:             wholeStorey("site:L-02"),
			expectedOutlines: []string{},
		},
		{
			name:             "reaches through the building to the storey below it",
			args:             wholeStorey("site:B-01"),
			expectedOutlines: []string{"site:R-01", "site:R-02"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, _ := planned(t, exitSuccess, testCase.args...)

			assert.Equal(t, outputVersion, result.Version)
			assert.Equal(t, "plan", result.Command)
			assert.Equal(t, testCase.expectedOutlines, outlined(result))
			assert.True(t, result.Planned)
		})
	}
}

// TestRunPlanCarriesTheProvenanceOfEveryClaim is its own function because it is
// the criterion the whole command exists for: a rendered string is a claim and
// not a formatting of a number.
func TestRunPlanCarriesTheProvenanceOfEveryClaim(t *testing.T) {
	result, _ := planned(t, exitSuccess, wholeStorey("site:L-01")...)

	var area annotationEntry
	for _, outline := range result.Outlines {
		for _, annotation := range outline.Annotations {
			if outline.Node == "site:R-01" && annotation.Predicate == "area" {
				area = annotation
			}
		}
	}

	require.NotNil(t, area.Value.Scalar)
	assert.InDelta(t, 12.0, *area.Value.Scalar, 1e-9)
	assert.Equal(t, "m2", area.Value.Unit)
	assert.Equal(t, "As-built check AB-2026-002", area.Source)
	assert.Equal(t, "method:total-station", area.Method)
	assert.Equal(t, "2026-05-06", area.Date)
	assert.Equal(t, []accuracyTerm{{Kind: "independent", Magnitude: 0.05, Unit: "m2"}}, area.Accuracy)

	// Nothing was resolved, so nothing says which claim won.
	assert.Empty(t, area.Resolution)
}

// TestRunPlanAnchorsEveryClaimToWhatItIsWrittenOn is its own function because
// the anchor is what stops a consumer re-deriving which claim belongs to which
// pair of corners.
func TestRunPlanAnchorsEveryClaimToWhatItIsWrittenOn(t *testing.T) {
	result, _ := planned(t, exitSuccess, wholeStorey("site:L-01")...)

	assert.Equal(t, []string{
		"area @ node site:R-01",
		"caption @ node site:R-01",
		"caption @ node site:R-01",
		"wall-length @ edge geom:E-01",
		"wall-length @ edge geom:E-02",
		"wall-length @ edge geom:E-02",
	}, anchored(result, "site:R-01"))

	for _, outline := range result.Outlines {
		for _, annotation := range outline.Annotations {
			switch annotation.Anchor.Kind {
			case "node":
				assert.NotEmpty(t, annotation.Anchor.Rings, "a node anchor names its rings")
				assert.Empty(t, annotation.Anchor.Vertices)
			case "edge":
				assert.Len(t, annotation.Anchor.Vertices, 2, "an edge anchor names its two corners")
				assert.Empty(t, annotation.Anchor.Rings)
			default:
				t.Fatalf("unexpected anchor kind %q", annotation.Anchor.Kind)
			}
		}
	}

	// The edge is authored once, so both rooms report the same two corners in
	// the same order however their rings traverse it.
	assert.Equal(t,
		vertices(result, "site:R-01", "geom:E-02"),
		vertices(result, "site:R-02", "geom:E-02"),
	)
	assert.Equal(t, []string{"geom:V-02", "geom:V-03"}, vertices(result, "site:R-01", "geom:E-02"))
}

// vertices is the two corners one outline's claim on an edge is anchored to.
func vertices(result planResult, node, edge string) []string {
	for _, outline := range result.Outlines {
		if outline.Node != node {
			continue
		}
		for _, annotation := range outline.Annotations {
			if annotation.Anchor.ID == edge {
				return annotation.Anchor.Vertices
			}
		}
	}
	return nil
}

// TestRunPlanReportsBothCompetingClaims is its own function because it asserts
// that resolution is not applied rather than that it is applied a certain way.
func TestRunPlanReportsBothCompetingClaims(t *testing.T) {
	result, _ := planned(t, exitSuccess, wholeStorey("site:L-01")...)

	var lengths []float64
	for _, outline := range result.Outlines {
		if outline.Node != "site:R-01" {
			continue
		}
		for _, annotation := range outline.Annotations {
			if annotation.Anchor.ID == "geom:E-02" {
				require.NotNil(t, annotation.Value.Scalar)
				lengths = append(lengths, *annotation.Value.Scalar)
			}
		}
	}

	assert.Equal(t, []float64{3.0, 3.02}, lengths, "both live claims come back; the sheet decides")

	// The caption is the same shape of answer: two live text claims, neither
	// retracted, and no rule here to pick between them.
	var captions []string
	for _, outline := range result.Outlines {
		for _, annotation := range outline.Annotations {
			if annotation.Predicate == "caption" {
				require.NotNil(t, annotation.Value.Text)
				captions = append(captions, *annotation.Value.Text)
			}
		}
	}

	assert.Equal(t, []string{"Meeting Room A", "MR-A"}, captions)
}

// TestRunPlanEchoesThePredicatesItWasAskedFor is its own function because a
// predicate nothing is written under is the answer to "why is there no width on
// this wall", and dropping it from the echo would leave that unanswerable.
func TestRunPlanEchoesThePredicatesItWasAskedFor(t *testing.T) {
	result, _ := planned(t, exitSuccess,
		"--annotate", "area",
		"--annotate", "area",
		"--annotate", "wall-length",
		"--position", "position",
		"--tolerance", "coincident",
		"site:L-02",
	)

	assert.Equal(t, []string{"area", "wall-length"}, result.Annotating)
	assert.Empty(t, result.Outlines)
}

// TestRunPlanReportsTheDigestTheBudgetAndTheTolerance is its own function
// because those are what say which model a sheet was drawn from and how well
// the lines on it are known.
func TestRunPlanReportsTheDigestTheBudgetAndTheTolerance(t *testing.T) {
	result, _ := planned(t, exitSuccess, wholeStorey("site:L-01")...)

	assert.NotEmpty(t, result.Digest)
	assert.Equal(t, "frame:building", result.Frame)
	assert.Equal(t, "m", result.Unit)

	require.NotNil(t, result.Tolerance)
	assert.Equal(t, "coincident", result.Tolerance.Name)

	require.NotNil(t, result.Budget)
	require.NotNil(t, result.Budget.Combined)
	assert.Positive(t, result.Budget.Combined.Magnitude)
	assert.Equal(t, "m", result.Budget.Combined.Unit)

	// The budget is the accuracy of the rings. Every term behind it came from a
	// position claim, so every one of them is a length.
	for _, term := range result.Budget.Terms {
		assert.Equal(t, "m", term.Unit, term.Name)
	}
}

// TestRunPlanRefusesAnInvocationWithNoVocabulary is its own function because
// naming every missing flag at once is what makes fixing it one edit.
func TestRunPlanRefusesAnInvocationWithNoVocabulary(t *testing.T) {
	testCases := []struct {
		name            string
		args            []string
		expectedMissing []string
	}{
		{
			name:            "names all three when none was given",
			args:            []string{"site:L-01"},
			expectedMissing: []string{"annotate", "position", "tolerance"},
		},
		{
			name:            "names the predicate to annotate with when that is the one left out",
			args:            []string{"--position", "position", "--tolerance", "coincident", "site:L-01"},
			expectedMissing: []string{"annotate"},
		},
		{
			name:            "names the tolerance when that is the one left out",
			args:            []string{"--annotate", "area", "--position", "position", "site:L-01"},
			expectedMissing: []string{"tolerance"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, stderr := planned(t, exitUsage, testCase.args...)

			for _, flag := range testCase.expectedMissing {
				assert.Contains(t, stderr, "--"+flag)
			}
		})
	}
}

// TestRunPlanRefusesWhatIsNotAPlace is its own function because answering
// "nothing is in here" for a zone would be a quieter wrong answer than refusing
// the question.
func TestRunPlanRefusesWhatIsNotAPlace(t *testing.T) {
	_, stderr := planned(t, exitUsage, wholeStorey("site:Z-01")...)

	assert.Contains(t, stderr, "site:Z-01")
	assert.Contains(t, stderr, "Zone")
}

// TestRunPlanRefusesAPredicateNobodyDeclared is its own function because a
// predicate nobody declared and a predicate nothing is claimed under are
// different answers, and a caller which cannot tell them apart retries a
// misspelling forever.
func TestRunPlanRefusesAPredicateNobodyDeclared(t *testing.T) {
	_, stderr := planned(t, exitUsage,
		"--annotate", "area",
		"--annotate", "aera",
		"--position", "position",
		"--tolerance", "coincident",
		"site:L-01",
	)

	assert.Contains(t, stderr, "aera")
	assert.Contains(t, stderr, "wall-length", "the refusal lists what the registry does declare")
}

// TestRunPlanRefusesAnIDNothingHolds is its own function because the shape of
// the refusal is the one every read command gives.
func TestRunPlanRefusesAnIDNothingHolds(t *testing.T) {
	_, stderr := planned(t, exitUsage, wholeStorey("site:L-99")...)

	assert.Contains(t, stderr, "site:L-99")
}

// TestRunPlanWritesTheSameBytesTwice is its own function because determinism is
// a promise about the whole payload rather than about any entry of it.
func TestRunPlanWritesTheSameBytesTwice(t *testing.T) {
	t.Chdir(tree(t, planFixture()))

	written := make([]string, 2)
	for i := range written {
		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess,
			run(append([]string{"plan"}, wholeStorey("site:L-01")...), &stdout, &stderr), stderr.String())
		written[i] = stdout.String()
	}

	assert.Equal(t, written[0], written[1])
}

// TestRunPlanHumanOutputStaysOffStdout is its own function because the two
// streams are the contract: stdout is the same bytes whether or not anybody
// asked to read the run.
func TestRunPlanHumanOutputStaysOffStdout(t *testing.T) {
	t.Chdir(tree(t, planFixture()))

	var quiet, spoken bytes.Buffer
	var quietErr, spokenErr bytes.Buffer

	require.Equal(t, exitSuccess,
		run(append([]string{"plan"}, wholeStorey("site:L-01")...), &quiet, &quietErr), quietErr.String())
	require.Equal(t, exitSuccess,
		run(append([]string{"plan", "--format", "human", "-v"}, wholeStorey("site:L-01")...), &spoken, &spokenErr),
		spokenErr.String())

	assert.Equal(t, quiet.String(), spoken.String())
	assert.Contains(t, spokenErr.String(), "site:L-01: 2 outlines")
	assert.Contains(t, spokenErr.String(), "Meeting Room A")

	// Whatever a person is shown, no JSON value on stdout carries it.
	var payload map[string]any
	require.NoError(t, json.Unmarshal(quiet.Bytes(), &payload))
	assert.NotContains(t, strings.ToLower(quiet.String()), "outlines,")
}
