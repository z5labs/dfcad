// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"iter"
	"slices"
	"strings"

	sexpr "github.com/z5labs/sexpr-go"
)

// assertChild is the tag an assertion is written with, on any of the four forms
// which carry one.
const assertChild = "assert"

// Assertion is one check written on one thing, per specification section 6.8.
//
// An assertion is a check name and its parameters and nothing else. There is no
// expression language, so what an assertion constrains is readable from what is
// written here — the name resolves in the engine's closed check registry and the
// registry says what each parameter means
// ([0011](docs/decisions/0011-assertions-are-named-parameterised-checks.md)).
//
// It is written on the thing it constrains, which is what tells it apart from
// the [Invariant] of a type: an invariant is one rule about every instance of a
// type, and an assertion is one rule about this patio. Neither is a place to
// record what something *is*; see [ResolveAssertions] for the rule which keeps
// an assertion from becoming one.
//
// The check name and the parameters are the check registry's vocabulary rather
// than this layer's, so what is recorded is what was written and where.
type Assertion struct {
	// Check is the check name written after the assert tag.
	Check string

	// Parameters are the parameter forms written after it, unmodified.
	Parameters []*Node

	// Span is where the assertion was written, which is inside the form of the
	// thing it constrains.
	Span Span
}

// Arguments reads the parameters the assertion was written with.
func (a Assertion) Arguments() []Argument { return arguments(a.Parameters) }

// String renders the assertion as the check name followed by the parameters it
// was written with.
func (a Assertion) String() string {
	written := make([]string, 0, len(a.Parameters)+1)
	written = append(written, a.Check)

	for _, argument := range a.Arguments() {
		written = append(written, argument.String())
	}

	return strings.Join(written, " ")
}

// clone copies the assertion so that a caller reading the ones written on a
// thing cannot write to them.
func (a Assertion) clone() Assertion {
	a.Parameters = slices.Clone(a.Parameters)
	return a
}

// cloneAssertions copies a slice of assertions, which is what every accessor
// returning the ones written on a thing hands back.
func cloneAssertions(written []Assertion) []Assertion {
	out := make([]Assertion, 0, len(written))
	for _, assertion := range written {
		out = append(out, assertion.clone())
	}
	return out
}

// assertions reads the assertions written on one form, reporting what the check
// registry makes of each.
//
// The check name and the parameters are validated here, against the registry,
// because this is where the written form is: an unregistered name, a parameter
// the check does not take, a missing one, and a value which is not the sort of
// datum the check declares are each a diagnostic before anything has run
// ([ValidateAssertion]). What is *not* asked here is anything about the model —
// whether an id resolves, and whether the assertion restates a claim — because
// one loader has read one family and neither question is answerable from it.
// [ResolveAssertions] is the pass which has read the whole model.
//
// An assertion whose check nothing registers is still carried. It is a
// diagnostic and not a reason to lose what somebody wrote: a listing of what
// constrains a thing which quietly dropped the misspelled one would read as
// though it were not there.
func (r *reader) assertions(form *Node, registry *Registry, set *checkSet) []Assertion {
	written := childForms(form, assertChild)
	if len(written) == 0 {
		return nil
	}

	out := make([]Assertion, 0, len(written))
	for _, child := range written {
		r.add(validateAssertion(child, registry, set)...)

		// The name is read without reporting, because the validation above has
		// already reported an assertion which names no check.
		arg, ok := argument(child, 0)
		if !ok {
			continue
		}

		name, ok := arg.Datum.(sexpr.Symbol)
		if !ok {
			continue
		}

		_, parameters := split(elements(child))
		out = append(out, Assertion{
			Check:      name.Value,
			Parameters: parameters,
			Span:       child.Span,
		})
	}

	return out
}

// AssertionBinding is one assertion together with what the check registry says
// the check it names constrains and takes.
//
// It is the assertion as something above the format can act on: the written
// form resolved against the closed registry, with the parameters read as
// arguments and the implementation attached where there is one. Nothing is
// stored on the subject, so an assertion is bound from what was written every
// time it is asked for.
type AssertionBinding struct {
	// Subject is the thing the assertion was written on, whichever family holds
	// it.
	Subject Entity

	// Form is the form it was written on, which is the axis a check declares
	// first.
	Form SubjectForm

	// Check is what the engine's closed check registry says the named check
	// constrains and takes.
	Check CheckDeclaration

	// Declared is the assertion as it was written. Its span is the file and
	// line a violation points back at.
	Declared Assertion

	// Arguments are the parameters it was written with.
	Arguments []Argument

	// runner is the implementation of the check, and is nil for a check which
	// declares itself and cannot yet be run.
	runner Runner
}

