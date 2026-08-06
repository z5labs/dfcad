// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// exportRegistry is the vocabulary the fixture below is judged against.
//
// It declares one type per kind the export writes, and it declares the
// classifications three different ways on purpose: a type which names an
// entity the writer can write, one which names an entity it cannot, and one
// which names nothing at all. Those are the three branches the fallback to a
// proxy has.
const exportRegistry = `(project
  (label "Riverside example")
  (description "The example model the export fixture is drawn from.")
  (globalid-namespace "https://example.org/models/riverside"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace site (description "Semantic nodes minted by this model."))

(frame frame:building (label "Building local grid") (unit m))

(type Parcel
  (kind Site)
  (geometry absent)
  (description "A plot of land.")
  (classification "IFC4" "IfcSite"))

(type OfficeBuilding
  (kind Building)
  (geometry absent)
  (description "A building let as offices."))

(type Level
  (kind Storey)
  (geometry absent)
  (description "One floor of a building."))

(type MeetingRoom
  (kind Space)
  (geometry absent)
  (description "An enclosed room used for meetings.")
  (classification "IFC4" "IfcSpace")
  (classification "Uniclass2015" "SL_25_10_50"))

(type Campus
  (kind Zone)
  (geometry absent)
  (description "A group of things administered together."))

(type PartitionWall
  (kind Element)
  (geometry absent)
  (description "A wall dividing two rooms.")
  (classification "IFC4" "IfcWall"))

(type Fitting
  (kind Element)
  (geometry absent)
  (description "Something fitted into a room, which no IFC entity names."))

(type Threshold
  (kind Interface)
  (geometry absent)
  (description "Where two rooms meet.")
  (classification "IFC4" "IfcRelSpaceBoundary"))
`

// exportEntities is the model the golden holds.
//
// It is written with its nodes out of id order and out of containment order,
// which is what says that the emitted file's order is the model's rather than
// the order somebody typed it in. The retired node is here because a thing
// which stopped existing must not reach a receiving system as a live one.
const exportEntities = `(node site:S-102
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (within site:L-01)
  (member-of site:C-01))

(node site:P-01
  (label "Plot one")
  (kind Site)
  (type Parcel))

(node site:W-01
  (label "Room A north partition")
  (kind Element)
  (type PartitionWall)
  (within site:S-101))

(node site:B-01
  (label "Block A")
  (kind Building)
  (type OfficeBuilding)
  (within site:P-01))

(node site:C-01
  (label "West campus")
  (kind Zone)
  (type Campus))

(node site:S-101
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (within site:L-01)
  (member-of site:C-01))

(node site:F-01
  (label "Room A projector mount")
  (kind Element)
  (type Fitting)
  (within site:S-101))

(node site:T-01
  (label "Rooms A and B, shared wall")
  (kind Interface)
  (type Threshold)
  (within site:S-101))

(node site:L-01
  (label "Level one")
  (kind Storey)
  (type Level)
  (within site:B-01))

(node site:S-199
  (label "The old store")
  (kind Space)
  (type MeetingRoom)
  (within site:L-01)
  (retired
    (date "2026-04-02")
    (reason "Merged into Meeting Room A.")
    (superseded-by site:S-101)))
`

// exportSecondFrame is a second grid, in another unit, measured against the
// first.
//
// It is a sound model — a fabrication grid in millimetres beside a building
// grid in metres is an ordinary thing to have — which is the point: what it
// exercises is a refusal over something correct rather than over a mistake.
const exportSecondFrame = `
(namespace method (description "Measurement methods used on this project."))

(predicate frame-transform
  (shape transform)
  (description "The rigid transform from a frame to its parent."))

(frame frame:fabrication
  (label "Fabrication grid")
  (unit mm)
  (parent frame:building)
  (transform site:C-0001)
  (frame-transform
    (id site:C-0001)
    (value
      (transform
        (translation 0.0 0.0 0.0)
        (rotation 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0)
        (scale 1.0)))
    (source "Setting-out record SO-2026-014, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-03-02")))
`

