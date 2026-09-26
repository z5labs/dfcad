// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shapeChecks are the checks which read a shape, which is every check a curve
// can change the answer of.
var shapeChecks = []string{
	"claim-agrees-with-geometry",
	"contained-areas-do-not-overlap",
	"contained-areas-sum",
	"sits-inside",
	"stays-clear-of-zone",
}

func TestEveryCheckWhichReadsAShapeCanBeToldHowToReadACurve(t *testing.T) {
	for _, name := range shapeChecks {
		t.Run(name+" takes the arc vocabulary and a chord, none of them required", func(t *testing.T) {
			declared, ok := LookupCheck(name)
			require.True(t, ok)

			for _, parameter := range []struct {
				name     string
				expected ParameterType
			}{
				{name: arcCentreParameter, expected: ParameterPredicate},
				{name: arcThroughParameter, expected: ParameterPredicate},
				{name: chordParameter, expected: ParameterTolerance},
			} {
				got, takes := declared.Parameter(parameter.name)
				require.True(t, takes, "%s takes no (%s ...)", name, parameter.name)
				assert.Equal(t, parameter.expected, got.Type)
				assert.False(t, got.Required, "a model which claims no curve has nothing for (%s ...) to read",
					parameter.name)
			}
		})

		t.Run(name+" says which shapes it reads", func(t *testing.T) {
			_, reads := registeredChecks.runner(name).(curveReader)
			assert.True(t, reads, "a check which reads a shape and cannot say which cannot say which curves it read straight")
		})
	}
}

func TestAShapeCheckReadsACurveWhereTheRuleNamesIt(t *testing.T) {
	run := runCheckFixture(t, "curved")

	testCases := []struct {
		name     string
		check    string
		instance ID
		expected []string
	}{
		{
			name:     "agrees with an area claimed of the arc, read as the arc",
			check:    "claim-agrees-with-geometry",
			instance: "site:EASEMENT",
			expected: nil,
		},
		{
			name:     "says the figure an area claim disagreed with is the chord's, where it read the curve straight",
			check:    "claim-agrees-with-geometry",
			instance: "site:EASEMENT-CHORDED",
			expected: []string{
				"expected the area claimed of site:EASEMENT-CHORDED to agree with the shape it is drawn as, found " +
					"139.27 ft² claimed against 100.0 ft² measured, which is 39.27000000000001 ft² more than the " +
					"shape; the figure compared is the chord's, with geom:ZE2 read as the straight line between its " +
					"ends rather than the arc it states",
			},
		},
		{
			name:     "of two rules on a curved edge, fails only the one which measured its chord against a length along the arc",
			check:    "claim-agrees-with-geometry",
			instance: "geom:ZE2",
			expected: []string{
				"expected the length claimed of geom:ZE2 to agree with the corners it runs between, geom:Z2 and " +
					"geom:Z3, found 15.71 ft claimed against 10.0 ft measured, which is 5.710000000000001 ft more " +
					"than the span; the figure compared is the chord's, with geom:ZE2 read as the straight line " +
					"between its ends rather than the arc it states",
			},
		},
		{
			name:     "fails a shed standing in the bow of a zone it is to stay clear of, read as the arc",
			check:    "stays-clear-of-zone",
			instance: "site:SHED-ARC",
			expected: []string{
				"expected site:SHED-ARC to stay clear of the zone site:EASEMENT, found it crossing into it over 2.0 ft²",
			},
		},
		{
			name:     "passes the same shed over the chord, which is what the run then discloses",
			check:    "stays-clear-of-zone",
			instance: "site:SHED",
			expected: nil,
		},
		{
			name:     "holds a shed standing in the bow inside the zone, read as the arc",
			check:    "sits-inside",
			instance: "site:SHED-ARC",
			expected: nil,
		},
		{
			name:     "says the figure is the chord's where a shed is found outside a boundary read straight",
			check:    "sits-inside",
			instance: "site:SHED",
			expected: []string{
				"expected site:SHED to sit inside site:EASEMENT, found 2.0 ft² of it outside, reaching 3.0 ft past " +
					"the boundary at (13.0 4.0 0.0); the figure compared is the chord's, with geom:ZE2 read as the " +
					"straight line between its ends rather than the arc it states",
			},
		},
		{
			name:     "refuses a rule naming one half of the arc vocabulary, and does not call it a chord's figure",
			check:    "stays-clear-of-zone",
			instance: "site:SHED-HALF",
			expected: []string{"expected (arc-through ...) beside (arc-centre ...), found only (arc-centre ...)"},
		},
		{
			name:     "refuses a rule which reads a curve by an overlay and names no chord to draw it to",
			check:    "stays-clear-of-zone",
			instance: "site:SHED-UNDRAWN",
			expected: []string{
				"expected (chord ...) on the rule to draw geom:ZE2 to, found the rule reads it as an arc and names " +
					"no chord tolerance",
			},
		},
		{
			name:     "sums rooms bounded by the arc to the whole bounded by it",
			check:    "contained-areas-sum",
			instance: "site:L-01",
			expected: nil,
		},
		{
			name:     "finds a closet in the bow overlapping the room the arc bounds, and misses it over the chord",
			check:    "contained-areas-do-not-overlap",
			instance: "site:L-01",
			expected: []string{
				"expected no two of the shapes within site:L-01 to cover the same ground, found site:R-02 and " +
					"site:R-03 overlapping by 2.0 ft²",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, reportedBy(run, testCase.check, testCase.instance))
		})
	}
}

