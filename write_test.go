// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"errors"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The pieces of the model the transaction tests are written against: a
// vocabulary, one node written under it, and a second file the changes below
// leave alone.
//
// It is deliberately the smallest tree which loads. What these tests are about
// is what a change does to files, and a fixture large enough to be interesting
// as a model would only make the assertions about bytes harder to read.
const (
	writeRegistry = `(project
  (label "Write fixture")
  (globalid-namespace "https://example.org/models/write"))

(namespace site (description "Semantic nodes minted by this model."))

(type Campus
  (kind Zone)
  (geometry absent)
  (description "A group of things administered together."))

(predicate width
  (unit m)
  (shape scalar)
  (description "How thick an element is, measured across it."))
`

	writeSite = `(node site:Z-01 (label "Riverside campus") (kind Zone) (type Campus))
`

	// writeOther is a file no change below touches, written in a spelling the
	// canonical printer would not produce, so that a test which finds it
	// canonical afterwards has found a write nobody asked for.
	writeOther = `(node site:Z-02    (type Campus)
   (kind Zone)   (label "Northern campus"))
`
)

// writeFixture writes the model the transaction tests change and returns its
// root.
func writeFixture(t *testing.T) string {
	t.Helper()

	return tree(t, map[string]string{
		"registry.dfc":       writeRegistry,
		"entities/site.dfc":  writeSite,
		"entities/other.dfc": writeOther,
	})
}

// begin opens a transaction against root, requiring the model to load.
func begin(t *testing.T, root string) *Tx {
	t.Helper()

	tx, diags, err := Begin(root)
	require.NoError(t, err)
	require.Empty(t, diags)
	require.NotNil(t, tx)

	t.Cleanup(func() { _ = tx.Close() })

	return tx
}

// written parses one top-level form, which is how a test writes what a mutation
// inserts. A command builds one; a test reads more easily as text.
func written(t *testing.T, src string) *Node {
	t.Helper()

	file, err := Parse("", strings.NewReader(src))
	require.NoError(t, err)
	require.Len(t, file.Nodes, 1)

	return file.Nodes[0]
}

// commit commits a transaction, requiring it to have been written.
func commit(t *testing.T, tx *Tx) Commit {
	t.Helper()

	out, diags, err := tx.Commit()
	require.NoError(t, err)
	require.Empty(t, diags)

	return out
}

// changed names each file of a commit and what happened to it, so that an
// expectation does not hold a temporary directory nobody can predict.
func changed(t *testing.T, root string, out Commit) []string {
	t.Helper()

	files := make([]string, 0, len(out.Files))
	for _, file := range out.Files {
		rel, err := filepath.Rel(root, file.Path)
		require.NoError(t, err)

		files = append(files, filepath.ToSlash(rel)+" "+string(file.Status))
	}

	return files
}

