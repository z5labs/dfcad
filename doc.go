// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package dfcad is a data-first CAD engine.
//
// A model is a declarative entity graph held in plain text files. The engine
// loads that graph, answers questions about it with the provenance and error
// budget behind every answer, edits it without losing either, and checks
// assertions over it.
//
// # Scope
//
// The engine is generic. The only vocabulary compiled in is a small closed set
// — node kinds and geometry forms. Everything domain specific arrives as
// registry data supplied by the consuming repository: types, claim predicates,
// frames, id namespaces, tolerances and the rules saying which file a new node
// is written to. A domain concept therefore never needs a change here, which is
// what keeps vocabulary growth reviewable.
//
// There is no database, no daemon and no network transport. The library is
// paired with a command line interface under cmd/dfcad, which is the intended
// entry point for scripts, CI and agents.
//
// # Loading
//
// [LoadGraph] is the entry point. It reads a directory of files into one
// validated [Graph] — the vocabulary the model declares, both families of
// nodes, the claims written on them, the frames those claims measure and the
// boundaries which join the two families — in a single pass which reports every
// problem it finds rather than stopping at the first.
//
// The passes it composes are exported as well, because a caller which wants one
// of them should not have to load six: [LoadRegistry], [LoadNodes],
// [LoadTopology], [LoadClaims], [ResolveFrames] and [ResolveBoundaries]. What
// the graph adds over calling them in order is the checks no single pass can
// make, and not having to know what that order is.
//
// # Writing
//
// [Begin] is the entry point for changing a model. A [Tx] loads the whole
// graph, holds every file of it in memory, applies mutations to those trees and
// — at [Tx.Commit] — interprets the result as though it had already been
// written. A change which would produce a model that does not load is refused
// with the diagnostics the load would have raised, and nothing reaches the
// filesystem; a change which validates is written in canonical form, atomically,
// and all of its files or none of them.
//
// Which file a new node goes in is [Registry.Destination], from the routing
// rules the registry declares, or [Override] where a command was told outright.
// Deciding it is separate from writing it so that the destination can be
// reported before anything is written, and so that a node the rules do not
// place is refused rather than filed somewhere plausible.
//
// The changes themselves are on the transaction: [Tx.AddNode] writes a new
// semantic node with every axis checked against the registry first,
// [Tx.SetLabel] changes what a thing is called and nothing else, and [Tx.Retire]
// records that a thing stopped existing. Retiring is not deleting — the id stays
// in the graph and is never issued again, so a reference written years ago
// resolves either to the thing it always named or to a node which says what
// happened to it.
//
// It is the mechanism every authoring command is built on rather than a command
// itself. See docs/decisions/0015-the-cli-is-the-primary-write-path.md for why
// the command line interface writes at all,
// docs/decisions/0016-writes-are-all-or-nothing.md for why a write is refused
// rather than reported, and
// docs/decisions/0002-immutable-id-mutable-label.md for why a label is free to
// change and an id is not.
//
// # Accuracy
//
// This is the convention every accuracy in the engine is stated under, and it
// is stated here because a consumer combining two numbers has to be able to
// find it in one place.
//
// An accuracy is a standard uncertainty: one standard deviation, k = 1, stated
// in a unit of the same quantity as the claim's own value and written beside
// the magnitude. There is no other storage convention. A figure quoted at any
// other coverage — a half-width bound, a 95% interval, a manufacturer's
// "typical" — is converted to 1σ at the point it enters the model, by whoever
// enters it, and the conversion is part of the claim's provenance rather than
// something the engine guesses. A bare number with no stated meaning is worse
// than none at all, because it gets combined arithmetically anyway and the
// three conventions differ by a factor of two.
//
// Nothing converts between units. Terms written in more than one unit do not
// combine, and a budget holding them says so rather than reconciling them.
//
// Each term of an accuracy is tagged independent or systematic, because whether
// an error is shared is a fact about how the measurement was made and cannot be
// inferred from the number. Independent terms combine in quadrature. A
// systematic term — a georeference fit applied to every indoor fact alike —
// does not partially cancel and does not average away, so it adds linearly, and
// a term reached through two inputs of one computation is counted once.
// [Budget] is where that arithmetic lives, [Uncertainty] is what comes out of
// it, and a figure widened past 1σ always states the coverage factor it was
// widened by. Nothing widened is ever stored.
//
// See docs/decisions/0006-accuracy-is-one-sigma.md for why, and specification
// section 6.6.5 for how an accuracy is written down.
package dfcad
