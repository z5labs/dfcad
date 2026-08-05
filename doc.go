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
// [Tx.AddClaim] attaches a value and the evidence for it to a thing, checked
// against what the registry declares the predicate takes. A value is never
// edited: [Tx.Supersede] corrects one by writing the new claim and retracting
// the old in the same change, and [Tx.DeprecateClaim] retracts a claim in
// favour of one already written. A retraction requires a replacement, which is
// what keeps deprecating from being deleting, and the retracted claim keeps
// everything it said — the record of why the number changed is the thing being
// kept. None of the three ever writes over what a claim states.
//
// What none of them refuses but all of them report is a [Notice]: a claim which
// carries no accuracy and so can never win resolution, a claim which disagrees
// with one already written, and a retraction which leaves nothing asserted about
// a subject and predicate at all. Each is a legitimate state for a model to be
// in, and none is a thing to find out about later.
//
// It is the mechanism every authoring command is built on rather than a command
// itself. See docs/decisions/0015-the-cli-is-the-primary-write-path.md for why
// the command line interface writes at all,
// docs/decisions/0016-writes-are-all-or-nothing.md for why a write is refused
// rather than reported, and
// docs/decisions/0002-immutable-id-mutable-label.md for why a label is free to
// change and an id is not.
//
// # Checks
//
// An assertion names a check and supplies its parameters, and contains nothing
// else: no operators, no traversal and no user-defined logic. The checks it may
// name are a closed registry compiled into the engine, which [Checks] lists and
// [LookupCheck] reads one entry of. Each declares what it constrains, the
// parameters it takes and the sort of datum each parameter is, so what an
// assertion constrains can be answered by reading it rather than by evaluating
// it against a model.
//
// [ValidateAssertion] is that reading. It resolves the check name and every
// parameter — including the ones naming a type, a predicate, a frame or a
// tolerance, which are resolved against the model's own registry — at load,
// which is where a misspelled check name and a parameter of the wrong sort are
// reported. A tolerance is always a name from the registry: a numeric literal
// written where one belongs is refused, so how close is close enough stays one
// decision in one place.
//
// Adding a check is a change to the engine — a type implementing [Check] and a
// line in the registered set — rather than something a model file can do. See
// docs/decisions/0011-assertions-are-named-parameterised-checks.md for why the
// registry is closed, and
// docs/decisions/0012-tolerances-are-registry-data.md for why no check carries a
// number.
//
// What the set covers is the structural invariants a model has to hold from its
// first file: that the loops bounding a thing close, that what a node contains
// covers no ground twice and adds up to the whole, that what an edge says
// physically realises it is a node the model holds, that expressing something in
// another frame stays inside a stated error budget, and that a shape stays out
// of a zone it is to keep clear of. Each takes its tolerance by name from the
// registry, and each which measures is told which predicate carries a position,
// because neither of those is the engine's to assume. A check the registry
// declares and nothing implements binds, lists and validates exactly as one
// which does and decides nothing, which [Rule.Runs] is how to tell apart — and
// "nothing decided this" is a different answer from "this holds".
//
// # Assertions
//
// An assertion is a check written on one thing — a node, a vertex, an edge or a
// loop — and it constrains that thing. [SemanticNode.Assertions] and its
// siblings are the ones written on it, as written; [Graph.Assertions] is the
// same list resolved against the check registry, [Graph.AllAssertions] is every
// one in the model and [Graph.CheckAssertions] runs them.
//
// [ResolveAssertions] is the half of validating one that needs the whole model,
// and it runs as part of [LoadGraph]. A check that cannot examine the thing the
// assertion was written on is refused, because a rule with nothing to look at
// passes forever rather than failing. Every id an assertion names is checked to
// resolve. And an assertion that restates a value the claims already carry is
// refused: a claim is where a value is recorded, with the source, the method and
// the date it came from, and repeating it is a second source of truth that goes
// on saying what it says the day the claim is superseded. What that rule does
// and does not catch is [ResolveAssertions].
//
// # Invariants
//
// A type registry entry may declare invariants, which are checks written once on
// the type and applied to every instance of it. [Graph.Invariants] is what bears
// on one instance and [Graph.AllInvariants] is every binding in the model.
// Nothing is stored on an instance, so a rule declared today reaches a node
// written tomorrow without either being touched — which is the point of stating
// it on the type rather than copying it onto a hundred and fifty nodes.
//
// Invariants are not inherited. A node's are the ones its own type declares:
// none descends from the node containing it, from a zone it belongs to or from
// another type, because the type registry declares no hierarchy. What is
// filtered is applicability — a check declares which kinds and which geometry
// forms it can examine, an invariant naming one that could examine no instance
// of the type is refused when the registry loads, and one that can examine some
// of them binds to those.
//
// [Graph.CheckInvariants] runs them and returns a [Violation] for each way an
// instance does not satisfy one. A violation names the instance, the check, the
// parameters it was evaluated with and the registry file and line that declared
// the rule, so a failure of a rule stated once leads back to the one place it is
// written. A check declaring itself and implementing nothing binds and lists
// exactly as one that does and runs nothing, which [InvariantBinding.Runnable]
// is how to tell apart.
//
// # Running them together
//
// A gate does not ask the two questions separately. [Graph.Rules] is every rule
// the model states as one list — each type's invariants bound to its instances,
// then each assertion bound to the thing it is written on — and [Rules.Run] runs
// them and returns a [CheckRun]: how many rules there were, how many ran, how
// many passed and failed, and a [Violation] for each way one was not satisfied.
// [RuleFilter] narrows that to one thing, one type or one check, which is what a
// gate somebody is iterating against runs.
//
// The order is deterministic and is the order the model was read in, so a
// listing of what would run and a report of what did both diff against the last
// run's. A rule whose check declares itself and has no implementation is bound,
// listed and counted apart from the ones that ran, because "this rule holds" and
// "nothing has been written to decide whether it holds" are different answers;
// [Rule.Runs] is how to tell them apart.
//
// # Measuring
//
// How big a room is, is computed from its boundary and is never read from a
// field. [Topology.MeasureRegion] gives the area, the perimeter, the centroid
// and the axis-aligned bounding box of a semantic node from the loops it
// references; [Topology.MeasureLoop] and [Topology.MeasureEdge] do the same one
// and two levels down. Nothing any of them produces is written back, so a
// measurement cannot disagree with the geometry it describes: move a corner and
// the answer moves, with no edit which says so and no recorded number left
// behind to go stale (see docs/decisions/0009-derived-values-are-never-written-back.md).
//
// A [Survey] is what they are computed against — where the vertices are, the
// claims which put them there, the tolerance rings are judged against, and the
// registry. Which predicate carries a position is vocabulary the consuming
// repository owns, so the positions are resolved by the caller and handed in;
// [Survey.Place] takes a [Resolution] and fills both halves at once. Every
// figure comes back in the unit of the frame the thing is declared in, an area
// in the square of it, and with the [Budget] of the position claims it rests on.
//
// Each figure also comes back with whether it could be computed at all. A ring
// which does not close, one which crosses itself, one whose corners are not in
// one plane and one whose corners are collinear each produce a diagnostic and no
// area — never a plausible-looking number, which for the first three is exactly
// what a projection or a signed sum would give. A region bounded by more than
// one ring is measured by nesting, so a courtyard subtracts without anything in
// the model having to declare which loop is the outside one.
//
// # Offsetting and overlaying
//
// How big something is answers one question. Whether it fits answers the other,
// and that one needs the shapes overlaid rather than measured.
// [Topology.RegionOf] reads what a node covers out of the loops bounding it as a
// [Region], and [Region.Buffer], [Region.Union], [Region.Intersect],
// [Region.Difference] and [Region.Containment] are the operations over it. All
// of it runs in process: a setback or a clearance is answerable with no kernel,
// no spatial database and nothing to stand up first.
//
// A result is a set of [Piece]s, each a ring with the rings taken out of it,
// because an operation can leave several pieces which do not touch and can leave
// one with a hole in it. [Region.Buffer] takes the distance either way round —
// outwards for a setback, inwards for a clearance — from one construction, so an
// inward offset which eats the shape returns a region covering nothing rather
// than the inside-out shape offsetting each edge on its own would give.
//
// The same refusals apply as everywhere else in this package, and for the same
// reasons. A ring which crosses itself or encloses nothing is a diagnostic and
// no region. Two regions in different frames are refused rather than combined —
// [Region.In] is the explicit way across, and it carries the accuracy of the
// transform with it — and so are two which do not lie in one plane, because a
// room on the storey above is inside this one seen from above and is not inside
// it. Coincidence and the resolution of a rounded corner are judged against the
// tolerance the registry declares, never against a number compiled in here (see
// docs/decisions/0012-tolerances-are-registry-data.md). Every answer carries the
// [Budget] of the claims behind both operands, so an overlap knows how well the
// corners which decided it were known.
//
// [Region.Containment] is six states rather than a yes and a no, because "not
// inside" covers a region which is nowhere near, one which straddles the
// boundary and one which is inside and touching. Two regions sharing a wall are
// [ContainmentTouching] and never [ContainmentOverlapping]: a party wall has no
// area, and reporting it as an overlap would turn every pair of adjacent rooms
// into a conflict.
//
// # Reviewing a change
//
// Every rule above constrains one revision. Some things are suspicious only as
// changes — a wall which quietly moved, a measurement retracted with nothing in
// its place, an id which is simply gone — and none of them can be seen without
// the revision the change was made against. [Review] is that question: two
// graphs in, a [Finding] out for each change which needs an explanation.
//
// It is kept apart from assertions rather than folded into them, and neither is
// meant to grow into the other. An assertion says the model is wrong and can be
// evaluated on a fresh checkout; a finding says this change needs an
// explanation and cannot be evaluated at all without two revisions. A rule
// which needed the previous revision would be one nothing could run.
//
// What each finding means is a [Policy], which is data rather than a check per
// flag: [DefaultPolicy] fails on an id which disappeared and on a claim
// retracted with nothing behind it, and warns about a boundary which moved,
// because that last one is routinely legitimate. A change which genuinely
// re-surveyed a room is one somebody meant, and [Policy.With] is how to say so
// once rather than by not running the check at all. A finding a policy ignored
// stays in the result, because a check silently switched off is one nobody
// remembers is off.
//
// The two revisions come from git, which [Repository] is the whole of: the
// merge base of the branch and what it is being merged into, the tree at that
// commit, and which commit last changed each file — so a finding names the
// commit which introduced it rather than leaving a reviewer to bisect for it.
// A checkout whose history does not reach the merge base is refused and told
// what to fetch, because git answers a shallow clone with the commit its
// history was cut off at, and a review against that would attribute the whole
// of the branch's ancestry to this change.
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
