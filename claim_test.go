// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"math"
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

	claims, diags := LoadClaims(claimFixture(name), mustLoadRegistry(t, claimFixture(name)))

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
			name:    "names both claims holding one id, in whichever files they are, and the frame a claim named itself after",
			fixture: "duplicate-id",
		},
		{
			name:    "names a reference to a claim which carries no id of its own",
			fixture: "dangling-reference",
		},
		{
			name:    "shows the minimal claim where a bare scalar was written instead",
			fixture: "bare-scalar",
		},
		{
			name:    "names a claim written under a predicate declared to take a plain value",
			fixture: "claim-for-a-plain-value",
		},
		{
			name:    "names a plain value of a shape other than the one its predicate declares, as a value child's is named",
			fixture: "plain-value-wrong-shape",
		},
		{
			name:    "names a plain value in a unit its predicate does not declare rather than converting it",
			fixture: "plain-value-wrong-unit",
		},
		{
			name:    "names a dimensional plain value written with no unit",
			fixture: "plain-value-without-a-unit",
		},
		{
			name:    "names a unit written after a non-dimensional plain value",
			fixture: "plain-value-with-a-unit",
		},
		{
			name:    "names a plain coordinate with other than the components its predicate declares",
			fixture: "plain-value-wrong-dimension",
		},
		{
			name:    "names every bad plain value in a tree, in file and then position order",
			fixture: "plain-value-collected",
		},
		{
			name:    "names a deprecation which left out what replaced it, and the claim it retracted",
			fixture: "deprecated-without-replacement",
		},
		{
			name:    "names both ends of a supersession pointing at a claim the model does not hold",
			fixture: "dangling-supersession",
		},
		{
			name:    "names a claim which gave itself as what replaced it",
			fixture: "self-supersession",
		},
		{
			name:    "names every claim of a supersession cycle, once for the ring",
			fixture: "cyclic-supersession",
		},
		{
			name:    "names a replacement written on a claim which was never deprecated",
			fixture: "supersession-without-deprecation",
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

// mixedUnitsRegistry declares what the mixed-units tests write: one linear
// predicate, and a namespace a systematic term can be shared with.
const mixedUnitsRegistry = `(project (globalid-namespace "https://example.org/models/mixed-units"))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
(namespace control (description "Survey control points."))
(namespace survey (description "Claims read off a survey."))
(type MeetingRoom (kind Space) (geometry area) (description "An enclosed room."))
(predicate width (unit m) (shape scalar) (description "How wide the thing is."))
`

// mixedUnitsNode writes one node carrying one width claim, whose accuracy is
// the terms given — or none at all, where terms is empty.
func mixedUnitsNode(id, claimID, terms string) string {
	var out strings.Builder
	out.WriteString("(node " + id + "\n  (kind Space)\n  (type MeetingRoom)\n  (geometry area)\n  (width\n")
	if claimID != "" {
		out.WriteString("    (id " + claimID + ")\n")
	}
	out.WriteString("    (value 5.001 m)\n    (source \"Resurvey RS-2026-011\")\n    (method method:total-station)\n")
	if terms != "" {
		out.WriteString("    (accuracy " + terms + ")\n")
	}
	out.WriteString("    (date \"2026-09-28\")))\n")
	return out.String()
}

// TestLoadClaimsWarnsOfAnAccuracyInMixedUnits checks the one diagnostic a claim
// whose accuracy terms are not all in one unit carries: a warning, since the
// claim is valid and loads, saying why it will never be ranked.
func TestLoadClaimsWarnsOfAnAccuracyInMixedUnits(t *testing.T) {
	const terms = "(independent 1.0 mm) (systematic 0.001 m control:CP-3) (independent 2.0 mm)"

	written := mixedUnitsNode("site:CDU-01", "survey:C-0309", terms)
	claims, diags := loadClaimModel(t, mixedUnitsRegistry, written)

	require.Len(t, diags, 1)
	warning := diags[0]

	t.Run("is a warning, and the claim still loads", func(t *testing.T) {
		assert.Equal(t, SeverityWarning, warning.Severity)

		claim, ok := claims.Claim("survey:C-0309")
		require.True(t, ok)

		accuracy, hasAccuracy := claim.Accuracy()
		assert.True(t, hasAccuracy, "the accuracy is still what was written")
		assert.Len(t, accuracy.Terms, 3)
		assert.False(t, claim.Rankable(), "terms nothing converts between rank nowhere")
	})

	t.Run("points at the accuracy form", func(t *testing.T) {
		start := strings.Index(written, "(accuracy")
		require.GreaterOrEqual(t, start, 0)

		assert.Equal(t, start, warning.Span.Start.Offset)
		assert.Equal(t, 10, warning.Span.Start.Line)
		assert.Equal(t, 5, warning.Span.Start.Column)
		assert.Equal(t, start+len("(accuracy "+terms+")"), warning.Span.End.Offset)
	})

	t.Run("names the claim as a diagnostic spells it", func(t *testing.T) {
		claim, ok := claims.Claim("survey:C-0309")
		require.True(t, ok)

		assert.Contains(t, warning.Message, claimName(claim))
	})

	t.Run("names every unit found, each once, in the order written", func(t *testing.T) {
		assert.Contains(t, warning.Message, "mm and m")
		assert.NotContains(t, warning.Message, "m and mm")
	})

	t.Run("points at every term, with the unit each was written in", func(t *testing.T) {
		require.Len(t, warning.Related, 3)

		for i, want := range []struct {
			text string
			unit string
		}{
			{text: "(independent 1.0 mm)", unit: "mm"},
			{text: "(systematic 0.001 m control:CP-3)", unit: "m"},
			{text: "(independent 2.0 mm)", unit: "mm"},
		} {
			related := warning.Related[i]
			start := strings.Index(written, want.text)
			require.GreaterOrEqual(t, start, 0)

			assert.Equal(t, start, related.Span.Start.Offset, "term %d", i)
			assert.Equal(t, start+len(want.text), related.Span.End.Offset, "term %d", i)
			assert.True(t, strings.HasSuffix(related.Message, " "+want.unit), "term %d: %q", i, related.Message)
		}
	})
}

// TestLoadClaimsWarnsOfMixedUnitsOnlyWhereTheyAreMixed checks which claims the
// warning is for: one per claim whose terms are in more than one unit, and none
// for a claim whose terms agree or which carries no accuracy at all.
func TestLoadClaimsWarnsOfMixedUnitsOnlyWhereTheyAreMixed(t *testing.T) {
	testCases := []struct {
		name     string
		entities string
		expected int
	}{
		{
			name:     "warns once for one claim whose terms are mixed",
			entities: mixedUnitsNode("site:CDU-01", "", "(independent 1.0 mm) (independent 0.002 m)"),
			expected: 1,
		},
		{
			name: "warns once for each claim whose terms are mixed",
			entities: mixedUnitsNode("site:CDU-01", "", "(independent 1.0 mm) (independent 0.002 m)") +
				mixedUnitsNode("site:CDU-02", "", "(independent 0.002 m) (independent 1.0 mm)"),
			expected: 2,
		},
		{
			name:     "says nothing of a claim whose terms share one unit",
			entities: mixedUnitsNode("site:CDU-01", "", "(independent 1.0 mm) (systematic 2.0 mm control:CP-3)"),
			expected: 0,
		},
		{
			name:     "says nothing of a claim with no accuracy, which is unrankable without a warning",
			entities: mixedUnitsNode("site:CDU-01", "", ""),
			expected: 0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, diags := loadClaimModel(t, mixedUnitsRegistry, testCase.entities)

			assert.Len(t, diags, testCase.expected)
			for _, diag := range diags {
				assert.Equal(t, SeverityWarning, diag.Severity)
			}
		})
	}
}

