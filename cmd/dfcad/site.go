// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/z5labs/dfcad"
)

const siteUsage = `dfcad site — decide whether one thing fits inside another, across whatever
frames the two are declared in, and say how well the answer is known.

Usage:

	dfcad site [flags] --within <id> <id>

The subject is read out of the corners surveyed in its own frame, carried into
the frame the envelope is declared in across the transform claims which relate
the two, grown by whatever clearance is required of it, overlaid on the
envelope and measured. Nothing is written back: the answer is recomputed from
the claims every time it is asked for, so a re-survey which landed this morning
is already in it.

The error budget is the point of the command. The georeference is one transform
applied to every fact declared indoors, so its residual does not cancel between
two indoor points and does not average away against an outdoor one. Systematic
terms are added linearly and each is counted once however many inputs
contributed it — which matters most in exactly this query, because a control
point behind the interior corners is routinely behind the boundary survey and
the georeference as well. Combining everything in quadrature would report a
narrower answer than the evidence supports, which is the direction nobody
investigates.

Flags:

	--within <id>            the thing the subject has to sit inside
	                         (required)
	--position <predicate>   the predicate a corner's position is claimed
	                         under, which both outlines are read from
	                         (required)
	--tolerance <name>       the tolerance corners are judged coincident
	                         against and rounded corners are drawn to
	                         (required)
	--clearance <distance>   how much room the subject has to keep between
	                         itself and the envelope's boundary, in the linear
	                         unit of the envelope's frame (default 0)

Neither the predicate nor the tolerance has a default and neither ever will.
Which predicate carries a position, and how close two corners have to be to be
one corner, are things the project wrote down, and a value compiled in here
would be the engine deciding one of them on a project's behalf.

The verdict distinguishes fitting from possibly fitting, and it is the reason
the clearance is never reported on its own:

	fits           the margin is wider than the uncertainty of the margin
	might-fit      the margin is inside the uncertainty of the margin, in
	               either direction, so the model as measured cannot tell
	does-not-fit   the deficit is wider than the uncertainty of the margin
	unknown        the uncertainty could not be computed, so there is nothing
	               to judge the margin against — an unstated accuracy is
	               unknown rather than nought

A clearance of forty millimetres is a comfortable fit where the answer is known
to five and no answer at all where it is known to sixty. Both come back as a
number; only the verdict tells them apart.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object site writes carries "subject" and "within", "sited" and the
"digest" of the source tree it was computed against, the "frame" the answer is
expressed in and the "declared-in" frame the subject was written in, the
"unit" and the "tolerance" it was judged against, the "verdict" and whether it
"decided", the "clearance" — required, actual, margin and the uncertainty of
the margin — the "envelope", "proposal", "needed", "shared" and "spill"
regions, and the "budget": the accuracy of the answer broken out by term, each
naming the claims which contributed it.

Exit code 1 is a question which could not be answered — an outline which could
not be read, two frames with no measured chain between them, a clearance
shorter than the tolerance. The object still comes back, with "sited" false, so
a caller reads why from the diagnostics on stderr rather than from an empty
stream. A subject which does not fit is a successful run: the command answered,
and the answer is no.
`

// The flags site takes beyond the global ones, named here because the errors
// which refuse them name them.
const (
	flagWithin    = "within"
	flagClearance = "clearance"
)

// ErrMissingEnvelope is a run which did not say what the subject has to fit
// inside.
//
// It is separate from [MissingVocabularyError] because it is not vocabulary: a
// predicate and a tolerance are words the project chose, and this is the second
// half of the question. A run which named one thing has asked about one thing.
var ErrMissingEnvelope = errors.New(
	"expected the id of the thing to fit inside, given with --within: a clearance is between two things and there " +
		"is no default for the second of them",
)

