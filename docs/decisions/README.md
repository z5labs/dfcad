# Architecture decision records

These records exist because the decisions they describe are expensive or impossible to
walk back once data exists, and because each of them can be gutted by a change that does
not appear to touch it. Modelling a dimension as a plain number deletes the entire
provenance model without editing a single line that mentions provenance. A record is the
only thing standing between that change and a reviewer who did not know the semantics
were there.

Each record states one decision, why it was made, what it costs, and what would reverse
it. The last two are not optional garnish. A decision recorded without its cost reads as
dogma, and the next maintainer either obeys it without understanding or ignores it
entirely; a decision recorded without a reversal condition cannot be revisited on
evidence, only overturned by whoever is loudest.

## Numbering convention

- Records are numbered with a four-digit, zero-padded, monotonically increasing integer:
  `0001`, `0002`, `0003`.
- The file name is `NNNN-kebab-case-title.md` — the number, a hyphen, then the title of
  the record in lower-case with hyphens for spaces.
- `0000-template.md` is reserved for the template and is not a decision.
- **A number is never reused and never renumbered.** Records are referred to by number
  from commit messages, issues and other records; a number that means one thing today and
  another thing next year makes every one of those references a lie. A superseded record
  keeps its number and its file, with its status changed and a pointer to the record that
  replaced it.
- The next number is the highest existing number plus one. Two branches that pick the same
  number is a merge conflict to resolve, not a reason for a more elaborate scheme.

## Status

A record is in one of three states:

| Status       | Meaning                                                                |
|--------------|------------------------------------------------------------------------|
| `Accepted`   | In force. The codebase is expected to match it.                        |
| `Superseded` | Replaced by a later record, which is named in the status line.          |
| `Withdrawn`  | Retracted without a replacement — the decision turned out not to be one.|

There is no `Proposed`. A record lands when the decision is made, in the same pull request
as the change that makes it, so that the reasoning and the code arrive together.

## Writing one

Copy [`0000-template.md`](./0000-template.md), give it the next number, and fill in every
section. Prose, not bullet fragments — the reader is a maintainer a year from now who has
the code in front of them and needs the part that is not in the code.

## Index

| #                                                    | Decision                                                 | Status   |
|------------------------------------------------------|----------------------------------------------------------|----------|
| [0001](./0001-two-node-families.md)                   | Two node families: semantic and geometric                | Accepted |
| [0002](./0002-immutable-id-mutable-label.md)          | Immutable id, mutable label                              | Accepted |
| [0003](./0003-id-namespaces-are-a-closed-registry.md) | Id namespaces are a closed registry                      | Accepted |
| [0004](./0004-globalid-derives-from-a-pinned-namespace.md) | GlobalId derives from a pinned namespace            | Accepted |
| [0005](./0005-one-linear-unit-per-frame.md)           | One linear unit per frame, foot definition pinned        | Accepted |
| [0006](./0006-accuracy-is-one-sigma.md)               | Accuracy is 1σ, and systematic terms add linearly        | Accepted |
| [0007](./0007-rank-is-closed.md)                      | Rank is closed: normal and deprecated                    | Accepted |
| [0008](./0008-a-bare-scalar-is-a-load-error.md)       | A bare scalar where a claim belongs is a load error      | Accepted |
| [0009](./0009-derived-values-are-never-written-back.md) | Derived values are never written back to source        | Accepted |
| [0010](./0010-the-engine-carries-no-domain-vocabulary.md) | The engine carries no domain vocabulary             | Accepted |
| [0011](./0011-assertions-are-named-parameterised-checks.md) | Assertions are named parameterised checks, not expressions | Accepted |
| [0012](./0012-tolerances-are-registry-data.md)        | Tolerances are registry data                             | Accepted |
| [0013](./0013-variants-are-branches.md)               | Variants are branches                                    | Accepted |
| [0014](./0014-the-machine-output-contract-is-part-of-the-interface.md) | The machine output contract is part of the interface | Accepted |
| [0015](./0015-the-cli-is-the-primary-write-path.md)   | The CLI is the primary write path                        | Accepted |
| [0016](./0016-writes-are-all-or-nothing.md)           | Writes are all-or-nothing                                | Accepted |
| [0017](./0017-the-answer-is-the-default-and-the-evidence-is-asked-for.md) | The answer is the default and the evidence is asked for | Accepted |
| [0018](./0018-three-versions-stamped-by-the-pipeline.md) | Three versions, moving separately, stamped by the pipeline | Accepted |
| [0019](./0019-the-registry-is-the-distribution-channel.md) | The registry is the distribution channel, digests are authoritative, no `latest` | Accepted |
| [0020](./0020-export-is-a-boundary-and-the-closed-set-is-what-crosses-it.md) | Export is a boundary, and the closed set is what crosses it | Accepted |
| [0021](./0021-an-export-is-a-build-output-keyed-by-its-source-digest.md) | An export is a build output keyed by its source digest | Accepted |
