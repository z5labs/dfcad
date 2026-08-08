// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixture every test here derives is testdata/derived/model: a floor plate
// with a courtyard cut out of it, the courtyard itself, a room inside the plate,
// a desk zone inside that room, and a store in a second frame.
//
// Every figure it yields was worked out by hand, which is what makes the cache
// tests worth anything: a cached answer is only as good as the answer it stood
// in for, and comparing two computations of the same wrong number would pass.
const (
	derivedFixture  = "testdata/derived/model"
	derivedPosition = "position"
)

// derivation is what every test below derives against, with the cache filled in
// per test.
func derivation(cache *Cache) Derivation {
	return Derivation{Tolerance: closureTolerance, Position: derivedPosition, Cache: cache}
}

// copyTree copies a fixture into a directory of the test's own, so that a test
// which edits a source file edits its own copy.
func copyTree(t *testing.T, from string) string {
	t.Helper()

	to := t.TempDir()

	require.NoError(t, filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}

		target := filepath.Join(to, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(target, content, 0o644)
	}))

	return to
}

// derived loads a tree and derives it, failing the test where either reported
// anything.
func derived(t *testing.T, root string, cache *Cache) Footprints {
	t.Helper()

	graph, diags := LoadGraph(root)
	require.Empty(t, renderBoundaryDiagnostics(t, diags), "the fixture loads clean")

	prints, derivedDiags := graph.Derive(derivation(cache))
	require.Empty(t, renderBoundaryDiagnostics(t, derivedDiags), "the fixture derives clean")

	return prints
}

// treeContents is every file of a tree, keyed by its path relative to the root, so
// that a tree can be compared byte for byte against itself later.
func treeContents(t *testing.T, root string) map[string][]byte {
	t.Helper()

	out := make(map[string][]byte)

	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		out[filepath.ToSlash(rel)] = content
		return nil
	}))

	return out
}

// moveOneCorner rewrites one coordinate of a copied fixture, which is the
// smallest edit a source tree can carry and the one every staleness test below
// is made of.
func moveOneCorner(t *testing.T, root string) {
	t.Helper()

	path := filepath.Join(root, "model.dfc")

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	edited := strings.Replace(string(content), "(value (20.0 0.0 0.0) m)", "(value (30.0 0.0 0.0) m)", 1)
	require.NotEqual(t, string(content), edited, "the edit reached the fixture")

	require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))
}

func TestDigestOf(t *testing.T) {
	testCases := []struct {
		name    string
		edit    func(t *testing.T, root string)
		differs bool
	}{
		{
			name:    "digests a tree nothing changed to the same value",
			edit:    func(t *testing.T, root string) {},
			differs: false,
		},
		{
			name:    "digests a tree one coordinate moved in differently",
			edit:    moveOneCorner,
			differs: true,
		},
		{
			name: "digests a tree an entity file was added to differently",
			edit: func(t *testing.T, root string) {
				require.NoError(t, os.WriteFile(filepath.Join(root, "extra.dfc"), []byte("\n"), 0o644))
			},
			differs: true,
		},
		{
			name: "digests a tree an entity file was renamed in differently",
			edit: func(t *testing.T, root string) {
				require.NoError(t, os.Rename(filepath.Join(root, "model.dfc"), filepath.Join(root, "renamed.dfc")))
			},
			differs: true,
		},
		{
			name: "digests a tree a file which is not an entity file was added to the same",
			edit: func(t *testing.T, root string) {
				require.NoError(t, os.WriteFile(filepath.Join(root, "notes.md"), []byte("nothing derives from this\n"), 0o644))
			},
			differs: false,
		},
		{
			name: "digests a tree a build output was written into the same",
			edit: func(t *testing.T, root string) {
				dir := CacheDir(root)
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "entry.json"), []byte("{}"), 0o644))
			},
			differs: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := copyTree(t, derivedFixture)

			before, err := DigestOf(root)
			require.NoError(t, err)
			require.True(t, before.Known(), "a tree which was read has a digest")

			testCase.edit(t, root)

			after, err := DigestOf(root)
			require.NoError(t, err)

			if testCase.differs {
				assert.NotEqual(t, before, after)
				return
			}
			assert.Equal(t, before, after)
		})
	}
}

