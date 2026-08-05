// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The model the geometry tests write into: a vocabulary with a position
// predicate, a coincidence tolerance and a rule which files geometry on the
// namespace of its id, and one room and one wall for a reference to reach.
const (
	geometryRegistry = `(project
  (label "Geometry fixture")
  (globalid-namespace "https://example.org/models/geometry"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))

(frame frame:building (label "Building local grid") (unit m))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings."))

(type Partition
  (kind Element)
  (geometry surface)
  (description "A wall between two spaces."))

(predicate position
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "The location of a vertex in its frame."))

(predicate area
  (unit m2)
  (shape scalar)
  (description "How much floor a space has."))

(tolerance coincident
  (value 0.005 m)
  (description "How far apart two corners may be and still be one point."))

(tolerance fabrication-fit
  (value 5.0 mm)
  (description "How far a fabricated part may be from where it was drawn."))

(route rooms (kind Space) (type MeetingRoom) (file "entities/site.dfc"))

(route partitions (kind Element) (type Partition) (file "entities/site.dfc"))

(route geometry
  (namespace geom)
  (file "entities/geometry.dfc")
  (description "Vertices, edges and loops, which declare neither a kind nor a type."))
`

	geometrySite = `(node site:S-101
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building))

(node site:W-01
  (label "Partition between A and the corridor")
  (kind Element)
  (type Partition)
  (geometry surface)
  (frame frame:building))
`
)

// geometryFixture writes a model holding the geometry given and returns its
// root. The geometry may be empty, which is a model with none: an empty entity
// file is legal and contributes nothing.
func geometryFixture(t *testing.T, geometry string) string {
	t.Helper()

	return tree(t, map[string]string{
		"registry.dfc":          geometryRegistry,
		"entities/site.dfc":     geometrySite,
		"entities/geometry.dfc": geometry,
	})
}

// read is one file of the model beneath root, as it stands.
func read(t *testing.T, root, path string) string {
	t.Helper()

	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)

	return string(src)
}

// corner is one corner of a scaffolded loop, in metres.
func corner(x, y, z float64) Corner {
	return Corner{Position: CoordinateValue([]float64{x, y, z}, "m")}
}

// surveyed is the evidence every position claim these tests write carries.
func surveyed(t *testing.T) ClaimSpec {
	t.Helper()

	term, err := ParseAccuracyTerm("independent 0.004 m")
	require.NoError(t, err)

	return ClaimSpec{
		Source:   "Interior control set IC-01, Acme Surveys",
		Method:   "method:total-station",
		Accuracy: []AccuracyTerm{term},
		Date:     time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC),
	}
}

// room is a scaffold of the four corners given, closed by naming the first
// again.
func room(t *testing.T, corners ...Corner) ScaffoldSpec {
	t.Helper()

	return ScaffoldSpec{
		Namespace:  "geom",
		Frame:      "frame:building",
		Corners:    append(corners, corners[0]),
		Predicate:  "position",
		Provenance: surveyed(t),
		Tolerance:  "coincident",
		Snap:       true,
	}
}

// square is the four corners of a rectangle with its north-west corner at the
// origin, walked clockwise.
func square(width, depth float64) []Corner {
	return []Corner{corner(0, 0, 0), corner(width, 0, 0), corner(width, depth, 0), corner(0, depth, 0)}
}

// scaffolded applies one scaffold to the fixture and returns what it created
// together with the model it left behind.
func scaffolded(t *testing.T, root string, spec ScaffoldSpec) (Scaffolding, *Graph) {
	t.Helper()

	tx := begin(t, root)

	built, _, err := tx.Scaffold(spec, "")
	require.NoError(t, err)

	out, diags, err := tx.Commit()
	require.NoError(t, err)
	require.Empty(t, diags)
	require.NotEmpty(t, out.Written())

	graph, found := LoadGraph(root)
	require.Empty(t, found)

	return built, graph
}

// declined applies one scaffold to the fixture and returns the error it was
// refused with, requiring nothing to have been written.
func declined(t *testing.T, root string, spec ScaffoldSpec) error {
	t.Helper()

	before := contents(t, root)

	tx := begin(t, root)
	_, _, err := tx.Scaffold(spec, "")
	require.Error(t, err)
	require.NoError(t, tx.Close())

	assert.Equal(t, before, contents(t, root), "a refused scaffold writes nothing")

	return err
}

