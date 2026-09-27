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

// chamberRegistry and chamberEntities are the reproduction
// IfcDistributionChamberElement was reported against: one building, and one
// element classified as a distribution chamber within it, which the
// export used to write as a proxy naming its type.
const chamberRegistry = `(project
  (label "Unwritten classification repro")
  (globalid-namespace "https://example.org/unwritten-repro"))

(namespace frame (description "Coordinate frames."))
(namespace site (description "Semantic nodes."))

(frame frame:site (label "Site grid") (unit usft))

(type building (kind Building) (geometry absent) (description "The dwelling."))
(type distributionchamberelement (kind Element) (geometry absent) (description "Classified IfcDistributionChamberElement.")
  (classification "IFC4" "IfcDistributionChamberElement"))
`

const chamberEntities = `(node site:dwelling (label "Dwelling") (kind Building) (type building))
(node site:distributionchamberelement-1 (label "A IfcDistributionChamberElement") (kind Element) (type distributionchamberelement)
  (within site:dwelling))
`

// chamberModel is the reproduction as a fixture tree.
func chamberModel() map[string]string {
	return map[string]string{
		"registry.dfc": chamberRegistry,
		"entities.dfc": chamberEntities,
	}
}

// withALocatedChamber is the element fixture's located panel reclassified as
// the septic distribution box the consumer this story came from authors: a
// node whose declared geometry is a point.
func withALocatedChamber() map[string]string {
	files := withALocatedElement()
	files["registry.dfc"] = strings.Replace(files["registry.dfc"],
		`(description "A distribution board, recorded at the point it was set out at."))`,
		`(description "A distribution board, recorded at the point it was set out at.")
  (classification "IFC4" "IfcDistributionChamberElement"))`, 1)

	return files
}

func TestRunExportWritesADistributionChamberElementAsTheEntityItIsClassifiedAs(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, chamberModel())
	require.True(t, result.Derived, stderr)

	source := artefact(t, result)

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		assert.Equal(t, chamberGolden(t, source), source,
			"the exported artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})

	t.Run("writes the reproduction's chamber as IFCDISTRIBUTIONCHAMBERELEMENT", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:distributionchamberelement-1"))

		assert.Equal(t, "IFCDISTRIBUTIONCHAMBERELEMENT", held.keyword)
	})

	t.Run("gives it the attribute list IFC4 gives it, which is one past the tag", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:distributionchamberelement-1"))

		require.Len(t, held.attributes, 9)
		assert.Equal(t, "'site:distributionchamberelement-1'", held.attributes[2], "Name")
		assert.Equal(t, "'A IfcDistributionChamberElement'", held.attributes[3], "Description")
		assert.Equal(t, "'distributionchamberelement'", held.attributes[4], "ObjectType")
		assert.Equal(t, "$", held.attributes[8], "PredefinedType is absent, as export --help says")
	})

	t.Run("contains it in the building it is within", func(t *testing.T) {
		assert.Contains(t, containedIn(t, source), namedIn(t, source, "site:distributionchamberelement-1"))
	})

	t.Run("reports no classification it could not carry", func(t *testing.T) {
		assert.NotNil(t, result.Classifications)
		assert.Empty(t, result.Classifications)
	})

	t.Run("writes no warning about it", func(t *testing.T) {
		assert.NotContains(t, stderr, "IfcDistributionChamberElement")
		assert.NotContains(t, stderr, "IfcBuildingElementProxy")
	})

	t.Run("writes no proxy at all", func(t *testing.T) {
		assert.NotContains(t, source, "IFCBUILDINGELEMENTPROXY")
	})
}

// chamberGolden is the recorded distribution chamber artefact, rewritten
// from got under -update.
func chamberGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/chamber.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestRunExportWritesADistributionChamberElementWithEverythingTheProxyCarried is
// its own function because what it asserts is a relation between two exports
// rather than a line of one.
//
// The proxy a distribution chamber used to be written as and the
// IfcDistributionChamberElement it is written as now have the same attribute list
// — the head IfcElement declares and one optional attribute after it — so a
// file differing from the proxied one in anything but the keyword is a file
// which lost or changed something the proxy carried: a GlobalId, a name, a
// placement, a containment, a footprint or a body.
func TestRunExportWritesADistributionChamberElementWithEverythingTheProxyCarried(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
		args  []string
	}{
		{
			name:  "the reproduction, carried by nothing but its identity and its containment",
			files: chamberModel(),
		},
		{
			name:  "a distribution box placed at the point the model puts it",
			files: withALocatedChamber(),
			args:  bodyFlags(),
		},
		{
			name:  "a chamber drawn from an outline, with a footprint and a body",
			files: reclassified(t, elementModel(), "IfcFurnishingElement", "IfcDistributionChamberElement"),
			args:  bodyFlags(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			classified, _, stderr := exporting(t, exitSuccess, testCase.files, testCase.args...)
			require.True(t, classified.Derived, stderr)
			assert.Empty(t, classified.Classifications)

			proxied, _, stderr := exporting(t, exitSuccess,
				unclassified(t, testCase.files, `(classification "IFC4" "IfcDistributionChamberElement")`), testCase.args...)
			require.True(t, proxied.Derived, stderr)

			chamber, proxy := artefact(t, classified), artefact(t, proxied)
			require.Contains(t, proxy, "IFCBUILDINGELEMENTPROXY(", "the chamber was a proxy without its classification")
			require.Contains(t, chamber, "IFCDISTRIBUTIONCHAMBERELEMENT(")

			assert.Equal(t,
				strings.Replace(proxy, "IFCBUILDINGELEMENTPROXY(", "IFCDISTRIBUTIONCHAMBERELEMENT(", 1), chamber,
				"the chamber is the proxy it replaced, under the entity it is classified as")
		})
	}
}

