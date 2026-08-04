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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// claimFixture is the root of one fixture model: a registry and the claims
// judged against it.
func claimFixture(name string) string { return filepath.Join("testdata", "claim", name) }

// loadClaimFixture loads a fixture model and renders the claim diagnostics the
// way the command line interface would.
//
// The registry's own diagnostics are asserted empty rather than rendered. Every
// fixture here declares a registry which loads clean, so that what the golden
// beside it holds is what this layer had to say and nothing else.
func loadClaimFixture(t *testing.T, name string) (*Claims, string) {
	t.Helper()

	registry, registryDiags := LoadRegistry(claimFixture(name))
	require.Empty(t, registryDiags, "the fixture registry loads clean")

	claims, diags := LoadClaims(claimFixture(name), registry)

	var collected Diagnostics
	collected.Add(diags...)

	var rendered strings.Builder
	require.NoError(t, collected.Render(&rendered, FileSources{}))

	return claims, rendered.String()
}

// expectedClaimDiagnostics returns the rendering held beside the fixture,
// having first rewritten it from got when -update was passed.
func expectedClaimDiagnostics(t *testing.T, name string, got string) string {
	t.Helper()

	path := filepath.Join(claimFixture(name), "diagnostics.txt")
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

func TestLoadClaims(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
	}{
		{
			name:    "names a predicate no registry file declares, and its position",
			fixture: "undeclared-predicate",
		},
		{
			name:    "names the predicate, the shape it declares and what was found instead",
			fixture: "wrong-shape",
		},
		{
			name:    "names a unit the predicate does not declare rather than converting it",
			fixture: "wrong-unit",
		},
		{
			name:    "names a date which is not the one spelling of a date",
			fixture: "malformed-date",
		},
		{
			name:    "names the closed set a rank was reaching outside of",
			fixture: "unknown-rank",
		},
		{
			name:    "names both claims holding one id, in whichever files they are",
			fixture: "duplicate-id",
		},
		{
			name:    "names a reference to a claim which carries no id of its own",
			fixture: "dangling-reference",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, got := loadClaimFixture(t, testCase.fixture)

			assert.Equal(t, expectedClaimDiagnostics(t, testCase.fixture, got), got)
		})
	}
}

// TestLoadClaimsProvenance reads one claim in full, which is the whole point of
// the layer: retrieving a number gets where it came from, how it was obtained
// and how good it is with no second lookup.
func TestLoadClaimsProvenance(t *testing.T) {
	claims, rendered := loadClaimFixture(t, "valid")
	require.Empty(t, rendered, "the valid fixture loads clean")

	claim, ok := claims.Claim("survey:C-0210")
	require.True(t, ok)

	id, hasID := claim.ID()
	assert.Equal(t, ID("survey:C-0210"), id)
	assert.True(t, hasID)

	assert.Equal(t, ID("site:S-101"), claim.Subject())
	assert.Equal(t, "width", claim.Predicate())
	assert.Equal(t, "Plan set A-101, sheet 3", claim.Source())
	assert.Equal(t, ID("method:scaled-from-plan"), claim.Method())
	assert.Equal(t, time.Date(2026, time.January, 9, 0, 0, 0, 0, time.UTC), claim.Date())
	assert.Equal(t, RankNormal, claim.Rank())

	value := claim.Value()
	assert.Equal(t, ShapeScalar, value.Shape())
	assert.Equal(t, Unit("m"), value.Unit())

	number, isScalar := value.Scalar()
	require.True(t, isScalar)
	assert.Equal(t, 8.5, number)

	accuracy, hasAccuracy := claim.Accuracy()
	require.True(t, hasAccuracy)
	assert.True(t, claim.Rankable())
	assert.Equal(t, []AccuracyTerm{
		{
			Kind:      TermIndependent,
			Magnitude: 0.05,
			Unit:      "m",
			Span:      accuracy.Terms[0].Span,
		},
	}, accuracy.Terms)

	superseded, isSuperseded := claim.SupersededBy()
	assert.False(t, isSuperseded)
	assert.Empty(t, superseded)

	assert.Equal(t, filepath.Join(claimFixture("valid"), "claims.dfc"), claim.Span().Start.Path)
}

