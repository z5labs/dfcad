// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// BatchVersion is the version of the operation file format this engine reads.
//
// A file which says nothing about its version is read as one written against
// this one, which is the only version there is; a file which names another is
// refused rather than guessed at. The rule the number carries is the one the
// machine output contract carries
// ([0014](docs/decisions/0014-the-machine-output-contract-is-part-of-the-interface.md)):
// a member may be added at any time, and a member is never removed, renamed or
// given a different meaning without this number changing.
const BatchVersion = 1

// opMember is the member of an operation object which says which operation it
// is, and so which shape the rest of the object has.
const opMember = "op"

// The names the operation file writes the operations under, which are the names
// of the commands making the same change on its own.
//
// They are the command names rather than names of their own so that an author
// who knows the interface knows the file, and so that a batch and the commands
// it replaces cannot come to spell one change two ways.
const (
	addNodeOperation        = "add-node"
	addVertexOperation      = "add-vertex"
	addEdgeOperation        = "add-edge"
	addLoopOperation        = "add-loop"
	scaffoldLoopOperation   = "scaffold-loop"
	classifyTypeOperation   = "classify-type"
	setLabelOperation       = "set-label"
	retireOperation         = "retire"
	addClaimOperation       = "add-claim"
	supersedeOperation      = "supersede"
	deprecateClaimOperation = "deprecate-claim"
)

// ErrNoOperations is an operation file which carries no operation at all.
//
// It is refused rather than applied as a change which does nothing: a file with
// an empty list is one somebody stopped writing, and a run which reported it as
// a successful change of nothing would say the batch landed.
var ErrNoOperations = errors.New("a batch is one or more operations, in the order they are applied")

// ErrExtraInput is an operation file with something after its object.
//
// One file is one batch. Two objects in one file is either a batch somebody
// concatenated or a stream of them, and applying the first while silently
// dropping the rest is the reading which loses work.
var ErrExtraInput = errors.New("a batch is one JSON object and nothing after it")

// ErrNoType is a classification naming no type to write itself on.
var ErrNoType = errors.New("a classification names the declared type it is written on")

// ErrNoValue is a claim written with nothing claimed.
//
// The empty value is not this: a text claim of "" is a value a claim may
// legally hold, and leaving the member out says nothing at all.
var ErrNoValue = errors.New("a claim is written with the value being claimed")

// ErrNoSupersedingClaim is a deprecation naming no claim to stand in the
// retracted one's place.
var ErrNoSupersedingClaim = errors.New("a deprecation names the claim which stands in the retracted one's place")

// UnknownOperationError is an `op` naming no operation.
type UnknownOperationError struct {
	// Operation is the name which was written.
	Operation string

	// Known is every operation there is, in the order the format documents
	// them.
	Known []string
}

// Error implements [error].
func (e UnknownOperationError) Error() string {
	return fmt.Sprintf("unknown operation %q: want one of %s", e.Operation, strings.Join(e.Known, ", "))
}

// UnknownBatchVersionError is an operation file written against a version of
// the format this engine does not read.
type UnknownBatchVersionError struct {
	// Version is what the file said.
	Version int

	// Known is the version this engine reads.
	Known int
}

// Error implements [error].
func (e UnknownBatchVersionError) Error() string {
	return fmt.Sprintf("operation file version %d: this engine reads version %d", e.Version, e.Known)
}

// OperationError is a problem with one operation of a batch.
//
// The index is what makes it actionable. A batch is a list an author generated,
// and a refusal which says only what is wrong leaves them to find which of fifty
// operations it is wrong about.
type OperationError struct {
	// Index is the operation's place in the batch, counted from one as a person
	// counts a list.
	Index int

	// Operation is the name it was written under, and is empty for one whose
	// name could not be read.
	Operation string

	// Err is what is wrong with it.
	Err error
}

// Error implements [error].
func (e OperationError) Error() string {
	if e.Operation == "" {
		return fmt.Sprintf("operation %d: %v", e.Index, e.Err)
	}
	return fmt.Sprintf("operation %d, %s: %v", e.Index, e.Operation, e.Err)
}

// Unwrap returns what is wrong with the operation, so that [errors.Is] and
// [errors.As] reach it.
func (e OperationError) Unwrap() error {
	return e.Err
}

// BatchError is everything wrong with one operation file.
//
// It carries every problem rather than the first, which is the rule the whole
// engine reports input by ([0016](docs/decisions/0016-writes-are-all-or-nothing.md)):
// an author fixing a refused batch should not have to resubmit it once per
// mistake.
type BatchError struct {
	// Errs is what was found, in the order the operations were written. Each
	// one about an operation is an [OperationError] naming which.
	Errs []error
}

// Error implements [error].
func (e BatchError) Error() string {
	spelled := make([]string, 0, len(e.Errs))
	for _, err := range e.Errs {
		spelled = append(spelled, err.Error())
	}
	return strings.Join(spelled, "; ")
}

