// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"iter"
	"slices"
	"sync"
	"time"

	sexpr "github.com/z5labs/sexpr-go"
)

// Rank is how a claim stands relative to the other claims about the same thing,
// per specification section 6.5.
//
// The set is closed at two members and is compiled in. There is no `preferred`,
// no numeric priority, no weight and no override: a claim cannot be promoted
// above its peers, and where two normal claims disagree the disagreement stays
// visible until one of them is deprecated or corrected
// ([0007](docs/decisions/0007-rank-is-closed.md)).
type Rank string

// The ranks, in the order specification section 6.5 lists them.
const (
	RankNormal     Rank = "normal"
	RankDeprecated Rank = "deprecated"
)

// ranks is the closed set, in specification order.
var ranks = []Rank{RankNormal, RankDeprecated}

// Ranks returns the closed set of claim ranks, in specification order.
func Ranks() []Rank { return slices.Clone(ranks) }

// unknownRank is the diagnostic for a symbol written where a rank belongs.
//
// It says what the set is and that there is nothing else in it, because the
// value somebody reaches for here is usually a priority the format deliberately
// does not have.
func unknownRank(span Span, written string) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message:  fmt.Sprintf("expected a rank, found %s", written),
		Hint: "a rank is one of " + join(spellings(ranks), "and") +
			"; there is no preferred, no numeric priority and no weight",
	}
}

// TermKind is which of the two kinds of error an accuracy term describes, per
// specification section 6.6.5.
type TermKind string

// The accuracy terms, in the order specification section 6.6.5 lists them. The
// string value is the tag the term is written with.
const (
	// TermIndependent is an error which does not correlate with any other.
	// Independent terms combine in quadrature.
	TermIndependent TermKind = "independent"

	// TermSystematic is an error shared with everything else naming the same
	// term id. Systematic terms add linearly, and a term contributing to two
	// inputs of one derivation is counted once.
	TermSystematic TermKind = "systematic"
)

// AccuracyTerm is one term of a claim's accuracy.
//
// The magnitude is a standard uncertainty: one standard deviation, k = 1. There
// is no other storage convention, and a figure quoted at any other coverage is
// converted at the point it enters the model, by whoever enters it
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)).
type AccuracyTerm struct {
	// Kind is which of the two terms this is.
	Kind TermKind

	// Magnitude is the one-sigma figure, in Unit.
	Magnitude float64

	// Unit is the unit the magnitude is expressed in, as it was written.
	Unit Unit

	// Source is the id the systematic error is shared with. Empty for an
	// independent term, which is shared with nothing.
	//
	// Two systematic terms are the same term when their ids are byte-equal, not
	// when their magnitudes happen to match. The namespace has to be one the
	// registry declares; the id need not name a node.
	Source ID

	// Span is where the term was written.
	Span Span
}

// Accuracy is how well a claim's value is known, per specification section
// 6.6.5.
//
// The terms are held in the order they were written. They are unordered as far
// as meaning goes — the canonical printer sorts them — and keeping the written
// order is what lets a diagnostic about one point at the right line.
type Accuracy struct {
	// Terms are the terms written inside the accuracy, in written order. There
	// is at least one: an accuracy holding none is a diagnostic rather than an
	// accuracy.
	Terms []AccuracyTerm

	// Span is where the accuracy was written.
	Span Span
}

// Transform is the value of a transform claim, per specification section 6.6.3.
//
// It maps a point in the frame which declares it to a point in that frame's
// parent. Scale is a scale and not a unit conversion: a transform between
// frames of different units has a scale of one unless the fit genuinely found a
// scale error ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
type Transform struct {
	// Translation is the offset, ordered tx, ty, tz, expressed in the parent
	// frame's linear unit.
	Translation [3]float64

	// Rotation is a 3x3 matrix in row-major order.
	Rotation [9]float64

	// Scale is the one scale factor.
	Scale float64

	// Span is where the transform was written.
	Span Span
}