// positions is where every vertex of the model is, read under the predicate the
// fixture declares for one.
func positions(t *testing.T, graph *Graph) Positions {
	t.Helper()

	out := Positions{}
	for vertex := range graph.Topology().Vertices() {
		resolution, err := graph.Claims().Resolve(vertex.ID(), "position", graph.Registry())
		require.NoError(t, err)

		if value, ok := resolution.Value(); ok {
			out[vertex.ID()] = value
		}
	}

	return out
}

func TestTxAddVertex(t *testing.T) {
	testCases := []struct {
		name     string
		spec     VertexSpec
		expected string
	}{
		{
			name: "writes a corner with where it is and how that is known",
			spec: VertexSpec{
				ID:    "geom:V-01",
				Label: "Room A, north-west corner",
				Frame: "frame:building",
				Position: ClaimSpec{
					Predicate: "position",
					Value:     CoordinateValue([]float64{0, 0, 0}, "m"),
					Source:    "Interior control set IC-01, Acme Surveys",
					Method:    "method:total-station",
					Date:      time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC),
				},
			},
			expected: `(vertex
  geom:V-01
  (label "Room A, north-west corner")
  (frame frame:building)
  (position
    (value (0.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (date "2026-02-18")))`,
		},
		{
			name:     "writes a corner nobody has yet surveyed, whose position is unknown rather than zero",
			spec:     VertexSpec{ID: "geom:V-02", Frame: "frame:building"},
			expected: `(vertex geom:V-02 (frame frame:building))`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := geometryFixture(t, "")

			graph := authored(t, root, func(tx *Tx) error {
				return tx.AddVertex(testCase.spec, "entities/geometry.dfc")
			})

			vertex, ok := graph.Topology().Vertex(testCase.spec.ID)
			require.True(t, ok, "the model holds %s", testCase.spec.ID)
			assert.Equal(t, testCase.spec.Label, vertex.Label())
			assert.Equal(t, testCase.spec.Frame, vertex.Frame())

			assert.Contains(t, read(t, root, "entities/geometry.dfc"), testCase.expected)
		})
	}
}

func TestTxAddEdge(t *testing.T) {
	const corners = `(vertex geom:V-01 (frame frame:building))
(vertex geom:V-02 (frame frame:building))
`

	testCases := []struct {
		name     string
		spec     EdgeSpec
		expected string
	}{
		{
			name: "writes a connection between two corners which already exist",
			spec: EdgeSpec{
				ID:    "geom:E-01",
				Label: "Room A, north wall",
				Frame: "frame:building",
				Start: "geom:V-01",
				End:   "geom:V-02",
			},
			expected: `(edge
  geom:E-01
  (label "Room A, north wall")
  (frame frame:building)
  (vertices geom:V-01 geom:V-02))`,
		},
		{
			name: "writes what physically realises it, which is what makes it a physical boundary",
			spec: EdgeSpec{
				ID:       "geom:E-02",
				Frame:    "frame:building",
				Start:    "geom:V-01",
				End:      "geom:V-02",
				BackedBy: []ID{"site:W-01"},
			},
			expected: `(edge
  geom:E-02
  (frame frame:building)
  (vertices geom:V-01 geom:V-02)
  (backed-by site:W-01))`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := geometryFixture(t, corners)

			graph := authored(t, root, func(tx *Tx) error {
				return tx.AddEdge(testCase.spec, "entities/geometry.dfc")
			})

			edge, ok := graph.Topology().Edge(testCase.spec.ID)
			require.True(t, ok, "the model holds %s", testCase.spec.ID)

			start, end := edge.Vertices()
			assert.Equal(t, testCase.spec.Start, start)
			assert.Equal(t, testCase.spec.End, end)
			assert.Equal(t, testCase.spec.BackedBy, edge.BackedBy())

			assert.Contains(t, read(t, root, "entities/geometry.dfc"), testCase.expected)
		})
	}
}

