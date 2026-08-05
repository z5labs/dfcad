// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ObservationExtension is the file extension a directory walk treats as an
// observation file, per observation specification section 2.
//
// It is compared byte-wise, like every other comparison in this system, so a
// file named Session.OBS is not an observation file. A path named explicitly is
// read whatever its extension; the extension decides only what a walk picks up.
const ObservationExtension = ".obs"

// The form tags of the observation format, per observation specification
// section 3. There are two and there is no third: a line whose tag is neither
// is refused rather than skipped.
const (
	// ObservationTag introduces one observation.
	ObservationTag = "obs"

	// RetirementTag introduces a record retiring an earlier one.
	RetirementTag = "retire"
)

// observationTags is the closed set, in the order a diagnostic lists them.
var observationTags = []string{ObservationTag, RetirementTag}

// ObservationTags returns the form tags an observation file may write, in the
// order a diagnostic names them.
func ObservationTags() []string { return slices.Clone(observationTags) }

// observationFields is how many fields an `obs` record carries after its tag,
// per observation specification section 5.
const observationFields = 12

// retirementFields is how many fields a `retire` record carries after its tag,
// per observation specification section 6.
const retirementFields = 4

// Observation is one measurement somebody took, as one line of an observation
// file, per observation specification section 5.
//
// It is not a claim. A claim is what the model asserts about a thing; an
// observation is a shot, and several of them may sit behind one claim. Nothing
// here names an entity: what cites a record is a claim's provenance, in the
// entity format, which is the one place the model says what it believes and
// why.
//
// Every length on the record — the three coordinate components, both precisions
// and the antenna height — is in the linear unit of Frame, which is the one unit
// that frame has ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)). No
// unit token is written on the line and nothing here converts one.
type Observation struct {
	// ID is this record's identity. It is what a claim's provenance points at
	// and what a retirement names, so it is minted once and never reused —
	// including for a record which was retired.
	ID ID

	// At is the instant the observation was taken, by the instrument's clock.
	At time.Time

	// AtWritten is the timestamp exactly as it was written, which carries which
	// offset the author was working in. The instant alone does not: an offset
	// is lost the moment a time is normalised, and it is evidence about where
	// somebody was standing.
	AtWritten string

	// Frame is the frame the coordinate is expressed in, which a registry file
	// declares.
	Frame ID

	// Coordinate is the position, component by component, in the frame's axis
	// order and in the frame's linear unit.
	Coordinate Point

	// Method is how the value was obtained — the same vocabulary a claim's
	// method names. Which methods exist is registry data
	// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
	Method ID

	// Fix is the fix quality the instrument reported at the moment of the shot:
	// what solution it had, not how good anybody thought it was. Registry data
	// for the reason Method is.
	Fix ID

	// HorizontalPrecision is the standard uncertainty of this shot in the plane
	// of the first two components — one standard deviation, k = 1
	// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)) — in the frame's
	// unit.
	HorizontalPrecision float64

	// VerticalPrecision is the standard uncertainty along the third component,
	// on the same convention and in the same unit. It is separate because it is
	// routinely two to three times the horizontal figure, and one number for
	// both would have to be the worse of them.
	VerticalPrecision float64

	// AntennaHeight is the antenna or instrument height the shot was reduced
	// by: the vertical offset from the mark to the phase centre or to the
	// prism, in the frame's unit. The coordinate is of the mark, with this
	// height already reduced out; the figure is recorded so that the reduction
	// can be checked, and undone, by somebody who was not there.
	AntennaHeight float64

	// Session is the occupation this record belongs to — one setup, one base
	// station, one operator's afternoon. It is what makes a systematic error
	// attributable: a base station on the wrong mark ruins a session, and the
	// session is how its records are found.
	Session ID

	// Span is the line the record was written on.
	Span Span

	// fields is where each field the registry is asked about was written, so
	// that a diagnostic about a frame nobody declared underlines the frame
	// rather than the hundred and thirty characters around it.
	fields observationFieldSpans

	// order is where this record sits in the whole log, across every file of
	// it. A retirement is a *later* record, and later is a question about the
	// log rather than about one file of it.
	order int
}

// observationFieldSpans is where the fields a later pass reports on were
// written. They are not exported because they are the diagnostics' business:
// what a caller wants from a record is its values, and Span is the line they
// were all read from.
type observationFieldSpans struct {
	id      Span
	frame   Span
	method  Span
	fix     Span
	session Span
}

// Line is the line of its file the record was written on, which is the only
// place it can have been: a record is entirely on one line.
func (o *Observation) Line() int { return o.Span.Start.Line }

// ObservationRetirement is a later record retiring an earlier one, per
// observation specification section 6.
//
// It removes nothing. The retired record stays exactly where it was written,
// byte for byte, and both are read: what the log resolves to is every
// observation no retirement names. Editing the original in place would destroy
// the thing which made it evidence — after the edit there is no way to tell,
// from the file, that the number ever said anything else.
type ObservationRetirement struct {
	// ID is this record's own identity, minted like any other. A retirement is
	// a record in the log, not an annotation on one.
	ID ID

	// At is when the decision to retire was taken, which is not when the
	// retired shot was measured.
	At time.Time

	// AtWritten is the timestamp exactly as it was written.
	AtWritten string

	// Supersedes is the identity of the record being retired, which appears
	// earlier in the log.
	Supersedes ID

	// Reason is why, in the words of whoever decided. It is never empty: a
	// retirement with no reason is not evidence of anything, and the next
	// person to read the file cannot tell a mistake from a change of mind.
	Reason string

	// Span is the line the record was written on.
	Span Span

	// fields is where the two ids a later pass reports on were written.
	fields retirementFieldSpans

	// order is where this record sits in the whole log.
	order int
}

