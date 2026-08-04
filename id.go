// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"fmt"
	"strings"

	sexpr "github.com/z5labs/sexpr-go"
)

// idSeparator divides an id into its two parts. The split is on the first one:
// a local part may hold further colons, and a namespace never does.
const idSeparator = ":"

// ID identifies one thing in a model for the life of that thing, per
// specification section 4.1.
//
// An id is assigned once and never changes. Claims, assertions, observation
// links and every downstream artifact point at ids, so an id which moved would
// break all of them silently and none of them would say so
// ([0002](docs/decisions/0002-immutable-id-mutable-label.md)). A [SemanticNode]
// label is the other half of that arrangement: it is display text, it is free
// to change, and nothing resolves through it.
//
// An ID is written `namespace:local`. The namespace says which authority
// minted the id and is declared in the registry
// ([0003](docs/decisions/0003-id-namespaces-are-a-closed-registry.md)); the
// local part is the authority's own name for the thing and is opaque here. The
// engine attaches no meaning to either beyond that. Nothing infers a [Kind], a
// type, an accuracy or a rendering from an id prefix, which is what keeps
// `site:S-101` a name rather than a schema.
//
// The underlying type is a string so that an ID is comparable, usable as a map
// key, and compared exactly the way specification section 4.1 requires: byte
// for byte, with no case folding, no Unicode normalisation and no trimming.
// Two ids are the same id when their bytes are the same.
//
// A conversion is not a check. [ParseID] is what enforces the production, and
// the zero ID is the id of a thing whose id could not be read — which is a
// diagnostic naming what was written, not an id anything resolves.
type ID string

// ParseID reads s as an id, per specification section 4.1.
//
// Both parts are non-empty and the colon is required, so `:x`, `survey:` and an
// unqualified `corner` are each a [MalformedIDError] rather than an id with a
// part missing. The namespace is ASCII and begins with a letter; the local part
// is written with the characters a symbol is written with, which is what makes
// every well-formed id a well-formed symbol and so writable in a file without
// quoting.
//
// Whether the namespace is one the model declares is a question only a
// [Registry] answers, and ParseID does not ask it. The shape of an id is a
// property of the id; which namespaces exist is a property of the model.
//
// A failure yields the zero ID. What was written is carried on the error, so a
// diagnostic can quote it without the caller having kept a second copy.
func ParseID(s string) (ID, error) {
	namespace, local, qualified := strings.Cut(s, idSeparator)

	switch {
	case !qualified:
		return "", MalformedIDError{Written: s, Reason: IDUnqualified}
	case namespace == "":
		return "", MalformedIDError{Written: s, Reason: IDEmptyNamespace}
	case local == "":
		return "", MalformedIDError{Written: s, Reason: IDEmptyLocal}
	case !wellFormedNamespace(namespace):
		return "", MalformedIDError{Written: s, Reason: IDMalformedNamespace}
	case !symbolic(s):
		return "", MalformedIDError{Written: s, Reason: IDMalformedLocal}
	}

	return ID(s), nil
}

// Namespace returns the part of the id before the first colon, which is the
// authority which minted it.
//
// It is empty for an id which is not one, which only the zero ID and a value
// converted rather than parsed can be.
func (id ID) Namespace() string {
	namespace, _, qualified := strings.Cut(string(id), idSeparator)
	if !qualified {
		return ""
	}
	return namespace
}

// Local returns the part of the id after the first colon, which is the minting
// authority's own name for the thing and means nothing here.
//
// Further colons belong to it: the split is on the first one only.
func (id ID) Local() string {
	_, local, qualified := strings.Cut(string(id), idSeparator)
	if !qualified {
		return ""
	}
	return local
}

// String returns the id as it was written.
func (id ID) String() string { return string(id) }

// wellFormedNamespace reports whether s matches the namespace production of
// specification section 4.1.
//
// ASCII, and beginning with a letter, is not stylistic. A namespace is part of
// an id, an id is written as a bare symbol, and a symbol beginning with a digit
// is a malformed number to the tokenizer — a lexical error reported before any
// of this specification is consulted. Confining the namespace is what makes
// every well-formed id a well-formed symbol. Confining it to ASCII keeps two
// namespaces which render identically from being different ids.
//
// The registry declaring a namespace checks it with this too: a namespace no id
// could carry is not one anything could be declared under.
func wellFormedNamespace(s string) bool {
	if s == "" {
		return false
	}

	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '-' || c == '_'):
		default:
			return false
		}
	}

	return true
}

