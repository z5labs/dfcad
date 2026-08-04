// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"iter"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// graphFixture is the root of one fixture model: everything a load reads, under
// one directory.
func graphFixture(name string) string { return filepath.Join("testdata", "graph", name) }

// renderGraphDiagnostics renders diagnostics the way the command line interface
// would, which is what the golden beside a fixture holds.
func renderGraphDiagnostics(t *testing.T, diags []Diagnostic) string {
	t.Helper()

	var collected Diagnostics
	collected.Add(diags...)

	var rendered strings.Builder
	require.NoError(t, collected.Render(&rendered, FileSources{}))

	return rendered.String()
}

// expectedGraphDiagnostics returns the rendering held beside the fixture,
// having first rewritten it from got when -update was passed.
func expectedGraphDiagnostics(t *testing.T, name string, got string) string {
	t.Helper()

	path := filepath.Join(graphFixture(name), "diagnostics.txt")
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// loadGraphFixture loads one fixture model and returns it with its diagnostics
// rendered.
func loadGraphFixture(t *testing.T, name string) (*Graph, string) {
	t.Helper()

	graph, diags := LoadGraph(graphFixture(name))
	require.NotNil(t, graph, "a load always yields a usable graph")

	return graph, renderGraphDiagnostics(t, diags)
}

func TestLoadGraph(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
	}{
		{
			name:    "names an id one family holds which another family holds too",
			fixture: "duplicate-id",
		},
		{
			name:    "names both ends of every reference which reaches nothing",
			fixture: "unresolved",
		},
		{
			name:    "names every member of a ring of references, in each relation which can hold one",
			fixture: "cyclic",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, got := loadGraphFixture(t, testCase.fixture)

			assert.Equal(t, expectedGraphDiagnostics(t, testCase.fixture, got), got)
		})
	}
}

// TestLoadGraphReadsAWholeModelInOnePass is its own function because what it
// asserts is that the six pieces are there and agree, which is a different
// shape of assertion from a golden rendering.
//
// The fixture is written so that no file could be read on its own: the geometry
// is walked before the semantic nodes which reference its loops, and both are
// walked before the registry which declares the type, the predicate and the
// frame they are judged against. A load which resolved as it read, or which
// interpreted an entity before the registry was complete, reports diagnostics
// here rather than nothing.
func TestLoadGraphReadsAWholeModelInOnePass(t *testing.T) {
	graph, diags := loadGraphFixture(t, "valid")
	require.Empty(t, diags, "the valid fixture loads clean")

	assert.Equal(t, graphFixture("valid"), graph.Root())
	assert.True(t, graph.Registry().Declares(SortType, "MeetingRoom"))
	assert.Equal(t, 7, graph.Nodes().Len())
	assert.Equal(t, 15, graph.Topology().Len())
	assert.Equal(t, 10, graph.Claims().Len())

	root, ok := graph.Frames().Root()
	require.True(t, ok, "the frame chain reaches a root")
	assert.Equal(t, ID("frame:survey-grid"), root.ID)

	room, ok := graph.Node("site:S-101")
	require.True(t, ok)
	assert.Equal(t, []ID{"geom:L-01"}, loopIDs(graph.Loops(room)))
}

// loopIDs is the ids of a sequence of loops, which is what a traversal is
// asserted on: comparing pointers says the same thing less legibly.
func loopIDs(loops iter.Seq[*Loop]) []ID {
	var out []ID
	for loop := range loops {
		out = append(out, loop.ID())
	}
	return out
}

// nodeIDs is the ids of a sequence of semantic nodes, for the reason above.
func nodeIDs(nodes iter.Seq[*SemanticNode]) []ID {
	var out []ID
	for node := range nodes {
		out = append(out, node.ID())
	}
	return out
}

// relatedIDs is the ids of a sequence of related nodes, for the reason above.
func relatedIDs(related iter.Seq[Related]) []ID {
	var out []ID
	for node := range related {
		out = append(out, node.Node().ID())
	}
	return out
}

