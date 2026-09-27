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

// distributionRegistry and distributionEntities are the reproduction
// IfcDistributionElement was reported against: one building, and one element
// classified as a distribution element within it, which the export used to
// write as a proxy naming its type.
const distributionRegistry = `(project
  (label "Unwritten classification repro")
  (globalid-namespace "https://example.org/unwritten-repro"))

(namespace frame (description "Coordinate frames."))
(namespace site (description "Semantic nodes."))

(frame frame:site (label "Site grid") (unit usft))

(type building (kind Building) (geometry absent) (description "The dwelling."))
(type distributionelement (kind Element) (geometry absent) (description "Classified IfcDistributionElement.")
  (classification "IFC4" "IfcDistributionElement"))
`

const distributionEntities = `(node site:dwelling (label "Dwelling") (kind Building) (type building))
(node site:distributionelement-1 (label "A IfcDistributionElement") (kind Element) (type distributionelement)
  (within site:dwelling))
`

// distributionModel is the reproduction as a fixture tree.
func distributionModel() map[string]string {
	return map[string]string{
		"registry.dfc": distributionRegistry,
		"entities.dfc": distributionEntities,
	}
}

// withALocatedDevice is the element fixture's located panel reclassified as
// the device the consumer this story came from authors — a receptacle, a
// switch, a smoke detector — which is a node whose declared geometry is a
// point.
func withALocatedDevice() map[string]string {
	files := withALocatedElement()
	files["registry.dfc"] = strings.Replace(files["registry.dfc"],
		`(description "A distribution board, recorded at the point it was set out at."))`,
		`(description "A distribution board, recorded at the point it was set out at.")
  (classification "IFC4" "IfcDistributionElement"))`, 1)

	return files
}

// withADrawnDistributionElement is the element fixture's countertop
// reclassified as a distribution element, so that one is drawn from an
// outline with a footprint and a body.
func withADrawnDistributionElement(t *testing.T) map[string]string {
	return reclassified(t, elementModel(), "IfcFurnishingElement", "IfcDistributionElement")
}

func TestRunExportWritesADistributionElementAsTheEntityItIsClassifiedAs(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, distributionModel())
	require.True(t, result.Derived, stderr)

	source := artefact(t, result)

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		assert.Equal(t, distributionGolden(t, source), source,
			"the exported artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})

	t.Run("writes the reproduction's distribution element as IFCDISTRIBUTIONELEMENT", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:distributionelement-1"))

		assert.Equal(t, "IFCDISTRIBUTIONELEMENT", held.keyword)
	})

	t.Run("gives it the attribute list IFC4 gives an element, which ends at the tag", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:distributionelement-1"))

		require.Len(t, held.attributes, 8)
		assert.Equal(t, "$", held.attributes[1], "OwnerHistory")
		assert.Equal(t, "'site:distributionelement-1'", held.attributes[2], "Name")
		assert.Equal(t, "'A IfcDistributionElement'", held.attributes[3], "Description")
		assert.Equal(t, "'distributionelement'", held.attributes[4], "ObjectType")
		assert.Equal(t, "$", held.attributes[6], "Representation: the type draws nothing")
		assert.Equal(t, "$", held.attributes[7], "Tag")
	})

	t.Run("contains it in the building it is within", func(t *testing.T) {
		assert.Contains(t, containedIn(t, source), namedIn(t, source, "site:distributionelement-1"))
	})

	t.Run("reports no classification it could not carry", func(t *testing.T) {
		assert.NotNil(t, result.Classifications)
		assert.Empty(t, result.Classifications)
	})

	t.Run("writes no warning about it", func(t *testing.T) {
		assert.NotContains(t, stderr, "IfcDistributionElement")
		assert.NotContains(t, stderr, "IfcBuildingElementProxy")
	})

	t.Run("writes no proxy at all", func(t *testing.T) {
		assert.NotContains(t, source, "IFCBUILDINGELEMENTPROXY")
	})
}

// distributionGolden is the recorded distribution element artefact,
// rewritten from got under -update.
func distributionGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/distribution.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// asDistributionElement is a proxied artefact with its one proxy rewritten as
// the distribution element it stood in for: the keyword replaced, and the
// PredefinedType the proxy carries — absent — taken off the end, because IFC4
// gives a distribution element no PredefinedType and its list ends at Tag.
func asDistributionElement(t *testing.T, proxied string) string {
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

		lines[i] = at + "=IFCDISTRIBUTIONELEMENT(" + head + ");"
		rewritten++
	}

	require.Equal(t, 1, rewritten, "the artefact holds one proxy")

	return strings.Join(lines, "\n")
}

