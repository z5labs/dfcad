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

// sanitaryRegistry and sanitaryEntities are the reproduction
// IfcSanitaryTerminal was reported against: one building, and one element
// classified as a sanitary terminal within it, which the export used to write
// as a proxy naming its type.
const sanitaryRegistry = `(project
  (label "Unwritten classification repro")
  (globalid-namespace "https://example.org/unwritten-repro"))

(namespace frame (description "Coordinate frames."))
(namespace site (description "Semantic nodes."))

(frame frame:site (label "Site grid") (unit usft))

(type building (kind Building) (geometry absent) (description "The dwelling."))
(type sanitaryterminal (kind Element) (geometry absent) (description "Classified IfcSanitaryTerminal.")
  (classification "IFC4" "IfcSanitaryTerminal"))
`

const sanitaryEntities = `(node site:dwelling (label "Dwelling") (kind Building) (type building))
(node site:sanitaryterminal-1 (label "A IfcSanitaryTerminal") (kind Element) (type sanitaryterminal)
  (within site:dwelling))
`

// sanitaryModel is the reproduction as a fixture tree.
func sanitaryModel() map[string]string {
	return map[string]string{
		"registry.dfc": sanitaryRegistry,
		"entities.dfc": sanitaryEntities,
	}
}

// withAnOutlinedPlumbingFixture is the element fixture's countertop
// reclassified as the plumbing fixture the consumer this story came from
// authors — a toilet, a lavatory, a sink, a tub or a shower: a node whose
// declared geometry is an area, drawn with a footprint and a body.
func withAnOutlinedPlumbingFixture(t *testing.T) map[string]string {
	return reclassified(t, elementModel(), "IfcFurnishingElement", "IfcSanitaryTerminal")
}

func TestRunExportWritesASanitaryTerminalAsTheEntityItIsClassifiedAs(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, sanitaryModel())
	require.True(t, result.Derived, stderr)

	source := artefact(t, result)

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		assert.Equal(t, sanitaryGolden(t, source), source,
			"the exported artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})

	t.Run("writes the reproduction's sanitary terminal as IFCSANITARYTERMINAL", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:sanitaryterminal-1"))

		assert.Equal(t, "IFCSANITARYTERMINAL", held.keyword)
	})

	t.Run("gives it the attribute list IFC4 gives it, which is one past the tag", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:sanitaryterminal-1"))

		require.Len(t, held.attributes, 9)
		assert.Equal(t, "'site:sanitaryterminal-1'", held.attributes[2], "Name")
		assert.Equal(t, "'A IfcSanitaryTerminal'", held.attributes[3], "Description")
		assert.Equal(t, "'sanitaryterminal'", held.attributes[4], "ObjectType")
		assert.Equal(t, "$", held.attributes[8], "PredefinedType is absent, as export --help says")
	})

	t.Run("contains it in the building it is within", func(t *testing.T) {
		assert.Contains(t, containedIn(t, source), namedIn(t, source, "site:sanitaryterminal-1"))
	})

	t.Run("reports no classification it could not carry", func(t *testing.T) {
		assert.NotNil(t, result.Classifications)
		assert.Empty(t, result.Classifications)
	})

	t.Run("writes no warning about it", func(t *testing.T) {
		assert.NotContains(t, stderr, "IfcSanitaryTerminal")
		assert.NotContains(t, stderr, "IfcBuildingElementProxy")
	})

	t.Run("writes no proxy at all", func(t *testing.T) {
		assert.NotContains(t, source, "IFCBUILDINGELEMENTPROXY")
	})
}

