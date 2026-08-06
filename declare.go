// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"iter"
	"net/url"
	"path"
	"slices"
	"strings"

	sexpr "github.com/z5labs/sexpr-go"
)

// projectTag is the one registry form which declares no name.
const projectTag = "project"

// registrySorts maps the top-level tag of a registry form to the registry it
// declares an entry in.
//
// It is written down rather than derived because it is the one fact the form
// tables do not hold: they say what a `type` form may contain and not that a
// `type` form is registry data at all. A tag absent from it is an entity form,
// which this pass walks past — registries resolve before any entity is
// interpreted, and interpreting one here would invert that.
var registrySorts = map[string]Sort{
	"namespace": SortNamespace,
	"type":      SortType,
	"predicate": SortPredicate,
	"frame":     SortFrame,
	"tolerance": SortTolerance,
	"route":     SortRoute,
}

// LoadRegistry reads every registry form beneath root into one registry.
//
// root is what [Load] takes: a single file, or a directory beneath which every
// file whose extension is [Extension] is read. Every file is read, because a
// registry is a property of the whole source tree and not of any one file in
// it: a type declared in one file and a frame naming it in another are one
// declaration and one reference, and a loader reading files independently could
// not say so.
//
// Loading is one pass which reports everything it finds. A file which does not
// parse, a form which is structurally wrong, a value of the wrong sort, a name
// declared twice and a reference to something undeclared are each a diagnostic,
// and none of them stops the rest of the tree from being read. Fixing a
// registry one diagnostic at a time is a guessing loop.
//
// The registry which comes back is always usable, whatever the diagnostics say.
// A tree holding no registry form at all yields an empty registry rather than a
// nil one, so every layer above answers "is this declared" with "no" rather
// than crashing on the model whose registry is the thing that has not been
// written yet.
//
// Diagnostics come back in the order the pass found them. Collecting them into
// a [Diagnostics] is what puts them in reporting order.
func LoadRegistry(root string) (*Registry, []Diagnostic) {
	return loadRegistry(root, readTree(root))
}

// loadRegistry is [LoadRegistry] over a tree somebody else read, which is what
// lets [LoadGraph] read the files once and interpret them four times.
func loadRegistry(root string, sources iter.Seq[source]) (*Registry, []Diagnostic) {
	l := &registryLoader{
		root: root,
		registry: &Registry{
			namespaces: make(map[string]Namespace),
			types:      make(map[string]Type),
			predicates: make(map[string]Predicate),
			frames:     make(map[ID]Frame),
			tolerances: make(map[string]Tolerance),
			routes:     make(map[string]Route),
		},
	}

	interpret(&l.reader, sources, l.file)

	l.resolve()

	return l.registry, l.diags
}

// registryLoader collects one load of a registry set.
type registryLoader struct {
	reader

	// root is what the load was asked for, which is where a diagnostic about
	// the model as a whole rather than about any one file points.
	root string

	// registry is what has been declared so far.
	registry *Registry

	// frames are the accepted frame declarations in the order they were read,
	// with the spans a cross-reference diagnostic points at. Frames are the one
	// registry whose entries refer to each other, so they are the one whose
	// declarations outlive the file they were read from.
	frames []*frameDeclaration

	// routes are the accepted routing rules in the order they were read, with
	// the spans of the names they borrow from the other registries.
	routes []*routeDeclaration

	// invariants are the invariant forms of the accepted type declarations, in
	// the order they were read.
	//
	// They are checked against the check registry once every file has been
	// read, for the reason a frame's parent is: an invariant naming a tolerance
	// declared in the last file the walk reaches is as declared as one naming a
	// tolerance in the first, and a loader which resolved as it read would
	// report it undeclared for no reason but the order the directory happened
	// to be listed in.
	invariants []typeInvariant
}

// typeInvariant is one written invariant together with the name of the type it
// was written on.
//
// The type comes along because whether the check it names can apply to anything
// is a question about that type's kinds and geometry forms, and a `kind` or a
// `geometry` written after the invariant is as declared as one written before
// it. The name rather than the declaration, because the declaration is not
// complete until its form has been read to the end.
type typeInvariant struct {
	// declaredType is the name of the type the invariant was written on.
	declaredType string

	// form is the invariant form as it was written.
	form *Node
}

