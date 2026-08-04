// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/z5labs/dfcad"
)

const getUsage = `dfcad get — retrieve one thing by its id, with what is claimed about it.

Usage:

	dfcad get [flags] <id>

The thing the id names, with its axes, its label, its frame, the references it
wrote and the claims written on it. Retrieving the subject gets the evidence
with it: the claims are inlined on the node they were written on, so provenance
is this one call rather than a second lookup.

An id is unique across the whole model, so this is one command for both
families. A vertex, an edge and a loop are retrieved by the same call a
semantic node is, and the "family" field of the answer says which came back and
so which of the fields to expect.

Flags:

	--claims <how>   full, which is every claim written on it, or resolved,
	                 which is the current claim under each predicate
	                 (default "full")
	--deprecated     include the claims which have been deprecated

References come back as ids and are never inlined, so the answer is the size of
the thing asked for rather than of the model behind it. Following one is another
call.

Deprecated claims are left out unless they are asked for. A deprecated claim is
retracted rather than out-ranked, and resolution never considers one — which is
why --deprecated says nothing under --claims resolved and is refused there
rather than ignored.

An id nothing in the model holds is a usage error naming it, and naming the
nearest id there is when one is close enough to be the id that was meant. It is
not an empty answer: a thing which is not there and a thing with nothing said
about it are different answers, and a caller which cannot tell them apart
retries a misspelling forever.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object get writes carries "entity": the thing found, its axes, the ids it
references, where it was written, and its claims in predicate order.
`

// The ways get reports the claims written on the thing it found.
const (
	// claimsFull is every claim, which is what the model says.
	claimsFull = "full"

	// claimsResolved is the current claim under each predicate, which is what
	// the resolution rule makes of what the model says.
	claimsResolved = "resolved"
)

// claimSelections is every legal --claims value, in the order the usage lists
// them.
var claimSelections = []string{claimsFull, claimsResolved}

// What one claim was left as by resolution, reported under --claims resolved
// and never otherwise.
const (
	// resolutionCurrent is the claim which won outright.
	resolutionCurrent = "current"

	// resolutionTied is one of several claims the rule could not separate,
	// whether because they are equally accurate and equally recent or because
	// nothing rankable was said about any of them. Several unrankable claims are
	// equally current in the same way equally good ones are, so they read the
	// same way here.
	resolutionTied = "tied"

	// resolutionUnranked is the one live claim under a predicate nothing
	// rankable was said about, so nothing won and there is nothing for it to be
	// tied with.
	resolutionUnranked = "unranked"
)

// The families an id can name, which is what says which fields of the answer to
// expect. They are the tags the forms are written with, so that the answer
// names a family the way the file does.
const (
	familyNode   = "node"
	familyVertex = "vertex"
	familyEdge   = "edge"
	familyLoop   = "loop"
)

// ErrMissingID is a get with no id to get.
var ErrMissingID = errors.New("expected the id of the thing to get, found no argument")

// ErrDeprecatedNotResolvable is --deprecated asked for beside --claims
// resolved.
//
// It is refused rather than ignored because the two cannot both be honoured: a
// deprecated claim is retracted rather than out-ranked, and resolution never
// sees one, so a run which accepted both would answer as though the flag had
// not been written. A flag which is silently ignored is worse than one which
// does not exist.
var ErrDeprecatedNotResolvable = errors.New(
	"--deprecated says nothing under --claims resolved: a deprecated claim is retracted rather than " +
		"out-ranked, and resolution never considers one",
)

// UnknownClaimsError is a --claims which names neither way of reporting them.
type UnknownClaimsError struct {
	// Selection is what was asked for.
	Selection string

	// Known is every way there is.
	Known []string
}

// Error implements [error].
func (e UnknownClaimsError) Error() string {
	return fmt.Sprintf("unknown claims %q: want one of %s", e.Selection, strings.Join(e.Known, ", "))
}

// UnknownIDError is an id nothing in the model holds.
//
// It carries the nearest id rather than only saying there was none, because the
// great majority of ids which reach nothing are the id which was meant with a
// character wrong, and a caller told only that its lookup failed goes back to
// reading files to find out which character.
type UnknownIDError struct {
	// ID is what was asked for.
	ID string

	// Nearest is the id the model does hold which is close enough to be a
	// misspelling of it. Empty when nothing is close.
	Nearest string
}

