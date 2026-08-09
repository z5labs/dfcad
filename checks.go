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
	claimAgreesWithGeometry{},
	containedAreasDoNotOverlap{},
	containedAreasSum{},
	crossFrameBudgetHolds{},
	edgeBackingResolves{},
	edgeEndpointsDiffer{},
	groundToGridStated{},
	requiredClaim{},
	sitsInside{},
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

	// predicateParameter is the predicate a check is about a quantity of: the
	// one a claim has to be written under, or the one carrying the value the
	// check compares something with.
	//
	// It is the subject's own quantity, which is what tells it apart from
	// positionParameter above: that one names a quantity of the subject's
	// corners.
	predicateParameter = "predicate"

	// kindParameter narrows a check over what a node contains to the contents of
	// one kind.
	kindParameter = "kind"

	// containerParameter is the node whose shape a subject has to lie inside.
	//
	// It is written rather than taken from what the subject is `within`, because
	// the shape a thing has to be inside of is not always the thing which
	// contains it: a storey's footprint is judged against the surveyed outline
	// of the building it stands for, and a storey is written within the
	// building rather than within the outline.
	containerParameter = "container"

	// crsParameter is the predicate the projected coordinate reference system a
	// chain is rooted at is named under.
	//
	// The engine neither resolves the identifier nor interprets it. What it is
	// used for here is a single question — does this model sit in a projection
	// at all — because a projection is the one thing which makes the difference
	// between a ground distance and a grid distance worth reporting.
	crsParameter = "crs"

	// groundToGridParameter is the predicate the combined ground-to-grid factor
	// is stated under.
	//
	// It is the project's own vocabulary and never the engine's
	// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)):
	// the factor is the grid scale factor times the elevation factor, it depends
	// on the project's height, and nothing here can derive it or default it.
	groundToGridParameter = "ground-to-grid"

	// areaParameter is how far two areas may differ, which is a second tolerance
	// and not the one shapes are read against: coincidence is a distance and a
	// discrepancy between areas is an area, and one figure cannot be both.
	areaParameter = "area-tolerance"

	// discrepancyParameter is how far a figure written down and the same figure
	// computed from a shape may differ before they disagree.
	//
	// It is not spelled area-tolerance because the figure it bounds is an area
	// on a subject bounded by loops and a length on one drawn as a line, and a
	// name which claimed to be an area would be wrong on half the subjects it is
	// written on.
	discrepancyParameter = "discrepancy"
)

// boundaryLoopsClose is the check that a loop is a closed cycle.
type boundaryLoopsClose struct{}

// Declare implements [Check].
func (boundaryLoopsClose) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name: "boundary-loops-close",
		Description: "Every loop bounding the subject closes: traversing its edges returns to the vertex it " +
			"started from, within the named tolerance. A loop every node bounded by it draws as a line is an open " +
			"run and is not asked to close.",
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
//
// A loop every node bounded by it draws as a line is passed over. The check
// declares that it does not apply to a node drawn as a line, and the loop that
// node references is the same shape reached from the other end: asking it to
// close would be this check answering two ways about one loop, and the answer a
// consuming registry would see is whichever of its two rules happened to route
// the loop. A loop nothing bounds is still asked to close — nothing says it is a
// run, and a loop nobody has referenced yet is a ring being written.
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
		if walkedAsRun(graph, loop) {
			continue
		}

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

// walkedAsRun reports whether a loop is walked as an open run rather than as a
// ring, which is true of one every node bounded by it draws as a line.
//
// Every, and not any. A loop shared by a room and a door is the room's boundary
// and has to close for the room's sake, and passing it over because something
// else read it as a chain would drop a real defect on the strength of an
// unrelated reference. That is a model saying two things about one loop, which
// [Topology.Assemble] and [Topology.AssembleRun] cannot both be right about — and
// a check which reported nothing would hide it rather than leave it to be found.
//
// A loop nothing bounds is not a run. Nothing has said what it is yet, and a
// loop being written is a ring until something says otherwise.
func walkedAsRun(graph *Graph, loop *Loop) bool {
	var bounded bool

	for node := range graph.Boundaries().Bounded(loop) {
		bounded = true
		if !drawnAsLine(node) {
			return false
		}
	}

	return bounded
}

// claimAgreesWithGeometry is the check that a measurement written down still
// matches the shape it describes.
type claimAgreesWithGeometry struct{}

// Declare implements [Check].
func (claimAgreesWithGeometry) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name: "claim-agrees-with-geometry",
		Description: "The measurement claimed of the subject under the named predicate agrees with the one its " +
			"shape computes to: an area for a subject bounded by loops, a length for one drawn as a line, and the " +
			"distance between its two corners for an edge.",
		Parameters: []CheckParameter{
			{
				Name:     predicateParameter,
				Type:     ParameterPredicate,
				Required: true,
				Description: "The predicate the claimed measurement is written under, which is the number the shape " +
					"is compared against.",
			},
			{
				Name:     positionParameter,
				Type:     ParameterPredicate,
				Required: true,
				Description: "The predicate a corner's position is claimed under, which is what the shape is read " +
					"from.",
			},
			{
				Name:        toleranceParameter,
				Type:        ParameterTolerance,
				Required:    true,
				Description: "How close two corners are one corner, which is what the shape is read against.",
			},
			{
				Name:     discrepancyParameter,
				Type:     ParameterTolerance,
				Required: true,
				Description: "How far the claim and the shape may differ before they disagree. It is a figure of what " +
					"is compared, so it is declared in the square of the frame's unit for an area — 0.05 m2 where the " +
					"frame is in m — and in the unit itself for a length.",
			},
		},
		Forms:      []SubjectForm{SubjectNode, SubjectEdge},
		Geometries: []Geometry{GeometryArea, GeometrySurface, GeometryLine},
	}
}