// Unwrap returns every problem, so that [errors.Is] and [errors.As] reach each
// of them.
func (e BatchError) Unwrap() []error {
	return e.Errs
}

// Batch is an ordered list of authoring operations applied as one change.
//
// It is what an operation file decodes to. Applying it is [Tx.Apply], which is
// one transaction however many operations it holds: the model is read once, the
// operations are applied to it in order, and the model they produce together is
// interpreted once at [Tx.Commit]. A batch of fifty edits therefore costs one
// load rather than fifty, which is the reason it exists
// ([0015](docs/decisions/0015-the-cli-is-the-primary-write-path.md)).
type Batch struct {
	// Version is the version of the format the file was written against, which
	// is [BatchVersion] for a file which did not say.
	Version int

	// Operations are the operations, in the order they are applied. The order
	// is the data: an operation may name what an earlier one wrote.
	Operations []Operation
}

// Operation is one authoring operation of a [Batch].
//
// The set is closed and is exactly the commands which change the model, so that
// an author who knows the interface knows the file. It is an interface with an
// unexported method rather than one struct carrying every axis of every
// operation, because a member written for an operation which does not read it
// is a mistake this way and a silently ignored member the other.
type Operation interface {
	// Name is what the operation file writes it under, which is the name of the
	// command making the same change on its own.
	Name() string

	// check reports what is wrong with the operation on its own terms, before
	// any model is read: a member the operation requires and which was not
	// written. It is what lets every such mistake in a file come back at once.
	check() error

	// apply makes the change, filling in what it did beyond its effects.
	apply(tx *Tx, out *Applied) error
}

// made is every operation there is, by the name it is written under, in the
// order the format documents them.
//
// It is a table rather than a switch for the reason the subcommands of the
// command line interface are one: everything which has to be true of every
// operation — that its name is known, that it is decoded strictly, that a name
// which is not here is refused naming the ones which are — is done by walking
// it.
var made = []struct {
	name string
	make func() Operation
}{
	{addNodeOperation, func() Operation { return &AddNodeOperation{} }},
	{addVertexOperation, func() Operation { return &AddVertexOperation{} }},
	{addEdgeOperation, func() Operation { return &AddEdgeOperation{} }},
	{addLoopOperation, func() Operation { return &AddLoopOperation{} }},
	{scaffoldLoopOperation, func() Operation { return &ScaffoldLoopOperation{} }},
	{classifyTypeOperation, func() Operation { return &ClassifyTypeOperation{} }},
	{setLabelOperation, func() Operation { return &SetLabelOperation{} }},
	{retireOperation, func() Operation { return &RetireOperation{} }},
	{addClaimOperation, func() Operation { return &AddClaimOperation{} }},
	{supersedeOperation, func() Operation { return &SupersedeOperation{} }},
	{deprecateClaimOperation, func() Operation { return &DeprecateClaimOperation{} }},
}

// Operations is the name of every operation an operation file may carry, in the
// order the format documents them.
//
// It is exported because it is the answer to "what can a batch say", which is a
// question a caller building an operation file asks before it writes one.
func Operations() []string {
	out := make([]string, 0, len(made))
	for _, operation := range made {
		out = append(out, operation.name)
	}
	return out
}

// ParseBatch reads an operation file.
//
// The file is one JSON object: an optional `version`, which must be
// [BatchVersion], and `operations`, the operations in the order they are
// applied. Every operation is an object whose `op` names which operation it is
// and whose other members are that operation's axes, spelled as the flags of
// the command which makes the same change on its own.
//
// **Nothing is guessed.** A member no operation of that name reads is refused
// rather than ignored, an operation nothing declares is refused naming the ones
// there are, and a member an operation requires and which was not written is
// refused before any model is read. A batch which a caller generated is exactly
// the input where a silently dropped member is discovered a week later.
//
// **Every problem comes back at once**, as a [BatchError] holding one
// [OperationError] per operation which has one, in the order they were written.
// An author fixing a refused batch should not have to resubmit it once per
// mistake ([0016](docs/decisions/0016-writes-are-all-or-nothing.md)).
//
// What cannot be answered here is what the registry decides: which shape a
// value takes, which predicates and types are declared, which ids are free.
// Those are read when the batch is applied to a model, by [Tx.Apply], and are
// reported the same way — naming the operation they are about.
func ParseBatch(r io.Reader) (Batch, error) {
	var file struct {
		Version    *int              `json:"version"`
		Operations []json.RawMessage `json:"operations"`
	}

	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&file); err != nil {
		return Batch{}, err
	}

	// One file is one batch, and what follows the object is not part of it.
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Batch{}, ErrExtraInput
	}

	batch := Batch{Version: BatchVersion}
	if file.Version != nil {
		batch.Version = *file.Version
	}
	if batch.Version != BatchVersion {
		return Batch{}, UnknownBatchVersionError{Version: batch.Version, Known: BatchVersion}
	}

	if len(file.Operations) == 0 {
		return Batch{}, ErrNoOperations
	}

	var problems []error

	for at, raw := range file.Operations {
		operation, err := readOperation(raw)
		if err != nil {
			problems = append(problems, OperationError{Index: at + 1, Operation: whichOperation(raw), Err: err})
			continue
		}

		if err := operation.check(); err != nil {
			problems = append(problems, OperationError{Index: at + 1, Operation: operation.Name(), Err: err})
			continue
		}

		batch.Operations = append(batch.Operations, operation)
	}

	if len(problems) > 0 {
		return Batch{}, BatchError{Errs: problems}
	}

	return batch, nil
}

