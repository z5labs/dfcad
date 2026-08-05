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
func TestRunGetSaysWhereItWasDefined(t *testing.T) {
	entity := retrieved(t, "site:E-01")

	assert.Contains(t, entity.Span.Start.Path, "site.dfc")
	assert.Positive(t, entity.Span.Start.Line)
	assert.Positive(t, entity.Span.Start.Column)
	assert.Greater(t, entity.Span.End.Offset, entity.Span.Start.Offset)
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
			name: "rejects a second id",
			args: []string{"get", "site:S-101", "site:Z-01"},
			expectedStderr: "dfcad get: " +
				UnexpectedArgumentsError{Extra: []string{"site:Z-01"}}.Error() + "\n\n" + getUsage,
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
