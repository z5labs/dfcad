// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runnableRequiredClaim is the registered required-claim check with an
// implementation of it.
//
// The checks the engine compiles in declare themselves and are implemented by
// the story which writes the initial check set, so a test of what *running* an
// invariant does has to bring its own. Registering a set of its own is how that
// is done without reopening the closed registry: a set assembled here and the
// one the engine compiles in are the same type, exercised the same way.
type runnableRequiredClaim struct{ requiredClaim }

// Run implements [Runner].
func (runnableRequiredClaim) Run(subject CheckSubject) []Failure {
	argument, ok := subject.Argument("predicate")
	if !ok {
		return nil
	}

	predicate, ok := argument.Symbol()
	if !ok {
		return nil
	}

	for range subject.Graph().Claims().Under(subject.Node().ID(), predicate) {
		return nil
	}

	return []Failure{{
		Message: fmt.Sprintf("expected a claim under %s on the subject, found none", predicate),
		Hint:    "the type requires one of every instance; write the claim, or take the invariant off the type",
	}}
}

// runnableWithinResolves is the registered within-resolves check with an
// implementation which is satisfied by everything, which is what a test of an
// invariant that passes needs.
type runnableWithinResolves struct{ withinResolves }

// Run implements [Runner].
func (runnableWithinResolves) Run(CheckSubject) []Failure { return nil }

// runnableChecks is the check set the run tests use: two checks which can be
// run and one which declares itself and cannot.
func runnableChecks() *checkSet {
	return newCheckSet(runnableRequiredClaim{}, runnableWithinResolves{}, boundaryLoopsClose{})
}

// invariantFixture is the root of one fixture model whose types carry
// invariants.
func invariantFixture(name string) string { return filepath.Join("testdata", "invariant", name) }

// loadInvariantFixture loads one fixture model, failing the test on any
// diagnostic: every case below is about what a well-formed model binds and
// reports, so a diagnostic means the fixture says something other than what it
// was written to say.
func loadInvariantFixture(t *testing.T, name string) *Graph {
	t.Helper()

	graph, diags := LoadGraph(invariantFixture(name))
	require.NotNil(t, graph, "a load always yields a usable graph")

	for _, diagnostic := range diags {
		t.Errorf("unexpected diagnostic: %s", diagnostic)
	}

	return graph
}

// instanceOf returns the node the fixture holds under id.
func instanceOf(t *testing.T, graph *Graph, id ID) *SemanticNode {
	t.Helper()

	node, ok := graph.Node(id)
	require.True(t, ok, "the fixture holds %s", id)

	return node
}

// renderBindings renders bindings the way [InvariantBinding.String] does, which
// carries the instance, the check and every parameter it was bound with.
func renderBindings(bindings []InvariantBinding) []string {
	out := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, binding.String())
	}
	return out
}

func TestGraphInvariants(t *testing.T) {
	testCases := []struct {
		name     string
		instance ID
		expected []string
	}{
		{
			name:     "binds every invariant the instance's type declares",
			instance: "site:S-102",
			expected: []string{"site:S-102 required-claim (predicate width)"},
		},
		{
			name:     "binds the invariant to an instance written after it was declared",
			instance: "site:S-103",
			expected: []string{"site:S-103 required-claim (predicate width)"},
		},
		{
			name:     "binds nothing to an instance of a type which declares none",
			instance: "site:S-201",
			expected: []string{},
		},
		{
			name:     "binds a check which can examine the instance's geometry",
			instance: "site:Z-02",
			expected: []string{"site:Z-02 boundary-loops-close (tolerance boundary-closure)"},
		},
		{
			name:     "binds nothing a check could not examine to an instance with no shape",
			instance: "site:Z-01",
			expected: []string{},
		},
	}

	graph := loadInvariantFixture(t, "valid")

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := graph.Invariants(instanceOf(t, graph, testCase.instance))

			assert.Equal(t, testCase.expected, renderBindings(got))
		})
	}
}