// retirementFieldSpans is where a retirement's own identity and the identity it
// names were written.
type retirementFieldSpans struct {
	id         Span
	supersedes Span
}

// Line is the line of its file the record was written on.
func (r *ObservationRetirement) Line() int { return r.Span.Start.Line }

// observationRecord is the little every record of the log has in common: an
// identity, a place it was written and a place in the order.
//
// It is unexported because it is the log's own bookkeeping. A caller ranges
// over observations or over retirements, which are different things and are
// asked for separately.
type observationRecord interface {
	identity() ID
	identitySpan() Span
	span() Span
	position() int
}

func (o *Observation) identity() ID       { return o.ID }
func (o *Observation) identitySpan() Span { return o.fields.id }
func (o *Observation) span() Span         { return o.Span }
func (o *Observation) position() int      { return o.order }

func (r *ObservationRetirement) identity() ID       { return r.ID }
func (r *ObservationRetirement) identitySpan() Span { return r.fields.id }
func (r *ObservationRetirement) span() Span         { return r.Span }
func (r *ObservationRetirement) position() int      { return r.order }

// ObservationLog is the records of one or more observation files, in the order
// they were written.
//
// Order is the log's own: within a file it is the order of the lines, and
// across files it is the lexical order of their paths, which is the order a
// walk reads them in. That is what makes "earlier in the log" a question with
// one answer however many files the log is spread over.
//
// A log holds every record it read, including ones the validator refuses. The
// two questions — what does this file say, and is what it says sound — are
// answered separately so that a diagnostic about the second can still quote the
// first.
type ObservationLog struct {
	// records is every record, in log order, which is what the ordering
	// questions are answered against.
	records []observationRecord

	// observations and retirements are the same records, split by form, so that
	// a caller ranging over one is not filtering the other out.
	observations []*Observation
	retirements  []*ObservationRetirement

	// byID is the *first* record written under each identity. A second is a
	// duplicate, which the validator reports naming both; keeping the first is
	// what lets it name the other end.
	byID map[ID]observationRecord

	// retiredBy is the first retirement naming each identity. A second is a
	// double retirement, reported the same way.
	retiredBy map[ID]*ObservationRetirement
}

// newObservationLog returns an empty log ready to be read into.
func newObservationLog() *ObservationLog {
	return &ObservationLog{
		byID:      make(map[ID]observationRecord),
		retiredBy: make(map[ID]*ObservationRetirement),
	}
}

// add appends one record, in log order.
func (l *ObservationLog) add(record observationRecord) {
	l.records = append(l.records, record)

	if _, seen := l.byID[record.identity()]; !seen {
		l.byID[record.identity()] = record
	}

	switch record := record.(type) {
	case *Observation:
		record.order = len(l.records) - 1
		l.observations = append(l.observations, record)
	case *ObservationRetirement:
		record.order = len(l.records) - 1
		l.retirements = append(l.retirements, record)
		if _, retired := l.retiredBy[record.Supersedes]; !retired {
			l.retiredBy[record.Supersedes] = record
		}
	}
}

// Len is how many records the log holds, of both forms.
func (l *ObservationLog) Len() int {
	if l == nil {
		return 0
	}
	return len(l.records)
}

// Observations ranges over every observation, in log order.
func (l *ObservationLog) Observations() iter.Seq[*Observation] {
	return func(yield func(*Observation) bool) {
		if l == nil {
			return
		}
		for _, observation := range l.observations {
			if !yield(observation) {
				return
			}
		}
	}
}

// Retirements ranges over every retirement, in log order.
func (l *ObservationLog) Retirements() iter.Seq[*ObservationRetirement] {
	return func(yield func(*ObservationRetirement) bool) {
		if l == nil {
			return
		}
		for _, retirement := range l.retirements {
			if !yield(retirement) {
				return
			}
		}
	}
}

// Observation returns the observation written under id, and whether there is
// one. A second record written under the same identity is a duplicate the
// validator refuses; what this returns is the first.
func (l *ObservationLog) Observation(id ID) (*Observation, bool) {
	if l == nil {
		return nil, false
	}
	observation, ok := l.byID[id].(*Observation)
	return observation, ok
}

// RetirementOf returns the retirement naming id, and whether there is one.
func (l *ObservationLog) RetirementOf(id ID) (*ObservationRetirement, bool) {
	if l == nil {
		return nil, false
	}
	retirement, ok := l.retiredBy[id]
	return retirement, ok
}

// Retired reports whether any retirement names id.
func (l *ObservationLog) Retired(id ID) bool {
	_, retired := l.RetirementOf(id)
	return retired
}

// Current ranges over every observation no retirement names, in log order.
//
// This is what the log resolves to, and it is a view rather than a rewrite: the
// retired records are still in the file, still readable, and still say exactly
// what the instrument said they said.
func (l *ObservationLog) Current() iter.Seq[*Observation] {
	return func(yield func(*Observation) bool) {
		for observation := range l.Observations() {
			if l.Retired(observation.ID) {
				continue
			}
			if !yield(observation) {
				return
			}
		}
	}
}

// Session ranges over the observations of one occupation, in log order.
func (l *ObservationLog) Session(id ID) iter.Seq[*Observation] {
	return func(yield func(*Observation) bool) {
		for observation := range l.Observations() {
			if observation.Session != id {
				continue
			}
			if !yield(observation) {
				return
			}
		}
	}
}

