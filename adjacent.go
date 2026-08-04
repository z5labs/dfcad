// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"iter"
	"slices"
)

// Adjacent is one region which shares boundary with another, and the edges the
// two of them share.
//
// Adjacency is defined by those edges and by nothing else: two semantic nodes
// are adjacent when at least one edge is part of the boundary of both. It is
// not a distance, not an overlap and not a guess from two outlines which happen
// to touch — an edge is one node with one identity, and two regions which reach
// it through their own loops are either reaching the same edge or they are not
// ([0001](docs/decisions/0001-two-node-families.md)).
//
// Nothing in the format says two regions are adjacent. The relation is computed
// from the boundary references every time it is asked, so a partition which
// comes to be shared makes two rooms adjacent with no edit which says so, and
// the answer cannot drift away from what the model says
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// The zero value holds no node and no edges, which no traversal yields.
type Adjacent struct {
	// node is the region which was reached.
	node *SemanticNode

	// via are the edges it shares with the region it was reached from, in the
	// order that region's boundary traverses them.
	via []*Edge

	// depth is how many regions the walk crossed to reach it.
	depth int
}

// Node returns the region the traversal reached.
func (a Adjacent) Node() *SemanticNode { return a.node }

// Via returns the edges this region shares with the one it was reached from, in
// the order that region's boundary traverses them.
//
// Every shared edge is named rather than only the first. Two rooms either side
// of a partition with a doorway through it share two edges, and which of them a
// question is about — the wall, or the opening — is the difference between "what
// separates these rooms" and "how do you get between them". [Boundaries.Classified]
// is what says which each of them is.
//
// A result reached at a depth past the first shares its edges with the region
// which reached it rather than with the region the question was asked about, for
// the reason it is a step further away: nothing joins them directly.
func (a Adjacent) Via() []*Edge { return slices.Clone(a.via) }

// Depth returns how many steps of adjacency the traversal took to reach it: one
// for a region on the other side of an edge of this one, two for a region on the
// other side of one of those, and so on.
func (a Adjacent) Depth() int { return a.depth }

// Relation returns [RelationAdjacency], which is what says the result means a
// shared boundary edge rather than enclosure or grouping.
func (a Adjacent) Relation() Relation { return RelationAdjacency }

// Adjacent iterates the regions which share at least one boundary edge with
// region, each once and with the edges they share.
//
// This is "what borders this room", and it is answered from the model rather
// than from geometry: the shared edge is the same node reached from both sides,
// so two rooms are adjacent exactly when one of them reaches an edge the other
// reaches too. Two outlines which meet along a line drawn twice are two
// boundaries which happen to coincide, and this reports them as what they are —
// unrelated — which is the whole reason a boundary is a reference rather than a
// copy of a coordinate.
//
// A region is never adjacent to itself, however many of its own edges it
// reaches. Its own boundary is not something on the other side of it.
//
// The order is the order region's boundary traverses its edges, which is the
// order its loops were written in and is deterministic. A neighbour reached
// through two shared edges comes back once, carrying both.
func (b *Boundaries) Adjacent(region *SemanticNode) iter.Seq[Adjacent] {
	return b.AdjacentTo(region, 1)
}

// AdjacentTo iterates the regions region borders, the regions those border, and
// so on, stopping after depth steps.
//
// It is [Boundaries.Adjacent] followed outward: a depth of one is what is on the
// other side of this room's walls, a depth of two adds what is on the other side
// of theirs, and [Unbounded] is everything reachable from it through shared
// boundary — which for a floor plan drawn as one connected set of rooms is the
// floor.
//
// Each region comes back once, at the fewest steps it can be reached in, so a
// ring of rooms terminates and a room reachable two ways is one result. A depth
// of zero takes no step and yields nothing.
func (b *Boundaries) AdjacentTo(region *SemanticNode, depth int) iter.Seq[Adjacent] {
	return func(yield func(Adjacent) bool) {
		if region == nil || depth == 0 {
			return
		}

		index := b.index()

		seen := map[*SemanticNode]bool{region: true}
		frontier := []*SemanticNode{region}

		for level := 1; len(frontier) > 0 && (depth < 0 || level <= depth); level++ {
			var next []*SemanticNode

			for _, from := range frontier {
				for _, neighbour := range index.neighbours(from) {
					if seen[neighbour.node] {
						continue
					}
					seen[neighbour.node] = true

					neighbour.depth = level
					if !yield(neighbour) {
						return
					}

					next = append(next, neighbour.node)
				}
			}

			frontier = next
		}
	}
}

// neighbours is the regions which share an edge with region, in the order its
// boundary reaches them and each with every edge it shares.
//
// It is a slice rather than a sequence because a neighbour reached through two
// edges is one neighbour: the edges have to be collected before the first result
// can be complete, and a caller handed the same region twice would be counting
// the ways it got there rather than what is next to it.
func (b *Boundaries) neighbours(region *SemanticNode) []Adjacent {
	var out []Adjacent

	at := make(map[*SemanticNode]int)
	for _, edge := range b.edges[region] {
		for _, neighbour := range b.regions[edge] {
			if neighbour == region {
				continue
			}

			if index, found := at[neighbour]; found {
				out[index].via = append(out[index].via, edge)
				continue
			}

			at[neighbour] = len(out)
			out = append(out, Adjacent{node: neighbour, via: []*Edge{edge}})
		}
	}

	return out
}
