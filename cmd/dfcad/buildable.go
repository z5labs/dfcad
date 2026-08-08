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

const buildableUsage = `dfcad buildable — derive what may be built inside a boundary once its setbacks
are taken off it.

Usage:

	dfcad buildable [flags] <id>

The buildable area of a plot, computed from the plot's boundary and from the
setback claimed on each of its edges. Nothing in the model says what is
buildable and nothing here writes it back: the region is read out of the
corners, the edges and the claims every time it is asked for, so it cannot
disagree with any of them. A buildable area authored as a polygon of its own is
a second statement of where a permanent structure may go, and the day a setback
claim changes it is the wrong one.

Flags:

	--setback <predicate>    the predicate an edge's setback distance is
	                         claimed under (required)
	--position <predicate>   the predicate a corner's position is claimed
	                         under, which is what the boundary is read from
	                         (required)
	--tolerance <name>       the tolerance corners are judged coincident
	                         against and rounded corners are drawn to
	                         (required)

None of the three has a default and none of them ever will. Which predicate
carries a setback, which carries a position, and how close two corners have to
be to be one corner, are things the project wrote down, and a value compiled in
here would be the engine deciding one of them on a project's behalf.

Different setbacks per edge are the ordinary case: six metres at the road, four
at the rear, three at each flank. Which edge is which is not modelled — a
setback is a claim written on the edge it governs, so the numbers go on the
edges and each is applied where it was written.

An edge with no live setback claim is a failure naming that edge, never a
setback of nought. An edge which really is not set back says so, as a claim
with a value of nought and the provenance every other value carries.

Setbacks which meet in the middle leave nothing buildable. That is the answer
rather than a failure: the run succeeds, "empty" is true, and a warning on
stderr says which parcel its own regime consumed. What never comes back is the
inside-out shape offsetting each edge on its own produces when the offsets
cross over each other.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object buildable writes carries "subject", "derived" and the "digest" of
the source tree it was derived from, the "frame" and "unit" it is expressed
in, the "tolerance" it was judged against, the "parcel" it was
derived from, the "setbacks" applied one per edge, the "region" left buildable
with the ring of every piece of it, and the "budget": the accuracy of the
answer broken out by term, over the position claims and the setback claims
together.

Exit code 1 is a derivation which could not be made — an edge nothing states
the setback of, two claims equally current about one, a boundary which could
not be read. The object still comes back, with "derived" false, so a caller
reads why from the diagnostics on stderr rather than from an empty stream.
`

// The flags buildable takes beyond the global ones, named here because the
// errors which refuse them name them.
const (
	flagSetback   = "setback"
	flagPosition  = "position"
	flagTolerance = "tolerance"
)

// MissingVocabularyError is a run which did not say which predicate or which
// tolerance to read the model against.
//
// It is one error over all of them rather than one each, because they are one
// mistake: a query is asked in the project's own vocabulary, and a run which
// supplied part of it has not asked a question yet. Naming every missing flag
// at once is what makes fixing it one edit.
type MissingVocabularyError struct {
	// Flags are the flags which were not given, spelled without their dashes,
	// in the order the usage lists them.
	Flags []string
}

// Error implements [error].
func (e MissingVocabularyError) Error() string {
	spelled := make([]string, 0, len(e.Flags))
	for _, flag := range e.Flags {
		spelled = append(spelled, "--"+flag)
	}

	return fmt.Sprintf(
		"expected the vocabulary to ask in, found no %s: which predicate carries which measurement, and how close "+
			"two corners have to be to be one corner, are project data and have no default here",
		join(spelled),
	)
}

// given is one vocabulary flag and what a run passed for it.
type given struct {
	// flag is the flag's name, spelled without its dashes.
	flag string

	// value is what was passed for it, empty where nothing was.
	value string
}

