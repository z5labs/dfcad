// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"slices"
	"strings"
)

// Rule is one check bound to one thing it constrains: an invariant of the
// thing's type, or an assertion written on the thing itself.
//
// It is the one shape a gate reads. [InvariantBinding] and [AssertionBinding]
// each answer a question about one kind of rule — which invariants bear on this
// instance, which assertions are written on this loop — and a run which has to
// report how many checks there were, run them in a deterministic order and name
// the ones which failed is asking about both at once. Held as two lists it would
// be two counts, two orders and two renderings of a failure, and the two would
// drift.
//
// The two are told apart by [Rule.Invariant] rather than by which list they came
// out of, and a reader acts on them the same way: what failed, which rule, the
// parameters it ran with, and where the rule is written are here whichever kind
// it is.
type Rule struct {
	// Subject is the thing the rule is about, whichever family holds it.
	Subject Entity

	// Form is the family the subject belongs to, which is the axis a check
	// declares first.
	Form SubjectForm

	// Type is the name of the type which declared the rule, and is empty for an
	// assertion: an assertion is written on the thing itself, and
	// [Rule.Declared] already points there.
	Type string

	// Check is what the engine's closed check registry says the named check
	// constrains and takes.
	Check CheckDeclaration

	// Arguments are the parameters the rule was written with, in the order they
	// were written.
	Arguments []Argument

	// Declared is where the rule is written. For an invariant that is a
	// position in a registry file rather than in the file the subject is in;
	// for an assertion it is the assertion, on the thing it constrains.
	Declared Span

	// invariant records which kind of rule this is, rather than leaving it to
	// be inferred from Type being empty: the two are the same today and an
	// inference is the sort of thing which stops being true quietly.
	invariant bool

	// applicable records whether the check can examine the subject, decided
	// where the binding was made rather than here, because the two kinds of
	// rule decide it differently.
	applicable bool

	// graph is the model the check answers about, and runner is the
	// implementation of it — nil for a check which declares itself and cannot
	// yet be run.
	graph  *Graph
	runner Runner
}

// Invariant reports whether the rule is an invariant of the subject's type, as
// against an assertion written on the subject itself.
func (r Rule) Invariant() bool { return r.invariant }

// Runnable reports whether the check has an implementation to run.
//
// A check which declares itself and implements nothing binds, lists and
// validates exactly as one which does, and running it reports nothing. It is
// told apart here rather than by an empty result, because "this rule holds" and
// "nothing has been written to decide whether it holds" are different answers.
func (r Rule) Runnable() bool { return r.runner != nil }

// Applicable reports whether the check can examine the thing the rule is bound
// to.
//
// It is false only for an assertion, and an assertion it is false for is a load
// error rather than a rule which quietly does not apply ([ResolveAssertions]).
// An invariant which could not examine an instance was never bound to it: a type
// declares one rule for instances which differ, so a check about an area not
// reaching the instance which has no shape is ordinary rather than wrong.
func (r Rule) Applicable() bool { return r.applicable }

// Runs reports whether [Rule.Run] would run anything, which is what a listing of
// the rules a gate would exercise reports of each of them.
func (r Rule) Runs() bool { return r.Runnable() && r.Applicable() }

// String renders the rule as the thing it is bound to and the rule as it was
// written.
func (r Rule) String() string {
	written := make([]string, 0, len(r.Arguments)+2)
	if r.Subject != nil {
		written = append(written, string(r.Subject.ID()))
	}
	written = append(written, r.Check.Name)

	for _, argument := range r.Arguments {
		written = append(written, argument.String())
	}

	return strings.Join(written, " ")
}

