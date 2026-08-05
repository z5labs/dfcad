// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// FindingKind names which diff-aware check produced a finding.
//
// It is a string so that its machine-readable form is the same word a policy is
// written with and the same word the human rendering prints, for the reason
// [Severity] is one: a number would have to be kept in step with three
// spellings of itself.
type FindingKind string

// The checks a review runs, in the order it reports them.
const (
	// FindingBoundaryMoved is a physical boundary which moved with no new
	// measurement to account for it — a wall in a different place, where the
	// only thing which changed is the number.
	FindingBoundaryMoved FindingKind = "boundary-moved-without-claim"

	// FindingClaimDeprecated is a claim retracted with nothing standing in its
	// place. It is the belt-and-braces against the load-time rule: the load
	// refuses a deprecation whose replacement it cannot find, and this catches
	// the same shape across two revisions, including the change which retracts
	// a claim and removes its replacement together.
	FindingClaimDeprecated FindingKind = "claim-deprecated-without-replacement"

	// FindingIDDisappeared is an id the base revision held and head does not.
	// An id is assigned once and never reissued
	// ([0002](docs/decisions/0002-immutable-id-mutable-label.md)), so a thing
	// which stopped existing is retired and keeps its id; one which is simply
	// gone takes every reference to it with it.
	FindingIDDisappeared FindingKind = "id-disappeared-without-supersession"
)

// findingKinds is the closed set, in the order a review reports them.
var findingKinds = []FindingKind{FindingBoundaryMoved, FindingClaimDeprecated, FindingIDDisappeared}

// FindingKinds returns every kind a review can report, in the order it reports
// them.
func FindingKinds() []FindingKind { return slices.Clone(findingKinds) }

// UnknownFindingKindError is a policy written about a check no review runs.
//
// The set is listed rather than pointed at, because it is closed and compiled
// in: a caller told only that its name was wrong has nowhere to look the real
// ones up.
type UnknownFindingKindError struct {
	// Kind is what was asked for.
	Kind string

	// Known is every kind a review reports, in the order it reports them.
	Known []FindingKind
}

// Error implements [error].
func (e UnknownFindingKindError) Error() string {
	return fmt.Sprintf("unknown finding %s: want one of %s", e.Kind, joinKinds(e.Known))
}

// ParseFindingKind reads a kind as a policy writes it.
func ParseFindingKind(written string) (FindingKind, error) {
	if slices.Contains(findingKinds, FindingKind(written)) {
		return FindingKind(written), nil
	}
	return "", UnknownFindingKindError{Kind: written, Known: FindingKinds()}
}

// Ruling is what a [Policy] says to do about a finding.
//
// The three exist because a diff-aware check is a question rather than a rule:
// a wall which moved because it was measured again is a change somebody meant,
// and a gate which could only fail on it would be one every author learned to
// route around. Downgrading a kind to a warning, or acknowledging it outright,
// is the stated way to say so.
type Ruling string

// The rulings, from the one which stops a change to the one which says nothing.
const (
	// RulingFailure reports the finding and fails the run.
	RulingFailure Ruling = "failure"

	// RulingWarning reports the finding and does not fail the run.
	RulingWarning Ruling = "warning"

	// RulingIgnored keeps the finding in the result and reports it nowhere
	// else. It is in the result rather than dropped so that what a policy
	// acknowledged is readable from the run which acknowledged it: a check
	// silently switched off is one nobody remembers is off.
	RulingIgnored Ruling = "ignored"
)

// rulings is the closed set, in the order above.
var rulings = []Ruling{RulingFailure, RulingWarning, RulingIgnored}

// Rulings returns every ruling a policy may state, strongest first.
func Rulings() []Ruling { return slices.Clone(rulings) }

// UnknownRulingError is a policy which names no ruling.
type UnknownRulingError struct {
	// Ruling is what was asked for.
	Ruling string

	// Known is every ruling there is.
	Known []Ruling
}

