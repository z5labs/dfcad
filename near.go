// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
)

// NotCoordinateError reports a predicate asked to say where a vertex is which
// does not declare a coordinate.
//
// A position is a point in a frame, so the predicate it is claimed under has to
// take a coordinate. A scalar or a transform predicate names something a vertex
// may well carry, but it is not where the vertex is, and measuring the distance
// from a point to a setback would be an answer to a question nobody can ask.
type NotCoordinateError struct {
	// Predicate is the predicate which was named.
	Predicate string

	// Shape is the shape it does declare.
	Shape Shape
}

// Error implements the [error] interface.
func (e NotCoordinateError) Error() string {
	return fmt.Sprintf(
		"expected a predicate which declares a coordinate, found %s, which declares %s",
		e.Predicate, spellShape(e.Shape),
	)
}

// NearSpec is a point to find the vertices around.
//
// It is the question a scaffold asks of every corner — which vertex does this
// land on — asked on its own and without writing anything. Nothing in it has a
// default, for the reason nothing in a [ScaffoldSpec] has one: the frame, the
// predicate a position is claimed under and how close is close enough are all
// the consuming repository's to say.
type NearSpec struct {
	// Frame is the frame the point is expressed in. Only the vertices written in
	// it are considered, and the point, the positions and the tolerance are all
	// in its one unit.
	Frame ID

	// Predicate is the predicate a vertex's position is claimed under, which has
	// to declare a coordinate.
	Predicate string

	// Tolerance is the name of the declared tolerance a vertex has to be within
	// of the point, which has to be declared in the frame's unit.
	Tolerance string

	// Point is where to look, in the shape the predicate declares and the unit of
	// the frame. [ParseCorner] reads one from a command line.
	Point Value
}

// Nearby is one vertex within the tolerance of a point.
type Nearby struct {
	// Vertex is the vertex.
	Vertex ID

	// At is where its position resolves to, component by component, in the
	// frame's unit.
	At []float64

	// Distance is how far it is from the point, in the frame's unit.
	Distance float64
}

// Nearness is what [Graph.VerticesNear] found.
type Nearness struct {
	// Point is the point asked about, component by component, in the frame's
	// unit.
	Point []float64

	// Vertices are the vertices within the tolerance of the point, in the order
	// the model's walk reached them. Empty rather than nil when there are none.
	Vertices []Nearby

	// Tolerance is the declaration the vertices were judged against, which
	// travels with the answer because the answer depends on it
	// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
	Tolerance Tolerance

	// Unit is the frame's unit, which every coordinate and distance above is in.
	Unit Unit
}

// VerticesNear lists the vertices within a tolerance of a point, and writes
// nothing.
//
// It is exactly the rule [Tx.Scaffold] applies to decide which vertex a corner
// lands on, and it is one function rather than two copies of it: a vertex is a
// candidate when its position resolves under the predicate, in the frame's unit,
// and it is near when that position lies within the tolerance's value of the
// point. The vertex a scaffold snaps a corner to is always the nearest of the
// ones this lists at that corner, so a lookup made before a scaffold says what
// the scaffold will do.
//
// A vertex whose position does not resolve is not listed, because nobody can
// say where it is. Nothing within the tolerance is an empty list rather than an
// error: a point where nothing stands is an ordinary answer.
func (g *Graph) VerticesNear(spec NearSpec) (Nearness, error) {
	registry := g.Registry()

	if spec.Frame == "" {
		return Nearness{}, ErrNoFrame
	}

	frame, ok := registry.Frame(spec.Frame)
	if !ok {
		return Nearness{}, UnknownAxisError{
			Axis:      "frame",
			Value:     string(spec.Frame),
			Permitted: registry.Names(SortFrame),
		}
	}

	tolerance, ok := registry.Tolerance(spec.Tolerance)
	if !ok {
		return Nearness{}, UnknownAxisError{
			Axis:      string(SortTolerance),
			Value:     spec.Tolerance,
			Permitted: registry.Names(SortTolerance),
		}
	}

	if tolerance.Unit != frame.Unit {
		return Nearness{}, ToleranceUnitError{Tolerance: tolerance, Frame: spec.Frame, Want: frame.Unit}
	}

	declared, ok := registry.Predicate(spec.Predicate)
	if !ok {
		return Nearness{}, UnknownAxisError{
			Axis:      string(SortPredicate),
			Value:     spec.Predicate,
			Permitted: registry.Names(SortPredicate),
		}
	}

	if declared.Shape != ShapeCoordinate {
		return Nearness{}, NotCoordinateError{Predicate: spec.Predicate, Shape: declared.Shape}
	}

	components, ok := spec.Point.Coordinate()
	if !ok {
		return Nearness{}, ValueShapeError{Predicate: spec.Predicate, Want: ShapeCoordinate, Got: spec.Point.Shape()}
	}

	if spec.Point.Unit() != frame.Unit {
		return Nearness{}, UnitError{Predicate: spec.Predicate, Want: frame.Unit, Got: spec.Point.Unit()}
	}

	if declared.Dimension > 0 && len(components) != declared.Dimension {
		return Nearness{}, DimensionError{Predicate: spec.Predicate, Want: declared.Dimension, Got: len(components)}
	}

	index := indexVertices(g, spec.Frame, spec.Predicate, frame.Unit)

	return Nearness{
		Point:     components,
		Vertices:  index.within(components, tolerance.Value),
		Tolerance: tolerance,
		Unit:      frame.Unit,
	}, nil
}