// TestGraphInvariantsAreNotInherited is its own function because it is an
// assertion about two nodes at once rather than about one: the rule is that an
// invariant reaches the instances of the type which declares it and nothing
// else, which cannot be stated by looking at either end alone.
func TestGraphInvariantsAreNotInherited(t *testing.T) {
	graph := loadInvariantFixture(t, "valid")

	storey := instanceOf(t, graph, "site:L-01")
	zone := instanceOf(t, graph, "site:Z-02")
	room := instanceOf(t, graph, "site:S-101")

	// The room is written within the storey and is a member of the zone, and
	// both of those carry an invariant of their own.
	within, ok := room.Within()
	require.True(t, ok)
	assert.Equal(t, storey.ID(), within)
	assert.Equal(t, []ID{zone.ID()}, room.MemberOf())

	assert.Equal(t, []string{"site:L-01 within-resolves"}, renderBindings(graph.Invariants(storey)))
	assert.Equal(t,
		[]string{"site:Z-02 boundary-loops-close (tolerance boundary-closure)"},
		renderBindings(graph.Invariants(zone)),
	)

	// Neither relationship carries either invariant to the room: what bears on
	// it is what its own type declares.
	assert.Equal(t,
		[]string{"site:S-101 required-claim (predicate width)"},
		renderBindings(graph.Invariants(room)),
	)
}

func TestGraphAllInvariants(t *testing.T) {
	graph := loadInvariantFixture(t, "valid")

	var got []string
	for binding := range graph.AllInvariants() {
		got = append(got, binding.String())
	}

	// Node by node in the order the load read them — the lexical order of the
	// paths, and within a file the order the forms were written — and within a
	// node in the order its type declared them.
	assert.Equal(t, []string{
		"site:S-103 required-claim (predicate width)",
		"site:L-01 within-resolves",
		"site:Z-02 boundary-loops-close (tolerance boundary-closure)",
		"site:S-101 required-claim (predicate width)",
		"site:S-102 required-claim (predicate width)",
	}, got)
}

func TestGraphCheckInvariants(t *testing.T) {
	graph := loadInvariantFixture(t, "valid")

	violations := graph.checkInvariants(runnableChecks())

	t.Run("reports the one instance of several which does not satisfy its type's invariant", func(t *testing.T) {
		require.Len(t, violations, 1)

		assert.Equal(t, ID("site:S-101"), violations[0].Instance)
		assert.Equal(t, "MeetingRoom", violations[0].Type)
		assert.Equal(t, "required-claim", violations[0].Check)
		assert.Equal(t, []string{"(predicate width)"}, violations[0].Arguments)
		assert.Equal(t, "required-claim (predicate width)", violations[0].Written())
		assert.Equal(t, "expected a claim under width on the subject, found none", violations[0].Message)
	})

	t.Run("points at the instance which failed and at the registry line which declared the rule", func(t *testing.T) {
		require.Len(t, violations, 1)

		assert.Equal(t, "site.dfc", filepath.Base(violations[0].Subject.Start.Path))
		assert.Equal(t, "registry.dfc", filepath.Base(violations[0].Declared.Start.Path))

		declared, ok := graph.Registry().Type("MeetingRoom")
		require.True(t, ok)
		require.Len(t, declared.Invariants, 1)
		assert.Equal(t, declared.Invariants[0].Span, violations[0].Declared)
	})

	t.Run("renders as a diagnostic naming the rule and where it is written", func(t *testing.T) {
		require.Len(t, violations, 1)

		diagnostic := violations[0].Diagnostic()

		assert.Equal(t, SeverityError, diagnostic.Severity)
		assert.Equal(t,
			"expected site:S-101 to satisfy the invariant required-claim (predicate width) of its type MeetingRoom: "+
				"expected a claim under width on the subject, found none",
			diagnostic.Message,
		)
		require.Len(t, diagnostic.Related, 1)
		assert.Equal(t, violations[0].Declared, diagnostic.Related[0].Span)
		assert.Contains(t, diagnostic.Related[0].Message, "MeetingRoom")
	})
}

// TestGraphCheckInvariantsRunsNothingUnimplemented is its own function because
// it asserts an absence which the count above cannot distinguish from a check
// which ran and passed.
func TestGraphCheckInvariantsRunsNothingUnimplemented(t *testing.T) {
	graph := loadInvariantFixture(t, "valid")
	set := runnableChecks()

	zone := instanceOf(t, graph, "site:Z-02")
	bindings := graph.invariants(zone, set)

	// boundary-loops-close is bound to the zone and declares itself only, so
	// nothing runs it and the run says nothing about the zone at all.
	require.Len(t, bindings, 1)
	assert.False(t, bindings[0].Runnable())

	room := instanceOf(t, graph, "site:S-102")
	implemented := graph.invariants(room, set)

	require.Len(t, implemented, 1)
	assert.True(t, implemented[0].Runnable())

	for _, violation := range graph.checkInvariants(set) {
		assert.NotEqual(t, ID("site:Z-02"), violation.Instance)
	}
}

