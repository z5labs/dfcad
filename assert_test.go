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

// nearTo is a check which names other things, which the engine's registered set
// has none of yet.
//
// The checks the engine compiles in are written by the story which writes the
// initial check set, so a test of what happens to an assertion naming an id has
// to bring a check which takes one. Registering a set of its own is how that is
// done without reopening the closed registry: a set assembled here and the one
// the engine compiles in are the same type, read by the same passes.
type nearTo struct{}

// Declare implements [Check].
func (nearTo) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name:        "near-to",
		Description: "The subject is within the named tolerance of the thing the assertion names.",
		Parameters: []CheckParameter{
			{
				Name:        "of",
				Type:        ParameterID,
				Required:    true,
				Description: "The thing the subject has to be near.",
			},
			{
				Name:        "beside",
				Type:        ParameterID,
				Repeated:    true,
				Description: "The things it may also be near, if any.",
			},
			{
				Name:        "tolerance",
				Type:        ParameterTolerance,
				Required:    true,
				Description: "How near is near enough.",
			},
		},
		Forms: []SubjectForm{SubjectNode, SubjectVertex},
	}
}

// claimedValueIs is a check which compares a claimed value against one the
// assertion supplies, which is the shape the restatement rule is about.
type claimedValueIs struct{}

// Declare implements [Check].
func (claimedValueIs) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name:        "claimed-value-is",
		Description: "The subject's claim under the named predicate holds the value the assertion supplies.",
		Parameters: []CheckParameter{
			{
				Name:        "predicate",
				Type:        ParameterPredicate,
				Required:    true,
				Description: "The predicate the value is of.",
			},
			{
				Name:        "is",
				Type:        ParameterReal,
				Required:    true,
				Restates:    true,
				Description: "The value the claim has to hold.",
			},
		},
		Forms: []SubjectForm{SubjectNode},
	}
}

// clearanceAtLeast is the check the restatement rule must not catch: it names a
// predicate and takes a number, and the number is a bound rather than a value of
// the subject's.
type clearanceAtLeast struct{}

// Declare implements [Check].
func (clearanceAtLeast) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name:        "clearance-at-least",
		Description: "The subject's claim under the named predicate is at least the given magnitude.",
		Parameters: []CheckParameter{
			{
				Name:        "predicate",
				Type:        ParameterPredicate,
				Required:    true,
				Description: "The predicate the bound is on.",
			},
			{
				Name:        "minimum",
				Type:        ParameterReal,
				Required:    true,
				Description: "The magnitude the value has to reach.",
			},
		},
		Forms: []SubjectForm{SubjectNode},
	}
}

// elementIsBacked is a check which applies to one kind, which the registered set
// also has none of.
type elementIsBacked struct{}

// Declare implements [Check].
func (elementIsBacked) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name:        "element-is-backed",
		Description: "The element the subject is has an edge which realises it.",
		Forms:       []SubjectForm{SubjectNode},
		Kinds:       []Kind{KindElement},
	}
}

// assertionChecks is the check set the assertion tests read their fixtures
// against: the engine's registered checks, plus the four shapes it does not yet
// have one of.
func assertionChecks() *checkSet {
	return newCheckSet(
		boundaryLoopsClose{},
		edgeEndpointsDiffer{},
		requiredClaim{},
		withinResolves{},
		zoneMembersResolve{},
		nearTo{},
		claimedValueIs{},
		clearanceAtLeast{},
		elementIsBacked{},
	)
}

// assertFixture is the root of one fixture model whose things carry assertions.
func assertFixture(name string) string { return filepath.Join("testdata", "assert", name) }

// loadAssertFixture loads one fixture against a set of checks, which is how a
// fixture naming a check the engine has not written yet is read through the same
// load every model goes through.
func loadAssertFixture(t *testing.T, name string, set *checkSet) (*Graph, []Diagnostic) {
	t.Helper()

	root := assertFixture(name)

	var (
		parsed []source
		diags  []Diagnostic
	)
	for src := range readTree(root) {
		require.NotNil(t, src.file, "the fixture parses: %s", src.diag)
		parsed = append(parsed, src)
	}

	graph, diags := loadGraph(root, parsed, diags, set)
	require.NotNil(t, graph, "a load always yields a usable graph")

	return graph, diags
}

