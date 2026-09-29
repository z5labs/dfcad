# 0030. A read may assume a batch

**Status:** Accepted

## Context

Discussion #304 asks for `check`, a derivation or an export to be evaluated over the model
*plus* a batch of operations — in memory, writing nothing. The question it wants answered is
"if I made this change, would the rules still hold, and what would the artefact look like?",
asked before the change is made rather than after.

Nothing answers it. `apply --dry-run` is the nearest thing, and it answers a different
question. It loads the tree, applies the batch, prints the files the batch would change,
reads them back and validates the model they produce, and then reports the diff and the
effects: its payload is `command`, `dryRun`, `files`, `notices`, `operations`, `totals` and
`version`. No rule runs and nothing is derived. The model a rule or a derivation would need
is built — `Tx.prepare` (`write.go`) interprets exactly the model the batch would produce —
and thrown away on the next line (`_, diags = loadGraph(...)`), because a write only needs to
know whether it loads.

`apply --dry-run` is also a write in every respect but the last rename, so it takes the write
lock: `Begin` calls `acquire` before it reads anything. A hypothetical built on it therefore
cannot run beside another transaction, beside a write in progress, or on a checkout nobody may
write to. On a copy of `testdata/checks/satisfied` holding a `.dfcad.lock`,
`dfcad apply --root m --dry-run into-setback.json` exits 2 with
`m/.dfcad.lock: another transaction holds the model root`, while `dfcad check --root m` over
the same root answers. A question about a model which is not on disk should not be refused
because somebody is writing the one which is.

The pieces the feature needs already exist on the read side. Every read of the model goes
through one function, `loadGate` in `cmd/dfcad/list.go`, which `loadModel` wraps; there is no
second way a command gets a graph. And `loadGraph` (`graph.go`) is separate from `LoadGraph`
precisely "so that a model which is not on disk can be interpreted by the same passes in the
same order" — which is what `Tx.Commit` already does with the trees a change substituted.

Meanwhile the consumer that wants this most has built it by hand. Zaba505/mi-casa proves
that each of its checks can fail — that a rule which passes is passing because the model
satisfies it and not because it reads nothing — by copying the model and rewriting `.dfc`
text with about 320 lines of string and regex surgery. That surgery depends on the printer's
exact layout, so a change to canonical form that no reader of the model could notice breaks
it, and the mutations it writes are in the vocabulary of text rather than of the model.

Three shapes were live. Growing `apply --dry-run` to also run rules, derivations and exports
would teach one write command every read, keep the lock, and make "what does this batch do to
the files" and "what would the model then answer" one payload. Leaving it to consumers is the
harness above, once per consumer. The third is to let every read take the batch as input and
answer over the model it would produce, which is what is decided here.

