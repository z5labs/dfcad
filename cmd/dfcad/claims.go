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

const claimsUsage = `dfcad claims — every claim written on one thing, or on every thing.

Usage:

	dfcad claims [flags] [<id> [predicate]]

Every claim on the subject, live and retracted alike, each with its value, its
unit, what evidences it, how it was obtained, how well it is known, when, its
rank and its own id. With a predicate, only the claims written under that one.

With no id, every claim written on every node, vertex, edge and loop in the
model: exactly what "dfcad claims <id>" answers for each of them, one after
another in id order. That is the audit view of the whole model, which is how
"every position claim, with its method and its accuracy" is one call rather than
a listing followed by one call per thing listed.

This is the audit view. "dfcad get" answers what the model says about a thing
now; this answers everything anybody has said about it and what became of each
statement. Deprecated claims are therefore in the answer rather than behind a
flag, marked as retracted and carrying the id of the claim which replaced them,
so a retraction is followable forward without a second call.

Every claim says what resolution made of it:

	current     the claim resolution picks under its predicate
	tied        one of several claims resolution cannot separate, whether
	            because they are equally accurate and equally recent or
	            because nothing rankable was said about any of them
	unranked    the one live claim under a predicate nothing rankable was
	            said about, which leaves nothing to choose between
	outranked   a live claim which another claim under the same predicate beat
	retracted   a deprecated claim, which resolution never considers

Flags:

	--predicate <name>  only claims written under this predicate; repeat
	--type <name>       only claims on a node declaring this type; repeat
	--family <family>   only claims on a thing of this family: node, vertex,
	                    edge or loop; repeat
	--method <id>       only claims obtained by this method; repeat
	--unrankable        only claims resolution cannot rank: those which state
	                    no accuracy, and those whose accuracy is in more than
	                    one unit

Filters combine: a claim is listed when it satisfies every filter given, and a
filter written more than once is satisfied by any of its values. They apply with
an id as well as without one, and a predicate written after the id counts as one
more --predicate, checked before the flag's values. --type beside --family values none of which is node is refused
rather than answered with nothing: only a node declares a type, so no claim
satisfies both, and an empty answer would read as a model with no such claims.

A method is an id, and it is matched exactly. There is no registry of methods to
check one against: its namespace is what the registry governs, so a value which
is not an id, or an id in a namespace the registry does not declare, is a usage
error. A well-formed id in a declared namespace which no claim in the model
names is answered with an empty list, and a warning on stderr says that nothing
names it, in every format: it may be misspelt, and it may be a method nobody has
used yet, and nothing here can tell which. Every claim whose method is not one
of a set is the complement of this listing, which is the caller's to take.

--unrankable selects a state, not a missing field. Of the children a claim may
leave out (specification section 6.5), source, method and date may not be, so a
claim without one does not load, and an id left out says nothing about the
claim. Accuracy is the one whose absence changes what a claim is: it loads and
is unrankable, which is what resolve reports as "unranked" and plan and measure
as budget.unranked. A claim whose accuracy terms are in more than one unit is
unrankable for the same reason, since nothing reduces them to one figure, and is
listed too, carrying its "units". Retracted claims are listed beside live ones,
each still marked with its resolution. A model in which every claim states an
accuracy answers an empty list.

Claims come back in subject id order, then in predicate order and then in the
order they were written, so two runs over one model diff against each other and
moving a claim between files does not reshuffle the answer. Each carries the
subject it is written on, that subject's family, its type for a node, and
"retired" where the node has been retired, so the listing reads without a "get"
per subject.

The id may be a frame the registry declares as well as a node, a vertex, an
edge or a loop: a frame carries claims — the transform which places it in its
parent among them — and they are answered as any other subject's are, with
"frame" as the family. The listing with no id is of the four families above,
so a claim written on a frame is asked about by the frame's id.

A disagreement is a finding rather than a failure, so this exits zero whatever
it finds. Whether a disagreement is allowed is what "dfcad check" answers.

An id nothing in the model holds is a usage error naming it, and naming the
nearest id there is when one is close enough to be the id that was meant. A
predicate or a type the registry does not declare, and a family which is none of
the four, is a usage error for the same reason, whichever of a filter's values
it is: a predicate nobody declared and a predicate nothing is claimed under are
different answers, and a caller which cannot tell them apart retries a
misspelling forever. A declared predicate nothing is claimed under is an empty
list and exit zero.

` + globalFlagsHelp + `
` + readFlagsHelp + `
` + outputContractHelp + `
The object claims writes carries "subject", the id it was asked about, when one
was, and "claims", every claim written on it — or on every subject, where no id
was given — in subject, predicate and written order.
`

