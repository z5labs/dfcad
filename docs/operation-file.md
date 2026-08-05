# The operation file format

This is the shape of the file `dfcad apply` reads: a list of authoring operations applied
as one change. It is the input half of the interface an agent programs against, and it is
versioned for the same reason [the machine output contract](./machine-output.md) is.

Why it exists is
[0015. The command line interface is the primary write path](./decisions/0015-the-cli-is-the-primary-write-path.md)
and [0016. Writes are all-or-nothing](./decisions/0016-writes-are-all-or-nothing.md): every
write validates the whole model before any byte reaches disk, so a batch of edits applied
one command at a time pays that cost once per edit — and a mistake in the twelfth of them
leaves eleven half-applied changes behind. A batch pays it once and lands completely or not
at all.

## The file

One JSON object, and nothing after it:

```json
{
  "version": 1,
  "operations": [
    {"op": "add-node", "id": "site:S-104", "kind": "Space", "type": "MeetingRoom",
     "geometry": "area", "frame": "frame:building", "label": "Meeting Room D"},
    {"op": "add-claim", "subject": "site:S-104", "predicate": "area",
     "claim": {"value": "18.0", "unit": "m2",
               "source": "As-built check AB-2026-012, Acme Surveys",
               "method": "method:total-station",
               "accuracy": ["independent 0.05 m2"],
               "date": "2026-05-06"}}
  ]
}
```

| Member | Type | Meaning |
|--------|------|---------|
| `version` | integer, optional | The version of this format the file was written against. Absent means the version this engine reads, which is `1`. Any other value is refused rather than guessed at. |
| `operations` | array | The operations, in the order they are applied. The order is the data: an operation may name what an earlier one wrote. At least one. |

The file is read from the path named on the command line, or from standard input where none
is named or where the path is `-`. A relative path is resolved against the model root, as
every path this interface takes is.

## An operation

Every operation is an object whose `op` names which operation it is and whose other members
are that operation's axes:

| Member | Meaning |
|--------|---------|
| `op` | The operation. It is the name of the command that makes the same change on its own, so an author who knows the interface knows the file. |

The rest of the members are tabled per operation below. They are the flags and arguments of
that command, spelled in lower camel case where the flag is two words: `--backed-by` is
`backedBy`, `--superseded-by` is `supersededBy`, `--no-snap` is `noSnap`.

**Nothing is guessed.** A member no operation of that name reads is refused rather than
ignored, an operation nothing declares is refused naming the ones there are, and a member an
operation requires and which was not written is refused before any model is read. A batch
somebody generated is exactly the input where a silently dropped member is discovered a week
later.

## A claim

Four operations write a claim, and they write it the same way: as a `claim` object holding
the value and the evidence for it.

| Member | Meaning |
|--------|---------|
| `value` | What is claimed, in the shape the predicate declares. |
| `unit` | The unit it is expressed in, which must be the one the predicate declares. A non-dimensional predicate takes none, and there is no unitless token. |
| `source` | The evidence: a report, a drawing, a person, an instrument log. |
| `method` | An id naming how the value was obtained. |
| `accuracy` | The terms of how well it is known, each written as the entity format writes one without its parentheses: `independent <magnitude> <unit>`, or `systematic <magnitude> <unit> <term-id>`. |
| `date` | The day the value was obtained, as `YYYY-MM-DD`. The day the change is made where it is absent. |
| `id` | The claim's own id. Absent for a claim nothing references, which is most of them. |

**A value is a string, not a JSON number.** `"value": "18.0"`, and a coordinate is
`"value": "12.0 0.0 0.0"`. Which of the four shapes a value takes is registry data
([0010](./decisions/0010-the-engine-carries-no-domain-vocabulary.md)), so reading
`1.0 2.0 3.0` as a scalar or as a coordinate is a question only the predicate's declaration
answers — and one spelling means a claim written on a command line and a claim written here
are read by the same code and refused in the same words.

An empty string is a value rather than an absence: `"value": ""` under a predicate declaring
text is the empty string, which is a claim, and leaving the member out says nothing at all.

Leaving `accuracy` out is permitted and is reported: the claim loads and is unrankable, so it
can never win resolution and is not given a default.

## The operations

Every operation there is, in the order this document tables them. Each is exactly the
command of the same name; that command's help — `dfcad <command> -h` — is the longer
description of what it does and what it refuses.

### `add-node`

A new semantic node.

| Member | Meaning |
|--------|---------|
| `id` | The id it will be written with. Required. |
| `kind` | The kind it declares. |
| `type` | The type it declares. |
| `geometry` | The geometry form it declares. Absent for a node with no geometry, which its type has to permit. |
| `frame` | The coordinate frame it is expressed in. |
| `label` | Its display text, which nothing resolves through. |
| `file` | Write it here instead, overriding the routing rules. |

### `add-vertex`

A new corner, with where it is and how that is known.

| Member | Meaning |
|--------|---------|
| `id` | The id it will be written with. Required. |
| `frame` | The frame its position is expressed in. Required: a geometric node is always in exactly one. |
| `label` | Its display text. |
| `file` | Write it here instead, overriding the routing rules. |
| `predicate` | The predicate its position is claimed under. Absent for a corner that has been named and not yet surveyed, whose position is then unknown rather than zero. |
| `claim` | The position claim, read only where `predicate` is given. |

### `add-edge`

A connection between two corners.