// TestLoadClaimsOrdersMixedUnitsWarnings checks that the warnings about several
// claims come back collected and ordered by file and then by position, so that
// a model with two such claims hears about both, in an order that diffs.
func TestLoadClaimsOrdersMixedUnitsWarnings(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "registry"+Extension), []byte(mixedUnitsRegistry), 0o644))

	const mixed = "(independent 1.0 mm) (independent 0.002 m)"
	files := map[string]string{
		"a" + Extension: mixedUnitsNode("site:A-01", "", mixed) + mixedUnitsNode("site:A-02", "", mixed),
		"b" + Extension: mixedUnitsNode("site:B-01", "", mixed),
	}
	for name, text := range files {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(text), 0o644))
	}

	registry := mustLoadRegistry(t, root)
	_, diags := LoadClaims(root, registry)
	require.Len(t, diags, 3)

	for i, want := range []struct {
		file    string
		subject string
	}{
		{file: "a" + Extension, subject: "site:A-01"},
		{file: "a" + Extension, subject: "site:A-02"},
		{file: "b" + Extension, subject: "site:B-01"},
	} {
		assert.Equal(t, want.file, filepath.Base(diags[i].Span.Start.Path), "warning %d", i)
		assert.Contains(t, diags[i].Message, "the width of "+want.subject, "warning %d", i)
	}

	assert.Less(t, diags[0].Span.Start.Offset, diags[1].Span.Start.Offset)
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

// plainValueRegistry declares one predicate, width, either claim-bearing or
// not. Nothing else about the declaration moves, so a diagnostic pointing at it
// points at the same place whichever spelling the registry asks for.
func plainValueRegistry(claimBearing bool) string {
	bearing := "#t"
	if !claimBearing {
		bearing = "#f"
	}

	return `(project (globalid-namespace "https://example.org/models/plain"))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
(type MeetingRoom (kind Space) (geometry area) (description "An enclosed room."))
(predicate width (unit m) (shape scalar) (claim-bearing ` + bearing + `) (description "How wide it is."))
`
}

