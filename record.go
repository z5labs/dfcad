// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	sexpr "github.com/z5labs/sexpr-go"
)

// The children of a claim, per specification section 6.5. They are named here
// rather than written out at each use because a claim is built here, rewritten
// here when it is retracted, and read by the loader against the same tags.
const (
	idChild       = "id"
	valueChild    = "value"
	sourceChild   = "source"
	methodChild   = "method"
	accuracyChild = "accuracy"
	dateChild     = "date"
	rankChild     = "rank"
)

// ErrNoSubject is a claim written about nothing.
var ErrNoSubject = errors.New("a claim is written about a subject")

// ErrNoPredicate is a claim written under no predicate. A claim's tag is its
// predicate, so a claim with no predicate has no tag to be written with.
var ErrNoPredicate = errors.New("a claim is written under a predicate, which is its tag")

// NotClaimBearingError is a claim written under a predicate the registry
// declares takes a plain value instead.
//
// The two directions are one rule read from either side, and the loader reports
// the other one: a claim form under a non-claim-bearing predicate and a plain
// value under a claim-bearing one are each refused, naming the predicate and
// what the registry says about it.
type NotClaimBearingError struct {
	// Predicate is the predicate that was written under.
	Predicate string

	// Written is the plain value a claim under it would have to be written as
	// instead.
	Written string
}

// Error implements the [error] interface.
func (e NotClaimBearingError) Error() string {
	return fmt.Sprintf(
		"expected the plain value the predicate %s declares, found a claim: it is declared (claim-bearing #f) and takes its value directly, as %s",
		e.Predicate, e.Written,
	)
}

// ValueShapeError is a value of a shape other than the one its predicate
// declares.
type ValueShapeError struct {
	// Predicate is the predicate the value was written under.
	Predicate string

	// Want is the shape the registry declares.
	Want Shape

	// Got is the shape of the value that was written, and is empty for a claim
	// carrying no value at all.
	Got Shape
}

// Error implements the [error] interface.
func (e ValueShapeError) Error() string {
	if e.Got == "" {
		return fmt.Sprintf("expected the %s value the predicate %s declares, found none", e.Want, e.Predicate)
	}
	return fmt.Sprintf(
		"expected the %s value the predicate %s declares, found %s",
		e.Want, e.Predicate, spellShape(e.Got),
	)
}

// DimensionError is a coordinate with a number of components other than the one
// its predicate declares.
type DimensionError struct {
	// Predicate is the predicate the coordinate was written under.
	Predicate string

	// Want is how many components the registry declares.
	Want int

	// Got is how many were written.
	Got int
}

// Error implements the [error] interface.
func (e DimensionError) Error() string {
	return fmt.Sprintf(
		"expected the %d components the predicate %s declares, found %s",
		e.Want, e.Predicate, count(e.Got),
	)
}

// UnitError is a value expressed in a unit other than the one its predicate
// declares.
//
// The rule is equality and not convertibility, in both directions: a value in
// another unit is converted by whoever writes it rather than by the engine, and
// there is no unitless token, so a non-dimensional value is written with no unit
// at all ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
type UnitError struct {
	// Predicate is the predicate the value was written under.
	Predicate string

	// Want is the unit the registry declares, and is empty for a
	// non-dimensional predicate.
	Want Unit

	// Got is the unit that was written, and is empty where none was.
	Got Unit
}

// Error implements the [error] interface.
func (e UnitError) Error() string {
	switch {
	case e.Want == "":
		return fmt.Sprintf(
			"expected no unit after the value, found %s: %s declares no unit, and there is no unitless token",
			e.Got, e.Predicate,
		)
	case e.Got == "":
		return fmt.Sprintf("expected a unit after the value, found none: %s declares %s", e.Predicate, e.Want)
	default:
		return fmt.Sprintf(
			"expected the unit the predicate %s declares, found %s: %s declares %s",
			e.Predicate, e.Got, e.Predicate, e.Want,
		)
	}
}

// MissingChildError is a claim written without one of the children every claim
// carries.
//
// The accuracy is never this. Leaving it out is the one escape hatch the rule
// keeps open, and a claim which takes it loads and is unrankable.
type MissingChildError struct {
	// Predicate is the predicate the claim was written under.
	Predicate string

	// Child is the tag of the child that was left out.
	Child string

	// Wants is what that child holds.
	Wants string
}

// Error implements the [error] interface.
func (e MissingChildError) Error() string {
	return fmt.Sprintf(
		"expected the %s of the %s claim, found none: every claim carries (%s %s)",
		e.Child, e.Predicate, e.Child, e.Wants,
	)
}

// UnknownClaimError is a claim id nothing in the model carries.
//
// A claim id is optional, so an id naming no claim is as often a claim which
// wrote none as a claim which is not there. Both readings are in the message,
// because the answer to the second is to write an id on the claim rather than to
// go looking for a claim which does not exist.
type UnknownClaimError struct {
	// ID is the id that was asked for.
	ID ID
}

// Error implements the [error] interface.
func (e UnknownClaimError) Error() string {
	return fmt.Sprintf(
		"expected a claim id something in this model carries, found %s, which names no claim: "+
			"a claim which anything references carries an (id ...) of its own",
		e.ID,
	)
}

