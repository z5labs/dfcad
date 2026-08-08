// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/z5labs/dfcad"
)

const applyUsage = `dfcad apply — apply a batch of edits in one change.

Usage:

	dfcad apply [flags] [<file>]

The operation file is an ordered list of authoring operations, and they are
applied as one transaction: the model is read once, every operation is applied
to it in order, and the model they produce together is validated once. Fifty
edits therefore cost one load rather than fifty, and a mistake in the fiftieth
leaves none of the other forty-nine applied.

Reading is from standard input where no file is named, or where the file is
named ` + "`-`" + `, so a batch can be generated and piped in. A relative path is
resolved against the model root, as every path this interface takes is.

The file is one JSON object:

	{
	  "version": 1,
	  "operations": [
	    {"op": "add-vertex", "id": "geom:V-07", "frame": "frame:building",
	     "predicate": "position",
	     "claim": {"value": "12.0 0.0 0.0", "unit": "m",
	               "source": "Interior control set IC-02",
	               "method": "method:total-station",
	               "accuracy": ["independent 0.004 m"],
	               "date": "2026-02-18"}},
	    {"op": "add-edge", "id": "geom:E-08", "frame": "frame:building",
	     "start": "geom:V-05", "end": "geom:V-07"}
	  ]
	}

Each operation's ` + "`op`" + ` is the name of the command which makes the same change
on its own, and its other members are that command's flags and arguments. Every
operation there is, and every member of each, is tabled in
docs/operation-file.md.

The file is validated before anything is applied, and every problem it has comes
back at once: a member no operation of that name reads, an operation nothing
declares, a member an operation requires and which was not written. Each names
the operation it is about by its place in the list, counted from one.

An operation may name what an earlier one wrote. The ids the batch has already
written are taken, resolve as references and answer as the subject of a claim,
which is what makes a node and the claims about it, or an edge between two
vertices of the same batch, one statement rather than several changes which have
to be applied in turn. Nothing is validated against the model as it stands
halfway through: the model the whole batch produces is what is validated, so an
end state which loads is accepted however its intermediate states would have
read.

What a batch has written is what it wrote, not what it means. An operation which
reads what the model says — a supersession, which finds the one live claim of a
subject and predicate, and a deprecation, which finds the claim an id names —
sees the claims the model held when the batch began. Retiring a node the batch
itself created, or retracting a claim it just wrote, is refused: a batch which
creates a thing and withdraws it again is a batch which should not have created
it.

Any failure applies nothing at all. The operation which failed is named, with
its place in the list and what was wrong with it, and the model is exactly as it
was: the correct response is to fix the file and reissue it, because there is no
partial state to reconcile.

` + globalFlagsHelp + `
` + writeFlagsHelp + `
` + outputContractHelp + `
` + writeOutputHelp + `
It also carries "operations", one entry per operation in the order they were
applied, each with its place in the list, what it did to the model, the claim it
wrote or retracted, and what it had to say about the model it produced; and
"totals", how much was created, modified and retired by the batch as a whole.
`

// ErrTooManyBatches is an apply given more than one operation file.
//
// It takes one because a batch is one change: two files applied together are
// one batch and are written as one, and applying them in turn is two commands.
var ErrTooManyBatches = errors.New("expected one operation file, or none to read standard input")

// stdinPath is the file which names standard input, written where a batch is
// piped in and the invocation names it explicitly.
const stdinPath = "-"

// applyResult is the object apply writes to stdout.
//
// It is the write result with what each operation did beside it, rather than a
// shape of its own, so that a caller reading .files and .dryRun reads them the
// same way whichever command wrote them.
type applyResult struct {
	envelope
	dfcad.Commit

	// Operations is what each operation of the batch did, in the order they
	// were applied.
	Operations []operationEntry `json:"operations"`

	// Totals is what the batch did to the model as a whole.
	Totals totals `json:"totals"`

	// Notices are what the change had to say about the model it produced, in
	// the order the operations reported them. Empty rather than null when it
	// had nothing to say.
	Notices []noticeEntry `json:"notices"`
}

// operationEntry is what one operation of the batch did.
type operationEntry struct {
	// Index is its place in the batch, counted from one as a person counts a
	// list, which is how a refusal names one.
	Index int `json:"index"`

	// Op is the operation it was.
	Op string `json:"op"`

	// Effects are what it did to the model, in the order the mutations were
	// applied. They are the same effects the files carry, grouped by the
	// operation which caused them instead.
	Effects []dfcad.Effect `json:"effects"`

	// Claim is the id of the claim it wrote, and is absent where it wrote none
	// or wrote one with no id of its own.
	Claim string `json:"claim,omitempty"`

	// Replaced is the id of the claim it retracted, and is absent for an
	// operation which retracted none.
	Replaced string `json:"replaced,omitempty"`

	// Snaps is every corner a scaffold landed on a vertex the model already
	// held, and is absent for every other operation.
	Snaps []snapEntry `json:"snaps,omitempty"`

	// Notices are what it had to say about the model it produced.
	Notices []noticeEntry `json:"notices"`
}

// totals is what a batch did to the model, counted over every operation.
//
// It is counted here rather than left to the caller because it is the answer to
// "did that do what I asked": an author who submitted fifty operations reads one
// line and then goes looking only if it surprises them.
type totals struct {
	// Operations is how many operations were applied.
	Operations int `json:"operations"`

	// Created, Modified and Retired are how many things the batch did each of
	// to the model. They count effects rather than files: what an author asked
	// for is a node, not the file it landed in.
	Created  int `json:"created"`
	Modified int `json:"modified"`
	Retired  int `json:"retired"`
}

