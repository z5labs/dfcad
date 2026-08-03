# 0001. Two node families: semantic and geometric

**Status:** Accepted

## Context

A CAD model has to hold two different sorts of thing. There are the things the domain
talks about — a site, a storey, a wall, a boundary between two spaces — and there are the
points, curves and rings that give those things a shape. Most CAD data models fuse the
two: an element *is* its solid, and its identity, its properties and its geometry live in
one object.

Fusing them is what makes those models hostile to a text-first, diff-reviewed workflow.
Every geometric edit touches the object that carries the semantics, so a reviewer cannot
tell a re-survey of a corner from a change of what the thing *is*. It also forces the
geometry to be as heavyweight as the semantics: a vertex acquires a type, a classification
and a property set it has no use for, purely because there is one node shape and
everything must fit it.

The alternative — geometry as an opaque blob hanging off the semantic node — fails the
other way. Two spaces sharing a wall face have to share the boundary, not each hold a copy
of it, or the model cannot state that they are coincident and the engine cannot check it.
A blob has no addressable interior, so nothing inside it can be shared, referenced or
claimed about.

## Decision

There are exactly two families of node, and the difference between them is structural, not
a flag.

**Semantic nodes** carry a `kind` drawn from the closed set of seven — `Zone`, `Site`,
`Building`, `Storey`, `Space`, `Element`, `Interface` — and a `type` drawn from the type
registry supplied by the consuming repository. They carry claims, containment and
references to geometry.

**Geometric nodes** are `vertex`, `edge` and `loop`. They carry no `kind` and no `type` —
those fields are not optional for a geometric node, they are not part of its shape at all.
A geometric node carries a frame, claims, and references to other geometric nodes: an edge
references vertices, a loop references edges.

**A semantic node references geometry; it never is geometry.** There is no node that is
both, and no path by which a semantic node acquires coordinates directly.

## Consequences

Geometry is shared rather than copied. Two spaces either side of a partition reference the
same loop, and the fact that they are coincident is a property of the graph rather than a
tolerance check between two independent copies that have drifted.

Provenance lands where the measurement was actually made. A surveyed corner is a claim on
a vertex — with its source, method, accuracy and date — and every semantic node whose
outline runs through that corner inherits the accuracy of the thing that was measured, not
an accuracy attributed to the room.

The two families validate under different rules, and the loader can say so precisely. A
`type` on a vertex is an error with a position, not a silently ignored field. A missing
`kind` on a semantic node is likewise an error, because there is no case where a semantic
node legitimately has none.

Renaming or reclassifying an element does not touch a single coordinate, and re-surveying
a corner does not touch a single semantic node. Two kinds of change, two disjoint diffs.

## Cost

Indirection. The shape of a space is not in the space's record; reading it means following
references through loops to edges to vertices, which is more hops for a human reader and
more joins for the engine. Small models feel more verbose than they would with inline
coordinates.

It also means the graph can be inconsistent in ways a fused model cannot: a semantic node
can reference a loop that does not close, or that does not exist. Those are now real
failure modes the loader has to detect and report rather than states that were
unrepresentable.

## What would reverse it

Evidence that the sharing never actually happens — that in practice every loop is
referenced by exactly one semantic node, across real models over real time — would mean
the indirection buys nothing and inline geometry would be both simpler and cheaper.

Unwinding is a one-way flattening. Inlining geometry into semantic nodes is mechanical,
but it destroys the identity of the shared geometric nodes, and with it every claim and
every accuracy figure attached to a specific vertex. The provenance of a coordinate cannot
be reconstructed from a flattened model, so the reversal is a data loss, not a migration.
