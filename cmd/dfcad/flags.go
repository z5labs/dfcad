// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The formats a run can report itself in.
//
// The format chooses what is written for a *person*, on stderr. It never
// chooses what is written on stdout: stdout is the machine contract on every
// run, in every format, and human-facing output is never on it. That is the
// one property of this interface that cannot be walked back, because the
// moment prose shares the stream with results, callers start tolerating it.
const (
	// formatJSON is the default. Stdout carries the result object; stderr
	// carries diagnostics and nothing else.
	formatJSON = "json"

	// formatHuman adds a rendering of the result to stderr for someone reading
	// a terminal. Stdout is byte-for-byte what it would have been without it,
	// so `dfcad ... --format human | jq` still works and the person running it
	// still sees the readable version.
	formatHuman = "human"
)

// formats is every legal --format value, in the order the usage lists them.
var formats = []string{formatJSON, formatHuman}

// UnknownFormatError is a --format that names no format.
type UnknownFormatError struct {
	// Format is what was asked for.
	Format string

	// Known is every format there is.
	Known []string
}

// Error implements [error].
func (e UnknownFormatError) Error() string {
	return fmt.Sprintf("unknown format %q: want one of %s", e.Format, strings.Join(e.Known, ", "))
}

// ErrNotADirectory is a model root that is there but is not a directory.
var ErrNotADirectory = errors.New("not a directory")

// RootError is a model root that could not be opened.
type RootError struct {
	// Path is the root, as it was given.
	Path string

	// Cause is what stopped it being opened.
	Cause error
}

// Error implements [error].
func (e RootError) Error() string {
	return fmt.Sprintf("model root %s: %v", e.Path, e.Cause)
}

// Unwrap implements the interface [errors.Is] and [errors.As] walk.
func (e RootError) Unwrap() error {
	return e.Cause
}

// InvalidVerbosityError is a --verbose that names no level.
type InvalidVerbosityError struct {
	// Value is what was given.
	Value string
}

// Error implements [error].
func (e InvalidVerbosityError) Error() string {
	return fmt.Sprintf("invalid verbosity %q: want a count of zero or more", e.Value)
}

// verbosity is how much a run says on stderr about what it is doing, as
// distinct from what it found.
//
// It is a [flag.Value] rather than a plain int so that -v can be repeated the
// way it is everywhere else, while --verbose=2 still sets a level outright.
type verbosity int

// The levels. Higher says more; nothing above the highest is defined, and a
// larger number is clamped to nothing in particular — it simply says at least
// as much as the level below it.
const (
	// verbosityQuiet reports problems and nothing else. It is the default,
	// because a run in CI has nobody watching it.
	verbosityQuiet verbosity = 0

	// verbosityProgress adds what the run is doing as it does it.
	verbosityProgress verbosity = 1
)

// String implements [flag.Value].
//
// The nil check is not defensive: the flag package builds a zero value of this
// type by reflection to find the default, and for a pointer receiver that zero
// value is a nil pointer.
func (v *verbosity) String() string {
	if v == nil {
		return "0"
	}
	return strconv.Itoa(int(*v))
}

// Set implements [flag.Value].
func (v *verbosity) Set(value string) error {
	switch value {
	case "true":
		// -v with no value, which is the spelling that repeats.
		*v++
	case "false":
		*v = verbosityQuiet
	default:
		level, err := strconv.Atoi(value)
		if err != nil || level < 0 {
			return InvalidVerbosityError{Value: value}
		}
		*v = verbosity(level)
	}
	return nil
}

// IsBoolFlag lets -v stand alone, without swallowing the argument after it.
func (v *verbosity) IsBoolFlag() bool {
	return true
}

// globals are the flags every subcommand takes, and takes identically.
//
// They are defined here once rather than per subcommand so that --root cannot
// come to mean one thing for one command and something else for another, and
// so that adding a subcommand cannot forget one of them. A command registers
// them by building its flag set with [newFlagSet].
type globals struct {
	// Root is the directory the model is read from and written to. Every
	// relative path a command is given is resolved against it.
	Root string

	// Format is how the run reports itself to a person, on stderr. It is one
	// of [formats].
	Format string

	// Verbosity is how much the run says about what it is doing.
	Verbosity verbosity
}