func TestDigestOfIsDeterministic(t *testing.T) {
	t.Run("digests one tree to the same value however often it is asked", func(t *testing.T) {
		first, err := DigestOf(derivedFixture)
		require.NoError(t, err)

		second, err := DigestOf(derivedFixture)
		require.NoError(t, err)

		assert.Equal(t, first, second)
	})

	t.Run("digests a copy of a tree to the same value as the tree", func(t *testing.T) {
		original, err := DigestOf(derivedFixture)
		require.NoError(t, err)

		copied, err := DigestOf(copyTree(t, derivedFixture))
		require.NoError(t, err)

		assert.Equal(t, original, copied, "nothing about the filesystem enters a digest")
	})

	t.Run("digests a tree holding no entity file to a digest of its own", func(t *testing.T) {
		empty, err := DigestOf(t.TempDir())
		require.NoError(t, err)

		assert.True(t, empty.Known(), "a model nobody has written yet is still a model")

		model, err := DigestOf(derivedFixture)
		require.NoError(t, err)

		assert.NotEqual(t, empty, model)
	})

	t.Run("reports the file which stopped it", func(t *testing.T) {
		_, err := DigestOf(filepath.Join(t.TempDir(), "nothing-here"))

		var digestErr DigestError
		require.ErrorAs(t, err, &digestErr)
		assert.True(t, errors.Is(err, fs.ErrNotExist))
	})
}

func TestDigestText(t *testing.T) {
	t.Run("reads back a digest it wrote", func(t *testing.T) {
		digest, err := DigestOf(derivedFixture)
		require.NoError(t, err)

		text, err := digest.MarshalText()
		require.NoError(t, err)

		var read Digest
		require.NoError(t, read.UnmarshalText(text))

		assert.Equal(t, digest, read)
		assert.Equal(t, digest.String(), string(text))
	})

	t.Run("writes the digest of nothing as nothing", func(t *testing.T) {
		text, err := Digest{}.MarshalText()
		require.NoError(t, err)
		assert.Empty(t, text)

		var read Digest
		require.NoError(t, read.UnmarshalText(text))

		assert.Equal(t, Digest{}, read)
		assert.False(t, read.Known())
		assert.Equal(t, "unknown", read.String())
	})

	t.Run("refuses text which is not hex at all", func(t *testing.T) {
		var read Digest

		var digestErr DigestError
		require.ErrorAs(t, read.UnmarshalText([]byte("not a digest")), &digestErr)
	})

	t.Run("refuses a digest of some other width, saying which", func(t *testing.T) {
		var read Digest

		err := read.UnmarshalText([]byte("beef"))

		var lengthErr DigestLengthError
		require.ErrorAs(t, err, &lengthErr)
		assert.Equal(t, sha256.Size, lengthErr.Want)
		assert.Equal(t, 2, lengthErr.Got)
	})
}

