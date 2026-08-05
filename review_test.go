// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reviewRegistry is the vocabulary the two revisions below are judged against.
const reviewRegistry = `(project
  (label "Review fixture")
  (globalid-namespace "https://example.org/models/review"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "Measurement methods used on this project."))
(namespace site (description "Semantic nodes minted by this model."))

(frame frame:building (label "Building local grid") (unit m))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings."))

(predicate area
  (unit m2)
  (shape scalar)
  (description "How much floor a space has."))

(predicate position
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "The location of a vertex in its frame."))
`

// reviewEntities is the semantic family of the merge base: one room bounded by
// a ring of walls, and one room bounded by nothing which states its area once.
//
// The second room is what the deprecation checks are run against: a subject
// with one live claim is a subject a retraction can leave with nothing at all,
// which is the state the model is allowed to be in and never the state to find
// out about later.
const reviewEntities = `(node site:S-101
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (boundary geom:L-01)
  (area
    (id site:M-0001)
    (value 12.0 m2)
    (source "As-built check AB-2026-009, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-05-06")))

(node site:S-102
  (label "Meeting Room B")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (area
    (id site:M-0003)
    (value 31.0 m2)
    (source "Design drawing DR-2026-004, Acme Architects")
    (method method:estimate)
    (accuracy (independent 0.5 m2))
    (date "2026-01-12")))
`

// reviewGeometry is the geometric family of the merge base: four corners, the
// four walls between them, and the ring they close.
const reviewGeometry = `(vertex geom:V-01
  (label "North-west corner")
  (frame frame:building)
  (position
    (value (0.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(vertex geom:V-02
  (label "North-east corner")
  (frame frame:building)
  (position
    (value (4.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(vertex geom:V-03
  (label "South-east corner")
  (frame frame:building)
  (position
    (value (4.0 3.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(vertex geom:V-04
  (label "South-west corner")
  (frame frame:building)
  (position
    (value (0.0 3.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

(edge geom:E-01 (label "North wall") (frame frame:building) (vertices geom:V-01 geom:V-02))

(edge geom:E-02 (label "East wall") (frame frame:building) (vertices geom:V-02 geom:V-03))

(edge geom:E-03 (label "South wall") (frame frame:building) (vertices geom:V-03 geom:V-04))

(edge geom:E-04 (label "West wall") (frame frame:building) (vertices geom:V-04 geom:V-01))

(loop geom:L-01
  (label "Meeting Room A outline")
  (frame frame:building)
  (edges geom:E-01 geom:E-02 geom:E-03 geom:E-04))
`

// The corner Meeting Room A's east wall meets, as the merge base states it and
// as three different changes restate it.
const (
	// reviewCornerBefore is the claim which says where the corner is, written
	// with no id of its own — which is the ordinary case, because nothing
	// references it.
	reviewCornerBefore = `(vertex geom:V-02
  (label "North-east corner")
  (frame frame:building)
  (position
    (value (4.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))`

	// reviewCornerRewritten is the same claim with a different number in it.
	// Everything which says where the number came from is unchanged, so the
	// model now asserts that the February survey found the new value.
	reviewCornerRewritten = `(vertex geom:V-02
  (label "North-east corner")
  (frame frame:building)
  (position
    (value (4.6 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))`

	// reviewCornerResurveyed is the corner moved the way the model is meant to
	// move one: the February claim keeps every word it said and is retracted,
	// and the new position arrives as a measurement of its own.
	reviewCornerResurveyed = `(vertex geom:V-02
  (label "North-east corner")
  (frame frame:building)
  (position
    (value (4.0 0.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")
    (rank deprecated)
    (superseded-by geom:P-0005))
  (position
    (id geom:P-0005)
    (value (4.6 0.0 0.0) m)
    (source "Interior control set IC-02, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-06-01")))`
)

