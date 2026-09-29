// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ErrNoAssertionSubject is an assertion written on nothing.
var ErrNoAssertionSubject = errors.New("an assertion is written on the thing it constrains")

// ErrNoCheck is an assertion naming no check.
//
// An assertion is a check name and its parameters and nothing else
// ([0011](docs/decisions/0011-assertions-are-named-parameterised-checks.md)), so
// one with no name has nothing left to say.
var ErrNoCheck = errors.New("an assertion names the check it applies")

// UnregisteredCheckError is an assertion naming a check the engine does not
// register.
//
// The registry is closed and compiled in, so the set it names is the whole of
// what could have been meant: adding a check is a change to the engine, and
// reading the set is how an author finds that out.
type UnregisteredCheckError struct {
	// Check is the name which was written.
	Check string

	// Registered is every check the engine registers, in name order.
	Registered []string

	// Diagnostic is what a load reports for the same assertion written by hand.
	Diagnostic Diagnostic
}

// Error implements the [error] interface.
func (e UnregisteredCheckError) Error() string {
	return fmt.Sprintf("%s: the registered checks are %s", e.Diagnostic.Message, join(e.Registered, "and"))
}

// UnknownParameterError is a parameter the check does not take.
type UnknownParameterError struct {
	// Check is the check the assertion names.
	Check string

	// Parameter is the tag which was written.
	Parameter string

	// Takes is every parameter the check does take, in declared order.
	Takes []string

	// Diagnostic is what a load reports for the same assertion written by hand.
	Diagnostic Diagnostic
}

// Error implements the [error] interface.
func (e UnknownParameterError) Error() string { return spellDiagnostic(e.Diagnostic) }

// RepeatedParameterError is a parameter written twice.
//
// A parameter takes every value it has at once, so the second is not more of
// the first: it is a second answer to the same question.
type RepeatedParameterError struct {
	// Check is the check the assertion names.
	Check string

	// Parameter is the tag which was written more than once.
	Parameter string

	// Diagnostic is what a load reports for the same assertion written by hand.
	Diagnostic Diagnostic
}

// Error implements the [error] interface.
func (e RepeatedParameterError) Error() string { return spellDiagnostic(e.Diagnostic) }

// MissingParameterError is a parameter the check requires and the assertion
// does not write.
type MissingParameterError struct {
	// Check is the check the assertion names.
	Check string

	// Parameter is the parameter left out.
	Parameter string

	// Want is the sort of datum it takes.
	Want ParameterType

	// Diagnostic is what a load reports for the same assertion written by hand.
	Diagnostic Diagnostic
}

// Error implements the [error] interface.
func (e MissingParameterError) Error() string { return spellDiagnostic(e.Diagnostic) }

// ParameterValueError is a parameter whose value is not the sort of datum the
// check declares it takes: a number where a tolerance name belongs, a predicate
// the registry does not declare, two values where one is taken.
type ParameterValueError struct {
	// Check is the check the assertion names.
	Check string

	// Parameter is the parameter whose value is wrong.
	Parameter string

	// Want is the sort of datum it takes.
	Want ParameterType

	// Diagnostic is what a load reports for the same assertion written by hand,
	// which says what was found.
	Diagnostic Diagnostic
}

// Error implements the [error] interface.
func (e ParameterValueError) Error() string { return spellDiagnostic(e.Diagnostic) }

// spellDiagnostic is a diagnostic as the message of an error: what was expected
// and found, then what to do about it.
func spellDiagnostic(diagnostic Diagnostic) string {
	if diagnostic.Hint == "" {
		return diagnostic.Message
	}
	return diagnostic.Message + ": " + diagnostic.Hint
}

// InvalidAssertionError is an assertion the check registry refuses, carrying
// every reason it does.
//
// Each reason is one of [UnregisteredCheckError], [UnknownParameterError],
// [RepeatedParameterError], [MissingParameterError] and [ParameterValueError],
// and [errors.As] reaches each of them. They are every problem the validation
// found rather than the first, for the reason a load reports every problem it
// finds: an author fixing one assertion should not have to reissue it once per
// mistake.
type InvalidAssertionError struct {
	// Subject is the thing the assertion was to be written on.
	Subject ID

	// Check is the check it names.
	Check string

	// Errs is every reason it was refused, in the order the validation found
	// them.
	Errs []error
}

