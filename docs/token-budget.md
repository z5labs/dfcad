# The discovery token budget

The engine's central claim is that an agent which has never seen a model is better off
asking it two questions than reading it. `list-types` says what kinds of thing exist,
`list-instances` says which of them are there, and between them they are supposed to make
a model learnable for a few hundred tokens rather than for the tens of thousands its files
cost. Everything else in the query surface assumes you already know what you are looking
for, so if that claim is wrong the partitioning is wrong.

This file is that claim measured. It is not an estimate: the counts below come from a byte
pair encoder, against a model somebody could have authored, and they are regenerated from
the code that produced them rather than typed in.

```sh
go test ./cmd/dfcad -update
```

The harness is `cmd/dfcad/budget_test.go`. Everything between the two markers below is
written by it, and editing that block by hand fails
`TestTheRecordedTokenBudgetIsCurrent`.

## What is measured, and how

**A real tokenizer.** Counts come from `github.com/tiktoken-go/tokenizer`, which embeds
its rank tables, so CI counts the same tokens an author does and nothing reaches the
network. Two encodings, because a partitioning that fits under one vendor's vocabulary and
not another's has not been measured, it has been lucky. Both are OpenAI encodings, because
those are the ones with a published, offline implementation. No Claude tokenizer is
distributable, so the record names what was used rather than implying a coverage the
measurement does not have.

**What is counted.** Only stdout: the JSON object a caller pays for. Diagnostics, progress
and everything else on stderr are for a person and never reach a model, so they are
discarded rather than counted. The argv of each call is a handful of tokens and is left
out; it does not change any conclusion here.

**Against what.** `cmd/dfcad/testdata/budget` is a two-storey office building — its
registry, its spaces, the elements that divide them, and the surveyed corners, edges and
rings its outlines are assembled from. It is committed rather than generated, so its size
is a fact somebody can read, and its own README says how it is laid out. The comparison is
the whole model read as text, and, as the harder case, the single file the answer happens
to be written in.

**The question asked.** How big is Meeting Room B on level 1, from an agent that has read
nothing: `list-types`, then `list-instances MeetingRoom`, then `get`, then `resolve`.
`MeetingRoom` because it is a middling type in this model — six instances against
`Office`'s sixteen and `OfficeBuilding`'s one — and measuring the smallest would report
the arrangement at its best.

<!-- begin measurements -->

## The tokenizer

Counted with `github.com/tiktoken-go/tokenizer`, at the version go.mod pins:

| Encoding | Version | The tokenizer of |
|----------|---------|------------------|
| `o200k_base` | v0.7.0 | GPT-5, GPT-4.1, GPT-4o, the o-series |
| `cl100k_base` | v0.7.0 | GPT-4, GPT-3.5 Turbo |

## The model

`cmd/dfcad/testdata/budget`, which holds 63 nodes, 56 vertices, 90 edges, 26 loops, 140 claims, 0 conflicts, 0 unresolved.

| File | Bytes | Lines |
|------|-------|-------|
| `entities/building.dfc` | 1376 | 54 |
| `entities/level-01.dfc` | 10868 | 465 |
| `entities/level-02.dfc` | 10868 | 465 |
| `geometry/level-01.dfc` | 15202 | 496 |
| `geometry/level-02.dfc` | 15234 | 496 |
| `registry.dfc` | 3483 | 124 |
| **the model** | **57031** | **2100** |

## The cost of discovery

Answering: what kinds of thing are in this model, and which meeting rooms exist.

| Call | `o200k_base` | `cl100k_base` |
|------|-------|-------|
| `dfcad list-types` | 376 | 355 |
| `dfcad list-instances MeetingRoom` | 183 | 182 |
| **the whole path** | **559** | **537** |

Target 500 tokens: **missed**. Regression ceiling 600 tokens.

## The cost of a dimensional question from a cold start

Answering: how big is Meeting Room B on level 1, starting from nothing.

| Call | `o200k_base` | `cl100k_base` |
|------|-------|-------|
| `dfcad list-types` | 376 | 355 |
| `dfcad list-instances MeetingRoom` | 183 | 182 |
| `dfcad get site:S-111` | 388 | 378 |
| `dfcad resolve site:S-111 area` | 196 | 192 |
| **the whole path** | **1143** | **1107** |

