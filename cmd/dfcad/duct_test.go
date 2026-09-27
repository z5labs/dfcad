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

// ductRegistry and ductEntities are the reproduction IfcDuctSegment was
// reported against: one building, and one element classified as a duct
// segment within it, which the export used to write as a proxy naming its
// type.
const ductRegistry = `(project
  (label "Unwritten classification repro")
  (globalid-namespace "https://example.org/unwritten-repro"))

(namespace frame (description "Coordinate frames."))
(namespace site (description "Semantic nodes."))

(frame frame:site (label "Site grid") (unit usft))

(type building (kind Building) (geometry absent) (description "The dwelling."))
(type ductsegment (kind Element) (geometry absent) (description "Classified IfcDuctSegment.")
  (classification "IFC4" "IfcDuctSegment"))
`

const ductEntities = `(node site:dwelling (label "Dwelling") (kind Building) (type building))
(node site:ductsegment-1 (label "A IfcDuctSegment") (kind Element) (type ductsegment)
  (within site:dwelling))
`

// ductModel is the reproduction as a fixture tree.
func ductModel() map[string]string {
	return map[string]string{
		"registry.dfc": ductRegistry,
		"entities.dfc": ductEntities,
	}
}

// withADuctRun is the element fixture's railing reclassified as the duct
// run the consumer this story came from authors, drawn as its centreline: a
// node whose declared geometry is a line, drawn with a footprint and a body.
func withADuctRun(t *testing.T) map[string]string {
	return reclassified(t, elementModel(), "IfcRailing", "IfcDuctSegment")
}

func TestRunExportWritesADuctSegmentAsTheEntityItIsClassifiedAs(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, ductModel())
	require.True(t, result.Derived, stderr)

	source := artefact(t, result)

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		assert.Equal(t, ductGolden(t, source), source,
			"the exported artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})

	t.Run("writes the reproduction's duct segment as IFCDUCTSEGMENT", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:ductsegment-1"))

		assert.Equal(t, "IFCDUCTSEGMENT", held.keyword)
	})

	t.Run("gives it the attribute list IFC4 gives it, which is one past the tag", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:ductsegment-1"))

		require.Len(t, held.attributes, 9)
		assert.Equal(t, "'site:ductsegment-1'", held.attributes[2], "Name")
		assert.Equal(t, "'A IfcDuctSegment'", held.attributes[3], "Description")
		assert.Equal(t, "'ductsegment'", held.attributes[4], "ObjectType")
		assert.Equal(t, "$", held.attributes[8], "PredefinedType is absent, as export --help says")
	})

	t.Run("contains it in the building it is within", func(t *testing.T) {
		assert.Contains(t, containedIn(t, source), namedIn(t, source, "site:ductsegment-1"))
	})

	t.Run("reports no classification it could not carry", func(t *testing.T) {
		assert.NotNil(t, result.Classifications)
		assert.Empty(t, result.Classifications)
	})

	t.Run("writes no warning about it", func(t *testing.T) {
		assert.NotContains(t, stderr, "IfcDuctSegment")
		assert.NotContains(t, stderr, "IfcBuildingElementProxy")
	})

	t.Run("writes no proxy at all", func(t *testing.T) {
		assert.NotContains(t, source, "IFCBUILDINGELEMENTPROXY")
	})
}

