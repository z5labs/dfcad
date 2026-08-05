// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	sexpr "github.com/z5labs/sexpr-go"
)

// SubjectForm is one of the forms a check may be written on, spelled as the tag
// that form is written with.
//
// It is what a check declares it applies to on the axis every subject has. A
// check about the endpoints of an edge is meaningless on a vertex, and saying so
// in the declaration is what lets the mistake be a diagnostic rather than a
// check which quietly passes on everything it cannot examine.
type SubjectForm string

// The forms an assertion may be written on, per specification sections 6.1
// through 6.4. A type-level invariant is written on a type declaration and
// applies to its instances, which are nodes.
const (
	SubjectNode   SubjectForm = "node"
	SubjectVertex SubjectForm = "vertex"
	SubjectEdge   SubjectForm = "edge"
	SubjectLoop   SubjectForm = "loop"
)

var subjectForms = []SubjectForm{SubjectNode, SubjectVertex, SubjectEdge, SubjectLoop}

// SubjectForms returns the forms an assertion may be written on, in
// specification order.
func SubjectForms() []SubjectForm { return slices.Clone(subjectForms) }

// ParameterType is the sort of datum one parameter of a check takes.
//
// The set is closed and is what specification section 6.8 permits a parameter's
// value to be: an id, a name symbol — of a registry entry, or of one of the
// closed sets the engine compiles in — a real number, a string or a boolean. A
// check declares which of them each of its parameters takes, and that
// declaration is the whole of what validates an assertion: there is no second
// place saying what `(tolerance ...)` means.
type ParameterType string

// The sorts of datum a parameter takes. The four which name a registry entry —
// a type, a predicate, a frame and a tolerance — are resolved against the
// registry of their sort at load, which is what makes an assertion inspectable
// without evaluating it ([0011](docs/decisions/0011-assertions-are-named-parameterised-checks.md)).
const (
	ParameterID        ParameterType = "id"
	ParameterReal      ParameterType = "real"
	ParameterString    ParameterType = "string"
	ParameterBoolean   ParameterType = "boolean"
	ParameterKind      ParameterType = "kind"
	ParameterGeometry  ParameterType = "geometry"
	ParameterTypeName  ParameterType = "type"
	ParameterPredicate ParameterType = "predicate"
	ParameterFrame     ParameterType = "frame"
	ParameterTolerance ParameterType = "tolerance"
)

var parameterTypes = []ParameterType{
	ParameterID, ParameterReal, ParameterString, ParameterBoolean,
	ParameterKind, ParameterGeometry, ParameterTypeName, ParameterPredicate,
	ParameterFrame, ParameterTolerance,
}

// ParameterTypes returns the sorts of datum a check parameter may take.
func ParameterTypes() []ParameterType { return slices.Clone(parameterTypes) }

// sort returns the registry a parameter of this type names an entry of, and
// whether it names one at all.
func (t ParameterType) sort() (Sort, bool) {
	switch t {
	case ParameterTypeName:
		return SortType, true
	case ParameterPredicate:
		return SortPredicate, true
	case ParameterFrame:
		return SortFrame, true
	case ParameterTolerance:
		return SortTolerance, true
	}
	return "", false
}

// spelling is what a diagnostic calls a value of this type, and reads after
// "expected".
func (t ParameterType) spelling() string {
	switch t {
	case ParameterID:
		return "an id"
	case ParameterReal:
		return "a real number"
	case ParameterString:
		return "a string"
	case ParameterBoolean:
		return "a boolean"
	case ParameterKind:
		return "a kind"
	case ParameterGeometry:
		return "a geometry form"
	case ParameterTypeName:
		return "a declared type name"
	case ParameterPredicate:
		return "a declared predicate name"
	case ParameterFrame:
		return "a declared frame id"
	case ParameterTolerance:
		return "a declared tolerance name"
	}
	return "a value"
}

