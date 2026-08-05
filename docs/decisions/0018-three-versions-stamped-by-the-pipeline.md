# 0018. Three versions, moving separately, stamped by the pipeline

**Status:** Accepted

## Context

Two questions were unsettled at the same time, and settling either one alone would have
prejudged the other.

The first is what "the version of dfcad" means. There are three candidates and they are not
the same thing. The machine output contract already carries a version of its own
([0014](./0014-the-machine-output-contract-is-part-of-the-interface.md)), an integer that
moves when a field is removed, renamed or retyped. The entity format carries a second, a
`MAJOR.MINOR` stated at the top of `SPEC.md` and governed by its section 10. Neither is the
version of the tool, which is the thing somebody installs, pins in a `go.mod`, or names in
a bug report. Consumers of the three are different people asking different questions, and
they change at different rates: the entity format has moved once, the output contract once,
and the tool has never been tagged at all.

The second is where the tool's version comes from. Every alternative that puts it in the
source tree — a `const Version` bumped by hand, a `VERSION` file, a generated file
committed alongside — has the same defect: it is a claim the repository makes about itself,
and a build from any commit other than the one where the bump landed reports the version of
a release it is not. That is the failure mode the whole story exists to remove. A bug report
saying `v1.2.0` when the binary was built from four commits after the tag is worse than one
saying nothing, because it sends whoever is reading it to the wrong source.

The alternative is to derive it from git, at build time, which means the build has to be
able to inject it. When this was first written down the standard pipeline could not: `GoApp`
in `z5labs/devex/daggerverse/z5labs` compiled with a closed link line, `-ldflags "-s -w"`
and no injection point. The two live options were to hand-roll a second build here beside
the standard one, or to fix the module. Per the continuous integration section of
`CLAUDE.md` the second is the rule, and it was done: z5labs/devex#328. The shape it landed
in is stronger than the one this repository asked for — the module does not take a caller's
stamps at all, it derives the version and the commit from `HEAD` itself and stamps every
binary it builds, under symbol names it fixes. A consumer cannot supply a value that varies
between two builds of one commit, because a consumer cannot supply a value.

## Decision

**There are three versions and they move separately.** The tool's version is a git tag. The
machine output contract's is `outputVersion`, an integer written on every object. The entity
format's is `dfcad.SpecVersion`, a `MAJOR.MINOR` string tracking `SPEC.md`. None is derived
from another, and `dfcad version` reports all three so that no consumer has to map between
them out of band. `docs/versioning.md` states which changes force which to move; the
short form is that a MAJOR move of either contract forces a MAJOR move of the tool, and
that the converse does not hold.

**The tool's version and commit are stamped by the standard pipeline and by nothing else.**
They are `main.version` and `main.commit`, the symbol names the module fixes, initialised to
the placeholders `dev` and `unknown`. This repository adds no build path that stamps them:
`dagger call ... ci` and `dagger call ... builder binary` both stamp, identically, for the
same commit, and a plain `go build` produces a binary that reports the placeholders and
`"stamped": false`.

**The tag convention is `vMAJOR.MINOR.PATCH`, with `-rc.N` pre-releases, and nothing else.**
No semver build metadata, no path-shaped tags, one tag per commit. This is not a free
choice: the module maps a git tag to the published image tag verbatim except for a rewrite
into docker's tag charset, so a scheme containing anything outside `[A-Za-z0-9_.-]` is
mangled into a different string in both the image tag and the binary. `v1.2.3+build.5`
becomes `v1.2.3-build.5`, which is also a legitimate pre-release tag; two distinct tags
colliding on one image is the concrete harm.

## Consequences

`dfcad version` is the first subcommand that reads no model, and it is listed first. The
object it writes nests the build under `build` rather than writing `version` at the top
level, because the envelope has already spent that name on the output contract; `.version`
and `.build.version` are different numbers in different forms, and the nesting is what says
so at the point of reading.

`dfcad.SpecVersion` is now part of the library's exported API. It is exported rather than
kept beside the loader because `SPEC.md` section 10 already tells a consumer who needs the
format version to read it from the engine, and this is that reading. It is a second copy of
a fact `SPEC.md` states, so a test reads the specification and requires the two to agree —
the copy is acceptable only while it cannot drift.

The name `commit` in `cmd/dfcad` belongs to the stamped variable. The helper that wrote a
change was called that and is now `commitChange`. This is a real constraint the module
imposes on every application it builds: `-X` against a symbol that is a function, or that
lives in another package, is a silent no-op, so the collision would have produced an
unstamped binary with nothing failing. CI runs the binary the pipeline built and requires
it to report itself stamped, which is what makes that failure loud.

A release is cut by pushing a tag, and nothing in this repository is edited to make one.

## Cost

The version is not readable from the source tree. `grep` for the current version finds
nothing, `go build` produces a binary that cannot say which commit it came from, and a
contributor who wants a stamped binary has to go through Dagger. That is a real
inconvenience traded for the guarantee that a version a binary reports is one it was
actually built as.

Three numbers are three things to explain, and every interface change now needs a judgement
about which of them moves. `docs/versioning.md` exists because that judgement is not
obvious, and it is a document that can go stale in a way the numbers themselves cannot.

The tag convention is narrower than semver allows, and the reason lives in another
repository. Somebody reading only this repository will see build metadata rejected for no
visible cause; the table in `docs/versioning.md` is the mitigation, and it is prose rather
than a check.

Publishing every commit and only stamping a release version onto tagged ones means the
majority of built images carry a version string that is not a version. That is deliberate,
but it does mean `.build.version` is not always parseable as semver and a consumer must not
assume it is.

## What would reverse it

If the module stopped stamping, or stopped fixing the symbol names, the first half of this
would have to be rebuilt on whatever replaced it — and the reversal is upstream, not here.
A repository-local link line added to work around a module gap is the specific thing this
record rules out, and reversing that is a change to `CLAUDE.md` rather than to this file.

If the output contract and the entity format turned out to move together in practice —
several releases where each MAJOR bump of one accompanied a bump of the other, with no
release moving one alone — then the separation is costing explanation and buying nothing,
and they should collapse into one number. Unwinding would mean a version change to the
output contract to drop a field, plus whatever mapping consumers had built on the pair.

If docker tag charset restrictions stopped applying — a registry that took a tag verbatim,
or a module that stamped the raw git tag into the binary while sanitising only the image tag
— the build metadata restriction could be lifted. That is a change to the table in
`docs/versioning.md` and to nothing else; no data would need migrating, because a tag scheme
constrains only tags not yet written.