// Run implements [Runner].
//
// The failure this catches is quiet and entirely ordinary. A wall moves, the
// boundary follows it, and the area written down when the room was first
// measured stays where it was: both halves are well-formed, the conflict
// register has nothing to say because the geometry is not a claim, and the model
// is now wrong in a way nothing reports.
//
// # What is compared with what
//
// The claim is the one resolution makes current under the named predicate, which
// is what keeps a deprecated number out of this: a retracted claim is never
// resolved to ([Claims.Resolve]), and a retracted number is not a disagreement.
// The shape is recomputed from the corners' position claims every time this
// runs, which is what makes it unable to have gone stale
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// Which shape that is comes from the form the rule was written on. A node is
// measured through the loops which bound it — an area where it encloses one, a
// total length where it is drawn as a line — and an edge is the distance between
// the two corners it runs between. The edge is the most directly checkable
// measurement the format can express, because both ends are already in the
// model, and it is the one an outline never reaches: a span written on an edge
// which belongs to no loop is a legal thing to write and has no boundary for a
// node-bound rule to be about.
//
// # Uncertainty, and why a bare tolerance is not enough
//
// Two figures which differ by less than their combined uncertainty do not
// disagree. A claimed area at ±0.4 m² and a shape whose corners put its area
// within ±0.3 m² are consistent at a 0.5 m² gap and in conflict at 2 m², and a
// rule which compared them against a flat declared figure would report the first
// and miss the second on a model with tighter survey.
//
// So the declared discrepancy is the floor rather than the whole test: the two
// may differ by it, or by their combined one-sigma uncertainty where that is
// wider. Where either side states no accuracy the floor is the whole of it,
// which is what the floor is for — an unstated accuracy is unknown rather than
// zero ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)), and a band
// computed as though it were zero would report a disagreement the evidence
// cannot support.
//
// The two are combined in quadrature, as two separate measurements of one
// quantity. A systematic term written into both the claim and the corners is
// therefore counted twice rather than once, which widens the band; the two
// cannot be accumulated into one budget instead, because an area claim's
// accuracy is in the square of the unit its corners are surveyed in and
// [Budget.Combined] refuses to mix units — deliberately.
//
// The corners' budget is a distance and the figure compared may be an area, so
// for an area it is carried across by the length of the boundary: displacing a
// boundary of length P by δ changes the area it encloses by about P·δ. It is a
// first-order sensitivity and is stated as one. Nothing is written back and
// nothing about it reaches [Measurement.Budget], which reports the uncertainty
// of the corners and reduces it to no figure of its own.
//
// # What it declines to decide
//
// A subject carrying the claim and no shape, and one with a shape and no claim
// under the named predicate, are both left alone. There is nothing to compare in
// either, and a room somebody has drawn and not yet measured — or measured and
// not yet drawn — is an ordinary state of a model being written rather than a
// disagreement. An edge whose ends nobody has surveyed under the position
// predicate is the same state seen from the geometry's side: a span nothing can
// measure is not a span which disagrees. It is the corners' predicate and not
// the claim's which decides that one — the number is there and what is missing
// is somewhere to measure it against.
func (claimAgreesWithGeometry) Run(subject CheckSubject) []Failure {
	graph := subject.Graph()

	place, compares := comparing(graph, subject.Subject())
	if !compares {
		return nil
	}

	predicate, ok := symbolOf(subject, predicateParameter)
	if !ok {
		return nil
	}
	position, ok := symbolOf(subject, positionParameter)
	if !ok {
		return nil
	}
	tolerance, ok := symbolOf(subject, toleranceParameter)
	if !ok {
		return nil
	}
	discrepancy, ok := symbolOf(subject, discrepancyParameter)
	if !ok {
		return nil
	}

	declared, found := graph.Registry().Tolerance(discrepancy)
	if !found {
		// A rule naming a tolerance the registry does not declare is a load
		// error which names it and points at it. Reporting it again here would
		// be one mistake told twice, in the vocabulary of the rule rather than
		// of the registry.
		return nil
	}

	// The claim is read before the shape is measured, because a subject which
	// claims nothing under the predicate is left alone whatever its geometry
	// turns out to be.
	resolution, err := graph.Claims().Resolve(subject.Subject().ID(), predicate, graph.Registry())
	if err != nil {
		// Two equally current claims under a strict predicate are the conflict
		// register's to report, and comparing one of them against the shape
		// would be picking a winner this check has no rule for.
		return nil
	}

	claim, stated := currentClaim(resolution)
	if !stated {
		return nil
	}

	value := claim.Value()
	claimed, numeric := value.Scalar()
	if !numeric {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected the %s claimed of %s to be a number its shape could be compared against, found %s",
				predicate, place.name, describeShape(value.Shape()),
			),
			Hint: "a measurement which agrees or disagrees with a shape is one number; the predicate this rule names " +
				"carries something else, so the rule as written cannot be decided either way",
			Span:    claim.Span(),
			Related: place.related,
		}}
	}

	shape, failures := measuredGeometry(graph, subject.Subject(), tolerance, position)
	if len(failures) > 0 {
		return failures
	}

	if !shape.measured {
		return nil
	}

	if value.Unit() != shape.unit {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected the %s claimed of %s in %s, the unit its shape is measured in, found %s %s",
				predicate, place.name, shape.unit, decimal(claimed), value.Unit(),
			),
			Hint: "nothing here converts between units, so a claim written in one and a shape measured in another " +
				"are two figures which cannot be compared",
			Span:    claim.Span(),
			Related: place.related,
		}}
	}

	if declared.Unit != shape.unit {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected the discrepancy %s in %s, the unit the shape of %s is measured in, found %s %s",
				declared.Name, shape.unit, place.name, decimal(declared.Value), declared.Unit,
			),
			Hint: "how far a claim and a shape may differ is a figure of what they are figures of: an area for a " +
				"subject bounded by loops and a length for one drawn as a line, and nothing here converts between " +
				"the two. It is the (discrepancy ...) parameter rather than the (tolerance ...) one, which is a " +
				"distance and is what corners are judged coincident against",
			Span:    place.at,
			Related: []RelatedLocation{{Span: declared.Span, Message: "the discrepancy is declared here"}},
		}}
	}

	band := declared.Value
	if combined, known := agreementBand(claim, shape); known && combined > band {
		band = combined
	}

	difference := claimed - shape.value
	if math.Abs(difference) <= band {
		return nil
	}

	sense := "more than"
	if difference < 0 {
		sense = "less than"
	}

	return []Failure{{
		Message: fmt.Sprintf(
			"expected the %s claimed of %s to agree with %s, found %s%s claimed against %s%s "+
				"measured, which is %s%s %s %s",
			predicate, place.name, shape.wording.against,
			decimal(claimed), shape.suffix,
			decimal(shape.value), shape.suffix,
			decimal(math.Abs(difference)), shape.suffix, sense, shape.wording.noun,
		),
		Hint: fmt.Sprintf(
			"the shape is recomputed from the corners' %s claims, judged against the tolerance %s; the two may "+
				"differ by %s, which is %s %s, or by their combined uncertainty where that is wider — so either the "+
				"claim has gone stale or %s",
			position, tolerance, declared.Name, decimal(declared.Value), declared.Unit, shape.wording.blame,
		),
		Span:    claim.Span(),
		Related: place.related,
	}}
}

// compared is the half of a failure of this check which the form the rule was
// written on decides: what to call the subject, where its id is written, and
// where to send a reader for the geometry the claim was compared against.
//
// The three travel together because a failure which named one form and pointed
// at another's geometry would send a reader to a line which is not the one to
// change, and because the comparison between them is the same arithmetic
// whichever form carried the claim.
type compared struct {
	// name is what a message calls the subject.
	name string

	// at is where the subject's id is written, which is where a failure about
	// the rule rather than about the claim points.
	at Span

	// related is where the geometry the claim was compared against is written.
	related []RelatedLocation
}

