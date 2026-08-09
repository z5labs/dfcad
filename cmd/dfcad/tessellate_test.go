// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// curvedRegistry is the vocabulary the curved fixture is read against.
//
// It declares the two predicates an arc is written under, which is what makes a
// curve readable at all: no form, no kind and nothing compiled into the engine
// learns a name when an arc arrives, so the names below are this fixture's and
// are handed to the command as flags.
const curvedRegistry = `(project
  (label "Tessellation fixture")
  (globalid-namespace "https://example.org/models/tessellate"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))

(frame frame:building (label "Building local grid") (unit m))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings."))

(type Campus
  (kind Zone)
  (geometry absent)
  (description "A group of things administered together, which has no shape."))

(type OfficeStorey
  (kind Storey)
  (geometry solid)
  (description "One floor plate of a building."))

(type Parcel
  (kind Site)
  (geometry area)
  (description "A plot of land with a boundary and a planning regime over it."))

(predicate position
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "The location of a vertex in its frame."))

(predicate arc-centre
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "The centre of the circle a curved edge runs on, in the frame of the edge."))

(predicate arc-through
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "A point a curved edge passes through between its two ends."))

(predicate setback
  (unit m)
  (shape scalar)
  (description "How far back from the boundary edge it is written on a structure has to sit."))

(tolerance coincident
  (value 0.005 m)
  (description "How far apart two corners may be and still be one point."))

(tolerance chord-deviation
  (value 0.01 m)
  (description "How far a straight segment standing in for a curve may fall from it."))

(tolerance coarse-chord-deviation
  (value 0.5 m)
  (description "The same, where only a rough outline is wanted."))

(tolerance hair-chord-deviation
  (value 0.000000000001 m)
  (description "Finer than anything on this project was surveyed to, and here to be refused rather than drawn."))
`

