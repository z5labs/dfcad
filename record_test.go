// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The model the claim-authoring tests change: a vocabulary with a predicate of
// each shape worth writing from a caller, and rooms carrying claims in each of
// the states a correction has to tell apart — one which nothing has ever said
// anything about, one stated once by a claim which wrote no id, and one stated
// twice by claims which did.
const (
	recordRegistry = `(project
  (label "Recording fixture")
  (globalid-namespace "https://example.org/models/record"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))

(frame frame:site-grid (label "Site survey grid") (unit m))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings."))

(predicate area
  (unit m2)
  (shape scalar)
  (description "How much floor a space has."))

(predicate centroid
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "Where the middle of a thing is."))

(predicate finish
  (shape text)
  (description "What the floor is finished in."))

(predicate occupancy
  (shape scalar)
  (strict #t)
  (description "How many people it seats."))

(predicate nominal-area
  (unit m2)
  (shape scalar)
  (claim-bearing #f)
  (description "The area the brief asked for, which is not a measurement."))
`

	recordModel = `(node site:S-101
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:site-grid))

(node site:S-102
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:site-grid)
  (area
    (value 24.2 m2)
    (source "Design drawing DR-2026-004, Acme Architects")
    (method method:estimate)
    (accuracy (independent 0.5 m2))
    (date "2026-01-12")))

(node site:S-103
  (label "Meeting Room C")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:site-grid)
  (area
    (id site:M-0001)
    (value 31.0 m2)
    (source "Design drawing DR-2026-004, Acme Architects")
    (method method:estimate)
    (accuracy (independent 0.5 m2))
    (date "2026-01-12"))
  (area
    (id site:M-0002)
    (value 31.4 m2)
    (source "As-built check AB-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-05-06")))

(node site:S-104
  (label "Meeting Room D")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:site-grid)
  (area
    (id site:M-0003)
    (value 12.0 m2)
    (source "Design drawing DR-2026-004, Acme Architects")
    (method method:estimate)
    (accuracy (independent 0.5 m2))
    (date "2026-01-12")))
`
)

// recordFixture writes the model the claim-authoring tests change and returns
// its root.
func recordFixture(t *testing.T) string {
	t.Helper()

	return tree(t, map[string]string{
		"registry.dfc":      recordRegistry,
		"entities/site.dfc": recordModel,
	})
}

// aClaim is a well-formed claim of the fixture's scalar predicate, which every
// case below starts from and changes the one axis it is about.
func aClaim() ClaimSpec {
	return ClaimSpec{
		Subject:   "site:S-101",
		Predicate: "area",
		Value:     ScalarValue(24.2, "m2"),
		Source:    "As-built check AB-2026-009, Acme Surveys",
		Method:    "method:total-station",
		Accuracy:  []AccuracyTerm{{Kind: TermIndependent, Magnitude: 0.05, Unit: "m2"}},
		Date:      time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
}

// onlyClaim is the one claim written on a subject under a predicate, requiring
// the model to hold exactly one.
func onlyClaim(t *testing.T, graph *Graph, subject ID, predicate string) *Claim {
	t.Helper()

	claims := slices.Collect(graph.Claims().Under(subject, predicate))
	require.Len(t, claims, 1)

	return claims[0]
}

func TestTxAddClaim(t *testing.T) {
	testCases := []struct {
		name     string
		spec     func(spec ClaimSpec) ClaimSpec
		expected string
	}{
		{
			name: "writes a claim carrying every axis",
			spec: func(spec ClaimSpec) ClaimSpec { return spec },
			expected: `(area
    (value 24.2 m2)
    (source "As-built check AB-2026-009, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-05-06"))`,
		},
		{
			name: "writes the least a claim may say, which carries no accuracy",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Accuracy = nil
				return spec
			},
			expected: `(area
    (value 24.2 m2)
    (source "As-built check AB-2026-009, Acme Surveys")
    (method method:total-station)
    (date "2026-05-06"))`,
		},
		{
			name: "writes an id where one was asked for",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.ID = "site:M-0100"
				return spec
			},
			expected: `(area
    (id site:M-0100)
    (value 24.2 m2)`,
		},
		{
			name: "writes a systematic term beside an independent one",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Accuracy = append(spec.Accuracy, AccuracyTerm{
					Kind:      TermSystematic,
					Magnitude: 0.002,
					Unit:      "m2",
					Source:    "method:total-station",
				})
				return spec
			},
			expected: `(accuracy (independent 0.05 m2) (systematic 0.002 m2 method:total-station))`,
		},
		{
			name: "writes a coordinate value with its components in order",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Predicate = "centroid"
				spec.Value = CoordinateValue([]float64{3.5, 1.25, 0}, "m")
				spec.Accuracy = []AccuracyTerm{{Kind: TermIndependent, Magnitude: 0.01, Unit: "m"}}
				return spec
			},
			expected: `(value (3.5 1.25 0.0) m)`,
		},
		{
			name: "writes a text value, which carries no unit",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Predicate = "finish"
				spec.Value = TextValue("sealed concrete")
				spec.Accuracy = nil
				return spec
			},
			expected: `(value "sealed concrete")`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := recordFixture(t)
			spec := testCase.spec(aClaim())

			graph := authored(t, root, func(tx *Tx) error {
				_, _, err := tx.AddClaim(spec)
				return err
			})

			claim := onlyClaim(t, graph, spec.Subject, spec.Predicate)
			assert.Equal(t, spec.Value.Shape(), claim.Value().Shape())
			assert.Equal(t, spec.Value.Unit(), claim.Value().Unit())
			assert.Equal(t, spec.Source, claim.Source())
			assert.Equal(t, spec.Method, claim.Method())
			assert.Equal(t, spec.Date, claim.Date())
			assert.Equal(t, RankNormal, claim.Rank())
			assert.Equal(t, len(spec.Accuracy) > 0, claim.Rankable())

			assert.Contains(t, entityFile(t, root), testCase.expected)
		})
	}
}

