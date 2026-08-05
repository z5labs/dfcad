// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"iter"
	"slices"
	"strings"

	sexpr "github.com/z5labs/sexpr-go"
)

// Argument is one parameter of an invariant as it was written.
//
// The values are the datums the registry file holds, unmodified. Nothing
// re-reads them here: every one of them was read as the sort of datum the check
// declares it takes when the registry loaded, and a name it resolved — a
// tolerance, a predicate, a type — was resolved against the registry then
// ([ValidateAssertion]). What a run needs from this layer is what was written
// and where, which is also what a violation has to print.
type Argument struct {
	// Name is the tag the parameter was written with.
	Name string

	// Values are the datums written after that tag, in the order they were
	// written. A parameter which takes one value has one.
	Values []*Node

	// Span is where the parameter form was written.
	Span Span
}

// Symbol returns the one symbol written for the parameter, and whether exactly
// one was written and it is a symbol.
//
// It is how a parameter naming an entry of a registry is read, which is most of
// them: a tolerance, a predicate and a type are each written as the name they
// were declared under.
func (a Argument) Symbol() (string, bool) {
	if len(a.Values) != 1 {
		return "", false
	}

	symbol, ok := a.Values[0].Datum.(sexpr.Symbol)
	if !ok {
		return "", false
	}
	return symbol.Value, true
}

// Symbols returns every symbol written for the parameter, in the order they
// were written, which is how a repeated parameter naming registry entries is
// read. A value which is not a symbol is left out.
func (a Argument) Symbols() []string {
	out := make([]string, 0, len(a.Values))
	for _, value := range a.Values {
		if symbol, ok := value.Datum.(sexpr.Symbol); ok {
			out = append(out, symbol.Value)
		}
	}
	return out
}

// String renders the parameter the way it was written, which is what a
// violation naming the parameters it was evaluated with prints.
//
// A value which is not an atom is rendered as an ellipsis rather than expanded.
// No parameter of any check takes one — every [ParameterType] is an atom — so
// this is the rendering of something already reported at load, and quoting a
// whole subtree into a one-line message would bury the rest of it.
func (a Argument) String() string {
	written := make([]string, 0, len(a.Values)+1)
	written = append(written, a.Name)

	for _, value := range a.Values {
		if text := atom(value.Datum); text != "" {
			written = append(written, text)
			continue
		}
		written = append(written, "…")
	}

	return "(" + strings.Join(written, " ") + ")"
}

// clone copies the argument so that what a check is handed cannot be written
// through to the binding it came from.
//
// The datums themselves are shared rather than copied. A loaded tree is never
// written to — a [Graph] is read-only once loaded — so what this protects is the
// cheap accident of a check writing over the slice it was given, and copying a
// subtree per parameter per instance would copy most of a registry file to hand
// a check a name it only reads.
func (a Argument) clone() Argument {
	a.Values = slices.Clone(a.Values)
	return a
}

// arguments reads the parameters written on one invariant.
func arguments(invariant Invariant) []Argument {
	out := make([]Argument, 0, len(invariant.Parameters))

	for _, parameter := range invariant.Parameters {
		name, ok := formTag(parameter)
		if !ok {
			// Something written where a parameter belongs which is not a form
			// is reported by the validation the registry load ran; there is no
			// parameter here to carry.
			continue
		}

		out = append(out, Argument{
			Name:   name,
			Values: slices.Clone(elements(parameter)),
			Span:   parameter.Span,
		})
	}

	return out
}

// InvariantBinding is one invariant of a type bound to one instance of that
// type.
//
// It is what makes an invariant stated once apply to a hundred and fifty rooms:
// nothing is copied onto the instance and nothing is stored on it, so the
// binding is computed from the type every time it is asked for and an instance
// written after the invariant was declared carries it as fully as one written
// before ([Graph.Invariants]).
type InvariantBinding struct {
	// Instance is the node the invariant applies to.
	Instance *SemanticNode

	// Type is the name of the type which declared it, which is the type the
	// instance names.
	Type string

	// Check is what the engine's closed check registry says the named check
	// constrains and takes.
	Check CheckDeclaration

	// Declared is the invariant as the registry wrote it. Its span is the
	// registry file and line a violation points back at.
	Declared Invariant

	// Arguments are the parameters it was written with.
	Arguments []Argument

	// runner is the implementation of the check, and is nil for a check which
	// declares itself and cannot yet be run.
	runner Runner
}

// Runnable reports whether the check has an implementation to run.
//
// A check which declares itself and implements nothing binds, lists and
// validates exactly as one which does, and running it reports nothing. The two
// are told apart here rather than by an empty result, because "this rule holds"
// and "nothing has been written to decide whether it holds" are different
// answers.
func (b InvariantBinding) Runnable() bool { return b.runner != nil }