// curvedModel is a ten metre floor plate with a round courtyard in the middle of
// it: four straight walls around two arcs.
//
// It is the shape the region-level drawing exists for. Nothing measures it as it
// stands — which of two rings is inside the other is decided at their corners,
// and a ring which bulges past a corner is not a question the corners answer —
// so it is exactly the outline which has no exportable form until somebody names
// a chord tolerance and draws it.
const curvedModel = `(vertex geom:V-01
  (label "Floor plate, corner 1")
  (frame frame:building)
  (position
    (value (0.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))
(vertex geom:V-02
  (label "Floor plate, corner 2")
  (frame frame:building)
  (position
    (value (10.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))
(vertex geom:V-03
  (label "Floor plate, corner 3")
  (frame frame:building)
  (position
    (value (10.0 10.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))
(vertex geom:V-04
  (label "Floor plate, corner 4")
  (frame frame:building)
  (position
    (value (0.0 10.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))
(edge geom:E-01 (label "Floor plate, side 1") (frame frame:building) (vertices geom:V-01 geom:V-02))
(edge geom:E-02 (label "Floor plate, side 2") (frame frame:building) (vertices geom:V-02 geom:V-03))
(edge geom:E-03 (label "Floor plate, side 3") (frame frame:building) (vertices geom:V-03 geom:V-04))
(edge geom:E-04 (label "Floor plate, side 4") (frame frame:building) (vertices geom:V-04 geom:V-01))
(loop geom:L-01
  (label "Floor plate boundary")
  (frame frame:building)
  (edges geom:E-01 geom:E-02 geom:E-03 geom:E-04))

(vertex geom:V-11
  (label "Courtyard, west point")
  (frame frame:building)
  (position
    (value (3.0 5.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))
(vertex geom:V-12
  (label "Courtyard, east point")
  (frame frame:building)
  (position
    (value (7.0 5.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))
(edge geom:E-11
  (label "Courtyard, south wall")
  (frame frame:building)
  (vertices geom:V-11 geom:V-12)
  (arc-centre
    (value (5.0 5.0 0.0) m)
    (source "Setting-out report SO-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-19"))
  (arc-through
    (value (5.0 3.0 0.0) m)
    (source "Setting-out report SO-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.006 m))
    (date "2026-02-19")))
(edge geom:E-12
  (label "Courtyard, north wall")
  (frame frame:building)
  (vertices geom:V-12 geom:V-11)
  (arc-centre
    (value (5.0 5.0 0.0) m)
    (source "Setting-out report SO-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-19"))
  (arc-through
    (value (5.0 7.0 0.0) m)
    (source "Setting-out report SO-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.006 m))
    (date "2026-02-19")))
(loop geom:L-11
  (label "Courtyard boundary")
  (frame frame:building)
  (edges geom:E-11 geom:E-12))

(node site:S-01
  (label "Floor plate with a round courtyard")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (boundary geom:L-01)
  (boundary geom:L-11))

(node site:L-01
  (label "Level 1")
  (kind Storey)
  (type OfficeStorey)
  (geometry solid)
  (frame frame:building))

(node site:Z-01
  (label "The estate the plate is part of")
  (kind Zone)
  (type Campus)
  (geometry absent))

; A second level holding one room whose east wall bows out into a bay.
;
; It is a level of its own because the plate above it has a courtyard bounded by
; two arcs and nothing else: read as chords that ring is two corners on one line,
; which is a refusal rather than a drawing. A bay is the ordinary case — a ring
; which reads perfectly well as chords and is the wrong shape.
(node site:L-02
  (label "Level 2")
  (kind Storey)
  (type OfficeStorey)
  (geometry solid)
  (frame frame:building))

(vertex geom:V-41
  (frame frame:building)
  (position
    (value (12.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))
(vertex geom:V-42
  (frame frame:building)
  (position
    (value (16.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))
(vertex geom:V-43
  (frame frame:building)
  (position
    (value (16.0 4.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))
(vertex geom:V-44
  (frame frame:building)
  (position
    (value (12.0 4.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(edge geom:E-41 (frame frame:building) (vertices geom:V-41 geom:V-42))
(edge geom:E-42
  (label "Bay window")
  (frame frame:building)
  (vertices geom:V-42 geom:V-43)
  (arc-centre
    (value (16.0 2.0 0.0) m)
    (source "Setting-out report SO-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-19"))
  (arc-through
    (value (18.0 2.0 0.0) m)
    (source "Setting-out report SO-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.006 m))
    (date "2026-02-19")))
(edge geom:E-43 (frame frame:building) (vertices geom:V-43 geom:V-44))
(edge geom:E-44 (frame frame:building) (vertices geom:V-44 geom:V-41))

(loop geom:L-41
  (label "Bay room boundary")
  (frame frame:building)
  (edges geom:E-41 geom:E-42 geom:E-43 geom:E-44))

(node site:S-41
  (label "Room with a bay window")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-02)
  (boundary geom:L-41))

; A plot whose road frontage bows out into the street along an arc of radius
; twenty-six, with a setback written on each of its four edges.
;
; The bulge is the whole of the fixture. Read as the chord between its two ends
; the plot is a rectangle of 240 m2; read as the arc it is a circular segment
; more than that, which is where a scheme either does or does not fit.
(vertex geom:V-21
  (label "Plot, south-west corner")
  (frame frame:building)
  (position
    (value (20.0 0.0 0.0) m)
    (source "Boundary survey BS-2026-004, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-11")))
(vertex geom:V-22
  (label "Plot, south-east corner")
  (frame frame:building)
  (position
    (value (40.0 0.0 0.0) m)
    (source "Boundary survey BS-2026-004, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-11")))
(vertex geom:V-23
  (label "Plot, north-east corner")
  (frame frame:building)
  (position
    (value (40.0 12.0 0.0) m)
    (source "Boundary survey BS-2026-004, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-11")))
(vertex geom:V-24
  (label "Plot, north-west corner")
  (frame frame:building)
  (position
    (value (20.0 12.0 0.0) m)
    (source "Boundary survey BS-2026-004, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-11")))

(edge geom:E-21
  (label "Plot, road frontage")
  (frame frame:building)
  (vertices geom:V-21 geom:V-22)
  (arc-centre
    (value (30.0 24.0 0.0) m)
    (source "Boundary survey BS-2026-004, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-11"))
  (arc-through
    (value (30.0 -2.0 0.0) m)
    (source "Boundary survey BS-2026-004, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.006 m))
    (date "2026-03-11"))
  (setback
    (value 5.0 m)
    (source "Planning consent PC-2026-014, condition 1")
    (method method:statutory-instrument)
    (accuracy (independent 0.01 m))
    (date "2026-04-02")))

(edge geom:E-22
  (label "Plot, east flank")
  (frame frame:building)
  (vertices geom:V-22 geom:V-23)
  (setback
    (value 2.0 m)
    (source "Planning consent PC-2026-014, condition 2")
    (method method:statutory-instrument)
    (accuracy (independent 0.01 m))
    (date "2026-04-02")))

(edge geom:E-23
  (label "Plot, rear")
  (frame frame:building)
  (vertices geom:V-23 geom:V-24)
  (setback
    (value 3.0 m)
    (source "Planning consent PC-2026-014, condition 3")
    (method method:statutory-instrument)
    (accuracy (independent 0.01 m))
    (date "2026-04-02")))

(edge geom:E-24
  (label "Plot, west flank")
  (frame frame:building)
  (vertices geom:V-24 geom:V-21)
  (setback
    (value 2.0 m)
    (source "Planning consent PC-2026-014, condition 4")
    (method method:statutory-instrument)
    (accuracy (independent 0.01 m))
    (date "2026-04-02")))

(loop geom:L-21
  (label "Plot boundary")
  (frame frame:building)
  (edges geom:E-21 geom:E-22 geom:E-23 geom:E-24))

(node site:P-01
  (label "Plot one")
  (kind Site)
  (type Parcel)
  (geometry area)
  (frame frame:building)
  (boundary geom:L-21))

; A pavilion sitting in the bulge: entirely inside the arc and entirely outside
; the chord between its ends, which is what makes siting it a question the arc
; decides.
(vertex geom:V-31
  (frame frame:building)
  (position
    (value (29.0 -1.5 0.0) m)
    (source "Design drawing DR-2026-004, Acme Architects")
    (method method:estimate)
    (accuracy (independent 0.01 m))
    (date "2026-04-20")))
(vertex geom:V-32
  (frame frame:building)
  (position
    (value (31.0 -1.5 0.0) m)
    (source "Design drawing DR-2026-004, Acme Architects")
    (method method:estimate)
    (accuracy (independent 0.01 m))
    (date "2026-04-20")))
(vertex geom:V-33
  (frame frame:building)
  (position
    (value (31.0 -0.5 0.0) m)
    (source "Design drawing DR-2026-004, Acme Architects")
    (method method:estimate)
    (accuracy (independent 0.01 m))
    (date "2026-04-20")))
(vertex geom:V-34
  (frame frame:building)
  (position
    (value (29.0 -0.5 0.0) m)
    (source "Design drawing DR-2026-004, Acme Architects")
    (method method:estimate)
    (accuracy (independent 0.01 m))
    (date "2026-04-20")))

(edge geom:E-31 (frame frame:building) (vertices geom:V-31 geom:V-32))
(edge geom:E-32 (frame frame:building) (vertices geom:V-32 geom:V-33))
(edge geom:E-33 (frame frame:building) (vertices geom:V-33 geom:V-34))
(edge geom:E-34 (frame frame:building) (vertices geom:V-34 geom:V-31))

(loop geom:L-31
  (label "Pavilion outline")
  (frame frame:building)
  (edges geom:E-31 geom:E-32 geom:E-33 geom:E-34))

(node site:S-31
  (label "Pavilion")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (boundary geom:L-31))
`

