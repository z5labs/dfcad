// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// Annotations is which claims a plan reports beside the rings it draws.
//
// It is a list of predicates and nothing else. Whether a measurement is worth
// drawing on a sheet is not a question this engine can answer — it has no
// vocabulary of its own
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)) —
// so the answer is "it is worth drawing if the caller asked for that
// predicate", stated on the invocation, with no default and no default ever.
//
// The consequence is the point: the plan learns nothing about drawing. It does
// not know that a width goes on a leader and a room name goes in the middle,
// because it never decides which of them to report.
type Annotations struct {
	// Predicates are the predicates whose claims are reported, in the order
	// they were named. A predicate named twice is read once.
	Predicates []string
}

// AnchorKind is which family of thing a claim reported on a plan is written on.
//
// There are two because there are two places a fact about a room can be
// written: on the room, or on one of the edges bounding it. A width is written
// on the edge it is the width of; an area and a name are written on the node.
// Which of the two an annotation came from is what a renderer needs in order to
// put it anywhere at all, and it is a fact about the model rather than a
// property of the claim.
type AnchorKind string

// The kinds of anchor.
const (
	// AnchorEdge is a claim written on an edge of a ring.
	AnchorEdge AnchorKind = "edge"

	// AnchorNode is a claim written on the semantic node the ring bounds.
	AnchorNode AnchorKind = "node"
)

// Anchor is what a reported claim is written on.
//
// It carries what the claim is attached to *and* the geometry which locates it,
// so that a consumer never has to go back to the model to place an annotation:
// an edge anchor names its two vertices in the order the edge was authored, and
// a node anchor names the rings bounding the node. A renderer given only the id
// of the edge would have to re-read the edge to find out which two corners a
// dimension runs between, and re-reading is where the two answers drift apart.
//
// The vertices are in authored order and not in the order any ring traverses
// them. Two rings either side of a party wall run through one edge opposite
// ways, and a claim written on the edge is written on the edge rather than on
// either traversal of it; a consumer which needs the traversal direction reads
// it from the region's boundary segments, which is where that question is
// already answered.
//
// The zero Anchor names nothing, and every method below works on it.
type Anchor struct {
	// kind is which family the anchor belongs to.
	kind AnchorKind

	// id is the edge or the node the claim is written on.
	id ID

	// start and end are the edge's two vertices, in the order the edge was
	// written. Both are empty for a node anchor.
	start ID
	end   ID

	// rings are the loops bounding the node, in the order it references them.
	// Empty for an edge anchor.
	rings []ID
}

// Kind returns which family of thing the claim is written on.
func (a Anchor) Kind() AnchorKind { return a.kind }

// ID returns the id of the edge or the node the claim is written on.
func (a Anchor) ID() ID { return a.id }

// Vertices returns the anchor's two vertices in the order the edge was
// authored, and whether it is an edge anchor at all.
func (a Anchor) Vertices() (start, end ID, ok bool) {
	if a.kind != AnchorEdge {
		return "", "", false
	}
	return a.start, a.end, true
}

// Rings returns the loops bounding the node the claim is written on, in the
// order the node references them.
//
// It is empty for an edge anchor, and for a node which references no loop —
// which is what a claim carried by an entry of [Plan.Undrawn] is anchored to
// where the reason is [UndrawnNoBoundary].
func (a Anchor) Rings() []ID { return slices.Clone(a.rings) }

// String renders the anchor as a person reads it: what the claim is written on
// and where that is.
func (a Anchor) String() string {
	switch a.kind {
	case AnchorEdge:
		return fmt.Sprintf("edge %s, %s to %s", a.id, a.start, a.end)
	case AnchorNode:
		if len(a.rings) == 0 {
			return fmt.Sprintf("node %s", a.id)
		}
		return fmt.Sprintf("node %s, ring %s", a.id, strings.Join(spelledIDs(a.rings), " and "))
	}
	return "nothing"
}

// Annotation is one live claim reported on a plan, with what it is written on.
//
// The claim comes back whole rather than as a number and a unit. A dimension
// handed over as a bare figure is exactly what this format exists to refuse
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)), and a
// sheet is the last place it should be refused less firmly: the string a
// renderer prints against a wall is a claim somebody made, with a source, a
// method, a date and an accuracy, and printing it without them is how a design
// estimate comes to look like an as-built survey.
//
// The zero Annotation carries no claim, and every method below works on it.
type Annotation struct {
	// anchor is what the claim is written on.
	anchor Anchor

	// claim is the claim itself.
	claim *Claim
}

// Anchor returns what the claim is written on.
func (a Annotation) Anchor() Anchor { return a.anchor }

// Claim returns the claim, whole.
func (a Annotation) Claim() *Claim { return a.claim }

