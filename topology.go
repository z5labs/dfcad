// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"iter"
	"slices"
)

// The top-level tags of the geometric family, one per member.
//
// The semantic family has one tag and carries its kind as a value; this one has
// a tag per member. The asymmetry is deliberate: every semantic node has the
// same shape whatever its kind, so the kind is data, while a vertex, an edge and
// a loop have genuinely different shapes, so the tag is what selects the shape.
const (
	vertexTag = "vertex"
	edgeTag   = "edge"
	loopTag   = "loop"
)

// The children of the geometric forms which are references into the family
// itself, per specification sections 6.3 and 6.4.
const (
	verticesChild = "vertices"
	edgesChild    = "edges"
)

// geometric is what every member of the geometric family carries: an id, a
// label, the frame its coordinates are expressed in, and where it was written.
//
// What it does not carry is a kind and a type. Those are not optional here and
// are not part of the shape at all — a geometric node is a different family
// rather than a semantic node with fields left out
// ([0001](docs/decisions/0001-two-node-families.md)) — so writing either is a
// load error naming the node, which the structural pass reports before this one
// interprets anything.
//
// It is embedded rather than exported because the three members do not
// generalise past these four things. Everything which makes a vertex a vertex
// and an edge an edge is the references it writes, and those have no common
// shape worth naming.
type geometric struct {
	// id is the node's id. The zero ID belongs to a node whose id was not
	// written or was not an id, which is a diagnostic carrying what was there.
	id ID

	// label is the node's display text. Empty when it was not written, which is
	// the same thing as an empty label to everything but a person reading it.
	label string

	// frame is the id of the frame the node's coordinates are expressed in.
	//
	// There is no pair reporting whether it was written, as a semantic node's
	// frame has. A geometric node is always in exactly one frame, so a form
	// without one is structurally wrong and is not interpreted at all. The zero
	// ID here belongs to a node whose `frame` child held something no id could
	// be read from, which is a diagnostic of its own.
	frame ID

	// span is where the form was written.
	span Span
}

// Vertex is a point of the geometric family, per specification section 6.2.
//
// A vertex has no coordinates of its own. Its position is a claim like any
// other, with the same predicate validation, resolution and accuracy rules, and
// is read with [LoadClaims] rather than from here. That is the whole point of
// the arrangement: two surveys of one corner are two claims on one vertex, each
// with its source, its method, its date and its accuracy, and the disagreement
// between them is what the conflict register is for. A coordinate field would
// have room for exactly one of them and no room at all for where it came from.
//
// A Vertex is read-only once loaded. The zero value is a vertex with no id and
// no frame, which is what a form nothing could be read from yields; every
// method below works on it.
type Vertex struct {
	geometric
}

// ID returns the vertex's id, which never changes for the life of the corner it
// names ([0002](docs/decisions/0002-immutable-id-mutable-label.md)).
func (v *Vertex) ID() ID { return v.id }

// Label returns the vertex's display text, which is empty when none was
// written.
func (v *Vertex) Label() string { return v.label }

// Frame returns the id of the frame the vertex's position is expressed in.
//
// There is no second result, as a semantic node's frame has, because a vertex
// is always in exactly one frame: a position means nothing without one, so a
// vertex which wrote none is structurally wrong rather than a vertex with an
// axis absent.
func (v *Vertex) Frame() ID { return v.frame }

// Span returns where the vertex form was written, which is what a diagnostic
// about the vertex as a whole points at.
func (v *Vertex) Span() Span { return v.span }