// Error implements [error].
func (e UnknownRulingError) Error() string {
	written := make([]string, 0, len(e.Known))
	for _, ruling := range e.Known {
		written = append(written, string(ruling))
	}
	return fmt.Sprintf("unknown ruling %q: want one of %s", e.Ruling, strings.Join(written, ", "))
}

// ParseRuling reads a ruling as a policy writes it.
func ParseRuling(written string) (Ruling, error) {
	if slices.Contains(rulings, Ruling(written)) {
		return Ruling(written), nil
	}
	return "", UnknownRulingError{Ruling: written, Known: Rulings()}
}

// Policy is what a review does about each kind of finding.
//
// It is stated rather than implied, and it is data rather than a flag per
// check, so that "this repository fails on a disappearing id and warns on a
// wall which moved" is one readable value which a run reports back.
type Policy struct {
	// Default is the ruling for a kind Kinds does not name. The zero Ruling is
	// read as [RulingFailure]: a check added to the engine later is answered
	// rather than silently allowed by a policy written before it existed.
	Default Ruling

	// Kinds is the ruling for each kind it names.
	Kinds map[FindingKind]Ruling
}

// DefaultPolicy is the policy a review runs under when nobody stated one.
//
// Two of the three fail. An id which disappeared and a claim retracted with
// nothing in its place are both breaches of a rule the model is built on rather
// than judgment calls, and each takes references or evidence with it.
//
// A boundary which moved warns, because it is the one of the three which is
// routinely legitimate: a corner measured again genuinely moves, and the check
// cannot see the survey which justified it. Warning is what makes it a question
// for the reviewer rather than a wall for the author.
func DefaultPolicy() Policy {
	return Policy{
		Default: RulingFailure,
		Kinds: map[FindingKind]Ruling{
			FindingBoundaryMoved:   RulingWarning,
			FindingClaimDeprecated: RulingFailure,
			FindingIDDisappeared:   RulingFailure,
		},
	}
}

// Ruling is what the policy says about one kind.
func (p Policy) Ruling(kind FindingKind) Ruling {
	if ruling, ok := p.Kinds[kind]; ok {
		return ruling
	}
	if p.Default == "" {
		return RulingFailure
	}
	return p.Default
}

// With is the policy with one kind ruled differently, and the receiver
// unchanged.
func (p Policy) With(kind FindingKind, ruling Ruling) Policy {
	out := Policy{Default: p.Default, Kinds: maps.Clone(p.Kinds)}
	if out.Kinds == nil {
		out.Kinds = make(map[FindingKind]Ruling, 1)
	}
	out.Kinds[kind] = ruling
	return out
}

// Which of the two revisions a finding's span points into.
//
// A finding about something head no longer holds can only be pointed at in the
// revision it was removed from, and a reader jumping to a file needs to be told
// which of the two it is looking for rather than finding out that the line is
// not there.
const (
	// SideHead is the revision under review.
	SideHead = "head"

	// SideBase is the merge base it is compared against.
	SideBase = "base"
)

// Revision names one commit.
//
// It is what "the commit which introduced this change" means to a reviewer: the
// hash to quote, the subject line to recognise it by, and who wrote it and
// when. A finding which could not say that would leave the reviewer bisecting
// the branch to find out who moved the wall.
type Revision struct {
	// SHA is the full object name of the commit.
	SHA string `json:"sha"`

	// Summary is the first line of its message.
	Summary string `json:"summary,omitempty"`

	// Author is the name the commit was authored under.
	Author string `json:"author,omitempty"`

	// Date is when it was authored, in the zone it was authored in.
	Date time.Time `json:"date,omitzero"`
}

// Named reports whether the revision names a commit at all.
func (r Revision) Named() bool { return r.SHA != "" }

