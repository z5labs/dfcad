// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The vocabulary the storey fixture below is read and annotated with.
const (
	planPosition  = "position"
	planTolerance = "coincident"
	planArea      = "area"
	planLength    = "wall-length"
	planCaption   = "caption"
)

// planFixtureRoot is the model every case below is read from: a storey holding
// two rooms which share a party wall, an alcove inside the first, a doorway
// with no outline and a zone which is not a place at all.
const planFixtureRoot = "testdata/plan/storey"

// planFixture is the storey fixture loaded, with the survey every ring below is
// read against.
type planFixture struct {
	graph  *Graph
	survey Survey
}

// storey loads the fixture, failing the test where any pass reports anything.
func storey(t *testing.T) planFixture {
	t.Helper()

	graph, diags := LoadGraph(planFixtureRoot)
	require.Empty(t, diagnosticMessages(diags), "the fixture loads clean")

	// One survey over every corner of the model rather than one per room. A
	// corner read against two surveys is a corner which can be in two places,
	// and two rooms sharing a wall is where that shows up as a gap down the
	// middle of a sheet.
	survey := Survey{Tolerance: planTolerance, Registry: graph.Registry()}
	for vertex := range graph.Topology().Vertices() {
		resolution, err := graph.Claims().Resolve(vertex.ID(), planPosition, graph.Registry())
		require.NoError(t, err)

		survey.Place(vertex.ID(), resolution)
	}

	return planFixture{graph: graph, survey: survey}
}

// plan reads the plan of one node of the fixture under the predicates named.
func (f planFixture) plan(t *testing.T, id ID, predicates ...string) (Plan, []Diagnostic) {
	t.Helper()

	node, ok := f.graph.Node(id)
	require.True(t, ok, "the fixture holds a node %s", id)

	return f.graph.PlanOf(node, f.survey, Annotations{Predicates: predicates})
}

// drawnIDs is the id of each outline, which is what a case asserting about
// which nodes were drawn and in what order reads.
func drawnIDs(plan Plan) []string {
	out := make([]string, 0, len(plan.Outlines()))
	for _, outline := range plan.Outlines() {
		out = append(out, string(outline.Subject()))
	}
	return out
}

// annotated is every claim of one outline as "predicate value @ anchor", which
// is the whole of what a reported claim says in one readable line.
func annotated(plan Plan, node ID) []string {
	var out []string

	for _, outline := range plan.Outlines() {
		if outline.Subject() != node {
			continue
		}
		for _, annotation := range outline.Annotations() {
			out = append(out, fmt.Sprintf("%s %s @ %s",
				annotation.Predicate(), spelledValue(annotation.Claim().Value()), annotation.Anchor(),
			))
		}
	}

	return out
}

// spelledValue is a claim's value as the lines above spell it.
func spelledValue(value Value) string {
	if text, ok := value.Text(); ok {
		return fmt.Sprintf("%q", text)
	}
	if scalar, ok := value.Scalar(); ok {
		return decimal(scalar) + unitSuffix(value.Unit())
	}
	return string(value.Shape())
}

func TestPlanOf(t *testing.T) {
	testCases := []struct {
		name             string
		subject          ID
		predicates       []string
		expectedOutlines []string
	}{
		{
			name:       "draws every ring the storey contains, however deep",
			subject:    "site:L-01",
			predicates: []string{planArea},
			// The alcove is a space inside a space, so a walk which stopped at
			// the storey's own children would leave it off the sheet. The
			// doorway and the railing are drawn as lines, and a run is drawn
			// beside a ring rather than instead of one.
			expectedOutlines: []string{"site:A-01", "site:D-01", "site:H-01", "site:R-01", "site:R-02"},
		},
		{
			name:             "draws what one room contains rather than the room",
			subject:          "site:R-01",
			predicates:       []string{planArea},
			expectedOutlines: []string{"site:A-01"},
		},
		{
			name:             "draws the whole building through the storey below it",
			subject:          "site:B-01",
			predicates:       []string{planArea},
			expectedOutlines: []string{"site:A-01", "site:D-01", "site:H-01", "site:R-01", "site:R-02"},
		},
		{
			name:             "draws nothing for a room nothing is inside",
			subject:          "site:R-02",
			predicates:       []string{planArea},
			expectedOutlines: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			plan, diags := storey(t).plan(t, testCase.subject, testCase.predicates...)

			assert.Empty(t, diagnosticMessages(diags))
			assert.Equal(t, testCase.expectedOutlines, drawnIDs(plan))
			assert.Equal(t, testCase.subject, plan.Subject())
		})
	}
}