// TestLoadClaimsShapes reads a claim of each of the four value shapes, which is
// what says the shapes describe the whole vocabulary rather than the part
// somebody happened to write a case for.
func TestLoadClaimsShapes(t *testing.T) {
	claims, rendered := loadClaimFixture(t, "valid")
	require.Empty(t, rendered)

	testCases := []struct {
		name      string
		subject   ID
		predicate string
		shape     Shape
		unit      Unit
	}{
		{
			name:      "reads a scalar and the unit it is expressed in",
			subject:   "site:S-101",
			predicate: "width",
			shape:     ShapeScalar,
			unit:      "m",
		},
		{
			name:      "reads a scalar of a non-dimensional predicate, written with no unit",
			subject:   "site:S-101",
			predicate: "occupancy",
			shape:     ShapeScalar,
		},
		{
			name:      "reads a string, which carries no unit",
			subject:   "site:S-101",
			predicate: "finish",
			shape:     ShapeText,
		},
		{
			name:      "reads a coordinate and the unit it is expressed in",
			subject:   "geom:V-02",
			predicate: "position",
			shape:     ShapeCoordinate,
			unit:      "m",
		},
		{
			name:      "reads a transform, which carries no unit",
			subject:   "frame:building",
			predicate: "frame-transform",
			shape:     ShapeTransform,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			read := slices.Collect(claims.Under(testCase.subject, testCase.predicate))
			require.NotEmpty(t, read)

			value := read[0].Value()

			assert.Equal(t, testCase.shape, value.Shape())
			assert.Equal(t, testCase.unit, value.Unit())
		})
	}

	t.Run("reads every shape the engine compiles in", func(t *testing.T) {
		var read []Shape
		for _, testCase := range testCases {
			if slices.Contains(read, testCase.shape) {
				continue
			}
			read = append(read, testCase.shape)
		}

		assert.ElementsMatch(t, Shapes(), read)
	})
}

// TestLoadClaimsReadsEachShapesValue checks that the value of each shape comes
// back through the accessor for that shape and through none of the others.
//
// The one accessor per shape is what keeps a coordinate from being read as a
// scalar by a caller which forgot to look at the predicate.
func TestLoadClaimsReadsEachShapesValue(t *testing.T) {
	claims, rendered := loadClaimFixture(t, "valid")
	require.Empty(t, rendered)

	t.Run("reads a coordinate in the order it was written", func(t *testing.T) {
		claim, ok := claims.Claim("survey:C-0181")
		require.True(t, ok)

		components, isCoordinate := claim.Value().Coordinate()
		require.True(t, isCoordinate)
		assert.Equal(t, []float64{4.05, 0.0, 0.0}, components)

		_, isScalar := claim.Value().Scalar()
		_, isText := claim.Value().Text()
		_, isTransform := claim.Value().Transform()
		assert.False(t, isScalar)
		assert.False(t, isText)
		assert.False(t, isTransform)
	})

	t.Run("copies the components, so that sorting them sorts nothing in the model", func(t *testing.T) {
		claim, ok := claims.Claim("survey:C-0181")
		require.True(t, ok)

		components, _ := claim.Value().Coordinate()
		slices.Sort(components)

		again, _ := claim.Value().Coordinate()
		assert.Equal(t, []float64{4.05, 0.0, 0.0}, again)
	})

	t.Run("reads a transform row by row, sorting nothing", func(t *testing.T) {
		claim, ok := claims.Claim("survey:C-0031")
		require.True(t, ok)

		transform, isTransform := claim.Value().Transform()
		require.True(t, isTransform)

		assert.Equal(t, [3]float64{401235.117, 3172884.902, 44.318}, transform.Translation)
		assert.Equal(t, [9]float64{
			0.9999985, -0.0017453, 0.0,
			0.0017453, 0.9999985, 0.0,
			0.0, 0.0, 1.0,
		}, transform.Rotation)
		assert.Equal(t, 1.0, transform.Scale)
	})

	t.Run("reads a string", func(t *testing.T) {
		read := slices.Collect(claims.Under("site:S-101", "finish"))
		require.Len(t, read, 1)

		text, isText := read[0].Value().Text()
		require.True(t, isText)
		assert.Equal(t, "slate", text)
	})

	t.Run("reads a scalar of a non-dimensional predicate", func(t *testing.T) {
		read := slices.Collect(claims.Under("site:S-101", "occupancy"))
		require.Len(t, read, 1)

		number, isScalar := read[0].Value().Scalar()
		require.True(t, isScalar)
		assert.Equal(t, 12.0, number)
		assert.Empty(t, read[0].Value().Unit())
	})
}

