// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"math"
)

// This file is how a check reads a curved edge, and what a rule says about the
// ones it did not read.
//
// Every check which reads a shape read every edge as the straight line between
// its two ends until this, and said nothing. An easement whose boundary bows
// toward a lot was read short of the bow, and a shed standing in it passed a
// rule whose whole job is keeping it out — the permissive direction, on the one
// check a gate trusts to say no. Which way a check errs over a chord depends on
// which side of it the question sits, so no check can be trusted to err safely
// and the only answer which holds is the one the commands already give: read the
// curve where the vocabulary it is written in has been named, and say so where
// it has not.
//
// The vocabulary is the rule's to name, as every other name a check reads is
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md),
// [0012](docs/decisions/0012-tolerances-are-registry-data.md)): the predicates
// an arc's centre and a point on it are claimed under, and the tolerance a curve
// is drawn to where an overlay needs straight segments. None of the three has a
// default, and none of them is a flag on the command — a rule is one line of a
// model, and a rule whose answer changed with how it was invoked would be a gate
// whose meaning lived outside the file it is written in.

// The parameters a check which reads a shape takes to read a curve, spelled once
// for the same reason the others at the top of checks.go are: two checks asking
// for one thing ask for it under one name.
const (
	// arcCentreParameter is the predicate the centre of the arc an edge bends
	// along is claimed under, on the edge.
	arcCentreParameter = "arc-centre"

	// arcThroughParameter is the predicate a point the arc passes through is
	// claimed under, on the edge, which is what says which of the two arcs
	// between its ends is meant ([Arc.Through]).
	arcThroughParameter = "arc-through"

	// chordParameter is the tolerance a curve is drawn to where the question
	// cannot be answered without straight segments: an overlay, and the nesting
	// of rings one of which bends.
	chordParameter = "chord"
)

// curveParameters are the three optional parameters every check which reads a
// shape declares, in the order a listing prints them.
//
// They are optional because a model which claims no curve has nothing for them
// to read, and every rule written before a check could read one is a rule over
// straight edges; requiring them would refuse all of those for the want of a
// name nothing would apply. What is not optional is being told: a rule which
// names none of them over a shape which states a curve reports that edge as read
// straight ([Rule.Chorded]).
func curveParameters() []CheckParameter {
	return []CheckParameter{
		{
			Name:     arcCentreParameter,
			Type:     ParameterPredicate,
			Required: false,
			Description: "The predicate the centre of the arc an edge bends along is claimed under. Named together " +
				"with arc-through, an edge claiming both is read as its arc; left out, it is read as the straight " +
				"line between its ends and the run says so.",
		},
		{
			Name:     arcThroughParameter,
			Type:     ParameterPredicate,
			Required: false,
			Description: "The predicate a point the arc passes through is claimed under, which says which of the two " +
				"arcs between an edge's ends is meant. Named together with arc-centre.",
		},
		{
			Name:     chordParameter,
			Type:     ParameterTolerance,
			Required: false,
			Description: "How far a straight segment standing in for a curve may fall from it, where the answer " +
				"cannot be reached without straight segments: an overlay between two shapes, and the nesting of " +
				"rings one of which bends. The run reports what the curve was drawn to and the deviation achieved.",
		},
	}
}

// curves is the vocabulary one rule reads a curved edge under.
type curves struct {
	centre  string
	through string
	chord   string
}

// curvesOf reads the vocabulary a rule names.
func curvesOf(subject CheckSubject) curves {
	var named curves
	named.centre, _ = symbolOf(subject, arcCentreParameter)
	named.through, _ = symbolOf(subject, arcThroughParameter)
	named.chord, _ = symbolOf(subject, chordParameter)
	return named
}

// named reports whether the rule named both predicates an arc is written under,
// which is what reading one takes.
func (c curves) named() bool { return c.centre != "" && c.through != "" }

// halfNamed is the failure for a rule naming one of the two predicates an arc is
// written under and not the other, and nothing where it named both or neither.
//
// It is a failure rather than a rule quietly reading nothing. An arc needs its
// centre and a point on it ([Arc.Through]), so one alone bends nothing — and a
// rule whose author wrote one had meant to read the curve, and would otherwise
// be told only that it was read straight, which is the thing they tried to fix.
func (c curves) halfNamed(subject CheckSubject) []Failure {
	written, missing := arcCentreParameter, arcThroughParameter
	switch {
	case c.centre != "" && c.through == "":
	case c.through != "" && c.centre == "":
		written, missing = arcThroughParameter, arcCentreParameter
	default:
		return nil
	}

	return []Failure{{
		Message: fmt.Sprintf(
			"expected (%s ...) beside (%s ...), found only (%s ...)",
			missing, written, written,
		),
		Hint: "an arc is read from its centre and a point it passes through, so one of the two predicates bends " +
			"nothing; name both, or neither and have the curve read as its chord",
		unread: true,
	}}
}

