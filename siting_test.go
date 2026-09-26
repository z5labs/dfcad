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

// The tolerance and the predicate the siting corpus is read against.
const (
	sitingTolerance = "boundary-closure"
	sitingPosition  = "position"
)

// clearanceSlack is how far a clearance computed here may sit from the
// arithmetic of the rectangles it is measured between.
//
// It is the same allowance the overlay corpus makes and for the same reason:
// two corners closer together than the declared tolerance are one corner, so a
// boundary is decided to within that distance of where the arithmetic puts it.
// Asserting exactness would be asserting that the tolerance is not applied.
const clearanceSlack = 0.001

// sitingModel is one fixture loaded and ready to be sited from: the families
// joined, the frames resolved against the claims which measure them, and the
// survey the boundaries are read against.
type sitingModel struct {
	registry   *Registry
	nodes      *Nodes
	topology   *Topology
	claims     *Claims
	boundaries *Boundaries
	frames     *Frames
	survey     Survey
}

// loadSitingModel loads one fixture from a root, failing the test where any
// pass beneath the one under test reports anything.
func loadSitingModel(t *testing.T, root string) sitingModel {
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

	frames, frameDiags := ResolveFrames(registry, claims)
	require.Empty(t, frameDiags, "the fixture's frames resolve clean")

	survey := Survey{Tolerance: sitingTolerance, Registry: registry}
	for vertex := range topology.Vertices() {
		resolution, err := claims.Resolve(vertex.ID(), sitingPosition, registry)
		require.NoError(t, err)

		survey.Place(vertex.ID(), resolution)
	}

	return sitingModel{
		registry:   registry,
		nodes:      nodes,
		topology:   topology,
		claims:     claims,
		boundaries: boundaries,
		frames:     frames,
		survey:     survey,
	}
}

// sited is the corpus every case below is sited against.
func sited(t *testing.T) sitingModel {
	t.Helper()

	return loadSitingModel(t, filepath.Join("testdata", "siting", "surveyed"))
}

// resited is the same corpus with the georeference re-fitted and nothing else
// changed.
func resited(t *testing.T) sitingModel {
	t.Helper()

	return loadSitingModel(t, filepath.Join("testdata", "siting", "resurveyed"))
}

// fit sites one node inside another, returning what came back and whatever was
// reported about it.
func (m sitingModel) fit(t *testing.T, proposal, envelope ID, clearance float64) (Fit, []Diagnostic) {
	t.Helper()

	proposed, ok := m.nodes.Node(proposal)
	require.True(t, ok, "the fixture holds a node %s", proposal)

	within, ok := m.nodes.Node(envelope)
	require.True(t, ok, "the fixture holds a node %s", envelope)

	return m.topology.FitWithin(proposed, within, m.boundaries, m.survey, Siting{
		Frames:    m.frames,
		Clearance: clearance,
	})
}

// termNamed is the accumulated term of that name, and whether the budget holds
// one.
func termNamed(budget Budget, name string) (BudgetTerm, bool) {
	for _, term := range budget.Terms() {
		if term.Name == name {
			return term, true
		}
	}
	return BudgetTerm{}, false
}

// quadrature is the figure a budget would combine to if every term of it were
// independent, which is the mistake this whole arrangement exists to refuse.
//
// It is computed here rather than exposed by the library on purpose: it is the
// wrong answer, and the only thing it is good for is asserting that the right
// one differs from it.
func quadrature(budget Budget) float64 {
	var squares float64
	for _, term := range budget.Terms() {
		squares += term.Magnitude * term.Magnitude
	}
	return math.Sqrt(squares)
}

