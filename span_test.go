// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpanJSON(t *testing.T) {
	testCases := []struct {
		name     string
		span     Span
		expected string
	}{
		{
			name: "writes a span with extent as one path and both ends",
			span: Span{
				Start: Position{Path: "entities/site.dfc", Line: 13, Column: 1, Offset: 142},
				End:   Position{Path: "entities/site.dfc", Line: 52, Column: 43, Offset: 1284},
			},
			expected: `"entities/site.dfc:13:1-52:43"`,
		},
		{
			name:     "writes an empty span as the one point it is at",
			span:     Position{Path: "site/b.dfc", Line: 1, Column: 7, Offset: 6}.Span(),
			expected: `"site/b.dfc:1:7"`,
		},
		{
			name: "writes a span which starts and ends on one line",
			span: Span{
				Start: Position{Path: "registry.dfc", Line: 37, Column: 3},
				End:   Position{Path: "registry.dfc", Line: 37, Column: 40},
			},
			expected: `"registry.dfc:37:3-37:40"`,
		},
		{
			name: "keeps a path which holds the characters the form separates on",
			span: Span{
				Start: Position{Path: "entities/level-01.dfc", Line: 224, Column: 1},
				End:   Position{Path: "entities/level-01.dfc", Line: 244, Column: 26},
			},
			expected: `"entities/level-01.dfc:224:1-244:26"`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			encoded, err := json.Marshal(testCase.span)

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, string(encoded))
		})
	}
}

// TestSpanRoundTripsThroughJSON is the property, rather than the literals
// above: a span written and read back names the same place.
//
// The offsets are the documented exception. The text form does not carry them,
// so what comes back is where the node is and not how far into the file it is,
// and a test which asserted otherwise would be asserting an encoding this one
// deliberately does not have.
func TestSpanRoundTripsThroughJSON(t *testing.T) {
	testCases := []struct {
		name string
		span Span
	}{
		{
			name: "a span with extent",
			span: Span{
				Start: Position{Path: "entities/level-01.dfc", Line: 224, Column: 1, Offset: 5342},
				End:   Position{Path: "entities/level-01.dfc", Line: 244, Column: 26, Offset: 5851},
			},
		},
		{
			name: "an empty span",
			span: Position{Path: "a.dfc", Line: 9, Column: 4, Offset: 88}.Span(),
		},
		{
			name: "a path holding a colon and a dash",
			span: Span{
				Start: Position{Path: `C:\models\level-01.dfc`, Line: 1, Column: 1},
				End:   Position{Path: `C:\models\level-01.dfc`, Line: 2, Column: 3},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			encoded, err := json.Marshal(testCase.span)
			require.NoError(t, err)

			var got Span
			require.NoError(t, json.Unmarshal(encoded, &got))

			assert.Equal(t, testCase.span.Start.Path, got.Start.Path)
			assert.Equal(t, testCase.span.Start.Line, got.Start.Line)
			assert.Equal(t, testCase.span.Start.Column, got.Start.Column)
			assert.Equal(t, testCase.span.End.Path, got.End.Path)
			assert.Equal(t, testCase.span.End.Line, got.End.Line)
			assert.Equal(t, testCase.span.End.Column, got.End.Column)

			assert.Zero(t, got.Start.Offset)
			assert.Zero(t, got.End.Offset)

			again, err := json.Marshal(got)
			require.NoError(t, err)
			assert.Equal(t, string(encoded), string(again))
		})
	}
}

func TestSpanRejectsTextItDidNotWrite(t *testing.T) {
	testCases := []struct {
		name string
		text string
	}{
		{name: "nothing at all", text: ""},
		{name: "a path with no position on it", text: "entities/site.dfc"},
		{name: "a line and no column", text: "entities/site.dfc:13"},
		{name: "a line which is not a number", text: "entities/site.dfc:first:1"},
		{name: "a line before the first", text: "entities/site.dfc:0:1"},
		{name: "a column before the first", text: "entities/site.dfc:13:0"},
		{name: "a position with no path", text: ":13:1"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			encoded, err := json.Marshal(testCase.text)
			require.NoError(t, err)

			var got Span
			err = json.Unmarshal(encoded, &got)

			var wanted SpanTextError
			require.True(t, errors.As(err, &wanted), "expected a SpanTextError, got %T", err)
			assert.Equal(t, testCase.text, wanted.Text)
		})
	}
}