// Predicate returns the predicate the claim was written under, which is one of
// the predicates the invocation named.
func (a Annotation) Predicate() string {
	if a.claim == nil {
		return ""
	}
	return a.claim.Predicate()
}

// String renders the annotation as a person reads it: the predicate, what it is
// written on and where the claim itself was written.
func (a Annotation) String() string {
	if a.claim == nil {
		return "nothing"
	}
	return fmt.Sprintf("%s on %s, from %s", a.claim.Predicate(), a.anchor, a.claim.Span().Start)
}

// Outline is one contained node drawn as rings, with the claims written on it
// and on the edges bounding it.
//
// The claims travel with the ring rather than in a list beside it, because
// which pair of corners a dimension belongs to is a fact the model holds and a
// consumer would otherwise have to re-derive by matching ids. Re-deriving it is
// the thing this whole answer exists to make unnecessary.
//
// The zero Outline names no node and covers nothing.
type Outline struct {
	// node is the contained node this was read from.
	node *SemanticNode

	// region is the area it covers, as [Topology.RegionOf] reads it, carried
	// into the plan's frame where it was read in another.
	region Region

	// declaredIn is the frame the shape was read in, before it was carried.
	declaredIn ID

	// annotations are the claims reported on it, in anchor order: the node
	// first, then each edge of its boundary in the order its loops traverse
	// them.
	annotations []Annotation
}

// Node returns the contained node the rings were read from.
func (o Outline) Node() *SemanticNode { return o.node }

// DeclaredIn returns the frame the node's shape was read in, which is the frame
// every claim written on it and on its edges is in.
//
// It is [Plan.Frame] for a node declared where its plan is, and another frame
// for one the plan carried: the region comes back in the plan's frame, and the
// claims come back whole, as they were written. A coordinate-valued annotation
// is a coordinate in this frame and not in the plan's, and this is what says
// so.
func (o Outline) DeclaredIn() ID { return o.declaredIn }

// Subject returns the id of that node, which is what names the rings.
func (o Outline) Subject() ID {
	if o.node == nil {
		return ""
	}
	return o.node.ID()
}

// Region returns the area the node covers, with the rings bounding it and the
// edge behind each straight run of them, in [Plan.Frame].
func (o Outline) Region() Region { return o.region }

// Annotations returns the claims reported on it, in anchor order.
func (o Outline) Annotations() []Annotation { return slices.Clone(o.annotations) }

// String renders the outline as a person reads it: what it is, what it covers
// and how much is written on it.
func (o Outline) String() string {
	if o.node == nil {
		return "nothing"
	}

	name := string(o.node.ID())
	if label := o.node.Label(); label != "" {
		name = fmt.Sprintf("%s (%s)", name, label)
	}

	// A node drawn as a point covers nothing, and an area of nought reported
	// for one would read as a room whose ring collapsed rather than as a panel
	// which is where it is.
	shape := decimal(o.region.Area()) + squareSuffix(o.region.Unit())
	if at, located := o.region.Location(); located {
		shape = "at " + pointText(at, o.region.printed())
	}

	return fmt.Sprintf("%s: %s, %s", name, shape, plural(len(o.annotations), "claim"))
}

// UndrawnReason is why a node inside a plan's subject was not drawn.
//
// It is a closed set, because the ways a plan can fail to draw something it was
// asked about are a closed set: the model gives the node no shape at all, it
// gives edges which could not be read, or it gives a node whose shape is a
// position and says nothing about where that position is. Which one it is
// decides who acts on it — the first is ordinary and nobody has to do anything,
// the others are defects whose position the diagnostics carry — and a consumer
// which could not tell them apart would have to decide by reading prose.
type UndrawnReason string

// The reasons a node inside a plan's subject was not drawn.
const (
	// UndrawnNoBoundary is a node which references no loop. A circuit group, a
	// warranty and a lighting schedule are all ordinary and none of them has
	// edges, so this is a shape the model never gave rather than one it gave
	// wrongly.
	UndrawnNoBoundary UndrawnReason = "no-boundary"

	// UndrawnUnreadableBoundary is a node which references loops the run could
	// not read: a ring which does not close, one which crosses itself, corners
	// which are not in one plane, a curve the invocation named no chord
	// tolerance for. Every one of those is reported as a diagnostic naming the
	// loop, the file and the position, which is where an author reads what to
	// change.
	UndrawnUnreadableBoundary UndrawnReason = "unreadable-boundary"

	// UndrawnNoPosition is a node whose geometry form is point and which
	// nothing claims a position of under the predicate the run named. A panel
	// nobody has set out yet is this, and it is a defect rather than an
	// ordinary absence: the node declares that its shape is where it is, and
	// the model then does not say where that is.
	//
	// It is kept apart from UndrawnNoBoundary because the fix is different and
	// so is the urgency. A circuit group never had a shape; a receptacle has
	// one and it is missing, and a sheet drawn without it is a sheet with a
	// device left off.
	UndrawnNoPosition UndrawnReason = "no-position"

	// UndrawnUncarried is a node whose shape was read, in a frame other than
	// the plan's, and could not be carried into the plan's frame: the two
	// frames are not related by any chain of measured transforms, a transform
	// on the way could not be applied, or the plan's frame is in a unit other
	// than the tolerance's. [Region.In] says which, on a diagnostic.
	//
	// It is a defect of the same urgency as an unreadable boundary. The shape is
	// there and is somewhere the plan cannot say where, and a sheet which drew
	// it at its own coordinates would draw it somewhere the model does not put
	// it.
	UndrawnUncarried UndrawnReason = "uncarried"
)