// readOperation reads one operation object: which operation it is, and then its
// axes against that operation and nothing else.
//
// The `op` member is taken out before the axes are read rather than being a
// field of every operation, so that the member which chose the shape is not
// reported by a strict decode as a member that shape does not know.
func readOperation(raw json.RawMessage) (Operation, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, err
	}

	name, err := operationName(members)
	if err != nil {
		return nil, err
	}

	operation, ok := newOperation(name)
	if !ok {
		return nil, UnknownOperationError{Operation: name, Known: Operations()}
	}

	delete(members, opMember)

	axes, err := json.Marshal(members)
	if err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(bytes.NewReader(axes))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(operation); err != nil {
		return nil, err
	}

	return operation, nil
}

// operationName is the `op` of one operation object.
func operationName(members map[string]json.RawMessage) (string, error) {
	written, ok := members[opMember]
	if !ok {
		return "", UnknownOperationError{Known: Operations()}
	}

	var name string
	if err := json.Unmarshal(written, &name); err != nil {
		return "", err
	}

	return name, nil
}

// whichOperation is the operation an object says it is, for a refusal which could not
// read the rest of it. It is empty where even that could not be read, which is
// the honest answer: the refusal then names the index alone.
func whichOperation(raw json.RawMessage) string {
	var object struct {
		Op string `json:"op"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return ""
	}
	return object.Op
}

// newOperation is an empty operation of that name, and whether there is one.
func newOperation(name string) (Operation, bool) {
	for _, operation := range made {
		if operation.name == name {
			return operation.make(), true
		}
	}
	return nil, false
}

// Applied is what one operation of a batch did.
//
// It is reported per operation rather than only in total because a batch is
// authored as a list and read back as one: an author who wrote fifty operations
// and is told that eleven nodes were created has to work out which eleven.
type Applied struct {
	// Index is the operation's place in the batch, counted from one as a person
	// counts a list.
	Index int

	// Operation is the name it was written under.
	Operation string

	// Effects are what it did to the model, in the order the mutations were
	// applied. They are the same effects [Commit] reports against the files
	// they landed in, grouped by the operation which caused them instead.
	Effects []Effect

	// Claim is the id of the claim it wrote, and is empty for an operation
	// which wrote none or wrote one with no id of its own.
	Claim ID

	// Replaced is the id of the claim it retracted, and is empty for an
	// operation which retracted none.
	Replaced ID

	// Snaps is every corner a scaffold landed on a vertex the model already
	// held, and is empty for every other operation. A reuse is the one thing
	// about a scaffold which is surprising when it happens and worse when it
	// does not, so it is reported per operation rather than left to be inferred
	// from which nodes appeared.
	Snaps []Snap

	// Tolerance is the declaration those snaps were judged against, and is the
	// zero tolerance for an operation which judged nothing. It travels with them
	// because the answer depends on it.
	Tolerance Tolerance

	// Notices are what it had to say about the model it produced — that a claim
	// is unrankable, that it now competes with another — in the order the
	// engine reported them. None of them refuses anything.
	Notices []Notice
}

// Apply performs every operation of a batch, in order, as part of this change.
//
// **It is one change.** The model is read once, at [Begin]; every operation is
// applied to the trees the transaction holds; and the model they produce
// together is interpreted once, at [Tx.Commit], which refuses one which would
// not load. No operation is validated against the model as it stands halfway
// through the batch, so an end state which loads is accepted however its
// intermediate states would have read — a node and the claims which reference
// it are one statement, and requiring each half of it to stand alone would make
// the pair unwritable.
//
// **An operation may name what an earlier one wrote.** The ids this same change
// has already written count as taken, resolve as references and answer as the
// subject of a claim, which is what makes an edge between two vertices of the
// same batch, or a claim on a node of it, the ordinary case rather than a
// special one.
//
// **Any failure refuses the whole batch.** The first operation the model
// refuses comes back as an [OperationError] naming which it was and why, and
// the transaction is left with nothing to commit: a caller which gets one closes
// it and reissues the corrected file ([0016](docs/decisions/0016-writes-are-all-or-nothing.md)).
// The operations after it are not attempted, because an operation may depend on
// the one which failed and reporting the failures it caused beside the failure
// itself buries the one which is real.
//
// Nothing reaches the disk here whether it succeeded or failed. [Tx.Commit] is
// what writes, and [Tx.DryRun] is what does everything except the writing.
func (tx *Tx) Apply(batch Batch) ([]Applied, error) {
	if tx.finished {
		return nil, ErrFinished
	}

	applied := make([]Applied, 0, len(batch.Operations))

	for at, operation := range batch.Operations {
		out := Applied{Index: at + 1, Operation: operation.Name()}

		mark := tx.marked()

		if err := operation.apply(tx, &out); err != nil {
			return nil, OperationError{Index: out.Index, Operation: out.Operation, Err: err}
		}

		out.Effects = tx.effectsSince(mark)

		applied = append(applied, out)
	}

	return applied, nil
}

// marked is how many effects each file of the transaction has recorded.
//
// What one operation of a batch did is the effects which appeared while it ran,
// and the transaction records them against the file they landed in rather than
// against whatever caused them. Reading the count before and the tail after is
// what tells the two apart without the mutations having to know they are part of
// a batch.
func (tx *Tx) marked() map[string]int {
	mark := make(map[string]int, len(tx.files))
	for path, file := range tx.files {
		mark[path] = len(file.effects)
	}
	return mark
}

// effectsSince is every effect recorded after the mark, file by file in the
// order the transaction holds them.
func (tx *Tx) effectsSince(mark map[string]int) []Effect {
	var out []Effect

	for _, path := range tx.order {
		file := tx.files[path]
		if at := mark[path]; at < len(file.effects) {
			out = append(out, file.effects[at:]...)
		}
	}

	return out
}

// ClaimAxes is a claim as an operation file writes it: the value and the
// evidence for it, each spelled exactly as the flag of the same name spells it
// on a command line.
//
// The spellings are the command line's rather than JSON's own — a value is the
// string `18.0` and not the number 18.0, an accuracy term is `independent 0.05
// m2` — because which of the four shapes a value takes is registry data
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)), and
// reading `1.0 2.0 3.0` as a scalar or as a coordinate is a question only the
// declaration answers. One spelling means a claim written on a command line and
// a claim written in an operation file are read by the same code and refused in
// the same words.
type ClaimAxes struct {
	// Value is what is claimed, in the shape the predicate declares. It is a
	// pointer because the empty string is a value rather than an absence: a text
	// claim of "" is a claim, and a member nobody wrote is not.
	Value *string `json:"value,omitempty"`

	// Unit is the unit it is expressed in, which must be the one the predicate
	// declares. A non-dimensional predicate takes none, and there is no unitless
	// token.
	Unit string `json:"unit,omitempty"`

	// Source is the evidence: a report, a drawing, a person, an instrument log.
	Source string `json:"source,omitempty"`

	// Method is an id naming how the value was obtained.
	Method string `json:"method,omitempty"`

	// Accuracy are the terms of how well it is known, each written as the file
	// writes one without its parentheses: `independent <magnitude> <unit>`, or
	// `systematic <magnitude> <unit> <term-id>`.
	Accuracy []string `json:"accuracy,omitempty"`

	// Date is the day the value was obtained, as YYYY-MM-DD. The day the change
	// is made where it is empty.
	Date string `json:"date,omitempty"`

	// ID is the claim's own id, and is empty for a claim which writes none.
	ID string `json:"id,omitempty"`
}

