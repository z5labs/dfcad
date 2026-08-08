// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/z5labs/dfcad"
)

const reviewUsage = `dfcad review — report the changes in this revision which need an explanation.

Usage:

	dfcad review [flags]

Every rule ` + "`dfcad check`" + ` runs constrains one revision. These need two. A wall
which quietly moved, a measurement retracted with nothing in its place and an id
which is simply gone are each perfectly loadable models and each a change a
reviewer would want to have been told about, and none of them can be seen
without the revision the change was made against.

The comparison is against the merge base and not against the tip of the branch
being merged into: those two differ the moment anything else lands there, and a
review against the tip would report everybody else's work as part of this
change.

Flags:

	--against <ref>      the branch this revision is being merged into; the
	                     merge base of it and HEAD is what the model is compared
	                     against (default "origin/HEAD")
	--base-root <dir>    compare against a model in this directory instead of
	                     against a revision, which is the escape hatch for a
	                     comparison git cannot be asked about. Nothing is
	                     attributed to a commit under it
	--policy <k>=<r>     what one kind of finding means: failure, warning or
	                     ignored; repeat for more
	--annotate <path>    write a Markdown summary of the findings to path, which
	                     is what $GITHUB_STEP_SUMMARY makes a reviewer see; ` + "`-`" + `
	                     writes it to stderr

The checks, and what each is called in --policy:

	boundary-moved-without-claim
	    A physical boundary moved and nothing new was measured. A corner which
	    moved was surveyed again, and a survey is a claim with its own source,
	    method, date and accuracy — so rewriting the old claim's value in place
	    leaves the model asserting that the first survey found the second
	    number. Both ways a boundary moves are reported: a corner rewritten in
	    place, and a boundary drawn round different corners. It warns by
	    default, because it is the one of the three which is routinely
	    legitimate.

	claim-deprecated-without-replacement
	    A claim was retracted with nothing standing in its place — either its
	    replacement is not in this revision, which is the load-time rule seen
	    across two of them, or the retraction left nothing at all asserted about
	    a subject and a predicate. It fails by default.

	id-disappeared-without-supersession
	    An id the merge base held is gone. An id is assigned once and never
	    reissued, so a thing which stopped existing is retired and keeps its id;
	    one which is simply removed takes every reference to it with it, and
	    each of those is named. It fails by default.

A policy is what makes this usable rather than something to route around: a
change which genuinely re-surveyed a room is a change somebody meant, and
` + "`--policy boundary-moved-without-claim=ignored`" + ` is how to say so once, in the
invocation, rather than by not running the command. A finding a policy ignored
is still in the result — a check silently switched off is one nobody remembers
is off — and is reported nowhere else.

The command needs the history reaching back to the merge base. A shallow
checkout does not have it, and git answers with the commit its history was cut
off at rather than with an error, so a shallow clone is refused and told what to
fetch instead of quietly reviewing this change against the beginning of time.
` + "`fetch-depth: 0`" + ` on actions/checkout is what a CI job needs, which is what the
containerized pipeline requires anyway.

A revision which changed nothing suspicious reports no findings and succeeds.
A model root which is not inside a git working tree is a load failure, and so is
one whose merge base does not load: a review needs both revisions, and reporting
half a comparison would say that the half it read was all there was.

` + globalFlagsHelp + `
` + outputContractHelp + `
The object review writes carries "comparison" — which two revisions were read —
"policy", the ruling every check ran under, "summary", how many findings there
were and of what weight, and "findings": one entry per change which needs an
explanation, each naming the commit which introduced it.
`

// MalformedPolicyError is a --policy which is not a kind and a ruling.
type MalformedPolicyError struct {
	// Written is the value as it was given.
	Written string
}

// Error implements [error].
func (e MalformedPolicyError) Error() string {
	return fmt.Sprintf("malformed policy %q: want <check>=<ruling>", e.Written)
}