// Edge is a connection between two vertices, per specification section 6.3.
//
// # Non-straight edges
//
// An edge says which two vertices it runs between and says nothing about the
// shape of the curve between them, and that is not an omission. Curvature
// arrives as a claim under a predicate the consuming repository registers — an
// arc centre, a bulge, a radius — carrying its own source, method, date and
// accuracy like every other measurement.
//
// **The predicate registry is the extension point.** An arc is registry data
// rather than a change to this type, to the form tables, to the specification
// or to anything else compiled in: the consuming repository declares
// `(predicate arc-centre (unit m) (shape coordinate) (dimension 3) ...)`, an
// edge is written with a claim under it, and this pass reads that edge exactly
// as it reads a straight one. Nothing here has to be told which predicates mean
// curvature, which is what keeps the vocabulary out of the engine
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
//
// An Edge is read-only once loaded. The zero value is an edge with no id, no
// frame and no endpoints; every method below works on it.
type Edge struct {
	geometric

	// start and end are the ids of the two vertices the edge runs between, in
	// the order they were written. Either is the zero ID where what was written
	// was not an id, which is a diagnostic carrying what was there.
	start ID
	end   ID

	// backing are the ids the edge wrote where the id of an element which
	// physically realises it belongs, in the order they were written and with a
	// repeated one held once.
	//
	// They are the ids as written and not the elements they reached. This pass
	// has read one family, so it cannot know that an id names a semantic node,
	// that it names a vertex, or that it names nothing at all
	// ([0001](docs/decisions/0001-two-node-families.md)); each of those is a
	// diagnostic from [ResolveBoundaries].
	backing []ID
}

// ID returns the edge's id.
func (e *Edge) ID() ID { return e.id }

// Label returns the edge's display text, which is empty when none was written.
func (e *Edge) Label() string { return e.label }

// Frame returns the id of the frame the edge is expressed in.
func (e *Edge) Frame() ID { return e.frame }

// Vertices returns the ids of the two vertices the edge runs between, in the
// order they were written: start then end.
//
// The order is significant and is never sorted. An edge is directed, a loop is
// traversed through its edges in the order it names them, and an implementation
// which sorted the pair would answer "which way does this run" differently on
// each read.
//
// These are the ids as the edge wrote them. Both name vertices this model holds
// wherever it loaded clean: an id naming no vertex, and one naming a member of
// the family which is not a vertex, are each a load error rather than a state a
// caller has to interpret.
func (e *Edge) Vertices() (start, end ID) { return e.start, e.end }

// BackedBy returns the ids this edge wrote where the id of an element which
// physically realises it belongs, in the order they were written.
//
// An edge which at least one of these ids resolves for is a physical boundary;
// an edge which wrote none of them is a virtual one — the open line between a
// foyer and a dining room.
//
// **That classification is computed and never stored.** Nothing in the format
// says which an edge is, and adding an element flips the answer with no other
// edit ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
// [Boundaries.Classified] is what computes it, because resolving these ids means
// having read both families.
//
// **These are the ids as they were written, and not the elements they reached.**
// An id naming a vertex, one naming a node which is not an Element, and one
// naming nothing this model holds are each a diagnostic from [ResolveBoundaries]
// and are each still here, because a caller reporting on an edge wants to say
// what it says as well as what is wrong with it. A repeated id — which is a load
// error — is held once.
func (e *Edge) BackedBy() []ID { return slices.Clone(e.backing) }

// Span returns where the edge form was written.
func (e *Edge) Span() Span { return e.span }

// Loop is an ordered ring of edges, per specification section 6.4.
//
// A loop is what a semantic node references when it has an outline: the shape
// is shared rather than copied, so two spaces either side of a partition
// reference one edge and cannot drift apart
// ([0001](docs/decisions/0001-two-node-families.md)).
//
// A Loop is read-only once loaded. The zero value is a loop with no id, no
// frame and no edges; every method below works on it.
type Loop struct {
	geometric

	// edges are the ids of the edges the loop is traversed through, in the
	// order they were written and with a repeated one held as many times as it
	// was written.
	edges []ID

	// at is where each of those ids was written, one per entry of edges and in
	// the same order.
	//
	// The two are parallel rather than one slice of pairs because the ids are
	// what a caller reads and the spans are what a diagnostic points at, and a
	// pair would put a position into the value every caller of [Loop.Edges]
	// handles. A diagnostic about the third edge of a ring has to underline the
	// third edge of the ring: pointing at the whole form instead would quote a
	// dozen lines and say "somewhere in here".
	at []Span
}

// ID returns the loop's id.
func (l *Loop) ID() ID { return l.id }

// Label returns the loop's display text, which is empty when none was written.
func (l *Loop) Label() string { return l.label }

// Frame returns the id of the frame the loop is expressed in.
func (l *Loop) Frame() ID { return l.frame }

