// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"net/url"
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
	l := &registryLoader{
		root: root,
		registry: &Registry{
			namespaces: make(map[string]Namespace),
			types:      make(map[string]Type),
			predicates: make(map[string]Predicate),
			frames:     make(map[string]Frame),
			tolerances: make(map[string]Tolerance),
		},
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

	return l.registry, l.diags
}

// registryLoader collects one load of a registry set.
type registryLoader struct {
	// root is what the load was asked for, which is where a diagnostic about
	// the model as a whole rather than about any one file points.
	root string

	// registry is what has been declared so far.
	registry *Registry

	// diags are the problems found so far.
	diags []Diagnostic

	// frames are the accepted frame declarations in the order they were read,
	// with the spans a cross-reference diagnostic points at. Frames are the one
	// registry whose entries refer to each other, so they are the one whose
	// declarations outlive the file they were read from.
	frames []*frameDeclaration
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

// add records diagnostics.
func (l *registryLoader) add(diags ...Diagnostic) {
	l.diags = append(l.diags, diags...)
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

// wellFormedNamespace reports whether s matches the namespace production of
// specification section 4.1.
//
// ASCII, and beginning with a letter, is not stylistic. A namespace is part of
// an id, an id is written as a bare symbol, and a symbol beginning with a digit
// is a malformed number to the tokenizer — a lexical error reported before any
// of this specification is consulted. Confining the namespace is what makes
// every well-formed id a well-formed symbol.
func wellFormedNamespace(s string) bool {
	if s == "" {
		return false
	}

	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '-' || c == '_'):
		default:
			return false
		}
	}

	return true
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
			l.add(Diagnostic{
				Severity: SeverityError,
				Span:     arg.Span,
				Message:  fmt.Sprintf("expected a kind, found %s", written),
				Hint:     "a kind is one of " + join(spellings(kinds), "and"),
			})
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
			l.add(Diagnostic{
				Severity: SeverityError,
				Span:     arg.Span,
				Message:  fmt.Sprintf("expected a geometry form or absent, found %s", written),
				Hint:     "a geometry form is one of " + join(spellings(geometries), "and") + ", and absent permits an instance to omit the child",
			})
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
	}

	if existing, ok := l.registry.types[name]; ok {
		l.duplicate(SortType, name, span, existing.Span)
		return
	}

	l.registry.types[name] = declared
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

	// The two cross-field rules of section 7.4 and section 6.6.4. Both are
	// checked against the shape as written rather than against the shape that
	// survived validation, so a predicate whose shape was misspelled reports
	// that once and not twice.
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
	id, span, ok := l.name(node, "an id")
	if !ok {
		return
	}

	declaration := &frameDeclaration{frame: Frame{ID: id, Span: span}, id: span}

	if arg, ok := argumentOf(node, "label"); ok {
		declaration.frame.Label, _ = l.text(arg, "a string")
	}

	if arg, ok := argumentOf(node, "unit"); ok {
		if written, ok := l.symbol(arg, "a unit"); ok {
			declaration.frame.Unit = Unit(written)
		}
	}

	parent, hasParent := childForm(node, "parent")
	if hasParent {
		if arg, ok := argument(parent, 0); ok {
			if written, ok := l.symbol(arg, "a frame id"); ok {
				declaration.frame.Parent = written
				declaration.parent = arg.Span
			}
		}
	}

	transform, hasTransform := childForm(node, "transform")
	if hasTransform {
		if arg, ok := argument(transform, 0); ok {
			if written, ok := l.symbol(arg, "a claim id"); ok {
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
		l.duplicate(SortFrame, id, span, existing.Span)
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
		l.registered(declaration.frame.ID, declaration.id)

		if declaration.frame.Transform != "" {
			l.registered(declaration.frame.Transform, declaration.transform)
		}

		if parent := declaration.frame.Parent; parent != "" && !l.registry.Declares(SortFrame, parent) {
			l.add(l.registry.Undeclared(SortFrame, parent, declaration.parent))
		}
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

// registered checks that the namespace of an id is one the registry declares.
//
// The namespace of a frame's parent is not checked here, because a parent which
// resolves was checked where it was declared and a parent which does not is
// already being reported as undeclared. Two diagnostics about one misspelling
// is one more than anybody needs.
func (l *registryLoader) registered(id string, span Span) {
	namespace, _, ok := splitID(id)
	if !ok {
		l.add(Diagnostic{
			Severity: SeverityError,
			Span:     span,
			Message:  fmt.Sprintf("expected an id, found %s", id),
			Hint:     "an id is namespace:local, split on the first colon, and the namespace is declared in a registry file",
		})
		return
	}

	if !l.registry.Declares(SortNamespace, namespace) {
		l.add(l.registry.Undeclared(SortNamespace, namespace, span))
	}
}

// splitID divides an id into the namespace and the local part, and reports
// whether it is one at all.
//
// The split is on the first colon only: a local part may hold further colons,
// and the namespace never does
// ([0003](docs/decisions/0003-id-namespaces-are-a-closed-registry.md)).
func splitID(id string) (namespace, local string, ok bool) {
	i := strings.Index(id, ":")
	if i <= 0 || i == len(id)-1 {
		return "", "", false
	}
	return id[:i], id[i+1:], true
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

	state := make(map[string]int, len(l.frames))

	for _, declaration := range l.frames {
		var path []string

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
func (l *registryLoader) cycle(path []string, reentered string) {
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
			strings.Join(append(slices.Clone(members), reentered), " -> "),
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

// name reads the positional name of a declaration, with the span a diagnostic
// about that name points at.
func (l *registryLoader) name(node *Node, what string) (string, Span, bool) {
	arg, ok := argument(node, 0)
	if !ok {
		return "", Span{}, false
	}

	name, ok := l.symbol(arg, what)
	return name, arg.Span, ok
}

// symbol reads a symbol, reporting what was written there instead.
func (l *registryLoader) symbol(node *Node, what string) (string, bool) {
	datum, ok := node.Datum.(sexpr.Symbol)
	if !ok {
		l.wrong(node, what)
		return "", false
	}
	return datum.Value, true
}

// text reads a string.
func (l *registryLoader) text(node *Node, what string) (string, bool) {
	datum, ok := node.Datum.(sexpr.String)
	if !ok {
		l.wrong(node, what)
		return "", false
	}
	return datum.Value, true
}

// boolean reads a boolean.
func (l *registryLoader) boolean(node *Node, what string) (bool, bool) {
	datum, ok := node.Datum.(sexpr.Bool)
	if !ok {
		l.wrong(node, what)
		return false, false
	}
	return datum.Value, true
}

// integer reads a count, which specification section 4.3 writes with neither a
// fraction nor an exponent so that it reads back as an integer.
func (l *registryLoader) integer(node *Node, what string) (int64, bool) {
	datum, ok := node.Datum.(sexpr.Int)
	if !ok {
		l.wrong(node, what)
		return 0, false
	}
	return datum.Value, true
}

// real reads a real number, which specification section 4.3 writes with a
// fraction or an exponent so that it reads back as a real.
//
// A whole number written as one — `5` where `5.0` was meant — is reported
// rather than widened, because the distinction is the only thing telling a
// magnitude apart from a count in a format where both are written as digits.
func (l *registryLoader) real(node *Node, what string) (float64, bool) {
	datum, ok := node.Datum.(sexpr.Float)
	if !ok {
		if _, isInt := node.Datum.(sexpr.Int); isInt {
			l.add(Diagnostic{
				Severity: SeverityError,
				Span:     node.Span,
				Message:  fmt.Sprintf("expected %s, found %s", what, describe(node)),
				Hint:     "a real number is written with a fraction or an exponent, so that it reads back as a real",
			})
			return 0, false
		}

		l.wrong(node, what)
		return 0, false
	}
	return datum.Value, true
}

// wrong reports a datum of the wrong sort written where a declaration wanted
// one of another.
func (l *registryLoader) wrong(node *Node, what string) {
	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     node.Span,
		Message:  fmt.Sprintf("expected %s, found %s", what, describe(node)),
	})
}

// elements is everything written after a form's tag.
func elements(node *Node) []*Node {
	if len(node.Children) == 0 {
		return nil
	}
	return node.Children[1:]
}

// argument returns the i-th positional argument of a form, and whether it was
// written.
func argument(node *Node, i int) (*Node, bool) {
	written, _ := split(elements(node))
	if i < 0 || i >= len(written) {
		return nil, false
	}
	return written[i], true
}

// childForm returns the first child of node written with tag, and whether one
// was written.
func childForm(node *Node, tag string) (*Node, bool) {
	_, children := split(elements(node))

	for _, child := range children {
		if written, ok := formTag(child); ok && written == tag {
			return child, true
		}
	}

	return nil, false
}

// childForms returns every child of node written with tag, in the order they
// were written.
func childForms(node *Node, tag string) []*Node {
	_, children := split(elements(node))

	var out []*Node
	for _, child := range children {
		if written, ok := formTag(child); ok && written == tag {
			out = append(out, child)
		}
	}

	return out
}

// argumentOf returns the single positional argument of the child written with
// tag, which is how every one-value child of a registry form is read.
func argumentOf(node *Node, tag string) (*Node, bool) {
	child, ok := childForm(node, tag)
	if !ok {
		return nil, false
	}
	return argument(child, 0)
}

// spellings spells a closed set for a diagnostic which lists it.
func spellings[T ~string](set []T) []string {
	out := make([]string, 0, len(set))
	for _, member := range set {
		out = append(out, string(member))
	}
	return out
}
