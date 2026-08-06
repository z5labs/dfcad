# 0022. A command whose product is a file answers on stdout like every other command

**Status:** Accepted

## Context

[0020](./0020-export-is-a-boundary-and-the-closed-set-is-what-crosses-it.md) decides that an
exporter may live in this repository and fixes what crosses the boundary.
[0021](./0021-an-export-is-a-build-output-keyed-by-its-source-digest.md) decides where the
artefact lands, what keys it, and that it is byte-identical for identical input. Neither says
what the *command* reports, and until now the machine output contract
([0014](./0014-the-machine-output-contract-is-part-of-the-interface.md)) has never had to
describe a command whose product is not the object on stdout.

Every command in the contract so far is one of two shapes. A read computes an answer and the
answer *is* the object — `measure` writes the figures, `resolve` writes the value. A write
changes the authored tree and the object is an account of the change — which files, what
happened to each, what the change did to the model. An export is neither. It produces a file,
outside the authored tree, in a format this contract does not describe and cannot embed, so
the object on stdout can only ever be an account of something the caller has to go and read
elsewhere.

That is a new shape, and the two answers that suggest themselves are both wrong. **Writing
nothing to stdout** — the file is the result, so what is there to say — makes the first
command in this interface that a script cannot drive without looking at the filesystem, and
strands the one fact a caller most needs, which is the digest the artefact was derived from.
**Writing the artefact's bytes to stdout** breaks 0014 outright: stdout carries one JSON
object and nothing else, not behind a flag, not when stdout is a terminal, not on the first
run. 0021 names "ask for the bytes on stdout" in passing while ruling on destinations, which
is a loose end this record has to close rather than inherit.

**The shape is mostly already fixed by precedent, and the precedents disagree on one point.**
Three exist. `fmt` rewrites files it was pointed at and reports `files[].path`,
`files[].status` and `files[].diagnostics`. The write commands and `apply` change the tree and
report `dryRun`, `files[].status`, `files[].effects[]` and `files[].diff`. And the derived
commands — `measure`, `buildable`, `site` — report `derived`, saying whether there is an
answer at all, and `measure` reports `digest`, the tree the answer was computed against. An
export is a derived artefact written to a file, so it wants fields from both sides, and taking
them is not the hard part.

The point they disagree on is what a **refusal** writes.

- A refused write command "writes nothing at all to **stdout**. It produced no result, and an
  object describing a change that did not happen reads exactly like one describing a change
  that did."
- A refused `measure` or `buildable` still writes its object, with `derived` false, "so a
  caller reads why from the diagnostics on stderr rather than from an empty stream".

Both rules are right for their own family, and the reason they differ is one flag. A write
command under `--dry-run` emits a complete object describing files it did not write, and
`dryRun` is the *only* field distinguishing it from a run that did — `files[].status` says
`rewritten` either way. A refusal emitting the same object would be indistinguishable from a
success at every field a caller is likely to read, so the only safe thing is to emit nothing.
A derived command has no such mode. `derived` is not a flag the caller set; it is the answer
to whether there is an answer, and nothing else in the object contradicts it.

So "what does a refused export write" turns out not to be a question about exports. It is a
question about whether an artefact command takes `--dry-run`, and it has to be answered first.

## Decision

**An artefact-producing command answers on stdout like every other command. The artefact is
never the answer; the account of it is.**

Every command whose product is a file — an export today, anything else that writes a build
output tomorrow — writes one object of this shape, and the shape is documented once in
[`docs/machine-output.md`](../machine-output.md) rather than per command, exactly as the write
commands' shape is:

