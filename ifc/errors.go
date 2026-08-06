// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package ifc

import (
	"fmt"
	"strings"
)

// UnknownEntityError reports an [Entity] this package does not know the
// attribute list of.
//
// It is refused rather than written with a guessed attribute list, because an
// entity written with the wrong number of attributes is a file which fails to
// load in every reader there is, and does so a long way from the mapping which
// caused it. The known set travels with the error so that a caller can say
// what it could have written instead.
type UnknownEntityError struct {
	// Entity is what was asked for.
	Entity Entity

	// Position says where in the model it was asked for, in IFC's own terms:
	// "a spatial element", "a product", "a group".
	Position string

	// Known is every entity this package can write in that position, in the
	// order it lists them.
	Known []Entity
}

// Error implements the [error] interface.
func (e UnknownEntityError) Error() string {
	return fmt.Sprintf("expected %s to be one of %s, found %s",
		e.Position, spelled(e.Known), e.Entity)
}

// UnknownMemberError reports a group member no object in the model carries.
//
// A member is named by the identifier of the object it assigns, so a name
// nothing answers to is a reference which would dangle in the file. IFC
// readers vary in how loudly they complain about one, which is the reason this
// is caught here: a file which loads in one tool and not in the next is worse
// than one which was never written.
type UnknownMemberError struct {
	// Group is the identifier of the group which named it.
	Group GlobalID

	// Member is the identifier which named nothing.
	Member GlobalID
}

// Error implements the [error] interface.
func (e UnknownMemberError) Error() string {
	return fmt.Sprintf("expected group %s to assign an object this model writes, found %s, which it does not",
		e.Group, e.Member)
}

// MissingGlobalIDError reports a rooted object written without an identifier.
//
// Every IfcRoot subtype has a GlobalId and the attribute is not optional, so
// there is nothing to write for an object which has none. That includes the
// relationships: an aggregation, a containment and an assignment are rooted
// objects too, and the identifier of each is derived by whoever derived the
// identifiers of the things it joins.
type MissingGlobalIDError struct {
	// Entity is what was being written.
	Entity Entity

	// Of names the object in the caller's terms where there is something to
	// name it by — the identifier of the element a relationship belongs to.
	// It is empty where there is not.
	Of GlobalID
}

// Error implements the [error] interface.
func (e MissingGlobalIDError) Error() string {
	if e.Of == "" {
		return fmt.Sprintf("expected an identifier on every %s, found one with none", e.Entity)
	}
	return fmt.Sprintf("expected an identifier on the %s of %s, found none", e.Entity, e.Of)
}

// DuplicateGlobalIDError reports one identifier written on two objects.
//
// A GlobalId identifies one thing for the life of that thing, so two objects
// carrying the same one is not a file with a duplicate in it — it is two
// things a receiving system will merge into one, silently, and keep merged
// across every later exchange.
type DuplicateGlobalIDError struct {
	// GlobalID is the identifier which was written twice.
	GlobalID GlobalID
}

// Error implements the [error] interface.
func (e DuplicateGlobalIDError) Error() string {
	return fmt.Sprintf("expected every identifier to name one object, found %s on two", e.GlobalID)
}

// EmptyUnitsError reports a model with no unit assignment.
//
// IfcProject requires one and there is no sensible default: a file whose
// lengths are in no stated unit is one every reader guesses at, and they do
// not all guess the same.
type EmptyUnitsError struct{}

// Error implements the [error] interface.
func (EmptyUnitsError) Error() string {
	return "expected at least one unit in the assignment, found none: a file which states no unit is one every reader guesses at"
}

// UnrepresentableRealError reports a floating point value part 21 has no
// spelling for.
//
// There is no NaN and no infinity in the format. Writing the nearest thing to
// one — a very large number, a zero, the three letters — produces a file which
// loads and lies, which is the one outcome worse than a refusal.
type UnrepresentableRealError struct {
	// Value is what could not be written.
	Value float64
}

// Error implements the [error] interface.
func (e UnrepresentableRealError) Error() string {
	return fmt.Sprintf("expected a finite real, found %v, which this format has no spelling for", e.Value)
}

// UnknownSubcontextError reports a [Shape] expressed in a subcontext the
// file's [RepresentationContext] does not declare.
//
// A shape's context is what says which view of the model it belongs to, and a
// reference to a context which is not in the file is one no reader can follow.
// The declared set travels with the error so that a caller can say what it
// could have named instead.
type UnknownSubcontextError struct {
	// Context is the identifier which was named.
	Context string

	// Known is every subcontext identifier the file declares, in the order it
	// declares them. The model's own context is the empty string and is not
	// listed.
	Known []string
}

// Error implements the [error] interface.
func (e UnknownSubcontextError) Error() string {
	if len(e.Known) == 0 {
		return fmt.Sprintf("expected a shape in the model's own context, found one in %s, which this file declares no "+
			"subcontext for", e.Context)
	}
	return fmt.Sprintf("expected a shape in the model's own context or in one of %s, found one in %s",
		strings.Join(e.Known, ", "), e.Context)
}

// UnnamedSubcontextError reports a [Subcontext] with no identifier.
//
// The identifier is how a shape names the subcontext it is written in, so one
// without a name is a view of the model nothing can be put into.
type UnnamedSubcontextError struct{}

// Error implements the [error] interface.
func (UnnamedSubcontextError) Error() string {
	return "expected an identifier on every representation subcontext, found one with none: the identifier is what a " +
		"shape names to be written in it"
}