// CheckParameter is one named parameter of a check: what it is called, what
// sort of datum it takes, whether it may be left out and what it is for.
//
// The description is not decoration. It is what a listing of the registry
// prints and what the diagnostic for the parameter left out says, so a check
// whose parameters are undescribed is one nobody can use without reading the
// engine.
type CheckParameter struct {
	// Name is the tag the parameter is written with.
	Name string

	// Type is the sort of datum its value is.
	Type ParameterType

	// Required says the assertion must write it. An optional parameter left out
	// is not a diagnostic and the check decides what it means.
	Required bool

	// Repeated says it takes one or more values rather than exactly one, which
	// may be written either as a sequence after the tag or as one parenthesised
	// list.
	Repeated bool

	// Description is what the parameter is for, in one line.
	Description string
}

// CheckDeclaration is everything a check says about itself: its name, what it
// constrains, the parameters it takes and what it applies to.
//
// It is data rather than code because everything above the check reads it:
// validation of an assertion at load, a listing of the registry, and the choice
// of which checks bear on a node somebody is editing. A check whose parameters
// were known only to its implementation would answer none of those without
// being run, which is the property the format is built to keep
// ([0011](docs/decisions/0011-assertions-are-named-parameterised-checks.md)).
type CheckDeclaration struct {
	// Name is the check name an assertion writes after its tag.
	Name string

	// Description is what the check constrains, in one line.
	Description string

	// Parameters are the parameters it takes, in the order a listing prints
	// them and an assertion is expected to write them.
	Parameters []CheckParameter

	// Forms are the forms it may be written on. A check applies to at least
	// one; there is no check which applies to nothing.
	Forms []SubjectForm

	// Kinds are the node kinds it applies to. Empty applies to every kind, and
	// is what a check with nothing to say about the axis declares.
	Kinds []Kind

	// Geometries are the geometry forms it applies to. Empty applies to any
	// geometry and to a subject with none; a check which measures declares the
	// forms it can measure, and a node with no geometry then satisfies none of
	// them.
	Geometries []Geometry
}

// Parameter returns the declaration of one parameter by name, and whether the
// check takes one under that name.
func (d CheckDeclaration) Parameter(name string) (CheckParameter, bool) {
	for _, parameter := range d.Parameters {
		if parameter.Name == name {
			return parameter, true
		}
	}
	return CheckParameter{}, false
}

// PermitsForm reports whether the check may be written on a subject of this
// form.
func (d CheckDeclaration) PermitsForm(form SubjectForm) bool {
	return slices.Contains(d.Forms, form)
}

// PermitsKind reports whether the check applies to a node of this kind. A check
// which declares no kind applies to every one of them.
func (d CheckDeclaration) PermitsKind(kind Kind) bool {
	return len(d.Kinds) == 0 || slices.Contains(d.Kinds, kind)
}

// PermitsGeometry reports whether the check applies to a subject with this
// geometry form. A subject with no geometry is the zero [Geometry], which
// satisfies a check declaring no geometry and nothing else.
func (d CheckDeclaration) PermitsGeometry(geometry Geometry) bool {
	return len(d.Geometries) == 0 || slices.Contains(d.Geometries, geometry)
}

// parameterNames are the parameters in declared order, for a diagnostic which
// lists them.
func (d CheckDeclaration) parameterNames() []string {
	out := make([]string, 0, len(d.Parameters))
	for _, parameter := range d.Parameters {
		out = append(out, parameter.Name)
	}
	return out
}

// clone copies the declaration deeply, so that a caller reading the registry
// cannot write to it.
func (d CheckDeclaration) clone() CheckDeclaration {
	d.Parameters = slices.Clone(d.Parameters)
	d.Forms = slices.Clone(d.Forms)
	d.Kinds = slices.Clone(d.Kinds)
	d.Geometries = slices.Clone(d.Geometries)
	return d
}

