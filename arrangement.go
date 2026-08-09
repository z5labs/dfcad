// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"math"
	"slices"
)

// vec is a point in the plane a region has been projected into, in the linear
// unit of the frame the region is declared in.
//
// Nothing here is a coordinate in the model. A plane figure is projected into
// an orthonormal basis of its own plane, overlaid there and lifted back, and
// these are the two numbers it is overlaid with.
type vec struct{ X, Y float64 }

// add adds two plane points component by component.
func (v vec) add(o vec) vec { return vec{v.X + o.X, v.Y + o.Y} }

// sub subtracts one plane point from another component by component.
func (v vec) sub(o vec) vec { return vec{v.X - o.X, v.Y - o.Y} }

// scale multiplies both components by a factor.
func (v vec) scale(f float64) vec { return vec{v.X * f, v.Y * f} }

// dot is the dot product of two plane points read as vectors.
func (v vec) dot(o vec) float64 { return v.X*o.X + v.Y*o.Y }

// cross is the z component of the cross product of two plane points read as
// vectors, which is twice the signed area of the triangle they make with the
// origin.
func (v vec) cross(o vec) float64 { return v.X*o.Y - v.Y*o.X }

// length is the length of a plane point read as a vector.
func (v vec) length() float64 { return math.Hypot(v.X, v.Y) }

// left is the vector a quarter turn anticlockwise from this one, which is the
// side of a directed segment the interior of a ring written anticlockwise lies
// on.
func (v vec) left() vec { return vec{-v.Y, v.X} }

// compareVec orders plane points lexicographically.
//
// It is the order every deterministic decision in this file is made in: which
// point of a set of coincident ones is the one the others snap to, which ring a
// traversal starts from, and where a ring starts. None of those may depend on
// the order operands were passed in, and an order over the points themselves is
// what makes them not.
func compareVec(a, b vec) int {
	if a.X != b.X {
		return cmpFloat(a.X, b.X)
	}
	return cmpFloat(a.Y, b.Y)
}

// cmpFloat orders two numbers.
func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// contour is one closed ring of plane points, in traversal order and without
// the first point repeated at the end.
//
// A ring written anticlockwise bounds what is inside it and one written
// clockwise bounds a hole, which is the convention every ring in this file is
// held in and the one [arrangement.trace] emits.
type contour []vec

// signedArea is the area the ring encloses, positive where it runs
// anticlockwise and negative where it runs clockwise.
func (c contour) signedArea() float64 {
	var twice float64
	for i, point := range c {
		twice += point.cross(c[(i+1)%len(c)])
	}
	return twice / 2
}

// perimeter is the total length of the ring's segments.
func (c contour) perimeter() float64 {
	var total float64
	for i, point := range c {
		total += c[(i+1)%len(c)].sub(point).length()
	}
	return total
}

// width is how wide the ring is on average, which is the measure a sliver is
// judged by: a ring narrower than the declared tolerance is two boundaries the
// project cannot tell apart, not a shape.
//
// It is twice the area over the perimeter, which is the width of the rectangle
// of the same area and perimeter and is exact for one.
func (c contour) width() float64 {
	perimeter := c.perimeter()
	if perimeter == 0 {
		return 0
	}
	return 2 * math.Abs(c.signedArea()) / perimeter
}

// holds reports whether a point falls inside the ring, by the non-zero winding
// rule.
//
// A point on the ring's own boundary is not a question this answers reliably
// and is never asked one: it is used to nest a ring inside another, and the
// rings of an arrangement meet only at their ends.
func (c contour) holds(point vec) bool {
	var winding int
	for i, a := range c {
		winding += crossing(a, c[(i+1)%len(c)], point)
	}
	return winding != 0
}

// touches reports whether a point lies on the ring's own boundary, no further
// from one of its segments than the tolerance two positions are one position
// within.
//
// It is what makes [contour.holds] safe to ask: the winding count is exact for
// a point off the boundary and arbitrary for one on it, so a point this reports
// is one the count is never taken at.
func (c contour) touches(point vec, tolerance float64) bool {
	for i, a := range c {
		if distanceToSegment(point, a, c[(i+1)%len(c)]) <= tolerance {
			return true
		}
	}
	return false
}

