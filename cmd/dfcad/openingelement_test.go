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

// openingElementRegistry and openingElementEntities are the reproduction
// IfcOpeningElement was reported against: one building, and one element
// classified as an opening element within it, which the export used to write
// as a proxy naming its type.
const openingElementRegistry = `(project
  (label "Unwritten classification repro")
  (globalid-namespace "https://example.org/unwritten-repro"))

(namespace frame (description "Coordinate frames."))
(namespace site (description "Semantic nodes."))

(frame frame:site (label "Site grid") (unit usft))

(type building (kind Building) (geometry absent) (description "The dwelling."))
(type openingelement (kind Element) (geometry absent) (description "Classified IfcOpeningElement.")
  (classification "IFC4" "IfcOpeningElement"))
`

const openingElementEntities = `(node site:dwelling (label "Dwelling") (kind Building) (type building))
(node site:openingelement-1 (label "A IfcOpeningElement") (kind Element) (type openingelement)
  (within site:dwelling))
`

// openingElementModel is the reproduction as a fixture tree.
func openingElementModel() map[string]string {
	return map[string]string{
		"registry.dfc": openingElementRegistry,
		"entities.dfc": openingElementEntities,
	}
}

// withAnOutlinedOpeningElement is the element fixture's countertop
// reclassified as an opening element: a node whose declared geometry is an
// area, drawn with a footprint and a body, and within no element, so voiding
// nothing.
func withAnOutlinedOpeningElement(t *testing.T) map[string]string {
	return reclassified(t, elementModel(), "IfcFurnishingElement", "IfcOpeningElement")
}

// casedOpening is the opening fixture with a cased opening in its wall beside
// the door and the window: a doorway with no door, declared to fill an opening
// exactly as they are, which is how the consumer this story came from authors
// one.
func casedOpening(t *testing.T) map[string]string {
	t.Helper()

	files := openingModel()

	files["registry.dfc"] += `(type CasedOpening (kind Element) (geometry line)
  (description "A cased opening with no leaf, drawn as its run between jambs.")
  (classification "IFC4" "IfcOpeningElement")
  (fills-opening #t))
`

	files["geometry.dfc"] += `
(vertex geom:V-O1 (frame frame:building)
  (position (value (1.0 0.0 0.0) usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25")))
(vertex geom:V-O2 (frame frame:building)
  (position (value (3.5 0.0 0.0) usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25")))
(edge geom:E-O (frame frame:building) (vertices geom:V-O1 geom:V-O2))
(loop geom:L-O (frame frame:building) (edges geom:E-O))
`

	files["entities.dfc"] += `
(node site:O-01 (label "Hall opening") (kind Element) (type CasedOpening)
  (geometry line) (frame frame:building) (within site:W-01) (boundary geom:L-O)
  (height (value 6.667 usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25"))
  (thickness (value 0.5 usft) (source "Tape") (method method:tape)
    (accuracy (independent 0.02 usft)) (date "2026-09-25")))
`

	return files
}

func TestRunExportWritesAnOpeningElementAsTheEntityItIsClassifiedAs(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, openingElementModel())
	require.True(t, result.Derived, stderr)

	source := artefact(t, result)

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		assert.Equal(t, exportedGolden(t, "openingelement.ifc", source), source,
			"the exported artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})

	t.Run("writes the reproduction's element as IFCOPENINGELEMENT", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:openingelement-1"))

		assert.Equal(t, "IFCOPENINGELEMENT", held.keyword)
	})

	t.Run("gives it the attribute list IFC4 gives it, which is one past the tag", func(t *testing.T) {
		held := instance(t, source, namedIn(t, source, "site:openingelement-1"))

		require.Len(t, held.attributes, 9)
		assert.Equal(t, "'site:openingelement-1'", held.attributes[2], "Name")
		assert.Equal(t, "'A IfcOpeningElement'", held.attributes[3], "Description")
		assert.Equal(t, "'openingelement'", held.attributes[4], "ObjectType")
		assert.Equal(t, "$", held.attributes[8], "PredefinedType is absent, as export --help says")
	})

	t.Run("contains it in the building it is within", func(t *testing.T) {
		assert.Contains(t, containedIn(t, source), namedIn(t, source, "site:openingelement-1"))
	})

	t.Run("voids nothing, because it is within no element", func(t *testing.T) {
		assert.Empty(t, relatedBy(t, source, "IFCRELVOIDSELEMENT"))
		assert.Empty(t, relatedBy(t, source, "IFCRELFILLSELEMENT"))
	})

	t.Run("reports no classification it could not carry", func(t *testing.T) {
		assert.NotNil(t, result.Classifications)
		assert.Empty(t, result.Classifications)
	})

	t.Run("writes no warning about it", func(t *testing.T) {
		assert.NotContains(t, stderr, "IfcOpeningElement")
		assert.NotContains(t, stderr, "IfcBuildingElementProxy")
	})

	t.Run("writes no proxy at all", func(t *testing.T) {
		assert.NotContains(t, source, "IFCBUILDINGELEMENTPROXY")
	})
}

