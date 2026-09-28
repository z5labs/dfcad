// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// getRegistry is the vocabulary the model below is judged against.
const getRegistry = `(project
  (label "Retrieval fixture")
  (globalid-namespace "https://example.org/models/get"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
(namespace survey (description "Claim ids and control points issued by Acme Surveys."))

(frame frame:building (label "Building local grid") (unit m))

(type Campus
  (kind Zone)
  (geometry absent)
  (description "A group of things administered together, which has no shape."))

(type Level
  (kind Storey)
  (geometry surface)
  (description "One storey, taken as its finished floor."))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings."))

(type Partition
  (kind Element)
  (geometry line)
  (description "A non-loadbearing wall between spaces."))

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

// getModel is the semantic family: a campus, a storey, one room and the
// partition inside it.
//
// The room carries four predicates, and each of them says something different
// about resolution. Two live area claims of different accuracy resolve to one;
// two height claims of the same accuracy and the same date resolve to neither;
// and the occupancy and the note carry no accuracy at all, so nothing about
// them can be ranked. One area claim is deprecated in favour of another, which
// is what a retrieval leaves out until it is asked for it.
const getModel = `(node site:Z-01
  (label "Riverside campus")
  (kind Zone)
  (type Campus))

(node site:L-01
  (label "Level 1")
  (kind Storey)
  (type Level)
  (geometry surface)
  (frame frame:building))

(node site:S-101
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (within site:L-01)
  (member-of site:Z-01)
  (boundary geom:L-01)
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
  (occupancy
    (id survey:O-0001)
    (value 0.0)
    (source "Fire strategy FS-01")
    (method method:assumed)
    (date "2026-02-01"))
  (note
    (value "Booked through the front desk.")
    (source "Facilities handbook, 2026 edition")
    (method method:assumed)
    (date "2026-02-01"))
  (assert within-resolves)
  (assert required-claim (predicate area)))

(node site:E-01
  (label "Partition between the room and the corridor")
  (kind Element)
  (type Partition)
  (geometry line)
  (frame frame:building)
  (within site:S-101)
  (note
    (value "")
    (source "Fit-out check FC-2026-002, Acme Surveys")
    (method method:assumed)
    (date "2026-02-01")))
`

// getGeometry is the geometric family: the four corners of the room, the walls
// between them and the loop they close.
const getGeometry = `(vertex geom:V-01
  (label "Room A, north-west corner")
  (frame frame:building)
  (position
    (id survey:P-0001)
    (value (0.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m) (systematic 0.008 m survey:CP-3))
    (date "2026-02-18")))

