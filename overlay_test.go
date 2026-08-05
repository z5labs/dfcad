// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// overlaidModel is one fixture loaded and ready to be overlaid: the vocabulary
// it declares, both families joined, the frames it relates and the survey every
// region is read against.
type overlaidModel struct {
	registry   *Registry
	nodes      *Nodes
	topology   *Topology
	boundaries *Boundaries
	frames     *Frames
	survey     Survey
}

// loadOverlaidModel loads one fixture, failing the test where any pass beneath
// the one under test reports anything.
//
// Every fixture here loads clean, including the one holding shapes which are not
// shapes: a ring which crosses itself is a well-formed loop and is refused by
// the overlay rather than by the loader, which is the distinction the golden
// beside that fixture is there to hold on to.
func loadOverlaidModel(t *testing.T, name string) overlaidModel {
	t.Helper()

	root := filepath.Join("testdata", "overlay", name)

	registry, registryDiags := LoadRegistry(root)
	require.Empty(t, registryDiags, "the fixture registry loads clean")

	nodes, nodeDiags := LoadNodes(root, registry)
	require.Empty(t, nodeDiags, "the fixture's semantic family loads clean")

	topology, topologyDiags := LoadTopology(root, registry)
	require.Empty(t, topologyDiags, "the fixture's geometric family loads clean")

	claims, claimDiags := LoadClaims(root, registry)
	require.Empty(t, claimDiags, "the fixture's claims load clean")

	boundaries, boundaryDiags := ResolveBoundaries(nodes, topology)
	require.Empty(t, renderBoundaryDiagnostics(t, boundaryDiags), "the fixture's two families join clean")

	frames, frameDiags := ResolveFrames(registry, claims)
	require.Empty(t, renderBoundaryDiagnostics(t, frameDiags), "the fixture's frames resolve clean")

	survey := Survey{Tolerance: closureTolerance, Registry: registry}
	for vertex := range topology.Vertices() {
		resolution, err := claims.Resolve(vertex.ID(), "position", registry)
		require.NoError(t, err)

		survey.Place(vertex.ID(), resolution)
	}

	return overlaidModel{
		registry:   registry,
		nodes:      nodes,
		topology:   topology,
		boundaries: boundaries,
		frames:     frames,
		survey:     survey,
	}
}

// region reads one region of a fixture, failing the test where the fixture holds
// no node of that id or where reading it reported anything.
func (m overlaidModel) region(t *testing.T, id ID) Region {
	t.Helper()

	node, ok := m.nodes.Node(id)
	require.True(t, ok, "the fixture holds a node %s", id)

	region, diags := m.topology.RegionOf(node, m.boundaries, m.survey)
	require.Empty(t, renderBoundaryDiagnostics(t, diags), "%s is a region which can be operated on", id)

	return region
}

// holes counts the rings taken out of every piece of a region.
func holes(region Region) int {
	var count int
	for _, piece := range region.Pieces() {
		count += len(piece.Holes())
	}
	return count
}

func TestRegionOf(t *testing.T) {
	testCases := []struct {
		name   string
		region ID
		area   float64
		pieces int
		holes  int
	}{
		{
			name:   "reads a rectangle back out of its four corners",
			region: "site:S-01",
			area:   12,
			pieces: 1,
		},
		{
			name:   "reads a courtyard as a hole rather than as a second piece",
			region: "site:S-04",
			area:   84,
			pieces: 1,
			holes:  1,
		},
		{
			name:   "reads a region far from the origin without losing its size",
			region: "site:S-08",
			area:   12,
			pieces: 1,
		},
	}

	model := loadOverlaidModel(t, "shapes")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			region := model.region(t, testCase.region)

			assert.Equal(t, testCase.area, region.Area())
			assert.Len(t, region.Pieces(), testCase.pieces)
			assert.Equal(t, testCase.holes, holes(region))
			assert.Equal(t, Unit("m"), region.Unit())
			assert.Equal(t, ID("frame:building"), region.Frame())
		})
	}
}

