// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Severity says how much a diagnostic matters.
//
// It is a string so that its machine-readable form is the same word its human
// rendering prints, and so that neither has to be kept in step with a number.
type Severity string

const (
	// SeverityError marks input the engine refuses. Nothing downstream of an
	// error may be trusted to mean what it says.
	SeverityError Severity = "error"

	// SeverityWarning marks input the engine accepts but which is very likely
	// not what its author meant.
	SeverityWarning Severity = "warning"
)

// Diagnostic is one problem found in user-authored input.
//
// A diagnostic is not an [error]. An error is for the caller of a function; a
// diagnostic is for whoever wrote the file, human or otherwise, and it exists
// to be actionable without anybody interpreting it. That is why every
// diagnostic carries where it is rather than only what went wrong, and why a
// pass over a file collects them rather than returning the first one.
//
// Both renderings come from these fields directly: [Diagnostic.Render] writes
// the human one, and encoding/json writes the machine one. Neither is produced
// by parsing the other.
type Diagnostic struct {
	// Severity says how much this diagnostic matters.
	Severity Severity `json:"severity"`

	// Span is the source text the diagnostic is about. A diagnostic about a
	// point rather than a range — something missing, something at the end of
	// the file — carries the empty span there, which [Position.Span] builds.
	Span Span `json:"span"`

	// Message says what was expected and what was found, in that order.
	// "invalid entity" is not a message anybody can act on; "expected a unit
	// after the value, found `)`" is.
	Message string `json:"message"`

	// Hint is optional advice on what to do about it. It says something the
	// message does not — where the vocabulary is defined, what the usual cause
	// is — rather than restating the message in other words.
	Hint string `json:"hint,omitempty"`

	// Related are the other places that explain this one: the earlier
	// definition an id collides with, the frame a unit disagrees with. Each is
	// rendered beneath the diagnostic with its own quoted source.
	Related []RelatedLocation `json:"related,omitempty"`
}

// RelatedLocation is a second place a diagnostic points at, with its own
// message saying why it is being pointed at.
//
// "already defined" is only actionable when it can show both definitions, and
// a diagnostic that could name just one position could not say that at all.
type RelatedLocation struct {
	// Span is the source text this location is about.
	Span Span `json:"span"`

	// Message says why this location is related — "first defined here", not a
	// repeat of the diagnostic's own message.
	Message string `json:"message"`
}

// String renders the diagnostic as the single line an editor and a terminal
// already know how to jump to: path:line:column, the severity and the message.
//
// It quotes no source and shows no hint. [Diagnostic.Render] is the full human
// rendering.
func (d Diagnostic) String() string {
	return fmt.Sprintf("%s: %s: %s", d.Span.Start, d.Severity, d.Message)
}

// Render writes the human rendering of the diagnostic to w: the header line,
// the source lines the span covers with the offending text underlined, the
// hint, and each related location rendered the same way.
//
// src supplies the bytes of the files being pointed at. Where it has none for
// a path — because it is nil, or because the file has since changed — the
// header lines are still written and the quoting is left out, so a diagnostic
// is never lost for want of the source it came from.
func (d Diagnostic) Render(w io.Writer, src SourceMap) error {
	var b strings.Builder
	d.render(&b, src)

	_, err := io.WriteString(w, b.String())
	return err
}

// render writes the diagnostic into b.
func (d Diagnostic) render(b *strings.Builder, src SourceMap) {
	width := quote(b, d.Span, string(d.Severity), d.Message, src)

	if d.Hint != "" {
		fmt.Fprintf(b, "%*s = hint: %s\n", width, "", d.Hint)
	}

	for _, related := range d.Related {
		quote(b, related.Span, "note", related.Message, src)
	}
}

// SourceMap resolves a file path to the bytes read from it, so a diagnostic
// can quote the line it points at.
//
// It is an interface rather than a map so that a caller holding a whole tree
// can re-read a file on demand instead of keeping every file it loaded in
// memory for the sake of the few that turn out to have problems.
type SourceMap interface {
	// Source returns the bytes of path, and whether it has them.
	Source(path string) ([]byte, bool)
}

// Sources is the [SourceMap] a caller already holding the bytes uses.
type Sources map[string][]byte

// Source implements [SourceMap].
func (s Sources) Source(path string) ([]byte, bool) {
	src, ok := s[path]
	return src, ok
}

