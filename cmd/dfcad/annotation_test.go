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

// annotationRegistry and annotationEntities are the reproduction IfcAnnotation
// was reported against: one building, and one element classified as an
// annotation within it, which the export used to write as a proxy naming its
// type.
const annotationRegistry = `(project
  (label "Unwritten classification repro")
  (globalid-namespace "https://example.org/unwritten-repro"))

(namespace frame (description "Coordinate frames."))
(namespace site (description "Semantic nodes."))

(frame frame:site (label "Site grid") (unit usft))

(type building (kind Building) (geometry absent) (description "The dwelling."))
(type annotation (kind Element) (geometry absent) (description "Classified IfcAnnotation.")
  (classification "IFC4" "IfcAnnotation"))
`

const annotationEntities = `(node site:dwelling (label "Dwelling") (kind Building) (type building))
(node site:annotation-1 (label "A IfcAnnotation") (kind Element) (type annotation)
  (within site:dwelling))
`

// annotationModel is the reproduction as a fixture tree.
func annotationModel() map[string]string {
	return map[string]string{
		"registry.dfc": annotationRegistry,
		"entities.dfc": annotationEntities,
	}
}

// withAnUncontainedAnnotation is the reproduction with the annotation within
// nothing, which is how every annotation in the model this story came from is
// authored: a north arrow or a survey tie stands in no room.
func withAnUncontainedAnnotation(t *testing.T) map[string]string {
	t.Helper()

	files := annotationModel()

	entities := strings.Replace(files["entities.dfc"], "\n  (within site:dwelling)", "", 1)
	require.NotEqual(t, files["entities.dfc"], entities, "the annotation is within the dwelling")
	files["entities.dfc"] = entities

	return files
}

// withALocatedAnnotation is the element fixture's located panel reclassified
// as an annotation: a node whose declared geometry is a point, which is what a
// survey tie is.
func withALocatedAnnotation() map[string]string {
	files := withALocatedElement()
	files["registry.dfc"] = strings.Replace(files["registry.dfc"],
		`(description "A distribution board, recorded at the point it was set out at."))`,
		`(description "A distribution board, recorded at the point it was set out at.")
  (classification "IFC4" "IfcAnnotation"))`, 1)

	return files
}

func TestRunExportWritesAnAnnotationAsTheEntityItIsClassifiedAs(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, annotationModel())
	require.True(t, result.Derived, stderr)

	source := artefact(t, result)

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		assert.Equal(t, annotationGolden(t, source), source,
			"the exported artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})

	t.Run("writes the reproduction's annotation as IFCANNOTATION", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:annotation-1"))

		assert.Equal(t, "IFCANNOTATION", held.keyword)
	})

	t.Run("gives it the attribute list IFC4 gives a product, which has no tag", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:annotation-1"))

		require.Len(t, held.attributes, 7)
		assert.Equal(t, "$", held.attributes[1], "OwnerHistory")
		assert.Equal(t, "'site:annotation-1'", held.attributes[2], "Name")
		assert.Equal(t, "'A IfcAnnotation'", held.attributes[3], "Description")
		assert.Equal(t, "'annotation'", held.attributes[4], "ObjectType")
		assert.Equal(t, "$", held.attributes[6], "Representation: the type draws nothing")
	})

	t.Run("contains it in the building it is within", func(t *testing.T) {
		assert.Contains(t, containedIn(t, source), namedIn(t, source, "site:annotation-1"))
	})

	t.Run("reports no classification it could not carry", func(t *testing.T) {
		assert.NotNil(t, result.Classifications)
		assert.Empty(t, result.Classifications)
	})

	t.Run("writes no warning about it", func(t *testing.T) {
		assert.NotContains(t, stderr, "IfcAnnotation")
		assert.NotContains(t, stderr, "IfcBuildingElementProxy")
	})

	t.Run("writes no proxy at all", func(t *testing.T) {
		assert.NotContains(t, source, "IFCBUILDINGELEMENTPROXY")
	})
}

// annotationGolden is the recorded annotation artefact, rewritten from got
// under -update.
func annotationGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/annotations.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// asAnnotation is a proxied artefact with its one proxy rewritten as the
// annotation it stood in for: the keyword replaced, and the Tag and the
// PredefinedType the proxy carries as an element — both absent — taken off the
// end, because an annotation is a product and not an element and has neither.
func asAnnotation(t *testing.T, proxied string) string {
	t.Helper()

	lines := strings.Split(proxied, "\n")

	rewritten := 0
	for i, line := range lines {
		at, written, found := strings.Cut(line, "=IFCBUILDINGELEMENTPROXY(")
		if !found {
			continue
		}

		head, tagged := strings.CutSuffix(written, ",$,$);")
		require.True(t, tagged, "the proxy ends with an absent Tag and PredefinedType: %s", line)

		lines[i] = at + "=IFCANNOTATION(" + head + ");"
		rewritten++
	}

	require.Equal(t, 1, rewritten, "the artefact holds one proxy")

	return strings.Join(lines, "\n")
}

