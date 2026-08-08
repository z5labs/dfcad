// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/z5labs/dfcad"
)

// claimFlagsHelp describes the axes a claim is written with, which is the same
// set for the command which adds one and the command which corrects one.
//
// It is written once for the reason the global flags are: a source which meant
// one thing for one command and something else for another is an axis nobody can
// rely on.
const claimFlagsHelp = `Flags, which are the axes of a claim:

	--value <value>      what is claimed, in the shape the predicate declares: a
	                     scalar is one real number, a coordinate is its
	                     components in order, a text value is written as it
	                     stands, and a transform is thirteen reals — three of
	                     translation, nine of rotation, then the scale. Required
	--unit <unit>        the unit it is expressed in, which must be the one the
	                     predicate declares. A non-dimensional predicate takes
	                     none, and there is no unitless token
	--source "<text>"    the evidence: a report, a drawing, a person, an
	                     instrument log. Required
	--method <id>        an id naming how the value was obtained. Required
	--accuracy "<term>"  how well it is known, written as the file writes a term
	                     without its parentheses: "independent <magnitude>
	                     <unit>", or "systematic <magnitude> <unit> <term-id>".
	                     Repeat for more than one term
	--date <YYYY-MM-DD>  the day the value was obtained; today by default
	--id <claim-id>      write the claim with this id instead of leaving it
	                     unnamed
`

const addClaimUsage = `dfcad add-claim — attach a measured value to a thing, with its provenance.

Usage:

	dfcad add-claim [flags] <subject> <predicate>

A claim is a value and the evidence for it: where it came from, how it was
obtained, how well it is known and when. A dimension is never a bare column in
this system, which is what makes "how wide is that room, and how do you know"
one lookup rather than a join.

The predicate is checked against the registry before anything reaches the disk,
and so are the value's shape, its number of components and its unit. A predicate
nothing declares, a predicate declared to take a plain value instead, a value of
another shape and a unit other than the declared one are each a usage error
naming what would have been permitted.

Leaving out the accuracy is permitted and is reported: the claim loads and is
unrankable, which means it can never win resolution and is not given a default.
That is the one escape hatch the rule keeps open, and taking it deliberately is
different from taking it by accident.

Adding a second claim under a subject and predicate which already carries one
succeeds and reports that it created a conflict, naming what it now competes
with. Repeating a predicate is the normal case rather than an error: two width
claims on one node are two measurements, and the disagreement between them is
the most valuable thing in the file. Use "dfcad supersede" where the intent is
to correct rather than to disagree.

An id is written only where one was asked for. A claim which nothing references
needs no name; one something references is found by the name it wrote, and
"dfcad supersede" mints one at the moment a reference to it is written.

` + globalFlagsHelp + `
` + writeFlagsHelp + `
` + claimFlagsHelp + `
` + outputContractHelp + `
` + writeOutputHelp + `
It also carries "claim", the id of the claim which was written where it wrote
one, "rankable", whether it can take part in resolution, and "notices": what the
change has to say about the model it produced, each with its kind, the subject
and predicate it is about, and — for a conflict — the claims it now competes
with.
`

const deprecateClaimUsage = `dfcad deprecate-claim — record that a claim was retracted.

Usage:

	dfcad deprecate-claim [flags] <claim-id>

Flags:

	--superseded-by <claim-id>   the claim which stands in its place; required

Deprecating is not deleting, and it is not editing. The claim stays in the file
with its value, its evidence, its method and its date exactly as they were
written, and what changes is that it now says it was retracted and by what. A
claim which was believed and then corrected is the record of why the number
changed, and that record is the thing this model exists to keep.

A replacement is required, and a deprecation naming none is refused. That is the
whole of what keeps "deprecated" from becoming a delete button: a rank cannot be
used to make a measurement quietly go away. A replacement which names no claim,
and a claim named as its own replacement, are refused for the same reason.

Retracting the only live claim of a subject and predicate is permitted and is
reported: nothing then resolves under that predicate, which is a state a model
may legitimately be in — a value somebody withdrew and has not yet re-measured
is unknown rather than wrong — and is not something to find out about later.

A claim is named by the id it wrote. Most claims write none, because an id is
required only of a claim something references; where the intent is to correct a
value rather than to retract one already named, "dfcad supersede" names the
claim by its subject and predicate and mints the id itself.

` + globalFlagsHelp + `
` + writeFlagsHelp + `
` + outputContractHelp + `
` + writeOutputHelp + `
It also carries "replaced", the claim which was retracted, and "notices": what
the change has to say about the model it produced.
`

