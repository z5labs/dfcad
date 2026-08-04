// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"iter"
)

// boundaryChild is the child a semantic node writes to reference the loop which
// bounds it, per specification section 6.1.
const boundaryChild = "boundary"

// boundaryHint is what a diagnostic about a `boundary` carries: how the
// reference is written, which is what the author needs where the id names the
// wrong thing.
const boundaryHint = "a semantic node references its outline, written (boundary <loop-id>) and naming a loop; " +
	"a node never carries coordinates of its own"

// boundaryReference is one `(boundary <loop-id>)` as it was written: which node
// wrote it, which id it names, where that id was written, and where the node
// itself is named.
//
// The spans are what the diagnostics point at, and a [SemanticNode] carries
// none of them: it holds the ids of the loops which bound it, which is what a
// caller reads, rather than the positions the children were written at.
type boundaryReference struct {
	// node is the node which wrote it.
	node *SemanticNode

	// loop is the id it names.
	loop ID

	// at is where that id was written, which is what a diagnostic about the
	// loop it names points at.
	at Span

	// where is where the node itself is named, which is the other end such a
	// diagnostic points back at.
	where Span
}

// Boundaries is the boundary references of one model, resolved: which loop each
// semantic node is bounded by, and which nodes each loop, edge and vertex is
// part of the boundary of.
//
// It is the join between the two families and is the only thing in the engine
// which holds one. A `boundary` is written on a semantic node and names a member
// of the geometric family, so no pass which has read one family can resolve it
// ([0001](docs/decisions/0001-two-node-families.md)); this is what the pass
// which has read both produces.
//
// Both directions are indexed, because both are asked and only one is written.
// "What is this room bounded by" reads the reference forwards; "what does this
// wall bound" reads it backwards, is written nowhere, and is the question which
// makes a shared edge visible as shared. Computing the reverse by scanning every
// node per question would make it quadratic in the size of the model.
//
// A Boundaries is read-only once resolved. The zero value holds nothing, which
// is what a model with no boundary reference in it yields, and every method
// below works on it.
type Boundaries struct {
	// loops are the loops each node is bounded by, in the order it wrote them.
	loops map[*SemanticNode][]*Loop

	// edges and vertices are what each node's boundary is assembled from, in
	// the order the loops reach them and each held once.
	//
	// They are indexed rather than walked per question for the reason the
	// reverse direction is: a caller asking what a region depends on asks it of
	// every region, and re-walking the loops each time would read the whole
	// geometric family once per node.
	edges    map[*SemanticNode][]*Edge
	vertices map[*SemanticNode][]*Vertex

	// bounded is the nodes each loop bounds, and regions the nodes each edge is
	// part of the boundary of, both in the order the walk read the nodes.
	//
	// Neither direction is written in the model. An edge shared by two regions
	// is one node with one identity, referenced from both, and this is what
	// makes the sharing readable from the edge rather than only from the
	// regions.
	bounded map[*Loop][]*SemanticNode
	regions map[*Edge][]*SemanticNode

	// backing is the elements which physically realise each edge, in the order
	// the edge named them and holding only the references which resolved.
	//
	// It is the other reference which crosses between the families, and it is
	// held here for the reason the loops are: a `backed-by` is written on an edge
	// and names a semantic node, so the pass which has read both families is the
	// only one which can answer it
	// ([0001](docs/decisions/0001-two-node-families.md)).
	//
	// What is *not* here is whether an edge is a physical boundary or a virtual
	// one. That is computed from this map and from what the edge wrote, every
	// time it is asked, and is stored nowhere
	// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
	backing map[*Edge][]*SemanticNode
}

// Loops iterates the loops region is bounded by, in the order it wrote them.
//
// A node with no boundary yields nothing, which is ordinary: a circuit group and
// a warranty have no outline, and neither is malformed.
//
// Only a reference which resolved is here. A `boundary` naming nothing, or
// naming something which is not a loop, is a diagnostic from
// [ResolveBoundaries] rather than a state a caller has to interpret.
func (b *Boundaries) Loops(region *SemanticNode) iter.Seq[*Loop] {
	return sequence(b.index().loops[region])
}

// Edges iterates the edges region's boundary is assembled from, in the order its
// loops traverse them and each yielded once.
//
// It reaches through the loops rather than past them. A region depends on an
// edge because a loop it references names that edge, which is what makes the
// dependency real: the edge is one node with one identity, and moving it moves
// every region which reaches it.
//
// An edge reached through two of a region's loops is yielded once, because the
// question is what the region depends on and not how many ways it gets there.
func (b *Boundaries) Edges(region *SemanticNode) iter.Seq[*Edge] {
	return sequence(b.index().edges[region])
}

// Vertices iterates the vertices region's boundary is assembled from, in the
// order its edges reach them — start then end, per edge — and each yielded once.
//
// It is [Boundaries.Edges] one step further down. A vertex shared by two edges
// of the same ring is one corner and is yielded once, which is what makes the
// count of them the count of the region's corners rather than twice it.
func (b *Boundaries) Vertices(region *SemanticNode) iter.Seq[*Vertex] {
	return sequence(b.index().vertices[region])
}