// TestLoadClaimsAccuracyIsOneSigma checks that both terms of an accuracy are
// read, and that the systematic one carries the id it is shared through.
//
// Two systematic terms are the same term when their ids are byte-equal, not
// when their magnitudes happen to match, so the id is the load-bearing part of
// the term rather than a note beside it.
func TestLoadClaimsAccuracyIsOneSigma(t *testing.T) {
	claims, rendered := loadClaimFixture(t, "valid")
	require.Empty(t, rendered)

	claim, ok := claims.Claim("survey:C-0181")
	require.True(t, ok)

	accuracy, hasAccuracy := claim.Accuracy()
	require.True(t, hasAccuracy)
	require.Len(t, accuracy.Terms, 2)

	assert.Equal(t, TermIndependent, accuracy.Terms[0].Kind)
	assert.Equal(t, 0.003, accuracy.Terms[0].Magnitude)
	assert.Equal(t, Unit("m"), accuracy.Terms[0].Unit)
	assert.Empty(t, accuracy.Terms[0].Source, "an independent error is shared with nothing")

	assert.Equal(t, TermSystematic, accuracy.Terms[1].Kind)
	assert.Equal(t, 0.008, accuracy.Terms[1].Magnitude)
	assert.Equal(t, Unit("m"), accuracy.Terms[1].Unit)
	assert.Equal(t, ID("survey:CP-3"), accuracy.Terms[1].Source)
}

// TestLoadClaimsWithoutAnAccuracy is its own function because it is the case
// the optional accuracy exists for rather than a variation on a table: a claim
// which does not know how good it is loads, says so, and is never given a
// number nobody wrote.
//
// A default here would be the one figure the claim exists to record, invented
// by this package. Unrankable is the honest reading, and it is visible as such
// rather than buried in a resolution that quietly preferred it.
func TestLoadClaimsWithoutAnAccuracy(t *testing.T) {
	claims, rendered := loadClaimFixture(t, "valid")
	require.Empty(t, rendered, "a claim with no accuracy is not a diagnostic")

	read := slices.Collect(claims.Under("site:S-101", "occupancy"))
	require.Len(t, read, 1)

	accuracy, hasAccuracy := read[0].Accuracy()

	assert.False(t, hasAccuracy)
	assert.False(t, read[0].Rankable())
	assert.Empty(t, accuracy.Terms, "no default term was invented")

	// The claim beside it in the same node writes one, so the absence is the
	// claim's and not the loader's.
	beside := slices.Collect(claims.Under("site:S-101", "width"))
	require.NotEmpty(t, beside)
	assert.True(t, beside[0].Rankable())
}

// TestLoadClaimsRankDefaultsToNormal checks the default the canonical printer
// leaves out, and that the closed set has exactly one other member.
func TestLoadClaimsRankDefaultsToNormal(t *testing.T) {
	claims, rendered := loadClaimFixture(t, "valid")
	require.Empty(t, rendered)

	t.Run("a claim which wrote no rank is normal", func(t *testing.T) {
		claim, ok := claims.Claim("survey:C-0210")
		require.True(t, ok)

		assert.Equal(t, RankNormal, claim.Rank())
	})

	t.Run("a claim which wrote deprecated names what replaced it", func(t *testing.T) {
		claim, ok := claims.Claim("survey:C-0104")
		require.True(t, ok)

		assert.Equal(t, RankDeprecated, claim.Rank())

		superseded, isSuperseded := claim.SupersededBy()
		require.True(t, isSuperseded)
		assert.Equal(t, ID("survey:C-0181"), superseded)
	})

	t.Run("there is no third rank", func(t *testing.T) {
		assert.Equal(t, []Rank{RankNormal, RankDeprecated}, Ranks())
	})
}

