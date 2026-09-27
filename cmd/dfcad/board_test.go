// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// boardRegistry and boardEntities are the reproduction
// IfcElectricDistributionBoard was reported against: one building, and one element
// classified as an electric distribution board within it, which the export used to
// write as a proxy naming its type.
const boardRegistry = `(project
  (label "Unwritten classification repro")
  (globalid-namespace "https://example.org/unwritten-repro"))

(namespace frame (description "Coordinate frames."))
(namespace site (description "Semantic nodes."))

(frame frame:site (label "Site grid") (unit usft))

(type building (kind Building) (geometry absent) (description "The dwelling."))
(type electricdistributionboard (kind Element) (geometry absent) (description "Classified IfcElectricDistributionBoard.")
  (classification "IFC4" "IfcElectricDistributionBoard"))
`

const boardEntities = `(node site:dwelling (label "Dwelling") (kind Building) (type building))
(node site:electricdistributionboard-1 (label "A IfcElectricDistributionBoard") (kind Element) (type electricdistributionboard)
  (within site:dwelling))
`

// boardModel is the reproduction as a fixture tree.
func boardModel() map[string]string {
	return map[string]string{
		"registry.dfc": boardRegistry,
		"entities.dfc": boardEntities,
	}
}

// withALocatedDistributionBoard is the element fixture's located panel
// classified as the distribution board it is, which is how the consumer this
// story came from authors its panels: a node whose declared geometry is a
// point.
func withALocatedDistributionBoard() map[string]string {
	files := withALocatedElement()
	files["registry.dfc"] = strings.Replace(files["registry.dfc"],
		`(description "A distribution board, recorded at the point it was set out at."))`,
		`(description "A distribution board, recorded at the point it was set out at.")
  (classification "IFC4" "IfcElectricDistributionBoard"))`, 1)

	return files
}

// withAnOutlinedDistributionBoard is the element fixture's countertop
// reclassified as a distribution board, a load centre drawn from its outline:
// a node whose declared geometry is an area, drawn with a footprint and a
// body.
func withAnOutlinedDistributionBoard(t *testing.T) map[string]string {
	return reclassified(t, elementModel(), "IfcFurnishingElement", "IfcElectricDistributionBoard")
}

func TestRunExportWritesADistributionBoardAsTheEntityItIsClassifiedAs(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, boardModel())
	require.True(t, result.Derived, stderr)

	source := artefact(t, result)

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		assert.Equal(t, boardGolden(t, source), source,
			"the exported artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})

	t.Run("writes the reproduction's panel as IFCELECTRICDISTRIBUTIONBOARD", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:electricdistributionboard-1"))

		assert.Equal(t, "IFCELECTRICDISTRIBUTIONBOARD", held.keyword)
	})

	t.Run("gives it the attribute list IFC4 gives it, which is one past the tag", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:electricdistributionboard-1"))

		require.Len(t, held.attributes, 9)
		assert.Equal(t, "'site:electricdistributionboard-1'", held.attributes[2], "Name")
		assert.Equal(t, "'A IfcElectricDistributionBoard'", held.attributes[3], "Description")
		assert.Equal(t, "'electricdistributionboard'", held.attributes[4], "ObjectType")
		assert.Equal(t, "$", held.attributes[8], "PredefinedType is absent, as export --help says")
	})

	t.Run("contains it in the building it is within", func(t *testing.T) {
		assert.Contains(t, containedIn(t, source), namedIn(t, source, "site:electricdistributionboard-1"))
	})

	t.Run("reports no classification it could not carry", func(t *testing.T) {
		assert.NotNil(t, result.Classifications)
		assert.Empty(t, result.Classifications)
	})

	t.Run("writes no warning about it", func(t *testing.T) {
		assert.NotContains(t, stderr, "IfcElectricDistributionBoard")
		assert.NotContains(t, stderr, "IfcBuildingElementProxy")
	})

	t.Run("writes no proxy at all", func(t *testing.T) {
		assert.NotContains(t, source, "IFCBUILDINGELEMENTPROXY")
	})
}

// boardGolden is the recorded electric distribution board artefact, rewritten from
// got under -update.
func boardGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/board.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestRunExportWritesADistributionBoardWithEverythingTheProxyCarried is its
// own function because what it asserts is a relation between two exports
// rather than a line of one.
//
// The proxy a distribution board used to be written as and the
// IfcElectricDistributionBoard it is written as now have the same attribute list — the
// head IfcElement declares and one optional attribute after it — so a file
// differing from the proxied one in anything but the keyword is a file which
// lost or changed something the proxy carried: a GlobalId, a name, a
// placement, a containment, a footprint or a body.
func TestRunExportWritesADistributionBoardWithEverythingTheProxyCarried(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
		args  []string
	}{
		{
			name:  "the reproduction, carried by nothing but its identity and its containment",
			files: boardModel(),
		},
		{
			name:  "a panel placed at the point the model puts it",
			files: withALocatedDistributionBoard(),
			args:  bodyFlags(),
		},
		{
			name:  "a load centre drawn from an outline, with a footprint and a body",
			files: withAnOutlinedDistributionBoard(t),
			args:  bodyFlags(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			classified, _, stderr := exporting(t, exitSuccess, testCase.files, testCase.args...)
			require.True(t, classified.Derived, stderr)
			assert.Empty(t, classified.Classifications)

			proxied, _, stderr := exporting(t, exitSuccess,
				unclassified(t, testCase.files, `(classification "IFC4" "IfcElectricDistributionBoard")`), testCase.args...)
			require.True(t, proxied.Derived, stderr)

			board, proxy := artefact(t, classified), artefact(t, proxied)
			require.Contains(t, proxy, "IFCBUILDINGELEMENTPROXY(", "the board was a proxy without its classification")
			require.Contains(t, board, "IFCELECTRICDISTRIBUTIONBOARD(")

			assert.Equal(t,
				strings.Replace(proxy, "IFCBUILDINGELEMENTPROXY(", "IFCELECTRICDISTRIBUTIONBOARD(", 1), board,
				"the board is the proxy it replaced, under the entity it is classified as")
		})
	}
}

