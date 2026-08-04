// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// topologyFixture is the root of one fixture model: a registry and the
// geometric nodes judged against it.
func topologyFixture(name string) string { return filepath.Join("testdata", "topology", name) }

// loadTopologyFixture loads a fixture model and renders the diagnostics of the
// geometric pass the way the command line interface would.
//
// The registry's own diagnostics are asserted empty rather than rendered, for
// the reason the semantic loader's helper asserts the same: every fixture here
// declares a registry which loads clean, so what the golden beside it holds is
// what this layer had to say and nothing else.
func loadTopologyFixture(t *testing.T, name string) (*Topology, string) {
	t.Helper()

	registry, registryDiags := LoadRegistry(topologyFixture(name))
	require.Empty(t, registryDiags, "the fixture registry loads clean")

	topology, diags := LoadTopology(topologyFixture(name), registry)

	var collected Diagnostics
	collected.Add(diags...)

	var rendered strings.Builder
	require.NoError(t, collected.Render(&rendered, FileSources{}))

	return topology, rendered.String()
}

// expectedTopologyDiagnostics returns the rendering held beside the fixture,
// having first rewritten it from got when -update was passed.
func expectedTopologyDiagnostics(t *testing.T, name string, got string) string {
	t.Helper()

	path := filepath.Join(topologyFixture(name), "diagnostics.txt")
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

func TestLoadTopology(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
	}{
		{
			name:    "names a reference which reaches nothing, and one which reaches the wrong sort of node",
			fixture: "dangling-reference",
		},
		{
			name:    "names both ends of an edge which starts and ends at one vertex",
			fixture: "degenerate-edge",
		},
		{
			name:    "names both definitions of an id the model already holds, in whichever files they are",
			fixture: "duplicate-id",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, got := loadTopologyFixture(t, testCase.fixture)

			assert.Equal(t, expectedTopologyDiagnostics(t, testCase.fixture, got), got)
		})
	}
}

// TestLoadTopologyReadsTheGeometricFamily checks that all three members load
// with what they carry: an id, a label, a frame and the references they write.
//
// The three are asserted together rather than one per function because the
// point is the family. A vertex which loads while an edge does not is a model
// with corners and no walls, and the tag is what selects the shape.
func TestLoadTopologyReadsTheGeometricFamily(t *testing.T) {
	topology, rendered := loadTopologyFixture(t, "valid")
	require.Empty(t, rendered, "the valid fixture loads clean")

	require.Equal(t, 9, topology.Len())

	t.Run("reads a vertex, which is a corner and not a coordinate", func(t *testing.T) {
		vertex, ok := topology.Vertex("geom:V-01")

		require.True(t, ok)
		assert.Equal(t, ID("geom:V-01"), vertex.ID())
		assert.Equal(t, "Room B, north-west corner", vertex.Label())
		assert.Equal(t, ID("frame:building"), vertex.Frame())
		assert.Equal(t, filepath.Join(topologyFixture("valid"), "geometry.dfc"), vertex.Span().Start.Path)
	})

	t.Run("reads an edge as an ordered pair of vertices", func(t *testing.T) {
		edge, ok := topology.Edge("geom:E-01")

		require.True(t, ok)
		assert.Equal(t, ID("geom:E-01"), edge.ID())
		assert.Equal(t, "North wall", edge.Label())
		assert.Equal(t, ID("frame:building"), edge.Frame())

		// Start then end, in the order they were written. Both name vertices
		// this model holds, which is what makes the pair an edge rather than
		// two names.
		start, end := edge.Vertices()
		assert.Equal(t, ID("geom:V-01"), start)
		assert.Equal(t, ID("geom:V-02"), end)

		for _, id := range []ID{start, end} {
			_, ok := topology.Vertex(id)
			assert.True(t, ok, "%s names a vertex", id)
		}
	})

	t.Run("reads a loop as an ordered list of edges", func(t *testing.T) {
		loop, ok := topology.Loop("geom:L-01")

		require.True(t, ok)
		assert.Equal(t, ID("geom:L-01"), loop.ID())
		assert.Equal(t, "Meeting Room B boundary", loop.Label())
		assert.Equal(t, ID("frame:building"), loop.Frame())
		assert.Len(t, loop.Edges(), 4)

		for _, id := range loop.Edges() {
			_, ok := topology.Edge(id)
			assert.True(t, ok, "%s names an edge", id)
		}
	})

	t.Run("carries neither a kind nor a type, on any of the three", func(t *testing.T) {
		// The absence is structural: there is no axis to ask about, which is
		// what the two families being different shapes means. What there is to
		// ask is whether the semantic index holds any of them, and it holds
		// none — a geometric node is not a node with two fields left out.
		nodes, diags := LoadNodes(topologyFixture("valid"), nil)

		assert.Zero(t, nodes.Len())
		assert.Empty(t, diags)
	})
}

