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

// terminalRegistry and terminalEntities are the reproduction IfcAirTerminal
// was reported against: one building, and one element classified as an air
// terminal within it, which the export used to write as a proxy naming its
// type.
const terminalRegistry = `(project
  (label "Unwritten classification repro")
  (globalid-namespace "https://example.org/unwritten-repro"))

(namespace frame (description "Coordinate frames."))
(namespace site (description "Semantic nodes."))

(frame frame:site (label "Site grid") (unit usft))

(type building (kind Building) (geometry absent) (description "The dwelling."))
(type airterminal (kind Element) (geometry absent) (description "Classified IfcAirTerminal.")
  (classification "IFC4" "IfcAirTerminal"))
`

const terminalEntities = `(node site:dwelling (label "Dwelling") (kind Building) (type building))
(node site:airterminal-1 (label "A IfcAirTerminal") (kind Element) (type airterminal)
  (within site:dwelling))
`

// terminalModel is the reproduction as a fixture tree.
func terminalModel() map[string]string {
	return map[string]string{
		"registry.dfc": terminalRegistry,
		"entities.dfc": terminalEntities,
	}
}

// unclassified is files with the IFC4 classification line quoted taken out of
// the registry, which is the model whose nodes of that type reach the file as a
// proxy: what an air terminal was written as before this writer held its
// attribute list.
func unclassified(t *testing.T, files map[string]string, classification string) map[string]string {
	t.Helper()

	out := make(map[string]string, len(files))
	for name, source := range files {
		out[name] = source
	}

	registry := strings.Replace(out["registry.dfc"], "\n  "+classification, "", 1)
	require.NotEqual(t, out["registry.dfc"], registry, "the registry declares %s", classification)
	out["registry.dfc"] = registry

	return out
}

// reclassified is files with the IFC4 classification of one type replaced by
// another.
func reclassified(t *testing.T, files map[string]string, from, to string) map[string]string {
	t.Helper()

	out := make(map[string]string, len(files))
	for name, source := range files {
		out[name] = source
	}

	registry := strings.Replace(out["registry.dfc"], `"`+from+`"`, `"`+to+`"`, 1)
	require.NotEqual(t, out["registry.dfc"], registry, "the registry classifies something as %s", from)
	out["registry.dfc"] = registry

	return out
}

// withALocatedTerminal is the element fixture's located panel reclassified as
// the supply register the consumer this story came from authors: a node whose
// declared geometry is a point.
func withALocatedTerminal() map[string]string {
	files := withALocatedElement()
	files["registry.dfc"] = strings.Replace(files["registry.dfc"],
		`(description "A distribution board, recorded at the point it was set out at."))`,
		`(description "A distribution board, recorded at the point it was set out at.")
  (classification "IFC4" "IfcAirTerminal"))`, 1)

	return files
}

func TestRunExportWritesAnAirTerminalAsTheEntityItIsClassifiedAs(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, terminalModel())
	require.True(t, result.Derived, stderr)

	source := artefact(t, result)

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		assert.Equal(t, terminalGolden(t, source), source,
			"the exported artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})

	t.Run("writes the reproduction's air terminal as IFCAIRTERMINAL", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:airterminal-1"))

		assert.Equal(t, "IFCAIRTERMINAL", held.keyword)
	})

	t.Run("gives it the attribute list IFC4 gives it, which is one past the tag", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:airterminal-1"))

		require.Len(t, held.attributes, 9)
		assert.Equal(t, "'site:airterminal-1'", held.attributes[2], "Name")
		assert.Equal(t, "'A IfcAirTerminal'", held.attributes[3], "Description")
		assert.Equal(t, "'airterminal'", held.attributes[4], "ObjectType")
		assert.Equal(t, "$", held.attributes[8], "PredefinedType is absent, as export --help says")
	})

	t.Run("contains it in the building it is within", func(t *testing.T) {
		assert.Contains(t, containedIn(t, source), namedIn(t, source, "site:airterminal-1"))
	})

	t.Run("reports no classification it could not carry", func(t *testing.T) {
		assert.NotNil(t, result.Classifications)
		assert.Empty(t, result.Classifications)
	})

	t.Run("writes no warning about it", func(t *testing.T) {
		assert.NotContains(t, stderr, "IfcAirTerminal")
		assert.NotContains(t, stderr, "IfcBuildingElementProxy")
	})

	t.Run("writes no proxy at all", func(t *testing.T) {
		assert.NotContains(t, source, "IFCBUILDINGELEMENTPROXY")
	})
}

