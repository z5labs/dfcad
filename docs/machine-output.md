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

Stdout carries a result object exactly when the run produced a result. A run that produced
none writes nothing at all to stdout:

- help
- a usage error — no subcommand, an unknown one, a malformed flag, an unknown `--format`
- a load failure that stopped the run before it began, such as a model root that is not
  there or is not a directory

A run that *ran* and found something wrong did produce a result. A file that does not parse
and a file that is not in canonical form are both reported in the object on stdout, with a
non-zero exit code beside it.

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

## Exit codes

| Code | Meaning | Stdout |
|------|---------|--------|
| 0 | Success. The command did what was asked. | The result object, or empty for help. |
| 1 | Check failure. It ran and answered, and the answer is no. | The result object. |
| 2 | Load failure. Input could not be read, did not parse, or was not written. | The result object, or empty when nothing could be loaded at all. |
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

The descriptions are asked for rather than given. They are prose about the vocabulary rather
than about this model, they grow with the registry rather than with the model, and this is
the call every cold start begins with — so whoever is deciding which type to ask about next
paid for them on every run and read them on almost none. The measurement is in
[`token-budget.md`](./token-budget.md) and the reasoning in
[0017](./decisions/0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md).

The classifications are asked for rather than given, for a related reason and a different
reader: the caller which needs them is one mapping this model into a foreign schema, and it
asks once. A model which declares none pays nothing for the flag either way.

### `list-instances`

The instances of one type, or of the whole model. It takes an optional type argument and
two filters.

| Flag | Meaning |
|------|---------|
| `--kind <kind>` | Only instances that declare this kind. |
| `--frame <id>` | Only instances that declare this coordinate frame. |
| `--retired` | Include the instances that stopped existing. |

Filters combine: an instance is listed when it satisfies every filter given. Flags and the
type argument may be written in either order.

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
| `instances` | array | One entry per instance that satisfied every filter, in id order. Empty rather than null when nothing did. |
| `instances[].id` | string | The id the model holds it under. |
| `instances[].label` | string, optional | Its name for a person reading it. Absent when it was not written. |
| `instances[].type` | string | The type it declares, reported whether or not a type was filtered on. It need not be one the registry declares: a node naming an undeclared type is a diagnostic and is still a node of the type it named. |
| `instances[].kind` | string | The kind it declares, reported whether or not a kind was filtered on. |
| `instances[].frame` | string, optional | The coordinate frame it is expressed in. Absent when it declares none. |
| `instances[].retired` | boolean, optional | Whether the thing it names stopped existing. Absent on a node that did not, so a listing that was not asked for the retired ones holds nothing else. |

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
list either.

### `list-geometry`

The geometric nodes — vertices, edges and loops — which carry a claim under one predicate.
It takes no arguments and two flags.

| Flag | Meaning |
|------|---------|
| `--predicate <name>` | The predicate the node carries. **Required**, and it has no default. |
| `--family <family>` | Only nodes of this family: `vertex`, `edge` or `loop`. |

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
| `predicate` | string | The predicate the nodes below carry, which is the one asked for. It travels with the answer because an empty list means nothing without it. |
| `nodes` | array | One entry per geometric node carrying a live claim under that predicate, in id order. Empty rather than null when nothing does. |
| `nodes[].id` | string | The id the model holds it under, which is what every other command takes. |
| `nodes[].family` | string | Which family holds it: `vertex`, `edge` or `loop`. Reported whether or not a family was filtered on, so a listing read whole is readable on its own. |
| `nodes[].label` | string, optional | Its name for a person reading it. Absent when it was not written, which is the ordinary case for geometry. |
| `nodes[].frame` | string, optional | The coordinate frame it is expressed in. Absent only when it declares none, which is a diagnostic rather than an ordinary node. |
| `nodes[].start` | string, optional | The vertex an edge runs **from**. Absent for a vertex and for a loop. |
| `nodes[].end` | string, optional | The vertex an edge runs **to**. Absent for a vertex and for a loop. |
| `nodes[].span` | string | Where it was written, as `path:line:col-line:col`. |

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
predicates that are declared, and so is a `--family` which is none of the three. A predicate
nobody declared and a predicate nothing is written under are different answers, and a caller
that cannot tell them apart retries a misspelling forever.

### `get`

One thing, by its id, with the claims written on it. It takes one id argument and three
flags.

| Flag | Meaning |
|------|---------|
| `--claims <how>` | `full` (default), every claim written on it, or `resolved`, the current claim under each predicate. |
| `--deprecated` | Include the claims that have been deprecated. Refused beside `--claims resolved`. |
| `--observations` | Read the observation files it links to and inline the records. Without it, the files are named and not opened. |

