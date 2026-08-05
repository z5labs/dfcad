// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// SurfaceMethod is how a ground surface is interpolated from the shots behind
// it.
//
// It is a closed set compiled in, like a geometry form and unlike a type or a
// tolerance ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)):
// each member is an algorithm this package implements, so a name nothing here
// implements is not a surface anybody could be handed.
//
// Which one produced a surface travels with the surface, because two
// interpolations of one set of points are two different answers to the same
// question. A caller handed a grid of levels with no method on it cannot tell an
// answer which honours every shot from one which smooths them away.
type SurfaceMethod string

const (
	// SurfaceTIN is a Delaunay triangulated irregular network with linear
	// interpolation across each facet.
	//
	// It honours every shot exactly: the elevation at a point a shot was taken
	// at is that shot's elevation, and nothing between two shots is higher or
	// lower than both. That is what makes it the method to reach for where the
	// shots are the answer rather than a sample of one.
	SurfaceTIN SurfaceMethod = "tin"

	// SurfaceIDW is inverse distance weighting: every elevation is the weighted
	// mean of the shots around it, weighted by distance in plan raised to a
	// negative power.
	//
	// It smooths, and it is the method to reach for where the shots are a noisy
	// sample of a surface which is genuinely smooth. It honours a shot exactly
	// only at the shot itself.
	SurfaceIDW SurfaceMethod = "idw"
)

// surfaceMethods is every method this package implements, in the order they are
// reported in.
var surfaceMethods = []SurfaceMethod{SurfaceTIN, SurfaceIDW}

// SurfaceMethods returns every interpolation method a surface may be derived by.
func SurfaceMethods() []SurfaceMethod { return slices.Clone(surfaceMethods) }

// Known reports whether this is a method the engine implements.
func (m SurfaceMethod) Known() bool { return slices.Contains(surfaceMethods, m) }

// minimumSurfacePoints is how many distinct points in plan it takes to derive a
// surface at all.
//
// Three, because two points bound no area: a surface derived from them would
// have to guess which way the ground fell away from the line between them, and a
// guess is exactly what a surface derived from measurements must not contain.
const minimumSurfacePoints = 3

// defaultSurfacePower is the exponent [SurfaceIDW] weights by where a caller
// names none. Two is the conventional choice and is recorded on the result like
// any other parameter, so a surface never depends on a default nobody can read.
const defaultSurfacePower = 2.0

// SurfaceParameter is one input a derivation was carried out under, written the
// way it goes into a cache key and onto a result.
//
// Both halves are text. A parameter set which is compared, hashed and shown has
// to have one rendering, and a set of typed fields of which each method uses
// half would need a second rendering for every one of those three jobs.
type SurfaceParameter struct {
	// Name is what the parameter is called.
	Name string `json:"name"`

	// Value is what it was, rendered.
	Value string `json:"value"`
}

// String writes the parameter as name=value.
func (p SurfaceParameter) String() string { return p.Name + "=" + p.Value }

// surfaceParameterText is a whole parameter set as one line, which is what a
// cache key holds and what two derivations are compared on.
func surfaceParameterText(parameters []SurfaceParameter) string {
	parts := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		parts = append(parts, parameter.String())
	}
	return strings.Join(parts, " ")
}

// SessionSystematic is a systematic uncertainty every shot of one occupation
// shares, stated by whoever asks for a surface.
//
// It is the error which does not average away however many shots the afternoon
// produced: a base station set up over the wrong mark, an antenna height typed
// in once, a benchmark whose level is out. Every shot of that session moves the
// same way and by the same amount, so the term is added linearly and counted
// once ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)) rather than
// combined in quadrature with itself.
//
// It is stated on the derivation because the observation format does not carry
// it. A record states the precision the instrument reported for that shot, which
// is the independent part and nothing else; what the whole setup was worth is a
// judgment made afterwards, by whoever reduced the session. Stating it here
// keeps that judgment on the question — it lands on the result and in the cache
// key like every other parameter — rather than in a constant nobody can read
// back off the answer.
//
// A session nothing is stated for contributes no shared term. That is not a
// claim that it has none, and [Elevation.Complete] is where a level says how far
// its budget reaches.
type SessionSystematic struct {
	// Session is the occupation the term belongs to, matched against
	// [SurfacePoint.Session].
	Session ID

	// Magnitude is the one-sigma figure, in the frame's linear unit. Zero or
	// less states nothing and contributes nothing.
	Magnitude float64
}

// String writes the term as the session and what it is worth.
func (s SessionSystematic) String() string {
	return fmt.Sprintf("%s@%s", s.Session, decimal(s.Magnitude))
}

// SurfaceDerivation is a request for a ground surface: which region's shots to
// derive it from, by which method, and under which parameters.
//
// Every field of it ends up on the result and in the cache key. That is the
// point of the type: a surface is not a thing the model holds, it is the answer
// to this question, and an answer separated from its question is a grid of
// numbers nobody can check.
type SurfaceDerivation struct {
	// Against is the geometry derivation the region is read from: the named
	// tolerance, the named position predicate, and the cache both this surface
	// and that geometry are looked up in.
	Against Derivation

	// Method is how the points are interpolated. An empty Method is
	// [SurfaceTIN], which honours the shots exactly and so is the answer a
	// caller who did not choose should get.
	Method SurfaceMethod

	// Power is the exponent [SurfaceIDW] weights distance by. Zero or less is
	// [defaultSurfacePower]. It is read by no other method.
	Power float64

	// Neighbours is how many of the nearest shots [SurfaceIDW] weights. Zero or
	// less is every shot of the region. It is read by no other method.
	Neighbours int

	// Ambiguous is whether shots the boundary rule could not confidently place
	// are used.
	//
	// It is a modelling choice and not a detail. A surface derived without them
	// stops at the shots the model is sure of, which pulls the surveyed area in
	// from the boundary; one derived with them reaches the boundary and rests
	// part way on evidence the model has said it cannot place. Neither is wrong
	// and the answer differs, so which was asked for is recorded.
	Ambiguous bool

	// Roughness is how far the ground departs from the interpolation, one sigma
	// per unit of distance in plan from the nearest shot.
	//
	// It is the only statement anybody can make about the ground *between* the
	// shots, and it has to be stated because nothing in a set of shots implies
	// it: a laid slab and a ploughed field sampled on the same grid give the
	// same arithmetic and are not known equally well. A surface derived without
	// one still answers, and every level it gives reports [Elevation.Complete]
	// false — the figure is then a floor, the uncertainty the answer has from
	// the shots alone.
	//
	// Zero or less states nothing. It is read by every method.
	Roughness float64

	// Systematic is what each occupation's shared error is worth, one entry per
	// session. A session named twice takes the larger of the two figures, for
	// the reason a budget does ([BudgetTerm.Magnitude]); a session nothing is
	// stated for contributes no shared term.
	Systematic []SessionSystematic
}

// normalised fills in the defaults, so that everything below reads the values
// which were actually used rather than the values which were asked for.
func (d SurfaceDerivation) normalised() SurfaceDerivation {
	if d.Method == "" {
		d.Method = SurfaceTIN
	}
	if d.Power <= 0 {
		d.Power = defaultSurfacePower
	}
	if d.Neighbours < 0 {
		d.Neighbours = 0
	}
	if !(d.Roughness > 0) || math.IsInf(d.Roughness, 0) {
		d.Roughness = 0
	}
	d.Systematic = normalisedSystematic(d.Systematic)
	return d
}

// normalisedSystematic is the stated session terms with the ones which state
// nothing dropped, the ones stated twice merged, and the rest in one order.
//
// The order is by session, because the set goes into a cache key: two orderings
// of one set of terms would key one answer under two names, and the second run
// would recompute what the first had already stored.
func normalisedSystematic(stated []SessionSystematic) []SessionSystematic {
	terms := make([]SessionSystematic, 0, len(stated))

	for _, term := range stated {
		if term.Session == "" || !(term.Magnitude > 0) || math.IsInf(term.Magnitude, 0) {
			continue
		}

		if i := slices.IndexFunc(terms, func(held SessionSystematic) bool {
			return held.Session == term.Session
		}); i >= 0 {
			terms[i].Magnitude = math.Max(terms[i].Magnitude, term.Magnitude)
			continue
		}

		terms = append(terms, term)
	}

	slices.SortFunc(terms, func(a, b SessionSystematic) int {
		return strings.Compare(string(a.Session), string(b.Session))
	})

	if len(terms) == 0 {
		return nil
	}
	return terms
}

// Parameters is every input this derivation was carried out under, in a fixed
// order.
//
// The order is fixed because this set is hashed into a cache key: two orderings
// of one set of parameters would key one answer under two names, and the second
// run would recompute what the first had already stored.
//
// A parameter a method does not read is not reported. Recording a power on a
// triangulation would say that changing it changes the answer, and it does not.
func (d SurfaceDerivation) Parameters() []SurfaceParameter {
	d = d.normalised()

	parameters := []SurfaceParameter{
		{Name: "method", Value: string(d.Method)},
		{Name: "tolerance", Value: d.Against.Tolerance},
		{Name: "position", Value: d.Against.Position},
		{Name: "minimum-points", Value: strconv.Itoa(minimumSurfacePoints)},
		{Name: "ambiguous", Value: includedOrNot(d.Ambiguous)},
		{Name: "roughness", Value: statedOrNot(d.Roughness)},
		{Name: "systematic", Value: systematicText(d.Systematic)},
	}

	if d.Method == SurfaceIDW {
		parameters = append(parameters,
			SurfaceParameter{Name: "power", Value: decimal(d.Power)},
			SurfaceParameter{Name: "neighbours", Value: neighbourText(d.Neighbours)},
		)
	}

	return parameters
}

// includedOrNot writes a choice which is about whether something was used.
func includedOrNot(included bool) string {
	if included {
		return "included"
	}
	return "excluded"
}

// statedOrNot writes a magnitude which somebody either stated or did not.
//
// The word rather than a nought, because the two are different answers: nothing
// here is entitled to record "the ground is perfectly smooth" on behalf of a
// caller who said nothing about it.
func statedOrNot(magnitude float64) string {
	if !(magnitude > 0) {
		return "unstated"
	}
	return decimal(magnitude)
}

