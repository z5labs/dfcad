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
	"strings"
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

// unknownKind is the diagnostic for a symbol written where a kind belongs.
//
// It lives beside the set rather than at each call site because the answer to
// "that is not a kind" is the same on a type declaration and on the node which
// declares that type, and because the permitted set is listed in specification
// order — the order somebody reading the specification met them in — which only
// this file knows.
func unknownKind(span Span, written string) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message:  fmt.Sprintf("expected a kind, found %s", written),
		Hint:     "a kind is one of " + join(spellings(kinds), "and"),
	}
}

// unknownGeometry is the diagnostic for a symbol written where one of the five
// geometry forms belongs, which is every position but a type declaration.
//
// `absent` gets its own hint because writing it on a node is not a misspelling
// of anything: it is the one legal spelling of absence in the other position,
// and the fix is to delete the child rather than to correct the word.
func unknownGeometry(span Span, written string) Diagnostic {
	hint := "a geometry form is one of " + join(spellings(geometries), "and")
	if written == absentGeometry {
		hint = "absent is written only in a type declaration; a node with no geometry omits the (geometry ...) child"
	}

	return Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message:  fmt.Sprintf("expected a geometry form, found %s", written),
		Hint:     hint,
	}
}

// unknownGeometryOrAbsent is the diagnostic for a symbol written where a type
// declaration permits a geometry form or `absent`.
func unknownGeometryOrAbsent(span Span, written string) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message:  fmt.Sprintf("expected a geometry form or absent, found %s", written),
		Hint:     "a geometry form is one of " + join(spellings(geometries), "and") + ", and absent permits an instance to omit the child",
	}
}

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

// The linear units the engine defines, in the order the table below pins them:
// the metre and the two prefixed spellings of it a model is authored in, then
// the two feet.
const (
	UnitMillimetre Unit = "mm"
	UnitCentimetre Unit = "cm"
	UnitMetre      Unit = "m"
	UnitKilometre  Unit = "km"
	UnitFoot       Unit = "ft"
	UnitSurveyFoot Unit = "usft"
)

// linearUnits is the closed set, in the order above.
var linearUnits = []Unit{UnitMillimetre, UnitCentimetre, UnitMetre, UnitKilometre, UnitFoot, UnitSurveyFoot}

// metres is how long one of each linear unit is, in metres.
//
// Two of them are pinned by
// [0005](docs/decisions/0005-one-linear-unit-per-frame.md) and are load
// bearing: `ft` is the international foot, exactly 0.3048 m, and `usft` is the
// US survey foot, exactly 1200/3937 m. They differ by two parts per million,
// which is invisible on a room and is four feet on a state plane coordinate, so
// `usft` is never a synonym for `ft` in any position under any registry.
var metres = map[Unit]float64{
	UnitMillimetre: 0.001,
	UnitCentimetre: 0.01,
	UnitMetre:      1,
	UnitKilometre:  1000,
	UnitFoot:       0.3048,
	UnitSurveyFoot: 1200.0 / 3937.0,
}

// LinearUnits returns the closed set of linear units, in the order above.
func LinearUnits() []Unit { return slices.Clone(linearUnits) }

// Metres returns how long one of the unit is in metres, and whether it is a
// linear unit the engine defines.
//
// It answers for the linear units and for nothing else. A predicate may declare
// a unit of any quantity its consuming repository measures — an angle, a mass, a
// temperature — and the set of those is not the engine's to close; the set of
// linear units is, because a frame declares one and every cross-frame answer is
// computed through it.
func (u Unit) Metres() (float64, bool) {
	length, ok := metres[u]
	return length, ok
}

