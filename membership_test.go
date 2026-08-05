// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The three membership fixtures: one garden, the same garden with a bed carved
// out of it, and a model with a corner of the survey nobody has tied in yet.
//
// The first two are one project surveyed once and drawn twice. Every observation
// file is byte for byte the same in both, which is what
// [TestObservationsWithinTouchesNoObservationFile] asserts and what makes every
// difference between their answers a difference in geometry.
const (
	yardFixture     = "yard"
	carvedFixture   = "carved"
	strandedFixture = "stranded"
)

// membershipPosition is the predicate the corners of these fixtures are read
// from.
const membershipPosition = "position"

// loadMembershipFixture loads one membership fixture, failing the test where the
// load reports anything.
//
// The stranded fixture is deliberately not loaded through here: its frame with
// no fit is a diagnostic, and that diagnostic is the situation it exists to put
// a membership query in.
func loadMembershipFixture(t *testing.T, name string) *Graph {
	t.Helper()

	graph, diags := LoadGraph(filepath.Join("testdata", "membership", name))
	require.NotNil(t, graph, "a load always yields a usable graph")
	require.Empty(t, renderGraphDiagnostics(t, diags), "the fixture loads clean")

	return graph
}

// membershipOf is the shots one region holds, failing the test where placing
// them reported anything.
func membershipOf(t *testing.T, graph *Graph, subject ID) Members {
	t.Helper()

	members, diags := graph.ObservationsWithin(subject, Derivation{
		Tolerance: closureTolerance,
		Position:  membershipPosition,
	})
	require.Empty(t, renderGraphDiagnostics(t, diags), "%s places its observations clean", subject)

	return members
}

// records is the identity of every record behind a set of results, in the order
// they came back.
func records(members []Membership) []string {
	out := make([]string, 0, len(members))
	for _, member := range members {
		out = append(out, string(member.Observation().ID))
	}
	return out
}

// TestObservationsWithin is the whole question in one table: given a boundary
// and a corpus, which shots are in it.
//
// The two fixtures are the same corpus, so the rows either side of the carve are
// directly comparable and every id which moves between them moved because a line
// was drawn.
func TestObservationsWithin(t *testing.T) {
	testCases := []struct {
		name      string
		fixture   string
		subject   ID
		inside    []string
		ambiguous []string
	}{
		{
			name:      "holds every shot inside the boundary, wherever it came from",
			fixture:   yardFixture,
			subject:   "site:S-yard",
			inside:    []string{"shot:0001", "shot:0002", "shot:0003", "shot:0004", "shot:0006", "shot:0009"},
			ambiguous: []string{"shot:0007"},
		},
		{
			name:      "gives up the shots which fall in the ground carved out of it",
			fixture:   carvedFixture,
			subject:   "site:S-yard",
			inside:    []string{"shot:0001", "shot:0003", "shot:0006"},
			ambiguous: []string{"shot:0004", "shot:0007"},
		},
		{
			name:      "gives a region drawn today the shots taken in it months ago",
			fixture:   carvedFixture,
			subject:   "site:S-bed",
			inside:    []string{"shot:0002", "shot:0009"},
			ambiguous: []string{"shot:0004"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			members := membershipOf(t, loadMembershipFixture(t, testCase.fixture), testCase.subject)

			assert.Equal(t, testCase.subject, members.Subject())
			assert.Equal(t, testCase.inside, records(members.Inside()))
			assert.Equal(t, testCase.ambiguous, records(members.Ambiguous()))
			assert.Equal(t, len(testCase.inside)+len(testCase.ambiguous), members.Len())
		})
	}
}