// DefaultDiagnosticLimit is how many diagnostics a [Diagnostics] retains when
// its Limit is left unset.
//
// A cap exists because a single misplaced parenthesis can make every later
// form in a file wrong, and a thousand diagnostics describing one mistake is
// worse than ten: whatever reads them, a person scrolling or an agent with a
// context window, is pushed past the one that matters.
const DefaultDiagnosticLimit = 100

// Diagnostics collects the problems found in one pass over user-authored
// input.
//
// One pass reports every independent problem it finds. Stopping at the first
// turns fixing a file into a guessing loop: fix, rerun, discover the next one,
// repeat, with no way to see how much is left.
//
// The zero value is ready to use and caps at [DefaultDiagnosticLimit]. It must
// be collected into through a pointer; the reading methods take a value and
// mutate nothing.
type Diagnostics struct {
	// Limit is how many diagnostics to retain. Zero means
	// [DefaultDiagnosticLimit] and a negative Limit retains everything. Set it
	// before adding anything: lowering it later suppresses nothing already
	// retained.
	Limit int

	reported   []Diagnostic
	suppressed int
}

// Add records diagnostics, retaining them up to the limit and counting the
// rest as suppressed.
func (d *Diagnostics) Add(diags ...Diagnostic) {
	limit := d.limit()

	for _, diagnostic := range diags {
		if limit >= 0 && len(d.reported) >= limit {
			d.suppressed++
			continue
		}
		d.reported = append(d.reported, diagnostic)
	}
}

// limit is the effective cap, or -1 when everything is retained.
func (d Diagnostics) limit() int {
	switch {
	case d.Limit == 0:
		return DefaultDiagnosticLimit
	case d.Limit < 0:
		return -1
	default:
		return d.Limit
	}
}

// All returns the retained diagnostics in reporting order: by file, then by
// position within it, then by severity and message so that two diagnostics at
// one position still order the same way on every run.
//
// Ordering is deterministic because the output of two runs over the same input
// is meant to be diffable; an order that depended on which check happened to
// run first would make every diff noise.
func (d Diagnostics) All() []Diagnostic {
	out := slices.Clone(d.reported)

	slices.SortStableFunc(out, func(a, b Diagnostic) int {
		return cmp.Or(
			comparePositions(a.Span.Start, b.Span.Start),
			comparePositions(a.Span.End, b.Span.End),
			cmp.Compare(a.Severity, b.Severity),
			cmp.Compare(a.Message, b.Message),
		)
	})

	return out
}

// comparePositions orders two positions by file and then by place in it.
func comparePositions(a, b Position) int {
	return cmp.Or(
		cmp.Compare(a.Path, b.Path),
		cmp.Compare(a.Line, b.Line),
		cmp.Compare(a.Column, b.Column),
		cmp.Compare(a.Offset, b.Offset),
	)
}

// Len is how many diagnostics were retained, which is not how many were added
// once the limit has suppressed any.
func (d Diagnostics) Len() int { return len(d.reported) }

// Suppressed is how many diagnostics were dropped by the limit.
func (d Diagnostics) Suppressed() int { return d.suppressed }

// HasErrors reports whether any retained diagnostic is an error. It is what
// decides whether input is refused, and it is separate from Len because a set
// holding only warnings is not a failure.
func (d Diagnostics) HasErrors() bool {
	return slices.ContainsFunc(d.reported, func(diagnostic Diagnostic) bool {
		return diagnostic.Severity == SeverityError
	})
}

