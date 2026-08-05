# 0017. The answer is the default and the evidence is asked for

**Status:** Accepted

## Context

The engine is built on a bet: an agent that has never seen a model is better off asking it
questions than reading its files. [0014](./0014-the-machine-output-contract-is-part-of-the-interface.md)
made the answers machine-readable and versioned; nothing in it said how much of an answer
an answer should be.

The bet was measured, and [`docs/token-budget.md`](../token-budget.md) recorded the result.
The ratio was never in doubt — the query path came in between eighteen and thirty-seven
times cheaper than reading the model — but the absolute figures missed. Discovery cost 559
tokens against a few hundred; a dimensional question from a cold start cost 1,143 against
the low hundreds. Pricing each field by re-encoding the same answer without it said where
the tokens were, and the three findings were the same finding three times:

- Spans were about half of `dfcad get` and a third of `dfcad resolve`. Each was two nested
  objects of four fields — path, line, column, offset — with the whole path written twice,
  on every claim of every answer.
- `resolve` returned the entire winning claim beside the value it had already reported.
  Take the claim away and 199 tokens became 51: three quarters of the answer to "how big is
  this room" was who said so, how, when and where they wrote it down.
- `list-types` carried the registry's prose on every call. That is a cost which grows with
  the vocabulary rather than with the model, and it is paid again on every cold start by
  whoever is only deciding which type to ask about next.

Each of those is provenance, and provenance is the thing this format exists to keep. The
mistake was not carrying it. The mistake was carrying it *by default*, on the call whose
whole purpose is to be cheap, to a reader who in the overwhelming majority of cases wants a
number and a sense of how good it is.

There is a real objection to trimming, and it is the one
[0009](./0009-derived-values-are-never-written-back.md) and the claim model exist to answer:
a dimension handed over as a bare number is exactly what this engine refuses to do. An
answer with no accuracy on it is a number somebody has to go and qualify, and an interface
that made qualifying it a second call would be one where nobody qualifies anything.

## Decision

**An answer carries what says how good it is. It does not carry what says who to argue
with.**

That line is drawn in one place and applied everywhere:

| In the answer | Asked for |
|---------------|-----------|
| the value and its unit | the source the claim names |
| its accuracy, term by term | the method it was obtained by |
| the id of the claim it came from | the claim's rank and its date |
| which step of the rule chose it | where the claim was written |

The three changes that follow, all of them version `2` of the machine output contract:

**A span is a string.** `"entities/level-01.dfc:234:3-239:25"`, or
`"entities/level-01.dfc:234:3"` for an empty span, wherever the engine writes one — query
answers, diagnostics, invariant violations, review findings alike. The path is written once
because a span never crosses a file. The byte offsets are not written at all: they are a
convenience for a tool holding the source bytes, and such a tool is one line index away from
recovering them, while every other reader pays for them on every span it never reads.

**`resolve` answers the question.** The value, its `accuracy`, its `claim-id` and the reason
the rule chose it are the answer. `--evidence` adds the winning claim in full. `--candidates`
is unchanged and still reports every live claim in full, and an ambiguous outcome still
returns every tied claim whether or not it was asked for — where there is no answer, the
evidence *is* the answer.

**`list-types` leaves out the prose.** `--describe` adds the descriptions back. `absent` is
written only where it holds, the way `retired` already is on a listed instance.

**One rendering per thing, not a terse one and a full one.** The span form is the same in
every payload; there is no `--spans full` restoring the object shape. A field that has two
shapes depending on a flag is two shapes every caller has to learn, and the second is
learned at the moment something breaks.

**Rejected: dropping a field because the caller could have known it.** `list-instances
MeetingRoom` repeats `"type": "MeetingRoom"` on every entry, which is 30 of the 183 tokens
it costs, and the cold path is one token inside its target. Leaving it out when the type was
the argument was considered and refused: it makes the shape of an entry depend on how the
listing was narrowed, and a caller that merges two listings would have to remember what it
asked for each of them. Cheapness is not the only property being optimised.

## Consequences

The gate is met. Discovery costs 428 tokens against a target of 500, and a dimensional
question from a cold start costs 499. Asked again by an agent that already has the
vocabulary it costs 254. `docs/token-budget.md` records all of it, regenerated from the
measurement rather than typed in.

The answer still cannot be mistaken for a bare number. `accuracy` sits beside `value` on
every resolution that has one, and a claim which stated no accuracy still says so by its
absence and by the `unranked` reason beside it. Nothing about the refusal to hand over
unqualified dimensions has moved.

Spans got cheaper everywhere at once, including on the paths nobody was measuring —
diagnostics, `check`, `review`, `traverse` — because the encoding lives on the type rather
than in each payload.

The trimmed calls are the ones an agent makes without thinking, and the expensive ones are
the ones it makes on purpose. `--evidence` and `--describe` are typed by whoever has decided
they want to look.

## Cost

Callers on version `1` break, and they break in the two ways that are hardest to see: a
field that used to be an object is now a string, and a field that used to be there is now
absent. `version` says so, and the contract has said since 0014 that it would. This is the
first time it has been spent.

Byte offsets are gone from the JSON. A consumer slicing source text out of a file by offset
now has to build a line index first. That is a real loss for a hypothetical tool and a real
saving on every answer, and it was decided that way round because no such tool exists and
every answer does.

Two flags exist that would not need to if the defaults were fuller. `--evidence` and
`--describe` are surface, and somebody will ask for the audit trail, not get it, and have to
find out why.

A one-token margin is not a margin. The cold path is at 499 against 500 under `o200k_base`,
and the next field added to a listing entry spends it. The ceiling in
`cmd/dfcad/budget_test.go` turns that into a failing test rather than a silently worse
number, which is the most this decision can do about it.

## What would reverse it

A measured case where the trimmed answer causes an agent to make a second call more often
than not. The whole argument here is that the audit trail is wanted rarely; if `--evidence`
turns out to be typed on most resolutions, then the split is costing two round trips to save
128 tokens and the default was wrong.

A consumer of the machine contract that genuinely needs byte offsets — an editor
integration, a source-rewriting tool — would be an argument for putting them back, and the
form has room: a fourth number per end, at about five tokens a span. That is a version
change and it is additive to the grammar rather than a return to the object.

What would not reverse it is a reader finding a trimmed answer harder to read by eye. The
answers are for a program; a person reading one has `--format human` on stderr, and it is
unchanged.
