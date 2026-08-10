// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checkFixture is the root of one fixture model the check set is run over.
func checkFixture(name string) string { return filepath.Join("testdata", "checks", name) }

// loadCheckFixture loads one fixture model, failing the test on any diagnostic.
//
// Every failure these tests are about is a shape or a reference which loads
// perfectly well — a loop which does not close, two rooms drawn over one
// another, a fit too loose for what it is used for — so a diagnostic from the
// load means the fixture says something other than what it was written to say,
// and the check under test would be reporting on a model which is not the one on
// disk.
func loadCheckFixture(t *testing.T, name string) *Graph {
	t.Helper()

	graph, diags := LoadGraph(checkFixture(name))
	require.NotNil(t, graph, "a load always yields a usable graph")

	for _, diagnostic := range diags {
		t.Errorf("unexpected diagnostic: %s", diagnostic)
	}

	return graph
}

// runCheckFixture runs every rule one fixture model states.
func runCheckFixture(t *testing.T, name string) CheckRun {
	t.Helper()

	return loadCheckFixture(t, name).Rules().Run()
}

// reportedBy is the message of every violation one check reported about one
// thing, in the order the run found them.
//
// Both halves of the filter matter. A fixture states more than one rule about
// more than one thing on purpose — a check which reported about the wrong
// subject would otherwise look like the right answer — and asserting on the
// whole run would make every case here fail when any one of them changed.
func reportedBy(run CheckRun, check string, instance ID) []string {
	var out []string
	for _, violation := range run.Violations {
		if violation.Check != check || violation.Instance != instance {
			continue
		}
		out = append(out, violation.Message)
	}
	return out
}

// TestTheInitialCheckSetReportsWhatItIsFor covers the message each check leaves
// on a model which breaks it.
//
// The exact message is the assertion rather than the fact of a failure. A check
// which fires and cannot say what it found, or says it in a figure nobody can
// act on, sends the reader to look at the whole drawing: four square metres of
// overlap is a wall in the wrong place and 0.3 m of gap is a corner naming the
// wrong vertex, and neither reads as "this does not hold".
func TestTheInitialCheckSetReportsWhatItIsFor(t *testing.T) {
	run := runCheckFixture(t, "violating")

	testCases := []struct {
		name     string
		check    string
		instance ID
		expected string
	}{
		{
			name:     "reports a loop which does not close, naming the gap and how wide it is",
			check:    "boundary-loops-close",
			instance: "site:S-901",
			expected: "expected the loop geom:L-13 to close, found a gap of 0.3 m between geom:V-13 and geom:V-09",
		},
		{
			name:     "reports a gap it could not measure where the rule names no position predicate",
			check:    "boundary-loops-close",
			instance: "site:S-902",
			expected: "expected the loop geom:L-13 to close, found a gap between geom:V-13 and geom:V-09 whose size could not be measured",
		},
		{
			name:     "reports two contents which cover the same ground, naming the pair and the area they share",
			check:    "contained-areas-do-not-overlap",
			instance: "site:L-01",
			expected: "expected no two of the shapes within site:L-01 to cover the same ground, found site:S-101 and " +
				"site:S-102 overlapping by 4.0 m²",
		},
		{
			name:     "reports contents which do not add up to the whole, naming the discrepancy and its sign",
			check:    "contained-areas-sum",
			instance: "site:L-01",
			expected: "expected what site:L-01 contains to add up to its own 24.0 m², found 28.0 m², which is " +
				"4.0 m² more than the whole",
		},
		{
			name:     "refuses to sum an area read in one frame into a whole drawn in another",
			check:    "contained-areas-sum",
			instance: "site:L-05",
			expected: "expected everything summed into site:L-05 to be declared in frame:building, the frame it is " +
				"drawn in, found site:S-501 in frame:annex",
		},
		{
			name:     "reports a shape which crosses into a zone, naming the zone and how much of it is crossed",
			check:    "stays-clear-of-zone",
			instance: "site:S-102",
			expected: "expected site:S-102 to stay clear of the zone site:Z-90, found it crossing into it over 4.0 m²",
		},
		{
			name:     "reports a cross-frame answer wider than its limit, naming the answer, the budget and the limit",
			check:    "cross-frame-budget-holds",
			instance: "site:A-01",
			expected: "expected site:A-01 in frame:building to be known to within 0.008 m, found a combined " +
				"uncertainty of 0.01 m (k = 1.0, ≈ 68%) accumulated from 2 terms",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := reportedBy(run, testCase.check, testCase.instance)

			require.Len(t, got, 1, "the rule failed exactly once")
			assert.Equal(t, testCase.expected, got[0])
		})
	}

	t.Run("says what to do about each of them", func(t *testing.T) {
		for _, violation := range run.Violations {
			assert.NotEmpty(t, violation.Hint, "%s on %s", violation.Check, violation.Instance)
		}
	})

	t.Run("runs every rule the model states and fails the seven which are broken", func(t *testing.T) {
		assert.Equal(t, 8, run.Rules)
		assert.Equal(t, 8, run.Ran, "every check the fixture names has an implementation")
		assert.Equal(t, 7, run.Failed)
		assert.Equal(t, 1, run.Passed)
	})

	t.Run("compares only the contents of the kind the rule narrows to", func(t *testing.T) {
		// The storey states the overlap rule twice: once over everything it
		// contains, which the two rooms break, and once over the elements in it,
		// which is the wall and nothing to pair it with. Same subject, same
		// shapes, and one of the two decides differently — which is the whole of
		// what the kind parameter does.
		var narrowed int
		for _, rule := range loadCheckFixture(t, "violating").Rules() {
			if rule.Check.Name != "contained-areas-do-not-overlap" {
				continue
			}
			for _, argument := range rule.Arguments {
				if argument.Name != "kind" {
					continue
				}

				narrowed++
				assert.Empty(t, rule.Run())
			}
		}

		assert.Equal(t, 1, narrowed, "the fixture states one narrowed rule")
	})
}

// TestTheInitialCheckSetPassesAModelWhichHolds is its own function because it
// asserts an absence, which the messages above cannot distinguish from a check
// which reports a violation of everything put to it.
func TestTheInitialCheckSetPassesAModelWhichHolds(t *testing.T) {
	run := runCheckFixture(t, "satisfied")

	assert.Empty(t, run.Violations)
	assert.Equal(t, run.Rules, run.Ran, "every rule the fixture states has an implementation")
	assert.Equal(t, run.Rules, run.Passed)
	assert.Zero(t, run.Failed)
}