// Check is what a check implements to be registered, and it is the whole of the
// interface: a check declares itself, and the declaration is everything the
// registry, the loader and a listing need.
//
// Adding a check is therefore a type implementing this method and a line in the
// registered set below — a change to the engine, reviewed and released like any
// other. There is deliberately no way to define one from data
// ([0011](docs/decisions/0011-assertions-are-named-parameterised-checks.md)):
// an assertion is a name and its parameters, and a format which let a file
// define what a name meant would be an expression language with extra steps.
type Check interface {
	// Declare returns what the check is called, what it takes and what it
	// applies to.
	Declare() CheckDeclaration
}

// invalidCheckError reports a check registered with a declaration the registry
// cannot hold.
//
// It is a mistake in engine code rather than anything a model can cause — no
// file reaches it, because no file can register a check — so it is raised where
// it is made, at initialisation, rather than carried as a diagnostic to
// somebody who cannot act on it.
type invalidCheckError struct {
	// Check is the check name, or the empty string where the declaration has
	// no usable one.
	Check string

	// Reason is what is wrong with the declaration.
	Reason string
}

// Error implements the [error] interface.
func (e invalidCheckError) Error() string {
	name := e.Check
	if name == "" {
		name = "a check"
	}
	return fmt.Sprintf("invalid check registration: %s %s", name, e.Reason)
}

// checkSet is a set of registered checks, indexed by name.
//
// It is a value rather than a package-level map so that the registry the engine
// compiles in and a set assembled for a test are the same thing, exercised the
// same way. Nothing writes to one after it is built.
type checkSet struct {
	byName map[string]CheckDeclaration
	names  []string
}

// newCheckSet registers checks, panicking on a declaration the set cannot hold.
//
// Every rule it enforces is one an assertion downstream would otherwise break
// on: two checks under one name make the name ambiguous, two parameters under
// one name make the parameter ambiguous, a check applying to no form applies to
// nothing, and a tolerance parameter which is not a tolerance name is the
// numeric literal tolerance the format exists to keep out
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
func newCheckSet(checks ...Check) *checkSet {
	set := &checkSet{byName: make(map[string]CheckDeclaration, len(checks))}

	for _, check := range checks {
		declared := check.Declare().clone()

		if err := validCheck(declared); err != nil {
			panic(err)
		}

		if _, taken := set.byName[declared.Name]; taken {
			panic(invalidCheckError{Check: declared.Name, Reason: "is registered twice"})
		}

		set.byName[declared.Name] = declared
	}

	set.names = slices.Sorted(maps.Keys(set.byName))
	return set
}

// validCheck reports what is wrong with a declaration, or nil where it is one
// the registry can hold.
func validCheck(d CheckDeclaration) error {
	if !symbolic(d.Name) {
		return invalidCheckError{
			Check:  d.Name,
			Reason: "is not a name an assertion can write: a check name has to read back out of a file as the one symbol it was written as",
		}
	}

	if d.Description == "" {
		return invalidCheckError{Check: d.Name, Reason: "declares no description, so a listing of the registry cannot say what it constrains"}
	}

	if len(d.Forms) == 0 {
		return invalidCheckError{Check: d.Name, Reason: "applies to no form, so nothing could write it"}
	}

	for _, form := range d.Forms {
		if !slices.Contains(subjectForms, form) {
			return invalidCheckError{Check: d.Name, Reason: fmt.Sprintf("applies to %s, which is no form of the format", form)}
		}
	}

	if len(d.Kinds) > 0 && !slices.Contains(d.Forms, SubjectNode) {
		return invalidCheckError{Check: d.Name, Reason: "restricts the kinds it applies to and does not apply to a node, which is the only form with a kind"}
	}

	seen := make(map[string]struct{}, len(d.Parameters))
	for _, parameter := range d.Parameters {
		if err := validParameter(d.Name, parameter); err != nil {
			return err
		}

		if _, taken := seen[parameter.Name]; taken {
			return invalidCheckError{Check: d.Name, Reason: fmt.Sprintf("declares the parameter %s twice", parameter.Name)}
		}
		seen[parameter.Name] = struct{}{}
	}

	return nil
}

