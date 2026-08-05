// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// observedFixture is the model whose entities link to observation files: a
// room, three corners and three afternoons of field work.
const observedFixture = "observed"

// counted replaces a graph's reader with one which records every path it was
// asked for, and returns the recording.
//
// A read is the thing under test in most of this file, and "it was not read" is
// a claim about behaviour with nothing observable behind it unless somebody
// counts. The counter goes in rather than around the call because there is no
// way from outside to tell a file which was opened from one which was not.
func counted(t *testing.T, graph *Graph) *reads {
	t.Helper()

	require.NotNil(t, graph.observations, "a loaded graph carries an observation store")

	recorded := &reads{}
	read := graph.observations.read
	graph.observations.read = func(path string) ([]byte, error) {
		recorded.record(path)
		return read(path)
	}

	return recorded
}

// reads is every path a store was asked to read, in order.
type reads struct {
	mu    sync.Mutex
	paths []string
}

func (r *reads) record(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, filepath.Base(path))
}

// names is the base name of every file read, in the order they were read.
func (r *reads) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.paths...)
}

// TestObservedIn is about the links alone, which are part of the model and are
// read when it is. Nothing here opens an observation file, and the case names
// say "names" rather than "reads" for that reason.
func TestObservedIn(t *testing.T) {
	graph, diags := loadGraphFixture(t, observedFixture)
	require.Empty(t, diags, "the fixture loads clean")

	testCases := []struct {
		name     string
		id       ID
		expected []string
	}{
		{
			name:     "names the file a semantic node was measured in",
			id:       "site:S-101",
			expected: []string{"observations/2026-05-07-interior.obs"},
		},
		{
			name: "names every file a corner was measured in, in the order they were written",
			id:   "geom:V-01",
			expected: []string{
				"observations/2026-05-06-site-control.obs",
				"observations/2026-05-07-interior.obs",
			},
		},
		{
			name:     "names the one file a corner measured in a single afternoon links to",
			id:       "geom:V-02",
			expected: []string{"observations/2026-05-06-site-control.obs"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var written []string
			for _, link := range entityOf(t, graph, testCase.id).ObservedIn() {
				written = append(written, link.Path)
			}

			assert.Equal(t, testCase.expected, written)
		})
	}
}

// TestObservedInIsWrittenOnEveryFamily is its own function because what it
// asserts is that all four entity forms carry the link rather than what any one
// of them holds.
//
// The link is on the [Entity] interface, so a family which stopped carrying one
// would not compile; what this checks is the half a compiler cannot, which is
// that each family's loader actually reads the child it is permitted.
func TestObservedInIsWrittenOnEveryFamily(t *testing.T) {
	file, err := Parse("entities/families.dfc", strings.NewReader(`
(node site:S-101
  (kind Space)
  (type MeetingRoom)
  (observed-in "observations/a.obs"))

(vertex geom:V-01
  (frame frame:site)
  (observed-in "observations/a.obs"))

(edge geom:E-01
  (frame frame:site)
  (vertices geom:V-01 geom:V-02)
  (observed-in "observations/a.obs"))

(loop geom:L-01
  (frame frame:site)
  (edges geom:E-01)
  (observed-in "observations/a.obs"))
`))
	require.NoError(t, err)
	require.Empty(t, Validate(file), "every family permits the child")

	sources := func(yield func(source) bool) { yield(source{path: file.Path, file: file}) }

	nodes, _ := loadNodes(sources, nil, registeredChecks)
	topology, _ := loadTopology(sources, nil, registeredChecks)

	for _, entity := range []Entity{
		mustLoaded[*SemanticNode](t, nodes.Node, "site:S-101"),
		mustLoaded[*Vertex](t, topology.Vertex, "geom:V-01"),
		mustLoaded[*Edge](t, topology.Edge, "geom:E-01"),
		mustLoaded[*Loop](t, topology.Loop, "geom:L-01"),
	} {
		links := entity.ObservedIn()

		require.Len(t, links, 1, "%s carries its link", entity.ID())
		assert.Equal(t, "observations/a.obs", links[0].Path)
		assert.Equal(t, file.Path, links[0].Span.Start.Path, "the link carries where it was written")
	}
}

// mustLoaded is the entity one loader's own lookup found, whichever family it
// belongs to. It is generic over the lookup so that the four families are one
// table rather than four near-identical helpers.
func mustLoaded[T Entity](t *testing.T, lookup func(ID) (T, bool), id ID) T {
	t.Helper()

	node, ok := lookup(id)
	require.True(t, ok, "the load read %s", id)

	return node
}