// bend records in a survey the arc every edge bounding the subjects bends along,
// where the rule named the vocabulary and the edge claims both halves of it.
//
// An edge whose claims do not both resolve is left straight here and is not
// refused. [Rule.Chorded] is what says so, naming the edge, which is more than a
// failure raised in the middle of reading a shape could.
//
// The chord is set only where a curve can be read at all. A rule which named a
// chord and no arc has nothing to draw to it, and asking the overlay to read a
// tolerance it will never apply would refuse a straight shape for a unit mistake
// in a number nothing uses.
func (c curves) bend(graph *Graph, survey *Survey, subjects ...Entity) {
	if !c.named() {
		return
	}

	survey.Chord = c.chord

	registry := graph.Registry()
	for _, subject := range subjects {
		for edge := range graph.Bounding(subject) {
			if edge == nil {
				continue
			}

			at, err := graph.Claims().Resolve(edge.ID(), c.centre, registry)
			if err != nil {
				continue
			}

			on, err := graph.Claims().Resolve(edge.ID(), c.through, registry)
			if err != nil {
				continue
			}

			survey.Bend(edge.ID(), at, on)
		}
	}
}

// curveReader is a check which reads the shape of something, and says of which
// things for one subject.
//
// It is what lets a rule say which curves it will read straight before it runs
// as well as after: the things are the rule's subject and whatever it names — a
// container, a zone, the contents it sums — and the question of which of their
// edges state a curve is one about the model, answered the same way whether the
// rule is listed or run.
type curveReader interface {
	Runner

	// reads returns the things whose shapes the check reads for the subject,
	// in the order it reads them. A thing the rule names which the model does
	// not hold is left out: the check reports that itself, as a failure.
	reads(subject CheckSubject) []Entity
}

// curveLog is what one run of a rule drew its curves to.
type curveLog struct {
	tolerance Tolerance
	deviation float64
	unit      Unit
}

// drawnShape is a figure read from a shape which may have drawn a curve on the
// way: a [Region] and a [Measurement] alike.
type drawnShape interface {
	ChordTolerance() (Tolerance, bool)
	Deviation() float64
	Unit() Unit
}

// drew records that a shape the check read was drawn to a chord tolerance, and
// how far the worst segment of it fell from its curve.
//
// It is a record of what happened rather than of what was asked for: a shape
// with nothing curved in it is drawn to nothing, and reporting a tolerance for it
// would say a boundary had been approximated which was not.
func (s CheckSubject) drew(shapes ...drawnShape) {
	if s.log == nil {
		return
	}

	for _, shape := range shapes {
		tolerance, drawn := shape.ChordTolerance()
		if !drawn {
			continue
		}

		s.log.tolerance = tolerance
		s.log.unit = shape.Unit()
		s.log.deviation = math.Max(s.log.deviation, shape.Deviation())
	}
}

// ReadsArcs reports whether the rule names the vocabulary a curved edge is
// written in, which is whether an edge its shapes are bounded by and which
// claims an arc under those names is read as that arc.
//
// It is false for a rule whose check reads no shape, which has no curve to read.
func (r Rule) ReadsArcs() bool {
	if _, reads := r.runner.(curveReader); !reads {
		return false
	}
	return curvesOf(r.subject()).named()
}

