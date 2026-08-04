// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"strconv"
	"strings"

	sexpr "github.com/z5labs/sexpr-go"
)

// topLevelContext is how a diagnostic names the outermost context of a file.
const topLevelContext = "the top level of a file"

// Validate checks a loaded file against the forms of the entity format,
// returning one diagnostic for every problem it finds.
//
// This is the schema layer over the generic tree: the part that knows `(node
// ...)` is a form and `(ndoe ...)` is not, that a form has required and optional
// children, and which of those children may legitimately repeat. Repeating a
// claim is the normal case — two width claims on one node are two measurements
// — so repeated and duplicate are different questions, and only the second is
// an error.
//
// One pass reports everything it can find. Validation of a form continues into
// its children after a problem with the form itself, and a problem with one
// top-level form never hides the next, because fixing a file one diagnostic at
// a time is a guessing loop.
//
// What it does not check is anything the registry decides. Whether a tag is a
// declared predicate, whether that predicate bears claims, which shape its
// value takes, whether an id resolves and whether a check name exists are all
// questions about registry data rather than about the shape of a form, and they
// are answered once the registry has been loaded. Structural validation
// therefore accepts both `(colour "slate")` and the claim form of the same
// predicate, and leaves the choice between them to the layer which knows.
//
// It follows that a form which carries claims has no unknown child tags: a tag
// it does not reserve is a claim on it, however it is spelled, and only the
// predicate registry can say otherwise. A misspelled `kind` on a node is still
// reported — as the required child it leaves missing.
//
// Diagnostics come back in the order the pass found them. Collecting them into
// a [Diagnostics] is what puts them in reporting order, which is by position
// and is the same for every run over the same input.
func Validate(file *File) []Diagnostic {
	if file == nil {
		return nil
	}

	v := &validator{}
	v.children(scope{
		form:     topLevelForm,
		expected: "a top-level form",
		where:    topLevelContext,
	}, file.Nodes)

	return v.diags
}

// validateForm checks one written form against the description of it, which is
// what a pass interpreting that form does before reading it.
//
// [Validate] is the same thing over a whole file, from the top level down. This
// is the entry a loader which has already found the form it interprets takes, so
// that a pass reporting on a claim does not also report on the node the claim
// was written on — a second copy of every diagnostic the pass which owns that
// node already produced, from a second walk over the same tree.
func validateForm(node *Node, f *form, tag string) []Diagnostic {
	v := &validator{}
	v.check(node, f, tag)
	return v.diags
}

// validator collects the diagnostics of one pass.
type validator struct {
	diags []Diagnostic
}

// add records one diagnostic.
func (v *validator) add(span Span, message string, hint string, related ...RelatedLocation) {
	v.diags = append(v.diags, Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message:  message,
		Hint:     hint,
		Related:  related,
	})
}

// scope is the context a written form sits in: the description of the enclosing
// form, and the two phrasings a diagnostic about its contents needs.
type scope struct {
	// form describes what may be written here.
	form *form

	// expected reads after "expected" in a diagnostic about a child written
	// here — "a child of the node form", "a top-level form".
	expected string

	// where names the context itself — "the node form" — and reads after "of"
	// in a diagnostic about how many of a child there are.
	where string

	// span is the enclosing form, which is what a diagnostic about something
	// missing from it points at. The top level of a file has none.
	span Span
}

// check validates one written form against the description of it, and then
// everything written inside it.
func (v *validator) check(node *Node, f *form, tag string) {
	elements := node.Children
	if len(elements) > 0 {
		// The first child of a list is the tag itself, which is how the form
		// was found in the first place.
		elements = elements[1:]
	}

	v.improper(node)

	written, children := split(elements)
	v.args(node, f, tag, written)

	for _, argument := range written {
		v.excluded(argument)
	}

	if len(elements) < f.minElements {
		v.add(node.Span, fmt.Sprintf("expected at least one element after the %s tag, found none", tag), "")
	}

	v.children(scope{
		form:     f,
		expected: "a child of " + f.name(tag),
		where:    f.name(tag),
		span:     node.Span,
	}, children)
}

