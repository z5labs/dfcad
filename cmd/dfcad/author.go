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
	"time"

	"github.com/z5labs/dfcad"
)

// writeOutputHelp describes what a write reports, which is the same object
// whichever command produced it.
const writeOutputHelp = `The object a write command produces carries "dryRun", whether anything was
written, and "files": one entry per file the change touched, in the lexical
order of their paths, each with what happened to it, what the change did to the
model in it, and the unified diff from what was on disk to what was written.

A change which would produce a model that does not load is refused. Its
diagnostics are the ones a load of the result would have raised, nothing at all
is written, and nothing reaches stdout: the correct response is to fix the
command and reissue it, because there is no partial state to reconcile.
`

const addNodeUsage = `dfcad add-node — write a new semantic node.

Usage:

	dfcad add-node [flags] <id>

The node is written with the axes below, each of them checked against the
registry before anything reaches the disk: an unregistered id namespace, a kind
or a geometry form which is not one, a type nothing declares, a type which does
not permit the kind or the geometry form written here, and a frame the registry
does not declare are each a usage error naming what would have been permitted.

Flags:

	--kind <kind>        the kind it declares
	--type <type>        the type it declares
	--geometry <form>    the geometry form it declares; omitted for a node with
	                     no geometry, which its type has to permit
	--frame <id>         the coordinate frame it is expressed in
	--label "<text>"     its display text, which nothing resolves through
	--file <path>        write it here instead, overriding the routing rules; a
	                     path relative to the model root, ending in ` + dfcad.Extension + `

Where the node goes is the registry's decision, taken exactly as ` + "`dfcad route`" + `
takes it and reported the same way.

An id something already holds is refused, naming where that thing is defined. A
retired id is refused the same way: retiring says the thing stopped existing,
not that its name came free, and an id is never issued twice.

` + globalFlagsHelp + `
` + writeFlagsHelp + `
` + outputContractHelp + `
` + writeOutputHelp

const relateUsage = `dfcad relate — say what a node is inside, grouped with and bounded by.

Usage:

	dfcad relate [flags] <id>

A node written by ` + "`dfcad add-node`" + ` is attached to nothing: it declares its own
axes and makes no reference to anything else. This writes the references — the
one node which strictly contains it, the zones it is grouped into, and the loops
which bound it — so that a batch which creates a room, a circuit and a
receptacle can also say the receptacle is in the room and on the circuit.

Flags:

	--within <node-id>   the node which strictly contains this one
	--member-of <id>     a zone it is a member of; repeat for more than one
	--boundary <loop-id> a loop which bounds it; repeat for more than one

The three are different relations and are never collapsed into one. Containment
is physical enclosure, nests strictly and is at most one, so naming a parent
replaces whatever parent was written before rather than being written beside it.
Membership is arbitrary grouping, is many to many, and is added. A boundary
leaves the semantic family altogether and names a loop, and is added the same
way.

At least one of the three is required: a relation which relates the node to
nothing is refused rather than written as a change which did nothing.

Nothing is resolved here. A parent which does not exist, a parent the hierarchy
does not permit, a --member-of naming something which is not a Zone and a
--boundary naming something which is not a loop are each refused when the model
this would produce is interpreted, with the diagnostics a load of that model
would have raised — the same ones the same mistake gets when it is typed into a
file by hand.

` + globalFlagsHelp + `
` + writeFlagsHelp + `
` + outputContractHelp + `
` + writeOutputHelp

const setLabelUsage = `dfcad set-label — change what a thing is called.

Usage:

	dfcad set-label [flags] <id> "<label>"

A label is display text. Nothing in the engine resolves through it, nothing is
derived from it, and no two things are required to have different ones — so
renaming is a one-line diff rather than a re-identification: the id, the global
id derived from it and every reference written to it are what they were.

An empty label removes it, which is how a thing goes back to having none.

` + globalFlagsHelp + `
` + writeFlagsHelp + `
` + outputContractHelp + `
` + writeOutputHelp

