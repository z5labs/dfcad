// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"slices"
	"strings"
	"sync"
)

// arity is how many of something a form permits: at least min and at most max,
// where a negative max means unbounded.
//
// It spells the specification's four arities — 1, 0..1, 1..n and 0..n — and the
// counted positional arguments of a form with one type, so that the tables
// below read the way section 5 of the specification writes them.
type arity struct {
	min int
	max int
}

// exactly is the arity of something written n times and no more.
func exactly(n int) arity { return arity{min: n, max: n} }

// atMost is the arity of something optional, written at most n times.
func atMost(n int) arity { return arity{min: 0, max: n} }

// atLeast is the arity of something required and repeatable, written at least
// n times.
func atLeast(n int) arity { return arity{min: n, max: -1} }

// repeated is the arity of something optional and repeatable.
var repeated = arity{min: 0, max: -1}

// permits reports whether n of something satisfies the arity.
func (a arity) permits(n int) bool {
	return n >= a.min && (a.max < 0 || n <= a.max)
}

// form is the description of one form of the entity format: what may follow its
// tag, what may be written inside it, and how many of each.
//
// The point of describing a form as data rather than as a function is that
// adding one is an entry in a table. Every rule the validator enforces is read
// from these fields, so a new form arrives with no new code path and no new
// place for the two to disagree.
//
// A form carries no tag of its own. The tag is whatever the parent's [child]
// entry names it — the same description serves `assert` and `invariant`, and a
// claim has no fixed tag at all — and diagnostics name the tag as it was
// written rather than as the table spells it.
type form struct {
	// args is how many positional arguments follow the tag. Everything after
	// them is a child form.
	args arity

	// argsDesc names those arguments for a diagnostic — "an id", "two vertex
	// ids" — and reads after "expected".
	argsDesc string

	// children are the child forms this one permits, in the canonical child
	// order of the specification's tables. Order matters twice: it is the order
	// a printer emits and the order arity diagnostics are reported in.
	children []child

	// claims, when set, describes a claim, and the form permits one under any
	// tag it does not reserve. It is what makes `(width (value 8.5 m) ...)` a
	// claim on a node rather than an unknown child of it.
	claims *form

	// free permits any child tag, unvalidated. It is for the parameters of an
	// assertion, whose names and values belong to the check registry rather
	// than to the shape of the form.
	free bool

	// minElements is how many elements the form must hold at all, counting
	// arguments and children together. It is for the two forms whose emptiness
	// no per-child arity catches: a `value` is one thing or another, and an
	// `accuracy` is one or more terms.
	minElements int

	// noun is what a diagnostic calls this form. Empty means "form".
	noun string
}

// child is one child form a [form] permits, and how many times.
type child struct {
	// tag is the tag the child is written with.
	tag string

	// arity is how many of it the parent permits.
	arity arity

	// form describes the child itself.
	form *form

	// omitted is this child's canonical printing when it holds its default,
	// which canonical form leaves out — `(rank normal)` is written back as
	// nothing at all. Empty means the child has no default and is written back
	// whenever it was written.
	//
	// It is the printing rather than the value because that is what the printer
	// already has in hand, and because it settles the spellings a value has more
	// than one of: `#true` prints as `#t`, so a default written the long way is
	// still recognised as the default.
	omitted string
}

// child returns the description of the child written with tag, and whether the
// form permits one at all.
func (f *form) child(tag string) (child, bool) {
	_, c, ok := f.childAt(tag)
	return c, ok
}

// childAt returns the description of the child written with tag together with
// its place in the canonical child order, and whether the form permits one at
// all.
func (f *form) childAt(tag string) (int, child, bool) {
	i := slices.IndexFunc(f.children, func(c child) bool { return c.tag == tag })
	if i < 0 {
		return 0, child{}, false
	}
	return i, f.children[i], true
}

// tags is the tags of the children the form permits, in canonical order.
func (f *form) tags() []string {
	out := make([]string, 0, len(f.children))
	for _, c := range f.children {
		out = append(out, c.tag)
	}
	return out
}

// name is how a diagnostic refers to this form when it was written with tag.
func (f *form) name(tag string) string {
	noun := f.noun
	if noun == "" {
		noun = "form"
	}
	return "the " + tag + " " + noun
}

