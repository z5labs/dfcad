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

// nodeFixture is the root of one fixture model: a registry and the nodes
// judged against it.
func nodeFixture(name string) string { return filepath.Join("testdata", "node", name) }

// loadNodeFixture loads a fixture model and renders the node diagnostics the
// way the command line interface would.
//
// The registry's own diagnostics are asserted empty rather than rendered. Every
// fixture here declares a registry which loads clean, so that what the golden
// beside it holds is what this layer had to say and nothing else. A fixture
// whose registry did not load clean fails here rather than further down: the
// nodes would then be judged against a registry missing whatever failed to
// load, and the mismatched golden that produces says nothing about the reason.
func loadNodeFixture(t *testing.T, name string) (*Nodes, string) {
	t.Helper()

	registry, registryDiags := LoadRegistry(nodeFixture(name))
	require.Empty(t, registryDiags, "the fixture registry loads clean")

	nodes, diags := LoadNodes(nodeFixture(name), registry)

	var collected Diagnostics
	collected.Add(diags...)

	var rendered strings.Builder
	require.NoError(t, collected.Render(&rendered, FileSources{}))

	return nodes, rendered.String()
}

// expectedNodeDiagnostics returns the rendering held beside the fixture, having
// first rewritten it from got when -update was passed.
func expectedNodeDiagnostics(t *testing.T, name string, got string) string {
	t.Helper()

	path := filepath.Join(nodeFixture(name), "diagnostics.txt")
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

func TestLoadNodes(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
	}{
		{
			name:    "names the closed set a kind or a geometry form was reaching into",
			fixture: "unknown-value",
		},
		{
			name:    "names the type and its position when no registry file declares it",
			fixture: "undeclared-type",
		},
		{
			name:    "names the node, the type and the value a type does not permit",
			fixture: "not-permitted",
		},
		{
			name:    "names the rule an id which is not one broke",
			fixture: "malformed-id",
		},
		{
			name:    "names the namespace an id was minted in and the registered set",
			fixture: "unknown-namespace",
		},
		{
			name:    "names both definitions of an id the model already holds, in whichever files they are",
			fixture: "duplicate-id",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, got := loadNodeFixture(t, testCase.fixture)

			assert.Equal(t, expectedNodeDiagnostics(t, testCase.fixture, got), got)
		})
	}
}

// loadModel writes a one-file registry and a one-file set of nodes into a
// temporary directory and loads them, requiring that neither has anything to
// report.
//
// It is for the tests which vary one thing about a model and compare the
// readings. A fixture on disk is the right shape for a test about diagnostics,
// where the rendering beside the source is the point; it is the wrong shape for
// a test whose whole subject is the difference between two models, because the
// difference is then somewhere other than in the test.
func loadModel(t *testing.T, registry, nodes string) *Nodes {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "registry"+Extension), []byte(registry), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "nodes"+Extension), []byte(nodes), 0o644))

	declared, diags := LoadRegistry(root)
	require.Empty(t, diags, "the written registry loads clean")

	read, diags := LoadNodes(root, declared)
	require.Empty(t, diags, "the written nodes load clean")

	return read
}

