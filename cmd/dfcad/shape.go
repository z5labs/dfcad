// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/z5labs/dfcad"
	"github.com/z5labs/dfcad/ifc"
)

// The flags export takes to name the predicates a body is built from, named
// here because the usage and the errors which refuse them name them.
//
// Neither has a default and neither ever will. Which predicate carries a
// room's height and which carries a partition's thickness is project
// vocabulary, and a figure compiled in here would be this command measuring
// something nobody measured
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
const (
	flagHeight    = "height"
	flagThickness = "thickness"
)

// The representations a space is written with, and the views of the model's
// coordinate space each is drawn in.
//
// The two are separate representations of one shape definition rather than one
// representation, and that is the whole point of this file. The footprint is
// what the model states: authors write plan polygons, the specification's form
// for a space is a bounded curve, and a file carrying only that is correct.
// The body is a convenience for a viewer, built from a height claimed
// elsewhere, and it is labelled as a convenience by being a second
// representation instead of the only one — so a reader which wants what the
// model says takes the footprint and never has to guess which of the two that
// was.
const (
	footprintShape = "FootPrint"
	bodyShape      = "Body"

	// The RepresentationTypes IFC fixes for what each holds.
	curveRepresentation = "Curve2D"
	solidRepresentation = "SweptSolid"

	// The IfcGeometricProjectionEnum member each subcontext is for.
	planView  = "PLAN_VIEW"
	modelView = "MODEL_VIEW"
)

// contextType is the ContextType every context and subcontext this export
// writes carries, which is what IFC spells the modelling view of a project.
const contextType = "Model"

// claimDateLayout is how a claim's date is written into the file, which is the
// date form the model itself is authored in.
const claimDateLayout = "2006-01-02"

// heightProvenance and thicknessProvenance are the property sets the two
// claims behind a body are recorded in, and the descriptions a reader sees them
// under.
//
// Each name carries the project's own prefix rather than IFC's `Pset_`, which
// is reserved for the sets the specification defines. What is in them is not a
// standard set and saying otherwise would be a claim about the schema.
//
// They are two sets rather than one because they are two measurements: a wall's
// height may be surveyed and its thickness taken off a drawing, and a reader
// deciding whether to trust the solid has to be able to tell which is which.
const (
	heightProvenance            = "dfcad_HeightProvenance"
	heightProvenanceDescription = "The claim the extruded body's height was resolved from, and what is known about it."

	thicknessProvenance            = "dfcad_ThicknessProvenance"
	thicknessProvenanceDescription = "The claim the swept run's thickness was resolved from, and what is known about it."
)

// The properties that set holds, in the order they are written.
//
// They are the claim, taken apart rather than rendered into a sentence: a
// receiving system shows them one per row, and a reader deciding whether to
// trust the solid in front of them reads the source, the method and the
// accuracy rather than parsing prose out of one field.
const (
	propertyPredicate = "Predicate"
	propertyHeight    = "Height"
	propertyThickness = "Thickness"
	propertyUnit      = "Unit"
	propertySource    = "Source"
	propertyMethod    = "Method"
	propertyAccuracy  = "Accuracy"
	propertyDate      = "Date"
	propertyClaim     = "Claim"
	propertyReason    = "Reason"
)

// shapes is the vocabulary a run reads geometry under.
//
// None of it has a default and none of it ever will
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)): which
// predicate carries a corner's position, which carries a room's height, how
// close two corners have to be to be one corner and how closely a curve has to
// be followed are things the project wrote down.
type shapes struct {
	// position is the predicate a corner's position is claimed under, which is
	// what a boundary is read from.
	position string

	// tolerance is the tolerance corners are judged coincident against and
	// rings judged planar against.
	tolerance string

	// chord is the tolerance a segment standing in for a curve may fall from
	// it by.
	chord string

	// arcCentre and arcThrough are the vocabulary a curved edge is written in.
	// They go together or not at all.
	arcCentre  string
	arcThrough string

	// height is the predicate a node's height is claimed under. A run which
	// names none exports footprints and no bodies.
	height string

	// thickness is the predicate a node drawn as a line is claimed to be thick
	// by, which is what widens its run into something a height can be swept
	// over. A run which names none draws no line at all: a centreline of no
	// width is not a solid, and IFC has nowhere to put one.
	thickness string
}