func TestFitWithin(t *testing.T) {
	testCases := []struct {
		name              string
		proposal          ID
		clearance         float64
		expectedClearance float64
		expectedVerdict   Verdict
		expectedSpill     bool
	}{
		{
			name:              "clears the boundary by more than the answer is known to",
			proposal:          "plan:S-01",
			expectedClearance: 4,
			expectedVerdict:   VerdictFits,
		},
		{
			name:              "reports a clearance inside its own uncertainty as undecided",
			proposal:          "plan:S-03",
			expectedClearance: 0.01,
			expectedVerdict:   VerdictMightFit,
		},
		{
			name:              "reports how deep a proposal reaches past the boundary it crosses",
			proposal:          "plan:S-02",
			expectedClearance: -5,
			expectedVerdict:   VerdictDoesNotFit,
			expectedSpill:     true,
		},
		{
			name:              "reports how far away a proposal which is nowhere near it is",
			proposal:          "plan:S-04",
			expectedClearance: -15,
			expectedVerdict:   VerdictDoesNotFit,
			expectedSpill:     true,
		},
		{
			name:              "takes the clearance required of a proposal off the room it has",
			proposal:          "plan:S-01",
			clearance:         4.5,
			expectedClearance: 4,
			expectedVerdict:   VerdictDoesNotFit,
			expectedSpill:     true,
		},
		{
			name:              "keeps a proposal which clears the requirement as well as the boundary",
			proposal:          "plan:S-01",
			clearance:         3.5,
			expectedClearance: 4,
			expectedVerdict:   VerdictFits,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			answer, diags := sited(t).fit(t, testCase.proposal, "plan:P-01", testCase.clearance)

			assert.Empty(t, sitingErrors(diags), "a fit which was computed reports no error")

			assert.InDelta(t, testCase.expectedClearance, answer.Clearance(), clearanceSlack)
			assert.InDelta(t, testCase.expectedClearance-testCase.clearance, answer.Margin(), clearanceSlack)
			assert.Equal(t, testCase.expectedVerdict, answer.Verdict())
			assert.Equal(t, testCase.expectedSpill, !answer.Spill().Empty())

			assert.Equal(t, testCase.proposal, answer.Subject())
			assert.Equal(t, ID("frame:site"), answer.Frame())
			assert.Equal(t, ID("frame:building"), answer.DeclaredIn())
			assert.True(t, answer.Carried(), "the proposal was declared in the building's own frame")
			assert.Equal(t, Unit("m"), answer.Unit())
			assert.Equal(t, testCase.clearance, answer.Required())
		})
	}
}

// sitingErrors is the diagnostics of error severity, which is what a case
// asserting that a fit was computed reads.
func sitingErrors(diags []Diagnostic) []string {
	var out []string
	for _, diagnostic := range diags {
		if diagnostic.Severity == SeverityError {
			out = append(out, diagnostic.Message)
		}
	}
	return out
}

// TestFitWithinCountsTheSharedTermOnce is its own function because it asserts
// about the arithmetic of the budget rather than about the answer: the
// georeference residual is one error reached from both sides of the comparison,
// and what makes the budget honest is that it appears in the sum exactly once.
func TestFitWithinCountsTheSharedTermOnce(t *testing.T) {
	answer, _ := sited(t).fit(t, "plan:S-01", "plan:P-01", 0)

	budget := answer.Budget()

	shared, ok := termNamed(budget, "control:CP-1")
	require.True(t, ok, "the budget carries the control point behind every measurement")
	assert.Equal(t, TermSystematic, shared.Kind, "a georeference residual does not average away")
	assert.Equal(t, ID("control:CP-1"), shared.Source)
	assert.Equal(t, 0.008, shared.Magnitude)

	// Four corners set out on the building's grid, the fit which relates that
	// grid to the site's, and four corners surveyed on the site grid. Every one
	// of them carried this term and every one of them is named for it, in the
	// order the walk reached them: the proposal, the frame chain it came across,
	// then the envelope.
	assert.True(t, shared.Shared(), "more than one claim contributed it")
	assert.Equal(t,
		[]string{
			"survey:P-0011", "survey:P-0012", "survey:P-0013", "survey:P-0014",
			"survey:C-0001",
			"survey:P-0001", "survey:P-0002", "survey:P-0003", "survey:P-0004",
		},
		claimIDs(shared.Contributors),
	)

	// It is counted once for all that: the combined figure is the one the terms
	// give with a single 0.008, and nine of them would be nowhere near it.
	independent := 4*0.004*0.004 + 4*0.003*0.003 + 0.012*0.012
	systematic := 0.008 + 0.005

	combined, err := answer.Uncertainty()
	require.NoError(t, err)
	assert.InDelta(t, math.Sqrt(independent+systematic*systematic), combined.Magnitude, 1e-12)
	assert.Equal(t, Unit("m"), combined.Unit)
	assert.Equal(t, 1.0, combined.CoverageFactor, "everything the engine produces is one sigma")

	// Every term names the claims behind it, which is what makes a budget
	// actionable rather than a number.
	for _, term := range budget.Terms() {
		assert.NotEmpty(t, term.Name, "every term is named")
		assert.NotEmpty(t, term.Contributors, "every term names the claims it came from")
	}

	dominant, ok := budget.Dominant()
	require.True(t, ok)
	assert.Equal(t, "survey:C-0001", dominant.Name, "the georeference is the thing to re-measure")
}

