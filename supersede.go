// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"iter"
	"strings"
)

// Replacement returns the claim which replaced claim, and whether the model
// holds one.
//
// It is one step along the chain rather than the end of it: the claim which
// replaced this one may itself be deprecated, which is what makes a supersession
// a chain rather than a pair. [Claims.Current] walks it to the end.
//
// A claim which was not deprecated has no replacement, and neither has a
// deprecated claim whose `superseded-by` names a claim this model does not hold
// — which is a load diagnostic rather than a state a caller has to interpret,
// and is reported as one.
func (c *Claims) Replacement(claim *Claim) (*Claim, bool) {
	if c == nil || claim == nil || claim.supersededBy == "" {
		return nil, false
	}
	return c.Claim(claim.supersededBy)
}

// Replaced iterates the claims claim replaced, in source order.
//
// It is the reverse of [Claims.Replacement], and there may be more than one:
// two estimates corrected by one survey are two claims naming that survey as
// what replaced them, and both of them are what the survey stands in place of.
//
// Only the claims which named this one directly come back. [Claims.History]
// walks the whole of what a claim replaced, through everything those claims
// replaced in their turn.
func (c *Claims) Replaced(claim *Claim) iter.Seq[*Claim] {
	return func(yield func(*Claim) bool) {
		if c == nil || claim == nil || claim.id == "" {
			return
		}

		for _, replaced := range c.superseded[claim.id] {
			// A claim which named itself is a load error rather than its own
			// predecessor, and yielding it here would make the history of every
			// claim after it infinite.
			if replaced == claim {
				continue
			}
			if !yield(replaced) {
				return
			}
		}
	}
}

// Current returns the claim at the end of the chain of replacements which
// begins at claim, and whether the chain reached one.
//
// A claim nothing replaced is its own answer, so asking this of a live claim
// gives that claim back. Asking it of a deprecated one follows the replacements
// as far as they go, which is how a value read out of an old report is traced
// forward to what the model says now.
//
// The second result is false where the chain does not end in a claim which is
// still asserted: a `superseded-by` naming a claim this model does not hold, a
// deprecation which named nothing, and a cycle each leave the walk with no
// current claim to return. Every one of them is a load diagnostic, so a caller
// which loaded clean never sees a false here.
func (c *Claims) Current(claim *Claim) (*Claim, bool) {
	if c == nil || claim == nil {
		return nil, false
	}

	// The walk cannot be trusted to terminate on its own. A cyclic supersession
	// is reported at load and the model is still returned, because a caller
	// reporting on a model wants to say what is in it as well as what is wrong
	// with it — and it is this traversal which would otherwise spin on one.
	seen := make(map[*Claim]bool)

	for {
		if seen[claim] {
			return nil, false
		}
		seen[claim] = true

		replacement, ok := c.Replacement(claim)
		if !ok {
			if claim.Rank() == RankDeprecated {
				return nil, false
			}
			return claim, true
		}

		claim = replacement
	}
}

// History iterates everything claim replaced, nearest first.
//
// It is [Claims.Replaced] followed all the way back: the claims which named
// this one, then the claims those replaced, and so on to the ones nothing
// stands behind. Together with [Claims.Current] it is what makes a supersession
// walkable in both directions — a current value traced back through every claim
// it replaced, and an old one traced forward to what the model says now.
//
// Each claim comes back once, in the order the claims which replaced it were
// written, depth first. A claim which was reached is not walked into twice, so
// a cyclic supersession — a load diagnostic, and still a model this can be
// asked of — terminates here rather than spinning.
func (c *Claims) History(claim *Claim) iter.Seq[*Claim] {
	return func(yield func(*Claim) bool) {
		if c == nil || claim == nil {
			return
		}

		seen := map[*Claim]bool{claim: true}

		var walk func(*Claim) bool
		walk = func(claim *Claim) bool {
			for replaced := range c.Replaced(claim) {
				if seen[replaced] {
					continue
				}
				seen[replaced] = true

				if !yield(replaced) || !walk(replaced) {
					return false
				}
			}
			return true
		}

		walk(claim)
	}
}