// Sessions returns every session the log names, in the order it first named
// them.
func (l *ObservationLog) Sessions() []ID {
	var (
		sessions []ID
		seen     = make(map[ID]bool)
	)

	for observation := range l.Observations() {
		if seen[observation.Session] {
			continue
		}
		seen[observation.Session] = true
		sessions = append(sessions, observation.Session)
	}

	return sessions
}

// TimestampProblem is the rule of observation specification section 4.2 a
// timestamp broke.
//
// It is a string so that the machine-readable form is the same word the human
// rendering prints, which is the convention [Severity] follows for the same
// reason.
type TimestampProblem string

const (
	// TimestampNotRFC3339 is text which is not an RFC 3339 date-time at all.
	TimestampNotRFC3339 TimestampProblem = "not-rfc3339"

	// TimestampNoOffset is a date-time written with no offset. It is a local
	// time in a zone the file does not name, so it denotes a different instant
	// depending on where it is read, and two records written this way cannot be
	// ordered.
	TimestampNoOffset TimestampProblem = "no-offset"

	// TimestampUnknownOffset is a date-time written with the offset -00:00,
	// which RFC 3339 section 4.3 defines as *the offset is unknown*. A record
	// whose instant is unknown is not evidence of when anything was measured.
	TimestampUnknownOffset TimestampProblem = "unknown-offset"
)

// rule is the requirement the timestamp broke, phrased for whoever wrote it.
func (p TimestampProblem) rule() string {
	switch p {
	case TimestampNoOffset:
		return "a timestamp states its offset from UTC, as Z or as ±HH:MM; without one it is a local time in a zone nothing names"
	case TimestampUnknownOffset:
		return "-00:00 is RFC 3339 for an offset nobody knows, which is not something a record of when a measurement was taken may say"
	}
	return "a timestamp is written as RFC 3339 date-time: 2026-05-06T09:14:22Z, or with an offset such as +02:00"
}

// MalformedTimestampError reports text written where an observation timestamp
// belongs which is not one.
//
// There is one spelling of a timestamp in this format and it is the engine's
// rather than the caller's, so the parse and the refusal are here: a command
// taking one on its command line and a file carrying one are held to the same
// spelling, and neither has a second copy of it to drift from.
type MalformedTimestampError struct {
	// Written is the text as it appeared.
	Written string

	// Reason is the rule of observation specification section 4.2 it broke.
	Reason TimestampProblem
}

// Error implements the [error] interface.
func (e MalformedTimestampError) Error() string {
	return fmt.Sprintf("expected a timestamp, found %s: %s", e.Written, e.Reason.rule())
}

// localTimestampLayout is an RFC 3339 date-time with its offset taken off,
// which is what tells a timestamp somebody forgot the offset of from text which
// is not a timestamp at all. The two are different mistakes and the second is
// not a hint about the first.
const localTimestampLayout = "2006-01-02T15:04:05.999999999"

// unknownOffset is RFC 3339's spelling of an offset nobody knows.
const unknownOffset = "-00:00"

// ParseObservationTime reads an observation timestamp, per observation
// specification section 4.2.
//
// It is RFC 3339 date-time with a numeric offset. Fractional seconds are
// permitted and carry whatever resolution was written. Z is +00:00 and is
// unambiguous; -00:00 is not, and neither is a date-time with no offset at all.
//
// A zone abbreviation — CEST, EST — is not RFC 3339 and comes back as a
// malformed timestamp rather than as an offset: those abbreviations are not
// unique across the world, and several of them denote two different offsets in
// one year.
func ParseObservationTime(written string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339, written)
	if err == nil {
		if strings.HasSuffix(written, unknownOffset) {
			return time.Time{}, MalformedTimestampError{Written: written, Reason: TimestampUnknownOffset}
		}
		return at, nil
	}

	if _, local := time.Parse(localTimestampLayout, written); local == nil {
		return time.Time{}, MalformedTimestampError{Written: written, Reason: TimestampNoOffset}
	}

	return time.Time{}, MalformedTimestampError{Written: written, Reason: TimestampNotRFC3339}
}

// malformedTimestamp is the diagnostic for text written where a timestamp
// belongs which is not one.
func malformedTimestamp(span Span, err MalformedTimestampError) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message:  fmt.Sprintf("expected a timestamp, found %s", err.Written),
		Hint:     err.Reason.rule(),
	}
}

// asMalformedTimestamp recovers the failure [ParseObservationTime] reports,
// which is the only one it reports.
func asMalformedTimestamp(err error) (MalformedTimestampError, bool) {
	var malformed MalformedTimestampError
	ok := errors.As(err, &malformed)
	return malformed, ok
}

// numberKind is what a lexeme written where a number belongs turned out to be,
// under the number lexis of specification section 4.3.
type numberKind int

const (
	// notANumber is a lexeme which is not a number at all. `1.`, `--1` and
	// `12abc` are each one.
	notANumber numberKind = iota

	// integerNumber is a number written with neither a fraction nor an
	// exponent, which reads back as a count.
	integerNumber

	// realNumber is a number written with a fraction or an exponent, which
	// reads back as a real.
	realNumber
)

