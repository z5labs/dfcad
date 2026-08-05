// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"iter"
	"math"
	"slices"
)

// This file is the check set: what the engine's closed registry holds, what
// each check says about itself and what running one against a subject decides.
//
// The declaration and the implementation are kept together because they are one
// statement about one rule. A check whose declaration said it took a tolerance
// and whose implementation read a different parameter would be two descriptions
// of one thing in two files, and the format's whole claim — that an assertion
// can be validated, listed and bound without being evaluated
// ([0011](docs/decisions/0011-assertions-are-named-parameterised-checks.md)) —
// rests on those two never disagreeing.
//
// The machinery around them is in check.go: what a declaration may say, what
// validates an assertion against one, and what the registry refuses to hold.

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
	containedAreasDoNotOverlap{},
	containedAreasSum{},
	crossFrameBudgetHolds{},
	edgeBackingResolves{},
	edgeEndpointsDiffer{},
	requiredClaim{},
	staysClearOfZone{},
	withinResolves{},
	zoneMembersResolve{},
)

// The parameters the checks below share, spelled once so that two checks asking
// for the same thing ask for it under the same name.
//
// A model writing `(position position)` on one check and `(positioned-by …)` on
// the next would be reading the same decision out of two vocabularies, and the
// author would have to look up which spelling this rule takes.
const (
	// toleranceParameter is how close is close enough, named from the registry
	// and never written as a number
	// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
	toleranceParameter = "tolerance"

	// positionParameter is the predicate a vertex's position is claimed under.
	// Which predicate that is belongs to the consuming repository and not to the
	// engine ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)),
	// so a check which needs coordinates is told the name rather than assuming
	// one.
	positionParameter = "position"

	// kindParameter narrows a check over what a node contains to the contents of
	// one kind.
	kindParameter = "kind"

	// areaParameter is how far two areas may differ, which is a second tolerance
	// and not the one shapes are read against: coincidence is a distance and a
	// discrepancy between areas is an area, and one figure cannot be both.
	areaParameter = "area-tolerance"
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
				Name:        toleranceParameter,
				Type:        ParameterTolerance,
				Required:    true,
				Description: "How far a loop may fail to close and still count as closed.",
			},
			{
				Name:     positionParameter,
				Type:     ParameterPredicate,
				Required: false,
				Description: "The predicate a corner's position is claimed under, which is what the size of a gap " +
					"is measured from.",
			},
		},
		Forms:      []SubjectForm{SubjectNode, SubjectLoop},
		Geometries: []Geometry{GeometryArea, GeometrySurface, GeometrySolid},
	}
}

// Run implements [Runner].
//
// The traversal is [Topology.Assemble]'s, which is the one place a loop is read
// as the ring its edges walk. A second walk here would be a second answer to
// "does this close", and the two would disagree the first time either learned
// something about coincident corners.
//
// The position predicate is optional and what it decides is how much the failure
// can say. Written, two corners no further apart than the tolerance are one
// corner and a gap is reported with its size; left out, closure is decided by
// vertex identity alone and a gap is reported as one whose size could not be
// measured — which is what is true, rather than a silent pass on a model nobody
// gave coordinates for.
func (boundaryLoopsClose) Run(subject CheckSubject) []Failure {
	tolerance, ok := symbolOf(subject, toleranceParameter)
	if !ok {
		return nil
	}
	position, _ := symbolOf(subject, positionParameter)

	graph := subject.Graph()

	var loops []*Loop
	switch thing := subject.Subject().(type) {
	case *Loop:
		loops = []*Loop{thing}
	case *SemanticNode:
		loops = slices.Collect(graph.Boundaries().Loops(thing))
	}

	var out []Failure
	for _, loop := range loops {
		survey := positionSurvey(graph, tolerance, position, verticesOf(graph, loop))

		assembly, diags := graph.Topology().Assemble(loop, survey.Positions, tolerance, graph.Registry())
		if assembly.Closed() {
			continue
		}

		if len(diags) == 0 {
			// Every way a loop fails to close carries a diagnostic of its own,
			// so this is unreachable from a loop the model holds. It is here
			// because "did not close" and "said nothing about why" must not be
			// the same answer as "closed".
			out = append(out, Failure{
				Message: fmt.Sprintf(
					"expected the loop %s to close, found a traversal which does not return to the corner it started from",
					loop.ID(),
				),
				Span: loop.Span(),
			})
			continue
		}

		for _, diagnostic := range diags {
			out = append(out, failureOf(diagnostic))
		}
	}

	return out
}

