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

const fmtUsage = `dfcad fmt — rewrite entity files into canonical form.

Usage:

	dfcad fmt [flags] [paths...]

A path names a file, which is formatted whatever its extension, or a directory,
beneath which every file ending in .dfc is formatted. Several of each may be
given. A relative path is resolved against the model root. With no path, the
tree beneath the model root is formatted.

A file is replaced atomically, so an interrupted run leaves the file it was
writing as it was rather than half of what it was going to be. A file that does
not parse is reported and left untouched, and the files after it are still
formatted.

Flags:

	--check   write nothing, and report which files are not in canonical form
	--diff    write nothing, and print what would change as a unified diff

--diff implies --check: nothing is written, and the exit code says whether
anything would change.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object fmt writes carries "files": one entry per file the run reached, in
walk order, each with its path, its status — unchanged, formatted, unformatted
or failed — and the machine-readable form of any diagnostic found in it.
`

// fmtResult is the object fmt writes to stdout, and is the whole of stdout.
//
// The envelope is embedded rather than nested so that "version" and "command"
// come out ahead of the payload, in the same place they do for every other
// command.
type fmtResult struct {
	envelope

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
func runFmt(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	check := flags.Bool("check", false, "")
	diff := flags.Bool("diff", false, "")

	paths, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(paths) == 0 {
		paths = []string{"."}
	}
	for i, path := range paths {
		paths[i] = globals.resolve(path)
	}

	if globals.Verbosity >= verbosityProgress {
		for _, path := range paths {
			_, _ = fmt.Fprintf(stderr, "dfcad fmt: searching %s\n", path)
		}
	}

	formatter := dfcad.Formatter{Rewrite: !*check && !*diff, Diff: *diff}
	formatted := formatter.Format(paths...)

	report(formatted, globals, stderr)

	if err := emit(stdout, result(formatted)); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad fmt: %v\n", err)
		return exitLoad
	}

	return code(formatted)
}

// report writes the human rendering of a run to stderr: the diagnostics of
// every file that has any, the failures that are not about a file's contents,
// and each file that is not in canonical form, with its diff where one was
// asked for. Asked for the human format, it adds the status of every file and
// a summary.
//
// Nothing here goes to stdout, in any format and at any verbosity. A caller
// piping stdout gets one JSON object whatever this printed.
func report(formatted []dfcad.Formatted, globals *globals, stderr io.Writer) {
	src := dfcad.FileSources{}

	for _, file := range formatted {
		for _, diagnostic := range file.Diagnostics {
			// A diagnostic renders itself; the file it points at is re-read
			// from disk to quote it, which is correct because a file that
			// failed is a file nothing was written to.
			_ = diagnostic.Render(stderr, src)
		}

		if file.Err != nil {
			_, _ = fmt.Fprintf(stderr, "dfcad fmt: %v\n", file.Err)
		}

		if file.Changed && !file.Written {
			_, _ = fmt.Fprintf(stderr, "%s: not in canonical form\n", file.Path)
		}
		if file.Diff != "" {
			_, _ = fmt.Fprint(stderr, file.Diff)
		}
	}

	if !globals.human() {
		return
	}

	counts := map[string]int{}
	for _, file := range formatted {
		counts[status(file)]++

		// The status of every file, rather than only of the ones something is
		// wrong with, is detail rather than result — the lines above already
		// name each file that failed or is not canonical.
		if globals.Verbosity >= verbosityProgress {
			_, _ = fmt.Fprintf(stderr, "%s: %s\n", file.Path, status(file))
		}
	}

	// The statuses are listed rather than ranged over so that the summary
	// reads the same way every time, whichever of them a run happened to
	// produce.
	_, _ = fmt.Fprintf(stderr, "%s: %d unchanged, %d formatted, %d unformatted, %d failed\n",
		plural(len(formatted), "file"),
		counts[statusUnchanged],
		counts[statusFormatted],
		counts[statusUnformatted],
		counts[statusFailed],
	)
}

// plural is a count and its noun, so that a summary meant for a person does
// not say "1 files".
func plural(count int, noun string) string {
	return pluralOf(count, noun, noun+"s")
}

// pluralOf is the same for a noun whose plural is not itself with an s on the
// end, which "vertex" is and every noun [plural] is called with is not.
func pluralOf(count int, one, many string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, one)
	}
	return fmt.Sprintf("%d %s", count, many)
}

// result is the object a run writes to stdout.
func result(formatted []dfcad.Formatted) fmtResult {
	out := fmtResult{
		envelope: newEnvelope("fmt"),

		// The slice is made rather than declared so that a run reaching no
		// file writes an empty list rather than a null, and a caller indexing
		// it needs no special case for the empty tree.
		Files: make([]fmtFile, 0, len(formatted)),
	}

	for _, file := range formatted {
		entry := fmtFile{Path: file.Path, Status: status(file), Diagnostics: file.Diagnostics}
		if file.Err != nil {
			entry.Error = file.Err.Error()
		}
		out.Files = append(out.Files, entry)
	}

	return out
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