func TestBegin(t *testing.T) {
	t.Run("loads the model it is about to change", func(t *testing.T) {
		root := writeFixture(t)

		tx := begin(t, root)

		require.NotNil(t, tx.Graph())
		assert.Equal(t, root, tx.Root())
		assert.Equal(t, 2, tx.Graph().Summary().Nodes())
	})

	t.Run("refuses a tree which does not already load", func(t *testing.T) {
		root := tree(t, map[string]string{
			"registry.dfc": writeRegistry,
			"site.dfc":     writeSite,
			// The same id a second time, which is what a load reports and what a
			// change must not be asked to write on top of.
			"again.dfc": writeSite,
		})

		tx, diags, err := Begin(root)

		require.NoError(t, err)
		assert.Nil(t, tx)
		assert.True(t, refused(diags), "the pre-existing errors are reported")

		// And the root is released, so the refusal costs the next caller
		// nothing.
		assert.NoFileExists(t, filepath.Join(root, LockName))
	})

	t.Run("refuses a model root another transaction holds", func(t *testing.T) {
		root := writeFixture(t)

		first := begin(t, root)

		second, diags, err := Begin(root)

		assert.Nil(t, second)
		assert.Nil(t, diags)

		var locked LockError
		require.ErrorAs(t, err, &locked)
		assert.ErrorIs(t, locked.Err, ErrLocked)
		assert.Equal(t, filepath.Join(root, LockName), locked.Path)

		// And the root is free again once the first one lets go.
		require.NoError(t, first.Close())

		third, _, err := Begin(root)
		require.NoError(t, err)
		require.NotNil(t, third)
		require.NoError(t, third.Close())
	})

	t.Run("reports a model root which is not there", func(t *testing.T) {
		tx, diags, err := Begin(filepath.Join(t.TempDir(), "missing"))

		assert.Nil(t, tx)
		assert.Nil(t, diags)

		var locked LockError
		require.ErrorAs(t, err, &locked)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("holds the model root for the life of the transaction", func(t *testing.T) {
		root := writeFixture(t)

		tx := begin(t, root)
		assert.FileExists(t, filepath.Join(root, LockName))

		commit(t, tx)
		assert.NoFileExists(t, filepath.Join(root, LockName))
	})

	t.Run("steps over the lock it holds", func(t *testing.T) {
		root := writeFixture(t)

		tx := begin(t, root)

		// The lock file sits in the model root while the model is read, so a
		// transaction which mistook it for an entity file would refuse every
		// tree it locked.
		require.NotNil(t, tx.Graph())
		assert.NotContains(t, slices.Collect(walkedPaths(root)), filepath.Join(root, LockName))
	})
}

// walkedPaths is every path a walk of root yields, which is what the loader
// reads.
func walkedPaths(root string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for path, err := range Walk(root) {
			if err != nil {
				continue
			}
			if !yield(path) {
				return
			}
		}
	}
}

func TestTxWritesCanonicalForm(t *testing.T) {
	root := writeFixture(t)
	site := filepath.Join(root, "entities", "site.dfc")

	tx := begin(t, root)

	// Written in an order the canonical printer does not use, so that what
	// lands on disk cannot be the bytes it was handed.
	require.NoError(t, tx.Insert(
		"entities/site.dfc",
		written(t, `(node site:Z-03 (type Campus) (label "Eastern campus") (kind Zone))`),
	))

	out := commit(t, tx)

	assert.Equal(t, []string{"entities/site.dfc rewritten"}, changed(t, root, out))

	src, err := os.ReadFile(site)
	require.NoError(t, err)
	assert.Contains(t, string(src), `(node site:Z-03 (label "Eastern campus") (kind Zone) (type Campus))`)

	// What a write leaves behind already satisfies fmt --check: formatting it
	// finds nothing to do.
	formatted := Formatter{}.Format(site)
	require.Len(t, formatted, 1)
	assert.False(t, formatted[0].Changed, "a written file is already canonical")

	// And the model it wrote is the model it meant to write.
	graph, diags := LoadGraph(root)
	require.Empty(t, diags)

	node, ok := graph.Node("site:Z-03")
	require.True(t, ok)
	assert.Equal(t, "Eastern campus", node.Label())
}

func TestTxWritesOnlyTheFilesItTouched(t *testing.T) {
	root := writeFixture(t)

	before := contents(t, root)

	tx := begin(t, root)
	require.NoError(t, tx.Insert(
		"entities/site.dfc",
		written(t, `(node site:Z-03 (kind Zone) (type Campus))`),
	))
	commit(t, tx)

	after := contents(t, root)

	// The one file the change was about is the one file which moved. Every
	// other byte of the tree — including a file nobody has ever formatted — is
	// exactly what it was.
	for _, path := range slices.Sorted(maps.Keys(before)) {
		if path == "entities/site.dfc" {
			assert.NotEqual(t, before[path], after[path])
			continue
		}
		assert.Equal(t, before[path], after[path], "%s was rewritten and nothing asked for it", path)
	}

	assert.Equal(t, slices.Sorted(maps.Keys(before)), slices.Sorted(maps.Keys(after)),
		"a write leaves no temporary file behind")
}

func TestTxRefusesAChangeWhichWouldNotLoad(t *testing.T) {
	root := writeFixture(t)
	site := filepath.Join(root, "entities", "site.dfc")

	before := contents(t, root)

	// An id the model already holds, which is a rule about the whole model
	// rather than about the form, so nothing short of interpreting the result
	// can find it.
	duplicate := `(node site:Z-01 (label "Second campus") (kind Zone) (type Campus))`

	tx := begin(t, root)
	require.NoError(t, tx.Insert("entities/site.dfc", written(t, duplicate)))

	out, diags, err := tx.Commit()

	require.NoError(t, err)
	assert.Equal(t, Commit{}, out)
	require.True(t, refused(diags), "the change is refused")

	assert.Equal(t, before, contents(t, root), "a refused change leaves the tree byte-identical")

	// The diagnostics are the ones the load would have raised, because they came
	// from the same passes over the text which would have been written. Writing
	// that text by hand and loading it is what checks the claim.
	file, err := parse(site, []byte(before["entities/site.dfc"]))
	require.NoError(t, err)
	file.Nodes = append(file.Nodes, written(t, duplicate))

	var printed bytes.Buffer
	require.NoError(t, Print(&printed, file))
	require.NoError(t, os.WriteFile(site, printed.Bytes(), 0o600))

	_, loaded := LoadGraph(root)
	assert.Equal(t, loaded, diags)
}

func TestTxValidatesEverythingALoadValidates(t *testing.T) {
	// A change is refused by interpreting the model it would produce, not by a
	// second set of rules written beside the loader. Each case below is one of
	// the things a load decides, and each is decided here for the same reason
	// and in the same words.
	testCases := []struct {
		name     string
		form     string
		expected string
	}{
		{
			name:     "refuses a form which is not well formed",
			form:     `(node site:Z-03 (type Campus))`,
			expected: "expected a (kind ...) child of the node form, found none",
		},
		{
			name:     "refuses a type no registry file declares",
			form:     `(node site:Z-03 (kind Zone) (type NoSuchType))`,
			expected: "expected a declared type, found NoSuchType, which no registry file declares",
		},
		{
			name:     "refuses an id something in the model already holds",
			form:     `(node site:Z-01 (kind Zone) (type Campus))`,
			expected: "expected an id nothing else holds, found site:Z-01, which already names something in this model",
		},
		{
			name:     "refuses a reference which reaches nothing",
			form:     `(node site:Z-03 (kind Zone) (type Campus) (within site:NOPE))`,
			expected: "expected a node id something in this model holds, found site:NOPE, which names no node",
		},
		{
			name:     "refuses a predicate no registry file declares",
			form:     `(node site:Z-03 (kind Zone) (type Campus) (colour "slate"))`,
			expected: "expected a declared predicate, found colour, which no registry file declares",
		},
		{
			name:     "refuses a plain value where the predicate bears claims",
			form:     `(node site:Z-03 (kind Zone) (type Campus) (width 0.1 m))`,
			expected: "expected the claim the predicate width bears, found a plain value",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := writeFixture(t)
			before := contents(t, root)

			tx := begin(t, root)
			require.NoError(t, tx.Insert("entities/site.dfc", written(t, testCase.form)))

			out, diags, err := tx.Commit()

			require.NoError(t, err)
			assert.Equal(t, Commit{}, out)
			require.True(t, refused(diags), "the change is refused")

			messages := make([]string, 0, len(diags))
			for _, diagnostic := range diags {
				messages = append(messages, diagnostic.Message)
			}
			assert.Contains(t, messages, testCase.expected)

			assert.Equal(t, before, contents(t, root), "nothing is written")
		})
	}
}