// frameDeclaration is one accepted frame together with where the parts a later
// pass has something to say about were written.
type frameDeclaration struct {
	frame Frame

	// id, parent and transform are the spans of those three ids as written. A
	// span left zero belongs to something which was not written.
	id        Span
	parent    Span
	transform Span
}

// routeDeclaration is one accepted routing rule together with where the names
// it borrows from the other registries were written.
type routeDeclaration struct {
	route Route

	// namespace and declaredType are the spans of those two criteria as
	// written. A span left zero belongs to a criterion which was not written,
	// which matches anything and refers to nothing.
	namespace    Span
	declaredType Span
}

// file interprets the registry forms of one loaded file.
//
// A form is validated structurally before anything reads it, and one which is
// structurally wrong is not interpreted at all. Reading a `predicate` which is
// missing its `shape` would mean either inventing a shape or reporting its
// absence a second time, and both are worse than the diagnostic the structural
// pass already produced.
func (l *registryLoader) file(file *File) {
	for _, node := range file.Nodes {
		tag, ok := formTag(node)
		if !ok {
			continue
		}

		sort, isRegistry := registrySorts[tag]
		if !isRegistry && tag != projectTag {
			continue
		}

		if diags := Validate(&File{Path: file.Path, Nodes: []*Node{node}}); len(diags) > 0 {
			l.add(diags...)
			continue
		}

		if tag == projectTag {
			l.declareProject(node)
			continue
		}

		switch sort {
		case SortNamespace:
			l.declareNamespace(node)
		case SortType:
			l.declareType(node)
		case SortPredicate:
			l.declarePredicate(node)
		case SortFrame:
			l.declareFrame(node)
		case SortTolerance:
			l.declareTolerance(node)
		case SortRoute:
			l.declareRoute(node)
		}
	}
}

// declareProject reads specification section 7.1.
func (l *registryLoader) declareProject(node *Node) {
	project := Project{Span: node.Span}

	if arg, ok := argumentOf(node, "label"); ok {
		project.Label, _ = l.text(arg, "a string")
	}

	if arg, ok := argumentOf(node, "globalid-namespace"); ok {
		if namespace, ok := l.text(arg, "a string holding a URL"); ok && l.isURL(arg, namespace) {
			project.GlobalIDNamespace = namespace
		}
	}

	if arg, ok := argumentOf(node, "description"); ok {
		project.Description, _ = l.text(arg, "a string")
	}

	if l.registry.project != nil {
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     node.Span,
			Message:  "expected one project declaration for the model, found another",
			Hint:     "a model declares its project once, in one registry file, however many files it is spread over",
			Related:  []RelatedLocation{{Span: l.registry.project.Span, Message: "first declared here"}},
		})
		return
	}

	l.registry.project = &project
}

// isURL reports whether a globalid-namespace is one, adding the diagnostic when
// it is not.
//
// It has to be absolute because the namespace UUID every IFC GlobalId derives
// from is UUIDv5 of exactly these bytes
// ([0004](docs/decisions/0004-globalid-derives-from-a-pinned-namespace.md)). A
// relative reference resolves against something, and there is nothing here for
// it to resolve against.
func (l *registryLoader) isURL(node *Node, raw string) bool {
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return true
	}

	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     node.Span,
		Message:  fmt.Sprintf("expected an absolute URL, found %q", raw),
		Hint:     "every GlobalId derives from this URL, so it names something the project controls and does not change",
	})
	return false
}

// declareNamespace reads specification section 7.2.
func (l *registryLoader) declareNamespace(node *Node) {
	name, span, ok := l.name(node, "a namespace")
	if !ok {
		return
	}

	if !wellFormedNamespace(name) {
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     span,
			Message:  fmt.Sprintf("expected a namespace, found %s", name),
			Hint:     "a namespace is ASCII, begins with a letter, and continues with letters, digits, hyphens and underscores",
		})
		return
	}

	namespace := Namespace{Name: name, Span: span}
	if arg, ok := argumentOf(node, "description"); ok {
		namespace.Description, _ = l.text(arg, "a string")
	}

	if existing, ok := l.registry.namespaces[name]; ok {
		l.duplicate(SortNamespace, name, span, existing.Span)
		return
	}

	l.registry.namespaces[name] = namespace
}