// AlreadyDeprecatedError is a claim retracted a second time.
type AlreadyDeprecatedError struct {
	// ID is the claim.
	ID ID

	// SupersededBy is the claim which already stands in its place.
	SupersededBy ID
}

// Error implements the [error] interface.
func (e AlreadyDeprecatedError) Error() string {
	return fmt.Sprintf("%s was already deprecated, superseded by %s", e.ID, e.SupersededBy)
}

// MissingReplacementError is a deprecation which names nothing to stand in the
// retracted claim's place.
//
// Requiring one is the whole of what keeps `deprecated` from being a delete
// button ([0007](docs/decisions/0007-rank-is-closed.md)).
type MissingReplacementError struct {
	// ID is the claim that would have been retracted.
	ID ID
}

// Error implements the [error] interface.
func (e MissingReplacementError) Error() string {
	return fmt.Sprintf(
		"expected the claim which replaces %s, found none: a deprecated claim carries (superseded-by <claim-id>), "+
			"which is what keeps deprecated from being a delete",
		e.ID,
	)
}

// SelfSupersessionError is a claim named as its own replacement.
type SelfSupersessionError struct {
	// ID is the claim.
	ID ID
}

// Error implements the [error] interface.
func (e SelfSupersessionError) Error() string {
	return fmt.Sprintf(
		"expected the claim which replaces %s, found %s, which is the claim itself: "+
			"a claim replaced by itself is retracted with nothing standing in its place",
		e.ID, e.ID,
	)
}

// NothingToSupersedeError is a correction of a subject and predicate the model
// says nothing about.
//
// It is a refusal rather than an addition because the two are different
// intentions: correcting a value which is not there is a mistake about which
// thing is being corrected, and quietly adding the claim would leave the author
// believing something had been superseded.
type NothingToSupersedeError struct {
	// Subject is the thing the claim would have been about.
	Subject ID

	// Predicate is the predicate it would have been written under.
	Predicate string
}

// Error implements the [error] interface.
func (e NothingToSupersedeError) Error() string {
	return fmt.Sprintf(
		"expected a claim of %s to correct, found none written on %s: "+
			"a value nothing yet states is added rather than superseded",
		e.Predicate, e.Subject,
	)
}

// AmbiguousSupersessionError is a correction of a subject and predicate the
// model states more than once.
//
// Which of the competing claims is being corrected is not something to guess at,
// so the claims are named and the deprecation is left to be spelled out one at a
// time.
type AmbiguousSupersessionError struct {
	// Subject is the thing the claims are about.
	Subject ID

	// Predicate is the predicate they were written under.
	Predicate string

	// Competing names each live claim, by its id where it wrote one and by
	// where it was written where it did not.
	Competing []string
}

// Error implements the [error] interface.
func (e AmbiguousSupersessionError) Error() string {
	return fmt.Sprintf(
		"expected one claim of %s on %s to correct, found %s: %s; deprecate the one being corrected by its id",
		e.Predicate, e.Subject, count(len(e.Competing)), strings.Join(e.Competing, ", "),
	)
}

// ValueProblem says why text written where a value belongs is not one.
//
// It is a field rather than wording inside a message so that what to do about
// the value is something a caller and a test can act on.
type ValueProblem string

const (
	// ValueNotANumber is a component which is not a real number.
	ValueNotANumber ValueProblem = "not a number"

	// ValueWrongCount is a value written with the wrong number of components
	// for its shape.
	ValueWrongCount ValueProblem = "wrong number of components"

	// ValueNoShape is a value under a predicate whose declared shape could not
	// be read, which leaves nothing to read the value as.
	ValueNoShape ValueProblem = "no declared shape"
)

// MalformedValueError reports text written where a value belongs which is not
// one of the shape the predicate declares.
type MalformedValueError struct {
	// Written is the text as it was given.
	Written string

	// Shape is the shape it was being read as, and is empty where the predicate
	// declared none.
	Shape Shape

	// Reason is why it could not be read.
	Reason ValueProblem
}

// Error implements the [error] interface.
func (e MalformedValueError) Error() string {
	if e.Shape == "" {
		return fmt.Sprintf("expected a value, found %s: the predicate declares no shape to read it as", strconv.Quote(e.Written))
	}
	return fmt.Sprintf(
		"expected a %s value, found %s: %s, and %s",
		e.Shape, strconv.Quote(e.Written), e.Reason, spellShapeWritten(e.Shape),
	)
}

// TermProblem says why text written where an accuracy term belongs is not one.
type TermProblem string

const (
	// TermUnknownKind is a term which is neither independent nor systematic.
	TermUnknownKind TermProblem = "unknown kind"

	// TermWrongCount is a term written with the wrong number of parts for its
	// kind.
	TermWrongCount TermProblem = "wrong number of parts"

	// TermNotANumber is a magnitude which is not a real number.
	TermNotANumber TermProblem = "magnitude is not a number"
)

// MalformedTermError reports text written where an accuracy term belongs which
// is not one.
type MalformedTermError struct {
	// Written is the text as it was given.
	Written string

	// Reason is why it could not be read.
	Reason TermProblem
}

