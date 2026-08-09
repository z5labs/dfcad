// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	sexpr "github.com/z5labs/sexpr-go"
)

// frameChild is the child every geometric node writes to say which frame its
// coordinates are expressed in. A geometric node is always in exactly one
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
const frameChild = "frame"

// The marks a scaffolded id carries between its namespace and its ordinal.
//
// They are the tags of the three geometric forms rather than initials somebody
// chose, so that the minting convention introduces no vocabulary of its own:
// `geom:vertex-1` is read off the form it names. Nothing is ever inferred back
// out of one — it is a name and not a schema, the rule every id in this model
// is held to ([0002](docs/decisions/0002-immutable-id-mutable-label.md)).
const (
	vertexMark = vertexTag
	edgeMark   = edgeTag
	loopMark   = loopTag
)

// ParseCorner reads one corner of a scaffold's list, in the shape the position
// predicate declares.
//
// It is [ParseValue] with one difference, and the difference is the whole
// reason it exists: a corner written with no unit is read in the unit the
// predicate declares rather than in none. A claim's unit is an axis of the
// claim — the author says what they measured in, and a unit other than the
// declared one is the mistake the check exists to catch — but a corner is not a
// claim somebody wrote. It is a coordinate in a frame, and the only unit it may
// legally be in is the one the predicate declares, so requiring it to be
// written is requiring a flag whose one permitted value is already known.
//
// A unit which was written is read as written and is still held to the
// declaration, so a corner list typed in the wrong unit is refused exactly as
// it was.
func ParseCorner(written string, unit Unit, declared Predicate) (Value, error) {
	if unit == "" {
		unit = declared.Unit
	}
	return ParseValue(written, unit, declared)
}

// ErrNoFrame is a geometric node written without one.
//
// It is not optional as a semantic node's frame is. A point with no frame is
// not a point with an axis left out; it is a coordinate nobody can locate
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
var ErrNoFrame = errors.New("a geometric node is written in exactly one frame")

// ErrNoEndpoints is an edge written without both of the vertices it runs
// between.
var ErrNoEndpoints = errors.New("an edge runs between two vertices, written start then end")

// ErrNoEdges is a loop written with no edges to traverse.
var ErrNoEdges = errors.New("a loop is traversed through one or more edges, in order")

// ErrNoNamespace is a scaffold with no id namespace to mint into.
var ErrNoNamespace = errors.New("scaffolding mints ids, which are minted in a declared namespace")

// ErrNoCorners is a scaffold with no corner list.
//
// It is the corners which are the room: a scaffold with none is a request to
// lay out nothing, and reporting it as a list which does not close would answer
// the wrong question.
var ErrNoCorners = errors.New("scaffolding a loop is written with the corners of the loop, in order and closed")

// NotOfFamilyError reports an id which names a member of the model other than
// the one the reference required.
//
// It is separate from [NotANodeError], which is about the one operation only
// the semantic family has. This one is about a reference between geometric
// nodes: an edge names two vertices and a loop names edges, and an id which
// reached the wrong one of the three is a mistake about which thing was meant
// rather than about which family it belongs to.
type NotOfFamilyError struct {
	// ID is the id which was written.
	ID ID

	// Want is the tag the reference required: `vertex` or `edge`.
	Want string

	// Got is the tag the model wrote it as.
	Got string
}

// Error implements the [error] interface.
func (e NotOfFamilyError) Error() string {
	return fmt.Sprintf(
		"%s names %s %s, and %s %s was required",
		e.ID, article(e.Got), e.Got, article(e.Want), e.Want,
	)
}

// SelfLoopError reports an edge written between one vertex and itself.
type SelfLoopError struct {
	// ID is the edge which was being written.
	ID ID

	// Vertex is the vertex written at both ends of it.
	Vertex ID
}

// Error implements the [error] interface.
func (e SelfLoopError) Error() string {
	return fmt.Sprintf("%s runs from %s to itself, which is not an edge", e.ID, e.Vertex)
}

// ToleranceUnitError reports a tolerance declared in a unit other than the one
// of the frame it is being applied in.
//
// Nothing converts between units
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)). A tolerance in
// millimetres silently scaled to judge a frame in metres is the mistake that
// decision exists to prevent, and one applied without scaling would be a
// thousand times the number somebody wrote down.
type ToleranceUnitError struct {
	// Tolerance is the declaration which was reached for.
	Tolerance Tolerance

	// Frame is the frame it was to be applied in.
	Frame ID

	// Want is the frame's unit, which is what it had to be declared in.
	Want Unit
}

// Error implements the [error] interface.
func (e ToleranceUnitError) Error() string {
	return fmt.Sprintf(
		"expected the tolerance %s in %s, which is the unit of the frame %s, found it in %s",
		e.Tolerance.Name, e.Want, e.Frame, e.Tolerance.Unit,
	)
}

// TooFewCornersError reports a corner list which cannot describe a ring.
//
// A closed list names its first corner twice, so the shortest one there is has
// four entries and describes a triangle. Anything shorter is not a loop which
// failed to close; it is a list which never described one.
type TooFewCornersError struct {
	// Corners is how many were written.
	Corners int
}

// Error implements the [error] interface.
func (e TooFewCornersError) Error() string {
	return fmt.Sprintf(
		"%d corners describe no loop: a closed list names its first corner again at the end, "+
			"so the shortest there is holds four and describes a triangle",
		e.Corners,
	)
}