// Short is the abbreviated object name, which is the spelling a person reads
// and every git command accepts.
func (r Revision) Short() string {
	if len(r.SHA) <= 12 {
		return r.SHA
	}
	return r.SHA[:12]
}

// String renders the revision the way a log line does: the short name and the
// subject.
func (r Revision) String() string {
	switch {
	case !r.Named():
		return ""
	case r.Summary == "":
		return r.Short()
	default:
		return r.Short() + " " + r.Summary
	}
}

// History says which commit introduced the change to a file.
//
// It is an interface so that the checks below depend on the idea of a history
// and not on git: a review of two directories somebody exported by hand is the
// same review with nothing to attribute, and it runs rather than being a case
// the checks have to know about.
type History interface {
	// Introduced returns the commit which most recently changed the file at
	// path, and whether the history holds one. The path is a span's, exactly as
	// the loader reached it.
	Introduced(path string) (Revision, bool)
}

// Histories is the histories of both revisions, asked in turn.
//
// A review reads its two revisions out of two directories, and a finding about
// something head no longer holds points into the second of them. One history
// per tree, asked in order, is what lets both be attributed to commits of the
// same range without either having to know that the other exists.
type Histories []History

// Introduced implements [History], answering with the first history which
// recognises the path.
func (h Histories) Introduced(path string) (Revision, bool) {
	for _, history := range h {
		if history == nil {
			continue
		}
		if commit, ok := history.Introduced(path); ok {
			return commit, true
		}
	}
	return Revision{}, false
}

// Dangling is one reference left pointing at nothing.
type Dangling struct {
	// From is the entity which made the reference.
	From ID `json:"from"`

	// Relation is the child it was written as — `within`, `member-of`,
	// `boundary`, `superseded-by`, `vertices`, `edges` or `backed-by`.
	Relation string `json:"relation"`

	// Span is where the entity making the reference is written.
	Span Span `json:"span"`
}

// Finding is one change which is suspicious as a change.
//
// It is not a [Violation] and not a [Diagnostic], and the difference is the
// whole reason this mechanism is separate from assertions. An assertion
// constrains one revision and says the model is wrong; a finding needs two and
// says this change needs an explanation. Keeping them apart is what stops
// either growing into the other — an assertion which needed the previous
// revision would be a rule nothing could evaluate on a fresh checkout.
//
// [Finding.Diagnostic] is the human rendering, through the same renderer every
// diagnostic uses. The machine rendering is this struct, and neither is
// produced by parsing the other.
type Finding struct {
	// Kind is which check reported it.
	Kind FindingKind `json:"kind"`

	// Ruling is what the policy said to do about it.
	Ruling Ruling `json:"ruling"`

	// Subject is the id of the thing the finding is about: the boundary which
	// moved, the claim which was retracted, the id which disappeared.
	Subject ID `json:"subject"`

	// Side is which revision Span points into: [SideHead] or [SideBase].
	Side string `json:"side"`

	// Span is where the change is: in head for a change to something head
	// still holds, and in base for something it does not.
	Span Span `json:"span"`

	// Commit is the commit which introduced the change, and is the zero
	// [Revision] where the review was given no history to attribute it to.
	Commit Revision `json:"commit,omitzero"`

	// Message says what changed and what would have accounted for it.
	Message string `json:"message"`

	// Hint is advice on what to do about it, which is usually the command which
	// records the change properly.
	Hint string `json:"hint,omitempty"`

	// Related are the other places which explain this one — the claim which was
	// rewritten, the vertex which moved.
	Related []RelatedLocation `json:"related,omitempty"`

	// Dangling are the references head still makes to an id it no longer holds,
	// and are empty for every other kind.
	Dangling []Dangling `json:"dangling,omitempty"`
}

