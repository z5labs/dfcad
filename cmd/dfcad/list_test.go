// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// listRegistry is the vocabulary the model below is judged against.
//
// Fitting declares its kinds and its geometry forms out of specification order
// and permits absence as well, which is what says whether a listing reports the
// axes the registry permits or the order somebody happened to type them in.
const listRegistry = `(project
  (label "Listing fixture")
  (globalid-namespace "https://example.org/models/list"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))

(frame frame:site-grid (label "Site survey grid") (unit m))

(frame frame:building
  (label "Building local grid")
  (unit m)
  (parent frame:site-grid)
  (transform site:C-0001)
  (frame-transform
    (id site:C-0001)
    (value
      (transform
        (translation 100.0 200.0 0.0)
        (rotation 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0)
        (scale 1.0)))
    (source "Setting-out record SO-2026-014, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-02")))

(type Campus
  (kind Zone)
  (geometry absent)
  (description "A group of things administered together, which has no shape."))

(type Fitting
  (kind Interface)
  (kind Element)
  (geometry surface)
  (geometry line)
  (geometry absent)
  (description "Something fitted between two spaces."))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings.")
  (classification "IFC4" "IfcSpace")
  (classification "Uniclass2015" "SL_25_10_50"))

(type Parcel
  (kind Site)
  (geometry area)
  (description "A plot of land with a boundary and a planning regime over it.")
  (classification "IFC4" "IfcSite"))

(type OfficeBuilding
  (kind Building)
  (geometry solid)
  (description "A building let as offices."))

(predicate area
  (unit m2)
  (shape scalar)
  (description "How much floor a space has."))

(predicate frame-transform
  (shape transform)
  (description "The rigid transform from a frame to its parent."))

(predicate position
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "The location of a vertex in its frame."))

(predicate setback
  (unit m)
  (shape scalar)
  (description "How far back from the boundary edge it is written on a structure has to sit."))

(tolerance coincident
  (value 0.005 m)
  (description "How far apart two corners may be and still be one point."))

(tolerance chord-deviation
  (value 0.01 m)
  (description "How far a straight segment standing in for a curve may fall from it."))

(route buildings
  (kind Building)
  (type OfficeBuilding)
  (file "entities/buildings.dfc"))

(route campuses (kind Zone) (type Campus) (file "entities/campuses.dfc"))

(route geometry
  (namespace geom)
  (file "entities/geometry.dfc")
  (description "Vertices, edges and loops, which declare neither a kind nor a type."))

(route parcels (kind Site) (type Parcel) (file "entities/parcels.dfc"))

(route rooms
  (kind Space)
  (type MeetingRoom)
  (file "entities/site.dfc")
  (description "Meeting rooms, beside the building they are in."))
`

// listModel is written with its nodes out of id order, so that a listing which
// reported the walk order rather than the id order would say so.
//
// Room A carries a claim which writes no id of its own, which is the ordinary
// case — an id is required only of a claim something references — so that the
// walks over every command reach a question `dfcad resolve` can answer and a
// correction which has to mint an id has something to correct.
//
// Room C carries two, both named, which disagree. That is a state a model is
// routinely in and it is the one a retraction is issued against: naming a claim
// on a command line means naming it by the id it wrote, so the fixture has to
// hold claims which wrote one.
const listModel = `(node site:S-102
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building))

(node site:B-01
  (label "Block A")
  (kind Building)
  (type OfficeBuilding)
  (geometry solid)
  (frame frame:site-grid))

(node site:S-101
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (area
    (value 24.2 m2)
    (source "As-built check AB-2026-009, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-05-06")))

(node site:S-103
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:site-grid)
  (boundary geom:L-21)
  (area
    (id site:M-0001)
    (value 31.0 m2)
    (source "Design drawing DR-2026-004, Acme Architects")
    (method method:estimate)
    (accuracy (independent 0.5 m2))
    (date "2026-01-12"))
  (area
    (id site:M-0002)
    (value 31.4 m2)
    (source "As-built check AB-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-05-06")))

(node site:C-01
  (label "West campus")
  (kind Zone)
  (type Campus))
`