// Error implements the [error] interface.
func (e InvalidAssertionError) Error() string {
	spelled := make([]string, 0, len(e.Errs))
	for _, err := range e.Errs {
		spelled = append(spelled, err.Error())
	}
	return fmt.Sprintf("assertion %s on %s: %s", e.Check, e.Subject, strings.Join(spelled, "; "))
}

// Unwrap returns every reason, so that [errors.Is] and [errors.As] reach each of
// them.
func (e InvalidAssertionError) Unwrap() []error { return e.Errs }

// MalformedParameterError is text written where an assertion parameter belongs
// which is not one.
//
// A parameter is written as the entity format writes it without its
// parentheses — `tolerance boundary-closure`, `zone site:Z-90` — so what is
// refused here is text which does not read as exactly one such form.
type MalformedParameterError struct {
	// Written is the text as it was given.
	Written string

	// Err is why it could not be read as a form, and is nil where it read as
	// something other than one tagged form.
	Err error
}

// Error implements the [error] interface.
func (e MalformedParameterError) Error() string {
	found := strconv.Quote(e.Written)
	if e.Err != nil {
		found += ": " + e.Err.Error()
	}
	return fmt.Sprintf(`expected a parameter written as "<name> <value>...", found %s`, found)
}

// Unwrap returns why the text could not be read.
func (e MalformedParameterError) Unwrap() error { return e.Err }

// NotAssertableError is an assertion on something which carries none.
//
// An assertion is a child of a node, a vertex, an edge or a loop
// (specification sections 6.1 through 6.4). A frame is registry data and
// declares no `assert` child (section 7.5), and a claim is a statement about a
// thing rather than a thing an assertion constrains. What must hold of every
// instance of a type is the type's `invariant`, which is registry data too and
// is written by hand.
type NotAssertableError struct {
	// ID is the id the assertion was to be written on.
	ID ID

	// What is what that id names: "a frame" or "a claim".
	What string

	// At is where it is defined.
	At Span
}

// Error implements the [error] interface.
func (e NotAssertableError) Error() string {
	return fmt.Sprintf(
		"%s names %s, defined at %s, which carries no assertion: an assertion is written on a node, a vertex, an edge or a loop",
		e.ID, e.What, e.At.Start,
	)
}

// AssertionSpec is an assertion which is not written yet: the thing it
// constrains, the check it names and the parameters it supplies.
//
// The parameters are text rather than forms, each spelled as the entity format
// writes it without its parentheses — `predicate width`, `tolerance
// boundary-closure`, `zone site:Z-90` — which is how an accuracy term is
// spelled on a command line and in an operation file. Which sort of datum a
// parameter takes is the check's declaration, exactly as which shape a value
// takes is the predicate's, so one spelling means a parameter written in either
// place is read by the same code and refused in the same words.
type AssertionSpec struct {
	// Subject is the thing the assertion constrains.
	Subject ID

	// Check is the name of the check it applies.
	Check string

	// Parameters are the parameters, in the order they are written.
	Parameters []string
}

// form is the `assert` form the spec describes, and the reason it could not be
// built where one of its parameters does not read.
func (spec AssertionSpec) form() (*Node, error) {
	children := make([]*Node, 0, len(spec.Parameters)+1)
	children = append(children, symbolNode(spec.Check))

	for _, written := range spec.Parameters {
		parameter, err := ParseParameter(written)
		if err != nil {
			return nil, err
		}
		children = append(children, parameter)
	}

	return formNode(assertChild, children...), nil
}

// ParseParameter reads one assertion parameter as it is written on a command
// line and in an operation file: the parameter form without its parentheses,
// its tag first and then its values.
//
// Nothing about the check is asked here. Whether the tag is one the check takes
// and whether each value is the sort of datum it declares are the check
// registry's questions, answered by the validation a load runs
// ([ValidateAssertion]).
func ParseParameter(written string) (*Node, error) {
	file, err := parse("", []byte("("+written+"\n)"))
	if err != nil {
		return nil, MalformedParameterError{Written: written, Err: err}
	}

	// Text which closed its own parenthesis and opened another reads as two
	// forms, and a comment is text the parameter would carry into the file
	// without anybody having written it there: neither is one parameter,
	// whatever each half would have meant.
	if len(file.Nodes) != 1 || len(file.Comments) > 0 || commented(file.Nodes[0]) {
		return nil, MalformedParameterError{Written: written}
	}

	parameter := file.Nodes[0]
	if _, ok := formTag(parameter); !ok {
		return nil, MalformedParameterError{Written: written}
	}

	return parameter, nil
}

