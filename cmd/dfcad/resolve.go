// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/z5labs/dfcad"
)

const resolveUsage = `dfcad resolve — answer one predicate about one thing, with its evidence.

Usage:

	dfcad resolve [flags] <id> <predicate>

The current value of one predicate about one thing: the number, the unit it is
in, how well it is known, the id of the claim it came from and which step of the
rule picked that claim over the others. This is the call the rest of the system
exists to make cheap — "how wide is that room" answered once, with the evidence
attached rather than a lookup away.

Flags:

	--candidates     report every live claim under the predicate beside the
	                 answer, each marked with what resolution made of it
	--frame <id>     express a coordinate answer in this frame rather than in
	                 the one the thing is written in

The rule is stated in one place and this reports what it did:

	only        the one live claim under the predicate
	accuracy    its accuracy is smaller than that of every claim it could be
	            compared against, which is the criterion
	recency     it tied on accuracy and carried the later date, which is the
	            tiebreaker and never the criterion
	unranked    the one live claim under a predicate nothing rankable was said
	            about; it is still the answer, and it is not one the rule chose
	ambiguous   more than one claim nothing separates
	unclaimed   nothing live is written under the predicate at all

An ambiguity is never broken by picking one. Every tied claim comes back and the
run exits 4, because narrowing four claims to two is most of the work of
deciding between them and a caller shown one of the two cannot tell the other is
there. Under a predicate the registry declares strict the same ambiguity exits 5
instead: strictness is the author's assertion that for this quantity no answer
is safer than an arbitrary one.

A thing nothing is claimed about under the predicate exits 1 and says so. It is
not the same answer as an id nothing holds, which is a usage error naming the
nearest id there is: a thing which is not there and a thing nobody has measured
are different situations, and a caller which cannot tell them apart retries a
misspelling forever.

--frame transforms a coordinate answer through the frame graph, which is a
measurement rather than a conversion. The accumulated error of the answer comes
back with it, broken out by term and attributed to the claims which contributed
each, so that "the georeference is most of your budget" is readable off the
answer rather than reconstructed from it. A systematic term reached through two
fits is counted once. Nothing converts between units: a route whose fits were
written in different units reports the terms and no combined figure.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object resolve writes carries "subject" and "predicate", the "outcome" and
the "reason" for it, and the "value" and the "claim" it came from where there is
one.
`

// The outcomes a resolution can have, which is what says which of the fields of
// the answer to expect.
//
// They are a coarsening of the reason rather than a second vocabulary: a caller
// branching on what it got reads this, and one which wants to know why reads the
// reason beside it.
const (
	// outcomeResolved is one claim the rule picked. The value and the claim are
	// both there.
	outcomeResolved = "resolved"

	// outcomeUnranked is the one live claim under a predicate nothing rankable
	// was said about. The value and the claim are there and the rule chose
	// neither.
	outcomeUnranked = "unranked"

	// outcomeAmbiguous is more than one claim nothing separates. There is no
	// value and no claim; every tied claim is in the candidates.
	outcomeAmbiguous = "ambiguous"

	// outcomeUnclaimed is a subject with nothing live written under the
	// predicate. There is no value, no claim and no candidate.
	outcomeUnclaimed = "unclaimed"
)

// ErrMissingSubject is a resolve with nothing to resolve about.
var ErrMissingSubject = errors.New("expected the id of the thing to resolve about, found no argument")

// ErrMissingPredicate is a resolve with a subject and no predicate to resolve
// under.
//
// It is its own error rather than the one above because the two are different
// mistakes with different fixes, and a caller told only that an argument is
// missing has to count them to find out which.
var ErrMissingPredicate = errors.New("expected the predicate to resolve under, found only the id")

// UnframedSubjectError is --frame asked of a thing which is written in no frame
// at all.
//
// There is nothing to transform from. A zone administered together has no shape
// and no frame, and answering as though it were in the root frame would be
// inventing the one end of a relationship the model does not state.
type UnframedSubjectError struct {
	// Subject is the thing which declares no frame.
	Subject string

	// Frame is the frame the answer was asked for in.
	Frame string
}

