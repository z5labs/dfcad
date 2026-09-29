# 0029. Every diagnostic a run renders is written in its answer on stdout

**Status:** Accepted

## Context

[0014](./0014-the-machine-output-contract-is-part-of-the-interface.md) says where a
diagnostic goes twice over. Its human rendering goes to stderr, "and each diagnostic's
machine-readable form appears in the stdout object as well — the two renderings are produced
from the same data, and neither is derived by parsing the other". The repository's
conventions say the same thing from the other side: every diagnostic has both a human
rendering and a machine-readable form carrying the same fields.

The library holds up its half. Every field of `dfcad.Diagnostic` — `severity`, `span`,
`message`, `hint`, `related` — carries a JSON tag, and `encoding/json` writes the machine
form straight from the fields `Render` reads. The command line interface holds up very
little of its half. One command writes the form: `fmt`, under `files[].diagnostics`. Two
more write something close to it for their own findings — `check`'s `violations[]` and
`chorded[]`, `review`'s `findings[]` — and nothing of the load's. Every other diagnostic
reaches stderr only, through the one `render` in `cmd/dfcad`, as `path:line:col: error: …`
text, under the default `--format json` exactly as under `human`.

Run against copies of the siting fixture, every command that can refuse falls into one of
these rows:

| Command | The load refused the model | The command's own refusal | Diagnostics in the stdout object |
|---|---|---|---|
| `list-types`, `list-instances`, `list-geometry`, `get`, `traverse`, `claims`, `conflicts` | exit 0, answer with `"refused": true` | — | none |
| `check` | exit 2, object with `"refused": true` | exit 1, `violations[]` | its own violations and chorded edges; none of the load's |
| `resolve`, `route` | exit 2, nothing | usage errors; `resolve` exits 4 or 5 with its result on an ambiguity | none |
| `measure`, `tessellate`, `buildable`, `site`, `plan` | exit 2, nothing | exit 1, object with `derived`/`sited`/`planned` false; `plan` names the node under `undrawn[]` | none |
| `export`, `export-map` | exit 2, nothing | exit 1, `derived` false, `files` empty; `export-map` names the node under `undrawn[]` | none |
| `review` | exit 2, nothing | exit 1, `findings[]` | its own findings; none of the load's |
| `apply` and the twelve write commands | exit 2, nothing | exit 2, nothing — the model the change would produce does not load | none |
| `fmt` | — | exit 2, `files[].status` `failed` | `files[].diagnostics` |

A footprint ring that crosses itself is the case that made this matter. `check` passes over
it, because no rule reads the ring. `export` refuses it with exit 1, an object saying
`derived` is false and `files` is empty, and the one fact a caller needs — which loop, where,
crossing what — in a caret rendering on stderr. A dangling edge reference is worse: `export`
exits 2 and stdout is empty, so the reason exists only as prose.

