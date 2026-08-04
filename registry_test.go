// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sexpr "github.com/z5labs/sexpr-go"
)

// registryFixture is the root of one fixture registry set.
func registryFixture(name string) string { return filepath.Join("testdata", "registry", name) }

// loadRegistryFixture loads a fixture set and renders its diagnostics the way
// the command line interface would.
//
// The rendering is what is compared rather than the [Diagnostic] values because
// it holds every field of every one of them — the position, the message, the
// hint and each related location, against the source line each points at — and
// a reviewer can read what a fixture is meant to say without reconstructing it
// from struct literals.
func loadRegistryFixture(t *testing.T, name string) (*Registry, string) {
	t.Helper()

	registry, diags := LoadRegistry(registryFixture(name))

	var collected Diagnostics
	collected.Add(diags...)

	var rendered strings.Builder
	require.NoError(t, collected.Render(&rendered, FileSources{}))

	return registry, rendered.String()
}

// expectedRegistryDiagnostics returns the rendering held beside the fixture,
// having first rewritten it from got when -update was passed.
func expectedRegistryDiagnostics(t *testing.T, name string, got string) string {
	t.Helper()

	path := filepath.Join(registryFixture(name), "diagnostics.txt")
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

func TestLoadRegistry(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
	}{
		{
			name:    "names both declarations when a name is declared twice",
			fixture: "duplicates",
		},
		{
			name:    "names the entry and what the registry permits when a value is not one",
			fixture: "malformed",
		},
		{
			name:    "reports a form the tables do not permit and does not interpret it",
			fixture: "unknown-key",
		},
		{
			name:    "names both ends of a reference which does not resolve",
			fixture: "dangling",
		},
		{
			name:    "names every frame in a parent chain which never reaches a root",
			fixture: "cycle",
		},
		{
			name:    "names both root frames, and the frames which are half of a non-root",
			fixture: "roots",
		},
		{
			name:    "reports the missing project of a tree which declares no registry at all",
			fixture: "empty",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, got := loadRegistryFixture(t, testCase.fixture)

			assert.Equal(t, expectedRegistryDiagnostics(t, testCase.fixture, got), got)
		})
	}
}

// TestLoadRegistryAccepts is its own function because its assertion is the
// other shape: a registry which is well formed has nothing to render, and the
// thing worth reporting on a failure is the diagnostics themselves.
func TestLoadRegistryAccepts(t *testing.T) {
	registry, diags := LoadRegistry(registryFixture("valid"))

	for _, diagnostic := range diags {
		t.Errorf("unexpected diagnostic: %s", diagnostic)
	}

	require.NotNil(t, registry)
}

