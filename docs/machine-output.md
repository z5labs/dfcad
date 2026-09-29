# The machine output contract

This is the shape of what `dfcad` writes, the rule by which that shape may change, and
what each exit code means. It is the interface a script, a CI job or an agent programs
against, and it is versioned for the same reason a library's exported API is.

The reasoning behind it is
[0014. The machine output contract is part of the interface](./decisions/0014-the-machine-output-contract-is-part-of-the-interface.md).
This file is the contract itself.

## The two streams

| Stream | Carries |
|--------|---------|
| stdout | One JSON object, and nothing else. |
| stderr | Diagnostics, progress, help, and everything else meant for a person. |

Nothing human-facing is ever written to stdout — not behind a flag, not when stdout is a
terminal, not on the first run. That includes help: `dfcad --help` writes to stderr and
exits zero, so that `dfcad ... | jq` reads a result object or nothing at all, and never a
page of prose.

Stdout carries a result object exactly when the run produced a result or read a model and was
refused. A run that did neither writes nothing at all to stdout:

- help
- a usage error — no subcommand, an unknown one, a malformed flag, an unknown `--format`
- a load failure that stopped the run before it began, such as a model root that is not
  there, is not a directory or cannot be read, or an `--entity-format` this engine does not
  implement
- a model root held by another transaction
- a file that could not be written, or an operation file that could not be read at all

Each of those is an error rather than a diagnostic: it is reported on stderr as a
`dfcad <cmd>: …` line, and there is nothing in the model for a diagnostic to point at.