func TestGraphLookup(t *testing.T) {
	graph, diags := loadGraphFixture(t, "valid")
	require.Empty(t, diags, "the valid fixture loads clean")

	testCases := []struct {
		name string
		id   ID
		what string
	}{
		{name: "finds a semantic node by its id", id: "site:S-101", what: "*dfcad.SemanticNode"},
		{name: "finds a vertex by its id", id: "geom:V-01", what: "*dfcad.Vertex"},
		{name: "finds an edge by its id", id: "geom:E-01", what: "*dfcad.Edge"},
		{name: "finds a loop by its id", id: "geom:L-01", what: "*dfcad.Loop"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			entity, ok := graph.Entity(testCase.id)

			require.True(t, ok)
			assert.Equal(t, testCase.id, entity.ID())
			assert.Equal(t, testCase.what, typeName(entity))
		})
	}
}

// typeName is the Go type of an entity, which is what tells a lookup which
// family answered.
func typeName(entity Entity) string {
	switch entity.(type) {
	case *SemanticNode:
		return "*dfcad.SemanticNode"
	case *Vertex:
		return "*dfcad.Vertex"
	case *Edge:
		return "*dfcad.Edge"
	case *Loop:
		return "*dfcad.Loop"
	}
	return ""
}

// TestGraphLookupFindsNothingUnderAnIdNothingHolds is its own function because
// the assertion is the negative one, and threading it through the table above
// would mean a nil entity in every row which does hold something.
func TestGraphLookupFindsNothingUnderAnIdNothingHolds(t *testing.T) {
	graph, diags := loadGraphFixture(t, "valid")
	require.Empty(t, diags, "the valid fixture loads clean")

	for _, id := range []ID{"site:S-999", "", "survey:W-0001"} {
		entity, ok := graph.Entity(id)

		assert.False(t, ok, "%q names no entity", id)
		assert.Nil(t, entity)
	}
}

func TestGraphNearest(t *testing.T) {
	graph, diags := loadGraphFixture(t, "valid")
	require.Empty(t, diags, "the valid fixture loads clean")

	testCases := []struct {
		name     string
		id       ID
		expected ID
	}{
		{
			name:     "suggests the node an id was misspelled from",
			id:       "site:S-1O1",
			expected: "site:S-101",
		},
		{
			name:     "suggests across the families rather than only the semantic one",
			id:       "geom:V-O1",
			expected: "geom:V-01",
		},
		{
			name:     "reads two characters the wrong way round as the one mistake it is",
			id:       "geom:L-10",
			expected: "geom:L-01",
		},
		{
			name:     "suggests the id itself, which is what an exact match is nearest to",
			id:       "site:S-101",
			expected: "site:S-101",
		},
		{
			name:     "suggests nothing for an id nothing in the model resembles",
			id:       "other:nothing-like-it",
			expected: "",
		},
		{
			name:     "suggests nothing for a claim id, which is not an entity",
			id:       "survey:W-000",
			expected: "",
		},
		{
			name:     "suggests nothing for the zero id, which names nothing",
			id:       "",
			expected: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			nearest, ok := graph.Nearest(testCase.id)

			assert.Equal(t, testCase.expected, nearest)
			assert.Equal(t, testCase.expected != "", ok)
		})
	}
}

// TestGraphNearestIsAPropertyOfTheModel is its own function because it asserts
// about two loads rather than one: a suggestion which came out of the order the
// walk read the files in would change when a node moved between them, while the
// model went on holding the same ids.
func TestGraphNearestIsAPropertyOfTheModel(t *testing.T) {
	graph, diags := loadGraphFixture(t, "valid")
	require.Empty(t, diags, "the valid fixture loads clean")

	// Every id which is one edit from an id the model holds, asked for twice.
	for id := range graph.ids() {
		first, ok := graph.Nearest(ID(id))
		require.True(t, ok, id)

		second, _ := graph.Nearest(ID(id))
		assert.Equal(t, first, second)
	}
}