func TestTxDryRun(t *testing.T) {
	root := writeFixture(t)

	before := contents(t, root)

	tx := begin(t, root)
	tx.DryRun = true

	require.NoError(t, tx.Insert(
		"entities/site.dfc",
		written(t, `(node site:Z-03 (kind Zone) (type Campus))`),
	))

	out := commit(t, tx)

	assert.True(t, out.DryRun)
	assert.Equal(t, []string{"entities/site.dfc rewritten"}, changed(t, root, out))

	require.Len(t, out.Files, 1)
	assert.Contains(t, out.Files[0].Diff, "+(node site:Z-03 (kind Zone) (type Campus))")
	assert.Contains(t, out.Files[0].Diff, "--- "+out.Files[0].Path)

	// A dry run wrote nothing, whatever it would have written, so the files it
	// wrote are none. A caller reading this as "the files this run touched"
	// would otherwise report a dry run as having changed the tree.
	assert.Empty(t, out.Written())

	assert.Equal(t, before, contents(t, root), "a dry run writes nothing")
}

func TestTxDryRunIsRefusedByTheSameDiagnostics(t *testing.T) {
	root := writeFixture(t)

	refusal := func(dry bool) []Diagnostic {
		tx := begin(t, root)
		tx.DryRun = dry

		require.NoError(t, tx.Insert(
			"entities/site.dfc",
			written(t, `(node site:Z-01 (kind Zone) (type Campus))`),
		))

		_, diags, err := tx.Commit()
		require.NoError(t, err)

		return diags
	}

	dry := refusal(true)
	wet := refusal(false)

	require.True(t, refused(dry))
	assert.Equal(t, wet, dry)
}