A run that read the model and was **refused** by what it read — a derivation, an export,
`review` or a change, over a model the load refused, or a change refused because the model it
would produce does not load — writes an object carrying no answer, only the envelope,
`"refused": true` and the diagnostics that refused it, and exits `2`
([The refusal](#the-refusal)).

A run that *ran* and found something wrong did produce a result. A file that does not parse
and a file that is not in canonical form are both reported in the object on stdout, with a
non-zero exit code beside it.

**Every diagnostic a run renders on stderr is in its object on stdout as well**, as decided
in [0029. Every diagnostic a run renders is written in its answer on stdout](./decisions/0029-every-diagnostic-a-run-renders-is-written-in-its-answer-on-stdout.md):
a top-level `diagnostics` array, written after every other field, one entry per diagnostic
in the order rendered, each in the shape `fmt` writes under `files[].diagnostics` and
carrying `ids` and `nodes` naming what it is about; `diagnostics-suppressed` where the limit
held some back; and both absent where the run rendered none. `fmt`, whose `files[].diagnostics` already is
this form, gains no top-level copy. A run that read the model and
was refused — a derivation, an export, `review`, or a change — writes the envelope,
`"refused": true` and `diagnostics`, and nothing else, with its exit code unchanged. Help, a
usage error and a load failure that read no model still write nothing. The record is the
rule; [Diagnostics](#diagnostics) says what every object carries today, and the sections below
state the rest command by command.

Output is deterministic: the same input produces byte-identical stdout. Keys come out in a
fixed order, collections in a documented order, and nothing timing-dependent appears at
all.

## The envelope

Every object on stdout begins with the same two fields, whichever command wrote it:

```json
{
  "version": 2,
  "command": "fmt"
}
```

| Field     | Type     | Meaning |
|-----------|----------|---------|
| `version` | integer  | The version of this contract the object was written against. |
| `command` | string   | The subcommand that produced the object. |

The payload's own fields sit beside these, not nested beneath them, so that a caller can
read `.version` and `.command` without knowing which command it invoked, and read the rest
once it does.

`version` is one number across the whole command line interface rather than one per
subcommand. The thing being versioned is this contract — the envelope, the streams either
side of it and the exit codes — and a caller reads it once for every command it drives.

### `assumed`

A read given [`--assume`](#global-flags) answers over the model an operation file would
produce rather than over the one on disk
([0030](./decisions/0030-a-read-may-assume-a-batch.md)), and its envelope says so with a third
member, written after `command` and before every field of the payload:

```json
{
  "version": 2,
  "command": "check",
  "assumed": {
    "batch": "into-setback.json",
    "operations": 1,
    "base": "fc1a3f31762ffb626ebb3b786ac3ebced0f785fa9f99a73c4673f37fa335215e",
    "digest": "0abd955519383d486e92c9f69c7bf7b68422b32ff3170288816bb0d8220f601f"
  },
  "refused": false
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `assumed` | object, optional | Written only under `--assume`, on every read. Absent otherwise, and the object is then byte-identical to what it is without the flag. |
| `assumed.batch` | string | The operation file exactly as it was given on the command line, or `-` for standard input. |
| `assumed.operations` | integer | How many operations the batch holds. |
| `assumed.base` | string | The digest of the tree that was read, lower-case hex: which tree the batch was assumed over. |
| `assumed.digest` | string | The digest of the tree the batch would produce, lower-case hex — what `DigestOf` would compute were the batch written. Every payload which carries a `digest` reports this same value under the flag; `check`, which carries none, is marked by `assumed` alone. |

A caller which must never act on a hypothetical tests `.assumed == null`. [The
refusal](#the-refusal) never carries it, as it carries no `digest`: a batch the run refused
produced no answer to mark. The member is an added optional field, so the contract stays at
version `2` ([the versioning rule](#the-versioning-rule)).

## The versioning rule

- A field may be **added** at any time, to the envelope or to any payload. A caller that
  reads a documented field keeps working across releases.
- A field is **never** removed, renamed, retyped, or given a different meaning without
  `version` changing.
- The documented order of a collection is part of the contract, and changing it is a
  version change.

Growth is cheap and breakage is loud, deliberately. Every output change has to be
classified as one or the other, and that judgement is a review burden on purpose.

Version `2` is the only version change so far. It is
[0017. The answer is the default and the evidence is asked for](./decisions/0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md),
and it did three things a caller on version `1` sees:

- Every `span` became a string. It was two nested objects.
- `resolve` stopped writing `claim` by default and started writing `accuracy` and
  `claim-id`. `--evidence` restores `claim`.
- `list-types` stopped writing `description` by default, and writes `absent` only where it
  holds. `--describe` restores `description`.

## Spans

Wherever this contract writes where something is — a query answer, a diagnostic, an
invariant violation, a review finding — it writes a **string**:

```
path:line:column-line:column
path:line:column
```

The path is exactly as the loader reached the file, so it opens as written. Lines and
columns are 1-based, and a column is a byte offset into its line rather than a count of
characters. The second form is an empty span, which is what something with no source text
of its own — a token that is missing, the end of a file — points at; it is written when the
two ends are the same point rather than repeated.

The path is written once because a span never crosses a file. It is written first, and the
numbers are read from the right, so a path holding a colon or a dash parses unambiguously.

Byte offsets are not written. They are a convenience for a tool holding the source bytes,
which is one line index away from recovering them; everything else that reads a span wants a
file and a line, and paid for the offsets on every span it never read. That is the
version-`2` change, and it is why the whole span is a string rather than the same object
with two fields dropped.

## Diagnostics

Every diagnostic a run renders on stderr is written in its object on stdout as well, as
[0029](./decisions/0029-every-diagnostic-a-run-renders-is-written-in-its-answer-on-stdout.md)
decides. It is one rule for every command that writes an object — the discovery reads,
`check`, `resolve`, `route`, the derivations, the exports, `review`, `apply` and every write
command — with one exception, `fmt`, below.

```json
{
  "version": 2,
  "command": "measure",
  "subject": "plan:S-01",
  "family": "node",
  "derived": false,
  "digest": "67b93ad0…",
  "diagnostics": [
    {
      "severity": "error",
      "span": "surveyed/model.dfc:178:7-178:16",
      "message": "expected the loop geom:L-11 not to cross itself, found the segment geom:V-12 to geom:V-13 crossing geom:V-14 to geom:V-11 at (5.0 4.0 0.0) m",
      "hint": "a ring which crosses itself encloses no one region; …",
      "ids": ["geom:L-11"],
      "nodes": ["plan:S-01"]
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back. It is the number the stderr rendering's last line gives — "7 more diagnostics suppressed by the limit of 100" — summed over the run. Absent where the limit held back none. |
| `diagnostics` | array, optional | One entry per diagnostic the run rendered on stderr, in the order it rendered them. Absent where the run rendered none, so an answer over a model with nothing to report is the bytes it always was. |
| `diagnostics[].severity` | string | `error` or `warning`. An error is what refuses a model or a change; a warning is rendered and refuses nothing. |
| `diagnostics[].span` | string | Where it is, as a [span](#spans). |
| `diagnostics[].message` | string | What was expected and what was found. |
| `diagnostics[].hint` | string, optional | What to do about it, where the diagnostic says. |
| `diagnostics[].related` | array, optional | The other places that explain it, each with its own `span` and `message`. |
| `diagnostics[].ids` | array of strings, optional | The entities it is about: the id of every vertex, edge, loop or node whose form encloses its `span` or the `span` of one of its `related` entries, ascending and each once. Absent where none does — a diagnostic on a registry form, between forms, or from a run which held no model. |
| `diagnostics[].nodes` | array of strings, optional | The nodes those belong to, ascending and each once: a node itself; for a loop, every node it bounds; for an edge, every node whose boundary it is part of; for a vertex, every node whose boundary's vertices include it. Absent where empty. |

Each entry is the shape `fmt` writes under `files[].diagnostics` — `dfcad.Diagnostic` written
by `encoding/json`, from the same fields `Diagnostic.Render` writes the stderr rendering from —
followed by `ids` and `nodes`. Neither rendering is derived by parsing the other, so there is
nothing to keep in step — decode the entries into `[]dfcad.Diagnostic`, render each in order
with `Diagnostic.Render` and `dfcad.FileSources{}`, and the result is the run's stderr under
the default format, wherever that stderr held only diagnostics. Decoding into
`[]dfcad.Diagnostic` drops `ids` and `nodes`, which the rendering does not read.

**`ids` and `nodes` name what a diagnostic is about, computed from its spans.** A refusal is
about a loop, an edge or a vertex, because that is what is wrong; what a caller acts on is
usually the node that shape belongs to — the room missing from the sheet — and these two fields
say which without reading `message` and without a second parser of the entity files. Both are
computed from the spans against the model the run loaded, and neither is read out of `message`:

- `ids` is `Graph.Enclosing` of the diagnostic's `span` and of each `related[].span`: the
  entity whose declaring form, matched by path, line and column, encloses it. A span inside a
  claim is inside the node the claim is written on, so a diagnostic about a claim names that
  node. A span in a registry file, in a comment between forms, or in a file the model does not
  hold encloses nothing.
- `nodes` is `Graph.Owners` of each of those. For a loop it is `Graph.Bounded` and for an edge
  `Graph.Regions` — the same lookup [`traverse bounds`](#traverse) answers from — so it is the
  reverse of `traverse boundary-of`: the `nodes` of a diagnostic about one loop or one edge are
  exactly what `traverse bounds` answers for it, and the query and the diagnostic cannot
  disagree about which node a shape belongs to. For a vertex it is every node whose boundary
  reaches it, through the edges `traverse boundary-of` lists.

They are written wherever the run loaded the model as a graph: the discovery reads, `check`,
`resolve`, `route`, the derivations, `export`, `export-map`, and `review` against its head
revision. They are never written on a write command's diagnostics — a refused change's spans
are in a model the run never held as a graph. `review` reads every diagnostic against its head
revision, so one about the base it compares against — extracted from the repository, or read
from another `--base-root` — is in a file the head does not hold, and names nothing. A load
that failed early, such as a file that does not parse, has less model to derive them from, and
they are absent there rather than guessed. Both are added fields, so the contract version is
unchanged.

**`diagnostics` is the last field of the object.** `diagnostics-suppressed`, where it is
written, is the field before it. Everything the command answers comes first, so a caller
reading the answer reads it before the reasons for it.

**It is the run's account of what it rendered, not a second answer.** A diagnostic a command
also carries in an array of its own — one of `check`'s `violations[]` or `chorded[]`, one of
`review`'s `findings[]`, the reason behind one of `plan`'s or `export-map`'s `undrawn[]` — is
in `diagnostics` too. Those arrays are each command's answer, shaped for its question and
unchanged; `diagnostics` is what the run told a person, and reading it never needs to know
which commands have a second place for some of it.

**Nothing is a diagnostic that is not one.** A usage error, a `dfcad <cmd>: …` line,
`--verbose` progress and the `--format human` summary are not in it. `--format` and
`--verbose` change none of it: stdout is the same bytes under every format and verbosity.

**`fmt` is the exception, and only in where it writes them.** Its `files[].diagnostics`
already is this form, grouped by the file each is about, so it writes no top-level copy.

### The refusal

A command which reads the model and has no answer to give through a load that refused it —
every derivation (`resolve`, `route`, `measure`, `tessellate`, `buildable`, `site`, `plan`),
`export`, `export-map`, `review` over a head or a base revision that does not load, `apply`
and every write command — exits `2` and writes exactly this:

```json
{
  "version": 2,
  "command": "measure",
  "refused": true,
  "diagnostics": [
    {
      "severity": "error",
      "span": "dangling/model.dfc:181:40-181:49",
      "message": "expected an edge id something in this model holds, found geom:E-99, which names no edge"
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `refused` | boolean | Always `true`. The same field, meaning the same thing, as a discovery read's and `check`'s over the same tree. |
| `diagnostics-suppressed` | integer, optional | As under [Diagnostics](#diagnostics). |
| `diagnostics` | array | Every diagnostic the run rendered on stderr, at least one of them an error. It is the whole of the reason. |

Nothing else is written: no `derived`, no `digest`, no `files`, no `dryRun`, no subject and no
comparison. Each of those describes an answer, and there is none — a figure computed out of a
model the load refused is an answer to a question nobody asked, and a refused change changed
nothing. A caller tells a refusal from an answer by `refused` alone, whichever command it ran.

A write is refused the same way whether it was the tree it read or the model it would produce
that does not load, and `review` whether it was the head or the base; either way the
diagnostics say which. `review` over a base that does not load also writes a
`dfcad review: …` line on stderr, which is an error message and not a diagnostic.

It is written only where a load refused what it read. A run that exits `2` for an error — the
cases listed under [The two streams](#the-two-streams) — still writes nothing, even where it
rendered diagnostics before the error: `review --annotate` renders the findings a policy ruled
failures as errors, and then failing to write its summary is a file that could not be written,
not a refusal.

A discovery read over a refused model still answers in full with `refused` true and exit `0`,
and `check` still writes its whole object with exit `2` — see
[Diagnostics and the exit code of a read](#diagnostics-and-the-exit-code-of-a-read).

The field was added under version `2`, which the [versioning rule](#the-versioning-rule)
allows: a caller that never reads it sees the object it always did.

## Exit codes

| Code | Meaning | Stdout |
|------|---------|--------|
| 0 | Success. The command did what was asked. | The result object, or empty for help. |
| 1 | Check failure. It ran and answered, and the answer is no. | The result object. |
| 2 | Load failure. Input could not be read, did not parse, or was not written. | The result object; or [the refusal](#the-refusal) where a load refused the model and the command has no answer to give through it; or empty when nothing could be loaded at all, or for an error that is not a diagnostic. |
| 3 | Usage error. The invocation itself was wrong. | Empty. |
| 4 | Ambiguous. Resolution could not choose between the claims, and every one it could not choose between is in the result. | The result object. |
| 5 | Strict ambiguity. The same, under a predicate the registry declares strict. | The result object. |

A caller can branch on the code alone, without reading a message. A check failure and a
broken invocation are different situations for a CI job — one says the model is wrong, the
other says the job is — and telling them apart must never mean matching prose.

Codes `4` and `5` are what `resolve` answers with, and they are codes of their own for the
same reason. An ambiguity is a state of the model rather than a rule the model broke: two
equally good measurements of one thing genuinely do not decide between themselves, and a
caller that is going to ask a person needs to tell that from a model that says nothing at
all. `5` separates the case where the author declared that for this quantity no answer is
safer than an arbitrary one, which is not a file to fix but a thing to go and measure.

## Global flags

Every subcommand takes these, and takes them identically.

| Flag | Default | Meaning |
|------|---------|---------|
| `--root <dir>` | `.` | The model root. A relative path argument is resolved against it; an absolute one is left alone. A root that is not a readable directory is a load failure. |
| `--format <fmt>` | `json` | How the run reports itself **to a person, on stderr**. See below. |
| `--entity-format <version>` | asserts nothing | The `MAJOR.MINOR` entity format the model was authored against. A format this engine does not implement is a load failure before anything is read. |
| `-v`, `--verbose` | off | Say more on stderr about what the run is doing. Repeatable; `--verbose=<n>` sets the level outright. |
| `-h`, `--help` | — | Print the command's help to stderr and exit zero. |

`--format` never changes stdout. `json` reports only problems on stderr; `human` adds a
readable summary of the result there as well. Stdout is byte-for-byte the same either way,
so `dfcad ... --format human | jq` still works and the person who typed it still sees the
readable version in their terminal.

`--verbose` is progress — what the run is doing, and the detail behind the summary — not
result. It never changes stdout either. Raising it adds what the run is working on, and,
under `--format human`, the status of every item rather than only of the ones something is
wrong with.

Neither flag has any effect on the exit code.

**A flag which takes one value is written once.** Written twice in one invocation — these
globals, or any flag a command declares with one value, such as `resolve --frame`, `--depth`,
`--position`, `--tolerance` or `get --claims` — it is a **usage error**: exit `3`, nothing on
stdout, and stderr naming the flag and every value it was given. That holds whether or not the
values agree. The alternative is to keep the last value and say nothing, and a run which
answers `resolve --frame frame:annex --frame frame:site` in `frame:site` is answering a
question other than the one it spelled. Every flag which repeats does so by design: the
[filters](#filters), each of which is satisfied by any of its values, the flags which build a
list, such as `plan --annotate` and `scaffold-loop --corner`, and `-v`. Each says so where
its command documents it; a flag whose entry does not say it repeats is written once.

`--entity-format` does, and is the one global flag that does. It is an assertion by the
caller about the model, because there is nothing in a model to read it out of: files carry
no version stamp, deliberately ([SPEC.md §10](../SPEC.md#10-versioning-of-this-specification)).
Given one, the engine compares it against the format it implements — the same string
`dfcad version` reports as `.contracts.entity-format` — before it opens the model root:

- the same `MAJOR`, and a `MINOR` at or below the engine's: the run proceeds, and produces
  the same exit code and byte-for-byte the same stdout as the run without the flag;
- a later `MINOR`, or a `MAJOR` apart in either direction: **exit `2`, stdout empty**, and
  stderr naming both versions. Nothing was read, so nothing is reported: a model at a format
  this engine does not implement would otherwise reach the loader and come back as an
  unrecognised form, which reads as a misspelling in the author's file rather than as a
  mismatch with their engine;
- not a `MAJOR.MINOR` version at all: **exit `3`**, like any other malformed flag.

It is taken by every command, `version` among them, which is the cheapest form of the check
because it reads no model: `dfcad version --entity-format 1.2` exits `0` where this engine
loads a 1.2 model and `2` where it does not. [`versioning.md`](./versioning.md) is what a
consumer does with that.

### The flag every read takes

Every command which reads the model and changes nothing in it — `list-types`,
`list-predicates`, `list-tolerances`, `list-frames`, `list-instances`, `list-geometry`, `get`,
`resolve`, `traverse`, `claims`, `conflicts`, `route`, `measure`, `tessellate`, `buildable`,
`site`, `plan`, `export`, `export-map` and `check` — also takes this one. The rule is the
definition and the list is its membership: a read added later takes it because it is a read.
Every other command — the writes, which have `--dry-run`, and `version`, `fmt` and `review` —
refuses it as a usage error, exit `3`, nothing on stdout.

| Flag | Default | Meaning |
|------|---------|---------|
| `--assume <file>` | off | Answer over the model the [operation file](./operation-file.md) would produce, rather than over the one on disk. Nothing is written to the authored tree and nothing is locked. The path is resolved against the model root, as `apply`'s is, and `-` reads the batch from standard input. Written once. |

The defining property is `apply`'s: for every batch `apply` accepts, `dfcad <read> --assume F`
writes the same stdout and exits with the same code as `dfcad apply F` followed by
`dfcad <read>`, apart from the envelope's [`assumed`](#assumed) member. Every `digest` a
payload reports is therefore that of the tree the batch would produce, and `export` and
`export-map` write beneath `.dfcad/export/<assumed.digest>/` unless `--out` names a
destination — the artefact `apply` followed by the export would write, byte for byte.

**A batch is refused as `apply --dry-run` refuses it, and the read does not run.** A file which
cannot be read or is not a batch exits `2` with nothing on stdout; an operation the model
refuses exits `3` with nothing on stdout; a base tree which does not load, or a result which
would not load, exits `2` with [the refusal](#the-refusal) on stdout, under the command that was
run. That holds for the discovery reads and for `check` as well, which answer through a refused
tree without the flag: a model nobody could write is not answered about. Stderr is `apply`'s,
in `apply`'s words.

Standard input is one input. `get - --assume -`, which would read the ids and the batch from
it both, is a usage error, exit `3`.

## Filters

A **filter** narrows what a command reports. It never changes the shape of the answer, and it
never changes what a command reads to reach it: `traverse --kind Space` still walks through
the building and the storey between a site and its rooms, and reports only the rooms.

Every filter follows one rule, whichever command takes it:

- **Across flags, all of them.** A thing is reported when it satisfies every filter given.
  `list-instances --kind Space --frame frame:building` lists the spaces expressed in that
  frame, and nothing else.
- **Within one flag, any of its values.** A filter written more than once is satisfied by any
  of its values. `list-instances --kind Space --kind Element` lists the spaces and the
  elements. A thing which satisfies two values of one filter is reported once, in the
  command's documented order, and a value written twice is the same as writing it once.
- **A value nobody declared is a usage error naming it** — exit `3`, with nothing on stdout —
  whichever of a filter's values it is: the first such value, in the order they were written,
  is the one reported. A type the registry does not declare, a kind which is not one of the
  seven, a family which is not one of the three (four for `claims`, which adds `node`), a
  frame the registry does not declare, a `claims --method` whose namespace the registry does
  not declare: each
  is the error it is when written alone.
  A filter that silently dropped a value nobody declared would answer a narrower question
  than the one asked, and the answer would read as complete.

A flag which names **what an answer is of** is not a filter, and takes one value.
`list-geometry --predicate` is one: the listing is of the nodes carrying that predicate, and
the answer reports it as a single `predicate` string. Written more than once it is a **usage
error** rather than a union of two listings, because a union would need each node to say
which predicate put it there.

The filters are marked **Repeatable** in the flag table of each command which takes them:
[`list-instances`](#list-instances), [`list-geometry`](#list-geometry),
[`traverse`](#traverse), [`plan`](#plan), [`claims`](#claims), [`conflicts`](#conflicts) and
[`check`](#check).

## Payloads

### `version`

Which build this is, and which contracts it implements. It reads no model and takes no
arguments.

```json
{
  "version": 2,
  "command": "version",
  "build": {
    "version": "v1.2.3",
    "commit": "abc1234",
    "stamped": true
  },
  "contracts": {
    "output": 2,
    "entity-format": "1.2"
  }
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `build.version` | string | The tool's version: the git tag pointing at the commit it was built from, or `<short-sha>-<commit-time>` where no tag does. `dev` on a binary nobody stamped. |
| `build.commit` | string | The short SHA it was built from. `unknown` on a binary nobody stamped. |
| `build.stamped` | boolean | Whether the two above came from the build. `false` means a plain `go build`, and that neither value beside it identifies anything. |
| `contracts.output` | integer | The version of this contract, which is the same number the envelope carries. |
| `contracts.entity-format` | string | The `MAJOR.MINOR` version of the entity format in [`SPEC.md`](../SPEC.md) that this build loads and prints. |

The build's version is nested rather than written at the top level because the envelope has
already spent `version` on this contract. `.version` is the contract the object was written
against; `.build.version` is the tool that wrote it. The two are different numbers in
different forms, and [`versioning.md`](./versioning.md) is the relationship between them,
the entity format version and the git tags they come from.

Exit codes: `3` if the invocation was wrong — an argument, an unknown flag, an unknown
`--format`, an `--entity-format` that is not a `MAJOR.MINOR` version. `2` if `--root` names
something that is not a directory this run can read, or if `--entity-format` names a format
this engine does not implement — both of which this command checks like every other one even
though it reads no model: a global flag that is accepted everywhere and enforced in all but
one place is one nobody can rely on. That is what makes `dfcad version --entity-format 1.2`
the cheapest way for a consumer to ask whether the engine it just installed can load the
model it is about to run against. `0` otherwise.

### `fmt`

```json
{
  "version": 2,
  "command": "fmt",
  "files": [
    {
      "path": "site/a.dfc",
      "status": "formatted"
    },
    {
      "path": "site/b.dfc",
      "status": "failed",
      "diagnostics": [
        {
          "severity": "error",
          "span": "site/b.dfc:1:7",
          "message": "unexpected end of tokens at line 1, column 7, expected one of: RParen"
        }
      ]
    },
    {
      "path": "missing.dfc",
      "status": "failed",
      "error": "stat missing.dfc: no such file or directory"
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `files` | array | One entry per file the run reached, in walk order, plus one for each path it could not reach at all. Empty rather than null when the run reached no file. |
| `files[].path` | string | The file, exactly as the walk reached it. A path that could not be reached appears as it was given, and need not name a file. |
| `files[].status` | string | One of `unchanged`, `formatted`, `unformatted`, `failed`. |
| `files[].diagnostics` | array, optional | The problems found in the file's contents, carrying the same positions and spans as the human rendering on stderr. Neither rendering is derived by parsing the other. |
| `files[].error` | string, optional | What stopped the file being read or written, where the failure is not about its contents and so has no diagnostic. |

Statuses:

| Status | Meaning |
|--------|---------|
| `unchanged` | The file was already in canonical form. |
| `formatted` | The file was rewritten into canonical form. |
| `unformatted` | The file is not in canonical form and nothing was written, which is what `--check` and `--diff` report. |
| `failed` | The file could not be read, did not parse, or could not be written. Nothing is known about whether it is canonical. |

Exit codes: `2` if any file failed, otherwise `1` if any is `unformatted`, otherwise `0`.
A failure outranks a file that is merely not canonical, because a run that could not read
half the tree has not answered the question the other half answered.

### `list-types`

The whole registry, which is the first call to make against a model nothing has read
before. It takes no arguments and one flag.

| Flag | Meaning |
|------|---------|
| `--describe` | Include the one line the registry gives each type. |
| `--classification` | Include how schemes outside this model name each type. |

```json
{
  "version": 2,
  "command": "list-types",
  "types": [
    {
      "name": "Campus",
      "kinds": ["Zone"],
      "geometries": [],
      "absent": true,
      "instances": 1
    },
    {
      "name": "MeetingRoom",
      "kinds": ["Space"],
      "geometries": ["area"],
      "instances": 12
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `refused` | boolean | True where the load refused the model — an error among the diagnostics on stderr, the ones `check` exits `2` for — and what follows was read through it. False over a model which loads. See [Diagnostics and the exit code of a read](#diagnostics-and-the-exit-code-of-a-read). |
| `types` | array | One entry per declared type, in name order. Empty rather than null when the registry declares none. |
| `types[].name` | string | The type name, which is what `list-instances` takes. |
| `types[].kinds` | array | The kinds an instance may declare, in specification order rather than the order the declaration was written in. |
| `types[].geometries` | array | The geometry forms an instance may declare, in specification order. |
| `types[].absent` | boolean, optional | Whether an instance may omit its geometry entirely. Absent on a type that requires one, the way `retired` is on a listed instance. Absence is not a geometry form — a node with no geometry omits the child rather than naming one — so it is a field of its own rather than a member of `geometries`. |
| `types[].description` | string, optional | The one line the registry gives the type. Written under `--describe`, and absent under it too when the registry wrote none. |
| `types[].classifications` | array, optional | How schemes outside this model name the type, in the order the registry wrote them. Written under `--classification`, and absent under it too when the registry wrote none, which is the ordinary case. |
| `types[].classifications[].system` | string | The scheme's name, exactly as the registry wrote it. Nothing here interprets it: there is no list of known systems. |
| `types[].classifications[].code` | string | What the type is called within that scheme, exactly as the registry wrote it, and equally uninterpreted. |
| `types[].instances` | integer | How many semantic nodes declare this type. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

The descriptions are asked for rather than given. They are prose about the vocabulary rather
than about this model, they grow with the registry rather than with the model, and this is
the call every cold start begins with — so whoever is deciding which type to ask about next
paid for them on every run and read them on almost none. The measurement is in
[`token-budget.md`](./token-budget.md) and the reasoning in
[0017](./decisions/0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md).

The classifications are asked for rather than given, for a related reason and a different
reader: the caller which needs them is one mapping this model into a foreign schema, and it
asks once. A model which declares none pays nothing for the flag either way.

### `list-predicates`

The claim predicates the registry declares, with the shape and the spelling each takes.
These are the names `list-geometry --predicate`, `claims` and `resolve` accept — exactly
`Registry.Names(dfcad.SortPredicate)` — and the only spellings a flag which names a predicate
takes. It takes no arguments and one flag.

| Flag | Meaning |
|------|---------|
| `--describe` | Include the one line the registry gives each predicate. |

```json
{
  "version": 2,
  "command": "list-predicates",
  "refused": false,
  "predicates": [
    {"name": "crs", "shape": "text", "claim-bearing": false},
    {"name": "frame-transform", "shape": "transform", "claim-bearing": true},
    {"name": "ground-to-grid", "shape": "scalar", "claim-bearing": true},
    {"name": "position", "shape": "coordinate", "unit": "m", "dimension": 3, "claim-bearing": true}
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `refused` | boolean | True where the load refused the model, and what follows was read through it. See [Diagnostics and the exit code of a read](#diagnostics-and-the-exit-code-of-a-read). |
| `predicates` | array | One entry per declared predicate, in name order. Empty rather than null when the registry declares none. |
| `predicates[].name` | string | The predicate name, which is what `list-geometry --predicate`, `claims` and `resolve` take. |
| `predicates[].shape` | string | The shape its values take: `scalar`, `coordinate`, `transform` or `text`. |
| `predicates[].unit` | string, optional | The unit its values are written in. Absent for a non-dimensional predicate. |
| `predicates[].dimension` | integer, optional | How many components a coordinate has. Written for a `coordinate` and for no other shape. |
| `predicates[].claim-bearing` | boolean | Whether a value under the predicate is a claim rather than a plain value. **Always written**: its default is true, and an absent field reads as false to every JSON consumer. |
| `predicates[].strict` | boolean, optional | Whether an ambiguous resolution of the predicate is a failure rather than a report. Written only where it is true, the way `absent` is on a listed type. |
| `predicates[].description` | string, optional | The one line the registry gives the predicate. Written under `--describe`, and absent under it too when the registry wrote none. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

Predicates come back in name order, so two runs over one model write the same bytes. Each
entry's keys are written in the order the table gives them.

The listing reports what was declared and singles nothing out. Which predicate carries a
position, a coordinate reference system or a ground-to-grid factor is project data, and a
listing which marked one would be the engine choosing. The descriptions are asked for rather
than given, for the reason `list-types`' are.

It is a command of its own rather than a flag on `list-types`, and it is on no gated path of
[`token-budget.md`](./token-budget.md): the cold start pays nothing for it, and only the caller
which asks for the vocabulary does.

### `list-tolerances`

The named tolerances the registry declares, with the value and the unit each was declared
in. These are the names `--tolerance`, `--chord` and every other flag naming a tolerance
accept — exactly `Registry.Names(dfcad.SortTolerance)`, the set the undeclared-tolerance
hint prints — and the only spellings those flags take. It takes no arguments and one flag.

| Flag | Meaning |
|------|---------|
| `--describe` | Include the one line the registry gives each tolerance. |

```json
{
  "version": 2,
  "command": "list-tolerances",
  "refused": false,
  "tolerances": [
    {"name": "boundary-closure", "value": 0.005, "unit": "m"}
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `refused` | boolean | True where the load refused the model, and what follows was read through it. See [Diagnostics and the exit code of a read](#diagnostics-and-the-exit-code-of-a-read). |
| `tolerances` | array | One entry per declared tolerance, in name order. Empty rather than null when the registry declares none. |
| `tolerances[].name` | string | The tolerance name, which is what every flag naming a tolerance takes. |
| `tolerances[].value` | number | The declared magnitude. |
| `tolerances[].unit` | string | The unit the magnitude was declared in. Never converted: a tolerance declared in `mm` is listed in `mm`. |
| `tolerances[].description` | string, optional | The one line the registry gives the tolerance. Written under `--describe`, and absent under it too when the registry wrote none. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

Tolerances come back in name order, so two runs over one model write the same bytes. Each
entry's keys are written in the order the table gives them.

A tolerance is registry data with a value and a unit and nothing else
([0012](./decisions/0012-tolerances-are-registry-data.md)), and its unit is the one it was
declared in ([0005](./decisions/0005-one-linear-unit-per-frame.md)). The listing reports
both exactly as written, and suggests no tolerance for any operation: which one a check or
a derivation uses is the caller's to name. The descriptions are asked for rather than
given, for the reason `list-types`' are.

It is a command of its own rather than a flag on `list-types`, and it is on no gated path of
[`token-budget.md`](./token-budget.md): the cold start pays nothing for it, and only the caller
which asks for the vocabulary does.

### `list-frames`

The coordinate frames the registry declares, with the unit each was declared in, the frame it
is expressed relative to and the claim holding its transform to that frame. These are the ids
`--frame` accepts on every command which takes one — exactly `Registry.Names(dfcad.SortFrame)`,
the set the unknown-frame usage error lists — and the only spellings it takes. It takes no
arguments and no flags of its own: a frame declares a label and no description, so there is
no `--describe`.

```json
{
  "version": 2,
  "command": "list-frames",
  "refused": false,
  "frames": [
    {"id": "frame:site", "label": "Site setting-out grid", "unit": "m", "parent": "frame:survey-grid", "transform": "survey:C-0001"},
    {"id": "frame:survey-grid", "label": "Site survey grid", "unit": "m"}
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `refused` | boolean | True where the load refused the model, and what follows was read through it. See [Diagnostics and the exit code of a read](#diagnostics-and-the-exit-code-of-a-read). |
| `frames` | array | One entry per declared frame, in id order. Empty rather than null when the registry declares none. |
| `frames[].id` | string | The frame's id, which is what every `--frame` flag takes. |
| `frames[].label` | string, optional | The frame's name for a person. Absent when it was not written. |
| `frames[].unit` | string | The frame's one linear unit, as declared. Never converted. |
| `frames[].parent` | string, optional | The id of the frame this one is expressed relative to. Absent on the root. |
| `frames[].transform` | string, optional | The id of the claim the frame names as its transform to the parent. Absent on the root. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

Frames come back in id order, so two runs over one model write the same bytes. Each entry's
keys are written in the order the table gives them. The root is the entry with no `parent`;
following `parent` from any other entry walks the chain to it. Over a model the load refused
the listing still answers, and a frame whose parent is not declared is listed with the parent
it names, as written.

A frame is a node, its unit is its one linear unit
([0005](./decisions/0005-one-linear-unit-per-frame.md)), and its transform is a claim. The
listing names the transform by the id of its claim and inlines nothing: no claim, no plain value and no span.
What is written *on* a frame — the transform's value, a coordinate reference system, a
ground-to-grid factor — is that frame's retrieval, which is [`get`](#get) of its id. Nor does
the listing mark any frame as carrying a coordinate reference system: which predicate names
one is project data.

With `--format human` it renders the chain on stderr, one line per frame — its id, its unit,
and `→` the parent where it has one — then the count. Stdout is the same in every format.

It is a command of its own rather than a flag on `list-types`, and it is on no gated path of
[`token-budget.md`](./token-budget.md): the cold start pays nothing for it, and only the caller
which asks for the vocabulary does.

### `list-instances`

The instances of one type, or of the whole model. It takes an optional type argument and
two filters.

| Flag | Meaning |
|------|---------|
| `--kind <kind>` | Only instances that declare this kind. [Repeatable](#filters). |
| `--frame <id>` | Only instances that declare this coordinate frame. [Repeatable](#filters). |
| `--retired` | Include the instances that stopped existing. |

Filters combine: an instance is listed when it satisfies every filter given, and a filter
written more than once is satisfied by any of its values; see [Filters](#filters). Flags and
the type argument may be written in either order. The type argument is not a filter: a
second one is a usage error.

A **retired** node is left out unless it is asked for. It is still a node the model holds —
its id is never issued again, and a reference to it still resolves — but a listing is a
question about what is there, and answering it with things that stopped existing makes every
caller filter them out again. Asked for, they come back carrying `"retired": true`, so a
caller reading a mixed listing can tell which is which without asking about each of them.

```json
{
  "version": 2,
  "command": "list-instances",
  "instances": [
    {
      "id": "site:S-101",
      "label": "Meeting Room B",
      "type": "MeetingRoom",
      "kind": "Space",
      "frame": "frame:building"
    },
    {
      "id": "site:Z-01",
      "label": "Riverside campus",
      "type": "Campus",
      "kind": "Zone"
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `refused` | boolean | True where the load refused the model — an error among the diagnostics on stderr, the ones `check` exits `2` for — and what follows was read through it. False over a model which loads. See [Diagnostics and the exit code of a read](#diagnostics-and-the-exit-code-of-a-read). |
| `instances` | array | One entry per instance that satisfied every filter, in id order. Empty rather than null when nothing did. |
| `instances[].id` | string | The id the model holds it under. |
| `instances[].label` | string, optional | Its name for a person reading it. Absent when it was not written. |
| `instances[].type` | string | The type it declares, reported whether or not a type was filtered on. It need not be one the registry declares: a node naming an undeclared type is a diagnostic and is still a node of the type it named. |
| `instances[].kind` | string | The kind it declares, reported whether or not a kind was filtered on. |
| `instances[].frame` | string, optional | The coordinate frame it is expressed in. Absent when it declares none. |
| `instances[].retired` | boolean, optional | Whether the thing it names stopped existing. Absent on a node that did not, so a listing that was not asked for the retired ones holds nothing else. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

Instances come back in id order rather than in walk order, so the listing does not change
when a node is moved between files while the model it describes stays the same.

A type, a kind or a frame the model does not declare is a **usage error** — exit `3`, with
nothing on stdout — naming what was asked for. It is not an empty list: a type nobody
declared and a type nothing instantiates are different answers, and a caller that cannot
tell them apart retries a misspelling forever.

Each of the three says where to look, and they do not all say the same thing, because the
three sets are not the same size. An unknown **type** points at `list-types`: a registry
worth discovering is one too large to print into an error. An unknown **kind** lists the
seven, which are a closed set compiled into the engine and are not in `list-types` at all.
An unknown **frame** lists the frames the registry declares, which `list-types` does not
list either; [`list-frames`](#list-frames) lists them, with each one's unit, parent and
transform.

### `list-geometry`

The geometric nodes — vertices, edges and loops — which carry a claim under one predicate.
It takes no arguments and five flags.

| Flag | Meaning |
|------|---------|
| `--predicate <name>` | The predicate the node carries. **Required**, and it has no default. Written once: it names what is listed rather than filtering it. |
| `--family <family>` | Only nodes of this family: `vertex`, `edge` or `loop`. [Repeatable](#filters). |
| `--frame <id>` | Only nodes expressed in this coordinate frame. [Repeatable](#filters). |
| `--near "<x> <y> …"` | Only the vertices within `--tolerance` of this point, in the shape `--predicate` declares. Needs exactly one `--frame` and a `--tolerance`. See [Vertices at a point](#vertices-at-a-point). |
| `--tolerance <name>` | The declared tolerance a vertex has to be within of the `--near` point. Read only beside `--near`. |

`--family` and `--frame` are filters. They combine: a node is listed when it satisfies every
filter given, and a filter written more than once is satisfied by any of its values; see
[Filters](#filters). `--frame` is the flag `list-instances` takes, with the same meaning: the
match is exact, so a node expressed in a child frame is not listed for its parent. The answer
does not echo it, because every node already carries its `frame`.

It is the geometric sibling of `list-instances`, which reports the `type` and the `kind` a
vertex, an edge and a loop do not have. Without it a geometric node is reachable only by its
id: `claims` takes one id, and `traverse` refuses geometry by design, so a measured span
between two corners — an ordinary edge carrying a claim, belonging to no loop and bounding
nothing — could be found only by somebody who already knew its name.

`--predicate` has no default and never will, for the reason `buildable` has none: which
predicate carries a position, a setback or a span is something the project wrote down, and a
name compiled into the engine would be it deciding a project's vocabulary on its behalf. A
run which names none is a **usage error** — exit `3`, with nothing on stdout.

A node is listed when a **live** claim is written on it under that predicate. A deprecated
claim is retracted rather than out-ranked, and resolution never considers one, so a corner
whose only surveyed position was withdrawn records nothing under it. `claims` is the audit
view which reports those, one subject at a time.

```json
{
  "version": 2,
  "command": "list-geometry",
  "predicate": "setback",
  "nodes": [
    {
      "id": "geom:E-11",
      "family": "edge",
      "label": "Plot one, road frontage",
      "frame": "frame:building",
      "start": "geom:V-11",
      "end": "geom:V-12",
      "span": "entities/geometry.dfc:90:1-96:26"
    },
    {
      "id": "geom:E-14",
      "family": "edge",
      "label": "Plot one, west flank",
      "frame": "frame:building",
      "start": "geom:V-14",
      "end": "geom:V-11",
      "span": "entities/geometry.dfc:114:1-120:26"
    }
  ]
}
```

A vertex and a loop carry the same shape without `start` and `end`:

```json
{
  "id": "geom:V-11",
  "family": "vertex",
  "label": "Plot one, south-west corner",
  "frame": "frame:building",
  "span": "entities/geometry.dfc:50:1-58:26"
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `refused` | boolean | True where the load refused the model — an error among the diagnostics on stderr, the ones `check` exits `2` for — and what follows was read through it. False over a model which loads. See [Diagnostics and the exit code of a read](#diagnostics-and-the-exit-code-of-a-read). |
| `predicate` | string | The predicate the nodes below carry, which is the one asked for. It travels with the answer because an empty list means nothing without it. |
| `nodes` | array | One entry per geometric node carrying a live claim under that predicate, in id order. Empty rather than null when nothing does. |
| `nodes[].id` | string | The id the model holds it under, which is what every other command takes. |
| `nodes[].family` | string | Which family holds it: `vertex`, `edge` or `loop`. Reported whether or not a family was filtered on, so a listing read whole is readable on its own. |
| `nodes[].label` | string, optional | Its name for a person reading it. Absent when it was not written, which is the ordinary case for geometry. |
| `nodes[].frame` | string, optional | The coordinate frame it is expressed in. Absent only when it declares none, which is a diagnostic rather than an ordinary node. |
| `nodes[].start` | string, optional | The vertex an edge runs **from**. Absent for a vertex and for a loop. |
| `nodes[].end` | string, optional | The vertex an edge runs **to**. Absent for a vertex and for a loop. |
| `nodes[].span` | string | Where it was written, as `path:line:col-line:col`. |
| `near` | object, optional | Under `--near` only: the query, as `{"at": [...], "tolerance": {"name", "value", "unit"}}` — the point, component by component, and the declared tolerance a vertex had to be within of it. |
| `nodes[].at` | array, optional | Under `--near` only: where the vertex's position resolves, component by component, in the frame's unit. |
| `nodes[].distance` | number, optional | Under `--near` only: how far that position is from the point, in the frame's unit. `0` for a vertex exactly at it. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

An edge names its two vertices **in the order they were authored**. The order is the data —
an edge is directed, and the region on the other side of it traverses it the other way — so
it is reported as written and is never sorted.

Nodes come back in **id order** rather than grouped by family, so the listing does not change
when a node moves between files, and grouping does not reorder the whole answer the day an
edge is given a claim it did not have before.

A predicate no geometric node carries is an **empty list and exit `0`**. A model which
records no spans is an ordinary model, and answering it with a failure would make a caller
parse a message to tell nothing-there from something-wrong.

A predicate the registry does not declare is a **usage error** naming it and listing the
predicates that are declared, and so is a `--family` which is none of the three. A `--frame`
the registry does not declare is one too, listing the frames that are declared, exactly as
it is for `list-instances`. Each is exit `3`, with nothing on stdout, whichever of a repeated
filter's values it is. A predicate nobody declared and a predicate nothing is written under
are different answers, and a caller that cannot tell them apart retries a misspelling forever.

`--predicate` written more than once is a **usage error** too — exit `3`, with nothing on
stdout — naming the flag and every value it was given, in the order they were written. It is
not a filter: it names what the listing is of, and `predicate` in the answer is one string.
A caller wanting two predicates' nodes asks twice, and gets two answers each saying which
question it answers. See [Filters](#filters).

#### Vertices at a point

`--near` answers "which vertex is at this coordinate?" without writing anything. A vertex is
listed when both hold: its position resolves under `--predicate`, in the unit of the one
`--frame`, and that position lies within the value of `--tolerance` of the point. **The match
is [`scaffold-loop`](#scaffold-loop)'s** — the two commands call one function for it — so the
vertex a scaffold reuses at a corner is always the one listed with the smallest `distance` at
that point, and a lookup made first says what the scaffold will do.

Against `testdata/siting/surveyed`:

```console
$ dfcad list-geometry --predicate position --frame frame:building \
    --near "10.002 0 0" --tolerance boundary-closure
```

```json
{
  "version": 2,
  "command": "list-geometry",
  "refused": false,
  "predicate": "position",
  "near": {"at": [10.002, 0, 0], "tolerance": {"name": "boundary-closure", "value": 0.005, "unit": "m"}},
  "nodes": [
    {
      "id": "geom:V-12",
      "family": "vertex",
      "label": "Block A footprint, south-east corner",
      "frame": "frame:building",
      "span": "model.dfc:140:1-149:26",
      "at": [10, 0, 0],
      "distance": 0.002000000000000668
    }
  ]
}
```

Vertices still come in id order, which is the listing's order; `distance` is what picks the
nearest. A vertex whose position does not resolve is not listed, because nobody can say where
it is. Nothing within the tolerance is an **empty `nodes` and exit `0`**.

Each of these is a **usage error** — exit `3`, with nothing on stdout:

- `--near` without a `--frame` or without a `--tolerance`, and `--tolerance` without `--near`.
- `--near` beside more than one `--frame`: the point is a coordinate in one frame.
- `--family edge` or `--family loop` beside `--near`: only a vertex is at a point.
  `--family vertex` is accepted.
- A predicate which does not declare a coordinate.
- A point whose number of components is not the predicate's `dimension`, or one of whose
  components is not a number. It is read exactly as a `scaffold-loop` corner is.
- A tolerance declared in a unit other than the frame's, which `scaffold-loop` refuses in the
  same way.

### `get`

One thing, by its id, with the claims and the assertions written on it. It takes one id argument and three
flags. Given `-` in place of the id it reads many ids from standard input instead — see
[Many ids at once](#many-ids-at-once).

| Flag | Meaning |
|------|---------|
| `--claims <how>` | `full` (default), every claim written on it, or `resolved`, the current claim under each predicate. |
| `--deprecated` | Include the claims that have been deprecated. Refused beside `--claims resolved`. |
| `--observations` | Read the observation files it links to and inline the records. Without it, the files are named and not opened. |

An id is unique across the whole model, so this is one command for both families. A vertex,
an edge and a loop are retrieved by the same call a semantic node is, and `family` says
which came back and so which of the fields to expect. So is a frame: it is both a registry
entry and a node ([SPEC §7.5](../SPEC.md#75-frame)), its id is drawn from the same id space,
and it comes back as `family` `frame` — see [A frame](#a-frame).

```json
{
  "version": 2,
  "command": "get",
  "entity": {
    "id": "site:S-101",
    "family": "node",
    "label": "Meeting Room A",
    "kind": "Space",
    "type": "MeetingRoom",
    "geometry": "area",
    "frame": "frame:building",
    "within": "site:L-01",
    "member-of": ["site:Z-01"],
    "boundaries": ["geom:L-01"],
    "observations": ["observations/2026-05-07-interior.obs"],
    "span": "entities/site.dfc:13:1-52:43",
    "claims": [
      {
        "id": "survey:A-0002",
        "predicate": "area",
        "value": {"shape": "scalar", "unit": "m2", "scalar": 24.2},
        "source": "As-built check AB-2026-009, Acme Surveys",
        "method": "method:total-station",
        "accuracy": [{"kind": "independent", "magnitude": 0.05, "unit": "m2"}],
        "combined": {"magnitude": 0.05, "unit": "m2", "coverage-factor": 1},
        "date": "2026-05-06",
        "rank": "normal",
        "span": "entities/site.dfc:30:3-36:25"
      }
    ],
    "assertions": [
      {
        "check": "boundary-loops-close",
        "parameters": ["(tolerance boundary-closure)"],
        "span": "entities/site.dfc:51:3-51:61"
      }
    ]
  }
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `refused` | boolean | True where the load refused the model — an error among the diagnostics on stderr, the ones `check` exits `2` for — and what follows was read through it. False over a model which loads. See [Diagnostics and the exit code of a read](#diagnostics-and-the-exit-code-of-a-read). |
| `entity` | object | The thing the id named. |
| `entity.id` | string | The id the model holds it under, which is the id that was asked for. |
| `entity.family` | string | One of `node`, `vertex`, `edge`, `loop`, `frame`. It says which of the fields below to expect. |
| `entity.label` | string, optional | Its name for a person reading it. |
| `entity.kind` | string, optional | The kind a semantic node declares. |
| `entity.type` | string, optional | The type a semantic node declares, which need not be one the registry declares. |
| `entity.geometry` | string, optional | The geometry form a semantic node declares. Absent when it has none, which is ordinary rather than incomplete. |
| `entity.frame` | string, optional | The coordinate frame it is expressed in. |
| `entity.within` | string, optional | The id of the node that strictly contains a semantic node. |
| `entity.member-of` | array, optional | The ids of the zones a semantic node declares membership of, in the order it wrote them. |
| `entity.boundaries` | array, optional | The ids a semantic node wrote where a loop id belongs, in the order it wrote them, and as written rather than as resolved. |
| `entity.start`, `entity.end` | string, optional | The ids of the vertices an edge runs between. |
| `entity.backed-by` | array, optional | The ids of the elements that physically realise an edge. |
| `entity.edges` | array, optional | The ids of the edges a loop is assembled from, in the order it wrote them. |
| `entity.unit` | string, optional | A frame's one linear unit, as declared ([0005](./decisions/0005-one-linear-unit-per-frame.md)). Never converted. Written only on a frame. |
| `entity.parent` | string, optional | The id of the frame a frame is expressed relative to. Written only on a frame, and absent on the root. |
| `entity.transform` | string, optional | The id of the claim a frame names as its transform to the parent, as written; the claim itself is in `entity.claims`. Written only on a frame, and absent on the root. |
| `entity.observations` | array, optional | The observation files it links to, as paths relative to the model root, in the order it wrote them. Absent when it links to none. Producing this reads nothing. |
| `entity.observation-records` | array, optional | The records those files hold, written under `--observations` and absent otherwise. Empty rather than absent when the flag was given and the files hold no record, because "nobody has surveyed this" and "you did not ask" are different answers. |
| `entity.retired` | object, optional | How a semantic node stopped existing: `date`, `reason`, and `superseded-by` where something stands in its place. Absent for a node that was not retired. |
| `entity.span` | span | Where it was written: the file, and the line and column of both ends of the form. |
| `entity.claims` | array | The claims written on it, in predicate order and then by where each was written. Empty rather than null when nothing is claimed about it. |
| `entity.values` | array, optional | The plain values written on it — the spelling a predicate the registry declares `(claim-bearing #f)` takes ([SPEC §6.5](../SPEC.md#65-claims)) — in predicate order and then by where each was written. Absent when it carries none, which is the ordinary case. `--claims` and `--deprecated` do not change it: a plain value is never resolved and never deprecated. |
| `entity.assertions` | array | The assertions written on it, in the order they were written. Empty rather than null when nothing constrains it. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

Every claim carries the evidence for its value, because a value without it is the bare
number the format exists to stop:

| Field | Type | Meaning |
|-------|------|---------|
| `claims[].id` | string, optional | The claim's own id. Absent when it wrote none, which is the great majority of claims. |
| `claims[].predicate` | string | The predicate it was written under. |
| `claims[].value` | object | `shape` is one of `scalar`, `coordinate`, `text`, `transform`, and says which of `scalar`, `coordinate`, `text` and `transform` carries the value. `unit` is absent for a non-dimensional predicate and for the shapes that carry no unit. |
| `claims[].source` | string, optional | What the value is evidenced by — a report, a drawing, an instrument log. |
| `claims[].method` | string, optional | The id naming how the value was obtained. |
| `claims[].accuracy` | array, optional | One entry per term, each with its `kind` (`independent` or `systematic`), its one-sigma `magnitude`, its `unit`, and the `source` a systematic term is shared with. Absent when the claim carries none, which makes it unrankable rather than exact. |
| `claims[].combined` | object, optional | The accuracy reduced to one standard uncertainty by the rule a budget is combined with — independent terms in quadrature, systematic terms of distinct ids linearly, and the two totals in quadrature, every magnitude by its absolute value ([SPEC §6.6.5](../SPEC.md#665-accuracy-terms)) — in the shape [`budget.combined`](#budget) takes: `magnitude`, `unit` and `coverage-factor`, which is `1`. `unit` is absent for a non-dimensional accuracy. It is the figure resolution ranks the claim by. Written wherever `accuracy` is and its terms combine; absent where the claim states no accuracy, which `accuracy` being absent already says, and absent where the terms are in more than one unit, which `units` says. |
| `claims[].units` | array, optional | The units the accuracy's terms were written in, each once in the order written, where there is more than one. Nothing converts between units, so such an accuracy combines to no figure, `combined` is absent and the claim is unrankable. Absent wherever `combined` is present. |
| `claims[].date` | string, optional | The day the value was obtained, as a full date. |
| `claims[].rank` | string | `normal` or `deprecated`, reported whether or not it was written. |
| `claims[].superseded-by` | string, optional | The id of the claim that replaced this one. |
| `claims[].resolution` | string, optional | What the rule left this claim as: `current`, `tied` or `unranked`. Written under `--claims resolved` and absent otherwise, because under `--claims full` nothing has been resolved. |
| `claims[].span` | span | Where the claim was written. |

A plain value is not a claim, which is why it is beside `claims` rather than among them: it
has no id, no source, no method, no accuracy, no date and no rank, and an entry of `claims`
without them would read as an unrankable claim the model does not contain. Its value is the
object a claim's value is, so a caller reads one value shape:

| Field | Type | Meaning |
|-------|------|---------|
| `values[].predicate` | string | The predicate it was written under. |
| `values[].value` | object | The value, exactly as [`claims[].value`](#get) writes one: `shape`, `unit` where the predicate declares one, and the field its shape names. |
| `values[].span` | span | Where the plain value was written: the whole form, predicate and value. |

Each assertion is what was written on the thing, rather than what the check registry makes
of it:

| Field | Type | Meaning |
|-------|------|---------|
| `assertions[].check` | string | The name of the check the assertion names. |
| `assertions[].parameters` | array, optional | The parameters it supplies, each rendered the way it was written — `(tolerance boundary-closure)` rather than the value the tolerance stands for. Absent when the check takes none. |
| `assertions[].span` | span | Where the assertion was written, which is inside the form of the thing it constrains. |

The claims are what is known about the thing; the assertions are what has to hold of it.
They come back together because retrieving a thing is how somebody finds out about it, and
what it may not stop measuring is half of that. **The invariants of its type are not here:**
those are stated on the type, and this call is about this thing. An assertion naming a
check nothing registers is a load error, reported on stderr like any other diagnostic, and
is **still reported here** — a retrieval that quietly dropped it would read as though
nobody had written it.

Each record of `entity.observation-records` is one shot, in log order across every file the
thing links to:

| Field | Type | Meaning |
|-------|------|---------|
| `observation-records[].id` | string | The record's identity, which is what a claim's provenance points at and what a retirement names. |
| `observation-records[].at` | string | When it was taken, exactly as it was written: the offset the author was working in is evidence about where somebody was standing. |
| `observation-records[].frame` | string | The frame the coordinate is expressed in. Every length on the record is in that frame's linear unit, and nothing here converts one. |
| `observation-records[].coordinate` | array | The position, component by component, in the frame's axis order. Ordered, and never sorted. |
| `observation-records[].method` | string | How the shot was taken. |
| `observation-records[].fix` | string | The solution the instrument reported at the moment of it. |
| `observation-records[].horizontal-precision`, `observation-records[].vertical-precision` | number | The standard uncertainties in the plane and along the vertical, one sigma. |
| `observation-records[].antenna-height` | number | The offset from the mark to the phase centre or prism the coordinate has already been reduced by. |
| `observation-records[].session` | string | The occupation the record belongs to, which is how a systematic error is attributed to the setup that caused it. |
| `observation-records[].retired` | object, optional | The later record that retired this one: `id`, `at`, `reason` and `span`. Absent for a record nothing retired. |
| `observation-records[].span` | span | The line of the file the record was written on. |

A retired record is reported rather than dropped. Retirement removes trust in a number and
never the number itself, and an answer that quietly left it out would be the tool rewriting
the evidence it was asked to show.

**Without `--observations`, no observation file is opened.** The links come from the model,
which was loaded either way; the records come from files that are three orders of magnitude
larger and are read only when something asks for them. Anything wrong with what they hold —
a malformed line, a duplicate identity, a retirement naming a record that is not there — is
reported on stderr like any other diagnostic and does not change the exit code, because the
retrieval succeeded and it is the survey log that is wrong.

Under `--claims resolved` a predicate appears once, as the claim that won. Where nothing
won it appears as every claim that could still be the answer — `tied` where more than one
could, whether the rule could not separate them or nothing rankable was said about any of
them, and `unranked` where exactly one is left and so there is nothing to choose between —
because narrowing four claims to two is most of the work of deciding between them, and a
caller shown one of the two cannot tell that the other exists.

Deprecated claims are left out unless `--deprecated` asks for them. `--deprecated` beside
`--claims resolved` is a **usage error** rather than a flag that is quietly ignored: a
deprecated claim is retracted rather than out-ranked, and resolution never considers one.

References are ids and are never the things they name, so the answer is the size of the
thing that was asked for rather than of the model behind it. Following one is another call.

A **retired** node answers here whether or not it was retired, which is the half of
retirement a listing does not do: `list-instances` leaves retired nodes out unless asked,
and a retrieval by id resolves to the node and says what happened to it. That is what makes
a reference written years ago answerable — it either names the thing it always named, or
names something that says it stopped existing and, where there is one, what replaced it
([0002](./decisions/0002-immutable-id-mutable-label.md)).

#### A frame

A frame id is answered from the registry's frames when no entity holds it. It carries `id`,
`label` where one was written, `unit`, `parent` and `transform` (both absent on the root),
`span` — the whole frame form — its `claims` and its `values`, and `assertions`, which is
always `[]`: a frame form carries none. Against `testdata/checks/grid/affirmed`, run from its
parent directory:

```json
{
  "version": 2,
  "command": "get",
  "refused": false,
  "entity": {
    "id": "frame:survey-grid",
    "family": "frame",
    "label": "Site survey grid",
    "unit": "m",
    "span": "affirmed/registry.dfc:51:1-60:26",
    "claims": [
      {
        "id": "survey:C-0010",
        "predicate": "ground-to-grid",
        "value": {"shape": "scalar", "scalar": 1},
        "source": "Georeferencing report GR-2026-002, Acme Surveys, section 4: combined factor",
        "method": "method:gnss-static",
        "date": "2026-02-11",
        "rank": "normal",
        "span": "affirmed/registry.dfc:55:3-60:25"
      }
    ],
    "values": [
      {"predicate": "crs", "value": {"shape": "text", "text": "EPSG:25831"}, "span": "affirmed/registry.dfc:54:3-54:21"}
    ],
    "assertions": []
  }
}
```

and `get frame:site` carries `"parent": "frame:survey-grid", "transform": "survey:C-0001"`
beside the `frame-transform` claim that `transform` names, whose `value.transform.scale` is
the scale of the fit.

- **Claims are claims.** A claim written on a frame is reported in the `claims[]` object
  every family's is, and `--claims resolved` and `--deprecated` do to it exactly what they
  do anywhere else.
- **Nothing is singled out.** Which predicate names a coordinate reference system, and
  which states a ground-to-grid factor, is project data, so the answer carries every claim
  and plain value and the caller selects by its own names. A factor is stated as a claim
  (`entity.claims`), as a plain value (`entity.values`), or as a transform whose scale is not
  `1` (`value.transform.scale` of the claim `entity.transform` names); whether it is stated
  at all is what `check`'s `ground-to-grid-stated` answers.
- **No observations.** A frame links no observation file, so `observations` is absent and
  `--observations` writes `"observation-records": []`.
- **Only get.** The frames are consulted by `get` alone. `traverse`, `check --subject`,
  `plan` and the writers answer a frame id as they always have, and a node, vertex, edge or
  loop answers byte for byte as it did.
- **Human format.** `--format human` renders the frame on stderr: its unit, and its parent
  and transform where it has them, at every verbosity, then its claims and values as for any
  other family.

`"family": "frame"` is written only for an id which was a usage error with nothing on stdout,
and `unit`, `parent` and `transform` are optional and absent on every other family, so the
contract stays at version `2` ([The versioning rule](#the-versioning-rule)).

#### Unknown ids

An id nothing in the model holds is a **usage error** — exit `3`, with nothing on stdout —
naming it, and naming the nearest id there is when one is close enough to be the id that
was meant. An id is held when an entity holds it or the registry declares a frame under it. It is not an empty answer: a thing that is not there and a thing with nothing
said about it are different answers. An argument that is not a well-formed id is the same
exit code, reporting the rule it broke rather than a lookup that was never going to find
anything.

#### Many ids at once

`dfcad get [flags] -` reads ids from standard input, separated by whitespace — one per line
is what `jq -r` writes — and answers them all against one load of the model:

```sh
dfcad list-instances Device | jq -r '.instances[].id' | dfcad get --claims resolved -
```

```json
{
  "version": 2,
  "command": "get",
  "refused": false,
  "entities": [
    {"id": "site:S-101", "family": "node", "label": "Meeting Room A", "...": "..."},
    {"id": "site:S-102", "family": "node", "label": "Meeting Room B", "...": "..."}
  ]
}
```

The elements are cut short above, where `"..."` stands for the rest of the object `entity`
is documented as.

| Field | Type | Meaning |
|-------|------|---------|
| `refused` | boolean | As for one id. |
| `entities` | array | One element per distinct id read, each **exactly** the object `get <id>` writes under `entity` for that id, under the same `--claims`, `--deprecated` and `--observations`. |

- **Order.** `entities` is in id order, and an id written more than once is answered once.
  The order of standard input changes nothing on stdout.
- **Empty.** Standard input holding no id answers `"entities": []` with exit `0`. Nothing
  was named, so nothing is unknown.
- **Every bad id, not the first.** Each id that is malformed or that nothing in the model
  holds is reported on stderr, one line each, in the words `get` of that id alone would
  use, and the run exits `3` with **nothing on stdout** — no partial answer, which a caller
  would read as the whole. The error is one `BatchIDsError` whose elements are each that
  id's `MalformedIDError` or `UnknownIDError`, with its nearest id.
- **Observations.** Under `--observations` each file is read once per run however many of
  the entities link to it, and a problem in a shared file is rendered once on stderr.
- **Human format.** `--format human` summarises each entity on stderr as `get <id>` does,
  and adds one line counting them. Stdout is unchanged by it.

The ids come from standard input rather than from further arguments so that the shape of the
answer never depends on how many there are. `get <id>` writes `entity`, byte for byte as it
always has; a caller substituting a computed listing into argv would otherwise get `entity`
when the listing held one id and a usage error when it held none. So more than one id
argument, or `-` beside one, is a **usage error** — exit `3`, `SeveralIDsError` — whose
message names `-` as the way to retrieve several.

`entities` is a field written only under a new invocation and `entity` is untouched, so the
contract stays at version `2` ([The versioning rule](#the-versioning-rule)).

### `resolve`

One predicate about one thing, answered: the value, the unit it is in, how well it is
known, the id of the claim it came from and which step of the rule picked that claim.

```json
{
  "version": 2,
  "command": "resolve",
  "subject": "site:S-101",
  "predicate": "area",
  "outcome": "resolved",
  "reason": "accuracy",
  "strict": false,
  "value": {"shape": "scalar", "unit": "m2", "scalar": 24.2},
  "accuracy": [{"kind": "independent", "magnitude": 0.05, "unit": "m2"}],
  "combined": {"magnitude": 0.05, "unit": "m2", "coverage-factor": 1},
  "claim-id": "survey:A-0002"
}
```

That is the answer. The audit trail behind it — who said so, how, when, and where they
wrote it down — is `--evidence`:

```json
{
  "version": 2,
  "command": "resolve",
  "subject": "site:S-101",
  "predicate": "area",
  "outcome": "resolved",
  "reason": "accuracy",
  "strict": false,
  "value": {"shape": "scalar", "unit": "m2", "scalar": 24.2},
  "accuracy": [{"kind": "independent", "magnitude": 0.05, "unit": "m2"}],
  "combined": {"magnitude": 0.05, "unit": "m2", "coverage-factor": 1},
  "claim-id": "survey:A-0002",
  "claim": {
    "id": "survey:A-0002",
    "predicate": "area",
    "value": {"shape": "scalar", "unit": "m2", "scalar": 24.2},
    "source": "As-built check AB-2026-009, Acme Surveys",
    "method": "method:total-station",
    "accuracy": [{"kind": "independent", "magnitude": 0.05, "unit": "m2"}],
    "combined": {"magnitude": 0.05, "unit": "m2", "coverage-factor": 1},
    "date": "2026-05-06",
    "rank": "normal",
    "resolution": "current",
    "span": "entities/site.dfc:18:3-24:26"
  }
}
```

| Flag | Meaning |
|------|---------|
| `--evidence` | Report the winning claim in full beside the answer: its source, its method, its rank, its date and where it was written. |
| `--candidates` | Report every live claim under the predicate beside the answer, each in full and marked with what resolution made of it. |
| `--frame <id>` | Express a coordinate answer in this frame rather than in the one the thing is written in. |

| Field | Type | Meaning |
|-------|------|---------|
| `subject` | string | The id the question was asked about. |
| `predicate` | string | The predicate it was asked under. |
| `outcome` | string | `resolved`, `unranked`, `ambiguous` or `unclaimed`. It says which of the fields below to expect. |
| `reason` | string | Which step of the rule produced that outcome: `only`, `accuracy`, `recency`, `unranked`, `ambiguous` or `unclaimed`. |
| `strict` | boolean | Whether the registry declares the predicate strict. Written whatever the outcome. |
| `value` | object, optional | The answer, in the same shape `claims[].value` takes elsewhere. Absent where nothing resolved. |
| `accuracy` | array, optional | How well the answer is known, term by term, as the claim it came from stated it. Absent where nothing resolved, and absent where the claim stated none — which makes the answer unrankable rather than exact, and is what `reason` says. Present on an `unranked` answer whose claim wrote terms in more than one unit: those terms never combine into a figure, because nothing converts between units, so the claim is unrankable with an accuracy beside it. |
| `combined` | object, optional | `accuracy` reduced to one standard uncertainty, exactly as [`claims[].combined`](#get) writes it for the claim the answer came from: the figure resolution ranked it by. Under `--frame` it is still the claim's own; the transformed answer's is `budget.combined`. |
| `units` | array, optional | The units `accuracy`'s terms were written in, each once, where there is more than one — which is the `unranked` answer whose terms never combine — and `combined` is then absent. As [`claims[].units`](#get). |
| `claim-id` | string, optional | The id of the claim the answer came from. Absent where nothing resolved, and absent where the claim wrote no id, which is the great majority of them: an id is required only of a claim something references. |
| `frame` | string, optional | The coordinate frame the value is expressed in. Absent for a value that is not a position, which is in no frame. |
| `claim` | object, optional | The claim the answer came from, in the shape documented under `get`. Written under `--evidence` and absent otherwise. |
| `candidates` | array, optional | Claims that could still be the answer, each in that same full shape and marked with its `resolution`. |
| `budget` | object, optional | The accumulated error of a cross-frame answer, broken out by term. Written only where a frame transform was applied. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

`value`, `accuracy` and `claim-id` are the answer; `claim` is the audit trail. The split is
[0017. The answer is the default and the evidence is asked for](./decisions/0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md),
and it is drawn where it is because how good a number is belongs to the number, while who to
argue with about it is a separate question asked far less often. It is not a trim of what
this engine refuses to hand over: an answer never comes back as a bare figure, because
`accuracy` is beside it whenever the claim stated one — and `combined` beside that, the same
terms reduced to the one figure resolution ranked the claim by, so that a caller wanting a
single error bar reads it rather than reimplementing the rule that produces it.

The four outcomes and the four exit codes line up, because what a caller does about each is
different:

| Outcome | Exit | Carries |
|---------|------|---------|
| `resolved` | `0` | `value`, `accuracy` and `claim-id`, and `claim` under `--evidence`. `reason` is `only`, `accuracy` or `recency`. |
| `unranked` | `0` | The same. The one live claim under a predicate nothing rankable was said about: still what the model says, and not an answer the rule chose. Either there is no `accuracy`, which is why, or there is one whose terms are in more than one unit, which nothing converts between and so never combines into a figure to rank by. |
| `ambiguous` | `4`, or `5` where `strict` | `candidates`, every one of them and each in full. No `value`, no `accuracy`, no `claim-id` and no `claim`. |
| `unclaimed` | `1` | None of them. Nothing live is written under the predicate. |

Under `ambiguous` the candidates come back in full whether or not `--evidence` was given.
Where there is no answer the evidence is the answer, and a caller asked to choose between
two claims cannot do it from two values.

An ambiguity is never broken by picking one. Every tied claim comes back whether or not
`--candidates` was given, because narrowing four claims to two is most of the work of
deciding between them and a caller shown one of the two cannot tell the other is there.

`--candidates` widens `candidates` from the tied claims to **every live claim** under the
predicate, each marked `current`, `tied`, `unranked` or `outranked`. Deprecated claims are
not among them at any time: a deprecated claim is retracted rather than out-ranked and was
never a candidate, so listing it would say the rule weighed something it never saw. `dfcad
claims` is the view that reports a retraction.

The subject may be a **frame** the registry declares, as well as a node, a vertex, an edge or
a loop — the one id space `get` answers ([A frame](#a-frame)). A claim written on a frame, such
as its ground-to-grid factor or the transform placing it in its parent, is resolved exactly
as a claim written on anything else: the same object, the same outcomes and the same exit
codes. A plain value written on a frame — its `crs`, say — is not a claim, so `resolve`
answers `unclaimed`, exit `1`, as it does for a plain value on a node; `get`'s `values` is
where it is reported. `--frame` beside a frame subject is a usage error, the one given for a
subject that declares no frame: a frame is not expressed in a frame, and its relation to one
is its transform.

```sh
dfcad resolve frame:survey-grid ground-to-grid
```

```json
{
  "version": 2,
  "command": "resolve",
  "subject": "frame:survey-grid",
  "predicate": "ground-to-grid",
  "outcome": "unranked",
  "reason": "unranked",
  "strict": false,
  "value": {"shape": "scalar", "scalar": 1},
  "claim-id": "survey:C-0010"
}
```

An id nothing in the model holds is a **usage error** — exit `3`, with nothing on stdout —
naming it and the nearest id there is. That is a different answer from `unclaimed`, which
is the model answering that nobody has measured the thing; a caller that cannot tell them
apart retries a misspelling forever. A predicate no registry file declares, and a `--frame`
no registry file declares, are usage errors for the same reason.

#### `budget`

Written when `--frame` moved the answer between two frames, because the accuracy of such an
answer is not the accuracy of the claim it came from: the fits along the route are part of
what is known about it. So an answer under `--frame` carries two combined figures, and they
are two different things: the top-level `combined` is the claim's own accuracy, as written,
and `budget.combined` is the transformed answer's, route and all.

| Field | Type | Meaning |
|-------|------|---------|
| `budget.from` | string, optional | The frame the value was written in. Absent for a budget that is a computation rather than a route between two frames — `buildable` writes one. |
| `budget.to` | string, optional | The frame it was expressed in. Absent for the same reason. |
| `budget.terms[].kind` | string | `independent` or `systematic`, which is how it combines: independent terms in quadrature, systematic terms of distinct ids linearly, and the two totals in quadrature ([SPEC §6.6.5](../SPEC.md#665-accuracy-terms)). |
| `budget.terms[].name` | string | The id a systematic error is shared with, or the name of the claim an independent one came from. |
| `budget.terms[].magnitude` | number | The one-sigma figure, as it was written. |
| `budget.terms[].unit` | string | The unit that figure is expressed in. |
| `budget.terms[].source` | string, optional | The id a systematic error is shared with. Absent for an independent term. |
| `budget.terms[].contributors` | array | The claims that carried the term, each once. More than one is a shared term counted once. |
| `budget.combined` | object, optional | The terms reduced to one standard uncertainty — independent terms in quadrature, systematic terms of distinct ids linearly, and the two totals in quadrature ([SPEC §6.6.5](../SPEC.md#665-accuracy-terms)): `magnitude`, `unit` and `coverage-factor`, which is `1` for everything the engine produces. |
| `budget.unknown` | array, optional | The claims the answer was computed from that stated no accuracy. One of them taints the whole budget, and `combined` is then absent: an unstated accuracy is unknown rather than zero. |
| `budget.unranked` | array, optional | The things — a vertex, a node, an edge — the answer read a claim of that stated no accuracy at all, each once. Such a claim is unrankable and is still read where nothing rankable was said ([SPEC §6.5](../SPEC.md#65-claims)): a corner nobody gave an accuracy to is where the model says it is, and this names it rather than folding it into the budget as though it carried one. Each such claim is also in `unknown`. |
| `budget.units` | array, optional | The units the terms were written in where they disagree. Nothing converts between them, so `combined` is absent rather than reconciled. |

The terms are a list rather than a figure on purpose. "±0.0098 m" is an answer nobody can
act on; "the control point is most of it, and these two claims put it there" says what to
re-measure.

A `--frame` the model cannot relate the subject's frame to is a **load failure** — exit
`2`, with nothing on stdout. A frame whose fit is missing, two frames whose chains never
meet, a chain that cycles and a transform that cannot be run backwards are all the model
failing to say how the two relate, and a position computed anyway would be the invented
georeference the whole arrangement exists to prevent. `--frame` naming the frame the thing
is already written in transforms nothing and reports no budget, which is what asking
without the flag answers.

### `traverse`

A walk of the model: what contains what, what belongs to what, and what borders what. It
takes a query, an id, and seven flags.

| Flag | Meaning |
|------|---------|
| `--depth <n>` | How many steps of the relation to follow: a count of one or more, or `all` to follow it as far as the model goes. Default `1`. |
| `--kind <kind>` | Only results that declare this kind. [Repeatable](#filters). |
| `--type <name>` | Only results that declare this type. [Repeatable](#filters). |
| `--cross-virtual` | `adjacent-to` only. An edge nothing backs may be crossed. |
| `--cross-type <name>` | `adjacent-to` only. An edge may be crossed when at least one element backing it declares this type. Repeatable, and any of its values allows a crossing. Unlike a [filter](#filters), it decides what is walked and not only what is reported. |
| `--walk-kind <kind>` | `adjacent-to` only. Enter — report, and walk through — only things that declare this kind. Repeatable, and any of its values will do. Unlike a [filter](#filters), it decides what is walked and not only what is reported. |
| `--walk-type <name>` | `adjacent-to` only. Enter — report, and walk through — only things that declare this type. Repeatable, and any of its values will do. Unlike a [filter](#filters), it decides what is walked and not only what is reported. |

| Query | Answers | Relation |
|-------|---------|----------|
| `contains` | What the thing holds, level by level inward. | `containment` |
| `contained-by` | What holds the thing, outward towards the root. | `containment` |
| `members-of` | The zones the thing is a member of, and the zones those are members of where membership nests. | `membership` |
| `members` | What a zone groups — every node which wrote `(member-of <id>)`, whatever its type — and what those group where they are zones themselves. | `membership` |
| `boundary-of` | The edges the thing's outline is assembled from, each classified by what physically realises it. | `boundary` |
| `bounds` | Given a loop, the nodes which name it in a `(boundary …)`; given an edge, the nodes whose boundary reaches it through any of their loops. The reverse of `boundary-of`, walked from the shape. | `boundary` |
| `adjacent-to` | The things that share a boundary edge with it. | `adjacency` |

```json
{
  "version": 2,
  "command": "traverse",
  "subject": "site:S-101",
  "query": "adjacent-to",
  "depth": 1,
  "results": [
    {
      "id": "site:S-102",
      "family": "node",
      "relation": "adjacency",
      "depth": 1,
      "label": "East Corridor",
      "kind": "Space",
      "type": "Corridor",
      "frame": "frame:building",
      "from": "site:S-101",
      "via": ["geom:E-02"],
      "span": "entities/site.dfc:41:1-48:25"
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `refused` | boolean | True where the load refused the model — an error among the diagnostics on stderr, the ones `check` exits `2` for — and what follows was read through it. False over a model which loads. See [Diagnostics and the exit code of a read](#diagnostics-and-the-exit-code-of-a-read). |
| `subject` | string | The id the walk started from. |
| `query` | string | The query it answered, which is what says which relation the results carry. |
| `depth` | integer | The bound the walk was given, and `-1` where it was told to follow the relation as far as the model goes. It is what tells a caller reading a stored result whether the walk stopped where the model ran out or where the bound did. |
| `results` | array | What the walk reached. Empty rather than null where it reached nothing. |
| `results[].id` | string | The id the model holds it under. |
| `results[].family` | string | `node`, or `edge` for the boundary of one. |
| `results[].relation` | string | Which relation reached it: `containment`, `membership`, `boundary` or `adjacency`. |
| `results[].depth` | integer | How many steps of that relation the walk took to reach it, which is the fewest there are. |
| `results[].label` | string, optional | Its name for a person reading it. |
| `results[].kind`, `results[].type` | string, optional | What a semantic node declares. Absent for an edge, which declares neither. |
| `results[].frame` | string, optional | The coordinate frame it is expressed in. |
| `results[].classification` | string, optional | What an edge of a boundary separates the region by: `physical`, `virtual`, or `unresolved` where it names a backing element the model does not hold. Absent for a result that is not an edge. |
| `results[].backing` | array, optional | The ids of the elements that physically realise an edge, in the order the edge named them. Absent for a virtual edge, which names none. |
| `results[].backing-types` | array, optional | The type each element in `backing` declares, at the same position, so that `backing-types[i]` is the type of `backing[i]` and a wall is told from a door without a second call and a join on id. Absent exactly where `backing` is: for a virtual edge and for an unresolved one. Over a model the load refused, an element which declares no type — one whose `(type …)` could not be read — contributes `""`, so the two arrays stay aligned. There is no `backing-kinds`: an edge is backed only by a node of kind `Element`, so the kind of every backing element is `Element` and a field carrying it would be a constant. |
| `results[].from` | string, optional | The id of the thing an adjacent thing was reached from, which is the thing `via` names the shared edges with. At depth 1 that is the subject, and it is written there too, so a result's shape does not depend on its depth. Past it, it is a result one step nearer: where more than one thing a step nearer shares an edge with it — a crossable edge, under `--cross-virtual` or `--cross-type` — the one with the smallest id. The walk is breadth first, so following `from` from any result reaches the subject in exactly `depth` steps, and that chain is a shortest path. Written under `adjacent-to` and absent otherwise. |
| `results[].via` | array, optional | The ids of the edges an adjacent thing shares with the thing it was reached from, in the order that boundary traverses them. At depth 1 that is the subject. Past it, where more than one thing a step nearer shares an edge with it, it was reached from the one with the smallest id, so `via` does not move when a node moves between files and can be checked against `boundary-of`. Under `--cross-virtual` or `--cross-type` it names only the edges that may be crossed, so it says how to get between the two rather than what separates them. Written under `adjacent-to` and absent otherwise. |
| `results[].span` | span | Where it was written. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

Every result says which relation produced it, and containment is never reported as
membership or the other way round. A wall inside a storey and grouped into three zones is
inside one thing and a member of three; a result that blurred the two would answer "what is
in this storey" with the zones.

Adjacency is **shared boundary edges and nothing else**: two things are adjacent when an
edge is part of the boundary of both. An edge is one node with one identity, so this is a
fact about the model rather than a comparison of two outlines — two boundaries drawn along
the same line with two edges are not adjacent. A doorway and the wall it is cut into are two
shared edges between the same pair of rooms, so the neighbour is reported once carrying
both, and `boundary-of` is what says which of them is a wall.

Each adjacent result names the thing it was reached from as `from`, and its `via` is the
edges it shares with that one. The walk is breadth first and reports each thing at the
fewest steps it can be reached in, so the chain of `from` is a shortest path back to the
subject: a path can be rebuilt from one walk, without walking again from each room on it.

What `adjacent-to` may cross is every shared edge unless it is told otherwise. With
neither `--cross-virtual` nor `--cross-type` written, every shared edge may be crossed and
the answer is the one above. With either written, an edge may be crossed in exactly two
cases:

- it is virtual — its `classification` under `boundary-of` is `virtual` — and
  `--cross-virtual` was written, or
- at least one element in its `backing` declares a type named by a `--cross-type`. At least
  one rather than all, because a doorway is cut into a wall and the model says so by backing
  one edge with both.

An `unresolved` edge is never crossed under either flag: nothing is known about what realises
it. A thing that shares only edges which may not be crossed is not reached across them,
though it may be reached another way. Which types are ways through is the caller's to say —
the names are compared with the types the backing elements declare and carry no meaning
of their own, so a `Doorway` is a passage because the caller named it and a window, which
fills an opening and is not a way through, is not one unless the caller names it too.

Under a crossing filter `via` names **only the edges crossed**: the crossable edges the
result shares with `from`. So it answers "how do you get between them" rather than "what
separates them", and `boundary-of` is still where the wall beside the door is found. `from`
is the thing one step nearer with the smallest id among those sharing a crossable edge with
the result, so the chain of `from` is a shortest path through the edges that may be crossed.

Unlike `--kind` and `--type`, which narrow only what is reported, these two decide what is
walked. They combine with the narrowing filters as before: `--cross-type Doorway --kind Space`
walks through doors and reports the spaces it reaches.

A `--cross-type` the registry does not declare is a usage error, exactly as it is for
`--type`. So is one whose type does not permit kind `Element`: only an `Element` backs an
edge, so nothing of that type can back one and the walk could never cross anything by it.
Either flag written beside any query but `adjacent-to` is a usage error, because only an
adjacency walk crosses edges.

What `adjacent-to` may enter is everything that shares a crossable edge, unless it is told
otherwise. With neither `--walk-kind` nor `--walk-type` written, the answer is the one above.
With either written, a thing is **entered** — reported, and walked through — only when it
satisfies each walk flag given:

- its `kind` is one of the `--walk-kind` values, where `--walk-kind` was written, and
- its `type` is one of the `--walk-type` values, where `--walk-type` was written.

A thing that is not entered is neither reported nor walked through. That is what the crossing
flags cannot say on their own: a site, zone or storey whose outline is drawn with the same
edges as the rooms along its side shares an edge with each of them, so a walk which may enter
it steps from one room onto the site and from the site into a room on its far side, and
reports two rooms which share no edge as reachable from each other. When those outside edges
are virtual, `--cross-virtual` crosses them. `--walk-kind Space` does not step onto the site
at all.

The walk starts from the subject whatever the subject declares, so a walk from a site with
`--walk-kind Space` reports the rooms along its outline. `from` is always a thing that was
entered, or the subject. `--kind` and `--type` sit beside the walk flags and still narrow only
what is reported: `--walk-kind Space --type Bedroom` walks through every space and reports the
bedrooms.

A `--walk-kind` naming none of the seven kinds is a usage error, exactly as it is for
`--kind`, and so is a `--walk-type` the registry does not declare, exactly as it is for
`--type`. Either flag written beside any query but `adjacent-to` is a usage error, because only
an adjacency walk chooses what it walks through.

The reachability question — which spaces can be reached from the entrance through a door or
across open floor — is two calls. One lists every space there is, the other every space
reached:

```sh
dfcad list-instances --kind Space | jq -r '.instances[].id' | sort > spaces
dfcad traverse adjacent-to --depth all --cross-virtual --cross-type Doorway --walk-kind Space site:S-113 \
  | jq -r '.results[].id, .subject' | sort > reached
comm -23 spaces reached
```

A space in the first list that is missing from the second is one nothing reaches through a
door or across an open line, which is a closet drawn with no opening or a room sealed off when
it was redrawn. Without `--walk-kind Space`, a site or storey outline sharing virtual edges
with the rooms along its side would carry the walk around the outside of the building and
hide exactly those.

Depth is bounded by default, because a traversal of a model nobody has read should not be
able to return the whole of it by accident; `--depth all` is how a caller asks for that on
purpose. Each thing is reported once, at the fewest steps it can be reached in, so a cycle in
the model terminates and something reachable two ways is one result rather than two.

A filter narrows what is reported and never what is walked. Every room three levels below a
site is still reached with `--kind Space`, though the building and the storey between them
are not reported. Filters combine: a result is reported when it satisfies every filter given,
and a filter written more than once is satisfied by any of its values; see
[Filters](#filters).

Results come back in depth order and then in id order, so two runs over one model diff
against each other and moving a node between files does not move the answer. The edges of a
boundary are the exception: they come back in the order the loops traverse them, because
that order is the ring itself and is data rather than presentation.

`boundary-of` is one step from the thing it bounds, and its results are edges. `--depth`,
`--kind` and `--type` written beside it are **usage errors**, however many times they are
written, rather than flags that are quietly ignored, for the reason `--deprecated` beside `--claims resolved` is: a flag that is
silently dropped answers a different question from the one that was asked.

`bounds` is the same relation read from the other end, so it is one step too: every result
is a node at `depth` `1` with the relation `boundary`, in id order, and `--depth` beside it is
a usage error for the same reason. Its results are nodes, so `--kind` and `--type` are
honoured. A loop or an edge that bounds nothing is an empty `results`, not an error.

An id nothing in the model holds is a **usage error** — exit `3`, with nothing on stdout —
naming it and the nearest id there is, exactly as `get` reports one. Every query walks from a
semantic node except `bounds`, which walks from a loop or an edge; an id of any other family
is a usage error too, naming which family it is and which the query takes. `bounds` is how a
walk starts from a shape: it names the nodes the shape is the boundary of, and every other
query walks on from them.
A walk that reaches nothing is not an error — it is an empty `results` and exit `0`.

### `claims`

Every claim written on one thing, live and retracted alike — or, with no id, on every thing.
It takes an optional id, a predicate after the id, and five filters. The id may name a node,
a vertex, an edge or a loop, or a **frame** the registry declares, as it may for
[`get`](#a-frame): a frame carries claims — the transform placing it in its parent among
them — and they are answered exactly as any other subject's are, each row carrying
`"family": "frame"`.

| Flag | Meaning |
|------|---------|
| `--predicate <name>` | Only claims written under this predicate. [Repeatable](#filters). A predicate written after the id counts as one more value of this flag, and is checked first, with the id. |
| `--type <name>` | Only claims on a node declaring this type. [Repeatable](#filters). |
| `--family <family>` | Only claims on a thing of this family: `node`, `vertex`, `edge` or `loop`. [Repeatable](#filters). |
| `--method <id>` | Only claims obtained by this method, matched exactly against `claims[].method`. [Repeatable](#filters). |
| `--unrankable` | Only claims resolution cannot rank: those that state no accuracy, and those whose accuracy is in more than one unit ([SPEC §6.5](../SPEC.md#65-claims)). Takes no value. |

Filters combine: a claim is listed when it satisfies every filter given, and a filter written
more than once is satisfied by any of its values; see [Filters](#filters). They apply with an
id as well as without one.

A method is an **id**, not a member of a known set: there is no method registry, because its
namespace is what the registry governs ([SPEC §12](../SPEC.md#12-reviewed-against-the-decision-records),
"One open reading, resolved here"). So `--method` checks what can be checked and no more. A
value that is not an id is a usage error, and so is an id in a namespace the registry does not
declare — the error `add-claim --method` gives for the same mistake. A well-formed id in a
declared namespace that no claim in the model names cannot be told from a method nobody has
used yet, so it is answered truthfully, with `"claims": []` and exit `0`, and a warning on
stderr names it and says no claim in the model names it. The warning is written in every
`--format`, and stdout is the same bytes with or without it. An id some claim names, which the
other filters happen to exclude, is not warned about.

There is no `--method-not`. Every claim whose method is *not* one of a set is the complement of
this listing — `claims --predicate position` with the rows `--method` would select taken out —
and taking it is the caller's: one call, and no comparison operator added to the filters.

`--unrankable` selects a **state**, not a missing field. [SPEC §6.5](../SPEC.md#65-claims) closes
which children of a claim may be left out: `source`, `method` and `date` are arity `1`, so a
claim without one does not load and is already a diagnostic with a position; `id` is `0..1`,
and leaving it out is the ordinary case and says nothing about the claim; `accuracy` is `0..1`,
and its absence is the only one that changes what the claim is. A field selector over that
closed set would have one useful value. The spec's word for the state is *unrankable* —
`resolve` reports it as `unranked`, and `plan` and `measure` as `budget.unranked` — so the flag
names the state. It is the test resolution ranks by, so a claim whose `accuracy` terms are in
more than one unit is listed too: it states an accuracy, but nothing reduces its terms to one
figure, and its entry carries `units` and no `combined`. Every entry `--unrankable` lists is
one with no `combined`. Retracted claims are listed beside live ones, each still marked with its
`resolution`, and the flag combines with every other filter, with an id or without one. A model
in which every claim states an accuracy answers `"claims": []` and exit `0`.

`get` answers what the model says about a thing now; `claims` answers everything anybody has
said about it and what became of each statement. Deprecated claims are therefore in the
answer rather than behind a flag, marked as retracted and carrying the id of the claim that
replaced them, so a retraction is followable forward without a second call.

```json
{
  "version": 2,
  "command": "claims",
  "refused": false,
  "subject": "site:S-101",
  "claims": [
    {
      "subject": "site:S-101",
      "family": "node",
      "type": "MeetingRoom",
      "id": "survey:A-0001",
      "predicate": "area",
      "value": {"shape": "scalar", "unit": "m2", "scalar": 23.0},
      "source": "Plan set A-101, sheet 3",
      "method": "method:scaled-from-plan",
      "accuracy": [{"kind": "independent", "magnitude": 0.5, "unit": "m2"}],
      "combined": {"magnitude": 0.5, "unit": "m2", "coverage-factor": 1},
      "date": "2026-01-09",
      "rank": "deprecated",
      "superseded-by": "survey:A-0002",
      "resolution": "retracted",
      "span": "entities/site.dfc:20:3-28:34"
    },
    {
      "subject": "site:S-101",
      "family": "node",
      "type": "MeetingRoom",
      "id": "survey:A-0002",
      "predicate": "area",
      "value": {"shape": "scalar", "unit": "m2", "scalar": 24.2},
      "source": "As-built check AB-2026-009, Acme Surveys",
      "method": "method:total-station",
      "accuracy": [{"kind": "independent", "magnitude": 0.05, "unit": "m2"}],
      "combined": {"magnitude": 0.05, "unit": "m2", "coverage-factor": 1},
      "date": "2026-05-06",
      "rank": "normal",
      "resolution": "current",
      "span": "entities/site.dfc:29:3-35:25"
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `refused` | boolean | True where the load refused the model — an error among the diagnostics on stderr, the ones `check` exits `2` for — and what follows was read through it. False over a model which loads. See [Diagnostics and the exit code of a read](#diagnostics-and-the-exit-code-of-a-read). |
| `subject` | string, optional | The id the claims below are written on, which is the id that was asked for. Absent when no id was, and the claims are every subject's. |
| `claims` | array | Every claim written on it, in predicate order and then by where each was written — or, with no id, every claim on every subject, in subject id order first. Empty rather than null when nothing is claimed. Each entry is the claim object `get` writes, documented above, with the four fields below beside it. |
| `claims[].subject` | string | The id of the thing the claim is written on. Written whether or not an id was asked about, so that an entry has one shape whatever narrowed the listing. |
| `claims[].family` | string | Which family holds that thing: `node`, `vertex`, `edge` or `loop`, or `frame` where the id asked about is a frame. |
| `claims[].type` | string, optional | The type the thing declares, where it is a node. Absent for a vertex, an edge, a loop or a frame, which declare none. |
| `claims[].retired` | boolean, optional | `true` where the thing is a node which has been retired. Absent otherwise: a retired node's claims are still claims the model holds, and the audit view lists them. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

With no id, `claims` is the audit view of the whole model: exactly what `claims <id>` answers
for each node, vertex, edge and loop, one after another in id order. Rows come in subject id
order, then predicate order, then the order each was written, so "every live position claim,
with its method and its accuracy" is one call rather than a listing followed by one call per
corner:

```sh
dfcad claims --predicate position --family vertex
```

```json
{
  "version": 2,
  "command": "claims",
  "refused": false,
  "claims": [
    {
      "subject": "geom:V-01",
      "family": "vertex",
      "id": "survey:P-0001",
      "predicate": "position",
      "value": {"shape": "coordinate", "unit": "m", "coordinate": [0, 0, 0]},
      "source": "Boundary survey BS-2026-011, Acme Surveys",
      "method": "method:total-station",
      "accuracy": [
        {"kind": "independent", "magnitude": 0.004, "unit": "m"},
        {"kind": "systematic", "magnitude": 0.008, "unit": "m", "source": "control:CP-1"}
      ],
      "combined": {"magnitude": 0.008944271909999158, "unit": "m", "coverage-factor": 1},
      "date": "2026-03-11",
      "rank": "normal",
      "resolution": "current",
      "span": "model.dfc:25:3-31:25"
    },
    {
      "subject": "geom:V-02",
      "family": "vertex",
      "id": "survey:P-0002",
      "predicate": "position",
      "value": {"shape": "coordinate", "unit": "m", "coordinate": [30, 0, 0]},
      "rank": "normal",
      "resolution": "current",
      "span": "model.dfc:36:3-42:25"
    }
  ]
}
```

A claim written on a frame is not in this listing, which is of the four families `--family`
names: it is answered by the frame's id, `claims frame:building`. A frame is the one subject
the listing leaves out, and bringing its claims in beside the rest would be an addition of
its own — a fifth family for `--family` to accept — rather than something the listing does
now.

```sh
dfcad claims frame:site
```

```json
{
  "version": 2,
  "command": "claims",
  "refused": false,
  "subject": "frame:site",
  "claims": [
    {
      "subject": "frame:site",
      "family": "frame",
      "id": "survey:C-0001",
      "predicate": "frame-transform",
      "value": {"shape": "transform", "transform": {"translation": [100, 200, 0], "rotation": [1, 0, 0, 0, 1, 0, 0, 0, 1], "scale": 1}},
      "source": "Georeferencing report GR-2026-002, Acme Surveys",
      "method": "method:gnss-static",
      "accuracy": [{"kind": "independent", "magnitude": 0.012, "unit": "m"}],
      "combined": {"magnitude": 0.012, "unit": "m", "coverage-factor": 1},
      "date": "2026-02-11",
      "rank": "normal",
      "resolution": "current",
      "span": "registry.dfc:70:3-80:25"
    }
  ]
}
```

`resolution` is written on **every** claim here, rather than only under a flag as it is in
`get`, and it takes two values `get` never writes, because this view reports every claim
rather than only the ones that could still be the answer:

| Value | Meaning |
|-------|---------|
| `current` | The claim resolution picks under its predicate. |
| `tied` | One of several claims resolution cannot separate, so it picks none of them. |
| `unranked` | The one live claim under a predicate nothing rankable was said about, which leaves nothing to choose between. |
| `outranked` | A live claim that another claim under the same predicate beat. |
| `retracted` | A deprecated claim, which resolution never considers. |

`tied` and `unranked` are told apart by how many claims are still in the running, not by why
they are. Several claims nothing rankable was said about are `tied` — they are equally
current, and resolution picks none of them — exactly as several equally accurate and equally
recent claims are. `unranked` is what a claim reads as when it is the only one left, so there
is nothing for it to be tied with; a caller filtering for the pairs that need somebody to
decide wants `tied`, and a single unrankable claim is not one of them.

A claim that lost and a claim that was withdrawn are both left out of a resolution, and
reporting them as the same thing would say a measurement somebody bettered and one somebody
retracted are the same kind of not-current.

An id nothing in the model holds is a **usage error** — exit `3`, with nothing on stdout —
naming it, and naming the nearest id there is, exactly as `get` does. A predicate the
registry does not declare is a usage error for the same reason a filter naming an undeclared
type is: a predicate nobody declared and a predicate nothing is claimed under are different
answers. So is a type the registry does not declare and a family which is none of the four,
whichever of a filter's values it is. `--type` beside `--family` values none of which is
`node` is a usage error too, rather than an empty list, for the reason `conflicts` refuses
`--ambiguous` beside `--resolved`: only a node declares a type, so no claim satisfies both,
and an empty answer would read as a model with none. A predicate that *is* declared and that
nothing is claimed under, and a model with no claims at all, are an empty list and exit `0`.

### `conflicts`

The conflict register: every subject and predicate pair the model states more than once,
with the competing claims and what resolution makes of them. It takes no arguments and four
filters.

| Flag | Meaning |
|------|---------|
| `--type <name>` | Only pairs whose subject declares this type. [Repeatable](#filters). |
| `--predicate <name>` | Only pairs written under this predicate. [Repeatable](#filters). |
| `--ambiguous` | Only pairs resolution cannot decide. |
| `--resolved` | Only pairs resolution can. |

Filters combine: a pair is listed when it satisfies every filter given, and a filter written
more than once is satisfied by any of its values; see [Filters](#filters). `--ambiguous` and
`--resolved` together are a **usage error** rather than an empty register — a pair carrying
more than one live claim either has a best claim or does not, so no pair is both, and an
empty answer would read as a model nobody disagrees about.

```json
{
  "version": 2,
  "command": "conflicts",
  "conflicts": [
    {
      "subject": "site:S-101",
      "predicate": "area",
      "type": "MeetingRoom",
      "ambiguous": false,
      "current": "survey:A-0002",
      "claims": [
        {
          "id": "survey:A-0002",
          "predicate": "area",
          "value": {"shape": "scalar", "unit": "m2", "scalar": 24.2},
          "rank": "normal",
          "resolution": "current",
          "span": "entities/site.dfc:29:3-35:25"
        },
        {
          "id": "survey:A-0003",
          "predicate": "area",
          "value": {"shape": "scalar", "unit": "m2", "scalar": 24.0},
          "rank": "normal",
          "resolution": "outranked",
          "span": "entities/site.dfc:36:3-42:25"
        }
      ]
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `refused` | boolean | True where the load refused the model — an error among the diagnostics on stderr, the ones `check` exits `2` for — and what follows was read through it. False over a model which loads. See [Diagnostics and the exit code of a read](#diagnostics-and-the-exit-code-of-a-read). |
| `conflicts` | array | One entry per pair the model states more than once, ordered by subject and then by predicate. Empty rather than null when nothing disagrees. |
| `conflicts[].subject` | string | The id of the thing the competing claims are about. |
| `conflicts[].predicate` | string | The predicate they were written under. |
| `conflicts[].type` | string, optional | The type the subject declares, reported whether or not a type was filtered on. Absent for a vertex, an edge or a loop, which declare none. |
| `conflicts[].ambiguous` | boolean | Whether resolution picks nothing, so the disagreement has no answer. Exactly one of this and a claim marked `current` holds of every entry. |
| `conflicts[].current` | string, optional | The id of the claim resolution picks. Absent when nothing was picked, and also when the claim that was picked wrote no id of its own — the claim marked `current` below carries the span that names it instead. |
| `conflicts[].claims` | array | The competing claims, in the order they were written, each the claim object documented under `get` with the `resolution` field documented under `claims`. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

A pair conflicts when more than one live claim is written on it, whatever those claims say.
Whether two values *agree* is a question about a tolerance, and tolerances are registry data
the consuming repository owns, so the register reports that the model states a thing twice
and what each statement is, and leaves agreement to whoever declared what agreement means.

A deprecated claim is never competing. It is retracted rather than out-ranked, so a pair
whose second claim is deprecated has one live claim and no entry here. That is the one way of
silencing a conflict there is, and it requires asserting in the file that the claim is wrong.

Neither `claims` nor `conflicts` exits non-zero merely because the model disagrees with
itself. A conflict is a finding, not a failure; whether a particular disagreement is allowed
is what `dfcad check` answers, and answering it in two commands is how the two come to
disagree.

### `route`

Which file a newly authored node would be written to, decided from the registry's routing
rules and reported without writing anything. It takes the id the node would be written with,
and three flags.

| Flag | Meaning |
|------|---------|
| `--kind <kind>` | The kind the new node will declare. |
| `--type <name>` | The type the new node will declare. |
| `--file <path>` | Write it here instead, overriding the rules. A path relative to the model root, ending in `.dfc`. |

A vertex, an edge and a loop carry neither a kind nor a type, so routing one means leaving
both of the first two flags out. Such a node is matched by a rule that matches on its
namespace alone, or by one that matches on nothing.

```json
{
  "version": 2,
  "command": "route",
  "subject": {"id": "site:S-104", "kind": "Space", "type": "MeetingRoom"},
  "destination": {
    "path": "entities/level-1.dfc",
    "rule": "rooms",
    "overridden": false,
    "exists": true
  }
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `subject` | object | What was asked, echoed back, so a collected result says what the answer was about. |
| `subject.id` | string | The id the node would be written with. Its namespace is what a rule matching on one compares against. |
| `subject.kind` | string, optional | The kind it would declare. Absent for a geometric node. |
| `subject.type` | string, optional | The type it would declare. Absent for a geometric node. |
| `destination.path` | string | The target file, relative to the model root. |
| `destination.rule` | string, optional | The routing rule that chose it. Absent when the destination was overridden — an override names no rule, and a caller must not go looking in the registry for one. |
| `destination.overridden` | boolean | Whether `--file` named the destination outright. |
| `destination.exists` | boolean | Whether the model already holds that file. A destination that does not is created, with any directories above it, by the write that lands there. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

**Exactly one rule must match.** A node matched by none, and a node matched by more than one,
are both a **usage error** naming the node and every rule consulted — never a silent default.
Neither is resolved by picking a rule, not the first written and not the most specific: a
filing decision the tool makes on its own is visible in nothing the author wrote. The fix for
both is a change to the registry, which is where the rules are ([7.7 of the
specification](../SPEC.md#77-route)).

`route` writes nothing, whatever it answers. It is the same decision every write command
makes, asked on its own — which is how an author checks where something would land before
authoring it.

### `measure`

How big one thing is, computed from the geometry it is written in terms of. It takes the id
of the thing to measure and two flags, neither of which has a default.

| Flag | Meaning |
|------|---------|
| `--position <predicate>` | The predicate a corner's position is claimed under, which every figure is read from. Required. |
| `--tolerance <name>` | The tolerance corners are judged coincident against and rings judged planar against. Required. |

Which predicate carries a position and how close two corners are one corner are project data
([0012](decisions/0012-tolerances-are-registry-data.md)), so a run that names neither is a
**usage error** naming both flags at once.

**The id is the whole of the dispatch.** There is no flag saying which family it names: a
semantic node is measured through the loops which bound it, a loop through the ring its edges
traverse, an edge from its two ends and a vertex from where it is. `family` says which
answered, and so which of the figures to expect — an edge encloses nothing, and that is a
different state from a region whose area could not be computed.

**This is a computation and never an assertion.** Nothing here reads a claimed `area` and
nothing here writes one back
([0009](decisions/0009-derived-values-are-never-written-back.md)); every figure is recomputed
from the corners each time it is asked for. `resolve <id> area` is the other question and
neither substitutes for the other: a claimed area which disagrees with a computed one is the
most valuable thing in the file, and it stays visible only while the two are asked
separately.

| Field | Type | Meaning |
|-------|------|---------|
| `subject` | string | The id the measurement was asked about. Written whatever the outcome. |
| `family` | string, optional | Which family holds it: `node`, `vertex`, `edge` or `loop`. |
| `derived` | bool | Whether the figures below were computed. Written whatever the outcome, so a thing which measures nothing — a node referencing no loop — reads as `derived` true with no figures, where a boundary which could not be read reads as `derived` false. |
| `digest` | string, optional | The digest of the source tree the answer was computed against, lower-case hex, so a caller can check the computation against the tree in front of them. Written on a refusal too. Absent for a model which was not read from disk, or one a file of which could not be read at all. Under [`--assume`](#assumed), the digest of the tree the batch would produce. |
| `frame` | string, optional | The frame the answer was computed in. |
| `unit` | string, optional | That frame's linear unit. Nothing is converted into any other ([0005](decisions/0005-one-linear-unit-per-frame.md)). |
| `tolerance` | object, optional | The tolerance corners were judged coincident against: `name`, `value` and `unit`. |
| `area.value` | number | What the thing encloses. Absent, with the whole `area` object, for anything which encloses nothing an area can be computed of. |
| `area.unit` | string, optional | `unit` with a superscript two after it — `m²` — which is the square of the frame's linear unit. It is written that way rather than as a name of its own because the engine has none to write: what a project calls a square metre is its own vocabulary, in its own predicate declarations, and a computed area may not borrow it. |
| `length.value` | number | The extent of an edge, or the total length of the edges of a loop or a region. For a closed ring that is its perimeter. |
| `length.unit` | string, optional | The frame's linear unit. |
| `centroid.at` | array | Where the area is centred, component by component. The midpoint for an edge and the point itself for a vertex, which is the same definition one and two dimensions down. It is the area centroid and never the mean of the corners. |
| `centroid.unit` | string, optional | The frame's linear unit. |
| `bounds.min`, `bounds.max` | array | The corners of the axis-aligned bounding box, on the frame's own axes and on no others. The extent between them is not written: it is one subtraction, and a field restating it is a second place for it to be wrong. |
| `bounds.unit` | string, optional | The frame's linear unit. |
| `budget` | object, optional | The accuracy of the corners every figure was computed from, broken out by term. Same shape as [`budget`](#budget), without `from` and `to`. Absent where there is nothing to report — no terms, no combined figure and no reason for there being none — because an object carrying neither the figure nor a reason for its absence reads as an answer known exactly. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

**Every figure is written only where it could be computed.** "There is no answer" and "the
answer is zero" are different states, and a shape which does not close has the first. Nothing
encloses an area unless it is a ring which closes, does not cross itself and lies in one
plane; a projection of a shape which is not planar, and the signed sum over one which crosses
itself, are both numbers and neither is an area.

**The budget is of the corners and not of the area,** and it is one budget over the whole
measurement rather than one per figure. How much an area moves when a corner does is a
per-corner quantity, and a single number standing in for all of them would be exactly the
plausible-looking answer the rest of this refuses to give. What the budget does say is what
the answer rests on: which claims, which shared terms among them were counted once, and
whether any corner stated no accuracy at all. Independent terms combine in quadrature,
systematic ones of distinct ids linearly, and the two totals in quadrature, and a term
reached through four corners appears once with all four named under it
([SPEC §6.6.5](../SPEC.md#665-accuracy-terms),
[0006](decisions/0006-accuracy-is-one-sigma.md)).

That budget grows with the shape while the figures do not — one term per corner — which makes
this the most expensive call on the dimensional path. What it costs, and what each part of it
costs, is measured in [the token budget](token-budget.md).

**Exit `1`** is a measurement which could not be made: a ring which does not close, corners
which are not in one plane, a ring which crosses itself, one whose corners are collinear, a
corner nothing states the position of, a tolerance the registry does not declare in the unit
of the frame. Each is its own diagnostic naming which mistake it is. The object still comes
back with `derived` false and each of those diagnostics under
[`diagnostics`](#diagnostics), so a caller reads why from the object rather than from stderr.

A node which references no loop is **exit `0`** with `derived` true and no figures. A circuit
group and a warranty have no outline, which is not a fault in either of them.

### `tessellate`

The outline of one thing as rings of straight segments, drawn to a chord tolerance the run
names. It takes the id of the thing to draw and five flags, three of which are required.

| Flag | Meaning |
|------|---------|
| `--position <predicate>` | The predicate a corner's position is claimed under, which the boundary is read from. Required. |
| `--tolerance <name>` | The tolerance corners are judged coincident against and rings judged planar against. Required. |
| `--chord <name>` | The tolerance a straight segment standing in for a curve may fall from it by. Required. |
| `--arc-centre <predicate>` | The predicate a curved edge's centre is claimed under. |
| `--arc-through <predicate>` | The predicate the point a curved edge passes through is claimed under. |

None of the first three has a default and none of them ever will
([0012](decisions/0012-tolerances-are-registry-data.md)). How closely a curve has to be
followed is a decision a project makes — a millimetre for a setting-out drawing, a hundred
millimetres for an area take-off — and a value compiled into the command would be the engine
choosing the resolution of somebody else's drawing. A run that names none of the three is a
**usage error** naming every flag it was not given at once.

The last two are the vocabulary an arc is written in, and **they are a pair**: a centre with
no point on the curve beside it leaves two arcs between the same two ends — the short way
round and the long way round — and does not say which was meant, so naming one and not the
other is a **usage error**. A run that names neither reads every edge as straight, which is
what almost every edge is and what every edge of a model nobody has claimed an arc in is; the
engine carries no domain vocabulary, so which predicate holds a centre is never something it
knows ([0010](decisions/0010-the-engine-carries-no-domain-vocabulary.md)).

**This is the one place a curve becomes segments.** Nothing else in the engine tessellates on
the way to an answer: an area, a length, a centroid and a bounding box are computed from the
arc itself, so the resolution of a drawing never leaks into a figure somebody reports. What
this writes is a drawing, it says what it was drawn to, and nothing is written back into the
model ([0009](decisions/0009-derived-values-are-never-written-back.md)).

| Field | Type | Meaning |
|-------|------|---------|
| `subject` | string | The id the drawing was asked about. Written whatever the outcome. |
| `derived` | bool | Whether there is a region below. Written whatever the outcome, so a node with no outline to draw (`derived` true, `region.empty` true) reads differently from one whose outline could not be read (`derived` false). |
| `digest` | string, optional | The digest of the source tree the drawing was derived from, lower-case hex, so a caller can check the drawing against the tree in front of them. Written on a refusal too. Absent for a model which was not read from disk, or one a file of which could not be read at all. Under [`--assume`](#assumed), the digest of the tree the batch would produce. |
| `frame` | string, optional | The frame the boundary and the drawing are expressed in. |
| `unit` | string, optional | That frame's linear unit. Nothing is converted into any other ([0005](decisions/0005-one-linear-unit-per-frame.md)). |
| `tolerance` | object, optional | The tolerance corners were judged coincident against: `name`, `value` and `unit`. |
| `chord` | object, optional | The tolerance the curves were drawn to, same shape. It travels with the answer because a list of points that does not say how closely it follows the curve it came from is an approximation nobody downstream can judge, and nobody can reproduce. Absent, with `deviation`, for a node which references no loop: nothing was drawn for one, so there is no tolerance it was drawn to. |
| `deviation.value` | number | How far the worst segment of the drawing actually falls from the curve it stands in for. Absent wherever `chord` is absent, and absent on its own wherever `chorded` is written. |
| `deviation.unit` | string, optional | The frame's linear unit. |
| `chorded[].edge` | string | An edge of the boundary which states a curve this run did not read. Absent for a run which read every curve and for a node whose boundary claims none. |
| `chorded[].predicates` | array | The predicates that edge states a position under, which is what to name to have the curve read. |
| `chorded[].span` | object | Where that edge was written. |
| `region` | object, optional | What the drawing came to. Written for a drawing that succeeded whether or not it covers anything. Same shape as [`buildable`](#buildable)'s `region`. |
| `region.area` | number | What it covers, holes taken away, in the square of `unit`. It is the area of the segments and not of the curves — `measure` is what computes the exact figure, from the arcs themselves. |
| `region.empty` | bool | Whether it covers nothing. |
| `region.pieces[].area` | number | What one connected part encloses once its holes are taken away. |
| `region.pieces[].outer` | array | The ring bounding that part, closed without repeating its first corner, each corner as its components. |
| `region.pieces[].holes` | array, optional | The rings taken out of it. Absent where there are none. |
| `region.boundary[]` | array, optional | Which edge produced each straight run of the boundary. Same shape as [`buildable`](#buildable)'s `region.boundary`. Every run of a drawing names an edge: a chord standing in for part of an arc has `origin` `arc` and names the edge that bends along it. |
| `budget` | object, optional | The accuracy of the corners the drawing was read from, broken out by term. Same shape as [`budget`](#budget), without `from` and `to`. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

**`deviation` is what was achieved and `chord` is what was asked for,** and the two differ
because a curve is divided into a whole number of segments: an arc that needs two and a bit
gets three, and follows the curve more closely than it had to. The deviation is always within
the chord tolerance, and it is reported so that a caller can check the approximation it got
against the one it asked for rather than assuming the bound was met exactly.

**A drawing never reports a `deviation` it did not achieve.** A run which did not name
`--arc-centre` and `--arc-through` over a boundary whose edges claim a curve drew the straight
line between two corners rather than the wall, and its distance from that wall is however far
the wall bows — a figure this run has no vocabulary to compute. So no `deviation` is written
at all, and `chorded` is written instead, naming the edges and the predicates to name. Zero is
the one answer that must not be given: beside a named `chord` it is an affirmative statement
that the curve was followed exactly, and it is precisely the field a consumer would assert on
to prove that it had. `chord` is still written, because what a caller asked for is part of what
it got.

**A boundary with nothing curved in it is drawn to itself, unchanged** — the same rings, the
same orientation, `deviation` zero — so this is one command rather than one for curved
outlines and another for straight ones. That zero is the true one: four straight edges were
followed exactly. A node which references **no loop** is the
other case and reads differently: it is **exit `0`** with `derived` true and an empty
`region`, and neither `chord` nor `deviation` is written, because nothing was drawn. A campus
and a warranty have no outline, which is not a fault in either of them.

**The rings are nested and wound the way every other region's are.** A ring inside an odd
number of others is a hole and runs the other way round from the ring holding it, which is the
same even-odd rule `measure` takes a courtyard's area away by; nothing in the model declares
which loop is the outside one. A region drawn here and a region read by any other command are
therefore interchangeable downstream. Drawing each loop separately is **not** the same thing:
which ring is a hole is a property of the region and not of any ring in it.

Nesting is decided at the segments here rather than at the corners, and that is what makes a
curved outline nestable at all. A courtyard whose wall bows out past a corner of the plate
around it is inside the plate and outside the polygon of its chords, so a count taken at the
corners would flip on which side of a bulge a corner happened to fall — a region wrong by a
whole ring rather than by a sag. That is the shape `measure` refuses to nest rather than
answer wrongly about, and drawing the curve is the caller deciding to answer it, to a
resolution they named.

**Exit `1`** is a drawing which could not be made: everything that refuses a region refuses
this — a ring which does not close, a corner nothing states the position of, corners which are
not in one plane, a ring which crosses itself, one whose corners are collinear, a tolerance
the registry does not declare in the unit of the frame. So is an arc which the named chord
tolerance would take more segments to follow than anything can use: that is refused with a
diagnostic naming the edge, rather than truncated, because a tolerance far finer than the
coordinates the arc was surveyed to draws a curve to a resolution nothing behind it supports.
The result object still comes back with `derived` false and the refusal under
[`diagnostics`](#diagnostics), so a caller reads why from the object rather than from stderr.

### `buildable`

What may be built inside a boundary once the setback claimed on each of its edges has been
taken off it. It takes the id of the thing to derive, and three flags — none of which has a
default.

| Flag | Meaning |
|------|---------|
| `--setback <predicate>` | The predicate an edge's setback distance is claimed under. Required. |
| `--position <predicate>` | The predicate a corner's position is claimed under, which is what the boundary is read from. Required. |
| `--tolerance <name>` | The tolerance corners are judged coincident against and rounded corners are drawn to. Required. |

Which predicate carries a setback, which carries a position, and how close two corners are
one corner are project data ([0012](decisions/0012-tolerances-are-registry-data.md)). A
default compiled into the command would be the engine deciding one of them on a project's
behalf, so a run that names none of the three is a **usage error** naming every flag it was
not given at once.

Nothing in the model says what is buildable, and nothing here writes it back
([0009](decisions/0009-derived-values-are-never-written-back.md)). The region is read out of
the corners, the edges and the claims every time it is asked for, so it cannot disagree with
any of them — which matters more here than anywhere else, because the shape it describes is
the one a permanent structure gets placed against.

| Field | Type | Meaning |
|-------|------|---------|
| `subject` | string | The id the derivation was asked about. |
| `derived` | bool | Whether there is a region below. Written whatever the outcome, so a parcel whose setbacks left nothing of it (`derived` true, `region.empty` true) reads differently from one whose setbacks could not be read (`derived` false). |
| `digest` | string, optional | The digest of the source tree the region was derived from, lower-case hex, so a caller can check the derivation against the tree in front of them. Written on a refusal too. Absent for a model which was not read from disk, or one a file of which could not be read at all. Under [`--assume`](#assumed), the digest of the tree the batch would produce. |
| `frame` | string, optional | The frame the boundary and the answer are expressed in. |
| `unit` | string, optional | That frame's linear unit. Every distance here is in it and every area in the square of it. |
| `tolerance` | object, optional | The tolerance corners were judged coincident against: `name`, `value` and `unit`. |
| `parcel` | object, optional | The boundary the setbacks were taken off, as the model holds it. Same shape as `region`. |
| `setbacks[].edge` | string | The id of the edge the setback was claimed on. |
| `setbacks[].distance` | number | How far back it pushes the boundary, in `unit`. |
| `setbacks[].unit` | string | The unit that distance is in, which is the frame's. |
| `setbacks[].claim` | string, optional | The id of the claim it was resolved from. Absent for a claim that wrote none, which is most of them. |
| `setbacks[].source` | string, optional | The evidence the distance came from: a consent, a statute, a deed. |
| `setbacks[].span` | span | Where that claim was written. |
| `region` | object, optional | What is left buildable. Written for a derivation that succeeded whether or not it covers anything. |
| `region.area` | number | What it covers, holes taken away, in the square of `unit`. |
| `region.empty` | bool | Whether it covers nothing, which is a state of the answer rather than an absence of one. |
| `region.pieces[].area` | number | What one connected part encloses once its holes are taken away. |
| `region.pieces[].outer` | array | The ring bounding that part, closed without repeating its first corner, each corner as its components. |
| `region.pieces[].holes` | array, optional | The rings taken out of it. Absent where there are none. |
| `region.at` | array, optional | Where a node drawn as a point sits, as its components in the order they were written. It is the whole shape of such a node, which covers no area and has no boundary to attribute, so a region carrying it has no pieces. Absent for every region read from loops and for every region an operation over an area produced — which is what tells a thing with a position from a thing with an outline, and why a buildable region never carries it. |
| `region.boundary[].ring` | number | Which ring of the boundary a straight run belongs to, counted from zero in the order the rings are traversed. |
| `region.boundary[].edge` | string | The id of the edge that run was written as, or whose arc it stands in for. |
| `region.boundary[].origin` | string | What produced the run: `edge` where it is the edge itself, corner to corner as it was written, and `arc` where it is one chord of the drawing of the arc that edge bends along. |
| `region.boundary[].reversed` | bool | Whether the run goes against the order the edge was written. |
| `region.boundary[].from` | array | The corner the run leaves, as its components. |
| `region.boundary[].to` | array | The corner it arrives at. |
| `budget` | object, optional | The accuracy of the answer broken out by term, over the position claims and the setback claims together. Same shape as [`budget`](#budget), without `from` and `to`. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

**`boundary` is what attributes a ring back to the model it came from.** A polygon on its own
is anonymous coordinates: it cannot say which segment is the party wall, cannot carry a
relationship onto the element backing the edge behind it, and cannot carry a claim written on
an edge through to the run that edge produced. Every consumer that wants those has to
re-derive the correspondence by matching coordinates, which is exactly the re-derivation this
engine exists to prevent — so the pairing is reported from where the boundary is assembled and
is known.

**The direction is stated because a loop traverses an edge in either order.** Two regions
either side of a party wall name one edge and run through it opposite ways, and a caller that
read the edge's own vertices and assumed the run followed them would draw one of them inside
out.

**A run an operation produced names no edge, and is not written.** `boundary` carries the runs
an edge is behind, which is every run of a region read from the model — `parcel` here, and the
`region` of a `tessellate` — and none of a region an operation produced. The boundary of a
buildable region, of an intersection or of an offset runs where the operation put it, and
naming the nearest edge that nearly produced it would be a lie the next derivation acts on.
Repeating those corners under a name that says only "an operation put this here" would double
the payload to say what `pieces` already says, so `boundary` is **absent** for such a region;
`derived` on the result is what tells that apart from a region nothing was computed for.

Different setbacks per edge are the ordinary case — six metres at the road, four at the rear,
three at each flank — and which edge is which is not modelled. A setback is a claim written on
the edge it governs, so the numbers go on the edges and each is applied where it was written;
the engine carries no domain vocabulary
([0010](decisions/0010-the-engine-carries-no-domain-vocabulary.md)).

An edge with no live setback claim is **exit `1`**, with a diagnostic naming that edge —
never a setback of nought. An edge that really is not set back says so, as a claim with a
value of nought and the provenance every other value carries. Two claims equally current
about one edge, a setback written outwards, one written in a unit the frame is not in and one
shorter than the tolerance are refused the same way. The result object still comes back with
`derived` false and the refusal under [`diagnostics`](#diagnostics), so a caller reads why
from the object rather than from stderr.

Setbacks that meet in the middle are **exit `0`**: `derived` is true, `region.empty` is true,
and a warning on stderr says which parcel its own regime consumed. That is the answer to the
question rather than a failure to answer it, and it is reported so that an empty region cannot
be read as one that was never computed. What never comes back is the inside-out shape
offsetting each edge on its own produces when the offsets cross over each other.

### `site`

Whether one thing fits inside another, across whatever frames the two are declared in, and
how well that answer is known. It takes the id of the subject and five flags.

| Flag | Meaning |
|------|---------|
| `--within <id>` | The thing the subject has to sit inside. Required. |
| `--position <predicate>` | The predicate a corner's position is claimed under, which both outlines are read from. Required. |
| `--tolerance <name>` | The tolerance corners are judged coincident against and rounded corners are drawn to. Required. |
| `--clearance <distance>` | How much room the subject has to keep between itself and the envelope's boundary, in the linear unit of the envelope's frame. Default `0`, which is "inside it at all". |
| `--setback <predicate>` | The predicate an edge's setback distance is claimed under. Given, the subject is sited inside what the envelope's setbacks leave buildable rather than inside its outline. No default: without it the subject is sited inside the outline, exactly as before the flag existed. |

The subject is read out of the corners surveyed in its own frame, carried into the envelope's
frame across the transform claims which relate the two, grown by the required clearance,
overlaid on the envelope and measured. Every step accumulates the accuracy of what it read
([0006](decisions/0006-accuracy-is-one-sigma.md)), so the clearance and its error bar are two
halves of one answer. Nothing is written back
([0009](decisions/0009-derived-values-are-never-written-back.md)).

**The budget is the point.** The georeference is one transform applied to every fact declared
indoors, so its residual does not cancel between two indoor points and does not average away
against an outdoor one. Each systematic term is counted once however many inputs
contributed it — which matters most in exactly this query, because a control point behind
the interior corners is routinely behind the boundary survey and the georeference as well —
and terms of distinct ids add linearly, because nothing states how far two sources
correlate and full correlation is the bound. That total joins the independent terms' total
in quadrature, since an independent error correlates with nothing
([SPEC §6.6.5](../SPEC.md#665-accuracy-terms)). Combining everything in quadrature reports
a narrower answer than the evidence supports, which is the direction nobody investigates.
[The worked example](siting-worked-example.md) runs one query end to end, from the claims
involved to the final budget.

**`--setback` sites inside what the plot allows rather than inside the plot.** The setback
claimed on each edge of the envelope is taken off that edge exactly as
[`buildable`](#buildable) takes it off, and the subject is sited inside what is left. The
region is derived and never authored
([0009](decisions/0009-derived-values-are-never-written-back.md)), and it is derived inside
this query rather than beside it, so one budget carries the setback claims and the envelope's
corners beside the subject's corners and the transforms. A systematic term they share — a
control point behind the boundary survey and the georeference alike — is counted once. Taking
`budget.combined` from a `buildable` answer and from a `site` answer against the outline and
combining the two counts it, and the envelope's corners, twice. `--clearance` is kept on top
of the setbacks rather than instead of them.

| Field | Type | Meaning |
|-------|------|---------|
| `subject` | string | The id which was sited. |
| `within` | string | The id it had to sit inside. |
| `sited` | bool | Whether there is an answer below. Written whatever the outcome, so a subject which does not fit (`sited` true, `verdict` `does-not-fit`) reads differently from a question which could not be asked (`sited` false). |
| `digest` | string, optional | The digest of the source tree the answer was computed against, lower-case hex, so a caller can check the computation against the tree in front of them. Written on a refusal too. Absent for a model which was not read from disk, or one a file of which could not be read at all. Under [`--assume`](#assumed), the digest of the tree the batch would produce. |
| `frame` | string, optional | The frame the answer is expressed in, which is the envelope's. |
| `declared-in` | string, optional | The frame the subject was written in. |
| `carried` | bool | Whether a frame chain was walked to compare the two, which is what says whether a georeference is in the budget at all. |
| `unit` | string, optional | The linear unit of `frame`. Every distance here is in it and every area in the square of it. |
| `tolerance` | object, optional | The tolerance corners were judged coincident against: `name`, `value` and `unit`. |
| `verdict` | string, optional | One of `fits`, `might-fit`, `does-not-fit`, `unknown`. |
| `decided` | bool | Whether the verdict answers the question. False for `might-fit` and for `unknown`. |
| `clearance.required` | number | The clearance the subject was asked to keep. |
| `clearance.actual` | number | How much room it has. Negative where it does not sit inside: how far the part which is outside reaches past the boundary where the two overlap, and how far apart they are where the subject is not over the envelope at all. |
| `clearance.margin` | number | `actual` less `required`, which is the quantity the verdict is decided on. |
| `clearance.unit` | string, optional | The linear unit all three are in. |
| `clearance.uncertainty` | object, optional | How well the margin is known: `magnitude`, `unit` and `coverage-factor`. Absent where the budget could not be reduced to one figure, which `budget` says the reason for. |
| `envelope` | object, optional | The region the subject had to sit inside. Same shape as `buildable`'s `region`, `boundary` included where the envelope was read from the model rather than carried into another frame. |
| `proposal` | object, optional | The subject, expressed in the envelope's frame. |
| `needed` | object, optional | The proposal grown by the required clearance, which is the shape the envelope had to accommodate. The proposal itself where nothing beyond fitting at all was required. |
| `shared` | object, optional | What the two have in common. |
| `spill` | object, optional | What the proposal needs and the envelope does not offer. Where a refusal points: a fit answered only by "no" leaves somebody to work out which corner is over the line. |
| `parcel` | object, optional | The envelope's outline as the model holds it, which the setbacks were taken off. Same shape as `buildable`'s `parcel`. Written only where `--setback` was given, so a run without it is the same bytes it always was; `envelope` is then what the setbacks leave, and carries no `boundary` because an operation produced it. |
| `setbacks` | array, optional | The setbacks which were applied, one per edge of the envelope in the order its loops traverse them. Same shape as `buildable`'s `setbacks[]`: `edge`, `distance`, `unit`, `claim`, `source`, `span`. Written only where `--setback` was given. |
| `budget` | object, optional | The accuracy of the answer broken out by term, over the position claims behind both outlines, the setback claims where `--setback` was given, and the transform claims of every frame the subject was carried through. Same shape as [`budget`](#budget), without `from` and `to`. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

The four verdicts are four different situations and are never rounded into two. A clearance
of forty millimetres is a comfortable fit where the answer is known to five and no answer at
all where it is known to sixty; both come back as the same number, and only the verdict tells
them apart. `might-fit` says the model as measured cannot tell, and what to do about it is to
re-measure whatever dominates the budget. `unknown` says the uncertainty could not be
computed at all — an unstated accuracy is unknown rather than nought — and what to do about
it is to state the accuracy the budget names as missing.

A subject which does not fit is **exit `0`**: the command answered, and the answer is no. So
is one whose verdict is withheld, with a warning on stderr saying which of the two reasons it
was. **Exit `1`** is a question which could not be answered — an outline which could not be
read, two frames with no measured chain between them, a clearance shorter than the tolerance,
a clearance written as a distance outwards — and, under `--setback`, an edge of the envelope
with no live setback claim, which is a diagnostic naming that edge and never a setback of
nought, and every other setback `buildable` refuses. The object still comes back with `sited`
false.

Setbacks which leave nothing buildable are **exit `0`**: nothing fits an empty region, so the
verdict is `does-not-fit` and `decided` is true, `envelope.empty` is true, and a warning on
stderr says which parcel its own setbacks consumed. `clearance` is **absent**, because there
is no boundary to measure a room to and a clearance of nought would read as a subject touching
one. That is the answer to the question rather than a failure to answer it.

### `plan`

What a spatial node contains, as rings, with the claims written on them. It takes the id of
the thing to plan and eight flags, three of which are required — and none of which has a
default.

| Flag | Meaning |
|------|---------|
| `--annotate <predicate>` | A predicate whose claims are reported on every ring and on the edges bounding it. Repeatable, and at least one is required. |
| `--position <predicate>` | The predicate a corner's position is claimed under, which the rings are read from. Required. |
| `--tolerance <name>` | The tolerance corners are judged coincident against. Required. |
| `--arc-centre <predicate>` | The predicate a curved edge's centre is claimed under. |
| `--arc-through <predicate>` | The predicate the point a curved edge passes through is claimed under. |
| `--chord <name>` | The tolerance a straight segment standing in for a curve may fall from it by. |
| `--kind <kind>` | Only descendants that declare this kind. [Repeatable](#filters). |
| `--type <name>` | Only descendants that declare this type. [Repeatable](#filters). |

The last three are the vocabulary a curved wall is read under, exactly as
[`tessellate`](#tessellate) reads one, and all three are needed to read one: a ring is a list
of points, so a curve has to become points somewhere, and `--chord` is where it is said how
closely. **`--arc-centre` and `--arc-through` are a pair**: a centre with no point on the
curve beside it leaves two arcs between the same two ends and does not say which was meant,
so naming one and not the other is a **usage error**. A run which names both and no `--chord`
reads a curve it meets as a ring it cannot draw — that room comes back under `undrawn` as
`unreadable-boundary`, with a diagnostic saying no chord tolerance was named — rather than
drawing it to a resolution nobody chose.

A run which names neither predicate draws every edge as the straight line between its two
ends, and says so wherever the model states otherwise: `chorded` lists every edge which claims
a position — which is how and only how a curve is written, because an edge has no position of
its own — and a warning on stderr names each of them. A ring drawn straight through a curve is
a drawing error rather than a rounding, and a sheet is the last place anything would notice
it.

**This is a query and not an export.** It writes no file
([0022](decisions/0022-a-command-whose-product-is-a-file-answers-on-stdout.md) is about the
commands that do). It returns the rings the model already holds and the claims already
written on the edges bounding them, under the same envelope, digest and budget every other
answer carries, and it knows nothing about paper, scale, title blocks, text height or where a
leader goes. Those are the consumer's, and this command is the boundary that keeps them so.

**`--kind` and `--type` narrow what is reported and never what is walked**, under the
[filter rule](#filters) exactly as [`traverse`](#traverse) has them: within one flag any of its
values, across the two flags both. Under `--type Office` an office three levels below the
subject is reported though nothing between it and the subject is an office. A kind that is not
one of the seven is a usage error raising the same error `traverse` does, listing the seven,
and a type the registry does not declare is one pointing at [`list-types`](#list-types) — exit
`3`, with nothing on stdout. A declared type nothing instantiates is an empty `outlines` and
exit `0`. Everything computed from the rings drawn — the survey they are read against,
`budget`, `chord` and `deviation`, and `chorded` — is over the selected nodes alone, and a
selected node's outline is exactly what it is in the unfiltered plan, `region` and
`annotations` alike: a filter decides which rooms come back and never how a room is drawn. A
plan of one type is how a sheet of the meeting rooms, or a site plan of the site nodes, is
asked for without taking every outline in a parcel back to reach a dozen of them; a sheet of
several types asks for them in one call by repeating `--type`. `--depth` is deliberately not
offered: how deep a node is nested reflects how an author happened to group things, and the
type is what a renderer already switches on.

**`--annotate` is the whole of the answer to "is this dimension worth drawing".** It is worth
drawing if the caller asked for that predicate. Nothing else in the payload encodes a drawing
judgement, which is what keeps the engine from acquiring a drawing convention every consuming
project would then disagree with — the same rule that keeps domain vocabulary out of it
([0010](decisions/0010-the-engine-carries-no-domain-vocabulary.md)).

| Field | Type | Meaning |
|-------|------|---------|
| `subject` | string | The id the plan was asked about. |
| `planned` | bool | Whether every ring below could be read. Written whatever the outcome, so a storey nobody has outlined yet (`planned` true, `outlines` empty) reads differently from one a room of which could not be read (`planned` false). |
| `digest` | string, optional | The digest of the source tree the rings and the claims were read from, lower-case hex, so a consumer can say which model a sheet was drawn from. Written on a refusal too. Absent for a model that was not read from disk. Under [`--assume`](#assumed), the digest of the tree the batch would produce. |
| `frame` | string, optional | The frame every coordinate here is in. It is the frame the subject declares; where the subject declares none, the frame of its first boundary loop, which is how a node's frame is read everywhere else; and where it has neither, the root frame. |
| `unit` | string, optional | That frame's linear unit. Every coordinate here is in it and every area in the square of it. |
| `tolerance` | object, optional | The tolerance corners were judged coincident against: `name`, `value` and `unit`. |
| `annotating` | array | The predicates the run asked for, in the order it named them and with a repeat written once. |
| `filter` | object, optional | The filters the run was narrowed by: `kind` and `type`, each an array of the values given, in the order they were written and with a repeat written once, and each absent where its flag was not given. Absent where neither was given. It is echoed for the reason `annotating` is: a stored plan of the meeting rooms and one of the whole storey answer different questions, and the object says which. |
| `chord` | object, optional | The tolerance the curves of the rings were drawn to: `name`, `value` and `unit`. Of the plan rather than of any outline in it. It travels with the answer because a ring that does not say how closely it follows the curve it came from is an approximation nobody downstream can judge, and nobody can reproduce. Absent, with `deviation`, for a plan in which no ring bent — a storey of straight walls, and a run which read no curve: nothing was approximated, so there is no tolerance it was drawn to. |
| `deviation.value` | number | How far the worst segment of any outline actually falls from the curve it stands in for. Absent wherever `chord` is absent, and so absent wherever `chorded` is written: a curve that was not read was not drawn to anything. |
| `deviation.unit` | string, optional | The frame's linear unit. |
| `chorded[].edge` | string | An edge of a ring which states a curve this run did not read. Absent for a run which read every curve and for a storey which claims none. |
| `chorded[].predicates` | array | The predicates that edge states a position under, which is what to name to have the curve read. |
| `chorded[].span` | object | Where that edge was written. |
| `outlines` | array | One entry per contained node that was drawn — and that the filter selects, where one was given — in id order. Empty rather than null for a subject that contains nothing drawable, and for a filter that selects nothing. |
| `outlines[].node` | string | The id of the node the rings were read from, which is what names them. |
| `outlines[].label` | string, optional | What it is called. Absent where it is called nothing. |
| `outlines[].kind` | string, optional | The kind it declares. |
| `outlines[].type` | string, optional | The type it declares. |
| `outlines[].within` | string | The id of the node it is directly within — the same value [`get`](#get) reports as `entity.within`, and never an ancestor further up. Always written: every outline is a descendant of the subject and so is within something, and the subject's children name the subject. It is what a renderer groups rooms by storey, or draws an alcove inside its room, from. |
| `outlines[].region` | object | The area it covers, with `area`, `empty`, `pieces`, `at` and `boundary` exactly as [`buildable`](#buildable) writes them. A node drawn as a point — a panel, a receptacle, a survey monument — is an outline whose region carries `at`, the coordinate a sheet places a symbol at, and no pieces: it covers nothing and has no boundary to attribute. |
| `outlines[].declared-in` | string, optional | The frame the node's shape was read in, written only where it is not `frame`: the region was carried out of it, and the claims were not. A claim is reported as it was written, so a coordinate-valued annotation on this node is a coordinate in this frame. |
| `outlines[].annotations` | array | The claims reported on it, the node's own first and then those of each edge of its boundary. Empty rather than null for a room nobody has written anything on. |
| `outlines[].annotations[].anchor.kind` | string | Which family the claim is written on: `edge` for one written on an edge of a ring, `node` for one written on the node that ring bounds. |
| `outlines[].annotations[].anchor.id` | string | The id of that edge or that node. |
| `outlines[].annotations[].anchor.vertices` | array, optional | The edge's two corners, in the order the edge was authored. Absent for a node anchor. |
| `outlines[].annotations[].anchor.rings` | array, optional | The loops bounding the node, in the order it references them. Absent for an edge anchor. |
| `measured` | array, optional | One entry per edge that bounds no outline the plan drew, both of whose vertices are corners of the outlines it drew, and that carries at least one live claim under an `--annotate` predicate — in edge id order. Absent where there is none, so a model nobody measured across writes the bytes it always did. The rule is set out below the table. |
| `measured[].edge` | string | The id of the edge. |
| `measured[].label` | string, optional | What the edge is called. Absent where it is called nothing. |
| `measured[].vertices` | array | The edge's two vertices in the order it was authored, exactly as `anchor.vertices` gives them. |
| `measured[].from` | array | Where the first of those vertices is, in `frame`: identical to the coordinate that corner has in `outlines[].region.boundary`, which is where it is taken from. |
| `measured[].to` | array | Where the second is, on the same terms. |
| `measured[].declared-in` | string, optional | The frame the edge was written in, written only where it is not `frame`, as an outline's is: a coordinate-valued claim on it is in this frame. |
| `measured[].annotations` | array | The live claims on it under the annotated predicates, each with an `edge` anchor, in the shape and the order an outline's annotations take. |
| `undrawn` | array, optional | One entry per contained node that was **not** drawn — and that the filter selects, where one was given — in id order. Absent for a subject every node of which was drawn — a key a consumer has to read to learn nothing is one that should not be there. |
| `undrawn[].node` | string | The id of the node that was not drawn. |
| `undrawn[].label` | string, optional | What it is called. Absent where it is called nothing. |
| `undrawn[].kind` | string, optional | The kind it declares. |
| `undrawn[].type` | string, optional | The type it declares. |
| `undrawn[].within` | string | The id of the node it is directly within, exactly as `outlines[].within` is, and always written for the same reason. |
| `undrawn[].reason` | string | Why it was not drawn: `no-boundary` for a node that references no loop, `unreadable-boundary` for one whose loops this run could not read, `no-position` for a node drawn as a point which nothing claims a position of under `--position`, `uncarried` for one whose shape was read in another frame and could not be carried into `frame` — the two frames are not related, a transform on the way could not be applied, or `frame` is in a unit other than the tolerance's. A closed set. The detail — which loop, which corner, where — is the diagnostic behind it in [`diagnostics`](#diagnostics), whose `nodes` names this node and whose `ids` names the shapes it is about. |
| `undrawn[].declared-in` | string, optional | The frame the node's shape was read in — or, for a node with no shape, the frame it declares — written only where it is not `frame`, so that its claims can be read in the frame they were written in. |
| `undrawn[].annotations` | array | The claims reported on it, in the same order and the same shape as an outline's. Empty rather than null. A node that references no loop has no edges, so what it carries is exactly its own claims and no edge anchors. |
| `budget` | object, optional | The accuracy of the rings, over the position claims that put every drawn corner where it is, and the transform claims of every frame an outline was carried through, and over the rings that were **drawn** — a ring that was refused put no corner anywhere. Same shape as [`budget`](#budget), without `from` and `to`. Absent where there is nothing to report — no terms, no combined figure and no reason for there being none — because an object carrying neither the figure nor a reason for its absence reads as an answer known exactly. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

Beside `anchor`, every annotation carries the claim object `get` writes — `id`, `predicate`,
`value`, `source`, `method`, `accuracy`, `combined` or `units`, `date`, `rank` and `span` — so a claim on a plan
reads exactly like a claim anywhere else in this contract. `resolution` is **never** written,
because nothing here was resolved.

**A rendered string is a claim, not a formatting of a number.** The whole claim comes back
rather than a value and a unit, and that matters more on a sheet than anywhere else: the
string a renderer prints against a wall is something somebody stated, from a source, by a
method, on a date, to an accuracy, and printing it without them is how a design estimate comes
to look like an as-built survey
([0009](decisions/0009-derived-values-are-never-written-back.md)).

**The anchor is what stops a consumer re-deriving the pairing.** A ring of coordinates and a
list of claims beside it leaves whoever draws the sheet to work out which dimension belongs to
which pair of corners, by matching ids or worse by matching coordinates — which is exactly the
re-derivation `region.boundary` exists to prevent one layer down. So an edge anchor names its
two vertices and a node anchor names its rings, and neither has to be looked up again.

The vertices are the **edge's own order** and not the order any ring traverses them. Two rings
either side of a party wall run through one edge opposite ways, and a claim written on the
edge is written on the edge rather than on either traversal of it; a consumer that needs the
traversal direction reads it from `region.boundary`, where that question is already answered.
One edge therefore carries one anchor, identical from both rooms that reference it.

**Nothing is resolved.** Where two live claims compete under one predicate on one anchor, both
come back with the same anchor, and which of them a sheet prints is the caller's decision — a
query that picked one would be making that decision invisibly and in the wrong place. A
retracted claim is never reported, because resolution never considers one and a sheet printing
a value somebody has withdrawn is the failure this refuses to make possible.

**The budget is over the geometry and not over the annotations.** It answers the question a
sheet has to carry — how well is the line I am drawing known — and each claim reported carries
its own accuracy, because each is a separate statement about a separate quantity and combining
a room's area with a wall's fire rating would produce a figure of nothing at all
([0006](decisions/0006-accuracy-is-one-sigma.md)).

**Every outline is carried into `frame`, the way [`site`](#site) and `export-map` carry a
region.** A room declared on a grid other than the plan's is read in its own frame and then
expressed in the plan's, so a porch set out on the main floor's grid is drawn beside the house
rather than at the origin of the survey grid. The transform is a measurement, so its accuracy
is merged into `budget` — a carried outline is known no better than the fit that carried it.
Unlike a region carried anywhere else, a carried outline **keeps its edge attribution**: a
transform between two frames maps each authored run onto exactly one carried run, so every run
of `region.boundary` keeps its `ring`, `edge`, `origin` and `reversed`, and only `from` and `to`
move. The claims do not move — a claim is reported as it was written — which is what
`declared-in` is for. A curve is judged against `--chord` where it was drawn, before the
carry. A node that cannot be carried is named under `undrawn` as `uncarried`, its claims still
reported, and the diagnostic saying why goes to stderr; planning across a unit boundary stays
impossible, because one `--position` and one `--tolerance` each carry one unit. A model whose
subject and every contained node are declared in one frame carries nothing and writes the
bytes it always did.

**A measured edge is on the plan when both its ends are corners the plan drew.** A
measurement between two corners is an ordinary edge carrying a claim, and nothing requires that
edge to be in any loop: a dimension string across a room, a span between two jambs. Such an
edge bounds nothing, so it is on no outline, and `measured` is where it comes back. A corner is
a vertex of an edge in the boundary of a drawn outline, a ring or an open run alike; the edge
must itself bound no drawn outline, and must carry a live claim under an `--annotate`
predicate, which keeps `--annotate` the whole of the answer to what is worth drawing. An edge
that bounds a drawn outline is not in `measured`, and its claims stay on that outline.

The corners place it because they are structural: a measured edge between two corners of a
storey's rings is about that storey. Containment cannot place it — an edge has no `within` —
and **its frame is deliberately not used**. The frame is what a consumer filtering edges by
hand would reach for, but a frame is not a place: two storeys may share one, and a plan of a
parcel would pull in every edge on the site grid whether or not anything drawn meets it. Under
`--kind` or `--type` the corners are those of the outlines the plan reported, so a measurement
across a room the filter did not select is not on a sheet of those it did. **An edge running to
a vertex no drawn outline has is not on the plan**, and stays with
[`list-geometry`](#list-geometry), which finds every edge carrying a claim whatever it bounds.

`from` and `to` are the corners as the outlines drew them — in `frame`, carried as those
outlines were — and so equal the `from` of the `region.boundary` runs leaving those vertices,
and what [`resolve`](#resolve) says of each vertex in `frame`. The ends were drawn, so their
position claims are already in `budget`, which a measured edge leaves unchanged. **A curve
claimed on a measured edge is not drawn**: a measurement runs between two points, so `from`
and `to` are its ends, and the edge is never listed in `chorded`.

Which nodes are drawn is every descendant of the subject — every one the filter selects,
where one is given — that references at least one loop this run could read, however deep: a
room inside a storey and an alcove inside that room are both places somebody draws. So is every descendant whose declared geometry is `point` and
which a claim under `--position` places, read from the position claimed of the node itself:
its outline carries `at` and no pieces, because a panel, a receptacle and a survey monument
are each that shape, and a plan that reported them as having no boundary would leave every
device on a floor off the sheet. The subject itself is not drawn — the question is what is in
it.

**Nothing the filter selects is dropped** — and with no filter given, it selects everything the
subject contains. Every selected descendant that was not drawn comes back under `undrawn`,
named, with what it is, why it was not drawn and the claims written on it — so `outlines` and
`undrawn` partition between them every node the filter selects, and a renderer that drew
every outline and listed every undrawn node has drawn or named every one of them: under no
filter, the whole storey. A node the filter does not select appears in neither list, because
it was not asked about, which is a different thing from being left off a sheet it was asked
for. A circuit group has no edges and is ordinary; a ring that does not close is a defect;
both are things somebody put inside that storey. This is the one place the payload could omit
an authored fact without saying so, and the failure it would cause has no downstream symptom
at all: the sheet renders, looks complete, and is missing a door.

**The reason is a token and the detail is a diagnostic.** `reason` says which of them
applies, because that is what decides whether anybody has to act — nobody fixes a circuit
group, somebody fixes a ring that will not close or places a receptacle nobody set out — and a consumer deciding that should read a
field rather than match prose. Where the reason is a defect, the diagnostic behind it is
under [`diagnostics`](#diagnostics) in the same object, carrying the loop, the file, the
position and the size of the gap, and is rendered on stderr for whoever wrote the file. The
two are one `dfcad.Diagnostic` written twice, by `encoding/json` and by `Render`, so there is
nothing to keep in step between them.

**An undrawable node degrades on its own and never refuses the storey**, whichever way it is
undrawable. The other rooms are still drawn and the object still comes back. Whether the *run*
succeeded is the separate question the diagnostics answer, and the two are kept apart so a
caller can draw the seven rooms it has while it fixes the eighth. A ring that does not close
and a ring that crosses itself are treated identically — they are two spellings of one
mistake, and behaving differently between them would let which of the two a model happens to
hold decide how much of the sheet comes back.

An `undrawn` entry is **not** an outline covering nothing, and the two must not be conflated.
An open run of edges — a doorway, a railing — legitimately covers no area and is drawn from
`region.boundary`, so it is an outline with `region.empty` true; a consumer that read "no
area" as "not drawn" would leave every door off the sheet.

A subject that is not a place is a **usage error** — exit `3`, with nothing on stdout. A zone
holds its members by membership and contains nothing, so answering "nothing is in here" for
one would read as a zone whose members have no outlines, which is a quieter wrong answer than
refusing the question. A predicate the registry does not declare is a usage error for the same
reason it is one in `claims`.

A storey containing nothing with an outline is **exit `0`** with `outlines` empty: that is the
truthful answer to what it looks like in plan, and it is reported so that it cannot be read as
a plan that was never computed. A storey holding a node that references no loop is exit `0`
too, with that node under `undrawn`: nothing is wrong with a circuit group, and a diagnostic
about one would be a diagnostic about a model in which nothing is wrong.

**Exit `1`** is a plan a ring of which could not be read — a boundary that does not close, one
that crosses itself, corners that are not in one plane, a tolerance the registry does not
declare in the frame's unit, a curve met with no `--chord` named — or a node drawn as a point
that nothing places, which declares that its shape is where it is and then does not say where,
or a node whose shape could not be carried into `frame`.
The other rooms are still drawn and the object still comes back
with `planned` false and the room named under `undrawn`, because a sheet with one room missing
is more use than no sheet and the diagnostics — on stderr, and under
[`diagnostics`](#diagnostics) in the object — say which room to fix.

### `check`

Every rule the model states, run: each type's invariants bound to each of its instances, and
each assertion written on a thing. It is the gate — one command, one exit code, and a report
naming what failed and where the rule that failed it is written. It takes no arguments and
four flags.

| Flag | Meaning |
|------|---------|
| `--subject <id>` | Only the rules bound to this thing. [Repeatable](#filters). |
| `--type <name>` | Only the rules bound to instances of this type. [Repeatable](#filters). |
| `--check <name>` | Only the rules naming this check. [Repeatable](#filters). |
| `--list` | Write what would run, and run none of it. |

Filters combine: a rule is selected when it satisfies every filter given, and a filter written
more than once is satisfied by any of its values. A name nothing answers to — an id no thing
in the model holds, a type no registry file declares, a check the engine does not register —
is a **usage error** rather than an empty run, because a gate that passed on a misspelled
filter would pass on nothing having run at all.

`--subject` takes the id of a node, a vertex, an edge or a loop, because an assertion is
written on any of the four; that is why it is not spelled `--node`. `--type` never selects a
rule written on a vertex, an edge or a loop, because none of them declares a type.

```json
{
  "version": 2,
  "command": "check",
  "refused": false,
  "summary": {"checks": 7, "runnable": 7, "ran": 7, "passed": 5, "failed": 2, "widened": 1},
  "violations": [
    {
      "instance": "site:S-102",
      "type": "MeetingRoom",
      "check": "required-claim",
      "arguments": ["(predicate width)"],
      "parameters": [{"name": "predicate", "type": "predicate", "values": ["width"]}],
      "declared": "registry.dfc:37:3-37:40",
      "subject": "entities/site.dfc:29:1-34:24",
      "message": "expected a claim under width on the subject, found none",
      "hint": "the type requires one of every instance; write the claim, or take the invariant off the type"
    }
  ],
  "bands": [
    {
      "instance": "site:S-107",
      "check": "claim-agrees-with-geometry",
      "arguments": ["(predicate area)", "(position position)", "(tolerance boundary-closure)", "(discrepancy area-discrepancy)"],
      "parameters": [
        {"name": "predicate", "type": "predicate", "values": ["area"]},
        {"name": "position", "type": "predicate", "values": ["position"]},
        {"name": "tolerance", "type": "tolerance", "values": ["boundary-closure"]},
        {"name": "discrepancy", "type": "tolerance", "values": ["area-discrepancy"]}
      ],
      "declared": "entities/site.dfc:198:3-202:36",
      "subject": "entities/site.dfc:185:1-202:37",
      "band": {
        "tolerance": "area-discrepancy",
        "floor": 0.05,
        "applied": 0.2739415996156845,
        "unit": "m2",
        "difference": 0.1999999999999993,
        "widened": true,
        "decisive": true,
        "terms": [
          {"source": "claim", "sigma": 0.25, "unit": "m2", "sensitivity": 1, "contribution": 0.25},
          {"source": "corners", "sigma": 0.008, "unit": "m", "sensitivity": 14, "contribution": 0.112}
        ]
      }
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `refused` | boolean | Whether the model was **not** loaded: a file could not be read, did not parse, or held something the load refuses outright. Written on every run, true and false alike. |
| `summary.checks` | integer | How many rules the filters selected. |
| `summary.runnable` | integer | How many of them would run: those whose check has an implementation and can examine the thing it is bound to. `checks` minus `runnable` is how many are bound and decide nothing. |
| `summary.ran` | integer | How many actually ran. It equals `runnable` for a run and is `0` for a `--list`, so a listing cannot be read as a run in which every check passed. |
| `summary.passed` | integer | How many of the ones that ran were satisfied. |
| `summary.failed` | integer | How many were not, which is how many rules the violations are about. |
| `summary.widened` | integer | How many of the ones that passed did so **only** because the band they were decided against is wider than the tolerance they name. It is `0` for a `--list`. |
| `violations` | array | One entry per way a rule was not satisfied, in the order the rules ran. Empty rather than null when nothing failed. |
| `violations[].instance` | string | The id of the thing that failed. |
| `violations[].type` | string, optional | The type that declared the rule. Absent for an assertion, which is declared on the thing itself. |
| `violations[].check` | string | The check name the rule names. |
| `violations[].arguments` | array, optional | The parameters it ran with, each rendered as it was written — the tolerance it was measured against among them. `parameters` is the same thing as data. |
| `violations[].parameters` | array, optional | The same parameters as data, one entry per entry of `arguments` and in the same order, so `parameters[i]` and `arguments[i]` are one parameter. Absent where `arguments` is. See [Parameters as data](#parameters-as-data). |
| `violations[].declared` | span | Where the rule is written: a registry file for an invariant, the thing itself for an assertion. |
| `violations[].subject` | span | Where what failed is written: the thing, or the part of it the check pointed at. |
| `violations[].message` | string | What was expected and what was found. |
| `violations[].hint` | string, optional | What to do about it. |
| `violations[].related` | array, optional | The other places that explain this one, each a span and a message. Where the rule was declared is not among them; `declared` is. |
| `bands` | array | One entry per comparison decided against a figure the tolerance the rule names is only the floor under, in the order the rules ran. Empty rather than null when no rule that ran widened anything. Rules that **passed** are in here as much as ones that failed. |
| `bands[].instance` | string | The id of the thing the rule ran against. |
| `bands[].type` | string, optional | The type that declared the rule. Absent for an assertion. |
| `bands[].check` | string | The check name the rule names. |
| `bands[].arguments` | array, optional | The parameters it ran with, each rendered as it was written. `parameters` is the same thing as data. |
| `bands[].parameters` | array, optional | The same parameters as data, as in `violations`. |
| `bands[].declared` | span | Where the rule is written. |
| `bands[].subject` | span | Where the thing it ran against is written. |
| `bands[].band.tolerance` | string | The name of the declared tolerance the rule was given, which is where to go to change the floor. |
| `bands[].band.floor` | number | That tolerance's value: the narrowest figure the comparison could have been decided against. |
| `bands[].band.applied` | number | The figure the difference was **actually** compared against: `floor`, or the terms combined where that is wider. |
| `bands[].band.unit` | string | What `floor`, `applied` and `difference` are in — the unit of what is compared, so the square of the frame's linear unit for an area. |
| `bands[].band.difference` | number | The magnitude of the gap the band was applied to. Unsigned: which figure is larger is the violation's to say. |
| `bands[].band.widened` | boolean | Whether `applied` is wider than `floor`, which is whether the check decided against a figure nobody wrote down. |
| `bands[].band.decisive` | boolean | Whether the widening is what decided the answer: the difference is inside `applied` and outside `floor`. This is what tells a pass that **needed** the widening from a pass within the tolerance as written. |
| `bands[].band.terms` | array, optional | The accuracies combined into `applied`, one per side of the comparison that stated one. Absent where neither did and the floor is the whole of the test. |
| `bands[].band.terms[].source` | string | Which side stated it: `claim`, `corners` — the subject's own shape — `container`, the shape it is judged against, or `transform`, the fits a subject declared in another frame than its container's was carried across to be judged. A systematic error the fits share with either shape is counted once, on the shape's side, so `transform` is what the fits add. |
| `bands[].band.terms[].sigma` | number | The one standard uncertainty that side states. |
| `bands[].band.terms[].unit` | string | What `sigma` is in, which is not always the band's unit: an area is compared in the square of a length and the corners behind it are surveyed in the length. |
| `bands[].band.terms[].sensitivity` | number | How far the compared figure moves per unit of `sigma`, which carries the term into the band's unit. `1` where the two are already the same unit; the length of the boundary where a corner displacement is carried across to the area it encloses. |
| `bands[].band.terms[].contribution` | number | `sigma × sensitivity`, in the band's unit, which is the figure combined in quadrature into `applied`. |
| `bands[].band.terms[].claims` | array, optional | The ids of the claims the term was read from, where they appear nowhere else in the answer: the transform claims a `transform` term was accumulated from, in the order the route between the two frames passes through them. Absent for every other source. |
| `chorded` | array, optional | One entry per edge a rule read as the straight line between its ends although the model states a curve on it, per rule, in the order the rules ran — or would run, under `--list`. Rules that **passed** are in here as much as ones that failed. Absent where no rule read a curve straight, which is every run over a model that claims none. |
| `chorded[].instance` | string | The id of the thing the rule is bound to. |
| `chorded[].type` | string, optional | The type that declared the rule. Absent for an assertion. |
| `chorded[].check` | string | The check name the rule names. |
| `chorded[].arguments` | array, optional | The parameters it is written with, each rendered as it was written. `parameters` is the same thing as data. |
| `chorded[].parameters` | array, optional | The same parameters as data, as in `violations`. |
| `chorded[].declared` | span | Where the rule is written, which is where to name the vocabulary that reads the curve. |
| `chorded[].subject` | span | Where the thing it is bound to is written. |
| `chorded[].edge` | string | The edge read straight: one bounding a shape the rule reads, which claims a position the rule did not read as an arc. |
| `chorded[].predicates` | array | The predicates that edge states a position under, in name order — the shape of `chorded` on `measure` and `site`, and what to name on the rule. |
| `chorded[].span` | span | Where that edge is written. |
| `drawn` | array, optional | One entry per rule that read a curve through straight segments, in the order the rules ran. Absent where no rule drew one. |
| `drawn[].instance`, `drawn[].type`, `drawn[].check`, `drawn[].arguments`, `drawn[].parameters`, `drawn[].declared`, `drawn[].subject` | | The rule, as in `bands`. |
| `drawn[].chord` | string | The name of the declared tolerance the curves were drawn to — the rule's `(chord ...)`. |
| `drawn[].value` | number | That tolerance's value. |
| `drawn[].deviation` | number | How far the worst segment of the drawing fell from the curve it stands in for: what was achieved, never more than `value`. |
| `drawn[].unit` | string | What `value` and `deviation` are in, the linear unit of the frame. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

**Every violation and every chorded edge is in [`diagnostics`](#diagnostics) as well**, the
violation as an error and the chorded edge as a warning, after the diagnostics the load
reported. `violations[]` and `chorded[]` are `check`'s answer, shaped for it — which rule,
declared where, bound to what — and keep that shape; `diagnostics` is what the run rendered on
stderr, in the order it rendered it, which for `check` is the load's and then its own.

The counts are of **rules**, not of violations. One loop that does not close and one that
closes the wrong way are two ways of failing one check, and a summary counting them as two
failures would say the model breaks two rules.

**`refused` is read before the summary is believed.** A run over a model that did not load
selects no rule, runs none and finds no violation — `{"summary": {"checks": 0, "ran": 0,
…}, "violations": []}`, which is byte for byte what a model with nothing wrong with it
reports. Only the exit code told the two apart, and stdout is what a caller is told to
parse. `refused: true` is that distinction on the stream the caller is reading: the
emptiness is the absence of a run, not the absence of a problem. It is written on every run
rather than only on the refused ones, so a caller reads it unconditionally instead of
treating a missing key as an answer.

The refusal *is* still reported, rather than stdout being left empty, because a gate wants
both halves: `dfcad check` reports what it managed to bind even over a model it could not
run, and `--list` over such a model is how the two reasons a rule decides nothing are read.
An `--entity-format` this engine does not implement is the other case and is not this one —
there the run stops before anything is read, and stdout is empty because there is nothing to
report.

`--list` adds `checks` beside an empty `violations` — nothing ran, so nothing failed. It is
one entry per rule the filters selected, in the order it would run in, each carrying
`subject`, `form` — `node`, `vertex`, `edge` or `loop` — `rule`, which is `invariant` or
`assertion`, the `type` that declared it where one did, the `check` name, its `arguments`
and the same parameters as data under `parameters`, its `declared` span, and two booleans: `runs`, which says whether running it would decide
anything, and `applicable`, which says whether the check can examine the thing it is bound
to.

`runs` is false for two different reasons and `applicable` is which of them. A check that
declares itself and has no implementation is the engine's to write; a check that cannot
examine the thing it was written on is a line in the model. The second appears only in a
model the load already refused, because such an assertion is a load error rather than a rule
that quietly never fires — and reporting it as unimplemented would send its author to the
wrong repository.

A check that declares itself and has no implementation is bound, listed and counted apart
from the ones that ran. "This rule holds" and "nothing has been written to decide whether it
holds" are different answers, and a summary that folded the second into the first would report
a model sound because nothing looked at it.

**Every check this engine registers has an implementation**, and a test over the registry
keeps it that way — a check the documentation describes as deciding, shipped deciding nothing,
is a gate that exits `0` over a model breaking it. So of the two reasons, only the second is
one a listing from this engine shows, and only over a model the load refused: over a model
that loaded, `runnable` equals `checks`. The field and the first reason stay in the contract
because they are how a registry which ever broke that rule would say so, rather than passing
quietly.

**`bands` is what makes a passing run falsifiable.** Some checks treat the tolerance they are
declared with as a floor rather than as the whole test: two figures that differ by less than
their combined uncertainty do not disagree, so the band widens to what the evidence can
actually tell apart. That is right — a claim cannot be held to a precision the geometry does
not have — and it means the number the registry states is not the number the check applied.
The gap is not small. A 0.5 usft² discrepancy declared over a 926 usft² region with a 137 usft
perimeter is applied as about 8, because the perimeter multiplies the corners' survey accuracy
sixteen-fold. A rule written as "the boundary agrees with the appraisal to within half a square
foot" is then not testing that, and without `bands` the run that passed it reads exactly like
one that did.

So every such comparison reports the band, on passes as much as on failures — a failure has a
message to carry it, and a pass has nothing else at all. `decisive` is the field a gate reads:
it is true only where the difference is outside the declared tolerance and inside the widened
band, which is a criterion the survey is not accurate enough to decide. `summary.widened`
counts those, and `--format human` says so on stderr beside the summary that counts them as
passes.

The three checks that report a band are `claim-agrees-with-geometry`, `contained-areas-sum`
and `sits-inside`. A check that decides against the tolerance it was given reports none: there
is nothing to disclose, because the declared figure is the applied one and it is already
written in the rule. Neither does a comparison a check declined to make — a room drawn and not
yet measured discloses nothing, because nothing was tested.

**`chorded` is what keeps a pass over a curve nobody read from reading as a real one.** Five
checks read a shape — `claim-agrees-with-geometry`, `contained-areas-do-not-overlap`,
`contained-areas-sum`, `sits-inside` and `stays-clear-of-zone` — and each reads an edge that
bends as the arc it is, where the rule names the vocabulary the arc is written in, and as the
straight line between its ends where it does not. All five take the same three optional
parameters, none of them defaulted and none of them a flag, because a rule is one line of the
model and its answer must not change with how it was invoked:

| Parameter | Meaning |
|-----------|---------|
| `(arc-centre <predicate>)` | The predicate the centre of the arc an edge bends along is claimed under. |
| `(arc-through <predicate>)` | The predicate a point on the arc is claimed under, which says which of the two arcs between the ends is meant. Named together with `arc-centre`; a rule naming one of the two fails, saying so. |
| `(chord <tolerance>)` | The tolerance a curve is drawn to where the answer needs straight segments: the overlay `sits-inside`, `stays-clear-of-zone` and the two `contained-areas` checks decide by, and the nesting of rings one of which bends. A rule that reads an arc by an overlay and names no chord fails, naming the edge. |

Which way a check errs over a chord depends on which side of it the question sits, so none
can be trusted to err safely. A shed standing in the bow of a curved easement passes
`stays-clear-of-zone` read over the chord and fails it read over the arc; the same easement's
plat area fails `claim-agrees-with-geometry` over the chord and agrees over the arc. So the
engine never picks: a rule that names the vocabulary reads the curve, and a rule that does
not is named in `chorded`, one entry per edge it read straight, each also a warning on stderr
naming the rule and what to write on it. A **violation** of such a rule says in its own
message that the figure it compared was the chord's, and points at the edge. A rule that
names the vocabulary and meets an edge that does not claim both halves of it reads that edge
straight too, and is in `chorded` the same way.

Where a rule drew a curve to its chord, `drawn` says what to and the deviation that achieved,
because the answer is then about the drawing and no closer to the arc than that.
`claim-agrees-with-geometry` reads an arc's area and length from its parameterisation and draws
nothing, so it appears there only where it had to nest rings.

`--list` says of each rule, before anything runs, how it will read a curve: `curves` is `arc`
where the rule names the vocabulary and every curved edge of its shapes states both halves,
`chord` where at least one will be read straight — listed by id under `chorded` on the entry,
and in the top-level `chorded` as on a run — and absent where the rule reads no curve at all.
A model that claims no curve carries none of `chorded`, `drawn` or `curves`, and its output is
byte for byte what it was before a rule could read one.

Rules run in a deterministic order and are reported in it: every invariant, node by node in
the order the model was read, and then every assertion, thing by thing. Two runs over one
model produce byte-identical stdout, bands included.

| Code | When |
|------|------|
| `0` | Every rule that ran was satisfied — including a model that states no rule at all, which runs nothing and succeeds. |
| `1` | A rule was not satisfied. Every violation is in the result. |
| `2` | The model could not be read: a file did not parse, the root holds no model at all, or `--entity-format` named a format this engine does not implement. It outranks a rule that failed, because a gate reporting on half a model is answering a question nobody asked — and it is what keeps a `--root` with a character wrong from passing by having nothing in it. `refused` says which of the first two it was; the third writes no object at all. |
| `3` | The invocation was wrong: an argument the command does not take, or a filter naming something no model holds. |

**How long the run took is not on stdout.** The same input has to produce the same bytes
there, and a duration is the one thing that never does. It is written to stderr instead: with
the summary under `--format human`, and on its own under `-v` in any format, so that a check
set becoming slow is visible without stdout ceasing to be diffable.

Every violation is also rendered to stderr as a diagnostic, on every run and in every format,
because it is a problem in something somebody wrote. The struct above is the machine form of
the same finding, and neither is produced by parsing the other.

#### Parameters as data

Every entry naming a rule — under `checks`, `violations`, `bands`, `chorded` and `drawn` —
carries its parameters twice. `arguments` is each parameter as it was written,
`"(tolerance boundary-closure)"`; `parameters` is the same parameter as data, so a caller
wanting the tolerance or the predicate a rule runs with reads it rather than parsing an
s-expression back out of a string:

```json
"parameters": [
  {"name": "tolerance", "type": "tolerance", "values": ["boundary-closure"]},
  {"name": "position", "type": "predicate", "values": ["position"]}
]
```

| Field | Type | Meaning |
|-------|------|---------|
| `name` | string | The tag the parameter is written with. |
| `type` | string, optional | What the check declares the parameter takes: one of `id`, `real`, `string`, `boolean`, `kind`, `geometry`, `type`, `predicate`, `frame` or `tolerance`. It is what says `height` in `(predicate height)` is the name of a predicate rather than a string. Absent where the check declares no parameter by that name, which only a model the load refused can hold. |
| `values` | array | The values written for it, one element per value and in the order written — always an array, whether the check takes one value or several. A `real` is a number, a `boolean` a boolean, and every other type a string holding the name, the id or the text as written, unquoted. A value that is not an atom of the declared type — again, only in a model the load refused — is `null`. |

A parameter the check declares as taking one or more values may be written as a sequence after
its tag or as one parenthesised list — `(kinds Space Element)` and `(kinds (Space Element))`
— and both give the same `values`. `arguments` renders the second `(kinds …)`, which is the
lossy rendering `parameters` exists to replace; `arguments` stays byte for byte as it was.

Nothing a value names is resolved: a tolerance is its name here, and the figure it stands for
is `bands[].band.floor`'s to report.

#### `claim-agrees-with-geometry`

The check registry is closed and compiled into the engine, and `dfcad check --list` is what
prints the whole of it. One member of it needs saying here, because reading its violation
means knowing what it compared and what band it compared against.

It reports a **measurement written down which no longer matches the shape it describes** — an
`area` claim on a node whose boundary computes to something else, a `length` claim on a run of
wall which has moved. It takes four parameters, all required and none defaulted:
`(predicate <name>)`, the predicate the claimed measurement is written under; `(position
<name>)`, the predicate a corner's position is claimed under; `(tolerance <name>)`, how close
two corners are one corner; and `(discrepancy <name>)`, how far the two may differ. It is
written on a node whose geometry is `area` or `surface`, where the comparison is of areas, or
`line`, where it is of lengths.

It is also written **on an edge**, where the comparison is of the claimed length against the
distance between the two corners the edge runs between. That is the most directly checkable
measurement the format can express, because both ends are already in the model, and it is the
one no node-bound rule reaches: an edge belongs to no loop unless something says so, and a
span written on a loose edge has no boundary for a rule about an outline to be about. A
schedule of recorded spans becomes checkable by writing each of them as a claim on the edge it
was measured along.

`discrepancy` is a **floor and not the whole test.** Two figures which differ by less than
their combined uncertainty do not disagree, so the band is the wider of the declared
discrepancy and the two figures' combined one-sigma uncertainty: the claim's own accuracy, and
the accuracy the corners' position claims put behind the shape. Those two are added in
quadrature, as separate measurements of one quantity. For an area the corners' budget is a
distance and the figure is an area, so it is carried across by the length of the boundary — a
boundary of length P displaced by δ moves the area it encloses by about P·δ, which is a
first-order sensitivity and is stated as one. Where a side states no accuracy it narrows
nothing and the declared discrepancy decides, because an unstated accuracy is unknown rather
than zero.

**The band it applied is in `bands`, on a pass as much as on a failure**, with each accuracy
that widened it and the sensitivity that carried it into the unit compared. The violation's
`hint` names it too, in the same sentence as the tolerance under it, because a reader told only
the declared figure would go and tighten a number that decided nothing.

The discrepancy in the message is **signed**: a claim larger than its shape and one smaller
are two different mistakes, and the message says which way it runs. `subject` is the span of
the claim that disagrees, not of the node, and `related` carries the geometry it was compared
against — the boundary, for a claim on a node, and both corners for a span on an edge. Either
the number or the geometry may be the one to change, and on an edge which of the two ends
moved is what a reader goes on to find out.

Two states are **not** violations and report nothing. A subject carrying the claim and no
shape, and one carrying a shape and no claim under the named predicate, have nothing to
compare: a room drawn and not yet measured, or measured and not yet drawn, is an ordinary
state of a model being written. An edge whose ends nobody has surveyed under the `(position
<name>)` predicate is the second of those seen from the geometry's side — the number is
there and what is missing is somewhere to measure it against, and a span nothing can measure
is not a span which disagrees. A `deprecated` claim is never compared either — it is
retracted rather than out-ranked, and a retracted number is not a disagreement.

### `review`

The changes in this revision which need an explanation. Every rule `check` runs constrains
one revision; these need two, and the second one is the merge base. It takes no arguments
and four flags.

| Flag | Meaning |
|------|---------|
| `--against <ref>` | The branch this revision is being merged into. The merge base of it and `HEAD` is what the model is compared against. Default `origin/HEAD`. |
| `--base-root <dir>` | Compare against a model in this directory instead of against a revision. A relative path is resolved against `--root`. Nothing is attributed to a commit under it. |
| `--policy <check>=<ruling>` | What one kind of finding means: `failure`, `warning` or `ignored`. Repeatable. |
| `--annotate <path>` | Append a Markdown summary of the findings to `path`, which is what `$GITHUB_STEP_SUMMARY` makes a reviewer see. `-` writes it to stderr. |

The comparison is against the **merge base** and not against the tip of `--against`: those
two differ the moment anything else lands there, and a review against the tip would report
everybody else's work as part of this change.

The three checks, and what each is called in `--policy`:

| Check | Reports | Default |
|-------|---------|---------|
| `boundary-moved-without-claim` | A physical boundary moved with no new measurement to account for it: a corner's claim rewritten in place, or a boundary drawn round different corners. | `warning` |
| `claim-deprecated-without-replacement` | A claim retracted with nothing standing in its place — a replacement this revision does not hold, or a retraction which left nothing at all asserted about a subject and a predicate. | `failure` |
| `id-disappeared-without-supersession` | An id the merge base held and this revision does not, with every reference which now names nothing. | `failure` |

A boundary which moved warns because it is the one of the three which is routinely
legitimate: a corner measured again genuinely moves, and the check cannot see the survey
which justified it. The other two are breaches of a rule the model is built on, and each
takes references or evidence with it.

A policy is what makes this usable rather than something to route around. A finding ruled
`ignored` **is still in the result** — a check silently switched off is one nobody remembers
is off — and is reported nowhere else: not on stderr, not in the annotation, and not in the
exit code.

```json
{
  "version": 2,
  "command": "review",
  "comparison": {
    "against": "main",
    "base": "8f1c0a2b6d4e79f3b5c8a1d0e2f4a6b8c0d2e4f6",
    "head": "5f2b8c1d9e3a47b6c0d1e2f3a4b5c6d7e8f90123",
    "files": 2
  },
  "policy": {
    "boundary-moved-without-claim": "warning",
    "claim-deprecated-without-replacement": "failure",
    "id-disappeared-without-supersession": "failure"
  },
  "summary": {"findings": 2, "failures": 1, "warnings": 1, "ignored": 0},
  "findings": [
    {
      "kind": "boundary-moved-without-claim",
      "ruling": "warning",
      "subject": "site:S-101",
      "side": "head",
      "span": "entities/geometry.dfc:15:5-15:28",
      "commit": {
        "sha": "5f2b8c1d9e3a47b6c0d1e2f3a4b5c6d7e8f90123",
        "summary": "story(site): widen Meeting Room A",
        "author": "A Surveyor",
        "date": "2026-06-01T09:30:00Z"
      },
      "message": "the boundary of site:S-101 moved: the position of geom:V-02 was rewritten from (4.0 0.0 0.0) m to (4.6 0.0 0.0) m inside the claim which already stated it, so nothing new was measured",
      "hint": "a corner which moved was measured again, so write the measurement: `dfcad supersede geom:V-02 position ...` keeps what the first survey said beside what the second one found",
      "related": [
        {
          "span": "entities/geometry.dfc:12:3-18:30",
          "message": "the claim this rewrote, as the merge base holds it"
        }
      ]
    },
    {
      "kind": "id-disappeared-without-supersession",
      "ruling": "failure",
      "subject": "geom:V-04",
      "side": "base",
      "span": "entities/geometry.dfc:28:1-35:24",
      "commit": {
        "sha": "5f2b8c1d9e3a47b6c0d1e2f3a4b5c6d7e8f90123",
        "summary": "story(site): widen Meeting Room A",
        "author": "A Surveyor",
        "date": "2026-06-01T09:30:00Z"
      },
      "message": "geom:V-04 is gone from this revision: the vertex was removed rather than retired, and 2 references still name it",
      "hint": "a thing which stopped existing keeps its id: `dfcad retire geom:V-04 --reason ...` records what happened and leaves every reference resolving",
      "dangling": [
        {
          "from": "geom:E-03",
          "relation": "vertices",
          "span": "entities/geometry.dfc:41:1-41:92"
        }
      ]
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `comparison.against` | string, optional | The revision the merge base was taken with, as it was written. Absent for a `--base-root` run. |
| `comparison.base` | string | The merge base: the full object name of the commit, or the directory a `--base-root` run read instead. |
| `comparison.head` | string, optional | The full object name of the revision under review. Absent for a `--base-root` run. |
| `comparison.files` | integer | How many files the range between them touched, which is what a finding is attributed through. Zero for a comparison with no history to read. |
| `policy` | object | The ruling each check ran under, by its name. **Every** check is here, not only the ones a flag named: what a green run did about the checks it did not report is what a reader of a green run needs to know. |
| `summary.findings` | integer | How many findings there were, ignored ones included. |
| `summary.failures` | integer | How many the policy ruled a failure, which is what decides the exit code. |
| `summary.warnings` | integer | How many it ruled a warning. |
| `summary.ignored` | integer | How many it acknowledged, which are reported here and nowhere else. |
| `findings` | array | One entry per change which needs an explanation. Empty rather than null when there are none. |
| `findings[].kind` | string | Which check reported it, spelled as `--policy` spells it. |
| `findings[].ruling` | string | What the policy said to do about it: `failure`, `warning` or `ignored`. |
| `findings[].subject` | string | The id of the thing it is about: the boundary which moved, the subject of the claim which was retracted, the id which disappeared. |
| `findings[].side` | string | Which revision `span` points into: `head` for a change to something this revision still holds, `base` for something it does not. |
| `findings[].span` | span | Where the change is. A reader jumping to it needs `side` to know which revision the line is in. |
| `findings[].commit` | object, optional | The commit which introduced the change: `sha`, `summary`, `author` and `date`. Absent for a comparison with no history. |
| `findings[].message` | string | What changed and what would have accounted for it. |
| `findings[].hint` | string, optional | What to do about it, which is usually the command which records the change properly. |
| `findings[].related` | array, optional | The other places which explain this one, each a span and a message. |
| `findings[].dangling` | array, optional | The references this revision still makes to an id it no longer holds, each a `from`, a `relation` and a `span`. Only `id-disappeared-without-supersession` fills it in. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

**Every finding whose ruling is `failure` or `warning` is in [`diagnostics`](#diagnostics) as
well**, after the diagnostics either revision's load reported. A finding ruled `ignored` is not
rendered and is not there. `findings[]` is `review`'s answer and keeps its shape; `diagnostics`
is what the run rendered on stderr.

Findings are ordered by check, in the order the table above lists them, then by subject, then
by position. Two runs over the same pair of revisions produce byte-identical stdout, so a
diff between two runs is about what changed in the branch.

| Code | When |
|------|------|
| `0` | Nothing the policy ruled a failure. A revision which changed nothing suspicious, and one whose every finding was warned about or acknowledged, both land here. |
| `1` | At least one finding the policy ruled a failure. Every finding is in the result. |
| `2` | A revision could not be read: the model root is not inside a git working tree, the branch does not exist, the checkout is too shallow to reach the merge base, or the merge base itself does not load. A review needs both revisions, and half a comparison would report every id in the half it did not read as an id which disappeared. Where the head or the base did not load, stdout carries [the refusal](#the-refusal); where a revision could not be reached at all, nothing. |
| `3` | The invocation was wrong: an argument the command does not take, or a `--policy` naming no check or no ruling. |

**A shallow checkout is refused rather than answered from.** Git reports a merge base at the
point a shallow clone's history was cut off, which is a commit the two revisions never
shared, so the review which followed would attribute the whole of the branch's ancestry to
this change. The message names what to fetch — `git fetch --unshallow`, or `fetch-depth: 0`
on `actions/checkout`, which the containerized pipeline requires anyway.

Every finding the policy did not ignore is also rendered to stderr as a diagnostic, on every
run and in every format, because it is a problem in a change somebody made. `--annotate`
writes the same findings as Markdown, for `$GITHUB_STEP_SUMMARY`, which is where a reviewer
sees them. All three are built from the fields above, and none is produced by parsing
another.

### The shape every write command reports

Adding a node, retiring one, adding a claim, correcting one, authoring geometry and
applying a batch of edits are all commands that change the tree, and they all change it the
same way: load the whole model, apply the change in memory, interpret the result as though
it had already been written, and only then replace the files
([0015](./decisions/0015-the-cli-is-the-primary-write-path.md),
[0016](./decisions/0016-writes-are-all-or-nothing.md)). What they report is therefore the
same too, and it is documented once here rather than repeated per command. A write command
adds fields describing what it was asked to do; the ones below mean the same thing in all of
them.

Every write command takes these flags beyond the global ones:

| Flag | Default | Meaning |
|------|---------|---------|
| `--dry-run` | off | Perform every step of the change, including validation, and write nothing. The result object says what would have changed, and carries the unified diff of each file. |
| `--file <path>` | routed | Write into this file rather than the one the routing rules choose. A path relative to the model root, ending in `.dfc`. |

A command that adds something to the model decides where it goes before it changes anything,
by the routing rules of [7.7 of the specification](../SPEC.md#77-route), and reports that
decision. `dfcad route` is the same decision asked on its own; see its payload above for what
the decision looks like and for what happens when the rules do not place a node.

```json
{
  "version": 2,
  "command": "add-node",
  "dryRun": false,
  "files": [
    {
      "path": "entities/level-1.dfc",
      "status": "rewritten",
      "effects": [
        {"op": "created", "tag": "node", "id": "site:S-103"}
      ],
      "diff": "--- entities/level-1.dfc.orig\n+++ entities/level-1.dfc\n@@ -7,3 +7,4 @@\n..."
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `dryRun` | boolean | Whether the change was validated and described without being written. |
| `files` | array | One entry per file the change touched, in the lexical order of their paths, which is the order a walk of the model reaches them. Empty rather than null when the change touched no file. |
| `files[].path` | string | The file, as the walk reached it or as the change named it. |
| `files[].status` | string | One of `created`, `rewritten`, `unchanged` — what happened to the file, or, on a dry run, what would have. |
| `files[].effects` | array, optional | What the change did to the *model* in this file, in the order the mutations were applied. |
| `files[].effects[].op` | string | One of `created`, `modified`, `retired`. |
| `files[].effects[].tag` | string | The form it was written as — `node`, `vertex`, `edge`, `loop`, `type` — so an effect says which family it is about without the reader resolving the id. |
| `files[].effects[].id` | string, optional | The thing it was about. Absent for a form carrying no id, which is every registry entry other than a frame. |
| `files[].effects[].name` | string, optional | The plain symbol a registry entry is declared under, which is what a `type` effect is about. Absent for every form that names itself with an id instead. It is a field of its own rather than a second spelling of `id`: an id is namespaced, is never reissued and resolves to a node, and a registry name is none of those. |
| `files[].diff` | string, optional | The unified diff from what was on disk to what was written. Absent where the two are the same. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

**A change carries what the model it produced rendered.** A change which is written can still
render warnings — about the model the change produced, or the one it was made to — and those
are in [`diagnostics`](#diagnostics). They carry no `ids` and no `nodes`: a write reads the model
through a transaction and never holds it, or the model it would produce, as a graph, so there is
nothing to compute them against — a refused change's spans are in a model the run never held. A change the model refuses writes nothing to stdout, as
below, so its diagnostics are on stderr only.

Statuses:

| Status | Meaning |
|--------|---------|
| `created` | The model held no such file before the change. |
| `rewritten` | An existing file was replaced by its new contents, in canonical form. |
| `unchanged` | A mutation touched the file and its canonical printing turned out to be exactly what was already on disk. Nothing was written for it. |

Files nothing touched are not listed and are not rewritten, whether or not they are in
canonical form. A write command is not a formatter: rewriting a file nobody asked about
would put somebody else's reformatting in the author's diff. Files that *are* written are
always written in canonical form, so what a write command leaves behind already satisfies
`fmt --check`.

Exit codes:

| Code | When |
|------|------|
| `0` | The change was written, or, under `--dry-run`, would have been. |
| `2` | The change was refused because the resulting model would not load or the tree did not load to begin with — stdout carries [the refusal](#the-refusal) — or the model root is held by another transaction or a file could not be written, which write nothing to stdout. |
| `3` | The invocation itself was wrong. |

A refused change writes nothing at all, and its diagnostics are the ones a load of the
result would have raised — every independent problem, each with its position, rather than
the first. Because the model is unchanged, the correct response to a refusal is to fix the
command and reissue it: there is no partial state to inspect and nothing to reconcile.

A refused change writes no result to **stdout**: an object describing a change that did not
happen reads exactly like one describing a change that did. It writes [the refusal](#the-refusal)
instead — the envelope, `"refused": true` and the diagnostics that refused it, with no
`dryRun` and no `files` — so a caller reads why from stdout. A model root held by another
transaction and a file that could not be written are errors rather than refusals, and write
nothing.

### `add-node`

A new semantic node. It takes the id it will be written with, and the axes it declares.

| Flag | Meaning |
|------|---------|
| `--kind <kind>` | The kind it declares. |
| `--type <name>` | The type it declares. |
| `--geometry <form>` | The geometry form it declares. Omitted for a node with no geometry, which its type has to permit. |
| `--frame <id>` | The coordinate frame it is expressed in. |
| `--label "<text>"` | Its display text, which nothing resolves through. |
| `--file <path>` | Write it here instead, overriding the routing rules. |

Every axis is checked against the registry before anything is written. An unregistered id
namespace, a kind or a geometry form that is not one, a type nothing declares, a type that
does not permit the kind or the geometry form written here, and a frame the registry does
not declare are each a **usage error** — exit `3`, with nothing on stdout — naming what was
asked for and what would have been permitted.

The axes are checked before the routing rules are consulted, because the rules match on
three of them: a misspelled kind reported as a node no rule places is an answer about the
wrong mistake.

An id something already holds is refused, naming where that thing is defined. **A retired id
is refused the same way.** Retiring says the thing stopped existing, not that its name came
free, and an id is never issued twice
([0002](./decisions/0002-immutable-id-mutable-label.md)).

```json
{
  "version": 2,
  "command": "add-node",
  "dryRun": false,
  "files": [
    {
      "path": "entities/site.dfc",
      "status": "rewritten",
      "effects": [{"op": "created", "tag": "node", "id": "site:S-104"}],
      "diff": "--- entities/site.dfc.orig\n+++ entities/site.dfc\n@@ -7,3 +7,4 @@\n..."
    }
  ]
}
```

### `add-vertex`

A new corner. It takes the id it will be written with, the frame it is in, and — where the
position is already known — the claim saying where it is.

| Flag | Meaning |
|------|---------|
| `--frame <id>` | The coordinate frame it is expressed in. Required: a geometric node is always in exactly one. |
| `--label "<text>"` | Its display text, which nothing resolves through. |
| `--predicate <name>` | The predicate its position is claimed under. The claim flags below are read only when it is given. |
| `--file <path>` | Write it here instead, overriding the routing rules. |

A vertex carries no coordinate of its own. **Where it is, is a claim like any other**, held
to the same predicate validation, the same accuracy rules and the same resolution — so the
claim flags of `add-claim` are the claim flags here, and two surveys of one corner are two
claims rather than a number somebody overwrote. Leave `--predicate` out for a corner that
has been named and not yet surveyed: its position is then unknown rather than zero.

A geometric node declares neither a kind nor a type, so the one criterion a routing rule can
match it on is the namespace of its id. A rule written with a kind or a type never places
one, which is what keeps the rules that file semantic nodes from filing geometry as a side
effect.

The payload is the write payload above and nothing more.

### `add-edge`

A connection between two corners. It takes the id it will be written with and the two
vertices it runs between.

| Flag | Meaning |
|------|---------|
| `--frame <id>` | The coordinate frame it is expressed in. Required. |
| `--start <vertex-id>` | The vertex it runs from. Required. |
| `--end <vertex-id>` | The vertex it runs to. Required. |
| `--backed-by <id>` | A semantic node that physically realises it. Repeatable. |
| `--label "<text>"` | Its display text. |
| `--file <path>` | Write it here instead, overriding the routing rules. |

Both endpoints are resolved before anything is written, against the model and against what
the same change has already added. An id naming nothing, an id naming something that is not
a vertex, and one vertex written at both ends are each a **usage error** naming what was
reached.

Naming the ends by id rather than by coordinate is what makes the **shared-edge case**
ordinary: two regions either side of a partition name one edge, so the second of them is
written by naming the vertices the first already has. The order of the pair is significant
and is never sorted — an edge is directed, and the region on the other side traverses it the
other way.

Whether an edge is a physical boundary or a virtual one is **computed** from `--backed-by`
rather than written, so adding the wall later flips the answer with no other edit
([0009](./decisions/0009-derived-values-are-never-written-back.md)).

The payload is the write payload above and nothing more.

### `add-loop`

An ordered ring of edges. It takes the id it will be written with and the edges, in the
order the ring is walked.

| Flag | Meaning |
|------|---------|
| `--frame <id>` | The coordinate frame it is expressed in. Required. |
| `--edge <edge-id>` | An edge of the ring. Repeat once per edge, in traversal order. |
| `--label "<text>"` | Its display text. |
| `--file <path>` | Write it here instead, overriding the routing rules. |

The order is the data: it is preserved exactly as written and is never sorted. Every edge id
is resolved before anything is written; whether the ring closes is judged when the model the
change produces is loaded, and a change that would produce a model that does not load is
refused.

The payload is the write payload above and nothing more.

### `scaffold-loop`

A room's corners, walls and outline, from an ordered coordinate list, in one change.

| Flag | Meaning |
|------|---------|
| `--corner "<x> <y> …"` | One corner, in the shape the position predicate declares. Repeat once per corner, in order, naming the first corner again at the end. |
| `--namespace <name>` | The declared id namespace the new nodes are minted in. Required. |
| `--predicate <name>` | The predicate a corner's position is claimed under. Required. |
| `--tolerance <name>` | The declared tolerance two corners are judged to be one point by, which is also what says the list closed. Required. |
| `--frame <id>` | The coordinate frame the corners are expressed in. Required. |
| `--no-snap` | Write a new vertex at every corner, even where one is already there. |
| `--label "<text>"` | The loop's display text. |
| `--file <path>` | Write everything here instead, overriding the routing rules. |
| `--bounds <node-id>` | The semantic node the loop bounds. The `boundary` reference is written on it in the same change, and it is the same child `relate --boundary` writes. |
| `--vertex-mark <mark>` | What the minted vertex ids are named after. |
| `--edge-mark <mark>` | What the minted edge ids are named after. |
| `--loop-mark <mark>` | What the minted loop id is named after. |

The evidence every position claim carries is `--source`, `--method`, `--accuracy` and
`--date`, and they mean what they mean for `add-claim`. `--value` and `--id` are not read: a
corner's value is the corner, and every claim a scaffold writes is one of many rather than
one somebody named. `--unit` is the unit the corners are written in and defaults to the one
the position predicate declares, because a corner is a coordinate in a frame rather than a
value somebody chose a unit for — a unit written and disagreeing with the declaration is
refused exactly as it always was.

Ids are minted as `<namespace>:<mark>-<n>` — the namespace, the mark, and the lowest ordinal
nothing in the model already holds. The mark is the tag of the form being written where the
invocation names none, so `geom:vertex-1` by default; the three flags above are what put a
generated batch into a consuming repository's own scheme rather than rewriting every minted
id afterwards. It is a name and not a schema, and nothing is inferred back out of one
([0002](./decisions/0002-immutable-id-mutable-label.md)).

**The list is authored closed.** Its last corner names its first again, and a list that does
not return to where it started is a **usage error** naming the gap and its size. Closing one
silently would leave the tool unable to tell an outline somebody finished from one they
stopped typing halfway through, and the wall it invented would appear in no diagnostic
anywhere.

**A corner within the tolerance of a vertex the model already holds reuses that vertex**, and
the edge between two reused corners is reused too. That is what makes a partition one node
named by both rooms rather than two that can drift apart, and a duplicate vertex a millimetre
away is exactly the sliver a shared topology exists to prevent. `--no-snap` writes the
duplicate anyway and still reports the coincidence.

Two corners of one list at the same point are refused: either a coordinate was typed twice
or the outline doubles back, and a ring visits each of its corners once. That holds under
`--no-snap` too — switching snapping off says to write a vertex where one already is, not
that a ring may visit a corner twice — and it holds for two corners far enough apart to be
corners that both land on one vertex the model already holds.

A predicate the registry does not declare is refused before any corner is read, naming the
predicates there are: which shape a position takes is what the declaration says, so there is
nothing to read a corner against until it is known.

```json
{
  "version": 2,
  "command": "scaffold-loop",
  "dryRun": false,
  "files": [
    {
      "path": "entities/geometry.dfc",
      "status": "rewritten",
      "effects": [
        {"op": "created", "tag": "vertex", "id": "geom:vertex-1"},
        {"op": "created", "tag": "vertex", "id": "geom:vertex-2"},
        {"op": "created", "tag": "edge", "id": "geom:edge-1"},
        {"op": "created", "tag": "edge", "id": "geom:edge-2"},
        {"op": "created", "tag": "edge", "id": "geom:edge-3"},
        {"op": "created", "tag": "loop", "id": "geom:loop-1"}
      ],
      "diff": "--- entities/geometry.dfc.orig\n+++ entities/geometry.dfc\n@@ -68,3 +68,40 @@\n..."
    }
  ],
  "loop": "geom:loop-1",
  "vertices": ["geom:V-04", "geom:V-03", "geom:vertex-1", "geom:vertex-2"],
  "created": ["geom:vertex-1", "geom:vertex-2"],
  "edges": ["geom:E-03", "geom:edge-1", "geom:edge-2", "geom:edge-3"],
  "reused": ["geom:E-03"],
  "snaps": [
    {"corner": 1, "vertex": "geom:V-04", "distance": 0.0, "unit": "m", "reused": true},
    {"corner": 2, "vertex": "geom:V-03", "distance": 0.0, "unit": "m", "reused": true}
  ],
  "tolerance": {"name": "boundary-closure", "value": 0.005, "unit": "m"},
  "notices": []
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `loop` | string | The loop that was written. |
| `bounds` | string | The node the loop was written on as a boundary. Absent for a scaffold that bound nothing. |
| `vertices` | array | The vertex each corner is at, in corner order, with the closing corner left out — it is the first corner written again. |
| `created` | array | The vertices that were minted, in the order they were. A corner that reused one is not here and is in `snaps` instead. |
| `edges` | array | The ring, in traversal order. |
| `reused` | array | The edges of that ring the model already held, in the order the traversal reaches them. Empty rather than null when none was. |
| `snaps` | array | Every corner that landed on a vertex the model already held, in corner order. Empty rather than null when none did. |
| `snaps[].corner` | number | The corner's place in the list, counted from one. |
| `snaps[].vertex` | string | The vertex it landed on. |
| `snaps[].distance` | number | How far it was from that vertex. |
| `snaps[].unit` | string | The unit that distance is in, which is the frame's. |
| `snaps[].reused` | boolean | Whether the vertex was used rather than a second one written at the same point. False exactly when snapping was switched off, which is the case worth looking at. |
| `tolerance` | object | The declared tolerance coincidence and closure were judged against, with its name, its magnitude and its unit. |
| `notices` | array | What the change had to say about the model it produced, in the shape the claim commands report a notice in. |

The tolerance travels with the answer because the answer depends on it: "these two corners
are one point" is a fact about a stated tolerance and not about the corners alone
([0012](./decisions/0012-tolerances-are-registry-data.md)).

Every snap is also written to stderr, on every run and in every format — a reuse is the one
thing about a scaffold that is surprising when it happens and worse when it does not, and a
duplicate written under `--no-snap` is a warning whether or not anybody asked to see the
result.

Under `--dry-run` every field above is what it would have been, which is the whole point of
running one first: the ids, the reuses and the tolerance that decided them are what an author
is checking before committing to them.

To ask which vertex a single point lands on — without a closed list of four corners, the
evidence flags or a route — use [`list-geometry --near`](#vertices-at-a-point). It applies
this command's rule through the same function, so it lists the vertex a corner here would
snap to, with its distance.

### `relate`

What a node is inside, grouped with and bounded by. It takes the node's id and reports the
write payload above with nothing added to it.

| Flag | Meaning |
|------|---------|
| `--within <node-id>` | The node that strictly contains this one. |
| `--member-of <id>` | A zone it is a member of. Repeat for more than one. |
| `--boundary <loop-id>` | A loop that bounds it. Repeat for more than one. |

At least one of the three is required, and a relation that relates the node to nothing is a
**usage error** answered before the model is read: it is wrong whatever the tree holds.

The three are different relations and are never collapsed into one. Containment is physical
enclosure, nests strictly and is at most one, so naming a parent replaces whatever parent was
written before rather than being written beside it — two of them is a node claiming two
parents, which is a model that does not load. Membership is arbitrary grouping and is many to
many, so naming a zone adds it. A boundary leaves the semantic family altogether and names a
loop, and is added the same way ([0001](./decisions/0001-two-node-families.md)).

**Nothing is resolved here.** A parent that does not exist, a parent the hierarchy does not
permit, a `--member-of` naming something that is not a Zone and a `--boundary` naming
something that is not a loop are each refused when the model this would produce is
interpreted — so stdout carries [the refusal](#the-refusal), its diagnostics are the whole of
the answer, and the exit code is the load failure one. They are the diagnostics a load of the
result would have raised, which are the same ones the same mistake gets when it is typed
into a file by hand.

This is the other half of `add-node`, which writes a node's own axes and none of its
references: a node is added and then related, so that the refusal to place it and the
refusal to relate it are two answers rather than one compound one.

### `classify-type`

How a scheme outside this model names a declared type. It takes the type, the system and the
code, and reports the write payload above with nothing added to it.

Both strings are opaque. No scheme is known to the engine, no code is checked against a
syntax, and nothing anywhere reads either value — which is what keeps a mapping to a foreign
vocabulary a line of registry data somebody reviews rather than a table compiled into the
tool ([0010](./decisions/0010-the-engine-carries-no-domain-vocabulary.md)). A type carries as
many of them as there are schemes worth mapping into, and at most one code per system:
classifying a type in a system it already carries is a usage error naming the code it already
has, because a second code from one scheme is a disagreement nothing has a rule for resolving.

The change lands in the registry file the type was declared in. That is not a routing
decision — a type is where somebody wrote it, and this adds a child to that declaration.

### `set-label`

The display text of one thing, and nothing else. It takes an id and a label.

A label carries no meaning to anything in the engine: nothing resolves through it, nothing
is derived from it, and no two things are required to have different ones. Renaming is
therefore a one-line diff rather than a re-identification — the id, the global id derived
from it and every reference written to it are what they were
([0002](./decisions/0002-immutable-id-mutable-label.md)).

An empty label, written `dfcad set-label site:S-101 ""`, removes it, which is how a thing
goes back to having none. Leaving the argument out altogether is a usage error rather than
the same thing.

### `retire`

That a thing stopped existing. It takes the id, and says why.

| Flag | Meaning |
|------|---------|
| `--reason "<text>"` | Why it stopped existing. Required. |
| `--replacement <id>` | The node that stands in its place, where one does. |
| `--date <YYYY-MM-DD>` | When it stopped existing. Today by default. |

Retiring is **not** deleting. The node stays in the file, its id stays in the graph and
every claim ever written on it is still there to be read, so a reference written years ago
resolves either to the thing it always named or to a retired node that says what happened to
it.

A reason is required because a retirement with no reason is a deletion wearing a hat: what
the record loses is not the node, which is still there, but the one sentence explaining why
it stopped being true.

A node other things still reference is a **usage error** naming every referrer and the
relation each wrote. Supply a replacement and those references are redirected to it in the
same change, which is the whole of what a replacement is for — and is why redirecting them
is not left as a second command somebody may not run. A replacement that is itself retired
is refused: that is the same problem one reference further along.

```json
{
  "version": 2,
  "command": "retire",
  "dryRun": false,
  "files": [
    {
      "path": "entities/site.dfc",
      "status": "rewritten",
      "effects": [
        {"op": "modified", "tag": "node", "id": "site:S-102"},
        {"op": "modified", "tag": "node", "id": "site:S-101"}
      ],
      "diff": "--- entities/site.dfc.orig\n+++ entities/site.dfc\n@@ -37,6 +37,11 @@\n..."
    }
  ]
}
```

The effects of a retirement with a replacement are the referrers that were redirected and
then the node itself, in the order the change applied them.

### What the claim commands add to the write payload

`add-claim`, `supersede` and `deprecate-claim` write the payload above with three fields
beside it. They are documented once here for the reason the payload itself is: they mean the
same thing in all three.

| Field | Type | Meaning |
|-------|------|---------|
| `claim` | string, optional | The id of the claim that was written. Absent where it wrote none, which is the ordinary case: an id is required only of a claim something references. |
| `replaced` | string, optional | The id of the claim that was retracted. Absent for a change that retracted none, and absent on a supersession whose retracted claim wrote no id of its own. |
| `rankable` | boolean | Whether the claim that was written can take part in resolution, which is whether it carries an accuracy whose terms combine into one figure. False for a claim with no accuracy, and false for one whose terms are in more than one unit, since nothing converts between them; either carries an `unrankable` notice. |
| `notices` | array | What the change has to say about the model it produced. Empty rather than null when it had nothing to say. |
| `notices[].kind` | string | One of `unrankable`, `conflict`, `unresolvable`. |
| `notices[].message` | string | The notice as a sentence, which is presentation. A caller branches on the kind. |
| `notices[].subject` | string | The thing the claim is about. |
| `notices[].predicate` | string | The predicate it was written under. |
| `notices[].competing` | array, optional | The claims already written on the same subject and predicate, each in the shape `claims` reports a claim in. Present only on a `conflict`. |

A **notice is not a diagnostic and not a failure.** Nothing is wrong with what anybody wrote:
the files load, the change is permitted, and what is being reported is a consequence of it
the author is entitled to have wanted. A claim with no accuracy is a legitimate claim, a
second claim about one thing is the most valuable thing in a model, and a retraction that
leaves nothing behind is sometimes exactly the record that should be kept. What none of them
is, is something to discover later. Every notice is also written to stderr, on every run and
in every format.

| Kind | When |
|------|------|
| `unrankable` | The claim's accuracy does not combine into one figure: it carries none, or its terms are in more than one unit, which nothing converts between and which the message names, or a term's magnitude is not a finite number. It loads, it can never win resolution, and it is not given a default. |
| `conflict` | The claim was written on a subject and predicate the model already states. The competing claims are named. |
| `unresolvable` | A retraction left its subject and predicate with no live claim at all, so nothing resolves under it. |

### `add-claim`

A value and the evidence for it, attached to one thing. It takes the subject and the
predicate, and the axes of the claim.

| Flag | Meaning |
|------|---------|
| `--value <value>` | What is claimed, in the shape the predicate declares: a scalar is one real number, a coordinate is its components in order, a text value is written as it stands, and a transform is thirteen reals — three of translation, nine of rotation, then the scale. Required. |
| `--unit <unit>` | The unit it is expressed in, which must be the one the predicate declares. A non-dimensional predicate takes none, and there is no unitless token. |
| `--source "<text>"` | The evidence: a report, a drawing, a person, an instrument log. Required. |
| `--method <id>` | An id naming how the value was obtained. Required. |
| `--accuracy "<term>"` | A term of how well it is known, written as the file writes one without its parentheses: `independent <magnitude> <unit>`, or `systematic <magnitude> <unit> <term-id>`. Repeat for more than one term. |
| `--date <YYYY-MM-DD>` | The day the value was obtained. Today by default. |
| `--id <claim-id>` | Write the claim with this id instead of leaving it unnamed. |

The predicate is checked against the registry before anything is written, and so are the
value's shape, its number of components and its unit. A predicate nothing declares, a
predicate declared to take a plain value instead, a value of another shape, a coordinate of
another dimension and a unit other than the declared one are each a **usage error** — exit
`3`, with nothing on stdout — naming what was asked for and what would have been permitted.

Leaving the accuracy out is permitted, and is reported as `unrankable`. That is the one
escape hatch the bare-scalar rule keeps open
([0008](./decisions/0008-a-bare-scalar-is-a-load-error.md)), and taking it deliberately is
different from taking it by accident.

Adding a second claim under a subject and predicate that already carries one **succeeds** and
reports a `conflict` naming what it now competes with. Repeating a predicate is the normal
case rather than an error: two width claims on one node are two measurements, and the
disagreement between them is the most valuable thing in the file. `supersede` is the command
for correcting rather than disagreeing.

```json
{
  "version": 2,
  "command": "add-claim",
  "dryRun": false,
  "files": [
    {
      "path": "entities/site.dfc",
      "status": "rewritten",
      "effects": [{"op": "modified", "tag": "node", "id": "site:S-101"}],
      "diff": "--- entities/site.dfc.orig\n+++ entities/site.dfc\n@@ -7,3 +7,9 @@\n..."
    }
  ],
  "rankable": true,
  "notices": []
}
```

### `supersede`

A correction: the new claim is written and the claim it replaces is deprecated in its favour,
in one change that lands completely or not at all. It takes the same flags as `add-claim`,
and the same subject and predicate.

The claim being corrected is the **one live claim** written on that subject under that
predicate. It is named that way rather than by an id because most claims write none. A
subject and predicate nothing states is refused rather than added to — a value nothing yet
claims is added with `add-claim` — and one stated more than once is refused naming the
competing claims, because which of them is being corrected is not something to guess at;
deprecate that one by its id instead.

The new claim is **given an id**, because the claim it replaces names it. That is when a
claim id is generated, and the format is `<subject>:<predicate>:<n>`, where `n` is the lowest
ordinal from one that nothing in the model already holds. Nothing is ever inferred back out
of it: it is a name and not a schema, like every other id in this model
([0002](./decisions/0002-immutable-id-mutable-label.md)).

Correction is supersession and **never an edit**. No command in this interface writes over a
claim's value: the old claim keeps its value, its evidence, its method and its date exactly
as they were written, and the model gains the reason the number changed rather than losing
the number it used to be
([0009](./decisions/0009-derived-values-are-never-written-back.md)).

```json
{
  "version": 2,
  "command": "supersede",
  "dryRun": false,
  "files": [
    {
      "path": "entities/site.dfc",
      "status": "rewritten",
      "effects": [
        {"op": "modified", "tag": "node", "id": "site:S-101"},
        {"op": "modified", "tag": "node", "id": "site:S-101"}
      ],
      "diff": "--- entities/site.dfc.orig\n+++ entities/site.dfc\n@@ -7,6 +7,14 @@\n..."
    }
  ],
  "claim": "site:S-101:area:1",
  "rankable": true,
  "notices": []
}
```

### `deprecate-claim`

That a claim was retracted. It takes the id of the claim, and the id of the claim that stands
in its place.

| Flag | Meaning |
|------|---------|
| `--superseded-by <claim-id>` | The claim that stands in its place. Required. |

Deprecating is not deleting, and it is not editing. The claim stays in the file with
everything it said, and what changes is that it now says it was retracted and by what.

A replacement is **required**, and a deprecation naming none is refused. That is the whole of
what keeps `deprecated` from becoming a delete button: a rank cannot be used to make a
measurement quietly go away ([0007](./decisions/0007-rank-is-closed.md)). A replacement that
names no claim, a claim named as its own replacement, and a claim that is already deprecated
are each a **usage error** for the same reason. A supersession that closes a ring is refused
at commit, by the pass that walks the chain.

Retracting the only live claim of a subject and predicate is permitted, and is reported as
`unresolvable`.

```json
{
  "version": 2,
  "command": "deprecate-claim",
  "dryRun": false,
  "files": [
    {
      "path": "entities/site.dfc",
      "status": "rewritten",
      "effects": [{"op": "modified", "tag": "node", "id": "site:S-103"}],
      "diff": "--- entities/site.dfc.orig\n+++ entities/site.dfc\n@@ -30,4 +30,6 @@\n..."
    }
  ],
  "replaced": "site:M-0001",
  "rankable": false,
  "notices": [
    {
      "kind": "unresolvable",
      "message": "nothing is left asserted about the area of site:S-103, so it has no resolvable value",
      "subject": "site:S-103",
      "predicate": "area"
    }
  ]
}
```

### `apply`

A batch of edits from an operation file, applied as one change. It takes the file to read,
or none — or `-` — to read standard input, so a generated batch can be piped in.

The file's shape is [the operation file format](./operation-file.md): one JSON object
carrying an optional `version` and the `operations`, each naming the command that makes the
same change on its own and carrying that command's flags as its members. That document is the
input contract; this is what applying one reports.

A batch is one transaction. The model is read once, every operation is applied to it in
order, and the model they produce together is validated once — so an operation may name what
an earlier one wrote, and nothing is judged against the model as it stands halfway through.

```json
{
  "version": 2,
  "command": "apply",
  "dryRun": false,
  "files": [
    {
      "path": "entities/site.dfc",
      "status": "rewritten",
      "effects": [
        {"op": "created", "tag": "node", "id": "site:S-104"},
        {"op": "modified", "tag": "node", "id": "site:S-104"}
      ],
      "diff": "--- entities/site.dfc.orig\n+++ entities/site.dfc\n@@ -7,3 +7,4 @@\n..."
    }
  ],
  "operations": [
    {
      "index": 1,
      "op": "add-node",
      "effects": [{"op": "created", "tag": "node", "id": "site:S-104"}],
      "notices": []
    },
    {
      "index": 2,
      "op": "add-claim",
      "effects": [{"op": "modified", "tag": "node", "id": "site:S-104"}],
      "notices": []
    }
  ],
  "totals": {"operations": 2, "created": 1, "modified": 1, "retired": 0},
  "notices": []
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `operations` | array | One entry per operation, in the order they were applied. |
| `operations[].index` | integer | Its place in the batch, counted from one — which is how a refusal names an operation, so the two can be read together. |
| `operations[].op` | string | The operation it was. |
| `operations[].effects` | array | What it did to the model, in the order the mutations were applied. The same effects `files[].effects` carries, grouped by the operation that caused them instead of by the file they landed in. |
| `operations[].claim` | string, optional | The id of the claim it wrote. Absent where it wrote none, or wrote one with no id of its own. |
| `operations[].replaced` | string, optional | The id of the claim it retracted. Absent for an operation that retracted none. |
| `operations[].snaps` | array, optional | Every corner a `scaffold-loop` landed on a vertex the model already held, in the shape that command's payload documents. Absent for every other operation. |
| `operations[].notices` | array | What it had to say about the model it produced, in the shape the claim commands document. |
| `totals` | object | What the batch did as a whole. |
| `totals.operations` | integer | How many operations were applied. |
| `totals.created` | integer | How many things the batch created. It counts effects rather than files: what an author asked for is a node, not the file it landed in. |
| `totals.modified` | integer | How many it modified. |
| `totals.retired` | integer | How many it retired. |
| `notices` | array | Every notice the batch produced, in the order the operations reported them. The same notices `operations[].notices` carries, gathered. |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

Exit codes are the ones every write command has, with the operation file reading as input:

| Code | When |
|------|------|
| `0` | The batch was applied, or, under `--dry-run`, would have been. |
| `2` | The operation file could not be read or is not a batch, or a file could not be written, which write nothing to stdout; or the change was refused because the resulting model would not load or the tree did not load to begin with, where stdout carries [the refusal](#the-refusal). |
| `3` | The invocation was wrong, or an operation of the batch was: an id something already holds, a type nothing declares, a value of the wrong shape. It is the code the same mistake gets from the command that makes the change on its own. |

A refused batch writes nothing at all, whichever of the three passes refused it. A file which
could not be read or is not a batch is an error, and nothing reaches stdout; a batch whose
model would not load, or a tree which did not load to begin with, is a refusal, and stdout
carries [the refusal](#the-refusal) with the diagnostics that refused it. What is wrong with the *file* is reported in full — every operation that
has a problem, each named by its index — because an author fixing a generated batch should
not have to reissue it once per mistake. What the *model* refuses is the first operation it
refuses: the operations after it may depend on it, and the failures they would then have
would bury the one that is real.

### The shape every artefact command reports

An **artefact command** is one whose product is a file this contract does not describe — an
export, or anything else that writes a build output outside the authored tree. What it writes
to stdout is not the artefact and never can be: it is the account of one, and it has the same
shape whichever command wrote it, so it is documented once here rather than repeated per
command ([0022](./decisions/0022-a-command-whose-product-is-a-file-answers-on-stdout.md)).

[`export`](#export) and [`export-map`](#export-map) are the commands which produce one. The
shape below is the shape both write, and it was fixed here before either existed so that the
first of them could not invent one.

```json
{
  "version": 2,
  "command": "export",
  "derived": true,
  "digest": "9f2c1ab4c0d7e5f38a2b6109d4e7c8b5a3f10e29d6c4b8a70f5312cd9e846b7a",
  "files": [
    {
      "path": ".dfcad/export/9f2c1ab4c0d7e5f38a2b6109d4e7c8b5a3f10e29d6c4b8a70f5312cd9e846b7a/model.ifc",
      "status": "written"
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `derived` | bool | Whether an artefact was produced. Written whatever the outcome, with the same meaning it has on [`measure`](#measure), [`buildable`](#buildable) and [`site`](#site): an artefact that was written reads as `derived` true, and a model no artefact could be made of reads as `derived` false. |
| `digest` | string, optional | The digest of the source tree the artefact was derived from, lower-case hex, so a caller can check the artefact against the tree in front of them. Written on a refusal too. Absent for a model which was not read from disk, or one a file of which could not be read at all. Under [`--assume`](#assumed), the digest of the tree the batch would produce. |
| `files` | array | One entry per file the artefact consists of, ascending by `path` compared byte-wise. Empty rather than null when nothing was written. |
| `files[].path` | string | Where the file is, exactly as it would be opened. An artefact under the build directory is written beneath a directory named for the key it was produced under ([0021](./decisions/0021-an-export-is-a-build-output-keyed-by-its-source-digest.md)), which is a path a caller cannot predict — so this field is how what was just produced is found. |
| `files[].status` | string | One of `written`, `unchanged`. |
| `identifiers` | array, optional | Written only under `--evidence`. One entry per rooted object, ascending by `id`, each a node `id` and the `global-id` derived for it ([0004](./decisions/0004-globalid-derives-from-a-pinned-namespace.md)). It is left out by default because it grows one entry per node and because every entry is recomputable exactly from the model a caller already has ([0017](./decisions/0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md)). |
| `diagnostics-suppressed` | integer, optional | How many diagnostics the limit held back from `diagnostics` and from stderr alike. Absent where it held back none. See [Diagnostics](#diagnostics). |
| `diagnostics` | array, optional | Every diagnostic the run rendered on stderr, in the order rendered, and always the last field of the object. Absent where the run rendered none. See [Diagnostics](#diagnostics). |

**What an artefact command refused is in [`diagnostics`](#diagnostics).** On exit `1`, the
diagnostics which kept the artefact from being written — a ring which will not read, a curve
drawn to no tolerance — are there, and a warning rendered on a run which did write one, such
as a map naming no coordinate reference system, is there too. `export-map`'s `undrawn[]` keeps
its shape, and the diagnostic behind an entry whose reason is a defect is in `diagnostics` as
well.

Statuses:

| Status | Meaning |
|--------|---------|
| `written` | This run wrote the file. |
| `unchanged` | The artefact for this key was already on disk and this run left it in place. Nothing was written for it. |

**`files[]` describes files that are on disk, and never anything else.** There is no
`--dry-run` on an artefact command and no `dryRun` field: what these commands write is
disposable, ignored by git and reproducible, so there is nothing for a dry run to protect and
no diff for it to show. There is also no `failed` status, because **an artefact is
all-or-nothing** — one run produces its whole file set or none of it, and a run that could not
finish leaves nothing behind that a later run would read as the artefact for that key.

**The artefact is never written to stdout,** under any flag. A caller who wants the bytes names
a destination outside the model root and reads the file; stdout stays one JSON object, as it is
for every other command.

**A clock-derived field inside the artefact carries the derivation epoch,
`1970-01-01T00:00:00Z`.** Where the target format defines a field as a creation or a
modification time — a part 21 header's time stamp, a PDF's `CreationDate`, a container
manifest's `created` — the field is omitted where the schema permits it and written as that
instant where the schema requires it. No exporter reads the system clock, so re-running an
artefact command over an unchanged tree produces a byte-identical file and a `files[].status`
of `unchanged` rather than a new artefact
([0021](./decisions/0021-an-export-is-a-build-output-keyed-by-its-source-digest.md)).

It is one derivation — `dfcad.DerivationEpoch`, taking the digest of the tree the artefact was
derived from — and one set of renderings, so a format's encoding is not each exporter's own
business. A tree a file of which could not be read has no digest and still derives the same
instant: a refusal is a diagnostic, and nothing about a time stamp is entitled to fail on the
way to reporting one.

There is no field on this payload for it and no flag which overrides it. The value is a
constant, so reporting it would be noise; the provenance the field pretends to carry is the
`digest` above, which is the thing that actually moves with the model. A caller who needs the
real date of an export run attaches it outside the file, where it is visibly a fact about the
run rather than a fact about the model.

Exit codes:

| Code | When |
|------|------|
| `0` | The command answered. Either the artefact exists — `derived` true, with `files` naming it — or the model held nothing the format carries, which is `derived` true with `files` empty. |
| `1` | The artefact could not be produced from the model that was read. `derived` false, `files` empty, `digest` written, and the refusal under [`diagnostics`](#diagnostics), so a caller reads why from the object rather than from stderr. |
| `2` | The model could not be read: the root is not there, the tree did not load, or a file of it could not be read. Where the tree did not load, stdout carries [the refusal](#the-refusal) — the envelope, `"refused": true` and the diagnostics, and no `derived`, `digest` or `files`; where it could not be read at all, nothing. |
| `3` | The invocation was wrong: a required flag missing, or a destination inside the authored tree, which is refused before anything is read. |

A model that exports to nothing is **exit `0`**, and it is the same judgement `buildable`
makes about a parcel its own setbacks consumed: the command answered, and the answer is that
there is nothing. Whether a format has a meaningful empty artefact — a header with no contents
— is that format's own business; where one is written it appears in `files[]` like any other.

### `export`

The model's spatial structure, written as an IFC4 exchange file. It is an [artefact
command](#the-shape-every-artefact-command-reports) and writes that shape, plus one field of
its own.

```json
{
  "version": 2,
  "command": "export",
  "derived": true,
  "digest": "9f2c1ab4c0d7e5f38a2b6109d4e7c8b5a3f10e29d6c4b8a70f5312cd9e846b7a",
  "schema": "IFC4",
  "files": [
    {
      "path": ".dfcad/export/9f2c1ab4c0d7e5f38a2b6109d4e7c8b5a3f10e29d6c4b8a70f5312cd9e846b7a/model.ifc",
      "status": "written"
    }
  ],
  "classifications": [
    {
      "id": "site:TY-01",
      "type": "Typo",
      "code": "IfcWahl",
      "entity": "IFCBUILDINGELEMENTPROXY",
      "reason": "unknown"
    },
    {
      "id": "site:LW-01",
      "type": "LegacyWall",
      "code": "IfcWallStandardCase",
      "entity": "IFCBUILDINGELEMENTPROXY",
      "reason": "unwritten"
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `schema` | string, optional | The schema the artefact was written in, exactly as the file's `FILE_SCHEMA` declares it: `IFC4`. Absent on a refusal, because nothing was written in any schema. It is a field of this payload rather than of the shared shape, because what a format calls its version is that format's business. |
| `classifications` | array | One entry per node whose type declared an `IFC4` classification this writer could not carry, and which therefore reached the file as an `IFCBUILDINGELEMENTPROXY`. Ascending by `id`, and `[]` rather than absent when there are none. Written on a refusal too: which classifications could not be carried is a fact about the model rather than about the artefact. |
| `classifications[].id` | string | The node written as a proxy. |
| `classifications[].type` | string | The type it is declared as, which is what carries the classification and what would be edited to fix it. |
| `classifications[].code` | string | The classification the type declares under the `IFC4` system, exactly as the registry spells it — the registry's spelling and not the upper-cased one the writer compares, because it is what a person would search the registry for. |
| `classifications[].entity` | string | What the node was written as instead: `IFCBUILDINGELEMENTPROXY`. Stated rather than assumed. |
| `classifications[].reason` | string | `unwritten` or `unknown`; see below. |
| `storeys` | array, optional | Written only under `--evidence`, and only beside an artefact: absent without the flag and absent on a refusal, exactly as `identifiers` is. One entry per `IfcBuildingStorey` in the file, ascending by `id` compared byte-wise, and `[]` rather than absent when the file holds none. It sits under `--evidence` for the reason the manifest does: it grows with the model, on a call whose default answer is a handful of fields ([0022](./decisions/0022-a-command-whose-product-is-a-file-answers-on-stdout.md)). |
| `storeys[].id` | string | The node written as the storey. |
| `storeys[].elevation` | object, optional | Where the storey was written. Absent for a storey declaring no frame, which the file writes with `Elevation` `$`. |
| `storeys[].elevation.value` | number | The elevation, exactly the number the file writes as the storey's `IfcBuildingStorey.Elevation` and as the lift of its placement. It is recorded from the value the writer wrote rather than derived a second time, so the file and the answer cannot disagree. |
| `storeys[].elevation.unit` | string | The root frame's linear unit, which every coordinate in the file is written in ([0005](./decisions/0005-one-linear-unit-per-frame.md)). |
| `storeys[].elevation.frame` | string | The root frame, which is the frame every coordinate in the file is written in ([0024](./decisions/0024-every-coordinate-in-an-export-is-written-in-the-root-frame.md)). An elevation in any other frame would be a value the artefact does not hold. |
| `storeys[].elevation.budget` | object, optional | The accumulated uncertainty of the fits the storey's frame chain passes through on its way to the root, in route order, with `from` the storey's frame and `to` the root: the [`budget`](#budget) `resolve --frame` writes, field for field, including `unknown` for a fit stating no accuracy and `units` for fits written in different units. Absent where the storey's frame is the root, because nothing moved it and its value is `0`. |
| `bodies` | array, optional | Written only under `--evidence`, and only beside an artefact: absent without the flag and absent on a refusal, exactly as `storeys` is. One entry per node the file gives a `Body` representation — the swept solids of a node drawn as an area or as a line — ascending by `id` compared byte-wise, and `[]` rather than absent when there are none, which is what a run naming no `--height` writes. A node drawn as several solids is one entry, because its base and height are read once for the node. An opening cut for a filling is not a body of any node and is not listed. |
| `bodies[].id` | string | The node the body was written for. |
| `bodies[].entity` | string | The entity the node was written as, upper-case as `classifications[].entity` spells it: `IFCSPACE`, `IFCWALL`, `IFCBUILDINGELEMENTPROXY`. |
| `bodies[].base` | object | Where the body starts, in the shape [`storeys[].elevation`](#export) takes: `value`, `unit`, `frame` and `budget`. |
| `bodies[].base.value` | number | The level the boundary lies at in the root frame, moved by the offset claimed of it under `--offset`: exactly the position the file gives the solids plus the datum the node's placement stands at, which is what a reader composing the file's placement chain with the solid's position arrives at. It is recorded from the values the writer wrote rather than derived a second time. |
| `bodies[].base.unit` | string | The root frame's linear unit, as for `storeys[].elevation.unit`. |
| `bodies[].base.frame` | string | The root frame, as for `storeys[].elevation.frame`. |
| `bodies[].base.budget` | object | The [`budget`](#budget) of the boundary carried into the root frame — the claims its corners were read from and the fits on the route — with the offset claim added where one was read. Written without `from` or `to`, because it is a computation rather than a route, as `buildable` writes one. A claim stating no accuracy is named in `unknown` and `combined` is absent. |
| `bodies[].top` | object | Where the body ends, in the same shape: `value` is `base.value` plus the depth the solids are swept through, and `budget` is `base.budget` with the height claim added. A term the two share is counted once. |

Everything else — `derived`, `digest`, `files[]`, `identifiers` under `--evidence` — is the
shared shape, with the meanings documented there. `storeys` and `bodies` are the two fields
`--evidence` adds beside the manifest which are this command's own.

A storey two fits above the root, with the one below it on the root itself, reads:

```json
"storeys": [
  {"id": "site:L-01", "elevation": {"value": 0, "unit": "m", "frame": "frame:plan-ground"}},
  {"id": "site:L-03", "elevation": {"value": 5.8, "unit": "m", "frame": "frame:plan-ground",
    "budget": {"from": "frame:plan-attic", "to": "frame:plan-ground",
      "terms": [{"kind": "independent", "name": "site:C-0002", "magnitude": 0.003, "unit": "m", "contributors": ["site:C-0002"]},
                {"kind": "independent", "name": "site:C-0001", "magnitude": 0.004, "unit": "m", "contributors": ["site:C-0001"]}],
      "combined": {"magnitude": 0.005, "unit": "m", "coverage-factor": 1}}}}
]
```

A room drawn on a plan grid lifted 3 m above the root, with its floor stepped up by a claimed
0.15 m and a claimed clear height of 2.4 m, reads (corner terms elided):

```json
"bodies": [
  {"id": "site:S-02", "entity": "IFCSPACE",
   "base": {"value": 3.15, "unit": "m", "frame": "frame:plan-ground",
     "budget": {"terms": [{"kind": "independent", "name": "the position of geom:V-302-A", "magnitude": 0.004, "unit": "m", "contributors": ["..."]},
                          {"kind": "independent", "name": "site:C-0001", "magnitude": 0.004, "unit": "m", "contributors": ["site:C-0001"]},
                          {"kind": "independent", "name": "the step of site:S-02", "magnitude": 0.005, "unit": "m", "contributors": ["..."]}],
       "combined": {"magnitude": 0.0102, "unit": "m", "coverage-factor": 1}}},
   "top": {"value": 5.55, "unit": "m", "frame": "frame:plan-ground",
     "budget": {"terms": ["... the base's terms ...",
                          {"kind": "independent", "name": "the clear-height of site:S-02", "magnitude": 0.006, "unit": "m", "contributors": ["..."]}],
       "combined": {"magnitude": 0.0119, "unit": "m", "coverage-factor": 1}}}}
]
```

**The spatial structure crosses the boundary always, and the geometry only when the run says
what to read it under.**
The project, and every node whose kind is `Site`, `Building`, `Storey` or `Space`, is written
as the IFC entity that kind is — the two vocabularies are one for one — nested by `within`
through `IfcRelAggregates`, each with a local placement relative to its parent's. A node whose
kind is `Zone` is written as an `IfcZone` with its `member-of` members assigned through
`IfcRelAssignsToGroup`. A node whose kind is `Element` or `Interface` is contained in the
nearest spatial ancestor it has, through `IfcRelContainedInSpatialStructure`.

**A node within nothing is written all the same, contained in nothing.** An element or an
interface no site, building, storey or space contains, at any remove, is written in no
relationship at all — IFC4 does not require a product to stand in the spatial structure, and
an annotation, a survey mark or a meter nobody has put in a room commonly does not. Its local
placement is relative to nothing, which is the root frame the whole file is written in, and it
is drawn, placed as a point or widened as a line exactly as any node of its geometry is. It is
not put in a site or a storey the model does not say it is in, and it is not left out: every
node the model has not retired is in the file.

**What an element is written as comes from its type's classification, and the fallback is a
proxy.** A type declaring `(classification "IFC4" "IfcWall")` puts its instances in the file as
`IFCWALL`. A type declaring nothing under that system — or naming an entity this writer has no
attribute list for — puts them in as `IFCBUILDINGELEMENTPROXY` with the type's own name in
`ObjectType`, which is what the IFC specification blesses that entity for. Classifications
under any other system are carried by the model and read by nobody here: they name the thing in
somebody else's vocabulary rather than naming an entity in this file's.

**The set of entities a classification may name is closed, and it is this:**

```
IfcAirTerminal              IfcDistributionChamberElement  IfcOpeningElement
IfcAnnotation               IfcDistributionElement         IfcOutlet
IfcBeam                     IfcDistributionFlowElement     IfcPipeFitting
IfcBuildingElementProxy     IfcDoor                        IfcPipeSegment
IfcCableSegment             IfcDuctSegment                 IfcPlate
IfcCivilElement             IfcElectricAppliance           IfcRailing
IfcColumn                   IfcElectricDistributionBoard   IfcRamp
IfcCommunicationsAppliance  IfcFooting                     IfcRoof
IfcCovering                 IfcFurnishingElement           IfcSanitaryTerminal
IfcCurtainWall              IfcGeographicElement           IfcSlab
                            IfcMember                      IfcStair
                                                           IfcTank
                                                           IfcValve
                                                           IfcWall
                                                           IfcWindow
```

A registry is authored against that list rather than against the writer's source. The set is
what the writer holds a transcribed IFC4 attribute list for, and an entity outside it is
absent because IFC4 gives it a different attribute list — an instance written with the wrong
number of attributes is a file no reader loads, so the answer is a proxy and a report rather
than a guess.

**`IfcAnnotation` is the one entity in that set which is not an element.** A dimension, a
label, a survey tie or a north arrow is drawn to be read rather than built, and IFC4 makes it
an `IfcProduct` and no more: its attribute list is a product's seven — `GlobalId`,
`OwnerHistory`, `Name`, `Description`, `ObjectType`, `ObjectPlacement`, `Representation` — with
no `Tag`, which is `IfcElement`'s, and no `PredefinedType`, which IFC4 does not give it. It
carries everything a proxy of its type would: its identity, its text, its placement, its
containment and, where it is drawn, its `FootPrint` and `Body` exactly as any drawn element's.
What it cannot be is voided, the filling of an opening, or the element bounding a space, because
IFC4 types each of those relationships as naming an `IfcElement`; an export which would need
one is refused rather than written with a reference no reader accepts.

**An entity carrying a `PredefinedType` is written with it absent (`$`), never as `.NOTDEFINED.`.**
That holds of `IfcAirTerminal`, `IfcCableSegment`, `IfcCommunicationsAppliance`,
`IfcDistributionChamberElement`, `IfcDuctSegment`, `IfcElectricAppliance`,
`IfcElectricDistributionBoard`, `IfcGeographicElement`, `IfcOpeningElement`, `IfcOutlet`,
`IfcPipeFitting`, `IfcPipeSegment`, `IfcSanitaryTerminal`, `IfcTank` and `IfcValve` as much as of a wall or a door. Which member of the entity's
enumeration applies — for an air terminal, a diffuser, a grille, a register or a louvre; for a
cable segment, a cable, a conductor, a core or a busbar; for a communications appliance, a
router, a modem, a network hub or a gateway; for a distribution chamber, a manhole, an
inspection chamber, a sump or a valve chamber; for a duct segment, a rigid segment or a
flexible one; for an electric appliance, a dishwasher, a washing machine or a refrigerator; for
an electric distribution board, a distribution board, a consumer unit, a switchboard or a motor
control centre; for a geographic element, whether it is terrain; for an opening, whether it goes
right through what it is in or is a recess part of the way into it; for an outlet, a power, a
data, a telephone or an audio-visual outlet; for a pipe fitting, a bend, a junction, a transition,
an entry, an exit, an obstruction or a connector; for a pipe segment, a rigid segment, a flexible
segment, a culvert, a gutter or a spool; for a sanitary terminal, a toilet pan, a wash-hand basin, a
sink, a bath, a shower, a urinal or a bidet; for a tank, a basin, a break-pressure tank, an
expansion vessel, a feed-and-expansion tank, a pressure vessel, a storage tank or a vessel; for a valve, an isolating valve, a
stopcock, a check valve, a faucet, a draw-off cock, a pressure-reducing or a pressure-relief
valve — is a statement about the thing, and the model holds no predicate making it.
`.NOTDEFINED.` is a value, and writing it would say somebody looked and found no member fits,
which nobody did; absent says only that the file does not know. A type's name is not read for
it: a type called `register`, `lv-run`, `media-panel`, `septic-dbox`, `duct`, `appliance`,
`panel`, `control-point`, `opening`, `jack`, `cleanout`, `septic-trench`, `plumbing-fixture`, `septic-tank` or `valve` is a name its author chose, not a claim about the enumeration, and it reaches the file
in `ObjectType`, as it does for every product. An entity IFC4 gives no
`PredefinedType` is written without one, its attribute list ending at `Tag`: `IfcCivilElement`
— a driveway, a walk, a patio, a retaining wall run — as much as `IfcFurnishingElement`,
`IfcDistributionElement` — a receptacle, a switch, a smoke detector, a thermostat — as much as
either, and `IfcDistributionFlowElement` — an air handler, a condenser, a damper, a water heater
— as much as any of them. The first three are declared directly under `IfcElement` in IFC4 and
add nothing to it, and `IfcDistributionFlowElement` is declared under `IfcDistributionElement`
and adds nothing to that; the `PredefinedType` their subtypes carry belongs to those subtypes.

The openings the export cuts for a filling are the one exception, and are written `.OPENING.`:
each is cut through the whole thickness of the element it voids by construction, so which
member applies is known rather than claimed. A node classified `IfcOpeningElement` is the
model's own statement about an opening, drawn to whatever depth its author drew it, and nothing
says whether that goes right through.

**A filling classified `IfcOpeningElement` is the opening itself.** A cased opening — a doorway
with no door — whose type declares `(fills-opening #t)` is written as an `IFCOPENINGELEMENT`
contained in its storey like any product, with its own `GlobalId`, text, placement and shapes,
and one `IFCRELVOIDSELEMENT` joining the element it is within to it. Nothing is cut for it to
stand in and nothing fills it, because IFC4 fills no opening with another; the relationship's
identifier is derived from `ifc/voids/<id>`, the name a filling's would be, and no
`ifc/opening/<id>` or `ifc/fills/<id>` is derived for it. For the same reason a filling within
an element classified `IfcOpeningElement` is refused naming both. One within no element is
written contained, voiding nothing, which is all the model says of it.

**A classification the writer cannot carry is reported, not silently proxied.** Every node
whose type declared a code outside that set is named in `classifications[]`, with the code it
declared, and a warning naming the *type* — one per type, pointing at the line of the registry
which declared it — goes to stderr. The node-level answer and the type-level diagnostic are two
granularities on purpose: a node is what a caller holding the file has in front of it, and a
type is what would be edited to fix it, so a model with nineteen doors of one type is told once
about the type and lists nineteen nodes.

**The two mistakes are told apart, because their fixes differ.** A `reason` of `unwritten`
means IFC4 defines a product entity of that name and this writer has no attribute list for it
— `IfcPile`, `IfcLightFixture`, the deprecated `IfcWallStandardCase`. The classification is right,
the proxy stands in for it faithfully, and the fix is a line in the writer. A `reason` of
`unknown` means IFC4 defines no product entity of that name at all — a misspelling like
`IfcWahl`, or a code naming something a product may not be, such as a relationship
(`IfcRelSpaceBoundary`) or a type object (`IfcWallType`). The classification is wrong and the
proxy is standing in for nothing anybody meant.

**A type declaring no `IFC4` classification at all is not reported.** That is the case the
proxy is specified for — an element which no more specific entity covers, named in
`ObjectType` — and listing it would bury the codes which are actually wrong under every node
nobody has classified yet.

**Neither reason is a refusal.** The file is written and the exit code is `0`: a proxy naming
its own type is a complete statement of what the model holds, and an export which refused would
stop a model being exchanged over a mapping its author may have meant. The one place a
classification *is* a refusal is a node somebody claimed a height of whose classification
cannot carry a shape, which is [`--height`](#export)'s business and is documented below.

**Every rooted object carries the `GlobalId`
[0004](./decisions/0004-globalid-derives-from-a-pinned-namespace.md) derives**, from the URL
the project pins and the node's id. The relationships are rooted objects too, and theirs are
derived from a name no id could collide with — `ifc/aggregates/<id>`, `ifc/contains/<id>`,
`ifc/assigns/<id>`, `ifc/boundary/<space>/<edge>/<element>`, and `ifc/project` for the project
itself — because an id is written `namespace:local` and a namespace never contains a slash.
Under `--evidence` the manifest accounts for all of them.

**A retired node is not written.** A thing which stopped existing must not reach a receiving
system as a live one, and what a retirement means for an exchange — a delete, or nothing at
all — is the receiving system's question rather than this command's.

**A node carries the outline its model states, and the solid a viewer can draw, as two
representations of one shape definition.** `--position`, `--tolerance` and `--chord` are the
vocabulary a boundary is read under; they go together or not at all, and a run naming none of
them writes the spatial structure and no shape, which is a correct IFC file and is what this
command wrote before it could draw anything. A run naming all three gives every node whose
boundary it can read a `FootPrint` / `Curve2D` representation built from the rings bounding
it, holes included, drawn to the named chord tolerance — so a curved wall reaches the file as
the curve it is rather than as the straight line between its ends. Arcs are read only where
`--arc-centre` and `--arc-through` name the vocabulary they are written in, exactly as in
[`tessellate`](#tessellate).

**What is drawn is decided by a node's boundary and its declared geometry, never by its
kind.** A room and a countertop are both an area with a height over it, and the sweep which
makes a solid of either is one operation, so an element bounded by a ring is drawn exactly as
a space is; what its kind decides is only which entity the shape is written on. An
`IfcProduct` carries its shape in the same attribute whichever it is, because that is where
the schema declares it, and a product nobody drew writes it absent as every product did
before there was one to write.

**`--height <predicate>` is what adds a body, and it has no default.** Where it names a
predicate and a node's height resolves under it, the node additionally carries a `Body` /
`SweptSolid` representation: the footprint extruded upwards through that height, with the
holes carried through as the profile's `InnerCurves`, so the even-odd nesting the region
derivation computed reaches the file rather than being worked out again from a heap of curves.
The two live in one `IfcProductDefinitionShape`. They are two representations rather than one
because they are not the same statement: the footprint is what the model says, and the body is
a convenience built from a claim. A run naming no predicate, and a node nothing claims a
height of, both export as footprints — a two dimensional file is correct, and it is what an
author who has drawn plans and measured nothing should get.

**`--thickness <predicate>` is the same for a node drawn as a line, and has no default
either.** A partition, a railing and a duct run are each authored as a centreline — one run of
the model, shared by whatever stands either side of it — and each is built as a solid, so the
thickness claimed of the node is what turns the one into the other. Its run is read edge by
edge rather than assembled into a ring, which is the distinction `measure` already draws: a
wall not being a closed cycle is what a line is rather than a mistake in one. Each straight
segment is widened by the claimed thickness, half either side, and swept upwards through the
height. That is a profile per segment rather than one outline mitred around the whole run,
because the joint where two segments meet is a detail the model does not state and is not this
command's to invent. A node drawn as a line with no thickness claimed carries no shape at all:
a centreline of no width is not a solid, and IFC has nowhere to put one.

**`--offset <predicate>` says where a body starts, and has no default either.** A body is
swept from the level its boundary's corners lie at. Where `--offset` names a predicate and a
node's offset resolves under it, the `IfcExtrudedAreaSolid` is positioned at that level plus
the offset instead — a window's sill above the floor it is set in, a garage slab stepped down
below the floor whose walls it shares. The offset is signed, so a base below the boundary's
level is as ordinary as one above it, and an offset of nought is the answer a run naming none
gives. It moves the sweep and nothing else: a node drawn as an area and one drawn as a line
are moved alike whatever their kind, the `FootPrint` stays the plan the model states, and a
storey's frame-chain elevation composes with it exactly as it does with an unmoved body. That
is what lets a window stay a run between two jambs of its wall's centreline — the corners the
wall shares — rather than a second copy of them authored at the sill's height, which nothing
relates to the first. The offset is read only where a body is swept, so a run naming
`--offset` without `--height` is a usage error. An offset which is not a distance, or not in
the unit of its boundary, is refused naming the claim.

**Each claim behind a body travels into the file beside it**, as an `IfcPropertySet` named
`dfcad_HeightProvenance`, `dfcad_ThicknessProvenance` or `dfcad_OffsetProvenance` attached through
`IfcRelDefinesByProperties`. Each carries the predicate, the figure and its unit, and whatever
the claim states: its source, its method, its accuracy, its date, its id, and which step of
the resolution rule chose it. That last is how a surveyed height is told from an assumed one
without holding the model — a claim nothing rankable was said about reads as `unranked`, and
is still used, because it is what the model says. They are separate sets rather than one
because they are separate measurements: a wall's height may be surveyed, its thickness taken
off a drawing, and a window's sill read off an elevation.

**A body claimed of something no entity here can carry one on is refused, naming the claim.**
The proxy fallback above is the answer to a classification this writer has no attribute list
for, and it stays the answer for everything nobody measured. It is not the answer for a node
somebody claimed a height of whose type is classified as an entity which is not a product — a
relationship, a spatial element. The claim says a body was meant and the classification says
where, the two disagree, and saying which claim and which entity is more use than writing the
body somewhere the model did not point at or dropping it in silence.

**A space's boundaries cross as relationships, drawn or not.** Every edge of a room's outline
which names the element realising it is written as an `IfcRelSpaceBoundary` between the two —
one per space and element, so a party wall two rooms reach is two relationships and not one.
This is the place where the engine already holds something a receiving system usually has to
guess at: the wall between two rooms is one node both of them reference, so the fact that it
separates them is stated rather than recovered by comparing outlines and hoping the arithmetic
agrees.

`PhysicalOrVirtualBoundary` is the classification [SPEC §6.3](../SPEC.md#63-edge) computes,
carried through and re-derived nowhere:
an edge something backs is `.PHYSICAL.`, and nothing in the model stores that answer,
so adding a wall changes it with no second edit.
`InternalOrExternalBoundary` is read off the containment the model already states — an element
in the same building as the room is `.INTERNAL.`, one anywhere else, including on the site
rather than in a building, is `.EXTERNAL.` — and off nothing else, because a type name or a
geometry would be a second source for an answer the containment already gives.
`ConnectionGeometry` is written where the run drew the room and omitted where it did not: the
curve is the run of the footprint that edge produced, taken from the segment attribution, so
it is made of the corners the outline already holds and a curved wall's chords come through as
the chords the outline has. A run naming no drawing vocabulary writes the relationships
without any geometry at all, which the schema allows and a topological model should prefer.

**A boundary this schema cannot express is reported and left out, and the export still
succeeds.** `RelatedBuildingElement` is mandatory, so two rooms with nothing built between
them have no relationship to be written as; IFC's own answer is an `IfcVirtualElement`, and
writing one would be this command putting a thing into the artefact which the model does not
hold. The same goes for an edge backed by an element the model has retired, which is written
nowhere for a relationship to point at. An element within nothing is not one of those: it is in
the file, and bounds the room from outside. Both are warnings on stderr naming the space
and the edge, because a gap somebody is told about is one they can close and a silently
missing boundary is not. An edge which bounds one room and nothing else is not reported: the
model has said nothing about what runs along it, so there is no boundary between two things to
leave out. And an edge naming a backing element the model does not hold is a load error
already, reported when the model is read; the exporter does not reclassify it as a boundary
with nothing along it.

**`--crs <predicate>` is what puts the project on the earth, and it has no default.** It names
the predicate the root frame writes the identifier of its projected coordinate reference system
under — a non-claim-bearing text predicate, `(crs "EPSG:6543")`, which needs no change to the
format because a frame already collects any non-structural child verbatim. The file then
carries an `IfcProjectedCRS` naming it and an `IfcMapConversion` into it. A run naming no
predicate exports without a georeference, which is a correct file and is the one a model
nobody has sited should get; so does a run which names one the model does not use.

**The identifier is recorded and never interpreted.** It is checked for shape only — an
authority and a code, `EPSG:6543` — and nothing here resolves it, converts it or looks it up.
Interpreting it would mean a geodetic library, which means cgo, which breaks the static image
this tool ships as, and a licensed parameter dataset besides — for a capability no answer here
needs, because every cross-frame answer in this engine is a similarity transform in the plane
the survey was already projected into.

**`--crs-definition <predicate>` names the register's own definition where the project holds
one**, and it is copied byte for byte into the entity's `Description`. Its linear unit token is
checked against the unit the frame declares — the token, and never the conversion factor beside
it, because the US survey foot is exactly 1200/3937 m and the registers spell that several ways
which differ in their last digits. A definition stating no linear unit token this recognises is
copied unchecked. The flag is of no use on its own: naming it without `--crs` is a usage error,
because a definition is written beside an identifier and there would be nothing to write it
beside.

**Every coordinate in the file is written in the root frame.** A shape authored on any other
frame — a room drawn at nought on the plan grid of its level, a wall set out on a fabrication
grid — is carried there first, by the chain of measured transforms the model states and by
nothing else, which is the same walk `export-map` makes and is what keeps the two exports
agreeing about where one model is
[0024](decisions/0024-every-coordinate-in-an-export-is-written-in-the-root-frame.md). Nothing
reprojects. A shape whose frame does not reach the root is refused naming that frame, and the
export writes no file. What the placements above a shape already stand at — a storey's
elevation, say — is taken back off it, so a coordinate in the file composed with the placements
above it is the coordinate the model states rather than the same lift applied twice.

**The map conversion states what is left, which is nothing.** The root frame *is* the projected
system the chain is rooted at [SPEC §7.5](../SPEC.md#75-frame) and the file's coordinates have
been carried into the root frame, so `Eastings`, `Northings` and `OrthogonalHeight` are nought —
written because the schema requires them, and nought because those two facts hold rather than
because the writer says so. `XAxisAbscissa`, `XAxisOrdinate` and `Scale` are absent, which the
schema reads as no rotation and unit scale. Writing a scale there would state a fit nobody
measured.

**The `IfcProjectedCRS` carries `Name` and `Description` and nothing else.** `GeodeticDatum`,
`VerticalDatum`, `MapProjection` and `MapZone` are written absent, because what this model
holds about a coordinate reference system is two strings — an entry in somebody else's
register, and that register's own text about it — and filling a datum or a projection in from
the identifier would mean reading the identifier, which is the one thing this command promises
never to do with it. `MapUnit` is absent because the file's unit assignment already states the
unit, and two places to state one unit is one place for them to disagree.

**Every `IfcAxis2Placement3D` in the file is axis-aligned, whatever the frames say.** A
rotation between a frame and the root is applied to the coordinates as they are carried into
the root frame, not written into a placement: `Axis` and `RefDirection` are absent throughout,
which the schema reads as the default axes. So a consumer reading placements alone sees an
unrotated model, and one reading coordinates sees the model the frames describe. The
coordinates are the statement; a placement here says only where a thing stands.

**A coordinate reference system on any frame but the root is a refusal**, as are an identifier
which is not an authority and a code, two of them on one frame, and a definition whose unit
contradicts the frame's. Every other frame reaches the root through a measured transform, so a
system written on one would be a second georeference for the same model — and nothing here
reconciles two.

**A model which pins no URL, or whose frames disagree about the linear unit, is a refusal**:
`derived` false, no files, the digest written, and the reason on stderr. The second is a
refusal over something correct — a survey grid in metres beside a fabrication grid in
millimetres is an ordinary model — because an exchange file states one set of units and
nothing here could choose between them.

**A model authored in feet is written in feet.** The unit the frames agree on is the unit the
file states, whichever it is: the four metric spellings are an `IfcSIUnit` with the prefix
each carries, and either foot is an `IfcConversionBasedUnit` stating its factor over the
metre — `0.3048` for `ft` and `1200/3937` for `usft`, the second to the whole of its `float64`
because it does not terminate in decimal. Length, area and volume are a conversion each,
distinguished by the dimensional exponent the quantity has; the plane angle stays an
`IfcSIUnit` in radians. The two feet are written under names which tell them apart — `foot`
and `US survey foot`, and the square and cube of each — because a reader keying off the name
rather than the factor holds its own table, and the tables in the wild have one entry for
`foot` at 0.3048 and none at all for the survey foot: a model read that way lands four feet
out at a state plane false easting.

**Nothing is converted.** The factor is named beside the coordinates and never applied to
them, which is what [0005](decisions/0005-one-linear-unit-per-frame.md) means by conversion
happening at an export boundary — a value written in the source is the value in the file.
Converting instead would round every coordinate of a model in survey feet, and the file would
stop carrying the numbers the surveyor published.

**A shape which was asked for and cannot be drawn is a refusal too**, and for the same reason
an artefact is all or nothing: a file with one room's solid quietly missing is worse than no
file. A boundary which does not close, a corner nothing states the position of, a boundary
lying in a plane which is not level, two equally current heights, a height which is not a
distance, one written in another unit than the boundary, and one which is nought or less are
each named on stderr against the claim or the corner which caused it. A height of nought is
refused rather than drawn flat because the depth a profile is swept through is a positive
length measure and there is no zero-height solid.

**Nothing here reads a clock.** `IfcOwnerHistory` is absent throughout, which is what removes
the only mandatory creation time in the schema, and the part 21 header's time stamp is the
derivation epoch. Two exports of an unchanged tree are the same bytes, so the second reports
`unchanged` and writes nothing.

### `export-map`

The model's regions, written as a GML 3.2 document a GIS opens with the project's coordinate
reference system already attached. It is an [artefact
command](#the-shape-every-artefact-command-reports) and writes that shape, plus the same one
field of its own that [`export`](#export) does.

```json
{
  "version": 2,
  "command": "export-map",
  "derived": true,
  "digest": "9f2c1ab4c0d7e5f38a2b6109d4e7c8b5a3f10e29d6c4b8a70f5312cd9e846b7a",
  "schema": "GML 3.2.1",
  "chord": { "name": "facet", "value": 0.1, "unit": "m" },
  "deviation": { "value": 0.0416, "unit": "m" },
  "files": [
    {
      "path": ".dfcad/export/9f2c1ab4c0d7e5f38a2b6109d4e7c8b5a3f10e29d6c4b8a70f5312cd9e846b7a/model.gml",
      "status": "written"
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `schema` | string, optional | The version of GML the document conforms to: `GML 3.2.1`. Absent on a refusal, because nothing was written in any version. |
| `chord` | object, optional | The tolerance the document's curves were drawn to: `name`, `value` and `unit`. Of the document rather than of any feature in it. Absent for a run which drew nothing. |
| `deviation.value` | number | How far the worst segment of the worst feature actually falls from the curve it stands in for. Absent wherever `chord` is absent, and absent on its own wherever `chorded` is written. |
| `deviation.unit` | string, optional | The unit the chord tolerance is declared in, which is the frame's: a tolerance in any other unit refuses the drawing outright, so every region in a document which was written shares this one. |
| `chorded[].edge` | string | An edge of a drawn region which states a curve this run did not read, each edge once however many features reach it. Absent for a run which read every curve and for a model which claims none. |
| `chorded[].predicates` | array | The predicates that edge states a position under, which is what to name to have the curve read. |
| `chorded[].span` | object | Where that edge was written. |
| `undrawn[].node` | string | A node the model gives a shape to — one with a boundary, or one drawn as a point — which is not a feature of the document, in id order. Absent where every such node was drawn. |
| `undrawn[].label`, `undrawn[].kind`, `undrawn[].type` | string, optional | What that node is called and what it is, each absent where the node has none. |
| `undrawn[].reason` | string | Why: `unreadable-boundary` for edges this run could not read, `no-position` for a point nothing places, `unrooted` for a model whose frames reach no root, `uncarried` for a frame the chain does not relate to the root, `not-level` for corners which do not lie at one level. The first two are [`plan`](#plan)'s words for the same findings. The detail is the diagnostic behind it in [`diagnostics`](#diagnostics), whose `nodes` names this node and whose `ids` names the shapes it is about. |

Everything else — `derived`, `digest`, `files[]` — is the shared shape, with the meanings
documented there. There is no `identifiers`: this format derives no identifier of the
project's, and the id of every node written is a property of the feature it was written as.

#### The feature properties

The document itself is not JSON, but its property names are as much a contract as the fields
above: they are what a downstream style rule, definition query or layer filter is written
against. A feature carries these, in this order and where the node has them, in the
application namespace `https://github.com/z5labs/dfcad/gml/1`, which the document binds to the
prefix `dfcad`:

| Property | Meaning |
|----------|---------|
| `dfcad:id` | The id of the node the feature was drawn from, in the model's own spelling. On every feature, and the join back to the model. |
| `dfcad:label` | What that node is called, where it has a label. |
| `dfcad:kind` | The kind the model gives it. |
| `dfcad:type` | The type the project declared it as, exactly as the registry wrote it. |
| `dfcad:within` | The id of the node which **immediately** contains it. |
| `dfcad:frame` | The id of the frame its outline was declared in, which is not the frame its coordinates are in unless the two are the same one. |

Only `dfcad:id` is written on every feature. The rest are written where the node has them and
are absent — not empty — where it does not, which is what makes a filter on one of them mean
"has this value" rather than "has this value or nothing". The id is a property rather than the
feature's `gml:id` because an id in this model's spelling is not a name XML can write; the
`gml:id` is an ordinal, and it identifies an element of one document rather than a thing in the
model.

**`dfcad:within` is the immediate container and never an ancestor.** A room reports the storey
it sits on and not the site that storey stands on, because it is the model's own containment
edge written out. Asking what a whole site holds is a join the reader makes — follow `within`
from feature to feature until it names nothing the document holds — or one
`dfcad traverse contains <id>` against the model, whose answer comes back in the same ids
`dfcad:id` carries.

**`export-map` takes no selector, and the properties above are how a run is narrowed after the
fact.** Every node the model gave a shape to is drawn, so a document of a model holding rooms,
countertops and closets holds all three stacked on the parcel. Choosing what a sheet shows is
the reader's
([0025](./decisions/0025-the-map-export-draws-every-region-and-its-properties-are-the-filter.md)):
a selector on the command would make the source digest an incomplete key for the artefact, and
two runs over one tree selecting differently would disagree about what belongs at the one path
that digest names.

**The namespace resolves to nothing and there is no `xsi:schemaLocation`, deliberately.** A
namespace URI identifies a vocabulary rather than a document to fetch, and the version at the
end of this one is what moves if a property here changes meaning. There is no `.xsd` to publish
and none is pointed at; GDAL infers the schema from the instance and everything downstream of it
goes through GDAL. A reader which insists on resolving a schema before it will open a document
refuses this one
([0023](./decisions/0023-the-map-export-names-its-coordinate-system-in-the-file.md)).

**Every node the model gave a shape to is drawn in the shape it was given.** A node bounded by
rings is a `gml:MultiSurface`; one whose declared geometry is `point` is a `gml:MultiPoint` at
the position claimed of it; one whose declared geometry is `line` is a `gml:MultiCurve` holding
one `gml:LineString` per loop of its boundary, the corners of the run in the order the loop
walks them and left open. A trench, a fence, a railing and a partition drawn as its centreline
cover nothing and are still on the map. A run is read, carried and judged for level exactly as a
ring is, and a curved edge in one is drawn to `chord` or listed under `chorded` exactly as an
edge of a ring is.

**Nothing the model shaped leaves the document without a word.** The features and `undrawn`
account between them for every node with a shape which has not been retired, so a reader who
finds a thing missing from the layer finds it named in the answer. A node named under `undrawn`
is an error on stderr as well, and a run which names one writes no file — a layer with one plot
quietly missing looks exactly like land nobody has claimed.

**`chord` and `deviation` are here because the file carries neither.** A GML document is
positions, so a reader holding one cannot tell a ring which follows its curve to a tenth of a
metre from one drawn coarsely — and a map is drawn once and read for years. They are what a
downstream check reads to assert that the layer it was handed was drawn to the tolerance it
intended.

**A feature drawn straight through a curve nothing read is a boundary in the wrong place**, in
a file somebody keeps. A run which did not name `--arc-centre` and `--arc-through` over a model
whose edges claim curves writes `chorded` and no `deviation` at all, for the reason
[`tessellate`](#tessellate) does: a deviation of nothing beside a named `chord` would be this
command saying the boundary is in the right place. `chorded` is written on a refusal too — a
curve nothing read is a fact about the model rather than about the file, and a run which
refused for some other reason has that wrong with it as well.

**The default destination is the same directory `export` writes into**, keyed by the same
digest, so the artefacts of one revision sit together and `.dfcad` remains a thing which can
be deleted whole.

**The vocabulary an outline is read under is required rather than optional**, which is the one
way this command's invocation differs from `export`'s. `--position`, `--tolerance` and
`--chord` go together and a run naming none of them is exit `3`. A spatial structure with no
shape in it is a correct IFC file; a vector layer with no shape in it is a file with nothing
in it at all.

**Every feature is expressed in the coordinates of the frame the chain is rooted at.** A
region outlined on another frame is carried there by the chain of measured transforms the
model already states — the same arithmetic [`site`](#site) does across frames — and a chain
which does not reach is a refusal rather than a feature written where it was drawn. Nothing
reprojects: the identifier is carried and never read, so the coordinates in the document are
the model's own
([0023](./decisions/0023-the-map-export-names-its-coordinate-system-in-the-file.md)).

**A coordinate is the model's own, which is not the same as being the digits somebody typed.**
A corner authored on the root frame reaches the file as it was written. One carried across a
frame by a measured transform, or drawn along an arc, has floating-point arithmetic done to it
on the way, so `2000000.0` can be written `1999999.9999999995` — about 5e-10 survey feet, which
is the last bits of a double and not a reprojection. Determinism is over two runs of one tree,
which are byte-identical; a check comparing this file against a surveyed figure compares to a
tolerance rather than as text.

**A run naming no `--crs` still writes the file, and warns.** The document then carries no
`srsName`, which is a layer a reader has to be told the system of out of band. It is a warning
rather than a refusal because a model nobody has sited is one somebody is still working on,
and exit `0` because the file is what was asked for.

**The elevation is dropped and a boundary which is not level is refused.** A map is a plan, so
each position is two ordinates, easting then northing; a boundary whose corners do not lie at
one level in the root frame has no plan, and the projection which would give it one is not
this command's to choose. The level is judged after the carry, because a transform between
frames preserves the plane a region lies in but not which plane that is.

### Diagnostics and the exit code of a read

Every command which reads the model renders the diagnostics its load reported, in full, on
stderr. What an **error** among them does to the rest of the run depends on what the command
answers, and there are two answers.

**A discovery read answers through it, and says that it did.** The listings, `get`,
`traverse`, `claims` and `conflicts` exit `0` whenever they answered, whatever the model's
diagnostics say, and carry `"refused": true` in the object they write where the load refused
the model — the same field, meaning the same thing, as `check`'s. Over a model which loads it
is `false`; it is written on every run so that a caller can read it without asking first
whether it is there.

A listing says what a model holds, and a node whose containment does not resolve is still a
node the model holds. Whether the model is *sound* is what `dfcad check` answers; answering
it in two commands, with two definitions of sound, is how the two come to disagree. It also
keeps discovery usable on a model somebody is halfway through writing, which is the model
discovery is most needed on. What a caller acting on the listing is owed is the difference
between a listing of a model which loads and one of a model which does not, and an exit code
of `0` on both cannot carry it — so the object does.

**Everything else exits `2`, as `check` does.** `resolve`, `route`, `measure`, `tessellate`,
`buildable`, `site` and `plan` are derivations: each computes an answer *out of* the model —
the value a predicate resolves to, the file a node would be written to, an area, a region, a
fit, a sheet — rather than reporting what the model holds. A figure computed out of a model the
load refused is, in `check`'s words, answering a question nobody asked: a caller reading the
exit code of `measure` could not tell it from a figure over a model which loads, and would
carry it on. So each of them is a **load failure** — exit `2` — on any tree whose load reports
an error, whether or not that error is anywhere near the subject it was asked about. The
exports, `review` and every write command are too, for the same reason. What each writes on
stdout is [the refusal](#the-refusal): the envelope, `"refused": true` and the diagnostics,
and none of the answer's own fields, so a caller reads why from the object as a discovery
read's caller does, and cannot read a figure out of it.

The line is drawn by what the command answers and not by which error the load reported. An
error a derivation could be shown not to depend on — a frame declared in a namespace nothing
declares, say, which leaves the frame usable — is still an error in a model `check` refuses,
and a derivation that answered through the errors it judged harmless would be a second
definition of sound, which is what the paragraph above says there must not be.