// drawing reports whether the run asked for geometry at all.
//
// Every flag which is only of use to a drawing counts, and not just the three
// a boundary is read under. A run which named the vocabulary an arc is written
// in and nothing else meant to draw curves; treating it as a spatial export
// would answer that with a file holding no geometry and no reason for it,
// which the caller would have to find out by opening the file.
func (s shapes) drawing() bool {
	return s.position != "" || s.tolerance != "" || s.chord != "" ||
		s.arcCentre != "" || s.arcThrough != ""
}

// complete reports whether it named all three of the flags a boundary is read
// under, which is what decides whether anything is drawn.
func (s shapes) complete() bool {
	return s.position != "" && s.tolerance != "" && s.chord != ""
}

// subcontexts are the views a drawn export declares, and are declared only by
// a run which draws.
//
// A file which carries no shape carries no view to put one in. Writing them
// anyway would put two contexts nothing references into every export of a
// model nobody has drawn, which is two instances a reader has to follow to
// find out they lead nowhere.
func (s shapes) subcontexts() []ifc.Subcontext {
	if !s.complete() {
		return nil
	}

	return []ifc.Subcontext{
		{Identifier: bodyShape, Type: contextType, TargetView: modelView},
		{Identifier: footprintShape, Type: contextType, TargetView: planView},
	}
}

// UnusableBodyVocabularyError is a run which named a predicate a body is built
// from without naming the vocabulary a boundary is read under.
//
// It is a usage error rather than a claim quietly ignored. A body is a boundary
// swept upwards, so there is nothing to extrude until the boundary has been
// read — and a run which asked for solids and got none would have to find that
// out by opening the file.
type UnusableBodyVocabularyError struct {
	// Given are the flags which were given, spelled without their dashes, in
	// the order the usage lists them.
	Given []string

	// Missing are the flags which were not, in the same order.
	Missing []string
}

// Error implements [error].
func (e UnusableBodyVocabularyError) Error() string {
	return fmt.Sprintf(
		"expected the vocabulary a boundary is read under alongside %s, found no %s: a body is a boundary swept "+
			"upwards, so there is nothing to extrude until the outline has been read",
		join(dashed(e.Given)), join(dashed(e.Missing)),
	)
}

// dashed is a list of flag names as a message spells them.
func dashed(flags []string) []string {
	spelled := make([]string, 0, len(flags))
	for _, flag := range flags {
		spelled = append(spelled, "--"+flag)
	}

	return spelled
}

// shapeVocabularyOf reports a run which asked for geometry without saying
// enough to read any.
//
// The three flags a boundary is read under go together or not at all. Naming
// one of them is a run which meant to export shapes, and exporting none anyway
// would answer that with a file which looks complete and holds no geometry;
// naming none is the ordinary spatial export, which is what this command did
// before there was any geometry to write.
func shapeVocabularyOf(drawn shapes) error {
	if drawn.drawing() {
		return vocabularyOf(
			given{flagPosition, drawn.position},
			given{flagTolerance, drawn.tolerance},
			given{flagChord, drawn.chord},
		)
	}

	asked := drawn.bodied()
	if len(asked) == 0 {
		return nil
	}

	return UnusableBodyVocabularyError{
		Given:   asked,
		Missing: []string{flagPosition, flagTolerance, flagChord},
	}
}

// bodied are the flags naming a claim a body is built from which this run gave,
// in the order the usage lists them.
func (s shapes) bodied() []string {
	var asked []string

	if s.height != "" {
		asked = append(asked, flagHeight)
	}
	if s.thickness != "" {
		asked = append(asked, flagThickness)
	}

	return asked
}