// reviewResult is the object review writes to stdout.
type reviewResult struct {
	envelope

	// Comparison is which two revisions were read.
	Comparison reviewComparison `json:"comparison"`

	// Policy is the ruling each check ran under, by its name. Every check is
	// here rather than only the ones a flag named, because what a run did about
	// the checks it did not report is exactly what a reader of a green run needs
	// to know.
	Policy map[string]string `json:"policy"`

	// Summary is how many findings there were and of what weight.
	Summary reviewSummary `json:"summary"`

	// Findings is one entry per change which needs an explanation, ordered by
	// check, then by subject, then by position. Empty rather than null when
	// nothing was found.
	Findings []dfcad.Finding `json:"findings"`
}

// reviewComparison is which two revisions a run read.
//
// It is in the result rather than left to be inferred from the flags, because
// the merge base is derived: a caller reading a review has to be able to say
// which commit the change was measured against without recomputing it.
type reviewComparison struct {
	// Against is the revision the merge base was taken with, as it was written,
	// and is empty for a comparison against a directory.
	Against string `json:"against,omitempty"`

	// Base is the merge base: the full object name of the commit, or the
	// directory a --base-root run read instead.
	Base string `json:"base"`

	// Head is the full object name of the revision under review, and is empty
	// for a comparison against a directory.
	Head string `json:"head,omitempty"`

	// Files is how many files the range between them touched, which is what a
	// finding is attributed through. It is zero for a comparison with no
	// history to read.
	Files int `json:"files"`
}

// reviewSummary is how many findings a run reported and of what weight.
//
// The three counts are of findings rather than of checks, because a finding is
// what a reviewer acts on: one change which moved four corners is four things to
// look at and not one check which failed.
type reviewSummary struct {
	// Findings is how many there were, ignored ones included.
	Findings int `json:"findings"`

	// Failures is how many the policy ruled a failure, which is what decides the
	// exit code.
	Failures int `json:"failures"`

	// Warnings is how many it ruled a warning.
	Warnings int `json:"warnings"`

	// Ignored is how many it acknowledged, which are reported here and nowhere
	// else.
	Ignored int `json:"ignored"`
}

// runReview is the review command.
func runReview(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	against := flags.String("against", "origin/HEAD", "")
	baseRoot := flags.String("base-root", "", "")
	annotate := flags.String("annotate", "", "")
	rulings := &repeated{}
	flags.Var(rulings, "policy", "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(arguments) > 0 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments}, stderr, true)
	}

	policy, err := reviewPolicy(*rulings)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	head, refused := loadGate(cmd, globals, stderr)
	if refused {
		return exitLoad
	}

	base, comparison, history, err := previous(cmd, globals, *against, *baseRoot, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}
	defer func() {
		if comparison.cleanup != nil {
			comparison.cleanup()
		}
	}()

	result := reviewResult{
		envelope:   newEnvelope(cmd.name),
		Comparison: comparison.reviewComparison,
		Policy:     rulingsUnder(policy),
		Findings:   dfcad.Review(base, head, policy, history),
	}
	result.Summary = summarise(result.Findings)

	// A finding is a problem in a change somebody made, so it is rendered for
	// whoever made it on every run and in every format. The struct above is the
	// machine form of the same finding; neither is produced by parsing the
	// other. The ones a policy acknowledged are left out here and are in the
	// result, which is what "ignored" means.
	render(diagnoseFindings(result.Findings), stderr)

	if *annotate != "" {
		if err := annotated(*annotate, result, stderr); err != nil {
			_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
			return exitLoad
		}
	}

	reportReview(result, globals, stderr)

	if err := emit(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %v\n", cmd.name, err)
		return exitLoad
	}

	if result.Summary.Failures > 0 {
		return exitCheck
	}
	return exitSuccess
}

// comparison is what was compared against, and what has to be cleaned up after.
type comparison struct {
	reviewComparison

	// cleanup removes the tree a revision was extracted into, and is nil for a
	// comparison which extracted nothing.
	cleanup func()
}