(vertex geom:V-02
  (label "Room A, north-east corner")
  (frame frame:building)
  (position
    (value (4.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(vertex geom:V-03
  (label "Room A, south-east corner")
  (frame frame:building)
  (position
    (value (4.0 6.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(vertex geom:V-04
  (label "Room A, south-west corner")
  (frame frame:building)
  (position
    (value (0.0 6.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(edge geom:E-01
  (label "Room A, north wall")
  (frame frame:building)
  (vertices geom:V-01 geom:V-02)
  (backed-by site:E-01))

(edge geom:E-02
  (label "Room A, east wall")
  (frame frame:building)
  (vertices geom:V-02 geom:V-03))

(edge geom:E-03
  (label "Room A, south wall")
  (frame frame:building)
  (vertices geom:V-03 geom:V-04))

(edge geom:E-04
  (label "Room A, west wall")
  (frame frame:building)
  (vertices geom:V-04 geom:V-01))

(loop geom:L-01
  (label "Meeting Room A boundary")
  (frame frame:building)
  (edges geom:E-01 geom:E-02 geom:E-03 geom:E-04))
`

// retrievable is the fixture tree get is run against.
func retrievable() map[string]string {
	return map[string]string{
		"registry.dfc":          getRegistry,
		"entities/site.dfc":     getModel,
		"entities/geometry.dfc": getGeometry,
	}
}

// retrieved runs get over the fixture and decodes what reached stdout.
func retrieved(t *testing.T, args ...string) getEntity {
	t.Helper()

	t.Chdir(tree(t, retrievable()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run(append([]string{"get"}, args...), &stdout, &stderr), stderr.String())

	// Nothing is wrong with the fixture, so nothing is on stderr. It is
	// asserted rather than assumed because a model which quietly stopped
	// loading — a reference which no longer resolves, a predicate nobody
	// declares — would still answer every assertion below about the axes.
	require.Empty(t, stderr.String())

	result := listed[getResult](t, stdout.String())
	assert.Equal(t, outputVersion, result.Version)
	assert.Equal(t, "get", result.Command)

	return result.Entity
}

// axes is one entity without the three things asserted on elsewhere: where it
// was written, which moves whenever the fixture does, and the claims and the
// assertions, which are tables of their own.
func axes(entity getEntity) getEntity {
	entity.Span = dfcad.Span{}
	entity.Claims = nil
	entity.Assertions = nil
	return entity
}

// predicates is each claim as "predicate id", which is what says both which
// claims came back and in which order.
func predicates(claims []claimEntry) []string {
	out := make([]string, 0, len(claims))
	for _, claim := range claims {
		out = append(out, strings.TrimSpace(claim.Predicate+" "+claim.ID))
	}
	return out
}

func TestRunGet(t *testing.T) {
	testCases := []struct {
		name     string
		id       string
		expected getEntity
	}{
		{
			name: "returns a node with its axes, its label, its frame and the references it wrote",
			id:   "site:S-101",
			expected: getEntity{
				ID:         "site:S-101",
				Family:     familyNode,
				Label:      "Meeting Room A",
				Kind:       "Space",
				Type:       "MeetingRoom",
				Geometry:   "area",
				Frame:      "frame:building",
				Within:     "site:L-01",
				MemberOf:   []string{"site:Z-01"},
				Boundaries: []string{"geom:L-01"},
			},
		},
		{
			name: "returns a node which has no geometry, no frame and nothing above it",
			id:   "site:Z-01",
			expected: getEntity{
				ID:     "site:Z-01",
				Family: familyNode,
				Label:  "Riverside campus",
				Kind:   "Zone",
				Type:   "Campus",
			},
		},
		{
			name: "returns a vertex by the same call a node is returned by",
			id:   "geom:V-01",
			expected: getEntity{
				ID:     "geom:V-01",
				Family: familyVertex,
				Label:  "Room A, north-west corner",
				Frame:  "frame:building",
			},
		},
		{
			name: "returns an edge with the vertices it runs between and what backs it",
			id:   "geom:E-01",
			expected: getEntity{
				ID:       "geom:E-01",
				Family:   familyEdge,
				Label:    "Room A, north wall",
				Frame:    "frame:building",
				Start:    "geom:V-01",
				End:      "geom:V-02",
				BackedBy: []string{"site:E-01"},
			},
		},
		{
			name: "returns a loop with the edges it is assembled from",
			id:   "geom:L-01",
			expected: getEntity{
				ID:     "geom:L-01",
				Family: familyLoop,
				Label:  "Meeting Room A boundary",
				Frame:  "frame:building",
				Edges:  []string{"geom:E-01", "geom:E-02", "geom:E-03", "geom:E-04"},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, axes(retrieved(t, testCase.id)))
		})
	}
}

// TestRunGetReportsTheClaimsWrittenOnIt is its own function because it is about
// the evidence rather than about the axes: the point of inlining claims on the
// node is that retrieving the subject retrieves what is known about it, with no
// second lookup.
func TestRunGetReportsTheClaimsWrittenOnIt(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name: "leaves out the claims which have been deprecated",
			args: []string{"site:S-101"},
			expected: []string{
				"area survey:A-0002",
				"area survey:A-0003",
				"height survey:H-0001",
				"height survey:H-0002",
				"note",
				"occupancy survey:O-0001",
			},
		},
		{
			name: "includes the deprecated ones when they are asked for",
			args: []string{"--deprecated", "site:S-101"},
			expected: []string{
				"area survey:A-0001",
				"area survey:A-0002",
				"area survey:A-0003",
				"height survey:H-0001",
				"height survey:H-0002",
				"note",
				"occupancy survey:O-0001",
			},
		},
		{
			name:     "reports a thing nothing is claimed about as no claims at all",
			args:     []string{"site:Z-01"},
			expected: []string{},
		},
		{
			name:     "reports the claims written on a geometric node",
			args:     []string{"geom:V-01"},
			expected: []string{"position survey:P-0001"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, predicates(retrieved(t, testCase.args...).Claims))
		})
	}
}

// TestRunGetReportsWhatConstrainsIt is its own function because it is about the
// rules rather than about the evidence: retrieving a thing says what is known
// about it and what has to hold of it, and a caller which had to make a second
// call for the second half would read one and act on it.
func TestRunGetReportsWhatConstrainsIt(t *testing.T) {
	testCases := []struct {
		name     string
		id       string
		expected []assertionEntry
	}{
		{
			name: "reports the assertions written on it, in the order they were written",
			id:   "site:S-101",
			expected: []assertionEntry{
				{Check: "within-resolves"},
				{Check: "required-claim", Parameters: []string{"(predicate area)"}},
			},
		},
		{
			name:     "reports a thing nothing constrains as no assertions at all",
			id:       "site:Z-01",
			expected: []assertionEntry{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			written := retrieved(t, testCase.id).Assertions

			for i := range written {
				require.NotZero(t, written[i].Span.Start.Line, "an assertion says where it was written")
				written[i].Span = dfcad.Span{}
			}

			assert.Equal(t, testCase.expected, written)
		})
	}
}

// TestRunGetReportsTheEvidenceForAClaim is its own function because it asserts
// about the whole of one claim rather than about which claims came back. A
// value which arrived without where it came from, how it was obtained and how
// good it is would be the bare number the format exists to stop.
func TestRunGetReportsTheEvidenceForAClaim(t *testing.T) {
	entity := retrieved(t, "geom:V-01")

	require.Len(t, entity.Claims, 1)
	claim := entity.Claims[0]

	assert.Equal(t, "survey:P-0001", claim.ID)
	assert.Equal(t, "position", claim.Predicate)
	assert.Equal(t, "Interior control set IC-01, Acme Surveys", claim.Source)
	assert.Equal(t, "method:total-station", claim.Method)
	assert.Equal(t, "2026-02-18", claim.Date)
	assert.Equal(t, string(dfcad.RankNormal), claim.Rank)
	assert.Empty(t, claim.SupersededBy)
	assert.Equal(t, []accuracyTerm{
		{Kind: string(dfcad.TermIndependent), Magnitude: 0.004, Unit: "m"},
		{Kind: string(dfcad.TermSystematic), Magnitude: 0.008, Unit: "m", Source: "survey:CP-3"},
	}, claim.Accuracy)

	assert.Equal(t, claimValue{
		Shape:      string(dfcad.ShapeCoordinate),
		Unit:       "m",
		Coordinate: []float64{0, 0, 0},
	}, claim.Value)

	// The claim was written in the file the vertex was, and the retrieval says
	// so about the claim as well as about the thing it is about.
	assert.Contains(t, claim.Span.Start.Path, "geometry.dfc")
}

// TestRunGetReportsADeprecatedClaimAsDeprecated is its own function because a
// retracted claim is not the same shape of answer as a live one: it says which
// claim replaced it, and that reference is what makes the history walkable.
func TestRunGetReportsADeprecatedClaimAsDeprecated(t *testing.T) {
	claims := retrieved(t, "--deprecated", "site:S-101").Claims

	require.NotEmpty(t, claims)
	deprecated := claims[0]

	assert.Equal(t, "survey:A-0001", deprecated.ID)
	assert.Equal(t, string(dfcad.RankDeprecated), deprecated.Rank)
	assert.Equal(t, "survey:A-0002", deprecated.SupersededBy)
}

// TestRunGetResolvesClaimsToOneValueEach is its own function because it asserts
// about a different question: not what the model says, but what the resolution
// rule makes of what it says.
//
// The three states are each here. One claim wins outright; two equally accurate
// and equally recent claims tie, and both come back rather than one of them
// being picked; and a predicate nothing rankable was said about resolves to
// nothing, with its live claims still reported as the candidates they are.
func TestRunGetResolvesClaimsToOneValueEach(t *testing.T) {
	claims := retrieved(t, "--claims", claimsResolved, "site:S-101").Claims

	spelled := make([]string, 0, len(claims))
	for _, claim := range claims {
		spelled = append(spelled, strings.TrimSpace(claim.Predicate+" "+claim.ID)+" "+claim.Resolution)
	}

	assert.Equal(t, []string{
		"area survey:A-0002 " + resolutionCurrent,
		"height survey:H-0001 " + resolutionTied,
		"height survey:H-0002 " + resolutionTied,
		"note " + resolutionUnranked,
		"occupancy survey:O-0001 " + resolutionUnranked,
	}, spelled)
}

// TestRunGetLeavesResolutionOutWhenNothingWasResolved checks that the field
// which says what the rule made of a claim is written only by the run which
// applied the rule. Reporting every claim as unresolved would be reporting an
// answer to a question nobody asked.
func TestRunGetLeavesResolutionOutWhenNothingWasResolved(t *testing.T) {
	for _, claim := range retrieved(t, "site:S-101").Claims {
		assert.Empty(t, claim.Resolution, claim.Predicate)
	}
}

// TestRunGetReportsEveryShapeOfValue walks the shapes a claim's value takes,
// because a value is read through the accessor for the shape it has and a
// payload which named the shape wrongly would send a caller to the wrong field.
func TestRunGetReportsEveryShapeOfValue(t *testing.T) {
	claims := retrieved(t, "site:S-101").Claims

	byPredicate := make(map[string]claimEntry, len(claims))
	for _, claim := range claims {
		byPredicate[claim.Predicate] = claim
	}

	area := byPredicate["area"]
	require.NotNil(t, area.Value.Scalar)
	assert.Equal(t, string(dfcad.ShapeScalar), area.Value.Shape)
	assert.Equal(t, "m2", area.Value.Unit)

	// A predicate which declares no unit carries none, rather than an empty one.
	occupancy := byPredicate["occupancy"]
	require.NotNil(t, occupancy.Value.Scalar)
	assert.Zero(t, *occupancy.Value.Scalar)
	assert.Empty(t, occupancy.Value.Unit)

	note := byPredicate["note"]
	assert.Equal(t, string(dfcad.ShapeText), note.Value.Shape)
	require.NotNil(t, note.Value.Text)
	assert.Equal(t, "Booked through the front desk.", *note.Value.Text)
	assert.Nil(t, note.Value.Scalar)
}

// TestRunGetReportsAnEmptyValue is its own function because it is about the two
// values a payload silently drops: a claim of zero and a claim of the empty
// string are each a claim somebody wrote, and a field which went missing would
// read as a claim which was never written at all.
func TestRunGetReportsAnEmptyValue(t *testing.T) {
	testCases := []struct {
		name     string
		id       string
		expected string
	}{
		{
			name:     "writes a scalar of zero rather than dropping the field",
			id:       "site:S-101",
			expected: `"scalar":0`,
		},
		{
			name:     "writes text of the empty string rather than dropping the field",
			id:       "site:E-01",
			expected: `"text":""`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, retrievable()))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run([]string{"get", testCase.id}, &stdout, &stderr), stderr.String())

			assert.Contains(t, stdout.String(), testCase.expected)
		})
	}
}

// TestRunGetSaysWhereItWasDefined is its own function because it is about the
// one field which sends a reader back to the file rather than to a search.
//
// The span is written as text — path:line:column-line:column — so what is
// asserted is that the text reads back as a place: the right file, a line and a
// column at both ends, and an end which is not before the start. The byte
// offsets are not in the text form and are not what a reader jumps to.
func TestRunGetSaysWhereItWasDefined(t *testing.T) {
	entity := retrieved(t, "site:E-01")

	assert.Contains(t, entity.Span.Start.Path, "site.dfc")
	assert.Equal(t, entity.Span.Start.Path, entity.Span.End.Path)

	assert.Positive(t, entity.Span.Start.Line)
	assert.Positive(t, entity.Span.Start.Column)
	assert.Positive(t, entity.Span.End.Line)
	assert.Positive(t, entity.Span.End.Column)

	// The end is after the start, which on one line is a column and across
	// lines is a line. Comparing only the lines would let a form which ended
	// before it began pass.
	if entity.Span.End.Line == entity.Span.Start.Line {
		assert.Greater(t, entity.Span.End.Column, entity.Span.Start.Column)
	} else {
		assert.Greater(t, entity.Span.End.Line, entity.Span.Start.Line)
	}
}

// TestRunGetWritesASpanAsOneString is what the contract's readers see, which
// the decoded form above cannot show: the object shape is gone, and with it the
// path written twice and the two byte offsets nobody was jumping to.
func TestRunGetWritesASpanAsOneString(t *testing.T) {
	t.Chdir(tree(t, retrievable()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"get", "site:E-01"}, &stdout, &stderr), stderr.String())

	var written struct {
		Entity struct {
			Span string `json:"span"`
		} `json:"entity"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &written))

	assert.Regexp(t, `^[^"]+\.dfc:\d+:\d+-\d+:\d+$`, written.Entity.Span)
	assert.NotContains(t, stdout.String(), `"offset"`)
}

// TestRunGetReferencesAreIDsRatherThanTheThingsTheyName is its own function
// because it is about what is *not* in the answer: a retrieval which inlined
// what it referenced would return the model rather than the thing asked for.
func TestRunGetReferencesAreIDsRatherThanTheThingsTheyName(t *testing.T) {
	t.Chdir(tree(t, retrievable()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"get", "site:S-101"}, &stdout, &stderr), stderr.String())

	// The ids of the things it references are there.
	assert.Contains(t, stdout.String(), "site:L-01")
	assert.Contains(t, stdout.String(), "geom:L-01")

	// What those ids name is not: the storey's own label and the loop's edges
	// are one call away and are not this answer.
	assert.NotContains(t, stdout.String(), "Level 1")
	assert.NotContains(t, stdout.String(), "geom:E-01")
}

// TestRunGetRejectsWhatTheModelDoesNotHold walks the ways an argument can be
// wrong. Each is a usage error rather than an empty answer, and stdout stays
// empty because the run produced no result.
func TestRunGetRejectsWhatTheModelDoesNotHold(t *testing.T) {
	testCases := []struct {
		name           string
		args           []string
		expectedStderr string
	}{
		{
			name: "names the nearest id when one is close enough to be the one meant",
			args: []string{"get", "site:S-1O1"},
			expectedStderr: "dfcad get: " +
				UnknownIDError{ID: "site:S-1O1", Nearest: "site:S-101"}.Error() + "\n",
		},
		{
			name: "says where to look when nothing is close",
			args: []string{"get", "other:nothing-like-it"},
			expectedStderr: "dfcad get: " +
				UnknownIDError{ID: "other:nothing-like-it"}.Error() + "\n",
		},
		{
			name: "reports an argument which is not an id at all",
			args: []string{"get", "S-101"},
			expectedStderr: "dfcad get: " +
				dfcad.MalformedIDError{Written: "S-101", Reason: dfcad.IDUnqualified}.Error() + "\n",
		},
		{
			name: "rejects a claims selection which names neither way of reporting them",
			args: []string{"get", "--claims", "some", "site:S-101"},
			expectedStderr: "dfcad get: " +
				UnknownClaimsError{Selection: "some", Known: claimSelections}.Error() + "\n",
		},
		{
			name:           "refuses to include deprecated claims in a resolution which never sees one",
			args:           []string{"get", "--claims", claimsResolved, "--deprecated", "site:S-101"},
			expectedStderr: "dfcad get: " + ErrDeprecatedNotResolvable.Error() + "\n",
		},
		{
			name:           "reports a get with no id at all",
			args:           []string{"get"},
			expectedStderr: "dfcad get: " + ErrMissingID.Error() + "\n\n" + getUsage,
		},
		{
			name: "refuses a second id, pointing at standard input for several",
			args: []string{"get", "site:S-101", "site:Z-01"},
			expectedStderr: "dfcad get: " +
				SeveralIDsError{IDs: []string{"site:S-101", "site:Z-01"}}.Error() + "\n\n" + getUsage,
		},
		{
			name: "refuses standard input beside an id",
			args: []string{"get", "-", "site:Z-01"},
			expectedStderr: "dfcad get: " +
				SeveralIDsError{IDs: []string{"-", "site:Z-01"}}.Error() + "\n\n" + getUsage,
		},
		{
			name: "refuses an id beside standard input",
			args: []string{"get", "site:S-101", "-"},
			expectedStderr: "dfcad get: " +
				SeveralIDsError{IDs: []string{"site:S-101", "-"}}.Error() + "\n\n" + getUsage,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, retrievable()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitUsage, run(testCase.args, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Equal(t, testCase.expectedStderr, stderr.String())
		})
	}
}

// TestUnknownIDErrorSaysWhereToLook checks that the error carries the
// suggestion for a caller to branch on, rather than only spelling it into a
// message a caller would have to parse back apart.
func TestUnknownIDErrorSaysWhereToLook(t *testing.T) {
	suggested := UnknownIDError{ID: "site:S-1O1", Nearest: "site:S-101"}
	assert.Contains(t, suggested.Error(), "site:S-101")

	// With nothing close, the answer is where to look rather than a guess.
	none := UnknownIDError{ID: "other:nothing-like-it"}
	assert.Contains(t, none.Error(), "list-instances")
}

// TestRunGetStillAnswersOnAModelWithDiagnostics is its own function because it
// is about a run over a model which is not sound. The thing asked for is still
// a thing the model holds, the diagnostics still reach whoever wrote the file,
// and whether the model is sound is what `dfcad check` answers.
func TestRunGetStillAnswersOnAModelWithDiagnostics(t *testing.T) {
	files := retrievable()
	files["entities/broken.dfc"] = unparseable

	t.Chdir(tree(t, files))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"get", "site:S-101"}, &stdout, &stderr))

	assert.Equal(t, "site:S-101", listed[getResult](t, stdout.String()).Entity.ID)
	assert.Contains(t, stderr.String(), "broken.dfc:1:")
}

// TestRunGetOutputIsDeterministic checks that two runs over the same model
// write byte-identical results, which is what makes diffing two runs mean
// something.
func TestRunGetOutputIsDeterministic(t *testing.T) {
	for _, args := range [][]string{
		{"get", "site:S-101"},
		{"get", "--claims", claimsResolved, "site:S-101"},
		{"get", "--deprecated", "site:S-101"},
		{"get", "geom:E-01"},
	} {
		t.Run(strings.Join(args[1:], " ")+" writes the same bytes twice", func(t *testing.T) {
			var results []string
			for range 2 {
				t.Chdir(tree(t, retrievable()))

				var stdout, stderr bytes.Buffer
				require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

				results = append(results, stdout.String())
			}

			assert.Equal(t, results[0], results[1])
		})
	}
}

// TestRunGetHumanOutputNeverChangesStdout is its own function because it is
// about the one property the format flag must not have: whichever format was
// asked for, and however loud the run was told to be, stdout is the same bytes.
func TestRunGetHumanOutputNeverChangesStdout(t *testing.T) {
	retrieval := func(t *testing.T, args ...string) (string, string) {
		t.Helper()

		t.Chdir(tree(t, retrievable()))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

		return stdout.String(), stderr.String()
	}

	machine, machineReport := retrieval(t, "get", "site:S-101")
	human, humanReport := retrieval(t, "get", "site:S-101", "--format", formatHuman)
	both, bothReport := retrieval(t, "get", "site:S-101", "--format", formatHuman, "-v")

	assert.Equal(t, machine, human)
	assert.Equal(t, machine, both)

	// The summary is behind the format flag; the claims behind it are behind the
	// verbosity flag, because the claims are already the result on stdout.
	assert.Empty(t, machineReport)
	assert.Contains(t, humanReport, "node site:S-101 at ")
	assert.Contains(t, humanReport, "Meeting Room A, Space MeetingRoom, 6 claims")
	assert.NotContains(t, humanReport, "area: 24.2 m2")
	assert.Contains(t, bothReport, "area: 24.2 m2 by method:total-station on 2026-05-06")
	assert.Contains(t, bothReport, "note: \"Booked through the front desk.\" by method:assumed")

	// A geometric node reads the same way, without the axes it does not have.
	_, vertexReport := retrieval(t, "get", "geom:V-01", "--format", formatHuman, "-v")
	assert.Contains(t, vertexReport, "position: (0 0 0) m by method:total-station")
	assert.Contains(t, vertexReport, "vertex geom:V-01 at ")
}

// TestRunGetUsage checks that help goes to stderr and exits zero, which is the
// half of the contract that keeps prose off the stream a caller pipes.
func TestRunGetUsage(t *testing.T) {
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer

	require.Equal(t, exitSuccess, run([]string{"get", "-h"}, &stdout, &stderr))

	assert.Empty(t, stdout.String())
	assert.Equal(t, getUsage, stderr.String())
}

// TestGetErrorsAreNotSwallowed checks that a stdout which cannot be written
// reports a failure rather than an unexplained success.
func TestGetErrorsAreNotSwallowed(t *testing.T) {
	t.Chdir(tree(t, retrievable()))

	var stderr bytes.Buffer

	assert.Equal(t, exitLoad, run([]string{"get", "site:S-101"}, brokenWriter{}, &stderr))
	assert.Contains(t, stderr.String(), "dfcad get:")
}

// TestCheckClaimsAcceptsEveryWayOfReportingThem is the other half of the
// rejection table: every selection the command does take passes, so the check
// is not simply refusing everything.
func TestCheckClaimsAcceptsEveryWayOfReportingThem(t *testing.T) {
	for _, selection := range claimSelections {
		assert.NoError(t, checkClaims(selection, false), selection)
	}

	assert.NoError(t, checkClaims(claimsFull, true))
}

// observedRegistry is the vocabulary the surveyed fixture below is judged
// against. An observation file names four vocabularies and declares none of
// them, so all four arrive from here.
const observedRegistry = `(project
  (label "Observation retrieval fixture")
  (globalid-namespace "https://example.org/models/observed"))

(namespace fix (description "Fix qualities an instrument reports."))
(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "How a value was obtained."))
(namespace retirement (description "Retirement records issued by the field crew."))
(namespace session (description "Field occupations."))
(namespace shot (description "Observation records issued by the field crew."))

(frame frame:site (label "Site survey grid") (unit m))
`

// observedModel is two corners: one linked to an afternoon of sound records,
// and one linked to a file holding a line nothing can read.
//
// The second is what makes "the file was not read" observable from outside the
// engine. Retrieving that corner is a clean run, which it could not be if the
// file behind it had been opened; asking for the records is what produces the
// diagnostic.
const observedModel = `(vertex geom:V-01
  (label "The corner somebody shot twice")
  (frame frame:site)
  (observed-in "observations/site-control.obs"))

(vertex geom:V-02
  (label "The corner whose file nobody has fixed")
  (frame frame:site)
  (observed-in "observations/suspect.obs"))
`

// observedRecords is one morning of control with a float shot retired by a
// later record.
const observedRecords = `# id at frame x y z method fix h-precision v-precision antenna session
obs shot:2026-05-06-0001 2026-05-06T09:14:22Z frame:site 412300.120 5318220.455 34.210 method:gnss-rtk fix:rtk-fixed 0.012 0.021 2.000 session:2026-05-06-am
obs shot:2026-05-06-0002 2026-05-06T09:18:47Z frame:site 412318.880 5318241.330 34.402 method:gnss-rtk fix:rtk-float 0.240 0.510 2.000 session:2026-05-06-am
retire retirement:2026-05-06-0001 2026-05-06T16:02:00Z shot:2026-05-06-0002 "float solution beside a fixed reshot of the same corner"
`

// suspectRecords is one line whose timestamp is not one.
const suspectRecords = `obs shot:2026-05-08-0001 the-eighth-of-may frame:site 412300.108 5318241.344 34.407 method:total-station fix:observed 0.004 0.006 1.520 session:2026-05-08-am
`

// observedTree is the fixture tree the observation retrievals are run against.
func observedTree() map[string]string {
	return map[string]string{
		"registry.dfc":                  observedRegistry,
		"entities/geometry.dfc":         observedModel,
		"observations/site-control.obs": observedRecords,
		"observations/suspect.obs":      suspectRecords,
	}
}

// TestRunGetNamesTheObservationFilesWithoutReadingThem is the retrieval this
// story is about: the answer says where the evidence is and costs nothing to
// produce.
//
// The corner it asks about links to a file with a malformed line in it, and the
// run is clean. That is the assertion — a run which had opened the file would
// have had to report the line — and it is made from outside the engine, with
// nothing stubbed.
func TestRunGetNamesTheObservationFilesWithoutReadingThem(t *testing.T) {
	t.Chdir(tree(t, observedTree()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"get", "geom:V-02"}, &stdout, &stderr), stderr.String())

	assert.Empty(t, stderr.String(), "the file behind the corner was not opened, so its malformed line is unreported")

	entity := listed[getResult](t, stdout.String()).Entity

	assert.Equal(t, []string{"observations/suspect.obs"}, entity.Observations)
	assert.Nil(t, entity.Records, "the records are absent rather than empty, because nobody asked for them")
}

// TestRunGetReadsTheObservationFilesWhenAskedFor is the other half: a run which
// needs the records opens the files, and says what is wrong with what it found.
func TestRunGetReadsTheObservationFilesWhenAskedFor(t *testing.T) {
	t.Chdir(tree(t, observedTree()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"get", "--observations", "geom:V-01"}, &stdout, &stderr), stderr.String())

	assert.Empty(t, stderr.String(), "the records behind this corner are sound")

	entity := listed[getResult](t, stdout.String()).Entity

	assert.Equal(t, []string{"observations/site-control.obs"}, entity.Observations)
	require.NotNil(t, entity.Records)
	require.Len(t, *entity.Records, 2, "the retired shot is reported rather than dropped")

	records := *entity.Records

	assert.Equal(t, "shot:2026-05-06-0001", records[0].ID)
	assert.Equal(t, "2026-05-06T09:14:22Z", records[0].At)
	assert.Equal(t, "frame:site", records[0].Frame)
	assert.Equal(t, []float64{412300.120, 5318220.455, 34.210}, records[0].Coordinate)
	assert.Equal(t, "method:gnss-rtk", records[0].Method)
	assert.Equal(t, "fix:rtk-fixed", records[0].Fix)
	assert.Equal(t, 0.012, records[0].HorizontalPrecision)
	assert.Equal(t, 0.021, records[0].VerticalPrecision)
	assert.Equal(t, 2.0, records[0].AntennaHeight)
	assert.Equal(t, "session:2026-05-06-am", records[0].Session)
	assert.Nil(t, records[0].Retired, "nothing retired the fixed shot")

	require.NotNil(t, records[1].Retired, "the float shot carries the record which retired it")
	assert.Equal(t, "retirement:2026-05-06-0001", records[1].Retired.ID)
	assert.Equal(t, "float solution beside a fixed reshot of the same corner", records[1].Retired.Reason)
}

// TestRunGetReportsWhatIsWrongWithTheRecordsItRead checks that reading the
// files is reading them properly: the observation format's own diagnostics
// reach stderr, and the answer on stdout is still one object.
func TestRunGetReportsWhatIsWrongWithTheRecordsItRead(t *testing.T) {
	t.Chdir(tree(t, observedTree()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"get", "--observations", "geom:V-02"}, &stdout, &stderr))

	assert.Contains(t, stderr.String(), "observations/suspect.obs")
	assert.Contains(t, stderr.String(), "error:")

	entity := listed[getResult](t, stdout.String()).Entity

	require.NotNil(t, entity.Records)
	assert.Empty(t, *entity.Records, "a line which could not be read is not a record, and the list is still a list")
}

// TestRunGetReportsTheObservationFilesToAPerson checks the human rendering,
// which is where somebody reading the run finds out there is field work behind
// the thing at all.
func TestRunGetReportsTheObservationFilesToAPerson(t *testing.T) {
	t.Chdir(tree(t, observedTree()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess,
		run([]string{"get", "--observations", "geom:V-01", "--format", formatHuman, "-v"}, &stdout, &stderr))

	assert.Contains(t, stderr.String(), "observed-in: observations/site-control.obs")
	assert.Contains(t, stderr.String(), "1 observation file, 2 records")
}

// combinedRegistry is the vocabulary [withCombined] adds to a fixture's
// registry. Every name in it is one no fixture declares already, so it can be
// appended to any of them.
const combinedRegistry = `
(namespace control (description "Survey control points."))
(namespace resurvey (description "Claim ids issued by the resurvey."))

(predicate width (unit m) (shape scalar) (description "How wide the thing is."))
(predicate depth (unit m) (shape scalar) (description "How deep the thing is."))
(predicate seats (shape scalar) (description "How many people it seats."))
`

// combinedModel is one room whose claims say each thing a combined accuracy
// can: one term, which is itself; an independent and a systematic term, which
// join in quadrature; terms in two units, which combine to nothing and say so;
// no accuracy at all, which writes neither field; and two depths tied on
// accuracy and date, which is a conflict whose claims carry the figure too.
//
// Its within is formatted in, because a plan reads only what is inside the
// storey it draws and every other command reads a node wherever it is.
const combinedModel = `
(node site:S-120
  (label "Meeting Room C")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)%s
  (width
    (id resurvey:W-0001)
    (value 8.5 m)
    (source "Plan set A-101, sheet 3")
    (method method:scaled-from-plan)
    (accuracy (independent 0.05 m))
    (date "2026-01-09"))
  (width
    (id resurvey:W-0002)
    (value 8.53 m)
    (source "As-built check AB-2026-009, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.003 m) (systematic 0.008 m control:CP-3))
    (date "2026-05-06"))
  (width
    (id resurvey:W-0003)
    (value 8.52 m)
    (source "Resurvey RS-2026-011")
    (method method:total-station)
    (accuracy (independent 1.0 mm) (systematic 0.001 m control:CP-3))
    (date "2026-09-28"))
  (depth
    (id resurvey:D-0001)
    (value 6.0 m)
    (source "Section A-A, sheet 5")
    (method method:tape)
    (accuracy (independent 0.01 m))
    (date "2026-04-01"))
  (depth
    (id resurvey:D-0002)
    (value 6.01 m)
    (source "Fit-out check FC-2026-002, Acme Surveys")
    (method method:tape)
    (accuracy (independent 0.01 m))
    (date "2026-04-01"))
  (seats
    (id resurvey:S-0001)
    (value 12.0)
    (source "Fire strategy FS-2026-001")
    (method method:assumed)
    (date "2026-03-14")))
`

// withCombined is a fixture tree with [combinedModel] added to it, inside the
// node within names where it names one.
//
// The model loads with one warning, for the claim whose accuracy mixes units,
// which is why it is added by the tests about the combined figure rather than
// written into a fixture every other test asserts loads clean.
func withCombined(files map[string]string, within string) map[string]string {
	clause := ""
	if within != "" {
		clause = "\n  (within " + within + ")"
	}

	files["registry.dfc"] += combinedRegistry
	files["entities/combined.dfc"] = fmt.Sprintf(combinedModel, clause)
	return files
}

// combinedClaims is what each claim of [combinedModel] carries beside its
// accuracy, by id: the combined figure, or the units which stopped there being
// one, or neither.
var combinedClaims = map[string]struct {
	combined *combinedUncertainty
	units    []string
}{
	"resurvey:W-0001": {combined: &combinedUncertainty{Magnitude: 0.05, Unit: "m", CoverageFactor: 1}},
	"resurvey:W-0002": {combined: &combinedUncertainty{Magnitude: 0.008544003745317531, Unit: "m", CoverageFactor: 1}},
	"resurvey:W-0003": {units: []string{"mm", "m"}},
	"resurvey:D-0001": {combined: &combinedUncertainty{Magnitude: 0.01, Unit: "m", CoverageFactor: 1}},
	"resurvey:D-0002": {combined: &combinedUncertainty{Magnitude: 0.01, Unit: "m", CoverageFactor: 1}},
	"resurvey:S-0001": {},
}

// assertCombined checks every claim of [combinedModel] among claims against
// [combinedClaims], and reports how many it found.
func assertCombined(t *testing.T, claims []claimEntry) int {
	t.Helper()

	found := 0
	for _, claim := range claims {
		expected, ok := combinedClaims[claim.ID]
		if !ok {
			continue
		}
		found++

		assert.Equal(t, expected.combined, claim.Combined, claim.ID)
		assert.Equal(t, expected.units, claim.Units, claim.ID)
		if expected.combined == nil && expected.units == nil {
			assert.Empty(t, claim.Accuracy, "%s: a claim with no accuracy says so by having none", claim.ID)
		}
	}
	return found
}

// TestRunGetCarriesEachClaimsAccuracyCombined checks the figure each claim's
// accuracy reduces to, beside the terms it was reduced from.
func TestRunGetCarriesEachClaimsAccuracyCombined(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected int
	}{
		{name: "on every claim written on the thing", args: []string{"site:S-120"}, expected: 6},
		{name: "on the claims resolution kept", args: []string{"--claims", claimsResolved, "site:S-120"}, expected: 4},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, withCombined(retrievable(), "")))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run(append([]string{"get"}, testCase.args...), &stdout, &stderr), stderr.String())

			result := listed[getResult](t, stdout.String())
			require.False(t, result.Refused, stderr.String())

			claims := result.Entity.Claims
			assert.Equal(t, testCase.expected, assertCombined(t, claims), "how many of the claims were checked")
		})
	}
}