// distanceToSegment is how far a point lies from the nearest point of a
// segment, which is a perpendicular where the foot of it falls between the two
// ends and the distance to the nearer end otherwise.
func distanceToSegment(point, a, b vec) float64 {
	along := b.sub(a)

	length := along.dot(along)
	if length == 0 {
		return point.sub(a).length()
	}

	at := min(max(point.sub(a).dot(along)/length, 0), 1)

	return point.sub(a.add(along.scale(at))).length()
}

// nesting is how one ring of a region sits against another: the two answers the
// even-odd rule can be applied to, and the two ways there is no answer to apply
// it to.
//
// The two refusals are told apart because they are different things to have
// drawn and want different things said back. Rings which cross are two shapes
// overlapping in part; rings which cannot be told apart are one boundary written
// twice. Reporting either as the other sends whoever reads it to the wrong
// loop.
type nesting int

const (
	// ringBeside is a ring which lies outside the other one, touching it or
	// not. Both are added.
	ringBeside nesting = iota

	// ringWithin is a ring the whole of which lies inside the other one, which
	// is what makes it a hole in it.
	ringWithin

	// ringsCrossing is two rings each of which holds part of the other and
	// neither of which holds the whole of it.
	ringsCrossing

	// ringsIndistinct is a ring no point of which is off the other's boundary,
	// so there is nothing to decide which of them is inside which.
	ringsIndistinct
)

// nestedIn reports how one ring sits inside, beside or across another.
//
// Every probe of the inner ring which is not on the outer ring's boundary is
// asked, and they have to agree. Asking one point is what a nesting used to be
// decided by, and the point asked was the corner the traversal happened to
// start from — so a ring whose first corner fell on the other ring's boundary
// was nested or not nested according to which corner somebody wrote the loop
// down from, and two rectangles abutting along a wall cancelled instead of
// unioning. Rotating a loop's edge list cannot change a consensus taken over
// all of its corners, which is what makes the answer a property of the shape.
//
// Disagreement is the two rings crossing: neither holds the whole of the other,
// so neither is a hole in the other and the even-odd rule has nothing to say
// about them. Nothing to disagree — every probe on the other ring's boundary —
// is the two rings being one boundary as far as this can tell. Both come back
// as a refusal rather than as a number, because the number would be an area no
// shape has.
func nestedIn(inner, outer contour, tolerance float64) nesting {
	var inside, answered bool

	for _, probe := range probesOf(inner) {
		if outer.touches(probe, tolerance) {
			continue
		}

		held := outer.holds(probe)

		if !answered {
			inside, answered = held, true
			continue
		}

		if held != inside {
			return ringsCrossing
		}
	}

	switch {
	case !answered:
		return ringsIndistinct
	case inside:
		return ringWithin
	default:
		return ringBeside
	}
}

// probesOf is the points a ring is tested against another ring by: every corner
// of it, and then the midpoint of every segment.
//
// The midpoints are what keep a ring whose every corner falls on the other
// ring's boundary answerable — a square inscribed in another at its edge
// midpoints has no corner off it, and every one of its own midpoints is
// strictly inside.
func probesOf(c contour) []vec {
	probes := make([]vec, 0, 2*len(c))

	probes = append(probes, c...)
	for i, point := range c {
		probes = append(probes, point.add(c[(i+1)%len(c)]).scale(0.5))
	}

	return probes
}

// reversed is the ring traversed the other way round, which turns what is
// inside it into a hole and back.
func (c contour) reversed() contour {
	out := make(contour, 0, len(c))
	for i := len(c) - 1; i >= 0; i-- {
		out = append(out, c[i])
	}
	return out
}

// rotated is the ring written from its lexicographically smallest point.
//
// A ring is the same ring whichever of its points it was written from, and two
// results which are the same shape must be the same value — so where the
// traversal happened to start is normalised away rather than left in the
// output for a caller to compare around.
func (c contour) rotated() contour {
	if len(c) == 0 {
		return c
	}

	at := 0
	for i, point := range c {
		if compareVec(point, c[at]) < 0 {
			at = i
		}
	}

	out := make(contour, 0, len(c))
	out = append(out, c[at:]...)
	out = append(out, c[:at]...)

	return out
}