// TestPlanOfLeavesOutWhatHasNoRing is its own function because it is about what
// is absent from the answer rather than about what one entry says.
func TestPlanOfLeavesOutWhatHasNoRing(t *testing.T) {
	plan, diags := storey(t).plan(t, "site:L-01", planArea, planCaption)

	require.Empty(t, diagnosticMessages(diags))

	// The circuit group is contained by the storey and carries a caption
	// somebody wrote for a sheet. It references no loop at all, so there is
	// nothing of it to draw — and that is ordinary rather than a defect in the
	// model.
	assert.NotContains(t, drawnIDs(plan), "site:C-01")
	assert.Empty(t, annotated(plan, "site:C-01"))
}

// TestPlanOfReportsWhatEachClaimIsWrittenOn is its own function because the
// anchor is the whole point of the answer: a claim which cannot say which pair
// of corners it belongs to leaves a renderer to work it out, which is the
// re-derivation this query exists to prevent.
func TestPlanOfReportsWhatEachClaimIsWrittenOn(t *testing.T) {
	plan, diags := storey(t).plan(t, "site:L-01", planArea, planCaption, planLength)

	require.Empty(t, diagnosticMessages(diags))

	assert.Equal(t, []string{
		// The node's own claims first, in the order the predicates were named,
		// and within one predicate in the order they were written.
		`area 12.0 m2 @ node site:R-01, ring geom:L-01`,
		`caption "Meeting Room A" @ node site:R-01, ring geom:L-01`,
		`caption "MR-A" @ node site:R-01, ring geom:L-01`,
		// Then each edge of the boundary, in the order the ring traverses them,
		// each naming its two vertices in the order the edge was authored.
		`wall-length 4.0 m @ edge geom:E-01, geom:V-01 to geom:V-02`,
		`wall-length 3.0 m @ edge geom:E-02, geom:V-02 to geom:V-03`,
		`wall-length 3.02 m @ edge geom:E-02, geom:V-02 to geom:V-03`,
		`wall-length 4.0 m @ edge geom:E-03, geom:V-03 to geom:V-04`,
	}, annotated(plan, "site:R-01"))
}

// TestPlanOfReportsBothLiveClaims is its own function because it asserts that
// resolution is not applied rather than that it is applied a particular way.
//
// Two people measured the party wall and nobody reconciled them. A query which
// picked one would be deciding, invisibly and in the engine, what a sheet
// prints — which is exactly the decision a plan exists to hand back.
func TestPlanOfReportsBothLiveClaims(t *testing.T) {
	fixture := storey(t)

	plan, diags := fixture.plan(t, "site:L-01", planLength)
	require.Empty(t, diagnosticMessages(diags))

	assert.Equal(t, []string{
		`wall-length 3.0 m @ edge geom:E-02, geom:V-02 to geom:V-03`,
		`wall-length 3.02 m @ edge geom:E-02, geom:V-02 to geom:V-03`,
	}, party(plan, "site:R-01"))

	// Resolution, asked the same question, answers with one of them. That the
	// two differ is the finding: the plan is not a formatting of the resolved
	// answer.
	resolution, err := fixture.graph.Claims().Resolve("geom:E-02", planLength, fixture.graph.Registry())
	require.NoError(t, err)

	claim, resolved := resolution.Claim()
	require.True(t, resolved)

	value, ok := claim.Value().Scalar()
	require.True(t, ok)
	assert.InDelta(t, 3.02, value, 1e-9)
}