// claimIDs is the written id of each claim, which is how a budget's
// attribution reads when every claim wrote one.
func claimIDs(claims []*Claim) []string {
	out := make([]string, 0, len(claims))
	for _, claim := range claims {
		id, _ := claim.ID()
		out = append(out, string(id))
	}
	return out
}

// TestFitWithinIsNotQuadrature is its own function because it asserts about two
// ways of combining one budget rather than about one answer.
//
// A regression to all-quadrature is the failure this whole file is written
// against, and it is invisible in every other assertion here: the terms are the
// same terms, the attribution is the same attribution, and only the one number
// somebody acts on is wrong — narrower than the truth, which is the direction
// nobody investigates.
func TestFitWithinIsNotQuadrature(t *testing.T) {
	answer, _ := sited(t).fit(t, "plan:S-01", "plan:P-01", 0)

	budget := answer.Budget()

	combined, err := budget.Combined()
	require.NoError(t, err)

	naive := quadrature(budget)

	assert.Greater(t, combined.Magnitude-naive, 1e-9,
		"the systematic terms add linearly, so quadrature is a different and narrower figure — which is the "+
			"direction nobody investigates")

	// The difference is what the two systematic terms cross-multiply to, which
	// is the whole of the arithmetic being asserted: (a+b)² against a²+b².
	assert.InDelta(t, 2*0.008*0.005, combined.Magnitude*combined.Magnitude-naive*naive, 1e-12)
}

// TestFitWithinFollowsAMoreAccurateClaim is its own function because it asserts
// about two runs over two models rather than about one.
//
// Nothing is derived and written down here, so replacing one measurement with a
// better one has to move both halves of the answer with no other edit
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
func TestFitWithinFollowsAMoreAccurateClaim(t *testing.T) {
	// The models either side of the re-survey are the same bytes, so the only
	// thing which changed is the one claim in the registry which measures the
	// two frames against each other.
	before, err := os.ReadFile(filepath.Join("testdata", "siting", "surveyed", "model.dfc"))
	require.NoError(t, err)

	after, err := os.ReadFile(filepath.Join("testdata", "siting", "resurveyed", "model.dfc"))
	require.NoError(t, err)

	require.Equal(t, string(before), string(after), "the two fixtures differ only in the georeference")

	original, _ := sited(t).fit(t, "plan:S-01", "plan:P-01", 0)
	refitted, _ := resited(t).fit(t, "plan:S-01", "plan:P-01", 0)

	// The answer moves: the building lands twenty millimetres further from the
	// edge it was tightest against.
	assert.InDelta(t, 4.0, original.Clearance(), clearanceSlack)
	assert.InDelta(t, 4.02, refitted.Clearance(), clearanceSlack)

	// And the budget narrows, because the term which changed was one of its
	// larger ones.
	was, err := original.Uncertainty()
	require.NoError(t, err)

	now, err := refitted.Uncertainty()
	require.NoError(t, err)

	assert.Less(t, now.Magnitude, was.Magnitude, "a better measurement is a narrower budget")
	assert.InDelta(t, math.Sqrt(4*0.004*0.004+4*0.003*0.003+0.002*0.002+0.013*0.013), now.Magnitude, 1e-12)

	// The georeference's random term is what improved. Its systematic terms did
	// not, because re-occupying the same control point with a better instrument
	// does not make the control point's own error smaller — and so what
	// dominates the answer afterwards is the control point rather than the fit.
	refit, ok := termNamed(refitted.Budget(), "survey:C-0001")
	require.True(t, ok)
	assert.Equal(t, 0.002, refit.Magnitude)

	control, ok := termNamed(refitted.Budget(), "control:CP-1")
	require.True(t, ok)
	assert.Equal(t, 0.008, control.Magnitude, "the control point is as well known as it ever was")

	dominant, ok := refitted.Budget().Dominant()
	require.True(t, ok)
	assert.Equal(t, "control:CP-1", dominant.Name)
}

