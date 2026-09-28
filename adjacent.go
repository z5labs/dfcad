// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"iter"
	"slices"
	"strings"
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
// The zero value holds no node, no region it was reached from and no edges,
// which no traversal yields.
type Adjacent struct {
	// node is the region which was reached.
	node *SemanticNode

	// from is the region it was reached from: the subject at the first step,
	// and a region one step nearer past it.
	from *SemanticNode

	// via are the edges it shares with the region it was reached from, in the
	// order that region's boundary traverses them.
	via []*Edge

	// depth is how many regions the walk crossed to reach it.
	depth int
}

// Node returns the region the traversal reached.
func (a Adjacent) Node() *SemanticNode { return a.node }

// From returns the region this one was reached from, which is the region
// [Adjacent.Via] names the edges shared with. At a depth of one it is the region
// the walk started from.
//
// The walk is breadth first and reports each region once, at the fewest steps it
// can be reached in, so one region reached-from per result is a shortest-path
// tree: following From from any result reaches the region the walk started from
// in exactly [Adjacent.Depth] steps, and every region on the way is itself a
// result of the same walk. Where more than one region a step nearer borders this
// one, it is the one with the smallest id, for the reason Via explains.
//
// It is written at every depth rather than only past the first, where it could
// only ever be the subject, so that a result has one shape whichever step it was
// found at.
func (a Adjacent) From() *SemanticNode { return a.from }

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
// the reason it is a step further away: nothing joins them directly. Where more
// than one region a step nearer borders it, the one it was reached from is the
// one with the smallest id — never the one read first — so which edges these are
// is decided by the model and not by which file each region is written in, and
// can be checked against the boundaries of the regions a step nearer.
//
// A walk given an [AdjacencyFilter] names only the edges it crossed: the shared
// edges the filter allows, and not the wall beside the doorway. Under a filter
// this is the answer to "how do you get between them", and [Boundaries.Classify]
// is still where "what separates them" is asked.
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
//
// Every result at the first step is reached from region itself. Past it, a
// result is reached from the region one step nearer which has the smallest id
// among those it shares an edge with, and that is the region its
// [Adjacent.From] names and its [Adjacent.Via] names the edges shared with. Each step is expanded in id
// order to make it so: expanding in the order regions were discovered would
// choose by the order the files were read in, and moving a region between
// files would change the answer.
//
// Every shared edge may be crossed. It is [Boundaries.AdjacentWalk] with an empty
// [AdjacencyFilter], and answers exactly what that does.
func (b *Boundaries) AdjacentTo(region *SemanticNode, depth int) iter.Seq[Adjacent] {
	return b.AdjacentWalk(region, depth, AdjacencyFilter{})
}

// AdjacencyFilter says which shared edges an adjacency walk may cross, and which
// regions it may enter on the other side of them.
//
// It follows the convention [RuleFilter] sets: a field left empty matches
// everything, so the zero value crosses every shared edge into every region and
// a walk with it is [Boundaries.AdjacentTo]. Within one field the values are
// alternatives.
//
// The crossing fields, [AdjacencyFilter.CrossVirtual] and
// [AdjacencyFilter.CrossTypes], are alternatives to each other too, because each
// names a way through: an edge may be crossed when either of them allows it.
//
// The walk fields, [AdjacencyFilter.WalkKinds] and [AdjacencyFilter.WalkTypes],
// are not, because each says something a region has to be: a region is entered
// only when it satisfies every walk field given. A region the walk does not enter
// is neither reported nor walked through, which is what keeps a site or a storey
// outline drawn along the outside of two rooms from joining rooms that share no
// edge with each other.
//
// Which types count as a way through, or as somewhere to go, is the caller's to
// say. The engine compares the names it is handed with the types the model
// declares and attaches no meaning to either
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)): a
// Doorway is a passage in one registry and a word nobody declared in another.
type AdjacencyFilter struct {
	// CrossVirtual allows an edge nothing backs to be crossed. The open line
	// between a foyer and a dining room is written that way, and so is a doorway
	// in a model which draws its openings as nothing at all.
	CrossVirtual bool

	// CrossTypes are the types a backing element may declare for the edge it
	// backs to be crossed. At least one of an edge's backing elements has to
	// declare one of them, rather than all of them, because a doorway is cut into
	// a wall and the model says so by backing one edge with both.
	CrossTypes []string

	// WalkKinds are the kinds a region may declare for the walk to enter it.
	// Every region but the one the walk starts from has to declare one of them.
	WalkKinds []Kind

	// WalkTypes are the types a region may declare for the walk to enter it.
	// Every region but the one the walk starts from has to declare one of them.
	WalkTypes []string
}

// crossesEverything reports whether the filter was given no crossing field, in
// which case every edge may be crossed.
func (f AdjacencyFilter) crossesEverything() bool {
	return !f.CrossVirtual && len(f.CrossTypes) == 0
}