// String renders the binding as the instance it applies to and the invariant as
// it was written.
func (b InvariantBinding) String() string {
	written := make([]string, 0, len(b.Arguments)+2)
	if b.Instance != nil {
		written = append(written, string(b.Instance.ID()))
	}
	written = append(written, b.Declared.Check)

	for _, argument := range b.Arguments {
		written = append(written, argument.String())
	}

	return strings.Join(written, " ")
}

// CheckSubject is what a check is run against: the model, the node the
// invariant was bound to, and the parameters it supplies.
//
// The graph comes along because a check answers a question about the model and
// not only about the node — whether the thing a node is written within resolves,
// whether a claim was ever made under a predicate — and a check which held its
// own copy of any of that would answer from a model which had moved on.
type CheckSubject struct {
	graph     *Graph
	node      *SemanticNode
	arguments []Argument
}

// Graph returns the model the subject belongs to.
func (s CheckSubject) Graph() *Graph { return s.graph }

// Node returns the node under check.
func (s CheckSubject) Node() *SemanticNode { return s.node }

// Arguments returns every parameter the invariant supplied, in the order they
// were written.
//
// What comes back is the check's own copy, down to the values of each
// parameter: a check is handed what the invariant says and cannot write back
// through it to the binding, to the instance after it, or to the rendering of
// the violation it is about to report.
func (s CheckSubject) Arguments() []Argument {
	out := make([]Argument, 0, len(s.arguments))
	for _, argument := range s.arguments {
		out = append(out, argument.clone())
	}
	return out
}

// Argument returns the parameter written under name, and whether one was.
//
// A parameter the check declares as required is always here, because an
// invariant missing one is a load error and never reaches a run. An optional
// one left out is ordinary, which is why this reports whether it was written
// rather than assuming it was.
func (s CheckSubject) Argument(name string) (Argument, bool) {
	for _, argument := range s.arguments {
		if argument.Name == name {
			return argument.clone(), true
		}
	}
	return Argument{}, false
}

// Failure is what a check reports about a subject which does not satisfy it.
//
// It carries what was wrong and nothing about which rule was being applied:
// the check name, the parameters and where the rule was declared are the
// binding's, and are attached to the [Violation] the run produces. A check
// which repeated them would be a second place they could disagree from.
type Failure struct {
	// Message says what was expected and what was found, in that order.
	Message string

	// Hint is optional advice on what to do about it.
	Hint string

	// Span is the part of the subject which failed — the loop which does not
	// close, the edge which reaches nothing. The zero span means the failure is
	// about the whole subject, and the violation points at the node.
	Span Span

	// Related are the other places which explain this one.
	Related []RelatedLocation
}

// Runner is a [Check] which can be run against a subject.
//
// It is a second interface rather than a method on [Check] because declaring a
// check and implementing it are separately useful: everything above the
// registry — validating an assertion at load, listing what a model may write,
// binding an invariant to the instances it applies to — reads the declaration
// and never runs anything, and a check which is declared and not yet
// implemented is exactly what those layers have to keep working against.
type Runner interface {
	Check

	// Run examines one subject and returns one failure for every way it does
	// not satisfy the check. A subject which satisfies it yields none.
	//
	// Run is never called with a subject the check does not apply to: whether
	// the check can examine a node of this kind, with this geometry, is decided
	// by the binding and by the registry load before it.
	Run(subject CheckSubject) []Failure
}

// Violation is one instance failing one invariant of its type.
//
// Every field a reader needs to act on it is here rather than folded into the
// message: which thing failed, which rule, the parameters that rule was
// evaluated with, and the registry file and line which declared it — because a
// rule stated once and applied to a hundred and fifty instances is one whose
// failure has to lead back to the one place it is written.
//
// [Violation.Diagnostic] is the human rendering, through the same renderer
// every diagnostic uses. The machine rendering is this struct, and neither is
// produced by parsing the other.
type Violation struct {
	// Instance is the id of the node which failed.
	Instance ID `json:"instance"`

	// Type is the name of its type, which is what declared the invariant.
	Type string `json:"type"`

	// Check is the check name the invariant named.
	Check string `json:"check"`

	// Arguments are the parameters it was evaluated with, each rendered the way
	// it was written.
	Arguments []string `json:"arguments,omitempty"`

	// Declared is where the invariant was written, which is a position in a
	// registry file rather than in the file the instance is in.
	Declared Span `json:"declared"`

	// Subject is where what failed was written: the node, or the part of it the
	// check pointed at.
	Subject Span `json:"subject"`

	// Message says what was expected and what was found.
	Message string `json:"message"`

	// Hint is optional advice on what to do about it.
	Hint string `json:"hint,omitempty"`

	// Related are the other places which explain this one. Where the invariant
	// was declared is not among them; it is [Violation.Declared], and
	// [Violation.Diagnostic] renders it.
	Related []RelatedLocation `json:"related,omitempty"`
}