// TestFitWithinInOneFrameReadsNoTransform is its own function because it
// asserts about what is absent from a budget.
//
// Two things declared in one frame are compared without a georeference, so the
// answer is as good as the positions and no worse. A budget which carried a
// transform term anyway would be charging for a measurement nothing read.
func TestFitWithinInOneFrameReadsNoTransform(t *testing.T) {
	answer, diags := sited(t).fit(t, "plan:P-01", "plan:P-01", 0)

	assert.Empty(t, sitingErrors(diags))
	assert.False(t, answer.Carried(), "both sides are on the site grid")
	assert.Equal(t, ID("frame:site"), answer.DeclaredIn())

	_, ok := termNamed(answer.Budget(), "survey:C-0001")
	assert.False(t, ok, "no transform was applied, so no fit is in the budget")

	// A region sits inside itself, touching all the way round, which is a
	// clearance of nought rather than a refusal.
	assert.InDelta(t, 0.0, answer.Clearance(), clearanceSlack)
	assert.Equal(t, VerdictMightFit, answer.Verdict(), "nought is inside any uncertainty")
	assert.True(t, answer.Spill().Empty())
	assert.InDelta(t, 600.0, answer.Shared().Area(), 0.25)
}

// TestFitWithinComposesTheRequiredClearance is its own function because it
// asserts about the shape a requirement produces rather than about the number
// it decides.
func TestFitWithinComposesTheRequiredClearance(t *testing.T) {
	answer, diags := sited(t).fit(t, "plan:S-01", "plan:P-01", 4.5)

	assert.Empty(t, sitingErrors(diags))

	// Ten by eight grown by four and a half all round, corners rounded to the
	// declared tolerance: nineteen by seventeen less what the rounding takes off
	// the four corners.
	assert.InDelta(t, 19*17-(4-math.Pi)*4.5*4.5, answer.Needed().Area(), 0.5)

	// Half a metre of it hangs over the western edge, which is the strip a
	// refusal points at.
	require.False(t, answer.Spill().Empty())
	assert.Greater(t, answer.Spill().Area(), 0.0)
	assert.Less(t, answer.Spill().Area(), answer.Needed().Area())

	// The proposal itself is still wholly inside, which is why the clearance is
	// positive and the margin is not.
	assert.Positive(t, answer.Clearance())
	assert.Negative(t, answer.Margin())
	assert.Equal(t, VerdictDoesNotFit, answer.Verdict())
}

// TestFitWithinRefusals is its own function because every case in it comes back
// with no answer at all, which is a different shape from a fit with a verdict.
func TestFitWithinRefusals(t *testing.T) {
	testCases := []struct {
		name            string
		clearance       float64
		frames          bool
		expectedMessage string
	}{
		{
			name:            "refuses a clearance written as a distance outwards",
			clearance:       -1,
			frames:          true,
			expectedMessage: "expected the clearance to keep inside plan:P-01 to be a distance, found -1.0 m",
		},
		{
			name:            "refuses a clearance shorter than the tolerance corners are judged against",
			clearance:       0.001,
			frames:          true,
			expectedMessage: "expected an offset of 0.001 m to be further than the tolerance boundary-closure",
		},
		{
			name:            "refuses to compare two frames when it was given no measurements between them",
			frames:          false,
			expectedMessage: "expected the frames of the model to express plan:S-01 in the frame frame:site",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			model := sited(t)

			proposal, ok := model.nodes.Node("plan:S-01")
			require.True(t, ok)

			envelope, ok := model.nodes.Node("plan:P-01")
			require.True(t, ok)

			siting := Siting{Clearance: testCase.clearance}
			if testCase.frames {
				siting.Frames = model.frames
			}

			answer, diags := model.topology.FitWithin(proposal, envelope, model.boundaries, model.survey, siting)

			assert.Equal(t, Fit{}, answer, "a fit which was refused is not a fit with a verdict")

			require.NotEmpty(t, diags)
			assert.Contains(t, diagnosticMessages(diags)[len(diags)-1], testCase.expectedMessage)
		})
	}
}