// systematicText writes the stated session terms as one value of one parameter,
// which is how they reach a cache key.
func systematicText(terms []SessionSystematic) string {
	if len(terms) == 0 {
		return "none"
	}

	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		parts = append(parts, term.String())
	}
	return strings.Join(parts, ",")
}

// neighbourText writes a neighbour count, where none means every point.
func neighbourText(neighbours int) string {
	if neighbours <= 0 {
		return "all"
	}
	return strconv.Itoa(neighbours)
}

// SurfaceKey identifies one derived surface: the tree it came from, the region
// it covers, and every parameter it was derived under.
//
// It is [Key] for a surface and is separate from it for the reason [Key] holds
// the tolerance and the predicate: what is not in the tree has to be in the key,
// or an answer to one question is served for another. The subject is in it
// because a surface is one region's and not the model's.
type SurfaceKey struct {
	// Digest is the digest of the source tree the surface was derived from.
	Digest Digest

	// Subject is the node whose area the shots were taken from.
	Subject ID

	// Method is how they were interpolated.
	Method SurfaceMethod

	// Parameters is every parameter of the derivation, rendered by
	// [SurfaceDerivation.Parameters].
	Parameters string
}

// String writes the key as it reads: the digest, the region, then the
// parameters.
func (k SurfaceKey) String() string {
	return fmt.Sprintf("%s subject=%s %s", k.Digest, k.Subject, k.Parameters)
}

// cacheable reports whether anything may be stored under this key.
func (k SurfaceKey) cacheable() bool { return k.Digest.Known() && k.Subject != "" }

// entry is the file name an entry under this key is written to, beneath the
// directory named by its digest.
//
// The name is prefixed rather than only hashed. A surface and a set of
// footprints are different shapes under one digest, and a prefix is what makes
// a directory listing of a build output readable by somebody working out why a
// run is slow.
func (k SurfaceKey) entry() string {
	sum := sha256.New()
	digestField(sum, []byte(k.Subject))
	digestField(sum, []byte(k.Method))
	digestField(sum, []byte(k.Parameters))
	return "surface-" + hex.EncodeToString(sum.Sum(nil)) + ".json"
}

// interpolationTerm is what the term for the ground between the shots is called
// in a budget. It is not a record's name and cannot collide with one: an id
// carries a namespace and a colon, and this does not.
const interpolationTerm = "interpolation"

// SurfaceTerm is one term of the budget of something read off a surface.
//
// It is [BudgetTerm] for a surface and is separate from it for one reason: what
// contributes here is an observation record and not a claim, and a budget which
// had to name a claim could not attribute a level to the afternoon somebody
// stood there. The two kinds combine identically —
// [0006](docs/decisions/0006-accuracy-is-one-sigma.md) — and [SurfaceBudget]
// applies exactly the arithmetic [Budget.Combined] does.
//
// The magnitude is what the term contributes to *this* answer, weight already
// applied. An independent shot which carries a tenth of the weight of a level
// contributes a tenth of its own uncertainty to it, and the term says so rather
// than making a reader multiply.
type SurfaceTerm struct {
	// Kind is which of the two kinds of error this is, and so how it combines:
	// independent terms in quadrature, systematic terms linearly.
	Kind TermKind

	// Name is what to call the term in a report: the record for a shot's own
	// error, the term id for a shared one, and [interpolationTerm] for the
	// ground between the shots.
	Name string

	// Source is the id a systematic error is shared with — a session, or the
	// control the transform which carried a shot was fitted to. It is empty for
	// an independent term.
	Source ID

	// Magnitude is the one-sigma contribution to the answer, in the surface's
	// unit, with the weight of everything which carried it already applied.
	//
	// It is zero for a term which cancelled: a session error behind both ends of
	// a fall moves both by the same amount and is not in the difference at all.
	// Such a term is reported rather than dropped, because "the base station
	// cancels" is the answer to why a fall is known better than either level.
	Magnitude float64

	// From are the records which carried the term into this answer, sorted.
	From []ID
}

// Shared reports whether more than one record carried this term.
func (t SurfaceTerm) Shared() bool { return len(t.From) > 1 }

// String writes the term as it reads in a report.
//
// The interpolation term names the shot its distance was measured from, because
// an answer with two ends has two of them and neither is the other.
func (t SurfaceTerm) String() string {
	name := t.Name
	if name == interpolationTerm && len(t.From) > 0 {
		name += " near " + string(t.From[0])
	}

	written := fmt.Sprintf("%s %s: %s", t.Kind, name, decimal(t.Magnitude))

	if t.Kind == TermSystematic && t.Shared() {
		written += ", counted once"
	}

	switch {
	case t.Magnitude != 0:
	case t.Kind == TermSystematic:
		written += ", cancels"
	default:
		written += ", carries no weight"
	}

	return written
}

// clone is the term with records of its own, so that a reader handing one out
// cannot let an append reach back into the budget it came from.
func (t SurfaceTerm) clone() SurfaceTerm {
	t.From = slices.Clone(t.From)
	return t
}

// SurfaceBudget is the accumulated uncertainty of one answer read off a surface,
// broken out by contributing term.
//
// It exists for the reason [Budget] does. A level is a weighted sum of shots,
// and combining everything behind it in quadrature is right only where the
// errors are independent: two shots of one afternoon share a base station, and a
// systematic error in it moves both the same way, so it adds linearly and is
// counted once however many shots carried it. Getting that backwards understates
// a level and — much worse — makes a *difference* of two levels look uncertain
// when the shared part of it cancels exactly.
//
// The zero SurfaceBudget holds nothing and every method below works on it.
type SurfaceBudget struct {
	// terms are the accumulated terms: independent ones in the order they were
	// contributed, then the systematic ones, sorted by the id they are shared
	// with.
	terms []SurfaceTerm

	// unit is the linear unit every magnitude here is in, which is the
	// surface's own and so is one unit by construction.
	unit Unit

	// complete is whether the ground between the shots was accounted for: a
	// roughness was stated, so the budget is a budget of the answer rather than
	// of the shots behind it.
	complete bool
}

// Terms returns the accumulated terms.
//
// The terms are a copy, down to the records attributed to each, so nothing a
// caller does to the result reaches back into the budget.
func (b SurfaceBudget) Terms() []SurfaceTerm {
	out := make([]SurfaceTerm, 0, len(b.terms))
	for _, term := range b.terms {
		out = append(out, term.clone())
	}
	return out
}

// Unit returns the linear unit every magnitude in the budget is in.
func (b SurfaceBudget) Unit() Unit { return b.unit }

// Complete reports whether the budget accounts for the ground between the shots.
//
// It is false where the derivation stated no [SurfaceDerivation.Roughness],
// which makes every figure here a floor: the uncertainty the answer has from the
// shots alone, with nothing said about how well the interpolation between them
// models ground nobody measured.
func (b SurfaceBudget) Complete() bool { return b.complete }

// Combined reduces the budget to one standard uncertainty.
//
// The terms combine the way specification section 6.6.5 says they do, which is
// the way [Budget.Combined] combines a budget of claims:
//
//	u = √( Σ uᵢ² + ( Σ |sⱼ| )² )
//
// Every magnitude is already one standard deviation, so the result is too, and
// the [Uncertainty] which comes back says so. Widening is [Uncertainty.Widen],
// which attaches the factor it widened by.
func (b SurfaceBudget) Combined() Uncertainty {
	var squares, shared float64

	for _, term := range b.terms {
		magnitude := math.Abs(term.Magnitude)
		switch term.Kind {
		case TermSystematic:
			shared += magnitude
		default:
			squares += magnitude * magnitude
		}
	}

	return Uncertainty{
		Magnitude:      math.Sqrt(squares + shared*shared),
		Unit:           b.unit,
		CoverageFactor: 1,
	}
}

// Dominant returns the term contributing most to the combined figure, and
// whether the budget holds one to return.
//
// It is what makes a budget actionable. "±20 mm" is a number somebody has to do
// something about, and what to do — take more shots, take better ones, or level
// the session in — is decided by which term is most of it.
func (b SurfaceBudget) Dominant() (SurfaceTerm, bool) {
	if len(b.terms) == 0 {
		return SurfaceTerm{}, false
	}

	dominant := b.terms[0]
	for _, term := range b.terms[1:] {
		if math.Abs(term.Magnitude) > math.Abs(dominant.Magnitude) {
			dominant = term
		}
	}
	return dominant.clone(), true
}

// String writes the combined figure with its unit and coverage factor.
func (b SurfaceBudget) String() string {
	written := b.Combined().String()
	if !b.complete {
		written += ", the shots only"
	}
	return written
}

// surfaceBudgetBuilder accumulates the terms of one answer.
//
// It is where the two rules of
// [0006](docs/decisions/0006-accuracy-is-one-sigma.md) are applied, and it is
// one type rather than two pieces of arithmetic because a level and a difference
// of two levels are the same accumulation under different coefficients: a level
// takes each shot at the weight the interpolation gave it, and a fall takes each
// shot at the difference of its two weights, which is what makes the shared part
// of a difference cancel.
type surfaceBudgetBuilder struct {
	// independent are the per-record terms, keyed by the record, in the order
	// they were first reached.
	independent []surfaceCoefficient

	// systematic are the shared terms, keyed by the id they are shared with.
	systematic []surfaceCoefficient

	// extra are terms which belong to no record and are not shared with
	// anything: the interpolation term of each end of the answer.
	extra []SurfaceTerm

	// unit and complete travel onto the budget unchanged.
	unit     Unit
	complete bool
}

// surfaceCoefficient is one accumulating term: what it is worth, and the signed
// coefficient everything which carried it has contributed so far.
//
// The coefficient is signed and accumulated before its absolute value is taken.
// That is the whole of the correlation arithmetic: a term reached through two
// inputs with opposite signs is a term which cancels, and one which took the
// absolute value first would report a difference as though the two ends were
// measured by different afternoons.
type surfaceCoefficient struct {
	key         ID
	name        string
	magnitude   float64
	coefficient float64
	from        []ID
}