// TestObservationsWithinTouchesNoObservationFile is the half of the story the
// table above cannot state: that the reassignment cost no edit anywhere.
//
// It compares the bytes rather than the records. A test which parsed both and
// compared what it read back would pass just as happily if a line had been
// rewritten into an equivalent one, and "no observation file has to be touched"
// is a claim about the files.
func TestObservationsWithinTouchesNoObservationFile(t *testing.T) {
	files := []string{"2026-05-06-yard.obs", "2026-05-07-field.obs"}

	for _, name := range files {
		t.Run("is the same file either side of the carve: "+name, func(t *testing.T) {
			before, err := os.ReadFile(filepath.Join("testdata", "membership", yardFixture, "observations", name))
			require.NoError(t, err)

			after, err := os.ReadFile(filepath.Join("testdata", "membership", carvedFixture, "observations", name))
			require.NoError(t, err)

			assert.Equal(t, string(before), string(after))
		})
	}

	t.Run("holds shots in a region which cites no file at all", func(t *testing.T) {
		graph := loadMembershipFixture(t, carvedFixture)

		bed, held := graph.Node("site:S-bed")
		require.True(t, held, "the carved fixture holds the bed")
		require.Empty(t, bed.ObservedIn(), "the bed links no observation file")

		assert.Equal(t, []string{"shot:0002", "shot:0009"}, records(membershipOf(t, graph, "site:S-bed").Inside()))
	})
}

// TestObservationsWithinFollowsTheCarve asserts the reassignment as one
// statement about both regions, which is the property the two rows of the table
// above only imply.
//
// Every shot the garden held before the carve is held afterwards by exactly one
// of the two regions, or is reported as unplaceable by both. Nothing is lost and
// nothing belongs to both.
func TestObservationsWithinFollowsTheCarve(t *testing.T) {
	before := membershipOf(t, loadMembershipFixture(t, yardFixture), "site:S-yard")

	carved := loadMembershipFixture(t, carvedFixture)
	yard := membershipOf(t, carved, "site:S-yard")
	bed := membershipOf(t, carved, "site:S-bed")

	after := map[string]Members{"site:S-yard": yard, "site:S-bed": bed}

	inside, unplaceable := make(map[string][]string), make(map[string][]string)
	for subject, members := range after {
		for _, member := range members.Inside() {
			record := string(member.Observation().ID)
			inside[record] = append(inside[record], subject)
		}
		for _, member := range members.Ambiguous() {
			record := string(member.Observation().ID)
			unplaceable[record] = append(unplaceable[record], subject)
		}
	}

	for _, member := range before.Inside() {
		record := string(member.Observation().ID)

		t.Run("keeps "+record+" without giving it to two regions or to none", func(t *testing.T) {
			if len(unplaceable[record]) > 0 {
				assert.Empty(t, inside[record], "a shot no region can place is assigned to none of them")
				return
			}

			assert.Len(t, inside[record], 1, "%s is held by one region, not by both and not by neither", record)
		})
	}

	t.Run("gives the shot on the new line to neither and reports it to both", func(t *testing.T) {
		assert.ElementsMatch(t, []string{"site:S-yard", "site:S-bed"}, unplaceable["shot:0004"])
		assert.Empty(t, inside["shot:0004"], "and assigns it to neither")
	})
}

// TestObservationsWithinTellsAShotOfFromAShotIn is its own function because it
// asserts a different thing from where a shot fell: which of the two
// relationships each result is.
func TestObservationsWithinTellsAShotOfFromAShotIn(t *testing.T) {
	members := membershipOf(t, loadMembershipFixture(t, yardFixture), "site:S-yard")

	linked := make(map[string]bool)
	for _, member := range members.Inside() {
		linked[string(member.Observation().ID)] = member.Linked()
	}

	testCases := []struct {
		name     string
		record   string
		expected bool
	}{
		{
			name:     "reports a shot in a file the region cites as a shot of it",
			record:   "shot:0001",
			expected: true,
		},
		{
			name:     "reports a shot no region cites as one merely in the place",
			record:   "shot:0003",
			expected: false,
		},
		{
			name:     "does not make a shot cited by a corner a shot of the region that corner bounds",
			record:   "shot:0002",
			expected: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Contains(t, linked, testCase.record, "the garden holds %s", testCase.record)
			assert.Equal(t, testCase.expected, linked[testCase.record])
		})
	}
}

