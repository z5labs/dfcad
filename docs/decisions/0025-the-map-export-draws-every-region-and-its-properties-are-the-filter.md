# 0025. The map export draws every region, and its feature properties are the filter

**Status:** Accepted

## Context

`dfcad export-map` writes every node the model gives a shape to. On the model this repository
tests against that is a handful of features and reads as a plan. On a real one it is not: a
consuming project composing a site plan in QGIS opened a four-feature document and found the
countertop and the closet drawn on top of the parcel, because a countertop is a node with an
outline and the command draws nodes with outlines.

The obvious response is a selector, and the consumer reached for one first. `--kind`, `--type`,
`--select`, `--filter`, `--only` and `--subject` are each `flag provided but not defined`. That
is a defensible answer — the alternative is that the reader narrows the layer — but nothing
said so. The help text described what the command draws and never what it does not, and the
properties a reader would narrow it *with* were misdocumented: the text named a `container`
property and the document carries `within`, so a definition query written from the
documentation selected nothing and raised nothing. A silent empty layer is the worst shape
this failure could have taken, and it is the shape it took.

So the decision is not only whether to add a selector. It is where the narrowing lives, and
what a consumer is entitled to rely on when they write it.

Three answers were live.

**A selector on the command.** `--kind Space`, `--type Parcel`, `--within site:L-01`. Familiar,
and it is what the listings already do — `dfcad list-instances` takes exactly those. Against it
is where the artefact lands. An export is a build output keyed by the digest of the tree it was
derived from ([0021](./0021-an-export-is-a-build-output-keyed-by-its-source-digest.md)), written
to `.dfcad/export/<digest>/model.gml`, and a second run over an unchanged tree finds the file
and reports it `unchanged` without reading it. A selector makes the digest an incomplete key:
two runs over one tree, selecting differently, disagree about what belongs at that path, and the
one which ran second is told its document is unchanged when it is a document of something else.
The key would have to grow to cover the selection, which means the artefact directory stops
being "this revision's exports" and becomes "this revision's exports under these arguments" —
a cache with a compound key, in a directory whose whole value is that it can be deleted and
rebuilt.

**A query language.** More expressive and worse: it is a second query engine beside `traverse`
and the listings, over a vocabulary the engine deliberately does not
know ([0010](./0010-the-engine-carries-no-domain-vocabulary.md)), in the one command whose
output nobody reads directly.

**The reader's own filter.** GML is read through GDAL, and everything downstream of GDAL —
QGIS, ArcGIS, PostGIS, `ogr2ogr` — has an expression language over feature attributes. A layer
filter, a definition query and a style rule are the same facility, and a consumer composing a
sheet is already writing them for every other layer on it.

## Decision

`export-map` takes no selector. It draws every node the model gives a shape to and has not
retired, whatever its kind, its type or what contains it, and there is no flag which narrows
that.

The feature properties are the interface for narrowing, and they are a contract rather than an
incidental convenience. A feature carries `id`, `label`, `kind`, `type`, `within` and `frame`,
in that order; `id` is on every feature and the rest are written where the node has them and
absent where it does not. Those names and their meanings change only with the version in the
vocabulary namespace, which is what the trailing number of
`https://github.com/z5labs/dfcad/gml/1` is for.

`within` is the immediate container and never an ancestor. A room reports the storey it sits
on, not the site that storey stands on, because `within` is the model's own containment edge
written out and that is what that edge means. A consumer asking what a whole site holds either
follows `within` from feature to feature within the document, or asks the model with
`dfcad traverse contains <id>` and joins the answer back on the `id` property.

The help text is required by a test to name exactly the properties a document carries. That is
the part of this decision which is enforced rather than intended: the properties are only a
contract if the documentation of them is right, and the documentation was wrong for as long as
nothing checked it.

## Consequences

One tree exports to one document, and the digest remains a complete key for it. `unchanged`
keeps meaning what it says, `.dfcad` stays a directory which can be deleted whole, and the
artefact a check reads is the artefact the tree produces rather than one of a family of them.

A consumer's filter is written once, against property names, and survives changes to the model
which a selector expressed in flags would not: a room becoming a different type is a row the
filter does or does not match, not an invocation somebody has to go and edit.

The properties are load-bearing, so they are documented in three places which have to
agree — the command's help, the machine output contract, and this record — and a test holds the
first of those to what the writer emits.

Composing a sheet is the reader's work, which is where the rest of that work already is. This
command's product is a layer, not a drawing.

## Cost

**A document of the whole model, every time.** A model with thousands of nodes writes thousands
of features, and a consumer who wants the parcel gets the countertops with it. The file is
larger and slower to write than a narrowed one would be, and a reader which opens it without a
filter shows a plan nobody would draw.

**The narrowing has to be written somewhere else.** A consumer whose tooling has no expression
language cannot use this export as a sheet; they get a layer and a job to do. That every reader
in this ecosystem does have one is an observation about GDAL, not a guarantee.

**A filter is only as good as the vocabulary it names.** Narrowing by `type` means knowing the
project's types, which are registry data and can change. The consumer's filter is coupled to
the model's vocabulary — which is true of any answer to this question, but with a selector the
coupling would at least be visible in a command line somebody runs.

## What would reverse it

Evidence that the whole-model document is prohibitive rather than merely untidy — a model whose
export a consumer's reader will not open, or a write slow enough to matter in the composition
loop. That is measurable, and nobody has measured it: the case which prompted this record was
four features.

Unwinding it means deciding what the artefact is keyed by before deciding what the flag is
called. Either the selection joins the digest in the key, so `.dfcad/export/<digest>/…` gains a
level and a result names what was selected, or a selecting run is confined to `--out` and never
writes into the build directory at all. The document a run selecting everything writes would be
byte-identical to today's, so no existing artefact would be invalidated and nothing would need
re-deriving — which is what makes this reversible on evidence rather than expensive to revisit.
