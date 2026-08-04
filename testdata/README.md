# The fixture corpus

Every layer of this engine treats the parse and print layer as ground truth. A
defect there does not announce itself as a parse failure — it surfaces much
later as a value which is subtly wrong, in a system whose whole purpose is being
able to trust its values. These directories are what keep that from happening
quietly.

A directory walk reads only files whose extension is `.dfc`, so this file and
the goldens beside the fixtures are invisible to it.

## Layout

| Directory                  | Holds                                                                                     |
|----------------------------|-------------------------------------------------------------------------------------------|
| `corpus/valid`             | One file per construct of [`SPEC.md`](../SPEC.md), plus one combined model. Each has its canonical printing recorded beside it as `.want`. |
| `corpus/invalid`           | One file per error case about a file and its lexis. Each has its rendered diagnostic recorded beside it as `.txt`. |
| `validate`                 | The validator's own fixtures: the error cases about the shape of a form, each with its rendered diagnostic as `.txt`, plus three files which are accepted. |
| `registry`                 | The registry loader's own fixtures. One *directory* per case, because a registry is a property of a source tree and not of a file, with its rendered diagnostics inside it as `diagnostics.txt`. |
| `node`                     | The semantic node loader's own fixtures. One *directory* per case, each a registry and the nodes judged against it, with its rendered diagnostics inside it as `diagnostics.txt`. |
| `claim`                    | The claim loader's own fixtures. One *directory* per case, each a registry and the claims judged against it, with its rendered diagnostics inside it as `diagnostics.txt`. |
| `print`                    | The printer's own fixtures: one per printing behaviour, each with its canonical printing as `.want`. |
| `diagnostics`              | One file per shape of diagnostic rendering, each as `.txt`.                                 |
| `model`                    | A two-file model the runnable examples load.                                                |

`corpus/valid` is not a second copy of `print`. The printer's fixtures are one
per *behaviour* — how a comment moves, where a line breaks — and the corpus is
one per *construct*, so that a form of the specification which nothing writes is
a test failure rather than an omission nobody notices. `TestCorpusCoversEveryForm`
walks the tables in `forms.go` and is what enforces it.

## Regenerating the goldens

Every golden in every directory above is written from what this package
produced:

```sh
go test . -update
```

One flag rather than one per directory, because a change to the printer or to
the diagnostic renderer reaches all of them at once and regenerating half a
corpus leaves a tree nobody can read a diff of.

Run it when a change to the printer or the renderer is deliberate, and read the
diff — that diff *is* the review of the change. CI regenerates and then requires
the tree to be unchanged, so a golden edited by hand, or left stale by a change
nobody regenerated, fails there.

## Which fixture covers which error case

The cases about a file and its lexis are in `corpus/invalid`; the cases about
the shape of a form are in `validate`; the cases about what a registry declares
are in `registry`; the cases about a node read against a loaded registry are in
`node`, and the cases about a claim read against one are in `claim`. Each is
where the tests of that layer read them from.