// shaped is the geometry of one space: the footprint its boundary states, the
// body a height makes of that footprint, and where the height came from.
//
// The drawing comes back as well as the shapes because it is what attributes
// them: [dfcad.RegionTessellation.Segments] says which edge produced each run
// of the outline, which is what a space boundary's connection geometry is cut
// from. Re-drawing the room to answer that would be drawing it twice and
// hoping the two agreed.
//
// A space which references no loop is drawn as nothing at all rather than as
// an empty shape. That is an answer: a room nobody has bounded has no outline,
// and a representation holding no shapes is a file IFC refuses.
func (e *exporter) shaped(
	node *dfcad.SemanticNode,
) (*ifc.Representation, []ifc.PropertySet, dfcad.RegionTessellation) {
	drawn, diags := e.graph.Topology().TessellateRegion(
		node,
		e.graph.Boundaries(),
		bent(e.graph, node, e.shapes.position, e.shapes.tolerance, e.shapes.arcCentre, e.shapes.arcThrough),
		e.shapes.chord,
	)
	e.diags = append(e.diags, diags...)

	pieces := drawn.Pieces()
	if len(pieces) == 0 {
		return nil, nil, drawn
	}

	elevation, level := e.level(node, rings(pieces), drawn.Region().Tolerance().Value)
	if !level {
		return nil, nil, drawn
	}

	// The outline is what the model states and is written whatever else is
	// known, which is what makes a footprint-only export the ordinary case
	// rather than a degraded one.
	outlines := make([]ifc.Item, 0, len(pieces))
	for _, piece := range pieces {
		outlines = append(outlines, planar(piece.Outer()))
		for _, hole := range piece.Holes() {
			outlines = append(outlines, planar(hole))
		}
	}

	representation := &ifc.Representation{Shapes: []ifc.Shape{{
		Context:    footprintShape,
		Identifier: footprintShape,
		Type:       curveRepresentation,
		Items:      outlines,
	}}}

	swept := heightOf(e.shapes.height)

	height, resolution, resolved := e.length(node, swept, drawn.Unit())
	if !resolved {
		return representation, nil, drawn
	}

	solids := make([]ifc.Item, 0, len(pieces))
	for _, piece := range pieces {
		// The holes are the region's own, which is what carries the even-odd
		// nesting it worked out through to the file rather than leaving a
		// reader to work it out again from a heap of curves.
		inner := make([]ifc.Polyline, 0, len(piece.Holes()))
		for _, hole := range piece.Holes() {
			inner = append(inner, planar(hole))
		}

		solids = append(solids, ifc.ExtrudedArea{
			Profile: ifc.ArbitraryProfile{Outer: planar(piece.Outer()), Inner: inner},
			// The profile is drawn in the xy of this placement, so the
			// elevation the boundary sits at goes here. The footprint beside
			// it is a plan and carries no elevation at all, which is what
			// makes the two different drawings of one room rather than two
			// disagreeing ones.
			Position:  ifc.Placement{Location: ifc.Point{Z: elevation}},
			Direction: ifc.Direction{Z: 1},
			Depth:     height,
		})
	}

	representation.Shapes = append(representation.Shapes, ifc.Shape{
		Context:    bodyShape,
		Identifier: bodyShape,
		Type:       solidRepresentation,
		Items:      solids,
	})

	return representation, e.provenance(node, swept, drawn.Unit(), height, resolution), drawn
}

// modelled is the geometry of a node standing in a spatial element: the outline
// its boundary states and the body the claims about it make of that outline.
//
// Which of the two ways it is drawn is the node's declared geometry and never
// its kind. A room and a countertop are both an area with a height over it, and
// the sweep which makes a solid of either is one operation — so an element
// bounded by a ring is drawn exactly as a space is, and what the kind decides is
// only which entity the shape is written on.
func (e *exporter) modelled(node *dfcad.SemanticNode) (*ifc.Representation, []ifc.PropertySet) {
	if geometry, _ := node.Geometry(); geometry == dfcad.GeometryLine {
		return e.thickened(node)
	}

	representation, properties, _ := e.shaped(node)

	return representation, properties
}

