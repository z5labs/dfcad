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
| `gate.sh` | The gate. Runs the commands over one model root, annotates what failed, records how long it took, and exits non-zero if any of them said no. |
| `selftest.sh` | Runs `gate.sh` against `broken/` and requires it to block. |
| `broken/` | A deliberately broken model: one file not in canonical form, and one node naming a type the registry does not declare. |

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
| `<root>.timing.json` | How many milliseconds each command took, and the two together. |

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
  level.
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

## Why the self-test runs on every build

The self-test is the answer to "has this gate ever actually blocked anything?",
and it is asked on every run rather than once when the gate was written.

A one-off broken branch proves the gate worked on the day somebody tried it. It
cannot notice the change six months later that makes the gate pass everything —
an exit code swallowed by a pipeline, a `jq` filter which stopped matching a
renamed field, a `--binary` which is not there. Those failures are silent by
construction: everything goes green, which is what a working gate also looks
like.

So `broken/` is committed, and `selftest.sh` runs `gate.sh` against it before
the gate is believed about anything else in the job. It requires a non-zero
exit, both stages to have written their results, `fmt --check` to have flagged
exactly the one file that is not canonical, `check` to have refused the model
without claiming to have run any rule over it, and an annotation naming the file
and a diagnostic naming the file, line and column. A gate which stopped blocking
fails there, in the run that broke it.

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

1. Copy `gate.sh` and, if you want the assurance, `selftest.sh` and `broken/`.
   `broken/` is a model in its own right — a registry, a file that is not
   canonical, and a node naming an undeclared type — so it is worth rewriting in
   your own vocabulary rather than carrying this one's.
2. Install a pinned `dfcad` and pass it as `--binary`.
3. Call `gate.sh` once per model root you want gated, with `--results` pointing
   at a directory you upload. Add `--against origin/<default branch>` once your
   checkout has the history for it, and `--policy` for any check your repository
   wants ruled differently.
4. Make the job a required status check on your default branch. A gate that does
   not block is a report.

What you do **not** copy is the `build` job. That is this repository's Go
pipeline, and a data repository has none.