// TestLoadClaimsRepeatsArePermitted checks that more than one claim under one
// predicate on one subject is read as more than one claim.
//
// Two width claims on one node are two measurements, and the disagreement
// between them is the most valuable thing in the file. A loader which kept the
// last would delete the disagreement before anything could report it.
func TestLoadClaimsRepeatsArePermitted(t *testing.T) {
	claims, rendered := loadClaimFixture(t, "valid")
	require.Empty(t, rendered, "repeating a predicate is not a diagnostic")

	read := slices.Collect(claims.Under("site:S-101", "width"))
	require.Len(t, read, 2)

	first, _ := read[0].Value().Scalar()
	second, _ := read[1].Value().Scalar()

	assert.Equal(t, 8.5, first)
	assert.Equal(t, 8.53, second, "the claims are read in the order they were written")
	assert.NotEqual(t, first, second, "the two claims disagree, and both are kept")
}

// TestLoadClaimsIndexesBySubjectAndID checks that a load answers both "what
// does this model say about site:S-101" and "which claim is survey:C-0210"
// without walking the model.
func TestLoadClaimsIndexesBySubjectAndID(t *testing.T) {
	claims, rendered := loadClaimFixture(t, "valid")
	require.Empty(t, rendered)

	var read int
	for claim := range claims.All() {
		read++

		assert.Contains(t, slices.Collect(claims.Of(claim.Subject())), claim)

		id, hasID := claim.ID()
		if !hasID {
			continue
		}

		found, ok := claims.Claim(id)
		require.True(t, ok, "every claim which wrote an id is reachable by it")
		assert.Same(t, claim, found)
	}
	assert.Equal(t, claims.Len(), read, "All yields every claim once")

	_, ok := claims.Claim("survey:no-such-claim")
	assert.False(t, ok)

	assert.Empty(t, slices.Collect(claims.Of("site:no-such-node")))
	assert.Empty(t, slices.Collect(claims.Under("site:S-101", "no-such-predicate")))
}

// TestLoadClaimsWithoutAnIDAreStillRead checks that a claim which wrote no id
// is a claim.
//
// An id is required only of a claim something references, so the great majority
// of claims write none. A loader which needed one would be requiring a name for
// everything nothing points at.
func TestLoadClaimsWithoutAnIDAreStillRead(t *testing.T) {
	claims, rendered := loadClaimFixture(t, "valid")
	require.Empty(t, rendered)

	read := slices.Collect(claims.Under("site:S-101", "width"))
	require.Len(t, read, 2)

	id, hasID := read[1].ID()
	assert.False(t, hasID)
	assert.Empty(t, id)

	assert.Equal(t, "As-built check AB-2026-009, Acme Surveys", read[1].Source(), "the rest of the claim was still read")

	_, ok := claims.Claim("")
	assert.False(t, ok, "the zero id names nothing")
}

// TestLoadClaimsReturnsWhatItCouldRead checks that a claim whose value the
// registry rejects is still a claim.
//
// A caller reporting on a tree wants to say "site:S-101 claims a width from a
// plan set, in a unit width does not declare", and one which had been handed
// only the diagnostic could say only the second half of that.
func TestLoadClaimsReturnsWhatItCouldRead(t *testing.T) {
	claims, rendered := loadClaimFixture(t, "wrong-unit")
	require.NotEmpty(t, rendered)

	read := slices.Collect(claims.Under("site:S-101", "width"))
	require.Len(t, read, 2)

	assert.Equal(t, "Plan set A-101, sheet 3", read[0].Source())
	assert.Equal(t, ID("method:scaled-from-plan"), read[0].Method())
	assert.Equal(t, time.Date(2026, time.January, 9, 0, 0, 0, 0, time.UTC), read[0].Date())

	// The value is not read, because a value in a unit the predicate does not
	// declare is not a value anything may resolve. Nothing here converts it.
	assert.Equal(t, Shape(""), read[0].Value().Shape())
	assert.Empty(t, read[0].Value().Unit())

	_, isScalar := read[0].Value().Scalar()
	assert.False(t, isScalar)
}