// validParameter reports what is wrong with one parameter of a declaration.
//
// The rule about a tolerance is the one worth stating twice. A parameter named
// for a tolerance and typed as a number is how a numeric literal tolerance gets
// back in: the assertion then writes 0.005 where it should name the registry
// entry, and how close is close enough becomes a decision made once per
// assertion instead of once per model.
func validParameter(check string, p CheckParameter) error {
	if !symbolic(p.Name) {
		return invalidCheckError{
			Check:  check,
			Reason: fmt.Sprintf("declares the parameter %q, which is not a name an assertion can write", p.Name),
		}
	}

	if !slices.Contains(parameterTypes, p.Type) {
		return invalidCheckError{Check: check, Reason: fmt.Sprintf("declares the parameter %s as %q, which is no parameter type", p.Name, p.Type)}
	}

	if p.Description == "" {
		return invalidCheckError{Check: check, Reason: fmt.Sprintf("declares the parameter %s with no description", p.Name)}
	}

	if tolerant(p.Name) && p.Type != ParameterTolerance {
		return invalidCheckError{
			Check: check,
			Reason: fmt.Sprintf(
				"declares the parameter %s as %s: a tolerance is named from the registry and is never a literal",
				p.Name, p.Type,
			),
		}
	}

	return nil
}

// tolerant reports whether a parameter name says it carries a tolerance, which
// is the name itself or a name ending in it.
func tolerant(name string) bool {
	return name == string(SortTolerance) || strings.HasSuffix(name, "-"+string(SortTolerance))
}

// lookup returns the declaration of one check, and whether the set registers it.
func (s *checkSet) lookup(name string) (CheckDeclaration, bool) {
	declared, ok := s.byName[name]
	if !ok {
		return CheckDeclaration{}, false
	}
	return declared.clone(), true
}

// all returns every declaration the set holds, in name order.
func (s *checkSet) all() []CheckDeclaration {
	out := make([]CheckDeclaration, 0, len(s.names))
	for _, name := range s.names {
		out = append(out, s.byName[name].clone())
	}
	return out
}

// unknown is the diagnostic for a check name the set registers nothing under.
//
// It names the assertion, where it was written and the nearest registered name,
// because the overwhelmingly likely cause is a misspelling of a check which does
// exist. Where nothing is close enough to be one, the registered set is listed
// instead: the set is closed and small enough to print, and reading it is how
// somebody finds out that the check they wanted has to be added to the engine.
func (s *checkSet) unknown(tag, name string, span Span) Diagnostic {
	var hint string
	switch near, ok := nearest(name, s.names); {
	case ok:
		hint = fmt.Sprintf("did you mean %s?", near)
	case len(s.names) == 0:
		hint = "the engine registers no check at all"
	default:
		hint = "the check registry is closed and compiled into the engine; the registered checks are " + join(s.names, "and")
	}

	return Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message:  fmt.Sprintf("expected a registered check name after the %s tag, found %s, which the engine registers no check under", tag, name),
		Hint:     hint,
	}
}

// Checks returns the declaration of every check the engine registers, in name
// order.
//
// The registry is closed and compiled in, so this is the whole set for every
// model: two repositories reading the same engine can write the same
// assertions. It is what a command listing the available checks prints, and it
// carries each check's parameters rather than only its name, because a name
// with no parameters is not something anybody can write an assertion from.
func Checks() []CheckDeclaration { return registeredChecks.all() }

// LookupCheck returns the declaration of one check by name, and whether the
// engine registers one under it.
func LookupCheck(name string) (CheckDeclaration, bool) { return registeredChecks.lookup(name) }