// Diagnostic is the finding rendered for whoever wrote the change.
//
// A ruling of [RulingFailure] is an error and everything else is a warning, so
// that the severity a reader sees is the one the policy stated rather than a
// second opinion about how much the finding matters.
func (f Finding) Diagnostic() Diagnostic {
	severity := SeverityWarning
	if f.Ruling == RulingFailure {
		severity = SeverityError
	}

	related := slices.Clone(f.Related)
	for _, dangling := range f.Dangling {
		related = append(related, RelatedLocation{
			Span:    dangling.Span,
			Message: fmt.Sprintf("%s still names it as its %s", dangling.From, dangling.Relation),
		})
	}

	message := f.Message
	if f.Commit.Named() {
		message += ", in " + f.Commit.String()
	}

	return Diagnostic{
		Severity: severity,
		Span:     f.Span,
		Message:  message,
		Hint:     f.Hint,
		Related:  related,
	}
}

// String renders the finding as one line: the ruling, the kind and the message.
func (f Finding) String() string {
	return fmt.Sprintf("%s: %s: %s", f.Ruling, f.Kind, f.Message)
}

// Review compares head against its merge base and reports every change which is
// suspicious as a change.
//
// base and head are the same model loaded at two revisions. What comes back is
// not a judgment about either of them on its own: every finding is about the
// difference, and a model which has always been in the state a check reports is
// reported by neither call.
//
// policy decides what each finding means, and every finding carries the ruling
// it was given — including [RulingIgnored], which is in the result and is
// reported nowhere else. history attributes each finding to the commit which
// introduced it and may be nil, which is a review with nothing to attribute
// rather than an error.
//
// The order is deterministic: by check, in the order [FindingKinds] lists them,
// then by subject, then by position. Two runs over the same pair of revisions
// therefore produce the same list, so a diff between two runs is about what
// changed in the branch.
func Review(base, head *Graph, policy Policy, history History) []Finding {
	if base == nil || head == nil {
		return nil
	}

	found := make([]Finding, 0)
	found = append(found, reviewBoundaries(base, head)...)
	found = append(found, reviewDeprecations(base, head)...)
	found = append(found, reviewDisappearances(base, head)...)

	for i := range found {
		found[i].Ruling = policy.Ruling(found[i].Kind)

		if history != nil {
			if commit, ok := history.Introduced(found[i].Span.Start.Path); ok {
				found[i].Commit = commit
			}
		}
	}

	slices.SortStableFunc(found, compareFindings)

	return found
}

// compareFindings orders findings by check, then by subject, then by where they
// are.
func compareFindings(a, b Finding) int {
	if order := slices.Index(findingKinds, a.Kind) - slices.Index(findingKinds, b.Kind); order != 0 {
		return order
	}
	if order := strings.Compare(string(a.Subject), string(b.Subject)); order != 0 {
		return order
	}
	if order := strings.Compare(a.Span.Start.Path, b.Span.Start.Path); order != 0 {
		return order
	}
	if order := a.Span.Start.Offset - b.Span.Start.Offset; order != 0 {
		return order
	}
	return strings.Compare(a.Message, b.Message)
}

