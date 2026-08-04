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

// nodeTag is the top-level tag of the one semantic form. The geometric family
// has a tag per member; the semantic family has this one and carries its kind
// as a value.
const nodeTag = "node"

// SemanticNode is one node of the semantic family, per specification section
// 6.1.
//
// A semantic node is described by a small fixed set of axes rather than by a
// class per concept: a kind, a type, and — where it has them — a geometry form
// and a frame. That is what keeps this API the size of the axes rather than the
// size of the vocabulary. A consuming repository which needs a hundred and
// fifty types writes a hundred and fifty registry entries and no Go types
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
//
// The two axes which may be absent are absent as a state and not as a
// degenerate value. A circuit group, a warranty and a system have no geometry
// and no frame, and none of them is malformed; representing them without a
// special case is the point of the design
// ([0001](docs/decisions/0001-two-node-families.md)).
// [SemanticNode.Geometry] and [SemanticNode.Frame] therefore each report
// whether the node has that axis, which is what tells a node with no geometry
// apart from one whose geometry form is the empty string — a thing the closed
// set has no member for and which no file can produce.
//
// The fields are unexported and read through the methods below. The tree a node
// was read from is not part of its interface: a caller which reached through to
// it would be reading the file a second time, in a second way, and the two
// readings would disagree the first time canonical form moved a child.
//
// A SemanticNode is read-only once loaded. The zero value is a node with no id,
// no kind and no type, which is what a form nothing could be read from yields;
// every method below works on it.
type SemanticNode struct {
	// id is the node's id. The zero ID belongs to a node whose id was not
	// written or was not an id, which is a diagnostic carrying what was there.
	id ID

	// label is the node's display text. Empty when it was not written, which is
	// the same thing as an empty label to everything but a person reading it.
	label string

	// kind is the node's kind, which is always one of the closed set.
	kind Kind

	// declaredType is the name of the type the node declares. The field is not
	// called type because that is a keyword, and the accessor is, because the
	// accessor is what a caller reads.
	declaredType string

	// geometry is the node's geometry form, and hasGeometry whether it has one.
	// A `geometry` child which no form could be read from leaves both as they
	// are, because the alternative is an empty Geometry reported as present —
	// the one value the closed set has no member for. That the child was there
	// is not lost: reading it failed, which is a diagnostic, and the pass
	// tracks the child's presence separately for the checks that need it.
	geometry    Geometry
	hasGeometry bool

	// frame is the id of the frame the node's geometry is expressed in, and
	// hasFrame whether it has one. A `frame` child which no id could be read
	// from leaves both as they are, for the reason above: an empty frame id
	// reported as present names no frame.
	frame    ID
	hasFrame bool

	// within is the id of the node which strictly contains this one, and
	// hasWithin whether it has one. A node with no parent is ordinary: a site is
	// the root of its hierarchy, and a circuit group sits in no hierarchy at all.
	// A `within` child which no id could be read from leaves both as they are,
	// for the reason [SemanticNode.Frame]'s pair does.
	within    ID
	hasWithin bool

	// zones are the ids of the zones this node declares membership of, in the
	// order they were written and with a repeated one written once. Membership
	// is many to many, so the field is a slice and its emptiness is ordinary.
	zones []ID

	// span is where the node form was written.
	span Span
}

// ID returns the node's id, which never changes for the life of the thing it
// names ([0002](docs/decisions/0002-immutable-id-mutable-label.md)).
func (n *SemanticNode) ID() ID { return n.id }

// Label returns the node's display text, which is empty when none was written.
//
// A label is free-form and carries no uniqueness guarantee. Nothing resolves
// through it and nothing in the engine reads it, so changing it changes the
// label and nothing else about the node: a rename is a one-line diff and not a
// re-identification.
func (n *SemanticNode) Label() string { return n.label }

// Kind returns the node's kind, which is one of the seven [Kinds] compiled into
// the engine.
func (n *SemanticNode) Kind() Kind { return n.kind }

// Type returns the name of the node's type, which the consuming repository's
// registry declares. The engine attaches no meaning to it beyond the axes that
// declaration permits.
func (n *SemanticNode) Type() string { return n.declaredType }

