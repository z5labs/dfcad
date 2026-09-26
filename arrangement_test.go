// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// rectangle is the ring of an axis-aligned rectangle, written anticlockwise
// from its lowest corner.
func rectangle(minX, minY, maxX, maxY float64) contour {
	return contour{{minX, minY}, {maxX, minY}, {maxX, maxY}, {minX, maxY}}
}

func TestNestedIn(t *testing.T) {
	testCases := []struct {
		name     string
		inner    contour
		outer    contour
		expected nesting
	}{
		{
			name:     "reads a ring abutting another along part of a wall as beside it",
			inner:    rectangle(0, 0, 4, 2),
			outer:    rectangle(4, 0, 8, 4),
			expected: ringBeside,
		},
		{
			name:     "reads a ring meeting another at one corner as beside it",
			inner:    rectangle(0, 0, 4, 4),
			outer:    rectangle(4, 4, 6, 6),
			expected: ringBeside,
		},
		{
			name:     "reads a courtyard as within the plate around it",
			inner:    rectangle(3, 3, 7, 7),
			outer:    rectangle(0, 0, 10, 10),
			expected: ringWithin,
		},
		{
			name:     "reads the plate as beside the courtyard it holds",
			inner:    rectangle(0, 0, 10, 10),
			outer:    rectangle(3, 3, 7, 7),
			expected: ringBeside,
		},
		{
			name:     "reads a courtyard sharing part of the plate's wall as within the plate",
			inner:    rectangle(0, 3, 4, 7),
			outer:    rectangle(0, 0, 10, 10),
			expected: ringWithin,
		},
		{
			name:     "reads a courtyard written clockwise as within the plate just the same",
			inner:    rectangle(3, 3, 7, 7).reversed(),
			outer:    rectangle(0, 0, 10, 10),
			expected: ringWithin,
		},
		{
			name:     "reads two rings crossing as a plus sign as crossing",
			inner:    rectangle(0, 1.5, 4, 2.5),
			outer:    rectangle(1.5, 0, 2.5, 4),
			expected: ringsCrossing,
		},
		{
			name:     "reads the run of an L overlapping its leg along part of a side as crossing it",
			inner:    rectangle(0, 3, 4, 4),
			outer:    rectangle(0, 0, 1, 4),
			expected: ringsCrossing,
		},
		{
			name:     "reads the leg of an L overlapping its run along part of a side as crossing it",
			inner:    rectangle(0, 0, 1, 4),
			outer:    rectangle(0, 3, 4, 4),
			expected: ringsCrossing,
		},
		{
			name:     "reads two plates overlapping from a shared corner as crossing",
			inner:    rectangle(0, 0, 4, 2),
			outer:    rectangle(0, 0, 2, 4),
			expected: ringsCrossing,
		},
		{
			name:     "reads a ring whose every probe is inside another but which runs across a notch in it as crossing it",
			inner:    rectangle(0, 0, 4, 3.5),
			outer:    contour{{0, 0}, {4, 0}, {4, 4}, {1.5, 4}, {1, 3}, {0.5, 4}, {0, 4}},
			expected: ringsCrossing,
		},
		{
			name:     "reads two rings overlapping by less than the tolerance as beside one another",
			inner:    rectangle(0, 0, 4, 4),
			outer:    rectangle(3.9995, 0, 8, 4),
			expected: ringBeside,
		},
		{
			name:     "reads one ring written twice as indistinct",
			inner:    rectangle(0, 0, 4, 4),
			outer:    rectangle(0, 0, 4, 4).reversed(),
			expected: ringsIndistinct,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, nestedIn(testCase.inner, testCase.outer, 0.001))
		})
	}
}
