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

// The model the authoring tests change: a vocabulary with something to refuse
// on every axis, and a hierarchy deep enough that retiring something in the
// middle of it has referrers to report.
const (
	authorRegistry = `(project
  (label "Authoring fixture")
  (globalid-namespace "https://example.org/models/author"))

(namespace frame (description "Coordinate frames declared by this model."))
(namespace site (description "Semantic nodes minted by this model."))

(frame frame:site-grid (label "Site survey grid") (unit m))

(type Campus
  (kind Zone)
  (geometry absent)
  (description "A group of things administered together."))

(type Plot
  (kind Site)
  (geometry area)
  (description "The ground a project is built on."))

(type OfficeBuilding
  (kind Building)
  (geometry solid)
  (description "A building let as offices."))

(type Level
  (kind Storey)
  (geometry area)
  (description "One floor plate of a building."))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings."))
`

	authorModel = `(node site:P-01
  (label "Riverside plot")
  (kind Site)
  (type Plot)
  (geometry area)
  (frame frame:site-grid))

(node site:B-01
  (label "Block A")
  (kind Building)
  (type OfficeBuilding)
  (geometry solid)
  (frame frame:site-grid)
  (within site:P-01))

(node site:B-02
  (label "Block B")
  (kind Building)
  (type OfficeBuilding)
  (geometry solid)
  (frame frame:site-grid)
  (within site:P-01))

(node site:L-01
  (label "Level 1")
  (kind Storey)
  (type Level)
  (geometry area)
  (frame frame:site-grid)
  (within site:B-01)
  (member-of site:Z-01))

(node site:Z-01
  (label "West campus")
  (kind Zone)
  (type Campus))
`
)

// authorFixture writes the model the authoring tests change and returns its
// root.
func authorFixture(t *testing.T) string {
	t.Helper()

	return tree(t, map[string]string{
		"registry.dfc":      authorRegistry,
		"entities/site.dfc": authorModel,
	})
}

// authored applies one change to the fixture and returns the model it left
// behind, requiring the change to have been written.
func authored(t *testing.T, root string, change func(tx *Tx) error) *Graph {
	t.Helper()

	tx := begin(t, root)
	require.NoError(t, change(tx))

	out, diags, err := tx.Commit()
	require.NoError(t, err)
	require.Empty(t, diags)
	require.NotEmpty(t, out.Written())

	graph, found := LoadGraph(root)
	require.Empty(t, found)

	return graph
}

// rejected applies one change to the fixture and returns the error it was
// refused with, requiring nothing to have been written.
func rejected(t *testing.T, root string, change func(tx *Tx) error) error {
	t.Helper()

	before := contents(t, root)

	tx := begin(t, root)
	err := change(tx)
	require.Error(t, err)
	require.NoError(t, tx.Close())

	assert.Equal(t, before, contents(t, root), "a refused change writes nothing")

	return err
}

func TestTxAddNode(t *testing.T) {
	testCases := []struct {
		name     string
		spec     NodeSpec
		expected string
	}{
		{
			name: "writes every axis it was given",
			spec: NodeSpec{
				ID:       "site:S-101",
				Kind:     KindSpace,
				Type:     "MeetingRoom",
				Geometry: GeometryArea,
				Frame:    "frame:site-grid",
				Label:    "Meeting Room A",
			},
			expected: `(node
  site:S-101
  (label "Meeting Room A")
  (kind Space)
  (type MeetingRoom)
  (geometry area)
  (frame frame:site-grid))`,
		},
		{
			name:     "writes a node which has no geometry, no frame and no label",
			spec:     NodeSpec{ID: "site:Z-02", Kind: KindZone, Type: "Campus"},
			expected: `(node site:Z-02 (kind Zone) (type Campus))`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := authorFixture(t)

			graph := authored(t, root, func(tx *Tx) error {
				return tx.AddNode(testCase.spec, "entities/new.dfc")
			})

			node, ok := graph.Node(testCase.spec.ID)
			require.True(t, ok, "the model holds the node which was added")
			assert.Equal(t, testCase.spec.Label, node.Label())
			assert.Equal(t, testCase.spec.Kind, node.Kind())
			assert.Equal(t, testCase.spec.Type, node.Type())

			geometry, hasGeometry := node.Geometry()
			assert.Equal(t, testCase.spec.Geometry != "", hasGeometry)
			if hasGeometry {
				assert.Equal(t, testCase.spec.Geometry, geometry)
			}

			frame, hasFrame := node.Frame()
			assert.Equal(t, testCase.spec.Frame != "", hasFrame)
			if hasFrame {
				assert.Equal(t, testCase.spec.Frame, frame)
			}

			// The file it landed in is written in canonical form, which is what
			// says the axes were written as children rather than as text which
			// happened to read back.
			written, err := os.ReadFile(filepath.Join(root, "entities", "new.dfc"))
			require.NoError(t, err)
			assert.Equal(t, testCase.expected+"\n", string(written))
		})
	}
}

