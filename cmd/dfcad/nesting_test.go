// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// abuttingFixture is the engine's own fixture of regions bounded by more than
// one ring — the rings which abut, nest, cross and overlap — read from where the
// engine's tests read it rather than copied here.
//
// It is read rather than restated because the point of the tests below is that
// every command answers the same model the way `dfcad measure` does, and a
// second copy of the model would be free to drift from the first until the two
// were agreeing about different shapes.
//
// The one thing added is a storey holding the overlapping regions, because a
// plan is asked of a place rather than of a room and the fixture has no place in
// it. Nothing about any ring changes.
func abuttingFixture(t *testing.T) map[string]string {
	t.Helper()

	files := make(map[string]string, 3)
	for _, name := range []string{"registry.dfc", "model.dfc"} {
		src, err := os.ReadFile(filepath.Join("..", "..", "testdata", "measure", "abutting", name))
		require.NoError(t, err)

		files[name] = string(src)
	}

	files["registry.dfc"] += `
(type OfficeStorey (kind Storey) (geometry solid) (description "One floor plate of a building."))
`

	files["storey.dfc"] = `(node site:L-01
  (label "The floor the overlapping counters are on")
  (kind Storey)
  (type OfficeStorey)
  (geometry solid)
  (frame frame:building))
`

	for _, id := range []string{"site:S-13", "site:S-14"} {
		node := "(node " + id + "\n"
		require.Contains(t, files["model.dfc"], node, "the fixture still holds %s", id)

		files["model.dfc"] = strings.Replace(files["model.dfc"], node, node+"  (within site:L-01)\n", 1)
	}

	return files
}

// TestEveryCommandRefusesRingsWhichOverlapFromASharedCorner is its own function
// because what it asserts spans every command which orients a region's rings
// rather than any one of them: each nests the rings through the same rule, and a
// plan and a measurement of one node which disagreed about whether it has an
// area would be one model saying two things.
//
// The two regions are the ones the rule used to sum: an L-shaped counter drawn
// as its two runs, which overlap in the inside corner and share part of a side
// there, and two plates overlapping from a corner they share as one vertex. Each
// has to come back as the pair of loops named, from every command, and no
// command may answer as though the pair were a shape.
func TestEveryCommandRefusesRingsWhichOverlapFromASharedCorner(t *testing.T) {
	vocabulary := []string{"--position", "position", "--tolerance", "boundary-closure"}
	drawing := []string{"--position", "position", "--tolerance", "boundary-closure", "--chord", "chord-deviation"}

	regions := []struct {
		name  string
		id    string
		loops []string
	}{
		{name: "a counter drawn as two runs which overlap", id: "site:S-13", loops: []string{"geom:L-121", "geom:L-122"}},
		{name: "two plates which overlap from a shared corner", id: "site:S-14", loops: []string{"geom:L-131", "geom:L-132"}},
	}

	for _, region := range regions {
		testCases := []struct {
			name string
			args []string

			// answered is the member of the result saying whether it is an
			// answer, which each command names for what it produces.
			answered string
		}{
			{
				name:     "measure refuses " + region.name,
				args:     append(append([]string{"measure"}, vocabulary...), region.id),
				answered: "derived",
			},
			{
				name:     "tessellate refuses " + region.name,
				args:     append(append([]string{"tessellate"}, drawing...), region.id),
				answered: "derived",
			},
			{
				name:     "plan refuses " + region.name,
				args:     append(append([]string{"plan", "--annotate", "position"}, vocabulary...), "site:L-01"),
				answered: "planned",
			},
			{
				name:     "export refuses " + region.name,
				args:     append(append([]string{"export"}, drawing...), "--out", filepath.Join(t.TempDir(), "ifc")),
				answered: "derived",
			},
			{
				name:     "export-map refuses " + region.name,
				args:     append(append([]string{"export-map"}, drawing...), "--out", filepath.Join(t.TempDir(), "gml")),
				answered: "derived",
			},
		}

		for _, testCase := range testCases {
			t.Run(testCase.name, func(t *testing.T) {
				stdout, stderr := invoke(t, exitCheck, tree(t, abuttingFixture(t)), testCase.args...)

				var result map[string]any
				require.NoError(t, json.Unmarshal([]byte(stdout), &result))
				assert.Equal(t, false, result[testCase.answered], "no answer came back as though the pair were a shape")

				// The whole-model commands refuse the fixture's crossing plates
				// too, so the exit code alone would pass without this region
				// being refused. The diagnostic naming its two loops is what
				// says it was.
				for _, loop := range region.loops {
					assert.Contains(t, stderr, loop)
				}
				assert.Contains(t, stderr, region.loops[0]+" and "+region.loops[1]+" cross")
			})
		}
	}
}
