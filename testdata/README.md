# The fixture corpus

Every layer of this engine treats the parse and print layer as ground truth. A
defect there does not announce itself as a parse failure — it surfaces much
later as a value which is subtly wrong, in a system whose whole purpose is being
able to trust its values. These directories are what keep that from happening
quietly.

A directory walk reads only files whose extension is `.dfc`, so this file and
the goldens beside the fixtures are invisible to it. Observation files are
`.obs` and are read by a walk of their own, which is what keeps the two formats
from picking each other up.

## Layout

| Directory                  | Holds                                                                                     |
|----------------------------|-------------------------------------------------------------------------------------------|
| `corpus/valid`             | One file per construct of [`SPEC.md`](../SPEC.md), plus one combined model. Each has its canonical printing recorded beside it as `.want`. |
| `corpus/invalid`           | One file per error case about a file and its lexis. Each has its rendered diagnostic recorded beside it as `.txt`. |
| `validate`                 | The validator's own fixtures: the error cases about the shape of a form, each with its rendered diagnostic as `.txt`, plus three files which are accepted. |
| `registry`                 | The registry loader's own fixtures. One *directory* per case, because a registry is a property of a source tree and not of a file, with its rendered diagnostics inside it as `diagnostics.txt`. |
| `node`                     | The semantic node loader's own fixtures. One *directory* per case, each a registry and the nodes judged against it, with its rendered diagnostics inside it as `diagnostics.txt`. |
| `claim`                    | The claim loader's own fixtures. One *directory* per case, each a registry and the claims judged against it, with its rendered diagnostics inside it as `diagnostics.txt`. |
| `topology`                 | The geometric node loader's own fixtures. One *directory* per case, each a registry and the vertices, edges and loops judged against it, with its rendered diagnostics inside it as `diagnostics.txt`. |
| `boundary`                 | The fixtures of the pass which joins the two families. One *directory* per case, each a registry and whichever families the case needs, with its rendered diagnostics inside it as `diagnostics.txt`. |
| `frame`                    | The fixtures of the pass which resolves each frame's transform to the claim measuring it. One *directory* per case, each a registry declaring frames and the claims judged against it, with its rendered diagnostics inside it as `diagnostics.txt`. |
| `print`                    | The printer's own fixtures: one per printing behaviour, each with its canonical printing as `.want`. |
| `diagnostics`              | One file per shape of diagnostic rendering, each as `.txt`.                                 |
| `graph`                    | The whole-graph loader's own fixtures. One *directory* per case, each a whole model — registry, both families and the claims on them — with its rendered diagnostics inside it as `diagnostics.txt`. Two of them are about the links to observation files: `observed` loads clean and holds three `.obs` files, one of which carries a line no reader can make sense of, which is what lets a test tell a file that was read from one that was not; `unresolved-observations` holds a link to a file that is not there, one that leaves the model root and one whose extension is not `.obs`, and links to nothing at all on disk. |
| `invariant`                | The fixtures of the pass which binds a type's invariants to its instances. One *directory* per case, each a whole model whose types carry invariants, and each expected to load clean — what these assert is what binds and what a run of it reports, not a diagnostic. |
| `assert`                   | The fixtures of the pass which reads the assertions written on things against the model they were written in. One *directory* per case, each a whole model whose things carry assertions. `valid` loads clean; the rest each hold one shape of refusal, and three of them name checks the engine has not written yet and are read against a check set the test assembles. |
| `rules`                    | The fixtures of the pass which runs a model's rules of both kinds together. One *directory* per case, each a whole model whose types carry invariants and whose things carry assertions, and each expected to load clean — what these assert is what binds, what a run of it reports and what a filter narrows it to, not a diagnostic. |
| `checks`                   | The fixtures the registered check set is run over. Every one loads clean, because every failure these are about is a shape or a reference which is perfectly well-formed: `violating` breaks one rule per check, `satisfied` is the same model drawn right — which is what catches a check that reports a violation of everything put to it — and `degenerate` is the model as it looks before anybody has finished drawing one. `inside` is the one the containment rule is run over: a floor plate, the outline a boundary survey recorded for it, and the rooms, runs of wall and devices which sit inside the plate or reach past it — one fixture per form a subject can take, because a point, a run of corners and an outline are read out of the model three different ways and answer one question. `grid` is four *directories* which differ by one line each: `silent` is a chain rooted at a projected coordinate reference system where every transform is a scale of one and nobody said whether ground equals grid, `affirmed` states the combined factor as a claim, `stated` carries it on the georeferencing transform, and `unnamed` is rooted at no projection at all. The plot in all four is 400 m by 300 m, so it is 500 m corner to corner and the arithmetic in the diagnostic is a figure a reader can check. |
| `review`                   | One model at two revisions — `base` is the merge base and `head` is the change under review — which the runnable examples of the diff-aware checks compare. Both load clean: what these fixtures are about is the difference between them, and a revision which did not load would be a different question. |
| `measure`                  | The fixtures of the pass which computes how big things are. One *directory* per case, each a registry and the geometry measured against it. `shapes`, `courtyard` and `far-from-the-origin` load and measure clean — what those assert is the figure, worked out by hand in the test beside them — and the rest each hold one shape which cannot be measured, with its rendered diagnostics inside it as `diagnostics.txt`. `arcs` is the curved corner of it: four shapes whose walls bend, all but the last measuring clean, with `degenerate.txt` and `crossing.txt` beside it holding what a parameterisation which is not a curve and a curve which laps over its own ring had to say. |
| `overlay`                  | The fixtures of the pass which offsets and overlays regions. One *directory* per case: `shapes` loads and overlays clean and is where every operation over a plane figure is worked out by hand, and `unusable` holds two shapes which are not shapes — one crossing itself, one enclosing nothing — with its rendered diagnostics inside it as `diagnostics.txt`. |
| `buildable`                | The fixture the setback-constrained buildable region is derived over. One model of eight plots which loads clean: two whose regime leaves a region, one whose regime consumes it, and five which the derivation refuses rather than the loader — an edge nothing states the setback of, an edge two claims are equally current about, setbacks written in a unit the frame is not in, one written outwards, and one shorter than the tolerance. What is buildable is written nowhere in it. |
| `siting`                   | The fixture the cross-frame siting query is answered over. Two *directories* holding byte-identical models and registries which differ in exactly one claim — the fit which measures the building's grid against the site's — so that replacing a measurement with a better one is a change a test can see both halves of the answer move under. The model loads clean and holds a buildable area on the site grid and five footprints set out on other grids: one clearing by metres, one clearing by ten millimetres against an uncertainty of twenty, one crossing the boundary, one nowhere near it, and one on a grid nobody stated the accuracy of the fit for. One control point is behind the boundary survey, the interior control and the georeference alike, which is what makes a shared systematic term arrive from both sides of a comparison. |
| `plan`                     | The fixture the storey plan is read over. One model which loads clean: a storey holding two meeting rooms which share a party wall, an alcove inside the first — a space inside a space, which is what says the walk goes all the way down — a doorway written as a line and so with no ring at all, and a zone, which is a grouping rather than a place. The party wall is measured twice and never reconciled, so two live claims arrive under one predicate on one anchor; a third wall carries a length somebody withdrew beside the one which replaced it; and the first room carries two captions, both live, neither rankable. What a sheet would print is written nowhere in it. |
| `derived`                  | The fixture the derived-geometry cache is computed over. One model which loads and derives clean: a floor plate with a courtyard cut out of it, the courtyard itself, a room inside the plate, a desk zone inside that room and so inside both, and a store in a second frame which is a member of nothing. Every figure it yields is worked out by hand in the test beside it, because a cached answer is only as good as the answer it stood in for and two computations of the same wrong number agree. Nothing is written into it: the tests which populate a cache copy it first and assert every file is byte-identical afterwards. |
| `membership`               | The fixtures the spatial membership of observations is derived over. Three *directories*: `yard` is a garden as one rectangle and `carved` is the same garden with a raised bed taken out of its north-east corner — the registry and both observation files are byte for byte identical between them, so every shot which changes hands between the two changed hands because a line was drawn and for no other reason. The bed links no observation file at all. Between them the shots cover a shot the garden cites as its own, shots it merely holds, one exactly on the line the carve is made along, one outside by less than the float solution which took it could see, two taken on a neighbouring grid which have to be carried across, one outside everything and one retired. `stranded` is the awkward third: a frame whose fit to the site has not been reduced, so it is the one fixture here which does not load clean, and a region somebody has decided on and nobody has measured. |
| `surface/terrace`          | The fixture the terrain surface is derived over. One model which loads clean, holding three pieces of ground and the shots taken on each: `site:S-terrace` has enough, `site:S-strip` has two, and `site:S-ridge` has four which all fall on one line. Every terrace shot which counts lies on the plane z = 100 + 0.05x - 0.02y, so an expected level is arithmetic a test does rather than a number somebody copied out of a run — and the ones which do not lie on it are exactly the ones which must not be used: a second shot of one mark two millimetres away in plan, a shot outside the terrace, and one five metres out in level and retired for it. One shot is taken on the neighbouring plot's grid and has to be carried across before it rests the surface on anything, and one sits a hundred millimetres beyond the north boundary on a solution which cannot see that far, so the surface reaches the boundary only where the derivation asks for the shots it cannot place. Nothing about the ground is written into it. |
| `surface/patio`            | The fixture the accuracy of a derived surface is put to a decision over: two patios of the same shape and the same 1.5 m by 1.25 m survey density ten metres apart, one picked up on a GNSS rover good to 21 mm in the vertical and one levelled to 2 mm, each in one occupation. Whatever differs between the two answers cannot be the density, because the density is identical. Every shot of both lies on the plane z = 100 + 0.0125y — one in eighty away from the house wall — so the fall is arithmetic a test states rather than a number copied out of a run, and what the fixture measures is what an answer is *worth* rather than what it is. `decision.txt` beside it is the outcome of the gate: the requirement stated before anything was derived, the achieved accuracy, and the verdict either way. It is regenerated by `go test . -update`, so a change to the propagation which moves the finding is a diff somebody has to explain. |
| `model`                    | A two-file model the runnable examples load.                                                |
| `observations`             | The observation format's own fixtures, which are not `.dfc` and are read by a walk of their own. One *directory* per case, each a registry and the observation files judged against it, with its rendered diagnostics inside it as `diagnostics.txt`. `valid` is one afternoon of site control with a retirement in the middle of it and loads clean; `malformed` holds every malformed-line case on its own line, so that one pass over one file reports all of them; `unterminated` is a file whose last line carries no line feed, which the next append would rewrite; `encoding` holds a byte order mark, a byte which begins no encoding and a carriage return, each in its own file because each stops the file it is in; and `log` is two files whose problems need more than one line — and three of which need more than one file — to see. `append` is the odd one out: four revisions of one file rather than a model, compared pairwise by [`ValidateAppendOnly`](../observation.go), with `edited.txt` and `truncated.txt` beside them holding what a quiet correction and a deletion had to say. |

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
`node`, the cases about a claim read against one are in `claim`, the cases about
the geometric family read against one are in `topology`, and the cases about a
frame's transform read against the claims which measure it are in `frame`. Each
is where the tests of that layer read them from.

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
| 7.7           | A routing rule filing into a path no walk of the model reaches, and one whose criteria name a kind, a namespace or a type nothing declares | `registry/routes/` |
| 6.8, 7.3      | An invariant naming a check nothing registers, one missing a parameter the check requires, one writing a parameter it does not take, one writing a numeric literal tolerance, and one naming a predicate nothing declares | `registry/checks/` |
| 7.3           | An invariant naming a check written on an edge rather than on a node, and two naming one which measures a geometry form the type permits none of | `registry/invariants/` |
| 6.8           | An assertion naming a check which applies to another form, and one naming a check which measures a geometry the node has none of | `assert/inapplicable/` |
| 6.8           | An assertion naming a check which applies to another kind | `assert/kind/`                          |
| 6.8, 6.9      | An assertion naming an id nothing in the model answers to, written as a single value and inside a parenthesised list | `assert/references/` |
| 6.8.1         | An assertion which restates a claimed value, beside four which name a predicate the subject claims and do not restate it | `assert/restatement/` |
| 1, 6.1        | A node kind or geometry form which is not one, and `absent` named on a node | `node/unknown-value/`         |
| 6.1, 7.3      | A node naming a type no registry file declares       | `node/undeclared-type/`                     |
| 6.1, 7.3      | A node whose type permits a different kind or geometry form, and one whose type does not permit absence | `node/not-permitted/` |
| 6.1, 6.9.1    | A node written inside two things                     | `node/two-parents/`                         |
| 6.9.1         | A containment which never reaches a node with no parent | `node/containment-cycle/`                |
| 6.9.1         | A nesting the containment hierarchy does not permit, including a zone at either end of one | `node/nesting-not-permitted/` |
| 6.9, 6.9.1    | A `within` or a `member-of` naming no node, a `member-of` naming a node which is not a `Zone`, a zone named twice, and each relation naming the node it was written on | `node/dangling-relation/` |
| 6.7, 6.9      | A retirement replaced by a node this model does not hold, and one replaced by the node it was written on | `node/retirement/` |
| 6.5, 7.4      | A claim written under a predicate no registry file declares | `claim/undeclared-predicate/`         |
| 6.6           | A claim value of a shape the predicate does not declare, and a coordinate of the wrong length | `claim/wrong-shape/` |
| 4.5, 6.6      | A claim value in a unit the predicate does not declare, one written with no unit, and one written with a unit where the predicate declares none | `claim/wrong-unit/` |
| 4.4, 6.5      | A date which is not RFC 3339 full-date                | `claim/malformed-date/`                     |
| 6.5           | A rank outside the closed set                         | `claim/unknown-rank/`                       |
| 4.1, 6.5      | One claim id written on two claims across two files, and one written on a claim and a frame | `claim/duplicate-id/`   |
| 6.5           | A reference to a claim which carries no id of its own | `claim/dangling-reference/`                 |
| 6.5, 7.4      | A bare scalar where a claim-bearing predicate belongs, beside the minimal and the full claim of the same predicate | `claim/bare-scalar/` |
| 6.5, 7.4      | A claim written under a predicate declared non-claim-bearing, beside the plain value it takes | `claim/claim-for-a-plain-value/` |
| 6.1           | A kind or a type written on a geometric node          | `validate/geometric-axes.dfc`               |
| 6.3, 6.4, 6.9 | A `vertices` or an `edges` naming nothing this model holds, and one naming the wrong sort of geometric node | `topology/dangling-reference/` |
| 6.3           | An edge which starts and ends at one vertex           | `topology/degenerate-edge/`                 |
| 4.1, 6.2–6.4  | One id written on two geometric nodes across two files, on nodes of two sorts, and on a node and a frame | `topology/duplicate-id/` |
| 6.1, 6.9      | A `boundary` naming nothing this model holds, one naming a geometric node which is not a loop, one naming a semantic node, and a loop named twice | `boundary/dangling-boundary/` |
| 6.3, 6.9      | A `backed-by` naming nothing this model holds, one naming a node which is not an `Element`, one naming a geometric node, and an element named twice | `boundary/dangling-backing/` |
| 6.4, 7.6      | A loop which does not close, beside one which closes within the declared tolerance | `boundary/closure/`         |
| 6.4           | A loop whose edges form one ring written in the order it is not traversed | `boundary/out-of-order/`     |
| 6.4           | A loop with a branch, one which traverses an edge twice, and one whose edges form two rings | `boundary/not-a-simple-cycle/` |
| 6.4           | A ring whose corners all lie on one line, one whose edges never close, one which passes through a corner written as two coincident vertices, and an edge whose two ends were surveyed to one coordinate | `measure/degenerate/` |
| 6.4           | A ring whose two long sides cross in the middle of it | `measure/self-intersecting/`                |
| 6.4, 7.6      | A ring with a corner out of the plane of the others by more than the declared tolerance | `measure/not-planar/`     |
| 6.4, 6.6      | A ring with a corner no position resolves for         | `measure/unmeasurable/`                     |
| 6.4, 6.9, 7.6 | A region bounded by two rings which face the same way in different planes | `measure/two-planes/`   |
| 6.3, 6.4      | An arc of no radius, one whose ends are not the same distance from its centre, one whose point on the curve is in line with its centre, past the far end, or out of the ring's plane, and one with no centre at all | `measure/arcs/` |
| 4.5, 7.5      | A frame declaring a unit which is no linear unit      | `registry/unknown-unit/`                    |
| 6.3, 6.4, 7.5 | An edge running to a vertex in another frame, and a loop traversing an edge in one | `topology/two-frames/`   |
| 6.1, 6.9, 7.5 | A node bounded by a loop declared in another frame     | `boundary/two-frames/`                      |
| 6.6.3, 7.5    | A frame whose `transform` names a claim whose value is not a transform | `frame/not-a-transform/`    |
| 4.1, 6.9      | One id held by two of the three families, which no single pass can see | `graph/duplicate-id/`       |
| 6.9           | One reference of each class naming something the model does not hold | `graph/unresolved/`          |
| 6.9, 6.9.1    | A ring in each of the three relations which can hold one: containment, frame parents and claim supersession | `graph/cyclic/` |

