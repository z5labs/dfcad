# The observation file format

This is the format field data is recorded in: the shots a survey produces, one per line,
appended as they are collected and never edited afterwards.

It is deliberately not the entity format. [`SPEC.md`](../SPEC.md) covers the entity and
registry forms and says in [section 11](../SPEC.md#11-not-in-this-version) that observations
are their own format with their own specification. This is that specification.

## Contents

- [1. Why it is separate](#1-why-it-is-separate)
- [2. Files](#2-files)
- [3. Lines](#3-lines)
- [4. Fields](#4-fields)
- [5. `obs` — one observation](#5-obs--one-observation)
- [6. `retire` — retiring an observation](#6-retire--retiring-an-observation)
- [7. What the validator checks](#7-what-the-validator-checks)
- [8. Append-only](#8-append-only)
- [9. Worked example](#9-worked-example)
- [10. Not in this version](#10-not-in-this-version)

## 1. Why it is separate

Two properties separate observations from everything else in a model, and each of them on
its own would be enough.

**Bulk.** An afternoon with a rover produces thousands of shots. Entity files are read whole,
parsed into trees and held in memory by every pass over the model; a format whose files are
three orders of magnitude larger than the rest would make every one of those passes pay for
data almost none of them look at. Observations sit outside the entity files so that loading a
model does not mean loading a season of field work.

**Chain of custody.** A record was produced by an instrument, at a time, under conditions.
Editing it in place destroys the thing that made it evidence: after the edit there is no way
to tell, from the file, that the number ever said anything else. So a bad shot is retired by
a later record naming it, never by changing or deleting the original, and the format is
line-delimited precisely so that the only legal change to a file is bytes appended to its
end. [Section 8](#8-append-only) is the invariant that makes that checkable rather than
merely intended.

Everything else follows from those two. There is no nesting, because nesting is what stops a
record being one line. There is no canonical re-printing, because re-printing rewrites lines
that must not be rewritten. There is no version stamp, for the reason
[`SPEC.md` section 10](../SPEC.md#10-versioning-of-this-specification) gives: a model is
version controlled alongside the engine that reads it.

An observation is not a claim. A claim is what the model asserts about a thing; an
observation is a measurement somebody took, and several of them may sit behind one claim. The
link runs from a claim's provenance to a record identity, which is why every record carries an
id of its own in the same production entity ids are written in
([`SPEC.md` 4.1](../SPEC.md#41-identifiers)).

## 2. Files

An observation file is UTF-8 encoded text with the extension `.obs`.

| Rule | Statement |
|------|-----------|
| Encoding | UTF-8. A byte that begins no valid encoding is an error naming its position. |
| Byte order mark | A file beginning with one is an error. UTF-8 needs none, and it puts every offset in the file three bytes out. |
| Line terminator | A single line feed, `U+000A`. A carriage return is not a terminator and is not whitespace here: a `\r` before the line feed is an error, because a file written with them appends differently on the next machine. |
| Final line | **Terminated.** A last line with no line feed after it is an error. Appending to such a file rewrites its last line, which is the one thing [section 8](#8-append-only) exists to forbid. |
| Layout | A directory walk reads every file whose extension is exactly `.obs`, in the lexical order of their paths. A path named explicitly is read whatever its extension. Where the files live inside the model is the consuming repository's business; `observations/` is the convention and nothing enforces it. |

## 3. Lines

Every line is one of three things, decided by its first non-blank byte:

- **A record.** A form tag, then that form's fields, separated by whitespace.
- **A comment.** A line whose first non-blank byte is `#`. Ignored, and conventionally used
  once at the top of a file to name the columns.
- **Blank.** A line holding only spaces and tabs. Ignored.

A record is entirely on its own line. There is no continuation, no escape that spans a line
break, and no form that occupies two lines — an appended record must be appendable without
reading what came before it, and a byte-for-byte comparison of two revisions
([section 8](#8-append-only)) is only meaningful when a line is a whole record.

Two form tags exist:

| Tag | Meaning |
|-----|---------|
| `obs` | One observation. [Section 5](#5-obs--one-observation). |
| `retire` | A later record retiring an earlier one. [Section 6](#6-retire--retiring-an-observation). |

Any other tag is an error naming the tags there are. Nothing is guessed from an unknown tag:
a file half of whose lines are silently skipped is a file whose record count is wrong and
whose author has no way to find out.

## 4. Fields

Fields are separated by one or more spaces or tabs. Leading and trailing whitespace on a line
is permitted and means nothing. **The number of fields is fixed by the form** — a record with
too few or too many is an error saying how many that form takes and how many were written,
rather than being read as far as it goes.

Four kinds of field appear.

### 4.1 Ids

Written exactly as [`SPEC.md` 4.1](../SPEC.md#41-identifiers) writes an id: `namespace:local`,
both parts non-empty, the namespace ASCII and beginning with a letter, compared byte for byte
with no case folding and no trimming. The validator checks the shape of every id field and
checks that its namespace is one the registry declares; what the local part names is the
minting authority's business and means nothing here
([0003](./decisions/0003-id-namespaces-are-a-closed-registry.md)).

### 4.2 Timestamps

An RFC 3339 `date-time` **with a numeric offset**:

```
2026-05-06T09:14:22Z
2026-05-06T11:14:22+02:00
2026-05-06T09:14:22.480Z
```

Fractional seconds are permitted and carry whatever resolution was written.

**An ambiguous timestamp is an error**, and there are exactly two shapes of it:

| Written | Why it is refused |
|---------|-------------------|
| `2026-05-06T09:14:22` | No offset. It is a local time in a zone the file does not name, so it denotes a different instant depending on where it is read, and two records written this way cannot be ordered. |
| `2026-05-06T09:14:22-00:00` | RFC 3339 section 4.3 defines `-00:00` as *the offset is unknown*. A record whose instant is unknown is not evidence of when anything was measured. |

`Z` is `+00:00` and is unambiguous. A zone abbreviation — `CEST`, `EST` — is not RFC 3339 and
is a malformed timestamp, not an offset: those abbreviations are not unique across the world
and several of them denote two different offsets in one year.

The engine stores the instant *and* the text it was written in, so a record printed back
still says what its author wrote about which offset they were working in.

### 4.3 Real numbers

The number lexis of [`SPEC.md` 4.3](../SPEC.md#43-numbers), under the same rule: a real is
always written with a fraction or an exponent, so that it reads back as a real rather than an
integer. `100` is not a legal coordinate; `100.0` is, and so is `1.0e2`. `1.` is not a number.

**No unit token appears on an observation line.** Every length on the line — the three
coordinate components, both precisions and the antenna height — is in the linear unit of the
frame the record names, which is the one unit that frame has
([0005](./decisions/0005-one-linear-unit-per-frame.md)). Repeating it on every line of a file
of ten thousand records is ten thousand chances for one of them to disagree with the frame,
and the disagreement would have to be resolved somehow.

### 4.4 Quoted text

A double-quoted string, with `\"` and `\\` as its only escapes and no line break inside it.
**Exactly one field takes one**, and it is the only field which may: the `reason` of a
`retire` record. A quoted id is an error and so is an unquoted reason, because one spelling
per field is what stops two authors writing the same id two ways in a format nothing
re-prints. A quoted string that is never closed, or that carries an escape the format does
not know, is an error at the offending byte.

## 5. `obs` — one observation

```
obs <id> <at> <frame> <x> <y> <z> <method> <fix> <h-precision> <v-precision> <antenna> <session>
```

Twelve fields, in that order, all required.

| # | Field | Kind | Meaning |
|---|-------|------|---------|
| 1 | `id` | id | This record's identity. It is what a claim's provenance points at and what a `retire` record names, so it is minted once and never reused, including for a record that was retired. |
| 2 | `at` | timestamp | The instant the observation was taken, by the instrument's clock. Not the instant it was written to the file and not the day it was processed. |
| 3 | `frame` | id | The frame the coordinates are expressed in, which must be one the registry declares ([`SPEC.md` 7.5](../SPEC.md#75-frame)). It carries the unit every length on the line is in. |
| 4–6 | `x` `y` `z` | real | The coordinate, component by component, in the frame's axis order and in the frame's linear unit. Three components always: a frame is three-dimensional even where the work is not, and a record with a component missing is a record whose axes cannot be told apart. |
| 7 | `method` | id | How the value was obtained — the same vocabulary a claim's `method` names. Which methods exist is registry data ([0010](./decisions/0010-the-engine-carries-no-domain-vocabulary.md)); the engine checks the id and its namespace and attaches no meaning to it. |
| 8 | `fix` | id | The fix quality the instrument reported at the moment of the shot: what solution it had, not how good the operator thought it was. Registry data for the same reason `method` is. Conventionally `fix:rtk-fixed`, `fix:rtk-float`, `fix:dgps`, `fix:autonomous`, `fix:static`, `fix:total-station`, `fix:levelled` — a convention this document records and does not impose. |
| 9 | `h-precision` | real, ≥ 0 | The **horizontal** standard uncertainty of this shot, in the frame's unit: one standard deviation, k = 1, in the plane of the first two components. It is the instrument's reported figure for this shot, not a specification for the instrument in general. |
| 10 | `v-precision` | real, ≥ 0 | The **vertical** standard uncertainty of this shot, in the frame's unit, on the same convention, along the third component. It is stated separately because it is routinely two to three times the horizontal figure and a single number would have to be the worse of them. |
| 11 | `antenna` | real, ≥ 0 | The antenna or instrument height that was used to reduce the shot: the vertical offset from the mark to the phase centre or to the prism. **The coordinate is of the mark, with this height already reduced out.** It is recorded so that the reduction can be checked, and undone, by somebody who was not there. |
| 12 | `session` | id | The occupation this record belongs to: one setup, one base station, one operator's afternoon. It is what makes a systematic error attributable — a base station on the wrong mark ruins a session, and the session is how its records are found. |

Both precisions are standard uncertainties because every accuracy in this system is
([0006](./decisions/0006-accuracy-is-one-sigma.md)). A figure the instrument reports at any
other coverage — a 95% figure, a manufacturer's "typical" — is converted to 1σ by whoever
records it, and the conversion belongs in the provenance of whatever claim cites the record.
There is no second convention and nothing here guesses which one a number was written under.

A precision of `0.0` is legal and says the shot has no uncertainty, which no instrument
reports; it is not a way of saying the figure is unknown. There is no spelling for an unknown
precision, deliberately — a record whose precision nobody knows cannot be combined with
another, and writing a zero for it produces a budget that is confidently wrong in the
optimistic direction.

## 6. `retire` — retiring an observation

```
retire <id> <at> <supersedes> "<reason>"
```

Four fields, in that order, all required.

| # | Field | Kind | Meaning |
|---|-------|------|---------|
| 1 | `id` | id | This record's own identity, minted like any other. A retirement is a record in the log, not an annotation on one. |
| 2 | `at` | timestamp | When the decision to retire was taken. Not when the retired shot was measured. |
| 3 | `supersedes` | id | The identity of the record being retired, which must appear **earlier in the log**. |
| 4 | `reason` | quoted text | Why, in the words of whoever decided. Non-empty: a retirement with no reason is not evidence of anything, and the next person to read the file has no way to tell a mistake from a change of mind. |

A retirement removes nothing. The retired record stays exactly where it was written, byte for
byte, and both are read: what the log resolves to is every observation no retirement names.
A corrected value is simply a later `obs` record; nothing links it to the one it replaces
beyond the reason text, because a link would be a second claim about which record is right
and there is already exactly one — the retirement.

## 7. What the validator checks

Every check below produces a [diagnostic](../SPEC.md#5-notation), not an exception: **one
pass reports every independent problem it finds**, with a file, a line and a column, and
carries on past each one. A file with eleven malformed lines is eleven diagnostics on one
run, not eleven runs.

Diagnostics are ordered by file, then by position, so the output of two runs over the same
input diffs against itself.

### 7.1 Per line

| Case | Reported as |
|------|-------------|
| A byte that begins no valid UTF-8 encoding | error at that byte |
| A byte order mark | error at the first byte of the file |
| A carriage return before the line feed | error at the carriage return |
| A final line with no line feed after it | error at the end of the file |
| A form tag nothing declares | error naming the tags there are |
| Too few or too many fields | error saying how many the form takes and how many were written |
| An id that is not one | error naming which part of the production it failed |
| A timestamp that is not RFC 3339 | error at the field |
| A timestamp with no offset, or with `-00:00` | error saying it is ambiguous and why |
| A number that is not one, or an integer where a real belongs | error at the field |
| A negative precision or antenna height | error naming the value |
| An unterminated quoted string, or an unknown escape in one | error at the offending byte |
| A quoted string in a field which is not `reason`, or an unquoted `reason` | error at the field |
| An empty `reason` | error at the field |

### 7.2 Across the log

| Case | Reported as |
|------|-------------|
| Two records sharing one identity | error naming **both** lines: the second is the diagnostic's position and the first is a related location beneath it |
| A `retire` naming a record the log does not hold | error naming the identity that did not resolve |
| A `retire` naming itself | error |
| A `retire` naming a record written later in the log | error naming both lines — a retirement is a *later* record, and one that is not cannot have been a decision about what it retires |
| A record retired twice | error naming both retirements |
| A `frame` no registry file declares | error naming the frames there are |
| An id whose namespace no registry file declares | error naming the namespaces there are |
| A `retire` timestamped before the record it retires | **warning**, not an error: instrument clocks disagree, and a warning says so without refusing data that is otherwise sound |

Ordering across files is the walk's order — lexical by path — so "later in the log" is a
question with one answer however many files the log is spread over.

## 8. Append-only

The invariant is one sentence: **between two revisions of an observation file, every line the
older revision had is present, unchanged, at the same line number in the newer one.**

Checking it needs both revisions, so it is a check about a pair of files rather than about a
file, and it is the one check in this format that a single file cannot fail on its own.

| Finding | Reported as |
|---------|-------------|
| Line *n* differs between the revisions | error at line *n* of the newer revision, with line *n* of the older one as a related location |
| The newer revision has fewer lines | error saying how many lines were removed, with the first removed line as a related location |
| The newer revision has lines the older one did not | not a finding. That is an append, which is the only legal change. |
| The file is absent from the newer revision | every line removed — the caller passes empty bytes for a file that is gone |

Comparison is byte for byte over whole lines, including comment and blank lines. Re-indenting
a record, re-spelling a number that means the same, correcting a spelling mistake in a comment
and rewriting the whole file with a different offset in every timestamp are all the same
finding, because none of them is distinguishable from a quiet correction of the data and the
format's entire purpose is that they are refused rather than judged.

The check is a comparison of two byte sequences and knows nothing about git. Where those
sequences come from — two commits, a working tree against its merge base, a backup against
what is on disk — is the caller's question, and the model's revision plumbing already answers
it for entity files.

## 9. Worked example

`observations/2026-05-06-site-control.obs`:

```
# id at frame x y z method fix h-prec v-prec antenna session
obs shot:2026-05-06-0001 2026-05-06T09:14:22Z frame:site 412300.120 5318220.455 34.210 method:gnss-rtk fix:rtk-fixed 0.012 0.021 2.000 session:2026-05-06-am
obs shot:2026-05-06-0002 2026-05-06T09:16:03Z frame:site 412318.905 5318220.512 34.198 method:gnss-rtk fix:rtk-fixed 0.011 0.019 2.000 session:2026-05-06-am
obs shot:2026-05-06-0003 2026-05-06T09:18:47Z frame:site 412318.880 5318241.330 34.402 method:gnss-rtk fix:rtk-float 0.240 0.510 2.000 session:2026-05-06-am
obs shot:2026-05-06-0004 2026-05-06T09:21:10Z frame:site 412318.902 5318241.318 34.395 method:gnss-rtk fix:rtk-fixed 0.013 0.022 2.000 session:2026-05-06-am
retire retire:2026-05-06-0001 2026-05-06T16:02:00Z shot:2026-05-06-0003 "float solution beside a fixed reshot of the same corner"
```

Four shots and one retirement. What the log resolves to is shots 1, 2 and 4; shot 3 is still
in the file, still readable, and still says exactly what the instrument said it said.

The afternoon's work appends to the same file:

```
obs shot:2026-05-06-0005 2026-05-06T13:44:51Z frame:site 412300.108 5318241.344 34.407 method:gnss-rtk fix:rtk-fixed 0.014 0.023 2.000 session:2026-05-06-pm
```

— and the append-only check between the two revisions has nothing to say, because the first
six lines are byte-identical and the seventh is new. Had the afternoon instead corrected
`0.240` to `0.012` on line 4, the check would report line 4 as modified and name both
revisions of it, which is the finding the whole format exists to produce.

## 10. Not in this version

Named so that their absence reads as a decision rather than an oversight.

- **Raw instrument data.** A file of this format holds reduced coordinates and the conditions
  they were reduced under, not observables. Carrier phase, angle sets and level runs are the
  instrument's own formats, are archived as they came off the instrument, and are cited by a
  record's `method` and `session` rather than restated here.
- **Attaching an observation to an entity.** A record is not written on a node and names none;
  what cites a record is a claim's provenance, in the entity format, which is the one place
  the model says what it believes and why.
- **Derived quantities.** No mean, no residual, no adjusted coordinate, no session summary. A
  derivation is computed from the records and never written back beside them
  ([0009](./decisions/0009-derived-values-are-never-written-back.md)).
- **A canonical printing.** There is none, and there cannot be: canonical form is produced by
  rewriting a file, and rewriting a file is what this format forbids. A record is written once,
  in whatever legal spelling its author used, and stays that way.
- **Editing or deleting a record.** Not deferred — excluded. It is the one thing
  [section 1](#1-why-it-is-separate) is about.
