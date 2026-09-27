// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/z5labs/dfcad"
	"github.com/z5labs/dfcad/ifc"
)

// filling is one element standing in an opening of the element it is within.
type filling struct {
	// node is the element standing in the opening: the door, the window.
	node *dfcad.SemanticNode

	// host is the element it is within, which the opening is cut through.
	host *dfcad.SemanticNode
}

// drawnRun is what drawing a node as a line established about it.
//
// It is recorded a piece at a time, because the drawing stops at the first
// thing it cannot establish: a run with no thickness claimed has a plan and
// nothing to widen it by, and one with no height claimed has a footprint and no
// body. What an opening needs is read off whichever pieces were reached.
type drawnRun struct {
	// runs are the straight pieces of the node's run, in the root frame.
	runs [][]dfcad.Point

	// thickness is what the run was widened by, where widened says it was.
	thickness float64
	widened   bool

	// base is where the body starts, from the datum the node's placement
	// stands at, and height is how far it rises from there, where swept says
	// a body was drawn at all.
	base   float64
	height float64
	swept  bool
}

// nested is the element node is within, where it is within one.
//
// Only an Element may be written within an Element
// ([SPEC §6.9.1](SPEC.md#691-the-containment-hierarchy)), so this is one step
// up the containment chain and not a walk: a node within a space is contained
// by it whatever kind the node is, and a node within an element is either a
// part of it or stands in an opening of it.
func (e *exporter) nested(node *dfcad.SemanticNode) (*dfcad.SemanticNode, bool) {
	within, ok := node.Within()
	if !ok {
		return nil, false
	}

	parent, held := e.graph.Node(within)
	if !held || parent.Retired() || parent.Kind() != dfcad.KindElement {
		return nil, false
	}

	return parent, true
}

// fillsOpening reports whether node's type says its instances stand in an
// opening of the element they are within.
//
// It is the registry's `(fills-opening #t)` and nothing else. The type's name
// and its classification are not read: a door classified as IfcDoor which the
// registry has not said fills an opening is a part of its wall here, and a
// hatch nobody classified which the registry has said fills one is cut into
// its slab. Anything else would be the switch on a type name
// [0020](docs/decisions/0020-export-is-a-boundary-and-the-closed-set-is-what-crosses-it.md)
// rules out, spelled in IFC's vocabulary instead of this project's.
func (e *exporter) fillsOpening(node *dfcad.SemanticNode) bool {
	declared, held := e.registry.Type(node.Type())
	return held && declared.FillsOpening
}

// opening reports whether node is written as an IfcOpeningElement: whether the
// classification its type declares names the entity this export writes an
// opening as.
//
// It does not decide whether node fills an opening — `(fills-opening #t)`
// does that, and nothing else
// ([0027](docs/decisions/0027-an-element-fills-an-opening-because-its-type-says-so.md)).
// It decides how IFC4 states one which does. IFC4 fills no opening with
// another, so a filling written as one is the opening in its host rather than
// something standing in an opening cut for it, and no element is cut through
// or filled by one. That is the schema's rule about the entity written, not a
// reading of what the type is called.
func (e *exporter) opening(node *dfcad.SemanticNode) bool {
	declared, held := e.registry.Type(node.Type())
	if !held {
		return false
	}

	code, classified := declared.ClassifiedAs(classificationSystem)

	return classified && ifc.Entity(strings.ToUpper(code)) == openingEntity
}

// openingEntity is the product entity a node is written as when it is an
// opening element itself — the cased opening, not the IfcRelVoidsElement
// relating it to its host — and the one entity nothing is cut through and
// nothing fills.
const openingEntity ifc.Entity = "IFCOPENINGELEMENT"

// refuseOpeningHost refuses a filling set in an element written as an
// IfcOpeningElement.
//
// IFC4 voids no opening and fills none: a door set in a cased opening is a door
// set in the wall the opening is in, and a file which cut a second opening
// through the first is one no reader can subtract.
func (e *exporter) refuseOpeningHost(node, host *dfcad.SemanticNode) {
	e.refuse(node, fmt.Sprintf(
		"expected %s, whose type says it fills an opening, to be within an element an opening can be cut through, "+
			"found %s, whose type is classified IfcOpeningElement: IFC4 cuts no opening through an opening and "+
			"fills none", node.ID(), host.ID()),
		"put the filling within the element the opening is in, or take (fills-opening #t) off its type")
	e.diags[len(e.diags)-1].Related = []dfcad.RelatedLocation{{
		Span:    host.Span(),
		Message: "the opening it is within",
	}}
}

// swept reports whether a representation holds a body.
func swept(representation *ifc.Representation) bool {
	if representation == nil {
		return false
	}

	for _, shape := range representation.Shapes {
		if shape.Identifier == bodyShape {
			return true
		}
	}

	return false
}

