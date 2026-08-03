// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"cmp"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	sexpr "github.com/z5labs/sexpr-go"
)

// Print writes file in the canonical form of the entity format.
//
// Every model has exactly one canonical printing, and this is it. The command
// line interface writes it, fmt rewrites a hand-written file into it, and a
// pull request diff over a tree of them is then about the change rather than
// about whitespace.
//
// What canonical form settles, and why none of them is a preference:
//
//   - Content is sorted. Top-level forms go in the fixed tag order of the
//     format and then by their own name or id; a form's children go in the
//     order its table lists them, with repeats, claims and assertions sorted
//     among themselves by their inline rendering. Sorting is what stops the two
//     authors of a file — a person and this package — from disagreeing, since
//     an in-memory graph does not carry the order anybody typed things in.
//   - A child equal to its default is left out. (rank normal) never prints;
//     (rank deprecated) does. Both spellings load, and only one is written
//     back, so one graph has one printing.
//   - A number prints as the shortest decimal that reads back as the same
//     value, gaining a fraction where it would otherwise read back as an
//     integer. Trailing zeros carry no meaning here: significance is a claim's
//     accuracy, written where a query can read it.
//   - A sequence whose order is data is never sorted — the ids of a boundary,
//     the components of a coordinate, the terms of a transform. They are
//     written as positional arguments, and positional arguments keep the places
//     they were written in.
//
// Comments survive. A comment attaches to the next sibling written after it and
// travels with that sibling when it sorts, so a comment above a claim stays
// above that claim wherever the claim lands. One written after the last sibling
// stays at the end of the form it was written in. This is the one place a
// format cycle moves something a person wrote, which is why it is stated rather
// than left to be discovered.
//
// Line breaking, indentation and the spelling of an atom come from the
// underlying S-expression printer unchanged, which is also why canonical output
// carries no blank lines: a blank line between two forms is not preserved, and
// there is no rule that would put one back. Output ends with exactly one line
// feed, has no trailing whitespace on any line, and is identical for the same
// model on every run, in every process.
//
// Printing a file which does not load is not an error. Anything the format does
// not recognise — an unknown tag, a datum which is not a form — is written back
// as it was given, after everything the format does recognise. Deciding whether
// a file is well formed is [Validate]'s job, and a printer which refused to
// write what it could not classify would be one nobody could run on the file
// they were fixing.
//
// A nil file writes nothing, as does a file holding no forms.
func Print(w io.Writer, file *File) error {
	if file == nil {
		return nil
	}
	return sexpr.Print(w, canonical(file))
}

// canonical is the tree [Print] writes: the file in canonical order, carrying
// the positions the underlying printer reads to interleave the comments with
// the datums they annotate.
//
// It is separate from [Print] so that a test can compare it against what
// parsing the printed bytes gives back, which is the half of the round trip
// comparing output text cannot check.
func canonical(file *File) *sexpr.File {
	groups := attach(file.Comments, file.Nodes)
	items, carried := arrange(file.Nodes, groups, topLevelForm, byTag)

	var lines counter
	out := &sexpr.File{}
	for _, item := range items {
		out.Comments = append(out.Comments, lines.comments(item.before)...)
		out.Nodes = append(out.Nodes, item.node(&lines))
	}
	out.Comments = append(out.Comments, lines.comments(slices.Concat(carried, groups[len(file.Nodes)]))...)

	return out
}