func TestGraphDigest(t *testing.T) {
	t.Run("digests the bytes it was built from", func(t *testing.T) {
		graph, diags := LoadGraph(derivedFixture)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		digest, ok := graph.Digest()
		require.True(t, ok)

		direct, err := DigestOf(derivedFixture)
		require.NoError(t, err)

		assert.Equal(t, direct, digest, "one rule, computed in one place")
	})

	t.Run("keeps the digest of what it read when the tree changes underneath it", func(t *testing.T) {
		root := copyTree(t, derivedFixture)

		graph, diags := LoadGraph(root)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		read, ok := graph.Digest()
		require.True(t, ok)

		moveOneCorner(t, root)

		after, ok := graph.Digest()
		require.True(t, ok)
		assert.Equal(t, read, after, "a digest names the model in hand, not whatever is on disk when it is asked for")

		now, err := DigestOf(root)
		require.NoError(t, err)
		assert.NotEqual(t, read, now)
	})

	t.Run("digests a graph a transaction read the same way", func(t *testing.T) {
		root := copyTree(t, derivedFixture)

		tx, diags, err := Begin(root)
		require.NoError(t, err)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))
		defer func() { _ = tx.Close() }()

		digest, ok := tx.Graph().Digest()
		require.True(t, ok, "a transaction's graph is as much a reading of the tree as any other")

		direct, err := DigestOf(root)
		require.NoError(t, err)
		assert.Equal(t, direct, digest)
	})

	t.Run("has no digest where the bytes were not read from anywhere", func(t *testing.T) {
		var parsed []source
		for src := range readTree(derivedFixture) {
			require.NotNil(t, src.file, "the fixture parses")
			parsed = append(parsed, src)
		}

		graph, diags := loadGraph(derivedFixture, parsed, nil, registeredChecks)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		digest, ok := graph.Digest()

		assert.False(t, ok, "nothing may be keyed by a digest nobody accumulated")
		assert.False(t, digest.Known())
	})

	t.Run("has no digest where a file the walk reached could not be read", func(t *testing.T) {
		root := copyTree(t, derivedFixture)

		// A dangling symlink is an entity file a walk reaches and nothing can
		// read, which is the case wanted here. Making one unreadable by its
		// permissions would not be: a run as root reads it anyway, and the test
		// would then assert nothing wherever CI happens to be root.
		require.NoError(t, os.Symlink("gone.dfc", filepath.Join(root, "dangling.dfc")))

		graph, diags := LoadGraph(root)
		require.NotEmpty(t, diags, "the file which could not be read is reported")

		_, ok := graph.Digest()
		assert.False(t, ok, "one key would name a different set of inputs on a machine which could read it")

		_, err := DigestOf(root)
		var digestErr DigestError
		require.ErrorAs(t, err, &digestErr, "and reading the tree for a digest reports it rather than skipping it")
	})
}

func TestDerive(t *testing.T) {
	testCases := []struct {
		name      string
		subject   ID
		frame     ID
		area      float64
		perimeter float64
		centroid  Point
		bounds    Box
		pieces    int
		holes     int
		within    []ID
	}{
		{
			name:      "derives a floor plate with the courtyard taken out of it",
			subject:   "site:S-01",
			frame:     "frame:building",
			area:      192,
			perimeter: 72,
			centroid:  Point{10, 5, 0},
			bounds:    Box{Min: Point{0, 0, 0}, Max: Point{20, 10, 0}, Unit: "m"},
			pieces:    1,
			holes:     1,
		},
		{
			name:      "derives the courtyard which is that hole, as a region of its own",
			subject:   "site:S-02",
			frame:     "frame:building",
			area:      8,
			perimeter: 12,
			centroid:  Point{10, 5, 0},
			bounds:    Box{Min: Point{8, 4, 0}, Max: Point{12, 6, 0}, Unit: "m"},
			pieces:    1,
		},
		{
			name:      "derives a room as a member of the plate it sits in",
			subject:   "site:S-03",
			frame:     "frame:building",
			area:      16,
			perimeter: 16,
			centroid:  Point{4, 4, 0},
			bounds:    Box{Min: Point{2, 2, 0}, Max: Point{6, 6, 0}, Unit: "m"},
			pieces:    1,
			within:    []ID{"site:S-01"},
		},
		{
			name:      "derives a desk zone as a member of both the room and the plate",
			subject:   "site:S-04",
			frame:     "frame:building",
			area:      1,
			perimeter: 4,
			centroid:  Point{3.5, 3.5, 0},
			bounds:    Box{Min: Point{3, 3, 0}, Max: Point{4, 4, 0}, Unit: "m"},
			pieces:    1,
			within:    []ID{"site:S-01", "site:S-03"},
		},
		{
			name:      "derives a region in another frame as a member of nothing",
			subject:   "site:S-05",
			frame:     "frame:annex",
			area:      12,
			perimeter: 14,
			centroid:  Point{2, 1.5, 0},
			bounds:    Box{Min: Point{0, 0, 0}, Max: Point{4, 3, 0}, Unit: "m"},
			pieces:    1,
		},
	}

	prints := derived(t, derivedFixture, nil)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			print, ok := prints.Of(testCase.subject)
			require.True(t, ok, "the model derives a footprint for %s", testCase.subject)

			assert.Equal(t, testCase.subject, print.Subject())
			assert.Equal(t, testCase.frame, print.Frame())
			assert.Equal(t, Unit("m"), print.Unit())

			area, hasArea := print.Area()
			require.True(t, hasArea)
			assert.InDelta(t, testCase.area, area, 1e-9)

			perimeter, hasPerimeter := print.Perimeter()
			require.True(t, hasPerimeter)
			assert.InDelta(t, testCase.perimeter, perimeter, 1e-9)

			centroid, hasCentroid := print.Centroid()
			require.True(t, hasCentroid)
			for axis := range 3 {
				assert.InDelta(t, testCase.centroid[axis], centroid[axis], 1e-9)
			}

			bounds, hasBounds := print.Bounds()
			require.True(t, hasBounds)
			assert.Equal(t, testCase.bounds, bounds)

			pieces := print.Pieces()
			require.Len(t, pieces, testCase.pieces)

			var holes int
			for _, piece := range pieces {
				holes += len(piece.Holes())
			}
			assert.Equal(t, testCase.holes, holes)

			assert.Equal(t, testCase.within, print.Within())
			assert.Equal(t, prints.Digest(), print.Digest())
		})
	}
}