// comparing is what a failure of this check says about the thing the rule was
// written on, and whether this check compares anything on that form at all.
//
// A form it does not compare on is left alone rather than reported. An assertion
// naming a check which cannot examine the thing it is written on is refused when
// the model is loaded ([ResolveAssertions]), so this is the guard behind that
// and never the diagnostic somebody reads.
func comparing(graph *Graph, entity Entity) (compared, bool) {
	switch subject := entity.(type) {
	case *SemanticNode:
		return compared{
			name:    nodeName(subject),
			at:      graph.Nodes().named(subject),
			related: boundaryOf(graph, subject),
		}, true
	case *Edge:
		return compared{
			name:    geometricName(edgeTag, subject.ID()),
			at:      graph.Topology().namedAt(subject.ID(), subject.Span()),
			related: endsOf(graph, subject),
		}, true
	}

	return compared{}, false
}

// measuredGeometry is the figure the subject's own geometry computes to, and
// what stopped it being read.
//
// A node is measured through the loops which bound it and an edge from its two
// ends, which is the whole of the difference between the two forms this check
// runs on. Everything past this point — the unit, the band, the sign of the gap
// — is one comparison.
func measuredGeometry(graph *Graph, entity Entity, tolerance, position string) (shape, []Failure) {
	switch subject := entity.(type) {
	case *SemanticNode:
		return measuredShape(graph, subject, tolerance, position)
	case *Edge:
		return measuredSpan(graph, subject, tolerance, position)
	}

	return shape{}, nil
}

// currentClaim is the claim a shape is compared against, and whether the model
// states one to compare it against at all.
//
// It is the claim resolution picked, and otherwise the one live claim resolution
// could not rank. A claim which states no accuracy is unrankable and so is never
// what [Resolution.Claim] reports, and skipping it here would leave every
// unmeasured number in a model exempt from this check — which is the one place
// a stale figure is most likely to be sitting. It is still what the model says
// about the subject; what it is not is a figure which can narrow the band, and
// the declared discrepancy is the floor which decides it instead.
//
// More than one candidate is left alone. Two equally current claims are a
// conflict the register reports, and comparing one of them against the shape
// would be picking a winner this check has no rule for.
func currentClaim(resolution Resolution) (*Claim, bool) {
	if claim, resolved := resolution.Claim(); resolved {
		return claim, true
	}

	if candidates := resolution.Candidates(); len(candidates) == 1 {
		return candidates[0], true
	}

	return nil, false
}

// shape is what a subject's geometry computes to, together with what says how
// well it is known.
//
// The figure and the unit it is in travel with the budget the corners put behind
// it, because the whole of what this check does is compare the three against a
// claim, and a caller which read the figure from one place and its uncertainty
// from another could compare a figure against a budget of something else.
type shape struct {
	// value is the figure the geometry computes to: an area for a subject
	// bounded by loops, a length for one drawn as a line.
	value float64

	// unit is the unit value is in, which is the square of the frame's linear
	// unit for an area and the linear unit itself for a length.
	unit Unit

	// suffix is how that unit is written after a figure in a message.
	suffix string

	// linear is the frame's linear unit, which is what the corners' budget is
	// accumulated in.
	linear Unit

	// sensitivity is how far value moves per unit of corner displacement: the
	// length of the boundary for an area, and one for a length.
	sensitivity float64

	// budget is the accumulated accuracy of the position claims value was
	// computed from.
	budget Budget

	// measured reports whether there is a figure at all. A subject which
	// references no loop has none, and that is not the same state as a figure
	// which came out zero.
	measured bool

	// wording is how a failure names what the figure was computed from.
	wording wording
}

// wording is the part of a disagreement's sentence which the geometry the figure
// came from decides.
//
// A node bounded by loops disagrees with an outline somebody drew; an edge
// disagrees with the two corners it runs between, and telling a reader to look
// at a boundary would send them to a form an edge need not belong to at all. The
// rest of the sentence — the two figures, the gap between them and which way it
// runs — is one comparison whatever was measured, so only the phrases which name
// the geometry travel with the shape.
type wording struct {
	// against is what the claim was expected to agree with, written after "to
	// agree with".
	against string

	// noun is what the measured figure is a figure of, written after "more
	// than" or "less than".
	noun string

	// blame is the other thing which may be wrong where the two disagree,
	// written after "either the claim has gone stale or".
	blame string
}

// drawn is how a failure words a figure computed from the loops which bound a
// node: the outline is the thing to look at, and it is a thing somebody drew.
//
// It is a function rather than a package variable because it is a constant and a
// struct cannot be one, and a variable holding it could be written to from
// anywhere in the package.
func drawn() wording {
	return wording{
		against: "the shape it is drawn as",
		noun:    "the shape",
		blame:   "the boundary is drawn wrong",
	}
}

// measuredShape computes what a subject's geometry says it measures, and reports
// what stopped it being read.
//
// The two geometries are read differently on purpose. A subject bounded by loops
// is measured as a region, which is what nests a courtyard inside a plate and
// takes it away. A subject drawn as a line is measured edge by edge instead:
// reading it as a region would assemble its edges into a ring and report the gap
// where the two ends do not meet, and a wall not being a closed cycle is what a
// line is rather than a mistake in one.
func measuredShape(graph *Graph, node *SemanticNode, tolerance, position string) (shape, []Failure) {
	survey := shapeSurvey(graph, tolerance, position, node)

	if geometry, _ := node.Geometry(); geometry == GeometryLine {
		return measuredLine(graph, node, survey)
	}

	measurement, diags := graph.Measure(node, survey)
	if len(diags) > 0 {
		return shape{}, failuresOf(diags)
	}

	area, computed := measurement.Area()
	if !computed {
		return shape{}, nil
	}

	// The boundary's length is how much the area moves when a corner does, and
	// a region which measured an area has one.
	perimeter, _ := measurement.Length()

	return shape{
		value:       area,
		unit:        squareUnit(measurement.Unit()),
		suffix:      squareSuffix(measurement.Unit()),
		linear:      measurement.Unit(),
		sensitivity: perimeter,
		budget:      measurement.Budget(),
		measured:    true,
		wording:     drawn(),
	}, nil
}

