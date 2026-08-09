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
// # Observations
//
// Field data is not an entity file. An afternoon with a rover produces
// thousands of shots, and a record was produced by an instrument, at a time,
// under conditions — so observations sit outside the entity files, one record
// per line, appended as they are collected and never edited afterwards. Editing
// one in place destroys the thing which made it evidence: after the edit there
// is no way to tell, from the file, that the number ever said anything else.
//
// [LoadObservations] reads a tree of them and validates it against the
// registry; [ParseObservations] reads one file without a registry, which is the
// lexis of a line rather than the soundness of a log. What comes back is an
// [ObservationLog], whose [ObservationLog.Current] is every [Observation] no
// [ObservationRetirement] names. A retirement removes nothing: the retired
// record stays where it was written and still says exactly what the instrument
// said it said.
//
// [ValidateAppendOnly] is the invariant which needs two revisions rather than
// one file — every line the older revision had, present and unchanged at the
// same line number in the newer one. Lines beyond the end of the older revision
// are an append, which is the only legal change. It knows nothing about git:
// where the two byte sequences came from is the caller's question.
//
// Which shots fall inside a region is derived and is never stored.
// [Graph.ObservationsWithin] computes it from the coordinates on the records and
// the boundary of the region, over the whole model's corpus
// ([Graph.AllObservations]), so carving a new bed out of the back garden gives
// it the shots inside it with no edit to an observation file anywhere. What *is*
// stored is the other relationship: an entity naming the file a shot was written
// in is a deliberate statement that this evidence backs this thing, and
// [Membership.Linked] is which of the two a result is.
//
// A record written in another frame is carried across before it is tested, and
// what the transform cost widens the band the boundary is judged against. A shot
// nearer the boundary than the survey can place it is reported rather than
// assigned to a side, which is why [Members.Inside] and [Members.Ambiguous] are
// separate slices and not one carrying a flag.
//
// The ground itself is derived from those shots the same way.
// [Graph.SurfaceWithin] interpolates a [Surface] over the observations inside a
// region — [SurfaceTIN] triangulating them, [SurfaceIDW] weighting them by
// distance — and [Surface.Elevation] answers what the ground does at a point
// nobody stood on. It is a build output: it is kept in the derived cache under
// [BuildDir] and written into no source, so deleting that cache costs time and
// nothing else.
//
// Two things travel with it because they are what make it checkable.
// [Surface.Method] and [Surface.Parameters] are how it was arrived at, since two
// interpolations of one set of points are two different answers, and
// [Surface.Observations] is every record behind it. Its edge is the shots rather
// than the region: a point beyond the convex hull of them is reported as outside
// and never extrapolated to.
//
// Every level comes back with the budget it was propagated from
// ([Elevation.Budget]), under the same two rules a claim's budget follows: the
// shots' own errors in quadrature under the weights the interpolation gave them,
// and a term a whole afternoon shares — a base station, the control a transform
// was fitted to — added linearly and counted once. Where the derivation states a
// [SurfaceDerivation.Roughness], the ground between the shots is charged for too,
// growing with the distance from the nearest of them; where it does not,
// [Elevation.Complete] is false and the figure is a floor.
//
// [Surface.Fall] is the difference of two levels, and it is a first-class answer
// because the subtraction is where the correlation lives: what the two ends share
// is in both by the same amount and so cancels out of the difference. A caller
// combining two levels in quadrature would report a drainage fall as several
// times less certain than it is. docs/surface-accuracy-gate.md is that put to a
// decision with a stated accuracy requirement, and what it found.
//
// docs/observation-file.md is the specification: the line schema field by
// field, what each precision figure means and which uncertainty convention it
// follows, and the two shapes of ambiguous timestamp the format refuses.
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
// another frame stays inside a stated error budget, that a shape stays out of a
// zone it is to keep clear of, and that a measurement written down still matches
// the shape it describes. Each takes its tolerance by name from the registry,
// and each which measures is told which predicate carries a position, because
// neither of those is the engine's to assume. A check the registry declares and
// nothing implements binds, lists and validates exactly as one which does and
// decides nothing, which [Rule.Runs] is how to tell apart — and "nothing decided
// this" is a different answer from "this holds".
//
// The last of them is the one the rest of the engine cannot state on its own.
// The conflict register compares claims with each other and has nothing to say
// about a claim disagreeing with the geometry, because the geometry is not a
// claim — so a node carrying an area whose boundary computes to something else
// loads, formats, resolves and passes every other rule the model states.
// `claim-agrees-with-geometry` is that comparison, and what makes it a rule
// rather than a subtraction is uncertainty: two figures which differ by less
// than their combined uncertainty do not disagree, so the declared discrepancy
// is the floor under the comparison rather than the whole of it. It is written
// on a node, where the shape is the one its boundary encloses or the line it is
// drawn as, and on an edge, where it is the distance between the two corners the
// edge runs between — which is the most directly checkable measurement the
// format can express, and the one an outline never reaches.
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
// references; [Topology.MeasureLoop], [Topology.MeasureEdge] and
// [Topology.MeasureVertex] do the same one, two and three levels down. Nothing
// any of them produces is written back, so a measurement cannot disagree with
// the geometry it describes: move a corner and the answer moves, with no edit
// which says so and no recorded number left behind to go stale (see
// docs/decisions/0009-derived-values-are-never-written-back.md).
//
// A node whose declared geometry is `line` is read as open runs rather than as
// rings. Its loops are the same authoring and are refused for the same four
// things — an edge walked twice, a corner where three edges meet, a shape in two
// pieces, an order the chain is not walked in — and are not asked to close: a
// door, a railing and a wall run each begin somewhere and end somewhere else.
// What one of them measures is a length and a bounding box, with no area and no
// centroid, and [Topology.AssembleRun] is [Topology.Assemble] with that one
// requirement dropped.
//
// A node whose declared geometry is `point` is read from the position claimed of
// the node itself, under the same predicate a corner's position is claimed
// under. A panel, a receptacle, a condenser and a survey monument are each a
// thing whose only interesting geometry is where it is, and a boundary authored
// for one would be dimensions nobody measured. What it measures is what a corner
// measures — the point as the centroid, a box of no extent, no length and no
// area — with the accuracy of the claim which placed it.
//
// [Graph.Measure] is the whole of that in one call, dispatching on whichever
// family an id names, and [Graph.Corners] with [Graph.Located] is what a survey
// for it has to carry. They are here so that a caller asking how big something is does not
// write the dispatch itself: which of the four calls a subject takes, and which
// corners the answer rests on, are properties of the model rather than of the
// question, and a second implementation of them is a second set of answers.
// `dfcad measure` is the same pair on the command line.
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
// Which ring is inside which is a property of the two shapes and not of the
// corner either loop happened to be written down from: the answer is taken over
// the whole of a ring rather than at one point of it, so rotating a loop's edge
// list or walking it the other way round leaves every figure unchanged, and two
// rectangles abutting along a wall union rather than cancel. It is the same
// nesting [Topology.RegionOf] orients its rings by, which is what stops a plan
// and a measurement of one node disagreeing about its area. Two rings which
// cross are neither nested nor beside one another, so the rule has nothing to
// say about them and the pair is named rather than summed.
//
// # Arcs and tessellation
//
// An edge which curves is stored as the curve it is. An [Arc] states where the
// centre of the circle is and one point the wall passes through, and both are
// claims like any other — resolved by the caller under whichever predicates the
// consuming repository registers, and handed in through [Survey.Bend] beside the
// positions. No form, no kind and nothing compiled into this package learns a
// name when an arc arrives.
//
// Everything is then measured from the circle. The length of a curved wall is
// the length of the curve, the area of a ring which bends is its polygon of
// chords plus the circular segment over each arc, the centroid is where that
// combined area is centred and the box is where the curve reaches rather than
// where its two ends do. None of it is a sum over segments, so the sag of a
// curve is in the answer at full precision and no resolution is chosen on a
// caller's behalf.
//
// Tessellation is therefore a thing you ask for. [Topology.TessellateEdge] and
// [Topology.TessellateLoop] take the name of a chord tolerance the registry
// declares and return a [Tessellation] carrying both the segments and the
// tolerance they were drawn to, so what a drawing is good for can be read off
// it. The same arc and the same tolerance give the same points every time. What
// will not happen is a curve becoming segments on the way to an answer nobody
// asked to have drawn: an overlay is computed over straight edges, so
// [Topology.RegionOf] refuses a curved boundary until the survey names the
// tolerance it may be drawn to ([Survey.Chord]), and the [Region] which comes
// back then says what it was drawn to ([Region.ChordTolerance]) and how far the
// drawing fell from the curve ([Region.Deviation]). The one place a measurement
// needs the same is the even-odd nesting of several rings, which is decided at
// the boundary each ring was drawn as and cannot be where a bulge reaches past
// one; the figures it nests are still the arcs', exact whatever the chord was.
//
// The other half of that is [Graph.UnreadArcs], which is what stops a chorded
// boundary being a silent one. A caller who never named the vocabulary an arc is
// written under has no way to tell a model of straight walls from one whose
// walls curve, and reads the second as the first — reporting the chord between
// two corners as though it were the wall, which can be out by a third. What is
// recognisable without the vocabulary is the shape of the claim: an edge has no
// position of its own, so a position claimed on one is a curve or it is nothing.
// That is what it reports, as a warning naming the edge and the predicates to
// name, beside whichever answer was computed.
//
// [Topology.TessellateRegion] is the same for a whole semantic node, and is what
// an export goes through: it draws every ring bounding the node at once and
// returns a [RegionTessellation] whose outer rings and holes are nested by the
// same even-odd rule a measurement uses, wound the same way round, and readable
// as an ordinary [Region]. Doing it a ring at a time is not the same thing,
// because which ring is a hole is a property of the region and not of any ring
// in it. A boundary with nothing curved in it is drawn to itself, unchanged, so
// a caller has one path rather than a curved one and a straight one. An arc
// which the named tolerance would take more segments to follow than anything can
// use is refused, naming the edge, rather than truncated.
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
// A node drawn as a line reads back as a region with no pieces and a boundary:
// it covers nothing, so there is no area for an operation to be computed over,
// and every straight run of it names the edge it was written as, the way it was
// walked and the two corners it runs between — which is the attribution a ring's
// boundary carries and is what a sheet draws the run from. A node drawn as a
// point reads back the same way with a [Region.Location] instead: it covers
// nothing and it is somewhere, which is what a sheet places a symbol at and what
// a map writes as a point feature. Covering nothing is a
// state of that answer and not a refusal, which is what keeps one open run in a
// storey from refusing the plan of the storey or the map of the model.
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
// # Attributing a boundary
//
// A ring of coordinates can be drawn and cannot be attributed. [Region.Segments]
// is the other half of the answer: one [BoundarySegment] per straight run of the
// boundary, saying which ring it belongs to, which two corners it runs between,
// which edge produced it and which way round the loop traversed that edge. With
// it a segment can be named as the party wall, an element backing an edge
// reaches the run that edge produced, and a claim written on an edge carries
// through to the segment it is about — none of which a polygon on its own
// supports, and all of which a consumer would otherwise re-derive by matching
// coordinates.
//
// [BoundarySegment.Origin] is what keeps the attribution honest. A run of a
// region read from the model is the edge itself ([SegmentOriginEdge]); a run of
// a drawn boundary is one chord standing in for part of the arc an edge bends
// along ([SegmentOriginArc]), which names that edge and is not it; and a run an
// operation produced ([SegmentOriginOperation]) names no edge at all, because
// the boundary of an intersection runs partly along each operand and partly
// along where they cross and there is no edge which is the second kind.
//
// # The buildable region
//
// [Topology.BuildableOf] is the derivation those operations exist for: the area
// left inside a boundary once the setback claimed on each of its edges has been
// taken off it, as a [Buildable] carrying the parcel, the region left and the
// [Setback] applied to each edge.
//
// It is derived and never authored, and that matters more here than anywhere
// else in this package. A buildable region written down as a polygon of its own
// is a second statement of where a permanent structure may go, and the day a
// setback claim changes it is the wrong one (see
// docs/decisions/0009-derived-values-are-never-written-back.md).
//
// A setback is a claim on the edge it governs, so different setbacks per edge —
// front, rear, flank — need nothing from the engine but the predicate they were
// written under: which edge is which is project vocabulary and never lands here
// (see docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md). An edge
// with no live claim under that predicate is a diagnostic naming that edge and
// no region, because reading the silence as nought would site a building up
// against a boundary nobody intended it to touch; an edge which really is not
// set back says so, as a claim with a value of nought. Setbacks which meet in
// the middle leave a region covering nothing, reported as a warning rather than
// as a failure, because that is the answer to the question. The [Budget] of the
// answer is accumulated over the position claims and the setback claims
// together, so a control point reached through both is counted once.
//
// # Reading a storey as a plan
//
// [Graph.PlanOf] is the answer an annotated floor plan is drawn from: everything
// a spatial node contains, as rings, each named by the node it came from and
// each carrying the claims written on it and on the edges bounding it. It is a
// [Plan] of [Outline] values, and it is a query rather than an export — nothing
// is written anywhere, and everything in it is read out of the model every time
// it is asked for.
//
// It knows nothing about paper. There is no scale here, no sheet size, no title
// block and nowhere a leader goes, and there never will be: those are decisions
// about a drawing, and the moment this package knows one of them it owns a
// drawing convention that every consuming project disagrees about.
//
// Which claims come back is stated by the caller, as the predicates of an
// [Annotations], with no default. That is the whole of the answer to "is this
// dimension worth drawing" — it is worth drawing if the caller asked for that
// predicate — and it is what keeps a domain judgement out of an engine which
// carries no vocabulary of its own (see
// docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md).
//
// Each claim comes back whole and carries its [Anchor]: an edge with its two
// vertices in the order the edge was authored, or the node with the rings
// bounding it. The pairing is what stops a renderer working out for itself
// which claim belongs to which pair of corners, which is the re-derivation
// [Region.Segments] exists to prevent one layer down.
//
// Nothing is resolved. Two live claims disagreeing about one wall both come
// back under the same anchor, because which of them a sheet prints is a
// decision about the sheet; a retracted claim never comes back at all. The
// [Budget] of the plan is over the position claims behind the rings and not
// over the claims reported, because each of those is a separate statement about
// a separate quantity and carries its own accuracy.
//
// Nothing the subject contains is dropped. A node the plan cannot draw comes
// back in [Plan.Undrawn] rather than being left out — with an [UndrawnReason]
// saying which of the two ways it was undrawable, and with the claims written on
// it — so [Plan.Outlines] and [Plan.Undrawn] account between them for every
// descendant of the subject. A circuit group has no edges and is ordinary; a
// ring which does not close is a defect the diagnostics locate; both are things
// somebody put inside that storey, and omitting either would report a sheet as
// complete which is missing a door. Every undrawable node degrades on its own:
// the rest of the storey is drawn whichever way it was undrawable, and whether
// the *run* succeeded is the separate question the diagnostics answer.
//
// # Derived geometry, and where it is kept
//
// Everything above is computed on demand and none of it is stored in the model.
// That is the whole of why an area cannot contradict the boundary it came from
// (see docs/decisions/0009-derived-values-are-never-written-back.md), and it is
// also why asking the same question of the same model twice pays for it twice.
//
// [Graph.Derive] is the answer to the second half without giving up the first.
// It computes the derived geometry of a whole model — what each thing covers,
// its area, its perimeter, its centroid, its bounding box and which regions it
// lies inside — as a [Footprints], and keeps it in a [Cache] which is a build
// output directory and never a source.
//
// The cache cannot serve a stale answer, because the key is a [Digest] over the
// bytes of the source tree the geometry was derived from. A tree which changed
// anywhere has a different key and misses. There is no invalidation pass, no
// timestamp comparison and no dependency list to get wrong — the key *is* the
// invalidation. The two named decisions a derivation is not otherwise pinned by
// — the tolerance and the position predicate — are in the key as well, because a
// footprint judged against a different tolerance is a different answer.
//
// It is advisory in the strongest sense: deleting it changes what a run reports
// by nothing at all and changes only how long the run takes. Two rules hold that
// up. An entry which does not verify — truncated, corrupt, written by another
// version, written under another key — is discarded and recomputed rather than
// raised, because failing a run over a damaged build output would make a
// disposable artefact load-bearing. And only a derivation which reported no
// diagnostic is stored, so the diagnostics a run reports never depend on what a
// cache happens to hold.
//
// It is bounded by [Cache.Prune], which keeps one digest and removes every
// other: a build which prunes with the digest it just derived against leaves
// exactly one generation, which is the working set. Removing the directory is
// always safe.
//
// Membership here is derived rather than read. A courtyard is inside the floor
// plate around it because of where its corners are, whether or not anybody wrote
// the containment down, and [Footprint.Within] is that answer where
// [Nodes.Within] is the authored one. It is judged only between regions one
// operation could be run over — one frame, one tolerance, one plane — so two
// plates on different storeys are members of neither.
//
// # Artefacts, and the instant they carry
//
// An exported artefact is a build output under the same rule: it is keyed by the
// digest of the tree it was derived from, written beneath the build directory
// and never into the authored tree, and byte-identical for identical input
// (see docs/decisions/0021-an-export-is-a-build-output-keyed-by-its-source-digest.md).
// Nothing may reach an exported byte which is not either covered by the digest
// or named in the artefact's key.
//
// A clock is the usual way that promise is lost. Every exchange format worth
// targeting has a field defined as a creation or a modification time — a part 21
// header's time stamp, a PDF's CreationDate, a container manifest's created —
// and filling one from the clock costs a line and makes every export of an
// unchanged model differ from the last for a reason which has nothing to do with
// the model.
//
// [DerivationEpoch] is the single derivation of that instant, and every command
// whose product is a file reads it from there. It is a function of the source
// tree, which is why it takes the tree's [Digest], and the function is constant:
// 1970-01-01T00:00:00Z, for every tree, including the one nothing could be read
// from. An obviously wrong constant is better than a convincing lie, and the
// provenance the field pretends to carry is carried properly instead, by
// stating the digest through whatever mechanism the target format has for it.
//
// [Epoch] carries the encodings such formats demand — [Epoch.ISO8601],
// [Epoch.STEP], [Epoch.PDF], [Epoch.Seconds] — so that adding a format is a
// method there rather than a layout string in an exporter, which is one
// character away from being a clock reading again.
//
// [ExportDir] is where such an artefact lands, a sibling of [CacheDir] under
// the same build directory. The first exporter to use it is the IFC4 writer in
// github.com/z5labs/dfcad/ifc, which is a package of its own and imports
// nothing of this one: it knows IFC and part 21, the command which drives it
// knows which entity a [Kind] is, and neither knows the other's vocabulary
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
// [DeriveGlobalID] is what joins them — the derivation is this package's and
// the 22-character encoding is IFC's, so this package derives and that one
// encodes.
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