// TestLoadClaimsWithoutARegistry checks the load a consuming repository whose
// registry has not been written yet gets: every claim names a predicate nothing
// declares, and says so with a position.
func TestLoadClaimsWithoutARegistry(t *testing.T) {
	claims, diags := LoadClaims(claimFixture("valid"), nil)

	require.Equal(t, 7, claims.Len(), "every claim is still read")

	var predicates int
	for _, diagnostic := range diags {
		assert.Equal(t, SeverityError, diagnostic.Severity)
		assert.NotEmpty(t, diagnostic.Span.Start.Path)

		if strings.Contains(diagnostic.Message, "expected a declared predicate") {
			predicates++
		}
	}

	assert.Equal(t, claims.Len(), predicates, "one undeclared predicate per claim")
}

// TestLoadClaimsIgnoresAPlainValue checks that a predicate written with a value
// and no children is left where it is.
//
// Whether that spelling is the right one is registry data — a non-claim-bearing
// predicate takes exactly that and a claim-bearing one does not — and choosing
// between them is the bare-scalar rule's rather than this pass's.
func TestLoadClaimsIgnoresAPlainValue(t *testing.T) {
	const registry = `(project (globalid-namespace "https://example.org/models/plain"))
(namespace site (description "Semantic nodes minted by this model."))
(type MeetingRoom (kind Space) (geometry area) (description "An enclosed room."))
(predicate colour (shape text) (claim-bearing #f) (description "The colour it was finished in."))
`

	const written = `(node site:S-101
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (colour "slate"))
`

	claims, diags := loadClaimModel(t, registry, written)

	assert.Zero(t, claims.Len())
	assert.Empty(t, diags)
}

// loadClaimModel writes a one-file registry and a one-file set of entities into
// a temporary directory and loads the claims of it.
//
// It is for the tests which vary one thing about a model and compare the
// readings. A fixture on disk is the right shape for a test about diagnostics,
// where the rendering beside the source is the point; it is the wrong shape for
// a test whose whole subject is the difference between two models, because the
// difference is then somewhere other than in the test.
func loadClaimModel(t *testing.T, registry, entities string) (*Claims, []Diagnostic) {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "registry"+Extension), []byte(registry), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "entities"+Extension), []byte(entities), 0o644))

	declared, diags := LoadRegistry(root)
	require.Empty(t, diags, "the written registry loads clean")

	return LoadClaims(root, declared)
}

// TestLoadClaimsReportsStructureBeforeReadingIt checks that a claim which is
// structurally wrong is reported and not interpreted.
//
// A claim missing its source has no source to invent, and reading it would put
// a value into the model with nothing behind it — which is the one thing the
// claim exists to prevent.
func TestLoadClaimsReportsStructureBeforeReadingIt(t *testing.T) {
	const registry = `(project (globalid-namespace "https://example.org/models/structure"))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
(type MeetingRoom (kind Space) (geometry area) (description "An enclosed room."))
(predicate width (unit m) (shape scalar) (description "How wide the thing is."))
`

	const written = `(node site:S-101
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (width (value 8.5 m) (method method:total-station) (date "2026-05-06")))
`

	claims, diags := loadClaimModel(t, registry, written)

	assert.Zero(t, claims.Len())
	require.Len(t, diags, 1)
	assert.Equal(t, "expected a (source ...) child of the width claim, found none", diags[0].Message)
}

// TestLoadClaimsReportsOnlyTheClaim checks that this pass says nothing about
// the form the claim was written on.
//
// A node missing its kind is the node loader's diagnostic. Reporting it here as
// well would mean two passes over one tree producing two copies of one
// sentence, and a caller collecting both would print each mistake twice.
func TestLoadClaimsReportsOnlyTheClaim(t *testing.T) {
	const registry = `(project (globalid-namespace "https://example.org/models/enclosing"))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
(predicate width (unit m) (shape scalar) (description "How wide the thing is."))
`

	const written = `(node site:S-101
  (width
    (value 8.5 m)
    (source "Plan set A-101, sheet 3")
    (method method:total-station)
    (date "2026-05-06")))
`

	claims, diags := loadClaimModel(t, registry, written)

	assert.Empty(t, diags, "the node's missing kind and type belong to the node loader")
	require.Equal(t, 1, claims.Len())

	read := slices.Collect(claims.Under("site:S-101", "width"))
	require.Len(t, read, 1)

	number, isScalar := read[0].Value().Scalar()
	require.True(t, isScalar)
	assert.Equal(t, 8.5, number)
}