// Geometry returns the node's geometry form, and whether it has one.
//
// A node with no geometry is ordinary rather than incomplete, so the second
// result is a state and not a failure. It is false when the node wrote no
// `geometry` child, which the node's type has to permit, and also when what it
// wrote was not a geometry form — that is a diagnostic of its own, and there is
// no form to report.
func (n *SemanticNode) Geometry() (Geometry, bool) { return n.geometry, n.hasGeometry }

// Frame returns the id of the node's frame, and whether it has one.
//
// A node is declared in at most one frame; declaring it in two is
// unrepresentable, which is the point. A node with no geometry usually has no
// frame either, and a node may legitimately have neither. The second result is
// false on the same two occasions [SemanticNode.Geometry]'s is: no `frame`
// child, or one no id could be read from.
func (n *SemanticNode) Frame() (ID, bool) { return n.frame, n.hasFrame }

// Within returns the id of the node which strictly contains this one, and
// whether it wrote one.
//
// Containment nests strictly, so there is at most one: a node is written inside
// one other node, never two. A node with no parent is ordinary rather than
// incomplete, which is why the second result is a state and not a failure.
//
// This is the id as the node wrote it. [Nodes.Within] resolves it to the node,
// labelled with the relation which produced it, and is what a traversal uses.
func (n *SemanticNode) Within() (ID, bool) { return n.within, n.hasWithin }

// MemberOf returns the ids of the zones this node declares membership of, in
// the order they were written.
//
// Membership is many to many and unconstrained in its count: a node belongs to
// as many zones as it says, and one belonging to none is as ordinary as one
// belonging to three. Nothing here reads the containment references, so a
// node's zones are its own and never inherited from what contains it.
//
// These are the ids as the node wrote them, with a repeated one — which is a
// load error — held once. [Nodes.Zones] resolves them to the zones, labelled
// with the relation which produced them.
func (n *SemanticNode) MemberOf() []ID { return slices.Clone(n.zones) }

// Span returns where the node form was written, which is what a diagnostic
// about the node as a whole points at.
func (n *SemanticNode) Span() Span { return n.span }

// Nodes are the semantic nodes of one load: every node the walk read, in the
// order it read them, and indexed by id.
//
// The index is the load's own. A load has to build one anyway — an id is unique
// across the whole model, and finding a duplicate means having somewhere to
// look the id up — so handing it back costs nothing and saves every caller
// above building a second one. Two indexes over one model would be two answers
// to "what is site:S-101", and they would differ the first time a node moved
// between files.
//
// A Nodes is read-only once loaded. The zero value holds no nodes, which is
// what a source tree holding none yields, and every method below works on it.
type Nodes struct {
	// inOrder is every node read, in the order the walk reached them.
	inOrder []*SemanticNode

	// byID is the node each id names. A node whose id was not written, was not
	// an id, or was already taken is absent: the first two have no id to be
	// found by, and the third is a diagnostic which leaves the id naming what it
	// named first.
	byID map[ID]*SemanticNode

	// contained is the nodes written directly within the node each id names, in
	// the order the walk read them. The reference is written on the contained
	// node, so this direction is written nowhere and is indexed at load.
	contained map[ID][]*SemanticNode

	// members is the nodes which declared membership of the zone each id names,
	// in the order the walk read them, and is indexed for the reason above.
	members map[ID][]*SemanticNode
}

// Len reports how many nodes were read.
func (n *Nodes) Len() int {
	if n == nil {
		return 0
	}
	return len(n.inOrder)
}

// All iterates the nodes in the order the walk reached them.
//
// That order is deterministic — the lexical order of the paths, and within a
// file the order the forms were written — so anything built from it diffs
// against the last run's.
func (n *Nodes) All() iter.Seq[*SemanticNode] {
	return func(yield func(*SemanticNode) bool) {
		if n == nil {
			return
		}
		for _, node := range n.inOrder {
			if !yield(node) {
				return
			}
		}
	}
}

// Node returns the node id names, and whether the model holds one.
//
// The lookup is by index and does not scan. Every layer above resolves
// references by id — containment, zone membership, boundaries, supersession —
// and a scan per reference would make resolving a model quadratic in the size
// of the thing being resolved.
//
// The zero ID names nothing, so a node whose id could not be read is reachable
// through [Nodes.All] and not through here. It is a node with a diagnostic
// against it rather than a node anything may reference.
func (n *Nodes) Node(id ID) (*SemanticNode, bool) {
	if n == nil {
		return nil, false
	}
	node, ok := n.byID[id]
	return node, ok
}