// Value is the value of one claim, together with the unit it is expressed in,
// per specification section 6.6.
//
// Which of the four shapes a value takes is registry data — the predicate
// declares it — so a Value is read through the accessor for the shape it has
// and yields nothing through the other three. That is what keeps a coordinate
// from being read as a scalar by a caller which forgot to look.
//
// The fields are unexported and read through the methods below. The zero Value
// is the value of a claim whose value could not be read: its shape is the empty
// string, which the closed set has no member for, and every accessor reports
// that it holds nothing.
type Value struct {
	// shape is which of the four shapes the value takes, and so which of the
	// four fields below holds it.
	shape Shape

	// number is a scalar value.
	number float64

	// components are the ordered components of a coordinate value. The order is
	// significant and is never sorted.
	components []float64

	// text is a text value.
	text string

	// transform is a transform value.
	transform Transform

	// unit is the unit the value is expressed in, as it was written. Empty for a
	// non-dimensional predicate, for a text value and for a transform, none of
	// which carries a unit token.
	unit Unit

	// span is where the value form was written.
	span Span
}

// Shape returns which of the four shapes the value takes, which is the empty
// string for a value which could not be read.
func (v Value) Shape() Shape { return v.shape }

// Unit returns the unit the value is expressed in, exactly as it was written.
//
// It is empty for a non-dimensional predicate and for the two shapes which
// carry no unit token. Nothing here converts: a value whose unit is not the one
// its predicate declares is a diagnostic, and the value is not read
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
func (v Value) Unit() Unit { return v.unit }

// Span returns where the value form was written.
func (v Value) Span() Span { return v.span }

// Scalar returns the value as a real number, and whether it is one.
func (v Value) Scalar() (float64, bool) {
	if v.shape != ShapeScalar {
		return 0, false
	}
	return v.number, true
}

// Coordinate returns the ordered components of the value, and whether it is a
// coordinate.
//
// The components are copied, because the order of them is significant and a
// caller which sorted the slice in place would be re-ordering the model's own
// reading of the file.
func (v Value) Coordinate() ([]float64, bool) {
	if v.shape != ShapeCoordinate {
		return nil, false
	}
	return slices.Clone(v.components), true
}

// Text returns the value as a string, and whether it is one.
func (v Value) Text() (string, bool) {
	if v.shape != ShapeText {
		return "", false
	}
	return v.text, true
}

// Transform returns the value as a transform, and whether it is one.
func (v Value) Transform() (Transform, bool) {
	if v.shape != ShapeTransform {
		return Transform{}, false
	}
	return v.transform, true
}

// Claim is one measurable fact about one thing, per specification section 6.5.
//
// A dimension is never a bare column in this system. It is a value plus the
// evidence for it: where it came from, how it was obtained, how good it is and
// when. That is what makes "how wide is that room, and how do you know" one
// lookup rather than a join, and it is the invariant the rest of the engine
// rests on ([0008](docs/decisions/0008-a-bare-scalar-is-a-load-error.md)).
//
// A claim's tag is its predicate. There is no `claim` keyword and no other way
// to attach a value to a node, so which predicate a claim is under is a fact
// about where it was written rather than a child of it.
//
// The fields are unexported and read through the methods below. A Claim is
// read-only once loaded, and the zero value is a claim nothing could be read
// from; every method below works on it.
type Claim struct {
	// id is the claim's own id, which is the zero ID when none was written. It
	// is optional, and required only of a claim something else references.
	id ID

	// subject is the id of the thing the claim is about, which is the form the
	// claim was written inside.
	subject ID

	// predicate is the tag the claim was written under, which is the name of the
	// predicate the registry declares.
	predicate string

	// value is the value and its unit.
	value Value

	// source names the evidence — a report, a drawing, a person, an instrument
	// log. It is a string rather than an id because it cites a document.
	source string

	// method is the id naming how the value was obtained. It is an id so that
	// its namespace is registered like any other, which is what keeps
	// `total-station`, `TotalStation` and `ts` from being three methods.
	method ID

	// accuracy is how well the value is known, and hasAccuracy whether the claim
	// carries one at all. A claim with no accuracy is unrankable rather than
	// accurate to some default.
	accuracy    Accuracy
	hasAccuracy bool

	// date is the day the value was obtained, read from the RFC 3339 full-date
	// the file was written with.
	date time.Time

	// rank is normal unless the claim wrote otherwise, which canonical form
	// leaves out.
	rank Rank

	// supersededBy is the id of the claim which replaced this one. Empty unless
	// the claim is deprecated, and a deprecation without one is a diagnostic the
	// supersession pass reports.
	supersededBy ID

	// span is where the claim form was written.
	span Span
}

// ID returns the claim's own id, and whether it wrote one.
//
// A claim id is optional. It is required of a claim something else references —
// a frame's transform, a supersession — because a reference needs a name to
// resolve through, and it is noise on the great majority of claims which
// nothing points at.
func (c *Claim) ID() (ID, bool) { return c.id, c.id != "" }

