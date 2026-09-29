// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import "slices"

// SetEdges replaces the edges the loop id names is traversed through.
//
// The whole ordered list is replaced, as [Tx.AddLoop] writes it: the order is
// the data (specification section 6.4), so a change to a ring states the ring
// rather than patching one, and a patch to an ordered list is the edit most
// likely to be wrong. The edges are written in the order they were given and
// never sorted. Everything else the loop carries — its label, frame, claims,
// assertions, observation links and the comments written inside it — is the
// subtree which was there, and its identity is fixed
// ([0002](docs/decisions/0002-immutable-id-mutable-label.md)). Every node which
// names the loop in `boundary` reads the new ring with no edit of its own,
// because the shape is shared rather than copied.
//
// What is refused here is what is wrong with the invocation, before anything is
// written: an empty id, an empty list of edges, an edge named as the empty id,
// an id nothing holds or which names something other than a loop, and an edge id
// which names nothing or something other than an edge. The edges are resolved
// as [Tx.AddLoop] resolves them, against the model and against what this same
// change has already written, so an edge written earlier in the change counts.
//
// Whether the edges are expressed in the loop's frame is judged when the model
// the change produces is loaded: [Tx.Commit] refuses a ring naming an edge in
// another frame with the diagnostic a load of the result would have raised, the
// one the same edit typed into the file by hand gets.
//
// **Whether the ring closes is judged nowhere on the write path**, as it is not
// for [Tx.AddLoop]. It is a question about where the edges' vertices are, which
// is answered against a tolerance the registry names, and the load names none.
// A ring which does not close is written, and the `boundary-loops-close` check
// reports it wherever the model asks for it — which is what makes this the edit
// that proves that check can fail.
func (tx *Tx) SetEdges(id ID, edges []ID) error {
	if tx.finished {
		return ErrFinished
	}

	if id == "" {
		return ErrNoID
	}

	if len(edges) == 0 {
		return ErrNoEdges
	}

	// An edge named as the empty id is refused rather than dropped, for the
	// reason [BackingSpec.Check] refuses one: dropping it would write a shorter
	// ring than was asked for and report having written the one asked for.
	if slices.Contains(edges, "") {
		return ErrNoID
	}

	if err := tx.references(id, loopTag); err != nil {
		return err
	}

	registry := tx.graph.Registry()
	for _, edge := range edges {
		if err := declaredNamespace(registry, edge); err != nil {
			return err
		}
		if err := tx.references(edge, edgeTag); err != nil {
			return err
		}
	}

	// The loop is there and the transaction holds no form under its id, which
	// is what a form removed by an earlier mutation of this transaction leaves
	// behind. It is a different answer from an id nothing answers to, which the
	// check above has already given.
	form, ok := tx.Form(id)
	if !ok {
		return UnknownFormError{}
	}

	return tx.Replace(form, traversedAs(form, edges))
}

// traversedAs is form with its `edges` child replaced by one naming edges, in
// the order given.
//
// The child is replaced where it stood rather than removed and appended, and the
// list it replaces lends it its comments, so that a comment somebody wrote
// inside the ring survives the ring being rewritten. A loop form which carried
// no `edges` child at all — which does not load, so is not one a model holds —
// gets one appended.
func traversedAs(form *Node, edges []ID) *Node {
	ring := make([]*Node, 0, len(edges))
	for _, edge := range edges {
		ring = append(ring, symbolNode(string(edge)))
	}

	children := slices.Clone(form.Children)

	for at, child := range children {
		if tag, ok := formTag(child); ok && tag == edgesChild {
			children[at] = relisted(child, append([]*Node{child.Children[0]}, ring...))
			return relisted(form, children)
		}
	}

	return relisted(form, append(children, formNode(edgesChild, ring...)))
}
