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

const tessellateUsage = `dfcad tessellate — draw the boundary of one thing as straight segments, to a
chord tolerance you name.

Usage:

	dfcad tessellate [flags] <id>

The outline of a semantic node as rings of straight segments: an outer ring per
connected part and the rings taken out of it. Every artefact target wants this
and none of them can take a curve — an IFC profile, a plan ring and a GIS
polygon are all straight segments — so this is where the curve becomes segments
and where it is said how closely.

It is the one place that happens. Nothing in the engine tessellates on the way
to an answer: an area, a length, a centroid and a bounding box are all computed
from the arc itself, so the resolution of a drawing never leaks into a figure
somebody reports. What comes back here is a drawing, it says what it was drawn
to, and it is thrown away after — nothing is written back into the model
([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).

A boundary with nothing curved in it is drawn to itself, unchanged. That is the
answer rather than a special case: the same rings, the same orientation and a
deviation of nothing, so a caller has one command rather than one for curved
outlines and another for straight ones.

Flags:

	--position <predicate>     the predicate a corner's position is claimed
	                           under, which the boundary is read from
	                           (required)
	--tolerance <name>         the tolerance corners are judged coincident
	                           against and rings judged planar against
	                           (required)
	--chord <name>             the tolerance a segment standing in for a curve
	                           may fall from it by (required)
	--arc-centre <predicate>   the predicate a curved edge's centre is claimed
	                           under
	--arc-through <predicate>  the predicate the point a curved edge passes
	                           through is claimed under

None of the first three has a default and none of them ever will. Which
predicate carries a position, how close two corners have to be to be one
corner, and how closely a curve has to be followed are things the project wrote
down, and a value compiled in here would be the engine deciding the resolution
of somebody else's drawing.

The last two are the vocabulary an arc is written in, and they go together: a
centre without a point on the curve leaves two arcs between the same two ends
and does not say which was meant, so naming one and not the other is a usage
error. A run which names neither reads every edge as straight, which is what
almost every edge is and what every edge in a model nobody has claimed an arc
in is.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object tessellate writes carries "subject", "derived" and the "digest" of
the source tree the drawing was derived from, the "frame" and "unit" it is
expressed in, the "tolerance" it was judged against, the "chord" tolerance it
was drawn to and the "deviation" that drawing actually achieved, and the
"region" it came to with the rings of every piece of it.

The deviation is what was achieved rather than what was asked for, and it is
always within the chord tolerance: a curve is divided into a whole number of
segments, so an arc which needs two and a bit gets three and follows the curve
more closely than it had to. It is reported so that a caller can check the
approximation it got against the one it asked for.

Where a curve went unread the object carries "chorded": the edges which state
one, each with the predicates it states it under, and no "deviation" at all.
A drawing which ran straight through a curve it never read did not deviate
from that curve by nothing — it deviated from it by however far the wall bows,
which is a figure this run has no vocabulary to compute. Writing zero there
would be the drawing saying it followed a curve it never looked at, and it is
exactly the field a consumer would assert on to prove that it had.

A node which references no loop — a campus, a warranty — has no outline to
draw, and that is an answer rather than a failure: the region comes back empty
and neither the chord tolerance nor the deviation is written, because nothing
was drawn for it.

Exit code 1 is a drawing which could not be made — a ring which does not close,
a corner nothing states the position of, a shape which crosses itself, a chord
tolerance the registry does not declare in the unit of the frame, or an arc a
tolerance would take more segments to follow than anything can use. The object
still comes back, with "derived" false, so a caller reads why from the
diagnostics on stderr rather than from an empty stream.
`

// The flags tessellate takes beyond the ones the dimensional commands share,
// named here because the errors which refuse them name them.
const (
	flagChord      = "chord"
	flagArcCentre  = "arc-centre"
	flagArcThrough = "arc-through"
)

// PartialArcVocabularyError is a run which named one of the two predicates an
// arc is written under and not the other.
//
// It is a usage error rather than a silent reading of every edge as straight,
// because a run which named one of them meant to read arcs. Reading them as
// straight lines anyway would answer the question it was asked with a drawing
// of a different shape, and nothing in the answer would say so.
type PartialArcVocabularyError struct {
	// Given is the flag which was given, spelled without its dashes.
	Given string

	// Missing is the one which was not, spelled the same way.
	Missing string
}