// curved is the fixture tree the drawings below are read out of.
func curved() map[string]string {
	return map[string]string{
		"registry.dfc":          curvedRegistry,
		"entities/geometry.dfc": curvedModel,
	}
}

// drawing is the vocabulary every invocation below is asked in: which predicate
// carries a position, how close two corners are one corner, how closely a curve
// has to be followed, and the two predicates an arc is written under.
func drawing(args ...string) []string {
	return append([]string{
		"tessellate",
		"--position", "position",
		"--tolerance", "coincident",
		"--arc-centre", "arc-centre",
		"--arc-through", "arc-through",
	}, args...)
}

// drawn runs tessellate against a fixture and returns what it wrote, requiring
// the run to have exited with the code given.
func drawn(t *testing.T, expectedCode int, files map[string]string, args ...string) (tessellateResult, string) {
	t.Helper()

	root := tree(t, files)
	stdout, stderr := invoke(t, expectedCode, root, drawing(args...)...)

	return listed[tessellateResult](t, stdout), stderr
}

func TestRunTessellate(t *testing.T) {
	result, _ := drawn(t, exitSuccess, curved(), "--chord", "chord-deviation", "site:S-01")

	assert.Equal(t, "tessellate", result.Command)
	assert.Equal(t, "site:S-01", result.Subject)
	assert.True(t, result.Derived)
	assert.Equal(t, "frame:building", result.Frame)
	assert.Equal(t, "m", result.Unit)

	require.NotNil(t, result.Tolerance)
	assert.Equal(t, "coincident", result.Tolerance.Name)

	// The tolerance the drawing was made to travels with it, so what the points
	// are good for can be read off the answer rather than remembered.
	require.NotNil(t, result.Chord)
	assert.Equal(t, "chord-deviation", result.Chord.Name)
	assert.Equal(t, 0.01, result.Chord.Value)
	assert.Equal(t, "m", result.Chord.Unit)

	// And so does what it actually achieved, which is inside what it asked for
	// because a curve is divided into a whole number of segments.
	require.NotNil(t, result.Deviation)
	assert.Positive(t, result.Deviation.Value)
	assert.LessOrEqual(t, result.Deviation.Value, result.Chord.Value)
	assert.Equal(t, "m", result.Deviation.Unit)

	// Ten metres square with a round courtyard of radius two taken out of it.
	// The area is of the segments and not of the curve, so it is the exact
	// figure approached rather than reached.
	require.NotNil(t, result.Region)
	assert.False(t, result.Region.Empty)
	assert.InDelta(t, 100-4*math.Pi, result.Region.Area, 0.1)

	require.Len(t, result.Region.Pieces, 1)
	assert.Len(t, result.Region.Pieces[0].Outer, 4, "the plate is four straight walls and stays four corners")
	require.Len(t, result.Region.Pieces[0].Holes, 1)
	assert.Len(t, result.Region.Pieces[0].Holes[0], 32, "the courtyard is two arcs drawn to a centimetre")

	// Every corner is written with the components it was claimed with, which is
	// three here and is never padded to any other number.
	for _, corner := range result.Region.Pieces[0].Holes[0] {
		assert.Len(t, corner, 3)
	}

	require.NotNil(t, result.Budget)
	assert.NotEmpty(t, result.Budget.Terms)
}