// Subject returns the id of the thing the claim is about.
func (c *Claim) Subject() ID { return c.subject }

// Predicate returns the name of the predicate the claim was written under,
// which the consuming repository's registry declares.
func (c *Claim) Predicate() string { return c.predicate }

// Value returns the claim's value and the unit it is expressed in.
func (c *Claim) Value() Value { return c.value }

// Source returns the string naming the evidence for the value.
func (c *Claim) Source() string { return c.source }

// Method returns the id naming how the value was obtained.
//
// The engine attaches no meaning to it beyond the namespace being registered.
// An estimate is a method, and so is an assumption.
func (c *Claim) Method() ID { return c.method }

// Accuracy returns how well the value is known, and whether the claim carries
// an accuracy at all.
//
// The second result is a state and not a failure. A claim with no accuracy
// loads, and is unrankable: it can never win resolution, and it is not given a
// default, because a default would be this package inventing the one number the
// claim exists to record.
func (c *Claim) Accuracy() (Accuracy, bool) { return c.accuracy, c.hasAccuracy }

// Rankable reports whether the claim carries an accuracy, and so whether it can
// take part in resolution at all.
func (c *Claim) Rankable() bool { return c.hasAccuracy }

// Date returns the day the value was obtained.
func (c *Claim) Date() time.Time { return c.date }

// Rank returns the claim's rank, which is [RankNormal] unless the claim wrote
// otherwise.
func (c *Claim) Rank() Rank { return c.rank }

// SupersededBy returns the id of the claim which replaced this one, and whether
// one was written.
func (c *Claim) SupersededBy() (ID, bool) { return c.supersededBy, c.supersededBy != "" }

// Span returns where the claim form was written, which is what a diagnostic
// about the claim as a whole points at.
func (c *Claim) Span() Span { return c.span }

// Claims are the claims of one load: every claim the walk read, in the order it
// read them, indexed by the id of the thing each is about and by the claim's own
// id where it wrote one.
//
// Both indexes are the load's own, for the reason [Nodes] holds one: a second
// index over one model would be a second answer to "what does this model say
// about site:S-101", and the two would differ the first time a claim moved
// between files.
//
// A Claims is read-only once loaded. The zero value holds no claims, which is
// what a source tree holding none yields, and every method below works on it.
type Claims struct {
	// inOrder is every claim read, in the order the walk reached them.
	inOrder []*Claim

	// byID is the claim each written claim id names. A claim which wrote no id
	// is absent, and so is one whose id was already taken: the id goes on naming
	// what it named first.
	byID map[ID]*Claim

	// bySubject is the claims written on each subject, in written order.
	// Repeating a predicate is the normal case, so a subject maps to a sequence
	// rather than to one claim per predicate.
	bySubject map[ID][]*Claim
}

// Len reports how many claims were read.
func (c *Claims) Len() int {
	if c == nil {
		return 0
	}
	return len(c.inOrder)
}

// All iterates the claims in the order the walk reached them.
//
// That order is deterministic — the lexical order of the paths, and within a
// file the order the forms were written — so anything built from it diffs
// against the last run's.
func (c *Claims) All() iter.Seq[*Claim] {
	return func(yield func(*Claim) bool) {
		if c == nil {
			return
		}
		for _, claim := range c.inOrder {
			if !yield(claim) {
				return
			}
		}
	}
}

// Claim returns the claim id names, and whether the model holds one.
//
// The zero ID names nothing, so a claim which wrote no id is reachable through
// [Claims.All] and [Claims.Of] and not through here. That is the whole of the
// arrangement: a claim nothing references needs no name, and a claim something
// references is found by the name it wrote.
func (c *Claims) Claim(id ID) (*Claim, bool) {
	if c == nil {
		return nil, false
	}
	claim, ok := c.byID[id]
	return claim, ok
}

// Of iterates the claims written on one subject, in written order.
//
// A subject with nothing said about it yields nothing, which is a thing with no
// claims rather than a thing which is missing.
func (c *Claims) Of(subject ID) iter.Seq[*Claim] {
	return func(yield func(*Claim) bool) {
		if c == nil {
			return
		}
		for _, claim := range c.bySubject[subject] {
			if !yield(claim) {
				return
			}
		}
	}
}