// Error implements [error].
func (e UnframedSubjectError) Error() string {
	return fmt.Sprintf(
		"cannot express %s in %s: %s declares no frame, so there is nothing to transform from",
		e.Subject, e.Frame, e.Subject,
	)
}

// UntransformableValueError is --frame asked of a value which is not a position
// a frame can hold.
//
// A frame relates positions to positions. How much floor a space has is the same
// number whichever grid it is reported against, and transforming it would be
// arithmetic on a quantity the transform says nothing about.
type UntransformableValueError struct {
	// Predicate is the predicate the value was written under.
	Predicate string

	// Shape is the shape its value takes, which is empty for a value which
	// could not be read at all.
	Shape string

	// Components is how many components a coordinate value carries, and is
	// meaningful only where Shape is a coordinate.
	Components int
}

// Error implements [error].
func (e UntransformableValueError) Error() string {
	if e.Shape != string(dfcad.ShapeCoordinate) {
		return fmt.Sprintf(
			"cannot express a %s in another frame: %s is declared (shape %s), and a frame relates positions",
			e.Predicate, e.Predicate, spelledShape(e.Shape),
		)
	}
	return fmt.Sprintf(
		"expected a coordinate of three components to transform, found %s under %s",
		plural(e.Components, "component"), e.Predicate,
	)
}

// resolveResult is the object resolve writes to stdout.
type resolveResult struct {
	envelope

	// Subject is the id the question was asked about, which is the id asked
	// for.
	Subject string `json:"subject"`

	// Predicate is the predicate it was asked under.
	Predicate string `json:"predicate"`

	// Outcome is what came of it: resolved, unranked, ambiguous or unclaimed.
	// It says which of the fields below to expect.
	Outcome string `json:"outcome"`

	// Reason is which step of the rule produced that outcome: only, accuracy,
	// recency, unranked, ambiguous or unclaimed.
	Reason string `json:"reason"`

	// Strict reports that the registry declares the predicate strict, for which
	// an ambiguity is a failure rather than a finding. It is written whatever
	// the outcome, so that a caller can tell a predicate which resolved from
	// one which could not have failed.
	Strict bool `json:"strict"`

	// Value is the answer, in Frame where one was asked for. Absent where
	// nothing resolved.
	Value *claimValue `json:"value,omitempty"`

	// Frame is the coordinate frame the value is expressed in. Absent for a
	// value which is not a position, which is in no frame.
	Frame string `json:"frame,omitempty"`

	// Claim is the claim the answer came from, with the evidence it carries:
	// its id, its source, its method, its accuracy and its date. Absent where
	// nothing resolved.
	Claim *claimEntry `json:"claim,omitempty"`

	// Candidates are the claims which could still be the answer, in source
	// order, each marked with what resolution made of it. An ambiguous outcome
	// carries every tied claim here whether or not they were asked for; under
	// --candidates it carries every live claim under the predicate, which is
	// the audit view of one answer.
	Candidates []claimEntry `json:"candidates,omitempty"`

	// Budget is the accumulated error of the answer, broken out by term.
	// Written where a frame transform was applied and absent otherwise, because
	// an answer read out of one claim in its own frame is that claim's accuracy
	// and nothing further.
	Budget *budgetReport `json:"budget,omitempty"`
}

// budgetReport is the accumulated uncertainty of a cross-frame answer.
//
// It is a list of terms rather than a figure. "±0.06 m" is an answer nobody can
// act on; "the georeference is 80% of it, and it came from this claim" says what
// to re-measure.
type budgetReport struct {
	// From and To are the frames the route ran between.
	From string `json:"from"`
	To   string `json:"to"`

	// Terms are the accumulated terms, in the order they were contributed.
	Terms []budgetTerm `json:"terms"`

	// Combined is the terms reduced to one standard uncertainty. Absent where
	// they cannot be reduced to one, which Unknown and Units say the reason
	// for.
	Combined *combinedUncertainty `json:"combined,omitempty"`

	// Unknown are the claims the answer was computed from which stated no
	// accuracy, named the way a diagnostic names a claim. One of them taints
	// the whole budget: an unstated accuracy is unknown rather than zero.
	Unknown []string `json:"unknown,omitempty"`

	// Units are the units the terms were written in where they disagree, each
	// once. Nothing converts between them, so a budget whose terms disagree
	// combines to nothing and says which units it was asked to reconcile.
	Units []string `json:"units,omitempty"`
}

