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

// spelled lists entities the way a message wants them.
func spelled(entities []Entity) string {
	written := make([]string, 0, len(entities))
	for _, entity := range entities {
		written = append(written, string(entity))
	}
	return strings.Join(written, ", ")
}