// previous loads the revision head is being compared against.
//
// It is the merge base with --against, extracted into a directory of its own,
// or the directory --base-root names. The two are one function because what
// comes back is the same three things either way, and a caller which had to
// know which of them it got would be a caller which branches on a flag.
func previous(cmd command, globals *globals, against, baseRoot string, stderr io.Writer) (*dfcad.Graph, comparison, dfcad.History, error) {
	if baseRoot != "" {
		// A relative path is resolved against the model root, as every path a
		// command is given is: the two models being compared are two revisions
		// of one tree, and a directory named relative to the process would mean
		// something different the moment --root did.
		root := globals.resolve(baseRoot)

		graph, found := dfcad.LoadGraph(root)
		if render(found, stderr) {
			return nil, comparison{}, nil, ComparisonError{Revision: root}
		}

		return graph, comparison{reviewComparison: reviewComparison{Base: root}}, nil, nil
	}

	repository, err := dfcad.OpenRepository(globals.Root)
	if err != nil {
		return nil, comparison{}, nil, err
	}

	prefix, err := repository.Prefix(globals.Root)
	if err != nil {
		return nil, comparison{}, nil, err
	}

	revision, err := repository.Resolve("HEAD")
	if err != nil {
		return nil, comparison{}, nil, err
	}

	mergeBase, err := repository.MergeBase("HEAD", against)
	if err != nil {
		return nil, comparison{}, nil, err
	}

	extracted, err := os.MkdirTemp("", "dfcad-review-")
	if err != nil {
		return nil, comparison{}, nil, err
	}
	out := comparison{
		reviewComparison: reviewComparison{Against: against, Base: mergeBase, Head: revision},
		cleanup:          func() { _ = os.RemoveAll(extracted) },
	}

	if err := repository.Extract(mergeBase, extracted); err != nil {
		out.cleanup()
		return nil, comparison{}, nil, err
	}

	if globals.Verbosity >= verbosityProgress {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: comparing against the merge base %s\n", cmd.name, mergeBase)
	}

	root := filepath.Join(extracted, filepath.FromSlash(prefix))

	graph, found := dfcad.LoadGraph(root)
	if render(found, stderr) {
		out.cleanup()
		return nil, comparison{}, nil, ComparisonError{Revision: mergeBase}
	}

	log, err := repository.Log(mergeBase, revision)
	if err != nil {
		out.cleanup()
		return nil, comparison{}, nil, err
	}
	out.Files = log.Len()

	// Both trees are attributed through the one log: a finding about something
	// head no longer holds points into the extracted tree, and its file is
	// where the repository holds it once the prefix is put back.
	history := dfcad.Histories{
		log.Within(globals.Root, prefix),
		log.Within(root, prefix),
	}

	return graph, out, history, nil
}

// ComparisonError is a revision which does not load, and so is not something to
// compare against.
//
// A review needs both revisions. Reporting the findings of a comparison against
// half a model would say that the half which loaded was all there was, and
// every id in the rest of it would read as an id which disappeared.
type ComparisonError struct {
	// Revision is the merge base, or the directory a --base-root run named.
	Revision string
}

// Error implements [error].
func (e ComparisonError) Error() string {
	return fmt.Sprintf("the revision being compared against, %s, does not load: there is nothing to compare this one with", e.Revision)
}

// reviewPolicy is the policy the --policy flags ask for.
//
// A name which is not a check and a ruling which is not a ruling are both usage
// errors rather than a run under a policy which quietly did something else: a
// gate configured with a typo would pass on the strength of it.
func reviewPolicy(written []string) (dfcad.Policy, error) {
	policy := dfcad.DefaultPolicy()

	for _, entry := range written {
		name, ruling, ok := strings.Cut(entry, "=")
		if !ok {
			return dfcad.Policy{}, MalformedPolicyError{Written: entry}
		}

		kind, err := dfcad.ParseFindingKind(strings.TrimSpace(name))
		if err != nil {
			return dfcad.Policy{}, err
		}

		parsed, err := dfcad.ParseRuling(strings.TrimSpace(ruling))
		if err != nil {
			return dfcad.Policy{}, err
		}

		policy = policy.With(kind, parsed)
	}

	return policy, nil
}

// rulingsUnder is the ruling every check ran under, by its name.
func rulingsUnder(policy dfcad.Policy) map[string]string {
	out := make(map[string]string, len(dfcad.FindingKinds()))
	for _, kind := range dfcad.FindingKinds() {
		out[string(kind)] = string(policy.Ruling(kind))
	}
	return out
}

// summarise counts the findings by what the policy said about each.
func summarise(findings []dfcad.Finding) reviewSummary {
	summary := reviewSummary{Findings: len(findings)}

	for _, finding := range findings {
		switch finding.Ruling {
		case dfcad.RulingFailure:
			summary.Failures++
		case dfcad.RulingWarning:
			summary.Warnings++
		case dfcad.RulingIgnored:
			summary.Ignored++
		}
	}

	return summary
}