// link indexes the supersessions the claims carry by the claim each names, which
// is what makes the chain walkable backwards.
//
// The forward direction is written on the claim, so it needs no index. The
// backward direction is not written anywhere, and computing it by scanning every
// claim per question would make walking a chain quadratic in the size of the
// model.
func (c *Claims) link() {
	if c == nil {
		return
	}

	c.superseded = make(map[ID][]*Claim)
	for _, claim := range c.inOrder {
		if claim.supersededBy == "" {
			continue
		}
		c.superseded[claim.supersededBy] = append(c.superseded[claim.supersededBy], claim)
	}
}

// supersession is how one claim was retracted, as it was written: where the
// deprecation and the replacement were spelled, and where the claim itself is
// named.
//
// The spans are what the diagnostics point at, and a [Claim] carries none of
// them: it holds the rank and the id of its replacement, which is what a caller
// reads, rather than the positions of the two children they were written in.
type supersession struct {
	// claim is the claim which was written.
	claim *Claim

	// rank is where the (rank ...) child was written, and the zero span where
	// none was.
	rank Span

	// named is where the whole (superseded-by ...) child was written, and the
	// zero span where none was. It is not the same question as whether the
	// claim carries a replacement: an argument which is not an id is written
	// and does not read.
	named Span

	// at is where the argument of that child was written, which is what a
	// diagnostic about the claim it names points at.
	at Span

	// where the claim itself is named — its own id where it wrote one, and the
	// predicate it was written under where it did not. It is the other end a
	// diagnostic about the deprecation points back at.
	where Span
}

// retraction reads how a claim was retracted and records it for the pass which
// checks that the retraction holds together.
//
// The reference to the replacement is recorded alongside the references
// everything else makes to a claim, so that a `superseded-by` naming a claim
// nothing carries is reported in the one wording that failure has. A second
// wording of it here would be a second sentence about one mistake, and the two
// would drift.
func (l *claimLoader) retraction(claim *Claim, form *Node, id Span) {
	written := supersession{claim: claim, where: id}
	if written.where == (Span{}) {
		written.where = tagSpan(form)
	}

	if child, ok := childForm(form, "rank"); ok {
		written.rank = child.Span
	}

	if child, ok := childForm(form, "superseded-by"); ok {
		written.named = child.Span

		if arg, ok := argument(child, 0); ok {
			written.at = arg.Span

			if superseded, ok := l.id(arg, "a claim id"); ok {
				claim.supersededBy = superseded
				l.registered(l.registry, superseded, arg.Span)

				l.references = append(l.references, claimReference{
					id:   superseded,
					at:   arg.Span,
					by:   written.where,
					what: "the deprecation of " + claimName(claim),
				})
			}
		}
	}

	l.supersessions = append(l.supersessions, written)
}

// supersede checks that every retraction the tree wrote holds together: that a
// deprecation names what replaced it, that only a deprecation names one, and
// that following the replacements terminates.
//
// Requiring a replacement is the whole of what keeps `deprecated` from being a
// delete button ([0007](docs/decisions/0007-rank-is-closed.md)). A claim which
// was believed and then corrected is a record of why the number changed, and
// deleting it — or retracting it with nothing standing in its place — destroys
// exactly that.
//
// Whether the claim a retraction names exists at all is answered beside every
// other reference to a claim, in [claimLoader.resolve].
func (l *claimLoader) supersede() {
	l.claims.link()

	for _, written := range l.supersessions {
		l.retracted(written)
	}

	l.cycles()
}

// retracted checks one written retraction against the claim it was written on.
func (l *claimLoader) retracted(written supersession) {
	claim := written.claim
	deprecated := claim.Rank() == RankDeprecated

	switch {
	case deprecated && written.named == (Span{}):
		at := written.rank
		if at == (Span{}) {
			at = claim.Span()
		}

		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     at,
			Message: fmt.Sprintf(
				"expected the claim which replaced %s, found a deprecation naming none",
				claimName(claim),
			),
			Hint:    "a deprecated claim carries (superseded-by <claim-id>) naming what replaced it, which is what keeps deprecated from being a delete",
			Related: []RelatedLocation{{Span: written.where, Message: "the deprecated claim is written here"}},
		})

	case !deprecated && written.named != (Span{}):
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     written.named,
			Message: fmt.Sprintf(
				"expected a replacement only where a claim was deprecated, found one on %s, which is of rank %s",
				claimName(claim), claim.Rank(),
			),
			Hint:    "(superseded-by ...) says what stands in place of a retracted claim; a claim still asserted has nothing standing in place of it",
			Related: []RelatedLocation{{Span: written.where, Message: "the claim is written here"}},
		})
	}

	if claim.id == "" || claim.supersededBy != claim.id {
		return
	}

	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     written.at,
		Message: fmt.Sprintf(
			"expected the claim which replaced %s, found %s, which is the claim itself",
			claimName(claim), claim.supersededBy,
		),
		Hint: "a claim is replaced by a later one; a claim replaced by itself is retracted with nothing standing in its place",
	})
}