// TestObservationsWithinCarriesShotsIntoTheSubjectsFrame is its own function
// because a carried shot is asserted on differently: the coordinate judged is
// not the coordinate written, and what the carrying cost is part of the answer.
func TestObservationsWithinCarriesShotsIntoTheSubjectsFrame(t *testing.T) {
	graph := loadMembershipFixture(t, yardFixture)
	members := membershipOf(t, graph, "site:S-yard")

	var carried Membership
	for _, member := range members.Inside() {
		if member.Observation().ID == "shot:0006" {
			carried = member
		}
	}
	require.NotNil(t, carried.Observation(), "the garden holds the shot taken on the neighbouring grid")

	t.Run("judges it in the region's frame and not in its own", func(t *testing.T) {
		assert.True(t, carried.Carried())
		assert.Equal(t, ID("frame:annex"), carried.Observation().Frame)
		assert.Equal(t, ID("frame:site"), carried.Frame())
		assert.Equal(t, Point{5.0, 3.0, 0.0}, carried.Observation().Coordinate)
		assert.Equal(t, Point{17.0, 5.0, 0.0}, carried.At())
	})

	t.Run("carries the accuracy of the transform with the coordinate", func(t *testing.T) {
		uncertainty, err := carried.Budget().Combined()
		require.NoError(t, err)

		assert.InDelta(t, math.Hypot(0.006, 0.008), uncertainty.Standard(), 1e-12)
		assert.Equal(t, Unit("m"), uncertainty.Unit)
	})

	t.Run("widens the band at the boundary by what the transform cost", func(t *testing.T) {
		doubt, bounded := carried.Doubt()
		require.True(t, bounded)

		tolerance := carried.Tolerance()
		assert.Equal(t, closureTolerance, tolerance.Name)
		assert.InDelta(t,
			tolerance.Value+math.Hypot(carried.Observation().HorizontalPrecision, math.Hypot(0.006, 0.008)),
			doubt, 1e-12,
		)

		var alone Membership
		for _, member := range members.Inside() {
			if member.Observation().ID == "shot:0003" {
				alone = member
			}
		}
		require.NotNil(t, alone.Observation(), "the garden holds a shot taken on its own grid")

		near, _ := alone.Doubt()
		assert.Greater(t, doubt, near, "a carried shot is placed less well than one which was not")
	})
}

// TestObservationsWithinReportsShotsItCannotPlace is its own function because
// the assertion is about the *reason* a shot came back ambiguous, and the two
// reasons in the fixture are different: one shot is on the line, and one is off
// it by less than the instrument could see.
func TestObservationsWithinReportsShotsItCannotPlace(t *testing.T) {
	testCases := []struct {
		name      string
		fixture   string
		subject   ID
		record    string
		clearance float64
		doubt     float64
	}{
		{
			name:      "reports a shot on the line rather than assigning it to a side",
			fixture:   carvedFixture,
			subject:   "site:S-bed",
			record:    "shot:0004",
			clearance: 0,
			doubt:     0.005 + 0.012,
		},
		{
			name:      "reports a shot outside by less than its own precision rather than excluding it",
			fixture:   yardFixture,
			subject:   "site:S-yard",
			record:    "shot:0007",
			clearance: -0.100,
			doubt:     0.005 + 0.240,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			members := membershipOf(t, loadMembershipFixture(t, testCase.fixture), testCase.subject)

			var found Membership
			for _, member := range members.Ambiguous() {
				if string(member.Observation().ID) == testCase.record {
					found = member
				}
			}
			require.NotNil(t, found.Observation(), "%s is reported as unplaceable", testCase.record)

			assert.True(t, found.Ambiguous())
			assert.InDelta(t, testCase.clearance, found.Clearance(), 1e-9)

			doubt, bounded := found.Doubt()
			require.True(t, bounded)
			assert.InDelta(t, testCase.doubt, doubt, 1e-12)
			assert.LessOrEqual(t, math.Abs(found.Clearance()), doubt, "it is nearer the boundary than it is known to")

			assert.NotContains(t, records(members.Inside()), testCase.record, "and it was not quietly assigned as well")
		})
	}
}