// declareType reads specification section 7.3.
func (l *registryLoader) declareType(node *Node) {
	name, span, ok := l.name(node, "a type name")
	if !ok {
		return
	}

	declared := Type{Name: name, Span: span}

	seen := make(map[string]Span)
	for _, child := range childForms(node, "kind") {
		arg, ok := argument(child, 0)
		if !ok {
			continue
		}

		written, ok := l.symbol(arg, "a kind")
		if !ok {
			continue
		}

		kind := Kind(written)
		if !slices.Contains(kinds, kind) {
			l.add(unknownKind(arg.Span, written))
			continue
		}

		if first, ok := seen[written]; ok {
			l.repeated(arg.Span, "a kind", written, first)
			continue
		}

		seen[written] = arg.Span
		declared.Kinds = append(declared.Kinds, kind)
	}

	seen = make(map[string]Span)
	for _, child := range childForms(node, "geometry") {
		arg, ok := argument(child, 0)
		if !ok {
			continue
		}

		written, ok := l.symbol(arg, "a geometry form or absent")
		if !ok {
			continue
		}

		geometry := Geometry(written)
		if written != absentGeometry && !slices.Contains(geometries, geometry) {
			l.add(unknownGeometryOrAbsent(arg.Span, written))
			continue
		}

		if first, ok := seen[written]; ok {
			l.repeated(arg.Span, "a geometry form", written, first)
			continue
		}

		seen[written] = arg.Span
		if written == absentGeometry {
			declared.Absent = true
			continue
		}
		declared.Geometries = append(declared.Geometries, geometry)
	}

	if arg, ok := argumentOf(node, "description"); ok {
		declared.Description, _ = l.text(arg, "a string")
	}

	l.classify(&declared, node)

	var invariants []typeInvariant
	for _, child := range childForms(node, "invariant") {
		check, _, ok := l.name(child, "a check name")
		if !ok {
			continue
		}

		_, parameters := split(elements(child))
		declared.Invariants = append(declared.Invariants, Invariant{
			Check:      check,
			Parameters: parameters,
			Span:       child.Span,
		})

		invariants = append(invariants, typeInvariant{declaredType: name, form: child})
	}

	if existing, ok := l.registry.types[name]; ok {
		l.duplicate(SortType, name, span, existing.Span)
		return
	}

	l.registry.types[name] = declared
	l.invariants = append(l.invariants, invariants...)
}

// classificationChild is the tag a type's external classification is written
// with, per specification section 7.3.
const classificationChild = "classification"

// classify reads the `classification` children of a type declaration.
//
// Everything checked here is structure: that both halves of the pair were
// written as strings, that neither is blank, and that no system is given twice.
// Nothing is checked about what either string says. There is no list of known
// systems and no syntax a code is held to, because a system the engine had an
// opinion about would be a scheme compiled into it, which is the table this
// child exists to keep out of the engine
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
func (l *registryLoader) classify(declared *Type, node *Node) {
	systems := make(map[string]Span)

	for _, child := range childForms(node, classificationChild) {
		system, ok := l.classificationField(child, 0, "a classification system")
		if !ok {
			continue
		}

		code, ok := l.classificationField(child, 1, "a classification code")
		if !ok {
			continue
		}

		if first, written := systems[system]; written {
			l.reclassified(child.Span, system, first)
			continue
		}

		systems[system] = child.Span
		declared.Classifications = append(declared.Classifications, ExternalClassification{
			System: system,
			Code:   code,
			Span:   child.Span,
		})
	}
}

