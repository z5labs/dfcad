// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// git runs one git command in dir and fails the test if it refuses.
//
// The identity and the initial branch are set on the repository rather than
// taken from whoever is running the test, so a machine with no git identity
// configured and one with a different init.defaultBranch both produce the same
// fixture.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=A Surveyor",
		"GIT_AUTHOR_EMAIL=surveyor@example.org",
		"GIT_AUTHOR_DATE=2026-06-01T09:30:00+00:00",
		"GIT_COMMITTER_NAME=A Surveyor",
		"GIT_COMMITTER_EMAIL=surveyor@example.org",
		"GIT_COMMITTER_DATE=2026-06-01T09:30:00+00:00",
	)

	out, err := command.CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)

	return strings.TrimSpace(string(out))
}

// repository writes a model into a new git repository and commits it on the
// default branch, which is what a merge base is taken against.
func repository(t *testing.T, files map[string]string) string {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on the path, and this is what it tests")
	}

	root := t.TempDir()

	git(t, root, "init", "--initial-branch=main", "--quiet")
	git(t, root, "config", "user.name", "A Surveyor")
	git(t, root, "config", "user.email", "surveyor@example.org")

	committed(t, root, "the model as it stands", files)

	return root
}

// committed writes files into a working tree and commits everything in it.
func committed(t *testing.T, root string, message string, files map[string]string) string {
	t.Helper()

	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))

		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	git(t, root, "add", "--all")
	git(t, root, "commit", "--quiet", "--message", message)

	return git(t, root, "rev-parse", "HEAD")
}

// branched is a repository holding the merge base on main and one change on a
// branch, which is the shape every review runs against.
//
// It returns the working tree, the merge base and the commit which made the
// change.
func branched(t *testing.T, base, head map[string]string) (root, mergeBase, change string) {
	t.Helper()

	root = repository(t, base)
	mergeBase = git(t, root, "rev-parse", "HEAD")

	git(t, root, "checkout", "--quiet", "-b", "widen-the-room")
	change = committed(t, root, "story(site): widen Meeting Room A", head)

	// Something lands on main after the branch was taken, which is what makes
	// the merge base and the tip of main two different commits.
	git(t, root, "checkout", "--quiet", "main")
	committed(t, root, "chore(docs): note the survey programme", map[string]string{"NOTES.md": "The survey runs in June.\n"})
	git(t, root, "checkout", "--quiet", "widen-the-room")

	return root, mergeBase, change
}

func TestOpenRepository(t *testing.T) {
	t.Run("finds the working tree from a directory inside it", func(t *testing.T) {
		root := repository(t, reviewBase())

		repo, err := OpenRepository(filepath.Join(root, "entities"))

		require.NoError(t, err)
		assert.Equal(t, resolved(t, root), resolved(t, repo.Dir()))
		assert.False(t, repo.Shallow())
	})

	t.Run("reports a directory which is not inside one, and says which", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git is not on the path, and this is what it tests")
		}

		dir := t.TempDir()

		_, err := OpenRepository(dir)

		require.ErrorIs(t, err, ErrNotARepository)

		var outside NotARepositoryError
		require.ErrorAs(t, err, &outside)
		assert.Equal(t, dir, outside.Dir)
	})
}

func TestRepositoryPrefix(t *testing.T) {
	root := repository(t, reviewBase())

	repo, err := OpenRepository(root)
	require.NoError(t, err)

	testCases := []struct {
		name           string
		dir            string
		expectedPrefix string
	}{
		{
			name:           "reports the top level as no prefix at all",
			dir:            root,
			expectedPrefix: "",
		},
		{
			name:           "reports a model beneath the top level as its path, slash-separated",
			dir:            filepath.Join(root, "entities"),
			expectedPrefix: "entities",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			prefix, err := repo.Prefix(testCase.dir)

			require.NoError(t, err)
			assert.Equal(t, testCase.expectedPrefix, prefix)
		})
	}

	t.Run("refuses a directory the repository does not hold", func(t *testing.T) {
		elsewhere := filepath.Join(root, "..", "elsewhere")

		_, err := repo.Prefix(elsewhere)

		require.ErrorIs(t, err, ErrOutsideModel)

		var outside OutsideRepositoryError
		require.ErrorAs(t, err, &outside)
		assert.Equal(t, elsewhere, outside.Dir)
		assert.Equal(t, repo.Dir(), outside.Root)
	})
}