// TestFitReportsWhatItWasComputedFrom is its own function because it asserts
// about a rendering rather than about a number.
func TestFitReportsWhatItWasComputedFrom(t *testing.T) {
	answer, _ := sited(t).fit(t, "plan:S-01", "plan:P-01", 0)

	assert.Contains(t, answer.String(), "plan:S-01 in plan:P-01: fits")
	assert.Contains(t, answer.String(), "clearance 4.0 m")
	assert.Contains(t, answer.String(), "k = 1")

	report := answer.Report()
	assert.Contains(t, report, "systematic control:CP-1")
	assert.Contains(t, report, "counted once")
	assert.Contains(t, report, "independent survey:C-0001")

	assert.Equal(t, "nothing was sited", Fit{}.String())
}

// TestVerdictSaysWhetherItDecided is its own function because it is about the
// closed set rather than about any one query.
func TestVerdictSaysWhetherItDecided(t *testing.T) {
	testCases := []struct {
		name            string
		verdict         Verdict
		expectedDecided bool
	}{
		{name: "a clearance beyond its uncertainty decides", verdict: VerdictFits, expectedDecided: true},
		{name: "a deficit beyond its uncertainty decides", verdict: VerdictDoesNotFit, expectedDecided: true},
		{name: "a clearance inside its uncertainty does not", verdict: VerdictMightFit},
		{name: "an uncertainty which could not be computed does not", verdict: VerdictUnknown},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expectedDecided, testCase.verdict.Decided())
			assert.Equal(t, string(testCase.verdict), testCase.verdict.String())
		})
	}
}

// TestFitWithinWithoutAnAccuracyIsUndecided is its own function because it
// asserts about an answer which was computed and cannot be judged, which is a
// third outcome beside fitting and not fitting.
func TestFitWithinWithoutAnAccuracyIsUndecided(t *testing.T) {
	answer, diags := sited(t).fit(t, "plan:S-05", "plan:P-01", 0)

	assert.Empty(t, sitingErrors(diags), "the clearance was computed; only the verdict is withheld")

	// A metre of daylight, and no way at all to say whether it fits: the grid it
	// was set out on was never fitted to the site.
	assert.Equal(t, ID("frame:annex"), answer.DeclaredIn())
	assert.InDelta(t, 1.0, answer.Clearance(), clearanceSlack)
	assert.Equal(t, VerdictUnknown, answer.Verdict())
	assert.Contains(t, answer.String(), "uncertainty unknown")

	_, err := answer.Uncertainty()

	var unknown UnknownAccuracyError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, []string{"survey:C-0002"}, claimIDs(unknown.Claims))

	// The corner which said nothing is named in the report, so that fixing it is
	// one edit rather than a search.
	assert.Contains(t, answer.Report(), "unstated accuracy: survey:C-0002")

	assert.Contains(t, diagnosticMessages(diags),
		"whether plan:S-05 fits inside plan:P-01 cannot be decided: expected an accuracy on every claim the answer "+
			"was computed from, found 1 with none: an unstated accuracy is unknown rather than zero")
}