// The ring of walls, as the merge base closes it and as a change redraws it.
const (
	reviewRingBefore = `(loop geom:L-01
  (label "Meeting Room A outline")
  (frame frame:building)
  (edges geom:E-01 geom:E-02 geom:E-03 geom:E-04))`

	// reviewRingRecornered cuts the corner: the ring runs back from the
	// south-east corner to where it started, and the south-west corner is no
	// longer part of the room. The corner itself and the walls which met at it
	// are still in the model, so this is a boundary which moved and nothing
	// which disappeared.
	reviewRingRecornered = `(edge geom:E-05 (label "Diagonal wall") (frame frame:building) (vertices geom:V-03 geom:V-01))

(loop geom:L-01
  (label "Meeting Room A outline")
  (frame frame:building)
  (edges geom:E-01 geom:E-02 geom:E-05))`
)

// The room which states its area once, as the merge base states it and as two
// changes retract it.
const (
	reviewRoomBefore = `  (area
    (id site:M-0003)
    (value 31.0 m2)
    (source "Design drawing DR-2026-004, Acme Architects")
    (method method:estimate)
    (accuracy (independent 0.5 m2))
    (date "2026-01-12")))`

	// reviewRoomRetracted retracts the only thing the model said about the
	// room's area in favour of a measurement of the other room, which is a
	// legal supersession and leaves this room's area unanswered.
	reviewRoomRetracted = `  (area
    (id site:M-0003)
    (value 31.0 m2)
    (source "Design drawing DR-2026-004, Acme Architects")
    (method method:estimate)
    (accuracy (independent 0.5 m2))
    (date "2026-01-12")
    (rank deprecated)
    (superseded-by site:M-0001)))`

	// reviewRoomSuperseded retracts it in favour of a measurement of the same
	// room, which is what a correction is.
	reviewRoomSuperseded = `  (area
    (id site:M-0003)
    (value 31.0 m2)
    (source "Design drawing DR-2026-004, Acme Architects")
    (method method:estimate)
    (accuracy (independent 0.5 m2))
    (date "2026-01-12")
    (rank deprecated)
    (superseded-by site:M-0004))
  (area
    (id site:M-0004)
    (value 31.4 m2)
    (source "As-built check AB-2026-011, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-05-06")))`
)

// removedRoom is the semantic family with the second room simply deleted,
// which takes the only claim about its area with it.
const removedRoom = `(node site:S-101
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (boundary geom:L-01)
  (area
    (id site:M-0001)
    (value 12.0 m2)
    (source "As-built check AB-2026-009, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-05-06")))
`

// swap is one fixture with one of its forms written differently, which is what
// a change to a model is. The form has to be there, so that a fixture edited
// later cannot leave a case silently comparing a revision with itself.
func swap(t *testing.T, src, before, after string) string {
	t.Helper()

	require.Contains(t, src, before, "the fixture no longer holds the form this case changes")

	return strings.Replace(src, before, after, 1)
}

// reviewBase is the merge base every case below is a change against.
func reviewBase() map[string]string {
	return map[string]string{
		"registry.dfc":          reviewRegistry,
		"entities/site.dfc":     reviewEntities,
		"entities/geometry.dfc": reviewGeometry,
	}
}

// revised is the merge base with some of its files written differently, which
// is what a change to a model is.
func revised(changes map[string]string) map[string]string {
	files := reviewBase()
	maps.Copy(files, changes)
	return files
}

// loadRevision loads one revision of a model out of a fixture tree.
func loadRevision(t *testing.T, files map[string]string) *Graph {
	t.Helper()

	graph, _ := LoadGraph(tree(t, files))
	require.NotNil(t, graph)

	return graph
}

// reviewed is the findings of comparing two revisions under the default policy,
// with nothing attributed.
func reviewed(t *testing.T, base, head map[string]string) []Finding {
	t.Helper()

	return Review(loadRevision(t, base), loadRevision(t, head), DefaultPolicy(), nil)
}

// summarised is each finding as "kind subject", which is what every case below
// asserts about: which check fired, and about what.
func summarised(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, finding := range findings {
		out = append(out, string(finding.Kind)+" "+string(finding.Subject))
	}
	return out
}

// addedRoom is a room, its measurement and a relabelling — a change which adds
// and renames and takes nothing away.
const addedRoom = `
(node site:S-103
  (label "Meeting Room C")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (area
    (id site:M-0005)
    (value 9.0 m2)
    (source "As-built check AB-2026-020, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.05 m2))
    (date "2026-06-02")))
`

