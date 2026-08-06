// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package gml

import (
	"fmt"
	"strconv"
)

// Every refusal here is a document which would not have been readable, and
// each is a type rather than a formatted string so that a caller can tell them
// apart without matching prose. The values which made the refusal are fields,
// because the caller mapping its own vocabulary onto this one is the thing
// which has to be fixed, and "a ring did not close" without saying which ring
// or by how far is a message that sends somebody looking.

// NotAnNCNameError is a name which cannot be written as an XML name.
//
// GML identifies elements with an NCName, which is XML's own name production
// with the colon taken out, and there is no escaping for one: a character
// which may not appear in a name may not appear in a name. So a caller whose
// own identifiers are spelled some other way — with colons, slashes or spaces
// in them — maps them into one, and carries the original in a property where
// it is text and is under no such rule.
//
// The check this package applies is XML's letters, digits, `.`, `-` and `_`,
// with a letter or `_` first. It is narrower than the specification, which
// also admits combining characters and extenders; a name it refuses which XML
// would have taken is a name to spell differently rather than a defect to work
// around.
type NotAnNCNameError struct {
	// What the name was for, so that a refusal names the field to go and look
	// at rather than only the value in it.
	What string

	// Name is the name as it was given.
	Name string
}

// Error implements [error].
func (e NotAnNCNameError) Error() string {
	return fmt.Sprintf("expected %s to be an XML name, found %s", e.What, strconv.Quote(e.Name))
}

// ReservedPrefixError is a collection whose namespace was to be bound to a
// prefix which is not the caller's to bind.
//
// Two of these exist. [Prefix] is bound to GML's own namespace in every
// document this package writes, so a caller asking for it would produce two
// bindings of one prefix in one start tag — which is not a document with a
// duplicate in it, it is not a document: an XML parser refuses a repeated
// attribute name outright. And the XML Namespaces specification reserves every
// prefix beginning with the letters `x`, `m` and `l` in any case, so `xml` and
// `xmlns` are spoken for whatever a caller means by them.
//
// It is a refusal rather than a rename because a prefix is the caller's
// spelling of the caller's namespace, and picking a different one on their
// behalf would put a document into the world under a name they did not choose.
type ReservedPrefixError struct {
	// Prefix is the prefix which was asked for.
	Prefix string

	// Bound is the namespace it is already bound to, and is empty where the
	// prefix is one the specification reserves outright rather than one this
	// package has already used.
	Bound string
}

// Error implements [error].
func (e ReservedPrefixError) Error() string {
	if e.Bound == "" {
		return fmt.Sprintf(
			"expected a prefix the XML Namespaces specification does not reserve, found %s",
			strconv.Quote(e.Prefix))
	}

	return fmt.Sprintf(
		"expected a prefix which is not already bound in this document, found %s, which is bound to %s",
		strconv.Quote(e.Prefix), strconv.Quote(e.Bound))
}

// MissingNamespaceError is a collection which named no application namespace.
//
// The collection element, the feature elements and every property element are
// written in it, so there is no document to write without one. It is not
// defaulted to anything: a namespace URI is what says whose vocabulary these
// features are in, and a package which invented one would be answering that
// question on the caller's behalf.
type MissingNamespaceError struct{}

// Error implements [error].
func (MissingNamespaceError) Error() string {
	return "expected the collection to name the namespace its features are written in, found none"
}

// DuplicateIDError is one gml:id written twice in one document.
//
// GML requires an id to be unique across the document, and a reader which
// meets a repeat resolves every reference to it to whichever of the two it saw
// first. That is not a document with a duplicate in it; it is a document two
// readers disagree about.
//
// The ids of the geometries are derived from the id of the feature holding
// them, so a collision between one of those and a feature id somebody supplied
// is refused here too, rather than written and left to be found.
type DuplicateIDError struct {
	// ID is the identifier which was written twice.
	ID string
}

// Error implements [error].
func (e DuplicateIDError) Error() string {
	return fmt.Sprintf("expected each %s:id to be written once, found %s written twice", Prefix, strconv.Quote(e.ID))
}

// NoGeometryError is a feature covering no area at all.
//
// A feature of this collection is a shape on the ground: that is what it is
// for, and what a reader of the document will go looking for. One with no
// surfaces would be a row in the layer which draws nothing, which is worse
// than an absent row — it reads as a thing which is there and covers nothing.
type NoGeometryError struct {
	// Feature is the id of the feature which has none.
	Feature string
}

// Error implements [error].
func (e NoGeometryError) Error() string {
	return fmt.Sprintf("expected the feature %s to cover at least one area, found no surfaces", strconv.Quote(e.Feature))
}

// TooFewPositionsError is a ring with too few positions to bound anything.
//
// Four is the fewest a closed ring can have: three corners, and the first
// repeated as the last. Anything shorter encloses no area, and writing it
// would produce a polygon whose interior is empty in a document which says it
// is a surface.
type TooFewPositionsError struct {
	// Feature is the id of the feature the ring belongs to.
	Feature string

	// Positions is how many there were.
	Positions int
}

// Error implements [error].
func (e TooFewPositionsError) Error() string {
	return fmt.Sprintf(
		"expected every ring of the feature %s to hold at least 4 positions, three corners and the first repeated as "+
			"the last, found one holding %d",
		strconv.Quote(e.Feature), e.Positions)
}

// UnclosedRingError is a ring whose last position is not its first.
//
// A ring which does not close is not a ring, and there is exactly one way it
// could be repaired — repeat the first position — which is the caller's to do
// rather than this package's. Closing it here would take a ring which was
// meant to close somewhere else, silently move its last edge, and produce a
// shape nobody drew.
type UnclosedRingError struct {
	// Feature is the id of the feature the ring belongs to.
	Feature string

	// First and Last are the two positions which were expected to be one.
	First Position
	Last  Position
}

// Error implements [error].
func (e UnclosedRingError) Error() string {
	return fmt.Sprintf(
		"expected every ring of the feature %s to close, found one running from %s to %s",
		strconv.Quote(e.Feature), spell(e.First), spell(e.Last))
}

// NonFiniteCoordinateError is a coordinate which is not a finite number.
//
// A not-a-number and an infinity are how arithmetic reports that it went
// wrong, and they reach a writer from a transform which divided by nothing or
// a value nobody set. There is no spelling for either in this format, so
// writing one would produce a document a reader either rejects or reads as a
// position somewhere unrelated.
type NonFiniteCoordinateError struct {
	// Feature is the id of the feature the position belongs to.
	Feature string

	// Easting and Northing are the position as it was given, so that a caller
	// can see which of the two is the one that is not a number.
	Easting  float64
	Northing float64
}

// Error implements [error].
func (e NonFiniteCoordinateError) Error() string {
	return fmt.Sprintf(
		"expected every position of the feature %s to be a finite number, found (%v %v)",
		strconv.Quote(e.Feature), e.Easting, e.Northing)
}

// spell is one position as a refusal names it, which is the same text the
// document would have carried.
func spell(at Position) string {
	return "(" + ordinate(at.Easting) + " " + ordinate(at.Northing) + ")"
}