| Member | Meaning |
|--------|---------|
| `id` | The id it will be written with. Required. |
| `frame` | The frame it is expressed in. Required. |
| `label` | Its display text. |
| `file` | Write it here instead, overriding the routing rules. |
| `start` | The vertex it runs from. Required. |
| `end` | The vertex it runs to. Required. |
| `backedBy` | The semantic nodes that physically realise it, as an array. An edge naming none is virtual, which is computed rather than written. |

The order of the two ends is significant and is never sorted: an edge is directed, and the
region on the other side of it traverses it the other way.

### `add-loop`

An ordered ring of edges.

| Member | Meaning |
|--------|---------|
| `id` | The id it will be written with. Required. |
| `frame` | The frame it is expressed in. Required. |
| `label` | Its display text. |
| `file` | Write it here instead, overriding the routing rules. |
| `edges` | The edges of the ring, in traversal order. At least one. |

The order is the data. It is preserved exactly as written and is never sorted. Whether the
ring closes is judged when the model the batch produces is loaded.

### `scaffold-loop`

A room's corners, walls and outline in one operation.

| Member | Meaning |
|--------|---------|
| `namespace` | The declared id namespace the new nodes are minted in. Required. |
| `frame` | The frame the corners are expressed in. Required. |
| `label` | The loop's display text. |
| `file` | Write everything here instead, overriding the routing rules. |
| `predicate` | The predicate a corner's position is claimed under. Required. |
| `tolerance` | The declared tolerance two corners are judged to be one point by, which is also what says the list closed. Required. |
| `corners` | The corners, in order and authored closed: the last names the first corner again. At least one. |
| `noSnap` | Write a new vertex at every corner, even where one is already there. Every corner that would have been reused is still reported. |
| `claim` | The evidence every position claim is written with. Its `value` is not read: a corner's value is the corner. |

### `set-label`

What a thing is called, and nothing else.

| Member | Meaning |
|--------|---------|
| `id` | The thing being renamed. Required. |
| `label` | What it is now called. An empty one removes the label, which is how a thing goes back to having none. |

### `retire`

That a thing stopped existing.

| Member | Meaning |
|--------|---------|
| `id` | The thing that stopped existing. Required. |
| `reason` | Why, in the author's words. Required: a retirement with no reason is a deletion wearing a hat. |
| `replacement` | The node that stands in its place, where one does. Supplying one is also what redirects the references to it. |
| `date` | When it stopped existing, as `YYYY-MM-DD`. Today by default. |

### `add-claim`

A measured value attached to a thing, with its provenance.

| Member | Meaning |
|--------|---------|
| `subject` | The thing the claim is about. Required. |
| `predicate` | The predicate it is written under. Required. |
| `claim` | The value and the evidence for it. Required. |

### `supersede`

A correction: the new value stated and the old one retracted, in one operation.

| Member | Meaning |
|--------|---------|
| `subject` | The thing the claim is about. Required. |
| `predicate` | The predicate the claim being corrected was written under. Required. |
| `claim` | The new value and the evidence for it. Required. |

The claim being corrected is the one live claim written on that subject and predicate. A
pair nothing states is refused rather than added to, and one stated more than once is refused
naming the competing claims.

### `deprecate-claim`

That a claim was retracted.

| Member | Meaning |
|--------|---------|
| `claim` | The id of the claim being retracted, which is the id it wrote. Required. |
| `supersededBy` | The claim that stands in its place. Required. |

## What one operation may assume of another

**An operation may name what an earlier one wrote.** The ids the batch has already written
are taken, resolve as references, and answer as the subject of a claim — so a node and the
claims about it, or an edge between two vertices of the same batch, are one statement rather
than several changes that have to be applied in turn. That is the whole reason the order is
the data.

**The model is interpreted once, after the last operation.** No operation is validated
against the model as it stands halfway through the batch, so an end state that loads is
accepted however its intermediate states would have read.

**What a batch has written is what it wrote, not what it means.** The claims a batch writes
are in the files it is about to produce; they are not resolved, ranked or re-read until that
model is loaded. So an operation that reads what the model *says* — `supersede`, which finds
the one live claim of a subject and predicate, and `deprecate-claim`, which finds the claim
an id names — sees the claims the model held when the batch began. Correcting a claim the
same batch wrote is two statements about one measurement, and it is written as one.

**A retirement names what the model already holds.** Retiring a node the same batch created
is refused: a batch that creates a thing and withdraws it again is a batch that should not
have created it.

## What is refused, and how

Two passes, and each says everything it found rather than the first thing:

**The file, before any model is read.** A file that is not one JSON object, an operation
nothing declares, a member no operation of that name reads, a member an operation requires
and which was not written, a version this engine does not read. Every problem comes back at
once, each naming the operation it is about by its place in the list, counted from one.

**The operations, against the model.** An id something already holds, a type nothing
declares, a value of the wrong shape, a unit other than the declared one, a reference that
names nothing. The first operation the model refuses is the answer: the operations after it
may depend on it, and reporting the failures they would then have would bury the one that is
real.

Either way **nothing at all is written**, and the model is exactly what it was. The correct
response to a refusal is to fix the file and reissue it: there is no partial state to inspect
and nothing to reconcile ([0016](./decisions/0016-writes-are-all-or-nothing.md)).

The model the batch produces is validated last, exactly as it is for a single write command,
and a batch that would produce a model that does not load is refused with the diagnostics
that load would have raised.

## What it reports

The result object is documented with the other payloads, under
[`apply`](./machine-output.md#apply).