// Under iterates the claims written on one subject under one predicate, in
// written order.
//
// More than one is the normal case rather than an error. Two width claims on
// one node are two measurements, and the disagreement between them is the most
// valuable thing in the file — which is why this returns all of them and
// resolution is a separate question asked of the result.
func (c *Claims) Under(subject ID, predicate string) iter.Seq[*Claim] {
	return func(yield func(*Claim) bool) {
		if c == nil {
			return
		}
		for _, claim := range c.bySubject[subject] {
			if claim.predicate != predicate {
				continue
			}
			if !yield(claim) {
				return
			}
		}
	}
}

// claimBearing is the top-level forms which carry claims, by the tag they are
// written with.
//
// It is derived from the form tables rather than written down so that a form
// which gains claims is read here without a second list being remembered. The
// semantic family, the three geometric forms and the frame all carry them: a
// claim is a claim wherever it is written, and the only thing the enclosing form
// decides is which tags are structural children of it rather than predicates.
var claimBearing = sync.OnceValue(func() map[string]*form {
	out := make(map[string]*form)
	for _, c := range topLevelForm.children {
		if c.form.claims != nil {
			out[c.tag] = c.form
		}
	}
	return out
})

// LoadClaims reads every claim beneath root, checked against registry.
//
// root is what [Load] takes: a single file, or a directory beneath which every
// file whose extension is [Extension] is read. Claims come back in the order the
// walk reached them, which is deterministic, and indexed both by the subject
// they are about and by their own id, so neither resolving a reference nor
// asking what a model says about a thing has to scan.
//
// registry is a loaded one, because every question this pass asks past the shape
// of the form is one only registry data answers: is this predicate declared,
// which of the four shapes does its value take, which unit is it expressed in,
// and is the namespace of this id one the model declares. A nil registry
// declares nothing, and every claim is then reported as naming a predicate
// nothing declares, which is both true and the diagnostic somebody whose
// registry has not been written yet needs.
//
// Loading is one pass which reports everything it finds. A claim which is
// structurally wrong, a predicate nothing declares, a value of the wrong shape,
// a unit which is not the declared one, a date which is not one and a rank which
// is not one of the two are each a diagnostic, and none of them stops the rest
// of the tree from being read.
//
// Claims which could be read come back whatever the diagnostics say, for the
// reason a node whose type is undeclared is still a node: a caller reporting on
// a tree wants to say what the claim says as well as what is wrong with it.
//
// Two things this pass deliberately leaves alone. A predicate the registry
// declares non-claim-bearing takes a plain value and not a claim form, and
// choosing between the two spellings is the bare-scalar rule's job rather than
// this one's; a form written with no children is not read here. And whether a
// deprecated claim names the claim which replaced it, and whether that chain
// terminates, is the supersession pass's question — what is read here is the
// reference itself.
//
// Diagnostics come back in the order the pass found them. Collecting them into
// a [Diagnostics] is what puts them in reporting order.
func LoadClaims(root string, registry *Registry) (*Claims, []Diagnostic) {
	l := &claimLoader{
		registry: registry,
		claims: &Claims{
			byID:      make(map[ID]*Claim),
			bySubject: make(map[ID][]*Claim),
		},
		defined: make(map[ID]Span),
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

	return l.claims, l.diags
}

// claimLoader collects one load of the claims of a source tree.
type claimLoader struct {
	reader

	// registry is what the claims are judged against. It is not written to.
	registry *Registry

	// claims are the claims read so far, in the order they were reached.
	claims *Claims

	// defined is where the claim holding each id was written, which is what a
	// duplicate points its reader back at.
	defined map[ID]Span

	// references are the places something named a claim by its id. They are
	// resolved once every file has been read, because a claim in the last file
	// the walk reaches is as written as one in the first.
	references []claimReference
}

// claimReference is one place a claim was named by its id.
type claimReference struct {
	// id is the claim id which was written.
	id ID

	// at is where it was written.
	at Span

	// by is where the form making the reference was written, which is the other
	// end a diagnostic about a reference which does not resolve names.
	by Span

	// what the reference is, phrased for a diagnostic.
	what string
}

// file interprets the claims of one loaded file.
func (l *claimLoader) file(file *File) {
	for _, node := range file.Nodes {
		tag, ok := formTag(node)
		if !ok {
			continue
		}

		enclosing, ok := claimBearing()[tag]
		if !ok {
			continue
		}

		l.subject(node, enclosing, tag)
	}
}

// subject reads the claims written on one form.
//
// The form itself is not validated here, and neither is its id. Whether a node
// declares its kind, whether a frame's parent and transform are written
// together, and whether the id after the tag is one are questions the pass which
// owns that form answers, and answering them a second time here would be one
// mistake reported twice by two passes over the same tree.
func (l *claimLoader) subject(node *Node, enclosing *form, tag string) {
	subject := subjectID(node)

	_, children := split(elements(node))
	for _, child := range children {
		written, ok := formTag(child)
		if !ok {
			continue
		}

		// A tag the enclosing form reserves is a structural child of it. A tag
		// it does not reserve is a claim on it, whatever it is spelled, which is
		// what makes `(width (value 8.5 m) ...)` a claim rather than an unknown
		// child.
		if _, structural := enclosing.child(written); structural {
			if tag == frameTag && written == transformChild {
				l.reference(node, child, subject)
			}
			continue
		}

		// A form written with no child forms is the plain value of a predicate
		// the registry declares non-claim-bearing, or somebody writing one where
		// a claim belongs. Both are the bare-scalar rule's to decide between.
		if _, inside := split(elements(child)); len(inside) == 0 {
			continue
		}

		if diags := validateForm(child, claimForm, written); len(diags) > 0 {
			l.add(diags...)
			continue
		}

		l.declare(subject, child, written)
	}
}

// declare reads one structurally valid claim form.
//
// Every child is read whatever happened to the ones before it, because a claim
// with an unparseable date still has a value worth checking against its
// predicate. Bailing out on the first would turn fixing a file into a guessing
// loop.
func (l *claimLoader) declare(subject ID, form *Node, predicate string) {
	claim := &Claim{
		subject:   subject,
		predicate: predicate,
		rank:      RankNormal,
		span:      form.Span,
	}

	var id Span
	if arg, ok := argumentOf(form, "id"); ok {
		if read, ok := l.id(arg, "a claim id"); ok {
			claim.id, id = read, arg.Span
		}
	}

	if arg, ok := argumentOf(form, "source"); ok {
		claim.source, _ = l.text(arg, "a string naming the evidence")
	}

	if arg, ok := argumentOf(form, "method"); ok {
		if method, ok := l.id(arg, "a method id"); ok {
			claim.method = method
			l.registered(l.registry, method, arg.Span)
		}
	}

	if arg, ok := argumentOf(form, "date"); ok {
		claim.date, _ = l.date(arg, "a date")
	}

	if arg, ok := argumentOf(form, "rank"); ok {
		if rank, ok := l.rank(arg); ok {
			claim.rank = rank
		}
	}

	if arg, ok := argumentOf(form, "superseded-by"); ok {
		if superseded, ok := l.id(arg, "a claim id"); ok {
			claim.supersededBy = superseded
			l.registered(l.registry, superseded, arg.Span)
		}
	}

	if child, ok := childForm(form, "accuracy"); ok {
		claim.accuracy, claim.hasAccuracy = l.accuracy(child)
	}

	// The predicate is looked up before the value is read because the value is
	// judged against what it declares. A predicate nothing declares declares no
	// shape and no unit, so the value is read as it was written and reported
	// against nothing: a claim under a misspelled predicate hears about the
	// misspelling once rather than about the misspelling and then about a shape
	// nothing can judge.
	declared, isDeclared := l.registry.Predicate(predicate)
	if !isDeclared {
		l.add(l.registry.Undeclared(SortPredicate, predicate, tagSpan(form)))
	}

	if child, ok := childForm(form, "value"); ok {
		claim.value = l.value(child, predicate, declared, isDeclared)
	}

	l.identify(claim, id)

	l.claims.inOrder = append(l.claims.inOrder, claim)
	if subject != "" {
		l.claims.bySubject[subject] = append(l.claims.bySubject[subject], claim)
	}
}

// identify checks a claim's id: that its namespace is one the registry
// declares, and that no other claim already holds it.
//
// A claim which wrote no id, or whose id could not be read, is not checked. It
// has no namespace to look up and nothing to collide with, and it is reachable
// by nothing — which is exactly what an unreferenced claim needs to be.
//
// Whether a claim id collides with a node or a frame id is a question about the
// whole graph rather than about the claims, and is asked once every family has
// been read.
func (l *claimLoader) identify(claim *Claim, at Span) {
	if at == (Span{}) {
		return
	}

	l.registered(l.registry, claim.id, at)

	if first, ok := l.defined[claim.id]; ok {
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     at,
			Message: fmt.Sprintf(
				"expected an id nothing else holds, found %s, which already names a claim in this model",
				claim.id,
			),
			Hint:    "an id is unique across the whole model, and is never issued again to a different thing",
			Related: []RelatedLocation{{Span: first, Message: "first defined here"}},
		})
		return
	}

	l.defined[claim.id] = at
	l.claims.byID[claim.id] = claim
}