// siteResult is the object site writes to stdout.
type siteResult struct {
	envelope

	// Subject is the id which was sited and Within the id it had to sit inside.
	// Both are written whatever the outcome: a refusal a caller cannot
	// attribute to a question is one it has to correlate by position.
	Subject string `json:"subject"`
	Within  string `json:"within"`

	// Sited reports whether there is an answer below. It is written whatever
	// the outcome, so that a caller can tell a subject which does not fit from
	// a question which could not be asked: the first is sited with a verdict of
	// does-not-fit, the second is not sited at all.
	Sited bool `json:"sited"`

	// Digest is the digest of the source tree the fit was computed against,
	// which is what lets a caller check the answer against the model in front
	// of them rather than taking it on trust. It is written on a refusal too: a
	// refusal is still about a tree, and saying which one is the point.
	//
	// Absent for a model which was not read from disk, or one a file of which
	// could not be read at all, because there is then nothing anything may be
	// keyed by.
	Digest string `json:"digest,omitempty"`

	// Frame is the coordinate frame the answer is expressed in, which is the
	// envelope's, and DeclaredIn the frame the subject was written in. They are
	// the same frame where nothing had to be carried, which is what says
	// whether a georeference is in the budget at all.
	Frame      string `json:"frame,omitempty"`
	DeclaredIn string `json:"declared-in,omitempty"`

	// Carried reports whether a frame chain was walked to compare the two.
	Carried bool `json:"carried"`

	// Unit is the linear unit of the answer's frame, which every distance here
	// is in and every area in the square of.
	Unit string `json:"unit,omitempty"`

	// Tolerance is what corners were judged coincident against and rounded
	// corners drawn to.
	Tolerance *toleranceEntry `json:"tolerance,omitempty"`

	// Verdict is what the clearance makes of the question once its own
	// uncertainty is taken into account, and Decided whether that answers it.
	Verdict string `json:"verdict,omitempty"`
	Decided bool   `json:"decided"`

	// Clearance is how much room there is, what was required of it, and how
	// well the difference is known.
	Clearance *clearanceEntry `json:"clearance,omitempty"`

	// Envelope is the region the subject had to sit inside and Proposal the
	// subject itself, expressed in the envelope's frame.
	Envelope *regionEntry `json:"envelope,omitempty"`
	Proposal *regionEntry `json:"proposal,omitempty"`

	// Needed is the proposal grown by the clearance required of it, which is
	// the shape the envelope had to accommodate. It is the proposal itself
	// where nothing beyond fitting at all was required.
	Needed *regionEntry `json:"needed,omitempty"`

	// Shared is what the two have in common and Spill what the proposal needs
	// and the envelope does not offer. The second is where a refusal points: a
	// fit answered only by "no" leaves somebody to work out which corner is
	// over the line.
	Shared *regionEntry `json:"shared,omitempty"`
	Spill  *regionEntry `json:"spill,omitempty"`

	// Budget is the accuracy of the answer, broken out by term, over the
	// position claims behind both outlines and the transform claims of every
	// frame the subject was carried through.
	Budget *budgetReport `json:"budget,omitempty"`
}

// clearanceEntry is the measured half of a siting answer.
//
// The margin is written rather than left to be subtracted, because it is the
// quantity the verdict is decided on and a caller recomputing it is a second
// implementation of the rule.
type clearanceEntry struct {
	// Required is the clearance the subject was asked to keep, Actual how much
	// room it has, and Margin the second less the first.
	//
	// Actual is negative where the subject does not sit inside the envelope:
	// how far the part which is outside reaches past the boundary where the two
	// overlap, and how far apart they are where the subject is not over the
	// envelope at all.
	Required float64 `json:"required"`
	Actual   float64 `json:"actual"`
	Margin   float64 `json:"margin"`

	// Unit is the linear unit all three are in.
	Unit string `json:"unit,omitempty"`

	// Uncertainty is how well the margin is known, which is what the verdict
	// judged it against. Absent where the budget could not be reduced to one
	// figure, which the budget below says the reason for.
	Uncertainty *combinedUncertainty `json:"uncertainty,omitempty"`
}

