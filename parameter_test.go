// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// everyParameter is a check declaring one parameter of every sort a parameter
// may take, which is what lets every encoding be exercised whether or not a
// registered check happens to use it.
var everyParameter = CheckDeclaration{
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
}

// writtenArgument reads one parameter form, written on its own, as the
// argument an assertion carrying it would hold.
func writtenArgument(t *testing.T, written string) Argument {
	t.Helper()

	read := arguments([]*Node{parsedForm(t, written)})
	require.Len(t, read, 1)

	return read[0]
}

// writtenParameter prints a parameter back as the form it was read from, the
// way [Argument.String] prints an argument.
func writtenParameter(p Parameter) string {
	written := []string{p.Name}

	for _, value := range p.Values {
		switch value := value.(type) {
		case float64:
			written = append(written, decimal(value))
		case bool:
			if value {
				written = append(written, "#t")
			} else {
				written = append(written, "#f")
			}
		case string:
			if p.Type == ParameterString {
				written = append(written, quoteText(value))
			} else {
				written = append(written, value)
			}
		default:
			written = append(written, "…")
		}
	}

	return "(" + strings.Join(written, " ") + ")"
}

func TestArgumentParameter(t *testing.T) {
	testCases := []struct {
		name     string
		written  string
		expected Parameter
	}{
		{
			name:     "reads an id as the id written",
			written:  "(of site:S-101)",
			expected: Parameter{Name: "of", Type: ParameterID, Values: []any{"site:S-101"}},
		},
		{
			name:     "reads a real as a number",
			written:  "(minimum 0.9)",
			expected: Parameter{Name: "minimum", Type: ParameterReal, Values: []any{0.9}},
		},
		{
			name:     "reads a string as its text, unquoted",
			written:  `(note "Egress \"route\" A")`,
			expected: Parameter{Name: "note", Type: ParameterString, Values: []any{`Egress "route" A`}},
		},
		{
			name:     "reads a true boolean as a boolean",
			written:  "(strict #t)",
			expected: Parameter{Name: "strict", Type: ParameterBoolean, Values: []any{true}},
		},
		{
			name:     "reads a false boolean as a boolean",
			written:  "(strict #f)",
			expected: Parameter{Name: "strict", Type: ParameterBoolean, Values: []any{false}},
		},
		{
			name:     "reads a kind as its name",
			written:  "(kinds Space)",
			expected: Parameter{Name: "kinds", Type: ParameterKind, Values: []any{"Space"}},
		},
		{
			name:     "reads a geometry form as its name",
			written:  "(shape area)",
			expected: Parameter{Name: "shape", Type: ParameterGeometry, Values: []any{"area"}},
		},
		{
			name:     "reads a type as its name",
			written:  "(instance-of MeetingRoom)",
			expected: Parameter{Name: "instance-of", Type: ParameterTypeName, Values: []any{"MeetingRoom"}},
		},
		{
			name:     "reads a predicate as its name",
			written:  "(predicate width)",
			expected: Parameter{Name: "predicate", Type: ParameterPredicate, Values: []any{"width"}},
		},
		{
			name:     "reads a frame as its id",
			written:  "(measured-in frame:survey-grid)",
			expected: Parameter{Name: "measured-in", Type: ParameterFrame, Values: []any{"frame:survey-grid"}},
		},
		{
			name:     "reads a tolerance as its name, unresolved",
			written:  "(tolerance boundary-closure)",
			expected: Parameter{Name: "tolerance", Type: ParameterTolerance, Values: []any{"boundary-closure"}},
		},
		{
			name:     "reads a repeated parameter written as a sequence as one value apiece",
			written:  "(kinds Space Element)",
			expected: Parameter{Name: "kinds", Type: ParameterKind, Values: []any{"Space", "Element"}},
		},
		{
			name:     "reads a repeated parameter written as one parenthesised list as the same values",
			written:  "(kinds (Space Element))",
			expected: Parameter{Name: "kinds", Type: ParameterKind, Values: []any{"Space", "Element"}},
		},
		{
			name:     "gives no value for a whole number written where a real belongs",
			written:  "(minimum 1)",
			expected: Parameter{Name: "minimum", Type: ParameterReal, Values: []any{nil}},
		},
		{
			name:     "gives no value for a symbol written where a string belongs",
			written:  "(note egress)",
			expected: Parameter{Name: "note", Type: ParameterString, Values: []any{nil}},
		},
		{
			name:     "gives no value for a string written where a boolean belongs",
			written:  `(strict "yes")`,
			expected: Parameter{Name: "strict", Type: ParameterBoolean, Values: []any{nil}},
		},
		{
			name:     "gives no value for something which is not an id",
			written:  "(of 12.0)",
			expected: Parameter{Name: "of", Type: ParameterID, Values: []any{nil}},
		},
		{
			name:     "gives no value for a string written where a predicate belongs",
			written:  `(predicate "width")`,
			expected: Parameter{Name: "predicate", Type: ParameterPredicate, Values: []any{nil}},
		},
		{
			name:     "gives no value for a kind outside the closed set, and still reads the rest",
			written:  "(kinds Space Cupboard)",
			expected: Parameter{Name: "kinds", Type: ParameterKind, Values: []any{"Space", nil}},
		},
		{
			name:     "gives no value for a geometry form outside the closed set",
			written:  "(shape blob)",
			expected: Parameter{Name: "shape", Type: ParameterGeometry, Values: []any{nil}},
		},
		{
			name:     "gives no value for a list written where one value belongs",
			written:  "(shape (area line))",
			expected: Parameter{Name: "shape", Type: ParameterGeometry, Values: []any{nil}},
		},
		{
			name:     "gives no type and no values for a parameter the check does not declare",
			written:  "(colour red)",
			expected: Parameter{Name: "colour", Values: []any{nil}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := writtenArgument(t, testCase.written).parameter(everyParameter)

			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestParameterJSON is its own function because it is about the bytes a caller
// reads rather than the values a parameter holds: which keys are written, and
// what each sort of value is written as.
func TestParameterJSON(t *testing.T) {
	testCases := []struct {
		name     string
		written  string
		expected string
	}{
		{
			name:     "writes a name as a string",
			written:  "(tolerance boundary-closure)",
			expected: `{"name":"tolerance","type":"tolerance","values":["boundary-closure"]}`,
		},
		{
			name:     "writes a real as a number",
			written:  "(minimum 0.9)",
			expected: `{"name":"minimum","type":"real","values":[0.9]}`,
		},
		{
			name:     "writes a boolean as a boolean",
			written:  "(strict #t)",
			expected: `{"name":"strict","type":"boolean","values":[true]}`,
		},
		{
			name:     "writes a value which is not an atom of the declared type as null",
			written:  "(minimum 1)",
			expected: `{"name":"minimum","type":"real","values":[null]}`,
		},
		{
			name:     "leaves the type out where the check declares no parameter by that name",
			written:  "(colour red)",
			expected: `{"name":"colour","values":[null]}`,
		},
		{
			name:     "writes the values of a repeated parameter as one array however they were written",
			written:  "(kinds (Space Element))",
			expected: `{"name":"kinds","type":"kind","values":["Space","Element"]}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			encoded, err := json.Marshal(writtenArgument(t, testCase.written).parameter(everyParameter))

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, string(encoded))
		})
	}
}

// checkFixtureRoots is every fixture model under testdata/checks, by root.
func checkFixtureRoots(t *testing.T) []string {
	t.Helper()

	var roots []string
	err := filepath.WalkDir(filepath.Join("testdata", "checks"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == "registry.dfc" {
			roots = append(roots, filepath.Dir(path))
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, roots)

	return roots
}

// TestRuleParametersRoundTrip is its own function because it is a property of
// every rule the fixtures hold rather than a table of literal cases: a
// parameter read as data and printed back is the parameter as it was written,
// and that printing read again is the same data.
func TestRuleParametersRoundTrip(t *testing.T) {
	for _, root := range checkFixtureRoots(t) {
		t.Run(filepath.ToSlash(root), func(t *testing.T) {
			graph, _ := LoadGraph(root)
			require.NotNil(t, graph)

			for _, rule := range graph.Rules() {
				parameters := rule.Parameters()
				require.Len(t, parameters, len(rule.Arguments), rule.String())

				for i, parameter := range parameters {
					written := writtenParameter(parameter)
					assert.Equal(t, rule.Arguments[i].String(), written, rule.String())

					reread := writtenArgument(t, written).parameter(rule.Check)
					assert.Equal(t, parameter, reread, rule.String())
				}
			}
		})
	}
}

// TestRuleParametersNamesEveryTypeTheFixturesUse is its own function because it
// is about the fixtures rather than about a rule: the round trip above is only
// as strong as the sorts of value the fixtures exercise.
func TestRuleParametersNamesEveryTypeTheFixturesUse(t *testing.T) {
	seen := make(map[ParameterType]bool)

	for _, root := range checkFixtureRoots(t) {
		graph, _ := LoadGraph(root)
		require.NotNil(t, graph)

		for _, rule := range graph.Rules() {
			for _, parameter := range rule.Parameters() {
				assert.NotEmpty(t, parameter.Type, "%s: %s", root, rule)
				assert.NotContains(t, parameter.Values, nil, "%s: %s", root, rule)
				seen[parameter.Type] = true
			}
		}
	}

	for _, expected := range []ParameterType{ParameterID, ParameterKind, ParameterTypeName, ParameterPredicate, ParameterFrame, ParameterTolerance} {
		assert.True(t, seen[expected], "no fixture writes a %s parameter", expected)
	}
}

// TestRuleParametersIsNilWithoutParameters is its own function because it is
// about absence: a rule written with no parameters has none to report, which is
// what keeps them off the wire.
func TestRuleParametersIsNilWithoutParameters(t *testing.T) {
	assert.Nil(t, Rule{Check: everyParameter}.Parameters())
}

// TestRuleReportsItsParametersEverywhereItIsNamed is its own function because
// it is about the four places a run names a rule rather than about reading one
// parameter: every one of them carries the parameters the rule reports, so no
// two of them can disagree about what the rule was written with.
func TestRuleReportsItsParametersEverywhereItIsNamed(t *testing.T) {
	reported := make(map[string]int)

	for _, root := range checkFixtureRoots(t) {
		graph, _ := LoadGraph(root)
		require.NotNil(t, graph)

		for _, rule := range graph.Rules() {
			expected := rule.Parameters()
			got := rule.judge()

			for _, violation := range got.violations {
				assert.Equal(t, expected, violation.Parameters, rule.String())
				reported["violations"]++
			}
			for _, band := range got.bands {
				assert.Equal(t, expected, band.Parameters, rule.String())
				reported["bands"]++
			}
			for _, chorded := range got.chorded {
				assert.Equal(t, expected, chorded.Parameters, rule.String())
				reported["chorded"]++
			}
			if got.drawn != nil {
				assert.Equal(t, expected, got.drawn.Parameters, rule.String())
				reported["drawn"]++
			}
		}
	}

	// Each of the four is exercised by some fixture, or the assertions above
	// would pass over a place that never carried anything.
	for _, place := range []string{"violations", "bands", "chorded", "drawn"} {
		assert.NotZero(t, reported[place], "no fixture reports a rule under %s", place)
	}
}
