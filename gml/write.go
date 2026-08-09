// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package gml

import (
	"bufio"
	"encoding/xml"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// The elements a document is made of, named here rather than spelled at each
// use because a typo in one of them is a document which parses and holds
// nothing a reader recognises.
const (
	// collectionElement is the collection, written in the caller's namespace.
	// GML 3.2 deprecated its own gml:FeatureCollection in favour of one an
	// application defines, which is what this is.
	collectionElement = "FeatureCollection"

	// geometryElement is the property each feature's shape is written under,
	// also in the caller's namespace. GML puts a geometry inside a property
	// element rather than directly inside the feature, so this is the property
	// and gml:MultiSurface below is the geometry.
	geometryElement = "geometry"

	// The GML elements themselves.
	boundedByElement     = "boundedBy"
	envelopeElement      = "Envelope"
	lowerCornerElement   = "lowerCorner"
	upperCornerElement   = "upperCorner"
	memberElement        = "featureMember"
	multiSurfaceElement  = "MultiSurface"
	surfaceMemberElement = "surfaceMember"
	polygonElement       = "Polygon"
	exteriorElement      = "exterior"
	interiorElement      = "interior"
	linearRingElement    = "LinearRing"
	posListElement       = "posList"
	multiPointElement    = "MultiPoint"
	pointMemberElement   = "pointMember"
	pointElement         = "Point"
	posElement           = "pos"
)

// The attributes carrying an identity and a coordinate reference system.
const (
	idAttribute           = "id"
	srsNameAttribute      = "srsName"
	srsDimensionAttribute = "srsDimension"
)

// The suffixes the gml:id of a geometry is derived from the feature's own by.
//
// They are derived rather than asked for because they identify a piece of XML
// and nothing else: a caller supplying one would be supplying plumbing, and
// the one property they have to have — being unique across the document — is
// checked here whichever way they were arrived at.
const (
	geometrySuffix = ".geometry"
	surfaceSuffix  = ".surface."
	pointSuffix    = ".point."
)

// ReservedPropertyError is a property whose name is the one this package
// writes a feature's geometry under.
//
// Two sibling elements with one name, one holding text and one holding a
// surface, is a feature a reader reads as having two of whatever it decides
// the name means. The property is the caller's to rename, because the geometry
// is not optional and the name of it is not a field.
type ReservedPropertyError struct {
	// Feature is the id of the feature carrying it.
	Feature string

	// Name is the reserved name, which is the element the geometry is written
	// under.
	Name string
}

// Error implements [error].
func (e ReservedPropertyError) Error() string {
	return "expected the properties of the feature " + strconv.Quote(e.Feature) +
		" to be named something other than " + strconv.Quote(e.Name) + ", which is the element the geometry is written under"
}

// Write serialises a collection as a GML 3.2 document.
//
// Nothing is written until the whole collection has been checked, so a
// refusal leaves the writer untouched rather than half a document followed by
// an error. What is checked is what would make the document unreadable — a
// name XML cannot spell, an id written twice, a ring which does not close, a
// coordinate which is not a number — and nothing else: this package has no
// opinion about whether a shape is the right shape, only about whether it is
// one.
func Write(w io.Writer, collection Collection) error {
	if err := check(collection); err != nil {
		return err
	}

	out := &writer{to: bufio.NewWriter(w)}
	out.document(collection)

	if out.err != nil {
		return out.err
	}

	return out.to.Flush()
}

// check is every refusal, applied in one pass before a byte is written.
func check(collection Collection) error {
	if collection.Namespace == "" {
		return MissingNamespaceError{}
	}

	for _, named := range []struct{ what, name string }{
		{"the prefix the collection's namespace is bound to", collection.Prefix},
		{"the feature type", collection.Type},
		{"the id of the collection", collection.ID},
	} {
		if !ncname(named.name) {
			return NotAnNCNameError{What: named.what, Name: named.name}
		}
	}

	if err := checkPrefix(collection.Prefix); err != nil {
		return err
	}

	// Every identifier the document will carry, whether it was supplied or
	// derived, so that the two cannot collide unnoticed.
	written := map[string]bool{collection.ID: true}

	for _, feature := range collection.Features {
		if err := checkFeature(feature, written); err != nil {
			return err
		}
	}

	return nil
}

// checkPrefix is the caller's prefix against the two it may not be.
//
// The comparison against the reserved family is over lower case, because the
// specification reserves the letters and not the spelling: `XML`, `Xml` and
// `xMl` are each of them reserved, and a document which bound one of those
// would be refused by a parser rather than by this.
func checkPrefix(prefix string) error {
	if prefix == Prefix {
		return ReservedPrefixError{Prefix: prefix, Bound: Namespace}
	}

	if strings.HasPrefix(strings.ToLower(prefix), "xml") {
		return ReservedPrefixError{Prefix: prefix}
	}

	return nil
}

// checkFeature is one feature, and records every identifier it will write.
func checkFeature(feature Feature, written map[string]bool) error {
	if !ncname(feature.ID) {
		return NotAnNCNameError{What: "the id of a feature", Name: feature.ID}
	}

	switch {
	case len(feature.Surfaces) == 0 && len(feature.Points) == 0:
		return NoGeometryError{Feature: feature.ID}
	case len(feature.Surfaces) > 0 && len(feature.Points) > 0:
		return MixedGeometryError{
			Feature:  feature.ID,
			Surfaces: len(feature.Surfaces),
			Points:   len(feature.Points),
		}
	}

	identifiers := []string{feature.ID, feature.ID + geometrySuffix}
	for i := range feature.Surfaces {
		identifiers = append(identifiers, feature.ID+surfaceSuffix+strconv.Itoa(i+1))
	}
	for i := range feature.Points {
		identifiers = append(identifiers, feature.ID+pointSuffix+strconv.Itoa(i+1))
	}

	for _, id := range identifiers {
		if written[id] {
			return DuplicateIDError{ID: id}
		}
		written[id] = true
	}

	for _, property := range feature.Properties {
		if !ncname(property.Name) {
			return NotAnNCNameError{What: "the name of a property", Name: property.Name}
		}
		if property.Name == geometryElement {
			return ReservedPropertyError{Feature: feature.ID, Name: property.Name}
		}
	}

	for _, surface := range feature.Surfaces {
		for _, ring := range rings(surface) {
			if err := checkRing(feature.ID, ring); err != nil {
				return err
			}
		}
	}

	for _, at := range feature.Points {
		if err := checkPosition(feature.ID, at); err != nil {
			return err
		}
	}

	return nil
}

// checkPosition is one position which stands on its own: made of numbers.
//
// A point has no closure and no length to be too short, so being finite is the
// whole of what can be wrong with one — and it is the same refusal a corner of
// a ring gets, because it is the same defect arriving through a different
// field.
func checkPosition(feature string, at Position) error {
	if math.IsNaN(at.Easting) || math.IsInf(at.Easting, 0) ||
		math.IsNaN(at.Northing) || math.IsInf(at.Northing, 0) {
		return NonFiniteCoordinateError{Feature: feature, Easting: at.Easting, Northing: at.Northing}
	}

	return nil
}

// checkRing is one ring: long enough, made of numbers, and closed.
//
// The order is what makes the refusals readable. A ring of two positions is
// reported as too short rather than as one which failed to close, and a ring
// holding a not-a-number is reported as holding one rather than as failing a
// comparison no not-a-number can pass.
func checkRing(feature string, ring LinearRing) error {
	if len(ring.Positions) < 4 {
		return TooFewPositionsError{Feature: feature, Positions: len(ring.Positions)}
	}

	for _, at := range ring.Positions {
		if err := checkPosition(feature, at); err != nil {
			return err
		}
	}

	first, last := ring.Positions[0], ring.Positions[len(ring.Positions)-1]
	if first != last {
		return UnclosedRingError{Feature: feature, First: first, Last: last}
	}

	return nil
}

// rings is a polygon's rings in the order they are written: the exterior, then
// the holes.
func rings(surface Polygon) []LinearRing {
	return append([]LinearRing{surface.Exterior}, surface.Interior...)
}

// ncname reports whether a name is one XML can spell as an element name or an
// identifier.
//
// See [NotAnNCNameError] for why this is narrower than the production in the
// specification.
func ncname(name string) bool {
	if name == "" {
		return false
	}

	for i, r := range name {
		switch {
		case r == '_' || unicode.IsLetter(r):
		case i > 0 && (r == '-' || r == '.' || unicode.IsDigit(r)):
		default:
			return false
		}
	}

	return true
}

// ordinate is one number as this format writes one: the shortest decimal which
// reads back as the same number, and never an exponent.
//
// Never an exponent because a coordinate in a projected system is a figure
// somebody reads — an easting in feet in the millions, an offset in
// millimetres — and because a reader which took `1e+06` for a string would put
// the feature at the origin rather than refusing it.
func ordinate(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// writer is one document being written, and the first thing which went wrong
// with it.
//
// The error is held rather than returned from each call for the reason the
// same shape is used by the writer beside this one: the document has already
// been checked, so the only thing which can go wrong here is the destination,
// and checking each write at its call site would put an error path between
// every two lines of a function whose subject is the layout of a document.
type writer struct {
	to  *bufio.Writer
	err error
}

// write writes text as it stands.
func (out *writer) write(text string) {
	if out.err != nil {
		return
	}

	_, out.err = out.to.WriteString(text)
}

// attribute is one attribute of an element, written in the order it is given.
type attribute struct {
	name  string
	value string
}

// document writes the whole collection.
func (out *writer) document(collection Collection) {
	out.write(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")

	out.open(0, collection.Prefix+":"+collectionElement,
		attribute{"xmlns:" + collection.Prefix, collection.Namespace},
		attribute{"xmlns:" + Prefix, Namespace},
		attribute{Prefix + ":" + idAttribute, collection.ID},
	)

	out.bounds(collection)

	for _, feature := range collection.Features {
		out.open(1, Prefix+":"+memberElement)
		out.feature(collection, feature)
		out.close(1, Prefix+":"+memberElement)
	}

	out.close(0, collection.Prefix+":"+collectionElement)
}

// bounds writes the envelope every position in the collection falls inside.
//
// It is derived here rather than supplied because it is a fact about the
// document: a reader uses it to know where the layer is before reading the
// layer, and one which disagreed with the features under it would send that
// reader to the wrong place. A collection with no features has no envelope and
// is written without one, which is what GML's optional boundedBy is for.
func (out *writer) bounds(collection Collection) {
	lower, upper, bounded := extent(collection)
	if !bounded {
		return
	}

	out.open(1, Prefix+":"+boundedByElement)
	out.open(2, Prefix+":"+envelopeElement, out.reference(collection.CRS)...)
	out.leaf(3, Prefix+":"+lowerCornerElement, ordinate(lower.Easting)+" "+ordinate(lower.Northing))
	out.leaf(3, Prefix+":"+upperCornerElement, ordinate(upper.Easting)+" "+ordinate(upper.Northing))
	out.close(2, Prefix+":"+envelopeElement)
	out.close(1, Prefix+":"+boundedByElement)
}

// feature writes one member of the collection: its properties, then its shape.
func (out *writer) feature(collection Collection, feature Feature) {
	element := collection.Prefix + ":" + collection.Type

	out.open(2, element, attribute{Prefix + ":" + idAttribute, feature.ID})

	for _, property := range feature.Properties {
		out.leaf(3, collection.Prefix+":"+property.Name, property.Value)
	}

	out.open(3, collection.Prefix+":"+geometryElement)
	if len(feature.Points) > 0 {
		out.points(collection, feature)
	} else {
		out.surfaces(collection, feature)
	}
	out.close(3, collection.Prefix+":"+geometryElement)

	out.close(2, element)
}

// points writes a feature's shape as one multi point.
//
// Always a multi point, including for a feature of one position, for the
// reason [writer.surfaces] always writes a multi surface: a layer whose
// features are sometimes a point and sometimes a collection of them is a layer
// a reader has to accept two geometry types for, and the reader which does not
// takes the first and refuses the rest.
func (out *writer) points(collection Collection, feature Feature) {
	attributes := append(
		[]attribute{{Prefix + ":" + idAttribute, feature.ID + geometrySuffix}},
		out.reference(collection.CRS)...,
	)

	out.open(4, Prefix+":"+multiPointElement, attributes...)

	for i, at := range feature.Points {
		out.open(5, Prefix+":"+pointMemberElement)
		out.open(6, Prefix+":"+pointElement,
			attribute{Prefix + ":" + idAttribute, feature.ID + pointSuffix + strconv.Itoa(i+1)})
		out.leaf(7, Prefix+":"+posElement, ordinate(at.Easting)+" "+ordinate(at.Northing))
		out.close(6, Prefix+":"+pointElement)
		out.close(5, Prefix+":"+pointMemberElement)
	}

	out.close(4, Prefix+":"+multiPointElement)
}

// surfaces writes a feature's shape as one multi surface.
//
// Always a multi surface, including for a feature of one polygon. A layer
// whose features are sometimes a polygon and sometimes a collection of them is
// a layer a reader has to accept two geometry types for, and the reader which
// does not accept two takes the first one it meets and refuses the rest.
func (out *writer) surfaces(collection Collection, feature Feature) {
	attributes := append(
		[]attribute{{Prefix + ":" + idAttribute, feature.ID + geometrySuffix}},
		out.reference(collection.CRS)...,
	)

	out.open(4, Prefix+":"+multiSurfaceElement, attributes...)

	for i, surface := range feature.Surfaces {
		out.open(5, Prefix+":"+surfaceMemberElement)
		out.open(6, Prefix+":"+polygonElement,
			attribute{Prefix + ":" + idAttribute, feature.ID + surfaceSuffix + strconv.Itoa(i+1)})

		out.ring(7, exteriorElement, surface.Exterior)
		for _, hole := range surface.Interior {
			out.ring(7, interiorElement, hole)
		}

		out.close(6, Prefix+":"+polygonElement)
		out.close(5, Prefix+":"+surfaceMemberElement)
	}

	out.close(4, Prefix+":"+multiSurfaceElement)
}

// ring writes one ring under the role it plays in its polygon.
func (out *writer) ring(depth int, role string, ring LinearRing) {
	positions := make([]byte, 0, len(ring.Positions)*16)
	for i, at := range ring.Positions {
		if i > 0 {
			positions = append(positions, ' ')
		}
		positions = append(positions, ordinate(at.Easting)...)
		positions = append(positions, ' ')
		positions = append(positions, ordinate(at.Northing)...)
	}

	out.open(depth, Prefix+":"+role)
	out.open(depth+1, Prefix+":"+linearRingElement)
	out.leaf(depth+2, Prefix+":"+posListElement, string(positions))
	out.close(depth+1, Prefix+":"+linearRingElement)
	out.close(depth, Prefix+":"+role)
}

// reference is the coordinate reference system as a geometry carries it.
//
// The identifier is written exactly as it was given and is not looked at. The
// dimension beside it is this package's, because the number of ordinates in
// what it writes is this package's ([Dimension]).
func (out *writer) reference(crs string) []attribute {
	attributes := make([]attribute, 0, 2)

	if crs != "" {
		attributes = append(attributes, attribute{srsNameAttribute, crs})
	}

	return append(attributes, attribute{srsDimensionAttribute, strconv.Itoa(Dimension)})
}

// extent is the lowest and highest corner of every position in the collection,
// and whether there was one.
func extent(collection Collection) (lower, upper Position, bounded bool) {
	reach := func(at Position) {
		if !bounded {
			lower, upper, bounded = at, at, true
			return
		}

		lower.Easting = math.Min(lower.Easting, at.Easting)
		lower.Northing = math.Min(lower.Northing, at.Northing)
		upper.Easting = math.Max(upper.Easting, at.Easting)
		upper.Northing = math.Max(upper.Northing, at.Northing)
	}

	for _, feature := range collection.Features {
		for _, surface := range feature.Surfaces {
			for _, ring := range rings(surface) {
				for _, at := range ring.Positions {
					reach(at)
				}
			}
		}

		// A point is inside the envelope like every other position. A layer of
		// control points whose boundedBy came back empty would send a reader
		// looking for the layer at the origin, which is the one thing the
		// envelope exists to prevent.
		for _, at := range feature.Points {
			reach(at)
		}
	}

	return lower, upper, bounded
}

// open writes a start tag on a line of its own.
func (out *writer) open(depth int, name string, attributes ...attribute) {
	out.indent(depth)
	out.write("<" + name)
	out.attributes(attributes)
	out.write(">\n")
}

// close writes an end tag on a line of its own.
func (out *writer) close(depth int, name string) {
	out.indent(depth)
	out.write("</" + name + ">\n")
}

// leaf writes an element holding text, all on one line.
func (out *writer) leaf(depth int, name, text string) {
	out.indent(depth)
	out.write("<" + name + ">")
	out.text(text)
	out.write("</" + name + ">\n")
}

// attributes writes attributes in the order they were given.
func (out *writer) attributes(attributes []attribute) {
	for _, written := range attributes {
		out.write(" " + written.name + `="`)
		out.text(written.value)
		out.write(`"`)
	}
}

// text writes characters escaped as XML requires.
//
// The escaping is [xml.EscapeText] rather than a table here, and it is the
// same call for an attribute value as for element content: it escapes the
// quote as well as the angle brackets and the ampersand, so one function is
// correct in both places and there is no second one to be wrong.
func (out *writer) text(text string) {
	if out.err != nil {
		return
	}

	out.err = xml.EscapeText(out.to, []byte(text))
}

// indent writes the leading spaces of a line: two per level, always spaces.
func (out *writer) indent(depth int) {
	for range depth {
		out.write("  ")
	}
}
