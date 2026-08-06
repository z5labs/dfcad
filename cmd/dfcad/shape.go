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

// The flag export takes to name the predicate a space's height is claimed
// under, named here because the usage and the errors which refuse it name it.
const flagHeight = "height"

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

// heightProvenance is the property set the height behind a body is recorded
// in, and the description a reader sees it under.
//
// The name carries the project's own prefix rather than IFC's `Pset_`, which
// is reserved for the sets the specification defines. What is in it is not a
// standard set and saying otherwise would be a claim about the schema.
const (
	heightProvenance            = "dfcad_HeightProvenance"
	heightProvenanceDescription = "The claim the extruded body's height was resolved from, and what is known about it."
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

	// height is the predicate a space's height is claimed under. A run which
	// names none exports footprints and no bodies.
	height string
}

// drawing reports whether the run named the vocabulary a boundary is read
// under, which is what decides whether anything is drawn at all.
func (s shapes) drawing() bool {
	return s.position != "" || s.tolerance != "" || s.chord != ""
}

// complete reports whether it named all three of them.
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

// UnusableHeightVocabularyError is a run which named the predicate a height is
// claimed under without naming the vocabulary a boundary is read under.
//
// It is a usage error rather than a height quietly ignored. A body is the
// footprint swept upwards, so there is nothing to extrude until the boundary
// has been read — and a run which asked for solids and got none would have to
// find that out by opening the file.
type UnusableHeightVocabularyError struct {
	// Missing are the flags which were not given, spelled without their
	// dashes, in the order the usage lists them.
	Missing []string
}

// Error implements [error].
func (e UnusableHeightVocabularyError) Error() string {
	spelled := make([]string, 0, len(e.Missing))
	for _, flag := range e.Missing {
		spelled = append(spelled, "--"+flag)
	}

	return fmt.Sprintf(
		"expected the vocabulary a boundary is read under alongside --%s, found no %s: a body is the footprint swept "+
			"upwards, so there is nothing to extrude until the outline has been read",
		flagHeight, join(spelled),
	)
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

	if drawn.height == "" {
		return nil
	}

	return UnusableHeightVocabularyError{Missing: []string{flagPosition, flagTolerance, flagChord}}
}