// exportModel is the fixture tree every test below is run against.
func exportModel() map[string]string {
	return map[string]string{
		"registry.dfc":      exportRegistry,
		"entities/site.dfc": exportEntities,
	}
}

// exporting runs export against a fixture and returns what it wrote, requiring
// the run to have exited with the code given.
func exporting(t *testing.T, expectedCode int, files map[string]string, args ...string) (exportResult, string, string) {
	t.Helper()

	root := tree(t, files)
	stdout, stderr := invoke(t, expectedCode, root, append([]string{"export"}, args...)...)

	if strings.TrimSpace(stdout) == "" {
		return exportResult{}, root, stderr
	}

	return listed[exportResult](t, stdout), root, stderr
}

// artefact is the bytes of the one file a result names.
func artefact(t *testing.T, result exportResult) string {
	t.Helper()

	require.Len(t, result.Files, 1)

	src, err := os.ReadFile(result.Files[0].Path)
	require.NoError(t, err)

	return string(src)
}

func TestRunExport(t *testing.T) {
	result, root, _ := exporting(t, exitSuccess, exportModel())

	assert.Equal(t, "export", result.Command)
	assert.True(t, result.Derived)
	assert.Equal(t, "IFC4", result.Schema)
	assert.Empty(t, result.Identifiers, "the manifest is evidence, and evidence is asked for")

	t.Run("names the digest of the source tree the artefact was derived from", func(t *testing.T) {
		digest, err := dfcad.DigestOf(root)
		require.NoError(t, err)

		assert.Equal(t, digest.String(), result.Digest)
	})

	t.Run("writes one file, beneath the build directory, keyed by that digest", func(t *testing.T) {
		require.Len(t, result.Files, 1)

		assert.Equal(t, statusWritten, result.Files[0].Status)
		assert.Equal(t,
			filepath.Join(root, dfcad.BuildDir, "export", result.Digest, "model.ifc"),
			result.Files[0].Path)

		assert.FileExists(t, result.Files[0].Path)
	})

	t.Run("holds the golden the review of this format reads", func(t *testing.T) {
		got := artefact(t, result)

		assert.Equal(t, exportGolden(t, got), got,
			"the exported artefact is stale; regenerate it with: go test ./cmd/dfcad -update")
	})
}

// exportGolden is the recorded artefact, rewritten from got under -update.
func exportGolden(t *testing.T, got string) string {
	t.Helper()

	const path = "testdata/export/model.ifc"

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestRunExportIsByteIdenticalForAnUnchangedTree is its own function because
// it is about two runs rather than about one: the golden above says what the
// bytes are, and this says that nothing outside the model reaches them.
func TestRunExportIsByteIdenticalForAnUnchangedTree(t *testing.T) {
	first, _, _ := exporting(t, exitSuccess, exportModel())

	// A second tree, at a different path, written at a different moment, by a
	// second process's worth of map iteration.
	second, _, _ := exporting(t, exitSuccess, exportModel())

	assert.Equal(t, first.Digest, second.Digest)
	assert.Equal(t, artefact(t, first), artefact(t, second))
}

// TestRunExportLeavesAnArtefactItWouldHaveRewrittenAlone is its own function
// because it is about the second run over one tree, which is the cache hit the
// digest key buys.
func TestRunExportLeavesAnArtefactItWouldHaveRewrittenAlone(t *testing.T) {
	root := tree(t, exportModel())

	stdout, _ := invoke(t, exitSuccess, root, "export")
	first := listed[exportResult](t, stdout)
	require.Len(t, first.Files, 1)
	require.Equal(t, statusWritten, first.Files[0].Status)

	before, err := os.Stat(first.Files[0].Path)
	require.NoError(t, err)

	stdout, _ = invoke(t, exitSuccess, root, "export")
	second := listed[exportResult](t, stdout)

	require.Len(t, second.Files, 1)
	assert.Equal(t, statusUnchanged, second.Files[0].Status)
	assert.Equal(t, first.Files[0].Path, second.Files[0].Path)

	after, err := os.Stat(second.Files[0].Path)
	require.NoError(t, err)

	assert.Equal(t, before.ModTime(), after.ModTime(),
		"an unchanged artefact is left where it is rather than rewritten with the same bytes")
}

// TestRunExportNeverWritesToTheModelRoot is its own function because what it
// asserts is about the authored tree rather than about the artefact: an export
// beside the entity files is read by nothing and reviewed by nobody.
func TestRunExportNeverWritesToTheModelRoot(t *testing.T) {
	root := tree(t, exportModel())

	before := authoredFiles(t, root)

	invoke(t, exitSuccess, root, "export")

	assert.Equal(t, before, authoredFiles(t, root))
}

// authoredFiles is every file beneath root which is not in the build directory,
// with its contents, which is the whole of what an export must not touch.
func authoredFiles(t *testing.T, root string) map[string]string {
	t.Helper()

	out := make(map[string]string)

	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == dfcad.BuildDir {
				return filepath.SkipDir
			}
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		out[relative] = string(src)

		return nil
	}))

	return out
}

