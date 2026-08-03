# 0012. Tolerances are registry data

**Status:** Accepted

## Context

Geometry cannot be compared exactly. Two vertices are the same vertex when they are close
enough; an edge is horizontal when its slope is small enough; a loop closes when its last
point is near enough to its first. Every one of those needs a number, and the number has to
come from somewhere.

The path of least resistance is a literal in the code. `if dist < 1e-9` gets written while
implementing coincidence detection, because at that moment the number feels like an
implementation detail — a guard against floating-point noise, not a modelling decision. The
same author, an hour later, writes `if dist < 0.001` in the snapping code, because a
nanometre is obviously too tight for snapping surveyed points.

Now the engine has two tolerances, they disagree, neither is written down, and nothing says
which one applies where. The failure that follows is the characteristic one: two vertices
that snapping considered identical are considered distinct by coincidence detection, and
the model contains a hairline crack that no diagnostic mentions. Finding it means reading
the engine's source and comparing constants.

The deeper problem is that a tolerance is not a property of an algorithm. It is a judgement
about measurement quality. "Points within 5 mm are the same point" is a statement about the
survey equipment, the field procedure and the accuracy the model is willing to claim — the
same statement the accuracy on a claim makes ([0006](./0006-accuracy-is-one-sigma.md)). A
site surveyed with a total station and a site digitised from an aerial photograph do not
have the same answer, and neither of them has an answer that belongs to the intersection
routine. A number that encodes a judgement about data, living in code, is domain vocabulary
smuggled past the boundary in [0010](./0010-the-engine-carries-no-domain-vocabulary.md) —
and it is the easiest kind to smuggle, because it does not look like vocabulary at all.

Literals also make the engine untestable at its edges. A test that wants to know what
happens just inside and just outside the coincidence threshold has to know the threshold,
which means either duplicating the literal in the test or reaching into the package to read
it. Both make the test a restatement of the implementation.

## Decision

**Tolerances are named entries in a registry supplied by the consuming repository.** Each
entry declares a name, a value and a unit. Nothing else — a tolerance is a magnitude, and
the engine attaches no other meaning to it.

**No numeric literal tolerance appears in engine code.** No comparison against a hard-coded
epsilon, no default that applies when a tolerance is missing, no fallback constant. An
operation that needs a tolerance takes it as a named parameter, and the caller supplies the
name.

**An operation that needs a tolerance and is not given one is an error, not a guess.**
Referring to an unregistered tolerance is a load error with a position. There is no
implicit tolerance anywhere in the engine, because an implicit tolerance is a judgement
about someone else's data made by whoever wrote the algorithm.

**A tolerance carries a unit and is compared in a frame.** Comparing a length against a
tolerance requires both to be in the same linear unit, which
[0005](./0005-one-linear-unit-per-frame.md) makes checkable: the frame declares the unit,
the tolerance declares its own, and a mismatch is caught rather than silently scaled.

**Every result computed against a tolerance says which tolerance it used.** A snap, a
coincidence, a containment decision at a boundary — each reports the named tolerance that
produced it, so a surprising answer can be traced to the number that caused it without
reading engine source.

This decision is about the numbers that express measurement judgement. It does not reach
the numerical guards internal to a single algorithm — a degeneracy test in a solver, a
convergence bound — which are properties of the arithmetic and carry no statement about the
data. The distinction is whether changing the number changes what the model means. If it
does, it is a tolerance and it belongs in the registry.

## Consequences

The tolerances a model uses are in one file, with their units, readable by the person who
knows how the data was collected. Changing one is a reviewed change to that file, and its
diff says exactly what changed and by how much.

Two operations that should agree can be made to agree, by naming the same tolerance. Two
that should differ can differ, visibly, because both names are in the same file.

Re-running a model at a different tolerance is a registry edit, not a rebuild. That makes
sensitivity a thing anyone can check: tighten the coincidence tolerance, re-run the checks,
and see what stops holding.

The engine's tests declare their own tolerances like any other consumer, so a test for
behaviour at a threshold states the threshold in the test rather than importing it from the
implementation.

A model that has not declared a tolerance cannot silently get someone else's. It gets a
diagnostic naming the tolerance the operation wanted.

## Cost

Setup burden. A repository cannot run its first geometric query until it has declared
tolerances, and the person doing that at the start of a project is exactly the person least
equipped to choose the numbers. The engine cannot help them, because helping would mean
having a default.

Threading. A tolerance name has to reach every operation that needs one, through queries,
checks and authoring commands, which is parameters in signatures and fields in output that
would not exist if a constant sat in the comparison.

Verbosity at the call site. `coincident(a, b, tol)` where `tol` names a registry entry is
more to write and more to explain than `coincident(a, b)`, and every caller has to make a
choice it would rather not think about.

The judgement does not go away, it moves. A registry with a badly-chosen tolerance produces
wrong answers just as readily as a badly-chosen literal; the gain is that the number is
visible and attributable, not that it is right.

## What would reverse it

Evidence that a particular tolerance is genuinely universal — the same value correct for
every plausible model, independent of how the data was collected — would argue for
compiling that one in. That argument is weaker than it looks: a value that never varies
costs one registry line to declare, and declaring it keeps the "no implicit tolerance"
property intact.

Adding a default is the reversal that cannot be undone. The moment an operation has a
fallback, models start relying on it without declaring anything, and their results become
untraceable: nothing in the model or its output records which number produced which answer.
Removing the default later means every such model changes behaviour at once, with no way to
tell in advance which of its results move — and the ones that move quietly, without failing
a check, are the ones nobody finds.
