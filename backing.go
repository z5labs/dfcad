// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrNoBacking is a change to an edge's backing which states neither what it is
// backed by nor that it is virtual.
//
// It is refused rather than read as "backed by nothing", for the reason a
// relation to nothing is ([ErrNoRelation]): a command which named an edge and no
// backing is one somebody stopped typing, and writing the edge virtual on the
// strength of it would remove a wall nobody asked to remove. The empty set is a
// statement, and it is made explicitly.
var ErrNoBacking = errors.New(
	"a backing names the elements an edge is backed by, or says that it is virtual",
)

// ConflictingBackingError is a change to an edge's backing which says both that
// it is virtual and what it is backed by.
//
// The two are contradictory rather than additive: a virtual edge is one backed
// by nothing, so there is no reading of the pair which writes what was asked.
type ConflictingBackingError struct {
	// ID is the edge the change was about.
	ID ID

	// BackedBy are the elements the change also named.
	BackedBy []ID
}

// Error implements the [error] interface.
func (e ConflictingBackingError) Error() string {
	named := make([]string, 0, len(e.BackedBy))
	for _, id := range e.BackedBy {
		named = append(named, string(id))
	}

	return fmt.Sprintf(
		"expected %s to be backed by elements or to be virtual, found both: virtual and backed by %s",
		e.ID, strings.Join(named, ", "),
	)
}

// BackingSpec is what an edge is physically realised by: the whole set, stated
// rather than patched.
//
// `backed-by` is unordered and repeatable, so a change to it says what the edge
// is backed by now rather than which references to add and which to remove. The
// empty set is stated with Virtual, never by leaving BackedBy empty: an absent
// statement must not read as "backed by nothing"
// (specification section 6.3).
type BackingSpec struct {
	// BackedBy are the ids of the elements which physically realise the edge,
	// in the order they were given. Canonical form sorts them, so the order
	// decides nothing.
	BackedBy []ID

	// Virtual says the edge is backed by nothing at all, which is how a
	// demolished wall's edge becomes the open line it now is.
	Virtual bool
}

// Check reports what is wrong with the spec on its own terms, before any model
// is read: that it states neither the elements nor that the edge is virtual,
// that it states both, or that one of the elements is the empty id.
//
// The edge the spec is written on is named so that the refusal can say which
// edge it was about.
func (spec BackingSpec) Check(id ID) error {
	switch {
	case spec.Virtual && len(spec.BackedBy) > 0:
		return ConflictingBackingError{ID: id, BackedBy: slices.Clone(spec.BackedBy)}
	case !spec.Virtual && len(spec.BackedBy) == 0:
		return ErrNoBacking
	}

	// An element named as the empty id is refused rather than dropped, for the
	// reason [Tx.Relate] refuses one: dropping it would write fewer backings
	// than were asked for and report having written them all.
	if slices.Contains(spec.BackedBy, "") {
		return ErrNoID
	}

	return nil
}

// SetBacking replaces what the edge id names is physically realised by.
//
// Every `backed-by` the edge wrote is removed and the ones the spec states are
// written in their place, so that a wall built after its edge was drawn, a wall
// demolished and a wall replaced by another are each one statement of what the
// edge is backed by now. Its vertices, frame, label, claims and assertions are
// untouched: none of them is what the change is about, and an edge's identity is
// fixed ([0002](docs/decisions/0002-immutable-id-mutable-label.md)).
//
// Whether the edge is then a physical boundary or a virtual one is computed from
// what this writes and is stored nowhere, so the change flips that answer with
// no other edit (specification section 6.3,
// [0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// What is refused here is what is wrong with the invocation: an empty id, an id
// nothing holds, an id naming something other than an edge, and a spec
// [BackingSpec.Check] refuses. **The elements are not resolved here.** One which
// names nothing, one which names geometry and one which names a node of another
// kind are each refused at [Tx.Commit] with the diagnostic a load of the result
// would have raised — the one the same mistake gets when it is typed into a file
// by hand.
//
// Retiring the element a demolished wall was is a second change, after this one.
// [Tx.Retire] reads the references of the model as the change found it, so an
// edge made virtual earlier in the same change still counts as referring to the
// element.
func (tx *Tx) SetBacking(id ID, spec BackingSpec) error {
	if tx.finished {
		return ErrFinished
	}

	if id == "" {
		return ErrNoID
	}

	if err := spec.Check(id); err != nil {
		return err
	}

	if err := tx.references(id, edgeTag); err != nil {
		return err
	}

	// The edge is there and the transaction holds no form under its id, which
	// is what a form removed by an earlier mutation of this transaction leaves
	// behind. It is a different answer from an id nothing answers to, which the
	// check above has already given.
	form, ok := tx.Form(id)
	if !ok {
		return UnknownFormError{}
	}

	return tx.Replace(form, backedAs(form, spec.BackedBy))
}

// backedAs is form with every `backed-by` it wrote replaced by one naming each
// of backing.
//
// The new children are appended, which decides nothing: canonical form sorts the
// children of every form, and repeats among themselves, so they print where
// specification section 6.3 tables them whatever order they were given in.
// Everything else the form carried — its comments among it — is the subtree
// which was there.
func backedAs(form *Node, backing []ID) *Node {
	children := make([]*Node, 0, len(form.Children)+len(backing))

	for _, child := range form.Children {
		if tag, ok := formTag(child); ok && tag == backedByChild {
			continue
		}
		children = append(children, child)
	}

	for _, element := range backing {
		children = append(children, formNode(backedByChild, symbolNode(string(element))))
	}

	return relisted(form, children)
}
