// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
)

// Entity is one thing a model holds under an id: a semantic node, or a vertex,
// an edge or a loop of the geometric family.
//
// It exists because an id is unique across the whole model rather than within a
// family (specification section 6.9), so "what does this id name" is a question
// with one answer and no family in it. A caller which knows which family it
// wants asks that family; a caller holding an id out of a file, a command line
// or a diagnostic asks [Graph.Entity] and switches on what came back.
//
// A claim is not an Entity. A claim id is optional, so a claim carries
// `ID() (ID, bool)` rather than an id, and the model holds only the claims
// which wrote one. [Claims.Claim] is the lookup for those.
type Entity interface {
	// ID returns the id the model holds it under.
	ID() ID

	// Span returns where it was written, which is what a diagnostic about it
	// points at.
	Span() Span
}

// Graph is one whole model, loaded: the vocabulary it declares, both families
// of nodes, the claims written on them, the frames those claims measure and the
// boundaries which join the two families.
//
// This is the type every command reads. A caller which loaded the six pieces
// itself would have to know that a registry is resolved before an entity is
// interpreted, that boundaries need both families, that frames need the claims,
// and that all four passes read the same tree; getting any of that wrong is a
// model which loads differently depending on which question was asked of it
// first.
//
// A Graph is read-only once loaded. Nothing here writes to a file and nothing
// caches a derived answer: the conflict register, a loop's closure and an
// edge's classification are computed from the model every time they are asked
// for ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
type Graph struct {
	// root is what the load was asked for, which is where a diagnostic about
	// the model as a whole rather than about any one file points.
	root string

	// The six pieces, in the order the load produced them. Each is what its own
	// loader returned, unmodified: this type composes the passes rather than
	// re-reading what they read.
	registry   *Registry
	nodes      *Nodes
	topology   *Topology
	claims     *Claims
	frames     *Frames
	boundaries *Boundaries

	// byKind and byType are the semantic nodes grouped by the two axes a caller
	// asks about in bulk, each in the order the walk reached them.
	//
	// They are indexed at load for the reason [Nodes.Node] is indexed: every
	// query which selects by kind or by type would otherwise scan the whole
	// model, and a command answering several of them would scan it several
	// times.
	byKind map[Kind][]*SemanticNode
	byType map[string][]*SemanticNode

	// summary is what the load counted.
	summary Summary
}

