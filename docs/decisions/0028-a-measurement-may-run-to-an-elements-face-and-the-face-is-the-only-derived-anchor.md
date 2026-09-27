# 0028. A measurement may run to an element's face, and the face is the only derived anchor

**Status:** Accepted

## Context

A measurement taken on site runs between two things. Today the format can say what those
things are in exactly one case: when both are corners joined by an edge. A claim written on
an edge is a measurement between its two vertices, and
[`claim-agrees-with-geometry`](../../SPEC.md#68-assert) checks it against the distance those
vertices' positions give. `dfcad plan --annotate` reports every such claim with an anchor of
one of two kinds, `edge` or `node`, and nothing else can be anchored.

A tape does not only run between corners. It runs from a corner to the far side of a wall,
between the two faces of a room, from a corner to where a wall would reach if it carried on.
None of those ends is a vertex, so none of those readings can be a claim. What happens to
them instead is the failure this record exists for: the reading is converted by hand into a
figure the model can hold — a corner moved by half a wall, a room caption — and the
conversion is written down as though it had been measured. Nothing compares the caption with
the room, because the caption is not attached to anything the engine can compute. That is
how one house came to be dimensioned face to face on one level and centreline to centreline
on the other, and how one of its rooms carries 31.49 usft² between centrelines and 27.40
between faces, a 13% difference nothing in the model could see.

ISO 10303 AP242 names this problem. It calls the points a measurement runs to *shape
aspects*, marks each one either *integral* — a physical feature of the product, such as a
face — or *derived* — a constructed one, such as a centreline — and lists three derived
points: a point along an edge, the intersection of two lines extended past their segments,
and the projection of a point onto a line. The format has neither the list nor the mark.

This record was deliberately not written until a consuming repository had tried the cheaper
thing first: author every point a measurement runs to as an ordinary vertex, with a position
claim carrying its source, method and accuracy, and report what could not be authored that
way. `Zaba505/mi-casa` did that for both levels of a real house (mi-casa#87, measured
against dfcad `af78277`). Its 62 dimension strings have 124 ends. The result:

| | Ends |
|---|--:|
| An authored corner | 46 |
| The intersection of two lines — edges extended, or faces | 43 |
| No point at all: the string crosses one line, and where is layout | 22 |
| A point the product defines — a window's centre | 12 |
| The foot of a perpendicular from a corner | 1 |
| A point at a parameter along an edge | **0** |

The cheaper thing worked where it applied. Every one of the 66 jambs of the 33 openings is an
authored corner which splits its wall's run, so the wall and the opening share the point, and
`plan` returns every opening as a run. The expectation that motivated the story — that most
of the residue would be stations along an edge — did not hold at all. Twenty-two residue
points do lie inside an authored edge, but each is fixed by other geometry, and authoring it
as a vertex would state a derived figure as though it had been measured.

What the strings measure between is the more useful table, because every one of them is
orthogonal and so each measures a distance across something:

| Between | Strings |
|---|--:|
| Corner and corner | 17 |
| **Face and face** | **27** |
| Corner and an opening's centre | 6 |
| Corner and an edge's line, carried past its end in 3 of 5 | 5 |
| Opening centre and opening centre | 3 |
| Corner and a face | 2 |
| An edge's line and a face | 2 |

Thirty-one of the 45 strings that do not run between two corners run to a face. That is the
need, and it is the integral-versus-derived distinction in AP242's own terms: the engine
already reads the run of an element drawn as a line as its centreline, widened by its
thickness half either side
([`dfcad export --thickness`](../../cmd/dfcad/export.go)), so every authored line along a
wall is a derived feature of it, and the face a tape reads is the integral one. The format can
name the first and not the second.

The question is not settled by [0010](./0010-the-engine-carries-no-domain-vocabulary.md). An
anchor names no building component, discipline or code rule; a face of something drawn as a
line with a thickness is as meaningful for a fence, a kerb or a duct as for a wall. It passes
0010's test, so 0010 cannot be the record that admits or refuses it. What decides it is the
test [0007](./0007-rank-is-closed.md) and
[0011](./0011-assertions-are-named-parameterised-checks.md) already apply: whether what is
stated can be wrong, and whether the engine can tell.

## Decision

**A measurement's end is model data when the engine can compute where it lies from other
model data**, because then a claim about the distance to it can disagree with the model and
the engine can say so. An end the engine cannot place that way is not model data, however
precisely it was measured, because a claim about it can never be caught being wrong.

**A derived anchor is representable, and the set is closed with one member: the face.**

- **A face** is the physical surface of an element drawn as a line, on one side of a straight
  edge of that element's run: the edge's line, offset to the named side by half of the
  element's thickness. It is named by the element, the edge and the side — left or right of
  the edge's start-to-end order, which the format already preserves and never sorts. Which
  predicate states a thickness is named by whoever reads the face, exactly as a check names
  the predicate a corner's position is claimed under and `--thickness` names it for the
  export; it is project vocabulary and is not written into the anchor. A face is a line, not a
  segment: it does not stop where the edge's corners do.

A face of an edge that is not in the named element's run, of an element not drawn as a line,
or of an edge a curvature predicate bends, is refused naming the reference.

**A measurement between two ends is a geometric form of its own.** It carries an id, a frame,
exactly two ends and claims, as an edge does; it has no topology — it is in no loop, bounds
nothing, is backed by nothing and draws nothing. Each end is one of exactly three things:

| End      | What the distance runs to                                          | Integral or derived |
|----------|--------------------------------------------------------------------|---------------------|
| A vertex | Its resolved position.                                             | As authored         |
| An edge  | The whole line through its two corners, not only the segment.      | As authored — for an element drawn as a line, its centreline |
| A face   | The line defined above.                                            | Integral            |

The distance between two ends is the ordinary one: between two points, the length between
them; between a point and a line, the perpendicular; between two lines, the separation, which
exists only where they are parallel within a named tolerance and is refused naming both ends
where they are not. A claim on a measurement is the measured value of that distance, and
`claim-agrees-with-geometry` extends to it with the distance as the shape it compares. That
is what makes a tape's clear width a claim that can disagree with the runs and thicknesses the
model states — the 13% room becomes a failure with a position, not a caption.

**The integral-versus-derived distinction is carried, by the end and never by a flag.** A face
end states that the measurement ran to a physical surface of a named element, and the engine
computes where that surface is. An edge end states that it ran to the authored line. There is
no boolean beside an end saying "this was the face": a flag on an edge end would state a
surface the engine would then measure to the edge's line anyway, so it could be written wrong
and never be caught — the silent convention this record removes, moved into the file.

**A derived anchor is a reference, written where it is used, and never a node with a
position.** It carries no coordinate, so there is nothing in the source tree for a moved
corner to leave stale ([0009](./0009-derived-values-are-never-written-back.md)). A query that
reports where a face lies reports it as a derived value, with the digest it was computed
against, like any other.

**Excluded, and why.** Each of the following was a candidate, and each is refused for a
reason this record states rather than left out:

- **A point at a parameter along an edge.** Not derived. Its parameter is a measurement, and
  nothing else in the model fixes it, so a claim about it restates the measurement that
  placed it. A measured location already has a home that can be checked — a vertex with a
  position claim, splitting the run it lies on — and 66 of 66 jambs in the evidence show that
  home is usable. None of the 124 ends needed anything else. Excluded, not deferred.
- **The station where a string crosses a line.** Not a point of anything: the model fixes the
  line, and where the string meets it is wherever it was drawn. A measurement with such an end
  measures to the line, which is an edge or a face end. Where it appears on a sheet is the
  consumer's. Excluded, not deferred.
- **Whether a measurement appears on a drawing** — its witness lines, text and placement.
  Editorial, and the consumer's, as composing a sheet already is
  ([0025](./0025-the-map-export-draws-every-region-and-its-properties-are-the-filter.md)).
  Excluded, not deferred.
- **The intersection of two lines.** Falsifiable, and not needed: every string in the
  evidence that ended at one measured between lines, and a point there carries a station the
  measurement does not have. Not admitted.
- **The foot of a perpendicular.** A measurement from a vertex to an edge or face end already
  measures to it. Not admitted.
- **The centre of an opening.** Falsifiable, and computable from two authored jambs, but of
  the six windows in the evidence whose centres a string runs to, the tape measured the centre
  of only two; the others are figures taken from the jambs. Not admitted.

The last three pass the first test and fail on evidence, which is the difference between
them and the first three. They are listed so that admitting one is a change to this record,
argued on new readings, and not a member added in passing.

**This record decides; it does not specify.** The spelling — the tags, their children, their
canonical order — is [SPEC.md](../../SPEC.md)'s, fixed by the story that implements it, and its
version effect is whatever [§10](../../SPEC.md#10-versioning-of-this-specification) says of
what that story adds. Until then [§11](../../SPEC.md#11-not-in-this-version) names both the
decision and the exclusions.

## Consequences

The tape's reading is the claim. A clear width is written as measured, with its own source,
method, accuracy and date, between the two faces it was read between, and the conversion to a
centreline figure that used to happen in somebody's head happens in the engine, where it can
be wrong visibly.

A model dimensioned to two conventions stops being silent. A face-to-face reading and a
centreline span are different statements in the file, both are checked against the same runs
and thickness claims, and a level whose runs were placed from face readings as though they
were centrelines disagrees with its own clears by the thickness it forgot.

Fifty-three of the 62 strings in the evidence become model data: every one that runs between
corners, edges' lines and faces. The nine that touch an opening's centre stay the consumer's
to derive, from `plan` and the jambs.

A corner-to-corner measurement no longer needs an edge between its corners. A string across a
room, or along a run split at its jambs, is stated between the two corners it was read
between, without a phantom edge that would join them and duplicate the wall it runs beside.

The reading of a run as its element's centreline, until now a property of one command,
becomes a property of the format. Every face is computed from it.

`plan`'s anchor kinds grow, and its output contract with them
([0014](./0014-the-machine-output-contract-is-part-of-the-interface.md)). The geometric
families of [0001](./0001-two-node-families.md) grow by one form, which has a frame and claims
and no kind or type, and so stays on the geometric side of that record's line.

## Cost

A fourth geometric form, one reference shape with three variants, a parallelism rule that
needs a tolerance, and an extension of a check — a real amount of format and engine for one
member. The cheaper thing covered 46 of 124 ends and every opening; this is what the other
78 cost.

The centreline reading is now load-bearing in model data. An author who drew a wall's run
along one of its faces gets faces in the wrong place, by half a thickness, and every face
measurement against that wall disagrees. That disagreement is true — the run is not where the
format says a run is — but it arrives as a wall of failures on a model that loaded cleanly
yesterday, and the remedy is to redraw the run, not to annotate it.

A face of a curved edge is refused. An arc's offset is a concentric arc and the distance to
it is well defined, but nothing in the evidence measured to one, and the parallelism rule
would need restating for curves.

Nine strings stay underivable, and the consumer keeps a computation over `plan` for them. The
two genuine readings to a window's centre in the evidence have no home but a jamb position,
which is exactly the derived-figure-stated-as-measured this record refuses for faces. It
accepts that for two readings rather than add a member for them.

The thickness predicate is named by the reader, not the file, so a face means nothing until
something names it — the same bargain every check with a position parameter already makes,
and the same way two readers naming different predicates get two answers.

## What would reverse it

Evidence from a second consumer, or a second house, that measurements run to points:
diagonal checks between the outer corners of two faces, strings that are not orthogonal, or
readings to opening centres that the tape took rather than the jambs restated. Any of those
would admit the intersection of two lines, the foot of a perpendicular or the centre of an
opening, each by amending this record — they pass the falsifiability test already, and the
exclusion is on evidence alone.

A model whose runs are authored along faces rather than axes, deliberately and in number,
would show the centreline reading to be a convention rather than a fact about what a run is.
The response would be a statement, per element, of where its run lies relative to it — not a
flag on the measurement, which is the one form this record rules out.

Removing the face once models use it is close to permanent. Every clear written against one
would have no home but the positions and captions it replaced, and turning a face-to-face
reading back into a centreline figure is precisely the unrecorded, uncheckable conversion the
face exists to take out of people's heads. The readings would survive; what they were
measured between would not.