// TestTxAddClaimReportsWhatItLeftBehind is its own function because it asserts
// about the notices rather than about what was written: a claim which cannot be
// ranked and a claim which disagrees with one already there are both legitimate,
// and both are things to hear about now rather than to find out later.
func TestTxAddClaimReportsWhatItLeftBehind(t *testing.T) {
	testCases := []struct {
		name              string
		spec              func(spec ClaimSpec) ClaimSpec
		expectedKinds     []NoticeKind
		expectedCompeting int
	}{
		{
			name:          "says nothing about a rankable claim on a subject nothing states",
			spec:          func(spec ClaimSpec) ClaimSpec { return spec },
			expectedKinds: nil,
		},
		{
			name: "reports a claim with no accuracy as unrankable",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Accuracy = nil
				return spec
			},
			expectedKinds: []NoticeKind{NoticeUnrankable},
		},
		{
			name: "reports a second claim on one pair as a conflict, naming the competing claim",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Subject = "site:S-102"
				return spec
			},
			expectedKinds:     []NoticeKind{NoticeConflict},
			expectedCompeting: 1,
		},
		{
			name: "reports both where both hold",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Subject, spec.Accuracy = "site:S-103", nil
				return spec
			},
			expectedKinds:     []NoticeKind{NoticeUnrankable, NoticeConflict},
			expectedCompeting: 2,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			spec := testCase.spec(aClaim())
			tx := begin(t, recordFixture(t))
			defer func() { _ = tx.Close() }()

			_, notices, err := tx.AddClaim(spec)
			require.NoError(t, err)

			kinds := make([]NoticeKind, 0, len(notices))
			for _, notice := range notices {
				kinds = append(kinds, notice.Kind)

				assert.Equal(t, spec.Subject, notice.Subject)
				assert.Equal(t, spec.Predicate, notice.Predicate)
				assert.NotEmpty(t, notice.Message())
			}

			assert.Equal(t, testCase.expectedKinds, sliceOrNil(kinds))

			for _, notice := range notices {
				if notice.Kind == NoticeConflict {
					assert.Len(t, notice.Competing, testCase.expectedCompeting)
				}
			}
		})
	}
}

