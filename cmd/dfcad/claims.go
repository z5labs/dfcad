// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/z5labs/dfcad"
)

const claimsUsage = `dfcad claims — every claim written on one thing.

Usage:

	dfcad claims [flags] <id> [predicate]

Every claim on the subject, live and retracted alike, each with its value, its
unit, what evidences it, how it was obtained, how well it is known, when, its
rank and its own id. With a predicate, only the claims written under that one.

This is the audit view of one subject. "dfcad get" answers what the model says
about a thing now; this answers everything anybody has said about it and what
became of each statement. Deprecated claims are therefore in the answer rather
than behind a flag, marked as retracted and carrying the id of the claim which
replaced them, so a retraction is followable forward without a second call.

Every claim says what resolution made of it:

	current     the claim resolution picks under its predicate
	tied        one of several claims resolution cannot separate, whether
	            because they are equally accurate and equally recent or
	            because nothing rankable was said about any of them
	unranked    the one live claim under a predicate nothing rankable was
	            said about, which leaves nothing to choose between
	outranked   a live claim which another claim under the same predicate beat
	retracted   a deprecated claim, which resolution never considers

Claims come back in predicate order and then in the order they were written, so
two runs over one model diff against each other and moving a claim between
files does not reshuffle the answer.

A disagreement is a finding rather than a failure, so this exits zero whatever
it finds. Whether a disagreement is allowed is what "dfcad check" answers.

An id nothing in the model holds is a usage error naming it, and naming the
nearest id there is when one is close enough to be the id that was meant. A
predicate the registry does not declare is a usage error for the same reason: a
predicate nobody declared and a predicate nothing is claimed under are different
answers, and a caller which cannot tell them apart retries a misspelling
forever.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object claims writes carries "subject", the id it was asked about, and
"claims", every claim written on it in predicate order.
`

const conflictsUsage = `dfcad conflicts — every disagreement in the model.

Usage:

	dfcad conflicts [flags]

Every subject and predicate the model states more than once, with the competing
claims and what resolution makes of them. It takes no arguments: the answer is
the whole conflict register, which is the thing this command exists to make
lookable-at rather than a property of the design.

Flags:

	--type <name>       only pairs whose subject declares this type
	--predicate <name>  only pairs written under this predicate
	--ambiguous         only pairs resolution cannot decide
	--resolved          only pairs resolution can

Filters combine: a pair is listed when it satisfies every filter given.
--ambiguous and --resolved are refused together rather than answered with
nothing: a pair with more than one live claim either has a best one or does not,
so no pair is both and an empty answer would read as a model without conflicts.

A pair conflicts when more than one live claim is written on it, whatever those
claims say. Whether two values agree is a question about a tolerance, and
tolerances are registry data the consuming repository owns rather than a
constant hidden in this walk, so the register reports that the model states a
thing twice and what each statement is, and leaves agreement to whoever declared
what agreement means.

A deprecated claim is never competing. It is retracted rather than out-ranked,
so a pair whose second claim is deprecated has one live claim and no entry here.
That is the one way of silencing a conflict there is, and it requires asserting
in the file that the claim is wrong.

Pairs come back ordered by subject and then by predicate, which is an order of
what the claims are about rather than of where they were written.

A conflict is a finding rather than a failure, so this exits zero however many
it finds. Whether a disagreement is allowed is what "dfcad check" answers, and
answering it twice, in two commands, is how the two come to disagree.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object conflicts writes carries "conflicts": one entry per pair, in subject
and then predicate order, each with the competing claims and whether resolution
picks a winner.
`

// The remaining things resolution can leave a claim as, beside the three
// [claimsResolved] already reports.
//
// They exist because the audit view reports every claim rather than only the
// ones which could still be the answer: a claim which lost and a claim which was
// retracted are both left out of a resolution, and reporting them as the same
// thing would say a measurement somebody withdrew and one somebody bettered are
// the same kind of not-current.
const (
	// resolutionOutranked is a live claim another claim under the same predicate
	// beat.
	resolutionOutranked = "outranked"

	// resolutionRetracted is a deprecated claim, which resolution never
	// considers.
	resolutionRetracted = "retracted"
)

