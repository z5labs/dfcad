// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"strings"

	"github.com/z5labs/dfcad"
)

// flagNear is the list-geometry flag which narrows the listing to the vertices
// at a point.
const flagNear = "near"

// RequiredFlagError is a flag written without another it cannot be read
// without.
//
// It is refused rather than read with a guess for the other, because each of
// them is project data: which frame a point is in and how close counts as at it
// are things the project wrote down, and a lookup in a frame nobody named
// answers a question nobody asked.
type RequiredFlagError struct {
	// Flag is the flag which was written, spelled without its dashes.
	Flag string

	// Requires are the flags it needs beside it which were not written, spelled
	// without their dashes, in the order the usage lists them.
	Requires []string
}

// Error implements [error].
func (e RequiredFlagError) Error() string {
	spelled := make([]string, 0, len(e.Requires))
	for _, flag := range e.Requires {
		spelled = append(spelled, "--"+flag)
	}

	return fmt.Sprintf("--%s is read beside %s, found no %s",
		e.Flag, strings.Join(spelled, " and "), strings.Join(spelled, " or "))
}

// NearFamilyError is a --family other than vertex written beside --near.
//
// Only a vertex is at a point. An edge and a loop run between vertices, and
// listing one because an end of it is near would be a different question from
// the one the flag asks, answered as though it were the same.
type NearFamilyError struct {
	// Family is the family which was asked for.
	Family string
}

// Error implements [error].
func (e NearFamilyError) Error() string {
	return fmt.Sprintf("--near lists vertices, found --family %s: only a vertex is at a point", e.Family)
}

// nearAsked is the part of a list-geometry invocation which asks for the
// vertices at a point, as it was written.
type nearAsked struct {
	// point is --near, and empty where it was not written.
	point string

	// tolerance is --tolerance, and empty where it was not written.
	tolerance string

	// predicate is --predicate, which a position is claimed under.
	predicate string

	// frames and families are the filters, less their empty values.
	frames   []string
	families []string
}

// asked reports whether the invocation asked for a lookup at all.
func (asked nearAsked) asked() bool {
	return asked.point != ""
}

// check reports a lookup which cannot be asked whatever the model says: a flag
// written without the ones it needs, and a family which is never at a point.
//
// It reads nothing of the model, so it runs before the load, for the reason
// every closed-set check does: nothing in the tree makes an edge a thing which
// is at a point.
func (asked nearAsked) check() error {
	if !asked.asked() {
		if asked.tolerance != "" {
			return RequiredFlagError{Flag: flagTolerance, Requires: []string{flagNear}}
		}
		return nil
	}

	var missing []string
	if len(asked.frames) == 0 {
		missing = append(missing, flagFrame)
	}
	if asked.tolerance == "" {
		missing = append(missing, flagTolerance)
	}
	if len(missing) > 0 {
		return RequiredFlagError{Flag: flagNear, Requires: missing}
	}

	// One frame and not a union of them: the point is a coordinate in one frame,
	// and the tolerance is held to that frame's unit.
	if _, err := once(flagFrame, asked.frames); err != nil {
		return err
	}

	for _, family := range asked.families {
		if family != familyVertex {
			return NearFamilyError{Family: family}
		}
	}

	return nil
}

// lookup is the vertices within the tolerance of the point, read by the rule
// scaffold-loop snaps a corner by.
//
// The point is read the way scaffold-loop reads a corner, by
// [dfcad.ParseCorner], so a component count or a number which one refuses this
// refuses in the same words. A predicate which declares no coordinate is not
// read against at all: the point is handed over unread, and the engine names
// the predicate as the mistake rather than the point.
func (asked nearAsked) lookup(graph *dfcad.Graph) (dfcad.Nearness, error) {
	spec := dfcad.NearSpec{
		Frame:     dfcad.ID(asked.frames[0]),
		Predicate: asked.predicate,
		Tolerance: asked.tolerance,
	}

	declared, ok := graph.Registry().Predicate(asked.predicate)
	if ok && declared.Shape == dfcad.ShapeCoordinate {
		point, err := dfcad.ParseCorner(asked.point, "", declared)
		if err != nil {
			return dfcad.Nearness{}, err
		}
		spec.Point = point
	}

	return graph.VerticesNear(spec)
}

// nearEntry is the lookup a list-geometry answer was narrowed by, as it echoes
// it.
type nearEntry struct {
	// At is the point, component by component, in the frame's unit.
	At []float64 `json:"at"`

	// Tolerance is the declaration a vertex had to be within of the point.
	Tolerance toleranceEntry `json:"tolerance"`
}