// TestObservedInHoldsARepeatedPathOnce checks the rule a link shares with every
// other reference of the format: naming a file twice says what naming it once
// says.
func TestObservedInHoldsARepeatedPathOnce(t *testing.T) {
	file, err := Parse("entities/twice.dfc", strings.NewReader(`
(vertex geom:V-01
  (frame frame:site)
  (observed-in "observations/a.obs")
  (observed-in "observations/b.obs")
  (observed-in "observations/a.obs"))
`))
	require.NoError(t, err)

	sources := func(yield func(source) bool) { yield(source{path: file.Path, file: file}) }
	topology, _ := loadTopology(sources, nil, registeredChecks)

	vertex := mustLoaded[*Vertex](t, topology.Vertex, "geom:V-01")

	var written []string
	for _, link := range vertex.ObservedIn() {
		written = append(written, link.Path)
	}

	assert.Equal(t, []string{"observations/a.obs", "observations/b.obs"}, written)
}

// TestLoadGraphReadsNoObservationFile is the story's central claim, checked two
// ways at once.
//
// The counter says that the store opened nothing, and the fixture says the same
// thing without depending on the counter being wired to the only path a read
// can take: one of the files it links to holds a line no reader can make sense
// of, and the model loads clean. A load which read the file would have to
// report that line, so a clean load is evidence rather than an absence of it.
func TestLoadGraphReadsNoObservationFile(t *testing.T) {
	root := graphFixture(observedFixture)

	// The store is built by the load, so the counter cannot be installed before
	// it. What it counts here is everything after the load, which the
	// assertions below require to be nothing at all.
	graph, diags := LoadGraph(root)
	require.Empty(t, renderGraphDiagnostics(t, diags), "the model loads clean, malformed observation file and all")

	recorded := counted(t, graph)

	assert.Empty(t, recorded.names(), "the load opened no observation file")
	assert.Zero(t, graph.observations.reads, "the load read no observation file")
	assert.Empty(t, graph.observations.content, "the load holds no observation file's bytes")
}

// TestLoadGraphReportsALinkWithoutReadingIt checks that the diagnostics a load
// produces about the links cost no reading either.
//
// It is the same claim as the one above from the other side: a model whose
// links are every kind of wrong is still a model nothing opened a file for, so
// the report a load gives about its observations is a report about paths.
func TestLoadGraphReportsALinkWithoutReadingIt(t *testing.T) {
	graph, diags := LoadGraph(graphFixture("unresolved-observations"))
	require.NotEmpty(t, diags, "the fixture is reported rather than accepted")

	assert.Zero(t, graph.observations.reads, "the diagnostics were produced without reading anything")
}

func TestGraphObservations(t *testing.T) {
	graph, diags := loadGraphFixture(t, observedFixture)
	require.Empty(t, diags, "the fixture loads clean")

	recorded := counted(t, graph)

	log, problems := graph.Observations(entityOf(t, graph, "geom:V-01"))

	require.Empty(t, problems, "the two files the corner links to are sound")
	assert.Equal(t, []string{
		"2026-05-06-site-control.obs",
		"2026-05-07-interior.obs",
	}, recorded.names(), "the files are read in the lexical order of their paths")

	// The log is one log across both files, which is what makes "earlier" a
	// question with one answer: the retirement in the first file names a record
	// in the first file, and the records of the second follow all of them.
	var records []string
	for observation := range log.Observations() {
		records = append(records, string(observation.ID))
	}
	assert.Equal(t, []string{
		"shot:2026-05-06-0001",
		"shot:2026-05-06-0002",
		"shot:2026-05-06-0003",
		"shot:2026-05-07-0001",
		"shot:2026-05-07-0002",
	}, records)

	assert.True(t, log.Retired("shot:2026-05-06-0003"), "the float shot is retired by the later record")
}

// TestGraphObservationsReadsTheFileTheQueryNeeds is the other half of the
// central claim: a question the records answer does read them.
//
// The malformed line is what makes it visible. Nothing about the entity says
// the file is suspect, and nothing about the load reported it; asking for the
// records is what produces the diagnostic, which is only possible if the file
// was opened by this call.
func TestGraphObservationsReadsTheFileTheQueryNeeds(t *testing.T) {
	graph, diags := loadGraphFixture(t, observedFixture)
	require.Empty(t, diags, "the fixture loads clean")

	recorded := counted(t, graph)

	log, problems := graph.Observations(entityOf(t, graph, "geom:V-03"))

	assert.Equal(t, []string{"2026-05-08-suspect.obs"}, recorded.names(), "the query read the file")
	require.Len(t, problems, 1, "the malformed line the load said nothing about is reported here")
	assert.Equal(t, SeverityError, problems[0].Severity)
	assert.Equal(t,
		filepath.Join(graphFixture(observedFixture), "observations", "2026-05-08-suspect.obs"),
		problems[0].Span.Start.Path,
		"the file is reported as this run can reach it, so the line it is about can be quoted")
	assert.Zero(t, log.Len(), "a line which could not be read is not a record")
}