// diagnoseFindings is the findings as diagnostics, which is the rendering for
// whoever wrote the change. A finding the policy ignored is not among them.
func diagnoseFindings(findings []dfcad.Finding) []dfcad.Diagnostic {
	out := make([]dfcad.Diagnostic, 0, len(findings))
	for _, finding := range findings {
		if finding.Ruling == dfcad.RulingIgnored {
			continue
		}
		out = append(out, finding.Diagnostic())
	}
	return out
}

// annotated writes the Markdown summary a reviewer reads.
//
// It is a file rather than something on stdout because stdout is the machine
// contract and carries one JSON object; and it is Markdown rather than a
// workflow command because a summary appended to $GITHUB_STEP_SUMMARY is
// rendered on the run a reviewer opens from the pull request, whichever forge
// the run is on. The path is resolved against the working directory rather than
// the model root: it is an artifact of the run and not part of the model.
func annotated(path string, result reviewResult, stderr io.Writer) error {
	rendered := summaryMarkdown(result)

	if path == "-" {
		_, err := io.WriteString(stderr, rendered)
		return err
	}

	// Appended rather than truncated, because $GITHUB_STEP_SUMMARY is one file
	// per step and a run which wrote a summary before this one meant to keep it.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}

	// Closed exactly once, and which error comes back says which thing went
	// wrong: a write which failed is reported as itself, and a write which
	// succeeded has only reached the kernel, so a full disk is reported by the
	// close or by nothing at all.
	if _, err := io.WriteString(file, rendered); err != nil {
		_ = file.Close()
		return err
	}

	return file.Close()
}

// summaryMarkdown is the findings as a table a reviewer reads.
func summaryMarkdown(result reviewResult) string {
	var out strings.Builder

	out.WriteString("## dfcad review\n\n")

	if result.Comparison.Base != "" {
		fmt.Fprintf(&out, "Compared against `%s`.\n\n", result.Comparison.Base)
	}

	reported := slices.DeleteFunc(slices.Clone(result.Findings), func(finding dfcad.Finding) bool {
		return finding.Ruling == dfcad.RulingIgnored
	})

	if len(reported) == 0 {
		out.WriteString("No change in this revision needs an explanation.\n")
		if result.Summary.Ignored > 0 {
			fmt.Fprintf(&out, "\n%s acknowledged by the policy.\n", plural(result.Summary.Ignored, "finding"))
		}
		return out.String()
	}

	fmt.Fprintf(&out, "%s: %d failing, %d warning.\n\n",
		plural(result.Summary.Findings, "finding"), result.Summary.Failures, result.Summary.Warnings)

	out.WriteString("| Ruling | Check | Subject | Where | Commit | What |\n")
	out.WriteString("|---|---|---|---|---|---|\n")

	for _, finding := range reported {
		fmt.Fprintf(&out, "| %s | `%s` | `%s` | `%s` | %s | %s |\n",
			finding.Ruling,
			finding.Kind,
			finding.Subject,
			finding.Span.Start,
			markdownCommit(finding.Commit),
			escapeCell(finding.Message),
		)
	}

	return out.String()
}

// markdownCommit is how a finding's commit reads in the table, and says so when
// there is none.
func markdownCommit(commit dfcad.Revision) string {
	if !commit.Named() {
		return "—"
	}
	return "`" + commit.Short() + "`"
}

// escapeCell keeps a message inside its table cell: a pipe would end it and a
// line feed would end the row.
func escapeCell(message string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ").Replace(message)
}

// reportReview renders a run for a person, on stderr.
//
// Nothing here reaches stdout, in any format and at any verbosity: stdout is the
// same bytes whether or not anybody asked to read the run.
func reportReview(result reviewResult, globals *globals, stderr io.Writer) {
	if !globals.human() {
		return
	}

	summary := result.Summary

	_, _ = fmt.Fprintf(stderr, "%s: %d failing, %d warning, %d acknowledged\n",
		plural(summary.Findings, "finding"), summary.Failures, summary.Warnings, summary.Ignored)
}