// crossing is the signed contribution one directed segment makes to the winding
// number of a point, counting the crossings of the ray which leaves the point
// along the positive first axis.
//
// It is the standard winding-number count: an upward segment passing to the
// right of the point winds once anticlockwise round it and a downward one winds
// once clockwise. The comparisons are half open, which is what stops a ray
// through a vertex counting the two segments meeting there twice or not at all.
func crossing(a, b, point vec) int {
	if a.Y <= point.Y {
		if b.Y > point.Y && isLeft(a, b, point) > 0 {
			return 1
		}
		return 0
	}

	if b.Y <= point.Y && isLeft(a, b, point) < 0 {
		return -1
	}
	return 0
}

// isLeft is positive where the point lies to the left of the directed segment,
// negative where it lies to the right and zero where it is on the line through
// it.
func isLeft(a, b, point vec) float64 {
	return (b.X-a.X)*(point.Y-a.Y) - (point.X-a.X)*(b.Y-a.Y)
}

// segment is one straight piece of one operand's boundary, as it was handed in
// and before anything was split.
type segment struct {
	a, b vec
}

// link is one piece of the arrangement: a straight stretch of boundary which no
// other boundary crosses anywhere but at its ends.
//
// The two ends are held as indices into the arrangement's points, ordered so
// that the lower index comes first. That canonical direction is what lets a
// stretch walked by both operands, or walked twice by one of them, be one link
// carrying a count rather than two links which happen to coincide — and the
// count is what makes a shared boundary come out right without a rule of its
// own.
type link struct {
	// a and b are the ends, a < b, which is the direction the counts below are
	// measured along.
	a, b int

	// wind is, per operand, how many of that operand's segments run along the
	// link in the canonical direction less how many run against it.
	wind [2]int

	// left and right are, per operand, the winding number of that operand
	// immediately to either side of the link. Whether the operand is filled
	// there is whether its winding number is not zero.
	left, right [2]int
}

// arc is one link of the arrangement selected into a result, walked in the
// direction which keeps the result's interior on its left.
type arc struct {
	from, to int
}

// arrangement is the planar subdivision of the boundaries of up to two
// operands, together with what each operand covers either side of every piece
// of it.
//
// It is the whole of the overlay. Every operation — union, intersection,
// difference, and the self-union an offset is — is this one subdivision walked
// with a different rule for which side of a piece has to be covered for that
// piece to be part of the answer. There is no second algorithm and no special
// case for boundaries which coincide: two operands sharing a stretch of
// boundary share a link, and the counts on it say which way each of them was
// walking.
type arrangement struct {
	// tolerance is the declared distance two points are the same point within,
	// which is the only distance in here that is not read off the geometry.
	tolerance float64

	// segments are the operands' boundaries as handed in.
	segments [2][]segment

	// points are the distinct positions everything is snapped to, in ascending
	// lexicographic order.
	points []vec

	// buckets index those points by a cell of the tolerance grid, which is what
	// keeps snapping from being a comparison against every point already seen.
	buckets map[[2]int][]int

	// links are the pieces of the subdivision, and index finds one by its ends.
	links []*link
	index map[[2]int]int
}

