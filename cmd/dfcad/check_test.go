// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// checkRegistry is the vocabulary the model below is judged against.
//
// Two of its types declare an invariant and one declares none, so that a run
// over the model has rules bound to some of its instances and not to others —
// which is what says whether a count of how many checks there were is a count
// of rules rather than of things.
const checkRegistry = `(project
  (label "Check fixture")
  (globalid-namespace "https://example.org/models/check"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
(namespace survey (description "Claim ids issued by Acme Surveys."))

(frame frame:building (label "Building local grid") (unit m))

(tolerance boundary-closure
  (value 0.005 m)
  (description "How far a loop may fail to close and still count as closed."))

(predicate position
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "The location of a vertex in its frame."))

(predicate width
  (unit m)
  (shape scalar)
  (description "The clear width between two faces."))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings.")
  (invariant required-claim (predicate width)))

(type Corridor
  (kind Space)
  (geometry area)
  (description "A circulation space between rooms."))

(type OccupancyZone
  (kind Zone)
  (geometry absent)
  (description "A grouping occupancy is counted over, with no shape of its own.")
  (invariant within-resolves))
`

// checkModel is a model carrying both kinds of rule: the invariants its types
// declare, and the assertions written on the things themselves.
//
// The nodes are written out of id order, so that a run reporting the order the
// model was read rather than the order the ids sort in says so.
const checkModel = `(node site:Z-01
  (label "Level 1 occupancy")
  (kind Zone)
  (type OccupancyZone)
  (assert required-claim (predicate width)))

(node site:S-101
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (width
    (id survey:W-0001)
    (value 4.2 m)
    (source "As-built check AB-2026-009, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.003 m))
    (date "2026-05-06"))
  (assert boundary-loops-close (tolerance boundary-closure)))

(node site:S-102
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building))

(node site:C-01
  (label "Level 1 corridor")
  (kind Space)
  (type Corridor)
  (geometry area)
  (frame frame:building))

(vertex geom:V-01
  (label "Room A, north-west corner")
  (frame frame:building)
  (position
    (value (0.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18"))
  (assert required-claim (predicate position)))

(vertex geom:V-02
  (label "Room A, north-east corner")
  (frame frame:building)
  (position
    (value (4.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(edge geom:E-01
  (label "Room A, north wall")
  (frame frame:building)
  (vertices geom:V-01 geom:V-02)
  (assert required-claim (predicate position)))
`

// ruled is the fixture tree the check command is run against.
func ruled() map[string]string {
	return map[string]string{
		"registry.dfc":      checkRegistry,
		"entities/site.dfc": checkModel,
	}
}

// checked runs the check command against a tree and returns the result object,
// the exit code and what reached stderr.
func checked(t *testing.T, files map[string]string, args ...string) (checkResult, int, string) {
	t.Helper()

	t.Chdir(tree(t, files))

	var stdout, stderr bytes.Buffer
	code := run(append([]string{"check"}, args...), &stdout, &stderr)

	if stdout.Len() == 0 {
		return checkResult{}, code, stderr.String()
	}

	return listed[checkResult](t, stdout.String()), code, stderr.String()
}

// rules is each listed check as "subject check parameters", which is how the
// rule reads on the thing it is bound to.
func rules(result checkResult) []string {
	out := make([]string, 0, len(result.Checks))
	for _, entry := range result.Checks {
		out = append(out, writtenRule(entry))
	}
	return out
}

