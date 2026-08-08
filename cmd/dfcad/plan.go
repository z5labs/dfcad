// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"io"

	"github.com/z5labs/dfcad"
)

const planUsage = `dfcad plan — report what a spatial node contains as rings, with the claims
written on them.

Usage:

	dfcad plan [flags] <id>

The outlines of everything inside a storey — its rooms, the alcoves inside
those rooms, anything else the model gives edges to — each named by the node it
came from, and each carrying the claims the invocation asked for, anchored to
what they are written on. It is the input an annotated floor plan is drawn
from: facts with provenance, rather than a polygon a renderer has to re-derive
which claim belongs to which pair of corners from.

This is a query and not an export. It writes no file. It returns the rings the
model already holds and the claims already written on the edges bounding them,
under the same envelope, digest and budget every other answer carries, and it
knows nothing about paper, scale, title blocks, text height or where a leader
goes. Those are the consumer's, and this command is the boundary that keeps
them so.

Flags:

	--annotate <predicate>     a predicate whose claims are reported on every
	                           ring and on the edges bounding it; repeat it for
	                           more than one (at least one required)
	--position <predicate>     the predicate a corner's position is claimed
	                           under, which the rings are read from (required)
	--tolerance <name>         the tolerance corners are judged coincident
	                           against (required)
	--arc-centre <predicate>   the predicate a curved edge's centre is claimed
	                           under
	--arc-through <predicate>  the predicate the point a curved edge passes
	                           through is claimed under
	--chord <name>             the tolerance a curved edge is drawn to

None of the first three has a default and none of them ever will. Which
predicate carries a position, and how close two corners have to be to be one
corner, are things the project wrote down. --annotate is the same rule applied
to the question a sheet asks: whether a measurement is worth drawing is not
something this engine can know, so the answer is that it is worth drawing if the
caller asked for that predicate — which is what keeps the format from learning
anything about drawing.

The last three are the vocabulary a curved wall is read under, and all three are
needed to read one. A ring is a list of points, so a curve has to become points
somewhere, and --chord is where it is said how closely; the two predicates go
together, because a centre without a point on the curve leaves two arcs between
the same two ends and does not say which was meant.

A run which names none of them draws every edge as the straight line between its
two ends, and says so where the model states otherwise: "chorded" lists every
edge which claims a position — which is how and only how a curve is written,
because an edge has no position of its own — and a warning on stderr names each
of them. A ring drawn straight through a curve is a drawing error rather than a
rounding, and it is one nothing downstream can detect.

Every claim comes back whole: its value and unit, what evidences it, how it was
obtained, how well it is known and when. A rendered string is a claim, not a
formatting of a number, and a sheet is the last place to print a design
estimate looking like an as-built survey.

Nothing is resolved. Where two live claims compete under one predicate on one
anchor, both come back with the same anchor, and which of them a sheet prints
is the caller's decision. A retracted claim is never reported.

The subject is a place rather than a grouping. A zone holds its members by
membership and contains nothing, so asking for the plan of one is a usage error
rather than an empty answer.

A node with no boundary contributes nothing, because it has no ring: a doorway
written as a line, a circuit group and a warranty are all ordinary and none of
them is an outline. A storey containing nothing with an outline is an empty
result and exit 0 — the truthful answer to what it looks like in plan.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object plan writes carries "subject", "planned" and the "digest" of the
source tree it was read from, the "frame" and "unit" it is expressed in, the
"tolerance" it was judged against, the "annotating" predicates it was asked
for, one "outlines" entry per contained node with its "region" and its
"annotations", and the "budget": the accuracy of the rings, over the position
claims which put every drawn corner where it is. Where a ring bent it also
carries the "chord" tolerance it was drawn to and the "deviation" that drawing
achieved, and where a curve went unread it carries "chorded": the edges which
state one, each with the predicates it states it under.

Exit code 1 is a plan a ring of which could not be read — a boundary which does
not close, corners which are not in one plane, a tolerance the registry does
not declare in the frame's unit. The other rooms are still drawn and the object
still comes back, with "planned" false, so a caller reads which room to fix
from the diagnostics on stderr rather than from an empty stream.
`

// flagAnnotate is the flag naming a predicate whose claims a plan reports. It
// is named here because the error which refuses a run without one names it.
const flagAnnotate = "annotate"

// NotSpatialError is a plan asked of something which is not a place.
//
// It is separate from the errors about an id nothing holds because the id is
// perfectly good: a zone exists, it is a semantic node, and it holds its members
// by membership rather than by containment. Answering "nothing is in here" for
// one would read as a zone whose members have no outlines, which is a different
// and much quieter wrong answer than refusing the question.
type NotSpatialError struct {
	// ID is what was asked about.
	ID string

	// Kind is the kind it declares.
	Kind string
}