// TestRunExportDrawsADistributionBoardAsAnyDrawnElementIsDrawn is its own
// function because it asserts on the shapes rather than on the entity: a
// load centre drawn from an outline carries the footprint and the body a
// countertop drawn from that outline carries, and a panel placed by a point
// stands where the proxy it replaced stood.
func TestRunExportDrawsADistributionBoardAsAnyDrawnElementIsDrawn(t *testing.T) {
	t.Run("gives a load centre drawn from an outline a footprint and a body", func(t *testing.T) {
		counter, _, stderr := exporting(t, exitSuccess, elementModel(), bodyFlags()...)
		require.True(t, counter.Derived, stderr)

		board, _, stderr := exporting(t, exitSuccess, withAnOutlinedDistributionBoard(t), bodyFlags()...)
		require.True(t, board.Derived, stderr)

		drawn := artefact(t, board)

		assert.Equal(t, shapesOf(artefact(t, counter)), shapesOf(drawn))
		assert.Equal(t, 3, strings.Count(drawn, "'FootPrint','Curve2D'"))
		assert.Equal(t, 3, strings.Count(drawn, "'Body','SweptSolid'"))

		held := instance(t, drawn, namedIn(t, drawn, "site:K-01"))
		require.Equal(t, "IFCELECTRICDISTRIBUTIONBOARD", held.keyword)
		assert.NotEqual(t, "$", held.attributes[6], "Representation")
		assert.Equal(t, "$", held.attributes[8], "PredefinedType")
	})

	t.Run("places a panel where the model puts it", func(t *testing.T) {
		result, _, stderr := exporting(t, exitSuccess, withALocatedDistributionBoard(), bodyFlags()...)
		require.True(t, result.Derived, stderr)

		source := artefact(t, result)

		held := instance(t, source, namedIn(t, source, "site:PNL-01"))
		require.Equal(t, "IFCELECTRICDISTRIBUTIONBOARD", held.keyword)
		assert.Equal(t, "$", held.attributes[6], "a point has a position and no extent")

		instances := parsed(t, source)
		axis := instances[placedBy(t, source, "site:PNL-01").attributes[1]]
		require.Equal(t, "IFCAXIS2PLACEMENT3D", axis.keyword)
		assert.Equal(t, []string{"(2.5,1.5,1.2)"}, instances[axis.attributes[0]].attributes)
	})
}

// TestRunExportOfADistributionBoardIsAFunctionOfTheModel is the determinism
// property over the reproduction: the same tree exports to the same bytes,
// keyed by the digest of the tree and nothing else, and a second run over it
// finds the file already there.
func TestRunExportOfADistributionBoardIsAFunctionOfTheModel(t *testing.T) {
	root := tree(t, boardModel())

	stdout, stderr := invoke(t, exitSuccess, root, "export")
	first := listed[exportResult](t, stdout)
	require.True(t, first.Derived, stderr)

	digest, err := dfcad.DigestOf(root)
	require.NoError(t, err)
	assert.Equal(t, digest.String(), first.Digest)

	written := artefact(t, first)

	for range 4 {
		stdout, _ := invoke(t, exitSuccess, root, "export")
		again := listed[exportResult](t, stdout)

		assert.Equal(t, first.Digest, again.Digest)
		require.Len(t, again.Files, 1)
		assert.Equal(t, statusUnchanged, again.Files[0].Status)
		assert.Equal(t, written, artefact(t, again))
	}
}

// TestExportUsageStatesWhatPredefinedTypeADistributionBoardIsWrittenWith is
// its own function because it is about the documentation rather than about a
// run: the value written for IfcElectricDistributionBoard's PredefinedType is a
// decision, and a decision nobody can read is one a receiving system has to
// infer.
func TestExportUsageStatesWhatPredefinedTypeADistributionBoardIsWrittenWith(t *testing.T) {
	assert.Contains(t, exportUsage, " IfcElectricDistributionBoard ")
	assert.Contains(t, exportUsage, "An IfcElectricDistributionBoard is written the same way.")
	assert.Contains(t, exportUsage, "for an electric distribution board a distribution board, a consumer unit")
	assert.Contains(t, exportUsage, `"panel"`)
	assert.Contains(t, exportUsage, `absent, "$", and never
as .NOTDEFINED.`)
}

// TestExportHintOffersADistributionBoard is its own function because it is
// about the refusal's hint rather than a run: a registry author told which
// entities a classification may name is told IfcElectricDistributionBoard is one of
// them.
func TestExportHintOffersADistributionBoard(t *testing.T) {
	assert.Contains(t, writableEntities(), "IFCELECTRICDISTRIBUTIONBOARD")
}