// terminalGolden is the recorded air terminal artefact, rewritten from got
// under -update.
func terminalGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/terminals.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestRunExportWritesAnAirTerminalWithEverythingTheProxyCarried is its own
// function because what it asserts is a relation between two exports rather
// than a line of one.
//
// The proxy an air terminal used to be written as and the IfcAirTerminal it is
// written as now have the same attribute list — the head IfcElement declares
// and one optional attribute after it — so a file differing from the proxied
// one in anything but the keyword is a file which lost or changed something the
// proxy carried: a GlobalId, a name, a placement, a containment, a footprint or
// a body.
func TestRunExportWritesAnAirTerminalWithEverythingTheProxyCarried(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
		args  []string
	}{
		{
			name:  "the reproduction, carried by nothing but its identity and its containment",
			files: terminalModel(),
		},
		{
			name:  "a register placed at the point the model puts it",
			files: withALocatedTerminal(),
			args:  bodyFlags(),
		},
		{
			name:  "a terminal drawn from an outline, with a footprint and a body",
			files: reclassified(t, elementModel(), "IfcFurnishingElement", "IfcAirTerminal"),
			args:  bodyFlags(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			classified, _, stderr := exporting(t, exitSuccess, testCase.files, testCase.args...)
			require.True(t, classified.Derived, stderr)
			assert.Empty(t, classified.Classifications)

			proxied, _, stderr := exporting(t, exitSuccess,
				unclassified(t, testCase.files, `(classification "IFC4" "IfcAirTerminal")`), testCase.args...)
			require.True(t, proxied.Derived, stderr)

			terminal, proxy := artefact(t, classified), artefact(t, proxied)
			require.Contains(t, proxy, "IFCBUILDINGELEMENTPROXY(", "the terminal was a proxy without its classification")
			require.Contains(t, terminal, "IFCAIRTERMINAL(")

			assert.Equal(t,
				strings.Replace(proxy, "IFCBUILDINGELEMENTPROXY(", "IFCAIRTERMINAL(", 1), terminal,
				"the terminal is the proxy it replaced, under the entity it is classified as")
		})
	}
}

// TestRunExportDrawsAnAirTerminalAsAnyDrawnElementIsDrawn is its own function
// because it asserts on the shapes rather than on the entity: an air terminal
// drawn from an outline carries the footprint and the body a countertop drawn
// from that outline carries, and a register placed by a point stands where the
// panel it was reclassified from stood.
func TestRunExportDrawsAnAirTerminalAsAnyDrawnElementIsDrawn(t *testing.T) {
	t.Run("gives a terminal drawn from an outline a footprint and a body", func(t *testing.T) {
		counter, _, stderr := exporting(t, exitSuccess, elementModel(), bodyFlags()...)
		require.True(t, counter.Derived, stderr)

		terminal, _, stderr := exporting(t, exitSuccess,
			reclassified(t, elementModel(), "IfcFurnishingElement", "IfcAirTerminal"), bodyFlags()...)
		require.True(t, terminal.Derived, stderr)

		drawn := artefact(t, terminal)

		assert.Equal(t, shapesOf(artefact(t, counter)), shapesOf(drawn))
		assert.Equal(t, 3, strings.Count(drawn, "'FootPrint','Curve2D'"))
		assert.Equal(t, 3, strings.Count(drawn, "'Body','SweptSolid'"))

		held := instance(t, drawn, namedIn(t, drawn, "site:K-01"))
		require.Equal(t, "IFCAIRTERMINAL", held.keyword)
		assert.NotEqual(t, "$", held.attributes[6], "Representation")
	})

	t.Run("places a register where the model puts it", func(t *testing.T) {
		result, _, stderr := exporting(t, exitSuccess, withALocatedTerminal(), bodyFlags()...)
		require.True(t, result.Derived, stderr)

		source := artefact(t, result)

		held := instance(t, source, namedIn(t, source, "site:PNL-01"))
		require.Equal(t, "IFCAIRTERMINAL", held.keyword)
		assert.Equal(t, "$", held.attributes[6], "a point has a position and no extent")

		instances := parsed(t, source)
		axis := instances[placedBy(t, source, "site:PNL-01").attributes[1]]
		require.Equal(t, "IFCAXIS2PLACEMENT3D", axis.keyword)
		assert.Equal(t, []string{"(2.5,1.5,1.2)"}, instances[axis.attributes[0]].attributes)
	})
}

// TestRunExportOfAnAirTerminalIsAFunctionOfTheModel is the determinism property
// over the reproduction: the same tree exports to the same bytes, keyed by the
// digest of the tree and nothing else, and a second run over it finds the file
// already there.
func TestRunExportOfAnAirTerminalIsAFunctionOfTheModel(t *testing.T) {
	root := tree(t, terminalModel())

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