// measuredLine is the total length of the edges a subject drawn as a line is
// assembled from.
//
// It is every edge or none. A total summed over the edges which happened to
// measure is a line with a piece missing and no figure saying which piece, which
// is the answer which reads as an answer — the same refusal [Topology.MeasureRegion]
// makes of a region one of whose rings did not assemble.
func measuredLine(graph *Graph, node *SemanticNode, survey Survey) (shape, []Failure) {
	var (
		out      shape
		failures []Failure
		edges    int
		measured int
	)

	for edge := range graph.Boundaries().Edges(node) {
		edges++

		measurement, diags := graph.Measure(edge, survey)
		if len(diags) > 0 {
			failures = append(failures, failuresOf(diags)...)
			continue
		}

		length, computed := measurement.Length()
		if !computed {
			continue
		}

		measured++
		out.value += length
		out.unit, out.linear = measurement.Unit(), measurement.Unit()
		out.suffix = unitSuffix(measurement.Unit())
		out.budget.Merge(measurement.Budget())
	}

	if len(failures) > 0 {
		return shape{}, failures
	}

	if edges == 0 || measured != edges {
		return shape{}, nil
	}

	out.sensitivity, out.measured = 1, true
	out.wording = drawn()

	return out, nil
}

// measuredSpan is how far one edge reaches: the distance between the two corners
// it runs between, with the accuracy their position claims put behind it.
//
// It is [Graph.Measure]'s answer and not a subtraction of its own, which is what
// makes an edge that bends measure along its arc rather than across its chord. A
// second reading of an edge's length here would be a second answer to the
// question the whole engine already answers in one place.
//
// An end nothing places is no figure rather than a failure. A corner nobody has
// surveyed under the position predicate — the one the survey is built from, and
// never the predicate the claim is written under — is an ordinary state of a
// model being written, the same state as a room drawn and not yet measured, and
// a span which cannot be measured is not a span which disagrees. What is reported is
// what stopped a span whose ends *are* placed being read: a position in a unit
// the edge's frame is not in, two ends written with different numbers of
// components, an edge whose ends are at one point.
func measuredSpan(graph *Graph, edge *Edge, tolerance, position string) (shape, []Failure) {
	survey := positionSurvey(graph, tolerance, position, graph.Corners(edge))

	start, end := edge.Vertices()
	for _, corner := range []ID{start, end} {
		if _, placed := survey.Positions[corner]; !placed {
			return shape{}, nil
		}
	}

	measurement, diags := graph.Measure(edge, survey)
	if len(diags) > 0 {
		return shape{}, failuresOf(diags)
	}

	length, computed := measurement.Length()
	if !computed {
		return shape{}, nil
	}

	return shape{
		value:       length,
		unit:        measurement.Unit(),
		suffix:      unitSuffix(measurement.Unit()),
		linear:      measurement.Unit(),
		sensitivity: 1,
		budget:      measurement.Budget(),
		measured:    true,
		wording: wording{
			against: fmt.Sprintf("the corners it runs between, %s and %s", start, end),
			noun:    "the span",
			blame:   "an end of it has moved",
		},
	}, nil
}

// agreementBand is the combined one-sigma uncertainty of a claimed figure and
// the shape it is compared against, and whether either side stated an accuracy
// at all.
//
// A side which stated none contributes nothing rather than stopping the
// arithmetic, because the declared discrepancy is the floor under the answer and
// is what decides a comparison the evidence cannot narrow. Where neither side
// stated one there is no band here at all, and the floor is the whole of the
// test.
func agreementBand(claim *Claim, of shape) (float64, bool) {
	var own Budget
	own.Add(claim)

	claimed, stated := sigmaIn(own, of.unit)
	derived, surveyed := sigmaIn(of.budget, of.linear)

	if !stated && !surveyed {
		return 0, false
	}

	// The corners' budget is a distance and the figure may be an area, so it is
	// carried across by how far the figure moves per unit of corner
	// displacement.
	derived *= of.sensitivity

	return math.Sqrt(claimed*claimed + derived*derived), true
}

// sigmaIn is a budget as one standard uncertainty in the given unit, and whether
// it combines into one at all.
//
// A budget which is tainted by a claim stating no accuracy, which holds no term,
// or which accumulated in another unit is not a figure this can compare: none of
// those is zero, and reporting zero for any of them would narrow the band on
// evidence which does not support it.
func sigmaIn(budget Budget, unit Unit) (float64, bool) {
	uncertainty, err := budget.Combined()
	if err != nil || uncertainty.Unit != unit {
		return 0, false
	}
	return uncertainty.Standard(), true
}

// boundaryOf is where the outline a figure was computed from is written, for a
// failure which has to name both places: the claim, and the geometry it
// disagrees with.
//
// A subject bounded by nothing relates to nothing rather than to itself. The
// failure already points at the claim, and a related location repeating the node
// would send a reader to a line they are already looking at.
func boundaryOf(graph *Graph, node *SemanticNode) []RelatedLocation {
	var out []RelatedLocation
	for loop := range graph.Boundaries().Loops(node) {
		out = append(out, RelatedLocation{
			Span:    loop.Span(),
			Message: "the boundary it was compared against",
		})
	}
	return out
}

// endsOf is where the two corners an edge's span was measured between are
// written, for a failure which has to name both places: the claim, and the
// geometry it disagrees with.
//
// Both are named rather than the edge itself. A span which no longer matches
// what was written down is either a stale number or a corner which moved, and
// which of the two ends moved is exactly what the reader is about to go and
// find out.
//
// An end the model does not hold is left out rather than pointed at. An edge
// naming a corner nothing answers to is a load error against the edge, and a
// related location for it would be a second report of that in the vocabulary of
// a rule.
func endsOf(graph *Graph, edge *Edge) []RelatedLocation {
	start, end := edge.Vertices()

	var out []RelatedLocation
	for _, corner := range []ID{start, end} {
		vertex, held := graph.Topology().Vertex(corner)
		if !held {
			continue
		}

		out = append(out, RelatedLocation{
			Span:    graph.Topology().namedAt(vertex.ID(), vertex.Span()),
			Message: "a corner the span was measured between",
		})
	}

	return out
}

