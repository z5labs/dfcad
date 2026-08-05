// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/z5labs/dfcad"
)

const checkUsage = `dfcad check — run every rule the model states and say whether it holds.

Usage:

	dfcad check [flags]

Every invariant each type declares, bound to each of its instances, and every
assertion written on a thing, run against the loaded model. It is the gate: one
command, one exit code, and a report naming what failed and where the rule which
failed it is written.

Flags:

	--subject <id>   only the rules bound to this thing; repeat for more
	--type <name>    only the rules bound to instances of this type; repeat
	--check <name>   only the rules naming this check; repeat
	--list           write what would run and run none of it

Filters combine: a rule is selected when it satisfies every filter given, and a
filter written more than once is satisfied by any of its values. A name no model
holds — an id nothing answers to, a type no registry file declares, a check the
engine does not register — is a usage error rather than an empty run, because a
gate which passed on a misspelled filter would pass on nothing having run at all.

A vertex, an edge and a loop declare no type, so --type never selects a rule
written on one. --subject takes the id of any of the four, because an assertion
is written on any of them, and that is why it is not spelled --node.

Rules run in a deterministic order and are reported in it: every invariant, node
by node in the order the model was read, and then every assertion, thing by
thing. Two runs over one model produce byte-identical stdout, so a diff between
them is about what changed in the model.

A check which declares itself and has no implementation is bound, listed and run
over, and decides nothing: it is counted apart from the ones which ran, because
"this rule holds" and "nothing has been written to decide whether it holds" are
different answers. --list is where that is read before a run rather than after.

How long the run took is written to stderr — with the summary under
--format human, and on its own under -v in any format. It is not on stdout: the
same input has to produce the same bytes there, and a duration is the one thing
which never does.

A model which states no rule runs nothing and succeeds. Nothing to check is not
a failure. A model root which holds no model at all is a different answer: it is
reported as a load failure, so that a --root with a character wrong cannot pass
the gate by having nothing in it.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object check writes carries "summary" — how many rules there were, how many
would run, how many ran, and how many passed and failed — and "violations": one
entry per way a rule was not satisfied. Asked to --list, it carries "checks" as
well, one entry per rule in the order it would run in, and nothing ran so
"violations" is empty.
`

// UnknownCheckError is a --check which names no check the engine registers.
//
// The registered set is listed rather than pointed at, because it is closed and
// compiled in: there is no command which would print a longer list, and a caller
// told only that its name was wrong has nowhere to look it up.
type UnknownCheckError struct {
	// Check is what was asked for.
	Check string

	// Known is every check the engine registers, in name order.
	Known []string
}

// Error implements [error].
func (e UnknownCheckError) Error() string {
	return fmt.Sprintf("unknown check %s: want one of %s", e.Check, strings.Join(e.Known, ", "))
}

// checkResult is the object check writes to stdout.
type checkResult struct {
	envelope

	// Summary is how many rules there were and how they went.
	Summary checkSummary `json:"summary"`

	// Checks is one entry per rule the filters selected, in the order it would
	// run in, and is written only for --list. A run reports what failed rather
	// than what there was: a model with a rule per room would otherwise answer
	// every gate with a listing of itself.
	Checks []listedCheck `json:"checks,omitempty"`

	// Violations is one entry per way a rule was not satisfied, in the order
	// the rules ran. Empty rather than null when nothing failed.
	Violations []dfcad.Violation `json:"violations"`
}

// checkSummary is how many rules a run covered and what became of them.
//
// The counts are of rules rather than of violations, because a rule is what
// passes or fails: one loop which does not close and one which closes the wrong
// way are two ways of failing one check, and a summary counting them as two
// failures would say the model breaks two rules.
type checkSummary struct {
	// Checks is how many rules the filters selected.
	Checks int `json:"checks"`

	// Runnable is how many of them would run: those whose check has an
	// implementation and can examine the thing it is bound to. Checks minus
	// Runnable is how many are bound and decide nothing.
	Runnable int `json:"runnable"`

	// Ran is how many actually ran, which is Runnable for a run and zero for a
	// --list. The two are separate fields so that a listing cannot be read as a
	// run in which every check passed.
	Ran int `json:"ran"`

	// Passed is how many of the ones which ran were satisfied.
	Passed int `json:"passed"`

	// Failed is how many were not, which is how many rules the violations are
	// about.
	Failed int `json:"failed"`
}