// LoadGraph reads the whole model beneath root in one pass.
//
// root is what [Load] takes: a single file, or a directory beneath which every
// file whose extension is [Extension] is read. Every file is read exactly once
// and parsed exactly once, and the four passes which interpret a tree —
// registry, semantic family, geometric family and claims — each read the trees
// that one walk produced. Asking four questions of a model is four
// interpretations of it and not four readings of the disk.
//
// The order of the passes is the one dependency between them: registries
// resolve before any entity is interpreted, because whether a type is declared,
// which shape a predicate's value takes and which unit it is written in are
// questions only registry data answers, and a registry is a property of the
// whole tree rather than of the file an entity happens to sit in. The two joins
// run last, over what the four passes read: frames, which are declarations
// measured by claims, and boundaries, which are the references between the two
// families.
//
// Loading is one pass which reports everything it finds. A file which does not
// parse, a form which is structurally wrong, a name nothing declares, a
// reference which reaches nothing, an id two things hold and a ring of
// references which returns to where it started are each a diagnostic, and none
// of them stops the rest of the tree from being read. Fixing a model one
// diagnostic at a time is a guessing loop.
//
// The one thing this pass checks which none of the four can is uniqueness
// across the families. Each of them enforces it within what it read and against
// the frames the registry declared — two nodes, two vertices, two claims, a
// vertex and a frame — and none of them can see a vertex and a claim which hold
// the same id, because an id is unique across the whole model and not within a
// family (specification section 6.9).
//
// The graph which comes back is always usable, whatever the diagnostics say. A
// tree holding no entity file at all yields an empty graph rather than a nil
// one, so a caller reporting on a model which has not been written yet reports
// on nothing rather than crashing.
//
// Loading is deterministic. The walk is in the lexical order of the paths, so
// two loads of one tree produce the same graph in the same order, and anything
// derived from it — a summary, a listing, a rendering of the diagnostics —
// diffs against the last run's.
//
// Diagnostics come back in the order the passes found them, which is pass by
// pass and not file by file. Collecting them into a [Diagnostics] is what puts
// them in reporting order.
func LoadGraph(root string) (*Graph, []Diagnostic) {
	// The trees are collected rather than streamed because four passes read
	// them. What one pass costs in memory is the largest file; what this costs
	// is the tree, which is the price of reading it once instead of four times.
	var (
		parsed []source
		diags  []Diagnostic
	)
	for src := range readTree(root) {
		if src.file == nil {
			diags = append(diags, src.diag)
			continue
		}
		parsed = append(parsed, src)
	}
	sources := slices.Values(parsed)

	registry, registryDiags := loadRegistry(root, sources)
	diags = append(diags, registryDiags...)

	nodes, nodeDiags := loadNodes(sources, registry)
	diags = append(diags, nodeDiags...)

	topology, topologyDiags := loadTopology(sources, registry)
	diags = append(diags, topologyDiags...)

	claims, claimDiags := loadClaims(sources, registry)
	diags = append(diags, claimDiags...)

	frames, frameDiags := ResolveFrames(registry, claims)
	diags = append(diags, frameDiags...)

	boundaries, boundaryDiags := ResolveBoundaries(nodes, topology)
	diags = append(diags, boundaryDiags...)

	g := &Graph{
		root:       root,
		registry:   registry,
		nodes:      nodes,
		topology:   topology,
		claims:     claims,
		frames:     frames,
		boundaries: boundaries,
		byKind:     make(map[Kind][]*SemanticNode),
		byType:     make(map[string][]*SemanticNode),
	}

	diags = append(diags, g.unique()...)
	g.index()

	return g, diags
}

// Root returns what the load was asked for, which is the directory or file the
// model was read from.
func (g *Graph) Root() string {
	if g == nil {
		return ""
	}
	return g.root
}

// Registry returns the vocabulary the model declares.
func (g *Graph) Registry() *Registry {
	if g == nil {
		return nil
	}
	return g.registry
}

// Nodes returns the semantic family.
func (g *Graph) Nodes() *Nodes {
	if g == nil {
		return nil
	}
	return g.nodes
}

// Topology returns the geometric family.
func (g *Graph) Topology() *Topology {
	if g == nil {
		return nil
	}
	return g.topology
}

// Claims returns the claims written on both families, which is also where the
// conflict register and the resolution rule are read from.
func (g *Graph) Claims() *Claims {
	if g == nil {
		return nil
	}
	return g.claims
}

// Frames returns the frames the registry declares, joined to the claims which
// measure them.
func (g *Graph) Frames() *Frames {
	if g == nil {
		return nil
	}
	return g.frames
}

// Boundaries returns the join between the two families: which loop bounds which
// region, and what each edge of a boundary is backed by.
func (g *Graph) Boundaries() *Boundaries {
	if g == nil {
		return nil
	}
	return g.boundaries
}

// Summary returns what the load counted.
func (g *Graph) Summary() Summary {
	if g == nil {
		return Summary{}
	}
	return g.summary
}

// Entity returns the thing id names, whichever family holds it, and whether the
// model holds one.
//
// An id is unique across the whole model, so at most one thing answers to it.
// The semantic family is looked in first and the geometric family second, which
// decides nothing: an id both of them hold is a duplicate the load already
// reported, and the answer is the definition which came first in the files.
//
// A claim is not reachable here, because a claim id is optional and a claim is
// not an [Entity]. [Claims.Claim] is the lookup for one.
func (g *Graph) Entity(id ID) (Entity, bool) {
	if g == nil || id == "" {
		return nil, false
	}

	if node, ok := g.nodes.Node(id); ok {
		return node, true
	}
	if vertex, ok := g.topology.Vertex(id); ok {
		return vertex, true
	}
	if edge, ok := g.topology.Edge(id); ok {
		return edge, true
	}
	if loop, ok := g.topology.Loop(id); ok {
		return loop, true
	}

	return nil, false
}