// Error implements [error].
func (e PartialArcVocabularyError) Error() string {
	return fmt.Sprintf(
		"expected both of the predicates an arc is written under or neither, found --%s with no --%s: a centre and two "+
			"ends leave two arcs, the short way round and the long way round, and the point the curve passes through "+
			"is what says which of them was meant",
		e.Given, e.Missing,
	)
}

// tessellateResult is the object tessellate writes to stdout.
type tessellateResult struct {
	envelope

	// Subject is the id the drawing was asked about, which is written whether
	// or not there was an answer: a refusal a caller cannot attribute to a
	// question is one it has to correlate by position.
	Subject string `json:"subject"`

	// Derived reports whether there is a region below. It is written whatever
	// the outcome, so that a caller can tell a node with no outline to draw
	// from one whose outline could not be read: the first is derived and empty,
	// the second is neither.
	Derived bool `json:"derived"`

	// Digest is the digest of the source tree the drawing was derived from,
	// which is what lets a caller check the drawing against the model in front
	// of them rather than taking it on trust. It is written on a refusal too: a
	// refusal is still about a tree, and saying which one is the point.
	//
	// Absent for a model which was not read from disk, or one a file of which
	// could not be read at all, because there is then nothing anything may be
	// keyed by.
	Digest string `json:"digest,omitempty"`

	// Frame is the coordinate frame the boundary and the drawing are expressed
	// in, and Unit that frame's linear unit. Nothing is converted into any
	// other ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
	Frame string `json:"frame,omitempty"`
	Unit  string `json:"unit,omitempty"`

	// Tolerance is what corners were judged coincident against and rings judged
	// planar against.
	Tolerance *toleranceEntry `json:"tolerance,omitempty"`

	// Chord is the tolerance the curves were drawn to: how far a segment
	// standing in for a curve was allowed to fall from it. It travels with the
	// answer because a list of points which does not say how closely it follows
	// the curve it came from is an approximation nobody downstream can judge.
	Chord *toleranceEntry `json:"chord,omitempty"`

	// Deviation is how far the worst segment of the drawing actually falls from
	// the curve it stands in for, in Unit. It is at most Chord, and a boundary
	// with nothing curved in it deviates from itself by nothing.
	//
	// Absent where Chorded below is not, and that absence is the whole of this
	// story's finding. A drawing which ran straight through a curve it never
	// read deviates from that curve by however far the wall bows, which this
	// run has no vocabulary to compute; the figure it does have — how far the
	// segments fall from the straight edges they were drawn from — is nothing,
	// and writing it here would be an affirmative statement that the curve was
	// followed exactly.
	Deviation *measuredValue `json:"deviation,omitempty"`

	// Chorded is the edges of the boundary which state a curve this run did not
	// read, each with the predicates it states it under. Absent for a run which
	// read every curve and for a node whose boundary claims none.
	//
	// It is in the answer and not only on stderr because every point below it
	// is on the straight line between two corners rather than on the wall, and
	// this is a drawing somebody keeps.
	Chorded []chordedEntry `json:"chorded,omitempty"`

	// Region is what the drawing came to. It is written for a drawing which
	// succeeded whether or not it covers anything: a node which references no
	// loop has no outline, which is an answer, and omitting it would make it
	// look like one which was never computed.
	Region *regionEntry `json:"region,omitempty"`

	// Budget is the accuracy of the corners the drawing was read from, broken
	// out by term.
	//
	// It is of the corners and not of the segments between them, for the reason
	// a measurement's budget is of the corners and not of the area. What the
	// segments add is the deviation above, which is a stated bound rather than
	// an accuracy and is reported as its own figure.
	Budget *budgetReport `json:"budget,omitempty"`
}