// classificationField reads one string argument of a classification, refusing a
// blank one.
//
// A blank half is refused rather than carried because the child's whole content
// is the pair: a classification with no system names no scheme and still takes a
// scheme's place in the uniqueness rule, and one with no code says that the type
// is in a scheme without saying what it is called there. Neither is a mapping
// anybody can act on, and both read as an unfinished edit.
func (l *registryLoader) classificationField(child *Node, index int, what string) (string, bool) {
	arg, ok := argument(child, index)
	if !ok {
		return "", false
	}

	written, ok := l.text(arg, what)
	if !ok {
		return "", false
	}

	if written == "" {
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     arg.Span,
			Message:  fmt.Sprintf("expected %s, found an empty string", what),
			Hint:     "a classification is a system and a code, and neither half is optional",
		})
		return "", false
	}

	return written, true
}

// reclassified reports a second classification in a system the type is already
// classified in, naming both.
func (l *registryLoader) reclassified(span Span, system string, first Span) {
	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message: fmt.Sprintf(
			"expected a classification system this type is not already classified in, found %q",
			system,
		),
		Hint:    "a type carries at most one code per system; several systems on one type is the ordinary case",
		Related: []RelatedLocation{{Span: first, Message: "first classified in this system here"}},
	})
}

// declarePredicate reads specification section 7.4.
func (l *registryLoader) declarePredicate(node *Node) {
	name, span, ok := l.name(node, "a predicate name")
	if !ok {
		return
	}

	// A claim is written under its predicate's name, in the same position a
	// structural child of the form carrying it is written, so the two sets
	// cannot overlap. The reserved set is the one the form tables produce
	// rather than a second copy of it: a form which gains a child gains a
	// reservation, and a registry which had already used that name has to hear
	// about it.
	if forms().reserved[name] {
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     span,
			Message:  fmt.Sprintf("expected a predicate name, found %s, which is a structural child tag", name),
			Hint:     "a claim is written under its predicate's name, so a predicate may not be named for a child of a form which carries claims",
		})
		return
	}

	predicate := Predicate{Name: name, ClaimBearing: true, Span: span}

	unit, hasUnit := childForm(node, "unit")
	if hasUnit {
		if arg, ok := argument(unit, 0); ok {
			if written, ok := l.symbol(arg, "a unit"); ok {
				predicate.Unit = Unit(written)
			}
		}
	}

	if arg, ok := argumentOf(node, "shape"); ok {
		if written, ok := l.symbol(arg, "a value shape"); ok {
			shape := Shape(written)
			if !slices.Contains(shapes, shape) {
				l.add(Diagnostic{
					Severity: SeverityError,
					Span:     arg.Span,
					Message:  fmt.Sprintf("expected a value shape, found %s", written),
					Hint:     "a value shape is one of " + join(spellings(shapes), "and"),
				})
			} else {
				predicate.Shape = shape
			}
		}
	}

	dimension, hasDimension := childForm(node, "dimension")
	if hasDimension {
		if arg, ok := argument(dimension, 0); ok {
			if written, ok := l.integer(arg, "a count"); ok {
				if written != 2 && written != 3 {
					l.add(Diagnostic{
						Severity: SeverityError,
						Span:     arg.Span,
						Message:  fmt.Sprintf("expected a dimension of 2 or 3, found %d", written),
					})
				} else {
					predicate.Dimension = int(written)
				}
			}
		}
	}

	if arg, ok := argumentOf(node, "claim-bearing"); ok {
		if written, ok := l.boolean(arg, "#t or #f"); ok {
			predicate.ClaimBearing = written
		}
	}

	if arg, ok := argumentOf(node, "strict"); ok {
		if written, ok := l.boolean(arg, "#t or #f"); ok {
			predicate.Strict = written
		}
	}

	if arg, ok := argumentOf(node, "description"); ok {
		predicate.Description, _ = l.text(arg, "a string")
	}

	// The two cross-field rules of section 7.4 and section 6.6.4. Both read the
	// shape which survived validation, so a predicate whose shape was
	// misspelled hears about the misspelling once rather than about the
	// misspelling and then about a dimension that nothing can judge.
	switch {
	case predicate.Shape == ShapeCoordinate && !hasDimension:
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     node.Span,
			Message:  "expected a (dimension ...) child of the predicate form, found none",
			Hint:     "a coordinate predicate declares how many components its values have",
		})
	case predicate.Shape != "" && predicate.Shape != ShapeCoordinate && hasDimension:
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     dimension.Span,
			Message:  fmt.Sprintf("expected no (dimension ...) child of a %s predicate, found one", predicate.Shape),
			Hint:     "only a coordinate predicate declares a dimension",
		})
	}

	if predicate.Shape == ShapeText && hasUnit {
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     unit.Span,
			Message:  "expected no (unit ...) child of a text predicate, found one",
			Hint:     "a text predicate is non-dimensional by construction",
		})
		predicate.Unit = ""
	}

	if existing, ok := l.registry.predicates[name]; ok {
		l.duplicate(SortPredicate, name, span, existing.Span)
		return
	}

	l.registry.predicates[name] = predicate
}