// Error implements [error].
func (e NotSpatialError) Error() string {
	return fmt.Sprintf(
		"expected somewhere to draw the contents of, found %s, which declares kind %s: a zone groups things which "+
			"are somewhere and is nowhere itself, so nothing is inside it to draw",
		e.ID, e.Kind,
	)
}

// planResult is the object plan writes to stdout.
type planResult struct {
	envelope

	// Subject is the id the plan was asked about, which is written whether or
	// not there was an answer: a refusal a caller cannot attribute to a
	// question is one it has to correlate by position.
	Subject string `json:"subject"`

	// Planned reports whether every ring below could be read. It is written
	// whatever the outcome, so that a caller can tell a storey nobody has
	// outlined yet — planned, with no outlines — from one a room of which could
	// not be read.
	Planned bool `json:"planned"`

	// Digest is the digest of the source tree the rings and the claims were
	// read from, which is what lets a consumer say which model a sheet was
	// drawn from rather than taking it on trust. It is written on a refusal
	// too.
	//
	// Absent for a model which was not read from disk, or one a file of which
	// could not be read at all, because there is then nothing anything may be
	// keyed by.
	Digest string `json:"digest,omitempty"`

	// Frame is the coordinate frame the subject is declared in, and Unit that
	// frame's linear unit, which every coordinate here is in and every area in
	// the square of.
	Frame string `json:"frame,omitempty"`
	Unit  string `json:"unit,omitempty"`

	// Tolerance is what corners were judged coincident against.
	Tolerance *toleranceEntry `json:"tolerance,omitempty"`

	// Annotating is the predicates the run asked for, in the order it named
	// them and with a repeat written once. It is echoed because it is the whole
	// of the answer to "why is this dimension on the sheet and that one not",
	// and a caller collecting plans has to be able to say which question each
	// object answers.
	Annotating []string `json:"annotating"`

	// Chord is the tolerance the curves of the rings were drawn to and
	// Deviation how far the worst of those segments fell from the curve it
	// stood in for, over every outline. Both are absent for a storey with
	// nothing curved in it, which was approximated nowhere.
	Chord     *toleranceEntry `json:"chord,omitempty"`
	Deviation *measuredValue  `json:"deviation,omitempty"`

	// Chorded is the edges of the rings which state a curve this run did not
	// read, each with the predicates it states it under. Absent for a run which
	// read every curve and for a storey which claims none.
	//
	// A ring drawn straight through a curve is a drawing error and not a
	// rounding, and a sheet is the last place it would be noticed: it looks
	// like a wall somebody meant.
	Chorded []chordedEntry `json:"chorded,omitempty"`

	// Outlines is one entry per contained node which has a ring, in id order.
	// Empty rather than null for a subject which contains nothing drawable.
	Outlines []outlineEntry `json:"outlines"`

	// Budget is the accuracy of the rings, over the position claims which put
	// every drawn corner where it is. It is the accuracy of the geometry and
	// not of the annotations: each claim below carries its own.
	Budget *budgetReport `json:"budget,omitempty"`
}

// outlineEntry is one contained node drawn as rings, with what is written on
// it.
type outlineEntry struct {
	// Node is the id of the node the rings were read from, which is what names
	// them.
	Node string `json:"node"`

	// Label is what it is called, absent where it is called nothing. Kind and
	// Type are what it is, which is what a sheet reads to decide how to draw
	// it — and which is the consumer's decision rather than this command's.
	Label string `json:"label,omitempty"`
	Kind  string `json:"kind,omitempty"`
	Type  string `json:"type,omitempty"`

	// Region is the area it covers, with the ring bounding each piece and the
	// edge behind each straight run of them.
	Region regionEntry `json:"region"`

	// Annotations are the claims reported on it, the node's own first and then
	// those of each edge of its boundary. Empty rather than null for a room
	// nobody has written anything on.
	Annotations []annotationEntry `json:"annotations"`
}

// annotationEntry is one live claim reported on a plan, with what it is written
// on.
//
// The claim's fields are written beside the anchor rather than nested under a
// key of their own, so that a claim on a plan reads exactly like a claim
// anywhere else in this contract and a consumer needs one reader for both.
type annotationEntry struct {
	// Anchor is what the claim is written on, and where that is.
	Anchor anchorEntry `json:"anchor"`

	claimEntry
}

