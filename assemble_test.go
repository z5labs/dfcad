// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closureTolerance is the tolerance every fixture below declares, and is a name
// rather than a number everywhere it appears — here as much as in the engine
// ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
const closureTolerance = "boundary-closure"

// assembleAll assembles every loop of a fixture, in the order the walk read
// them, and renders what the assemblies had to say.
//
// Every loop is assembled rather than one named loop, because a fixture holding
// three shapes which are not a ring should report on three of them: a helper
// which took a name would have to be told which loops to look at, and a loop
// nobody remembered to name would be a case which silently stopped being tested.
func assembleAll(t *testing.T, name string) (map[ID]Assembly, string) {
	t.Helper()

	model := loadBoundaryModel(t, name)

	assemblies := make(map[ID]Assembly)

	var diags []Diagnostic
	for loop := range model.topology.Loops() {
		assembly, found := model.topology.Assemble(loop, model.positions, closureTolerance, model.registry)

		assemblies[loop.ID()] = assembly
		diags = append(diags, found...)
	}

	return assemblies, renderBoundaryDiagnostics(t, diags)
}

func TestAssemble(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
	}{
		{
			name:    "names both corners of a gap and how far apart they are",
			fixture: "closure",
		},
		{
			name:    "says the edges form a ring written in the wrong order, which is not a gap",
			fixture: "out-of-order",
		},
		{
			name:    "names a branch, an edge traversed twice and edges forming more than one ring",
			fixture: "not-a-simple-cycle",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, got := assembleAll(t, testCase.fixture)

			assert.Equal(t, expectedBoundaryDiagnostics(t, testCase.fixture, got), got)
		})
	}
}

// TestAssembleClosesARingThroughSharedVertices is its own function because the
// assertion is the assembled ring rather than a diagnostic: which edge the
// traversal runs through at each step, and which way round it runs.
func TestAssembleClosesARingThroughSharedVertices(t *testing.T) {
	model := loadBoundaryModel(t, "valid")

	room, ok := model.topology.Loop("geom:L-01")
	require.True(t, ok)

	assembly, diags := model.topology.Assemble(room, model.positions, closureTolerance, model.registry)
	require.Empty(t, renderBoundaryDiagnostics(t, diags), "the room's boundary closes")

	t.Run("closes, and says which tolerance said so", func(t *testing.T) {
		assert.True(t, assembly.Closed())
		assert.Equal(t, closureTolerance, assembly.Tolerance().Name)
		assert.Equal(t, 0.005, assembly.Tolerance().Value)
		assert.Equal(t, Unit("m"), assembly.Tolerance().Unit)
	})

	t.Run("walks each edge from the corner the last one ended at", func(t *testing.T) {
		steps := assembly.Steps()
		require.Len(t, steps, 4)

		for i, step := range steps {
			assert.Equal(t, steps[(i+3)%4].To(), step.From(), "step %d begins where step %d ended", i, (i+3)%4)
		}
	})

	t.Run("traverses a shared edge the other way round from the other side of it", func(t *testing.T) {
		corridor, ok := model.topology.Loop("geom:L-02")
		require.True(t, ok)

		other, diags := model.topology.Assemble(corridor, model.positions, closureTolerance, model.registry)
		require.Empty(t, renderBoundaryDiagnostics(t, diags), "the corridor's boundary closes")
		require.True(t, other.Closed())

		// One edge, one identity, two directions. The room runs through the
		// partition the way it was written and the corridor runs through it the
		// other way, which is what the sharing costs and what a second copy of
		// the wall would have hidden.
		assert.Equal(t, Step{
			edge:     stepThrough(t, assembly, "geom:E-02").Edge(),
			from:     "geom:V-02",
			to:       "geom:V-03",
			reversed: false,
		}, stepThrough(t, assembly, "geom:E-02"))

		assert.Equal(t, Step{
			edge:     stepThrough(t, other, "geom:E-02").Edge(),
			from:     "geom:V-03",
			to:       "geom:V-02",
			reversed: true,
		}, stepThrough(t, other, "geom:E-02"))

		assert.Same(t, stepThrough(t, assembly, "geom:E-02").Edge(), stepThrough(t, other, "geom:E-02").Edge())
	})
}

