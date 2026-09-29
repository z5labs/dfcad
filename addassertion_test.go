// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two fixtures an assertion is written into: one whose every rule holds, and
// one written to exercise a rule requiring a claim.
const (
	satisfiedFixture = "testdata/checks/satisfied"
	claimedFixture   = "testdata/checks/claimed"
)

// spelledAssertions is every assertion bound on one thing, as the entity format
// writes it without its parentheses.
func spelledAssertions(graph *Graph, id ID) []string {
	entity, ok := graph.Entity(id)
	if !ok {
		return nil
	}

	var out []string
	for _, binding := range graph.Assertions(entity) {
		out = append(out, binding.Declared.String())
	}
	return out
}

func TestTxAddAssertion(t *testing.T) {
	testCases := []struct {
		name     string
		fixture  string
		spec     AssertionSpec
		expected string
	}{
		{
			name:    "writes a check with its parameters on a loop",
			fixture: satisfiedFixture,
			spec: AssertionSpec{
				Subject:    "geom:L-10",
				Check:      "boundary-loops-close",
				Parameters: []string{"tolerance boundary-closure", "position position"},
			},
			// Canonical form sorts the parameters of an assertion, so they read
			// back in that order whichever order they were written in.
			expected: "boundary-loops-close (position position) (tolerance boundary-closure)",
		},
		{
			name:     "writes a check which takes no parameter on an edge",
			fixture:  satisfiedFixture,
			spec:     AssertionSpec{Subject: "geom:E-01", Check: "edge-backing-resolves"},
			expected: "edge-backing-resolves",
		},
		{
			name:    "writes a check naming another thing on a node",
			fixture: satisfiedFixture,
			spec: AssertionSpec{
				Subject:    "site:S-101",
				Check:      "stays-clear-of-zone",
				Parameters: []string{"zone site:Z-90", "tolerance boundary-closure", "position position"},
			},
			expected: "stays-clear-of-zone (position position) (tolerance boundary-closure) (zone site:Z-90)",
		},
		{
			name:     "writes a check on a vertex",
			fixture:  claimedFixture,
			spec:     AssertionSpec{Subject: "geom:V-02", Check: "required-claim", Parameters: []string{"predicate position"}},
			expected: "required-claim (predicate position)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, testCase.fixture)

			graph := authored(t, root, func(tx *Tx) error {
				return tx.AddAssertion(testCase.spec)
			})

			assert.Contains(t, spelledAssertions(graph, testCase.spec.Subject), testCase.expected)
		})
	}
}

// TestTxAddAssertionRoundTrips checks the assertion as a property of the file it
// lands in rather than as a literal: the file is written in canonical form, so
// reading it and printing it again gives back the bytes on disk, and the
// assertion read back is the one which was written.
func TestTxAddAssertionRoundTrips(t *testing.T) {
	root := copied(t, satisfiedFixture)

	spec := AssertionSpec{
		Subject:    "site:S-101",
		Check:      "stays-clear-of-zone",
		Parameters: []string{"zone site:Z-90", "tolerance boundary-closure", "position position"},
	}

	graph := authored(t, root, func(tx *Tx) error { return tx.AddAssertion(spec) })

	path := filepath.Join(root, "model.dfc")
	src, err := os.ReadFile(path)
	require.NoError(t, err)

	file, err := Parse(path, bytes.NewReader(src))
	require.NoError(t, err)

	var printed bytes.Buffer
	require.NoError(t, Print(&printed, file))
	assert.Equal(t, string(src), printed.String(), "the file is written in canonical form")

	node, ok := graph.Node(spec.Subject)
	require.True(t, ok)

	written := node.Assertions()
	require.Len(t, written, 1)
	assert.Equal(t, spec.Check, written[0].Check)

	arguments := make([]string, 0, len(written[0].Arguments()))
	for _, argument := range written[0].Arguments() {
		arguments = append(arguments, argument.String())
	}
	expected := make([]string, 0, len(spec.Parameters))
	for _, parameter := range spec.Parameters {
		expected = append(expected, "("+parameter+")")
	}
	assert.ElementsMatch(t, expected, arguments, "the parameters read back as they were written")
}

// TestTxAddAssertionPrintsWhereTheSpecificationTablesIt checks where the child
// lands in its form, which is canonical form's decision and not the order it was
// appended in: after every child the specification tables before it.
func TestTxAddAssertionPrintsWhereTheSpecificationTablesIt(t *testing.T) {
	root := copied(t, satisfiedFixture)

	authored(t, root, func(tx *Tx) error {
		return tx.AddAssertion(AssertionSpec{
			Subject:    "geom:L-10",
			Check:      "boundary-loops-close",
			Parameters: []string{"tolerance boundary-closure", "position position"},
		})
	})

	src, err := os.ReadFile(filepath.Join(root, "model.dfc"))
	require.NoError(t, err)

	assert.Contains(t, string(src), `(loop
  geom:L-10
  (label "Level outline")
  (frame frame:building)
  (edges geom:E-01 geom:E-02 geom:E-03 geom:E-04)
  (assert boundary-loops-close (position position) (tolerance boundary-closure)))`)
}