// Runnable reports whether the check has an implementation to run.
//
// A check which declares itself and implements nothing binds, lists and
// validates exactly as one which does, and running it reports nothing.
func (b AssertionBinding) Runnable() bool { return b.runner != nil }

// Applicable reports whether the check can examine the thing the assertion was
// written on.
//
// One which cannot is a load error rather than a filter, which is what tells it
// apart from an invariant: an invariant is declared once for a type whose
// instances differ, so a check which cannot examine this instance is ordinary,
// while an assertion is written on one thing by somebody looking at it.
func (b AssertionBinding) Applicable() bool { return examinesSubject(b.Check, b.Subject, b.Form) }

// Argument returns the parameter written under name, and whether one was.
func (b AssertionBinding) Argument(name string) (Argument, bool) {
	for _, argument := range b.Arguments {
		if argument.Name == name {
			return argument.clone(), true
		}
	}
	return Argument{}, false
}

// String renders the binding as the thing it was written on and the assertion
// as it was written.
func (b AssertionBinding) String() string {
	written := make([]string, 0, len(b.Arguments)+2)
	if b.Subject != nil {
		written = append(written, string(b.Subject.ID()))
	}
	written = append(written, b.Declared.Check)

	for _, argument := range b.Arguments {
		written = append(written, argument.String())
	}

	return strings.Join(written, " ")
}

// writtenAssertions returns the assertions one thing carries and the form they
// were written on, and whether it is a thing which carries any at all.
//
// All four forms carry assertions, and which form a thing is is the first axis
// a check declares, so the two come back together: a caller which had to ask
// the family twice would be reading the same type switch in two places.
func writtenAssertions(subject Entity) ([]Assertion, SubjectForm, bool) {
	switch found := subject.(type) {
	case *SemanticNode:
		return found.assertions, SubjectNode, true
	case *Vertex:
		return found.assertions, SubjectVertex, true
	case *Edge:
		return found.assertions, SubjectEdge, true
	case *Loop:
		return found.assertions, SubjectLoop, true
	}
	return nil, "", false
}

// examinesSubject reports whether a check can examine the thing an assertion
// was written on.
//
// The kind and the geometry form are axes of the semantic family alone, so a
// subject of the geometric family is judged on its form and on nothing else. A
// check which declares a kind declares [SubjectNode] beside it — the registry
// refuses one which does not — so nothing is waved through here that a node
// would have been held to.
func examinesSubject(check CheckDeclaration, subject Entity, form SubjectForm) bool {
	if !check.PermitsForm(form) {
		return false
	}

	node, ok := subject.(*SemanticNode)
	if !ok {
		return true
	}
	return examines(check, node)
}

// Assertions returns the assertions written on one thing, bound to what the
// check registry says each check constrains, in the order they were written.
//
// It is what a caller showing a thing shows beside its claims: the claims are
// what is known about it and these are what must hold of it. [Graph.Entity] is
// how the thing is found, and either family answers.
//
// An assertion naming a check nothing registers is not among them. That is a
// load error rather than a rule, and there is no declaration to bind it to;
// [SemanticNode.Assertions] and its siblings are the assertions as written,
// which is what a listing that wants to show the misspelled one reads.
func (g *Graph) Assertions(subject Entity) []AssertionBinding {
	return g.assertions(subject, registeredChecks)
}

// assertions is [Graph.Assertions] against a given set of checks, which is what
// lets the engine's closed registry and a set assembled for a test be the same
// thing exercised the same way.
func (g *Graph) assertions(subject Entity, set *checkSet) []AssertionBinding {
	if g == nil || subject == nil {
		return nil
	}

	written, form, ok := writtenAssertions(subject)
	if !ok {
		return nil
	}

	out := make([]AssertionBinding, 0, len(written))
	for _, assertion := range written {
		check, ok := set.lookup(assertion.Check)
		if !ok {
			continue
		}

		out = append(out, AssertionBinding{
			Subject:   subject,
			Form:      form,
			Check:     check,
			Declared:  assertion.clone(),
			Arguments: assertion.Arguments(),
			runner:    set.runner(assertion.Check),
		})
	}

	return out
}