// buildableResult is the object buildable writes to stdout.
type buildableResult struct {
	envelope

	// Subject is the id the derivation was asked about, which is written
	// whether or not there was an answer: a refusal a caller cannot attribute
	// to a question is one it has to correlate by position.
	Subject string `json:"subject"`

	// Derived reports whether there is a region below. It is written whatever
	// the outcome, so that a caller can tell a parcel whose setbacks left
	// nothing of it from one whose setbacks could not be read: the first is
	// derived and empty, the second is neither.
	Derived bool `json:"derived"`

	// Digest is the digest of the source tree the region was derived from,
	// which is what lets a caller check the derivation against the model in
	// front of them rather than taking it on trust. It is written on a refusal
	// too: a refusal is still about a tree, and saying which one is the point.
	//
	// Absent for a model which was not read from disk, or one a file of which
	// could not be read at all, because there is then nothing anything may be
	// keyed by.
	Digest string `json:"digest,omitempty"`

	// Frame is the coordinate frame the boundary and the answer are expressed
	// in, and Unit that frame's linear unit, which every distance here is in
	// and every area in the square of.
	Frame string `json:"frame,omitempty"`
	Unit  string `json:"unit,omitempty"`

	// Tolerance is what corners were judged coincident against and rounded
	// corners drawn to.
	Tolerance *toleranceEntry `json:"tolerance,omitempty"`

	// Parcel is the boundary the setbacks were taken off, as the model holds
	// it.
	Parcel *regionEntry `json:"parcel,omitempty"`

	// Setbacks are the setbacks which were applied, one per edge of the
	// boundary and in the order its loops traverse them.
	Setbacks []setbackEntry `json:"setbacks,omitempty"`

	// Region is what is left buildable. It is written for a derivation which
	// succeeded whether or not it covers anything: a region covering nothing is
	// an answer, and omitting it would make it look like one which was never
	// computed.
	Region *regionEntry `json:"region,omitempty"`

	// Budget is the accuracy of the answer, broken out by term, over the
	// position claims which put the corners where they are and the setback
	// claims which pushed each edge back.
	Budget *budgetReport `json:"budget,omitempty"`
}

// regionEntry is one region as the machine contract writes it.
type regionEntry struct {
	// Area is what it covers, holes taken away, in the square of the unit.
	Area float64 `json:"area"`

	// Empty reports whether it covers nothing, which is a state of the answer
	// rather than an absence of one.
	Empty bool `json:"empty"`

	// Pieces are the connected parts it covers, each with the ring bounding it
	// and the rings taken out of it.
	Pieces []pieceEntry `json:"pieces"`

	// Boundary is which edge produced each straight run of the boundary, in the
	// order the rings are traversed. It is what lets a ring be attributed back
	// to the model it came from rather than arriving as anonymous coordinates.
	//
	// Only the runs an edge is behind are written, which is every run of a
	// region read from the model and none of a region an operation produced. An
	// intersection's boundary runs partly along each operand and partly along
	// where they cross, and writing its corners a second time under a name which
	// says "an operation put this here" would double the payload to say what
	// `pieces` already says. Absent where there is nothing to attribute, which
	// `derived` on the result tells apart from a region nothing was computed
	// for.
	Boundary []segmentEntry `json:"boundary,omitempty"`
}

// pieceEntry is one connected part of a region.
//
// The holes are carried beside the outer ring rather than folded into it,
// because neither is derivable from the other: a plot with a protected tree in
// the middle of it is one piece with a hole, and a plot a watercourse cuts in
// two is two pieces with none.
type pieceEntry struct {
	// Area is what the piece encloses once its holes are taken away.
	Area float64 `json:"area"`

	// Outer is the ring bounding it, closed without repeating its first corner
	// at the end.
	Outer [][]float64 `json:"outer"`

	// Holes are the rings taken out of it. Absent where there are none.
	Holes [][][]float64 `json:"holes,omitempty"`
}