// containedAreasDoNotOverlap is the check that the things written within a node
// do not cover the same ground as one another.
type containedAreasDoNotOverlap struct{}

// Declare implements [Check].
func (containedAreasDoNotOverlap) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name: "contained-areas-do-not-overlap",
		Description: "No two nodes written within the subject cover the same ground: every pair of the shapes it " +
			"contains meets in nothing, judged against the named tolerance.",
		Parameters: []CheckParameter{
			{
				Name:        toleranceParameter,
				Type:        ParameterTolerance,
				Required:    true,
				Description: "How close two corners are one corner, and how thin a shared sliver is nothing at all.",
			},
			{
				Name:        positionParameter,
				Type:        ParameterPredicate,
				Required:    true,
				Description: "The predicate a corner's position is claimed under, which is what the shapes are read from.",
			},
			{
				Name:     kindParameter,
				Type:     ParameterKind,
				Required: false,
				Description: "Compares only the contents of this kind. Every content with a shape is compared where " +
					"it is left out.",
			},
		},
		Forms: []SubjectForm{SubjectNode},
	}
}

// Run implements [Runner].
//
// The subject is the container rather than either of the things which overlap,
// because "no two of these overlap" is a rule about the set: written on one of
// them it would have to name the others, and the model already says which nodes
// those are.
//
// Two shapes which meet only along a boundary overlap in nothing, which is
// [Region.Intersect]'s rule and the reason this is not a check every pair of
// adjacent rooms fails.
func (containedAreasDoNotOverlap) Run(subject CheckSubject) []Failure {
	node, ok := subject.Subject().(*SemanticNode)
	if !ok {
		return nil
	}

	tolerance, ok := symbolOf(subject, toleranceParameter)
	if !ok {
		return nil
	}
	position, ok := symbolOf(subject, positionParameter)
	if !ok {
		return nil
	}

	graph := subject.Graph()

	wanted, _ := symbolOf(subject, kindParameter)

	shapes, out := shapesWithin(graph, node, wanted, tolerance, position)

	for i, one := range shapes {
		for _, other := range shapes[i+1:] {
			overlap, diags := one.region.Intersect(other.region)
			if len(diags) > 0 {
				for _, diagnostic := range diags {
					out = append(out, failureOf(diagnostic))
				}
				continue
			}

			if overlap.Empty() {
				continue
			}

			out = append(out, Failure{
				Message: fmt.Sprintf(
					"expected no two of the shapes within %s to cover the same ground, found %s and %s overlapping by %s%s",
					nodeName(node), one.node.ID(), other.node.ID(),
					decimal(overlap.Area()), squareSuffix(overlap.Unit()),
				),
				Hint: "two shapes which meet only along a boundary overlap in nothing; this pair covers ground both " +
					"of them claim, so one of the two outlines is drawn over the other",
				Span: graph.Nodes().named(other.node),
				Related: []RelatedLocation{
					{Span: graph.Nodes().named(one.node), Message: "one of the two is written here"},
				},
			})
		}
	}

	return out
}

// containedAreasSum is the check that what a node contains adds up to what it
// is.
type containedAreasSum struct{}

// Declare implements [Check].
func (containedAreasSum) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name: "contained-areas-sum",
		Description: "The areas of the nodes written within the subject add up to the subject's own area, within " +
			"the named area tolerance.",
		Parameters: []CheckParameter{
			{
				Name:        toleranceParameter,
				Type:        ParameterTolerance,
				Required:    true,
				Description: "How close two corners are one corner, which is what each shape is read against.",
			},
			{
				Name:     areaParameter,
				Type:     ParameterTolerance,
				Required: true,
				Description: "How far the contents may fail to add up to the whole. It is an area, so it is declared " +
					"in the square of the frame's unit — 0.05 m2 where the frame is in m.",
			},
			{
				Name:        positionParameter,
				Type:        ParameterPredicate,
				Required:    true,
				Description: "The predicate a corner's position is claimed under, which is what the shapes are read from.",
			},
			{
				Name:     kindParameter,
				Type:     ParameterKind,
				Required: false,
				Description: "Sums only the contents of this kind. Every content with a shape is summed where it is " +
					"left out.",
			},
		},
		Forms:      []SubjectForm{SubjectNode},
		Geometries: []Geometry{GeometryArea, GeometrySurface},
	}
}