```json
{
  "version": 2,
  "command": "export",
  "derived": true,
  "digest": "9f2c1ab4c0d7e5f38a2b6109d4e7c8b5a3f10e29d6c4b8a70f5312cd9e846b7a",
  "files": [
    {
      "path": ".dfcad/export/9f2c1ab4…9e846b7a/model.ifc",
      "status": "written"
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `version`, `command` | — | The envelope, unchanged. An artefact command adds no envelope field; `command` names the subcommand and that is all a caller needs to know which payload follows. |
| `derived` | bool | Whether an artefact was produced. Written whatever the outcome, with the same meaning it has on `measure`, `buildable` and `site`. |
| `digest` | string, optional | The digest of the source tree the artefact was derived from, lower-case hex. Written on a refusal too. Absent for a model that was not read from disk. |
| `files` | array | One entry per file the artefact consists of, ascending by `path`, compared byte-wise. Empty rather than null. |
| `files[].path` | string | Where the file is, exactly as it would be opened. |
| `files[].status` | string | `written` or `unchanged`, and nothing else. |

**`files[]` describes files that are on disk, and never anything else.** `written` means this
run wrote the file. `unchanged` means the artefact for this key was already there and this run
left it in place — the ordinary outcome of re-exporting an unchanged tree, and, per 0021, a
statement about bytes rather than a guess, since an entry that cannot be verified is discarded
rather than served. There is no `failed`: **an artefact is all-or-nothing.** One run produces
its whole file set or none of it, so a run that could not finish leaves nothing behind that a
later run would read as the artefact for that key, and there is no half-written export to
describe.

**`--dry-run` does not apply, and `dryRun` is not a member of this shape.** The flag exists on
the write commands because a write edits authored source somebody has to review, and what it
buys is the `diff` — take the diff away and it degrades to "yes, I could do that". An artefact
command edits nothing: it lands in `.dfcad`, which 0021 makes ignored and disposable, so
`git status` after an export is clean and deleting the whole of it costs time and nothing
else. There is no consequence to insure against, the artefact is deterministic so the run is
repeatable, and there is no diff to show, because 0021 puts the review surface in a golden
fixture rather than at export time. What a dry run would report is a path and a status, and
computing the path means reading the whole tree and deriving the digest, which is most of the
work it claims to defer.

Dropping the flag is also what buys this shape its unambiguity, and that is the larger reason.
`--dry-run` would be the only thing that could ever put a file in `files[]` that is not on
disk. Without it, `derived` and `files[]` mean one thing each, and the write commands' rule
about refusals — an object describing a change that did not happen — has nothing to bite on.

**A refusal answers, with `derived` false and `files` empty.** An artefact command that read
the model and could not produce an artefact from it comes back with the object, so a caller
reads *why* from the diagnostics on stderr rather than from an empty stream, and reads the
digest of the tree it was refused against. The write commands' rule is honoured in the form
that actually matters — nothing in the object describes a file that was not written, because
there is no entry at all — rather than by suppressing the answer. This is the same treatment
`measure` gives a ring that does not close, and it is what makes `derived` a field worth
reading instead of a constant.

**Exit codes distinguish "nothing to emit" from "could not emit", the way `buildable` and
`site` already do:**

| Code | When |
|------|------|
| `0` | The command answered. Either the artefact exists — `derived` true, `files` naming it as `written` or `unchanged` — or the model held nothing this format carries, which is `derived` true with `files` empty. |
| `1` | The command could not produce the artefact from the model it read. `derived` false, `files` empty, `digest` written. |
| `2` | The model could not be read: the root is not there, the tree did not load, a file could not be read. |
| `3` | The invocation was wrong: a required flag missing, or a destination inside the authored tree, which 0021 refuses before anything is read. |

A model that exports to nothing is **exit `0`**, and it is the same judgement `buildable` makes
about a parcel its own setbacks consumed: the command answered, and the answer is that there
is nothing. Whether a given format has a meaningful empty artefact — a header with no contents
— is that format's exporter's business; if it writes one, the file appears in `files[]` like
any other. What is fixed here is that "wrote nothing and nothing went wrong" is `0` with
`derived` true, and never confused with `1`.

**A destination inside the authored tree is exit `3` and writes nothing to stdout.** It is a
fact about the invocation rather than about the model, it is decidable before a byte is read,
and 0014 already says a usage error produces no result object. That keeps 0021's refusal where
it belongs: a mistake in the command line, not a verdict on the model.

**The identifier manifest is evidence, and `--evidence` asks for it.** The per-node table
mapping each node id to the `GlobalId` 0004 derives for it is not written by default. Under
`--evidence` the object gains `identifiers`, an array of `{"id", "global-id"}` entries
ascending by `id`. Two reasons, and the second is the one that decides it:
[0017](./0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md) rules that an answer
carries what says how good it is and not what says who to argue with, and a manifest is neither
— it grows one entry per node, on the call whose answer is four fields; and every entry is
recomputable exactly, by anyone holding the model, from the node id and the pinned project
URL, because 0004 makes the derivation a pure function. Evidence a caller can reproduce
byte-for-byte from what it already has is the clearest case there is for not sending it by
default.

**The artefact never goes to stdout.** 0021 names "ask for the bytes on stdout" in passing
while ruling on destinations; that option does not exist. A caller who wants the bytes names a
destination outside the model root and reads the file, which is one line of shell and keeps
`dfcad ... | jq` meaning what it means for every other command. Nothing else 0021 decides is
disturbed: the authored tree is still closed, `.dfcad` is still the exception, and a
destination outside the root is still ordinary.

**Rejected: a size and a hash of each file in `files[]`.** Both were considered — a size is
free, and a hash of the artefact is the thing a publishing step would key on. Both were left
out. A caller holding the file can measure and hash it, the fields would be paid for on every
export by everyone who does not, and the digest that says whether the artefact is current is
`digest`, which is over the *input* and is the only one of the three that answers a question
the caller could not answer alone. The versioning rule makes adding a field cheap and removing
one a version change, so the asymmetry says wait.

## Consequences

The first artefact command is a story about a file format, not a story about the interface. Its
payload is this one, its exit codes are these, and the review question is whether the exporter
produces the bytes — not what it should print.

The machine output contract keeps its one invariant intact: stdout is one JSON object, for
every command, including the ones whose real product is somewhere else. A caller can drive
`dfcad` without ever special-casing a subcommand, and `dfcad export … | jq -r .digest` is a
sentence rather than an exception.

An artefact is discoverable without the caller reproducing 0021's key derivation. The path is
computed from the digest and the caller cannot predict it, so `files[].path` is the only
supported way to find what was just produced, and it is in the answer rather than in a log line
on stderr.

Re-exporting an unchanged tree is a first-class, reportable outcome rather than a no-op. A
build script sees `unchanged` and knows the artefact it has is the artefact this tree produces,
which is the cache-hit property 0021 gives the key, made visible.

The `derived` and `digest` fields now mean one thing across four commands rather than being
`measure`'s idiom. A caller that learned them on a dimensional query reads an export payload
without learning anything new, which is the whole argument for documenting the shape once.

## Cost

**An artefact command reports less than a write command, and somebody will ask for the
difference.** No `diff`, no `effects`, no size, no hash — an export answers with a path, a
status and a digest, and that is deliberately thin. The first person automating a publishing
step will want the artefact's own hash and will have to compute it, and they will be right that
the exporter had it.

**Dropping `--dry-run` costs a real habit.** "Show me what this would do" is the first thing a
careful person types before a command that writes, and on this one it is a usage error. The
answer — run it, it is disposable — is correct and it has to be learned, and it will read as
the interface being clever at the user's expense at least once. It also means there is no way
to validate a model against a target format's requirements without paying for the
serialisation, which is a cheap thing to want and now has no flag.

**`files[]` with two statuses will be asked to grow.** A format that writes a sidecar the
caller may supply themselves, a partial export somebody wants to inspect, a per-file warning —
each is a plausible argument for a third status, and each would weaken the all-or-nothing rule
that makes the absence of `failed` safe. The rule is load-bearing and it is not obviously so
from the outside.

**Answering on a refusal costs the property the write commands protect.** A caller that reads
`files` without reading `derived` sees an empty array on a refusal and on an empty model alike,
and will have to read the exit code or the field to tell them apart. That is exactly the
confusion the write commands avoid by writing nothing, traded for the digest and the
diagnostics being reachable, and it is a trade rather than a free win.

**The identifier manifest behind `--evidence` will be found late.** Somebody reconciling an
exported file against the model will look for the mapping, not get it, and have to discover the
flag — 0017 already booked this cost for `--evidence` and `--describe`, and this is the third
time it is paid.

## What would reverse it

A target format whose export is genuinely expensive — minutes rather than seconds on a real
model — would make `--dry-run` worth its ambiguity again, because the thing it defers would
stop being most of the work. The reversal is not free: adding it back means `files[]` can
describe files that do not exist, which forces the refusal rule back to "write nothing to
stdout" and takes `derived` false off the wire with it. Both halves move together or neither
does, and that coupling is the honest summary of this record.

A consumer that needs the artefact on stdout — a pipeline with no writable filesystem, a
container step that streams — would be an argument against the "never on stdout" rule, and the
right answer is still not to break 0014. It is to write to a named file descriptor or a named
path the caller chose, which is additive and leaves the JSON where it is.

Evidence that the manifest is wanted on most exports reverses the `--evidence` split, by
0017's own test: if the flag is typed nearly every time, the split is costing a round trip to
save a field, and the default was wrong. The move is additive and does not change the version,
since it puts a field back rather than taking one away.

What would **not** reverse any of it is an exporter finding the shape inconvenient. The reason
this record exists before any exporter does is that the first one would otherwise have decided
the interface as a side effect of deciding a file format, and a shape arrived at that way is a
shape nobody reviewed.