// classifyNumber reads a lexeme under the number lexis of specification section
// 4.3: an optional sign, ASCII digits, an optional fraction and an optional
// decimal exponent, and nothing else.
//
// It is written out rather than delegated to [strconv.ParseFloat] because that
// function accepts several spellings this format does not have — `1.`, `Inf`,
// `NaN`, a hexadecimal float and an underscore separator — and each of them
// would be a number the entity format refuses and an observation file accepted.
func classifyNumber(written string) numberKind {
	i := 0

	if i < len(written) && (written[i] == '+' || written[i] == '-') {
		i++
	}

	whole := digits(written, &i)
	if whole == 0 {
		return notANumber
	}

	real := false

	if i < len(written) && written[i] == '.' {
		i++
		if digits(written, &i) == 0 {
			return notANumber
		}
		real = true
	}

	if i < len(written) && (written[i] == 'e' || written[i] == 'E') {
		i++
		if i < len(written) && (written[i] == '+' || written[i] == '-') {
			i++
		}
		if digits(written, &i) == 0 {
			return notANumber
		}
		real = true
	}

	if i != len(written) {
		return notANumber
	}

	if real {
		return realNumber
	}
	return integerNumber
}

// digits advances i over a run of ASCII digits and returns how many there were.
func digits(written string, i *int) int {
	start := *i
	for *i < len(written) && written[*i] >= '0' && written[*i] <= '9' {
		*i++
	}
	return *i - start
}

// ParseObservations reads one observation file, per observation specification
// sections 2 to 6.
//
// What it checks is the file: its encoding, its line terminators, and the lexis
// and the field count of every record on every line. What it does not check is
// anything needing more than one line or a registry — a duplicate identity, a
// retirement which resolves, a frame somebody declared — which is
// [ValidateObservations] and needs the whole log.
//
// One pass reports every independent problem it finds and carries on past each
// one, so a file with eleven malformed lines is eleven diagnostics on one run
// rather than eleven runs. A line which does not parse contributes no record
// and stops nothing: the lines after it are read.
//
// The log comes back holding whatever did parse, which is what lets a caller
// report on a file and still say what the sound part of it holds.
func ParseObservations(path string, r io.Reader) (*ObservationLog, []Diagnostic) {
	src, err := io.ReadAll(r)
	if err != nil {
		return newObservationLog(), []Diagnostic{diagnose(path, err)}
	}

	log := newObservationLog()
	diags := parseObservationFile(path, src, log)

	return log, diags
}

// observationParser reads one file into a log which may already hold others.
type observationParser struct {
	// lines is where a byte offset is, which every diagnostic here is built
	// from.
	lines lineIndex

	// src is the file, kept so that a field can be quoted by offset.
	src []byte

	// log is what parses into. It is shared across the files of one walk.
	log *ObservationLog

	// diags are the problems found so far, in the order they were found.
	diags []Diagnostic
}