// declareFrame reads specification section 7.5.
func (l *registryLoader) declareFrame(node *Node) {
	id, span, ok := l.identifier(node, "an id")
	if !ok {
		return
	}

	declaration := &frameDeclaration{frame: Frame{ID: id, Span: span}, id: span}

	if arg, ok := argumentOf(node, "label"); ok {
		declaration.frame.Label, _ = l.text(arg, "a string")
	}

	// The frame's unit is checked against the set the engine defines, because a
	// frame's unit is the one which has to have a magnitude: every cross-frame
	// answer converts through it, and a unit nothing defines would be converted
	// as though it were metres
	// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)). One which is
	// not a linear unit is left unread rather than recorded, for the reason an id
	// which is not one is: everything downstream would judge it as though
	// somebody had written a unit.
	if arg, ok := argumentOf(node, "unit"); ok {
		if written, ok := l.symbol(arg, "a unit"); ok {
			if _, defined := Unit(written).Metres(); !defined {
				l.add(unknownUnit(arg.Span, written))
			} else {
				declaration.frame.Unit = Unit(written)
			}
		}
	}

	parent, hasParent := childForm(node, "parent")
	if hasParent {
		if arg, ok := argument(parent, 0); ok {
			if written, ok := l.id(arg, "a frame id"); ok {
				declaration.frame.Parent = written
				declaration.parent = arg.Span
			}
		}
	}

	transform, hasTransform := childForm(node, "transform")
	if hasTransform {
		if arg, ok := argument(transform, 0); ok {
			if written, ok := l.id(arg, "a claim id"); ok {
				declaration.frame.Transform = written
				declaration.transform = arg.Span
			}
		}
	}

	// A root frame declares neither; every other frame declares both. A frame
	// with a parent and no transform claims a relationship it has no
	// measurement for, and a transform with no parent measures a relationship
	// to nothing.
	switch {
	case hasParent && !hasTransform:
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     parent.Span,
			Message:  "expected a (transform ...) child beside the parent, found none",
			Hint:     "parent and transform are present together or absent together, because the relationship between two frames is a measurement",
		})
	case hasTransform && !hasParent:
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     transform.Span,
			Message:  "expected a (parent ...) child beside the transform, found none",
			Hint:     "parent and transform are present together or absent together, because a transform maps this frame into its parent",
		})
	}

	// Whatever is left is a claim on the frame. Which predicate each is under,
	// and whether its value is the shape that predicate declares, is the claim
	// layer's question; keeping the forms is what saves that layer reading the
	// file again.
	_, children := split(elements(node))
	for _, child := range children {
		tag, ok := formTag(child)
		if !ok {
			continue
		}
		if _, structural := frameForm.child(tag); structural {
			continue
		}
		declaration.frame.Claims = append(declaration.frame.Claims, child)
	}

	if existing, ok := l.registry.frames[id]; ok {
		l.duplicate(SortFrame, string(id), span, existing.Span)
		return
	}

	l.registry.frames[id] = declaration.frame
	l.frames = append(l.frames, declaration)
}