// args builds the description of a form which takes positional arguments and
// holds nothing else, which is most of them.
func args(a arity, desc string) *form {
	return &form{args: a, argsDesc: desc}
}

// noArgs is the arguments of a form which takes none and is written with
// children only.
const noArgs = "no arguments"

// article is the indefinite article a tag reads with, so that a diagnostic
// naming a form is a sentence rather than a template with a word slotted into
// it. Every tag of the format is ASCII, so the first letter decides.
func article(tag string) string {
	if strings.ContainsRune("aeiou", rune(tag[0])) {
		return "an"
	}
	return "a"
}

// The forms of the entity format, as specification sections 6 and 7 give them.
// Each var is one table there, and the comment above it cites which.
var (
	// topLevelForm is the ten top-level tags of section 6.
	//
	// Every one of them is repeatable because a file constrains none of their
	// counts: "exactly one project" is a rule about a whole model, checked once
	// every file has been read, and a model spread over ten files would fail it
	// nine times over if it were checked here.
	topLevelForm = &form{
		args:     exactly(0),
		argsDesc: noArgs,
		children: []child{
			{tag: "project", arity: repeated, form: projectForm},
			{tag: "namespace", arity: repeated, form: namespaceForm},
			{tag: "type", arity: repeated, form: typeForm},
			{tag: "predicate", arity: repeated, form: predicateForm},
			{tag: "tolerance", arity: repeated, form: toleranceForm},
			{tag: "frame", arity: repeated, form: frameForm},
			{tag: "node", arity: repeated, form: nodeForm},
			{tag: "vertex", arity: repeated, form: vertexForm},
			{tag: "edge", arity: repeated, form: edgeForm},
			{tag: "loop", arity: repeated, form: loopForm},
		},
	}

	// nodeForm is section 6.1.
	nodeForm = &form{
		args:     exactly(1),
		argsDesc: "an id",
		children: []child{
			{tag: "label", arity: atMost(1), form: args(exactly(1), "a string")},
			{tag: "kind", arity: exactly(1), form: args(exactly(1), "a kind")},
			{tag: "type", arity: exactly(1), form: args(exactly(1), "a type name")},
			{tag: "geometry", arity: atMost(1), form: args(exactly(1), "a geometry form")},
			{tag: "frame", arity: atMost(1), form: args(exactly(1), "a frame id")},
			{tag: "within", arity: atMost(1), form: args(exactly(1), "a node id")},
			{tag: "member-of", arity: repeated, form: args(exactly(1), "a zone node id")},
			{tag: "boundary", arity: repeated, form: args(exactly(1), "a loop id")},
			{tag: "retired", arity: atMost(1), form: retiredForm},
			{tag: "assert", arity: repeated, form: assertForm},
		},
		claims: claimForm,
	}

	// vertexForm is section 6.2.
	vertexForm = &form{
		args:     exactly(1),
		argsDesc: "an id",
		children: []child{
			{tag: "label", arity: atMost(1), form: args(exactly(1), "a string")},
			{tag: "frame", arity: exactly(1), form: args(exactly(1), "a frame id")},
			{tag: "assert", arity: repeated, form: assertForm},
		},
		claims: claimForm,
	}

	// edgeForm is section 6.3.
	edgeForm = &form{
		args:     exactly(1),
		argsDesc: "an id",
		children: []child{
			{tag: "label", arity: atMost(1), form: args(exactly(1), "a string")},
			{tag: "frame", arity: exactly(1), form: args(exactly(1), "a frame id")},
			{tag: "vertices", arity: exactly(1), form: args(exactly(2), "two vertex ids")},
			{tag: "backed-by", arity: repeated, form: args(exactly(1), "a node id")},
			{tag: "assert", arity: repeated, form: assertForm},
		},
		claims: claimForm,
	}

	// loopForm is section 6.4.
	loopForm = &form{
		args:     exactly(1),
		argsDesc: "an id",
		children: []child{
			{tag: "label", arity: atMost(1), form: args(exactly(1), "a string")},
			{tag: "frame", arity: exactly(1), form: args(exactly(1), "a frame id")},
			{tag: "edges", arity: exactly(1), form: args(atLeast(1), "one or more edge ids")},
			{tag: "assert", arity: repeated, form: assertForm},
		},
		claims: claimForm,
	}

	// claimForm is section 6.5. A claim's tag is its predicate, so the table
	// names none.
	claimForm = &form{
		args:     exactly(0),
		argsDesc: noArgs,
		noun:     "claim",
		children: []child{
			{tag: "id", arity: atMost(1), form: args(exactly(1), "a claim id")},
			{tag: "value", arity: exactly(1), form: valueForm},
			{tag: "source", arity: exactly(1), form: args(exactly(1), "a string")},
			{tag: "method", arity: exactly(1), form: args(exactly(1), "a method id")},
			{tag: "accuracy", arity: atMost(1), form: accuracyForm},
			{tag: "date", arity: exactly(1), form: args(exactly(1), "a date string")},
			{tag: "rank", arity: atMost(1), form: args(exactly(1), "normal or deprecated"), omitted: "(rank normal)"},
			{tag: "superseded-by", arity: atMost(1), form: args(exactly(1), "a claim id")},
		},
	}

	// valueForm is section 6.6.
	//
	// Three of the four value shapes are written as arguments — a scalar, a
	// coordinate and a unit, or a string — and the fourth is a transform form.
	// Which shape a predicate takes is registry data, so the shapes are not
	// distinguished here; what is checked is that a value holds something.
	valueForm = &form{
		args:        atMost(2),
		argsDesc:    "a value and its unit",
		minElements: 1,
		children: []child{
			{tag: "transform", arity: atMost(1), form: transformForm},
		},
	}

	// transformForm is section 6.6.3.
	transformForm = &form{
		args:     exactly(0),
		argsDesc: noArgs,
		children: []child{
			{tag: "translation", arity: exactly(1), form: args(exactly(3), "three numbers")},
			{tag: "rotation", arity: exactly(1), form: args(exactly(9), "nine numbers")},
			{tag: "scale", arity: exactly(1), form: args(exactly(1), "one number")},
		},
	}

	// accuracyForm is section 6.6.5. An accuracy carries one or more terms, and
	// the terms are unordered, so both are optional and the form as a whole is
	// not permitted to be empty.
	accuracyForm = &form{
		args:        exactly(0),
		argsDesc:    noArgs,
		minElements: 1,
		children: []child{
			{tag: "independent", arity: repeated, form: args(exactly(2), "a magnitude and a unit")},
			{tag: "systematic", arity: repeated, form: args(exactly(3), "a magnitude, a unit and a term id")},
		},
	}

	// retiredForm is section 6.7.
	retiredForm = &form{
		args:     exactly(0),
		argsDesc: noArgs,
		children: []child{
			{tag: "date", arity: exactly(1), form: args(exactly(1), "a date string")},
			{tag: "reason", arity: exactly(1), form: args(exactly(1), "a string")},
			{tag: "superseded-by", arity: atMost(1), form: args(exactly(1), "a node id")},
		},
	}

	// assertForm is section 6.8, and is the same shape as the `invariant` of
	// section 7.3.
	//
	// A parameter is any tag, and its value is a single datum the check
	// declares the sort of. Neither the names nor the values are structure: the
	// check registry knows which parameters a check takes, and validating them
	// against a guess here would be a second, weaker copy of that.
	assertForm = &form{
		args:     exactly(1),
		argsDesc: "a check name",
		free:     true,
	}

	// projectForm is section 7.1.
	projectForm = &form{
		args:     exactly(0),
		argsDesc: noArgs,
		children: []child{
			{tag: "label", arity: atMost(1), form: args(exactly(1), "a string")},
			{tag: "globalid-namespace", arity: exactly(1), form: args(exactly(1), "a string holding a URL")},
			{tag: "description", arity: atMost(1), form: args(exactly(1), "a string")},
		},
	}

	// namespaceForm is section 7.2.
	namespaceForm = &form{
		args:     exactly(1),
		argsDesc: "a namespace",
		children: []child{
			{tag: "description", arity: exactly(1), form: args(exactly(1), "a string")},
		},
	}

	// typeForm is section 7.3.
	typeForm = &form{
		args:     exactly(1),
		argsDesc: "a type name",
		children: []child{
			{tag: "kind", arity: atLeast(1), form: args(exactly(1), "a kind")},
			{tag: "geometry", arity: atLeast(1), form: args(exactly(1), "a geometry form or absent")},
			{tag: "description", arity: exactly(1), form: args(exactly(1), "a string")},
			{tag: "invariant", arity: repeated, form: assertForm},
		},
	}

	// predicateForm is section 7.4.
	predicateForm = &form{
		args:     exactly(1),
		argsDesc: "a predicate name",
		children: []child{
			{tag: "unit", arity: atMost(1), form: args(exactly(1), "a unit")},
			{tag: "shape", arity: exactly(1), form: args(exactly(1), "a value shape")},
			{tag: "dimension", arity: atMost(1), form: args(exactly(1), "an integer")},
			{tag: "claim-bearing", arity: atMost(1), form: args(exactly(1), "#t or #f"), omitted: "(claim-bearing #t)"},
			{tag: "strict", arity: atMost(1), form: args(exactly(1), "#t or #f"), omitted: "(strict #f)"},
			{tag: "description", arity: atMost(1), form: args(exactly(1), "a string")},
		},
	}

	// frameForm is section 7.5. A frame is both a registry entry and a node,
	// which is why it is the one registry form carrying claims.
	frameForm = &form{
		args:     exactly(1),
		argsDesc: "an id",
		children: []child{
			{tag: "label", arity: atMost(1), form: args(exactly(1), "a string")},
			{tag: "unit", arity: exactly(1), form: args(exactly(1), "a unit")},
			{tag: "parent", arity: atMost(1), form: args(exactly(1), "a frame id")},
			{tag: "transform", arity: atMost(1), form: args(exactly(1), "a claim id")},
		},
		claims: claimForm,
	}

	// toleranceForm is section 7.6.
	toleranceForm = &form{
		args:     exactly(1),
		argsDesc: "a tolerance name",
		children: []child{
			{tag: "value", arity: exactly(1), form: args(exactly(2), "a number and a unit")},
			{tag: "description", arity: atMost(1), form: args(exactly(1), "a string")},
		},
	}
)

