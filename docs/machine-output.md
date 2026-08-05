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
| `entity.retired` | object, optional | How a semantic node stopped existing: `date`, `reason`, and `superseded-by` where something stands in its place. Absent for a node that was not retired. |
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
known, the claim it came from and which step of the rule picked that claim.

```json
{
  "version": 1,
  "command": "resolve",
  "subject": "site:S-101",
  "predicate": "area",
  "outcome": "resolved",
  "reason": "accuracy",
  "strict": false,
  "value": {"shape": "scalar", "unit": "m2", "scalar": 24.2},
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
    "span": {"start": {"path": "entities/site.dfc", "line": 18, "column": 3, "offset": 402},
             "end": {"path": "entities/site.dfc", "line": 24, "column": 26, "offset": 619}}
  }
}
```

| Flag | Meaning |
|------|---------|
| `--candidates` | Report every live claim under the predicate beside the answer, each marked with what resolution made of it. |
| `--frame <id>` | Express a coordinate answer in this frame rather than in the one the thing is written in. |

| Field | Type | Meaning |
|-------|------|---------|
| `subject` | string | The id the question was asked about. |
| `predicate` | string | The predicate it was asked under. |
| `outcome` | string | `resolved`, `unranked`, `ambiguous` or `unclaimed`. It says which of the fields below to expect. |
| `reason` | string | Which step of the rule produced that outcome: `only`, `accuracy`, `recency`, `unranked`, `ambiguous` or `unclaimed`. |
| `strict` | boolean | Whether the registry declares the predicate strict. Written whatever the outcome. |
| `value` | object, optional | The answer, in the same shape `claims[].value` takes elsewhere. Absent where nothing resolved. |
| `frame` | string, optional | The coordinate frame the value is expressed in. Absent for a value that is not a position, which is in no frame. |
| `claim` | object, optional | The claim the answer came from, in the shape documented under `get`. Absent where nothing resolved. |
| `candidates` | array, optional | Claims that could still be the answer, each in that same shape and marked with its `resolution`. |
| `budget` | object, optional | The accumulated error of a cross-frame answer, broken out by term. Written only where a frame transform was applied. |

The four outcomes and the four exit codes line up, because what a caller does about each is
different:

| Outcome | Exit | Carries |
|---------|------|---------|
| `resolved` | `0` | `value` and `claim`. `reason` is `only`, `accuracy` or `recency`. |
| `unranked` | `0` | `value` and `claim`. The one live claim under a predicate nothing rankable was said about: still what the model says, and not an answer the rule chose. |
| `ambiguous` | `4`, or `5` where `strict` | `candidates`, every one of them. No `value` and no `claim`. |
| `unclaimed` | `1` | Neither. Nothing live is written under the predicate. |

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
| `budget.from` | string | The frame the value was written in. |
| `budget.to` | string | The frame it was expressed in. |
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
  "version": 1,
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
      "span": {
        "start": {"path": "entities/site.dfc", "line": 41, "column": 1, "offset": 812},
        "end": {"path": "entities/site.dfc", "line": 48, "column": 25, "offset": 994}
      }
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
| `results[].span` | object | Where it was written. |

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
  "version": 1,
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
      "span": {
        "start": {"path": "entities/site.dfc", "line": 20, "column": 3, "offset": 412},
        "end": {"path": "entities/site.dfc", "line": 28, "column": 34, "offset": 703}
      }
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
      "span": {
        "start": {"path": "entities/site.dfc", "line": 29, "column": 3, "offset": 706},
        "end": {"path": "entities/site.dfc", "line": 35, "column": 25, "offset": 928}
      }
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
  "version": 1,
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
          "span": {
            "start": {"path": "entities/site.dfc", "line": 29, "column": 3, "offset": 706},
            "end": {"path": "entities/site.dfc", "line": 35, "column": 25, "offset": 928}
          }
        },
        {
          "id": "survey:A-0003",
          "predicate": "area",
          "value": {"shape": "scalar", "unit": "m2", "scalar": 24.0},
          "rank": "normal",
          "resolution": "outranked",
          "span": {
            "start": {"path": "entities/site.dfc", "line": 36, "column": 3, "offset": 931},
            "end": {"path": "entities/site.dfc", "line": 42, "column": 25, "offset": 1147}
          }
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
  "version": 1,
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
  "version": 1,
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
| `files[].effects[].tag` | string | The form it was written as — `node`, `vertex`, `edge`, `loop` — so an effect says which family it is about without the reader resolving the id. |
| `files[].effects[].id` | string, optional | The thing it was about. Absent for a form carrying no id. |
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
  "version": 1,
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

The claim flags of `add-claim` supply the evidence every position claim carries — `--source`,
`--method`, `--accuracy`, `--date` and `--unit`. `--value` is not read: a corner's value is
the corner.

Ids are minted as `<namespace>:<form>-<n>` — the namespace, the tag of the form being
written, and the lowest ordinal nothing in the model already holds. It is a name and not a
schema, and nothing is inferred back out of one
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
or the outline doubles back, and a ring visits each of its corners once.

```json
{
  "version": 1,
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
  "version": 1,
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
  "version": 1,
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
  "version": 1,
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
  "version": 1,
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

### Diagnostics and the exit code of a read

The listings, `get`, `traverse`, `claims` and `conflicts` exit `0` whenever they answered,
whatever the model's diagnostics say. Those diagnostics are still rendered in full on stderr.

A listing says what a model holds, and a node whose containment does not resolve is still a
node the model holds. Whether the model is *sound* is what `dfcad check` answers; answering
it in two commands, with two definitions of sound, is how the two come to disagree. It also
keeps discovery usable on a model somebody is halfway through writing, which is the model
discovery is most needed on.