func TestRunExportWritesTheSpatialStructureFromTheKind(t *testing.T) {
	result, _, _ := exporting(t, exitSuccess, exportModel())
	source := artefact(t, result)

	testCases := []struct {
		name     string
		expected string
	}{
		{
			name:     "declares the schema exactly",
			expected: "FILE_SCHEMA(('IFC4'));",
		},
		{
			name:     "stamps the header with the derivation epoch rather than a clock",
			expected: "'1970-01-01T00:00:00'",
		},
		{
			name:     "writes a site for the node whose kind is Site",
			expected: "IFCSITE('",
		},
		{
			name:     "writes a building for the node whose kind is Building",
			expected: "IFCBUILDING('",
		},
		{
			name:     "writes a storey for the node whose kind is Storey",
			expected: "IFCBUILDINGSTOREY('",
		},
		{
			name:     "writes a space for the node whose kind is Space",
			expected: "IFCSPACE('",
		},
		{
			name:     "writes a zone for the node whose kind is Zone",
			expected: "IFCZONE('",
		},
		{
			name:     "carries a type's declared IFC4 classification through as the entity",
			expected: "IFCWALL('",
		},
		{
			name:     "falls back to a proxy naming the type where none is declared",
			expected: "IFCBUILDINGELEMENTPROXY('",
		},
		{
			name:     "aggregates the decomposition",
			expected: "IFCRELAGGREGATES('",
		},
		{
			name:     "contains the products in the space they stand in",
			expected: "IFCRELCONTAINEDINSPATIALSTRUCTURE('",
		},
		{
			name:     "assigns the zone's members to it",
			expected: "IFCRELASSIGNSTOGROUP('",
		},
		{
			name:     "places every element in the coordinate system of its parent",
			expected: "IFCLOCALPLACEMENT(",
		},
		{
			name:     "states the units everything is expressed in",
			expected: "IFCSIUNIT(*,.LENGTHUNIT.,$,.METRE.);",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Contains(t, source, testCase.expected)
		})
	}

	t.Run("writes no owner history, which is where a clock would be mandatory", func(t *testing.T) {
		assert.NotContains(t, source, "IFCOWNERHISTORY")
	})

	t.Run("writes nothing for a node which stopped existing", func(t *testing.T) {
		assert.NotContains(t, source, "site:S-199")
	})

	t.Run("names a proxy's type in its object type", func(t *testing.T) {
		assert.Contains(t, source, "'site:F-01','Room A projector mount','Fitting'")
	})

	t.Run("falls back to a proxy where the declared entity is not one it can write", func(t *testing.T) {
		// The Threshold type is classified as IfcRelSpaceBoundary, which is a
		// relationship rather than a product. The thing is still in the model
		// and still has to be in the file.
		assert.Contains(t, source, "'site:T-01','Rooms A and B, shared wall','Threshold'")
		assert.NotContains(t, source, "IFCRELSPACEBOUNDARY")
	})
}