// Nearest returns the id closest to the one asked for which the model does
// hold, and whether one is close enough to be worth suggesting.
//
// It is what turns "nothing answers to site:S-1O1" into a sentence somebody can
// act on. Ids are written by hand and by agents, and most of the ones which
// reach nothing are the id which was meant with a character wrong; a lookup
// which reports only that it found nothing leaves whoever wrote it reading the
// files to find out which character.
//
// Close is what it is for a misspelled tag: the same distance and the same
// tolerance, which is one edit for something short and two for anything longer.
// The measure counts a transposition as one mistake rather than two, because
// swapping two characters is what typing produces.
//
// Only entities are considered, which is what [Graph.Entity] answers for. A
// claim id is not among them, for the reason it is not an [Entity]: a claim id
// is optional, and the model holds only the claims which wrote one.
//
// Candidates are considered in lexical order, so two ids equally close to what
// was asked for resolve to the same suggestion on every run and the answer is a
// property of the model rather than of the order the walk read it in.
func (g *Graph) Nearest(id ID) (ID, bool) {
	if g == nil || id == "" {
		return "", false
	}

	found, ok := nearest(string(id), slices.Sorted(g.ids()))
	return ID(found), ok
}

// ids iterates the id of every entity the model holds, family by family.
//
// The zero id is not among them. It belongs to a thing whose id could not be
// read, which is a diagnostic carrying what was written rather than a name
// anything resolves.
func (g *Graph) ids() iter.Seq[string] {
	return func(yield func(string) bool) {
		if g == nil {
			return
		}

		for _, entities := range []iter.Seq[Entity]{
			asEntities(g.nodes.All()),
			asEntities(g.topology.Vertices()),
			asEntities(g.topology.Edges()),
			asEntities(g.topology.Loops()),
		} {
			for entity := range entities {
				if entity.ID() == "" {
					continue
				}
				if !yield(string(entity.ID())) {
					return
				}
			}
		}
	}
}

// asEntities is a sequence of one family's members as the interface all four
// share, so that a walk over every family is one loop rather than four.
func asEntities[T Entity](seq iter.Seq[T]) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		for member := range seq {
			if !yield(member) {
				return
			}
		}
	}
}

// Node returns the semantic node id names, and whether the model holds one.
//
// It is [Graph.Entity] narrowed to the family most questions are about, so that
// a caller which wants a node does not type-assert one out of an interface to
// find out it had a vertex.
func (g *Graph) Node(id ID) (*SemanticNode, bool) {
	if g == nil {
		return nil, false
	}
	return g.nodes.Node(id)
}

// OfKind iterates the semantic nodes of one kind, in the order the walk reached
// them.
//
// A kind nothing was written under yields nothing, which is ordinary: a model
// with no zone in it is a model, not a model with something missing.
func (g *Graph) OfKind(kind Kind) iter.Seq[*SemanticNode] {
	return func(yield func(*SemanticNode) bool) {
		if g == nil {
			return
		}
		for _, node := range g.byKind[kind] {
			if !yield(node) {
				return
			}
		}
	}
}

// OfType iterates the semantic nodes which declare one type, in the order the
// walk reached them.
//
// The name is the one the registry declares. A type nothing was written under
// yields nothing, and so does a type nothing declares: a node naming an
// undeclared type is a diagnostic and is still a node of the type it named, so
// it is yielded here — the graph reports what was written rather than deciding
// it was not.
func (g *Graph) OfType(name string) iter.Seq[*SemanticNode] {
	return func(yield func(*SemanticNode) bool) {
		if g == nil {
			return
		}
		for _, node := range g.byType[name] {
			if !yield(node) {
				return
			}
		}
	}
}