const supersedeUsage = `dfcad supersede — correct a value: state the new one and retract the old.

Usage:

	dfcad supersede [flags] <subject> <predicate>

The new claim is written and the claim it replaces is deprecated in its favour,
in one change which lands completely or not at all. There is no state in which
the correction was recorded and the retraction was not, or the other way about.

Correction is supersession and never an edit. No command in this interface
writes over a claim's value: the old claim keeps everything it said, and the
model gains the reason the number changed rather than losing the number it used
to be ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).

The claim being corrected is the one live claim written on the subject under the
predicate. It is named that way rather than by an id because most claims write
none. A subject and predicate nothing states is refused rather than added to —
a value nothing yet claims is added with "dfcad add-claim" — and one stated more
than once is refused naming the competing claims, because which of them is being
corrected is not something to guess at; deprecate that one by its id instead.

The new claim is given an id, because the claim it replaces names it. That is
when a claim id is generated, and the format is ` + "`<subject>:<predicate>:<n>`" + `,
where n is the lowest ordinal from one which nothing in the model already holds.
Nothing is inferred back out of it: it is a name and not a schema.

` + globalFlagsHelp + `
` + writeFlagsHelp + `
` + claimFlagsHelp + `
` + outputContractHelp + `
` + writeOutputHelp + `
It also carries "claim", the id of the claim which was written, "replaced", the
claim it retracted, "rankable", whether the new claim can take part in
resolution, and "notices": what the change has to say about the model it
produced.
`

// ErrMissingValue is a claim with nothing claimed, named by the flag which
// writes it.
//
// It is not the same as an empty one: `--value ""` under a predicate declaring
// text is the empty string, which is a value a claim may legally hold, and
// leaving the flag out says nothing at all. The engine's [dfcad.ErrNoValue] is
// the same absence said without a flag to name.
var ErrMissingValue = errors.New("expected the value being claimed, found no --value")

// ErrMissingSubjectAndPredicate is a claim command with nothing to write the
// claim on.
var ErrMissingSubjectAndPredicate = errors.New(
	"expected the id of the subject and the predicate to claim under, found no arguments",
)

// ErrMissingClaimID is a deprecation with no claim to retract.
var ErrMissingClaimID = errors.New("expected the id of the claim to deprecate, found no argument")

// optional is a string flag which remembers whether it was written at all.
//
// The empty string is a value rather than an absence for one of these — a text
// claim of "" is a claim — so a command which read the empty default as "not
// given" could not be asked for one, and one which read it as given would write
// an empty claim for an invocation which said nothing.
type optional struct {
	value string
	set   bool
}

// String implements [flag.Value].
//
// The nil check is not defensive: the flag package builds a zero value of this
// type by reflection to find the default, and for a pointer receiver that zero
// value is a nil pointer.
func (o *optional) String() string {
	if o == nil {
		return ""
	}
	return o.value
}

// Set implements [flag.Value].
func (o *optional) Set(value string) error {
	o.value, o.set = value, true
	return nil
}

// repeated is a string flag which may be written more than once, keeping every
// value in the order they were written.
type repeated []string

// String implements [flag.Value].
func (r *repeated) String() string {
	if r == nil {
		return ""
	}
	return strings.Join(*r, ", ")
}

// Set implements [flag.Value].
func (r *repeated) Set(value string) error {
	*r = append(*r, value)
	return nil
}

// claimResult is the object the commands which write a claim produce.
//
// It is the write result with what the change did to the claims beside it,
// rather than a shape of its own, so that a caller reading .files and .dryRun
// reads them the same way whichever command wrote them.
type claimResult struct {
	envelope
	dfcad.Commit

	// Claim is the id of the claim which was written. Absent where it wrote
	// none, which is the ordinary case for a claim nothing references.
	Claim string `json:"claim,omitempty"`

	// Replaced is the id of the claim which was retracted, and is absent for a
	// change which retracted none.
	Replaced string `json:"replaced,omitempty"`

	// Rankable reports whether the claim which was written can take part in
	// resolution, which is whether it carries an accuracy. It is reported
	// whether or not it does, because a claim which can never win is a property
	// of the claim rather than an absence.
	Rankable bool `json:"rankable"`

	// Notices are what the change has to say about the model it produced, in
	// the order the engine reported them. Empty rather than null when it had
	// nothing to say.
	Notices []noticeEntry `json:"notices"`
}