// Error implements the [error] interface.
func (e MalformedTermError) Error() string {
	return fmt.Sprintf(
		"expected an accuracy term, found %s: %s, and a term is written as "+
			`"independent <magnitude> <unit>" or "systematic <magnitude> <unit> <term-id>"`,
		strconv.Quote(e.Written), e.Reason,
	)
}

// ScalarValue is a real number in a unit, which is the value of a claim under a
// predicate declaring [ShapeScalar].
//
// The four constructors here are how a value is built by something authoring
// one, as against read out of a file. A [Value] carries no exported fields
// because a value read from a file is what the file said and nothing else is
// free to say otherwise; an authored one has no span, which is the only
// difference between the two.
func ScalarValue(number float64, unit Unit) Value {
	return Value{shape: ShapeScalar, number: number, unit: unit}
}

// CoordinateValue is an ordered set of components in a unit, which is the value
// of a claim under a predicate declaring [ShapeCoordinate].
//
// The order is significant and is never sorted: the components are the axes of
// the frame the value is expressed in, in the order that frame gives them.
func CoordinateValue(components []float64, unit Unit) Value {
	return Value{shape: ShapeCoordinate, components: slices.Clone(components), unit: unit}
}

// TextValue is a string, which is the value of a claim under a predicate
// declaring [ShapeText]. A text value carries no unit token.
func TextValue(text string) Value {
	return Value{shape: ShapeText, text: text}
}

// TransformValue is a rigid transform, which is the value of a claim under a
// predicate declaring [ShapeTransform]. A transform carries no unit token.
func TransformValue(transform Transform) Value {
	return Value{shape: ShapeTransform, transform: transform}
}

// transformComponents is how many reals a transform is written with on a
// command line: the three of the translation, the nine of the rotation, and the
// one scale, in that order.
const transformComponents = 3 + 9 + 1

// ParseValue reads text written where a value belongs, as the shape the
// predicate declares.
//
// It is here rather than in a command for the reason [ParseDate] is: the one
// spelling of a value is the engine's, so a value handed in by a caller
// authoring a change and a value written in a file are held to the same rule and
// refused in the same words. What differs is only the punctuation the two
// mediums allow — a coordinate is parenthesised in a file and is a run of
// numbers here — and unit tokens, which a file writes after the value and which
// arrive here as an argument of their own.
//
// A scalar is one real number. A coordinate is its components in order,
// separated by whitespace. Text is taken exactly as it was written, including
// an empty string, which is a text value a claim may legally hold. A transform
// is its thirteen reals in the order the form writes them: the three of the
// translation, the nine of the rotation row by row, then the scale.
//
// Whether the unit is the one the predicate declares is [ClaimSpec.Check]'s
// question rather than this one's: the unit is carried through unread so that a
// value which is malformed and a value which is in the wrong unit are two
// answers rather than one.
func ParseValue(written string, unit Unit, declared Predicate) (Value, error) {
	switch declared.Shape {
	case ShapeText:
		return TextValue(written), nil

	case ShapeScalar:
		numbers, err := parseReals(written, ShapeScalar, 1)
		if err != nil {
			return Value{}, err
		}
		return ScalarValue(numbers[0], unit), nil

	case ShapeCoordinate:
		numbers, err := parseReals(written, ShapeCoordinate, declared.Dimension)
		if err != nil {
			return Value{}, err
		}
		return CoordinateValue(numbers, unit), nil

	case ShapeTransform:
		numbers, err := parseReals(written, ShapeTransform, transformComponents)
		if err != nil {
			return Value{}, err
		}

		transform := Transform{Scale: numbers[transformComponents-1]}
		copy(transform.Translation[:], numbers[0:3])
		copy(transform.Rotation[:], numbers[3:12])

		return TransformValue(transform), nil
	}

	return Value{}, MalformedValueError{Written: written, Reason: ValueNoShape}
}

// parseReals reads the whitespace-separated real numbers of a value, requiring
// exactly want of them where want is positive and at least one otherwise.
//
// A dimension of zero is a predicate which declared none, and a coordinate under
// one is read as written: how many components it should have is a question the
// registry answers, and answering it with a guess here would refuse a value the
// registry permits.
func parseReals(written string, shape Shape, want int) ([]float64, error) {
	fields := strings.Fields(written)

	if (want > 0 && len(fields) != want) || len(fields) == 0 {
		return nil, MalformedValueError{Written: written, Shape: shape, Reason: ValueWrongCount}
	}

	numbers := make([]float64, 0, len(fields))
	for _, field := range fields {
		number, err := strconv.ParseFloat(field, 64)
		if err != nil {
			return nil, MalformedValueError{Written: written, Shape: shape, Reason: ValueNotANumber}
		}
		numbers = append(numbers, number)
	}

	return numbers, nil
}

// spellShapeWritten is how a value of this shape is written where one is being
// asked for, which is what somebody who wrote something else needs to read.
func spellShapeWritten(shape Shape) string {
	switch shape {
	case ShapeScalar:
		return "a scalar is one real number"
	case ShapeCoordinate:
		return "a coordinate is its components in order, separated by spaces"
	case ShapeTransform:
		return "a transform is thirteen reals: three of translation, nine of rotation, then the scale"
	case ShapeText:
		return "a text value is written as it stands"
	}
	return ""
}