The feature touches four accepted records, which is why it is decided before it is built:
[0013](./0013-variants-are-branches.md) (variants are branches, and "no query takes one as a
parameter"), [0016](./0016-writes-are-all-or-nothing.md) (what a write refuses),
[0009](./0009-derived-values-are-never-written-back.md) and
[0021](./0021-an-export-is-a-build-output-keyed-by-its-source-digest.md) (every derived value
reports the digest it was computed against, and an export is keyed by it), and
[0014](./0014-the-machine-output-contract-is-part-of-the-interface.md) (the envelope). This
record changes no code; #346 builds the engine half and #347 the command line half.

## Decision

**The flag is `--assume <file>`, and every read takes it.** Precisely: every command which
reads the model through the read gate and writes nothing to the authored tree. Today that is
`list-types`, `list-predicates`, `list-tolerances`, `list-frames`, `list-instances`,
`list-geometry`, `get`, `resolve`, `traverse`, `claims`, `conflicts`, `route`, `measure`,
`tessellate`, `buildable`, `site`, `plan`, `export`, `export-map` and `check`. The rule is
the definition and the list is its membership; a read added later takes the flag because it
is a read. (`list-predicates`, `list-tolerances` and `list-frames` landed after the discussion
was triaged, and are members for exactly that reason.)

It is not taken by:

- the write commands — `apply` and every single-operation write — which already have
  `--dry-run`, and for which a batch assumed beneath a batch applied would be two batches in
  one change;
- `fmt`, which formats bytes on disk, and a batch has none until it is printed;
- `version`, which reads no model;
- `review`, which compares two revisions, and a batch assumed on one side of that comparison
  is a third thing that is neither.

Two names were rejected. `--apply`, because on a read it reads as an instruction to write,
and a CI script searched for `apply` to find where it writes would find a read. `--overlay`,
because the word already names the region operation checks decide by — `overlay` in
`arrangement.go`, the overlays in `overlay.go`, and `dfcad check -h`: "a check which decides
by an overlay" — and one word meaning two things in one interface is a word nobody can search
for.

**The batch is an operation file.** It is [the operation file](../operation-file.md),
version 1, read by `ParseBatch`, from a path resolved against the model root or from standard
input when the path is `-` — exactly the file `apply` reads, found exactly the way `apply`
finds it. It carries exactly the operations `apply` accepts, and **no operation exists only
for a hypothetical**: a batch which may be assumed is a batch which may be applied, and the
other way round. The defining property, which the implementation stories test, is this: for
every batch `apply` accepts, `dfcad <command> --assume F` writes the same stdout and exits
with the same code as `dfcad apply F` followed by `dfcad <command>`, apart from the `assumed`
member below.

**Refusal is `apply`'s refusal.** A base tree which does not load, a file which is not a
batch, an operation the model refuses, and a batch whose result would not load are each
refused with the exit code and the stderr `apply --dry-run` gives for the same input — `3`
for an operation the model refuses, `2` for the rest — and the read does not run. Stdout is
what `apply --dry-run` writes for the same input: empty for an operation refused and for a
file which is not a batch; and where a load refused the base tree or the model the batch would
produce, [the refusal](../machine-output.md#the-refusal) object, under the command that was
run, as [0029](./0029-every-diagnostic-a-run-renders-is-written-in-its-answer-on-stdout.md)
requires of every run a load refused. This holds for every read, including the discovery
reads which, without the flag, answer over a refused tree with `"refused": true`, and `check`,
which writes its whole object with exit `2`. A model nobody could write is not answered
about: whether a batch loads is the question `apply --dry-run` already answers, and a read
under `--assume` does not answer it a second way.

**Nothing is written to the authored tree and nothing is locked.** No lock file, no entity
file, no temporary file beside one. An artefact command still writes its artefact, because
the file is its product
([0022](./0022-a-command-whose-product-is-a-file-answers-on-stdout.md)), and writes it where it
always does: beneath `.dfcad/export` under the digest below, or to `--out`.

**The digest is the digest of the tree the batch would produce.** It is what `DigestOf` would
compute were the batch written — the key `Graph.Digest` already calls the right one for a
graph interpreted from trees a write substituted (`cache.go`: "were those bytes on disk,
[DigestOf] would compute it"). It differs from the digest of the tree on disk whenever the
batch changes a byte. It is what every payload's `digest` reports, and what an artefact is
keyed by and stamped with ([0021](./0021-an-export-is-a-build-output-keyed-by-its-source-digest.md)).

Two alternatives were rejected. The base tree's digest files a hypothetical under the real
tree's key, so the export of a model that does not exist would be served as the export of the
one that does — the stale hit 0009 makes unrepresentable. No digest at all would send every
hypothetical export to `.dfcad/export/unknown/`, because an unknown `Digest` renders as
`unknown` and both `export` and `export-map` join it into the destination path; every
hypothesis would overwrite every other.

**The answer says it is hypothetical.** The envelope gains `assumed`, written only under
`--assume`:

```json
{
  "version": 2,
  "command": "check",
  "assumed": {
    "batch": "into-setback.json",
    "operations": 1,
    "base": "<digest of the tree read>",
    "digest": "<digest of the tree the batch would produce>"
  }
}
```

`batch` is the path as it was given, or `-`; `operations` is how many operations the batch
holds; `base` is the digest of the tree that was read; `digest` is the digest of the tree the
batch would produce, the same value the payload's own `digest` reports. Without the flag the
member is absent and stdout is byte-identical to what it is today, so the output contract
stays at version `2`: this is an added field, which
[the versioning rule](../machine-output.md#the-versioning-rule) allows. A caller which must
never act on a hypothetical tests `.assumed == null`. The refusal object carries no `assumed`,
as it carries no `digest`: each describes an answer, and a refusal has none.

**This is not a variant dimension.** Nothing enters the schema: no node, claim, frame or
registry entry learns that a hypothetical exists. Nothing is persisted: the batch is read,
assumed and forgotten, and the only thing left behind is an artefact the command would have
written anyway, under a content key. One model is loaded per invocation — the model the batch
would produce — so every rule, resolution and containment test still means one thing. The
batch is caller input, like any flag or argument, and not a parameter the model knows about.
An N-way comparison is still N invocations. 0013's decision, its consequences and its costs
are unchanged; 0013, 0016 and 0009 each gain a one-line pointer to this record and nothing
else.

## Consequences

Every read gains the flag at once and none gains it separately. Because every read already
loads through `loadGate`, that is where the batch is honoured: the gate interprets the model
the batch would produce, by the same `loadGraph` a write validates against, and hands the
command a graph it cannot tell from one read off disk. A command's own code does not change
to answer over a hypothetical, which is what makes the defining property true of every
command rather than of the ones somebody remembered.

The engine needs a way to interpret a batch without beginning a transaction. Today applying
a batch and holding the write lock are the same call; #346 separates them, so that the apply,
the print and the read-back `Tx.prepare` performs can run over a tree nobody holds. The
refusal rules above are what keep the two paths one answer: a batch either applies and loads
under both or is refused by both, in the same words.

A consumer proving a check can fail writes the batch that should make it fail, in the model's
own vocabulary, and runs `dfcad check --assume that.json`, expecting exit `1`. No copy of the
model, no text surgery, and nothing that depends on how the printer lays a file out. Those
batches are ordinary operation files, so the same file can later be applied for real and the
answer will not change.

A read under `--assume` runs where any read runs: beside a write, beside another read, on a
checkout nobody may write to. It reads the tree without the lock exactly as a read without the
flag does, and `assumed.base` names the tree it read, so a caller can tell which one that was.

The hypothetical key is a correct key and not a special one. An export written under
`--assume` is the export of the tree the batch would produce; if that batch is later applied,
the tree on disk has that digest and the export is a hit. The same holds for anything else a
derivation caches. Nothing has to be invalidated when a hypothetical becomes real, because the
key is the content.

Standard input is one input. A command which already reads it for its own purposes — `get -`,
reading ids — cannot also read the batch from it, so `--assume -` beside such an argument is a
usage error, exit `3`, rather than a guess at which consumer gets which bytes.

## Cost

**A second way to see a model which is not on disk.** Until now every answer on stdout was
about the tree at `--root` (and, for `review`, a base revision the caller named). Every
answer may now be about a tree that does not exist, carrying a `digest` that matches nothing
in any checkout. A caller which ignores `assumed` and acts on the answer — a CI job gating a
merge on `check --assume` exiting `0`, say — has acted on a model nobody wrote. The field is
the whole of the defence, and a field can be ignored.

**A hypothetical export directory per distinct batch under `.dfcad/export`.** Every batch
that changes a byte is a new key, so a consumer exporting fifty hypotheses accumulates fifty
directories. They are build outputs like any other key, bounded the same way — a prune that
keeps the current key and removes the rest, or deleting the directory outright — but a
workflow that explores many batches grows the directory faster than one that follows commits.

**A read given the flag pays for an apply, a print and a second interpretation.** The base
tree is interpreted so the batch can be applied against it, the changed files are printed and
parsed back, and the model they produce is interpreted again before the command does any work
of its own. On a large model that is roughly twice the load cost of the same read without the
flag, for every invocation, because nothing is kept between them.

The discovery reads give up, under the flag, their behaviour of answering through a tree the
load refused. That is deliberate, and it means the same `list-instances` exits `0` without the
flag and `2` with it over a base tree with an error in it.

The operation file becomes an input to reads as well as to writes. Its version was a write
contract; it is now also the contract of every read given `--assume`, so a change to it that
would have been reviewed as an authoring change is also a change to what twenty commands
accept.

What can be assumed is exactly what can be applied. A mutation `apply` cannot express — a
syntax error, a file which does not parse, a model that does not load — cannot be assumed
either, so a consumer testing that its model is refused for the right reason still needs a
fixture on disk.

## What would reverse it

Callers routinely acting on hypothetical answers without reading `assumed` would be evidence
that a member is too quiet a signal. The first response is a louder one on the same flag — an
exit code of its own — not removing the feature, which the harnesses it replaced would then be
rebuilt to provide.

A workflow whose primary act is comparing many batches over one base — where `--assume` is
invoked dozens of times per question — is 0013's reversal condition arriving by a new route,
and should be judged there rather than answered by growing this flag into a list of batches.
A driver outside the engine invoking the command once per batch remains the first answer.

The cost of the second interpretation becoming prohibitive would argue for interpreting only
what the batch can affect. That is an optimisation of how the model is produced, not a change
to which model is answered about, in the same way 0016's incremental validation would be.

Unwinding is cheap, which is part of why this is the shape chosen. Nothing is persisted in the
model, so removing the flag changes no data and no schema; the hypothetical export directories
are disposable build outputs; and a caller that never passed the flag sees no difference at
all. What would be expensive is the other direction — turning an assumed batch into a named,
stored alternative — which is exactly the variant dimension 0013 declines, and would have to
be decided there.
