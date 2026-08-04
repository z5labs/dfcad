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
  "version": 1,
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

## Exit codes

| Code | Meaning | Stdout |
|------|---------|--------|
| 0 | Success. The command did what was asked. | The result object, or empty for help. |
| 1 | Check failure. It ran and answered, and the answer is no. | The result object. |
| 2 | Load failure. Input could not be read, did not parse, or was not written. | The result object, or empty when nothing could be loaded at all. |
| 3 | Usage error. The invocation itself was wrong. | Empty. |

A caller can branch on the code alone, without reading a message. A check failure and a
broken invocation are different situations for a CI job — one says the model is wrong, the
other says the job is — and telling them apart must never mean matching prose.

## Global flags

Every subcommand takes these, and takes them identically.

| Flag | Default | Meaning |
|------|---------|---------|
| `--root <dir>` | `.` | The model root. A relative path argument is resolved against it; an absolute one is left alone. A root that is not a readable directory is a load failure. |
| `--format <fmt>` | `json` | How the run reports itself **to a person, on stderr**. See below. |
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

## Payloads

### `fmt`

```json
{
  "version": 1,
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
          "span": {
            "start": {"path": "site/b.dfc", "line": 1, "column": 7, "offset": 6},
            "end": {"path": "site/b.dfc", "line": 1, "column": 7, "offset": 6}
          },
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
before. It takes no arguments.

```json
{
  "version": 1,
  "command": "list-types",
  "types": [
    {
      "name": "Campus",
      "kinds": ["Zone"],
      "geometries": [],
      "absent": true,
      "description": "A group of things administered together, which has no shape.",
      "instances": 1
    },
    {
      "name": "MeetingRoom",
      "kinds": ["Space"],
      "geometries": ["area"],
      "absent": false,
      "description": "An enclosed room used for meetings.",
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
| `types[].absent` | boolean | Whether an instance may omit its geometry entirely. Absence is not a geometry form — a node with no geometry omits the child rather than naming one — so it is a field of its own rather than a member of `geometries`. |
| `types[].description` | string, optional | The one line the registry gives the type. Absent when it was not written. |
| `types[].instances` | integer | How many semantic nodes declare this type. |

### `list-instances`

The instances of one type, or of the whole model. It takes an optional type argument and
two filters.

| Flag | Meaning |
|------|---------|
| `--kind <kind>` | Only instances that declare this kind. |
| `--frame <id>` | Only instances that declare this coordinate frame. |

Filters combine: an instance is listed when it satisfies every filter given. Flags and the
type argument may be written in either order.

```json
{
  "version": 1,
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

### `get`

One thing, by its id, with the claims written on it. It takes one id argument and two
flags.

| Flag | Meaning |
|------|---------|
| `--claims <how>` | `full` (default), every claim written on it, or `resolved`, the current claim under each predicate. |
| `--deprecated` | Include the claims that have been deprecated. Refused beside `--claims resolved`. |

An id is unique across the whole model, so this is one command for both families. A vertex,
an edge and a loop are retrieved by the same call a semantic node is, and `family` says
which came back and so which of the fields to expect.

```json
{
  "version": 1,
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
    "span": {
      "start": {"path": "entities/site.dfc", "line": 13, "column": 1, "offset": 142},
      "end": {"path": "entities/site.dfc", "line": 52, "column": 43, "offset": 1284}
    },
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
        "span": {
          "start": {"path": "entities/site.dfc", "line": 30, "column": 3, "offset": 712},
          "end": {"path": "entities/site.dfc", "line": 36, "column": 25, "offset": 934}
        }
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
| `entity.span` | object | Where it was written: file, line, column and byte offset, at both ends of the form. |
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
| `claims[].span` | object | Where the claim was written. |

Under `--claims resolved` a predicate appears once, as the claim that won. Where nothing
won it appears as every claim that could still be the answer — `tied` where the rule could
not separate them, `unranked` where nothing rankable was said — because narrowing four
claims to two is most of the work of deciding between them, and a caller shown one of the
two cannot tell that the other exists.

Deprecated claims are left out unless `--deprecated` asks for them. `--deprecated` beside
`--claims resolved` is a **usage error** rather than a flag that is quietly ignored: a
deprecated claim is retracted rather than out-ranked, and resolution never considers one.

References are ids and are never the things they name, so the answer is the size of the
thing that was asked for rather than of the model behind it. Following one is another call.

An id nothing in the model holds is a **usage error** — exit `3`, with nothing on stdout —
naming it, and naming the nearest id there is when one is close enough to be the id that
was meant. It is not an empty answer: a thing that is not there and a thing with nothing
said about it are different answers. An argument that is not a well-formed id is the same
exit code, reporting the rule it broke rather than a lookup that was never going to find
anything.

### Diagnostics and the exit code of a read

The listings and `get` exit `0` whenever they answered, whatever the model's diagnostics
say. Those diagnostics are still rendered in full on stderr.

A listing says what a model holds, and a node whose containment does not resolve is still a
node the model holds. Whether the model is *sound* is what `dfcad check` answers; answering
it in two commands, with two definitions of sound, is how the two come to disagree. It also
keeps discovery usable on a model somebody is halfway through writing, which is the model
discovery is most needed on.
