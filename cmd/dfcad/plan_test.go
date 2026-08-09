// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"encoding/json"
	"math"
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

; The doorway through the party wall. Its shape is an open run of edges along
; the wall it pierces, which does not close and is not asked to: a ring here
; would be a rectangle standing beside the wall rather than an opening through
; it.
(vertex geom:V-21 (frame frame:building)
  (position (value (4.0 1.0 0.0) m) (source "Fit-out drawing FD-2026-004") (method method:tape)
    (accuracy (independent 0.01 m)) (date "2026-03-02")))
(vertex geom:V-22 (frame frame:building)
  (position (value (4.0 1.9 0.0) m) (source "Fit-out drawing FD-2026-004") (method method:tape)
    (accuracy (independent 0.01 m)) (date "2026-03-02")))
(vertex geom:V-23 (frame frame:building)
  (position (value (4.0 2.6 0.0) m) (source "Fit-out drawing FD-2026-004") (method method:tape)
    (accuracy (independent 0.01 m)) (date "2026-03-02")))

(edge geom:E-21 (frame frame:building) (vertices geom:V-21 geom:V-22)
  (wall-length (value 0.9 m) (source "Door schedule DS-2026-001") (method method:schedule)
    (accuracy (independent 0.005 m)) (date "2026-03-01")))
(edge geom:E-22 (frame frame:building) (vertices geom:V-22 geom:V-23))

(loop geom:L-21 (frame frame:building) (edges geom:E-21 geom:E-22))
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
  (within site:L-01)
  (boundary geom:L-21))

(node site:Z-01
  (label "Level 1 occupancy zone")
  (kind Zone)
  (type Campus))
`

// planFixture is the tree the runs below are made against.
//
// Every node it holds is drawable, which is what makes it the fixture the
// unchanged-answer case is asserted against: an object written from it carries
// no "undrawn" key at all.
func planFixture() map[string]string {
	return map[string]string{
		"registry.dfc":          planRegistry,
		"entities/model.dfc":    planEntities,
		"entities/geometry.dfc": planGeometry,
	}
}

// planUndrawableTypes is the one type the fixture below adds: a thing which is
// somewhere and has no shape at all, which is what `(geometry absent)` says.
const planUndrawableTypes = `
(type CircuitGroup (kind Element) (geometry absent) (description "Outlets served by one circuit."))
`

// planUndrawableEntities is a node inside the storey which references no loop
// and carries a caption somebody wrote for a sheet.
//
// It is ordinary rather than a defect — a circuit group covers no area — and it
// is the case that used to leave the sheet without saying so.
const planUndrawableEntities = `
(node site:C-01
  (label "Level 1 lighting circuit")
  (kind Element)
  (type CircuitGroup)
  (frame frame:building)
  (within site:L-01)
  (caption
    (value "C-01")
    (source "Electrical schedule ES-2026-001")
    (method method:schedule)
    (date "2026-03-01")))