// sanitaryGolden is the recorded sanitary terminal artefact, rewritten from
// got under -update.
func sanitaryGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/sanitary.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestRunExportWritesASanitaryTerminalWithEverythingTheProxyCarried is its
// own function because what it asserts is a relation between two exports
// rather than a line of one.
//
// The proxy a sanitary terminal used to be written as and the
// IfcSanitaryTerminal it is written as now have the same attribute list — the
// head IfcElement declares and one optional attribute after it — so a file
// differing from the proxied one in anything but the keyword is a file which
// lost or changed something the proxy carried: a GlobalId, a name, a
// placement, a containment, a footprint or a body.
func TestRunExportWritesASanitaryTerminalWithEverythingTheProxyCarried(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
		args  []string
	}{
		{
			name:  "the reproduction, carried by nothing but its identity and its containment",
			files: sanitaryModel(),
		},
		{
			name:  "a plumbing fixture drawn from an outline, with a footprint and a body",
			files: withAnOutlinedPlumbingFixture(t),
			args:  bodyFlags(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			classified, _, stderr := exporting(t, exitSuccess, testCase.files, testCase.args...)
			require.True(t, classified.Derived, stderr)
			assert.Empty(t, classified.Classifications)

			proxied, _, stderr := exporting(t, exitSuccess,
				unclassified(t, testCase.files, `(classification "IFC4" "IfcSanitaryTerminal")`), testCase.args...)
			require.True(t, proxied.Derived, stderr)

			terminal, proxy := artefact(t, classified), artefact(t, proxied)
			require.Contains(t, proxy, "IFCBUILDINGELEMENTPROXY(", "the sanitary terminal was a proxy without its classification")
			require.Contains(t, terminal, "IFCSANITARYTERMINAL(")

			assert.Equal(t,
				strings.Replace(proxy, "IFCBUILDINGELEMENTPROXY(", "IFCSANITARYTERMINAL(", 1), terminal,
				"the sanitary terminal is the proxy it replaced, under the entity it is classified as")
		})
	}
}

// TestRunExportDrawsASanitaryTerminalAsAnyDrawnElementIsDrawn is its own
// function because it asserts on the shapes rather than on the entity: a
// plumbing fixture drawn from an outline carries the footprint and the body a
// countertop drawn from that outline carries.
func TestRunExportDrawsASanitaryTerminalAsAnyDrawnElementIsDrawn(t *testing.T) {
	counter, _, stderr := exporting(t, exitSuccess, elementModel(), bodyFlags()...)
	require.True(t, counter.Derived, stderr)

	fixture, _, stderr := exporting(t, exitSuccess, withAnOutlinedPlumbingFixture(t), bodyFlags()...)
	require.True(t, fixture.Derived, stderr)

	drawn := artefact(t, fixture)

	assert.Equal(t, shapesOf(artefact(t, counter)), shapesOf(drawn))
	assert.Equal(t, 3, strings.Count(drawn, "'FootPrint','Curve2D'"))
	assert.Equal(t, 3, strings.Count(drawn, "'Body','SweptSolid'"))

	held := instance(t, drawn, namedIn(t, drawn, "site:K-01"))
	require.Equal(t, "IFCSANITARYTERMINAL", held.keyword)
	assert.NotEqual(t, "$", held.attributes[6], "Representation")
	assert.Equal(t, "$", held.attributes[8], "PredefinedType")
}

// TestRunExportOfASanitaryTerminalIsAFunctionOfTheModel is the determinism
// property over the reproduction: the same tree exports to the same bytes,
// keyed by the digest of the tree and nothing else, and a second run over it
// finds the file already there.
func TestRunExportOfASanitaryTerminalIsAFunctionOfTheModel(t *testing.T) {
	root := tree(t, sanitaryModel())

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

// TestExportUsageStatesWhatPredefinedTypeASanitaryTerminalIsWrittenWith is
// its own function because it is about the documentation rather than about a
// run: the value written for IfcSanitaryTerminal's PredefinedType is a
// decision, and a decision nobody can read is one a receiving system has to
// infer.
func TestExportUsageStatesWhatPredefinedTypeASanitaryTerminalIsWrittenWith(t *testing.T) {
	assert.Contains(t, exportUsage, " IfcSanitaryTerminal\n")
	assert.Contains(t, exportUsage, "An IfcSanitaryTerminal is written the same way.")
	assert.Contains(t, exportUsage, "for a sanitary terminal a toilet pan, a wash-hand basin, a sink, a bath, a\nshower, a urinal or a bidet")
	assert.Contains(t, exportUsage, `"plumbing-fixture"`)
	assert.Contains(t, exportUsage, `absent, "$", and never
as .NOTDEFINED.`)
}

// TestExportHintOffersASanitaryTerminal is its own function because it is
// about the refusal's hint rather than a run: a registry author told which
// entities a classification may name is told IfcSanitaryTerminal is one of
// them.
func TestExportHintOffersASanitaryTerminal(t *testing.T) {
	assert.Contains(t, writableEntities(), "IFCSANITARYTERMINAL")
}
