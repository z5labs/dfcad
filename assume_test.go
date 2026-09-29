// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures an assumption is tested over: a model every rule holds of, and
// one sited across a georeference, which is where the claims on a frame are.
const (
	assumeSatisfied = "testdata/checks/satisfied"
	assumeSurveyed  = "testdata/siting/surveyed"
)

// appliedGraph is the graph a batch produces when it is written for real: a
// transaction begun, the batch applied and the change committed, then the tree
// loaded back off disk.
func appliedGraph(t *testing.T, root string, batch Batch) *Graph {
	t.Helper()

	tx := begin(t, root)

	_, err := tx.Apply(batch)
	require.NoError(t, err)

	_, diags, err := tx.Commit()
	require.NoError(t, err)
	require.False(t, refused(diags), "the batch was refused when it was applied: %v", diags)

	graph, diags := LoadGraph(root)
	require.False(t, refused(diags), "the tree the batch wrote does not load: %v", diags)

	return graph
}

// assumed is Assume over root, requiring it to have answered.
func assumed(t *testing.T, root string, batch Batch) Assumption {
	t.Helper()

	out, diags, err := Assume(root, batch)
	require.NoError(t, err)
	require.False(t, refused(diags), "the batch was refused when it was assumed: %v", diags)
	require.NotNil(t, out.Graph)

	return out
}

// sameClaims asserts that subject resolves to the same claim under every
// predicate it carries a claim under in either graph.
//
// The claims are compared by what they say and where they were written relative
// to the model root, because the two graphs were read from two copies of the
// fixture and every span names the copy it came from.
func sameClaims(t *testing.T, subject ID, assumedRoot string, assumed *Graph, appliedRoot string, applied *Graph) {
	t.Helper()

	var predicates []string
	for _, graph := range []*Graph{assumed, applied} {
		for claim := range graph.Claims().Of(subject) {
			if !slices.Contains(predicates, claim.Predicate()) {
				predicates = append(predicates, claim.Predicate())
			}
		}
	}
	require.NotEmpty(t, predicates, "%s carries no claim in either graph", subject)

	for _, predicate := range predicates {
		want := applied.Claims().resolve(subject, predicate)
		got := assumed.Claims().resolve(subject, predicate)

		wantClaim, wantResolved := want.Claim()
		gotClaim, gotResolved := got.Claim()

		require.Equal(t, wantResolved, gotResolved, "%s %s resolves in one graph and not the other", subject, predicate)
		if !wantResolved {
			assert.Len(t, got.Candidates(), len(want.Candidates()))
			continue
		}

		assert.Equal(t, spelledClaim(appliedRoot, wantClaim), spelledClaim(assumedRoot, gotClaim),
			"%s %s resolves to a different claim", subject, predicate)
	}
}

// spelledClaim is everything a claim says and where it was written, with the
// root of the copy it was read from taken out of every span.
func spelledClaim(root string, claim *Claim) string {
	return strings.ReplaceAll(fmt.Sprintf("%+v", *claim), root, "<root>")
}

// nodeSeen is a semantic node as the round-trip compares it: what it is and
// what it is related to.
type nodeSeen struct {
	Label      string
	Kind       Kind
	Type       string
	Within     ID
	MemberOf   []ID
	Boundaries []ID
	Retired    bool
}

func spelledNode(graph *Graph, id ID) (nodeSeen, bool) {
	node, ok := graph.Nodes().Node(id)
	if !ok {
		return nodeSeen{}, false
	}

	within, _ := node.Within()

	return nodeSeen{
		Label:      node.Label(),
		Kind:       node.Kind(),
		Type:       node.Type(),
		Within:     within,
		MemberOf:   node.MemberOf(),
		Boundaries: node.Boundaries(),
		Retired:    node.Retired(),
	}, true
}

