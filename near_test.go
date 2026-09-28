// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVertexIndexNearest(t *testing.T) {
	// Recorded out of id order, so that a tie broken by id rather than by the
	// order the index read them would say so.
	index := &vertexIndex{at: map[ID][]float64{}}
	index.record("geom:V-02", []float64{4, 0, 0})
	index.record("geom:V-01", []float64{0, 0, 0})
	index.record("geom:V-03", []float64{2, 5, 0})

	testCases := []struct {
		name      string
		point     []float64
		tolerance float64
		expected  ID
		found     bool
	}{
		{
			name:      "lands on the only vertex within the tolerance",
			point:     []float64{0.001, 0, 0},
			tolerance: 0.005,
			expected:  "geom:V-01",
			found:     true,
		},
		{
			name:      "lands on the nearer of two vertices within the tolerance",
			point:     []float64{1, 0, 0},
			tolerance: 3.5,
			expected:  "geom:V-01",
			found:     true,
		},
		{
			name:      "breaks a tie by the order the vertices were read",
			point:     []float64{2, 0, 0},
			tolerance: 2.5,
			expected:  "geom:V-02",
			found:     true,
		},
		{
			name:      "lands nowhere when nothing is within the tolerance",
			point:     []float64{10, 10, 0},
			tolerance: 0.005,
		},
		{
			name:      "never lands on a vertex of another dimension",
			point:     []float64{0, 0},
			tolerance: 0.005,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, distance, found := index.nearest(testCase.point, testCase.tolerance)

			assert.Equal(t, testCase.found, found)
			assert.Equal(t, testCase.expected, got)

			if !found {
				return
			}

			// The one it lands on is one of the ones listed, at the smallest
			// distance any of them is.
			listed := index.within(testCase.point, testCase.tolerance)
			smallest := listed[0].Distance
			for _, candidate := range listed {
				smallest = min(smallest, candidate.Distance)
			}
			assert.Equal(t, smallest, distance)
		})
	}
}