// reviewBoundaries reports a physical boundary which moved with no new
// measurement claim to account for it.
//
// A boundary moves in exactly two ways, and both are checked because the second
// is the one a diff makes hard to see. A corner keeps its id and its claim is
// rewritten in place, which is a one-line diff in a file full of numbers; or
// the ring itself is re-cornered, which is a bigger diff and an easier one to
// wave through.
//
// What accounts for a boundary moving is a new claim: a corner which moved was
// measured again, and the measurement is a claim with its own source, method,
// date and accuracy. The two ways differ in whether anything can account for
// them, which is why this is not one rule with one escape hatch.
//
// A rewritten value has no justification at all. A value is never edited in
// this model — a correction writes the new claim and retracts the old one in
// the same change — so a rewrite has thrown away the record of why the number
// changed whatever else the change added, and the model is left asserting that
// the original survey found the new number. The policy is the escape hatch
// there, and it is the right one, because acknowledging it is a decision
// somebody makes rather than something the check infers.
//
// A re-cornered boundary is different, because re-cornering one properly is
// exactly what a resurvey looks like: new corners, each carrying the claim
// which says where it is. So there, a claim head holds and base does not is
// what accounts for the change.
func reviewBoundaries(base, head *Graph) []Finding {
	var out []Finding

	baseClaims := claimIndex(base)
	headClaims := claimIndex(head)

	for _, node := range sortedNodes(head) {
		if len(node.Boundaries()) == 0 {
			continue
		}

		was, ok := base.Node(node.ID())
		if !ok {
			// A boundary written by this change did not move; it arrived, and
			// what justifies it is the review of a new room rather than this.
			continue
		}

		before := boundaryVertices(base, was)
		now := boundaryVertices(head, node)

		for _, vertex := range now {
			if !slices.Contains(before, vertex) {
				continue
			}

			for _, change := range rewrittenClaims(baseClaims, head, vertex) {
				out = append(out, movedFinding(node, vertex, change))
			}
		}

		if slices.Equal(before, now) {
			continue
		}
		if slices.ContainsFunc(now, func(vertex ID) bool { return measuredAgain(baseClaims, headClaims, vertex) }) {
			continue
		}

		out = append(out, recorneredFinding(node, before, now))
	}

	return out
}

// rewritten is one claim whose value changed without the claim changing.
type rewritten struct {
	// before and after are the claim as each revision holds it.
	before, after *Claim
}

// rewrittenClaims are the claims on subject which both revisions hold and which
// hold different values, in the order head wrote them.
func rewrittenClaims(base map[string]*Claim, head *Graph, subject ID) []rewritten {
	var out []rewritten

	for claim := range head.Claims().Of(subject) {
		was, ok := base[fingerprint(claim)]
		if !ok {
			continue
		}
		if sameValue(was.Value(), claim.Value()) {
			continue
		}
		out = append(out, rewritten{before: was, after: claim})
	}

	return out
}

// measuredAgain reports whether head carries a claim about subject which base
// does not — a measurement written by this change.
func measuredAgain(base, head map[string]*Claim, subject ID) bool {
	for print, claim := range head {
		if claim.Subject() != subject {
			continue
		}
		if _, ok := base[print]; !ok {
			return true
		}
	}
	return false
}

// movedFinding is one corner of one boundary whose position was rewritten.
func movedFinding(node *SemanticNode, vertex ID, change rewritten) Finding {
	predicate := change.after.Predicate()

	return Finding{
		Kind:    FindingBoundaryMoved,
		Subject: node.ID(),
		Side:    SideHead,
		Span:    change.after.Value().Span(),
		Message: fmt.Sprintf(
			"the boundary of %s moved: the %s of %s was rewritten from %s to %s inside the claim which already stated it, so nothing new was measured",
			node.ID(), predicate, vertex,
			writtenValue(change.before.Value()), writtenValue(change.after.Value()),
		),
		Hint: fmt.Sprintf(
			"a corner which moved was measured again, so write the measurement: `dfcad supersede %s %s ...` keeps what the first survey said beside what the second one found",
			vertex, predicate,
		),
		Related: []RelatedLocation{
			{Span: change.before.Span(), Message: "the claim this rewrote, as the merge base holds it"},
			{Span: node.Span(), Message: "the boundary it is part of"},
		},
	}
}

// recorneredFinding is one boundary whose corners changed with nothing measured.
func recorneredFinding(node *SemanticNode, before, now []ID) Finding {
	return Finding{
		Kind:    FindingBoundaryMoved,
		Subject: node.ID(),
		Side:    SideHead,
		Span:    node.Span(),
		Message: fmt.Sprintf(
			"the boundary of %s moved: it is bounded by %s where the merge base has %s, and this change writes no new claim about any of them",
			node.ID(), writtenIDs(now), writtenIDs(before),
		),
		Hint: "a boundary drawn round different corners was surveyed again: write the survey as claims on the corners, so the shape has evidence rather than only a shape",
	}
}

