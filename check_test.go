// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureCheck is a check a test registers, which is every check the engine
// does not: the registered set is closed, so exercising a parameter type no
// check happens to use today needs a check assembled for the purpose.
type fixtureCheck struct {
	declaration CheckDeclaration
}

// Declare implements [Check].
func (c fixtureCheck) Declare() CheckDeclaration { return c.declaration }

// declaredOnly is a registered check with its implementation taken off: it
// declares exactly what the engine's does and nothing runs it.
//
// Every layer above the registry — validating an assertion at load, listing what
// a model may write, binding a rule to the instances it applies to — reads the
// declaration and runs nothing, and a check which is declared and not yet
// implemented is what those layers have to keep working against. The engine
// implements more of its set with every story, so a test which needs that state
// makes it rather than borrowing whichever check happens not to be implemented
// this month.
//
// The embedded field is a [Check] rather than the concrete type, which is the
// whole trick: only Declare is promoted through it, so a Run on the check inside
// is not a Run on this.
type declaredOnly struct{ Check }

// parsedForm parses one written form, which is what an assertion is validated
// from.
func parsedForm(t *testing.T, written string) *Node {
	t.Helper()

	file, err := Parse("entities/level-1.dfc", strings.NewReader(written))
	require.NoError(t, err)
	require.Len(t, file.Nodes, 1)

	return file.Nodes[0]
}

// checkedAgainst validates one written assertion against a set assembled for
// the test, and returns the message of every diagnostic in the order the pass
// found them.
func checkedAgainst(t *testing.T, set *checkSet, registry *Registry, written string) []string {
	t.Helper()

	v := &checkValidator{set: set, registry: registry}
	v.assertion(parsedForm(t, written))

	messages := make([]string, 0, len(v.diags))
	for _, diagnostic := range v.diags {
		messages = append(messages, diagnostic.Message)
	}
	return messages
}

// checked validates one written assertion against the registered set and the
// vocabulary of the worked example.
func checked(t *testing.T, written string) []Diagnostic {
	t.Helper()

	registry, _ := LoadRegistry(registryFixture("valid"))

	return ValidateAssertion(parsedForm(t, written), registry)
}

// messagesOf is the message of every diagnostic, in the order the pass found
// them.
func messagesOf(diags []Diagnostic) []string {
	messages := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		messages = append(messages, diagnostic.Message)
	}
	return messages
}