// budgetTerm is one term of an accumulated budget.
type budgetTerm struct {
	// Kind is which of the two kinds of error it is, and so how it combines:
	// independent terms in quadrature, systematic terms linearly.
	Kind string `json:"kind"`

	// Name is what the term is called: the id a systematic error is shared
	// with, or the name of the claim an independent one came from.
	Name string `json:"name"`

	// Magnitude is the one-sigma figure, as it was written.
	Magnitude float64 `json:"magnitude"`

	// Unit is the unit that figure is expressed in.
	Unit string `json:"unit,omitempty"`

	// Source is the id a systematic error is shared with. Absent for an
	// independent term, which is shared with nothing.
	Source string `json:"source,omitempty"`

	// Contributors are the claims which carried this term, each once, in the
	// order they were accumulated. More than one is a shared term counted once,
	// which is what an honest cross-frame budget looks like from the reporting
	// side.
	Contributors []string `json:"contributors"`
}

// combinedUncertainty is a budget reduced to one figure, with the coverage it
// is stated at.
//
// The coverage factor travels with the figure rather than beside it: a
// half-width bound, a one-sigma uncertainty and a 95% interval differ by a
// factor of two, and nothing in the bare number tells them apart.
type combinedUncertainty struct {
	// Magnitude is the combined figure, at CoverageFactor standard deviations.
	Magnitude float64 `json:"magnitude"`

	// Unit is the unit it is expressed in.
	Unit string `json:"unit,omitempty"`

	// CoverageFactor is how many standard uncertainties Magnitude is, which is
	// one for everything the engine produces.
	CoverageFactor float64 `json:"coverage-factor"`
}

// runResolve is the resolve command.
func runResolve(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	auditing := flags.Bool("candidates", false, "")
	into := flags.String("frame", "", "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	switch {
	case len(arguments) == 0:
		return usageError(cmd, ErrMissingSubject, stderr, true)
	case len(arguments) == 1:
		return usageError(cmd, ErrMissingPredicate, stderr, true)
	case len(arguments) > 2:
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments[2:]}, stderr, true)
	}

	// An argument which is not an id is a different mistake from an id nothing
	// holds, and the production it broke is a better answer than a lookup which
	// was never going to find anything.
	subject, err := dfcad.ParseID(arguments[0])
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}
	predicate := arguments[1]

	graph := loadModel(cmd, globals, stderr)
	registry := graph.Registry()

	entity, held := graph.Entity(subject)
	if !held {
		nearest, _ := graph.Nearest(subject)
		return usageError(cmd, UnknownIDError{ID: string(subject), Nearest: string(nearest)}, stderr, false)
	}
	if !registry.Declares(dfcad.SortPredicate, predicate) {
		return usageError(cmd, UnknownPredicateError{
			Predicate: predicate,
			Declared:  registry.Names(dfcad.SortPredicate),
		}, stderr, false)
	}
	// The frame is checked against the registry and the declaration rather than
	// against whatever resolved, so that the flag is refused on a question it
	// could never have answered instead of quietly doing nothing on a subject
	// which turned out to be ambiguous. A flag which is silently ignored is
	// worse than one which does not exist.
	if *into != "" {
		if err := expressible(registry, entity, predicate, *into); err != nil {
			return usageError(cmd, err, stderr, false)
		}
	}

	// The error is the one thing the registry decides about a resolution, and
	// the resolution comes back beside it carrying every tied claim. It is read
	// off the resolution below rather than from the error, so that the strict
	// and the ordinary ambiguity are one path which differs in its exit code.
	resolution, _ := graph.Claims().Resolve(subject, predicate, registry)

	result := resolveResult{
		envelope:  newEnvelope(cmd.name),
		Subject:   string(subject),
		Predicate: predicate,
		Outcome:   outcomeOf(resolution),
		Reason:    string(resolution.Reason()),
		Strict:    strict(registry, predicate),
	}

	answer, answerable := answered(resolution)
	if answerable {
		entry := entryOf(answer, madeOf(answer, resolution))
		result.Claim = &entry

		value := entry.Value
		result.Value = &value

		if written, ok := frameOf(entity); ok && value.Shape == string(dfcad.ShapeCoordinate) {
			result.Frame = string(written)
		}
	}

	// Every tied claim, whether or not an audit was asked for: an ambiguity
	// answered with one of the claims it could not choose between would be the
	// arbitrary pick this command exists to refuse. An audit widens that to
	// every live claim, which is what the rule weighed rather than what is left
	// of it.
	switch {
	case *auditing:
		result.Candidates = marked(live(graph, subject, predicate), resolution)
	case resolution.Ambiguous():
		result.Candidates = marked(resolution.Candidates(), resolution)
	}

	if *into != "" && answerable {
		if code := express(&result, graph, entity, answer, *into, cmd, stderr); code != exitSuccess {
			return code
		}
	}

	reportResolution(result, globals, stderr)

	if err := emit(stdout, result); err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return codeFor(result)
}