// listGeometry is the geometry the fixture holds: three corners joined into a
// ring, and a fourth corner nothing yet reaches.
//
// It is here rather than only in the tests which write geometry because the
// walks over every command reach the commands which do: an edge is written
// between two vertices which already exist, a loop is written over edges which
// already exist, and a scaffold has something to snap onto.
const listGeometry = `(vertex geom:V-01
  (label "Room B, north-west corner")
  (frame frame:building)
  (position
    (value (0.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(vertex geom:V-02
  (label "Room B, north-east corner")
  (frame frame:building)
  (position
    (value (4.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(vertex geom:V-03
  (label "Room B, south-east corner")
  (frame frame:building)
  (position
    (value (4.0 3.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(vertex geom:V-04
  (label "Room B, south-west corner")
  (frame frame:building)
  (position
    (value (0.0 3.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(edge geom:E-01 (label "Room B, north wall") (frame frame:building) (vertices geom:V-01 geom:V-02))

(edge geom:E-02 (label "Room B, east wall") (frame frame:building) (vertices geom:V-02 geom:V-03))

(edge geom:E-03 (label "Room B, south wall") (frame frame:building) (vertices geom:V-03 geom:V-04))

; The plot the buildable region is derived over. It says where its corners are
; and how far back a building sits from each of its edges, and nowhere does it say
; what may be built on it: that is read out of these every time it is asked for.
(vertex geom:V-11
  (label "Plot one, south-west corner")
  (frame frame:building)
  (position
    (value (20.0 0.0 0.0) m)
    (source "Boundary survey BS-2026-004, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-11")))

(vertex geom:V-12
  (label "Plot one, south-east corner")
  (frame frame:building)
  (position
    (value (40.0 0.0 0.0) m)
    (source "Boundary survey BS-2026-004, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-11")))

(vertex geom:V-13
  (label "Plot one, north-east corner")
  (frame frame:building)
  (position
    (value (40.0 12.0 0.0) m)
    (source "Boundary survey BS-2026-004, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-11")))

(vertex geom:V-14
  (label "Plot one, north-west corner")
  (frame frame:building)
  (position
    (value (20.0 12.0 0.0) m)
    (source "Boundary survey BS-2026-004, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-11")))

(edge geom:E-11 (label "Plot one, road frontage") (frame frame:building) (vertices geom:V-11 geom:V-12)
  (setback
    (value 5.0 m)
    (source "Planning consent PC-2026-014, condition 1")
    (method method:statutory-instrument)
    (accuracy (independent 0.01 m))
    (date "2026-04-02")))

(edge geom:E-12 (label "Plot one, east flank") (frame frame:building) (vertices geom:V-12 geom:V-13)
  (setback
    (value 2.0 m)
    (source "Planning consent PC-2026-014, condition 2")
    (method method:statutory-instrument)
    (accuracy (independent 0.01 m))
    (date "2026-04-02")))

(edge geom:E-13 (label "Plot one, rear") (frame frame:building) (vertices geom:V-13 geom:V-14)
  (setback
    (value 3.0 m)
    (source "Planning consent PC-2026-014, condition 3")
    (method method:statutory-instrument)
    (accuracy (independent 0.01 m))
    (date "2026-04-02")))

(edge geom:E-14 (label "Plot one, west flank") (frame frame:building) (vertices geom:V-14 geom:V-11)
  (setback
    (value 2.0 m)
    (source "Planning consent PC-2026-014, condition 4")
    (method method:statutory-instrument)
    (accuracy (independent 0.01 m))
    (date "2026-04-02")))

(loop geom:L-11
  (label "Plot one boundary")
  (frame frame:building)
  (edges geom:E-11 geom:E-12 geom:E-13 geom:E-14))

; Room C, outlined on the site grid rather than on the building's. It is what
; makes a fit a cross-frame question: the plot above is drawn on one grid and
; this is drawn on another, so deciding whether one sits inside the other means
; reading the claim which measures the two against each other and carrying its
; accuracy into the answer.
(vertex geom:V-21
  (label "Room C, south-west corner")
  (frame frame:site-grid)
  (position
    (value (125.0 203.0 0.0) m)
    (source "Interior control set IC-02, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(vertex geom:V-22
  (label "Room C, south-east corner")
  (frame frame:site-grid)
  (position
    (value (133.0 203.0 0.0) m)
    (source "Interior control set IC-02, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(vertex geom:V-23
  (label "Room C, north-east corner")
  (frame frame:site-grid)
  (position
    (value (133.0 209.0 0.0) m)
    (source "Interior control set IC-02, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(vertex geom:V-24
  (label "Room C, north-west corner")
  (frame frame:site-grid)
  (position
    (value (125.0 209.0 0.0) m)
    (source "Interior control set IC-02, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(edge geom:E-21 (label "Room C, south wall") (frame frame:site-grid) (vertices geom:V-21 geom:V-22))
(edge geom:E-22 (label "Room C, east wall") (frame frame:site-grid) (vertices geom:V-22 geom:V-23))
(edge geom:E-23 (label "Room C, north wall") (frame frame:site-grid) (vertices geom:V-23 geom:V-24))
(edge geom:E-24 (label "Room C, west wall") (frame frame:site-grid) (vertices geom:V-24 geom:V-21))

(loop geom:L-21
  (label "Room C outline")
  (frame frame:site-grid)
  (edges geom:E-21 geom:E-22 geom:E-23 geom:E-24))
`

// listParcels is the plot the buildable derivation is run over.
//
// It is a file of its own because the registry routes a Site to one: a node
// written outside the file its rule names is a model somebody has to reconcile
// by hand later.
const listParcels = `(node site:P-01
  (label "Plot one")
  (kind Site)
  (type Parcel)
  (geometry area)
  (frame frame:building)
  (boundary geom:L-11))
`

// listBatch is the operation file every walk over the commands applies.
//
// It is two operations rather than one because the second names what the first
// wrote, which is the property a batch exists for: a node and the claim about it
// are one statement, and applying them as two commands would mean two loads and
// a window in which the node has no area.
//
// It sits in the model root and the walk never reads it: the walk reads entity
// files, and an operation file is an input to a command rather than part of the
// model.
const listBatch = `{
  "version": 1,
  "operations": [
    {
      "op": "add-node",
      "id": "site:S-104",
      "kind": "Space",
      "type": "MeetingRoom",
      "geometry": "area",
      "frame": "frame:building",
      "label": "Meeting Room D"
    },
    {
      "op": "add-claim",
      "subject": "site:S-104",
      "predicate": "area",
      "claim": {
        "value": "18.0",
        "unit": "m2",
        "source": "As-built check AB-2026-012, Acme Surveys",
        "method": "method:total-station",
        "accuracy": ["independent 0.05 m2"],
        "date": "2026-05-06"
      }
    }
  ]
}
`

// model is the fixture tree both commands are run against.
func model() map[string]string {
	return map[string]string{
		"registry.dfc":          listRegistry,
		"entities/site.dfc":     listModel,
		"entities/geometry.dfc": listGeometry,
		"entities/parcels.dfc":  listParcels,
		"batch.json":            listBatch,
	}
}

// listed decodes the one JSON object on stdout into T.
//
// It requires that stdout holds exactly one JSON value and nothing after it,
// because that is the contract as a caller experiences it rather than a detail
// of this test.
func listed[T any](t *testing.T, stdout string) T {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(stdout))

	var result T
	require.NoError(t, decoder.Decode(&result))

	_, err := decoder.Token()
	require.ErrorIs(t, err, io.EOF, "stdout holds more than one JSON value")

	return result
}