// Spec is the claim these axes describe, read against what the registry
// declares about the predicate.
//
// The registry is needed before the value can be read at all: which of the four
// shapes a value takes is registry data, and reading `1.0 2.0 3.0` as a scalar
// or as a coordinate is a question only the declaration answers. A predicate
// nothing declares is therefore answered by [ClaimSpec.Check] rather than by
// guessing a shape and reporting the value against it.
func (axes ClaimAxes) Spec(subject ID, predicate string, registry *Registry) (ClaimSpec, error) {
	spec := ClaimSpec{Subject: subject, Predicate: predicate, Source: axes.Source}

	// The predicate is answered before anything else is read, because nothing
	// else can be read without it: a value reported against a shape nobody
	// declared says nothing anybody can act on.
	declared, ok := registry.Predicate(predicate)
	if !ok {
		return spec, spec.Check(registry)
	}

	if axes.Value == nil {
		return spec, ErrNoValue
	}

	value, err := ParseValue(*axes.Value, Unit(axes.Unit), declared)
	if err != nil {
		return spec, err
	}
	spec.Value = value

	if spec.Date, err = readDate(axes.Date); err != nil {
		return spec, err
	}

	if spec.Method, err = identify(axes.Method); err != nil {
		return spec, err
	}

	if spec.ID, err = identify(axes.ID); err != nil {
		return spec, err
	}

	if spec.Accuracy, err = axes.terms(); err != nil {
		return spec, err
	}

	return spec, nil
}

