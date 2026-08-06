// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package ifc writes IFC4 spatial models as ISO 10303-21 exchange files.
//
// It is a file format library and nothing else. It knows IFC4's entity
// vocabulary and part 21's encoding, and it knows nothing about the model a
// caller derived its values from: there is no notion here of a node, an id, a
// claim, a frame, a tolerance or a diagnostic, and no table saying that one
// thing is an [EntitySpace] and another an [EntityBuilding]. That mapping is
// the caller's, because it is the caller's vocabulary being mapped — a package
// which held an opinion about it would be usable only by the one program whose
// opinion it held.
//
// The dependency arrow is one way for the same reason. This package imports
// nothing but the standard library, which is checked by a test rather than
// intended, so it could be moved to a repository of its own without an edit to
// its source.
//
// # Everything here is deterministic
//
// The same [Model] produces the same bytes on every run, on every machine, in
// every working directory and at every time of day. Nothing reads a clock, a
// random source, the environment or a map in iteration order:
//
//   - Instance numbers come from one fixed traversal of the model, so #14 is
//     the same entity in two runs over the same input.
//   - Shared points and directions are interned in the order that traversal
//     first reaches them, so a coordinate written twice is one instance and
//     the instance it is does not move.
//   - Reals are written in one canonical form, always with a decimal point.
//   - The header's time stamp is a field of [Header], supplied by the caller,
//     rather than a value this package reads from the system clock.
//   - IfcOwnerHistory is never written. It became optional in IFC4, and
//     leaving it out removes the only mandatory clock field in the schema
//     along with the five entity types which would have to exist to fill it.
//
// That is what makes an exported file diffable, cacheable and checkable
// against a golden fixture: two writes of an unchanged model are the same
// bytes, so anything that is not is a change somebody made.
//
// # What it writes
//
// One [Model] is one exchange file: a header, a unit assignment, a geometric
// representation context, and the spatial decomposition beneath one
// [Project] — sites, buildings, storeys and spaces nested by containment,
// products contained in them, and zones grouping any of them. The
// relationships are IFC's own: IfcRelAggregates for the decomposition,
// IfcRelContainedInSpatialStructure for the products, and
// IfcRelAssignsToGroup for the zones.
//
// Every rooted object carries a [GlobalID], which the caller supplies. This
// package neither derives one nor invents one, because a derived identifier is
// a statement about the model it was derived from; what it does hold is
// [EncodeGlobalID], the 22-character encoding IFC fixes, which everybody
// writing this format needs and nobody else does.
package ifc
