// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package gml writes polygon features as GML 3.2 documents.
//
// It is a file format library and nothing else. It knows GML's geometry
// vocabulary and the XML encoding of it, and it knows nothing about the model
// a caller derived its values from: there is no notion here of a node, an id,
// a claim, a frame, a region or a diagnostic, and no table saying that one
// thing is a plot and another a room. That mapping is the caller's, because it
// is the caller's vocabulary being mapped — a package which held an opinion
// about it would be usable only by the one program whose opinion it held.
//
// The dependency arrow is one way for the same reason. This package imports
// nothing but the standard library, which is checked by a test rather than
// intended, so it could be moved to a repository of its own without an edit to
// its source.
//
// # The coordinate reference system is carried and never read
//
// [Collection.CRS] is written into the document as the srsName of every
// geometry in it, exactly as it was given. This package does not parse it,
// resolve it, look it up or convert anything into or out of it, and there is
// no code path here which could: an identifier is an entry in somebody else's
// register, and reading one means a geodetic dataset and the arithmetic over
// it, which is a product rather than a field of a struct.
//
// So the coordinates a caller supplies are the coordinates written, to the
// last digit. A document this package produced is in the system its srsName
// names because the caller's coordinates already were.
//
// # Easting then northing
//
// [Position] is an easting and a northing, in that order, and a gml:posList is
// written in that order. That is the order the consuming ecosystem assumes for
// a projected system named as an authority and a code, and it is fixed here
// rather than left to a flag because a file whose axis order is a setting is a
// file every reader has to be told about out of band.
//
// # Everything here is deterministic
//
// The same [Collection] produces the same bytes on every run, on every machine
// and at every time of day. Nothing reads a clock, a random source, the
// environment or a map in iteration order: the features are written in the
// order they were given, the properties of each in the order they were given,
// the gml:id of every geometry is derived from the feature's own, and every
// number is written in one canonical form. That is what makes a document
// diffable, cacheable and checkable against a golden fixture.
//
// # What it writes
//
// One [Collection] is one document: an application-namespaced feature
// collection holding one [Feature] per member, each with its properties as
// text elements and its geometry as a gml:MultiSurface. Every feature is a
// multi surface, including a feature of one polygon, so that a reader sees one
// geometry type across the whole layer rather than having to accept two.
//
// Holes are [Polygon.Interior] and survive as gml:interior rings, which is the
// whole reason a polygon is a shape here rather than a list of rings: a
// courtyard is not a second building, and a format which lost the distinction
// would render a plot as a solid block.
package gml