// TestLoadNodesAxes reads one node of each of the seven kinds, which is what
// says the axes describe the whole vocabulary rather than the part somebody
// happened to write a case for.
func TestLoadNodesAxes(t *testing.T) {
	nodes, rendered := loadNodeFixture(t, "valid")
	require.Empty(t, rendered, "the valid fixture loads clean")

	testCases := []struct {
		name     string
		id       ID
		label    string
		kind     Kind
		declared string
		geometry Geometry
	}{
		{
			name:     "reads a Zone",
			id:       "site:Z-01",
			label:    "Riverside campus",
			kind:     KindZone,
			declared: "Campus",
			geometry: GeometryArea,
		},
		{
			name:     "reads a Site",
			id:       "site:S-01",
			label:    "Riverside parcel",
			kind:     KindSite,
			declared: "SiteBoundary",
			geometry: GeometryArea,
		},
		{
			name:     "reads a Building",
			id:       "site:B-01",
			label:    "Riverside House",
			kind:     KindBuilding,
			declared: "OfficeBuilding",
			geometry: GeometrySolid,
		},
		{
			name:     "reads a Storey",
			id:       "site:L-01",
			label:    "Level 1",
			kind:     KindStorey,
			declared: "Level",
			geometry: GeometrySurface,
		},
		{
			name:     "reads a Space",
			id:       "site:S-101",
			label:    "Meeting Room B",
			kind:     KindSpace,
			declared: "MeetingRoom",
			geometry: GeometryArea,
		},
		{
			name:     "reads an Element",
			id:       "site:E-01",
			label:    "Partition between B and C",
			kind:     KindElement,
			declared: "Partition",
			geometry: GeometryLine,
		},
		{
			name:     "reads an Interface",
			id:       "site:I-01",
			label:    "Door into Meeting Room B",
			kind:     KindInterface,
			declared: "Doorway",
			geometry: GeometryPoint,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			node, ok := nodes.Node(testCase.id)

			require.True(t, ok)
			assert.Equal(t, testCase.label, node.Label())
			assert.Equal(t, testCase.kind, node.Kind())
			assert.Equal(t, testCase.declared, node.Type())

			geometry, hasGeometry := node.Geometry()
			assert.True(t, hasGeometry)
			assert.Equal(t, testCase.geometry, geometry)

			frame, hasFrame := node.Frame()
			assert.True(t, hasFrame)
			assert.Equal(t, ID("frame:building"), frame)

			assert.Equal(t, filepath.Join(nodeFixture("valid"), "nodes.dfc"), node.Span().Start.Path)
		})
	}

	t.Run("reads every kind the engine compiles in", func(t *testing.T) {
		var read []Kind
		for _, testCase := range testCases {
			read = append(read, testCase.kind)
		}

		assert.ElementsMatch(t, Kinds(), read)
	})
}

// TestLoadNodesWithoutGeometryOrFrame is its own function because it is the
// case the axes exist for rather than a variation on the table above: a node
// with no shape and no coordinate system loads, answers every question, and is
// malformed nowhere.
//
// A circuit group, a warranty and a system are all of them this node. A model
// which could not hold one would be a model in which every such thing had to be
// given a shape it does not have.
func TestLoadNodesWithoutGeometryOrFrame(t *testing.T) {
	nodes, rendered := loadNodeFixture(t, "valid")
	require.Empty(t, rendered)

	node, ok := nodes.Node("site:C-01")
	require.True(t, ok, "a node with neither geometry nor frame is still loaded")

	assert.Equal(t, KindZone, node.Kind())
	assert.Equal(t, "CircuitGroup", node.Type())
	assert.Equal(t, "Lighting circuit group 3", node.Label())

	// Absence is a state and not a value. Both axes report that they were not
	// written, which is what tells them apart from an axis written as an empty
	// one — a thing neither closed set has a member for.
	geometry, hasGeometry := node.Geometry()
	assert.False(t, hasGeometry)
	assert.Equal(t, Geometry(""), geometry)

	frame, hasFrame := node.Frame()
	assert.False(t, hasFrame)
	assert.Empty(t, frame)

	// The node beside it in the same file writes both, so absence here is the
	// node's and not the loader's.
	beside, ok := nodes.Node("site:S-101")
	require.True(t, ok)

	_, hasGeometry = beside.Geometry()
	_, hasFrame = beside.Frame()
	assert.True(t, hasGeometry)
	assert.True(t, hasFrame)
}

// TestLoadNodesReturnsWhatItCouldRead checks that a node whose type nothing
// declares is still a node.
//
// A caller reporting on a tree wants to say "site:S-101 is a Space whose type
// is undeclared", and one which had been handed only the diagnostic could say
// only the second half of that.
func TestLoadNodesReturnsWhatItCouldRead(t *testing.T) {
	nodes, rendered := loadNodeFixture(t, "undeclared-type")

	require.NotEmpty(t, rendered)
	require.Equal(t, 2, nodes.Len())

	node, ok := nodes.Node("site:S-101")

	require.True(t, ok)
	assert.Equal(t, ID("site:S-101"), node.ID())
	assert.Equal(t, KindSpace, node.Kind())
	assert.Equal(t, "MeetingRoon", node.Type())

	geometry, hasGeometry := node.Geometry()
	assert.True(t, hasGeometry)
	assert.Equal(t, GeometryArea, geometry)
}