// parseObservationFile reads src into log and returns what was wrong with it.
func parseObservationFile(path string, src []byte, log *ObservationLog) []Diagnostic {
	lines := newLineIndex(path, src)

	if offset := firstInvalidUTF8(src); offset >= 0 {
		return []Diagnostic{diagnose(path, EncodingError{Position: lines.at(offset), Byte: src[offset]})}
	}
	if bytes.HasPrefix(src, byteOrderMark) {
		return []Diagnostic{diagnose(path, ByteOrderMarkError{Position: lines.at(0)})}
	}

	p := &observationParser{lines: lines, src: src, log: log}

	if len(src) > 0 && src[len(src)-1] != '\n' {
		p.at(lines.at(len(src)), Diagnostic{
			Severity: SeverityError,
			Message:  "expected a line feed after the last record, found the end of the file",
			Hint: "a record whose line is not terminated is rewritten by the next append, " +
				"which is the one change an observation file may not carry",
		})
	}

	for offset := 0; offset < len(src); {
		end := bytes.IndexByte(src[offset:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += offset
		}

		p.line(offset, end)
		offset = end + 1
	}

	return p.diags
}

// at appends a diagnostic at a position, which is what something with no text
// of its own points at.
func (p *observationParser) at(position Position, diagnostic Diagnostic) {
	diagnostic.Span = position.Span()
	p.diags = append(p.diags, diagnostic)
}

// add appends a diagnostic which already carries its span.
func (p *observationParser) add(diagnostic Diagnostic) {
	p.diags = append(p.diags, diagnostic)
}

// spanOf is the span of src[start:end].
func (p *observationParser) spanOf(start, end int) Span {
	return Span{Start: p.lines.at(start), End: p.lines.at(end)}
}

// line reads one line of the file, which is src[start:end] with its terminator
// already taken off.
func (p *observationParser) line(start, end int) {
	// A carriage return before the line feed is not whitespace here. A file
	// written with them appends differently on the next machine, and a
	// comparison of two revisions would report every line of it.
	if end > start && p.src[end-1] == '\r' {
		p.at(p.lines.at(end-1), Diagnostic{
			Severity: SeverityError,
			Message:  "expected a line feed, found a carriage return before it",
			Hint:     "observation files are terminated with a single line feed, so that an append is the same bytes on every machine",
		})
		end--
	}

	fields, ok := p.fields(start, end)
	if !ok || len(fields) == 0 {
		return
	}

	tag := fields[0]
	switch tag.text {
	case ObservationTag:
		p.observation(tag, fields[1:])
	case RetirementTag:
		p.retirement(tag, fields[1:])
	default:
		p.add(Diagnostic{
			Severity: SeverityError,
			Span:     tag.span,
			Message:  fmt.Sprintf("expected a record form, found %s, which is not one", tag.text),
			Hint:     fmt.Sprintf("the record forms are %s", join(observationTags, "and")),
		})
	}
}

// observationLexeme is one field of a record, as written and where.
type observationLexeme struct {
	// text is the field's value: the bytes as written, with a quoted string's
	// quotes taken off and its escapes resolved.
	text string

	// quoted says whether it was written as a quoted string. It is carried
	// because exactly one field of one form takes one, and a format with two
	// spellings of the same id is a format two authors spell differently.
	quoted bool

	// span is where it was written, which is what a diagnostic about it points
	// at.
	span Span
}

// fields splits a line into its fields, or reports why it could not.
//
// A blank line and a comment line yield nothing and are not a failure: both are
// ignored by observation specification section 3, and a file whose first line
// names its columns is the convention.
func (p *observationParser) fields(start, end int) ([]observationLexeme, bool) {
	var fields []observationLexeme

	i := start
	for i < end && (p.src[i] == ' ' || p.src[i] == '\t') {
		i++
	}

	if i == end || p.src[i] == '#' {
		return nil, true
	}

	for i < end {
		if p.src[i] == ' ' || p.src[i] == '\t' {
			i++
			continue
		}

		if p.src[i] == '"' {
			field, next, ok := p.quoted(i, end)
			if !ok {
				return nil, false
			}
			fields = append(fields, field)
			i = next
			continue
		}

		from := i
		for i < end && p.src[i] != ' ' && p.src[i] != '\t' {
			i++
		}
		fields = append(fields, observationLexeme{text: string(p.src[from:i]), span: p.spanOf(from, i)})
	}

	return fields, true
}

// quoted reads a double-quoted string beginning at start, per observation
// specification section 4.4.
func (p *observationParser) quoted(start, end int) (observationLexeme, int, bool) {
	var text strings.Builder

	for i := start + 1; i < end; i++ {
		switch p.src[i] {
		case '"':
			field := observationLexeme{text: text.String(), quoted: true, span: p.spanOf(start, i+1)}
			if i+1 < end && p.src[i+1] != ' ' && p.src[i+1] != '\t' {
				p.at(p.lines.at(i+1), Diagnostic{
					Severity: SeverityError,
					Message:  "expected whitespace after a quoted string, found more text",
					Hint:     "fields are separated by spaces or tabs",
				})
				return field, i + 1, false
			}
			return field, i + 1, true
		case '\\':
			if i+1 >= end {
				break
			}
			i++
			switch p.src[i] {
			case '"', '\\':
				text.WriteByte(p.src[i])
			default:
				p.at(p.lines.at(i), Diagnostic{
					Severity: SeverityError,
					Message:  fmt.Sprintf("expected an escape, found %s, which is not one", strconv.QuoteRune(rune(p.src[i]))),
					Hint:     `the escapes of a quoted string are \" and \\, and there is no third`,
				})
				return observationLexeme{}, end, false
			}
		default:
			text.WriteByte(p.src[i])
		}
	}

	p.at(p.lines.at(end), Diagnostic{
		Severity: SeverityError,
		Message:  "expected a closing quote, found the end of the line",
		Hint:     "a quoted string is opened and closed on one line; a record is entirely on its own line",
	})

	return observationLexeme{}, end, false
}

// arity reports whether a record carries the fields its form takes, and says
// how many that is when it does not.
func (p *observationParser) arity(tag observationLexeme, fields []observationLexeme, want int) bool {
	if len(fields) == want {
		return true
	}

	span := tag.span
	if len(fields) > 0 {
		span = Span{Start: tag.span.Start, End: fields[len(fields)-1].span.End}
	}

	p.add(Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message: fmt.Sprintf("expected the %d fields %s takes, found %d",
			want, tag.text, len(fields)),
		Hint: observationShape(tag.text),
	})

	return false
}

// observationShape is how a record of this form is written, which is what
// somebody who wrote a different number of fields needs to read.
func observationShape(tag string) string {
	switch tag {
	case ObservationTag:
		return "an observation is written obs <id> <at> <frame> <x> <y> <z> <method> <fix> <h-precision> <v-precision> <antenna> <session>"
	case RetirementTag:
		return `a retirement is written retire <id> <at> <supersedes> "<reason>"`
	}
	return ""
}

// observation reads an `obs` record.
//
// Every field is read even where an earlier one failed, so that a line with
// three mistakes reports three of them. What a failed field costs is the
// record: a record which cannot be read is not filed, because a half-read
// observation is worse than none — it would resolve, and it would be wrong.
func (p *observationParser) observation(tag observationLexeme, fields []observationLexeme) {
	if !p.arity(tag, fields, observationFields) {
		return
	}

	observation := &Observation{
		Span:      Span{Start: tag.span.Start, End: fields[len(fields)-1].span.End},
		AtWritten: fields[1].text,
		fields: observationFieldSpans{
			id:      fields[0].span,
			frame:   fields[2].span,
			method:  fields[6].span,
			fix:     fields[7].span,
			session: fields[11].span,
		},
	}

	read := p.id(fields[0], "the record's identity", &observation.ID)
	read = p.timestamp(fields[1], &observation.At) && read
	read = p.id(fields[2], "the frame", &observation.Frame) && read

	for axis := range observation.Coordinate {
		read = p.real(fields[3+axis], "a coordinate component", &observation.Coordinate[axis]) && read
	}

	read = p.id(fields[6], "the method", &observation.Method) && read
	read = p.id(fields[7], "the fix quality", &observation.Fix) && read
	read = p.magnitude(fields[8], "the horizontal precision", &observation.HorizontalPrecision) && read
	read = p.magnitude(fields[9], "the vertical precision", &observation.VerticalPrecision) && read
	read = p.magnitude(fields[10], "the antenna height", &observation.AntennaHeight) && read
	read = p.id(fields[11], "the session", &observation.Session) && read

	if !read {
		return
	}

	p.log.add(observation)
}