// thickened is the geometry of a node drawn as a line: the run its boundary
// states widened by the thickness claimed of it, and the body a height sweeps
// out of that.
//
// A partition, a railing and a duct run are all this shape. Each is authored as
// a centreline because that is what it is — one run of the model, shared by
// whatever stands either side of it — and each is built as a solid, so the
// thickness is the number which turns the one into the other. There is no
// default for it and there never will be, for the reason there is none for a
// height: how thick a wall is is a fact somebody measured.
//
// A node nothing claims a thickness of is drawn as nothing at all rather than as
// its bare centreline. A curve where a solid belongs is the failure mode this
// whole story is about — a receiving system shows it as nothing, or as a hairline
// nobody can select — and an object with no shape at least says plainly that the
// model does not state one.
func (e *exporter) thickened(node *dfcad.SemanticNode) (*ifc.Representation, []ifc.PropertySet) {
	runs, unit, drew := e.runs(node)
	if !drew {
		return nil, nil
	}

	// A region reads its own tolerance on the way to being assembled and is
	// refused by the engine where the registry declares none. A run is never
	// assembled, so this is the only place that refusal can be made for one —
	// and taking the zero value instead would judge every run level against a
	// tolerance of nothing, which is the engine deciding how close is close
	// enough ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
	tolerance, declared := e.registry.Tolerance(e.shapes.tolerance)
	if !declared {
		e.refuse(node, fmt.Sprintf(
			"expected a declared tolerance to judge the run of %s level, found that the registry declares no "+
				"tolerance %q", node.ID(), e.shapes.tolerance),
			"how far apart two corners may be and still be at one level is a distance the project wrote down; there "+
				"is no default for it, and one compiled in here would be the engine deciding how close is close enough")
		return nil, nil
	}

	elevation, level := e.level(node, runs, tolerance.Value)
	if !level {
		return nil, nil
	}

	claimed := thicknessOf(e.shapes.thickness)

	thickness, resolution, resolved := e.length(node, claimed, unit)
	if !resolved {
		return nil, nil
	}

	plans := widened(runs, thickness)
	if len(plans) == 0 {
		return nil, nil
	}

	outlines := make([]ifc.Item, 0, len(plans))
	for _, plan := range plans {
		outlines = append(outlines, plan)
	}

	representation := &ifc.Representation{Shapes: []ifc.Shape{{
		Context:    footprintShape,
		Identifier: footprintShape,
		Type:       curveRepresentation,
		Items:      outlines,
	}}}

	properties := e.provenance(node, claimed, unit, thickness, resolution)

	swept := heightOf(e.shapes.height)

	height, over, resolved := e.length(node, swept, unit)
	if !resolved {
		return representation, properties
	}

	solids := make([]ifc.Item, 0, len(plans))
	for _, plan := range plans {
		solids = append(solids, ifc.ExtrudedArea{
			Profile: ifc.ArbitraryProfile{Outer: plan},
			// The profile is drawn in the xy of this placement, so the level the
			// run sits at goes here, exactly as a room's does.
			Position:  ifc.Placement{Location: ifc.Point{Z: elevation}},
			Direction: ifc.Direction{Z: 1},
			Depth:     height,
		})
	}

	representation.Shapes = append(representation.Shapes, ifc.Shape{
		Context:    bodyShape,
		Identifier: bodyShape,
		Type:       solidRepresentation,
		Items:      solids,
	})

	return representation, append(properties, e.provenance(node, swept, unit, height, over)...)
}

// runs are the straight runs of a node drawn as a line — one per edge its
// boundary reaches, in the order its loops traverse them — and the unit they
// are in.
//
// It is edge by edge rather than a ring assembled out of them, which is the
// same distinction [dfcad.Graph.Measure] draws for the same reason: assembling
// the edges of a wall into a ring reports the gap where its two ends do not
// meet, and a wall not being a closed cycle is what a line is rather than a
// mistake in one.
//
// Each edge is drawn under the same vocabulary a region is, so a curved railing
// reaches the file as the curve it is rather than as the chord between its
// ends.
func (e *exporter) runs(node *dfcad.SemanticNode) ([][]dfcad.Point, dfcad.Unit, bool) {
	survey := bent(e.graph, node, e.shapes.position, e.shapes.tolerance, e.shapes.arcCentre, e.shapes.arcThrough)

	var out [][]dfcad.Point
	var unit dfcad.Unit

	for edge := range e.graph.Boundaries().Edges(node) {
		drawn, diags := e.graph.Topology().TessellateEdge(edge, survey, e.shapes.chord)
		e.diags = append(e.diags, diags...)

		points := drawn.Points()
		if len(points) < 2 {
			continue
		}

		if unit == "" {
			unit = drawn.Unit()
		}

		out = append(out, points)
	}

	return out, unit, len(out) > 0
}