// Bounded iterates the nodes loop bounds, in the order the walk read them.
//
// It is the reverse of [Boundaries.Loops]. The reference is written on the
// semantic node, so this direction is written nowhere and is indexed when the
// two families are joined.
func (b *Boundaries) Bounded(loop *Loop) iter.Seq[*SemanticNode] {
	return sequence(b.index().bounded[loop])
}

// Regions iterates the nodes edge is part of the boundary of, in the order the
// walk read them.
//
// This is what a shared wall looks like from the wall. Two spaces either side of
// a partition reference one edge through their own loops, and asking the edge
// which regions reach it gives both — from one edge with one identity, rather
// than from two copies of a coordinate which can drift apart
// ([0001](docs/decisions/0001-two-node-families.md)).
func (b *Boundaries) Regions(edge *Edge) iter.Seq[*SemanticNode] {
	return sequence(b.index().regions[edge])
}

// index is the receiver with its maps readable, so that every method above works
// on a nil Boundaries and on the zero value alike.
func (b *Boundaries) index() *Boundaries {
	if b == nil {
		return &Boundaries{}
	}
	return b
}

// sequence iterates a slice, which is what every traversal above hands back.
func sequence[T any](items []T) iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, item := range items {
			if !yield(item) {
				return
			}
		}
	}
}

// ResolveBoundaries joins the two families of one model: it resolves every
// `boundary` a semantic node wrote to the loop it names and every `backed-by` an
// edge wrote to the element it names, and indexes what each region depends on
// and what depends on each loop and edge.
//
// It takes both families already loaded rather than a root, because it adds no
// reading of its own. [LoadNodes] and [LoadTopology] each walk the tree once and
// each report what its own family got wrong; a third walk here would read every
// file a third time and report the parse error in it a third time.
//
// It is a pass of its own for the reason there are two loaders at all. A
// `boundary` is written on a semantic node and names a loop, so answering it
// means having read both families, and a loader which had read one would have to
// either resolve it wrongly or leave it — which is what they do
// ([0001](docs/decisions/0001-two-node-families.md)).
//
// Every reference is checked and every one which fails is reported: a `boundary`
// naming nothing this model holds, one naming a member of either family which is
// not a loop, one naming the same loop twice, and the same three for a
// `backed-by` which has to reach a semantic node of kind Element. A reference
// which resolves is indexed; one which does not is not, because an index entry
// for it would be an edge with one end.
//
// Whether the loops themselves close is not asked here. That is a question about
// the edges of one loop and the positions of their vertices, it is answered
// against a named tolerance, and [Topology.Assemble] is what answers it.
//
// Diagnostics come back in the order the pass found them. Collecting them into a
// [Diagnostics] is what puts them in reporting order.
func ResolveBoundaries(nodes *Nodes, topology *Topology) (*Boundaries, []Diagnostic) {
	l := &boundaryLoader{
		nodes:    nodes,
		topology: topology,
		boundaries: &Boundaries{
			loops:    make(map[*SemanticNode][]*Loop),
			edges:    make(map[*SemanticNode][]*Edge),
			vertices: make(map[*SemanticNode][]*Vertex),
			bounded:  make(map[*Loop][]*SemanticNode),
			regions:  make(map[*Edge][]*SemanticNode),
			backing:  make(map[*Edge][]*SemanticNode),
		},
	}

	l.resolve()
	l.back()
	l.assembleDependencies()

	return l.boundaries, l.diags
}

// boundaryLoader joins one loaded pair of families.
type boundaryLoader struct {
	reader

	// nodes and topology are the two families, already loaded. Neither is
	// written to.
	nodes    *Nodes
	topology *Topology

	// boundaries is the join being built.
	boundaries *Boundaries
}

// resolve checks every `boundary` the semantic family wrote and indexes the ones
// which reach a loop.
func (l *boundaryLoader) resolve() {
	if l.nodes == nil {
		return
	}

	// named is where each node first named each loop, which is what a repeated
	// boundary points its reader back at.
	type naming struct {
		node *SemanticNode
		loop ID
	}
	named := make(map[naming]Span, len(l.nodes.boundaries))

	for _, written := range l.nodes.boundaries {
		if first, ok := named[naming{node: written.node, loop: written.loop}]; ok {
			l.add(Diagnostic{
				Severity: SeverityError,
				Span:     written.at,
				Message: fmt.Sprintf(
					"expected a loop %s does not already name, found %s a second time",
					nodeName(written.node), written.loop,
				),
				Hint:    "a boundary is a reference and not a count; naming a loop twice says exactly what naming it once says",
				Related: []RelatedLocation{{Span: first, Message: "first named here"}},
			})
			continue
		}
		named[naming{node: written.node, loop: written.loop}] = written.at

		loop, ok := l.topology.Loop(written.loop)
		if !ok {
			l.add(l.unresolved(written))
			continue
		}

		l.declaredTwice(written, loop)

		l.boundaries.loops[written.node] = append(l.boundaries.loops[written.node], loop)
	}
}