// reviewDeprecations reports a claim this change retracted with nothing
// standing in its place.
//
// Two shapes are reported under the one kind, because a reader acts on them the
// same way and a policy has one thing to say about both. A deprecation whose
// replacement the model does not hold is the load-time rule seen across two
// revisions — the load refuses each revision on its own, and this catches the
// change which retracts a claim and deletes its replacement together. A
// deprecation which leaves nothing asserted about a subject and a predicate is
// the substantive one: the model went from answering a question to not
// answering it, which is a legitimate thing to do and never a thing to find out
// about later.
func reviewDeprecations(base, head *Graph) []Finding {
	var out []Finding

	before := claimIndex(base)
	silenced := make(map[string]bool)

	for _, claim := range sortedClaims(head) {
		if claim.Rank() != RankDeprecated {
			continue
		}

		// A claim which arrived retracted was retracted by this change as
		// surely as one which was live in the merge base; what is skipped is a
		// retraction the merge base already recorded.
		if was, ok := before[fingerprint(claim)]; ok && was.Rank() == RankDeprecated {
			continue
		}

		out = append(out, deprecationFindings(head, claim, silenced)...)
	}

	return out
}

// deprecationFindings is what one newly retracted claim has to answer for.
func deprecationFindings(head *Graph, claim *Claim, silenced map[string]bool) []Finding {
	var out []Finding

	subject, predicate := claim.Subject(), claim.Predicate()

	replacement, named := claim.SupersededBy()
	_, held := head.Claims().Claim(replacement)

	switch {
	case !named:
		out = append(out, Finding{
			Kind:    FindingClaimDeprecated,
			Subject: subject,
			Side:    SideHead,
			Span:    claim.Span(),
			Message: fmt.Sprintf(
				"this change retracts the %s of %s and names no claim to stand in its place",
				predicate, subject,
			),
			Hint: "a retraction names its replacement: `dfcad supersede` writes the new value and retracts the old one in the same change",
		})
	case !held:
		out = append(out, Finding{
			Kind:    FindingClaimDeprecated,
			Subject: subject,
			Side:    SideHead,
			Span:    claim.Span(),
			Message: fmt.Sprintf(
				"this change retracts the %s of %s in favour of %s, which this revision does not hold",
				predicate, subject, replacement,
			),
			Hint: "the replacement was removed or never written; a retraction and the deletion of what replaced it are one change which leaves nothing behind",
		})
	}

	// A subject and a predicate with nothing left live is one answer the model
	// stopped giving, however many of its claims this change retracted.
	key := string(subject) + "\x00" + predicate
	if len(head.Claims().Live(subject, predicate)) == 0 && !silenced[key] {
		silenced[key] = true

		out = append(out, Finding{
			Kind:    FindingClaimDeprecated,
			Subject: subject,
			Side:    SideHead,
			Span:    claim.Span(),
			Message: fmt.Sprintf(
				"this change leaves nothing asserted about the %s of %s: every claim under it is retracted",
				predicate, subject,
			),
			Hint: "a model which no longer answers a question it used to answer is a legitimate state and a deliberate one — acknowledge it in the policy, or write what is known now",
		})
	}

	return out
}