// TestLoadClaimsChecksAPlainValueAsAValueChild checks that a plain value is
// read by the reader a claim's value child is read by: the same mistake written
// either way is refused in the same words, with the same hint, pointing at the
// same declaration.
func TestLoadClaimsChecksAPlainValueAsAValueChild(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{name: "refuses a string where a scalar is declared", value: `"wide"`},
		{name: "refuses a coordinate where a scalar is declared", value: `(8.5 3.0) m`},
		{name: "refuses a unit other than the declared one", value: `8.5 ft`},
		{name: "refuses a dimensional value with no unit", value: `8.5`},
		{name: "refuses an integer where a real is spelled", value: `8 m`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			node := func(predicate string) string {
				return "(node site:S-101\n  (kind Space)\n  (type MeetingRoom)\n  (geometry area)\n  " + predicate + ")\n"
			}

			plainClaims, plain := loadClaimModel(t, plainValueRegistry(false), node("(width "+testCase.value+")"))
			_, asClaim := loadClaimModel(t, plainValueRegistry(true), node(
				`(width (value `+testCase.value+`) (source "Plan A-101") (method method:scaled) (date "2026-01-09"))`,
			))

			// Everything about the claim but its value is well formed, so the
			// value's is the one diagnostic either spelling produces.
			require.Len(t, plain, 1)
			require.Len(t, asClaim, 1)
			fromValue := asClaim[0]

			assert.Zero(t, plainClaims.Len(), "a plain value becomes no claim, refused or not")
			assert.Equal(t, SeverityError, plain[0].Severity)
			assert.Equal(t, fromValue.Severity, plain[0].Severity)
			assert.Equal(t, fromValue.Message, plain[0].Message, "the value child is refused in the same words")
			assert.Equal(t, fromValue.Hint, plain[0].Hint)

			// The two models live in two directories, so the declaration is
			// compared by where in its file it is rather than by the file.
			require.Len(t, plain[0].Related, len(fromValue.Related))
			for i, related := range plain[0].Related {
				assert.Equal(t, filepath.Base(fromValue.Related[i].Span.Start.Path), filepath.Base(related.Span.Start.Path))
				assert.Equal(t, fromValue.Related[i].Span.Start.Offset, related.Span.Start.Offset)
				assert.Equal(t, fromValue.Related[i].Span.End.Offset, related.Span.End.Offset)
				assert.Equal(t, fromValue.Related[i].Message, related.Message)
			}
		})
	}
}

// TestLoadClaimsPointsAtThePlainValue checks where the diagnostic about a plain
// value lands: within the form which wrote it, rather than at the node it was
// written on or at the declaration.
func TestLoadClaimsPointsAtThePlainValue(t *testing.T) {
	const written = `(node site:S-101
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (width 8.5 ft))
`

	_, diags := loadClaimModel(t, plainValueRegistry(false), written)
	require.Len(t, diags, 1)

	form := strings.Index(written, "(width 8.5 ft)")
	unit := strings.Index(written, "ft)")

	assert.Equal(t, unit, diags[0].Span.Start.Offset, "the unit is the thing which is wrong")
	assert.GreaterOrEqual(t, diags[0].Span.Start.Offset, form)
	assert.LessOrEqual(t, diags[0].Span.End.Offset, form+len("(width 8.5 ft)"))
}

// TestLoadClaimsReportsAnUndeclaredPlainValueOnce checks that a plain value
// under a predicate nothing declares is heard about as the misspelling it is,
// once, and not a second time about a value nothing can judge.
func TestLoadClaimsReportsAnUndeclaredPlainValueOnce(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{name: "a value which would read as a scalar", value: `8.5 ft`},
		{name: "a value which would read as nothing at all", value: `wide`},
		{name: "an integer", value: `8`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			written := "(node site:S-101\n  (kind Space)\n  (type MeetingRoom)\n  (geometry area)\n  (breadth " + testCase.value + "))\n"

			claims, diags := loadClaimModel(t, plainValueRegistry(false), written)

			assert.Zero(t, claims.Len())
			require.Len(t, diags, 1)
			assert.Contains(t, diags[0].Message, "expected a declared predicate")
		})
	}
}

// TestLoadClaimsCollectsBadPlainValues checks that a tree holding several bad
// plain values hears about every one of them in one load, in file and then
// position order, and that the same load twice says the same thing.
func TestLoadClaimsCollectsBadPlainValues(t *testing.T) {
	registry := mustLoadRegistry(t, claimFixture("plain-value-collected"))

	claims, diags := LoadClaims(claimFixture("plain-value-collected"), registry)
	_, again := LoadClaims(claimFixture("plain-value-collected"), registry)

	assert.Zero(t, claims.Len(), "no plain value becomes a claim")
	assert.Equal(t, diags, again)

	var collected Diagnostics
	collected.Add(diags...)
	sorted := collected.All()
	require.Len(t, sorted, 4)

	files := make([]string, 0, len(sorted))
	for _, diag := range sorted {
		assert.Equal(t, SeverityError, diag.Severity)
		files = append(files, filepath.Base(diag.Span.Start.Path))
	}

	assert.Equal(t, []string{"a.dfc", "a.dfc", "a.dfc", "b.dfc"}, files, "the second file is read whatever the first held")
	assert.Less(t, sorted[0].Span.Start.Offset, sorted[1].Span.Start.Offset)
	assert.Less(t, sorted[1].Span.Start.Offset, sorted[2].Span.Start.Offset)
}

