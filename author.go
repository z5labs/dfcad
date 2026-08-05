// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"fmt"
	"slices"
	"time"

	sexpr "github.com/z5labs/sexpr-go"
)

// ErrNoID reports something authored without an id.
//
// Every entity is written with one, and an id is never derived from anything
// else about the thing: a node is what its id says it is, for as long as the
// model holds it ([0002](docs/decisions/0002-immutable-id-mutable-label.md)).
var ErrNoID = errors.New("a node is written with an id")

// UnknownAxisError reports a value on one axis of an authored node which names
// nothing the model has.
//
// It carries the permitted set rather than only the name, because a refusal
// which says a value is wrong without saying what would have been right is one
// the author answers by guessing. Which set that is depends on the axis: the
// kinds and the geometry forms are closed sets compiled into the engine, and
// the namespaces, the types and the frames are registry data the consuming
// repository writes ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
type UnknownAxisError struct {
	// Axis is what was being named: `namespace`, `kind`, `type`, `geometry` or
	// `frame`.
	Axis string

	// Value is what was asked for.
	Value string

	// Permitted is every value the axis accepts, in the order the set is
	// spelled: specification order for a closed set, name order for registry
	// data.
	Permitted []string
}

// Error implements the [error] interface.
func (e UnknownAxisError) Error() string {
	if len(e.Permitted) == 0 {
		return fmt.Sprintf("unknown %s %s: this model has none at all", e.Axis, e.Value)
	}
	return fmt.Sprintf("unknown %s %s: want %s", e.Axis, e.Value, join(e.Permitted, "or"))
}

// NotPermittedError reports an axis of an authored node which the node's type
// does not permit.
//
// It is a different refusal from [UnknownAxisError] and reads as one: the value
// is a real member of its set, and the type this node declares is not one which
// takes it. The fix is a different type or a different value, and which of the
// two it is depends on what the author meant — so the message says what the
// type permits rather than choosing for them.
type NotPermittedError struct {
	// Type is the type the node declares.
	Type string

	// Axis is which axis was refused: `kind` or `geometry`.
	Axis string

	// Value is what was written there, and is empty where the axis was omitted
	// and the type requires it.
	Value string

	// Permitted is what the type permits on that axis.
	Permitted []string
}

// Error implements the [error] interface.
func (e NotPermittedError) Error() string {
	if e.Value == "" {
		return fmt.Sprintf("%s requires a %s: %s permits %s", e.Type, e.Axis, e.Type, join(e.Permitted, "and"))
	}
	return fmt.Sprintf("%s does not permit the %s %s: it permits %s", e.Type, e.Axis, e.Value, join(e.Permitted, "and"))
}

// TakenIDError reports an id which already names something in the model.
//
// An id is unique across the whole model and is never issued again to a
// different thing, which is what makes a reference written years ago mean today
// what it meant then ([0002](docs/decisions/0002-immutable-id-mutable-label.md)).
// A retired id is taken as firmly as a live one is: retiring says the thing
// stopped existing, not that its name came free.
type TakenIDError struct {
	// ID is the id which was asked for.
	ID ID

	// What names the sort of thing which already holds it, so that the refusal
	// says what was hit rather than only that something was.
	What string

	// At is where that thing is defined.
	At Span

	// Retired reports whether the thing which holds it was retired, which is
	// the case an author is most likely to have thought was free.
	Retired bool
}

// Error implements the [error] interface.
func (e TakenIDError) Error() string {
	if e.Retired {
		return fmt.Sprintf("%s already names %s, retired at %s: an id is never issued again", e.ID, e.What, e.At.Start)
	}
	return fmt.Sprintf("%s already names %s, defined at %s", e.ID, e.What, e.At.Start)
}

// UnknownEntityError reports an id nothing in the model answers to.
type UnknownEntityError struct {
	// ID is the id which was asked for.
	ID ID

	// Nearest is the id closest to it which the model does hold, and is empty
	// where nothing is close enough to be worth suggesting.
	Nearest ID
}

