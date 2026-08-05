# 0019. The registry is the distribution channel, digests are authoritative, and there is no `latest`

**Status:** Accepted

## Context

`dfcad` is a command line tool whose main consumer is somebody else's pipeline. That shapes
the question "how does a user get it" differently from a tool people install on a laptop: the
consumer is a configuration file, it has no opinion about ergonomics, and the property it
needs is that the thing it ran yesterday is the thing it runs today.

Three channels were live. A binary attached to a GitHub release is the familiar one, and it
is not available: the standard pipeline produces images and stops there, emitting binaries
independently was asked for as z5labs/devex#329 and closed `NOT_PLANNED`, and building them
here instead would be the second build definition the continuous integration section of
`CLAUDE.md` exists to prevent — see [0018](./0018-three-versions-stamped-by-the-pipeline.md),
which ruled out the same move for the version stamp. `go install` is Go's channel rather than
this repository's, and the binary it produces is unstamped: it reports `"version": "dev"` and
`"stamped": false`, so it cannot say which build it is, which is the one thing a tool running
in automation has to be able to say. A container image is what the pipeline already makes,
what a pipeline already knows how to pull, and what carries the SBOMs and the signed
provenance that z5labs/devex#330 attaches.

Given a registry, two questions follow that are easy to answer by habit and expensive to
answer wrongly.

The first is what a consumer pins. A tag is a mutable pointer; a digest names bytes. Nothing
in this pipeline moves a tag — a git tag maps to an image tag verbatim and a rebuild of one
commit produces identical bytes — but that is a property of the pipeline rather than a
promise the registry makes, and a consumer cannot verify it from outside.

The second is whether to publish `latest`. Every registry has one and most projects push one.
Doing it here means either teaching the module to push an alias, which is an upstream change,
or pushing a second tag from this repository beside the standard pipeline, which is the drift
`CLAUDE.md` forbids. Neither cost is enormous; what settled it is what `latest` is *for*.

## Decision

**The container image is the distribution channel.** `ghcr.io/z5labs/dfcad`, published by the
standard pipeline's one `dagger call`, with the registry, the credentials and the `publishOn`
regex passed as inputs to it. There are no release binaries, no checksum file and no release
assets, and no path in this repository produces any of them.

**Digests are authoritative and tags are advisory.** A tag is how a person finds a digest
once; a pinned reference is `ghcr.io/z5labs/dfcad@sha256:…`. The attestations anchor to the
digest for the same reason, so a pin by digest is also what makes them checkable.

**There is no `latest`, and no upstream issue asks for one.** The requirement is dropped
rather than deferred. A consumer that pulls `latest` gets behaviour that changes without a
commit in its own repository and cannot afterwards say what it ran — which is the failure
`.build.version` and the digest both exist to remove. Offering the name would be offering the
failure, and an alias nobody should use is not worth an upstream change to publish.

**Release tags are kept forever; branch builds expire.** A `v1.2.3` image is something
somebody pinned. A `<short-sha>-<commit-time>` image is a build, and after ninety days it is
a build nobody is bisecting against.

## Consequences

Installation is one instruction — pull the image — and the README says so once.
`docs/publishing.md` is where the tags, the pin, the retention and the attestations are
written down, and it describes the module's behaviour rather than a convention layered on it.

A consumer that wants a binary on a host has to extract it from the image or build from
source, and building from source gives an unstamped binary. That is a real gap in what this
repository ships, and it is upstream's to close if it ever is.

Retention has to distinguish a branch build from a release, and it cannot do that by asking
whether a version is untagged. A release cut from the tip of `main` is one digest wearing both
`v1.2.3` and a branch-build tag, and on GHCR the per-platform manifests under a
multi-architecture index are themselves untagged versions of the package. The sweep therefore
matches on every tag a version carries and never deletes an untagged one.

## Cost

Anyone without a container runtime cannot run the tool. That is most of the point — the
consumer is automation — but it does exclude the person who wanted one binary on a laptop, and
`go install` is a worse answer for them than a release asset would have been.

Pinning by digest is more typing and less readable than pinning by tag, and it does not tell
you at a glance which version it is. The mitigation is that `dfcad version` reports the tag
the binary was stamped with, so a pinned digest can still say what it is — but it has to be
run to say it.

No `latest` means every consumer writes a version somewhere, and every upgrade is a commit in
somebody's repository. That is the intended cost and it is still a cost: nobody gets a fix by
re-pulling.

Ninety days of branch builds is a bounded but real hole. A bisect across a wider window has to
rebuild the commits rather than pull them.

## What would reverse it

If the module grew a way to emit binaries and checksums — z5labs/devex#329 reopened, or
whatever replaces it — release assets could be attached beside the image without a second
build definition here, and the "one install path" half of this record would be worth
revisiting. The digest and `latest` halves would not: they are about what a consumer pins,
not about what is shipped.

If a consumer appeared who genuinely cannot pin — a demo, a documentation snippet somebody has
to be able to copy without editing — then a moving tag has a use, and the honest form of it is
an upstream alias push rather than a second tag from here. It would still be the wrong thing
for a pipeline to use, so it would need a name that says so rather than `latest`.

If registries stopped guaranteeing that a digest names immutable bytes, the pin would be
worthless and the whole record would need rebuilding on whatever replaced it. Nothing would
migrate; every pinned reference in every consumer would simply have to change.
