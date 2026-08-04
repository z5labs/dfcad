// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"strconv"
	"time"

	sexpr "github.com/z5labs/sexpr-go"
)

// reader reads typed values out of a spanned tree, collecting a diagnostic
// wherever the tree held something other than what was wanted.
//
// Every pass which interprets forms embeds one: the registry loader and the
// entity loader both. "expected a symbol, found a string" is the same answer
// wherever the symbol was wanted, and a second copy of it would be a second
// wording of one sentence which would then drift from the first.
//
// The zero value is ready to use. Diagnostics accumulate in the order the pass
// found them; collecting them into a [Diagnostics] is what puts them in
// reporting order.
type reader struct {
	// diags are the problems found so far.
	diags []Diagnostic
}

// add records diagnostics.
func (r *reader) add(diags ...Diagnostic) {
	r.diags = append(r.diags, diags...)
}

// name reads the positional name of a declaration, with the span a diagnostic
// about that name points at.
func (r *reader) name(node *Node, what string) (string, Span, bool) {
	arg, ok := argument(node, 0)
	if !ok {
		return "", Span{}, false
	}

	name, ok := r.symbol(arg, what)
	return name, arg.Span, ok
}

// identifier reads the positional id of a declaration, with the span a
// diagnostic about that id points at.
func (r *reader) identifier(node *Node, what string) (ID, Span, bool) {
	arg, ok := argument(node, 0)
	if !ok {
		return "", Span{}, false
	}

	id, ok := r.id(arg, what)
	return id, arg.Span, ok
}

// id reads an id, reporting the rule it broke where what was written is not
// one.
//
// An id which is not one yields the zero ID rather than the text it was written
// with, for the reason every other axis of a form does: the diagnostic already
// carries what was written, and a value which is not one of the things it could
// have been would be judged by everything downstream as though somebody had
// written it.
func (r *reader) id(node *Node, what string) (ID, bool) {
	written, ok := r.symbol(node, what)
	if !ok {
		return "", false
	}

	id, err := ParseID(written)
	if malformed, ok := asMalformedID(err); ok {
		r.add(malformedID(node.Span, malformed))
		return "", false
	}

	return id, true
}

// registered checks that the namespace of an id is one registry declares.
//
// It is the whole of what this layer has to say about an id: whether the thing
// it names exists is a question for the layer which holds those things, and
// whether the namespace exists is a question only the registry answers
// ([0003](docs/decisions/0003-id-namespaces-are-a-closed-registry.md)).
//
// The zero ID is not checked. It belongs to something which was not written or
// which was already reported as not being an id, and a namespace diagnostic on
// top of that is one mistake reported twice.
func (r *reader) registered(registry *Registry, id ID, span Span) bool {
	if id == "" {
		return false
	}

	if !registry.Declares(SortNamespace, id.Namespace()) {
		r.add(registry.Undeclared(SortNamespace, id.Namespace(), span))
		return false
	}

	return true
}

// symbol reads a symbol, reporting what was written there instead.
func (r *reader) symbol(node *Node, what string) (string, bool) {
	datum, ok := node.Datum.(sexpr.Symbol)
	if !ok {
		r.wrong(node, what)
		return "", false
	}
	return datum.Value, true
}

// text reads a string.
func (r *reader) text(node *Node, what string) (string, bool) {
	datum, ok := node.Datum.(sexpr.String)
	if !ok {
		r.wrong(node, what)
		return "", false
	}
	return datum.Value, true
}

// boolean reads a boolean.
func (r *reader) boolean(node *Node, what string) (bool, bool) {
	datum, ok := node.Datum.(sexpr.Bool)
	if !ok {
		r.wrong(node, what)
		return false, false
	}
	return datum.Value, true
}

// integer reads a count, which specification section 4.3 writes with neither a
// fraction nor an exponent so that it reads back as an integer.
func (r *reader) integer(node *Node, what string) (int64, bool) {
	datum, ok := node.Datum.(sexpr.Int)
	if !ok {
		r.wrong(node, what)
		return 0, false
	}
	return datum.Value, true
}