// Error implements the [error] interface.
func (e UnknownEntityError) Error() string {
	if e.Nearest != "" {
		return fmt.Sprintf("nothing answers to %s: did you mean %s?", e.ID, e.Nearest)
	}
	return fmt.Sprintf("nothing answers to %s", e.ID)
}

// NotANodeError reports an id which names something other than a semantic node
// where one was required.
//
// Retiring is a thing the semantic family does. A vertex, an edge and a loop
// are the shape other things are written in terms of, and a shape which stopped
// existing is one nothing references any more — which is a change to whatever
// referenced it rather than a state written on the shape
// ([0001](docs/decisions/0001-two-node-families.md)).
type NotANodeError struct {
	// ID is the id which was asked for.
	ID ID

	// Family is the tag the model wrote it as: `vertex`, `edge` or `loop`.
	Family string
}

// Error implements the [error] interface.
func (e NotANodeError) Error() string {
	return fmt.Sprintf("%s names a %s, which is not a node", e.ID, e.Family)
}

// MissingReasonError reports a retirement which said nothing about why.
//
// The reason is required because a retirement with no reason is a deletion
// wearing a hat: the record of what was there survives, and the record of why
// it stopped being there is the half which explains it.
type MissingReasonError struct {
	// ID is the node which was being retired.
	ID ID
}

// Error implements the [error] interface.
func (e MissingReasonError) Error() string {
	return fmt.Sprintf("no reason: retiring %s says why it stopped existing", e.ID)
}

// AlreadyRetiredError reports a node which was retired before this change.
type AlreadyRetiredError struct {
	// ID is the node.
	ID ID

	// Date is when it was retired.
	Date time.Time

	// Reason is why it was.
	Reason string
}

// Error implements the [error] interface.
func (e AlreadyRetiredError) Error() string {
	return fmt.Sprintf("%s was retired on %s: %s", e.ID, e.Date.Format(dateLayout), e.Reason)
}

// SelfReplacementError reports a node retired in favour of itself.
type SelfReplacementError struct {
	// ID is the node.
	ID ID
}

// Error implements the [error] interface.
func (e SelfReplacementError) Error() string {
	return fmt.Sprintf("%s cannot replace itself: a thing which stands in its own place did not stop existing", e.ID)
}

// ReferencedError reports a node which other things still point at.
//
// Retiring one would leave every reference below pointing at something which
// says it stopped existing, which is a model whose answers depend on which of
// the two ends of each reference the reader believes. Either those references
// are redirected to whatever replaced it — which is what a replacement is for —
// or they are changed first and the retirement reissued.
type ReferencedError struct {
	// ID is the node which was being retired.
	ID ID

	// By is every reference to it, in the order the walk reached the entities
	// which made them.
	By []Reference
}

// Error implements the [error] interface.
//
// Every referrer is named rather than counted: "still referenced" without
// saying by what leaves the author reading every file in the model to find out.
func (e ReferencedError) Error() string {
	referrers := make([]string, 0, len(e.By))
	for _, reference := range e.By {
		referrers = append(referrers, fmt.Sprintf("%s (%s)", reference.From, reference.Relation))
	}

	return fmt.Sprintf("%s is still referenced by %s", e.ID, join(referrers, "and"))
}

// NodeSpec is a semantic node which does not exist yet: the axes it will be
// written with.
//
// It is the axes rather than a [SemanticNode] because the node being authored
// has not been read from anywhere, so there is nothing to carry a span, a
// resolved reference or a claim. The three optional axes are optional as
// emptiness: a node with no geometry, no frame and no label is ordinary, and
// the empty string is not a member of either closed set, so nothing is lost by
// spelling absence that way.
//
// The references a node makes — what contains it, which zones it belongs to,
// which loops bound it — are not here. A new node is added and then related, so
// that the refusal to place it and the refusal to relate it are two answers
// rather than one compound one.
type NodeSpec struct {
	// ID is the id it will be written with. Its namespace must be one the
	// registry declares.
	ID ID

	// Kind is the kind it will declare, which must be one of [Kinds] and must
	// be one the type permits.
	Kind Kind

	// Type is the type it will declare, which the registry must declare.
	Type string

	// Geometry is the geometry form it will declare, and is empty for a node
	// with no geometry — which the type has to permit.
	Geometry Geometry

	// Frame is the coordinate frame it is expressed in, and is empty for a node
	// which declares none.
	Frame ID

	// Label is its display text, and is empty for a node with none. Nothing
	// resolves through a label, so it is the one axis here which changes nothing
	// but what a person reads.
	Label string
}