func TestDeriveReports(t *testing.T) {
	t.Run("leaves out a node which references no loop", func(t *testing.T) {
		prints := derived(t, derivedFixture, nil)

		assert.Equal(t, 5, prints.Len(), "the fixture holds five nodes which cover area")

		_, ok := prints.Of("geom:V-011")
		assert.False(t, ok, "a vertex is not a region")
	})

	t.Run("iterates in the order the walk reached the nodes", func(t *testing.T) {
		prints := derived(t, derivedFixture, nil)

		var order []ID
		for print := range prints.All() {
			order = append(order, print.Subject())
		}

		assert.Equal(t, []ID{"site:S-01", "site:S-02", "site:S-03", "site:S-04", "site:S-05"}, order)
	})

	t.Run("says what it was derived under", func(t *testing.T) {
		prints := derived(t, derivedFixture, nil)

		assert.Equal(t, closureTolerance, prints.Tolerance())
		assert.Equal(t, derivedPosition, prints.Position())
		assert.True(t, prints.Digest().Known())
	})

	t.Run("reports the geometry it could not read rather than a figure", func(t *testing.T) {
		graph, diags := LoadGraph("testdata/measure/self-intersecting")
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		prints, derivedDiags := graph.Derive(derivation(nil))

		assert.Zero(t, prints.Len())
		assert.NotEmpty(t, derivedDiags)
	})
}