// TestLoadClaimsIsDeterministic checks that the order claims come back in is
// the walk's and not a map's.
//
// Anything built from a load is meant to diff against the last run's, so two
// loads of one tree have to agree about the order of everything in them.
func TestLoadClaimsIsDeterministic(t *testing.T) {
	registry, _ := LoadRegistry(claimFixture("valid"))

	first, firstDiags := LoadClaims(claimFixture("valid"), registry)
	second, secondDiags := LoadClaims(claimFixture("valid"), registry)

	assert.Equal(t, firstDiags, secondDiags)

	var read [2][]string
	for i, claims := range [2]*Claims{first, second} {
		for claim := range claims.All() {
			read[i] = append(read[i], string(claim.Subject())+" "+claim.Predicate())
		}
	}

	assert.Equal(t, read[0], read[1])
	assert.NotEmpty(t, read[0])
}

func TestLoadClaimsUnreadableRoot(t *testing.T) {
	claims, diags := LoadClaims(filepath.Join("testdata", "claim", "no-such-directory"), nil)

	assert.Zero(t, claims.Len())
	assert.NotEmpty(t, diags)
}

// TestClaimsZeroValue checks that the collection a tree holding no claim yields
// answers every question rather than panicking on the model whose entities have
// not been written yet.
func TestClaimsZeroValue(t *testing.T) {
	var claims *Claims

	assert.Zero(t, claims.Len())
	assert.Empty(t, slices.Collect(claims.All()))
	assert.Empty(t, slices.Collect(claims.Of("site:S-101")))
	assert.Empty(t, slices.Collect(claims.Under("site:S-101", "width")))

	_, ok := claims.Claim("survey:C-0210")
	assert.False(t, ok)
}

// TestValueZeroValue checks that the value of a claim which could not be read
// reports that it holds nothing through every accessor.
func TestValueZeroValue(t *testing.T) {
	var value Value

	assert.Equal(t, Shape(""), value.Shape())
	assert.Empty(t, value.Unit())

	_, isScalar := value.Scalar()
	_, isCoordinate := value.Coordinate()
	_, isText := value.Text()
	_, isTransform := value.Transform()

	assert.False(t, isScalar)
	assert.False(t, isCoordinate)
	assert.False(t, isText)
	assert.False(t, isTransform)
}

// TestReaderDate checks the one spelling of a date, and the spellings which are
// close enough to look like it.
func TestReaderDate(t *testing.T) {
	testCases := []struct {
		name     string
		written  string
		expected time.Time
	}{
		{
			name:     "reads an RFC 3339 full-date",
			written:  `"2026-03-14"`,
			expected: time.Date(2026, time.March, 14, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "reads the first day of a year",
			written:  `"2026-01-01"`,
			expected: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "reads a leap day",
			written:  `"2024-02-29"`,
			expected: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "rejects a day-first spelling",
			written: `"14/03/2026"`,
		},
		{
			name:    "rejects a month written with one digit",
			written: `"2026-3-14"`,
		},
		{
			name:    "rejects a date carrying a time",
			written: `"2026-05-06T09:41:00Z"`,
		},
		{
			name:    "rejects a day which does not exist",
			written: `"2026-02-30"`,
		},
		{
			name:    "rejects a date written as a symbol, which no file could hold",
			written: `unquoted`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			file, err := Parse("dates"+Extension, strings.NewReader("(date "+testCase.written+")"))
			require.NoError(t, err)

			arg, ok := argument(file.Nodes[0], 0)
			require.True(t, ok)

			var r reader
			got, read := r.date(arg, "a date")

			if testCase.expected.IsZero() {
				assert.False(t, read)
				require.Len(t, r.diags, 1)
				assert.NotEmpty(t, r.diags[0].Message, "a date diagnostic says what was written instead")
				return
			}

			require.True(t, read)
			assert.Empty(t, r.diags)
			assert.Equal(t, testCase.expected, got)
		})
	}
}