// retirement reads a `retire` record.
func (p *observationParser) retirement(tag observationLexeme, fields []observationLexeme) {
	if !p.arity(tag, fields, retirementFields) {
		return
	}

	retirement := &ObservationRetirement{
		Span:      Span{Start: tag.span.Start, End: fields[len(fields)-1].span.End},
		AtWritten: fields[1].text,
		Reason:    fields[3].text,
		fields:    retirementFieldSpans{id: fields[0].span, supersedes: fields[2].span},
	}

	read := p.id(fields[0], "the record's identity", &retirement.ID)
	read = p.timestamp(fields[1], &retirement.At) && read
	read = p.id(fields[2], "the record being retired", &retirement.Supersedes) && read

	switch {
	case !fields[3].quoted:
		p.add(Diagnostic{
			Severity: SeverityError,
			Span:     fields[3].span,
			Message:  fmt.Sprintf("expected a quoted reason, found %s", fields[3].text),
			Hint:     `a reason is a sentence, so it is written in double quotes: "the base was on the wrong mark"`,
		})
		read = false
	case retirement.Reason == "":
		p.add(Diagnostic{
			Severity: SeverityError,
			Span:     fields[3].span,
			Message:  "expected a reason, found an empty one",
			Hint: "a retirement with no reason is not evidence of anything: " +
				"the next person to read the file cannot tell a mistake from a change of mind",
		})
		read = false
	}

	if !read {
		return
	}

	p.log.add(retirement)
}

// Each field reader writes into the record and reports whether *its own* field
// was readable. The caller conjoins them all without short-circuiting, which is
// what makes a line with three mistakes report three of them: every field is
// read whatever the ones before it did, and what a failure costs is the record
// rather than the rest of the line.

// bare reports whether a field was written without quotes, which every field
// but a retirement's reason is.
//
// One spelling per field is what keeps two authors from writing the same id two
// ways in a format nothing re-prints. Quoting exists for the one field which
// holds a sentence, and nowhere else.
func (p *observationParser) bare(field observationLexeme, what string) bool {
	if !field.quoted {
		return true
	}

	p.add(Diagnostic{
		Severity: SeverityError,
		Span:     field.span,
		Message:  fmt.Sprintf("expected %s, found a quoted string", what),
		Hint:     "quoting is for a retirement's reason, which is the one field holding a sentence",
	})

	return false
}

// id reads an id field, per specification section 4.1.
func (p *observationParser) id(field observationLexeme, what string, into *ID) bool {
	if !p.bare(field, what) {
		return false
	}

	id, err := ParseID(field.text)
	if err != nil {
		malformed, _ := asMalformedID(err)
		diagnostic := malformedID(field.span, malformed)
		diagnostic.Message = fmt.Sprintf("expected %s as an id, found %s", what, malformed.Written)
		p.add(diagnostic)
		return false
	}

	*into = id
	return true
}

// timestamp reads a timestamp field, per observation specification section 4.2.
func (p *observationParser) timestamp(field observationLexeme, into *time.Time) bool {
	if !p.bare(field, "a timestamp") {
		return false
	}

	at, err := ParseObservationTime(field.text)
	if err != nil {
		malformed, _ := asMalformedTimestamp(err)
		p.add(malformedTimestamp(field.span, malformed))
		return false
	}

	*into = at
	return true
}

// real reads a real number field, per observation specification section 4.3.
func (p *observationParser) real(field observationLexeme, what string, into *float64) bool {
	if !p.bare(field, what) {
		return false
	}

	switch classifyNumber(field.text) {
	case realNumber:
		value, err := strconv.ParseFloat(field.text, 64)
		if err != nil {
			// The lexis accepted it, so this is a magnitude no float64 holds
			// rather than a spelling the format refuses.
			p.add(Diagnostic{
				Severity: SeverityError,
				Span:     field.span,
				Message:  fmt.Sprintf("expected %s, found %s, which is outside the range of a real number", what, field.text),
			})
			return false
		}

		*into = value
		return true
	case integerNumber:
		p.add(Diagnostic{
			Severity: SeverityError,
			Span:     field.span,
			Message:  fmt.Sprintf("expected %s, found the count %s", what, field.text),
			Hint:     "a real number is written with a fraction or an exponent, so that it reads back as a real",
		})
	default:
		p.add(Diagnostic{
			Severity: SeverityError,
			Span:     field.span,
			Message:  fmt.Sprintf("expected %s, found %s, which is not a number", what, field.text),
			Hint:     "a number is an optional sign, digits, an optional fraction and an optional exponent",
		})
	}

	return false
}

// magnitude reads a real number field which may not be negative: a precision
// and an antenna height are both lengths, and a negative one is a sign error
// somebody would otherwise carry into a budget.
func (p *observationParser) magnitude(field observationLexeme, what string, into *float64) bool {
	if !p.real(field, what, into) {
		return false
	}

	if *into < 0 {
		p.add(Diagnostic{
			Severity: SeverityError,
			Span:     field.span,
			Message:  fmt.Sprintf("expected %s to be zero or more, found %s", what, field.text),
			Hint:     "a precision is a standard uncertainty and an antenna height is a length above the mark; neither is signed",
		})
		*into = 0
		return false
	}

	return true
}

// WalkObservations yields the observation files beneath root, in the lexical
// order of their paths.
//
// root may name a single file, which is read whatever its extension, or a
// directory, beneath which every file whose extension is [ObservationExtension]
// is yielded and everything else is ignored. A directory holding no observation
// file yields nothing and is not an error.
//
// It is separate from [Walk] because the two answer different questions about
// one tree: entity files and observation files sit in the same repository and
// neither walk should pick the other up.
func WalkObservations(root string) iter.Seq2[string, error] {
	return walkExtension(root, ObservationExtension)
}