// Provenance is the evidence every claim written with these axes carries: its
// source, its method, its accuracy and its date, without the value.
//
// It is what a scaffold is written with, because a scaffold's values are its
// corners: one value for a list of forty corners would have to mean one of them,
// and there is no reading of that which is not a guess.
func (axes ClaimAxes) Provenance(predicate string) (ClaimSpec, error) {
	spec := ClaimSpec{Predicate: predicate, Source: axes.Source}

	var err error
	if spec.Date, err = readDate(axes.Date); err != nil {
		return spec, err
	}

	if spec.Method, err = identify(axes.Method); err != nil {
		return spec, err
	}

	if spec.Accuracy, err = axes.terms(); err != nil {
		return spec, err
	}

	return spec, nil
}

// terms are the accuracy terms these axes were written with, in the order they
// were written.
func (axes ClaimAxes) terms() ([]AccuracyTerm, error) {
	var out []AccuracyTerm

	for _, written := range axes.Accuracy {
		term, err := ParseAccuracyTerm(written)
		if err != nil {
			return nil, err
		}
		out = append(out, term)
	}

	return out, nil
}

// readDate is the day a change is dated, which is the day it is made where
// nothing said.
//
// The one spelling of a date is the format's, so the parse is too: a date
// written on a command line, in an operation file and in an entity file are held
// to the same rule and refused in the same words.
func readDate(written string) (time.Time, error) {
	if written == "" {
		return time.Time{}, nil
	}
	return ParseDate(written)
}

// identify reads an id written in an operation file.
//
// An empty one comes back as the empty id rather than as a refusal, because a
// member nobody wrote and a member written empty are the same absence — which
// the spec being built reports against the axis it belongs to, naming what is
// missing rather than what is malformed.
func identify(written string) (ID, error) {
	if written == "" {
		return "", nil
	}
	return ParseID(written)
}

// identifyAll reads a list of ids written in an operation file, in the order
// they were written.
func identifyAll(written []string) ([]ID, error) {
	out := make([]ID, 0, len(written))

	for _, one := range written {
		id, err := identify(one)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}

	return out, nil
}

// AddNodeOperation writes a new semantic node. It is `add-node`.
type AddNodeOperation struct {
	// ID is the id it will be written with.
	ID string `json:"id"`

	// Kind is the kind it declares.
	Kind string `json:"kind,omitempty"`

	// Type is the type it declares.
	Type string `json:"type,omitempty"`

	// Geometry is the geometry form it declares, and is empty for a node with
	// none — which its type has to permit.
	Geometry string `json:"geometry,omitempty"`

	// Frame is the coordinate frame it is expressed in.
	Frame string `json:"frame,omitempty"`

	// Label is its display text, which nothing resolves through.
	Label string `json:"label,omitempty"`

	// File is where to write it, overriding the routing rules. A path relative
	// to the model root, ending in [Extension].
	File string `json:"file,omitempty"`
}

// Name implements [Operation].
func (o *AddNodeOperation) Name() string { return addNodeOperation }

func (o *AddNodeOperation) check() error {
	if o.ID == "" {
		return ErrNoID
	}
	return nil
}

func (o *AddNodeOperation) apply(tx *Tx, _ *Applied) error {
	id, err := identify(o.ID)
	if err != nil {
		return err
	}

	frame, err := identify(o.Frame)
	if err != nil {
		return err
	}

	spec := NodeSpec{
		ID:       id,
		Kind:     Kind(o.Kind),
		Type:     o.Type,
		Geometry: Geometry(o.Geometry),
		Frame:    frame,
		Label:    o.Label,
	}

	// Where it goes is decided before it is written, by the rules `dfcad route`
	// reports: an author who checked where something would land and a batch
	// which writes it there have to be answering one question.
	destination, err := spec.Destination(tx.graph.Registry(), o.File)
	if err != nil {
		return err
	}

	return tx.AddNode(spec, destination.Path)
}

// AddVertexOperation writes a new corner, with where it is and how that is
// known. It is `add-vertex`.
type AddVertexOperation struct {
	// ID is the id it will be written with.
	ID string `json:"id"`

	// Frame is the frame its position is expressed in. A geometric node is
	// always in exactly one.
	Frame string `json:"frame,omitempty"`

	// Label is its display text.
	Label string `json:"label,omitempty"`

	// File is where to write it, overriding the routing rules.
	File string `json:"file,omitempty"`

	// Predicate is the predicate its position is claimed under, and is empty
	// for a corner which has been named and not yet surveyed. The claim below
	// is read only where it is given.
	Predicate string `json:"predicate,omitempty"`

	// Claim is the position claim written in the same change as the vertex,
	// because a vertex and the first thing anybody knows about where it is are
	// one statement.
	Claim ClaimAxes `json:"claim,omitempty"`
}

// Name implements [Operation].
func (o *AddVertexOperation) Name() string { return addVertexOperation }