// TestGraphNearestOnAnEmptyModel is its own function because a model holding
// nothing has nothing to suggest, and a lookup which invented something would
// be worse than one which said so.
func TestGraphNearestOnAnEmptyModel(t *testing.T) {
	// The empty fixture has a diagnostic of its own — a model declares one
	// project and this one declares none — and it is not what this is about: a
	// graph is usable whatever its diagnostics say.
	graph, _ := loadGraphFixture(t, "empty")

	nearest, ok := graph.Nearest("site:S-101")

	assert.False(t, ok)
	assert.Empty(t, nearest)
}

func TestGraphIteration(t *testing.T) {
	graph, diags := loadGraphFixture(t, "valid")
	require.Empty(t, diags, "the valid fixture loads clean")

	testCases := []struct {
		name     string
		iterate  func() []ID
		expected []ID
	}{
		{
			name:     "iterates the nodes of one kind in the order the walk read them",
			iterate:  func() []ID { return nodeIDs(graph.OfKind(KindSpace)) },
			expected: []ID{"site:S-101", "site:S-102"},
		},
		{
			name:     "iterates the nodes of one type in the order the walk read them",
			iterate:  func() []ID { return nodeIDs(graph.OfType("MeetingRoom")) },
			expected: []ID{"site:S-101"},
		},
		{
			name:     "yields nothing for a kind nothing was written under",
			iterate:  func() []ID { return nodeIDs(graph.OfKind(KindInterface)) },
			expected: nil,
		},
		{
			name:     "yields nothing for a type nothing declares",
			iterate:  func() []ID { return nodeIDs(graph.OfType("Nonesuch")) },
			expected: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, testCase.iterate())
		})
	}
}

func TestGraphTraversals(t *testing.T) {
	graph, diags := loadGraphFixture(t, "valid")
	require.Empty(t, diags, "the valid fixture loads clean")

	room, ok := graph.Node("site:S-101")
	require.True(t, ok)

	partition, ok := graph.Node("site:E-01")
	require.True(t, ok)

	shared, ok := graph.Topology().Edge("geom:E-02")
	require.True(t, ok)

	testCases := []struct {
		name     string
		traverse func() []ID
		expected []ID
	}{
		{
			name:     "reaches the containment chain above a node",
			traverse: func() []ID { return relatedIDs(graph.Ancestors(room)) },
			expected: []ID{"site:L-01", "site:B-01", "site:S-01"},
		},
		{
			name:     "reaches everything contained beneath a node",
			traverse: func() []ID { return relatedIDs(graph.Descendants(room)) },
			expected: []ID{"site:E-01"},
		},
		{
			name:     "reaches the zones a node declared membership of",
			traverse: func() []ID { return relatedIDs(graph.Zones(room)) },
			expected: []ID{"site:Z-01"},
		},
		{
			name:     "reaches the members of a zone",
			traverse: func() []ID { return relatedIDs(graph.Members(mustNode(t, graph, "site:Z-01"))) },
			expected: []ID{"site:S-101", "site:S-102"},
		},
		{
			name:     "reaches both regions of a shared edge",
			traverse: func() []ID { return nodeIDs(graph.Regions(shared)) },
			expected: []ID{"site:S-101", "site:S-102"},
		},
		{
			name:     "reaches the element backing an edge of a boundary",
			traverse: func() []ID { return backingIDs(graph.Classified(shared)) },
			expected: []ID{partition.ID()},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, testCase.traverse())
		})
	}
}

// mustNode is one node of a fixture, failing the test where the model does not
// hold it.
func mustNode(t *testing.T, graph *Graph, id ID) *SemanticNode {
	t.Helper()

	node, ok := graph.Node(id)
	require.True(t, ok, "the fixture holds %s", id)

	return node
}

// backingIDs is the ids of the elements which physically realise one edge.
func backingIDs(edge BoundaryEdge) []ID {
	var out []ID
	for _, node := range edge.Backing() {
		out = append(out, node.ID())
	}
	return out
}