// TestRunExportWritesAnAnnotationWithEverythingTheProxyCarried is its own
// function because what it asserts is a relation between two exports rather
// than a line of one.
//
// The proxy an annotation used to be written as and the IfcAnnotation it is
// written as now share the head IfcProduct declares, and differ in the two
// attributes IfcElement and the proxy add after it — which the proxy writes
// absent. So a file differing from the proxied one in anything but the keyword
// and those two is a file which lost or changed something the proxy carried: a
// GlobalId, a name, a placement, a containment, a footprint or a body.
func TestRunExportWritesAnAnnotationWithEverythingTheProxyCarried(t *testing.T) {
	testCases := []struct {
		name  string
		files func(t *testing.T) map[string]string
		args  []string
	}{
		{
			name:  "the reproduction, carried by nothing but its identity and its containment",
			files: func(*testing.T) map[string]string { return annotationModel() },
		},
		{
			name:  "an annotation within nothing, as every one the consumer authors is",
			files: withAnUncontainedAnnotation,
		},
		{
			name:  "a survey tie placed at the point the model puts it",
			files: func(*testing.T) map[string]string { return withALocatedAnnotation() },
			args:  bodyFlags(),
		},
		{
			name: "an annotation drawn from an outline, with a footprint and a body",
			files: func(t *testing.T) map[string]string {
				return reclassified(t, elementModel(), "IfcFurnishingElement", "IfcAnnotation")
			},
			args: bodyFlags(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			files := testCase.files(t)

			classified, _, stderr := exporting(t, exitSuccess, files, testCase.args...)
			require.True(t, classified.Derived, stderr)
			assert.Empty(t, classified.Classifications)

			proxied, _, stderr := exporting(t, exitSuccess,
				unclassified(t, files, `(classification "IFC4" "IfcAnnotation")`), testCase.args...)
			require.True(t, proxied.Derived, stderr)

			annotation, proxy := artefact(t, classified), artefact(t, proxied)
			require.Contains(t, annotation, "IFCANNOTATION(")

			assert.Equal(t, asAnnotation(t, proxy), annotation,
				"the annotation is the proxy it replaced, under the entity it is classified as")
		})
	}
}

// TestRunExportDrawsAnAnnotationAsAnyDrawnElementIsDrawn is its own function
// because it asserts on the shapes rather than on the entity: an annotation
// drawn from an outline carries the footprint and the body a countertop drawn
// from that outline carries, and one placed by a point stands where the panel
// it was reclassified from stood.
func TestRunExportDrawsAnAnnotationAsAnyDrawnElementIsDrawn(t *testing.T) {
	t.Run("gives an annotation drawn from an outline a footprint and a body", func(t *testing.T) {
		counter, _, stderr := exporting(t, exitSuccess, elementModel(), bodyFlags()...)
		require.True(t, counter.Derived, stderr)

		annotation, _, stderr := exporting(t, exitSuccess,
			reclassified(t, elementModel(), "IfcFurnishingElement", "IfcAnnotation"), bodyFlags()...)
		require.True(t, annotation.Derived, stderr)

		drawn := artefact(t, annotation)

		assert.Equal(t, shapesOf(artefact(t, counter)), shapesOf(drawn))
		assert.Equal(t, 3, strings.Count(drawn, "'FootPrint','Curve2D'"))
		assert.Equal(t, 3, strings.Count(drawn, "'Body','SweptSolid'"))

		held := instance(t, drawn, namedIn(t, drawn, "site:K-01"))
		require.Equal(t, "IFCANNOTATION", held.keyword)
		require.Len(t, held.attributes, 7)
		assert.NotEqual(t, "$", held.attributes[6], "Representation")
	})

	t.Run("places an annotation where the model puts it", func(t *testing.T) {
		result, _, stderr := exporting(t, exitSuccess, withALocatedAnnotation(), bodyFlags()...)
		require.True(t, result.Derived, stderr)

		source := artefact(t, result)

		held := instance(t, source, namedIn(t, source, "site:PNL-01"))
		require.Equal(t, "IFCANNOTATION", held.keyword)
		assert.Equal(t, "$", held.attributes[6], "a point has a position and no extent")

		instances := parsed(t, source)
		axis := instances[placedBy(t, source, "site:PNL-01").attributes[1]]
		require.Equal(t, "IFCAXIS2PLACEMENT3D", axis.keyword)
		assert.Equal(t, []string{"(2.5,1.5,1.2)"}, instances[axis.attributes[0]].attributes)
	})
}

// TestRunExportRefusesAnOpeningInAnAnnotation is its own function because it
// is the one thing an annotation cannot do which the proxy it replaced could:
// IFC4 voids an element and nothing else, so a door set in a node classified
// IfcAnnotation has no opening the file can hold. The export is refused rather
// than written with a relationship no reader accepts, or with the door silently
// made a part of the annotation instead.
func TestRunExportRefusesAnOpeningInAnAnnotation(t *testing.T) {
	files := openingEdit(t, "registry.dfc", `(classification "IFC4" "IfcWall")`,
		`(classification "IFC4" "IfcAnnotation")`)

	result, _, stderr := exporting(t, exitCheck, files, openingFlags()...)

	assert.False(t, result.Derived)
	assert.Empty(t, result.Files)
	assert.Contains(t, stderr, "RelatingBuildingElement", "the refusal names the relationship's attribute")
}

// TestRunExportOfAnAnnotationIsAFunctionOfTheModel is the determinism property
// over the reproduction: the same tree exports to the same bytes, keyed by the
// digest of the tree and nothing else, and a second run over it finds the file
// already there.
func TestRunExportOfAnAnnotationIsAFunctionOfTheModel(t *testing.T) {
	root := tree(t, annotationModel())

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