func TestRunCheck(t *testing.T) {
	testCases := []struct {
		name           string
		files          map[string]string
		args           []string
		expectedChecks int
	}{
		{
			name:           "runs every invariant and every assertion the model states",
			files:          ruled(),
			expectedChecks: 7,
		},
		{
			name:           "runs only the rules bound to one thing",
			files:          ruled(),
			args:           []string{"--subject", "site:S-101"},
			expectedChecks: 2,
		},
		{
			name:           "runs the rules bound to any of several things",
			files:          ruled(),
			args:           []string{"--subject", "geom:V-01", "--subject", "geom:E-01"},
			expectedChecks: 2,
		},
		{
			name:           "runs both kinds of rule bound to the instances of one type",
			files:          ruled(),
			args:           []string{"--type", "OccupancyZone"},
			expectedChecks: 2,
		},
		{
			name:           "runs only the rules naming one check",
			files:          ruled(),
			args:           []string{"--check", "boundary-loops-close"},
			expectedChecks: 1,
		},
		{
			name:           "narrows by every filter given at once",
			files:          ruled(),
			args:           []string{"--type", "MeetingRoom", "--check", "required-claim"},
			expectedChecks: 2,
		},
		{
			name:           "runs nothing for a type whose instances state no rule",
			files:          ruled(),
			args:           []string{"--type", "Corridor"},
			expectedChecks: 0,
		},
		{
			name:           "runs nothing over a model which states no rule at all",
			files:          model(),
			expectedChecks: 0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, code, stderr := checked(t, testCase.files, testCase.args...)

			require.Equal(t, exitSuccess, code, stderr)
			assert.Equal(t, outputVersion, result.Version)
			assert.Equal(t, "check", result.Command)
			assert.Equal(t, testCase.expectedChecks, result.Summary.Checks)

			// Nothing ran which was not selected, and a run reports what failed
			// rather than what there was.
			assert.Equal(t, result.Summary.Runnable, result.Summary.Ran)
			assert.Equal(t, result.Summary.Ran, result.Summary.Passed+result.Summary.Failed)
			assert.Empty(t, result.Checks)
			assert.Equal(t, []dfcad.Violation{}, result.Violations)
		})
	}
}

// TestRunCheckListsWhatWouldRun is its own function because --list answers a
// different question from a run: not what the model breaks, but which rules bear
// on it and which of them decide anything.
func TestRunCheckListsWhatWouldRun(t *testing.T) {
	result, code, stderr := checked(t, ruled(), "--list")

	require.Equal(t, exitSuccess, code, stderr)

	t.Run("lists every rule in the order it would run in", func(t *testing.T) {
		// Every invariant, node by node in the order the model was read, and
		// then every assertion, thing by thing.
		assert.Equal(t, []string{
			"site:Z-01 within-resolves",
			"site:S-101 required-claim (predicate width)",
			"site:S-102 required-claim (predicate width)",
			"site:Z-01 required-claim (predicate width)",
			"site:S-101 boundary-loops-close (tolerance boundary-closure)",
			"geom:V-01 required-claim (predicate position)",
			"geom:E-01 required-claim (predicate position)",
		}, rules(result))
	})

	t.Run("says which kind of rule each is and where it is written", func(t *testing.T) {
		require.Len(t, result.Checks, 7)

		invariant := result.Checks[1]
		assert.Equal(t, ruleInvariant, invariant.Rule)
		assert.Equal(t, "MeetingRoom", invariant.Type)
		assert.Equal(t, "node", invariant.Form)
		assert.Equal(t, "registry.dfc", invariant.Declared.Start.Path)

		assertion := result.Checks[3]
		assert.Equal(t, ruleAssertion, assertion.Rule)
		assert.Empty(t, assertion.Type)
		assert.Equal(t, "entities/site.dfc", assertion.Declared.Start.Path)

		assert.Equal(t, "vertex", result.Checks[5].Form)
		assert.Equal(t, "edge", result.Checks[6].Form)
	})

	t.Run("counts what would run without running any of it", func(t *testing.T) {
		assert.Equal(t, 7, result.Summary.Checks)
		assert.Zero(t, result.Summary.Ran)
		assert.Zero(t, result.Summary.Passed)
		assert.Zero(t, result.Summary.Failed)
		assert.Empty(t, result.Violations)

		// A check which declares itself and has no implementation is listed and
		// counted apart, so the two numbers say how much of the model is
		// actually decided.
		would := 0
		for _, entry := range result.Checks {
			if entry.Runs {
				would++
			}
		}
		assert.Equal(t, would, result.Summary.Runnable)
	})
}