// loadValidAssertFixture loads one fixture and fails the test on any diagnostic,
// which is what a fixture written to load clean is for.
func loadValidAssertFixture(t *testing.T, name string, set *checkSet) *Graph {
	t.Helper()

	graph, diags := loadAssertFixture(t, name, set)
	for _, diagnostic := range diags {
		t.Errorf("unexpected diagnostic: %s", diagnostic)
	}

	return graph
}

// loadAsserted loads one written model against the vocabulary of the valid
// fixture, which is what a test varying one assertion reads.
//
// A fixture on disk is the right shape for a test about a whole model; it is the
// wrong shape for a table whose cases differ by one line, because the difference
// is then somewhere other than in the test.
func loadAsserted(t *testing.T, model string) (*Graph, []Diagnostic) {
	t.Helper()

	registry, err := os.ReadFile(filepath.Join(assertFixture("valid"), "registry"+Extension))
	require.NoError(t, err)

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "registry"+Extension), registry, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "model"+Extension), []byte(model), 0o644))

	return LoadGraph(root)
}

// entityOf looks one thing up by id, failing the test where the model does not
// hold it.
func entityOf(t *testing.T, graph *Graph, id ID) Entity {
	t.Helper()

	entity, ok := graph.Entity(id)
	require.True(t, ok, "the model holds %s", id)

	return entity
}

// messages renders diagnostics as their messages, for a test asserting which
// problems a model has rather than how they are rendered.
func messages(diags []Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		out = append(out, diagnostic.Message)
	}
	return out
}

// TestAssertionsAreRetrievablePerThing covers the assertions written on each of
// the four forms coming back from the thing they were written on.
//
// It is what a caller showing a thing reads: the claims say what is known about
// it and these say what has to hold of it, and neither is derivable from the
// other.
func TestAssertionsAreRetrievablePerThing(t *testing.T) {
	graph := loadValidAssertFixture(t, "valid", registeredChecks)

	testCases := []struct {
		name     string
		id       ID
		expected []string
	}{
		{
			name: "a node carries the assertions written on it, in the order they were written",
			id:   "site:S-101",
			expected: []string{
				"within-resolves",
				"required-claim (predicate width)",
				"boundary-loops-close (tolerance boundary-closure)",
			},
		},
		{
			name:     "a node with no geometry carries the ones which do not need one",
			id:       "site:Z-01",
			expected: []string{"required-claim (predicate width)"},
		},
		{
			name:     "a vertex carries its own",
			id:       "geom:V-01",
			expected: []string{"required-claim (predicate position)"},
		},
		{
			name:     "an edge carries its own",
			id:       "geom:E-01",
			expected: []string{"edge-endpoints-differ"},
		},
		{
			name:     "a loop carries its own",
			id:       "geom:L-01",
			expected: []string{"boundary-loops-close (tolerance boundary-closure)"},
		},
		{
			name:     "a thing nobody wrote an assertion on carries none",
			id:       "geom:V-02",
			expected: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			written, _, ok := writtenAssertions(entityOf(t, graph, testCase.id))
			require.True(t, ok, "%s is a thing which carries assertions", testCase.id)

			rendered := make([]string, 0, len(written))
			for _, assertion := range written {
				rendered = append(rendered, assertion.String())
			}

			assert.Equal(t, testCase.expected, nilIfEmpty(rendered))
		})
	}
}

// nilIfEmpty is an empty slice read as the absence of one, so that a case
// expecting nothing is written as nothing.
func nilIfEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}

// TestAssertionsBindToTheCheckRegistry covers an assertion coming back with what
// the registry says the check it names constrains and takes.
func TestAssertionsBindToTheCheckRegistry(t *testing.T) {
	graph := loadValidAssertFixture(t, "valid", registeredChecks)

	bindings := graph.Assertions(entityOf(t, graph, "site:S-101"))
	require.Len(t, bindings, 3)

	assert.Equal(t, SubjectNode, bindings[1].Form)
	assert.Equal(t, "required-claim", bindings[1].Check.Name)
	assert.True(t, bindings[1].Applicable())
	assert.False(t, bindings[1].Runnable(), "a check which declares itself and implements nothing does not run")
	assert.Equal(t, "site:S-101 required-claim (predicate width)", bindings[1].String())

	argument, ok := bindings[1].Argument("predicate")
	require.True(t, ok)

	symbol, ok := argument.Symbol()
	require.True(t, ok)
	assert.Equal(t, "width", symbol)

	_, ok = bindings[1].Argument("tolerance")
	assert.False(t, ok, "a parameter the assertion did not write is not there to be read")
}