// overlay subdivides two operands' rings and walks the subdivision with one
// rule, returning the rings of the result.
//
// The rule is asked, for one side of one stretch of boundary, whether the
// result covers it given whether each operand does. A stretch with the result
// on one side of it and not the other is a boundary of the result; a stretch
// with the result on both sides or neither is inside it or outside it and is
// dropped. That is every operation in this file, and the operations differ only
// in the rule.
//
// The second operand may be empty, which is how an offset is overlaid: its
// pieces are unioned with each other by the same walk, because a rule which
// asks only whether the first operand covers a side is the non-zero rule and
// the non-zero rule over overlapping anticlockwise rings is their union.
func overlay(subject, clip []contour, tolerance float64, covered func(a, b bool) bool) []contour {
	shift, ok := anchorOf(subject, clip)
	if !ok {
		return nil
	}

	a := &arrangement{
		tolerance: tolerance,
		buckets:   make(map[[2]int][]int),
		index:     make(map[[2]int]int),
	}

	for side, rings := range [2][]contour{subject, clip} {
		for _, ring := range rings {
			a.ring(side, ring, shift)
		}
	}

	a.split()
	a.winding()

	rings := a.trace(covered)
	for i, ring := range rings {
		for j, point := range ring {
			rings[i][j] = point.add(shift)
		}
	}

	return rings
}

// anchorOf is the corner every point is measured from while the overlay runs,
// which is the smallest corner of everything both operands cover.
//
// Subtracting it is what keeps a site surveyed on a national grid from losing
// precision: a coordinate in the millions has fifteen significant figures of
// which the first seven are shared by every point in the arrangement, and where
// two segments cross is the difference between two of them. It is the smallest
// corner rather than any particular point because a minimum over a set does not
// depend on the order the set was walked in, which an overlay's answer must not
// either.
func anchorOf(operands ...[]contour) (vec, bool) {
	var anchor vec
	var found bool

	for _, rings := range operands {
		for _, ring := range rings {
			for _, point := range ring {
				if !found {
					anchor, found = point, true
					continue
				}
				anchor = vec{math.Min(anchor.X, point.X), math.Min(anchor.Y, point.Y)}
			}
		}
	}

	return anchor, found
}

// ring records one operand's ring, measured from the anchor.
func (a *arrangement) ring(side int, ring contour, shift vec) {
	for i, point := range ring {
		next := ring[(i+1)%len(ring)]
		a.segments[side] = append(a.segments[side], segment{a: point.sub(shift), b: next.sub(shift)})
	}
}

// split reduces both operands' boundaries to the links of one subdivision.
//
// Three things happen in order, and the order is the whole of the robustness
// here. Every position anything reaches is collected first — the ends of every
// segment and every place two of them cross. They are then snapped to each
// other at the declared tolerance, in one pass over them sorted, so that which
// position a cluster collapses onto is decided by the positions and not by
// which operand was walked first. Only then is a segment split, and it is split
// at every snapped position lying on it rather than at the crossings computed
// for it — which is what makes a segment ending against the middle of another
// one, and two segments lying along each other, need no case of their own.
func (a *arrangement) split() {
	var candidates []vec

	all := slices.Concat(a.segments[0], a.segments[1])
	for _, one := range all {
		candidates = append(candidates, one.a, one.b)
	}

	for i, one := range all {
		for _, other := range all[i+1:] {
			if at, ok := meeting(one, other); ok {
				candidates = append(candidates, at)
			}
		}
	}

	slices.SortFunc(candidates, compareVec)
	for _, point := range candidates {
		a.intern(point)
	}

	for side := range a.segments {
		for _, one := range a.segments[side] {
			along := a.along(one)
			for i := 0; i+1 < len(along); i++ {
				a.connect(side, along[i], along[i+1])
			}
		}
	}
}

// intern is the index of the point every position within the tolerance of it
// collapses onto, recording a new one where nothing is that close.
func (a *arrangement) intern(point vec) int {
	cell := a.cell(point)

	for x := cell[0] - 1; x <= cell[0]+1; x++ {
		for y := cell[1] - 1; y <= cell[1]+1; y++ {
			for _, i := range a.buckets[[2]int{x, y}] {
				if a.points[i].sub(point).length() <= a.tolerance {
					return i
				}
			}
		}
	}

	a.points = append(a.points, point)
	at := len(a.points) - 1
	a.buckets[cell] = append(a.buckets[cell], at)

	return at
}

// cell is the tolerance grid cell a position falls in.
func (a *arrangement) cell(point vec) [2]int {
	if a.tolerance <= 0 {
		return [2]int{}
	}
	return [2]int{int(math.Floor(point.X / a.tolerance)), int(math.Floor(point.Y / a.tolerance))}
}

