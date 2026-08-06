// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// batched parses one operation file, requiring it to be read.
func batched(t *testing.T, written string) Batch {
	t.Helper()

	batch, err := ParseBatch(strings.NewReader(written))
	require.NoError(t, err)

	return batch
}

// batchApplied begins a transaction against a copy of the graph fixture, applies the
// batch and commits it, requiring every step to succeed.
//
// It returns what each operation did, what the commit did and the model root,
// which are the three things a test about a batch asks after.
func batchApplied(t *testing.T, written string) ([]Applied, Commit, string) {
	t.Helper()

	root := copied(t, "testdata/graph/valid")

	tx, diags, err := Begin(root)
	require.NoError(t, err)
	require.Empty(t, diags)
	defer tx.Close()

	out, err := tx.Apply(batched(t, written))
	require.NoError(t, err)

	commit, refused, err := tx.Commit()
	require.NoError(t, err)
	require.Empty(t, refused)

	return out, commit, root
}

// copied is a writable copy of a fixture tree, which is what a transaction
// needs: it writes, and a fixture is read by every other test in this package.
func copied(t *testing.T, fixture string) string {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.CopyFS(root, os.DirFS(fixture)))

	return root
}

// files is every entity file beneath root, by its path relative to root.
func files(t *testing.T, root string) map[string]string {
	t.Helper()

	out := make(map[string]string)

	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		out[filepath.ToSlash(rel)] = string(src)

		return nil
	}))

	return out
}

// touched is each file a commit changed, named relative to the model root so
// that a test asserts on the file rather than on where the fixture was copied.
func touched(t *testing.T, root string, commit Commit) []string {
	t.Helper()

	out := make([]string, 0, len(commit.Files))

	for _, file := range commit.Files {
		rel, err := filepath.Rel(root, file.Path)
		require.NoError(t, err)

		out = append(out, filepath.ToSlash(rel))
	}

	return out
}

// spelledEffects is each effect as "op tag id", which is what a test about a
// batch asserts on: what was created, modified or retired, and in which order.
func spelledEffects(effects []Effect) []string {
	out := make([]string, 0, len(effects))
	for _, effect := range effects {
		// One of the two names an effect can carry is always empty: an entity
		// has an id and a registry entry has a name.
		out = append(out, string(effect.Op)+" "+effect.Tag+" "+string(effect.ID)+effect.Name)
	}
	return out
}

func TestParseBatch(t *testing.T) {
	testCases := []struct {
		name               string
		written            string
		expectedVersion    int
		expectedOperations []string
	}{
		{
			name: "reads every operation in the order it was written",
			written: `{"version": 1, "operations": [
				{"op": "add-node", "id": "site:S-201"},
				{"op": "add-claim", "subject": "site:S-201", "predicate": "width",
				 "claim": {"value": "0.1"}},
				{"op": "set-label", "id": "site:S-201", "label": "Room"}
			]}`,
			expectedVersion:    1,
			expectedOperations: []string{"add-node", "add-claim", "set-label"},
		},
		{
			name:               "reads a file which does not say which version it was written against",
			written:            `{"operations": [{"op": "retire", "id": "site:S-201", "reason": "Never built."}]}`,
			expectedVersion:    BatchVersion,
			expectedOperations: []string{"retire"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			batch := batched(t, testCase.written)

			assert.Equal(t, testCase.expectedVersion, batch.Version)

			written := make([]string, 0, len(batch.Operations))
			for _, operation := range batch.Operations {
				written = append(written, operation.Name())
			}
			assert.Equal(t, testCase.expectedOperations, written)
		})
	}
}