// TestTheInitialCheckSetOnDegenerateInput covers what each check does with the
// models it meets before anybody has finished drawing one.
//
// Every case here is a decision rather than an accident. A node whose type
// declares an area and which references no outline is not a pass, because there
// is nothing to hold the rule of; a storey nobody has divided up is, because
// nothing was subdivided. The difference between those two is the difference
// between a gate which reports a model sound and one which reports it unchecked.
func TestTheInitialCheckSetOnDegenerateInput(t *testing.T) {
	run := runCheckFixture(t, "degenerate")

	testCases := []struct {
		name     string
		check    string
		instance ID
		expected []string
	}{
		{
			name:     "holds of a container with one content, which has nothing to overlap",
			check:    "contained-areas-do-not-overlap",
			instance: "site:L-02",
			expected: nil,
		},
		{
			name:     "holds where the one content is the whole of the container",
			check:    "contained-areas-sum",
			instance: "site:L-02",
			expected: nil,
		},
		{
			name:     "holds of a container which has been divided up into nothing",
			check:    "contained-areas-sum",
			instance: "site:L-03",
			expected: nil,
		},
		{
			name:     "reports a container with no outline for its contents to add up to",
			check:    "contained-areas-sum",
			instance: "site:L-04",
			expected: []string{"expected a shape on site:L-04 to add its contents up to, found no loop bounding it"},
		},
		{
			name:     "holds of a node with no loop bounding it, which has none which fail to close",
			check:    "boundary-loops-close",
			instance: "site:L-04",
			expected: nil,
		},
		{
			name:     "reports a subject with no outline to judge against the zone",
			check:    "stays-clear-of-zone",
			instance: "site:S-401",
			expected: []string{
				"expected a shape on site:S-401 to judge against the zone site:Z-41, found no loop bounding it",
			},
		},
		{
			name:     "reports a zone which encloses nothing to stay clear of",
			check:    "stays-clear-of-zone",
			instance: "site:S-402",
			expected: []string{
				"expected the zone site:Z-40 to have a shape to stay clear of, found no loop bounding it",
			},
		},
		{
			name:     "holds of a subject already in the frame the answer is wanted in",
			check:    "cross-frame-budget-holds",
			instance: "site:A-02",
			expected: nil,
		},
		{
			name:     "holds of an edge which says nothing backs it, which is a virtual boundary",
			check:    "edge-backing-resolves",
			instance: "geom:E-04",
			expected: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, reportedBy(run, testCase.check, testCase.instance))
		})
	}

	t.Run("decides every rule stated of it either way", func(t *testing.T) {
		assert.Equal(t, run.Rules, run.Ran)
		assert.Equal(t, 3, run.Failed)
	})
}

// TestClaimAgreesWithGeometry is its own function because its fixture holds the
// geometry still and varies only what is written about it.
//
// The shape is one outline, one run of wall and one pair of corners, shared by
// every subject which uses them, so the difference between a case which passes
// and a case which fails is the claim and nothing else. That is the failure the
// check is for: a number which stopped matching a boundary nobody touched
// afterwards.
//
// The spans at the end are written on edges rather than on nodes, and every one
// of them belongs to no loop. That the fixture loads clean is half the assertion
// about them: an assertion naming a check which cannot examine the form it is
// written on is refused when the model is loaded, so a check which did not bind
// to an edge would fail here before any of these cases were decided.
func TestClaimAgreesWithGeometry(t *testing.T) {
	run := runCheckFixture(t, "agreement")

	testCases := []struct {
		name     string
		instance ID
		expected []string
	}{
		{
			name:     "holds where the number written down is the one the shape computes to",
			instance: "site:S-101",
			expected: nil,
		},
		{
			name:     "reports a claim larger than its shape, naming the discrepancy and which way it runs",
			instance: "site:S-102",
			expected: []string{
				"expected the area claimed of site:S-102 to agree with the shape it is drawn as, found 14.0 m² " +
					"claimed against 12.0 m² measured, which is 2.0 m² more than the shape",
			},
		},
		{
			name:     "reports a claim smaller than its shape, which is the other mistake",
			instance: "site:S-103",
			expected: []string{
				"expected the area claimed of site:S-103 to agree with the shape it is drawn as, found 10.0 m² " +
					"claimed against 12.0 m² measured, which is 2.0 m² less than the shape",
			},
		},
		{
			name:     "leaves a subject which is measured and not yet drawn alone",
			instance: "site:S-104",
			expected: nil,
		},
		{
			name:     "leaves a subject which is drawn and not yet measured alone",
			instance: "site:S-105",
			expected: nil,
		},
		{
			name:     "compares the claim which replaced a retracted one rather than the retracted one",
			instance: "site:S-106",
			expected: nil,
		},
		{
			name:     "holds where the two differ by more than the declared discrepancy and less than their uncertainty",
			instance: "site:S-107",
			expected: nil,
		},
		{
			name:     "holds where nothing says how good the claim is and the two are inside the declared discrepancy",
			instance: "site:S-109",
			expected: nil,
		},
		{
			name:     "reports a claim which says nothing about how good it is and still does not match",
			instance: "site:S-110",
			expected: []string{
				"expected the area claimed of site:S-110 to agree with the shape it is drawn as, found 12.5 m² " +
					"claimed against 12.0 m² measured, which is 0.5 m² more than the shape",
			},
		},
		{
			name:     "reports a predicate carrying something the shape could never be compared against",
			instance: "site:S-108",
			expected: []string{
				"expected the remark claimed of site:S-108 to be a number its shape could be compared against, " +
					"found a text value",
			},
		},
		{
			name:     "holds of a length written on a run of wall which is not a ring",
			instance: "site:W-01",
			expected: nil,
		},
		{
			name:     "reports a length which no longer matches the run it describes",
			instance: "site:W-02",
			expected: []string{
				"expected the length claimed of site:W-02 to agree with the shape it is drawn as, found 6.0 m " +
					"claimed against 7.0 m measured, which is 1.0 m less than the shape",
			},
		},
		{
			name:     "holds of a span written on an edge which belongs to no loop",
			instance: "geom:E-20",
			expected: nil,
		},
		{
			name:     "reports a span longer than the corners it runs between, naming both of them",
			instance: "geom:E-21",
			expected: []string{
				"expected the length claimed of geom:E-21 to agree with the corners it runs between, geom:V-20 " +
					"and geom:V-21, found 4.5 m claimed against 4.0 m measured, which is 0.5 m more than the span",
			},
		},
		{
			name:     "reports a span shorter than the corners it runs between, which is the other mistake",
			instance: "geom:E-22",
			expected: []string{
				"expected the length claimed of geom:E-22 to agree with the corners it runs between, geom:V-20 " +
					"and geom:V-21, found 3.5 m claimed against 4.0 m measured, which is 0.5 m less than the span",
			},
		},
		{
			name:     "leaves an edge with nothing claimed under the predicate alone",
			instance: "geom:E-23",
			expected: nil,
		},
		{
			name:     "leaves an edge whose ends nobody has surveyed alone",
			instance: "geom:E-24",
			expected: nil,
		},
		{
			name:     "compares the span which replaced a retracted one rather than the retracted one",
			instance: "geom:E-25",
			expected: nil,
		},
		{
			name:     "holds where a span and its corners differ by less than what the evidence can tell apart",
			instance: "geom:E-26",
			expected: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected,
				reportedBy(run, "claim-agrees-with-geometry", testCase.instance))
		})
	}

	t.Run("decides every rule the fixture states either way", func(t *testing.T) {
		assert.Equal(t, 19, run.Rules)
		assert.Equal(t, run.Rules, run.Ran)
		assert.Equal(t, 7, run.Failed)
		assert.Equal(t, 12, run.Passed)
	})

	t.Run("says what to do about each of them", func(t *testing.T) {
		for _, violation := range run.Violations {
			assert.NotEmpty(t, violation.Hint, violation.Instance)
		}
	})
}