// ErrAmbiguousAndResolved is --ambiguous asked for beside --resolved.
//
// It is refused rather than answered with nothing because the two partition the
// register: a pair carrying more than one live claim either has a best claim or
// does not, and no pair is both. A run which accepted the pair of flags would
// write an empty register, which reads as a model nobody disagrees about.
var ErrAmbiguousAndResolved = errors.New(
	"--ambiguous and --resolved name the two halves of the register: a conflicting pair either has a " +
		"best claim or does not, and none is both",
)

// UnknownPredicateError is a predicate no registry file declares.
type UnknownPredicateError struct {
	// Predicate is what was asked for.
	Predicate string

	// Declared is every predicate the registry declares, in name order.
	Declared []string
}

// Error implements [error].
func (e UnknownPredicateError) Error() string {
	if len(e.Declared) == 0 {
		return fmt.Sprintf("unknown predicate %s: this model declares no predicate at all", e.Predicate)
	}
	return fmt.Sprintf("unknown predicate %s: want one of %s", e.Predicate, strings.Join(e.Declared, ", "))
}

// claimsResult is the object claims writes to stdout.
type claimsResult struct {
	envelope

	// Subject is the id the claims below are written on, which is the id asked
	// for.
	Subject string `json:"subject"`

	// Claims is every claim written on it, in predicate order. Empty rather
	// than null when nothing is claimed about it.
	Claims []claimEntry `json:"claims"`
}

// conflictsResult is the object conflicts writes to stdout.
type conflictsResult struct {
	envelope

	// Conflicts is one entry per pair the model states more than once, in
	// subject and then predicate order. Empty rather than null when nothing
	// disagrees.
	Conflicts []conflictEntry `json:"conflicts"`
}

// conflictEntry is one subject and predicate the model states more than once.
//
// It carries the competing claims rather than a count of them, because the next
// thing anybody does with a conflict is read what each side says and where it
// was written; an entry which reported only that there was a disagreement would
// be a second lookup per line of the register.
type conflictEntry struct {
	// Subject is the id of the thing the competing claims are about.
	Subject string `json:"subject"`

	// Predicate is the predicate they were written under.
	Predicate string `json:"predicate"`

	// Type is the type the subject declares, when the subject is a semantic
	// node. Absent for a vertex, an edge or a loop, which declare none.
	Type string `json:"type,omitempty"`

	// Ambiguous reports that resolution picks nothing, so the disagreement has
	// no answer. Exactly one of this and a claim marked current holds of every
	// entry.
	Ambiguous bool `json:"ambiguous"`

	// Current is the id of the claim resolution picks. Absent when nothing was
	// picked, and also when the claim which was picked wrote no id of its own —
	// the claim marked current below carries the span which names it instead.
	Current string `json:"current,omitempty"`

	// Claims are the competing claims, in the order they were written.
	Claims []claimEntry `json:"claims"`
}

// runClaims is the claims command.
func runClaims(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(arguments) == 0 {
		return usageError(cmd, ErrMissingID, stderr, true)
	}
	if len(arguments) > 2 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments[2:]}, stderr, true)
	}

	// An argument which is not an id is a different mistake from an id nothing
	// holds, and the production it broke is a better answer than a lookup which
	// was never going to find anything.
	subject, err := dfcad.ParseID(arguments[0])
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	var predicate string
	if len(arguments) == 2 {
		predicate = arguments[1]
	}

	graph := loadModel(cmd, globals, stderr)

	if _, ok := graph.Entity(subject); !ok {
		nearest, _ := graph.Nearest(subject)
		return usageError(cmd, UnknownIDError{ID: string(subject), Nearest: string(nearest)}, stderr, false)
	}
	if err := checkPredicate(graph.Registry(), predicate); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	result := claimsResult{
		envelope: newEnvelope(cmd.name),
		Subject:  string(subject),
		Claims:   audited(graph, subject, predicate),
	}

	reportClaims(result, globals, stderr)

	if err := emit(stdout, result); err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// runConflicts is the conflicts command.