// outcomeOf is what came of a resolution, which says which fields of the answer
// to expect.
func outcomeOf(resolution dfcad.Resolution) string {
	switch resolution.Reason() {
	case dfcad.ReasonUnranked:
		return outcomeUnranked
	case dfcad.ReasonAmbiguous:
		return outcomeAmbiguous
	case dfcad.ReasonUnclaimed:
		return outcomeUnclaimed
	default:
		return outcomeResolved
	}
}

// answered is the claim the value comes from, and whether there is one.
//
// The unranked claim is one of them. It is the one live claim under a predicate
// nothing rankable was said about, which makes it what the model says rather
// than what the rule chose — and a command which withheld it would report a
// measured thing as unmeasured because nobody wrote down how good the
// measurement was.
func answered(resolution dfcad.Resolution) (*dfcad.Claim, bool) {
	if claim, ok := resolution.Claim(); ok {
		return claim, true
	}

	if resolution.Reason() == dfcad.ReasonUnranked {
		candidates := resolution.Candidates()
		if len(candidates) == 1 {
			return candidates[0], true
		}
	}

	return nil, false
}

// codeFor is the exit code the answer carries.
//
// The four outcomes get four codes because what a caller does about each is
// different: carry on, ask a person, go and measure, or report that nothing has
// been measured. Telling them apart must not mean reading a message.
func codeFor(result resolveResult) int {
	if result.Outcome == outcomeAmbiguous {
		if result.Strict {
			return exitStrict
		}
		return exitAmbiguous
	}

	if result.Outcome == outcomeUnclaimed {
		return exitCheck
	}

	return exitSuccess
}

// expressible reports the first reason an answer cannot be moved into another
// frame at all.
//
// Every one of them is a property of the vocabulary and of the thing asked
// about rather than of the claim which won, which is what lets them be answered
// before anything is resolved.
func expressible(registry *dfcad.Registry, entity dfcad.Entity, predicate, into string) error {
	if !registry.Declares(dfcad.SortFrame, into) {
		return UnknownFrameError{Frame: into, Declared: registry.Names(dfcad.SortFrame)}
	}

	// A frame relates positions to positions. How much floor a space has is the
	// same number whichever grid it is reported against.
	declared, _ := registry.Predicate(predicate)
	if declared.Shape != dfcad.ShapeCoordinate || declared.Dimension != 3 {
		return UntransformableValueError{
			Predicate:  predicate,
			Shape:      string(declared.Shape),
			Components: declared.Dimension,
		}
	}

	if _, ok := frameOf(entity); !ok {
		return UnframedSubjectError{Subject: string(entity.ID()), Frame: into}
	}

	return nil
}

// strict reports whether the registry declares the predicate strict.
func strict(registry *dfcad.Registry, predicate string) bool {
	declared, ok := registry.Predicate(predicate)
	return ok && declared.Strict
}