// runApply is the apply command.
func runApply(cmd command, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(arguments) > 1 {
		return usageError(cmd, ErrTooManyBatches, stderr, true)
	}

	// The batch is read and checked before the model is, because a file which
	// is not a batch is wrong whatever the model holds — and reading the whole
	// tree before saying so buries the one thing which is.
	batch, exit, ok := batched(cmd, globals, arguments, stdin, stderr)
	if !ok {
		return exit
	}

	tx, exit, ok := begin(cmd, globals, stderr)
	if !ok {
		return exit
	}
	defer func() { _ = tx.Close() }()

	applied, err := tx.Apply(batch)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	notices := gathered(applied)

	reportApplied(cmd, applied, globals, stderr)

	// A reuse and a duplicate written beside an existing vertex are warnings
	// whether or not anybody asked to read the run, exactly as they are when a
	// scaffold is the whole command.
	for _, operation := range applied {
		reportSnaps(cmd, operation.Snaps, operation.Tolerance, stderr)
	}

	out, exit, ok := apply(cmd, tx, globals, stderr)
	if !ok {
		return exit
	}

	reportNotices(cmd, notices, stderr)

	result := applyResult{
		envelope:   newEnvelope(cmd.name),
		Commit:     out,
		Operations: performed(applied),
		Totals:     counted(applied),
		Notices:    noticed(notices),
	}

	reportTotals(result.Totals, globals, stderr)

	return emitted(cmd, stdout, stderr, result)
}

// batched reads the operation file the invocation named, which is standard
// input where it named none or named `-`.
//
// A file which cannot be read and one which is not a batch are both load
// failures rather than usage errors: the invocation was well formed and the
// input it named could not be read, which is exactly the distinction the exit
// codes exist to draw. What is wrong with a batch is reported in full — every
// operation which has a problem, in the order they were written — rather than
// one problem per run.
func batched(
	cmd command,
	globals *globals,
	arguments []string,
	stdin io.Reader,
	stderr io.Writer,
) (dfcad.Batch, int, bool) {
	source, name := stdin, "standard input"

	if len(arguments) == 1 && arguments[0] != stdinPath {
		path := globals.resolve(arguments[0])

		file, err := os.Open(path)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
			return dfcad.Batch{}, exitLoad, false
		}
		defer func() { _ = file.Close() }()

		source, name = file, path
	}

	if globals.Verbosity >= verbosityProgress {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: reading the batch from %s\n", cmd.name, name)
	}

	batch, err := dfcad.ParseBatch(source)
	if err != nil {
		for _, problem := range problems(err) {
			_, _ = fmt.Fprintf(stderr, "dfcad %s: %s: %v\n", cmd.name, name, problem)
		}
		return dfcad.Batch{}, exitLoad, false
	}

	if globals.Verbosity >= verbosityProgress {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %s\n", cmd.name, plural(len(batch.Operations), "operation"))
	}

	return batch, exitSuccess, true
}

// problems is every problem a refused batch has, which is one per operation
// which has one and the refusal itself where the file could not be read at all.
func problems(err error) []error {
	var batch dfcad.BatchError
	if errors.As(err, &batch) {
		return batch.Errs
	}
	return []error{err}
}

// performed is each operation as the result object carries it.
func performed(applied []dfcad.Applied) []operationEntry {
	// Made rather than declared so that the list is empty rather than null,
	// which a caller indexing it needs no special case for.
	out := make([]operationEntry, 0, len(applied))

	for _, operation := range applied {
		entry := operationEntry{
			Index:    operation.Index,
			Op:       operation.Operation,
			Effects:  operation.Effects,
			Claim:    string(operation.Claim),
			Replaced: string(operation.Replaced),
			Notices:  noticed(operation.Notices),
		}

		if entry.Effects == nil {
			entry.Effects = []dfcad.Effect{}
		}
		if len(operation.Snaps) > 0 {
			entry.Snaps = snapped(operation.Snaps)
		}

		out = append(out, entry)
	}

	return out
}

// counted is what the batch did to the model as a whole.
func counted(applied []dfcad.Applied) totals {
	out := totals{Operations: len(applied)}

	for _, operation := range applied {
		for _, effect := range operation.Effects {
			switch effect.Op {
			case dfcad.OpCreated:
				out.Created++
			case dfcad.OpModified:
				out.Modified++
			case dfcad.OpRetired:
				out.Retired++
			}
		}
	}

	return out
}

// gathered is every notice the batch produced, in the order the operations
// reported them.
func gathered(applied []dfcad.Applied) []dfcad.Notice {
	var out []dfcad.Notice
	for _, operation := range applied {
		out = append(out, operation.Notices...)
	}
	return out
}

// reportApplied says what each operation did, on stderr.
//
// It is progress rather than result — the result is on stdout — so it is behind
// the verbosity flag, and it is written before the change is committed because
// that is when it happened: a batch refused by the model it produced has still
// told the author which of their operations wrote what.
func reportApplied(cmd command, applied []dfcad.Applied, globals *globals, stderr io.Writer) {
	if globals.Verbosity < verbosityProgress {
		return
	}

	for _, operation := range applied {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %d %s: %s\n",
			cmd.name,
			operation.Index,
			operation.Operation,
			join(spelledEffects(operation.Effects, true)),
		)
	}
}

// reportTotals renders what the batch did for a person, on stderr.
func reportTotals(counts totals, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	_, _ = fmt.Fprintf(stderr, "%s: %d created, %d modified, %d retired\n",
		plural(counts.Operations, "operation"),
		counts.Created,
		counts.Modified,
		counts.Retired,
	)
}