// noticeEntry is one thing a change had to say about the model it produced.
//
// It is neither a diagnostic nor a failure: nothing is wrong with what anybody
// wrote, and what is being reported is a consequence of the change which the
// author is entitled to have wanted. What it is not is something to discover
// later.
type noticeEntry struct {
	// Kind is what it is about: unrankable, conflict or unresolvable.
	Kind string `json:"kind"`

	// Message is the notice as a sentence, which is presentation. A caller
	// branches on the kind.
	Message string `json:"message"`

	// Subject is the thing the claim is about.
	Subject string `json:"subject"`

	// Predicate is the predicate it was written under.
	Predicate string `json:"predicate"`

	// Competing are the claims already written on the same subject and
	// predicate. Absent for a notice which is not about a disagreement.
	Competing []claimEntry `json:"competing,omitempty"`
}

// runAddClaim is the add-claim command.
func runAddClaim(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	written := claimFlags(flags)

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	subject, predicate, exit, ok := claimedOn(cmd, arguments, stderr)
	if !ok {
		return exit
	}

	tx, exit, ok := begin(cmd, globals, stderr)
	if !ok {
		return exit
	}
	defer func() { _ = tx.Close() }()

	spec, err := written.spec(subject, predicate, tx.Graph().Registry())
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	id, notices, err := tx.AddClaim(spec)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	return finish(cmd, tx, globals, stdout, stderr, claimResult{
		Claim:    string(id),
		Rankable: len(spec.Accuracy) > 0,
		Notices:  noticed(notices),
	}, notices)
}

// runSupersede is the supersede command.
func runSupersede(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	written := claimFlags(flags)

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	subject, predicate, exit, ok := claimedOn(cmd, arguments, stderr)
	if !ok {
		return exit
	}

	tx, exit, ok := begin(cmd, globals, stderr)
	if !ok {
		return exit
	}
	defer func() { _ = tx.Close() }()

	spec, err := written.spec(subject, predicate, tx.Graph().Registry())
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	// The claim which is about to be retracted is read before the change, so
	// that the result can name it: after the change it is still there, and the
	// answer is the same, but reading it here is what makes the two halves of
	// the supersession one sentence.
	replaced := retracted(tx.Graph().Claims().Live(subject, predicate))

	id, notices, err := tx.Supersede(spec)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	return finish(cmd, tx, globals, stdout, stderr, claimResult{
		Claim:    string(id),
		Replaced: replaced,
		Rankable: len(spec.Accuracy) > 0,
		Notices:  noticed(notices),
	}, notices)
}

// runDeprecateClaim is the deprecate-claim command.
func runDeprecateClaim(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	supersededBy := flags.String("superseded-by", "", "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	switch {
	case len(arguments) == 0:
		return usageError(cmd, ErrMissingClaimID, stderr, true)
	case len(arguments) > 1:
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments[1:]}, stderr, true)
	}

	// Neither argument is checked against the model before the model is read,
	// and both are checked against the production before it: nothing about the
	// tree makes a malformed id well formed.
	id, err := dfcad.ParseID(arguments[0])
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	var replacement dfcad.ID
	if *supersededBy != "" {
		if replacement, err = dfcad.ParseID(*supersededBy); err != nil {
			return usageError(cmd, err, stderr, false)
		}
	}

	tx, exit, ok := begin(cmd, globals, stderr)
	if !ok {
		return exit
	}
	defer func() { _ = tx.Close() }()

	notices, err := tx.DeprecateClaim(id, replacement)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	return finish(cmd, tx, globals, stdout, stderr, claimResult{
		Replaced: string(id),
		Notices:  noticed(notices),
	}, notices)
}

// claimAxes are the flags a claim is written with, as they were given.
type claimAxes struct {
	value    *optional
	unit     *string
	source   *string
	method   *string
	accuracy *repeated
	date     *string
	id       *string
}

// claimFlags defines the axes of a claim on a command's flag set.
func claimFlags(flags *flag.FlagSet) claimAxes {
	axes := claimAxes{
		value:    &optional{},
		accuracy: &repeated{},
		unit:     flags.String("unit", "", ""),
		source:   flags.String("source", "", ""),
		method:   flags.String("method", "", ""),
		date:     flags.String("date", "", ""),
		id:       flags.String("id", "", ""),
	}

	flags.Var(axes.value, "value", "")
	flags.Var(axes.accuracy, "accuracy", "")

	return axes
}