// Within returns the node which strictly contains node, labelled with the
// relation which produced it, and whether it has one.
func (g *Graph) Within(node *SemanticNode) (Related, bool) {
	return g.Nodes().Within(node)
}

// Contains iterates the nodes written directly within node.
func (g *Graph) Contains(node *SemanticNode) iter.Seq[Related] {
	return g.Nodes().Contains(node)
}

// Ancestors iterates the containment chain above node, nearest first.
func (g *Graph) Ancestors(node *SemanticNode) iter.Seq[Related] {
	return g.Nodes().Ancestors(node)
}

// Descendants iterates everything contained beneath node, depth first.
func (g *Graph) Descendants(node *SemanticNode) iter.Seq[Related] {
	return g.Nodes().Descendants(node)
}

// Zones iterates the zones node declared membership of.
func (g *Graph) Zones(node *SemanticNode) iter.Seq[Related] {
	return g.Nodes().Zones(node)
}

// Members iterates the nodes which declared membership of zone.
func (g *Graph) Members(zone *SemanticNode) iter.Seq[Related] {
	return g.Nodes().Members(zone)
}

// Loops iterates the loops region is bounded by, in the order it wrote them.
func (g *Graph) Loops(region *SemanticNode) iter.Seq[*Loop] {
	return g.Boundaries().Loops(region)
}

// Edges iterates the edges region's boundary is assembled from.
func (g *Graph) Edges(region *SemanticNode) iter.Seq[*Edge] {
	return g.Boundaries().Edges(region)
}

// Vertices iterates the vertices region's boundary is assembled from.
func (g *Graph) Vertices(region *SemanticNode) iter.Seq[*Vertex] {
	return g.Boundaries().Vertices(region)
}

// Bounded iterates the nodes loop bounds.
func (g *Graph) Bounded(loop *Loop) iter.Seq[*SemanticNode] {
	return g.Boundaries().Bounded(loop)
}

// Regions iterates the nodes edge is part of the boundary of, which is what a
// shared wall looks like from the wall.
func (g *Graph) Regions(edge *Edge) iter.Seq[*SemanticNode] {
	return g.Boundaries().Regions(edge)
}

// Classify iterates the edges of region's boundary, each classified physical or
// virtual.
func (g *Graph) Classify(region *SemanticNode) iter.Seq[BoundaryEdge] {
	return g.Boundaries().Classify(region)
}

// Classified pairs edge with its classification and with the elements which
// physically realise it.
func (g *Graph) Classified(edge *Edge) BoundaryEdge {
	return g.Boundaries().Classified(edge)
}

// index groups the semantic nodes by kind and by type and counts what the load
// read.
func (g *Graph) index() {
	byKind := make(map[Kind]int)
	byType := make(map[string]int)

	for node := range g.nodes.All() {
		g.byKind[node.Kind()] = append(g.byKind[node.Kind()], node)
		g.byType[node.Type()] = append(g.byType[node.Type()], node)

		byKind[node.Kind()]++
		byType[node.Type()]++
	}

	var vertices, edges, loops int
	for range g.topology.Vertices() {
		vertices++
	}
	for range g.topology.Edges() {
		edges++
	}
	for range g.topology.Loops() {
		loops++
	}

	var conflicts int
	for range g.claims.Conflicts() {
		conflicts++
	}

	g.summary = Summary{
		nodes:      g.nodes.Len(),
		byKind:     byKind,
		byType:     byType,
		vertices:   vertices,
		edges:      edges,
		loops:      loops,
		claims:     g.claims.Len(),
		conflicts:  conflicts,
		unresolved: g.unresolved(),
	}
}

// definition is one place an id was defined, for the check that nothing else
// defined it too.
type idDefinition struct {
	// what the definition is, phrased for a diagnostic.
	what string

	// at is where the id was written, which is what the diagnostic points at.
	at Span
}