// TestObservationsWithinIgnoresRetiredRecords is its own function because it is
// an assertion about which records are evidence at all, before any geometry is
// looked at.
func TestObservationsWithinIgnoresRetiredRecords(t *testing.T) {
	graph := loadMembershipFixture(t, yardFixture)

	log, diags := graph.AllObservations()
	require.Empty(t, renderGraphDiagnostics(t, diags), "the corpus reads clean")

	retired, written := log.Observation("shot:0008")
	require.True(t, written, "the record is in the log")
	require.True(t, log.Retired("shot:0008"), "and a later record retires it")
	require.Equal(t, Point{2.0, 2.0, 0.0}, retired.Coordinate, "at a coordinate well inside the garden")

	members := membershipOf(t, graph, "site:S-yard")

	assert.NotContains(t, records(members.Inside()), "shot:0008")
	assert.NotContains(t, records(members.Ambiguous()), "shot:0008")
}

// TestObservationsWithinIsDeterministic asserts the ordering rule rather than
// one ordering: the results come back sorted by record identity, and a second
// run of the same question gives the same slice.
func TestObservationsWithinIsDeterministic(t *testing.T) {
	graph := loadMembershipFixture(t, yardFixture)

	first := membershipOf(t, graph, "site:S-yard")

	t.Run("orders results by record identity", func(t *testing.T) {
		for _, members := range [][]Membership{first.Inside(), first.Ambiguous()} {
			assert.IsIncreasing(t, records(members))
		}
	})

	t.Run("gives the same answer to the same question", func(t *testing.T) {
		for range 4 {
			again := membershipOf(t, loadMembershipFixture(t, yardFixture), "site:S-yard")

			assert.Equal(t, records(first.Inside()), records(again.Inside()))
			assert.Equal(t, records(first.Ambiguous()), records(again.Ambiguous()))
		}
	})
}

// TestObservationsWithinUsesTheDerivedCache is its own function because what it
// asserts is not an answer but where the figure the answer was computed against
// came from.
func TestObservationsWithinUsesTheDerivedCache(t *testing.T) {
	graph := loadMembershipFixture(t, carvedFixture)

	cache, err := OpenCache(t.TempDir())
	require.NoError(t, err)

	against := Derivation{Tolerance: closureTolerance, Position: membershipPosition, Cache: cache}

	first, diags := graph.ObservationsWithin("site:S-yard", against)
	require.Empty(t, renderGraphDiagnostics(t, diags))
	require.Zero(t, cache.Stats().Hits, "the first question has nothing to hit")
	require.Positive(t, cache.Stats().Stores, "and stores what it derived")

	second, diags := graph.ObservationsWithin("site:S-bed", against)
	require.Empty(t, renderGraphDiagnostics(t, diags))

	assert.Positive(t, cache.Stats().Hits, "the second question reads the figure back rather than deriving it again")
	assert.Equal(t, []string{"shot:0001", "shot:0003", "shot:0006"}, records(first.Inside()))
	assert.Equal(t, []string{"shot:0002", "shot:0009"}, records(second.Inside()))
}

// TestObservationsWithinRefuses is the table of everything it will not answer,
// each of which is a diagnostic and not an empty result.
func TestObservationsWithinRefuses(t *testing.T) {
	testCases := []struct {
		name      string
		fixture   string
		subject   ID
		tolerance string
		message   string
	}{
		{
			name:      "names an id which is not a semantic node at all",
			fixture:   yardFixture,
			subject:   "geom:V-01",
			tolerance: closureTolerance,
			message:   "found no semantic node geom:V-01",
		},
		{
			name:      "names a tolerance the registry does not declare",
			fixture:   yardFixture,
			subject:   "site:S-yard",
			tolerance: "no-such-tolerance",
			message:   "no-such-tolerance",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			graph := loadMembershipFixture(t, testCase.fixture)

			members, diags := graph.ObservationsWithin(testCase.subject, Derivation{
				Tolerance: testCase.tolerance,
				Position:  membershipPosition,
			})

			assert.Zero(t, members.Len(), "a refusal answers nothing")
			require.NotEmpty(t, diags)
			assert.Contains(t, diags[0].Message, testCase.message)
			assert.Equal(t, SeverityError, diags[0].Severity)
		})
	}
}

