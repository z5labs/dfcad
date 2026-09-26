// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unrankedRegistry is the vocabulary of a room one of whose corners states no
// accuracy: the reproduction of z5labs/dfcad#237, with what buildable and check
// need to have something to say about it — a setback predicate, and an area
// claimed of the room that an invariant compares against its shape.
const unrankedRegistry = `(project
  (label "Unranked position repro")
  (globalid-namespace "https://example.org/models/unranked-repro")
  (description "A room with one corner whose position states no accuracy."))
(namespace frame (description "Coordinate frames."))
(namespace geom (description "Geometric nodes."))
(namespace method (description "How a value was obtained."))
(namespace site (description "Semantic nodes."))
(type House (kind Building) (geometry absent) (description "A dwelling."))
(type Level (kind Storey) (geometry absent) (description "One floor."))
(type
  Room
  (kind Space)
  (geometry area)
  (description "A room drawn as its outline.")
  (classification "IFC4" "IfcSpace")
  (invariant
    claim-agrees-with-geometry
    (discrepancy area-slack)
    (position position)
    (predicate floor-area)
    (tolerance corner)))
(predicate
  floor-area
  (unit usft2)
  (shape scalar)
  (description "How much floor a room has."))
(predicate
  height
  (unit usft)
  (shape scalar)
  (description "How tall a thing is, from its base."))
(predicate
  position
  (unit usft)
  (shape coordinate)
  (dimension 3)
  (description "Where a corner is."))
(predicate
  setback
  (unit usft)
  (shape scalar)
  (description "How far back from an edge a thing has to sit."))
(tolerance
  area-slack
  (value 0.5 usft2)
  (description "How far a claimed area and a computed one may differ."))
(tolerance
  corner
  (value 0.01 usft)
  (description "How close two corners are one corner."))
(tolerance
  facet
  (value 0.05 usft)
  (description "How far a chord may fall from its curve."))
(frame frame:plan (label "Plan grid") (unit usft))
`

// unrankedEntities is the house, its floor and the room, whose height states no
// accuracy either — which is what lets one export show an unranked scalar and an
// unranked position read by the same rule.
const unrankedEntities = `(node site:B (label "House") (kind Building) (type House))
(node site:L (label "Main floor") (kind Storey) (type Level) (within site:B))
(node
  site:R
  (label "Den")
  (kind Space)
  (type Room)
  (geometry area)
  (within site:L)
  (boundary geom:R)
  (floor-area
    (value 100.0 usft2)
    (source "Schedule")
    (method method:take-off)
    (accuracy (independent 0.5 usft2))
    (date "2026-09-26"))
  (height
    (value 9.0 usft)
    (source "Section")
    (method method:take-off)
    (date "2026-09-26")))
`

// unrankedGeometry is a 10 by 10 usft square, whose corner geom:C carries a
// position claim with no accuracy, written in the unit of its frame.
const unrankedGeometry = `(vertex
  geom:A
  (frame frame:plan)
  (position
    (value (0.0 0.0 0.0) usft)
    (source "Plan")
    (method method:take-off)
    (accuracy (independent 0.01 usft))
    (date "2026-09-26")))
(vertex
  geom:B
  (frame frame:plan)
  (position
    (value (10.0 0.0 0.0) usft)
    (source "Plan")
    (method method:take-off)
    (accuracy (independent 0.01 usft))
    (date "2026-09-26")))
(vertex
  geom:C
  (frame frame:plan)
  (position
    (value (10.0 10.0 0.0) usft)
    (source "Plan")
    (method method:take-off)
    (date "2026-09-26")))
(vertex
  geom:D
  (frame frame:plan)
  (position
    (value (0.0 10.0 0.0) usft)
    (source "Plan")
    (method method:take-off)
    (accuracy (independent 0.01 usft))
    (date "2026-09-26")))
(edge
  geom:A-B
  (frame frame:plan)
  (vertices geom:A geom:B)
  (setback
    (value 1.0 usft)
    (source "Plan")
    (method method:take-off)
    (accuracy (independent 0.01 usft))
    (date "2026-09-26")))
(edge
  geom:B-C
  (frame frame:plan)
  (vertices geom:B geom:C)
  (setback
    (value 1.0 usft)
    (source "Plan")
    (method method:take-off)
    (accuracy (independent 0.01 usft))
    (date "2026-09-26")))
(edge
  geom:C-D
  (frame frame:plan)
  (vertices geom:C geom:D)
  (setback
    (value 1.0 usft)
    (source "Plan")
    (method method:take-off)
    (accuracy (independent 0.01 usft))
    (date "2026-09-26")))
(edge
  geom:D-A
  (frame frame:plan)
  (vertices geom:D geom:A)
  (setback
    (value 1.0 usft)
    (source "Plan")
    (method method:take-off)
    (accuracy (independent 0.01 usft))
    (date "2026-09-26")))
(loop geom:R (frame frame:plan) (edges geom:A-B geom:B-C geom:C-D geom:D-A))
`

