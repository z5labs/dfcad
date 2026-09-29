// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// surveyedModel is the siting fixture's files, read from the repository, under
// a directory of the same name.
func surveyedModel(t *testing.T) map[string]string {
	t.Helper()

	root := filepath.Join("..", "..", "testdata", "siting", "surveyed")
	entries, err := os.ReadDir(root)
	require.NoError(t, err)

	files := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != dfcad.Extension {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, entry.Name()))
		require.NoError(t, err)
		files["surveyed/"+entry.Name()] = string(src)
	}
	require.Contains(t, files, "surveyed/model.dfc")

	return files
}

// crossedFootprint is the siting fixture with Block A's north-east and
// north-west corners swapped, so that its footprint ring geom:L-11 crosses
// itself. `check` still passes over it, because no rule reads the ring.
func crossedFootprint(t *testing.T) map[string]string {
	t.Helper()

	files := surveyedModel(t)
	model := files["surveyed/model.dfc"]

	const northEast, northWest = "(value (10.0 8.0 0.0) m)", "(value (0.0 8.0 0.0) m)"
	require.Contains(t, model, northEast, "the fixture no longer holds the corner this swaps")
	require.Contains(t, model, northWest, "the fixture no longer holds the corner this swaps")

	model = strings.Replace(model, northEast, "\x00", 1)
	model = strings.Replace(model, northWest, northEast, 1)
	model = strings.Replace(model, "\x00", northWest, 1)

	files["surveyed/model.dfc"] = model
	return files
}

// openFootprint is the siting fixture with geom:E-14 dropped from Block A's
// footprint ring, so that geom:L-11 no longer closes: the traversal reaches
// geom:V-14 and the next edge begins at geom:V-11, eight metres away.
func openFootprint(t *testing.T) map[string]string {
	t.Helper()

	files := surveyedModel(t)
	model := files["surveyed/model.dfc"]

	const ring = "(edges geom:E-11 geom:E-12 geom:E-13 geom:E-14))"
	require.Contains(t, model, ring, "the fixture no longer holds the ring this opens")
	files["surveyed/model.dfc"] = strings.Replace(model, ring, "(edges geom:E-11 geom:E-12 geom:E-13))", 1)

	return files
}

// exportSurveyed is the export of the siting fixture, in its own vocabulary.
var exportSurveyed = []string{"export", "--root", "surveyed", "--position", "position", "--tolerance", "boundary-closure", "--chord", "boundary-closure"}

// TestADiagnosticNamesTheThingsItIsAbout is the case the story was filed with:
// a refusal about a ring, where what a caller acts on is the building the ring
// is the footprint of.
func TestADiagnosticNamesTheThingsItIsAbout(t *testing.T) {
	testCases := []struct {
		name    string
		files   func(t *testing.T) map[string]string
		args    []string
		message string
		ids     []dfcad.ID
		nodes   []dfcad.ID
		only    bool
	}{
		{
			name:    "names the ring export found crossing itself, and the footprint it bounds",
			files:   crossedFootprint,
			args:    exportSurveyed,
			message: "expected the loop geom:L-11 not to cross itself",
			ids:     []dfcad.ID{"geom:L-11"},
			nodes:   []dfcad.ID{"plan:S-01"},
			only:    true,
		},
		{
			name:    "names the ring export found open and the two corners either side of the gap",
			files:   openFootprint,
			args:    exportSurveyed,
			message: "expected the loop geom:L-11 to close",
			ids:     []dfcad.ID{"geom:L-11", "geom:V-11", "geom:V-14"},
			nodes:   []dfcad.ID{"plan:S-01"},
		},
		{
			name:    "names the same for measure over the open ring",
			files:   openFootprint,
			args:    []string{"measure", "--root", "surveyed", "--position", "position", "--tolerance", "boundary-closure", "plan:S-01"},
			message: "expected the loop geom:L-11 to close",
			ids:     []dfcad.ID{"geom:L-11", "geom:V-11", "geom:V-14"},
			nodes:   []dfcad.ID{"plan:S-01"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, testCase.files(t)))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitCheck, run(testCase.args, &stdout, &stderr), stderr.String())

			assertRoundTrips(t, stdout.String(), stderr.String())

			diagnostics := carriedAbout(t, stdout.String())
			if testCase.only {
				assert.Len(t, diagnostics, 1, "the refusal is the one diagnostic the run rendered")
			}

			var found []aboutDiagnostic
			for _, diagnostic := range diagnostics {
				if strings.Contains(diagnostic.Message, testCase.message) {
					found = append(found, diagnostic)
				}
			}
			require.Len(t, found, 1, "the refusal is in the object once")

			assert.Equal(t, testCase.ids, found[0].IDs)
			assert.Equal(t, testCase.nodes, found[0].Nodes)
		})
	}
}