func TestRegionOverlay(t *testing.T) {
	testCases := []struct {
		name     string
		subject  ID
		clip     ID
		overlay  func(a, b Region) (Region, []Diagnostic)
		area     float64
		pieces   int
		holes    int
		expected Containment
	}{
		{
			name:     "unions two overlapping rectangles into one piece",
			subject:  "site:S-01",
			clip:     "site:S-02",
			overlay:  Region.Union,
			area:     24,
			pieces:   1,
			expected: ContainmentOverlapping,
		},
		{
			name:    "intersects two overlapping rectangles in the square they share",
			subject: "site:S-01",
			clip:    "site:S-02",
			overlay: Region.Intersect,
			area:    4,
			pieces:  1,
		},
		{
			name:    "takes one overlapping rectangle out of the other",
			subject: "site:S-01",
			clip:    "site:S-02",
			overlay: Region.Difference,
			area:    8,
			pieces:  1,
		},
		{
			name:     "unions two rectangles sharing a wall into one piece",
			subject:  "site:S-01",
			clip:     "site:S-03",
			overlay:  Region.Union,
			area:     18,
			pieces:   1,
			expected: ContainmentTouching,
		},
		{
			name:    "intersects two rectangles sharing a wall in nothing",
			subject: "site:S-01",
			clip:    "site:S-03",
			overlay: Region.Intersect,
			area:    0,
		},
		{
			name:    "leaves a rectangle sharing a wall whole when the other is taken out of it",
			subject: "site:S-01",
			clip:    "site:S-03",
			overlay: Region.Difference,
			area:    12,
			pieces:  1,
		},
		{
			name:     "takes a duct out of the plate it is inside, leaving a second hole",
			subject:  "site:S-04",
			clip:     "site:S-06",
			overlay:  Region.Difference,
			area:     82,
			pieces:   1,
			holes:    2,
			expected: ContainmentInside,
		},
		{
			name:     "intersects a plate with the garden filling its courtyard in nothing",
			subject:  "site:S-04",
			clip:     "site:S-05",
			overlay:  Region.Intersect,
			area:     0,
			expected: ContainmentTouching,
		},
		{
			name:     "unions a plate with the garden filling its courtyard into a solid plate",
			subject:  "site:S-04",
			clip:     "site:S-05",
			overlay:  Region.Union,
			area:     100,
			pieces:   1,
			expected: ContainmentTouching,
		},
		{
			name:     "intersects two rectangles which are nowhere near each other in nothing",
			subject:  "site:S-01",
			clip:     "site:S-07",
			overlay:  Region.Intersect,
			area:     0,
			expected: ContainmentDisjoint,
		},
		{
			name:    "overlays two rectangles far from the origin as exactly as it does two at it",
			subject: "site:S-08",
			clip:    "site:S-09",
			overlay: Region.Intersect,
			area:    4,
			pieces:  1,
		},
	}

	model := loadOverlaidModel(t, "shapes")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			subject, clip := model.region(t, testCase.subject), model.region(t, testCase.clip)

			result, diags := testCase.overlay(subject, clip)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			assert.Equal(t, testCase.area, result.Area())
			assert.Len(t, result.Pieces(), testCase.pieces)
			assert.Equal(t, testCase.holes, holes(result))
			assert.Equal(t, testCase.pieces == 0, result.Empty())

			if testCase.expected == "" {
				return
			}

			containment, diags := subject.Containment(clip)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))
			assert.Equal(t, testCase.expected, containment)
		})
	}
}

