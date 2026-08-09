// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"slices"
)

// ErrNoRelation is a relation which relates a node to nothing at all.
//
// It is refused rather than applied as a change which does nothing, for the
// reason an empty batch is: a command which named a node and no relation is one
// somebody stopped typing, and reporting it as a successful change of nothing
// would say the relation landed.
var ErrNoRelation = errors.New(
	"a relation names the node which contains this one, a zone it is a member of, or a loop which bounds it",
)

// RelationSpec is how a node is joined to the rest of the model: the one node
// which strictly contains it, the zones it is grouped into, and the loops which
// bound it.
//
// The three are carried together and are never collapsed into one. They are
// different relations with different shapes — containment is at most one and
// nests, membership is any number and does not, a boundary leaves the semantic
// family altogether — and none of them is ever derived from another
// ([0001](docs/decisions/0001-two-node-families.md)). What they share is only
// that they are references a node makes rather than axes it declares, which is
// why [NodeSpec] carries none of them: a node is added and then related, so
// that the refusal to place it and the refusal to relate it are two answers
// rather than one compound one.
//
// Nothing here is resolved. Whether an id names a node this model holds,
// whether the hierarchy permits the pairing, whether a `boundary` names a loop
// and whether following the containment terminates are all questions about the
// whole model, and the model this change produces is interpreted at [Tx.Commit]
// — so a relation to something which does not exist is refused with the
// diagnostic a load of the result would have raised, which is the same one a
// hand-written file gets for the same mistake.
type RelationSpec struct {
	// Within is the node which strictly contains this one, and is empty where
	// the change says nothing about containment.
	//
	// A node is contained by one other node, never two, so writing one replaces
	// whatever parent the node already had rather than being written beside it.
	Within ID

	// MemberOf are zones this node is grouped into, in the order they are
	// written. They are added to whatever memberships the node already
	// declares, because membership is many to many and a node belonging to
	// three zones is as ordinary as one belonging to none.
	MemberOf []ID

	// Boundary are loops which bound this node, in the order they are written,
	// and are added the same way. A node with two is ordinary as well: an
	// outline and the void it encloses are two loops on one node.
	Boundary []ID
}

// relates reports whether the spec says anything at all.
func (spec RelationSpec) relates() bool {
	return spec.Within != "" || len(spec.MemberOf) > 0 || len(spec.Boundary) > 0
}

// Relate writes the references a node makes to the rest of the model: what
// contains it, which zones it is grouped into, and which loops bound it.
//
// It is the other half of [Tx.AddNode], which writes a node's own axes and none
// of its references. A batch which creates a room, a circuit and a receptacle
// and cannot then say the receptacle is in the room and on the circuit has
// written three things and no model, and the relations are exactly the class of
// fact which used to have to be hand-edited into the files afterwards.
//
// **Containment is replaced and the other two are added.** A node is strictly
// contained by one other node, so naming a parent supersedes whatever parent
// was written before; membership and boundary are any number, so naming one
// adds it. Naming a zone or a loop the node already names is not refused here
// and is refused at [Tx.Commit], because a reference written twice is a load
// error and the load is what says so.
//
// **Nothing here is resolved.** A parent which does not exist, a parent the
// hierarchy does not permit, a `member-of` naming something which is not a Zone
// and a `boundary` naming something which is not a loop are each refused when
// the model this change would produce is interpreted, with the diagnostics that
// load would have raised — so relating a node wrongly through this interface
// and authoring the same mistake by hand are answered in the same words.
//
// What is refused here is what is wrong with the invocation rather than with
// the model: an id naming nothing, an id naming something which is not a
// semantic node, and a relation which relates the node to nothing.
func (tx *Tx) Relate(id ID, spec RelationSpec) error {
	if tx.finished {
		return ErrFinished
	}

	if !spec.relates() {
		return ErrNoRelation
	}

	// A zone or a loop named as the empty id is refused rather than dropped. A
	// flag nobody wrote and a flag written empty are the same absence
	// everywhere else in this interface, but here dropping one would write
	// fewer relations than were asked for and report having written them all.
	for _, named := range [][]ID{spec.MemberOf, spec.Boundary} {
		if slices.Contains(named, "") {
			return ErrNoID
		}
	}

	// The subject is a semantic node and never a vertex, an edge or a loop.
	// Geometry carries no `within`, no `member-of` and no `boundary`: it is
	// what a boundary points at, and a loop written inside a storey is the
	// exact confusion the two families exist to keep out of the model.
	if err := tx.references(id, nodeTag); err != nil {
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

	return tx.Replace(form, related(form, spec))
}

// related is form with the relations written on it.
//
// The children are appended, which decides nothing: canonical form sorts the
// children of every form, so they print where specification section 6.1 tables
// them whatever order they were added in.
//
// The one child which is removed is the `within` a new parent replaces. Two of
// them is a model which does not load — a node claiming two parents — so a
// re-parenting which wrote the second beside the first would refuse itself, and
// refuse itself with a diagnostic about the file rather than about the change
// which put it there.
func related(form *Node, spec RelationSpec) *Node {
	children := make([]*Node, 0, len(form.Children)+1+len(spec.MemberOf)+len(spec.Boundary))

	for _, child := range form.Children {
		if tag, ok := formTag(child); ok && tag == withinChild && spec.Within != "" {
			continue
		}
		children = append(children, child)
	}

	if spec.Within != "" {
		children = append(children, formNode(withinChild, symbolNode(string(spec.Within))))
	}

	for _, zone := range spec.MemberOf {
		children = append(children, formNode(memberOfChild, symbolNode(string(zone))))
	}

	for _, loop := range spec.Boundary {
		children = append(children, formNode(boundaryChild, symbolNode(string(loop))))
	}

	return relisted(form, children)
}