// names is each listed type as "name kinds geometries instances", which is
// every axis the discovery path promises in one readable line.
func names(result listTypesResult) []string {
	out := make([]string, 0, len(result.Types))
	for _, declared := range result.Types {
		geometries := declared.Geometries
		if declared.Absent {
			geometries = append(slices.Clone(geometries), "absent")
		}
		out = append(out, strings.Join(declared.Kinds, "+")+" "+strings.Join(geometries, "+")+" "+
			declared.Name+" "+plural(declared.Instances, "instance"))
	}
	return out
}

// ids is the id of each listed instance.
func ids(result listInstancesResult) []string {
	out := make([]string, 0, len(result.Instances))
	for _, instance := range result.Instances {
		out = append(out, instance.ID)
	}
	return out
}

func TestRunListTypes(t *testing.T) {
	testCases := []struct {
		name          string
		files         map[string]string
		expectedTypes []string
	}{
		{
			name:  "reports every declared type with its axes and its instance count",
			files: model(),
			expectedTypes: []string{
				"Zone absent Campus 1 instance",
				"Element+Interface line+surface+absent Fitting 0 instances",
				"Space area MeetingRoom 3 instances",
				"Building solid OfficeBuilding 1 instance",
				"Site area Parcel 1 instance",
			},
		},
		{
			name:          "reports an empty model as no types at all",
			files:         map[string]string{"notes.md": "nothing to see"},
			expectedTypes: []string{},
		},
		{
			name:          "reports a registry which declares no type as no types at all",
			files:         map[string]string{"registry.dfc": "(project (globalid-namespace \"https://example.org/e\"))\n"},
			expectedTypes: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, testCase.files))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitSuccess, run([]string{"list-types"}, &stdout, &stderr), stderr.String())

			result := listed[listTypesResult](t, stdout.String())
			assert.Equal(t, outputVersion, result.Version)
			assert.Equal(t, "list-types", result.Command)
			assert.Equal(t, testCase.expectedTypes, names(result))
		})
	}
}

// TestRunListTypesDescribesOnlyWhenAsked is its own function because it is
// about what the first call of every cold start does not carry.
//
// The descriptions are prose about the vocabulary rather than about this model,
// and they grow with the registry rather than with the model, so whoever is
// only deciding which type to ask about next was paying for them on every run.
// See docs/decisions/0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md.
func TestRunListTypesDescribesOnlyWhenAsked(t *testing.T) {
	t.Run("leaves the registry's prose out", func(t *testing.T) {
		t.Chdir(tree(t, model()))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run([]string{"list-types"}, &stdout, &stderr), stderr.String())

		result := listed[listTypesResult](t, stdout.String())
		for _, declared := range result.Types {
			assert.Empty(t, declared.Description, declared.Name)
		}

		// The axes are what the call is for, and they are all still there.
		assert.NotContains(t, stdout.String(), "An enclosed room used for meetings")
		assert.Contains(t, stdout.String(), "MeetingRoom")
	})

	t.Run("reports it when it is asked for", func(t *testing.T) {
		t.Chdir(tree(t, model()))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run([]string{"list-types", "--describe"}, &stdout, &stderr), stderr.String())

		result := listed[listTypesResult](t, stdout.String())

		described := make(map[string]string, len(result.Types))
		for _, declared := range result.Types {
			described[declared.Name] = declared.Description
		}

		assert.Equal(t, "An enclosed room used for meetings.", described["MeetingRoom"])
		assert.Equal(t, "A building let as offices.", described["OfficeBuilding"])
	})
}

// TestRunListTypesClassifiesOnlyWhenAsked is the same shape of gate as the one
// above and for a different reader: the caller which needs a mapping into a
// foreign schema is one exporting rather than one exploring, and it asks once.
//
// The measurement is what settles it rather than an argument: the classifications
// of the budget model's registry cost more than the whole of the cold discovery
// path's remaining headroom against its target. See docs/token-budget.md.
func TestRunListTypesClassifiesOnlyWhenAsked(t *testing.T) {
	t.Run("leaves the foreign vocabulary out", func(t *testing.T) {
		t.Chdir(tree(t, model()))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run([]string{"list-types"}, &stdout, &stderr), stderr.String())

		result := listed[listTypesResult](t, stdout.String())
		for _, declared := range result.Types {
			assert.Empty(t, declared.Classifications, declared.Name)
		}

		assert.NotContains(t, stdout.String(), "IfcSpace")
		assert.Contains(t, stdout.String(), "MeetingRoom")
	})

	t.Run("reports every scheme a type names when it is asked for", func(t *testing.T) {
		t.Chdir(tree(t, model()))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run([]string{"list-types", "--classification"}, &stdout, &stderr), stderr.String())

		result := listed[listTypesResult](t, stdout.String())

		classified := make(map[string][]listedClassification, len(result.Types))
		for _, declared := range result.Types {
			classified[declared.Name] = declared.Classifications
		}

		assert.Equal(t, []listedClassification{
			{System: "IFC4", Code: "IfcSpace"},
			{System: "Uniclass2015", Code: "SL_25_10_50"},
		}, classified["MeetingRoom"])

		assert.Equal(t, []listedClassification{
			{System: "IFC4", Code: "IfcSite"},
		}, classified["Parcel"])

		// A type nothing maps into a foreign scheme writes no empty list for a
		// caller to step over. It is the ordinary case, and it is what the
		// engine carrying no vocabulary of its own looks like from here.
		assert.Nil(t, classified["Campus"])
		assert.Equal(t, 2, strings.Count(stdout.String(), `"classifications"`),
			"the field is written on the two types which carry one and on no other")
	})
}

// TestRunListTypesWritesAbsentOnlyWhereItHolds is its own function because it
// is about a field which is written on a minority of entries: a false on every
// type which requires a geometry is a word per type saying what the type before
// it also said.
func TestRunListTypesWritesAbsentOnlyWhereItHolds(t *testing.T) {
	t.Chdir(tree(t, model()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"list-types"}, &stdout, &stderr), stderr.String())

	result := listed[listTypesResult](t, stdout.String())

	permits := make(map[string]bool, len(result.Types))
	for _, declared := range result.Types {
		permits[declared.Name] = declared.Absent
	}

	// A zone administered together has no shape, and a fitting may or may not;
	// a meeting room must have one.
	assert.True(t, permits["Campus"])
	assert.True(t, permits["Fitting"])
	assert.False(t, permits["MeetingRoom"])

	assert.Equal(t, 2, strings.Count(stdout.String(), `"absent"`),
		"absent is written on the two types which permit it and on no other")
}

