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

// civilRegistry and civilEntities are the reproduction IfcCivilElement was
// reported against: one building, and one element classified as a civil
// element within it, which the export used to write as a proxy naming its
// type.
const civilRegistry = `(project
  (label "Unwritten classification repro")
  (globalid-namespace "https://example.org/unwritten-repro"))

(namespace frame (description "Coordinate frames."))
(namespace site (description "Semantic nodes."))

(frame frame:site (label "Site grid") (unit usft))

(type building (kind Building) (geometry absent) (description "The dwelling."))
(type civilelement (kind Element) (geometry absent) (description "Classified IfcCivilElement.")
  (classification "IFC4" "IfcCivilElement"))
`

const civilEntities = `(node site:dwelling (label "Dwelling") (kind Building) (type building))
(node site:civilelement-1 (label "A IfcCivilElement") (kind Element) (type civilelement)
  (within site:dwelling))
`

// civilModel is the reproduction as a fixture tree.
func civilModel() map[string]string {
	return map[string]string{
		"registry.dfc": civilRegistry,
		"entities.dfc": civilEntities,
	}
}

// withAPad is the element fixture's countertop reclassified as the hardscape
// the consumer this story came from authors as an area: a patio or a pad,
// drawn from an outline with a footprint and a body.
func withAPad(t *testing.T) map[string]string {
	return reclassified(t, elementModel(), "IfcFurnishingElement", "IfcCivilElement")
}

// withARetainingWallRun is the element fixture's railing reclassified as the
// hardscape the consumer authors as a line: a retaining wall, a curb or a
// culvert as a run, drawn with a footprint and a body.
func withARetainingWallRun(t *testing.T) map[string]string {
	return reclassified(t, elementModel(), "IfcRailing", "IfcCivilElement")
}

func TestRunExportWritesACivilElementAsTheEntityItIsClassifiedAs(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, civilModel())
	require.True(t, result.Derived, stderr)

	source := artefact(t, result)

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		assert.Equal(t, civilGolden(t, source), source,
			"the exported artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})

	t.Run("writes the reproduction's civil element as IFCCIVILELEMENT", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:civilelement-1"))

		assert.Equal(t, "IFCCIVILELEMENT", held.keyword)
	})

	t.Run("gives it the attribute list IFC4 gives an element, which ends at the tag", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:civilelement-1"))

		require.Len(t, held.attributes, 8)
		assert.Equal(t, "$", held.attributes[1], "OwnerHistory")
		assert.Equal(t, "'site:civilelement-1'", held.attributes[2], "Name")
		assert.Equal(t, "'A IfcCivilElement'", held.attributes[3], "Description")
		assert.Equal(t, "'civilelement'", held.attributes[4], "ObjectType")
		assert.Equal(t, "$", held.attributes[6], "Representation: the type draws nothing")
		assert.Equal(t, "$", held.attributes[7], "Tag")
	})

	t.Run("contains it in the building it is within", func(t *testing.T) {
		assert.Contains(t, containedIn(t, source), namedIn(t, source, "site:civilelement-1"))
	})

	t.Run("reports no classification it could not carry", func(t *testing.T) {
		assert.NotNil(t, result.Classifications)
		assert.Empty(t, result.Classifications)
	})

	t.Run("writes no warning about it", func(t *testing.T) {
		assert.NotContains(t, stderr, "IfcCivilElement")
		assert.NotContains(t, stderr, "IfcBuildingElementProxy")
	})

	t.Run("writes no proxy at all", func(t *testing.T) {
		assert.NotContains(t, source, "IFCBUILDINGELEMENTPROXY")
	})
}

// civilGolden is the recorded civil element artefact, rewritten from got
// under -update.
func civilGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/civil.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// asCivilElement is a proxied artefact with its one proxy rewritten as the
// civil element it stood in for: the keyword replaced, and the PredefinedType
// the proxy carries — absent — taken off the end, because IFC4 gives a civil
// element no PredefinedType and its list ends at Tag.
func asCivilElement(t *testing.T, proxied string) string {
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

		lines[i] = at + "=IFCCIVILELEMENT(" + head + ");"
		rewritten++
	}

	require.Equal(t, 1, rewritten, "the artefact holds one proxy")

	return strings.Join(lines, "\n")
}