// Written renders the invariant as the check name followed by the parameters it
// was evaluated with, which is how a report names the rule that failed.
func (v Violation) Written() string {
	return strings.Join(append([]string{v.Check}, v.Arguments...), " ")
}

// Diagnostic renders the violation as a diagnostic, pointing at what failed and
// noting where the rule it failed was declared.
func (v Violation) Diagnostic() Diagnostic {
	related := make([]RelatedLocation, 0, len(v.Related)+1)
	related = append(related, v.Related...)
	related = append(related, RelatedLocation{
		Span:    v.Declared,
		Message: fmt.Sprintf("the type %s declares the invariant here, for every instance of it", v.Type),
	})

	return Diagnostic{
		Severity: SeverityError,
		Span:     v.Subject,
		Message: fmt.Sprintf(
			"expected %s to satisfy the invariant %s of its type %s: %s",
			v.Instance, v.Written(), v.Type, v.Message,
		),
		Hint:    v.Hint,
		Related: related,
	}
}

// String renders the violation as the single line an editor and a terminal
// already know how to jump to.
func (v Violation) String() string { return v.Diagnostic().String() }

// Invariants returns the invariants which bear on one instance, in the order
// its type declared them.
//
// **Invariants are not inherited.** A node's invariants are the ones its own
// type declares and nothing else: none descends from the node which contains
// it, from a zone it is a member of, or from another type — the type registry
// declares no hierarchy for one type to inherit through
// (specification section 7.3). An invariant which should hold of the rooms
// inside a storey is declared on the room type, not on the storey's, and a rule
// which reached instances through containment would apply to a different set of
// nodes every time somebody moved one.
//
// What is filtered is applicability, not inheritance. A type may permit more
// than one kind and more than one geometry form — including no geometry at all
// — and a check declares which of those it can examine. An invariant naming a
// check which could examine no instance of the type is refused when the registry
// loads; one which can examine some of them binds to those and not to the rest,
// so a check about an area is not run against the instance which has no shape.
//
// Nothing is stored on the instance. The binding is computed from the type every
// time, which is what makes an invariant declared today apply to an instance
// written tomorrow without either being touched.
func (g *Graph) Invariants(node *SemanticNode) []InvariantBinding {
	return g.invariants(node, registeredChecks)
}

// invariants is [Graph.Invariants] against a given set of checks, which is what
// lets the engine's closed registry and a set assembled for a test be the same
// thing exercised the same way.
func (g *Graph) invariants(node *SemanticNode, set *checkSet) []InvariantBinding {
	if g == nil || node == nil {
		return nil
	}

	declaredType, ok := g.registry.Type(node.Type())
	if !ok {
		// A node naming a type nothing declares is a load error, and there is
		// no type here to read invariants from.
		return nil
	}

	var out []InvariantBinding
	for _, invariant := range declaredType.Invariants {
		check, ok := set.lookup(invariant.Check)
		if !ok {
			// An unregistered check name is a load error. A graph is usable
			// whatever the diagnostics say, so this is reached, and binding a
			// rule nothing can read would be worse than binding none.
			continue
		}

		if !examines(check, node) {
			continue
		}

		out = append(out, InvariantBinding{
			Instance:  node,
			Type:      declaredType.Name,
			Check:     check,
			Declared:  invariant,
			Arguments: arguments(invariant),
			runner:    set.runner(invariant.Check),
		})
	}

	return out
}

// AllInvariants iterates every invariant bound to every instance of the model,
// node by node in the order the load read them and, within a node, in the order
// its type declared them.
//
// The order is deterministic for the reason the load's is: a listing of what
// would run, and a report of what did, have to diff against the last run's.
func (g *Graph) AllInvariants() iter.Seq[InvariantBinding] {
	return g.allInvariants(registeredChecks)
}

// allInvariants is [Graph.AllInvariants] against a given set of checks.
func (g *Graph) allInvariants(set *checkSet) iter.Seq[InvariantBinding] {
	return func(yield func(InvariantBinding) bool) {
		if g == nil {
			return
		}

		for node := range g.Nodes().All() {
			for _, binding := range g.invariants(node, set) {
				if !yield(binding) {
					return
				}
			}
		}
	}
}

// CheckInvariants runs every invariant bound to every instance and returns one
// violation for each way one of them is not satisfied.
//
// A model whose types declare no invariant, and one whose instances all satisfy
// theirs, both yield none: a type with nothing to say produces no output rather
// than a line saying it had nothing to say.
//
// Violations come back in the order [Graph.AllInvariants] bound them, and within
// one binding in the order the check reported them. Collecting them into a
// [Diagnostics] is what puts them in reporting order, which is by position.
func (g *Graph) CheckInvariants() []Violation {
	return g.checkInvariants(registeredChecks)
}

