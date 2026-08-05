# A siting query, worked end to end

This is one question — *does this building fit on this plot?* — followed from the claims it
is answered from to the budget it comes back with, and then asked again after one of those
claims is replaced by a better measurement.

It is the question the rest of the engine exists to make answerable, and the only one that
touches every part of it at once: two families of node, two coordinate frames, the claim that
measures one frame against the other, an offset, an overlay, a measurement, and an error
budget over all of it. Everything below runs against
[`testdata/siting/surveyed`](../testdata/siting/surveyed), which is checked in.

## The model

Two grids. `frame:site` is the site survey grid and is the root. `frame:building` is the
grid the building was set out on, and it reaches the site through one claim:

```
(frame frame:building
  (label "Building local grid")
  (unit m)
  (parent frame:site)
  (transform survey:C-0001)
  (frame-transform
    (id survey:C-0001)
    (value
      (transform
        (translation 5.0 4.0 0.0)
        (rotation 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0)
        (scale 1.0)))
    (source "Georeferencing report GR-2026-002, Acme Surveys")
    (method method:gnss-static)
    (accuracy (independent 0.012 m) (systematic 0.008 m control:CP-1) (systematic 0.005 m control:CP-2))
    (date "2026-02-11")))
```

That is the georeference, and it is a claim like every other measurement in the model rather
than a configuration constant: it has a source, a method, a date, and — the part everything
below turns on — an accuracy in three terms. Twelve millimetres of random error in the fit
itself, eight of shared error at `control:CP-1`, five at `control:CP-2`.

The buildable area is thirty metres by twenty, surveyed on the site grid. Each of its corners
is a claim:

```
(vertex geom:V-01
  (label "Buildable area, south-west corner")
  (frame frame:site)
  (position
    (id survey:P-0001)
    (value (0.0 0.0 0.0) m)
    (source "Boundary survey BS-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m) (systematic 0.008 m control:CP-1))
    (date "2026-03-11")))
```

The proposed footprint is ten by eight, set out from the building grid origin, and its
corners are claims of the same shape — four millimetres of instrument noise apiece rather
than three, and the same `control:CP-1` behind them:

```
(vertex geom:V-11
  (label "Block A footprint, south-west corner")
  (frame frame:building)
  (position
    (id survey:P-0011)
    (value (0.0 0.0 0.0) m)
    (source "Interior control set IC-2026-03, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.003 m) (systematic 0.008 m control:CP-1))
    (date "2026-04-08")))
```

Nine claims in all — four corners on each side and the fit between the grids — and
`control:CP-1` is behind every one of them. That is not a contrivance. A project ties its
boundary survey, its interior control and its georeference to the same control point as a
matter of routine, and the consequence is the whole reason this document exists.

## The question

```sh
dfcad site --root testdata/siting/surveyed \
  --within plan:B-01 \
  --position position \
  --tolerance boundary-closure \
  plan:S-01
```

Neither predicate nor the tolerance has a default. Which predicate carries a position, and
how close two corners have to be to be one corner, are things this project wrote down
([0012](decisions/0012-tolerances-are-registry-data.md)); a value compiled into the command
would be the engine deciding one of them on the project's behalf.

## What the query does

1. **Reads both outlines.** Each node's boundary loops are assembled into a plane figure from
   the position claims of its corners, judged against the declared tolerance. Each figure
   carries the accuracy of the claims it was read from.
2. **Carries the footprint into the site frame.** The two are in different frames, so they
   are not comparable until one of them moves — and moving it is an explicit step, because
   the transform between two frames is a measurement with an accuracy of its own and not a
   detail an operation should apply behind a caller's back. The accuracy of `survey:C-0001`
   joins the footprint's budget here.
3. **Offsets by the required clearance,** where one was required. `--clearance 1.5` grows the
   footprint by a metre and a half all round, corners rounded to the declared tolerance, and
   what the envelope has to accommodate is that shape rather than the footprint.
