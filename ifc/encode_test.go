// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package ifc

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendReal(t *testing.T) {
	testCases := []struct {
		name     string
		value    float64
		expected string
	}{
		{name: "writes a whole number with the point which says it is a real", value: 1, expected: "1."},
		{name: "writes zero as zero", value: 0, expected: "0."},
		{name: "writes negative zero as zero", value: math.Copysign(0, -1), expected: "0."},
		{name: "keeps the sign of a negative value", value: -4.5, expected: "-4.5"},
		{name: "writes a small value in full rather than as an exponent", value: 0.00001, expected: "0.00001"},
		{name: "writes the shortest digits which read back as the same float", value: 0.1, expected: "0.1"},
		{name: "writes a value which needs seventeen digits", value: 0.30000000000000004, expected: "0.30000000000000004"},
		{name: "writes a large value without an exponent", value: 1e21, expected: "1000000000000000000000."},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := appendReal(nil, testCase.value)

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, string(got))
		})
	}
}

// TestAppendRealRefusesWhatTheFormatCannotSpell is its own function because
// what it asserts is a refusal and the fields it carries, rather than the text
// of a value which was written.
func TestAppendRealRefusesWhatTheFormatCannotSpell(t *testing.T) {
	testCases := []struct {
		name  string
		value float64
	}{
		{name: "not a number", value: math.NaN()},
		{name: "positive infinity", value: math.Inf(1)},
		{name: "negative infinity", value: math.Inf(-1)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := appendReal(nil, testCase.value)

			var got UnrepresentableRealError
			require.ErrorAs(t, err, &got)

			assert.Equal(t, math.IsNaN(testCase.value), math.IsNaN(got.Value))
		})
	}
}

// TestAppendRealRoundTrips is its own function because it is a property over
// generated input rather than a table of literals: an exact output somebody
// typed can be right about the digits and wrong about whether they read back.
func TestAppendRealRoundTrips(t *testing.T) {
	values := []float64{
		0, 1, -1, 0.5, -0.5, 1e-9, 1e9, 3.141592653589793,
		math.SmallestNonzeroFloat64, math.MaxFloat64, -math.MaxFloat64,
	}

	for _, value := range values {
		written, err := appendReal(nil, value)
		require.NoError(t, err)

		parsed, err := read("ISO-10303-21;\nHEADER;\nENDSEC;\nDATA;\n#1=IFCDIRECTION((" +
			string(written) + "));\nENDSEC;\nEND-ISO-10303-21;\n")
		require.NoError(t, err, "%v is written as %s, which parses", value, written)

		held, ok := parsed.instance(1)
		require.True(t, ok)
		require.Equal(t, itemReal, held.attributes[0].items[0].form,
			"%v is written as %s, which is a real rather than an integer", value, written)
	}
}

func TestAppendString(t *testing.T) {
	testCases := []struct {
		name     string
		value    string
		expected string
	}{
		{name: "quotes plain text", value: "Meeting Room A", expected: "'Meeting Room A'"},
		{name: "writes the empty string as two quotes", value: "", expected: "''"},
		{name: "doubles a quote", value: "Level 1's plant room", expected: "'Level 1''s plant room'"},
		{name: "doubles a backslash so it does not start a directive", value: `a\b`, expected: `'a\\b'`},
		{
			name:     "encodes a character the format has no place for",
			value:    "Café",
			expected: `'Caf\X2\00E9\X0\'`,
		},
		{
			name:     "encodes a run of them as one directive",
			value:    "Ötzi Straße",
			expected: `'\X2\00D6\X0\tzi Stra\X2\00DF\X0\e'`,
		},
		{
			name:     "encodes a character outside the basic plane as two code units",
			value:    "\U0001F600",
			expected: `'\X2\D83DDE00\X0\'`,
		},
		{
			name:     "encodes a control character rather than writing it raw",
			value:    "a\nb",
			expected: `'a\X2\000A\X0\b'`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, string(appendString(nil, testCase.value)))
		})
	}
}

// TestOptionalAttributesAreAbsentRatherThanEmpty is its own function because
// it is about two encodings meaning different things rather than about what
// either of them looks like.
func TestOptionalAttributesAreAbsentRatherThanEmpty(t *testing.T) {
	t.Run("an empty string is written as absent", func(t *testing.T) {
		written, err := encode([]value{optionalText("")})

		require.NoError(t, err)
		assert.Equal(t, "$", written)
	})

	t.Run("a string which is there is written as a string", func(t *testing.T) {
		written, err := encode([]value{optionalText("Model")})

		require.NoError(t, err)
		assert.Equal(t, "'Model'", written)
	})

	t.Run("an empty enumeration member is written as absent", func(t *testing.T) {
		written, err := encode([]value{optionalEnumeration("")})

		require.NoError(t, err)
		assert.Equal(t, "$", written)
	})

	t.Run("a member which is there is written between dots", func(t *testing.T) {
		written, err := encode([]value{optionalEnumeration("ELEMENT")})

		require.NoError(t, err)
		assert.Equal(t, ".ELEMENT.", written)
	})
}

func TestEncode(t *testing.T) {
	testCases := []struct {
		name       string
		attributes []value
		expected   string
	}{
		{
			name:       "writes an empty list as a list of nothing",
			attributes: []value{list{}},
			expected:   "()",
		},
		{
			name:       "separates a list's elements with commas",
			attributes: []value{list{integer(1), integer(2)}},
			expected:   "(1,2)",
		},
		{
			name:       "writes a reference as a hash and a number",
			attributes: []value{reference(12)},
			expected:   "#12",
		},
		{
			name:       "writes a derived attribute as a star and an absent one as a dollar",
			attributes: []value{derived{}, absent{}},
			expected:   "*,$",
		},
		{
			name:       "nests lists",
			attributes: []value{list{list{real(0), real(0)}, list{}}},
			expected:   "((0.,0.),())",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := encode(testCase.attributes)

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestEncodeReportsWhatItCouldNotWrite is its own function because a failure
// deep in a nested list has to come back out rather than being swallowed at
// the level which found it.
func TestEncodeReportsWhatItCouldNotWrite(t *testing.T) {
	_, err := encode([]value{list{list{real(math.NaN())}}})

	var got UnrepresentableRealError
	require.ErrorAs(t, err, &got)
	assert.True(t, math.IsNaN(got.Value))
}
