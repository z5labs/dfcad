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

const measureUsage = `dfcad measure — compute how big one thing is from the geometry it is written in
terms of.

Usage:

	dfcad measure [flags] <id>

The area a room encloses, the length of its boundary, where its area is centred
and how far it reaches, computed from the corners every time it is asked for.
Nothing in the model says how big a room is unless somebody wrote it down as a
claim, and this never reads such a claim and never writes one: an area recorded
beside the boundary it describes is a second source of truth which goes stale
the first time a wall moves.

That is the whole difference between this and "dfcad resolve <id> area", and
neither substitutes for the other. Resolution answers what somebody stated,
with the claim it came from and the rule which chose it. This answers what the
geometry comes to, with the digest of the tree it was computed against. A
claimed area which disagrees with a computed one is the most valuable thing in
the file, and it only stays visible while the two are asked separately.

The id may name anything the model holds and there is no flag saying which:

	a semantic node   area, boundary length, centroid and bounds, read
	                  through the loops which bound it
	a loop            the same, for the ring its edges traverse
	an edge           length, midpoint and bounds
	a vertex          where it is

A node whose declared geometry is "point" is measured from the position claimed
of the node itself under --position, and what comes back is what a vertex's
measurement is: the point as the centroid, a bounding box of no extent, no
length and no area, with the accuracy of the claim which placed it. A panel, a
receptacle and a survey monument are each that shape.

A node which references no loop — a circuit group, a warranty — measures
nothing, and that is an answer rather than a failure.

Flags:

	--position <predicate>     the predicate a corner's position is claimed
	                           under, which every figure is read from
	                           (required)
	--tolerance <name>         the tolerance corners are judged coincident
	                           against and rings judged planar against
	                           (required)
	--arc-centre <predicate>   the predicate a curved edge's centre is claimed
	                           under
	--arc-through <predicate>  the predicate the point a curved edge passes
	                           through is claimed under
	--chord <name>             the tolerance a curve may be drawn to where the
	                           rings of a region have to be nested at the
	                           segments

Neither of the first two has a default and neither ever will. Which predicate
carries a position, and how close two corners have to be to be one corner, are
things the project wrote down, and a value compiled in here would be the engine
deciding one of them on a project's behalf.

The last three are the vocabulary a curve is read under. The two predicates go
together: a centre without a point on the curve leaves two arcs between the same
two ends and does not say which was meant, so naming one and not the other is a
usage error.

A run which names them measures the arc and not the chord. Every figure comes
from the parameterisation — the area under a bay window, the length along it,
where it puts the centroid, how far it reaches — so nothing here is drawn and
--chord is not needed to read a curve at all. It is needed for one thing: a
region bounded by several rings, one of which bends, is nested by asking which
ring holds which, and that question is answered at the segments because a bulge
which reaches past a corner is a whole ring counted the wrong way. Such a region
is refused until a chord tolerance names what it may be drawn to, and the answer
then carries that tolerance and the deviation the drawing achieved.

A run which names none of them reads every edge as the straight line between its
two ends, which is what almost every edge is. Where that discarded something the
model states, the answer says so: "chorded" lists every edge which claims a
position — which is how and only how a curve is written, because an edge has no
position of its own — and a warning on stderr names each of them. An area
reported over a chorded boundary can be out by a third, and the one thing worse
than refusing it is reporting it silently.

Each figure is reported only where it could be computed, and never as a
plausible-looking number instead. A ring which does not close, corners which do
not lie in one plane, a ring which crosses itself and one whose corners are
collinear each encloses no area, and each is its own diagnostic naming which
mistake it is.

The accuracy is of the corners rather than of the area. Independent terms
combine in quadrature and systematic ones linearly, and a term shared by
several corners — a georeference behind every indoor fact alike — is counted
once however many of them carried it. What is not reported is one figure
standing in for the sensitivity of an area to each of its corners, because
that is a per-corner quantity and a single number for it would be exactly the
plausible answer the rest of this refuses to give.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object measure writes carries "subject" and the "family" which holds it,
"derived" and the "digest" of the source tree it was computed against, the
"frame" and "unit" it was computed in, the "tolerance" it was judged against,
whichever of "area", "length", "centroid" and "bounds" could be computed, each
with its own unit, and the "budget": the accuracy of the corners broken out by
term. Where rings had to be drawn to nest them it also carries the "chord"
tolerance they were drawn to and the "deviation" that drawing achieved, and
where a curve went unread it carries "chorded": the edges which state one, each
with the predicates it states it under.

Exit code 1 is a measurement which could not be made — a ring which does not
close, a corner nothing states the position of, a shape which crosses itself.
The object still comes back, with "derived" false, so a caller reads why from
the diagnostics on stderr rather than from an empty stream.
`