// live is every claim resolution considered under one predicate, in the order
// they were written.
//
// The deprecated ones are not among them. A deprecated claim is retracted rather
// than out-ranked and was never a candidate, so listing it here would say the
// rule weighed something it never saw; `dfcad claims` is the view which reports
// a retraction.
func live(graph *dfcad.Graph, subject dfcad.ID, predicate string) []*dfcad.Claim {
	var out []*dfcad.Claim
	for claim := range graph.Claims().Under(subject, predicate) {
		if claim.Rank() == dfcad.RankDeprecated {
			continue
		}
		out = append(out, claim)
	}
	return out
}

// marked is a set of claims as the answer reports them, each with what
// resolution made of it.
func marked(claims []*dfcad.Claim, resolution dfcad.Resolution) []claimEntry {
	out := make([]claimEntry, 0, len(claims))
	for _, claim := range claims {
		out = append(out, entryOf(claim, madeOf(claim, resolution)))
	}

	inPredicateOrder(out)

	return out
}

// frameOf is the coordinate frame a thing is written in, and whether it
// declares one.
func frameOf(entity dfcad.Entity) (dfcad.ID, bool) {
	switch found := entity.(type) {
	case *dfcad.SemanticNode:
		return found.Frame()
	case *dfcad.Vertex:
		return found.Frame(), found.Frame() != ""
	case *dfcad.Edge:
		return found.Frame(), found.Frame() != ""
	case *dfcad.Loop:
		return found.Frame(), found.Frame() != ""
	}
	return "", false
}

// express transforms a resolved coordinate into another frame and accumulates
// what that cost in accuracy.
//
// The budget is the claim's own accuracy together with the fits the route passed
// through, accumulated in one place so that a systematic term reached through
// two of them is counted once. Adding the combined figures instead would inflate
// exactly the budget a shared control point should not inflate, and the usual
// response to that is to widen a tolerance rather than to fix the arithmetic.
func express(
	result *resolveResult,
	graph *dfcad.Graph,
	entity dfcad.Entity,
	answer *dfcad.Claim,
	into string,
	cmd command,
	stderr io.Writer,
) int {
	target := dfcad.ID(into)
	written, _ := frameOf(entity)

	// The declaration already said the predicate is a three-component
	// coordinate, so a value which is not one is a value the loader could not
	// read — which it has already said, on this stream.
	if result.Value.Shape != string(dfcad.ShapeCoordinate) || len(result.Value.Coordinate) != 3 {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, UntransformableValueError{
			Predicate:  result.Predicate,
			Shape:      result.Value.Shape,
			Components: len(result.Value.Coordinate),
		})
		return exitLoad
	}

	// The frame it is already in. No transform is applied and no fit is read, so
	// there is nothing to be uncertain about beyond the claim itself — which is
	// what asking without the flag answers, and the two must not differ.
	if written == target {
		return exitSuccess
	}

	point := dfcad.Point{
		result.Value.Coordinate[0],
		result.Value.Coordinate[1],
		result.Value.Coordinate[2],
	}

	moved, err := graph.Frames().TransformPoint(point, written, target)
	if err != nil {
		// The route is a measurement the model states or does not. A frame
		// whose fit is missing, two frames whose chains never meet and a
		// transform which cannot be run backwards are all the model failing to
		// say how the two relate, which is a load failure and never an answer
		// computed anyway.
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	budget, err := graph.Frames().TransformBudget(written, target)
	if err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	// The measurement itself is a term of the answer's error, not only the
	// route to it: a control point read to four millimetres and georeferenced
	// to twenty is known to neither on its own.
	budget.Add(answer)

	result.Value.Coordinate = moved[:]
	result.Frame = into

	// A frame declares exactly one linear unit, so a position expressed in it is
	// in that unit. Reporting the unit the claim was written in beside a
	// transformed coordinate would be the unlabelled conversion the whole
	// arrangement exists to prevent.
	if declared, ok := graph.Registry().Frame(target); ok {
		result.Value.Unit = string(declared.Unit)
	}

	report := budgetReport{From: string(written), To: into, Terms: make([]budgetTerm, 0)}
	for _, term := range budget.Terms() {
		report.Terms = append(report.Terms, budgetTerm{
			Kind:         string(term.Kind),
			Name:         term.Name,
			Magnitude:    term.Magnitude,
			Unit:         string(term.Unit),
			Source:       string(term.Source),
			Contributors: named(term.Contributors),
		})
	}

	combined, err := budget.Combined()
	switch {
	case err == nil:
		report.Combined = &combinedUncertainty{
			Magnitude:      combined.Magnitude,
			Unit:           string(combined.Unit),
			CoverageFactor: combined.CoverageFactor,
		}
	default:
		// A budget which cannot be reduced to a figure still reports its terms
		// and why it could not. An empty combined figure with no reason beside
		// it reads as an answer known exactly.
		var unknown dfcad.UnknownAccuracyError
		var mixed dfcad.MixedUnitsError

		if errors.As(err, &unknown) {
			report.Unknown = named(unknown.Claims)
		}
		if errors.As(err, &mixed) {
			report.Units = spellings(mixed.Units)
		}
	}

	result.Budget = &report

	return exitSuccess
}