// shot accumulates one record's own error at the given coefficient.
func (b *surfaceBudgetBuilder) shot(point SurfacePoint, coefficient float64) {
	b.add(&b.independent, point.observation, string(point.observation), point.sigma, coefficient, point.observation)

	for _, term := range point.shared {
		b.add(&b.systematic, term.Source, term.Name, term.Magnitude, coefficient, point.observation)
	}
}

// interpolation accumulates the term for the ground between the shots at one
// point of the surface, which belongs to that point and to nothing else.
func (b *surfaceBudgetBuilder) interpolation(roughness, distance float64, nearest ID) {
	if !(roughness > 0) || !(distance > 0) {
		return
	}

	b.extra = append(b.extra, SurfaceTerm{
		Kind:      TermIndependent,
		Name:      interpolationTerm,
		Magnitude: roughness * distance,
		From:      []ID{nearest},
	})
}

// add merges one contribution into the set it belongs to.
func (b *surfaceBudgetBuilder) add(into *[]surfaceCoefficient, key ID, name string, magnitude, coefficient float64, from ID) {
	if !(magnitude > 0) {
		return
	}

	for i := range *into {
		if (*into)[i].key != key {
			continue
		}

		// The wider of two figures for one shared error, exactly as
		// [Budget.contribute] takes it: a budget which cannot tell which fit of
		// a shared error is the right one reports the wider, because a check
		// which fails on a wide budget prompts an investigation and one which
		// passes on a narrow one does not.
		(*into)[i].magnitude = math.Max((*into)[i].magnitude, magnitude)
		(*into)[i].coefficient += coefficient
		if !slices.Contains((*into)[i].from, from) {
			(*into)[i].from = append((*into)[i].from, from)
		}
		return
	}

	*into = append(*into, surfaceCoefficient{
		key:         key,
		name:        name,
		magnitude:   magnitude,
		coefficient: coefficient,
		from:        []ID{from},
	})
}

// budget is everything accumulated, as the budget of one answer.
func (b *surfaceBudgetBuilder) budget() SurfaceBudget {
	out := SurfaceBudget{unit: b.unit, complete: b.complete}

	for _, held := range b.independent {
		out.terms = append(out.terms, held.term(TermIndependent))
	}
	out.terms = append(out.terms, b.extra...)

	shared := slices.Clone(b.systematic)
	slices.SortStableFunc(shared, func(a, b surfaceCoefficient) int {
		return strings.Compare(string(a.key), string(b.key))
	})
	for _, held := range shared {
		out.terms = append(out.terms, held.term(TermSystematic))
	}

	return out
}

// term is one accumulated contribution as a term of the finished budget.
func (c surfaceCoefficient) term(kind TermKind) SurfaceTerm {
	term := SurfaceTerm{
		Kind:      kind,
		Name:      c.name,
		Magnitude: math.Abs(c.coefficient) * c.magnitude,
		From:      slices.Clone(c.from),
	}
	if kind == TermSystematic {
		term.Source = c.key
	}
	slices.Sort(term.From)

	return term
}

// SurfacePoint is one shot the surface rests on, in the surface's frame.
//
// It is a shot and not a coordinate: which record it came from is on it, so a
// level read off the surface can be traced back to the afternoon somebody stood
// there. That is what [Surface.Observations] is assembled from.
//
// The zero SurfacePoint holds no shot and every method below works on it.
type SurfacePoint struct {
	// observation is the record this point was read from.
	observation ID

	// session is the occupation that record belongs to, which is how a
	// systematic error in the surface is attributed to the afternoon which
	// caused it.
	session ID

	// at is the coordinate in the surface's frame: the first two components in
	// plan, the third the elevation.
	at Point

	// unit is the linear unit at and every uncertainty here are in, repeated
	// from the surface so that a shot's budget carried around on its own still
	// says what its figures mean.
	unit Unit

	// sigma is the *independent* standard uncertainty of that elevation, one
	// sigma ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)): the record's
	// own vertical precision, in quadrature with the independent part of the
	// transform's where the shot was carried in from another frame. It is the
	// part of the shot's error which correlates with no other shot.
	sigma float64

	// shared are the systematic terms this shot carries: the occupation's, where
	// the derivation stated one, and the control behind the transform which
	// carried it, where it was carried. They are held apart from sigma because
	// they do not combine with it the same way — a term two shots share is
	// counted once, and a term two shots share which is behind both ends of a
	// difference is not in the difference at all.
	shared []SurfaceTerm

	// carried is whether the shot was written in another frame and brought
	// across.
	carried bool

	// ambiguous is whether the boundary rule could not confidently place it, so
	// that a surface which reaches the boundary can say which of its points it
	// only nearly rests on.
	ambiguous bool

	// coincident is every other record this point stands for: shots of the same
	// mark, which are one point in plan and cannot each be a vertex.
	coincident []ID
}

// Observation returns the identity of the record this point was read from.
func (p SurfacePoint) Observation() ID { return p.observation }

// Session returns the occupation that record belongs to.
func (p SurfacePoint) Session() ID { return p.session }

// At returns the coordinate, in the surface's frame and unit.
func (p SurfacePoint) At() Point { return p.at }

// Unit returns the linear unit the coordinate and every uncertainty on the shot
// are in.
func (p SurfacePoint) Unit() Unit { return p.unit }

// Elevation returns the third component of the coordinate, which is what a
// surface is a surface of.
func (p SurfacePoint) Elevation() float64 { return p.at[2] }

// Uncertainty returns the standard uncertainty of that elevation, one sigma:
// the independent part and every systematic term the shot carries, combined the
// way a budget combines them.
func (p SurfacePoint) Uncertainty() float64 { return p.Budget().Combined().Magnitude }

// Independent returns the part of that uncertainty which correlates with no
// other shot — the instrument's own precision, and the random part of any
// transform the shot was carried across.
//
// It is separate from [SurfacePoint.Uncertainty] because it is the only part
// which averages away. Twenty shots of one afternoon reduce this by a factor of
// four or so between them and reduce the systematic terms by nothing at all.
func (p SurfacePoint) Independent() float64 { return p.sigma }

// Systematic returns the shared terms the shot carries, sorted by the id each is
// shared with.
func (p SurfacePoint) Systematic() []SurfaceTerm {
	out := make([]SurfaceTerm, 0, len(p.shared))
	for _, term := range p.shared {
		out = append(out, term.clone())
	}
	return out
}

// Budget returns the shot's own uncertainty broken out by term.
//
// It is the budget of the shot rather than of anything read off the surface: the
// weights an interpolation applies are not in it, and neither is the ground
// between this shot and the next one.
func (p SurfacePoint) Budget() SurfaceBudget {
	budget := &surfaceBudgetBuilder{unit: p.unit, complete: true}
	budget.shot(p, 1)
	return budget.budget()
}

// Carried reports whether the shot was written in another frame and transformed
// before it was used.
func (p SurfacePoint) Carried() bool { return p.carried }

// Ambiguous reports whether the boundary rule could not confidently place the
// shot in the region. A surface holds such a point only where the derivation
// asked for one.
func (p SurfacePoint) Ambiguous() bool { return p.ambiguous }

// Coincident returns every other record this point stands for: shots which fall
// within the derivation's tolerance of it in plan and so are the same point.
//
// They are recorded rather than dropped. Two shots of one mark half an hour
// apart are evidence about repeatability, and a surface which mentioned only the
// one it happened to keep would lose the other from its provenance entirely.
func (p SurfacePoint) Coincident() []ID { return slices.Clone(p.coincident) }

// String writes the point as its record and where it is.
func (p SurfacePoint) String() string {
	if p.observation == "" {
		return "no point"
	}
	return fmt.Sprintf("%s at %s", p.observation, pointText(p.at, 3))
}

// SurfaceFacet is one triangle of a [SurfaceTIN] surface, as three indices into
// [Surface.Points].
//
// It is indices rather than points so that a facet and the shots it was built
// from cannot come apart: an elevation read off a facet names the three records
// it was interpolated from, and it can only do that if the facet still knows
// which points those were.
type SurfaceFacet [3]int

// Vertices returns the three indices into [Surface.Points], counter-clockwise in
// plan.
func (f SurfaceFacet) Vertices() (int, int, int) { return f[0], f[1], f[2] }

// Surface is a ground surface derived from the observations inside one region.
//
// It is a build output and never a source. Nothing here is written into an
// entity file, an observation file, or anything else a walk would read
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)); the
// only place it is written at all is the derived cache under [BuildDir], which
// the repository ignores and which can be deleted without changing a single
// answer. A surface stored as source would be a drawing of the ground which
// stopped being true the first time somebody took another shot.
//
// # The method travels with it
//
// [Surface.Method] and [Surface.Parameters] are on the result rather than being
// remembered by whoever asked, because two interpolations of one set of points
// are two different surfaces. A cut-and-fill worked out against a triangulation
// and one worked out against a smoothed surface disagree, and the disagreement
// is invisible in the numbers.
//
// # Plan and elevation
//
// The surface is a function of the first two components of its frame, and the
// elevation it answers with is the third
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md) — one unit for all
// three). The region supplies which shots are in it and nothing else: a region
// is a figure in plan, and a terrace whose loop is drawn on a slope is still
// surveyed in the frame's own vertical.
//
// # Its edge is the shots, not the region
//
// A surface reaches as far as there is evidence: the convex hull of the points
// it rests on. A point beyond that hull is reported as outside rather than
// answered for ([Surface.Elevation]), because there is no measurement out there
// and continuing the last facet's slope into the neighbour's garden is a made-up
// number which reads exactly like a surveyed one.
//
// The zero Surface holds nothing, is [Surface.Derived] false, and every method
// below works on it.
type Surface struct {
	// subject is the region the shots were taken from.
	subject ID

	// digest is the source tree the surface was derived from, which is what
	// lets a consumer check that a surface it was handed matches the model in
	// front of it.
	digest Digest

	// frame is the frame every coordinate here is in, and unit its linear unit.
	frame ID
	unit  Unit

	// method is how the points were interpolated, and parameters is every input
	// that interpolation was carried out under.
	method     SurfaceMethod
	parameters []SurfaceParameter

	// points are the shots the surface rests on, ordered by record identity.
	points []SurfacePoint

	// facets are the triangles of a [SurfaceTIN] surface, and are empty for
	// every other method.
	facets []SurfaceFacet

	// hull is the convex hull of the points in plan, counter-clockwise, as
	// indices into points. It is the whole of where this surface has anything
	// to say.
	hull []int

	// slack is how far outside a figure a point may be and still be on it: the
	// derivation's tolerance, which is the distance the project has said it does
	// not distinguish from zero.
	slack float64

	// roughness is how far the ground was said to depart from the interpolation
	// per unit of distance from the nearest shot, and is zero where nobody said.
	// It is kept because it is read at every question rather than at derivation:
	// how far from the evidence a level was asked for is a property of the
	// question.
	roughness float64
}