// vertexIndex is where the vertices of one frame are, read by the rule a point
// lands on a vertex by.
//
// It is the one place that rule lives. [Tx.Scaffold] reads a corner against one
// and [Graph.VerticesNear] reads a point against one, so that a lookup and a
// scaffold cannot disagree about which vertex a corner lands on.
type vertexIndex struct {
	// at is where each vertex is.
	at map[ID][]float64

	// order is the ids of at, in the order they were recorded, so that two runs
	// over one model pick the same vertex where two are equally near.
	order []ID
}

// indexVertices reads where every vertex of the model written in a frame is.
//
// A vertex whose position does not resolve is not in it. That is a state and
// not a failure — nothing was claimed about it, or the claims tie and the tie is
// unbroken — and a point cannot be said to land on a vertex nobody can say the
// whereabouts of. One whose only position states no accuracy is in it: it is
// unranked rather than unknown, and is read wherever the model is, by the rule
// [currentClaim] states. One whose position is in a unit other than the frame's
// is not, because nothing converts between units
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)).
func indexVertices(graph *Graph, frame ID, predicate string, unit Unit) *vertexIndex {
	registry := graph.Registry()
	index := &vertexIndex{at: map[ID][]float64{}}

	for vertex := range graph.Topology().Vertices() {
		if vertex.Frame() != frame {
			continue
		}

		resolution, err := graph.Claims().Resolve(vertex.ID(), predicate, registry)
		if err != nil {
			continue
		}

		claim, ok := currentClaim(resolution)
		if !ok {
			continue
		}

		value := claim.Value()
		if value.Unit() != unit {
			continue
		}

		components, ok := value.Coordinate()
		if !ok {
			continue
		}

		index.record(vertex.ID(), components)
	}

	return index
}

// record adds a vertex to the index, or moves one it already holds.
func (v *vertexIndex) record(id ID, components []float64) {
	if _, held := v.at[id]; !held {
		v.order = append(v.order, id)
	}
	v.at[id] = components
}

// within is every vertex no further than tolerance from a point, in the order
// the index recorded them.
func (v *vertexIndex) within(components []float64, tolerance float64) []Nearby {
	out := make([]Nearby, 0)

	for _, id := range v.order {
		at := v.at[id]

		if gap, ok := withinTolerance(at, components, tolerance); ok {
			out = append(out, Nearby{Vertex: id, At: at, Distance: gap})
		}
	}

	return out
}

// nearest is the vertex a point lands on: the nearest of the ones within the
// tolerance, and whether there is one at all.
//
// It is one pass rather than [vertexIndex.within] followed by a second over what
// that found, because a scaffold asks it once per corner; the two agree because
// both admit a vertex by [withinTolerance]. Ties are broken by the order the index recorded
// them. It is an order rather than an arbitrary choice so that two runs over one
// model land on the same vertex.
func (v *vertexIndex) nearest(components []float64, tolerance float64) (ID, float64, bool) {
	var (
		found   ID
		nearest float64
	)

	for _, id := range v.order {
		gap, ok := withinTolerance(v.at[id], components, tolerance)
		if !ok {
			continue
		}

		if found == "" || gap < nearest {
			found, nearest = id, gap
		}
	}

	return found, nearest, found != ""
}

// withinTolerance is how far a vertex at one coordinate is from a point, and whether that
// is within the tolerance: the one test a vertex is admitted by, whether a
// lookup is listing or a scaffold is snapping.
//
// A vertex whose position has a different number of components from the point
// is never within it: nothing is padded, and a question with no answer is not
// given a plausible wrong one.
func withinTolerance(at, point []float64, tolerance float64) (float64, bool) {
	gap, ok := distanceBetween(at, point)
	if !ok || gap > tolerance {
		return 0, false
	}
	return gap, true
}
