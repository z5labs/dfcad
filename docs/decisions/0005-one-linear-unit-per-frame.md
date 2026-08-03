# 0005. One linear unit per frame, foot definition pinned

**Status:** Accepted

## Context

A model that spans a survey, a site plan and a building interior spans more than one
coordinate frame, and in practice more than one unit: the survey arrives in feet on a state
plane grid, the building is modelled in millimetres, the site drawing is in metres.

The failure mode is not conversion — conversion is arithmetic. It is *unlabelled*
conversion. A number written as `3.5` is meaningless without a unit, and once it is stored
without one, the unit lives in whoever wrote it. Every subsequent reader guesses, usually
correctly, until one guesses wrong. Mixed-unit models fail this way silently and
expensively, and the failure is discovered on site.

Converting eagerly — normalising everything to metres on load — looks like the fix and is
not. It destroys the authored value. A dimension surveyed and recorded as `12.00 ft`
becomes `3.6576 m`, and every later export back to feet reproduces it as `12.000000000001`
or `11.999999999998` depending on the route taken. The recorded number is no longer the
number anyone measured, and the round-trip property the format is built on
(see [CLAUDE.md](../../CLAUDE.md#testing)) no longer holds.

Then there is the foot. There are two of them in current use in the United States, they
differ by 2 parts per million, and nothing in a file that says `ft` distinguishes them.

## Decision

**Every frame declares exactly one linear unit.** The frame registry entry carries it. A
frame with no declared unit is a registry error; a frame with two is unrepresentable.

**Every claim carries a unit.** Not "may carry" — a dimensional value without a unit is a
load error, of the same family as [0008](./0008-a-bare-scalar-is-a-load-error.md). The
claim's unit must be the unit of its frame, or a unit the registry declares as convertible
within it.

**Conversion happens only at an export boundary.** The loader converts nothing. Queries
answer in the unit the value was authored in, and say which unit that is. A caller that
wants metres asks for metres at the point of output, and that is the only place a
conversion factor is applied.

**The foot is pinned to the international foot: `1 ft = 0.3048 m`, exactly.** This is the
definition of `ft`.

**The US survey foot is a separate unit, `usft`, defined as `1 usft = 1200/3937 m`
(0.30480060960121920… m), and it is never a synonym for `ft`.** A registry may declare it;
nothing may treat the two as interchangeable.

**The magnitude of the distinction is 2 parts per million** — one part in 500,000, the
`usft` being the longer. That is:

| Over…                                          | The two feet differ by |
|------------------------------------------------|------------------------|
| 1 km                                           | 2 mm                   |
| 10,000 ft (≈ 3 km)                             | 0.02 ft (≈ 6 mm)       |
| a State Plane coordinate of 2,000,000 ft       | 4 ft                   |

The last row is the one that matters. The error is proportional to the *coordinate value*,
not to the size of the building, and state plane coordinates are large numbers. A 2 ppm
error on a local dimension is invisible; the same error on a northing puts the whole site
several feet from where it belongs. The United States retired the survey foot for new work
at the end of 2022, which means data authored on either side of that date exists and is
not self-describing — exactly why the model has to be.

## Consequences

Authored numbers survive. A value written as `12.00 ft` is stored, queried and printed as
`12.00 ft`, and parse-print-parse round-trips exactly.

Every value in the model is self-describing. A number lifted out of a file, a query result
or an error message carries its unit with it, so there is no context in which the unit has
to be recovered from convention.

Conversion is one code path, at one boundary, with one place to test. Accumulated
floating-point drift from repeated conversion cannot occur, because repeated conversion
cannot occur.

Mixing units within a frame is caught at load with a position, rather than producing a
number that is off by a factor of a thousand and looks plausible.

The foot ambiguity becomes representable and therefore checkable. A model that has to hold
both can hold both, and the engine can tell them apart, which is the only way the 4 ft is
ever noticed before it costs something.

## Cost

Verbosity, again: a unit on every claim, including the overwhelming majority where it is
the same as the last one.

Arithmetic across frames has to convert, and the engine has to be explicit about where
that happens rather than assuming a common basis. Comparing a value in one frame against a
value in another is never free.

Pinning `ft` to the international foot means a repository whose survey data is in survey
feet must say `usft` and mean it. Data that arrived saying `ft` and meant `usft` will be
wrong, and the engine cannot detect that — it can only decline to let the ambiguity
continue past the point of authoring.

## What would reverse it

A model confined to a single frame and a single unit for its whole life would make the
per-claim unit pure overhead. That is a smaller model than this engine is for, and the
cost of keeping the unit is bounded, so this is a weak reversal.

Eager normalisation would reverse the "convert only at export" half. It would take a
migration that rewrites every claim, and it destroys the authored values in the process:
the original number cannot be recovered from the normalised one, because the conversion is
not exact in binary floating point. That is data loss, not a migration.

Redefining `ft` is not available at any price. Every stored value would change meaning
without changing text, which is the one failure mode this record exists to make
impossible. If the pinned definition ever had to change, it would be by introducing a new
unit name and migrating claims to it explicitly, one at a time, with the old name left
meaning what it always meant.