// Description is the reason as a person reads it.
//
// It is a phrase rather than a sentence, so that a renderer can put it after
// whatever it is a reason about. It says which of the two reasons applies and
// nothing more: the detail — which loop, where it was written, how wide the gap
// is — is in the diagnostics, because that is where anything an author has to
// act on belongs and a second copy of it here would be a second thing to keep
// true.
func (r UndrawnReason) Description() string {
	switch r {
	case UndrawnNoBoundary:
		return "references no loop"
	case UndrawnUnreadableBoundary:
		return "its boundary could not be read"
	case UndrawnNoPosition:
		return "has no position stated"
	case UndrawnUncarried:
		return "could not be carried into the plan's frame"
	}
	return "it was not drawn"
}

// Undrawn is one node inside a plan's subject which the plan could not draw,
// with why, and with the claims written on it anyway.
//
// It is the other half of [Plan.Outlines] rather than a footnote to it. Together
// the two account for every node the subject contains, which is the property
// that makes a sheet checkable: a renderer which drew every outline and listed
// every [Undrawn] has drawn or named everything the model puts inside that
// storey, and a caption nobody drew is a line on the sheet rather than an
// absence nothing records.
//
// The claims come back whichever way the node was undrawn, because they are
// authored facts and being unable to draw the thing they are written on is not a
// reason to withhold them. What cannot come back is an edge anchor for a node
// which has no edges — a node with no loop has none, so what it carries is
// exactly its own claims.
//
// The zero Undrawn names nothing, and every method below works on it.
type Undrawn struct {
	// node is the contained node which was not drawn.
	node *SemanticNode

	// reason is why it was not.
	reason UndrawnReason

	// declaredIn is the frame its shape was read in, or the frame the node
	// declares where it has no shape to read.
	declaredIn ID

	// annotations are the claims reported on it, in the same anchor order an
	// outline's are.
	annotations []Annotation
}

// Node returns the contained node which was not drawn.
func (u Undrawn) Node() *SemanticNode { return u.node }

// Subject returns the id of that node.
func (u Undrawn) Subject() ID {
	if u.node == nil {
		return ""
	}
	return u.node.ID()
}

// Reason returns why it was not drawn.
func (u Undrawn) Reason() UndrawnReason { return u.reason }

// DeclaredIn returns the frame the node's shape was read in, which is the frame
// the claims written on it are in. Where the node references no loop there was
// no shape to read, and it is the frame the node declares, if any.
func (u Undrawn) DeclaredIn() ID { return u.declaredIn }

// Annotations returns the claims reported on it, in anchor order.
func (u Undrawn) Annotations() []Annotation { return slices.Clone(u.annotations) }

// String renders it as a person reads it: what it is, why it is not on the sheet
// and how much is written on it.
func (u Undrawn) String() string {
	if u.node == nil {
		return "nothing"
	}

	name := string(u.node.ID())
	if label := u.node.Label(); label != "" {
		name = fmt.Sprintf("%s (%s)", name, label)
	}

	return fmt.Sprintf("%s: not drawn, %s, %s",
		name, u.reason.Description(), plural(len(u.annotations), "claim"),
	)
}