The observation format has its own specification —
[`docs/observation-file.md`](../docs/observation-file.md) — and its own table, because
nothing in it is an S-expression and none of the cases above reaches it.

| Specification | Error case                                          | Fixture                                     |
|---------------|-----------------------------------------------------|---------------------------------------------|
| 3, 7.1        | A form tag nothing declares                          | `observations/malformed/`                   |
| 4, 5, 6, 7.1  | Too few and too many fields                          | `observations/malformed/`                   |
| 4.1, 7.1      | A record identity and a frame which are not ids      | `observations/malformed/`                   |
| 4.2, 7.1      | A timestamp which is not RFC 3339, one with no offset at all, and one whose offset is `-00:00` | `observations/malformed/` |
| 4.3, 7.1      | A count where a real belongs, and a lexeme which is not a number | `observations/malformed/`       |
| 5, 7.1        | A negative precision and a negative antenna height   | `observations/malformed/`                   |
| 4.4, 6, 7.1   | A quoted string never closed, one carrying an escape the format does not know, one which is empty, and one followed by text | `observations/malformed/` |
| 4.4, 5, 6, 7.1 | A quoted id, and a reason nobody quoted              | `observations/malformed/`                   |
| 2, 7.1        | A last line with no line feed after it               | `observations/unterminated/`                |
| 2, 7.1        | A byte order mark, a byte which begins no encoding, and a carriage return before the line feed | `observations/encoding/` |
| 7.2           | Two records sharing one identity, across two files    | `observations/log/`                         |
| 6, 7.2        | A retirement naming a record the log does not hold, one naming itself, one naming a record written later in another file, and a record retired twice | `observations/log/` |
| 5, 7.2        | A frame and an id namespace no registry file declares | `observations/log/`                         |
| 6, 7.2        | A retirement timestamped before the record it retires | `observations/log/`                         |
| 8             | A line an edit rewrote, and records a truncation deleted rather than retired | `observations/append/`      |