// party is the claims of one outline written on the shared edge.
func party(plan Plan, node ID) []string {
	var out []string
	for _, line := range annotated(plan, node) {
		if strings.Contains(line, "geom:E-02") {
			out = append(out, line)
		}
	}
	return out
}

// TestPlanOfNeverReportsARetractedClaim is its own function because it is about
// a claim which must not be in the answer at all.
func TestPlanOfNeverReportsARetractedClaim(t *testing.T) {
	plan, diags := storey(t).plan(t, "site:L-01", planLength)

	require.Empty(t, diagnosticMessages(diags))

	written := strings.Join(annotated(plan, "site:R-01"), "\n")

	assert.Contains(t, written, "4.0 m @ edge geom:E-03")
	assert.NotContains(t, written, "3.9 m", "a length somebody withdrew is never drawn")
}

// TestPlanOfAnchorsOneEdgeTheSameWayFromBothSides is its own function because
// the shared edge is traversed opposite ways by the two rings which reference
// it, and a claim written on it is written on the edge rather than on either
// traversal.
func TestPlanOfAnchorsOneEdgeTheSameWayFromBothSides(t *testing.T) {
	plan, diags := storey(t).plan(t, "site:L-01", planLength)

	require.Empty(t, diagnosticMessages(diags))

	assert.Equal(t, party(plan, "site:R-01"), party(plan, "site:R-02"),
		"one edge with one identity carries one anchor, whichever room reports it")

	// The direction the ring runs through it is still recoverable, from the
	// region's own boundary rather than from the anchor.
	assert.True(t, reversedThrough(plan, "site:R-02", "geom:E-02"))
	assert.False(t, reversedThrough(plan, "site:R-01", "geom:E-02"))
}

// reversedThrough reports whether one outline's boundary runs through an edge
// against the order that edge was written.
func reversedThrough(plan Plan, node ID, edge ID) bool {
	for _, outline := range plan.Outlines() {
		if outline.Subject() != node {
			continue
		}
		for _, segment := range outline.Region().Segments() {
			if segment.Edge() != nil && segment.Edge().ID() == edge {
				return segment.Reversed()
			}
		}
	}
	return false
}

// TestPlanOfReportsOnePredicateOnce is its own function because a caller's
// typing mistake must not become a duplicated dimension on a sheet.
func TestPlanOfReportsOnePredicateOnce(t *testing.T) {
	plan, diags := storey(t).plan(t, "site:L-01", planArea, planArea)

	require.Empty(t, diagnosticMessages(diags))
	assert.Equal(t, []string{`area 12.0 m2 @ node site:R-01, ring geom:L-01`}, annotated(plan, "site:R-01"))
}

// TestPlanOfIsDeterministic is its own function because the ordering is a
// promise about the whole answer rather than about any entry of it.
func TestPlanOfIsDeterministic(t *testing.T) {
	fixture := storey(t)

	first, _ := fixture.plan(t, "site:L-01", planArea, planCaption, planLength)
	second, _ := fixture.plan(t, "site:L-01", planArea, planCaption, planLength)

	assert.Equal(t, drawnIDs(first), drawnIDs(second))
	for _, outline := range first.Outlines() {
		assert.Equal(t, annotated(first, outline.Subject()), annotated(second, outline.Subject()))
	}

	// The order is by id, which is a property of what the model says rather
	// than of which file each node was written in.
	assert.Equal(t, []string{"site:A-01", "site:D-01", "site:H-01", "site:R-01", "site:R-02"}, drawnIDs(first))
}

