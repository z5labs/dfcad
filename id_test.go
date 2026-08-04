// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseID(t *testing.T) {
	testCases := []struct {
		name      string
		written   string
		namespace string
		local     string
	}{
		{
			name:      "splits on the colon",
			written:   "site:S-101",
			namespace: "site",
			local:     "S-101",
		},
		{
			name:      "splits on the first colon only, because a local part may hold more",
			written:   "survey:2026:CP-3",
			namespace: "survey",
			local:     "2026:CP-3",
		},
		{
			name:      "accepts letters, digits, hyphens and underscores in a namespace",
			written:   "acme-survey_2026:CP-3",
			namespace: "acme-survey_2026",
			local:     "CP-3",
		},
		{
			name:      "accepts the punctuation a symbol permits in a local part",
			written:   "site:S-101.2/a+b",
			namespace: "site",
			local:     "S-101.2/a+b",
		},
		{
			name:      "accepts a local part which is not ASCII, because only the namespace is confined",
			written:   "site:gebäude",
			namespace: "site",
			local:     "gebäude",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			id, err := ParseID(testCase.written)

			require.NoError(t, err)
			assert.Equal(t, ID(testCase.written), id)
			assert.Equal(t, testCase.namespace, id.Namespace())
			assert.Equal(t, testCase.local, id.Local())
			assert.Equal(t, testCase.written, id.String())
		})
	}
}

// TestParseIDMalformed is its own function because it asserts on the rule an id
// broke rather than on the parts it split into.
//
// The rule is a field rather than wording inside a message so that "you forgot
// the namespace" and "that namespace holds a character no namespace may hold"
// are told apart by something other than reading English.
func TestParseIDMalformed(t *testing.T) {
	testCases := []struct {
		name    string
		written string
		reason  IDProblem
	}{
		{
			name:    "rejects a symbol with no colon in it",
			written: "S-101",
			reason:  IDUnqualified,
		},
		{
			name:    "rejects an empty namespace",
			written: ":S-101",
			reason:  IDEmptyNamespace,
		},
		{
			name:    "rejects an empty local part",
			written: "site:",
			reason:  IDEmptyLocal,
		},
		{
			name:    "rejects a namespace which begins with a digit",
			written: "3d:S-101",
			reason:  IDMalformedNamespace,
		},
		{
			name:    "rejects a namespace holding punctuation a symbol permits and a namespace does not",
			written: "site.local:S-101",
			reason:  IDMalformedNamespace,
		},
		{
			name:    "rejects a namespace which is not ASCII",
			written: "gebäude:S-101",
			reason:  IDMalformedNamespace,
		},
		{
			name:    "rejects a local part holding a character no symbol holds",
			written: `site:S 101`,
			reason:  IDMalformedLocal,
		},
		{
			name:    "rejects a local part which would end the form it was written in",
			written: "site:S-101)",
			reason:  IDMalformedLocal,
		},
		{
			name:    "rejects a local part which begins a comment",
			written: "site:S;101",
			reason:  IDMalformedLocal,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			id, err := ParseID(testCase.written)

			assert.Equal(t, ID(""), id, "a failure yields no id to be going on with")

			var malformed MalformedIDError
			require.True(t, errors.As(err, &malformed))
			assert.Equal(t, testCase.reason, malformed.Reason)
			assert.Equal(t, testCase.written, malformed.Written)
			assert.Contains(t, malformed.Error(), malformed.Reason.rule())
		})
	}
}

// TestIDComparesByValue is the property everything referencing an id depends
// on: an ID is a value, so two of them are the same id when their bytes are the
// same, and either can be looked up by the other.
//
// It is asserted here rather than assumed because the alternative — a pointer,
// or a struct holding one — compares by identity, and a model in which two
// readings of `site:S-101` were different ids would resolve nothing.
func TestIDComparesByValue(t *testing.T) {
	first, err := ParseID("site:S-101")
	require.NoError(t, err)

	second, err := ParseID("site:S-101")
	require.NoError(t, err)

	assert.Equal(t, first, second)
	assert.True(t, first == second)

	// Comparison is byte-wise: no case folding, no normalisation, no trimming.
	different, err := ParseID("site:s-101")
	require.NoError(t, err)
	assert.False(t, first == different)

	// And so an ID is a map key which behaves the way anything resolving a
	// reference needs it to.
	byID := map[ID]string{first: "Meeting Room B"}

	label, ok := byID[second]
	assert.True(t, ok)
	assert.Equal(t, "Meeting Room B", label)

	_, ok = byID[different]
	assert.False(t, ok)
}

// TestIDPartsOfSomethingWhichIsNotAnID checks that the accessors answer rather
// than guess for a value which was converted instead of parsed.
//
// The zero ID is the id of a node whose id could not be read, and every layer
// above holds one sooner or later. Reporting parts it does not have would be
// inventing a namespace for something which has none.
func TestIDPartsOfSomethingWhichIsNotAnID(t *testing.T) {
	assert.Empty(t, ID("").Namespace())
	assert.Empty(t, ID("").Local())
	assert.Empty(t, ID("S-101").Namespace())
	assert.Empty(t, ID("S-101").Local())
}

func TestWellFormedNamespace(t *testing.T) {
	testCases := []struct {
		name      string
		namespace string
		want      bool
	}{
		{
			name:      "accepts letters, digits, hyphens and underscores after a letter",
			namespace: "acme-survey_2026",
			want:      true,
		},
		{
			name:      "rejects a namespace which begins with a digit",
			namespace: "3d",
		},
		{
			name:      "rejects a namespace holding punctuation a symbol permits and a namespace does not",
			namespace: "site.local",
		},
		{
			name:      "rejects a namespace which is not ASCII",
			namespace: "gebäude",
		},
		{
			name:      "rejects an empty namespace",
			namespace: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, wellFormedNamespace(testCase.namespace))
		})
	}
}