// Edges returns the ids of the edges the loop is traversed through, in the
// order they were written.
//
// The order is the data. It is the order the loop is traversed, it is preserved
// exactly as authored, and it is never sorted or made canonical — which is why
// the result is a slice and not a set. A repeated edge is held as many times as
// it was written, for the same reason: what the file says is what a diagnostic
// about it has to be able to quote back.
//
// An id which was not one is not here, because there was no id to keep; that it
// was written is a diagnostic carrying what was there.
func (l *Loop) Edges() []ID { return slices.Clone(l.edges) }

// Span returns where the loop form was written.
func (l *Loop) Span() Span { return l.span }

// Topology is the geometric family of one load: every vertex, edge and loop the
// walk read, in the order it read them, and each indexed by id.
//
// The name is the one the three members have together. They are the topological
// entities of a boundary representation — a point, a connection between two
// points, and a ring of connections — and what this type holds is the way they
// refer to one another rather than any coordinate, which lives on a claim.
//
// The index is the load's own, for the reason [Nodes] holds one: an id is
// unique across the whole model, so a load has to build an index to find a
// duplicate at all, and two indexes over one model would be two answers to
// "what is geom:V-01".
//
// A Topology is read-only once loaded. The zero value holds nothing, which is
// what a source tree with no geometry in it yields, and every method below works
// on it.
type Topology struct {
	// vertices, edges and loops are what was read, each in the order the walk
	// reached them.
	vertices []*Vertex
	edges    []*Edge
	loops    []*Loop

	// vertexByID, edgeByID and loopByID are what each id names.
	//
	// They are three indexes rather than one over a common type because every
	// question asked of them is about one of the three: an edge endpoint has to
	// name a vertex and a loop's traversal has to name an edge, and an index
	// which answered "some geometric node" would leave every caller to check
	// which sort it got.
	//
	// A node whose id was not written, was not an id, or was already taken is
	// absent: the first two have no id to be found by, and the third is a
	// diagnostic which leaves the id naming what it named first.
	vertexByID map[ID]*Vertex
	edgeByID   map[ID]*Edge
	loopByID   map[ID]*Loop

	// defined is what each id names and where the id was written.
	//
	// It is what a diagnostic about a reference points back at, and the pass
	// which resolves the references leaving this family needs it as much as the
	// one which resolved the references within it: a `boundary` naming a vertex
	// has to be able to say that it is a vertex, and to show where.
	defined map[ID]definition

	// backings are the `backed-by` references the edges wrote, in the order they
	// were read, with the spans a diagnostic about one needs.
	//
	// They are kept unresolved because this pass cannot resolve them. A
	// `backed-by` names a semantic node, and no loader which has read one family
	// answers a question about both
	// ([0001](docs/decisions/0001-two-node-families.md)). [ResolveBoundaries] is
	// the pass which has read both, and this is what it reads them from.
	backings []backingReference
}

// Len reports how many geometric nodes were read, of all three sorts together.
func (t *Topology) Len() int {
	if t == nil {
		return 0
	}
	return len(t.vertices) + len(t.edges) + len(t.loops)
}

// Vertices iterates the vertices in the order the walk reached them.
//
// That order is deterministic — the lexical order of the paths, and within a
// file the order the forms were written — so anything built from it diffs
// against the last run's.
func (t *Topology) Vertices() iter.Seq[*Vertex] {
	return func(yield func(*Vertex) bool) {
		if t == nil {
			return
		}
		for _, vertex := range t.vertices {
			if !yield(vertex) {
				return
			}
		}
	}
}

// Edges iterates the edges in the order the walk reached them.
func (t *Topology) Edges() iter.Seq[*Edge] {
	return func(yield func(*Edge) bool) {
		if t == nil {
			return
		}
		for _, edge := range t.edges {
			if !yield(edge) {
				return
			}
		}
	}
}

// Loops iterates the loops in the order the walk reached them.
func (t *Topology) Loops() iter.Seq[*Loop] {
	return func(yield func(*Loop) bool) {
		if t == nil {
			return
		}
		for _, loop := range t.loops {
			if !yield(loop) {
				return
			}
		}
	}
}

// Vertex returns the vertex id names, and whether the model holds one.
//
// The lookup is by index and does not scan, for the reason [Nodes.Node] is:
// every layer above resolves references by id, and a scan per reference would
// make resolving a model quadratic in the size of the thing being resolved.
//
// The zero ID names nothing, so a vertex whose id could not be read is
// reachable through [Topology.Vertices] and not through here. It is a vertex
// with a diagnostic against it rather than one anything may reference.
func (t *Topology) Vertex(id ID) (*Vertex, bool) {
	if t == nil {
		return nil, false
	}
	vertex, ok := t.vertexByID[id]
	return vertex, ok
}