func TestDeriveWithCache(t *testing.T) {
	t.Run("computes the first derivation and reads the second out of the cache", func(t *testing.T) {
		cache, err := OpenCache(t.TempDir())
		require.NoError(t, err)

		first := derived(t, derivedFixture, cache)
		assert.Equal(t, CacheStats{Misses: 1, Stores: 1}, cache.Stats())

		second := derived(t, derivedFixture, cache)
		assert.Equal(t, CacheStats{Misses: 1, Stores: 1, Hits: 1}, cache.Stats())

		assert.Equal(t, first, second, "a cached derivation is the derivation")
	})

	t.Run("misses on a source tree which changed, so a stale entry can never be read", func(t *testing.T) {
		root := copyTree(t, derivedFixture)

		cache, err := OpenCache(t.TempDir())
		require.NoError(t, err)

		before := derived(t, root, cache)
		plate, ok := before.Of("site:S-01")
		require.True(t, ok)
		area, _ := plate.Area()
		require.InDelta(t, 192.0, area, 1e-9)

		moveOneCorner(t, root)

		after := derived(t, root, cache)
		assert.Equal(t, CacheStats{Misses: 2, Stores: 2}, cache.Stats(), "a changed tree is a different key")
		assert.NotEqual(t, before.Digest(), after.Digest())

		widened, ok := after.Of("site:S-01")
		require.True(t, ok)
		area, _ = widened.Area()
		assert.InDelta(t, 242.0, area, 1e-9,
			"the moved corner makes the plate a trapezoid of 250, and its courtyard is still eight square metres")
	})

	t.Run("misses on a derivation judged against another tolerance", func(t *testing.T) {
		cache, err := OpenCache(t.TempDir())
		require.NoError(t, err)

		derived(t, derivedFixture, cache)

		graph, diags := LoadGraph(derivedFixture)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		_, _ = graph.Derive(Derivation{Tolerance: "not-a-tolerance", Position: derivedPosition, Cache: cache})

		assert.Equal(t, 2, cache.Stats().Misses, "the tolerance is part of the key")
		assert.Equal(t, 0, cache.Stats().Hits)
	})

	t.Run("stores nothing for a derivation which reported a diagnostic", func(t *testing.T) {
		cache, err := OpenCache(t.TempDir())
		require.NoError(t, err)

		graph, diags := LoadGraph("testdata/overlay/shapes")
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		first, firstDiags := graph.Derive(derivation(cache))
		require.NotEmpty(t, firstDiags, "the fixture holds a ring which crosses itself")

		second, secondDiags := graph.Derive(derivation(cache))

		assert.Equal(t, CacheStats{Misses: 2}, cache.Stats(), "a run which reported something is recomputed")
		assert.Equal(t, firstDiags, secondDiags, "the diagnostics of a run never depend on the cache")
		assert.Equal(t, first, second)
	})

	t.Run("caches nothing for a graph which has no digest", func(t *testing.T) {
		var parsed []source
		for src := range readTree(derivedFixture) {
			parsed = append(parsed, src)
		}

		graph, diags := loadGraph(derivedFixture, parsed, nil, registeredChecks)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		cache, err := OpenCache(t.TempDir())
		require.NoError(t, err)

		prints, derivedDiags := graph.Derive(derivation(cache))
		require.Empty(t, renderBoundaryDiagnostics(t, derivedDiags))

		assert.Equal(t, 5, prints.Len())
		assert.Equal(t, CacheStats{Misses: 1, Errors: 1}, cache.Stats())

		entries, err := os.ReadDir(cache.Dir())
		require.NoError(t, err)
		assert.Empty(t, entries, "an entry pinned by nothing could be read back against any tree at all")
	})

	t.Run("derives everything every time against no cache at all", func(t *testing.T) {
		graph, diags := LoadGraph(derivedFixture)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		first, _ := graph.Derive(Derivation{Tolerance: closureTolerance, Position: derivedPosition})
		second, _ := graph.Derive(Derivation{Tolerance: closureTolerance, Position: derivedPosition})

		assert.Equal(t, first, second)
	})
}

func TestCacheIsAdvisory(t *testing.T) {
	t.Run("derives the same thing whether the cache is there or not", func(t *testing.T) {
		dir := t.TempDir()

		cache, err := OpenCache(dir)
		require.NoError(t, err)

		graph, diags := LoadGraph(derivedFixture)
		require.Empty(t, renderBoundaryDiagnostics(t, diags))

		warm, warmDiags := graph.Derive(derivation(cache))
		require.Equal(t, 1, cache.Stats().Stores, "the cache was populated")

		require.NoError(t, os.RemoveAll(dir))

		cold, coldDiags := graph.Derive(derivation(cache))

		assert.Equal(t, warm, cold, "deleting a build output changes results not at all")
		assert.Equal(t, warmDiags, coldDiags)
	})

	t.Run("derives the same thing against no cache as against a warm one", func(t *testing.T) {
		cache, err := OpenCache(t.TempDir())
		require.NoError(t, err)

		derived(t, derivedFixture, cache)

		warm := derived(t, derivedFixture, cache)
		require.Equal(t, 1, cache.Stats().Hits)

		assert.Equal(t, derived(t, derivedFixture, nil), warm)
	})
}