// stepThrough is the step of an assembly which runs through the named edge.
func stepThrough(t *testing.T, assembly Assembly, id ID) Step {
	t.Helper()

	for _, step := range assembly.Steps() {
		if step.Edge().ID() == id {
			return step
		}
	}

	t.Fatalf("the assembly of %s runs through no edge %s", assembly.Loop().ID(), id)
	return Step{}
}

// TestAssembleClosesTwoCornersWithinTheTolerance is its own function because the
// two loops it compares differ in one thing only — how far apart the two corners
// which should meet are — and the tolerance is what decides between them.
//
// It is the case which makes the tolerance registry data rather than a constant:
// four millimetres is one corner surveyed twice by two crews on a project which
// says so, and a quarter of a metre is an edge naming the wrong corner on the
// same project.
func TestAssembleClosesTwoCornersWithinTheTolerance(t *testing.T) {
	assemblies, _ := assembleAll(t, "closure")

	t.Run("closes a ring whose ends are nearer than the tolerance", func(t *testing.T) {
		assert.True(t, assemblies["geom:L-12"].Closed())
	})

	t.Run("does not close a ring whose ends are further apart than it", func(t *testing.T) {
		assert.False(t, assemblies["geom:L-11"].Closed())
	})
}

// TestAssembleWithoutADeclaredTolerance is its own function because its
// assertions are a different shape: nothing is measured, so what is checked is
// the diagnostic and the identity-only answer it leaves behind.
func TestAssembleWithoutADeclaredTolerance(t *testing.T) {
	model := loadBoundaryModel(t, "closure")

	t.Run("reports a tolerance no registry file declares", func(t *testing.T) {
		loop, ok := model.topology.Loop("geom:L-12")
		require.True(t, ok)

		assembly, diags := model.topology.Assemble(loop, model.positions, "no-such-tolerance", model.registry)

		require.Len(t, diags, 2)
		assert.Equal(t, SeverityError, diags[0].Severity)
		assert.Contains(t, diags[0].Message, "no-such-tolerance")

		// Closure falls back to nothing. The ring which needed four millimetres
		// of slack does not get it, because there is no default and no fallback
		// tolerance anywhere — so it is reported as the open ring it is, with
		// the four millimetres measured and named.
		assert.Equal(t, Tolerance{}, assembly.Tolerance())
		assert.False(t, assembly.Closed())
		assert.Contains(t, diags[1].Message, "found a gap of 0.004 m")

		// And the hint names no tolerance, because none was applied. Offering
		// one here would send whoever reads it to check a number which had no
		// part in the answer.
		assert.NotContains(t, diags[1].Hint, "tolerance")
	})

	t.Run("closes a ring which meets itself at one vertex without measuring anything", func(t *testing.T) {
		model := loadBoundaryModel(t, "valid")

		loop, ok := model.topology.Loop("geom:L-01")
		require.True(t, ok)

		assembly, diags := model.topology.Assemble(loop, nil, "no-such-tolerance", model.registry)

		require.Len(t, diags, 1)
		assert.True(t, assembly.Closed(), "the corners are the same vertices, which needs no tolerance")
	})
}

// TestAssembleNothing is its own function because a nil loop is not a loop with
// something wrong with it: there is nothing to report on and nothing to report
// it against.
func TestAssembleNothing(t *testing.T) {
	model := loadBoundaryModel(t, "valid")

	assembly, diags := model.topology.Assemble(nil, model.positions, closureTolerance, model.registry)

	assert.Empty(t, diags)
	assert.Nil(t, assembly.Loop())
	assert.False(t, assembly.Closed())
	assert.Empty(t, assembly.Steps())
}