// Edge returns the edge id names, and whether the model holds one.
func (t *Topology) Edge(id ID) (*Edge, bool) {
	if t == nil {
		return nil, false
	}
	edge, ok := t.edgeByID[id]
	return edge, ok
}

// Loop returns the loop id names, and whether the model holds one.
func (t *Topology) Loop(id ID) (*Loop, bool) {
	if t == nil {
		return nil, false
	}
	loop, ok := t.loopByID[id]
	return loop, ok
}

// definitionOf returns what an id names within this family and where the id was
// written, for the pass which resolves the references reaching into it.
//
// It answers about the whole family rather than about one member, which is the
// question a reference from outside asks: a `boundary` naming a vertex is
// wrong, and saying so means being able to say that a vertex is what it named.
func (t *Topology) definitionOf(id ID) (definition, bool) {
	if t == nil {
		return definition{}, false
	}
	declared, ok := t.defined[id]
	return declared, ok
}

// LoadTopology reads every geometric node beneath root, checked against
// registry.
//
// root is what [Load] takes: a single file, or a directory beneath which every
// file whose extension is [Extension] is read. Nodes come back in the order the
// walk reached them, which is deterministic, so anything built from them diffs
// against the last run's, and indexed by id, so resolving a reference does not
// scan.
//
// registry is a loaded one, because the one question this pass asks past the
// shape of a form and the references between forms — is the namespace of this
// id one the model declares — is a question only registry data answers. A nil
// registry declares nothing, and every node is then reported as minting its id
// in a namespace nothing declares, which is both true and the diagnostic
// somebody whose registry has not been written yet needs.
//
// It is a pass of its own rather than part of [LoadNodes] because the two
// families validate under different rules. A vertex is not a node missing its
// kind, and a loader which read both through one path would have to say so with
// a flag on every check it makes ([0001](docs/decisions/0001-two-node-families.md)).
//
// What a geometric node says about a value it holds is not read here. A vertex's
// position, an edge's clearance and the curvature of a non-straight edge are all
// of them claims, and claims are read by [LoadClaims] wherever they are written
// — on a semantic node, on a frame or on one of these. This pass reads the nodes
// and the references between them, which is what a claim is then about.
//
// Loading is one pass which reports everything it finds. A file which does not
// parse, a form which is structurally wrong, an id which is not one, an id
// something else already holds, a reference naming nothing and a reference
// naming the wrong sort of node are each a diagnostic, and none of them stops
// the rest of the tree from being read.
//
// Nodes which could be read come back whatever the diagnostics say, for the
// reason a node whose type is undeclared is still a node: a caller reporting on
// a tree wants to say what is there as well as what is wrong with it.
//
// The references within the family are resolved once every file has been read.
// An edge in the first file the walk reaches may run between vertices written in
// the last, and a loader which resolved as it read would report them missing for
// no reason but the order the directory happened to be listed in.
//
// The references which leave the family are not resolved here. A `backed-by` on
// an edge names a semantic node and a `boundary` on a semantic node names a
// loop, and neither pass has read both families; they are questions for the one
// which has, which for `boundary` is [ResolveBoundaries].
//
// Whether a loop closes is not asked here either. It is a question about the
// positions of the vertices its edges run between, judged against a named
// tolerance, and neither of those is something reading the references answers.
// [Topology.Assemble] is what asks it.
//
// Diagnostics come back in the order the pass found them. Collecting them into
// a [Diagnostics] is what puts them in reporting order.
func LoadTopology(root string, registry *Registry) (*Topology, []Diagnostic) {
	l := &topologyLoader{
		registry: registry,
		topology: &Topology{
			vertexByID: make(map[ID]*Vertex),
			edgeByID:   make(map[ID]*Edge),
			loopByID:   make(map[ID]*Loop),
			defined:    make(map[ID]definition),
		},
	}

	for path, err := range Walk(root) {
		if err != nil {
			l.add(diagnose(path, err))
			continue
		}

		file, err := LoadFile(path)
		if err != nil {
			l.add(diagnose(path, err))
			continue
		}

		l.file(file)
	}

	l.resolve()

	return l.topology, l.diags
}