// TestParseBatchReadsEveryOperationsAxes is its own function because it asserts
// about the axes each operation carries rather than about which operations were
// read, and the axes differ per operation.
func TestParseBatchReadsEveryOperationsAxes(t *testing.T) {
	value := "12.0 0.0 0.0"

	testCases := []struct {
		name     string
		written  string
		expected Operation
	}{
		{
			name: "reads a node with every axis",
			written: `{"op": "add-node", "id": "site:S-201", "kind": "Space", "type": "MeetingRoom",
			           "geometry": "area", "frame": "frame:building", "label": "Room", "file": "a.dfc"}`,
			expected: &AddNodeOperation{
				ID: "site:S-201", Kind: "Space", Type: "MeetingRoom",
				Geometry: "area", Frame: "frame:building", Label: "Room", File: "a.dfc",
			},
		},
		{
			name: "reads a vertex with the claim which says where it is",
			written: `{"op": "add-vertex", "id": "geom:V-07", "frame": "frame:building",
			           "predicate": "position",
			           "claim": {"value": "12.0 0.0 0.0", "unit": "m", "source": "IC-01",
			                     "method": "method:total-station",
			                     "accuracy": ["independent 0.004 m"], "date": "2026-02-18"}}`,
			expected: &AddVertexOperation{
				ID: "geom:V-07", Frame: "frame:building", Predicate: "position",
				Claim: ClaimAxes{
					Value: &value, Unit: "m", Source: "IC-01",
					Method: "method:total-station", Accuracy: []string{"independent 0.004 m"},
					Date: "2026-02-18",
				},
			},
		},
		{
			name: "reads an edge with both of its ends and what backs it",
			written: `{"op": "add-edge", "id": "geom:E-08", "frame": "frame:building",
			           "start": "geom:V-05", "end": "geom:V-06", "backedBy": ["site:E-01"]}`,
			expected: &AddEdgeOperation{
				ID: "geom:E-08", Frame: "frame:building",
				Start: "geom:V-05", End: "geom:V-06", BackedBy: []string{"site:E-01"},
			},
		},
		{
			name:     "reads a loop as the ring it is traversed through",
			written:  `{"op": "add-loop", "id": "geom:L-03", "frame": "frame:building", "edges": ["geom:E-01", "geom:E-02"]}`,
			expected: &AddLoopOperation{ID: "geom:L-03", Frame: "frame:building", Edges: []string{"geom:E-01", "geom:E-02"}},
		},
		{
			name: "reads a scaffold with its corners and the tolerance which judges them",
			written: `{"op": "scaffold-loop", "namespace": "geom", "frame": "frame:building",
			           "predicate": "position", "tolerance": "boundary-closure",
			           "corners": ["0 0 0", "1 0 0", "0 0 0"], "noSnap": true}`,
			expected: &ScaffoldLoopOperation{
				Namespace: "geom", Frame: "frame:building", Predicate: "position", Tolerance: "boundary-closure",
				Corners: []string{"0 0 0", "1 0 0", "0 0 0"}, NoSnap: true,
			},
		},
		{
			name:     "reads a classification as the opaque pair it is",
			written:  `{"op": "classify-type", "type": "Campus", "system": "IFC4", "code": "IfcZone"}`,
			expected: &ClassifyTypeOperation{Type: "Campus", System: "IFC4", Code: "IfcZone"},
		},
		{
			name:     "reads a retirement with the node which stands in its place",
			written:  `{"op": "retire", "id": "site:S-201", "reason": "Never built.", "replacement": "site:S-202", "date": "2026-05-06"}`,
			expected: &RetireOperation{ID: "site:S-201", Reason: "Never built.", Replacement: "site:S-202", Date: "2026-05-06"},
		},
		{
			name:     "reads a deprecation with the claim which replaces the retracted one",
			written:  `{"op": "deprecate-claim", "claim": "survey:W-0002", "supersededBy": "survey:W-0003"}`,
			expected: &DeprecateClaimOperation{Claim: "survey:W-0002", SupersededBy: "survey:W-0003"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			batch := batched(t, `{"operations": [`+testCase.written+`]}`)

			require.Len(t, batch.Operations, 1)
			assert.Equal(t, testCase.expected, batch.Operations[0])
		})
	}
}

// TestParseBatchRefusesAFileWhichIsNotABatch is its own function because it is
// about the file rather than about an operation of it: none of these produces
// an operation to name.
func TestParseBatchRefusesAFileWhichIsNotABatch(t *testing.T) {
	testCases := []struct {
		name          string
		written       string
		expectedError error
	}{
		{
			name:          "refuses a batch with no operation in it",
			written:       `{"operations": []}`,
			expectedError: ErrNoOperations,
		},
		{
			name:          "refuses a file with a second object after the batch",
			written:       `{"operations": [{"op": "add-node", "id": "site:S-201"}]}{"operations": []}`,
			expectedError: ErrExtraInput,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParseBatch(strings.NewReader(testCase.written))

			assert.ErrorIs(t, err, testCase.expectedError)
		})
	}
}