// TestPlanOfBudgetsTheGeometryAndNotTheAnnotations is its own function because
// what the budget is over is a decision rather than an accumulation: the rings
// are what the plan derived, and each reported claim is a separate statement
// about a separate quantity.
func TestPlanOfBudgetsTheGeometryAndNotTheAnnotations(t *testing.T) {
	plan, diags := storey(t).plan(t, "site:L-01", planArea, planLength)

	require.Empty(t, diagnosticMessages(diags))

	combined, err := plan.Budget().Combined()
	require.NoError(t, err)

	assert.Equal(t, Unit("m"), combined.Unit)
	assert.Positive(t, combined.Magnitude)

	// Every contributor is a position claim. An area and a wall length are
	// reported values rather than inputs to the rings, and a budget which mixed
	// metres with square metres would combine to nothing at all.
	for _, term := range plan.Budget().Terms() {
		assert.Equal(t, Unit("m"), term.Unit, term.Name)
	}
}

// TestPlanOfCarriesTheFrameAndTheTolerance is its own function because they are
// properties of the answer as a whole rather than of any one ring.
func TestPlanOfCarriesTheFrameAndTheTolerance(t *testing.T) {
	plan, diags := storey(t).plan(t, "site:L-01", planArea)

	require.Empty(t, diagnosticMessages(diags))

	assert.Equal(t, ID("frame:building"), plan.Frame())
	assert.Equal(t, Unit("m"), plan.Unit())
	assert.Equal(t, planTolerance, plan.Tolerance().Name)
	assert.False(t, plan.Empty())
	assert.Equal(t, 3, plan.Annotations())
}

// TestPlanOfAZeroValueAnswersNothing is its own function because the zero value
// is the state a refusal leaves behind, and every method has to work on it.
func TestPlanOfAZeroValueAnswersNothing(t *testing.T) {
	var plan Plan

	assert.True(t, plan.Empty())
	assert.Empty(t, plan.Outlines())
	assert.Zero(t, plan.Annotations())
	assert.Equal(t, "nothing was planned", plan.String())
	assert.Equal(t, "nothing was planned", plan.Report())

	var outline Outline
	assert.Equal(t, ID(""), outline.Subject())
	assert.Equal(t, "nothing", outline.String())

	var annotation Annotation
	assert.Equal(t, "", annotation.Predicate())
	assert.Equal(t, "nothing", annotation.String())

	var anchor Anchor
	assert.Equal(t, "nothing", anchor.String())
	assert.Empty(t, anchor.Rings())

	_, _, ok := anchor.Vertices()
	assert.False(t, ok)
}

// TestPlanOfANilNodeAnswersNothing is its own function because a caller which
// looked a node up and did not find it must not get a plan of something else.
func TestPlanOfANilNodeAnswersNothing(t *testing.T) {
	fixture := storey(t)

	plan, diags := fixture.graph.PlanOf(nil, fixture.survey, Annotations{Predicates: []string{planArea}})

	assert.Empty(t, diags)
	assert.True(t, plan.Empty())
	assert.Equal(t, ID(""), plan.Subject())
}

// TestPlanOfRendersForAPerson is its own function because the human rendering is
// a different shape of assertion from the machine one.
func TestPlanOfRendersForAPerson(t *testing.T) {
	plan, diags := storey(t).plan(t, "site:L-01", planArea, planCaption)

	require.Empty(t, diagnosticMessages(diags))

	assert.Equal(t, "site:L-01: 5 outlines, 6 claims", plan.String())

	report := plan.Report()
	assert.Contains(t, report, "site:R-01 (Meeting Room A): 12.0 m², 3 claims")
	assert.Contains(t, report, "caption on node site:R-01, ring geom:L-01, from")
}

