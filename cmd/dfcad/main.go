// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Command dfcad is the command line interface to the dfcad engine.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// command is one dfcad subcommand.
//
// A subcommand is a value in [commands] rather than a case in a switch so that
// everything which has to be true of every subcommand — that it takes the
// global flags, that its help describes the contract and exits zero, that it
// writes nothing but a result object to stdout — is checkable by walking the
// list, rather than by remembering to add each new one to a test.
type command struct {
	// name is what selects it on the command line.
	name string

	// summary is its line in the top-level usage.
	summary string

	// usage is its own help message, printed for -h and for a malformed
	// invocation.
	usage string

	// writes reports whether the subcommand changes the model, which is what
	// says it takes --dry-run and holds the model root while it runs.
	//
	// It is a field rather than something a test lists, for the reason the
	// table itself is one: a write command added later takes the flag because
	// it is a write command, and the walk which checks that reaches it without
	// anybody remembering to add it.
	writes bool

	// run does the work and returns the exit code.
	//
	// The reader is the run's standard input, which one command reads a batch
	// of operations from and the rest ignore. It is a parameter rather than
	// [os.Stdin] reached for inside the command, for the reason the two writers
	// are: a test drives the whole command without a subprocess and without
	// touching the process's own streams.
	run func(cmd command, args []string, stdin io.Reader, stdout, stderr io.Writer) int
}

// commands is every subcommand, in the order the usage lists them.
var commands = []command{
	// First, because it is the one command which answers a question about the
	// tool rather than about a model — and the one somebody reaches for before
	// they have a model at all, or when they are writing down what they were
	// running when something went wrong.
	{
		name:    "version",
		summary: "say which build this is, and which contracts it implements",
		usage:   versionUsage,
		run:     runVersion,
	},
	{
		name:    "fmt",
		summary: "rewrite entity files into canonical form",
		usage:   fmtUsage,
		run:     runFmt,
	},
	{
		name:    "list-types",
		summary: "list the node types the model declares",
		usage:   listTypesUsage,
		run:     runListTypes,
	},
	{
		name:    "list-instances",
		summary: "list the instances of a type",
		usage:   listInstancesUsage,
		run:     runListInstances,
	},
	{
		name:    "get",
		summary: "retrieve one thing by its id, with its claims",
		usage:   getUsage,
		run:     runGet,
	},
	{
		name:    "resolve",
		summary: "answer one predicate about one thing, with its evidence",
		usage:   resolveUsage,
		run:     runResolve,
	},
	{
		name:    "traverse",
		summary: "walk the model: what contains, belongs to or borders what",
		usage:   traverseUsage,
		run:     runTraverse,
	},
	{
		name:    "claims",
		summary: "list every claim written on one thing",
		usage:   claimsUsage,
		run:     runClaims,
	},
	{
		name:    "conflicts",
		summary: "list every disagreement in the model",
		usage:   conflictsUsage,
		run:     runConflicts,
	},
	{
		name:    "route",
		summary: "say which file a new node would be written to",
		usage:   routeUsage,
		run:     runRoute,
	},
	{
		name:    "buildable",
		summary: "derive what may be built inside a boundary once its setbacks are taken off",
		usage:   buildableUsage,
		run:     runBuildable,
	},
	{
		name:    "site",
		summary: "decide whether one thing fits inside another, and how well that is known",
		usage:   siteUsage,
		run:     runSite,
	},
	{
		name:    "check",
		summary: "run every rule the model states and say whether it holds",
		usage:   checkUsage,
		run:     runCheck,
	},
	{
		name:    "review",
		summary: "report the changes in this revision which need an explanation",
		usage:   reviewUsage,
		run:     runReview,
	},
	{
		name:    "apply",
		summary: "apply a batch of edits from an operation file",
		usage:   applyUsage,
		run:     runApply,
		writes:  true,
	},
	{
		name:    "add-node",
		summary: "write a new semantic node",
		usage:   addNodeUsage,
		run:     runAddNode,
		writes:  true,
	},
	{
		name:    "add-vertex",
		summary: "write a new corner, with where it is and how that is known",
		usage:   addVertexUsage,
		run:     runAddVertex,
		writes:  true,
	},
	{
		name:    "add-edge",
		summary: "write a connection between two corners",
		usage:   addEdgeUsage,
		run:     runAddEdge,
		writes:  true,
	},
	{
		name:    "add-loop",
		summary: "write an ordered ring of edges",
		usage:   addLoopUsage,
		run:     runAddLoop,
		writes:  true,
	},
	{
		name:    "scaffold-loop",
		summary: "write a room's corners, walls and outline in one change",
		usage:   scaffoldLoopUsage,
		run:     runScaffoldLoop,
		writes:  true,
	},
	{
		name:    "set-label",
		summary: "change what a thing is called, and nothing else",
		usage:   setLabelUsage,
		run:     runSetLabel,
		writes:  true,
	},
	{
		name:    "retire",
		summary: "record that a thing stopped existing",
		usage:   retireUsage,
		run:     runRetire,
		writes:  true,
	},
	{
		name:    "add-claim",
		summary: "attach a measured value to a thing, with its provenance",
		usage:   addClaimUsage,
		run:     runAddClaim,
		writes:  true,
	},
	{
		name:    "supersede",
		summary: "correct a value: state the new one and retract the old",
		usage:   supersedeUsage,
		run:     runSupersede,
		writes:  true,
	},
	{
		name:    "deprecate-claim",
		summary: "record that a claim was retracted",
		usage:   deprecateClaimUsage,
		run:     runDeprecateClaim,
		writes:  true,
	},
}