func TestRegistryDeclarations(t *testing.T) {
	registry, _ := LoadRegistry(registryFixture("valid"))

	t.Run("declares the project the GlobalId derivation reads", func(t *testing.T) {
		project, ok := registry.Project()

		require.True(t, ok)
		assert.Equal(t, "Riverside example", project.Label)
		assert.Equal(t, "https://example.org/models/riverside", project.GlobalIDNamespace)
		assert.Equal(t, "The registry half of the worked example.", project.Description)
	})

	t.Run("declares the permitted id namespaces and nothing else", func(t *testing.T) {
		assert.Equal(t, []string{"frame", "geom", "method", "site", "survey"}, registry.Names(SortNamespace))

		namespace, ok := registry.Namespace("survey")

		require.True(t, ok)
		assert.Equal(t, "Claim ids and control points issued by Acme Surveys.", namespace.Description)
	})

	t.Run("declares a type with its kinds, its geometry forms and its invariants", func(t *testing.T) {
		declared, ok := registry.Type("MeetingRoom")

		require.True(t, ok)
		assert.Equal(t, []Kind{KindSpace}, declared.Kinds)
		assert.Equal(t, []Geometry{GeometryArea}, declared.Geometries)
		assert.True(t, declared.Absent)
		assert.Equal(t, "An enclosed room used for meetings.", declared.Description)

		require.Len(t, declared.Invariants, 1)
		assert.Equal(t, "boundary-loops-close", declared.Invariants[0].Check)
		assert.Len(t, declared.Invariants[0].Parameters, 1)
	})

	t.Run("reports which kind and which geometry form a type permits", func(t *testing.T) {
		declared, ok := registry.Type("Partition")

		require.True(t, ok)
		assert.True(t, declared.PermitsKind(KindElement))
		assert.True(t, declared.PermitsKind(KindInterface))
		assert.False(t, declared.PermitsKind(KindSpace))
		assert.True(t, declared.PermitsGeometry(GeometryLine))
		assert.False(t, declared.PermitsGeometry(GeometryArea))
		assert.False(t, declared.Absent)
	})

	t.Run("declares a predicate with its unit, its shape and its dimension", func(t *testing.T) {
		predicate, ok := registry.Predicate("position")

		require.True(t, ok)
		assert.Equal(t, Unit("m"), predicate.Unit)
		assert.Equal(t, ShapeCoordinate, predicate.Shape)
		assert.Equal(t, 3, predicate.Dimension)
	})

	t.Run("takes a predicate as claim-bearing and not strict unless it says otherwise", func(t *testing.T) {
		transform, ok := registry.Predicate("frame-transform")

		require.True(t, ok)
		assert.True(t, transform.ClaimBearing)
		assert.False(t, transform.Strict)
		assert.Equal(t, Unit(""), transform.Unit)

		colour, ok := registry.Predicate("colour")

		require.True(t, ok)
		assert.False(t, colour.ClaimBearing)

		width, ok := registry.Predicate("width")

		require.True(t, ok)
		assert.True(t, width.Strict)
	})

	t.Run("declares a frame with its linear unit, its parent and its claims", func(t *testing.T) {
		building, ok := registry.Frame("frame:building")

		require.True(t, ok)
		assert.Equal(t, "Building local grid", building.Label)
		assert.Equal(t, Unit("m"), building.Unit)
		assert.Equal(t, "frame:survey-grid", building.Parent)
		assert.Equal(t, "survey:C-0031", building.Transform)
		assert.Len(t, building.Claims, 1)

		root, ok := registry.Frame("frame:survey-grid")

		require.True(t, ok)
		assert.Empty(t, root.Parent)
		assert.Empty(t, root.Transform)
		assert.Empty(t, root.Claims)
	})

	t.Run("declares a tolerance with its value and its unit", func(t *testing.T) {
		tolerance, ok := registry.Tolerance("boundary-closure")

		require.True(t, ok)
		assert.Equal(t, 0.005, tolerance.Value)
		assert.Equal(t, Unit("m"), tolerance.Unit)
	})

	t.Run("iterates every registry in name order", func(t *testing.T) {
		var types []string
		for declared := range registry.Types() {
			types = append(types, declared.Name)
		}
		assert.Equal(t, []string{"MeetingRoom", "Partition"}, types)

		var predicates []string
		for predicate := range registry.Predicates() {
			predicates = append(predicates, predicate.Name)
		}
		assert.Equal(t, []string{"colour", "frame-transform", "position", "width"}, predicates)

		var frames []string
		for frame := range registry.Frames() {
			frames = append(frames, frame.ID)
		}
		assert.Equal(t, []string{"frame:building", "frame:survey-grid"}, frames)

		var namespaces []string
		for namespace := range registry.Namespaces() {
			namespaces = append(namespaces, namespace.Name)
		}
		assert.Equal(t, []string{"frame", "geom", "method", "site", "survey"}, namespaces)

		var tolerances []string
		for tolerance := range registry.Tolerances() {
			tolerances = append(tolerances, tolerance.Name)
		}
		assert.Equal(t, []string{"boundary-closure"}, tolerances)
	})
}