// TestTxAddEdgeIsTheSharedEdgeCase is its own function because it is about the
// operation rather than about the axes: a second region reusing the edge the
// first already has is one node named twice, which is what makes a partition one
// wall rather than two which can drift apart.
func TestTxAddEdgeIsTheSharedEdgeCase(t *testing.T) {
	root := geometryFixture(t, "")

	first, _ := scaffolded(t, root, room(t, square(4, 3)...))

	// The corridor east of the room shares the room's east wall, so its corners
	// land on the room's and the edge between them is the one already written.
	second, graph := scaffolded(t, root, room(t,
		corner(4, 0, 0), corner(8, 0, 0), corner(8, 3, 0), corner(4, 3, 0),
	))

	require.Len(t, second.Reused, 1, "the shared wall is reused and nothing else is")
	assert.Contains(t, first.Edges, second.Reused[0])
	assert.Len(t, second.Created, 2, "only the two corners away from the shared wall are new")

	// One edge, named by both loops, is the whole of the property: the two
	// regions cannot drift apart because there is nothing to drift.
	shared, ok := graph.Topology().Edge(second.Reused[0])
	require.True(t, ok)

	var naming []ID
	for _, id := range []ID{first.Loop, second.Loop} {
		loop, ok := graph.Topology().Loop(id)
		require.True(t, ok)

		if assert.Contains(t, loop.Edges(), shared.ID()) {
			naming = append(naming, id)
		}
	}
	assert.Equal(t, []ID{first.Loop, second.Loop}, naming)
}

func TestTxAddLoop(t *testing.T) {
	const ring = `(vertex geom:V-01 (frame frame:building))
(vertex geom:V-02 (frame frame:building))
(vertex geom:V-03 (frame frame:building))
(edge geom:E-01 (frame frame:building) (vertices geom:V-01 geom:V-02))
(edge geom:E-02 (frame frame:building) (vertices geom:V-02 geom:V-03))
(edge geom:E-03 (frame frame:building) (vertices geom:V-03 geom:V-01))
`

	root := geometryFixture(t, ring)

	graph := authored(t, root, func(tx *Tx) error {
		return tx.AddLoop(LoopSpec{
			ID:    "geom:L-01",
			Label: "Room A boundary",
			Frame: "frame:building",
			Edges: []ID{"geom:E-01", "geom:E-02", "geom:E-03"},
		}, "entities/geometry.dfc")
	})

	loop, ok := graph.Topology().Loop("geom:L-01")
	require.True(t, ok)

	// The order is the data: it is the order the ring is walked, and it is kept
	// exactly as it was written rather than sorted.
	assert.Equal(t, []ID{"geom:E-01", "geom:E-02", "geom:E-03"}, loop.Edges())
}