// TestRunGetWritesTheCombinedFigureImmediatelyAfterTheAccuracy checks where the
// two fields sit in a claim object, which is part of the contract: beside the
// terms they were computed from.
func TestRunGetWritesTheCombinedFigureImmediatelyAfterTheAccuracy(t *testing.T) {
	t.Chdir(tree(t, withCombined(retrievable(), "")))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"get", "site:S-120"}, &stdout, &stderr), stderr.String())

	written := stdout.String()
	for _, fragment := range []string{
		`"accuracy":[{"kind":"independent","magnitude":0.05,"unit":"m"}],"combined":{"magnitude":0.05,"unit":"m","coverage-factor":1},"date"`,
		`"source":"control:CP-3"}],"units":["mm","m"],"date"`,
		`"method":"method:assumed","date"`,
	} {
		assert.Contains(t, written, fragment)
	}
}

// documentedFields is every field the `get` section of docs/machine-output.md
// names in the first column of one of its tables.
//
// A row may name more than one field — `entity.start` and `entity.end` share
// one — so each backquoted name in the first cell counts.
func documentedFields(t testing.TB) map[string]bool {
	t.Helper()

	fields := make(map[string]bool)
	for line := range strings.SplitSeq(contractSection(t, "get"), "\n") {
		cells := strings.Split(line, "|")
		if !strings.HasPrefix(line, "| `") || len(cells) < 3 {
			continue
		}

		names := strings.Split(cells[1], "`")
		for i := 1; i < len(names); i += 2 {
			fields[names[i]] = true
		}
	}
	return fields
}