// unranked is the model every test below runs against.
func unranked() map[string]string {
	return map[string]string{
		"registry.dfc": unrankedRegistry,
		"entities.dfc": unrankedEntities,
		"geometry.dfc": unrankedGeometry,
	}
}

// TestRunReadsAnUnrankedCornerInEveryCommandWhichReadsPositions is a table over
// commands rather than one per command because the behaviour is one rule: a
// corner whose one position claim states no accuracy is read, as specification
// section 6.5 says an unrankable claim is, by every command which reads a shape.
// Before that rule, each of these refused the room and blamed the unit.
func TestRunReadsAnUnrankedCornerInEveryCommandWhichReadsPositions(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "resolve answers the corner",
			args: []string{"resolve", "geom:C", "position"},
		},
		{
			name: "measure measures the room",
			args: []string{"measure", "--position", "position", "--tolerance", "corner", "site:R"},
		},
		{
			name: "tessellate draws the room",
			args: []string{"tessellate", "--position", "position", "--tolerance", "corner", "--chord", "facet", "site:R"},
		},
		{
			name: "plan draws the floor",
			args: []string{"plan", "--position", "position", "--tolerance", "corner", "--annotate", "height", "site:L"},
		},
		{
			name: "buildable derives the room's buildable region",
			args: []string{
				"buildable", "--setback", "setback", "--position", "position", "--tolerance", "corner", "site:R",
			},
		},
		{
			name: "site sites the room inside itself",
			args: []string{"site", "--within", "site:R", "--position", "position", "--tolerance", "corner", "site:R"},
		},
		{
			name: "check compares the room's claimed area against its shape",
			args: []string{"check"},
		},
		{
			name: "export writes the room",
			args: []string{
				"export", "--out", "model.ifc",
				"--position", "position", "--tolerance", "corner", "--chord", "facet", "--height", "height",
			},
		},
		{
			name: "export-map writes the room",
			args: []string{
				"export-map", "--out", "model.gml",
				"--position", "position", "--tolerance", "corner", "--chord", "facet",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, unranked())

			args := slices.Clone(testCase.args)
			for i, arg := range args {
				if arg == "--out" {
					args[i+1] = filepath.Join(t.TempDir(), args[i+1])
				}
			}

			stdout, stderr := invoke(t, exitSuccess, root, args...)

			assert.NotEmpty(t, stdout)
			assert.NotContains(t, stderr, "geom:C", "no diagnostic is about the corner which states no accuracy")
		})
	}
}

// TestRunMeasureNamesAnUnrankedCorner is its own function because the
// assertion is on what the answer rests on rather than on whether there is one:
// a measurement over a corner nobody gave an accuracy to is still the room's
// area, and it says which corner its uncertainty cannot be computed for.
func TestRunMeasureNamesAnUnrankedCorner(t *testing.T) {
	root := tree(t, unranked())
	stdout, _ := invoke(t, exitSuccess, root, "measure", "--position", "position", "--tolerance", "corner", "site:R")

	result := listed[measureResult](t, stdout)

	require.NotNil(t, result.Area)
	assert.InDelta(t, 100.0, result.Area.Value, 1e-9)
	assert.Equal(t, "usft²", result.Area.Unit)

	require.NotNil(t, result.Budget)
	assert.Equal(t, []string{"geom:C"}, result.Budget.Unranked)
	assert.Len(t, result.Budget.Unknown, 1, "the unranked claim taints the budget")
	assert.Nil(t, result.Budget.Combined, "and no combined figure comes out of it")
	assert.Len(t, result.Budget.Terms, 3, "one term per corner which stated an accuracy, and none for the one which did not")
}