// Run implements [Runner].
//
// The discrepancy is signed and the failure says which way it runs, because the
// two directions are two different mistakes: contents which come to more than
// the whole are drawn over one another or over the outside, and contents which
// come to less leave ground nothing accounts for.
//
// A subject which contains nothing with a shape is satisfied rather than failed.
// Nothing was subdivided, so there is no subdivision to disagree with the whole,
// and a check which reported the whole area as missing would fail every node
// somebody has not got round to splitting up yet.
func (containedAreasSum) Run(subject CheckSubject) []Failure {
	node, ok := subject.Subject().(*SemanticNode)
	if !ok {
		return nil
	}

	tolerance, ok := symbolOf(subject, toleranceParameter)
	if !ok {
		return nil
	}
	area, ok := symbolOf(subject, areaParameter)
	if !ok {
		return nil
	}
	position, ok := symbolOf(subject, positionParameter)
	if !ok {
		return nil
	}

	graph := subject.Graph()

	declared, found := graph.Registry().Tolerance(area)
	if !found {
		// A rule naming a tolerance the registry does not declare is a load
		// error which names it and points at it. Reporting it again here would
		// be one mistake told twice, in the vocabulary of the rule rather than
		// of the registry.
		return nil
	}

	whole, failures := shapeOf(graph, node, tolerance, position)
	if len(failures) > 0 {
		return failures
	}

	if !whole.ready {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected a shape on %s to add its contents up to, found no loop bounding it",
				nodeName(node),
			),
			Hint: "a node's area is read from the loops which bound it; a node whose type declares an area and which " +
				"references none has no whole for its parts to sum to",
			Span: graph.Nodes().named(node),
		}}
	}

	if wanted := squareUnit(whole.Unit()); declared.Unit != wanted {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected the tolerance %s in %s, the square of the unit %s is measured in, found %s %s",
				declared.Name, wanted, nodeName(node), decimal(declared.Value), declared.Unit,
			),
			Hint: "a discrepancy between areas is an area; nothing here converts between units, so a tolerance " +
				"declared in a length would be compared against a figure it is not a figure of",
			Span:    graph.Nodes().named(node),
			Related: []RelatedLocation{{Span: declared.Span, Message: "the tolerance is declared here"}},
		}}
	}

	wanted, _ := symbolOf(subject, kindParameter)

	shapes, failures := shapesWithin(graph, node, wanted, tolerance, position)
	if len(failures) > 0 {
		return failures
	}

	if len(shapes) == 0 {
		return nil
	}

	var parts float64
	for _, one := range shapes {
		parts += one.region.Area()
	}

	discrepancy := parts - whole.Area()
	if math.Abs(discrepancy) <= declared.Value {
		return nil
	}

	sense := "more than"
	if discrepancy < 0 {
		sense = "less than"
	}

	return []Failure{{
		Message: fmt.Sprintf(
			"expected what %s contains to add up to its own %s%s, found %s%s, which is %s%s %s the whole",
			nodeName(node), decimal(whole.Area()), squareSuffix(whole.Unit()),
			decimal(parts), squareSuffix(whole.Unit()),
			decimal(math.Abs(discrepancy)), squareSuffix(whole.Unit()), sense,
		),
		Hint: fmt.Sprintf(
			"the sum is of the %s it contains which have a shape, judged against the tolerance %s, which is %s %s; "+
				"either a part is drawn wrong or the whole is",
			plural(len(shapes), "node"), declared.Name, decimal(declared.Value), declared.Unit,
		),
		Span: graph.Nodes().named(node),
	}}
}

// crossFrameBudgetHolds is the check that expressing something in another frame
// is an answer worth having.
type crossFrameBudgetHolds struct{}