// TestRunTessellateFollowsTheChordToleranceItWasGiven is its own function
// because it asserts about two runs over one model rather than about one: what
// has to follow the tolerance is the number of points, and the only way to see
// that is to change the tolerance and nothing else.
func TestRunTessellateFollowsTheChordToleranceItWasGiven(t *testing.T) {
	fine, _ := drawn(t, exitSuccess, curved(), "--chord", "chord-deviation", "site:S-01")
	coarse, _ := drawn(t, exitSuccess, curved(), "--chord", "coarse-chord-deviation", "site:S-01")

	assert.Greater(t,
		len(fine.Region.Pieces[0].Holes[0]), len(coarse.Region.Pieces[0].Holes[0]),
		"following the curve more closely takes more segments")

	assert.Less(t, fine.Deviation.Value, coarse.Deviation.Value)

	assert.Less(t, fine.Region.Area, coarse.Region.Area,
		"a coarser courtyard is a smaller hole, so more of the plate is left")

	assert.Equal(t, len(fine.Region.Pieces[0].Outer), len(coarse.Region.Pieces[0].Outer),
		"nothing on the plate bends, so nothing about it depends on the tolerance")
}

// TestRunTessellateDrawsAStraightBoundaryToItself is its own function because
// what it asserts is that there is no special case: a boundary with nothing
// curved in it is an ordinary answer with a deviation of nothing, rather than
// something a caller has to avoid asking for.
func TestRunTessellateDrawsAStraightBoundaryToItself(t *testing.T) {
	root := tree(t, model())

	stdout, _ := invoke(t, exitSuccess, root,
		"tessellate",
		"--position", "position",
		"--tolerance", "coincident",
		"--chord", "chord-deviation",
		"site:P-01",
	)

	result := listed[tessellateResult](t, stdout)

	assert.True(t, result.Derived)

	require.NotNil(t, result.Deviation)
	assert.Equal(t, 0.0, result.Deviation.Value, "a straight boundary departs from itself by nothing")

	require.NotNil(t, result.Chord)
	assert.Equal(t, "chord-deviation", result.Chord.Name,
		"the tolerance it was asked for is still reported, so a caller can check what it got")

	// Twenty by twelve, exactly as the model holds it and exactly as buildable
	// reports the same parcel.
	require.NotNil(t, result.Region)
	require.Len(t, result.Region.Pieces, 1)
	assert.Len(t, result.Region.Pieces[0].Outer, 4)
	assert.Empty(t, result.Region.Pieces[0].Holes)
	assert.InDelta(t, 240.0, result.Region.Area, 0.25)
}