// args checks how many positional arguments were written after the tag.
//
// A form is written as its arguments and then its children, so the arguments
// are the elements before the first child form. Where there are too many, the
// diagnostic points at the first one that is too many rather than at the whole
// form: the form is not the mistake, the extra argument is.
func (v *validator) args(node *Node, f *form, tag string, written []*Node) {
	if f.args.permits(len(written)) {
		return
	}

	span := node.Span
	if f.args.max >= 0 && len(written) > f.args.max {
		span = written[f.args.max].Span
	}

	v.add(span, fmt.Sprintf("expected %s after the %s tag, found %s", f.argsDesc, tag, count(len(written))), "")
}

// children checks everything written inside a form, and then how many of each
// there turned out to be.
func (v *validator) children(s scope, elements []*Node) {
	seen := make(map[string][]*Node)

	for _, element := range elements {
		tag, ok := formTag(element)
		if !ok {
			v.add(element.Span, fmt.Sprintf("expected %s, found %s", s.expected, describe(element)), "")
			continue
		}

		switch c, permitted := s.form.child(tag); {
		case permitted:
			seen[tag] = append(seen[tag], element)
			v.check(element, c.form, tag)

		case s.form.free:
			// An assertion's parameters belong to the check registry.

		case s.form.claims != nil && !forms().reserved[tag]:
			v.claim(element, tag)

		// A tag the format knows, written where it does not belong. Inside a
		// form which carries claims that means a reserved tag, since a tag it
		// does not reserve was a claim above; anywhere else it means a tag some
		// other form permits.
		case s.form.claims != nil, forms().permittedIn[tag] != nil:
			v.misplaced(s, element, tag)

		default:
			v.unknown(s, element, tag)
		}
	}

	v.count(s, seen)
}

// claim checks a child written under a tag its enclosing form does not reserve,
// which is a claim on it.
//
// Whether the predicate bears claims is registry data, and the two spellings it
// chooses between are distinguishable without it: a claim is written as child
// forms, and the plain value of a non-claim-bearing predicate is written as
// arguments and nothing else. Only the second is left alone, and only until the
// registry says which of the two the predicate takes.
func (v *validator) claim(node *Node, tag string) {
	elements := node.Children[1:]
	if len(elements) == 0 {
		v.add(node.Span, fmt.Sprintf("expected a value or the children of a claim after the %s tag, found none", tag), "")
		return
	}

	if _, children := split(elements); len(children) == 0 {
		return
	}

	v.check(node, claimForm, tag)
}

// improper reports a list written as a dotted pair.
//
// A dotted pair is a legal S-expression and is no form of this format: every
// list in a dfcad file is proper. It is reported against the whole list rather
// than against the tail because the list is the construct which is wrong — the
// tail is a datum which would be unremarkable one space to the left.
//
// The tail is otherwise left where it was written, and whatever else is wrong
// with the form is still reported. Reading the pair as though the dot were not
// there would be this package guessing, and dropping the tail would turn one
// mistake into a form which is also missing whatever the tail held.
func (v *validator) improper(node *Node) {
	if list, ok := node.Datum.(sexpr.List); !ok || list.Tail == nil {
		return
	}

	v.add(node.Span, "expected a proper list, found a dotted pair", "every list in a dfcad file is proper: write the tail as a further element")
}