// TestRunExportDrawsADistributionChamberElementAsAnyDrawnElementIsDrawn is its own
// function because it asserts on the shapes rather than on the entity: an
// chamber drawn from an outline carries the footprint and the body a
// countertop drawn from that outline carries, and a distribution box placed by a
// point stands where the panel it was reclassified from stood.
func TestRunExportDrawsADistributionChamberElementAsAnyDrawnElementIsDrawn(t *testing.T) {
	t.Run("gives a chamber drawn from an outline a footprint and a body", func(t *testing.T) {
		counter, _, stderr := exporting(t, exitSuccess, elementModel(), bodyFlags()...)
		require.True(t, counter.Derived, stderr)

		chamber, _, stderr := exporting(t, exitSuccess,
			reclassified(t, elementModel(), "IfcFurnishingElement", "IfcDistributionChamberElement"), bodyFlags()...)
		require.True(t, chamber.Derived, stderr)

		drawn := artefact(t, chamber)

		assert.Equal(t, shapesOf(artefact(t, counter)), shapesOf(drawn))
		assert.Equal(t, 3, strings.Count(drawn, "'FootPrint','Curve2D'"))
		assert.Equal(t, 3, strings.Count(drawn, "'Body','SweptSolid'"))

		held := instance(t, drawn, namedIn(t, drawn, "site:K-01"))
		require.Equal(t, "IFCDISTRIBUTIONCHAMBERELEMENT", held.keyword)
		assert.NotEqual(t, "$", held.attributes[6], "Representation")
		assert.Equal(t, "$", held.attributes[8], "PredefinedType")
	})

	t.Run("places a distribution box where the model puts it", func(t *testing.T) {
		result, _, stderr := exporting(t, exitSuccess, withALocatedChamber(), bodyFlags()...)
		require.True(t, result.Derived, stderr)

		source := artefact(t, result)

		held := instance(t, source, namedIn(t, source, "site:PNL-01"))
		require.Equal(t, "IFCDISTRIBUTIONCHAMBERELEMENT", held.keyword)
		assert.Equal(t, "$", held.attributes[6], "a point has a position and no extent")

		instances := parsed(t, source)
		axis := instances[placedBy(t, source, "site:PNL-01").attributes[1]]
		require.Equal(t, "IFCAXIS2PLACEMENT3D", axis.keyword)
		assert.Equal(t, []string{"(2.5,1.5,1.2)"}, instances[axis.attributes[0]].attributes)
	})
}

// TestRunExportOfADistributionChamberElementIsAFunctionOfTheModel is the
// determinism property over the reproduction: the same tree exports to the
// same bytes, keyed by the digest of the tree and nothing else, and a second
// run over it finds the file already there.
func TestRunExportOfADistributionChamberElementIsAFunctionOfTheModel(t *testing.T) {
	root := tree(t, chamberModel())

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

// TestExportUsageStatesWhatPredefinedTypeADistributionChamberElementIsWrittenWith
// is its own function because it is about the documentation rather than about
// a run: the value written for IfcDistributionChamberElement's PredefinedType is a
// decision, and a decision nobody can read is one a receiving system has to
// infer.
func TestExportUsageStatesWhatPredefinedTypeADistributionChamberElementIsWrittenWith(t *testing.T) {
	assert.Contains(t, exportUsage, "  IfcDistributionChamberElement  ")
	assert.Contains(t, exportUsage, "IfcDistributionChamberElement as much as any of them")
	assert.Contains(t, exportUsage, "for a distribution chamber a manhole")
	assert.Contains(t, exportUsage, `"septic-dbox"`)
	assert.Contains(t, exportUsage, `absent, "$", and never
as .NOTDEFINED.`)
}

// TestExportHintOffersADistributionChamberElement is its own function because it
// is about the refusal's hint rather than a run: a registry author told which
// entities a classification may name is told IfcDistributionChamberElement is one
// of them.
func TestExportHintOffersADistributionChamberElement(t *testing.T) {
	assert.Contains(t, writableEntities(), "IFCDISTRIBUTIONCHAMBERELEMENT")
}