// TestRunTessellateReportsTheDigestItWasDrawnFrom is its own function because it
// is about the provenance of a drawing rather than about a shape: a derived
// value which cannot say which tree it came from is one nobody can check against
// the tree in front of them
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
func TestRunTessellateReportsTheDigestItWasDrawnFrom(t *testing.T) {
	result, _ := drawn(t, exitSuccess, curved(), "--chord", "chord-deviation", "site:S-01")
	require.NotEmpty(t, result.Digest)

	files := curved()
	files["entities/geometry.dfc"] += "\n(node site:S-02 (kind Space) (type MeetingRoom) (geometry absent))\n"

	moved, _ := drawn(t, exitSuccess, files, "--chord", "chord-deviation", "site:S-01")

	assert.NotEqual(t, result.Digest, moved.Digest, "a model which changed anywhere is a different tree")
	assert.Equal(t, result.Region.Area, moved.Region.Area, "and the shape it was asked about did not move")
}

// TestRunTessellateANodeWithNoOutline is its own function because a node which
// bounds nothing is an answer rather than a refusal, and what it asserts is
// which fields are absent from that answer.
//
// A campus and a warranty have no outline, which is not a fault in either of
// them. Nothing was drawn for one, so there is no tolerance it was drawn to and
// no deviation from anything — and writing either of them anyway would put a
// tolerance with no name and a deviation from nothing into the result, which
// reads as a drawing rather than as the absence of one.
func TestRunTessellateANodeWithNoOutline(t *testing.T) {
	result, _ := drawn(t, exitSuccess, curved(), "--chord", "chord-deviation", "site:Z-01")

	assert.True(t, result.Derived, "a node with no outline is an answer rather than a failure")

	require.NotNil(t, result.Region)
	assert.True(t, result.Region.Empty)
	assert.Empty(t, result.Region.Pieces)

	assert.Nil(t, result.Chord, "nothing was drawn, so there is no tolerance it was drawn to")
	assert.Nil(t, result.Deviation, "and nothing to have departed from anything")
}

func TestRunTessellateRefusesWhatItCannotDraw(t *testing.T) {
	testCases := []struct {
		name  string
		chord string
	}{
		{name: "refuses a chord tolerance the registry does not declare", chord: "no-such-tolerance"},
		{name: "refuses a tolerance finer than anything behind the arc supports", chord: "hair-chord-deviation"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, stderr := drawn(t, exitCheck, curved(), "--chord", testCase.chord, "site:S-01")

			assert.False(t, result.Derived, "no drawing, rather than one made to a number nobody declared")
			assert.Nil(t, result.Region)
			assert.Nil(t, result.Chord)

			assert.NotEmpty(t, result.Subject, "the refusal still says which question it answers")
			assert.NotEmpty(t, result.Digest, "and which tree it was asked about")

			assert.Contains(t, stderr, "error:", "why is on stderr rather than in the result")
		})
	}
}

func TestRunTessellateNeedsTheVocabularyItIsAskedIn(t *testing.T) {
	testCases := []struct {
		name string
		args []string
		says string
	}{
		{
			name: "refuses a run which named no chord tolerance",
			args: []string{
				"tessellate",
				"--position", "position", "--tolerance", "coincident",
				"site:S-01",
			},
			says: "--chord",
		},
		{
			name: "names every word it was not given at once",
			args: []string{"tessellate", "site:S-01"},
			says: "--position",
		},
		{
			name: "refuses a centre with no point on the curve beside it",
			args: []string{
				"tessellate",
				"--position", "position", "--tolerance", "coincident",
				"--chord", "chord-deviation", "--arc-centre", "arc-centre",
				"site:S-01",
			},
			says: "--arc-through",
		},
		{
			name: "refuses a point on the curve with no centre beside it",
			args: []string{
				"tessellate",
				"--position", "position", "--tolerance", "coincident",
				"--chord", "chord-deviation", "--arc-through", "arc-through",
				"site:S-01",
			},
			says: "--arc-centre",
		},
	}

	root := tree(t, curved())

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stdout, stderr := invoke(t, exitUsage, root, testCase.args...)

			assert.Empty(t, stdout, "a usage error produces no result object at all")
			assert.Contains(t, stderr, testCase.says)
		})
	}
}

