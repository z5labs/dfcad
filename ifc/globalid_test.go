// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package ifc

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bits is the sixteen bytes written in the canonical text of a UUID.
func bits(t *testing.T, written string) [16]byte {
	t.Helper()

	decoded, err := hex.DecodeString(strings.ReplaceAll(written, "-", ""))
	require.NoError(t, err)
	require.Len(t, decoded, 16)

	var out [16]byte
	copy(out[:], decoded)

	return out
}

func TestEncodeGlobalID(t *testing.T) {
	// Every expected value here was produced by an implementation outside this
	// repository, which is what makes them a check on this one rather than a
	// record of what it happens to do.
	testCases := []struct {
		name     string
		uuid     string
		expected GlobalID
	}{
		{
			name:     "encodes a real IFC GlobalId back to the characters it was published as",
			uuid:     "a16bfc45-7156-4558-b57c-544102ce43fb",
			expected: "2XQ$n5SLP5MBLyL442paFx",
		},
		{
			name:     "encodes the smallest value as twenty-two zeroes",
			uuid:     "00000000-0000-0000-0000-000000000000",
			expected: "0000000000000000000000",
		},
		{
			name:     "encodes the largest value, which uses the last character of the alphabet",
			uuid:     "ffffffff-ffff-ffff-ffff-ffffffffffff",
			expected: "3$$$$$$$$$$$$$$$$$$$$$",
		},
		{
			name:     "encodes a value covering every group boundary",
			uuid:     "01234567-89ab-cdef-0123-456789abcdef",
			expected: "018qLdYQlDxm4ZHMU9gytl",
		},
		{
			name:     "encodes a version 5 UUID minted outside this package",
			uuid:     "886313e1-3b8a-5372-9b90-0c9aee199e5d",
			expected: "28OnFXEufJSfkG39hk6PvT",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := EncodeGlobalID(bits(t, testCase.uuid))

			assert.Equal(t, testCase.expected, got)
			assert.Equal(t, string(testCase.expected), got.String())
		})
	}
}

// TestEncodeGlobalIDIsTheShapeIFCFixes is its own function because it is a
// property over generated input rather than a table of values somebody
// computed: a table can be right about the characters and wrong about the
// shape they always have.
func TestEncodeGlobalIDIsTheShapeIFCFixes(t *testing.T) {
	for seed := range 256 {
		var value [16]byte
		for i := range value {
			// A cheap, entirely deterministic spread across the byte range.
			value[i] = byte(seed*31 + i*17)
		}

		got := EncodeGlobalID(value).String()

		assert.Len(t, got, Length)

		// Every character is one IFC reads, and the leading one is never above
		// `3`: two characters hold twelve bits where the first byte needs
		// eight.
		assert.Empty(t, strings.Trim(got, Alphabet))
		assert.LessOrEqual(t, got[0], byte('3'))
	}
}

// TestEncodeGlobalIDIsInjective is its own function because what it asserts is
// a relation between values rather than a property of one: two identifiers
// which encoded to the same characters would be two things a receiving system
// merges into one.
func TestEncodeGlobalIDIsInjective(t *testing.T) {
	seen := make(map[GlobalID][16]byte)

	for at := range 16 {
		for bit := range 8 {
			var value [16]byte
			value[at] = 1 << bit

			got := EncodeGlobalID(value)

			previous, held := seen[got]
			require.False(t, held, "%v and %v both encode to %s", previous, value, got)

			seen[got] = value
		}
	}
}