// Subject returns the region the surface covers.
func (s Surface) Subject() ID { return s.subject }

// Digest returns the digest of the source tree the surface was derived from.
func (s Surface) Digest() Digest { return s.digest }

// Frame returns the frame every coordinate on the surface is in.
func (s Surface) Frame() ID { return s.frame }

// Unit returns the linear unit of that frame, which every length here is in.
func (s Surface) Unit() Unit { return s.unit }

// Method returns how the points were interpolated.
func (s Surface) Method() SurfaceMethod { return s.method }

// Parameters returns every input the derivation was carried out under,
// including the method and the defaults which were filled in.
func (s Surface) Parameters() []SurfaceParameter { return slices.Clone(s.parameters) }

// Roughness returns how far the ground was stated to depart from the
// interpolation, one sigma per unit of distance in plan from the nearest shot.
// It is zero where the derivation stated nothing, which is what makes every
// level read off the surface report [Elevation.Complete] false.
func (s Surface) Roughness() float64 { return s.roughness }

// Points returns the shots the surface rests on, ordered by record identity.
func (s Surface) Points() []SurfacePoint { return slices.Clone(s.points) }

// Len returns how many points the surface rests on.
func (s Surface) Len() int { return len(s.points) }

// Facets returns the triangles of a [SurfaceTIN] surface, as indices into
// [Surface.Points]. Every other method has none.
func (s Surface) Facets() []SurfaceFacet { return slices.Clone(s.facets) }

// Hull returns the convex hull of the points in plan, counter-clockwise.
//
// It is the edge of the surface: [Surface.Elevation] answers inside it and
// reports outside beyond it.
func (s Surface) Hull() []Point {
	hull := make([]Point, 0, len(s.hull))
	for _, index := range s.hull {
		hull = append(hull, s.points[index].at)
	}
	return hull
}

// Observations returns every record the surface was derived from, sorted.
//
// This is the traceability the surface exists to keep: a level read off it came
// from these shots and no others, so a base station later found to have been on
// the wrong mark can be followed forwards into every surface which used it.
//
// It includes records which are not vertices — shots of a mark another shot
// already stands for — because they were read, judged and used to decide which
// point that mark is.
func (s Surface) Observations() []ID {
	ids := make([]ID, 0, len(s.points))
	for _, point := range s.points {
		ids = append(ids, point.observation)
		ids = append(ids, point.coincident...)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// Derived reports whether a surface was arrived at at all.
//
// A region with too few shots in it, or with shots which all fall on one line,
// yields a diagnostic and no surface; this is how a caller which kept only the
// result tells that from a surface which happens to be flat.
func (s Surface) Derived() bool { return len(s.points) >= minimumSurfacePoints && len(s.hull) >= 3 }

// String writes what the surface is: the region, the method, and what it rests
// on.
func (s Surface) String() string {
	if !s.Derived() {
		return fmt.Sprintf("%s: no surface", s.subject)
	}

	parts := []string{
		fmt.Sprintf("%s from %s", s.method, plural(len(s.points), "point")),
	}
	if len(s.facets) > 0 {
		parts = append(parts, plural(len(s.facets), "facet"))
	}
	parts = append(parts, fmt.Sprintf("hull of %s", plural(len(s.hull), "point")))

	return fmt.Sprintf("%s: %s", s.subject, strings.Join(parts, ", "))
}

// Elevation is the ground level the surface reports at one point, and where that
// level came from.
//
// The zero Elevation is what a point outside the surface gets, and every method
// below works on it.
type Elevation struct {
	// at is the point in plan the question was asked at, in the surface's
	// frame, with the answer as its third component.
	at Point

	// budget is where that answer's uncertainty came from, term by term. The
	// combined figure is derived from it rather than held beside it, so the two
	// cannot come apart.
	budget SurfaceBudget

	// nearest is how far in plan the question was asked from the nearest shot
	// the surface rests on, which is what the interpolation term of the budget
	// is computed from.
	nearest float64

	// method is how it was interpolated, repeated from the surface so that an
	// elevation passed on its own still says how it was arrived at.
	method SurfaceMethod

	// from are the records it was interpolated from, and weights how much each
	// of them counted, in the same order.
	from    []ID
	weights []float64
}

// At returns the point the question was asked at, with the answer as its third
// component.
func (e Elevation) At() Point { return e.at }

// Value returns the ground level.
func (e Elevation) Value() float64 { return e.at[2] }

// Uncertainty returns the standard uncertainty of that level, one sigma, in the
// surface's unit.
//
// It is propagated from the shots behind the answer under the weights the
// interpolation gave them, with each shot's independent error in quadrature and
// every systematic term the shots share added linearly and counted once
// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)). Where the derivation
// stated a roughness it also carries the ground between the shots, which grows
// with the distance from them; where it did not, [Elevation.Complete] is false
// and this figure is a floor.
func (e Elevation) Uncertainty() float64 { return e.budget.Combined().Magnitude }

// Budget returns that uncertainty broken out by contributing term.
//
// It is the actionable half, and it is what a caller needs to combine a level
// with anything else: two levels off one surface share the terms their shots
// share, and adding two combined figures in quadrature would count those twice.
// [Surface.Fall] is that arithmetic done properly for the difference of two
// levels.
func (e Elevation) Budget() SurfaceBudget { return e.budget }

// Nearest returns how far in plan the level was asked for from the nearest shot
// the surface rests on, in the surface's unit.
//
// It is the distance the interpolation term of the budget is computed from, and
// on its own it is the honest answer to "how far from the evidence is this?".
func (e Elevation) Nearest() float64 { return e.nearest }

// Complete reports whether the uncertainty accounts for the ground between the
// shots.
//
// It is false where the derivation stated no [SurfaceDerivation.Roughness]. The
// level is still answered and its budget still holds every term the shots put
// there — what is missing is the one term no arithmetic over the shots can
// supply, and a caller deciding anything against a figure this is false for is
// deciding against a floor.
func (e Elevation) Complete() bool { return e.budget.Complete() }

// Method returns how the level was interpolated.
func (e Elevation) Method() SurfaceMethod { return e.method }

// From returns the records the level was interpolated from, sorted.
func (e Elevation) From() []ID { return slices.Clone(e.from) }

// Weights returns how much each of those records counted, in the same order as
// [Elevation.From]. They sum to one.
func (e Elevation) Weights() []float64 { return slices.Clone(e.weights) }

// String writes the level, its uncertainty and what it came from.
func (e Elevation) String() string {
	if len(e.from) == 0 {
		return "no elevation"
	}

	names := make([]string, 0, len(e.from))
	for _, id := range e.from {
		names = append(names, string(id))
	}

	return fmt.Sprintf("%s ± %s by %s from %s",
		decimal(e.Value()), decimal(e.Uncertainty()), e.method, strings.Join(names, ", "))
}

// Report is the level rendered for a person: the summary, and one line per term
// of its budget beneath it.
//
// It is here rather than in the command for the reason [Fit.Report] is: a
// library caller reporting a level and the command reporting one write the same
// thing.
func (e Elevation) Report() string {
	if len(e.from) == 0 {
		return "no elevation"
	}

	var out strings.Builder

	out.WriteString(e.String())
	fmt.Fprintf(&out, "\n  known to %s", e.budget)
	fmt.Fprintf(&out, "\n  %s from the nearest shot", decimal(e.nearest))

	for _, term := range e.budget.Terms() {
		fmt.Fprintf(&out, "\n  %s", term)
	}

	if !e.Complete() {
		out.WriteString("\n  no roughness stated: the figure is the shots only")
	}

	return out.String()
}

// Covers reports whether the surface has anything to say at a point.
//
// It is the hull test [Surface.Elevation] applies, asked on its own, for a
// caller sampling a grid which wants to know which cells are surveyed before it
// asks for a level in any of them.
func (s Surface) Covers(at Point) bool {
	if !s.Derived() {
		return false
	}
	return s.holds(vec{at[0], at[1]})
}

// Elevation returns the ground level at a point, and whether the surface has
// anything to say there.
//
// The second return is the whole of the rule at the edge. A point beyond the
// convex hull of the shots is **outside**, and outside is reported rather than
// answered: the last facet's slope continued past the last shot is a number with
// no measurement under it, and handed back beside real ones it is
// indistinguishable from one. A caller which wants ground level beyond the
// survey has to go and take a shot there.
//
// Only the first two components of at are read. The third is the answer.
func (s Surface) Elevation(at Point) (Elevation, bool) {
	if !s.Derived() {
		return Elevation{}, false
	}

	plan := vec{at[0], at[1]}
	if !s.holds(plan) {
		return Elevation{}, false
	}

	indices, weights, ok := s.contributions(plan)
	if !ok {
		return Elevation{}, false
	}

	return s.answer(at, indices, weights), true
}

// contributions is which points answer for a place and how much each of them
// counts, which is the one thing the two methods disagree about.
func (s Surface) contributions(plan vec) ([]int, []float64, bool) {
	var (
		indices []int
		weights []float64
	)

	switch s.method {
	case SurfaceIDW:
		indices, weights = s.weighByDistance(plan)
	default:
		indices, weights = s.weighByFacet(plan)
	}

	if len(indices) == 0 {
		return nil, nil, false
	}

	// Ordered by record identity. Which order a method happened to find its
	// points in is an implementation detail, and a caller diffing two runs
	// should not see one.
	order := make([]int, len(indices))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return strings.Compare(
			string(s.points[indices[a]].observation),
			string(s.points[indices[b]].observation),
		)
	})

	sorted := make([]int, 0, len(order))
	counted := make([]float64, 0, len(order))
	for _, i := range order {
		sorted = append(sorted, indices[i])
		counted = append(counted, weights[i])
	}

	return sorted, counted, true
}

