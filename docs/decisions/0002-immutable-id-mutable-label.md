# 0002. Immutable id, mutable label

**Status:** Accepted

## Context

Things get renamed. A room called `Office 2.14` becomes `Meeting Room B`; a wall type is
reclassified; a site that was `Parcel A` becomes `North Parcel` when a second one is
bought. Every one of those is a change in how people refer to the thing, and none of them
is a change in which thing it is.

If the name is also the identifier, those two changes are indistinguishable to everything
downstream. A rename becomes a delete plus an insert in the diff, every reference has to
be rewritten in the same commit, and any external record that pointed at the old name — an
issue, a report, an export, an as-built photograph filed under the old room number —
silently points at nothing. Worse, if the old name is later reused for a different room,
those external records now point at the wrong thing, and nothing anywhere reports an
error.

Reuse is the failure that costs the most, because it is invisible. A deleted id leaves a
hole; a *reused* id leaves an answer that is wrong and confident.

## Decision

Every node has an **id** and may have a **label**, and they do different jobs.

**An id never changes for the life of the thing it names.** It is assigned once, at
creation, and it is the only thing anything else references.

**A label is display text.** Renaming a thing changes its label and nothing else. A label
carries no uniqueness guarantee, is never referenced, and is never used to resolve
anything.

**Retiring an id is supersession, never deletion or reuse.** A thing that no longer exists
in the model is marked retired, with a pointer to what replaced it where there is a
replacement. The id stays in the graph. It is never removed, and it is never issued again
to a different thing.

## Consequences

A rename is a one-line diff on the node that was renamed. No reference anywhere else moves,
and a reviewer sees exactly what changed.

References are stable across the life of the model. An id captured in an external system —
a photograph, a defect report, an approved drawing, a prior export — resolves to the same
thing years later, or resolves to a retired node that says what happened to it. It never
resolves to something else.

The history of a thing is followable. Because supersession leaves both the retired id and
the pointer to its replacement, "what became of the thing we called X" is a query, not an
archaeology exercise across commits.

Ids are not descriptive, and should not be read as if they were. An id that encodes a room
number is an id that becomes a lie the first time the room is renumbered, so the id
namespace registry (see [0003](./0003-id-namespaces-are-a-closed-registry.md)) governs
form, and nothing in the engine parses meaning out of the local part.

## Cost

Two names for one thing, which means a human reading raw source sees identifiers that do
not tell them what they are looking at, and every tool that shows a node to a person has
to resolve the label to be legible.

Retired nodes accumulate. A long-lived model carries a growing tail of ids that name
nothing currently present, and every query and every check has to be explicit about
whether it includes them.

There is also an authoring cost: getting the id right matters more than getting the label
right, because the label is free to fix later and the id is not.

## What would reverse it

Nothing short of the model ceasing to be referenced from outside itself. As long as
anything external — an issue, a report, an export, a photograph — points at an id, the
guarantee has to hold.

Reversing it is not a migration. Collapsing id and label means picking, for every node,
which of its historical names is *the* name, and every external reference to a name that
loses is broken with no record that it ever pointed anywhere. The information needed to
repair those references does not exist anywhere else, so the reversal destroys it.
