# 0011. Assertions are named parameterised checks, not expressions

**Status:** Accepted

## Context

A model is supposed to hold properties: every space is inside exactly one storey, no two
vertices in a frame are closer than the coincidence tolerance, every element of a given
type carries a fire-rating claim. Writing those down so CI enforces them is the point of
the assertion layer.

There are two ways to let an author express one. The first is an expression language:
give them a small language with comparison, boolean logic and traversal, and let them write
whatever predicate they need. It is enormously appealing. Nobody has to wait for the engine
to grow a check, the expressiveness is unbounded, and the first ten assertions are easy to
write.

The second is a closed registry of named, parameterised checks: `unique-within`,
`required-claim`, `containment-cardinality`, each with declared parameters, each
implemented in the engine.

The expression language loses the bet the whole model rests on, and it loses it through a
side entrance. The engine is declarative: the value of a declarative model is that a tool
can know what it says without running it. Queries can be answered, conflicts can be
enumerated, and a change can be reasoned about statically. An assertion written as an
expression breaks that property for assertions specifically — to know what
`forall e in elements: e.claims.height.value > 0 or e.type == "x"` constrains, you must
evaluate it. Not read it: evaluate it, against a model.

Everything downstream of "what does this assertion constrain?" then becomes impossible.
The engine cannot tell an author which assertions bear on the node they are editing. It
cannot run only the checks a diff could have affected, because it cannot tell which those
are. It cannot explain a failure in terms of the model rather than in terms of the
expression that failed. A documentation page listing the model's invariants has to print
source code.

The failure mode after that is well known. The expression language acquires a function,
then a way to reuse a fragment, then a way to define a fragment somewhere else, and at some
point it is a programming language embedded in a data format, with no debugger, no tests of
its own, and semantics that exist only in the evaluator.

## Decision

**Assertions are named checks drawn from a closed registry.** An assertion names a check
and supplies its parameters. It contains no expression, no operators, no traversal syntax
and no user-defined logic.

**The check registry is closed and compiled into the engine.** Each check declares its
name, its parameters and their types, what it means, and what its failure diagnostic says.
Adding a check is an engine change, reviewed and released like any other.

**A check is general or it does not belong in the engine.** A check that only makes sense
for one domain is domain vocabulary, and by [0010](./0010-the-engine-carries-no-domain-vocabulary.md)
it does not live here. What lives here is the general form — "every node of this type
carries a claim for this predicate" — which a model parameterises with its own type and
predicate. The parameters are registry references; the check is not.

**A parameterised assertion is inspectable without evaluation.** From the assertion alone,
the engine can say which check it is, which types, predicates, frames or tolerances it
refers to, and therefore which parts of the model it constrains — before running anything.

## Consequences

The set of properties a model claims to hold is enumerable. It can be listed, indexed by
the nodes it touches, and printed as documentation, because each entry is structured data
rather than code.

Checks can run against only what changed. Knowing statically which assertions could be
affected by a diff is what makes a diff-aware check run possible at all, and that is the
difference between a check suite that runs on every pull request and one that gets disabled
for being slow.

Failure diagnostics are written once, by the person implementing the check, with the
check's semantics in hand. They can name the parameter that was violated and the node that
violated it, rather than reporting that an expression evaluated false.

Checks are tested. Each one is engine code with its own table-driven tests, so a check's
behaviour at its edges is pinned down once rather than rediscovered by every model that
uses it.

The registry is a bottleneck by design. A model that needs a property no check expresses
files an issue, and the check arrives for everyone or does not arrive at all — which is the
review step that keeps the set small and general.

## Cost

Waiting. An author who needs a property the registry does not cover cannot express it
today. They wait for an engine change, and if the property is genuinely specific to their
model, the answer may be that it will not be added.

The registry will not cover everything, and some real invariants will go unchecked. A
closed set always has a boundary, and properties just outside it are properties nobody is
enforcing.

Parameterisation is harder to design than an expression language is to implement. Finding
the general form of a check — the version that is useful to models that do not share a
domain — takes judgement, and a check parameterised badly is worse than none, because it
looks like coverage.

There is a real risk of registry sprawl in the other direction: a long tail of narrow
checks, each added for one requester, collectively amounting to a badly-designed expression
language spelled out in names. Keeping the set small is an ongoing cost of review, not a
property the decision provides for free.

## What would reverse it

A sustained pattern of legitimate assertions that cannot be expressed as any general
parameterised check — not "would be easier as an expression", but genuinely inexpressible —
would be evidence the registry model is too narrow. The first response to that is a more
capable parameter vocabulary (predicate sets, quantified path parameters), which preserves
static inspectability. An expression language is the last resort, not the first.

If it ever were adopted, the property that must not be given up is static inspectability:
any such language would have to be restricted enough that the engine can still extract, by
inspection, the set of nodes and predicates an assertion constrains. A Turing-complete
evaluator cannot offer that.

Reversing this by opening the set is close to permanent. Once expressions exist in model
files, every one of them has to be read and reclassified by hand to get back to named
checks, and the ones that turn out to be inexpressible have no home. The intent behind a
complicated predicate lives in the head of whoever wrote it, and by then they have moved on.