// ductGolden is the recorded duct segment artefact, rewritten from got
// under -update.
func ductGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/ducts.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestRunExportWritesADuctSegmentWithEverythingTheProxyCarried is its own
// function because what it asserts is a relation between two exports rather
// than a line of one.
//
// The proxy a duct segment used to be written as and the IfcDuctSegment it
// is written as now have the same attribute list — the head IfcElement
// declares and one optional attribute after it — so a file differing from the
// proxied one in anything but the keyword is a file which lost or changed
// something the proxy carried: a GlobalId, a name, a placement, a containment,
// a footprint or a body.
func TestRunExportWritesADuctSegmentWithEverythingTheProxyCarried(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
		args  []string
	}{
		{
			name:  "the reproduction, carried by nothing but its identity and its containment",
			files: ductModel(),
		},
		{
			name:  "a run drawn from a line, with a footprint and a body",
			files: withADuctRun(t),
			args:  bodyFlags(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			classified, _, stderr := exporting(t, exitSuccess, testCase.files, testCase.args...)
			require.True(t, classified.Derived, stderr)
			assert.Empty(t, classified.Classifications)

			proxied, _, stderr := exporting(t, exitSuccess,
				unclassified(t, testCase.files, `(classification "IFC4" "IfcDuctSegment")`), testCase.args...)
			require.True(t, proxied.Derived, stderr)

			duct, proxy := artefact(t, classified), artefact(t, proxied)
			require.Contains(t, proxy, "IFCBUILDINGELEMENTPROXY(", "the duct was a proxy without its classification")
			require.Contains(t, duct, "IFCDUCTSEGMENT(")

			assert.Equal(t,
				strings.Replace(proxy, "IFCBUILDINGELEMENTPROXY(", "IFCDUCTSEGMENT(", 1), duct,
				"the duct segment is the proxy it replaced, under the entity it is classified as")
		})
	}
}

// TestRunExportDrawsADuctSegmentAsAnyDrawnElementIsDrawn is its own function
// because it asserts on the shapes rather than on the entity: a duct run drawn
// from a line carries the footprint and the body the railing drawn from that
// line carries.
func TestRunExportDrawsADuctSegmentAsAnyDrawnElementIsDrawn(t *testing.T) {
	railing, _, stderr := exporting(t, exitSuccess, elementModel(), bodyFlags()...)
	require.True(t, railing.Derived, stderr)

	duct, _, stderr := exporting(t, exitSuccess, withADuctRun(t), bodyFlags()...)
	require.True(t, duct.Derived, stderr)

	drawn := artefact(t, duct)

	assert.Equal(t, shapesOf(artefact(t, railing)), shapesOf(drawn))
	assert.Equal(t, 3, strings.Count(drawn, "'FootPrint','Curve2D'"))
	assert.Equal(t, 3, strings.Count(drawn, "'Body','SweptSolid'"))

	held := instance(t, drawn, namedIn(t, drawn, "site:R-01"))
	require.Equal(t, "IFCDUCTSEGMENT", held.keyword)
	assert.NotEqual(t, "$", held.attributes[6], "Representation")
	assert.Equal(t, "$", held.attributes[8], "PredefinedType")
}

// TestRunExportOfADuctSegmentIsAFunctionOfTheModel is the determinism
// property over the reproduction: the same tree exports to the same bytes,
// keyed by the digest of the tree and nothing else, and a second run over it
// finds the file already there.
func TestRunExportOfADuctSegmentIsAFunctionOfTheModel(t *testing.T) {
	root := tree(t, ductModel())

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

// TestExportUsageStatesWhatPredefinedTypeADuctSegmentIsWrittenWith is its own
// function because it is about the documentation rather than about a run: the
// value written for IfcDuctSegment's PredefinedType is a decision, and a
// decision nobody can read is one a receiving system has to infer.
func TestExportUsageStatesWhatPredefinedTypeADuctSegmentIsWrittenWith(t *testing.T) {
	assert.Contains(t, exportUsage, " IfcDuctSegment ")
	assert.Contains(t, exportUsage, "An IfcDuctSegment is\nwritten the same way.")
	assert.Contains(t, exportUsage, "for a duct segment a rigid segment or a flexible one")
	assert.Contains(t, exportUsage, `"duct"`)
	assert.Contains(t, exportUsage, `absent, "$", and never
as .NOTDEFINED.`)
}

// TestExportHintOffersADuctSegment is its own function because it is about
// the refusal's hint rather than a run: a registry author told which entities
// a classification may name is told IfcDuctSegment is one of them.
func TestExportHintOffersADuctSegment(t *testing.T) {
	assert.Contains(t, writableEntities(), "IFCDUCTSEGMENT")
}