// topologyLoader collects one load of the geometric family of a source tree.
type topologyLoader struct {
	reader

	// registry is what the nodes are judged against. It is not written to.
	registry *Registry

	// topology is what has been read so far, in the order it was reached. What
	// each id names and where it was written is kept there rather than here,
	// because the pass which resolves the references leaving this family needs
	// it too and has only the [Topology] to read it from.
	topology *Topology

	// references are the ids the geometric nodes wrote naming one another. They
	// are resolved once every file has been read, because an edge in the first
	// file the walk reaches may run between vertices written in the last.
	references []reference
}

// definition is one id this pass has seen written on a node: which sort of node
// holds it, and where the id was written.
type definition struct {
	// tag is the tag of the form which holds it, which is what a diagnostic
	// about a reference of the wrong sort names.
	tag string

	// at is where the id was written, which is what a diagnostic pointing back
	// at the definition points at.
	at Span
}

// reference is one id a geometric node wrote naming another, together with
// everything a diagnostic about it needs.
//
// The three sorts of reference the family writes within itself are one shape —
// an id which has to name a member of a named sort — so they are checked by one
// path. A second copy of "names no vertex" for edges and "names no edge" for
// loops would be two wordings of one sentence, and they would drift.
type reference struct {
	// id is the id which was written.
	id ID

	// want is the tag of the sort of node it has to name.
	want string

	// at is where it was written, which is what a diagnostic about it points
	// at.
	at Span

	// where is where the node making the reference is named, which is the other
	// end such a diagnostic points back at.
	where Span

	// by names the form which made the reference, and reads in "the edge which
	// names it is written here".
	by string

	// hint is what a diagnostic about it carries: how the reference is written,
	// which is what the author needs where the id names the wrong thing.
	hint string
}

// The hints of the two references the geometric family writes within itself.
const (
	verticesHint = "an edge runs between two vertices, written (vertices <vertex-id> <vertex-id>) in the order start then end"

	edgesHint = "a loop is a ring of edges, written (edges <edge-id> ...) in the order the loop is traversed"
)

// file interprets the geometric forms of one loaded file.
//
// A form is validated structurally before anything reads it, and one which is
// structurally wrong is not interpreted at all. That is what makes the two
// families' rules enforceable at all: a vertex which wrote a `kind` is reported
// as a node of the wrong family rather than read as a node of this one with a
// child nobody looked at.
func (l *topologyLoader) file(file *File) {
	for _, form := range file.Nodes {
		tag, ok := formTag(form)
		if !ok {
			continue
		}

		// Which shape to read the form as is settled before it is validated,
		// which is what leaves a tag this family does not hold entirely alone:
		// a node, a frame and every registry form belong to a pass which reads
		// them, and validating them here would be one tree walked twice and one
		// mistake reported twice.
		var read func(*Node)
		switch tag {
		case vertexTag:
			read = l.vertex
		case edgeTag:
			read = l.edge
		case loopTag:
			read = l.loop
		default:
			continue
		}

		if diags := Validate(&File{Path: file.Path, Nodes: []*Node{form}}); len(diags) > 0 {
			l.add(diags...)
			continue
		}

		read(form)
	}
}

// read reads what all three members carry, with the span a diagnostic about the
// node's id points at.
//
// Every axis is read whatever happened to the ones before it, because a node
// with an unreadable frame still has an id worth reporting the rest against.
// Bailing out on the first would turn fixing a file into a guessing loop.
func (l *topologyLoader) read(form *Node) (geometric, Span) {
	g := geometric{span: form.Span}

	var at Span
	if arg, ok := argument(form, 0); ok {
		if id, ok := l.id(arg, "an id"); ok {
			g.id, at = id, arg.Span
		}
	}

	if arg, ok := argumentOf(form, "label"); ok {
		g.label, _ = l.text(arg, "a string")
	}

	if arg, ok := argumentOf(form, frameTag); ok {
		g.frame, _ = l.id(arg, "a frame id")
	}

	return g, at
}

// vertex reads one structurally valid vertex form.
func (l *topologyLoader) vertex(form *Node) {
	g, at := l.read(form)

	vertex := &Vertex{geometric: g}
	l.topology.vertices = append(l.topology.vertices, vertex)

	if l.identify(vertexTag, vertex.id, at) {
		l.topology.vertexByID[vertex.id] = vertex
	}
}

