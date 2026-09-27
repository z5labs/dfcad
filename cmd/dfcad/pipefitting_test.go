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

// pipeFittingRegistry and pipeFittingEntities are the reproduction
// IfcPipeFitting was reported against: one building, and one element
// classified as a pipe fitting within it, which the export used to write as a
// proxy naming its type.
const pipeFittingRegistry = `(project
  (label "Unwritten classification repro")
  (globalid-namespace "https://example.org/unwritten-repro"))

(namespace frame (description "Coordinate frames."))
(namespace site (description "Semantic nodes."))

(frame frame:site (label "Site grid") (unit usft))

(type building (kind Building) (geometry absent) (description "The dwelling."))
(type pipefitting (kind Element) (geometry absent) (description "Classified IfcPipeFitting.")
  (classification "IFC4" "IfcPipeFitting"))
`

const pipeFittingEntities = `(node site:dwelling (label "Dwelling") (kind Building) (type building))
(node site:pipefitting-1 (label "A IfcPipeFitting") (kind Element) (type pipefitting)
  (within site:dwelling))
`

// pipeFittingModel is the reproduction as a fixture tree.
func pipeFittingModel() map[string]string {
	return map[string]string{
		"registry.dfc": pipeFittingRegistry,
		"entities.dfc": pipeFittingEntities,
	}
}

// withALocatedPipeFitting is the element fixture's located panel reclassified
// as the cleanout the consumer this story came from declares — a drain or sewer
// cleanout — which is authored as a point.
func withALocatedPipeFitting() map[string]string {
	files := withALocatedElement()
	files["registry.dfc"] = strings.Replace(files["registry.dfc"],
		`(description "A distribution board, recorded at the point it was set out at."))`,
		`(description "A distribution board, recorded at the point it was set out at.")
  (classification "IFC4" "IfcPipeFitting"))`, 1)

	return files
}

// withAnOutlinedPipeFitting is the element fixture's countertop reclassified
// as a pipe fitting — a junction drawn from its outline: a node whose declared
// geometry is an area, drawn with a footprint and a body.
func withAnOutlinedPipeFitting(t *testing.T) map[string]string {
	return reclassified(t, elementModel(), "IfcFurnishingElement", "IfcPipeFitting")
}

func TestRunExportWritesAPipeFittingAsTheEntityItIsClassifiedAs(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, pipeFittingModel())
	require.True(t, result.Derived, stderr)

	source := artefact(t, result)

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		assert.Equal(t, pipeFittingGolden(t, source), source,
			"the exported artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})

	t.Run("writes the reproduction's pipe fitting as IFCPIPEFITTING", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:pipefitting-1"))

		assert.Equal(t, "IFCPIPEFITTING", held.keyword)
	})

	t.Run("gives it the attribute list IFC4 gives it, which is one past the tag", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:pipefitting-1"))

		require.Len(t, held.attributes, 9)
		assert.Equal(t, "'site:pipefitting-1'", held.attributes[2], "Name")
		assert.Equal(t, "'A IfcPipeFitting'", held.attributes[3], "Description")
		assert.Equal(t, "'pipefitting'", held.attributes[4], "ObjectType")
		assert.Equal(t, "$", held.attributes[8], "PredefinedType is absent, as export --help says")
	})

	t.Run("contains it in the building it is within", func(t *testing.T) {
		assert.Contains(t, containedIn(t, source), namedIn(t, source, "site:pipefitting-1"))
	})

	t.Run("reports no classification it could not carry", func(t *testing.T) {
		assert.NotNil(t, result.Classifications)
		assert.Empty(t, result.Classifications)
	})

	t.Run("writes no warning about it", func(t *testing.T) {
		assert.NotContains(t, stderr, "IfcPipeFitting")
		assert.NotContains(t, stderr, "IfcBuildingElementProxy")
	})

	t.Run("writes no proxy at all", func(t *testing.T) {
		assert.NotContains(t, source, "IFCBUILDINGELEMENTPROXY")
	})
}