// answer assembles an elevation from the points which contributed and how much
// each contributed, which is the same arithmetic whichever method chose them.
func (s Surface) answer(at Point, indices []int, weights []float64) Elevation {
	elevation := Elevation{
		at:      at,
		method:  s.method,
		nearest: s.nearestShot(vec{at[0], at[1]}),
	}

	budget := &surfaceBudgetBuilder{unit: s.unit, complete: s.roughness > 0}

	var value float64
	for i, index := range indices {
		point, weight := s.points[index], weights[i]

		value += weight * point.at[2]

		// Each shot enters the budget at the weight the interpolation gave it,
		// its own error as an independent term and everything it shares with
		// another shot as a systematic one. Two shots of one afternoon meet each
		// other on that session's term, which is added once at the sum of their
		// weights rather than twice in quadrature — a base station on the wrong
		// mark moves the whole surface and does not average away.
		budget.shot(point, weight)

		elevation.from = append(elevation.from, point.observation)
		elevation.weights = append(elevation.weights, weight)
	}

	budget.interpolation(s.roughness, elevation.nearest, s.nearestRecord(vec{at[0], at[1]}))

	elevation.at[2] = value
	elevation.budget = budget.budget()

	return elevation
}

// nearestShot is how far a place in plan is from the nearest shot the surface
// rests on, which is the distance the interpolation term grows with.
//
// It is the nearest shot of the whole surface and not the nearest of the ones
// which carried weight. "How far from the evidence is this?" is a question about
// the survey rather than about which triangle the arithmetic landed in.
func (s Surface) nearestShot(plan vec) float64 {
	nearest := math.Inf(1)
	for i := range s.points {
		at := s.plan(i)
		nearest = math.Min(nearest, math.Hypot(at.X-plan.X, at.Y-plan.Y))
	}

	if math.IsInf(nearest, 0) {
		return 0
	}
	return nearest
}

// nearestRecord is which shot that distance was measured to, taken by record
// identity where two are equally near so that the answer does not depend on the
// order the corpus was walked in.
func (s Surface) nearestRecord(plan vec) ID {
	var (
		nearest  = math.Inf(1)
		observed ID
	)

	for i, point := range s.points {
		at := s.plan(i)
		distance := math.Hypot(at.X-plan.X, at.Y-plan.Y)

		if distance < nearest || (distance == nearest && point.observation < observed) {
			nearest, observed = distance, point.observation
		}
	}

	return observed
}

// Fall is how much the ground drops between two points of one surface, and how
// well that drop is known.
//
// It is a first-class answer rather than a subtraction a caller does, because
// the subtraction is where the correlation lives. Two levels off one surface are
// not two independent measurements: they are weighted sums of shots which share
// afternoons, base stations and the georeference which carried them, and the
// shared part of that error is in both levels *by the same amount*. It therefore
// cancels out of the difference — exactly, where both ends rest on the same
// session — and a caller who combined [Elevation.Uncertainty] twice in
// quadrature would report a fall as several times less certain than it is, then
// go and re-survey ground which was never the problem.
//
// The zero Fall is what a pair of points outside the surface gets, and every
// method below works on it.
type Fall struct {
	// from is the level at the high end of the question as asked, and to at the
	// other. Which is actually higher is the answer rather than the question.
	from Elevation
	to   Elevation

	// run is how far apart the two are in plan, which is what a gradient is per.
	run float64

	// budget is the uncertainty of the difference, term by term, with every
	// shared term entered once at the difference of the weights the two ends
	// gave it.
	budget SurfaceBudget
}

// From returns the level at the point the fall was measured from.
func (f Fall) From() Elevation { return f.from }

// To returns the level at the point it was measured to.
func (f Fall) To() Elevation { return f.to }

// Value returns the drop from the first point to the second, in the surface's
// unit.
//
// It is positive where the ground falls away from the first point, which is what
// a drainage question asks, and negative where it rises towards the second.
func (f Fall) Value() float64 { return f.from.Value() - f.to.Value() }

// Run returns how far apart the two points are in plan.
func (f Fall) Run() float64 { return f.run }

// Gradient returns the drop per unit of run, positive where the ground falls
// away. It is zero over a run of nothing, there being no gradient to state.
func (f Fall) Gradient() float64 {
	if !(f.run > 0) {
		return 0
	}
	return f.Value() / f.run
}

// Budget returns the uncertainty of the drop, broken out by contributing term.
//
// A term behind both ends appears once, at the difference of the weights the two
// ends gave it, so a session behind the whole surface comes back at nought and
// says so. That is the arithmetic which makes a fall answerable at all from
// shots no one level is worth deciding against.
func (f Fall) Budget() SurfaceBudget { return f.budget }

// Uncertainty returns the standard uncertainty of the drop, one sigma, in the
// surface's unit.
func (f Fall) Uncertainty() float64 { return f.budget.Combined().Magnitude }

// Complete reports whether that uncertainty accounts for the ground between the
// shots, which it does only where the derivation stated a roughness.
func (f Fall) Complete() bool { return f.budget.Complete() }

// Decides reports whether the fall is known well enough for a decision which
// needs it to a standard uncertainty of required, in the surface's unit.
//
// It is deliberately a comparison against a figure the caller states rather than
// a verdict this package reaches: what a fall has to be known to is a property of
// what is being built on it — a drain, a step, a threshold — and nothing here
// knows that ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
//
// A requirement which is not a figure greater than zero decides nothing, and a
// fall whose budget does not account for the ground between the shots decides
// nothing either: a floor compared against a requirement passes by leaving
// something out, which is the one way this could report a decision somebody
// should not make.
func (f Fall) Decides(required float64) bool {
	if !(required > 0) || !f.Complete() {
		return false
	}
	return f.Uncertainty() <= required
}

// String renders the fall as a person reads it.
func (f Fall) String() string {
	if len(f.from.from) == 0 || len(f.to.from) == 0 {
		return "no fall"
	}

	return fmt.Sprintf("falls %s over %s (%s) ± %s",
		decimal(f.Value()), decimal(f.run), gradeText(f.Gradient()), decimal(f.Uncertainty()))
}

// Report is the fall rendered for a person: the summary, and one line per term
// of the budget beneath it.
//
// The terms are the actionable half, and in a fall they are the surprising half
// too: the line which reads "cancels" is the whole reason a difference of two
// levels is worth asking for rather than working out by hand.
func (f Fall) Report() string {
	if len(f.from.from) == 0 || len(f.to.from) == 0 {
		return "no fall"
	}

	var out strings.Builder

	out.WriteString(f.String())
	fmt.Fprintf(&out, "\n  from %s", f.from)
	fmt.Fprintf(&out, "\n  to %s", f.to)
	fmt.Fprintf(&out, "\n  known to %s", f.budget)

	for _, term := range f.budget.Terms() {
		fmt.Fprintf(&out, "\n  %s", term)
	}

	if !f.Complete() {
		out.WriteString("\n  no roughness stated: the figure is the shots only")
	}

	return out.String()
}

// gradeText writes a gradient the way a fall is specified on a drawing.
//
// A gradient of nought is level and has no denominator, and one steeper than a
// slope anybody builds is written as itself rather than as a ratio nobody reads.
//
// The denominator is written to a tenth and is the one figure here which is
// rounded. A grade is a specification — one in eighty, one in forty — and
// "1 in 79.99999999998181" is that specification rendered as though the
// arithmetic behind it were the point. The value it came from and the
// uncertainty beside it are written in full, which is where the arithmetic
// belongs.
func gradeText(gradient float64) string {
	if gradient == 0 {
		return "level"
	}

	magnitude := math.Abs(gradient)
	if magnitude >= 1 {
		return fmt.Sprintf("%s per unit", decimal(gradient))
	}

	written := fmt.Sprintf("1 in %.1f", 1/magnitude)
	if gradient < 0 {
		return "rises, " + written
	}
	return written
}

// Fall returns how much the ground drops between two points, and whether the
// surface has anything to say at both of them.
//
// Both ends are answered by [Surface.Elevation] and are subject to the same rule
// at the edge: a point beyond the hull of the shots is outside, and a fall with
// one end outside is not answered at all rather than answered from an
// extrapolation nobody measured.
//
// The uncertainty which comes back is **not** the two levels combined in
// quadrature. Every term the two ends share is entered once, at the difference
// of the weights they gave it, which is what makes a systematic error behind the
// whole survey cancel out of the difference instead of being counted twice into
// it. The independent error of a shot which backs both ends partially cancels
// the same way.
//
// Only the first two components of each point are read.
func (s Surface) Fall(from, to Point) (Fall, bool) {
	if !s.Derived() {
		return Fall{}, false
	}

	near, far := vec{from[0], from[1]}, vec{to[0], to[1]}
	if !s.holds(near) || !s.holds(far) {
		return Fall{}, false
	}

	fromIndices, fromWeights, ok := s.contributions(near)
	if !ok {
		return Fall{}, false
	}

	toIndices, toWeights, ok := s.contributions(far)
	if !ok {
		return Fall{}, false
	}

	fall := Fall{
		from: s.answer(from, fromIndices, fromWeights),
		to:   s.answer(to, toIndices, toWeights),
		run:  math.Hypot(far.X-near.X, far.Y-near.Y),
	}

	// The difference is z(from) − z(to), so a shot behind the first end enters
	// at the weight it was given there and one behind the second at minus its
	// weight. A shot behind both meets itself and enters once at the difference.
	budget := &surfaceBudgetBuilder{unit: s.unit, complete: s.roughness > 0}
	for i, index := range fromIndices {
		budget.shot(s.points[index], fromWeights[i])
	}
	for i, index := range toIndices {
		budget.shot(s.points[index], -toWeights[i])
	}

	// The ground between the shots is not shared: how far the ground at one end
	// departs from the interpolation says nothing about how far it departs four
	// metres away, so the two ends contribute two independent terms rather than
	// one which cancels.
	budget.interpolation(s.roughness, fall.from.nearest, s.nearestRecord(near))
	budget.interpolation(s.roughness, fall.to.nearest, s.nearestRecord(far))

	fall.budget = budget.budget()

	return fall, true
}