// openings are the voids cut through the elements of the model, one per
// element standing in an opening, in the id order of the element standing in
// it.
//
// Each carries identifiers derived from the id of the element filling it,
// under names no node id can carry, so an opening is as stable across exports
// of an unchanged tree as the door which fills it and never shares an
// identifier with it
// ([0004](docs/decisions/0004-globalid-derives-from-a-pinned-namespace.md)):
// an opening is not a node of the model, and there is one per filling.
//
// A filling or a host the file does not hold — a retired one — is left out
// rather than referenced, for the reason a zone's members are.
func (e *exporter) openings() []ifc.Opening {
	out := make([]ifc.Opening, 0, len(e.filled))

	for _, held := range e.filled {
		host, filler := held.host.ID(), held.node.ID()
		if !e.written[host] || !e.written[filler] {
			continue
		}

		opening := ifc.Opening{
			GlobalID:  e.identify(dfcad.ID("ifc/opening/" + filler)),
			Host:      e.identify(host),
			Voids:     e.identify(dfcad.ID("ifc/voids/" + filler)),
			Filling:   e.identify(filler),
			Fills:     e.identify(dfcad.ID("ifc/fills/" + filler)),
			Placement: &ifc.Placement{},
		}

		if e.shapes.complete() {
			opening.Representation = e.cut(held)
		}

		out = append(out, opening)
	}

	return out
}

// voids are the relationships joining each element written as an
// IfcOpeningElement which fills an opening to the element it is within, in id
// order of the opening.
//
// The opening is already in the file as a product, contained in its storey
// with its own placement and its own shapes like any drawn element; this is
// the one relationship that says what it is cut out of. Its identifier is
// derived under the name the relationship voiding its host would have if it
// were a filling rather than an opening, which no node id can carry, and a
// node is one or the other.
func (e *exporter) voids() []ifc.Void {
	out := make([]ifc.Void, 0, len(e.voiding))

	for _, held := range e.voiding {
		host, opening := held.host.ID(), held.node.ID()
		if !e.written[host] || !e.written[opening] {
			continue
		}

		out = append(out, ifc.Void{
			GlobalID: e.identify(dfcad.ID("ifc/voids/" + opening)),
			Host:     e.identify(host),
			Opening:  e.identify(opening),
		})
	}

	return out
}

// cut is the body of the void a filling stands in, and is nil where there is
// no body to cut it out of or none to cut it to.
//
// The void is the filling's run, widened by the thickness of the host rather
// than of the filling, and swept from the filling's own base through the
// filling's own height. The host's thickness because a door is thinner than
// the wall it is in and the hole goes through the wall: a void as thick as the
// door leaves a skin either side of it. The filling's base and height because
// the hole is where the door is, which is its sill claimed of it and nothing
// read off the wall.
//
// It is placed at the host's own placement, which every host drawn as a line
// has at its storey's origin, so the coordinates are the filling's as the walk
// drew them.
func (e *exporter) cut(held filling) *ifc.Representation {
	host, filler := held.host, held.node

	hosted, drawnHost := e.lines[host.ID()]
	filled, drawnFilling := e.lines[filler.ID()]

	// A filling off its host's run is refused whenever both runs were drawn,
	// whether or not there is a solid to cut: a door drawn beside its wall is a
	// mistake in the model which a void would put somewhere the model does not
	// put the door, and one which no void would be cut for is still the same
	// mistake.
	if drawnHost && drawnFilling && !e.alongside(held, hosted.runs, filled.runs) {
		return nil
	}

	// A host with no body has nothing to cut. The relationships still say which
	// element the filling stands in, which is the whole of what a model with
	// no solids states.
	if !e.bodied[host.ID()] {
		return nil
	}

	if !drawnHost || !hosted.widened || !drawnFilling || !filled.swept {
		e.diags = append(e.diags, dfcad.Diagnostic{
			Severity: dfcad.SeverityWarning,
			Span:     filler.Span(),
			Message: fmt.Sprintf(
				"expected %s to be drawn as a run along %s with a body swept over it, to cut the opening it fills "+
					"out of %s, found %s: the opening is written without a shape and %s stays solid where %s stands",
				filler.ID(), host.ID(), host.ID(), e.uncut(held, hosted, filled), host.ID(), filler.ID()),
			Hint: "an opening is cut to the run, the height and the base of what fills it, through the thickness " +
				"of what it is cut out of; a host and a filling drawn as lines, each with a thickness and the " +
				"filling with a height claimed, are what one is cut from",
			Related: []dfcad.RelatedLocation{{
				Span:    host.Span(),
				Message: "the element it fills an opening in",
			}},
		})
		return nil
	}

	plans := widened(filled.runs, hosted.thickness)
	if len(plans) == 0 {
		return nil
	}

	solids := make([]ifc.Item, 0, len(plans))
	for _, plan := range plans {
		solids = append(solids, ifc.ExtrudedArea{
			Profile:   ifc.ArbitraryProfile{Outer: plan},
			Position:  ifc.Placement{Location: ifc.Point{Z: filled.base}},
			Direction: ifc.Direction{Z: 1},
			Depth:     filled.height,
		})
	}

	return &ifc.Representation{Shapes: []ifc.Shape{{
		Context:    bodyShape,
		Identifier: bodyShape,
		Type:       solidRepresentation,
		Items:      solids,
	}}}
}