func TestRepositoryMergeBase(t *testing.T) {
	root, mergeBase, _ := branched(t, reviewBase(), revised(map[string]string{
		"entities/geometry.dfc": strings.Replace(reviewGeometry, reviewCornerBefore, reviewCornerRewritten, 1),
	}))

	repo, err := OpenRepository(root)
	require.NoError(t, err)

	t.Run("is the commit the two revisions last shared, not the tip of the branch", func(t *testing.T) {
		got, err := repo.MergeBase("HEAD", "main")

		require.NoError(t, err)
		assert.Equal(t, mergeBase, got)
		assert.NotEqual(t, git(t, root, "rev-parse", "main"), got, "the tip of main has moved on since the branch was taken")
	})

	t.Run("reports a revision the repository does not hold", func(t *testing.T) {
		_, err := repo.Resolve("no-such-branch")

		var unknown UnknownRevisionError
		require.ErrorAs(t, err, &unknown)
		assert.Equal(t, "no-such-branch", unknown.Revision)
	})
}

// TestRepositoryMergeBaseRefusesAShallowHistory is its own function because it
// needs a second repository cloned from the first, which no other case does.
//
// It is the case a CI checkout produces by default, and the one which has to
// say what it needs rather than answer: git reports a merge base at the point
// the history was cut off, and a review against that base would attribute the
// whole of the repository's history to this change.
func TestRepositoryMergeBaseRefusesAShallowHistory(t *testing.T) {
	root, _, _ := branched(t, reviewBase(), revised(map[string]string{
		"entities/geometry.dfc": strings.Replace(reviewGeometry, reviewCornerBefore, reviewCornerRewritten, 1),
	}))

	shallow := filepath.Join(t.TempDir(), "checkout")
	git(t, t.TempDir(), "clone", "--quiet", "--depth", "1", "--branch", "widen-the-room",
		"file://"+resolved(t, root), shallow)

	repo, err := OpenRepository(shallow)
	require.NoError(t, err)
	require.True(t, repo.Shallow(), "a clone of depth one is what a CI checkout does by default")

	_, err = repo.MergeBase("HEAD", "origin/main")

	var truncated ShallowHistoryError
	require.ErrorAs(t, err, &truncated)
	assert.Contains(t, truncated.Error(), "fetch-depth: 0", "a refusal a CI job cannot act on is no better than a wrong answer")
	assert.Contains(t, truncated.Error(), "git fetch --unshallow")
}

func TestRepositoryExtract(t *testing.T) {
	root, mergeBase, _ := branched(t, reviewBase(), revised(map[string]string{
		"entities/geometry.dfc": strings.Replace(reviewGeometry, reviewCornerBefore, reviewCornerRewritten, 1),
	}))

	repo, err := OpenRepository(root)
	require.NoError(t, err)

	dest := t.TempDir()
	require.NoError(t, repo.Extract(mergeBase, dest))

	t.Run("writes the tree as that revision held it, not as the working tree holds it", func(t *testing.T) {
		src, err := os.ReadFile(filepath.Join(dest, "entities", "geometry.dfc"))

		require.NoError(t, err)
		assert.Contains(t, string(src), "(4.0 0.0 0.0)", "the merge base has the corner where it was")
		assert.NotContains(t, string(src), "(4.6 0.0 0.0)")
	})

	t.Run("writes a tree the loader reads as a model", func(t *testing.T) {
		graph, diags := LoadGraph(dest)

		require.NotNil(t, graph)
		assert.Empty(t, errorsAmong(diags), "the merge base of a fixture which loads also loads")
		assert.Equal(t, 2, graph.Nodes().Len())
	})
}

// TestRepositoryLogAttributesEveryFileToTheCommitWhichChangedIt is its own
// function because it asserts about the attribution rather than about a review,
// which is the half of "each finding names the commit" that git answers.
func TestRepositoryLogAttributesEveryFileToTheCommitWhichChangedIt(t *testing.T) {
	root, mergeBase, change := branched(t, reviewBase(), revised(map[string]string{
		"entities/geometry.dfc": strings.Replace(reviewGeometry, reviewCornerBefore, reviewCornerRewritten, 1),
	}))

	repo, err := OpenRepository(root)
	require.NoError(t, err)

	log, err := repo.Log(mergeBase, "HEAD")
	require.NoError(t, err)
	assert.Equal(t, 1, log.Len(), "the change touched one file")

	commit, ok := log.Within(root, "").Introduced(filepath.Join(root, "entities", "geometry.dfc"))

	require.True(t, ok)
	assert.Equal(t, change, commit.SHA)
	assert.Equal(t, "story(site): widen Meeting Room A", commit.Summary)
	assert.Equal(t, "A Surveyor", commit.Author)
	assert.Equal(t, "2026-06-01", commit.Date.UTC().Format("2006-01-02"))

	t.Run("attributes nothing to a file the change did not touch", func(t *testing.T) {
		_, ok := log.Within(root, "").Introduced(filepath.Join(root, "registry.dfc"))

		assert.False(t, ok)
	})

	t.Run("attributes nothing to a file outside the tree it was asked about", func(t *testing.T) {
		_, ok := log.Within(root, "").Introduced(filepath.Join(t.TempDir(), "geometry.dfc"))

		assert.False(t, ok)
	})

	t.Run("puts the prefix back for a model read out of a tree extracted elsewhere", func(t *testing.T) {
		// The merge base is read out of a directory of its own, and a model
		// which does not sit at the top level of the repository is read from a
		// subdirectory of that. Without the prefix put back, every file of it
		// is attributed to nothing at all.
		elsewhere := t.TempDir()

		commit, ok := log.Within(filepath.Join(elsewhere, "entities"), "entities").
			Introduced(filepath.Join(elsewhere, "entities", "geometry.dfc"))

		require.True(t, ok, "the same file, read out of the revision it was removed from")
		assert.Equal(t, change, commit.SHA)
	})
}

