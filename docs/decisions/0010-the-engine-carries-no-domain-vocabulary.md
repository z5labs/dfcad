# 0010. The engine carries no domain vocabulary

**Status:** Accepted

## Context

Every CAD or BIM system that has ever been written accumulated a vocabulary. It started
with a handful of obviously-necessary concepts — a wall, a door, a storey — and it grew,
because each addition was individually reasonable and none of them was the one that made
the system a building tool rather than a modelling tool. By the time anyone noticed, the
vocabulary was the product, and a repository modelling a substation, a pipeline or a
farm was either using terms that meant something else or was not using the system at all.

The failure is not that somebody decided to make the engine domain-specific. Nobody
decides that. It happens because a domain concept is easiest to add exactly where it is
needed, and where it is needed is usually in the engine: the query that has to know a
setback from a boundary, the validation that knows a wall thickness is not negative, the
default accuracy that only makes sense for a surveyed point. Each of those is a small
patch to a file that already exists. None of them arrives as a pull request titled "make
dfcad a building tool".

The second-order problem is reviewability. A vocabulary compiled into the engine grows
through code changes, and a code change adding a `const` to a list is nearly invisible in
review — it looks like plumbing. The same growth expressed as a registry file is a diff
that someone reads as a statement about the model, because that is all the file contains.

Some structure genuinely has to be compiled in, or the engine has no shape at all. The
question is which, and the answer is: the structure the engine's own algorithms depend on,
and nothing else.

## Decision

**Two vocabularies are closed and compiled into the engine.**

- `kind`: `Zone`, `Site`, `Building`, `Storey`, `Space`, `Element`, `Interface`.
- `geometry`: `point`, `line`, `area`, `surface`, `solid`, or absent.

These are compiled in because containment, traversal and geometric dispatch are written
against them: the engine's own code branches on a node's kind and on the dimensionality of
its geometry, and a member added at runtime would be a member no algorithm knows what to
do with. Adding a member to either set is an engine change, made deliberately and recorded
as its own decision record. Both sets are meant to stay small.

Two further sets are fixed by the engine for the same reason, and are recorded elsewhere:
the node families ([0001](./0001-two-node-families.md)) and the check registry
([0011](./0011-assertions-are-named-parameterised-checks.md)).

**Everything else arrives as registry data**, supplied by the consuming repository and
validated by the engine before it interprets anything:

| Registry         | Declares                                                                  |
|------------------|---------------------------------------------------------------------------|
| Types            | Each `type` with its permitted `kind`, permitted `geometry` and description |
| Claim predicates | Each predicate with its unit, value shape and validation rules             |
| Frames           | Each frame with its linear unit and its parent                             |
| Id namespaces    | The permitted namespaces for `namespace:local` identifiers                 |
| Tolerances       | Named tolerances with values and units                                     |

**The engine attaches no meaning to a registry entry beyond the structure it checks.** It
verifies that a `type` is registered and that its `kind` and `geometry` are permitted; it
does not know that one type is a wall and another is a parcel, and no behaviour anywhere
depends on which. The same holds for predicates, frames, namespaces and tolerances: the
engine checks shape, uniqueness and reference, and stops.

**The test for where a change belongs is whether it would be meaningless to a repository
modelling a different subject.** A change that names a specific building component, a
discipline, a code rule, or a numeric tolerance belongs in the data repository. A change
that adds a query, an output field, a resolution rule or a check any model could use
belongs in the engine.

## Consequences

Vocabulary growth is reviewable. Adding a type is a diff to a registry file whose entire
content is vocabulary, read by someone who can judge whether the model needs another term.
It cannot arrive as a side effect of a refactor.

The engine is testable without a domain. Its test fixtures declare whatever registry they
need, so a test for claim resolution does not have to pick a real building component to
resolve claims about, and a test never encodes an assumption about what a type means.

A repository modelling something else gets the same guarantees. There is nothing to strip
out and nothing to work around, because the engine never learned the first domain.

The registry becomes a load-time dependency: the engine cannot interpret an entity file
without first loading and validating the registries it references. Registry errors are
diagnostics with positions, like any other input problem.

An unregistered type, predicate, frame, namespace or tolerance is a load error with a
position, not a warning and not an implicit registration.

## Cost

Indirection, paid on every read. Understanding what a model says requires the entity files
and the registries together, and the interesting semantics — what a type may contain, what
a predicate's unit is — are one file away from where they are used.

Two repositories, and therefore two pull requests, for work that is conceptually one
change. Introducing a new type means a registry change that must land before the entity
file using it can load. That is friction on exactly the routine work people do most.

Genuinely useful domain behaviour has to be expressed some other way or not at all. A
validation rule that only makes sense for one kind of component cannot be a special case in
the engine; it has to be a general mechanism the registry parameterises, which is more work
and sometimes a worse fit.

And the closed sets will occasionally be wrong. A model that needs a semantic kind the
engine does not have is stuck until the set grows, and growing it is a deliberate,
recorded, engine-side change — slow by design, and slow is a cost even when it is the right
call.

## What would reverse it

A second serious consumer whose needs cannot be expressed through the registries — not
awkwardly, but genuinely cannot — would be evidence that the split is in the wrong place.
The honest response to that is usually a richer registry schema rather than compiled-in
vocabulary, because the reviewability argument does not weaken with scale.

Opening the closed sets, or letting the engine attach meaning to registry entries, is the
reversal that cannot be undone. Once a query's answer depends on the engine knowing what a
particular type is, that knowledge is load-bearing for every model that relies on the
query, and removing it means re-examining every answer the engine has given to find out
which ones came from something it should not have known. There is no migration for that,
because the affected answers are not recorded anywhere.