func TestTxAddAssertionRefusesTheAssertion(t *testing.T) {
	testCases := []struct {
		name     string
		spec     AssertionSpec
		expected func(*testing.T, error)
	}{
		{
			name: "an assertion on nothing",
			spec: AssertionSpec{Check: "edge-backing-resolves"},
			expected: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrNoAssertionSubject)
			},
		},
		{
			name: "an assertion naming no check",
			spec: AssertionSpec{Subject: "geom:E-01"},
			expected: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrNoCheck)
			},
		},
		{
			name: "a subject nothing holds",
			spec: AssertionSpec{Subject: "geom:E-99", Check: "edge-backing-resolves"},
			expected: func(t *testing.T, err error) {
				var unknown UnknownEntityError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, ID("geom:E-99"), unknown.ID)
			},
		},
		{
			name: "a frame, which carries no assertion",
			spec: AssertionSpec{Subject: "frame:building", Check: "required-claim", Parameters: []string{"predicate position"}},
			expected: func(t *testing.T, err error) {
				var unassertable NotAssertableError
				require.ErrorAs(t, err, &unassertable)
				assert.Equal(t, ID("frame:building"), unassertable.ID)
				assert.Equal(t, "a frame", unassertable.What)
			},
		},
		{
			name: "a claim, which carries none either",
			spec: AssertionSpec{Subject: "survey:C-0001", Check: "required-claim", Parameters: []string{"predicate position"}},
			expected: func(t *testing.T, err error) {
				var unassertable NotAssertableError
				require.ErrorAs(t, err, &unassertable)
				assert.Equal(t, ID("survey:C-0001"), unassertable.ID)
				assert.Equal(t, "a claim", unassertable.What)
			},
		},
		{
			name: "a check the engine does not register, naming the ones it does",
			spec: AssertionSpec{Subject: "geom:L-10", Check: "loops-close"},
			expected: func(t *testing.T, err error) {
				var unregistered UnregisteredCheckError
				require.ErrorAs(t, err, &unregistered)
				assert.Equal(t, "loops-close", unregistered.Check)

				names := make([]string, 0, len(Checks()))
				for _, check := range Checks() {
					names = append(names, check.Name)
				}
				assert.Equal(t, names, unregistered.Registered)
			},
		},
		{
			name: "a parameter the check does not take",
			spec: AssertionSpec{
				Subject:    "geom:L-10",
				Check:      "boundary-loops-close",
				Parameters: []string{"tolerance boundary-closure", "position position", "limit frame-budget"},
			},
			expected: func(t *testing.T, err error) {
				var unknown UnknownParameterError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, "boundary-loops-close", unknown.Check)
				assert.Equal(t, "limit", unknown.Parameter)
				assert.Contains(t, unknown.Takes, "tolerance")
			},
		},
		{
			name: "a parameter the check requires and which is missing",
			spec: AssertionSpec{
				Subject:    "site:S-101",
				Check:      "stays-clear-of-zone",
				Parameters: []string{"tolerance boundary-closure", "position position"},
			},
			expected: func(t *testing.T, err error) {
				var missing MissingParameterError
				require.ErrorAs(t, err, &missing)
				assert.Equal(t, "zone", missing.Parameter)
				assert.Equal(t, ParameterID, missing.Want)
			},
		},
		{
			name: "a parameter written twice",
			spec: AssertionSpec{
				Subject:    "geom:L-10",
				Check:      "boundary-loops-close",
				Parameters: []string{"tolerance boundary-closure", "position position", "tolerance boundary-closure"},
			},
			expected: func(t *testing.T, err error) {
				var repeated RepeatedParameterError
				require.ErrorAs(t, err, &repeated)
				assert.Equal(t, "tolerance", repeated.Parameter)
			},
		},
		{
			name: "a tolerance written as a number rather than named",
			spec: AssertionSpec{
				Subject:    "geom:L-10",
				Check:      "boundary-loops-close",
				Parameters: []string{"tolerance 0.005", "position position"},
			},
			expected: func(t *testing.T, err error) {
				var value ParameterValueError
				require.ErrorAs(t, err, &value)
				assert.Equal(t, "tolerance", value.Parameter)
				assert.Equal(t, ParameterTolerance, value.Want)
			},
		},
		{
			name: "a predicate the registry does not declare",
			spec: AssertionSpec{
				Subject:    "geom:L-10",
				Check:      "boundary-loops-close",
				Parameters: []string{"tolerance boundary-closure", "position elevation"},
			},
			expected: func(t *testing.T, err error) {
				var value ParameterValueError
				require.ErrorAs(t, err, &value)
				assert.Equal(t, "position", value.Parameter)
				assert.Equal(t, ParameterPredicate, value.Want)
			},
		},
		{
			name: "text which is not one parameter",
			spec: AssertionSpec{
				Subject:    "geom:L-10",
				Check:      "boundary-loops-close",
				Parameters: []string{"tolerance boundary-closure) (position position"},
			},
			expected: func(t *testing.T, err error) {
				var malformed MalformedParameterError
				require.ErrorAs(t, err, &malformed)
				assert.Equal(t, "tolerance boundary-closure) (position position", malformed.Written)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, satisfiedFixture)

			testCase.expected(t, rejected(t, root, func(tx *Tx) error {
				return tx.AddAssertion(testCase.spec)
			}))
		})
	}
}