// Run runs the check against the thing the rule is bound to and returns one
// violation for each way it is not satisfied.
//
// A rule which satisfies its check yields none, and so does one nothing runs:
// [Rule.Runs] is what tells those two apart, and a caller counting how many
// checks passed has to ask it rather than reading the empty result.
func (r Rule) Run() []Violation {
	if !r.Runs() {
		return nil
	}

	subject := CheckSubject{graph: r.graph, subject: r.Subject, arguments: r.Arguments}

	var out []Violation
	for _, failure := range r.runner.Run(subject) {
		out = append(out, r.violation(failure))
	}
	return out
}

// violation attaches the rule — what failed, which rule, the parameters it ran
// with and where that rule is written — to what the check found.
//
// It is one function for both kinds of rule rather than one apiece, because the
// only difference between them is whether a type declared it, and two copies of
// this is two renderings of a failure which have to be kept saying the same
// thing.
func (r Rule) violation(failure Failure) Violation {
	subject := failure.Span
	if subject == (Span{}) && r.Subject != nil {
		subject = r.Subject.Span()
	}

	written := make([]string, 0, len(r.Arguments))
	for _, argument := range r.Arguments {
		written = append(written, argument.String())
	}

	var instance ID
	if r.Subject != nil {
		instance = r.Subject.ID()
	}

	return Violation{
		Instance:  instance,
		Type:      r.Type,
		Check:     r.Check.Name,
		Arguments: written,
		Declared:  r.Declared,
		Subject:   subject,
		Message:   failure.Message,
		Hint:      failure.Hint,
		Related:   failure.Related,
	}
}

// Rules is a set of rules in the order they will be run.
type Rules []Rule

// Rules returns every rule the model holds: each type's invariants bound to each
// of its instances, and each assertion bound to the thing it was written on.
//
// The order is every invariant, in the order [Graph.AllInvariants] binds them,
// and then every assertion, in the order [Graph.AllAssertions] binds them. It is
// deterministic for the reason the load's is: a listing of what would run, and a
// report of what did, have to diff against the last run's. The two families are
// kept whole rather than interleaved per thing because a rule of a type and a
// rule about this patio are read differently — the first is changed in the
// registry, for every instance at once — and a listing which alternated between
// them would have to be sorted before it could be read.
//
// A rule whose check declares itself and implements nothing is here like any
// other. It binds, it lists, and running it reports nothing; see [Rule.Runs].
func (g *Graph) Rules() Rules { return g.rules(registeredChecks) }

// rules is [Graph.Rules] against a given set of checks, which is what lets the
// engine's closed registry and a set assembled for a test be the same thing
// exercised the same way.
func (g *Graph) rules(set *checkSet) Rules {
	var out Rules

	for binding := range g.allInvariants(set) {
		out = append(out, Rule{
			Subject:    binding.Instance,
			Form:       SubjectNode,
			Type:       binding.Type,
			Check:      binding.Check,
			Arguments:  binding.Arguments,
			Declared:   binding.Declared.Span,
			invariant:  true,
			applicable: true,
			graph:      g,
			runner:     binding.runner,
		})
	}

	for binding := range g.allAssertions(set) {
		out = append(out, Rule{
			Subject:    binding.Subject,
			Form:       binding.Form,
			Check:      binding.Check,
			Arguments:  binding.Arguments,
			Declared:   binding.Declared.Span,
			applicable: binding.Applicable(),
			graph:      g,
			runner:     binding.runner,
		})
	}

	return out
}

// invariants is the rules of a type, which is what [Graph.CheckInvariants]
// reports on. It is unexported because the two kinds are one thing to run and
// one thing to report, and a caller splitting them again would be undoing what
// [Rule] is for; the two are still told apart by [Rule.Invariant].
func (rs Rules) invariants() Rules {
	return rs.kind(true)
}

// assertions is the rules written on a thing, which is what
// [Graph.CheckAssertions] reports on.
func (rs Rules) assertions() Rules {
	return rs.kind(false)
}