// pipeFittingGolden is the recorded pipe fitting artefact, rewritten from got under
// -update.
func pipeFittingGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/pipefitting.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestRunExportWritesAPipeFittingWithEverythingTheProxyCarried is its own function
// because what it asserts is a relation between two exports rather than a
// line of one.
//
// The proxy a pipe fitting used to be written as and the IfcPipeFitting it is
// written as now have the same attribute list — the head IfcElement declares and one
// optional attribute after it — so a file differing from the proxied one in
// anything but the keyword is a file which lost or changed something the proxy
// carried: a GlobalId, a name, a placement, a containment, a footprint or a
// body.
func TestRunExportWritesAPipeFittingWithEverythingTheProxyCarried(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
		args  []string
	}{
		{
			name:  "the reproduction, carried by nothing but its identity and its containment",
			files: pipeFittingModel(),
		},
		{
			name:  "a cleanout placed at the point the model puts it",
			files: withALocatedPipeFitting(),
			args:  bodyFlags(),
		},
		{
			name:  "a junction drawn from an outline, with a footprint and a body",
			files: withAnOutlinedPipeFitting(t),
			args:  bodyFlags(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			classified, _, stderr := exporting(t, exitSuccess, testCase.files, testCase.args...)
			require.True(t, classified.Derived, stderr)
			assert.Empty(t, classified.Classifications)

			proxied, _, stderr := exporting(t, exitSuccess,
				unclassified(t, testCase.files, `(classification "IFC4" "IfcPipeFitting")`), testCase.args...)
			require.True(t, proxied.Derived, stderr)

			fitting, proxy := artefact(t, classified), artefact(t, proxied)
			require.Contains(t, proxy, "IFCBUILDINGELEMENTPROXY(", "the pipe fitting was a proxy without its classification")
			require.Contains(t, fitting, "IFCPIPEFITTING(")

			assert.Equal(t,
				strings.Replace(proxy, "IFCBUILDINGELEMENTPROXY(", "IFCPIPEFITTING(", 1), fitting,
				"the pipe fitting is the proxy it replaced, under the entity it is classified as")
		})
	}
}

// TestRunExportDrawsAPipeFittingAsAnyDrawnElementIsDrawn is its own function
// because it asserts on the shapes rather than on the entity: a junction
// drawn from an outline carries the footprint and the body a countertop drawn
// from that outline carries, and a cleanout placed by a point stands where the
// panel it was reclassified from stood.
func TestRunExportDrawsAPipeFittingAsAnyDrawnElementIsDrawn(t *testing.T) {
	t.Run("gives a junction drawn from an outline a footprint and a body", func(t *testing.T) {
		counter, _, stderr := exporting(t, exitSuccess, elementModel(), bodyFlags()...)
		require.True(t, counter.Derived, stderr)

		fitting, _, stderr := exporting(t, exitSuccess, withAnOutlinedPipeFitting(t), bodyFlags()...)
		require.True(t, fitting.Derived, stderr)

		drawn := artefact(t, fitting)

		assert.Equal(t, shapesOf(artefact(t, counter)), shapesOf(drawn))
		assert.Equal(t, 3, strings.Count(drawn, "'FootPrint','Curve2D'"))
		assert.Equal(t, 3, strings.Count(drawn, "'Body','SweptSolid'"))

		held := instance(t, drawn, namedIn(t, drawn, "site:K-01"))
		require.Equal(t, "IFCPIPEFITTING", held.keyword)
		assert.NotEqual(t, "$", held.attributes[6], "Representation")
		assert.Equal(t, "$", held.attributes[8], "PredefinedType")
	})

	t.Run("places a cleanout where the model puts it", func(t *testing.T) {
		result, _, stderr := exporting(t, exitSuccess, withALocatedPipeFitting(), bodyFlags()...)
		require.True(t, result.Derived, stderr)

		source := artefact(t, result)

		held := instance(t, source, namedIn(t, source, "site:PNL-01"))
		require.Equal(t, "IFCPIPEFITTING", held.keyword)
		assert.Equal(t, "$", held.attributes[6], "a point has a position and no extent")

		instances := parsed(t, source)
		axis := instances[placedBy(t, source, "site:PNL-01").attributes[1]]
		require.Equal(t, "IFCAXIS2PLACEMENT3D", axis.keyword)
		assert.Equal(t, []string{"(2.5,1.5,1.2)"}, instances[axis.attributes[0]].attributes)
	})
}

// TestRunExportOfAPipeFittingIsAFunctionOfTheModel is the determinism property
// over the reproduction: the same tree exports to the same bytes, keyed by the
// digest of the tree and nothing else, and a second run over it finds the file
// already there.
func TestRunExportOfAPipeFittingIsAFunctionOfTheModel(t *testing.T) {
	root := tree(t, pipeFittingModel())

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

// TestExportUsageStatesWhatPredefinedTypeAPipeFittingIsWrittenWith is its own
// function because it is about the documentation rather than about a run: the
// value written for IfcPipeFitting's PredefinedType is a decision, and a decision
// nobody can read is one a receiving system has to infer.
func TestExportUsageStatesWhatPredefinedTypeAPipeFittingIsWrittenWith(t *testing.T) {
	assert.Contains(t, exportUsage, " IfcPipeFitting\n")
	assert.Contains(t, exportUsage, "IfcPipeFitting is written the same way.")
	assert.Contains(t, exportUsage, "for a pipe fitting a bend, a junction, a transition, an entry, an exit,\nan obstruction or a connector")
	assert.Contains(t, exportUsage, `"cleanout"`)
	assert.Contains(t, exportUsage, `absent, "$", and never
as .NOTDEFINED.`)
}

// TestExportHintOffersAPipeFitting is its own function because it is about the
// refusal's hint rather than a run: a registry author told which entities a
// classification may name is told IfcPipeFitting is one of them.
func TestExportHintOffersAPipeFitting(t *testing.T) {
	assert.Contains(t, writableEntities(), "IFCPIPEFITTING")
}