4. **Overlays.** What the two have in common is the intersection; what the footprint needs and
   the plot does not offer is the difference, and it is empty for a proposal that fits.
5. **Measures.** The clearance is the shortest distance between the two boundaries, taken at
   the tightest point — the loosest point or the average would pass a structure that touches
   at one corner.
6. **Combines the budget** and decides the verdict against it.

## The answer

```
plan:S-01 in plan:B-01: fits, clearance 4.0 m, known to 0.0203224… m (k = 1.0, ≈ 68%)
```

Four metres of clearance, known to twenty millimetres at one standard uncertainty. On stdout,
the same answer as an object a pipeline can branch on — `"verdict": "fits"`, `"decided":
true`, `clearance.actual` 4, `clearance.uncertainty.magnitude` 0.0203224…

## The budget, term by term

`dfcad site --format human -v` writes the terms under the summary:

```
  independent survey:P-0011: 0.003 m from 1 claim
  systematic control:CP-1: 0.008 m from 9 claims, counted once
  independent survey:P-0012: 0.003 m from 1 claim
  independent survey:P-0013: 0.003 m from 1 claim
  independent survey:P-0014: 0.003 m from 1 claim
  independent survey:C-0001: 0.012 m from 1 claim
  systematic control:CP-2: 0.005 m from 1 claim
  independent survey:P-0001: 0.004 m from 1 claim
  independent survey:P-0002: 0.004 m from 1 claim
  independent survey:P-0003: 0.004 m from 1 claim
  independent survey:P-0004: 0.004 m from 1 claim
```

Eleven terms from nine claims. Every one of them is named, and every one names the claims
that put it there — which is what makes a budget something to act on. "±0.02 m" is an answer
nobody can do anything about; "the georeference fit is the largest single term, and here is
the claim it came from" says what to re-measure.

Note the second line. `control:CP-1` was contributed by four footprint corners, four boundary
corners and the georeference — **nine claims, one term.** It appears in the arithmetic once,
and all nine are still named beside it.

### How the terms combine

Independent terms go in quadrature; systematic terms add linearly; the two combine in
quadrature ([0006](decisions/0006-accuracy-is-one-sigma.md)):

```
u = √( Σ uᵢ² + ( Σ |sⱼ| )² )

Σ uᵢ² = 4×0.003² + 0.012² + 4×0.004² = 0.000244 m²
Σ |sⱼ| = 0.008 + 0.005                = 0.013 m

u = √( 0.000244 + 0.013² ) = √0.000413 = 0.020322 m
```

The naive answer — everything in quadrature, which is what you get by treating the shared
terms as though they were noise — is:

```
√( 0.000244 + 0.008² + 0.005² ) = √0.000333 = 0.018248 m
```

Ten per cent narrower, and **narrower is the direction nobody investigates.** A check that
passes is a check nobody reads. That is why the difference between the two figures is asserted
by a test of its own (`TestFitWithinIsNotQuadrature`): the terms would be the same terms and
the attribution the same attribution, and only the one number somebody acts on would be
wrong.

### Why the systematic terms do not cancel

The georeference is *one* transform applied to every fact declared indoors. If the fit is
eight millimetres north of the truth, every interior corner is eight millimetres north of the
truth — together, in the same direction. Two interior points do not cancel that error against
each other, and an interior point compared with an exterior one does not average it away.
Combining it in quadrature assumes exactly the cancellation that cannot happen, and it
assumes it worst in the case that motivates asking the question in the first place.

Counting it once matters as much in the other direction. Nine copies of 0.008 m added
linearly would give a combined figure of 0.075 m — nearly four times the truth — and the usual
response to a budget that wide is to widen a tolerance rather than to fix the arithmetic.

## The verdict, and why it is not the clearance

Four metres against twenty millimetres is not a close call, so the verdict is `fits`. The
verdict exists for the case that is:

