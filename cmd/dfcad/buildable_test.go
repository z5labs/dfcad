// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// deriving is the vocabulary every invocation below is asked in: which
// predicate carries a setback, which carries a position, and how close two
// corners are one corner.
func deriving(args ...string) []string {
	return append([]string{
		"buildable",
		"--setback", "setback",
		"--position", "position",
		"--tolerance", "coincident",
	}, args...)
}

// derived runs buildable against a fixture and returns what it wrote, requiring
// the run to have exited with the code given.
func derived(t *testing.T, expectedCode int, files map[string]string, args ...string) (buildableResult, string) {
	t.Helper()

	root := tree(t, files)
	stdout, stderr := invoke(t, expectedCode, root, deriving(args...)...)

	return listed[buildableResult](t, stdout), stderr
}

func TestRunBuildable(t *testing.T) {
	result, _ := derived(t, exitSuccess, model(), "site:P-01")

	assert.Equal(t, "buildable", result.Command)
	assert.Equal(t, "site:P-01", result.Subject)
	assert.True(t, result.Derived)
	assert.Equal(t, "frame:building", result.Frame)
	assert.Equal(t, "m", result.Unit)

	require.NotNil(t, result.Tolerance)
	assert.Equal(t, "coincident", result.Tolerance.Name)

	// Twenty by twelve, set back five at the road, three at the rear and two at
	// each flank: sixteen by four is what is left.
	require.NotNil(t, result.Parcel)
	require.NotNil(t, result.Region)
	assert.InDelta(t, 240.0, result.Parcel.Area, 0.25)
	assert.InDelta(t, 64.0, result.Region.Area, 0.25)
	assert.False(t, result.Region.Empty)
	require.Len(t, result.Region.Pieces, 1)
	assert.Len(t, result.Region.Pieces[0].Outer, 4)

	// One setback per edge, each naming the edge it was claimed on and the
	// evidence behind it.
	require.Len(t, result.Setbacks, 4)
	assert.Equal(t,
		[]string{"geom:E-11", "geom:E-12", "geom:E-13", "geom:E-14"},
		[]string{
			result.Setbacks[0].Edge, result.Setbacks[1].Edge,
			result.Setbacks[2].Edge, result.Setbacks[3].Edge,
		},
	)
	assert.Equal(t, 5.0, result.Setbacks[0].Distance)
	assert.Equal(t, "m", result.Setbacks[0].Unit)
	assert.Contains(t, result.Setbacks[0].Source, "Planning consent PC-2026-014")

	// The accuracy of the answer follows from both families of claim, which is
	// what says it was derived from both.
	require.NotNil(t, result.Budget)
	assert.NotEmpty(t, result.Budget.Terms)
	require.NotNil(t, result.Budget.Combined)
	assert.Positive(t, result.Budget.Combined.Magnitude)
	assert.Empty(t, result.Budget.From, "this budget is a computation rather than a route between frames")
}

// TestRunBuildableFollowsTheClaim is its own function because it asserts about
// two runs over two models rather than about one: the region has to follow the
// setback, and the only way to see that is to change the setback and nothing
// else.
func TestRunBuildableFollowsTheClaim(t *testing.T) {
	before, _ := derived(t, exitSuccess, model(), "site:P-01")

	files := model()
	files["entities/geometry.dfc"] = strings.Replace(
		files["entities/geometry.dfc"], "(value 5.0 m)", "(value 7.0 m)", 1)
	require.NotEqual(t, model()["entities/geometry.dfc"], files["entities/geometry.dfc"],
		"the frontage setback is the one thing which changed")

	after, _ := derived(t, exitSuccess, files, "site:P-01")

	assert.InDelta(t, 64.0, before.Region.Area, 0.25)
	assert.InDelta(t, 32.0, after.Region.Area, 0.25, "two metres more frontage over a sixteen metre width")
	assert.InDelta(t, before.Parcel.Area, after.Parcel.Area, 0.25, "and the plot itself did not move")
}

// TestRunBuildableReportsTheDigestItWasDerivedFrom is its own function because
// it is about the provenance of a derivation rather than about a region: a
// derived value which cannot say which tree it came from is one nobody can
// check against the tree in front of them
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
func TestRunBuildableReportsTheDigestItWasDerivedFrom(t *testing.T) {
	files := model()
	root := tree(t, files)

	stdout, _ := invoke(t, exitSuccess, root, deriving("site:P-01")...)
	result := listed[buildableResult](t, stdout)

	t.Run("names the digest of the source tree", func(t *testing.T) {
		digest, err := dfcad.DigestOf(root)
		require.NoError(t, err)
		require.True(t, digest.Known())

		assert.Equal(t, digest.String(), result.Digest)
	})

	t.Run("names a different one for a tree which moved", func(t *testing.T) {
		moved := model()
		moved["entities/geometry.dfc"] = strings.Replace(
			moved["entities/geometry.dfc"], "(value 5.0 m)", "(value 7.0 m)", 1)

		after, _ := derived(t, exitSuccess, moved, "site:P-01")

		assert.NotEqual(t, result.Digest, after.Digest)
		assert.NotEqual(t, result.Region.Area, after.Region.Area,
			"the answer moved with the tree, which is what the digest is of")
	})
}