// Declare implements [Check].
func (crossFrameBudgetHolds) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name: "cross-frame-budget-holds",
		Description: "Expressing the subject in the named frame stays inside its error budget: the accuracy of " +
			"every transform between the two frames, combined, does not exceed the named limit.",
		Parameters: []CheckParameter{
			{
				Name:        "frame",
				Type:        ParameterFrame,
				Required:    true,
				Description: "The frame the answer is wanted in.",
			},
			{
				Name:        "limit",
				Type:        ParameterTolerance,
				Required:    true,
				Description: "How uncertain that answer may be and still be one somebody can act on.",
			},
		},
		Forms: []SubjectForm{SubjectNode, SubjectVertex, SubjectEdge, SubjectLoop},
	}
}

// Run implements [Runner].
//
// What is judged is the route rather than the shape. Every answer about the
// subject in the target frame passes through the same chain of fits, so the
// combined uncertainty of that chain is the floor under all of them: a clearance
// computed to a millimetre through a georeference known to eight centimetres is
// known to eight centimetres.
//
// A subject already in the target frame passes. No transform is applied, so
// there is nothing to be uncertain about — and reporting an exact zero for it
// would be the one thing a budget never says
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)).
func (crossFrameBudgetHolds) Run(subject CheckSubject) []Failure {
	target, ok := symbolOf(subject, "frame")
	if !ok {
		return nil
	}
	limit, ok := symbolOf(subject, "limit")
	if !ok {
		return nil
	}

	graph := subject.Graph()
	thing := subject.Subject()

	from, ok := frameOf(thing)
	if !ok {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected %s to declare the frame it is measured in, found none, so nothing says what it would be transformed from",
				thing.ID(),
			),
			Hint: "a cross-frame answer is a route between two declared frames; a subject in no frame is in none of them",
		}}
	}

	if from == ID(target) {
		return nil
	}

	budget, err := graph.Frames().TransformBudget(from, ID(target))
	if err != nil {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected a route from %s to %s whose accuracy could be read, found that %s",
				from, target, err,
			),
			Hint: "the relationship between two frames is a measurement rather than a configuration constant; a route " +
				"which is not measured has no accuracy to compare against a limit",
		}}
	}

	combined, err := budget.Combined()
	if err != nil {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected the accuracy of the route from %s to %s to combine into one figure, found that %s",
				from, target, err,
			),
			Hint: "an unstated accuracy is unknown rather than zero, and a route whose fits disagree about their unit " +
				"is not one figure; either way there is no budget to hold the answer to",
		}}
	}

	declared, found := graph.Registry().Tolerance(limit)
	if !found {
		return nil
	}

	if declared.Unit != combined.Unit {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected the limit %s in %s, the unit the route from %s to %s is measured in, found %s %s",
				declared.Name, combined.Unit, from, target, decimal(declared.Value), declared.Unit,
			),
			Hint: "nothing here converts between units, so a limit written in one and a budget accumulated in " +
				"another are two figures which cannot be compared",
			Related: []RelatedLocation{{Span: declared.Span, Message: "the limit is declared here"}},
		}}
	}

	if combined.Magnitude <= declared.Value {
		return nil
	}

	hint := fmt.Sprintf(
		"the answer is only as well known as the fits it is read through; the limit %s is declared as %s %s",
		declared.Name, decimal(declared.Value), declared.Unit,
	)
	if dominant, some := budget.Dominant(); some {
		hint = fmt.Sprintf(
			"%s, and the largest single contribution is %s at %s %s",
			hint, dominant.Name, decimal(math.Abs(dominant.Magnitude)), dominant.Unit,
		)
	}

	return []Failure{{
		Message: fmt.Sprintf(
			"expected %s in %s to be known to within %s %s, found a combined uncertainty of %s accumulated from %s",
			thing.ID(), target, decimal(declared.Value), declared.Unit,
			combined, plural(len(budget.Terms()), "term"),
		),
		Hint: hint,
	}}
}

// edgeBackingResolves is the check that what physically realises an edge is
// something the model holds.
type edgeBackingResolves struct{}

// Declare implements [Check].
func (edgeBackingResolves) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name: "edge-backing-resolves",
		Description: "Every element the subject says physically realises it is a node the model holds, of kind " +
			"Element, so that a boundary called physical is backed by something real.",
		Forms: []SubjectForm{SubjectEdge},
	}
}

