// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/z5labs/dfcad"
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

	// exitAmbiguous reports that resolution could not choose between the claims
	// it was given, and that every claim it could not choose between is in the
	// result.
	//
	// It is a code of its own rather than a check failure because an ambiguity
	// is a state of the model rather than a rule the model broke: two equally
	// good measurements of one thing genuinely do not decide between
	// themselves. A caller which is going to ask a person is the caller this
	// code exists for, and it must be able to tell that case from a model which
	// says nothing at all without matching a message.
	exitAmbiguous = 4

	// exitStrict reports the same ambiguity under a predicate the registry
	// declares strict, which is a failure rather than a finding.
	//
	// Strictness is the author's assertion that for this quantity no answer is
	// safer than an arbitrary one, and a run which reported it with the code
	// above would leave a caller free to carry on as though the disagreement
	// were routine. It is separate rather than folded into the check failure
	// for the same reason: what to do about it is to go and measure, not to fix
	// the file.
	exitStrict = 5
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
//
// 2 is the trimming
// docs/decisions/0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md
// argues for: a span is a string rather than two objects, `resolve` reports the
// answer and its accuracy rather than the whole winning claim, and `list-types`
// leaves out the registry's prose. Each of those is a field retyped or dropped
// from a default, which is what this number is for.
const outputVersion = 2

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
//
// Where stdout is a run's [answerStream], the diagnostics the run rendered on
// its stderr are written after every other field of the object — see
// [answerStream.close]. That is done here rather than by each result carrying a
// field of its own, so that no command can write an object which leaves out a
// diagnostic it rendered: there is no field for one to forget to fill in.
func emit(stdout io.Writer, result any) error {
	var encoded bytes.Buffer

	encoder := json.NewEncoder(&encoded)

	// Escaping the characters that matter in HTML would rewrite bytes of a
	// path or a message that mean nothing of the sort here, and the output is
	// read by a pipeline rather than embedded in a page.
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(result); err != nil {
		return err
	}

	out := encoded.Bytes()
	answer, joined := stdout.(*answerStream)
	if joined {
		closed, err := answer.close(out)
		if err != nil {
			return err
		}
		out = closed
	}

	_, err := stdout.Write(out)
	if joined {
		answer.written = true
	}
	return err
}

// diagnosticStream is a run's stderr, holding every diagnostic rendered on it
// so that the run's object on stdout can carry them as well.
//
// It is the stream rather than something beside it because rendering a
// diagnostic and recording it for the object are one act: [render] does both to
// the writer it is handed, so a diagnostic cannot reach a person without also
// reaching the object, and a command cannot hold one without the other.
type diagnosticStream struct {
	io.Writer

	// rendered is every diagnostic rendered on the stream, in the order it was
	// rendered in.
	rendered []dfcad.Diagnostic

	// suppressed is how many diagnostics the limit held back from the
	// rendering, summed over every rendering which held any back.
	suppressed int

	// refused reports that a load the run made refused what it read: the model
	// beneath the root, a revision `review` compared against, or the model a
	// change would produce. It is what tells [answerStream.refuse] that a run
	// which exits 2 with nothing on stdout exited for its diagnostics, rather
	// than for an error — a root held by another transaction, a file which
	// could not be written — which is not one.
	refused bool
}

// refuse notes on a run's stderr that a load it made refused what it read.
//
// It is called where that is decided — [loadGate], [begin], [apply] and
// `review`'s comparison — and nowhere else, so that a diagnostic which is an
// error is not on its own taken for a refusal: `review` renders its findings as
// errors and can still fail to write the file --annotate names, and that run
// has no answer to refuse.
func refuse(stderr io.Writer) {
	if stream, ok := stderr.(*diagnosticStream); ok {
		stream.refused = true
	}
}

// record notes the diagnostics one rendering wrote, in the order it wrote
// them.
func (s *diagnosticStream) record(rendered dfcad.Diagnostics) {
	s.rendered = append(s.rendered, rendered.All()...)
	s.suppressed += rendered.Suppressed()
}

// answerStream is a run's stdout, joined to the stream its diagnostics were
// rendered on.
type answerStream struct {
	io.Writer

	// diagnostics is the run's stderr.
	diagnostics *diagnosticStream

	// written reports that the run wrote its object.
	written bool
}

// refusedResult is the object a run writes where a load refused the model it read
// and the command has no answer to give through that: the envelope, "refused"
// true, and — written after it by [emit] — the diagnostics that refused it.
//
// It carries no subject, digest, file or anything else a command's own object
// would, because every one of those is a property of an answer, and there is
// none: a figure computed out of a model the load refused is an answer to a
// question nobody asked, and a change refused wrote nothing.
type refusedResult struct {
	envelope

	// Refused is always true. It is written rather than implied so that a
	// caller reads the same field it reads on a discovery read or `check` over
	// the same tree.
	Refused bool `json:"refused"`
}

// refuse writes the refusal of a run which exited 2 because a load refused
// what it read, where the command itself wrote nothing.
//
// It is here rather than at each place a command returns, so that no command
// which reads the model can exit 2 for its diagnostics and leave stdout empty:
// a command added later is covered the day it is added. A run which wrote its
// own object — `check`, which reports a refused model as its answer — is left
// as it is, and so is a run whose exit 2 is an error rather than a refusal.
func (a *answerStream) refuse(command string, code int) error {
	if code != exitLoad || a.written || a.diagnostics == nil || !a.diagnostics.refused {
		return nil
	}

	return emit(a, refusedResult{envelope: newEnvelope(command), Refused: true})
}

// renderedDiagnostics is the tail of an object whose run rendered a
// diagnostic, in the order the fields are written.
//
// The count comes first so that `diagnostics` is the last field of the object,
// which is where docs/machine-output.md says it is.
type renderedDiagnostics struct {
	Suppressed  int                `json:"diagnostics-suppressed,omitempty"`
	Diagnostics []dfcad.Diagnostic `json:"diagnostics"`
}

// close returns the encoded object with the run's diagnostics written after
// every other field, or exactly as it was given where the run rendered none.
//
// It splices rather than wrapping the result in a struct of its own because a
// result's fields are the command's and are embedded at the top level of the
// object; there is no Go type which puts a trailing field beside an arbitrary
// struct's fields without knowing what they are.
func (a *answerStream) close(encoded []byte) ([]byte, error) {
	stream := a.diagnostics
	if stream == nil || len(stream.rendered) == 0 && stream.suppressed == 0 {
		return encoded, nil
	}

	body := bytes.TrimRight(encoded, "\n")
	if len(body) < 2 || body[0] != '{' || body[len(body)-1] != '}' {
		return encoded, nil
	}

	var tail bytes.Buffer
	encoder := json.NewEncoder(&tail)
	encoder.SetEscapeHTML(false)

	diagnostics := stream.rendered
	if diagnostics == nil {
		diagnostics = []dfcad.Diagnostic{}
	}
	if err := encoder.Encode(renderedDiagnostics{Suppressed: stream.suppressed, Diagnostics: diagnostics}); err != nil {
		return nil, err
	}

	// The tail is an object of its own; its fields go where the result's
	// closing brace was, after a comma where the result has fields to follow.
	fields := bytes.TrimRight(tail.Bytes(), "\n")
	fields = fields[1 : len(fields)-1]

	out := make([]byte, 0, len(body)+len(fields)+2)
	out = append(out, body[:len(body)-1]...)
	if len(bytes.TrimSpace(body[1:len(body)-1])) > 0 {
		out = append(out, ',')
	}
	out = append(out, fields...)
	out = append(out, '}', '\n')

	return out, nil
}