// TestABuildableOfATreeWithNoDigestWritesNoDigestField is its own function
// because it asserts about the bytes rather than about a figure: a digest of
// nothing would key a derivation to a tree nobody could produce, so where there
// is none the field is absent rather than empty.
func TestABuildableOfATreeWithNoDigestWritesNoDigestField(t *testing.T) {
	// A graph assembled from no tree at all has no digest, which is the state a
	// model that was never read from disk leaves behind.
	result := reportBuildable(command{name: "buildable"}, nil, "site:P-01", dfcad.Buildable{}, false)

	assert.Empty(t, result.Digest)

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), `"digest"`)
}

// TestRunBuildableConsumedByItsSetbacks is its own function because it is the
// one successful run which answers with nothing: an empty region is the answer
// to the question rather than a failure to answer it.
func TestRunBuildableConsumedByItsSetbacks(t *testing.T) {
	files := model()
	files["entities/geometry.dfc"] = strings.ReplaceAll(
		files["entities/geometry.dfc"], "(value 5.0 m)", "(value 9.0 m)")

	result, stderr := derived(t, exitSuccess, files, "site:P-01")

	assert.True(t, result.Derived, "it was derived")
	require.NotNil(t, result.Region)
	assert.True(t, result.Region.Empty, "and there is nothing on it")
	assert.Zero(t, result.Region.Area)
	assert.Empty(t, result.Region.Pieces, "no inside-out shape came back instead")

	assert.Len(t, result.Setbacks, 4, "the rule which consumed it is still reported")
	assert.Contains(t, stderr, "leave nothing buildable")
}

// TestRunBuildableRefusesWhatItCannotRead is its own function because every case
// in it exits with a check failure and carries no region.
func TestRunBuildableRefusesWhatItCannotRead(t *testing.T) {
	testCases := []struct {
		name             string
		edit             func(files map[string]string)
		expectedInStderr []string
	}{
		{
			name: "names the edge a setback was needed for rather than reading the silence as nought",
			edit: func(files map[string]string) {
				files["entities/geometry.dfc"] = strings.Replace(
					files["entities/geometry.dfc"], `(setback
    (value 3.0 m)
    (source "Planning consent PC-2026-014, condition 3")
    (method method:statutory-instrument)
    (accuracy (independent 0.01 m))
    (date "2026-04-02"))`, "", 1)
			},
			expectedInStderr: []string{"geom:E-13", "found none on"},
		},
		{
			name: "refuses a setback written outwards",
			edit: func(files map[string]string) {
				files["entities/geometry.dfc"] = strings.Replace(
					files["entities/geometry.dfc"], "(value 5.0 m)", "(value -5.0 m)", 1)
			},
			expectedInStderr: []string{"geom:E-11", "a distance inwards from the boundary"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			files := model()
			testCase.edit(files)

			result, stderr := derived(t, exitCheck, files, "site:P-01")

			assert.False(t, result.Derived)
			assert.Equal(t, "site:P-01", result.Subject, "a refusal still says which question it answers")
			assert.NotEmpty(t, result.Digest, "and which tree it was asked of")
			assert.Nil(t, result.Region, "a derivation which was not made carries no region")
			assert.Empty(t, result.Setbacks)

			for _, expected := range testCase.expectedInStderr {
				assert.Contains(t, stderr, expected)
			}
		})
	}
}

// TestRunBuildableRefusesAnInvocationWithoutTheVocabulary is its own function
// because it is about what the run was asked rather than about what the model
// says: nothing here loads a model at all.
func TestRunBuildableRefusesAnInvocationWithoutTheVocabulary(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedInStderr []string
	}{
		{
			name:             "names every flag it was not given at once",
			args:             []string{"buildable", "site:P-01"},
			expectedInStderr: []string{"--setback", "--position", "--tolerance"},
		},
		{
			name:             "names the one flag it was not given",
			args:             []string{"buildable", "--position", "position", "--tolerance", "coincident", "site:P-01"},
			expectedInStderr: []string{"--setback"},
		},
		{
			name:             "refuses a run with nothing to derive",
			args:             deriving(),
			expectedInStderr: []string{ErrMissingSubject.Error()},
		},
		{
			name:             "refuses an id which names a shape rather than a thing",
			args:             deriving("geom:L-11"),
			expectedInStderr: []string{"geom:L-11", "loop"},
		},
		{
			name:             "refuses an id nothing in the model holds",
			args:             deriving("site:P-99"),
			expectedInStderr: []string{"site:P-99"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, model())
			stdout, stderr := invoke(t, exitUsage, root, testCase.args...)

			assert.Empty(t, stdout, "a run which asked no question writes no result")
			for _, expected := range testCase.expectedInStderr {
				assert.Contains(t, stderr, expected)
			}
		})
	}
}

// TestRunBuildableHumanOutputNeverChangesStdout is its own function because it
// is about the one property the format flag must not have.
func TestRunBuildableHumanOutputNeverChangesStdout(t *testing.T) {
	root := tree(t, model())

	machine, machineReport := invoke(t, exitSuccess, root, deriving("site:P-01")...)
	human, humanReport := invoke(t, exitSuccess, root, deriving("site:P-01", "--format", formatHuman)...)
	loud, loudReport := invoke(t, exitSuccess, root, deriving("site:P-01", "--format", formatHuman, "-v")...)

	assert.Equal(t, machine, human)
	assert.Equal(t, machine, loud)

	assert.Empty(t, machineReport)
	assert.Contains(t, humanReport, "site:P-01: 64.0 m² buildable of 240.0 m²")
	assert.NotContains(t, humanReport, "geom:E-11: 5.0 m")
	assert.Contains(t, loudReport, "geom:E-11: 5.0 m")
}