// UnclosedLoopError reports a corner list whose last corner does not return to
// its first.
//
// A closed list is the whole of what says the author finished the room. A tool
// which quietly joined the last corner back to the first could not tell "I have
// written the outline" from "I stopped typing halfway", and the wall it invented
// would be in no diagnostic anywhere.
type UnclosedLoopError struct {
	// Last is the index of the final corner, counted from one as a person
	// counts a list.
	Last int

	// Gap is how far that corner is from the first, in the frame's unit, and
	// is negative where it could not be measured at all.
	Gap float64

	// Unit is the unit the gap is in, which is the frame's.
	Unit Unit

	// Tolerance is the declaration the gap was judged against.
	Tolerance Tolerance
}

// Error implements the [error] interface.
func (e UnclosedLoopError) Error() string {
	if e.Gap < 0 {
		return fmt.Sprintf(
			"the corner list does not close: corner %d cannot be compared with corner 1, "+
				"so nothing says the outline returned to where it started",
			e.Last,
		)
	}

	return fmt.Sprintf(
		"the corner list does not close: corner %d is %s %s from corner 1, and the tolerance %s permits %s %s",
		e.Last, decimal(e.Gap), e.Unit, e.Tolerance.Name, decimal(e.Tolerance.Value), e.Tolerance.Unit,
	)
}

// CollapsedRingError reports a corner list two of whose corners are one point.
//
// It is refused rather than quietly folded away. Two corners at one point are
// either a coordinate typed twice or an outline which doubles back on itself,
// and a scaffold which dropped one of them would write a room with a wall
// missing and report having written the room that was asked for.
type CollapsedRingError struct {
	// First and Second are the indices of the two corners, counted from one.
	First, Second int

	// Vertex is the vertex both of them are at.
	Vertex ID

	// Tolerance is the declaration they were judged coincident against.
	Tolerance Tolerance
}

// Error implements the [error] interface.
func (e CollapsedRingError) Error() string {
	return fmt.Sprintf(
		"corners %d and %d are both at %s, within the tolerance %s: a ring visits each of its corners once",
		e.First, e.Second, e.Vertex, e.Tolerance.Name,
	)
}

// VertexSpec is a vertex which does not exist yet: the axes it will be written
// with.
//
// The position is part of it rather than a claim added afterwards because a
// vertex is written into a file and a claim is written inside the form of its
// subject: a vertex and the first thing anybody knows about where it is are one
// change or they are two, and two leaves a vertex with no position in the model
// in between.
type VertexSpec struct {
	// ID is the id it will be written with. Its namespace must be one the
	// registry declares.
	ID ID

	// Label is its display text, and is empty for a vertex with none.
	Label string

	// Frame is the frame its position is expressed in, which the registry must
	// declare. A vertex is always in exactly one.
	Frame ID

	// Position is the claim saying where it is, and is written only where it
	// names a predicate. Its subject is this vertex whatever it holds.
	//
	// A vertex with no position claim is ordinary rather than incomplete: a
	// corner somebody has named and not yet surveyed is a corner whose position
	// is unknown, which is a state this model can hold and a coordinate of zero
	// is not.
	Position ClaimSpec
}

// Check reports the first axis of the spec which the registry does not permit.
func (spec VertexSpec) Check(registry *Registry) error {
	if err := checkGeometric(registry, spec.ID, spec.Frame); err != nil {
		return err
	}

	if spec.Position.Predicate == "" {
		return nil
	}

	position := spec.Position
	position.Subject = spec.ID

	return position.Check(registry)
}

// Destination is the file the vertex goes in, decided exactly as
// [NodeSpec.Destination] decides a node's.
func (spec VertexSpec) Destination(registry *Registry, override string) (Destination, error) {
	if err := spec.Check(registry); err != nil {
		return Destination{}, err
	}
	return geometricDestination(registry, spec.ID, override)
}

// form is the vertex as it will be written.
//
// The children are written in the order specification section 6.2 tables them,
// which decides nothing: canonical form sorts the children of every form.
func (spec VertexSpec) form() *Node {
	children := []*Node{symbolNode(string(spec.ID))}

	if spec.Label != "" {
		children = append(children, formNode(labelChild, stringNode(spec.Label)))
	}

	children = append(children, formNode(frameChild, symbolNode(string(spec.Frame))))

	if spec.Position.Predicate != "" {
		position := spec.Position
		position.Subject = spec.ID
		children = append(children, position.form())
	}

	return formNode(vertexTag, children...)
}

// EdgeSpec is an edge which does not exist yet.
type EdgeSpec struct {
	// ID is the id it will be written with.
	ID ID

	// Label is its display text, and is empty for an edge with none.
	Label string

	// Frame is the frame it is expressed in.
	Frame ID

	// Start and End are the vertices it runs between, in that order. The order
	// is significant and is never sorted: an edge is directed, and the two
	// regions either side of it traverse it opposite ways.
	Start, End ID

	// BackedBy are the ids of the semantic nodes which physically realise it,
	// in the order they were given. An edge which names none is virtual, which
	// is computed from the references rather than written
	// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
	BackedBy []ID
}

// Check reports the first axis of the spec which the registry does not permit.
func (spec EdgeSpec) Check(registry *Registry) error {
	if err := checkGeometric(registry, spec.ID, spec.Frame); err != nil {
		return err
	}

	if spec.Start == "" || spec.End == "" {
		return ErrNoEndpoints
	}

	if spec.Start == spec.End {
		return SelfLoopError{ID: spec.ID, Vertex: spec.Start}
	}

	for _, id := range append([]ID{spec.Start, spec.End}, spec.BackedBy...) {
		if err := declaredNamespace(registry, id); err != nil {
			return err
		}
	}

	return nil
}

// Destination is the file the edge goes in.
func (spec EdgeSpec) Destination(registry *Registry, override string) (Destination, error) {
	if err := spec.Check(registry); err != nil {
		return Destination{}, err
	}
	return geometricDestination(registry, spec.ID, override)
}

