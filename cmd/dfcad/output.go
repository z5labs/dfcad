// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"encoding/json"
	"io"
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

// outputVersion is the version of the object every command writes to stdout.
//
// It is one number across the whole command line interface rather than one per
// subcommand, because the thing being versioned is the contract — the envelope
// below, the streams either side of it and the exit codes — and a caller reads
// that contract once for every command it drives.
//
// The rule the number carries: a field may be added at any time, and a caller
// that reads a documented field keeps working. A field is never removed,
// renamed, reordered into a different meaning, or given a different type
// without this number changing. Growth is cheap; breakage is loud.
const outputVersion = 1

// envelope is the head of the object every command writes to stdout, and the
// only part of that object whose shape does not depend on which command ran.
//
// It is embedded rather than nested so that a payload's own fields sit beside
// these two — a caller reads .version and .command without knowing anything
// about the command it invoked, and reads the rest once it does.
type envelope struct {
	// Version is the version of the output contract this object was written
	// against. It is part of the payload rather than something a caller has to
	// infer from the binary it invoked.
	Version int `json:"version"`

	// Command names what produced the object, so that a caller reading a
	// collected result knows which payload it is reading.
	Command string `json:"command"`
}

// newEnvelope is the head of a result written by the named command.
func newEnvelope(command string) envelope {
	return envelope{Version: outputVersion, Command: command}
}

// emit writes one result object to stdout, and is the only thing in this
// command that ever writes to stdout at all.
//
// Every result is a struct rather than a map so that its keys come out in a
// fixed order, which is half of what makes two runs over the same input
// byte-identical.
func emit(stdout io.Writer, result any) error {
	encoder := json.NewEncoder(stdout)

	// Escaping the characters that matter in HTML would rewrite bytes of a
	// path or a message that mean nothing of the sort here, and the output is
	// read by a pipeline rather than embedded in a page.
	encoder.SetEscapeHTML(false)

	return encoder.Encode(result)
}