// writtenByGet runs get over the retrieval fixture for one id and returns what
// reached stdout, undecoded, so that a test can read the keys it actually wrote
// rather than the ones a Go type happens to declare.
func writtenByGet(t *testing.T, id string) []byte {
	t.Helper()

	t.Chdir(tree(t, retrievable()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"get", id}, &stdout, &stderr), stderr.String())
	return stdout.Bytes()
}

// TestTheContractDocumentsEveryFieldGetWritesOnTheEntity checks that the `get`
// section of docs/machine-output.md has a row for every key get writes on
// "entity", read out of what reached stdout rather than listed here.
//
// It is what holds the contract page to the emitter: "assertions" was written
// on every answer from the day instance-level assertions landed while the page
// a caller programs against never mentioned it, and nothing noticed.
func TestTheContractDocumentsEveryFieldGetWritesOnTheEntity(t *testing.T) {
	documented := documentedFields(t)

	testCases := []struct {
		name string
		id   string
	}{
		{name: "documents every field of a node carrying assertions", id: "site:S-101"},
		{name: "documents every field of a vertex", id: "geom:V-01"},
		{name: "documents every field of an edge", id: "geom:E-01"},
		{name: "documents every field of a loop", id: "geom:L-01"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var written struct {
				Entity map[string]json.RawMessage `json:"entity"`
			}
			require.NoError(t, json.Unmarshal(writtenByGet(t, testCase.id), &written))
			require.NotEmpty(t, written.Entity, "get wrote an entity")

			for key := range written.Entity {
				assert.True(t, documented["entity."+key],
					"get writes entity.%s on %s and the get section of docs/machine-output.md has no row for it", key, testCase.id)
			}
		})
	}
}