// Chorded returns every edge this rule reads as the straight line between its
// ends although the model states a curve on it: an edge bounding a shape the
// rule reads, which claims a position the rule does not read as an arc.
//
// It is a question about the model and the rule and not about a run, so it has
// the same answer before a run as after, and a gate can read which of its passes
// will rest on an unread arc without running one. The rule either named no arc
// vocabulary, or named one the edge does not state both halves of.
//
// The order is by edge id, for the reason [Graph.UnreadArcs] gives. A rule whose
// check reads no shape, or which cannot run, reads nothing straight.
func (r Rule) Chorded() []ChordedEdge {
	if !r.Runs() {
		return nil
	}

	reader, reads := r.runner.(curveReader)
	if !reads {
		return nil
	}

	subject := r.subject()
	named := curvesOf(subject)
	things := reader.reads(subject)

	survey := Survey{Registry: r.graph.Registry()}
	named.bend(r.graph, &survey, things...)

	unread, _ := r.graph.UnreadArcs(survey, things...)
	if len(unread) == 0 {
		return nil
	}

	written := r.written()

	var instance ID
	var at Span
	if r.Subject != nil {
		instance = r.Subject.ID()
		at = r.Subject.Span()
	}

	out := make([]ChordedEdge, 0, len(unread))
	for _, arc := range unread {
		entry := ChordedEdge{
			Instance:   instance,
			Type:       r.Type,
			Check:      r.Check.Name,
			Arguments:  written,
			Declared:   r.Declared,
			Subject:    at,
			Predicates: arc.Predicates(),
			Span:       arc.Span(),
			vocabulary: named,
		}
		if edge := arc.Edge(); edge != nil {
			entry.Edge = edge.ID()
		}
		out = append(out, entry)
	}

	return out
}

// subject is what the check is handed when this rule runs.
func (r Rule) subject() CheckSubject {
	return CheckSubject{graph: r.graph, subject: r.Subject, arguments: r.Arguments, declaredBy: r.Type}
}

// written is the rule's arguments, each rendered the way it was written.
func (r Rule) written() []string {
	out := make([]string, 0, len(r.Arguments))
	for _, argument := range r.Arguments {
		out = append(out, argument.String())
	}
	return out
}

// ChordedEdge is one edge a rule read as the straight line between its ends
// although the model states a curve on it, with the rule which read it.
//
// It is to [UnreadArc] what [AppliedBand] is to [Band]: the model says the edge
// bends, and the run says which rule decided without reading it — because the
// fix is on the rule, and a rule written once as an invariant and bound to a
// hundred instances is one whose disclosure has to lead back to where it is
// declared.
//
// It is reported on a pass as much as on a failure, which is the point of it. A
// check which reads a curve straight answers a question about the chord, and a
// pass over the chord is indistinguishable from a real one by everything else a
// run says.
type ChordedEdge struct {
	// Instance is the id of the thing the rule is bound to.
	Instance ID `json:"instance"`

	// Type is the type which declared the rule, and is empty for an assertion.
	Type string `json:"type,omitempty"`

	// Check is the check name the rule names.
	Check string `json:"check"`

	// Arguments are the parameters the rule is written with, each rendered as
	// it was written.
	Arguments []string `json:"arguments,omitempty"`

	// Declared is where the rule is written.
	Declared Span `json:"declared"`

	// Subject is where the thing the rule is bound to is written.
	Subject Span `json:"subject"`

	// Edge is the id of the edge read straight.
	Edge ID `json:"edge"`

	// Predicates are the predicates the edge states a position under, in name
	// order, which is what to name on the rule to have the curve read.
	Predicates []string `json:"predicates"`

	// Span is where that edge is written.
	Span Span `json:"span"`

	// vocabulary is what the rule named to read a curve under, which decides
	// what to do about it: name it, finish naming it, or claim the edge under
	// what was named.
	vocabulary curves
}

// Diagnostic renders the finding as the warning a person reads.
//
// It is a warning rather than an error for the reason [Graph.UnreadArcs] gives:
// reading a curve as its chord is a decision worth reporting, and one a rough
// rule can want. What is not on offer is not saying.
func (c ChordedEdge) Diagnostic() Diagnostic {
	rule := fmt.Sprintf("the assertion %s on %s", c.written(), c.Instance)
	declared := "the assertion is written here"
	if c.Type != "" {
		rule = fmt.Sprintf("the invariant %s of the type %s, on %s", c.written(), c.Type, c.Instance)
		declared = fmt.Sprintf("the type %s declares the invariant here, for every instance of it", c.Type)
	}

	hint := fmt.Sprintf(
		"an edge has no position of its own, so a position claimed on one is the arc it bends along; name the "+
			"predicates it is written under on the rule — (%s ...) and (%s ...), with (%s ...) for a check which "+
			"draws it — and the rule reads the curve, until then every figure it decides against is the chord's",
		arcCentreParameter, arcThroughParameter, chordParameter,
	)
	switch {
	case c.vocabulary.named():
		hint = fmt.Sprintf(
			"the rule reads an arc under %s and %s, and this edge does not resolve a claim under both of them; until "+
				"it does, every figure the rule decides against is the chord's",
			c.vocabulary.centre, c.vocabulary.through,
		)
	case c.vocabulary.centre != "" || c.vocabulary.through != "":
		hint = fmt.Sprintf(
			"the rule names one of the two predicates an arc is read under, and an arc is its centre and a point it "+
				"passes through; name both — (%s ...) and (%s ...) — and the rule reads the curve",
			arcCentreParameter, arcThroughParameter,
		)
	}

	return Diagnostic{
		Severity: SeverityWarning,
		Span:     c.Span,
		Message: fmt.Sprintf(
			"expected %s to read %s as the arc it states, found it read as the straight line between its ends, "+
				"with a position claimed on it under %s",
			rule, geometricName(edgeTag, c.Edge), join(c.Predicates, "and"),
		),
		Hint:    hint,
		Related: []RelatedLocation{{Span: c.Declared, Message: declared}},
	}
}