// Check reports the first axis of the spec which the registry does not permit.
//
// The checks are ordered rather than independent because they build on each
// other, for the reason the load pass orders the same three: a type nothing
// declares permits nothing, and reporting a kind as one an undeclared type does
// not permit says nothing anybody can act on.
//
// [Tx.AddNode] calls it, so a caller which only adds nodes never has to. It is
// exported for the caller which has something else to do with the axes first —
// deciding which file the node goes in, which is a question about the same three
// — because a misspelled kind answered as a routing rule which matched nothing
// is an answer about the wrong mistake.
func (spec NodeSpec) Check(registry *Registry) error {
	if spec.ID == "" {
		return ErrNoID
	}

	if !registry.Declares(SortNamespace, spec.ID.Namespace()) {
		return UnknownAxisError{
			Axis:      string(SortNamespace),
			Value:     spec.ID.Namespace(),
			Permitted: registry.Names(SortNamespace),
		}
	}

	if !slices.Contains(kinds, spec.Kind) {
		return UnknownAxisError{Axis: "kind", Value: string(spec.Kind), Permitted: spellings(kinds)}
	}

	if spec.Geometry != "" && !slices.Contains(geometries, spec.Geometry) {
		return UnknownAxisError{Axis: "geometry", Value: string(spec.Geometry), Permitted: spellings(geometries)}
	}

	if spec.Frame != "" && !registry.Declares(SortFrame, string(spec.Frame)) {
		return UnknownAxisError{Axis: "frame", Value: string(spec.Frame), Permitted: registry.Names(SortFrame)}
	}

	declared, ok := registry.Type(spec.Type)
	if !ok {
		return UnknownAxisError{Axis: string(SortType), Value: spec.Type, Permitted: registry.Names(SortType)}
	}

	if !declared.PermitsKind(spec.Kind) {
		return NotPermittedError{
			Type:      declared.Name,
			Axis:      "kind",
			Value:     string(spec.Kind),
			Permitted: spellings(declared.Kinds),
		}
	}

	switch {
	case spec.Geometry != "" && !declared.PermitsGeometry(spec.Geometry):
		return NotPermittedError{
			Type:      declared.Name,
			Axis:      "geometry",
			Value:     string(spec.Geometry),
			Permitted: spellings(declared.Geometries),
		}

	case spec.Geometry == "" && !declared.Absent:
		return NotPermittedError{
			Type:      declared.Name,
			Axis:      "geometry",
			Permitted: spellings(declared.Geometries),
		}
	}

	return nil
}

// Destination is the file the node goes in: the one the registry's routing
// rules choose for it, or the one an override names outright.
//
// override is a path relative to the model root, and is empty where the rules
// are to decide. It is not checked against them, which is the whole of what an
// override is for: the rules describe where things ordinarily go, and the one
// command which needs somewhere else says so rather than having a rule written
// for it.
//
// The axes are checked before the rules are consulted, and that order is here
// rather than in the caller because it is the difference between two answers to
// one mistake. Three of the axes are what a rule matches on, so a misspelled
// kind reported as a node no rule places sends the author to the registry to
// write a rule for a kind which does not exist.
func (spec NodeSpec) Destination(registry *Registry, override string) (Destination, error) {
	if err := spec.Check(registry); err != nil {
		return Destination{}, err
	}

	if override != "" {
		return Override(override)
	}

	return registry.Destination(Subject{ID: spec.ID, Kind: spec.Kind, Type: spec.Type})
}

