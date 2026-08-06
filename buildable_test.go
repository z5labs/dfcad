// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tolerance and the two predicates the parcel fixture is derived against.
const (
	parcelTolerance = "boundary-closure"
	setbackOf       = "setback"
	setbackInMM     = "setback-mm"
)

// areaSlack is how far an area computed here may sit from the arithmetic of the
// rectangle it ought to be.
//
// It is not floating-point noise. Two corners closer together than the declared
// tolerance are one corner, so a boundary drawn through a junction of a strip,
// a rounded corner and the parcel's own edge lands within that distance of where
// the arithmetic puts it — which over a boundary of a few tens of metres is a
// fraction of a square metre. Asserting exactness would be asserting that the
// tolerance is not applied.
const areaSlack = 0.25

// parcelModel is one fixture loaded and ready to be derived from: the families
// joined, the survey the boundaries are read against and the claims the
// setbacks are resolved from.
type parcelModel struct {
	registry   *Registry
	nodes      *Nodes
	topology   *Topology
	claims     *Claims
	boundaries *Boundaries
	survey     Survey
}

// loadParcelModel loads one fixture from a root, failing the test where any
// pass beneath the one under test reports anything.
//
// Every fixture here loads clean, including the parcels whose setbacks are
// refused: a setback written in the wrong unit, one written outwards and one
// nobody wrote at all are well-formed claims about well-formed edges, and it is
// the derivation which refuses them rather than the loader.
func loadParcelModel(t *testing.T, root string) parcelModel {
	t.Helper()

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

	survey := Survey{Tolerance: parcelTolerance, Registry: registry}
	for vertex := range topology.Vertices() {
		resolution, err := claims.Resolve(vertex.ID(), "position", registry)
		require.NoError(t, err)

		survey.Place(vertex.ID(), resolution)
	}

	return parcelModel{
		registry:   registry,
		nodes:      nodes,
		topology:   topology,
		claims:     claims,
		boundaries: boundaries,
		survey:     survey,
	}
}

// parcels is the fixture corpus every case below is derived from.
func parcels(t *testing.T) parcelModel {
	t.Helper()

	return loadParcelModel(t, filepath.Join("testdata", "buildable", "parcels"))
}

// buildable derives one parcel of a fixture under a predicate, returning what
// came back and whatever was reported about it.
func (m parcelModel) buildable(t *testing.T, id ID, predicate string) (Buildable, []Diagnostic) {
	t.Helper()

	node, ok := m.nodes.Node(id)
	require.True(t, ok, "the fixture holds a node %s", id)

	return m.topology.BuildableOf(node, m.boundaries, m.survey, Setbacks{
		Predicate: predicate,
		Claims:    m.claims,
	})
}

// diagnosticMessages is what a set of diagnostics says, which is what a case asserting
// about the wording of a refusal reads.
func diagnosticMessages(diags []Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		out = append(out, diagnostic.Message)
	}
	return out
}

func TestBuildableOf(t *testing.T) {
	testCases := []struct {
		name             string
		parcel           ID
		expectedArea     float64
		expectedParcel   float64
		expectedSetbacks []float64
	}{
		{
			name:             "takes a different setback off each edge it was written on",
			parcel:           "plan:P-01",
			expectedArea:     240,
			expectedParcel:   600,
			expectedSetbacks: []float64{6, 3, 4, 3},
		},
		{
			name:             "reaches the boundary along an edge set back by nought",
			parcel:           "plan:P-05",
			expectedArea:     128,
			expectedParcel:   200,
			expectedSetbacks: []float64{0, 2, 2, 2},
		},
	}

	model := parcels(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, diags := model.buildable(t, testCase.parcel, setbackOf)
			require.Empty(t, renderBoundaryDiagnostics(t, diags))

			assert.InDelta(t, testCase.expectedArea, result.Area(), areaSlack)
			assert.InDelta(t, testCase.expectedParcel, result.Boundary().Area(), areaSlack)
			assert.Equal(t, testCase.parcel, result.Subject())
			assert.False(t, result.Empty())

			applied := make([]float64, 0, len(result.Setbacks()))
			edges := make([]ID, 0, len(result.Setbacks()))
			for _, setback := range result.Setbacks() {
				applied = append(applied, setback.Distance())
				edges = append(edges, setback.Edge().ID())
				assert.Equal(t, Unit("m"), setback.Unit())
				assert.NotNil(t, setback.Claim(), "every setback carries the claim it came from")
			}

			assert.Equal(t, testCase.expectedSetbacks, applied)
			assert.Len(t, edges, len(testCase.expectedSetbacks))
		})
	}
}

