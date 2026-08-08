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

const classifyTypeUsage = `dfcad classify-type — say how a scheme outside this model names a type.

Usage:

	dfcad classify-type [flags] <type> "<system>" "<code>"

A type declares what its instances may be — which kinds, which geometry forms —
and nothing about what it is called anywhere else. This writes that: the name of
a scheme, and the code this project's type has within it. A type carries as many
of them as there are schemes worth mapping into, so one type can be an IFC class,
a Uniclass code and an OmniClass code at once.

Both strings are opaque. No scheme is known here, no code is checked against a
syntax, and nothing in the engine reads either value — the pair is written,
printed and reported back, and what it means is the business of whoever reads
this model against that scheme. That is what keeps a mapping to a foreign
vocabulary a line of registry data somebody reviews rather than a table compiled
into the tool.

A type carries at most one code per system. Classifying a type in a system it is
already classified in is refused, naming the code it already has: a second code
from one scheme is not a richer mapping, it is a disagreement nothing has a rule
for resolving. Correcting one is an edit to the registry file.

The change lands in the registry file the type was declared in, which is not a
routing decision: a type is where somebody wrote it, and this adds a child to
that declaration rather than writing a new form anywhere.

` + globalFlagsHelp + `
` + writeFlagsHelp + `
` + outputContractHelp + `
` + writeOutputHelp

// ErrMissingType is a classify-type with no type to classify.
var ErrMissingType = errors.New("expected the type to classify, found no argument")

// ErrMissingClassification is a classify-type given a type but not the pair to
// write on it.
//
// It is one error for both halves rather than one each, because neither is
// meaningful without the other and the usage line shows the shape: an invocation
// missing either is the same mistake, which is having stopped typing early.
var ErrMissingClassification = errors.New("expected a classification system and a code, found fewer than two arguments")

// runClassifyType is the classify-type command.
func runClassifyType(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	switch {
	case len(arguments) == 0:
		return usageError(cmd, ErrMissingType, stderr, true)
	case len(arguments) < 3:
		return usageError(cmd, ErrMissingClassification, stderr, true)
	case len(arguments) > 3:
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments[3:]}, stderr, true)
	}

	tx, exit, ok := begin(cmd, globals, stderr)
	if !ok {
		return exit
	}
	defer func() { _ = tx.Close() }()

	classification := dfcad.ExternalClassification{System: arguments[1], Code: arguments[2]}

	if err := tx.Classify(arguments[0], classification); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	return commitChange(cmd, tx, globals, stdout, stderr)
}
