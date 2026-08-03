# 0009. Derived values are never written back to source

**Status:** Accepted

## Context

A great deal of what the engine computes is expensive: the area of a space, the centroid of
a parcel, a tessellated arc, a setback-constrained buildable region, a terrain surface
derived from thousands of observation points. Computing those on every invocation is slow,
and the obvious remedy is to keep the answer.

The tempting place to keep it is in the source file, next to the thing it describes. It is
convenient — the area is right there when you open the file, no separate artefact to
manage, no cache to invalidate — and it destroys the property the whole model rests on.

The moment a derived value sits in the source tree, the tree has two kinds of content that
look identical and are not. One is authored: someone asserted it, and it is true because
they said so. The other is computed: it is true only if it was computed from the current
state of its inputs. Nothing in the file distinguishes them. A diff shows both. A reviewer
approves both. And the derived one is *stale by default* — the instant an input vertex
moves, the recorded area is wrong, and it is wrong in the most durable possible way,
because it is now sitting in the repository looking exactly as authoritative as the
coordinates it disagrees with.

It gets worse when someone edits it. A stored area that a person has adjusted by hand is
indistinguishable from one the engine wrote, so the next recomputation silently discards a
human's decision, or — if the engine is careful and refuses to overwrite — the model
permanently holds an area that does not match its own boundary. Both outcomes are bad, and
which one happens depends on an implementation detail nobody remembers.

The diff noise is a separate cost. A one-vertex change that alters an area recomputes every
downstream figure, so a one-line semantic edit arrives as a two-hundred-line diff, and
review of the one line that matters stops happening.

## Decision

**Derived values are never written into the authored source tree.** No area, length,
centroid, bounding box, tessellation, membership set, derived surface or propagated
accuracy is stored in an entity file. Not as a field, not as a comment, not as a sidecar
file inside the source tree.

**Derived data is a build output.** It is written to a location outside the authored tree,
and it is disposable: deleting all of it costs time and nothing else, because every value
in it can be recomputed from the source.

**A derived artefact is keyed by the digest of the source it was derived from.** The cache
key is a digest over the source tree the derivation read, so a cached value is used only
when its inputs are bit-for-bit what produced it. A changed input produces a different key
and therefore a miss, not a stale hit. There is no invalidation logic to get wrong — the key
*is* the invalidation.

**Every derived value the engine reports says it is derived**, and reports the digest it
was computed against, so a consumer can tell an assertion from a computation and can check
that the computation matches the source in front of them.

## Consequences

The source tree is exactly the set of things somebody asserted. Reviewing a diff is
reviewing decisions, and every line in it is a line a human meant.

Stale derived data is unrepresentable. There is no state in which a recorded area
contradicts the boundary it came from, because the recorded area is not in the model — the
worst case is a cache miss and a recomputation.

Diffs stay proportional to the edit. Moving one vertex is a one-line diff regardless of how
many derived quantities depend on it.

The build is reproducible and cacheable in the ordinary way. The same source produces the
same derived artefacts on any machine, which is what makes CI checks over derived
quantities trustworthy — and it is the same property that lets a check run against only
what changed.

Nothing needs a database. The source tree plus a content-addressed cache is the whole
storage model, which is one of the engine's stated non-goals holding up under load.

## Cost

Recomputation. A cold cache pays the full cost of every derivation, and for the expensive
ones — terrain surfaces, large boolean regions — that is not a small number. Anything that
reads a derived value has to be prepared for it to be computed rather than looked up.

There is no way to pin a derived value. A model that wants to record "the area as computed
and signed off on this date" cannot do it by storing the number as derived data; it has to
assert it as a claim, with its own source and method, at which point it is an authored
statement that may disagree with the computation — which is the conflict register doing its
job, but it is a different and more deliberate act than editing a field.

The digest has to cover exactly the inputs a derivation actually read, and getting that
wrong is a correctness bug with a nasty signature: too narrow, and a stale value is served
against changed inputs; too broad, and the cache misses constantly and the whole mechanism
stops paying for itself.

## What would reverse it

A derivation so expensive that recomputation is not viable even with a warm cache — and
whose result is genuinely stable across the edits people actually make — would be an
argument for a checked-in artefact. The honest form of that is not a value written back
into an entity file; it is a separate, clearly-labelled generated file, carrying its source
digest, that CI regenerates and fails on if it does not match. That keeps the "stale is
detectable" property, which is the part that must not be given up.

Reversing it properly is not a migration. Once derived values live in the source tree,
telling them apart from authored ones requires knowing which fields were derived *at the
time each was written*, and if any of them were hand-adjusted, the human intent behind the
adjustment exists nowhere else. Extracting derived data back out of the tree therefore
either discards those edits or preserves values nobody can justify.