func TestTxAddClaimChecksEveryAxisAgainstTheRegistry(t *testing.T) {
	testCases := []struct {
		name     string
		spec     func(spec ClaimSpec) ClaimSpec
		expected error
	}{
		{
			name: "refuses a predicate nothing declares",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Predicate = "aera"
				return spec
			},
			expected: UnknownAxisError{Axis: "predicate", Value: "aera"},
		},
		{
			name: "refuses a claim under a predicate declared to take a plain value",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Predicate = "nominal-area"
				return spec
			},
			expected: NotClaimBearingError{Predicate: "nominal-area"},
		},
		{
			name: "refuses a value of another shape than the predicate declares",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Value = TextValue("about twenty-four square metres")
				return spec
			},
			expected: ValueShapeError{Predicate: "area", Want: ShapeScalar, Got: ShapeText},
		},
		{
			name: "refuses a claim carrying no value at all",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Value = Value{}
				return spec
			},
			expected: ValueShapeError{Predicate: "area", Want: ShapeScalar},
		},
		{
			name: "refuses a coordinate of the wrong dimension",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Predicate = "centroid"
				spec.Value = CoordinateValue([]float64{3.5, 1.25}, "m")
				return spec
			},
			expected: DimensionError{Predicate: "centroid", Want: 3, Got: 2},
		},
		{
			name: "refuses a unit other than the declared one",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Value = ScalarValue(24.2, "ft")
				return spec
			},
			expected: UnitError{Predicate: "area", Want: "m2", Got: "ft"},
		},
		{
			name: "refuses a dimensional value written with no unit",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Value = ScalarValue(24.2, "")
				return spec
			},
			expected: UnitError{Predicate: "area", Want: "m2"},
		},
		{
			name: "refuses a unit on a predicate which declares none",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Predicate = "occupancy"
				spec.Value = ScalarValue(12, "m2")
				return spec
			},
			expected: UnitError{Predicate: "occupancy", Got: "m2"},
		},
		{
			name: "refuses a claim with nothing evidencing it",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Source = ""
				return spec
			},
			expected: MissingChildError{Predicate: "area", Child: "source"},
		},
		{
			name: "refuses a claim which does not say how the value was obtained",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Method = ""
				return spec
			},
			expected: MissingChildError{Predicate: "area", Child: "method"},
		},
		{
			name: "refuses a method whose namespace nobody declared",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Method = "instrument:total-station"
				return spec
			},
			expected: UnknownAxisError{Axis: "namespace", Value: "instrument"},
		},
		{
			name: "refuses a systematic term whose namespace nobody declared",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Accuracy = []AccuracyTerm{{
					Kind: TermSystematic, Magnitude: 0.002, Unit: "m2", Source: "instrument:baseline",
				}}
				return spec
			},
			expected: UnknownAxisError{Axis: "namespace", Value: "instrument"},
		},
		{
			name: "refuses a claim about nothing",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Subject = ""
				return spec
			},
			expected: ErrNoSubject,
		},
		{
			name: "refuses a claim under no predicate",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Predicate = ""
				return spec
			},
			expected: ErrNoPredicate,
		},
		{
			name: "refuses a subject the model does not hold, naming the nearest",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.Subject = "site:S-1O1"
				return spec
			},
			expected: UnknownEntityError{ID: "site:S-1O1", Nearest: "site:S-101"},
		},
		{
			name: "refuses an id something already holds",
			spec: func(spec ClaimSpec) ClaimSpec {
				spec.ID = "site:M-0001"
				return spec
			},
			expected: TakenIDError{ID: "site:M-0001"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := recordFixture(t)
			spec := testCase.spec(aClaim())

			err := rejected(t, root, func(tx *Tx) error {
				_, _, err := tx.AddClaim(spec)
				return err
			})

			assertRefusal(t, err, testCase.expected)
		})
	}
}

// assertRefusal requires err to be the same refusal as expected, compared by
// type and by the fields which say what was refused.
//
// The message is never compared. What was expected, what was found and which
// axis they are about are the things a caller acts on, and the sentence around
// them is presentation.
func assertRefusal(t *testing.T, err, expected error) {
	t.Helper()

	switch want := expected.(type) {
	case UnknownAxisError:
		var got UnknownAxisError
		require.ErrorAs(t, err, &got)
		assert.Equal(t, want.Axis, got.Axis)
		assert.Equal(t, want.Value, got.Value)
		assert.NotEmpty(t, got.Permitted, "the refusal names what would have been permitted")

	case NotClaimBearingError:
		var got NotClaimBearingError
		require.ErrorAs(t, err, &got)
		assert.Equal(t, want.Predicate, got.Predicate)
		assert.NotEmpty(t, got.Written, "the refusal spells the plain value it wanted instead")

	case ValueShapeError:
		var got ValueShapeError
		require.ErrorAs(t, err, &got)
		assert.Equal(t, want, got)

	case DimensionError:
		var got DimensionError
		require.ErrorAs(t, err, &got)
		assert.Equal(t, want, got)

	case UnitError:
		var got UnitError
		require.ErrorAs(t, err, &got)
		assert.Equal(t, want, got)

	case MissingChildError:
		var got MissingChildError
		require.ErrorAs(t, err, &got)
		assert.Equal(t, want.Predicate, got.Predicate)
		assert.Equal(t, want.Child, got.Child)

	case UnknownEntityError:
		var got UnknownEntityError
		require.ErrorAs(t, err, &got)
		assert.Equal(t, want, got)

	case TakenIDError:
		var got TakenIDError
		require.ErrorAs(t, err, &got)
		assert.Equal(t, want.ID, got.ID)

	default:
		assert.ErrorIs(t, err, expected)
	}
}

