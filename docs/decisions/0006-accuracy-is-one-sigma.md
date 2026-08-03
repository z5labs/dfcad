# 0006. Accuracy is 1σ, and systematic terms add linearly

**Status:** Accepted

## Context

Every measured value in the model carries an accuracy, and the accuracy is only useful if
two of them can be combined. The moment a query answers a question about a *derived*
quantity — the distance between two surveyed corners, the clearance between a wall face and
a setback line — it is combining the uncertainties of its inputs, and the answer is only as
trustworthy as the arithmetic that produced it.

Two things make that arithmetic go wrong, and both go wrong silently.

The first is the convention. An accuracy of `0.02 ft` means nothing until someone says
whether that is a standard deviation, a 95% figure, or a manufacturer's "typical". The
same instrument specification, quoted three ways, produces numbers differing by a factor of
two. If the model does not fix a convention, every value is in whichever convention its
author had in mind, and combining them is meaningless arithmetic on incompatible numbers.

The second is the combination rule. Combining everything in quadrature — root sum of
squares — is the familiar formula and it is right only for *independent* terms. Errors that
are shared between the inputs do not partially cancel; they add. Two corners located from
the same control point share that control point's error entirely. Treating the shared term
as independent makes the combined uncertainty look smaller than it is, by a factor that
grows with how much the inputs share, and the answer is confidently wrong in the
optimistic direction. That is the worst direction for a number that a setback check
depends on.

Counting the shared term twice is the mirror failure: it inflates the budget, checks start
failing that should pass, and the usual response is to widen a tolerance rather than to fix
the arithmetic.

## Decision

**Every accuracy in the model is a standard uncertainty: one standard deviation, k = 1.**
This is the storage convention, without exception. A figure quoted at any other coverage is
converted to 1σ at the point it enters the model, by whoever enters it, and the
conversion is part of the claim's provenance — not something the engine guesses.

**Each uncertainty term is classified as independent or systematic.** The classification
is a property of the term, carried with it, because whether an error is shared is a fact
about how the measurement was made and cannot be inferred from the number.

**Independent terms combine in quadrature.** For independent standard uncertainties
`u₁ … uₙ`, the combined contribution is `√(u₁² + … + uₙ²)`.

**Systematic terms shared between inputs combine linearly.** For shared terms `s₁ … sₘ`,
the contribution is `|s₁| + … + |sₘ|`, added to the quadrature result rather than folded
into it.

**A shared term is counted once.** When two inputs to a derivation carry the same
systematic term — the same control point, the same datum realisation, the same instrument
scale error — that term contributes once to the result, not once per input. Identifying it
as the same term is the point of carrying the classification; two terms are the same term
when they name the same source, not when they happen to have the same magnitude.

**Coverage factors are stated wherever a budget is widened.** Any output that presents an
uncertainty at other than 1σ says so, with the factor and the approximate confidence: `k =
2` (≈ 95% for a normal distribution), `k = 3` (≈ 99.7%). A widened number never appears
without its factor attached, and a widened number is never stored.

## Consequences

Uncertainties from different sources are commensurable. An instrument specification, a
survey report and a manufacturer's tolerance all arrive as 1σ figures and can be combined
without a per-source correction.

Derived answers are honest in the conservative direction. Where the engine cannot prove two
terms are independent, treating a shared term as shared makes the budget wider, not
narrower, and a check that fails on a wide budget prompts an investigation rather than a
surprise on site.

The error budget is itemised, not a single number. Because terms are carried separately
with their classification, a query can report *which* term dominates — and "the control
point is 80% of your budget" is a different and more useful answer than "±0.06 ft".

Storage stays canonical. Presentation is free to widen; the stored figure is always 1σ, so
the model never accumulates two conventions.

## Cost

Classification is a judgement someone has to make, per term, and the engine cannot check
it. A systematic term recorded as independent produces a budget that is too narrow, and
nothing detects it — the arithmetic is only as good as the honesty of the input.

Linear addition of systematic terms is deliberately conservative. It is the right answer
when terms are fully correlated and an overestimate when they are only partly correlated,
and this model has no way to express partial correlation. Budgets will sometimes be wider
than a full covariance treatment would give.

Quoting at 1σ is unfamiliar to some of the professions the data comes from. Survey and
instrument specifications are frequently published at 95%, so the conversion at the point
of entry is an extra step, and forgetting it silently doubles the figure.

## What would reverse it

A requirement to express partial correlation — a correlation coefficient between terms
rather than a binary independent/systematic classification — would replace the combination
rule with a covariance formulation. That is a genuine improvement in fidelity and a
significant increase in what an author has to supply, and it is the most likely reason to
revisit this.

Changing the storage convention from 1σ is the reversal to avoid. Every stored number would
change meaning without changing text: a `0.02` that meant one standard deviation would mean
95%, and the same file would produce budgets that differ by a factor of two with nothing in
the diff. There is no way to tell, from a stored value alone, which convention it was
written under, so a migration would have to be driven by the date of each claim rather than
by its content — which is to say, guessed. If the convention ever had to change, it would
be by a new, distinctly named field, with the old one left meaning what it always meant.
