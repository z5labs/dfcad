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
)

// Exit codes. Structured results go to stdout; everything human facing goes to
// stderr, so a caller can pipe stdout without parsing prose.
//
// The four are the ones documented in
// docs/decisions/0014-the-machine-output-contract-is-part-of-the-interface.md,
// and they are what they are so that a caller can branch on the code alone. A
// model that is wrong and an invocation that is wrong are completely different
// situations for a CI job, and telling them apart must not mean matching a
// message.
const (
	// exitSuccess reports that the command did what was asked.
	exitSuccess = 0

	// exitCheck reports that the command ran and answered, and the answer is
	// no: an assertion did not hold, a file is not in canonical form. Nothing
	// went wrong.
	exitCheck = 1

	// exitLoad reports that a file could not be read, did not parse, or could
	// not be written. Nothing downstream of it means anything.
	exitLoad = 2

	// exitUsage reports that the invocation itself was wrong — no subcommand,
	// an unknown one, or a malformed flag. Nothing was loaded and nothing ran.
	exitUsage = 3
)

const usage = `dfcad — a data-first CAD engine.

Usage:

	dfcad <command> [arguments]

Commands:

	fmt    rewrite entity files into canonical form

Flags:

	-h, --help   print this message and exit

Run ` + "`dfcad <command> -h`" + ` for the arguments and exit codes of a command.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the whole of main, minus the process. Keeping it a function of its
// arguments and its writers is what lets a test drive the command without a
// subprocess and without touching the real os.Stdout.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}

	switch args[0] {
	case "-h", "--help", "help":
		// Help was asked for, so it is the result rather than a diagnostic.
		fmt.Fprint(stdout, usage)
		return exitSuccess
	case "fmt":
		return runFmt(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "dfcad: unknown command %q\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
}