const retireUsage = `dfcad retire — record that a thing stopped existing.

Usage:

	dfcad retire [flags] <id>

Retiring is not deleting. The node stays in the file, its id stays in the graph
and every claim ever written on it is still there to be read, so a reference
written years ago resolves either to the thing it always named or to a retired
node which says what happened to it. The id is never issued again.

Flags:

	--reason "<text>"    why it stopped existing; required
	--replacement <id>   the node which stands in its place, where one does
	--date <YYYY-MM-DD>  when it stopped existing; today by default

A reason is required because a retirement with no reason is a deletion wearing
a hat: what the record loses is not the node, which is still there, but the one
sentence explaining why it stopped being true.

A node other things still reference is refused, naming every referrer. Supply a
replacement and those references are redirected to it in the same change, which
is the whole of what a replacement is for.

` + globalFlagsHelp + `
` + writeFlagsHelp + `
` + outputContractHelp + `
` + writeOutputHelp

// ErrMissingLabel is a set-label with no label to set.
//
// The empty label is not this: `dfcad set-label site:S-101 ""` says to remove
// the label, and leaving the argument out says nothing at all.
var ErrMissingLabel = errors.New("expected the label to set, found no argument")

// writeResult is the object every command which changes the model writes to
// stdout.
//
// The commit is embedded rather than nested because it is the whole of what
// happened: a caller reads .files and .dryRun beside .command without knowing
// which command wrote them, which is what makes one reader work for all of them.
type writeResult struct {
	envelope
	dfcad.Commit
}