// symbolic reports whether s is exactly one symbol to the tokenizer the format
// delegates its lexis to.
//
// The local part of an id is written with the delegated symbol characters, and
// there is one authority on which those are. Restating the set here would be a
// second copy of a rule which lives in the tokenizer, and the two would
// disagree the first time either moved — the same reason spanning tokenizes a
// second time rather than scanning for the end of a lexeme itself.
//
// Asking it of the whole id rather than of the local part alone is what also
// settles the question the production leaves implicit: an id has to read back
// out of a file as the one symbol it was written as.
func symbolic(s string) bool {
	var symbols int

	for tok, err := range sexpr.Tokenize(strings.NewReader(s)) {
		if err != nil || tok.Type != sexpr.TokenSymbol || string(tok.Value) != s {
			return false
		}
		symbols++
	}

	return symbols == 1
}

// IDProblem says which rule of specification section 4.1 a malformed id broke.
//
// It is a field rather than wording inside a message so that what to do about
// the id is something a caller and a test can act on. The string value is the
// phrase a diagnostic would use for it, which is what keeps the two spellings
// of one problem from drifting.
type IDProblem string

const (
	// IDUnqualified is an id written with no colon at all, which is a local part
	// with no authority behind it.
	IDUnqualified IDProblem = "unqualified"

	// IDEmptyNamespace is an id beginning with the colon.
	IDEmptyNamespace IDProblem = "empty namespace"

	// IDEmptyLocal is an id ending with the colon.
	IDEmptyLocal IDProblem = "empty local part"

	// IDMalformedNamespace is a namespace which is not ASCII, does not begin
	// with a letter, or holds something other than letters, digits, hyphens and
	// underscores.
	IDMalformedNamespace IDProblem = "malformed namespace"

	// IDMalformedLocal is a local part holding something a symbol may not hold,
	// which is an id no file could be written with.
	IDMalformedLocal IDProblem = "malformed local part"
)

// rule states the production the id broke.
//
// It is the hint a diagnostic carries and the second half of the error message,
// because the answer to "that is not an id" is the rule it missed rather than a
// restatement that it missed one.
func (p IDProblem) rule() string {
	switch p {
	case IDUnqualified:
		return "an id is namespace:local, split on the first colon"
	case IDEmptyNamespace:
		return "the namespace is everything before the first colon, and it is not empty"
	case IDEmptyLocal:
		return "the local part is everything after the first colon, and it is not empty"
	case IDMalformedNamespace:
		return "a namespace is ASCII, begins with a letter, and continues with letters, digits, hyphens and underscores"
	case IDMalformedLocal:
		return "a local part is written with the characters a symbol is written with, so that the id as a whole is one"
	}
	return ""
}

// MalformedIDError reports text written where an id belongs which is not one.
//
// The text is carried rather than only described because a diagnostic about an
// id quotes it, and because the rule it broke is a field rather than a sentence
// so that a caller can tell an unqualified id — which is somebody forgetting a
// namespace — apart from one whose namespace is spelled with characters no
// namespace may hold.
type MalformedIDError struct {
	// Written is the text as it appeared.
	Written string

	// Reason is the rule of specification section 4.1 it broke.
	Reason IDProblem
}

// Error implements the [error] interface.
func (e MalformedIDError) Error() string {
	return fmt.Sprintf("expected an id, found %s: %s", e.Written, e.Reason.rule())
}

// malformedID is the diagnostic for a symbol written where an id belongs which
// is not one.
//
// It lives beside the production rather than at each call site because the
// answer to "that is not an id" is the same on a frame declaration, on a node
// and on the reference from one to the other.
func malformedID(span Span, err MalformedIDError) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message:  fmt.Sprintf("expected an id, found %s", err.Written),
		Hint:     err.Reason.rule(),
	}
}

// asMalformedID recovers the failure [ParseID] reports, which is the only one
// it reports.
func asMalformedID(err error) (MalformedIDError, bool) {
	var malformed MalformedIDError
	ok := errors.As(err, &malformed)
	return malformed, ok
}