// tagIndex is what the tables say about a tag written somewhere it does not
// belong: whether the format knows it at all, where it does belong, and whether
// a form carrying claims reserves it.
type tagIndex struct {
	// permittedIn maps a tag to the contexts which permit it, phrased for a
	// diagnostic — "a node form", "the top level of a file" — in a stable
	// order. A tag the format does not know is absent.
	permittedIn map[string][]string

	// reserved is the structural child tags of the forms which also carry
	// claims, which is the reserved set of specification section 4.2. A tag in
	// it is never read as a predicate, so writing one where its form does not
	// belong is a misplaced form rather than a claim.
	reserved map[string]bool
}

// forms returns the index, built once from the tables above.
//
// It is derived rather than written down so that the reserved set and the
// "belongs in" of every diagnostic stay true by construction when a form is
// added. A second copy of either would be a second thing to forget.
var forms = sync.OnceValue(func() *tagIndex {
	index := &tagIndex{
		permittedIn: make(map[string][]string),
		reserved:    make(map[string]bool),
	}

	// seen keys on the pair rather than on the form alone: one description
	// serves more than one tag, and each context it is reached through is a
	// place its children may be written.
	type context struct {
		form *form
		name string
	}
	seen := make(map[context]bool)

	var walk func(ctx context)
	walk = func(ctx context) {
		if seen[ctx] {
			return
		}
		seen[ctx] = true

		for _, c := range ctx.form.children {
			if !slices.Contains(index.permittedIn[c.tag], ctx.name) {
				index.permittedIn[c.tag] = append(index.permittedIn[c.tag], ctx.name)
			}
			if ctx.form.claims != nil {
				index.reserved[c.tag] = true
			}
			walk(context{form: c.form, name: article(c.tag) + " " + c.tag + " form"})
		}

		if ctx.form.claims != nil {
			walk(context{form: ctx.form.claims, name: "a claim"})
		}
	}
	walk(context{form: topLevelForm, name: topLevelContext})

	for tag := range index.permittedIn {
		slices.Sort(index.permittedIn[tag])
	}

	return index
})