func TestAssume(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
		written string

		// claimed are the subjects whose claims the batch wrote or replaced,
		// and related the nodes whose relations or state it changed.
		claimed []ID
		related []ID
	}{
		{
			name:    "supersedes the position of a vertex",
			fixture: assumeSatisfied,
			written: `{"operations": [
				{"op": "supersede", "subject": "geom:V-21", "predicate": "position",
				 "claim": {"value": "3.0 6.5 0.0", "unit": "m", "source": "Re-survey RS-2026-003, Acme Surveys",
				           "method": "method:total-station", "accuracy": ["independent 0.002 m"],
				           "date": "2026-06-01"}}
			]}`,
			claimed: []ID{"geom:V-21"},
		},
		{
			name:    "writes a node and the claim about it",
			fixture: assumeSurveyed,
			written: `{"operations": [
				{"op": "add-node", "id": "plan:P-02", "kind": "Site", "type": "Plot",
				 "geometry": "area", "frame": "frame:site", "label": "Second plot", "file": "model.dfc"},
				{"op": "add-claim", "subject": "plan:P-02", "predicate": "setback",
				 "claim": {"value": "2.0", "unit": "m", "source": "Planning consent PC-2026-017",
				           "method": "method:total-station", "accuracy": ["independent 0.001 m"],
				           "date": "2026-06-02"}}
			]}`,
			claimed: []ID{"plan:P-02"},
			related: []ID{"plan:P-02"},
		},
		{
			name:    "relates a node to a zone",
			fixture: assumeSatisfied,
			written: `{"operations": [
				{"op": "relate", "id": "site:S-901", "memberOf": ["site:Z-90"]}
			]}`,
			related: []ID{"site:S-901"},
		},
		{
			name:    "retires a node in favour of a replacement",
			fixture: assumeSatisfied,
			written: `{"operations": [
				{"op": "retire", "id": "site:S-901", "reason": "Merged into Meeting Room B.",
				 "replacement": "site:S-102", "date": "2026-06-03"}
			]}`,
			related: []ID{"site:S-901", "site:S-102"},
		},
		{
			name:    "supersedes the transform of a frame",
			fixture: assumeSurveyed,
			written: `{"operations": [
				{"op": "supersede", "subject": "frame:building", "predicate": "frame-transform",
				 "claim": {"value": "5.0 4.1 0.0 1.0 0.0 0.0 0.0 1.0 0.0 0.0 0.0 1.0 1.0",
				           "source": "Georeferencing report GR-2026-003, Acme Surveys",
				           "method": "method:gnss-static", "accuracy": ["independent 0.010 m"],
				           "date": "2026-06-04"}}
			]}`,
			claimed: []ID{"frame:building"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			batch := batched(t, testCase.written)

			assumedRoot := copied(t, testCase.fixture)
			appliedRoot := copied(t, testCase.fixture)

			base, err := DigestOf(assumedRoot)
			require.NoError(t, err)

			out := assumed(t, assumedRoot, batch)
			applied := appliedGraph(t, appliedRoot, batch)

			// The property: what the batch would produce is what applying it
			// does produce, byte for byte, and the digest says so.
			written, err := DigestOf(appliedRoot)
			require.NoError(t, err)

			digest, known := out.Graph.Digest()
			require.True(t, known)
			assert.Equal(t, written, digest)
			assert.NotEqual(t, base, digest, "the batch changed no byte of the tree")
			assert.Equal(t, base, out.Base)

			require.Len(t, out.Applied, len(batch.Operations))
			for at, operation := range out.Applied {
				assert.Equal(t, at+1, operation.Index)
				assert.Equal(t, batch.Operations[at].Name(), operation.Operation)
				assert.NotEmpty(t, operation.Effects)
			}

			for _, subject := range testCase.claimed {
				sameClaims(t, subject, assumedRoot, out.Graph, appliedRoot, applied)
			}

			for _, id := range testCase.related {
				want, wantFound := spelledNode(applied, id)
				got, gotFound := spelledNode(out.Graph, id)

				require.True(t, wantFound, "%s is not in the model the batch wrote", id)
				require.True(t, gotFound, "%s is not in the model the batch would produce", id)
				assert.Equal(t, want, got)
			}
		})
	}
}