// TestLoadClaimsRefusesABareScalar is the rule the model depends on: where a
// claim belongs, a number on its own does not load.
//
// It is checked on the fixture which writes one predicate three ways, so that
// what is refused and what is accepted are a few lines apart and the difference
// between them is the whole of the escape hatch.
func TestLoadClaimsRefusesABareScalar(t *testing.T) {
	claims, rendered := loadClaimFixture(t, "bare-scalar")
	require.NotEmpty(t, rendered, "the bare scalar is reported")

	read := slices.Collect(claims.Under("site:S-101", "width"))
	require.Len(t, read, 2, "the two claims load and the bare scalar is not one of them")

	t.Run("the minimal claim loads and is unrankable", func(t *testing.T) {
		accuracy, hasAccuracy := read[0].Accuracy()

		assert.False(t, hasAccuracy)
		assert.False(t, read[0].Rankable())
		assert.Empty(t, accuracy.Terms, "no default accuracy was invented")

		number, isScalar := read[0].Value().Scalar()
		require.True(t, isScalar)
		assert.Equal(t, 8.4, number)
		assert.Equal(t, ID("method:estimated"), read[0].Method(), "the method it does have was still stated")
	})

	t.Run("the full claim loads and is rankable", func(t *testing.T) {
		assert.True(t, read[1].Rankable())

		number, isScalar := read[1].Value().Scalar()
		require.True(t, isScalar)
		assert.Equal(t, 8.5, number)
	})

	t.Run("the bare scalar became no claim at all", func(t *testing.T) {
		for _, claim := range read {
			assert.NotEmpty(t, claim.Source(), "a claim in the model carries its evidence")
		}
	})
}

// TestLoadClaimsBareScalarNamesWhatWouldBeAccepted checks the three things the
// diagnostic has to carry: which predicate, where, and the form to write
// instead.
//
// The last is what separates this from "invalid claim". Somebody who wrote a
// number where a claim belongs is missing four children and the spelling of
// each of them, and a diagnostic which only refused the number would leave them
// to find the specification.
func TestLoadClaimsBareScalarNamesWhatWouldBeAccepted(t *testing.T) {
	_, diags := LoadClaims(claimFixture("bare-scalar"), mustLoadRegistry(t, claimFixture("bare-scalar")))
	require.Len(t, diags, 1)

	diagnostic := diags[0]

	assert.Equal(t, SeverityError, diagnostic.Severity)
	assert.Contains(t, diagnostic.Message, "width", "the diagnostic names the predicate")
	assert.Equal(t, filepath.Join(claimFixture("bare-scalar"), "claims.dfc"), diagnostic.Span.Start.Path)
	assert.Positive(t, diagnostic.Span.Start.Line)
	assert.Positive(t, diagnostic.Span.Start.Column)
	assert.Contains(
		t, diagnostic.Hint,
		`(width (value <number> m) (source "<evidence>") (method <method-id>) (date "<YYYY-MM-DD>"))`,
		"the diagnostic shows the minimal claim which would be accepted",
	)
	assert.NotEmpty(t, diagnostic.Related, "and where the predicate was declared")
}

// LoadClaims takes a root and a registry, and there is no third parameter an
// option set, a severity or a strictness could arrive through. The rule below
// is a property of the loader rather than of a call, and this is where that is
// asserted at compile time.
var _ func(string, *Registry) (*Claims, []Diagnostic) = LoadClaims

// TestLoadClaimsBareScalarIsUnconditional checks that nothing in the
// environment turns the rule into a warning.
//
// A rule which can be waived is waived the same afternoon somebody has ten
// thousand of them to fix, and the model which comes back six months later has
// provenance on a minority of its values with no commit saying so. That is the
// failure decision 0008 is about, so the absence of a downgrade is a behaviour
// with a test rather than a thing nobody got round to implementing.
func TestLoadClaimsBareScalarIsUnconditional(t *testing.T) {
	const registry = `(project (globalid-namespace "https://example.org/models/unconditional"))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
(type MeetingRoom (kind Space) (geometry area) (description "An enclosed room."))
(predicate width (unit m) (shape scalar) (description "How wide the thing is."))
`

	const written = `(node site:S-101
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (width 8.5))
`

	testCases := []struct {
		name     string
		variable string
		value    string
	}{
		{
			name: "with nothing set in the environment",
		},
		{
			name:     "with the rule named directly",
			variable: "DFCAD_ALLOW_BARE_SCALARS",
			value:    "1",
		},
		{
			name:     "with a severity asked for",
			variable: "DFCAD_BARE_SCALAR_SEVERITY",
			value:    "warning",
		},
		{
			name:     "with strictness turned off",
			variable: "DFCAD_STRICT",
			value:    "0",
		},
		{
			name:     "with linting turned off",
			variable: "DFCAD_LINT",
			value:    "off",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.variable != "" {
				t.Setenv(testCase.variable, testCase.value)
			}

			claims, diags := loadClaimModel(t, registry, written)

			assert.Zero(t, claims.Len(), "the bare scalar is not read as a claim")
			require.Len(t, diags, 1)
			assert.Equal(t, SeverityError, diags[0].Severity)

			var collected Diagnostics
			collected.Add(diags...)
			assert.True(t, collected.HasErrors(), "the load fails")
		})
	}
}