// checkInvariants is [Graph.CheckInvariants] against a given set of checks.
func (g *Graph) checkInvariants(set *checkSet) []Violation {
	var out []Violation

	for binding := range g.allInvariants(set) {
		if binding.runner == nil {
			continue
		}

		subject := CheckSubject{graph: g, node: binding.Instance, arguments: binding.Arguments}
		for _, failure := range binding.runner.Run(subject) {
			out = append(out, binding.violation(failure))
		}
	}

	return out
}

// violation attaches the binding — what failed, which rule and where that rule
// is written — to what the check found.
func (b InvariantBinding) violation(failure Failure) Violation {
	subject := failure.Span
	if subject == (Span{}) && b.Instance != nil {
		subject = b.Instance.Span()
	}

	written := make([]string, 0, len(b.Arguments))
	for _, argument := range b.Arguments {
		written = append(written, argument.String())
	}

	var instance ID
	if b.Instance != nil {
		instance = b.Instance.ID()
	}

	return Violation{
		Instance:  instance,
		Type:      b.Type,
		Check:     b.Declared.Check,
		Arguments: written,
		Declared:  b.Declared.Span,
		Subject:   subject,
		Message:   failure.Message,
		Hint:      failure.Hint,
		Related:   failure.Related,
	}
}

// examines reports whether a check can examine one instance.
//
// An invariant is written on a type and applies to nodes, so a check which is
// not written on a node cannot be one. The other two axes are the instance's own
// and not the type's: a type permitting a kind or a geometry form says what its
// instances may be, and what this instance is is what decides whether the check
// has anything to look at.
func examines(check CheckDeclaration, node *SemanticNode) bool {
	if !check.PermitsForm(SubjectNode) {
		return false
	}

	if !check.PermitsKind(node.Kind()) {
		return false
	}

	// A node with no geometry carries the zero form, which satisfies a check
	// declaring no geometry and nothing else.
	geometry, _ := node.Geometry()
	return check.PermitsGeometry(geometry)
}

// inapplicable says why a check could apply to no instance of a type at all,
// and whether it could not. It is the rule the registry load enforces, and what
// comes back is the message and the hint of the diagnostic it raises.
//
// The three axes are asked in the order a reader would: an invariant is written
// on a type and applies to nodes, a type says which kinds its instances may be,
// and it says which shapes they may have. Only the first mismatch found is
// reported, because they are one mistake and the second reading of it says
// nothing the first did not.
//
// An axis the check leaves open applies to everything on it, and an axis the
// type leaves empty is a type which is already being reported as malformed;
// neither yields a mismatch here.
func inapplicable(check CheckDeclaration, declared Type) (message string, hint string, ok bool) {
	if !check.PermitsForm(SubjectNode) {
		return fmt.Sprintf(
				"expected an invariant naming a check which applies to a node, found %s, which applies to %s",
				check.Name, join(spellings(check.Forms), "and"),
			),
			"an invariant is declared on a type and applies to its instances, which are nodes; a check which applies to " +
				"a vertex, an edge or a loop is written as an assert on that subject instead",
			true
	}

	if len(check.Kinds) > 0 && len(declared.Kinds) > 0 && !anyPermitted(declared.Kinds, check.PermitsKind) {
		return fmt.Sprintf(
				"expected an invariant naming a check which applies to a kind the type %s permits, found %s, which applies to %s",
				declared.Name, check.Name, join(spellings(check.Kinds), "and"),
			),
			fmt.Sprintf(
				"%s permits %s, so no instance of it is a subject this check could examine",
				declared.Name, join(spellings(declared.Kinds), "and"),
			),
			true
	}

	declaresGeometry := len(declared.Geometries) > 0 || declared.Absent
	if len(check.Geometries) > 0 && declaresGeometry && !anyPermitted(declared.Geometries, check.PermitsGeometry) {
		return fmt.Sprintf(
				"expected an invariant naming a check which applies to a geometry form the type %s permits, found %s, which applies to %s",
				declared.Name, check.Name, join(spellings(check.Geometries), "and"),
			),
			fmt.Sprintf(
				"%s permits %s, so no instance of it is a subject this check could examine; an invariant which applies to "+
					"nothing does not fail, it passes on every instance forever",
				declared.Name, declared.permittedGeometry(),
			),
			true
	}

	return "", "", false
}

// anyPermitted reports whether the check permits any one of what the type
// declares.
func anyPermitted[T ~string](declared []T, permits func(T) bool) bool {
	for _, value := range declared {
		if permits(value) {
			return true
		}
	}
	return false
}