// TestKindSpatial is its own function because it is about the closed set of
// kinds rather than about any model.
func TestKindSpatial(t *testing.T) {
	testCases := []struct {
		name     string
		kind     Kind
		expected bool
	}{
		{name: "a zone groups things which are somewhere and is nowhere", kind: KindZone, expected: false},
		{name: "a site is a place", kind: KindSite, expected: true},
		{name: "a building is a place", kind: KindBuilding, expected: true},
		{name: "a storey is a place", kind: KindStorey, expected: true},
		{name: "a space is a place", kind: KindSpace, expected: true},
		{name: "an element is a place", kind: KindElement, expected: true},
		{name: "an interface is a place", kind: KindInterface, expected: true},
		{name: "a word which is not a kind is not one", kind: Kind("Parcel"), expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, testCase.kind.Spatial())
		})
	}

	assert.Equal(t, len(Kinds())-1, len(SpatialKinds()), "every kind but the zone is a place")
}

// TestPlanOfDrawsAnOpenRun is its own function because what an open run
// contributes to a sheet is a different shape of answer from a room's: it
// covers nothing, and the whole of what a renderer draws it from is the runs of
// its boundary.
func TestPlanOfDrawsAnOpenRun(t *testing.T) {
	plan, diags := storey(t).plan(t, "site:L-01", planArea, planCaption, planLength)

	// The storey holds an open run and is planned anyway. One door does not
	// refuse a floor.
	require.Empty(t, diagnosticMessages(diags), "an open run is not a defect in the storey holding it")

	var railing Outline
	for _, outline := range plan.Outlines() {
		if outline.Subject() == "site:H-01" {
			railing = outline
		}
	}
	require.Equal(t, ID("site:H-01"), railing.Subject())

	t.Run("covers nothing and is drawn from its boundary", func(t *testing.T) {
		assert.True(t, railing.Region().Empty())
		assert.Zero(t, railing.Region().Area())

		segments := railing.Region().Segments()
		require.Len(t, segments, 2)

		for i, expected := range []ID{"geom:E-22", "geom:E-23"} {
			require.NotNil(t, segments[i].Edge())
			assert.Equal(t, expected, segments[i].Edge().ID())
			assert.Equal(t, SegmentOriginEdge, segments[i].Origin())
		}

		assert.Equal(t, Point{4, 3, 0}, segments[0].From())
		assert.Equal(t, Point{6, 5, 0}, segments[1].To())
	})

	t.Run("carries the claims written on it and on the edges of its run", func(t *testing.T) {
		assert.Equal(t, []string{
			`caption "D-01" @ node site:D-01, ring geom:L-21`,
			`wall-length 0.9 m @ edge geom:E-21, geom:V-21 to geom:V-22`,
		}, annotated(plan, "site:D-01"))
	})

	t.Run("draws the rooms it sits among exactly as before", func(t *testing.T) {
		var room Outline
		for _, outline := range plan.Outlines() {
			if outline.Subject() == "site:R-01" {
				room = outline
			}
		}

		assert.InDelta(t, 12.0, room.Region().Area(), 1e-9)
	})
}

// TestDeriveOverAModelHoldingAnOpenRun is its own function because what it
// asserts is an absence which reaches every command that draws an artefact: a
// storey with a door in it derives clean, so the map of the model is written
// rather than refused.
func TestDeriveOverAModelHoldingAnOpenRun(t *testing.T) {
	graph, diags := LoadGraph(planFixtureRoot)
	require.Empty(t, diagnosticMessages(diags), "the fixture loads clean")

	prints, derived := graph.Derive(Derivation{Position: planPosition, Tolerance: planTolerance})

	assert.Empty(t, diagnosticMessages(derived), "one open run does not refuse the whole derivation")

	// The run itself has no footprint, because a footprint is an area and a
	// chain covers none. That is what it has rather than a defect it caused.
	_, held := prints.Of("site:H-01")
	assert.False(t, held)

	room, held := prints.Of("site:R-01")
	require.True(t, held, "the rooms beside it are derived exactly as before")

	area, hasArea := room.Area()
	require.True(t, hasArea)
	assert.InDelta(t, 12.0, area, 1e-9)
}