func (o *AddVertexOperation) check() error {
	if o.ID == "" {
		return ErrNoID
	}
	if o.Frame == "" {
		return ErrNoFrame
	}
	return nil
}

func (o *AddVertexOperation) apply(tx *Tx, _ *Applied) error {
	id, err := identify(o.ID)
	if err != nil {
		return err
	}

	frame, err := identify(o.Frame)
	if err != nil {
		return err
	}

	spec := VertexSpec{ID: id, Label: o.Label, Frame: frame}

	if o.Predicate != "" {
		if spec.Position, err = o.Claim.Spec(id, o.Predicate, tx.graph.Registry()); err != nil {
			return err
		}
	}

	destination, err := spec.Destination(tx.graph.Registry(), o.File)
	if err != nil {
		return err
	}

	return tx.AddVertex(spec, destination.Path)
}

// AddEdgeOperation writes a connection between two corners. It is `add-edge`.
type AddEdgeOperation struct {
	// ID is the id it will be written with.
	ID string `json:"id"`

	// Frame is the frame it is expressed in.
	Frame string `json:"frame,omitempty"`

	// Label is its display text.
	Label string `json:"label,omitempty"`

	// File is where to write it, overriding the routing rules.
	File string `json:"file,omitempty"`

	// Start and End are the vertices it runs between, in that order. The order
	// is significant and is never sorted: an edge is directed.
	Start string `json:"start"`
	End   string `json:"end"`

	// BackedBy are the semantic nodes which physically realise it. An edge
	// which names none is virtual, which is computed rather than written.
	BackedBy []string `json:"backedBy,omitempty"`
}

// Name implements [Operation].
func (o *AddEdgeOperation) Name() string { return addEdgeOperation }

func (o *AddEdgeOperation) check() error {
	if o.ID == "" {
		return ErrNoID
	}
	if o.Frame == "" {
		return ErrNoFrame
	}
	if o.Start == "" || o.End == "" {
		return ErrNoEndpoints
	}
	return nil
}

func (o *AddEdgeOperation) apply(tx *Tx, _ *Applied) error {
	written, err := identifyAll([]string{o.ID, o.Frame, o.Start, o.End})
	if err != nil {
		return err
	}

	backing, err := identifyAll(o.BackedBy)
	if err != nil {
		return err
	}

	spec := EdgeSpec{
		ID:       written[0],
		Label:    o.Label,
		Frame:    written[1],
		Start:    written[2],
		End:      written[3],
		BackedBy: backing,
	}

	destination, err := spec.Destination(tx.graph.Registry(), o.File)
	if err != nil {
		return err
	}

	return tx.AddEdge(spec, destination.Path)
}

// AddLoopOperation writes an ordered ring of edges. It is `add-loop`.
type AddLoopOperation struct {
	// ID is the id it will be written with.
	ID string `json:"id"`

	// Frame is the frame it is expressed in.
	Frame string `json:"frame,omitempty"`

	// Label is its display text.
	Label string `json:"label,omitempty"`

	// File is where to write it, overriding the routing rules.
	File string `json:"file,omitempty"`

	// Edges are the edges of the ring, in the order the loop is traversed. The
	// order is the data and is never sorted.
	Edges []string `json:"edges"`
}

// Name implements [Operation].
func (o *AddLoopOperation) Name() string { return addLoopOperation }

func (o *AddLoopOperation) check() error {
	if o.ID == "" {
		return ErrNoID
	}
	if o.Frame == "" {
		return ErrNoFrame
	}
	if len(o.Edges) == 0 {
		return ErrNoEdges
	}
	return nil
}

func (o *AddLoopOperation) apply(tx *Tx, _ *Applied) error {
	id, err := identify(o.ID)
	if err != nil {
		return err
	}

	frame, err := identify(o.Frame)
	if err != nil {
		return err
	}

	edges, err := identifyAll(o.Edges)
	if err != nil {
		return err
	}

	spec := LoopSpec{ID: id, Label: o.Label, Frame: frame, Edges: edges}

	destination, err := spec.Destination(tx.graph.Registry(), o.File)
	if err != nil {
		return err
	}

	return tx.AddLoop(spec, destination.Path)
}

// ScaffoldLoopOperation writes a room's corners, walls and outline in one
// operation. It is `scaffold-loop`.
type ScaffoldLoopOperation struct {
	// Namespace is the declared id namespace the new nodes are minted in.
	Namespace string `json:"namespace"`

	// Frame is the frame the corners are expressed in.
	Frame string `json:"frame,omitempty"`

	// Label is the loop's display text.
	Label string `json:"label,omitempty"`

	// File is where to write everything, overriding the routing rules.
	File string `json:"file,omitempty"`

	// Predicate is the predicate a corner's position is claimed under.
	Predicate string `json:"predicate"`

	// Tolerance is the declared tolerance two corners are judged to be one
	// point by, which is also what says the list closed.
	Tolerance string `json:"tolerance"`

	// Corners are the corners of the loop, in order and authored closed: the
	// last names the first corner again. Each is written in the shape the
	// position predicate declares.
	Corners []string `json:"corners"`

	// NoSnap writes a new vertex at every corner, even where one is already
	// there. Every corner which would have been reused is still reported.
	NoSnap bool `json:"noSnap,omitempty"`

	// Claim is the evidence every position claim is written with. Its value is
	// not read: a corner's value is the corner.
	Claim ClaimAxes `json:"claim,omitempty"`
}