// chordedBy is every edge the run says one check read straight on one thing.
func chordedBy(run CheckRun, check string, instance ID) []ID {
	var out []ID
	for _, edge := range run.Chorded {
		if edge.Check == check && edge.Instance == instance {
			out = append(out, edge.Edge)
		}
	}
	return out
}

func TestARuleWhichReadsACurveStraightSaysSo(t *testing.T) {
	graph := loadCheckFixture(t, "curved")
	run := graph.Rules().Run()

	testCases := []struct {
		name     string
		check    string
		instance ID
		expected []ID
	}{
		{
			name:     "on a pass, which is where it would otherwise go unsaid",
			check:    "stays-clear-of-zone",
			instance: "site:SHED",
			expected: []ID{"geom:ZE2"},
		},
		{
			name:     "on a failure",
			check:    "sits-inside",
			instance: "site:SHED",
			expected: []ID{"geom:ZE2"},
		},
		{
			name:     "on a measurement",
			check:    "claim-agrees-with-geometry",
			instance: "site:EASEMENT-CHORDED",
			expected: []ID{"geom:ZE2"},
		},
		{
			name:     "on a rule which names half of what reading the curve takes",
			check:    "stays-clear-of-zone",
			instance: "site:SHED-HALF",
			expected: []ID{"geom:ZE2"},
		},
		{
			name:     "once per edge, over a rule reading several shapes the edge bounds",
			check:    "contained-areas-do-not-overlap",
			instance: "site:L-01",
			expected: []ID{"geom:ZE2"},
		},
		{
			name:     "not at all for a rule which read the curve",
			check:    "stays-clear-of-zone",
			instance: "site:SHED-ARC",
			expected: nil,
		},
		{
			name:     "not at all for a rule which read every curve and summed them",
			check:    "contained-areas-sum",
			instance: "site:L-01",
			expected: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, chordedBy(run, testCase.check, testCase.instance))
		})
	}

	t.Run("naming the predicates the edge states a position under, and where it is written", func(t *testing.T) {
		chorded := run.Chorded[0]

		assert.Equal(t, []string{"arc-centre", "arc-through"}, chorded.Predicates)
		edge, held := graph.Topology().Edge("geom:ZE2")
		require.True(t, held)
		assert.Equal(t, graph.Topology().namedAt(edge.ID(), edge.Span()), chorded.Span)
	})
}