// TestGraphObservationsReadsEachFileOnce checks that reading is once per file
// for the life of the graph, however many things link to it and however often
// they are asked about.
//
// Three entities and four questions between them reach three files, and the
// two which share a file share the reading of it. That is what "once within a
// single command invocation" means: a command which walks a model asking about
// every corner pays for the field work once rather than once per corner.
func TestGraphObservationsReadsEachFileOnce(t *testing.T) {
	graph, diags := loadGraphFixture(t, observedFixture)
	require.Empty(t, diags, "the fixture loads clean")

	recorded := counted(t, graph)

	for _, id := range []ID{"geom:V-01", "geom:V-01", "site:S-101", "geom:V-02", "geom:V-01"} {
		log, problems := graph.Observations(entityOf(t, graph, id))

		require.Empty(t, problems, "%s links to sound files", id)
		require.NotZero(t, log.Len(), "%s has records behind it", id)
	}

	assert.Equal(t, []string{
		"2026-05-06-site-control.obs",
		"2026-05-07-interior.obs",
	}, recorded.names(), "each file was read once, whoever asked for it")
}

// TestGraphObservationsOfAnEntityWhichLinksToNothing is its own function
// because an entity with no links is the ordinary case rather than a failure,
// and what it asserts is that nothing happens.
func TestGraphObservationsOfAnEntityWhichLinksToNothing(t *testing.T) {
	graph, diags := loadGraphFixture(t, "valid")
	require.Empty(t, diags, "the fixture loads clean")

	recorded := counted(t, graph)

	for entity := range graph.entities() {
		log, problems := graph.Observations(entity)

		assert.Zero(t, log.Len(), "%s has no records behind it", entity.ID())
		assert.Empty(t, problems)
	}

	assert.Empty(t, recorded.names(), "a model which links to nothing opens nothing")
}

// TestGraphObservationsOfNothing checks the two calls which have no entity to
// answer about, which are the ones a caller makes without meaning to.
func TestGraphObservationsOfNothing(t *testing.T) {
	graph, _ := loadGraphFixture(t, observedFixture)

	testCases := []struct {
		name   string
		graph  *Graph
		entity Entity
	}{
		{name: "answers nothing about no entity", graph: graph},
		{name: "answers nothing about no graph", graph: nil, entity: &Vertex{}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			log, problems := testCase.graph.Observations(testCase.entity)

			assert.Zero(t, log.Len())
			assert.Empty(t, problems)
		})
	}
}

// TestGraphObservationFiles checks the whole-model question, which is answered
// from the links alone.
func TestGraphObservationFiles(t *testing.T) {
	graph, diags := loadGraphFixture(t, observedFixture)
	require.Empty(t, diags, "the fixture loads clean")

	recorded := counted(t, graph)

	assert.Equal(t, []string{
		"observations/2026-05-06-site-control.obs",
		"observations/2026-05-07-interior.obs",
		"observations/2026-05-08-suspect.obs",
	}, graph.ObservationFiles())
	assert.Empty(t, recorded.names(), "how much field work there is does not require reading any of it")
}

// TestGraphObservationsOfAFileWhichWentAway checks the read which fails, which
// is a different answer from the load-time one: the file was there when the
// model loaded and is not there now.
func TestGraphObservationsOfAFileWhichWentAway(t *testing.T) {
	root := t.TempDir()
	writeObservationModel(t, root, 1)

	graph, diags := LoadGraph(root)
	require.Empty(t, renderGraphDiagnostics(t, diags), "the model loads clean while the file is there")

	require.NoError(t, os.Remove(filepath.Join(root, "observations", "field.obs")))

	log, problems := graph.Observations(entityOf(t, graph, "geom:V-01"))

	require.Len(t, problems, 1, "the read which failed is reported rather than swallowed")
	assert.Equal(t, SeverityError, problems[0].Severity)
	assert.Contains(t, problems[0].Message, "geom:V-01",
		"the query reports a file which went away in the words the load reports one in")
	assert.Contains(t, problems[0].Message, "observations/field.obs")
	assert.Equal(t, filepath.Join(root, "model.dfc"), problems[0].Span.Start.Path,
		"the diagnostic points at the link rather than at a file which is not there to point at")
	assert.Zero(t, log.Len())
}

