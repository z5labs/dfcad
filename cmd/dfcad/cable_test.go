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

// cableRegistry and cableEntities are the reproduction IfcCableSegment was
// reported against: one building, and one element classified as a cable
// segment within it, which the export used to write as a proxy naming its
// type.
const cableRegistry = `(project
  (label "Unwritten classification repro")
  (globalid-namespace "https://example.org/unwritten-repro"))

(namespace frame (description "Coordinate frames."))
(namespace site (description "Semantic nodes."))

(frame frame:site (label "Site grid") (unit usft))

(type building (kind Building) (geometry absent) (description "The dwelling."))
(type cablesegment (kind Element) (geometry absent) (description "Classified IfcCableSegment.")
  (classification "IFC4" "IfcCableSegment"))
`

const cableEntities = `(node site:dwelling (label "Dwelling") (kind Building) (type building))
(node site:cablesegment-1 (label "A IfcCableSegment") (kind Element) (type cablesegment)
  (within site:dwelling))
`

// cableModel is the reproduction as a fixture tree.
func cableModel() map[string]string {
	return map[string]string{
		"registry.dfc": cableRegistry,
		"entities.dfc": cableEntities,
	}
}

// withACableRun is the element fixture's railing reclassified as the
// low-voltage run the consumer this story came from authors: a node whose
// declared geometry is a line, drawn with a footprint and a body.
func withACableRun(t *testing.T) map[string]string {
	return reclassified(t, elementModel(), "IfcRailing", "IfcCableSegment")
}

func TestRunExportWritesACableSegmentAsTheEntityItIsClassifiedAs(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, cableModel())
	require.True(t, result.Derived, stderr)

	source := artefact(t, result)

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		assert.Equal(t, cableGolden(t, source), source,
			"the exported artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})

	t.Run("writes the reproduction's cable segment as IFCCABLESEGMENT", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:cablesegment-1"))

		assert.Equal(t, "IFCCABLESEGMENT", held.keyword)
	})

	t.Run("gives it the attribute list IFC4 gives it, which is one past the tag", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:cablesegment-1"))

		require.Len(t, held.attributes, 9)
		assert.Equal(t, "'site:cablesegment-1'", held.attributes[2], "Name")
		assert.Equal(t, "'A IfcCableSegment'", held.attributes[3], "Description")
		assert.Equal(t, "'cablesegment'", held.attributes[4], "ObjectType")
		assert.Equal(t, "$", held.attributes[8], "PredefinedType is absent, as export --help says")
	})

	t.Run("contains it in the building it is within", func(t *testing.T) {
		assert.Contains(t, containedIn(t, source), namedIn(t, source, "site:cablesegment-1"))
	})

	t.Run("reports no classification it could not carry", func(t *testing.T) {
		assert.NotNil(t, result.Classifications)
		assert.Empty(t, result.Classifications)
	})

	t.Run("writes no warning about it", func(t *testing.T) {
		assert.NotContains(t, stderr, "IfcCableSegment")
		assert.NotContains(t, stderr, "IfcBuildingElementProxy")
	})

	t.Run("writes no proxy at all", func(t *testing.T) {
		assert.NotContains(t, source, "IFCBUILDINGELEMENTPROXY")
	})
}

// cableGolden is the recorded cable segment artefact, rewritten from got
// under -update.
func cableGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/cables.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestRunExportWritesACableSegmentWithEverythingTheProxyCarried is its own
// function because what it asserts is a relation between two exports rather
// than a line of one.
//
// The proxy a cable segment used to be written as and the IfcCableSegment it
// is written as now have the same attribute list — the head IfcElement
// declares and one optional attribute after it — so a file differing from the
// proxied one in anything but the keyword is a file which lost or changed
// something the proxy carried: a GlobalId, a name, a placement, a containment,
// a footprint or a body.
func TestRunExportWritesACableSegmentWithEverythingTheProxyCarried(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
		args  []string
	}{
		{
			name:  "the reproduction, carried by nothing but its identity and its containment",
			files: cableModel(),
		},
		{
			name:  "a run drawn from a line, with a footprint and a body",
			files: withACableRun(t),
			args:  bodyFlags(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			classified, _, stderr := exporting(t, exitSuccess, testCase.files, testCase.args...)
			require.True(t, classified.Derived, stderr)
			assert.Empty(t, classified.Classifications)

			proxied, _, stderr := exporting(t, exitSuccess,
				unclassified(t, testCase.files, `(classification "IFC4" "IfcCableSegment")`), testCase.args...)
			require.True(t, proxied.Derived, stderr)

			cable, proxy := artefact(t, classified), artefact(t, proxied)
			require.Contains(t, proxy, "IFCBUILDINGELEMENTPROXY(", "the cable was a proxy without its classification")
			require.Contains(t, cable, "IFCCABLESEGMENT(")

			assert.Equal(t,
				strings.Replace(proxy, "IFCBUILDINGELEMENTPROXY(", "IFCCABLESEGMENT(", 1), cable,
				"the cable segment is the proxy it replaced, under the entity it is classified as")
		})
	}
}

// TestRunExportDrawsACableSegmentAsAnyDrawnElementIsDrawn is its own function
// because it asserts on the shapes rather than on the entity: a cable run drawn
// from a line carries the footprint and the body the railing drawn from that
// line carries.
func TestRunExportDrawsACableSegmentAsAnyDrawnElementIsDrawn(t *testing.T) {
	railing, _, stderr := exporting(t, exitSuccess, elementModel(), bodyFlags()...)
	require.True(t, railing.Derived, stderr)

	cable, _, stderr := exporting(t, exitSuccess, withACableRun(t), bodyFlags()...)
	require.True(t, cable.Derived, stderr)

	drawn := artefact(t, cable)

	assert.Equal(t, shapesOf(artefact(t, railing)), shapesOf(drawn))
	assert.Equal(t, 3, strings.Count(drawn, "'FootPrint','Curve2D'"))
	assert.Equal(t, 3, strings.Count(drawn, "'Body','SweptSolid'"))

	held := instance(t, drawn, namedIn(t, drawn, "site:R-01"))
	require.Equal(t, "IFCCABLESEGMENT", held.keyword)
	assert.NotEqual(t, "$", held.attributes[6], "Representation")
	assert.Equal(t, "$", held.attributes[8], "PredefinedType")
}

// TestRunExportOfACableSegmentIsAFunctionOfTheModel is the determinism
// property over the reproduction: the same tree exports to the same bytes,
// keyed by the digest of the tree and nothing else, and a second run over it
// finds the file already there.
func TestRunExportOfACableSegmentIsAFunctionOfTheModel(t *testing.T) {
	root := tree(t, cableModel())

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

// TestExportUsageStatesWhatPredefinedTypeACableSegmentIsWrittenWith is its own
// function because it is about the documentation rather than about a run: the
// value written for IfcCableSegment's PredefinedType is a decision, and a
// decision nobody can read is one a receiving system has to infer.
func TestExportUsageStatesWhatPredefinedTypeACableSegmentIsWrittenWith(t *testing.T) {
	assert.Contains(t, exportUsage, "IfcCableSegment as much as a wall")
	assert.Contains(t, exportUsage, `absent, "$", and never
as .NOTDEFINED.`)
}

// TestExportHintOffersACableSegment is its own function because it is about
// the refusal's hint rather than a run: a registry author told which entities
// a classification may name is told IfcCableSegment is one of them.
func TestExportHintOffersACableSegment(t *testing.T) {
	assert.Contains(t, writableEntities(), "IFCCABLESEGMENT")
}
