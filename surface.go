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
	return d
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

	// sigma is the standard uncertainty of that elevation, one sigma
	// ([0006](docs/decisions/0006-accuracy-is-one-sigma.md)): the record's own
	// vertical precision, combined with the transform's where the shot was
	// carried in from another frame.
	sigma float64

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

// Elevation returns the third component of the coordinate, which is what a
// surface is a surface of.
func (p SurfacePoint) Elevation() float64 { return p.at[2] }

// Uncertainty returns the standard uncertainty of that elevation, one sigma.
func (p SurfacePoint) Uncertainty() float64 { return p.sigma }

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

	// sigma is the standard uncertainty of that answer, one sigma, propagated
	// from the shots it was interpolated from.
	sigma float64

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
// It is propagated from the shots behind the answer and from nothing else. It is
// not a statement about how well the interpolation models the ground between
// them, which is a question no arithmetic over the shots can answer.
func (e Elevation) Uncertainty() float64 { return e.sigma }

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
		decimal(e.Value()), decimal(e.sigma), e.method, strings.Join(names, ", "))
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
		return Elevation{}, false
	}

	return s.answer(at, indices, weights), true
}

// answer assembles an elevation from the points which contributed and how much
// each contributed, which is the same arithmetic whichever method chose them.
//
// The contributions are sorted by record identity on the way out. Which order a
// method happened to find its points in is an implementation detail, and a
// caller diffing two runs should not see one.
func (s Surface) answer(at Point, indices []int, weights []float64) Elevation {
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

	elevation := Elevation{at: at, method: s.method}

	var value, variance float64
	for _, i := range order {
		point, weight := s.points[indices[i]], weights[i]

		value += weight * point.at[2]

		// The terms are combined in quadrature as independent one-sigma
		// figures. They are not: two shots of one afternoon share a base
		// station, and a systematic error in it moves both the same way. What
		// this is, is the floor — the uncertainty the answer has even where the
		// shots are independent — and [SurfacePoint.Session] is what a caller
		// needs to do better.
		variance += (weight * point.sigma) * (weight * point.sigma)

		elevation.from = append(elevation.from, point.observation)
		elevation.weights = append(elevation.weights, weight)
	}

	elevation.at[2] = value
	elevation.sigma = math.Sqrt(variance)

	return elevation
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
	}

	taken := members.Inside()
	if against.Ambiguous {
		taken = append(taken, members.Ambiguous()...)
	}

	shots := make([]SurfacePoint, 0, len(taken))
	for _, member := range taken {
		surface.frame, surface.unit = member.Frame(), member.Unit()
		shots = append(shots, surfacePointOf(member))
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

// surfacePointOf reads one placed shot as a point of a surface.
func surfacePointOf(member Membership) SurfacePoint {
	observation := member.Observation()

	point := SurfacePoint{
		observation: observation.ID,
		session:     observation.Session,
		at:          member.At(),
		sigma:       observation.VerticalPrecision,
		carried:     member.Carried(),
		ambiguous:   member.Ambiguous(),
	}

	// A carried shot is known no better than the transform which carried it.
	// The two are independent one-sigma figures and combine in quadrature; a
	// transform whose accuracy nothing states adds nothing here, and the shot
	// comes back marked ambiguous by the boundary rule instead.
	if uncertainty, err := member.Budget().Combined(); err == nil && uncertainty.Unit == member.Unit() {
		point.sigma = math.Hypot(point.sigma, uncertainty.Standard())
	}

	return point
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
			switch {
			case shots[i].sigma < shots[best].sigma:
				best = i
			case shots[i].sigma == shots[best].sigma && shots[i].observation < shots[best].observation:
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
	Points     []surfacePointRecord `json:"points,omitempty"`
	Facets     []SurfaceFacet       `json:"facets,omitempty"`
	Hull       []int                `json:"hull,omitempty"`
}

// surfacePointRecord is one point of a surface as it is written.
type surfacePointRecord struct {
	Observation ID      `json:"observation"`
	Session     ID      `json:"session,omitempty"`
	At          Point   `json:"at"`
	Sigma       float64 `json:"sigma"`
	Carried     bool    `json:"carried,omitempty"`
	Ambiguous   bool    `json:"ambiguous,omitempty"`
	Coincident  []ID    `json:"coincident,omitempty"`
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
		Facets:     surface.facets,
		Hull:       surface.hull,
	}

	for _, point := range surface.points {
		entry.Points = append(entry.Points, surfacePointRecord{
			Observation: point.observation,
			Session:     point.session,
			At:          point.at,
			Sigma:       point.sigma,
			Carried:     point.carried,
			Ambiguous:   point.ambiguous,
			Coincident:  point.coincident,
		})
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
	}

	for _, record := range entry.Points {
		if record.Observation == "" {
			return Surface{}, false
		}
		surface.points = append(surface.points, SurfacePoint{
			observation: record.Observation,
			session:     record.Session,
			at:          record.At,
			sigma:       record.Sigma,
			carried:     record.Carried,
			ambiguous:   record.Ambiguous,
			coincident:  record.Coincident,
		})
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