// ParseAccuracyTerm reads one term of a claim's accuracy written as the form
// writes it, minus the parentheses: the kind, the magnitude, the unit, and — for
// a systematic term — the id the error is shared with.
//
// The magnitude is a standard uncertainty, one standard deviation, k = 1. There
// is no other storage convention, and a figure quoted at any other coverage is
// converted by whoever enters it
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)).
func ParseAccuracyTerm(written string) (AccuracyTerm, error) {
	fields := strings.Fields(written)
	if len(fields) == 0 {
		return AccuracyTerm{}, MalformedTermError{Written: written, Reason: TermWrongCount}
	}

	kind := TermKind(fields[0])
	if kind != TermIndependent && kind != TermSystematic {
		return AccuracyTerm{}, MalformedTermError{Written: written, Reason: TermUnknownKind}
	}

	parts := 3
	if kind == TermSystematic {
		parts = 4
	}
	if len(fields) != parts {
		return AccuracyTerm{}, MalformedTermError{Written: written, Reason: TermWrongCount}
	}

	magnitude, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return AccuracyTerm{}, MalformedTermError{Written: written, Reason: TermNotANumber}
	}

	term := AccuracyTerm{Kind: kind, Magnitude: magnitude, Unit: Unit(fields[2])}

	if kind == TermSystematic {
		source, err := ParseID(fields[3])
		if err != nil {
			return AccuracyTerm{}, err
		}
		term.Source = source
	}

	return term, nil
}

// ClaimSpec is a claim as it is being written: a value, and the evidence for it.
//
// It is the written form of a [Claim] rather than the read one, which is why the
// fields are exported and carry no spans: nothing here has been written anywhere
// yet. The rank is not among them, because a claim is never authored retracted —
// a retraction is [Tx.DeprecateClaim] on a claim which was believed and then
// corrected, and the record of that correction is the thing the model exists to
// keep.
type ClaimSpec struct {
	// ID is the claim's own id, and is empty for a claim which writes none.
	//
	// It is optional: an id is required of a claim something references and is
	// noise on the great majority, which nothing points at. [Tx.Supersede] mints
	// one rather than leaving it empty, because the claim it retracts is about
	// to name this one.
	ID ID

	// Subject is the id of the thing the claim is about, which is the form the
	// claim is written inside.
	Subject ID

	// Predicate is the predicate it is written under, which is also its tag. The
	// registry must declare it, and must declare it claim-bearing.
	Predicate string

	// Value is the value and the unit it is expressed in. Its shape and its unit
	// must be the ones the predicate declares.
	Value Value

	// Source is the string naming the evidence — a report, a drawing, a person,
	// an instrument log. It is required.
	Source string

	// Method is the id naming how the value was obtained. It is required, and
	// its namespace must be one the registry declares.
	Method ID

	// Accuracy are the terms of how well the value is known, and are empty for a
	// claim which says nothing about it. Such a claim loads and is unrankable:
	// it can never win resolution and is not given a default.
	Accuracy []AccuracyTerm

	// Date is the day the value was obtained. The zero time is written as the
	// day the change is made.
	Date time.Time
}

// Check reports the first thing about the claim the registry does not permit.
//
// The checks are ordered rather than independent, for the reason
// [NodeSpec.Check]'s are: they build on each other, and a value reported against
// the shape of a predicate nothing declares says nothing anybody can act on.
//
// [Tx.AddClaim] calls it, so a caller which only adds claims never has to. It is
// exported for the caller which has something to do with the axes first —
// reading a value out of a command line, which needs the declared shape before
// it can read anything at all.
func (spec ClaimSpec) Check(registry *Registry) error {
	if spec.Subject == "" {
		return ErrNoSubject
	}
	if spec.Predicate == "" {
		return ErrNoPredicate
	}

	declared, ok := registry.Predicate(spec.Predicate)
	if !ok {
		return UnknownAxisError{
			Axis:      string(SortPredicate),
			Value:     spec.Predicate,
			Permitted: registry.Names(SortPredicate),
		}
	}

	if !declared.ClaimBearing {
		return NotClaimBearingError{
			Predicate: spec.Predicate,
			Written:   spellPlainValue(spec.Predicate, declared),
		}
	}

	if spec.Value.Shape() != declared.Shape {
		return ValueShapeError{Predicate: spec.Predicate, Want: declared.Shape, Got: spec.Value.Shape()}
	}

	if components, ok := spec.Value.Coordinate(); ok && declared.Dimension > 0 && len(components) != declared.Dimension {
		return DimensionError{Predicate: spec.Predicate, Want: declared.Dimension, Got: len(components)}
	}

	if spec.Value.Unit() != declared.Unit {
		return UnitError{Predicate: spec.Predicate, Want: declared.Unit, Got: spec.Value.Unit()}
	}

	if spec.Source == "" {
		return MissingChildError{Predicate: spec.Predicate, Child: sourceChild, Wants: `"<evidence>"`}
	}
	if spec.Method == "" {
		return MissingChildError{Predicate: spec.Predicate, Child: methodChild, Wants: "<method-id>"}
	}

	for _, id := range []ID{spec.ID, spec.Method} {
		if err := declaredNamespace(registry, id); err != nil {
			return err
		}
	}

	for _, term := range spec.Accuracy {
		if err := declaredNamespace(registry, term.Source); err != nil {
			return err
		}
	}

	return nil
}

