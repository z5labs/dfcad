// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// exportCRSVocabulary is the pair of predicates a coordinate reference system
// is written under, declared the way a consuming repository would declare them:
// text, and carrying no provenance, because a register's name for a system is
// not a measurement of anything.
const exportCRSVocabulary = `
(predicate crs
  (shape text)
  (claim-bearing #f)
  (description "The projected coordinate reference system the chain is rooted at."))

(predicate crs-definition
  (shape text)
  (claim-bearing #f)
  (description "The register's own definition of that system."))
`

// exportRootFrame is the fixture's root frame exactly as exportRegistry writes
// it, which is what the values below are spliced into.
const exportRootFrame = `(frame frame:building (label "Building local grid") (unit m))`

// exportChildFrame is a second grid in the same unit, measured against the
// root, carrying whatever is written into it.
//
// It is here so that a coordinate reference system can be written somewhere it
// does not belong. The unit is the root's, so the model is one this export
// otherwise accepts and the refusal is about the placement of the value rather
// than about anything else being wrong.
const exportChildFrame = `
(namespace method (description "Measurement methods used on this project."))

(predicate frame-transform
  (shape transform)
  (description "The rigid transform from a frame to its parent."))

(frame frame:fabrication
  (label "Fabrication grid")
  (unit m)
  (parent frame:building)
  (transform site:C-0001)
  %s
  (frame-transform
    (id site:C-0001)
    (value
      (transform
        (translation 0.0 0.0 0.0)
        (rotation 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0)
        (scale 1.0)))
    (source "Setting-out record SO-2026-014, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-02")))
`

// louisianaSouth is a well known text definition in the unit its register
// writes, cut down to the parts this reads.
//
// The nested angular unit is what makes it worth having: a reader taking the
// first unit token it met would take the degree, which is the unit of the
// geographic system this one is projected from rather than of this one.
const louisianaSouth = `PROJCS["NAD83(2011) / Louisiana South (ftUS)",` +
	`GEOGCS["NAD83(2011)",UNIT["degree",0.0174532925199433]],` +
	`UNIT["US survey foot",0.304800609601219]]`

// utmZone31N is the same in the unit the fixture's frame is authored in, which
// is what the export accepts. It carries the nested angular unit for the reason
// the one above does.
const utmZone31N = `PROJCS["ETRS89 / UTM zone 31N",` +
	`GEOGCS["ETRS89",UNIT["degree",0.0174532925199433]],` +
	`UNIT["metre",1]]`

// quoted is a definition as it is written inside a string in the format, which
// escapes the quotes well known text is full of.
func quoted(definition string) string {
	return strings.ReplaceAll(definition, `"`, `\"`)
}

// sited is the export fixture with the coordinate reference system vocabulary
// declared and the forms given written on the root frame.
func sited(onRoot ...string) map[string]string {
	written := exportRootFrame
	if len(onRoot) > 0 {
		written = "(frame frame:building\n  (label \"Building local grid\")\n  (unit m)\n  " +
			strings.Join(onRoot, "\n  ") + ")"
	}

	return map[string]string{
		"registry.dfc": strings.Replace(exportRegistry, exportRootFrame, written, 1) +
			exportCRSVocabulary,
		"entities/site.dfc": exportEntities,
	}
}

func TestRunExportCarriesTheCoordinateReferenceSystem(t *testing.T) {
	result, _, _ := exporting(t, exitSuccess,
		sited(`(crs "EPSG:6543")`, `(crs-definition "`+quoted(utmZone31N)+`")`),
		"--crs", "crs", "--crs-definition", "crs-definition")

	source := artefact(t, result)

	testCases := []struct {
		name     string
		expected string
	}{
		{
			name:     "names the system as the projected coordinate reference system",
			expected: "IFCPROJECTEDCRS('EPSG:6543',",
		},
		{
			name:     "copies the definition beside it byte for byte",
			expected: `'PROJCS["ETRS89 / UTM zone 31N",`,
		},
		{
			name:     "converts into it from the model's own coordinate space",
			expected: "IFCMAPCONVERSION(",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Contains(t, source, testCase.expected)
		})
	}

	t.Run("emits the identity, inventing neither an offset nor a scale", func(t *testing.T) {
		for _, line := range strings.Split(source, "\n") {
			if !strings.Contains(line, "IFCMAPCONVERSION(") {
				continue
			}

			assert.Regexp(t, `IFCMAPCONVERSION\(#\d+,#\d+,0\.,0\.,0\.,\$,\$,\$\);$`, line)

			return
		}

		t.Fatal("the artefact holds a map conversion")
	})
}