// TestRunExportGivesEveryObjectTheIdentifierTheModelDerives is its own
// function because it asserts a relation between the artefact and a derivation
// anybody can reproduce, rather than the presence of something in the file.
func TestRunExportGivesEveryObjectTheIdentifierTheModelDerives(t *testing.T) {
	result, _, _ := exporting(t, exitSuccess, exportModel(), "--evidence")
	source := artefact(t, result)

	const url = "https://example.org/models/riverside"

	t.Run("derives each node's identifier from the pinned URL and its id", func(t *testing.T) {
		for _, id := range []string{"site:P-01", "site:B-01", "site:L-01", "site:S-101", "site:C-01", "site:W-01"} {
			derived := dfcad.DeriveGlobalID(url, dfcad.ID(id))

			assert.Contains(t, source, "'"+derived.String()+"'", id)
		}
	})

	t.Run("reports the manifest under --evidence, ascending by id", func(t *testing.T) {
		require.NotEmpty(t, result.Identifiers)

		ids := make([]string, 0, len(result.Identifiers))
		for _, identifier := range result.Identifiers {
			ids = append(ids, identifier.ID)
			assert.Len(t, identifier.GlobalID, 22)
		}

		assert.True(t, slices.IsSorted(ids), "the manifest is ascending by id")
		assert.Contains(t, ids, "site:S-101")
		assert.Contains(t, ids, "ifc/project")
	})

	t.Run("gives the relationships identifiers of their own", func(t *testing.T) {
		nodes := 0
		for _, identifier := range result.Identifiers {
			if strings.HasPrefix(identifier.ID, "ifc/") {
				continue
			}
			nodes++
		}

		assert.Less(t, nodes, len(result.Identifiers),
			"a relationship is a rooted object and carries an identifier the manifest accounts for")
	})

	t.Run("derives an identifier for a retired node no more than for a live one", func(t *testing.T) {
		derived := dfcad.DeriveGlobalID(url, dfcad.ID("site:S-199"))

		assert.NotContains(t, source, derived.String())
	})
}

func TestRunExportWritesWhereItIsTold(t *testing.T) {
	t.Run("writes to a destination outside the model root", func(t *testing.T) {
		destination := filepath.Join(t.TempDir(), "elsewhere", "spatial.ifc")

		root := tree(t, exportModel())
		stdout, _ := invoke(t, exitSuccess, root, "export", "--out", destination)
		result := listed[exportResult](t, stdout)

		require.Len(t, result.Files, 1)
		assert.Equal(t, destination, result.Files[0].Path)
		assert.FileExists(t, destination)
	})

	t.Run("writes to a destination inside the build directory, which is not authored", func(t *testing.T) {
		root := tree(t, exportModel())

		stdout, _ := invoke(t, exitSuccess, root, "export", "--out", ".dfcad/latest.ifc")
		result := listed[exportResult](t, stdout)

		require.Len(t, result.Files, 1)
		assert.FileExists(t, filepath.Join(root, ".dfcad", "latest.ifc"))
	})
}

// TestRunExportRefusesADestinationInsideTheAuthoredTree is its own function
// because it is a usage error rather than a verdict on a model: it is decided
// before anything is read, and it writes nothing at all to stdout.
func TestRunExportRefusesADestinationInsideTheAuthoredTree(t *testing.T) {
	testCases := []struct {
		name string
		out  string
	}{
		{name: "the model root itself", out: "model.ifc"},
		{name: "a directory of entity files", out: "entities/model.ifc"},
		{name: "a path which walks back in", out: ".dfcad/../model.ifc"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, exportModel())

			stdout, stderr := invoke(t, exitUsage, root, "export", "--out", testCase.out)

			assert.Empty(t, stdout)
			assert.Contains(t, stderr, "outside the model root")
			assert.NoFileExists(t, filepath.Join(root, testCase.out))
		})
	}
}

