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
const (
	// exitSuccess reports that the command did what was asked.
	exitSuccess = 0

	// exitUsage reports that the invocation itself was wrong — no subcommand,
	// an unknown one, or a malformed flag. Nothing was loaded and nothing ran.
	exitUsage = 2
)

const usage = `dfcad — a data-first CAD engine.

Usage:

	dfcad <command> [arguments]

No commands are available yet.

Flags:

	-h, --help   print this message and exit
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
	default:
		fmt.Fprintf(stderr, "dfcad: unknown command %q\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
}