// TestReviewOfARealChange is its own function because it drives the whole
// mechanism end to end: two revisions read out of one repository, compared, and
// every finding attributed to the commit which introduced it.
func TestReviewOfARealChange(t *testing.T) {
	root, mergeBase, change := branched(t, reviewBase(), revised(map[string]string{
		"entities/geometry.dfc": strings.Replace(reviewGeometry, reviewCornerBefore, reviewCornerRewritten, 1),
	}))

	repo, err := OpenRepository(root)
	require.NoError(t, err)

	base := t.TempDir()
	require.NoError(t, repo.Extract(mergeBase, base))

	log, err := repo.Log(mergeBase, "HEAD")
	require.NoError(t, err)

	head, _ := LoadGraph(root)
	previous, _ := LoadGraph(base)

	findings := Review(previous, head, DefaultPolicy(), Histories{
		log.Within(root, ""),
		log.Within(base, ""),
	})

	require.Len(t, findings, 1)
	assert.Equal(t, FindingBoundaryMoved, findings[0].Kind)
	assert.Equal(t, ID("site:S-101"), findings[0].Subject)
	assert.Equal(t, change, findings[0].Commit.SHA, "the finding names the commit which moved the wall")
	assert.Equal(t, "story(site): widen Meeting Room A", findings[0].Commit.Summary)
}

// TestReviewOfARemovalNamesTheCommitWhichRemovedIt is its own function because
// the finding it is about points into the merge base rather than into head,
// which is the attribution the prefix exists for.
func TestReviewOfARemovalNamesTheCommitWhichRemovedIt(t *testing.T) {
	root := repository(t, reviewBase())
	mergeBase := git(t, root, "rev-parse", "HEAD")

	git(t, root, "checkout", "--quiet", "-b", "tidy-up")
	require.NoError(t, os.Remove(filepath.Join(root, "entities", "site.dfc")))
	git(t, root, "add", "--all")
	git(t, root, "commit", "--quiet", "--message", "chore(site): drop the room file")
	change := git(t, root, "rev-parse", "HEAD")

	repo, err := OpenRepository(root)
	require.NoError(t, err)

	base := t.TempDir()
	require.NoError(t, repo.Extract(mergeBase, base))

	log, err := repo.Log(mergeBase, "HEAD")
	require.NoError(t, err)

	head, _ := LoadGraph(root)
	previous, _ := LoadGraph(base)

	findings := Review(previous, head, DefaultPolicy(), Histories{
		log.Within(root, ""),
		log.Within(base, ""),
	})

	require.NotEmpty(t, findings)
	for _, finding := range findings {
		assert.Equal(t, FindingIDDisappeared, finding.Kind)
		assert.Equal(t, SideBase, finding.Side)
		assert.Equal(t, change, finding.Commit.SHA, "a file which is gone from head is attributed through the base")
	}
}

