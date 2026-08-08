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

const versionUsage = `dfcad version — say exactly which build this is.

Usage:

	dfcad version [flags]

It takes no arguments and reads no model. What it reports is three numbers
which move independently of one another:

	the build         the version and the commit this binary was built from
	the output        the version of the machine output contract every command
	                  writes against
	the entity format the version of the specification in SPEC.md that this
	                  engine loads and prints

A bug report which quotes this object identifies a commit rather than a
recollection of when the binary was installed, and says which contract the
output beside it was written against.

The version and the commit are stamped at link time by the Z5Labs standard
pipeline, so a binary built by ` + "`dagger call ... ci`" + ` and one built by
` + "`dagger call ... builder binary`" + ` report the same values for the same commit.
A plain ` + "`go build`" + ` stamps nothing, and such a binary says so: it reports
the placeholders below and "stamped": false, rather than reporting a version it
does not have.

	version   ` + unstampedVersion + `
	commit    ` + unstampedCommit + `

The tag convention the stamped version follows, and the relationship between
the three numbers, are in docs/versioning.md.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object version writes carries "build" — the version, the commit and whether
they were stamped — and "contracts", the version of the output contract above
and of the entity format.
`

// The values a binary nobody stamped reports.
//
// They are words rather than an empty string or a zero version so that the
// answer to "which build is this" is never blank and never plausible: "dev"
// beside "stamped": false is a binary somebody compiled themselves, and reads
// that way in a bug report without anyone having to know what an empty version
// would have meant.
const (
	unstampedVersion = "dev"
	unstampedCommit  = "unknown"
)

// version and commit are what this binary reports as its build, and are
// overwritten at link time by the Z5Labs standard pipeline with -X.
//
// The names are fixed by that pipeline rather than chosen here — it stamps
// main.version and main.commit into every Go application it builds, so that
// every z5labs binary answers "which build am I running" the same way. Renaming
// either of these, or moving one out of package main, silently produces an
// unstamped binary: the linker's -X is a no-op against a symbol that is not
// there. The CI job which runs the built binary and requires it to be stamped
// is what turns that silence into a failure.
//
// They are variables initialised from constants because that is the form -X
// requires: it takes effect only on a string variable declared uninitialised or
// initialised to a constant string expression.
var (
	version = unstampedVersion
	commit  = unstampedCommit
)

// versionResult is the object version writes to stdout.
//
// The build's version is nested under "build" rather than written at the top
// level because the envelope has already spent "version" on the output
// contract, and two different versions under one name is the sort of ambiguity
// a caller resolves by guessing. Nesting states which is which at the point of
// reading: .build.version is the tool, .version is the contract that object was
// written against.
type versionResult struct {
	envelope

	// Build is which build of the tool this is.
	Build buildStamp `json:"build"`

	// Contracts is the version of each interface the tool implements, as
	// distinct from the version of the tool implementing them.
	Contracts contractVersions `json:"contracts"`
}

// buildStamp is what the link line put in the binary.
type buildStamp struct {
	// Version is the tool's version: a tag pointing at the commit it was built
	// from, or "<short-sha>-<commit-time>" where no tag does.
	Version string `json:"version"`

	// Commit is the short SHA it was built from.
	Commit string `json:"commit"`

	// Stamped reports whether the two above came from the build or are the
	// placeholders a plain `go build` leaves.
	//
	// It is written rather than left for a caller to infer by comparing against
	// the placeholder strings, because that inference is a copy of this file's
	// constants in somebody else's code, and it would go stale silently. A false
	// here is the one thing a bug report needs to know before believing either
	// value beside it.
	Stamped bool `json:"stamped"`
}

// contractVersions is the version of each thing a caller programs against.
type contractVersions struct {
	// Output is the version of the machine output contract, which is the same
	// number the envelope of every object carries.
	Output int `json:"output"`

	// EntityFormat is the version of the entity format specification in
	// SPEC.md that this engine loads and prints.
	EntityFormat string `json:"entity-format"`
}

// runVersion is the version command.
//
// It reads no model, and still takes and validates the global flags the way
// every other command does — a --root that is not there is a load failure here
// too. That is deliberate: a flag which is accepted by every command and means
// nothing in one of them is a flag nobody can rely on, and the cost of the
// uniformity is that the one command with nothing to load checks a root it will
// not read.
func runVersion(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(arguments) > 0 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments}, stderr, true)
	}

	result := versionResult{
		envelope: newEnvelope(cmd.name),
		Build: buildStamp{
			Version: version,
			Commit:  commit,
			Stamped: stamped(),
		},
		Contracts: contractVersions{
			Output:       outputVersion,
			EntityFormat: dfcad.SpecVersion,
		},
	}

	reportVersion(result, globals, stderr)

	if err := emit(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// stamped reports whether the build values came from the link line.
//
// Either placeholder surviving is enough to answer no. A binary stamped with
// one value and not the other is not a build anybody can identify, and calling
// it stamped because half of it is would be the more confident of the two wrong
// answers.
func stamped() bool {
	return version != unstampedVersion && commit != unstampedCommit
}

// reportVersion renders a version result for a person, on stderr.
func reportVersion(result versionResult, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	build := fmt.Sprintf("dfcad %s (commit %s)", result.Build.Version, result.Build.Commit)
	if !result.Build.Stamped {
		build += " — unstamped, built outside the standard pipeline"
	}

	_, _ = fmt.Fprintln(stderr, build)
	_, _ = fmt.Fprintf(stderr, "output contract %d, entity format %s\n",
		result.Contracts.Output, result.Contracts.EntityFormat)
}