// listedCheck is one rule as --list reports it.
type listedCheck struct {
	// Subject is the id of the thing the rule is bound to.
	Subject string `json:"subject"`

	// Form is which family the subject belongs to: node, vertex, edge or loop.
	Form string `json:"form"`

	// Rule is which kind of rule it is: an invariant of the subject's type, or
	// an assertion written on the subject itself.
	Rule string `json:"rule"`

	// Type is the type which declared it, and is absent for an assertion, which
	// is declared on the thing itself.
	Type string `json:"type,omitempty"`

	// Check is the check name it names.
	Check string `json:"check"`

	// Arguments are the parameters it would run with, each rendered as it was
	// written.
	Arguments []string `json:"arguments,omitempty"`

	// Runs reports whether running it would decide anything. It is false both
	// for a check which declares itself and has no implementation and for one
	// which cannot examine the thing it is bound to, which is why Applicable is
	// written beside it rather than left to be inferred from this.
	Runs bool `json:"runs"`

	// Applicable reports whether the check can examine the thing the rule is
	// bound to.
	//
	// It is false only for an assertion, and only in a model the load refused:
	// a check written on something it cannot look at is a load error rather
	// than a rule which quietly never fires, and an invariant which could not
	// examine an instance was never bound to it. It is reported so that a rule
	// which decides nothing says which of the two reasons it is, in a model
	// which is being fixed and therefore has both.
	Applicable bool `json:"applicable"`

	// Declared is where the rule is written: a registry file for an invariant,
	// and the thing itself for an assertion.
	Declared dfcad.Span `json:"declared"`
}

// The two kinds of rule, as the listing spells them.
const (
	// ruleInvariant is a rule of the subject's type, which is stated once and
	// applies to every instance of it.
	ruleInvariant = "invariant"

	// ruleAssertion is a rule written on the subject itself.
	ruleAssertion = "assertion"
)

// runCheck is the check command.
func runCheck(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	subjects := &repeated{}
	types := &repeated{}
	checks := &repeated{}

	flags.Var(subjects, "subject", "")
	flags.Var(types, "type", "")
	flags.Var(checks, "check", "")
	list := flags.Bool("list", false, "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(arguments) > 0 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments}, stderr, true)
	}

	// The model is loaded before the filters are checked because the model is
	// what says whether a type is declared and whether an id names anything.
	// Its diagnostics reach stderr either way, so a filter which is unknown
	// because a registry file did not parse is reported beside the reason.
	graph, refused := loadGate(cmd, globals, stderr)

	filter, err := ruleFilter(graph, *subjects, *types, *checks)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	// The clock starts at the binding rather than at the run, because binding a
	// type's rule to each of its instances is part of what a check set costs: a
	// registry which grew a rule per type is slower here and nowhere else.
	started := time.Now()

	rules := graph.Rules().Select(filter)

	result := checkResult{
		envelope:   newEnvelope(cmd.name),
		Summary:    checkSummary{Checks: len(rules)},
		Violations: make([]dfcad.Violation, 0),
	}

	for _, rule := range rules {
		if rule.Runs() {
			result.Summary.Runnable++
		}
	}

	switch {
	case *list:
		result.Checks = listChecks(rules)
	default:
		run := rules.Run()

		result.Summary.Ran = run.Ran
		result.Summary.Passed = run.Passed
		result.Summary.Failed = run.Failed
		result.Violations = append(result.Violations, run.Violations...)
	}
	elapsed := time.Since(started)

	// A violation is a problem in something somebody wrote, so it is rendered
	// for whoever wrote it on every run and in every format. The struct above
	// is the machine form of the same finding; neither is produced by parsing
	// the other.
	render(diagnose(result.Violations), stderr)

	reportCheck(result, *list, elapsed, globals, stderr)

	if err := emit(stdout, result); err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return checkCode(result.Summary, refused)
}

// checkCode is the exit code of a run.
//
// A model which could not be read outranks a rule which was not satisfied, for
// the reason it does in fmt: a gate which reported the rules it managed to run
// over half a model would be answering a question nobody asked. It also covers
// the model root which holds no model at all — a --root with a character wrong
// is a load failure and not a model with nothing to check.
func checkCode(summary checkSummary, refused bool) int {
	switch {
	case refused:
		return exitLoad
	case summary.Failed > 0:
		return exitCheck
	default:
		return exitSuccess
	}
}

// ruleFilter is the filter the flags ask for, or the first name which names
// nothing.
//
// An unknown name is a usage error rather than an empty run. A rule nobody wrote
// and a filter nobody spelled right are different answers, and a gate which
// cannot tell them apart is one which reports a model sound because it checked
// nothing.
func ruleFilter(graph *dfcad.Graph, subjects, types, checks []string) (dfcad.RuleFilter, error) {
	filter := dfcad.RuleFilter{Types: types, Checks: checks}

	for _, subject := range subjects {
		id, err := dfcad.ParseID(subject)
		if err != nil {
			return dfcad.RuleFilter{}, err
		}

		if _, ok := graph.Entity(id); !ok {
			nearest, _ := graph.Nearest(id)
			return dfcad.RuleFilter{}, UnknownIDError{ID: string(id), Nearest: string(nearest)}
		}

		filter.Subjects = append(filter.Subjects, id)
	}

	registry := graph.Registry()
	for _, declared := range types {
		if !registry.Declares(dfcad.SortType, declared) {
			return dfcad.RuleFilter{}, UnknownTypeError{Type: declared, Declared: registry.Names(dfcad.SortType)}
		}
	}

	for _, check := range checks {
		if _, ok := dfcad.LookupCheck(check); !ok {
			return dfcad.RuleFilter{}, UnknownCheckError{Check: check, Known: checkNames()}
		}
	}

	return filter, nil
}