// exportedGolden is a recorded export artefact under testdata/export,
// rewritten from got under -update.
func exportedGolden(t *testing.T, name, got string) string {
	t.Helper()

	path := filepath.Join("testdata", "export", name)

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestRunExportWritesAnOpeningElementWithEverythingTheProxyCarried is its own
// function because what it asserts is a relation between two exports rather
// than a line of one.
//
// The proxy an opening element within no element used to be written as and
// the IfcOpeningElement it is written as now have the same attribute list — the
// head IfcElement declares and one optional attribute after it — so a file
// differing from the proxied one in anything but the keyword is a file which
// lost or changed something the proxy carried: a GlobalId, a name, a
// placement, a containment, a footprint or a body.
func TestRunExportWritesAnOpeningElementWithEverythingTheProxyCarried(t *testing.T) {
	testCases := []struct {
		name  string
		files func(t *testing.T) map[string]string
		args  []string
	}{
		{
			name:  "the reproduction, carried by nothing but its identity and its containment",
			files: func(*testing.T) map[string]string { return openingElementModel() },
		},
		{
			name:  "an opening drawn from an outline, with a footprint and a body",
			files: withAnOutlinedOpeningElement,
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
				unclassified(t, files, `(classification "IFC4" "IfcOpeningElement")`), testCase.args...)
			require.True(t, proxied.Derived, stderr)

			opening, proxy := artefact(t, classified), artefact(t, proxied)
			require.Contains(t, proxy, "IFCBUILDINGELEMENTPROXY(", "the element was a proxy without its classification")
			require.Contains(t, opening, "IFCOPENINGELEMENT(")

			assert.Equal(t,
				strings.Replace(proxy, "IFCBUILDINGELEMENTPROXY(", "IFCOPENINGELEMENT(", 1), opening,
				"the opening element is the proxy it replaced, under the entity it is classified as")
		})
	}
}

// TestRunExportDrawsAnOpeningElementAsAnyDrawnElementIsDrawn is its own
// function because it asserts on the shapes rather than on the entity: an
// opening drawn from an outline carries the footprint and the body a
// countertop drawn from that outline carries.
func TestRunExportDrawsAnOpeningElementAsAnyDrawnElementIsDrawn(t *testing.T) {
	counter, _, stderr := exporting(t, exitSuccess, elementModel(), bodyFlags()...)
	require.True(t, counter.Derived, stderr)

	opening, _, stderr := exporting(t, exitSuccess, withAnOutlinedOpeningElement(t), bodyFlags()...)
	require.True(t, opening.Derived, stderr)

	drawn := artefact(t, opening)

	assert.Equal(t, shapesOf(artefact(t, counter)), shapesOf(drawn))
	assert.Equal(t, 3, strings.Count(drawn, "'FootPrint','Curve2D'"))
	assert.Equal(t, 3, strings.Count(drawn, "'Body','SweptSolid'"))

	held := instance(t, drawn, namedIn(t, drawn, "site:K-01"))
	require.Equal(t, "IFCOPENINGELEMENT", held.keyword)
	assert.NotEqual(t, "$", held.attributes[6], "Representation")
	assert.Equal(t, "$", held.attributes[8], "PredefinedType")
}

