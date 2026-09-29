// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"io"

	"github.com/z5labs/dfcad"
)

const addAssertionUsage = `dfcad add-assertion — write a check the thing it names has to satisfy.

Usage:

	dfcad add-assertion [flags] <subject> <check>

An assertion is a named check and its parameters, written on the one thing it
constrains: a node, a vertex, an edge or a loop. There is no expression
language — the check is one the engine registers, and what each parameter means
is what that check declares — so what an assertion requires is readable from
what is written.

Flags:

	--parameter "<name> <value>..."
	                     one parameter, written as the entity format writes
	                     it without its parentheses: "tolerance boundary-closure",
	                     "predicate width", "zone site:Z-90". Repeat for more
	                     than one; a check which takes none is written with none

Which sort of datum a parameter takes is the check's declaration, exactly as
which shape a claim's value takes is the predicate's, so a parameter is read by
the code a load reads one with. Refused before anything is written, as a usage
error naming what was wrong: a subject nothing holds; a frame, which is registry
data and carries no assertion; a check the engine does not register, naming the
ones it does; a parameter the check does not take, one it requires and which is
missing, and a value which is not the sort the check declares.

Refused when the model this would produce is interpreted, with the diagnostics a
load of that model would have raised: a check which cannot examine the subject's
form, kind or geometry; an assertion restating a value the subject's claims
already carry; and a parameter naming an id nothing holds.

What must hold of every instance of a type is not this. That is the type's
invariant, which is registry data and stays an edit to the registry file which
declares the type.

` + globalFlagsHelp + `
` + writeFlagsHelp + `
` + outputContractHelp + `
` + writeOutputHelp

// ErrMissingCheck is an add-assertion given a subject but no check to apply to
// it.
var ErrMissingCheck = errors.New("expected the check to apply, found one argument")

// runAddAssertion is the add-assertion command.
func runAddAssertion(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	parameters := &repeated{}
	flags.Var(parameters, "parameter", "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	id, exit, ok := subject(cmd, arguments, 2, stderr)
	if !ok {
		return exit
	}

	if len(arguments) < 2 {
		return usageError(cmd, ErrMissingCheck, stderr, true)
	}

	// A parameter which is not one is answered before the model is read, for
	// the reason a malformed id is: nothing about the tree makes it read.
	for _, written := range *parameters {
		if _, err := dfcad.ParseParameter(written); err != nil {
			return usageError(cmd, err, stderr, false)
		}
	}

	tx, exit, ok := begin(cmd, globals, stderr)
	if !ok {
		return exit
	}
	defer func() { _ = tx.Close() }()

	spec := dfcad.AssertionSpec{Subject: id, Check: arguments[1], Parameters: *parameters}

	if err := tx.AddAssertion(spec); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	return commitChange(cmd, tx, globals, stdout, stderr)
}