func TestDeriveWritesNothingIntoTheSourceTree(t *testing.T) {
	t.Run("leaves every source file byte-identical after a cache-populating run", func(t *testing.T) {
		root := copyTree(t, derivedFixture)
		before := treeContents(t, root)
		require.NotEmpty(t, before)

		cache, err := OpenCache(t.TempDir())
		require.NoError(t, err)

		derived(t, root, cache)
		require.Equal(t, 1, cache.Stats().Stores, "the run populated the cache")

		derived(t, root, cache)
		require.Equal(t, 1, cache.Stats().Hits, "the run read the cache back")

		assert.Equal(t, before, treeContents(t, root),
			"a derived value in the source tree is a second source of truth which goes stale silently")
	})

	t.Run("writes the cache beneath the build output directory of the tree it derived", func(t *testing.T) {
		root := copyTree(t, derivedFixture)

		cache, err := OpenCache(CacheDir(root))
		require.NoError(t, err)

		prints := derived(t, root, cache)

		entry := filepath.Join(CacheDir(root), prints.Digest().String())
		info, err := os.Stat(entry)
		require.NoError(t, err)
		assert.True(t, info.IsDir(), "an entry is written under the digest it was derived from")

		assert.Equal(t, filepath.Join(root, BuildDir, "cache"), CacheDir(root))

		reread := derived(t, root, cache)
		assert.Equal(t, prints, reread)
		assert.Equal(t, 1, cache.Stats().Hits, "nothing beneath the build output directory is read as a source")
	})
}

func TestBuildOutputDirectoryIsGitignored(t *testing.T) {
	t.Run("ignores the build output directory", func(t *testing.T) {
		content, err := os.ReadFile(".gitignore")
		require.NoError(t, err)

		assert.Contains(t, strings.Split(string(content), "\n"), BuildDir+"/",
			"a build output committed beside its source is a second copy of the model which goes stale")
	})
}

func TestCacheDiscardsWhatDoesNotVerify(t *testing.T) {
	testCases := []struct {
		name   string
		damage func(t *testing.T, content []byte) []byte
	}{
		{
			name: "an entry truncated mid-write",
			damage: func(t *testing.T, content []byte) []byte {
				return content[:len(content)/2]
			},
		},
		{
			name: "an entry with a byte flipped inside a number",
			damage: func(t *testing.T, content []byte) []byte {
				return []byte(strings.Replace(string(content), "192", "193", 1))
			},
		},
		{
			name: "an entry with no checksum at all",
			damage: func(t *testing.T, content []byte) []byte {
				_, payload, _ := strings.Cut(string(content), "\n")
				return []byte(payload)
			},
		},
		{
			name: "an entry whose payload is not JSON",
			damage: func(t *testing.T, content []byte) []byte {
				payload := []byte("this is not an entry")
				sum := sha256.Sum256(payload)
				return append([]byte(hex.EncodeToString(sum[:])+"\n"), payload...)
			},
		},
		{
			name: "an entry written by another version of the engine",
			damage: func(t *testing.T, content []byte) []byte {
				_, payload, _ := strings.Cut(string(content), "\n")

				var entry map[string]any
				require.NoError(t, json.Unmarshal([]byte(payload), &entry))
				entry["version"] = cacheVersion + 1

				rewritten, err := json.Marshal(entry)
				require.NoError(t, err)

				sum := sha256.Sum256(rewritten)
				return append([]byte(hex.EncodeToString(sum[:])+"\n"), rewritten...)
			},
		},
		{
			name: "an entry written under another key",
			damage: func(t *testing.T, content []byte) []byte {
				_, payload, _ := strings.Cut(string(content), "\n")

				var entry map[string]any
				require.NoError(t, json.Unmarshal([]byte(payload), &entry))
				entry["tolerance"] = "some-other-tolerance"

				rewritten, err := json.Marshal(entry)
				require.NoError(t, err)

				sum := sha256.Sum256(rewritten)
				return append([]byte(hex.EncodeToString(sum[:])+"\n"), rewritten...)
			},
		},
		{
			name: "an entry which is empty",
			damage: func(t *testing.T, content []byte) []byte {
				return nil
			},
		},
	}

	for _, testCase := range testCases {
		t.Run("discards "+testCase.name+" and recomputes it", func(t *testing.T) {
			dir := t.TempDir()

			cache, err := OpenCache(dir)
			require.NoError(t, err)

			clean := derived(t, derivedFixture, cache)
			require.Equal(t, 1, cache.Stats().Stores)

			path := onlyEntry(t, dir)

			content, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, testCase.damage(t, content), 0o644))

			recomputed := derived(t, derivedFixture, cache)

			assert.Equal(t, 1, cache.Stats().Discards, "a corrupt build output is a recomputation, never a failed run")
			assert.Equal(t, 0, cache.Stats().Hits)
			assert.Equal(t, clean, recomputed)

			// What did not verify was thrown away and the recomputation wrote a
			// fresh entry in its place, which the next run reads.
			assert.Equal(t, 2, cache.Stats().Stores)
			assert.Equal(t, clean, derived(t, derivedFixture, cache))
			assert.Equal(t, 1, cache.Stats().Hits)
		})
	}
}