// TestRegionDifferenceLeavesMoreThanOnePiece is its own function because it is a
// different shape of answer rather than another row of one: an operation which
// cuts a region in two has to come back as two pieces which do not touch, and a
// result type which could only hold one would have had to pick a half.
func TestRegionDifferenceLeavesMoreThanOnePiece(t *testing.T) {
	model := loadOverlaidModel(t, "shapes")

	plate := model.region(t, "site:S-04")
	corridor := model.region(t, "site:S-12")

	// The corridor runs out of the plate at both ends, so what is left of the
	// plate is a piece north of it and a piece south of it, each with its half
	// of the courtyard taken off the edge rather than out of the middle.
	result, diags := plate.Difference(corridor)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	require.Len(t, result.Pieces(), 2)
	assert.Equal(t, 0, holes(result))
	assert.Equal(t, 72.0, result.Area())

	for _, piece := range result.Pieces() {
		assert.Equal(t, 36.0, piece.Area())
	}
}

func TestRegionBuffer(t *testing.T) {
	testCases := []struct {
		name     string
		region   ID
		distance float64
		area     float64
		pieces   int
		holes    int
	}{
		{
			name:     "grows a rectangle by the offset all the way round it, with rounded corners",
			region:   "site:S-01",
			distance: 1,
			area:     12 + 14 + math.Pi,
			pieces:   1,
		},
		{
			name:     "shrinks a rectangle to the part of it clear of every wall",
			region:   "site:S-01",
			distance: -1,
			area:     2,
			pieces:   1,
		},
		{
			name:     "grows a plate outwards and its courtyard inwards at the same time",
			region:   "site:S-04",
			distance: 1,
			area:     100 + 40 + math.Pi - 4,
			pieces:   1,
			holes:    1,
		},
		{
			name:     "shrinks a plate inwards and its courtyard outwards at the same time",
			region:   "site:S-04",
			distance: -1,
			area:     64 - (16 + 16 + math.Pi),
			pieces:   1,
			holes:    1,
		},
		{
			name:     "collapses a corridor narrower than twice the offset to nothing",
			region:   "site:S-07",
			distance: -0.3,
			area:     0,
		},
		{
			name:     "leaves a region alone when it is offset by nothing",
			region:   "site:S-01",
			distance: 0,
			area:     12,
			pieces:   1,
		},
	}

	model := loadOverlaidModel(t, "shapes")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			region := model.region(t, testCase.region)

			result, diags := region.Buffer(testCase.distance)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			// A rounded corner is drawn to the declared tolerance, so an area
			// which includes one is right to within about that tolerance times
			// the length of the arc rather than exactly.
			assert.InDelta(t, testCase.area, result.Area(), region.Tolerance().Value*result.Area())
			assert.Len(t, result.Pieces(), testCase.pieces)
			assert.Equal(t, testCase.holes, holes(result))
		})
	}
}

// TestRegionBufferBelowTheToleranceIsRefused is its own function because the
// answer is a diagnostic rather than a region: an offset the project has said it
// cannot tell from zero has no shape to come back.
func TestRegionBufferBelowTheToleranceIsRefused(t *testing.T) {
	model := loadOverlaidModel(t, "shapes")

	region := model.region(t, "site:S-01")

	result, diags := region.Buffer(region.Tolerance().Value / 2)

	assert.True(t, result.Empty())
	require.Len(t, diags, 1)
	assert.Equal(t, SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, "boundary-closure")
}

func TestRegionContainment(t *testing.T) {
	testCases := []struct {
		name     string
		subject  ID
		other    ID
		expected Containment
	}{
		{
			name:     "reports a region inside another as inside it",
			subject:  "site:S-04",
			other:    "site:S-06",
			expected: ContainmentInside,
		},
		{
			name:     "reports the same pair the other way round as surrounded",
			subject:  "site:S-06",
			other:    "site:S-04",
			expected: ContainmentSurrounds,
		},
		{
			name:     "reports two regions covering the same area as coincident",
			subject:  "site:S-05",
			other:    "site:S-05",
			expected: ContainmentCoincident,
		},
		{
			name:     "reports a region straddling another's boundary as overlapping",
			subject:  "site:S-01",
			other:    "site:S-02",
			expected: ContainmentOverlapping,
		},
		{
			name:     "reports two regions sharing a wall as touching rather than overlapping",
			subject:  "site:S-01",
			other:    "site:S-03",
			expected: ContainmentTouching,
		},
		{
			name:     "reports a garden filling a courtyard as touching the plate around it",
			subject:  "site:S-04",
			other:    "site:S-05",
			expected: ContainmentTouching,
		},
		{
			name:     "reports two regions with neither area nor boundary in common as disjoint",
			subject:  "site:S-01",
			other:    "site:S-07",
			expected: ContainmentDisjoint,
		},
	}

	model := loadOverlaidModel(t, "shapes")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			subject, other := model.region(t, testCase.subject), model.region(t, testCase.other)

			containment, diags := subject.Containment(other)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			assert.Equal(t, testCase.expected, containment)
		})
	}
}