// anchorEntry is what a reported claim is written on.
//
// It carries the geometry which locates the claim and not only the id it is
// attached to. A consumer given the id of an edge alone would have to re-read
// the model to find which two corners a dimension runs between, and re-reading
// is where two answers drift apart.
type anchorEntry struct {
	// Kind is which family the anchor belongs to: `edge` for a claim written on
	// an edge of a ring, `node` for one written on the node that ring bounds.
	Kind string `json:"kind"`

	// ID is the id of that edge or that node.
	ID string `json:"id"`

	// Vertices are the edge's two corners, in the order the edge was authored.
	// Absent for a node anchor.
	//
	// They are the edge's own order and not the order any ring traverses them.
	// Two rings either side of a party wall run through one edge opposite ways,
	// and a claim written on the edge is written on the edge rather than on
	// either traversal of it; a consumer which needs the traversal direction
	// reads it from `region.boundary`, where that question is already answered.
	Vertices []string `json:"vertices,omitempty"`

	// Rings are the loops bounding the node, in the order it references them.
	// Absent for an edge anchor.
	Rings []string `json:"rings,omitempty"`
}

// runPlan is the plan command.
func runPlan(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	annotate := &repeated{}
	flags.Var(annotate, flagAnnotate, "")
	position := flags.String(flagPosition, "", "")
	tolerance := flags.String(flagTolerance, "", "")
	centre := flags.String(flagArcCentre, "", "")
	through := flags.String(flagArcThrough, "", "")
	chord := flags.String(flagChord, "", "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	switch {
	case len(arguments) == 0:
		return usageError(cmd, ErrMissingSubject, stderr, true)
	case len(arguments) > 1:
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments[1:]}, stderr, true)
	}

	// A predicate to annotate with is vocabulary like the other two: which of a
	// project's measurements belongs on a sheet is a thing the project decides,
	// so a run which named none has not asked a question yet and is refused
	// beside whichever of the others it also left out.
	annotating := ""
	if len(*annotate) > 0 {
		annotating = (*annotate)[0]
	}

	if err := vocabularyOf(
		given{flagAnnotate, annotating},
		given{flagPosition, *position},
		given{flagTolerance, *tolerance},
	); err != nil {
		return usageError(cmd, err, stderr, true)
	}

	if err := arcVocabularyOf(*centre, *through); err != nil {
		return usageError(cmd, err, stderr, true)
	}

	subject, err := dfcad.ParseID(arguments[0])
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	graph := loadModel(cmd, globals, stderr)

	node, err := traversable(graph, subject)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	if !node.Kind().Spatial() {
		return usageError(cmd, NotSpatialError{ID: string(subject), Kind: string(node.Kind())}, stderr, false)
	}

	// A predicate the registry does not declare is a usage error rather than an
	// empty answer, for the reason it is one in a listing: a predicate nobody
	// declared and a predicate nothing is claimed under are different answers,
	// and a caller which cannot tell them apart retries a misspelling forever.
	for _, predicate := range *annotate {
		if err := checkPredicate(graph.Registry(), predicate); err != nil {
			return usageError(cmd, err, stderr, false)
		}
	}

	// One survey over every corner the plan could draw rather than one per room.
	// A corner read against two surveys is a corner which can be in two places,
	// and two rooms sharing a wall is where that shows up as a gap down the
	// middle of a sheet.
	var rooms []dfcad.Entity
	for contained := range graph.Descendants(node) {
		rooms = append(rooms, contained.Node())
	}

	survey := bent(graph, *position, *tolerance, arcs{centre: *centre, through: *through, chord: *chord}, rooms...)

	drawn, diags := graph.PlanOf(node, survey, dfcad.Annotations{Predicates: *annotate})

	// What the survey could not bend, over every room which could be drawn: a
	// ring run straight through a curve looks like a wall somebody meant, and a
	// sheet is the last place anybody would catch it.
	chorded, unread := chordedOf(graph, survey, rooms...)
	diags = append(diags, unread...)

	// The diagnostics are for whoever wrote the model, so they go to stderr on
	// every run and in every format, and whether any of them is an error is what
	// decides between an answer and a refusal.
	refused := render(diags, stderr)

	result := reportPlan(cmd, graph, subject, *annotate, drawn, chorded, !refused)

	reportPlanFor(result, drawn, globals, stderr)

	if err := emit(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	if refused {
		return exitCheck
	}

	return exitSuccess
}