// TestTxAddNodeChecksEveryAxisAgainstTheRegistry is its own function because
// every case here asserts about a refusal and about the set it names, which is
// a different shape of assertion from the writes above.
func TestTxAddNodeChecksEveryAxisAgainstTheRegistry(t *testing.T) {
	testCases := []struct {
		name              string
		spec              NodeSpec
		expectedAxis      string
		expectedValue     string
		expectedPermitted []string
	}{
		{
			name:              "refuses an id whose namespace nothing declares",
			spec:              NodeSpec{ID: "plant:S-101", Kind: KindSpace, Type: "MeetingRoom", Geometry: GeometryArea},
			expectedAxis:      "namespace",
			expectedValue:     "plant",
			expectedPermitted: []string{"frame", "site"},
		},
		{
			name:              "refuses a kind which is not one of the seven",
			spec:              NodeSpec{ID: "site:S-101", Kind: "Room", Type: "MeetingRoom", Geometry: GeometryArea},
			expectedAxis:      "kind",
			expectedValue:     "Room",
			expectedPermitted: spellings(Kinds()),
		},
		{
			name:              "refuses a geometry form which is not one of the five",
			spec:              NodeSpec{ID: "site:S-101", Kind: KindSpace, Type: "MeetingRoom", Geometry: "polygon"},
			expectedAxis:      "geometry",
			expectedValue:     "polygon",
			expectedPermitted: spellings(Geometries()),
		},
		{
			name: "refuses a frame the registry does not declare",
			spec: NodeSpec{
				ID:       "site:S-101",
				Kind:     KindSpace,
				Type:     "MeetingRoom",
				Geometry: GeometryArea,
				Frame:    "frame:no-such-grid",
			},
			expectedAxis:      "frame",
			expectedValue:     "frame:no-such-grid",
			expectedPermitted: []string{"frame:site-grid"},
		},
		{
			name:              "refuses a type nothing declares",
			spec:              NodeSpec{ID: "site:S-101", Kind: KindSpace, Type: "BoardRoom", Geometry: GeometryArea},
			expectedAxis:      "type",
			expectedValue:     "BoardRoom",
			expectedPermitted: []string{"Campus", "Level", "MeetingRoom", "OfficeBuilding", "Plot"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := authorFixture(t)

			err := rejected(t, root, func(tx *Tx) error {
				return tx.AddNode(testCase.spec, "entities/new.dfc")
			})

			var unknown UnknownAxisError
			require.ErrorAs(t, err, &unknown)
			assert.Equal(t, testCase.expectedAxis, unknown.Axis)
			assert.Equal(t, testCase.expectedValue, unknown.Value)
			assert.Equal(t, testCase.expectedPermitted, unknown.Permitted)
		})
	}
}

func TestTxAddNodeChecksTheAxesAgainstTheType(t *testing.T) {
	testCases := []struct {
		name              string
		spec              NodeSpec
		expectedAxis      string
		expectedValue     string
		expectedPermitted []string
	}{
		{
			name:              "refuses a kind the type does not permit",
			spec:              NodeSpec{ID: "site:S-101", Kind: KindZone, Type: "MeetingRoom", Geometry: GeometryArea},
			expectedAxis:      "kind",
			expectedValue:     "Zone",
			expectedPermitted: []string{"Space"},
		},
		{
			name:              "refuses a geometry form the type does not permit",
			spec:              NodeSpec{ID: "site:S-101", Kind: KindSpace, Type: "MeetingRoom", Geometry: GeometrySolid},
			expectedAxis:      "geometry",
			expectedValue:     "solid",
			expectedPermitted: []string{"area"},
		},
		{
			name:              "refuses an omitted geometry the type requires",
			spec:              NodeSpec{ID: "site:S-101", Kind: KindSpace, Type: "MeetingRoom"},
			expectedAxis:      "geometry",
			expectedValue:     "",
			expectedPermitted: []string{"area"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := authorFixture(t)

			err := rejected(t, root, func(tx *Tx) error {
				return tx.AddNode(testCase.spec, "entities/new.dfc")
			})

			var permitted NotPermittedError
			require.ErrorAs(t, err, &permitted)
			assert.Equal(t, testCase.spec.Type, permitted.Type)
			assert.Equal(t, testCase.expectedAxis, permitted.Axis)
			assert.Equal(t, testCase.expectedValue, permitted.Value)
			assert.Equal(t, testCase.expectedPermitted, permitted.Permitted)
		})
	}
}