// TestTheContractDocumentsEveryFieldOfAnAssertion checks that the `get` section
// of docs/machine-output.md has a row for every key an assertion carries, read
// out of what get wrote for a node whose assertions between them carry every
// optional field.
func TestTheContractDocumentsEveryFieldOfAnAssertion(t *testing.T) {
	documented := documentedFields(t)

	var written struct {
		Entity struct {
			Assertions []map[string]json.RawMessage `json:"assertions"`
		} `json:"entity"`
	}
	require.NoError(t, json.Unmarshal(writtenByGet(t, "site:S-101"), &written))
	require.NotEmpty(t, written.Entity.Assertions, "the fixture node carries assertions")

	keys := make(map[string]bool)
	for _, assertion := range written.Entity.Assertions {
		for key := range assertion {
			keys[key] = true
		}
	}
	require.True(t, keys["parameters"], "one of the fixture's assertions supplies parameters, so the optional field is exercised")

	for key := range keys {
		assert.True(t, documented["assertions[]."+key],
			"get writes assertions[].%s and the get section of docs/machine-output.md has no row for it", key)
	}
}

// TestSeveralIDsErrorPointsAtStandardInput checks that the refusal of several
// id arguments names the way to retrieve several, so that the misuse points at
// the fix.
func TestSeveralIDsErrorPointsAtStandardInput(t *testing.T) {
	err := SeveralIDsError{IDs: []string{"site:S-111", "site:S-112"}}

	assert.Contains(t, err.Error(), "write "+stdinPath)
}