```sh
dfcad site --root testdata/siting/surveyed --within plan:B-01 \
  --position position --tolerance boundary-closure plan:S-03
```

```
plan:S-03 in plan:B-01: might-fit, clearance 0.00999… m, known to 0.0203224… m (k = 1.0, ≈ 68%)
```

Ten millimetres of daylight, and the answer is known to twenty. The clearance is positive and
means nothing: the model as measured cannot tell. `might-fit` is neither a pass rounded down
nor a failure rounded up, and `"decided"` is `false` — what to do about it is to re-measure
whatever dominates the budget, which here is the georeference.

The fourth verdict is the one that is not a measurement problem at all:

```
plan:S-05 in plan:B-01: unknown, clearance 1.0 m, uncertainty unknown
```

`plan:S-05` is set out on a third grid whose fit to the site nobody stated an accuracy for. A
metre of clearance buys no confidence at all, because an unstated accuracy is *unknown* rather
than nought — reading it as nought would let a measurement nobody made pass through and come
out looking like the most accurate input the query had. The clearance is still reported; only
the verdict is withheld, and the budget names the claim that has to say something.

## Asking again after a re-survey

[`testdata/siting/resurveyed`](../testdata/siting/resurveyed) is the same model with the
georeference re-fitted and **nothing else changed** — the two `model.dfc` files are
byte-identical, and a test asserts it. The building lands twenty millimetres further north,
and the random part of the fit drops from twelve millimetres to two:

```
(accuracy (independent 0.002 m) (systematic 0.008 m control:CP-1) (systematic 0.005 m control:CP-2))
```

Both halves of the answer move:

```
plan:S-01 in plan:B-01: fits, clearance 4.02 m, known to 0.0165227… m (k = 1.0, ≈ 68%)
```

Nothing was edited to make that happen, and nothing could have been: the fit is recomputed
from the claims every time it is asked for and is never written back
([0009](decisions/0009-derived-values-are-never-written-back.md)). A clearance authored as a
number somewhere would be the stale one from this morning.

What did *not* move is the systematic part. Re-occupying the same control point with a better
instrument does not make the control point's own error smaller, so `control:CP-1` is still
0.008 m — and it is now the largest term in the budget. The advice the answer gives has
changed with it: re-running the georeference again would buy almost nothing, and the next
improvement has to come from the control network.

## From the library

The same query, without the command line:

```go
registry, _ := dfcad.LoadRegistry(root)
nodes, _ := dfcad.LoadNodes(root, registry)
topology, _ := dfcad.LoadTopology(root, registry)
claims, _ := dfcad.LoadClaims(root, registry)
boundaries, _ := dfcad.ResolveBoundaries(nodes, topology)
frames, _ := dfcad.ResolveFrames(registry, claims)

survey := dfcad.Survey{Tolerance: "boundary-closure", Registry: registry}
for vertex := range topology.Vertices() {
    resolution, _ := claims.Resolve(vertex.ID(), "position", registry)
    survey.Place(vertex.ID(), resolution)
}

footprint, _ := nodes.Node("plan:S-01")
buildable, _ := nodes.Node("plan:B-01")

answer, diags := topology.FitWithin(footprint, buildable, boundaries, survey, dfcad.Siting{
    Frames: frames,
})
```

`ExampleTopology_FitWithin` in `example_test.go` is this, runnable, with its output checked on
every build.

## See also

- [`site`](machine-output.md#site) — the object the command writes, field by field.
- [0006](decisions/0006-accuracy-is-one-sigma.md) — why an accuracy is one sigma, why an
  unstated one is unknown rather than nought, and how the terms combine.
- [0005](decisions/0005-one-linear-unit-per-frame.md) — why nothing here converts between
  units, and why a frame declares exactly one.
- [0009](decisions/0009-derived-values-are-never-written-back.md) — why the answer is
  recomputed rather than stored.