// TestGraphCheckInvariantsQuiet covers the two ways a model has nothing to
// report: a type which declares no invariant, and instances which satisfy the
// ones their types do declare.
func TestGraphCheckInvariantsQuiet(t *testing.T) {
	graph := loadInvariantFixture(t, "valid")

	corridor := instanceOf(t, graph, "site:S-201")
	assert.Empty(t, graph.Invariants(corridor))

	// Every check runs and is satisfied, so a model whose instances are all in
	// order produces no output rather than a line per check which passed.
	set := newCheckSet(runnableWithinResolves{})
	assert.Empty(t, graph.checkInvariants(set))
}

func TestInapplicable(t *testing.T) {
	spaceOnly := CheckDeclaration{
		Name:        "space-only",
		Description: "A check which applies to spaces and to nothing else.",
		Forms:       []SubjectForm{SubjectNode},
		Kinds:       []Kind{KindSpace},
	}

	testCases := []struct {
		name     string
		check    CheckDeclaration
		declared Type
		message  string
	}{
		{
			name:  "refuses a check which is not written on a node",
			check: edgeEndpointsDiffer{}.Declare(),
			declared: Type{
				Name:       "Partition",
				Kinds:      []Kind{KindElement},
				Geometries: []Geometry{GeometryLine},
			},
			message: "expected an invariant naming a check which applies to a node, found edge-endpoints-differ, " +
				"which applies to edge",
		},
		{
			name:  "refuses a check which applies to no kind the type permits",
			check: spaceOnly,
			declared: Type{
				Name:       "Partition",
				Kinds:      []Kind{KindElement, KindInterface},
				Geometries: []Geometry{GeometryLine},
			},
			message: "expected an invariant naming a check which applies to a kind the type Partition permits, " +
				"found space-only, which applies to Space",
		},
		{
			name:  "refuses a check which applies to no geometry form the type permits",
			check: boundaryLoopsClose{}.Declare(),
			declared: Type{
				Name:       "Partition",
				Kinds:      []Kind{KindElement},
				Geometries: []Geometry{GeometryLine},
			},
			message: "expected an invariant naming a check which applies to a geometry form the type Partition " +
				"permits, found boundary-loops-close, which applies to area, surface and solid",
		},
		{
			name:     "refuses a check which measures on a type whose instances have no shape",
			check:    boundaryLoopsClose{}.Declare(),
			declared: Type{Name: "Campus", Kinds: []Kind{KindZone}, Absent: true},
			message: "expected an invariant naming a check which applies to a geometry form the type Campus " +
				"permits, found boundary-loops-close, which applies to area, surface and solid",
		},
		{
			name:  "accepts a check which applies to one of the geometry forms the type permits",
			check: boundaryLoopsClose{}.Declare(),
			declared: Type{
				Name:       "MeetingRoom",
				Kinds:      []Kind{KindSpace},
				Geometries: []Geometry{GeometryArea},
				Absent:     true,
			},
		},
		{
			name:  "accepts a check which restricts neither axis",
			check: withinResolves{}.Declare(),
			declared: Type{
				Name:   "Campus",
				Kinds:  []Kind{KindZone},
				Absent: true,
			},
		},
		{
			name:     "says nothing about a type which declares no axis at all",
			check:    boundaryLoopsClose{}.Declare(),
			declared: Type{Name: "Malformed"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			message, hint, refused := inapplicable(testCase.check, testCase.declared)

			if testCase.message == "" {
				assert.False(t, refused)
				assert.Empty(t, message)
				return
			}

			require.True(t, refused)
			assert.Equal(t, testCase.message, message)
			assert.NotEmpty(t, hint, "a diagnostic about a rule which applies to nothing says what to do about it")
		})
	}
}