// widened is the plan of a set of runs: one closed rectangle per straight
// segment of them, as long as the segment and as wide as the thickness claimed.
//
// It is a rectangle per segment rather than one outline drawn around the whole
// run, because the joint where two segments meet is a detail the model does not
// state. Mitring them into a single profile would be this command choosing a
// joint — and choosing one which folds through itself wherever a run turns back
// on itself — whereas two rectangles which overlap at the corner are exactly the
// two runs the model wrote, each carrying the thickness it claims and nothing
// else. It is also what the model's own shape is: a line is edges rather than a
// ring, so a run is already a set of pieces before anything here widens one.
//
// A segment of no length is left out. There is no direction across a run of
// nothing to offset along, and the rectangle built on one would be a profile of
// no area, which IFC has no meaning for.
func widened(runs [][]dfcad.Point, thickness float64) []ifc.Polyline {
	out := make([]ifc.Polyline, 0, len(runs))

	half := thickness / 2

	for _, run := range runs {
		for i := 0; i+1 < len(run); i++ {
			from, to := run[i], run[i+1]

			along := math.Hypot(to[0]-from[0], to[1]-from[1])
			if !(along > 0) {
				continue
			}

			// The offset is across the run in plan, half the thickness either
			// side of it, which is what makes the claimed figure the width of
			// the solid rather than half of it.
			x := -(to[1] - from[1]) / along * half
			y := (to[0] - from[0]) / along * half

			// Wound counter-clockwise, which is the direction IFC's implementer
			// agreements give the outer curve of a profile, and closed by
			// repeating the corner it began at.
			out = append(out, ifc.Polyline{Points: []ifc.Point2D{
				{X: from[0] - x, Y: from[1] - y},
				{X: to[0] - x, Y: to[1] - y},
				{X: to[0] + x, Y: to[1] + y},
				{X: from[0] + x, Y: from[1] + y},
				{X: from[0] - x, Y: from[1] - y},
			}})
		}
	}

	return out
}

// dimension is one of the two lengths a solid is built from, and everything a
// refusal or a property set has to say about it.
//
// The two are one type rather than two nearly identical functions because a
// height and a thickness are the same thing here: a length, claimed of a node
// under a predicate the run named, refused for the same four reasons and
// carried into the file the same way. What differs between them is the words,
// and the words are data.
type dimension struct {
	// predicate is the predicate the run named it under, and is empty for a run
	// which named none.
	predicate string

	// noun is what a diagnostic calls the number: "height", "thickness". plural
	// is that word in the plural, written out rather than suffixed, because
	// "thicknesss" is what suffixing gives.
	noun   string
	plural string

	// purpose is what the number is for, written into a refusal after "to":
	// "sweep its body through", "widen its run by".
	purpose string

	// property is what the property set calls the figure, and set and
	// description are that set's own name and description.
	property    string
	set         string
	description string

	// segment is the piece of a derived identifier which keeps the two sets on
	// one node apart.
	segment string
}

// heightOf is the height claim, under the predicate a run named.
func heightOf(predicate string) dimension {
	return dimension{
		predicate:   predicate,
		noun:        "height",
		plural:      "heights",
		purpose:     "sweep its body through",
		property:    propertyHeight,
		set:         heightProvenance,
		description: heightProvenanceDescription,
		segment:     "height",
	}
}

// thicknessOf is the thickness claim, under the predicate a run named.
func thicknessOf(predicate string) dimension {
	return dimension{
		predicate:   predicate,
		noun:        "thickness",
		plural:      "thicknesses",
		purpose:     "widen its run by",
		property:    propertyThickness,
		set:         thicknessProvenance,
		description: thicknessProvenanceDescription,
		segment:     "thickness",
	}
}

