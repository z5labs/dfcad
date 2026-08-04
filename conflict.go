// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"cmp"
	"iter"
	"slices"
)

// Conflict is one subject and predicate about which the model holds more than
// one live claim, together with what resolution makes of them.
//
// A conflict is not a state anybody records. It is the condition of a pair
// carrying more than one non-deprecated value, which is why the register is a
// traversal: computed, it is always current, where a stored flag is current
// until somebody forgets to clear it
// ([0007](docs/decisions/0007-rank-is-closed.md)).
//
// The entry is a value rather than a reference into the model so that a caller
// holding one is holding what was true of the claims when it asked, and nothing
// it does to the entry reaches the claims.
type Conflict struct {
	// claims are every live claim of the pair, in source order.
	claims []*Claim

	// resolution is what the resolution rule makes of those claims, which is
	// what says whether the disagreement has an answer.
	resolution Resolution
}

// Subject returns the id of the thing the competing claims are about.
func (c Conflict) Subject() ID { return c.resolution.Subject() }

// Predicate returns the predicate they were written under.
func (c Conflict) Predicate() string { return c.resolution.Predicate() }

// Claims returns every live claim of the pair, in source order.
//
// It is every competing claim and not only the ones which could still win,
// because the register exists to show the disagreement rather than the answer:
// a claim carrying no accuracy is out-ranked by one that does, and it is still
// a second statement somebody wrote about the same thing.
// [Conflict.Resolution] narrows them to the candidates.
//
// Each claim carries the evidence for itself — its value, accuracy, date,
// source and method — so an entry can be reported without a second lookup. The
// slice is a copy of the entry's own, so re-ordering it re-orders nothing.
func (c Conflict) Claims() []*Claim { return slices.Clone(c.claims) }

// Resolution returns what the resolution rule makes of the competing claims:
// which one wins, or which ones are equally current.
func (c Conflict) Resolution() Resolution { return c.resolution }

// Resolved reports whether resolution picks a winner among the competing
// claims.
func (c Conflict) Resolved() bool { return c.resolution.Resolved() }

// Ambiguous reports whether the competing claims are equally current, so that
// resolution picks nothing.
//
// Exactly one of [Conflict.Resolved] and Ambiguous holds of any entry. A pair
// with more than one live claim either has a best one or does not, and there is
// no third answer to report.
func (c Conflict) Ambiguous() bool { return c.resolution.Ambiguous() }

// Conflicts iterates the conflict register: every subject and predicate pair
// carrying more than one non-deprecated claim, in a deterministic order.
//
// The register is computed by walking the claims, every time it is asked for.
// Nothing about it is written in a file, stored on a claim or cached here, so
// it cannot go quiet while the disagreement is still there
// ([0007](docs/decisions/0007-rank-is-closed.md)): deprecating one of two
// competing claims removes the entry because the claim is retracted, and
// nothing else does.
//
// A pair conflicts when more than one live claim is written on it, whatever
// those claims say. Whether two values agree is a question about a tolerance,
// and tolerances are registry data the consuming repository owns
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)) rather than a
// constant hidden in this walk — so the register reports that the model states
// a thing twice, and what each statement is, and leaves agreement to whoever
// declared what agreement means.
//
// A deprecated claim is never competing. It is retracted rather than
// out-ranked, so a pair whose second claim is deprecated has one live claim and
// no entry here, which is the one way of silencing a conflict the format has
// and it requires asserting in the file that the claim is wrong.
//
// Entries come back ordered by subject and then by predicate, which is an order
// of what the claims are about rather than of where they were written or of
// when the walk reached them. Two runs over one model therefore print a
// register which diffs to nothing, and moving a claim between files does not
// reshuffle it.
//
// A conflict is a finding rather than a failure and this traversal cannot fail.
// A predicate the registry declares strict escalates an ambiguity to an
// [AmbiguousResolutionError] for a caller asking [Claims.Resolve] for one
// number, because there it has to answer; the register is asked what the model
// says, and a strict predicate with two equally current claims is exactly what
// it exists to report.
func (c *Claims) Conflicts() iter.Seq[Conflict] {
	return func(yield func(Conflict) bool) {
		for _, conflict := range c.register() {
			if !yield(conflict) {
				return
			}
		}
	}
}

// pair is one subject and one predicate, which is the thing a conflict is
// about.
type pair struct {
	subject   ID
	predicate string
}

// register computes the conflict register.
//
// It walks the claims in the order they were read and groups the live ones,
// which keeps the claims of an entry in written order without a map deciding
// it, and then sorts the entries themselves. The pairs are unique, so the sort
// is total and the answer is a function of the claims rather than of the walk.
func (c *Claims) register() []Conflict {
	if c == nil {
		return nil
	}

	live := make(map[pair][]*Claim)
	var order []pair

	for _, claim := range c.inOrder {
		if claim.rank == RankDeprecated {
			continue
		}

		of := pair{subject: claim.subject, predicate: claim.predicate}
		if _, seen := live[of]; !seen {
			order = append(order, of)
		}
		live[of] = append(live[of], claim)
	}

	var register []Conflict
	for _, of := range order {
		competing := live[of]
		if len(competing) < 2 {
			continue
		}

		competing = slices.Clone(competing)
		slices.SortStableFunc(competing, compareClaims)

		register = append(register, Conflict{
			claims:     competing,
			resolution: c.resolve(of.subject, of.predicate),
		})
	}

	slices.SortFunc(register, func(a, b Conflict) int {
		return cmp.Or(
			cmp.Compare(a.Subject(), b.Subject()),
			cmp.Compare(a.Predicate(), b.Predicate()),
		)
	})

	return register
}
