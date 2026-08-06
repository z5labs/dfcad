# Versioning and the tag convention

There are three versions in this repository, and they move independently. This file says
what each one covers, how a release is tagged, and which changes force which of the three
to move.

`dfcad version` reports all three:

```json
{
  "version": 2,
  "command": "version",
  "build": {
    "version": "v1.2.3",
    "commit": "abc1234",
    "stamped": true
  },
  "contracts": {
    "output": 2,
    "entity-format": "1.2"
  }
}
```

## The three versions

| Version | Where it lives | Form | What it covers |
|---------|----------------|------|----------------|
| The tool | git tags, stamped into the binary as `.build.version` | `vMAJOR.MINOR.PATCH` | The `dfcad` binary and the Go library beside it: the subcommands, their flags, the exported API of `package dfcad`. |
| The output contract | `outputVersion` in `cmd/dfcad/output.go`, written as `.version` on every object and as `.contracts.output` | a single integer | The JSON on stdout, the two streams and the exit codes. [`machine-output.md`](./machine-output.md) is the contract. |
| The entity format | `dfcad.SpecVersion`, written as `.contracts.entity-format` | `MAJOR.MINOR` | What a `.dfc` file may contain and what its one canonical printing is. [`SPEC.md`](../SPEC.md) is the specification, and its section 10 is the rule this number moves under. |

None of the three is derived from another, and none of them is a copy of another. A caller
which needs one reads that one.

The reason they are separate is that they answer different questions and change at
different rates. "Can my script still parse this?" is the output contract. "Will my model
still load?" is the entity format. "Which build produced this bug?" is the tool. Folding
them into one number would mean a release that touched none of the contracts still telling
every consumer that something they depend on may have moved, and the warning would stop
being read.

## The relationship between them

The connection runs one way, from the contracts to the tool:

- A change to **either** contract's MAJOR component forces a MAJOR bump of the **tool**.
  The output contract going `2` → `3`, or the entity format going `1.x` → `2.0`, is a
  breaking change to something a consumer programs against, and the tool version is the
  number a consumer pins.
- A MINOR entity format bump — a new optional child, a new value shape, a whole new form,
  per SPEC.md section 10 — is at most a MINOR bump of the tool. Files that loaded still
  load.
- A field **added** to the output contract does not move the contract's number at all, by
  its own versioning rule, and is at most a MINOR bump of the tool.

The converse does not hold. A MAJOR bump of the tool does **not** imply that either
contract moved: dropping a subcommand, removing a flag, or a breaking change to the
exported Go API is a major tool change on its own. So a consumer that cares only about the
JSON reads `.contracts.output` rather than inferring it from the tool version, and a
consumer that cares only about whether its files load reads `.contracts.entity-format`.

Both contract numbers are therefore reported by `dfcad version` rather than left to be
looked up against a release table. A version-to-contract mapping maintained outside the
binary is a second copy of a fact the binary already holds, and it goes stale the first
time somebody builds from a branch.

## The tag convention

A release is a git tag on the default branch of the form:

```
v1.2.3
v1.2.3-rc.1
```

That is [semantic versioning](https://semver.org) with a leading `v`, which is what the Go
module proxy requires of a tagged Go module anyway.

- **MAJOR** — a breaking change to anything above: a contract's MAJOR component moved, a
  subcommand or flag was removed or repurposed, or the exported Go API broke.
- **MINOR** — something was added and nothing broke: a new subcommand, a new flag, a new
  field on an output object, a MINOR entity format bump.
- **PATCH** — a fix with no interface change in either direction.

**Pre-release tags** are `-rc.N`, counting from `1`, on the commit the release would be cut
from: `v1.0.0-rc.1`, `v1.0.0-rc.2`, then `v1.0.0` on the same or a later commit. They exist
to get an image published and exercised before the version is claimed. Nothing else is a
pre-release identifier here — no `-alpha`, no `-beta.1+build`, no date suffixes — because
the fewer shapes there are, the fewer there are to check against the constraint below.

**One tag per commit.** Where more than one tag points at `HEAD`, the standard pipeline
takes the most recently created and stamps that; a second tag on the same commit therefore
decides the version by creation order, which is not a fact anybody reading the repository
can see.

### Why the convention stops where it does

The convention is constrained by what the standard pipeline does with a tag, not chosen
freely. `GoApp` in [`z5labs/devex/daggerverse/z5labs`](https://github.com/z5labs/devex/tree/main/daggerverse/z5labs)
maps a git tag pointing at `HEAD` to the published image tag **verbatim**, and stamps the
same string into the binary as `.build.version`. Verbatim is subject to one rewrite: a
docker tag may hold only `[A-Za-z0-9_.-]` and may not begin with `.` or `-`, so any other
character is replaced with `-`.

A tag scheme that survives that rewrite unchanged is one this repository can use. A scheme
that does not is not, because the version in the image tag and the version in the binary
would no longer be the version anybody wrote:

| Tag | Published as | Usable |
|-----|--------------|--------|
| `v1.2.3` | `v1.2.3` | yes |
| `v1.2.3-rc.1` | `v1.2.3-rc.1` | yes — `-` and `.` are both in the charset |
| `v1.2.3+build.5` | `v1.2.3-build.5` | **no** — semver build metadata is mangled, and the result collides with the legitimate pre-release tag `v1.2.3-build.5` |
| `release/v1.2.3` | `release-v1.2.3` | **no** — the prefix survives as noise and the tag no longer parses as semver |

So: **no build metadata and no path-shaped tags.** Semver permits the first and plenty of
projects use the second; neither is available here, and the reason is one level down in the
pipeline rather than a matter of taste.

Pushing one of these tags is what cuts a release, and what a release *is* — the image, where
it goes, what a consumer pins and how long it stays there — is
[`publishing.md`](./publishing.md).

## What an untagged build reports

Every commit is built, and only a tagged one is a release. A build from a commit with no
tag on it is stamped with `<short-sha>-<commit-time>`:

```
f205b64-2026-08-05T12-00-00Z
```

which is deliberately not semver — a branch build is not a release and must not read like
one — and is the same string the pipeline uses as that build's image tag. The commit is
still `.build.commit`, so such a binary identifies itself exactly as precisely as a tagged
one does.

Such a build is published and pullable, which is what makes bisecting against `main` cheap.
It is not kept forever: [`publishing.md`](./publishing.md) states the retention window.

## What an unstamped build reports

Stamping is done by the pipeline's link line, so a binary built by `go build` or `go run`
directly has none:

```json
{"build": {"version": "dev", "commit": "unknown", "stamped": false}}
```

`stamped: false` is the field to read first. A bug report quoting `version: "dev"` names no
commit, and the values beside it mean nothing — the placeholders are words rather than
blanks so that this case reads as itself rather than as a version somebody has to go and
look up.

There is no build path in this repository that stamps a binary by hand. `dagger call ...
ci` and `dagger call ... builder binary` stamp identically for the same commit, because
both route through the same per-platform compile in the module; `builder` is how a stamp is
checked locally without a push. Adding a second, bespoke link line here to produce a
stamped binary would be a second definition of the build, and the two would drift — see the
continuous integration section of [`CLAUDE.md`](../CLAUDE.md). The `model` job in CI runs
the binary the pipeline built and requires it to report itself stamped, which is what turns
the module quietly ceasing to stamp into a failure rather than into a fleet of anonymous
binaries.