// uncut says which piece of an opening's drawing is missing, in the words a
// warning wants.
func (e *exporter) uncut(held filling, hosted, filled *drawnRun) string {
	switch {
	case hosted == nil:
		return fmt.Sprintf("that %s is not drawn as a run", held.host.ID())
	case !hosted.widened:
		return fmt.Sprintf("that %s is widened by no thickness", held.host.ID())
	case filled == nil:
		return fmt.Sprintf("that %s is not drawn as a run", held.node.ID())
	default:
		return fmt.Sprintf("that %s has no body", held.node.ID())
	}
}

// alongside reports whether a filling's run lies on its host's run in plan,
// and refuses the filling naming both where it does not.
//
// Every corner of the filling's run and the midpoint of every straight piece
// of it has to lie on the host's run: the corners say where the filling starts
// and stops, and the midpoints are what stop a door drawn across the corner of
// an L-shaped wall — both ends on the wall, the middle through the room —
// passing as one set in it.
//
// It is in plan because the two are at different heights as often as not: a
// window drawn at its sill lies on its wall's run exactly as one drawn at the
// floor with its sill claimed of it does. How close is close enough is the
// corner tolerance the run named, widened by the chord tolerance, because both
// runs are drawn to the chord tolerance and a curved wall's chords and the
// chords of a window in it are two drawings of one curve.
func (e *exporter) alongside(held filling, host, filler [][]dfcad.Point) bool {
	allowance := 0.0
	if corner, declared := e.registry.Tolerance(e.shapes.tolerance); declared {
		allowance += corner.Value
	}
	if chord, declared := e.registry.Tolerance(e.shapes.chord); declared {
		allowance += chord.Value
	}

	for _, run := range filler {
		for i, corner := range run {
			probes := []dfcad.Point{corner}
			if i+1 < len(run) {
				next := run[i+1]
				probes = append(probes, dfcad.Point{
					(corner[0] + next[0]) / 2, (corner[1] + next[1]) / 2, (corner[2] + next[2]) / 2,
				})
			}

			for _, probe := range probes {
				off := planDistance(probe, host)
				if off <= allowance {
					continue
				}

				e.refuse(held.node, fmt.Sprintf(
					"expected the run of %s to lie along the run of %s, the element it fills an opening in, found "+
						"a point of it at (%s, %s) %s from that run in plan",
					held.node.ID(), held.host.ID(), figure(probe[0]), figure(probe[1]), figure(off)),
					"an opening is cut where its filling stands, so a filling off its host's run would void the "+
						"host somewhere the model does not put it; draw the filling on the host's run, or take it "+
						"out of the host with (within ...)")
				e.diags[len(e.diags)-1].Related = []dfcad.RelatedLocation{{
					Span:    held.host.Span(),
					Message: "the element it fills an opening in",
				}}

				return false
			}
		}
	}

	return true
}

// planDistance is how far a point is from the nearest straight piece of a set
// of runs, in plan.
func planDistance(point dfcad.Point, runs [][]dfcad.Point) float64 {
	nearest := math.Inf(1)

	for _, run := range runs {
		for i := 0; i+1 < len(run); i++ {
			nearest = math.Min(nearest, segmentDistance(point, run[i], run[i+1]))
		}

		// A run of one point is a point, which is still somewhere a filling
		// could be measured from.
		if len(run) == 1 {
			nearest = math.Min(nearest, math.Hypot(point[0]-run[0][0], point[1]-run[0][1]))
		}
	}

	return nearest
}

// segmentDistance is how far a point is from a straight piece, in plan.
func segmentDistance(point, from, to dfcad.Point) float64 {
	dx, dy := to[0]-from[0], to[1]-from[1]

	along := dx*dx + dy*dy
	if along == 0 {
		return math.Hypot(point[0]-from[0], point[1]-from[1])
	}

	t := ((point[0]-from[0])*dx + (point[1]-from[1])*dy) / along
	t = math.Max(0, math.Min(1, t))

	return math.Hypot(point[0]-(from[0]+t*dx), point[1]-(from[1]+t*dy))
}