// TestRunExportOfAnOpeningElementIsAFunctionOfTheModel is the determinism
// property over the reproduction: the same tree exports to the same bytes,
// keyed by the digest of the tree and nothing else, and a second run over it
// finds the file already there.
func TestRunExportOfAnOpeningElementIsAFunctionOfTheModel(t *testing.T) {
	root := tree(t, openingElementModel())

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

func TestRunExportWritesACasedOpeningAsTheOpeningInItsWall(t *testing.T) {
	source, stderr := exportOpenings(t, casedOpening(t), openingFlags()...)

	wall := namedIn(t, source, "site:W-01")
	opening := namedIn(t, source, "site:O-01")

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		assert.Equal(t, exportedGolden(t, "cased.ifc", source), source,
			"the exported artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})

	t.Run("writes the cased opening as IFCOPENINGELEMENT with its own attributes", func(t *testing.T) {
		held := instance(t, source, opening)

		require.Equal(t, "IFCOPENINGELEMENT", held.keyword)
		require.Len(t, held.attributes, 9)
		assert.Equal(t, "'Hall opening'", held.attributes[3], "Description")
		assert.Equal(t, "'CasedOpening'", held.attributes[4], "ObjectType")
		assert.Equal(t, "$", held.attributes[8], "PredefinedType is absent, as export --help says")
	})

	t.Run("voids the wall it is within", func(t *testing.T) {
		assert.Contains(t, relatedBy(t, source, "IFCRELVOIDSELEMENT"), [2]string{wall, opening})
	})

	t.Run("is filled by nothing and fills nothing", func(t *testing.T) {
		for _, fills := range relatedBy(t, source, "IFCRELFILLSELEMENT") {
			assert.NotEqual(t, opening, fills[0])
			assert.NotEqual(t, opening, fills[1])
		}
	})

	t.Run("cuts no second opening for it to stand in", func(t *testing.T) {
		assert.Equal(t, 3, strings.Count(source, "IFCOPENINGELEMENT("),
			"one for the door, one for the window, and the cased opening itself")
		assert.Len(t, relatedBy(t, source, "IFCRELVOIDSELEMENT"), 3)
		assert.Len(t, relatedBy(t, source, "IFCRELFILLSELEMENT"), 2)
	})

	t.Run("keeps it contained in its storey", func(t *testing.T) {
		assert.Contains(t, containedIn(t, source), opening)
	})

	t.Run("still cuts the door and the window their openings", func(t *testing.T) {
		for _, filler := range []string{"site:D-01", "site:N-01"} {
			_, host := voidOf(t, source, filler)
			assert.Equal(t, wall, host)
		}
	})

	t.Run("writes no warning about it", func(t *testing.T) {
		assert.NotContains(t, stderr, "IfcOpeningElement")
		assert.NotContains(t, stderr, "site:O-01")
	})
}

// TestRunExportDrawsACasedOpeningAsTheProxyItReplacedWasDrawn is its own
// function because it compares two exports rather than reading one: the cased
// opening carries the footprint and the body its proxy carried, and the file
// differs from the proxied one in how the two are related to the wall and in
// nothing it says about the opening's own shape.
func TestRunExportDrawsACasedOpeningAsTheProxyItReplacedWasDrawn(t *testing.T) {
	cased, _ := exportOpenings(t, casedOpening(t), openingFlags()...)
	proxied, _ := exportOpenings(t,
		unclassified(t, casedOpening(t), `(classification "IFC4" "IfcOpeningElement")`), openingFlags()...)

	opening := instance(t, cased, namedIn(t, cased, "site:O-01"))
	proxy := instance(t, proxied, namedIn(t, proxied, "site:O-01"))

	require.Equal(t, "IFCBUILDINGELEMENTPROXY", proxy.keyword)
	require.Equal(t, "IFCOPENINGELEMENT", opening.keyword)

	assert.Equal(t, proxy.attributes[0], opening.attributes[0], "GlobalId")
	assert.Equal(t, proxy.attributes[2:5], opening.attributes[2:5], "Name, Description and ObjectType")
	assert.NotEqual(t, "$", opening.attributes[6], "Representation")

	assert.Equal(t,
		sweptBox(t, proxied, namedIn(t, proxied, "site:O-01")),
		sweptBox(t, cased, namedIn(t, cased, "site:O-01")),
		"the body is the one the proxy carried")
	assert.Contains(t, cased, "'FootPrint','Curve2D'")

	_, proxyHost := voidOf(t, proxied, "site:O-01")
	assert.Equal(t, namedIn(t, proxied, "site:W-01"), proxyHost,
		"the proxy stood in an opening cut for it, which is what the cased opening now is")
}

func TestRunExportRefusesAFillingSetInACasedOpening(t *testing.T) {
	files := casedOpening(t)
	files["entities.dfc"] = strings.Replace(files["entities.dfc"],
		"(type Door)\n  (geometry line) (frame frame:building) (within site:W-01)",
		"(type Door)\n  (geometry line) (frame frame:building) (within site:O-01)", 1)
	require.Contains(t, files["entities.dfc"], "(within site:O-01)")

	result, _, stderr := exporting(t, exitCheck, files, openingFlags()...)

	assert.False(t, result.Derived)
	assert.Empty(t, result.Files)
	assert.Contains(t, stderr, "site:D-01")
	assert.Contains(t, stderr, "site:O-01")
}

// TestRunExportOfACasedOpeningIsAFunctionOfTheModel is the determinism
// property over the cased opening, whose relationship to its wall is written
// after the walk.
func TestRunExportOfACasedOpeningIsAFunctionOfTheModel(t *testing.T) {
	first, _ := exportOpenings(t, casedOpening(t), openingFlags()...)

	for range 4 {
		again, _ := exportOpenings(t, casedOpening(t), openingFlags()...)
		assert.Equal(t, first, again)
	}
}

// TestRunExportGivesACasedOpeningsVoidAnIdentifierOfItsOwn is its own function
// because it is about the manifest rather than the file: the relationship
// voiding the wall is derived from the opening's id under a name no node id can
// carry, and accounted for with the rest.
func TestRunExportGivesACasedOpeningsVoidAnIdentifierOfItsOwn(t *testing.T) {
	result, _, stderr := exporting(t, exitSuccess, casedOpening(t), append(openingFlags(), "--evidence")...)
	require.True(t, result.Derived, stderr)

	var names []string
	for _, identifier := range result.Identifiers {
		names = append(names, identifier.ID)
	}

	assert.Contains(t, names, "ifc/voids/site:O-01")
	assert.NotContains(t, names, "ifc/opening/site:O-01", "no second opening is cut for it")
	assert.NotContains(t, names, "ifc/fills/site:O-01", "nothing fills it")
}

// TestExportUsageStatesWhatPredefinedTypeAnOpeningElementIsWrittenWith is its
// own function because it is about the documentation rather than about a run:
// the value written for IfcOpeningElement's PredefinedType is a decision, and
// a decision nobody can read is one a receiving system has to infer.
func TestExportUsageStatesWhatPredefinedTypeAnOpeningElementIsWrittenWith(t *testing.T) {
	assert.Contains(t, exportUsage, " IfcOpeningElement\n")
	assert.Contains(t, exportUsage, "An IfcOpeningElement is\nwritten the same way.")
	assert.Contains(t, exportUsage, "for an opening whether it goes right through what it is in or is a recess")
	assert.Contains(t, exportUsage, `"opening"`)
	assert.Contains(t, exportUsage, `absent, "$", and never
as .NOTDEFINED.`)
	assert.Contains(t, exportUsage, "each is written .OPENING., because each is cut through")
}

// TestExportUsageStatesWhatACasedOpeningIsWrittenAs is its own function for the
// reason the one above is: a filling classified IfcOpeningElement is written
// differently from every other filling, and that is a decision a reader of the
// file has to be able to look up.
func TestExportUsageStatesWhatACasedOpeningIsWrittenAs(t *testing.T) {
	assert.Contains(t, exportUsage, "A filling whose type is classified IfcOpeningElement")
	assert.Contains(t, exportUsage, "is within by an IfcRelVoidsElement; nothing is cut for it to stand in and\nnothing fills it")
}

// TestExportHintOffersAnOpeningElement is its own function because it is
// about the refusal's hint rather than a run: a registry author told which
// entities a classification may name is told IfcOpeningElement is one of
// them.
func TestExportHintOffersAnOpeningElement(t *testing.T) {
	assert.Contains(t, writableEntities(), "IFCOPENINGELEMENT")
}