func TestGeometryRefusesWhatTheRegistryDoesNotPermit(t *testing.T) {
	const corners = `(vertex geom:V-01 (frame frame:building))
(vertex geom:V-02 (frame frame:building))
(edge geom:E-01 (frame frame:building) (vertices geom:V-01 geom:V-02))
`

	testCases := []struct {
		name   string
		change func(tx *Tx) error
		expect func(t *testing.T, err error)
	}{
		{
			name: "refuses a vertex whose id namespace nobody declared",
			change: func(tx *Tx) error {
				return tx.AddVertex(VertexSpec{ID: "shape:V-09", Frame: "frame:building"}, "entities/geometry.dfc")
			},
			expect: func(t *testing.T, err error) {
				var unknown UnknownAxisError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, "shape", unknown.Value)
				assert.Contains(t, unknown.Permitted, "geom")
			},
		},
		{
			name: "refuses a geometric node written in no frame at all",
			change: func(tx *Tx) error {
				return tx.AddVertex(VertexSpec{ID: "geom:V-09"}, "entities/geometry.dfc")
			},
			expect: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrNoFrame)
			},
		},
		{
			name: "refuses a frame the registry does not declare",
			change: func(tx *Tx) error {
				return tx.AddVertex(VertexSpec{ID: "geom:V-09", Frame: "frame:mars"}, "entities/geometry.dfc")
			},
			expect: func(t *testing.T, err error) {
				var unknown UnknownAxisError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, "frame", unknown.Axis)
				assert.Equal(t, "frame:mars", unknown.Value)
			},
		},
		{
			name: "refuses an id something in the model already holds",
			change: func(tx *Tx) error {
				return tx.AddVertex(VertexSpec{ID: "geom:V-01", Frame: "frame:building"}, "entities/geometry.dfc")
			},
			expect: func(t *testing.T, err error) {
				var taken TakenIDError
				require.ErrorAs(t, err, &taken)
				assert.Equal(t, ID("geom:V-01"), taken.ID)
			},
		},
		{
			name: "refuses an id this same change already wrote",
			change: func(tx *Tx) error {
				spec := VertexSpec{ID: "geom:V-09", Frame: "frame:building"}
				if err := tx.AddVertex(spec, "entities/geometry.dfc"); err != nil {
					return err
				}
				return tx.AddVertex(spec, "entities/geometry.dfc")
			},
			expect: func(t *testing.T, err error) {
				var taken TakenIDError
				require.ErrorAs(t, err, &taken)
				assert.Equal(t, ID("geom:V-09"), taken.ID)
			},
		},
		{
			name: "refuses a position claimed under a predicate nothing declares",
			change: func(tx *Tx) error {
				return tx.AddVertex(VertexSpec{
					ID:    "geom:V-09",
					Frame: "frame:building",
					Position: ClaimSpec{
						Predicate: "where",
						Value:     CoordinateValue([]float64{0, 0, 0}, "m"),
						Source:    "A drawing",
						Method:    "method:total-station",
					},
				}, "entities/geometry.dfc")
			},
			expect: func(t *testing.T, err error) {
				var unknown UnknownAxisError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, "where", unknown.Value)
			},
		},
		{
			name: "refuses an edge which runs from a vertex to itself",
			change: func(tx *Tx) error {
				return tx.AddEdge(EdgeSpec{
					ID:    "geom:E-09",
					Frame: "frame:building",
					Start: "geom:V-01",
					End:   "geom:V-01",
				}, "entities/geometry.dfc")
			},
			expect: func(t *testing.T, err error) {
				var loop SelfLoopError
				require.ErrorAs(t, err, &loop)
				assert.Equal(t, ID("geom:V-01"), loop.Vertex)
			},
		},
		{
			name: "refuses an edge with only one end written",
			change: func(tx *Tx) error {
				return tx.AddEdge(EdgeSpec{
					ID:    "geom:E-09",
					Frame: "frame:building",
					Start: "geom:V-01",
				}, "entities/geometry.dfc")
			},
			expect: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrNoEndpoints)
			},
		},
		{
			name: "refuses an endpoint which names nothing in the model",
			change: func(tx *Tx) error {
				return tx.AddEdge(EdgeSpec{
					ID:    "geom:E-09",
					Frame: "frame:building",
					Start: "geom:V-01",
					End:   "geom:V-99",
				}, "entities/geometry.dfc")
			},
			expect: func(t *testing.T, err error) {
				var unknown UnknownEntityError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, ID("geom:V-99"), unknown.ID)
			},
		},
		{
			name: "refuses an endpoint which names something that is not a vertex",
			change: func(tx *Tx) error {
				return tx.AddEdge(EdgeSpec{
					ID:    "geom:E-09",
					Frame: "frame:building",
					Start: "geom:V-01",
					End:   "geom:E-01",
				}, "entities/geometry.dfc")
			},
			expect: func(t *testing.T, err error) {
				var family NotOfFamilyError
				require.ErrorAs(t, err, &family)
				assert.Equal(t, ID("geom:E-01"), family.ID)
				assert.Equal(t, "vertex", family.Want)
				assert.Equal(t, "edge", family.Got)
			},
		},
		{
			name: "refuses a loop with no edges to traverse",
			change: func(tx *Tx) error {
				return tx.AddLoop(LoopSpec{ID: "geom:L-09", Frame: "frame:building"}, "entities/geometry.dfc")
			},
			expect: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ErrNoEdges)
			},
		},
		{
			name: "refuses a loop naming something that is not an edge",
			change: func(tx *Tx) error {
				return tx.AddLoop(LoopSpec{
					ID:    "geom:L-09",
					Frame: "frame:building",
					Edges: []ID{"geom:E-01", "geom:V-02"},
				}, "entities/geometry.dfc")
			},
			expect: func(t *testing.T, err error) {
				var family NotOfFamilyError
				require.ErrorAs(t, err, &family)
				assert.Equal(t, ID("geom:V-02"), family.ID)
				assert.Equal(t, "edge", family.Want)
				assert.Equal(t, "vertex", family.Got)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := geometryFixture(t, corners)

			testCase.expect(t, rejected(t, root, testCase.change))
		})
	}
}