// lookup is the subcommand of that name.
func lookup(name string) (command, bool) {
	for _, cmd := range commands {
		if cmd.name == name {
			return cmd, true
		}
	}
	return command{}, false
}

// globalFlagsHelp describes the flags every subcommand takes. It is part of
// every help message, because a flag documented in one place and accepted in
// all of them is a flag nobody finds.
const globalFlagsHelp = `Global flags, taken by every command:

	--root <dir>     the model root; a relative path is resolved against it
	                 (default ".")
	--format <fmt>   how the run reports itself to a person on stderr: json,
	                 which reports only problems, or human, which adds a
	                 readable summary of the result. Neither changes stdout
	                 (default "json")
	-v, --verbose    say more on stderr about what the run is doing and about
	                 what it found; repeat for more
	-h, --help       print this message and exit
`

// writeFlagsHelp describes the flags every command which changes the model
// takes beyond the global ones.
//
// It is written once, for the reason [globalFlagsHelp] is: a dry run which
// meant one thing for one command and something else for another is a flag
// nobody can rely on, and a command added later cannot forget it.
const writeFlagsHelp = `Flags every command which changes the model takes:

	--dry-run        do everything except the writing: load, apply the change,
	                 validate the model it would produce, and report what would
	                 have changed with the diff of every file
`

// outputContractHelp describes the two streams, the versioning rule and the
// exit codes. It is part of every help message for the same reason as the
// flags: the contract is the interface, and an interface documented only in a
// file somewhere in the repository is one a caller has to go looking for.
const outputContractHelp = `Output:

	Stdout is one JSON object and nothing else, so it can be piped into jq
	without filtering prose out of it first. The object carries a "version"
	field: fields are added compatibly, and a field is never removed, renamed
	or given a different meaning without that number changing. The same input
	produces byte-identical stdout.

	A run which produces no result — help, a usage error, a model root that
	cannot be read — writes nothing at all to stdout.

	Diagnostics, progress and everything else for a person go to stderr, on
	every run and in every format. Nothing human-facing is ever on stdout.

Exit codes:

	0   success: the command did what was asked
	1   check failure: it ran and answered, and the answer is no
	2   load failure: input could not be read, did not parse, or was not
	    written
	3   usage error: the invocation itself was wrong
	4   ambiguous: resolution could not choose between the claims, and every
	    one it could not choose between is in the result
	5   strict ambiguity: the same, under a predicate the registry declares
	    strict, for which no answer is safer than an arbitrary one

The shape of each command's object is documented in docs/machine-output.md.
`

// usageHead is the top-level help, up to the list of commands.
const usageHead = `dfcad — a data-first CAD engine.

Usage:

	dfcad <command> [flags] [arguments]

Commands:

`

// usage is the top-level help.
//
// It is built from [commands] so that a subcommand cannot exist without being
// listed, which is the sort of drift nobody notices until they go looking for
// a command that is right there.
func usage() string {
	var out strings.Builder

	out.WriteString(usageHead)

	width := 0
	for _, cmd := range commands {
		width = max(width, len(cmd.name))
	}
	for _, cmd := range commands {
		fmt.Fprintf(&out, "\t%-*s   %s\n", width, cmd.name, cmd.summary)
	}

	out.WriteString("\n")
	out.WriteString(globalFlagsHelp)
	out.WriteString("\n")
	out.WriteString(outputContractHelp)
	out.WriteString("\nRun `dfcad <command> -h` for the arguments of a command.\n")

	return out.String()
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the whole of main, minus the process. Keeping it a function of its
// arguments and its writers is what lets a test drive the command without a
// subprocess and without touching the real os.Stdout.
//
// The input it reads is the process's, which is what a caller piping a batch
// into `dfcad apply` expects. [runOn] is the same run with the input handed to
// it, which is what lets a test drive that path without a subprocess either.
func run(args []string, stdout, stderr io.Writer) int {
	return runOn(args, os.Stdin, stdout, stderr)
}

// runOn is run with the standard input it reads given to it rather than taken
// from the process.
func runOn(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage())
		return exitUsage
	}

	switch args[0] {
	case "-h", "--help", "help":
		// Help was asked for, so it is a success rather than a usage error. It
		// is still for a person, so it is still on stderr and stdout stays
		// empty: a caller piping stdout gets a result object or nothing at
		// all, and never a page of prose in place of one.
		fmt.Fprint(stderr, usage())
		return exitSuccess
	}

	cmd, ok := lookup(args[0])
	if !ok {
		fmt.Fprintf(stderr, "dfcad: unknown command %q\n\n", args[0])
		fmt.Fprint(stderr, usage())
		return exitUsage
	}

	return cmd.run(cmd, args[1:], stdin, stdout, stderr)
}
