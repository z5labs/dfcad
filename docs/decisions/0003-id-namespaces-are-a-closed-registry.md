# 0003. Id namespaces are a closed registry

**Status:** Accepted

## Context

A model of any size draws identifiers from more than one authority. Some are minted by the
project; some come from a survey firm's point numbering; some are a local authority's
parcel reference; some are a manufacturer's part number. Left unqualified, these collide —
two authorities issue `1042` and mean different things — and the collision is silent,
because a bare identifier carries no evidence of where it came from.

Qualifying an id fixes the collision. `survey:1042` and `parcel:1042` are different ids,
and nothing has to guess. But qualification alone only moves the problem: if any prefix is
legal, then `srvy:1042`, `survey:1042` and `Survey:1042` are three different ids naming one
point, and the model has a silent fan-out of near-duplicate namespaces that nobody notices
until a query returns two answers.

The temptation at that point is to make the engine understand namespaces — to teach it
that `survey` means points from a total station, and therefore to apply particular
validation, particular accuracy defaults, particular rendering. That is domain vocabulary
in the engine, and it fails the test in [the README](../../README.md#where-does-a-change-belong):
the meaning would be nonsense to a model of a different subject.

## Decision

**An id is `namespace:local`.** The namespace is the part before the first colon; the
local part is everything after it.

**The permitted namespaces are a closed registry**, supplied by the consuming repository
as registry data alongside types, predicates, frames and tolerances. An id whose namespace
is not in the registry is a load error with a position, not a warning.

**The engine attaches no meaning to a namespace beyond existence.** It checks that the
namespace is registered and that the id is unique within it. It does not infer accuracy,
validation, ordering, rendering, or anything else from which namespace an id is in. Two
ids in different namespaces are unrelated; that is the whole of the semantics.

The registry entry for a namespace is a declaration and a description — what authority
issues these ids, and what a local part looks like to a human. The description is for the
person reading the registry, not for the engine.

## Consequences

A typo in a namespace is caught at load, at the position it occurs, rather than becoming a
new namespace with one member. The set of authorities a model draws from is visible in one
file, and growing it is a reviewed change to that file.

Merging two models, or importing a second survey, is a namespace decision made once and
recorded, rather than a collision discovered later.

The engine stays domain-free. A repository modelling something entirely different declares
its own namespaces and gets the same guarantees, because the engine never learned what
`survey` was.

The `namespace:local` form is fixed syntax, so the first colon is structural. A local part
may contain further colons; the split is on the first one only.

## Cost

Verbosity. Every id in every file carries its namespace, including in models that only
ever use one. That is real noise in the source and in the diff.

It also puts a registry edit in front of legitimate work. Someone introducing a new source
of identifiers cannot simply start using it — they add it to the registry first, and if
that registry lives in another repository, that is a second pull request before the first
one can load.

And because the engine attaches no meaning, per-namespace behaviour that genuinely would
be useful — defaulting an accuracy for a class of surveyed point, say — has to be
expressed some other way, as claims or as registry data that names it explicitly.

## What would reverse it

A model that only ever draws from a single authority, forever, would make the namespace
pure overhead — though the cheaper answer there is a registry with one entry, not a change
to the rule.

Opening the set is a one-line change and is not reversible in practice: once unregistered
namespaces load, the model accumulates them, and closing the set again means auditing every
id in the tree and deciding, for each unregistered namespace, whether it was intentional or
a typo — a judgement the data no longer contains.

Teaching the engine what a namespace *means* is the reversal that would be hardest to
undo, because the meaning would leak into query results and check outcomes, and every
model relying on it would have to be re-examined to find out which of its answers came
from the engine knowing something it should not have.