// Run implements [Runner].
//
// An edge which names no backing element satisfies this. The open line between a
// foyer and a dining room is a virtual boundary and is as ordinary as a wall;
// what this refuses is an edge which says something backs it and names nothing
// the model holds, because that is a boundary whose classification cannot be
// answered either way (specification section 6.3).
func (edgeBackingResolves) Run(subject CheckSubject) []Failure {
	edge, ok := subject.Subject().(*Edge)
	if !ok {
		return nil
	}

	graph := subject.Graph()

	var out []Failure
	for _, id := range edge.BackedBy() {
		found, held := graph.Entity(id)
		if !held {
			out = append(out, dangling(graph, edge, id, fmt.Sprintf(
				"expected an element id something in this model holds, found %s, which names no node", id,
			)))
			continue
		}

		element, semantic := found.(*SemanticNode)
		if !semantic {
			out = append(out, dangling(graph, edge, id, fmt.Sprintf(
				"expected an element id, found %s, which is %s %s", id, article(entityTag(found)), entityTag(found),
			)))
			continue
		}

		if element.Kind() == KindElement {
			continue
		}

		out = append(out, dangling(graph, edge, id, fmt.Sprintf(
			"expected a node of kind %s, found %s, which is %s", KindElement, id, kindName(element.Kind()),
		)))
	}

	return out
}

// dangling is one backing reference of an edge which did not reach an element,
// pointed at where that reference was written.
//
// The span is the reference rather than the edge, because an edge realised by
// three elements one of which is missing is a line to change and not a form to
// re-read.
func dangling(graph *Graph, edge *Edge, element ID, message string) Failure {
	span := edge.Span()
	for _, written := range graph.Topology().backings {
		if written.edge == edge && written.element == element {
			span = written.at
			break
		}
	}

	return Failure{
		Message: message,
		Hint:    backedByHint,
		Span:    span,
	}
}

// staysClearOfZone is the check that a shape does not cross into a zone it is
// meant to keep out of.
type staysClearOfZone struct{}

// Declare implements [Check].
func (staysClearOfZone) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name: "stays-clear-of-zone",
		Description: "The subject's shape does not cross into the named zone's: the two meet in nothing, judged " +
			"against the named tolerance.",
		Parameters: []CheckParameter{
			{
				Name:        "zone",
				Type:        ParameterID,
				Required:    true,
				Description: "The zone the subject is to stay clear of.",
			},
			{
				Name:        toleranceParameter,
				Type:        ParameterTolerance,
				Required:    true,
				Description: "How close two corners are one corner, and how thin a crossing is nothing at all.",
			},
			{
				Name:        positionParameter,
				Type:        ParameterPredicate,
				Required:    true,
				Description: "The predicate a corner's position is claimed under, which is what the shapes are read from.",
			},
		},
		Forms:      []SubjectForm{SubjectNode},
		Geometries: []Geometry{GeometryArea, GeometrySurface, GeometrySolid},
	}
}