// danglingModel is the fixture model with the last edge of Room C's outline,
// geom:L-21, renamed to one nothing holds. Every command which reads the model
// refuses it, and the reference is written inside the loop.
func danglingModel(t *testing.T) map[string]string {
	t.Helper()

	files := model()

	const ring = "(edges geom:E-21 geom:E-22 geom:E-23 geom:E-24)"
	require.Contains(t, files["entities/geometry.dfc"], ring, "the fixture no longer holds the ring this breaks")
	files["entities/geometry.dfc"] = strings.Replace(files["entities/geometry.dfc"], ring, "(edges geom:E-21 geom:E-22 geom:E-23 geom:E-99)", 1)

	return files
}

// TestEveryCommandWhichHoldsTheModelNamesWhatADiagnosticIsAbout walks every
// command over a model whose loop names an edge nothing holds. Every one which
// loads the model as a graph names the loop and the room it bounds on that
// diagnostic; every write names neither, because the model a change is refused
// over is one the run never held as a graph.
func TestEveryCommandWhichHoldsTheModelNamesWhatADiagnosticIsAbout(t *testing.T) {
	for _, cmd := range commands {
		if readsNoModel[cmd.name] {
			continue
		}

		t.Run(cmd.name+" names what the dangling reference is about, or nothing for a write", func(t *testing.T) {
			t.Chdir(tree(t, danglingModel(t)))

			var stdout, stderr bytes.Buffer
			code := run(sample(t, cmd), &stdout, &stderr)
			require.Contains(t, []int{exitSuccess, exitLoad}, code, stderr.String())

			var dangling []aboutDiagnostic
			for _, diagnostic := range carriedAbout(t, stdout.String()) {
				if strings.Contains(diagnostic.Message, "geom:E-99") {
					dangling = append(dangling, diagnostic)
				}
			}
			require.Len(t, dangling, 1, "the dangling reference is reported once")

			if cmd.writes {
				assert.Nil(t, dangling[0].IDs, "a write held no model to name anything from")
				assert.Nil(t, dangling[0].Nodes, "a write held no model to name anything from")
				assert.NotContains(t, stdout.String(), `"ids"`)
				assert.NotContains(t, stdout.String(), `"nodes"`)
				return
			}

			assert.Equal(t, []dfcad.ID{"geom:L-21"}, dangling[0].IDs)
			assert.Equal(t, []dfcad.ID{"site:S-103"}, dangling[0].Nodes)
		})
	}
}

// TestADiagnosticOnARegistryFormNamesNothing is its own function because it
// asserts an absence: a registry declares vocabulary rather than things, so a
// diagnostic about one carries neither field, on any command.
func TestADiagnosticOnARegistryFormNamesNothing(t *testing.T) {
	for _, cmd := range commands {
		if readsNoModel[cmd.name] {
			continue
		}

		t.Run(cmd.name+" names nothing for a load diagnostic on a registry form", func(t *testing.T) {
			t.Chdir(tree(t, unloadable(t)))

			var stdout, stderr bytes.Buffer
			code := run(sample(t, cmd), &stdout, &stderr)
			require.Contains(t, []int{exitSuccess, exitLoad}, code, stderr.String())

			diagnostics := carriedAbout(t, stdout.String())
			require.NotEmpty(t, diagnostics)
			for _, diagnostic := range diagnostics {
				require.Equal(t, "registry.dfc", diagnostic.Span.Start.Path)
				assert.Nil(t, diagnostic.IDs)
				assert.Nil(t, diagnostic.Nodes)
			}

			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &fields))
			assert.NotContains(t, string(fields["diagnostics"]), `"ids"`, "an empty list is not written")
			assert.NotContains(t, string(fields["diagnostics"]), `"nodes"`, "an empty list is not written")
		})
	}
}

// TestADiagnosticNamesTheNodesTraverseBoundsAnswers holds the diagnostic to the
// query over the same relation, over every loop and edge of the adjacency
// fixture: the nodes a diagnostic inside a loop or an edge names are exactly
// what `dfcad traverse bounds` answers for it. The two cannot disagree about
// which node a shape belongs to.
func TestADiagnosticNamesTheNodesTraverseBoundsAnswers(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "boundary", "adjacent"))
	require.NoError(t, err)

	graph, _ := dfcad.LoadGraph(root)
	require.NotNil(t, graph)

	var shapes []dfcad.Entity
	for edge := range graph.Topology().Edges() {
		shapes = append(shapes, edge)
	}
	for loop := range graph.Topology().Loops() {
		shapes = append(shapes, loop)
	}
	require.NotEmpty(t, shapes)

	for _, shape := range shapes {
		t.Run("names what traverse bounds answers for "+string(shape.ID()), func(t *testing.T) {
			// A diagnostic pointing just inside the shape's form, as a
			// derivation's refusal of it would.
			within := shape.Span()
			within.Start.Column++
			within.End = within.Start

			named := about(graph, dfcad.Diagnostic{Severity: dfcad.SeverityError, Span: within, Message: "expected something else"})
			assert.Equal(t, []dfcad.ID{shape.ID()}, named.IDs)

			bounded := reachedIDs(walkIn(t, root, queryBounds, string(shape.ID())))
			expected := make([]dfcad.ID, 0, len(bounded))
			for _, id := range bounded {
				expected = append(expected, dfcad.ID(id))
			}
			slices.Sort(expected)

			assert.Equal(t, expected, named.Nodes)
		})
	}
}