// written renders the rule as its check name followed by its parameters.
func (c ChordedEdge) written() string {
	return Violation{Check: c.Check, Arguments: c.Arguments}.Written()
}

// DrawnCurve is the chord tolerance one rule drew its curves to, and the
// deviation that drawing achieved, with the rule which drew them.
//
// A check deciding by an overlay reads a curve through straight segments, and
// where it did the answer is about the drawing and not about the arc: within the
// deviation, and no closer. That is a decision the rule took and stated rather
// than an error, and it is reported for the reason a band is — the figure the
// rule names is not the whole of what decided the answer, and a pass is where
// that goes unsaid.
type DrawnCurve struct {
	// Instance is the id of the thing the rule ran against.
	Instance ID `json:"instance"`

	// Type is the type which declared the rule, and is empty for an assertion.
	Type string `json:"type,omitempty"`

	// Check is the check name the rule names.
	Check string `json:"check"`

	// Arguments are the parameters it ran with, each rendered as it was
	// written.
	Arguments []string `json:"arguments,omitempty"`

	// Declared is where the rule is written.
	Declared Span `json:"declared"`

	// Subject is where the thing it ran against is written.
	Subject Span `json:"subject"`

	// Chord is the name of the tolerance the curves were drawn to, and Value
	// its declared value.
	Chord string  `json:"chord"`
	Value float64 `json:"value"`

	// Deviation is how far the worst segment of the drawing fell from the curve
	// it stands in for: what was achieved, which is never more than Value.
	Deviation float64 `json:"deviation"`

	// Unit is what Value and Deviation are in, the linear unit of the frame.
	Unit Unit `json:"unit"`
}

// drawnBy attaches the rule to what one of its runs drew, and reports whether it
// drew anything.
func (r Rule) drawnBy(log *curveLog) (DrawnCurve, bool) {
	if log == nil || log.tolerance.Name == "" {
		return DrawnCurve{}, false
	}

	var (
		instance ID
		at       Span
	)
	if r.Subject != nil {
		instance = r.Subject.ID()
		at = r.Subject.Span()
	}

	return DrawnCurve{
		Instance:  instance,
		Type:      r.Type,
		Check:     r.Check.Name,
		Arguments: r.written(),
		Declared:  r.Declared,
		Subject:   at,
		Chord:     log.tolerance.Name,
		Value:     log.tolerance.Value,
		Deviation: log.deviation,
		Unit:      log.unit,
	}, true
}

// chordedViolation says in a violation that the rule which raised it read a
// curve straight.
//
// A failure decided over a chord is a failure about a shape the model does not
// state. It is still reported — the rule failed as written — and it says so in
// its own message, so that a reader who sees "found 100 ft² measured" beside a
// claim of 139.27 is told the hundred is the chord's before going to re-survey a
// boundary which is right.
//
// A failure about how the rule is written compared no figure, and says nothing
// of the kind.
func chordedViolation(violation Violation, failure Failure, chorded []ChordedEdge) Violation {
	if len(chorded) == 0 || failure.unread {
		return violation
	}

	edges := make([]string, 0, len(chorded))
	related := make([]RelatedLocation, 0, len(violation.Related)+len(chorded))
	related = append(related, violation.Related...)

	for _, edge := range chorded {
		edges = append(edges, string(edge.Edge))
		related = append(related, RelatedLocation{
			Span:    edge.Span,
			Message: "read as the straight line between its ends rather than the arc it states",
		})
	}

	violation.Message = fmt.Sprintf(
		"%s; the figure compared is the chord's, with %s read as the straight line between its ends rather "+
			"than the arc it states",
		violation.Message, join(edges, "and"),
	)
	violation.Related = related

	return violation
}