// TestRunListTypesOrdersByName is its own function because it asserts about the
// order of the whole list rather than about what one entry says.
func TestRunListTypesOrdersByName(t *testing.T) {
	t.Chdir(tree(t, model()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"list-types"}, &stdout, &stderr), stderr.String())

	result := listed[listTypesResult](t, stdout.String())

	ordered := make([]string, 0, len(result.Types))
	for _, declared := range result.Types {
		ordered = append(ordered, declared.Name)
	}

	assert.Equal(t, []string{"Campus", "Fitting", "MeetingRoom", "OfficeBuilding", "Parcel"}, ordered)
	assert.True(t, sortedStrings(ordered))
}

// sortedStrings reports whether items are in ascending order.
func sortedStrings(items []string) bool {
	for i := 1; i < len(items); i++ {
		if items[i-1] > items[i] {
			return false
		}
	}
	return true
}

func TestRunListInstances(t *testing.T) {
	testCases := []struct {
		name        string
		args        []string
		expectedIDs []string
	}{
		{
			name:        "reports every instance when no type is named",
			args:        []string{"list-instances"},
			expectedIDs: []string{"site:B-01", "site:C-01", "site:P-01", "site:S-101", "site:S-102", "site:S-103"},
		},
		{
			name:        "reports the instances of one type",
			args:        []string{"list-instances", "MeetingRoom"},
			expectedIDs: []string{"site:S-101", "site:S-102", "site:S-103"},
		},
		{
			name:        "reports a declared type nothing instantiates as no instances",
			args:        []string{"list-instances", "Fitting"},
			expectedIDs: []string{},
		},
		{
			name:        "filters by kind",
			args:        []string{"list-instances", "--kind", "Space"},
			expectedIDs: []string{"site:S-101", "site:S-102", "site:S-103"},
		},
		{
			name:        "filters by frame",
			args:        []string{"list-instances", "--frame", "frame:building"},
			expectedIDs: []string{"site:P-01", "site:S-101", "site:S-102"},
		},
		{
			name:        "combines a type with a frame",
			args:        []string{"list-instances", "MeetingRoom", "--frame", "frame:site-grid"},
			expectedIDs: []string{"site:S-103"},
		},
		{
			name:        "combines a kind with a frame",
			args:        []string{"list-instances", "--kind", "Building", "--frame", "frame:site-grid"},
			expectedIDs: []string{"site:B-01"},
		},
		{
			name:        "reports nothing when the filters agree on nothing",
			args:        []string{"list-instances", "Campus", "--kind", "Space"},
			expectedIDs: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, model()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitSuccess, run(testCase.args, &stdout, &stderr), stderr.String())

			result := listed[listInstancesResult](t, stdout.String())
			assert.Equal(t, outputVersion, result.Version)
			assert.Equal(t, "list-instances", result.Command)
			assert.Equal(t, testCase.expectedIDs, ids(result))
		})
	}
}

// TestRunListInstancesReportsIDAndLabel is its own function because it asserts
// about the whole of one entry rather than about which entries came back.
func TestRunListInstancesReportsIDAndLabel(t *testing.T) {
	t.Chdir(tree(t, model()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run([]string{"list-instances", "MeetingRoom"}, &stdout, &stderr), stderr.String())

	result := listed[listInstancesResult](t, stdout.String())

	assert.Equal(t, []listedInstance{
		{ID: "site:S-101", Label: "Meeting Room A", Type: "MeetingRoom", Kind: "Space", Frame: "frame:building"},
		{ID: "site:S-102", Label: "Meeting Room B", Type: "MeetingRoom", Kind: "Space", Frame: "frame:building"},

		// A node which was never labelled reports no label rather than an
		// invented one, and the field is absent from the object entirely.
		{ID: "site:S-103", Type: "MeetingRoom", Kind: "Space", Frame: "frame:site-grid"},
	}, result.Instances)

	assert.NotContains(t, stdout.String(), `"label":""`)
}

// TestRunListInstancesOnAnEmptyModel is its own function because an empty model
// declares no type, so every filter that could be given to it names something
// undeclared and the only invocation left is the bare one.
func TestRunListInstancesOnAnEmptyModel(t *testing.T) {
	t.Chdir(tree(t, map[string]string{"notes.md": "nothing to see"}))

	var stdout, stderr bytes.Buffer

	require.Equal(t, exitSuccess, run([]string{"list-instances"}, &stdout, &stderr), stderr.String())

	result := listed[listInstancesResult](t, stdout.String())
	assert.Equal(t, "list-instances", result.Command)
	assert.Equal(t, []listedInstance{}, result.Instances)

	// An empty collection is a list rather than a null, so a caller indexing it
	// needs no special case for the model nobody has written yet.
	assert.Contains(t, stdout.String(), `"instances":[]`)
}

// TestRunListRejectsWhatTheModelDoesNotDeclare walks the three names a caller
// can get wrong.
//
// Each is a usage error rather than an empty list: a name nobody declared and a
// name nothing instantiates are different answers, and stdout stays empty
// because the run produced no result.
func TestRunListRejectsWhatTheModelDoesNotDeclare(t *testing.T) {
	declaredTypes := []string{"Campus", "Fitting", "MeetingRoom", "OfficeBuilding", "Parcel"}
	declaredFrames := []string{"frame:building", "frame:site-grid"}

	testCases := []struct {
		name           string
		args           []string
		expectedStderr string
	}{
		{
			name: "names an undeclared type and points at list-types",
			args: []string{"list-instances", "MeetingRoomm"},
			expectedStderr: "dfcad list-instances: " +
				UnknownTypeError{Type: "MeetingRoomm", Declared: declaredTypes}.Error() + "\n",
		},
		{
			name: "names a kind which is not one of the seven",
			args: []string{"list-instances", "--kind", "Room"},
			expectedStderr: "dfcad list-instances: " +
				UnknownKindError{Kind: "Room", Known: dfcad.Kinds()}.Error() + "\n",
		},
		{
			name: "names an undeclared frame",
			args: []string{"list-instances", "--frame", "frame:annex"},
			expectedStderr: "dfcad list-instances: " +
				UnknownFrameError{Frame: "frame:annex", Declared: declaredFrames}.Error() + "\n",
		},
		{
			name: "rejects a second type argument",
			args: []string{"list-instances", "MeetingRoom", "Campus"},
			expectedStderr: "dfcad list-instances: " +
				UnexpectedArgumentsError{Extra: []string{"Campus"}}.Error() + "\n\n" + listInstancesUsage,
		},
		{
			name: "rejects an argument to list-types, which takes none",
			args: []string{"list-types", "MeetingRoom"},
			expectedStderr: "dfcad list-types: " +
				UnexpectedArgumentsError{Extra: []string{"MeetingRoom"}}.Error() + "\n\n" + listTypesUsage,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, model()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitUsage, run(testCase.args, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Equal(t, testCase.expectedStderr, stderr.String())
		})
	}
}

// TestUnknownTypeErrorSaysWhereToLook checks that the error carries the
// declared set for a caller to branch on, and points a person at the call which
// lists it rather than printing a registry into a message.
func TestUnknownTypeErrorSaysWhereToLook(t *testing.T) {
	testCases := []struct {
		name     string
		declared []string
	}{
		{
			name:     "points at list-types when the registry declares some",
			declared: []string{"Campus", "MeetingRoom"},
		},
		{
			name:     "says the registry is empty when it declares none",
			declared: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := checkFilters(registryOf(t, testCase.declared), "Nope", "", "")

			var unknown UnknownTypeError
			require.ErrorAs(t, err, &unknown)
			assert.Equal(t, "Nope", unknown.Type)
			assert.Equal(t, testCase.declared, unknown.Declared)
			assert.Contains(t, unknown.Error(), "list-types")
		})
	}
}