// TestLoadClaimsRefusesAValueWrittenAsAChildForm checks that the rule reaches
// the one value shape which is itself written as a child form.
//
// A transform value is `(transform ...)`, so a form holding child forms is not
// proof that a claim was written. A loader which read `(frame-transform
// (transform ...))` as a claim would report the four children it is missing and
// never say the one thing which is wrong with it — that a transform between two
// frames is a measurement, and this one arrived with nothing behind it.
func TestLoadClaimsRefusesAValueWrittenAsAChildForm(t *testing.T) {
	const written = `(node site:S-101
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame-transform
    (transform
      (translation 0.0 0.0 0.0)
      (rotation 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0)
      (scale 1.0))))
`

	t.Run("a transform written where a claim-bearing predicate belongs is refused", func(t *testing.T) {
		const registry = `(project (globalid-namespace "https://example.org/models/transform"))
(namespace site (description "Semantic nodes minted by this model."))
(type MeetingRoom (kind Space) (geometry area) (description "An enclosed room."))
(predicate frame-transform (shape transform) (description "The rigid transform to a parent."))
`

		claims, diags := loadClaimModel(t, registry, written)

		assert.Zero(t, claims.Len())
		require.Len(t, diags, 1, "one diagnostic, and it is the rule rather than four missing children")
		assert.Equal(t, SeverityError, diags[0].Severity)
		assert.Contains(t, diags[0].Message, "frame-transform")
		assert.Contains(t, diags[0].Hint, "(transform (translation ...) (rotation ...) (scale ...))")
	})

	t.Run("and the same transform is the plain value a non-claim-bearing predicate takes", func(t *testing.T) {
		const registry = `(project (globalid-namespace "https://example.org/models/transform"))
(namespace site (description "Semantic nodes minted by this model."))
(type MeetingRoom (kind Space) (geometry area) (description "An enclosed room."))
(predicate frame-transform (shape transform) (claim-bearing #f) (description "The rigid transform to a parent."))
`

		claims, diags := loadClaimModel(t, registry, written)

		assert.Zero(t, claims.Len())
		assert.Empty(t, diags, "the classification is by what was written, not by whether there are child forms")
	})
}

// TestLoadClaimsLeavesAnEmptyFormToTheStructuralPass checks that a predicate
// written with nothing after it is not reported here as a bare scalar.
//
// It is not one. Nothing was written where the value goes, and a diagnostic
// saying a plain value was found would name something nobody wrote. That a form
// holds nothing is [Validate]'s sentence, and it says it once.
func TestLoadClaimsLeavesAnEmptyFormToTheStructuralPass(t *testing.T) {
	const registry = `(project (globalid-namespace "https://example.org/models/empty"))
(namespace site (description "Semantic nodes minted by this model."))
(type MeetingRoom (kind Space) (geometry area) (description "An enclosed room."))
(predicate width (unit m) (shape scalar) (description "How wide the thing is."))
`

	const written = `(node site:S-101
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (width))
`

	claims, diags := loadClaimModel(t, registry, written)

	assert.Zero(t, claims.Len())
	assert.Empty(t, diags, "the claim pass says nothing about a form which holds nothing")

	file, err := Parse("entities"+Extension, strings.NewReader(written))
	require.NoError(t, err)

	structural := Validate(file)
	require.Len(t, structural, 1, "and the structural pass does")
	assert.Equal(t, "expected a value or the children of a claim after the width tag, found none", structural[0].Message)
}

// TestLoadClaimsRefusesAClaimForAPlainValue checks the other direction of the
// same rule: a predicate declared to carry no provenance does not quietly grow
// some.
func TestLoadClaimsRefusesAClaimForAPlainValue(t *testing.T) {
	claims, rendered := loadClaimFixture(t, "claim-for-a-plain-value")

	assert.NotEmpty(t, rendered, "the claim is reported")
	assert.Zero(t, claims.Len(), "and neither spelling is read as a claim")
}