An id is unique across the whole model, so this is one command for both families. A vertex,
an edge and a loop are retrieved by the same call a semantic node is, and `family` says
which came back and so which of the fields to expect.

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
        "date": "2026-05-06",
        "rank": "normal",
        "span": "entities/site.dfc:30:3-36:25"
      }
    ]
  }
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `entity` | object | The thing the id named. |
| `entity.id` | string | The id the model holds it under, which is the id that was asked for. |
| `entity.family` | string | One of `node`, `vertex`, `edge`, `loop`. It says which of the fields below to expect. |
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
| `entity.observations` | array, optional | The observation files it links to, as paths relative to the model root, in the order it wrote them. Absent when it links to none. Producing this reads nothing. |
| `entity.observation-records` | array, optional | The records those files hold, written under `--observations` and absent otherwise. Empty rather than absent when the flag was given and the files hold no record, because "nobody has surveyed this" and "you did not ask" are different answers. |
| `entity.retired` | object, optional | How a semantic node stopped existing: `date`, `reason`, and `superseded-by` where something stands in its place. Absent for a node that was not retired. |
| `entity.span` | span | Where it was written: the file, and the line and column of both ends of the form. |
| `entity.claims` | array | The claims written on it, in predicate order and then by where each was written. Empty rather than null when nothing is claimed about it. |

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
| `claims[].date` | string, optional | The day the value was obtained, as a full date. |
| `claims[].rank` | string | `normal` or `deprecated`, reported whether or not it was written. |
| `claims[].superseded-by` | string, optional | The id of the claim that replaced this one. |
| `claims[].resolution` | string, optional | What the rule left this claim as: `current`, `tied` or `unranked`. Written under `--claims resolved` and absent otherwise, because under `--claims full` nothing has been resolved. |
| `claims[].span` | span | Where the claim was written. |

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