// registryOf is a registry declaring one type per name and nothing else.
func registryOf(t *testing.T, types []string) *dfcad.Registry {
	t.Helper()

	var src strings.Builder
	src.WriteString("(project (globalid-namespace \"https://example.org/models/filters\"))\n")
	for _, name := range types {
		src.WriteString("(type " + name + " (kind Space) (geometry area) (description \"One type.\"))\n")
	}

	dir := tree(t, map[string]string{"registry.dfc": src.String()})

	graph, diags := dfcad.LoadGraph(dir)
	require.Empty(t, diags)

	return graph.Registry()
}

// TestCheckFiltersAcceptsWhatTheModelDeclares is the other half of the
// rejection table: every name the model does declare passes, so the check is
// not simply refusing everything.
func TestCheckFiltersAcceptsWhatTheModelDeclares(t *testing.T) {
	dir := tree(t, model())

	graph, diags := dfcad.LoadGraph(dir)
	require.Empty(t, diags)

	registry := graph.Registry()

	assert.NoError(t, checkFilters(registry, "", "", ""))
	assert.NoError(t, checkFilters(registry, "MeetingRoom", "Space", "frame:building"))

	for _, kind := range dfcad.Kinds() {
		assert.NoError(t, checkFilters(registry, "", string(kind), ""))
	}
}

// TestParseEndsTheFlagsAtADoubleDash is its own function because it is about
// the shared parsing rather than about either listing: resuming after an
// argument must not resume past a `--`, which is the one spelling there is for
// a path or a name that begins with a dash.
func TestParseEndsTheFlagsAtADoubleDash(t *testing.T) {
	testCases := []struct {
		name               string
		args               []string
		expectedPositional []string
	}{
		{
			name:               "takes everything after a double dash as an argument",
			args:               []string{"--", "-a.dfc", "-b.dfc"},
			expectedPositional: []string{"-a.dfc", "-b.dfc"},
		},
		{
			name:               "takes a flag written after a double dash as an argument",
			args:               []string{"--", "MeetingRoom", "--kind", "Space"},
			expectedPositional: []string{"MeetingRoom", "--kind", "Space"},
		},
		{
			name:               "still reads the flags written before it",
			args:               []string{"--format", formatHuman, "--", "-a.dfc"},
			expectedPositional: []string{"-a.dfc"},
		},
		{
			name:               "ends the flags after an argument as well",
			args:               []string{"a.dfc", "--", "-b.dfc"},
			expectedPositional: []string{"a.dfc", "-b.dfc"},
		},
		{
			name:               "reads a lone dash as an argument rather than as a terminator",
			args:               []string{"-", "--format", formatHuman},
			expectedPositional: []string{"-"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()

			cmd, ok := lookup("fmt")
			require.True(t, ok)

			globals := &globals{}
			flags := newFlagSet(cmd, globals)

			var stderr bytes.Buffer

			positional, code, done := parse(cmd, flags, globals,
				append([]string{"--root", dir}, testCase.args...), &stderr)

			require.False(t, done, stderr.String())
			require.Equal(t, exitSuccess, code)
			assert.Equal(t, testCase.expectedPositional, positional)
		})
	}
}

// TestRunListStillAnswersOnAModelWithDiagnostics is its own function because it
// is about a run over a model which is not sound: the listing is still a
// listing of what is there, the diagnostics still reach the person who wrote
// the file, and the exit code does not answer a question these commands were
// not asked. A discovery call which refuses to describe a tree until the tree
// is finished is one nobody reaches for while writing it.
func TestRunListStillAnswersOnAModelWithDiagnostics(t *testing.T) {
	files := model()
	files["entities/broken.dfc"] = unparseable

	for _, args := range [][]string{
		{"list-types"},
		{"list-instances"},
		{"list-geometry", "--predicate", "position"},
	} {
		t.Run(args[0]+" lists what loaded and reports the rest on stderr", func(t *testing.T) {
			t.Chdir(tree(t, files))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitSuccess, run(args, &stdout, &stderr))

			// What loaded is a real answer about what loaded.
			result := object(t, stdout.String())
			assert.Equal(t, args[0], result["command"])
			assert.NotEmpty(t, result)

			// The diagnostic is for whoever wrote the file, so it is on stderr
			// and never on the stream a caller pipes.
			assert.Contains(t, stderr.String(), "broken.dfc:1:")
		})
	}
}