func TestReview(t *testing.T) {
	testCases := []struct {
		name             string
		head             func(t *testing.T) map[string]string
		expectedFindings []string
	}{
		{
			name:             "reports nothing about a revision which changed nothing",
			head:             func(*testing.T) map[string]string { return reviewBase() },
			expectedFindings: []string{},
		},
		{
			name: "reports nothing about a change which adds a room, a measurement and a label",
			head: func(t *testing.T) map[string]string {
				return revised(map[string]string{
					"entities/site.dfc": swap(t, reviewEntities, `(label "Meeting Room B")`, `(label "Boardroom")`) + addedRoom,
				})
			},
			expectedFindings: []string{},
		},
		{
			name: "reports a corner whose position was rewritten inside the claim which stated it",
			head: func(t *testing.T) map[string]string {
				return revised(map[string]string{
					"entities/geometry.dfc": swap(t, reviewGeometry, reviewCornerBefore, reviewCornerRewritten),
				})
			},
			expectedFindings: []string{"boundary-moved-without-claim site:S-101"},
		},
		{
			name: "reports nothing when the corner which moved was measured again",
			head: func(t *testing.T) map[string]string {
				return revised(map[string]string{
					"entities/geometry.dfc": swap(t, reviewGeometry, reviewCornerBefore, reviewCornerResurveyed),
				})
			},
			expectedFindings: []string{},
		},
		{
			name: "reports a boundary drawn round different corners with nothing measured",
			head: func(t *testing.T) map[string]string {
				return revised(map[string]string{
					"entities/geometry.dfc": swap(t, reviewGeometry, reviewRingBefore, reviewRingRecornered),
				})
			},
			expectedFindings: []string{"boundary-moved-without-claim site:S-101"},
		},
		{
			name: "reports a retraction which leaves nothing asserted about a subject",
			head: func(t *testing.T) map[string]string {
				return revised(map[string]string{
					"entities/site.dfc": swap(t, reviewEntities, reviewRoomBefore, reviewRoomRetracted),
				})
			},
			expectedFindings: []string{"claim-deprecated-without-replacement site:S-102"},
		},
		{
			name: "reports nothing about a retraction which states the value standing in its place",
			head: func(t *testing.T) map[string]string {
				return revised(map[string]string{
					"entities/site.dfc": swap(t, reviewEntities, reviewRoomBefore, reviewRoomSuperseded),
				})
			},
			expectedFindings: []string{},
		},
		{
			name: "reports a node which was removed rather than retired, and the claim it took with it",
			head: func(*testing.T) map[string]string {
				return revised(map[string]string{"entities/site.dfc": removedRoom})
			},
			expectedFindings: []string{
				"id-disappeared-without-supersession site:M-0003",
				"id-disappeared-without-supersession site:S-102",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			findings := reviewed(t, reviewBase(), testCase.head(t))

			assert.Equal(t, testCase.expectedFindings, summarised(findings))
		})
	}
}

// TestReviewLeavesARetiredNodeAlone is its own function because the case needs
// a different merge base: the node has to have been retired and superseded
// before the change which removes it, which is not something a head revision
// can say.
func TestReviewLeavesARetiredNodeAlone(t *testing.T) {
	retired := `
(node site:S-104
  (label "The old store")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:building)
  (retired
    (date "2026-04-02")
    (reason "Merged into Meeting Room A.")
    (superseded-by site:S-101)))
`

	base := revised(map[string]string{"entities/site.dfc": reviewEntities + retired})

	t.Run("reports nothing when the record of where it went outlived it", func(t *testing.T) {
		findings := reviewed(t, base, reviewBase())

		assert.Empty(t, summarised(findings), "removing a node already retired and superseded is a tidy-up")
	})

	t.Run("reports a node retired with nowhere to go when it is then removed", func(t *testing.T) {
		unsuperseded := swap(t, base["entities/site.dfc"], "\n    (superseded-by site:S-101)", "")
		findings := reviewed(t, revised(map[string]string{"entities/site.dfc": unsuperseded}), reviewBase())

		assert.Equal(t, []string{"id-disappeared-without-supersession site:S-104"}, summarised(findings))
	})
}