// TestRunExportWritesADistributionElementWithEverythingTheProxyCarried is its
// own function because what it asserts is a relation between two exports
// rather than a line of one.
//
// The proxy a distribution element used to be written as and the
// IfcDistributionElement it is written as now share the head IfcElement
// declares, Tag included, and differ in the PredefinedType the proxy adds
// after it — which it writes absent. So a file differing from the proxied one
// in anything but the keyword and that one attribute is a file which lost or
// changed something the proxy carried: a GlobalId, a name, a placement, a
// containment, a footprint or a body.
func TestRunExportWritesADistributionElementWithEverythingTheProxyCarried(t *testing.T) {
	testCases := []struct {
		name  string
		files func(t *testing.T) map[string]string
		args  []string
	}{
		{
			name:  "the reproduction, carried by nothing but its identity and its containment",
			files: func(*testing.T) map[string]string { return distributionModel() },
		},
		{
			name:  "a device placed at the point the model puts it",
			files: func(*testing.T) map[string]string { return withALocatedDevice() },
			args:  bodyFlags(),
		},
		{
			name:  "a distribution element drawn from an outline, with a footprint and a body",
			files: withADrawnDistributionElement,
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
				unclassified(t, files, `(classification "IFC4" "IfcDistributionElement")`), testCase.args...)
			require.True(t, proxied.Derived, stderr)

			distribution, proxy := artefact(t, classified), artefact(t, proxied)
			require.Contains(t, distribution, "IFCDISTRIBUTIONELEMENT(")

			assert.Equal(t, asDistributionElement(t, proxy), distribution,
				"the distribution element is the proxy it replaced, under the entity it is classified as")
		})
	}
}

// TestRunExportDrawsADistributionElementAsAnyDrawnElementIsDrawn is its own
// function because it asserts on the shapes rather than on the entity: a
// distribution element drawn from an outline carries the footprint and the
// body a countertop drawn from that outline carries, and a device placed by a
// point stands where the panel it was reclassified from stood.
func TestRunExportDrawsADistributionElementAsAnyDrawnElementIsDrawn(t *testing.T) {
	t.Run("gives a distribution element drawn from an outline a footprint and a body", func(t *testing.T) {
		counter, _, stderr := exporting(t, exitSuccess, elementModel(), bodyFlags()...)
		require.True(t, counter.Derived, stderr)

		distribution, _, stderr := exporting(t, exitSuccess, withADrawnDistributionElement(t), bodyFlags()...)
		require.True(t, distribution.Derived, stderr)

		drawn := artefact(t, distribution)

		assert.Equal(t, shapesOf(artefact(t, counter)), shapesOf(drawn))
		assert.Equal(t, 3, strings.Count(drawn, "'FootPrint','Curve2D'"))
		assert.Equal(t, 3, strings.Count(drawn, "'Body','SweptSolid'"))

		held := instance(t, drawn, namedIn(t, drawn, "site:K-01"))
		require.Equal(t, "IFCDISTRIBUTIONELEMENT", held.keyword)
		require.Len(t, held.attributes, 8)
		assert.NotEqual(t, "$", held.attributes[6], "Representation")
	})

	t.Run("places a device where the model puts it", func(t *testing.T) {
		result, _, stderr := exporting(t, exitSuccess, withALocatedDevice(), bodyFlags()...)
		require.True(t, result.Derived, stderr)

		source := artefact(t, result)

		held := instance(t, source, namedIn(t, source, "site:PNL-01"))
		require.Equal(t, "IFCDISTRIBUTIONELEMENT", held.keyword)
		require.Len(t, held.attributes, 8)
		assert.Equal(t, "$", held.attributes[6], "a point has a position and no extent")

		instances := parsed(t, source)
		axis := instances[placedBy(t, source, "site:PNL-01").attributes[1]]
		require.Equal(t, "IFCAXIS2PLACEMENT3D", axis.keyword)
		assert.Equal(t, []string{"(2.5,1.5,1.2)"}, instances[axis.attributes[0]].attributes)
	})
}

// TestRunExportOfADistributionElementIsAFunctionOfTheModel is the determinism
// property over the reproduction: the same tree exports to the same bytes,
// keyed by the digest of the tree and nothing else, and a second run over it
// finds the file already there.
func TestRunExportOfADistributionElementIsAFunctionOfTheModel(t *testing.T) {
	root := tree(t, distributionModel())

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

// TestExportUsageListsADistributionElement is its own function because it is
// about the documentation rather than about a run: a registry author reading
// the list of entities a classification may name finds IfcDistributionElement
// in it, and finds what it is written with.
func TestExportUsageListsADistributionElement(t *testing.T) {
	assert.Contains(t, exportUsage, " IfcDistributionElement ")
	assert.Contains(t, exportUsage, "an IfcDistributionElement — a receptacle")
}

// TestExportHintOffersADistributionElement is its own function because it is
// about the refusal's hint rather than a run: a registry author told which
// entities a classification may name is told IfcDistributionElement is one of
// them.
func TestExportHintOffersADistributionElement(t *testing.T) {
	assert.Contains(t, strings.Split(writableEntities(), ", "), "IFCDISTRIBUTIONELEMENT")
}