// unique reports every id which two of the three families hold.
//
// Each pass enforces uniqueness within what it read and against the frames the
// registry declared, and none of them can do more than that: the pass which
// read the vertices never saw the claims. An id is unique across the whole
// model rather than within a family — a claim id and a node id can never
// collide either (specification section 6.9) — so the three pairs which span
// two families are checked here, by the pass which has read all of them.
//
// Only the first definition of an id in a family is indexed, because a family
// which saw two of them reported that itself and left the id naming what it
// named first. What is left here is one definition per family, which is why an
// id appearing twice below is a collision and not a repeat of a diagnostic
// somebody has already read.
//
// Definitions are ordered by where they were written, so the one reported is
// the later of the pair whichever family it came from, and the reader is sent
// back to the one which came first. Ordering by family instead would report the
// first definition in the file as the duplicate as soon as a vertex and a node
// collided.
func (g *Graph) unique() []Diagnostic {
	defined := make(map[ID][]idDefinition)

	for id, at := range g.nodes.namedAt {
		defined[id] = append(defined[id], idDefinition{what: "a node", at: at})
	}
	for id, definition := range g.topology.defined {
		defined[id] = append(defined[id], idDefinition{
			what: article(definition.tag) + " " + definition.tag,
			at:   definition.at,
		})
	}
	for id, at := range g.claims.definedAt {
		defined[id] = append(defined[id], idDefinition{what: "a claim", at: at})
	}

	var diags []Diagnostic
	for _, id := range slices.Sorted(maps.Keys(defined)) {
		definitions := defined[id]
		if len(definitions) < 2 {
			continue
		}

		slices.SortStableFunc(definitions, func(a, b idDefinition) int { return compareSpans(a.at, b.at) })

		first := definitions[0]
		for _, definition := range definitions[1:] {
			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Span:     definition.at,
				Message: fmt.Sprintf(
					"expected an id nothing else holds, found %s, which already names something in this model",
					id,
				),
				Hint:    "an id is unique across the whole model, and is never issued again to a different thing",
				Related: []RelatedLocation{{Span: first.at, Message: "first defined here, as " + first.what}},
			})
		}
	}

	return diags
}

// compareSpans orders two spans by where they were written: by file, then by
// offset within it. It is the order [Diagnostics] reports in, applied to the
// definitions themselves.
func compareSpans(a, b Span) int {
	if c := strings.Compare(a.Start.Path, b.Start.Path); c != 0 {
		return c
	}
	return a.Start.Offset - b.Start.Offset
}

// unresolved counts the references which name nothing the model holds.
//
// It is a count of references and not of diagnostics. One id written in two
// places is two references and two diagnostics; one reference which is also of
// the wrong sort is one of each. What the number is for is the question "how
// much of this model does not join up", asked of a tree somebody is part way
// through writing, and the diagnostics are what say where.
func (g *Graph) unresolved() int {
	count := 0

	// holds reports whether an id names anything, which every reference below
	// asks of the family the reference has to reach.
	dangling := func(id ID, resolved bool) {
		if id == "" || resolved {
			return
		}
		count++
	}

	for node := range g.nodes.All() {
		if within, ok := node.Within(); ok {
			_, resolved := g.nodes.Node(within)
			dangling(within, resolved)
		}
		for _, zone := range node.MemberOf() {
			_, resolved := g.nodes.Node(zone)
			dangling(zone, resolved)
		}
		for _, loop := range node.Boundaries() {
			_, resolved := g.topology.Loop(loop)
			dangling(loop, resolved)
		}
	}

	for edge := range g.topology.Edges() {
		start, end := edge.Vertices()
		for _, vertex := range []ID{start, end} {
			_, resolved := g.topology.Vertex(vertex)
			dangling(vertex, resolved)
		}
		for _, element := range edge.BackedBy() {
			_, resolved := g.nodes.Node(element)
			dangling(element, resolved)
		}
	}

	for loop := range g.topology.Loops() {
		for _, edge := range loop.Edges() {
			_, resolved := g.topology.Edge(edge)
			dangling(edge, resolved)
		}
	}

	for claim := range g.claims.All() {
		if replacement, ok := claim.SupersededBy(); ok {
			_, resolved := g.claims.Claim(replacement)
			dangling(replacement, resolved)
		}
	}

	for frame := range g.registry.Frames() {
		dangling(frame.Parent, g.registry.Declares(SortFrame, string(frame.Parent)))

		_, resolved := g.claims.Claim(frame.Transform)
		dangling(frame.Transform, resolved)
	}

	return count
}