// form is the edge as it will be written, per specification section 6.3.
func (spec EdgeSpec) form() *Node {
	children := []*Node{symbolNode(string(spec.ID))}

	if spec.Label != "" {
		children = append(children, formNode(labelChild, stringNode(spec.Label)))
	}

	children = append(children,
		formNode(frameChild, symbolNode(string(spec.Frame))),
		formNode(verticesChild, symbolNode(string(spec.Start)), symbolNode(string(spec.End))),
	)

	for _, backing := range spec.BackedBy {
		children = append(children, formNode(backedByChild, symbolNode(string(backing))))
	}

	return formNode(edgeTag, children...)
}

// LoopSpec is a loop which does not exist yet.
type LoopSpec struct {
	// ID is the id it will be written with.
	ID ID

	// Label is its display text, and is empty for a loop with none.
	Label string

	// Frame is the frame it is expressed in.
	Frame ID

	// Edges are the edges it is traversed through, in traversal order. The
	// order is the data and is never sorted.
	Edges []ID
}

// Check reports the first axis of the spec which the registry does not permit.
func (spec LoopSpec) Check(registry *Registry) error {
	if err := checkGeometric(registry, spec.ID, spec.Frame); err != nil {
		return err
	}

	if len(spec.Edges) == 0 {
		return ErrNoEdges
	}

	for _, id := range spec.Edges {
		if err := declaredNamespace(registry, id); err != nil {
			return err
		}
	}

	return nil
}

// Destination is the file the loop goes in.
func (spec LoopSpec) Destination(registry *Registry, override string) (Destination, error) {
	if err := spec.Check(registry); err != nil {
		return Destination{}, err
	}
	return geometricDestination(registry, spec.ID, override)
}

// form is the loop as it will be written, per specification section 6.4.
func (spec LoopSpec) form() *Node {
	children := []*Node{symbolNode(string(spec.ID))}

	if spec.Label != "" {
		children = append(children, formNode(labelChild, stringNode(spec.Label)))
	}

	edges := make([]*Node, 0, len(spec.Edges))
	for _, edge := range spec.Edges {
		edges = append(edges, symbolNode(string(edge)))
	}

	children = append(children,
		formNode(frameChild, symbolNode(string(spec.Frame))),
		formNode(edgesChild, edges...),
	)

	return formNode(loopTag, children...)
}

// checkGeometric reports the first of the two axes every geometric node has
// which the registry does not permit.
func checkGeometric(registry *Registry, id, frame ID) error {
	if id == "" {
		return ErrNoID
	}

	if !registry.Declares(SortNamespace, id.Namespace()) {
		return UnknownAxisError{
			Axis:      string(SortNamespace),
			Value:     id.Namespace(),
			Permitted: registry.Names(SortNamespace),
		}
	}

	if frame == "" {
		return ErrNoFrame
	}

	if !registry.Declares(SortFrame, string(frame)) {
		return UnknownAxisError{Axis: "frame", Value: string(frame), Permitted: registry.Names(SortFrame)}
	}

	return nil
}

// geometricDestination is the file a new geometric node is written to.
//
// The subject carries the id and nothing else, because a geometric node has
// nothing else: it declares no kind and no type, so the one criterion a routing
// rule can match it on is the namespace of its id. A rule written with a kind or
// a type therefore never matches one, which is what keeps the rules placing
// semantic nodes from placing geometry as a side effect.
func geometricDestination(registry *Registry, id ID, override string) (Destination, error) {
	if override != "" {
		return Override(override)
	}
	return registry.Destination(Subject{ID: id})
}

// AddVertex writes a new vertex into the file at path.
//
// The id, its namespace and the frame are checked against the registry before
// anything is written, and so is the position claim where one is given: the
// predicate, the shape of the value, its dimension and its unit are held to
// exactly the rules [Tx.AddClaim] holds a claim to, because it is the same kind
// of claim written in the same place.
//
// Nothing reaches the disk until [Tx.Commit], which interprets the model the
// change would produce and refuses one which would not load.
func (tx *Tx) AddVertex(spec VertexSpec, path string) error {
	if tx.finished {
		return ErrFinished
	}

	if err := spec.Check(tx.graph.Registry()); err != nil {
		return err
	}

	if err := tx.unheld(spec.ID); err != nil {
		return err
	}

	if spec.Position.Predicate != "" {
		if spec.Position.ID != "" {
			if err := tx.unheld(spec.Position.ID); err != nil {
				return err
			}
			if tx.claimWritten(spec.Position.ID) {
				return TakenIDError{ID: spec.Position.ID, What: "a claim this change wrote"}
			}
		}

		if spec.Position.Date.IsZero() {
			spec.Position.Date = time.Now().UTC()
		}
	}

	return tx.Insert(path, spec.form())
}

// AddEdge writes a new edge into the file at path.
//
// Both endpoints are resolved before anything is written, against the model and
// against what this same change has already added: an id naming nothing, and one
// naming a member of the model which is not a vertex, are each a refusal naming
// what was reached. An edge between one vertex and itself is refused as well,
// which is a load error rather than a shape this format can hold.
//
// The shared-edge case is this operation and nothing more. Two regions which
// meet along a wall name one edge, so the second of them is written by naming
// the vertices the first already has — which is why the endpoints are named by
// id rather than by coordinate.
func (tx *Tx) AddEdge(spec EdgeSpec, path string) error {
	if tx.finished {
		return ErrFinished
	}

	if err := spec.Check(tx.graph.Registry()); err != nil {
		return err
	}

	if err := tx.unheld(spec.ID); err != nil {
		return err
	}

	for _, vertex := range []ID{spec.Start, spec.End} {
		if err := tx.references(vertex, vertexTag); err != nil {
			return err
		}
	}

	for _, backing := range spec.BackedBy {
		if err := tx.references(backing, nodeTag); err != nil {
			return err
		}
	}

	return tx.Insert(path, spec.form())
}