// AllAssertions iterates every assertion of the model, family by family in the
// order [Graph.Entity] looks them up and, within one thing, in the order they
// were written.
//
// The order is deterministic for the reason the load's is: a listing of what
// would run, and a report of what did, have to diff against the last run's.
func (g *Graph) AllAssertions() iter.Seq[AssertionBinding] {
	return g.allAssertions(registeredChecks)
}

// allAssertions is [Graph.AllAssertions] against a given set of checks.
func (g *Graph) allAssertions(set *checkSet) iter.Seq[AssertionBinding] {
	return func(yield func(AssertionBinding) bool) {
		if g == nil {
			return
		}

		for entity := range g.entities() {
			for _, binding := range g.assertions(entity, set) {
				if !yield(binding) {
					return
				}
			}
		}
	}
}

// CheckAssertions runs every assertion the model writes and returns one
// violation for each way one of them is not satisfied.
//
// A model whose things carry no assertion, and one whose assertions all hold,
// both yield none. An assertion naming a check which cannot examine the thing it
// was written on is not run: that is a load error ([ResolveAssertions]), and
// running it would be answering a question about a subject the check has nothing
// to look at on.
//
// Violations come back in the order [Graph.AllAssertions] bound them, and within
// one binding in the order the check reported them. Collecting them into a
// [Diagnostics] is what puts them in reporting order, which is by position.
func (g *Graph) CheckAssertions() []Violation {
	return g.checkAssertions(registeredChecks)
}

// checkAssertions is [Graph.CheckAssertions] against a given set of checks.
//
// It is the assertions of [Graph.rules] run, rather than a run of its own, for
// the reason [Graph.checkInvariants] is.
func (g *Graph) checkAssertions(set *checkSet) []Violation {
	return g.rules(set).assertions().Run().Violations
}

// ResolveAssertions checks every assertion of a loaded model against the model
// it was written in, returning one diagnostic for every problem it finds.
//
// It is the half of validating an assertion which needs the whole model. The
// other half is the check registry's and runs as each file is read: the name
// resolves, the parameters are ones the check takes, and each value is the sort
// of datum it declares ([ValidateAssertion]). Three questions are left, and none
// of them can be answered by a pass which has read one file or one family.
//
// # The check has to be able to examine the subject
//
// A check declares what it applies to on three axes — the form, the node kind
// and the geometry form — and an assertion written on something it cannot
// examine is refused. `edge-endpoints-differ` on a space is not a rule which
// happens not to fire; it is a rule which passes on every model forever, and
// the only moment the mistake is visible is the one where the check and the
// thing are both in front of the reader.
//
// # Every id it names has to resolve
//
// A parameter may name another thing — the setback this patio must stay inside,
// the element that door has to reach — and every id written in one is checked to
// name something the model holds, with a diagnostic naming both ends when one
// does not.
//
// # An assertion constrains; it does not record
//
// **An assertion which restates a value the claims already carry is refused.**
// A claim is where a value is recorded, with the source, the method, the date
// and the accuracy it came from, and an assertion repeating that value is a
// second source of truth for one quantity — one which goes on saying what it
// says the day the claim is superseded, and which no resolution rule reaches
// because it is not a claim.
//
// The rule reads the check's own declaration rather than guessing from the
// shape of what was written. A check declares which parameter names the
// predicate, and — where it has one — which parameter carries a value of that
// quantity ([CheckParameter.Restates]). An assertion writing both, on a subject
// which already claims that predicate, is the claim written twice and is
// refused.
//
// What it catches, given a check declaring `(predicate …)` and a restating
// `(is …)`:
//
//	; Refused: site:S-101 already claims width, and this says it again.
//	(node site:S-101
//	  (width (value 3.6 m) (source "…") (method method:…) (date "2026-03-02"))
//	  (assert claimed-value-is (predicate width) (is 3.6)))
//
// What it does not catch, and deliberately:
//
//   - A bound, a range or a relation. `(assert clearance-at-least (predicate
//     clearance) (minimum 0.9))` names a predicate the subject claims and
//     constrains it; the check declares `minimum` as a bound rather than as a
//     value of the subject's, so nothing is restated.
//   - A check with no value at all. `(assert required-claim (predicate width))`
//     says a claim must be there and does not say what it is.
//   - The same pair on a subject which claims nothing under that predicate.
//     There is no first statement for it to be the second of, and the assertion
//     is a constraint on a value which is not there yet.
//   - A value under a *different* predicate of the same subject. Two predicates
//     are two quantities, and the rule is about one.
//
// A model with no assertions yields nothing. Diagnostics come back in the order
// the pass found them, which is [Graph.AllAssertions]'s; collecting them into a
// [Diagnostics] is what puts them in reporting order.
func ResolveAssertions(graph *Graph) []Diagnostic {
	return resolveAssertions(graph, registeredChecks)
}

