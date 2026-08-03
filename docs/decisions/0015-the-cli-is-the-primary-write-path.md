# 0015. The CLI is the primary write path

**Status:** Accepted

## Context

A read-only engine is the safer design, and it is the one this decision rejects. The
argument for read-only is strong: the source tree is the authored record, humans edit it
with the tools they already have, and a tool that only reads can never corrupt anything. A
write path is a second author of the same file, and two authors of one file is a category
of problem that does not exist until you create it.

It is rejected because of who is actually writing these files.

The format is nested s-expressions. Nested s-expressions are unpleasant to hand-edit, and
the unpleasantness is not aesthetic — it is that the failure mode is a misplaced paren. A
paren in the wrong place does not produce an error at the paren; it produces a structure
that is well-formed and wrong, and the diagnostic lands somewhere else entirely, often at
the end of the file. A human loses a few minutes to that. An LLM author loses a debugging
loop: it writes the entity, the load fails at a position that does not correspond to its
mistake, it revises the wrong thing, and it burns a round trip on a problem that has
nothing to do with the model. That loop is pure waste, and it is waste in the interaction
that this engine exists to make cheap.

Text editing has worse failure modes than parens, too. A reference to a node that does not
exist is only a problem at load; so is a claim whose predicate is not registered, an id
that duplicates another, a type not permitted for a kind. Every one of those is a mistake
that the engine could have refused at the moment of writing, and instead discovers later,
in a file that has already been saved and possibly committed.

The counter-argument stands, and it is the real cost: once the CLI writes, a file has two
authors. If they disagree about formatting, every CLI edit produces a formatting diff over
hand-edited regions and every hand edit produces one over CLI-written regions, and the
history becomes noise. That is not hypothetical; it is what happens by default.

## Decision

**The command line interface is the primary write path.** Adding a node, retiring one,
adding a claim, correcting one, deprecating one, authoring geometry and applying a batch of
edits are all commands. The expected way to change a model is to run one.

**Every write validates before any byte reaches disk.** The command constructs the change,
loads the model as it would be after the change, and validates it — syntax, registry
references, id uniqueness, reference resolution — and refuses if the result would not load.
What this buys is stated plainly: no syntax errors, no dangling references, no unregistered
predicates, no duplicate ids, and no tokens spent debugging parens that a tool could have
placed correctly.

**The CLI always emits canonical form.** Every file it writes is written as the canonical
printer would print it, in full — not patched in place, not appended to. This is the
mitigation for the two-author problem: because the CLI's output is exactly what `fmt`
produces, CLI edits and hand edits converge instead of fighting. A hand-edited file passed
through `fmt` is byte-identical to the same content written by the CLI, so the two authors
cannot develop competing house styles.

**Hand editing stays fully supported.** The files are text and they are the record. A person
may edit them with anything, and `fmt` and `fmt --check` exist so that hand-edited files
return to canonical form — with `fmt --check` in CI to keep the convergence honest rather
than aspirational.

**Writes are all-or-nothing**, which is a decision in its own right
([0016](./0016-writes-are-all-or-nothing.md)).

## Consequences

An authored change is validated at the moment it is made, by the tool making it, with the
whole model in hand. The class of error that used to be found at the next load is found
before the file changes.

An LLM author gets a structured interface instead of a text format. It emits a command with
named arguments and gets either a written file or a diagnostic about the model — never a
diagnostic about punctuation. That is the difference the write path exists to buy.

Formatting stops being a source of diff noise, because there is only one formatting. A diff
shows what changed semantically, which is what makes review of these files worth doing.

Round-tripping becomes a property the whole system depends on, not just the printer: what
the CLI writes must parse back to what it intended to write, and that is tested as a
property rather than against expected strings.

The CLI's write commands are a real API surface, with the same stability obligations as the
output contract ([0014](./0014-the-machine-output-contract-is-part-of-the-interface.md)).

Because writes go through validation, a model in the tree that fails to load can only have
got there by hand — which narrows the search when one does.

## Cost

Two authors of the same file, permanently. Canonical form makes them converge rather than
fight, but it does not make the problem go away: a hand edit that is not run through `fmt`
before the next CLI write will be reformatted by it, and that reformatting lands in
somebody's diff.

Comments are the sharp edge of that. Anything the canonical printer does not preserve
exactly is at risk of being rewritten by a CLI write, and preserving hand-written comments
and their attachment points through a full canonical rewrite is genuinely hard.

The CLI must cover the surface. A change that has no command has to be made by hand, which
puts the author back in the position the write path exists to avoid — and every new schema
feature now needs a command as well as a loader.

Concurrency. Two writes to the same file are a conflict, and the engine has no lock and no
daemon, so this is handled by refusing rather than by coordinating.

A write is not cheap. Validating the whole model before every edit means a batch of edits
applied one command at a time pays the load cost repeatedly, which is why applying a batch
from an operation file exists as its own command.

## What would reverse it

A format that is pleasant and safe to hand-edit would remove most of the argument. That is
not a small change — it is a different file format — and it would have to keep the
properties the s-expression format was chosen for.

Evidence that the write path is not actually used, or that authors route around it, would
mean the cost is being paid for nothing.

Withdrawing the write path is comparatively cheap in data terms: the files remain valid and
hand-editable, because they were always text and always canonical. What would be lost is
the guarantee about how they got that way — that every change was validated against the
whole model before it was written. Nothing in the tree records which edits came through the
CLI, so after the fact there is no way to tell a validated change from an unvalidated one.