// TestTxAddNodeRefusesATakenID is its own function because the assertion is
// about where the thing which holds the id is defined, which none of the axis
// refusals has anything to say about.
func TestTxAddNodeRefusesATakenID(t *testing.T) {
	root := authorFixture(t)

	err := rejected(t, root, func(tx *Tx) error {
		return tx.AddNode(NodeSpec{
			ID:       "site:B-01",
			Kind:     KindBuilding,
			Type:     "OfficeBuilding",
			Geometry: GeometrySolid,
			Frame:    "frame:site-grid",
		}, "entities/new.dfc")
	})

	var taken TakenIDError
	require.ErrorAs(t, err, &taken)
	assert.Equal(t, ID("site:B-01"), taken.ID)
	assert.Equal(t, "a node", taken.What)
	assert.False(t, taken.Retired)
	assert.Equal(t, filepath.Join(root, "entities", "site.dfc"), taken.At.Start.Path)
	assert.Equal(t, 8, taken.At.Start.Line, "it points at where the existing node is defined")
}

// TestTxAddNodeNeverReissuesARetiredID is the other half of
// [0002](docs/decisions/0002-immutable-id-mutable-label.md): an id is not freed
// by the thing it named ceasing to exist.
func TestTxAddNodeNeverReissuesARetiredID(t *testing.T) {
	root := authorFixture(t)

	authored(t, root, func(tx *Tx) error {
		return tx.Retire("site:B-02", RetirementSpec{
			Date:   time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			Reason: "Never built.",
		})
	})

	err := rejected(t, root, func(tx *Tx) error {
		return tx.AddNode(NodeSpec{
			ID:       "site:B-02",
			Kind:     KindBuilding,
			Type:     "OfficeBuilding",
			Geometry: GeometrySolid,
			Frame:    "frame:site-grid",
		}, "entities/new.dfc")
	})

	var taken TakenIDError
	require.ErrorAs(t, err, &taken)
	assert.Equal(t, ID("site:B-02"), taken.ID)
	assert.True(t, taken.Retired, "the refusal says the id was retired rather than only that it is taken")
}

func TestTxAddNodeRefusesANodeWithNoID(t *testing.T) {
	root := authorFixture(t)

	err := rejected(t, root, func(tx *Tx) error {
		return tx.AddNode(NodeSpec{Kind: KindZone, Type: "Campus"}, "entities/new.dfc")
	})

	assert.ErrorIs(t, err, ErrNoID)
}

// TestTxSetLabel checks the one property a rename has to have: everything the
// model identifies the thing by is what it was.
func TestTxSetLabel(t *testing.T) {
	testCases := []struct {
		name          string
		label         string
		expectedLabel string
	}{
		{
			name:          "changes the label",
			label:         "Block One",
			expectedLabel: "Block One",
		},
		{
			name:          "removes the label when it is set to nothing",
			label:         "",
			expectedLabel: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := authorFixture(t)

			before, _ := LoadGraph(root)
			original, ok := before.Node("site:B-01")
			require.True(t, ok)

			originalGlobalID, ok := before.Registry().GlobalID(original.ID())
			require.True(t, ok)

			graph := authored(t, root, func(tx *Tx) error {
				return tx.SetLabel("site:B-01", testCase.label)
			})

			node, ok := graph.Node("site:B-01")
			require.True(t, ok)
			assert.Equal(t, testCase.expectedLabel, node.Label())

			// Identity, and everything derived from it, is untouched. A rename
			// which moved either would report the thing as deleted and a new
			// one created to everything downstream holding the old value.
			assert.Equal(t, original.ID(), node.ID())

			globalID, ok := graph.Registry().GlobalID(node.ID())
			require.True(t, ok)
			assert.Equal(t, originalGlobalID, globalID)

			// And nothing else about the node moved either.
			assert.Equal(t, original.Kind(), node.Kind())
			assert.Equal(t, original.Type(), node.Type())
			assert.Equal(t, original.MemberOf(), node.MemberOf())

			within, ok := node.Within()
			require.True(t, ok)
			assert.Equal(t, ID("site:P-01"), within)
		})
	}
}