// TestTxScaffoldWritesAFreshRoom is the ordinary case: a coordinate list and
// nothing else in the model, which has to come back as a closed ring of shared
// vertices and edges.
func TestTxScaffoldWritesAFreshRoom(t *testing.T) {
	root := geometryFixture(t, "")

	built, graph := scaffolded(t, root, room(t, square(4, 3)...))

	assert.Len(t, built.Vertices, 4, "the closing corner is the first corner again, not a fifth")
	assert.Equal(t, built.Vertices, built.Created)
	assert.Len(t, built.Edges, 4)
	assert.Empty(t, built.Reused)
	assert.Empty(t, built.Snaps)
	assert.Equal(t, "coincident", built.Tolerance.Name)

	// The ids are minted from the namespace and the tag of the form, which is a
	// name and not a schema.
	assert.Equal(t, ID("geom:vertex-1"), built.Vertices[0])
	assert.Equal(t, ID("geom:edge-1"), built.Edges[0])
	assert.Equal(t, ID("geom:loop-1"), built.Loop)

	loop, ok := graph.Topology().Loop(built.Loop)
	require.True(t, ok)
	assert.Equal(t, built.Edges, loop.Edges())

	// The ring is what was asked for rather than a list of nodes which happen to
	// exist: it closes, against the tolerance the scaffold was given.
	assembly, diags := graph.Topology().Assemble(loop, positions(t, graph), "coincident", graph.Registry())
	assert.Empty(t, diags)
	assert.True(t, assembly.Closed(), "a scaffolded loop closes")

	// Every corner carries the provenance it was given, under the predicate it
	// was claimed under, like any other claim.
	claim, err := graph.Claims().Resolve(built.Vertices[1], "position", graph.Registry())
	require.NoError(t, err)

	value, ok := claim.Value()
	require.True(t, ok)

	components, ok := value.Coordinate()
	require.True(t, ok)
	assert.Equal(t, []float64{4, 0, 0}, components)
	assert.Equal(t, Unit("m"), value.Unit())
}

// TestTxScaffoldReusesAVertexACornerLandsOn is its own function because it is
// the behaviour the whole operation exists for, and because the assertions are
// about a reuse rather than about a room.
func TestTxScaffoldReusesAVertexACornerLandsOn(t *testing.T) {
	root := geometryFixture(t, "")

	first, _ := scaffolded(t, root, room(t, square(4, 3)...))

	// The second room's north-west corner is two millimetres from the first
	// room's north-east corner, which the tolerance calls one point.
	built, graph := scaffolded(t, root, room(t,
		corner(4.002, 0, 0), corner(8, 0, 0), corner(8, 3, 0), corner(4.002, 3, 0),
	))

	require.Len(t, built.Snaps, 2, "both corners of the shared wall land on a vertex which is there")

	for index, snap := range built.Snaps {
		assert.Equal(t, first.Vertices[index+1], snap.Vertex)
		assert.InDelta(t, 0.002, snap.Distance, 1e-9, "the distance snapped is reported, not inferred")
		assert.Equal(t, Unit("m"), snap.Unit)
		assert.True(t, snap.Reused)
	}
	assert.Equal(t, []int{1, 4}, []int{built.Snaps[0].Corner, built.Snaps[1].Corner})

	assert.Equal(t, first.Vertices[1], built.Vertices[0], "the corner is the vertex which was there")
	assert.Equal(t, first.Vertices[2], built.Vertices[3])
	assert.Len(t, built.Created, 2, "only the two corners away from the shared wall are new")

	// A duplicate vertex two millimetres from an existing one is exactly what
	// did not happen: six corners describe both rooms, not eight.
	assert.Len(t, positions(t, graph), 6)
}

