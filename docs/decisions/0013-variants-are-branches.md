# 0013. Variants are branches

**Status:** Accepted

## Context

Design work is comparative. Somebody always wants to see the scheme with the parking on the
north side against the scheme with it on the south, or the massing before and after the
setback changed, or phase one against the built-out condition. The model has to support
that somehow.

The obvious way to support it is a dimension in the schema. Give every node and every claim
an optional `scenario`, `variant` or `phase`, let a query filter on it, and both schemes
live in one tree.

That dimension is a trapdoor. It starts as an optional field with a default, which means
every existing node is implicitly in the base scenario, which means the semantics of every
untagged node now depend on a scenario the file does not mention. Claim resolution grows a
scenario term: given two claims for the same predicate, one tagged and one not, which wins,
and does the answer depend on which scenario you asked about? Containment grows one too —
is a node in the base scenario contained by a parent that exists only in a variant?
Uniqueness of ids becomes uniqueness within a scenario, except for the nodes shared across
scenarios, which are unique globally. Every check has to state which scenarios it applies
to. Every query grows a parameter, and every answer grows a qualifier.

The combinatorics arrive next. Two independent variants are four combinations, and the
schema has no way to say whether a given pair is meaningful, so the model contains states
nobody designed. Then somebody asks for a variant of a variant.

Meanwhile the repository already has the feature. A model is plain text under version
control ([README](../../README.md)). A branch is an isolated, complete, named alternative;
a diff between two branches is the comparison; a merge is adopting one; deleting the branch
is discarding it. All of that machinery exists, is understood by everyone, and has better
tooling than anything that would be built here.

## Decision

**There is no scenario, variant, phase or alternative dimension in the schema.** No node,
claim, frame or registry entry carries one, and no query takes one as a parameter. A loaded
model is one model — the state of the tree that was read.

**A proposal is a branch, and reviewing it is reviewing a pull request.** An alternative
scheme is a branch off the model. The comparison is the diff. Adopting it is a merge, and
the merge is where the review happens: the checks run, the conflicts surface, and a human
approves. Abandoning it is deleting the branch.

**Time is claims, not variants.** A design that changed is not two variants; it is claims
that were superseded, with their dates and their supersession recorded
([0007](./0007-rank-is-closed.md)). The history of what the model asserted lives in the
claim record and in the commit history, both of which are already there. A `phase` field
would be a third, worse copy of it.

**What this gives up is stated plainly: simultaneous N-way comparison.** The engine cannot
answer "give me the area of this space under each of these five schemes" in one invocation,
because only one scheme is loaded at a time. The available answer is five invocations, one
per branch, with the results compared outside the engine — which the machine output
contract ([0014](./0014-the-machine-output-contract-is-part-of-the-interface.md)) is
designed to make mechanical rather than manual.

## Consequences

The schema stays small, and every rule in it is unconditional. Resolution, containment,
uniqueness and every check mean one thing, with no scenario term to qualify them, and no
combination of dimensions to reason about.

A file says what the model is. Reading an entity file does not require knowing which
scenario is active, because there isn't one.

Comparison inherits version control's tooling and everyone's existing habits. Diffs, blame,
review threads, CI on a proposal, and a merge that either succeeds or conflicts — none of
which has to be built, documented or debugged here.

Isolation is genuine. Work on a proposal cannot affect the main model until it is merged,
which is a stronger property than a scenario tag provides: a tagged variant shares a file
with the base, so an edit to it can break the base by accident.

The checks are what gate adoption. A branch that does not load, or that fails an assertion,
does not merge, so an alternative is validated as a whole model rather than as a set of
tagged additions to one.

## Cost

N-way comparison is genuinely worse. Comparing five schemes means five checkouts or five
worktrees and a script to collect the results, where a scenario dimension would have been
one query. For anyone doing that comparison routinely, this is a real and repeated tax.

Long-lived branches drift and conflict. A proposal held open for months accumulates merge
pain against a model that moved, and the conflict resolution happens in text, on
s-expressions, which is not pleasant.

Cross-scheme queries are impossible from inside the engine. A check that wants to assert a
relationship *between* two alternatives — that they agree on the site boundary, say —
cannot be written, because only one of them is ever loaded.

Nothing in the model records that two branches are alternatives of the same thing. That
knowledge lives in branch names, pull request descriptions and people's heads, which is
weaker than a field, and it is not recoverable once the branches are gone.

Phasing, in particular, gets an answer that is thinner than the question. A model that
genuinely needs to describe a sequence of built states over time has to express it as
claims with dates and supersession, which is workable but is not the same as a first-class
phase.

## What would reverse it

A workflow whose primary act is N-way comparison — where the model exists to be evaluated
across many simultaneous alternatives rather than to describe one thing — would make this
the wrong shape. The first response is still not a schema dimension: it is a driver outside
the engine that loads several models and compares their outputs, which the stable machine
output contract already supports and which costs the schema nothing.

Genuine phasing, where one model must describe several built states simultaneously and
checks must run across them, is the strongest case for reconsidering, and it should be
reconsidered on its own terms rather than as a general variant dimension.

Adding the dimension later is a schema migration and a semantics migration at once. The
migration itself is mechanical — every existing node joins the default scenario — but every
rule that took an unconditional form has to be given a scenario term, and each of those is a
decision with no obviously right answer. Removing the dimension afterwards is worse: once
models contain scenario-tagged nodes, splitting them back into branches means deciding, for
every untagged node, which alternatives it belonged to, and the file no longer says.