func TestTxDeprecateClaim(t *testing.T) {
	root := recordFixture(t)

	graph := authored(t, root, func(tx *Tx) error {
		_, err := tx.DeprecateClaim("site:M-0001", "site:M-0002")
		return err
	})

	retracted, ok := graph.Claims().Claim("site:M-0001")
	require.True(t, ok)

	assert.Equal(t, RankDeprecated, retracted.Rank())

	replacement, ok := retracted.SupersededBy()
	require.True(t, ok)
	assert.Equal(t, ID("site:M-0002"), replacement)

	// Everything else it said is exactly what it said. Retracting is not
	// editing: the record of what was believed is the thing being kept.
	value, ok := retracted.Value().Scalar()
	require.True(t, ok)
	assert.Equal(t, 31.0, value)
	assert.Equal(t, "Design drawing DR-2026-004, Acme Architects", retracted.Source())
	assert.Equal(t, ID("method:estimate"), retracted.Method())
	assert.Equal(t, "2026-01-12", retracted.Date().Format(dateLayout))

	// And the chain is walkable in both directions from either end.
	current, ok := graph.Claims().Current(retracted)
	require.True(t, ok)

	id, ok := current.ID()
	require.True(t, ok)
	assert.Equal(t, ID("site:M-0002"), id)
}

// TestTxDeprecateClaimReportsASubjectLeftWithNoValue is its own function
// because the assertion is about what the model no longer says rather than
// about the claim which was retracted.
func TestTxDeprecateClaimReportsASubjectLeftWithNoValue(t *testing.T) {
	root := recordFixture(t)

	// Room C states its area twice, so retracting one of the two leaves the
	// pair with something still asserted and there is nothing to report.
	tx := begin(t, root)
	notices, err := tx.DeprecateClaim("site:M-0001", "site:M-0002")
	require.NoError(t, err)
	assert.Empty(t, notices, "a pair with a live claim left has a resolvable value")
	require.NoError(t, tx.Close())

	// Room D states its area once, and the claim standing in its place is a
	// measurement of another room — so the pair is left with nothing at all.
	after := authored(t, root, func(tx *Tx) error {
		notices, err = tx.DeprecateClaim("site:M-0003", "site:M-0002")
		return err
	})

	require.Len(t, notices, 1)
	assert.Equal(t, NoticeUnresolvable, notices[0].Kind)
	assert.Equal(t, ID("site:S-104"), notices[0].Subject)
	assert.Equal(t, "area", notices[0].Predicate)
	assert.Equal(t, ID("site:M-0003"), notices[0].Claim)

	assert.Empty(t, after.Claims().Live("site:S-104", "area"))
}

func TestTxDeprecateClaimRefusesWhatItCannotRetract(t *testing.T) {
	testCases := []struct {
		name         string
		id           ID
		supersededBy ID
		expected     error
	}{
		{
			name:         "refuses a deprecation which names nothing to stand in its place",
			id:           "site:M-0001",
			supersededBy: "",
			expected:     MissingReplacementError{ID: "site:M-0001"},
		},
		{
			name:         "refuses a claim named as its own replacement",
			id:           "site:M-0001",
			supersededBy: "site:M-0001",
			expected:     SelfSupersessionError{ID: "site:M-0001"},
		},
		{
			name:         "refuses a claim id nothing carries",
			id:           "site:M-0009",
			supersededBy: "site:M-0002",
			expected:     UnknownClaimError{ID: "site:M-0009"},
		},
		{
			name:         "refuses a replacement which names no claim",
			id:           "site:M-0001",
			supersededBy: "site:M-0009",
			expected:     UnknownClaimError{ID: "site:M-0009"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := rejected(t, recordFixture(t), func(tx *Tx) error {
				_, err := tx.DeprecateClaim(testCase.id, testCase.supersededBy)
				return err
			})

			switch want := testCase.expected.(type) {
			case MissingReplacementError:
				var got MissingReplacementError
				require.ErrorAs(t, err, &got)
				assert.Equal(t, want.ID, got.ID)

			case SelfSupersessionError:
				var got SelfSupersessionError
				require.ErrorAs(t, err, &got)
				assert.Equal(t, want.ID, got.ID)

			case UnknownClaimError:
				var got UnknownClaimError
				require.ErrorAs(t, err, &got)
				assert.Equal(t, want.ID, got.ID)
			}
		})
	}
}

