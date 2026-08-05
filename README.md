# dfcad

A data-first CAD engine: a declarative entity graph in text, queried and edited
with provenance and error budgets intact.

> **Status: under construction.** This README describes the engine's scope — the
> boundary it is being built to, and the boundary it is not allowed to cross.
> Most of what follows is not implemented yet.

## What it does

A model is a set of plain text files under version control. The engine works on
that model in four ways:

- **Loads** a declarative entity graph from text, reporting every problem it
  finds in one pass with a file, line and column for each.
- **Answers questions** about the graph — what exists, what it is, what it
  contains, what it touches — and returns the provenance and the error budget
  behind every answer, not just the number.
- **Edits** the graph safely. The command line interface is the primary write
  path: it validates first, writes canonical form, and refuses a change that
  would produce a graph that fails to load. Nothing is ever partially written.
- **Checks assertions** over the graph, so that the properties a model is
  supposed to hold are enforced in CI rather than remembered.

## The closed sets

Two vocabularies are compiled into the engine, and they are closed. Nothing else
is.

| Set        | Members                                                                 |
|------------|-------------------------------------------------------------------------|
| `kind`     | `Zone`, `Site`, `Building`, `Storey`, `Space`, `Element`, `Interface`    |
| `geometry` | `point`, `line`, `area`, `surface`, `solid`, or absent                   |

Two further sets are fixed by the engine for the same reason:

- **Node families.** Semantic nodes carry a `kind` and a `type`. Geometric nodes
  — `vertex`, `edge`, `loop` — carry neither, only a frame, claims and
  references to each other.
- **Checks.** Assertions are named, parameterised checks drawn from a closed
  registry, not an expression language. Adding a check is an engine change.

These sets are small, and they are meant to stay that way. Growing one is a
deliberate decision recorded in `docs/decisions/`, not a routine change.

## What arrives as registry data

Everything domain-specific is supplied by the consuming repository as registry
files that the engine loads and validates before it interprets anything else:

| Registry        | Declares                                                        |
|-----------------|------------------------------------------------------------------|
| Types           | Each `type` with its permitted `kind`, permitted `geometry` and a description |
| Claim predicates| Each predicate with its unit, value shape and validation rules    |
| Frames          | Each frame with its linear unit and its parent                    |
| Id namespaces   | The permitted namespaces for `namespace:local` identifiers        |
| Tolerances      | Named tolerances with values and units                            |
| File routing    | Which file a newly authored node is written to, by namespace, kind and type |

The engine attaches no meaning to any of these beyond the structure it checks.
A wall, a circuit, a setback rule and a survey monument are all registry entries
somewhere else; none of them appears in this repository.

## Non-goals

The engine deliberately does not contain:

- **No domain vocabulary.** No type, predicate, frame, namespace or tolerance
  specific to buildings, land, or any other subject matter. Vocabulary growth is
  only reviewable when it lives in a file whose diff someone reads, and that
  file belongs to the data repository.
- **No database.** The source tree is the store. Derived values are build
  outputs keyed by the digest of the source, never written back into the
  authored files.
- **No daemon.** Every invocation is a process that starts, does its work and
  exits.
- **No network transport.** No server, no client, no wire protocol. The engine
  reads files and writes files.

## Where does a change belong?

| The change...                                                | Belongs in       |
|--------------------------------------------------------------|------------------|
| Adds a type, predicate, frame, namespace, tolerance or routing rule | the data repo |
| Names a specific building component, discipline or code rule  | the data repo    |
| Adjusts a numeric tolerance                                   | the data repo    |
| Adds a query, an output field or a CLI subcommand             | here             |
| Changes how claims resolve or how uncertainty propagates      | here             |
| Adds a named check that any model could use                   | here             |
| Adds a member to `kind` or `geometry`                         | here, via an ADR |

The rule behind the table: if the change would be meaningless to a repository
modelling a different subject, it is not an engine change.

## Documentation

- [`docs/decisions/`](./docs/decisions/) — the architecture decision records.
  Each states the decision, its reasoning, what it costs and what would reverse
  it. The identity, claim and uncertainty decisions are recorded, as are the
  engine-boundary, CLI and authoring ones.
- [`docs/machine-output.md`](./docs/machine-output.md) — the command line
  interface's output contract: what is on stdout, what is on stderr, the shape
  of each command's JSON object and the rule by which that shape may change,
  and what each exit code means.
- [`docs/siting-worked-example.md`](./docs/siting-worked-example.md) — one
  question, *does this building fit on this plot?*, followed from the claims it
  is answered from through the frame chain and the overlay to the error budget
  it comes back with, and asked again after a re-survey. It is where the
  systematic terms earn their keep: the arithmetic is spelled out beside the
  naive all-quadrature figure it must not equal.
- [`docs/surface-accuracy-gate.md`](./docs/surface-accuracy-gate.md) — a derived
  surface put to a decision: *does this patio fall enough to drain?*, with the
  accuracy the decision needs stated before the surface was built and the
  achieved accuracy measured against it. It is where the propagation through a
  surface is spelled out — the shots in quadrature, the base station added once
  and cancelling out of a difference, the ground between the shots charged by
  distance — and it records a miss: the requirement is missed by a factor of
  four, and survey density is not the reason.
- [`docs/token-budget.md`](./docs/token-budget.md) — what the discovery path
  costs an agent, measured with a real tokenizer against a representative model,
  and how that compares with reading the files instead. It is regenerated from
  the measurement rather than written down, so the claim in the repository is
  always the current one.
- [`docs/observation-file.md`](./docs/observation-file.md) — the format field data
  is recorded in: one record per line, appended as it is collected and never
  edited, with a retirement being a later record naming the one it supersedes.
  It states the line schema field by field, what each precision figure means,
  and the append-only invariant a validator checks between two revisions.
- [`SPEC.md`](./SPEC.md) — the entity syntax: the legal tagged forms, their
  arity and ordering, and the canonical printing of each. It is the definition
  of the format; the loader is implemented against it, not the other way round.
- [`.github/gate/README.md`](./.github/gate/README.md) — the model gate:
  `dfcad fmt --check`, `dfcad check` and `dfcad review` run over a model root on
  every pull request, so a branch whose model is broken — or whose change to the
  model needs an explanation — is visibly failing. It is written to be copied —
  a consuming data repository runs the CLI over its own entities, and this is
  that half of CI.

## Install

As a library:

```sh
go get github.com/z5labs/dfcad
```

As a command line tool:

```sh
go install github.com/z5labs/dfcad/cmd/dfcad@latest
```

## License

[MIT](./LICENSE)