// TestBuildableOfIsNeverAuthored is its own function because it asserts about
// two derivations of one parcel rather than about one: the region has to follow
// the claim, and the only way to see that is to change the claim and nothing
// else.
func TestBuildableOfIsNeverAuthored(t *testing.T) {
	root := filepath.Join("testdata", "buildable", "parcels")

	before := loadParcelModel(t, root)
	first, diags := before.buildable(t, "plan:P-01", setbackOf)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	// One byte-level edit to one claim, in a copy of the fixture: the road
	// frontage is set back nine metres rather than six. Nothing else moves —
	// not a corner, not an edge, not the node, and nowhere does the model say
	// what is buildable.
	edited := reclaimed(t, root, "(value 6.0 m)", "(value 9.0 m)")

	after := loadParcelModel(t, edited)
	second, diags := after.buildable(t, "plan:P-01", setbackOf)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	assert.InDelta(t, 240.0, first.Area(), areaSlack)
	assert.InDelta(t, 168.0, second.Area(), areaSlack, "three metres more frontage over a 24 m width")

	// The parcel itself did not move, which is what says the difference is the
	// setback rather than the boundary.
	assert.InDelta(t, first.Boundary().Area(), second.Boundary().Area(), areaSlack)
}

// reclaimed copies a fixture into a directory of its own with one substitution
// made in its model, and returns the root of the copy.
//
// The substitution has to match exactly once. A change which quietly rewrote a
// second claim would be an edit to more than the one thing the test is about,
// and the assertion which follows would hold for the wrong reason.
func reclaimed(t *testing.T, root, from, to string) string {
	t.Helper()

	edited := t.TempDir()

	entries, err := os.ReadDir(root)
	require.NoError(t, err)

	var replaced int
	for _, entry := range entries {
		src, err := os.ReadFile(filepath.Join(root, entry.Name()))
		require.NoError(t, err)

		written := string(src)
		replaced += strings.Count(written, from)
		written = strings.ReplaceAll(written, from, to)

		require.NoError(t, os.WriteFile(filepath.Join(edited, entry.Name()), []byte(written), 0o644))
	}

	require.Equal(t, 1, replaced, "the edit is to exactly one claim")

	return edited
}

// TestBuildableOfConsumedByItsSetbacks is its own function because the answer it
// asserts about is an empty region beside a diagnostic, which is a different
// shape of result from a region with an area.
func TestBuildableOfConsumedByItsSetbacks(t *testing.T) {
	model := parcels(t)

	result, diags := model.buildable(t, "plan:P-02", setbackOf)

	assert.True(t, result.Empty(), "nothing is buildable")
	assert.Zero(t, result.Area())
	assert.Empty(t, result.Region().Pieces(), "and no inside-out shape was produced instead")

	// The parcel is still the parcel, and the setbacks which consumed it still
	// come back: an empty answer nobody can see the rule behind is one somebody
	// re-derives by hand.
	assert.InDelta(t, 64.0, result.Boundary().Area(), areaSlack)
	assert.Len(t, result.Setbacks(), 4)

	require.Len(t, diags, 1)
	assert.Equal(t, SeverityWarning, diags[0].Severity, "it is the answer rather than a failure")
	assert.Contains(t, diags[0].Message, "leave nothing buildable")
}