// form is the node as it will be written.
//
// The children are written in the order specification section 6.1 tables them,
// which decides nothing: canonical form sorts the children of every form, so a
// node built here and a node somebody typed print the same way.
func (spec NodeSpec) form() *Node {
	children := []*Node{symbolNode(string(spec.ID))}

	if spec.Label != "" {
		children = append(children, formNode(labelChild, stringNode(spec.Label)))
	}

	children = append(children,
		formNode("kind", symbolNode(string(spec.Kind))),
		formNode("type", symbolNode(spec.Type)),
	)

	if spec.Geometry != "" {
		children = append(children, formNode("geometry", symbolNode(string(spec.Geometry))))
	}

	if spec.Frame != "" {
		children = append(children, formNode("frame", symbolNode(string(spec.Frame))))
	}

	return formNode(nodeTag, children...)
}

// AddNode writes a new semantic node into the file at path.
//
// Every axis is checked against the registry before anything is written, and
// the first one which is not permitted comes back as an error carrying the set
// which would have been: an unregistered type, a kind or a geometry form the
// type does not permit, an undeclared frame and an id namespace nobody declared
// are each a refusal the author can act on without reading the registry.
//
// An id something already holds is refused, naming where that thing is defined,
// and an id this same change has already written is refused with it. A retired
// id is refused the same way, because retiring says the thing stopped existing
// rather than that its name came free: an id is never issued twice
// ([0002](docs/decisions/0002-immutable-id-mutable-label.md)).
//
// path is where the node goes. [Registry.Destination] is what decides it from
// the registry's routing rules, and [Override] is the one way a command names
// somewhere else; neither decision is taken here, so that `dfcad route` and
// every command which writes answer the question the same way.
//
// Nothing reaches the disk until [Tx.Commit], which interprets the model the
// change would produce and refuses one which would not load.
func (tx *Tx) AddNode(spec NodeSpec, path string) error {
	if tx.finished {
		return ErrFinished
	}

	if err := spec.Check(tx.graph.Registry()); err != nil {
		return err
	}

	if err := tx.unheld(spec.ID); err != nil {
		return err
	}

	return tx.Insert(path, spec.form())
}

// free reports whether anything in the model already holds id.
//
// Every space an id can be taken in is one space: entities, the frames the
// registry declares and the claims which wrote an id of their own all draw from
// it, so that a reference resolves to one thing whatever family holds it.
func (tx *Tx) free(id ID) error {
	if frame, ok := tx.graph.Registry().Frame(id); ok {
		return TakenIDError{ID: id, What: "a frame", At: frame.Span}
	}

	if claim, ok := tx.graph.Claims().Claim(id); ok {
		return TakenIDError{ID: id, What: "a claim", At: claim.Span()}
	}

	entity, ok := tx.graph.Entity(id)
	if !ok {
		return nil
	}

	taken := TakenIDError{ID: id, What: article(familyOf(entity)) + " " + familyOf(entity), At: entity.Span()}

	// A retired node is named by the retirement rather than by the form, because
	// the retirement is the answer: an author reaching for an id which was used
	// before is sent to the sentence saying what happened to the thing which had
	// it.
	if node, ok := entity.(*SemanticNode); ok {
		if retirement, ok := node.Retirement(); ok {
			taken.Retired, taken.At = true, retirement.Span()
		}
	}

	return taken
}

// entityTags are the forms which hold an entity, which is what a reference
// resolves to and what a claim is written on.
var entityTags = []string{nodeTag, vertexTag, edgeTag, loopTag}

// familyOf is the tag the model wrote an entity as.
func familyOf(entity Entity) string {
	switch entity.(type) {
	case *Vertex:
		return vertexTag
	case *Edge:
		return edgeTag
	case *Loop:
		return loopTag
	default:
		return nodeTag
	}
}