// Plan is a spatial node's contents drawn as rings, with the claims the
// invocation asked for anchored to what they are written on.
//
// It is a query and not an export. Everything in it is read out of the model
// every time it is asked for — the rings out of the corners and the edges, the
// claims out of the files they were written in — so a plan cannot disagree with
// the model it was read from, and there is no artefact of it to go stale
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// It knows nothing about paper. There is no scale here, no sheet size, no title
// block, no leader and no text height, and there never will be: those are
// decisions about a drawing, and this is the answer a drawing is made from. The
// boundary is deliberate — the moment the engine knows where a leader goes, it
// owns a drawing convention, and drawing conventions are the thing every
// consuming project disagrees about.
//
// Nothing here is resolved. Where two live claims compete under one predicate
// on one anchor, both come back: which of them a sheet prints is a decision
// about the sheet, and a query which picked one would make that decision
// invisibly and in the wrong place.
//
// The zero Plan draws nothing, and every method below works on it.
type Plan struct {
	// subject is the id of the node whose contents were drawn.
	subject ID

	// frame is the coordinate frame every coordinate of the plan is in, and
	// unit its linear unit. It is the frame the subject declares, or the frame
	// of its first loop where it declares none, or the root frame where it has
	// neither.
	frame ID
	unit  Unit

	// tolerance is what corners were judged coincident against.
	tolerance Tolerance

	// outlines are the contained nodes which were drawn, in id order.
	outlines []Outline

	// undrawn are the contained nodes which were not, in id order, each with
	// why and with what is written on it.
	//
	// It is beside the outlines rather than derived from their absence, because
	// the absence is exactly what a consumer cannot see: a node dropped from the
	// answer looks identical to a node the model does not hold.
	undrawn []Undrawn

	// chord is the declared chord tolerance the rings which bend were drawn
	// to, and deviation how far the worst of those segments falls from the
	// curve it stands in for.
	//
	// One tolerance covers the whole sheet, because every ring is read from one
	// survey: two rooms sharing a curved party wall drawn to two resolutions
	// would leave a gap down the middle of it which nobody authored.
	chord     Tolerance
	deviation float64

	// budget is the accumulated accuracy of the position claims behind every
	// ring drawn.
	budget Budget
}

// Subject returns the id of the node whose contents were drawn.
func (p Plan) Subject() ID { return p.subject }

// Frame returns the coordinate frame every coordinate of the plan is in.
//
// It is the frame the subject declares. Where the subject declares none it is
// the frame of the subject's first boundary loop, which is how
// [Topology.RegionOf] reads a node's frame, and where it has neither it is the
// root frame ([Frames.Root]). An outline read in any other frame is carried
// into this one, and [Outline.DeclaredIn] names the frame it came from.
func (p Plan) Frame() ID { return p.frame }

// Unit returns the linear unit of that frame, which every coordinate in the
// rings is in and every area in the square of.
func (p Plan) Unit() Unit { return p.unit }

// Tolerance returns what corners were judged coincident against.
func (p Plan) Tolerance() Tolerance { return p.tolerance }

// ChordTolerance returns the declared tolerance the rings which bend were drawn
// to, and whether any of them bent at all.
//
// A ring is a list of points, so a curve on a sheet is always an approximation
// of the wall; this is what says how good an approximation, and its absence is
// what says there was nothing to approximate. A plan which does not carry it and
// whose subject has curved walls is a sheet drawn straight through them, which
// [Graph.UnreadArcs] is what reports.
func (p Plan) ChordTolerance() (Tolerance, bool) { return p.chord, p.chord.Name != "" }

// Deviation returns how far the worst segment of that drawing falls from the
// curve it stands in for, in [Plan.Unit].
//
// It is what was achieved rather than what was asked for, and is always within
// [Plan.ChordTolerance].
func (p Plan) Deviation() float64 { return p.deviation }

// Outlines returns the contained nodes which were drawn, in id order.
//
// The order is by id rather than by the order the walk read them, so that it is
// a property of what the model says rather than of which file each node happens
// to be written in. Moving a room between files leaves the answer identical.
func (p Plan) Outlines() []Outline { return slices.Clone(p.outlines) }

// Undrawn returns the contained nodes which were not drawn, in id order, each
// with why it was not and with the claims written on it.
//
// It and [Plan.Outlines] partition everything the subject contains: every node
// the walk reached is in exactly one of them, so a consumer which reads both has
// seen the whole of what the model puts inside that storey. Nothing is dropped,
// which is the property this exists for — a node omitted from an answer reads
// identically to a node the model does not hold, and a doorway missing from a
// sheet is not a difference anybody notices downstream.
func (p Plan) Undrawn() []Undrawn { return slices.Clone(p.undrawn) }

// Empty reports whether nothing the subject contains was drawn.
//
// It is a state of the answer and not a failure. A storey nobody has outlined
// yet contains nothing drawable, which is the truthful answer to what it looks
// like in plan. It says nothing about [Plan.Undrawn], which may well hold
// entries for a plan which is empty: a storey holding one room whose ring does
// not close draws nothing and has something to say about why.
func (p Plan) Empty() bool { return len(p.outlines) == 0 }

// Annotations returns how many claims the plan reports in total.
//
// The claims of an undrawn node are counted, because they were reported. They
// are facts somebody authored about something the subject contains, and the plan
// hands them back whether or not it could draw the thing they are written on.
func (p Plan) Annotations() int {
	var count int
	for _, outline := range p.outlines {
		count += len(outline.annotations)
	}
	for _, undrawn := range p.undrawn {
		count += len(undrawn.annotations)
	}
	return count
}

