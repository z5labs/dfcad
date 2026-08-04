// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"iter"
	"maps"
	"slices"
)

// Kind is one of the node kinds compiled into the engine.
//
// The set is closed and is fixed by specification section 1 rather than by
// registry data. It is compiled in because a kind is a structural axis every
// consuming repository shares — a thing either contains other things or it does
// not — and not vocabulary about any one subject.
type Kind string

// The kinds, in the order specification section 1 lists them.
const (
	KindZone      Kind = "Zone"
	KindSite      Kind = "Site"
	KindBuilding  Kind = "Building"
	KindStorey    Kind = "Storey"
	KindSpace     Kind = "Space"
	KindElement   Kind = "Element"
	KindInterface Kind = "Interface"
)

// kinds is the closed set, in specification order. A diagnostic listing it
// lists it in this order, which is the order somebody reading the
// specification met them in.
var kinds = []Kind{KindZone, KindSite, KindBuilding, KindStorey, KindSpace, KindElement, KindInterface}

// Kinds returns the closed set of node kinds, in specification order.
func Kinds() []Kind { return slices.Clone(kinds) }

// Geometry is one of the geometry forms compiled into the engine.
//
// The set is closed for the same reason [Kind] is. Absence is not a member: a
// node with no geometry omits the child, and only the type registry names
// absence, which is what [Type.Absent] records.
type Geometry string

// The geometry forms, in the order specification section 1 lists them.
const (
	GeometryPoint   Geometry = "point"
	GeometryLine    Geometry = "line"
	GeometryArea    Geometry = "area"
	GeometrySurface Geometry = "surface"
	GeometrySolid   Geometry = "solid"
)

// geometries is the closed set, in specification order.
var geometries = []Geometry{GeometryPoint, GeometryLine, GeometryArea, GeometrySurface, GeometrySolid}

// Geometries returns the closed set of geometry forms, in specification order.
func Geometries() []Geometry { return slices.Clone(geometries) }

// absentGeometry is how the type registry spells "an instance may omit its
// geometry child". It is legal in that one position and nowhere else, so it is
// not a [Geometry].
const absentGeometry = "absent"

// Shape is the shape a predicate's claim values take, per specification
// section 6.6.
type Shape string

// The value shapes, in the order specification section 6.6 lists them.
const (
	ShapeScalar     Shape = "scalar"
	ShapeCoordinate Shape = "coordinate"
	ShapeTransform  Shape = "transform"
	ShapeText       Shape = "text"
)

// shapes is the closed set, in specification order.
var shapes = []Shape{ShapeScalar, ShapeCoordinate, ShapeTransform, ShapeText}

// Shapes returns the closed set of value shapes, in specification order.
func Shapes() []Shape { return slices.Clone(shapes) }

// Unit is a unit name as it was written.
//
// The set of unit names and the definition of each are the engine's, not a
// registry's ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)): a
// model that could declare its own would be a model in which ft means whatever
// the last registry said. What is recorded here is the name a registry entry
// was written with; the arithmetic layer is what gives it a magnitude.
type Unit string

// Sort is which of the five registries an entry belongs to.
//
// It exists so that the questions every layer above asks — is this name
// declared, and what do I say when it is not — are one method each rather than
// five. The string value is the tag the entry is written with, which is what
// makes a diagnostic about it read as the form somebody wrote.
type Sort string

// The registries a name may be declared in. There is no sort for the project
// declaration: it declares no name and there is exactly one of it.
const (
	SortNamespace Sort = "namespace"
	SortType      Sort = "type"
	SortPredicate Sort = "predicate"
	SortFrame     Sort = "frame"
	SortTolerance Sort = "tolerance"
)

// plural is how a diagnostic names more than one entry of this sort. Every tag
// of the format pluralises with an s.
func (s Sort) plural() string { return string(s) + "s" }

// Project is the one project declaration a model carries, per specification
// section 7.1.
type Project struct {
	// Label is the project's name for a person reading it. Empty when it was
	// not written.
	Label string

	// GlobalIDNamespace is the URL every IFC GlobalId is derived from
	// ([0004](docs/decisions/0004-globalid-derives-from-a-pinned-namespace.md)).
	// Changing it re-identifies every node in the model.
	GlobalIDNamespace string

	// Description is free text. Empty when it was not written.
	Description string

	// Span is where the declaration was written.
	Span Span
}

// Namespace is one declared id namespace, per specification section 7.2.
type Namespace struct {
	// Name is the namespace itself — the part of an id before the first colon.
	Name string

	// Description says what authority issues these ids. Nothing in the engine
	// reads it ([0003](docs/decisions/0003-id-namespaces-are-a-closed-registry.md)).
	Description string

	// Span is where the declaration was written.
	Span Span
}