// SetLabel changes the display text of the thing id names, and nothing else.
//
// A label carries no meaning to anything in the engine: nothing resolves
// through it, nothing derives from it, and no two things are required to have
// different ones. Renaming is therefore a one-line diff rather than a
// re-identification — the id, the global id derived from it and every reference
// written to it are what they were
// ([0002](docs/decisions/0002-immutable-id-mutable-label.md)).
//
// An empty label removes the child, which is how a thing goes back to having
// none. A label nobody wrote and an empty one are the same state to everything
// but a person reading it, and the format spells that state as the child being
// absent.
func (tx *Tx) SetLabel(id ID, label string) error {
	if tx.finished {
		return ErrFinished
	}

	if err := tx.entity(id); err != nil {
		return err
	}

	// The entity is there and the transaction holds no form under its id, which
	// is what a form removed by an earlier mutation of this transaction leaves
	// behind. It is a different answer from an id nothing answers to, which the
	// check above has already given.
	form, ok := tx.Form(id)
	if !ok {
		return UnknownFormError{}
	}

	return tx.Replace(form, labelled(form, label))
}

// entity reports whether the model holds an entity under id, counting what this
// same change has already written.
//
// The forms this change wrote answer for the reason [Tx.references] counts them:
// the graph is the model as the transaction found it, and a batch which writes a
// node and then names it is one statement rather than two changes which have to
// be committed in turn.
func (tx *Tx) entity(id ID) error {
	if _, ok := tx.graph.Entity(id); ok {
		return nil
	}

	if tx.wrote(id) {
		return nil
	}

	nearest, _ := tx.graph.Nearest(id)
	return UnknownEntityError{ID: id, Nearest: nearest}
}

// wrote reports whether this change has already written an entity under id.
//
// Only the entity forms answer. A registry form written under a name rather than
// an id is not something a reference reaches or a claim is written on, and
// reading one as an entity would make a batch which names a type resolve to it.
func (tx *Tx) wrote(id ID) bool {
	form, ok := tx.Form(id)
	if !ok {
		return false
	}

	tag, _ := formTag(form)

	return slices.Contains(entityTags, tag)
}

// RetirementSpec is how a node is being retired: when, why, and what replaces
// it where anything does.
//
// It is the written form of a [Retirement] rather than the read one, which is
// why the fields are exported and carry no spans: nothing here has been written
// anywhere yet.
type RetirementSpec struct {
	// Date is when the thing ceased to exist. The zero time is written as the
	// day the change is made, because a retirement is dated by when it happened
	// and the common case is that it is happening now.
	Date time.Time

	// Reason is why, in the author's words. It is required.
	Reason string

	// SupersededBy is the node which replaced it, and is empty where nothing
	// did. Supplying one is also what lets the references to the retired node be
	// redirected rather than refused.
	SupersededBy ID
}

// form is the `retired` child as it will be written.
func (spec RetirementSpec) form() *Node {
	children := []*Node{
		formNode("date", stringNode(spec.Date.Format(dateLayout))),
		formNode("reason", stringNode(spec.Reason)),
	}

	if spec.SupersededBy != "" {
		children = append(children, formNode(supersededByChild, symbolNode(string(spec.SupersededBy))))
	}

	return formNode(retiredChild, children...)
}