// TestLoadRegistryIsIndependentOfFileOrder checks that a registry set says the
// same thing however its files are reached.
//
// A frame declared in the last file the walk reaches is as declared as one in
// the first, which is the whole reason references are resolved in a second
// pass. Loading the same tree through one of its files at a time is what would
// catch a resolution which had crept back into the reading pass.
func TestLoadRegistryIsIndependentOfFileOrder(t *testing.T) {
	whole, diags := LoadRegistry(registryFixture("valid"))
	require.Empty(t, diags)

	var paths []string
	for path, err := range Walk(registryFixture("valid")) {
		require.NoError(t, err)
		paths = append(paths, path)
	}
	require.Len(t, paths, 2)

	// Read in reverse, one file at a time. Each file on its own is missing what
	// the other declares, so what is compared is the set of names rather than
	// the diagnostics.
	declared := make(map[Sort][]string)
	for _, path := range slices.Backward(paths) {
		partial, _ := LoadRegistry(path)
		for _, sort := range []Sort{SortNamespace, SortType, SortPredicate, SortFrame, SortTolerance} {
			declared[sort] = append(declared[sort], partial.Names(sort)...)
		}
	}

	for sort, names := range declared {
		slices.Sort(names)
		assert.Equal(t, whole.Names(sort), names, "the %s registry is the same set whichever file it was read from", sort)
	}
}

// TestEmptyRegistry is its own function because it is the case with no
// declarations to assert on at all: what it checks is that everything answers
// rather than crashes.
//
// A source tree whose registry has not been written yet is the first state
// every consuming repository is in. Every node in it is invalid, and each one
// has to be able to say so.
func TestEmptyRegistry(t *testing.T) {
	registry, diags := LoadRegistry(registryFixture("empty"))

	require.NotNil(t, registry)
	require.Len(t, diags, 1, "the only thing wrong with an empty registry set is that it declares no project")

	_, hasProject := registry.Project()
	assert.False(t, hasProject)

	for _, sort := range []Sort{SortNamespace, SortType, SortPredicate, SortFrame, SortTolerance} {
		assert.Empty(t, registry.Names(sort))
		assert.False(t, registry.Declares(sort, "anything"))
	}

	// Every registry name the fixture's nodes are written with is undeclared,
	// and each says so with a position and with what is declared instead.
	file, err := LoadFile(filepath.Join(registryFixture("empty"), "entities.dfc"))
	require.NoError(t, err)

	var reported []string
	for _, node := range file.Nodes {
		for _, child := range childForms(node, "type") {
			written, ok := argument(child, 0)
			require.True(t, ok)

			name, ok := written.Datum.(sexpr.Symbol)
			require.True(t, ok)

			diagnostic := registry.Undeclared(SortType, name.Value, written.Span)
			assert.Equal(t, SeverityError, diagnostic.Severity)
			assert.NotEmpty(t, diagnostic.Hint)
			reported = append(reported, diagnostic.String())
		}
	}

	assert.Equal(t, []string{
		filepath.Join(registryFixture("empty"), "entities.dfc") +
			":7:9: error: expected a declared type, found MeetingRoom, which no registry file declares",
	}, reported)
}

func TestLoadRegistryUnreadableRoot(t *testing.T) {
	testCases := []struct {
		name string
		root string
	}{
		{
			name: "reports a root which does not exist rather than failing silently",
			root: filepath.Join("testdata", "registry", "no-such-directory"),
		},
		{
			name: "reports a file which does not exist",
			root: filepath.Join("testdata", "registry", "no-such-file.dfc"),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			registry, diags := LoadRegistry(testCase.root)

			require.NotNil(t, registry)
			assert.NotEmpty(t, diags)
			assert.Empty(t, registry.Names(SortType))
		})
	}
}

func TestRegistryUndeclared(t *testing.T) {
	registry, _ := LoadRegistry(registryFixture("valid"))

	testCases := []struct {
		name     string
		registry *Registry
		sort     Sort
		written  string
		message  string
		hint     string
	}{
		{
			name:     "offers the declared name a misspelling is nearest to",
			registry: registry,
			sort:     SortType,
			written:  "MeetingRoon",
			message:  "expected a declared type, found MeetingRoon, which no registry file declares",
			hint:     "did you mean MeetingRoom?",
		},
		{
			name:     "lists the declared set when nothing is close enough to suggest",
			registry: registry,
			sort:     SortNamespace,
			written:  "parcel",
			message:  "expected a declared namespace, found parcel, which no registry file declares",
			hint:     "the declared namespaces are frame, geom, method, site and survey",
		},
		{
			name:     "says the set is empty rather than listing nothing",
			registry: &Registry{},
			sort:     SortTolerance,
			written:  "boundary-closure",
			message:  "expected a declared tolerance, found boundary-closure, which no registry file declares",
			hint:     "no tolerance is declared; a registry file declares one with (tolerance ...)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := testCase.registry.Undeclared(testCase.sort, testCase.written, Span{})

			assert.Equal(t, SeverityError, got.Severity)
			assert.Equal(t, testCase.message, got.Message)
			assert.Equal(t, testCase.hint, got.Hint)
		})
	}
}

