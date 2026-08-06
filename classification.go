// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"slices"
	"strings"

	sexpr "github.com/z5labs/sexpr-go"
)

// UndeclaredTypeError reports a type name no registry file declares.
//
// The declared set travels with it rather than only the name, so that a caller
// which cannot spell the type can list what there was without re-reading the
// registry.
type UndeclaredTypeError struct {
	// Type is the name which was asked for.
	Type string

	// Declared is every type the registry declares, in name order.
	Declared []string
}

// Error implements the [error] interface.
func (e UndeclaredTypeError) Error() string {
	if len(e.Declared) == 0 {
		return fmt.Sprintf("expected a declared type, found %s: this model declares no type at all", e.Type)
	}
	return fmt.Sprintf("expected a declared type, found %s, which no registry file declares", e.Type)
}

// AlreadyClassifiedError reports a type already classified in the system a
// change would classify it in.
//
// A type carries at most one code per system, so this is not a change which
// could be applied twice harmlessly: the second code is either the same one,
// which the file already says, or a different one, which is a correction rather
// than an addition and has to be written as one.
type AlreadyClassifiedError struct {
	// Type is the type the change was about.
	Type string

	// System is the scheme it is already classified in.
	System string

	// Code is what the registry already says it is called there.
	Code string
}

// Error implements the [error] interface.
func (e AlreadyClassifiedError) Error() string {
	return fmt.Sprintf("expected a system %s is not already classified in, found %q, in which it is already %q",
		e.Type, e.System, e.Code)
}

// IncompleteClassificationError reports a classification missing one of its two
// halves.
//
// Both are refused here rather than at the load which follows, because the load
// would report a file the caller never wrote and never sees: a write is refused
// before any byte reaches disk
// ([0015](docs/decisions/0015-the-cli-is-the-primary-write-path.md)), and the
// mistake is in the call rather than in the model.
type IncompleteClassificationError struct {
	// Type is the type the change was about.
	Type string

	// Missing names the halves which were blank, in the order they are written.
	Missing []string
}

// Error implements the [error] interface.
func (e IncompleteClassificationError) Error() string {
	return fmt.Sprintf("expected a classification system and a code, found no %s",
		strings.Join(e.Missing, " and no "))
}

// Classify writes an external classification onto a declared type.
//
// It is the write path for the `classification` child of specification section
// 7.3: how a scheme outside this model names one of this project's types. Both
// strings are carried through unread — the engine has no list of systems and no
// syntax for a code — so what this validates is that the type is declared, that
// both halves were given, and that the type is not already classified in that
// system.
//
// The child is appended to the declaration, which decides nothing about where it
// lands: canonical form sorts the children of every form, so the classification
// prints where specification section 7.3 tables it whatever order it was added
// in.
//
// A model which does not load is never reached here — [Begin] refuses one — and
// the change itself is validated by [Tx.Commit] like any other, so a
// classification which would not read back is refused before anything is
// written.
func (tx *Tx) Classify(name string, classification ExternalClassification) error {
	if tx.finished {
		return ErrFinished
	}

	var missing []string
	if classification.System == "" {
		missing = append(missing, "system")
	}
	if classification.Code == "" {
		missing = append(missing, "code")
	}
	if len(missing) > 0 {
		return IncompleteClassificationError{Type: name, Missing: missing}
	}

	registry := tx.Graph().Registry()

	declared, ok := registry.Type(name)
	if !ok {
		return UndeclaredTypeError{Type: name, Declared: registry.Names(SortType)}
	}

	form, ok := tx.declaration(string(SortType), name)
	if !ok {
		return UnknownFormError{Span: declared.Span}
	}

	// The systems are read off the form rather than off the declaration the
	// registry holds. [Tx.Graph] is the model as the transaction found it and
	// does not move as mutations are applied, so a second classification in one
	// transaction would otherwise be refused only by the load at [Tx.Commit] —
	// which is a refusal of the whole batch, pointing at a file the caller never
	// wrote, for a mistake it could have been told about where it made it.
	if code, ok := classifiedAs(form, classification.System); ok {
		return AlreadyClassifiedError{Type: name, System: classification.System, Code: code}
	}

	return tx.Replace(form, classified(form, classification))
}

// classifiedAs is the code a type form already carries in system, read from the
// form itself.
//
// A child which is not a well-formed classification is passed over rather than
// guessed at: what is wrong with it is the loader's to report, in the author's
// words and at the position it was written, and a second opinion here would name
// the same mistake twice.
func classifiedAs(form *Node, system string) (string, bool) {
	for _, child := range childForms(form, classificationChild) {
		written, ok := argument(child, 0)
		if !ok {
			continue
		}

		datum, ok := written.Datum.(sexpr.String)
		if !ok || datum.Value != system {
			continue
		}

		code, ok := argument(child, 1)
		if !ok {
			return "", true
		}

		if datum, ok := code.Datum.(sexpr.String); ok {
			return datum.Value, true
		}

		return "", true
	}

	return "", false
}

// declaration is the top-level registry form written with tag and the
// positional name, and whether the transaction holds one.
//
// [Tx.Form] answers the same question for the forms written with an id, which is
// every entity. A registry entry other than a frame is named with a plain
// symbol instead, so it needs its own lookup rather than a widening of that one:
// an id and a name are different things, and a lookup which took either would
// match a type called `site` against the id `site:S-101`.
func (tx *Tx) declaration(tag, name string) (*Node, bool) {
	if tx == nil || name == "" {
		return nil, false
	}

	for _, key := range tx.order {
		for _, node := range tx.files[key].file.Nodes {
			if written, ok := formTag(node); !ok || written != tag {
				continue
			}

			if declaredName(node) == name {
				return node, true
			}
		}
	}

	return nil, false
}

// classified is form with the classification written on it.
func classified(form *Node, classification ExternalClassification) *Node {
	child := formNode(classificationChild,
		stringNode(classification.System),
		stringNode(classification.Code),
	)

	return relisted(form, append(slices.Clone(form.Children), child))
}

// declaredName is the plain symbol a registry entry is declared under, and is
// empty for a form declared any other way.
//
// It is what an [Effect] about a registry form carries instead of an id, and
// what [Tx.declaration] matches on. An entity names itself with an id, so a
// first argument which is a symbol but not one is a diagnostic rather than a
// name to report back as though it were fine — which is why only the registry
// forms are asked.
func declaredName(form *Node) string {
	arg, ok := argument(form, 0)
	if !ok {
		return ""
	}

	symbol, ok := arg.Datum.(sexpr.Symbol)
	if !ok {
		return ""
	}

	return symbol.Value
}