// Summary is what one load counted: how much of a model there is, and how much
// of it does not agree with itself or does not join up.
//
// It is counted at load rather than derived on demand because it is what a
// command prints before anything else and what a person reads to know whether
// they loaded the tree they meant to. A number which is cheap to get is a
// number which gets printed; one which costs a traversal is one which gets left
// out of the output and then out of the habit.
//
// The zero value is the summary of an empty model, which is what a tree holding
// no entity file yields.
type Summary struct {
	// nodes is how many semantic nodes were read, and byKind and byType are the
	// same nodes counted along the two axes a caller asks about in bulk. A kind
	// or a type nothing was written under is absent rather than zero.
	nodes  int
	byKind map[Kind]int
	byType map[string]int

	// vertices, edges and loops are the geometric family, counted per member.
	// The three are separate because a total tells nobody whether a model has
	// no loops or no geometry at all.
	vertices int
	edges    int
	loops    int

	// claims is how many claims were read and conflicts how many subject and
	// predicate pairs more than one live claim was written on. A conflict is a
	// finding rather than a failure: two measurements of one width disagreeing
	// is the most valuable thing in the file.
	claims    int
	conflicts int

	// unresolved is how many references name nothing the model holds.
	unresolved int
}

// Nodes returns how many semantic nodes the load read.
func (s Summary) Nodes() int { return s.nodes }

// Kinds returns the kinds the model wrote at least one node under, in the order
// [Kinds] lists them.
//
// The order is the closed set's rather than the model's, so two models of the
// same shape summarise in the same order and a summary diffs against the last
// one. A kind nothing was written under is left out rather than reported as
// zero: a list of every kind with six zeroes in it hides the one line somebody
// is reading.
func (s Summary) Kinds() []Kind {
	var out []Kind
	for _, kind := range Kinds() {
		if s.byKind[kind] > 0 {
			out = append(out, kind)
		}
	}
	return out
}

// OfKind returns how many semantic nodes declared one kind.
func (s Summary) OfKind(kind Kind) int { return s.byKind[kind] }

// Types returns the type names the model wrote at least one node under, in
// lexical order.
//
// Lexical, because a type is registry data and the registry's own order is a
// property of which file declared what — a model which moved a declaration
// between files would summarise differently while holding the same nodes.
func (s Summary) Types() []string {
	return slices.Sorted(maps.Keys(s.byType))
}

// OfType returns how many semantic nodes declared one type.
func (s Summary) OfType(name string) int { return s.byType[name] }

// Vertices returns how many vertices the load read.
func (s Summary) Vertices() int { return s.vertices }

// Edges returns how many edges the load read.
func (s Summary) Edges() int { return s.edges }

// Loops returns how many loops the load read.
func (s Summary) Loops() int { return s.loops }

// Claims returns how many claims the load read.
func (s Summary) Claims() int { return s.claims }

// Conflicts returns how many subject and predicate pairs more than one live
// claim was written on.
func (s Summary) Conflicts() int { return s.conflicts }

// Unresolved returns how many references name nothing the model holds.
func (s Summary) Unresolved() int { return s.unresolved }

// String writes the totals as the one line a command prints after a load.
//
// The two breakdowns are not in it. They are as long as the model's vocabulary,
// and a line which grows with the number of declared types is a line nobody
// reads; [Summary.Kinds] and [Summary.Types] are what a caller which wants them
// walks.
func (s Summary) String() string {
	return fmt.Sprintf(
		"%d nodes, %d vertices, %d edges, %d loops, %d claims, %d conflicts, %d unresolved",
		s.nodes, s.vertices, s.edges, s.loops, s.claims, s.conflicts, s.unresolved,
	)
}