// TestAssertionsAreReadOnly covers what a caller cannot do to the assertions it
// was handed.
//
// The parameters are the loaded tree, which every reader of the model shares. A
// caller which wrote through them would change what the next reader — and the
// rendering of a violation — says somebody wrote.
func TestAssertionsAreReadOnly(t *testing.T) {
	graph := loadValidAssertFixture(t, "valid", registeredChecks)
	node, ok := graph.Node("site:S-101")
	require.True(t, ok)

	handed := node.Assertions()
	require.Len(t, handed, 3)
	require.NotEmpty(t, handed[1].Parameters)
	handed[1].Parameters[0] = &Node{}

	again := node.Assertions()
	require.Len(t, again, 3)
	assert.Equal(t, "required-claim (predicate width)", again[1].String())
}

// TestEveryAssertionOfAModel covers the walk over the whole model, which is what
// a command running or listing them reads.
func TestEveryAssertionOfAModel(t *testing.T) {
	graph := loadValidAssertFixture(t, "valid", registeredChecks)

	var bound []string
	for binding := range graph.AllAssertions() {
		bound = append(bound, binding.String())
	}

	assert.Equal(t, []string{
		"site:S-101 within-resolves",
		"site:S-101 required-claim (predicate width)",
		"site:S-101 boundary-loops-close (tolerance boundary-closure)",
		"site:Z-01 required-claim (predicate width)",
		"geom:V-01 required-claim (predicate position)",
		"geom:E-01 edge-endpoints-differ",
		"geom:L-01 boundary-loops-close (tolerance boundary-closure)",
	}, bound, "family by family, and within a thing in the order they were written")
}

// TestUnregisteredCheckDoesNotBind covers an assertion naming a check nothing
// registers: it is a load error, it is still on the thing it was written on, and
// there is no declaration to bind it to.
func TestUnregisteredCheckDoesNotBind(t *testing.T) {
	graph, diags := loadAssertFixture(t, "kind", registeredChecks)

	assert.Contains(t, strings.Join(messages(diags), "\n"), "element-is-backed")

	node, ok := graph.Node("site:W-01")
	require.True(t, ok)
	assert.Len(t, node.Assertions(), 1, "what somebody wrote is not dropped because it does not resolve")
	assert.Empty(t, graph.Assertions(node), "there is no declaration to bind it to")
}

// TestAssertionParametersAreValidatedAtLoad covers the check registry's half of
// validating an assertion, which runs as each file is read.
func TestAssertionParametersAreValidatedAtLoad(t *testing.T) {
	testCases := []struct {
		name     string
		written  string
		expected string
	}{
		{
			name:     "an unknown check name names the nearest registered one",
			written:  "(assert within-resolve)",
			expected: "expected a registered check name after the assert tag",
		},
		{
			name:     "a parameter the check does not take is named",
			written:  "(assert within-resolves (tolerance boundary-closure))",
			expected: "expected a parameter of the check within-resolves",
		},
		{
			name:     "a parameter the check requires and nobody wrote is named",
			written:  "(assert boundary-loops-close)",
			expected: "expected a (tolerance ...) parameter of the check boundary-loops-close, found none",
		},
		{
			name:     "a numeric literal written where a tolerance name belongs is refused",
			written:  "(assert boundary-loops-close (tolerance 0.005))",
			expected: "expected a declared tolerance name after the tolerance tag",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, diags := loadAsserted(t, `
(node site:S-101
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  `+testCase.written+`)
`)
			assert.Contains(t, strings.Join(messages(diags), "\n"), testCase.expected)
		})
	}
}

// TestAssertionOnASubjectTheCheckCannotExamine covers the three axes a check
// declares what it applies to on.
//
// Each of these is a rule which was never checked rather than one which failed:
// a check with nothing on its subject to look at passes on every run forever, so
// the load is the only place the mistake is visible.
func TestAssertionOnASubjectTheCheckCannotExamine(t *testing.T) {
	_, diags := loadAssertFixture(t, "inapplicable", registeredChecks)

	require.Len(t, diags, 2)

	assert.Equal(t,
		"expected an assertion naming a check which applies to a node, found edge-endpoints-differ, which applies to edge",
		diags[0].Message)
	assert.Contains(t, diags[0].Hint, "written on edge")
	require.Len(t, diags[0].Related, 1)
	assert.Equal(t, "site:S-101 is written here", diags[0].Related[0].Message)

	assert.Equal(t,
		"expected an assertion naming a check which applies to the geometry site:Z-01 has, found "+
			"boundary-loops-close, which applies to area, surface and solid",
		diags[1].Message)
	assert.Contains(t, diags[1].Hint, "no geometry at all")
	assert.Contains(t, diags[1].Hint, "passes on every run forever")
}