// ValidateAssertion checks one written `assert` or `invariant` form against the
// check registry, returning one diagnostic for every problem it finds.
//
// This is the load-time half of the decision that an assertion is a name and its
// parameters: the check name is resolved, every parameter is matched to one the
// check declares, and every value is read as the sort of datum the check says it
// takes. What comes out is either diagnostics or the knowledge that the
// assertion is one the engine could run, and it costs nothing but reading the
// form — which is the whole point of there being no expression language
// ([0011](docs/decisions/0011-assertions-are-named-parameterised-checks.md)).
//
// registry is the model's own vocabulary, which the parameters naming a type, a
// predicate, a frame or a tolerance are resolved against. A nil registry
// declares nothing, and every such parameter is then reported as undeclared,
// which is what a model with no registry files genuinely is.
//
// An `invariant` is the same shape as an `assert` and is validated by the same
// call. Whether the check applies to the subject it was written on is a question
// about that subject rather than about the assertion, and is not asked here.
//
// One pass reports everything it finds. A parameter which is not one the check
// takes does not stop the ones after it from being read, and a check name which
// resolves to nothing stops only the parameters, because parameters cannot be
// judged against a check nobody named.
func ValidateAssertion(form *Node, registry *Registry) []Diagnostic {
	return validateAssertion(form, registry)
}

// validateAssertion is [ValidateAssertion] as the loaders call it.
func validateAssertion(form *Node, registry *Registry) []Diagnostic {
	v := &checkValidator{set: registeredChecks, registry: registry}
	v.assertion(form)
	return v.diags
}

// checkValidator collects the diagnostics of one written assertion.
type checkValidator struct {
	reader

	// set is the check registry the name is resolved against.
	set *checkSet

	// registry is the model's vocabulary, which the parameters naming an entry
	// of it are resolved against.
	registry *Registry
}

// assertion reads one written `assert` or `invariant`.
func (v *checkValidator) assertion(form *Node) {
	tag, ok := formTag(form)
	if !ok {
		// Something which is not a form at all is already reported as that by
		// the structural pass, which every loader runs before it interprets
		// anything.
		return
	}

	name, span, ok := v.name(form, "a check name")
	if !ok {
		return
	}

	declared, ok := v.set.lookup(name)
	if !ok {
		v.add(v.set.unknown(tag, name, span))
		return
	}

	v.parameters(form, declared)
}

// parameters checks what was written against what the check takes: every
// parameter written is one it declares, none is written twice, and none it
// requires is missing.
func (v *checkValidator) parameters(form *Node, declared CheckDeclaration) {
	written := make(map[string]Span, len(declared.Parameters))

	_, children := split(elements(form))
	for _, child := range children {
		name, ok := formTag(child)
		if !ok {
			continue
		}

		parameter, ok := declared.Parameter(name)
		if !ok {
			v.add(unknownParameter(declared, name, tagSpan(child)))
			continue
		}

		if first, repeated := written[name]; repeated {
			v.add(Diagnostic{
				Severity: SeverityError,
				Span:     tagSpan(child),
				Message: fmt.Sprintf(
					"expected one (%s ...) parameter of the check %s, found a second",
					name, declared.Name,
				),
				Hint: "a parameter takes every value it has at once; a parameter which may have more than one is written (" +
					name + " <value> <value>)",
				Related: []RelatedLocation{{Span: first, Message: "the first is written here"}},
			})
			continue
		}
		written[name] = tagSpan(child)

		v.values(child, parameter)
	}

	for _, parameter := range declared.Parameters {
		if !parameter.Required {
			continue
		}
		if _, ok := written[parameter.Name]; ok {
			continue
		}

		v.add(Diagnostic{
			Severity: SeverityError,
			Span:     form.Span,
			Message: fmt.Sprintf(
				"expected a (%s ...) parameter of the check %s, found none",
				parameter.Name, declared.Name,
			),
			Hint: fmt.Sprintf("%s takes %s: %s", declared.Name, parameter.Type.spelling(), parameter.Description),
		})
	}
}