// declaredNamespace reports an id whose namespace the registry does not
// declare, and accepts the empty id, which is an id nobody wrote.
func declaredNamespace(registry *Registry, id ID) error {
	if id == "" || registry.Declares(SortNamespace, id.Namespace()) {
		return nil
	}

	return UnknownAxisError{
		Axis:      string(SortNamespace),
		Value:     id.Namespace(),
		Permitted: registry.Names(SortNamespace),
	}
}

// form is the claim as it will be written.
//
// The children are written in the order specification section 6.5 tables them,
// which decides nothing: canonical form sorts the children of every form, so a
// claim built here and a claim somebody typed print the same way.
func (spec ClaimSpec) form() *Node {
	children := make([]*Node, 0, 6)

	if spec.ID != "" {
		children = append(children, formNode(idChild, symbolNode(string(spec.ID))))
	}

	children = append(children,
		valueNode(spec.Value),
		formNode(sourceChild, stringNode(spec.Source)),
		formNode(methodChild, symbolNode(string(spec.Method))),
	)

	if len(spec.Accuracy) > 0 {
		terms := make([]*Node, 0, len(spec.Accuracy))
		for _, term := range spec.Accuracy {
			terms = append(terms, termNode(term))
		}
		children = append(children, formNode(accuracyChild, terms...))
	}

	children = append(children, formNode(dateChild, stringNode(spec.Date.Format(dateLayout))))

	return formNode(spec.Predicate, children...)
}

// valueNode is the `value` child of a claim, written in whichever of the four
// shapes the value has.
func valueNode(value Value) *Node {
	switch {
	case value.shape == ShapeScalar:
		return formNode(valueChild, withUnitNode(realNode(value.number), value.unit)...)

	case value.shape == ShapeCoordinate:
		components := make([]*Node, 0, len(value.components))
		for _, component := range value.components {
			components = append(components, realNode(component))
		}
		return formNode(valueChild, withUnitNode(relisted(nil, components), value.unit)...)

	case value.shape == ShapeText:
		return formNode(valueChild, stringNode(value.text))

	case value.shape == ShapeTransform:
		return formNode(valueChild, formNode(transformChild,
			formNode("translation", realNodes(value.transform.Translation[:])...),
			formNode("rotation", realNodes(value.transform.Rotation[:])...),
			formNode("scale", realNode(value.transform.Scale)),
		))
	}

	return formNode(valueChild)
}

// withUnitNode is a written value followed by its unit, and by nothing where the
// predicate declares none: there is no unitless token, so a non-dimensional
// value is written with no unit at all.
func withUnitNode(written *Node, unit Unit) []*Node {
	if unit == "" {
		return []*Node{written}
	}
	return []*Node{written, symbolNode(string(unit))}
}

// termNode is one term of an accuracy as it will be written.
func termNode(term AccuracyTerm) *Node {
	written := []*Node{realNode(term.Magnitude), symbolNode(string(term.Unit))}
	if term.Kind == TermSystematic {
		written = append(written, symbolNode(string(term.Source)))
	}

	return formNode(string(term.Kind), written...)
}

// realNode is a real number written on its own.
//
// It is a float whatever the value, because specification section 4.3 writes a
// magnitude with a fraction so that it reads back as a real: a whole number
// written as an integer is a count rather than a measurement, and the canonical
// printer is what adds the fraction back.
func realNode(value float64) *Node {
	return &Node{Datum: sexpr.Float{Value: value}}
}

// realNodes is a run of real numbers, in order.
func realNodes(values []float64) []*Node {
	out := make([]*Node, 0, len(values))
	for _, value := range values {
		out = append(out, realNode(value))
	}
	return out
}

// Live returns the claims written on subject under predicate which are still
// asserted, in written order.
//
// A deprecated claim is retracted rather than out-ranked: resolution never
// considers one, and it is nothing's competitor. So the claims which disagree
// about a subject and a predicate, the claim a correction supersedes, and the
// claims which would be left if one were retracted are all this same set.
func (c *Claims) Live(subject ID, predicate string) []*Claim {
	var out []*Claim

	for claim := range c.Under(subject, predicate) {
		if claim.Rank() != RankDeprecated {
			out = append(out, claim)
		}
	}

	return out
}

// NoticeKind is what a notice is about.
type NoticeKind string

const (
	// NoticeUnrankable is a claim written with no accuracy, which can never win
	// resolution.
	NoticeUnrankable NoticeKind = "unrankable"

	// NoticeConflict is a claim written on a subject and predicate the model
	// already states, which is a disagreement rather than a replacement.
	NoticeConflict NoticeKind = "conflict"

	// NoticeUnresolvable is a retraction which leaves a subject and predicate
	// with no live claim at all, so nothing resolves under it.
	NoticeUnresolvable NoticeKind = "unresolvable"
)

