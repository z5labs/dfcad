# 0023. The map export names its coordinate system in the file, which is why it is GML and not GeoJSON

**Status:** Accepted

## Context

[0020](./0020-export-is-a-boundary-and-the-closed-set-is-what-crosses-it.md) decides that an
exporter may live in this repository and fixes what crosses the boundary.
[0021](./0021-an-export-is-a-build-output-keyed-by-its-source-digest.md) decides where the
artefact lands. Neither picks a format, and the second exporter — the outdoor half of a model,
written for a GIS rather than for a BIM tool — has to.

The model's coordinates are already planar. A frame declares one linear unit
([0005](./0005-one-linear-unit-per-frame.md)) and the root frame *is* the projected coordinate
reference system the chain is rooted at, whose identifier
[0022](./0022-a-command-whose-product-is-a-file-answers-on-stdout.md)'s sibling story records
verbatim on that frame. So this export is a serialisation and not a transformation: the
numbers in the file are the numbers in the model, and the only question is which file format
carries them along with the name of the system they are in.

**The obvious answer does not work.** GeoJSON is what anything reaching for a vector
interchange format reaches for first, and RFC 7946 fixes its coordinate reference system at
WGS 84 longitude and latitude and *removed* the `crs` member that GeoJSON 2008 had, precisely
so that no file could name another. Writing a state-plane survey as GeoJSON therefore means
one of two things, and both are unacceptable here:

- **Violate the format** — write projected eastings and northings into a structure whose
  specification says they are longitudes and latitudes, and hope that whoever opens it knows.
  A file which lies about its coordinates is worse than no file, because it renders: the
  features appear somewhere off the coast of Africa, at a scale which looks plausible, and
  nothing anywhere says why.
- **Reproject** — convert the survey to WGS 84 on the way out. That is geodesy, which means a
  geodetic library, which means cgo and a licensed parameter dataset, and it means this
  repository taking responsibility for the accuracy of a datum transformation nobody measured.
  Every cross-frame answer in this engine is a similarity transform in the plane the survey was
  already projected into, and that is the whole reason it can be trusted. The decision not to
  do geodesy is what a `crs` predicate recorded and never interpreted already states.

So the format has to be one which carries an authority and a code *in the file*, and which can
be written in pure Go with no cgo — because the pipeline ships a scratch image
([0018](./0018-three-versions-stamped-by-the-pipeline.md)) and a dependency which breaks the
static build is not acceptable for an exporter.

Three candidates were live.

**FlatGeobuf** carries a full CRS record — organisation, code, name and well known text — in
its header, and is compact. It is also a FlatBuffers binary: correctness depends on reproducing
a vtable layout exactly, a golden fixture for it is unreviewable by eye, and the check that it
is readable would be this repository's own decoder agreeing with this repository's own encoder
about a schema neither was checked against. A wrong field slot produces a file that no reader
anywhere can open, and nothing in CI would say so.

**A delimited text file plus a geometry column** — CSV with WKT — is the cheapest thing that
works, and the reason it was not taken is that the coordinate reference system is not in it.
It travels in a sidecar, or in the caller's import command, which is exactly the arrangement
this export exists to end: a file whose placement on the earth depends on somebody remembering
something.

**GML 3.2 Simple Features** names the system as an `srsName` attribute on every geometry, is
text, has interior rings for holes, is read by GDAL and therefore by QGIS, ArcGIS and PostGIS,
and is writable with the standard library alone. Its cost is verbosity — the document is
several times the size of the equivalent binary — and a reputation for being heavyweight,
which belongs to the full schema-validated profile rather than to the subset written here.

## Decision

**The map export writes GML 3.2, and the identifier of the coordinate reference system is
written into it as `srsName` on every geometry.** GeoJSON is rejected for the reason above and
is not offered behind a flag.

The writer is `gml/`, a package named for the format it writes, which imports nothing of this
module and nothing outside the standard library — the same boundary `ifc/` has and for the
same reason. Its exported surface is GML's vocabulary: positions, rings, polygons, features,
properties, a collection and a `Write`. The mapping from this engine's regions to that
format's features lives in `cmd/dfcad`.

Four things are fixed by this decision:

- **The identifier is opaque.** `gml` takes it as a string, writes it, and has no code path
  which parses, resolves or converts it. Neither has the command: the shape check is an
  authority and a code, and nothing more.
- **Nothing reprojects, anywhere.** The coordinates written are the model's own. A region
  outlined on a non-root frame is carried into the root frame by the chain of measured
  transforms the model states, which is a similarity transform in one plane and is the same
  arithmetic `dfcad site` already does across frames. Where the chain does not reach, the
  export is refused.