// weighByFacet is the [SurfaceTIN] rule: the three corners of the facet the
// point falls in, weighted by barycentric coordinate.
//
// A point inside the hull which no facet claims is a sliver at the edge of the
// arithmetic rather than a hole in the surface — a Delaunay triangulation covers
// its hull exactly. It is answered from the nearest facet, which is the same
// answer to within the rounding which lost it.
func (s Surface) weighByFacet(plan vec) ([]int, []float64) {
	var (
		nearest  SurfaceFacet
		distance = math.Inf(1)
		found    bool
	)

	for _, facet := range s.facets {
		a, b, c := s.plan(facet[0]), s.plan(facet[1]), s.plan(facet[2])

		outside := math.Max(0, -minEdgeDistance(plan, a, b, c))
		if outside <= s.slack {
			return facet[:], barycentric(plan, a, b, c)
		}
		if outside < distance {
			nearest, distance, found = facet, outside, true
		}
	}

	if !found {
		return nil, nil
	}

	return nearest[:], barycentric(plan, s.plan(nearest[0]), s.plan(nearest[1]), s.plan(nearest[2]))
}

// weighByDistance is the [SurfaceIDW] rule: the nearest points, weighted by one
// over the distance raised to the power.
//
// A point on top of a shot is that shot's elevation and no arithmetic. The
// weight there is a division by zero, and the limit of the expression as the
// distance closes is the shot itself, so the shot is what is returned — which is
// also what stops a level asked for at a mark disagreeing with the mark.
func (s Surface) weighByDistance(plan vec) ([]int, []float64) {
	type reach struct {
		index    int
		distance float64
	}

	reaches := make([]reach, 0, len(s.points))
	for i, point := range s.points {
		distance := math.Hypot(point.at[0]-plan.X, point.at[1]-plan.Y)
		if distance <= s.slack {
			return []int{i}, []float64{1}
		}
		reaches = append(reaches, reach{index: i, distance: distance})
	}

	// Sorted by distance and then by record identity, so that two shots equally
	// far away are taken in an order which does not depend on the order the
	// corpus was walked in.
	slices.SortFunc(reaches, func(a, b reach) int {
		if order := cmpFloat(a.distance, b.distance); order != 0 {
			return order
		}
		return strings.Compare(
			string(s.points[a.index].observation),
			string(s.points[b.index].observation),
		)
	})

	if s.neighbours() > 0 && s.neighbours() < len(reaches) {
		reaches = reaches[:s.neighbours()]
	}

	indices := make([]int, 0, len(reaches))
	weights := make([]float64, 0, len(reaches))

	var total float64
	for _, taken := range reaches {
		weight := math.Pow(taken.distance, -s.power())
		indices = append(indices, taken.index)
		weights = append(weights, weight)
		total += weight
	}

	if total <= 0 || math.IsInf(total, 0) || math.IsNaN(total) {
		return nil, nil
	}

	for i := range weights {
		weights[i] /= total
	}

	return indices, weights
}

// power is the exponent this surface was weighted by, read back off the
// parameters it recorded rather than kept twice.
func (s Surface) power() float64 {
	for _, parameter := range s.parameters {
		if parameter.Name != "power" {
			continue
		}
		if power, err := strconv.ParseFloat(parameter.Value, 64); err == nil && power > 0 {
			return power
		}
	}
	return defaultSurfacePower
}

// neighbours is how many points this surface was weighted over, zero meaning all
// of them.
func (s Surface) neighbours() int {
	for _, parameter := range s.parameters {
		if parameter.Name != "neighbours" {
			continue
		}
		if count, err := strconv.Atoi(parameter.Value); err == nil && count > 0 {
			return count
		}
	}
	return 0
}

// plan is one point's position in plan.
func (s Surface) plan(index int) vec {
	return vec{s.points[index].at[0], s.points[index].at[1]}
}

// holds reports whether a point in plan is on the surface: inside the convex
// hull, or within the derivation's tolerance of it.
func (s Surface) holds(plan vec) bool {
	for i, index := range s.hull {
		from, to := s.plan(index), s.plan(s.hull[(i+1)%len(s.hull)])
		if edgeDistance(plan, from, to) < -s.slack {
			return false
		}
	}
	return true
}

// SurfaceWithin derives the ground surface of the observations inside one
// region.
//
// **It writes nothing into the model.** The surface is a build output: it is
// computed from the shots and the boundary every time it is asked, it is kept
// only in the derived cache under [BuildDir], and deleting that cache changes
// what this returns by nothing at all
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// Which shots it rests on is [Graph.ObservationsWithin] and the same boundary
// rule: every current record of the model whose coordinate falls inside the
// region, carried into the region's frame where it was written in another one.
// Shots the boundary rule could not confidently place are used only where
// against asks for them, and a shot which was retired is not evidence and is
// used never.
//
// # What it refuses
//
// Fewer than three distinct points in plan is a diagnostic saying how many were
// found and how many are needed, and no surface. So are points which all fall on
// one line: they bound no area, and a surface over them would have to invent
// which way the ground fell away from that line.
//
// # Determinism
//
// The same points and the same parameters give the same surface, byte for byte,
// on every run and on every machine. The points are ordered by record identity
// before anything is built from them, coincident shots are resolved by a rule
// which does not depend on the order they were read in, and the facets and the
// hull are sorted canonically on the way out. A surface which came back with its
// triangles in a different order each run would make every artefact derived from
// it differ from itself.
func (g *Graph) SurfaceWithin(subject ID, against SurfaceDerivation) (Surface, []Diagnostic) {
	if g == nil {
		return Surface{}, nil
	}

	against = against.normalised()

	if !against.Method.Known() {
		return Surface{}, []Diagnostic{{
			Severity: SeverityError,
			Span:     Span{Start: Position{Path: g.root}},
			Message: fmt.Sprintf(
				"expected a surface of %s to be derived by one of %s, found %s",
				subject, spellMethods(), against.Method,
			),
			Hint: "how a surface is interpolated is a closed set the engine implements, not registry data: a name " +
				"nothing here implements is a surface nobody could be handed",
		}}
	}

	digest, _ := g.Digest()

	key := SurfaceKey{
		Digest:     digest,
		Subject:    subject,
		Method:     against.Method,
		Parameters: surfaceParameterText(against.Parameters()),
	}

	if cached, ok := against.Against.Cache.LookupSurface(key); ok {
		return cached, nil
	}

	surface, diags := g.surface(digest, subject, against)

	if len(diags) == 0 {
		// Only a clean derivation is cached, exactly as in [Graph.Derive]: a
		// run which reported something recomputes it every time, so what a run
		// reports never depends on what a build output directory happens to
		// hold.
		_ = against.Against.Cache.StoreSurface(key, surface)
	}

	return surface, diags
}

// surface is [Graph.SurfaceWithin] with the cache already consulted and missed.
func (g *Graph) surface(digest Digest, subject ID, against SurfaceDerivation) (Surface, []Diagnostic) {
	members, diags := g.ObservationsWithin(subject, against.Against)

	if members.Len() == 0 && anyError(diags) {
		// The region could not be read at all, and the reason is already on a
		// diagnostic. Saying that no shots were found on top of it would report
		// a consequence as though it were a second problem.
		return Surface{}, diags
	}

	surface := Surface{
		subject:    subject,
		digest:     digest,
		method:     against.Method,
		parameters: against.Parameters(),
		slack:      members.Tolerance().Value,
		roughness:  against.Roughness,
	}

	taken := members.Inside()
	if against.Ambiguous {
		taken = append(taken, members.Ambiguous()...)
	}

	shots := make([]SurfacePoint, 0, len(taken))
	for _, member := range taken {
		surface.frame, surface.unit = member.Frame(), member.Unit()
		shots = append(shots, surfacePointOf(member, against.Systematic))
	}

	// Ordered before anything is built from them. Two slices were concatenated
	// above, so the order they arrive in is which slice a shot was in rather
	// than anything about the shot.
	slices.SortFunc(shots, func(a, b SurfacePoint) int {
		return strings.Compare(string(a.observation), string(b.observation))
	})

	surface.points = mergeCoincident(shots, surface.slack)

	// A surface which could not be derived still comes back carrying what was
	// found. [Surface.Derived] is false either way, and the points are what a
	// caller reading the diagnostic wants to look at next: which two shots the
	// region did hold is the question "found 2" immediately raises.
	if len(surface.points) < minimumSurfacePoints {
		return surface, append(diags, tooFewPoints(g, subject, against, len(surface.points)))
	}

	plan := make([]vec, 0, len(surface.points))
	for i := range surface.points {
		plan = append(plan, surface.plan(i))
	}

	surface.hull = convexHull(plan)
	if len(surface.hull) < 3 {
		return surface, append(diags, collinearPoints(g, subject, against, len(surface.points)))
	}

	if against.Method == SurfaceTIN {
		surface.facets = triangulate(plan)
	}

	return surface, diags
}