// values reads the values written for one parameter.
//
// A repeated parameter is written either as a sequence of values after its tag
// or as one parenthesised list of them, and the two mean the same thing —
// specification section 6.8 permits both spellings, and a validator which
// accepted only one would reject a file the format allows.
func (v *checkValidator) values(param *Node, declared CheckParameter) {
	values := elements(param)

	if declared.Repeated && len(values) == 1 {
		if list, ok := values[0].Datum.(sexpr.List); ok && list.Tail == nil {
			values = values[0].Children
		}
	}

	switch {
	case len(values) == 0:
		v.add(Diagnostic{
			Severity: SeverityError,
			Span:     param.Span,
			Message: fmt.Sprintf(
				"expected %s after the %s tag, found none",
				v.wanted(declared), declared.Name,
			),
			Hint: declared.Description,
		})
		return

	case !declared.Repeated && len(values) > 1:
		// The parameter is not the mistake; the value after the one it takes
		// is, which is where the diagnostic points.
		v.add(Diagnostic{
			Severity: SeverityError,
			Span:     values[1].Span,
			Message: fmt.Sprintf(
				"expected %s after the %s tag, found %s",
				v.wanted(declared), declared.Name, count(len(values)),
			),
			Hint: declared.Description,
		})
		return
	}

	for _, value := range values {
		v.value(value, declared)
	}
}

// wanted spells how many of what a parameter takes, for a diagnostic about how
// many were written.
func (v *checkValidator) wanted(declared CheckParameter) string {
	if declared.Repeated {
		return "one or more values, each " + declared.Type.spelling()
	}
	return "one value, " + declared.Type.spelling()
}

// value reads one value of a parameter as the sort of datum the check declares
// it takes.
//
// A value naming a registry entry is resolved here, which is what makes an
// assertion's references checkable without running it. Whether an id names a
// thing the model holds is a question about the entities rather than about the
// vocabulary, and belongs to the pass which has read them.
func (v *checkValidator) value(node *Node, declared CheckParameter) {
	what := declared.Type.spelling()

	switch declared.Type {
	case ParameterReal:
		v.real(node, what)

	case ParameterString:
		v.text(node, what)

	case ParameterBoolean:
		v.boolean(node, what)

	case ParameterID:
		if id, ok := v.id(node, what); ok {
			v.registered(v.registry, id, node.Span)
		}

	case ParameterKind:
		if written, ok := v.symbol(node, what); ok && !slices.Contains(kinds, Kind(written)) {
			v.add(unknownKind(node.Span, written))
		}

	case ParameterGeometry:
		if written, ok := v.symbol(node, what); ok && !slices.Contains(geometries, Geometry(written)) {
			v.add(unknownGeometry(node.Span, written))
		}

	case ParameterFrame:
		if id, ok := v.id(node, what); ok && !v.registry.Declares(SortFrame, string(id)) {
			v.add(v.registry.Undeclared(SortFrame, string(id), node.Span))
		}

	case ParameterTolerance:
		v.tolerance(node, declared)

	default:
		sort, ok := declared.Type.sort()
		if !ok {
			return
		}
		if written, ok := v.symbol(node, what); ok && !v.registry.Declares(sort, written) {
			v.add(v.registry.Undeclared(sort, written, node.Span))
		}
	}
}

// tolerance reads a tolerance parameter, which names an entry of the tolerance
// registry and is never a number.
//
// The number written where the name belongs gets its own diagnostic because it
// is not a typing mistake and the generic one would read as though it were. It
// is somebody deciding how close is close enough at the point of use, which is
// the decision the registry exists to hold once
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)): written per
// assertion, the same question is answered a hundred times and no two answers
// can be compared.
func (v *checkValidator) tolerance(node *Node, declared CheckParameter) {
	switch node.Datum.(type) {
	case sexpr.Float, sexpr.Int:
		v.add(Diagnostic{
			Severity: SeverityError,
			Span:     node.Span,
			Message: fmt.Sprintf(
				"expected %s after the %s tag, found %s",
				ParameterTolerance.spelling(), declared.Name, describe(node),
			),
			Hint: "a tolerance is registry data rather than a number written where it is used: declare it with " +
				"(tolerance <name> (value <magnitude> <unit>)) and name it here, so that how close is close enough " +
				"is one decision in one place",
		})
		return
	}

	written, ok := v.symbol(node, ParameterTolerance.spelling())
	if !ok {
		return
	}

	if !v.registry.Declares(SortTolerance, written) {
		v.add(v.registry.Undeclared(SortTolerance, written, node.Span))
	}
}