// excluded reports a datum written as a positional argument which is a legal
// S-expression and is no part of this format.
//
// The same four constructs written where a child belongs are reported by
// [validator.children], which names what it found there instead of a form. An
// argument is not a child and nothing above looks at one: `(label nil)` writes
// exactly the one argument the label form permits, and that argument is the
// placeholder specification section 2 excludes.
//
// It recurses because an argument may itself be a list. The components of a
// coordinate are written as one, and a dotted pair or a placeholder inside it is
// as excluded there as anywhere else.
func (v *validator) excluded(node *Node) {
	switch datum := node.Datum.(type) {
	case sexpr.Nil:
		v.add(node.Span, "expected a value, found nil", "absence is expressed by omitting a child, never by writing a placeholder")

	case sexpr.Quote:
		v.add(node.Span, "expected a value, found a quoted datum", "the quote shorthands have no meaning in a dfcad file")

	case sexpr.List:
		if len(datum.Elements) == 0 {
			v.add(node.Span, "expected a value, found an empty list", "every form has a tag")
			return
		}

		v.improper(node)
		for _, child := range node.Children {
			v.excluded(child)
		}
	}
}

// misplaced reports a form of the format written where it does not belong.
//
// It is reported apart from an unknown tag because the two are different
// mistakes with different fixes. An unknown tag is usually a misspelling; a
// known one in the wrong place is usually a child on the wrong node, and saying
// where it does belong is the whole of the answer.
func (v *validator) misplaced(s scope, node *Node, tag string) {
	var hint string
	if where := forms().permittedIn[tag]; len(where) > 0 {
		hint = fmt.Sprintf("(%s ...) belongs in %s", tag, join(where, "or"))
	}

	v.add(node.Span, fmt.Sprintf("expected %s, found (%s ...), which is not permitted here", s.expected, tag), hint)
}

// unknown reports a tag the format does not know, with the nearest tag it does
// know when one is close enough to be a misspelling of it.
//
// The suggestion is drawn from the tags permitted here rather than from every
// tag in the format, so that taking it produces a form which loads. Suggesting
// a tag which would then be reported as misplaced would be one wrong answer
// replacing another.
func (v *validator) unknown(s scope, node *Node, tag string) {
	permitted := s.form.tags()

	var hint string
	switch near, ok := nearest(tag, permitted); {
	case ok:
		hint = fmt.Sprintf("did you mean (%s ...)?", near)
	case len(permitted) == 0:
		hint = fmt.Sprintf("%s takes no children", s.where)
	default:
		hint = fmt.Sprintf("%s takes %s", s.where, join(parenthesise(permitted), "and"))
	}

	v.add(node.Span, fmt.Sprintf("expected %s, found (%s ...), which is not a known form", s.expected, tag), hint)
}

// count checks how many of each child were written against how many the form
// permits.
//
// The children are walked in the table's order rather than in the order they
// were written, because a child which is missing was written in no order at
// all, and the canonical child order is the only one left to report them in.
func (v *validator) count(s scope, seen map[string][]*Node) {
	for _, c := range s.form.children {
		written := seen[c.tag]
		if c.arity.permits(len(written)) {
			continue
		}

		if len(written) < c.arity.min {
			v.add(s.span, fmt.Sprintf("expected %s (%s ...) child of %s, found none", requirement(c.arity), c.tag, s.where), "")
			continue
		}

		// Every occurrence past the last permitted one is its own diagnostic,
		// pointing at itself and at the first, because a diagnostic about a
		// duplicate which names only one of the two leaves the reader to find
		// the other.
		for _, extra := range written[c.arity.max:] {
			v.add(
				extra.Span,
				fmt.Sprintf("expected at most one (%s ...) child of %s, found another", c.tag, s.where),
				c.duplicated,
				RelatedLocation{Span: written[0].Span, Message: "first written here"},
			)
		}
	}
}

// requirement is how a diagnostic says a missing child was required: as one, or
// as at least one of a child which may repeat.
func requirement(a arity) string {
	if a.max == a.min {
		return "a"
	}
	return "at least one"
}

// split divides the elements written after a tag into the positional arguments
// and the child forms.
//
// The arguments are the elements before the first child form, which is what
// makes `(value (0.0 4.05 0.0) m)` two arguments and `(value (transform ...))`
// one child: a list is a child form when something tagged it, and a positional
// datum otherwise.
func split(elements []*Node) (written, children []*Node) {
	for i, element := range elements {
		if _, ok := formTag(element); ok {
			return elements[:i], elements[i:]
		}
	}
	return elements, nil
}