// Budget returns the accumulated accuracy of the position claims which put
// every drawn corner where it is.
//
// It is the accuracy of the *geometry* and not of the annotations. Each claim
// reported carries its own accuracy, because each is a separate statement about
// a separate quantity and combining a room's area with a wall's fire rating
// would produce a figure of nothing at all
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)). What this answers is
// the question a sheet has to carry: how well is the line I am drawing known.
func (p Plan) Budget() Budget { return p.budget }

// String renders the plan as a person reads it.
func (p Plan) String() string {
	if p.subject == "" {
		return "nothing was planned"
	}

	if p.Empty() && len(p.undrawn) == 0 {
		return fmt.Sprintf("%s contains nothing with an outline", p.subject)
	}

	summary := fmt.Sprintf("%s: %s, %s",
		p.subject,
		plural(len(p.outlines), "outline"),
		plural(p.Annotations(), "claim"),
	)

	// Said only where there is something to say, so that a storey every node of
	// which drew reads exactly as it always did.
	if len(p.undrawn) > 0 {
		summary += fmt.Sprintf(", %d not drawn", len(p.undrawn))
	}

	return summary
}

// Report renders the plan with each outline under it and then everything it
// could not draw, which is the detail somebody reading a terminal asked for
// rather than the summary.
//
// What was not drawn comes last rather than in id order among the outlines,
// because it is a different answer: the outlines are the sheet, and this is the
// list of what is missing from it.
func (p Plan) Report() string {
	var out strings.Builder

	out.WriteString(p.String())
	for _, outline := range p.outlines {
		out.WriteString("\n  ")
		out.WriteString(outline.String())
		for _, annotation := range outline.annotations {
			out.WriteString("\n    ")
			out.WriteString(annotation.String())
		}
	}

	for _, undrawn := range p.undrawn {
		out.WriteString("\n  ")
		out.WriteString(undrawn.String())
		for _, annotation := range undrawn.annotations {
			out.WriteString("\n    ")
			out.WriteString(annotation.String())
		}
	}

	return out.String()
}

// PlanOf reads everything a spatial node contains as rings, with the claims
// written on those rings and on the nodes they bound.
//
// It is the answer a floor plan is drawn from: the outlines the model already
// holds, and the statements already written on the edges bounding them, in one
// answer rather than in one call per room and one more per wall. Nothing is
// computed which is not already implied by the model, and nothing is written
// anywhere.
//
// Which nodes are drawn is every descendant of the subject which references at
// least one loop the run could read, however deep — a room inside a storey and
// an alcove inside that room are both places somebody draws. The subject itself
// is not drawn; the question is what is in it.
//
// A descendant whose geometry form is `point` is drawn too, from the position
// claimed of the node itself: its outline covers nothing and carries a
// [Region.Location], which is where a sheet places a symbol. A panel, a
// receptacle, a condenser and a survey monument are each that shape — a thing
// whose only interesting geometry is where it is — and a plan which reported
// them as having no boundary would leave every device on a floor off the sheet
// while the model held all of them.
//
// **Nothing the subject contains is dropped.** Every descendant which was not
// drawn comes back in [Plan.Undrawn], named, with an [UndrawnReason] saying
// which of the ways it was undrawable and with the claims written on it. A
// circuit group has no edges and is ordinary; a room whose ring does not close
// is a defect; both are things somebody put inside this storey, and a query
// which answered by omitting them would be reporting a sheet as complete which
// is missing a door. That failure has no downstream symptom at all — the sheet
// renders, looks right, and the doorway is simply not on it — which is why the
// engine says so here rather than leaving it to be noticed.
//
// Every ring is read with [Topology.RegionOf], so everything which refuses a
// region refuses one here — a ring which does not close, one which crosses
// itself, corners which are not in one plane, a tolerance the registry does not
// declare in the frame's unit, a curve no chord tolerance was named for.
//
// **An undrawable node degrades per node and never refuses the storey**, whether
// it is undrawable for a reason an author can fix or for no reason at all. Its
// diagnostics are collected, it comes back under [Plan.Undrawn] rather than as an
// outline covering nothing, and every other node is still drawn. That is one
// rule for every case rather than one per shape of defect: a ring which crosses
// itself and a ring which does not close are two spellings of the same mistake,
// and behaving differently between them would make which of the two a model
// happens to hold decide how much of the sheet comes back. Whether the *run*
// succeeded is the separate question the diagnostics answer — an unreadable
// boundary is an error and a caller which treats one as a refusal still refuses
// — and separating the two is what lets a caller draw the seven rooms it has
// while it fixes the eighth.
//
// The claims reported are the live ones under each predicate [Annotations]
// names, on the node itself and on each edge of its boundary. Nothing is
// resolved: two live claims disagreeing about one wall both come back, marked
// with the same anchor, and which one a sheet prints is the caller's decision.
// A retracted claim is never reported — resolution never considers one, and a
// sheet printing a value somebody has withdrawn is the failure this refuses.
//
// It is [Graph.PlanOfSelected] with no selection: every descendant is asked
// about.
func (g *Graph) PlanOf(node *SemanticNode, survey Survey, annotations Annotations) (Plan, []Diagnostic) {
	return g.PlanOfSelected(node, survey, annotations, Selection{})
}