// measureResult is the object measure writes to stdout.
type measureResult struct {
	envelope

	// Subject is the id the measurement was asked about, which is written
	// whether or not there was an answer: a refusal a caller cannot attribute to
	// a question is one it has to correlate by position.
	Subject string `json:"subject"`

	// Family is which family holds it: node, vertex, edge or loop. It is what
	// says which figures to expect, so that an edge with no area reads
	// differently from a region whose area could not be computed.
	Family string `json:"family,omitempty"`

	// Derived reports whether the figures below were computed. It is written
	// whatever the outcome, so that a caller can tell a thing which measures
	// nothing from one whose geometry could not be read: the first is derived
	// with no figures, the second is not derived at all
	// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
	Derived bool `json:"derived"`

	// Digest is the digest of the source tree the answer was computed against,
	// which is what lets a caller check a computation against the model in front
	// of them rather than taking it on trust. It is written on a refusal too: a
	// refusal is still about a tree, and saying which one is the point.
	//
	// Absent for a model which was not read from disk, or one a file of which
	// could not be read at all, because there is then nothing anything may be
	// keyed by.
	Digest string `json:"digest,omitempty"`

	// Frame is the coordinate frame the answer was computed in, and Unit that
	// frame's linear unit. Nothing is converted into any other
	// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
	Frame string `json:"frame,omitempty"`
	Unit  string `json:"unit,omitempty"`

	// Tolerance is what corners were judged coincident against and rings judged
	// planar against. It travels with the answer because the answer depends on
	// it.
	Tolerance *toleranceEntry `json:"tolerance,omitempty"`

	// Area is what the thing encloses, in the square of Unit. Absent for
	// anything which encloses nothing an area can be computed of, which an edge
	// always is and a ring which does not close, does not lie in one plane or
	// crosses itself is.
	Area *measuredValue `json:"area,omitempty"`

	// Length is the extent of an edge, or the total length of the edges of a
	// loop or a region, in Unit. For a closed ring that is its perimeter.
	Length *measuredValue `json:"length,omitempty"`

	// Centroid is where the area is centred — the midpoint for an edge, the
	// point itself for a vertex — and never the mean of the corners.
	Centroid *measuredPoint `json:"centroid,omitempty"`

	// Bounds is the axis-aligned bounding box, on the frame's own axes and on no
	// others. It survives shapes which have no area: a ring with a gap in it
	// still reaches as far as it reaches.
	Bounds *measuredBox `json:"bounds,omitempty"`

	// Chord is the tolerance the rings were drawn to and Deviation how far the
	// worst segment of that drawing fell from the curve it stood in for.
	//
	// Both are absent for almost every measurement, which is the point of them.
	// Every figure above is computed from the arc itself, so the only thing a
	// drawing is ever needed for is deciding which of several rings holds which;
	// a measurement which carries neither was approximated nowhere.
	Chord     *toleranceEntry `json:"chord,omitempty"`
	Deviation *measuredValue  `json:"deviation,omitempty"`

	// Chorded is the edges of what was measured which state a curve this run
	// did not read, each with the predicates it states it under. Absent for a
	// run which read every curve and for a model which claims none.
	//
	// It is in the answer rather than only on stderr because the figures above
	// are about the straight lines between those edges' ends rather than about
	// the walls, and a consumer comparing a computed area against a stated one
	// has to be able to see that from the object it is comparing.
	Chorded []chordedEntry `json:"chorded,omitempty"`

	// Budget is the accuracy of the corners every figure was computed from,
	// broken out by term.
	//
	// It is the accuracy of the corners and not of the area, and it is not
	// reduced to a tolerance on any one figure above. How much an area moves
	// when a corner does is a per-corner quantity, and one number standing in
	// for all of them would read as an answer while being none.
	Budget *budgetReport `json:"budget,omitempty"`
}