// TestLoadNodesWithoutARegistry checks the load a consuming repository whose
// registry has not been written yet gets: every node names a type nothing
// declares and mints its id in a namespace nothing declares, and each says so
// with a position.
//
// Both are the same shape of answer for the same reason. Types and id
// namespaces are vocabulary the consuming repository owns, so a model with no
// registry has neither, and saying which of the two is empty is the whole of
// what a diagnostic here can usefully say.
func TestLoadNodesWithoutARegistry(t *testing.T) {
	nodes, diags := LoadNodes(nodeFixture("valid"), nil)

	require.Equal(t, 8, nodes.Len())

	hints := make(map[string]int, 2)
	for _, diagnostic := range diags {
		assert.Equal(t, SeverityError, diagnostic.Severity)
		assert.Contains(t, diagnostic.Message, "which no registry file declares")
		assert.NotEmpty(t, diagnostic.Span.Start.Path)

		hints[diagnostic.Hint]++
	}

	assert.Equal(t, map[string]int{
		"no type is declared; a registry file declares one with (type ...)":           8,
		"no namespace is declared; a registry file declares one with (namespace ...)": 8,
	}, hints, "one undeclared type and one undeclared namespace per node, and nothing else")
}

// TestLoadNodesIgnoresEverythingElse checks that this pass reads the semantic
// family and nothing else.
//
// The geometric family carries neither kind nor type, and registry forms are
// resolved before any node is interpreted. A pass which read either here would
// be reporting a vertex as a node missing its kind.
func TestLoadNodesIgnoresEverythingElse(t *testing.T) {
	registry, _ := LoadRegistry(registryFixture("valid"))

	nodes, diags := LoadNodes(registryFixture("valid"), registry)

	assert.Zero(t, nodes.Len())
	assert.Empty(t, diags)
}

func TestLoadNodesUnreadableRoot(t *testing.T) {
	nodes, diags := LoadNodes(filepath.Join("testdata", "node", "no-such-directory"), nil)

	assert.Zero(t, nodes.Len())
	assert.NotEmpty(t, diags)
}

// TestLoadNodesReportsStructureBeforeReadingIt checks that a node form which is
// structurally wrong is reported and not interpreted.
//
// A node missing its kind has no kind to invent, and one whose kind is written
// twice has two. Reading either would mean guessing, and the guess would then
// be judged against the registry as though somebody had written it.
func TestLoadNodesReportsStructureBeforeReadingIt(t *testing.T) {
	source := `(node site:S-101 (type MeetingRoom) (geometry area))` + "\n"

	dir := t.TempDir()
	path := filepath.Join(dir, "nodes"+Extension)
	require.NoError(t, os.WriteFile(path, []byte(source), 0o644))

	nodes, diags := LoadNodes(path, nil)

	assert.Zero(t, nodes.Len())
	require.Len(t, diags, 1)
	assert.Equal(t, "expected a (kind ...) child of the node form, found none", diags[0].Message)
}

// TestLoadNodesLabelIsNotIdentity checks the arrangement decision 0002 exists
// for: an id never changes, and a label is free to.
//
// A room called `Office 2.14` becomes `Meeting Room B`, and that is a change in
// what people call it rather than in which room it is. If the two were one
// field the rename would be a delete plus an insert to everything downstream,
// every reference would have to be rewritten in the same commit, and any
// external record filed under the old name would silently point at nothing.
func TestLoadNodesLabelIsNotIdentity(t *testing.T) {
	const registry = `(project (globalid-namespace "https://example.org/models/labels"))
(namespace site (description "Semantic nodes minted by this model."))
(type MeetingRoom (kind Space) (geometry area) (description "An enclosed room."))
`

	testCases := []struct {
		name     string
		written  string
		expected string
	}{
		{
			name:     "reads the label it was written with",
			written:  `(node site:S-101 (label "Office 2.14") (kind Space) (type MeetingRoom) (geometry area))`,
			expected: "Office 2.14",
		},
		{
			name:     "reads a label which changed, and changes nothing else",
			written:  `(node site:S-101 (label "Meeting Room B") (kind Space) (type MeetingRoom) (geometry area))`,
			expected: "Meeting Room B",
		},
		{
			name:    "reads a node whose label was left out, which is not a node missing something",
			written: `(node site:S-101 (kind Space) (type MeetingRoom) (geometry area))`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			nodes := loadModel(t, registry, testCase.written+"\n")

			// The same id resolves to the node however it is labelled, which is
			// the whole of what an id is for.
			node, ok := nodes.Node("site:S-101")

			require.True(t, ok)
			assert.Equal(t, testCase.expected, node.Label())

			// And every other axis reads the same, so a rename is a one-line
			// diff rather than a re-identification.
			assert.Equal(t, ID("site:S-101"), node.ID())
			assert.Equal(t, KindSpace, node.Kind())
			assert.Equal(t, "MeetingRoom", node.Type())

			geometry, hasGeometry := node.Geometry()
			assert.True(t, hasGeometry)
			assert.Equal(t, GeometryArea, geometry)
		})
	}
}