`

// planUnreadableGeometry is three walls of a rectangle and no fourth. The
// traversal ends two metres from the corner it began at, which is a gap and not
// a rounding.
const planUnreadableGeometry = `
(vertex geom:V-31 (frame frame:building)
  (position (value (0.0 10.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-32 (frame frame:building)
  (position (value (4.0 10.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-33 (frame frame:building)
  (position (value (4.0 12.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-34 (frame frame:building)
  (position (value (0.0 12.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-31 (frame frame:building) (vertices geom:V-31 geom:V-32))
(edge geom:E-32 (frame frame:building) (vertices geom:V-32 geom:V-33))
(edge geom:E-33 (frame frame:building) (vertices geom:V-33 geom:V-34))

(loop geom:L-31 (frame frame:building) (edges geom:E-31 geom:E-32 geom:E-33))
`

// planUnreadableEntities is the room that ring is meant to bound, carrying a
// caption of its own.
const planUnreadableEntities = `
(node site:R-03
  (label "Meeting Room C")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-31)
  (caption
    (value "MR-C")
    (source "Room schedule RS-2026-001")
    (method method:schedule)
    (date "2026-03-01")))
`

// undrawableFixture is the tree above with the circuit group added, which is a
// node nothing is wrong with and nothing can draw.
func undrawableFixture() map[string]string {
	return map[string]string{
		"registry.dfc":          planRegistry + planUndrawableTypes,
		"entities/model.dfc":    planEntities + planUndrawableEntities,
		"entities/geometry.dfc": planGeometry,
	}
}

// unreadableFixture is the tree above with a room whose ring does not close,
// which is a node something is wrong with.
func unreadableFixture() map[string]string {
	return map[string]string{
		"registry.dfc":          planRegistry,
		"entities/model.dfc":    planEntities + planUnreadableEntities,
		"entities/geometry.dfc": planGeometry + planUnreadableGeometry,
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
			expectedOutlines: []string{"site:D-01", "site:R-01", "site:R-02"},
		},
		{
			name:             "reports a storey nobody has outlined as no rings at all",
			args:             wholeStorey("site:L-02"),
			expectedOutlines: []string{},
		},
		{
			name:             "reaches through the building to the storey below it",
			args:             wholeStorey("site:B-01"),
			expectedOutlines: []string{"site:D-01", "site:R-01", "site:R-02"},
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

// TestRunPlanNamesANodeItCannotDraw is its own function because it is about a
// model with a node in it nothing can draw, which is a different tree from the
// one every case above reads.
//
// The circuit group is inside the storey, carries a caption somebody wrote for a
// sheet, and references no loop. It used to leave the answer with no entry, no
// diagnostic and exit 0 — which is a sheet that renders, looks complete and is
// missing something, and is the one failure nothing downstream can detect.
func TestRunPlanNamesANodeItCannotDraw(t *testing.T) {
	t.Chdir(tree(t, undrawableFixture()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess,
		run(append([]string{"plan"}, wholeStorey("site:L-01")...), &stdout, &stderr), stderr.String())

	result := listed[planResult](t, stdout.String())

	t.Run("still succeeds, because a node with no shape is not a defect", func(t *testing.T) {
		assert.True(t, result.Planned)
		assert.Equal(t, []string{"site:D-01", "site:R-01", "site:R-02"}, outlined(result))
	})

	t.Run("names it, with what it is and why it is not on the sheet", func(t *testing.T) {
		require.Len(t, result.Undrawn, 1)

		undrawn := result.Undrawn[0]
		assert.Equal(t, "site:C-01", undrawn.Node)
		assert.Equal(t, "Level 1 lighting circuit", undrawn.Label)
		assert.Equal(t, "Element", undrawn.Kind)
		assert.Equal(t, "CircuitGroup", undrawn.Type)
		assert.Equal(t, "no-boundary", undrawn.Reason)
	})

	t.Run("hands back the claim written on it, whole", func(t *testing.T) {
		require.Len(t, result.Undrawn[0].Annotations, 1)

		annotation := result.Undrawn[0].Annotations[0]
		assert.Equal(t, "caption", annotation.Predicate)
		assert.Equal(t, "node", annotation.Anchor.Kind)
		assert.Equal(t, "site:C-01", annotation.Anchor.ID)
		assert.Empty(t, annotation.Anchor.Rings, "a node with no loop is anchored to no ring")
		assert.Equal(t, "Electrical schedule ES-2026-001", annotation.Source)
	})
}

// TestRunPlanNamesARingItCouldNotRead is its own function because the reason is
// a different one — one an author fixes — and the run it produces is a refusal
// rather than an answer.
func TestRunPlanNamesARingItCouldNotRead(t *testing.T) {
	t.Chdir(tree(t, unreadableFixture()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitCheck, run(append([]string{"plan"}, wholeStorey("site:L-01")...), &stdout, &stderr))

	result := listed[planResult](t, stdout.String())

	t.Run("refuses the run and draws the rest of the storey anyway", func(t *testing.T) {
		assert.False(t, result.Planned)
		assert.Equal(t, []string{"site:D-01", "site:R-01", "site:R-02"}, outlined(result))
	})

	t.Run("says which reason in the object rather than only on stderr", func(t *testing.T) {
		require.Len(t, result.Undrawn, 1)

		assert.Equal(t, "site:R-03", result.Undrawn[0].Node)
		assert.Equal(t, "unreadable-boundary", result.Undrawn[0].Reason)

		// The detail belongs to the diagnostic, which is where an author reads
		// what to change: the loop, the file, the position and the size of the
		// gap. The object says which reason, which is what a consumer decides on.
		assert.Contains(t, stderr.String(), "geom:L-31")
		assert.Contains(t, stderr.String(), "to close")
	})

	t.Run("hands back the claim written on it", func(t *testing.T) {
		require.Len(t, result.Undrawn[0].Annotations, 1)
		assert.Equal(t, "caption", result.Undrawn[0].Annotations[0].Predicate)
	})

	t.Run("does not also report it as an outline covering nothing", func(t *testing.T) {
		assert.NotContains(t, outlined(result), "site:R-03")
	})
}

// TestRunPlanSaysNothingAboutUndrawnNodesWhereEverythingDrew is its own function
// because it asserts an absence from the object: a storey every node of which
// was drawn produces exactly the bytes it always did.
func TestRunPlanSaysNothingAboutUndrawnNodesWhereEverythingDrew(t *testing.T) {
	t.Chdir(tree(t, planFixture()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess,
		run(append([]string{"plan"}, wholeStorey("site:L-01")...), &stdout, &stderr), stderr.String())

	var payload map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &payload))

	assert.NotContains(t, payload, "undrawn",
		"a key a consumer has to read to learn nothing is a key which should not be there")
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

	// Nothing was accumulated, and an empty budget object — no terms, no
	// combined figure and no reason for there being none — would read as a plan
	// whose rings are known exactly.
	assert.Nil(t, result.Budget)
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
	testCases := []struct {
		name         string
		fixture      map[string]string
		expectedExit int
	}{
		{name: "over a storey every node of which draws", fixture: planFixture(), expectedExit: exitSuccess},
		{name: "over one holding a node with no shape", fixture: undrawableFixture(), expectedExit: exitSuccess},
		{name: "over one holding a ring which will not read", fixture: unreadableFixture(), expectedExit: exitCheck},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, testCase.fixture))

			written := make([]string, 2)
			digests := make([]string, 2)
			for i := range written {
				var stdout, stderr bytes.Buffer
				require.Equal(t, testCase.expectedExit,
					run(append([]string{"plan"}, wholeStorey("site:L-01")...), &stdout, &stderr), stderr.String())

				written[i] = stdout.String()
				digests[i] = listed[planResult](t, stdout.String()).Digest
			}

			assert.Equal(t, written[0], written[1])

			// The digest keys the answer to the source tree it was read from,
			// and what could not be drawn is not part of the tree. A model
			// holding an undrawable node is keyed the way any other is.
			assert.Equal(t, digests[0], digests[1])
			assert.NotEmpty(t, digests[0])
		})
	}
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
	assert.Contains(t, spokenErr.String(), "site:L-01: 3 outlines")
	assert.Contains(t, spokenErr.String(), "Meeting Room A")

	// Whatever a person is shown, no JSON value on stdout carries it.
	var payload map[string]any
	require.NoError(t, json.Unmarshal(quiet.Bytes(), &payload))
	assert.NotContains(t, strings.ToLower(quiet.String()), "outlines,")
}

// curvedPlan runs plan over the curved fixture's level, which holds the floor
// plate with the round courtyard in it.
func curvedPlan(t *testing.T, expectedExit int, args ...string) (planResult, string) {
	t.Helper()

	root := tree(t, curved())
	stdout, stderr := invoke(t, expectedExit, root, append([]string{
		"plan",
		"--annotate", "arc-centre",
		"--position", "position",
		"--tolerance", "coincident",
	}, args...)...)

	if stdout == "" {
		return planResult{}, stderr
	}

	return listed[planResult](t, stdout), stderr
}

// TestRunPlanDrawsACurvedRingOrSaysItDidNot is its own function because a sheet
// is where a chorded boundary is least visible: a ring run straight through a
// curve looks like a wall somebody meant, and nothing downstream of the drawing
// can tell that it was not.
func TestRunPlanDrawsACurvedRingOrSaysItDidNot(t *testing.T) {
	curving := []string{"--arc-centre", "arc-centre", "--arc-through", "arc-through"}

	t.Run("draws the chords and says which edge it chorded", func(t *testing.T) {
		result, stderr := curvedPlan(t, exitSuccess, "site:L-02")

		require.True(t, result.Planned, "a bay read as its chord is a ring, and the wrong one")
		require.Len(t, result.Outlines, 1)

		// Four by four, which is the room without its bay: the sheet would be
		// drawn with a straight east wall and nothing in it would say so.
		assert.InDelta(t, 16.0, result.Outlines[0].Region.Area, 1e-9)

		require.Len(t, result.Chorded, 1)
		assert.Equal(t, "geom:E-42", result.Chorded[0].Edge)
		assert.Equal(t, []string{"arc-centre", "arc-through"}, result.Chorded[0].Predicates)

		assert.Nil(t, result.Chord, "nothing bent, because nothing read the bend")
		assert.Contains(t, stderr, "geom:E-42")
	})

	t.Run("refuses to draw a curved ring until a chord tolerance names how closely", func(t *testing.T) {
		result, stderr := curvedPlan(t, exitCheck, append(curving, "site:L-02")...)

		assert.False(t, result.Planned, "no ring, rather than one drawn at a resolution nobody chose")
		assert.Contains(t, stderr, "no chord tolerance named")
	})

	t.Run("draws the curve where the whole vocabulary is named", func(t *testing.T) {
		result, _ := curvedPlan(t, exitSuccess, append(curving, "--chord", "chord-deviation", "site:L-02")...)

		require.True(t, result.Planned)
		require.Len(t, result.Outlines, 1)

		// Four by four with a semicircular bay of radius two on one side, drawn
		// as chords which lie inside the curve everywhere.
		region := result.Outlines[0].Region
		assert.Greater(t, region.Area, 16.0, "the bay is on the sheet now")
		assert.Less(t, region.Area, 16+2*math.Pi, "and the chords of it lie inside the curve")

		require.Len(t, region.Pieces, 1)
		assert.Greater(t, len(region.Pieces[0].Outer), 4, "a drawn bay is more than the room's four corners")

		require.NotNil(t, result.Chord)
		assert.Equal(t, "chord-deviation", result.Chord.Name)

		require.NotNil(t, result.Deviation)
		assert.Positive(t, result.Deviation.Value)
		assert.LessOrEqual(t, result.Deviation.Value, result.Chord.Value)

		assert.Empty(t, result.Chorded)
	})

	t.Run("says nothing about curves for a storey which claims none", func(t *testing.T) {
		result, _ := planned(t, exitSuccess, wholeStorey("site:L-01")...)

		assert.Empty(t, result.Chorded)
		assert.Nil(t, result.Chord)
		assert.Nil(t, result.Deviation)
	})
}

func TestRunPlanRefusesHalfTheArcVocabulary(t *testing.T) {
	root := tree(t, curved())

	_, stderr := invoke(t, exitUsage, root,
		"plan", "--annotate", "arc-centre", "--position", "position", "--tolerance", "coincident",
		"--arc-centre", "arc-centre", "site:L-02")

	assert.Contains(t, stderr, "--arc-through")
}

// TestRunPlanDrawsAnOpenRun is its own function because what an open run
// contributes to the contract is a different shape of entry from a room's: no
// pieces, no area, and a boundary which is the whole of what a sheet draws it
// from.
func TestRunPlanDrawsAnOpenRun(t *testing.T) {
	result, stderr := planned(t, exitSuccess, wholeStorey("site:L-01")...)

	// One door does not refuse the storey. The plan is written, the rooms are
	// in it, and nothing was reported against the model.
	require.True(t, result.Planned, stderr)

	var doorway outlineEntry
	for _, outline := range result.Outlines {
		if outline.Node == "site:D-01" {
			doorway = outline
		}
	}
	require.Equal(t, "site:D-01", doorway.Node)

	t.Run("covers nothing, and says so rather than being left out", func(t *testing.T) {
		assert.True(t, doorway.Region.Empty)
		assert.Zero(t, doorway.Region.Area)
		assert.Empty(t, doorway.Region.Pieces)
		assert.Equal(t, "Element", doorway.Kind)
	})

	t.Run("names the edge behind every straight run of it, in walked order", func(t *testing.T) {
		require.Len(t, doorway.Region.Boundary, 2)

		assert.Equal(t, "geom:E-21", doorway.Region.Boundary[0].Edge)
		assert.Equal(t, "edge", doorway.Region.Boundary[0].Origin)
		assert.Equal(t, []float64{4.0, 1.0, 0.0}, doorway.Region.Boundary[0].From)
		assert.Equal(t, []float64{4.0, 1.9, 0.0}, doorway.Region.Boundary[0].To)

		// The last run arrives at the far end of the chain rather than turning
		// back onto the corner it began at, which is the whole difference
		// between a run and a ring.
		assert.Equal(t, "geom:E-22", doorway.Region.Boundary[1].Edge)
		assert.Equal(t, []float64{4.0, 2.6, 0.0}, doorway.Region.Boundary[1].To)
	})

	t.Run("carries the claims written on the edges of the run", func(t *testing.T) {
		assert.Equal(t, []string{"wall-length @ edge geom:E-21"}, anchored(result, "site:D-01"))
	})
}