// TestLoadGraphMemoryIsIndependentOfObservationFileSize is the property the
// whole arrangement exists for, stated as a measurement.
//
// Two models are identical but for the size of the observation file behind
// their one corner: one holds a handful of records and the other holds a
// season's worth. If a load read either of them, the second load would allocate
// the difference between the two files and go on holding some of it, and the
// figures below would be orders of magnitude apart rather than within noise of
// each other.
//
// The margin is generous because both measurements are coarse — the runtime's
// own allocations move them by tens of kilobytes, and the race detector moves
// them further. It does not need to be tight: what it has to separate is a
// fixed cost from one which grows with a file measured in megabytes.
func TestLoadGraphMemoryIsIndependentOfObservationFileSize(t *testing.T) {
	var (
		small = t.TempDir()
		large = t.TempDir()
	)

	difference := writeObservationModel(t, large, largeObservationRecords) - writeObservationModel(t, small, 4)

	require.Greater(t, difference, int64(4<<20), "the large fixture is megabytes larger than the small one")

	// A megabyte is comfortably above the noise of either measurement and
	// comfortably below the difference between the two files, so a load which
	// read the file cannot pass this whichever way the noise fell.
	const margin = 1 << 20

	allocatedSmall, heldSmall := loadCost(t, small)
	allocatedLarge, heldLarge := loadCost(t, large)

	assert.Less(t, allocatedLarge, allocatedSmall+margin,
		"loading the model with %d bytes more observations allocated %d bytes more", difference, allocatedLarge-allocatedSmall)
	assert.Less(t, heldLarge, heldSmall+margin,
		"the graph holds %d bytes more for %d bytes more observations", heldLarge-heldSmall, difference)
}

// largeObservationRecords is how many records the large fixture holds. At a
// little over two hundred bytes a line it is a file of about eight megabytes,
// which is a modest afternoon and is three orders of magnitude larger than the
// model citing it.
const largeObservationRecords = 40000

// loadCost is what one load of a model costs: the bytes it allocated on the way
// and the bytes the graph goes on holding afterwards.
func loadCost(t testing.TB, root string) (allocated, held uint64) {
	t.Helper()

	var before, after runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&before)

	graph, diags := LoadGraph(root)
	require.Empty(t, diags, "the fixture loads clean")

	runtime.ReadMemStats(&after)
	allocated = after.TotalAlloc - before.TotalAlloc

	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(graph)

	if after.HeapAlloc > before.HeapAlloc {
		held = after.HeapAlloc - before.HeapAlloc
	}

	return allocated, held
}

// writeObservationModel writes a model of one corner linked to one observation
// file of records records, and returns the size of that file.
//
// It is generated rather than held in testdata because what it is for is a
// file too large to keep in a repository: the point of the fixture is its size,
// and a checked-in eight megabytes of synthetic shots would be paid for by
// every clone forever.
func writeObservationModel(t testing.TB, root string, records int) int64 {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Join(root, "observations"), 0o755))

	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
	}

	write("registry.dfc", `(project
  (label "Generated observation fixture")
  (globalid-namespace "https://example.org/models/generated"))

(namespace fix (description "Fix qualities an instrument reports."))
(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "How a value was obtained."))
(namespace session (description "Field occupations."))
(namespace shot (description "Observation records issued by the field crew."))

(frame frame:site (label "Site survey grid") (unit m))
`)

	write("model.dfc", `(vertex geom:V-01
  (label "The one corner")
  (frame frame:site)
  (observed-in "observations/field.obs"))
`)

	var out strings.Builder
	out.WriteString("# id at frame x y z method fix h-precision v-precision antenna session\n")
	for record := range records {
		fmt.Fprintf(&out,
			"obs shot:2026-05-06-%06d 2026-05-06T09:14:22Z frame:site %.3f %.3f %.3f method:gnss-rtk fix:rtk-fixed 0.012 0.021 2.000 session:2026-05-06-am\n",
			record, 412300.0+float64(record)/1000, 5318220.0+float64(record)/1000, 34.0+float64(record)/1000)
	}
	write(filepath.Join("observations", "field.obs"), out.String())

	info, err := os.Stat(filepath.Join(root, "observations", "field.obs"))
	require.NoError(t, err)

	return info.Size()
}

// BenchmarkLoadGraphWithObservations is the same measurement as the test above,
// run as a benchmark so that the figures are reportable rather than only
// asserted.
//
// Measured on 2026-08-05 with go1.26.2 on a Ryzen 9 5950X: four records and
// forty thousand of them — a file of about 8.6 MiB — both loaded in about
// 0.2 ms, allocating about 165 KiB over 2552 allocations and holding about
// 6 KiB, the two sizes agreeing to the allocation. The numbers are indicative
// and machine specific — what makes a regression visible is running the
// benchmark either side of a change and seeing the two sizes diverge at all.
func BenchmarkLoadGraphWithObservations(b *testing.B) {
	benchmarks := []struct {
		name    string
		records int
	}{
		{name: "four records", records: 4},
		{name: "a season of field work", records: largeObservationRecords},
	}

	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			root := b.TempDir()
			writeObservationModel(b, root, benchmark.records)

			b.ReportAllocs()

			for b.Loop() {
				graph, _ := LoadGraph(root)
				if graph == nil {
					b.Fatal("expected a graph")
				}
			}

			b.ReportMetric(float64(residentBytes(root)), "B/graph")
		})
	}
}