An id nothing in the model holds is a **usage error** — exit `3`, with nothing on stdout —
naming it, and naming the nearest id there is when one is close enough to be the id that
was meant. It is not an empty answer: a thing that is not there and a thing with nothing
said about it are different answers. An argument that is not a well-formed id is the same
exit code, reporting the rule it broke rather than a lookup that was never going to find
anything.

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
  "claim-id": "survey:A-0002",
  "claim": {
    "id": "survey:A-0002",
    "predicate": "area",
    "value": {"shape": "scalar", "unit": "m2", "scalar": 24.2},
    "source": "As-built check AB-2026-009, Acme Surveys",
    "method": "method:total-station",
    "accuracy": [{"kind": "independent", "magnitude": 0.05, "unit": "m2"}],
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
| `accuracy` | array, optional | How well the answer is known, term by term, as the claim it came from stated it. Absent where nothing resolved, and absent where the claim stated none — which makes the answer unrankable rather than exact, and is what `reason` says. |
| `claim-id` | string, optional | The id of the claim the answer came from. Absent where nothing resolved, and absent where the claim wrote no id, which is the great majority of them: an id is required only of a claim something references. |
| `frame` | string, optional | The coordinate frame the value is expressed in. Absent for a value that is not a position, which is in no frame. |
| `claim` | object, optional | The claim the answer came from, in the shape documented under `get`. Written under `--evidence` and absent otherwise. |
| `candidates` | array, optional | Claims that could still be the answer, each in that same full shape and marked with its `resolution`. |
| `budget` | object, optional | The accumulated error of a cross-frame answer, broken out by term. Written only where a frame transform was applied. |

`value`, `accuracy` and `claim-id` are the answer; `claim` is the audit trail. The split is
[0017. The answer is the default and the evidence is asked for](./decisions/0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md),
and it is drawn where it is because how good a number is belongs to the number, while who to
argue with about it is a separate question asked far less often. It is not a trim of what
this engine refuses to hand over: an answer never comes back as a bare figure, because
`accuracy` is beside it whenever the claim stated one.

The four outcomes and the four exit codes line up, because what a caller does about each is
different:

| Outcome | Exit | Carries |
|---------|------|---------|
| `resolved` | `0` | `value`, `accuracy` and `claim-id`, and `claim` under `--evidence`. `reason` is `only`, `accuracy` or `recency`. |
| `unranked` | `0` | The same. The one live claim under a predicate nothing rankable was said about: still what the model says, and not an answer the rule chose. There is no `accuracy`, which is why. |
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

An id nothing in the model holds is a **usage error** — exit `3`, with nothing on stdout —
naming it and the nearest id there is. That is a different answer from `unclaimed`, which
is the model answering that nobody has measured the thing; a caller that cannot tell them
apart retries a misspelling forever. A predicate no registry file declares, and a `--frame`
no registry file declares, are usage errors for the same reason.

#### `budget`

Written when `--frame` moved the answer between two frames, because the accuracy of such an
answer is not the accuracy of the claim it came from: the fits along the route are part of
what is known about it.

| Field | Type | Meaning |
|-------|------|---------|
| `budget.from` | string, optional | The frame the value was written in. Absent for a budget that is a computation rather than a route between two frames — `buildable` writes one. |
| `budget.to` | string, optional | The frame it was expressed in. Absent for the same reason. |
| `budget.terms[].kind` | string | `independent` or `systematic`, which is how it combines: independent terms in quadrature, systematic terms linearly. |
| `budget.terms[].name` | string | The id a systematic error is shared with, or the name of the claim an independent one came from. |
| `budget.terms[].magnitude` | number | The one-sigma figure, as it was written. |
| `budget.terms[].unit` | string | The unit that figure is expressed in. |
| `budget.terms[].source` | string, optional | The id a systematic error is shared with. Absent for an independent term. |
| `budget.terms[].contributors` | array | The claims that carried the term, each once. More than one is a shared term counted once. |
| `budget.combined` | object, optional | The terms reduced to one standard uncertainty: `magnitude`, `unit` and `coverage-factor`, which is `1` for everything the engine produces. |
| `budget.unknown` | array, optional | The claims the answer was computed from that stated no accuracy. One of them taints the whole budget, and `combined` is then absent: an unstated accuracy is unknown rather than zero. |
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
takes a query, an id, and three flags.

| Flag | Meaning |
|------|---------|
| `--depth <n>` | How many steps of the relation to follow: a count of one or more, or `all` to follow it as far as the model goes. Default `1`. |
| `--kind <kind>` | Only results that declare this kind. |
| `--type <name>` | Only results that declare this type. |

| Query | Answers | Relation |
|-------|---------|----------|
| `contains` | What the thing holds, level by level inward. | `containment` |
| `contained-by` | What holds the thing, outward towards the root. | `containment` |
| `members-of` | The zones the thing is a member of, and the zones those are members of where membership nests. | `membership` |
| `boundary-of` | The edges the thing's outline is assembled from, each classified by what physically realises it. | `boundary` |
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
      "via": ["geom:E-02"],
      "span": "entities/site.dfc:41:1-48:25"
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
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
| `results[].via` | array, optional | The ids of the edges an adjacent thing shares with the thing it was reached from, in the order that boundary traverses them. Written under `adjacent-to` and absent otherwise. |
| `results[].span` | span | Where it was written. |

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

Depth is bounded by default, because a traversal of a model nobody has read should not be
able to return the whole of it by accident; `--depth all` is how a caller asks for that on
purpose. Each thing is reported once, at the fewest steps it can be reached in, so a cycle in
the model terminates and something reachable two ways is one result rather than two.

A filter narrows what is reported and never what is walked. Every room three levels below a
site is still reached with `--kind Space`, though the building and the storey between them
are not reported.

Results come back in depth order and then in id order, so two runs over one model diff
against each other and moving a node between files does not move the answer. The edges of a
boundary are the exception: they come back in the order the loops traverse them, because
that order is the ring itself and is data rather than presentation.

`boundary-of` is one step from the thing it bounds, and its results are edges. `--depth`,
`--kind` and `--type` written beside it are **usage errors** rather than flags that are
quietly ignored, for the reason `--deprecated` beside `--claims resolved` is: a flag that is
silently dropped answers a different question from the one that was asked.

An id nothing in the model holds is a **usage error** — exit `3`, with nothing on stdout —
naming it and the nearest id there is, exactly as `get` reports one. An id that names a
vertex, an edge or a loop is a usage error too, naming which of them it is: the relations
above are written between semantic nodes, and a shape is reached through the node it bounds.
A walk that reaches nothing is not an error — it is an empty `results` and exit `0`.

### `claims`

Every claim written on one thing, live and retracted alike. It takes an id and an optional
predicate, and no flags of its own.

`get` answers what the model says about a thing now; `claims` answers everything anybody has
said about it and what became of each statement. Deprecated claims are therefore in the
answer rather than behind a flag, marked as retracted and carrying the id of the claim that
replaced them, so a retraction is followable forward without a second call.

```json
{
  "version": 2,
  "command": "claims",
  "subject": "site:S-101",
  "claims": [
    {
      "id": "survey:A-0001",
      "predicate": "area",
      "value": {"shape": "scalar", "unit": "m2", "scalar": 23.0},
      "source": "Plan set A-101, sheet 3",
      "method": "method:scaled-from-plan",
      "accuracy": [{"kind": "independent", "magnitude": 0.5, "unit": "m2"}],
      "date": "2026-01-09",
      "rank": "deprecated",
      "superseded-by": "survey:A-0002",
      "resolution": "retracted",
      "span": "entities/site.dfc:20:3-28:34"
    },
    {
      "id": "survey:A-0002",
      "predicate": "area",
      "value": {"shape": "scalar", "unit": "m2", "scalar": 24.2},
      "source": "As-built check AB-2026-009, Acme Surveys",
      "method": "method:total-station",
      "accuracy": [{"kind": "independent", "magnitude": 0.05, "unit": "m2"}],
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
| `subject` | string | The id the claims below are written on, which is the id that was asked for. |
| `claims` | array | Every claim written on it, in predicate order and then by where each was written. Empty rather than null when nothing is claimed about it. Each entry is the claim object `get` writes, documented above. |

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
answers. A predicate that *is* declared and that nothing on this subject is claimed under is
an empty list and exit `0`.

### `conflicts`

The conflict register: every subject and predicate pair the model states more than once,
with the competing claims and what resolution makes of them. It takes no arguments and four
filters.

| Flag | Meaning |
|------|---------|
| `--type <name>` | Only pairs whose subject declares this type. |
| `--predicate <name>` | Only pairs written under this predicate. |
| `--ambiguous` | Only pairs resolution cannot decide. |
| `--resolved` | Only pairs resolution can. |

Filters combine: a pair is listed when it satisfies every filter given. `--ambiguous` and
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
| `conflicts` | array | One entry per pair the model states more than once, ordered by subject and then by predicate. Empty rather than null when nothing disagrees. |
| `conflicts[].subject` | string | The id of the thing the competing claims are about. |
| `conflicts[].predicate` | string | The predicate they were written under. |
| `conflicts[].type` | string, optional | The type the subject declares, reported whether or not a type was filtered on. Absent for a vertex, an edge or a loop, which declare none. |
| `conflicts[].ambiguous` | boolean | Whether resolution picks nothing, so the disagreement has no answer. Exactly one of this and a claim marked `current` holds of every entry. |
| `conflicts[].current` | string, optional | The id of the claim resolution picks. Absent when nothing was picked, and also when the claim that was picked wrote no id of its own — the claim marked `current` below carries the span that names it instead. |
| `conflicts[].claims` | array | The competing claims, in the order they were written, each the claim object documented under `get` with the `resolution` field documented under `claims`. |

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
| `digest` | string, optional | The digest of the source tree the answer was computed against, lower-case hex, so a caller can check the computation against the tree in front of them. Written on a refusal too. Absent for a model which was not read from disk, or one a file of which could not be read at all. |
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
whether any corner stated no accuracy at all. Independent terms combine in quadrature and
systematic ones linearly, and a term reached through four corners appears once with all four
named under it ([0006](decisions/0006-accuracy-is-one-sigma.md)).

That budget grows with the shape while the figures do not — one term per corner — which makes
this the most expensive call on the dimensional path. What it costs, and what each part of it
costs, is measured in [the token budget](token-budget.md).

**Exit `1`** is a measurement which could not be made: a ring which does not close, corners
which are not in one plane, a ring which crosses itself, one whose corners are collinear, a
corner nothing states the position of, a tolerance the registry does not declare in the unit
of the frame. Each is its own diagnostic naming which mistake it is. The object still comes
back with `derived` false, so a caller reads why from the diagnostics on stderr rather than
from an empty stream.

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
| `digest` | string, optional | The digest of the source tree the drawing was derived from, lower-case hex, so a caller can check the drawing against the tree in front of them. Written on a refusal too. Absent for a model which was not read from disk, or one a file of which could not be read at all. |
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
The result object still comes back with `derived` false, so a caller reads why from the
diagnostics on stderr rather than from an empty stream.

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
| `digest` | string, optional | The digest of the source tree the region was derived from, lower-case hex, so a caller can check the derivation against the tree in front of them. Written on a refusal too. Absent for a model which was not read from disk, or one a file of which could not be read at all. |
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
| `region.boundary[].ring` | number | Which ring of the boundary a straight run belongs to, counted from zero in the order the rings are traversed. |
| `region.boundary[].edge` | string | The id of the edge that run was written as, or whose arc it stands in for. |
| `region.boundary[].origin` | string | What produced the run: `edge` where it is the edge itself, corner to corner as it was written, and `arc` where it is one chord of the drawing of the arc that edge bends along. |
| `region.boundary[].reversed` | bool | Whether the run goes against the order the edge was written. |
| `region.boundary[].from` | array | The corner the run leaves, as its components. |
| `region.boundary[].to` | array | The corner it arrives at. |
| `budget` | object, optional | The accuracy of the answer broken out by term, over the position claims and the setback claims together. Same shape as [`budget`](#budget), without `from` and `to`. |

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
`derived` false, so a caller reads why from the diagnostics on stderr rather than from an
empty stream.

Setbacks that meet in the middle are **exit `0`**: `derived` is true, `region.empty` is true,
and a warning on stderr says which parcel its own regime consumed. That is the answer to the
question rather than a failure to answer it, and it is reported so that an empty region cannot
be read as one that was never computed. What never comes back is the inside-out shape
offsetting each edge on its own produces when the offsets cross over each other.

### `site`

Whether one thing fits inside another, across whatever frames the two are declared in, and
how well that answer is known. It takes the id of the subject and four flags.

| Flag | Meaning |
|------|---------|
| `--within <id>` | The thing the subject has to sit inside. Required. |
| `--position <predicate>` | The predicate a corner's position is claimed under, which both outlines are read from. Required. |
| `--tolerance <name>` | The tolerance corners are judged coincident against and rounded corners are drawn to. Required. |
| `--clearance <distance>` | How much room the subject has to keep between itself and the envelope's boundary, in the linear unit of the envelope's frame. Default `0`, which is "inside it at all". |

The subject is read out of the corners surveyed in its own frame, carried into the envelope's
frame across the transform claims which relate the two, grown by the required clearance,
overlaid on the envelope and measured. Every step accumulates the accuracy of what it read
([0006](decisions/0006-accuracy-is-one-sigma.md)), so the clearance and its error bar are two
halves of one answer. Nothing is written back
([0009](decisions/0009-derived-values-are-never-written-back.md)).

**The budget is the point.** The georeference is one transform applied to every fact declared
indoors, so its residual does not cancel between two indoor points and does not average away
against an outdoor one. Systematic terms add linearly and each is counted once however many
inputs contributed it — which matters most in exactly this query, because a control point
behind the interior corners is routinely behind the boundary survey and the georeference as
well. Combining everything in quadrature reports a narrower answer than the evidence
supports, which is the direction nobody investigates.
[The worked example](siting-worked-example.md) runs one query end to end, from the claims
involved to the final budget.

| Field | Type | Meaning |
|-------|------|---------|
| `subject` | string | The id which was sited. |
| `within` | string | The id it had to sit inside. |
| `sited` | bool | Whether there is an answer below. Written whatever the outcome, so a subject which does not fit (`sited` true, `verdict` `does-not-fit`) reads differently from a question which could not be asked (`sited` false). |
| `digest` | string, optional | The digest of the source tree the answer was computed against, lower-case hex, so a caller can check the computation against the tree in front of them. Written on a refusal too. Absent for a model which was not read from disk, or one a file of which could not be read at all. |
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
| `budget` | object, optional | The accuracy of the answer broken out by term, over the position claims behind both outlines and the transform claims of every frame the subject was carried through. Same shape as [`budget`](#budget), without `from` and `to`. |

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
a clearance written as a distance outwards. The object still comes back with `sited` false.

### `plan`

What a spatial node contains, as rings, with the claims written on them. It takes the id of
the thing to plan and three flags — none of which has a default.

| Flag | Meaning |
|------|---------|
| `--annotate <predicate>` | A predicate whose claims are reported on every ring and on the edges bounding it. Repeatable, and at least one is required. |
| `--position <predicate>` | The predicate a corner's position is claimed under, which the rings are read from. Required. |
| `--tolerance <name>` | The tolerance corners are judged coincident against. Required. |

**This is a query and not an export.** It writes no file
([0022](decisions/0022-a-command-whose-product-is-a-file-answers-on-stdout.md) is about the
commands that do). It returns the rings the model already holds and the claims already
written on the edges bounding them, under the same envelope, digest and budget every other
answer carries, and it knows nothing about paper, scale, title blocks, text height or where a
leader goes. Those are the consumer's, and this command is the boundary that keeps them so.

**`--annotate` is the whole of the answer to "is this dimension worth drawing".** It is worth
drawing if the caller asked for that predicate. Nothing else in the payload encodes a drawing
judgement, which is what keeps the engine from acquiring a drawing convention every consuming
project would then disagree with — the same rule that keeps domain vocabulary out of it
([0010](decisions/0010-the-engine-carries-no-domain-vocabulary.md)).

| Field | Type | Meaning |
|-------|------|---------|
| `subject` | string | The id the plan was asked about. |
| `planned` | bool | Whether every ring below could be read. Written whatever the outcome, so a storey nobody has outlined yet (`planned` true, `outlines` empty) reads differently from one a room of which could not be read (`planned` false). |
| `digest` | string, optional | The digest of the source tree the rings and the claims were read from, lower-case hex, so a consumer can say which model a sheet was drawn from. Written on a refusal too. Absent for a model that was not read from disk. |
| `frame` | string, optional | The coordinate frame the subject is declared in. |
| `unit` | string, optional | That frame's linear unit. Every coordinate here is in it and every area in the square of it. |
| `tolerance` | object, optional | The tolerance corners were judged coincident against: `name`, `value` and `unit`. |
| `annotating` | array | The predicates the run asked for, in the order it named them and with a repeat written once. |
| `outlines` | array | One entry per contained node that was drawn, in id order. Empty rather than null for a subject that contains nothing drawable. |
| `outlines[].node` | string | The id of the node the rings were read from, which is what names them. |
| `outlines[].label` | string, optional | What it is called. Absent where it is called nothing. |
| `outlines[].kind` | string, optional | The kind it declares. |
| `outlines[].type` | string, optional | The type it declares. |
| `outlines[].region` | object | The area it covers, with `area`, `empty`, `pieces` and `boundary` exactly as [`buildable`](#buildable) writes them. |
| `outlines[].annotations` | array | The claims reported on it, the node's own first and then those of each edge of its boundary. Empty rather than null for a room nobody has written anything on. |
| `outlines[].annotations[].anchor.kind` | string | Which family the claim is written on: `edge` for one written on an edge of a ring, `node` for one written on the node that ring bounds. |
| `outlines[].annotations[].anchor.id` | string | The id of that edge or that node. |
| `outlines[].annotations[].anchor.vertices` | array, optional | The edge's two corners, in the order the edge was authored. Absent for a node anchor. |
| `outlines[].annotations[].anchor.rings` | array, optional | The loops bounding the node, in the order it references them. Absent for an edge anchor. |
| `undrawn` | array, optional | One entry per contained node that was **not** drawn, in id order. Absent for a subject every node of which was drawn — a key a consumer has to read to learn nothing is one that should not be there. |
| `undrawn[].node` | string | The id of the node that was not drawn. |
| `undrawn[].label` | string, optional | What it is called. Absent where it is called nothing. |
| `undrawn[].kind` | string, optional | The kind it declares. |
| `undrawn[].type` | string, optional | The type it declares. |
| `undrawn[].reason` | string | Why it was not drawn: `no-boundary` for a node that references no loop, `unreadable-boundary` for one whose loops this run could not read. A closed set of two. |
| `undrawn[].annotations` | array | The claims reported on it, in the same order and the same shape as an outline's. Empty rather than null. A node that references no loop has no edges, so what it carries is exactly its own claims and no edge anchors. |
| `budget` | object, optional | The accuracy of the rings, over the position claims that put every drawn corner where it is, and over the rings that were **drawn** — a ring that was refused put no corner anywhere. Same shape as [`budget`](#budget), without `from` and `to`. Absent where there is nothing to report — no terms, no combined figure and no reason for there being none — because an object carrying neither the figure nor a reason for its absence reads as an answer known exactly. |

Beside `anchor`, every annotation carries the claim object `get` writes — `id`, `predicate`,
`value`, `source`, `method`, `accuracy`, `date`, `rank` and `span` — so a claim on a plan
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

Which nodes are drawn is every descendant of the subject that references at least one loop
this run could read, however deep: a room inside a storey and an alcove inside that room are
both places somebody draws. The subject itself is not drawn — the question is what is in it.

**Nothing the subject contains is dropped.** Every descendant that was not drawn comes back
under `undrawn`, named, with what it is, why it was not drawn and the claims written on it —
so `outlines` and `undrawn` account between them for every node the subject holds, and a
renderer that drew every outline and listed every undrawn node has drawn or named the whole
storey. A circuit group has no edges and is ordinary; a ring that does not close is a defect;
both are things somebody put inside that storey. This is the one place the payload could omit
an authored fact without saying so, and the failure it would cause has no downstream symptom
at all: the sheet renders, looks complete, and is missing a door.

**The reason is a token and the detail is a diagnostic.** `reason` says which of the two
applies, because that is what decides whether anybody has to act — nobody fixes a circuit
group, somebody fixes a ring that will not close — and a consumer deciding that should read a
field rather than match prose. Where the reason is a defect, the diagnostics on stderr carry
the loop, the file, the position and the size of the gap, which is where anything an author
acts on belongs; a second copy of it on stdout would be a second thing to keep true.

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
declare in the frame's unit. The other rooms are still drawn and the object still comes back
with `planned` false and the room named under `undrawn`, because a sheet with one room missing
is more use than no sheet and the diagnostics on stderr say which room to fix.

### `check`

Every rule the model states, run: each type's invariants bound to each of its instances, and
each assertion written on a thing. It is the gate — one command, one exit code, and a report
naming what failed and where the rule that failed it is written. It takes no arguments and
four flags.

| Flag | Meaning |
|------|---------|
| `--subject <id>` | Only the rules bound to this thing. Repeatable. |
| `--type <name>` | Only the rules bound to instances of this type. Repeatable. |
| `--check <name>` | Only the rules naming this check. Repeatable. |
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
  "summary": {"checks": 7, "runnable": 6, "ran": 6, "passed": 4, "failed": 2},
  "violations": [
    {
      "instance": "site:S-102",
      "type": "MeetingRoom",
      "check": "required-claim",
      "arguments": ["(predicate width)"],
      "declared": "registry.dfc:37:3-37:40",
      "subject": "entities/site.dfc:29:1-34:24",
      "message": "expected a claim under width on the subject, found none",
      "hint": "the type requires one of every instance; write the claim, or take the invariant off the type"
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
| `violations` | array | One entry per way a rule was not satisfied, in the order the rules ran. Empty rather than null when nothing failed. |
| `violations[].instance` | string | The id of the thing that failed. |
| `violations[].type` | string, optional | The type that declared the rule. Absent for an assertion, which is declared on the thing itself. |
| `violations[].check` | string | The check name the rule names. |
| `violations[].arguments` | array, optional | The parameters it ran with, each rendered as it was written — the tolerance it was measured against among them. |
| `violations[].declared` | span | Where the rule is written: a registry file for an invariant, the thing itself for an assertion. |
| `violations[].subject` | span | Where what failed is written: the thing, or the part of it the check pointed at. |
| `violations[].message` | string | What was expected and what was found. |
| `violations[].hint` | string, optional | What to do about it. |
| `violations[].related` | array, optional | The other places that explain this one, each a span and a message. Where the rule was declared is not among them; `declared` is. |

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
`assertion`, the `type` that declared it where one did, the `check` name, its `arguments`,
its `declared` span, and two booleans: `runs`, which says whether running it would decide
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

Rules run in a deterministic order and are reported in it: every invariant, node by node in
the order the model was read, and then every assertion, thing by thing. Two runs over one
model produce byte-identical stdout.

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

Findings are ordered by check, in the order the table above lists them, then by subject, then
by position. Two runs over the same pair of revisions produce byte-identical stdout, so a
diff between two runs is about what changed in the branch.

| Code | When |
|------|------|
| `0` | Nothing the policy ruled a failure. A revision which changed nothing suspicious, and one whose every finding was warned about or acknowledged, both land here. |
| `1` | At least one finding the policy ruled a failure. Every finding is in the result. |
| `2` | A revision could not be read: the model root is not inside a git working tree, the branch does not exist, the checkout is too shallow to reach the merge base, or the merge base itself does not load. A review needs both revisions, and half a comparison would report every id in the half it did not read as an id which disappeared. |
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
| `2` | The change was refused because the resulting model would not load, the tree did not load to begin with, the model root is held by another transaction, or a file could not be written. |
| `3` | The invocation itself was wrong. |

A refused change writes nothing at all, and its diagnostics are the ones a load of the
result would have raised — every independent problem, each with its position, rather than
the first. Because the model is unchanged, the correct response to a refusal is to fix the
command and reissue it: there is no partial state to inspect and nothing to reconcile.

A refused change also writes nothing to **stdout**. It produced no result, and an object
describing a change that did not happen reads exactly like one describing a change that did.

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
interpreted — so nothing reaches stdout, the diagnostics on stderr are the whole of the
answer, and the exit code is the load failure one. They are the diagnostics a load of the
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
| `rankable` | boolean | Whether the claim that was written can take part in resolution, which is whether it carries an accuracy. |
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
| `unrankable` | The claim carries no accuracy. It loads, it can never win resolution, and it is not given a default. |
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

Exit codes are the ones every write command has, with the operation file reading as input:

| Code | When |
|------|------|
| `0` | The batch was applied, or, under `--dry-run`, would have been. |
| `2` | The operation file could not be read or is not a batch; or the change was refused because the resulting model would not load, the tree did not load to begin with, or a file could not be written. |
| `3` | The invocation was wrong, or an operation of the batch was: an id something already holds, a type nothing declares, a value of the wrong shape. It is the code the same mistake gets from the command that makes the change on its own. |

A refused batch writes nothing at all and nothing reaches stdout, whichever of the three
passes refused it. What is wrong with the *file* is reported in full — every operation that
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
| `digest` | string, optional | The digest of the source tree the artefact was derived from, lower-case hex, so a caller can check the artefact against the tree in front of them. Written on a refusal too. Absent for a model which was not read from disk, or one a file of which could not be read at all. |
| `files` | array | One entry per file the artefact consists of, ascending by `path` compared byte-wise. Empty rather than null when nothing was written. |
| `files[].path` | string | Where the file is, exactly as it would be opened. An artefact under the build directory is written beneath a directory named for the key it was produced under ([0021](./decisions/0021-an-export-is-a-build-output-keyed-by-its-source-digest.md)), which is a path a caller cannot predict — so this field is how what was just produced is found. |
| `files[].status` | string | One of `written`, `unchanged`. |
| `identifiers` | array, optional | Written only under `--evidence`. One entry per rooted object, ascending by `id`, each a node `id` and the `global-id` derived for it ([0004](./decisions/0004-globalid-derives-from-a-pinned-namespace.md)). It is left out by default because it grows one entry per node and because every entry is recomputable exactly from the model a caller already has ([0017](./decisions/0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md)). |

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
| `1` | The artefact could not be produced from the model that was read. `derived` false, `files` empty, and `digest` written, so a caller reads why from the diagnostics on stderr rather than from an empty stream. |
| `2` | The model could not be read: the root is not there, the tree did not load, or a file of it could not be read. |
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

Everything else — `derived`, `digest`, `files[]`, `identifiers` under `--evidence` — is the
shared shape, with the meanings documented there.

**The spatial structure crosses the boundary always, and the geometry only when the run says
what to read it under.**
The project, and every node whose kind is `Site`, `Building`, `Storey` or `Space`, is written
as the IFC entity that kind is — the two vocabularies are one for one — nested by `within`
through `IfcRelAggregates`, each with a local placement relative to its parent's. A node whose
kind is `Zone` is written as an `IfcZone` with its `member-of` members assigned through
`IfcRelAssignsToGroup`. A node whose kind is `Element` or `Interface` is contained in the
nearest spatial ancestor it has, through `IfcRelContainedInSpatialStructure`.

**What an element is written as comes from its type's classification, and the fallback is a
proxy.** A type declaring `(classification "IFC4" "IfcWall")` puts its instances in the file as
`IFCWALL`. A type declaring nothing under that system — or naming an entity this writer has no
attribute list for — puts them in as `IFCBUILDINGELEMENTPROXY` with the type's own name in
`ObjectType`, which is what the IFC specification blesses that entity for. Classifications
under any other system are carried by the model and read by nobody here: they name the thing in
somebody else's vocabulary rather than naming an entity in this file's.

**The set of entities a classification may name is closed, and it is this:**

```
IfcBeam                  IfcFooting            IfcRoof
IfcBuildingElementProxy  IfcFurnishingElement  IfcSlab
IfcColumn                IfcMember             IfcStair
IfcCovering              IfcPlate              IfcWall
IfcCurtainWall           IfcRailing            IfcWindow
IfcDoor                  IfcRamp
```

A registry is authored against that list rather than against the writer's source. The set is
what the writer holds a transcribed IFC4 attribute list for, and an entity outside it is
absent because IFC4 gives it a different attribute list — an instance written with the wrong
number of attributes is a file no reader loads, so the answer is a proxy and a report rather
than a guess.

**A classification the writer cannot carry is reported, not silently proxied.** Every node
whose type declared a code outside that set is named in `classifications[]`, with the code it
declared, and a warning naming the *type* — one per type, pointing at the line of the registry
which declared it — goes to stderr. The node-level answer and the type-level diagnostic are two
granularities on purpose: a node is what a caller holding the file has in front of it, and a
type is what would be edited to fix it, so a model with nineteen doors of one type is told once
about the type and lists nineteen nodes.

**The two mistakes are told apart, because their fixes differ.** A `reason` of `unwritten`
means IFC4 defines a product entity of that name and this writer has no attribute list for it
— `IfcPile`, `IfcOutlet`, the deprecated `IfcWallStandardCase`. The classification is right,
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

**Each claim behind a body travels into the file beside it**, as an `IfcPropertySet` named
`dfcad_HeightProvenance` or `dfcad_ThicknessProvenance` attached through
`IfcRelDefinesByProperties`. Each carries the predicate, the figure and its unit, and whatever
the claim states: its source, its method, its accuracy, its date, its id, and which step of
the resolution rule chose it. That last is how a surveyed height is told from an assumed one
without holding the model — a claim nothing rankable was said about reads as `unranked`, and
is still used, because it is what the model says. They are two sets rather than one because
they are two measurements: a wall's height may be surveyed and its thickness taken off a
drawing.

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
hold. The same goes for an edge backed by an element outside the spatial structure, which is
written nowhere for a relationship to point at. Both are warnings on stderr naming the space
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

The listings, `get`, `traverse`, `claims` and `conflicts` exit `0` whenever they answered,
whatever the model's diagnostics say. Those diagnostics are still rendered in full on stderr.

A listing says what a model holds, and a node whose containment does not resolve is still a
node the model holds. Whether the model is *sound* is what `dfcad check` answers; answering
it in two commands, with two definitions of sound, is how the two come to disagree. It also
keeps discovery usable on a model somebody is halfway through writing, which is the model
discovery is most needed on.