// along is every snapped position lying on a segment, in the order the segment
// passes them.
func (a *arrangement) along(one segment) []int {
	direction := one.b.sub(one.a)
	span := direction.dot(direction)
	if span == 0 {
		return nil
	}

	type stop struct {
		at    float64
		point int
	}

	var stops []stop
	seen := make(map[int]bool)

	for i, point := range a.points {
		at := point.sub(one.a).dot(direction) / span
		at = math.Min(math.Max(at, 0), 1)

		if one.a.add(direction.scale(at)).sub(point).length() > a.tolerance {
			continue
		}
		if seen[i] {
			continue
		}

		seen[i] = true
		stops = append(stops, stop{at: at, point: i})
	}

	slices.SortFunc(stops, func(x, y stop) int {
		if c := cmpFloat(x.at, y.at); c != 0 {
			return c
		}
		return cmpFloat(float64(x.point), float64(y.point))
	})

	out := make([]int, 0, len(stops))
	for _, one := range stops {
		out = append(out, one.point)
	}

	return out
}

// connect records that one operand walks one link, in one direction.
func (a *arrangement) connect(side, from, to int) {
	if from == to {
		return
	}

	key := [2]int{min(from, to), max(from, to)}

	at, ok := a.index[key]
	if !ok {
		a.links = append(a.links, &link{a: key[0], b: key[1]})
		at = len(a.links) - 1
		a.index[key] = at
	}

	if from < to {
		a.links[at].wind[side]++
		return
	}
	a.links[at].wind[side]--
}

// winding records, for every link and both operands, the winding number of that
// operand immediately either side of the link.
//
// It is read off a ray cast from the middle of the link along the link's own
// left normal, counting every other link weighted by how many times its operand
// walks it. That ray direction is what makes this exact rather than a probe at
// some small distance either side: a ray which leaves the link perpendicularly
// meets no link lying along it, so the count it comes back with is the winding
// number on the left, and the winding number on the right is that less what the
// link itself carries. Nothing is evaluated at a point which had to be nudged
// off a boundary first, so there is no distance here small enough to be wrong
// about.
func (a *arrangement) winding() {
	for _, one := range a.links {
		if one.wind[0] == 0 && one.wind[1] == 0 {
			continue
		}

		from, to := a.points[one.a], a.points[one.b]
		direction := to.sub(from)
		if direction.length() == 0 {
			continue
		}

		ray := direction.left().scale(1 / direction.length())
		middle := turn(from.add(to).scale(0.5), ray)

		for _, other := range a.links {
			if other == one {
				continue
			}

			at := crossing(turn(a.points[other.a], ray), turn(a.points[other.b], ray), middle)
			if at == 0 {
				continue
			}

			for side := range one.left {
				one.left[side] += at * other.wind[side]
			}
		}

		for side := range one.right {
			one.right[side] = one.left[side] - one.wind[side]
		}
	}
}

// turn rotates a point into the frame in which a ray direction is the positive
// first axis, which is the frame the winding count is taken in.
func turn(point, ray vec) vec {
	return vec{ray.X*point.X + ray.Y*point.Y, -ray.Y*point.X + ray.X*point.Y}
}

// trace selects the links a rule puts on the boundary of the result and chains
// them into rings.
func (a *arrangement) trace(covered func(a, b bool) bool) []contour {
	var arcs []arc

	for _, one := range a.links {
		if one.wind[0] == 0 && one.wind[1] == 0 {
			continue
		}

		left := covered(one.left[0] != 0, one.left[1] != 0)
		right := covered(one.right[0] != 0, one.right[1] != 0)

		switch {
		case left && !right:
			arcs = append(arcs, arc{from: one.a, to: one.b})
		case right && !left:
			arcs = append(arcs, arc{from: one.b, to: one.a})
		}
	}

	return a.chain(arcs)
}