// TestRunTessellateReadsArcsOnlyWhereItWasToldHowTheyAreWritten is its own
// function because what it asserts is an absence: the engine carries no domain
// vocabulary, so an arc is read only where the run named the predicates it was
// written under.
//
// A run which named neither reads every edge as straight, which is what almost
// every edge is. Here that leaves the courtyard as two corners on one line,
// which encloses nothing — so the run is refused, naming the ring, rather than
// answering about a shape the model does not hold.
func TestRunTessellateReadsArcsOnlyWhereItWasToldHowTheyAreWritten(t *testing.T) {
	root := tree(t, curved())

	stdout, stderr := invoke(t, exitCheck, root,
		"tessellate",
		"--position", "position",
		"--tolerance", "coincident",
		"--chord", "chord-deviation",
		"site:S-01",
	)

	result := listed[tessellateResult](t, stdout)

	assert.False(t, result.Derived)
	assert.Nil(t, result.Region)

	assert.Contains(t, stderr, "geom:L-11",
		"the ring which came to nothing without its arcs is the one named")
}

// straightened runs tessellate without naming the two predicates an arc is
// written under, which is how a caller who does not know the model carries
// curves runs it and is the run the disclosure below is about.
func straightened(t *testing.T, expectedCode int, files map[string]string, args ...string) (tessellateResult, string) {
	t.Helper()

	root := tree(t, files)
	stdout, stderr := invoke(t, expectedCode, root, append([]string{
		"tessellate",
		"--position", "position",
		"--tolerance", "coincident",
	}, args...)...)

	return listed[tessellateResult](t, stdout), stderr
}

// TestRunTessellateReadsACurvedEdgeOrSaysItDidNot is its own function because
// the halves of it are one behaviour: a run which names the vocabulary draws the
// wall and says how closely, and a run which does not draws the chord and says
// that it did. Either answer on its own is the failure.
//
// The deviation is the whole of it. A drawing is an artefact somebody keeps, and
// a deviation of nothing beside a named chord tolerance is an affirmative
// statement that the drawing followed the curve exactly — which is precisely the
// field a consumer would assert on to prove that it had.
func TestRunTessellateReadsACurvedEdgeOrSaysItDidNot(t *testing.T) {
	t.Run("names the edge it chorded and reports no deviation from a curve it never read", func(t *testing.T) {
		result, stderr := straightened(t, exitSuccess, curved(), "--chord", "chord-deviation", "site:P-01")

		require.True(t, result.Derived)

		require.Len(t, result.Chorded, 1, "one of the plot's four edges states a curve")
		assert.Equal(t, "geom:E-21", result.Chorded[0].Edge)
		assert.Equal(t, []string{"arc-centre", "arc-through"}, result.Chorded[0].Predicates,
			"the predicates to name, which is what makes the report actionable")
		assert.NotEmpty(t, result.Chorded[0].Span.String())

		// The tolerance asked for still travels with the drawing: what was
		// asked for is still what was asked for.
		require.NotNil(t, result.Chord)
		assert.Equal(t, "chord-deviation", result.Chord.Name)

		assert.Nil(t, result.Deviation,
			"the frontage was run straight through, so nothing here achieved anything against it")

		// And the drawing really is the chord, which is what makes a deviation
		// of nothing a claim about a shape the answer does not hold.
		require.NotNil(t, result.Region)
		assert.Len(t, result.Region.Pieces[0].Outer, 4, "the bow became the straight line between its ends")
		assert.InDelta(t, 240.0, result.Region.Area, 1e-9)

		assert.Contains(t, stderr, "geom:E-21", "and a person is told the same thing")
	})

	t.Run("reports what it achieved where the vocabulary is named", func(t *testing.T) {
		result, _ := drawn(t, exitSuccess, curved(), "--chord", "chord-deviation", "site:P-01")

		assert.Empty(t, result.Chorded, "there is nothing left unread to report")

		require.NotNil(t, result.Chord)
		require.NotNil(t, result.Deviation)
		assert.Positive(t, result.Deviation.Value)
		assert.LessOrEqual(t, result.Deviation.Value, result.Chord.Value,
			"a curve is divided into a whole number of segments, so it is followed at least as closely as asked")

		assert.Greater(t, result.Region.Area, 240.0, "the frontage bows out into the street")
	})

	t.Run("says nothing about curves for a boundary which claims none", func(t *testing.T) {
		result, stderr := straightened(t, exitSuccess, curved(), "--chord", "chord-deviation", "site:S-31")

		assert.Empty(t, result.Chorded, "the pavilion is four straight walls")

		// The answer a model carrying no arc claims gets is the answer it got
		// before: a drawing which followed four straight edges departed from
		// them by nothing, and that zero is true.
		require.NotNil(t, result.Deviation)
		assert.Zero(t, result.Deviation.Value)

		assert.NotContains(t, stderr, "curved edge")
	})
}