// Run implements [Runner].
//
// This is the setback: the shape which must not reach into the strip along a
// boundary, the plant room which must stay out of the protected zone. The zone
// is named rather than reached through membership, because a subject does not
// belong to the zone it has to keep out of — membership would say the opposite
// of what the rule means.
func (staysClearOfZone) Run(subject CheckSubject) []Failure {
	node, ok := subject.Subject().(*SemanticNode)
	if !ok {
		return nil
	}

	written, ok := symbolOf(subject, "zone")
	if !ok {
		return nil
	}
	tolerance, ok := symbolOf(subject, toleranceParameter)
	if !ok {
		return nil
	}
	position, ok := symbolOf(subject, positionParameter)
	if !ok {
		return nil
	}

	graph := subject.Graph()

	zone, held := graph.Node(ID(written))
	if !held {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected a zone this model holds, found %s, which names no node of it",
				written,
			),
			Hint: "the zone a subject stays clear of is a node of kind Zone with a shape of its own",
		}}
	}

	if zone.Kind() != KindZone {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected a node of kind %s, found %s, which is %s",
				KindZone, written, kindName(zone.Kind()),
			),
			Hint:    "a subject stays clear of a zone; what it keeps out of is a grouping and not another room",
			Related: []RelatedLocation{{Span: graph.Nodes().named(zone), Message: "the node it names is written here"}},
		}}
	}

	shape, failures := shapeOf(graph, node, tolerance, position)
	if len(failures) > 0 {
		return failures
	}

	keepOut, failures := shapeOf(graph, zone, tolerance, position)
	if len(failures) > 0 {
		return failures
	}

	if !keepOut.ready {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected the zone %s to have a shape to stay clear of, found no loop bounding it",
				zone.ID(),
			),
			Hint: "a zone with no outline encloses nothing, so nothing can cross into it; the rule as written cannot " +
				"be decided either way",
			Span:    graph.Nodes().named(node),
			Related: []RelatedLocation{{Span: graph.Nodes().named(zone), Message: "the zone it names is written here"}},
		}}
	}

	if !shape.ready {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected a shape on %s to judge against the zone %s, found no loop bounding it",
				nodeName(node), zone.ID(),
			),
			Hint: "a node whose type declares an area and which references no loop has no outline to compare; the " +
				"rule holds of a shape and there is none to hold it of",
			Span: graph.Nodes().named(node),
		}}
	}

	crossing, diags := shape.Intersect(keepOut)
	if len(diags) > 0 {
		out := make([]Failure, 0, len(diags))
		for _, diagnostic := range diags {
			out = append(out, failureOf(diagnostic))
		}
		return out
	}

	if crossing.Empty() {
		return nil
	}

	return []Failure{{
		Message: fmt.Sprintf(
			"expected %s to stay clear of the zone %s, found it crossing into it over %s%s",
			nodeName(node), zone.ID(), decimal(crossing.Area()), squareSuffix(crossing.Unit()),
		),
		Hint: "a shape which meets the zone only along its boundary crosses into nothing; this one covers ground " +
			"inside it",
		Span:    graph.Nodes().named(node),
		Related: []RelatedLocation{{Span: graph.Nodes().named(zone), Message: "the zone it crosses into is written here"}},
	}}
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

// contained is one node written within another, together with the shape it
// covers.
//
// The node travels with the region because a failure names what overlapped and
// points at where it was written, and a region carries the id but not the span
// of the node it was read from.
type contained struct {
	node   *SemanticNode
	region Region
}

// shapesWithin reads the shape of everything written within node, in the order
// the containment index holds them, and reports what stopped any of them being
// read.
//
// A content with no outline is left out rather than reported. A circuit group
// and a warranty are written within a room and cover nothing, and a check about
// what covers ground has nothing to say about either.
//
// A content whose outline could not be read is reported instead of skipped. It
// is the difference between a check which passed and one which could not decide,
// and answering the second with the first is how a gate reports a model sound
// because it measured nothing.
//
// kind narrows the contents to those of one kind, and is empty where the rule
// named none.
func shapesWithin(graph *Graph, node *SemanticNode, kind, tolerance, position string) ([]contained, []Failure) {
	var (
		shapes   []contained
		failures []Failure
	)

	for related := range graph.Contains(node) {
		child := related.Node()
		if kind != "" && string(child.Kind()) != kind {
			continue
		}

		region, reasons := shapeOf(graph, child, tolerance, position)
		if len(reasons) > 0 {
			failures = append(failures, reasons...)
			continue
		}

		if !region.ready {
			continue
		}

		shapes = append(shapes, contained{node: child, region: region})
	}

	return shapes, failures
}

// shapeOf reads the shape one node covers, judged against a named tolerance and
// read from the positions claimed under a named predicate.
//
// What comes back is the region and the reasons it is not one. A node which
// references no loop is neither: it covers nothing and says nothing is wrong,
// which [Region.ready] is what tells apart from a shape which could not be read.
func shapeOf(graph *Graph, node *SemanticNode, tolerance, position string) (Region, []Failure) {
	survey := positionSurvey(graph, tolerance, position, graph.Boundaries().Vertices(node))

	region, diags := graph.Topology().RegionOf(node, graph.Boundaries(), survey)
	if len(diags) == 0 {
		return region, nil
	}

	failures := make([]Failure, 0, len(diags))
	for _, diagnostic := range diags {
		failures = append(failures, failureOf(diagnostic))
	}

	return region, failures
}