// cycles reports every ring of claims which supersede one another.
//
// A supersession chain is walked forward by following one reference per claim,
// so the claims form a graph in which every claim has at most one successor.
// Walking from each claim in turn and remembering the path is therefore the
// whole of finding a cycle: the walk either leaves the path, reaches a claim an
// earlier walk already accounted for, or meets itself.
//
// Each ring is reported once, whichever of its claims the walk reached first,
// and is named from the claim of it which was written first. What is reported
// is a property of the model rather than of the order the walk happened to take.
func (l *claimLoader) cycles() {
	written := make(map[*Claim]supersession, len(l.supersessions))
	for _, s := range l.supersessions {
		written[s.claim] = s
	}

	order := make(map[*Claim]int, len(l.claims.inOrder))
	for i, claim := range l.claims.inOrder {
		order[claim] = i
	}

	walked := make(map[*Claim]bool)
	for _, start := range l.claims.inOrder {
		if walked[start] {
			continue
		}

		var path []*Claim
		reached := make(map[*Claim]int)

		for claim := start; !walked[claim]; {
			if at, ok := reached[claim]; ok {
				l.cycle(ring(path[at:], order), written)
				break
			}

			reached[claim] = len(path)
			path = append(path, claim)

			// A claim which named itself is reported as that rather than as a
			// cycle of one, which says the same thing in more words.
			replacement, ok := l.claims.Replacement(claim)
			if !ok || replacement == claim {
				break
			}
			claim = replacement
		}

		for _, claim := range path {
			walked[claim] = true
		}
	}
}

// ring puts the claims of one cycle in the order they are reported: the claim
// written first, and then the rest of the ring in the order the supersessions
// lead through it.
func ring(cycle []*Claim, order map[*Claim]int) []*Claim {
	first := 0
	for i, claim := range cycle {
		if order[claim] < order[cycle[first]] {
			first = i
		}
	}

	return append(append([]*Claim{}, cycle[first:]...), cycle[:first]...)
}

// cycle reports one ring of claims which supersede one another.
func (l *claimLoader) cycle(ring []*Claim, written map[*Claim]supersession) {
	names := make([]string, 0, len(ring)+1)
	for _, claim := range ring {
		names = append(names, claimName(claim))
	}
	names = append(names, names[0])

	var related []RelatedLocation
	for _, claim := range ring[1:] {
		related = append(related, RelatedLocation{
			Span:    written[claim].at,
			Message: fmt.Sprintf("%s names the next claim of the cycle here", claimName(claim)),
		})
	}

	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     written[ring[0]].at,
		Message: fmt.Sprintf(
			"expected a supersession which ends in a claim nothing replaced, found a cycle: %s",
			strings.Join(names, ", then "),
		),
		Hint:    "every deprecated claim is replaced by a later one, so following the replacements has to reach a claim which is still asserted",
		Related: related,
	})
}

// claimName names a claim for a diagnostic.
//
// A claim id is optional, so a claim is named by the id it wrote where it wrote
// one and by what it says about what where it did not. Neither reading is a
// position: every diagnostic which uses this carries the span as well, because
// two claims can be named identically and only one of them is the one being
// reported on.
func claimName(claim *Claim) string {
	if id, ok := claim.ID(); ok {
		return string(id)
	}

	if claim.Subject() == "" {
		return "the " + claim.Predicate() + " claim"
	}

	return fmt.Sprintf("the %s of %s", claim.Predicate(), claim.Subject())
}