// onlyEntry is the path of the single entry a cache holds, failing the test
// where it holds any other number of them.
func onlyEntry(t *testing.T, dir string) string {
	t.Helper()

	var paths []string
	require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		paths = append(paths, path)
		return nil
	}))

	require.Len(t, paths, 1, "the cache holds one entry")

	return paths[0]
}

func TestCachePrune(t *testing.T) {
	t.Run("keeps one generation and removes every other", func(t *testing.T) {
		root := copyTree(t, derivedFixture)

		cache, err := OpenCache(t.TempDir())
		require.NoError(t, err)

		derived(t, root, cache)
		moveOneCorner(t, root)
		latest := derived(t, root, cache)

		generations, err := os.ReadDir(cache.Dir())
		require.NoError(t, err)
		require.Len(t, generations, 2, "a run against a new revision writes a new generation")

		removed, err := cache.Prune(latest.Digest())
		require.NoError(t, err)
		assert.Equal(t, 1, removed)

		generations, err = os.ReadDir(cache.Dir())
		require.NoError(t, err)
		require.Len(t, generations, 1)
		assert.Equal(t, latest.Digest().String(), generations[0].Name())

		kept := derived(t, root, cache)
		assert.Equal(t, 1, cache.Stats().Hits, "the generation which was kept is the working set")
		assert.Equal(t, latest, kept)
	})

	t.Run("keeps nothing when it is given no digest", func(t *testing.T) {
		cache, err := OpenCache(t.TempDir())
		require.NoError(t, err)

		derived(t, derivedFixture, cache)

		removed, err := cache.Prune(Digest{})
		require.NoError(t, err)
		assert.Equal(t, 1, removed)

		generations, err := os.ReadDir(cache.Dir())
		require.NoError(t, err)
		assert.Empty(t, generations)

		derived(t, derivedFixture, cache)
		assert.Equal(t, 0, cache.Stats().Hits)
	})

	t.Run("prunes a cache which was never written to", func(t *testing.T) {
		cache, err := OpenCache(filepath.Join(t.TempDir(), "not-yet"))
		require.NoError(t, err)
		require.NoError(t, os.RemoveAll(cache.Dir()))

		removed, err := cache.Prune(Digest{})
		require.NoError(t, err)
		assert.Zero(t, removed)
	})
}

func TestNilCache(t *testing.T) {
	t.Run("holds nothing and reports nothing", func(t *testing.T) {
		var cache *Cache

		digest, err := DigestOf(derivedFixture)
		require.NoError(t, err)

		key := Key{Digest: digest, Tolerance: closureTolerance, Position: derivedPosition}

		_, ok := cache.Lookup(key)
		assert.False(t, ok)

		require.NoError(t, cache.Store(key, Footprints{}))

		removed, err := cache.Prune(digest)
		require.NoError(t, err)
		assert.Zero(t, removed)

		assert.Equal(t, CacheStats{}, cache.Stats())
		assert.Empty(t, cache.Dir())
	})
}