A consumer building artefacts from a model (Discussion #303) has done what any caller left
with that does. It regexes stderr for `error:` lines, then parses the entity files itself to
work out which node each reported loop belongs to. That is exactly the integration 0014
exists to prevent: prose matched as an interface, a second parser of the source, and a
contract nobody versioned that breaks when a message is reworded.

The documentation disagrees with 0014 in two places. `plan`'s section says of the diagnostic
behind an undrawn node that "a second copy of it on stdout would be a second thing to keep
true", and the comment on `undrawn[].reason` says the same. And the per-command rule that a
derivation, an export, `review` or a write over a refused model writes nothing to stdout —
set out under *Diagnostics and the exit code of a read* and under the write commands, and
carried by the refusal path in `cmd/dfcad/main.go` — leaves no object for the machine form
to be in on exactly the runs where a caller most needs it.

Two places for the machine form were live. **JSON on stderr under `--format json`**, where
the human rendering is now; or **the object on stdout**, with `"refused": true` where the
model was refused. The implementation stories need one of them settled before any of them
can start, because the field names, where they sit in the object and which runs write an
object at all are the same answer for every command.

## Decision

**The machine form of a diagnostic is in the object on stdout,** as a top-level
`diagnostics` array written after every other field of the object.

JSON on stderr is rejected. `--format` is how a run reports itself to a person, and it
"never changes stdout"; making it change what stderr *is* would turn it into a second output
contract on the stream 0014 reserved for people. `json` is `--format`'s default, so every
person who typed a command would get JSON where the caret rendering is today, and would have
to ask for `human` to read their own mistakes. And a caller would read two streams — the
answer on one, the reasons for it on the other, joined by nothing — where 0014 promises one
object.

**What `diagnostics` holds.** Every diagnostic the run rendered on stderr, one entry each, in
the order rendered. Each entry has the shape `fmt` already writes under `files[].diagnostics`:
`severity`, `span`, `message`, and `hint` and `related` where the diagnostic carries them —
which is the shape `encoding/json` writes from `dfcad.Diagnostic`. Nothing is a diagnostic
that is not one: a usage error, a `dfcad <cmd>: <error>` line, `--verbose` progress and the
`--format human` summary are not in it.

The rule is one rule for every command, with no exceptions for a caller to learn per command:

- A diagnostic that `check` also carries as a violation or a chorded edge, or `review` as a
  finding, is in `diagnostics` as well. Those arrays are each command's own answer, shaped for
  its question; `diagnostics` is the run's account of what it rendered, and a caller reading
  it does not need to know which commands have a second place for some of it.
- `fmt` gains no top-level copy. Its `files[].diagnostics` already *is* this form, grouped by
  the file it is about, and a second copy of the same entries one level up would be two
  places for one list.
- `diagnostics` is **absent** where the run rendered none, so an answer over a model with
  nothing to report is byte-identical to today's.
- Where `dfcad.Diagnostics`' limit suppressed some, `diagnostics-suppressed` carries how many,
  which the stderr rendering already says in its last line. It is absent where none were.

**What a run that writes nothing today writes.** A run that read the model and rendered a
diagnostic writes an object. Where the load refused the model — `resolve`, `route`,
`measure`, `tessellate`, `buildable`, `site`, `plan`, `export`, `export-map`, `review` — or
a change was refused — `apply` and every write command — that object is the envelope,
`"refused": true` and `diagnostics`, and nothing else. The exit code does not move: each of
those is exit `2` today and is exit `2` after.

The reasons each of those wrote nothing still hold, and the object meets them by carrying
nothing they objected to. A figure out of a refused model answers a question nobody asked,
so the object carries no figure, no `derived`, no `digest` and no `files`. An object
describing a change that did not happen reads like one describing a change that did
([0022](./0022-a-command-whose-product-is-a-file-answers-on-stdout.md)), so the object
carries no `dryRun`, no `files` and no effects. What is left says the run was refused and
why, and cannot be mistaken for an answer.

Help, a usage error, a root that is not a readable directory and an `--entity-format` this
engine does not implement read no model and render no diagnostic, and still write nothing.

**What names the things a diagnostic is about.** Each entry carries two more fields:

- `ids` — the vertices, edges, loops and nodes whose forms enclose its `span` or the `span`
  of one of its `related` entries;
- `nodes` — each of those that is a node, and every node whose boundary reaches each one that
  is a loop, an edge or a vertex.

Both are computed from the spans and the model's boundary index, never read out of
`message`, and both are absent where empty. The relation `nodes` reads is the reverse of
`traverse boundary-of` — the one `traverse bounds` walks, which Discussion #299 asked for as
`bounded-by` — so a caller asking `traverse bounds` about a loop a diagnostic named, and
reading that diagnostic's `nodes`, gets the same nodes.

**The contract version does not change.** `diagnostics`, `diagnostics-suppressed`, `ids` and
`nodes` are added fields, which the versioning rule allows at any time. An object on exit `2`
is what the exit-code table already allows — "the result object, or empty when nothing could
be loaded at all" — and a run that read the model and refused it is not a run that could load
nothing.

**`plan`'s "a second copy of it on stdout would be a second thing to keep true" is
withdrawn.** It assumed the machine form would be a second thing written beside the first.
Both renderings are produced from one `dfcad.Diagnostic`, the human one by `Render` and the
machine one by `encoding/json`, so there is nothing to keep in step: the two cannot disagree
without the value they were both written from disagreeing with itself. `undrawn[].reason`
stays a token for the reason it gives, and the diagnostic behind it is now in the same object.

## Consequences

A caller reads one stream. The answer, whether the model was refused, and every reason the
run gave a person are in one object, and the reasons carry spans, ids and the nodes they
concern as fields. The stderr regex and the second parser of the entity files have nothing
left to do.

Every command's implementation changes in the same way, which is why the rule is settled
before any of them. The one `render` in `cmd/dfcad` is where the diagnostics a run renders
already pass; it is where they are collected for the object as well, and each command's
answer gains a trailing field. The refusal path that writes nothing today writes the envelope
with `refused` and `diagnostics` instead. `docs/machine-output.md` changes command by command
as each story lands — the per-command rules that a refused run writes nothing, `plan`'s
section and the comment on `undrawn[].reason` among them.

`ids` and `nodes` need a span-to-form lookup over the loaded model and the boundary index read
backwards. The boundary index already exists for `traverse`; the enclosing-form lookup is new
work, done once in the library so every command answers it alike.

A warning is a diagnostic. A model that renders warnings on stderr today writes them in
`diagnostics` too, so its answer grows a field while a model with nothing to report does not.

## Cost

A caller that read an empty stdout as "refused" is the one that pays. It was a reasonable
reading of the old behaviour — every derivation, export, `review` and write over a refused
model wrote nothing — and after this it finds an object. The exit code already said `2` and
still does, and the object says `"refused": true`; a caller that branches on either is
unaffected, and one that branches on the emptiness of stdout has to move to one of them.
That is a behavioural change under an unchanged version number. It is allowed by the
exit-code table as written, which is why the version holds, but it is a change somebody's
script notices.

The object grows on every run with something to report, and for a model with a hundred
errors it grows by a hundred entries plus their `related` locations, `ids` and `nodes`. The
limit that caps the stderr rendering caps this too, and `diagnostics-suppressed` says so, but
the object is no longer bounded by the answer alone.

The same finding can now appear twice in one object — once in `check`'s `violations[]` or
`review`'s `findings[]`, once in `diagnostics`. That is accepted rather than engineered away:
the alternative is a per-command list of which diagnostics are left out, and that list is the
thing a caller would have to learn and the thing that would drift.

`ids` and `nodes` are derived data on a diagnostic, and a diagnostic raised by a load that
failed early — a file that does not parse — has less model to derive them from. They are
absent then rather than guessed, and a caller cannot rely on them being present on every
entry.

## What would reverse it

A consumer that genuinely needs diagnostics as they are found — a long run where waiting for
the whole object costs more than it saves — would be an argument for streaming them. That is a
versioned change to the contract opted into by the caller, as 0014 already says for results,
and it would still carry this record's shape per entry.

Evidence that `diagnostics` on refused runs is misread as an answer — callers treating the
envelope as a result despite `refused` and the exit code — would argue for a different
top-level shape for a refusal, which is a version change.

Reversing the decision outright, back to nothing on stdout for a refused run, is expensive
once callers read `diagnostics`: every one of them would be back to parsing stderr, and a
field removed is a version change the contract exists to make loud. `ids` and `nodes` are the
part hardest to take back, because they are the fields a consumer replaces its own entity
parser with.