Two stated limits are checked in Go rather than as fixtures, because a fixture
for either would be twenty kilobytes of parentheses whose golden nobody could
read: the nesting bound of `sexpr.MaxDepth`, and the cap on how many
diagnostics one run retains.

**What is not covered, and why.** Specification section 2 also excludes a
boolean written outside the positions which take one. Which positions those are
is partly registry data — a check's parameters are declared by the check
registry — so the engine cannot decide it until that registry loads, and it
belongs with the layer which reads it. The references of section 6.9 which cross
between the families — `boundary`, written on a semantic node and naming a loop,
and `backed-by`, written on an edge and naming a semantic node — are answered by
the pass which has read both, which is why the fixtures for both are in
`boundary/`. The references written within one family are answered by the loader
of that family, because one pass reads both ends of each: `within` and
`member-of` in `node`, and `vertices` and `edges` in `topology`.

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
- Every line of an observation file is modified in turn, one at a time, and each
  has to come back from `ValidateAppendOnly` as a finding at that line. A golden
  says what one edit reports; this says that *no* edit goes unreported, which is
  the whole of what the append-only invariant is worth.

A test which only compares output against a recorded string can pass while that
output no longer reads back — a number which lost a digit, a string escaped so
that it reads back as different text, a comment which swallowed the datum after
it. Each of those produces stable, plausible, wrong bytes.