// TestLoadNodesInfersNothingFromAnID checks that the engine attaches no meaning
// to a namespace beyond its being declared, and none at all to a local part.
//
// The temptation is real: a namespace called `zone` looks like it says the
// thing is a Zone, and a model which read it that way would work until the day
// somebody minted a Space in it. An id that encodes what a thing is becomes a
// lie the first time the thing is reclassified, so nothing here reads one.
func TestLoadNodesInfersNothingFromAnID(t *testing.T) {
	const registry = `(project (globalid-namespace "https://example.org/models/namespaces"))
(namespace site (description "Semantic nodes minted by this model."))
(namespace zone (description "A namespace whose name is also a kind."))
(namespace vertex (description "A namespace whose name is also a geometric tag."))
(type MeetingRoom (kind Space) (geometry area) (description "An enclosed room."))
`

	const written = `(node site:S-101 (kind Space) (type MeetingRoom) (geometry area))
(node zone:S-101 (kind Space) (type MeetingRoom) (geometry area))
(node vertex:S-101 (kind Space) (type MeetingRoom) (geometry area))
(node site:Zone (kind Space) (type MeetingRoom) (geometry area))
(node site:solid (kind Space) (type MeetingRoom) (geometry area))
`

	nodes := loadModel(t, registry, written)

	require.Equal(t, 5, nodes.Len())

	for node := range nodes.All() {
		assert.Equal(t, KindSpace, node.Kind(), "the kind is what the node declared")
		assert.Equal(t, "MeetingRoom", node.Type(), "the type is what the node declared")

		geometry, hasGeometry := node.Geometry()
		assert.True(t, hasGeometry)
		assert.Equal(t, GeometryArea, geometry, "the geometry form is what the node declared")

		_, hasFrame := node.Frame()
		assert.False(t, hasFrame, "a namespace called frame or otherwise puts a node in none")
	}
}

// TestLoadNodesIndexesByID checks that a load answers "what is site:S-101"
// without walking the model.
//
// Every layer above resolves references by id — containment, zone membership,
// boundaries, supersession — so a scan per reference would make resolving a
// model quadratic in the size of the thing being resolved.
func TestLoadNodesIndexesByID(t *testing.T) {
	nodes, rendered := loadNodeFixture(t, "valid")
	require.Empty(t, rendered)

	var read int
	for node := range nodes.All() {
		read++

		found, ok := nodes.Node(node.ID())

		require.True(t, ok, "every node the walk read is reachable by its id")
		assert.Same(t, node, found)
	}
	assert.Equal(t, nodes.Len(), read, "All yields every node once")

	_, ok := nodes.Node("site:no-such-node")
	assert.False(t, ok)
}

// TestLoadNodesKeepsTheFirstDefinitionOfADuplicateID checks which of two nodes
// sharing an id the id goes on naming.
//
// It is the first, because an id which moved to the later definition would be
// an id which changed what it means — the one thing an id never does. The
// second is reported and is still a node; it is just not what that id resolves
// to.
func TestLoadNodesKeepsTheFirstDefinitionOfADuplicateID(t *testing.T) {
	nodes, rendered := loadNodeFixture(t, "duplicate-id")

	require.NotEmpty(t, rendered)
	require.Equal(t, 3, nodes.Len())

	node, ok := nodes.Node("site:S-101")

	require.True(t, ok)
	assert.Equal(t, "Meeting Room B", node.Label())

	// The node colliding with a declared frame is not what that id resolves to
	// either: the frame declared it first, and a frame is not a semantic node.
	_, ok = nodes.Node("frame:building")
	assert.False(t, ok)
}

// TestLoadNodesWithoutAnID checks that a node whose id could not be read is
// still returned and is reachable by nothing.
//
// Losing it would lose the only place the mistake is visible. Indexing it under
// the text somebody wrote would let a reference resolve through something the
// author has already been told is not an id.
func TestLoadNodesWithoutAnID(t *testing.T) {
	nodes, rendered := loadNodeFixture(t, "malformed-id")

	require.NotEmpty(t, rendered)
	require.Equal(t, 4, nodes.Len())

	for node := range nodes.All() {
		assert.Equal(t, ID(""), node.ID())
		assert.Equal(t, KindSpace, node.Kind(), "the rest of the node was still read")
	}

	_, ok := nodes.Node("")
	assert.False(t, ok)
}