// TestRunExportWritesACivilElementWithEverythingTheProxyCarried is its own
// function because what it asserts is a relation between two exports rather
// than a line of one.
//
// The proxy a civil element used to be written as and the IfcCivilElement it
// is written as now share the head IfcElement declares, Tag included, and
// differ in the PredefinedType the proxy adds after it — which it writes
// absent. So a file differing from the proxied one in anything but the keyword
// and that one attribute is a file which lost or changed something the proxy
// carried: a GlobalId, a name, a placement, a containment, a footprint or a
// body.
func TestRunExportWritesACivilElementWithEverythingTheProxyCarried(t *testing.T) {
	testCases := []struct {
		name  string
		files func(t *testing.T) map[string]string
		args  []string
	}{
		{
			name:  "the reproduction, carried by nothing but its identity and its containment",
			files: func(*testing.T) map[string]string { return civilModel() },
		},
		{
			name:  "a pad drawn from an outline, with a footprint and a body",
			files: withAPad,
			args:  bodyFlags(),
		},
		{
			name:  "a retaining wall drawn from a line, with a footprint and a body",
			files: withARetainingWallRun,
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
				unclassified(t, files, `(classification "IFC4" "IfcCivilElement")`), testCase.args...)
			require.True(t, proxied.Derived, stderr)

			civil, proxy := artefact(t, classified), artefact(t, proxied)
			require.Contains(t, civil, "IFCCIVILELEMENT(")

			assert.Equal(t, asCivilElement(t, proxy), civil,
				"the civil element is the proxy it replaced, under the entity it is classified as")
		})
	}
}

// TestRunExportDrawsACivilElementAsAnyDrawnElementIsDrawn is its own function
// because it asserts on the shapes rather than on the entity: a civil element
// drawn from an outline carries the footprint and the body the countertop drawn
// from that outline carries, and one drawn from a line carries what the railing
// drawn from that line carries.
func TestRunExportDrawsACivilElementAsAnyDrawnElementIsDrawn(t *testing.T) {
	testCases := []struct {
		name  string
		files func(t *testing.T) map[string]string
		node  string
	}{
		{
			name:  "draws a pad as the countertop it was reclassified from is drawn",
			files: withAPad,
			node:  "site:K-01",
		},
		{
			name:  "draws a retaining wall as the railing it was reclassified from is drawn",
			files: withARetainingWallRun,
			node:  "site:R-01",
		},
	}

	element, _, stderr := exporting(t, exitSuccess, elementModel(), bodyFlags()...)
	require.True(t, element.Derived, stderr)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			civil, _, stderr := exporting(t, exitSuccess, testCase.files(t), bodyFlags()...)
			require.True(t, civil.Derived, stderr)

			drawn := artefact(t, civil)

			assert.Equal(t, shapesOf(artefact(t, element)), shapesOf(drawn))
			assert.Equal(t, 3, strings.Count(drawn, "'FootPrint','Curve2D'"))
			assert.Equal(t, 3, strings.Count(drawn, "'Body','SweptSolid'"))

			held := instance(t, drawn, namedIn(t, drawn, testCase.node))
			require.Equal(t, "IFCCIVILELEMENT", held.keyword)
			require.Len(t, held.attributes, 8)
			assert.NotEqual(t, "$", held.attributes[6], "Representation")
		})
	}
}

// TestRunExportOfACivilElementIsAFunctionOfTheModel is the determinism
// property over the reproduction: the same tree exports to the same bytes,
// keyed by the digest of the tree and nothing else, and a second run over it
// finds the file already there.
func TestRunExportOfACivilElementIsAFunctionOfTheModel(t *testing.T) {
	root := tree(t, civilModel())

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

// TestExportUsageListsACivilElement is its own function because it is about
// the documentation rather than about a run: a registry author reading the
// list of entities a classification may name finds IfcCivilElement in it, and
// finds what it is written with.
func TestExportUsageListsACivilElement(t *testing.T) {
	assert.Contains(t, exportUsage, "\tIfcCivilElement ")
	assert.Contains(t, exportUsage, "an IfcCivilElement — a driveway")
}

// TestExportHintOffersACivilElement is its own function because it is about
// the refusal's hint rather than a run: a registry author told which entities
// a classification may name is told IfcCivilElement is one of them.
func TestExportHintOffersACivilElement(t *testing.T) {
	assert.Contains(t, writableEntities(), "IFCCIVILELEMENT")
}