// runTessellate is the tessellate command.
func runTessellate(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	position := flags.String(flagPosition, "", "")
	tolerance := flags.String(flagTolerance, "", "")
	chord := flags.String(flagChord, "", "")
	centre := flags.String(flagArcCentre, "", "")
	through := flags.String(flagArcThrough, "", "")

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

	if err := vocabularyOf(
		given{flagPosition, *position},
		given{flagTolerance, *tolerance},
		given{flagChord, *chord},
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

	survey := bent(graph, *position, *tolerance, arcs{centre: *centre, through: *through}, node)

	drawn, diags := graph.Topology().TessellateRegion(node, graph.Boundaries(), survey, *chord)

	// What the survey could not bend. A drawing is the artefact this command
	// exists to produce, so an edge run straight through a curve is a wrong
	// shape in a file somebody keeps rather than a figure they can recompute.
	chorded, unread := chordedOf(graph, survey, node)
	diags = append(diags, unread...)

	// The diagnostics are for whoever wrote the model, so they go to stderr on
	// every run and in every format, and whether any of them is an error is what
	// decides between an answer and a refusal.
	refused := render(diags, stderr)

	result := reportTessellate(cmd, graph, subject, drawn, chorded, *tolerance, !refused)

	reportTessellateFor(result, drawn, globals, stderr)

	if err := emit(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	if refused {
		return exitCheck
	}

	return exitSuccess
}

// arcVocabularyOf reports a run which named one of the two predicates an arc is
// written under and not the other.
func arcVocabularyOf(centre, through string) error {
	switch {
	case centre != "" && through == "":
		return PartialArcVocabularyError{Given: flagArcCentre, Missing: flagArcThrough}
	case through != "" && centre == "":
		return PartialArcVocabularyError{Given: flagArcThrough, Missing: flagArcCentre}
	}

	return nil
}

// arcs is the vocabulary a curved edge is read under: the two predicates an arc
// is written in, and the tolerance it may be drawn to where an answer cannot be
// reached without straight segments.
//
// The three travel together because a run which named some of them has said
// something incomplete: two predicates without a chord is a curve which can be
// measured and not overlaid, and a chord without the predicates is a resolution
// named for a curve nothing will read. Which of them a command needs is the
// command's, and [arcVocabularyOf] is where each says so.
type arcs struct {
	// centre is the predicate an arc's centre is claimed under, and through the
	// predicate the point it passes through is claimed under.
	centre  string
	through string

	// chord is the tolerance a curve is drawn to where straight segments are
	// unavoidable. Empty is a run which refuses such a question rather than
	// answering it at a resolution nobody named.
	chord string
}

// bent is where every corner of the subjects is and which of their edges bend,
// read under the predicates the run named.
//
// The arcs are read only where the run named the vocabulary they are written in.
// An edge with no arc is straight, which is what almost every edge is and what a
// model nobody has claimed a curve in is made entirely of — so an absent
// predicate is a model with nothing to bend rather than a drawing missing its
// curves ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
// What it is not is a model whose curves may go unmentioned: [dfcad.Graph.UnreadArcs]
// is the other half of that, and every command which builds a survey here
// reports what this could not read.
//
// More than one subject is one survey and not several. A corner read against two
// surveys is a corner which can be in two places, and two shapes being compared
// — a proposal and the envelope it has to sit in, two rooms either side of a
// party wall — is exactly where that shows up as a gap nobody authored.
//
// A corner or an arc whose claim does not resolve is not placed and is not
// refused here. Whether it was needed is the answer's question, and it answers
// it with a diagnostic naming the corner or the edge and the shape it belonged
// to — which is more than this could say.
func bent(graph *dfcad.Graph, position, tolerance string, curved arcs, subjects ...dfcad.Entity) dfcad.Survey {
	survey := dfcad.Survey{Tolerance: tolerance, Chord: curved.chord, Registry: graph.Registry()}

	for _, subject := range subjects {
		for vertex := range graph.Corners(subject) {
			resolution, err := graph.Claims().Resolve(vertex.ID(), position, graph.Registry())
			if err != nil {
				continue
			}
			survey.Place(vertex.ID(), resolution)
		}
	}

	if curved.centre == "" || curved.through == "" {
		return survey
	}

	for _, subject := range subjects {
		for edge := range graph.Bounding(subject) {
			at, err := graph.Claims().Resolve(edge.ID(), curved.centre, graph.Registry())
			if err != nil {
				continue
			}

			on, err := graph.Claims().Resolve(edge.ID(), curved.through, graph.Registry())
			if err != nil {
				continue
			}

			survey.Bend(edge.ID(), at, on)
		}
	}

	return survey
}

// chordedEntry is one edge which states a curve the run did not read, as the
// machine contract writes it.
//
// It is in the answer and not only on stderr because it is part of the answer:
// every figure beside it is about the straight line between that edge's two ends
// rather than about the wall, and a consumer comparing a computed area against a
// surveyor's has to be able to see that from the object it is comparing.
type chordedEntry struct {
	// Edge is the id of the edge which states it.
	Edge string `json:"edge"`

	// Predicates are the predicates it states a position under, which is what
	// to name to have the curve read.
	Predicates []string `json:"predicates"`

	// Span is where that edge was written.
	Span dfcad.Span `json:"span"`
}

// chordedOf is the unread curves as the machine contract writes them, and the
// warnings which say the same thing to whoever wrote the model.
//
// Both come back because they are the same finding for two readers, and neither
// is derived by parsing the other: the diagnostics go to stderr on every run and
// in every format, and the entries go into the object on stdout.
func chordedOf(graph *dfcad.Graph, survey dfcad.Survey, subjects ...dfcad.Entity) ([]chordedEntry, []dfcad.Diagnostic) {
	unread, diags := graph.UnreadArcs(survey, subjects...)
	if len(unread) == 0 {
		return nil, diags
	}

	entries := make([]chordedEntry, 0, len(unread))
	for _, arc := range unread {
		entry := chordedEntry{Predicates: arc.Predicates(), Span: arc.Span()}
		if edge := arc.Edge(); edge != nil {
			entry.Edge = string(edge.ID())
		}
		entries = append(entries, entry)
	}

	return entries, diags
}

// asEntities is a list of nodes as the thing a survey is built over.
func asEntities(nodes ...*dfcad.SemanticNode) []dfcad.Entity {
	out := make([]dfcad.Entity, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node)
	}
	return out
}

// reportTessellate is the drawing as the machine contract writes it.
//
// The subject is the id the run was asked about rather than the one the answer
// carries, because a refused drawing carries none: a caller collecting results
// has to be able to say which question this object answers, and a refusal is
// exactly the object it most needs that of.
func reportTessellate(
	cmd command,
	graph *dfcad.Graph,
	subject dfcad.ID,
	drawn dfcad.RegionTessellation,
	chorded []chordedEntry,
	tolerance string,
	ok bool,
) tessellateResult {
	result := tessellateResult{
		envelope: newEnvelope(cmd.name),
		Subject:  string(subject),
		Derived:  ok,
		Chorded:  chorded,
	}

	if digest, known := graph.Digest(); known {
		result.Digest = digest.String()
	}

	if !ok {
		return result
	}

	unit := string(drawn.Unit())

	result.Frame = string(drawn.Frame())
	result.Unit = unit

	if found, held := graph.Registry().Tolerance(tolerance); held {
		entry := declared(found)
		result.Tolerance = &entry
	}

	// Both are properties of a drawing, and a node which references no loop had
	// none made: there was no curve to follow, so the tolerance was never read.
	// Writing them anyway would put a tolerance with no name and a deviation
	// from nothing into the answer, which reads as a drawing rather than as the
	// absence of one.
	if drawn.ChordTolerance().Name != "" {
		chord := declared(drawn.ChordTolerance())
		result.Chord = &chord

		// The deviation is the drawing's distance from the boundary it was
		// drawn from, and an unread curve is a boundary this drawing never saw.
		// The figure below would be its distance from the straight edge it
		// substituted, which is nothing — and nothing, beside a named chord
		// tolerance, reads as a curve followed exactly. The chord stays,
		// because what was asked for is still what was asked for; what goes is
		// the claim about what was achieved.
		if len(chorded) == 0 {
			result.Deviation = &measuredValue{Value: drawn.Deviation(), Unit: unit}
		}
	}

	region := regionOf(drawn.Region())
	result.Region = &region

	// A budget with nothing in it is left out rather than written empty, for the
	// reason a measurement's is: an object carrying neither the figure nor a
	// reason for its absence reads as an answer known exactly. A node which
	// references no loop was drawn from no claim at all and has no accuracy to
	// report.
	if budget := budgetOf(drawn.Budget()); !empty(budget) {
		result.Budget = &budget
	}

	return result
}

// reportTessellateFor renders a drawing for a person, on stderr.
//
// Nothing here reaches stdout, in any format and at any verbosity: stdout is the
// same bytes whether or not anybody asked to read the run.
func reportTessellateFor(result tessellateResult, drawn dfcad.RegionTessellation, globals *globals, stderr io.Writer) {
	if !globals.human() || !result.Derived {
		return
	}

	_, _ = fmt.Fprintf(stderr, "%s\n", drawn)
}