// TestRunListInstancesAcceptsFlagsAfterTheType is its own function because it
// is about how the arguments parse rather than about what came back. A flag
// written after the type has to narrow the listing rather than be handed back
// unread, because a filter which is silently ignored is worse than one which
// does not exist.
func TestRunListInstancesAcceptsFlagsAfterTheType(t *testing.T) {
	testCases := [][]string{
		{"list-instances", "--frame", "frame:site-grid", "MeetingRoom"},
		{"list-instances", "MeetingRoom", "--frame", "frame:site-grid"},
		{"list-instances", "MeetingRoom", "--frame=frame:site-grid"},
	}

	for _, args := range testCases {
		t.Run(strings.Join(args[1:], " ")+" lists the same instances", func(t *testing.T) {
			t.Chdir(tree(t, model()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())
			assert.Equal(t, []string{"site:S-103"}, ids(listed[listInstancesResult](t, stdout.String())))
		})
	}
}

// TestRunListOutputIsDeterministic checks that two runs over the same model
// write byte-identical results, which is what makes diffing two runs mean
// something.
func TestRunListOutputIsDeterministic(t *testing.T) {
	for _, args := range [][]string{{"list-types"}, {"list-instances"}, {"list-instances", "MeetingRoom"}} {
		t.Run(strings.Join(args, " ")+" writes the same bytes twice", func(t *testing.T) {
			var results []string
			for range 2 {
				t.Chdir(tree(t, model()))

				var stdout, stderr bytes.Buffer
				require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

				results = append(results, stdout.String())
			}

			assert.Equal(t, results[0], results[1])
		})
	}
}

// TestRunListHumanOutputNeverChangesStdout is its own function because it is
// about the one property the format flag must not have: whichever format was
// asked for, and however loud the run was told to be, stdout is the same bytes.
func TestRunListHumanOutputNeverChangesStdout(t *testing.T) {
	listing := func(t *testing.T, args ...string) (string, string) {
		t.Helper()

		t.Chdir(tree(t, model()))

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run(args, &stdout, &stderr), stderr.String())

		return stdout.String(), stderr.String()
	}

	machine, machineReport := listing(t, "list-types")
	human, humanReport := listing(t, "list-types", "--format", formatHuman)
	both, bothReport := listing(t, "list-types", "--format", formatHuman, "-v")

	assert.Equal(t, machine, human)
	assert.Equal(t, machine, both)

	// The summary is behind the format flag; the detail behind it is behind the
	// verbosity flag, because the detail is already the result on stdout.
	assert.Empty(t, machineReport)
	assert.Contains(t, humanReport, "5 types, 6 instances")
	assert.NotContains(t, humanReport, "MeetingRoom: kind Space")
	assert.Contains(t, bothReport, "MeetingRoom: kind Space, geometry area, 3 instances")
	assert.Contains(t, bothReport, "Fitting: kind Element, Interface, geometry line, surface, none at all, 0 instances")

	instances, instancesReport := listing(t, "list-instances", "--format", formatHuman, "-v")
	assert.Contains(t, instancesReport, "6 instances of 4 types")
	assert.Contains(t, instancesReport, "site:S-101: Meeting Room A, Space MeetingRoom")
	assert.Contains(t, instancesReport, "site:S-103: (no label), Space MeetingRoom")
	assert.NotEmpty(t, listed[listInstancesResult](t, instances).Instances)

	geometry, geometryReport := listing(t, "list-geometry", "--predicate", "setback", "--format", formatHuman, "-v")
	assert.Contains(t, geometryReport, "4 geometric nodes under setback: 0 vertices, 4 edges, 0 loops")

	// An edge is the one family which runs between two things, so it is the one
	// whose line says which two and in which direction.
	assert.Contains(t, geometryReport, "geom:E-14: edge geom:V-14 -> geom:V-11 in frame:building")
	assert.NotEmpty(t, listed[listGeometryResult](t, geometry).Nodes)

	corners, cornersReport := listing(t, "list-geometry", "--predicate", "position", "--format", formatHuman, "-v")
	assert.Contains(t, cornersReport, "12 geometric nodes under position: 12 vertices, 0 edges, 0 loops")
	assert.Contains(t, cornersReport, "geom:V-01: vertex in frame:building")
	assert.NotEmpty(t, listed[listGeometryResult](t, corners).Nodes)
}

// TestRunListUsage checks that help goes to stderr and exits zero, which is the
// half of the contract that keeps prose off the stream a caller pipes.
func TestRunListUsage(t *testing.T) {
	testCases := []struct {
		name           string
		args           []string
		expectedStderr string
	}{
		{
			name:           "prints the list-types usage to stderr and succeeds",
			args:           []string{"list-types", "-h"},
			expectedStderr: listTypesUsage,
		},
		{
			name:           "prints the list-instances usage to stderr and succeeds",
			args:           []string{"list-instances", "-h"},
			expectedStderr: listInstancesUsage,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitSuccess, run(testCase.args, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Equal(t, testCase.expectedStderr, stderr.String())
		})
	}
}

// TestListErrorsAreNotSwallowed checks that a stdout which cannot be written
// reports a failure rather than an unexplained success.
func TestListErrorsAreNotSwallowed(t *testing.T) {
	for _, args := range [][]string{
		{"list-types"},
		{"list-instances"},
		{"list-geometry", "--predicate", "position"},
	} {
		t.Run(args[0]+" reports a stdout it cannot write", func(t *testing.T) {
			t.Chdir(tree(t, model()))

			var stderr bytes.Buffer

			assert.Equal(t, exitLoad, run(args, brokenWriter{}, &stderr))
			assert.Contains(t, stderr.String(), "dfcad "+args[0]+":")
		})
	}
}