// TestRegionContainmentOfARegionTouchingFromTheInside is its own function
// because it is the case the definition of touching has to be checked against:
// a region inside another and reaching its wall is inside it, and only reaching
// across the wall makes it an overlap.
func TestRegionContainmentOfARegionTouchingFromTheInside(t *testing.T) {
	model := loadOverlaidModel(t, "shapes")

	room := model.region(t, "site:S-01")

	inside, diags := room.Buffer(-1)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	against, diags := room.Difference(inside)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	containment, diags := room.Containment(against)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))
	assert.Equal(t, ContainmentInside, containment)
}

// TestRegionOverlayIsIndependentOfOperandOrder checks the property the
// commutative operations are supposed to have, rather than that their areas
// agree: two results which are the same shape have to be the same value, with
// their pieces in the same order and their rings written from the same corner.
func TestRegionOverlayIsIndependentOfOperandOrder(t *testing.T) {
	testCases := []struct {
		name    string
		overlay func(a, b Region) (Region, []Diagnostic)
	}{
		{name: "unions the same way round either way round", overlay: Region.Union},
		{name: "intersects the same way round either way round", overlay: Region.Intersect},
	}

	model := loadOverlaidModel(t, "shapes")

	subject, clip := model.region(t, "site:S-01"), model.region(t, "site:S-02")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			one, diags := testCase.overlay(subject, clip)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			other, diags := testCase.overlay(clip, subject)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			assert.Equal(t, one.Pieces(), other.Pieces())
			assert.Equal(t, one.Area(), other.Area())
		})
	}
}

// TestRegionOverlayIsDeterministic runs the same overlay repeatedly, because
// nothing about an answer may depend on the order a map was walked in or on
// which ring a traversal happened to start from.
func TestRegionOverlayIsDeterministic(t *testing.T) {
	model := loadOverlaidModel(t, "shapes")

	plate, garden := model.region(t, "site:S-04"), model.region(t, "site:S-05")

	first, diags := plate.Union(garden)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	for range 8 {
		again, diags := plate.Union(garden)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		assert.Equal(t, first.Pieces(), again.Pieces())
	}

	offset, diags := plate.Buffer(1)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	for range 8 {
		again, diags := plate.Buffer(1)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		assert.Equal(t, offset.Pieces(), again.Pieces())
	}
}

// TestRegionAccuracyPropagates checks that an answer knows what it rests on:
// every claim behind either operand is in the result's budget, and a term the
// two share is in it once.
func TestRegionAccuracyPropagates(t *testing.T) {
	model := loadOverlaidModel(t, "shapes")

	subject, clip := model.region(t, "site:S-01"), model.region(t, "site:S-02")

	require.True(t, subject.Budget().Known(), "every corner of the fixture states an accuracy")

	for _, region := range []Region{subject, clip} {
		combined, err := region.Budget().Combined()
		require.NoError(t, err)
		assert.Greater(t, combined.Standard(), 0.0)
	}

	shared, diags := subject.Intersect(clip)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	assert.True(t, shared.Budget().Known())

	// The control point behind every corner of both rooms is one systematic
	// term, and an intersection of the two counts it once rather than twice.
	terms := shared.Budget().Terms()
	require.NotEmpty(t, terms)

	var systematic int
	for _, term := range terms {
		if term.Shared() {
			systematic++
		}
	}
	assert.Equal(t, 1, systematic)

	offset, diags := subject.Buffer(1)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))
	assert.Equal(t, subject.Budget().Terms(), offset.Budget().Terms(), "an offset rests on what it was offset from")
}

