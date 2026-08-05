# The dfcad entity format

**Specification version 1.1.**

This document defines the text format a dfcad model is written in: which tagged forms are
legal, where each may appear, with what arity and ordering, and what the one canonical
printing of each looks like.

It is written before the loader, and the loader is implemented against it. Where the two
disagree, this document is right and the loader has a bug.

## Contents

- [1. Scope](#1-scope)
- [2. What this specification delegates](#2-what-this-specification-delegates)
- [3. Files](#3-files)
- [4. Lexical additions](#4-lexical-additions)
- [5. Notation](#5-notation)
- [6. Entity forms](#6-entity-forms)
- [7. Registry forms](#7-registry-forms)
- [8. Canonical form](#8-canonical-form)
- [9. Worked example](#9-worked-example)
- [10. Versioning of this specification](#10-versioning-of-this-specification)
- [11. Not in this version](#11-not-in-this-version)
- [12. Reviewed against the decision records](#12-reviewed-against-the-decision-records)

## 1. Scope

A dfcad model is a set of text files under version control. Every one of them is an
S-expression source. This specification covers two layers of what those files may contain:

- **Entity forms** — the nodes of the graph: semantic nodes, geometric nodes, the claims
  attached to them, the references between them, and the assertions that constrain them.
- **Registry forms** — the vocabulary a consuming repository declares: its types, claim
  predicates, frames, id namespaces and tolerances, plus the one project declaration the
  `GlobalId` derivation reads.

It does not cover the generic S-expression grammar underneath, which is
[section 2](#2-what-this-specification-delegates). It does not cover the observation file
format, the CLI's machine output contract, or any exported representation; those have their
own specifications.

Two vocabularies are compiled into the engine and are therefore fixed by this document
rather than by registry data:

| Set        | Members                                                              |
|------------|----------------------------------------------------------------------|
| `kind`     | `Zone`, `Site`, `Building`, `Storey`, `Space`, `Element`, `Interface` |
| `geometry` | `point`, `line`, `area`, `surface`, `solid`, or absent               |

Everything else nameable in a model — every type, predicate, frame, id namespace and
tolerance — is registry data supplied by the consuming repository, per
[0010](./docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md).

## 2. What this specification delegates

The generic S-expression layer is [`z5labs/sexpr-go`](https://github.com/z5labs/sexpr-go).
This specification is the layer above it and does not restate it. Precisely these concerns
are delegated:

| Concern                        | Delegated behaviour                                                                                        |
|--------------------------------|------------------------------------------------------------------------------------------------------------|
| Tokenising and parsing         | `sexpr.Parse` produces the tree this document assigns meaning to.                                            |
| Atom lexis                     | What is a symbol, a number, a string, a boolean or `nil`, and where one ends.                                |
| Symbol characters              | Letters (including non-ASCII), digits, and the punctuation `+-*/<>=!?:$%_&~^@.`.                              |
| Number lexis                   | Optional sign, ASCII digits, optional fraction, optional decimal exponent. `1.` is not a number.              |
| Malformed-number rule          | A lexeme that begins like a number and is not one — `12abc`, `--1`, `2026-03-14` — is a lexical error, not a symbol. |
| String escapes                 | `\"` `\\` `\n` `\r` `\t` `\b` `\f` and `\uXXXX`. Any other escape is an error.                                |
| Comment syntax                 | `;` to end of line, and `#\| … \|#`, which spans lines and nests.                                            |
| Comment attachment             | A comment belongs to the file or the list that encloses it.                                                  |
| Nesting bound                  | `sexpr.MaxDepth` (10 000) — deeper input is an error rather than a stack exhaustion.                          |
| Positions                      | Every token carries a line and a column, which is what every diagnostic in this system is built on.            |
| Base layout                    | Line breaking and indentation for a list too wide to fit — see [8.1](#81-layout-is-inherited).                 |

Several parts of the generic grammar are legal S-expressions and are **not** legal in a
dfcad file. Each is a load error naming the construct and its position:

- **Improper lists.** A dotted pair — `(a b . c)` — never appears. Every list in a dfcad
  file is proper.
- **Quote shorthands.** `'x`, `` `x ``, `,x` and `,@x` have no meaning here.
- **`nil`.** Absence is expressed by omitting a child, never by writing a placeholder.
- **The empty list `()`.** Every form has a tag.
- **Booleans outside the positions that take one.** `#t` and `#f` appear only where a form's
  table below says so.

Rejecting these keeps one printing per model. A construct with no meaning that nevertheless
parses is a construct two authors will spell differently, and there is nothing for the
canonical printer to normalise it to.

## 3. Files

### 3.1 Encoding

A file is UTF-8. A file that is not valid UTF-8 is a load error, reported at the first
offending byte offset rather than as a whole-file rejection.

A byte order mark is a load error. UTF-8 needs no mark, a mark is invisible in every editor
that would have to remove it, and accepting one means every position in the file is off by
three bytes for anything reading it outside the loader.

### 3.2 Extension and layout

Entity forms and registry forms live in files with the extension **`.dfc`**. Any other
extension is not read by a directory walk. A path named explicitly on the command line is
read whatever its extension.

One extension covers both layers. A form's tag already says which layer it belongs to, so a
second extension would encode the same fact twice, and a file that is in the wrong one is a
failure with no diagnostic. Loading is two passes over the same set of files: every registry
form resolves before any entity form is interpreted.

This specification does not constrain directory layout or which forms share a file. Keeping
registry forms in their own files under a `registry/` directory is a convention worth
following and is not a rule.

### 3.3 Line endings and trailing newline

Line terminators are whitespace and carry no meaning. Input may use LF or CRLF; a lone CR
is whitespace but is not a line terminator, so a file using it reports every position on
line 1, which is a symptom rather than a rule.

Canonical output uses LF only, and ends with exactly one LF. A file that ends without one
parses identically and is not canonical; `fmt` adds it.

### 3.4 The format is not line-sensitive

No rule in this specification depends on where a line break falls. Two forms on one line and
one form across twenty lines mean the same thing. Line structure exists for the reader and
for the diff, and it is settled entirely by [section 8](#8-canonical-form).

The two exceptions are inherited from the underlying grammar: a `;` comment is terminated by
a line break, and a string literal may contain a literal line break. Canonical output
contains neither of the awkward cases — the printer escapes a line break inside a string, so
canonical form has no raw line terminator inside a string literal.

### 3.5 Empty files

A file containing no forms is legal and contributes nothing. A tree containing no `.dfc`
files loads and yields an empty graph. Neither is a warning.

## 4. Lexical additions

Everything in this section is a constraint on top of the delegated atom lexis. Each is
checked after tokenising succeeds, so a violation is a diagnostic with a span rather than a
lexical error.

### 4.1 Identifiers

An **id** identifies a node or a claim. Ids are the only thing anything references
([0002](./docs/decisions/0002-immutable-id-mutable-label.md)).

```
id        = namespace ":" local
namespace = ALPHA *( ALPHA / DIGIT / "-" / "_" )   ; ASCII only
local     = 1*symbol-char                          ; the delegated symbol characters
```

- The split is on the **first** colon. A local part may contain further colons; the
  namespace never does.
- Both parts are non-empty, and the colon is required. `:x`, `survey:` and an unqualified
  `corner` are all load errors.
- The namespace must be declared in the id namespace registry
  ([0003](./docs/decisions/0003-id-namespaces-are-a-closed-registry.md)). An unregistered
  namespace is a load error naming the namespace and listing the registered set.
- **The namespace is ASCII and must begin with a letter.** This is not stylistic. A lexeme
  beginning with a digit is a number attempt to the tokenizer, so `1042:corner` is a lexical
  error before any of this specification is consulted; restricting the namespace makes every
  well-formed id a well-formed symbol. Confining it to ASCII keeps two namespaces that render
  identically from being different ids.
- **Ids compare byte-wise.** No case folding, no Unicode normalisation, no trimming. Two ids
  are the same id when their bytes are the same.
- The engine attaches no meaning to a namespace beyond its being registered. Nothing infers
  a `kind`, a `type`, an accuracy or a rendering from an id prefix.

A **label** is not an id. It is display text, it is optional, it carries no uniqueness
guarantee, and nothing resolves through it.

### 4.2 Registry names

Type names, predicate names, tolerance names and check names are plain symbols — no
namespace, no colon required. They are declared once in the registry of their layer, unique
within it, and compared byte-wise.

They are not ids because they name vocabulary rather than things. A registry belongs to one
consuming repository, so there is no second authority to collide with, and a namespace on
every predicate would be noise on every claim in the model.

A **predicate name may not be one of the structural child tags** used by a form that also
carries claims — `node`, `vertex`, `edge`, `loop` and `frame` — because the two occupy the
same position. The reserved set is exactly:

```
label  kind  type  geometry  frame  within  member-of  boundary  retired  assert
vertices  edges  backed-by  unit  parent  transform
```

Declaring a predicate with one of these names is a registry error naming the collision.

### 4.3 Numbers

Numbers are written in the delegated number lexis. This specification adds one distinction:

- **Real numbers** — every claim value component, every accuracy magnitude, every tolerance
  value, every transform component — are always written with a fraction or an exponent, so
  that they read back as a real rather than an integer. `100` is not a legal coordinate;
  `100.0` is.
- **Counts** — the `dimension` of a coordinate predicate is the only one — are written with
  neither, so they read back as an integer.

**Trailing zeros carry no meaning anywhere in this system.** `8.50` and `8.5` are the same
value, and the canonical printer emits the shorter. A claim that needs to say "measured to
the hundredth" says so in its accuracy, explicitly, where a query can read it — see
[8.4](#84-numbers).

### 4.4 Dates

A date is written as a **string** in RFC 3339 full-date form:

```
(date "2026-03-14")
```

Four-digit year, two-digit month, two-digit day, proleptic Gregorian, hyphen separated. No
time, no zone, no other spelling.

It is a string rather than a bare symbol because it has to be. `2026-03-14` begins like a
number, so the tokenizer classifies it as a malformed number and fails before this
specification is reached. Quoting it is the only spelling that survives the delegated lexis.

### 4.5 Units

A unit is a plain symbol — `m`, `ft`, `usft`. **The set of unit names, and the definition of
each, is fixed by the engine**, and this document does not enumerate it: a unit is
arithmetic, not domain vocabulary, and a model that could declare its own would be a model in
which `ft` means whatever the last registry said. There is no unit registry.

Two definitions are pinned by
[0005](./docs/decisions/0005-one-linear-unit-per-frame.md) and are load-bearing:

| Unit   | Definition                                |
|--------|-------------------------------------------|
| `ft`   | `1 ft = 0.3048 m`, exactly                |
| `usft` | `1 usft = 1200/3937 m`, exactly           |

`usft` is never a synonym for `ft`, in any position, under any registry.

Where a unit token is required and where it is omitted is stated per form. The general rule
is [0005](./docs/decisions/0005-one-linear-unit-per-frame.md)'s: a dimensional value without
a unit is a load error, and the loader converts nothing.

### 4.6 Booleans

`#t` and `#f`, in the two registry positions that take one. `#true` and `#false` parse and
are not canonical; the printer emits the short spelling.

## 5. Notation

Each form below is given as a skeleton and a table of children.

- `<name>` is a placeholder for a value of the stated sort.
- The **table row order is the canonical child order.** A printer emits present children in
  the order the table lists them.
- **Arity** is one of `1` (required, exactly one), `0..1` (optional, at most one), `1..n`
  (required, repeatable) or `0..n` (optional, repeatable).
- A child not listed in a form's table is a load error naming both the child and the form it
  appeared in. Repeating a child whose arity is `1` or `0..1` is a load error naming both
  occurrences.

Every form's first element after the tag, where it has one, is positional. Everything else
is a child form.

## 6. Entity forms

The top-level forms of a file are exactly these ten tags:

```
project  namespace  type  predicate  tolerance  frame      ; registry, section 7
node  vertex  edge  loop                                   ; entities, this section
```

They are listed here in the order [8.3](#83-ordering) prints them in.

Anything else at the top level is a load error naming the tag, its position, and the nearest
known tag when one is close.

### 6.1 `node` — a semantic node

```
(node <id>
  (label "<text>")
  (kind <Kind>)
  (type <type-name>)
  (geometry <geometry>)
  (frame <frame-id>)
  (within <node-id>)
  (member-of <zone-id>)
  (boundary <loop-id>)
  (retired …)
  <claim> …
  (assert …) …)
```

| Child       | Arity  | Contents                                                                          |
|-------------|--------|-----------------------------------------------------------------------------------|
| `label`     | `0..1` | A string. Display text; changing it changes nothing else.                          |
| `kind`      | `1`    | One of the seven members of `kind`.                                                |
| `type`      | `1`    | A type name declared in the type registry.                                         |
| `geometry`  | `0..1` | One of `point`, `line`, `area`, `surface`, `solid`. Omitted means the node has no geometry, which is a distinct and ordinary state. |
| `frame`     | `0..1` | A frame id. A node declared in two frames is unrepresentable, which is the point.  |
| `within`    | `0..1` | The id of the node that strictly contains this one.                                |
| `member-of` | `0..n` | A zone node id. Membership is many-to-many and never implies containment.          |
| `boundary`  | `0..n` | A loop id. A semantic node references a loop; it never carries coordinates.        |
| `retired`   | `0..1` | See [6.7](#67-retired).                                                            |
| *claim*     | `0..n` | A claim form, tagged with a registered predicate. See [6.5](#65-claims).            |
| `assert`    | `0..n` | See [6.8](#68-assert).                                                             |

The semantic family has one tag and carries its `kind` as a value, while the geometric
family has a tag per member. That asymmetry is deliberate: every semantic node has the same
shape whatever its kind, so the kind is data; a vertex, an edge and a loop have genuinely
different shapes, so the tag is what selects the shape.

A `node` never carries `vertices`, `edges` or `backed-by`. A geometric node never carries
`kind` or `type`; both are load errors naming the node, not silently ignored fields
([0001](./docs/decisions/0001-two-node-families.md)).

### 6.2 `vertex`

```
(vertex <id>
  (label "<text>")
  (frame <frame-id>)
  <claim> …
  (assert …) …)
```

| Child    | Arity  | Contents                                              |
|----------|--------|-------------------------------------------------------|
| `label`  | `0..1` | A string.                                             |
| `frame`  | `1`    | A frame id. A vertex is always in exactly one frame.  |
| *claim*  | `0..n` | Claims. A vertex's position is one of them.           |
| `assert` | `0..n` | See [6.8](#68-assert).                                |

**A vertex's position is a claim like any other**, with the same predicate validation,
resolution and accuracy rules. Two surveys of the same corner are two claims on one vertex,
and the disagreement between them is exactly what the conflict register is for.

### 6.3 `edge`

```
(edge <id>
  (label "<text>")
  (frame <frame-id>)
  (vertices <vertex-id> <vertex-id>)
  (backed-by <element-id>)
  <claim> …
  (assert …) …)
```

| Child       | Arity  | Contents                                                                     |
|-------------|--------|------------------------------------------------------------------------------|
| `label`     | `0..1` | A string.                                                                    |
| `frame`     | `1`    | A frame id.                                                                  |
| `vertices`  | `1`    | Exactly two vertex ids, **ordered**: start then end.                          |
| `backed-by` | `0..n` | The id of a semantic node of kind `Element` that physically realises the edge. |
| *claim*     | `0..n` | Claims.                                                                      |
| `assert`    | `0..n` | See [6.8](#68-assert).                                                       |

- The two ids in `vertices` must name `vertex` nodes; anything else is a load error. The two
  must differ; a self-loop is a load error.
- The order of the pair is significant and is never sorted.
- `backed-by` is unordered and repeatable. An edge with at least one resolving `backed-by` is
  a physical boundary; one with none is virtual. **That classification is computed, never
  stored** — there is no flag for it in this format, and adding a backing element flips the
  answer with no other edit
  ([0009](./docs/decisions/0009-derived-values-are-never-written-back.md)).
- A `backed-by` that does not resolve is a load error, not a quiet reclassification to
  virtual.

**Non-straight edges.** This form says nothing about the shape of an edge between its two
vertices, and it does not need to. Curvature arrives as a claim under a predicate the
consuming repository registers — an arc centre, a bulge, a radius — with its own source,
method and accuracy like any other measurement. **The predicate registry is the extension
point, and adding an arc is registry data rather than a change to this specification.**

### 6.4 `loop`

```
(loop <id>
  (label "<text>")
  (frame <frame-id>)
  (edges <edge-id> …)
  <claim> …
  (assert …) …)
```

| Child    | Arity  | Contents                                    |
|----------|--------|---------------------------------------------|
| `label`  | `0..1` | A string.                                   |
| `frame`  | `1`    | A frame id.                                 |
| `edges`  | `1`    | One or more edge ids, **ordered**.          |
| *claim*  | `0..n` | Claims.                                     |
| `assert` | `0..n` | See [6.8](#68-assert).                      |

The order of `edges` is significant, is preserved exactly as authored, and is never sorted.
It is the order in which the loop is traversed.

Whether the edges form a closed, connected, simple cycle is a **load check**, not a
syntactic one, and it is performed against a named tolerance from the tolerance registry.
Syntactically, any non-empty ordered list of edge ids is a well-formed `loop`.

### 6.5 Claims

**A claim's tag is its predicate.** There is no `claim` keyword, and there is no other way to
attach a value to a node.

```
(<predicate>
  (id <claim-id>)
  (value <value>)
  (source "<text>")
  (method <method-id>)
  (accuracy <term> …)
  (date "<YYYY-MM-DD>")
  (rank deprecated)
  (superseded-by <claim-id>))
```

| Child           | Arity  | Contents                                                                       |
|-----------------|--------|--------------------------------------------------------------------------------|
| `id`            | `0..1` | The claim's own id. Required when anything references the claim, optional otherwise; a referenced claim with no id is a load error. |
| `value`         | `1`    | See [6.6](#66-value-shapes).                                                    |
| `source`        | `1`    | A string naming the evidence — a report, a drawing, a person, an instrument log. |
| `method`        | `1`    | An id naming how the value was obtained.                                        |
| `accuracy`      | `0..1` | Optional; at most one. When present it holds one or more terms — see [6.6.5](#665-accuracy-terms). |
| `date`          | `1`    | The date the value was obtained, per [4.4](#44-dates).                           |
| `rank`          | `0..1` | `normal` or `deprecated`. Omitted means `normal`, and canonical form omits it — see [8.2](#82-defaults-are-omitted). |
| `superseded-by` | `0..1` | A claim id. Required when `rank` is `deprecated`, forbidden otherwise.           |

Rules:

- **The predicate must be declared in the predicate registry**, and must be declared
  claim-bearing. An unregistered predicate is a load error naming the predicate and its
  position.
- **A bare scalar where a claim belongs is a load error.** `(width 8.5)` for a claim-bearing
  `width` does not load, is not a warning, and no flag, environment variable or configuration
  downgrades it. The diagnostic names the predicate, its position, and shows the minimal
  claim form that would be accepted
  ([0008](./docs/decisions/0008-a-bare-scalar-is-a-load-error.md)).
- **A predicate the registry declares non-claim-bearing takes a plain value** and no children.
  The plain value is written exactly as the same predicate's `value` child would be written,
  unit token included, and nothing else follows it: `(colour "slate")`,
  `(nominal-width 8.5 m)`. Writing a claim form for a non-claim-bearing predicate, or a plain
  value for a claim-bearing one, is a load error in each direction, naming the predicate and
  what the registry says about it.
- **Repeating a predicate on one node is the normal case**, not an error. Two `width` claims
  on one node are two measurements, and the disagreement between them is the most valuable
  thing in the file.
- `method` is an id so that its namespace is registered like any other, which is what keeps
  `total-station`, `TotalStation` and `ts` from being three methods. The engine attaches no
  meaning to it beyond the namespace being registered — an estimate is a method, and so is
  an assumption.
- `source` is a string rather than an id because it cites a document, and a citation is prose
  the moment it has to be recognisable to the person checking it.
- **`rank` is closed at `normal` and `deprecated`.** There is no `preferred`, no numeric
  priority, no weight and no override. A claim cannot be promoted above its peers; where two
  normal claims disagree, the disagreement stays in the conflict register until one is
  deprecated or corrected ([0007](./docs/decisions/0007-rank-is-closed.md)).
- **Deprecation requires a replacement.** `(rank deprecated)` without `superseded-by` is a
  load error, which is what keeps `deprecated` from becoming a delete button. A
  `superseded-by` naming a claim that does not exist, a claim superseding itself, and a cycle
  in a supersession chain are each a load error naming both ends.
- A claim with no `accuracy` loads and is **unrankable**: it can never win resolution, and it
  is not given a default. It is still returned as a candidate when nothing rankable exists.

### 6.6 Value shapes

The predicate registry declares which of four shapes a predicate's `value` takes. A value of
the wrong shape is a load error naming the predicate, the declared shape and what was found.

#### 6.6.1 `scalar`

```
(value 8.5 ft)
```

A real number followed by a unit. The unit must be a unit of the same quantity as the one the
predicate declares; a unit of a different quantity is a load error rather than a silent
conversion.

A predicate the registry declares with no unit is non-dimensional, and its scalar value is
written with no unit token. There is no `unitless` token — a non-dimensional quantity that
had to name its non-dimensionality would be two spellings of one thing.

#### 6.6.2 `coordinate`

```
(value (401235.117 3172884.902 44.318) m)
```

An ordered list of two or three real numbers followed by a unit. The count is fixed by the
predicate's declared `dimension`, and a value of the wrong length is a load error. **The order
of the components is significant and is never sorted.**

**A coordinate's unit must be exactly the declaring node's frame's linear unit.** Not a
convertible one — the same one. This is the rule that makes a mixed-unit frame a diagnostic
with a position rather than a number off by a factor of a thousand that looks plausible
([0005](./docs/decisions/0005-one-linear-unit-per-frame.md)).

#### 6.6.3 `transform`

```
(value
  (transform
    (translation <tx> <ty> <tz>)
    (rotation <r00> <r01> <r02> <r10> <r11> <r12> <r20> <r21> <r22>)
    (scale <s>)))
```

| Child         | Arity | Contents                                                        |
|---------------|-------|-----------------------------------------------------------------|
| `translation` | `1`   | Exactly three real numbers, ordered `tx ty tz`.                 |
| `rotation`    | `1`   | Exactly nine real numbers, a 3×3 matrix in **row-major** order. |
| `scale`       | `1`   | One real number.                                                |

The three children are in fixed order, and the numbers within each are ordered. None of them
is sorted.

The transform maps a point `p` in the frame that declares it to a point in that frame's
parent:

```
p_parent = t + s · R · convert(p, child unit → parent unit)
```

- `convert` applies the pinned unit definitions in [4.5](#45-units) and is the only
  conversion in the expression. **`scale` is a scale, not a unit conversion** — a transform
  between frames of different units has `scale` `1.0` unless the fit genuinely found a scale
  error.
- `translation` is expressed in the **parent** frame's linear unit and carries no unit token,
  because both frames declare theirs and a transform value appears in exactly one place.
- The whole value carries no unit token for the same reason: `rotation` and `scale` are
  dimensionless and `translation` is pinned to the parent.

#### 6.6.4 `text`

```
(value "slate")
```

A string, and nothing else. A `text` predicate is non-dimensional by construction: declaring
a `unit` for one is a registry error.

`text` exists so that a fact which is genuinely a word rather than a measurement — a colour,
a catalogue reference, a serial number — has somewhere to live that is not a `label`. A label
is display text with no provenance; a `text` claim is a statement with a source, a method and
a date like any other.

#### 6.6.5 Accuracy terms

```
(accuracy (independent 0.005 m))
(accuracy (independent 0.012 m) (systematic 0.008 m survey:CP-3))
```

| Term          | Positional arguments                | Meaning                                          |
|---------------|-------------------------------------|--------------------------------------------------|
| `independent` | magnitude, unit                     | An error that does not correlate with any other. |
| `systematic`  | magnitude, unit, **term id**        | An error shared with everything else naming the same term id. |

- An `accuracy` carries one or more terms and is unordered; the canonical printer sorts them.
- **Every magnitude is a standard uncertainty: one standard deviation, k = 1.** There is no
  other storage convention, and a figure quoted at any other coverage is converted at the
  point it enters the model, by whoever enters it
  ([0006](./docs/decisions/0006-accuracy-is-one-sigma.md)).
- Each term's unit must be a unit of the same quantity as the claim's value.
- **A systematic term names the source it is shared with, as an id.** Two systematic terms
  are the same term when their term ids are byte-equal — not when their magnitudes happen to
  match — and a term contributing to two inputs of one derivation is counted once. The
  namespace of a term id must be registered; the id need not name a node.
- Independent terms combine in quadrature, systematic terms add linearly, and no widened
  figure is ever stored. Any output presenting an uncertainty at other than 1σ states its
  coverage factor.

### 6.7 `retired`

```
(retired
  (date "<YYYY-MM-DD>")
  (reason "<text>")
  (superseded-by <node-id>))
```

| Child           | Arity  | Contents                                          |
|-----------------|--------|---------------------------------------------------|
| `date`          | `1`    | When the thing ceased to exist in the model.      |
| `reason`        | `1`    | A string. Why.                                    |
| `superseded-by` | `0..1` | The node that replaced it, where one did.         |

Retiring is not deleting. **The id stays in the graph, is never removed, and is never issued
again to a different thing** ([0002](./docs/decisions/0002-immutable-id-mutable-label.md)).
`superseded-by` is optional here — unlike on a claim — because a thing that stopped existing
did not necessarily get replaced by something. `reason` is required because a retirement with
no reason is a deletion wearing a hat.

### 6.8 `assert`

```
(assert <check-name>
  (<parameter> <value>) …)
```

| Element        | Arity  | Contents                                                        |
|----------------|--------|-----------------------------------------------------------------|
| *check name*   | `1`    | Positional. A check from the engine's closed check registry.     |
| *parameter*    | `0..n` | One child per named parameter, tagged with the parameter's name. |

- **There is no expression language.** No operators, no arithmetic, no boolean combinators,
  no traversal syntax, no user-defined logic. An assertion names a check and supplies its
  parameters, and nothing else
  ([0011](./docs/decisions/0011-assertions-are-named-parameterised-checks.md)).
- The check registry is closed and compiled in. An unknown check name is a load error naming
  the assertion, its position and the nearest registered name.
- Parameters are validated against the registry **at load**, not at run. A missing, extra or
  wrongly typed parameter is a load error naming the parameter.
- A parameter's value is a single datum: an id, a name symbol, a real number, a string, a
  boolean, or a parenthesised list of those. The check declares which.
- **A tolerance parameter takes a tolerance name from the registry.** No check anywhere
  contains a numeric literal tolerance, and writing one in an assertion is a load error
  ([0012](./docs/decisions/0012-tolerances-are-registry-data.md)).
- The form enclosing the assertion is its subject. Parameters may name other nodes by id, and
  every such id is checked to resolve at load, with a diagnostic naming both ends when one
  does not. A claim id resolves as well as a node's: ids are unique across the whole model.
- **A check declares what it applies to, and an assertion on a subject it cannot examine is a
  load error.** The axes are the form — node, vertex, edge or loop — the node kind and the
  geometry form; the kind and the geometry are the subject's own, so `boundary-loops-close`
  on a node with no shape is refused. A check with nothing on its subject to look at does not
  fail: it passes on every run forever, and the load is the only place the mistake is visible.
- **An assertion that restates a value the claims already carry is rejected at load.** An
  assertion constrains; it does not record. See [6.8.1](#681-restatement).

#### 6.8.1 Restatement

A claim is where a value is recorded, with the source, the method, the date and the accuracy
it came from. An assertion that repeats that value is a second source of truth for one
quantity — one that goes on saying what it says the day the claim is superseded, and that no
resolution rule reaches, because it is not a claim.

The rule reads the check's own declaration rather than the shape of what was written. A check
declares which parameter names the predicate and — where it has one — which parameter carries
a value *of that quantity*. An assertion writing both, on a subject that already claims that
predicate, is the claim written twice and is refused, whether or not the two values agree: a
value that disagrees is the same second source of truth, found earlier.

Caught, given a check declaring `(predicate …)` and a value parameter `(is …)`:

```
; Refused: the room already claims a width, and this says it again.
(node site:S-101
  (width (value 3.6 m) (source "Fit-out drawing FD-2026-011")
         (method method:drawing-take-off) (date "2026-03-02"))
  (assert claimed-value-is (predicate width) (is 3.6)))
```

Not caught, and deliberately:

| Written                                                          | Why it stands                                                     |
|------------------------------------------------------------------|-------------------------------------------------------------------|
| `(assert clearance-at-least (predicate width) (minimum 0.9))`     | A bound. The check declares `minimum` as a limit rather than as a value of the subject's, so 0.9 is not what the room measures. |
| `(assert required-claim (predicate width))`                       | No value at all. It says a claim must be there and does not say what it is. |
| `(assert claimed-value-is (predicate width) (is 3.6))` on a subject claiming nothing under `width` | There is no first statement for it to be the second of. |
| `(assert claimed-value-is (predicate clearance) (is 0.9))` on a subject claiming `width` | Two predicates are two quantities, and the rule is about one. |

### 6.9 References

Every reference in the format is an id, and every one of them must resolve. Dangling,
duplicate and cyclic references are reported distinctly.

| Reference                | Written as                | Must name                                    |
|--------------------------|---------------------------|----------------------------------------------|
| Containment              | `(within <id>)`           | A semantic node of a kind the hierarchy permits as a parent. One parent, no cycles. |
| Zone membership          | `(member-of <id>)`        | A semantic node of kind `Zone`.              |
| Region boundary          | `(boundary <id>)`         | A `loop`.                                    |
| Edge endpoints           | `(vertices <id> <id>)`    | Two distinct `vertex` nodes.                 |
| Loop traversal           | `(edges <id> …)`          | `edge` nodes.                                |
| Backing element          | `(backed-by <id>)`        | A semantic node of kind `Element`.           |
| Frame                    | `(frame <id>)`            | A `frame`.                                   |
| Frame parent             | `(parent <id>)`           | A `frame`. No cycles.                        |
| Frame transform          | `(transform <id>)`        | A claim whose value shape is `transform`.    |
| Claim supersession       | `(superseded-by <id>)`    | A claim, by claim id. No self-reference, no cycles. |
| Node supersession        | `(superseded-by <id>)`    | A node, inside `retired`.                    |
| Systematic term source   | third argument of `systematic` | Nothing — the id's namespace is checked, the id itself need not resolve. |

**An id is unique across the whole model.** A duplicate is a load error naming both
definitions with their positions. Claim ids share the same space as node ids, so a claim id
and a node id can never collide either.

#### 6.9.1 The containment hierarchy

Containment and zone membership are different relations and are never derived from one
another. `within` is physical enclosure and nests strictly — one parent, no overlap, and
following it reaches a node nothing contains. `member-of` is arbitrary grouping and is
many-to-many — a node is in any number of zones, a zone holds any number of nodes, and
neither says anything about where anything is. A wall belonging to two rooms and a circuit
belonging to no room are both ordinary rather than special cases.

Which kinds nest inside which is compiled into the engine, like the closed set of kinds
itself, and is this:

| Kind        | May be written within                              |
|-------------|-----------------------------------------------------|
| `Zone`      | Nothing. A zone groups by `member-of`, not by `within`. |
| `Site`      | Nothing. A site is a root of the hierarchy.         |
| `Building`  | `Site`                                              |
| `Storey`    | `Building`                                          |
| `Space`     | `Storey`, `Space`                                   |
| `Element`   | `Site`, `Building`, `Storey`, `Space`, `Element`     |
| `Interface` | `Site`, `Building`, `Storey`, `Space`               |

`Space` nests inside `Space` and `Element` inside `Element` because both genuinely do — an
alcove is a space inside a room, and a mullion is part of a curtain wall. Neither weakens
strict nesting, which is about a node having one parent rather than about the parent being
of a different kind.

A pairing the table does not list is a load error naming the kind written, the node it was
written inside, that node's kind, and where a node of the kind does belong. So is a `within`
naming the node it was written on, and so is a ring of nodes which contain one another,
which is named in full. A node with no parent and no zone is valid and is not reported.

Registry names are resolved by the same rule and are not in the table because they are not
ids: a `type`, a claim's predicate tag, a tolerance name in an assertion parameter, and a
check name in `assert` or `invariant` must each be declared in the registry of its layer, and
an undeclared one is a load error naming what was written and where.

## 7. Registry forms

Registry forms declare the vocabulary of one consuming repository. The engine validates their
structure, their uniqueness and their cross-references, and stops. **It attaches no meaning
to any entry beyond that** — it does not know that one type is a wall and another is a
parcel, and no behaviour anywhere depends on which
([0010](./docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).

### 7.1 `project`

```
(project
  (label "<text>")
  (globalid-namespace "<url>")
  (description "<text>"))
```

| Child                 | Arity  | Contents                                                      |
|-----------------------|--------|---------------------------------------------------------------|
| `label`               | `0..1` | A string.                                                     |
| `globalid-namespace`  | `1`    | A string holding a URL the project controls.                  |
| `description`         | `0..1` | A string.                                                     |

Exactly one `project` form exists across the whole model. Zero is a load error; two is a load
error naming both.

`globalid-namespace` is the pinned URL that
[0004](./docs/decisions/0004-globalid-derives-from-a-pinned-namespace.md) derives every IFC
`GlobalId` from. **There is no `GlobalId` field anywhere in this format** — it is derived on
export and never authored. **Changing this URL re-identifies every node in the model**, so
every downstream system holding a previously exported identifier sees the whole model deleted
and recreated. It is one line of a registry file and it is not a routine edit.

### 7.2 `namespace`

```
(namespace <namespace>
  (description "<text>"))
```

| Element       | Arity | Contents                                                                  |
|---------------|-------|---------------------------------------------------------------------------|
| *namespace*   | `1`   | Positional. The namespace, per the production in [4.1](#41-identifiers).   |
| `description` | `1`   | A string: what authority issues these ids, and what a local part looks like. |

The description is for the person reading the registry. Nothing in the engine reads it.

### 7.3 `type`

```
(type <type-name>
  (kind <Kind>)
  (geometry <geometry>)
  (description "<text>")
  (invariant <check-name> (<parameter> <value>) …))
```

| Child         | Arity  | Contents                                                                     |
|---------------|--------|------------------------------------------------------------------------------|
| `kind`        | `1..n` | The kinds a node of this type may declare.                                   |
| `geometry`    | `1..n` | The geometry forms a node of this type may declare, including `absent`.      |
| `description` | `1`    | A one-line string.                                                           |
| `invariant`   | `0..n` | A check that applies to every instance of the type. Same shape as `assert`.  |

- `geometry` here may take the value **`absent`**, which permits an instance to omit its
  `geometry` child. A type permitting both an area and no geometry lists both. `absent` is
  legal only in this position — a node expresses absence by omitting the child, never by
  naming it.
- **A node whose declared `kind` or `geometry` is not in its type's permitted set is a load
  error naming all three** — the node, the type, and the value that was not permitted.
- An `invariant` applies automatically to every instance of the type, including instances
  added later, so an invariant true of a type is stated once rather than copied onto a
  hundred and fifty instances. A violation names the instance, the invariant, the parameters
  it was evaluated with, and the registry file and line that declared it.
- An `invariant` is **never inherited.** It applies to the instances of the type that
  declares it and to nothing else: not to the nodes those instances contain, not to the
  members of a zone that carries one, and not from one type to another — the type registry
  declares no hierarchy for a type to inherit through. A rule that should hold of the rooms
  inside a storey is declared on the room type, because a rule that reached instances through
  containment would apply to a different set of nodes every time somebody moved one.
- An `invariant` naming a check that cannot apply to the type's `kind` or `geometry` is a
  load error caught at **registry** load, not at run. *Cannot apply* means no instance the
  type permits could be examined at all — a check written on an edge, one that applies to no
  `kind` the type permits, or one that measures a geometry form the type permits none of.
  Where a type permits several forms and the check applies to some of them, the invariant is
  accepted and binds to the instances it can examine: a type permitting both an area and no
  geometry carries a closure check on the instances that have an area.

### 7.4 `predicate`

```
(predicate <predicate-name>
  (unit <unit>)
  (shape <shape>)
  (dimension <n>)
  (claim-bearing #f)
  (strict #t)
  (description "<text>"))
```

| Child           | Arity  | Contents                                                                        |
|-----------------|--------|---------------------------------------------------------------------------------|
| `unit`          | `0..1` | The unit its values are expressed in. Omitted means the predicate is non-dimensional. |
| `shape`         | `1`    | `scalar`, `coordinate`, `transform` or `text`.                                  |
| `dimension`     | `0..1` | An integer, `2` or `3`. Required when `shape` is `coordinate`, forbidden otherwise. |
| `claim-bearing` | `0..1` | `#t` or `#f`. Omitted means `#t`, and canonical form omits it when true.         |
| `strict`        | `0..1` | `#t` or `#f`. Omitted means `#f`, and canonical form omits it when false.       |
| `description`   | `0..1` | A string.                                                                       |

- **`claim-bearing` defaults to true**, so opting a predicate out of provenance is a thing
  somebody writes down and a reviewer reads, rather than a thing that happens by omission.
- `strict` escalates an ambiguous resolution from a report to a failure for this predicate.
- A predicate name colliding with a reserved structural tag is a registry error — see
  [4.2](#42-registry-names).

### 7.5 `frame`

```
(frame <id>
  (label "<text>")
  (unit <unit>)
  (parent <frame-id>)
  (transform <claim-id>)
  <claim> …)
```

| Child       | Arity  | Contents                                                            |
|-------------|--------|---------------------------------------------------------------------|
| `label`     | `0..1` | A string.                                                           |
| `unit`      | `1`    | The frame's one linear unit.                                        |
| `parent`    | `0..1` | The frame this one is expressed relative to.                        |
| `transform` | `0..1` | The id of a claim whose value shape is `transform`.                 |
| *claim*     | `0..n` | Claims, typically including the transform claim `transform` names.   |

**A frame is both a registry entry and a node.** It is declared here because its unit and its
parent are vocabulary the consuming repository owns; it carries an id and claims because the
relationship between two frames is a measurement, not a configuration constant.

- **Exactly one linear unit per frame.** A frame with none is a registry error; a frame with
  two is unrepresentable ([0005](./docs/decisions/0005-one-linear-unit-per-frame.md)).
- `parent` and `transform` are present together or absent together. A root frame has neither
  and is the projected coordinate reference system the chain is rooted at. Exactly one root
  frame exists per model.
- A cycle in the parent chain is a load error naming the cycle. A missing parent, or a
  `transform` that does not resolve to a transform-shaped claim, is a load error naming both
  ends.
- The transform being a claim is what makes a cross-frame answer accountable: the georeference
  fit carries the source, method, date and accuracy of the fit that produced it, and that
  accuracy is a systematic term in every cross-frame budget it touches.

#### 7.5.1 The frame graph

**The unit a frame declares is the unit of every coordinate expressed in it**, and it is
read from that frame's registry entry rather than assumed. The definition of the unit —
how long one of it is — is the engine's and is pinned in [4.5](#45-units); which unit a
frame is in is registry data, and the two are never swapped for one another.

**Transforms compose along the chain, in both directions.** A position in one frame is a
position in any other frame the model declares: the route runs up the parent chain to the
lowest frame both chains pass through and back down the other, applying
[6.6.3](#663-transform) at each step going up and its inverse at each step coming down. A
transform is written in one direction and walked in both, because a fit measured from a
building to a survey is the same measurement as one from the survey to the building.
Writing the reverse down as a second claim would be a second source of truth for one fit.

A route that cannot be walked is answered with what stopped it and never with an
approximation: a frame nothing declares, a chain that cycles, a frame whose `transform`
resolves to no transform claim, two frames whose chains never meet, a frame whose unit is
no linear unit, and a transform that cannot be inverted.

**A shape is declared in exactly one frame and is transformed on demand.** Storing one
shape in two frames is two sources of truth that drift the moment the georeference is
re-fitted, so every part of one shape names one frame:

- An `edge` and each `vertex` it runs between are in one frame.
- A `loop` and each `edge` it traverses are in one frame.
- A `node` that declares a `frame` and each `loop` it names in `boundary` are in one frame.
  A node that declares none takes the frame its outline is in, which is ordinary.

Any of the three declared in two frames is a load error naming both frames and both ends.
Nothing is converted to reconcile them: the frames are related by a measurement with a
source and an accuracy, and applying it silently would put a fitted number into a file
somebody authored.

### 7.6 `tolerance`

```
(tolerance <tolerance-name>
  (value <n> <unit>)
  (description "<text>"))
```

| Child         | Arity  | Contents                                    |
|---------------|--------|---------------------------------------------|
| `value`       | `1`    | A real number and a unit.                   |
| `description` | `0..1` | A string.                                   |

**No numeric literal tolerance appears in engine code, and none appears in an assertion.**
An operation that needs a tolerance takes a name; referring to an unregistered tolerance is a
load error with a position; there is no default and no fallback
([0012](./docs/decisions/0012-tolerances-are-registry-data.md)). Every result computed
against a tolerance reports which named tolerance produced it.

### 7.7 `route`

```
(route <rule-name>
  (namespace <namespace>)
  (kind <kind>)
  (type <type-name>)
  (file "<path>")
  (description "<text>"))
```

| Element       | Arity  | Contents                                                                     |
|---------------|--------|------------------------------------------------------------------------------|
| *rule-name*   | `1`    | Positional. A registry name, per [4.2](#42-registry-names).                  |
| `namespace`   | `0..1` | A declared id namespace. Matches the namespace of the id the node is written with. |
| `kind`        | `0..1` | A kind. Matches the kind the node declares.                                  |
| `type`        | `0..1` | A declared type name. Matches the type the node declares.                    |
| `file`        | `1`    | A string: a path relative to the model root, ending in `.dfc`, written with forward slashes. |
| `description` | `0..1` | A string.                                                                    |

**A routing rule says which file a newly authored node is written to.** It is the answer to
the one question the write path has to ask and a read path never does. Left implicit, the
answer becomes "wherever the last person put things", which is how one category of thing ends
up spread over six files; and a filing convention that lives only in an author's head is one a
second author cannot follow ([0015](./docs/decisions/0015-the-cli-is-the-primary-write-path.md)).

**The rules are data, not code.** The engine does not know that meeting rooms belong beside
corridors, and no behaviour anywhere depends on which file a node is in — layout is authored,
and this is what authors it ([0010](./docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
Nothing *reads* through these rules either: a node is found by its id, wherever it was
written, and moving a file changes no answer the model gives.

**A criterion that is left out matches anything.** A rule written with none of the three
matches every node, which is the catch-all a small model files everything through. A rule
written with all three matches one type of one kind from one authority.

**A geometric node carries neither a kind nor a type** ([6.2](#62-vertex)–[6.4](#64-loop)),
so a rule naming either of those never matches one. Vertices, edges and loops are routed by a
rule that matches on `namespace` alone, or by a catch-all.

**Exactly one rule must match.** A node matched by none, and a node matched by more than one,
are both refused, naming the node and every rule consulted. Neither is resolved by picking a
rule — not the first written, and not the most specific. A filing decision the tool makes on
its own is one that is visible in nothing the author wrote, and the fix for both refusals is a
change to this registry: overlapping rules are made disjoint, and a node nothing covers gets a
rule. This is the one place where "no answer" is better than a plausible one.

`file` is relative because it is resolved against the model root, and so means the same thing
in every clone of the repository. A path that leaves the root, and one whose extension is not
`.dfc`, are each a load error on the rule: a walk of the model reads neither, so a node routed
into one would be a change that appears to have been made and was not.

## 8. Canonical form

**Every model has exactly one canonical printing.** `fmt` produces it, the CLI's write path
produces it, and `fmt --check` fails anything that is not it. That is what keeps a
hand-edited file and a CLI-written file from developing competing house styles
([0015](./docs/decisions/0015-the-cli-is-the-primary-write-path.md)).

Printing is idempotent, deterministic across runs, processes and machines, and
`parse → print → parse` yields an equal tree.

### 8.1 Layout is inherited

Line breaking and indentation come from `sexpr.Print` unchanged. Restated here because a
reimplementation needs them:

- Each top-level form starts on its own line. Blank lines between forms are not preserved.
- A list is written on one line, elements separated by a single space, whenever that line
  would end at or before column **80**.
- A list that would run past 80 breaks to one element per line. The head stays beside the
  opening parenthesis; the remaining elements are indented **two spaces past the list's own
  opening parenthesis**, so they line up under the head.
- Indentation compounds with nesting, measured from each list's own opening parenthesis.
- Only lists that do not fit are broken. A short inner list stays on one line inside a broken
  outer one.
- An atom is never broken. A string longer than 80 columns simply runs past it.
- No line has trailing whitespace. Output ends with exactly one LF.

### 8.2 Defaults are omitted

**A child equal to its default is not printed.** `(rank normal)` never appears; `(rank
deprecated)` does. `(claim-bearing #t)` never appears; `(claim-bearing #f)` does.
`(strict #f)` never appears; `(strict #t)` does.

Both spellings parse. Only one prints, which is what keeps one graph from having two
printings. The same rule is why the format has no shorthands: there is no abbreviated
accuracy, no implied unit and no positional alternative to a named child anywhere.

### 8.3 Ordering

**Canonical form sorts content.** This is the question the format had to settle before a
corpus existed, and it is settled in favour of sorting.

**The reasoning.** Preserving authored order means a file reads in the order somebody thought
about it, which is a genuine benefit and is not the one that decides it. The CLI is the
primary write path, and it prints a whole file from an in-memory graph
([0016](./docs/decisions/0016-writes-are-all-or-nothing.md) — writes are whole-file and
atomic). An in-memory graph does not carry the order a human happened to type things in, so a
printer that claimed to preserve authored order would either have to reorder anyway — fighting
every hand edit — or carry authored order as data in the graph, which is a second thing to get
right in every mutation for the sake of aesthetics. Sorting removes the choice: the two
authors of a file cannot disagree about order because neither of them picks it. It also makes
`fmt --check` a real normalisation gate rather than a check on part of the file, and it makes
a diff show what changed rather than where somebody inserted it.

**The cost is real and is accepted.** A file no longer reads in the order it was written.
Related nodes are not adjacent unless their ids sort adjacently, so a reader following a
thought jumps around. What remains authored is which node lives in which file, which is the
coarse organisation that actually helps, and comments move with what they annotate
([8.6](#86-comments)).

**The rules.**

1. **Top-level forms** are ordered first by tag, in this fixed order:

   ```
   project  namespace  type  predicate  tolerance  frame  route  node  vertex  edge  loop
   ```

   and then, within a tag, by the form's positional name or id, ascending, compared byte-wise
   on its UTF-8 encoding.

2. **Structural children** are printed in the order their form's table lists them. Where a
   structural child repeats — `member-of`, `boundary`, `backed-by`, `kind` and `geometry` in a
   type entry, `invariant`, and the terms inside an `accuracy` — the repeats are sorted within
   their own group.

3. **Claim forms** follow every structural child, sorted. **Assertion forms** follow the
   claims, sorted.

4. **The sort key is the child's inline rendering, compared byte-wise on its UTF-8
   encoding.** The inline rendering is the form printed on a single line with one space
   between elements and the 80-column limit ignored; it always exists, because an atom is
   never broken. Byte-wise comparison of UTF-8 is code-point order.

   This falls out usefully: a form's tag is the first thing in its rendering, so claims group
   by predicate before anything else distinguishes them, and two claims for one predicate then
   order by their values.

5. **Ordered sequences are never sorted.** Their order is data. Exhaustively, they are: the
   two ids of `vertices`, the ids of `edges`, the components of a coordinate value, and the
   `translation`, `rotation` and `scale` of a transform value together with the numbers inside
   each.

Sorting is a total order on any well-formed file: two children with the same inline rendering
are the same bytes, so their relative order is not observable.

### 8.4 Numbers

**A number prints as the shortest decimal that reads back as the same value**, with one
adjustment: a real whose shortest form has neither a fraction nor an exponent gains `.0`, so
that it reads back as a real rather than an integer.

- `8.50` prints as `8.5`. `100` as a real prints as `100.0`. `0.30480000000000002` prints as
  itself, because nothing shorter reads back the same.
- An exponent is used when it is shorter, and is printed as the underlying formatter produces
  it.
- **Trailing zeros carry no meaning**, here or anywhere else in the system. Significance is
  carried by a claim's accuracy, explicitly, where a query can read it — not by how somebody
  typed the number.
- A non-finite value — an infinity, a NaN — has no printing and cannot occur in a model.

### 8.5 Strings

A string prints double-quoted, escaping exactly what must be escaped for the same text to
read back:

| Character                       | Printed as                    |
|---------------------------------|-------------------------------|
| `"`                             | `\"`                          |
| `\`                             | `\\`                          |
| newline, carriage return, tab, backspace, form feed | `\n` `\r` `\t` `\b` `\f` |
| any other control character     | `\uXXXX`, hex digits upper case |
| anything else                   | itself, as UTF-8              |

Nothing else is escaped. A non-ASCII letter is written as itself, not as `\uXXXX`. An escape
round-trips: printing a parsed string and re-parsing it yields the same string.

Because a newline is escaped, **canonical form never contains a raw line break inside a
string literal**, even though the underlying grammar accepts one on input.

### 8.6 Comments

Comments survive a format cycle. Their text is never altered — a block comment is written back
exactly as it appeared, delimiters included.

**A comment attaches to the next sibling datum inside its enclosing form, and moves with it
when that sibling sorts.** A comment with no following sibling attaches to the end of its
enclosing form and stays there. A comment at the top of a file attaches to the first top-level
form and travels with it.

This is the whole of the guarantee, and it is the one place a format cycle can move something
a person wrote. It is stated positively because it has to be tested: a comment above a claim
stays above that claim wherever the claim sorts to, and a comment that annotated a *position*
rather than a datum — "everything below here is from the 2024 survey" — does not survive
sorting in any useful way. Write that as a comment on the first form it refers to.

Two consequences are inherited from the underlying printer and are worth stating:

- Each comment occupies its own line.
- **A form holding any comment is always broken across lines**, even when it would otherwise
  fit, and a comment before a form's first child pushes that child off the opening line.

## 9. Worked example

A complete, loadable model: two spaces sharing a boundary, a claim with full provenance, and
two frames related by a transform claim. Everything below is in canonical form.

### 9.1 `registry/registry.dfc`

```lisp
(project
  (label "Riverside example")
  (globalid-namespace "https://example.org/models/riverside"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
(namespace survey
  (description "Claim ids and control points issued by Acme Surveys."))

(type Corridor
  (kind Space)
  (geometry area)
  (description "A circulation space between rooms."))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings.")
  (invariant boundary-loops-close (tolerance boundary-closure)))

(type Partition
  (kind Element)
  (geometry line)
  (description "A non-loadbearing wall between spaces."))

(predicate frame-transform
  (shape transform)
  (description "The rigid transform from a frame to its parent."))

(predicate position
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "The location of a vertex in its frame."))

(tolerance boundary-closure
  (value 0.005 m)
  (description "How far a loop may fail to close and still count as closed."))

(frame frame:building
  (label "Building local grid")
  (unit m)
  (parent frame:survey-grid)
  (transform survey:C-0031)
  (frame-transform
    (id survey:C-0031)
    (value
      (transform
        (translation 401235.117 3172884.902 44.318)
        (rotation 0.9999985 -0.0017453 0.0 0.0017453 0.9999985 0.0 0.0 0.0 1.0)
        (scale 1.0)))
    (source "Georeferencing report GR-2026-002, Acme Surveys")
    (method method:gnss-static)
    (accuracy (independent 0.012 m) (systematic 0.008 m survey:CP-3))
    (date "2026-02-11")))

(frame frame:survey-grid (label "Site survey grid") (unit m))

(route corridors
  (kind Space)
  (type Corridor)
  (file "entities/circulation.dfc")
  (description "Circulation, kept apart from the rooms it connects."))

(route geometry
  (namespace geom)
  (file "geometry/level-1.dfc")
  (description "Every vertex, edge and loop of level 1."))

(route partitions
  (kind Element)
  (type Partition)
  (file "entities/partitions.dfc"))

(route rooms
  (kind Space)
  (type MeetingRoom)
  (file "entities/level-1.dfc"))
```

The four routing rules are disjoint, which is what makes each of them the whole answer for
what it covers. `rooms` and `corridors` share a kind and are told apart by their type;
`geometry` matches on namespace alone, because a vertex has no kind and no type to match on.
A `site:` node of a fourth type is matched by nothing and is refused rather than filed
somewhere plausible — the fix is a fifth rule, written here, where the next author can read
it.

The two frames are related by a measurement, not by a constant. Every cross-frame answer this
model produces carries `survey:CP-3` as a systematic term, counted once however many indoor
facts it reaches through — which is the difference between an honest error budget and an
optimistic one.

### 9.2 `entities/level-1.dfc`

```lisp
(node site:S-101
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (boundary geom:L-01))

(node site:S-102
  (label "East Corridor")
  (kind Space)
  (type Corridor)
  (geometry area)
  (frame frame:building)
  (boundary geom:L-02))

(node site:W-14
  (label "Partition, room B to corridor")
  (kind Element)
  (type Partition)
  (geometry line)
  (frame frame:building))

(vertex geom:V-01
  (frame frame:building)
  (position
    (value (0.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m) (systematic 0.008 m survey:CP-3))
    (date "2026-02-18")))

(vertex geom:V-02
  (frame frame:building)
  ; Shot twice: the 2026-02-18 value was taken before the partition moved.
  (position
    (id survey:C-0104)
    (value (4.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m) (systematic 0.008 m survey:CP-3))
    (date "2026-02-18")
    (rank deprecated)
    (superseded-by survey:C-0181))
  (position
    (id survey:C-0181)
    (value (4.05 0.0 0.0) m)
    (source "As-built check AB-2026-009, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.003 m) (systematic 0.008 m survey:CP-3))
    (date "2026-05-06")))

(vertex geom:V-03
  (frame frame:building)
  (position
    (value (4.05 3.0 0.0) m)
    (source "As-built check AB-2026-009, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.003 m) (systematic 0.008 m survey:CP-3))
    (date "2026-05-06")))

(vertex geom:V-04
  (frame frame:building)
  (position
    (value (0.0 3.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m) (systematic 0.008 m survey:CP-3))
    (date "2026-02-18")))

(vertex geom:V-05
  (frame frame:building)
  (position
    (value (8.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m) (systematic 0.008 m survey:CP-3))
    (date "2026-02-18")))

(vertex geom:V-06
  (frame frame:building)
  (position
    (value (8.0 3.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m) (systematic 0.008 m survey:CP-3))
    (date "2026-02-18")))

(edge geom:E-01 (frame frame:building) (vertices geom:V-01 geom:V-02))

; The shared partition. Both rooms reference this edge, so it cannot drift.
(edge geom:E-02
  (frame frame:building)
  (vertices geom:V-02 geom:V-03)
  (backed-by site:W-14))

(edge geom:E-03 (frame frame:building) (vertices geom:V-03 geom:V-04))

(edge geom:E-04 (frame frame:building) (vertices geom:V-04 geom:V-01))

(edge geom:E-05 (frame frame:building) (vertices geom:V-02 geom:V-05))

(edge geom:E-06 (frame frame:building) (vertices geom:V-05 geom:V-06))

(edge geom:E-07 (frame frame:building) (vertices geom:V-06 geom:V-03))

(loop geom:L-01
  (frame frame:building)
  (edges geom:E-01 geom:E-02 geom:E-03 geom:E-04))

(loop geom:L-02
  (frame frame:building)
  (edges geom:E-05 geom:E-06 geom:E-07 geom:E-02))
```

Reading it back:

- **The two spaces share a boundary rather than a coordinate.** `geom:E-02` appears in both
  loops, so the partition between the rooms is one edge with one identity. Moving it moves
  both rooms, and a sliver gap between them is not a state this file can express.
- **`geom:V-02` carries a claim with full provenance, twice.** The first is deprecated with a
  pointer to what replaced it, so the record of what was believed in February survives the
  correction in May. Resolution considers only the live claim; the deprecated one stays
  readable.
- **Both `position` claims on `geom:V-02` carry `survey:CP-3` as a systematic term.** A
  clearance computed between `geom:V-02` and `geom:V-05` counts that 8 mm once, not twice,
  because the two terms name the same id.
- **`site:W-14` is the element backing `geom:E-02`**, which makes that boundary physical.
  Nothing in the file says "physical" — the classification is computed from the reference, so
  it cannot drift away from what the model says.

## 10. Versioning of this specification

This document carries a version of the form `MAJOR.MINOR`, stated at the top. The current
version is **1.1**, which added the `route` form of [7.7](#77-route).

| Change                                                             | Version effect |
|--------------------------------------------------------------------|----------------|
| Adding an optional child, a value shape, or a whole new form        | MINOR          |
| Clarifying wording without changing what loads or what prints       | neither        |
| Anything that makes a currently valid file fail to load             | MAJOR          |
| Anything that changes what a currently valid file means             | MAJOR          |
| Anything that changes the bytes `fmt` produces for an unchanged file | MAJOR         |
| Reserving a new structural child tag                                | MAJOR          |

The last two carry the most weight and are the reason the rule is written down.

**The canonical printing is part of the contract.** A change that alters the bytes `fmt`
produces for a file nobody edited turns every model's next `fmt` run into a whole-tree diff,
which is exactly the noise the canonical form exists to prevent. Reformatting is therefore a
MAJOR change, however cosmetic the motivation.

**Reserving a structural tag is MAJOR** because a consuming repository may already have a
predicate by that name, and its files would stop loading — see [4.2](#42-registry-names).

**Files carry no version stamp**, and this is deliberate. A model is version controlled
alongside the engine version it is checked with, so a per-file stamp would be a second,
hand-maintained copy of a fact the repository already records — and a stamp that drifts is
worse than none, because it is confidently wrong. A consumer that needs the version reads it
from the engine, where the CLI's machine output contract already carries one
([0014](./docs/decisions/0014-the-machine-output-contract-is-part-of-the-interface.md)).

## 11. Not in this version

Named so that their absence reads as a decision rather than an oversight.

- **The observation file format.** Observations link to entities but are their own format
  with their own specification.
- **Non-straight edges.** Representable already, as a claim under a registered predicate — see
  [6.3](#63-edge). No form here changes when one arrives.
- **A `GlobalId` field.** Derived on export, never authored
  ([0004](./docs/decisions/0004-globalid-derives-from-a-pinned-namespace.md)).
- **Derived values of any sort** — area, length, centroid, bounding box, tessellation,
  membership, propagated accuracy. None of them is a field, a comment or a sidecar inside the
  source tree ([0009](./docs/decisions/0009-derived-values-are-never-written-back.md)).
- **A scenario, variant or phase dimension.** Not deferred — excluded. A variant is a branch
  ([0013](./docs/decisions/0013-variants-are-branches.md)).
- **A `preferred` rank, a priority or a weight.** Excluded, not deferred
  ([0007](./docs/decisions/0007-rank-is-closed.md)).
- **An expression language for assertions.** Excluded, not deferred
  ([0011](./docs/decisions/0011-assertions-are-named-parameterised-checks.md)).

## 12. Reviewed against the decision records

Every accepted record in [`docs/decisions/`](./docs/decisions/) was read against this
document. Where a record constrains the format, the constraint is implemented at the section
named. No contradiction was found; the one place the records left an open reading is recorded
below.

| Record | Where this document implements it                                                              |
|--------|------------------------------------------------------------------------------------------------|
| 0001   | [6.1](#61-node--a-semantic-node)–[6.4](#64-loop): two families, `kind`/`type` on one and not the other, semantic nodes reference loops. |
| 0002   | [4.1](#41-identifiers), [6.7](#67-retired): id versus label, retirement rather than deletion.    |
| 0003   | [4.1](#41-identifiers): `namespace:local`, split on the first colon, registry-closed, no inferred meaning. |
| 0004   | [7.1](#71-project): the pinned URL, and no `GlobalId` field anywhere.                            |
| 0005   | [4.5](#45-units), [6.6.2](#662-coordinate), [7.5](#75-frame): one unit per frame, `ft`/`usft` pinned, coordinates pinned to the frame's unit, no conversion at load. |
| 0006   | [6.6.5](#665-accuracy-terms): 1σ storage, independent and systematic terms, a shared term named by id and counted once. |
| 0007   | [6.5](#65-claims): rank closed at `normal` and `deprecated`, deprecation requires a replacement. |
| 0008   | [6.5](#65-claims): a bare scalar where a claim belongs is a load error, unconditionally.         |
| 0009   | [6.3](#63-edge), [11](#11-not-in-this-version): no stored classification, no derived value in the source tree. |
| 0010   | [1](#1-scope), [7](#7-registry-forms): two closed sets compiled in, everything else registry data with no meaning attached. |
| 0011   | [6.8](#68-assert): named parameterised checks, no expression language, parameters validated at load. |
| 0012   | [6.8](#68-assert), [7.6](#76-tolerance): named tolerances only, no literal anywhere.             |
| 0013   | [11](#11-not-in-this-version): no scenario, variant or phase dimension.                          |
| 0014   | [10](#10-versioning-of-this-specification): the version a consumer reads comes from the output contract, not from a file stamp. |
| 0015   | [8](#8-canonical-form): exactly one canonical printing, which is what makes the two authors of a file converge. |
| 0016   | [8.3](#83-ordering): whole-file printing from an in-memory graph is why sorting was chosen over preserving authored order. |

**One open reading, resolved here.** Record 0008 says the remedy for not knowing a claim's
provenance is to state it "using the vocabulary the registry provides for it", while record
0010 closes the registry set at five. This document resolves the two by making `method` an
**id** rather than a free string ([6.5](#65-claims)): its namespace is governed by the id
namespace registry, which is one of the five, so a method is registry-governed vocabulary
without a sixth registry. If a later story finds that insufficient, adding a method registry
is a MINOR change to this document and an amendment to 0010.