// TestRunCheckListsWhyARuleDecidesNothing is its own function because it is
// about the two reasons a rule does not run rather than about the listing: they
// are fixed in different places, so a report which folded them together would
// send the author of a model to the engine to fix a line of their own.
func TestRunCheckListsWhyARuleDecidesNothing(t *testing.T) {
	// An assertion naming a check which cannot examine the thing it is written
	// on is a load error, so this is a model somebody is part way through
	// fixing — which is the only model in which the two reasons are both here.
	written := checkModel + `
(node site:S-103
  (label "Meeting Room C")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (assert edge-endpoints-differ))
`

	result, code, stderr := checked(t, map[string]string{
		"registry.dfc":      checkRegistry,
		"entities/site.dfc": written,
	}, "--list")

	// The model was refused, and the listing still says what bears on it.
	require.Equal(t, exitLoad, code)
	assert.Contains(t, stderr, "edge-endpoints-differ")

	inapplicable, ok := listedFor(result, "site:S-103", "edge-endpoints-differ")
	require.True(t, ok)
	assert.False(t, inapplicable.Runs)
	assert.False(t, inapplicable.Applicable)
	assert.Equal(t, "does not apply to what it is written on", outcome(inapplicable))

	// The rule beside it is one the engine has not implemented, which is a
	// different answer and is fixed somewhere else.
	unimplemented, ok := listedFor(result, "site:S-101", "required-claim")
	require.True(t, ok)
	assert.False(t, unimplemented.Runs)
	assert.True(t, unimplemented.Applicable)
	assert.Equal(t, "declared, not implemented", outcome(unimplemented))
}

// listedFor is the listed rule which binds one check to one thing.
func listedFor(result checkResult, subject, check string) (listedCheck, bool) {
	for _, entry := range result.Checks {
		if entry.Subject == subject && entry.Check == check {
			return entry, true
		}
	}
	return listedCheck{}, false
}

// TestRunCheckIsDeterministic is its own function because it is a property of
// two runs rather than of one: a gate whose output moved between runs over one
// model would make every diff of its reports noise.
func TestRunCheckIsDeterministic(t *testing.T) {
	stdout := func(t *testing.T, args ...string) string {
		t.Helper()

		t.Chdir(tree(t, ruled()))

		var out, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run(append([]string{"check"}, args...), &out, &stderr), stderr.String())

		return out.String()
	}

	assert.Equal(t, stdout(t), stdout(t))
	assert.Equal(t, stdout(t, "--list"), stdout(t, "--list"))

	// Neither the format nor the verbosity reaches stdout, and neither does how
	// long the run took.
	assert.Equal(t, stdout(t), stdout(t, "--format", formatHuman, "-v"))
}

// TestRunCheckRejectsAFilterNamingNothing covers the names which name nothing.
//
// Each is a usage error rather than an empty run: a gate which reported a model
// sound because a filter was misspelled is worse than one which refuses to
// answer.
func TestRunCheckRejectsAFilterNamingNothing(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "rejects a subject nothing in the model holds",
			args: []string{"--subject", "site:S-999"},
		},
		{
			name: "rejects a type no registry file declares",
			args: []string{"--type", "BreakRoom"},
		},
		{
			name: "rejects a check the engine does not register",
			args: []string{"--check", "loops-close"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, ruled()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitUsage, run(append([]string{"check"}, testCase.args...), &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.NotEmpty(t, stderr.String())
		})
	}
}

// TestRuleFilterCarriesWhatWasWrongWithIt is its own function because it is
// about the error rather than about the exit code: a caller which misspelled a
// filter has to be able to read what it asked for and what there was without
// matching a message.
func TestRuleFilterCarriesWhatWasWrongWithIt(t *testing.T) {
	graph, _ := dfcad.LoadGraph(tree(t, ruled()))

	t.Run("names the nearest id to a subject nothing holds", func(t *testing.T) {
		_, err := ruleFilter(graph, []string{"site:C-02"}, nil, nil)

		var unknown UnknownIDError
		require.ErrorAs(t, err, &unknown)
		assert.Equal(t, "site:C-02", unknown.ID)
		assert.Equal(t, "site:C-01", unknown.Nearest)
	})

	t.Run("names the types there are for a type nothing declares", func(t *testing.T) {
		_, err := ruleFilter(graph, nil, []string{"BreakRoom"}, nil)

		var unknown UnknownTypeError
		require.ErrorAs(t, err, &unknown)
		assert.Equal(t, "BreakRoom", unknown.Type)
		assert.Equal(t, []string{"Corridor", "MeetingRoom", "OccupancyZone"}, unknown.Declared)
	})

	t.Run("names the checks there are for a check nothing registers", func(t *testing.T) {
		_, err := ruleFilter(graph, nil, nil, []string{"loops-close"})

		var unknown UnknownCheckError
		require.ErrorAs(t, err, &unknown)
		assert.Equal(t, "loops-close", unknown.Check)
		assert.Equal(t, checkNames(), unknown.Known)
	})

	t.Run("rejects a subject which is not an id at all", func(t *testing.T) {
		_, err := ruleFilter(graph, []string{"not an id"}, nil, nil)

		require.Error(t, err)
	})

	t.Run("takes every filter which names something", func(t *testing.T) {
		filter, err := ruleFilter(graph, []string{"site:S-101"}, []string{"MeetingRoom"}, []string{"required-claim"})

		require.NoError(t, err)
		assert.Equal(t, dfcad.RuleFilter{
			Subjects: []dfcad.ID{"site:S-101"},
			Types:    []string{"MeetingRoom"},
			Checks:   []string{"required-claim"},
		}, filter)
	})
}