func TestTxCreatesAFile(t *testing.T) {
	root := writeFixture(t)

	tx := begin(t, root)
	require.NoError(t, tx.Insert(
		"entities/campus.dfc",
		written(t, `(node site:Z-03 (kind Zone) (type Campus))`),
	))

	out := commit(t, tx)

	assert.Equal(t, []string{"entities/campus.dfc created"}, changed(t, root, out))

	graph, diags := LoadGraph(root)
	require.Empty(t, diags)

	_, ok := graph.Node("site:Z-03")
	assert.True(t, ok, "the created file is part of the model")
}

// TestTxCreatesTheDirectoriesAboveAFile is its own function because it is about
// the tree rather than about the file: a routing rule names where a node goes,
// and the first node routed somewhere new is the one whose write has to make the
// directory it named. A change refused for a directory nobody has created yet
// would make a rule unusable until somebody hand-made a folder for it.
func TestTxCreatesTheDirectoriesAboveAFile(t *testing.T) {
	root := writeFixture(t)

	tx := begin(t, root)
	require.NoError(t, tx.Insert(
		"entities/levels/one/campus.dfc",
		written(t, `(node site:Z-03 (kind Zone) (type Campus))`),
	))

	out := commit(t, tx)

	assert.Equal(t, []string{"entities/levels/one/campus.dfc created"}, changed(t, root, out))

	graph, diags := LoadGraph(root)
	require.Empty(t, diags)

	_, ok := graph.Node("site:Z-03")
	assert.True(t, ok, "a walk of the model reaches the file the change made a directory for")
}

// TestTxInsertsWhereCanonicalFormPutsIt is its own function because it is about
// where in a file a form lands rather than about which file: [Tx.Insert] appends
// to the tree, and canonical form then sorts it, so a node whose id sorts first
// is written first however late it was inserted. That is what makes the file a
// write leaves behind one nothing has to reformat.
func TestTxInsertsWhereCanonicalFormPutsIt(t *testing.T) {
	root := writeFixture(t)
	site := filepath.Join(root, "entities", "site.dfc")

	tx := begin(t, root)
	require.NoError(t, tx.Insert(
		"entities/site.dfc",
		written(t, `(node site:Z-00 (kind Zone) (type Campus))`),
	))
	commit(t, tx)

	src, err := os.ReadFile(site)
	require.NoError(t, err)

	assert.Equal(t,
		"(node site:Z-00 (kind Zone) (type Campus))\n"+
			`(node site:Z-01 (label "Riverside campus") (kind Zone) (type Campus))`+"\n",
		string(src),
		"the inserted node is written where the ordering rule puts it, not where it was appended",
	)

	formatted := Formatter{}.Format(site)
	require.Len(t, formatted, 1)
	assert.False(t, formatted[0].Changed, "the file needs no reformatting")
}