func TestTxSetLabelRefusesAnIDNothingHolds(t *testing.T) {
	root := authorFixture(t)

	err := rejected(t, root, func(tx *Tx) error {
		return tx.SetLabel("site:B-Ol", "Block One")
	})

	var unknown UnknownEntityError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, ID("site:B-Ol"), unknown.ID)
	assert.Equal(t, ID("site:B-01"), unknown.Nearest, "a misspelling is answered with the id which was meant")
}

func TestTxRetire(t *testing.T) {
	root := authorFixture(t)

	graph := authored(t, root, func(tx *Tx) error {
		return tx.Retire("site:B-02", RetirementSpec{
			Date:   time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			Reason: "Never built.",
		})
	})

	node, ok := graph.Node("site:B-02")
	require.True(t, ok, "a retired node is still a node the model holds")

	retirement, ok := node.Retirement()
	require.True(t, ok)
	assert.Equal(t, "Never built.", retirement.Reason())
	assert.Equal(t, "2026-06-01", retirement.Date().Format(dateLayout))

	replacement, ok := retirement.SupersededBy()
	assert.False(t, ok, "a thing which stopped existing was not necessarily replaced")
	assert.Empty(t, replacement)

	// Everything else about it is what it was: retiring is not deleting.
	assert.Equal(t, "Block B", node.Label())
	assert.Equal(t, KindBuilding, node.Kind())
}

// TestTxRetireDatesTheChangeWhenItWasMade checks the one field which is not the
// caller's to leave out: a retirement carries the day it happened, and the
// common case is that it is happening now.
func TestTxRetireDatesTheChangeWhenItWasMade(t *testing.T) {
	root := authorFixture(t)

	// The day either side of the change, because a run which crosses midnight
	// UTC between them wrote the earlier of the two and is not a failure. Taking
	// "today" after the fact and requiring it to match is the assertion which
	// fails once a year for no reason anybody can reproduce.
	began := time.Now().UTC().Format(dateLayout)

	graph := authored(t, root, func(tx *Tx) error {
		return tx.Retire("site:B-02", RetirementSpec{Reason: "Never built."})
	})

	ended := time.Now().UTC().Format(dateLayout)

	node, ok := graph.Node("site:B-02")
	require.True(t, ok)

	retirement, ok := node.Retirement()
	require.True(t, ok)
	assert.Contains(t, []string{began, ended}, retirement.Date().Format(dateLayout))
}

func TestTxRetireRefusesWhatItCannotRetire(t *testing.T) {
	testCases := []struct {
		name     string
		id       ID
		spec     RetirementSpec
		expected error
	}{
		{
			name:     "refuses a retirement which says nothing about why",
			id:       "site:B-02",
			spec:     RetirementSpec{},
			expected: MissingReasonError{ID: "site:B-02"},
		},
		{
			name:     "refuses an id nothing answers to",
			id:       "site:B-99",
			spec:     RetirementSpec{Reason: "Never built."},
			expected: UnknownEntityError{ID: "site:B-99"},
		},
		{
			name:     "refuses a node replaced by itself",
			id:       "site:B-02",
			spec:     RetirementSpec{Reason: "Never built.", SupersededBy: "site:B-02"},
			expected: SelfReplacementError{ID: "site:B-02"},
		},
		{
			name:     "refuses a replacement nothing answers to",
			id:       "site:B-02",
			spec:     RetirementSpec{Reason: "Never built.", SupersededBy: "site:B-99"},
			expected: UnknownEntityError{ID: "site:B-99"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := authorFixture(t)

			err := rejected(t, root, func(tx *Tx) error {
				return tx.Retire(testCase.id, testCase.spec)
			})

			assert.IsType(t, testCase.expected, err)
		})
	}
}