// gotMany runs get - over the representative model with written on standard
// input, requiring the exit code it was told to expect.
func gotMany(t *testing.T, expectedCode int, written string, args ...string) (stdout, stderr string) {
	t.Helper()

	return piped(t, expectedCode, budgetRoot, written, append([]string{"get"}, append(args, stdinPath)...)...)
}

// entityIDs is the id of each entity, in the order they were answered.
func entityIDs(entities []getEntity) []string {
	out := make([]string, 0, len(entities))
	for _, entity := range entities {
		out = append(out, entity.ID)
	}
	return out
}

// TestRunGetManyAnswersEveryFamilyInOneObject is the batch this form exists
// for: one id of each family, one load, one object, with "entities" in place of
// "entity".
func TestRunGetManyAnswersEveryFamilyInOneObject(t *testing.T) {
	stdout, stderr := gotMany(t, exitSuccess, "site:S-111\ngeom:V-1-A1\ngeom:E-1-A1-A2\ngeom:L-101\n")

	assert.Empty(t, stderr)

	var keys map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(stdout), &keys))
	assert.Contains(t, keys, "entities")
	assert.NotContains(t, keys, "entity", "the batch answers in one shape, and it is not the shape of one id")

	result := listed[getBatchResult](t, stdout)

	assert.Equal(t, outputVersion, result.Version)
	assert.Equal(t, "get", result.Command)
	assert.False(t, result.Refused)

	families := make(map[string]string)
	for _, entity := range result.Entities {
		families[entity.ID] = entity.Family
	}
	assert.Equal(t, map[string]string{
		"geom:E-1-A1-A2": familyEdge,
		"geom:L-101":     familyLoop,
		"geom:V-1-A1":    familyVertex,
		"site:S-111":     familyNode,
	}, families)
}

func TestRunGetManyIsInIDOrderAndAnswersEachIDOnce(t *testing.T) {
	testCases := []struct {
		name    string
		written string
	}{
		{
			name:    "answers in id order whatever order the ids were written in",
			written: "site:S-112\ngeom:V-1-A1\nsite:S-111\n",
		},
		{
			name:    "reads ids separated by any whitespace, not only by lines",
			written: "site:S-111 geom:V-1-A1\tsite:S-112",
		},
		{
			name:    "answers an id written more than once once",
			written: "site:S-112\nsite:S-111\n\nsite:S-112\n  geom:V-1-A1\nsite:S-111\n",
		},
	}

	// Every case names the same three ids, so every case writes the same
	// bytes: the order of standard input changes nothing on stdout.
	expected, _ := gotMany(t, exitSuccess, "geom:V-1-A1\nsite:S-111\nsite:S-112\n")
	assert.Equal(t,
		[]string{"geom:V-1-A1", "site:S-111", "site:S-112"},
		entityIDs(listed[getBatchResult](t, expected).Entities))

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stdout, _ := gotMany(t, exitSuccess, testCase.written)

			assert.Equal(t, expected, stdout)
		})
	}
}

func TestRunGetManyWithNoIDAnswersNothing(t *testing.T) {
	testCases := []struct {
		name    string
		written string
	}{
		{name: "answers an empty list for empty input", written: ""},
		{name: "answers an empty list for input holding only whitespace", written: "\n  \t\n\n"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stdout, stderr := gotMany(t, exitSuccess, testCase.written)

			assert.Empty(t, stderr)
			assert.Contains(t, stdout, `"entities":[]`, "empty rather than null")

			result := listed[getBatchResult](t, stdout)
			assert.Equal(t, "get", result.Command)
			assert.Empty(t, result.Entities)
		})
	}
}

func TestRetrieveAllReportsEveryIDItCannotRetrieve(t *testing.T) {
	graph, diags := dfcad.LoadGraph(budgetRoot)
	require.Empty(t, diags)

	testCases := []struct {
		name     string
		written  []string
		expected []error
	}{
		{
			name:    "reports an unknown id with the nearest id there is",
			written: []string{"site:S-111", "site:S-1O2"},
			expected: []error{
				UnknownIDError{ID: "site:S-1O2", Nearest: "site:S-102"},
			},
		},
		{
			name:    "reports an unknown id nothing is close to",
			written: []string{"other:nothing-like-it"},
			expected: []error{
				UnknownIDError{ID: "other:nothing-like-it"},
			},
		},
		{
			name:    "reports a malformed id with the rule it broke",
			written: []string{"S-111"},
			expected: []error{
				dfcad.MalformedIDError{Written: "S-111", Reason: dfcad.IDUnqualified},
			},
		},
		{
			name:    "reports every one of them rather than the first, in the order of the ids",
			written: []string{"site:S-1O2", "site:S-111", "S-111", "other:nothing-like-it", "site:S-1O2"},
			expected: []error{
				dfcad.MalformedIDError{Written: "S-111", Reason: dfcad.IDUnqualified},
				UnknownIDError{ID: "other:nothing-like-it"},
				UnknownIDError{ID: "site:S-1O2", Nearest: "site:S-102"},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			entities, err := retrieveAll(graph, testCase.written)

			assert.Nil(t, entities, "no partial answer")

			var batch BatchIDsError
			require.True(t, errors.As(err, &batch), "expected BatchIDsError, got %T", err)
			assert.Equal(t, testCase.expected, batch.Errs)

			// Each kind of element is reachable through the batch itself, which
			// is what Unwrap() []error is for: a caller asks errors.As of the
			// one error it was handed.
			for _, each := range testCase.expected {
				switch each.(type) {
				case UnknownIDError:
					var got UnknownIDError
					require.True(t, errors.As(err, &got), "errors.As reaches an unknown id through the batch")
					assert.Contains(t, testCase.expected, got)
				case dfcad.MalformedIDError:
					var got dfcad.MalformedIDError
					require.True(t, errors.As(err, &got), "errors.As reaches a malformed id through the batch")
					assert.Contains(t, testCase.expected, got)
				}
			}
		})
	}
}