func TestGraphSummary(t *testing.T) {
	testCases := []struct {
		name     string
		fixture  string
		expected string
	}{
		{
			name:     "counts both families, the claims and the pairs which disagree",
			fixture:  "valid",
			expected: "7 nodes, 6 vertices, 7 edges, 2 loops, 10 claims, 1 conflicts, 0 unresolved",
		},
		{
			name:     "counts every reference which reaches nothing",
			fixture:  "unresolved",
			expected: "1 nodes, 1 vertices, 1 edges, 1 loops, 2 claims, 0 conflicts, 5 unresolved",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			graph, _ := loadGraphFixture(t, testCase.fixture)

			assert.Equal(t, testCase.expected, graph.Summary().String())
		})
	}
}

// TestGraphSummaryBreaksNodesOutByKindAndByType is its own function because the
// two breakdowns are maps read through four methods rather than one line of
// totals.
func TestGraphSummaryBreaksNodesOutByKindAndByType(t *testing.T) {
	graph, diags := loadGraphFixture(t, "valid")
	require.Empty(t, diags, "the valid fixture loads clean")

	summary := graph.Summary()

	assert.Equal(
		t,
		[]Kind{KindZone, KindSite, KindBuilding, KindStorey, KindSpace, KindElement},
		summary.Kinds(),
		"the kinds present, in the order the closed set lists them",
	)
	assert.Equal(t, 2, summary.OfKind(KindSpace))
	assert.Equal(t, 0, summary.OfKind(KindInterface))

	assert.Equal(
		t,
		[]string{"Campus", "Corridor", "Level", "MeetingRoom", "OfficeBuilding", "Partition", "SiteBoundary"},
		summary.Types(),
		"the types present, in lexical order",
	)
	assert.Equal(t, 1, summary.OfType("MeetingRoom"))
	assert.Equal(t, 0, summary.OfType("Nonesuch"))
}

// TestLoadGraphOfATreeWithNoEntityFiles is its own function because the model it
// loads is nothing at all, so every assertion below is that something empty came
// back rather than that nothing did.
//
// It is the case a repository is in before anybody has written a file, and a
// command reporting on one has to report an empty model rather than crash on a
// nil graph.
//
// The load is not silent: a model declares one project, and a tree which
// declares none says so wherever it is empty. What makes this a success rather
// than a failure is that the graph came back usable and every question asked of
// it answers.
func TestLoadGraphOfATreeWithNoEntityFiles(t *testing.T) {
	graph, got := loadGraphFixture(t, "empty")

	assert.Equal(t, expectedGraphDiagnostics(t, "empty", got), got)

	assert.Equal(t, 0, graph.Nodes().Len())
	assert.Equal(t, 0, graph.Topology().Len())
	assert.Equal(t, 0, graph.Claims().Len())
	assert.Equal(t, "0 nodes, 0 vertices, 0 edges, 0 loops, 0 claims, 0 conflicts, 0 unresolved", graph.Summary().String())
	assert.Empty(t, graph.Summary().Kinds())
	assert.Empty(t, graph.Summary().Types())

	_, ok := graph.Entity("site:S-101")
	assert.False(t, ok)
}

// TestLoadGraphIsDeterministic checks that nothing about a load depends on the
// order the files happened to reach the disk.
//
// It writes one model several times, shuffling which file is created first, and
// asserts that every load produced the same graph in the same order. The walk
// is sorted, so this holds; a walk which took the directory in the order the
// filesystem returned it would pass on some filesystems and fail on others,
// which is the failure this asserts against — an output which differs run to
// run makes every diff meaningless and every golden file a flake.
func TestLoadGraphIsDeterministic(t *testing.T) {
	files := fixtureContents(t, graphFixture("valid"))

	var first string
	for run := range 8 {
		root := t.TempDir()

		order := slices.Sorted(maps.Keys(files))
		rand.New(rand.NewPCG(uint64(run), 1)).Shuffle(len(order), func(i, j int) {
			order[i], order[j] = order[j], order[i]
		})

		for _, name := range order {
			path := filepath.Join(root, name)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			require.NoError(t, os.WriteFile(path, files[name], 0o644))
		}

		graph, diags := LoadGraph(root)
		got := describeGraph(t, graph, diags)

		if run == 0 {
			first = got
			continue
		}
		assert.Equal(t, first, got, "the load is the same whatever order the files were written in")
	}
}