// TestReviewReportsWhatWouldHaveJustifiedAMove is its own function because it
// asserts about what one finding says rather than about which findings there
// are: naming the boundary without naming the claim which would have accounted
// for it leaves the reviewer to work out what to ask for.
func TestReviewReportsWhatWouldHaveJustifiedAMove(t *testing.T) {
	findings := reviewed(t, reviewBase(), revised(map[string]string{
		"entities/geometry.dfc": swap(t, reviewGeometry, reviewCornerBefore, reviewCornerRewritten),
	}))

	require.Len(t, findings, 1)
	finding := findings[0]

	assert.Equal(t, FindingBoundaryMoved, finding.Kind)
	assert.Equal(t, ID("site:S-101"), finding.Subject)
	assert.Equal(t, SideHead, finding.Side)

	// The corner, the predicate and both values, so that the message says which
	// number moved and to what without anybody opening the file.
	assert.Contains(t, finding.Message, "geom:V-02")
	assert.Contains(t, finding.Message, "position")
	assert.Contains(t, finding.Message, "(4.0 0.0 0.0) m")
	assert.Contains(t, finding.Message, "(4.6 0.0 0.0) m")

	// And the claim which would have justified it, as the command which writes
	// one.
	assert.Contains(t, finding.Hint, "dfcad supersede geom:V-02 position")

	// The span points at the value which changed, in the file it changed in.
	assert.Equal(t, "entities/geometry.dfc", relativeName(t, finding.Span.Start.Path))
	assert.Positive(t, finding.Span.Start.Line)

	// And the one-line rendering leads with what the policy said about it,
	// because that is what a reader scanning a list of findings is sorting on.
	assert.Equal(t, "warning: boundary-moved-without-claim: "+finding.Message, finding.String())
}

// TestReviewNamesEveryDanglingReference is its own function because it asserts
// about a field only one check fills in, and about the case the check exists
// for: an id removed while the model still points at it.
func TestReviewNamesEveryDanglingReference(t *testing.T) {
	findings := reviewed(t, reviewBase(), revised(map[string]string{
		"entities/geometry.dfc": swap(t, reviewGeometry, `(vertex geom:V-04
  (label "South-west corner")
  (frame frame:building)
  (position
    (value (0.0 3.0 0.0) m)
    (source "Interior control set IC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-02-18")))

`, ""),
	}))

	var corner Finding
	for _, finding := range findings {
		if finding.Subject == "geom:V-04" {
			corner = finding
		}
	}

	require.Equal(t, FindingIDDisappeared, corner.Kind)
	assert.Equal(t, SideBase, corner.Side, "what head no longer holds can only be pointed at in the base")

	// Both walls which met at the corner still name it, and both are reported:
	// "still referenced" without saying by what leaves the author reading every
	// file to find out.
	from := make([]ID, 0, len(corner.Dangling))
	for _, dangling := range corner.Dangling {
		from = append(from, dangling.From)
		assert.Equal(t, "vertices", dangling.Relation)
	}
	assert.ElementsMatch(t, []ID{"geom:E-03", "geom:E-04"}, from)
}

// TestReviewAttributesEveryFindingToACommit is its own function because it
// drives the review with a history, which is a different shape of call from
// every case above.
func TestReviewAttributesEveryFindingToACommit(t *testing.T) {
	authored := Revision{
		SHA:     "5f2b8c1d9e3a47b6c0d1e2f3a4b5c6d7e8f90123",
		Summary: "story(site): widen Meeting Room A",
		Author:  "A Surveyor",
		Date:    time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
	}

	findings := Review(
		loadRevision(t, reviewBase()),
		loadRevision(t, revised(map[string]string{
			"entities/geometry.dfc": swap(t, reviewGeometry, reviewCornerBefore, reviewCornerRewritten),
		})),
		DefaultPolicy(),
		everything{commit: authored},
	)

	require.Len(t, findings, 1)
	assert.Equal(t, authored, findings[0].Commit)

	// The commit reaches the human rendering as well, because a reviewer
	// reading a terminal is asking the same question as one reading the JSON.
	assert.Contains(t, findings[0].Diagnostic().Message, "5f2b8c1d9e3a")
	assert.Contains(t, findings[0].Diagnostic().Message, "widen Meeting Room A")
}

