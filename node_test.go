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
// beside it holds is what this layer had to say and nothing else.
func loadNodeFixture(t *testing.T, name string) ([]*SemanticNode, string) {
	t.Helper()

	registry, registryDiags := LoadRegistry(nodeFixture(name))
	for _, diagnostic := range registryDiags {
		t.Errorf("unexpected registry diagnostic: %s", diagnostic)
	}

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
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, got := loadNodeFixture(t, testCase.fixture)

			assert.Equal(t, expectedNodeDiagnostics(t, testCase.fixture, got), got)
		})
	}
}

// TestLoadNodesAxes reads one node of each of the seven kinds, which is what
// says the axes describe the whole vocabulary rather than the part somebody
// happened to write a case for.
func TestLoadNodesAxes(t *testing.T) {
	nodes, rendered := loadNodeFixture(t, "valid")
	require.Empty(t, rendered, "the valid fixture loads clean")

	byID := make(map[string]*SemanticNode, len(nodes))
	for _, node := range nodes {
		byID[node.ID()] = node
	}

	testCases := []struct {
		name     string
		id       string
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
			node, ok := byID[testCase.id]

			require.True(t, ok)
			assert.Equal(t, testCase.label, node.Label())
			assert.Equal(t, testCase.kind, node.Kind())
			assert.Equal(t, testCase.declared, node.Type())

			geometry, hasGeometry := node.Geometry()
			assert.True(t, hasGeometry)
			assert.Equal(t, testCase.geometry, geometry)

			frame, hasFrame := node.Frame()
			assert.True(t, hasFrame)
			assert.Equal(t, "frame:building", frame)

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

	var node *SemanticNode
	for _, candidate := range nodes {
		if candidate.ID() == "site:C-01" {
			node = candidate
		}
	}
	require.NotNil(t, node, "a node with neither geometry nor frame is still loaded")

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
	for _, candidate := range nodes {
		if candidate.ID() != "site:S-101" {
			continue
		}

		_, hasGeometry := candidate.Geometry()
		_, hasFrame := candidate.Frame()
		assert.True(t, hasGeometry)
		assert.True(t, hasFrame)
	}
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
	require.Len(t, nodes, 2)

	assert.Equal(t, "site:S-101", nodes[0].ID())
	assert.Equal(t, KindSpace, nodes[0].Kind())
	assert.Equal(t, "MeetingRoon", nodes[0].Type())

	geometry, hasGeometry := nodes[0].Geometry()
	assert.True(t, hasGeometry)
	assert.Equal(t, GeometryArea, geometry)
}

// TestLoadNodesWithoutARegistry checks the load a consuming repository whose
// registry has not been written yet gets: every node names a type nothing
// declares, and each says so with a position.
func TestLoadNodesWithoutARegistry(t *testing.T) {
	nodes, diags := LoadNodes(nodeFixture("valid"), nil)

	require.Len(t, nodes, 8)
	require.Len(t, diags, 8, "one undeclared type per node, and nothing else")

	for _, diagnostic := range diags {
		assert.Equal(t, SeverityError, diagnostic.Severity)
		assert.Contains(t, diagnostic.Message, "which no registry file declares")
		assert.Equal(t, "no type is declared; a registry file declares one with (type ...)", diagnostic.Hint)
		assert.NotEmpty(t, diagnostic.Span.Start.Path)
	}
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

	assert.Empty(t, nodes)
	assert.Empty(t, diags)
}

func TestLoadNodesUnreadableRoot(t *testing.T) {
	nodes, diags := LoadNodes(filepath.Join("testdata", "node", "no-such-directory"), nil)

	assert.Empty(t, nodes)
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

	assert.Empty(t, nodes)
	require.Len(t, diags, 1)
	assert.Equal(t, "expected a (kind ...) child of the node form, found none", diags[0].Message)
}