- **Easting then northing, and two ordinates.** That is the axis order the consuming ecosystem
  assumes for a system named as an authority and a code, and it is fixed in the package rather
  than offered as a setting. The elevation is dropped, because a map is a plan; a boundary
  which does not lie at one level in the root frame is refused rather than projected.
- **The document is byte-identical for an unchanged tree**, like every artefact
  ([0021](./0021-an-export-is-a-build-output-keyed-by-its-source-digest.md)), and the digest it
  was derived from is what the command reports.

**No schema is published and none is pointed at.** The application namespace the feature
properties are written in — `https://github.com/z5labs/dfcad/gml/1` — identifies a vocabulary
and deliberately resolves to nothing, and the document carries no `xsi:schemaLocation`. This is
recorded here because it looks like an oversight and is not: a namespace URI names a
vocabulary, it is not a URL to fetch, and the version at the end of it is what moves when a
property changes meaning. Publishing an `.xsd` beside the writer would be a second definition of
the document to keep in step with the first, checked by nothing, and a `schemaLocation` pointing
at it would make every reader's behaviour depend on a network fetch of a file this project would
then have to host forever. GDAL asks for neither and infers the schema from the instance, which
is what every reader in this ecosystem goes through. The cost is real and is stated below:
a reader which insists on resolving a schema refuses this document.

**Byte-identical is not digit-identical to the source text.** Determinism is a property of two
runs over one tree, not a promise that every coordinate in the file is the decimal somebody
typed. A corner authored on the root frame does reach the file as it was written; one carried
across a frame by a measured transform, or drawn along an arc, has floating-point arithmetic
done to it on the way, and `2000000.0` can be written `1999999.9999999995` — about 5e-10 survey
feet, the last bits of a double. Nothing there is a reprojection, and it is why a downstream
check on this file compares coordinates to a tolerance rather than as text.

## Consequences

There are two exporters and two packages beside the engine, `ifc` and `gml`, each of which
could move to a repository of its own without an edit to its source. A third format is a third
package and a third command rather than a flag on either of these, because what varies between
them is a whole vocabulary and not a switch.

A consuming repository gets a site plan by writing one text predicate on its root frame and
naming it. Nothing else about the model changes: the same rings that answer `dfcad plan` and
`dfcad site` are what the document holds, drawn to the same chord tolerance.

The engine still owns no geodesy, and the reason it does not is now written down twice — once
as a predicate it records and never reads, and once as a format chosen because it does not
force the question.

A reader of the document can go from a feature back to the model without holding the model:
the id of the node it was drawn from is a property, so a rendered plan is traceable rather
than merely pretty.

## Cost

**Size.** A GML document is several times the size of the equivalent binary, and every ordinate
is written as text. For the models this engine is for — hundreds of regions, not millions — the
cost is a file measured in hundreds of kilobytes, which is a price worth paying for a golden
fixture somebody can read in a pull request.

**GML's reputation.** Whoever opens the file next may expect a schema, a `.xsd` and a validation
step, and there is none: the document names an application namespace which resolves to nothing.
Readers which insist on resolving a schema will refuse it. GDAL does not, and everything in
this ecosystem goes through GDAL.

**No definition travels.** The register's own text about a system — the well known text a
project may hold beside the identifier — has nowhere to go in this format and is dropped. A
consumer wanting it takes it from the model or from the IFC export, both of which carry it.

**Two exporters to keep in step.** A change to how a region is drawn has to reach both of them,
and a change to how a coordinate reference system is read is shared code precisely so that the
two cannot come to disagree about where one model is.

## What would reverse it

A pure-Go FlatGeobuf writer with a test suite that checks its output against a reader nobody
here wrote — a fixture corpus from the format's own repository, say — would remove the reason
that candidate was refused, and the size argument would then be one-sided. Adopting it would
mean a second package beside `gml` and a `--format` flag on the command; the mapping from
regions to features would not move, which is the point of having it in the caller.

An OGC decision to give GeoJSON back a way to name a projected system would remove the reason
*this* record exists, though not the reason to prefer a format that already has one.

If this engine ever did take on geodesy — a decision that would need a record of its own, and
would have to answer for cgo and for a licensed dataset — then reprojecting on the way out
becomes possible and the format question reopens. Nothing about the artefact would be
recoverable from the old one: every coordinate in it would be different, so it would be a
re-derivation from the model rather than a migration.