// fixtureContents reads every entity file of a fixture, keyed by its path
// relative to the fixture root.
func fixtureContents(t *testing.T, root string) map[string][]byte {
	t.Helper()

	files := make(map[string][]byte)
	for path, err := range Walk(root) {
		require.NoError(t, err)

		relative, err := filepath.Rel(root, path)
		require.NoError(t, err)

		src, err := os.ReadFile(path)
		require.NoError(t, err)

		files[relative] = src
	}
	require.NotEmpty(t, files, "%s holds entity files", root)

	return files
}

// describeGraph renders everything about a load which two runs have to agree
// on: what was read, in what order, and what was said about it.
//
// The paths of the model are left out, because each run writes it to a
// different temporary directory and every position in every diagnostic carries
// one. What is compared is the shape of the result rather than where it was
// read from.
func describeGraph(t *testing.T, graph *Graph, diags []Diagnostic) string {
	t.Helper()

	var out strings.Builder
	out.WriteString(graph.Summary().String())
	out.WriteString("\n")

	for node := range graph.Nodes().All() {
		out.WriteString("node " + string(node.ID()) + "\n")
	}
	for vertex := range graph.Topology().Vertices() {
		out.WriteString("vertex " + string(vertex.ID()) + "\n")
	}
	for edge := range graph.Topology().Edges() {
		out.WriteString("edge " + string(edge.ID()) + "\n")
	}
	for loop := range graph.Topology().Loops() {
		out.WriteString("loop " + string(loop.ID()) + "\n")
	}
	for claim := range graph.Claims().All() {
		id, _ := claim.ID()
		out.WriteString("claim " + string(id) + " " + claim.Predicate() + "\n")
	}
	for conflict := range graph.Claims().Conflicts() {
		out.WriteString("conflict " + string(conflict.Subject()) + " " + conflict.Predicate() + "\n")
	}

	var collected Diagnostics
	collected.Add(diags...)
	for _, diagnostic := range collected.All() {
		out.WriteString("diagnostic " + filepath.Base(diagnostic.Span.Start.Path) + " " + diagnostic.Message + "\n")
	}

	return out.String()
}

// BenchmarkLoadGraph measures what a load costs, in time and in what the graph
// goes on holding once it is done.
//
// Both are reported because they answer different questions. The time is what a
// command pays before it can answer anything; the resident size is what an
// agent loading a model into a long-running process pays for as long as it
// holds it, and a change which halves the time by indexing everything twice
// shows up in the second number and nowhere else.
//
// Measured on 2026-08-04 with go1.26.2 on a Ryzen 9 5950X: the 28-file format
// corpus loaded in about 5.3 ms holding about 80 KiB, and the one whole model
// below in about 1.9 ms holding about 60 KiB. The numbers are indicative and
// machine specific — what makes a regression visible is running the benchmark
// either side of a change, not the figures recorded here.
func BenchmarkLoadGraph(b *testing.B) {
	benchmarks := []struct {
		name string
		root string
	}{
		{name: "the fixture corpus", root: validCorpus},
		{name: "one whole model", root: filepath.Join("testdata", "graph", "valid")},
	}

	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				graph, _ := LoadGraph(benchmark.root)
				if graph == nil {
					b.Fatal("expected a graph")
				}
			}

			b.ReportMetric(float64(residentBytes(benchmark.root)), "B/graph")
		})
	}
}

// residentBytes is how much heap one loaded graph goes on holding.
//
// It is measured by collecting twice with the graph alive and comparing against
// the same measurement with it collected, which is coarse — the runtime's own
// allocations move the number by a few kilobytes — and is the only measurement
// available without a heap profile. What it is for is the direction of a
// change rather than the absolute figure.
func residentBytes(root string) uint64 {
	var before, after runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&before)

	graph, _ := LoadGraph(root)

	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(graph)

	if after.HeapAlloc < before.HeapAlloc {
		return 0
	}
	return after.HeapAlloc - before.HeapAlloc
}