// TestParseBatchRefusesAVersionItDoesNotRead checks that the refusal says which
// version was asked for and which one is read, so that a caller branches on the
// numbers rather than on the message.
func TestParseBatchRefusesAVersionItDoesNotRead(t *testing.T) {
	_, err := ParseBatch(strings.NewReader(`{"version": 2, "operations": []}`))

	var unknown UnknownBatchVersionError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, 2, unknown.Version)
	assert.Equal(t, BatchVersion, unknown.Known)
}

// TestParseBatchRefusesAFileWhichIsNotJSON checks that the decoder's own error
// comes back rather than being flattened into one of this package's, because it
// carries the offset the file went wrong at.
func TestParseBatchRefusesAFileWhichIsNotJSON(t *testing.T) {
	_, err := ParseBatch(strings.NewReader(`{"operations": [`))

	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrNoOperations)
}

func TestParseBatchNamesTheOperationEachProblemIsAbout(t *testing.T) {
	testCases := []struct {
		name              string
		written           string
		expectedIndex     int
		expectedOperation string
		expectedError     error
	}{
		{
			name:              "refuses an operation nothing declares",
			written:           `{"op": "add-widget", "id": "site:S-201"}`,
			expectedIndex:     2,
			expectedOperation: "add-widget",
		},
		{
			name:          "refuses an operation which does not say which it is",
			written:       `{"id": "site:S-201"}`,
			expectedIndex: 2,
		},
		{
			name:              "refuses a node with no id to write it under",
			written:           `{"op": "add-node", "kind": "Space"}`,
			expectedIndex:     2,
			expectedOperation: "add-node",
			expectedError:     ErrNoID,
		},
		{
			name:              "refuses an edge with only one end",
			written:           `{"op": "add-edge", "id": "geom:E-08", "frame": "frame:building", "start": "geom:V-01"}`,
			expectedIndex:     2,
			expectedOperation: "add-edge",
			expectedError:     ErrNoEndpoints,
		},
		{
			name:              "refuses a loop with no edges to traverse",
			written:           `{"op": "add-loop", "id": "geom:L-03", "frame": "frame:building"}`,
			expectedIndex:     2,
			expectedOperation: "add-loop",
			expectedError:     ErrNoEdges,
		},
		{
			name:              "refuses a scaffold with no corners",
			written:           `{"op": "scaffold-loop", "namespace": "geom", "frame": "frame:building", "predicate": "position", "tolerance": "boundary-closure"}`,
			expectedIndex:     2,
			expectedOperation: "scaffold-loop",
			expectedError:     ErrNoCorners,
		},
		{
			name:              "refuses a retirement which says nothing about why",
			written:           `{"op": "retire", "id": "site:S-101"}`,
			expectedIndex:     2,
			expectedOperation: "retire",
			expectedError:     MissingReasonError{ID: "site:S-101"},
		},
		{
			name:              "refuses a claim with nothing claimed",
			written:           `{"op": "add-claim", "subject": "site:S-101", "predicate": "width", "claim": {}}`,
			expectedIndex:     2,
			expectedOperation: "add-claim",
			expectedError:     ErrNoValue,
		},
		{
			name:              "refuses a claim written about nothing",
			written:           `{"op": "supersede", "predicate": "width", "claim": {"value": "0.1"}}`,
			expectedIndex:     2,
			expectedOperation: "supersede",
			expectedError:     ErrNoSubject,
		},
		{
			name:              "refuses a deprecation naming nothing to stand in its place",
			written:           `{"op": "deprecate-claim", "claim": "survey:W-0002"}`,
			expectedIndex:     2,
			expectedOperation: "deprecate-claim",
			expectedError:     ErrNoSupersedingClaim,
		},
		{
			name:              "refuses a member the operation does not read",
			written:           `{"op": "add-node", "id": "site:S-201", "edges": ["geom:E-01"]}`,
			expectedIndex:     2,
			expectedOperation: "add-node",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// The operation under test is the second of three, so that the index
			// is the one it was written at rather than the first or the last.
			_, err := ParseBatch(strings.NewReader(`{"operations": [
				{"op": "add-node", "id": "site:S-200"},
				` + testCase.written + `,
				{"op": "add-node", "id": "site:S-202"}
			]}`))

			var problem OperationError
			require.ErrorAs(t, err, &problem)
			assert.Equal(t, testCase.expectedIndex, problem.Index)
			assert.Equal(t, testCase.expectedOperation, problem.Operation)

			if testCase.expectedError != nil {
				assert.ErrorIs(t, err, testCase.expectedError)
			}
		})
	}
}