// describeShape names the sort of value a claim carries, for a diagnostic which
// wanted a number and found one of the other three.
func describeShape(of Shape) string {
	if of == "" {
		return "a value which could not be read"
	}
	return article(string(of)) + " " + string(of) + " value"
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

	// Everything summed has to be declared in the frame the whole is, and a
	// content which is not is refused rather than added in. A coordinate means
	// nothing without the frame it was written in, the transform between two
	// frames carries a scale, and nothing here converts between them
	// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)) — so an area
	// read in one frame added to an area read in another is a total which is a
	// figure of no shape. It is the same refusal [Region.Intersect] makes of two
	// operands in different frames, made here because a sum has no second
	// operand to make it of.
	var (
		parts     float64
		elsewhere []Failure
	)
	for _, one := range shapes {
		if one.region.Frame() == whole.Frame() {
			parts += one.region.Area()
			continue
		}

		elsewhere = append(elsewhere, Failure{
			Message: fmt.Sprintf(
				"expected everything summed into %s to be declared in %s, the frame it is drawn in, found %s in %s",
				nodeName(node), whole.Frame(), one.node.ID(), one.region.Frame(),
			),
			Hint: "nothing here converts between frames on its own: the transform between two of them is a " +
				"measurement with an accuracy of its own, so an area read in one is not a figure of the same " +
				"shape in the other",
			Span:    graph.Nodes().named(one.node),
			Related: []RelatedLocation{{Span: graph.Nodes().named(node), Message: "the node its area would be summed into"}},
		})
	}

	if len(elsewhere) > 0 {
		return elsewhere
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

// groundToGridStated is the check that a model rooted at a projected coordinate
// reference system says whether a distance in it is a ground distance or a grid
// distance.
//
// A projection distorts distance, and the combined factor — the grid scale
// factor times the elevation factor — is routinely the largest systematic term
// in a model of a site. At a hundred parts per million a millimetre budget is
// spent over ten metres, and across a three hundred metre site the same factor
// is tens of millimetres, so a model which does not say what the factor is has a
// term in it larger than everything it does say.
//
// This is a diagnostic and not a computation. The factor depends on the
// project's height, which no coordinate reference system carries, so nothing
// here derives it, consults a geodetic parameter for it, or offers a default —
// all it can do is notice that nobody has said what it is
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
type groundToGridStated struct{}

// Declare implements [Check].
func (groundToGridStated) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name: "ground-to-grid-stated",
		Description: "The chain the subject is measured in says how a distance in it relates to the projected " +
			"coordinate reference system it is rooted at: either the combined ground-to-grid factor is stated, " +
			"or a transform on the chain already carries it.",
		Parameters: []CheckParameter{
			{
				Name:        crsParameter,
				Type:        ParameterPredicate,
				Required:    true,
				Description: "The predicate the coordinate reference system the chain is rooted at is named under.",
			},
			{
				Name:     groundToGridParameter,
				Type:     ParameterPredicate,
				Required: true,
				Description: "The predicate the combined ground-to-grid factor is stated under, which is what says " +
					"a ground distance and a grid distance are the same thing or by how much they are not.",
			},
			{
				Name:     positionParameter,
				Type:     ParameterPredicate,
				Required: true,
				Description: "The predicate a corner's position is claimed under, which is what the extent an " +
					"unstated factor would act over is read from.",
			},
		},
		Forms: []SubjectForm{SubjectNode, SubjectVertex, SubjectEdge, SubjectLoop},
	}
}

// Run implements [Runner].
//
// Passing takes an affirmative statement rather than the absence of a
// contradiction. Silence is exactly the state this reports, so a chain which
// says nothing about the factor and a chain which says the factor is one would
// otherwise be the same answer — and they are the opposite answer: one is a
// project which decided, and the other is a project which has not looked.
//
// The two ways of saying it are a statement under the named predicate anywhere
// on the chain, and a transform on the chain whose scale is not one. The second
// is a statement because a scale in this format is a scale and never a unit
// conversion ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)): a fit
// which wrote one found a distance discrepancy and wrote down what it was.
//
// A model rooted at a frame which names no coordinate reference system does not
// apply. Ground and grid are the same thing outside a projection, so there is
// nothing there to state and reporting it would be reporting every local model
// ever drawn.
func (groundToGridStated) Run(subject CheckSubject) []Failure {
	crs, ok := symbolOf(subject, crsParameter)
	if !ok {
		return nil
	}
	factor, ok := symbolOf(subject, groundToGridParameter)
	if !ok {
		return nil
	}
	position, ok := symbolOf(subject, positionParameter)
	if !ok {
		return nil
	}

	graph := subject.Graph()
	thing := subject.Subject()

	from, declared := frameOf(thing)
	if !declared {
		return nil
	}

	chain := slices.Collect(graph.Frames().Chain(from))
	if len(chain) == 0 {
		return nil
	}
	root := chain[len(chain)-1]

	identifier, at, rooted := namedSystem(graph, root, crs)
	if !rooted {
		return nil
	}

	for _, frame := range chain {
		if statedOn(graph, frame, factor) {
			return nil
		}
	}

	// Every frame of the chain but the last, which is the root and is what the
	// others are expressed relative to. A transform which did not resolve is a
	// load error already reported, and it states nothing either way, so it is
	// not counted among the ones which say the factor is one.
	unscaled := 0
	for _, frame := range chain[:len(chain)-1] {
		transform, resolved := graph.Frames().Transform(frame.ID)
		if !resolved {
			continue
		}
		if statesScale(transform) {
			return nil
		}
		unscaled++
	}

	return []Failure{{
		Message: fmt.Sprintf(
			"expected the chain %s is measured in, rooted at %s under %s, to state whether a ground distance is a "+
				"grid distance, found %s and nothing written under %s",
			thing.ID(), root.ID, identifier, unscaledText(unscaled), factor,
		),
		Hint: groundToGridHint(graph, position, root, factor),
		Related: []RelatedLocation{
			{Span: at, Message: "the coordinate reference system is named here"},
			{Span: root.Span, Message: "the chain is rooted here"},
		},
	}}
}

// unscaledText says how many transforms between the subject and the root leave
// the distance alone, which is what makes the silence a silence rather than an
// omission.
func unscaledText(unscaled int) string {
	switch unscaled {
	case 0:
		return "no transform between it and the root to carry one"
	case 1:
		return "the one transform to it at a scale of exactly 1.0"
	default:
		return fmt.Sprintf("all %d transforms to it at a scale of exactly 1.0", unscaled)
	}
}

// groundToGridHint says what the unstated factor is worth over this particular
// model.
//
// It is a rate rather than an error, because the error cannot be computed: the
// factor is what is missing. What can be computed is how far the model reaches,
// and that turns the factor from a number in a geodetic table into the size of
// the mistake it makes here — which is the whole difference between a warning
// somebody acts on and one they scroll past.
func groundToGridHint(graph *Graph, position string, root Frame, factor string) string {
	remedy := fmt.Sprintf(
		"the factor is the grid scale factor times the elevation factor and depends on the project's height, which "+
			"no coordinate reference system carries, so nothing here derives it: state it as a claim under %s, or "+
			"carry it on the transform which georeferences the model",
		factor,
	)

	extent, sized := modelExtent(graph, position, root)
	if !sized || extent <= 0 {
		return remedy
	}

	return fmt.Sprintf(
		"the model spans %s%s between its furthest corners, so every part per million of unstated factor is %s%s "+
			"across it; %s",
		decimal(extent), unitSuffix(root.Unit), proportional(extent/1e6), unitSuffix(root.Unit), remedy,
	)
}