// segmentEntry is one straight run of a region's boundary and the edge which
// produced it.
type segmentEntry struct {
	// Ring is which ring of the boundary the run belongs to, counted from zero
	// in the order the rings are traversed. A run of a courtyard and a run of
	// the plate around it are the same shape of object, and this is what says
	// which ring a caller is closing.
	Ring int `json:"ring"`

	// Edge is the id of the edge the run was written as, or whose arc it stands
	// in for.
	Edge string `json:"edge"`

	// Origin is what produced the run: `edge` where it is the edge itself,
	// corner to corner as it was written, and `arc` where it is one chord of the
	// drawing of the arc that edge bends along. A chord is not the edge, and
	// carrying it back into a model as one would write a straight wall where a
	// curved one is.
	Origin string `json:"origin"`

	// Reversed reports whether the run goes against the order the edge was
	// written. Two regions either side of a party wall name one edge and
	// traverse it opposite ways, so a caller which read the edge's own vertices
	// and assumed the run followed them would draw one of them inside out.
	Reversed bool `json:"reversed"`

	// From is the corner the run leaves and To the one it arrives at, each as
	// its components, in the order the ring is traversed.
	From []float64 `json:"from"`
	To   []float64 `json:"to"`
}

// setbackEntry is one edge's setback and the claim it came from.
type setbackEntry struct {
	// Edge is the id of the edge it was claimed on.
	Edge string `json:"edge"`

	// Distance is how far back it pushes the boundary, in Unit.
	Distance float64 `json:"distance"`

	// Unit is the unit that distance is in, which is the frame's.
	Unit string `json:"unit,omitempty"`

	// Claim is the id of the claim it was resolved from, and is absent for one
	// which wrote none — which is the majority of claims.
	Claim string `json:"claim,omitempty"`

	// Source names the evidence the distance came from: a consent, a statute,
	// a deed.
	Source string `json:"source,omitempty"`

	// Span is where that claim was written.
	Span dfcad.Span `json:"span"`
}