// TestParseBatchRefusesAnOperationNothingDeclares is its own function because
// the refusal carries every operation there is, which is a list rather than a
// value a table can compare.
func TestParseBatchRefusesAnOperationNothingDeclares(t *testing.T) {
	_, err := ParseBatch(strings.NewReader(`{"operations": [{"op": "add-widget"}]}`))

	var unknown UnknownOperationError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, "add-widget", unknown.Operation)
	assert.Equal(t, Operations(), unknown.Known)

	// An operation which does not say which it is, is the same refusal with
	// nothing to name: what it did not say is the whole of what is wrong.
	_, err = ParseBatch(strings.NewReader(`{"operations": [{"id": "site:S-201"}]}`))

	require.ErrorAs(t, err, &unknown)
	assert.Empty(t, unknown.Operation)
}

// TestParseBatchReportsEveryProblemAtOnce is its own function because it is
// about the set of problems rather than about one of them: an author fixing a
// refused batch should not have to resubmit it once per mistake.
func TestParseBatchReportsEveryProblemAtOnce(t *testing.T) {
	_, err := ParseBatch(strings.NewReader(`{"operations": [
		{"op": "add-node"},
		{"op": "add-node", "id": "site:S-201"},
		{"op": "add-widget"},
		{"op": "retire", "id": "site:S-202"}
	]}`))

	var batch BatchError
	require.ErrorAs(t, err, &batch)
	require.Len(t, batch.Errs, 3)

	indices := make([]int, 0, len(batch.Errs))
	for _, problem := range batch.Errs {
		var operation OperationError
		require.ErrorAs(t, problem, &operation)
		indices = append(indices, operation.Index)
	}

	assert.Equal(t, []int{1, 3, 4}, indices)
	assert.ErrorIs(t, err, ErrNoID)
	assert.ErrorIs(t, err, MissingReasonError{ID: "site:S-202"})
}

// TestBatchOperationsAreEveryWriteCommand walks the operations the format
// carries and requires each to be readable and to name itself.
//
// It walks [Operations] rather than naming them so that an operation added
// later is covered by this the day it is added, which is the only way a closed
// set stays closed.
func TestBatchOperationsAreEveryWriteCommand(t *testing.T) {
	for _, name := range Operations() {
		t.Run(name+" is read as the operation it names", func(t *testing.T) {
			written, err := json.Marshal(map[string]string{"op": name})
			require.NoError(t, err)

			operation, err := readOperation(written)

			require.NoError(t, err)
			assert.Equal(t, name, operation.Name())
		})
	}
}