// TestTxRetireRefusesANodeItWouldRetireTwice is its own function because the
// assertion is about what the model already said rather than about what the
// change asked for.
func TestTxRetireRefusesANodeItWouldRetireTwice(t *testing.T) {
	root := authorFixture(t)

	authored(t, root, func(tx *Tx) error {
		return tx.Retire("site:B-02", RetirementSpec{
			Date:   time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			Reason: "Never built.",
		})
	})

	err := rejected(t, root, func(tx *Tx) error {
		return tx.Retire("site:B-02", RetirementSpec{Reason: "Never built, again."})
	})

	var retired AlreadyRetiredError
	require.ErrorAs(t, err, &retired)
	assert.Equal(t, ID("site:B-02"), retired.ID)
	assert.Equal(t, "Never built.", retired.Reason)
	assert.Equal(t, "2026-06-01", retired.Date.Format(dateLayout))
}

// TestTxRetireRefusesANodeStillReferenced checks the refusal which keeps the
// model from holding a reference to something which says it stopped existing.
func TestTxRetireRefusesANodeStillReferenced(t *testing.T) {
	root := authorFixture(t)

	err := rejected(t, root, func(tx *Tx) error {
		return tx.Retire("site:B-01", RetirementSpec{Reason: "Demolished."})
	})

	var referenced ReferencedError
	require.ErrorAs(t, err, &referenced)
	assert.Equal(t, ID("site:B-01"), referenced.ID)

	referrers := make([]string, 0, len(referenced.By))
	for _, reference := range referenced.By {
		referrers = append(referrers, string(reference.From)+" "+reference.Relation)
	}
	assert.Equal(t, []string{"site:L-01 within"}, referrers)
}

// TestTxRetireRedirectsTheReferencesToTheReplacement is the other half of the
// refusal above: a replacement is what makes the references answerable, so the
// change which supplies one moves them.
func TestTxRetireRedirectsTheReferencesToTheReplacement(t *testing.T) {
	root := authorFixture(t)

	graph := authored(t, root, func(tx *Tx) error {
		return tx.Retire("site:B-01", RetirementSpec{
			Date:         time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			Reason:       "Demolished.",
			SupersededBy: "site:B-02",
		})
	})

	node, ok := graph.Node("site:B-01")
	require.True(t, ok)

	retirement, ok := node.Retirement()
	require.True(t, ok)

	replacement, ok := retirement.SupersededBy()
	require.True(t, ok)
	assert.Equal(t, ID("site:B-02"), replacement)

	// The storey which was inside the demolished block is inside the one which
	// replaced it, rather than inside something which says it is not there.
	storey, ok := graph.Node("site:L-01")
	require.True(t, ok)

	within, ok := storey.Within()
	require.True(t, ok)
	assert.Equal(t, ID("site:B-02"), within)

	// And nothing else about the referrer moved.
	assert.Equal(t, "Level 1", storey.Label())
	assert.Equal(t, []ID{"site:Z-01"}, storey.MemberOf())
}

// TestTxRetireRedirectsAZoneMembership is its own function because membership
// is written as a different child of a different shape from the containment
// above: any number of them, on any node, and never derived from where anything
// is.
func TestTxRetireRedirectsAZoneMembership(t *testing.T) {
	root := authorFixture(t)

	// A second zone for the membership to be redirected to, added by the same
	// API the test is about, which is one less way for the fixture and the
	// change to disagree.
	authored(t, root, func(tx *Tx) error {
		return tx.AddNode(NodeSpec{ID: "site:Z-02", Kind: KindZone, Type: "Campus"}, "entities/site.dfc")
	})

	graph := authored(t, root, func(tx *Tx) error {
		return tx.Retire("site:Z-01", RetirementSpec{
			Reason:       "Merged into the campus beside it.",
			SupersededBy: "site:Z-02",
		})
	})

	node, ok := graph.Node("site:L-01")
	require.True(t, ok)
	assert.Equal(t, []ID{"site:Z-02"}, node.MemberOf())
}