// runAddNode is the add-node command.
func runAddNode(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	kind := flags.String("kind", "", "")
	declaredType := flags.String("type", "", "")
	geometry := flags.String("geometry", "", "")
	frame := flags.String("frame", "", "")
	label := flags.String("label", "", "")
	file := flags.String("file", "", "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	id, exit, ok := subject(cmd, arguments, 1, stderr)
	if !ok {
		return exit
	}

	tx, exit, ok := begin(cmd, globals, stderr)
	if !ok {
		return exit
	}
	defer func() { _ = tx.Close() }()

	spec := dfcad.NodeSpec{
		ID:       id,
		Kind:     dfcad.Kind(*kind),
		Type:     *declaredType,
		Geometry: dfcad.Geometry(*geometry),
		Frame:    dfcad.ID(*frame),
		Label:    *label,
	}

	// Where it goes is decided before it is written, by the same rules `dfcad
	// route` reports: an author who checked where something would land and a
	// command which writes it there have to be answering one question.
	destination, err := spec.Destination(tx.Graph().Registry(), *file)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	if err := tx.AddNode(spec, destination.Path); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	if globals.Verbosity >= verbosityProgress {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %s -> %s\n", cmd.name, id, destination.Path)
	}

	return commitChange(cmd, tx, globals, stdout, stderr)
}

// runRelate is the relate command.
func runRelate(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	within := flags.String("within", "", "")

	zones, loops := &repeated{}, &repeated{}
	flags.Var(zones, "member-of", "")
	flags.Var(loops, "boundary", "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	id, exit, ok := subject(cmd, arguments, 1, stderr)
	if !ok {
		return exit
	}

	// A relation which relates the node to nothing is answered before the model
	// is read, for the reason `dfcad retire` answers a missing reason there: it
	// is a property of the invocation, wrong whatever the model holds, and
	// reporting a load of the whole tree before saying so buries the one thing
	// which is missing.
	if *within == "" && len(*zones) == 0 && len(*loops) == 0 {
		return usageError(cmd, dfcad.ErrNoRelation, stderr, true)
	}

	// The ids on the command line are read before the model is, for the reason
	// `dfcad route` reads one there: nothing about the tree makes a malformed
	// id well formed, and reporting a load of the whole model before saying so
	// buries the one thing which is wrong.
	parent, err := identified([]string{*within})
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	spec := dfcad.RelationSpec{Within: parent[0]}

	if spec.MemberOf, err = identified(*zones); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	if spec.Boundary, err = identified(*loops); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	tx, exit, ok := begin(cmd, globals, stderr)
	if !ok {
		return exit
	}
	defer func() { _ = tx.Close() }()

	if err := tx.Relate(id, spec); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	return commitChange(cmd, tx, globals, stdout, stderr)
}

// runSetLabel is the set-label command.
func runSetLabel(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	id, exit, ok := subject(cmd, arguments, 2, stderr)
	if !ok {
		return exit
	}

	if len(arguments) < 2 {
		return usageError(cmd, ErrMissingLabel, stderr, true)
	}

	tx, exit, ok := begin(cmd, globals, stderr)
	if !ok {
		return exit
	}
	defer func() { _ = tx.Close() }()

	if err := tx.SetLabel(id, arguments[1]); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	return commitChange(cmd, tx, globals, stdout, stderr)
}

// runRetire is the retire command.
func runRetire(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	reason := flags.String("reason", "", "")
	replacement := flags.String("replacement", "", "")
	date := flags.String("date", "", "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	id, exit, ok := subject(cmd, arguments, 1, stderr)
	if !ok {
		return exit
	}

	// The reason is checked before the model is read because it is a property of
	// the invocation: a retirement with nothing to say about why is wrong
	// whatever the model holds, and reporting a load of the whole tree before
	// saying so buries the one thing which is missing. The refusal is the
	// engine's, so a caller reading the message reads one sentence whether the
	// change came from here or from a library call.
	if *reason == "" {
		return usageError(cmd, dfcad.MissingReasonError{ID: id}, stderr, true)
	}

	when, err := on(*date)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	tx, exit, ok := begin(cmd, globals, stderr)
	if !ok {
		return exit
	}
	defer func() { _ = tx.Close() }()

	spec := dfcad.RetirementSpec{Date: when, Reason: *reason, SupersededBy: dfcad.ID(*replacement)}

	if err := tx.Retire(id, spec); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	return commitChange(cmd, tx, globals, stdout, stderr)
}

// subject is the id a write command was given, which is its first argument.
//
// most is how many arguments the command takes in all, so that an argument too
// many is reported as the wrong shape of invocation rather than silently
// ignored.
func subject(cmd command, arguments []string, most int, stderr io.Writer) (dfcad.ID, int, bool) {
	switch {
	case len(arguments) == 0:
		return "", usageError(cmd, ErrMissingID, stderr, true), false
	case len(arguments) > most:
		return "", usageError(cmd, UnexpectedArgumentsError{Extra: arguments[most:]}, stderr, true), false
	}

	// An argument which is not an id is answered before the model is read, for
	// the reason `dfcad route` answers one there: nothing about the tree changes
	// the answer, and no id in a model is malformed.
	id, err := dfcad.ParseID(arguments[0])
	if err != nil {
		return "", usageError(cmd, err, stderr, false), false
	}

	return id, exitSuccess, true
}

// on is the date a change is dated, which is the day it is made where the
// invocation did not say.
//
// The one spelling of a date is the engine's, so the parse is too: a date
// written on the command line and a date written in a file are held to the same
// rule and refused in the same words.
func on(date string) (time.Time, error) {
	if date == "" {
		return time.Time{}, nil
	}
	return dfcad.ParseDate(date)
}

// begin loads the whole model, locks it for one change and renders whatever is
// wrong with it to stderr.
//
// A tree which does not already load is refused here rather than written into.
// Writing into a model which is already broken would report the author's
// mistake and the pre-existing one together, leaving whoever reads the output to
// work out which of the two the command was responsible for.
//
// The transaction which comes back must be finished, which every caller does by
// deferring [dfcad.Tx.Close]: it does nothing to one which already committed,
// and without it the lock outlives the process which took it.
func begin(cmd command, globals *globals, stderr io.Writer) (*dfcad.Tx, int, bool) {
	reportLoading(cmd, globals, stderr)

	tx, diags, err := dfcad.Begin(globals.Root)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return nil, exitLoad, false
	}

	render(diags, stderr)

	if tx == nil {
		return nil, exitLoad, false
	}

	tx.DryRun = globals.DryRun

	return tx, exitSuccess, true
}

// commitChange writes the change and reports it, which is the same last step for
// every command which changes the model.
//
// It is not called `commit` because that name is spoken for: the standard
// pipeline stamps this package's build with -X main.commit, and -X takes effect
// only on a string variable of exactly that name. A function there instead is a
// binary which silently reports no commit at all, so the name belongs to the
// variable in version.go and this is the one which moved.
func commitChange(cmd command, tx *dfcad.Tx, globals *globals, stdout, stderr io.Writer) int {
	out, exit, ok := apply(cmd, tx, globals, stderr)
	if !ok {
		return exit
	}

	return emitted(cmd, stdout, stderr, writeResult{envelope: newEnvelope(cmd.name), Commit: out})
}

// apply writes the change, renders it for a person and says whether the run
// should go on to write a result at all.
//
// It is separate from [commitChange] because what a command has to say about a change
// is not the same for all of them: a claim written or retracted is reported
// beside the commit, and the object carrying both is the command's rather than
// this layer's.
func apply(cmd command, tx *dfcad.Tx, globals *globals, stderr io.Writer) (dfcad.Commit, int, bool) {
	out, diags, err := tx.Commit()

	refused := render(diags, stderr)

	if err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return dfcad.Commit{}, exitLoad, false
	}

	// A refused change produced no result, so it writes nothing at all to
	// stdout: the diagnostics on stderr are the whole of the answer, and an
	// object describing a change which did not happen would read as one which
	// did.
	if refused {
		return dfcad.Commit{}, exitLoad, false
	}

	reportCommit(out, globals, stderr)

	return out, exitSuccess, true
}

// emitted writes one result object to stdout, which is the last thing every
// write command does.
func emitted(cmd command, stdout, stderr io.Writer, result any) int {
	if err := emit(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// render writes diagnostics to stderr and reports whether any of them refuses
// the change.
//
// It is the one place which decides how a diagnostic reaches a person, so that a
// read and a write report the model's problems the same way.
func render(diags []dfcad.Diagnostic, stderr io.Writer) bool {
	var collected dfcad.Diagnostics
	collected.Add(diags...)

	// The files are read from disk to quote them, which is right in every case:
	// a read wrote to none of them, a refused change wrote nothing, and a change
	// which was written is what is there.
	_ = collected.Render(stderr, dfcad.FileSources{})

	return collected.HasErrors()
}

// reportLoading says that the model is about to be read, which every command
// which reads one says the same way.
func reportLoading(cmd command, globals *globals, stderr io.Writer) {
	if globals.Verbosity >= verbosityProgress {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: loading the model beneath %s\n", cmd.name, globals.Root)
	}
}

// reportCommit renders a write for a person, on stderr.
func reportCommit(out dfcad.Commit, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	// The detail behind the summary is progress rather than result — the result
	// is on stdout — so it is behind the verbosity flag.
	if globals.Verbosity >= verbosityProgress {
		for _, file := range out.Files {
			_, _ = fmt.Fprintf(stderr, "%s: %s (%s)\n", file.Path, file.Status, join(spelledEffects(file.Effects, true)))
		}
	}

	written := "wrote"
	if out.DryRun {
		written = "would write"
	}

	// The files which would change rather than the ones which did, because a dry
	// run wrote none whatever it would have written, and reporting that as "0
	// files" says the change did nothing rather than that it was not made.
	_, _ = fmt.Fprintf(stderr, "%s %s: %s\n",
		written,
		plural(len(out.Changed()), "file"),
		join(spelledEffects(out.Effects(), false)),
	)
}

// spelledEffects says what a change did to the model, for a person.
//
// The tag is written where the effects of one file are listed and left out of
// the summary, because the summary is read as a sentence about the change and
// the listing is read as a line per thing.
func spelledEffects(effects []dfcad.Effect, tagged bool) []string {
	spelled := make([]string, 0, len(effects))
	for _, effect := range effects {
		parts := []string{string(effect.Op)}
		if tagged {
			parts = append(parts, effect.Tag)
		}
		spelled = append(spelled, strings.TrimSpace(strings.Join(append(parts, string(effect.ID)), " ")))
	}

	return spelled
}