func TestTxApply(t *testing.T) {
	testCases := []struct {
		name            string
		written         string
		expectedEffects [][]string
		expectedFiles   []string
	}{
		{
			name: "applies a batch which writes a node and the claim about it",
			written: `{"operations": [
				{"op": "add-node", "id": "site:S-103", "kind": "Space", "type": "MeetingRoom",
				 "geometry": "area", "frame": "frame:building", "label": "Meeting Room C"},
				{"op": "add-claim", "subject": "site:S-103", "predicate": "width",
				 "claim": {"value": "0.12", "unit": "m", "source": "As-built check AB-2026-020",
				           "method": "method:total-station", "accuracy": ["independent 0.003 m"],
				           "date": "2026-05-06"}}
			]}`,
			expectedEffects: [][]string{
				{"created node site:S-103"},
				{"modified node site:S-103"},
			},
			expectedFiles: []string{"entities/site.dfc"},
		},
		{
			name: "applies a batch whose edge runs between vertices it wrote itself",
			written: `{"operations": [
				{"op": "add-vertex", "id": "geom:V-07", "frame": "frame:building"},
				{"op": "add-vertex", "id": "geom:V-08", "frame": "frame:building"},
				{"op": "add-edge", "id": "geom:E-08", "frame": "frame:building",
				 "start": "geom:V-07", "end": "geom:V-08"}
			]}`,
			expectedEffects: [][]string{
				{"created vertex geom:V-07"},
				{"created vertex geom:V-08"},
				{"created edge geom:E-08"},
			},
			expectedFiles: []string{"entities/geometry.dfc"},
		},
		{
			name: "applies a batch which renames what it wrote",
			written: `{"operations": [
				{"op": "add-node", "id": "site:S-103", "kind": "Space", "type": "MeetingRoom",
				 "geometry": "area", "frame": "frame:building"},
				{"op": "set-label", "id": "site:S-103", "label": "Meeting Room C"}
			]}`,
			expectedEffects: [][]string{
				{"created node site:S-103"},
				{"modified node site:S-103"},
			},
			expectedFiles: []string{"entities/site.dfc"},
		},
		{
			// The one operation whose subject is registry data rather than a
			// node, which is why it lands in a file no other operation touches.
			name: "applies a batch which classifies a type in two schemes",
			written: `{"operations": [
				{"op": "classify-type", "type": "MeetingRoom", "system": "IFC4", "code": "IfcSpace"},
				{"op": "classify-type", "type": "MeetingRoom", "system": "Uniclass2015", "code": "SL_25_10_50"}
			]}`,
			expectedEffects: [][]string{
				{"modified type MeetingRoom"},
				{"modified type MeetingRoom"},
			},
			expectedFiles: []string{"registry.dfc"},
		},
		{
			name: "applies a batch which corrects one measurement and retracts another",
			written: `{"operations": [
				{"op": "supersede", "subject": "geom:V-01", "predicate": "position",
				 "claim": {"value": "0.01 0.0 0.0", "unit": "m", "source": "Re-survey RS-2026-002",
				           "method": "method:total-station", "accuracy": ["independent 0.002 m"],
				           "date": "2026-06-01"}},
				{"op": "deprecate-claim", "claim": "survey:W-0003", "supersededBy": "survey:W-0002"}
			]}`,
			// A supersession is two mutations of one form — the new claim and
			// the retraction of the one it replaces — so the operation which
			// made them reports both.
			expectedEffects: [][]string{
				{"modified vertex geom:V-01", "modified vertex geom:V-01"},
				{"modified node site:E-01"},
			},
			expectedFiles: []string{"entities/geometry.dfc", "entities/site.dfc"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			out, commit, root := batchApplied(t, testCase.written)

			require.Len(t, out, len(testCase.expectedEffects))

			for at, operation := range out {
				assert.Equal(t, at+1, operation.Index)
				assert.Equal(t, testCase.expectedEffects[at], spelledEffects(operation.Effects))
			}

			assert.Equal(t, testCase.expectedFiles, touched(t, root, commit))
		})
	}
}

// TestTxApplyValidatesTheModelTheBatchProduces is its own function because it
// asserts about what was written rather than about what each operation did: the
// batch is one change, and the model it produces is what has to load.
func TestTxApplyValidatesTheModelTheBatchProduces(t *testing.T) {
	out, _, root := batchApplied(t, `{"operations": [
		{"op": "add-vertex", "id": "geom:V-07", "frame": "frame:building",
		 "predicate": "position",
		 "claim": {"value": "12.0 0.0 0.0", "unit": "m", "source": "Interior control set IC-02",
		           "method": "method:total-station", "accuracy": ["independent 0.004 m"],
		           "date": "2026-02-18"}},
		{"op": "add-edge", "id": "geom:E-08", "frame": "frame:building",
		 "start": "geom:V-05", "end": "geom:V-07"},
		{"op": "add-node", "id": "site:S-103", "kind": "Space", "type": "MeetingRoom",
		 "geometry": "area", "frame": "frame:building", "label": "Meeting Room C"}
	]}`)

	assert.Len(t, out, 3)

	// The model the batch produced is loadable, which is the property the
	// commit asserted before it wrote and this asserts of what is on disk.
	graph, diags := LoadGraph(root)
	require.Empty(t, diags)

	for _, id := range []ID{"geom:V-07", "geom:E-08", "site:S-103"} {
		_, ok := graph.Entity(id)
		assert.Truef(t, ok, "the model holds %s", id)
	}
}