// surfacePointOf reads one placed shot as a point of a surface, under whatever
// the derivation stated about the occupations behind the shots.
func surfacePointOf(member Membership, sessions []SessionSystematic) SurfacePoint {
	observation := member.Observation()

	point := SurfacePoint{
		observation: observation.ID,
		session:     observation.Session,
		at:          member.At(),
		unit:        member.Unit(),
		sigma:       observation.VerticalPrecision,
		carried:     member.Carried(),
		ambiguous:   member.Ambiguous(),
	}

	// A carried shot is known no better than the transform which carried it,
	// and *how* it is known no better matters: the fit's own scatter is
	// independent and combines in quadrature, while the control the fit was tied
	// to is shared with every other shot carried across the same transform, so
	// it is kept as a term rather than folded in. A transform whose accuracy
	// nothing states adds nothing here — the whole budget is refused rather than
	// half read — and the shot comes back marked ambiguous by the boundary rule
	// instead.
	if uncertainty, err := member.Budget().Combined(); err == nil && uncertainty.Unit == member.Unit() {
		for _, term := range member.Budget().Terms() {
			if term.Kind == TermSystematic && term.Source != "" {
				point.shared = append(point.shared, SurfaceTerm{
					Kind:      TermSystematic,
					Name:      term.Name,
					Source:    term.Source,
					Magnitude: math.Abs(term.Magnitude),
					From:      []ID{observation.ID},
				})
				continue
			}
			point.sigma = math.Hypot(point.sigma, term.Magnitude)
		}
	}

	// The occupation's own shared error, where somebody stated one. It is the
	// term which makes a whole afternoon's shots move together, and it is
	// stated on the derivation because no observation record carries it.
	if magnitude, stated := statedSystematic(sessions, observation.Session); stated {
		point.shared = append(point.shared, SurfaceTerm{
			Kind:      TermSystematic,
			Name:      string(observation.Session),
			Source:    observation.Session,
			Magnitude: magnitude,
			From:      []ID{observation.ID},
		})
	}

	slices.SortFunc(point.shared, func(a, b SurfaceTerm) int {
		return strings.Compare(string(a.Source), string(b.Source))
	})

	return point
}

// statedSystematic is what the derivation said one occupation's shared error is
// worth, and whether it said anything at all.
func statedSystematic(sessions []SessionSystematic, session ID) (float64, bool) {
	if session == "" {
		return 0, false
	}

	for _, stated := range sessions {
		if stated.Session == session {
			return stated.Magnitude, true
		}
	}
	return 0, false
}

// mergeCoincident resolves shots which are one point in plan into one point.
//
// Two shots of one mark cannot both be a vertex: a triangulation over them has a
// facet of no area, and an elevation asked for at the mark would be whichever of
// the two the arithmetic reached first. Which of them stands for the mark is the
// better measured one — the smaller vertical uncertainty — and the lower record
// identity where they are equally good, so the answer does not depend on the
// order the corpus was walked in.
//
// The shots which did not win are kept on the winner rather than dropped, so the
// provenance of the surface is every record behind it.
func mergeCoincident(shots []SurfacePoint, slack float64) []SurfacePoint {
	if len(shots) == 0 {
		return nil
	}

	// Clusters are connected components of "within slack of each other in
	// plan", which is an answer about the set rather than about the order it is
	// walked in.
	group := make([]int, len(shots))
	for i := range group {
		group[i] = i
	}

	root := func(i int) int {
		for group[i] != i {
			group[i] = group[group[i]]
			i = group[i]
		}
		return i
	}

	for i := range shots {
		for j := i + 1; j < len(shots); j++ {
			apart := math.Hypot(shots[i].at[0]-shots[j].at[0], shots[i].at[1]-shots[j].at[1])
			if apart > slack {
				continue
			}
			if a, b := root(i), root(j); a != b {
				group[max(a, b)] = min(a, b)
			}
		}
	}

	held := make(map[int][]int)
	var order []int
	for i := range shots {
		lead := root(i)
		if _, seen := held[lead]; !seen {
			order = append(order, lead)
		}
		held[lead] = append(held[lead], i)
	}

	points := make([]SurfacePoint, 0, len(order))
	for _, lead := range order {
		cluster := held[lead]

		best := cluster[0]
		for _, i := range cluster[1:] {
			// The comparison is of what each shot is worth in total — its own
			// error and everything it shares — rather than of the independent
			// part alone. A shot carried across an unmeasured control is not the
			// better shot of a mark because its instrument happened to report a
			// tighter figure.
			held, against := shots[i].Uncertainty(), shots[best].Uncertainty()
			switch {
			case held < against:
				best = i
			case held == against && shots[i].observation < shots[best].observation:
				best = i
			}
		}

		point := shots[best]
		for _, i := range cluster {
			if i != best {
				point.coincident = append(point.coincident, shots[i].observation)
			}
		}
		slices.Sort(point.coincident)

		points = append(points, point)
	}

	slices.SortFunc(points, func(a, b SurfacePoint) int {
		return strings.Compare(string(a.observation), string(b.observation))
	})

	return points
}

// tooFewPoints is the diagnostic for a region with not enough shots in it to
// derive anything, which says how many were found and how many it takes.
func tooFewPoints(g *Graph, subject ID, against SurfaceDerivation, found int) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Span:     g.spanOfNode(subject),
		Message: fmt.Sprintf(
			"expected at least %s to derive a %s surface of %s, found %s",
			plural(minimumSurfacePoints, "distinct observation point"), against.Method, subject,
			plural(found, "point"),
		),
		Hint: "a surface is derived from the shots inside the region and from nothing else; three points is what it " +
			"takes to bound any area at all, and shots of one mark count once",
	}
}

// collinearPoints is the diagnostic for a region whose shots all fall on one
// line.
func collinearPoints(g *Graph, subject ID, against SurfaceDerivation, found int) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Span:     g.spanOfNode(subject),
		Message: fmt.Sprintf(
			"expected the %s of a %s surface of %s to bound an area, found that they lie on one line",
			plural(found, "distinct observation point"), against.Method, subject,
		),
		Hint: "points on a line say nothing about which way the ground falls away from it; a surface over them " +
			"would be an invention, so a shot off the line is what is missing",
	}
}

// spanOfNode is where a node was named, for a diagnostic about a derivation
// which is nobody's line in particular.
func (g *Graph) spanOfNode(subject ID) Span {
	if node, held := g.nodes.Node(subject); held {
		return g.named(node)
	}
	return Span{Start: Position{Path: g.root}}
}

// spellMethods writes the closed set of interpolation methods the way a
// diagnostic naming the alternatives needs it.
func spellMethods() string {
	names := make([]string, 0, len(surfaceMethods))
	for _, method := range surfaceMethods {
		names = append(names, string(method))
	}
	return strings.Join(names, ", ")
}

// anyError reports whether a diagnostic set holds anything which stopped an
// answer being arrived at.
func anyError(diags []Diagnostic) bool {
	for _, diag := range diags {
		if diag.Severity == SeverityError {
			return true
		}
	}
	return false
}

// edgeDistance is how far a point lies to the left of a directed edge, which is
// positive inside a counter-clockwise ring.
func edgeDistance(point, from, to vec) float64 {
	dx, dy := to.X-from.X, to.Y-from.Y

	length := math.Hypot(dx, dy)
	if length == 0 {
		return -math.Hypot(point.X-from.X, point.Y-from.Y)
	}

	return (dx*(point.Y-from.Y) - dy*(point.X-from.X)) / length
}

// minEdgeDistance is how far a point is inside a triangle, negative outside it.
func minEdgeDistance(point, a, b, c vec) float64 {
	return math.Min(
		edgeDistance(point, a, b),
		math.Min(edgeDistance(point, b, c), edgeDistance(point, c, a)),
	)
}

// barycentric is how much each corner of a triangle counts at a point, summing
// to one.
//
// The weights are clamped and renormalised, so a point a hair outside the
// triangle — which is the only way one gets here — is answered from the edge it
// is outside rather than by continuing the facet's slope past its corner.
func barycentric(point, a, b, c vec) []float64 {
	area := cross(a, b, c)
	if area == 0 {
		return []float64{1.0 / 3, 1.0 / 3, 1.0 / 3}
	}

	weights := []float64{
		cross(point, b, c) / area,
		cross(a, point, c) / area,
		cross(a, b, point) / area,
	}

	var total float64
	for i, weight := range weights {
		weights[i] = math.Max(0, weight)
		total += weights[i]
	}

	if total <= 0 {
		return []float64{1.0 / 3, 1.0 / 3, 1.0 / 3}
	}

	for i := range weights {
		weights[i] /= total
	}

	return weights
}

// cross is twice the signed area of a triangle, positive counter-clockwise.
func cross(a, b, c vec) float64 {
	return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
}

// convexHull is the convex hull of a set of points in plan, counter-clockwise,
// as indices into the set.
//
// It is Andrew's monotone chain over the points in a canonical order, so the
// hull of a set is the same list whichever order the set arrived in. Points on
// the interior of a hull edge are not on the hull: a collinear run would put a
// vertex on the boundary which contributes nothing and which two runs could
// disagree about.
func convexHull(plan []vec) []int {
	if len(plan) < 3 {
		return nil
	}

	order := make([]int, len(plan))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int {
		if by := compareVec(plan[a], plan[b]); by != 0 {
			return by
		}
		return a - b
	})

	chain := func(order []int) []int {
		var half []int
		for _, index := range order {
			for len(half) >= 2 && cross(plan[half[len(half)-2]], plan[half[len(half)-1]], plan[index]) <= 0 {
				half = half[:len(half)-1]
			}
			half = append(half, index)
		}
		return half[:len(half)-1]
	}

	lower := chain(order)

	reversed := slices.Clone(order)
	slices.Reverse(reversed)
	upper := chain(reversed)

	hull := append(lower, upper...)
	if len(hull) < 3 {
		return nil
	}

	return rotatedToLowest(hull)
}

// rotatedToLowest turns a ring so that its lowest index comes first, which is
// what makes one ring one list rather than one list per starting point.
func rotatedToLowest(ring []int) []int {
	lowest := 0
	for i, index := range ring {
		if index < ring[lowest] {
			lowest = i
		}
	}
	return append(slices.Clone(ring[lowest:]), ring[:lowest]...)
}