// unknownUnit is the diagnostic for a symbol written where a frame's linear
// unit belongs.
//
// It lives beside the set for the reason [unknownKind] does, and it says where
// the set comes from because that is the thing somebody reaching for a unit the
// engine does not define needs to hear: there is no unit registry, and a unit is
// arithmetic rather than vocabulary a model declares.
func unknownUnit(span Span, written string) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message:  fmt.Sprintf("expected a linear unit, found %s", written),
		Hint: "a linear unit is one of " + join(spellings(linearUnits), "and") +
			"; the set and the definition of each are the engine's, and there is no unit registry",
	}
}

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
	SortRoute     Sort = "route"
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
	// ([0004](docs/decisions/0004-globalid-derives-from-a-pinned-namespace.md)),
	// by [DeriveGlobalID]. Changing it re-identifies every node in the model:
	// every downstream system holding a previously exported identifier sees the
	// whole model deleted and re-created, and the old values can only be
	// recomputed from the old URL.
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

// permittedGeometry spells what the type allows in the geometry position, for a
// diagnostic which lists it.
//
// Absence is listed as a phrase rather than as the word `absent`, because
// `absent` is not something a node may be corrected to write: a node with no
// geometry omits the child.
func (t Type) permittedGeometry() string {
	permitted := spellings(t.Geometries)
	if t.Absent {
		permitted = append(permitted, "no geometry at all")
	}
	if len(permitted) == 0 {
		return "no geometry form"
	}
	return join(permitted, "and")
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
	ID ID

	// Label is the frame's name for a person reading it. Empty when it was not
	// written.
	Label string

	// Unit is the frame's one linear unit
	// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
	Unit Unit

	// Parent is the id of the frame this one is expressed relative to. Empty
	// for the root frame.
	Parent ID

	// Transform is the id of the claim holding the transform to the parent.
	// Empty for the root frame.
	Transform ID

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

// Route is one declared file routing rule, per specification section 7.7.
//
// A routing rule answers the question a write path has to answer and a read
// path never asks: which file does a newly authored node go in. Left to each
// author it becomes "wherever the last person put things", which is how one
// category of thing ends up spread over six files.
//
// The three criteria are the axes a new node has before it has anything else:
// the namespace of its id, its kind and its type. A criterion left out matches
// anything, so a rule written with none of them matches every node. Which
// criteria are worth writing is the consuming repository's judgement and not
// the engine's ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
type Route struct {
	// Name is the rule name, which is a plain symbol and not an id. It is what a
	// report of where a node went, and a refusal to route one, name the rule by.
	Name string

	// Namespace is the id namespace an instance must carry. Empty matches any.
	Namespace string

	// Kind is the kind an instance must declare. Empty matches any.
	Kind Kind

	// Type is the type name an instance must declare. Empty matches any.
	Type string

	// File is the target file, relative to the model root and written with
	// forward slashes.
	File string

	// Description is free text. Empty when it was not written.
	Description string

	// Span is where the declaration was written.
	Span Span

	// unusable marks a rule something was written into which the loader could
	// not read — a kind which is not one, a file which is not a path a node may
	// be written to — and which was reported as such.
	//
	// It is a field rather than an absence because the two are not the same
	// thing here, and reading them as the same is how a mistake becomes a wrong
	// answer instead of a diagnostic. An unwritten criterion matches anything,
	// so a criterion which was written and could not be read would, dropped,
	// widen the rule to match everything that axis was there to exclude. A rule
	// like that is not a narrower rule than the author meant: it is a rule which
	// files nodes somewhere nobody asked for, and the registry has already been
	// told so.
	unusable bool
}

// Matches reports whether a new node with these axes satisfies every criterion
// the rule writes.
//
// A criterion the rule leaves out is not a criterion: it matches anything. That
// is what lets a registry say "every Space goes here" without also having to
// enumerate the types which are spaces.
//
// A rule the loader could not read matches nothing at all, whatever it says.
// The diagnostic about it is the answer; routing through it as though the parts
// which failed to read had been left out would turn one reported mistake into a
// node filed somewhere nothing points at.
func (r Route) Matches(subject Subject) bool {
	if r.unusable {
		return false
	}
	if r.Namespace != "" && r.Namespace != subject.ID.Namespace() {
		return false
	}
	if r.Kind != "" && r.Kind != subject.Kind {
		return false
	}
	if r.Type != "" && r.Type != subject.Type {
		return false
	}
	return true
}

// criteria spells what the rule matches on, for a message which lists the rules
// consulted. A rule with no criteria says so rather than reading as a rule with
// nothing written after its name.
func (r Route) criteria() string {
	var written []string
	if r.Namespace != "" {
		written = append(written, "namespace "+r.Namespace)
	}
	if r.Kind != "" {
		written = append(written, "kind "+string(r.Kind))
	}
	if r.Type != "" {
		written = append(written, "type "+r.Type)
	}
	if len(written) == 0 {
		return "any node"
	}
	return strings.Join(written, ", ")
}

// Registry is the vocabulary one consuming repository declares: its types,
// claim predicates, frames, id namespaces, tolerances and file routing rules,
// plus the one project declaration.
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
	frames     map[ID]Frame
	tolerances map[string]Tolerance
	routes     map[string]Route
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
func (r *Registry) Frame(id ID) (Frame, bool) {
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

// Route returns the declaration of a file routing rule, and whether it is
// declared.
func (r *Registry) Route(name string) (Route, bool) {
	if r == nil {
		return Route{}, false
	}
	entry, ok := r.routes[name]
	return entry, ok
}

// Namespaces iterates the declared id namespaces in name order.
//
// Every iterator here is ordered rather than in map order, because output built
// from one is meant to diff against the last run's.
func (r *Registry) Namespaces() iter.Seq[Namespace] {
	if r == nil {
		return ordered[string, Namespace](nil)
	}
	return ordered(r.namespaces)
}

// Types iterates the declared node types in name order.
func (r *Registry) Types() iter.Seq[Type] {
	if r == nil {
		return ordered[string, Type](nil)
	}
	return ordered(r.types)
}

// Predicates iterates the declared claim predicates in name order.
func (r *Registry) Predicates() iter.Seq[Predicate] {
	if r == nil {
		return ordered[string, Predicate](nil)
	}
	return ordered(r.predicates)
}

// Frames iterates the declared frames in id order.
func (r *Registry) Frames() iter.Seq[Frame] {
	if r == nil {
		return ordered[ID, Frame](nil)
	}
	return ordered(r.frames)
}

// Tolerances iterates the declared named tolerances in name order.
func (r *Registry) Tolerances() iter.Seq[Tolerance] {
	if r == nil {
		return ordered[string, Tolerance](nil)
	}
	return ordered(r.tolerances)
}

// Routes iterates the declared file routing rules in name order.
//
// Name order rather than the order they were written, for the reason every
// other iterator here is ordered: a rule which matches is chosen by matching
// and not by position, so nothing about routing depends on the order, and a
// listing which did would change when a registry file moved.
func (r *Registry) Routes() iter.Seq[Route] {
	if r == nil {
		return ordered[string, Route](nil)
	}
	return ordered(r.routes)
}

// ordered iterates the entries of one registry in key order.
//
// The key type is a parameter because the five registries do not share one: a
// frame is keyed by its [ID] and the other four by a plain name. Both have a
// string underneath, which is what the constraint says and all this needs.
func ordered[K ~string, T any](entries map[K]T) iter.Seq[T] {
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
		_, ok := r.frames[ID(name)]
		return ok
	case SortTolerance:
		_, ok := r.tolerances[name]
		return ok
	case SortRoute:
		_, ok := r.routes[name]
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
		// A frame is the one registry entry named by an id, so its key needs
		// spelling out where the other four are already strings.
		return spellings(slices.Sorted(maps.Keys(r.frames)))
	case SortTolerance:
		return slices.Sorted(maps.Keys(r.tolerances))
	case SortRoute:
		return slices.Sorted(maps.Keys(r.routes))
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