// TestSpellMinimalClaim checks that the form the bare-scalar diagnostic shows
// is the one the predicate declares rather than a fixed sentence.
//
// A skeleton with the wrong unit or the wrong number of components is worse
// than none: it is a form somebody copies, writes, and is refused a second time
// for a reason the first diagnostic invented.
func TestSpellMinimalClaim(t *testing.T) {
	testCases := []struct {
		name      string
		predicate Predicate
		expected  string
	}{
		{
			name:      "spells a scalar with the unit the predicate declares",
			predicate: Predicate{Shape: ShapeScalar, Unit: "m"},
			expected:  `(width (value <number> m) (source "<evidence>") (method <method-id>) (date "<YYYY-MM-DD>"))`,
		},
		{
			name:      "spells a non-dimensional scalar with no unit at all",
			predicate: Predicate{Shape: ShapeScalar},
			expected:  `(width (value <number>) (source "<evidence>") (method <method-id>) (date "<YYYY-MM-DD>"))`,
		},
		{
			name:      "spells a coordinate with as many components as the predicate declares",
			predicate: Predicate{Shape: ShapeCoordinate, Dimension: 3, Unit: "m"},
			expected:  `(width (value (<number> <number> <number>) m) (source "<evidence>") (method <method-id>) (date "<YYYY-MM-DD>"))`,
		},
		{
			name:      "spells a string, which carries no unit",
			predicate: Predicate{Shape: ShapeText},
			expected:  `(width (value "<text>") (source "<evidence>") (method <method-id>) (date "<YYYY-MM-DD>"))`,
		},
		{
			name:      "spells a transform as the child form it is written as",
			predicate: Predicate{Shape: ShapeTransform},
			expected:  `(width (value (transform (translation ...) (rotation ...) (scale ...))) (source "<evidence>") (method <method-id>) (date "<YYYY-MM-DD>"))`,
		},
		{
			name:      "says only that a value goes there when the predicate declares no shape",
			predicate: Predicate{},
			expected:  `(width (value <value>) (source "<evidence>") (method <method-id>) (date "<YYYY-MM-DD>"))`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, spellMinimalClaim("width", testCase.predicate))
		})
	}
}

// mustLoadRegistry loads a fixture registry which is required to load clean.
func mustLoadRegistry(t *testing.T, root string) *Registry {
	t.Helper()

	registry, diags := LoadRegistry(root)
	require.Empty(t, diags, "the fixture registry loads clean")

	return registry
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

// TestClaimCombined checks the figure a claim's own accuracy reduces to: the
// rule of specification section 6.6.5, applied to a budget of the one claim.
func TestClaimCombined(t *testing.T) {
	testCases := []struct {
		name     string
		terms    string
		expected Uncertainty
	}{
		{
			name:     "is one independent term as it was written",
			terms:    "(independent 0.05 m)",
			expected: Uncertainty{Magnitude: 0.05, Unit: "m", CoverageFactor: 1},
		},
		{
			name:     "joins an independent and a systematic term in quadrature",
			terms:    "(independent 0.003 m) (systematic 0.008 m control:CP-3)",
			expected: Uncertainty{Magnitude: math.Sqrt(0.003*0.003 + 0.008*0.008), Unit: "m", CoverageFactor: 1},
		},
		{
			name:     "counts a negative magnitude by its size",
			terms:    "(independent -0.003 m) (systematic -0.008 m control:CP-3)",
			expected: Uncertainty{Magnitude: math.Sqrt(0.003*0.003 + 0.008*0.008), Unit: "m", CoverageFactor: 1},
		},
		{
			name:     "counts one term id written twice once, at the larger magnitude",
			terms:    "(systematic 0.004 m control:CP-3) (systematic 0.008 m control:CP-3)",
			expected: Uncertainty{Magnitude: 0.008, Unit: "m", CoverageFactor: 1},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			claims, _ := loadClaimModel(t, mixedUnitsRegistry, mixedUnitsNode("site:CDU-01", "survey:C-0309", testCase.terms))

			claim, ok := claims.Claim("survey:C-0309")
			require.True(t, ok)

			got, err := claim.Combined()

			require.NoError(t, err)
			assert.Equal(t, testCase.expected.Unit, got.Unit)
			assert.Equal(t, testCase.expected.CoverageFactor, got.CoverageFactor)
			assert.InDelta(t, testCase.expected.Magnitude, got.Magnitude, 1e-15)
		})
	}
}

// TestClaimCombinedIsTheFigureOfABudgetOfTheClaimAlone checks that the claim's
// figure and a one-claim budget's figure are one function, exactly, over every
// claim of the fixture model.
func TestClaimCombinedIsTheFigureOfABudgetOfTheClaimAlone(t *testing.T) {
	claims, _ := loadClaimFixture(t, "valid")

	for claim := range claims.All() {
		var budget Budget
		budget.Add(claim)

		expected, expectedErr := budget.Combined()
		got, err := claim.Combined()

		assert.Equal(t, expected, got)
		assert.Equal(t, expectedErr == nil, err == nil)
	}
}

// TestClaimCombinedReportsAClaimWithNoAccuracyUnknown checks that a claim which
// states no accuracy has no figure, and says which claim it was.
func TestClaimCombinedReportsAClaimWithNoAccuracyUnknown(t *testing.T) {
	claims, _ := loadClaimModel(t, mixedUnitsRegistry, mixedUnitsNode("site:CDU-01", "survey:C-0309", ""))

	claim, ok := claims.Claim("survey:C-0309")
	require.True(t, ok)

	_, err := claim.Combined()

	var unknown UnknownAccuracyError
	require.True(t, errors.As(err, &unknown), "expected UnknownAccuracyError, got %T", err)
	assert.Equal(t, []*Claim{claim}, unknown.Claims)
}