// length is the length claimed of a node under one of those predicates, the
// resolution it came from, and whether there is one.
//
// A run which named no predicate, and a node nothing claims that length of,
// both come back with nothing and no diagnostic. Neither is a fault: the first
// asked for footprints and the second is a thing nobody has measured, and
// refusing an export over either would make the honest 2D file the issue
// impossible to produce.
//
// Everything else is a refusal. A length which is not a distance, one written
// in another unit than the boundary, one which is nought or less, and two
// equally current claims are each a body this cannot make rather than a body it
// should guess at.
func (e *exporter) length(
	node *dfcad.SemanticNode,
	of dimension,
	unit dfcad.Unit,
) (float64, dfcad.Resolution, bool) {
	if of.predicate == "" {
		return 0, dfcad.Resolution{}, false
	}

	// The registry is not asked whether the predicate is strict, because this
	// refuses an ambiguity either way: two current answers to how tall a room
	// is is not a question an export may pick from.
	resolution, _ := e.graph.Claims().Resolve(node.ID(), of.predicate, nil)

	if resolution.Ambiguous() {
		e.refuse(node, fmt.Sprintf(
			"expected at most one current %s claimed of %s to %s, found %d the resolution rule cannot separate",
			of.predicate, node.ID(), of.purpose, len(resolution.Candidates())),
			fmt.Sprintf("supersede the ones which no longer hold, or re-measure; a body built from one of two equally "+
				"current %s is a solid the file gives no reason for", of.plural))
		return 0, resolution, false
	}

	claim, held := current(resolution)
	if !held {
		return 0, resolution, false
	}

	value := claim.Value()
	// Every refusal below is about one claim rather than about the node, so
	// each names that claim: the span points at the value, and the message
	// names the claim's own id where it wrote one, because a room with four
	// heights on it needs to say which of them is the one to go and fix.
	claimed := namedClaim(claim, of.predicate, node.ID())

	length, scalar := value.Scalar()
	if !scalar {
		e.refuseAt(value.Span(), fmt.Sprintf(
			"expected %s to be a distance to %s, found a value of another shape", claimed, of.purpose),
			fmt.Sprintf("a %s is one number; the predicate it is claimed under is declared with a scalar value", of.noun))
		return 0, resolution, false
	}

	if value.Unit() != unit {
		e.refuseAt(value.Span(), fmt.Sprintf(
			"expected %s to be in %s, which is the unit of the frame its boundary is in, found %s",
			claimed, unit, value.Unit()),
			fmt.Sprintf("nothing here converts between units: a frame declares one linear unit, and a %s read off a "+
				"boundary has to be written in the unit the boundary is", of.noun))
		return 0, resolution, false
	}

	// Written as a comparison against zero rather than as `<=` so that a length
	// which is not a number is refused here too, naming the claim, rather than
	// reaching the writer as a depth with no spelling.
	if !(length > 0) {
		e.refuseAt(value.Span(), fmt.Sprintf(
			"expected %s to be a positive distance, found %s",
			claimed, figure(length)+" "+string(value.Unit())),
			fmt.Sprintf("a solid is bounded by positive length measures and there is no body of no %s; a thing nobody "+
				"has measured the %s of carries no claim at all, and is exported without a body", of.noun, of.noun))
		return 0, resolution, false
	}

	return length, resolution, true
}

// namedClaim is one claim as a diagnostic about it names it: the predicate, the
// subject it was claimed of, and the claim's own id where it wrote one.
func namedClaim(claim *dfcad.Claim, predicate string, subject dfcad.ID) string {
	spelled := fmt.Sprintf("the %s of %s", predicate, subject)

	if id, wrote := claim.ID(); wrote {
		spelled += fmt.Sprintf(", claimed as %s", id)
	}

	return spelled
}

