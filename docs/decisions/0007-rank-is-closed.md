# 0007. Rank is closed: normal and deprecated

**Status:** Accepted

## Context

More than one claim can be made about the same property of the same thing. A wall's
thickness is stated on the design drawing, measured on site, and stated again on the
as-built. Two of those disagree. That disagreement is the most valuable thing in the model
— it is a design that was not built as drawn, or a measurement that was taken wrongly, and
either way somebody needs to know.

The engine resolves such a set into a single answer deterministically, from what the claims
carry: their accuracy, their method, their date, their source. It also exposes the
disagreement itself, as a conflict register — a traversal that lists every place where
claims disagree by more than their combined uncertainty allows.

The obvious extra tool is a `preferred` rank: a way for an author to say "use this one" when
the resolution rule picks the wrong claim. It is obvious, it feels harmless, and it is the
mechanism by which the conflict register goes quiet.

The failure runs like this. The register lists a conflict. Investigating it properly means
finding out which measurement is wrong and why, which is work and may involve going back to
site. Marking one claim `preferred` takes five seconds, produces exactly the same
resolved answer as the investigation would have if the guess was right, and removes the
entry from the register. Under any deadline at all, the second option wins. After a year of
this, the register is empty — not because the model is consistent, but because every
inconsistency in it has been individually silenced by someone in a hurry, and the record of
which ones were silenced is spread across a year of commits.

The disagreement is still in the data. What is gone is the engine's ability to point at it.

## Decision

**Rank is a closed set with exactly two members: `normal` and `deprecated`.**

`normal` is every claim that is currently asserted. `deprecated` is a claim that is
asserted to be *wrong* — retracted, not out-ranked.

**There is no `preferred`, and no numeric priority, weight or override.** A claim cannot be
promoted above its peers. Where two `normal` claims disagree, resolution decides between
them on their recorded merits (see
[0006](./0006-accuracy-is-one-sigma.md) for the accuracy that feeds that), and the
disagreement stays in the conflict register until one of them is deprecated or corrected.

**Deprecation is a factual statement, not a preference.** It says this claim is not true —
the value was mistyped, the instrument was miscalibrated, the drawing it came from was
superseded — and it carries the reasoning for that. It is the same shape of act as
retiring an id (see [0002](./0002-immutable-id-mutable-label.md)): the claim stays in the
file, visible, with its rank changed.

## Consequences

The conflict register stays honest. It is quiet only when the claims genuinely agree, so
an empty register is evidence rather than an artefact of housekeeping, and a growing
register is a real signal about the state of the model.

Silencing a conflict requires asserting something falsifiable. Deprecating a claim states
that it is wrong, in the file, attributable, and reviewable in the diff — as opposed to
`preferred`, which states nothing at all and is therefore never wrong.

Resolution is a pure function of the claims. Nothing in the resolution path consults an
authored override, which means the rule can be tested exhaustively and the same claim set
always produces the same answer.

The distinction between "this measurement is wrong" and "this measurement is out of date"
has to be modelled as what it is — a date, a source, a method — rather than collapsed into
a priority number.

## Cost

There is no escape hatch. When resolution picks the claim a human believes is the worse
one, the only remedies are to fix the claims — supply the accuracy that was omitted,
correct the date, deprecate the one that is actually wrong — or to accept the answer. That
is more work than typing `preferred`, and it is more work at exactly the moment when
somebody is least willing to do it.

The resolution rule therefore has to be good, and it has to stay good, because nothing
patches over it locally. A deficiency in the rule is felt across the whole model at once
rather than being worked around case by case, which is the intended pressure but is a real
cost when the deficiency is discovered late.

Deprecation also demands a judgement that is sometimes not available. "One of these two is
wrong and I do not know which" has no expression: both stay `normal`, and the conflict
stays open. That is the correct state of the world, and it is uncomfortable to leave in a
model somebody wants to sign off.

## What would reverse it

Evidence that the conflict register is being suppressed anyway, through some other
mechanism — claims deleted rather than deprecated, accuracies inflated to make a
disagreement fall inside the combined budget — would show that the pressure found a
different outlet, and a rank with an explicit, reviewable override might be the more honest
design.

Adding a rank is a one-line change to a closed set and is close to irreversible in
practice. Once `preferred` exists, it is used, and removing it later means revisiting every
claim that carries it and answering the question that was skipped at the time — which of
these is actually wrong — with a year less context than whoever skipped it had. The data
does not contain the answer; the flag exists precisely because nobody worked it out.