// modelExtent is how far apart the two furthest corners of the whole model are,
// read in the frame the chain is rooted at, and whether there were two corners
// to read it from.
//
// It is the whole model rather than the subject the rule is written on. A scale
// error is a property of the projection and not of any one room in it, so the
// figure worth reporting is the longest distance the factor would act over —
// which is between the corners furthest apart, wherever in the model they are.
//
// Corners in other frames are carried into the root's before they are compared,
// because a box assembled from coordinates in three frames measures nothing. One
// which cannot be carried is left out rather than mixed in.
func modelExtent(graph *Graph, position string, root Frame) (float64, bool) {
	survey := positionSurvey(graph, "", position, graph.Topology().Vertices())

	points := make([]Point, 0, len(survey.Positions))
	for vertex := range graph.Topology().Vertices() {
		value, placed := survey.Positions[vertex.ID()]
		if !placed {
			continue
		}

		components, isCoordinate := value.Coordinate()
		if !isCoordinate {
			continue
		}

		point := asPoint(components)
		if vertex.Frame() != root.ID {
			carried, err := graph.Frames().TransformPoint(point, vertex.Frame(), root.ID)
			if err != nil {
				continue
			}
			point = carried
		}

		points = append(points, point)
	}

	if len(points) < 2 {
		return 0, false
	}

	size := boxOf(points).Size()

	return math.Sqrt(size[0]*size[0] + size[1]*size[1] + size[2]*size[2]), true
}

// namedSystem is the coordinate reference system a frame names, where it was
// written, and whether it names one at all.
//
// Both spellings are read. Which of them a project uses is its registry's
// decision — an identifier carries no provenance and is ordinarily declared
// (claim-bearing #f), but a project which records who georeferenced it writes a
// claim — and a check which read only one would report half of them as sitting
// in no projection.
func namedSystem(graph *Graph, frame Frame, predicate string) (string, Span, bool) {
	for _, value := range frame.Plain(predicate) {
		if text, isText := value.Text(); isText {
			return text, value.Span(), true
		}
	}

	for claim := range graph.Claims().Under(frame.ID, predicate) {
		if text, isText := claim.Value().Text(); isText {
			return text, claim.Span(), true
		}
	}

	return "", Span{}, false
}

// statedOn reports whether a frame says anything at all under predicate, in
// either spelling.
//
// What was said is not read. The check is about silence, and a project which
// wrote the factor down has stopped being silent whatever number it wrote:
// judging the number would be the engine having an opinion about a geodetic
// quantity it has no way to check.
func statedOn(graph *Graph, frame Frame, predicate string) bool {
	if len(frame.Plain(predicate)) > 0 {
		return true
	}

	for range graph.Claims().Under(frame.ID, predicate) {
		return true
	}

	return false
}

// statesScale reports whether a transform's scale says a distance through it is
// not the distance it started as.
//
// A scale of zero is not a statement of anything. The form tables require the
// child, so a transform which loaded carries a number; one carrying zero is
// singular and is refused wherever it is used, and reading it here as "not one,
// therefore stated" would let a broken transform silence the whole rule.
func statesScale(transform Transform) bool {
	if transform.Scale == 0 || math.IsNaN(transform.Scale) || math.IsInf(transform.Scale, 0) {
		return false
	}
	return transform.Scale != 1.0
}

// sitsInside is the check that a node's shape lies inside another node's.
type sitsInside struct{}

// Declare implements [Check].
func (sitsInside) Declare() CheckDeclaration {
	return CheckDeclaration{
		Name: "sits-inside",
		Description: "The subject's shape lies inside the shape of the named node: nothing of it reaches past that " +
			"node's boundary by more than the named tolerance, or than the combined accuracy of the two where that " +
			"is wider.",
		Parameters: []CheckParameter{
			{
				Name:        containerParameter,
				Type:        ParameterID,
				Required:    true,
				Description: "The node whose shape the subject is to lie inside.",
			},
			{
				Name:        toleranceParameter,
				Type:        ParameterTolerance,
				Required:    true,
				Description: "How close two corners are one corner, and how far past a boundary is not past it at all.",
			},
			{
				Name:        positionParameter,
				Type:        ParameterPredicate,
				Required:    true,
				Description: "The predicate a position is claimed under, which is what both shapes are read from.",
			},
		},
		Forms:      []SubjectForm{SubjectNode},
		Geometries: []Geometry{GeometryPoint, GeometryLine, GeometryArea, GeometrySurface, GeometrySolid},
	}
}

// Run implements [Runner].
//
// This is the failure a containment which resolves cannot catch. A device
// written `within` a storey and set out thirty feet outside its footprint
// satisfies [withinResolves] — the containment is one the hierarchy permits —
// and satisfies every other rule it declares, because nothing until here
// compared the two shapes.
//
// # The container is named, never assumed
//
// It is a parameter rather than the `within` parent, and that is the difference
// between one check and two. A storey's footprint is judged against the
// surveyed outline of the building it stands for, and a storey is not written
// within that outline — it is written within the building. A rule which reached
// for the parent could say the first thing and never the second, and the two
// are the same question about two pairs of shapes.
//
// # What the subject is read as
//
// A subject bounded by loops is read as a region and compared with the
// container's by taking one away from the other, which is what nests a
// courtyard inside a plate and what makes the part left over the part outside.
// A subject drawn as a line or recorded as a point covers no area and has none
// to take away: it is read as the places the model puts it — the corners its
// edges run between, or the position claimed of the node itself — and each of
// those is judged against the container's boundary.
//
// The two are not the same rigour and the difference is worth stating. A run of
// a line between two corners inside the container can leave it and come back
// where the container is notched, and this does not report that: it reports
// where the model puts a corner. The fix for a container shaped like that is to
// draw the corner the notch needs, which is a corner the model wanted anyway.
//
// Where a failure says a thing reaches past the boundary is a coordinate of the
// container's own frame, written with as many components as the model wrote its
// positions with ([Region.printed]). The comparison is made in the plane's axes
// and a place written in those would be a pair of numbers appearing nowhere in
// the file; a reader has to be able to find the corner this is about.
//
// A point-based subject is judged in the container's plane, and a subject with a
// plane of its own is refused unless the two planes are one ([Region.Difference]
// refuses it). That is not an inconsistency: a device is above the floor plate
// it stands on and is inside its footprint, so requiring the two to be coplanar
// would refuse the whole motivating case, while two floor plates on different
// storeys are inside each other seen from above and are not inside each other.
//
// # Uncertainty
//
// The declared tolerance is the floor rather than the whole test, exactly as it
// is for [claimAgreesWithGeometry]. A shape reaching past a boundary by less
// than the combined one-sigma uncertainty of the two shapes has not been shown
// to be outside it, and reporting it would be reporting the survey rather than
// the model. Where neither side states an accuracy the floor is the whole of it,
// because an unstated accuracy is unknown rather than zero
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)).
func (sitsInside) Run(subject CheckSubject) []Failure {
	node, ok := subject.Subject().(*SemanticNode)
	if !ok {
		return nil
	}

	written, ok := symbolOf(subject, containerParameter)
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

	container, held := graph.Node(ID(written))
	if !held {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected a container this model holds, found %s, which names no node of it",
				written,
			),
			Hint: "the node a subject sits inside is named rather than taken from what contains it, so that a " +
				"footprint can be judged against an outline it is not written within; it is a node of this model " +
				"with a shape of its own",
			Span: graph.Nodes().named(node),
		}}
	}

	enclosing, failures := shapeOf(graph, container, tolerance, position)
	if len(failures) > 0 {
		return failures
	}

	if !enclosing.ready {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected the container %s to have a shape to sit inside, found no loop bounding it",
				container.ID(),
			),
			Hint: "a node with no outline encloses nothing, so nothing can be inside it; the rule as written cannot " +
				"be decided either way",
			Span:    graph.Nodes().named(node),
			Related: pointingAt(graph, container, "the container it names is written here"),
		}}
	}

	if geometry, _ := node.Geometry(); geometry == GeometryPoint || geometry == GeometryLine {
		return sittingPoints(graph, node, container, enclosing, position)
	}

	return sittingRegion(graph, node, container, enclosing, tolerance, position)
}

