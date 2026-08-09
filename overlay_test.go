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

// TestRegionOverlayNamesTheOperandItRefuses checks the half of a refusal which
// makes it actionable: which of the two regions is the one which cannot be
// operated on, and where that one was written.
func TestRegionOverlayNamesTheOperandItRefuses(t *testing.T) {
	model := loadOverlaidModel(t, "shapes")

	room := model.region(t, "site:S-01")

	node, ok := model.nodes.Node("site:S-14")
	require.True(t, ok)

	crossed, found := model.topology.RegionOf(node, model.boundaries, model.survey)
	require.NotEmpty(t, found, "a room which crosses itself is not a region")

	result, refused := room.Union(crossed)

	assert.True(t, result.Empty())
	require.Len(t, refused, 1)
	assert.Equal(t, SeverityError, refused[0].Severity)
	assert.Contains(t, refused[0].Message, "site:S-14")
	assert.Equal(t, crossed.span, refused[0].Span, "the refusal points at the operand it is about")
	assert.NotEqual(t, room.span, refused[0].Span)

	// And a region nothing was ever read into carries no position of its own, so
	// a refusal about it points at the region the operation was asked of.
	result, refused = room.Union(Region{})

	assert.True(t, result.Empty())
	require.Len(t, refused, 1)
	assert.Equal(t, room.span, refused[0].Span)
}

// TestRegionSegments checks the attribution a boundary comes back with: which
// edge produced each straight run of it, and which ring of the boundary that run
// belongs to.
//
// The pairing is what an exporter attributes a ring back to the model with. A
// polygon without it is anonymous coordinates, and the correspondence can only
// be recovered by matching them — which is the re-derivation this engine exists
// to prevent.
func TestRegionSegments(t *testing.T) {
	testCases := []struct {
		name   string
		region ID
		rings  []int
		edges  []ID
	}{
		{
			name:   "names the edge every run of a boundary was written as",
			region: "site:S-01",
			rings:  []int{4},
			edges:  []ID{"geom:E-011", "geom:E-012", "geom:E-013", "geom:E-014"},
		},
		{
			name:   "keeps the runs of a courtyard apart from the runs of the plate around it",
			region: "site:S-04",
			rings:  []int{4, 4},
			edges:  []ID{"geom:E-041", "geom:E-042", "geom:E-043", "geom:E-044"},
		},
	}

	model := loadOverlaidModel(t, "shapes")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			region := model.region(t, testCase.region)

			segments := region.Segments()
			require.NotEmpty(t, segments)

			rings := make([]int, 0, len(testCase.rings))
			edges := make([]ID, 0, len(testCase.edges))

			for _, segment := range segments {
				require.NotNil(t, segment.Edge(), "a run of a boundary read from the model was written as an edge")

				// Nothing here bends, so every run is the edge itself rather
				// than a chord standing in for part of it.
				assert.Equal(t, SegmentOriginEdge, segment.Origin())
				assert.False(t, segment.Reversed(), "every loop of this fixture is written in traversal order")

				for segment.Ring() >= len(rings) {
					rings = append(rings, 0)
				}
				rings[segment.Ring()]++

				if segment.Ring() == 0 {
					edges = append(edges, segment.Edge().ID())
				}
			}

			assert.Equal(t, testCase.rings, rings, "one count per ring of the boundary")
			assert.Equal(t, testCase.edges, edges, "the edges of the first ring, in the order the loop traverses them")

			// The runs of one ring join up and the ring closes, which is what
			// makes them a boundary rather than a bag of pairs.
			assertRingsClose(t, segments)
		})
	}
}

// assertRingsClose checks that the runs of each ring leave the corner the
// previous one arrived at and that the last of them arrives back at the first
// corner.
func assertRingsClose(t *testing.T, segments []BoundarySegment) {
	t.Helper()

	for at := 0; at < len(segments); {
		ring := segments[at].Ring()

		end := at
		for end < len(segments) && segments[end].Ring() == ring {
			end++
		}

		for i := at; i < end-1; i++ {
			assert.Equal(t, segments[i].To(), segments[i+1].From(),
				"run %d of ring %d leaves where the one before it arrived", i-at+1, ring)
		}

		assert.Equal(t, segments[end-1].To(), segments[at].From(), "ring %d closes", ring)

		at = end
	}
}

// TestRegionSegmentsRunTheWayTheLoopTraversedTheEdge is its own function because
// what it asserts is an agreement between two regions rather than a property of
// either.
//
// A party wall is one edge which two loops name, and the second of them
// traverses it the other way round. A caller which read the edge's own vertices
// and assumed the run followed them would draw one of the two rooms inside out,
// so the direction is the traversal's answer and travels with the run.
func TestRegionSegmentsRunTheWayTheLoopTraversedTheEdge(t *testing.T) {
	model := loadMeasuredRoot(t, boundaryFixture("valid"))

	const partition = ID("geom:E-02")

	room := segmentOfRegion(t, model, "site:S-101", partition)
	corridor := segmentOfRegion(t, model, "site:S-102", partition)

	assert.False(t, room.Reversed(), "the room traverses the partition the way it was written")
	assert.True(t, corridor.Reversed(), "the corridor traverses it the other way round")

	// Which is the same wall run backwards and not a second wall: one edge, one
	// pair of corners, two directions.
	assert.Equal(t, room.From(), corridor.To())
	assert.Equal(t, room.To(), corridor.From())
	assert.Same(t, room.Edge(), corridor.Edge())
}