// Notice is something a change has to say about the model it produces, which is
// neither a refusal nor a diagnostic.
//
// It is not a [Diagnostic] because nothing is wrong with what anybody wrote: the
// files load, the change is permitted, and what is being reported is a
// consequence of it which the author is entitled to have wanted. A claim with no
// accuracy is a legitimate claim, a second claim about one thing is the most
// valuable thing in a model, and a retraction which leaves nothing behind is
// sometimes exactly the record that should be kept. What each of them is not is
// something to discover later.
//
// A notice is a value rather than a sentence so that a caller can branch on it.
// [Notice.Message] is the sentence, and is presentation.
type Notice struct {
	// Kind is what it is about.
	Kind NoticeKind

	// Subject is the thing the claim is about.
	Subject ID

	// Predicate is the predicate it was written under.
	Predicate string

	// Claim is the claim the notice is about, and is empty where that claim
	// wrote no id of its own.
	Claim ID

	// Competing are the claims already written on the same subject and
	// predicate, and are empty for a notice which is not about a disagreement.
	Competing []*Claim
}

// Message is the notice as a sentence, for a person reading a terminal.
func (n Notice) Message() string {
	switch n.Kind {
	case NoticeUnrankable:
		return fmt.Sprintf(
			"the %s of %s carries no accuracy, so it is unrankable: it can never win resolution, "+
				"and it is not given a default",
			n.Predicate, n.Subject,
		)

	case NoticeConflict:
		return fmt.Sprintf(
			"the %s of %s is now claimed by %d claims, which is a disagreement rather than a correction: "+
				"it competes with %s",
			n.Predicate, n.Subject, len(n.Competing)+1, join(namesOf(n.Competing), "and"),
		)

	case NoticeUnresolvable:
		return fmt.Sprintf(
			"nothing is left asserted about the %s of %s, so it has no resolvable value",
			n.Predicate, n.Subject,
		)
	}

	return ""
}

// namesOf names each claim for a person, by its id where it wrote one and by
// what it says about what where it did not.
func namesOf(claims []*Claim) []string {
	out := make([]string, 0, len(claims))
	for _, claim := range claims {
		out = append(out, claimName(claim))
	}
	return out
}

// AddClaim writes a new claim on the thing its subject names.
//
// Every axis is checked against the registry before anything is written: a
// predicate nothing declares, a predicate declared to take a plain value
// instead, a value of the wrong shape, a coordinate of the wrong dimension, a
// unit other than the declared one and an id whose namespace nobody declared are
// each a refusal the author can act on without reading the registry.
//
// The claim is written inside the form of its subject, which is where a claim
// lives: a claim's tag is its predicate, and there is no other way to attach a
// value to a thing.
//
// Nothing here edits a value. A claim which is wrong is corrected by
// [Tx.Supersede], which writes the new value and retracts the old one in the
// same change, so that the record of what was believed and why it changed
// survives ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// The notices are what the change has to say about the model it produced: that
// the claim is unrankable, and that it disagrees with what was already written.
// Neither refuses it.
func (tx *Tx) AddClaim(spec ClaimSpec) (ID, []Notice, error) {
	if tx.finished {
		return "", nil, ErrFinished
	}

	// The competing claims are read before the change is applied, because after
	// it the claim being written is one of them.
	competing := tx.graph.Claims().Live(spec.Subject, spec.Predicate)

	if err := tx.record(&spec); err != nil {
		return "", nil, err
	}

	var notices []Notice

	if len(spec.Accuracy) == 0 {
		notices = append(notices, Notice{
			Kind:      NoticeUnrankable,
			Subject:   spec.Subject,
			Predicate: spec.Predicate,
			Claim:     spec.ID,
		})
	}

	if len(competing) > 0 {
		notices = append(notices, Notice{
			Kind:      NoticeConflict,
			Subject:   spec.Subject,
			Predicate: spec.Predicate,
			Claim:     spec.ID,
			Competing: competing,
		})
	}

	return spec.ID, notices, nil
}

// record checks a claim and writes it into the form of its subject, which is the
// mutation both [Tx.AddClaim] and [Tx.Supersede] are built from.
//
// The spec is taken by pointer because the date it is written with is part of
// what happened: a claim dated by the day the change was made carries that day,
// and a caller which then reports the claim reports the date it has.
func (tx *Tx) record(spec *ClaimSpec) error {
	if err := spec.Check(tx.graph.Registry()); err != nil {
		return err
	}

	if err := tx.subject(spec.Subject); err != nil {
		return err
	}

	if spec.ID != "" {
		if err := tx.free(spec.ID); err != nil {
			return err
		}

		// And the claims this same change has already written, which the graph
		// does not hold.
		if tx.claimWritten(spec.ID) {
			return TakenIDError{ID: spec.ID, What: "a claim this change wrote"}
		}
	}

	if spec.Date.IsZero() {
		spec.Date = time.Now().UTC()
	}

	form, ok := tx.Form(spec.Subject)
	if !ok {
		return UnknownFormError{}
	}

	return tx.Replace(form, relisted(form, append(slices.Clone(form.Children), spec.form())))
}