// TestRunExportWritesNoGeoreferenceWhereTheRunNamesNoPredicate is its own
// function because the absence is the assertion: a model whose vocabulary
// nobody named exports as the file it was before there was a georeference to
// write, rather than failing over a flag it was not given.
func TestRunExportWritesNoGeoreferenceWhereTheRunNamesNoPredicate(t *testing.T) {
	result, _, _ := exporting(t, exitSuccess, sited(`(crs "EPSG:6543")`))
	source := artefact(t, result)

	assert.NotContains(t, source, "IFCPROJECTEDCRS")
	assert.NotContains(t, source, "IFCMAPCONVERSION")
	assert.NotContains(t, source, "EPSG:6543")
}

// TestRunExportWritesNoGeoreferenceWhereTheModelStatesNone is its own function
// because it is the other half of that: the run named the vocabulary and the
// model happens not to use it, which is a model nobody has sited and not a
// mistake.
func TestRunExportWritesNoGeoreferenceWhereTheModelStatesNone(t *testing.T) {
	result, _, _ := exporting(t, exitSuccess, sited(), "--crs", "crs")

	assert.NotContains(t, artefact(t, result), "IFCPROJECTEDCRS")
}

func TestRunExportRefusesAGeoreferenceItCannotWrite(t *testing.T) {
	testCases := []struct {
		name     string
		files    map[string]string
		args     []string
		expected string
	}{
		{
			name: "a coordinate reference system written on a frame which is not the root",
			files: map[string]string{
				"registry.dfc": exportRegistry + exportCRSVocabulary +
					strings.Replace(exportChildFrame, "%s", `(crs "EPSG:6543")`, 1),
				"entities/site.dfc": exportEntities,
			},
			args:     []string{"--crs", "crs"},
			expected: "root frame",
		},
		{
			name:     "an identifier which is not an authority and a code",
			files:    sited(`(crs "6543")`),
			args:     []string{"--crs", "crs"},
			expected: "an authority and a code",
		},
		{
			name:     "an identifier with nothing after the authority",
			files:    sited(`(crs "EPSG:")`),
			args:     []string{"--crs", "crs"},
			expected: "an authority and a code",
		},
		{
			name:     "two coordinate reference systems on one root frame",
			files:    sited(`(crs "EPSG:6543")`, `(crs "EPSG:26982")`),
			args:     []string{"--crs", "crs"},
			expected: "rooted at one coordinate reference system",
		},
		{
			name: "a definition whose linear unit is not the one the frame declares",
			files: sited(
				`(crs "EPSG:6543")`,
				`(crs-definition "`+quoted(louisianaSouth)+`")`,
			),
			args:     []string{"--crs", "crs", "--crs-definition", "crs-definition"},
			expected: "US survey foot",
		},
		{
			name:     "a definition with no identifier beside it",
			files:    sited(`(crs-definition "PROJCS[]")`),
			args:     []string{"--crs", "crs", "--crs-definition", "crs-definition"},
			expected: "only the definition",
		},
		{
			name:     "an identifier which is not a string",
			files:    sited(`(crs 6543.0)`),
			args:     []string{"--crs", "crs"},
			expected: "found a scalar value",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, _, stderr := exporting(t, exitCheck, testCase.files, testCase.args...)

			assert.False(t, result.Derived)
			assert.Empty(t, result.Files)
			assert.Contains(t, stderr, testCase.expected)
		})
	}
}

// TestRunExportReportsEveryGeoreferenceMistakeAtOnce is its own function
// because what it asserts is a property of the pass rather than of any one
// refusal: an identifier which is not one and a definition in the wrong unit
// are two independent mistakes, and hearing about one of them at a time turns
// fixing a file into a guessing loop.
func TestRunExportReportsEveryGeoreferenceMistakeAtOnce(t *testing.T) {
	_, _, stderr := exporting(t, exitCheck,
		sited(`(crs "6543")`, `(crs-definition "`+quoted(louisianaSouth)+`")`),
		"--crs", "crs", "--crs-definition", "crs-definition")

	assert.Contains(t, stderr, "an authority and a code")
	assert.Contains(t, stderr, "US survey foot")
}

// TestRunExportSaysOnlyWhatIsWrongAboutTwoIdentifiers is its own function
// because it is about a diagnostic which must not appear: a frame carrying two
// identifiers has none this can use, and saying beside that that there was
// "only the definition" would name a mistake nobody made.
func TestRunExportSaysOnlyWhatIsWrongAboutTwoIdentifiers(t *testing.T) {
	_, _, stderr := exporting(t, exitCheck,
		sited(
			`(crs "EPSG:6543")`,
			`(crs "EPSG:26982")`,
			`(crs-definition "`+quoted(utmZone31N)+`")`,
		),
		"--crs", "crs", "--crs-definition", "crs-definition")

	assert.Contains(t, stderr, "rooted at one coordinate reference system")
	assert.NotContains(t, stderr, "only the definition")
}