// TestRunTessellateSummarisesItselfForAPerson is its own function because what
// it asserts is which stream a rendering goes to rather than what the rendering
// says: stdout is the same bytes whether or not anybody asked to read the run.
func TestRunTessellateSummarisesItselfForAPerson(t *testing.T) {
	root := tree(t, curved())

	quiet, _ := invoke(t, exitSuccess, root, drawing("--chord", "chord-deviation", "site:S-01")...)
	loud, stderr := invoke(t, exitSuccess, root,
		append(drawing("--chord", "chord-deviation", "site:S-01"), "--format", "human")...)

	assert.Equal(t, quiet, loud, "the format a person reads never changes the machine contract")

	assert.Contains(t, stderr, "site:S-01")
	assert.Contains(t, stderr, "chord-deviation")
	assert.Contains(t, stderr, "hole")
}

// TestRunTessellateAttributesEveryChordToTheEdgeItApproximates checks that a
// drawing arrives saying where it came from.
//
// A drawing is the moment a boundary becomes points, and it is where an
// attribution is most easily lost: thirty-two chords arrive where a round
// courtyard was, and a consumer which could not say which wall they stand in for
// has a polygon and nothing else.
func TestRunTessellateAttributesEveryChordToTheEdgeItApproximates(t *testing.T) {
	result, _ := drawn(t, exitSuccess, curved(), "--chord", "chord-deviation", "site:S-01")

	require.NotNil(t, result.Region)
	require.NotEmpty(t, result.Region.Boundary)

	// One run per corner of the plate and one per chord of the courtyard, which
	// is the whole of the drawing described exactly once.
	assert.Len(t, result.Region.Boundary,
		len(result.Region.Pieces[0].Outer)+len(result.Region.Pieces[0].Holes[0]))

	arcs := map[string]int{}

	// The plate is straight and is itself; the courtyard is two arcs, and each
	// of the chords standing in for one names the edge which bends along it
	// rather than being read back as a wall somebody drew straight.
	expected := map[int]string{0: "edge", 1: "arc"}

	for _, segment := range result.Region.Boundary {
		assert.NotEmpty(t, segment.Edge, "every run of a drawn boundary names an edge")
		assert.Len(t, segment.From, 3)
		assert.Len(t, segment.To, 3)

		assert.Equal(t, expected[segment.Ring], segment.Origin, "ring %d", segment.Ring)

		if segment.Origin == "arc" {
			arcs[segment.Edge]++
		}
	}

	assert.Len(t, arcs, 2, "the courtyard is drawn from two arcs, and each chord names the one it stands in for")

	var chords int
	for _, count := range arcs {
		chords += count
	}
	assert.Equal(t, len(result.Region.Pieces[0].Holes[0]), chords)
}

// frontageBulge is how much area the curved fixture's road frontage bows out
// into the street: the circular segment an arc of radius twenty-six cuts off a
// chord of twenty.
//
// It is written as the closed form rather than as a decimal because the whole
// point of what it is asserted against is that the arithmetic agrees with the
// circle rather than with a drawing of it.
func frontageBulge() float64 {
	const radius, halfChord = 26.0, 10.0

	sweep := 2 * math.Asin(halfChord/radius)

	return radius * radius / 2 * (sweep - math.Sin(sweep))
}