// runSite is the site command.
func runSite(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	within := flags.String(flagWithin, "", "")
	position := flags.String(flagPosition, "", "")
	tolerance := flags.String(flagTolerance, "", "")
	clearance := flags.Float64(flagClearance, 0, "")

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

	if *within == "" {
		return usageError(cmd, ErrMissingEnvelope, stderr, true)
	}

	if err := vocabularyOf(
		given{flagPosition, *position},
		given{flagTolerance, *tolerance},
	); err != nil {
		return usageError(cmd, err, stderr, true)
	}

	subject, err := dfcad.ParseID(arguments[0])
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	envelope, err := dfcad.ParseID(*within)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	graph := loadModel(cmd, globals, stderr)

	proposed, err := traversable(graph, subject)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	inside, err := traversable(graph, envelope)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	// One survey over both outlines rather than one each. A corner read against
	// two surveys is a corner which can be in two places, and the two shapes
	// being compared have to have been read the same way for the comparison to
	// mean anything.
	survey := dfcad.Survey{Tolerance: *tolerance, Registry: graph.Registry()}
	for _, node := range []*dfcad.SemanticNode{proposed, inside} {
		for vertex := range graph.Vertices(node) {
			resolution, err := graph.Claims().Resolve(vertex.ID(), *position, graph.Registry())
			if err != nil {
				continue
			}
			survey.Place(vertex.ID(), resolution)
		}
	}

	answer, diags := graph.Topology().FitWithin(proposed, inside, graph.Boundaries(), survey, dfcad.Siting{
		Frames:    graph.Frames(),
		Clearance: *clearance,
	})

	// The diagnostics are for whoever wrote the model, so they go to stderr on
	// every run and in every format, and whether any of them is an error is what
	// decides between an answer and a refusal.
	refused := render(diags, stderr)

	result := reportSite(cmd, graph, subject, envelope, answer, !refused)

	reportSiteFor(result, answer, globals, stderr)

	if err := emit(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	if refused {
		return exitCheck
	}

	return exitSuccess
}

// reportSite is the fit as the machine contract writes it.
//
// The two ids are the ones the run was asked about rather than the ones the
// answer carries, because a refused fit carries none: a caller collecting
// results has to be able to say which question this object answers, and a
// refusal is exactly the object it most needs that of.
func reportSite(
	cmd command,
	graph *dfcad.Graph,
	subject, envelope dfcad.ID,
	answer dfcad.Fit,
	ok bool,
) siteResult {
	result := siteResult{
		envelope: newEnvelope(cmd.name),
		Subject:  string(subject),
		Within:   string(envelope),
		Sited:    ok,
	}

	if digest, known := graph.Digest(); known {
		result.Digest = digest.String()
	}

	if !ok {
		return result
	}

	within := answer.Envelope()

	result.Frame = string(answer.Frame())
	result.DeclaredIn = string(answer.DeclaredIn())
	result.Carried = answer.Carried()
	result.Unit = string(answer.Unit())
	result.Verdict = string(answer.Verdict())
	result.Decided = answer.Verdict().Decided()

	declaredTolerance := declared(within.Tolerance())
	result.Tolerance = &declaredTolerance

	measured := clearanceEntry{
		Required: answer.Required(),
		Actual:   answer.Clearance(),
		Margin:   answer.Margin(),
		Unit:     string(answer.Unit()),
	}
	if combined, err := answer.Uncertainty(); err == nil {
		measured.Uncertainty = &combinedUncertainty{
			Magnitude:      combined.Magnitude,
			Unit:           string(combined.Unit),
			CoverageFactor: combined.CoverageFactor,
		}
	}
	result.Clearance = &measured

	for _, one := range []struct {
		region dfcad.Region
		into   **regionEntry
	}{
		{within, &result.Envelope},
		{answer.Proposal(), &result.Proposal},
		{answer.Needed(), &result.Needed},
		{answer.Shared(), &result.Shared},
		{answer.Spill(), &result.Spill},
	} {
		entry := regionOf(one.region)
		*one.into = &entry
	}

	budget := budgetOf(answer.Budget())
	result.Budget = &budget

	return result
}

// reportSiteFor renders a fit for a person, on stderr.
//
// Nothing here reaches stdout, in any format and at any verbosity: stdout is
// the same bytes whether or not anybody asked to read the run.
func reportSiteFor(result siteResult, answer dfcad.Fit, globals *globals, stderr io.Writer) {
	if !globals.human() || !result.Sited {
		return
	}

	// The terms behind the answer are the detail under the summary, so they are
	// behind the verbosity flag — and they are rendered by the library rather
	// than spelled again here, so that a caller reporting the answer and this
	// command reporting it write the same thing.
	if globals.Verbosity >= verbosityProgress {
		_, _ = fmt.Fprintf(stderr, "%s\n", answer.Report())
		return
	}

	_, _ = fmt.Fprintf(stderr, "%s\n", answer)
}
