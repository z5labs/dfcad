# 0026. Reading a revision out of git is a host command, and the image stays scratch

**Status:** Accepted

## Context

The published image is the z5labs module's `scratch` image: one statically linked binary,
no shell, no libc and no `git` ([`publishing.md`](../publishing.md)). Every command but one
reads and writes files and nothing else, so every command but one runs from it — `check`,
`measure`, `plan`, `tessellate`, `export` and `export-map` were measured running clean against
a read-only mount, byte-identically across two runs, on 2026-08-11 against `d9e9cab`.

The one is `dfcad review` against a revision. It compares the model with the merge base of
`HEAD` and `--against`, and it reads that merge base — and the log that attributes each
finding to the commit which introduced it — by running `git rev-parse`, `git merge-base`,
`git archive` and `git log`. From the image it stops before reading anything:

```
dfcad review: git rev-parse --show-toplevel in /model: git is not on the path
```

with exit `2` and nothing on stdout. That is a correct failure, and it was discovered by
hitting it: nothing a consumer reads before building a gate said that the image was not a
complete answer for this command ([#228](https://github.com/z5labs/dfcad/issues/228)).

Three shapes were live.

**Put `git` in the image.** It is the cheapest to describe and the most expensive to hold. The
image is not this repository's to change: it is the module's `App`, and a base image carrying
a package manager's worth of `git` and its libraries is an upstream change to the Z5Labs
standard — [`CLAUDE.md`](../../CLAUDE.md) says a gap in the module is fixed there and not
worked around here. It would also stop the image being `scratch`, and the SBOM, the signature
and the provenance attached to every digest are worth what they are because there is almost
nothing in the image for them to describe. And it would not work unaided: the image runs as
`65532`, a mounted checkout is owned by whoever cloned it, and `git` refuses a repository
owned by somebody else unless `safe.directory` says otherwise — so a consumer would trade
"git is not on the path" for "dubious ownership" and a configuration flag to learn.

**Read the revision without shelling out.** The engine would read loose and packed objects,
resolve refs and packed refs, walk to a merge base, diff trees commit by commit, and parse
whatever `--against` is given — which today is anything `git rev-parse` accepts. That is a
second implementation of a format git owns, taken on to spare a pipeline one host step, and
it is exactly where the two would disagree: a revision spelled in syntax the reimplementation
does not parse, a partial clone whose objects are on a promisor remote, a repository in
SHA-256 object format, a worktree whose objects live in another directory. Each of those is a
review that answers wrongly rather than one that refuses, and a review that answers wrongly
is worse than one that cannot run.

**Say that `review --against` is a host command.** The command already has the other half of
the answer. `--base-root <dir>` compares against a model in a directory rather than against a
revision and reads no git at all, so it runs from the image. What it gives up is attribution:
a finding under it names no commit, because there is no history to attribute it through. A
consumer whose runner has `git` — which is every CI runner that checked the model out — can
materialise the merge base on the host and hand both trees to the pinned, attested binary.

## Decision

**`dfcad review --against` is a host command. The image stays `scratch`, and the engine keeps
reading revisions through the `git` on the path.** It is the only command that needs anything
from the host beyond the model; every other command, and `review --base-root`, runs from the
published image with nothing but a mount.

What that obliges:

- **The boundary is written where a reader meets it.** `dfcad review --help` says the command
  needs `git` on the path and that the image does not carry it; the model gate's README lists
  `git` among what a checkout needs for the review stage; and
  [`publishing.md`](../publishing.md) says which commands the image is a complete answer for,
  and how to run `review` from it with `--base-root`.
- **The failure names its cause and its way out.** When `git` is not on the path, `review`
  still exits `2` with nothing on stdout and still says `git is not on the path`, and it now
  also says that `--base-root` is the comparison that needs no `git`. A consumer who runs it
  from the image anyway gets two sentences, not a silent empty result.
- **`ErrGitMissing` stays distinct from a repository that refused.** A machine with no `git`
  is reported as that, not as a directory which is not a working tree, which is what lets the
  command say the right thing about it.

## Consequences

A consumer's gate that runs `review` against a revision either runs a `dfcad` binary on a host
that has `git`, or runs the image with `--base-root` and materialises the merge base itself:

```sh
base=$(git merge-base HEAD origin/main)
git worktree add --detach "$RUNNER_TEMP/base" "$base"
docker run --rm -v "$PWD:/model:ro" -v "$RUNNER_TEMP/base:/base:ro" \
  ghcr.io/z5labs/dfcad@sha256:<digest> review --root /model --base-root /base
```

Every other command a gate runs — `fmt --check`, `check`, and the queries and exports — needs
nothing from the host but the model.

## Cost

The one command whose whole job is to report what changed in a revision is the one command
the pinned, stamped, attested artefact does not answer on its own. A consumer who wants
`review --against` exactly as this repository runs it has to run a binary outside the image —
today by extracting it — and so gives up, for that command, the property
[0019](./0019-the-registry-is-the-distribution-channel.md) chose the registry for. The
`--base-root` route keeps the artefact but costs attribution: its findings name no commit, and
the step that finds the merge base is the consumer's to write and to get right, including the
`fetch-depth: 0` that the command would otherwise have checked for them.

## What would reverse it

The module offering a published base image with `git` in it, attested the way the current one
is, would make the first shape an input rather than a fork of the standard; so would a Go
reader of git's object store maintained widely enough that its disagreements with `git` are
found by somebody else first, which would make the second shape a dependency rather than a
reimplementation. Consumers routinely extracting the binary to run `review --against` would be
the evidence that `--base-root` is not enough. Unwinding either way changes no data and no
contract: the result object `review` writes is the same whichever way the revision was read.
