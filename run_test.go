// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadRuleFixture loads the model whose types carry invariants and whose things
// carry assertions, failing the test on any diagnostic: every case below is
// about what a well-formed model binds and what running it reports, so a
// diagnostic means the fixture says something other than what it was written to
// say.
func loadRuleFixture(t *testing.T, name string) *Graph {
	t.Helper()

	graph, diags := LoadGraph(filepath.Join("testdata", "rules", name))
	require.NotNil(t, graph, "a load always yields a usable graph")

	for _, diagnostic := range diags {
		t.Errorf("unexpected diagnostic: %s", diagnostic)
	}

	return graph
}

// renderRules renders rules the way [Rule.String] does, which carries the thing
// each is bound to, the check and every parameter it was bound with.
func renderRules(rules Rules) []string {
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		out = append(out, rule.String())
	}
	return out
}

func TestGraphRules(t *testing.T) {
	graph := loadRuleFixture(t, "valid")

	rules := graph.rules(runnableChecks())

	t.Run("binds every invariant and every assertion the model holds", func(t *testing.T) {
		// Every invariant, node by node in the order the load read them, and
		// then every assertion, thing by thing in the order the families are
		// looked up: the two are kept whole rather than interleaved, because a
		// rule of a type is changed somewhere else from a rule about this room.
		assert.Equal(t, []string{
			"site:Z-01 within-resolves",
			"site:S-101 required-claim (predicate width)",
			"site:S-102 required-claim (predicate width)",
			"site:Z-01 required-claim (predicate width)",
			"site:S-101 boundary-loops-close (tolerance boundary-closure)",
			"geom:V-01 required-claim (predicate position)",
			"geom:E-01 required-claim (predicate position)",
		}, renderRules(rules))
	})

	t.Run("carries the type which declared an invariant and none for an assertion", func(t *testing.T) {
		require.Len(t, rules, 7)

		assert.True(t, rules[1].Invariant())
		assert.Equal(t, "MeetingRoom", rules[1].Type)

		assert.False(t, rules[3].Invariant())
		assert.Empty(t, rules[3].Type)
	})

	t.Run("carries the form of the thing each rule is bound to", func(t *testing.T) {
		require.Len(t, rules, 7)

		assert.Equal(t, SubjectNode, rules[3].Form)
		assert.Equal(t, SubjectVertex, rules[5].Form)
		assert.Equal(t, SubjectEdge, rules[6].Form)
	})

	t.Run("points at the file and line the rule is written on", func(t *testing.T) {
		require.Len(t, rules, 7)

		// An invariant is written in a registry file, once for every instance;
		// an assertion is written on the thing it constrains.
		assert.Equal(t, "registry.dfc", filepath.Base(rules[1].Declared.Start.Path))
		assert.Equal(t, "model.dfc", filepath.Base(rules[3].Declared.Start.Path))
	})

	t.Run("tells a check which is implemented from one which declares itself", func(t *testing.T) {
		require.Len(t, rules, 7)

		assert.True(t, rules[1].Runs())
		assert.True(t, rules[1].Runnable())

		// boundary-loops-close applies to the room and is registered here with
		// no implementation, so it is bound, listed, and decides nothing.
		assert.False(t, rules[4].Runs())
		assert.False(t, rules[4].Runnable())
		assert.True(t, rules[4].Applicable())
	})
}

// TestGraphRulesOfNothing covers the states a caller reaches before a model has
// been written: no graph, and a model which declares no rule at all.
func TestGraphRulesOfNothing(t *testing.T) {
	var absent *Graph
	assert.Empty(t, absent.Rules())

	graph := loadRuleFixture(t, "valid")

	// A type which declares no invariant binds nothing to its instances, so the
	// corridor is in no rule of the model.
	for _, rule := range graph.Rules() {
		assert.NotEqual(t, ID("site:C-01"), rule.Subject.ID())
	}
}

func TestRulesRun(t *testing.T) {
	graph := loadRuleFixture(t, "valid")

	run := graph.rules(runnableChecks()).Run()

	t.Run("counts the rules it was given, the ones which ran, and how they went", func(t *testing.T) {
		assert.Equal(t, 7, run.Rules)

		// Six of the seven have an implementation which applies. The seventh —
		// boundary-loops-close on the room — is bound and decides nothing.
		assert.Equal(t, 6, run.Ran)
		assert.Equal(t, 3, run.Passed)
		assert.Equal(t, 3, run.Failed)
	})

	t.Run("reports one violation per way a rule was not satisfied, in rule order", func(t *testing.T) {
		require.Len(t, run.Violations, 3)

		assert.Equal(t, ID("site:S-102"), run.Violations[0].Instance)
		assert.Equal(t, ID("site:Z-01"), run.Violations[1].Instance)
		assert.Equal(t, ID("geom:E-01"), run.Violations[2].Instance)
	})

	t.Run("names the rule, the parameters it ran with and where each was declared", func(t *testing.T) {
		require.Len(t, run.Violations, 3)

		failed := run.Violations[0]
		assert.Equal(t, "MeetingRoom", failed.Type)
		assert.Equal(t, "required-claim", failed.Check)
		assert.Equal(t, []string{"(predicate width)"}, failed.Arguments)
		assert.Equal(t, "required-claim (predicate width)", failed.Written())
		assert.Equal(t, "expected a claim under width on the subject, found none", failed.Message)
		assert.Equal(t, "registry.dfc", filepath.Base(failed.Declared.Start.Path))
		assert.Equal(t, "model.dfc", filepath.Base(failed.Subject.Start.Path))

		// An assertion is declared on the thing which failed, and names no type.
		written := run.Violations[1]
		assert.Empty(t, written.Type)
		assert.Equal(t, "model.dfc", filepath.Base(written.Declared.Start.Path))
	})
}