// TestRunGetManyReportsEveryBadIDAndWritesNothing is the command's half of the
// rejection: exit 3, nothing on stdout, and every bad id on a line of its own in
// the words a get of that id alone would use.
func TestRunGetManyReportsEveryBadIDAndWritesNothing(t *testing.T) {
	stdout, stderr := gotMany(t, exitUsage, "site:S-1O2\nsite:S-111\nS-111\nother:nothing-like-it\n")

	assert.Empty(t, stdout, "no partial answer")
	assert.Equal(t,
		"dfcad get: "+dfcad.MalformedIDError{Written: "S-111", Reason: dfcad.IDUnqualified}.Error()+"\n"+
			"dfcad get: "+UnknownIDError{ID: "other:nothing-like-it"}.Error()+"\n"+
			"dfcad get: "+UnknownIDError{ID: "site:S-1O2", Nearest: "site:S-102"}.Error()+"\n",
		stderr)
}

// TestRunGetManyTakesFlagsOnEitherSideOfStandardInput checks that - is an
// argument like any other, which flags may be written before or after.
func TestRunGetManyTakesFlagsOnEitherSideOfStandardInput(t *testing.T) {
	before, _ := piped(t, exitSuccess, budgetRoot, "site:S-111", "get", "--claims", claimsResolved, stdinPath)
	after, _ := piped(t, exitSuccess, budgetRoot, "site:S-111", "get", stdinPath, "--claims", claimsResolved)

	assert.Equal(t, before, after)

	entities := listed[getBatchResult](t, before).Entities
	require.Len(t, entities, 1)
	for _, claim := range entities[0].Claims {
		assert.NotEmpty(t, claim.Resolution, "the flag reached the batch")
	}
}

// everyID is every id the representative model holds, in all four families.
func everyID(t *testing.T) []string {
	t.Helper()

	graph, diags := dfcad.LoadGraph(budgetRoot)
	require.Empty(t, diags)

	var out []string
	for node := range graph.Nodes().All() {
		out = append(out, string(node.ID()))
	}
	for vertex := range graph.Topology().Vertices() {
		out = append(out, string(vertex.ID()))
	}
	for edge := range graph.Topology().Edges() {
		out = append(out, string(edge.ID()))
	}
	for loop := range graph.Topology().Loops() {
		out = append(out, string(loop.ID()))
	}
	return out
}

// TestRunGetManyAnswersEachIDAsGetOfItAlone is the property the batch rests on:
// for every id the model holds, its element of get - is exactly the entity get
// of that id writes, compared as parsed JSON.
func TestRunGetManyAnswersEachIDAsGetOfItAlone(t *testing.T) {
	ids := everyID(t)
	require.NotEmpty(t, ids)

	for _, selection := range claimSelections {
		t.Run("under --claims "+selection+" answers each id as get of it alone", func(t *testing.T) {
			stdout, _ := gotMany(t, exitSuccess, strings.Join(ids, "\n"), "--claims", selection)

			var batch struct {
				Entities []map[string]any `json:"entities"`
			}
			require.NoError(t, json.Unmarshal([]byte(stdout), &batch))
			require.Len(t, batch.Entities, len(ids))

			elements := make(map[string]map[string]any, len(batch.Entities))
			for _, element := range batch.Entities {
				elements[element["id"].(string)] = element
			}

			for _, id := range ids {
				alone, _ := invoke(t, exitSuccess, budgetRoot, "get", "--claims", selection, id)

				var single struct {
					Entity map[string]any `json:"entity"`
				}
				require.NoError(t, json.Unmarshal([]byte(alone), &single))

				assert.Equal(t, single.Entity, elements[id], id)
			}
		})
	}
}

// sharedObservedTree is [observedTree] with a third corner whose links share a
// file with the second, so that a batch of the two reads that file for both.
func sharedObservedTree() map[string]string {
	files := observedTree()
	files["entities/geometry.dfc"] = observedModel + `
(vertex geom:V-03
  (label "The corner shot from both setups")
  (frame frame:site)
  (observed-in "observations/site-control.obs")
  (observed-in "observations/suspect.obs"))
`
	return files
}

// TestRunGetManyRendersAProblemInASharedFileOnce checks that a file several of
// the entities link to is reported on once, however many of them link to it.
func TestRunGetManyRendersAProblemInASharedFileOnce(t *testing.T) {
	root := tree(t, sharedObservedTree())

	_, alone := invoke(t, exitSuccess, root, "get", "--observations", "geom:V-02")
	require.Contains(t, alone, "suspect.obs:1:")

	stdout, stderr := piped(t, exitSuccess, root, "geom:V-02\ngeom:V-03\n", "get", "--observations", stdinPath)

	assert.Equal(t, strings.Count(alone, "suspect.obs:1:"), strings.Count(stderr, "suspect.obs:1:"),
		"the problem in the shared file is rendered once, as it is for one entity")

	entities := listed[getBatchResult](t, stdout).Entities
	require.Len(t, entities, 2)
	require.NotNil(t, entities[0].Records)
	require.NotNil(t, entities[1].Records)
	assert.Empty(t, *entities[0].Records)
	assert.Len(t, *entities[1].Records, 2, "the sound file behind the third corner is read for it")
}

// TestRunGetManyHumanOutputNeverChangesStdout checks the human rendering of a
// batch: each entity summarised as get of it alone would, and a line counting
// them, all on stderr.
func TestRunGetManyHumanOutputNeverChangesStdout(t *testing.T) {
	machine, machineReport := gotMany(t, exitSuccess, "site:S-111\ngeom:V-1-A1\n")
	human, humanReport := gotMany(t, exitSuccess, "site:S-111\ngeom:V-1-A1\n", "--format", formatHuman)

	assert.Equal(t, machine, human)
	assert.Empty(t, machineReport)

	assert.Contains(t, humanReport, "vertex geom:V-1-A1 at ")
	assert.Contains(t, humanReport, "node site:S-111 at ")
	assert.True(t, strings.HasSuffix(humanReport, "\n2 entities\n"), humanReport)

	_, emptyReport := gotMany(t, exitSuccess, "", "--format", formatHuman)
	assert.Equal(t, "0 entities\n", emptyReport)
}

// plainRegistry is the retrieval vocabulary with three predicates declared
// non-claim-bearing: two of text, one of them written twice below, and a
// dimensional scalar, which is what shows the unit travelling with the value.
const plainRegistry = getRegistry + `
(predicate crs
  (shape text)
  (claim-bearing #f)
  (description "The coordinate reference system it is expressed in."))

(predicate finish
  (shape text)
  (claim-bearing #f)
  (description "What it is finished in."))

(predicate nominal-width
  (unit m)
  (shape scalar)
  (claim-bearing #f)
  (description "The width it was designed to."))
`

// plainModel is a room carrying one claim and four plain values, written out of
// predicate order so that the order the answer puts them in is its own.
const plainModel = `(node site:S-201
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (area
    (value 12.0 m2)
    (source "As-built check AB-2026-009, Acme Surveys")
    (method method:tape)
    (accuracy (independent 0.1 m2))
    (date "2026-05-06"))
  (nominal-width 3.5 m)
  (finish "Oak")
  (crs "EPSG:1234")
  (finish "Walnut"))
`

// plainTree is the retrieval fixture with plainModel beside it, so that a thing
// carrying plain values and one carrying none are asked about in one model.
func plainTree() map[string]string {
	files := retrievable()
	files["registry.dfc"] = plainRegistry
	files["entities/plain.dfc"] = plainModel
	return files
}

// gotPlain runs get over plainTree and returns what reached stdout.
func gotPlain(t *testing.T, args ...string) string {
	t.Helper()

	t.Chdir(tree(t, plainTree()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run(append([]string{"get"}, args...), &stdout, &stderr), stderr.String())
	require.Empty(t, stderr.String(), "the fixture loads clean")

	return stdout.String()
}

// plainValues is each value as "predicate value", which says both which came
// back and in which order.
func plainValues(values []valueEntry) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.Predicate+" "+spellClaimValue(value.Value))
	}
	return out
}