// TestTxScaffoldWritesANewVertexOutsideTheTolerance is its own function because
// it is the other half of the same measurement: the near miss has to come back
// as a corner of its own rather than as a reuse, or the tolerance means nothing.
func TestTxScaffoldWritesANewVertexOutsideTheTolerance(t *testing.T) {
	root := geometryFixture(t, "")

	first, _ := scaffolded(t, root, room(t, square(4, 3)...))

	// Six millimetres, against a tolerance of five.
	built, _ := scaffolded(t, root, room(t,
		corner(4.006, 0, 0), corner(8, 0, 0), corner(8, 3, 0), corner(4.006, 3, 0),
	))

	assert.Empty(t, built.Snaps, "a corner outside the tolerance lands on nothing")
	assert.Empty(t, built.Reused)
	assert.Len(t, built.Created, 4)
	assert.NotContains(t, built.Vertices, first.Vertices[1])
}

// TestTxScaffoldReportsACoincidenceItWasToldNotToSnap is its own function
// because the assertion is that a run which wrote a duplicate said so: the
// duplicate is the sliver the topology model exists to prevent, and an author
// who asked for it is entitled to have it and entitled to be told.
func TestTxScaffoldReportsACoincidenceItWasToldNotToSnap(t *testing.T) {
	root := geometryFixture(t, "")

	first, _ := scaffolded(t, root, room(t, square(4, 3)...))

	spec := room(t, corner(4.002, 0, 0), corner(8, 0, 0), corner(8, 3, 0), corner(4.002, 3, 0))
	spec.Snap = false

	built, _ := scaffolded(t, root, spec)

	require.Len(t, built.Snaps, 2)

	for index, snap := range built.Snaps {
		assert.Equal(t, first.Vertices[index+1], snap.Vertex)
		assert.InDelta(t, 0.002, snap.Distance, 1e-9)
		assert.False(t, snap.Reused, "the coincidence is reported and the duplicate is written")
	}

	assert.Len(t, built.Created, 4)
	assert.NotContains(t, built.Vertices, first.Vertices[1])
	assert.Empty(t, built.Reused, "with no shared corners there is no shared edge either")
}

func TestTxScaffoldRefusesAListItCannotMakeARingOf(t *testing.T) {
	testCases := []struct {
		name   string
		spec   func(t *testing.T) ScaffoldSpec
		expect func(t *testing.T, err error)
	}{
		{
			name: "refuses a coordinate list which does not return to where it started",
			spec: func(t *testing.T) ScaffoldSpec {
				spec := room(t, square(4, 3)...)
				spec.Corners = spec.Corners[:len(spec.Corners)-1]
				spec.Corners = append(spec.Corners, corner(0, 0.4, 0))
				return spec
			},
			expect: func(t *testing.T, err error) {
				var unclosed UnclosedLoopError
				require.ErrorAs(t, err, &unclosed)
				assert.Equal(t, 5, unclosed.Last)
				assert.InDelta(t, 0.4, unclosed.Gap, 1e-9)
				assert.Equal(t, Unit("m"), unclosed.Unit)
				assert.Equal(t, "coincident", unclosed.Tolerance.Name)
			},
		},
		{
			name: "refuses a list too short to describe a ring at all",
			spec: func(t *testing.T) ScaffoldSpec {
				spec := room(t, corner(0, 0, 0), corner(4, 0, 0))
				return spec
			},
			expect: func(t *testing.T, err error) {
				var few TooFewCornersError
				require.ErrorAs(t, err, &few)
				assert.Equal(t, 3, few.Corners)
			},
		},
		{
			name: "refuses a list which visits one of its corners twice",
			spec: func(t *testing.T) ScaffoldSpec {
				return room(t, corner(0, 0, 0), corner(4, 0, 0), corner(0, 0, 0), corner(0, 3, 0))
			},
			expect: func(t *testing.T, err error) {
				var collapsed CollapsedRingError
				require.ErrorAs(t, err, &collapsed)
				assert.Equal(t, 1, collapsed.First)
				assert.Equal(t, 3, collapsed.Second)
				assert.Equal(t, "coincident", collapsed.Tolerance.Name)
			},
		},
		{
			name: "refuses a tolerance the registry does not declare",
			spec: func(t *testing.T) ScaffoldSpec {
				spec := room(t, square(4, 3)...)
				spec.Tolerance = "close-enough"
				return spec
			},
			expect: func(t *testing.T, err error) {
				var unknown UnknownAxisError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, "close-enough", unknown.Value)
				assert.Contains(t, unknown.Permitted, "coincident")
			},
		},
		{
			name: "refuses a tolerance declared in another unit than the frame's",
			spec: func(t *testing.T) ScaffoldSpec {
				spec := room(t, square(4, 3)...)
				spec.Tolerance = "fabrication-fit"
				return spec
			},
			expect: func(t *testing.T, err error) {
				var mismatched ToleranceUnitError
				require.ErrorAs(t, err, &mismatched)
				assert.Equal(t, Unit("m"), mismatched.Want)
				assert.Equal(t, Unit("mm"), mismatched.Tolerance.Unit)
				assert.Equal(t, ID("frame:building"), mismatched.Frame)
			},
		},
		{
			name: "refuses a namespace nobody declared to mint into",
			spec: func(t *testing.T) ScaffoldSpec {
				spec := room(t, square(4, 3)...)
				spec.Namespace = "shape"
				return spec
			},
			expect: func(t *testing.T, err error) {
				var unknown UnknownAxisError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, "shape", unknown.Value)
			},
		},
		{
			name: "refuses a position claim with no evidence behind it",
			spec: func(t *testing.T) ScaffoldSpec {
				spec := room(t, square(4, 3)...)
				spec.Provenance.Source = ""
				return spec
			},
			expect: func(t *testing.T, err error) {
				var missing MissingChildError
				require.ErrorAs(t, err, &missing)
				assert.Equal(t, "source", missing.Child)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := geometryFixture(t, "")

			testCase.expect(t, declined(t, root, testCase.spec(t)))
		})
	}
}

