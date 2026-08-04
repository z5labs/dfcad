// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"

	"github.com/z5labs/dfcad"
)

const routeUsage = `dfcad route — say which file a new node would be written to.

Usage:

	dfcad route [flags] <id>

The registry's routing rules decide which file a newly authored node belongs
in, from the namespace of its id, the kind it declares and the type it
declares. This command answers that question on its own, writing nothing: it
is how an author checks where something would land before authoring it, and it
is the same decision every write command makes.

Flags:

	--kind <kind>    the kind the new node will declare
	--type <type>    the type the new node will declare
	--file <path>    write it here instead, overriding the rules; a path
	                 relative to the model root, ending in ` + dfcad.Extension + `

A vertex, an edge and a loop carry neither a kind nor a type, so routing one
means leaving both flags out. Such a node is matched by a rule which matches on
its namespace alone, or by one which matches on nothing.

Exactly one rule must match. A node matched by none, and a node matched by more
than one, are both a usage error naming the node and every rule consulted —
never a silent default. A filing decision the tool makes on its own is one that
is visible in nothing the author wrote, and the fix for both is a change to the
registry: overlapping rules are made disjoint, and a node nothing covers gets a
rule.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object route writes carries "destination": the file, the rule which chose
it, whether it was overridden, and whether the model already holds that file.
`

// routeResult is the object route writes to stdout.
type routeResult struct {
	envelope

	// Subject is the node the decision was about, echoed back so that a
	// collected result says what was asked as well as what was answered.
	Subject routedSubject `json:"subject"`

	// Destination is where the node would be written.
	Destination routedDestination `json:"destination"`
}

// routedSubject is the three axes the rules matched on.
type routedSubject struct {
	// ID is the id the node would be written with.
	ID string `json:"id"`

	// Kind is the kind it would declare. Empty for a geometric node.
	Kind string `json:"kind,omitempty"`

	// Type is the type it would declare. Empty for a geometric node.
	Type string `json:"type,omitempty"`
}

// routedDestination is the answer: where, why there, and whether the file is
// one the model already holds.
type routedDestination struct {
	// Path is the target file, relative to the model root.
	Path string `json:"path"`

	// Rule is the routing rule which chose it. Absent when the destination was
	// overridden, because an override names no rule and a caller must not go
	// looking in the registry for one.
	Rule string `json:"rule,omitempty"`

	// Overridden reports whether --file named the destination outright.
	Overridden bool `json:"overridden"`

	// Exists reports whether the model already holds that file. A destination
	// which does not is created, with any directories above it, by the write
	// which lands there.
	Exists bool `json:"exists"`
}

// MissingIDError is a route invocation which named no node.
type MissingIDError struct{}

// Error implements [error].
func (MissingIDError) Error() string {
	return "no id: route takes the id the new node would be written with"
}

// runRoute is the route command.
func runRoute(cmd command, args []string, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd.name, globals)

	kind := flags.String("kind", "", "")
	declaredType := flags.String("type", "", "")
	file := flags.String("file", "", "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	switch {
	case len(arguments) == 0:
		return usageError(cmd, MissingIDError{}, stderr, true)
	case len(arguments) > 1:
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments[1:]}, stderr, true)
	}

	// An id which is not one is answered before the model is read: nothing about
	// the tree changes the answer, and reporting a load of the whole model
	// before saying the argument was not an id buries the one thing that is
	// wrong.
	id, err := dfcad.ParseID(arguments[0])
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	// The model is loaded before the flags are checked because the registry is
	// what says whether a type is declared and what the routing rules are, and
	// the registry is the model.
	graph := loadModel(cmd, globals, stderr)
	registry := graph.Registry()

	if err := checkAxes(registry, *kind, *declaredType); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	subject := dfcad.Subject{ID: id, Kind: dfcad.Kind(*kind), Type: *declaredType}

	destination, err := decide(registry, subject, *file)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	result := routeResult{
		envelope: newEnvelope(cmd.name),
		Subject:  routedSubject{ID: string(id), Kind: *kind, Type: *declaredType},
		Destination: routedDestination{
			Path:       destination.Path,
			Rule:       destination.Rule,
			Overridden: destination.Overridden,
			Exists:     holds(globals.resolve(destination.Path)),
		},
	}

	reportDestination(result, globals, stderr)

	if err := emit(stdout, result); err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// decide is the destination of a subject, which --file names outright and the
// registry's rules choose otherwise.
//
// The override is not checked against the rules. That is the whole of what an
// override is for: the rules describe where things ordinarily go, and the one
// command which needs somewhere else says so rather than having a rule written
// for it. What is still checked is that the path names somewhere a node can be
// written at all, because a file no walk of the model reaches is a change which
// appears to have been made and was not.
func decide(registry *dfcad.Registry, subject dfcad.Subject, file string) (dfcad.Destination, error) {
	if file != "" {
		return dfcad.Override(file)
	}
	return registry.Destination(subject)
}

// checkAxes reports a kind or a type which names nothing, for the reason
// [checkFilters] does: a name nobody declared and a name nothing matches are
// different answers, and a caller which cannot tell them apart retries a
// misspelling forever.
func checkAxes(registry *dfcad.Registry, kind, declaredType string) error {
	if kind != "" && !slices.Contains(dfcad.Kinds(), dfcad.Kind(kind)) {
		return UnknownKindError{Kind: kind, Known: dfcad.Kinds()}
	}

	if declaredType != "" && !registry.Declares(dfcad.SortType, declaredType) {
		return UnknownTypeError{Type: declaredType, Declared: registry.Names(dfcad.SortType)}
	}

	return nil
}

// holds reports whether the model already holds the file at path.
//
// Anything which is not an ordinary absence — a path which cannot be reached at
// all — is reported as not held, because this field says what the write will do
// and a write into an unreachable path creates nothing. The write itself is
// what reports why, with the error the file system gave it.
func holds(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return !errors.Is(err, fs.ErrNotExist)
	}
	return !info.IsDir()
}

// reportDestination renders a route result for a person, on stderr.
func reportDestination(result routeResult, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	because := "rule " + result.Destination.Rule
	if result.Destination.Overridden {
		because = "overridden"
	}

	held := "a new file"
	if result.Destination.Exists {
		held = "an existing file"
	}

	fmt.Fprintf(stderr, "%s -> %s (%s, %s)\n",
		result.Subject.ID, result.Destination.Path, because, held)
}