// TestRunExportRefusesADefinitionWithNothingToDefine is its own function
// because it is a usage error rather than a verdict on a model: it is decided
// from the invocation alone, before anything is read.
func TestRunExportRefusesADefinitionWithNothingToDefine(t *testing.T) {
	root := tree(t, sited())

	stdout, stderr := invoke(t, exitUsage, root, "export", "--crs-definition", "crs-definition")

	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "--crs")
}

// TestMalformedIdentifier drives the shape rule directly, over the spellings a
// command line test would need a tree on disk for each of.
//
// Every case here is about the shape and none is about the meaning. Whether
// EPSG:6543 exists, what it projects and where it applies are questions for a
// register this tool does not carry.
func TestMalformedIdentifier(t *testing.T) {
	testCases := []struct {
		name       string
		identifier string
		malformed  bool
	}{
		{name: "takes an authority and a code", identifier: "EPSG:6543"},
		{name: "takes an authority nothing here has heard of", identifier: "ACME:local-grid-3"},
		{name: "takes a code which is not a number", identifier: "ESRI:NAD_1983_StatePlane"},
		{name: "refuses a code with no authority", identifier: "6543", malformed: true},
		{name: "refuses an empty authority", identifier: ":6543", malformed: true},
		{name: "refuses an empty code", identifier: "EPSG:", malformed: true},
		{name: "refuses nothing at all", identifier: "", malformed: true},
		{name: "refuses two authorities", identifier: "urn:ogc:def:crs:EPSG::6543", malformed: true},
		{name: "refuses white space in it", identifier: "EPSG: 6543", malformed: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			diagnostic := malformedIdentifier(testCase.identifier, dfcad.Span{}, "crs")

			if !testCase.malformed {
				assert.Nil(t, diagnostic)
				return
			}

			require.NotNil(t, diagnostic)
			assert.Equal(t, dfcad.SeverityError, diagnostic.Severity)
			assert.Contains(t, diagnostic.Hint, "<authority>:<code>")
		})
	}
}

// TestDefinitionUnit drives the token rule directly, which is the half of the
// definition check with anything to decide.
//
// Not one case compares a conversion factor, and that is the point: the US
// survey foot is exactly 1200/3937 m and the registers spell that several ways
// which differ in their last digits, so a check on the number would refuse
// definitions which are right.
func TestDefinitionUnit(t *testing.T) {
	testCases := []struct {
		name       string
		definition string
		expected   dfcad.Unit
		stated     bool
	}{
		{
			name:       "reads the linear unit past the angular one it is projected from",
			definition: louisianaSouth,
			expected:   dfcad.UnitSurveyFoot,
			stated:     true,
		},
		{
			name:       "reads the metre",
			definition: `PROJCS["ETRS89 / UTM zone 31N",UNIT["metre",1]]`,
			expected:   dfcad.UnitMetre,
			stated:     true,
		},
		{
			name:       "reads the American spelling of it",
			definition: `PROJCS["x",UNIT["meter",1]]`,
			expected:   dfcad.UnitMetre,
			stated:     true,
		},
		{
			name:       "reads well known text 2's own keyword",
			definition: `PROJCRS["x",LENGTHUNIT["metre",1]]`,
			expected:   dfcad.UnitMetre,
			stated:     true,
		},
		{
			name:       "never reads the survey foot as the foot",
			definition: `PROJCS["x",UNIT["US survey foot",0.30480060960121924]]`,
			expected:   dfcad.UnitSurveyFoot,
			stated:     true,
		},
		{
			name:       "reads the international foot as itself",
			definition: `PROJCS["x",UNIT["foot",0.3048]]`,
			expected:   dfcad.UnitFoot,
			stated:     true,
		},
		{
			name:       "reads a register's punctuation through",
			definition: `PROJCS["x",UNIT["Foot_US",0.30480060960121924]]`,
			expected:   dfcad.UnitSurveyFoot,
			stated:     true,
		},
		{
			name:       "states nothing where there is no unit token",
			definition: `PROJCS["x"]`,
		},
		{
			name:       "states nothing where the only token is angular",
			definition: `GEOGCS["x",UNIT["degree",0.0174532925199433]]`,
		},
		{
			name:       "states nothing for a notation it does not read",
			definition: "+proj=lcc +units=us-ft",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			unit, _, stated := definitionUnit(testCase.definition)

			assert.Equal(t, testCase.stated, stated)
			assert.Equal(t, testCase.expected, unit)
		})
	}
}