Target 500 tokens: **missed**. Regression ceiling 1200 tokens.

## Where the tokens go

What each answer costs with one field removed. Both figures in a cell are of the
answer re-encoded from its parsed form, so that the difference between them is the
field rather than the marshaller. That re-encoding sorts object keys, so a "down
from" figure differs by a token or two from the same call in the tables above.

| Field | Answer | `o200k_base` without it | `cl100k_base` without it |
|-------|--------|--------|--------|
| the descriptions in `list-types` | `dfcad list-types` | 285, down from 376 | 264, down from 355 |
| the spans in `get` | `dfcad get site:S-111` | 207, down from 393 | 202, down from 379 |
| the spans in `resolve` | `dfcad resolve site:S-111 area` | 137, down from 199 | 134, down from 193 |
| the whole claim beside the value in `resolve` | `dfcad resolve site:S-111 area` | 51, down from 199 | 49, down from 193 |

## The cost of reading the files instead

| What is read | `o200k_base` | `cl100k_base` |
|--------------|-------|-------|
| the whole model | 20600 | 20702 |
| `entities/level-01.dfc` alone, the file the answer is written in | 3669 | 3683 |

## The ratio

| Path | Against the whole model | Against the one file |
|------|-------------------------|----------------------|
| discovery | 36.9×, 38.6× | 6.6×, 6.9× |
| a dimensional question from a cold start | 18.0×, 18.7× | 3.2×, 3.3× |

One figure per encoding, in the order of the table above.

<!-- end measurements -->

## The outcome

**The bet holds. The gate does not.**

The bet is the ratio, and it is not close: discovery costs 559 tokens against 20,600 to
read the model, which is 37 times cheaper, and the whole cold-start question costs 1,143,
which is 18 times cheaper. Against the single file the answer is written in — the harder
comparison, because knowing which file to open is itself something discovery had to supply
— discovery is still 6.6 times cheaper and the full question 3.2 times. Nothing here
suggests reading the files is the better arrangement.

The gate is the absolute figure, and it missed. [#38](https://github.com/z5labs/dfcad/issues/38)
asked for discovery plus a targeted fetch to land in the low hundreds of tokens. It lands
at 1,143. Discovery alone lands at 559 against the "few hundred"
[#33](https://github.com/z5labs/dfcad/issues/33) claimed for it. Neither is off by an order
of magnitude, and both are over.

So the gate does what a gate is for.
[#113](https://github.com/z5labs/dfcad/issues/113) is the partitioning review, opened with
the specific costs that missed rather than with a verdict. In short, from the breakdown
above:

- **Spans are about half of `get` and a third of `resolve`.** Two positions of four fields
  each, with the model root repeated in every path, on every claim. An agent asking how
  big a room is never opens the file.
- **`resolve` returns the whole winning claim beside the value it already reported.** Take
  the claim away and 199 tokens become 51: three quarters of that answer is the audit trail
  rather than the answer.
- **`list-types` pays for the registry's prose on every cold start.** That cost grows with
  the vocabulary rather than with the model, and it is paid again every time.

Until that review lands, the numbers above are held where they are rather than allowed to
drift: `TestTheDiscoveryPathDoesNotGetMoreExpensive` asserts a ceiling just above what was
measured, so a field added to a listing entry arrives as a failing test to be weighed
against the gate, not as a slightly larger number nobody looked at. The targets in
`cmd/dfcad/budget_test.go` stay where the stories put them. Raising one to make a test
green would turn the claim into a description of whatever the code happens to do, which is
the one repair that is not one.

## Reproducing it

```sh
go test ./cmd/dfcad -run TokenBudget          # check the record is current
go test ./cmd/dfcad -update                   # rewrite it from a fresh measurement
go test ./cmd/dfcad -bench . -run '^$'        # the benchmarks, reporting tokens/op
```

The benchmarks report `tokens/op` beside `ns/op`. The wall clock is measured and is not
the point: a discovery call that took a second and cost forty tokens would still be the
right arrangement, and one that cost forty thousand would not be at any speed.