func TestArgument(t *testing.T) {
	graph := loadInvariantFixture(t, "valid")

	bindings := graph.Invariants(instanceOf(t, graph, "site:S-102"))
	require.Len(t, bindings, 1)
	require.Len(t, bindings[0].Arguments, 1)

	argument := bindings[0].Arguments[0]

	assert.Equal(t, "predicate", argument.Name)
	assert.Equal(t, "(predicate width)", argument.String())
	assert.Equal(t, []string{"width"}, argument.Symbols())
	assert.Equal(t, "registry.dfc", filepath.Base(argument.Span.Start.Path))

	symbol, ok := argument.Symbol()
	require.True(t, ok)
	assert.Equal(t, "width", symbol)
}

// TestCheckSubject covers what a check is handed, which is the only way an
// implementation reaches the model it is judging.
func TestCheckSubject(t *testing.T) {
	graph := loadInvariantFixture(t, "valid")
	node := instanceOf(t, graph, "site:S-102")

	bindings := graph.Invariants(node)
	require.Len(t, bindings, 1)

	subject := CheckSubject{graph: graph, node: node, arguments: bindings[0].Arguments}

	assert.Same(t, graph, subject.Graph())
	assert.Same(t, node, subject.Node())
	assert.Len(t, subject.Arguments(), 1)

	argument, ok := subject.Argument("predicate")
	require.True(t, ok)
	assert.Equal(t, "(predicate width)", argument.String())

	_, ok = subject.Argument("tolerance")
	assert.False(t, ok, "a parameter the invariant did not write is not there to be read")
}

// TestCheckSubjectIsReadOnly covers what a check cannot do to the invariant it
// was handed.
//
// A check runs against every instance of its type in turn, so a check which
// wrote through its arguments would change the rule the instances after it are
// judged by, and the violation it is about to report would render as something
// nobody wrote.
func TestCheckSubjectIsReadOnly(t *testing.T) {
	graph := loadInvariantFixture(t, "valid")
	node := instanceOf(t, graph, "site:S-102")

	bindings := graph.Invariants(node)
	require.Len(t, bindings, 1)

	subject := CheckSubject{graph: graph, node: node, arguments: bindings[0].Arguments}

	handed := subject.Arguments()
	require.Len(t, handed, 1)
	require.NotEmpty(t, handed[0].Values)
	handed[0].Values[0] = &Node{}

	looked, ok := subject.Argument("predicate")
	require.True(t, ok)
	looked.Values[0] = &Node{}

	again, ok := subject.Argument("predicate")
	require.True(t, ok)
	assert.Equal(t, "(predicate width)", again.String())
	assert.Equal(t, "(predicate width)", bindings[0].Arguments[0].String())
}

// TestGraphInvariantsOfNothing covers the states a caller reaches before a model
// has been written: no graph, no node, and a node naming a type nothing
// declares.
func TestGraphInvariantsOfNothing(t *testing.T) {
	var absent *Graph
	assert.Empty(t, absent.Invariants(&SemanticNode{}))

	graph := loadInvariantFixture(t, "valid")
	assert.Empty(t, graph.Invariants(nil))
	assert.Empty(t, graph.Invariants(&SemanticNode{declaredType: "NoSuchType"}))

	for range absent.AllInvariants() {
		t.Error("a graph which does not exist binds nothing")
	}
	assert.Empty(t, absent.CheckInvariants())
}

// TestViolationRendersWhereItFailed covers a check which points at part of a
// subject rather than at the whole of it, which is what a spatial check
// reporting one loop of several does.
func TestViolationRendersWhereItFailed(t *testing.T) {
	graph := loadInvariantFixture(t, "valid")
	node := instanceOf(t, graph, "site:S-101")

	bindings := graph.Invariants(node)
	require.Len(t, bindings, 1)

	at := Position{Path: "entities/site.dfc", Line: 12, Column: 3}.Span()
	violation := bindings[0].violation(Failure{Message: "expected a claim, found none", Span: at})

	assert.Equal(t, at, violation.Subject)
	assert.True(t, strings.HasPrefix(violation.String(), "entities/site.dfc:12:3: error: "))

	// A failure which says nothing about where it is is about the whole node.
	whole := bindings[0].violation(Failure{Message: "expected a claim, found none"})
	assert.Equal(t, node.Span(), whole.Subject)
}