// Type is one declared node type, per specification section 7.3.
//
// The engine attaches no meaning to a type beyond the axes recorded here. It
// does not know that one type is a wall and another a parcel, and no behaviour
// anywhere depends on which
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
type Type struct {
	// Name is the type name, which is a plain symbol and not an id.
	Name string

	// Kinds are the kinds an instance may declare, in the order they were
	// written.
	Kinds []Kind

	// Geometries are the geometry forms an instance may declare, in the order
	// they were written.
	Geometries []Geometry

	// Absent reports whether an instance may omit its geometry child, which is
	// the registry's `absent`.
	Absent bool

	// Description is the one line the registry gives the type.
	Description string

	// Invariants are the checks which apply to every instance.
	Invariants []Invariant

	// Span is where the declaration was written.
	Span Span
}

// PermitsKind reports whether an instance of the type may declare kind.
func (t Type) PermitsKind(kind Kind) bool { return slices.Contains(t.Kinds, kind) }

// PermitsGeometry reports whether an instance of the type may declare geometry.
func (t Type) PermitsGeometry(geometry Geometry) bool {
	return slices.Contains(t.Geometries, geometry)
}

// Invariant is a check the type registry applies to every instance of a type,
// including instances added later.
//
// The check name and the parameters are the check registry's vocabulary rather
// than this layer's, so what is recorded is what was written and where. The
// layer holding that registry is what decides whether the check exists and
// whether these are its parameters.
type Invariant struct {
	// Check is the check name written after the invariant tag.
	Check string

	// Parameters are the parameter forms written after it, unmodified.
	Parameters []*Node

	// Span is where the invariant was written.
	Span Span
}

// Predicate is one declared claim predicate, per specification section 7.4.
type Predicate struct {
	// Name is the predicate name, which is also the tag a claim under it is
	// written with.
	Name string

	// Unit is the unit its values are expressed in. Empty means the predicate
	// is non-dimensional.
	Unit Unit

	// Shape is the shape its values take.
	Shape Shape

	// Dimension is how many components a coordinate value has. Zero for every
	// other shape.
	Dimension int

	// ClaimBearing reports whether the predicate takes a claim rather than a
	// plain value. It defaults to true, so opting out is something somebody
	// writes down and a reviewer reads.
	ClaimBearing bool

	// Strict reports whether an ambiguous resolution of this predicate is a
	// failure rather than a report. It defaults to false.
	Strict bool

	// Description is free text. Empty when it was not written.
	Description string

	// Span is where the declaration was written.
	Span Span
}

// Frame is one declared coordinate frame, per specification section 7.5.
//
// A frame is both a registry entry and a node: its unit and its parent are
// vocabulary the consuming repository owns, and it carries an id and claims
// because the relationship between two frames is a measurement rather than a
// configuration constant.
type Frame struct {
	// ID is the frame's id, which is why a frame is the one registry entry
	// named by an id rather than by a plain symbol.
	ID string

	// Label is the frame's name for a person reading it. Empty when it was not
	// written.
	Label string

	// Unit is the frame's one linear unit
	// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
	Unit Unit

	// Parent is the id of the frame this one is expressed relative to. Empty
	// for the root frame.
	Parent string

	// Transform is the id of the claim holding the transform to the parent.
	// Empty for the root frame.
	Transform string

	// Claims are the claim forms written on the frame, unmodified. Which
	// predicate each is under, and whether its value is what the predicate
	// declares, is the claim layer's question and not this one's.
	Claims []*Node

	// Span is where the declaration was written.
	Span Span
}

// Tolerance is one declared named tolerance, per specification section 7.6.
//
// No numeric literal tolerance appears in engine code and none appears in an
// assertion: an operation that needs one takes a name and reports which name it
// used ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
type Tolerance struct {
	// Name is the tolerance name.
	Name string

	// Value is its magnitude.
	Value float64

	// Unit is the unit that magnitude is in.
	Unit Unit

	// Description is free text. Empty when it was not written.
	Description string

	// Span is where the declaration was written.
	Span Span
}

// Registry is the vocabulary one consuming repository declares: its types,
// claim predicates, frames, id namespaces and tolerances, plus the one project
// declaration.
//
// It is the whole registry set of a model rather than one file's worth, and it
// is queryable as a whole so that every layer above validates against it
// instead of reading registry files again for itself. A second reading would be
// a second answer to "is this type declared", and the two would disagree the
// first time a file moved.
//
// A Registry is read-only once loaded. The zero value declares nothing, which
// is the same thing an empty source tree yields, and every query below works on
// it.
type Registry struct {
	project    *Project
	namespaces map[string]Namespace
	types      map[string]Type
	predicates map[string]Predicate
	frames     map[string]Frame
	tolerances map[string]Tolerance
}

// Project returns the model's project declaration, and whether it has one.
func (r *Registry) Project() (Project, bool) {
	if r == nil || r.project == nil {
		return Project{}, false
	}
	return *r.project, true
}

// Namespace returns the declaration of an id namespace, and whether it is
// declared.
//
// Every query below tolerates a nil registry, answering that nothing is
// declared. A caller which did not read the diagnostics of a load which failed
// outright is holding one, and "nothing is declared" is both true of it and the
// answer which produces a diagnostic rather than a panic.
func (r *Registry) Namespace(name string) (Namespace, bool) {
	if r == nil {
		return Namespace{}, false
	}
	entry, ok := r.namespaces[name]
	return entry, ok
}