// reportPlan is the plan as the machine contract writes it.
//
// The subject is the id the run was asked about rather than the one the answer
// carries, because a refused plan carries none: a caller collecting results has
// to be able to say which question this object answers, and a refusal is exactly
// the object it most needs that of.
func reportPlan(
	cmd command,
	graph *dfcad.Graph,
	subject dfcad.ID,
	annotating []string,
	drawn dfcad.Plan,
	chorded []chordedEntry,
	ok bool,
) planResult {
	result := planResult{
		envelope:   newEnvelope(cmd.name),
		Subject:    string(subject),
		Planned:    ok,
		Annotating: make([]string, 0, len(annotating)),
		Chorded:    chorded,
		Outlines:   make([]outlineEntry, 0, len(drawn.Outlines())),
	}

	// The predicates which were asked for, and not the ones something was
	// claimed under. A predicate nothing is written under is the answer to "why
	// is there no width on this wall", and dropping it from the echo would leave
	// a caller unable to tell it from one it forgot to ask for.
	for _, predicate := range annotating {
		result.Annotating = appendOnce(result.Annotating, predicate)
	}

	if digest, known := graph.Digest(); known {
		result.Digest = digest.String()
	}

	result.Frame = string(drawn.Frame())
	result.Unit = string(drawn.Unit())

	if tolerance := drawn.Tolerance(); tolerance.Name != "" {
		entry := declared(tolerance)
		result.Tolerance = &entry
	}

	// Written only where a curve was actually drawn: a storey of straight walls
	// was approximated nowhere, and a tolerance with no name beside a deviation
	// from nothing would read as an approximation of it.
	if made, bent := drawn.ChordTolerance(); bent {
		chord := declared(made)
		result.Chord = &chord

		result.Deviation = &measuredValue{Value: drawn.Deviation(), Unit: string(drawn.Unit())}
	}

	for _, outline := range drawn.Outlines() {
		result.Outlines = append(result.Outlines, outlineOf(outline))
	}

	// A storey nobody has outlined yet accumulated nothing, and an empty budget
	// object — no terms, no combined figure and no reason for there being none —
	// reads as a plan whose rings are known exactly. It is left out instead, the
	// way `measure` and `tessellate` leave theirs out.
	if budget := budgetOf(drawn.Budget()); !empty(budget) {
		result.Budget = &budget
	}

	return result
}

// outlineOf is one drawn node as the machine contract writes it.
func outlineOf(outline dfcad.Outline) outlineEntry {
	entry := outlineEntry{
		Node:        string(outline.Subject()),
		Region:      regionOf(outline.Region()),
		Annotations: make([]annotationEntry, 0, len(outline.Annotations())),
	}

	if node := outline.Node(); node != nil {
		entry.Label = node.Label()
		entry.Kind = string(node.Kind())
		entry.Type = node.Type()
	}

	for _, annotation := range outline.Annotations() {
		entry.Annotations = append(entry.Annotations, annotationOf(annotation))
	}

	return entry
}

// annotationOf is one reported claim as the machine contract writes it.
//
// The resolution state is left empty rather than computed. A plan resolves
// nothing — two live claims about one wall both come back — and writing a state
// which said one of them had won would be this command making, invisibly, the
// decision it exists to leave to the sheet.
func annotationOf(annotation dfcad.Annotation) annotationEntry {
	entry := annotationEntry{Anchor: anchorOf(annotation.Anchor())}

	if claim := annotation.Claim(); claim != nil {
		entry.claimEntry = entryOf(claim, "")
	}

	return entry
}

// anchorOf is what a reported claim is written on, as the machine contract
// writes it.
func anchorOf(anchor dfcad.Anchor) anchorEntry {
	entry := anchorEntry{Kind: string(anchor.Kind()), ID: string(anchor.ID())}

	if start, end, edge := anchor.Vertices(); edge {
		entry.Vertices = []string{string(start), string(end)}
	}

	for _, ring := range anchor.Rings() {
		entry.Rings = append(entry.Rings, string(ring))
	}

	return entry
}

// appendOnce adds name to names unless it is already there, keeping the order
// they were first seen in.
func appendOnce(names []string, name string) []string {
	if name == "" {
		return names
	}
	for _, written := range names {
		if written == name {
			return names
		}
	}
	return append(names, name)
}

// reportPlanFor renders a plan for a person, on stderr.
//
// Nothing here reaches stdout, in any format and at any verbosity: stdout is
// the same bytes whether or not anybody asked to read the run.
func reportPlanFor(result planResult, drawn dfcad.Plan, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	// The outlines and what is written on each are the detail under the summary,
	// so they are behind the verbosity flag — and they are rendered by the
	// library rather than spelled again here, so that a caller reporting the
	// answer and this command reporting it write the same thing.
	if globals.Verbosity >= verbosityProgress {
		_, _ = fmt.Fprintf(stderr, "%s\n", drawn.Report())
		return
	}

	_, _ = fmt.Fprintf(stderr, "%s\n", drawn)
}