// geometryIDs is the id of each listed geometric node.
func geometryIDs(result listGeometryResult) []string {
	out := make([]string, 0, len(result.Nodes))
	for _, node := range result.Nodes {
		out = append(out, node.ID)
	}
	return out
}

// listGeometry runs list-geometry against the fixture tree and returns what it
// wrote, failing the test on anything but a success.
func listGeometryOf(t *testing.T, args ...string) listGeometryResult {
	t.Helper()

	t.Chdir(tree(t, model()))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitSuccess, run(append([]string{"list-geometry"}, args...), &stdout, &stderr), stderr.String())

	result := listed[listGeometryResult](t, stdout.String())
	require.Equal(t, outputVersion, result.Version)
	require.Equal(t, "list-geometry", result.Command)

	return result
}

func TestRunListGeometry(t *testing.T) {
	testCases := []struct {
		name        string
		args        []string
		expectedIDs []string
	}{
		{
			name: "reports every geometric node carrying the predicate",
			args: []string{"--predicate", "position"},
			expectedIDs: []string{
				"geom:V-01", "geom:V-02", "geom:V-03", "geom:V-04",
				"geom:V-11", "geom:V-12", "geom:V-13", "geom:V-14",
				"geom:V-21", "geom:V-22", "geom:V-23", "geom:V-24",
			},
		},
		{
			// The question the story is about: the edges of a plot carry a
			// setback and nothing else in the model does, so naming the
			// predicate is the whole of the query.
			name:        "reports the edges a predicate only edges carry",
			args:        []string{"--predicate", "setback"},
			expectedIDs: []string{"geom:E-11", "geom:E-12", "geom:E-13", "geom:E-14"},
		},
		{
			name: "narrows to one family",
			args: []string{"--predicate", "position", "--family", "vertex"},
			expectedIDs: []string{
				"geom:V-01", "geom:V-02", "geom:V-03", "geom:V-04",
				"geom:V-11", "geom:V-12", "geom:V-13", "geom:V-14",
				"geom:V-21", "geom:V-22", "geom:V-23", "geom:V-24",
			},
		},
		{
			name:        "reports nothing when the family carries none of them",
			args:        []string{"--predicate", "setback", "--family", "loop"},
			expectedIDs: []string{},
		},
		{
			// A declared predicate nothing geometric is written under. It is an
			// answer rather than a failure: this model states areas of rooms
			// and no area of a corner, which is an ordinary model.
			name:        "reports a declared predicate no geometric node carries as an empty list",
			args:        []string{"--predicate", "area"},
			expectedIDs: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := listGeometryOf(t, testCase.args...)

			assert.Equal(t, testCase.expectedIDs, geometryIDs(result))
		})
	}
}

// TestRunListGeometryReportsThePredicateItWasAskedAbout is its own function
// because it is about the field which makes an empty answer readable: a caller
// collecting the listings of one level under three predicates has to be able to
// say which object answers which question.
func TestRunListGeometryReportsThePredicateItWasAskedAbout(t *testing.T) {
	assert.Equal(t, "setback", listGeometryOf(t, "--predicate", "setback").Predicate)
	assert.Equal(t, "area", listGeometryOf(t, "--predicate", "area").Predicate)
}

// TestRunListGeometryOnAPredicateNothingCarries is its own function because it
// asserts about the bytes rather than about the decoded answer: an empty
// collection is a list rather than a null, so a caller indexing it needs no
// special case for the model which records no spans.
func TestRunListGeometryOnAPredicateNothingCarries(t *testing.T) {
	t.Chdir(tree(t, model()))

	var stdout, stderr bytes.Buffer

	require.Equal(t, exitSuccess, run([]string{"list-geometry", "--predicate", "area"}, &stdout, &stderr),
		stderr.String())

	assert.Contains(t, stdout.String(), `"nodes":[]`)
}

// TestRunListGeometryNamesTheEndsOfAnEdgeInTheAuthoredOrder is its own function
// because it asserts about the whole of one entry rather than about which
// entries came back.
//
// The order of the two ends is the data: an edge is directed, and the region on
// the other side of it traverses it the other way. The west flank runs from the
// north-west corner back to the south-west one, which is the case a listing
// which sorted the pair would report the wrong way round.
func TestRunListGeometryNamesTheEndsOfAnEdgeInTheAuthoredOrder(t *testing.T) {
	result := listGeometryOf(t, "--predicate", "setback")

	assert.Equal(t, []listedGeometry{
		{
			ID:     "geom:E-11",
			Family: "edge",
			Label:  "Plot one, road frontage",
			Frame:  "frame:building",
			Start:  "geom:V-11",
			End:    "geom:V-12",
			Span:   result.Nodes[0].Span,
		},
		{
			ID:     "geom:E-12",
			Family: "edge",
			Label:  "Plot one, east flank",
			Frame:  "frame:building",
			Start:  "geom:V-12",
			End:    "geom:V-13",
			Span:   result.Nodes[1].Span,
		},
		{
			ID:     "geom:E-13",
			Family: "edge",
			Label:  "Plot one, rear",
			Frame:  "frame:building",
			Start:  "geom:V-13",
			End:    "geom:V-14",
			Span:   result.Nodes[2].Span,
		},
		{
			ID:     "geom:E-14",
			Family: "edge",
			Label:  "Plot one, west flank",
			Frame:  "frame:building",
			Start:  "geom:V-14",
			End:    "geom:V-11",
			Span:   result.Nodes[3].Span,
		},
	}, result.Nodes)
}