// reviewDisappearances reports an id the merge base held and head does not.
//
// An id is assigned once and never reissued, so a thing which stopped existing
// is retired: it keeps its id, records why, and a reference written years ago
// still resolves to something which says what happened. An id which is simply
// gone from the files answers those references with silence, which is why the
// dangling ones are named here rather than left to be found by loading the
// model and reading the diagnostics.
//
// The one disappearance which is not reported is an id the merge base already
// recorded as retired and superseded. The record of where it went outlived the
// thing, which is the whole point of retiring, and removing it afterwards is a
// tidy-up rather than a deletion nobody can trace.
func reviewDisappearances(base, head *Graph) []Finding {
	var out []Finding

	for _, entity := range sortedEntities(base) {
		id := entity.ID()

		if _, ok := head.Entity(id); ok {
			continue
		}
		if node, ok := entity.(*SemanticNode); ok {
			if retirement, retired := node.Retirement(); retired {
				if _, superseded := retirement.SupersededBy(); superseded {
					continue
				}
			}
		}

		out = append(out, disappearedFinding(head, id, entity.Span(), "the "+family(entity)))
	}

	for _, claim := range sortedClaims(base) {
		id, named := claim.ID()
		if !named {
			continue
		}
		if _, ok := head.Claims().Claim(id); ok {
			continue
		}

		out = append(out, disappearedFinding(head, id, claim.Span(),
			fmt.Sprintf("the claim about the %s of %s", claim.Predicate(), claim.Subject())))
	}

	return out
}

// disappearedFinding is one id the merge base held and head does not, with
// every reference head still makes to it.
func disappearedFinding(head *Graph, id ID, span Span, what string) Finding {
	var dangling []Dangling
	for reference := range head.References(id) {
		dangling = append(dangling, Dangling{
			From:     reference.From,
			Relation: reference.Relation,
			Span:     reference.Span,
		})
	}

	message := fmt.Sprintf("%s is gone from this revision: %s was removed rather than retired", id, what)
	if len(dangling) > 0 {
		message += fmt.Sprintf(", and %s still names it", plural(len(dangling), "reference"))
	}

	return Finding{
		Kind:     FindingIDDisappeared,
		Subject:  id,
		Side:     SideBase,
		Span:     span,
		Message:  message,
		Hint:     fmt.Sprintf("a thing which stopped existing keeps its id: `dfcad retire %s --reason ...` records what happened and leaves every reference resolving", id),
		Dangling: dangling,
	}
}

// family is the word for which family an entity belongs to, which is what a
// message about a missing id calls it.
func family(entity Entity) string {
	switch entity.(type) {
	case *SemanticNode:
		return "node"
	case *Vertex:
		return "vertex"
	case *Edge:
		return "edge"
	case *Loop:
		return "loop"
	default:
		return "entity"
	}
}