// declareTolerance reads specification section 7.6.
func (l *registryLoader) declareTolerance(node *Node) {
	name, span, ok := l.name(node, "a tolerance name")
	if !ok {
		return
	}

	tolerance := Tolerance{Name: name, Span: span}

	if child, ok := childForm(node, "value"); ok {
		if arg, ok := argument(child, 0); ok {
			tolerance.Value, _ = l.real(arg, "a real number")
		}
		if arg, ok := argument(child, 1); ok {
			if written, ok := l.symbol(arg, "a unit"); ok {
				tolerance.Unit = Unit(written)
			}
		}
	}

	if arg, ok := argumentOf(node, "description"); ok {
		tolerance.Description, _ = l.text(arg, "a string")
	}

	if existing, ok := l.registry.tolerances[name]; ok {
		l.duplicate(SortTolerance, name, span, existing.Span)
		return
	}

	l.registry.tolerances[name] = tolerance
}

// declareRoute reads specification section 7.7.
func (l *registryLoader) declareRoute(node *Node) {
	name, span, ok := l.name(node, "a rule name")
	if !ok {
		return
	}

	declaration := &routeDeclaration{route: Route{Name: name, Span: span}}

	// Each criterion is optional and a missing one matches anything, so nothing
	// here reports an absence. What is written is read, and what is not written
	// is the rule saying it does not care.
	//
	// Which is exactly why anything written and not read marks the rule
	// unusable. Leaving a criterion which failed to read at its zero value would
	// spell it the same way the format spells "I do not care about this axis",
	// and the rule would go on to match every node that axis was written to
	// exclude. The diagnostic each of these adds is what the author acts on; the
	// mark is what stops the rule acting in the meantime.
	if arg, ok := argumentOf(node, "namespace"); ok {
		if written, ok := l.symbol(arg, "a namespace"); ok {
			declaration.route.Namespace = written
			declaration.namespace = arg.Span
		} else {
			declaration.route.unusable = true
		}
	}

	if arg, ok := argumentOf(node, "kind"); ok {
		written, ok := l.symbol(arg, "a kind")
		switch kind := Kind(written); {
		case !ok:
			declaration.route.unusable = true
		case !slices.Contains(kinds, kind):
			l.add(unknownKind(arg.Span, written))
			declaration.route.unusable = true
		default:
			declaration.route.Kind = kind
		}
	}

	if arg, ok := argumentOf(node, "type"); ok {
		if written, ok := l.symbol(arg, "a type name"); ok {
			declaration.route.Type = written
			declaration.declaredType = arg.Span
		} else {
			declaration.route.unusable = true
		}
	}

	// The file is the one element which is not a criterion, so a rule whose file
	// did not read is unusable for a second reason as well: it names no
	// destination, and a destination which is the empty path is one every write
	// through it would fail on.
	if arg, ok := argumentOf(node, "file"); ok {
		file, read := "", false
		if written, ok := l.text(arg, "a string holding a path"); ok {
			file, read = l.target(arg, written)
		}

		declaration.route.File = file
		declaration.route.unusable = declaration.route.unusable || !read
	}

	if arg, ok := argumentOf(node, "description"); ok {
		declaration.route.Description, _ = l.text(arg, "a string")
	}

	if existing, ok := l.registry.routes[name]; ok {
		l.duplicate(SortRoute, name, span, existing.Span)
		return
	}

	l.registry.routes[name] = declaration.route
	l.routes = append(l.routes, declaration)
}

// target reads the file a routing rule files a node into, reporting the paths
// it will not accept.
//
// A rule's path is relative because it is resolved against the model root, and
// one written absolute files every clone of the repository into whatever
// directory the author happened to have. It stays beneath the root and it ends
// in [Extension] for the same reason [Tx.Insert] refuses both: a walk of the
// model does not read a file outside the root or one with another extension, so
// routing a node into one writes a change which appears to have been made and
// was not. Catching it on the declaration is one diagnostic pointing at the
// rule, rather than one per node routed through it pointing at the node.
//
// The path which comes back is cleaned, so that `entities/./rooms.dfc` and
// `entities/rooms.dfc` are one destination rather than two.
func (l *registryLoader) target(node *Node, written string) (string, bool) {
	clean := path.Clean(written)

	var hint string
	switch {
	case written == "":
		hint = "a routing rule names the file its nodes are written to, relative to the model root"
	case path.IsAbs(written):
		hint = "a path is relative to the model root, so that it means the same thing in every clone of the repository"
	case clean == ".." || strings.HasPrefix(clean, "../"):
		hint = "a path stays beneath the model root, because a walk of the model never reaches a file outside it"
	case path.Ext(clean) != Extension:
		hint = "an entity file ends in " + Extension + ", because a walk of the model reads no other extension"
	default:
		return clean, true
	}

	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     node.Span,
		Message:  fmt.Sprintf("expected a relative path to an entity file, found %q", written),
		Hint:     hint,
	})
	return "", false
}