// measuredValue is one figure with the unit it is in.
//
// The unit is on the figure rather than only at the top of the object because
// this payload carries figures in two units at once: a length is in the frame's
// linear unit and an area is in the square of it. A caller which had to know
// which of the fields were squared would be reading the contract out of prose.
type measuredValue struct {
	// Value is the figure, as it was computed.
	Value float64 `json:"value"`

	// Unit is what it is in: the frame's linear unit for a distance, and that
	// unit with a superscript two for an area.
	//
	// A squared unit is written that way rather than as a name of its own
	// because the engine has none to write. A frame declares one linear unit;
	// what a project calls a square metre is its own vocabulary, in its own
	// predicate declarations, and is not something a computed area may borrow.
	Unit string `json:"unit,omitempty"`
}

// measuredPoint is one point, with the unit its components are in.
type measuredPoint struct {
	// At is the components of the point, in the order they are written.
	At []float64 `json:"at"`

	// Unit is the linear unit they are in.
	Unit string `json:"unit,omitempty"`
}

// measuredBox is an axis-aligned bounding box, with the unit its corners are in.
//
// Both corners are written and the extent between them is not. It is one
// subtraction, and a field which restates it is a second place for it to be
// wrong ([0017](docs/decisions/0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md)).
type measuredBox struct {
	// Min and Max are the corners of the box, component by component.
	Min []float64 `json:"min"`
	Max []float64 `json:"max"`

	// Unit is the linear unit they are in.
	Unit string `json:"unit,omitempty"`
}