// Name implements [Operation].
func (o *ScaffoldLoopOperation) Name() string { return scaffoldLoopOperation }

func (o *ScaffoldLoopOperation) check() error {
	if o.Namespace == "" {
		return ErrNoNamespace
	}
	if o.Frame == "" {
		return ErrNoFrame
	}
	if o.Predicate == "" {
		return ErrNoPredicate
	}
	if len(o.Corners) == 0 {
		return ErrNoCorners
	}
	return nil
}

func (o *ScaffoldLoopOperation) apply(tx *Tx, out *Applied) error {
	frame, err := identify(o.Frame)
	if err != nil {
		return err
	}

	spec := ScaffoldSpec{
		Namespace: o.Namespace,
		Frame:     frame,
		Label:     o.Label,
		Predicate: o.Predicate,
		Tolerance: o.Tolerance,
		Snap:      !o.NoSnap,
	}

	if spec.Provenance, err = o.Claim.Provenance(o.Predicate); err != nil {
		return err
	}

	// Nothing can be read from a corner without the declaration — which shape a
	// value takes is what the declaration says — so where there is none the
	// list is handed over unread, and [Tx.Scaffold] refuses the predicate in
	// the engine's words.
	if declared, ok := tx.graph.Registry().Predicate(o.Predicate); ok {
		for _, written := range o.Corners {
			value, err := ParseValue(written, Unit(o.Claim.Unit), declared)
			if err != nil {
				return err
			}
			spec.Corners = append(spec.Corners, Corner{Position: value})
		}
	}

	built, notices, err := tx.Scaffold(spec, o.File)
	if err != nil {
		return err
	}

	out.Snaps, out.Tolerance, out.Notices = built.Snaps, built.Tolerance, notices

	return nil
}

// ClassifyTypeOperation says how a scheme outside this model names a type. It
// is `classify-type`.
type ClassifyTypeOperation struct {
	// Type is the declared type being classified.
	Type string `json:"type"`

	// System names the scheme. It is opaque: nothing here knows any scheme, and
	// no value is preferred over another.
	System string `json:"system"`

	// Code names the type within that scheme, and is opaque for the same
	// reason.
	Code string `json:"code"`
}

// Name implements [Operation].
func (o *ClassifyTypeOperation) Name() string { return classifyTypeOperation }

func (o *ClassifyTypeOperation) check() error {
	if o.Type == "" {
		return ErrNoType
	}
	return nil
}

func (o *ClassifyTypeOperation) apply(tx *Tx, _ *Applied) error {
	return tx.Classify(o.Type, ExternalClassification{System: o.System, Code: o.Code})
}

// SetLabelOperation changes what a thing is called, and nothing else. It is
// `set-label`.
type SetLabelOperation struct {
	// ID is the thing being renamed.
	ID string `json:"id"`

	// Label is what it is now called. An empty one removes the label, which is
	// how a thing goes back to having none.
	Label string `json:"label"`
}

// Name implements [Operation].
func (o *SetLabelOperation) Name() string { return setLabelOperation }

func (o *SetLabelOperation) check() error {
	if o.ID == "" {
		return ErrNoID
	}
	return nil
}

func (o *SetLabelOperation) apply(tx *Tx, _ *Applied) error {
	id, err := identify(o.ID)
	if err != nil {
		return err
	}

	return tx.SetLabel(id, o.Label)
}

// RetireOperation records that a thing stopped existing. It is `retire`.
type RetireOperation struct {
	// ID is the thing which stopped existing.
	ID string `json:"id"`

	// Reason is why, in the author's words. It is required: a retirement with
	// no reason is a deletion wearing a hat.
	Reason string `json:"reason"`

	// Replacement is the node which stands in its place, where one does.
	// Supplying one is also what redirects the references to it.
	Replacement string `json:"replacement,omitempty"`

	// Date is when it stopped existing, as YYYY-MM-DD. Today by default.
	Date string `json:"date,omitempty"`
}

// Name implements [Operation].
func (o *RetireOperation) Name() string { return retireOperation }

func (o *RetireOperation) check() error {
	if o.ID == "" {
		return ErrNoID
	}
	if o.Reason == "" {
		return MissingReasonError{ID: ID(o.ID)}
	}
	return nil
}

