# 0014. The machine output contract is part of the interface

**Status:** Accepted

## Context

The primary consumer of this engine's output is a program: a script, a CI job, another
tool, or an LLM agent working through the command line. A person reads the output too, but
they are not the case that breaks.

Nobody decides to make a command line tool interactive-first. It happens because output is
written while looking at a terminal. A progress line is added because a long query felt
silent. A blank line is added because two sections ran together. A count is printed at the
end because it is useful to see. Column alignment is added because a table looked ragged. A
colour is added because errors were easy to miss. Every one of those is a small improvement
to what a person reads, and collectively they make the output unparseable — and they do it
without a single commit that says "make the CLI interactive".

The damage is specific and it is silent. A caller pipes stdout into `jq`. It works. Someone
adds a "Loaded 412 nodes" line to stdout, and now `jq` gets prose before the JSON and
fails — or worse, the caller was reading the last line, and now it reads a summary count
instead of a result. Nothing failed at the point of the change; the test suite passed,
because the tests read the fields rather than the stream.

Exit codes rot the same way. A tool that returns 0 for success and 1 for everything else
forces the caller to parse a message to tell "this model has a check failure" from "this
file does not exist" — which are completely different situations for a CI job, one meaning
the model is wrong and the other meaning the invocation is wrong.

The shape of the output is a compatibility surface whether or not anyone says so. The only
question is whether it is one deliberately, with a version on it, or accidentally, with
every caller pinned to whatever it happened to emit.

## Decision

**Stdout is structured results, and nothing else.** Every invocation that produces a result
writes a single JSON object to stdout. Not JSON Lines, not several objects, not JSON
preceded by a banner or followed by a summary: one object, the whole of stdout. A caller
can pipe stdout into `jq` with no filtering.

**Nothing decorative ever appears on stdout.** No progress, no counts, no headings, no
blank lines, no colour, no alignment, no spinner, no "done". Not behind a flag that
defaults off, not when stdout is a terminal, not on the first run. If it is for a person,
it is not on stdout.

**Stderr is everything human-facing.** Diagnostics, progress, warnings and prose go to
stderr, where a person reads them and a pipeline ignores them. Diagnostics carry their
positions and spans as the diagnostics decision requires, and each diagnostic's
machine-readable form appears in the stdout object as well — the two renderings are
produced from the same data, and neither is derived by parsing the other.

**The output shape is documented and versioned.** The object carries a version field. The
documented shape is the contract: fields are added compatibly, and a field is never
removed, renamed or given a different meaning without the version changing. A caller that
reads a documented field keeps working across releases.

**Exit codes distinguish outcomes that mean different things:**

| Code | Meaning                                                                 |
|------|-------------------------------------------------------------------------|
| 0    | Success. The command did what was asked.                                |
| 1    | Check failure. The model loaded and answered; an assertion did not hold. |
| 2    | Load failure. The model could not be read or did not validate.          |
| 3    | Usage error. The invocation itself was wrong.                           |

A caller can branch on the code alone, without reading a message.

**Output is deterministic.** The same inputs produce byte-identical stdout: keys in a fixed
order, collections in a documented order, no timestamps, no durations, no paths that vary
by machine. Two runs that disagree mean the model disagreed.

**The contract is tested as a contract.** Tests assert on the exact shape of the object and
on exit codes, so a change to either is a test change somebody has to justify, not a silent
break.

## Consequences

Piping works, permanently. `dfcad query ... | jq` needs no filtering, on any release, and a
caller can rely on that without reading the source to check what got added.

Progress is free. Anything worth telling a person can be written to stderr at any time
without touching the contract, so there is no tension between a usable interactive
experience and a stable machine one.

CI can branch on outcome. A check failure and a broken invocation take different paths
without a job parsing English, which is what makes the CI integration something other than
a fragile grep.

Diffing runs is meaningful. Because output is deterministic, two runs can be diffed
directly and any difference is a real difference in the model.

An agent consuming the tool can rely on a fixed field to find its answer, rather than
locating it positionally in whatever the tool printed — which is the difference between an
integration that works and one that works until the next release.

Adding a field is cheap; changing one is a versioned event. That asymmetry is deliberate:
growth should be easy and breakage should be loud.

## Cost

The output is worse to read raw. A single JSON object is not what anyone wants to look at
after typing a command, and the answer — pipe it through `jq` — is a real ergonomic tax on
casual use.

Two renderings of everything. A diagnostic exists as human text on stderr and as structured
data on stdout, both produced from the same source, and keeping them genuinely in sync is
ongoing work rather than a one-time cost.

Buffering. A single object cannot be emitted until the result is complete, so a long query
produces nothing on stdout until it finishes. Streaming partial results would break the
contract, so progress has to live on stderr where a pipeline cannot use it.

Versioning discipline. Every output change has to be classified as additive or breaking,
and getting that judgement wrong breaks callers in exactly the way the contract exists to
prevent. That is a review burden on changes that would otherwise be trivial.

Determinism constrains implementation. Collections need documented ordering even when the
natural implementation would not produce one, and anything genuinely nondeterministic —
timing, concurrency-dependent ordering — cannot appear on stdout at all.

## What would reverse it

A result set too large to hold in memory as one object would be a genuine argument for a
streaming format such as JSON Lines. That is a versioned change to the contract, opted into
by the caller, not a quiet switch — and it keeps every other property, because JSON Lines on
stdout is still nothing but results.

The reversal that cannot be undone is putting anything human-facing on stdout, even once.
The moment prose shares the stream with results, callers start tolerating it: they grep, or
skip lines, or read the last line, and those workarounds become the real contract. Cleaning
the stream up later breaks every one of them, and there is no way to find them, because
they live in other people's scripts.