// TestClaimCombinedReportsTermsInMixedUnits checks that terms in more than one
// unit combine to nothing, and name their units each once in the order written.
func TestClaimCombinedReportsTermsInMixedUnits(t *testing.T) {
	const terms = "(independent 1.0 mm) (systematic 0.001 m control:CP-3) (independent 2.0 mm)"

	claims, _ := loadClaimModel(t, mixedUnitsRegistry, mixedUnitsNode("site:CDU-01", "survey:C-0309", terms))

	claim, ok := claims.Claim("survey:C-0309")
	require.True(t, ok)

	_, err := claim.Combined()

	var mixed MixedUnitsError
	require.True(t, errors.As(err, &mixed), "expected MixedUnitsError, got %T", err)
	assert.Equal(t, []Unit{"mm", "m"}, mixed.Units)
}

// plainOfRegistry declares a frame carrying a plain value, and a predicate of
// each spelling: three non-claim-bearing, of two shapes, and one claim-bearing
// so that a bare scalar written under it can be shown to be no plain value.
const plainOfRegistry = `(project (globalid-namespace "https://example.org/models/plain"))
(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))
(type Plot (kind Space) (geometry area) (description "A parcel of ground."))
(predicate crs (shape text) (claim-bearing #f) (description "The coordinate reference system."))
(predicate note (shape text) (claim-bearing #f) (description "A remark for a person reading it."))
(predicate nominal-width (unit m) (shape scalar) (claim-bearing #f) (description "The width it was designed to."))
(predicate width (unit m) (shape scalar) (description "How wide it is."))
(frame frame:site (label "Site grid") (unit m) (crs "EPSG:25831"))
`

// plainWant is what one plain value is expected to hold, read through the
// accessors a caller would read it through.
type plainWant struct {
	predicate string
	shape     Shape
	text      string
	scalar    float64
	unit      Unit
	written   string
}

func TestClaimsPlainOf(t *testing.T) {
	testCases := []struct {
		name     string
		entities string
		subject  ID
		expected []plainWant
	}{
		{
			name:     "reads a plain value written on a semantic node",
			entities: "(node site:S-01\n  (kind Space)\n  (type Plot)\n  (geometry area)\n  (note \"Fenced on three sides\"))\n",
			subject:  "site:S-01",
			expected: []plainWant{
				{predicate: "note", shape: ShapeText, text: "Fenced on three sides", written: `(note "Fenced on three sides")`},
			},
		},
		{
			name:     "reads a plain value written on a vertex, with the unit written beside it",
			entities: "(vertex geom:V-01 (frame frame:site) (nominal-width 8.5 m))\n",
			subject:  "geom:V-01",
			expected: []plainWant{
				{predicate: "nominal-width", shape: ShapeScalar, scalar: 8.5, unit: UnitMetre, written: "(nominal-width 8.5 m)"},
			},
		},
		{
			name:     "reads a plain value written on an edge",
			entities: "(edge geom:E-01 (frame frame:site) (vertices geom:V-01 geom:V-02) (note \"South boundary\"))\n",
			subject:  "geom:E-01",
			expected: []plainWant{
				{predicate: "note", shape: ShapeText, text: "South boundary", written: `(note "South boundary")`},
			},
		},
		{
			name:     "reads a plain value written on a loop",
			entities: "(loop geom:L-01 (frame frame:site) (edges geom:E-01) (note \"Plot outline\"))\n",
			subject:  "geom:L-01",
			expected: []plainWant{
				{predicate: "note", shape: ShapeText, text: "Plot outline", written: `(note "Plot outline")`},
			},
		},
		{
			name:     "reads a plain value written on a frame",
			entities: "",
			subject:  "frame:site",
			expected: []plainWant{
				{predicate: "crs", shape: ShapeText, text: "EPSG:25831", written: `(crs "EPSG:25831")`},
			},
		},
		{
			name:     "returns a repeated predicate's values, and every other, in written order",
			entities: "(node site:S-01\n  (kind Space)\n  (type Plot)\n  (geometry area)\n  (note \"first\")\n  (crs \"EPSG:1234\")\n  (note \"second\"))\n",
			subject:  "site:S-01",
			expected: []plainWant{
				{predicate: "note", shape: ShapeText, text: "first", written: `(note "first")`},
				{predicate: "crs", shape: ShapeText, text: "EPSG:1234", written: `(crs "EPSG:1234")`},
				{predicate: "note", shape: ShapeText, text: "second", written: `(note "second")`},
			},
		},
		{
			name:     "returns nothing for a bare scalar under a claim-bearing predicate",
			entities: "(node site:S-01\n  (kind Space)\n  (type Plot)\n  (geometry area)\n  (width 8.5 m))\n",
			subject:  "site:S-01",
		},
		{
			name:     "returns nothing for a plain value its predicate's declaration refuses",
			entities: "(node site:S-01\n  (kind Space)\n  (type Plot)\n  (geometry area)\n  (nominal-width 8.5 ft))\n",
			subject:  "site:S-01",
		},
		{
			name:     "returns nothing for a subject nothing was written on",
			entities: "(node site:S-01\n  (kind Space)\n  (type Plot)\n  (geometry area)\n  (note \"here\"))\n",
			subject:  "site:S-02",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			claims, _ := loadClaimModel(t, plainOfRegistry, testCase.entities)

			var got []PlainValue
			for value := range claims.PlainOf(testCase.subject) {
				got = append(got, value)
			}

			require.Len(t, got, len(testCase.expected))
			for i, want := range testCase.expected {
				assert.Equal(t, want.predicate, got[i].Predicate)
				assert.Equal(t, want.shape, got[i].Value.Shape())
				assert.Equal(t, want.unit, got[i].Value.Unit())

				switch want.shape {
				case ShapeText:
					text, ok := got[i].Value.Text()
					require.True(t, ok)
					assert.Equal(t, want.text, text)
				case ShapeScalar:
					scalar, ok := got[i].Value.Scalar()
					require.True(t, ok)
					assert.Equal(t, want.scalar, scalar)
				}

				// The span is the whole form, in whichever file wrote it, which
				// is what sends a reader to it.
				span := got[i].Value.Span()
				source, err := os.ReadFile(span.Start.Path)
				require.NoError(t, err)
				assert.Equal(t, want.written, string(source[span.Start.Offset:span.End.Offset]))
			}

			assert.Zero(t, claims.Len(), "a plain value is not counted among the claims")
		})
	}
}