// TestRunExportAnswersOnARefusal is its own function because a refusal is
// still an answer: the object comes back with derived false and the digest of
// the tree it was refused against, so a caller reads why from stderr rather
// than from an empty stream.
func TestRunExportAnswersOnARefusal(t *testing.T) {
	testCases := []struct {
		name     string
		files    map[string]string
		expected string
	}{
		{
			name: "a model whose frames disagree about the linear unit",
			files: map[string]string{
				"registry.dfc":      exportRegistry + exportSecondFrame,
				"entities/site.dfc": exportEntities,
			},
			expected: "one linear unit",
		},
		{
			name: "a model authored in a unit the schema has no SI spelling for",
			files: map[string]string{
				"registry.dfc":      strings.Replace(exportRegistry, "(unit m)", "(unit ft)", 1),
				"entities/site.dfc": exportEntities,
			},
			expected: "SI spelling",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, root, stderr := exporting(t, exitCheck, testCase.files)

			assert.False(t, result.Derived)
			assert.Empty(t, result.Files)
			assert.Empty(t, result.Schema)
			assert.Contains(t, stderr, testCase.expected)

			digest, err := dfcad.DigestOf(root)
			require.NoError(t, err)
			assert.Equal(t, digest.String(), result.Digest,
				"a refusal is still about a tree, and saying which one is the point")

			assert.NoDirExists(t, filepath.Join(root, dfcad.BuildDir, "export"),
				"an artefact is all or nothing, and nothing was produced")
		})
	}
}

// TestExportedRefusesAModelWhichPinsNoURL is its own function, and it is
// driven below the command rather than through it, because a model with no
// project declaration does not load at all: the command never reaches this.
//
// It is still where the refusal belongs. Every object in an IFC file carries a
// GlobalId, there is nothing to derive one from without the pinned URL, and an
// export which invented identifiers would be one whose second run re-created
// every object in the receiving system.
func TestExportedRefusesAModelWhichPinsNoURL(t *testing.T) {
	root := tree(t, map[string]string{
		"registry.dfc": strings.Replace(exportRegistry,
			"\n  (globalid-namespace \"https://example.org/models/riverside\")", "", 1),
		"entities/site.dfc": exportEntities,
	})

	graph, _ := dfcad.LoadGraph(root)

	_, manifest, diags := exported(graph, dfcad.DerivationEpoch(dfcad.Digest{}))

	assert.Empty(t, manifest)
	require.Len(t, diags, 1)
	assert.Equal(t, dfcad.SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, "GlobalId")
	assert.Contains(t, diags[0].Hint, "globalid-namespace")
}

// TestRunExportRefusesExtraArguments is its own function because it is about
// the shape of the invocation, which is the one thing the usage message
// documents.
func TestRunExportRefusesExtraArguments(t *testing.T) {
	root := tree(t, exportModel())

	stdout, stderr := invoke(t, exitUsage, root, "export", "site:S-101")

	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "site:S-101")
}

// TestExportDestinationHoldsTheAuthoredTreeClosed is its own function because
// it exercises the rule directly, over paths a command line test would need a
// tree on disk for each of.
func TestExportDestinationHoldsTheAuthoredTreeClosed(t *testing.T) {
	root := t.TempDir()

	testCases := []struct {
		name     string
		out      string
		expected bool
	}{
		{name: "refuses the model root", out: "model.ifc", expected: false},
		{name: "refuses a subdirectory of it", out: "entities/model.ifc", expected: false},
		{name: "refuses a path which walks out and back in", out: "../" + filepath.Base(root) + "/model.ifc", expected: false},
		{name: "allows the build directory", out: ".dfcad/model.ifc", expected: true},
		{name: "allows a directory beneath the build directory", out: ".dfcad/export/model.ifc", expected: true},
		{name: "allows a path outside the root", out: filepath.Join(t.TempDir(), "model.ifc"), expected: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := exportDestination(&globals{Root: root}, testCase.out)

			if testCase.expected {
				assert.NoError(t, err)
				return
			}

			var got DestinationInsideModelError
			require.ErrorAs(t, err, &got)
			assert.Equal(t, root, got.Root)
		})
	}
}