// commented reports whether a comment is written anywhere inside node.
func commented(node *Node) bool {
	if len(node.Comments) > 0 {
		return true
	}
	return slices.ContainsFunc(node.Children, commented)
}

// AddAssertion writes an assertion on the thing it constrains: a check name and
// the parameters it is applied with, as a child of that thing's form.
//
// **What is wrong with the assertion itself is refused here**, before anything
// is written: a subject nothing holds, a subject which carries no assertion (a
// frame, a claim), a check the engine does not register, and a parameter the
// check does not take, one it requires and which is missing, one written twice
// or a value not of the sort the check declares. The last four are the
// validation every load runs on every assertion ([ValidateAssertion]) and come
// back together as one [InvalidAssertionError], so the write path and a
// hand-written file are held to one rule in one set of words.
//
// **What is wrong with the assertion in this model is refused at [Tx.Commit]**,
// with the diagnostics a load of the result would have raised: a check which
// cannot examine the subject's form, kind or geometry, an assertion restating a
// value the subject's claims already carry, and a parameter naming an id nothing
// holds ([ResolveAssertions]). Those are questions about the whole model, and
// the model is interpreted once, when the change is committed.
//
// A subject this same change wrote counts, which is what lets one batch add a
// node and the rule it has to satisfy.
//
// What must hold of every instance of a type is not this. That is the type's
// `invariant`, which is registry data and is written by hand in the registry
// file which declares the type.
func (tx *Tx) AddAssertion(spec AssertionSpec) error {
	if tx.finished {
		return ErrFinished
	}

	if spec.Subject == "" {
		return ErrNoAssertionSubject
	}

	if spec.Check == "" {
		return ErrNoCheck
	}

	if err := tx.assertable(spec.Subject); err != nil {
		return err
	}

	assertion, err := spec.form()
	if err != nil {
		return err
	}

	if err := tx.validAssertion(spec, assertion); err != nil {
		return err
	}

	// The entity is there and the transaction holds no form under its id, which
	// is what a form removed by an earlier mutation of this transaction leaves
	// behind. It is a different answer from an id nothing answers to, which the
	// check above has already given.
	form, ok := tx.Form(spec.Subject)
	if !ok {
		return UnknownFormError{}
	}

	return tx.Replace(form, asserted(form, assertion))
}

// assertable reports whether id names a thing an assertion may be written on,
// counting what this same change has already written.
//
// A frame and a claim share the id space with the entities and are asked first,
// because an id which names one of them names something the model holds and is
// answered as that rather than as an id nothing answers to.
func (tx *Tx) assertable(id ID) error {
	if frame, ok := tx.graph.Registry().Frame(id); ok {
		return NotAssertableError{ID: id, What: "a frame", At: frame.Span}
	}

	if claim, ok := tx.graph.Claims().Claim(id); ok {
		return NotAssertableError{ID: id, What: "a claim", At: claim.Span()}
	}

	return tx.entity(id)
}

// validAssertion runs the check registry's validation over the assertion about
// to be written, returning every problem it finds as one error.
func (tx *Tx) validAssertion(spec AssertionSpec, assertion *Node) error {
	v := &checkValidator{set: tx.checks, registry: tx.graph.Registry()}
	v.assertion(assertion)

	if len(v.refusals) == 0 {
		return nil
	}

	return InvalidAssertionError{Subject: spec.Subject, Check: spec.Check, Errs: slices.Clone(v.refusals)}
}

// asserted is form with the assertion written on it.
//
// It is appended, which decides nothing: canonical form sorts the children of
// every form, so the `assert` child prints where specification sections 6.1
// through 6.4 table it whatever order it was added in.
func asserted(form, assertion *Node) *Node {
	return relisted(form, append(slices.Clone(form.Children), assertion))
}
