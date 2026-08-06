// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package gml

// Version is the version of GML the documents this package writes conform to.
//
// It is a constant rather than a field of [Collection] because it is not a
// choice a caller makes: what this package writes is GML 3.2, element by
// element, and a document which announced another version while carrying 3.2's
// elements would be read by a conforming reader as a document of a format it is
// not.
const Version = "3.2.1"

// Namespace is the XML namespace GML 3.2's own elements are written in.
//
// The version in it is the minor version and not the patch: 3.2.0 and 3.2.1
// share a namespace, which is the OGC's own rule and not this package's
// shortening of [Version].
const Namespace = "http://www.opengis.net/gml/3.2"

// Prefix is the namespace prefix GML's own elements are written under.
//
// It is fixed rather than a field because nothing is gained by moving it: an
// XML namespace is identified by its URI and the prefix is a spelling, so a
// document written under any other prefix means exactly the same thing to a
// conforming reader and differs only for whoever is reading it by eye. What is
// gained by fixing it is that the gml:id of every element in a document is
// stated in one spelling.
const Prefix = "gml"

// Dimension is the number of ordinates every position in a document carries,
// written as the srsDimension of each geometry.
//
// It is two, and there is no third. What this package writes is a plan — the
// shape of a thing on the ground, as a map holds it — and an elevation
// alongside it would be a height above a datum this package has no way to name
// and no way to check. A caller with an elevation to carry has a property to
// carry it in.
const Dimension = 2

// Position is one point on the plane, in the order this format's readers
// assume.
//
// Easting first, then northing. See the package documentation for why that
// order is fixed here rather than offered as a choice.
type Position struct {
	// Easting is the first ordinate: the distance east, in the linear unit of
	// the system [Collection.CRS] names.
	Easting float64

	// Northing is the second: the distance north, in the same unit.
	Northing float64
}

// LinearRing is a closed ring of positions bounding an area.
//
// The ring is closed by saying so rather than by being one: its last position
// repeats its first, which is what GML requires and what [Write] checks. A
// caller holding a ring in the other convention — the corners once round, with
// the closure implied — repeats the first position itself, because the
// conversion is one line and a package which did it silently would accept a
// ring which was not closed and had been meant to be.
type LinearRing struct {
	// Positions are the corners of the ring, first repeated as last. Four is
	// the fewest there can be: three corners and the repeat.
	Positions []Position
}

// Polygon is one area: an outer ring, and the rings taken out of it.
type Polygon struct {
	// Exterior is the ring bounding the area.
	Exterior LinearRing

	// Interior are the holes in it, which are written as gml:interior rings
	// and are what stops a courtyard rendering as part of the block around it.
	Interior []LinearRing
}

// Property is one attribute of a feature: a name and the text under it.
//
// The value is text, and only text. A GML document carrying no schema carries
// no types, so what a reader does with the characters between the tags is the
// reader's to decide — and a package which offered a number here would be
// promising a typing it has nowhere to write down.
type Property struct {
	// Name is the element the value is written as, which is the column a
	// reader shows it under. It is written in the collection's application
	// namespace, so it must be a name XML can spell.
	Name string

	// Value is the text under it, escaped on the way out. It is written even
	// when it is empty, because an attribute stated as nothing is different
	// from one nobody stated — a caller which means the second leaves the
	// property out.
	Value string
}

// Feature is one thing in the collection: what it is, and where it is.
type Feature struct {
	// ID is the feature's gml:id, which identifies it inside the document and
	// nowhere else. It is not the identity of whatever the feature came from:
	// that belongs in a [Property], where a reader can see it, sort by it and
	// join on it, and where it is under no obligation to be a name XML can
	// spell.
	//
	// It must be an XML NCName, which is what GML fixes for an identifier, and
	// it must be unique across the document.
	ID string

	// Surfaces are the polygons the feature covers, written as one
	// gml:MultiSurface. A feature covering one area has one of them; a feature
	// covering two disjoint areas has two, and neither is a special case.
	Surfaces []Polygon

	// Properties are its attributes, written in the order they are given.
	Properties []Property
}

// Collection is one document: what the features are called, what system they
// are in, and the features themselves.
type Collection struct {
	// ID is the collection's own gml:id, under the same rules as
	// [Feature.ID].
	ID string

	// Namespace is the XML namespace URI the collection element, the feature
	// elements and every property element are written in. It is the caller's,
	// because what these features are is the caller's vocabulary, and it is
	// written out verbatim.
	Namespace string

	// Prefix is the prefix that namespace is bound to in the document. Like
	// [Prefix] it is a spelling rather than a meaning, but it is a field here
	// because it is the caller's namespace being spelled.
	Prefix string

	// Type is the element name every feature is written as, which is the layer
	// name a reader shows them under.
	Type string

	// CRS is the identifier of the coordinate reference system the positions
	// are in, written as the srsName of every geometry and never read. It is
	// opaque: an authority and a code, a URN, anything a reader of this format
	// resolves.
	//
	// Empty writes no srsName at all, which is a document whose coordinates
	// name no system. That is a thing this package will write, because a
	// caller which has no system to name has nothing it could put here and a
	// refusal would only mean the caller invented one.
	CRS string

	// Features are the members of the collection, written in the order they
	// are given.
	Features []Feature
}