// setBack sites one node inside what the setbacks of another leave buildable,
// returning what came back and whatever was reported about it.
func (m sitingModel) setBack(t *testing.T, proposal, envelope ID, clearance float64) (Fit, []Diagnostic) {
	t.Helper()

	proposed, ok := m.nodes.Node(proposal)
	require.True(t, ok, "the fixture holds a node %s", proposal)

	within, ok := m.nodes.Node(envelope)
	require.True(t, ok, "the fixture holds a node %s", envelope)

	return m.topology.FitWithin(proposed, within, m.boundaries, m.survey, Siting{
		Frames:    m.frames,
		Clearance: clearance,
		Setbacks:  Setbacks{Predicate: setbackOf, Claims: m.claims},
	})
}

func TestFitWithinWhatTheSetbacksLeave(t *testing.T) {
	testCases := []struct {
		name              string
		proposal          ID
		clearance         float64
		expectedClearance float64
		expectedVerdict   Verdict
		expectedDeclared  ID
	}{
		{
			name:              "refuses a footprint inside the plot which reaches into its front setback",
			proposal:          "plan:S-01",
			expectedClearance: -2,
			expectedVerdict:   VerdictDoesNotFit,
			expectedDeclared:  "frame:building",
		},
		{
			name:              "keeps a required clearance on top of the setbacks rather than instead of them",
			proposal:          "plan:S-01",
			clearance:         1,
			expectedClearance: -2,
			expectedVerdict:   VerdictDoesNotFit,
			expectedDeclared:  "frame:building",
		},
		{
			name:              "carries a proposal across whichever frame it was set out in",
			proposal:          "plan:S-05",
			expectedClearance: -3,
			expectedVerdict:   VerdictUnknown,
			expectedDeclared:  "frame:annex",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			answer, diags := sited(t).setBack(t, testCase.proposal, "plan:P-01", testCase.clearance)

			assert.Empty(t, sitingErrors(diags), "a fit which was computed reports no error")

			assert.InDelta(t, testCase.expectedClearance, answer.Clearance(), clearanceSlack)
			assert.InDelta(t, testCase.expectedClearance-testCase.clearance, answer.Margin(), clearanceSlack)
			assert.Equal(t, testCase.expectedVerdict, answer.Verdict())
			assert.Equal(t, testCase.expectedDeclared, answer.DeclaredIn())
			assert.Equal(t, ID("frame:site"), answer.Frame())
			assert.False(t, answer.Consumed())

			// The envelope is what the setbacks leave, derived rather than read:
			// thirty by twenty less six at the front, four at the rear and three
			// at each flank is twenty-four by ten.
			derivation, setBack := answer.Buildable()
			require.True(t, setBack)
			assert.InDelta(t, 240.0, answer.Envelope().Area(), areaSlack)
			assert.InDelta(t, 600.0, derivation.Boundary().Area(), areaSlack)
			assert.Equal(t, answer.Envelope().Area(), derivation.Area())

			applied := derivation.Setbacks()
			require.Len(t, applied, 4, "one setback per edge, each taken off the edge which claims it")
			for i, expected := range []struct {
				edge     ID
				distance float64
			}{
				{"geom:E-01", 6}, {"geom:E-02", 3}, {"geom:E-03", 4}, {"geom:E-04", 3},
			} {
				assert.Equal(t, expected.edge, applied[i].Edge().ID())
				assert.Equal(t, expected.distance, applied[i].Distance())
			}

			assert.False(t, answer.Spill().Empty(), "the strip over the line is where the refusal points")
		})
	}
}

// TestFitWithinWhatTheSetbacksLeaveIsTheSameRegionBuildableDerives is its own
// function because it compares two queries rather than asserting about one:
// the region a footprint is sited inside has to be the region `buildable`
// reports, or the two commands disagree about where a structure may go.
func TestFitWithinWhatTheSetbacksLeaveIsTheSameRegionBuildableDerives(t *testing.T) {
	model := sited(t)

	answer, _ := model.setBack(t, "plan:S-01", "plan:P-01", 0)

	plot, ok := model.nodes.Node("plan:P-01")
	require.True(t, ok)

	derived, diags := model.topology.BuildableOf(plot, model.boundaries, model.survey,
		Setbacks{Predicate: setbackOf, Claims: model.claims})
	require.Empty(t, sitingErrors(diags))

	assert.Equal(t, derived.Region().Pieces(), answer.Envelope().Pieces())

	derivation, _ := answer.Buildable()
	assert.Equal(t, derived.Setbacks(), derivation.Setbacks())
}