// current is the claim a resolution came to, whether or not the resolution
// rule was the thing which chose it.
//
// A predicate nothing rankable was claimed under resolves to no winner and one
// candidate — a height taken off a drawing, with no accuracy stated. That is
// still what the model says about the room, and it is what an author who has
// drawn plans and measured nothing has: dropping it would leave the body out
// of a file which says nothing about why, over a claim the author did write.
//
// So it is used, and the resolution's reason travels into the property set
// beside the body. That is the whole reason the reason is in there: a solid
// swept through an unranked claim reads as `unranked` in the file, and a
// reader can tell it from one swept through a survey without holding the
// model.
//
// More than one candidate is [Resolution.Ambiguous] and is refused above,
// before this is reached.
func current(resolution dfcad.Resolution) (*dfcad.Claim, bool) {
	if claim, held := resolution.Claim(); held {
		return claim, true
	}

	candidates := resolution.Candidates()
	if len(candidates) != 1 {
		return nil, false
	}

	return candidates[0], true
}

// level is the elevation a node's boundary sits at, and whether it sits at one.
//
// A footprint is a plan and a body is that plan swept upwards, so both are
// answers about a boundary lying in one horizontal plane. A boundary which
// does not is not refused for being unusual — it is refused because the plan
// of it would be a projection this command chose, drawn without saying so
// beside a solid swept in a direction nothing in the model asked for.
//
// It takes rings rather than a drawn region because the two shapes it answers
// for are read differently: a room's rings come off its region and a wall's run
// comes off its loops, and whether either lies flat is the same question about
// the same points.
//
// How far apart two corners may be and still be at one level is the tolerance
// the run named, which is the same figure the boundary was assembled under.
func (e *exporter) level(node *dfcad.SemanticNode, drawn [][]dfcad.Point, tolerance float64) (float64, bool) {
	lies := levelOfRings(drawn, tolerance)

	if !lies.level {
		e.refuse(node, fmt.Sprintf(
			"expected the boundary of %s to lie at one level to draw it in plan and sweep it upwards, "+
				"found corners at %s and %s",
			node.ID(), figure(lies.elevation), figure(lies.offending)),
			"a footprint is a plan and a body is that plan swept upwards; a boundary which is not level "+
				"has neither, and the projection which would give it one is not this command's to choose")
		return 0, false
	}

	return lies.elevation, lies.bounded
}

// levels is what a set of rings says about the horizontal plane it lies in.
type levels struct {
	// elevation is the level the first corner sits at.
	elevation float64

	// offending is the first corner found at another level, where there is
	// one.
	offending float64

	// level says whether every corner agreed.
	level bool

	// bounded says whether there was a corner at all, which is what tells a
	// shape lying at one level from no shape.
	bounded bool
}

// levelOf reports whether every corner of a set of pieces lies at one level,
// and which level that is.
//
// It is shared by the two exports because it is a fact about a set of rings
// rather than about either format, and both of them are about to draw a plan:
// a footprint is one, and so is a map. What is not shared is the refusal,
// because what each was going to do with the plan is what makes an unlevel
// boundary worth refusing, and the two commands say that differently.
//
// How far apart two corners may be and still be at one level is the tolerance
// the boundary was assembled under, which is passed in for the same reason.
func levelOf(pieces []dfcad.Piece, tolerance float64) levels {
	return levelOfRings(rings(pieces), tolerance)
}

// rings are the rings a set of pieces is bounded by: the outer ring of each and
// the rings taken out of it, in that order.
func rings(pieces []dfcad.Piece) [][]dfcad.Point {
	out := make([][]dfcad.Point, 0, len(pieces))

	for _, piece := range pieces {
		out = append(out, piece.Outer())
		out = append(out, piece.Holes()...)
	}

	return out
}

// levelOfRings is [levelOf] over rings which came from somewhere other than a
// region: the runs of a node drawn as a line, which are not pieces of anything
// and still have to lie flat before a plan of them means anything.
func levelOfRings(drawn [][]dfcad.Point, tolerance float64) levels {
	// Nothing has disagreed yet, so a set of rings holding no corner at all
	// lies at one level vacuously. What it does not do is lie at an elevation,
	// which is what `bounded` is for and what a caller wanting one reads.
	lies := levels{level: true}

	for _, ring := range drawn {
		for _, at := range ring {
			if !lies.bounded {
				lies.elevation = at[2]
				lies.bounded = true
				continue
			}

			if math.Abs(at[2]-lies.elevation) > tolerance {
				lies.offending = at[2]
				lies.level = false

				return lies
			}
		}
	}

	return lies
}