// declaredTwice reports a region whose outline is a loop in another frame.
//
// A shape lives in exactly one frame and is transformed on demand
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)), and a node which
// declares one is declaring which frame its shape is expressed in. A loop in a
// different one makes that two frames for one shape, which is two sources of
// truth for where the region is the moment either is re-fitted.
//
// The reference is still indexed. It reaches a loop, so what the node is bounded
// by is not in doubt; which frame the pair is in is, and that is what the
// diagnostic says.
//
// A node which declares no frame is not reported. Its frame is the one its
// outline is in, which is an ordinary node rather than a node in two frames.
func (l *boundaryLoader) declaredTwice(written boundaryReference, loop *Loop) {
	frame, ok := written.node.Frame()
	if !ok || loop.frame == "" || frame == loop.frame {
		return
	}

	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     written.at,
		Message: fmt.Sprintf(
			"expected a loop in %s, found %s, which is declared in %s",
			frame, written.loop, loop.frame,
		),
		Hint: "a shape is declared in exactly one frame and is transformed on demand; the two frames are related by a " +
			"transform claim, and a node bounded by a loop in another frame is a shape in neither",
		Related: []RelatedLocation{
			{Span: l.topology.namedAt(loop.id, loop.span), Message: fmt.Sprintf("the loop it names is declared in %s, here", loop.frame)},
			{Span: written.where, Message: fmt.Sprintf("the node which names it is declared in %s, here", frame)},
		},
	})
}

// unresolved reports a `boundary` which did not reach a loop, saying what it
// reached instead where the model holds anything of that id at all.
//
// The two are one diagnostic with two messages rather than two diagnostics
// because they are one mistake: the reference does not name a loop. Which of the
// two it is decides what the author is told to look at — a name nothing holds is
// a typo or a deletion, and a name something else holds is a reference to the
// wrong thing, and the second can point at what it found.
func (l *boundaryLoader) unresolved(written boundaryReference) Diagnostic {
	diag := Diagnostic{
		Severity: SeverityError,
		Span:     written.at,
		Hint:     boundaryHint,
		Related:  []RelatedLocation{{Span: written.where, Message: "the node it is written on is named here"}},
	}

	switch declared, ok := l.topology.definitionOf(written.loop); {
	case ok:
		diag.Message = fmt.Sprintf(
			"expected a loop id, found %s, which is %s %s",
			written.loop, article(declared.tag), declared.tag,
		)
		diag.Related = append([]RelatedLocation{
			{Span: declared.at, Message: fmt.Sprintf("the %s it names is written here", declared.tag)},
		}, diag.Related...)

	default:
		node, isNode := l.nodes.Node(written.loop)
		if !isNode {
			diag.Message = fmt.Sprintf(
				"expected a loop id something in this model holds, found %s, which names no loop",
				written.loop,
			)
			break
		}

		diag.Message = fmt.Sprintf(
			"expected a loop id, found %s, which is a semantic node",
			written.loop,
		)
		diag.Related = append([]RelatedLocation{
			{Span: l.nodes.named(node), Message: "the node it names is written here"},
		}, diag.Related...)
	}

	return diag
}

// assembleDependencies indexes what each region's boundary is assembled from and
// what reaches each loop and edge.
//
// It walks the nodes in the order they were read rather than the map of resolved
// references, so that every list it builds is in a deterministic order and
// anything built from one diffs against the last run's. Iterating the map would
// order the regions of a shared edge by whatever the runtime felt like.
func (l *boundaryLoader) assembleDependencies() {
	if l.nodes == nil {
		return
	}

	for _, node := range l.nodes.inOrder {
		loops := l.boundaries.loops[node]
		if len(loops) == 0 {
			continue
		}

		// A region reaching one edge through two of its loops depends on it
		// once. The question is what it is built from, not how many ways it is
		// reached.
		seenEdges := make(map[*Edge]bool)
		seenVertices := make(map[*Vertex]bool)

		for _, loop := range loops {
			l.boundaries.bounded[loop] = append(l.boundaries.bounded[loop], node)

			for _, id := range loop.edges {
				edge, ok := l.topology.Edge(id)
				if !ok || seenEdges[edge] {
					continue
				}
				seenEdges[edge] = true

				l.boundaries.edges[node] = append(l.boundaries.edges[node], edge)
				l.boundaries.regions[edge] = append(l.boundaries.regions[edge], node)

				start, end := edge.Vertices()
				for _, id := range []ID{start, end} {
					vertex, ok := l.topology.Vertex(id)
					if !ok || seenVertices[vertex] {
						continue
					}
					seenVertices[vertex] = true

					l.boundaries.vertices[node] = append(l.boundaries.vertices[node], vertex)
				}
			}
		}
	}
}