// register defines the global flags on a subcommand's flag set.
func (g *globals) register(flags *flag.FlagSet) {
	flags.StringVar(&g.Root, "root", ".", "")
	flags.StringVar(&g.Format, "format", formatJSON, "")
	flags.Var(&g.Verbosity, "verbose", "")
	flags.Var(&g.Verbosity, "v", "")
}

// validate checks the global flags against each other and against nothing
// else. A flag that names something that does not exist is an invocation that
// is wrong, so it is a usage error rather than a load failure.
func (g *globals) validate() error {
	for _, format := range formats {
		if g.Format == format {
			return nil
		}
	}
	return UnknownFormatError{Format: g.Format, Known: formats}
}

// open checks that the model root is a directory that is there and can be
// read.
//
// A root that is not is a load failure rather than a usage error: the
// invocation was well formed and the input it named could not be read, which
// is exactly the distinction the exit codes exist to draw.
//
// The directory is opened rather than stat'd because stat answers a different
// question. A directory can be searchable and not readable, and stat succeeds
// on it — the run would then get past this check and report an unreadable root
// as a result object on stdout, which is the one thing the check exists to
// stop.
func (g *globals) open() error {
	root, err := os.Open(g.Root)
	if err != nil {
		return RootError{Path: g.Root, Cause: err}
	}
	defer root.Close()

	info, err := root.Stat()
	if err != nil {
		return RootError{Path: g.Root, Cause: err}
	}
	if !info.IsDir() {
		return RootError{Path: g.Root, Cause: ErrNotADirectory}
	}
	return nil
}

// resolve is path as reached from the model root.
//
// An absolute path is left alone: it already says where it is, and joining it
// to the root would silently name a different file.
func (g *globals) resolve(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(g.Root, path)
}

// human reports whether the run was asked to render its result for a person.
func (g *globals) human() bool {
	return g.Format == formatHuman
}

// newFlagSet builds a subcommand's flag set with the global flags already on
// it. A subcommand adds its own flags to what comes back and parses it with
// [parse].
func newFlagSet(name string, globals *globals) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)

	// The flag package writes its own usage, which is neither the command's
	// message nor on the stream the command was handed. Silencing it leaves
	// both to [parse].
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}

	globals.register(flags)

	return flags
}

// parse parses a subcommand's arguments and settles the global flags.
//
// It returns the arguments which are not flags, in the order they were given,
// the exit code the subcommand should return, and whether it should return
// rather than carry on. Doing this once is what keeps every subcommand agreeing
// on which stream help goes to, which code a malformed flag exits with, and
// that neither writes anything to stdout.
//
// Flags and arguments may be written in any order. The flag package stops at
// the first argument which is not a flag and hands back everything after it
// unparsed, which would make `dfcad list-instances MeetingRoom --kind Space`
// silently a listing of every kind — a flag that is ignored rather than
// rejected is worse than one that does not exist. Parsing resumes after each
// argument instead, which is what a person and an agent both expect and is why
// it is done here rather than in each subcommand: an interface where the
// meaning of a flag depends on which command it was written for is one nobody
// can hold in their head.
func parse(cmd command, flags *flag.FlagSet, globals *globals, args []string, stderr io.Writer) ([]string, int, bool) {
	var positional []string

	for {
		if err := flags.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				// Help is for a person, so it goes where everything for a
				// person goes. Stdout stays empty: a run that produced no
				// result writes no result object, and never writes prose
				// instead of one.
				fmt.Fprint(stderr, cmd.usage)
				return nil, exitSuccess, true
			}

			fmt.Fprintf(stderr, "dfcad %s: %v\n\n", cmd.name, err)
			fmt.Fprint(stderr, cmd.usage)
			return nil, exitUsage, true
		}

		args = flags.Args()
		if len(args) == 0 {
			break
		}

		// Everything the flag package stopped at is an argument only as far as
		// its first element: what follows it may be a flag again.
		positional = append(positional, args[0])
		args = args[1:]
	}

	if err := globals.validate(); err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n\n", cmd.name, err)
		fmt.Fprint(stderr, cmd.usage)
		return nil, exitUsage, true
	}

	if err := globals.open(); err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return nil, exitLoad, true
	}

	return positional, exitSuccess, false
}