// LoadObservations reads the observation files beneath root and validates them
// against the registry, per observation specification section 7.
//
// It is the whole of the format's own checking: [ParseObservations] answers what
// each file says and [ValidateObservations] answers whether the log those files
// make is sound. What it cannot answer on its own is
// [section 8](docs/observation-file.md#8-append-only) — whether a file was
// appended to rather than edited — which is a question about two revisions and
// is [ValidateAppendOnly].
//
// Files are read in the lexical order of their paths, which is what makes
// "earlier in the log" a question with one answer across a directory of them.
// A file which cannot be read is a diagnostic and the walk carries on, so one
// unreadable file does not hide what is wrong with the rest.
func LoadObservations(root string, registry *Registry) (*ObservationLog, []Diagnostic) {
	var (
		log   = newObservationLog()
		diags []Diagnostic
	)

	for path, err := range WalkObservations(root) {
		if err != nil {
			diags = append(diags, diagnose(path, err))
			continue
		}

		src, err := os.ReadFile(path)
		if err != nil {
			diags = append(diags, diagnose(path, err))
			continue
		}

		diags = append(diags, parseObservationFile(path, src, log)...)
	}

	return log, append(diags, ValidateObservations(log, registry)...)
}

// ValidateObservations checks a log against itself and against the registry,
// per observation specification section 7.2.
//
// These are the questions no single line answers: whether two records share an
// identity, whether a retirement names a record which is there and which is
// earlier, and whether the frame and the namespaces a record names are ones a
// registry file declares. Each is asked of the whole log because a record in
// the first file a walk reaches may be retired by one in the last, and a pass
// which asked as it read would report it missing for no reason but the order
// the directory happened to be listed in.
//
// Diagnostics come back in the order the pass found them. Collecting them into
// a [Diagnostics] is what puts them in reporting order.
func ValidateObservations(log *ObservationLog, registry *Registry) []Diagnostic {
	if log == nil {
		return nil
	}

	var diags []Diagnostic

	for _, record := range log.records {
		if first := log.byID[record.identity()]; first != record {
			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Span:     record.identitySpan(),
				Message:  fmt.Sprintf("expected an unused record identity, found %s, which is already written", record.identity()),
				Hint:     "a record identity is minted once and never reused, including for a record which was retired",
				Related: []RelatedLocation{{
					Span:    first.identitySpan(),
					Message: fmt.Sprintf("%s is written here", first.identity()),
				}},
			})
		}

		diags = append(diags, undeclaredNamespace(registry, record.identity(), record.identitySpan())...)
	}

	for _, observation := range log.observations {
		if !registry.Declares(SortFrame, string(observation.Frame)) {
			diags = append(diags, registry.Undeclared(SortFrame, string(observation.Frame), observation.fields.frame))
		}

		for _, named := range []struct {
			id   ID
			span Span
		}{
			{observation.Frame, observation.fields.frame},
			{observation.Method, observation.fields.method},
			{observation.Fix, observation.fields.fix},
			{observation.Session, observation.fields.session},
		} {
			diags = append(diags, undeclaredNamespace(registry, named.id, named.span)...)
		}
	}

	for _, retirement := range log.retirements {
		diags = append(diags, validateRetirement(log, retirement)...)
	}

	return diags
}

// undeclaredNamespace is the diagnostic for an id whose namespace no registry
// file declares, and nothing when it declares one.
//
// The shape of an id is a property of the id and is checked as the line is
// read; which namespaces exist is a property of the model and is checked here
// ([0003](docs/decisions/0003-id-namespaces-are-a-closed-registry.md)).
func undeclaredNamespace(registry *Registry, id ID, span Span) []Diagnostic {
	namespace := id.Namespace()
	if namespace == "" || registry.Declares(SortNamespace, namespace) {
		return nil
	}
	return []Diagnostic{registry.Undeclared(SortNamespace, namespace, span)}
}

// validateRetirement checks one retirement against the log it is written in.
func validateRetirement(log *ObservationLog, retirement *ObservationRetirement) []Diagnostic {
	if retirement.Supersedes == retirement.ID {
		return []Diagnostic{{
			Severity: SeverityError,
			Span:     retirement.fields.supersedes,
			Message:  fmt.Sprintf("expected the record being retired, found %s, which is this record", retirement.ID),
			Hint:     "a retirement is a later record naming an earlier one, and no record is earlier than itself",
		}}
	}

	retired, held := log.byID[retirement.Supersedes]
	if !held {
		return []Diagnostic{{
			Severity: SeverityError,
			Span:     retirement.fields.supersedes,
			Message:  fmt.Sprintf("expected the record being retired, found %s, which this log does not hold", retirement.Supersedes),
			Hint:     "a retirement names a record written earlier in the log; nothing outside the log can be retired",
		}}
	}

	if retired.position() > retirement.position() {
		return []Diagnostic{{
			Severity: SeverityError,
			Span:     retirement.fields.supersedes,
			Message:  fmt.Sprintf("expected the record being retired to be written earlier, found %s, which is written later", retirement.Supersedes),
			Hint:     "a retirement is a decision taken about a record which already existed, so it is always the later of the two",
			Related: []RelatedLocation{{
				Span:    retired.identitySpan(),
				Message: fmt.Sprintf("%s is written here", retired.identity()),
			}},
		}}
	}

	var diags []Diagnostic

	if first := log.retiredBy[retirement.Supersedes]; first != retirement {
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Span:     retirement.fields.supersedes,
			Message:  fmt.Sprintf("expected a record which is not retired, found %s, which is already retired", retirement.Supersedes),
			Hint:     "a record is retired once; a second retirement is two decisions about one shot with nothing saying which stands",
			Related: []RelatedLocation{{
				Span:    first.fields.supersedes,
				Message: fmt.Sprintf("%s is retired here", retirement.Supersedes),
			}},
		})
	}

	if observation, ok := retired.(*Observation); ok && retirement.At.Before(observation.At) {
		diags = append(diags, Diagnostic{
			Severity: SeverityWarning,
			Span:     retirement.Span,
			Message: fmt.Sprintf("expected a retirement no earlier than the record it retires, found %s, which is earlier than %s",
				retirement.AtWritten, observation.AtWritten),
			Hint: "instrument clocks disagree, so this is worth a look rather than a refusal",
			Related: []RelatedLocation{{
				Span:    observation.fields.id,
				Message: fmt.Sprintf("%s was taken here", observation.ID),
			}},
		})
	}

	return diags
}