// edge reads one structurally valid edge form.
func (l *topologyLoader) edge(form *Node) {
	g, at := l.read(form)

	edge := &Edge{geometric: g}
	l.topology.edges = append(l.topology.edges, edge)

	if l.identify(edgeTag, edge.id, at) {
		l.topology.edgeByID[edge.id] = edge
	}

	l.ends(edge, form, at)
	l.backing(edge, form, at)
}

// ends reads the two vertices an edge runs between.
//
// The pair is read here and checked once the whole tree has been: whether the
// two ids name vertices is a question about every file, and an edge written in
// the first one may run between vertices written in the last. Whether they are
// the same vertex is not — that is a property of the form alone, and is reported
// where it was written.
func (l *topologyLoader) ends(edge *Edge, form *Node, at Span) {
	child, ok := childForm(form, verticesChild)
	if !ok {
		return
	}

	// A structurally valid edge writes exactly two, so the ends are read by
	// position rather than by walking whatever was there.
	ends := [2]struct {
		id ID
		at Span
	}{}

	for i := range ends {
		arg, ok := argument(child, i)
		if !ok {
			continue
		}

		id, ok := l.id(arg, "a vertex id")
		if !ok {
			continue
		}

		ends[i].id, ends[i].at = id, arg.Span

		l.references = append(l.references, reference{
			id:    id,
			want:  vertexTag,
			at:    arg.Span,
			where: l.where(form, at),
			by:    edgeTag,
			hint:  verticesHint,
		})
	}

	edge.start, edge.end = ends[0].id, ends[1].id

	l.degenerate(edge, ends[0].at, ends[1].at)
}

// backing reads the ids of the elements an edge says physically realise it.
//
// They are recorded and not resolved, for the reason the endpoints are checked
// after the walk and then some: a `backed-by` names a member of the other family
// altogether, and no pass which has read one family can say whether an id names
// a member of the other ([0001](docs/decisions/0001-two-node-families.md)).
// [ResolveBoundaries] is what answers them.
func (l *topologyLoader) backing(edge *Edge, form *Node, at Span) {
	for _, child := range childForms(form, backedByChild) {
		arg, ok := argument(child, 0)
		if !ok {
			continue
		}

		element, ok := l.id(arg, "a node id")
		if !ok {
			continue
		}

		// An element named twice is a load error and is held once, so that
		// walking what backs an edge reports what realises it rather than the
		// number of times somebody wrote one of them down.
		if !slices.Contains(edge.backing, element) {
			edge.backing = append(edge.backing, element)
		}

		l.topology.backings = append(l.topology.backings, backingReference{
			edge:    edge,
			element: element,
			at:      arg.Span,
			where:   l.where(form, at),
		})
	}
}

// degenerate reports an edge whose two ends are the same vertex.
//
// It is reported against the second of the two, because that is the one which
// makes the pair a pair of one thing, and the first is where the reader is sent
// to see what it was already.
func (l *topologyLoader) degenerate(edge *Edge, first, second Span) {
	if edge.start == "" || edge.start != edge.end {
		return
	}

	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     second,
		Message: fmt.Sprintf(
			"expected the vertex %s ends at, found %s, which is the vertex it starts at",
			geometricName(edgeTag, edge.id), edge.end,
		),
		Hint: "an edge runs between two different vertices; one which starts and ends at the same vertex has no " +
			"length and no direction, and nothing bounded by it could be traversed through it",
		Related: []RelatedLocation{{Span: first, Message: "the vertex it starts at is named here"}},
	})
}

// loop reads one structurally valid loop form.
//
// The edges are kept in the order they were written, repeats included. The
// order is what the loop means — it is the order the loop is traversed — and a
// repeated edge is a loop which is not a simple cycle rather than a list with a
// duplicate in it. Sorting them, or holding each once, would lose the thing a
// diagnostic about either has to be able to quote back.
func (l *topologyLoader) loop(form *Node) {
	g, at := l.read(form)

	loop := &Loop{geometric: g}
	l.topology.loops = append(l.topology.loops, loop)

	if l.identify(loopTag, loop.id, at) {
		l.topology.loopByID[loop.id] = loop
	}

	child, ok := childForm(form, edgesChild)
	if !ok {
		return
	}

	written, _ := split(elements(child))
	for _, arg := range written {
		id, ok := l.id(arg, "an edge id")
		if !ok {
			continue
		}

		loop.edges = append(loop.edges, id)
		loop.at = append(loop.at, arg.Span)

		l.references = append(l.references, reference{
			id:    id,
			want:  edgeTag,
			at:    arg.Span,
			where: l.where(form, at),
			by:    loopTag,
			hint:  edgesHint,
		})
	}
}

