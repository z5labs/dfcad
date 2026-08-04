# The machine output contract

This is the shape of what `dfcad` writes, the rule by which that shape may change, and
what each exit code means. It is the interface a script, a CI job or an agent programs
against, and it is versioned for the same reason a library's exported API is.

The reasoning behind it is
[0014. The machine output contract is part of the interface](./decisions/0014-the-machine-output-contract-is-part-of-the-interface.md).
This file is the contract itself.

## The two streams

| Stream | Carries |
|--------|---------|
| stdout | One JSON object, and nothing else. |
| stderr | Diagnostics, progress, help, and everything else meant for a person. |

Nothing human-facing is ever written to stdout — not behind a flag, not when stdout is a
terminal, not on the first run. That includes help: `dfcad --help` writes to stderr and
exits zero, so that `dfcad ... | jq` reads a result object or nothing at all, and never a
page of prose.

Stdout carries a result object exactly when the run produced a result. A run that produced
none writes nothing at all to stdout:

- help
- a usage error — no subcommand, an unknown one, a malformed flag, an unknown `--format`
- a load failure that stopped the run before it began, such as a model root that is not
  there or is not a directory

A run that *ran* and found something wrong did produce a result. A file that does not parse
and a file that is not in canonical form are both reported in the object on stdout, with a
non-zero exit code beside it.

Output is deterministic: the same input produces byte-identical stdout. Keys come out in a
fixed order, collections in a documented order, and nothing timing-dependent appears at
all.

## The envelope

Every object on stdout begins with the same two fields, whichever command wrote it:

```json
{
  "version": 1,
  "command": "fmt"
}
```

| Field     | Type     | Meaning |
|-----------|----------|---------|
| `version` | integer  | The version of this contract the object was written against. |
| `command` | string   | The subcommand that produced the object. |

The payload's own fields sit beside these, not nested beneath them, so that a caller can
read `.version` and `.command` without knowing which command it invoked, and read the rest
once it does.

`version` is one number across the whole command line interface rather than one per
subcommand. The thing being versioned is this contract — the envelope, the streams either
side of it and the exit codes — and a caller reads it once for every command it drives.

## The versioning rule

- A field may be **added** at any time, to the envelope or to any payload. A caller that
  reads a documented field keeps working across releases.
- A field is **never** removed, renamed, retyped, or given a different meaning without
  `version` changing.
- The documented order of a collection is part of the contract, and changing it is a
  version change.

Growth is cheap and breakage is loud, deliberately. Every output change has to be
classified as one or the other, and that judgement is a review burden on purpose.

## Exit codes

| Code | Meaning | Stdout |
|------|---------|--------|
| 0 | Success. The command did what was asked. | The result object, or empty for help. |
| 1 | Check failure. It ran and answered, and the answer is no. | The result object. |
| 2 | Load failure. Input could not be read, did not parse, or was not written. | The result object, or empty when nothing could be loaded at all. |
| 3 | Usage error. The invocation itself was wrong. | Empty. |

A caller can branch on the code alone, without reading a message. A check failure and a
broken invocation are different situations for a CI job — one says the model is wrong, the
other says the job is — and telling them apart must never mean matching prose.

## Global flags

Every subcommand takes these, and takes them identically.

| Flag | Default | Meaning |
|------|---------|---------|
| `--root <dir>` | `.` | The model root. A relative path argument is resolved against it; an absolute one is left alone. A root that is not a readable directory is a load failure. |
| `--format <fmt>` | `json` | How the run reports itself **to a person, on stderr**. See below. |
| `-v`, `--verbose` | off | Say more on stderr about what the run is doing. Repeatable; `--verbose=<n>` sets the level outright. |
| `-h`, `--help` | — | Print the command's help to stderr and exit zero. |

`--format` never changes stdout. `json` reports only problems on stderr; `human` adds a
readable summary of the result there as well. Stdout is byte-for-byte the same either way,
so `dfcad ... --format human | jq` still works and the person who typed it still sees the
readable version in their terminal.

`--verbose` is progress — what the run is doing, and the detail behind the summary — not
result. It never changes stdout either. Raising it adds what the run is working on, and,
under `--format human`, the status of every item rather than only of the ones something is
wrong with.

Neither flag has any effect on the exit code.

## Payloads

### `fmt`

```json
{
  "version": 1,
  "command": "fmt",
  "files": [
    {
      "path": "site/a.dfc",
      "status": "formatted"
    },
    {
      "path": "site/b.dfc",
      "status": "failed",
      "diagnostics": [
        {
          "severity": "error",
          "span": {
            "start": {"path": "site/b.dfc", "line": 1, "column": 7, "offset": 6},
            "end": {"path": "site/b.dfc", "line": 1, "column": 7, "offset": 6}
          },
          "message": "unexpected end of tokens at line 1, column 7, expected one of: RParen"
        }
      ]
    },
    {
      "path": "missing.dfc",
      "status": "failed",
      "error": "stat missing.dfc: no such file or directory"
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `files` | array | One entry per file the run reached, in walk order, plus one for each path it could not reach at all. Empty rather than null when the run reached no file. |
| `files[].path` | string | The file, exactly as the walk reached it. A path that could not be reached appears as it was given, and need not name a file. |
| `files[].status` | string | One of `unchanged`, `formatted`, `unformatted`, `failed`. |
| `files[].diagnostics` | array, optional | The problems found in the file's contents, carrying the same positions and spans as the human rendering on stderr. Neither rendering is derived by parsing the other. |
| `files[].error` | string, optional | What stopped the file being read or written, where the failure is not about its contents and so has no diagnostic. |

Statuses:

| Status | Meaning |
|--------|---------|
| `unchanged` | The file was already in canonical form. |
| `formatted` | The file was rewritten into canonical form. |
| `unformatted` | The file is not in canonical form and nothing was written, which is what `--check` and `--diff` report. |
| `failed` | The file could not be read, did not parse, or could not be written. Nothing is known about whether it is canonical. |

Exit codes: `2` if any file failed, otherwise `1` if any is `unformatted`, otherwise `0`.
A failure outranks a file that is merely not canonical, because a run that could not read
half the tree has not answered the question the other half answered.
