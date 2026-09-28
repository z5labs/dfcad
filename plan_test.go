// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"math"
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

// planUnreadableRoot is a second storey, holding one room which reads, one
// whose ring does not close and one whose ring crosses itself.
//
// It is a fixture of its own because every case reading the storey above
// requires that model to be clean, and a defect added to it would be asserted
// against by every one of them rather than by the cases which are about it.
const planUnreadableRoot = "testdata/plan/unreadable"

// planFixture is a fixture loaded, with the survey every ring below is read
// against.
type planFixture struct {
	graph  *Graph
	survey Survey
}

// storey loads the ordinary fixture, failing the test where any pass reports
// anything.
func storey(t *testing.T) planFixture {
	t.Helper()

	return planModel(t, planFixtureRoot)
}

// unreadable loads the fixture whose rings will not read. It loads clean too:
// what is wrong with those rings is a question about corners and a tolerance,
// which nothing answers until a survey asks.
func unreadable(t *testing.T) planFixture {
	t.Helper()

	return planModel(t, planUnreadableRoot)
}

// planModel loads one fixture, failing the test where any pass reports
// anything.
func planModel(t *testing.T, root string) planFixture {
	t.Helper()

	graph, diags := LoadGraph(root)
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

	// And every node placed by a claim on itself, which is [Graph.Located]. A
	// panel has no corners, so a survey built from the corners alone would draw
	// the rooms of the storey and none of the devices standing in them.
	for node := range graph.Nodes().All() {
		located, ok := graph.Located(node)
		if !ok {
			continue
		}

		resolution, err := graph.Claims().Resolve(located.ID(), planPosition, graph.Registry())
		require.NoError(t, err)

		survey.Place(located.ID(), resolution)
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

// undrawnIDs is the id of each node the plan could not draw, which is what a
// case asserting about what was named rather than drawn reads.
func undrawnIDs(plan Plan) []string {
	out := make([]string, 0, len(plan.Undrawn()))
	for _, undrawn := range plan.Undrawn() {
		out = append(out, string(undrawn.Subject()))
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
		out = append(out, spelledAnnotations(outline.Annotations())...)
	}

	return out
}

// undrawnAnnotations is the same for a node the plan could not draw, which
// carries its claims the same way an outline does.
func undrawnAnnotations(plan Plan, node ID) []string {
	var out []string

	for _, undrawn := range plan.Undrawn() {
		if undrawn.Subject() != node {
			continue
		}
		out = append(out, spelledAnnotations(undrawn.Annotations())...)
	}

	return out
}

// spelledAnnotations is a list of reported claims as the two readers above
// spell them.
func spelledAnnotations(annotations []Annotation) []string {
	out := make([]string, 0, len(annotations))
	for _, annotation := range annotations {
		out = append(out, fmt.Sprintf("%s %s @ %s",
			annotation.Predicate(), spelledValue(annotation.Claim().Value()), annotation.Anchor(),
		))
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
			expectedOutlines: []string{"site:A-01", "site:D-01", "site:H-01", "site:P-01", "site:R-01", "site:R-02"},
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
			expectedOutlines: []string{"site:A-01", "site:D-01", "site:H-01", "site:P-01", "site:R-01", "site:R-02"},
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

// TestPlanOfNamesWhatItCouldNotDraw is its own function because it is about the
// second of the two lists a plan comes back with rather than about what one
// outline says.
//
// The circuit group is contained by the storey and carries a caption somebody
// wrote for a sheet. It references no loop at all, so there is nothing of it to
// draw — which is ordinary rather than a defect, and is not a reason to answer as
// though the model did not hold it.
func TestPlanOfNamesWhatItCouldNotDraw(t *testing.T) {
	plan, diags := storey(t).plan(t, "site:L-01", planArea, planCaption)

	require.Empty(t, diagnosticMessages(diags), "a node with no shape is not a defect in the model")

	// It is not an outline, because there is no ring to be one.
	assert.NotContains(t, drawnIDs(plan), "site:C-01")

	require.Equal(t, []string{"site:C-01"}, undrawnIDs(plan))

	undrawn := plan.Undrawn()[0]
	assert.Equal(t, ID("site:C-01"), undrawn.Subject())
	assert.Equal(t, UndrawnNoBoundary, undrawn.Reason())
	require.NotNil(t, undrawn.Node())
	assert.Equal(t, "Level 1 lighting circuit", undrawn.Node().Label())

	// The caption is still returned. It is a fact somebody authored about
	// something inside this storey, and being unable to draw the thing it is
	// written on is not a reason to withhold it.
	assert.Equal(t, []string{`caption "C-01" @ node site:C-01`}, undrawnAnnotations(plan, "site:C-01"))
}

// TestPlanOfAccountsForEveryNodeItContains is its own function because it
// asserts a property of the pair of lists rather than of either one: nothing the
// subject holds is dropped, which is the whole of what a sheet drawn from this
// answer depends on.
func TestPlanOfAccountsForEveryNodeItContains(t *testing.T) {
	fixture := storey(t)

	plan, diags := fixture.plan(t, "site:L-01", planArea, planCaption)
	require.Empty(t, diagnosticMessages(diags))

	node, ok := fixture.graph.Node("site:L-01")
	require.True(t, ok)

	var contained []string
	for related := range fixture.graph.Descendants(node) {
		contained = append(contained, string(related.Node().ID()))
	}

	reported := append(drawnIDs(plan), undrawnIDs(plan)...)

	assert.ElementsMatch(t, contained, reported,
		"every node the storey contains is drawn or is named as not drawn, and none is both")
	assert.Len(t, reported, len(contained))
}

// TestPlanOfNamesARingItCouldNotRead is its own function because it is about a
// storey with defects in it, which is a different model from the one every case
// above reads.
//
// The choice it pins down is the one the whole answer turns on: a node the plan
// cannot draw degrades per node and never refuses the storey, whichever way it
// is undrawable. A ring which does not close and a ring which crosses itself are
// two spellings of one mistake, and behaving differently between them would let
// which of the two a model happens to hold decide how much of the sheet comes
// back.
func TestPlanOfNamesARingItCouldNotRead(t *testing.T) {
	plan, diags := unreadable(t).plan(t, "site:L-01", planCaption)

	// The defects are reported, because they are defects and whoever wrote the
	// file has to hear about them.
	assert.NotEmpty(t, diagnosticMessages(diags))

	t.Run("draws the rooms which read and refuses neither of the others outright", func(t *testing.T) {
		assert.Equal(t, []string{"site:R-01"}, drawnIDs(plan))
		assert.Equal(t, []string{`caption "MR-A" @ node site:R-01, ring geom:L-01`}, annotated(plan, "site:R-01"))
	})

	t.Run("names both unreadable rings the same way", func(t *testing.T) {
		require.Equal(t, []string{"site:P-01", "site:R-02", "site:R-03"}, undrawnIDs(plan))

		expected := map[ID]UndrawnReason{
			"site:R-02": UndrawnUnreadableBoundary,
			"site:R-03": UndrawnUnreadableBoundary,
			// A node whose shape is a position and which nothing places is its
			// own reason. The fix is different — set the device out, rather
			// than mend a ring — and so is who acts on it.
			"site:P-01": UndrawnNoPosition,
		}

		for _, undrawn := range plan.Undrawn() {
			assert.Equal(t, expected[undrawn.Subject()], undrawn.Reason(), undrawn.Subject())
		}
	})

	t.Run("hands back the claims written on them", func(t *testing.T) {
		assert.Equal(t, []string{`caption "MR-B" @ node site:R-02, ring geom:L-11`},
			undrawnAnnotations(plan, "site:R-02"))
		assert.Equal(t, []string{`caption "MR-C" @ node site:R-03, ring geom:L-21`},
			undrawnAnnotations(plan, "site:R-03"))
	})

	t.Run("does not hand back a region covering nothing", func(t *testing.T) {
		// An outline covering nothing is what an open run of edges legitimately
		// comes back as, so a consumer which read one as "not drawn" would leave
		// every doorway and railing off the sheet. A ring which would not read is
		// named as undrawn instead, and the two answers stay distinguishable.
		for _, outline := range plan.Outlines() {
			assert.NotContains(t, []ID{"site:R-02", "site:R-03"}, outline.Subject())
		}
	})
}

// TestPlanOfBudgetsOnlyTheRingsItDrew is its own function because it is about
// what the budget is over rather than about which nodes came back.
func TestPlanOfBudgetsOnlyTheRingsItDrew(t *testing.T) {
	plan, _ := unreadable(t).plan(t, "site:L-01", planCaption)

	require.Len(t, plan.Outlines(), 1)

	// Room A has four corners and nothing else contributed. A ring which was
	// refused put no corner anywhere, and its position claims accumulated into
	// the figure would be an accuracy for geometry nobody is drawing.
	assert.Len(t, plan.Budget().Terms(), 4)
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

	// What was not drawn is ordered by the same rule and carries its claims in
	// the same order, so a consumer diffing two runs diffs the whole answer to
	// nothing rather than most of it.
	assert.Equal(t, undrawnIDs(first), undrawnIDs(second))
	for _, undrawn := range first.Undrawn() {
		assert.Equal(t,
			undrawnAnnotations(first, undrawn.Subject()),
			undrawnAnnotations(second, undrawn.Subject()),
		)
	}

	// The order is by id, which is a property of what the model says rather
	// than of which file each node was written in.
	assert.Equal(t, []string{"site:A-01", "site:D-01", "site:H-01", "site:P-01", "site:R-01", "site:R-02"}, drawnIDs(first))
	assert.Equal(t, []string{"site:C-01"}, undrawnIDs(first))
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
	assert.Empty(t, plan.Undrawn())
	assert.Zero(t, plan.Annotations())
	assert.Equal(t, "nothing was planned", plan.String())
	assert.Equal(t, "nothing was planned", plan.Report())

	var outline Outline
	assert.Equal(t, ID(""), outline.Subject())
	assert.Equal(t, "nothing", outline.String())

	var undrawn Undrawn
	assert.Nil(t, undrawn.Node())
	assert.Equal(t, ID(""), undrawn.Subject())
	assert.Equal(t, UndrawnReason(""), undrawn.Reason())
	assert.Empty(t, undrawn.Annotations())
	assert.Equal(t, "nothing", undrawn.String())
	assert.Equal(t, "it was not drawn", UndrawnReason("").Description())

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

	// The circuit group is the seventh claim and the one node not drawn. The
	// summary says both, because a sheet drawn from this answer is missing
	// something and nothing else on the line would say so.
	assert.Equal(t, "site:L-01: 6 outlines, 8 claims, 1 not drawn", plan.String())

	report := plan.Report()
	assert.Contains(t, report, "site:R-01 (Meeting Room A): 12.0 m², 3 claims")
	assert.Contains(t, report, "caption on node site:R-01, ring geom:L-01, from")
	assert.Contains(t, report, "site:C-01 (Level 1 lighting circuit): not drawn, references no loop, 1 claim")
	assert.Contains(t, report, "caption on node site:C-01, from")
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

// carriedRegistry is the vocabulary of a model written on three frames: the
// site grid it is rooted at, a building grid the survey fitted onto it at a
// translation of (5, 4, 0), and a works grid written in millimetres.
//
// The works grid is there to be a frame a plan cannot carry a room into. The
// tolerance is declared in metres, and nothing converts between the two.
const carriedRegistry = `(project
  (label "Carried plan fixture")
  (globalid-namespace "https://example.org/models/carried"))

(namespace control (description "Survey control the measurements are tied to."))
(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace plan (description "Semantic nodes minted by this model."))
(namespace survey (description "Claim ids issued by the surveyor."))

(predicate position (unit m) (shape coordinate) (dimension 3)
  (description "The location of a vertex in its frame."))
(predicate wall-length (unit m) (shape scalar) (description "How far a run of wall reaches."))
(predicate frame-transform (shape transform) (description "The rigid transform from a frame to its parent."))

(frame frame:site (label "Site survey grid") (unit m))

(frame frame:building
  (label "Building local grid")
  (unit m)
  (parent frame:site)
  (transform survey:C-0001)
  (frame-transform
    (id survey:C-0001)
    (value
      (transform
        (translation 5.0 4.0 0.0)
        (rotation 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0)
        (scale 1.0)))
    (source "Georeferencing report GR-2026-002")
    (method method:gnss-static)
    (accuracy (independent 0.012 m) (systematic 0.005 m control:CP-2))
    (date "2026-02-11")))

(frame frame:works
  (label "Works setting-out grid")
  (unit mm)
  (parent frame:site)
  (transform survey:C-0002)
  (frame-transform
    (id survey:C-0002)
    (value
      (transform
        (translation 0.0 0.0 0.0)
        (rotation 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0)
        (scale 1.0)))
    (source "Setting-out sketch SK-2026-044")
    (method method:gnss-static)
    (accuracy (independent 0.02 m))
    (date "2026-04-30")))

(type Level (kind Storey) (geometry solid) (description "One floor plate."))
(type Room (kind Space) (geometry area) (description "An enclosed room."))
(type Panel (kind Element) (geometry point) (description "A distribution panel."))
(type Fence (kind Element) (geometry line) (description "A run of fencing."))

(tolerance coincident (value 0.005 m)
  (description "How far apart two corners may be and still be one point."))
`

// carriedGeometry is a rectangle, a one-edge run and a second rectangle, every
// one of them written on the building grid.
const carriedGeometry = `(vertex geom:V-11 (frame frame:building)
  (position (value (0.0 0.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m) (systematic 0.003 m control:CP-1)) (date "2026-02-18")))
(vertex geom:V-12 (frame frame:building)
  (position (value (10.0 0.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m) (systematic 0.003 m control:CP-1)) (date "2026-02-18")))
(vertex geom:V-13 (frame frame:building)
  (position (value (10.0 8.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m) (systematic 0.003 m control:CP-1)) (date "2026-02-18")))
(vertex geom:V-14 (frame frame:building)
  (position (value (0.0 8.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m) (systematic 0.003 m control:CP-1)) (date "2026-02-18")))

(edge geom:E-11 (frame frame:building) (vertices geom:V-11 geom:V-12)
  (wall-length (value 10.0 m) (source "Set-out drawing SD-2026-001") (method method:tape)
    (accuracy (independent 0.01 m)) (date "2026-03-01")))
(edge geom:E-12 (frame frame:building) (vertices geom:V-12 geom:V-13))
(edge geom:E-13 (frame frame:building) (vertices geom:V-13 geom:V-14))
(edge geom:E-14 (frame frame:building) (vertices geom:V-14 geom:V-11))

; Traversed against the order its first edge was written, so that a carry which
; lost the direction of a run would be caught.
(loop geom:L-11 (frame frame:building) (edges geom:E-14 geom:E-13 geom:E-12 geom:E-11))
(loop geom:L-12 (frame frame:building) (edges geom:E-11))

(vertex geom:V-21 (frame frame:building)
  (position (value (0.0 20.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-22 (frame frame:building)
  (position (value (3.0 20.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-23 (frame frame:building)
  (position (value (3.0 23.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-21 (frame frame:building) (vertices geom:V-21 geom:V-22))
(edge geom:E-22 (frame frame:building) (vertices geom:V-22 geom:V-23))
(edge geom:E-23 (frame frame:building) (vertices geom:V-23 geom:V-21))

(loop geom:L-21 (frame frame:building) (edges geom:E-21 geom:E-22 geom:E-23))
`

// carriedEntities is two subjects. The first is on the site grid and holds a
// room, a panel and a fence written on the building grid; the second is on the
// works grid, in millimetres, and holds a room written on the building grid in
// metres.
const carriedEntities = `(node plan:P-01
  (label "Ground floor")
  (kind Storey)
  (type Level)
  (geometry solid)
  (frame frame:site))

(node plan:S-01
  (label "Block A")
  (kind Space)
  (type Room)
  (geometry area)
  (frame frame:building)
  (within plan:P-01)
  (boundary geom:L-11))

(node plan:M-01
  (label "Meter panel")
  (kind Element)
  (type Panel)
  (geometry point)
  (frame frame:building)
  (within plan:P-01)
  (position
    (value (1.0 1.0 0.0) m)
    (source "Services set-out SS-2026-007")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-02")))

(node plan:F-01
  (label "Boundary fence")
  (kind Element)
  (type Fence)
  (geometry line)
  (frame frame:building)
  (within plan:P-01)
  (boundary geom:L-12))

(node plan:P-02
  (label "Works level")
  (kind Storey)
  (type Level)
  (geometry solid)
  (frame frame:works))

(node plan:S-02
  (label "Site office")
  (kind Space)
  (type Room)
  (geometry area)
  (frame frame:building)
  (within plan:P-02)
  (boundary geom:L-21))
`

// carried loads the fixture above.
func carried(t *testing.T) planFixture {
	t.Helper()

	return planModel(t, tree(t, map[string]string{
		"registry.dfc":          carriedRegistry,
		"entities/model.dfc":    carriedEntities,
		"entities/geometry.dfc": carriedGeometry,
	}))
}

// outlineOf is the outline of one node of a plan, failing the test where the
// plan did not draw it.
func outlineOf(t *testing.T, plan Plan, node ID) Outline {
	t.Helper()

	for _, outline := range plan.Outlines() {
		if outline.Subject() == node {
			return outline
		}
	}

	require.Failf(t, "the plan draws the node", "%s is not among %v", node, drawnIDs(plan))
	return Outline{}
}

func TestPlanOfCarriesEveryOutlineIntoItsFrame(t *testing.T) {
	fixture := carried(t)

	plan, diags := fixture.plan(t, "plan:P-01", "wall-length", "position")
	require.Empty(t, diagnosticMessages(diags), "every node the level holds can be carried onto the site grid")

	assert.Equal(t, ID("frame:site"), plan.Frame())
	assert.Equal(t, Unit("m"), plan.Unit())
	assert.Equal(t, []string{"plan:F-01", "plan:M-01", "plan:S-01"}, drawnIDs(plan))

	frames := fixture.graph.Frames()

	// on is where the model puts a corner of the building grid on the site
	// grid, which is the answer every carried coordinate has to agree with.
	on := func(t *testing.T, point Point) Point {
		t.Helper()

		at, err := frames.TransformPoint(point, "frame:building", "frame:site")
		require.NoError(t, err)
		return at
	}

	// authored is where each vertex of the fixture was written, on the
	// building grid.
	authored := func(t *testing.T, vertex ID) Point {
		t.Helper()

		resolution, err := fixture.graph.Claims().Resolve(vertex, planPosition, fixture.graph.Registry())
		require.NoError(t, err)

		value, ok := resolution.Value()
		require.True(t, ok)

		components, ok := value.Coordinate()
		require.True(t, ok)

		var point Point
		copy(point[:], components)
		return point
	}

	testCases := []struct {
		name string
		node ID
	}{
		{name: "carries a room's rings and the runs of its boundary", node: "plan:S-01"},
		{name: "carries an open run", node: "plan:F-01"},
		{name: "carries a point", node: "plan:M-01"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			outline := outlineOf(t, plan, testCase.node)
			region := outline.Region()

			assert.Equal(t, ID("frame:site"), region.Frame())
			assert.Equal(t, ID("frame:building"), outline.DeclaredIn())

			for _, piece := range region.Pieces() {
				for _, corner := range piece.Outer() {
					// A carried corner is one of the authored corners, carried.
					var matched bool
					for _, vertex := range []ID{"geom:V-11", "geom:V-12", "geom:V-13", "geom:V-14"} {
						if pointsNear(corner, on(t, authored(t, vertex))) {
							matched = true
						}
					}
					assert.True(t, matched, "%v is the carry of an authored corner", corner)
				}
			}

			for _, segment := range region.Segments() {
				require.NotNil(t, segment.Edge(), "a carried run keeps the edge it was written as")
				assert.Equal(t, SegmentOriginEdge, segment.Origin())

				start, end := segment.Edge().Vertices()
				if segment.Reversed() {
					start, end = end, start
				}

				assert.True(t, pointsNear(on(t, authored(t, start)), segment.From()),
					"%s leaves %v, found %v", segment.Edge().ID(), on(t, authored(t, start)), segment.From())
				assert.True(t, pointsNear(on(t, authored(t, end)), segment.To()),
					"%s arrives at %v, found %v", segment.Edge().ID(), on(t, authored(t, end)), segment.To())
			}

			if at, located := region.Location(); located {
				assert.True(t, pointsNear(on(t, Point{1, 1, 0}), at), "the panel is at %v", at)
			}
		})
	}

	t.Run("keeps every run's edge, ring and direction as it was read", func(t *testing.T) {
		for _, node := range []ID{"plan:S-01", "plan:F-01"} {
			contained, ok := fixture.graph.Node(node)
			require.True(t, ok)

			read, found := fixture.graph.Topology().RegionOf(contained, fixture.graph.Boundaries(), fixture.survey)
			require.Empty(t, diagnosticMessages(found))

			authoredRuns := read.Segments()
			carriedRuns := outlineOf(t, plan, node).Region().Segments()
			require.Len(t, carriedRuns, len(authoredRuns))

			for i := range authoredRuns {
				assert.Equal(t, authoredRuns[i].Edge().ID(), carriedRuns[i].Edge().ID())
				assert.Equal(t, authoredRuns[i].Reversed(), carriedRuns[i].Reversed())
				assert.Equal(t, authoredRuns[i].Ring(), carriedRuns[i].Ring())
				assert.Equal(t, authoredRuns[i].Origin(), carriedRuns[i].Origin())
			}
		}

		// The room is traversed against the order geom:E-11 was written, which
		// is the case a carry that re-derived the direction would get wrong.
		var reversed bool
		for _, segment := range outlineOf(t, plan, "plan:S-01").Region().Segments() {
			if segment.Edge().ID() == "geom:E-11" {
				reversed = segment.Reversed()
			}
		}
		assert.True(t, reversed)
	})

	t.Run("carries the area without changing it", func(t *testing.T) {
		assert.InDelta(t, 80.0, outlineOf(t, plan, "plan:S-01").Region().Area(), 1e-9)
	})

	t.Run("reports the claims as they were written, in the frame they were written in", func(t *testing.T) {
		panel := outlineOf(t, plan, "plan:M-01")

		annotations := panel.Annotations()
		require.Len(t, annotations, 1)

		components, ok := annotations[0].Claim().Value().Coordinate()
		require.True(t, ok)
		assert.Equal(t, []float64{1, 1, 0}, components)
		assert.Equal(t, ID("frame:building"), panel.DeclaredIn())
	})

	t.Run("budgets the transform it carried through", func(t *testing.T) {
		for _, term := range []string{"survey:C-0001", "control:CP-2", "control:CP-1"} {
			_, found := termNamed(plan.Budget(), term)
			assert.True(t, found, "the budget holds %s", term)
		}
	})
}

func TestPlanOfNamesANodeItCannotCarry(t *testing.T) {
	plan, diags := carried(t).plan(t, "plan:P-02", "wall-length")

	assert.Equal(t, ID("frame:works"), plan.Frame())
	assert.Equal(t, Unit("mm"), plan.Unit())
	assert.Empty(t, drawnIDs(plan))

	require.Len(t, plan.Undrawn(), 1)
	undrawn := plan.Undrawn()[0]

	assert.Equal(t, ID("plan:S-02"), undrawn.Subject())
	assert.Equal(t, UndrawnUncarried, undrawn.Reason())
	assert.Equal(t, "could not be carried into the plan's frame", undrawn.Reason().Description())
	assert.Equal(t, ID("frame:building"), undrawn.DeclaredIn())

	require.NotEmpty(t, diags)

	var failures int
	for _, diag := range diags {
		if diag.Severity == SeverityError {
			failures++
		}
	}
	assert.Equal(t, 1, failures, "one error, saying why the room could not be carried")

	// The refusal is the unit one, and it points at the tolerance whose unit the
	// plan's frame is not in.
	var pointed bool
	for _, diag := range diags {
		for _, related := range diag.Related {
			if related.Span == plan.Tolerance().Span {
				pointed = true
			}
		}
	}
	assert.True(t, pointed, "the diagnostic points at the tolerance declared in another unit")
	assert.True(t, refused(diags), "a node which could not be carried refuses the run as an unreadable one does")

	assert.Empty(t, plan.Budget().Terms(), "nothing was drawn, so nothing is budgeted")
}

// pointsNear is whether two points are one point, to well inside anything a
// transform's arithmetic could round by.
func pointsNear(a, b Point) bool {
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-9 {
			return false
		}
	}
	return true
}

// selected reads the plan of one node of the fixture under the predicates
// named, asking about only what the selection selects.
func (f planFixture) selected(t *testing.T, id ID, selection Selection, predicates ...string) (Plan, []Diagnostic) {
	t.Helper()

	node, ok := f.graph.Node(id)
	require.True(t, ok, "the fixture holds a node %s", id)

	return f.graph.PlanOfSelected(node, f.survey, Annotations{Predicates: predicates}, selection)
}

func TestPlanOfSelected(t *testing.T) {
	testCases := []struct {
		name             string
		subject          ID
		selection        Selection
		expectedOutlines []string
		expectedUndrawn  []string
	}{
		{
			name:             "draws every descendant for a selection which narrows nothing",
			subject:          "site:L-01",
			selection:        Selection{},
			expectedOutlines: []string{"site:A-01", "site:D-01", "site:H-01", "site:P-01", "site:R-01", "site:R-02"},
			expectedUndrawn:  []string{"site:C-01"},
		},
		{
			name:             "draws only the type selected",
			subject:          "site:L-01",
			selection:        Selection{Types: []string{"MeetingRoom"}},
			expectedOutlines: []string{"site:R-01", "site:R-02"},
			expectedUndrawn:  []string{},
		},
		{
			name:             "draws only the kind selected, and names what of it could not be drawn",
			subject:          "site:L-01",
			selection:        Selection{Kinds: []Kind{"Element"}},
			expectedOutlines: []string{"site:D-01", "site:H-01", "site:P-01"},
			expectedUndrawn:  []string{"site:C-01"},
		},
		{
			name:             "selects what satisfies both fields",
			subject:          "site:L-01",
			selection:        Selection{Kinds: []Kind{"Space"}, Types: []string{"Alcove", "Doorway"}},
			expectedOutlines: []string{"site:A-01"},
			expectedUndrawn:  []string{},
		},
		{
			name:             "selects any of one field's values",
			subject:          "site:L-01",
			selection:        Selection{Types: []string{"Alcove", "Doorway"}},
			expectedOutlines: []string{"site:A-01", "site:D-01"},
			expectedUndrawn:  []string{},
		},
		{
			// The alcove is inside a room inside a storey inside the building,
			// and none of those is an alcove: the selection narrows what is
			// reported and never what is walked.
			name:             "walks through what it does not select to reach what it does",
			subject:          "site:B-01",
			selection:        Selection{Types: []string{"Alcove"}},
			expectedOutlines: []string{"site:A-01"},
			expectedUndrawn:  []string{},
		},
		{
			name:             "draws nothing, and refuses nothing, for a selection nothing satisfies",
			subject:          "site:L-01",
			selection:        Selection{Types: []string{"OfficeStorey"}},
			expectedOutlines: []string{},
			expectedUndrawn:  []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			plan, diags := storey(t).selected(t, testCase.subject, testCase.selection, planArea)

			assert.Empty(t, diagnosticMessages(diags))
			assert.Equal(t, testCase.expectedOutlines, drawnIDs(plan))
			assert.Equal(t, testCase.expectedUndrawn, undrawnIDs(plan))
		})
	}
}

// TestPlanOfIsPlanOfSelectedWithNoSelection is its own function because it
// asserts the relation between the two entry points rather than an answer:
// PlanOf is what it always was, and asks with no selection.
func TestPlanOfIsPlanOfSelectedWithNoSelection(t *testing.T) {
	fixture := storey(t)

	whole, wholeDiags := fixture.plan(t, "site:L-01", planArea, planCaption, planLength)
	unselected, unselectedDiags := fixture.selected(t, "site:L-01", Selection{}, planArea, planCaption, planLength)

	assert.Equal(t, whole, unselected)
	assert.Equal(t, wholeDiags, unselectedDiags)
}

// TestPlanOfSelectedDrawsANodeAsTheWholePlanDoes is the property a selection
// rests on: it changes which rooms come back and never how a room is drawn.
// Each outline of a selected plan is, entry for entry, the same node's outline
// in the unselected one, and so is each undrawn entry.
func TestPlanOfSelectedDrawsANodeAsTheWholePlanDoes(t *testing.T) {
	fixture := storey(t)

	whole, _ := fixture.plan(t, "site:L-01", planArea, planCaption, planLength)

	outlines := make(map[ID]Outline, len(whole.Outlines()))
	for _, outline := range whole.Outlines() {
		outlines[outline.Subject()] = outline
	}

	undrawn := make(map[ID]Undrawn, len(whole.Undrawn()))
	for _, entry := range whole.Undrawn() {
		undrawn[entry.Subject()] = entry
	}

	selections := []Selection{
		{Types: []string{"MeetingRoom"}},
		{Types: []string{"Alcove", "Doorway"}},
		{Kinds: []Kind{"Element"}},
		{Kinds: []Kind{"Space", "Element"}},
		{Kinds: []Kind{"Space"}, Types: []string{"Alcove"}},
	}

	for _, selection := range selections {
		t.Run(fmt.Sprintf("%v %v", selection.Kinds, selection.Types), func(t *testing.T) {
			plan, _ := fixture.selected(t, "site:L-01", selection, planArea, planCaption, planLength)

			require.NotEmpty(t, plan.Outlines())
			for _, outline := range plan.Outlines() {
				expected, ok := outlines[outline.Subject()]
				require.True(t, ok, "%s is drawn in the unselected plan too", outline.Subject())

				assert.Equal(t, expected.Region(), outline.Region(), outline.Subject())
				assert.Equal(t, expected.Annotations(), outline.Annotations(), outline.Subject())
			}

			for _, entry := range plan.Undrawn() {
				expected, ok := undrawn[entry.Subject()]
				require.True(t, ok, "%s is undrawn in the unselected plan too", entry.Subject())

				assert.Equal(t, expected, entry, entry.Subject())
			}
		})
	}
}

// TestPlanOfSelectedBudgetsOnlyTheRingsItSelected is its own function because
// it is about what the budget is over rather than about which nodes came back.
func TestPlanOfSelectedBudgetsOnlyTheRingsItSelected(t *testing.T) {
	fixture := storey(t)

	whole, _ := fixture.plan(t, "site:L-01", planArea)
	rooms, _ := fixture.selected(t, "site:L-01", Selection{Types: []string{"MeetingRoom"}}, planArea)

	// The budget of the meeting rooms is the budget of their rings, merged,
	// and nothing else: the alcove, the doorway and the panel are drawn on the
	// whole storey's sheet and not on this one.
	var expected Budget
	for _, outline := range rooms.Outlines() {
		expected.Merge(outline.Region().Budget())
	}

	assert.Equal(t, expected.Terms(), rooms.Budget().Terms())
	assert.Less(t, len(rooms.Budget().Terms()), len(whole.Budget().Terms()))
}

func TestSelectionSelects(t *testing.T) {
	fixture := storey(t)

	room, ok := fixture.graph.Node("site:R-01")
	require.True(t, ok)

	testCases := []struct {
		name      string
		selection Selection
		node      *SemanticNode
		expected  bool
	}{
		{name: "selects any node when it narrows nothing", selection: Selection{}, node: room, expected: true},
		{name: "selects a node of a kind it names", selection: Selection{Kinds: []Kind{"Element", "Space"}}, node: room, expected: true},
		{name: "refuses a node of a kind it does not name", selection: Selection{Kinds: []Kind{"Element"}}, node: room, expected: false},
		{name: "selects a node of a type it names", selection: Selection{Types: []string{"MeetingRoom"}}, node: room, expected: true},
		{name: "refuses a node of a type it does not name", selection: Selection{Types: []string{"Alcove"}}, node: room, expected: false},
		{
			name:      "refuses a node satisfying one field and not the other",
			selection: Selection{Kinds: []Kind{"Space"}, Types: []string{"Alcove"}},
			node:      room,
			expected:  false,
		},
		{name: "never selects a nil node", selection: Selection{}, node: nil, expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, testCase.selection.Selects(testCase.node))
		})
	}
}