// TestAssumeWritesNothingAndLocksNothing is its own function because it asserts
// about the disk rather than the model: every byte beneath the root is what it
// was, and the lock another transaction holds is neither refused nor released.
func TestAssumeWritesNothingAndLocksNothing(t *testing.T) {
	root := copied(t, assumeSatisfied)

	// A lock somebody else holds, which [Begin] would refuse on.
	lock := filepath.Join(root, LockName)
	require.NoError(t, os.WriteFile(lock, []byte("12345\n"), 0o666))

	_, _, err := Begin(root)
	require.ErrorIs(t, err, ErrLocked)

	before := files(t, root)

	out := assumed(t, root, batched(t, `{"operations": [
		{"op": "add-node", "id": "site:S-103", "kind": "Space", "type": "MeetingRoom",
		 "geometry": "area", "frame": "frame:building", "label": "Meeting Room C",
		 "file": "entities/new.dfc"},
		{"op": "add-vertex", "id": "geom:V-30", "frame": "frame:building", "file": "model.dfc"},
		{"op": "supersede", "subject": "geom:V-21", "predicate": "position",
		 "claim": {"value": "3.0 6.5 0.0", "unit": "m", "source": "Re-survey RS-2026-003, Acme Surveys",
		           "method": "method:total-station", "accuracy": ["independent 0.002 m"],
		           "date": "2026-06-01"}}
	]}`))

	_, ok := out.Graph.Nodes().Node("site:S-103")
	assert.True(t, ok, "the model the batch would produce lacks the node it wrote")

	assert.Equal(t, before, files(t, root))

	held, err := os.ReadFile(lock)
	require.NoError(t, err)
	assert.Equal(t, "12345\n", string(held))
}

// TestAssumeCreatesNoLock is its own function because the root it runs over
// holds none: an assumption which took one and released it would leave the tree
// as it found it and still have refused anybody writing while it ran.
func TestAssumeCreatesNoLock(t *testing.T) {
	root := copied(t, assumeSatisfied)
	before := files(t, root)

	// The lock is looked for while the batch is being applied, which is the
	// one moment a lock taken and released would be visible.
	probe := &lockProbe{root: root}

	_, diags, err := Assume(root, Batch{Operations: []Operation{probe}})
	require.NoError(t, err)
	require.False(t, refused(diags))

	assert.True(t, probe.ran)
	assert.False(t, probe.locked, "a lock file existed while the batch was applied")
	assert.Equal(t, before, files(t, root))
}

// lockProbe is an operation which changes nothing and reports whether the root
// was locked while it ran.
type lockProbe struct {
	root   string
	ran    bool
	locked bool
}

func (p *lockProbe) Name() string { return "probe" }

func (p *lockProbe) check() error { return nil }

func (p *lockProbe) apply(*Tx, *Applied) error {
	p.ran = true
	_, err := os.Stat(filepath.Join(p.root, LockName))
	p.locked = err == nil
	return nil
}

// TestAssumeRefusesATreeWhichDoesNotLoad is its own function because the
// refusal is about the model on disk rather than the batch: no operation is
// attempted, and the answer is what [Begin] would have given.
func TestAssumeRefusesATreeWhichDoesNotLoad(t *testing.T) {
	root := copied(t, assumeSatisfied)
	require.NoError(t, os.WriteFile(filepath.Join(root, "broken.dfc"), []byte("(node site:X-01\n"), 0o666))

	out, diags, err := Assume(root, batched(t, `{"operations": [
		{"op": "set-label", "id": "site:S-901", "label": "Store room"}
	]}`))

	require.NoError(t, err)
	assert.Equal(t, Assumption{}, out)
	assert.True(t, refused(diags))

	_, want, err := Begin(root)
	require.NoError(t, err)
	assert.Equal(t, want, diags)
}

// TestAssumeRefusesAnOperationTheModelRefuses is its own function because the
// refusal is an error naming the operation rather than diagnostics: the batch
// never produced a model to have diagnostics about.
func TestAssumeRefusesAnOperationTheModelRefuses(t *testing.T) {
	root := copied(t, assumeSatisfied)
	before := files(t, root)

	out, _, err := Assume(root, batched(t, `{"operations": [
		{"op": "set-label", "id": "site:S-901", "label": "Store room"},
		{"op": "retire", "id": "site:S-999", "reason": "Never built."}
	]}`))

	var refusal OperationError
	require.True(t, errors.As(err, &refusal), "expected an OperationError, got %T", err)
	assert.Equal(t, 2, refusal.Index)
	assert.Equal(t, "retire", refusal.Operation)
	assert.Equal(t, Assumption{}, out)
	assert.Equal(t, before, files(t, root))
}