// rank reads the one member of the closed set of ranks a claim declares.
func (l *claimLoader) rank(arg *Node) (Rank, bool) {
	written, ok := l.symbol(arg, "a rank")
	if !ok {
		return "", false
	}

	rank := Rank(written)
	if !slices.Contains(ranks, rank) {
		l.add(unknownRank(arg.Span, written))
		return "", false
	}

	return rank, true
}

// accuracy reads the terms of one accuracy form.
//
// The magnitudes are recorded in the units they were written with and are not
// converted or compared here. Whether a term's unit is one of the same quantity
// as the value's is a question about what a unit means, and what a unit means is
// the arithmetic layer's rather than this one's.
//
// An accuracy reports itself present only when a term could be read from it. An
// accuracy whose only term was malformed has already been reported as that, and
// calling the claim rankable on the strength of it would be ranking it by a
// magnitude nobody wrote.
func (l *claimLoader) accuracy(form *Node) (Accuracy, bool) {
	accuracy := Accuracy{Span: form.Span}

	_, children := split(elements(form))
	for _, child := range children {
		tag, ok := formTag(child)
		if !ok {
			continue
		}

		kind := TermKind(tag)
		if kind != TermIndependent && kind != TermSystematic {
			continue
		}

		term := AccuracyTerm{Kind: kind, Span: child.Span}

		arg, ok := argument(child, 0)
		if !ok {
			continue
		}
		magnitude, ok := l.real(arg, "a magnitude")
		if !ok {
			continue
		}
		term.Magnitude = magnitude

		arg, ok = argument(child, 1)
		if !ok {
			continue
		}
		unit, ok := l.symbol(arg, "a unit")
		if !ok {
			continue
		}
		term.Unit = Unit(unit)

		if kind == TermSystematic {
			arg, ok = argument(child, 2)
			if !ok {
				continue
			}
			source, ok := l.id(arg, "a term id")
			if !ok {
				continue
			}
			l.registered(l.registry, source, arg.Span)
			term.Source = source
		}

		accuracy.Terms = append(accuracy.Terms, term)
	}

	return accuracy, len(accuracy.Terms) > 0
}