// Retire marks the node id names as having stopped existing.
//
// Retiring is not deleting. The form stays in the file, the id stays in the
// graph, and every claim ever written on the node is still there to be read: a
// reference written years ago resolves either to the thing it always named or
// to a retired node which says what happened to it
// ([0002](docs/decisions/0002-immutable-id-mutable-label.md)). That is also why
// the id is never issued again — [Tx.AddNode] refuses a retired one.
//
// A reason is required. A retirement which does not say why is a deletion
// wearing a hat: what the record loses is not the node, which is still there,
// but the one sentence explaining why it stopped being true.
//
// A node other things still reference is refused, naming every referrer, unless
// a replacement is supplied. Where one is, every reference to the retired node
// is redirected to it in the same change — which is the whole of what a
// replacement is for, and is why redirecting them is not left as a second
// command somebody may not run.
//
// The change is still interpreted at [Tx.Commit] as though it had been written,
// so a redirection which produces a model that does not load — a containment
// the hierarchy does not permit, a `member-of` naming something which is not a
// Zone — is refused there with the diagnostics a load would have raised.
func (tx *Tx) Retire(id ID, spec RetirementSpec) error {
	if tx.finished {
		return ErrFinished
	}

	if spec.Reason == "" {
		return MissingReasonError{ID: id}
	}

	node, err := tx.node(id)
	if err != nil {
		return err
	}

	if retirement, ok := node.Retirement(); ok {
		return AlreadyRetiredError{ID: id, Date: retirement.Date(), Reason: retirement.Reason()}
	}

	if err := tx.replacement(id, spec.SupersededBy); err != nil {
		return err
	}

	form, ok := tx.Form(id)
	if !ok {
		return UnknownFormError{Span: node.Span()}
	}

	if spec.Date.IsZero() {
		spec.Date = time.Now().UTC()
	}

	if err := tx.redirect(id, spec.SupersededBy); err != nil {
		return err
	}

	return tx.Replace(form, retiredAs(form, spec))
}

// node is the semantic node id names.
func (tx *Tx) node(id ID) (*SemanticNode, error) {
	if node, ok := tx.graph.Node(id); ok {
		return node, nil
	}

	entity, ok := tx.graph.Entity(id)
	if !ok {
		nearest, _ := tx.graph.Nearest(id)
		return nil, UnknownEntityError{ID: id, Nearest: nearest}
	}

	return nil, NotANodeError{ID: id, Family: familyOf(entity)}
}

// replacement checks the node a retirement names as standing in its place.
//
// A replacement which is itself retired is refused. Redirecting the references
// to it would move every one of them onto something else which says it stopped
// existing, which is the same problem one reference further along.
func (tx *Tx) replacement(id, replacement ID) error {
	if replacement == "" {
		return nil
	}

	if replacement == id {
		return SelfReplacementError{ID: id}
	}

	node, err := tx.node(replacement)
	if err != nil {
		return err
	}

	if retirement, ok := node.Retirement(); ok {
		return AlreadyRetiredError{ID: replacement, Date: retirement.Date(), Reason: retirement.Reason()}
	}

	return nil
}

// redirect points everything which references id at replacement, or refuses the
// retirement where there is nothing to point them at.
//
// Each referring form is rewritten once, whatever number of references it
// makes, because rewriting is by form rather than by reference: an edge backed
// by the retired node twice is one form with two children to change, and the
// second reference to it finds the form already rewritten and nothing left to
// do.
//
// The references are read once. Walking them is a scan of the whole model, and
// the refusal below reports the same set the rewrite would have redirected.
func (tx *Tx) redirect(id, replacement ID) error {
	references := slices.Collect(tx.graph.References(id))
	if len(references) == 0 {
		return nil
	}

	if replacement == "" {
		return ReferencedError{ID: id, By: references}
	}

	for _, reference := range references {
		form, ok := tx.Form(reference.From)
		if !ok {
			return UnknownFormError{Span: reference.Span}
		}

		rewritten, changed := redirected(form, id, replacement)
		if !changed {
			continue
		}

		if err := tx.Replace(form, rewritten); err != nil {
			return err
		}
	}

	return nil
}

// referring are the children which name another entity by id, which are the
// ones a redirection rewrites.
//
// A claim's `superseded-by` names a claim rather than a node and is in the set
// all the same, which changes nothing: ids are unique across the whole model, so
// a child naming the node being retired is never a child naming a claim.
var referring = []string{
	withinChild,
	memberOfChild,
	boundaryChild,
	supersededByChild,
	verticesChild,
	edgesChild,
	backedByChild,
}