// TestUnknownCheckNamesTheChecksThereAre checks that the error carries the
// closed set rather than only saying the name was wrong, because the set is
// compiled in and there is no command which would print a longer one.
func TestUnknownCheckNamesTheChecksThereAre(t *testing.T) {
	known := checkNames()
	require.NotEmpty(t, known)
	assert.True(t, sortedStrings(known))

	err := UnknownCheckError{Check: "loops-close", Known: known}
	for _, name := range known {
		assert.Contains(t, err.Error(), name)
	}
}

// TestRunCheckRejectsWhatIsNotAnInvocation covers the invocations which are
// wrong before anything is looked up.
func TestRunCheckRejectsWhatIsNotAnInvocation(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "rejects an argument, which the command does not take",
			args: []string{"site:S-101"},
		},
		{
			name: "rejects a subject which is not an id at all",
			args: []string{"--subject", "not an id"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, ruled()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitUsage, run(append([]string{"check"}, testCase.args...), &stdout, &stderr))
			assert.Empty(t, stdout.String())
			assert.NotEmpty(t, stderr.String())
		})
	}
}

// TestRunCheckReportsAModelItCouldNotRead is its own function because it is
// about the exit code rather than about what ran: the gate has to tell a model
// which breaks a rule from a model it never managed to read, and a model root
// with nothing in it is the second of those rather than a model with nothing to
// check.
func TestRunCheckReportsAModelItCouldNotRead(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
	}{
		{
			name:  "reports a file which did not parse as a load failure",
			files: map[string]string{"registry.dfc": checkRegistry, "broken.dfc": "(node site:S-1"},
		},
		{
			name:  "reports a root which holds no model as a load failure",
			files: map[string]string{"notes.md": "nothing to see"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, code, stderr := checked(t, testCase.files)

			require.Equal(t, exitLoad, code)

			// It ran and answered, so the answer is on stdout beside the
			// non-zero code, and what is wrong with the model is on stderr.
			assert.Equal(t, "check", result.Command)
			assert.NotEmpty(t, stderr)
		})
	}
}

func TestCheckCode(t *testing.T) {
	testCases := []struct {
		name     string
		summary  checkSummary
		refused  bool
		expected int
	}{
		{
			name:     "succeeds when every rule which ran was satisfied",
			summary:  checkSummary{Checks: 3, Runnable: 3, Ran: 3, Passed: 3},
			expected: exitSuccess,
		},
		{
			name:     "succeeds when the model states no rule at all",
			summary:  checkSummary{},
			expected: exitSuccess,
		},
		{
			name:     "reports a rule which was not satisfied as a check failure",
			summary:  checkSummary{Checks: 3, Runnable: 3, Ran: 3, Passed: 2, Failed: 1},
			expected: exitCheck,
		},
		{
			name:     "reports a model it could not read as a load failure",
			summary:  checkSummary{},
			refused:  true,
			expected: exitLoad,
		},
		{
			name:     "prefers a load failure to a rule which was not satisfied",
			summary:  checkSummary{Checks: 3, Runnable: 3, Ran: 3, Failed: 3},
			refused:  true,
			expected: exitLoad,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, checkCode(testCase.summary, testCase.refused))
		})
	}
}