// TestBuildableOfRefusesWhatItCannotRead is its own function because every case
// in it produces no region at all, which is a different set of assertions from
// a case which produces one.
func TestBuildableOfRefusesWhatItCannotRead(t *testing.T) {
	testCases := []struct {
		name             string
		parcel           ID
		predicate        string
		expectedCount    int
		expectedInFirst  string
		expectedEdgeName string
	}{
		{
			name:             "names the edge a setback was needed for and never reads the silence as nought",
			parcel:           "plan:P-03",
			predicate:        setbackOf,
			expectedCount:    1,
			expectedInFirst:  "found none on",
			expectedEdgeName: "geom:E-303",
		},
		{
			name:             "refuses an edge two claims are equally current about",
			parcel:           "plan:P-04",
			predicate:        setbackOf,
			expectedCount:    1,
			expectedInFirst:  "equally current",
			expectedEdgeName: "geom:E-404",
		},
		{
			name:             "refuses every edge whose setback is in a unit the frame is not in",
			parcel:           "plan:P-06",
			predicate:        setbackInMM,
			expectedCount:    4,
			expectedInFirst:  "which is the unit of the frame",
			expectedEdgeName: "geom:E-601",
		},
		{
			name:             "refuses a setback written outwards",
			parcel:           "plan:P-07",
			predicate:        setbackOf,
			expectedCount:    1,
			expectedInFirst:  "a distance inwards from the boundary",
			expectedEdgeName: "geom:E-701",
		},
		{
			name:             "refuses a setback shorter than the tolerance it would be judged against",
			parcel:           "plan:P-08",
			predicate:        setbackOf,
			expectedCount:    1,
			expectedInFirst:  "to be further than the tolerance",
			expectedEdgeName: "geom:E-801",
		},
	}

	model := parcels(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, diags := model.buildable(t, testCase.parcel, testCase.predicate)

			assert.True(t, result.Empty(), "nothing was derived")
			assert.Empty(t, result.Setbacks())

			require.Len(t, diags, testCase.expectedCount)
			for _, diagnostic := range diags {
				assert.Equal(t, SeverityError, diagnostic.Severity)
			}

			assert.Contains(t, diags[0].Message, testCase.expectedInFirst)
			assert.Contains(t, strings.Join(diagnosticMessages(diags), "\n"), testCase.expectedEdgeName)
		})
	}
}

// TestBuildableOfNeedsAPredicateAndABoundary is its own function because it is
// about what the derivation is asked rather than about what the model says.
func TestBuildableOfNeedsAPredicateAndABoundary(t *testing.T) {
	model := parcels(t)

	node, ok := model.nodes.Node("plan:P-01")
	require.True(t, ok)

	t.Run("refuses a derivation told no predicate", func(t *testing.T) {
		result, diags := model.topology.BuildableOf(node, model.boundaries, model.survey, Setbacks{
			Claims: model.claims,
		})

		assert.True(t, result.Empty())
		require.Len(t, diags, 1)
		assert.Equal(t, SeverityError, diags[0].Severity)
		assert.Contains(t, diags[0].Message, "found no predicate named")
	})

	t.Run("refuses a derivation with no claims to read", func(t *testing.T) {
		result, diags := model.topology.BuildableOf(node, model.boundaries, model.survey, Setbacks{
			Predicate: setbackOf,
		})

		assert.True(t, result.Empty())
		require.Len(t, diags, 1)
		assert.Contains(t, diags[0].Message, "found no claims to read them from")
	})

	t.Run("refuses a node which bounds no parcel", func(t *testing.T) {
		result, diags := model.topology.BuildableOf(nil, model.boundaries, model.survey, Setbacks{
			Predicate: setbackOf,
			Claims:    model.claims,
		})

		assert.True(t, result.Empty())
		require.Len(t, diags, 1)
		assert.Contains(t, diags[0].Message, "covers no area which could be set back")
	})
}