// TestClaimsPlainOfIsNilSafe checks that the plain values of no load are none,
// as the claims of no load are.
func TestClaimsPlainOfIsNilSafe(t *testing.T) {
	var claims *Claims

	for range claims.PlainOf("site:S-01") {
		t.Fatal("a nil Claims holds no plain value")
	}
	for range (&Claims{}).PlainOf("site:S-01") {
		t.Fatal("the zero Claims holds no plain value")
	}
}

// TestClaimsPlainOfStopsWhenAsked checks that the iterator honours a caller
// which breaks out of it.
func TestClaimsPlainOfStopsWhenAsked(t *testing.T) {
	claims, _ := loadClaimModel(t, plainOfRegistry,
		"(node site:S-01\n  (kind Space)\n  (type Plot)\n  (geometry area)\n  (note \"first\")\n  (note \"second\"))\n")

	var read int
	for range claims.PlainOf("site:S-01") {
		read++
		break
	}

	assert.Equal(t, 1, read)
}

// TestClaimsPlainOfAgreesWithFramePlain checks, over every model in the
// fixture corpus which loads, that the plain values the claim pass records on a
// frame are the ones Frame.Plain reads off it: filtered to one predicate, the
// one answers exactly what the other does. Two readings of one form which
// disagreed would be two answers to what the model says.
func TestClaimsPlainOfAgreesWithFramePlain(t *testing.T) {
	var roots []string
	require.NoError(t, filepath.WalkDir("testdata", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == "registry"+Extension {
			roots = append(roots, filepath.Dir(path))
		}
		return nil
	}))

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "registry"+Extension), []byte(plainOfRegistry+
		"(frame frame:building (label \"Building grid\") (unit m) (parent frame:site) (crs \"EPSG:6543\") (nominal-width 8.5 m) (crs \"EPSG:26982\"))\n"), 0o644))
	roots = append(roots, root)

	var compared int
	for _, root := range roots {
		registry, diags := LoadRegistry(root)
		if hasError(diags) {
			continue
		}

		claims, diags := LoadClaims(root, registry)
		if hasError(diags) {
			continue
		}

		for frame := range registry.Frames() {
			predicates := make(map[string]struct{})
			for _, form := range frame.Claims {
				if tag, ok := formTag(form); ok {
					predicates[tag] = struct{}{}
				}
			}
			for value := range claims.PlainOf(frame.ID) {
				predicates[value.Predicate] = struct{}{}
			}

			for predicate := range predicates {
				var got []Value
				for value := range claims.PlainOf(frame.ID) {
					if value.Predicate == predicate {
						got = append(got, value.Value)
					}
				}

				assert.Equal(t, frame.Plain(predicate), got, "%s: %s under %s", root, frame.ID, predicate)
				if len(got) > 0 {
					compared++
				}
			}
		}
	}

	// The frames written here carry three predicates' worth between them, and
	// the corpus at least one more, so a count below that is a walk which found
	// nothing rather than a property which held.
	assert.GreaterOrEqual(t, compared, 4, "the property was checked against plain values rather than their absence")
}

// hasError reports whether any of diags is an error.
func hasError(diags []Diagnostic) bool {
	return slices.ContainsFunc(diags, func(diag Diagnostic) bool { return diag.Severity == SeverityError })
}