| Specification | Error case                                          | Fixture                                     |
|---------------|-----------------------------------------------------|---------------------------------------------|
| 3.1           | A file which is not valid UTF-8                      | `corpus/invalid/invalid-utf8.dfc`           |
| 3.1           | A file beginning with a byte order mark              | `corpus/invalid/byte-order-mark.dfc`        |
| 2             | An improper list                                     | `corpus/invalid/improper-list.dfc`          |
| 2             | A quote shorthand                                    | `corpus/invalid/quote-shorthand.dfc`        |
| 2             | `nil` written as a placeholder                       | `corpus/invalid/nil.dfc`                    |
| 2             | The empty list                                       | `corpus/invalid/empty-list.dfc`             |
| 2, 4.3        | A lexeme which begins like a number and is not one   | `corpus/invalid/malformed-number.dfc`       |
| 2             | An unrecognised string escape                        | `corpus/invalid/invalid-escape.dfc`         |
| 2             | An unterminated string literal                       | `corpus/invalid/unterminated-string.dfc`    |
| 2             | An unterminated block comment                        | `corpus/invalid/unterminated-comment.dfc`   |
| 2             | A list the input ran out before closing              | `corpus/invalid/unclosed-list.dfc`          |
| 2             | A closing parenthesis which closes nothing           | `corpus/invalid/unexpected-close-paren.dfc` |
| 6             | A tag the format does not know                       | `validate/unknown-tag.dfc`                  |
| 5             | A required child which is missing                    | `validate/missing-child.dfc`                |
| 5             | A child which may not repeat, repeated               | `validate/repeated-child.dfc`               |
| 5             | A form written where it is not permitted             | `validate/misplaced-form.dfc`               |
| 5             | The wrong number of positional arguments             | `validate/argument-count.dfc`               |
| 6.5, 6.6.5    | A form which holds nothing                           | `validate/empty-form.dfc`                   |
| 6             | A datum written where a form belongs                 | `validate/not-a-form.dfc`                   |
| 6.5           | A claim whose children are wrong                     | `validate/claim-children.dfc`               |
| —             | Several independent problems in one file             | `validate/one-pass.dfc`                     |
| 7             | A name declared twice, in one file and across two    | `registry/duplicates/`                      |
| 4.1, 7.2      | A namespace which is not one                         | `registry/malformed/`                       |
| 4.2, 7.4      | A predicate colliding with a reserved structural tag | `registry/malformed/`                       |
| 1, 7.3, 7.4   | A kind, a geometry form, a shape or a dimension which is not one | `registry/malformed/`           |
| 4.3, 7.6      | A tolerance magnitude written as a count             | `registry/malformed/`                       |
| 7.1           | A `globalid-namespace` which is not a URL            | `registry/malformed/`                       |
| 5, 7          | A child a registry form does not permit              | `registry/unknown-key/`                     |
| 4.1, 7.5      | A frame naming an unregistered namespace, or a parent nothing declares | `registry/dangling/`      |
| 7.5           | A frame parent chain which never reaches a root      | `registry/cycle/`                           |
| 7.5           | A second root frame, and half of a non-root one      | `registry/roots/`                           |
| 7.1           | A model declaring no project at all                  | `registry/empty/`                           |
| 1, 6.1        | A node kind or geometry form which is not one, and `absent` named on a node | `node/unknown-value/`         |
| 6.1, 7.3      | A node naming a type no registry file declares       | `node/undeclared-type/`                     |
| 6.1, 7.3      | A node whose type permits a different kind or geometry form, and one whose type does not permit absence | `node/not-permitted/` |
| 6.5, 7.4      | A claim written under a predicate no registry file declares | `claim/undeclared-predicate/`         |
| 6.6           | A claim value of a shape the predicate does not declare, and a coordinate of the wrong length | `claim/wrong-shape/` |
| 4.5, 6.6      | A claim value in a unit the predicate does not declare, one written with no unit, and one written with a unit where the predicate declares none | `claim/wrong-unit/` |
| 4.4, 6.5      | A date which is not RFC 3339 full-date                | `claim/malformed-date/`                     |
| 6.5           | A rank outside the closed set                         | `claim/unknown-rank/`                       |
| 4.1, 6.5      | One claim id written on two claims, across two files  | `claim/duplicate-id/`                       |
| 6.5           | A reference to a claim which carries no id of its own | `claim/dangling-reference/`                 |

Two stated limits are checked in Go rather than as fixtures, because a fixture
for either would be twenty kilobytes of parentheses whose golden nobody could
read: the nesting bound of `sexpr.MaxDepth`, and the cap on how many
diagnostics one run retains.

**What is not covered, and why.** Specification section 2 also excludes a
boolean written outside the positions which take one. Which positions those are
is partly registry data — a check's parameters are declared by the check
registry — so the engine cannot decide it until that registry loads, and it
belongs with the layer which reads it. So does what is left of section 6.9: a
reference from one entity to another which does not resolve is a question about
the whole graph rather than about any one family of it, and it is answered by
the pass which has read every family.

## The properties

Goldens say what the output is. They cannot say that the output still reads
back, which is what these check, over every valid fixture and over files nobody
wrote:

- `parse → print → parse` gives back the tree it was printed from.
- `print → print` is byte-identical: canonical form is a fixed point.
- Generated trees, drawn from the tables in `forms.go`, survive both.
- `FuzzParse` runs the loader against arbitrary bytes and requires it to report
  rather than crash.
- `FuzzPrintRoundTrip` runs the round trip against arbitrary bytes and requires
  the tree to survive it.

A test which only compares output against a recorded string can pass while that
output no longer reads back — a number which lost a digit, a string escaped so
that it reads back as different text, a comment which swallowed the datum after
it. Each of those produces stable, plausible, wrong bytes.