// TestEveryCheckWhichWidensAToleranceReportsTheBand is the guard behind every
// case below it.
//
// A check which treats its declared tolerance as a floor and does not implement
// [Judge] still decides correctly and discloses nothing, which is exactly the
// state this story was written about — and it is a state no assertion about a
// message would catch, because the message would be right. So the set is named
// here: adding a check which widens without reporting fails this, and taking the
// reporting off an existing one fails it too.
func TestEveryCheckWhichWidensAToleranceReportsTheBand(t *testing.T) {
	widening := []Runner{
		claimAgreesWithGeometry{},
		containedAreasSum{},
		sitsInside{},
	}

	for _, check := range widening {
		t.Run(check.Declare().Name, func(t *testing.T) {
			_, reports := check.(Judge)

			assert.True(t, reports, "a check which widens the tolerance it is given says what it applied")
		})
	}
}

// bandOf is the one band a check reported about one thing, and fails the test
// where the run reports any other number of them.
//
// A check which decided one comparison and reported two bands, or none, is a
// check whose disclosure does not match the answer it gave — which is the whole
// of what these cases are about, so it is a failure here rather than an index
// out of range further down.
func bandOf(t *testing.T, run CheckRun, check string, instance ID) Band {
	t.Helper()

	var out []Band
	for _, applied := range run.Bands {
		if applied.Check != check || applied.Instance != instance {
			continue
		}
		out = append(out, applied.Band)
	}

	require.Len(t, out, 1, "%s reports one band about %s", check, instance)
	return out[0]
}