// everything is a [History] which attributes every file to one commit.
type everything struct{ commit Revision }

// Introduced implements [History].
func (e everything) Introduced(string) (Revision, bool) { return e.commit, true }

// TestReviewIsDeterministic is its own function because it asserts about the
// order of the whole list rather than about what one finding says. A run whose
// output differs run to run makes every diff between two runs meaningless.
func TestReviewIsDeterministic(t *testing.T) {
	base, head := reviewBase(), revised(map[string]string{
		"entities/site.dfc":     removedRoom,
		"entities/geometry.dfc": swap(t, reviewGeometry, reviewCornerBefore, reviewCornerRewritten),
	})

	var first []string
	for run := range 4 {
		got := summarised(reviewed(t, base, head))

		if run == 0 {
			first = got
			require.NotEmpty(t, first)
			continue
		}
		assert.Equal(t, first, got, "two reviews of one change report it the same way")
	}

	// And the order is the one the checks are declared in, then by subject.
	assert.Equal(t, []string{
		"boundary-moved-without-claim site:S-101",
		"id-disappeared-without-supersession site:M-0003",
		"id-disappeared-without-supersession site:S-102",
	}, first)
}

func TestPolicy(t *testing.T) {
	testCases := []struct {
		name            string
		policy          Policy
		expectedRulings map[FindingKind]Ruling
	}{
		{
			name:   "fails what breaks a rule and warns about what needs an explanation",
			policy: DefaultPolicy(),
			expectedRulings: map[FindingKind]Ruling{
				FindingBoundaryMoved:   RulingWarning,
				FindingClaimDeprecated: RulingFailure,
				FindingIDDisappeared:   RulingFailure,
			},
		},
		{
			name:   "acknowledges a check without changing the others",
			policy: DefaultPolicy().With(FindingBoundaryMoved, RulingIgnored),
			expectedRulings: map[FindingKind]Ruling{
				FindingBoundaryMoved:   RulingIgnored,
				FindingClaimDeprecated: RulingFailure,
				FindingIDDisappeared:   RulingFailure,
			},
		},
		{
			name:   "falls back to the stated default for a check it does not name",
			policy: Policy{Default: RulingWarning},
			expectedRulings: map[FindingKind]Ruling{
				FindingBoundaryMoved:   RulingWarning,
				FindingClaimDeprecated: RulingWarning,
				FindingIDDisappeared:   RulingWarning,
			},
		},
		{
			name:   "answers a check it says nothing about at all, rather than allowing it",
			policy: Policy{},
			expectedRulings: map[FindingKind]Ruling{
				FindingBoundaryMoved:   RulingFailure,
				FindingClaimDeprecated: RulingFailure,
				FindingIDDisappeared:   RulingFailure,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			for _, kind := range FindingKinds() {
				assert.Equal(t, testCase.expectedRulings[kind], testCase.policy.Ruling(kind), string(kind))
			}
		})
	}
}

// TestReviewRulesEveryFindingByThePolicy is its own function because it asserts
// about what a policy does to a run rather than about what a policy says.
func TestReviewRulesEveryFindingByThePolicy(t *testing.T) {
	base, head := reviewBase(), revised(map[string]string{
		"entities/geometry.dfc": swap(t, reviewGeometry, reviewCornerBefore, reviewCornerRewritten),
	})

	t.Run("warns about a boundary which moved, so a justified change is not fought", func(t *testing.T) {
		findings := reviewed(t, base, head)

		require.Len(t, findings, 1)
		assert.Equal(t, RulingWarning, findings[0].Ruling)
		assert.Equal(t, SeverityWarning, findings[0].Diagnostic().Severity)
	})

	t.Run("keeps a finding it was told to ignore, rather than dropping it", func(t *testing.T) {
		policy := DefaultPolicy().With(FindingBoundaryMoved, RulingIgnored)

		findings := Review(loadRevision(t, base), loadRevision(t, head), policy, nil)

		require.Len(t, findings, 1, "a check switched off is one nobody remembers is off")
		assert.Equal(t, RulingIgnored, findings[0].Ruling)
	})

	t.Run("fails on a boundary which moved when the policy says so", func(t *testing.T) {
		policy := DefaultPolicy().With(FindingBoundaryMoved, RulingFailure)

		findings := Review(loadRevision(t, base), loadRevision(t, head), policy, nil)

		require.Len(t, findings, 1)
		assert.Equal(t, RulingFailure, findings[0].Ruling)
		assert.Equal(t, SeverityError, findings[0].Diagnostic().Severity)
	})
}