// kind is the rules of one of the two kinds, in the order they were in.
func (rs Rules) kind(invariant bool) Rules {
	out := make(Rules, 0, len(rs))
	for _, rule := range rs {
		if rule.Invariant() == invariant {
			out = append(out, rule)
		}
	}
	return out
}

// Select returns the rules which satisfy the filter, in the order they were in.
func (rs Rules) Select(filter RuleFilter) Rules {
	out := make(Rules, 0, len(rs))
	for _, rule := range rs {
		if filter.Matches(rule) {
			out = append(out, rule)
		}
	}
	return out
}

// Run runs every rule and reports what it found.
//
// Violations come back in the order the rules were in, and within one rule in
// the order the check reported them. Collecting them into a [Diagnostics] is
// what puts them in reporting order, which is by position.
func (rs Rules) Run() CheckRun {
	out := CheckRun{Rules: len(rs)}

	for _, rule := range rs {
		if !rule.Runs() {
			continue
		}

		out.Ran++

		violations := rule.Run()
		if len(violations) == 0 {
			out.Passed++
			continue
		}

		out.Failed++
		out.Violations = append(out.Violations, violations...)
	}

	return out
}

// CheckRun is what running a set of rules found.
//
// The counts are of rules rather than of violations, because a rule is what
// passes or fails: one loop which does not close and one which closes the wrong
// way are two ways of failing the same check, and a summary which counted them
// as two failures would say the model breaks two rules.
type CheckRun struct {
	// Rules is how many rules were run over, which is not how many ran.
	Rules int `json:"rules"`

	// Ran is how many of them ran: those whose check has an implementation and
	// can examine the thing it is bound to. Rules minus Ran is how many were
	// bound and decided nothing.
	Ran int `json:"ran"`

	// Passed is how many of the ones which ran were satisfied.
	Passed int `json:"passed"`

	// Failed is how many were not, which is how many rules the violations below
	// are about.
	Failed int `json:"failed"`

	// Violations is one entry per way a rule was not satisfied, in the order
	// the rules were run.
	Violations []Violation `json:"violations,omitempty"`
}

// RuleFilter narrows a run to a subset of the rules a model holds.
//
// A filter left empty matches everything, and filters combine: a rule is
// selected when it satisfies every filter which was given. Within one filter the
// values are alternatives, so two subjects means the rules of either.
//
// The point of narrowing is a gate somebody is iterating against — one room, one
// type, one check they have just written — and the answers have to compose the
// way a reader expects, which is that each filter can only take rules away.
type RuleFilter struct {
	// Subjects are the things the rule may be bound to, by id. A subject is any
	// of the four families, because an assertion is written on any of them.
	Subjects []ID

	// Types are the type names an instance may declare. It selects the rules
	// bound to instances of those types, whether the rule is an invariant of
	// the type or an assertion written on one of its instances; a vertex, an
	// edge and a loop declare no type and are selected by none of them.
	Types []string

	// Checks are the check names a rule may name.
	Checks []string
}

// Matches reports whether the rule satisfies every filter which was given.
func (f RuleFilter) Matches(rule Rule) bool {
	if len(f.Subjects) > 0 {
		if rule.Subject == nil || !slices.Contains(f.Subjects, rule.Subject.ID()) {
			return false
		}
	}

	if len(f.Types) > 0 && !slices.Contains(f.Types, declaredType(rule.Subject)) {
		return false
	}

	if len(f.Checks) > 0 && !slices.Contains(f.Checks, rule.Check.Name) {
		return false
	}

	return true
}

// declaredType is the type the rule's subject declares, and is empty for a
// subject which declares none.
//
// It is read from the subject rather than from [Rule.Type] because that field is
// the type which *declared* the rule, which an assertion has none of: filtering
// by type would then reach an instance's invariants and not the assertions
// written on the same instance, which is not a subset anybody asked for.
func declaredType(subject Entity) string {
	node, ok := subject.(*SemanticNode)
	if !ok || node == nil {
		return ""
	}
	return node.Type()
}