// formTag returns the tag a node was written with, and whether it is a form at
// all: a non-empty list whose first element is a symbol.
func formTag(node *Node) (string, bool) {
	list, ok := node.Datum.(sexpr.List)
	if !ok || len(list.Elements) == 0 {
		return "", false
	}

	symbol, ok := list.Elements[0].(sexpr.Symbol)
	if !ok {
		return "", false
	}

	return symbol.Value, true
}

// describe names what was written where a form was expected, so that the
// diagnostic says what is there rather than only what is not.
func describe(node *Node) string {
	switch datum := node.Datum.(type) {
	case sexpr.Symbol:
		return "the symbol " + datum.Value
	case sexpr.String:
		return "the string " + strconv.Quote(datum.Value)
	case sexpr.Int:
		return "the number " + strconv.FormatInt(datum.Value, 10)
	case sexpr.Float:
		return "the number " + strconv.FormatFloat(datum.Value, 'g', -1, 64)
	case sexpr.Bool:
		if datum.Value {
			return "the boolean #t"
		}
		return "the boolean #f"
	case sexpr.Nil:
		return "nil"
	case sexpr.Quote:
		return "a quoted datum"
	case sexpr.List:
		if len(datum.Elements) == 0 {
			return "an empty list"
		}
		return "a list with no tag"
	}
	return "something the format does not recognise"
}

// count spells how many of something were found, so that none reads as a word
// rather than as a zero.
func count(n int) string {
	if n == 0 {
		return "none"
	}
	return strconv.Itoa(n)
}

// parenthesise writes each tag the way a form is written, so that a list of
// them reads as forms rather than as words.
func parenthesise(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		out = append(out, "("+tag+" ...)")
	}
	return out
}

// join lists items with the given conjunction before the last.
func join(items []string, conjunction string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " " + conjunction + " " + items[len(items)-1]
}

// nearest returns the candidate closest to the tag written, and whether one is
// close enough to be read as a misspelling of it.
//
// Candidates are considered in the order they are given, which is the canonical
// child order, so two candidates equally close resolve the same way on every
// run.
func nearest(tag string, candidates []string) (string, bool) {
	best, distance := "", 0

	for _, candidate := range candidates {
		d := editDistance(tag, candidate)
		if best == "" || d < distance {
			best, distance = candidate, d
		}
	}

	if best == "" || distance > tolerated(tag) {
		return "", false
	}
	return best, true
}

// tolerated is how far a written tag may be from a known one and still be
// offered as a suggestion.
//
// A short tag is given less room than a long one because two edits turn a
// four-letter word into an unrelated one, and a suggestion nobody meant is
// worse than none: it sends the reader to change a line which was never the
// problem.
func tolerated(tag string) int {
	if len(tag) < 5 {
		return 1
	}
	return 2
}

// editDistance is the optimal string alignment distance between two strings, in
// bytes: an insertion, a deletion, a substitution or a transposition of two
// adjacent characters each cost one.
//
// A transposition costs one rather than the two a plain Levenshtein distance
// charges it because swapping two letters is what typing produces. `ndoe` for
// `node` is one mistake to whoever made it, and a measure which called it two
// would refuse to suggest the tag they meant.
//
// Bytes rather than runes because every tag of the format is ASCII, and a tag
// which is not is already wrong for a reason a distance cannot express.
func editDistance(a, b string) int {
	// Three rows are kept because a transposition reaches two rows back.
	twoBack := make([]int, len(b)+1)
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)

	for j := range previous {
		previous[j] = j
	}

	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}

			current[j] = min(previous[j-1]+cost, previous[j]+1, current[j-1]+1)

			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				current[j] = min(current[j], twoBack[j-2]+cost)
			}
		}
		twoBack, previous, current = previous, current, twoBack
	}

	return previous[len(b)]
}