// resolve checks everything which could only be checked once every file had
// been read: a reference from one registry into another, and the three rules
// about the model as a whole.
//
// It is a second pass because a registry is a property of the source tree and
// not of a file. A frame declared in the last file the walk reaches is as
// declared as one in the first, and a loader which resolved as it read would
// report it undeclared for no reason but the order the directory happened to be
// listed in.
func (l *registryLoader) resolve() {
	for _, declaration := range l.frames {
		l.registered(l.registry, declaration.frame.ID, declaration.id)

		if declaration.frame.Transform != "" {
			l.registered(l.registry, declaration.frame.Transform, declaration.transform)
		}

		// Both ends are named: the parent which is not declared, and the frame
		// which is expressed relative to it. A frame with a parent nothing
		// declares reaches no root, so what is wrong is the pair rather than
		// either of them, and a diagnostic naming only the missing end leaves the
		// reader to find which frame sent them there.
		if parent := declaration.frame.Parent; parent != "" && !l.registry.Declares(SortFrame, string(parent)) {
			undeclared := l.registry.Undeclared(SortFrame, string(parent), declaration.parent)
			undeclared.Related = append(undeclared.Related, RelatedLocation{
				Span:    declaration.id,
				Message: "the frame which names it as its parent is written here",
			})
			l.add(undeclared)
		}
	}

	// A routing rule borrows two of its three criteria from the other
	// registries, and a criterion naming something nothing declares matches
	// nothing — which is a rule that silently never fires rather than one that
	// is wrong, and is the harder of the two to notice.
	for _, declaration := range l.routes {
		if namespace := declaration.route.Namespace; namespace != "" && !l.registry.Declares(SortNamespace, namespace) {
			l.add(l.registry.Undeclared(SortNamespace, namespace, declaration.namespace))
		}

		if declared := declaration.route.Type; declared != "" && !l.registry.Declares(SortType, declared) {
			l.add(l.registry.Undeclared(SortType, declared, declaration.declaredType))
		}
	}

	// An invariant is a check name and its parameters, and both are vocabulary:
	// the name belongs to the engine's closed check registry and a parameter
	// naming a tolerance, a type or a predicate belongs to this one. Resolving
	// them here is what makes an invariant a thing the engine could run before
	// anything has run it
	// ([0011](docs/decisions/0011-assertions-are-named-parameterised-checks.md)).
	for _, invariant := range l.invariants {
		l.add(validateAssertion(invariant.form, l.registry, registeredChecks)...)
		l.applicable(invariant)
	}

	l.cycles()
	l.roots()

	if l.registry.project == nil {
		at := Position{Path: l.root, Line: 1, Column: 1}
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     at.Span(),
			Message:  "expected one project declaration for the model, found none",
			Hint:     `a registry file declares (project (globalid-namespace "<url>")), which is what every GlobalId derives from`,
		})
	}
}