// TestObservationsWithinReportsWhatItCannotReach is its own function because its
// fixture does not load clean, and that is the point of it: a frame whose fit
// has not been reduced is an ordinary state for a project to be in.
func TestObservationsWithinReportsWhatItCannotReach(t *testing.T) {
	graph, diags := LoadGraph(filepath.Join("testdata", "membership", strandedFixture))
	require.NotNil(t, graph)
	require.Len(t, diags, 1, "the frame nobody measured is the one thing the load reports")

	t.Run("places the shots it can and names the frame it cannot reach, once", func(t *testing.T) {
		members, diags := graph.ObservationsWithin("site:S-plot", Derivation{
			Tolerance: closureTolerance,
			Position:  membershipPosition,
		})

		assert.Equal(t, []string{"shot:0001"}, records(members.Inside()))

		require.Len(t, diags, 1, "two stranded records are one diagnostic about one frame")
		assert.Equal(t, SeverityError, diags[0].Severity)
		assert.Contains(t, diags[0].Message, "2 observation records written in frame:orphan")
		assert.Contains(t, diags[0].Message, "not related to frame:site")
	})

	t.Run("reports a shot carried across a fit nobody stated the accuracy of", func(t *testing.T) {
		members, _ := graph.ObservationsWithin("site:S-plot", Derivation{
			Tolerance: closureTolerance,
			Position:  membershipPosition,
		})

		require.Equal(t, []string{"shot:0004"}, records(members.Ambiguous()))

		unplaceable := members.Ambiguous()[0]
		assert.True(t, unplaceable.Carried())
		assert.InDelta(t, 4.0, unplaceable.Clearance(), 1e-9, "and it is four metres inside, not near the line")

		_, bounded := unplaceable.Doubt()
		assert.False(t, bounded, "a transform stating no accuracy bounds nothing")
		assert.False(t, unplaceable.Budget().Known(), "and the budget behind it says which claim is silent")

		assert.Contains(t, unplaceable.String(), "cannot be placed against site:S-plot")
		assert.Contains(t, unplaceable.String(), "by an amount nothing bounds")

		assert.NotContains(t, records(members.Inside()), "shot:0005")
		assert.NotContains(t, records(members.Ambiguous()), "shot:0005",
			"a shot on the same grid landing well clear of the plot is no result, unbounded or not")
	})

	t.Run("refuses a region which covers no area rather than reporting nothing is in it", func(t *testing.T) {
		members, diags := graph.ObservationsWithin("site:S-shed", Derivation{
			Tolerance: closureTolerance,
			Position:  membershipPosition,
		})

		assert.Zero(t, members.Len())
		require.NotEmpty(t, diags)
		assert.Contains(t, diags[len(diags)-1].Message, "site:S-shed covers no area")
	})
}

// TestMembershipString is about what a result says when it is printed, which is
// the rendering a command puts in front of somebody deciding whether to go back
// out with a receiver.
func TestMembershipString(t *testing.T) {
	members := membershipOf(t, loadMembershipFixture(t, yardFixture), "site:S-yard")

	rendered := make(map[string]string)
	for _, member := range append(members.Inside(), members.Ambiguous()...) {
		rendered[string(member.Observation().ID)] = member.String()
	}

	testCases := []struct {
		name     string
		record   string
		contains []string
	}{
		{
			name:     "says how far inside a shot fell and how well that is known",
			record:   "shot:0003",
			contains: []string{"shot:0003", "2.0 m inside site:S-yard", "known to 0.017 m"},
		},
		{
			name:     "says a shot it cannot place is near the boundary rather than inside it",
			record:   "shot:0007",
			contains: []string{"of the boundary of site:S-yard", "known to 0.245 m"},
		},
		{
			name:     "says which shots the region cites as its own",
			record:   "shot:0001",
			contains: []string{"it is a shot of it"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Contains(t, rendered, testCase.record)

			for _, want := range testCase.contains {
				assert.Contains(t, rendered[testCase.record], want)
			}
		})
	}

	t.Run("says nothing rather than panicking on a result which holds no record", func(t *testing.T) {
		assert.Equal(t, "no observation", Membership{}.String())
	})
}