// Render writes every retained diagnostic to w in reporting order, followed by
// a line saying how many were suppressed when any were.
//
// The count is printed because a truncated list that does not say it was
// truncated reads as a complete one, and fixing everything it names then
// leaves the file still failing for reasons nothing ever mentioned.
func (d Diagnostics) Render(w io.Writer, src SourceMap) error {
	var b strings.Builder

	for _, diagnostic := range d.All() {
		diagnostic.render(&b, src)
	}

	if d.suppressed > 0 {
		noun := "diagnostics"
		if d.suppressed == 1 {
			noun = "diagnostic"
		}
		fmt.Fprintf(&b, "%d more %s suppressed by the limit of %d\n", d.suppressed, noun, d.limit())
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// maxQuotedLines is how many source lines one span quotes before the middle of
// it is elided. A span covering a whole form can cover a page, and a page of
// underlined source buries the diagnostic it was meant to illustrate.
const maxQuotedLines = 6

// headQuotedLines is how many lines are quoted before an elision.
const headQuotedLines = 2

// quote writes one header line — path:line:column, a label and a message —
// followed by the source lines the span covers, each underlined beneath the
// text the span holds.
//
// It returns the width of the line number gutter it used so that anything
// written beneath it lines up with it.
func quote(b *strings.Builder, span Span, label, message string, src SourceMap) int {
	fmt.Fprintf(b, "%s: %s: %s\n", span.Start, label, message)

	lines, ok := sourceLines(src, span.Start.Path)
	if !ok {
		return 0
	}

	first, last := quotedRange(span, len(lines))
	if first > last {
		return 0
	}

	width := len(strconv.Itoa(last))
	for _, n := range quotedLines(first, last) {
		if n == 0 {
			fmt.Fprintf(b, "%*s ... %d lines omitted\n", width, "", last-first+1-(headQuotedLines+1))
			continue
		}

		line := lines[n-1]
		start, end := underlined(span, n, line)

		// An empty line is written without the space that would separate it
		// from the gutter, so that no quoted line ends in trailing whitespace
		// an editor or a diff would then argue about.
		if line == "" {
			fmt.Fprintf(b, "%*d |\n", width, n)
		} else {
			fmt.Fprintf(b, "%*d | %s\n", width, n, line)
		}
		fmt.Fprintf(b, "%*s | %s\n", width, "", underline(line, start, end))
	}

	return width
}

// quotedRange is the range of line numbers a span covers, clamped to the lines
// the file has.
//
// A span ending at the first column of a line ends with the line break before
// it and covers nothing of that line, so the line is not quoted; without that
// correction every span covering a whole line would quote the next one with an
// empty underline beneath it.
func quotedRange(span Span, count int) (first, last int) {
	first = max(span.Start.Line, 1)

	last = span.End.Line
	if last > first && span.End.Column <= 1 {
		last--
	}
	last = min(max(last, first), count)

	return first, last
}

// quotedLines is the sequence of line numbers to quote for a range, where a
// zero stands for the elided middle of a range too long to quote whole.
func quotedLines(first, last int) []int {
	if last-first+1 <= maxQuotedLines {
		out := make([]int, 0, last-first+1)
		for n := first; n <= last; n++ {
			out = append(out, n)
		}
		return out
	}

	out := make([]int, 0, headQuotedLines+2)
	for n := first; n < first+headQuotedLines; n++ {
		out = append(out, n)
	}
	return append(out, 0, last)
}

// underlined is the half-open range of bytes of line n that span covers.
//
// A line in the middle of a span is covered whole. The first line is covered
// from where the span starts, the last to where it ends, and a span within one
// line is both at once.
func underlined(span Span, n int, line string) (start, end int) {
	start, end = 0, len(line)

	if n == span.Start.Line {
		start = min(max(span.Start.Column-1, 0), len(line))
	}
	if n == span.End.Line {
		end = min(max(span.End.Column-1, start), len(line))
	}

	return start, end
}

// underline is the run of carets marking line[start:end], preceded by the
// padding that puts it under that text.
//
// The padding is one character per rune rather than per byte, so a line with
// multi-byte text in front of the span underlines the span rather than
// something several columns to the left of it. A tab is repeated as a tab, so
// that the alignment survives whatever width the reader's terminal gives it.
// An empty range still gets one caret: a diagnostic about something missing
// has nothing to underline and still has to point somewhere.
func underline(line string, start, end int) string {
	var b strings.Builder

	for _, r := range line[:start] {
		if r == '\t' {
			b.WriteByte('\t')
			continue
		}
		b.WriteByte(' ')
	}

	carets := utf8.RuneCountInString(line[start:end])
	b.WriteString(strings.Repeat("^", max(carets, 1)))

	return b.String()
}

// sourceLines splits the source of path into lines, without their terminators.
//
// A file ending in a line break has a final empty line, which is where a
// diagnostic at the end of the file points, so the split is not trimmed.
func sourceLines(src SourceMap, path string) ([]string, bool) {
	if src == nil {
		return nil, false
	}

	b, ok := src.Source(path)
	if !ok {
		return nil, false
	}

	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}

	return lines, true
}