// TestTxScaffoldReportsWhatADryRunWouldWrite is its own function because it
// asserts about a change which did not happen: the ids, the reuses and the
// tolerance are the whole of what an author checks before committing to them,
// so they have to be the same whether or not anything reached the disk.
func TestTxScaffoldReportsWhatADryRunWouldWrite(t *testing.T) {
	root := geometryFixture(t, "")
	before := contents(t, root)

	tx := begin(t, root)
	tx.DryRun = true

	built, notices, err := tx.Scaffold(room(t, square(4, 3)...), "")
	require.NoError(t, err)

	out, diags, err := tx.Commit()
	require.NoError(t, err)
	require.Empty(t, diags)

	assert.True(t, out.DryRun)
	assert.Equal(t, before, contents(t, root), "a dry run writes nothing")
	assert.Empty(t, notices, "the corners carry an accuracy, so nothing is unrankable")

	assert.Len(t, built.Created, 4)
	assert.Len(t, built.Edges, 4)
	assert.Equal(t, ID("geom:loop-1"), built.Loop)
	assert.NotEmpty(t, out.Changed(), "it says which files it would have written")
}

// TestTxScaffoldSaysWhenAPositionCannotWinResolution is its own function because
// a notice is neither a refusal nor a diagnostic: a claim with no accuracy is
// legitimate, and what it is not is something to discover later.
func TestTxScaffoldSaysWhenAPositionCannotWinResolution(t *testing.T) {
	root := geometryFixture(t, "")

	spec := room(t, square(4, 3)...)
	spec.Provenance.Accuracy = nil

	tx := begin(t, root)

	built, notices, err := tx.Scaffold(spec, "")
	require.NoError(t, err)

	require.Len(t, notices, len(built.Created))
	for index, notice := range notices {
		assert.Equal(t, NoticeUnrankable, notice.Kind)
		assert.Equal(t, built.Created[index], notice.Subject)
		assert.Equal(t, "position", notice.Predicate)
	}
}

// TestScaffoldedGeometryIsAuthoredTheSameWayAsHandWrittenGeometry checks the
// property which says the scaffold is a shortcut rather than a second format:
// what it writes is read back by the same loader, resolves under the same
// predicate, and satisfies the canonical form every other write leaves behind.
func TestScaffoldedGeometryIsAuthoredTheSameWayAsHandWrittenGeometry(t *testing.T) {
	root := geometryFixture(t, "")

	built, graph := scaffolded(t, root, room(t, square(4, 3)...))

	src := read(t, root, "entities/geometry.dfc")

	file, err := Parse("entities/geometry.dfc", strings.NewReader(src))
	require.NoError(t, err)
	require.Empty(t, Validate(file))

	var printed strings.Builder
	require.NoError(t, Print(&printed, file))
	assert.Equal(t, src, printed.String(), "a write leaves the file in canonical form")

	for _, id := range built.Created {
		vertex, ok := graph.Topology().Vertex(id)
		require.True(t, ok)
		assert.Equal(t, ID("frame:building"), vertex.Frame())
	}

	for _, id := range built.Edges {
		edge, ok := graph.Topology().Edge(id)
		require.True(t, ok)

		start, end := edge.Vertices()
		assert.NotEqual(t, start, end)
		assert.Contains(t, built.Vertices, start)
		assert.Contains(t, built.Vertices, end)
	}
}