// TestBuildableBudgetCarriesBothFamilies is its own function because it asserts
// about the accuracy of the answer rather than about its shape.
func TestBuildableBudgetCarriesBothFamilies(t *testing.T) {
	model := parcels(t)

	result, diags := model.buildable(t, "plan:P-01", setbackOf)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	budget := result.Budget()
	require.True(t, budget.Known(), "every claim behind the answer says how well it is known")

	var independent, systematic int
	var sources []string
	for _, term := range budget.Terms() {
		switch term.Kind {
		case TermIndependent:
			independent++
		case TermSystematic:
			systematic++
			sources = append(sources, string(term.Source))
		}
	}

	// Four corners and four setbacks, each with an independent term of its own,
	// and one control point shared by every corner — counted once, which is the
	// whole reason a budget accumulates claims rather than numbers.
	assert.Equal(t, 8, independent)
	assert.Equal(t, 1, systematic)
	assert.Equal(t, []string{"control:CP-1"}, sources)

	// The setback claims are in it as well as the position claims, which is what
	// says the accuracy of the answer is derived from both.
	var fromSetbacks int
	for _, term := range budget.Terms() {
		for _, contributor := range term.Contributors {
			if contributor.Predicate() == setbackOf {
				fromSetbacks++
			}
		}
	}
	assert.Equal(t, 4, fromSetbacks)

	combined, err := budget.Combined()
	require.NoError(t, err)
	assert.Positive(t, combined.Standard())
}

// TestBuildableRendering is its own function because it is about what a person
// reads rather than about what was computed.
func TestBuildableRendering(t *testing.T) {
	model := parcels(t)

	sited, diags := model.buildable(t, "plan:P-01", setbackOf)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	assert.Equal(t, "plan:P-01: 240.0 m² buildable of 600.0 m²", sited.String())
	assert.Contains(t, sited.Report(), "geom:E-101: 6.0 m")

	consumed, _ := model.buildable(t, "plan:P-02", setbackOf)
	assert.Equal(t, "nothing is buildable inside plan:P-02", consumed.String())

	assert.Equal(t, "nothing buildable was derived", Buildable{}.String())
}

// TestRegionSegmentsAreOnlyOnARegionRead is its own function because it is
// about the region rather than about the derivation over it: a region an
// operation produced has no edges of its own to set back.
func TestRegionSegmentsAreOnlyOnARegionRead(t *testing.T) {
	model := parcels(t)

	node, ok := model.nodes.Node("plan:P-01")
	require.True(t, ok)

	region, diags := model.topology.RegionOf(node, model.boundaries, model.survey)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))
	require.Len(t, region.segments, 4)

	offset, diags := region.Buffer(-1)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))
	assert.Empty(t, offset.segments, "an offset boundary runs where no edge was written")
}

// TestBuildableSetbacksFollowTheBoundarySegments is its own function because
// what it asserts is that two answers come from one place rather than that
// either is right.
//
// Which edge produced which run of a boundary is [Region.Segments], and a
// setback is that answer read under a predicate. A derivation which worked the
// pairing out again for itself would be a second implementation of it, and the
// day the two disagree is the day a setback is taken off an edge the boundary
// does not run along.
func TestBuildableSetbacksFollowTheBoundarySegments(t *testing.T) {
	model := parcels(t)

	result, diags := model.buildable(t, "plan:P-01", setbackOf)
	require.Empty(t, renderBoundaryDiagnostics(t, diags))

	// Every edge of the parcel's boundary, once, in the order the loop
	// traverses it — which is the order the setbacks come back in.
	var expected []ID
	seen := make(map[ID]bool)

	segments := result.Boundary().Segments()
	require.NotEmpty(t, segments)

	for _, segment := range segments {
		require.NotNil(t, segment.Edge())

		if seen[segment.Edge().ID()] {
			continue
		}
		seen[segment.Edge().ID()] = true

		expected = append(expected, segment.Edge().ID())
	}

	applied := make([]ID, 0, len(result.Setbacks()))
	for _, setback := range result.Setbacks() {
		applied = append(applied, setback.Edge().ID())
	}

	assert.Equal(t, expected, applied)

	// And what is buildable is derived, so it attributes none of its own
	// boundary: the strip along an edge is where the setback put it and no edge
	// of the model runs there.
	for _, segment := range result.Region().Segments() {
		assert.Equal(t, SegmentOriginOperation, segment.Origin())
		assert.Nil(t, segment.Edge())
	}
}