// TestNilRegistry checks the registry a caller which did not read the
// diagnostics is holding. Every query answers "nothing is declared" rather than
// panicking, which is the same answer an empty registry gives.
func TestNilRegistry(t *testing.T) {
	var registry *Registry

	_, ok := registry.Project()
	assert.False(t, ok)

	_, ok = registry.Type("MeetingRoom")
	assert.False(t, ok)

	_, ok = registry.Predicate("width")
	assert.False(t, ok)

	_, ok = registry.Namespace("site")
	assert.False(t, ok)

	_, ok = registry.Frame("frame:building")
	assert.False(t, ok)

	_, ok = registry.Tolerance("boundary-closure")
	assert.False(t, ok)

	assert.Empty(t, registry.Names(SortType))
	assert.Empty(t, registry.Names("unknown sort"))
	assert.False(t, registry.Declares(SortType, "MeetingRoom"))

	for range registry.Types() {
		t.Error("a nil registry declares no type")
	}
}

// TestClosedSets pins the two vocabularies specification section 1 compiles
// into the engine.
//
// They are here rather than as registry data because a kind is a structural
// axis every consuming repository shares. A member added to either is a change
// to the specification, and this is what says so out loud.
func TestClosedSets(t *testing.T) {
	assert.Equal(t, []Kind{
		KindZone, KindSite, KindBuilding, KindStorey, KindSpace, KindElement, KindInterface,
	}, Kinds())

	assert.Equal(t, []Geometry{
		GeometryPoint, GeometryLine, GeometryArea, GeometrySurface, GeometrySolid,
	}, Geometries())

	assert.Equal(t, []Shape{
		ShapeScalar, ShapeCoordinate, ShapeTransform, ShapeText,
	}, Shapes())

	// The sets come back by value, so a caller cannot edit the engine's
	// vocabulary by editing what it was handed.
	Kinds()[0] = "Sproket"
	assert.Equal(t, KindZone, Kinds()[0])
}

func TestSplitID(t *testing.T) {
	testCases := []struct {
		name      string
		id        string
		namespace string
		local     string
		ok        bool
	}{
		{
			name:      "splits on the colon",
			id:        "site:S-101",
			namespace: "site",
			local:     "S-101",
			ok:        true,
		},
		{
			name:      "splits on the first colon only, because a local part may hold more",
			id:        "survey:2026:CP-3",
			namespace: "survey",
			local:     "2026:CP-3",
			ok:        true,
		},
		{
			name: "rejects a symbol with no colon in it",
			id:   "S-101",
		},
		{
			name: "rejects an empty namespace",
			id:   ":S-101",
		},
		{
			name: "rejects an empty local part",
			id:   "site:",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			namespace, local, ok := splitID(testCase.id)

			assert.Equal(t, testCase.ok, ok)
			assert.Equal(t, testCase.namespace, namespace)
			assert.Equal(t, testCase.local, local)
		})
	}
}

func TestWellFormedNamespace(t *testing.T) {
	testCases := []struct {
		name      string
		namespace string
		want      bool
	}{
		{
			name:      "accepts letters, digits, hyphens and underscores after a letter",
			namespace: "acme-survey_2026",
			want:      true,
		},
		{
			name:      "rejects a namespace which begins with a digit",
			namespace: "3d",
		},
		{
			name:      "rejects a namespace holding punctuation a symbol permits and a namespace does not",
			namespace: "site.local",
		},
		{
			name:      "rejects a namespace which is not ASCII",
			namespace: "gebäude",
		},
		{
			name:      "rejects an empty namespace",
			namespace: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, wellFormedNamespace(testCase.namespace))
		})
	}
}