// TestRegionsInDifferentFramesAreRefused is the assertion that nothing here
// combines two frames' numbers because they are both numbers.
func TestRegionsInDifferentFramesAreRefused(t *testing.T) {
	testCases := []struct {
		name    string
		overlay func(a, b Region) (Region, []Diagnostic)
	}{
		{name: "refuses to union them", overlay: Region.Union},
		{name: "refuses to intersect them", overlay: Region.Intersect},
		{name: "refuses to take one from the other", overlay: Region.Difference},
	}

	model := loadOverlaidModel(t, "shapes")

	room, annex := model.region(t, "site:S-01"), model.region(t, "site:S-11")
	require.Equal(t, ID("frame:annex"), annex.Frame())

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, diags := testCase.overlay(room, annex)

			assert.True(t, result.Empty())
			require.Len(t, diags, 1)
			assert.Equal(t, SeverityError, diags[0].Severity)
			assert.Contains(t, diags[0].Message, "frame:annex")
		})
	}

	t.Run("refuses to say how they contain each other", func(t *testing.T) {
		containment, diags := room.Containment(annex)

		assert.Equal(t, Containment(""), containment)
		require.Len(t, diags, 1)
		assert.Equal(t, SeverityError, diags[0].Severity)
	})
}

// TestRegionInAnotherFrame checks the explicit way across: the numbers of the
// annex room are the meeting room's, and carrying it into the building frame is
// what puts it thirty metres away from it rather than on top of it.
func TestRegionInAnotherFrame(t *testing.T) {
	model := loadOverlaidModel(t, "shapes")

	room, annex := model.region(t, "site:S-01"), model.region(t, "site:S-11")

	carried, diags := annex.In("frame:building", model.frames)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	assert.Equal(t, ID("frame:building"), carried.Frame())
	assert.Equal(t, Unit("m"), carried.Unit())
	assert.InDelta(t, annex.Area(), carried.Area(), 1e-9, "a rigid transform does not change an area")

	bounds, ok := carried.Bounds()
	require.True(t, ok)
	assert.InDelta(t, 30, bounds.Min[0], 1e-9)

	containment, diags := room.Containment(carried)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))
	assert.Equal(t, ContainmentDisjoint, containment)

	// The transform between the two frames is a measurement, and what it cost
	// is in the budget of everything computed after it.
	assert.Greater(t, len(carried.Budget().Terms()), len(annex.Budget().Terms()))
}

// TestRegionsInDifferentPlanesAreRefused is the case which makes an overlay a
// question about a plane figure rather than about a plan drawing: a room on the
// storey above is inside this one seen from above and is not inside it.
func TestRegionsInDifferentPlanesAreRefused(t *testing.T) {
	model := loadOverlaidModel(t, "shapes")

	room, above := model.region(t, "site:S-01"), model.region(t, "site:S-10")

	result, diags := room.Intersect(above)

	assert.True(t, result.Empty())
	require.Len(t, diags, 1)
	assert.Equal(t, SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, "one plane")
}

// TestRegionOfUnusableGeometry checks that what an overlay refuses, it refuses
// with a diagnostic rather than with a plausible number.
func TestRegionOfUnusableGeometry(t *testing.T) {
	model := loadOverlaidModel(t, "unusable")

	var diags []Diagnostic
	for node := range model.nodes.All() {
		region, found := model.topology.RegionOf(node, model.boundaries, model.survey)

		assert.True(t, region.Empty(), "%s is not a region anything can be computed over", node.ID())
		assert.NotEmpty(t, found, "%s comes back with a reason rather than with nothing", node.ID())

		offset, refused := region.Buffer(1)
		assert.True(t, offset.Empty())
		assert.NotEmpty(t, refused, "an offset of a shape which is not a shape is refused too")

		diags = append(diags, found...)
	}

	got := renderBoundaryDiagnostics(t, diags)
	assert.Equal(t, expectedOverlayDiagnostics(t, "unusable", got), got)
}