// triangulate is the Delaunay triangulation of a set of points in plan, as
// triples of indices into the set, counter-clockwise.
//
// It is Bowyer-Watson: every point is inserted into a triangulation which starts
// as one triangle large enough to hold them all, the triangles whose circumcircle
// the point falls inside are removed, and the hole they leave is re-triangulated
// to the new point. The triangles touching the enclosing triangle are dropped at
// the end, which leaves the triangulation of the points themselves and nothing
// else.
//
// The result is sorted canonically. A Delaunay triangulation of points in
// general position is unique, so the order the triangles are found in is an
// artefact of the insertion and never part of the answer; points which are not
// in general position — four on a circle — are triangulated by an insertion order
// which is itself fixed, so the answer is the same on every run either way.
func triangulate(plan []vec) []SurfaceFacet {
	if len(plan) < 3 {
		return nil
	}

	// The three corners of the enclosing triangle sit past the end of the
	// points, so that a triangle touching one of them is recognised by an index
	// rather than by a coordinate comparison.
	work := make([]vec, len(plan), len(plan)+3)
	copy(work, plan)

	low, high := plan[0], plan[0]
	for _, point := range plan[1:] {
		low = vec{math.Min(low.X, point.X), math.Min(low.Y, point.Y)}
		high = vec{math.Max(high.X, point.X), math.Max(high.Y, point.Y)}
	}

	span := math.Max(high.X-low.X, high.Y-low.Y)
	if span <= 0 {
		return nil
	}

	middle := vec{(low.X + high.X) / 2, (low.Y + high.Y) / 2}
	work = append(work,
		vec{middle.X - 20*span, middle.Y - span},
		vec{middle.X + 20*span, middle.Y - span},
		vec{middle.X, middle.Y + 20*span},
	)

	facets := []SurfaceFacet{{len(plan), len(plan) + 1, len(plan) + 2}}

	for index := range plan {
		point := work[index]

		var kept, bad []SurfaceFacet
		for _, facet := range facets {
			if circumscribes(work[facet[0]], work[facet[1]], work[facet[2]], point) {
				bad = append(bad, facet)
				continue
			}
			kept = append(kept, facet)
		}

		if len(bad) == 0 {
			// The point is on the circumcircle of everything and inside
			// nothing, which is rounding rather than geometry. It adds no
			// triangle, and the facet it falls on answers for it.
			continue
		}

		// Every triangle is counter-clockwise, so an edge interior to the hole
		// appears once in each direction and an edge on its boundary appears
		// once. That is the whole test, and it needs no sorting of vertex pairs.
		interior := make(map[[2]int]bool, 3*len(bad))
		edges := make([][2]int, 0, 3*len(bad))
		for _, facet := range bad {
			for _, edge := range [3][2]int{{facet[0], facet[1]}, {facet[1], facet[2]}, {facet[2], facet[0]}} {
				interior[edge] = true
				edges = append(edges, edge)
			}
		}

		for _, edge := range edges {
			if interior[[2]int{edge[1], edge[0]}] {
				continue
			}
			kept = append(kept, SurfaceFacet{edge[0], edge[1], index})
		}

		facets = kept
	}

	out := make([]SurfaceFacet, 0, len(facets))
	for _, facet := range facets {
		if facet[0] >= len(plan) || facet[1] >= len(plan) || facet[2] >= len(plan) {
			continue
		}
		out = append(out, SurfaceFacet(rotatedToLowest(facet[:])))
	}

	slices.SortFunc(out, func(a, b SurfaceFacet) int {
		for i := range a {
			if a[i] != b[i] {
				return a[i] - b[i]
			}
		}
		return 0
	})

	return out
}

// circumscribes reports whether a point falls strictly inside the circumcircle
// of a counter-clockwise triangle.
func circumscribes(a, b, c, point vec) bool {
	ax, ay := a.X-point.X, a.Y-point.Y
	bx, by := b.X-point.X, b.Y-point.Y
	cx, cy := c.X-point.X, c.Y-point.Y

	return (ax*ax+ay*ay)*(bx*cy-cx*by)-
		(bx*bx+by*by)*(ax*cy-cx*ay)+
		(cx*cx+cy*cy)*(ax*by-bx*ay) > 0
}

// surfaceEntry is one derived surface as it is written into the cache.
//
// The key is written into the entry as well as into the path, exactly as
// [cacheEntry] writes it, so an entry moved or copied by hand is discarded
// rather than answered with.
type surfaceEntry struct {
	Version    int                  `json:"version"`
	Digest     Digest               `json:"digest"`
	Subject    ID                   `json:"subject"`
	Method     SurfaceMethod        `json:"method"`
	Parameters []SurfaceParameter   `json:"parameters"`
	Frame      ID                   `json:"frame,omitempty"`
	Unit       Unit                 `json:"unit,omitempty"`
	Slack      float64              `json:"slack,omitempty"`
	Roughness  float64              `json:"roughness,omitempty"`
	Points     []surfacePointRecord `json:"points,omitempty"`
	Facets     []SurfaceFacet       `json:"facets,omitempty"`
	Hull       []int                `json:"hull,omitempty"`
}

// surfacePointRecord is one point of a surface as it is written.
//
// Sigma is the independent part alone and the shared terms are written beside
// it, because the two do not combine the same way: an entry which stored the
// combined figure would answer a fall as though the base station behind both
// ends were two different base stations, which is the one arithmetic this whole
// budget exists to get right.
type surfacePointRecord struct {
	Observation ID                  `json:"observation"`
	Session     ID                  `json:"session,omitempty"`
	At          Point               `json:"at"`
	Sigma       float64             `json:"sigma"`
	Shared      []surfaceTermRecord `json:"shared,omitempty"`
	Carried     bool                `json:"carried,omitempty"`
	Ambiguous   bool                `json:"ambiguous,omitempty"`
	Coincident  []ID                `json:"coincident,omitempty"`
}

// surfaceTermRecord is one systematic term of a point as it is written.
type surfaceTermRecord struct {
	Name      string  `json:"name"`
	Source    ID      `json:"source"`
	Magnitude float64 `json:"magnitude"`
}

// LookupSurface returns the surface stored under key, and whether anything
// usable was.
//
// It is [Cache.Lookup] for a surface and behaves exactly as it does: an entry
// which is there and does not verify is discarded and reported as a miss, never
// returned and never raised.
func (c *Cache) LookupSurface(key SurfaceKey) (Surface, bool) {
	if !key.cacheable() {
		c.record(func(s *CacheStats) { s.Misses++ })
		return Surface{}, false
	}

	var surface Surface

	held := c.fetch(key.Digest, key.entry(), func(payload []byte) bool {
		var ok bool
		surface, ok = decodeSurfaceEntry(payload, key)
		return ok
	})
	if !held {
		return Surface{}, false
	}

	return surface, true
}

// StoreSurface writes a surface under key, replacing whatever was there.
//
// A key with no digest, or one naming no region, stores nothing and reports no
// error: nothing pins such an entry, so writing one is the one way this cache
// could serve a stale answer.
func (c *Cache) StoreSurface(key SurfaceKey, surface Surface) error {
	if c == nil || !key.cacheable() {
		return nil
	}

	payload, err := encodeSurfaceEntry(key, surface)
	if err != nil {
		c.record(func(s *CacheStats) { s.Errors++ })
		return CacheError{Op: "store", Path: c.dir, Err: err}
	}

	return c.put(key.Digest, key.entry(), payload)
}

// encodeSurfaceEntry writes a surface as the payload of one cache entry.
func encodeSurfaceEntry(key SurfaceKey, surface Surface) ([]byte, error) {
	entry := surfaceEntry{
		Version:    cacheVersion,
		Digest:     key.Digest,
		Subject:    key.Subject,
		Method:     surface.method,
		Parameters: surface.parameters,
		Frame:      surface.frame,
		Unit:       surface.unit,
		Slack:      surface.slack,
		Roughness:  surface.roughness,
		Facets:     surface.facets,
		Hull:       surface.hull,
	}

	for _, point := range surface.points {
		record := surfacePointRecord{
			Observation: point.observation,
			Session:     point.session,
			At:          point.at,
			Sigma:       point.sigma,
			Carried:     point.carried,
			Ambiguous:   point.ambiguous,
			Coincident:  point.coincident,
		}

		for _, term := range point.shared {
			record.Shared = append(record.Shared, surfaceTermRecord{
				Name:      term.Name,
				Source:    term.Source,
				Magnitude: term.Magnitude,
			})
		}

		entry.Points = append(entry.Points, record)
	}

	return json.Marshal(entry)
}

// decodeSurfaceEntry reads a surface back, reporting whether it verified against
// the key it was asked for.
//
// The indices are checked as well as the key. An entry whose facets name points
// it does not hold would panic the first time somebody asked it for a level, and
// a build output is never allowed to do that to a run.
func decodeSurfaceEntry(payload []byte, key SurfaceKey) (Surface, bool) {
	var entry surfaceEntry
	if err := json.Unmarshal(payload, &entry); err != nil {
		return Surface{}, false
	}

	if entry.Version != cacheVersion {
		return Surface{}, false
	}
	if entry.Digest != key.Digest || entry.Subject != key.Subject || entry.Method != key.Method {
		return Surface{}, false
	}
	if surfaceParameterText(entry.Parameters) != key.Parameters {
		return Surface{}, false
	}

	surface := Surface{
		subject:    entry.Subject,
		digest:     entry.Digest,
		frame:      entry.Frame,
		unit:       entry.Unit,
		method:     entry.Method,
		parameters: entry.Parameters,
		facets:     entry.Facets,
		hull:       entry.Hull,
		slack:      entry.Slack,
		roughness:  entry.Roughness,
	}

	for _, record := range entry.Points {
		if record.Observation == "" {
			return Surface{}, false
		}

		point := SurfacePoint{
			observation: record.Observation,
			session:     record.Session,
			at:          record.At,
			unit:        entry.Unit,
			sigma:       record.Sigma,
			carried:     record.Carried,
			ambiguous:   record.Ambiguous,
			coincident:  record.Coincident,
		}

		for _, term := range record.Shared {
			if term.Source == "" {
				return Surface{}, false
			}
			point.shared = append(point.shared, SurfaceTerm{
				Kind:      TermSystematic,
				Name:      term.Name,
				Source:    term.Source,
				Magnitude: term.Magnitude,
				From:      []ID{record.Observation},
			})
		}

		surface.points = append(surface.points, point)
	}

	for _, facet := range surface.facets {
		for _, index := range facet {
			if index < 0 || index >= len(surface.points) {
				return Surface{}, false
			}
		}
	}
	for _, index := range surface.hull {
		if index < 0 || index >= len(surface.points) {
			return Surface{}, false
		}
	}

	return surface, true
}