// Selection is which of a subject's descendants a plan is asked about: those
// declaring one of Kinds, and one of Types.
//
// Within one field any of its values, across the two fields both, and an empty
// field is no narrowing at all — so the zero Selection selects every node. That
// is the rule every filter of the command line follows, and it is any rather
// than every within a field because a node declares one kind and one type: a
// selection demanding two of either would select nothing by construction.
//
// It narrows what a plan reports and never what it walks. A room three levels
// below the subject is selected by Types {"Office"} whether or not anything
// between it and the subject is an office, because the question is which rooms
// to draw, and a walk which stopped at the first node the selection refused
// would answer "no rooms" for a model whose every room is inside a storey.
//
// A kind or type nobody declared is not refused here: it selects nothing,
// exactly as a declared one nothing instantiates does. Telling those two apart is
// a question about the registry which a caller asks before asking for a plan —
// the command line refuses such a value as a usage error — and the answer to
// "which of these nodes is an X" is the same whichever of the two X is.
type Selection struct {
	// Kinds are the kinds a selected node declares one of. Empty selects every
	// kind.
	Kinds []Kind

	// Types are the types a selected node declares one of. Empty selects every
	// type.
	Types []string
}

// Selects reports whether node is one the selection asks about. A nil node is
// never selected.
func (s Selection) Selects(node *SemanticNode) bool {
	if node == nil {
		return false
	}
	if len(s.Kinds) > 0 && !slices.Contains(s.Kinds, node.Kind()) {
		return false
	}
	if len(s.Types) > 0 && !slices.Contains(s.Types, node.Type()) {
		return false
	}
	return true
}

// PlanOfSelected is [Graph.PlanOf] asked about only the descendants selection
// selects.
//
// Every rule of [Graph.PlanOf] holds over what is selected. [Plan.Outlines] and
// [Plan.Undrawn] account between them for every selected descendant, and a
// descendant the selection does not select is in neither: it was not asked
// about, which is a different thing from being left off a sheet it was asked
// for. A selected node the plan cannot draw is still named under
// [Plan.Undrawn], with its reason and its claims, exactly as it would be
// unselected.
//
// Everything a plan computes from the rings it drew is over the selected nodes
// alone: the chord tolerance and the deviation, and [Plan.Budget]. A sheet of
// the meeting rooms carries the accuracy of the meeting rooms' corners and not
// of every corner in the storey. What a selected node's outline says — its
// region and its annotations — is exactly what it says in the unselected plan:
// the selection decides which rooms come back and never how a room is drawn.
//
// A selection which selects nothing is an empty plan, not a refusal.
func (g *Graph) PlanOfSelected(
	node *SemanticNode,
	survey Survey,
	annotations Annotations,
	selection Selection,
) (Plan, []Diagnostic) {
	if g == nil || node == nil {
		return Plan{}, nil
	}

	frame := g.planFrame(node)

	plan := Plan{subject: node.ID(), frame: frame, unit: frameUnit(survey.Registry, frame)}
	plan.tolerance, _ = survey.Registry.Tolerance(survey.Tolerance)

	predicates := distinct(annotations.Predicates)

	var diags []Diagnostic

	for _, contained := range g.contained(node) {
		// Narrowed here and not in the walk: a room is reached through a storey
		// the selection may not select, and it is what is reported which the
		// selection decides.
		if !selection.Selects(contained) {
			continue
		}

		// A node the model gives no edges raises no diagnostic. A circuit group
		// covers no area and a warranty has no shape, and a query which
		// complained about one would complain about most models; whether a node
		// which declares a shape ought to reference a boundary is a question
		// about the model, which is a check's to ask and not a query's. What is
		// wrong today is not that it goes unreported to whoever wrote the file —
		// it is that it goes unreported to whoever draws the sheet, and this is
		// where that is fixed.
		// A node drawn as a point references no loop and never will, so the
		// boundary test is not the question to ask of one. What places it is a
		// claim written on itself, and the region read below carries it.
		placed := drawnAsPoint(contained)

		if !placed && !g.outlined(contained) {
			declared, _ := contained.Frame()

			plan.undrawn = append(plan.undrawn, Undrawn{
				node:        contained,
				reason:      UndrawnNoBoundary,
				declaredIn:  declared,
				annotations: g.annotated(contained, predicates),
			})
			continue
		}

		region, found := g.Topology().RegionOf(contained, g.Boundaries(), survey)
		diags = append(diags, found...)

		// A boundary which could not be read is named as undrawn rather than
		// handed back as an outline covering nothing. The two are not the same
		// answer: an open run of edges legitimately covers nothing and is drawn
		// from its segments, and a consumer which read "no area" as "not drawn"
		// would leave every railing and doorway off the sheet.
		if refused(found) {
			reason := UndrawnUnreadableBoundary
			if placed {
				reason = UndrawnNoPosition
			}

			plan.undrawn = append(plan.undrawn, Undrawn{
				node:        contained,
				reason:      reason,
				declaredIn:  region.Frame(),
				annotations: g.annotated(contained, predicates),
			})
			continue
		}

		// Judged where the curve was drawn, and before the carry, which is where
		// the drawing was made: a chord is as far from its arc in one frame as
		// in another, and a node which then could not be carried was still
		// drawn to it.
		if tolerance, drawn := region.ChordTolerance(); drawn {
			plan.chord = tolerance
			plan.deviation = math.Max(plan.deviation, region.Deviation())
		}

		declared := region.Frame()

		carried, refusedCarry := g.carried(region, frame)
		if len(refusedCarry) > 0 {
			diags = append(diags, refusedCarry...)

			plan.undrawn = append(plan.undrawn, Undrawn{
				node:        contained,
				reason:      UndrawnUncarried,
				declaredIn:  declared,
				annotations: g.annotated(contained, predicates),
			})
			continue
		}

		plan.outlines = append(plan.outlines, Outline{
			node:        contained,
			region:      carried,
			declaredIn:  declared,
			annotations: g.annotated(contained, predicates),
		})

		// Over the rings which were drawn, because the budget is the accuracy of
		// the lines a sheet carries. A ring which was refused put no corner
		// anywhere, and its position claims accumulated into the figure would be
		// an accuracy for geometry nobody is drawing. A ring carried into the
		// plan's frame carries the transform which brought it there, because a
		// line drawn through a georeference is known no better than the fit.
		plan.budget.Merge(carried.Budget())
	}

	return plan, diags
}