// TestAssertionOnAThingWithNoReadableID covers the diagnostic for a thing whose
// id could not be read, which is a thing the model holds and which carries its
// assertions like any other.
//
// A message built from the id alone would read with a hole in it at exactly the
// moment somebody is reading two diagnostics about one form.
func TestAssertionOnAThingWithNoReadableID(t *testing.T) {
	_, diags := loadAsserted(t, `
(node 42
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (assert edge-endpoints-differ))
`)

	rendered := messages(diags)
	require.Contains(t, rendered,
		"expected an assertion naming a check which applies to a node, found edge-endpoints-differ, which applies to edge")

	for _, diagnostic := range diags {
		for _, related := range diagnostic.Related {
			assert.NotEqual(t, " is written here", related.Message, "no message is built around an id nobody could read")
		}
	}

	for _, diagnostic := range diags {
		if strings.Contains(diagnostic.Message, "edge-endpoints-differ") {
			require.Len(t, diagnostic.Related, 1)
			assert.Equal(t, "the node is written here", diagnostic.Related[0].Message)
		}
	}
}

// TestAssertionOnAKindTheCheckCannotExamine covers the middle axis, which the
// engine's registered checks have no case of yet.
func TestAssertionOnAKindTheCheckCannotExamine(t *testing.T) {
	_, diags := loadAssertFixture(t, "kind", assertionChecks())

	require.Len(t, diags, 1)
	assert.Equal(t,
		"expected an assertion naming a check which applies to a Space, found element-is-backed, which applies to Element",
		diags[0].Message)
	assert.Contains(t, diags[0].Hint, "site:S-101 is a Space")
}

// TestAssertionReferencesAreResolvedAtLoad covers an id an assertion names and
// nothing answers to, which is checked when the model loads rather than when
// something runs the check.
func TestAssertionReferencesAreResolvedAtLoad(t *testing.T) {
	_, diags := loadAssertFixture(t, "references", assertionChecks())

	require.Len(t, diags, 2, "one per id which reaches nothing, and the ids which resolve are silent")

	assert.Equal(t,
		"expected the (of ...) parameter of the assertion on site:S-102 to name something the model holds, "+
			"found site:S-104, which nothing answers to",
		diags[0].Message)
	assert.Equal(t, "did you mean site:S-101?", diags[0].Hint)
	require.Len(t, diags[0].Related, 1)
	assert.Equal(t, "site:S-102 is written here", diags[0].Related[0].Message)

	assert.Equal(t,
		"expected the (beside ...) parameter of the assertion on site:S-103 to name something the model holds, "+
			"found site:S-104, which nothing answers to",
		diags[1].Message,
		"a repeated parameter written as one parenthesised list is read the same way as a sequence")
}

// TestAssertionWhichRestatesAClaim covers the rule that an assertion constrains
// and does not record.
//
// The fixture holds one assertion which restates and four which look like it: a
// bound on the same predicate, a required claim with no value at all, the same
// pair on a subject which claims nothing under that predicate, and a value under
// a different predicate of the same subject. Only the first is refused, and the
// four beside it are what stop the rule from being a rule against constraining
// anything that was ever measured.
func TestAssertionWhichRestatesAClaim(t *testing.T) {
	_, diags := loadAssertFixture(t, "restatement", assertionChecks())

	require.Len(t, diags, 1)

	assert.Equal(t,
		"expected an assertion which constrains site:S-101, found one which restates the width it already claims",
		diags[0].Message)
	assert.Contains(t, diags[0].Hint, "an assertion constrains; it does not record")
	assert.Contains(t, diags[0].Hint, "the day the claim is superseded")

	require.Len(t, diags[0].Related, 1)
	assert.Equal(t, "the claim under width is written here", diags[0].Related[0].Message)
}