// TestLoadTopologyKeepsTheOrderOfALoop checks that the edges of a loop come
// back in the order they were written.
//
// The order is the data: it is the order the loop is traversed, and a loader
// which sorted it — or which held each edge once — would answer a different
// question from the one the file asked. The fixture writes a ring which begins
// at its third edge, so an implementation which sorted would pass a test
// written against a ring which happens to begin at its first.
func TestLoadTopologyKeepsTheOrderOfALoop(t *testing.T) {
	topology, rendered := loadTopologyFixture(t, "valid")
	require.Empty(t, rendered)

	loop, ok := topology.Loop("geom:L-01")
	require.True(t, ok)

	assert.Equal(t, []ID{"geom:E-03", "geom:E-04", "geom:E-01", "geom:E-02"}, loop.Edges())

	// And the slice handed back is the caller's, so re-ordering it re-orders
	// nothing in the model.
	read := loop.Edges()
	read[0], read[3] = read[3], read[0]

	assert.Equal(t, []ID{"geom:E-03", "geom:E-04", "geom:E-01", "geom:E-02"}, loop.Edges())
}

// TestLoadTopologyVertexPositionIsAClaim checks the arrangement decision 0001
// exists for: a corner's position is a claim like any other, so one vertex
// holds two surveys of itself with the evidence for both.
//
// A coordinate field would have room for one of the two and none for where it
// came from. Because the position is a claim, the second survey is read,
// ranked, and reported as a disagreement by the same code which does that for
// the width of a room.
func TestLoadTopologyVertexPositionIsAClaim(t *testing.T) {
	registry, diags := LoadRegistry(topologyFixture("valid"))
	require.Empty(t, diags)

	topology, diags := LoadTopology(topologyFixture("valid"), registry)
	require.Empty(t, diags)

	claims, diags := LoadClaims(topologyFixture("valid"), registry)
	require.Empty(t, diags, "a vertex's claims are validated by the pass which validates every other claim")

	vertex, ok := topology.Vertex("geom:V-01")
	require.True(t, ok)

	t.Run("holds both surveys of the corner, each with its own evidence", func(t *testing.T) {
		var read []*Claim
		for claim := range claims.Under(vertex.ID(), "position") {
			read = append(read, claim)
		}

		require.Len(t, read, 2)
		for _, claim := range read {
			assert.NotEmpty(t, claim.Source())
			assert.NotEmpty(t, claim.Method())
			assert.True(t, claim.Rankable())
		}
	})

	t.Run("prefers the more accurate of the two, by the ordinary rule", func(t *testing.T) {
		resolution, err := claims.Resolve(vertex.ID(), "position", registry)

		require.NoError(t, err)
		require.True(t, resolution.Resolved())

		value, ok := resolution.Value()
		require.True(t, ok)

		components, ok := value.Coordinate()
		require.True(t, ok)
		assert.Equal(t, []float64{0.004, 0.0, 0.0}, components)
	})

	t.Run("reports the disagreement between them in the conflict register", func(t *testing.T) {
		var subjects []ID
		for conflict := range claims.Conflicts() {
			subjects = append(subjects, conflict.Subject())
			assert.Equal(t, "position", conflict.Predicate())
			assert.Len(t, conflict.Claims(), 2)
		}

		assert.Equal(t, []ID{vertex.ID()}, subjects)
	})
}

