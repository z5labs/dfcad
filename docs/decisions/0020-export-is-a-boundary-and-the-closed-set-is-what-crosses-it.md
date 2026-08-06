# 0020. Export is a boundary, and the closed set is what crosses it

**Status:** Accepted

## Context

Export has never been decided, and it has never been forbidden either. It is absent from the
non-goals list in the README, which excludes a database, a daemon, a network transport and
domain vocabulary and stops there, while four accepted records presuppose an exporter rather
than merely permitting one.
[0004](./0004-globalid-derives-from-a-pinned-namespace.md) derives IFC's 22-character
`GlobalId` on every export and calls the derivation "a versioned part of the export
contract"; [0005](./0005-one-linear-unit-per-frame.md) rules that "conversion happens only at
an export boundary … and that is the only place a conversion factor is applied";
[`0000-template.md`](./0000-template.md) lists "an export-boundary change" among the
canonical costs a reversal can carry; and [SPEC §1](../../SPEC.md#1-scope) scopes exported
representations out by giving them a home of their own — "those have their own
specifications". Four records lean on a boundary that no record establishes. The derivation
in `globalid.go` exists, is correct, and is consumed by nothing.

Leaving it there is cheap until the first exporter is written, and then it is expensive,
because the pull request that adds the exporter also has to win the argument about whether
exporters are allowed at all. That argument deserves better than being had under a diff.

**The counter-argument is the one worth answering.**
[0010](./0010-the-engine-carries-no-domain-vocabulary.md) warns that the engine does not
become a building tool by decision: "none of them arrives as a pull request titled 'make
dfcad a building tool'". An IFC exporter is exactly that pull request wearing an output
format's clothes. IFC is a building schema — its entity names are `IfcWall`, `IfcDoor`,
`IfcBeam` — and an exporter has to produce one of those names for every node it writes.
Answering that export is "merely I/O" does not dispose of the objection; a table mapping this
project's type names to IFC classes is domain vocabulary whatever file it lives in, and
calling the file a serialiser does not change what a reviewer would have to read. Only a
design in which the mapping is data, rather than a table compiled into the engine, actually
answers it.

Applying 0010's own test — would this change be meaningless to a repository modelling a
different subject? — gives three different answers, and the line falls exactly where 0001 and
0010 already drew it, between `kind` and `type`.

Emitting `IfcSite` for a node whose `kind` is `Site` consults no type, reads no registry
entry, and depends on nothing a consuming repository supplies. It is a mapping between two
closed sets, both of which are already fixed: `kind` by 0010, IFC's spatial decomposition by
its own schema. A repository modelling a substation has sites, buildings, storeys and spaces
for the same structural reason a repository modelling a house does — that is why those seven
members are compiled in — so the mapping is no more domain-specific than the set it maps
from.

Emitting `IfcWallStandardCase` for a node whose `type` is `partition` requires the engine to
know that a particular type *is* a wall. 0010 forbids that in as many words — "it does not
know that one type is a wall and another is a parcel, and no behaviour anywhere depends on
which" — and the README's routing table sends "names a specific building component,
discipline or code rule" to the data repository, under the rule that a change meaningless to
a repository modelling a different subject is not an engine change. A compiled type-to-class
table fails that test on its first row.

Between the two sits the case that resolves it: an exporter that reads a name a *type
declares about itself* and copies it out. The engine still interprets nothing — it moves two
strings it cannot read — and the vocabulary lands in a registry file whose diff someone
reads, which is the whole of 0010's reviewability argument. That mechanism does not exist
yet. This record rules where the class must come from; it does not design the child that
carries it.

The alternative considered was to put export on the non-goals list and stop. It fails on the
same test it is meant to serve: it does not delete the mapping, it moves it downstream to
somebody's script, where there is no registry, no review rule and no record — and it strands
0004's derivation, whose only purpose is to be read at a boundary this repository would then
have declared it does not have.

## Decision

**Export is a boundary this repository owns, and the boundary is where the engine's
knowledge stops.** An exporter may live here. It is not a non-goal, and a story that adds one
does not have to re-argue that it may exist. What it does have to satisfy is everything
below.

**The mapping from `kind` to its IFC entity is closed, and it is this**, in the order SPEC §1
fixes the set:

| `kind`      | IFC entity                | Related by                                 |
|-------------|---------------------------|--------------------------------------------|
| `Zone`      | `IfcZone`                 | `member-of` → `IfcRelAssignsToGroup`        |
| `Site`      | `IfcSite`                 | `within` → `IfcRelAggregates`               |
| `Building`  | `IfcBuilding`             | `within` → `IfcRelAggregates`               |
| `Storey`    | `IfcBuildingStorey`       | `within` → `IfcRelAggregates`               |
| `Space`     | `IfcSpace`                | `within` → `IfcRelAggregates`               |
| `Element`   | `IfcBuildingElementProxy` | `within` → `IfcRelContainedInSpatialStructure`, or `IfcRelAggregates` inside another `Element` |
| `Interface` | `IfcVirtualElement`       | `within` → `IfcRelContainedInSpatialStructure` |

Four of the seven — `Site`, `Building`, `Storey` and `Space` — are IFC's spatial structure
decomposition one for one, which is not a coincidence: that hierarchy is where the set came
from, and `IfcSite`, `IfcBuilding`, `IfcBuildingStorey` and `IfcSpace` are exactly the
subtypes of `IfcSpatialStructureElement`. The other three land outside it. `Zone` is a
grouping rather than a container, and `IfcZone` is likewise an `IfcGroup` rather than a
spatial element, so `member-of` and `within` stay as distinct on export as
[SPEC §6.9.1](../../SPEC.md#691-the-containment-hierarchy) keeps them. `Element` and
`Interface` map to `IfcElement` subtypes, which is why they are *contained in* the spatial
structure rather than aggregated by it. `Interface` in particular maps to
`IfcVirtualElement`, IFC's own element for a boundary between two things that are not
physically separated, because that is what an interface is and because it is a product with a
placement — a relationship entity could not be written `within` anything.

**A specific element class is registry data, not an engine table.** Every `Element` exports
as `IfcBuildingElementProxy` unless the model itself says otherwise. Anything more precise —
`IfcWall`, `IfcSlab`, `IfcDoor` — names a specific building component, and the README routes
that change to the data repository: *if the change would be meaningless to a repository
modelling a different subject, it is not an engine change*. The engine may copy such a name
from a declaration a type carries; it may not hold a list of them, infer one from a type
name, or special-case any type by name anywhere.

**An exporter reads `kind`, and reads nothing else for meaning.** It may read a `type`,
a predicate, a frame, a claim or a tolerance for exactly the structure the engine already
checks — that a type is registered, that a predicate's value has the declared shape, that a
frame declares one linear unit, that an id resolves — and it may copy their values out
verbatim. It may not branch on which one it got. There is no `switch` on a type name, no
predicate whose name selects an IFC property set, and no frame treated specially because of
what it is called.

**Two things already decided cross this boundary and are restated here as its content.**
Every rooted object carries the `GlobalId` that 0004 derives from the node id and the pinned
project URL, recomputed rather than stored. Every dimensional value is converted here or not
at all, per 0005: the export boundary is the only place a conversion factor is applied, and
an exporter that writes metres from a model authored in feet is doing the one conversion this
system permits.

## Consequences

The first exporter is a story about a file format, not a story about whether the engine is
allowed to have a subject. Its review question is "does it read anything but `kind`", which
is a question a reviewer can answer by reading the code, rather than "is this the change that
makes dfcad a building tool", which is a question nobody can answer from one diff.

0004's derivation acquires a consumer. `GlobalId` stops being a function the engine computes
for no one, and its stability property — two exports of an unchanged node producing identical
identifiers — becomes observable rather than notional.

The `kind` set is now load-bearing in a second direction. It was the vocabulary the engine's
containment and traversal algorithms are written against; it is additionally the vocabulary
the export boundary is written against, and the two must not drift apart.

A model can be exported before its types declare anything, and the result is honest rather
than wrong: a spatial structure with correctly identified, correctly placed, correctly
contained proxies. A receiving tool gets geometry and hierarchy it can use and no claim about
what anything is, which is exactly what the model has said so far.

`Zone` loses any geometry it carries. `IfcZone` is an `IfcGroup` and has no representation,
so a zone's `boundary` has nowhere to go in the exported file. That is a property of IFC's
own model of grouping, not a shortfall in the mapping, and an exporter reports it rather than
inventing a spatial element to hold it.

## Cost

**`kind` acquires an executable IFC meaning, and growing it gets more expensive.** Adding a
member is already an engine change made deliberately and recorded as its own decision — 0010
says so and the README's routing table sends it "here, via an ADR". That route asks one
question: what would the engine's containment, traversal and dispatch do with the new member.
From this record onwards it must also ask what an exporter emits for it, and answer in the
record rather than in the exporter, because the mapping is closed. A kind with no defensible
IFC entity is now an argument against adding the kind, which is a constraint from a foreign
schema on a set that was supposed to be justified by the engine's own algorithms.

The mapping is a compatibility surface with the same character as 0004's derivation. Changing
which entity a `kind` produces changes what every previously exported file said, and the
records above make no allowance for a version of the mapping.

Some of the seven mappings are better than others. `IfcSite` through `IfcSpace` are exact;
`IfcBuildingElementProxy` is the class a receiving tool understands least, so an
unclassified model exports to something usable and unimpressive; and `IfcVirtualElement` is
the closest fit for `Interface` rather than an obviously right one, since IFC models most
interfaces as relationships and dfcad models this one as a node.

Holding the line costs work that a table would not. Anything class-specific has to be
expressed through a mechanism a registry parameterises, which is 0010's stated cost — "more
work and sometimes a worse fit" — paid again at a boundary where the temptation is
concentrated, because the foreign schema is right there naming the classes.

## What would reverse it

A target format whose structure genuinely cannot be reached from `kind` plus copied
declarations — not awkwardly, but genuinely cannot — would be evidence that the line is in
the wrong place. The honest response would be a richer declaration on the registry side, for
the reason 0010 gives: the reviewability argument does not weaken with scale.

**An exporter written against `type` is the reversal with no way back**, and it is worse here
than the general case 0010 describes. There, opening the sets means re-examining every answer
the engine has given to find which ones came from knowledge it should not have had, and the
record notes there is no migration "because the affected answers are not recorded anywhere".
An export's answers are recorded — but not here. They are in a receiving BIM tool, an
approved submission, a facility management database, in files this repository does not hold
and cannot enumerate. Removing a compiled type-to-class table later would mean every affected
`Element` reverting to the proxy in the next export, and because 0004 keeps the `GlobalId`
stable across the change, a receiving system would not see objects deleted and recreated,
which is at least loud. It would see existing objects quietly change class. Every downstream
rule keyed on that class — a schedule, a quantity take-off, a code check — would change its
answer with nothing in the file to say why.

The class of evidence that would justify paying it is narrow: a consuming repository whose
types cannot declare their own external names, in a target format that demands them, with no
registry change that would fix it. Two of those three are within this project's control, so
the honest reading is that the reversal would be a decision to stop being an engine, not a
response to a constraint.