func TestKey(t *testing.T) {
	t.Run("writes the digest and both names it pins", func(t *testing.T) {
		digest, err := DigestOf(derivedFixture)
		require.NoError(t, err)

		key := Key{Digest: digest, Tolerance: closureTolerance, Position: derivedPosition}

		assert.Contains(t, key.String(), digest.String())
		assert.Contains(t, key.String(), closureTolerance)
		assert.Contains(t, key.String(), derivedPosition)
	})

	t.Run("writes two keys differing only in a name to different entries", func(t *testing.T) {
		digest, err := DigestOf(derivedFixture)
		require.NoError(t, err)

		entries := []string{
			Key{Digest: digest, Tolerance: "a", Position: "bc"}.entry(),
			Key{Digest: digest, Tolerance: "ab", Position: "c"}.entry(),
			Key{Digest: digest, Tolerance: "a", Position: "c"}.entry(),
		}

		assert.Len(t, slices.Compact(slices.Sorted(slices.Values(entries))), len(entries),
			"every field of a key is written into it with its length")
	})
}

func TestCacheOpen(t *testing.T) {
	t.Run("creates the directory it was given", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "build", "cache")

		cache, err := OpenCache(dir)
		require.NoError(t, err)

		assert.Equal(t, dir, cache.Dir())

		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("reports a directory it cannot create", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "not-a-directory")
		require.NoError(t, os.WriteFile(file, []byte("."), 0o644))

		_, err := OpenCache(filepath.Join(file, "cache"))

		var cacheErr CacheError
		require.ErrorAs(t, err, &cacheErr)
		assert.Equal(t, "open", cacheErr.Op)
		assert.NotNil(t, cacheErr.Unwrap())
	})
}

// BenchmarkDerive is what the cache buys, on the fixture corpus: a miss pays for
// every footprint and every pairwise containment, a hit pays for the digest and
// a read.
//
// The digest is in both numbers deliberately. A hit is not free — the tree still
// has to be read to know which entry to read — and a benchmark which left that
// out would be measuring something no caller can have.
func BenchmarkDerive(b *testing.B) {
	graph, diags := LoadGraph(derivedFixture)
	if len(diags) > 0 {
		b.Fatalf("the fixture loads clean, got %d diagnostics", len(diags))
	}

	b.Run("miss", func(b *testing.B) {
		cache, err := OpenCache(b.TempDir())
		if err != nil {
			b.Fatal(err)
		}

		for b.Loop() {
			b.StopTimer()
			if _, err := cache.Prune(Digest{}); err != nil {
				b.Fatal(err)
			}
			// The digest is remembered on the graph, so a miss has to be forced
			// by emptying the cache rather than by changing the tree.
			b.StartTimer()

			if _, derivedDiags := graph.Derive(Derivation{
				Tolerance: closureTolerance,
				Position:  derivedPosition,
				Cache:     cache,
			}); len(derivedDiags) > 0 {
				b.Fatalf("the fixture derives clean, got %d diagnostics", len(derivedDiags))
			}
		}

		if cache.Stats().Hits > 0 {
			b.Fatalf("every iteration missed, got %d hits", cache.Stats().Hits)
		}
	})

	b.Run("hit", func(b *testing.B) {
		cache, err := OpenCache(b.TempDir())
		if err != nil {
			b.Fatal(err)
		}

		against := Derivation{Tolerance: closureTolerance, Position: derivedPosition, Cache: cache}
		if _, derivedDiags := graph.Derive(against); len(derivedDiags) > 0 {
			b.Fatalf("the fixture derives clean, got %d diagnostics", len(derivedDiags))
		}

		before := cache.Stats().Hits
		for b.Loop() {
			graph.Derive(against)
		}

		if cache.Stats().Hits == before {
			b.Fatal("every iteration hit the cache, got none")
		}
	})
}