func TestTxReportsWhatItChanged(t *testing.T) {
	root := writeFixture(t)

	tx := begin(t, root)

	existing, ok := tx.Form("site:Z-02")
	require.True(t, ok)
	require.NoError(t, tx.Replace(existing, written(t, `(node site:Z-02 (label "Renamed") (kind Zone) (type Campus))`)))

	retired, ok := tx.Form("site:Z-01")
	require.True(t, ok)
	require.NoError(t, tx.Remove(retired))

	require.NoError(t, tx.Insert(
		"entities/site.dfc",
		written(t, `(node site:Z-03 (kind Zone) (type Campus))`),
	))

	out := commit(t, tx)

	assert.Equal(t, []string{
		"entities/other.dfc rewritten",
		"entities/site.dfc rewritten",
	}, changed(t, root, out))

	assert.Equal(t, []Effect{
		{Op: OpModified, Tag: "node", ID: "site:Z-02"},
		{Op: OpRetired, Tag: "node", ID: "site:Z-01"},
		{Op: OpCreated, Tag: "node", ID: "site:Z-03"},
	}, out.Effects())

	rel := make([]string, 0, 2)
	for _, path := range out.Written() {
		name, err := filepath.Rel(root, path)
		require.NoError(t, err)
		rel = append(rel, filepath.ToSlash(name))
	}
	assert.Equal(t, []string{"entities/other.dfc", "entities/site.dfc"}, rel)
}

func TestTxWritesNothingForAChangeWhichChangesNothing(t *testing.T) {
	root := writeFixture(t)

	before := contents(t, root)

	tx := begin(t, root)

	// The file is already canonical, and what is put back is what was taken
	// out, so the printing of the result is the bytes which are already there.
	held, ok := tx.Form("site:Z-01")
	require.True(t, ok)
	require.NoError(t, tx.Replace(held, written(t, writeSite)))

	out := commit(t, tx)

	assert.Equal(t, []string{"entities/site.dfc unchanged"}, changed(t, root, out))
	assert.Empty(t, out.Written())
	assert.Empty(t, out.Files[0].Diff)
	assert.Equal(t, before, contents(t, root))
}

func TestTxRemovesEveryFormOfAFile(t *testing.T) {
	root := writeFixture(t)

	tx := begin(t, root)

	held, ok := tx.Form("site:Z-01")
	require.True(t, ok)
	require.NoError(t, tx.Remove(held))

	commit(t, tx)

	// The file stays, holding nothing. An empty entity file is legal and
	// contributes nothing, and deleting is a mutating step this write path does
	// not take.
	written, err := os.ReadFile(filepath.Join(root, "entities", "site.dfc"))
	require.NoError(t, err)
	assert.Empty(t, written)

	graph, diags := LoadGraph(root)
	require.Empty(t, diags)

	_, ok = graph.Node("site:Z-01")
	assert.False(t, ok)
}

func TestTxRollsBackAMultiFileChange(t *testing.T) {
	root := writeFixture(t)

	// A directory where the second file of the change is to be written, so that
	// the rename which would put it there fails after the first file's rename
	// has already succeeded. A walk steps over a directory, so the model the
	// transaction loads is exactly the fixture's.
	blocked := filepath.Join(root, "entities", "zzz.dfc")
	require.NoError(t, os.Mkdir(blocked, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(blocked, "occupied"), []byte("x"), 0o600))

	before := contents(t, root)

	tx := begin(t, root)
	require.NoError(t, tx.Insert(
		"entities/site.dfc",
		written(t, `(node site:Z-03 (kind Zone) (type Campus))`),
	))
	require.NoError(t, tx.Insert(
		"entities/zzz.dfc",
		written(t, `(node site:Z-04 (kind Zone) (type Campus))`),
	))

	out, diags, err := tx.Commit()

	assert.Equal(t, Commit{}, out)
	assert.Empty(t, diags, "the change was valid; what failed was the writing")

	var failure WriteError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, blocked, failure.Path)

	assert.Equal(t, before, contents(t, root),
		"the file which was written is put back and no temporary file is left")
}