// DuplicateSubcontextError reports one identifier on two subcontexts.
//
// A shape names the view it belongs to by that identifier, so two views
// answering to one name is a shape which belongs to whichever of them the
// writer happened to reach first.
type DuplicateSubcontextError struct {
	// Identifier is the identifier which was declared twice.
	Identifier string
}

// Error implements the [error] interface.
func (e DuplicateSubcontextError) Error() string {
	return fmt.Sprintf("expected every representation subcontext to carry an identifier of its own, found %s on two",
		e.Identifier)
}

// EmptyRepresentationError reports a [Representation] holding no shapes.
//
// IfcProductDefinitionShape requires at least one, and there is nothing to
// write for an object whose shape definition is empty. A caller with nothing
// to draw leaves the representation nil, which is an object nobody has drawn
// rather than one drawn as nothing.
type EmptyRepresentationError struct {
	// Of is the identifier of the object holding it.
	Of GlobalID
}

// Error implements the [error] interface.
func (e EmptyRepresentationError) Error() string {
	return fmt.Sprintf("expected at least one shape in the representation of %s, found none: an object with nothing to "+
		"draw carries no representation rather than an empty one", e.Of)
}

// EmptyShapeError reports a [Shape] holding no items.
//
// IfcShapeRepresentation requires at least one item, and a shape with none is
// a drawing of nothing which a reader still has to place, name and index.
type EmptyShapeError struct {
	// Identifier is the RepresentationIdentifier of the shape, where it has
	// one.
	Identifier string
}

// Error implements the [error] interface.
func (e EmptyShapeError) Error() string {
	if e.Identifier == "" {
		return "expected at least one item in every shape representation, found one with none"
	}
	return fmt.Sprintf("expected at least one item in the %s shape representation, found none", e.Identifier)
}

// UnknownItemError reports an [Item] this package has no attribute list for.
//
// The set of items is closed by an unexported method, so this is unreachable
// from outside the package. It is here because the set is closed by a method
// and not by the compiler: a geometry added to the package and not added to
// the writer is refused rather than written as nothing.
type UnknownItemError struct {
	// Item is the Go type of what was found.
	Item string
}

// Error implements the [error] interface.
func (e UnknownItemError) Error() string {
	return fmt.Sprintf("expected a geometry this package writes, found %s, which it has no attribute list for", e.Item)
}

// ShortPolylineError reports a [Polyline] through fewer than two points.
//
// A polyline is a run of segments between its points, so one point is a run of
// no segments: a curve of no length which every operation over it divides by.
type ShortPolylineError struct {
	// Points is how many there were.
	Points int
}

// Error implements the [error] interface.
func (e ShortPolylineError) Error() string {
	return fmt.Sprintf("expected at least two points in a polyline, found %d: a run of segments needs two ends",
		e.Points)
}

// OpenCurveError reports a curve of an [ArbitraryProfile] which does not
// close.
//
// The schema requires the curves of an arbitrary profile to be closed, and a
// profile bounded by an open curve encloses no area. Closing it here would be
// this package deciding where the missing segment goes.
type OpenCurveError struct {
	// First and Last are the ends which do not meet.
	First, Last Point2D

	// Inner reports whether it is one of the curves taken out of the profile
	// rather than the one bounding it.
	Inner bool
}

// Error implements the [error] interface.
func (e OpenCurveError) Error() string {
	which := "the curve bounding a profile"
	if e.Inner {
		which = "a curve taken out of a profile"
	}

	return fmt.Sprintf("expected %s to close, found one running from (%v, %v) to (%v, %v)",
		which, e.First.X, e.First.Y, e.Last.X, e.Last.Y)
}

// NonPositiveDepthError reports an [ExtrudedArea] swept no distance, or a
// negative one.
//
// IFC's depth is an IfcPositiveLengthMeasure, so there is nothing to write for
// a solid of no thickness — and a viewer handed one draws nothing, which is
// the outcome a body representation exists to avoid.
type NonPositiveDepthError struct {
	// Depth is what was asked for.
	Depth float64
}

// Error implements the [error] interface.
func (e NonPositiveDepthError) Error() string {
	return fmt.Sprintf("expected a positive depth to sweep a profile through, found %v: there is no solid of no "+
		"thickness", e.Depth)
}

// EmptyPropertySetError reports a [PropertySet] holding no properties.
//
// IfcPropertySet requires at least one, and a set which holds none is a name a
// reader shows with nothing under it.
type EmptyPropertySetError struct {
	// GlobalID is the identifier of the set.
	GlobalID GlobalID
}

// Error implements the [error] interface.
func (e EmptyPropertySetError) Error() string {
	return fmt.Sprintf("expected at least one property in the set %s, found none", e.GlobalID)
}

// UnnamedPropertyError reports a [Property] with no name.
//
// The name is how a reader shows the property and how a caller asks for it
// again, so a property without one is a value nothing can be said about.
type UnnamedPropertyError struct {
	// Set is the identifier of the set which holds it.
	Set GlobalID
}

// Error implements the [error] interface.
func (e UnnamedPropertyError) Error() string {
	return fmt.Sprintf("expected a name on every property of the set %s, found one with none", e.Set)
}

// spelled lists entities the way a message wants them.
func spelled(entities []Entity) string {
	written := make([]string, 0, len(entities))
	for _, entity := range entities {
		written = append(written, string(entity))
	}
	return strings.Join(written, ", ")
}