// TestRunListGeometryReportsWhereEachNodeWasWritten is its own function because
// the span is what makes this listing usable at all: an id which came back from
// a query nobody could have guessed is one whose next question is where it is
// written.
func TestRunListGeometryReportsWhereEachNodeWasWritten(t *testing.T) {
	result := listGeometryOf(t, "--predicate", "setback")

	require.NotEmpty(t, result.Nodes)
	for _, node := range result.Nodes {
		assert.Equal(t, "entities/geometry.dfc", filepath.ToSlash(node.Span.Start.Path), node.ID)
		assert.Positive(t, node.Span.Start.Line, node.ID)
		assert.Positive(t, node.Span.Start.Column, node.ID)
	}
}

// TestRunListGeometryOrdersByID is its own function because it asserts about
// the order of the whole list rather than about what one entry says.
//
// Id order rather than family order: grouping by family would reorder the whole
// answer the day an edge was given a claim it did not have before, and an id is
// the one thing about a node which does not change.
func TestRunListGeometryOrdersByID(t *testing.T) {
	ordered := geometryIDs(listGeometryOf(t, "--predicate", "position"))

	require.NotEmpty(t, ordered)
	assert.True(t, sortedStrings(ordered))
}

// listRetractedRegistry is the vocabulary [listRetractedGeometry] is judged
// against.
const listRetractedRegistry = `(project
  (label "Retraction fixture")
  (globalid-namespace "https://example.org/models/retracted"))

(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace survey (description "Claim ids issued by the surveyor."))

(frame frame:site-grid (label "Site survey grid") (unit m))

(predicate position
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "The location of a vertex in its frame."))
`

// listRetractedGeometry is a model whose only statement about one corner was
// withdrawn.
//
// It is a fixture of its own rather than an addition to [model] because it is a
// different shape of question: every other assertion here is about which nodes
// carry a predicate, and this is about which claims count as carrying one.
const listRetractedGeometry = `(vertex geom:V-01
  (frame frame:site-grid)
  (position
    (id survey:P-01a)
    (value (0.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")
    (rank deprecated)
    (superseded-by survey:P-01b))
  (position
    (id survey:P-01b)
    (value (0.012 0.0 0.0) m)
    (source "As-built check AB-2026-029, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.003 m))
    (date "2026-05-06")))

(vertex geom:V-02
  (frame frame:site-grid)
  (position
    (id survey:P-02a)
    (value (4.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")
    (rank deprecated)
    (superseded-by survey:P-01b)))
`

// TestRunListGeometryLeavesOutARetractedClaim is its own function because it
// asserts about which claims count as carrying a predicate rather than about
// which nodes carry one.
//
// A deprecated claim is retracted rather than out-ranked, and resolution never
// considers one. Listing the corner whose only position was withdrawn would
// answer "which corners does this model record a position for" with a corner
// nobody stands behind; `dfcad claims` is the audit view which reports it.
func TestRunListGeometryLeavesOutARetractedClaim(t *testing.T) {
	t.Chdir(tree(t, map[string]string{
		"registry.dfc":          listRetractedRegistry,
		"entities/geometry.dfc": listRetractedGeometry,
	}))

	var stdout, stderr bytes.Buffer

	require.Equal(t, exitSuccess, run([]string{"list-geometry", "--predicate", "position"}, &stdout, &stderr),
		stderr.String())

	result := listed[listGeometryResult](t, stdout.String())

	// V-01 was re-surveyed and still records where it is. V-02's one statement
	// was withdrawn and replaced by nothing, so the model records nothing about
	// where it is.
	assert.Equal(t, []string{"geom:V-01"}, geometryIDs(result))
}

// TestRunListGeometryRejectsWhatItCannotAskAbout walks the ways an invocation
// can fail to be a question.
//
// Each is a usage error rather than an empty list, and stdout stays empty
// because the run produced no result.
func TestRunListGeometryRejectsWhatItCannotAskAbout(t *testing.T) {
	declaredPredicates := []string{"area", "frame-transform", "position", "setback"}

	testCases := []struct {
		name           string
		args           []string
		expectedStderr string
	}{
		{
			// Which predicate carries what is project data, so there is nothing
			// to default to and the run has not asked a question yet.
			name: "refuses a run which named no predicate",
			args: []string{"list-geometry"},
			expectedStderr: "dfcad list-geometry: " +
				MissingVocabularyError{Flags: []string{flagPredicate}}.Error() + "\n\n" + listGeometryUsage,
		},
		{
			name: "names a predicate the registry does not declare",
			args: []string{"list-geometry", "--predicate", "setbackk"},
			expectedStderr: "dfcad list-geometry: " +
				UnknownPredicateError{Predicate: "setbackk", Declared: declaredPredicates}.Error() + "\n",
		},
		{
			name: "names a family which is none of the three",
			args: []string{"list-geometry", "--predicate", "position", "--family", "vertices"},
			expectedStderr: "dfcad list-geometry: " +
				UnknownFamilyError{Family: "vertices", Known: families}.Error() + "\n",
		},
		{
			name: "rejects an argument, which it takes none of",
			args: []string{"list-geometry", "--predicate", "position", "geom:V-01"},
			expectedStderr: "dfcad list-geometry: " +
				UnexpectedArgumentsError{Extra: []string{"geom:V-01"}}.Error() + "\n\n" + listGeometryUsage,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, model()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitUsage, run(testCase.args, &stdout, &stderr))

			assert.Empty(t, stdout.String())
			assert.Equal(t, testCase.expectedStderr, stderr.String())
		})
	}
}

// TestCheckFamilyAcceptsTheThreeFamilies is the other half of the rejection
// table: every family the format has passes, so the check is not simply
// refusing everything.
func TestCheckFamilyAcceptsTheThreeFamilies(t *testing.T) {
	assert.NoError(t, checkFamily(""))

	for _, family := range families {
		assert.NoError(t, checkFamily(family))
	}

	var unknown UnknownFamilyError
	require.ErrorAs(t, checkFamily("node"), &unknown)
	assert.Equal(t, "node", unknown.Family)
	assert.Equal(t, []string{"vertex", "edge", "loop"}, unknown.Known)
}