// layout is one datum on its way to being printed: the datum itself, the
// comments written before it, and, where it holds anything, what it holds in
// canonical order.
//
// It exists because ordering and positioning cannot happen in one pass. A
// child's sort key is its own canonical rendering, so a child is canonicalised
// before it is sorted; the positions the underlying printer reads to interleave
// comments with datums are only known once the sorting is done. A layout is
// what is finished enough to sort and not yet finished enough to print.
type layout struct {
	// before are the comments written before this datum inside the form which
	// holds it, in the order they were written.
	before []*Comment

	// datum is the datum itself. A list and a quote carry an empty shell of
	// one, because what they hold is below rather than inside them.
	datum sexpr.Node

	// elements are a list's elements, in canonical order.
	elements []*layout

	// tail is a list's dotted tail, where it has one.
	tail *layout

	// quoted is what a quote applies to.
	quoted *layout

	// after are the comments a list holds which no sibling follows.
	after []*Comment

	// rank is where this datum sits among its siblings before any of them are
	// compared by content. See [placeOf].
	rank int

	// inline is the datum's rendering on a single line, which is the sort key
	// of the format's ordering rule.
	inline string

	// name is the positional name or id the form was written with, which is
	// what a top-level form sorts by within its tag. Empty where the form has
	// none.
	name string
}

// arrange canonicalises the children written inside a form and puts them in
// canonical order, carrying each one's comments with it.
//
// groups holds the comments the enclosing form was written with, grouped by the
// sibling each annotates, and is indexed the way [attach] leaves it. The
// carried comments returned are the ones whose sibling turned out not to be
// printed at all, which the caller owes to whatever follows.
func arrange(children []*Node, groups [][]*Comment, f *form, order func(a, b *layout) int) (items []*layout, carried []*Comment) {
	for i, node := range children {
		tag, _ := formTag(node)
		at := placeOf(f, tag)

		item := canonicalise(node, at.form)
		item.rank = at.rank
		item.inline = item.render()
		if at.named {
			item.name = item.positional()
		}
		item.before = slices.Concat(carried, groups[i])
		carried = nil

		if at.omitted != "" && item.inline == at.omitted {
			// A child equal to its default is not printed, and whatever was
			// written above it annotates whatever comes next instead. Dropping
			// the comment along with the child would be this package deleting
			// something a person wrote.
			carried = item.before
			continue
		}

		items = append(items, item)
	}

	slices.SortStableFunc(items, order)

	return items, carried
}

// byTag is the canonical order of the top-level forms of a file: the fixed tag
// order of the format, then the form's own positional name or id, then the
// whole of its inline rendering.
//
// The last of the three is not in the format's rule and cannot change the
// answer where the rule decides one. It is there so that two forms carrying the
// same name still order the same way on every run.
func byTag(a, b *layout) int {
	if a.rank != b.rank {
		return cmp.Compare(a.rank, b.rank)
	}
	if order := strings.Compare(a.name, b.name); order != 0 {
		return order
	}
	return strings.Compare(a.inline, b.inline)
}

// byOrder is the canonical order of the children of a form: the place the
// form's table gives each child, and then, among repeats and among claims, the
// child's inline rendering compared byte-wise.
//
// Comparing UTF-8 byte-wise is code-point order, which is what makes the answer
// the same on every machine. It also falls out usefully: a form's tag is the
// first thing in its rendering, so claims group by predicate before anything
// else distinguishes them.
func byOrder(a, b *layout) int {
	if a.rank != b.rank {
		return cmp.Compare(a.rank, b.rank)
	}
	return strings.Compare(a.inline, b.inline)
}

// place is where a child written with some tag sits in the canonical child
// order of the form which holds it, and what the printer needs to know about
// it.
type place struct {
	// rank is where it sits before any two children are compared by content.
	rank int

	// form describes the child, or is nil where nothing here describes it.
	form *form

	// omitted is its canonical printing when it holds its default, which is not
	// printed at all. Empty where it has no default.
	omitted string

	// named reports whether the form's table names this tag, which is what
	// makes the child's own positional name worth sorting a top-level form by.
	// A tag the table does not name shares its rank with every other tag it
	// does not name, so the only thing which orders it is its whole rendering,
	// which begins with the tag.
	named bool
}