// planFrame is the frame a plan of node is expressed in: the frame the node
// declares, or where it declares none the frame of its first boundary loop,
// which is how [Topology.RegionOf] reads a node's frame, or where it has
// neither the root frame.
//
// A storey is usually declared on the grid its rooms are set out on, and then
// this is that grid and nothing is carried. It is the node which declares
// nothing that the fallbacks are for, and the root is the last of them
// because it is the one frame every other frame in a model reaches.
func (g *Graph) planFrame(node *SemanticNode) ID {
	if frame, declared := node.Frame(); declared && frame != "" {
		return frame
	}

	for loop := range g.Boundaries().Loops(node) {
		if frame := loop.Frame(); frame != "" {
			return frame
		}
		break
	}

	if root, ok := g.Frames().Root(); ok {
		return root.ID
	}

	return ""
}

// carried is a region read in one frame expressed in the plan's, with the edge
// behind each run of its boundary kept.
//
// It is [Region.In], as `site` and `export-map` carry a region, and it is taken
// only where the two frames differ: a region already in the plan's frame comes
// back exactly as it was read, which is what keeps a model authored in one frame
// writing the plan it always did. Where there is no plan frame, or the region
// was read in no frame at all, there is nothing to carry between and the region
// comes back as read.
//
// It departs from [Region.In] in one respect, and deliberately. A region carried
// by [Region.In] attributes no run of its boundary to an edge, because in
// general a carried boundary is a pair of coordinates in one frame and an edge
// whose coordinates are in another. A plan's boundary is what pairs an
// annotation written on an edge with the run it is about, and dropping it would
// lose exactly the edges a sheet's dimensions hang on. A transform between two
// frames is a similarity, so it maps each authored run onto exactly one carried
// run: the pairing is carried rather than re-derived, with each run keeping its
// ring, its edge, its origin and its direction, and only its two corners moved.
func (g *Graph) carried(region Region, frame ID) (Region, []Diagnostic) {
	if frame == "" || region.frame == "" || region.frame == frame {
		return region, nil
	}

	frames := g.Frames()

	carried, refused := region.In(frame, frames)
	if len(refused) > 0 {
		return Region{}, refused
	}

	if len(region.segments) == 0 {
		return carried, nil
	}

	segments := make([]BoundarySegment, 0, len(region.segments))
	for _, segment := range region.segments {
		from, err := frames.TransformPoint(segment.from, region.frame, frame)
		if err != nil {
			return Region{}, uncarriedRun(region, frame, segment.from, err)
		}

		to, err := frames.TransformPoint(segment.to, region.frame, frame)
		if err != nil {
			return Region{}, uncarriedRun(region, frame, segment.to, err)
		}

		segment.from, segment.to = from, to
		segments = append(segments, segment)
	}

	carried.segments = segments

	return carried, nil
}