// value reads a claim's value, checked against the shape and the unit the
// predicate declares.
//
// A value whose shape is not the declared one is not read at all. Reading a
// coordinate where a scalar was declared would put a value into the model that
// everything above it would then have to guard against, and the diagnostic
// already carries what was written.
func (l *claimLoader) value(form *Node, predicate string, declared Predicate, isDeclared bool) Value {
	written, children := split(elements(form))

	shape, ok := writtenShape(written, children)
	if !ok {
		// A value form holding nothing is already an arity diagnostic; a value
		// form holding something which is no value at all is reported as what it
		// was written as.
		if len(written) > 0 {
			l.wrong(written[0], "a value")
		}
		return Value{span: form.Span}
	}

	// A predicate whose own shape could not be read declares none, and a
	// mismatch against nothing is not a mismatch.
	if isDeclared && declared.Shape != "" && shape != declared.Shape {
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     form.Span,
			Message: fmt.Sprintf(
				"expected the %s value the predicate %s declares, found %s",
				declared.Shape, predicate, spellShape(shape),
			),
			Hint:    fmt.Sprintf("%s declares a %s value", predicate, declared.Shape),
			Related: []RelatedLocation{{Span: declared.Span, Message: "the predicate is declared here"}},
		})
		return Value{span: form.Span}
	}

	value := Value{shape: shape, span: form.Span}

	switch shape {
	case ShapeScalar:
		number, read := l.real(written[0], "a real number")
		unit, permitted := l.unit(form, written, predicate, declared, isDeclared)
		if !read || !permitted {
			return Value{span: form.Span}
		}
		value.number, value.unit = number, unit

	case ShapeCoordinate:
		components, read := l.components(written[0], predicate, declared, isDeclared)
		unit, permitted := l.unit(form, written, predicate, declared, isDeclared)
		if !read || !permitted {
			return Value{span: form.Span}
		}
		value.components, value.unit = components, unit

	case ShapeText:
		text, read := l.text(written[0], "a string")
		_, permitted := l.unit(form, written, predicate, declared, isDeclared)
		if !read || !permitted {
			return Value{span: form.Span}
		}
		value.text = text

	case ShapeTransform:
		transform, read := l.transform(children[0])
		if !read {
			return Value{span: form.Span}
		}
		value.transform = transform
	}

	return value
}