// segmentOfRegion is the one run of a region's boundary which names an edge,
// failing the test where the region has none.
func segmentOfRegion(t *testing.T, model measuredModel, subject, edge ID) BoundarySegment {
	t.Helper()

	node, ok := model.nodes.Node(subject)
	require.True(t, ok, "the fixture holds a node %s", subject)

	region, diags := model.topology.RegionOf(node, model.boundaries, model.survey)
	require.Empty(t, renderBoundaryDiagnostics(t, diags), "%s is a region which can be operated on", subject)

	for _, segment := range region.Segments() {
		if segment.Edge() != nil && segment.Edge().ID() == edge {
			return segment
		}
	}

	t.Fatalf("the boundary of %s has no run written as %s", subject, edge)

	return BoundarySegment{}
}

// TestRegionSegmentsOfARegionAnOperationProduced checks the answer the runs of a
// derived boundary give, which is that an operation put them there.
//
// It is the case attribution must not guess at. The boundary of an intersection
// runs partly along each operand and partly along where they cross, and there is
// no edge which is the second kind — so naming the nearest edge which nearly
// produced a run would be a lie the next derivation acts on.
func TestRegionSegmentsOfARegionAnOperationProduced(t *testing.T) {
	testCases := []struct {
		name   string
		derive func(subject, clip Region) (Region, []Diagnostic)
	}{
		{
			name:   "attributes nothing of an intersection to an edge",
			derive: Region.Intersect,
		},
		{
			name:   "attributes nothing of a union to an edge",
			derive: Region.Union,
		},
		{
			name:   "attributes nothing of a difference to an edge",
			derive: Region.Difference,
		},
		{
			name: "attributes nothing of an offset to an edge",
			derive: func(subject, _ Region) (Region, []Diagnostic) {
				return subject.Buffer(1)
			},
		},
	}

	model := loadOverlaidModel(t, "shapes")

	room := model.region(t, "site:S-01")
	store := model.region(t, "site:S-02")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			derived, diags := testCase.derive(room, store)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			segments := derived.Segments()
			require.NotEmpty(t, segments, "a derived boundary still says where it runs")

			for _, segment := range segments {
				assert.Equal(t, SegmentOriginOperation, segment.Origin())
				assert.Nil(t, segment.Edge())
				assert.False(t, segment.Reversed())
			}

			// One run per corner of every ring the operation left, which is the
			// whole of the boundary rather than the part of it which happens to
			// lie along an operand.
			var corners int
			for _, piece := range derived.Pieces() {
				corners += len(piece.Outer())
				for _, hole := range piece.Holes() {
					corners += len(hole)
				}
			}

			assert.Len(t, segments, corners)
			assertRingsClose(t, segments)
		})
	}
}

// TestRegionSegmentStrings checks the rendering a run has, which is what a
// reader sees rather than what a caller computes with.
func TestRegionSegmentStrings(t *testing.T) {
	model := loadOverlaidModel(t, "shapes")

	segments := model.region(t, "site:S-01").Segments()
	require.NotEmpty(t, segments)

	assert.Equal(t, "(0.0 0.0 0.0) to (4.0 0.0 0.0): edge geom:E-011, forwards", segments[0].String())

	offset, diags := model.region(t, "site:S-01").Buffer(1)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	produced := offset.Segments()
	require.NotEmpty(t, produced)
	assert.Contains(t, produced[0].String(), "operation")

	assert.Equal(t, "operation", SegmentOriginOperation.String())
}

// TestRegionOfAnOpenRun is its own function because a run is not a region with
// no area in it: the answer it produces carries a boundary and no pieces, and
// producing it is not a refusal of anything.
func TestRegionOfAnOpenRun(t *testing.T) {
	model := loadMeasuredRoot(t, boundaryFixture("run"))

	railing, ok := model.nodes.Node("site:D-02")
	require.True(t, ok)

	region, diags := model.topology.RegionOf(railing, model.boundaries, model.survey)
	require.Empty(t, renderBoundaryDiagnostics(t, diags), "an open run is a shape and not a defect")

	t.Run("covers nothing, and says so rather than refusing", func(t *testing.T) {
		assert.True(t, region.Empty())
		assert.Zero(t, region.Area())
		assert.Empty(t, region.Pieces())
		assert.Equal(t, ID("site:D-02"), region.Subject())
	})

	t.Run("attributes every straight run of it to the edge it was written as", func(t *testing.T) {
		segments := region.Segments()
		require.Len(t, segments, 2)

		for i, expected := range []ID{"geom:E-21", "geom:E-22"} {
			require.NotNil(t, segments[i].Edge())
			assert.Equal(t, expected, segments[i].Edge().ID())
			assert.Equal(t, SegmentOriginEdge, segments[i].Origin())
			assert.False(t, segments[i].Reversed())
			assert.Equal(t, 0, segments[i].Ring())
		}

		// The last run arrives at the free end of the chain rather than wrapping
		// back onto the corner it started from, which is the whole difference
		// between a run and a ring.
		assert.Equal(t, Point{4, 3, 0}, segments[0].From())
		assert.Equal(t, Point{6, 3, 0}, segments[0].To())
		assert.Equal(t, Point{6, 3, 0}, segments[1].From())
		assert.Equal(t, Point{6, 5, 0}, segments[1].To())
	})

	t.Run("carries the accuracy behind the corners it was read from", func(t *testing.T) {
		assert.NotEmpty(t, region.Budget().Terms())
	})
}