// TestLoadTopologyAdmitsANonStraightEdge checks the extension point specification
// section 6.3 names: the shape of an edge between its two vertices is a claim
// under a registered predicate, so an arc arrives as registry data.
//
// The proof is which side of the boundary the change is on. The same edge loads
// identically against two registries, and the only difference between them is a
// predicate declaration in the consuming repository's data — no form, no closed
// set and no Go type here knows that `arc-centre` means curvature.
func TestLoadTopologyAdmitsANonStraightEdge(t *testing.T) {
	registry, diags := LoadRegistry(topologyFixture("valid"))
	require.Empty(t, diags)

	topology, diags := LoadTopology(topologyFixture("valid"), registry)
	require.Empty(t, diags)

	edge, ok := topology.Edge("geom:E-02")
	require.True(t, ok)

	t.Run("reads the arc exactly as it reads a straight edge", func(t *testing.T) {
		start, end := edge.Vertices()

		assert.Equal(t, ID("geom:V-02"), start)
		assert.Equal(t, ID("geom:V-03"), end)
	})

	t.Run("carries the curvature as a claim, with its own accuracy", func(t *testing.T) {
		claims, diags := LoadClaims(topologyFixture("valid"), registry)
		require.Empty(t, diags)

		resolution, err := claims.Resolve(edge.ID(), "arc-centre", registry)
		require.NoError(t, err)
		require.True(t, resolution.Resolved())

		value, ok := resolution.Value()
		require.True(t, ok)

		centre, ok := value.Coordinate()
		require.True(t, ok)
		assert.Equal(t, []float64{2.0, 2.0, 0.0}, centre)
		assert.Equal(t, Unit("m"), value.Unit())
	})

	t.Run("is registry data and not engine vocabulary", func(t *testing.T) {
		// The same geometry, judged against a registry which declares
		// everything except the curvature predicate. The edge still loads; the
		// claim on it is reported as naming a predicate nothing declares, which
		// is what says the declaration is the whole of the extension.
		root := t.TempDir()
		copyFixture(t, filepath.Join(topologyFixture("valid"), "geometry.dfc"), filepath.Join(root, "geometry"+Extension))
		copyFixture(t, filepath.Join(topologyFixture("valid"), "registry.dfc"), filepath.Join(root, "registry"+Extension))
		strip(t, filepath.Join(root, "registry"+Extension), "(predicate arc-centre")

		declared, diags := LoadRegistry(root)
		require.Empty(t, diags)

		_, ok := declared.Predicate("arc-centre")
		require.False(t, ok)

		read, diags := LoadTopology(root, declared)
		require.Empty(t, diags, "the edge is read the same either way")

		_, ok = read.Edge("geom:E-02")
		assert.True(t, ok)

		_, diags = LoadClaims(root, declared)
		require.Len(t, diags, 1, "and the one thing which changed is what the registry declares")
	})
}

// copyFixture copies one fixture file into a temporary directory.
func copyFixture(t *testing.T, from, to string) {
	t.Helper()

	src, err := os.ReadFile(from)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(to, src, 0o644))
}

// strip removes the form beginning with the given prefix from a written file,
// which is what turns a registry which declares a predicate into one which does
// not.
func strip(t *testing.T, path, prefix string) {
	t.Helper()

	src, err := os.ReadFile(path)
	require.NoError(t, err)

	forms := strings.Split(string(src), "\n\n")
	kept := make([]string, 0, len(forms))
	for _, form := range forms {
		if strings.HasPrefix(strings.TrimSpace(form), prefix) {
			continue
		}
		kept = append(kept, form)
	}
	require.Len(t, kept, len(forms)-1, "the form to strip is written as one paragraph")

	require.NoError(t, os.WriteFile(path, []byte(strings.Join(kept, "\n\n")), 0o644))
}

// TestLoadTopologyRejectsAKindOrAType checks that neither axis of the semantic
// family is quietly ignored on a geometric node.
//
// A vertex with a type is a file whose author believes the two families are one
// family, and reading it as a vertex with a child nobody looked at would leave
// them believing it. The diagnostic names the node, because a file holding a
// hundred vertices needs to say which one.
func TestLoadTopologyRejectsAKindOrAType(t *testing.T) {
	testCases := []struct {
		name    string
		written string
		id      ID
	}{
		{
			name:    "reports a kind written on a vertex",
			written: "(vertex geom:V-01 (frame frame:building) (kind Space))",
			id:      "geom:V-01",
		},
		{
			name:    "reports a type written on an edge",
			written: "(edge geom:E-01 (frame frame:building) (vertices geom:V-01 geom:V-02) (type Partition))",
			id:      "geom:E-01",
		},
		{
			name:    "reports a type written on a loop",
			written: "(loop geom:L-01 (frame frame:building) (edges geom:E-01) (type SiteBoundary))",
			id:      "geom:L-01",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "geometry"+Extension)
			require.NoError(t, os.WriteFile(path, []byte(testCase.written+"\n"), 0o644))

			topology, diags := LoadTopology(path, nil)

			require.Len(t, diags, 1)
			assert.Equal(t, SeverityError, diags[0].Severity)
			assert.Contains(t, diags[0].Message, string(testCase.id), "the diagnostic names the node")

			// And the node is not read. Interpreting it would mean deciding what
			// a vertex with a kind is, and there is no such thing to decide.
			assert.Zero(t, topology.Len())
		})
	}
}