// expectedOverlayDiagnostics returns the rendering held beside the fixture,
// having first rewritten it from got when -update was passed.
func expectedOverlayDiagnostics(t *testing.T, name string, got string) string {
	t.Helper()

	path := filepath.Join("testdata", "overlay", name, "diagnostics.txt")
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestRegionOfANodeWithNoBoundary checks that a node which covers no area is not
// a failure: a circuit group has no outline, and asking for one is not a
// mistake.
func TestRegionOfANodeWithNoBoundary(t *testing.T) {
	model := loadOverlaidModel(t, "shapes")

	region, diags := model.topology.RegionOf(nil, model.boundaries, model.survey)
	assert.Empty(t, diags)
	assert.True(t, region.Empty())

	result, refused := region.Union(model.region(t, "site:S-01"))
	assert.True(t, result.Empty())
	require.Len(t, refused, 1)
	assert.Equal(t, SeverityError, refused[0].Severity)
}

// TestRegionStrings checks the two renderings a region has, which are what a
// reader sees rather than what a caller computes with.
func TestRegionStrings(t *testing.T) {
	model := loadOverlaidModel(t, "shapes")

	plate := model.region(t, "site:S-04")
	assert.Equal(t, "site:S-04: area 84.0 m², 1 piece, 1 hole", plate.String())

	room := model.region(t, "site:S-01")
	assert.Equal(t, "site:S-01: area 12.0 m², 1 piece", room.String())

	collapsed, diags := model.region(t, "site:S-07").Buffer(-0.3)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))
	assert.Equal(t, "the region derived from site:S-07: covers nothing", collapsed.String())

	assert.Equal(t, "touching", ContainmentTouching.String())
}

// TestRegionOfATiltedPlane is its own function because it is the assertion that
// an overlay is computed in the shape's own plane rather than on a plan drawing:
// a ramp covers what it covers, and the shadow it casts on the ground is a
// different and smaller shape.
func TestRegionOfATiltedPlane(t *testing.T) {
	model := loadOverlaidModel(t, "shapes")

	ramp := model.region(t, "site:S-13")

	// Four metres wide and three long up the slope, which is twelve square
	// metres and would be 9.6 measured across the ground.
	assert.InDelta(t, 12, ramp.Area(), 1e-9)

	// A metre in from every edge is two metres by one, and it is a metre in up
	// the slope rather than a metre in across the ground.
	inside, diags := ramp.Buffer(-1)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))
	assert.InDelta(t, 2, inside.Area(), 1e-9)

	containment, diags := ramp.Containment(inside)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))
	assert.Equal(t, ContainmentInside, containment)

	// Every corner it comes back with is a corner of the model, in the ramp's
	// own plane rather than projected onto one of the frame's.
	for _, piece := range inside.Pieces() {
		for _, point := range piece.Outer() {
			assert.InDelta(t, point[1]-20, point[2]*2.4/1.8, 1e-9, "the offset stays in the plane of the ramp")
		}
	}
}

// TestRegionInWithoutResolvedFrames checks the one case In cannot answer: the
// relationship between two frames is a claim, and without those claims read
// there is no transform to apply rather than an identity to fall back on.
func TestRegionInWithoutResolvedFrames(t *testing.T) {
	model := loadOverlaidModel(t, "shapes")

	annex := model.region(t, "site:S-11")

	result, diags := annex.In("frame:building", nil)

	assert.True(t, result.Empty())
	require.Len(t, diags, 1)
	assert.Equal(t, SeverityError, diags[0].Severity)

	// And the frame a region cannot be judged against its own tolerance in is
	// refused rather than converted into.
	unrelated, diags := annex.In("frame:nowhere", model.frames)

	assert.True(t, unrelated.Empty())
	require.Len(t, diags, 1)
	assert.Equal(t, SeverityError, diags[0].Severity)
}