// TestTxApplyRefusesTheWholeBatchWhenOneOperationFails is its own function
// because it asserts about the tree rather than about a return value: nothing at
// all is written, whichever operation of the batch was the one which failed.
func TestTxApplyRefusesTheWholeBatchWhenOneOperationFails(t *testing.T) {
	root := copied(t, "testdata/graph/valid")
	before := files(t, root)

	tx, _, err := Begin(root)
	require.NoError(t, err)
	defer tx.Close()

	// The last operation is the one which cannot be applied: the id it writes
	// under is one the second operation already took.
	batch := batched(t, `{"operations": [
		{"op": "add-node", "id": "site:S-103", "kind": "Space", "type": "MeetingRoom",
		 "geometry": "area", "frame": "frame:building"},
		{"op": "add-vertex", "id": "geom:V-07", "frame": "frame:building"},
		{"op": "add-vertex", "id": "geom:V-07", "frame": "frame:building"}
	]}`)

	out, err := tx.Apply(batch)

	assert.Nil(t, out)

	var problem OperationError
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, 3, problem.Index)
	assert.Equal(t, "add-vertex", problem.Operation)

	var taken TakenIDError
	require.ErrorAs(t, err, &taken)
	assert.Equal(t, ID("geom:V-07"), taken.ID)

	require.NoError(t, tx.Close())
	assert.Equal(t, before, files(t, root), "a refused batch writes nothing at all")
}

// TestTxApplyReportsWhatEachOperationClaimed is its own function because a claim
// operation reports more than its effects: the claim it wrote, the one it
// retracted, and what the change had to say about the model it produced.
func TestTxApplyReportsWhatEachOperationClaimed(t *testing.T) {
	out, _, _ := batchApplied(t, `{"operations": [
		{"op": "add-claim", "subject": "site:E-01", "predicate": "width",
		 "claim": {"id": "survey:W-0004", "value": "0.101", "unit": "m",
		           "source": "Fit-out check FC-2026-003", "method": "method:total-station",
		           "date": "2026-05-12"}},
		{"op": "deprecate-claim", "claim": "survey:W-0003", "supersededBy": "survey:W-0004"}
	]}`)

	require.Len(t, out, 2)

	assert.Equal(t, ID("survey:W-0004"), out[0].Claim)
	assert.Equal(t, ID("survey:W-0003"), out[1].Replaced)

	// The claim was written with no accuracy, so it is unrankable, and it was
	// written where two live claims already were, so it conflicts. Neither
	// refuses it and both are things to find out now rather than later.
	kinds := make([]NoticeKind, 0, len(out[0].Notices))
	for _, notice := range out[0].Notices {
		kinds = append(kinds, notice.Kind)
	}
	assert.Equal(t, []NoticeKind{NoticeUnrankable, NoticeConflict}, kinds)
}

// TestTxApplyRefusesAFinishedTransaction checks the one thing every mutation
// promises: a transaction which has committed changes nothing more.
func TestTxApplyRefusesAFinishedTransaction(t *testing.T) {
	root := copied(t, "testdata/graph/valid")

	tx, _, err := Begin(root)
	require.NoError(t, err)

	_, _, err = tx.Commit()
	require.NoError(t, err)

	_, err = tx.Apply(batched(t, `{"operations": [{"op": "add-node", "id": "site:S-103"}]}`))

	assert.ErrorIs(t, err, ErrFinished)
}

// TestOperationErrorNamesWhatItIsAbout checks that the refusal carries the
// index, the operation and the cause as fields, so that a caller reports it
// without matching a message.
func TestOperationErrorNamesWhatItIsAbout(t *testing.T) {
	err := OperationError{Index: 3, Operation: "add-node", Err: ErrNoID}

	assert.ErrorIs(t, err, ErrNoID)
	assert.Contains(t, err.Error(), "operation 3")
	assert.Contains(t, err.Error(), "add-node")

	// An operation whose name could not be read is named by its place alone,
	// which is the honest answer rather than an invented name.
	assert.Contains(t, OperationError{Index: 3, Err: ErrNoID}.Error(), "operation 3")
}

// TestBatchErrorReachesEveryProblem checks that errors.Is walks every problem
// rather than only the first, which is what makes collecting them worth doing.
func TestBatchErrorReachesEveryProblem(t *testing.T) {
	err := BatchError{Errs: []error{
		OperationError{Index: 1, Err: ErrNoID},
		OperationError{Index: 2, Err: ErrNoEdges},
	}}

	assert.ErrorIs(t, err, ErrNoID)
	assert.ErrorIs(t, err, ErrNoEdges)
	assert.False(t, errors.Is(err, ErrNoValue))
}