// runMeasure is the measure command.
func runMeasure(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

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

	if err := vocabularyOf(
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

	// Any of the four families answers, so the lookup is the whole of the
	// dispatch: what a measurement of an id is depends on which family holds it,
	// and a flag saying which would be a second statement of something the model
	// already knows.
	entity, held := graph.Entity(subject)
	if !held {
		nearest, _ := graph.Nearest(subject)
		return usageError(cmd, UnknownIDError{ID: string(subject), Nearest: string(nearest)}, stderr, false)
	}

	survey := bent(graph, *position, *tolerance, arcs{centre: *centre, through: *through, chord: *chord}, entity)

	measurement, diags := graph.Measure(entity, survey)

	// What the survey could not bend, reported before the answer is written: a
	// figure computed over a chord is still the answer, and saying which of its
	// edges is a chord is what makes it one somebody can act on.
	chorded, unread := chordedOf(graph, survey, entity)
	diags = append(diags, unread...)

	// The diagnostics are for whoever wrote the model, so they go to stderr on
	// every run and in every format, and whether any of them is an error is what
	// decides between an answer and a refusal.
	refused := render(diags, stderr)

	result := reportMeasure(cmd, graph, subject, entity, measurement, chorded, *tolerance, !refused)

	reportMeasureFor(result, measurement, globals, stderr)

	if err := emit(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	if refused {
		return exitCheck
	}

	return exitSuccess
}

// reportMeasure is the measurement as the machine contract writes it.
//
// The subject is the id the run was asked about rather than the one the answer
// carries, because a refused measurement carries none: a caller collecting
// results has to be able to say which question this object answers, and a
// refusal is exactly the object it most needs that of.
func reportMeasure(
	cmd command,
	graph *dfcad.Graph,
	subject dfcad.ID,
	entity dfcad.Entity,
	measurement dfcad.Measurement,
	chorded []chordedEntry,
	tolerance string,
	ok bool,
) measureResult {
	result := measureResult{
		envelope: newEnvelope(cmd.name),
		Subject:  string(subject),
		Family:   familyOf(entity),
		Derived:  ok,
		Chorded:  chorded,
	}

	if digest, known := graph.Digest(); known {
		result.Digest = digest.String()
	}

	if !ok {
		return result
	}

	unit := string(measurement.Unit())

	frame, _ := frameOf(entity)
	result.Frame = string(frame)
	result.Unit = unit

	if found, held := graph.Registry().Tolerance(tolerance); held {
		entry := declared(found)
		result.Tolerance = &entry
	}

	if area, computed := measurement.Area(); computed {
		result.Area = &measuredValue{Value: area, Unit: squared(unit)}
	}

	if length, computed := measurement.Length(); computed {
		result.Length = &measuredValue{Value: length, Unit: unit}
	}

	if centroid, computed := measurement.Centroid(); computed {
		result.Centroid = &measuredPoint{At: centroid[:], Unit: unit}
	}

	if bounds, computed := measurement.Bounds(); computed {
		result.Bounds = &measuredBox{Min: bounds.Min[:], Max: bounds.Max[:], Unit: string(bounds.Unit)}
	}

	// Both are properties of a drawing, and almost no measurement is one: the
	// figures above come from the arc itself, and the only thing which is ever
	// drawn is the nesting of several rings. Writing them anyway would put a
	// tolerance with no name and a deviation from nothing into every answer,
	// which reads as an approximation where there was none.
	if drawn, made := measurement.ChordTolerance(); made {
		chord := declared(drawn)
		result.Chord = &chord

		result.Deviation = &measuredValue{Value: measurement.Deviation(), Unit: unit}
	}

	// A budget with nothing in it is left out rather than written empty. An
	// absent "combined" means the terms could not be reduced to one figure, and
	// "unknown" and "units" beside it are what say why; an object carrying
	// neither the figure nor a reason for its absence reads as an answer known
	// exactly, which is the one thing a budget must never look like. A
	// measurement computed from no claim at all — a node which references no
	// loop — has no accuracy to report, and says that by reporting none.
	if budget := budgetOf(measurement.Budget()); !empty(budget) {
		result.Budget = &budget
	}

	return result
}

// empty reports whether a budget says nothing: no terms, no combined figure, and
// no reason for there being none.
func empty(budget budgetReport) bool {
	return len(budget.Terms) == 0 &&
		budget.Combined == nil &&
		len(budget.Unknown) == 0 &&
		len(budget.Units) == 0
}

// squared is the unit an area is in: the frame's linear unit, with a superscript
// two after it, and nothing at all where the frame declares no unit.
func squared(unit string) string {
	if unit == "" {
		return ""
	}
	return unit + "²"
}

// reportMeasureFor renders a measurement for a person, on stderr.
//
// Nothing here reaches stdout, in any format and at any verbosity: stdout is the
// same bytes whether or not anybody asked to read the run.
func reportMeasureFor(result measureResult, measurement dfcad.Measurement, globals *globals, stderr io.Writer) {
	if !globals.human() || !result.Derived {
		return
	}

	// The accuracy behind the figures is the detail under the summary, so it is
	// behind the verbosity flag — and it is rendered by the library rather than
	// spelled again here, so that a caller reporting the answer and this command
	// reporting it write the same thing.
	if globals.Verbosity >= verbosityProgress {
		_, _ = fmt.Fprintf(stderr, "%s\n", measurement.Report())
		return
	}

	_, _ = fmt.Fprintf(stderr, "%s\n", measurement)
}