// named is a set of claims as a budget names them: the claim's own id where it
// wrote one, and where it was written where it did not.
func named(claims []*dfcad.Claim) []string {
	out := make([]string, 0, len(claims))
	for _, claim := range claims {
		if id, ok := claim.ID(); ok {
			out = append(out, string(id))
			continue
		}
		out = append(out, claim.Span().Start.String())
	}
	return out
}

// spelledShape is a value shape for a message, and says so where the value
// could not be read at all rather than naming a shape nothing has.
func spelledShape(shape string) string {
	if shape == "" {
		return "no shape at all"
	}
	return shape
}

// reportResolution renders a resolve result for a person, on stderr.
//
// Nothing here reaches stdout, in any format and at any verbosity: stdout is the
// same bytes whether or not anybody asked to read the run.
func reportResolution(result resolveResult, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	if globals.Verbosity >= verbosityProgress {
		for _, candidate := range result.Candidates {
			fmt.Fprintf(stderr, "%s: %s\n", candidate.Predicate, spellClaim(candidate))
		}
		if result.Budget != nil {
			for _, term := range result.Budget.Terms {
				fmt.Fprintf(stderr, "%s %s: %s %s from %s\n",
					term.Kind, term.Name, number(term.Magnitude), term.Unit,
					plural(len(term.Contributors), "claim"))
			}
		}
	}

	fmt.Fprintf(stderr, "%s %s: %s\n", result.Subject, result.Predicate, spellAnswer(result))
}

// spellAnswer is what came of a resolution, for a person: the value where there
// is one, and why it is the answer.
func spellAnswer(result resolveResult) string {
	var out strings.Builder

	if result.Value == nil {
		out.WriteString(result.Outcome)
	} else {
		out.WriteString(spellClaimValue(*result.Value))
	}

	out.WriteString(", ")
	out.WriteString(spellReason(result))

	if result.Claim != nil && result.Claim.ID != "" {
		out.WriteString(", from " + result.Claim.ID)
	}
	if result.Budget != nil && result.Budget.Combined != nil {
		out.WriteString(", ±" + number(result.Budget.Combined.Magnitude) + " " + result.Budget.Combined.Unit)
	}

	return out.String()
}

// spellReason is why the answer is the answer, in a sentence rather than as the
// bare word the object carries.
func spellReason(result resolveResult) string {
	candidates := plural(len(result.Candidates), "claim")

	switch dfcad.Reason(result.Reason) {
	case dfcad.ReasonOnly:
		return "the one live claim under the predicate"
	case dfcad.ReasonAccuracy:
		return "its accuracy is the smallest of those which could be compared"
	case dfcad.ReasonRecency:
		return "the most recent of the claims which tied on accuracy"
	case dfcad.ReasonUnranked:
		return "nothing rankable was said about it, so the rule chose nothing"
	case dfcad.ReasonAmbiguous:
		if result.Strict {
			return "nothing separates " + candidates + " under a predicate declared strict"
		}
		return "nothing separates " + candidates
	default:
		return "nothing live is written under the predicate"
	}
}