// provenance is the property set which carries one of the claims behind a body
// into the file.
//
// It is a property set rather than a note in a description because a receiving
// system surfaces one beside the object: a reader looking at a solid can see
// the source, the method, the accuracy and the date behind the figure it was
// built from, and can therefore tell a surveyed height from an assumed one
// without opening the model it came from.
func (e *exporter) provenance(
	node *dfcad.SemanticNode,
	of dimension,
	unit dfcad.Unit,
	length float64,
	resolution dfcad.Resolution,
) []ifc.PropertySet {
	claim, held := current(resolution)
	if !held {
		return nil
	}

	properties := []ifc.Property{
		{Name: propertyPredicate, Value: of.predicate},
		{Name: of.property, Value: figure(length)},
		{Name: propertyUnit, Value: string(unit)},
	}

	written := func(name, value string) {
		if value == "" {
			return
		}
		properties = append(properties, ifc.Property{Name: name, Value: value})
	}

	written(propertySource, claim.Source())
	written(propertyMethod, string(claim.Method()))

	if accuracy, rankable := claim.Accuracy(); rankable {
		written(propertyAccuracy, spellAccuracy(accuracy))
	}

	if date := claim.Date(); !date.IsZero() {
		written(propertyDate, date.Format(claimDateLayout))
	}

	if id, named := claim.ID(); named {
		written(propertyClaim, string(id))
	}

	// The reason is what the resolution rule did rather than what the model
	// says, and it is the difference between one height and the best of four.
	written(propertyReason, string(resolution.Reason()))

	id := node.ID()

	return []ifc.PropertySet{{
		GlobalID:    e.identify(dfcad.ID("ifc/properties/" + of.segment + "/" + string(id))),
		Defines:     e.identify(dfcad.ID("ifc/defines/" + of.segment + "/" + string(id))),
		Name:        of.set,
		Description: of.description,
		Properties:  properties,
	}}
}

// refuse records an error about a node, pointing at where the node was
// written.
func (e *exporter) refuse(node *dfcad.SemanticNode, message, hint string) {
	e.refuseAt(node.Span(), message, hint)
}

// refuseAt records an error pointing at a span of its own.
func (e *exporter) refuseAt(span dfcad.Span, message, hint string) {
	e.diags = append(e.diags, dfcad.Diagnostic{
		Severity: dfcad.SeverityError,
		Span:     span,
		Message:  message,
		Hint:     hint,
	})
}

// planar is one ring of a region as the closed curve IFC wants.
//
// Two things happen here and both are the format's rather than the model's.
// The ring loses its third coordinate, because a footprint is a plan and a
// profile is drawn in the plane it is swept out of; the elevation is carried
// by the placement of the extrusion instead. And the first point is repeated
// as the last, because a region's rings are closed by being rings and IFC's
// are closed by saying so.
func planar(ring []dfcad.Point) ifc.Polyline {
	if len(ring) == 0 {
		return ifc.Polyline{}
	}

	points := make([]ifc.Point2D, 0, len(ring)+1)
	for _, at := range ring {
		points = append(points, ifc.Point2D{X: at[0], Y: at[1]})
	}
	points = append(points, points[0])

	return ifc.Polyline{Points: points}
}

// figure is one number as a property or a diagnostic writes it: the shortest
// decimal which reads back as the same number, and never an exponent.
func figure(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// spellAccuracy is an accuracy as a property carries it: one term per phrase,
// in the order the claim wrote them.
func spellAccuracy(accuracy dfcad.Accuracy) string {
	terms := make([]string, 0, len(accuracy.Terms))

	for _, term := range accuracy.Terms {
		spelled := fmt.Sprintf("%s %s %s", term.Kind, figure(term.Magnitude), term.Unit)
		if term.Source != "" {
			spelled += " shared with " + string(term.Source)
		}
		terms = append(terms, spelled)
	}

	return strings.Join(terms, ", ")
}
