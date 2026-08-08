# The model gate

`dfcad fmt --check`, `dfcad check` and `dfcad review`, run over a model root on
every pull request and on every push to `main`, so that a branch whose model is
broken — or whose change to the model needs an explanation — is visibly failing
rather than merely unreviewed.

Until this gate exists, a proposal with a broken model looks exactly like one
without, and pull-request-based work is a habit rather than something
load-bearing. That is the whole of what it is for.

## The two halves of CI, and which one this is

| Half | What it checks | Where it comes from |
|---|---|---|
| The Go half | `fmt`, `vet`, `golangci-lint`, `go test -race`, the per-platform image build and the publish | `GoApp.Ci` in [`z5labs/devex/daggerverse/z5labs`](https://github.com/z5labs/devex/tree/main/daggerverse/z5labs), invoked as one `dagger call` |
| The model half | `dfcad fmt --check`, `dfcad check` and `dfcad review` over a model root | **this directory** |

**This is the model half, and it is the half a consuming data repository
adopts.** A repository holding entities rather than engine source does not run
this repository's Go pipeline — it has no Go to run. It runs the CLI over its
own entities, which is exactly what `gate.sh` does.

The Go half is deliberately not restated anywhere in this repository. The module
has no hook for a project's own commands, which is why this half exists beside
it rather than inside it; a workflow step here which reran a module stage would
be a second definition of the standard, and the two would drift. See the
continuous integration section of [`CLAUDE.md`](../../CLAUDE.md).

## What is here

| Path | What it is |
|---|---|
| `gate.sh` | The gate. Runs the commands over one model root, annotates what failed, records how long it took and how much it said, and exits non-zero if any of them said no. |
| `selftest.sh` | Runs `gate.sh` against both broken models and requires it to block, and to say where. |
| `broken/` | A deliberately broken model which **loads**: one file not in canonical form, one room whose outline does not close against the invariant its type states, and one corner rewritten with no measurement behind it. |
| `unloadable/` | A deliberately broken model which **does not load**: one file which does not parse, and one node naming a type the registry does not declare. |

There are two models because no single one can exercise the whole gate. A file
which does not parse stops the model loading, and a model which does not load
runs no rule and produces no violation with a span on it; a model which loads
has no failed file for the `fmt` stage to report a diagnostic against. One root
can hold either the spanned check violation or the file which does not parse,
and not both.

## Running it

```sh
.github/gate/gate.sh --binary ./dfcad --root cmd/dfcad/testdata/budget
```

Run it from the directory paths should be reported relative to. In CI that is
the repository root, because GitHub resolves an annotation's file against the
repository root; locally it is wherever the paths in the log will make sense.

`--results <dir>` says where to write the structured output. The default is a
temporary directory, which is what a local run wants. CI passes a path it then
uploads as the `dfcad-gate-results` artifact, holding, per model root:

| File | Holds |
|---|---|
| `<root>.fmt.json` | `dfcad fmt --check`'s result object: one entry per file, with its status and any diagnostic. |
| `<root>.check.json` | `dfcad check`'s result object: the summary of how many rules there were, ran, passed and failed, and every violation. |
| `<root>.review.json` | `dfcad review`'s result object: which two revisions were compared, the policy each check ran under, and every finding. Written only when `--against` was given. |
| `<root>.review.md` | The same findings as the Markdown a reviewer reads, which the gate appends to `$GITHUB_STEP_SUMMARY`. Written only when `--against` was given. |
| `<root>.timing.json` | How many milliseconds each command took, and the three together. Written on every run, including the ones which found something — those are the runs the gate is slowest on. |
| `<root>.annotations.json` | How many annotations each stage placed, the output contract they were read against, and whether any result could not be read at all. |

## The binary it runs

The gate takes a `--binary` rather than reaching for `go run`, and the workflow
passes it the binary the standard pipeline built:

```sh
dagger call -m github.com/z5labs/devex/daggerverse/z5labs \
  go-app --source=. --pkg=./cmd/dfcad \
  builder binary export --path=./dfcad
```

`Builder` routes through the same per-platform build `Ci` does, so the tool the
gate runs is the tool the pipeline ships. A `go run ./cmd/dfcad` would be a
second way of producing it, and the gate and the shipped artifact could then
disagree about what `dfcad check` means — which is the disagreement a gate is
least able to survive.

A data repository has no `cmd/dfcad` to build. It should install a published
`dfcad` at a pinned version and pass that path instead; the pin is what stops
the gate's verdict moving under a model nobody changed.

## The third question, and why it is opt-in

`fmt --check` and `check` each constrain **one** revision. Some things are suspicious only
as *changes*: a wall which quietly moved with no new measurement, a claim retracted with
nothing standing in its place, an id which is simply gone and every reference which now
names nothing. None of those can be seen without the revision the change was made against,
and every one of them loads, formats and passes every rule the model states.

`--against <ref>` is what turns that stage on. The gate then runs

```sh
dfcad review --root <root> --against <ref> --annotate <results>/<root>.review.md
```

and compares the model against the **merge base** of `<ref>` and `HEAD` — not against the
tip of `<ref>`, which would report everybody else's work as part of this change.

It is opt-in rather than always-on because it is the one stage which needs two revisions.
A tarball, a shallow clone and a model root which is not in a git repository each have no
second revision to be compared with, and a gate which refused those outright would be one
nobody could adopt a step at a time. Left out, the stage does not run and the other two
answer exactly as they did before.

What a checkout has to be for it to run:

- **inside a git working tree**, because that is where the previous revision is;
- **with the history back to the merge base**, which means `fetch-depth: 0` on
  `actions/checkout`. A shallow clone is refused rather than answered from — git reports a
  merge base at the point the history was cut off, and a review against that would
  attribute the whole of the branch's ancestry to this change. The message names what to
  fetch.

`--policy <check>=<ruling>` is passed straight through, and is how a repository states what
each finding means to it: `failure`, `warning` or `ignored`. A change which genuinely
re-surveyed a room is one somebody meant, and saying so once in the invocation is the
supported way to say it. A finding ruled `ignored` is still in the result object, because a
check silently switched off is one nobody remembers is off.

## Reading a failure

Everything needed to fix the model is in the log, without a local reproduction:

- **Diagnostics** are `dfcad`'s own human rendering on stderr — `file:line:col`,
  the offending source line, and a caret under the span — streamed straight
  through. Each command's two streams are kept apart the way the machine output
  contract requires: stdout is the result object and goes to a file, stderr is
  for whoever wrote the model and goes to the log.
- **A formatting failure** additionally prints `dfcad fmt --diff`, so the exact
  hunks are there. `--diff` writes nothing and implies `--check`, so it cannot
  change what the gate already decided.
- **Annotations** are emitted from the JSON, never by parsing the prose. A file
  which does not parse and a violation both carry a span, so those land on the
  line; a file which is merely not canonical has no line to land on, because
  what is wrong with it is the whole file's shape, so its annotation is at file
  level. A span is the string `path:line:column-line:column`, and the numbers
  are read from the right, so a path holding a colon or a dash still parses.
- **A review finding** is annotated at the severity its policy gave it — a
  `failure` as an error, a `warning` as a warning — and names the commit which
  introduced the change, so the reviewer is not left bisecting the branch for
  it. A finding ruled `ignored` is annotated nowhere, which is what that ruling
  means. A finding about something this revision no longer holds carries a span
  in the **merge base**, which is a file the checkout may not have; the message
  says which revision the line is in, so a dropped annotation does not read as a
  wrong line number.
- **The review's own summary** is Markdown appended to `$GITHUB_STEP_SUMMARY`,
  which is the page a reviewer opens from the pull request. It is written to a
  file first and appended after the runtime table, because appending it between
  that table's header and its rows would break the table.

One case has no annotation of its own: a model which **does not load** exits `2`
with its diagnostics rendered on stderr and nothing on stdout to annotate from,
because the machine form of a load diagnostic is carried by the commands which
report per file and `check` reports per rule. The gate says so explicitly and
the log carries the file, the line and the caret. Closing that gap is a change
to the engine's output contract, not something to paper over here by parsing the
human rendering — the two renderings exist precisely so that neither is derived
from the other.

## The output contract it reads, and why it is stated

Every annotation the gate places is read out of a result object with `jq`, and
the shape of that object is [the machine output
contract](../../docs/machine-output.md). `gate.sh` states the version it reads
in one place — `OUTPUT_CONTRACT`, which `gate.sh --contract` prints — and three
things are checked against it:

1. **Every stage checks the result it just read.** A result object carries the
   contract it was written against in `.version`. One the filters were not
   written for is reported as an annotation of its own and fails the gate,
   because a gate which cannot read what the tool wrote does not know whether
   the model is sound.
2. **The self-test checks the binary.** `dfcad version` reports
   `.contracts.output`, and the self-test requires it to be the number the gate
   reads. That is the earlier signal of the two: it fails on the build which
   bumps the contract, whatever any one result object happens to contain.
3. **The self-test counts what each stage annotated**, and requires each of
   `fmt`, `check` and `review` to have said something about a model which gives
   it something to say.

All three exist because of one incident. Contract v2 made every span a string
where it had been two nested objects; the filters here went on reading
`.span.start.line`; and for three months the gate emitted **no annotation on any
repository** while still exiting non-zero — it blocked by crashing, with nothing
on the line, no `timing.json` and no sign in the run that anything was wrong. A
gate which blocks and says nothing looks exactly like a gate doing its job.

An engine change which bumps the contract therefore fails here first, in the
pull request that makes it, with a message naming the filters that have to
change.

## Why the self-test runs on every build

The self-test is the answer to "has this gate ever actually blocked anything?",
and it is asked on every run rather than once when the gate was written.

A one-off broken branch proves the gate worked on the day somebody tried it. It
cannot notice the change six months later that makes the gate pass everything —
an exit code swallowed by a pipeline, a `jq` filter which stopped matching a
renamed field, a `--binary` which is not there. Those failures are silent by
construction: everything goes green, which is what a working gate also looks
like.

So both broken models are committed, and `selftest.sh` runs `gate.sh` against
each of them before the gate is believed about anything else in the job. It
requires:

- a non-zero exit over each of them, and every result file written — including
  `timing.json`, which is written after the annotations and so goes missing when
  a gate dies partway through emitting them;
- `fmt --check` to have flagged exactly the one file that is not canonical;
- `check` to have failed the invariant on the model which loads, and to have
  refused the model which does not without claiming to have run any rule over
  it;
- `review` to have found the corner which moved with nothing measured behind it;
- a **non-zero annotation count for each of the three stages**, and, for every
  finding which names a line, an annotation on that line — matched against the
  span the result object carries, parsed a second time here in a different
  engine from the gate's, so that a filter and its assertion cannot agree with
  each other about a form neither of them reads correctly.

A gate which stopped blocking fails there, in the run that broke it. So does one
which stopped saying where.

The review stage needs two revisions, and the self-test makes them rather than
relying on this repository's own history: it builds a throwaway git repository
whose merge base is `broken/` with every `*.dfc.prior` file laid over its
neighbour, and whose branch is `broken/` as it stands. A `.dfc.prior` file is not
walked by the loader — the extension is not `.dfc` — so it is inert to every
stage of the gate, and it keeps the change under review down to the one number
that actually differs instead of a second copy of the whole model.

The self-test prefixes the gate's own workflow commands with `[captured] `
before echoing them, and unsets `GITHUB_STEP_SUMMARY` for the run. Without both,
a run whose gate correctly failed would decorate itself with three errors and a
red row in the runtime table that are the point of the run rather than a problem
with it.

The prefix is a visible marker rather than indentation because **Actions strips
leading whitespace before it looks for a workflow command**, so two spaces in
front of `::error` neutralise nothing — measured on
[run 30981085140](https://github.com/z5labs/dfcad/actions/runs/30981085140),
where an indented line still produced an annotation. Anything non-blank ahead of
the colons does work.

## Adopting it in a data repository

1. Copy `gate.sh` and, if you want the assurance, `selftest.sh`, `broken/` and
   `unloadable/`. Each of those is a model in its own right, so both are worth
   rewriting in your own vocabulary rather than carrying this one's. What has to
   survive the rewrite is what each stage is given to say: a file which is not
   canonical, a rule which fails on something that loads, a `*.dfc.prior` file
   holding the revision before the change under review, and — in the other root
   — a file which does not parse.
2. Install a pinned `dfcad` and pass it as `--binary`.
3. Call `gate.sh` once per model root you want gated, with `--results` pointing
   at a directory you upload. Add `--against origin/<default branch>` once your
   checkout has the history for it, and `--policy` for any check your repository
   wants ruled differently.
4. Make the job a required status check on your default branch. A gate that does
   not block is a report.

What you do **not** copy is the `build` job. That is this repository's Go
pipeline, and a data repository has none.