// real reads a real number, which specification section 4.3 writes with a
// fraction or an exponent so that it reads back as a real.
//
// A whole number written as one — `5` where `5.0` was meant — is reported
// rather than widened, because the distinction is the only thing telling a
// magnitude apart from a count in a format where both are written as digits.
func (r *reader) real(node *Node, what string) (float64, bool) {
	datum, ok := node.Datum.(sexpr.Float)
	if !ok {
		if _, isInt := node.Datum.(sexpr.Int); isInt {
			r.add(Diagnostic{
				Severity: SeverityError,
				Span:     node.Span,
				Message:  fmt.Sprintf("expected %s, found %s", what, describe(node)),
				Hint:     "a real number is written with a fraction or an exponent, so that it reads back as a real",
			})
			return 0, false
		}

		r.wrong(node, what)
		return 0, false
	}
	return datum.Value, true
}

// dateLayout is the one spelling of a date, per specification section 4.4: RFC
// 3339 full-date, four-digit year, two-digit month, two-digit day, proleptic
// Gregorian, hyphen separated. No time, no zone and no other spelling.
const dateLayout = time.DateOnly

// date reads a date, reporting the spelling it was meant to have where what was
// written is not one.
//
// It is read from a string rather than from a bare symbol because it has to be:
// `2026-03-14` begins like a number, so the delegated tokenizer classifies it as
// a malformed one and fails before this format is reached. Quoting it is the
// only spelling which survives that lexis.
//
// A failure yields the zero time rather than a partial reading, for the reason
// every other axis of a form does: the diagnostic already carries what was
// written, and a date nobody wrote would be compared by everything downstream as
// though somebody had.
func (r *reader) date(node *Node, what string) (time.Time, bool) {
	written, ok := r.text(node, what)
	if !ok {
		return time.Time{}, false
	}

	date, err := time.Parse(dateLayout, written)
	if err != nil {
		r.add(Diagnostic{
			Severity: SeverityError,
			Span:     node.Span,
			Message:  fmt.Sprintf("expected %s, found %s", what, strconv.Quote(written)),
			Hint:     `a date is written as a string in RFC 3339 full-date form, "YYYY-MM-DD", with no time and no zone`,
		})
		return time.Time{}, false
	}

	return date, true
}

// wrong reports a datum of the wrong sort written where a form wanted one of
// another.
func (r *reader) wrong(node *Node, what string) {
	r.add(Diagnostic{
		Severity: SeverityError,
		Span:     node.Span,
		Message:  fmt.Sprintf("expected %s, found %s", what, describe(node)),
	})
}

// elements is everything written after a form's tag.
func elements(node *Node) []*Node {
	if len(node.Children) == 0 {
		return nil
	}
	return node.Children[1:]
}

// argument returns the i-th positional argument of a form, and whether it was
// written.
func argument(node *Node, i int) (*Node, bool) {
	written, _ := split(elements(node))
	if i < 0 || i >= len(written) {
		return nil, false
	}
	return written[i], true
}

// childForm returns the first child of node written with tag, and whether one
// was written.
func childForm(node *Node, tag string) (*Node, bool) {
	_, children := split(elements(node))

	for _, child := range children {
		if written, ok := formTag(child); ok && written == tag {
			return child, true
		}
	}

	return nil, false
}

// childForms returns every child of node written with tag, in the order they
// were written.
func childForms(node *Node, tag string) []*Node {
	_, children := split(elements(node))

	var out []*Node
	for _, child := range children {
		if written, ok := formTag(child); ok && written == tag {
			out = append(out, child)
		}
	}

	return out
}

// argumentOf returns the single positional argument of the child written with
// tag, which is how every one-value child of a registry or entity form is read.
func argumentOf(node *Node, tag string) (*Node, bool) {
	child, ok := childForm(node, tag)
	if !ok {
		return nil, false
	}
	return argument(child, 0)
}

// spellings spells a closed set for a diagnostic which lists it.
func spellings[T ~string](set []T) []string {
	out := make([]string, 0, len(set))
	for _, member := range set {
		out = append(out, string(member))
	}
	return out
}