// resolveAssertions is [ResolveAssertions] against a given set of checks.
func resolveAssertions(graph *Graph, set *checkSet) []Diagnostic {
	if graph == nil {
		return nil
	}

	r := &assertionResolver{graph: graph}
	for binding := range graph.allAssertions(set) {
		r.applicable(binding)
		r.references(binding)
		r.restatement(binding)
	}

	return r.diags
}

// assertionResolver collects the diagnostics of one pass over a model's
// assertions.
type assertionResolver struct {
	reader

	// graph is the model the assertions are resolved against. It is not written
	// to.
	graph *Graph
}

// applicable reports an assertion naming a check which cannot examine the thing
// it was written on.
func (r *assertionResolver) applicable(binding AssertionBinding) {
	message, hint, ok := unexaminable(binding.Check, binding.Subject, binding.Form)
	if !ok {
		return
	}

	r.add(Diagnostic{
		Severity: SeverityError,
		Span:     binding.Declared.Span,
		Message:  message,
		Hint:     hint,
		Related: []RelatedLocation{{
			Span:    binding.Subject.Span(),
			Message: fmt.Sprintf("%s is written here", subjectName(binding)),
		}},
	})
}

// subjectName is the thing an assertion was written on as a diagnostic names it.
//
// It is the id, and the form it was written on where there is no id to use. A
// thing whose id could not be read is a thing the model holds with a diagnostic
// against it, and it carries assertions like any other, so a message built from
// its id alone reads with a hole in it — "the assertion on  names" — at exactly
// the moment somebody is reading two diagnostics about one form.
func subjectName(binding AssertionBinding) string {
	if id := binding.Subject.ID(); id != "" {
		return string(id)
	}
	return "the " + string(binding.Form)
}

// unexaminable says why a check cannot examine the thing an assertion was
// written on, and whether it cannot.
//
// The three axes are asked in the order a reader would, and only the first
// mismatch is reported: they are one mistake, and the second reading of it says
// nothing the first did not.
func unexaminable(check CheckDeclaration, subject Entity, form SubjectForm) (message string, hint string, ok bool) {
	if !check.PermitsForm(form) {
		return fmt.Sprintf(
				"expected an assertion naming a check which applies to %s %s, found %s, which applies to %s",
				article(string(form)), form, check.Name, join(spellings(check.Forms), "and"),
			),
			fmt.Sprintf(
				"an assertion is written on the thing the check examines, so %s is written on %s instead",
				check.Name, join(spellings(check.Forms), "or"),
			),
			true
	}

	node, isNode := subject.(*SemanticNode)
	if !isNode {
		return "", "", false
	}

	if !check.PermitsKind(node.Kind()) {
		return fmt.Sprintf(
				"expected an assertion naming a check which applies to a %s, found %s, which applies to %s",
				node.Kind(), check.Name, join(spellings(check.Kinds), "and"),
			),
			fmt.Sprintf(
				"%s is a %s, so this check has nothing on it to examine; an assertion which applies to nothing does "+
					"not fail, it passes on every run forever",
				node.ID(), node.Kind(),
			),
			true
	}

	geometry, has := node.Geometry()
	if !check.PermitsGeometry(geometry) {
		written := fmt.Sprintf("a %s", geometry)
		if !has {
			written = "no geometry at all"
		}

		return fmt.Sprintf(
				"expected an assertion naming a check which applies to the geometry %s has, found %s, which applies to %s",
				node.ID(), check.Name, join(spellings(check.Geometries), "and"),
			),
			fmt.Sprintf(
				"%s has %s, so this check has nothing on it to measure; an assertion which applies to nothing does "+
					"not fail, it passes on every run forever",
				node.ID(), written,
			),
			true
	}

	return "", "", false
}