// checkNames is every check the engine registers, in name order.
func checkNames() []string {
	declared := dfcad.Checks()

	out := make([]string, 0, len(declared))
	for _, check := range declared {
		out = append(out, check.Name)
	}
	slices.Sort(out)

	return out
}

// listChecks is the rules as --list reports them.
func listChecks(rules dfcad.Rules) []listedCheck {
	out := make([]listedCheck, 0, len(rules))

	for _, rule := range rules {
		kind := ruleAssertion
		if rule.Invariant() {
			kind = ruleInvariant
		}

		arguments := make([]string, 0, len(rule.Arguments))
		for _, argument := range rule.Arguments {
			arguments = append(arguments, argument.String())
		}

		entry := listedCheck{
			Form:       string(rule.Form),
			Rule:       kind,
			Type:       rule.Type,
			Check:      rule.Check.Name,
			Arguments:  arguments,
			Runs:       rule.Runs(),
			Applicable: rule.Applicable(),
			Declared:   rule.Declared,
		}
		if rule.Subject != nil {
			entry.Subject = string(rule.Subject.ID())
		}

		out = append(out, entry)
	}

	return out
}

// diagnose is the violations as diagnostics, which is the rendering for whoever
// wrote the model.
func diagnose(violations []dfcad.Violation) []dfcad.Diagnostic {
	out := make([]dfcad.Diagnostic, 0, len(violations))
	for _, violation := range violations {
		out = append(out, violation.Diagnostic())
	}
	return out
}

// reportCheck renders a run for a person, on stderr.
//
// Nothing here reaches stdout, in any format and at any verbosity: stdout is the
// same bytes whether or not anybody asked to read the run.
func reportCheck(result checkResult, list bool, elapsed time.Duration, globals *globals, stderr io.Writer) {
	// How long it took is progress rather than result — a result is the same on
	// every run over one model and a duration is not — so it is reported the two
	// ways progress is: with the summary a person asked for, and on its own when
	// a run was asked to say what it was doing.
	if globals.Verbosity >= verbosityProgress && !globals.human() {
		fmt.Fprintf(stderr, "dfcad check: %s in %s\n", plural(result.Summary.Checks, "rule"), duration(elapsed))
	}

	if !globals.human() {
		return
	}

	if globals.Verbosity >= verbosityProgress {
		for _, entry := range result.Checks {
			fmt.Fprintf(stderr, "%s: %s\n", writtenRule(entry), outcome(entry))
		}
	}

	summary := result.Summary
	if list {
		fmt.Fprintf(stderr, "%s: %d would run, %d would decide nothing (%s)\n",
			plural(summary.Checks, "check"),
			summary.Runnable,
			summary.Checks-summary.Runnable,
			duration(elapsed),
		)
		return
	}

	fmt.Fprintf(stderr, "%s: %d ran, %d passed, %d failed, %d decided nothing (%s)\n",
		plural(summary.Checks, "check"),
		summary.Ran,
		summary.Passed,
		summary.Failed,
		summary.Checks-summary.Ran,
		duration(elapsed),
	)
}

// writtenRule renders one listed rule the way it reads on the thing it is bound
// to.
func writtenRule(entry listedCheck) string {
	return strings.Join(append([]string{entry.Subject, entry.Check}, entry.Arguments...), " ")
}

// outcome says what --list has to say about one rule: whether running it would
// decide anything, and where it would not, which of the two reasons it is.
//
// The two are told apart rather than reported as one absence, because they are
// fixed in different places. A check nothing implements is the engine's to
// write; a check which cannot examine the thing it was written on is a line in
// the model, and reporting it as unimplemented would send its author to the
// wrong repository.
func outcome(entry listedCheck) string {
	switch {
	case entry.Runs:
		return "would run"
	case !entry.Applicable:
		return "does not apply to what it is written on"
	default:
		return "declared, not implemented"
	}
}

// duration renders how long something took for a person, to the microsecond,
// which is fine enough that a run over a model with nothing in it reports a
// number rather than "0s" and coarse enough that the number is readable.
func duration(elapsed time.Duration) string {
	return elapsed.Round(time.Microsecond).String()
}