// AddLoop writes a new loop into the file at path.
//
// Every edge id is resolved before anything is written, against the model and
// against what this same change has already added. Whether the edges form a
// closed, connected, simple cycle is a question about where their vertices are,
// which [Topology.Assemble] answers against a named tolerance; it is checked
// when the model the change produces is loaded, which is what refuses a change
// which would not load.
func (tx *Tx) AddLoop(spec LoopSpec, path string) error {
	if tx.finished {
		return ErrFinished
	}

	if err := spec.Check(tx.graph.Registry()); err != nil {
		return err
	}

	if err := tx.unheld(spec.ID); err != nil {
		return err
	}

	for _, edge := range spec.Edges {
		if err := tx.references(edge, edgeTag); err != nil {
			return err
		}
	}

	return tx.Insert(path, spec.form())
}

// unheld reports whether anything holds id, counting what this same change has
// already written.
//
// [Tx.free] is asked of the graph, which is the model as the transaction found
// it, so a form an earlier mutation of the same change inserted is not in it.
// Two vertices of one scaffold written under one id is exactly the mistake that
// gap produces.
func (tx *Tx) unheld(id ID) error {
	if err := tx.free(id); err != nil {
		return err
	}

	if form, ok := tx.Form(id); ok {
		tag, _ := formTag(form)
		return TakenIDError{ID: id, What: article(tag) + " " + tag + " this change wrote"}
	}

	return nil
}

// references reports whether id names something of the family a reference to it
// required, counting what this same change has already written.
func (tx *Tx) references(id ID, want string) error {
	if entity, ok := tx.graph.Entity(id); ok {
		if got := familyOf(entity); got != want {
			return NotOfFamilyError{ID: id, Want: want, Got: got}
		}
		return nil
	}

	if form, ok := tx.Form(id); ok {
		got, _ := formTag(form)
		if got != want {
			return NotOfFamilyError{ID: id, Want: want, Got: got}
		}
		return nil
	}

	nearest, _ := tx.graph.Nearest(id)

	return UnknownEntityError{ID: id, Nearest: nearest}
}

// MintID is the id a node this change creates is given.
//
// The format is `<namespace>:<mark>-<n>`: a declared id namespace, the tag of
// the form being written, and the lowest ordinal from one which nothing in the
// model already holds. It is a name and not a schema, and nothing is inferred
// back out of one ([0002](docs/decisions/0002-immutable-id-mutable-label.md)).
//
// The transaction is asked as well as the model, for the reason [Tx.MintClaimID]
// asks it: what this same change has already written is not in the model it
// loaded, and a scaffold minting a hundred vertices would otherwise mint the
// same id a hundred times.
func (tx *Tx) MintID(namespace, mark string) (ID, error) {
	for ordinal := 1; ; ordinal++ {
		minted, err := ParseID(fmt.Sprintf("%s%s%s-%d", namespace, idSeparator, mark, ordinal))
		if err != nil {
			return "", err
		}

		if tx.claimWritten(minted) {
			continue
		}

		if err := tx.unheld(minted); err == nil {
			return minted, nil
		}
	}
}

// Corner is one corner of a loop being scaffolded: where it is, and what the
// vertex written there is called.
type Corner struct {
	// Position is the coordinate, in the unit of the frame. Its shape and its
	// dimension are the ones the position predicate declares, which is what
	// [ParseValue] reads a command line against.
	Position Value

	// Label is the display text of the vertex written there, and is empty for
	// one with none. A corner which reuses an existing vertex does not rename
	// it: a label is what a thing is already called, and the vertex was there
	// first ([0002](docs/decisions/0002-immutable-id-mutable-label.md)).
	Label string
}

// Snap is one corner which landed on a vertex the model already held.
//
// Both outcomes are reported, and that is the point of the type. A reuse which
// happened and a reuse which was declined are the same measurement — this corner
// is this far from that vertex — and an author who is surprised by either wants
// to see the distance rather than infer it from which nodes appeared.
type Snap struct {
	// Corner is the corner's place in the list, counted from one as a person
	// counts a list.
	Corner int

	// Vertex is the vertex it landed on.
	Vertex ID

	// Distance is how far it was from that vertex, in the frame's unit.
	Distance float64

	// Unit is the unit the distance is in, which is the frame's.
	Unit Unit

	// Reused reports whether the vertex was used for this corner rather than a
	// second one being written at the same point. It is false exactly when
	// snapping was switched off, which is the case worth warning about: a
	// duplicate vertex at one point is the sliver the topology model exists to
	// prevent.
	Reused bool
}

// Scaffolding is what scaffolding a loop created.
//
// Everything in it is reported whether or not the change was written, which is
// what makes a dry run worth doing: the ids, the reuses and the tolerance which
// decided them are the whole of what the author is checking before committing to
// them.
type Scaffolding struct {
	// Loop is the id of the loop which was written.
	Loop ID

	// Vertices are the vertex each corner is at, in corner order, with the
	// closing corner left out — it is the first corner written again.
	Vertices []ID

	// Created are the vertices which were minted, in the order they were.
	Created []ID

	// Edges are the loop's edges, in traversal order.
	Edges []ID

	// Reused are the edges of that ring which the model already held, in the
	// order the traversal reaches them. An edge shared by two regions is
	// written once and named by both, which is the whole of what makes a
	// partition one wall rather than two.
	Reused []ID

	// Snaps are the corners which landed on a vertex the model already held, in
	// corner order. Empty rather than nil when none did.
	Snaps []Snap

	// Tolerance is the declaration coincidence and closure were judged against,
	// which travels with the answer because the answer depends on it
	// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
	Tolerance Tolerance

	// Bounds is the node the loop was written on as a boundary, and is empty
	// where the scaffold bound nothing. It is reported because it is the half
	// of the change which is not geometry: an author who asked for a room's
	// outline and for the room to reference it reads back both.
	Bounds ID
}