// unit reads the unit token after a value and checks it against the one the
// predicate declares.
//
// The rule is equality and not convertibility, in both directions. A value in a
// unit the predicate does not declare is a diagnostic with a position rather
// than a number quietly off by a factor of a thousand, and there is no unitless
// token, so a non-dimensional predicate's value is written with no unit at all
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
func (l *claimLoader) unit(form *Node, written []*Node, predicate string, declared Predicate, isDeclared bool) (Unit, bool) {
	at := form.Span

	var unit Unit
	if len(written) > 1 {
		at = written[1].Span

		read, ok := l.symbol(written[1], "a unit")
		if !ok {
			// Already reported as whatever was written there instead.
			return "", false
		}
		unit = Unit(read)
	}

	if !isDeclared {
		return unit, true
	}

	related := []RelatedLocation{{Span: declared.Span, Message: "the predicate is declared here"}}

	switch {
	case declared.Unit == unit:
		return unit, true

	case declared.Unit == "":
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     at,
			Message:  fmt.Sprintf("expected no unit after the value, found %s", unit),
			Hint: fmt.Sprintf(
				"%s declares no unit, and there is no unitless token: a non-dimensional value is written with no unit at all",
				predicate,
			),
			Related: related,
		})

	case unit == "":
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     at,
			Message:  "expected a unit after the value, found none",
			Hint: fmt.Sprintf(
				"%s declares %s, and a dimensional value is written with its unit",
				predicate, declared.Unit,
			),
			Related: related,
		})

	default:
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     at,
			Message:  fmt.Sprintf("expected the unit the predicate %s declares, found %s", predicate, unit),
			Hint: fmt.Sprintf(
				"%s declares %s, and a value written in another unit is converted by whoever writes it rather than by the loader",
				predicate, declared.Unit,
			),
			Related: related,
		})
	}

	return "", false
}

// components reads the ordered components of a coordinate value, checked
// against the dimension the predicate declares.
//
// The order is significant and is never sorted: the components are the axes of
// the frame the value is expressed in, in the order that frame gives them.
func (l *claimLoader) components(written *Node, predicate string, declared Predicate, isDeclared bool) ([]float64, bool) {
	if isDeclared && declared.Dimension > 0 && len(written.Children) != declared.Dimension {
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     written.Span,
			Message: fmt.Sprintf(
				"expected the %d components the predicate %s declares, found %s",
				declared.Dimension, predicate, count(len(written.Children)),
			),
			Hint:    fmt.Sprintf("%s declares a coordinate of %d components", predicate, declared.Dimension),
			Related: []RelatedLocation{{Span: declared.Span, Message: "the predicate is declared here"}},
		})
		return nil, false
	}

	components := make([]float64, 0, len(written.Children))
	for _, child := range written.Children {
		component, ok := l.real(child, "a real number")
		if !ok {
			return nil, false
		}
		components = append(components, component)
	}

	return components, true
}

// transform reads the three children of a transform value.
//
// Every count is fixed by the form tables and has already been checked, so what
// is read here is the numbers themselves. They are ordered — tx ty tz, the
// rotation row by row — and none of them is sorted.
func (l *claimLoader) transform(form *Node) (Transform, bool) {
	transform := Transform{Span: form.Span}
	ok := true

	if child, written := childForm(form, "translation"); written {
		ok = l.reals(child, transform.Translation[:]) && ok
	}

	if child, written := childForm(form, "rotation"); written {
		ok = l.reals(child, transform.Rotation[:]) && ok
	}

	if child, written := childForm(form, "scale"); written {
		var scale [1]float64
		ok = l.reals(child, scale[:]) && ok
		transform.Scale = scale[0]
	}

	return transform, ok
}

// reals reads the positional arguments of a form into out, in order.
func (l *claimLoader) reals(form *Node, out []float64) bool {
	ok := true

	for i := range out {
		arg, written := argument(form, i)
		if !written {
			ok = false
			continue
		}

		component, read := l.real(arg, "a real number")
		if !read {
			ok = false
			continue
		}

		out[i] = component
	}

	return ok
}