// chain walks selected links into closed rings, each with the result on its
// left.
//
// Where more than one selected link leaves a point, the one taken is the first
// met turning clockwise from the way the walk came in. That is what separates
// two pieces which touch at a corner into two rings rather than joining them
// into one ring pinched at the middle: hugging the boundary as tightly as the
// links allow keeps the result on the left the whole way round, and a shape
// which touches itself at a point is two shapes.
func (a *arrangement) chain(arcs []arc) []contour {
	slices.SortFunc(arcs, func(x, y arc) int {
		if x.from != y.from {
			return cmpFloat(float64(x.from), float64(y.from))
		}
		return cmpFloat(float64(x.to), float64(y.to))
	})

	leaving := make(map[int][]int, len(arcs))
	for i, one := range arcs {
		leaving[one.from] = append(leaving[one.from], i)
	}

	walked := make([]bool, len(arcs))

	var rings []contour
	for start := range arcs {
		if walked[start] {
			continue
		}

		var ring []int
		at := start

		for {
			walked[at] = true
			ring = append(ring, arcs[at].from)

			if arcs[at].to == arcs[start].from {
				break
			}

			next, ok := a.next(arcs, leaving, walked, at)
			if !ok {
				// Every point a selected link arrives at is one another
				// selected link leaves, because the result's boundary is closed
				// wherever it is a boundary at all. Reaching a dead end is this
				// file being wrong rather than the geometry being unusual, and
				// the ring it was part of is dropped rather than closed across
				// a gap nobody computed.
				ring = nil
				break
			}

			at = next
		}

		if len(ring) < 3 {
			continue
		}

		out := make(contour, 0, len(ring))
		for _, point := range ring {
			out = append(out, a.points[point])
		}
		rings = append(rings, out)
	}

	return a.usable(rings)
}

// next is the link to leave a point by, having arrived by another.
func (a *arrangement) next(arcs []arc, leaving map[int][]int, walked []bool, at int) (int, bool) {
	arrived := a.points[arcs[at].to].sub(a.points[arcs[at].from])
	back := math.Atan2(-arrived.Y, -arrived.X)

	best, found := 0, false
	turned := 0.0

	for _, candidate := range leaving[arcs[at].to] {
		if walked[candidate] {
			continue
		}

		out := a.points[arcs[candidate].to].sub(a.points[arcs[candidate].from])

		// Clockwise from the way the walk came in, with the way it came in
		// itself last: a link which doubles straight back is a spur, and taking
		// it before any other would walk into it and out again rather than round
		// the shape.
		angle := math.Mod(back-math.Atan2(out.Y, out.X), 2*math.Pi)
		if angle <= 0 {
			angle += 2 * math.Pi
		}

		if !found || angle < turned {
			best, turned, found = candidate, angle, true
		}
	}

	return best, found
}

// usable drops the rings which are not shapes: the ones which enclose nothing
// and the slivers narrower than the tolerance two boundaries are the same
// boundary within.
//
// A sliver is what an operation leaves where two operands almost coincide, and
// it is the plausible-looking wrong answer this whole file is written to avoid:
// a strip a thousandth of a millimetre wide reads as a real overlap to anything
// which asks only whether the result is empty.
func (a *arrangement) usable(rings []contour) []contour {
	out := make([]contour, 0, len(rings))

	for _, ring := range rings {
		if ring.signedArea() == 0 || ring.width() < a.tolerance {
			continue
		}
		out = append(out, ring.rotated())
	}

	slices.SortFunc(out, func(x, y contour) int {
		if c := compareVec(x[0], y[0]); c != 0 {
			return c
		}
		return cmpFloat(y.signedArea(), x.signedArea())
	})

	return out
}

// meeting is the one point two segments meet at, and whether they meet at a
// point at all.
//
// It is only ever asked about positions which are not already known, and there
// is exactly one kind: where two segments cross away from either's ends. A
// crossing at an end is reported too and costs nothing, because that position is
// already a candidate and both spellings of it intern to the same point.
//
// Segments which lie along each other meet at no single point and come back
// false, which is not a gap: every position either of them reaches is already a
// candidate, and [arrangement.along] splits a segment at every candidate lying
// on it. That is what makes an overlap along a shared wall need no case of its
// own here — and answering it here as well would be a second answer to a
// question already answered, with the two free to disagree the first time a
// rounding went the other way.
func meeting(one, other segment) (vec, bool) {
	first := one.b.sub(one.a)
	second := other.b.sub(other.a)

	denominator := first.cross(second)
	if denominator == 0 {
		return vec{}, false
	}

	between := other.a.sub(one.a)

	at := between.cross(second) / denominator
	on := between.cross(first) / denominator

	if at < 0 || at > 1 || on < 0 || on > 1 {
		return vec{}, false
	}

	return one.a.add(first.scale(at)), true
}

