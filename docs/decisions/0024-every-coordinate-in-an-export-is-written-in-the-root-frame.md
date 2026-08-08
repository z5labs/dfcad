# 0024. Every coordinate in an export is written in the root frame

**Status:** Accepted

## Context

[0023](./0023-the-map-export-names-its-coordinate-system-in-the-file.md) states the rule for the
map export: *"A region outlined on a non-root frame is carried into the root frame by the chain
of measured transforms the model states … Where the chain does not reach, the export is
refused."* No record stated one for the model export, and the model export did not do it.

The result was two exporters which disagreed about where one model is. Same tree, same
predicates, same run:

| command | coordinates written |
|---------|---------------------|
| `dfcad export-map` | `<gml:posList>3502100 552000 3502104 552000 …` |
| `dfcad export` | `#30=IFCCARTESIANPOINT((0.,0.));` `#31=…((4.,0.))` |

and, in the model file beside those coordinates:

```
#21=IFCMAPCONVERSION(#17,#20,0.,0.,0.,$,$,$);
#20=IFCPROJECTEDCRS('EPSG:6543',…);
```

The conversion is the identity because a writer wrote the identity, and the identity is correct
only if the file's coordinates are already the projected system's — which holds when every
shape was authored directly in the root frame, and fails for any model with a plan frame per
level. That is not an exotic arrangement; it is the ordinary shape of a building, and it is
what the sibling story [0005](./0005-one-linear-unit-per-frame.md) and the frame graph exist
to support. The file then asserted that a room was at the system's origin, a couple of million
feet from where the model puts it, and said nothing about it anywhere. A georeferenced file
which is wrong is worse than an ungeoreferenced one, for the reason 0023 gives about GeoJSON:
it renders, at a plausible scale, in the wrong place.

Two things had to be settled, and they are independent.

**Which coordinates go in the file.** Either the corners as authored, with the transform
carried some other way, or the corners carried into one frame. IFC does offer the other way:
a shape hangs off a chain of `IfcLocalPlacement`, so a plan frame's rigid transform could be
written into the placement of the storey and every corner left as drawn. That was live, and it
is what a storey's elevation attribute and placement already do for the third coordinate. It
was rejected as the general rule for two reasons. A frame transform is a
similarity — rotation, translation and a scale — and `IfcLocalPlacement` carries no scale, so
a chain which found one could not be written at all. And a placement carries the transform for
everything beneath it in the *spatial* tree, while a frame is declared per shape: a wall set
out on the fabrication grid inside a storey on the plan grid is two frames and one storey, and
there is no placement which is both.

**What the map conversion then says.** Once the coordinates are the root frame's, and the
coordinate reference system is refused anywhere but the root frame, nothing is left between
them. The identity is right — which is what made this bug survive: the value that was being
written was correct, and the reason it was correct was not true yet.

## Decision

**Every coordinate an export writes is expressed in the frame the model's chain is rooted at,
carried there by the chain of measured transforms, and the file's placements carry structure
rather than coordinates.** This is 0023's rule for the map, stated once for both exporters.

Four things follow and are fixed by this record.

- **The carry is per drawing and by the model's own transforms.** A region, a run of a node
  drawn as a line, and the connection curve cut out of a space boundary are each expressed in
  the root frame by `Frames.TransformPoint`, which is the same walk `dfcad.Region.In` makes
  corner by corner and the same one `dfcad site` answers with. Nothing reprojects and nothing
  is converted; the transform is a similarity in the plane the survey was already projected
  into.
- **A chain which does not reach the root refuses the shape, naming the frame.** Not the node
  alone: the frame is what has to be fixed, because a shape is drawn where its vertices were
  measured and a chain which does not reach the root is a missing transform between two
  frames. An export which collected an error writes no file
  ([0016](./0016-writes-are-all-or-nothing.md)), so a model this cannot place is a refusal
  rather than a file with a room at the origin.
- **A placement states what is left over, and never the same lift twice.** A storey still
  stands at the elevation its frame chain puts it at, because a reader needs to be told which
  level is which. Since the coordinates beneath it are already in the root frame, what is
  written on a shape is the root-frame value less the datum its placements already stand at —
  so the two compose to the coordinate the model states, rather than to twice the lift.
- **The map conversion states the offset which is left, and is asked for rather than
  assumed.** The system is named on the root frame and refused on any other; the coordinates
  are carried into the root frame; so the offset between them is nothing, and the conversion
  is the identity. The three factors stay absent, because a rotation or a scale between the
  model and the system would be geodesy, which this engine does not do.

A model authored entirely in the root frame is unaffected in every particular: the carry is
the identity, the datum is nought, and the bytes are the bytes it produced before.

## Consequences

The two exporters agree about where a model is, and the agreement is testable in one assertion
rather than inferred from two goldens: the corners of the map document are the corners of the
model file. A test which reads both is what keeps them in step, which is the cost 0023 named
and did not have a check for.

A consuming repository can author the way it wants to author. Levels get their own plan grids,
setting-out grids hang off the site grid, every corner is drawn at nought where it was
measured, and both artefacts come out placed on the earth. Nothing about the entity files
changes to get that.

The coordinates in a georeferenced model file are large — eastings of a few million in a
state-plane foot system — because that is what the root frame's coordinates are. A reader
which loses precision on them was already losing it on a model authored directly in the root
frame; what changed is that more models now reach that path.

A refusal names a frame rather than only a node, which points at the registry where the
transform is missing rather than at the entity file where the room is.

## Cost

**Large coordinates in the model file.** The IFC ecosystem's own advice is to keep model
coordinates near the origin and put the offset in `IfcMapConversion`, which is exactly the
arrangement this record does not adopt. Taking that advice would mean choosing a local origin,
which is a number nobody measured, and it would change the file a root-authored model produces
today. The offset is available to a reader either way — from the conversion under that
arrangement, from the coordinates under this one.

**Arithmetic per corner, per run.** Every corner of every shape goes through the frame walk on
every export, where it used to be copied. The artefact is keyed on its source digest
([0021](./0021-an-export-is-a-build-output-keyed-by-its-source-digest.md)), so this has to be
byte-identical run to run, which it is only because the walk is deterministic and nothing in it
reads a clock or a map order. That is now a property under test rather than an obvious one.

**A datum threaded through the spatial walk.** The export carries down how far the placements
above a shape already stand, and subtracts it. That is a second place where the elevation of a
storey appears, and the two have to agree; a change to how a storey is placed which forgets it
puts every room on that level at twice its height.

## What would reverse it

A frame graph whose transforms were no longer similarities — anything with a projection or a
datum shift in it — would make "carried into the root frame" a geodetic operation rather than
arithmetic, and the decision not to do geodesy would then force the transform back into the
placements or out of the file entirely. That would need a record of its own, and 0023's
argument would reopen with it.

Evidence that receiving systems lose usable precision on root-frame coordinates would argue
for a local origin with the offset in `IfcMapConversion`. Unwinding to that is a change to
every coordinate in every artefact — a re-derivation from the model rather than a migration —
but nothing is unrecoverable: the model holds the frames, and the artefact is a build output.

A decision to write frame transforms into `IfcLocalPlacement` after all would need IFC to
carry a scale on a placement, or this engine to refuse a frame transform which found one.