// placeOf is where a child written with tag sits in the canonical child order
// of f.
//
// Ranks are doubled. A form which carries claims writes them under no tag its
// table names, so they have no index of their own; doubling leaves an odd
// number between every pair of table entries, which is where the claims go —
// after every structural child and before the assertions, without sharing a
// rank with either.
func placeOf(f *form, tag string) place {
	if i, c, ok := f.childAt(tag); ok {
		return place{rank: 2 * i, form: c.form, omitted: c.omitted, named: true}
	}

	// The parameters of an assertion are named by the check registry rather
	// than by any table here, so they share a rank and sort by content alone.
	if f.free {
		return place{}
	}

	if f.claims != nil && !forms().reserved[tag] {
		return place{rank: 2*assertions(f) - 1, form: f.claims}
	}

	// A tag the format does not know here is written back after everything it
	// does. Sorting it in with the rest would interleave what somebody has to
	// fix with what they do not.
	return place{rank: 2*len(f.children) + 1}
}

// assertions is the place of the first assertion in a form's child order, which
// is where the claims that precede it end.
func assertions(f *form) int {
	for i, c := range f.children {
		if c.form == assertForm {
			return i
		}
	}
	return len(f.children)
}

// canonicalise rebuilds one written node in canonical form.
//
// f describes the form the node was written as. A nil f means nothing here
// describes it — a tag the format does not know, a datum which is not a form at
// all, the positional argument of a form — and the node is rebuilt exactly as
// written, because nothing says what any other order would be.
func canonicalise(node *Node, f *form) *layout {
	switch datum := node.Datum.(type) {
	case sexpr.Quote:
		out := &layout{datum: sexpr.Quote{Kind: datum.Kind}}
		if len(node.Children) > 0 {
			out.quoted = canonicalise(node.Children[0], nil)
		}
		return out

	case sexpr.List:
		return canonicaliseList(node, datum, f)

	default:
		return &layout{datum: node.Datum}
	}
}

// canonicaliseList rebuilds a written list: its tag and positional arguments
// where they were, its children in canonical order, and its comments attached
// to the siblings they annotate.
func canonicaliseList(node *Node, datum sexpr.List, f *form) *layout {
	out := &layout{datum: sexpr.List{}}

	written := node.Children
	var tail *Node
	if datum.Tail != nil && len(written) > 0 {
		// A dotted pair is no form of this format, so there is no table to put
		// its halves in an order, and the halves are not interchangeable.
		f = nil
		tail = written[len(written)-1]
		written = written[:len(written)-1]
	}

	// A form is its tag and then its positional arguments, and both keep the
	// places they were written in: a coordinate's components and a boundary's
	// ids are data in that order, not a listing somebody chose.
	fixed, children := written, []*Node(nil)
	if f != nil && len(written) > 0 {
		args, kids := split(written[1:])
		fixed, children = written[:1+len(args)], kids
	}

	groups := attach(node.Comments, node.Children)

	for i, element := range fixed {
		item := canonicalise(element, nil)
		item.before = groups[i]
		out.elements = append(out.elements, item)
	}

	items, carried := arrange(children, groups[len(fixed):], orDefault(f), byOrder)
	out.elements = append(out.elements, items...)

	if tail != nil {
		out.tail = canonicalise(tail, nil)
		out.tail.before = groups[len(node.Children)-1]
	}
	out.after = slices.Concat(carried, groups[len(node.Children)])

	return out
}

// orDefault is the description a list with no description of its own is
// canonicalised against: one which permits no child and reserves no tag, so
// that everything written inside it is written back as it was.
func orDefault(f *form) *form {
	if f != nil {
		return f
	}
	return unknownForm
}

// unknownForm describes a list the format knows nothing about.
var unknownForm = &form{}

// attach groups comments by the sibling each of them annotates.
//
// A comment attaches to the next sibling written after it and travels with that
// sibling when it sorts. One written after the last sibling annotates no datum,
// so it stays at the end of the form it was written in; that is the group one
// past the siblings, which the result always carries.
func attach(comments []*Comment, nodes []*Node) [][]*Comment {
	groups := make([][]*Comment, len(nodes)+1)
	for _, comment := range comments {
		i := nextSibling(comment, nodes)
		groups[i] = append(groups[i], comment)
	}
	return groups
}