func runConflicts(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	declaredType := flags.String("type", "", "")
	predicate := flags.String("predicate", "", "")
	ambiguous := flags.Bool("ambiguous", false, "")
	resolved := flags.Bool("resolved", false, "")

	extra, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(extra) > 0 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: extra}, stderr, true)
	}

	// The model is loaded before the filters are checked because the registry is
	// what says whether a type or a predicate exists, and the registry is the
	// model.
	graph := loadModel(cmd, globals, stderr)
	registry := graph.Registry()

	if *ambiguous && *resolved {
		return usageError(cmd, ErrAmbiguousAndResolved, stderr, false)
	}
	if *declaredType != "" && !registry.Declares(dfcad.SortType, *declaredType) {
		return usageError(cmd, UnknownTypeError{
			Type:     *declaredType,
			Declared: registry.Names(dfcad.SortType),
		}, stderr, false)
	}
	if err := checkPredicate(registry, *predicate); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	result := conflictsResult{
		envelope: newEnvelope(cmd.name),

		// Made rather than declared so that a model nobody disagrees about
		// writes an empty list rather than a null, and a caller indexing it
		// needs no special case for the model which is not in dispute.
		Conflicts: make([]conflictEntry, 0),
	}

	for conflict := range graph.Claims().Conflicts() {
		entry := disagreement(graph, conflict)

		if *predicate != "" && entry.Predicate != *predicate {
			continue
		}
		if *declaredType != "" && entry.Type != *declaredType {
			continue
		}
		if *ambiguous && !entry.Ambiguous {
			continue
		}
		if *resolved && entry.Ambiguous {
			continue
		}

		result.Conflicts = append(result.Conflicts, entry)
	}

	reportConflicts(result.Conflicts, globals, stderr)

	if err := emit(stdout, result); err != nil {
		fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// checkPredicate reports a predicate the registry does not declare, and accepts
// the empty one, which is no filter at all.
//
// An undeclared name is a usage error rather than an empty answer, for the
// reason an undeclared type is one in a listing: a predicate nobody declared and
// a predicate nothing is written under are different answers, and a caller which
// cannot tell them apart retries a misspelling forever.
func checkPredicate(registry *dfcad.Registry, predicate string) error {
	if predicate == "" || registry.Declares(dfcad.SortPredicate, predicate) {
		return nil
	}
	return UnknownPredicateError{Predicate: predicate, Declared: registry.Names(dfcad.SortPredicate)}
}

// audited is every claim written on one subject, each marked with what
// resolution made of it.
//
// The deprecated ones are in it. This is the view which answers what has been
// said about a thing rather than what is currently believed about it, and a
// retraction which is not in the answer cannot be told from a claim nobody ever
// wrote.
func audited(graph *dfcad.Graph, subject dfcad.ID, predicate string) []claimEntry {
	// Made rather than declared so that a thing nothing is claimed about carries
	// an empty list rather than a null.
	out := make([]claimEntry, 0)

	for _, written := range predicatesOf(graph, subject) {
		if predicate != "" && written != predicate {
			continue
		}

		// The error is a strict predicate resolving to more than one claim, and
		// the resolution comes back beside it carrying every one of them.
		// Reporting what the model says is this command's whole job; whether an
		// ambiguity is a failure is what `dfcad check` answers.
		resolution, _ := graph.Claims().Resolve(subject, written, graph.Registry())

		for claim := range graph.Claims().Under(subject, written) {
			out = append(out, entryOf(claim, madeOf(claim, resolution)))
		}
	}

	inPredicateOrder(out)

	return out
}

// disagreement is one conflict as the register reports it.
func disagreement(graph *dfcad.Graph, conflict dfcad.Conflict) conflictEntry {
	resolution := conflict.Resolution()

	entry := conflictEntry{
		Subject:   string(conflict.Subject()),
		Predicate: conflict.Predicate(),
		Ambiguous: conflict.Ambiguous(),
		Claims:    make([]claimEntry, 0, len(conflict.Claims())),
	}

	// The type is reported whether or not it was filtered on, so that a register
	// read whole is readable on its own and a caller does not have to retrieve
	// every subject to find out what sort of thing disagrees with itself.
	if node, ok := graph.Node(conflict.Subject()); ok {
		entry.Type = node.Type()
	}
	if id, ok := resolution.ClaimID(); ok {
		entry.Current = string(id)
	}

	for _, claim := range conflict.Claims() {
		entry.Claims = append(entry.Claims, entryOf(claim, madeOf(claim, resolution)))
	}

	return entry
}

// madeOf is what resolution left one claim as.
//
// A deprecated claim is retracted rather than out-ranked and resolution never
// sees it, so it is answered here rather than by asking a resolution which was
// computed without it.
func madeOf(claim *dfcad.Claim, resolution dfcad.Resolution) string {
	if claim.Rank() == dfcad.RankDeprecated {
		return resolutionRetracted
	}

	if winner, ok := resolution.Claim(); ok && winner == claim {
		return resolutionCurrent
	}

	// Not a candidate, so something comparable beat it. That is a different
	// answer from a claim which is still in the running, and a register which
	// reported both as simply not current would hide which of two competing
	// measurements the rule already decided about.
	if !slices.Contains(resolution.Candidates(), claim) {
		return resolutionOutranked
	}

	if resolution.Ambiguous() {
		return resolutionTied
	}
	return resolutionUnranked
}

// reportClaims renders a claims result for a person, on stderr.
//
// Nothing here reaches stdout, in any format and at any verbosity: stdout is the
// same bytes whether or not anybody asked to read the run.
func reportClaims(result claimsResult, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	predicates := make(map[string]struct{}, len(result.Claims))
	retracted := 0

	for _, claim := range result.Claims {
		predicates[claim.Predicate] = struct{}{}
		if claim.Rank == string(dfcad.RankDeprecated) {
			retracted++
		}

		// The claims themselves are already the result, on stdout, so the
		// reading of them is progress rather than result.
		if globals.Verbosity >= verbosityProgress {
			fmt.Fprintf(stderr, "%s: %s\n", claim.Predicate, spellClaim(claim))
		}
	}

	fmt.Fprintf(stderr, "%s of %s under %s, %d retracted\n",
		plural(len(result.Claims), "claim"),
		result.Subject,
		plural(len(predicates), "predicate"),
		retracted,
	)
}

// reportConflicts renders a conflicts result for a person, on stderr.
func reportConflicts(conflicts []conflictEntry, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	subjects := make(map[string]struct{}, len(conflicts))
	ambiguous := 0

	for _, conflict := range conflicts {
		subjects[conflict.Subject] = struct{}{}
		if conflict.Ambiguous {
			ambiguous++
		}

		if globals.Verbosity >= verbosityProgress {
			fmt.Fprintf(stderr, "%s %s: %s, %s\n",
				conflict.Subject,
				conflict.Predicate,
				plural(len(conflict.Claims), "claim"),
				settled(conflict),
			)
		}
	}

	fmt.Fprintf(stderr, "%s across %s, %d ambiguous\n",
		plural(len(conflicts), "conflict"),
		plural(len(subjects), "subject"),
		ambiguous,
	)
}

// settled is what resolution made of one pair, for a person.
func settled(conflict conflictEntry) string {
	if conflict.Ambiguous {
		return "ambiguous"
	}
	if conflict.Current == "" {
		return "resolved"
	}
	return "resolved to " + conflict.Current
}