// ScaffoldSpec is a loop to be created from an ordered list of corners.
//
// It is the one operation in this interface which mints ids, and it mints them
// because the alternative is naming several dozen vertices and edges by hand to
// describe one room. What it does not do is decide anything else: the frame, the
// predicate a position is claimed under, the tolerance two corners are judged
// coincident against and the evidence behind every claim are all supplied, and
// none of them has a default.
type ScaffoldSpec struct {
	// Namespace is the declared id namespace the new nodes are minted in.
	Namespace string

	// Frame is the frame the corners are expressed in.
	Frame ID

	// Corners are the corners of the loop, in order, closed: the last names the
	// first corner again.
	Corners []Corner

	// Label is the loop's display text, and is empty for a loop with none.
	Label string

	// Predicate is the predicate a corner's position is claimed under, which
	// the consuming repository declares rather than the engine knowing
	// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
	Predicate string

	// Provenance is the evidence every position claim is written with: its
	// source, its method, its accuracy and its date. The subject, the predicate
	// and the value of each claim come from the corner it is about.
	Provenance ClaimSpec

	// Tolerance is the name of the declared tolerance two corners are judged
	// coincident against, which is also what says the list closed.
	Tolerance string

	// Snap says to reuse an existing vertex a corner lands on rather than write
	// a second one at the same point.
	Snap bool

	// Bounds is the semantic node the loop bounds, and is empty where the
	// scaffold is to bind the loop to nothing.
	//
	// Naming one writes the `boundary` reference in this same change, which is
	// what makes a room and its outline one operation. A scaffold which could
	// mint the shape and not say what it is the shape of leaves the one fact
	// nothing else in the batch can state to be hand-edited in afterwards.
	Bounds ID

	// VertexMark, EdgeMark and LoopMark are what the minted ids are named
	// after, and are empty for the tag of the form being written — which mints
	// `<namespace>:vertex-1`, `<namespace>:edge-1` and `<namespace>:loop-1`.
	//
	// They are here so that a generated batch lands in the consuming
	// repository's own naming scheme rather than in this engine's, because the
	// alternative is rewriting every id the batch minted by hand, which is the
	// same pass a scaffold exists to avoid. A mark is a name and not a schema:
	// nothing is inferred back out of one
	// ([0002](docs/decisions/0002-immutable-id-mutable-label.md)), and the
	// three are separate rather than one pattern because the three forms are.
	VertexMark string
	EdgeMark   string
	LoopMark   string
}

// marks are the three marks a scaffold mints under, with the tag of each form
// standing in where the spec named none.
func (spec ScaffoldSpec) marks() (vertex, edge, loop string) {
	return either(spec.VertexMark, vertexMark),
		either(spec.EdgeMark, edgeMark),
		either(spec.LoopMark, loopMark)
}

// either is written where it is not empty, and otherwise the default.
func either(written, standing string) string {
	if written == "" {
		return standing
	}
	return written
}

// Scaffold writes the vertices, edges and closed loop an ordered corner list
// describes, in one change.
//
// **The list is authored closed**: its last corner names its first again, within
// the tolerance the spec names, and a list which does not return to where it
// started is refused with the gap and its size. Closing one silently would leave
// a tool unable to tell an outline somebody finished from one they stopped
// typing halfway through, and the wall it invented would appear in no diagnostic
// anywhere.
//
// **A corner within the tolerance of a vertex the model already holds reuses
// that vertex**, and the reuse is reported with the distance it moved. That is
// the whole reason this operation exists: a second room which shares a wall with
// the first shares its two corners and the edge between them, and a duplicate
// vertex a millimetre away is precisely the sliver the topology model is for.
// The edge between two reused corners is reused too, so the partition is one
// node named by both rooms rather than two which can drift apart.
//
// Snapping is switched off with Snap false, which is reported rather than
// silent: every corner which would have been reused comes back as a [Snap] with
// Reused false, so a run which chose to write a duplicate says which vertex it
// duplicated and by how far it missed.
//
// override is a `--file` naming where everything goes, and is empty where the
// routing rules are to decide. Nothing reaches the disk until [Tx.Commit].
func (tx *Tx) Scaffold(spec ScaffoldSpec, override string) (Scaffolding, []Notice, error) {
	if tx.finished {
		return Scaffolding{}, nil, ErrFinished
	}

	builder, err := tx.scaffolder(spec, override)
	if err != nil {
		return Scaffolding{}, nil, err
	}

	if err := builder.corners(); err != nil {
		return Scaffolding{}, nil, err
	}

	if err := builder.write(); err != nil {
		return Scaffolding{}, nil, err
	}

	return builder.built, builder.notices, nil
}