// sittingRegion decides a subject bounded by loops: what of it the container
// does not cover, and how far past the boundary the furthest corner of that
// reaches.
func sittingRegion(
	graph *Graph,
	node, container *SemanticNode,
	enclosing Region,
	tolerance, position string,
) []Failure {
	shape, failures := shapeOf(graph, node, tolerance, position)
	if len(failures) > 0 {
		return failures
	}

	if !shape.ready {
		return []Failure{{
			Message: fmt.Sprintf(
				"expected a shape on %s to judge against the container %s, found no loop bounding it",
				nodeName(node), container.ID(),
			),
			Hint: "a node whose type declares an area and which references no loop has no outline to compare; the " +
				"rule holds of a shape and there is none to hold it of",
			Span: graph.Nodes().named(node),
		}}
	}

	beyond, diags := shape.Difference(enclosing)
	if len(diags) > 0 {
		return failuresOf(diags)
	}

	if beyond.Empty() {
		return nil
	}

	at, depth := enclosing.deepest(beyond)

	declared := enclosing.Tolerance()
	if depth <= insideBand(declared, shape.Budget(), enclosing.Budget()) {
		return nil
	}

	return []Failure{{
		Message: fmt.Sprintf(
			"expected %s to sit inside %s, found %s%s of it outside, reaching %s%s past the boundary at %s",
			nodeName(node), container.ID(),
			decimal(beyond.Area()), squareSuffix(enclosing.Unit()),
			decimal(depth), unitSuffix(enclosing.Unit()), pointText(at, enclosing.printed()),
		),
		Hint:    outsideHint(declared, position),
		Span:    graph.Nodes().named(node),
		Related: pointingAt(graph, container, "the container it is to sit inside is written here"),
	}}
}

// sittingPoints decides a subject the model puts somewhere rather than draws an
// outline for: the corners a line runs between, or the position claimed of a
// node recorded as a point.
//
// The furthest placement outside is the one reported, and it is one failure
// rather than one per corner. What a reader has to do about a wall half outside
// a storey is decided by how far the worst of it is out; a failure per corner
// would say the same thing as many times as the wall has corners, and the
// number to act on would be spread across all of them.
func sittingPoints(
	graph *Graph,
	node, container *SemanticNode,
	enclosing Region,
	position string,
) []Failure {
	placements, budget, failures := placementsOf(graph, node, container, enclosing, position)
	if len(failures) > 0 {
		return failures
	}

	// The boundary is projected into its own axes once and every placement is
	// asked against that. A wall has as many corners as it has turns, and
	// re-projecting the container's whole outline for each of them would be the
	// whole of the work this does.
	figure := enclosing.figure(enclosing.basis)

	var (
		furthest placement
		depth    float64
		outside  bool
	)

	for _, one := range placements {
		at := enclosing.basis.project(one.at)
		if enclosedBy(at, figure) {
			continue
		}

		if away := nearestTo(at, figure); !outside || away > depth {
			furthest, depth, outside = one, away, true
		}
	}

	declared := enclosing.Tolerance()
	if !outside || depth <= insideBand(declared, budget, enclosing.Budget()) {
		return nil
	}

	return []Failure{{
		Message: fmt.Sprintf(
			"expected %s to sit inside %s, found %s %s%s outside the boundary, at %s",
			nodeName(node), container.ID(), furthest.name,
			decimal(depth), unitSuffix(enclosing.Unit()), pointText(furthest.at, enclosing.printed()),
		),
		Hint:    outsideHint(declared, position),
		Span:    furthest.span,
		Related: pointingAt(graph, container, "the container it is to sit inside is written here"),
	}}
}

// placement is one position the model puts a subject at: where it is, what a
// failure calls it, and where to send a reader to move it.
//
// The name is what tells the two point-based forms apart in a report. A line is
// out of its container at a corner somebody can go and re-survey, so the corner
// is named; a node recorded as a point is out of it at itself, and a message
// which named it a second time would be repeating the subject it just named.
type placement struct {
	at   Point
	name string
	span Span
}

