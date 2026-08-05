# The published image

`dfcad` is distributed as a container image and as nothing else. This file says where the
image is, what it is called, what to pin, what rides along with it, and how long a build
stays pullable.

```
ghcr.io/z5labs/dfcad
```

## Why a registry, and only a registry

The tool is mostly going to run inside somebody else's automation, and a registry is what
that automation already knows how to consume: it pulls by digest, it caches, and it does not
need a Go toolchain, an archive, or a trust decision about where a tarball came from.

There is no binary download — no release assets, no checksum file, no install script.
z5labs ships containers, which is [z5labs/devex#329](https://github.com/z5labs/devex/issues/329)
closed `NOT_PLANNED` and [#64](https://github.com/z5labs/dfcad/issues/64); the standard
pipeline produces images and stops there, and a second, repository-local path that produced
a binary would be exactly the drift the continuous integration section of
[`CLAUDE.md`](../CLAUDE.md) rules out.

`go install github.com/z5labs/dfcad/cmd/dfcad@latest` still works, because it is Go's, not
this repository's. It is not an install path this repository supports: the binary it
produces is not stamped, reports `"version": "dev"` and `"stamped": false`, and therefore
cannot say which build it is — see [`versioning.md`](./versioning.md). Use it to hack on the
tool, not to run it.

## What publishes, and what does not

The registry, the credentials and the `publishOn` regex are inputs to the one `dagger call`
in [`ci.yaml`](../.github/workflows/ci.yaml). Nothing in the workflow decides whether a run
publishes; the module matches `publishOn` against the refs pointing at `HEAD`:

```
^refs/(heads/main|tags/v.+)$
```

| Event | Refs at `HEAD` | Publishes |
|-------|----------------|-----------|
| Pull request | `refs/remotes/pull/<n>/merge` — the merge commit is on no branch and carries no tag | no |
| Push to `main` | `refs/heads/main` | yes, as a branch build |
| Push of `v1.2.3` | `refs/tags/v1.2.3`, and `refs/heads/main` when the tag is on the tip | yes, as a release |

A pull request builds the image on every platform and throws it away. That it does not
publish follows from the regex and from the ref, not from an `if:` on the job — which is
what makes "does a pull request publish?" a question with one answer written down in one
place, rather than a condition that can be true on a workflow somebody edited.

The checkout is `fetch-depth: 0`, because all of the above is read out of git at `HEAD`: the
publish decision, the image tag and the version stamped into the binary. A shallow checkout
does not fail the pipeline at the publish; it fails it at the start, which is the right place.

## The tags

The tag is the module's, applied verbatim to what git says. This is a description of
`GoApp`'s behaviour rather than a convention this repository chose:

| Ref at `HEAD` | Image tag |
|---------------|-----------|
| `refs/tags/v1.2.3` | `v1.2.3` |
| `refs/heads/main` | `<short-sha>-<commit-time>`, e.g. `f205b64-2026-08-05T12-00-00Z` |

Both strings are the same ones the binary reports as `.build.version`, so an image tag and
the `dfcad version` output of the binary inside it agree by construction rather than by
anybody keeping them in step. The tag charset costs the convention some freedom — no semver
build metadata, no path-shaped tags — and [`versioning.md`](./versioning.md) has the table.

Where a release tag sits on the tip of `main`, both refs match and the same bytes are pushed
twice under two tags. One digest, two names; that is the module publishing every matching
ref rather than choosing between them.

### There is no `latest`

`latest` is not published, and no upstream issue asks for it. The module maps a ref to a tag
and pushes what it built; an alias means pushing a name that points at something built from
a different commit, which is a second thing to keep true and a name that is wrong for the
window in which it is being updated.

More to the point, `latest` answers a question nobody automating this should be asking. A
pipeline that pulls `latest` is a pipeline whose behaviour changes without a commit in it,
and it cannot say afterwards what it ran. The recorded reasoning is
[ADR 0019](./decisions/0019-the-registry-is-the-distribution-channel.md).

## Digests are authoritative; tags are advisory

A tag is a mutable pointer. Nothing in this repository moves one today — every tag names a
commit, and a re-run of a tagged build pushes identical bytes — but that is a property of
the pipeline, not a promise the registry makes, and a consumer cannot verify it from
outside. A digest names the bytes.

**Pin by digest.** The tag is how a human finds the digest once:

```sh
docker buildx imagetools inspect ghcr.io/z5labs/dfcad:v1.2.3 --format '{{.Manifest.Digest}}'
```

```sh
docker run --rm ghcr.io/z5labs/dfcad@sha256:<digest> version
```

Everything attached to a published image — the SBOMs, the provenance — anchors to the digest
for the same reason, so a pin by digest is also what makes those attachments checkable.

## What the image is

The module's scratch image: one statically linked binary and nothing else. No shell, no
package manager, no libc, no CA bundle, no writable temporary directory. The entrypoint is
the binary, so arguments go straight to it.

That the engine reads files and writes files, has no daemon and speaks no network protocol
is why a scratch image is enough — see the non-goals in the [README](../README.md).

Three consequences worth knowing before debugging one:

- `docker exec ... sh` does not work, and neither does any `RUN` in a derived image.
- The model has to be mounted; there is nothing in the image to copy it in with.
- Writes land as the container's user. Authoring commands need `--user` to avoid leaving
  root-owned files in your working tree.

It is multi-architecture, `linux/amd64` and `linux/arm64`, which is the module's default
platform list. This repository does not override it: the platforms the image covers are the
platforms the pipeline builds, and stating them twice is how they come to disagree.

## Running it

Read-only, which is most queries:

```sh
docker run --rm -v "$PWD:/model:ro" ghcr.io/z5labs/dfcad:v1.2.3 \
  resolve --root /model site:S-103 area
```

Stdout is one JSON object and nothing else — diagnostics and everything else human-facing
are on stderr — so it pipes straight into `jq`:

```console
$ docker run --rm -v "$PWD:/model:ro" ghcr.io/z5labs/dfcad:v1.2.3 \
    resolve --root /model site:S-103 area | jq -c '{value, accuracy}'
{"value":{"shape":"scalar","unit":"m2","scalar":32},"accuracy":[{"kind":"independent","magnitude":0.08,"unit":"m2"}]}
```

Authoring writes into the model, so the mount is writable and the container runs as you:

```sh
docker run --rm -v "$PWD:/model" --user "$(id -u):$(id -g)" \
  ghcr.io/z5labs/dfcad:v1.2.3 \
  set-label --root /model site:S-103 "Office 204"
```

In a pipeline, pin the digest and let the exit code decide:

```sh
docker run --rm -v "$PWD:/model:ro" \
  ghcr.io/z5labs/dfcad@sha256:<digest> \
  check --root /model
```

## What comes with the image, from the module

Every published digest carries these, and none of them is produced by anything in this
repository. They are [z5labs/devex#330](https://github.com/z5labs/devex/issues/330), and
reproducing any of them here would be a second definition of something the standard pipeline
already does:

- **Source annotations** on every platform variant — `org.opencontainers.image.revision`
  (the full `HEAD` SHA), `.source` (the origin URL), `.created` (the commit's committer time,
  not the build's wall clock), and `.version` on a tag build. The commit an image was built
  from is readable off the manifest without pulling it.
- **An SPDX and a CycloneDX SBOM per platform**, generated from the binaries the pipeline
  compiled.
- **A signed SLSA provenance statement**, whose build identity comes from an exchanged
  workload identity token.

The provenance is why the `build` job carries `id-token: write`. The module resolves the
signing identity before it pushes a byte and refuses the publish if it cannot, so there is no
configuration of this repository that publishes an unattested image.

## Retention

Release tags are kept forever. A `v1.2.3` image is what somebody pinned, and a registry that
forgets it turns a working pin into a broken one.

Branch builds expire. [`ghcr-retention.yaml`](../.github/workflows/ghcr-retention.yaml) runs
weekly and deletes package versions whose every tag is a `<short-sha>-<commit-time>` build,
older than **90 days**, keeping the newest **10** whatever their age. Run it manually with
`dry_run` to see what it would take.

Two things it deliberately never deletes, both of which the obvious version of this job gets
wrong:

- **A version carrying any other tag.** A release cut from the tip of `main` is one digest
  wearing both `v1.2.3` and a branch-build tag. Matching on every tag rather than on one is
  what keeps that release.
- **Untagged versions.** On GHCR the per-platform manifests beneath a multi-architecture
  index are untagged versions of the same package, as are the referrer manifests holding the
  attestations. "Delete all untagged versions" is the recipe everybody reaches for, and on a
  multi-architecture image it deletes the `arm64` half of images that are still current.

## Cutting a release

Nothing in the working tree is edited to make one. Tag the commit and push the tag:

```sh
git tag v1.2.3
git push origin v1.2.3
```

The pipeline runs the same checks it runs on a pull request, builds the image for both
platforms, publishes it as `ghcr.io/z5labs/dfcad:v1.2.3`, and attaches the SBOMs and the
provenance. `docs/versioning.md` says which component of the version a change moves.