func TestRepositoryErrors(t *testing.T) {
	testCases := []struct {
		name             string
		err              error
		expectedFragment string
	}{
		{
			name:             "names both revisions when they share no commit",
			err:              NoMergeBaseError{Head: "HEAD", Base: "origin/main"},
			expectedFragment: "no merge base between HEAD and origin/main",
		},
		{
			name:             "names the commit the history was cut off at",
			err:              ShallowHistoryError{Head: "HEAD", Base: "origin/main", Boundary: "5f2b8c1d9e3a47b6c0d1e2f3a4b5c6d7e8f90123"},
			expectedFragment: "cut off at 5f2b8c1d9e3a",
		},
		{
			name:             "says what git was asked and what it said",
			err:              RepositoryError{Dir: "/models", Args: []string{"merge-base", "HEAD", "main"}, Stderr: "fatal: bad object", Cause: os.ErrInvalid},
			expectedFragment: "git merge-base HEAD main in /models",
		},
		{
			name:             "names the archive entry which tried to climb out",
			err:              ArchiveEntryError{Name: "../escaped.dfc", Dest: "/tmp/base"},
			expectedFragment: `archive entry "../escaped.dfc" does not name a path beneath /tmp/base`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Contains(t, testCase.err.Error(), testCase.expectedFragment)
		})
	}

	t.Run("keeps the cause reachable", func(t *testing.T) {
		err := error(RepositoryError{Cause: os.ErrInvalid})

		assert.ErrorIs(t, err, os.ErrInvalid)
	})
}

// resolved is a path with every symbolic link in it followed, which is what
// makes a temporary directory on a machine where /tmp is a link comparable with
// what git reports.
func resolved(t *testing.T, path string) string {
	t.Helper()

	out, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)

	return out
}

// errorsAmong is the diagnostics which refuse a load, which is what a test
// asserting that a tree loads is asserting about.
func errorsAmong(diags []Diagnostic) []Diagnostic {
	var out []Diagnostic
	for _, diag := range diags {
		if diag.Severity == SeverityError {
			out = append(out, diag)
		}
	}
	return out
}

// TestExtractRefusesAPathOutsideTheDestination is its own function because it
// drives the unpacking directly rather than through git: an archive naming a
// path which climbs out of the destination is not something git produces, and
// is exactly what the guard is for.
func TestExtractRefusesAPathOutsideTheDestination(t *testing.T) {
	testCases := []struct {
		name  string
		entry string
	}{
		{
			name:  "refuses a name which begins with a parent reference",
			entry: "../escaped.dfc",
		},
		{
			name:  "refuses a name which walks out through a parent part way along",
			entry: "entities/../../escaped.dfc",
		},
		{
			name:  "refuses an absolute name, which no join would have contained",
			entry: "/escaped.dfc",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dest := t.TempDir()

			err := unpack(archiveOf(t, testCase.entry, "(node site:S-101 (kind Space))\n"), dest)

			require.ErrorIs(t, err, ErrOutsideModel)
			assert.NoFileExists(t, filepath.Join(filepath.Dir(dest), "escaped.dfc"))
			assert.NoFileExists(t, filepath.Join(dest, "escaped.dfc"))

			// The name the archive wrote, not the path joining it would have
			// produced: what is reported is what the input said.
			var refused ArchiveEntryError
			require.ErrorAs(t, err, &refused)
			assert.Equal(t, testCase.entry, refused.Name)
		})
	}
}

// TestExtractSkipsWhatIsNotAFile is its own function for the same reason: a
// model is text files, and an archive entry which is neither a file nor a
// directory is not part of one.
func TestExtractSkipsWhatIsNotAFile(t *testing.T) {
	dest := t.TempDir()

	require.NoError(t, unpack(linkArchive(t, "link.dfc", "registry.dfc"), dest))

	assert.NoFileExists(t, filepath.Join(dest, "link.dfc"))
}

// archiveOf is a tar archive holding one regular file.
func archiveOf(t *testing.T, name, content string) *tar.Reader {
	t.Helper()

	var out bytes.Buffer
	writer := tar.NewWriter(&out)

	require.NoError(t, writer.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeReg,
		Mode:     0o600,
		Size:     int64(len(content)),
	}))
	_, err := writer.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	return tar.NewReader(&out)
}

// linkArchive is a tar archive holding one symbolic link and nothing else.
func linkArchive(t *testing.T, name, target string) *tar.Reader {
	t.Helper()

	var out bytes.Buffer
	writer := tar.NewWriter(&out)

	require.NoError(t, writer.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeSymlink,
		Linkname: target,
		Mode:     0o777,
	}))
	require.NoError(t, writer.Close())

	return tar.NewReader(&out)
}

func TestUnknownRevisionErrorNamesWhatWasAskedFor(t *testing.T) {
	err := error(UnknownRevisionError{Revision: "origin/HEAD"})

	assert.Contains(t, err.Error(), "origin/HEAD")

	var unknown UnknownRevisionError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, "origin/HEAD", unknown.Revision)
}

// TestReviewNeedsBothRevisionsToBeReadable asserts that a working tree which is
// not in a repository is reported as one rather than reviewed against nothing.
func TestReviewNeedsBothRevisionsToBeReadable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on the path, and this is what it tests")
	}

	_, err := OpenRepository(tree(t, reviewBase()))

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotARepository))
}