// TestTxDeprecateClaimRefusesARetractionOfARetraction is its own function
// because the state it refuses is one the model is already in rather than one
// the arguments describe.
func TestTxDeprecateClaimRefusesARetractionOfARetraction(t *testing.T) {
	root := recordFixture(t)

	tx := begin(t, root)
	_, err := tx.DeprecateClaim("site:M-0001", "site:M-0002")
	require.NoError(t, err)

	_, _, err = tx.Commit()
	require.NoError(t, err)

	err = rejected(t, root, func(tx *Tx) error {
		_, err := tx.DeprecateClaim("site:M-0001", "site:M-0002")
		return err
	})

	var already AlreadyDeprecatedError
	require.ErrorAs(t, err, &already)
	assert.Equal(t, ID("site:M-0001"), already.ID)
	assert.Equal(t, ID("site:M-0002"), already.SupersededBy)
}

func TestTxSupersede(t *testing.T) {
	root := recordFixture(t)

	spec := aClaim()
	spec.Subject, spec.Value = "site:S-102", ScalarValue(24.6, "m2")
	spec.Source = "Re-measure RM-2026-002, Acme Surveys"

	var minted ID

	graph := authored(t, root, func(tx *Tx) error {
		id, _, err := tx.Supersede(spec)
		minted = id
		return err
	})

	// The new claim carries the id which was minted for it, which is the id the
	// retraction names — and it was minted because of that reference and for no
	// other reason.
	assert.Equal(t, ID("site:S-102:area:1"), minted)

	replacement, ok := graph.Claims().Claim(minted)
	require.True(t, ok)
	assert.Equal(t, RankNormal, replacement.Rank())

	value, ok := replacement.Value().Scalar()
	require.True(t, ok)
	assert.Equal(t, 24.6, value)

	// The claim it corrected is retracted in its favour, and says everything it
	// said before: correction is supersession, never an edit.
	retracted := slices.Collect(graph.Claims().Replaced(replacement))
	require.Len(t, retracted, 1)

	assert.Equal(t, RankDeprecated, retracted[0].Rank())

	was, ok := retracted[0].Value().Scalar()
	require.True(t, ok)
	assert.Equal(t, 24.2, was)
	assert.Equal(t, "Design drawing DR-2026-004, Acme Architects", retracted[0].Source())

	// The pair is not left in dispute: a retracted claim is never competing, so
	// the correction resolves it rather than doubling it.
	assert.Len(t, graph.Claims().Live("site:S-102", "area"), 1)

	for conflict := range graph.Claims().Conflicts() {
		assert.NotEqual(t, ID("site:S-102"), conflict.Subject(),
			"a correction is not a disagreement")
	}
}

func TestTxSupersedeRefusesWhatItCannotCorrect(t *testing.T) {
	testCases := []struct {
		name    string
		subject ID
		assert  func(t *testing.T, err error)
	}{
		{
			name:    "refuses a pair the model says nothing about",
			subject: "site:S-101",
			assert: func(t *testing.T, err error) {
				var got NothingToSupersedeError
				require.ErrorAs(t, err, &got)
				assert.Equal(t, ID("site:S-101"), got.Subject)
				assert.Equal(t, "area", got.Predicate)
			},
		},
		{
			name:    "refuses a pair the model states more than once, naming the competing claims",
			subject: "site:S-103",
			assert: func(t *testing.T, err error) {
				var got AmbiguousSupersessionError
				require.ErrorAs(t, err, &got)
				assert.Equal(t, ID("site:S-103"), got.Subject)
				assert.Equal(t, []string{"site:M-0001", "site:M-0002"}, got.Competing)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			spec := aClaim()
			spec.Subject = testCase.subject

			err := rejected(t, recordFixture(t), func(tx *Tx) error {
				_, _, err := tx.Supersede(spec)
				return err
			})

			testCase.assert(t, err)
		})
	}
}