// plural renders a count with its noun, pluralised the one way this package
// needs.
func plural(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// boundaryVertices are the ids of the corners a node is bounded by, in the
// order the boundary reaches them.
func boundaryVertices(graph *Graph, node *SemanticNode) []ID {
	var out []ID
	for vertex := range graph.Vertices(node) {
		out = append(out, vertex.ID())
	}
	return out
}

// fingerprint identifies one claim across two revisions of a model.
//
// A claim which wrote an id is that id, which is the whole reason an id is
// written: something references it, and it is the same claim in both revisions
// however the file moved around it. A claim which wrote none is identified by
// everything about it except its value — its subject, its predicate, and the
// provenance which says where the number came from — because that is exactly
// the reading which makes rewriting the value in place visible as a rewrite
// rather than as one claim leaving and another arriving.
func fingerprint(claim *Claim) string {
	if id, ok := claim.ID(); ok {
		return "id\x00" + string(id)
	}
	return strings.Join([]string{
		"anonymous",
		string(claim.Subject()),
		claim.Predicate(),
		claim.Source(),
		string(claim.Method()),
		claim.Date().UTC().Format(time.RFC3339),
	}, "\x00")
}

// claimIndex is every claim of one revision by its fingerprint.
func claimIndex(graph *Graph) map[string]*Claim {
	out := make(map[string]*Claim)
	for claim := range graph.Claims().All() {
		out[fingerprint(claim)] = claim
	}
	return out
}

// sortedNodes is every semantic node of a revision in id order, which is what
// makes a walk over two revisions produce the same list on every run whatever
// order either was read in.
func sortedNodes(graph *Graph) []*SemanticNode {
	var out []*SemanticNode
	for node := range graph.Nodes().All() {
		out = append(out, node)
	}
	slices.SortStableFunc(out, func(a, b *SemanticNode) int {
		return strings.Compare(string(a.ID()), string(b.ID()))
	})
	return out
}

// sortedEntities is every entity of a revision — both families — in id order.
func sortedEntities(graph *Graph) []Entity {
	var out []Entity
	for node := range graph.Nodes().All() {
		out = append(out, node)
	}
	for vertex := range graph.Topology().Vertices() {
		out = append(out, vertex)
	}
	for edge := range graph.Topology().Edges() {
		out = append(out, edge)
	}
	for loop := range graph.Topology().Loops() {
		out = append(out, loop)
	}
	slices.SortStableFunc(out, func(a, b Entity) int {
		return strings.Compare(string(a.ID()), string(b.ID()))
	})
	return out
}

// sortedClaims is every claim of a revision, ordered by the fingerprint which
// identifies it across both.
func sortedClaims(graph *Graph) []*Claim {
	var out []*Claim
	for claim := range graph.Claims().All() {
		out = append(out, claim)
	}
	slices.SortStableFunc(out, func(a, b *Claim) int {
		return strings.Compare(fingerprint(a), fingerprint(b))
	})
	return out
}

// sameValue reports whether two claim values state the same thing.
//
// The span is deliberately not part of it: the same number written a line
// further down the file is the same value, and a comparison which read the
// position would report every claim in a reordered file as rewritten.
func sameValue(a, b Value) bool {
	if a.Shape() != b.Shape() || a.Unit() != b.Unit() {
		return false
	}

	switch a.Shape() {
	case ShapeScalar:
		left, _ := a.Scalar()
		right, _ := b.Scalar()
		return left == right
	case ShapeCoordinate:
		left, _ := a.Coordinate()
		right, _ := b.Coordinate()
		return slices.Equal(left, right)
	case ShapeText:
		left, _ := a.Text()
		right, _ := b.Text()
		return left == right
	case ShapeTransform:
		left, _ := a.Transform()
		right, _ := b.Transform()
		return left.Translation == right.Translation &&
			left.Rotation == right.Rotation &&
			left.Scale == right.Scale
	default:
		// A value nothing could be read from has no shape, and two of them say
		// nothing different from one another.
		return true
	}
}

// writtenValue renders a value the way the file writes it, so that a message
// about a number quotes the number somebody would search the file for.
func writtenValue(value Value) string {
	unit := ""
	if value.Unit() != "" {
		unit = " " + string(value.Unit())
	}

	switch value.Shape() {
	case ShapeScalar:
		number, _ := value.Scalar()
		return decimal(number) + unit
	case ShapeCoordinate:
		components, _ := value.Coordinate()
		written := make([]string, 0, len(components))
		for _, component := range components {
			written = append(written, decimal(component))
		}
		return "(" + strings.Join(written, " ") + ")" + unit
	case ShapeText:
		text, _ := value.Text()
		return quoteText(text)
	case ShapeTransform:
		transform, _ := value.Transform()
		return fmt.Sprintf("a transform translated (%s %s %s) and scaled %s",
			decimal(transform.Translation[0]),
			decimal(transform.Translation[1]),
			decimal(transform.Translation[2]),
			decimal(transform.Scale),
		)
	default:
		return "nothing readable"
	}
}

// writtenIDs renders a list of ids for a message, and says so when it is empty.
func writtenIDs(ids []ID) string {
	if len(ids) == 0 {
		return "nothing"
	}

	written := make([]string, 0, len(ids))
	for _, id := range ids {
		written = append(written, string(id))
	}
	return strings.Join(written, ", ")
}

// joinKinds renders the closed set of kinds for a message about a name which is
// not one of them.
func joinKinds(kinds []FindingKind) string {
	written := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		written = append(written, string(kind))
	}
	return strings.Join(written, ", ")
}