// nextSibling is the index of the first node written after the comment, or one
// past the last where none was.
func nextSibling(comment *Comment, nodes []*Node) int {
	for i, node := range nodes {
		if node.Span.Start.Offset > comment.Span.Start.Offset {
			return i
		}
	}
	return len(nodes)
}

// counter hands out the positions the underlying printer reads to interleave
// comments with the datums they annotate.
//
// The positions are synthetic and are not where anything was written: sorting
// has already moved it. All the printer asks of them is which of two things
// comes first, so one line each, in the order they will be printed, says
// everything it needs.
type counter struct {
	line int
}

// next is the position of the next thing to be printed.
func (c *counter) next() sexpr.Pos {
	c.line++
	return sexpr.Pos{Line: c.line, Column: 1}
}

// comments places a run of comments, in order.
func (c *counter) comments(cs []*Comment) []*sexpr.Comment {
	out := make([]*sexpr.Comment, 0, len(cs))
	for _, comment := range cs {
		out = append(out, &sexpr.Comment{Pos: c.next(), Text: text(comment)})
	}
	return out
}

// text is a comment's source text as canonical output holds it.
//
// A comment is written back exactly as it appeared, delimiters included, and
// what comes out of it here is only what carries no meaning: a line terminator
// is whitespace, canonical output uses line feeds alone, and no line of it ends
// in whitespace. The carriage return a line comment written in a CRLF file
// carries is both of those at once, and it is part of the comment's text
// because a line comment ends at the line feed.
func text(comment *Comment) string {
	lines := strings.Split(comment.Text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	return strings.Join(lines, "\n")
}

// node gives the layout and everything inside it a position, in the order it
// will be printed, and returns the datum the printer takes.
func (l *layout) node(lines *counter) sexpr.Node {
	switch datum := l.datum.(type) {
	case sexpr.List:
		datum.Pos = lines.next()
		for _, element := range l.elements {
			datum.Comments = append(datum.Comments, lines.comments(element.before)...)
			datum.Elements = append(datum.Elements, element.node(lines))
		}
		if l.tail != nil {
			datum.Comments = append(datum.Comments, lines.comments(l.tail.before)...)
			datum.Tail = l.tail.node(lines)
		}
		datum.Comments = append(datum.Comments, lines.comments(l.after)...)
		return datum

	case sexpr.Quote:
		datum.Pos = lines.next()
		if l.quoted != nil {
			datum.Datum = l.quoted.node(lines)
		}
		return datum

	case sexpr.Symbol:
		datum.Pos = lines.next()
		return datum
	case sexpr.String:
		datum.Pos = lines.next()
		return datum
	case sexpr.Int:
		datum.Pos = lines.next()
		return datum
	case sexpr.Float:
		datum.Pos = lines.next()
		return datum
	case sexpr.Bool:
		datum.Pos = lines.next()
		return datum
	case sexpr.Nil:
		datum.Pos = lines.next()
		return datum
	}

	return l.datum
}

// positional is the name or id a form was written with, which is what a
// top-level form sorts by within its tag.
//
// It is the first element after the tag when that element is not itself a form:
// a form there is a child, and a form with a child where its name should be has
// no name to sort by.
func (l *layout) positional() string {
	if _, ok := l.datum.(sexpr.List); !ok || len(l.elements) < 2 {
		return ""
	}
	if _, ok := l.elements[1].datum.(sexpr.List); ok {
		return ""
	}
	return l.elements[1].render()
}

// render is the layout's inline rendering: the datum on a single line, with one
// space between elements and the column limit ignored.
//
// This is the sort key the format's ordering rule is written in terms of, which
// is why it is spelled here rather than borrowed from the underlying printer.
// The printer's business is where to break a line; the rendering's is to exist
// for every datum, which it does because an atom is never broken and the limit
// is not consulted. TestInlineRenderingIsWhatThePrinterWrites keeps the two in
// step.
func (l *layout) render() string {
	var out strings.Builder
	l.write(&out)
	return out.String()
}

// write appends the layout's inline rendering.
func (l *layout) write(out *strings.Builder) {
	switch datum := l.datum.(type) {
	case sexpr.List:
		out.WriteByte('(')
		for i, element := range l.elements {
			if i > 0 {
				out.WriteByte(' ')
			}
			element.write(out)
		}
		if l.tail != nil {
			if len(l.elements) > 0 {
				out.WriteByte(' ')
			}
			out.WriteString(". ")
			l.tail.write(out)
		}
		out.WriteByte(')')

	case sexpr.Quote:
		macro := quoteMacros[datum.Kind]
		out.WriteString(macro)
		if l.quoted == nil {
			return
		}
		if datum.Kind == sexpr.QuoteKindUnquote && startsWithAt(l.quoted.datum) {
			// Keep the two shorthands apart, or they read back as the one
			// which splices.
			out.WriteByte(' ')
		}
		l.quoted.write(out)

	default:
		out.WriteString(atom(l.datum))
	}
}

// quoteMacros is the shorthand each quote kind is written with.
var quoteMacros = map[sexpr.QuoteKind]string{
	sexpr.QuoteKindQuote:           "'",
	sexpr.QuoteKindQuasiquote:      "`",
	sexpr.QuoteKindUnquote:         ",",
	sexpr.QuoteKindUnquoteSplicing: ",@",
}

// startsWithAt reports whether a datum is written starting with an at sign,
// which only a symbol can be.
func startsWithAt(n sexpr.Node) bool {
	symbol, ok := n.(sexpr.Symbol)
	return ok && strings.HasPrefix(symbol.Value, "@")
}

// atom is how a datum which holds nothing is written.
func atom(n sexpr.Node) string {
	switch datum := n.(type) {
	case sexpr.Symbol:
		return datum.Value
	case sexpr.String:
		return quoteText(datum.Value)
	case sexpr.Int:
		return strconv.FormatInt(datum.Value, 10)
	case sexpr.Float:
		return decimal(datum.Value)
	case sexpr.Bool:
		if datum.Value {
			return "#t"
		}
		return "#f"
	case sexpr.Nil:
		return "nil"
	}
	return ""
}

// decimal is how a real is written: the shortest decimal which reads back as
// the same value, with a fraction added where the shortest has neither a
// fraction nor an exponent, so that it reads back as a real rather than as an
// integer.
//
// 8.50 is written 8.5, 100 as a real is written 100.0, and
// 0.30480000000000002 is written as itself because nothing shorter reads back
// the same. Trailing zeros carry no meaning: what a measurement is good to is
// its accuracy, written down.
func decimal(v float64) string {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		// A non-finite value has no printing and cannot occur in a model. The
		// underlying printer reports it; all that is wanted here is a key which
		// is the same on every run.
		return fmt.Sprint(v)
	}

	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// quoteText is how a string is written: double-quoted, escaping exactly what
// has to be escaped for the same text to read back.
//
// Nothing else is escaped. A non-ASCII letter is written as itself, as UTF-8,
// because the format is a text format and a file full of \u escapes is one
// nobody can read. Because a line feed is escaped, canonical output never holds
// a raw line break inside a string, even though the grammar accepts one.
func quoteText(s string) string {
	var out strings.Builder
	out.Grow(len(s) + len(`""`))

	out.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		default:
			if unicode.IsControl(r) || r == utf8.RuneError {
				fmt.Fprintf(&out, `\u%04X`, r)
				continue
			}
			out.WriteRune(r)
		}
	}
	out.WriteByte('"')

	return out.String()
}