// TestFitWithinCountsTheSetbacksInOneBudget is its own function because it
// asserts about the arithmetic of the budget rather than about the answer.
//
// Composing `buildable` and a fit against the outline outside the engine counts
// the control point behind the boundary corners, and the corners themselves,
// once in each answer. Siting inside the setbacks in one query is what counts
// them once.
func TestFitWithinCountsTheSetbacksInOneBudget(t *testing.T) {
	model := sited(t)

	answer, _ := model.setBack(t, "plan:S-01", "plan:P-01", 0)

	budget := answer.Budget()

	// The setback claims are in the budget beside the corners and the fit, each
	// an independent term of its own.
	for _, id := range []string{"consent:SB-01", "consent:SB-02", "consent:SB-03", "consent:SB-04"} {
		term, ok := termNamed(budget, id)
		require.True(t, ok, "the budget carries the setback %s", id)
		assert.Equal(t, TermIndependent, term.Kind)
		assert.Equal(t, 0.001, term.Magnitude)
	}

	// The control point is one term however many of the thirteen claims reach it.
	shared, ok := termNamed(budget, "control:CP-1")
	require.True(t, ok)
	assert.Equal(t,
		[]string{
			"survey:P-0011", "survey:P-0012", "survey:P-0013", "survey:P-0014",
			"survey:C-0001",
			"survey:P-0001", "survey:P-0002", "survey:P-0003", "survey:P-0004",
		},
		claimIDs(shared.Contributors),
	)

	// The single-budget rule: every independent term in quadrature, each
	// systematic term once and linearly.
	independent := 4*0.004*0.004 + 4*0.003*0.003 + 0.012*0.012 + 4*0.001*0.001
	systematic := 0.008 + 0.005
	single := math.Sqrt(independent + systematic*systematic)

	combined, err := answer.Uncertainty()
	require.NoError(t, err)
	assert.InDelta(t, single, combined.Magnitude, 1e-12)

	// And not the figure two separate answers combined in quadrature give,
	// which counts the boundary's corners and its control point twice.
	plot, ok := model.nodes.Node("plan:P-01")
	require.True(t, ok)

	derived, _ := model.topology.BuildableOf(plot, model.boundaries, model.survey,
		Setbacks{Predicate: setbackOf, Claims: model.claims})
	buildable, err := derived.Budget().Combined()
	require.NoError(t, err)

	outline, _ := model.fit(t, "plan:S-01", "plan:P-01", 0)
	fitted, err := outline.Uncertainty()
	require.NoError(t, err)

	separate := math.Sqrt(buildable.Magnitude*buildable.Magnitude + fitted.Magnitude*fitted.Magnitude)

	assert.Greater(t, math.Abs(separate-combined.Magnitude), 1e-3,
		"two answers combined count every term they share twice")
	assert.Less(t, combined.Magnitude, separate)
}

