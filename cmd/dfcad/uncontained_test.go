// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// orphanRegistry and orphanEntities are the reproduction a node within nothing
// was first reported against: two slabs, one within the dwelling and one
// within nothing, which the export used to leave out of the file without a
// word.
const orphanRegistry = `(project
  (label "Orphan element repro")
  (globalid-namespace "https://example.org/orphan-repro"))

(namespace frame (description "Coordinate frames."))
(namespace site (description "Semantic nodes."))

(frame frame:site (label "Site grid") (unit usft))

(type building (kind Building) (geometry absent) (description "The dwelling."))
(type slab (kind Element) (geometry absent) (description "A slab.")
  (classification "IFC4" "IfcSlab"))
`

const orphanEntities = `(node site:dwelling (label "Dwelling") (kind Building) (type building))
(node site:slab-inside (label "A slab within the dwelling") (kind Element) (type slab)
  (within site:dwelling))
(node site:slab-nowhere (label "A slab within nothing") (kind Element) (type slab))
`

// orphanModel is the reproduction as a fixture tree.
func orphanModel() map[string]string {
	return map[string]string{
		"registry.dfc": orphanRegistry,
		"entities.dfc": orphanEntities,
	}
}

// placedBy is the placement the object named id in source is placed by, as
// the instance it is written as.
func placedBy(t *testing.T, source, id string) entity {
	t.Helper()

	instances := parsed(t, source)

	placement, held := instances[instances[namedIn(t, source, id)].attributes[5]]
	require.True(t, held, "%s names a placement the file holds", id)
	require.Equal(t, "IFCLOCALPLACEMENT", placement.keyword)

	return placement
}

func TestRunExportWritesANodeWithinNothing(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, orphanModel())
	require.True(t, result.Derived, stderr)
	assert.Empty(t, result.Classifications)

	source := artefact(t, result)

	t.Run("writes it as the entity its type is classified as", func(t *testing.T) {
		assert.Contains(t, source, "IFCSLAB('")
		assert.Contains(t, source, "'site:slab-nowhere','A slab within nothing','slab'")
	})

	t.Run("contains it in no spatial element", func(t *testing.T) {
		contained := containedIn(t, source)

		assert.Contains(t, contained, namedIn(t, source, "site:slab-inside"),
			"the slab within the dwelling is still contained in it")
		assert.NotContains(t, contained, namedIn(t, source, "site:slab-nowhere"))
	})

	t.Run("places it relative to nothing, which is the root frame the file is written in", func(t *testing.T) {
		assert.Equal(t, "$", placedBy(t, source, "site:slab-nowhere").attributes[0])
	})

	t.Run("says nothing about it on stderr, because nothing was left out", func(t *testing.T) {
		assert.NotContains(t, stderr, "site:slab-nowhere")
	})
}

// TestRunExportWritesEveryNodeTheModelHasNotRetired is its own function because
// it asserts an account rather than a line: the nodes the file holds and the
// nodes the answer names are, between them, every node the model has not
// retired. The answer names none — every such node is written — so the count
// is the file's alone, and a node the export selected and left out without a
// word is a node this count comes up one short on.
func TestRunExportWritesEveryNodeTheModelHasNotRetired(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
		args  []string
	}{
		{
			name:  "the reproduction, one slab of which is within nothing",
			files: orphanModel(),
		},
		{
			name:  "a model whose every node something spatial contains",
			files: exportModel(),
		},
		{
			name:  "a model with a screen within nothing backing a room's edge",
			files: boundaryModel(),
			args:  boundaryFlags(),
		},
		{
			name:  "a model whose elements are drawn, placed and widened within nothing",
			files: withinNothing(t),
			args:  bodyFlags(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, root, stderr := exporting(t, exitSuccess, testCase.files, testCase.args...)
			require.True(t, result.Derived, stderr)

			source := artefact(t, result)

			graph, found := dfcad.LoadGraph(root)
			require.NotNil(t, graph, "%v", found)

			names := make(map[string]bool)
			for _, held := range parsed(t, source) {
				if strings.HasPrefix(held.keyword, "IFCREL") || len(held.attributes) < 3 {
					continue
				}
				names[strings.Trim(held.attributes[2], "'")] = true
			}

			var unretired, written []string
			for node := range graph.Nodes().All() {
				if node.Retired() {
					continue
				}
				unretired = append(unretired, string(node.ID()))

				if names[string(node.ID())] {
					written = append(written, string(node.ID()))
				}
			}

			slices.Sort(unretired)
			slices.Sort(written)

			assert.Equal(t, unretired, written,
				"every node the model has not retired is written, and none is left out with no word said")
		})
	}
}