// written is the axes as the engine reads them, which is the same value an
// operation file decodes to.
//
// A claim written on a command line and a claim written in a batch are the same
// claim, so they are read by one piece of code and refused in the same words:
// the flags here are the spelling, and what a spelling means is the engine's.
func (axes claimAxes) written() dfcad.ClaimAxes {
	written := dfcad.ClaimAxes{
		Unit:     *axes.unit,
		Source:   *axes.source,
		Method:   *axes.method,
		Accuracy: []string(*axes.accuracy),
		Date:     *axes.date,
		ID:       *axes.id,
	}

	// A flag nobody wrote and a flag written empty are different: `--value ""`
	// under a predicate declaring text is the empty string, which is a value a
	// claim may legally hold.
	if axes.value.set {
		written.Value = &axes.value.value
	}

	return written
}

// spec is the claim the flags describe, read against what the registry declares
// about the predicate.
//
// The registry is needed before the value can be read at all: which of the four
// shapes a value takes is registry data, and reading `1.0 2.0 3.0` as a scalar
// or as a coordinate is a question only the declaration answers. A predicate
// nothing declares is therefore answered by the engine rather than by guessing a
// shape and reporting the value against it, so a caller reads one sentence
// whether the refusal came from here or from a library call.
func (axes claimAxes) spec(subject dfcad.ID, predicate string, registry *dfcad.Registry) (dfcad.ClaimSpec, error) {
	spec, err := axes.written().Spec(subject, predicate, registry)

	// The engine names the axis, because an axis is what a library caller and
	// an operation file both write. Here the axis was written as a flag, and
	// the flag is what the author typed and what they will fix.
	if errors.Is(err, dfcad.ErrNoValue) {
		return spec, ErrMissingValue
	}

	return spec, err
}

// claimedOn is the subject and the predicate a claim command was given.
func claimedOn(cmd command, arguments []string, stderr io.Writer) (dfcad.ID, string, int, bool) {
	switch {
	case len(arguments) < 2:
		return "", "", usageError(cmd, ErrMissingSubjectAndPredicate, stderr, true), false
	case len(arguments) > 2:
		return "", "", usageError(cmd, UnexpectedArgumentsError{Extra: arguments[2:]}, stderr, true), false
	}

	subject, err := dfcad.ParseID(arguments[0])
	if err != nil {
		return "", "", usageError(cmd, err, stderr, false), false
	}

	return subject, arguments[1], exitSuccess, true
}

// retracted names the claim a supersession is about to replace, which is the one
// live claim of the pair.
//
// It is empty where that claim wrote no id of its own, which is the ordinary
// case. The claim is still identified in the result, by the span every claim
// carries; what an empty id says is that nothing pointed at it before now.
func retracted(live []*dfcad.Claim) string {
	if len(live) != 1 {
		return ""
	}
	if id, ok := live[0].ID(); ok {
		return string(id)
	}
	return ""
}

// finish commits the change, says what the change had to say about the model,
// and writes the result.
func finish(
	cmd command,
	tx *dfcad.Tx,
	globals *globals,
	stdout, stderr io.Writer,
	result claimResult,
	notices []dfcad.Notice,
) int {
	out, exit, ok := apply(cmd, tx, globals, stderr)
	if !ok {
		return exit
	}

	reportNotices(cmd, notices, stderr)

	result.envelope, result.Commit = newEnvelope(cmd.name), out

	return emitted(cmd, stdout, stderr, result)
}

// noticed is each notice as the result object carries it.
func noticed(notices []dfcad.Notice) []noticeEntry {
	// Made rather than declared so that a change with nothing to say carries an
	// empty list rather than a null, and a caller indexing it needs no special
	// case for the ordinary change.
	out := make([]noticeEntry, 0, len(notices))

	for _, notice := range notices {
		entry := noticeEntry{
			Kind:      string(notice.Kind),
			Message:   notice.Message(),
			Subject:   string(notice.Subject),
			Predicate: notice.Predicate,
		}

		for _, claim := range notice.Competing {
			entry.Competing = append(entry.Competing, entryOf(claim, ""))
		}

		out = append(out, entry)
	}

	return out
}

// reportNotices writes what the change had to say about the model to stderr.
//
// They go there on every run and in every format, as diagnostics do: what a
// change turned out to mean is not a rendering somebody opted into, and stdout
// carries the same bytes either way.
func reportNotices(cmd command, notices []dfcad.Notice, stderr io.Writer) {
	for _, notice := range notices {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %s: %s\n", cmd.name, notice.Kind, notice.Message())
	}
}
