# 0027. An element fills an opening because its type says so

**Status:** Accepted

## Context

A door set in a wall reached the IFC file standing inside a solid wall, related to nothing.
The model said the door was `within` the wall and drew the door's run along the wall's; the
export wrote the wall as one uncut solid and the door as a peer of it in the storey, because
the containment walk went up to the nearest spatial node on purpose and wrote nothing for the
edge it passed. A reader subtracting openings had nothing to subtract, so every wall was drawn
solid through its doors — and a consumer replacing a FreeCAD-authored file carrying fifteen
`IfcOpeningElement`s with this export would have lost every one of them.

Two things were missing, and they are the two things one `within` edge between elements can
mean. [0020](./0020-export-is-a-boundary-and-the-closed-set-is-what-crosses-it.md)'s table
says an `Element` inside another `Element` is related by `IfcRelAggregates`: the inner one is
a *part* of the outer, as a mullion is of a curtain wall. IFC states a door in a wall
differently: the wall is voided by an `IfcOpeningElement` (`IfcRelVoidsElement`), the door
fills it (`IfcRelFillsElement`), and the door stays contained in its storey. For a door the two
pull in opposite directions, so the export has to know which one an element is — and
[SPEC §6.9.1](../../SPEC.md#691-the-containment-hierarchy) keeps `within` to one meaning,
physical enclosure, which says nothing about which.

Whatever decides it is constrained by 0020 and by
[0010](./0010-the-engine-carries-no-domain-vocabulary.md). Writing voids-and-fills because a
type is classified `IfcDoor` would be the `switch` on a type name 0020 rules out, spelled in
IFC's vocabulary rather than this project's; so would a list of classifications that open
their host. The mark has to be something the model declares.

Three places were live.

- **An export flag naming a predicate**, the way `--height` and `--offset` name the claims a
  body is built from. It fits the export's existing shape, but a predicate carries a value,
  and there is no value here: a claim of `(fills "yes")` on every door node is a marker
  dressed as a measurement, with a source, a method and an accuracy that mean nothing, and it
  has to be repeated on every instance.
- **A second relationship on the node**, `(fills site:W-01)` beside or instead of `within`.
  It states the right thing, but on every instance, and it is a new reference form with its
  own resolution, cycle and kind rules — a second containment edge the hierarchy would have to
  reconcile with the first.
- **A declaration on the type.** Whether a door fills a hole in what it is set in is a fact
  about doors in this project, not about one door. A type already declares what its
  instances may be — their kinds, their geometry forms — and this is one more thing of the
  same character: structural, closed and meaningless to nothing.

## Decision

**A type declares that its instances fill an opening, with `(fills-opening #t)`**, a boolean
child of [SPEC §7.3](../../SPEC.md#73-type) whose default, `#f`, is omitted in canonical form.
The format's version moves to 1.3, which is a MINOR change: an optional child was added and
every file that loaded still loads and prints to the same bytes.

It is legal only on a type permitting the kind `Element`, because only an `Element` is written
within an `Element`; on any other type it is a registry error.

**An `Element` within an `Element` is written by what its type declares**, and nothing else:

| The inner element's type | Written as                                                                 |
|--------------------------|----------------------------------------------------------------------------|
| says nothing, or `#f`    | A part: `IfcRelAggregates` from the outer element to it, and no containment in the storey, which it reaches through its whole. This is 0020's table as written. |
| `(fills-opening #t)`     | A filling: contained in its storey like any product, plus one `IfcOpeningElement` voiding the outer element (`IfcRelVoidsElement`) which it fills (`IfcRelFillsElement`). |

An element of a filling type written within anything other than an element is contained as
any element is. There is nothing there to cut.

**The opening's shape is the filling's, through the host.** Where both are drawn as lines and
the host has a body, the void is the filling's run widened by the *host's* thickness — a door
is thinner than its wall and the hole goes through the wall — and swept from the filling's
own base, offset included, through the filling's own height. A filling whose run does not lie
along its host's run in plan, within the corner tolerance widened by the chord tolerance, is
refused naming both, rather than voiding the host somewhere the model does not put the door.
Where there is no body to cut, the relationships are written without a shape; where the host
has a body but the filling has nothing to cut to, that is a warning and the same.

**An opening is not a node, and its identifiers are derived from its filling's id** under
names no node id can carry — `ifc/opening/<id>`, `ifc/voids/<id>`, `ifc/fills/<id>` — so each
is stable across exports of an unchanged tree and distinct from the filling's own, per
[0004](./0004-globalid-derives-from-a-pinned-namespace.md).

## Consequences

The exporter reads one more thing off a type, and it reads it the way 0020 permits: a
structural declaration copied into structure, never a name branched on. A type called `Door`
classified `IfcDoor` which says nothing is a part of its wall, and a type called anything
which says `#t` is cut into it — which is the review question, "does it read anything but the
declaration", answerable from the code.

A consumer marks its fillings once per type, not once per door. Nineteen doors of three types
are three lines of registry.

0020's table stands: `IfcRelAggregates` is still what an element inside another is written
as, and this record adds the one case the table's single row could not distinguish.

## Cost

**The format grew for an exporter.** Nothing in the engine's own containment, traversal or
checks reads `fills-opening`; its only reader today is the IFC export. That is a child of the
format justified by one consumer, which is exactly what 0010 warns a closed set accumulates.
The defence is that the statement is structural and true of the model whether or not anybody
exports it — a door in a wall *is* a hole in the wall — but it is a defence, not an absence of
cost.

**The mark is per type, and a type that sometimes fills and sometimes does not cannot say
so.** A panel that is a part of one assembly and set into an opening of another needs two
types. That is the price of not putting a second relationship on every node.

**The opening's geometry is narrow.** Only a filling drawn as a line along a host drawn as a
line is cut, with one void per straight piece of the filling's run. A skylight drawn as a ring
in a roof slab is related to it and not cut, and says so. Cutting an area out of an area is a
story of its own.

## What would reverse it

A consuming repository whose elements fill openings in some hosts and are parts of others,
often enough that doubling types is the larger cost, would argue for the relationship on the
node instead. Moving there means a new reference form beside `within`, a migration writing it
on every instance of every `fills-opening` type, and a MAJOR version once the child is
removed. Every exported file already written would read the same, because the identifiers are
derived from the filling's id rather than from how it was marked.