// TestClaimsWrittenByTheSameTransactionCount walks the three questions a
// transaction asks about a claim id, each of which has to count the claims the
// same change has already written rather than only the ones it loaded.
//
// It is its own function because the property is about the transaction rather
// than about any one mutation: the graph a Tx holds is the model as it found it,
// so every question answered from the graph alone is wrong by exactly the claims
// the change is in the middle of writing.
func TestClaimsWrittenByTheSameTransactionCount(t *testing.T) {
	t.Run("retracts a claim in favour of one the same change wrote", func(t *testing.T) {
		root := recordFixture(t)

		graph := authored(t, root, func(tx *Tx) error {
			spec := aClaim()
			spec.ID, spec.Subject, spec.Value = "site:M-0100", "site:S-103", ScalarValue(31.2, "m2")

			if _, _, err := tx.AddClaim(spec); err != nil {
				return err
			}

			// The replacement is not in the model the transaction loaded, and
			// naming it is the whole of what the second step is for.
			_, err := tx.DeprecateClaim("site:M-0001", "site:M-0100")
			return err
		})

		replacement, ok := graph.Claims().Claim("site:M-0100")
		require.True(t, ok)

		replaced := slices.Collect(graph.Claims().Replaced(replacement))
		require.Len(t, replaced, 1)

		id, ok := replaced[0].ID()
		require.True(t, ok)
		assert.Equal(t, ID("site:M-0001"), id)
	})

	t.Run("refuses a second claim written under an id the same change took", func(t *testing.T) {
		err := rejected(t, recordFixture(t), func(tx *Tx) error {
			spec := aClaim()
			spec.ID = "site:M-0100"

			if _, _, err := tx.AddClaim(spec); err != nil {
				return err
			}

			spec.Subject = "site:S-102"
			_, _, err := tx.AddClaim(spec)
			return err
		})

		var taken TakenIDError
		require.ErrorAs(t, err, &taken)
		assert.Equal(t, ID("site:M-0100"), taken.ID)
	})

	t.Run("mints past an id the same change already wrote a claim under", func(t *testing.T) {
		tx := begin(t, recordFixture(t))
		defer func() { _ = tx.Close() }()

		spec := aClaim()
		spec.ID, spec.Subject = "site:S-102:area:1", "site:S-101"

		_, _, err := tx.AddClaim(spec)
		require.NoError(t, err)

		minted, err := tx.MintClaimID("site:S-102", "area")
		require.NoError(t, err)
		assert.Equal(t, ID("site:S-102:area:2"), minted)
	})
}

// TestMintClaimIDIsStableAndFree checks the half of the generated format which
// is a promise: the id is well formed, it says what it is derived from, and it
// never lands on something the model already holds.
func TestMintClaimIDIsStableAndFree(t *testing.T) {
	tx := begin(t, recordFixture(t))
	defer func() { _ = tx.Close() }()

	minted, err := tx.MintClaimID("site:S-102", "area")
	require.NoError(t, err)
	assert.Equal(t, ID("site:S-102:area:1"), minted)

	// Twice over the same model is the same answer, because nothing was
	// written between the two.
	again, err := tx.MintClaimID("site:S-102", "area")
	require.NoError(t, err)
	assert.Equal(t, minted, again)

	// What comes back is an id: the namespace is the subject's, which the
	// registry declares, and the whole of it reads back as one symbol.
	parsed, err := ParseID(string(minted))
	require.NoError(t, err)
	assert.Equal(t, minted, parsed)
	assert.Equal(t, "site", parsed.Namespace())

	// And it steps over an ordinal something already holds rather than
	// colliding with it.
	require.NoError(t, tx.AddNode(NodeSpec{
		ID:       "site:S-101:area:1",
		Kind:     KindSpace,
		Type:     "MeetingRoom",
		Geometry: GeometryArea,
	}, "entities/site.dfc"))

	next, err := tx.MintClaimID("site:S-101", "area")
	require.NoError(t, err)
	assert.Equal(t, ID("site:S-101:area:2"), next)
}

// TestNoAuthoringPathEditsAClaimValue is the assertion behind
// [0009](docs/decisions/0009-derived-values-are-never-written-back.md): there is
// no way through this package to change what a claim says.
//
// It walks the mutations rather than naming one, because the property is about
// the surface as a whole: a mutation added later which wrote over a value would
// be caught by this the day it was added, which is the only way a rule like this
// stays true.
func TestNoAuthoringPathEditsAClaimValue(t *testing.T) {
	mutations := map[string]func(tx *Tx) error{
		"AddClaim": func(tx *Tx) error {
			spec := aClaim()
			spec.Subject = "site:S-102"
			spec.Value = ScalarValue(99.9, "m2")

			_, _, err := tx.AddClaim(spec)
			return err
		},
		"Supersede": func(tx *Tx) error {
			spec := aClaim()
			spec.Subject = "site:S-102"
			spec.Value = ScalarValue(99.9, "m2")

			_, _, err := tx.Supersede(spec)
			return err
		},
		"DeprecateClaim": func(tx *Tx) error {
			_, err := tx.DeprecateClaim("site:M-0001", "site:M-0002")
			return err
		},
		"SetLabel": func(tx *Tx) error {
			return tx.SetLabel("site:S-102", "Board Room")
		},
		"Retire": func(tx *Tx) error {
			return tx.Retire("site:S-102", RetirementSpec{Reason: "Knocked through."})
		},
	}

	for name, mutate := range mutations {
		t.Run(name+" leaves every claim already written saying what it said", func(t *testing.T) {
			root := recordFixture(t)

			before, diags := LoadGraph(root)
			require.Empty(t, diags)

			was := stated(before)

			for written := range stated(authored(t, root, mutate)) {
				delete(was, written)
			}

			assert.Empty(t, was, "every claim which was there still says exactly what it said")
		})
	}
}