// TestAllObservations is about the corpus a membership query is answered from:
// every file the model links, read once, as one log.
func TestAllObservations(t *testing.T) {
	graph := loadMembershipFixture(t, yardFixture)
	recorded := counted(t, graph)

	log, diags := graph.AllObservations()
	require.Empty(t, renderGraphDiagnostics(t, diags), "the corpus reads clean")

	t.Run("reads every file the model links to, once each", func(t *testing.T) {
		assert.Equal(t, []string{"2026-05-06-yard.obs", "2026-05-07-field.obs"}, recorded.names())
	})

	t.Run("holds every record of every one of them", func(t *testing.T) {
		var written []string
		for observation := range log.Observations() {
			written = append(written, string(observation.ID))
		}

		assert.Equal(t, []string{
			"shot:0001", "shot:0002", "shot:0003", "shot:0004",
			"shot:0005", "shot:0006", "shot:0007", "shot:0008", "shot:0009",
		}, written)
	})

	t.Run("does not read a file again for a second question", func(t *testing.T) {
		if _, diags := graph.AllObservations(); len(diags) > 0 {
			t.Fatalf("the corpus reads clean, got %d diagnostics", len(diags))
		}

		assert.Len(t, recorded.names(), 2)
	})

	t.Run("holds nothing for a graph which is not one", func(t *testing.T) {
		empty, diags := (*Graph)(nil).AllObservations()

		assert.Zero(t, empty.Len())
		assert.Empty(t, diags)
	})
}

// membershipRecords is how many shots the generated fixture below holds, chosen
// to be a season of field work rather than an afternoon of it.
const membershipRecords = 40000