// withinNothing is the element fixture with a located panel beside it, every
// element of which has been taken out of the storey it stood in.
func withinNothing(t *testing.T) map[string]string {
	t.Helper()

	files := withALocatedElement()

	entities := strings.ReplaceAll(files["entities/site.dfc"], `
  (within site:L-01)
  (boundary`, `
  (boundary`)
	entities = strings.Replace(entities, `(type Panel)
  (geometry point)
  (frame frame:building)
  (within site:L-01)`, `(type Panel)
  (geometry point)
  (frame frame:building)`, 1)

	require.NotContains(t, entities, "(within site:L-01)", "every element has been taken out of the storey")
	files["entities/site.dfc"] = entities

	return files
}

// shapesOf is the geometric instances of source with their instance numbers
// taken out, sorted: the shapes a file holds, independent of where in it they
// were numbered.
func shapesOf(source string) []string {
	var out []string

	numbered := regexp.MustCompile(`#\d+`)
	for _, line := range strings.Split(source, "\n") {
		_, instance, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		for _, keyword := range []string{
			"IFCCARTESIANPOINT(", "IFCDIRECTION(", "IFCPOLYLINE(", "IFCSHAPEREPRESENTATION(",
			"IFCARBITRARYCLOSEDPROFILEDEF(", "IFCEXTRUDEDAREASOLID(", "IFCPROPERTYSINGLEVALUE(",
		} {
			if strings.HasPrefix(instance, keyword) {
				out = append(out, numbered.ReplaceAllString(instance, "#"))
			}
		}
	}

	slices.Sort(out)

	return out
}

// TestRunExportDrawsANodeWithinNothingAsItWouldAnyOther is its own function
// because what it asserts is a relation between two exports rather than a line
// of one: the same elements, contained in a storey at the root frame's datum
// and then within nothing, are drawn, placed and widened into the same shapes.
// What differs is where they hang, and nothing else.
func TestRunExportDrawsANodeWithinNothingAsItWouldAnyOther(t *testing.T) {
	contained, _, stderr := exporting(t, exitSuccess, withALocatedElement(), bodyFlags()...)
	require.True(t, contained.Derived, stderr)

	uncontained, _, stderr := exporting(t, exitSuccess, withinNothing(t), bodyFlags()...)
	require.True(t, uncontained.Derived, stderr)

	inStorey, loose := artefact(t, contained), artefact(t, uncontained)

	t.Run("draws the same shapes from the same claims", func(t *testing.T) {
		assert.Equal(t, shapesOf(inStorey), shapesOf(loose))
		assert.Equal(t, 3, strings.Count(loose, "'FootPrint','Curve2D'"))
		assert.Equal(t, 3, strings.Count(loose, "'Body','SweptSolid'"))
	})

	t.Run("places a point where the model puts it, in the root frame", func(t *testing.T) {
		placement := placedBy(t, loose, "site:PNL-01")
		require.Equal(t, "$", placement.attributes[0], "the panel is placed relative to nothing")

		instances := parsed(t, loose)
		axis := instances[placement.attributes[1]]
		require.Equal(t, "IFCAXIS2PLACEMENT3D", axis.keyword)

		assert.Equal(t, "IFCCARTESIANPOINT", instances[axis.attributes[0]].keyword)
		assert.Equal(t, []string{"(2.5,1.5,1.2)"}, instances[axis.attributes[0]].attributes)
	})

	t.Run("contains none of them", func(t *testing.T) {
		assert.NotContains(t, loose, "IFCRELCONTAINEDINSPATIALSTRUCTURE")
	})
}