// TestTxAddAssertionReportsEveryProblemAtOnce is its own function because it is
// about how many reasons come back rather than which: the validation a load runs
// reports every problem with an assertion, and the write path is that
// validation.
func TestTxAddAssertionReportsEveryProblemAtOnce(t *testing.T) {
	root := copied(t, satisfiedFixture)

	err := rejected(t, root, func(tx *Tx) error {
		return tx.AddAssertion(AssertionSpec{
			Subject:    "site:S-101",
			Check:      "stays-clear-of-zone",
			Parameters: []string{"tolerance 0.005", "limit frame-budget", "position position"},
		})
	})

	var invalid InvalidAssertionError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, ID("site:S-101"), invalid.Subject)
	assert.Equal(t, "stays-clear-of-zone", invalid.Check)
	assert.Len(t, invalid.Errs, 3, "the number, the parameter it does not take and the one it is missing")

	var (
		value   ParameterValueError
		unknown UnknownParameterError
		missing MissingParameterError
	)
	assert.ErrorAs(t, err, &value)
	assert.ErrorAs(t, err, &unknown)
	assert.ErrorAs(t, err, &missing)
}

// TestTxAddAssertionIsRefusedInTheWordsALoadUses checks that every refusal of
// the write path is a diagnostic the load raises for the same assertion typed
// into a file by hand, which is what it means for the two to be one validation.
func TestTxAddAssertionIsRefusedInTheWordsALoadUses(t *testing.T) {
	root := copied(t, satisfiedFixture)
	spec := AssertionSpec{
		Subject:    "site:S-101",
		Check:      "stays-clear-of-zone",
		Parameters: []string{"tolerance 0.005", "limit frame-budget", "position position"},
	}

	tx := begin(t, root)
	defer func() { _ = tx.Close() }()

	err := tx.AddAssertion(spec)

	var invalid InvalidAssertionError
	require.ErrorAs(t, err, &invalid)

	form, err := spec.form()
	require.NoError(t, err)

	loaded := ValidateAssertion(form, tx.Graph().Registry())

	refused := make([]string, 0, len(invalid.Errs))
	for _, reason := range invalid.Errs {
		switch reason := reason.(type) {
		case ParameterValueError:
			refused = append(refused, reason.Diagnostic.Message)
		case UnknownParameterError:
			refused = append(refused, reason.Diagnostic.Message)
		case MissingParameterError:
			refused = append(refused, reason.Diagnostic.Message)
		default:
			t.Fatalf("unexpected reason %T", reason)
		}
	}

	messages := make([]string, 0, len(loaded))
	for _, diagnostic := range loaded {
		messages = append(messages, diagnostic.Message)
	}

	assert.Equal(t, messages, refused)
}

// TestTxAddAssertionIsRefusedByTheModelItWouldProduce checks that what is wrong
// with an assertion in this model, rather than with the assertion itself, is
// refused at commit with the diagnostics a load of the result would raise.
func TestTxAddAssertionIsRefusedByTheModelItWouldProduce(t *testing.T) {
	testCases := []struct {
		name     string
		spec     AssertionSpec
		expected string
	}{
		{
			name:     "a check which cannot examine the subject's form",
			spec:     AssertionSpec{Subject: "site:S-101", Check: "edge-backing-resolves"},
			expected: "expected an assertion naming a check which applies to a node, found edge-backing-resolves",
		},
		{
			name: "a check which cannot examine the subject's geometry",
			spec: AssertionSpec{
				Subject:    "site:A-01",
				Check:      "stays-clear-of-zone",
				Parameters: []string{"zone site:Z-90", "tolerance boundary-closure", "position position"},
			},
			expected: "expected an assertion naming a check which applies to the geometry site:A-01 has",
		},
		{
			name: "a parameter naming an id nothing holds",
			spec: AssertionSpec{
				Subject:    "site:S-101",
				Check:      "stays-clear-of-zone",
				Parameters: []string{"zone site:Z-99", "tolerance boundary-closure", "position position"},
			},
			expected: "found site:Z-99, which nothing answers to",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copied(t, satisfiedFixture)

			refusedAtCommit(t, root, func() *Tx { return begin(t, root) }, testCase.spec, testCase.expected)
		})
	}
}