// Matches reports whether the edge may be crossed.
//
// Only the crossing fields decide it; the walk fields decide which regions are
// entered, not which edges. A filter given no crossing field matches every edge.
// Otherwise an edge matches in two cases: it is virtual and
// [AdjacencyFilter.CrossVirtual] is set, or at least one of the elements backing
// it declares a type in [AdjacencyFilter.CrossTypes]. An unresolved edge — one
// which names backing elements the model does not hold — never matches a
// crossing field which was given, because nothing is known about what realises
// it: calling it virtual would be the silent reclassification its load error
// exists to prevent, and it has no element to declare a type.
func (f AdjacencyFilter) Matches(edge BoundaryEdge) bool {
	if f.crossesEverything() {
		return true
	}

	switch edge.Classification() {
	case ClassificationVirtual:
		return f.CrossVirtual
	case ClassificationPhysical:
		for _, element := range edge.backing {
			if slices.Contains(f.CrossTypes, element.Type()) {
				return true
			}
		}
	}

	return false
}

// Enters reports whether a walk may enter the region: report it, and walk on
// through it.
//
// Only the walk fields decide it. A region is entered when its kind is one of
// [AdjacencyFilter.WalkKinds] and its type is one of [AdjacencyFilter.WalkTypes],
// where a field left empty admits every region. A nil region is never entered.
//
// The region a walk starts from is not asked: it is where the walk is, whatever
// it declares.
func (f AdjacencyFilter) Enters(region *SemanticNode) bool {
	if region == nil {
		return false
	}

	if len(f.WalkKinds) > 0 && !slices.Contains(f.WalkKinds, region.Kind()) {
		return false
	}

	if len(f.WalkTypes) > 0 && !slices.Contains(f.WalkTypes, region.Type()) {
		return false
	}

	return true
}

// AdjacentWalk iterates the regions reachable from region across the shared
// edges filter allows to be crossed, through the regions it allows to be
// entered, stopping after depth steps.
//
// It is [Boundaries.AdjacentTo] with a say in which edges are ways through. A
// walk from a corridor with a filter naming only doorways reaches the rooms whose
// doors open onto it and not the riser behind a solid wall, which is the question
// "what can somebody reach from here" rather than "what is next to what". A
// region which shares only edges the filter refuses is not reached across them,
// though it may still be reached another way.
//
// A region the filter does not enter is neither reported nor walked through, so
// a walk which enters only spaces cannot step from one room onto the lot both
// border and from there into a room on its far side. The walk starts from
// region whatever region declares, and every result, and every region a result
// is reached from past the first step, is one the filter enters.
//
// [Adjacent.Via] names only the edges crossed — the crossable edges the result
// shares with the region it was reached from — so under a filter it answers "how
// do you get between them" rather than "what separates them". [Adjacent.From] is
// the region one step nearer with the smallest id among those which share a
// crossable edge with it.
//
// An empty filter crosses every shared edge into every region and answers
// exactly what [Boundaries.AdjacentTo] does.
func (b *Boundaries) AdjacentWalk(region *SemanticNode, depth int, filter AdjacencyFilter) iter.Seq[Adjacent] {
	return func(yield func(Adjacent) bool) {
		if region == nil || depth == 0 {
			return
		}

		index := b.index()

		seen := map[*SemanticNode]bool{region: true}
		frontier := []*SemanticNode{region}

		for level := 1; len(frontier) > 0 && (depth < 0 || level <= depth); level++ {
			// The region a result is reached from is the first of the frontier
			// to border it, so the frontier's order is what chooses it.
			slices.SortFunc(frontier, func(x, y *SemanticNode) int {
				return strings.Compare(string(x.ID()), string(y.ID()))
			})

			var next []*SemanticNode

			for _, from := range frontier {
				for _, neighbour := range index.neighbours(from, filter) {
					if seen[neighbour.node] {
						continue
					}
					seen[neighbour.node] = true

					neighbour.from = from
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

// neighbours is the regions filter enters which share an edge filter allows to
// be crossed with region, in the order its boundary reaches them and each with
// every such edge it shares.
//
// It is a slice rather than a sequence because a neighbour reached through two
// edges is one neighbour: the edges have to be collected before the first result
// can be complete, and a caller handed the same region twice would be counting
// the ways it got there rather than what is next to it.
func (b *Boundaries) neighbours(region *SemanticNode, filter AdjacencyFilter) []Adjacent {
	var out []Adjacent

	at := make(map[*SemanticNode]int)
	for _, edge := range b.edges[region] {
		if !filter.Matches(BoundaryEdge{edge: edge, backing: b.backing[edge]}) {
			continue
		}

		for _, neighbour := range b.regions[edge] {
			if neighbour == region || !filter.Enters(neighbour) {
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