const conflictsUsage = `dfcad conflicts — every disagreement in the model.

Usage:

	dfcad conflicts [flags]

Every subject and predicate the model states more than once, with the competing
claims and what resolution makes of them. It takes no arguments: the answer is
the whole conflict register, which is the thing this command exists to make
lookable-at rather than a property of the design.

Flags:

	--type <name>       only pairs whose subject declares this type; repeat
	--predicate <name>  only pairs written under this predicate; repeat
	--ambiguous         only pairs resolution cannot decide
	--resolved          only pairs resolution can

Filters combine: a pair is listed when it satisfies every filter given, and a
filter written more than once is satisfied by any of its values. A type or a
predicate the registry does not declare is a usage error naming it, whichever of
the values it is.

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
` + readFlagsHelp + `
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

// ErrTypeNeedsNodeFamily is --type asked for beside --family values none of
// which is node.
//
// It is refused rather than answered with nothing, for the reason --ambiguous
// beside --resolved is: only a node declares a type, so no claim on a vertex, an
// edge or a loop satisfies a type, and an empty answer would read as a model
// holding no such claims.
var ErrTypeNeedsNodeFamily = errors.New(
	"--type names a type only a node declares, and no --family value is node: no claim satisfies both",
)

// flagMethod is the filter on how a claim was obtained, named here because the
// warning about an unused method spells the flag.
const flagMethod = "method"

// claimFamilies are the four families a subject of a claim can belong to, in
// the order the usage lists them.
//
// They are list-geometry's three with node added, because a claim is written on
// a semantic node as often as on a geometric one.
var claimFamilies = []string{familyNode, familyVertex, familyEdge, familyLoop}

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
	loadState

	// Subject is the id the claims below are written on, which is the id asked
	// for. Absent where no id was, and the claims are every subject's.
	Subject string `json:"subject,omitempty"`

	// Claims is every claim written on it, in predicate order, or on every
	// subject in subject and then predicate order. Empty rather than null when
	// nothing is claimed.
	Claims []claimRow `json:"claims"`
}

// claimRow is one claim as claims reports it: the claim object get writes,
// with what it is written on beside it.
//
// The subject's fields are written beside the claim's rather than nested under a
// key of their own, as a plan's annotation writes them, so that a claim here
// reads exactly like a claim anywhere else in this contract. They are written
// whether or not an id was asked about, so that an entry has one shape whatever
// narrowed the listing.
type claimRow struct {
	// Subject is the id of the thing the claim is written on.
	Subject string `json:"subject"`

	// Family is which family holds the subject: node, vertex, edge or loop, or
	// frame where the subject asked about is a frame the registry declares.
	Family string `json:"family"`

	// Type is the type the subject declares, for a node. Absent for a vertex, an
	// edge, a loop or a frame, which declare none.
	Type string `json:"type,omitempty"`

	// Retired reports that the subject is a node which has been retired. Absent
	// otherwise.
	Retired bool `json:"retired,omitempty"`

	claimEntry
}

// conflictsResult is the object conflicts writes to stdout.
type conflictsResult struct {
	envelope
	loadState

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
func runClaims(cmd command, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	predicateFlag := &repeated{}
	typeFlag := &repeated{}
	familyFlag := &repeated{}
	methodFlag := &repeated{}

	flags.Var(predicateFlag, flagPredicate, "")
	flags.Var(typeFlag, "type", "")
	flags.Var(familyFlag, flagFamily, "")
	flags.Var(methodFlag, flagMethod, "")
	unrankable := flags.Bool("unrankable", false, "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(arguments) > 2 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments[2:]}, stderr, true)
	}

	// An argument which is not an id is a different mistake from an id nothing
	// holds, and the production it broke is a better answer than a lookup which
	// was never going to find anything.
	var subject dfcad.ID
	if len(arguments) > 0 {
		parsed, err := dfcad.ParseID(arguments[0])
		if err != nil {
			return usageError(cmd, err, stderr, false)
		}
		subject = parsed
	}

	// A predicate written after the id is one more --predicate, so that the
	// positional form and the flag are one filter rather than two which could
	// disagree. It is checked first, with the id it follows: arguments and flags
	// may be written in any order, and the flag package does not say where
	// between the flag's values an argument fell.
	predicates := filterOf(*predicateFlag)
	if len(arguments) == 2 && arguments[1] != "" {
		predicates = append([]string{arguments[1]}, predicates...)
	}
	types, wanted := filterOf(*typeFlag), filterOf(*familyFlag)

	// A family is checked before the load, as list-geometry checks one: the four
	// are a closed set compiled in, so nothing in the tree makes a misspelling
	// any more of a family.
	if err := checkClaimFamilies(types, wanted); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	// A method is checked for being an id before the load for the same reason: the
	// grammar of an id is compiled in. Whether its namespace is declared is
	// registry data, and is checked after.
	methods, err := parseMethods(filterOf(*methodFlag))
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	graph, loaded, exit := loadModel(cmd, globals, stdin, stderr)
	if exit != exitSuccess {
		return exit
	}
	registry := graph.Registry()

	// The id is looked up as get looks one up, so that a frame — which the
	// graph does not hold, and which carries claims all the same — is a subject
	// here exactly as it is there.
	if subject != "" {
		if _, ok := retrieve(graph, subject); !ok {
			nearest, _ := graph.Nearest(subject)
			return usageError(cmd, UnknownIDError{ID: string(subject), Nearest: string(nearest)}, stderr, false)
		}
	}
	for _, asked := range predicates {
		if err := checkPredicate(registry, asked); err != nil {
			return usageError(cmd, err, stderr, false)
		}
	}
	if err := checkFilters(registry, types, nil, nil); err != nil {
		return usageError(cmd, err, stderr, false)
	}
	if err := checkMethodNamespaces(registry, methods); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	subjects := []dfcad.ID{subject}
	if subject == "" {
		subjects = claimedSubjects(graph)
	}

	result := claimsResult{
		envelope:  newEnvelope(cmd.name),
		loadState: loaded,
		Subject:   string(subject),

		// Made rather than declared so that a model nothing is claimed about
		// writes an empty list rather than a null.
		Claims: make([]claimRow, 0),
	}

	// One predicate is handed to the walk rather than filtered after it, so that
	// the common --predicate X resolves that one predicate on each subject rather
	// than every predicate written there. The order is the same either way.
	var only string
	if len(predicates) == 1 {
		only = predicates[0]
	}

	for _, each := range subjects {
		found, _ := retrieve(graph, each)
		row := subjectOf(found)

		if !admits(wanted, row.Family) || !admits(types, row.Type) {
			continue
		}

		for _, claim := range audited(graph, each, only) {
			if !admits(predicates, claim.Predicate) || !admits(methods, claim.Method) {
				continue
			}
			if *unrankable && ranked(claim) {
				continue
			}
			row.claimEntry = claim
			result.Claims = append(result.Claims, row)
		}
	}

	reportUnnamedMethods(cmd, unnamedMethods(graph, methods), stderr)
	reportClaims(result, globals, stderr)

	if err := emit(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	return exitSuccess
}

// ranked reports whether resolution can rank a claim, read off the entry the
// listing writes for it.
//
// Combined is written exactly where the claim's accuracy reduces to one figure,
// which is the test [dfcad.Claim.Rankable] makes: absent for a claim which
// states no accuracy and for one whose terms are in more than one unit, and
// present otherwise. Reading it off the row keeps --unrankable the filter of
// what the row says rather than a second reading of the claim which could
// disagree with it.
func ranked(claim claimEntry) bool {
	return claim.Combined != nil
}

// parseMethods is the --method values as ids, in the order they were written,
// or the first which is not one.
//
// They stay strings, spelled as the claim rows spell a method, because what the
// filter compares them with is what each row writes.
func parseMethods(written []string) ([]string, error) {
	out := make([]string, 0, len(written))
	for _, value := range written {
		id, err := dfcad.ParseID(value)
		if err != nil {
			return nil, err
		}
		out = append(out, string(id))
	}
	return out, nil
}

// checkMethodNamespaces reports the first method whose namespace the registry
// does not declare, in the error the write path gives for the same mistake.
//
// It is the one check a method can be put to. A method is an id rather than a
// member of a known set (specification section 12), so its namespace is the only
// part of it the registry governs: a local part nobody has written yet cannot be
// told from a misspelt one.
func checkMethodNamespaces(registry *dfcad.Registry, methods []string) error {
	for _, method := range methods {
		namespace := dfcad.ID(method).Namespace()
		if registry.Declares(dfcad.SortNamespace, namespace) {
			continue
		}
		return dfcad.UnknownAxisError{
			Axis:      string(dfcad.SortNamespace),
			Value:     namespace,
			Permitted: registry.Names(dfcad.SortNamespace),
		}
	}
	return nil
}

// unnamedMethods is every method asked for which no claim in the model names,
// each once and in the order they were asked for.
//
// Every claim in the model counts, whatever the other filters exclude: a method
// some claim names is a method in use, and a listing the other filters emptied
// says nothing about its spelling.
func unnamedMethods(graph *dfcad.Graph, methods []string) []string {
	if len(methods) == 0 {
		return nil
	}

	// Only the methods asked for are tracked, and the walk stops once each has
	// been seen: the question is whether each is named at all, not by what.
	unseen := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		unseen[method] = struct{}{}
	}
	for claim := range graph.Claims().All() {
		delete(unseen, string(claim.Method()))
		if len(unseen) == 0 {
			return nil
		}
	}

	var out []string
	for _, method := range methods {
		if _, ok := unseen[method]; !ok || slices.Contains(out, method) {
			continue
		}
		out = append(out, method)
	}

	return out
}

// reportUnnamedMethods warns, once per method, that no claim names it.
//
// It is written in every format, as the curves a rule read straight are: the
// answer on stdout is an empty list either way, and only this line says that the
// list is empty because nothing in the model was obtained that way rather than
// because the other filters left nothing. Stdout is the same bytes with or
// without it.
func reportUnnamedMethods(cmd command, methods []string, stderr io.Writer) {
	for _, method := range methods {
		_, _ = fmt.Fprintf(stderr,
			"dfcad %s: warning: no claim in the model names the method %s, so --%s %s matches nothing; "+
				"a method is not checked against a known set, so it may be misspelt or not yet used\n",
			cmd.name, method, flagMethod, method,
		)
	}
}

// checkClaimFamilies reports the first --family value which is none of the four,
// and a --type which no --family value leaves a node to satisfy.
//
// Neither needs the model: the families are a closed set compiled in, and only
// a node declares a type whatever the registry says. The types themselves are
// registry data and are checked after the load, by [checkFilters].
func checkClaimFamilies(types, families []string) error {
	for _, asked := range families {
		if !slices.Contains(claimFamilies, asked) {
			return UnknownFamilyError{Family: asked, Known: claimFamilies}
		}
	}

	if len(types) > 0 && len(families) > 0 && !slices.Contains(families, familyNode) {
		return ErrTypeNeedsNodeFamily
	}

	return nil
}

// claimedSubjects is every node, vertex, edge and loop a claim is written on,
// each once and in id order.
//
// A subject [dfcad.Graph.Entity] does not hold is left out. Today that is a
// frame, which carries the claim placing it in its parent. "dfcad claims
// frame:building" answers it by id, as get does; the whole-model listing is of
// the four families --family names, and a frame joining it would be an addition
// to that listing of its own, with a family the filter would have to learn.
func claimedSubjects(graph *dfcad.Graph) []dfcad.ID {
	seen := make(map[dfcad.ID]struct{})
	var out []dfcad.ID

	for claim := range graph.Claims().All() {
		subject := claim.Subject()
		if _, done := seen[subject]; done {
			continue
		}
		seen[subject] = struct{}{}

		if _, ok := graph.Entity(subject); ok {
			out = append(out, subject)
		}
	}

	slices.Sort(out)

	return out
}

// subjectOf is what a claim row says about the thing it is written on.
//
// A frame is a family of its own here, as it is in get's answer: it is not a
// node, and a row reading "node" would send whoever filters on it to a thing
// with a type it does not have.
func subjectOf(subject held) claimRow {
	row := claimRow{Subject: string(subject.id())}
	if subject.frame != nil {
		row.Family = familyFrame
		return row
	}

	switch found := subject.entity.(type) {
	case *dfcad.SemanticNode:
		row.Family = familyNode
		row.Type = found.Type()
		row.Retired = found.Retired()
	case *dfcad.Vertex:
		row.Family = familyVertex
	case *dfcad.Edge:
		row.Family = familyEdge
	case *dfcad.Loop:
		row.Family = familyLoop
	}

	return row
}

// runConflicts is the conflicts command.
func runConflicts(cmd command, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	typeFlag := &repeated{}
	predicateFlag := &repeated{}

	flags.Var(typeFlag, "type", "")
	flags.Var(predicateFlag, "predicate", "")
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
	graph, loaded, exit := loadModel(cmd, globals, stdin, stderr)
	if exit != exitSuccess {
		return exit
	}
	registry := graph.Registry()

	if *ambiguous && *resolved {
		return usageError(cmd, ErrAmbiguousAndResolved, stderr, false)
	}

	types, predicates := filterOf(*typeFlag), filterOf(*predicateFlag)

	if err := checkFilters(registry, types, nil, nil); err != nil {
		return usageError(cmd, err, stderr, false)
	}
	for _, asked := range predicates {
		if err := checkPredicate(registry, asked); err != nil {
			return usageError(cmd, err, stderr, false)
		}
	}

	result := conflictsResult{
		envelope:  newEnvelope(cmd.name),
		loadState: loaded,

		// Made rather than declared so that a model nobody disagrees about
		// writes an empty list rather than a null, and a caller indexing it
		// needs no special case for the model which is not in dispute.
		Conflicts: make([]conflictEntry, 0),
	}

	for conflict := range graph.Claims().Conflicts() {
		entry := disagreement(graph, conflict)

		if !admits(predicates, entry.Predicate) {
			continue
		}
		if !admits(types, entry.Type) {
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
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
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
	subjects := make(map[string]struct{}, len(result.Claims))
	retracted := 0

	for _, claim := range result.Claims {
		predicates[claim.Predicate] = struct{}{}
		subjects[claim.Subject] = struct{}{}
		if claim.Rank == string(dfcad.RankDeprecated) {
			retracted++
		}

		// The claims themselves are already the result, on stdout, so the
		// reading of them is progress rather than result. Every subject's
		// claims are named by the subject, which one subject's need not be.
		if globals.Verbosity >= verbosityProgress {
			prefix := ""
			if result.Subject == "" {
				prefix = claim.Subject + " "
			}
			_, _ = fmt.Fprintf(stderr, "%s%s: %s\n", prefix, claim.Predicate, spellClaim(claim.claimEntry))
		}
	}

	of := result.Subject
	if of == "" {
		of = plural(len(subjects), "subject")
	}

	_, _ = fmt.Fprintf(stderr, "%s of %s under %s, %d retracted\n",
		plural(len(result.Claims), "claim"),
		of,
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
			_, _ = fmt.Fprintf(stderr, "%s %s: %s, %s\n",
				conflict.Subject,
				conflict.Predicate,
				plural(len(conflict.Claims), "claim"),
				settled(conflict),
			)
		}
	}

	_, _ = fmt.Fprintf(stderr, "%s across %s, %d ambiguous\n",
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