// TestTxRetireRefusesARetiredReplacement checks that a redirection never lands
// the references on something which also says it stopped existing.
func TestTxRetireRefusesARetiredReplacement(t *testing.T) {
	root := authorFixture(t)

	authored(t, root, func(tx *Tx) error {
		return tx.Retire("site:B-02", RetirementSpec{Reason: "Never built."})
	})

	err := rejected(t, root, func(tx *Tx) error {
		return tx.Retire("site:B-01", RetirementSpec{Reason: "Demolished.", SupersededBy: "site:B-02"})
	})

	var retired AlreadyRetiredError
	require.ErrorAs(t, err, &retired)
	assert.Equal(t, ID("site:B-02"), retired.ID)
}

// TestTxRetireRefusesAnIDWhichIsNotANode checks that the family the id belongs
// to is what decides, rather than the id resolving to something.
func TestTxRetireRefusesAnIDWhichIsNotANode(t *testing.T) {
	root := tree(t, map[string]string{
		"registry.dfc":      authorRegistry,
		"entities/site.dfc": authorModel + "\n(vertex site:V-01 (frame frame:site-grid))\n",
	})

	err := rejected(t, root, func(tx *Tx) error {
		return tx.Retire("site:V-01", RetirementSpec{Reason: "Moved."})
	})

	var family NotANodeError
	require.ErrorAs(t, err, &family)
	assert.Equal(t, ID("site:V-01"), family.ID)
	assert.Equal(t, "vertex", family.Family)
}

// TestTxAuthoringIsRefusedOnceCommitted checks that every authoring call is one
// change and one use, the way the mutations they are built on are.
func TestTxAuthoringIsRefusedOnceCommitted(t *testing.T) {
	root := authorFixture(t)

	tx := begin(t, root)
	_, _, err := tx.Commit()
	require.NoError(t, err)

	assert.ErrorIs(t, tx.AddNode(NodeSpec{ID: "site:Z-02", Kind: KindZone, Type: "Campus"}, "a.dfc"), ErrFinished)
	assert.ErrorIs(t, tx.SetLabel("site:B-01", "Block One"), ErrFinished)
	assert.ErrorIs(t, tx.Retire("site:B-02", RetirementSpec{Reason: "Never built."}), ErrFinished)
}

// TestTxRetireIsRefusedByTheModelItWouldProduce checks that a redirection which
// produces something the hierarchy does not permit is refused at the commit,
// with the diagnostics a load of the result would have raised, rather than
// being caught by a second copy of the rules here.
func TestTxRetireIsRefusedByTheModelItWouldProduce(t *testing.T) {
	root := authorFixture(t)

	tx := begin(t, root)

	// The plot is what the blocks are written within, so redirecting the
	// references to a storey puts a building inside a storey.
	require.NoError(t, tx.Retire("site:P-01", RetirementSpec{
		Reason:       "Sold.",
		SupersededBy: "site:L-01",
	}))

	out, diags, err := tx.Commit()
	require.NoError(t, err)
	assert.Empty(t, out.Files, "a refused change describes nothing")

	var collected Diagnostics
	collected.Add(diags...)
	assert.True(t, collected.HasErrors(), "the change was refused")

	// The refusal is the one a load of the result would have raised, pointing
	// at the reference the redirection rewrote rather than at the retirement.
	assert.True(t, slices.ContainsFunc(diags, func(diagnostic Diagnostic) bool {
		return strings.Contains(diagnostic.Message, "expected a kind the hierarchy permits to contain a Building")
	}), "the diagnostics say what the redirection produced: %v", diags)
}

func TestGraphReferences(t *testing.T) {
	testCases := []struct {
		name     string
		id       ID
		expected []string
	}{
		{
			name:     "reports what is written within a node",
			id:       "site:B-01",
			expected: []string{"site:L-01 within"},
		},
		{
			name:     "reports what is a member of a zone",
			id:       "site:Z-01",
			expected: []string{"site:L-01 member-of"},
		},
		{
			name:     "reports every reference to one node together",
			id:       "site:P-01",
			expected: []string{"site:B-01 within", "site:B-02 within"},
		},
		{
			name:     "reports nothing for a node nothing points at",
			id:       "site:L-01",
			expected: nil,
		},
		{
			name:     "reports nothing for an id nothing holds",
			id:       "site:B-99",
			expected: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			graph, diags := LoadGraph(authorFixture(t))
			require.Empty(t, diags)

			var referrers []string
			for reference := range graph.References(testCase.id) {
				referrers = append(referrers, string(reference.From)+" "+reference.Relation)
			}

			assert.Equal(t, testCase.expected, referrers)
		})
	}
}