// where is what a diagnostic about a reference points back at: the node which
// made it, named by its own id where it wrote one.
//
// A related location spanning the whole node quotes a dozen lines to point at
// one, so a node with no id to point at is pointed at by its tag rather than by
// the form it opens.
func (l *topologyLoader) where(form *Node, at Span) Span {
	if at != (Span{}) {
		return at
	}
	return tagSpan(form)
}

// identify checks a geometric node's id: that its namespace is one the registry
// declares, and that nothing else this pass has read already holds it. It
// reports whether the id is this node's to be indexed under.
//
// A node whose id could not be read is not checked here. It has no namespace to
// look up and nothing to collide with, and both diagnostics would be about the
// id the author has already been told is not one.
//
// The ids this pass can see are the geometric ones and the frames, which is
// what it checks against. There is one id space for the whole model, so a
// vertex may also collide with a semantic node — and that is a question for the
// pass which has read both families rather than one this can answer by reading
// half of them.
func (l *topologyLoader) identify(tag string, id ID, at Span) bool {
	if at == (Span{}) {
		return false
	}

	l.registered(l.registry, id, at)

	// A frame is a node as much as a registry entry, and the registry resolves
	// before any of this is read, so a frame holding an id always held it first.
	if frame, ok := l.registry.Frame(id); ok {
		l.taken(id, at, frame.Span, "first defined here, as a frame")
		return false
	}

	if first, ok := l.topology.defined[id]; ok {
		l.taken(id, at, first.at, fmt.Sprintf("first defined here, as %s %s", article(first.tag), first.tag))
		return false
	}

	l.topology.defined[id] = definition{tag: tag, at: at}
	return true
}

// taken reports an id which already names something, pointing at both.
func (l *topologyLoader) taken(id ID, at, first Span, what string) {
	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     at,
		Message: fmt.Sprintf(
			"expected an id nothing else holds, found %s, which already names something in this model",
			id,
		),
		Hint:    "an id is unique across the whole model, and is never issued again to a different thing",
		Related: []RelatedLocation{{Span: first, Message: what}},
	})
}

// resolve checks every reference the geometric family wrote within itself, once
// every file has been read.
func (l *topologyLoader) resolve() {
	for _, written := range l.references {
		l.refers(written)
	}
}

// refers checks one reference: that it names something this pass read, and that
// what it names is the sort of node the reference is for.
//
// The two are ordered rather than independent because they build on each other:
// an id naming nothing has no sort to report, and saying that a vertex which
// does not exist is not an edge would be true of every id nobody wrote.
func (l *topologyLoader) refers(written reference) {
	declared, ok := l.topology.defined[written.id]
	if !ok {
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     written.at,
			Message: fmt.Sprintf(
				"expected %s %s id something in this model holds, found %s, which names no %s",
				article(written.want), written.want, written.id, written.want,
			),
			Hint:    written.hint,
			Related: []RelatedLocation{{Span: written.where, Message: "the " + written.by + " which names it is written here"}},
		})
		return
	}

	if declared.tag == written.want {
		return
	}

	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     written.at,
		Message: fmt.Sprintf(
			"expected %s %s id, found %s, which is %s %s",
			article(written.want), written.want, written.id, article(declared.tag), declared.tag,
		),
		Hint:    written.hint,
		Related: []RelatedLocation{{Span: declared.at, Message: fmt.Sprintf("the %s it names is written here", declared.tag)}},
	})
}

// geometricName names a geometric node for a diagnostic.
//
// A node whose id could not be read is named by what it is rather than by the
// id it does not have. Every diagnostic which uses this carries the span as
// well, which is what says which node is meant when the name does not.
func geometricName(tag string, id ID) string {
	if id == "" {
		return "the " + tag
	}
	return string(id)
}