// TestLoadTopologyReturnsWhatItCouldRead checks that a geometric node with a
// diagnostic against it is still a geometric node.
//
// A caller reporting on a tree wants to say "geom:E-01 starts and ends at
// geom:V-01", and one which had been handed only the diagnostic could say only
// that something was wrong somewhere.
func TestLoadTopologyReturnsWhatItCouldRead(t *testing.T) {
	topology, rendered := loadTopologyFixture(t, "degenerate-edge")

	require.NotEmpty(t, rendered)

	edge, ok := topology.Edge("geom:E-01")

	require.True(t, ok)
	assert.Equal(t, "The wall which is a point", edge.Label())

	start, end := edge.Vertices()
	assert.Equal(t, ID("geom:V-01"), start)
	assert.Equal(t, ID("geom:V-01"), end)
}

// TestLoadTopologyIgnoresEverythingElse checks that this pass reads the
// geometric family and nothing else.
//
// The semantic family carries a kind and a type, and registry forms are
// resolved before any of this is interpreted. A pass which read either here
// would be reporting a node as a vertex whose endpoints are missing.
func TestLoadTopologyIgnoresEverythingElse(t *testing.T) {
	registry, _ := LoadRegistry(nodeFixture("valid"))

	topology, diags := LoadTopology(nodeFixture("valid"), registry)

	assert.Zero(t, topology.Len())
	assert.Empty(t, diags)
}

// TestLoadTopologyWithoutARegistry checks the load a consuming repository whose
// registry has not been written yet gets: every geometric node mints its id in
// a namespace nothing declares, and each says so with a position.
func TestLoadTopologyWithoutARegistry(t *testing.T) {
	topology, diags := LoadTopology(topologyFixture("valid"), nil)

	require.Equal(t, 9, topology.Len())
	require.Len(t, diags, 9, "one undeclared namespace per geometric node, and nothing else")

	for _, diagnostic := range diags {
		assert.Equal(t, SeverityError, diagnostic.Severity)
		assert.Contains(t, diagnostic.Message, "which no registry file declares")
		assert.Equal(t, "no namespace is declared; a registry file declares one with (namespace ...)", diagnostic.Hint)
		assert.NotEmpty(t, diagnostic.Span.Start.Path)
	}
}

// TestLoadTopologyReportsStructureBeforeReadingIt checks that a geometric form
// which is structurally wrong is reported and not interpreted.
//
// A vertex is always in exactly one frame, so one which wrote none has no frame
// to invent. Reading it would mean guessing, and the guess would then be
// reported as a frame nothing declares as though somebody had written it.
func TestLoadTopologyReportsStructureBeforeReadingIt(t *testing.T) {
	source := `(vertex geom:V-01 (label "A corner in no coordinate system"))` + "\n"

	dir := t.TempDir()
	path := filepath.Join(dir, "geometry"+Extension)
	require.NoError(t, os.WriteFile(path, []byte(source), 0o644))

	topology, diags := LoadTopology(path, nil)

	assert.Zero(t, topology.Len())
	require.Len(t, diags, 1)
	assert.Equal(t, "expected a (frame ...) child of the vertex form, found none", diags[0].Message)
}

func TestLoadTopologyUnreadableRoot(t *testing.T) {
	topology, diags := LoadTopology(filepath.Join("testdata", "topology", "no-such-directory"), nil)

	assert.Zero(t, topology.Len())
	assert.NotEmpty(t, diags)
}

// TestTopologyZeroValue checks that every method answers on a load which read
// nothing, which is what a source tree with no geometry in it yields.
func TestTopologyZeroValue(t *testing.T) {
	var topology *Topology

	assert.Zero(t, topology.Len())

	for range topology.Vertices() {
		t.Error("a nil topology holds no vertices")
	}
	for range topology.Edges() {
		t.Error("a nil topology holds no edges")
	}
	for range topology.Loops() {
		t.Error("a nil topology holds no loops")
	}

	_, ok := topology.Vertex("geom:V-01")
	assert.False(t, ok)

	_, ok = topology.Edge("geom:E-01")
	assert.False(t, ok)

	_, ok = topology.Loop("geom:L-01")
	assert.False(t, ok)
}