// uncarriedRun is the diagnostic for a corner of a boundary which could not be
// carried into a plan's frame, in the words [Region.In] uses for one.
//
// It is not expected to be reached — [Region.In] has already carried every
// corner of the same region along the same route — and it is here so that if it
// ever is, the node is named as uncarried rather than drawn with a run left in
// the frame it came from.
func uncarriedRun(region Region, frame ID, point Point, err error) []Diagnostic {
	return []Diagnostic{{
		Severity: SeverityError,
		Span:     region.span,
		Message: fmt.Sprintf(
			"expected to express %s in the frame %s, found that %s could not be carried across: %s",
			region.name(), frame, pointText(point, region.printed()), err,
		),
		Hint: "a transform which cannot be applied to one corner of a boundary cannot be applied to the boundary; " +
			"nothing here carries the corners it could and leaves the rest",
	}}
}

// contained is everything node contains, in id order.
//
// The walk is the containment one and reaches all the way down, so a storey's
// rooms and the alcoves inside those rooms are both in it. Nothing is filtered
// out here: what a plan can draw is decided per node by [Graph.outlined] and by
// whether its boundary read, and both answers are reported rather than used to
// shorten this list, and what a plan is asked about is decided per node by its
// [Selection], after the walk rather than during it.
//
// The order is by id so that both lists a plan comes back with are properties of
// what the model says rather than of which file each node happens to be written
// in.
func (g *Graph) contained(node *SemanticNode) []*SemanticNode {
	var out []*SemanticNode

	for related := range g.Descendants(node) {
		out = append(out, related.Node())
	}

	slices.SortFunc(out, func(a, b *SemanticNode) int {
		return strings.Compare(string(a.ID()), string(b.ID()))
	})

	return out
}

// outlined reports whether the model says where a node's edges are.
//
// The test is the boundary reference and not the kind: what makes something
// drawable is that the model gives it edges, and an element which is outlined is
// as drawable as a room.
func (g *Graph) outlined(node *SemanticNode) bool {
	for range g.Boundaries().Loops(node) {
		return true
	}
	return false
}

// annotated is the claims reported on one contained node: those written on the
// node itself, then those written on each edge of its boundary.
//
// It is asked of a node which was not drawn as well as of one which was, and it
// answers the same way. A node with no loop has no edge anchors and contributes
// its own claims; a node whose ring would not read still names its edges, and
// what is written on them is still what somebody wrote.
//
// The node comes first because it is the thing being drawn and its claims are
// about the whole of it — a name, an area, an occupancy — while an edge's are
// about one side. Within each anchor the predicates come in the order the
// invocation named them, and within one predicate the claims come in the order
// they were written, so the whole order is total and two runs over one model
// diff to nothing.
func (g *Graph) annotated(node *SemanticNode, predicates []string) []Annotation {
	// Made rather than declared so that an outline nothing is claimed about
	// carries an empty list rather than a null, and a consumer indexing it needs
	// no special case for the room nobody has measured yet.
	out := make([]Annotation, 0)

	var rings []ID
	for loop := range g.Boundaries().Loops(node) {
		rings = append(rings, loop.ID())
	}

	out = append(out, g.written(Anchor{
		kind:  AnchorNode,
		id:    node.ID(),
		rings: rings,
	}, predicates)...)

	for edge := range g.Boundaries().Edges(node) {
		start, end := edge.Vertices()

		out = append(out, g.written(Anchor{
			kind:  AnchorEdge,
			id:    edge.ID(),
			start: start,
			end:   end,
		}, predicates)...)
	}

	return out
}

// written is every live claim on one anchor under the predicates asked for.
//
// Nothing is resolved and nothing is ranked. A predicate with two live claims
// contributes both, in the order they were written, which is what makes a
// disagreement visible on the sheet it would otherwise be decided on silently.
func (g *Graph) written(anchor Anchor, predicates []string) []Annotation {
	var out []Annotation

	for _, predicate := range predicates {
		for claim := range g.Claims().Under(anchor.id, predicate) {
			if claim.Rank() == RankDeprecated {
				continue
			}
			out = append(out, Annotation{anchor: anchor, claim: claim})
		}
	}

	return out
}

// distinct is names with the repeats taken out, keeping the order they were
// first written in.
//
// A predicate named twice is one predicate. Reporting its claims twice would be
// a caller's typing mistake turned into a duplicated dimension on a sheet.
func distinct(names []string) []string {
	out := make([]string, 0, len(names))

	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, repeated := seen[name]; repeated {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}

	return out
}

// spelledIDs is a list of ids as strings, for a message which names them.
func spelledIDs(ids []ID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return out
}