// runBuildable is the buildable command.
func runBuildable(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	setback := flags.String(flagSetback, "", "")
	position := flags.String(flagPosition, "", "")
	tolerance := flags.String(flagTolerance, "", "")

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
		given{flagSetback, *setback},
		given{flagPosition, *position},
		given{flagTolerance, *tolerance},
	); err != nil {
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

	survey := dfcad.Survey{Tolerance: *tolerance, Registry: graph.Registry()}
	for vertex := range graph.Vertices(node) {
		resolution, err := graph.Claims().Resolve(vertex.ID(), *position, graph.Registry())
		if err != nil {
			continue
		}
		survey.Place(vertex.ID(), resolution)
	}

	derived, diags := graph.Topology().BuildableOf(node, graph.Boundaries(), survey, dfcad.Setbacks{
		Predicate: *setback,
		Claims:    graph.Claims(),
	})

	// The diagnostics are for whoever wrote the model, so they go to stderr on
	// every run and in every format, and whether any of them is an error is what
	// decides between an answer and a refusal.
	refused := render(diags, stderr)

	result := reportBuildable(cmd, graph, subject, derived, !refused)

	reportBuildableFor(result, derived, globals, stderr)

	if err := emit(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	if refused {
		return exitCheck
	}

	return exitSuccess
}

// vocabularyOf reports the vocabulary flags a run did not give, which is the one
// thing a command asked in a project's own words cannot supply for itself.
//
// It is variadic over the flags rather than fixed to one command's, because
// which words a query needs is a property of the query and refusing to guess at
// them is a property of every one of them.
func vocabularyOf(asked ...given) error {
	var missing []string

	for _, one := range asked {
		if one.value == "" {
			missing = append(missing, one.flag)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	return MissingVocabularyError{Flags: missing}
}

// reportBuildable is the derivation as the machine contract writes it.
//
// The subject is the id the run was asked about rather than the one the answer
// carries, because a refused derivation carries none: a caller collecting
// results has to be able to say which question this object answers, and a
// refusal is exactly the object it most needs that of.
func reportBuildable(
	cmd command,
	graph *dfcad.Graph,
	subject dfcad.ID,
	derived dfcad.Buildable,
	ok bool,
) buildableResult {
	result := buildableResult{
		envelope: newEnvelope(cmd.name),
		Subject:  string(subject),
		Derived:  ok,
	}

	if digest, known := graph.Digest(); known {
		result.Digest = digest.String()
	}

	if !ok {
		return result
	}

	boundary := derived.Boundary()

	result.Frame = string(boundary.Frame())
	result.Unit = string(boundary.Unit())

	entry := declared(boundary.Tolerance())
	result.Tolerance = &entry

	parcel := regionOf(boundary)
	result.Parcel = &parcel

	region := regionOf(derived.Region())
	result.Region = &region

	result.Setbacks = make([]setbackEntry, 0, len(derived.Setbacks()))
	for _, setback := range derived.Setbacks() {
		result.Setbacks = append(result.Setbacks, setbackOf(setback))
	}

	budget := budgetOf(derived.Budget())
	result.Budget = &budget

	return result
}

// regionOf is one region as the machine contract writes it.
func regionOf(region dfcad.Region) regionEntry {
	entry := regionEntry{
		Area:   region.Area(),
		Empty:  region.Empty(),
		Pieces: make([]pieceEntry, 0, len(region.Pieces())),
	}

	for _, piece := range region.Pieces() {
		written := pieceEntry{Area: piece.Area(), Outer: ring(piece.Outer())}
		for _, hole := range piece.Holes() {
			written.Holes = append(written.Holes, ring(hole))
		}
		entry.Pieces = append(entry.Pieces, written)
	}

	for _, segment := range region.Segments() {
		edge := segment.Edge()
		if edge == nil {
			// A run an operation produced attributes to nothing, and the region
			// it belongs to is already written as derived. Writing it anyway
			// would repeat the coordinates above under a name which names no
			// edge.
			continue
		}

		from, to := segment.From(), segment.To()

		entry.Boundary = append(entry.Boundary, segmentEntry{
			Ring:     segment.Ring(),
			Edge:     string(edge.ID()),
			Origin:   segment.Origin().String(),
			Reversed: segment.Reversed(),
			From:     from[:],
			To:       to[:],
		})
	}

	return entry
}

// ring is one closed ring of corners as the machine contract writes it: the
// components of each corner, in the order they were written.
func ring(points []dfcad.Point) [][]float64 {
	out := make([][]float64, 0, len(points))
	for _, point := range points {
		out = append(out, point[:])
	}
	return out
}

// setbackOf is one applied setback as the machine contract writes it.
func setbackOf(setback dfcad.Setback) setbackEntry {
	entry := setbackEntry{
		Distance: setback.Distance(),
		Unit:     string(setback.Unit()),
	}

	if edge := setback.Edge(); edge != nil {
		entry.Edge = string(edge.ID())
	}

	if claim := setback.Claim(); claim != nil {
		if id, ok := claim.ID(); ok {
			entry.Claim = string(id)
		}
		entry.Source = claim.Source()
		entry.Span = claim.Span()
	}

	return entry
}

// reportBuildableFor renders a derivation for a person, on stderr.
//
// Nothing here reaches stdout, in any format and at any verbosity: stdout is
// the same bytes whether or not anybody asked to read the run.
func reportBuildableFor(result buildableResult, derived dfcad.Buildable, globals *globals, stderr io.Writer) {
	if !globals.human() || !result.Derived {
		return
	}

	// The setback behind each edge is the detail under the summary, so it is
	// behind the verbosity flag — and it is rendered by the library rather than
	// spelled again here, so that a caller reporting the answer and this command
	// reporting it write the same thing.
	if globals.Verbosity >= verbosityProgress {
		_, _ = fmt.Fprintf(stderr, "%s\n", derived.Report())
		return
	}

	_, _ = fmt.Fprintf(stderr, "%s\n", derived)
}