// minArcSteps and maxArcSteps bound how many segments stand in for a circle.
//
// Neither is a tolerance. How closely the polygon has to follow the circle is
// the declared tolerance's to say and [arcSteps] reads it from there; these
// two are the range that answer is allowed to land in, so that a tolerance
// as coarse as the offset itself still produces a shape and one far finer
// than any coordinate does not produce a hundred thousand segments of it.
const (
	minArcSteps = 8
	maxArcSteps = 1024
)

// arcSteps is how many segments a full circle of a radius is drawn with so that
// no point of the circle is further than the tolerance from the polygon which
// stands in for it.
//
// The furthest a chord subtending an angle falls inside the circle is the
// radius less the radius times the cosine of half that angle, so the angle a
// tolerance allows is twice the arc cosine of one less the tolerance over the
// radius. That is the whole of the resolution of an offset, and it is read from
// the declared tolerance rather than from a count written down here, because
// how closely a curve has to be followed is a decision a project makes and not
// one this package makes for it.
func arcSteps(radius, tolerance float64) int {
	if radius <= 0 || tolerance <= 0 {
		return minArcSteps
	}

	ratio := 1 - tolerance/radius
	if ratio <= -1 {
		return minArcSteps
	}

	angle := 2 * math.Acos(ratio)
	if angle <= 0 {
		return maxArcSteps
	}

	return min(max(int(math.Ceil(2*math.Pi/angle)), minArcSteps), maxArcSteps)
}

// disc is the ring of a circle of a radius about a point, drawn to the
// tolerance and anticlockwise.
func disc(centre vec, radius, tolerance float64) contour {
	steps := arcSteps(radius, tolerance)

	ring := make(contour, 0, steps)
	for i := range steps {
		angle := 2 * math.Pi * float64(i) / float64(steps)
		ring = append(ring, centre.add(vec{math.Cos(angle), math.Sin(angle)}.scale(radius)))
	}

	return ring
}

// slab is the ring of everything within a distance of a straight segment,
// anticlockwise, without the ends which [disc] covers.
func slab(from, to vec, radius float64) (contour, bool) {
	direction := to.sub(from)
	length := direction.length()
	if length == 0 {
		return nil, false
	}

	offset := direction.left().scale(radius / length)

	return contour{
		from.sub(offset), to.sub(offset), to.add(offset), from.add(offset),
	}, true
}

// nested groups rings into the pieces they bound, each an anticlockwise ring
// with the clockwise rings inside it taken out of it.
//
// A hole belongs to the smallest ring which holds it, which is what makes an
// island inside a courtyard a piece of its own rather than a hole in the plate
// the courtyard is in.
func nested(rings []contour) ([]contour, [][]contour) {
	var outers, holes []contour

	for _, ring := range rings {
		if ring.signedArea() > 0 {
			outers = append(outers, ring)
			continue
		}
		holes = append(holes, ring)
	}

	within := make([][]contour, len(outers))

	for _, hole := range holes {
		owner, found := -1, false

		for i, outer := range outers {
			if !outer.holds(hole[0]) {
				continue
			}
			if !found || math.Abs(outer.signedArea()) < math.Abs(outers[owner].signedArea()) {
				owner, found = i, true
			}
		}

		if !found {
			// A ring which holds nothing and is held by nothing bounds a hole in
			// what is outside every piece, which is not a hole in anything. It
			// is dropped rather than attached to whichever piece happened to be
			// walked first.
			continue
		}

		within[owner] = append(within[owner], hole)
	}

	return outers, within
}