func TestTxWritesNothingWhenPreparationFails(t *testing.T) {
	root := writeFixture(t)

	before := contents(t, root)

	tx := begin(t, root)

	// The first file of the change is preparable and the second is not — the
	// directory it would need is an existing file, which is the one shape of
	// missing directory a write cannot make — so the failure lands after one
	// file's complete contents have been written to a temporary file and before
	// any rename. That is the interrupted write: the renames are the only steps
	// which change what a reader sees, and none of them happened.
	require.NoError(t, tx.Insert(
		"entities/site.dfc",
		written(t, `(node site:Z-03 (kind Zone) (type Campus))`),
	))
	require.NoError(t, tx.Insert(
		"entities/other.dfc/deep.dfc",
		written(t, `(node site:Z-04 (kind Zone) (type Campus))`),
	))

	out, diags, err := tx.Commit()

	assert.Equal(t, Commit{}, out)
	assert.Empty(t, diags, "the change was valid; what failed was the writing")

	var failure WriteError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, filepath.Join(root, "entities", "other.dfc", "deep.dfc"), failure.Path)

	assert.Equal(t, before, contents(t, root),
		"no file was replaced and no partial file was left behind")
}

func TestTxRefusesATargetTheModelWouldNotReadBack(t *testing.T) {
	root := writeFixture(t)

	testCases := []struct {
		name     string
		path     string
		expected error
	}{
		{
			name:     "refuses a file whose extension no walk picks up",
			path:     "entities/site.txt",
			expected: ErrNotAnEntityFile,
		},
		{
			name:     "refuses a file with no extension at all",
			path:     "entities/site",
			expected: ErrNotAnEntityFile,
		},
		{
			name:     "refuses a file above the model root",
			path:     filepath.Join(root, "..", "elsewhere.dfc"),
			expected: ErrOutsideModel,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tx := begin(t, root)

			err := tx.Insert(testCase.path, written(t, `(node site:Z-03 (kind Zone) (type Campus))`))

			var target TargetError
			require.ErrorAs(t, err, &target)
			assert.ErrorIs(t, target.Err, testCase.expected)
			assert.Equal(t, root, target.Root)
		})
	}
}

func TestTxAcceptsAnAbsoluteTargetInsideARelativeRoot(t *testing.T) {
	root := writeFixture(t)
	t.Chdir(root)

	// The command line resolves a relative argument against the root and leaves
	// an absolute one alone, so the two spellings meet here. Comparing them as
	// written would refuse a file which is inside the root.
	tx := begin(t, ".")
	require.NoError(t, tx.Insert(
		filepath.Join(root, "entities", "campus.dfc"),
		written(t, `(node site:Z-03 (kind Zone) (type Campus))`),
	))

	out := commit(t, tx)
	require.Len(t, out.Files, 1)
	assert.Equal(t, FileCreated, out.Files[0].Status)

	graph, diags := LoadGraph(".")
	require.Empty(t, diags)

	_, ok := graph.Node("site:Z-03")
	assert.True(t, ok)
}