func (o *RetireOperation) apply(tx *Tx, _ *Applied) error {
	id, err := identify(o.ID)
	if err != nil {
		return err
	}

	replacement, err := identify(o.Replacement)
	if err != nil {
		return err
	}

	when, err := readDate(o.Date)
	if err != nil {
		return err
	}

	return tx.Retire(id, RetirementSpec{Date: when, Reason: o.Reason, SupersededBy: replacement})
}

// AddClaimOperation attaches a measured value to a thing, with its provenance.
// It is `add-claim`.
type AddClaimOperation struct {
	// Subject is the thing the claim is about.
	Subject string `json:"subject"`

	// Predicate is the predicate it is written under.
	Predicate string `json:"predicate"`

	// Claim is the value and the evidence for it.
	Claim ClaimAxes `json:"claim"`
}

// Name implements [Operation].
func (o *AddClaimOperation) Name() string { return addClaimOperation }

func (o *AddClaimOperation) check() error { return claimed(o.Subject, o.Predicate, o.Claim) }

func (o *AddClaimOperation) apply(tx *Tx, out *Applied) error {
	spec, err := readClaim(tx, o.Subject, o.Predicate, o.Claim)
	if err != nil {
		return err
	}

	id, notices, err := tx.AddClaim(spec)
	if err != nil {
		return err
	}

	out.Claim, out.Notices = id, notices

	return nil
}

// SupersedeOperation corrects a value: it states the new one and retracts the
// old. It is `supersede`.
type SupersedeOperation struct {
	// Subject is the thing the claim is about.
	Subject string `json:"subject"`

	// Predicate is the predicate the claim being corrected was written under.
	Predicate string `json:"predicate"`

	// Claim is the new value and the evidence for it.
	Claim ClaimAxes `json:"claim"`
}

// Name implements [Operation].
func (o *SupersedeOperation) Name() string { return supersedeOperation }

func (o *SupersedeOperation) check() error { return claimed(o.Subject, o.Predicate, o.Claim) }

func (o *SupersedeOperation) apply(tx *Tx, out *Applied) error {
	spec, err := readClaim(tx, o.Subject, o.Predicate, o.Claim)
	if err != nil {
		return err
	}

	// The claim which is about to be retracted is read before the change, so
	// that what the operation did can name it: after the change it is still
	// there and the answer is the same, and reading it here is what makes the
	// two halves of the supersession one sentence.
	replaced := live(tx.graph.Claims().Live(spec.Subject, spec.Predicate))

	id, notices, err := tx.Supersede(spec)
	if err != nil {
		return err
	}

	out.Claim, out.Replaced, out.Notices = id, replaced, notices

	return nil
}

// DeprecateClaimOperation records that a claim was retracted. It is
// `deprecate-claim`.
type DeprecateClaimOperation struct {
	// Claim is the id of the claim being retracted, which is the id it wrote.
	Claim string `json:"claim"`

	// SupersededBy is the claim which stands in its place. It is required: a
	// rank cannot be used to make a measurement quietly go away.
	SupersededBy string `json:"supersededBy"`
}

// Name implements [Operation].
func (o *DeprecateClaimOperation) Name() string { return deprecateClaimOperation }

func (o *DeprecateClaimOperation) check() error {
	if o.Claim == "" {
		return ErrNoID
	}
	if o.SupersededBy == "" {
		return ErrNoSupersedingClaim
	}
	return nil
}

func (o *DeprecateClaimOperation) apply(tx *Tx, out *Applied) error {
	ids, err := identifyAll([]string{o.Claim, o.SupersededBy})
	if err != nil {
		return err
	}

	notices, err := tx.DeprecateClaim(ids[0], ids[1])
	if err != nil {
		return err
	}

	out.Replaced, out.Notices = ids[0], notices

	return nil
}

// claimed reports what is missing from an operation which writes a claim,
// before any model is read.
//
// The subject and the predicate are checked here as well as by
// [ClaimSpec.Check] so that a batch reports every operation which forgot one at
// once, and in the same words the engine would use later.
func claimed(subject, predicate string, axes ClaimAxes) error {
	if subject == "" {
		return ErrNoSubject
	}
	if predicate == "" {
		return ErrNoPredicate
	}
	if axes.Value == nil {
		return ErrNoValue
	}
	return nil
}

// readClaim is the claim an operation describes, read against the registry.
func readClaim(tx *Tx, subject, predicate string, axes ClaimAxes) (ClaimSpec, error) {
	id, err := identify(subject)
	if err != nil {
		return ClaimSpec{}, err
	}

	return axes.Spec(id, predicate, tx.graph.Registry())
}

// live names the one live claim of a subject and predicate, which is the claim
// a supersession is about to replace.
//
// It is empty where that claim wrote no id of its own, which is the ordinary
// case: what an empty id says is that nothing pointed at it before now.
func live(claims []*Claim) ID {
	if len(claims) != 1 {
		return ""
	}
	if id, ok := claims[0].ID(); ok {
		return id
	}
	return ""
}