// writeMembershipModel writes a model of one region linked to one observation
// file of records records, spread over a square four times the region's area so
// that most of them are outside it.
//
// It is generated rather than held in testdata for the reason the observation
// benchmark's fixture is: what it is for is its size, and a checked-in eight
// megabytes of synthetic shots would be paid for by every clone forever.
//
// Most of the shots being outside is deliberate. A corpus whose every record
// falls in the region under test would measure the point tests and never the
// rejection, and rejection is what a whole-model corpus is mostly made of.
func writeMembershipModel(t testing.TB, root string, records int) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Join(root, "observations"), 0o755))

	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
	}

	write("registry.dfc", `(project
  (label "Generated membership fixture")
  (globalid-namespace "https://example.org/models/generated"))

(namespace fix (description "Fix qualities an instrument reports."))
(namespace frame (description "Coordinate frames declared by this model."))
(namespace geom (description "Geometric nodes minted by this model."))
(namespace method (description "How a value was obtained."))
(namespace session (description "Field occupations."))
(namespace shot (description "Observation records issued by the field crew."))
(namespace site (description "Semantic nodes minted by this model."))

(predicate position
  (unit m)
  (shape coordinate)
  (dimension 3)
  (description "The location of a vertex in its frame."))

(frame frame:site (label "Site survey grid") (unit m))

(type Yard
  (kind Space)
  (geometry area)
  (description "Open ground around a dwelling."))

(tolerance boundary-closure
  (value 0.005 m)
  (description "How close two corners have to be to be one corner."))
`)

	var model strings.Builder
	corners := []Point{{0, 0, 0}, {100, 0, 0}, {100, 100, 0}, {0, 100, 0}}
	for i, corner := range corners {
		fmt.Fprintf(&model, `(vertex geom:V-%02d
  (label "Plot corner %d")
  (frame frame:site)
  (position
    (value (%.1f %.1f %.1f) m)
    (source "Site control set SC-01, Acme Surveys")
    (method method:total-station)
    (accuracy (independent 0.004 m))
    (date "2026-05-06")))
`, i+1, i+1, corner[0], corner[1], corner[2])
	}
	for i := range corners {
		fmt.Fprintf(&model, "(edge geom:E-%02d (label \"Plot side %d\") (frame frame:site) (vertices geom:V-%02d geom:V-%02d))\n",
			i+1, i+1, i+1, (i+1)%len(corners)+1)
	}
	model.WriteString(`(loop geom:L-01
  (label "Plot boundary")
  (frame frame:site)
  (edges geom:E-01 geom:E-02 geom:E-03 geom:E-04))
(node site:S-plot
  (label "The plot")
  (kind Space)
  (type Yard)
  (geometry area)
  (frame frame:site)
  (boundary geom:L-01)
  (observed-in "observations/field.obs"))
`)
	write("model.dfc", model.String())

	var out strings.Builder
	out.WriteString("# id at frame x y z method fix h-precision v-precision antenna session\n")
	for record := range records {
		// A lattice over a two hundred metre square, which is four times the
		// plot: a quarter of the shots land in it and the rest have to be
		// rejected.
		side := int(math.Ceil(math.Sqrt(float64(records))))
		x := float64(record%side) * 200 / float64(side)
		y := float64(record/side) * 200 / float64(side)

		fmt.Fprintf(&out,
			"obs shot:%06d 2026-05-06T09:14:22Z frame:site %.3f %.3f 0.000 method:gnss-rtk fix:rtk-fixed 0.012 0.021 2.000 session:2026-05-06-am\n",
			record, x, y)
	}
	write(filepath.Join("observations", "field.obs"), out.String())
}

// BenchmarkObservationsWithin is the cost of placing a season of field work
// against one boundary, with the derived geometry already cached and without.
//
// The two are separated because they answer different questions. A command
// asking about one region after another pays the derivation once and the point
// tests every time, which is the cached figure; a first run on a fresh checkout
// pays for both.
//
// Measured on 2026-08-05 with go1.26.2 on a Ryzen 9 5950X: forty thousand
// records placed against a four-cornered plot in about 11 ms, allocating about
// 19 MiB over 236 allocations — nearly all of it the ten thousand results
// themselves. Sixty-four records took 50 µs.
//
// The allocation count is the figure worth watching. It is flat in the size of
// the corpus because everything asked once per query is asked once per query:
// which records the subject cites, what each frame costs to come from, and the
// figure the shots are tested against. A change which pushes any of those inside
// the loop shows up here as allocations per record rather than as a slower
// number nobody can attribute.
func BenchmarkObservationsWithin(b *testing.B) {
	benchmarks := []struct {
		name    string
		records int
	}{
		{name: "an afternoon", records: 64},
		{name: "a season of field work", records: membershipRecords},
	}

	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			root := b.TempDir()
			writeMembershipModel(b, root, benchmark.records)

			graph, diags := LoadGraph(root)
			if len(diags) > 0 {
				b.Fatalf("the generated fixture loads clean, got %d diagnostics", len(diags))
			}

			cache, err := OpenCache(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}

			against := Derivation{Tolerance: closureTolerance, Position: membershipPosition, Cache: cache}

			// The first question is outside the loop so that reading the corpus
			// and deriving the figure are paid for once each, which is what a
			// command asking a second question pays.
			if _, diags := graph.ObservationsWithin("site:S-plot", against); len(diags) > 0 {
				b.Fatalf("the generated fixture places clean, got %d diagnostics", len(diags))
			}

			b.ReportAllocs()

			var held int
			for b.Loop() {
				members, _ := graph.ObservationsWithin("site:S-plot", against)
				held = members.Len()
			}

			b.ReportMetric(float64(held), "shots/op")
		})
	}
}