// TestAssumeRefusesABatchWhoseModelWouldNotLoad is its own function because
// every operation is accepted and the refusal comes from the model they produce
// together, exactly as a commit's does.
func TestAssumeRefusesABatchWhoseModelWouldNotLoad(t *testing.T) {
	// The retirement is accepted: it names a replacement for the references it
	// has to redirect. What it produces is the rooms the level contained now
	// contained by one of themselves, which is a model that does not load.
	written := `{"operations": [
		{"op": "retire", "id": "site:L-01", "reason": "Merged into Meeting Room A.",
		 "replacement": "site:S-101"}
	]}`

	root := copied(t, assumeSatisfied)
	before := files(t, root)

	out, diags, err := Assume(root, batched(t, written))

	require.NoError(t, err)
	assert.Equal(t, Assumption{}, out)
	require.True(t, refused(diags))

	// The diagnostics are the ones a dry-run commit of the same batch gives.
	tx := begin(t, copied(t, assumeSatisfied))
	tx.DryRun = true
	_, err = tx.Apply(batched(t, written))
	require.NoError(t, err)
	_, want, err := tx.Commit()
	require.NoError(t, err)

	assert.Equal(t, codes(want), codes(diags))
	assert.Equal(t, before, files(t, root))
}

// codes is each diagnostic's severity, message and where it points within its
// file, so that two refusals of one batch over two copies of a fixture compare.
func codes(diags []Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		start := diagnostic.Span.Start
		out = append(out, fmt.Sprintf("%s %s:%d:%d %s",
			diagnostic.Severity, filepath.Base(start.Path), start.Line, start.Column, diagnostic.Message))
	}
	return out
}

func TestAssumeRefusesARootWhichIsNotADirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "model.dfc")
	require.NoError(t, os.WriteFile(file, []byte(""), 0o666))

	testCases := []struct {
		name     string
		root     string
		expected error
	}{
		{
			name:     "refuses a root which is a file",
			root:     file,
			expected: ErrNotADirectory,
		},
		{
			name:     "refuses a root which is not there",
			root:     filepath.Join(t.TempDir(), "absent"),
			expected: os.ErrNotExist,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			out, diags, err := Assume(testCase.root, batched(t, `{"operations": [
				{"op": "set-label", "id": "site:S-901", "label": "Store room"}
			]}`))

			var rootErr RootError
			require.True(t, errors.As(err, &rootErr), "expected a RootError, got %T", err)
			assert.Equal(t, testCase.root, rootErr.Path)
			assert.ErrorIs(t, err, testCase.expected)
			assert.Equal(t, Assumption{}, out)
			assert.Empty(t, diags)
		})
	}
}

// TestDigestOfFilesIsTheOrderAWalkReads is its own function because it is about
// the digest of a tree nobody wrote agreeing with the digest of the same tree on
// disk, where the order a walk reads directories in is not the order of their
// paths.
func TestDigestOfFilesIsTheOrderAWalkReads(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
	}{
		{
			name: "digests a directory before a sibling file whose name it prefixes",
			files: map[string]string{
				"a/b.dfc": "(node site:Z-01)\n",
				"a-b.dfc": "(node site:Z-02)\n",
				"a.dfc":   "(node site:Z-03)\n",
			},
		},
		{
			name: "digests nested directories in the order a walk descends",
			files: map[string]string{
				"x/y/z.dfc":    "one\n",
				"x/y.dfc":      "two\n",
				"x.y/z.dfc":    "three\n",
				"registry.dfc": "four\n",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, testCase.files)

			want, err := DigestOf(root)
			require.NoError(t, err)

			var written []digested
			for path, content := range testCase.files {
				written = append(written, digested{path: filepath.Join(root, path), content: []byte(content)})
			}

			assert.Equal(t, want, digestOfFiles(root, written))
		})
	}
}