func TestRunGetReportsThePlainValuesWrittenOnIt(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "reports every plain value in predicate order and then by where each was written",
			args:     []string{"site:S-201"},
			expected: []string{`crs "EPSG:1234"`, `finish "Oak"`, `finish "Walnut"`, "nominal-width 3.5 m"},
		},
		{
			name:     "reports the same plain values when the claims are resolved",
			args:     []string{"site:S-201", "--claims", claimsResolved},
			expected: []string{`crs "EPSG:1234"`, `finish "Oak"`, `finish "Walnut"`, "nominal-width 3.5 m"},
		},
		{
			name:     "reports the same plain values when deprecated claims are asked for",
			args:     []string{"site:S-201", "--deprecated"},
			expected: []string{`crs "EPSG:1234"`, `finish "Oak"`, `finish "Walnut"`, "nominal-width 3.5 m"},
		},
		{
			name: "reports none on a thing which carries none",
			args: []string{"site:S-101"},
		},
		{
			name: "reports none on a vertex which carries none",
			args: []string{"geom:V-01"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			entity := listed[getResult](t, gotPlain(t, testCase.args...)).Entity

			if testCase.expected == nil {
				assert.Nil(t, entity.Values)
				return
			}
			assert.Equal(t, testCase.expected, plainValues(entity.Values))
		})
	}
}

// TestRunGetWritesAPlainValueInTheShapeAClaimsValueTakes checks each plain
// value field by field: the shape named, the text or the scalar carried where
// that shape says, the unit beside a dimensional one, and the span of the form
// that wrote it.
func TestRunGetWritesAPlainValueInTheShapeAClaimsValueTakes(t *testing.T) {
	testCases := []struct {
		name      string
		predicate string
		expected  claimValue
		written   string
	}{
		{
			name:      "writes a text value as text, with no unit",
			predicate: "crs",
			expected:  claimValue{Shape: string(dfcad.ShapeText), Text: ptr("EPSG:1234")},
			written:   `(crs "EPSG:1234")`,
		},
		{
			name:      "writes a scalar value with the unit written beside it",
			predicate: "nominal-width",
			expected:  claimValue{Shape: string(dfcad.ShapeScalar), Unit: "m", Scalar: ptr(3.5)},
			written:   "(nominal-width 3.5 m)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			entity := listed[getResult](t, gotPlain(t, "site:S-201")).Entity

			var found []valueEntry
			for _, value := range entity.Values {
				if value.Predicate == testCase.predicate {
					found = append(found, value)
				}
			}
			require.Len(t, found, 1)
			assert.Equal(t, testCase.expected, found[0].Value)

			// The span is the whole of the form which wrote it.
			line := strings.Split(plainModel, "\n")[found[0].Span.Start.Line-1]
			column := strings.Index(line, testCase.written) + 1
			assert.Equal(t, "entities/plain.dfc", found[0].Span.Start.Path)
			assert.Equal(t, column, found[0].Span.Start.Column)
			assert.Equal(t, found[0].Span.Start.Line, found[0].Span.End.Line)
			assert.Equal(t, column+len(testCase.written), found[0].Span.End.Column)
		})
	}
}

// ptr is a pointer to v, which is how a value's optional fields are spelled.
func ptr[T any](v T) *T { return &v }

// entityKeys is the keys of the object written under "entity", in the order
// they were written.
func entityKeys(t *testing.T, stdout string) []string {
	t.Helper()

	var written struct {
		Entity json.RawMessage `json:"entity"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &written))

	decoder := json.NewDecoder(bytes.NewReader(written.Entity))
	_, err := decoder.Token()
	require.NoError(t, err)

	var keys []string
	for decoder.More() {
		key, err := decoder.Token()
		require.NoError(t, err)
		keys = append(keys, key.(string))

		var skipped json.RawMessage
		require.NoError(t, decoder.Decode(&skipped))
	}
	return keys
}

// TestRunGetWritesThePlainValuesImmediatelyAfterTheClaims checks where the
// field goes and that it is not there at all where there is nothing to put in
// it, which is what keeps every answer over a model without plain values the
// bytes it was.
func TestRunGetWritesThePlainValuesImmediatelyAfterTheClaims(t *testing.T) {
	keys := entityKeys(t, gotPlain(t, "site:S-201"))

	claims := slices.Index(keys, "claims")
	require.GreaterOrEqual(t, claims, 0)
	require.Less(t, claims+1, len(keys))
	assert.Equal(t, "values", keys[claims+1])

	assert.NotContains(t, entityKeys(t, gotPlain(t, "site:S-101")), "values")
	assert.NotContains(t, gotPlain(t, "site:S-101"), `"values"`)
}

// TestRunGetWritesAPlainValueAsTheContractSpellsIt checks the bytes of one
// entry against the spelling docs/machine-output.md gives it.
func TestRunGetWritesAPlainValueAsTheContractSpellsIt(t *testing.T) {
	stdout := gotPlain(t, "site:S-201")

	assert.Contains(t, stdout,
		`"values":[{"predicate":"crs","value":{"shape":"text","text":"EPSG:1234"},"span":"entities/plain.dfc:15:3-15:20"}`)
}

// TestRunGetLeavesPlainValuesOutOfTheClaims checks that a plain value is not
// dressed as a claim anywhere a claim is reported: not among get's claims, not
// in the audit view, and not as the answer resolution gives.
func TestRunGetLeavesPlainValuesOutOfTheClaims(t *testing.T) {
	t.Run("get reports only the claim among the claims", func(t *testing.T) {
		entity := listed[getResult](t, gotPlain(t, "site:S-201")).Entity
		assert.Equal(t, []string{"area"}, predicates(entity.Claims))
	})

	t.Run("claims reports only the claim", func(t *testing.T) {
		t.Chdir(tree(t, plainTree()))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run([]string{"claims", "site:S-201"}, &stdout, &stderr), stderr.String())

		result := listed[claimsResult](t, stdout.String())
		require.Len(t, result.Claims, 1)
		assert.Equal(t, "area", result.Claims[0].Predicate)
	})

	t.Run("resolve answers unclaimed under a predicate holding only a plain value", func(t *testing.T) {
		t.Chdir(tree(t, plainTree()))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitCheck, run([]string{"resolve", "site:S-201", "crs"}, &stdout, &stderr), stderr.String())

		result := listed[resolveResult](t, stdout.String())
		assert.Equal(t, "unclaimed", result.Outcome)
	})
}

// TestRunGetListsThePlainValuesToAPersonAfterTheClaims checks the human
// rendering: each plain value on a line of its own, said to be one, after the
// claims and before the assertions.
func TestRunGetListsThePlainValuesToAPersonAfterTheClaims(t *testing.T) {
	t.Chdir(tree(t, plainTree()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"get", "site:S-201", "--format", formatHuman, "-v"}, &stdout, &stderr), stderr.String())

	report := stderr.String()
	claim := strings.Index(report, "area: 12 m2 by method:tape")
	crs := strings.Index(report, `crs: "EPSG:1234", plain value`)
	width := strings.Index(report, "nominal-width: 3.5 m, plain value")

	require.GreaterOrEqual(t, claim, 0, report)
	require.GreaterOrEqual(t, crs, 0, report)
	require.GreaterOrEqual(t, width, 0, report)
	assert.Less(t, claim, crs, "the plain values come after the claims")
	assert.Less(t, crs, width, "in predicate order")
}

// TestTheContractDocumentsEveryFieldOfAPlainValue checks that the `get` section
// of docs/machine-output.md has a row for entity.values and for every key one
// of its entries carries, read out of what get wrote rather than listed here.
func TestTheContractDocumentsEveryFieldOfAPlainValue(t *testing.T) {
	documented := documentedFields(t)

	var written struct {
		Entity struct {
			Values []map[string]json.RawMessage `json:"values"`
		} `json:"entity"`
	}
	require.NoError(t, json.Unmarshal([]byte(gotPlain(t, "site:S-201")), &written))
	require.NotEmpty(t, written.Entity.Values, "get wrote the plain values")

	assert.True(t, documented["entity.values"], "the get section of docs/machine-output.md has no row for entity.values")
	for _, value := range written.Entity.Values {
		for key := range value {
			assert.True(t, documented["values[]."+key],
				"get writes values[].%s and the get section of docs/machine-output.md has no row for it", key)
		}
	}
}
