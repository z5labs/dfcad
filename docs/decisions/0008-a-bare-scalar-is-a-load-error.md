# 0008. A bare scalar where a claim belongs is a load error

**Status:** Accepted

## Context

A claim is a value plus the things that make it trustworthy: its unit, its source, the
method by which it was obtained, its accuracy, its date. That structure is the entire
reason this model exists. Strip it and what is left is a number, indistinguishable from a
number in any other CAD file, and every question the engine is built to answer — how good
is this figure, where did it come from, what does it disagree with — becomes unanswerable.

The structure is also inconvenient at exactly the wrong moments. Someone sketching a
massing study knows the storey height is about three metres and does not have a source for
it. Someone converting a legacy model has ten thousand dimensions and no provenance for any
of them. In both cases the path of least resistance is to write the bare number and move
on.

The gentle response is a lint: accept the bare scalar, warn about it, and let the author
fill in the provenance later. This does not work, and the way it fails is predictable.
Warnings that appear ten thousand times are not read. They are suppressed — a flag, a
config line, an ignore file — and the suppression is added the same afternoon the warnings
start, by someone who fully intends to come back to it. Nobody comes back. Six months
later the model has a provenance system that describes a minority of its values, and the
queries that report accuracy return either nothing or a number that quietly excludes most
of the data. The provenance model was deleted, and no commit in the history says so.

There is a second, sharper reason. The deletion does not look like a deletion in review. A
diff that changes `(thickness (value 0.2 m) (source …) (method …) (accuracy …))` to
`(thickness 0.2)` reads as a simplification. Nothing in it mentions provenance. A reviewer
skimming a hundred such lines sees tidying.

## Decision

**Where the schema requires a claim, a bare scalar is a load error.** It is reported as a
diagnostic with a file, line and column, the load fails, and no query runs against the
file.

**It is not a lint, a warning, or a check that can be waived.** There is no flag, no
per-file suppression, no severity setting that turns it into a warning. The distinction
between a diagnostic that fails the load and one that does not is a property of the rule,
not a configuration.

**The diagnostic says what is missing and where**, in the terms of
[CLAUDE.md](../../CLAUDE.md#diagnostics): what was expected, what was found, and the
position of the offending value — not "invalid claim".

**The remedy for not knowing the provenance is to state that**, using the vocabulary the
registry provides for it — an estimate is a method, and an assumption has an author and a
date. What is unavailable is writing the number as though the question had not been asked.

## Consequences

Every value in the model that is supposed to carry provenance carries it, with no
qualification and no subset to remember. Queries over accuracy, source and date cover the
whole model, and their answers do not silently exclude anything.

The rule cannot decay. There is no accumulating backlog of warnings, no ignore file, and no
gradient down which the model can slide — the state where half the values have provenance
is not reachable.

Uncertainty is visible as uncertainty. A dimension that was guessed says it was guessed,
and the conflict register and the error budget can both take it into account. Under a lint,
the same dimension would be a bare number indistinguishable from a surveyed one.

Bulk import has to make a decision at the boundary. Ten thousand legacy dimensions arrive
with a stated method — "imported from drawing X, dated Y, accuracy unknown" — which is a
true statement, recorded once, rather than ten thousand numbers with nothing attached.

## Cost

Friction, at the moments when friction is least welcome. A sketch that would be five lines
is fifteen. An import that would be a script is a script plus a conversation about what the
source actually is. Some of that friction is the point; not all of it is, and it is paid
by everyone including the people who would have done the right thing anyway.

It also raises the floor for a first-time author. There is no gentle on-ramp where the
model half-works — the first file either carries provenance or does not load, and the
learning happens before the first success rather than after it.

## What would reverse it

A demonstration that the friction stops models from being started at all — that the real
outcome is not "provenance everywhere" but "no model, because the sketch was done in
something else" — would be a genuine reason to reconsider, though the better answer would
be authoring tools that make the structure cheap to write — which is the bet the command
line write path is making — rather than a schema that makes it optional.

Downgrading it to a lint would be a one-line change and is not reversible in practice. Once
bare scalars load, they accumulate, and restoring the rule means finding the provenance for
every value written in the interim — from people who no longer remember, about drawings
that have since been superseded. The information does not exist in the model, because the
whole point of the bare scalar was that nobody recorded it.