func TestParseFindingKind(t *testing.T) {
	for _, kind := range FindingKinds() {
		got, err := ParseFindingKind(string(kind))
		require.NoError(t, err)
		assert.Equal(t, kind, got)
	}

	_, err := ParseFindingKind("boundary-moved")

	var unknown UnknownFindingKindError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, "boundary-moved", unknown.Kind)
	assert.Equal(t, FindingKinds(), unknown.Known)
}

func TestParseRuling(t *testing.T) {
	for _, ruling := range Rulings() {
		got, err := ParseRuling(string(ruling))
		require.NoError(t, err)
		assert.Equal(t, ruling, got)
	}

	_, err := ParseRuling("error")

	var unknown UnknownRulingError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, "error", unknown.Ruling)
	assert.Equal(t, Rulings(), unknown.Known)
}

// TestReviewWithoutBothRevisions is its own function because it asserts about a
// call which cannot answer rather than about one which did.
func TestReviewWithoutBothRevisions(t *testing.T) {
	graph := loadRevision(t, reviewBase())

	assert.Empty(t, Review(nil, graph, DefaultPolicy(), nil))
	assert.Empty(t, Review(graph, nil, DefaultPolicy(), nil))
}

func TestHistories(t *testing.T) {
	first := Revision{SHA: "1111111111111111111111111111111111111111"}
	second := Revision{SHA: "2222222222222222222222222222222222222222"}

	histories := Histories{nil, only{name: "a.dfc", commit: first}, only{name: "b.dfc", commit: second}}

	got, ok := histories.Introduced("b.dfc")
	require.True(t, ok)
	assert.Equal(t, second, got)

	_, ok = histories.Introduced("c.dfc")
	assert.False(t, ok, "a path no history recognises is attributed to nothing")
}

// only is a [History] which knows about one file.
type only struct {
	name   string
	commit Revision
}

// Introduced implements [History].
func (o only) Introduced(name string) (Revision, bool) {
	if name != o.name {
		return Revision{}, false
	}
	return o.commit, true
}

func TestRevisionRendering(t *testing.T) {
	testCases := []struct {
		name             string
		revision         Revision
		expectedNamed    bool
		expectedRendered string
	}{
		{
			name:             "renders a commit as its short name and its subject",
			revision:         Revision{SHA: "5f2b8c1d9e3a47b6c0d1e2f3a4b5c6d7e8f90123", Summary: "story(site): widen a room"},
			expectedNamed:    true,
			expectedRendered: "5f2b8c1d9e3a story(site): widen a room",
		},
		{
			name:             "renders a commit with no subject as its short name alone",
			revision:         Revision{SHA: "5f2b8c1d9e3a47b6c0d1e2f3a4b5c6d7e8f90123"},
			expectedNamed:    true,
			expectedRendered: "5f2b8c1d9e3a",
		},
		{
			name:             "renders a finding nothing attributed as nothing at all",
			revision:         Revision{},
			expectedNamed:    false,
			expectedRendered: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expectedNamed, testCase.revision.Named())
			assert.Equal(t, testCase.expectedRendered, testCase.revision.String())
		})
	}
}

// relativeName is the last two path elements of a span's path, which is the
// fixture-relative name a test can assert about without knowing which temporary
// directory the revision was written into.
func relativeName(t *testing.T, path string) string {
	t.Helper()

	require.NotEmpty(t, path)

	elements := splitPath(path)
	if len(elements) < 2 {
		return path
	}
	return elements[len(elements)-2] + "/" + elements[len(elements)-1]
}

// splitPath splits a filesystem path into its elements, whichever separator it
// was written with.
func splitPath(path string) []string {
	var out []string
	current := ""
	for _, r := range path {
		if r == '/' || r == '\\' {
			if current != "" {
				out = append(out, current)
			}
			current = ""
			continue
		}
		current += string(r)
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}
