// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"path/filepath"
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