// reference records one place a claim was named by its id, to be resolved once
// every file has been read.
func (l *claimLoader) reference(form *Node, child *Node, subject ID) {
	arg, ok := argument(child, 0)
	if !ok {
		return
	}

	// Whether what was written is an id at all belongs to the pass which owns
	// the form, so it is read here without a word about it.
	symbol, ok := arg.Datum.(sexpr.Symbol)
	if !ok {
		return
	}
	id, err := ParseID(symbol.Value)
	if err != nil {
		return
	}

	// The other end is the form making the reference, named by its own id
	// rather than by the whole of it: a related location spanning a frame and
	// the claim written inside it quotes a dozen lines to point at one.
	by := form.Span
	if name, ok := argument(form, 0); ok {
		by = name.Span
	}

	l.references = append(l.references, claimReference{
		id:   id,
		at:   arg.Span,
		by:   by,
		what: fmt.Sprintf("the transform of the frame %s", subject),
	})
}

// resolve checks that everything which named a claim named one which exists.
//
// It is a second pass because a claim is a property of the source tree rather
// than of a file: a frame declared in the first file the walk reaches may name a
// claim written in the last, and a loader which resolved as it read would report
// it missing for no reason but the order the directory happened to be listed in.
//
// A reference which resolves to nothing is the other half of the rule that makes
// a claim id optional. A claim nothing points at needs no name; a claim
// something points at is found by the name it wrote, and one which wrote none is
// a claim the reference cannot reach.
func (l *claimLoader) resolve() {
	for _, reference := range l.references {
		if _, ok := l.claims.byID[reference.id]; ok {
			continue
		}

		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     reference.at,
			Message: fmt.Sprintf(
				"expected a claim id something in this model carries, found %s, which names no claim",
				reference.id,
			),
			Hint:    "a claim which anything references carries an (id ...) of its own; a claim nothing references may leave it out",
			Related: []RelatedLocation{{Span: reference.by, Message: reference.what + " is written here"}},
		})
	}
}

// frameTag is the top-level tag of the one registry form which carries claims.
const frameTag = "frame"

// transformChild is the child of a frame naming the claim which measures the
// relationship to its parent.
const transformChild = "transform"

// subjectID reads the id a claim-bearing form was written with, without
// reporting anything about it.
//
// Every form which carries claims is written with its id as its first argument,
// and whether that id is one is the question of the pass which owns the form. A
// claim on a subject whose id could not be read is still read and still
// reported on; it is simply about nothing this model can name.
func subjectID(form *Node) ID {
	arg, ok := argument(form, 0)
	if !ok {
		return ""
	}

	symbol, ok := arg.Datum.(sexpr.Symbol)
	if !ok {
		return ""
	}

	id, err := ParseID(symbol.Value)
	if err != nil {
		return ""
	}

	return id
}

// tagSpan is where a form's tag was written, which is what a diagnostic about
// the tag itself — a predicate nothing declares — points at.
func tagSpan(form *Node) Span {
	if len(form.Children) == 0 {
		return form.Span
	}
	return form.Children[0].Span
}

// writtenShape reports which of the four shapes a value takes as written, and
// whether it is one of them at all.
//
// It reads the shape from the file rather than from the registry on purpose: the
// diagnostic for a value of the wrong shape has to say what was found, and a
// value under a predicate nothing declares is still read as whatever it is.
//
// The arguments decide first. A transform is the one shape written as a child
// form, so anything written before one is a value of another shape with a
// transform after it rather than a transform with something in front of it.
func writtenShape(written, children []*Node) (Shape, bool) {
	if len(written) > 0 {
		switch written[0].Datum.(type) {
		case sexpr.String:
			return ShapeText, true
		case sexpr.Float, sexpr.Int:
			return ShapeScalar, true
		case sexpr.List:
			return ShapeCoordinate, true
		}
		return "", false
	}

	if len(children) > 0 {
		if tag, ok := formTag(children[0]); ok && tag == transformChild {
			return ShapeTransform, true
		}
	}

	return "", false
}

// spellShape is how a diagnostic names a value of this shape as it was found,
// which is what it looks like rather than what the registry calls it.
func spellShape(shape Shape) string {
	switch shape {
	case ShapeScalar:
		return "a number"
	case ShapeCoordinate:
		return "a coordinate"
	case ShapeTransform:
		return "a transform"
	case ShapeText:
		return "a string"
	}
	return "something else"
}
