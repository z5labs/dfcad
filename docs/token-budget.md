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
nothing: `list-types`, then `list-instances MeetingRoom`, then `resolve`. `MeetingRoom`
because it is a middling type in this model — six instances against `Office`'s sixteen and
`OfficeBuilding`'s one — and measuring the smallest would report the arrangement at its
best.

**Five paths, not two.** The first measurement, for
[#38](https://github.com/z5labs/dfcad/issues/38), ran `get` between the listing and the
resolution and called the four calls together "discovery plus a targeted fetch". `get` is
not a targeted fetch. It retrieves a thing entire — every claim written on it, every
reference it made, and where each of those was written — which is a different question
from how big the room is, and it is the one call on the path whose cost grows with how
much has been said about the subject. So the gate is read off the three calls that answer
the question, and the four-call path is still measured beside them, unchanged, so that
#38's figure stays comparable.

Two more paths are there because the costs on this path scale with different things.
`list-types` grows with the registry and is paid once per cold start; `list-instances` and
`resolve` grow with neither and are paid per question. A record that only ever reported
their sum would hide which of the two a larger vocabulary makes worse.

The fifth is the same question asked of the geometry rather than of a claim: `list-types`,
`list-instances MeetingRoom`, then `dfcad measure`. It is measured and it is not gated,
because it is not the same question. `resolve` answers what somebody wrote down; `measure`
answers what the corners come to, and says how well the corners are known — which is one
term per corner, so its cost grows with the shape while its figures do not. A target set
for the first would be a gate on the second for reasons nobody argued. What the cost is
made of is priced in "Where the tokens go" below, split into the budget as a whole and the
claims named under each of its terms, so that a partitioning review of this call has both
figures rather than a total.

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
| `dfcad list-types` | 245 | 224 |
| `dfcad list-instances MeetingRoom` | 183 | 182 |
| **the whole path** | **428** | **406** |

Target 500 tokens: **met**. Regression ceiling 460 tokens.

## The cost of a dimensional question from a cold start

Answering: how big is Meeting Room B on level 1, starting from nothing.

| Call | `o200k_base` | `cl100k_base` |
|------|-------|-------|
| `dfcad list-types` | 245 | 224 |
| `dfcad list-instances MeetingRoom` | 183 | 182 |
| `dfcad resolve site:S-111 area` | 71 | 68 |
| **the whole path** | **499** | **474** |

Target 500 tokens: **met**. Regression ceiling 530 tokens.

## The cost of the same question once the vocabulary is known

Answering: how big is Meeting Room B on level 1, for an agent which has already read list-types.

| Call | `o200k_base` | `cl100k_base` |
|------|-------|-------|
| `dfcad list-instances MeetingRoom` | 183 | 182 |
| `dfcad resolve site:S-111 area` | 71 | 68 |
| **the whole path** | **254** | **250** |

Target 300 tokens: **met**. Regression ceiling 280 tokens.

## The cost of the same question by way of a whole retrieval

Answering: how big is Meeting Room B on level 1, retrieving the thing itself on the way.

| Call | `o200k_base` | `cl100k_base` |
|------|-------|-------|
| `dfcad list-types` | 245 | 224 |
| `dfcad list-instances MeetingRoom` | 183 | 182 |
| `dfcad get site:S-111` | 275 | 269 |
| `dfcad resolve site:S-111 area` | 71 | 68 |
| **the whole path** | **774** | **743** |

No target: nothing asked this path to cost anything in particular. Regression ceiling 820 tokens.

## The cost of the same question answered from the geometry rather than from a claim

Answering: how big is Meeting Room B on level 1 by the corners it is drawn on, starting from nothing.

| Call | `o200k_base` | `cl100k_base` |
|------|-------|-------|
| `dfcad list-types` | 245 | 224 |
| `dfcad list-instances MeetingRoom` | 183 | 182 |
| `dfcad measure site:S-111` | 501 | 479 |
| **the whole path** | **929** | **885** |

No target: nothing asked this path to cost anything in particular. Regression ceiling 960 tokens.

## The cost of finding the geometry which carries a measurement

Answering: which corners of this model anybody has surveyed a position for.

| Call | `o200k_base` | `cl100k_base` |
|------|-------|-------|
| `dfcad list-geometry --predicate position --family vertex` | 2427 | 2370 |
| **the whole path** | **2427** | **2370** |

No target: nothing asked this path to cost anything in particular. Regression ceiling 2500 tokens.

## Where the tokens go

What each answer costs with one field removed. Both figures in a cell are of the
answer re-encoded from its parsed form, so that the difference between them is the
field rather than the marshaller. That re-encoding sorts object keys, so a "down
from" figure differs by a token or two from the same call in the tables above.

| Field | Answer | `o200k_base` without it | `cl100k_base` without it |
|-------|--------|--------|--------|
| the descriptions `--describe` adds | `dfcad list-types --describe` | 245, down from 336 | 224, down from 315 |
| the whole claim `--evidence` adds | `dfcad resolve site:S-111 area --evidence` | 72, down from 180 | 68, down from 174 |
| the spans in `get` | `dfcad get site:S-111` | 211, down from 277 | 206, down from 269 |
| the accuracy beside the value in `resolve` | `dfcad resolve site:S-111 area` | 51, down from 72 | 49, down from 68 |
| the error budget in `measure` | `dfcad measure site:S-111` | 169, down from 499 | 166, down from 475 |
| the claims named under each budget term in `measure` | `dfcad measure site:S-111` | 348, down from 499 | 337, down from 475 |

## The cost of reading the files instead

| What is read | `o200k_base` | `cl100k_base` |
|--------------|-------|-------|
| the whole model | 20600 | 20702 |
| `entities/level-01.dfc` alone, the file the answer is written in | 3669 | 3683 |

## The ratio

| Path | Against the whole model | Against the one file |
|------|-------------------------|----------------------|
| discovery | 48.1×, 51.0× | 8.6×, 9.1× |
| a dimensional question from a cold start | 41.3×, 43.7× | 7.4×, 7.8× |
| the same question once the vocabulary is known | 81.1×, 82.8× | 14.4×, 14.7× |
| the same question by way of a whole retrieval | 26.6×, 27.9× | 4.7×, 5.0× |
| the same question answered from the geometry rather than from a claim | 22.2×, 23.4× | 3.9×, 4.2× |
| finding the geometry which carries a measurement | 8.5×, 8.7× | 1.5×, 1.6× |

One figure per encoding, in the order of the table above.

<!-- end measurements -->

## The outcome

**The bet holds. The gate is met, and it was not met by moving it.**

The bet is the ratio, and it was never close: discovery costs 428 tokens against 20,600 to
read the model, which is 48 times cheaper, and the cold-start question costs 499, which is
41 times cheaper. Against the single file the answer is written in — the harder comparison,
because knowing which file to open is itself something discovery had to supply — discovery
is 8.6 times cheaper and the question 7.4 times. Nothing here suggests reading the files is
the better arrangement.

The gate is the absolute figure, and it now lands inside it.
[#38](https://github.com/z5labs/dfcad/issues/38) asked discovery plus a targeted fetch to
cost the low hundreds of tokens; it costs 499 under `o200k_base` and 474 under
`cl100k_base`, against a target of 500. Discovery alone costs 428 against the "few hundred"
[#33](https://github.com/z5labs/dfcad/issues/33) claimed for it. Asked a second time by an
agent that already has the vocabulary, the same question costs 254.

**499 against 500 is one token of margin, and that is worth saying out loud.** The
`o200k_base` figure would read `missed` if a single field grew by a word. The margin is not
what makes the arrangement right — the ratio is — and the ceiling in
`cmd/dfcad/budget_test.go` is what stops the next change spending it without anybody
noticing.

### What the review changed

[#113](https://github.com/z5labs/dfcad/issues/113) was the partitioning review the miss
triggered. Its finding was not that the partitioning is wrong; it is that the answers were
carrying the audit trail by default, and that an audit trail costs more than an answer.
Three changes, each a version-2 change to the machine output contract, and the reasoning
behind them is
[0017. The answer is the default and the evidence is asked for](./decisions/0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md):

- **A span is a string.** `entities/level-01.dfc:234:3-239:25` rather than two nested
  objects of four fields with the path in both. `dfcad get` fell from 392 tokens to 275 on
  that alone, and nothing about finding the definition changed.
- **`resolve` answers the question.** The value, its unit, its accuracy and the id of the
  claim it came from say how good the number is; the source, the method, the rank and the
  span say who to argue with. The second set moved behind `--evidence`, and the answer fell
  from 196 tokens to 71.
- **`list-types` leaves out the prose.** The descriptions moved behind `--describe`, and
  `absent` is written only where it holds. 376 tokens became 245, on the one call every
  cold start begins with.

One thing the review considered and did not do: `list-instances MeetingRoom` repeats
`"type": "MeetingRoom"` on every entry, which is 30 tokens of the 183 it costs. Leaving it
out when the type was the argument would buy the margin the cold path is short of, and it
would mean an entry whose shape depends on how the listing was narrowed. That is a worse
interface for a caller than a tighter number is a better one.

The numbers above are held where they are rather than allowed to drift:
`TestTheDiscoveryPathDoesNotGetMoreExpensive` asserts a ceiling just above what was
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