// claimWritten reports whether any claim in the files the transaction holds was
// written with this id, including a claim this same change has just written.
//
// [Tx.Graph] cannot answer it. The graph is the model as the transaction found
// it, so a claim added by an earlier mutation of the same transaction is not in
// it — and neither [Tx.Form] nor [Tx.free] closes the gap: the first answers only
// for forms written with an id as their first argument, which every entity is and
// no claim is, and the second is asked of the graph. A claim carries its id as an
// `(id ...)` child instead, which is what this walks.
//
// Both questions the transaction asks about a claim id need it. Whether an id is
// free has to count the one an earlier mutation took, or two claims of one change
// are written under one name; and whether a claim exists has to count the one an
// earlier mutation wrote, or a change which adds a claim and then retracts
// another in its favour is refused for naming a claim it is itself writing.
func (tx *Tx) claimWritten(id ID) bool {
	if tx == nil || id == "" {
		return false
	}

	for _, key := range tx.order {
		for _, node := range tx.files[key].file.Nodes {
			// A claim is a child of the form it is written inside, and the
			// `id` child belongs to no other form of the format, so the walk
			// stops one level down rather than searching the whole tree.
			for _, child := range node.Children {
				arg, ok := argumentOf(child, idChild)
				if !ok {
					continue
				}

				if symbol, ok := arg.Datum.(sexpr.Symbol); ok && ID(symbol.Value) == id {
					return true
				}
			}
		}
	}

	return false
}

// subject reports whether the model holds something a claim can be written on.
//
// A frame answers as well as an entity: a frame is both a registry entry and a
// node, and it carries claims because the relationship between two frames is a
// measurement rather than a configuration constant.
func (tx *Tx) subject(id ID) error {
	if _, ok := tx.graph.Entity(id); ok {
		return nil
	}
	if _, ok := tx.graph.Registry().Frame(id); ok {
		return nil
	}

	nearest, _ := tx.graph.Nearest(id)
	return UnknownEntityError{ID: id, Nearest: nearest}
}

// DeprecateClaim retracts the claim id names in favour of the claim
// supersededBy names.
//
// A replacement is required and is checked. `deprecated` with nothing standing
// in the retracted claim's place is what would make it a delete button, and a
// claim which was believed and then corrected is the record of why the number
// changed ([0007](docs/decisions/0007-rank-is-closed.md)).
//
// The claim itself is not touched beyond its rank: its value, its evidence, its
// method and its date stay exactly as they were written, which is the whole
// point of retracting rather than editing.
//
// A retraction which leaves the subject and predicate with no live claim is
// permitted and reported. Nothing then resolves under that predicate, which is a
// state a model may legitimately be in — an area somebody withdrew and has not
// yet re-measured is unknown rather than wrong — and is not something to find
// out about later.
func (tx *Tx) DeprecateClaim(id, supersededBy ID) ([]Notice, error) {
	if tx.finished {
		return nil, ErrFinished
	}

	claim, ok := tx.graph.Claims().Claim(id)
	if !ok {
		return nil, UnknownClaimError{ID: id}
	}

	if claim.Rank() == RankDeprecated {
		replacement, _ := claim.SupersededBy()
		return nil, AlreadyDeprecatedError{ID: id, SupersededBy: replacement}
	}

	switch {
	case supersededBy == "":
		return nil, MissingReplacementError{ID: id}
	case supersededBy == id:
		return nil, SelfSupersessionError{ID: id}
	}

	// The claims this same change has written count as claims. A change which
	// adds the corrected value and then retracts the old one in its favour is
	// exactly what a supersession is, and refusing it for naming a claim the
	// change is itself writing would make that impossible to spell out in two
	// steps.
	_, held := tx.graph.Claims().Claim(supersededBy)
	if !held && !tx.claimWritten(supersededBy) {
		return nil, UnknownClaimError{ID: supersededBy}
	}

	if err := tx.retract(claim, supersededBy); err != nil {
		return nil, err
	}

	return leftAsserted(tx.graph.Claims(), claim), nil
}

// retract writes the rank and the replacement onto a claim which is already in
// the model.
//
// The replacement is not looked up here, because [Tx.Supersede] retracts a claim
// in favour of one this same change has just written and which the model
// therefore does not yet hold. A replacement which names no claim at all is
// refused at [Tx.Commit] by the pass which resolves every reference to a claim,
// in the one wording that failure has.
func (tx *Tx) retract(claim *Claim, supersededBy ID) error {
	form, ok := tx.Form(claim.Subject())
	if !ok {
		return UnknownFormError{Span: claim.Span()}
	}

	rewritten, ok := deprecated(form, claim.Span(), supersededBy)
	if !ok {
		return UnknownFormError{Span: claim.Span()}
	}

	return tx.Replace(form, rewritten)
}

// leftAsserted reports that retracting a claim leaves its subject and predicate
// with nothing asserted about them, where it does.
//
// It is asked of the model as the transaction found it, and the claim being
// retracted is the one thing about to leave it: a pair whose only other live
// claim is this one is a pair which will have none. A supersession never reaches
// here, because it writes the replacement under the same pair in the same
// change.
func leftAsserted(claims *Claims, claim *Claim) []Notice {
	for _, live := range claims.Live(claim.Subject(), claim.Predicate()) {
		if live != claim {
			return nil
		}
	}

	notice := Notice{
		Kind:      NoticeUnresolvable,
		Subject:   claim.Subject(),
		Predicate: claim.Predicate(),
	}
	if id, ok := claim.ID(); ok {
		notice.Claim = id
	}

	return []Notice{notice}
}