// redirected is form with every reference to `from` written as `to`, and
// whether anything changed.
//
// The rewrite is by child tag rather than by matching the id anywhere in the
// form, which is what keeps it from rewriting the thing's own id: the id a form
// is defined under is the positional argument of the form itself, and the form's
// own tag is not one a reference is ever written with.
//
// A subtree nothing changed is the subtree which was there, pointer and all, so
// a redirection rebuilds the path to each change and copies nothing else. What
// that preserves is everything a rebuild would drop — the spans a diagnostic
// points at, and the comments somebody wrote.
func redirected(form *Node, from, to ID) (*Node, bool) {
	datum, ok := form.Datum.(sexpr.List)
	if !ok || datum.Tail != nil {
		return form, false
	}

	if tag, ok := formTag(form); ok && slices.Contains(referring, tag) {
		if rewritten, changed := repointed(form, from, to); changed {
			return rewritten, true
		}
	}

	var children []*Node

	for i, child := range form.Children {
		rewritten, rewrote := redirected(child, from, to)
		if !rewrote {
			continue
		}

		if children == nil {
			children = slices.Clone(form.Children)
		}
		children[i] = rewritten
	}

	if children == nil {
		return form, false
	}

	return relisted(form, children), true
}

// repointed is one reference child with every argument naming `from` written as
// `to`.
func repointed(child *Node, from, to ID) (*Node, bool) {
	arguments, _ := split(elements(child))

	var children []*Node

	for i, argument := range arguments {
		symbol, ok := argument.Datum.(sexpr.Symbol)
		if !ok || symbol.Value != string(from) {
			continue
		}

		if children == nil {
			children = slices.Clone(child.Children)
		}

		// The tag is the first child, so the i-th argument is the child after
		// it. Arguments precede children in a well-formed form, which the
		// structural pass has already required of anything read from a file.
		children[i+1] = symbolNode(string(to))
	}

	if children == nil {
		return child, false
	}

	return relisted(child, children), true
}

// labelled is form with its label written as label, which is added where the
// form carried none and removed where the new label is empty.
func labelled(form *Node, label string) *Node {
	children := make([]*Node, 0, len(form.Children)+1)

	for _, child := range form.Children {
		if tag, ok := formTag(child); ok && tag == labelChild {
			continue
		}
		children = append(children, child)
	}

	if label != "" {
		children = append(children, formNode(labelChild, stringNode(label)))
	}

	return relisted(form, children)
}

// retiredAs is form with the retirement written on it.
//
// It is appended, which decides nothing: canonical form sorts the children of
// every form, so the `retired` child prints where specification section 6.1
// tables it whatever order it was added in.
func retiredAs(form *Node, spec RetirementSpec) *Node {
	return relisted(form, append(slices.Clone(form.Children), spec.form()))
}

// symbolNode is a symbol written on its own.
func symbolNode(value string) *Node {
	return &Node{Datum: sexpr.Symbol{Value: value}}
}

// stringNode is a string written on its own.
func stringNode(value string) *Node {
	return &Node{Datum: sexpr.String{Value: value}}
}

// formNode is a form: a tag and everything written after it.
func formNode(tag string, children ...*Node) *Node {
	elements := make([]*Node, 1, len(children)+1)
	elements[0] = symbolNode(tag)

	return relisted(nil, append(elements, children...))
}

// relisted is a list holding children, keeping whatever the list it replaces
// carried which the children do not.
//
// The comments are the point of the second half. They belong to the list rather
// than to any one of its elements, so rebuilding a form to change one child
// would drop everything somebody wrote inside it — which is a write command
// deleting a comment nobody asked it to touch.
//
// The datum and the spanned children are built together because they are two
// readings of one thing: the datum carries the same sequence unspanned, and a
// tree whose two halves disagreed would print as one thing and report positions
// from the other.
func relisted(list *Node, children []*Node) *Node {
	elements := make([]sexpr.Node, 0, len(children))
	for _, child := range children {
		elements = append(elements, child.Datum)
	}

	datum := sexpr.List{Elements: elements}
	out := &Node{Children: children}

	if list != nil {
		if held, ok := list.Datum.(sexpr.List); ok {
			datum.Pos, datum.Comments = held.Pos, held.Comments
		}
		out.Span, out.Comments = list.Span, list.Comments
	}

	out.Datum = datum

	return out
}
