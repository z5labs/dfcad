// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/z5labs/dfcad"
)

const fmtUsage = `dfcad fmt — rewrite entity files into canonical form.

Usage:

	dfcad fmt [flags] [paths...]

A path names a file, which is formatted whatever its extension, or a directory,
beneath which every file ending in .dfc is formatted. Several of each may be
given. With no path, the tree beneath the current directory is formatted.

A file is replaced atomically, so an interrupted run leaves the file it was
writing as it was rather than half of what it was going to be. A file that does
not parse is reported and left untouched, and the files after it are still
formatted.

Flags:

	--check   write nothing, and report which files are not in canonical form
	--diff    write nothing, and print what would change as a unified diff
	-h        print this message and exit

--diff implies --check: nothing is written, and the exit code says whether
anything would change.

Exit codes:

	0   every file is in canonical form, or was rewritten into it
	1   --check or --diff: a file is not in canonical form
	2   a file could not be read, did not parse, or could not be written
	3   the invocation was wrong

Results go to stdout as one JSON object. Diagnostics, the list of files that
are not in canonical form, and any diff go to stderr.
`

// fmtVersion is the version of the object fmt writes to stdout.
//
// A caller reading a documented field keeps working across releases: fields
// are added compatibly, and one is never removed, renamed or given a different
// meaning without this changing.
const fmtVersion = 1

// fmtResult is the object fmt writes to stdout, and is the whole of stdout.
//
// It is a struct rather than a map so that its keys come out in a fixed order,
// which is half of what makes two runs over the same tree byte-identical. The
// other half is that the files below arrive in the order the walk yields them.
type fmtResult struct {
	// Version is the version of this object's shape.
	Version int `json:"version"`

	// Command names what produced it, so that a caller reading a collected
	// result knows which contract it is reading.
	Command string `json:"command"`

	// Files is one entry per file the command reached, in walk order, plus one
	// for each path it could not reach at all.
	Files []fmtFile `json:"files"`
}

// fmtFile is what formatting did to one file, or, where the status is failed
// and an error is carried, what stopped a path being reached.
type fmtFile struct {
	// Path is the file, exactly as the walk reached it. A path that could not
	// be reached is reported here as it was given, which need not name a file:
	// a directory that cannot be read and a name that is not there both land
	// here with a failed status.
	Path string `json:"path"`

	// Status is one of the fmtStatus values below.
	Status string `json:"status"`

	// Diagnostics are the problems found in the file's contents, in the same
	// form the human rendering on stderr was produced from. Neither is derived
	// by parsing the other.
	Diagnostics []dfcad.Diagnostic `json:"diagnostics,omitempty"`

	// Error is the failure that stopped the file being read or written, where
	// the failure is not about the file's contents and so has no diagnostic.
	Error string `json:"error,omitempty"`
}

// The statuses one file can come back with.
const (
	// statusUnchanged means the file was already in canonical form.
	statusUnchanged = "unchanged"

	// statusFormatted means the file was rewritten into canonical form.
	statusFormatted = "formatted"

	// statusUnformatted means the file is not in canonical form and nothing
	// was written, which is what --check and --diff report.
	statusUnformatted = "unformatted"

	// statusFailed means the file could not be read, did not parse, or could
	// not be written. Nothing is known about whether it is canonical.
	statusFailed = "failed"
)

// runFmt is the fmt command.
func runFmt(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("fmt", flag.ContinueOnError)

	// The flag package writes its own usage, which is neither this message nor
	// on the stream this command was handed. Silencing it leaves both to the
	// code below.
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}

	check := flags.Bool("check", false, "")
	diff := flags.Bool("diff", false, "")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// Help was asked for, so it is the result rather than a
			// diagnostic.
			fmt.Fprint(stdout, fmtUsage)
			return exitSuccess
		}

		fmt.Fprintf(stderr, "dfcad fmt: %v\n\n", err)
		fmt.Fprint(stderr, fmtUsage)
		return exitUsage
	}

	paths := flags.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}

	formatter := dfcad.Formatter{Rewrite: !*check && !*diff, Diff: *diff}
	formatted := formatter.Format(paths...)

	report(formatted, stderr)

	if err := emit(formatted, stdout); err != nil {
		fmt.Fprintf(stderr, "dfcad fmt: %v\n", err)
		return exitLoad
	}

	return code(formatted)
}

// report writes the human rendering of a run to stderr: the diagnostics of
// every file that has any, the failures that are not about a file's contents,
// and each file that is not in canonical form, with its diff where one was
// asked for.
//
// Nothing here goes to stdout. A caller piping stdout into jq gets one JSON
// object whatever this printed.
func report(formatted []dfcad.Formatted, stderr io.Writer) {
	src := dfcad.FileSources{}

	for _, file := range formatted {
		for _, diagnostic := range file.Diagnostics {
			// A diagnostic renders itself; the file it points at is re-read
			// from disk to quote it, which is correct because a file that
			// failed is a file nothing was written to.
			_ = diagnostic.Render(stderr, src)
		}

		if file.Err != nil {
			fmt.Fprintf(stderr, "dfcad fmt: %v\n", file.Err)
		}

		if file.Changed && !file.Written {
			fmt.Fprintf(stderr, "%s: not in canonical form\n", file.Path)
		}
		if file.Diff != "" {
			fmt.Fprint(stderr, file.Diff)
		}
	}
}

// emit writes the result object to stdout.
func emit(formatted []dfcad.Formatted, stdout io.Writer) error {
	result := fmtResult{
		Version: fmtVersion,
		Command: "fmt",

		// The slice is made rather than declared so that a run reaching no
		// file writes an empty list rather than a null, and a caller indexing
		// it needs no special case for the empty tree.
		Files: make([]fmtFile, 0, len(formatted)),
	}

	for _, file := range formatted {
		out := fmtFile{Path: file.Path, Status: status(file), Diagnostics: file.Diagnostics}
		if file.Err != nil {
			out.Error = file.Err.Error()
		}
		result.Files = append(result.Files, out)
	}

	encoder := json.NewEncoder(stdout)

	// Escaping the characters that matter in HTML would rewrite bytes of a
	// path or a message that mean nothing of the sort here, and the output is
	// read by a pipeline rather than embedded in a page.
	encoder.SetEscapeHTML(false)

	return encoder.Encode(result)
}

// status is what one file came back as.
func status(file dfcad.Formatted) string {
	switch {
	case file.Failed():
		return statusFailed
	case file.Written:
		return statusFormatted
	case file.Changed:
		return statusUnformatted
	default:
		return statusUnchanged
	}
}

// code is the exit code of a run.
//
// A failure outranks a file that is merely not canonical: a run that could not
// read half the tree has not answered the question the other half answered,
// and reporting the answer it does have would be reporting on a tree nobody
// asked about.
func code(formatted []dfcad.Formatted) int {
	var failed, unformatted bool

	for _, file := range formatted {
		switch {
		case file.Failed():
			failed = true
		case file.Changed && !file.Written:
			unformatted = true
		}
	}

	switch {
	case failed:
		return exitLoad
	case unformatted:
		return exitCheck
	default:
		return exitSuccess
	}
}