// positionSurvey is what a shape is read against: where the corners are, the
// claims which put them there, the tolerance they are judged coincident against
// and the registry the frames and their units come from.
//
// The positions and the claims behind them are placed together rather than
// filled separately, which is what [Survey.Place] is for: a corner whose
// position is measured and whose provenance is not comes out as an answer with a
// narrower budget than the evidence supports.
//
// A check which was told no predicate places nothing, which is a measurement
// nobody can make rather than one which came out zero: every distance read from
// it is reported as unmeasured rather than as an agreement.
func positionSurvey(graph *Graph, tolerance, position string, vertices iter.Seq[*Vertex]) Survey {
	survey := Survey{Tolerance: tolerance, Registry: graph.Registry()}
	if position == "" {
		return survey
	}

	for vertex := range vertices {
		resolution, err := graph.Claims().Resolve(vertex.ID(), position, graph.Registry())
		if err != nil {
			continue
		}
		survey.Place(vertex.ID(), resolution)
	}

	return survey
}

// verticesOf is the corners one loop passes through, each yielded once, in the
// order the loop reaches them.
func verticesOf(graph *Graph, loop *Loop) iter.Seq[*Vertex] {
	return func(yield func(*Vertex) bool) {
		seen := make(map[ID]bool, 2*len(loop.edges))

		for _, id := range loop.Edges() {
			edge, held := graph.Topology().Edge(id)
			if !held {
				continue
			}

			start, end := edge.Vertices()
			for _, corner := range []ID{start, end} {
				if seen[corner] {
					continue
				}
				seen[corner] = true

				vertex, held := graph.Topology().Vertex(corner)
				if !held {
					continue
				}

				if !yield(vertex) {
					return
				}
			}
		}
	}
}

// symbolOf reads the one symbol written for a parameter, and whether the rule
// wrote one.
//
// Every parameter of every check above is written as one symbol — a tolerance
// name, a predicate name, a kind, an id — so this is how each of them is read.
// A required parameter is always there, because a rule missing one is a load
// error and never reaches a run; an optional one is what this reports on.
func symbolOf(subject CheckSubject, parameter string) (string, bool) {
	argument, written := subject.Argument(parameter)
	if !written {
		return "", false
	}
	return argument.Symbol()
}

// failureOf carries a diagnostic a measurement raised into the failure of the
// check which asked for it.
//
// The wording is kept exactly. What stopped the shape being read is the same
// fact whether a command measured it or a rule did, and rewriting it here would
// be a second vocabulary for one defect.
func failureOf(diagnostic Diagnostic) Failure {
	return Failure{
		Message: diagnostic.Message,
		Hint:    diagnostic.Hint,
		Span:    diagnostic.Span,
		Related: diagnostic.Related,
	}
}

// frameOf is the frame a subject is measured in, whichever family holds it, and
// whether it declares one.
func frameOf(subject Entity) (ID, bool) {
	switch thing := subject.(type) {
	case *SemanticNode:
		return thing.Frame()
	case *Vertex:
		return thing.Frame(), thing.Frame() != ""
	case *Edge:
		return thing.Frame(), thing.Frame() != ""
	case *Loop:
		return thing.Frame(), thing.Frame() != ""
	}
	return "", false
}

// entityTag names the form a subject was written as, for a diagnostic which
// found one where another was wanted.
func entityTag(subject Entity) string {
	switch subject.(type) {
	case *SemanticNode:
		return nodeTag
	case *Vertex:
		return vertexTag
	case *Edge:
		return edgeTag
	case *Loop:
		return loopTag
	}
	return "entity"
}

// squareUnit is how the unit of an area is spelled: the linear unit of the
// frame with a 2 after it.
//
// It is a spelling rather than a conversion. An area tolerance is compared
// against a figure in the square of a frame's unit, and a registry which
// declared it in the length would be comparing a width against an area — so the
// two have to be told apart, and this is the one place which says how.
func squareUnit(unit Unit) Unit {
	if unit == "" {
		return ""
	}
	return unit + "2"
}