// scaffolder is one scaffolding run, with everything checked which can be
// checked before a corner is read.
type scaffolder struct {
	tx   *Tx
	spec ScaffoldSpec

	// override is the `--file` everything minted goes in, empty where the
	// routing rules are to decide.
	override string

	// unit is the frame's one linear unit, which everything is measured in and
	// nothing is converted to.
	unit Unit

	// tolerance is the declaration coincidence and closure are judged against.
	tolerance Tolerance

	// vertexOf, edgeOf and loopOf are the marks the three families of minted id
	// are named after, which is the spec's where it named them and the tag of
	// the form where it did not.
	vertexOf string
	edgeOf   string
	loopOf   string

	// at is where each candidate vertex is: every vertex of the model written
	// in this frame whose position resolves, plus every vertex this run has
	// minted.
	at map[ID][]float64

	// order is the ids of at, in the order they became candidates, so that two
	// runs over one model pick the same vertex where two are equally near.
	order []ID

	// read is where each corner of the list settled, in corner order.
	//
	// It is the corners rather than the vertices they became, because whether a
	// list visits one of its corners twice is a question about the list: with
	// snapping off, two corners at one point become two vertices, and asking
	// the vertices would answer that a ring which doubles back does not.
	read [][]float64

	built   Scaffolding
	notices []Notice
}

// scaffolder is the run with its registry axes checked and the model's vertices
// read.
func (tx *Tx) scaffolder(spec ScaffoldSpec, override string) (*scaffolder, error) {
	registry := tx.graph.Registry()

	if spec.Namespace == "" {
		return nil, ErrNoNamespace
	}

	if !registry.Declares(SortNamespace, spec.Namespace) {
		return nil, UnknownAxisError{
			Axis:      string(SortNamespace),
			Value:     spec.Namespace,
			Permitted: registry.Names(SortNamespace),
		}
	}

	if spec.Frame == "" {
		return nil, ErrNoFrame
	}

	frame, ok := registry.Frame(spec.Frame)
	if !ok {
		return nil, UnknownAxisError{
			Axis:      "frame",
			Value:     string(spec.Frame),
			Permitted: registry.Names(SortFrame),
		}
	}

	tolerance, ok := registry.Tolerance(spec.Tolerance)
	if !ok {
		return nil, UnknownAxisError{
			Axis:      string(SortTolerance),
			Value:     spec.Tolerance,
			Permitted: registry.Names(SortTolerance),
		}
	}

	if tolerance.Unit != frame.Unit {
		return nil, ToleranceUnitError{Tolerance: tolerance, Frame: spec.Frame, Want: frame.Unit}
	}

	// The predicate is checked before anything is read from a corner, and
	// unconditionally rather than only where there are corners to read. Which
	// shape a position takes is what the declaration says, so a caller which
	// has not been able to read a corner at all — the command line is one —
	// hands the list over unread and is answered here rather than having to
	// spell the same refusal itself.
	if !registry.Declares(SortPredicate, spec.Predicate) {
		return nil, UnknownAxisError{
			Axis:      string(SortPredicate),
			Value:     spec.Predicate,
			Permitted: registry.Names(SortPredicate),
		}
	}

	// The corners are checked after the axes the registry decides, and after the
	// predicate in particular, because a caller which could not read a corner
	// without the declaration hands the list over unread: reporting that as a
	// scaffold of no corners would answer with the consequence of the mistake
	// rather than with the mistake.
	if len(spec.Corners) == 0 {
		return nil, ErrNoCorners
	}

	// The node the loop is to bound is checked before anything is minted, for
	// the reason every other axis is: a scaffold which laid out a room and then
	// found the node it was for names nothing has refused the whole change
	// anyway, and reporting it up front says which axis is wrong rather than
	// which vertex was being written when it was noticed.
	if spec.Bounds != "" {
		if err := tx.references(spec.Bounds, nodeTag); err != nil {
			return nil, err
		}
	}

	vertexOf, edgeOf, loopOf := spec.marks()

	// A mark is a name the caller chose, so a mark which cannot make an id is
	// answered as the malformed id it would have minted rather than being
	// discovered on the first vertex.
	for _, mark := range []string{vertexOf, edgeOf, loopOf} {
		if _, err := ParseID(fmt.Sprintf("%s%s%s-1", spec.Namespace, idSeparator, mark)); err != nil {
			return nil, err
		}
	}

	// The provenance is checked once, against the id the first corner would be
	// minted under, rather than once per corner: every claim this run writes
	// carries the same evidence, so a missing source is a property of the
	// invocation, and reporting it against the fortieth corner says the wrong
	// thing about which one is wrong. The subject is a real id rather than a
	// stand-in because the check is the one every claim is held to, and one
	// held against a subject nobody is writing is a different check.
	first, err := tx.MintID(spec.Namespace, vertexOf)
	if err != nil {
		return nil, err
	}

	probe := spec.Provenance
	probe.Subject, probe.Predicate, probe.Value = first, spec.Predicate, spec.Corners[0].Position
	if err := probe.Check(registry); err != nil {
		return nil, err
	}

	builder := &scaffolder{
		tx:        tx,
		spec:      spec,
		override:  override,
		unit:      frame.Unit,
		tolerance: tolerance,
		vertexOf:  vertexOf,
		edgeOf:    edgeOf,
		loopOf:    loopOf,
		at:        map[ID][]float64{},
		built:     Scaffolding{Snaps: []Snap{}, Tolerance: tolerance, Bounds: spec.Bounds},
	}

	builder.candidates()

	return builder, nil
}

// candidates reads where every vertex of the model written in this frame is.
//
// A vertex whose position does not resolve is not a candidate. That is a state
// and not a failure — nothing was claimed about it, or the claims tie and the tie
// is unbroken — and a corner cannot be said to land on a vertex nobody can say
// the whereabouts of.
func (s *scaffolder) candidates() {
	registry := s.tx.graph.Registry()

	for vertex := range s.tx.graph.Topology().Vertices() {
		if vertex.Frame() != s.spec.Frame {
			continue
		}

		resolution, err := s.tx.graph.Claims().Resolve(vertex.ID(), s.spec.Predicate, registry)
		if err != nil {
			continue
		}

		value, ok := resolution.Value()
		if !ok || value.Unit() != s.unit {
			continue
		}

		components, ok := value.Coordinate()
		if !ok {
			continue
		}

		s.record(vertex.ID(), components)
	}
}