// TestCheckReportsAFailureInFull is its own function because it is about the
// shape of a violation rather than about a run: what a failure has to name is
// the thing, the rule, the parameters it ran with — the tolerance among them —
// and where both the thing and the rule are written.
func TestCheckReportsAFailureInFull(t *testing.T) {
	declared := dfcad.Position{Path: "registry.dfc", Line: 30, Column: 3}.Span()
	subject := dfcad.Position{Path: "entities/site.dfc", Line: 8, Column: 1}.Span()

	result := checkResult{
		envelope: newEnvelope("check"),
		Summary:  checkSummary{Checks: 1, Runnable: 1, Ran: 1, Failed: 1},
		Violations: []dfcad.Violation{{
			Instance:  "site:L-01",
			Type:      "MeetingRoom",
			Check:     "boundary-loops-close",
			Arguments: []string{"(tolerance boundary-closure)"},
			Declared:  declared,
			Subject:   subject,
			Message:   "expected the loop to close within 0.005 m, found a gap of 0.021 m",
			Hint:      "move the last corner onto the first, or widen the tolerance",
		}},
	}

	var stdout bytes.Buffer
	require.NoError(t, emit(&stdout, result))

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded))

	violations, ok := decoded["violations"].([]any)
	require.True(t, ok)
	require.Len(t, violations, 1)

	failure, ok := violations[0].(map[string]any)
	require.True(t, ok)

	// The thing, the rule and the parameters it ran with, including the
	// tolerance it was measured against.
	assert.Equal(t, "site:L-01", failure["instance"])
	assert.Equal(t, "MeetingRoom", failure["type"])
	assert.Equal(t, "boundary-loops-close", failure["check"])
	assert.Equal(t, []any{"(tolerance boundary-closure)"}, failure["arguments"])

	// What was expected and what was found.
	assert.Contains(t, failure["message"], "0.021 m")

	// Where the thing which failed is written, and where the rule which failed
	// it is declared. They are different files, which is the point of carrying
	// both.
	assert.Contains(t, stdout.String(), `"declared":`)
	assert.Contains(t, stdout.String(), `"subject":`)
	assert.NotEqual(t, failure["declared"], failure["subject"])
}

// TestCheckReportsWhatItFoundForAPerson is its own function because it is about
// stderr rather than about the result: a run reports itself to whoever is
// watching, and how long it took, without any of that reaching stdout.
func TestCheckReportsWhatItFoundForAPerson(t *testing.T) {
	report := func(t *testing.T, args ...string) string {
		t.Helper()

		t.Chdir(tree(t, ruled()))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run(append([]string{"check"}, args...), &stdout, &stderr), stderr.String())

		return stderr.String()
	}

	quiet := report(t)
	human := report(t, "--format", formatHuman)
	loud := report(t, "-v")
	listed := report(t, "--format", formatHuman, "--list", "-v")

	// The default says nothing about a model with nothing wrong with it.
	assert.Empty(t, quiet)

	// Asked to report itself, it says how the rules went and how long it took.
	assert.Contains(t, human, "7 checks: 0 ran")
	assert.Contains(t, human, "7 decided nothing")

	// Verbosity is progress rather than result, so it says how long the run
	// took without saying what the run found.
	assert.Contains(t, loud, "loading the model")
	assert.Contains(t, loud, "7 rules in ")
	assert.NotContains(t, loud, "0 passed")

	// The detail behind a listing is the rules themselves.
	assert.Contains(t, listed, "site:S-101 required-claim (predicate width): ")
	assert.Contains(t, listed, "7 checks: ")
	assert.Contains(t, listed, "would run")
}

// TestCheckRendersEveryViolationAsADiagnostic is its own function because it is
// about the second rendering of a finding: a violation is a problem in something
// somebody wrote, so it reaches stderr as a diagnostic on every run and in every
// format, and neither rendering is produced by parsing the other.
func TestCheckRendersEveryViolationAsADiagnostic(t *testing.T) {
	violation := dfcad.Violation{
		Instance:  "site:L-01",
		Type:      "MeetingRoom",
		Check:     "boundary-loops-close",
		Arguments: []string{"(tolerance boundary-closure)"},
		Declared:  dfcad.Position{Path: "registry.dfc", Line: 30, Column: 3}.Span(),
		Subject:   dfcad.Position{Path: "entities/site.dfc", Line: 8, Column: 1}.Span(),
		Message:   "expected the loop to close within 0.005 m, found a gap of 0.021 m",
	}

	diagnostics := diagnose([]dfcad.Violation{violation})
	require.Len(t, diagnostics, 1)

	rendered := diagnostics[0].String()
	assert.True(t, strings.HasPrefix(rendered, "entities/site.dfc:8:1: error: "))
	assert.Contains(t, rendered, "boundary-loops-close (tolerance boundary-closure)")
	assert.Contains(t, rendered, "MeetingRoom")
	assert.Contains(t, rendered, "0.021 m")

	assert.Empty(t, diagnose(nil))
}
