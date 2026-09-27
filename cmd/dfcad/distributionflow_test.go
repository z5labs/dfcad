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

// distributionFlowRegistry and distributionFlowEntities are the reproduction
// IfcDistributionFlowElement was reported against: one building, and one element
// classified as a distribution flow element within it, which the export used to
// write as a proxy naming its type.
const distributionFlowRegistry = `(project
  (label "Unwritten classification repro")
  (globalid-namespace "https://example.org/unwritten-repro"))

(namespace frame (description "Coordinate frames."))
(namespace site (description "Semantic nodes."))

(frame frame:site (label "Site grid") (unit usft))

(type building (kind Building) (geometry absent) (description "The dwelling."))
(type distributionflowelement (kind Element) (geometry absent) (description "Classified IfcDistributionFlowElement.")
  (classification "IFC4" "IfcDistributionFlowElement"))
`

const distributionFlowEntities = `(node site:dwelling (label "Dwelling") (kind Building) (type building))
(node site:distributionflowelement-1 (label "A IfcDistributionFlowElement") (kind Element) (type distributionflowelement)
  (within site:dwelling))
`

// distributionFlowModel is the reproduction as a fixture tree.
func distributionFlowModel() map[string]string {
	return map[string]string{
		"registry.dfc": distributionFlowRegistry,
		"entities.dfc": distributionFlowEntities,
	}
}

// withALocatedFlowDevice is the element fixture's located panel reclassified
// as the equipment the consumer this story came from authors — an air
// handler, a condenser, a water heater — which is a node whose declared
// geometry may be a point.
func withALocatedFlowDevice() map[string]string {
	files := withALocatedElement()
	files["registry.dfc"] = strings.Replace(files["registry.dfc"],
		`(description "A distribution board, recorded at the point it was set out at."))`,
		`(description "A distribution board, recorded at the point it was set out at.")
  (classification "IFC4" "IfcDistributionFlowElement"))`, 1)

	return files
}

// withADrawnDistributionFlowElement is the element fixture's countertop
// reclassified as a distribution flow element, so that one is drawn from an
// outline with a footprint and a body.
func withADrawnDistributionFlowElement(t *testing.T) map[string]string {
	return reclassified(t, elementModel(), "IfcFurnishingElement", "IfcDistributionFlowElement")
}

func TestRunExportWritesADistributionFlowElementAsTheEntityItIsClassifiedAs(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, distributionFlowModel())
	require.True(t, result.Derived, stderr)

	source := artefact(t, result)

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		assert.Equal(t, distributionFlowGolden(t, source), source,
			"the exported artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})

	t.Run("writes the reproduction's distribution flow element as IFCDISTRIBUTIONFLOWELEMENT", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:distributionflowelement-1"))

		assert.Equal(t, "IFCDISTRIBUTIONFLOWELEMENT", held.keyword)
	})

	t.Run("gives it the attribute list IFC4 gives an element, which ends at the tag", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:distributionflowelement-1"))

		require.Len(t, held.attributes, 8)
		assert.Equal(t, "$", held.attributes[1], "OwnerHistory")
		assert.Equal(t, "'site:distributionflowelement-1'", held.attributes[2], "Name")
		assert.Equal(t, "'A IfcDistributionFlowElement'", held.attributes[3], "Description")
		assert.Equal(t, "'distributionflowelement'", held.attributes[4], "ObjectType")
		assert.Equal(t, "$", held.attributes[6], "Representation: the type draws nothing")
		assert.Equal(t, "$", held.attributes[7], "Tag")
	})

	t.Run("contains it in the building it is within", func(t *testing.T) {
		assert.Contains(t, containedIn(t, source), namedIn(t, source, "site:distributionflowelement-1"))
	})

	t.Run("reports no classification it could not carry", func(t *testing.T) {
		assert.NotNil(t, result.Classifications)
		assert.Empty(t, result.Classifications)
	})

	t.Run("writes no warning about it", func(t *testing.T) {
		assert.NotContains(t, stderr, "IfcDistributionFlowElement")
		assert.NotContains(t, stderr, "IfcBuildingElementProxy")
	})

	t.Run("writes no proxy at all", func(t *testing.T) {
		assert.NotContains(t, source, "IFCBUILDINGELEMENTPROXY")
	})
}

// distributionFlowGolden is the recorded distribution flow element artefact,
// rewritten from got under -update.
func distributionFlowGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/distributionflow.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// asDistributionFlowElement is a proxied artefact with its one proxy rewritten as
// the distribution flow element it stood in for: the keyword replaced, and the
// PredefinedType the proxy carries — absent — taken off the end, because IFC4
// gives a distribution flow element no PredefinedType and its list ends at Tag.
func asDistributionFlowElement(t *testing.T, proxied string) string {
	t.Helper()

	lines := strings.Split(proxied, "\n")

	rewritten := 0
	for i, line := range lines {
		at, written, found := strings.Cut(line, "=IFCBUILDINGELEMENTPROXY(")
		if !found {
			continue
		}

		head, typed := strings.CutSuffix(written, ",$);")
		require.True(t, typed, "the proxy ends with an absent PredefinedType: %s", line)

		lines[i] = at + "=IFCDISTRIBUTIONFLOWELEMENT(" + head + ");"
		rewritten++
	}

	require.Equal(t, 1, rewritten, "the artefact holds one proxy")

	return strings.Join(lines, "\n")
}