func TestARuleSaysWhichCurvesItWillReadStraightBeforeItRuns(t *testing.T) {
	rules := loadCheckFixture(t, "curved").Rules()
	run := rules.Run()

	var before []ChordedEdge
	for _, rule := range rules {
		before = append(before, rule.Chorded()...)
	}

	assert.Equal(t, run.Chorded, before, "a listing and a run disagreeing about which passes rest on a chord is two answers")

	testCases := []struct {
		name     string
		instance ID
		check    string
		expected bool
	}{
		{name: "reads arcs where it names both predicates", instance: "site:SHED-ARC", check: "sits-inside", expected: true},
		{name: "does not where it names none", instance: "site:SHED", check: "sits-inside", expected: false},
		{name: "does not where it names one", instance: "site:SHED-HALF", check: "stays-clear-of-zone", expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			selected := rules.Select(RuleFilter{Subjects: []ID{testCase.instance}, Checks: []string{testCase.check}})
			require.Len(t, selected, 1)

			assert.Equal(t, testCase.expected, selected[0].ReadsArcs())
		})
	}
}

func TestARuleWhichDrewACurveSaysWhatTo(t *testing.T) {
	run := runCheckFixture(t, "curved")

	drew := make(map[string]DrawnCurve)
	for _, drawn := range run.Drawn {
		drew[string(drawn.Instance)+" "+drawn.Check] = drawn
	}

	testCases := []struct {
		name  string
		rule  string
		drawn bool
	}{
		{name: "an overlay which read the arc", rule: "site:SHED-ARC stays-clear-of-zone", drawn: true},
		{name: "a containment which read the arc", rule: "site:SHED-ARC sits-inside", drawn: true},
		{name: "a sum which read the arc", rule: "site:L-01 contained-areas-sum", drawn: true},
		{name: "not a measurement, which reads the arc without drawing it", rule: "site:EASEMENT claim-agrees-with-geometry"},
		{name: "not a rule which drew nothing curved", rule: "site:SHED stays-clear-of-zone"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			drawn, held := drew[testCase.rule]
			require.Equal(t, testCase.drawn, held)
			if !testCase.drawn {
				return
			}

			assert.Equal(t, "chord", drawn.Chord)
			assert.Equal(t, 0.01, drawn.Value)
			assert.Equal(t, Unit("ft"), drawn.Unit)
			assert.Positive(t, drawn.Deviation)
			assert.LessOrEqual(t, drawn.Deviation, drawn.Value, "what was achieved is within what was asked for")
		})
	}
}

func TestAChordedEdgeIsAWarningLeadingBackToTheRule(t *testing.T) {
	graph := loadCheckFixture(t, "curved")
	run := graph.Rules().Run()

	testCases := []struct {
		name     string
		instance ID
		hint     string
	}{
		{name: "tells a rule which named nothing what to name", instance: "site:SHED", hint: "(arc-centre ...)"},
		{name: "tells a rule which named half to name both", instance: "site:SHED-HALF", hint: "names one of the two predicates"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var chorded ChordedEdge
			for _, edge := range run.Chorded {
				if edge.Instance == testCase.instance {
					chorded = edge
					break
				}
			}
			require.Equal(t, testCase.instance, chorded.Instance)

			diagnostic := chorded.Diagnostic()

			assert.Equal(t, SeverityWarning, diagnostic.Severity, "reading a curve straight is reported, never refused")
			assert.Equal(t, chorded.Span, diagnostic.Span)
			assert.Contains(t, diagnostic.Hint, testCase.hint)
			require.Len(t, diagnostic.Related, 1)
			assert.Equal(t, chorded.Declared, diagnostic.Related[0].Span)
		})
	}
}

func TestAModelWhichClaimsNoCurveRunsAsItDidBefore(t *testing.T) {
	for _, fixture := range []string{"agreement", "appraised", "inside", "satisfied", "violating"} {
		t.Run(fixture+" reports no curve read straight and none drawn", func(t *testing.T) {
			run := runCheckFixture(t, fixture)

			assert.Empty(t, run.Chorded)
			assert.Empty(t, run.Drawn)

			for _, violation := range run.Violations {
				assert.False(t, strings.Contains(violation.Message, "chord's"), violation.Message)
			}
		})
	}
}

func TestACurvedRunIsDeterministic(t *testing.T) {
	first := runCheckFixture(t, "curved")
	second := runCheckFixture(t, "curved")

	assert.Equal(t, first, second)
}
