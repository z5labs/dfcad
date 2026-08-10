// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declaredSpecVersion matches the version SPEC.md states at the top of itself,
// which is the one place the specification declares which version it is.
var declaredSpecVersion = regexp.MustCompile(`(?m)^\*\*Specification version ([0-9]+\.[0-9]+)\.\*\*$`)

// TestSpecVersionIsWhatTheSpecificationDeclares reads SPEC.md and requires the
// constant to agree with it.
//
// The constant is a second copy of a fact the specification already states, and
// a second copy that drifts is worse than none: a caller which branches on it —
// or a bug report quoting `dfcad version` — would then be confidently wrong
// about which dialect of the format it is holding. Bumping the specification
// without bumping the constant fails here, which is the only moment at which the
// two are both in front of whoever is making the change.
func TestSpecVersionIsWhatTheSpecificationDeclares(t *testing.T) {
	t.Run("matches the version SPEC.md declares", func(t *testing.T) {
		specification, err := os.ReadFile("SPEC.md")
		require.NoError(t, err)

		match := declaredSpecVersion.FindSubmatch(specification)
		require.NotNil(t, match, "SPEC.md does not declare a version in the form the constant tracks")

		assert.Equal(t, string(match[1]), SpecVersion)
	})

	t.Run("parses into the format the engine implements", func(t *testing.T) {
		format, err := ParseEntityFormat(SpecVersion)

		require.NoError(t, err)
		assert.Equal(t, format, SpecFormat())
		assert.Equal(t, SpecVersion, SpecFormat().String())
	})
}

func TestParseEntityFormat(t *testing.T) {
	testCases := []struct {
		name     string
		written  string
		expected EntityFormat
	}{
		{
			name:     "reads the two components of a version",
			written:  "1.2",
			expected: EntityFormat{Major: 1, Minor: 2},
		},
		{
			name:     "reads a minor of more than one digit as a number rather than as text",
			written:  "1.10",
			expected: EntityFormat{Major: 1, Minor: 10},
		},
		{
			name:     "reads a zero component",
			written:  "2.0",
			expected: EntityFormat{Major: 2, Minor: 0},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := ParseEntityFormat(testCase.written)

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, got)

			// Parse then print then parse: the printing is the one spelling, so
			// a version which read back as something else would be two.
			printed := got.String()
			assert.Equal(t, testCase.written, printed)

			again, err := ParseEntityFormat(printed)
			require.NoError(t, err)
			assert.Equal(t, got, again)
		})
	}
}

// TestParseEntityFormatRefusesWhatIsNotAVersion is its own function because it
// asserts on an error and its field rather than on a value, which is a different
// shape of behaviour from the readings above.
func TestParseEntityFormatRefusesWhatIsNotAVersion(t *testing.T) {
	testCases := []struct {
		name    string
		written string
	}{
		{name: "refuses a version with no minor component", written: "1"},
		{name: "refuses a version with a patch component", written: "1.2.3"},
		{name: "refuses a leading zero, which is a second spelling of one version", written: "1.02"},
		{name: "refuses a signed component", written: "+1.2"},
		{name: "refuses a negative component", written: "1.-2"},
		{name: "refuses surrounding space", written: " 1.2"},
		{name: "refuses a leading v, which is how the tool is tagged and not how the format is versioned", written: "v1.2"},
		{name: "refuses a component which is not a number", written: "1.x"},
		{name: "refuses nothing at all", written: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParseEntityFormat(testCase.written)

			var malformed MalformedEntityFormatError
			require.ErrorAs(t, err, &malformed)
			assert.Equal(t, testCase.written, malformed.Written)
		})
	}
}

func TestEntityFormatLoadsUnder(t *testing.T) {
	testCases := []struct {
		name     string
		model    EntityFormat
		engine   EntityFormat
		expected bool
	}{
		{
			name:     "loads a model authored against the format the engine implements",
			model:    EntityFormat{Major: 1, Minor: 2},
			engine:   EntityFormat{Major: 1, Minor: 2},
			expected: true,
		},
		{
			name:     "loads a model authored against an earlier minor of the same major",
			model:    EntityFormat{Major: 1, Minor: 1},
			engine:   EntityFormat{Major: 1, Minor: 2},
			expected: true,
		},
		{
			name:     "refuses a model authored against a later minor, which may hold forms the engine does not know",
			model:    EntityFormat{Major: 1, Minor: 3},
			engine:   EntityFormat{Major: 1, Minor: 2},
			expected: false,
		},
		{
			name:     "compares the minor as a number rather than as text",
			model:    EntityFormat{Major: 1, Minor: 10},
			engine:   EntityFormat{Major: 1, Minor: 9},
			expected: false,
		},
		{
			name:     "refuses a model a major ahead",
			model:    EntityFormat{Major: 2, Minor: 0},
			engine:   EntityFormat{Major: 1, Minor: 2},
			expected: false,
		},
		{
			name:     "refuses a model a major behind, which the major was moved for",
			model:    EntityFormat{Major: 1, Minor: 2},
			engine:   EntityFormat{Major: 2, Minor: 0},
			expected: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, testCase.model.LoadsUnder(testCase.engine))
		})
	}
}

// TestAssertEntityFormat is the same rule read against the engine's own format,
// which is what a consumer asserting one actually calls.
func TestAssertEntityFormat(t *testing.T) {
	engine := SpecFormat()

	testCases := []struct {
		name    string
		model   EntityFormat
		refused bool
	}{
		{
			name:  "accepts the format this engine implements",
			model: engine,
		},
		{
			name:  "accepts a model authored against an earlier minor",
			model: EntityFormat{Major: engine.Major, Minor: 0},
		},
		{
			name:    "refuses a model authored against a later minor",
			model:   EntityFormat{Major: engine.Major, Minor: engine.Minor + 1},
			refused: true,
		},
		{
			name:    "refuses a model a major apart",
			model:   EntityFormat{Major: engine.Major + 1, Minor: 0},
			refused: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := AssertEntityFormat(testCase.model)

			if !testCase.refused {
				assert.NoError(t, err)
				return
			}

			// Both formats are carried, because naming one of them tells the
			// author half of what they have to decide between.
			var unsupported UnsupportedEntityFormatError
			require.ErrorAs(t, err, &unsupported)
			assert.Equal(t, testCase.model, unsupported.Model)
			assert.Equal(t, engine, unsupported.Engine)
		})
	}
}