// TestRunExportWritesADistributionFlowElementWithEverythingTheProxyCarried is its
// own function because what it asserts is a relation between two exports
// rather than a line of one.
//
// The proxy a distribution flow element used to be written as and the
// IfcDistributionFlowElement it is written as now share the head IfcElement
// declares, Tag included, and differ in the PredefinedType the proxy adds
// after it — which it writes absent. So a file differing from the proxied one
// in anything but the keyword and that one attribute is a file which lost or
// changed something the proxy carried: a GlobalId, a name, a placement, a
// containment, a footprint or a body.
func TestRunExportWritesADistributionFlowElementWithEverythingTheProxyCarried(t *testing.T) {
	testCases := []struct {
		name  string
		files func(t *testing.T) map[string]string
		args  []string
	}{
		{
			name:  "the reproduction, carried by nothing but its identity and its containment",
			files: func(*testing.T) map[string]string { return distributionFlowModel() },
		},
		{
			name:  "a piece of equipment placed at the point the model puts it",
			files: func(*testing.T) map[string]string { return withALocatedFlowDevice() },
			args:  bodyFlags(),
		},
		{
			name:  "a distribution flow element drawn from an outline, with a footprint and a body",
			files: withADrawnDistributionFlowElement,
			args:  bodyFlags(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			files := testCase.files(t)

			classified, _, stderr := exporting(t, exitSuccess, files, testCase.args...)
			require.True(t, classified.Derived, stderr)
			assert.Empty(t, classified.Classifications)

			proxied, _, stderr := exporting(t, exitSuccess,
				unclassified(t, files, `(classification "IFC4" "IfcDistributionFlowElement")`), testCase.args...)
			require.True(t, proxied.Derived, stderr)

			distribution, proxy := artefact(t, classified), artefact(t, proxied)
			require.Contains(t, distribution, "IFCDISTRIBUTIONFLOWELEMENT(")

			assert.Equal(t, asDistributionFlowElement(t, proxy), distribution,
				"the distribution flow element is the proxy it replaced, under the entity it is classified as")
		})
	}
}

// TestRunExportDrawsADistributionFlowElementAsAnyDrawnElementIsDrawn is its own
// function because it asserts on the shapes rather than on the entity: a
// distribution flow element drawn from an outline carries the footprint and the
// body a countertop drawn from that outline carries, and equipment placed by a
// point stands where the panel it was reclassified from stood.
func TestRunExportDrawsADistributionFlowElementAsAnyDrawnElementIsDrawn(t *testing.T) {
	t.Run("gives a distribution flow element drawn from an outline a footprint and a body", func(t *testing.T) {
		counter, _, stderr := exporting(t, exitSuccess, elementModel(), bodyFlags()...)
		require.True(t, counter.Derived, stderr)

		distribution, _, stderr := exporting(t, exitSuccess, withADrawnDistributionFlowElement(t), bodyFlags()...)
		require.True(t, distribution.Derived, stderr)

		drawn := artefact(t, distribution)

		assert.Equal(t, shapesOf(artefact(t, counter)), shapesOf(drawn))
		assert.Equal(t, 3, strings.Count(drawn, "'FootPrint','Curve2D'"))
		assert.Equal(t, 3, strings.Count(drawn, "'Body','SweptSolid'"))

		held := instance(t, drawn, namedIn(t, drawn, "site:K-01"))
		require.Equal(t, "IFCDISTRIBUTIONFLOWELEMENT", held.keyword)
		require.Len(t, held.attributes, 8)
		assert.NotEqual(t, "$", held.attributes[6], "Representation")
	})

	t.Run("places a piece of equipment where the model puts it", func(t *testing.T) {
		result, _, stderr := exporting(t, exitSuccess, withALocatedFlowDevice(), bodyFlags()...)
		require.True(t, result.Derived, stderr)

		source := artefact(t, result)

		held := instance(t, source, namedIn(t, source, "site:PNL-01"))
		require.Equal(t, "IFCDISTRIBUTIONFLOWELEMENT", held.keyword)
		require.Len(t, held.attributes, 8)
		assert.Equal(t, "$", held.attributes[6], "a point has a position and no extent")

		instances := parsed(t, source)
		axis := instances[placedBy(t, source, "site:PNL-01").attributes[1]]
		require.Equal(t, "IFCAXIS2PLACEMENT3D", axis.keyword)
		assert.Equal(t, []string{"(2.5,1.5,1.2)"}, instances[axis.attributes[0]].attributes)
	})
}

// TestRunExportOfADistributionFlowElementIsAFunctionOfTheModel is the determinism
// property over the reproduction: the same tree exports to the same bytes,
// keyed by the digest of the tree and nothing else, and a second run over it
// finds the file already there.
func TestRunExportOfADistributionFlowElementIsAFunctionOfTheModel(t *testing.T) {
	root := tree(t, distributionFlowModel())

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

// TestExportUsageListsADistributionFlowElement is its own function because it is
// about the documentation rather than about a run: a registry author reading
// the list of entities a classification may name finds IfcDistributionFlowElement
// in it, and finds what it is written with.
func TestExportUsageListsADistributionFlowElement(t *testing.T) {
	assert.Contains(t, exportUsage, " IfcDistributionFlowElement ")
	assert.Contains(t, exportUsage, "IfcDistributionFlowElement — an air handler")
}

// TestExportHintOffersADistributionFlowElement is its own function because it is
// about the refusal's hint rather than a run: a registry author told which
// entities a classification may name is told IfcDistributionFlowElement is one of
// them.
func TestExportHintOffersADistributionFlowElement(t *testing.T) {
	assert.Contains(t, strings.Split(writableEntities(), ", "), "IFCDISTRIBUTIONFLOWELEMENT")
}