// runnableEdgeEndpointsDiffer is the registered edge-endpoints-differ check with
// an implementation which is satisfied by nothing, which is what a test of an
// assertion that fails on a subject of the geometric family needs.
type runnableEdgeEndpointsDiffer struct{ edgeEndpointsDiffer }

// Run implements [Runner].
func (runnableEdgeEndpointsDiffer) Run(subject CheckSubject) []Failure {
	edge, ok := subject.Subject().(*Edge)
	if !ok {
		return []Failure{{Message: "expected an edge, found something else"}}
	}

	start, end := edge.Vertices()
	return []Failure{{Message: "expected two different vertices, found " + string(start) + " and " + string(end)}}
}

// TestRunningTheAssertionsOfAModel covers what running them reports: the thing
// which failed, the rule it failed, and where each of those is written.
//
// An assertion is declared on the thing itself, so the violation names no type —
// which is what tells it apart from an invariant's, where the type is where a
// reader has to go to change the rule.
func TestRunningTheAssertionsOfAModel(t *testing.T) {
	graph := loadValidAssertFixture(t, "valid", registeredChecks)

	set := newCheckSet(
		boundaryLoopsClose{},
		requiredClaim{},
		withinResolves{},
		runnableEdgeEndpointsDiffer{},
	)

	violations := graph.checkAssertions(set)
	require.Len(t, violations, 1, "only the check with an implementation runs")

	violation := violations[0]
	assert.Equal(t, ID("geom:E-01"), violation.Instance)
	assert.Empty(t, violation.Type, "an assertion is declared on the thing, not by a type")
	assert.Equal(t, "edge-endpoints-differ", violation.Check)
	assert.Equal(t, "edge-endpoints-differ", violation.Written())

	rendered := violation.Diagnostic()
	assert.Contains(t, rendered.Message, "expected geom:E-01 to satisfy the assertion edge-endpoints-differ written on it")
	require.Len(t, rendered.Related, 1)
	assert.Equal(t, "the assertion is written here", rendered.Related[0].Message)
}

// TestCheckDeclaringARestatingParameter covers what the registry will hold, which
// is what keeps the restatement rule from being a guess about the shape of what
// was written.
func TestCheckDeclaringARestatingParameter(t *testing.T) {
	testCases := []struct {
		name       string
		parameters []CheckParameter
		expected   string
	}{
		{
			name: "a value of the subject's own is declared beside the predicate it is of",
			parameters: []CheckParameter{
				{Name: "predicate", Type: ParameterPredicate, Required: true, Description: "The predicate."},
				{Name: "is", Type: ParameterReal, Restates: true, Description: "The value."},
			},
		},
		{
			name: "one with no predicate beside it is refused",
			parameters: []CheckParameter{
				{Name: "is", Type: ParameterReal, Restates: true, Description: "The value."},
			},
			expected: "names no predicate",
		},
		{
			name: "one which names a registry entry rather than holding a value is refused",
			parameters: []CheckParameter{
				{Name: "predicate", Type: ParameterPredicate, Required: true, Description: "The predicate."},
				{Name: "is", Type: ParameterTypeName, Restates: true, Description: "The value."},
			},
			expected: "carries a value of the subject's own",
		},
		{
			name: "two of them are refused, because a subject has one value under one predicate",
			parameters: []CheckParameter{
				{Name: "predicate", Type: ParameterPredicate, Required: true, Description: "The predicate."},
				{Name: "is", Type: ParameterReal, Restates: true, Description: "The value."},
				{Name: "also", Type: ParameterReal, Restates: true, Description: "The value again."},
			},
			expected: "declares two parameters carrying a value of the subject's own",
		},
		{
			name: "two predicates are refused, because nothing would say which a value is of",
			parameters: []CheckParameter{
				{Name: "predicate", Type: ParameterPredicate, Required: true, Description: "The predicate."},
				{Name: "against", Type: ParameterPredicate, Required: true, Description: "The other one."},
			},
			expected: "names two predicates",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validCheck(CheckDeclaration{
				Name:        "example",
				Description: "An example.",
				Parameters:  testCase.parameters,
				Forms:       []SubjectForm{SubjectNode},
			})

			if testCase.expected == "" {
				assert.NoError(t, err)
				return
			}

			var invalid invalidCheckError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, "example", invalid.Check)
			assert.Contains(t, invalid.Reason, testCase.expected)
		})
	}
}