// placementsOf reads where the model puts a subject which covers no area,
// together with the accuracy of the claims which put it there.
//
// A subject the model places nowhere is reported rather than passed. A device
// nobody has set out yet is not a device inside its storey, and answering "it
// does not reach past the boundary" about a position which does not exist is
// how a gate reports a model sound because it measured nothing.
//
// Every corner which could not be read is reported and not only the first. A
// run of wall nobody has surveyed is a list of corners to go and occupy, and
// naming one of them per run turns fixing it into a loop.
func placementsOf(
	graph *Graph,
	node, container *SemanticNode,
	enclosing Region,
	position string,
) ([]placement, Budget, []Failure) {
	var budget Budget

	if geometry, _ := node.Geometry(); geometry == GeometryPoint {
		frame, declared := node.Frame()
		if !declared || frame != enclosing.Frame() {
			mismatch := frameMismatch(graph, node, nodeName(node), frame, container, enclosing)
			return nil, budget, []Failure{mismatch}
		}

		claim, at, placed := placedAt(graph, node.ID(), position)
		if !placed {
			where := graph.Nodes().named(node)
			return nil, budget, []Failure{unplaced(graph, node, nodeName(node), container, position, where)}
		}

		budget.Add(claim)

		return []placement{{at: at, name: "it", span: claim.Span()}}, budget, nil
	}

	var (
		placements []placement
		failures   []Failure
		corners    int
	)

	for vertex := range graph.Corners(node) {
		corners++

		corner, where := string(vertex.ID()), graph.named(vertex)

		if vertex.Frame() != enclosing.Frame() {
			failures = append(failures,
				frameMismatch(graph, node, corner, vertex.Frame(), container, enclosing))
			continue
		}

		claim, at, placed := placedAt(graph, vertex.ID(), position)
		if !placed {
			failures = append(failures, unplaced(graph, node, corner, container, position, where))
			continue
		}

		budget.Add(claim)
		placements = append(placements, placement{at: at, name: corner, span: where})
	}

	switch {
	case len(failures) > 0:
		return nil, budget, failures

	case corners == 0:
		return nil, budget, []Failure{{
			Message: fmt.Sprintf(
				"expected a shape on %s to judge against the container %s, found no edge drawing it",
				nodeName(node), container.ID(),
			),
			Hint: "a node whose type declares a line and which references no loop is drawn nowhere; the rule holds " +
				"of a shape and there is none to hold it of",
			Span: graph.Nodes().named(node),
		}}
	}

	return placements, budget, nil
}

// placedAt is where one thing's current claim under a predicate puts it, and
// whether one puts it anywhere at all.
//
// A value which is not a coordinate places nothing. It is the same state as no
// claim at all as far as this is concerned — there is no position to judge —
// and the predicate declaring the wrong shape is the registry's to report.
func placedAt(graph *Graph, subject ID, position string) (*Claim, Point, bool) {
	resolution, err := graph.Claims().Resolve(subject, position, graph.Registry())
	if err != nil {
		return nil, Point{}, false
	}

	claim, stated := currentClaim(resolution)
	if !stated {
		return nil, Point{}, false
	}

	components, coordinate := claim.Value().Coordinate()
	if !coordinate {
		return nil, Point{}, false
	}

	var at Point
	copy(at[:], components)

	return claim, at, true
}

// unplaced is the failure for a subject the rule cannot find a position for.
func unplaced(
	graph *Graph,
	node *SemanticNode,
	what string,
	container *SemanticNode,
	position string,
	span Span,
) Failure {
	return Failure{
		Message: fmt.Sprintf(
			"expected a position claimed of %s under %s to judge against the container %s, found none",
			what, position, container.ID(),
		),
		Hint: "a subject which covers no area is where the model says it is; with nothing said under that predicate " +
			"there is no place to judge, and the rule as written cannot be decided either way",
		Span:    span,
		Related: pointingAt(graph, node, "the rule is written here"),
	}
}

// frameMismatch is the failure for a position written in one frame and a
// container drawn in another.
//
// Nothing here converts between frames on its own
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)), so two coordinates
// in two frames are two numbers rather than two places, and judging one against
// the other would be an answer rather than a refusal. It is the refusal
// [Region.comparable] makes of two regions, made of a point.
func frameMismatch(
	graph *Graph,
	node *SemanticNode,
	what string,
	frame ID,
	container *SemanticNode,
	enclosing Region,
) Failure {
	found := "no frame at all"
	if frame != "" {
		found = string(frame)
	}

	return Failure{
		Message: fmt.Sprintf(
			"expected %s to be declared in %s, the frame the container %s is drawn in, found %s",
			what, enclosing.Frame(), container.ID(), found,
		),
		Hint: "nothing here converts between frames on its own: the transform between two of them is a measurement " +
			"with an accuracy of its own, so a position in one frame and an outline in another are two numbers " +
			"rather than two places",
		Span:    graph.Nodes().named(node),
		Related: pointingAt(graph, container, "the container it names is written here"),
	}
}

// pointingAt is the one related location a failure of this check sends a reader
// to, which is always a node written somewhere in the model.
func pointingAt(graph *Graph, node *SemanticNode, message string) []RelatedLocation {
	return []RelatedLocation{{Span: graph.Nodes().named(node), Message: message}}
}

// insideBand is how far past a boundary is not past it at all: the declared
// tolerance, or the combined one-sigma uncertainty of the two shapes where that
// is wider.
//
// The two are combined in quadrature, as two separate measurements of where one
// boundary is relative to another. A side which stated no accuracy contributes
// nothing rather than stopping the arithmetic, because the declared tolerance is
// the floor under the answer and is what decides a comparison the evidence
// cannot narrow.
func insideBand(declared Tolerance, subject, container Budget) float64 {
	own, surveyed := sigmaIn(subject, declared.Unit)
	theirs, drawn := sigmaIn(container, declared.Unit)

	if !surveyed && !drawn {
		return declared.Value
	}

	return math.Max(declared.Value, math.Sqrt(own*own+theirs*theirs))
}

// outsideHint is what to do about a shape which reaches past its container,
// which is the same advice whichever way the subject was read.
func outsideHint(declared Tolerance, position string) string {
	return fmt.Sprintf(
		"both shapes are read from the %s claims and judged against the tolerance %s; nothing reaching past the "+
			"boundary by less than %s %s, or than the combined uncertainty of the two where that is wider, is "+
			"reported — so this is further out than the survey can account for",
		position, declared.Name, decimal(declared.Value), declared.Unit,
	)
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
				Name:        predicateParameter,
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
	survey := shapeSurvey(graph, tolerance, position, node)

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

// shapeSurvey is [positionSurvey] for one node, over everything the model
// places that node by: the corners its loops reach, and its own position where
// its declared geometry is the one whose whole shape is a coordinate.
//
// It is one call rather than the corner walk written out at each site, for the
// reason [Graph.Corners] and [Graph.Located] are a pair: a survey which held
// one and not the other reads every room of a floor and none of the devices
// standing in it, and reports each of those devices as a thing nothing places.
func shapeSurvey(graph *Graph, tolerance, position string, node *SemanticNode) Survey {
	survey := positionSurvey(graph, tolerance, position, graph.Corners(node))
	if position == "" {
		return survey
	}

	located, ok := graph.Located(node)
	if !ok {
		return survey
	}

	resolution, err := graph.Claims().Resolve(located.ID(), position, graph.Registry())
	if err != nil {
		return survey
	}

	survey.Place(located.ID(), resolution)

	return survey
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

// failuresOf carries every diagnostic a measurement raised into the failures of
// the check which asked for it, keeping the order they were reported in.
func failuresOf(diags []Diagnostic) []Failure {
	out := make([]Failure, 0, len(diags))
	for _, diagnostic := range diags {
		out = append(out, failureOf(diagnostic))
	}
	return out
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