// deprecated is form with the claim written at span retracted in favour of
// supersededBy, and whether the form held such a claim at all.
//
// The claim is found by where it was written rather than by its id, because the
// great majority of claims write none — and a claim which is being retracted is
// exactly one which somebody wants to name without having named it in advance.
func deprecated(form *Node, at Span, supersededBy ID) (*Node, bool) {
	for i, child := range form.Children {
		if child.Span != at {
			continue
		}

		retracted := relisted(child, append(slices.Clone(child.Children),
			formNode(rankChild, symbolNode(string(RankDeprecated))),
			formNode(supersededByChild, symbolNode(string(supersededBy))),
		))

		children := slices.Clone(form.Children)
		children[i] = retracted

		return relisted(form, children), true
	}

	return form, false
}

// Supersede corrects a value: it writes the new claim and retracts the one it
// replaces, in one change which lands completely or not at all.
//
// The claim being corrected is the one live claim written on the spec's subject
// under its predicate. It is named that way rather than by its id because the
// great majority of claims write none — an id is required only of a claim
// something references — and the claim being corrected is exactly one which is
// about to be referenced for the first time. A pair nothing states is refused
// rather than added to, and a pair stated more than once is refused naming the
// competing claims, because which of them is being corrected is not something to
// guess at.
//
// The new claim is given an id, minted where the spec carries none, because the
// claim it replaces names it. That is the whole of when a claim id is generated:
// a claim nothing points at needs no name, and one something points at is found
// by the name it wrote. The format is stated and stable — see [Tx.MintClaimID].
//
// Correction is supersession and never an edit. The old claim keeps its value,
// its evidence, its method and its date exactly as they were written, and what
// changes is that it now says it was retracted and by what
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
func (tx *Tx) Supersede(spec ClaimSpec) (ID, []Notice, error) {
	if tx.finished {
		return "", nil, ErrFinished
	}

	if err := spec.Check(tx.graph.Registry()); err != nil {
		return "", nil, err
	}

	live := tx.graph.Claims().Live(spec.Subject, spec.Predicate)

	switch len(live) {
	case 0:
		return "", nil, NothingToSupersedeError{Subject: spec.Subject, Predicate: spec.Predicate}
	case 1:
	default:
		return "", nil, AmbiguousSupersessionError{
			Subject:   spec.Subject,
			Predicate: spec.Predicate,
			Competing: namesOf(live),
		}
	}

	replaced := live[0]

	if spec.ID == "" {
		minted, err := tx.MintClaimID(spec.Subject, spec.Predicate)
		if err != nil {
			return "", nil, err
		}
		spec.ID = minted
	}

	if err := tx.record(&spec); err != nil {
		return "", nil, err
	}

	if err := tx.retract(replaced, spec.ID); err != nil {
		return "", nil, err
	}

	var notices []Notice
	if len(spec.Accuracy) == 0 {
		notices = append(notices, Notice{
			Kind:      NoticeUnrankable,
			Subject:   spec.Subject,
			Predicate: spec.Predicate,
			Claim:     spec.ID,
		})
	}

	return spec.ID, notices, nil
}

// MintClaimID is the id a claim of this subject and predicate is given when
// something comes to reference it.
//
// The format is `<subject>:<predicate>:<n>`: the id of the thing the claim is
// about, the predicate it is written under, and the lowest ordinal from one
// which nothing in the model already holds. Every part of it is already
// established vocabulary — the subject's namespace is declared, and a local part
// may hold further colons — so the id which comes back is a well-formed id and a
// well-formed symbol, writable in a file without quoting.
//
// It is stable in the sense that matters: an id, once written, is what it is
// forever, and minting one again for the same pair yields the next ordinal
// rather than the one already taken. Nothing is ever inferred back out of it. It
// is a name, not a schema — the same rule every other id in this model is held to
// ([0002](docs/decisions/0002-immutable-id-mutable-label.md)) — so a caller
// reading `site:S-101:area:2` learns nothing from it that the claim does not say
// for itself.
func (tx *Tx) MintClaimID(subject ID, predicate string) (ID, error) {
	for ordinal := 1; ; ordinal++ {
		minted, err := ParseID(fmt.Sprintf("%s%s%s%s%d", subject, idSeparator, predicate, idSeparator, ordinal))
		if err != nil {
			return "", err
		}

		// The transaction is asked as well as the model, because what this same
		// change has already written is not in the model it loaded — and two
		// corrections of one pair in one transaction would otherwise mint the
		// same id twice. Both are asked: a claim carries its id as a child, and
		// an entity carries it as the first argument of its form.
		if tx.claimWritten(minted) {
			continue
		}
		if _, written := tx.Form(minted); written {
			continue
		}

		if tx.free(minted) == nil {
			return minted, nil
		}
	}
}