// applicable reports an invariant naming a check which could apply to no
// instance of the type it was written on.
//
// It is caught here, at registry load, rather than when something runs the
// check, because an invariant which applies to nothing does not fail: it
// passes, silently, on every instance of the type forever. The mistake is that
// the rule was never checked, and the only moment that is visible is the one
// where the type and the check are both in front of the reader.
//
// A check name nothing registers, and an invariant nothing could read a check
// name out of, are already reported by the pass above; neither is reported
// twice here.
func (l *registryLoader) applicable(written typeInvariant) {
	declaredType, ok := l.registry.types[written.declaredType]
	if !ok {
		return
	}

	arg, ok := argument(written.form, 0)
	if !ok {
		return
	}

	name, ok := arg.Datum.(sexpr.Symbol)
	if !ok {
		return
	}

	check, ok := registeredChecks.lookup(name.Value)
	if !ok {
		return
	}

	message, hint, ok := inapplicable(check, declaredType)
	if !ok {
		return
	}

	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     written.form.Span,
		Message:  message,
		Hint:     hint,
		Related: []RelatedLocation{{
			Span:    declaredType.Span,
			Message: "the type it is written on is declared here",
		}},
	})
}

// cycles reports a parent chain which returns to a frame it already passed
// through.
//
// A cycle is reported once, against the frame the walk re-entered, and names
// every frame in it. Reporting it once per member would turn one mistake into
// as many diagnostics as the cycle is long, none of which says more than the
// first.
func (l *registryLoader) cycles() {
	const (
		unvisited = iota
		onPath
		settled
	)

	state := make(map[ID]int, len(l.frames))

	for _, declaration := range l.frames {
		var path []ID

		for id := declaration.frame.ID; ; {
			if state[id] == settled {
				break
			}
			if state[id] == onPath {
				l.cycle(path, id)
				break
			}

			frame, ok := l.registry.frames[id]
			if !ok {
				// A parent which does not resolve, already reported.
				break
			}

			state[id] = onPath
			path = append(path, id)

			if frame.Parent == "" {
				break
			}
			id = frame.Parent
		}

		for _, id := range path {
			state[id] = settled
		}
	}
}

// cycle reports one cycle, which is the tail of path from the frame the walk
// re-entered.
func (l *registryLoader) cycle(path []ID, reentered ID) {
	i := slices.Index(path, reentered)
	if i < 0 {
		return
	}
	members := path[i:]

	var related []RelatedLocation
	for _, id := range members[1:] {
		related = append(related, RelatedLocation{
			Span:    l.registry.frames[id].Span,
			Message: "and through here",
		})
	}

	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     l.registry.frames[reentered].Span,
		Message: fmt.Sprintf(
			"expected a parent chain which reaches a root frame, found the cycle %s",
			strings.Join(spellings(append(slices.Clone(members), reentered)), " -> "),
		),
		Hint:    "exactly one frame is the root, and every other frame reaches it through its parents",
		Related: related,
	})
}

// roots reports a second frame declaring neither a parent nor a transform.
//
// A frame declaring one of the two is not counted, however wrong the pair is:
// it has already been reported as the half-written frame it is, and calling it
// a second root as well would report one mistake as two unrelated ones.
//
// Zero root frames is not reported here and does not need to be: a set of
// frames with no root is a set in which every frame has a parent, which is
// either a cycle or a parent which does not resolve, and both of those are
// already diagnostics. A model with no frames at all has no root and nothing
// wrong with it.
func (l *registryLoader) roots() {
	var first *frameDeclaration

	for _, declaration := range l.frames {
		if declaration.frame.Parent != "" || declaration.frame.Transform != "" {
			continue
		}
		if first == nil {
			first = declaration
			continue
		}

		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     declaration.id,
			Message:  "expected one root frame for the model, found another",
			Hint:     "the root frame is the projected coordinate reference system every other frame is expressed relative to",
			Related:  []RelatedLocation{{Span: first.id, Message: "first root frame declared here"}},
		})
	}
}

// duplicate reports a name declared for the second time, naming both.
func (l *registryLoader) duplicate(sort Sort, name string, span, first Span) {
	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message:  fmt.Sprintf("expected an undeclared %s, found %s, which is already declared", sort, name),
		Related:  []RelatedLocation{{Span: first, Message: "first declared here"}},
	})
}

// repeated reports a value written twice inside one declaration, naming both.
func (l *registryLoader) repeated(span Span, what, written string, first Span) {
	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     span,
		Message:  fmt.Sprintf("expected %s which is not already permitted, found %s", what, written),
		Related:  []RelatedLocation{{Span: first, Message: "first written here"}},
	})
}