// shaped is the geometry of one space: the footprint its boundary states, the
// body a height makes of that footprint, and where the height came from.
//
// A space which references no loop is drawn as nothing at all rather than as
// an empty shape. That is an answer: a room nobody has bounded has no outline,
// and a representation holding no shapes is a file IFC refuses.
func (e *exporter) shaped(node *dfcad.SemanticNode) (*ifc.Representation, []ifc.PropertySet) {
	drawn, diags := e.graph.Topology().TessellateRegion(
		node,
		e.graph.Boundaries(),
		bent(e.graph, node, e.shapes.position, e.shapes.tolerance, e.shapes.arcCentre, e.shapes.arcThrough),
		e.shapes.chord,
	)
	e.diags = append(e.diags, diags...)

	pieces := drawn.Pieces()
	if len(pieces) == 0 {
		return nil, nil
	}

	elevation, level := e.level(node, drawn)
	if !level {
		return nil, nil
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

	height, resolution, resolved := e.height(node, drawn)
	if !resolved {
		return representation, nil
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

	return representation, e.provenance(node, drawn, height, resolution)
}

// height is the height a body is swept through, the resolution it came from,
// and whether there is one.
//
// A run which named no predicate, and a space nothing claims a height of, both
// come back with nothing and no diagnostic. Neither is a fault: the first
// asked for footprints and the second is a room whose height nobody has
// measured, and refusing an export over either would make the honest 2D file
// the issue impossible to produce.
//
// Everything else is a refusal. A height which is not a distance, one written
// in another unit than the boundary, one which is nought or less, and two
// equally current claims are each a body this cannot make rather than a body
// it should guess at.
func (e *exporter) height(
	node *dfcad.SemanticNode,
	drawn dfcad.RegionTessellation,
) (float64, dfcad.Resolution, bool) {
	if e.shapes.height == "" {
		return 0, dfcad.Resolution{}, false
	}

	// The registry is not asked whether the predicate is strict, because this
	// refuses an ambiguity either way: two current answers to how tall a room
	// is is not a question an export may pick from.
	resolution, _ := e.graph.Claims().Resolve(node.ID(), e.shapes.height, nil)

	if resolution.Ambiguous() {
		e.refuse(node, fmt.Sprintf(
			"expected at most one current %s claimed of %s to sweep its body through, found %d the resolution rule "+
				"cannot separate",
			e.shapes.height, node.ID(), len(resolution.Candidates())),
			"supersede the ones which no longer hold, or re-measure; a body swept through one of two equally current "+
				"heights is a solid the file gives no reason for")
		return 0, resolution, false
	}

	claim, held := current(resolution)
	if !held {
		return 0, resolution, false
	}

	value := claim.Value()
	// Every refusal below is about one claim rather than about the room, so
	// each names that claim: the span points at the value, and the message
	// names the claim's own id where it wrote one, because a room with four
	// heights on it needs to say which of them is the one to go and fix.
	claimed := namedClaim(claim, e.shapes.height, node.ID())

	height, scalar := value.Scalar()
	if !scalar {
		e.refuseAt(value.Span(), fmt.Sprintf(
			"expected %s to be a distance to sweep its body through, found a value of another shape", claimed),
			"a height is one number; the predicate it is claimed under is declared with a scalar value")
		return 0, resolution, false
	}

	if value.Unit() != drawn.Unit() {
		e.refuseAt(value.Span(), fmt.Sprintf(
			"expected %s to be in %s, which is the unit of the frame its boundary is in, found %s",
			claimed, drawn.Unit(), value.Unit()),
			"nothing here converts between units: a frame declares one linear unit, and a height swept off a boundary "+
				"has to be written in the unit the boundary is")
		return 0, resolution, false
	}

	// Written as a comparison against zero rather than as `<=` so that a
	// height which is not a number is refused here too, naming the claim,
	// rather than reaching the writer as a depth with no spelling.
	if !(height > 0) {
		e.refuseAt(value.Span(), fmt.Sprintf(
			"expected %s to be a positive distance, found %s",
			claimed, figure(height)+" "+string(value.Unit())),
			"the depth a profile is swept through is a positive length measure and there is no solid of no thickness; "+
				"a space nobody has measured the height of carries no claim at all, and is exported as its footprint")
		return 0, resolution, false
	}

	return height, resolution, true
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

// level is the elevation the space's boundary sits at, and whether it sits at
// one.
//
// A footprint is a plan and a body is that plan swept upwards, so both are
// answers about a boundary lying in one horizontal plane. A boundary which
// does not is not refused for being unusual — it is refused because the plan
// of it would be a projection this command chose, drawn without saying so
// beside a solid swept in a direction nothing in the model asked for.
//
// How far apart two corners may be and still be at one level is the tolerance
// the run named, which is the same figure the boundary was assembled under.
func (e *exporter) level(node *dfcad.SemanticNode, drawn dfcad.RegionTessellation) (float64, bool) {
	tolerance := drawn.Region().Tolerance().Value

	var elevation float64
	first := true

	for _, piece := range drawn.Pieces() {
		rings := append([][]dfcad.Point{piece.Outer()}, piece.Holes()...)

		for _, ring := range rings {
			for _, at := range ring {
				if first {
					elevation = at[2]
					first = false
					continue
				}

				if math.Abs(at[2]-elevation) > tolerance {
					e.refuse(node, fmt.Sprintf(
						"expected the boundary of %s to lie at one level to draw it in plan and sweep it upwards, "+
							"found corners at %s and %s",
						node.ID(), figure(elevation), figure(at[2])),
						"a footprint is a plan and a body is that plan swept upwards; a boundary which is not level "+
							"has neither, and the projection which would give it one is not this command's to choose")
					return 0, false
				}
			}
		}
	}

	return elevation, !first
}

// provenance is the property set which carries the height claim into the file.
//
// It is a property set rather than a note in a description because a receiving
// system surfaces one beside the object: a reader looking at a solid can see
// the source, the method, the accuracy and the date behind the height it was
// swept through, and can therefore tell a surveyed height from an assumed one
// without opening the model it came from.
func (e *exporter) provenance(
	node *dfcad.SemanticNode,
	drawn dfcad.RegionTessellation,
	height float64,
	resolution dfcad.Resolution,
) []ifc.PropertySet {
	claim, held := current(resolution)
	if !held {
		return nil
	}

	properties := []ifc.Property{
		{Name: propertyPredicate, Value: e.shapes.height},
		{Name: propertyHeight, Value: figure(height)},
		{Name: propertyUnit, Value: string(drawn.Unit())},
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
		GlobalID:    e.identify(dfcad.ID("ifc/properties/height/" + id)),
		Defines:     e.identify(dfcad.ID("ifc/defines/height/" + id)),
		Name:        heightProvenance,
		Description: heightProvenanceDescription,
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