// ObservationSource is one revision of one observation file: the path it is
// known by, and its bytes.
//
// It is a pair rather than a path because the revisions being compared are
// rarely both on disk — one of them is a commit, an archive entry or what was
// backed up — and because the path is what a diagnostic names rather than where
// the bytes came from.
type ObservationSource struct {
	// Path is the file's path, as a diagnostic should name it.
	Path string

	// Bytes is the revision's content. Nil is a revision in which the file is
	// absent, which is every one of its lines removed.
	Bytes []byte
}

// ValidateAppendOnly checks the append-only invariant between two revisions of
// one observation file, per observation specification section 8.
//
// The invariant is one sentence: every line the older revision had is present,
// unchanged, at the same line number in the newer one. Lines the newer revision
// adds beyond the end of the older one are not a finding — an append is the only
// legal change.
//
// Comparison is byte for byte over whole lines, including their terminators and
// including comment and blank lines. Re-indenting a record, re-spelling a number
// which means the same and rewriting every timestamp into another offset are all
// the same finding, because none of them is distinguishable from a quiet
// correction of the data, and the format exists so that they are refused rather
// than judged.
//
// It knows nothing about git. Where the two byte sequences came from — two
// commits, a working tree against its merge base, a backup against what is on
// disk — is the caller's question.
func ValidateAppendOnly(base, head ObservationSource) []Diagnostic {
	var (
		baseLines  = splitLines(base.Bytes)
		headLines  = splitLines(head.Bytes)
		baseIndex  = newLineIndex(base.Path, base.Bytes)
		headIndex  = newLineIndex(head.Path, head.Bytes)
		diagnostic []Diagnostic
	)

	for line := range min(len(baseLines), len(headLines)) {
		if bytes.Equal(baseLines[line].text, headLines[line].text) {
			continue
		}

		diagnostic = append(diagnostic, Diagnostic{
			Severity: SeverityError,
			Span:     lineSpan(headIndex, headLines[line]),
			Message:  fmt.Sprintf("expected line %d to be what the earlier revision wrote, found it modified", line+1),
			Hint: "an observation file is appended to and never edited: a bad record is retired by a later record naming it, " +
				"which leaves the original readable",
			Related: []RelatedLocation{{
				Span:    lineSpan(baseIndex, baseLines[line]),
				Message: fmt.Sprintf("the earlier revision of line %d", line+1),
			}},
		})
	}

	if removed := len(baseLines) - len(headLines); removed > 0 {
		diagnostic = append(diagnostic, Diagnostic{
			Severity: SeverityError,
			Span:     headIndex.at(len(head.Bytes)).Span(),
			Message: fmt.Sprintf("expected the %s the earlier revision wrote, found %d of them removed",
				plural(len(baseLines), "line"), removed),
			Hint: "a record is retired by a later record naming it, never by deleting it: " +
				"a deleted record is evidence which no longer exists",
			Related: []RelatedLocation{{
				Span:    lineSpan(baseIndex, baseLines[len(headLines)]),
				Message: fmt.Sprintf("the first line the later revision does not have, at line %d", len(headLines)+1),
			}},
		})
	}

	return diagnostic
}

// physicalLine is one line of a file: its bytes including any terminator, and
// where in the file they are.
type physicalLine struct {
	// text is the line including its terminator, which is what is compared: a
	// line which lost its line feed is a line which changed.
	text []byte

	// start is the offset of its first byte.
	start int

	// end is the offset one past its last byte, not counting the terminator,
	// which is what a diagnostic underlines.
	end int
}

// splitLines splits src into its lines, each carrying its terminator.
//
// A file ending in a line feed has no trailing empty line: the feed terminates
// the line before it rather than starting one after it.
func splitLines(src []byte) []physicalLine {
	var lines []physicalLine

	for offset := 0; offset < len(src); {
		end := bytes.IndexByte(src[offset:], '\n')
		if end < 0 {
			lines = append(lines, physicalLine{text: src[offset:], start: offset, end: len(src)})
			break
		}
		end += offset
		lines = append(lines, physicalLine{text: src[offset : end+1], start: offset, end: end})
		offset = end + 1
	}

	return lines
}

// lineSpan is the span a line covers, terminator excluded so that the underline
// under it stops where the text does.
func lineSpan(index lineIndex, line physicalLine) Span {
	return Span{Start: index.at(line.start), End: index.at(line.end)}
}