// LoadNodes reads every semantic node beneath root, checked against registry.
//
// root is what [Load] takes: a single file, or a directory beneath which every
// file whose extension is [Extension] is read. Nodes come back in the order the
// walk reached them, which is deterministic, so anything built from them diffs
// against the last run's, and indexed by id, so resolving a reference does not
// scan.
//
// registry is a loaded one, because the questions this pass asks — is this type
// declared, and does it permit the kind and the geometry form written here —
// are ones only registry data answers. That is also why the two loads are two
// calls: a registry is a property of the whole source tree, so every file has
// to have been read before any node is judged against it. A nil registry
// declares nothing, and every node is then reported as naming a type nothing
// declares, which is both true and the diagnostic somebody whose registry has
// not been written yet needs.
//
// Loading is one pass which reports everything it finds. A file which does not
// parse, a form which is structurally wrong, an axis which is not a member of
// its closed set, a type nothing declares and a type which does not permit what
// the node wrote are each a diagnostic, and none of them stops the rest of the
// tree from being read.
//
// Nodes which could be read come back whatever the diagnostics say. A node
// whose type is undeclared is still a node with an id, a kind and a label, and
// a caller reporting on a tree wants to say so rather than to lose it.
//
// An id is unique across the whole model, so a second definition of one is a
// diagnostic naming both with their positions. The id goes on naming what it
// named first: an id which moved to the later definition would be an id which
// changed what it means, which is the one thing an id never does
// ([0002](docs/decisions/0002-immutable-id-mutable-label.md)).
//
// The two relations a node writes to other nodes are read here and checked once
// the whole tree has been: containment, which nests strictly, and zone
// membership, which does not. A reference naming no node, a nesting the
// hierarchy does not permit, a containment cycle and a `member-of` naming
// something which is not a Zone are each a diagnostic. They are checked after
// the walk rather than during it because a node in the first file it reaches may
// be written inside one in the last. [Nodes.Within], [Nodes.Contains],
// [Nodes.Zones] and [Nodes.Members] are what read them back.
//
// The geometric family — `vertex`, `edge` and `loop` — is not read here. Those
// carry neither kind nor type and validate under their own rules
// ([0001](docs/decisions/0001-two-node-families.md)).
//
// Diagnostics come back in the order the pass found them. Collecting them into
// a [Diagnostics] is what puts them in reporting order.
func LoadNodes(root string, registry *Registry) (*Nodes, []Diagnostic) {
	l := &nodeLoader{
		registry: registry,
		nodes:    &Nodes{byID: make(map[ID]*SemanticNode)},
		defined:  make(map[ID]Span),
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

	l.relate()

	return l.nodes, l.diags
}

// nodeLoader collects one load of the semantic nodes of a source tree.
type nodeLoader struct {
	reader

	// registry is what the nodes are judged against. It is not written to.
	registry *Registry

	// nodes are the nodes read so far, in the order they were reached.
	nodes *Nodes

	// defined is where the node holding each id was written, which is what a
	// duplicate points its reader back at.
	defined map[ID]Span

	// containments and memberships are the two relations as they were written.
	// Both are checked once every file has been read, because a node in the
	// first file the walk reaches may be written inside one in the last, and a
	// loader which resolved as it read would report it missing for no reason but
	// the order the directory happened to be listed in.
	containments []containment
	memberships  []membership
}

// file interprets the node forms of one loaded file.
//
// A form is validated structurally before anything reads it, and one which is
// structurally wrong is not interpreted at all. Reading a node which is missing
// its `kind` would mean either inventing a kind or reporting its absence a
// second time, and both are worse than the diagnostic the structural pass
// already produced.
func (l *nodeLoader) file(file *File) {
	for _, node := range file.Nodes {
		if tag, ok := formTag(node); !ok || tag != nodeTag {
			continue
		}

		if diags := Validate(&File{Path: file.Path, Nodes: []*Node{node}}); len(diags) > 0 {
			l.add(diags...)
			continue
		}

		l.declare(node)
	}
}

// nodeDeclaration is one node together with where the axes the registry has
// something to say about were written.
//
// A span left zero belongs to an axis which was not written, or which was
// written as something no value could be read from. Neither is checked against
// the registry: the first is already an arity diagnostic and the second is
// already reported as what it was, and a type which then also says the axis is
// not one it permits reports one mistake twice.
type nodeDeclaration struct {
	node *SemanticNode

	id           Span
	kind         Span
	geometry     Span
	declaredType Span

	// wroteGeometry reports whether a `geometry` child was written at all,
	// which is what tells a node with no geometry apart from one whose geometry
	// form could not be read.
	wroteGeometry bool
}

// declare reads one structurally valid node form.
//
// Every axis is read whatever happened to the ones before it, because a node
// with a misspelled kind still has a type worth checking and an id worth
// reporting the rest against. Bailing out on the first would turn fixing a file
// into a guessing loop.
func (l *nodeLoader) declare(form *Node) {
	d := nodeDeclaration{node: &SemanticNode{span: form.Span}}

	if arg, ok := argument(form, 0); ok {
		if id, ok := l.id(arg, "an id"); ok {
			d.node.id, d.id = id, arg.Span
		}
	}

	if arg, ok := argumentOf(form, "label"); ok {
		d.node.label, _ = l.text(arg, "a string")
	}

	if arg, ok := argumentOf(form, "kind"); ok {
		if kind, ok := l.kind(arg); ok {
			d.node.kind, d.kind = kind, arg.Span
		}
	}

	if arg, ok := argumentOf(form, "type"); ok {
		if name, ok := l.symbol(arg, "a type name"); ok {
			d.node.declaredType, d.declaredType = name, arg.Span
		}
	}

	if arg, ok := argumentOf(form, "geometry"); ok {
		d.wroteGeometry = true
		if geometry, ok := l.geometry(arg); ok {
			d.node.geometry, d.node.hasGeometry, d.geometry = geometry, true, arg.Span
		}
	}

	if arg, ok := argumentOf(form, "frame"); ok {
		d.node.frame, d.node.hasFrame = l.id(arg, "a frame id")
	}

	l.relations(d, form)

	l.identify(d)
	l.permitted(d)

	l.nodes.inOrder = append(l.nodes.inOrder, d.node)
}

// relations reads the two relations a node writes to other nodes — the one node
// which contains it, and the zones it is a member of — and records each of them
// to be checked once every file has been read.
//
// The two are read together and kept apart, which is the whole point of them.
// They are different relations with different shapes: containment is at most one
// and nests, membership is any number and does not, and neither is ever derived
// from the other.
//
// Nothing is resolved here. Whether the ids name nodes this model holds, whether
// the hierarchy permits the pairing and whether following the containment
// terminates are questions about the whole source tree, and [nodeLoader.relate]
// asks them once it has been read.
func (l *nodeLoader) relations(d nodeDeclaration, form *Node) {
	// A diagnostic about a reference points back at the node making it, named by
	// its own id where it wrote one: a related location spanning the whole node
	// quotes a dozen lines to point at one.
	where := d.id
	if where == (Span{}) {
		where = tagSpan(form)
	}

	if arg, ok := argumentOf(form, "within"); ok {
		if within, ok := l.id(arg, "a node id"); ok {
			d.node.within, d.node.hasWithin = within, true
			l.containments = append(l.containments, containment{node: d.node, at: arg.Span, where: where})
		}
	}

	for _, child := range childForms(form, "member-of") {
		arg, ok := argument(child, 0)
		if !ok {
			continue
		}

		zone, ok := l.id(arg, "a zone node id")
		if !ok {
			continue
		}

		// A zone named twice is a load error and is held once, so that a
		// traversal of the node's zones reports the relation rather than the
		// number of times somebody wrote it.
		if !slices.Contains(d.node.zones, zone) {
			d.node.zones = append(d.node.zones, zone)
		}

		l.memberships = append(l.memberships, membership{node: d.node, zone: zone, at: arg.Span, where: where})
	}
}

// identify checks a node's id: that its namespace is one the registry declares,
// and that nothing else in the model already holds it.
//
// A node whose id could not be read is not checked here. It has no namespace to
// look up and nothing to collide with, and both diagnostics would be about the
// id the author has already been told is not one.
func (l *nodeLoader) identify(d nodeDeclaration) {
	if d.id == (Span{}) {
		return
	}

	l.registered(l.registry, d.node.id, d.id)

	// A frame is a node as much as a registry entry, and there is one id space
	// for the whole model, so an id declared as a frame is taken. The registry
	// resolves before any node is read, so a frame holding an id always held it
	// first, and there is nothing a second diagnostic about the same id adds.
	if frame, ok := l.registry.Frame(d.node.id); ok {
		l.taken(d, frame.Span, "first defined here, as a frame")
		return
	}

	if first, ok := l.defined[d.node.id]; ok {
		l.taken(d, first, "first defined here")
		return
	}

	l.defined[d.node.id] = d.id
	l.nodes.byID[d.node.id] = d.node
}

// taken reports an id which already names something, pointing at both.
func (l *nodeLoader) taken(d nodeDeclaration, first Span, what string) {
	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     d.id,
		Message: fmt.Sprintf(
			"expected an id nothing else holds, found %s, which already names something in this model",
			d.node.id,
		),
		Hint:    "an id is unique across the whole model, and is never issued again to a different thing",
		Related: []RelatedLocation{{Span: first, Message: what}},
	})
}