// TestRulesRunOfNothing is its own function because it asserts about a run which
// found nothing, which the counts above cannot distinguish from a run which
// found something and lost it.
func TestRulesRunOfNothing(t *testing.T) {
	graph := loadRuleFixture(t, "valid")

	empty := Rules(nil).Run()
	assert.Equal(t, CheckRun{}, empty)

	// A set in which nothing is implemented binds every rule and runs none, so
	// the model is neither reported sound nor reported broken.
	declared := graph.rules(newCheckSet(requiredClaim{}, withinResolves{}, declaredOnly{boundaryLoopsClose{}})).Run()
	assert.Equal(t, 7, declared.Rules)
	assert.Zero(t, declared.Ran)
	assert.Zero(t, declared.Passed)
	assert.Zero(t, declared.Failed)
	assert.Empty(t, declared.Violations)
}

// TestRulesRunAgreesWithTheTwoKindsSeparately is its own function because it is
// an assertion about three calls at once: running the rules whole has to report
// exactly what running each kind on its own does, or a gate and the two engine
// entry points would disagree about the same model.
func TestRulesRunAgreesWithTheTwoKindsSeparately(t *testing.T) {
	graph := loadRuleFixture(t, "valid")
	set := runnableChecks()

	assert.Equal(t, graph.checkInvariants(set), graph.rules(set).invariants().Run().Violations)
	assert.Equal(t, graph.checkAssertions(set), graph.rules(set).assertions().Run().Violations)

	whole := graph.rules(set).Run().Violations
	assert.Len(t, whole, len(graph.checkInvariants(set))+len(graph.checkAssertions(set)))
}

func TestRuleFilter(t *testing.T) {
	graph := loadRuleFixture(t, "valid")
	rules := graph.rules(runnableChecks())

	testCases := []struct {
		name     string
		filter   RuleFilter
		expected []string
	}{
		{
			name:     "selects everything when nothing was asked for",
			filter:   RuleFilter{},
			expected: renderRules(rules),
		},
		{
			name:   "selects the rules bound to one thing",
			filter: RuleFilter{Subjects: []ID{"site:S-101"}},
			expected: []string{
				"site:S-101 required-claim (predicate width)",
				"site:S-101 boundary-loops-close (tolerance boundary-closure)",
			},
		},
		{
			name:   "selects the rules bound to any of several things",
			filter: RuleFilter{Subjects: []ID{"geom:V-01", "geom:E-01"}},
			expected: []string{
				"geom:V-01 required-claim (predicate position)",
				"geom:E-01 required-claim (predicate position)",
			},
		},
		{
			name:   "selects both kinds of rule bound to the instances of one type",
			filter: RuleFilter{Types: []string{"OccupancyZone"}},
			expected: []string{
				"site:Z-01 within-resolves",
				"site:Z-01 required-claim (predicate width)",
			},
		},
		{
			name:   "selects the rules naming one check",
			filter: RuleFilter{Checks: []string{"boundary-loops-close"}},
			expected: []string{
				"site:S-101 boundary-loops-close (tolerance boundary-closure)",
			},
		},
		{
			name: "narrows by every filter given at once",
			filter: RuleFilter{
				Types:  []string{"MeetingRoom"},
				Checks: []string{"required-claim"},
			},
			expected: []string{
				"site:S-101 required-claim (predicate width)",
				"site:S-102 required-claim (predicate width)",
			},
		},
		{
			name:     "selects nothing for a type nothing the rules are bound to declares",
			filter:   RuleFilter{Types: []string{"Corridor"}},
			expected: []string{},
		},
		{
			name:     "selects no rule of a vertex by type, which declares none",
			filter:   RuleFilter{Subjects: []ID{"geom:V-01"}, Types: []string{"MeetingRoom"}},
			expected: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, renderRules(rules.Select(testCase.filter)))
		})
	}
}

// TestRuleFilterNarrowsWhatRuns is its own function because it is about the run
// rather than about the selection: a filtered gate reports the rules it was
// asked about and says nothing at all about the rest of the model.
func TestRuleFilterNarrowsWhatRuns(t *testing.T) {
	graph := loadRuleFixture(t, "valid")

	run := graph.rules(runnableChecks()).Select(RuleFilter{Subjects: []ID{"site:S-102"}}).Run()

	assert.Equal(t, 1, run.Rules)
	assert.Equal(t, 1, run.Ran)
	assert.Equal(t, 1, run.Failed)
	require.Len(t, run.Violations, 1)
	assert.Equal(t, ID("site:S-102"), run.Violations[0].Instance)
}

// TestViolationRendersWhereItFailed covers a check which points at part of a
// subject rather than at the whole of it, which is what a spatial check
// reporting one loop of several does.
func TestViolationRendersWhereItFailed(t *testing.T) {
	graph := loadRuleFixture(t, "valid")

	rules := graph.rules(runnableChecks()).Select(RuleFilter{Subjects: []ID{"site:S-102"}})
	require.Len(t, rules, 1)

	at := Position{Path: "model.dfc", Line: 12, Column: 3}.Span()
	violation := rules[0].violation(Failure{Message: "expected a claim, found none", Span: at})

	assert.Equal(t, at, violation.Subject)
	assert.True(t, strings.HasPrefix(violation.String(), "model.dfc:12:3: error: "))

	// A failure which says nothing about where it is is about the whole thing.
	whole := rules[0].violation(Failure{Message: "expected a claim, found none"})
	assert.Equal(t, rules[0].Subject.Span(), whole.Subject)
}