// references reports every id an assertion names which nothing in the model
// answers to.
//
// Only the parameters the check declares as ids are read. A tolerance, a
// predicate, a type and a frame are names resolved against the registry when
// the file was read, and an id written where one of those belongs is already
// reported as that.
func (r *assertionResolver) references(binding AssertionBinding) {
	for _, argument := range binding.Arguments {
		declared, ok := binding.Check.Parameter(argument.Name)
		if !ok || declared.Type != ParameterID {
			continue
		}

		for _, value := range parameterValues(argument, declared) {
			symbol, ok := value.Datum.(sexpr.Symbol)
			if !ok {
				// A value which is not a symbol is not an id, and the check
				// registry has already reported it as whatever it is.
				continue
			}

			id := ID(symbol.Value)
			if id == "" || r.resolves(id) {
				continue
			}

			r.add(r.dangling(binding, declared, id, value.Span))
		}
	}
}

// resolves reports whether an id names something the model holds.
//
// A claim id answers as well as an entity's. Ids are unique across the whole
// model and claims share the space with nodes (specification section 6.9), so
// an assertion naming the claim it is about names something which is there, and
// reporting it as dangling would be this pass looking in one of the two places
// an id can be.
func (r *assertionResolver) resolves(id ID) bool {
	if _, ok := r.graph.Entity(id); ok {
		return true
	}

	_, ok := r.graph.Claims().Claim(id)
	return ok
}

// dangling is the diagnostic for an id an assertion names and nothing answers
// to.
//
// It names both ends — the thing the assertion is written on and the id which
// reaches nothing — because either of them may be the mistake, and which one it
// is is a judgement only whoever wrote it can make.
func (r *assertionResolver) dangling(binding AssertionBinding, declared CheckParameter, id ID, at Span) Diagnostic {
	hint := fmt.Sprintf(
		"an assertion may name another thing, and every id it names has to resolve; %s is what %s takes: %s",
		declared.Type.spelling(), declared.Name, declared.Description,
	)
	if near, ok := r.graph.Nearest(id); ok {
		hint = fmt.Sprintf("did you mean %s?", near)
	}

	return Diagnostic{
		Severity: SeverityError,
		Span:     at,
		Message: fmt.Sprintf(
			"expected the (%s ...) parameter of the assertion on %s to name something the model holds, found %s, "+
				"which nothing answers to",
			declared.Name, subjectName(binding), id,
		),
		Hint: hint,
		Related: []RelatedLocation{{
			Span:    binding.Subject.Span(),
			Message: fmt.Sprintf("%s is written here", subjectName(binding)),
		}},
	}
}

// restatement reports an assertion which says again what a claim on the same
// subject already says. The rule, and what it does and does not catch, is
// [ResolveAssertions].
func (r *assertionResolver) restatement(binding AssertionBinding) {
	predicate, ok := binding.Check.predicateParameter()
	if !ok {
		return
	}

	value, ok := binding.Check.restatingParameter()
	if !ok {
		return
	}

	named, ok := binding.Argument(predicate.Name)
	if !ok {
		return
	}

	restated, ok := binding.Argument(value.Name)
	if !ok {
		// The check compares against a value and none was written. Whether that
		// is allowed is the parameter's Required, which the check registry has
		// already answered.
		return
	}

	name, ok := named.Symbol()
	if !ok {
		return
	}

	for claim := range r.graph.Claims().Under(binding.Subject.ID(), name) {
		r.add(Diagnostic{
			Severity: SeverityError,
			Span:     restated.Span,
			Message: fmt.Sprintf(
				"expected an assertion which constrains %s, found one which restates the %s it already claims",
				subjectName(binding), name,
			),
			Hint: fmt.Sprintf(
				"an assertion constrains; it does not record. The claim is where %s is written, with the source, "+
					"the method and the date it came from, and %s is a second answer to the same question — one "+
					"which goes on saying what it says the day the claim is superseded. Constrain the value instead, "+
					"or change the claim.",
				name, restated,
			),
			Related: []RelatedLocation{{
				Span:    claim.Span(),
				Message: fmt.Sprintf("the claim under %s is written here", name),
			}},
		})
		return
	}
}

// written are the values of one parameter as the check reads them.
//
// A repeated parameter is written either as a sequence of values after its tag
// or as one parenthesised list of them, and specification section 6.8 permits
// both, so a pass which read only one spelling would report half the ids in a
// file it accepted.
func parameterValues(argument Argument, declared CheckParameter) []*Node {
	values := argument.Values

	if declared.Repeated && len(values) == 1 {
		if list, ok := values[0].Datum.(sexpr.List); ok && list.Tail == nil {
			return values[0].Children
		}
	}

	return values
}
