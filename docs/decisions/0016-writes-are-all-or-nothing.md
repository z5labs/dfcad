# 0016. Writes are all-or-nothing

**Status:** Accepted

## Context

Once the command line interface writes files ([0015](./0015-the-cli-is-the-primary-write-path.md)),
the question is what happens when a write goes wrong — and there are two ways for it to go
wrong that have nothing to do with each other.

The first is semantic. A command asks for a change that would produce a graph that does not
load: a claim referring to an unregistered predicate, a node whose type is not permitted
for its kind, an edge referencing a vertex that was just retired. The tempting behaviour is
to write it anyway and report the problem, on the grounds that the author can see the
diagnostic and fix it. That leaves a broken tree, and it leaves it broken in the worst
possible state — a change the author believed succeeded, because a file did change.

The second is mechanical. A write is interrupted: the process is killed, the disk fills,
the machine loses power. The naive implementation truncates the target file and writes into
it, so an interruption leaves a file containing the first half of a model. That file is not
recognisably broken — it is syntactically plausible, it may even load, and it is missing
entities nobody knows are missing. Version control does not save anyone here either: the
half-written file is in the working tree, and the next `git add -A` commits it.

Both failures share a property that makes them unusually bad in this system. The tree is
the store; there is no database and no transaction log ([README](../../README.md)). If a
partial write lands, there is nothing to roll back to except whatever the author has
committed, and no record anywhere of what the write was trying to do.

A multi-file change compounds it. Applying a batch of edits, or an edit that routes new
entities across several files, can succeed on the first file and fail on the second — which
leaves the tree in a state that no author asked for and no command can describe.

## Decision

**A change that would produce a graph that fails to load is refused.** The command
validates the post-change model in full — syntax, registry references, id uniqueness,
reference resolution — and if the result would not load, it reports diagnostics and exits
non-zero. Nothing is written. This is not a warning and there is no flag to override it.

**No partial file is ever written.** A file is written by writing its complete new contents
to a temporary file in the same directory, flushing it to durable storage, and renaming it
over the target. A rename within a directory is atomic, so an observer sees either the old
file or the new one, never a prefix of the new one. An interruption at any point leaves the
original intact and at worst a temporary file behind.

**A change spanning several files is all-or-nothing across all of them.** Every file's new
contents are prepared and validated as a set before any rename happens, and the renames are
performed only once all of them are ready. A failure while preparing leaves the tree
untouched.

**The unit of refusal is the whole command.** A batch of edits from an operation file
either applies completely or not at all: if any operation in the batch would produce a graph
that fails to load, none of the batch is written. There is no partial application and no
"apply what worked" mode.

**Refusal is a diagnostic, not an exception.** A refused write reports every independent
problem it found, each with its position, rather than the first one — the same
"collect, do not stop at the first" rule that applies to loading. An author fixing a
refused batch should not have to resubmit it once per mistake.

**A refused write is distinguishable by exit code.** It exits with the load-failure code
from [0014](./0014-the-machine-output-contract-is-part-of-the-interface.md), so a caller can
tell "the model would have been invalid" from "the invocation was wrong" without reading a
message.

## Consequences

The tree is always loadable, or it was made unloadable by hand. That is a strong enough
invariant to build on: `fmt --check` and the assertion checks can assume the tree parses,
and a load failure in CI points at a hand edit rather than at a tool.

An author never has to work out what a failed command did. It did nothing. There is no
inspection step after a failure and no partial state to reconcile.

Crash safety comes for free with the rename, including for the multi-file case, because the
renames are the only mutating steps and they happen last.

Batches are usable. Applying fifty edits as one operation is safe precisely because a
mistake in the fiftieth does not leave the first forty-nine applied, which is what makes an
operation file worth writing at all.

An LLM author gets a clean retry. A refused command means the model is unchanged, so the
correct response is to fix the command and reissue it — no reconciliation, no undo, no
checking what landed.

The engine needs no journal, no lock file and no recovery mode, because there is no state to
recover.

## Cost

Whole-file rewrites. Changing one claim rewrites the entire file it lives in, which is more
I/O than a targeted patch and is why the canonical printer's output must be stable — an
unstable printer would turn every write into a whole-file diff.

Whole-model validation per write. Every command loads and validates the full model to decide
whether to write, so cost scales with model size rather than with edit size. Batching exists
to amortise this and does not remove it.

All-or-nothing is unforgiving in bulk. A batch of fifty edits where one is wrong applies
none of them, and for an author who knows the other forty-nine are fine, that is real
friction — deliberately accepted, because the alternative is a partially-applied batch
nobody can describe.

Atomicity is bounded by the filesystem. Rename-over is atomic within a directory on ordinary
filesystems; across filesystems, or on network storage that does not honour it, the
guarantee is weaker, and the engine cannot detect every such case.

Temporary files can accumulate. A process killed between writing the temporary file and
renaming it leaves the temporary behind — harmless, but litter that something has to ignore
or clean up.

There is no way to save work in progress. An author who wants to write a half-finished
entity and come back to it cannot do it through the CLI; they hand-edit, which is the path
[0015](./0015-the-cli-is-the-primary-write-path.md) exists to make unnecessary.

## What would reverse it

A model large enough that whole-model validation per write is genuinely prohibitive would
argue for incremental validation — validating only the subgraph a change can affect. That is
an optimisation of how the check is performed, not a relaxation of the rule: the write is
still refused if the result would not load.

Nothing would argue for writing partial files. The atomic rename is cheap, and the failure
it prevents is unbounded.

Relaxing the refusal is the reversal that cannot be undone. Once invalid changes can be
written, the tree stops being reliably loadable, and every downstream assumption — checks,
`fmt --check`, diff-aware runs, the CLI's own read-modify-write cycle — has to grow a path
for a model that does not parse. Restoring the invariant afterwards means repairing every
tree that drifted while it was relaxed, by hand, without any record of which writes were the
invalid ones.