// record adds a vertex to the candidates a corner may land on.
func (s *scaffolder) record(id ID, components []float64) {
	if _, held := s.at[id]; !held {
		s.order = append(s.order, id)
	}
	s.at[id] = components
}

// corners reads the corner list into the ring the loop will traverse.
func (s *scaffolder) corners() error {
	if len(s.spec.Corners) < 4 {
		return TooFewCornersError{Corners: len(s.spec.Corners)}
	}

	written := s.spec.Corners
	last := len(written) - 1

	// The list closes or it is not a list of a loop's corners. It is judged
	// before anything is minted, so a refusal leaves the transaction holding
	// nothing it would have to unpick.
	gap, ok := separation(written[last].Position, written[0].Position, s.unit)
	switch {
	case !ok:
		return UnclosedLoopError{Last: last + 1, Gap: -1, Unit: s.unit, Tolerance: s.tolerance}
	case gap > s.tolerance.Value:
		return UnclosedLoopError{Last: last + 1, Gap: gap, Unit: s.unit, Tolerance: s.tolerance}
	}

	for index, corner := range written[:last] {
		if err := s.corner(index, corner); err != nil {
			return err
		}
	}

	return nil
}

// corner settles which vertex one corner is at, minting one where it is
// somewhere nothing already is.
func (s *scaffolder) corner(index int, corner Corner) error {
	components, ok := corner.Position.Coordinate()
	if !ok || corner.Position.Unit() != s.unit {
		return UnitError{Predicate: s.spec.Predicate, Want: s.unit, Got: corner.Position.Unit()}
	}

	// Whether the list visits one of its own corners twice is settled before
	// anything else, and settled the same way whether or not snapping is on. It
	// is a mistake in the corner list — a coordinate typed twice, or an outline
	// which doubles back — and switching snapping off says to write a vertex
	// where one already is, not that a ring may visit a corner twice.
	if err := s.revisited(index, components); err != nil {
		return err
	}

	found, distance, ok := s.nearest(components)

	switch {
	case ok && s.spec.Snap:
		// Two corners far enough apart to be corners, both near enough to one
		// vertex the model already holds to snap to it, collapse the ring just
		// as a repeated coordinate does.
		if at := slices.Index(s.built.Vertices, found); at >= 0 {
			return CollapsedRingError{
				First:     at + 1,
				Second:    index + 1,
				Vertex:    found,
				Tolerance: s.tolerance,
			}
		}

		s.built.Snaps = append(s.built.Snaps, Snap{
			Corner:   index + 1,
			Vertex:   found,
			Distance: distance,
			Unit:     s.unit,
			Reused:   true,
		})
		s.built.Vertices = append(s.built.Vertices, found)
		s.read = append(s.read, components)

		return nil

	case ok:
		// Snapping is off, so a second vertex is written where one already is.
		// It is reported rather than silent: this is the sliver the topology
		// model exists to prevent, and an author who asked for it is entitled to
		// have it and entitled to be told.
		s.built.Snaps = append(s.built.Snaps, Snap{
			Corner:   index + 1,
			Vertex:   found,
			Distance: distance,
			Unit:     s.unit,
			Reused:   false,
		})
	}

	return s.mint(corner, components)
}

// mint writes a new vertex at a corner nothing already stands on.
//
// The vertex is written as it is minted rather than after the whole ring is
// settled, because an id is free until something holds it: minting four of them
// before writing any would ask the same question four times and get the same
// answer, and the room would be four vertices under one name.
func (s *scaffolder) mint(corner Corner, components []float64) error {
	minted, err := s.tx.MintID(s.spec.Namespace, s.vertexOf)
	if err != nil {
		return err
	}

	spec := VertexSpec{
		ID:       minted,
		Label:    corner.Label,
		Frame:    s.spec.Frame,
		Position: s.position(minted, corner.Position),
	}

	if err := s.insert(minted, spec.form()); err != nil {
		return err
	}

	s.record(minted, components)
	s.built.Vertices = append(s.built.Vertices, minted)
	s.built.Created = append(s.built.Created, minted)
	s.read = append(s.read, components)

	if len(s.spec.Provenance.Accuracy) == 0 {
		s.notices = append(s.notices, Notice{
			Kind:      NoticeUnrankable,
			Subject:   minted,
			Predicate: s.spec.Predicate,
		})
	}

	return nil
}

// revisited reports a corner which is at a point the list has already been to.
//
// It is refused rather than quietly folded away, and it is refused whether or
// not snapping is on. Two corners at one point are either a coordinate typed
// twice or an outline which doubles back, and a scaffold which dropped one of
// them would write a room with a wall missing and report having written the room
// that was asked for. Switching snapping off says to write a vertex where one
// already is; it does not say a ring may visit a corner twice.
func (s *scaffolder) revisited(index int, components []float64) error {
	for earlier, at := range s.read {
		gap, ok := distanceBetween(at, components)
		if !ok || gap > s.tolerance.Value {
			continue
		}

		return CollapsedRingError{
			First:     earlier + 1,
			Second:    index + 1,
			Vertex:    s.built.Vertices[earlier],
			Tolerance: s.tolerance,
		}
	}

	return nil
}