// TestFitWithinWhatTheSetbacksLeaveRefusesAnUnclaimedEdge is its own function
// because every case in it comes back with no answer at all.
func TestFitWithinWhatTheSetbacksLeaveRefusesAnUnclaimedEdge(t *testing.T) {
	unclaimed := reclaimed(t, filepath.Join("testdata", "siting", "surveyed"), `(setback
    (id consent:SB-02)
    (value 3.0 m)
    (source "Planning consent PC-2026-017, condition 4: the east flank")
    (method method:statutory-instrument)
    (accuracy (independent 0.001 m))
    (date "2026-01-20"))`, "")

	testCases := []struct {
		name             string
		root             string
		setbacks         func(Setbacks) Setbacks
		expectedMessages []string
	}{
		{
			name:             "names the edge nothing sets back rather than reading the silence as nought",
			root:             unclaimed,
			setbacks:         func(given Setbacks) Setbacks { return given },
			expectedMessages: []string{"found none on geom:E-02"},
		},
		{
			name:     "names every edge a predicate nobody claims leaves unset",
			root:     filepath.Join("testdata", "siting", "surveyed"),
			setbacks: func(given Setbacks) Setbacks { given.Predicate = "frontage"; return given },
			expectedMessages: []string{
				"found none on geom:E-01", "found none on geom:E-02",
				"found none on geom:E-03", "found none on geom:E-04",
			},
		},
		{
			name:             "refuses setbacks asked for without the predicate they are claimed under",
			root:             filepath.Join("testdata", "siting", "surveyed"),
			setbacks:         func(given Setbacks) Setbacks { given.Predicate = ""; return given },
			expectedMessages: []string{"found no predicate named"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			model := loadSitingModel(t, testCase.root)

			proposal, ok := model.nodes.Node("plan:S-01")
			require.True(t, ok)

			envelope, ok := model.nodes.Node("plan:P-01")
			require.True(t, ok)

			answer, diags := model.topology.FitWithin(proposal, envelope, model.boundaries, model.survey, Siting{
				Frames:   model.frames,
				Setbacks: testCase.setbacks(Setbacks{Predicate: setbackOf, Claims: model.claims}),
			})

			assert.Equal(t, Fit{}, answer, "a fit which was refused is not a fit with a verdict")

			errs := sitingErrors(diags)
			require.Len(t, errs, len(testCase.expectedMessages))
			for i, expected := range testCase.expectedMessages {
				assert.Contains(t, errs[i], expected)
			}
		})
	}
}

// TestFitWithinWhatTheSetbacksLeaveWhenTheyLeaveNothing is its own function
// because the answer is decided by the envelope covering nothing rather than by
// a margin, which is a different set of assertions from every fit above.
func TestFitWithinWhatTheSetbacksLeaveWhenTheyLeaveNothing(t *testing.T) {
	model := parcels(t)

	plot, ok := model.nodes.Node("plan:P-02")
	require.True(t, ok)

	answer, diags := model.topology.FitWithin(plot, plot, model.boundaries, model.survey, Siting{
		Setbacks: Setbacks{Predicate: setbackOf, Claims: model.claims},
	})

	assert.Empty(t, sitingErrors(diags), "nothing fits an empty region, which is an answer and not a refusal")
	require.Len(t, diags, 1)
	assert.Equal(t, SeverityWarning, diags[0].Severity)
	assert.Contains(t, diags[0].Message, "leave nothing buildable")

	assert.True(t, answer.Consumed(), "the answer says the region was empty")
	assert.Equal(t, VerdictDoesNotFit, answer.Verdict())
	assert.True(t, answer.Verdict().Decided())
	assert.True(t, answer.Envelope().Empty())
	assert.True(t, answer.Shared().Empty())
	assert.InDelta(t, answer.Needed().Area(), answer.Spill().Area(), areaSlack,
		"all of what the proposal needs is what the envelope does not offer")

	derivation, setBack := answer.Buildable()
	require.True(t, setBack)
	assert.Len(t, derivation.Setbacks(), 4)

	assert.Contains(t, answer.String(), "the setbacks leave nothing buildable")
	assert.Contains(t, answer.String(), "does-not-fit")
}

// TestFitWithinWhatTheSetbacksLeaveReportsIt is its own function because it
// asserts about a rendering rather than about a number.
func TestFitWithinWhatTheSetbacksLeaveReportsIt(t *testing.T) {
	answer, _ := sited(t).setBack(t, "plan:S-01", "plan:P-01", 0)

	assert.Contains(t, answer.String(),
		"plan:S-01 in what the setbacks of plan:P-01 leave buildable: does-not-fit, clearance -2.0 m")

	report := answer.Report()
	assert.Contains(t, report, "setback geom:E-01: 6.0 m, from consent:SB-01")
	assert.Contains(t, report, "independent consent:SB-01")

	outline, _ := sited(t).fit(t, "plan:S-01", "plan:P-01", 0)
	_, setBack := outline.Buildable()
	assert.False(t, setBack, "a fit against the outline took nothing off it")
	assert.NotContains(t, outline.Report(), "setback")
}