// stated is what every claim of a model says, keyed by everything about it a
// correction is forbidden to touch.
//
// The rank is deliberately not in the key: a retraction is the one thing a
// change may write onto a claim which was already there, and it says the claim
// was withdrawn rather than that it said something else.
func stated(graph *Graph) map[string]*Claim {
	out := make(map[string]*Claim)

	for claim := range graph.Claims().All() {
		var value strings.Builder

		if scalar, ok := claim.Value().Scalar(); ok {
			value.WriteString(decimal(scalar))
		}
		if components, ok := claim.Value().Coordinate(); ok {
			for _, component := range components {
				value.WriteString(decimal(component) + " ")
			}
		}
		if text, ok := claim.Value().Text(); ok {
			value.WriteString(text)
		}

		out[strings.Join([]string{
			string(claim.Subject()),
			claim.Predicate(),
			value.String(),
			string(claim.Value().Unit()),
			claim.Source(),
			string(claim.Method()),
			claim.Date().Format(dateLayout),
		}, "|")] = claim
	}

	return out
}

func TestParseValue(t *testing.T) {
	testCases := []struct {
		name      string
		written   string
		unit      Unit
		declared  Predicate
		expected  Value
		expectErr ValueProblem
	}{
		{
			name:     "reads a scalar and its unit",
			written:  "24.2",
			unit:     "m2",
			declared: Predicate{Shape: ShapeScalar, Unit: "m2"},
			expected: ScalarValue(24.2, "m2"),
		},
		{
			name:     "reads a whole number as the real it will be written as",
			written:  "24",
			unit:     "m2",
			declared: Predicate{Shape: ShapeScalar, Unit: "m2"},
			expected: ScalarValue(24, "m2"),
		},
		{
			name:     "reads the components of a coordinate in order",
			written:  "3.5 1.25 0",
			unit:     "m",
			declared: Predicate{Shape: ShapeCoordinate, Unit: "m", Dimension: 3},
			expected: CoordinateValue([]float64{3.5, 1.25, 0}, "m"),
		},
		{
			name:     "reads a coordinate as written where the predicate declares no dimension",
			written:  "3.5 1.25",
			declared: Predicate{Shape: ShapeCoordinate},
			expected: CoordinateValue([]float64{3.5, 1.25}, ""),
		},
		{
			name:     "reads text exactly as it stands",
			written:  "  sealed concrete  ",
			declared: Predicate{Shape: ShapeText},
			expected: TextValue("  sealed concrete  "),
		},
		{
			name:     "reads the empty string as the text value it is",
			written:  "",
			declared: Predicate{Shape: ShapeText},
			expected: TextValue(""),
		},
		{
			name:     "reads a transform as its thirteen reals in the order the form writes them",
			written:  "100 200 0 1 0 0 0 1 0 0 0 1 1",
			declared: Predicate{Shape: ShapeTransform},
			expected: TransformValue(Transform{
				Translation: [3]float64{100, 200, 0},
				Rotation:    [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1},
				Scale:       1,
			}),
		},
		{
			name:      "refuses a scalar which is not a number",
			written:   "twenty-four",
			declared:  Predicate{Shape: ShapeScalar},
			expectErr: ValueNotANumber,
		},
		{
			name:      "refuses a scalar written as more than one number",
			written:   "24.2 25.0",
			declared:  Predicate{Shape: ShapeScalar},
			expectErr: ValueWrongCount,
		},
		{
			name:      "refuses a coordinate of the wrong number of components",
			written:   "3.5 1.25",
			declared:  Predicate{Shape: ShapeCoordinate, Dimension: 3},
			expectErr: ValueWrongCount,
		},
		{
			name:      "refuses a transform of the wrong number of reals",
			written:   "100 200 0",
			declared:  Predicate{Shape: ShapeTransform},
			expectErr: ValueWrongCount,
		},
		{
			name:      "refuses a value under a predicate which declares no shape",
			written:   "24.2",
			declared:  Predicate{},
			expectErr: ValueNoShape,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := ParseValue(testCase.written, testCase.unit, testCase.declared)

			if testCase.expectErr != "" {
				var malformed MalformedValueError
				require.ErrorAs(t, err, &malformed)
				assert.Equal(t, testCase.expectErr, malformed.Reason)
				assert.Equal(t, testCase.written, malformed.Written)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

func TestParseAccuracyTerm(t *testing.T) {
	testCases := []struct {
		name      string
		written   string
		expected  AccuracyTerm
		expectErr TermProblem
	}{
		{
			name:     "reads an independent term",
			written:  "independent 0.05 m2",
			expected: AccuracyTerm{Kind: TermIndependent, Magnitude: 0.05, Unit: "m2"},
		},
		{
			name:    "reads a systematic term and the id it is shared with",
			written: "systematic 0.002 m survey:baseline",
			expected: AccuracyTerm{
				Kind: TermSystematic, Magnitude: 0.002, Unit: "m", Source: "survey:baseline",
			},
		},
		{
			name:      "refuses a kind which is neither",
			written:   "random 0.05 m2",
			expectErr: TermUnknownKind,
		},
		{
			name:      "refuses an independent term with an id it cannot share",
			written:   "independent 0.05 m2 survey:baseline",
			expectErr: TermWrongCount,
		},
		{
			name:      "refuses a systematic term which shares with nothing",
			written:   "systematic 0.002 m",
			expectErr: TermWrongCount,
		},
		{
			name:      "refuses a magnitude which is not a number",
			written:   "independent small m2",
			expectErr: TermNotANumber,
		},
		{
			name:      "refuses nothing at all",
			written:   "   ",
			expectErr: TermWrongCount,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := ParseAccuracyTerm(testCase.written)

			if testCase.expectErr != "" {
				var malformed MalformedTermError
				require.ErrorAs(t, err, &malformed)
				assert.Equal(t, testCase.expectErr, malformed.Reason)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestParseAccuracyTermRefusesATermIDWhichIsNotAnID is its own function because
// the refusal comes from the id production rather than from the shape of the
// term, and a caller telling the two apart is what the error types are for.
func TestParseAccuracyTermRefusesATermIDWhichIsNotAnID(t *testing.T) {
	_, err := ParseAccuracyTerm("systematic 0.002 m baseline")

	var malformed MalformedIDError
	require.ErrorAs(t, err, &malformed)
	assert.Equal(t, "baseline", malformed.Written)
	assert.Equal(t, IDUnqualified, malformed.Reason)
}

// TestClaimsLive walks the one question every correction asks: which claims of a
// pair are still asserted.
func TestClaimsLive(t *testing.T) {
	root := recordFixture(t)

	graph, diags := LoadGraph(root)
	require.Empty(t, diags)

	assert.Empty(t, graph.Claims().Live("site:S-101", "area"))
	assert.Len(t, graph.Claims().Live("site:S-102", "area"), 1)
	assert.Len(t, graph.Claims().Live("site:S-103", "area"), 2)

	// A retracted claim is never competing, so retracting one takes it out of
	// the answer rather than reordering it.
	after := authored(t, root, func(tx *Tx) error {
		_, err := tx.DeprecateClaim("site:M-0001", "site:M-0002")
		return err
	})

	live := after.Claims().Live("site:S-103", "area")
	require.Len(t, live, 1)

	id, ok := live[0].ID()
	require.True(t, ok)
	assert.Equal(t, ID("site:M-0002"), id)
}

// TestClaimAuthoringOnAFinishedTransaction checks that every mutation refuses a
// transaction which has already committed, rather than mutating a tree nothing
// will write.
func TestClaimAuthoringOnAFinishedTransaction(t *testing.T) {
	tx := begin(t, recordFixture(t))

	_, _, err := tx.Commit()
	require.NoError(t, err)

	_, _, err = tx.AddClaim(aClaim())
	assert.ErrorIs(t, err, ErrFinished)

	_, err = tx.DeprecateClaim("site:M-0001", "site:M-0002")
	assert.ErrorIs(t, err, ErrFinished)

	_, _, err = tx.Supersede(aClaim())
	assert.ErrorIs(t, err, ErrFinished)
}

// sliceOrNil is s, or nil where it is empty, so that a table can spell "nothing
// was reported" as nil rather than as an empty literal.
func sliceOrNil[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}

// entityFile is the entity file of the fixture as it stands.
func entityFile(t *testing.T, root string) string {
	t.Helper()

	return contents(t, root)["entities/site.dfc"]
}