// nearest is the candidate vertex a corner lands on: the nearest one within the
// tolerance, and whether there is one at all.
//
// Ties are broken by the order the candidates were read, which is the order the
// model's walk reached them followed by the order this run minted them. It is an
// order rather than an arbitrary choice so that two runs over one model reuse the
// same vertex.
func (s *scaffolder) nearest(components []float64) (ID, float64, bool) {
	var (
		found   ID
		nearest float64
	)

	for _, id := range s.order {
		gap, ok := distanceBetween(s.at[id], components)
		if !ok || gap > s.tolerance.Value {
			continue
		}

		if found == "" || gap < nearest {
			found, nearest = id, gap
		}
	}

	return found, nearest, found != ""
}

// write settles the ring's edges, writes the loop over them, and says what the
// loop is the shape of where the spec named anything.
func (s *scaffolder) write() error {
	if err := s.ring(); err != nil {
		return err
	}

	loop, err := s.tx.MintID(s.spec.Namespace, s.loopOf)
	if err != nil {
		return err
	}

	s.built.Loop = loop

	if err := s.insert(loop, LoopSpec{
		ID:    loop,
		Label: s.spec.Label,
		Frame: s.spec.Frame,
		Edges: s.built.Edges,
	}.form()); err != nil {
		return err
	}

	// The reference goes on last, because it names the loop and the loop's id
	// is not settled until it is written. It is the same `boundary` child
	// [Tx.Relate] writes, so a room outlined by a scaffold and a room outlined
	// by a loop somebody wrote by hand say the same thing in the same words.
	if s.spec.Bounds == "" {
		return nil
	}

	return s.tx.Relate(s.spec.Bounds, RelationSpec{Boundary: []ID{loop}})
}

// ring settles the edge between each pair of neighbouring corners, reusing the
// one the model already holds where there is one.
func (s *scaffolder) ring() error {
	corners := s.built.Vertices

	for index, start := range corners {
		end := corners[(index+1)%len(corners)]

		if held, ok := s.tx.between(start, end); ok {
			s.built.Edges = append(s.built.Edges, held)
			s.built.Reused = append(s.built.Reused, held)
			continue
		}

		minted, err := s.tx.MintID(s.spec.Namespace, s.edgeOf)
		if err != nil {
			return err
		}

		if err := s.insert(minted, EdgeSpec{
			ID:    minted,
			Frame: s.spec.Frame,
			Start: start,
			End:   end,
		}.form()); err != nil {
			return err
		}

		s.built.Edges = append(s.built.Edges, minted)
	}

	return nil
}

// position is the claim saying where a minted vertex is.
func (s *scaffolder) position(id ID, at Value) ClaimSpec {
	spec := s.spec.Provenance
	spec.ID = ""
	spec.Subject = id
	spec.Predicate = s.spec.Predicate
	spec.Value = at

	if spec.Date.IsZero() {
		spec.Date = time.Now().UTC()
	}

	return spec
}

// insert routes one minted form and adds it.
func (s *scaffolder) insert(id ID, form *Node) error {
	destination, err := geometricDestination(s.tx.graph.Registry(), id, s.override)
	if err != nil {
		return err
	}

	return s.tx.Insert(destination.Path, form)
}

// between is the edge the model holds between two vertices, whichever way round
// it was written, counting the edges this same change has added.
//
// The direction is not part of the question. An edge is one node named by both
// regions it separates, and the second of them traverses it the other way — so
// an edge written start to end is the edge a ring reaching it end to start is
// looking for ([0001](docs/decisions/0001-two-node-families.md)).
func (tx *Tx) between(first, second ID) (ID, bool) {
	for edge := range tx.graph.Topology().Edges() {
		start, end := edge.Vertices()
		if (start == first && end == second) || (start == second && end == first) {
			return edge.ID(), true
		}
	}

	for _, key := range tx.order {
		for _, node := range tx.files[key].file.Nodes {
			if tag, ok := formTag(node); !ok || tag != edgeTag {
				continue
			}

			written, ok := childForm(node, verticesChild)
			if !ok {
				continue
			}

			start, end := argumentID(written, 0), argumentID(written, 1)
			if (start == first && end == second) || (start == second && end == first) {
				return subjectID(node), true
			}
		}
	}

	return "", false
}

// argumentID is the id written as a form's nth argument, empty where what was
// written there was not one.
func argumentID(form *Node, index int) ID {
	written, ok := argument(form, index)
	if !ok {
		return ""
	}

	symbol, ok := written.Datum.(sexpr.Symbol)
	if !ok {
		return ""
	}

	id, err := ParseID(symbol.Value)
	if err != nil {
		return ""
	}

	return id
}

// separation is how far apart two coordinates are, in the unit given, and
// whether that could be measured at all.
func separation(from, to Value, unit Unit) (float64, bool) {
	if from.Unit() != unit || to.Unit() != unit {
		return 0, false
	}

	first, ok := from.Coordinate()
	if !ok {
		return 0, false
	}

	second, ok := to.Coordinate()
	if !ok {
		return 0, false
	}

	return distanceBetween(first, second)
}

// distanceBetween is the euclidean distance between two coordinates of the same
// dimension.
//
// Nothing is padded and nothing is converted: a two-component position compared
// against a three-component one is a question with no answer rather than one
// with a plausible wrong one
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
func distanceBetween(from, to []float64) (float64, bool) {
	if len(from) == 0 || len(from) != len(to) {
		return 0, false
	}

	var squares float64
	for i := range from {
		delta := from[i] - to[i]
		squares += delta * delta
	}

	gap := math.Sqrt(squares)
	if math.IsNaN(gap) || math.IsInf(gap, 0) {
		return 0, false
	}

	return gap, true
}