func TestValidateAssertion(t *testing.T) {
	testCases := []struct {
		name     string
		written  string
		expected []string
	}{
		{
			name:     "accepts an assertion whose parameters are the ones the check takes",
			written:  "(assert boundary-loops-close (tolerance boundary-closure))",
			expected: []string{},
		},
		{
			name:     "accepts a check which takes no parameter at all",
			written:  "(assert within-resolves)",
			expected: []string{},
		},
		{
			name:    "names the assertion and the check when the name is one nothing registers",
			written: "(assert boundary-loops-cluse (tolerance boundary-closure))",
			expected: []string{
				"expected a registered check name after the assert tag, found boundary-loops-cluse, which the engine registers no check under",
			},
		},
		{
			name:    "names the tag it was written with, so an invariant does not read as an assertion",
			written: "(invariant nothing-of-the-sort)",
			expected: []string{
				"expected a registered check name after the invariant tag, found nothing-of-the-sort, which the engine registers no check under",
			},
		},
		{
			name:    "names the parameter a check requires and the assertion left out",
			written: "(assert boundary-loops-close)",
			expected: []string{
				"expected a (tolerance ...) parameter of the check boundary-loops-close, found none",
			},
		},
		{
			name:    "names a parameter written which the check does not take",
			written: "(assert within-resolves (tolerance boundary-closure))",
			expected: []string{
				"expected a parameter of the check within-resolves, found (tolerance ...), which is not one of them",
			},
		},
		{
			name:    "names both of a parameter written twice",
			written: "(assert boundary-loops-close (tolerance boundary-closure) (tolerance boundary-closure))",
			expected: []string{
				"expected one (tolerance ...) parameter of the check boundary-loops-close, found a second",
			},
		},
		{
			name:    "names the registry when a tolerance parameter names nothing declared",
			written: "(assert boundary-loops-close (tolerance snug))",
			expected: []string{
				"expected a declared tolerance, found snug, which no registry file declares",
			},
		},
		{
			name:    "refuses a numeric literal written where a tolerance name belongs",
			written: "(assert boundary-loops-close (tolerance 0.005))",
			expected: []string{
				"expected a declared tolerance name after the tolerance tag, found the number 0.005",
			},
		},
		{
			name:    "names the parameter when its value is a datum of another sort",
			written: `(assert required-claim (predicate "width"))`,
			expected: []string{
				`expected a declared predicate name, found the string "width"`,
			},
		},
		{
			name:    "reports a parameter written with no value at all",
			written: "(assert required-claim (predicate))",
			expected: []string{
				"expected one value, a declared predicate name after the predicate tag, found none",
			},
		},
		{
			name:    "reports a second value on a parameter which takes one",
			written: "(assert required-claim (predicate width colour))",
			expected: []string{
				"expected one value, a declared predicate name after the predicate tag, found 2",
			},
		},
		{
			name:    "reports every independent problem in one pass",
			written: "(assert boundary-loops-close (tolarance boundary-closure) (predicate width))",
			expected: []string{
				"expected a parameter of the check boundary-loops-close, found (tolarance ...), which is not one of them",
				"expected a parameter of the check boundary-loops-close, found (predicate ...), which is not one of them",
				"expected a (tolerance ...) parameter of the check boundary-loops-close, found none",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := messagesOf(checked(t, testCase.written))

			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestValidateAssertionHints is its own function because its assertion is the
// other half of a diagnostic: the message says what is wrong and the hint says
// what to do about it, and the hint is where the nearest registered name and
// the closed registry are said out loud.
func TestValidateAssertionHints(t *testing.T) {
	testCases := []struct {
		name     string
		written  string
		expected string
	}{
		{
			name:     "offers the nearest registered check for a misspelled name",
			written:  "(assert boundary-loops-cluse (tolerance boundary-closure))",
			expected: "did you mean boundary-loops-close?",
		},
		{
			name:    "lists the registered checks when nothing written is close to one",
			written: "(assert everything-is-fine)",
			expected: "the check registry is closed and compiled into the engine; the registered checks are " +
				"boundary-loops-close, claim-agrees-with-geometry, contained-areas-do-not-overlap, " +
				"contained-areas-sum, cross-frame-budget-holds, edge-backing-resolves, edge-endpoints-differ, " +
				"ground-to-grid-stated, required-claim, sits-inside, stays-clear-of-zone, within-resolves and " +
				"zone-members-resolve",
		},
		{
			name:     "offers the nearest parameter the check does take",
			written:  "(assert boundary-loops-close (tolarance boundary-closure))",
			expected: "did you mean (tolerance ...)?",
		},
		{
			name:     "says a check takes no parameter rather than listing none",
			written:  "(assert within-resolves (tolerance boundary-closure))",
			expected: "within-resolves takes no parameter",
		},
		{
			name:    "says where a tolerance belongs when one was written as a number",
			written: "(assert boundary-loops-close (tolerance 0.005))",
			expected: "a tolerance is registry data rather than a number written where it is used: declare it with " +
				"(tolerance <name> (value <magnitude> <unit>)) and name it here, so that how close is close enough " +
				"is one decision in one place",
		},
		{
			name:     "says what a required parameter is for when it is missing",
			written:  "(assert required-claim)",
			expected: "required-claim takes a declared predicate name: The predicate a claim must be written under.",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			diags := checked(t, testCase.written)

			require.NotEmpty(t, diags)
			assert.Equal(t, testCase.expected, diags[0].Hint)
		})
	}
}

// TestValidateAssertionRelated is its own function because a diagnostic which
// points at two places is asserted on differently from one which points at one.
func TestValidateAssertionRelated(t *testing.T) {
	diags := checked(t, "(assert boundary-loops-close\n  (tolerance boundary-closure)\n  (tolerance boundary-closure))")

	require.Len(t, diags, 1)
	require.Len(t, diags[0].Related, 1)

	assert.Equal(t, 3, diags[0].Span.Start.Line)
	assert.Equal(t, 2, diags[0].Related[0].Span.Start.Line)
}

// TestValidateAssertionParameterTypes is its own function because it is about
// the parameter vocabulary rather than about the registered checks: every sort
// of datum a parameter may take is exercised, including the ones no registered
// check happens to use.
func TestValidateAssertionParameterTypes(t *testing.T) {
	set := newCheckSet(fixtureCheck{CheckDeclaration{
		Name:        "every-parameter",
		Description: "Takes one parameter of every sort a parameter may be.",
		Parameters: []CheckParameter{
			{Name: "of", Type: ParameterID, Description: "An id."},
			{Name: "minimum", Type: ParameterReal, Description: "A real number."},
			{Name: "note", Type: ParameterString, Description: "A string."},
			{Name: "strict", Type: ParameterBoolean, Description: "A boolean."},
			{Name: "kinds", Type: ParameterKind, Repeated: true, Description: "One or more kinds."},
			{Name: "shape", Type: ParameterGeometry, Description: "A geometry form."},
			{Name: "instance-of", Type: ParameterTypeName, Description: "A declared type."},
			{Name: "predicate", Type: ParameterPredicate, Description: "A declared predicate."},
			{Name: "measured-in", Type: ParameterFrame, Description: "A declared frame."},
			{Name: "tolerance", Type: ParameterTolerance, Description: "A declared tolerance."},
		},
		Forms: []SubjectForm{SubjectNode},
	}})

	registry, _ := LoadRegistry(registryFixture("valid"))

	testCases := []struct {
		name     string
		written  string
		expected []string
	}{
		{
			name: "accepts one value of every sort a parameter may take",
			written: `(assert every-parameter
  (of site:S-101)
  (minimum 0.9)
  (note "Egress route")
  (strict #t)
  (kinds Space Element)
  (shape area)
  (instance-of MeetingRoom)
  (predicate width)
  (measured-in frame:survey-grid)
  (tolerance boundary-closure))`,
			expected: []string{},
		},
		{
			name:     "reads a repeated parameter written as one parenthesised list",
			written:  "(assert every-parameter (kinds (Space Element)))",
			expected: []string{},
		},
		{
			name:     "reads a repeated parameter written with one value",
			written:  "(assert every-parameter (kinds Space))",
			expected: []string{},
		},
		{
			name:     "reports a repeated parameter written with no value",
			written:  "(assert every-parameter (kinds ()))",
			expected: []string{"expected one or more values, each a kind after the kinds tag, found none"},
		},
		{
			name:     "names the closed set when a kind is not one of them",
			written:  "(assert every-parameter (kinds Space Cupboard))",
			expected: []string{"expected a kind, found Cupboard"},
		},
		{
			name:     "names the closed set when a geometry form is not one of them",
			written:  "(assert every-parameter (shape blob))",
			expected: []string{"expected a geometry form, found blob"},
		},
		{
			name:     "reports a whole number written where a real belongs",
			written:  "(assert every-parameter (minimum 1))",
			expected: []string{"expected a real number, found the number 1"},
		},
		{
			name:     "reports a symbol written where a string belongs",
			written:  "(assert every-parameter (note egress))",
			expected: []string{"expected a string, found the symbol egress"},
		},
		{
			name:     "reports a value written where a boolean belongs",
			written:  `(assert every-parameter (strict "yes"))`,
			expected: []string{`expected a boolean, found the string "yes"`},
		},
		{
			name:     "reports an id whose namespace no registry file declares",
			written:  "(assert every-parameter (of hvac:S-101))",
			expected: []string{"expected a declared namespace, found hvac, which no registry file declares"},
		},
		{
			name:     "reports something which is not an id at all",
			written:  "(assert every-parameter (of 12.0))",
			expected: []string{"expected an id, found the number 12"},
		},
		{
			name:     "names the registry when a type parameter names nothing declared",
			written:  "(assert every-parameter (instance-of Broomcupboard))",
			expected: []string{"expected a declared type, found Broomcupboard, which no registry file declares"},
		},
		{
			name:     "names the registry when a frame parameter names nothing declared",
			written:  "(assert every-parameter (measured-in frame:orbital))",
			expected: []string{"expected a declared frame, found frame:orbital, which no registry file declares"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := checkedAgainst(t, set, registry, testCase.written)

			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestValidateAssertionWithoutRegistry is its own function because it is about
// the caller rather than about the file: a model whose registry has not been
// written yet declares nothing, and every parameter naming an entry of it is
// reported rather than crashed on.
func TestValidateAssertionWithoutRegistry(t *testing.T) {
	got := messagesOf(ValidateAssertion(parsedForm(t, "(assert boundary-loops-close (tolerance boundary-closure))"), nil))

	assert.Equal(t, []string{"expected a declared tolerance, found boundary-closure, which no registry file declares"}, got)
}

func TestChecks(t *testing.T) {
	t.Run("returns every registered check in name order", func(t *testing.T) {
		var names []string
		for _, declared := range Checks() {
			names = append(names, declared.Name)
		}

		assert.Equal(t, []string{
			"boundary-loops-close",
			"claim-agrees-with-geometry",
			"contained-areas-do-not-overlap",
			"contained-areas-sum",
			"cross-frame-budget-holds",
			"edge-backing-resolves",
			"edge-endpoints-differ",
			"ground-to-grid-stated",
			"required-claim",
			"sits-inside",
			"stays-clear-of-zone",
			"within-resolves",
			"zone-members-resolve",
		}, names)
		assert.True(t, slices.IsSorted(names))
	})

	t.Run("carries the parameters of a check, so a listing can be written from it", func(t *testing.T) {
		declared, ok := LookupCheck("boundary-loops-close")

		require.True(t, ok)
		require.Len(t, declared.Parameters, 2)
		assert.Equal(t, "tolerance", declared.Parameters[0].Name)
		assert.Equal(t, ParameterTolerance, declared.Parameters[0].Type)
		assert.True(t, declared.Parameters[0].Required)
		assert.NotEmpty(t, declared.Parameters[0].Description)
		assert.NotEmpty(t, declared.Description)
	})

	t.Run("registers nothing under a name it does not declare", func(t *testing.T) {
		_, ok := LookupCheck("clearance-at-least")

		assert.False(t, ok)
	})

	t.Run("hands out a copy, so a caller cannot write to the registry", func(t *testing.T) {
		first, ok := LookupCheck("boundary-loops-close")
		require.True(t, ok)

		first.Parameters[0].Name = "slack"
		first.Forms[0] = SubjectVertex

		second, ok := LookupCheck("boundary-loops-close")
		require.True(t, ok)
		assert.Equal(t, "tolerance", second.Parameters[0].Name)
		assert.Equal(t, SubjectNode, second.Forms[0])
	})

	t.Run("names every tolerance it uses and holds no numeric one", func(t *testing.T) {
		for _, declared := range Checks() {
			for _, parameter := range declared.Parameters {
				if !tolerant(parameter.Name) {
					continue
				}
				assert.Equal(t, ParameterTolerance, parameter.Type, "%s takes %s as a name", declared.Name, parameter.Name)
			}
		}
	})
}

func TestCheckDeclarationApplicability(t *testing.T) {
	declared, ok := LookupCheck("boundary-loops-close")
	require.True(t, ok)

	t.Run("applies to the forms it declares and to no others", func(t *testing.T) {
		assert.True(t, declared.PermitsForm(SubjectNode))
		assert.True(t, declared.PermitsForm(SubjectLoop))
		assert.False(t, declared.PermitsForm(SubjectVertex))
	})

	t.Run("applies to every kind when it restricts none", func(t *testing.T) {
		for _, kind := range Kinds() {
			assert.True(t, declared.PermitsKind(kind))
		}
	})

	t.Run("applies to the geometry forms it can measure and not to a subject with none", func(t *testing.T) {
		assert.True(t, declared.PermitsGeometry(GeometryArea))
		assert.False(t, declared.PermitsGeometry(GeometryLine))
		assert.False(t, declared.PermitsGeometry(""))
	})

	t.Run("applies to any geometry when it restricts none", func(t *testing.T) {
		within, ok := LookupCheck("within-resolves")

		require.True(t, ok)
		assert.True(t, within.PermitsGeometry(GeometryLine))
		assert.True(t, within.PermitsGeometry(""))
	})
}

// TestNewCheckSetRefuses is the registration half of the closed registry: a
// declaration the set cannot hold is a mistake in engine code, and it is raised
// where it is made rather than carried to somebody who cannot act on it.
func TestNewCheckSetRefuses(t *testing.T) {
	valid := CheckDeclaration{
		Name:        "well-formed",
		Description: "A check which is registered without complaint.",
		Forms:       []SubjectForm{SubjectNode},
	}

	testCases := []struct {
		name     string
		checks   []Check
		expected string
	}{
		{
			name: "a name which could not be written in a file",
			checks: []Check{fixtureCheck{CheckDeclaration{
				Name:        "not one symbol",
				Description: "A check nothing could name.",
				Forms:       []SubjectForm{SubjectNode},
			}}},
			expected: "not one symbol",
		},
		{
			name: "a check with no description",
			checks: []Check{fixtureCheck{CheckDeclaration{
				Name:  "undescribed",
				Forms: []SubjectForm{SubjectNode},
			}}},
			expected: "undescribed",
		},
		{
			name: "a check which applies to no form",
			checks: []Check{fixtureCheck{CheckDeclaration{
				Name:        "applies-to-nothing",
				Description: "A check nothing could be written on.",
			}}},
			expected: "applies-to-nothing",
		},
		{
			name: "a check which applies to a form the format does not have",
			checks: []Check{fixtureCheck{CheckDeclaration{
				Name:        "applies-to-a-storey",
				Description: "A check written on a form which is not one.",
				Forms:       []SubjectForm{"storey"},
			}}},
			expected: "applies-to-a-storey",
		},
		{
			name: "a check which restricts kinds and does not apply to a node",
			checks: []Check{fixtureCheck{CheckDeclaration{
				Name:        "kinds-on-a-loop",
				Description: "A check restricting an axis its subject does not have.",
				Forms:       []SubjectForm{SubjectLoop},
				Kinds:       []Kind{KindSpace},
			}}},
			expected: "kinds-on-a-loop",
		},
		{
			name: "a parameter declared twice",
			checks: []Check{fixtureCheck{CheckDeclaration{
				Name:        "twice-over",
				Description: "A check whose parameter is ambiguous.",
				Parameters: []CheckParameter{
					{Name: "of", Type: ParameterID, Description: "An id."},
					{Name: "of", Type: ParameterID, Description: "The same id."},
				},
				Forms: []SubjectForm{SubjectNode},
			}}},
			expected: "twice-over",
		},
		{
			name: "a parameter with no description",
			checks: []Check{fixtureCheck{CheckDeclaration{
				Name:        "undescribed-parameter",
				Description: "A check whose parameter nobody could use.",
				Parameters:  []CheckParameter{{Name: "of", Type: ParameterID}},
				Forms:       []SubjectForm{SubjectNode},
			}}},
			expected: "undescribed-parameter",
		},
		{
			name: "a parameter of a sort which is not one",
			checks: []Check{fixtureCheck{CheckDeclaration{
				Name:        "unknown-sort",
				Description: "A check whose parameter takes nothing the format writes.",
				Parameters:  []CheckParameter{{Name: "of", Type: "polygon", Description: "A polygon."}},
				Forms:       []SubjectForm{SubjectNode},
			}}},
			expected: "unknown-sort",
		},
		{
			name: "a tolerance parameter which takes a number",
			checks: []Check{fixtureCheck{CheckDeclaration{
				Name:        "literal-tolerance",
				Description: "A check carrying its tolerance as a number.",
				Parameters:  []CheckParameter{{Name: "tolerance", Type: ParameterReal, Description: "How close is close enough."}},
				Forms:       []SubjectForm{SubjectNode},
			}}},
			expected: "literal-tolerance",
		},
		{
			name: "a tolerance parameter under a compound name which takes a number",
			checks: []Check{fixtureCheck{CheckDeclaration{
				Name:        "literal-chord-tolerance",
				Description: "A check carrying a second tolerance as a number.",
				Parameters:  []CheckParameter{{Name: "chord-tolerance", Type: ParameterReal, Description: "How far a chord may deviate."}},
				Forms:       []SubjectForm{SubjectNode},
			}}},
			expected: "literal-chord-tolerance",
		},
		{
			name:     "one name registered twice",
			checks:   []Check{fixtureCheck{valid}, fixtureCheck{valid}},
			expected: "well-formed",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			defer func() {
				raised := recover()
				require.NotNil(t, raised, "registering %s panics", testCase.expected)

				err, ok := raised.(error)
				require.True(t, ok, "the value raised is an error")

				var invalid invalidCheckError
				require.True(t, errors.As(err, &invalid))
				assert.Equal(t, testCase.expected, invalid.Check)
				assert.NotEmpty(t, invalid.Reason)
			}()

			newCheckSet(testCase.checks...)
		})
	}
}

// TestNewCheckSetAccepts is its own function because its assertion is the other
// shape: a well-formed registration has nothing to raise, and what is worth
// asserting is that the set holds what it was given.
func TestNewCheckSetAccepts(t *testing.T) {
	set := newCheckSet(fixtureCheck{CheckDeclaration{
		Name:        "one-of-each",
		Description: "A check registered by implementing one interface.",
		Parameters:  []CheckParameter{{Name: "predicate", Type: ParameterPredicate, Required: true, Description: "A declared predicate."}},
		Forms:       []SubjectForm{SubjectNode},
		Kinds:       []Kind{KindSpace},
		Geometries:  []Geometry{GeometryArea},
	}})

	declared, ok := set.lookup("one-of-each")

	require.True(t, ok)
	assert.Equal(t, "one-of-each", declared.Name)
	assert.Len(t, set.all(), 1)
	assert.True(t, declared.PermitsKind(KindSpace))
	assert.False(t, declared.PermitsKind(KindZone))
}