// TestClaimAgreesWithGeometryReportsTheBandItApplied is its own function because
// its assertion is about the answer's disclosure rather than about the answer.
//
// The check widens the tolerance it is given by the combined uncertainty of what
// it compares, which is right and is invisible. On the fixture below a 0.05 m²
// discrepancy is applied as 0.27 — the corners are known to 0.008 m and the
// boundary is 14 m long, so their contribution alone is 0.112 m² — and the run
// which passes site:S-107 reads exactly like one which held to the declared
// figure. A rule nobody can falsify is the failure this reports.
func TestClaimAgreesWithGeometryReportsTheBandItApplied(t *testing.T) {
	run := runCheckFixture(t, "agreement")

	testCases := []struct {
		name     string
		instance ID
		expected Band
	}{
		{
			name:     "reports the band behind a pass, where nothing in the answer would otherwise say it",
			instance: "site:S-101",
			expected: Band{
				Tolerance:  "area-discrepancy",
				Floor:      0.05,
				Applied:    0.12265398485169571,
				Unit:       "m2",
				Difference: 0,
				Widened:    true,
				Terms: []BandTerm{
					{Source: BandFromClaim, Sigma: 0.05, Unit: "m2", Sensitivity: 1, Contribution: 0.05},
					{Source: BandFromCorners, Sigma: 0.008, Unit: "m", Sensitivity: 14, Contribution: 0.112},
				},
			},
		},
		{
			name:     "calls a pass which needed the widening decisive",
			instance: "site:S-107",
			expected: Band{
				Tolerance:  "area-discrepancy",
				Floor:      0.05,
				Applied:    0.2739415996156845,
				Unit:       "m2",
				Difference: 0.1999999999999993,
				Widened:    true,
				Decisive:   true,
				Terms: []BandTerm{
					{Source: BandFromClaim, Sigma: 0.25, Unit: "m2", Sensitivity: 1, Contribution: 0.25},
					{Source: BandFromCorners, Sigma: 0.008, Unit: "m", Sensitivity: 14, Contribution: 0.112},
				},
			},
		},
		{
			name:     "does not call a pass within the tolerance as written decisive",
			instance: "site:S-109",
			expected: Band{
				Tolerance:  "area-discrepancy",
				Floor:      0.05,
				Applied:    0.112,
				Unit:       "m2",
				Difference: 0.019999999999999574,
				Widened:    true,
				Terms: []BandTerm{
					{Source: BandFromCorners, Sigma: 0.008, Unit: "m", Sensitivity: 14, Contribution: 0.112},
				},
			},
		},
		{
			name:     "reports the band behind a failure too, which is what it was measured past",
			instance: "site:S-102",
			expected: Band{
				Tolerance:  "area-discrepancy",
				Floor:      0.05,
				Applied:    0.1501465950329877,
				Unit:       "m2",
				Difference: 2,
				Widened:    true,
				Terms: []BandTerm{
					{Source: BandFromClaim, Sigma: 0.1, Unit: "m2", Sensitivity: 1, Contribution: 0.1},
					{Source: BandFromCorners, Sigma: 0.008, Unit: "m", Sensitivity: 14, Contribution: 0.112},
				},
			},
		},
		{
			name:     "carries a span's corners across at a sensitivity of one, because a length is not an area",
			instance: "geom:E-20",
			expected: Band{
				Tolerance:  "length-discrepancy",
				Floor:      0.01,
				Applied:    0.01,
				Unit:       "m",
				Difference: 0,
				Terms: []BandTerm{
					{Source: BandFromClaim, Sigma: 0.005, Unit: "m", Sensitivity: 1, Contribution: 0.005},
					{
						Source:       BandFromCorners,
						Sigma:        0.00565685424949238,
						Unit:         "m",
						Sensitivity:  1,
						Contribution: 0.00565685424949238,
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := bandOf(t, run, "claim-agrees-with-geometry", testCase.instance)

			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestAComparisonItDeclinedToMakeReportsNoBand covers the other half of the
// disclosure: a subject this check leaves alone.
//
// A band about a comparison nobody made would be a number a reader could act on
// and a test which never ran, which is the same unfalsifiable answer the band
// exists to prevent — read the other way round.
func TestAComparisonItDeclinedToMakeReportsNoBand(t *testing.T) {
	run := runCheckFixture(t, "agreement")

	declined := []ID{
		"site:S-104", // measured and not yet drawn
		"site:S-105", // drawn and not yet measured
		"geom:E-23",  // a span with no number written on it
		"geom:E-24",  // a number with no surveyed corners to measure it against
	}

	for _, instance := range declined {
		t.Run(string(instance), func(t *testing.T) {
			for _, applied := range run.Bands {
				assert.NotEqual(t, instance, applied.Instance,
					"a comparison which was not made discloses nothing")
			}
		})
	}
}

// TestABandLeadsBackToTheRuleWhichAppliedIt covers what a band carries besides
// the arithmetic.
//
// A tolerance stated once in a registry and widened on a hundred and fifty
// instances is one whose band has to lead back to the single line the floor is
// declared on, and to the thing the comparison was about. Otherwise the answer
// is a number with nowhere to go.
func TestABandLeadsBackToTheRuleWhichAppliedIt(t *testing.T) {
	graph := loadCheckFixture(t, "agreement")

	rules := graph.Rules().Select(RuleFilter{Subjects: []ID{"site:S-107"}})
	require.Len(t, rules, 1)

	bands, violations := rules[0].Judge()
	require.Empty(t, violations, "site:S-107 agrees, once the band is widened")
	require.Len(t, bands, 1)

	applied := bands[0]

	assert.Equal(t, ID("site:S-107"), applied.Instance)
	assert.Equal(t, "claim-agrees-with-geometry", applied.Check)
	assert.Empty(t, applied.Type, "an assertion is declared on the thing itself")
	assert.Equal(t, []string{
		"(predicate area)",
		"(position position)",
		"(tolerance boundary-closure)",
		"(discrepancy area-discrepancy)",
	}, applied.Arguments)
	assert.Equal(t, rules[0].Declared, applied.Declared, "the band points at the rule which applied it")

	room, held := graph.Node("site:S-107")
	require.True(t, held)
	assert.Equal(t, room.Span(), applied.Subject, "and at the thing the comparison was about")
}

// TestClaimAgreesWithGeometryLeadsBackToBothPlaces is its own function because
// its assertion is about the spans rather than the messages.
//
// A stale number and the boundary it stopped matching are two lines, and either
// of them may be the one to change. A failure which pointed only at the node
// would send the reader to the form and leave them to find which of the two it
// was about.
func TestClaimAgreesWithGeometryLeadsBackToBothPlaces(t *testing.T) {
	graph := loadCheckFixture(t, "agreement")

	room, held := graph.Node("site:S-102")
	require.True(t, held)

	var violation Violation
	for _, found := range graph.Rules().Select(RuleFilter{Subjects: []ID{"site:S-102"}}).Run().Violations {
		violation = found
	}
	require.NotEmpty(t, violation.Check)

	claim, resolved := graph.Claims().Resolve("site:S-102", "area", graph.Registry())
	require.NoError(t, resolved)
	current, ok := claim.Claim()
	require.True(t, ok)

	assert.Equal(t, current.Span(), violation.Subject, "the failure points at the claim which disagrees")
	assert.NotEqual(t, room.Span(), violation.Subject)

	require.Len(t, violation.Related, 1)
	assert.NotEqual(t, violation.Subject, violation.Related[0].Span,
		"the boundary it was compared against is a different place from the claim")
}

// TestClaimAgreesWithGeometryOnAnEdgeLeadsBackToBothCorners is its own function
// for the same reason as the one above, and about a different pair of places.
//
// A span which no longer matches the corners it runs between is either a stale
// number or an end which moved, and which of the two ends moved is what the
// reader is about to go and find out. A failure which pointed at the edge would
// name neither.
func TestClaimAgreesWithGeometryOnAnEdgeLeadsBackToBothCorners(t *testing.T) {
	graph := loadCheckFixture(t, "agreement")

	edge, held := graph.Topology().Edge("geom:E-21")
	require.True(t, held)

	var violation Violation
	for _, found := range graph.Rules().Select(RuleFilter{Subjects: []ID{"geom:E-21"}}).Run().Violations {
		violation = found
	}
	require.NotEmpty(t, violation.Check)

	resolution, resolved := graph.Claims().Resolve("geom:E-21", "length", graph.Registry())
	require.NoError(t, resolved)
	current, ok := resolution.Claim()
	require.True(t, ok)

	assert.Equal(t, current.Span(), violation.Subject, "the failure points at the span which disagrees")
	assert.NotEqual(t, edge.Span(), violation.Subject)

	require.Len(t, violation.Related, 2, "both corners the span was measured between")
	assert.NotEqual(t, violation.Related[0].Span, violation.Related[1].Span)
	for _, related := range violation.Related {
		assert.NotEqual(t, violation.Subject, related.Span,
			"a corner it was compared against is a different place from the claim")
	}
}

// TestEdgeBackingResolvesReportsEveryReferenceWhichReachesNoElement is its own
// function because its fixture does not load clean, and that is what it is for.
//
// A backing reference which reaches nothing is a load error: an edge which says
// something realises it and names nothing the model holds is not quietly virtual
// (specification section 6.3). The rule says the same thing from the other side —
// on the edge, in the vocabulary of a gate, once per reference — which is what a
// run of the model's own rules reports rather than what reading the files does.
func TestEdgeBackingResolvesReportsEveryReferenceWhichReachesNoElement(t *testing.T) {
	graph, diags := LoadGraph(checkFixture("backing"))
	require.NotNil(t, graph)
	assert.Len(t, diags, 2, "the load reports each reference which reached no element")

	run := graph.Rules().Run()

	assert.Equal(t, []string{
		"expected a node of kind Element, found site:S-101, which is a Space",
		"expected an element id something in this model holds, found site:W-99, which names no node",
	}, reportedBy(run, "edge-backing-resolves", "geom:E-01"))

	// One rule, failed once, whatever it found: a summary counts rules rather
	// than violations, because an edge which names two elements it cannot reach
	// breaks one rule twice rather than two rules.
	assert.Equal(t, 1, run.Rules)
	assert.Equal(t, 1, run.Failed)

	t.Run("points at each reference rather than at the edge which wrote them", func(t *testing.T) {
		violations := run.Violations
		require.Len(t, violations, 2)

		assert.NotEqual(t, violations[0].Subject, violations[1].Subject)
		for _, violation := range violations {
			assert.NotEqual(t, violation.Declared, violation.Subject,
				"where the rule is written and where it failed are different places")
		}
	})
}

// TestAViolationOfTheCheckSetLeadsBackToBothPlaces is its own function because
// it is about the spans rather than the messages: a rule stated in one place and
// broken in another has to lead a reader to each of them.
func TestAViolationOfTheCheckSetLeadsBackToBothPlaces(t *testing.T) {
	run := runCheckFixture(t, "violating")

	var overlap Violation
	for _, violation := range run.Violations {
		if violation.Check == "contained-areas-do-not-overlap" {
			overlap = violation
		}
	}
	require.NotEmpty(t, overlap.Check, "the fixture reports an overlap")

	// The rule is written on the storey and what it found is a room drawn over
	// another, so the two spans are different lines of the same file.
	assert.Equal(t, "model.dfc", filepath.Base(overlap.Subject.Start.Path))
	assert.Equal(t, "model.dfc", filepath.Base(overlap.Declared.Start.Path))
	assert.NotEqual(t, overlap.Declared, overlap.Subject)

	// The other room is named too. A failure about a pair which pointed at one
	// of them would send the reader to the room which may well be right.
	require.Len(t, overlap.Related, 1)
	assert.NotEqual(t, overlap.Subject, overlap.Related[0].Span)

	rendered := overlap.Diagnostic()
	assert.Equal(t, SeverityError, rendered.Severity)
	assert.Contains(t, rendered.Message, "expected site:L-01 to satisfy the assertion contained-areas-do-not-overlap")
	assert.Len(t, rendered.Related, 2, "the other room, and where the rule is written")
}

// TestEveryRegisteredCheckTakesRegistryDataRatherThanNumbers is its own function
// because it is an assertion about the registry as a whole rather than about any
// check in it: how close is close enough is a decision a project writes down
// once, and a check which took a number would move it to the point of use.
func TestEveryRegisteredCheckTakesRegistryDataRatherThanNumbers(t *testing.T) {
	for _, declared := range Checks() {
		t.Run(declared.Name, func(t *testing.T) {
			assert.NotEmpty(t, declared.Description)

			for _, parameter := range declared.Parameters {
				assert.NotEmpty(t, parameter.Description, parameter.Name)
				assert.NotEqual(t, ParameterReal, parameter.Type,
					"%s takes a number, which is how a tolerance stops being registry data", parameter.Name)
			}
		})
	}
}

// TestGroundToGridStatedWantsAnAffirmation covers the one check whose passing
// condition is that somebody said something rather than that nothing is wrong.
//
// The four fixtures are four states of one model which differ in one line each,
// and the difference between the first two is the whole point of the rule: a
// chain whose every transform is a scale of one and a chain whose factor was
// determined to be one are the same geometry and the opposite answer. One is a
// project which decided; the other is a project which has not looked.
func TestGroundToGridStatedWantsAnAffirmation(t *testing.T) {
	testCases := []struct {
		name     string
		fixture  string
		expected []string
	}{
		{
			name:    "reports a chain rooted at a projection which says nothing about ground and grid",
			fixture: "grid/silent",
			expected: []string{
				"expected the chain site:S-01 is measured in, rooted at frame:survey-grid under EPSG:25831, to " +
					"state whether a ground distance is a grid distance, found the one transform to it at a scale " +
					"of exactly 1.0 and nothing written under ground-to-grid",
			},
		},
		{
			name:     "holds where a claim states the factor, which is the affirmation the silence is missing",
			fixture:  "grid/affirmed",
			expected: nil,
		},
		{
			name:     "holds where a transform on the chain already carries the factor",
			fixture:  "grid/stated",
			expected: nil,
		},
		{
			name:     "does not apply to a model rooted at no projection, where the two are the same distance",
			fixture:  "grid/unnamed",
			expected: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			run := runCheckFixture(t, testCase.fixture)

			assert.Equal(t, testCase.expected, reportedBy(run, "ground-to-grid-stated", "site:S-01"))
		})
	}
}

// TestGroundToGridStatedSizesTheSilence is its own function because it asserts
// the hint rather than the message, and because the figure in it is the reason
// the check exists.
//
// A factor nobody stated is an abstraction until it is multiplied by how far the
// model reaches. The plot is four hundred metres by three hundred, so it is five
// hundred corner to corner, and every part per million of unstated factor is
// half a millimetre across it — which puts the hundred parts per million these
// systems are designed to that the reader has to look up at fifty millimetres,
// on this model, without the engine having an opinion about geodesy.
func TestGroundToGridStatedSizesTheSilence(t *testing.T) {
	run := runCheckFixture(t, "grid/silent")

	var reported Violation
	for _, violation := range run.Violations {
		if violation.Check == "ground-to-grid-stated" {
			reported = violation
		}
	}
	require.NotEmpty(t, reported.Check, "the fixture reports the silence")

	assert.Equal(t,
		"the model spans 500.0 m between its furthest corners, so every part per million of unstated factor is "+
			"0.0005 m across it; the factor is the grid scale factor times the elevation factor and depends on the "+
			"project's height, which no coordinate reference system carries, so nothing here derives it: state it "+
			"as a claim under ground-to-grid, or carry it on the transform which georeferences the model",
		reported.Hint)

	// Both places a reader has to see to act on it: the identifier which says
	// the model is in a projection at all, and the frame that projection is the
	// root of. Neither is where the rule was written, which is Declared.
	require.Len(t, reported.Related, 2)
	assert.Equal(t, "registry.dfc", filepath.Base(reported.Related[0].Span.Start.Path))
	assert.Equal(t, "registry.dfc", filepath.Base(reported.Related[1].Span.Start.Path))
	assert.NotEqual(t, reported.Related[0].Span, reported.Related[1].Span)
}

// TestSitsInside is its own function because its fixture varies the form of the
// subject rather than what is written about one form.
//
// A device is a point, a partition is a run of corners and a floor plate is an
// outline, and the rule says one thing about all three: nothing of it reaches
// past the boundary of the thing it is checked against. Each of those is read
// out of the model differently, so a table over one fixture is what shows the
// three answering the same question rather than three rules under one name.
func TestSitsInside(t *testing.T) {
	run := runCheckFixture(t, "inside")

	testCases := []struct {
		name     string
		instance ID
		expected []string
	}{
		{
			name:     "holds of an outline wholly inside the one it is checked against",
			instance: "site:S-101",
			expected: nil,
		},
		{
			name:     "reports an outline reaching past its container, naming how much is out and how far",
			instance: "site:S-102",
			expected: []string{
				"expected site:S-102 to sit inside site:L-01, found 4.0 m² of it outside, reaching 3.0 m past " +
					"the boundary at (13.0 1.0 0.0)",
			},
		},
		{
			name:     "holds of a floor plate which agrees with the outline the survey recorded",
			instance: "site:L-01",
			expected: nil,
		},
		{
			name:     "reports a floor plate which disagrees with the outline the survey recorded",
			instance: "site:L-02",
			expected: []string{
				"expected site:L-02 to sit inside site:O-01, found 6.0 m² of it outside, reaching 2.0 m past " +
					"the boundary at (13.0 0.0 0.0)",
			},
		},
		{
			name:     "holds of a run of wall drawn inside the plate it runs across",
			instance: "site:W-01",
			expected: nil,
		},
		{
			name:     "reports a run of wall which leaves the plate, naming the corner which is furthest out",
			instance: "site:W-02",
			expected: []string{
				"expected site:W-02 to sit inside site:L-01, found geom:V-63 3.0 m outside the boundary, " +
					"at (13.0 5.5 0.0)",
			},
		},
		{
			name:     "holds of a device inside the footprint and well above its plane",
			instance: "site:D-01",
			expected: nil,
		},
		{
			name:     "reports a device set out past the footprint it is written within",
			instance: "site:D-02",
			expected: []string{
				"expected site:D-02 to sit inside site:L-01, found it 2.0 m outside the boundary, at (12.0 2.0 0.0)",
			},
		},
		{
			name:     "reports a device nobody has set out rather than passing it",
			instance: "site:D-03",
			expected: []string{
				"expected a position claimed of site:D-03 under position to judge against the container " +
					"site:L-01, found none",
			},
		},
		{
			name:     "refuses a position written in one frame against an outline drawn in another",
			instance: "site:D-04",
			expected: []string{
				"expected site:D-04 to be declared in frame:building, the frame the container site:L-01 is " +
					"drawn in, found frame:annex",
			},
		},
		{
			name:     "holds where a thing is outside by less than the accuracy which put it there",
			instance: "site:D-05",
			expected: nil,
		},
		{
			name:     "reports the same distance where the accuracy is narrow enough to tell it from nothing",
			instance: "site:D-06",
			expected: []string{
				"expected site:D-06 to sit inside site:L-01, found it 0.25 m outside the boundary, at (10.25 5.0 0.0)",
			},
		},
		{
			name:     "names every corner of a run nobody has surveyed rather than the first of them",
			instance: "site:W-03",
			expected: []string{
				"expected a position claimed of geom:V-71 under position to judge against the container " +
					"site:L-01, found none",
				"expected a position claimed of geom:V-72 under position to judge against the container " +
					"site:L-01, found none",
				"expected a position claimed of geom:V-73 under position to judge against the container " +
					"site:L-01, found none",
			},
		},
		{
			name:     "reports a subject whose type declares an area and which nobody has drawn",
			instance: "site:S-190",
			expected: []string{
				"expected a shape on site:S-190 to judge against the container site:L-01, found no loop bounding it",
			},
		},
		{
			name:     "reports a subject whose type declares a line and which nobody has drawn",
			instance: "site:W-90",
			expected: []string{
				"expected a shape on site:W-90 to judge against the container site:L-01, found no edge drawing it",
			},
		},
		{
			name:     "reports a container which encloses nothing to be inside of",
			instance: "site:D-08",
			expected: []string{
				"expected the container site:O-90 to have a shape to sit inside, found no loop bounding it",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, reportedBy(run, "sits-inside", testCase.instance))
		})
	}

	t.Run("decides every rule the fixture states either way", func(t *testing.T) {
		assert.Equal(t, 16, run.Rules)
		assert.Equal(t, run.Rules, run.Ran)
		assert.Equal(t, 11, run.Failed)
		assert.Equal(t, 5, run.Passed)
	})

	t.Run("says what to do about each of them", func(t *testing.T) {
		for _, violation := range run.Violations {
			assert.NotEmpty(t, violation.Hint, violation.Instance)
		}
	})
}

// TestSitsInsideJudgesAShapeItIsNotWrittenWithin is its own function because
// what it asserts is a property of the graph rather than of a message.
//
// The container is a parameter and never the containment parent, and the second
// story wanting this check is exactly the case where the two differ: a floor
// plate is checked against the outline a boundary survey recorded, and it is
// written within the building rather than within the survey. A rule which
// reached for the parent could not say that at all.
func TestSitsInsideJudgesAShapeItIsNotWrittenWithin(t *testing.T) {
	graph := loadCheckFixture(t, "inside")

	plate, held := graph.Node("site:L-02")
	require.True(t, held)

	parent, written := plate.Within()
	require.True(t, written)
	assert.Equal(t, ID("site:B-01"), parent, "the plate is written within the building")

	outline, held := graph.Node("site:O-01")
	require.True(t, held)

	run := graph.Rules().Select(RuleFilter{Subjects: []ID{"site:L-02"}}).Run()

	require.Len(t, run.Violations, 1)
	require.Len(t, run.Violations[0].Related, 1)
	assert.True(t, encloses(outline.Span(), run.Violations[0].Related[0].Span),
		"and is judged against the surveyed outline, which is not its parent")
}

// TestSitsInsideLeadsBackToWhatToMove is its own function because its assertion
// is about the spans rather than the messages.
//
// Two places are always involved and either may be the one to change: the thing
// which is outside, and the shape it is outside of. A failure which pointed
// only at the node would leave a reader with a wall of nine corners to find the
// one which is over the line.
func TestSitsInsideLeadsBackToWhatToMove(t *testing.T) {
	graph := loadCheckFixture(t, "inside")

	corner, held := graph.Topology().Vertex("geom:V-63")
	require.True(t, held)

	run := graph.Rules().Select(RuleFilter{Subjects: []ID{"site:W-02"}}).Run()
	require.Len(t, run.Violations, 1)

	wall, held := graph.Node("site:W-02")
	require.True(t, held)

	assert.True(t, encloses(corner.Span(), run.Violations[0].Subject),
		"the failure points at the corner which is outside")
	assert.False(t, encloses(wall.Span(), run.Violations[0].Subject),
		"rather than at the wall the corner belongs to")

	require.Len(t, run.Violations[0].Related, 1)
	assert.NotEqual(t, run.Violations[0].Subject, run.Violations[0].Related[0].Span,
		"the shape it is outside of is a different place from the corner")
}

// encloses reports whether one span lies inside another, which is how a test
// says that a failure points somewhere within a form without restating the
// arithmetic that produced the span.
func encloses(outer, inner Span) bool {
	return outer.Start.Path == inner.Start.Path &&
		outer.Start.Offset <= inner.Start.Offset &&
		inner.End.Offset <= outer.End.Offset
}

// TestSitsInsideReportsTheBandItApplied covers the second place a declared
// tolerance is a floor, and the disclosure it owes for the same reason.
//
// The tolerance is 0.005 m and two shapes surveyed to 0.008 m apiece put the
// band at 0.011 — twice the figure the registry states, before anything unusual
// has happened. What the fixture also holds is site:D-05, a device set out by a
// method good to half a metre: the same rule then permits a quarter-metre
// excursion past the boundary, and passes.
func TestSitsInsideReportsTheBandItApplied(t *testing.T) {
	run := runCheckFixture(t, "inside")

	testCases := []struct {
		name     string
		instance ID
		expected Band
	}{
		{
			name:     "reports a band on a shape the container covers entirely, where the reach past it is nothing",
			instance: "site:S-101",
			expected: Band{
				Tolerance:  "boundary-closure",
				Floor:      0.005,
				Applied:    0.01131370849898476,
				Unit:       "m",
				Difference: 0,
				Widened:    true,
				Terms: []BandTerm{
					{Source: BandFromCorners, Sigma: 0.008, Unit: "m", Sensitivity: 1, Contribution: 0.008},
					{Source: BandFromContainer, Sigma: 0.008, Unit: "m", Sensitivity: 1, Contribution: 0.008},
				},
			},
		},
		{
			name:     "names both shapes, because either survey can be the one which widened it",
			instance: "site:W-01",
			expected: Band{
				Tolerance:  "boundary-closure",
				Floor:      0.005,
				Applied:    0.010583005244258363,
				Unit:       "m",
				Difference: 0,
				Widened:    true,
				Terms: []BandTerm{
					{
						Source:       BandFromCorners,
						Sigma:        0.0069282032302755096,
						Unit:         "m",
						Sensitivity:  1,
						Contribution: 0.0069282032302755096,
					},
					{Source: BandFromContainer, Sigma: 0.008, Unit: "m", Sensitivity: 1, Contribution: 0.008},
				},
			},
		},
		{
			name:     "calls a placement which is only inside because its own survey is loose decisive",
			instance: "site:D-05",
			expected: Band{
				Tolerance:  "boundary-closure",
				Floor:      0.005,
				Applied:    0.5000639959045242,
				Unit:       "m",
				Difference: 0.25,
				Widened:    true,
				Decisive:   true,
				Terms: []BandTerm{
					{Source: BandFromCorners, Sigma: 0.5, Unit: "m", Sensitivity: 1, Contribution: 0.5},
					{Source: BandFromContainer, Sigma: 0.008, Unit: "m", Sensitivity: 1, Contribution: 0.008},
				},
			},
		},
		{
			name:     "reports the band behind a failure too, which is what the reach was measured past",
			instance: "site:D-02",
			expected: Band{
				Tolerance:  "boundary-closure",
				Floor:      0.005,
				Applied:    0.008944271909999158,
				Unit:       "m",
				Difference: 2,
				Widened:    true,
				Terms: []BandTerm{
					{Source: BandFromCorners, Sigma: 0.004, Unit: "m", Sensitivity: 1, Contribution: 0.004},
					{Source: BandFromContainer, Sigma: 0.008, Unit: "m", Sensitivity: 1, Contribution: 0.008},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := bandOf(t, run, "sits-inside", testCase.instance)

			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestContainedAreasSumReportsTheBandItApplied covers the third place a declared
// tolerance is a floor.
//
// Only the figure summed against widens this one — a sum of many regions has no
// single sensitivity to carry a corner budget across by — so a total judged
// against a subject's own outline reports a band with no term at all, and the
// declared tolerance is the whole of the test. That is worth reporting as much
// as a widened one: it is the answer which says the criterion held as written.
func TestContainedAreasSumReportsTheBandItApplied(t *testing.T) {
	run := runCheckFixture(t, "appraised")

	testCases := []struct {
		name     string
		instance ID
		expected Band
	}{
		{
			name:     "reports a band with no term where the total is judged against the subject's own outline",
			instance: "site:L-07",
			expected: Band{
				Tolerance:  "area-sum",
				Floor:      0.05,
				Applied:    0.05,
				Unit:       "m2",
				Difference: 32,
			},
		},
		{
			name:     "calls a total which is only inside because the figure says so decisive",
			instance: "site:L-08",
			expected: Band{
				Tolerance:  "area-sum",
				Floor:      0.05,
				Applied:    0.3,
				Unit:       "m2",
				Difference: 0.20000000000000284,
				Widened:    true,
				Decisive:   true,
				Terms: []BandTerm{
					{Source: BandFromClaim, Sigma: 0.3, Unit: "m2", Sensitivity: 1, Contribution: 0.3},
				},
			},
		},
		{
			name:     "reports the band behind a failure too, which is what the discrepancy was measured past",
			instance: "site:L-09",
			expected: Band{
				Tolerance:  "area-sum",
				Floor:      0.05,
				Applied:    0.3,
				Unit:       "m2",
				Difference: 1,
				Widened:    true,
				Terms: []BandTerm{
					{Source: BandFromClaim, Sigma: 0.3, Unit: "m2", Sensitivity: 1, Contribution: 0.3},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := bandOf(t, run, "contained-areas-sum", testCase.instance)

			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestBoundaryLoopsCloseDoesNotAskARunToClose is its own function because it
// asserts that a check declines to run rather than that it ran and passed, and
// because the difference between those two is the whole of what a node drawn as
// a line needed.
func TestBoundaryLoopsCloseDoesNotAskARunToClose(t *testing.T) {
	graph := loadCheckFixture(t, "satisfied")

	loop, ok := graph.Topology().Loop("geom:L-15")
	require.True(t, ok, "the fixture holds the partition's run")

	t.Run("passes over a loop every node bounded by it draws as a line", func(t *testing.T) {
		// The run is a single edge and does not come back to where it began, so
		// a check which read it as a ring would report a gap the length of the
		// partition.
		assembly, _ := graph.Topology().Assemble(loop, nil, "boundary-closure", graph.Registry())
		require.False(t, assembly.Closed(), "read as a ring, this loop does not close")

		run := graph.Rules().Run()
		assert.Empty(t, reportedBy(run, "boundary-loops-close", "geom:L-15"))
	})

	t.Run("still asks a loop a room is bounded by to close", func(t *testing.T) {
		store, ok := graph.Topology().Loop("geom:L-13")
		require.True(t, ok)

		assert.False(t, walkedAsRun(graph, store), "a room's outline is a ring whoever asks")
		assert.True(t, walkedAsRun(graph, loop))
	})
}

// TestContainedAreasSumAgainstAStatedFigure is its own function because its
// fixture holds the geometry still and varies only what the total is compared
// with and which of the contents go into it.
//
// Every storey below is drawn from the same four outlines — a plate of eighty
// square metres cut into forty-eight, twelve and twenty — so a case which passes
// and a case which fails differ in the figure and the set and in nothing else.
// That is the question the check learned to ask: an appraisal states the living
// area of a floor, the floor's outline is the living rooms plus the garage, and
// a garage is a Space in the same way a bedroom is.
func TestContainedAreasSumAgainstAStatedFigure(t *testing.T) {
	run := runCheckFixture(t, "appraised")

	testCases := []struct {
		name     string
		instance ID
		expected []string
	}{
		{
			name:     "holds where the living rooms come to the figure and everything comes to the outline",
			instance: "site:L-01",
			expected: nil,
		},
		{
			name:     "reports a stale figure against the rooms it was written about, not against the outline",
			instance: "site:L-02",
			expected: []string{
				"expected what site:L-02 contains to add up to the gross-living-area claimed of it, 72.0 m², " +
					"found 60.0 m², which is 12.0 m² less than the figure",
			},
		},
		{
			name:     "leaves a subject nobody has stated the figure of yet alone",
			instance: "site:L-03",
			expected: nil,
		},
		{
			name:     "reports a predicate carrying prose, which no set of areas adds up to",
			instance: "site:L-04",
			expected: []string{
				"expected the remark claimed of site:L-04 to be a number its contents could be summed against, " +
					"found a text value",
			},
		},
		{
			name:     "reports a figure written in a length against areas measured in its square",
			instance: "site:L-05",
			expected: []string{
				"expected the frontage claimed of site:L-05 in m2, the square of the unit it is drawn in, found 10.0 m",
			},
		},
		{
			name:     "reports a narrowing to the members of something which is not a zone",
			instance: "site:L-06",
			expected: []string{
				"expected a node of kind Zone, found site:L-01, which is a Storey",
			},
		},
		{
			name:     "narrows a sum against the subject's own outline, which the narrowed set no longer fills",
			instance: "site:L-07",
			expected: []string{
				"expected what site:L-07 contains to add up to its own 80.0 m², found 48.0 m², which is " +
					"32.0 m² less than the whole",
			},
		},
		{
			name:     "holds where the figure and the rooms differ by less than the figure says it is known to",
			instance: "site:L-08",
			expected: nil,
		},
		{
			name:     "reports a disagreement wider than the accuracy the figure states of itself",
			instance: "site:L-09",
			expected: []string{
				"expected what site:L-09 contains to add up to the gross-living-area claimed of it, 61.0 m², " +
					"found 60.0 m², which is 1.0 m² less than the figure",
			},
		},
		{
			name:     "reports a narrowed total without counting a content which covers nothing",
			instance: "site:L-10",
			expected: []string{
				"expected what site:L-10 contains to add up to its own 80.0 m², found 48.0 m², which is " +
					"32.0 m² less than the whole",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := reportedBy(run, "contained-areas-sum", testCase.instance)

			assert.Equal(t, testCase.expected, got)
		})
	}

	t.Run("says what to do about each of them", func(t *testing.T) {
		for _, violation := range run.Violations {
			assert.NotEmpty(t, violation.Hint, "%s on %s", violation.Check, violation.Instance)
		}
	})

	t.Run("runs every rule the model states and fails the seven which are broken", func(t *testing.T) {
		assert.Equal(t, 12, run.Rules)
		assert.Equal(t, 12, run.Ran, "every check the fixture names has an implementation")
		assert.Equal(t, 7, run.Failed)
		assert.Equal(t, 5, run.Passed)
	})

	t.Run("names the same set whether it is named by type or by the zone which lists it", func(t *testing.T) {
		// The main floor states the rule twice over the same three rooms, once
		// narrowing by type and once by membership of the zone the appraisal
		// counts. Both come to sixty and both agree with the figure, which is
		// what says the two narrowings are two spellings of one set rather than
		// two answers.
		var narrowed int
		for _, rule := range loadCheckFixture(t, "appraised").Rules().Select(RuleFilter{Subjects: []ID{"site:L-01"}}) {
			for _, argument := range rule.Arguments {
				if argument.Name != "type" && argument.Name != "member-of" {
					continue
				}

				narrowed++
				assert.Empty(t, rule.Run(), argument.String())
			}
		}

		assert.Equal(t, 2, narrowed, "the fixture states the rule once per spelling")
	})
}

// TestContainedAreasSumSaysWhatASubsetSummed is its own function because its
// assertion is about the related locations rather than about the message.
//
// A subset total is the one answer this check cannot tell apart from a correct
// one by arithmetic: a living-area figure which agrees because the garage was
// dropped and one which agrees because a bedroom was dropped and a closet
// counted twice are the same number. What was summed and what was not is the
// only evidence there is, so a narrowed sum carries both halves of it and an
// unnarrowed sum carries neither.
func TestContainedAreasSumSaysWhatASubsetSummed(t *testing.T) {
	t.Run("names every node it summed and every node the narrowing left out", func(t *testing.T) {
		graph := loadCheckFixture(t, "appraised")

		violations := graph.Rules().Select(RuleFilter{Subjects: []ID{"site:L-02"}}).Run().Violations
		require.Len(t, violations, 1)

		claim, resolved := graph.Claims().Resolve("site:L-02", "gross-living-area", graph.Registry())
		require.NoError(t, resolved)
		figure, stated := claim.Claim()
		require.True(t, stated)

		expected := []RelatedLocation{
			{Span: figure.Span(), Message: "the figure it is summed against is claimed here"},
			{Span: namedSpanOf(t, graph, "site:S-201"), Message: "summed into the total"},
			{Span: namedSpanOf(t, graph, "site:S-202"), Message: "summed into the total"},
			{
				Span:    namedSpanOf(t, graph, "site:S-203"),
				Message: "left out of the sum: it is of type Garage and the sum is of type LivingSpace",
			},
		}

		assert.Equal(t, expected, violations[0].Related)
	})

	t.Run("counts both halves in the hint, so the composition reads without the spans", func(t *testing.T) {
		run := runCheckFixture(t, "appraised")

		var hint string
		for _, violation := range run.Violations {
			if violation.Instance == "site:L-07" {
				hint = violation.Hint
			}
		}

		assert.Equal(t,
			"the sum is of the 1 node it contains with a shape, narrowed to those of type LivingSpace and "+
				"leaving 1 node out, judged against the tolerance area-sum, which is 0.05 m2; either a part is "+
				"drawn wrong or the whole is",
			hint,
			"a set of one and a set of many are described by the same sentence",
		)
	})

	t.Run("leaves a content which covers nothing out of both halves", func(t *testing.T) {
		// The circuit group is written within the workshop floor and has no
		// outline. It is not in the sum, and it is not in what the narrowing left
		// out either: it was left out by having nothing to contribute rather than
		// by the rule, and listing it would send a reader to widen a narrowing
		// which is already summing everything there is.
		graph := loadCheckFixture(t, "appraised")

		violations := graph.Rules().Select(RuleFilter{Subjects: []ID{"site:L-10"}}).Run().Violations
		require.Len(t, violations, 1)

		expected := []RelatedLocation{
			{Span: namedSpanOf(t, graph, "site:S-1001"), Message: "summed into the total"},
			{
				Span:    namedSpanOf(t, graph, "site:S-1002"),
				Message: "left out of the sum: it is of type Garage and the sum is of type LivingSpace",
			},
		}

		assert.Equal(t, expected, violations[0].Related)
		assert.Contains(t, violations[0].Hint, "leaving 1 node out",
			"the node with no outline is counted in neither half")
	})

	t.Run("names the accuracy of the figure where that is what the total was judged against", func(t *testing.T) {
		// The band is the wider of the tolerance and how well the figure says it
		// is known. Naming the tolerance where the figure widened it would send a
		// reader to tighten a number which decided nothing.
		run := runCheckFixture(t, "appraised")

		var hint string
		for _, violation := range run.Violations {
			if violation.Instance == "site:L-09" {
				hint = violation.Hint
			}
		}

		assert.Equal(t,
			"the sum is of the 2 nodes it contains with a shape, narrowed to those of type LivingSpace and "+
				"leaving 1 node out, judged against 0.3 m2: the tolerance area-sum, which is 0.05 m2, widened "+
				"by how well the claim says it is known (0.3 m2); either a part is drawn wrong or the figure is",
			hint,
		)
	})

	t.Run("says none of it where the rule narrowed nothing", func(t *testing.T) {
		// The set is everything the subject contains, which the model already
		// says in one place. A related location per room would push the line
		// worth reading off the end of a list of them.
		graph := loadCheckFixture(t, "violating")

		var violation Violation
		for _, found := range graph.Rules().Select(RuleFilter{Subjects: []ID{"site:L-01"}}).Run().Violations {
			if found.Check == "contained-areas-sum" {
				violation = found
			}
		}

		require.NotEmpty(t, violation.Check)
		assert.Empty(t, violation.Related)
		assert.Contains(t, violation.Hint, "it contains with a shape,",
			"the unnarrowed hint says only that it summed what has a shape")
	})
}

// TestContainedAreasSumIsDeterministic is its own function because it asserts
// that two runs are the same rather than that either of them is right.
//
// The set summed and the set left out are both reported now, and both are read
// out of the containment index. A listing whose order came from a map would give
// a different answer on every run, and the diff of a check report would stop
// meaning anything.
func TestContainedAreasSumIsDeterministic(t *testing.T) {
	graph := loadCheckFixture(t, "appraised")

	first := graph.Rules().Run()
	second := graph.Rules().Run()

	require.NotEmpty(t, first.Violations)
	assert.Equal(t, first.Violations, second.Violations)

	t.Run("lists each half in the order the model writes it", func(t *testing.T) {
		storey, held := graph.Node("site:L-02")
		require.True(t, held)

		var summed, left []Span
		for related := range graph.Contains(storey) {
			at := graph.Nodes().named(related.Node())
			if related.Node().Type() == "LivingSpace" {
				summed = append(summed, at)
				continue
			}
			left = append(left, at)
		}
		require.NotEmpty(t, summed)
		require.NotEmpty(t, left)

		violations := graph.Rules().Select(RuleFilter{Subjects: []ID{"site:L-02"}}).Run().Violations
		require.Len(t, violations, 1)

		// The first related location is the claim the total was judged against;
		// the composition follows it, summed nodes before omitted ones.
		var listed []Span
		for _, related := range violations[0].Related[1:] {
			listed = append(listed, related.Span)
		}

		assert.Equal(t, append(slices.Clone(summed), left...), listed)
	})
}

// namedSpanOf is where the id of one node of the graph is written.
func namedSpanOf(t *testing.T, graph *Graph, id ID) Span {
	t.Helper()

	node, held := graph.Node(id)
	require.True(t, held, "the fixture holds %s", id)

	return graph.Nodes().named(node)
}