func TestTxRefusesAFormItDoesNotHold(t *testing.T) {
	root := writeFixture(t)

	testCases := []struct {
		name    string
		mutate  func(tx *Tx, form *Node) error
		subject *Node
	}{
		{
			name:   "refuses replacing a form read out of some other model",
			mutate: func(tx *Tx, f *Node) error { return tx.Replace(f, f) },
		},
		{
			name:   "refuses removing a form read out of some other model",
			mutate: func(tx *Tx, f *Node) error { return tx.Remove(f) },
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tx := begin(t, root)

			err := testCase.mutate(tx, written(t, `(node site:Z-09 (kind Zone) (type Campus))`))

			var unknown UnknownFormError
			assert.ErrorAs(t, err, &unknown)
		})
	}
}

func TestTxRemovesAFormItInserted(t *testing.T) {
	root := writeFixture(t)

	before := contents(t, root)

	tx := begin(t, root)

	// A form built in memory carries the span of nowhere, so a transaction
	// which located one only by where it says it was written could not undo its
	// own insertion.
	inserted := written(t, `(node site:Z-03 (kind Zone) (type Campus))`)
	require.NoError(t, tx.Insert("entities/site.dfc", inserted))
	require.NoError(t, tx.Remove(inserted))

	out := commit(t, tx)

	assert.Equal(t, []string{"entities/site.dfc unchanged"}, changed(t, root, out))
	assert.Equal(t, before, contents(t, root))
}

func TestTxIsOneChange(t *testing.T) {
	root := writeFixture(t)

	tx := begin(t, root)
	commit(t, tx)

	_, _, err := tx.Commit()
	assert.ErrorIs(t, err, ErrFinished)

	assert.ErrorIs(t, tx.Insert("entities/site.dfc", written(t, `(node site:Z-03 (kind Zone) (type Campus))`)), ErrFinished)
	assert.ErrorIs(t, tx.Replace(nil, nil), ErrFinished)
	assert.ErrorIs(t, tx.Remove(nil), ErrFinished)

	// Closing a transaction which has committed does nothing, which is what
	// makes deferring it right in every case.
	assert.NoError(t, tx.Close())
}

func TestTxCommitsNothing(t *testing.T) {
	root := writeFixture(t)

	before := contents(t, root)

	tx := begin(t, root)
	out := commit(t, tx)

	assert.Empty(t, out.Files)
	assert.Empty(t, out.Effects())
	assert.Equal(t, before, contents(t, root))
}

func TestTxSurvivesTheRoundTrip(t *testing.T) {
	root := writeFixture(t)

	tx := begin(t, root)
	require.NoError(t, tx.Insert(
		"entities/site.dfc",
		written(t, `(node site:Z-03 (label "Eastern campus") (kind Zone) (type Campus))`),
	))
	commit(t, tx)

	// What a write leaves behind reads back as what it meant to write, and
	// writing the same change again over the result changes nothing: canonical
	// form is the fixed point both authors of a file converge on.
	graph, diags := LoadGraph(root)
	require.Empty(t, diags)
	assert.Equal(t, 3, graph.Summary().Nodes())

	after := contents(t, root)

	again := begin(t, root)
	held, ok := again.Form("site:Z-03")
	require.True(t, ok)
	require.NoError(t, again.Replace(held, held))

	out := commit(t, again)

	assert.Empty(t, out.Written())
	assert.Equal(t, after, contents(t, root))
}

func TestUnknownFormErrorReportsWhereItLooked(t *testing.T) {
	// A form built in memory says it came from nowhere, and a message which
	// rendered that as a position would send whoever read it to a file called
	// the empty string.
	assert.NotContains(t, UnknownFormError{}.Error(), ":")

	positioned := UnknownFormError{Span: Span{
		Start: Position{Path: "site.dfc", Line: 3, Column: 1},
	}}
	assert.Contains(t, positioned.Error(), "site.dfc:3:1")
}

func TestRollbackErrorNamesWhatItCouldNotPutBack(t *testing.T) {
	failure := WriteError{Path: "site.dfc", Err: errors.New("out of space")}
	err := RollbackError{Err: failure, Failed: []string{"a.dfc", "b.dfc"}}

	assert.ErrorIs(t, err, failure)
	assert.Contains(t, err.Error(), "a.dfc, b.dfc")
}