// kind reads the one member of the closed set of kinds a node declares.
func (l *nodeLoader) kind(arg *Node) (Kind, bool) {
	written, ok := l.symbol(arg, "a kind")
	if !ok {
		return "", false
	}

	kind := Kind(written)
	if !slices.Contains(kinds, kind) {
		l.add(unknownKind(arg.Span, written))
		return "", false
	}

	return kind, true
}

// geometry reads the one member of the closed set of geometry forms a node
// declares.
//
// `absent` is rejected here as firmly as a misspelling is. It is the type
// registry's word for "an instance may omit this child", and a node which
// writes it is naming absence rather than expressing it — which would leave the
// format with two spellings of one state and every reader of it choosing
// between them.
func (l *nodeLoader) geometry(arg *Node) (Geometry, bool) {
	written, ok := l.symbol(arg, "a geometry form")
	if !ok {
		return "", false
	}

	geometry := Geometry(written)
	if written == absentGeometry || !slices.Contains(geometries, geometry) {
		l.add(unknownGeometry(arg.Span, written))
		return "", false
	}

	return geometry, true
}

// permitted checks a node's type against the registry, and then the node's kind
// and geometry against what that type permits.
//
// The three checks are ordered rather than independent because they build on
// each other: a type nothing declares permits nothing, and reporting a kind as
// not permitted by a type which does not exist says nothing anybody can act on.
func (l *nodeLoader) permitted(d nodeDeclaration) {
	if d.declaredType == (Span{}) {
		return
	}

	declared, ok := l.registry.Type(d.node.declaredType)
	if !ok {
		l.add(l.registry.Undeclared(SortType, d.node.declaredType, d.declaredType))
		return
	}

	// Every diagnostic below names all three: the node, the type, and the value
	// the type does not permit. Two of the three are in the message and the
	// third is the source line the span quotes; the type's own declaration is a
	// related location, because the fix is as likely to be there as here.
	at := RelatedLocation{Span: declared.Span, Message: "the type is declared here"}

	if d.kind != (Span{}) && !declared.PermitsKind(d.node.kind) {
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     d.kind,
			Message: fmt.Sprintf(
				"expected a kind the type %s permits, found %s on %s",
				declared.Name, d.node.kind, d.node.id,
			),
			Hint:    fmt.Sprintf("%s permits %s", declared.Name, join(spellings(declared.Kinds), "and")),
			Related: []RelatedLocation{at},
		})
	}

	switch {
	case d.geometry != (Span{}) && !declared.PermitsGeometry(d.node.geometry):
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     d.geometry,
			Message: fmt.Sprintf(
				"expected a geometry form the type %s permits, found %s on %s",
				declared.Name, d.node.geometry, d.node.id,
			),
			Hint:    fmt.Sprintf("%s permits %s", declared.Name, declared.permittedGeometry()),
			Related: []RelatedLocation{at},
		})

	// A node with no geometry is ordinary only where its type says so. The
	// registry spells that permission `absent`, and a type which does not list
	// it is a type every instance of which has a shape.
	case !d.wroteGeometry && !declared.Absent:
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     d.node.span,
			Message: fmt.Sprintf(
				"expected a (geometry ...) child of %s, found none, which the type %s does not permit",
				d.node.id, declared.Name,
			),
			Hint:    fmt.Sprintf("%s permits %s; a type permits an instance to omit its geometry by declaring (geometry absent)", declared.Name, declared.permittedGeometry()),
			Related: []RelatedLocation{at},
		})
	}
}