// TestMintIDTakesTheLowestOrdinalNothingHolds checks the one property of the
// minting convention which matters: an id, once written, is what it is forever,
// so minting again yields the next ordinal rather than the one already taken.
func TestMintIDTakesTheLowestOrdinalNothingHolds(t *testing.T) {
	root := geometryFixture(t, `(vertex geom:vertex-1 (frame frame:building))
(vertex geom:vertex-3 (frame frame:building))
`)

	tx := begin(t, root)

	first, err := tx.MintID("geom", "vertex")
	require.NoError(t, err)
	assert.Equal(t, ID("geom:vertex-2"), first)

	require.NoError(t, tx.AddVertex(VertexSpec{ID: first, Frame: "frame:building"}, "entities/geometry.dfc"))

	second, err := tx.MintID("geom", "vertex")
	require.NoError(t, err)
	assert.Equal(t, ID("geom:vertex-4"), second, "an id this change wrote is not free either")
}

// TestGeometryIsRoutedOnTheNamespaceOfItsID checks that the rules which file
// semantic nodes do not file geometry as a side effect, and that a geometric
// node reaches the rule written for it.
func TestGeometryIsRoutedOnTheNamespaceOfItsID(t *testing.T) {
	registry, diags := LoadRegistry(geometryFixture(t, ""))
	require.Empty(t, diags)

	testCases := []struct {
		name string
		spec interface {
			Destination(*Registry, string) (Destination, error)
		}
		expected string
	}{
		{
			name:     "files a vertex by the rule written for the namespace",
			spec:     VertexSpec{ID: "geom:V-01", Frame: "frame:building"},
			expected: "entities/geometry.dfc",
		},
		{
			name:     "files an edge the same way",
			spec:     EdgeSpec{ID: "geom:E-01", Frame: "frame:building", Start: "geom:V-01", End: "geom:V-02"},
			expected: "entities/geometry.dfc",
		},
		{
			name:     "files a loop the same way",
			spec:     LoopSpec{ID: "geom:L-01", Frame: "frame:building", Edges: []ID{"geom:E-01"}},
			expected: "entities/geometry.dfc",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			destination, err := testCase.spec.Destination(registry, "")
			require.NoError(t, err)
			assert.Equal(t, testCase.expected, destination.Path)
			assert.Equal(t, "geometry", destination.Rule)
		})
	}

	// A rule which matches on a kind or a type never places one, because a
	// geometric node declares neither.
	_, err := VertexSpec{ID: "site:V-01", Frame: "frame:building"}.Destination(registry, "")

	var routing RoutingError
	require.ErrorAs(t, err, &routing)
	assert.False(t, routing.Ambiguous())
	assert.Empty(t, routing.Matched)
}

// TestGeometryTakesAnOverrideOverTheRules checks the one way a command names
// somewhere other than where the rules would file a node.
func TestGeometryTakesAnOverrideOverTheRules(t *testing.T) {
	registry, diags := LoadRegistry(geometryFixture(t, ""))
	require.Empty(t, diags)

	destination, err := VertexSpec{ID: "geom:V-01", Frame: "frame:building"}.
		Destination(registry, "entities/annexe.dfc")
	require.NoError(t, err)

	assert.Equal(t, "entities/annexe.dfc", destination.Path)
	assert.True(t, destination.Overridden)
	assert.Empty(t, destination.Rule)

	_, err = VertexSpec{ID: "geom:V-01", Frame: "frame:building"}.Destination(registry, "notes.md")
	assert.True(t, errors.Is(err, ErrNotAnEntityFile))
}