// TestTxAddAssertionRefusesARestatement is its own function because the engine
// registers no check carrying a value of the subject's own, so the restatement
// rule is exercised with a set which adds one — through the transaction, and so
// through the commit every change goes through.
func TestTxAddAssertionRefusesARestatement(t *testing.T) {
	root := copied(t, claimedFixture)

	against := func() *Tx {
		tx := &Tx{root: root, files: make(map[string]*staged), checks: assertionChecks()}

		var loaded Diagnostics
		loaded.Add(tx.read()...)
		require.False(t, loaded.HasErrors(), "the fixture loads against the set: %v", loaded)

		return tx
	}

	refusedAtCommit(t, root, against, AssertionSpec{
		Subject:    "site:S-101",
		Check:      "claimed-value-is",
		Parameters: []string{"predicate width", "is 4.2"},
	}, "found one which restates the width it already claims")
}

// refusedAtCommit writes the assertion, requiring the transaction to accept it,
// and requires the commit to refuse the model it would produce with a
// diagnostic saying so — writing nothing.
func refusedAtCommit(t *testing.T, root string, begin func() *Tx, spec AssertionSpec, expected string) {
	t.Helper()

	before := contents(t, root)
	tx := begin()

	require.NoError(t, tx.AddAssertion(spec))

	out, diags, err := tx.Commit()
	require.NoError(t, err)
	assert.Empty(t, out.Files, "a refused change describes nothing")
	assert.Equal(t, before, contents(t, root), "a refused change writes nothing")

	var collected Diagnostics
	collected.Add(diags...)
	require.True(t, collected.HasErrors(), "the change was refused")

	assert.True(t, slices.ContainsFunc(diags, func(diagnostic Diagnostic) bool {
		return strings.Contains(diagnostic.Message, expected)
	}), "the diagnostics say what the assertion produced: %v", diags)
}

func TestTxAddAssertionOnAFinishedTransaction(t *testing.T) {
	tx := begin(t, copied(t, satisfiedFixture))

	_, _, err := tx.Commit()
	require.NoError(t, err)

	assert.ErrorIs(t, tx.AddAssertion(AssertionSpec{Subject: "geom:E-01", Check: "edge-backing-resolves"}), ErrFinished)
}

func TestParseParameter(t *testing.T) {
	testCases := []struct {
		name     string
		written  string
		expected string
	}{
		{name: "reads a name and one value", written: "tolerance boundary-closure", expected: "tolerance boundary-closure"},
		{name: "reads an id", written: "zone site:Z-90", expected: "zone site:Z-90"},
		{name: "reads a number", written: "is 4.2", expected: "is 4.2"},
		{name: "reads a string", written: `note "north wall"`, expected: `note "north wall"`},
		{name: "reads more than one value", written: "beside site:S-101 site:S-102", expected: "beside site:S-101 site:S-102"},
		{name: "reads a name with no value", written: "strict", expected: "strict"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			parameter, err := ParseParameter(testCase.written)
			require.NoError(t, err)

			arguments := arguments([]*Node{parameter})
			require.Len(t, arguments, 1)
			assert.Equal(t, "("+testCase.expected+")", arguments[0].String())
		})
	}
}

func TestParseParameterRefusesWhatIsNotOneParameter(t *testing.T) {
	testCases := []struct {
		name    string
		written string
	}{
		{name: "nothing at all", written: ""},
		{name: "two forms", written: "tolerance a) (position b"},
		{name: "a value with no name", written: "0.005"},
		{name: "a form where the name belongs", written: "(tolerance a)"},
		{name: "an unbalanced parenthesis", written: "tolerance (a"},
		{name: "a comment", written: "tolerance a ; the closure tolerance"},
		{name: "a comment inside a value", written: "beside (site:S-101 #| and |# site:S-102)"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParseParameter(testCase.written)

			var malformed MalformedParameterError
			require.ErrorAs(t, err, &malformed)
			assert.Equal(t, testCase.written, malformed.Written)
		})
	}
}