// Error implements [error].
func (e UnknownIDError) Error() string {
	if e.Nearest != "" {
		return fmt.Sprintf("unknown id %s: did you mean %s?", e.ID, e.Nearest)
	}
	return fmt.Sprintf(
		"unknown id %s: nothing in this model holds it; run `dfcad list-instances` for the nodes it does hold",
		e.ID,
	)
}

// getResult is the object get writes to stdout.
type getResult struct {
	envelope

	// Entity is the thing the id named.
	Entity getEntity `json:"entity"`
}

// getEntity is one thing the model holds, as get reports it.
//
// It is one shape for all four families rather than one per family, with
// "family" saying which came back and which of the fields it carries. A caller
// holding an id out of a file or a diagnostic does not know which family will
// answer, and a payload whose top-level key depended on that would have to be
// probed before it could be read.
//
// Every reference is an id and is never the thing it names. That is what keeps
// the answer the size of the thing asked for: retrieving a site would otherwise
// retrieve the model.
type getEntity struct {
	// ID is the id the model holds it under, which is the id asked for.
	ID string `json:"id"`

	// Family is which family holds it: node, vertex, edge or loop.
	Family string `json:"family"`

	// Label is its name for a person reading it. Absent when it was not
	// written.
	Label string `json:"label,omitempty"`

	// Kind is the kind a semantic node declares.
	Kind string `json:"kind,omitempty"`

	// Type is the type a semantic node declares, which need not be one the
	// registry declares.
	Type string `json:"type,omitempty"`

	// Geometry is the geometry form a semantic node declares. Absent when it
	// has none, which is ordinary rather than incomplete.
	Geometry string `json:"geometry,omitempty"`

	// Frame is the id of the coordinate frame it is expressed in. Absent when
	// it declares none.
	Frame string `json:"frame,omitempty"`

	// Within is the id of the node which strictly contains a semantic node.
	// Absent when it wrote none.
	Within string `json:"within,omitempty"`

	// MemberOf are the ids of the zones a semantic node declares membership of,
	// in the order it wrote them.
	MemberOf []string `json:"member-of,omitempty"`

	// Boundaries are the ids a semantic node wrote where a loop id belongs, in
	// the order it wrote them, and as written rather than as resolved.
	Boundaries []string `json:"boundaries,omitempty"`

	// Start and End are the ids of the vertices an edge runs between.
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`

	// BackedBy are the ids of the elements which physically realise an edge, in
	// the order it wrote them.
	BackedBy []string `json:"backed-by,omitempty"`

	// Edges are the ids of the edges a loop is assembled from, in the order it
	// wrote them.
	Edges []string `json:"edges,omitempty"`

	// Span is where it was written: the file, the line and the column, which is
	// what sends a reader to the definition rather than to a search.
	Span dfcad.Span `json:"span"`

	// Claims are the claims written on it, in predicate order. Empty rather
	// than null when nothing is claimed about it.
	Claims []claimEntry `json:"claims"`
}

// claimEntry is one claim as get reports it: the value, and the evidence for
// it.
//
// The provenance is not optional detail. A dimension in this model is a value
// plus where it came from, how it was obtained, how good it is and when, and a
// retrieval which reported the number alone would be handing back the one thing
// the format exists to stop being handed back on its own.
type claimEntry struct {
	// ID is the claim's own id. Absent when it wrote none, which is the great
	// majority of claims: an id is required only of one something references.
	ID string `json:"id,omitempty"`

	// Predicate is the predicate it was written under.
	Predicate string `json:"predicate"`

	// Value is the value and the unit it is expressed in.
	Value claimValue `json:"value"`

	// Source names the evidence — a report, a drawing, an instrument log.
	Source string `json:"source,omitempty"`

	// Method is the id naming how the value was obtained.
	Method string `json:"method,omitempty"`

	// Accuracy is how well the value is known, one entry per term. Absent when
	// the claim carries none, which makes it unrankable rather than exact.
	Accuracy []accuracyTerm `json:"accuracy,omitempty"`

	// Date is the day the value was obtained, as the full date it was written
	// as.
	Date string `json:"date,omitempty"`

	// Rank is the claim's rank, which is reported whether or not it was
	// written: a claim nobody ranked is normal, and saying so costs a field.
	Rank string `json:"rank"`

	// SupersededBy is the id of the claim which replaced this one. Absent
	// unless it was deprecated in favour of one.
	SupersededBy string `json:"superseded-by,omitempty"`

	// Resolution is what the rule left this claim as: current, tied or
	// unranked. It is written under --claims resolved and is absent otherwise,
	// because under --claims full nothing has been resolved.
	Resolution string `json:"resolution,omitempty"`

	// Span is where the claim was written.
	Span dfcad.Span `json:"span"`
}

// claimValue is one claim's value, in whichever of the four shapes its
// predicate declares.
//
// The shape is named rather than inferred from which field is set, so that a
// caller reads the field its shape says to read instead of probing four of
// them.
type claimValue struct {
	// Shape is which of the four shapes the value takes.
	Shape string `json:"shape"`

	// Unit is the unit it is expressed in, as it was written. Absent for a
	// non-dimensional predicate and for the shapes which carry no unit.
	Unit string `json:"unit,omitempty"`

	// Scalar is a scalar value. It is a pointer so that a claim of zero is a
	// zero rather than a field which went missing.
	Scalar *float64 `json:"scalar,omitempty"`

	// Coordinate is the ordered components of a coordinate value. The order is
	// significant and is never sorted.
	Coordinate []float64 `json:"coordinate,omitempty"`

	// Text is a text value. It is a pointer for the reason Scalar is: the empty
	// string is a text value a claim can legally hold, and a field which went
	// missing would read as a value which could not be read at all.
	Text *string `json:"text,omitempty"`

	// Transform is a transform value.
	Transform *claimTransform `json:"transform,omitempty"`
}

// claimTransform is a rigid transform value.
type claimTransform struct {
	// Translation is the offset, ordered tx, ty, tz.
	Translation []float64 `json:"translation"`

	// Rotation is a 3x3 matrix in row-major order.
	Rotation []float64 `json:"rotation"`

	// Scale is the one scale factor.
	Scale float64 `json:"scale"`
}

// accuracyTerm is one term of a claim's accuracy.
type accuracyTerm struct {
	// Kind is which of the two kinds of error it describes: independent or
	// systematic.
	Kind string `json:"kind"`

	// Magnitude is the one-sigma figure.
	Magnitude float64 `json:"magnitude"`

	// Unit is the unit that figure is expressed in.
	Unit string `json:"unit,omitempty"`

	// Source is the id a systematic error is shared with. Absent for an
	// independent term, which is shared with nothing.
	Source string `json:"source,omitempty"`
}

// runGet is the get command.
func runGet(cmd command, args []string, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd.name, globals)

	selection := flags.String("claims", claimsFull, "")
	deprecated := flags.Bool("deprecated", false, "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(arguments) == 0 {
		return usageError(cmd, ErrMissingID, stderr, true)
	}
	if len(arguments) > 1 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments[1:]}, stderr, true)
	}

	if err := checkClaims(*selection, *deprecated); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	// An argument which is not an id is a different mistake from an id nothing
	// holds, and the production it broke is a better answer than a lookup which
	// was never going to find anything: no id in a model is malformed, because
	// a malformed one is a diagnostic rather than a name anything is held under.
	id, err := dfcad.ParseID(arguments[0])
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	graph := loadModel(cmd, globals, stderr)

	entity, ok := graph.Entity(id)
	if !ok {
		nearest, _ := graph.Nearest(id)
		return usageError(cmd, UnknownIDError{ID: string(id), Nearest: string(nearest)}, stderr, false)
	}

	result := getResult{
		envelope: newEnvelope(cmd.name),
		Entity:   describe(graph, entity, *selection, *deprecated),
	}

	reportEntity(result.Entity, globals, stderr)

	if err := emit(stdout, result); err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// checkClaims reports a --claims which names no way of reporting them, and a
// --deprecated which cannot be honoured beside the one it does name.
func checkClaims(selection string, deprecated bool) error {
	if !slices.Contains(claimSelections, selection) {
		return UnknownClaimsError{Selection: selection, Known: claimSelections}
	}

	if deprecated && selection == claimsResolved {
		return ErrDeprecatedNotResolvable
	}

	return nil
}

// describe is one entity as the answer reports it, whichever family holds it.
func describe(graph *dfcad.Graph, entity dfcad.Entity, selection string, deprecated bool) getEntity {
	out := getEntity{
		ID:     string(entity.ID()),
		Span:   entity.Span(),
		Claims: claimsOf(graph, entity.ID(), selection, deprecated),
	}

	switch found := entity.(type) {
	case *dfcad.SemanticNode:
		out.Family = familyNode
		out.Label = found.Label()
		out.Kind = string(found.Kind())
		out.Type = found.Type()
		if geometry, ok := found.Geometry(); ok {
			out.Geometry = string(geometry)
		}
		if frame, ok := found.Frame(); ok {
			out.Frame = string(frame)
		}
		if within, ok := found.Within(); ok {
			out.Within = string(within)
		}
		out.MemberOf = spellings(found.MemberOf())
		out.Boundaries = spellings(found.Boundaries())

	case *dfcad.Vertex:
		out.Family = familyVertex
		out.Label = found.Label()
		out.Frame = string(found.Frame())

	case *dfcad.Edge:
		out.Family = familyEdge
		out.Label = found.Label()
		out.Frame = string(found.Frame())
		start, end := found.Vertices()
		out.Start = string(start)
		out.End = string(end)
		out.BackedBy = spellings(found.BackedBy())

	case *dfcad.Loop:
		out.Family = familyLoop
		out.Label = found.Label()
		out.Frame = string(found.Frame())
		out.Edges = spellings(found.Edges())
	}

	return out
}

// claimsOf is the claims written on one subject, as the answer reports them.
func claimsOf(graph *dfcad.Graph, subject dfcad.ID, selection string, deprecated bool) []claimEntry {
	// Made rather than declared so that a thing nothing is claimed about
	// carries an empty list rather than a null, and a caller indexing it needs
	// no special case for the thing nobody has measured yet.
	out := make([]claimEntry, 0)

	if selection == claimsResolved {
		out = append(out, resolved(graph, subject)...)
	} else {
		for claim := range graph.Claims().Of(subject) {
			if claim.Rank() == dfcad.RankDeprecated && !deprecated {
				continue
			}
			out = append(out, entryOf(claim, ""))
		}
	}

	inPredicateOrder(out)

	return out
}

// inPredicateOrder sorts claims by predicate, and then by where each was
// written.
//
// Grouping by predicate is what makes two claims of one width readable as the
// disagreement they are, and it does not change when a claim moves between files
// while the model says the same thing. The claim's own id breaks the remaining
// tie, so the order is total and two runs over one model diff to nothing.
func inPredicateOrder(claims []claimEntry) {
	slices.SortStableFunc(claims, func(a, b claimEntry) int {
		return cmp.Or(
			strings.Compare(a.Predicate, b.Predicate),
			strings.Compare(a.Span.Start.Path, b.Span.Start.Path),
			cmp.Compare(a.Span.Start.Offset, b.Span.Start.Offset),
			strings.Compare(a.ID, b.ID),
		)
	})
}

// resolved is what the resolution rule makes of the claims of one subject: the
// claim which is current under each predicate, and every claim it could not
// choose between where it could not choose.
//
// A predicate whose every claim is deprecated contributes nothing, because
// resolution never considers a deprecated claim. That is the same exclusion
// --claims full applies by default, arrived at by the rule rather than by a
// second one written here.
func resolved(graph *dfcad.Graph, subject dfcad.ID) []claimEntry {
	var out []claimEntry

	for _, predicate := range predicatesOf(graph, subject) {
		// The error is a predicate the registry declares strict resolving to
		// more than one claim, and the resolution comes back beside it carrying
		// every one of them. Reporting what the model says is this command's
		// whole job; whether an ambiguity under a strict predicate is a failure
		// is what `dfcad check` answers, and answering it twice, in two
		// commands, is how the two come to disagree.
		resolution, _ := graph.Claims().Resolve(subject, predicate, graph.Registry())

		if claim, ok := resolution.Claim(); ok {
			out = append(out, entryOf(claim, resolutionCurrent))
			continue
		}

		// Nothing won. Either the rule could not separate the candidates or
		// nothing rankable was said about the predicate at all, and the honest
		// answer to both is every claim which could still be it — narrowing
		// four claims to two is most of the work of deciding between them, and
		// a caller shown one of the two cannot tell the other exists.
		state := resolutionUnranked
		if resolution.Ambiguous() {
			state = resolutionTied
		}
		for _, claim := range resolution.Candidates() {
			out = append(out, entryOf(claim, state))
		}
	}

	return out
}

// predicatesOf is the predicates written on one subject, in the order they were
// written and with a repeated one named once.
func predicatesOf(graph *dfcad.Graph, subject dfcad.ID) []string {
	var out []string

	seen := make(map[string]struct{})
	for claim := range graph.Claims().Of(subject) {
		if _, written := seen[claim.Predicate()]; written {
			continue
		}
		seen[claim.Predicate()] = struct{}{}
		out = append(out, claim.Predicate())
	}

	return out
}

// entryOf is one claim as the answer reports it, left as state by resolution.
func entryOf(claim *dfcad.Claim, state string) claimEntry {
	entry := claimEntry{
		Predicate:  claim.Predicate(),
		Value:      valueOf(claim.Value()),
		Source:     claim.Source(),
		Method:     string(claim.Method()),
		Rank:       string(claim.Rank()),
		Resolution: state,
		Span:       claim.Span(),
	}

	if id, ok := claim.ID(); ok {
		entry.ID = string(id)
	}
	if accuracy, ok := claim.Accuracy(); ok {
		for _, term := range accuracy.Terms {
			entry.Accuracy = append(entry.Accuracy, accuracyTerm{
				Kind:      string(term.Kind),
				Magnitude: term.Magnitude,
				Unit:      string(term.Unit),
				Source:    string(term.Source),
			})
		}
	}
	if date := claim.Date(); !date.IsZero() {
		entry.Date = date.Format(time.DateOnly)
	}
	if replacement, ok := claim.SupersededBy(); ok {
		entry.SupersededBy = string(replacement)
	}

	return entry
}

// valueOf is one claim's value in the shape its predicate declares.
func valueOf(value dfcad.Value) claimValue {
	out := claimValue{Shape: string(value.Shape()), Unit: string(value.Unit())}

	if scalar, ok := value.Scalar(); ok {
		out.Scalar = &scalar
	}
	if coordinate, ok := value.Coordinate(); ok {
		out.Coordinate = coordinate
	}
	if text, ok := value.Text(); ok {
		out.Text = &text
	}
	if transform, ok := value.Transform(); ok {
		out.Transform = &claimTransform{
			Translation: transform.Translation[:],
			Rotation:    transform.Rotation[:],
			Scale:       transform.Scale,
		}
	}

	return out
}

// reportEntity renders a get result for a person, on stderr.
//
// Nothing here reaches stdout, in any format and at any verbosity: stdout is
// the same bytes whether or not anybody asked to read the run.
func reportEntity(entity getEntity, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	for _, claim := range entity.Claims {
		// The claims themselves are already the result, on stdout, so the
		// reading of them is progress rather than result.
		if globals.Verbosity >= verbosityProgress {
			fmt.Fprintf(stderr, "%s: %s\n", claim.Predicate, spellClaim(claim))
		}
	}

	fmt.Fprintf(stderr, "%s %s at %s: %s, %s\n",
		entity.Family, entity.ID, entity.Span.Start, spellAxes(entity), plural(len(entity.Claims), "claim"))
}

// spellAxes is what the thing is, for a person: its label, and the axes the
// family it belongs to has.
func spellAxes(entity getEntity) string {
	label := entity.Label
	if label == "" {
		label = "(no label)"
	}

	if entity.Kind == "" {
		return label
	}
	return label + ", " + entity.Kind + " " + entity.Type
}

// spellClaim is one claim as a line for a person: what it says, and enough of
// where it came from to be worth reading without the rest of the object.
func spellClaim(claim claimEntry) string {
	var out strings.Builder

	out.WriteString(spellClaimValue(claim.Value))
	if claim.Method != "" {
		out.WriteString(" by " + claim.Method)
	}
	if claim.Date != "" {
		out.WriteString(" on " + claim.Date)
	}
	if claim.Resolution != "" {
		out.WriteString(", " + claim.Resolution)
	}
	if claim.Rank == string(dfcad.RankDeprecated) {
		out.WriteString(", deprecated")
	}

	return out.String()
}

// spellClaimValue is a value for a person, in whichever shape it has.
//
// A claim whose value could not be read has no shape at all and reads as
// nothing rather than as an empty one of the four: the diagnostic saying why is
// already on the same stream, and a line spelling it `""` would say the file
// held a value it does not.
func spellClaimValue(value claimValue) string {
	var written string

	switch {
	case value.Scalar != nil:
		written = number(*value.Scalar)
	case value.Coordinate != nil:
		components := make([]string, 0, len(value.Coordinate))
		for _, component := range value.Coordinate {
			components = append(components, number(component))
		}
		written = "(" + strings.Join(components, " ") + ")"
	case value.Transform != nil:
		written = "(" + strings.Join([]string{
			number(value.Transform.Translation[0]),
			number(value.Transform.Translation[1]),
			number(value.Transform.Translation[2]),
		}, " ") + ")"
	case value.Text != nil:
		written = strconv.Quote(*value.Text)
	default:
		written = "(no value)"
	}

	if value.Unit == "" {
		return written
	}
	return written + " " + value.Unit
}

// number is a real as a person reads it, which is the shortest spelling that
// reads back as the same number.
func number(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}