// unknownParameter is the diagnostic for a parameter the check does not take.
//
// It names the parameter and the check, and offers the nearest parameter the
// check does take. A check with no parameters at all says so rather than
// offering an empty list, because "takes no parameter" is the answer and a list
// of none is not.
func unknownParameter(declared CheckDeclaration, name string, span Span) Diagnostic {
	names := declared.parameterNames()

	var hint string
	switch near, ok := nearest(name, names); {
	case ok:
		hint = fmt.Sprintf("did you mean (%s ...)?", near)
	case len(names) == 0:
		hint = fmt.Sprintf("%s takes no parameter", declared.Name)
	default:
		hint = fmt.Sprintf("%s takes %s", declared.Name, join(parenthesise(names), "and"))
	}

	return Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message: fmt.Sprintf(
			"expected a parameter of the check %s, found (%s ...), which is not one of them",
			declared.Name, name,
		),
		Hint: hint,
	}
}

// registeredChecks is the check registry: the closed set of checks the engine
// compiles in.
//
// Adding one is a line here and a type below, which is the review step the
// closed registry exists to be. A check which is not general enough to be
// useful to a model that does not share the requester's domain is domain
// vocabulary and belongs in the consuming repository's registry instead of here
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
var registeredChecks = newCheckSet(
	boundaryLoopsClose{},
	edgeEndpointsDiffer{},
	requiredClaim{},
	withinResolves{},
	zoneMembersResolve{},
)

// boundaryLoopsClose is the check that a loop is a closed cycle.
type boundaryLoopsClose struct{}

// Declare implements [Check].
func (boundaryLoopsClose) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name: "boundary-loops-close",
		Description: "Every loop bounding the subject closes: traversing its edges returns to the vertex it " +
			"started from, within the named tolerance.",
		Parameters: []CheckParameter{
			{
				Name:        "tolerance",
				Type:        ParameterTolerance,
				Required:    true,
				Description: "How far a loop may fail to close and still count as closed.",
			},
		},
		Forms:      []SubjectForm{SubjectNode, SubjectLoop},
		Geometries: []Geometry{GeometryArea, GeometrySurface, GeometrySolid},
	}
}

// edgeEndpointsDiffer is the check that an edge has an extent.
type edgeEndpointsDiffer struct{}

// Declare implements [Check].
func (edgeEndpointsDiffer) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name: "edge-endpoints-differ",
		Description: "The two endpoints an edge names are different vertices, so the edge has an extent and a " +
			"loop through it has a direction.",
		Forms: []SubjectForm{SubjectEdge},
	}
}

// requiredClaim is the check that a subject carries a claim under a predicate.
type requiredClaim struct{}

// Declare implements [Check].
func (requiredClaim) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name: "required-claim",
		Description: "The subject carries a claim under the named predicate which is still asserted, so the " +
			"predicate has a resolvable value on it.",
		Parameters: []CheckParameter{
			{
				Name:        "predicate",
				Type:        ParameterPredicate,
				Required:    true,
				Description: "The predicate a claim must be written under.",
			},
		},
		Forms: []SubjectForm{SubjectNode, SubjectVertex, SubjectEdge, SubjectLoop},
	}
}

// withinResolves is the check that a node's containment is a parent the
// hierarchy permits.
type withinResolves struct{}

// Declare implements [Check].
func (withinResolves) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name: "within-resolves",
		Description: "The node the subject is written within is one the model holds, and the containment " +
			"hierarchy permits it as a parent of the subject's kind.",
		Forms: []SubjectForm{SubjectNode},
	}
}

// zoneMembersResolve is the check that a node's zone memberships name zones.
type zoneMembersResolve struct{}

// Declare implements [Check].
func (zoneMembersResolve) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name: "zone-members-resolve",
		Description: "Every zone the subject is a member of is a node the model holds, and each of them is of " +
			"kind Zone.",
		Forms: []SubjectForm{SubjectNode},
	}
}