// Type returns the declaration of a node type, and whether it is declared.
func (r *Registry) Type(name string) (Type, bool) {
	if r == nil {
		return Type{}, false
	}
	entry, ok := r.types[name]
	return entry, ok
}

// Predicate returns the declaration of a claim predicate, and whether it is
// declared.
func (r *Registry) Predicate(name string) (Predicate, bool) {
	if r == nil {
		return Predicate{}, false
	}
	entry, ok := r.predicates[name]
	return entry, ok
}

// Frame returns the declaration of a frame, and whether it is declared.
func (r *Registry) Frame(id string) (Frame, bool) {
	if r == nil {
		return Frame{}, false
	}
	entry, ok := r.frames[id]
	return entry, ok
}

// Tolerance returns the declaration of a named tolerance, and whether it is
// declared.
func (r *Registry) Tolerance(name string) (Tolerance, bool) {
	if r == nil {
		return Tolerance{}, false
	}
	entry, ok := r.tolerances[name]
	return entry, ok
}

// Namespaces iterates the declared id namespaces in name order.
//
// Every iterator here is ordered rather than in map order, because output built
// from one is meant to diff against the last run's.
func (r *Registry) Namespaces() iter.Seq[Namespace] {
	if r == nil {
		return ordered[Namespace](nil)
	}
	return ordered(r.namespaces)
}

// Types iterates the declared node types in name order.
func (r *Registry) Types() iter.Seq[Type] {
	if r == nil {
		return ordered[Type](nil)
	}
	return ordered(r.types)
}

// Predicates iterates the declared claim predicates in name order.
func (r *Registry) Predicates() iter.Seq[Predicate] {
	if r == nil {
		return ordered[Predicate](nil)
	}
	return ordered(r.predicates)
}

// Frames iterates the declared frames in id order.
func (r *Registry) Frames() iter.Seq[Frame] {
	if r == nil {
		return ordered[Frame](nil)
	}
	return ordered(r.frames)
}

// Tolerances iterates the declared named tolerances in name order.
func (r *Registry) Tolerances() iter.Seq[Tolerance] {
	if r == nil {
		return ordered[Tolerance](nil)
	}
	return ordered(r.tolerances)
}

// ordered iterates the entries of one registry in key order.
func ordered[T any](entries map[string]T) iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, name := range slices.Sorted(maps.Keys(entries)) {
			if !yield(entries[name]) {
				return
			}
		}
	}
}

// Declares reports whether the registry declares name in the given sort.
//
// It is the question every layer above asks of a name it did not mint — a
// node's type, a claim's predicate, an id's namespace, a check's tolerance
// parameter — and it is one method rather than five so that a caller holding a
// [Sort] does not switch on it. An unknown sort declares nothing.
func (r *Registry) Declares(sort Sort, name string) bool {
	if r == nil {
		return false
	}

	switch sort {
	case SortNamespace:
		_, ok := r.namespaces[name]
		return ok
	case SortType:
		_, ok := r.types[name]
		return ok
	case SortPredicate:
		_, ok := r.predicates[name]
		return ok
	case SortFrame:
		_, ok := r.frames[name]
		return ok
	case SortTolerance:
		_, ok := r.tolerances[name]
		return ok
	}
	return false
}

// Names returns every name declared in the given sort, in order. An unknown
// sort declares nothing.
func (r *Registry) Names(sort Sort) []string {
	if r == nil {
		return nil
	}

	switch sort {
	case SortNamespace:
		return slices.Sorted(maps.Keys(r.namespaces))
	case SortType:
		return slices.Sorted(maps.Keys(r.types))
	case SortPredicate:
		return slices.Sorted(maps.Keys(r.predicates))
	case SortFrame:
		return slices.Sorted(maps.Keys(r.frames))
	case SortTolerance:
		return slices.Sorted(maps.Keys(r.tolerances))
	}
	return nil
}

// Undeclared is the diagnostic for a name written where the registry declares
// no entry of that sort.
//
// It lives here rather than at each call site because the answer to "there is
// no such type" is the same wherever the type was written, and because only the
// registry can say what is declared instead. The hint offers the nearest
// declared name when one is close enough to be a misspelling of what was
// written, and otherwise lists the declared set, which is what makes the
// diagnostic actionable on an empty registry: the set is empty, and saying so
// is the whole of the answer.
func (r *Registry) Undeclared(sort Sort, name string, span Span) Diagnostic {
	declared := r.Names(sort)

	var hint string
	switch near, ok := nearest(name, declared); {
	case ok:
		hint = fmt.Sprintf("did you mean %s?", near)
	case len(declared) == 0:
		hint = fmt.Sprintf("no %s is declared; a registry file declares one with (%s ...)", sort, sort)
	default:
		hint = fmt.Sprintf("the declared %s are %s", sort.plural(), join(declared, "and"))
	}

	return Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message:  fmt.Sprintf("expected a declared %s, found %s, which no registry file declares", sort, name),
		Hint:     hint,
	}
}
