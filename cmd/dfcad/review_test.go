// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// git runs one git command in dir and fails the test if it refuses.
//
// The identity, the dates and the initial branch are set on the repository
// rather than taken from whoever is running the test, so a machine with no git
// identity configured produces the same fixture as one with.
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

// reviewed is a repository holding the fixture model on main and one change to
// it on a branch, checked out at the branch.
//
// It returns the working tree and the commit which made the change, because the
// commit is half of what a finding has to say.
func reviewedRepository(t *testing.T, head map[string]string) (root, change string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on the path, and this is what it needs")
	}

	root = tree(t, model())

	git(t, root, "init", "--initial-branch=main", "--quiet")
	git(t, root, "config", "user.name", "A Surveyor")
	git(t, root, "config", "user.email", "surveyor@example.org")
	git(t, root, "add", "--all")
	git(t, root, "commit", "--quiet", "--message", "the model as it stands")

	git(t, root, "checkout", "--quiet", "-b", "the-change")
	for name, content := range head {
		require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(content), 0o600))
	}
	git(t, root, "add", "--all")
	git(t, root, "commit", "--quiet", "--message", "chore(site): tidy up the campus")

	return root, git(t, root, "rev-parse", "HEAD")
}

// withoutTheCampus is the fixture's semantic family with the zone nothing
// references simply deleted, which is an id gone from the model.
func withoutTheCampus(t *testing.T) string {
	t.Helper()

	const campus = `
(node site:C-01
  (label "West campus")
  (kind Zone)
  (type Campus))
`
	require.Contains(t, listModel, campus)

	return strings.Replace(listModel, campus, "", 1)
}

func TestRunReview(t *testing.T) {
	testCases := []struct {
		name             string
		head             map[string]string
		flags            []string
		expectedCode     int
		expectedFindings []string
		expectedSummary  reviewSummary
	}{
		{
			name:             "succeeds and reports nothing when a change touched nothing suspicious",
			head:             map[string]string{"README.md": "The model of the west campus.\n"},
			expectedCode:     exitSuccess,
			expectedFindings: []string{},
		},
		{
			name:             "fails and names the id a change removed",
			head:             nil,
			expectedCode:     exitCheck,
			expectedFindings: []string{"id-disappeared-without-supersession site:C-01"},
			expectedSummary:  reviewSummary{Findings: 1, Failures: 1},
		},
		{
			name:             "downgrades a check the policy warns about, and succeeds",
			head:             nil,
			flags:            []string{"--policy", "id-disappeared-without-supersession=warning"},
			expectedCode:     exitSuccess,
			expectedFindings: []string{"id-disappeared-without-supersession site:C-01"},
			expectedSummary:  reviewSummary{Findings: 1, Warnings: 1},
		},
		{
			name:             "keeps a check the policy acknowledged in the result, and succeeds",
			head:             nil,
			flags:            []string{"--policy", "id-disappeared-without-supersession=ignored"},
			expectedCode:     exitSuccess,
			expectedFindings: []string{"id-disappeared-without-supersession site:C-01"},
			expectedSummary:  reviewSummary{Findings: 1, Ignored: 1},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			head := testCase.head
			if head == nil {
				head = map[string]string{"entities/site.dfc": withoutTheCampus(t)}
			}

			root, change := reviewedRepository(t, head)

			var stdout, stderr bytes.Buffer
			args := append([]string{"review", "--root", root, "--against", "main"}, testCase.flags...)

			require.Equal(t, testCase.expectedCode, run(args, &stdout, &stderr), stderr.String())

			result := listed[reviewResult](t, stdout.String())
			assert.Equal(t, outputVersion, result.Version)
			assert.Equal(t, "review", result.Command)

			found := make([]string, 0, len(result.Findings))
			for _, finding := range result.Findings {
				found = append(found, string(finding.Kind)+" "+string(finding.Subject))

				// Every finding names the commit which introduced the change,
				// which is the whole difference between this and reading the
				// diff by hand.
				assert.Equal(t, change, finding.Commit.SHA)
				assert.Equal(t, "chore(site): tidy up the campus", finding.Commit.Summary)
			}
			assert.Equal(t, testCase.expectedFindings, found)

			if len(testCase.expectedFindings) > 0 {
				assert.Equal(t, testCase.expectedSummary, result.Summary)
			}
		})
	}
}

// TestRunReviewReportsWhatItCompared is its own function because it asserts
// about the comparison rather than about the findings: the merge base is
// derived, and a caller which cannot read which commit a review was measured
// against has to recompute it.
func TestRunReviewReportsWhatItCompared(t *testing.T) {
	root, change := reviewedRepository(t, map[string]string{"entities/site.dfc": withoutTheCampus(t)})

	// Something lands on main after the branch was taken, so that the tip of
	// main and the merge base are two different commits.
	git(t, root, "checkout", "--quiet", "main")
	require.NoError(t, os.WriteFile(filepath.Join(root, "NOTES.md"), []byte("The survey runs in June.\n"), 0o600))
	git(t, root, "add", "--all")
	git(t, root, "commit", "--quiet", "--message", "chore(docs): note the survey programme")
	tip := git(t, root, "rev-parse", "HEAD")
	git(t, root, "checkout", "--quiet", "the-change")

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitCheck, run([]string{"review", "--root", root, "--against", "main"}, &stdout, &stderr), stderr.String())

	result := listed[reviewResult](t, stdout.String())

	assert.Equal(t, "main", result.Comparison.Against)
	assert.Equal(t, change, result.Comparison.Head)
	assert.NotEqual(t, tip, result.Comparison.Base, "the comparison is against the merge base, not the tip of main")
	assert.Equal(t, 1, result.Comparison.Files, "the change touched one file")

	// And the ruling every check ran under, including the ones which reported
	// nothing: what a green run did about the checks it did not report is what
	// a reader of a green run needs to know.
	assert.Equal(t, map[string]string{
		"boundary-moved-without-claim":         "warning",
		"claim-deprecated-without-replacement": "failure",
		"id-disappeared-without-supersession":  "failure",
	}, result.Policy)
}

// TestRunReviewRendersEveryFindingForAPerson is its own function because it
// asserts about stderr rather than about the result object. A finding is a
// problem in something somebody wrote, so it is rendered for them on every run
// and in every format.
func TestRunReviewRendersEveryFindingForAPerson(t *testing.T) {
	root, _ := reviewedRepository(t, map[string]string{"entities/site.dfc": withoutTheCampus(t)})

	t.Run("renders it as a diagnostic", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		require.Equal(t, exitCheck, run([]string{"review", "--root", root, "--against", "main"}, &stdout, &stderr))

		assert.Contains(t, stderr.String(), "site:C-01")
		assert.Contains(t, stderr.String(), "chore(site): tidy up the campus")
		assert.Contains(t, stderr.String(), "dfcad retire site:C-01")
	})

	t.Run("says nothing about a finding the policy acknowledged", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		args := []string{"review", "--root", root, "--against", "main",
			"--policy", "id-disappeared-without-supersession=ignored"}

		require.Equal(t, exitSuccess, run(args, &stdout, &stderr))

		assert.NotContains(t, stderr.String(), "site:C-01", "an acknowledged finding is in the result and nowhere else")
		assert.Len(t, listed[reviewResult](t, stdout.String()).Findings, 1)
	})
}

// TestRunReviewWritesTheSummaryAReviewerReads is its own function because it
// asserts about a file the run wrote rather than about either stream.
func TestRunReviewWritesTheSummaryAReviewerReads(t *testing.T) {
	root, _ := reviewedRepository(t, map[string]string{"entities/site.dfc": withoutTheCampus(t)})
	summary := filepath.Join(t.TempDir(), "step-summary.md")

	var stdout, stderr bytes.Buffer
	args := []string{"review", "--root", root, "--against", "main", "--annotate", summary}

	require.Equal(t, exitCheck, run(args, &stdout, &stderr), stderr.String())

	written, err := os.ReadFile(summary)
	require.NoError(t, err)

	assert.Contains(t, string(written), "## dfcad review")
	assert.Contains(t, string(written), "id-disappeared-without-supersession")
	assert.Contains(t, string(written), "site:C-01")

	t.Run("appends rather than replacing, because the file is one step's summary", func(t *testing.T) {
		require.Equal(t, exitCheck, run(args, &stdout, &stderr), stderr.String())

		again, err := os.ReadFile(summary)
		require.NoError(t, err)
		assert.Equal(t, 2, strings.Count(string(again), "## dfcad review"))
	})

	t.Run("says so plainly when there is nothing to report", func(t *testing.T) {
		clean, _ := reviewedRepository(t, map[string]string{"README.md": "Nothing to see.\n"})
		nothing := filepath.Join(t.TempDir(), "step-summary.md")

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitSuccess, run([]string{
			"review", "--root", clean, "--against", "main", "--annotate", nothing,
		}, &stdout, &stderr), stderr.String())

		written, err := os.ReadFile(nothing)
		require.NoError(t, err)
		assert.Contains(t, string(written), "No change in this revision needs an explanation.")
	})
}

// TestRunReviewAgainstADirectory is its own function because it is a different
// shape of invocation: two directories rather than two revisions, with nothing
// to attribute either of them to.
func TestRunReviewAgainstADirectory(t *testing.T) {
	base := tree(t, model())
	head := tree(t, map[string]string{
		"registry.dfc":          listRegistry,
		"entities/site.dfc":     withoutTheCampus(t),
		"entities/geometry.dfc": listGeometry,
		"entities/parcels.dfc":  listParcels,
	})

	var stdout, stderr bytes.Buffer
	args := []string{"review", "--root", head, "--base-root", base}

	require.Equal(t, exitCheck, run(args, &stdout, &stderr), stderr.String())

	result := listed[reviewResult](t, stdout.String())

	require.Len(t, result.Findings, 1)
	assert.Equal(t, dfcad.ID("site:C-01"), result.Findings[0].Subject)
	assert.False(t, result.Findings[0].Commit.Named(), "a comparison of two directories has no commit to name")
	assert.Equal(t, base, result.Comparison.Base)
	assert.Empty(t, result.Comparison.Against)
}

func TestRunReviewRejectsAPolicyItCannotRead(t *testing.T) {
	testCases := []struct {
		name             string
		policy           string
		expectedFragment string
	}{
		{
			name:             "rejects a policy which is not a check and a ruling",
			policy:           "boundary-moved-without-claim",
			expectedFragment: "want <check>=<ruling>",
		},
		{
			name:             "rejects a policy about a check the engine does not run",
			policy:           "wall-moved=warning",
			expectedFragment: "unknown finding wall-moved",
		},
		{
			name:             "rejects a ruling which is not one",
			policy:           "boundary-moved-without-claim=fatal",
			expectedFragment: `unknown ruling "fatal"`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, model()))

			var stdout, stderr bytes.Buffer

			require.Equal(t, exitUsage, run([]string{"review", "--policy", testCase.policy}, &stdout, &stderr))

			assert.Empty(t, stdout.String(), "a run which answered nothing writes no result object")
			assert.Contains(t, stderr.String(), testCase.expectedFragment)
		})
	}
}

// TestRunReviewWithoutAHistory is its own function because every case in it is
// a run which could not read its second revision, which is a load failure
// rather than a review with nothing to report.
func TestRunReviewWithoutAHistory(t *testing.T) {
	t.Run("reports a model root which is not inside a repository", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git is not on the path, and this is what it tests")
		}

		root := tree(t, model())

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitLoad, run([]string{"review", "--root", root}, &stdout, &stderr))

		assert.Empty(t, stdout.String())
		assert.Contains(t, stderr.String(), "not inside a git working tree")
	})

	t.Run("reports a branch the repository does not hold", func(t *testing.T) {
		root, _ := reviewedRepository(t, map[string]string{"README.md": "Nothing to see.\n"})

		var stdout, stderr bytes.Buffer
		require.Equal(t, exitLoad, run([]string{"review", "--root", root, "--against", "origin/HEAD"}, &stdout, &stderr))

		assert.Empty(t, stdout.String())
		assert.Contains(t, stderr.String(), "origin/HEAD")
	})

	t.Run("reports a base directory which holds no model", func(t *testing.T) {
		root := tree(t, model())

		var stdout, stderr bytes.Buffer
		args := []string{"review", "--root", root, "--base-root", filepath.Join(t.TempDir(), "missing")}

		require.Equal(t, exitLoad, run(args, &stdout, &stderr))

		assert.Empty(t, stdout.String())
		assert.Contains(t, stderr.String(), "does not load")
	})
}

// TestRunReviewRefusesAShallowCheckout is its own function because it needs a
// second clone, and because what it asserts about is the message rather than
// the code: a CI job told only that its checkout is wrong cannot fix it.
func TestRunReviewRefusesAShallowCheckout(t *testing.T) {
	root, _ := reviewedRepository(t, map[string]string{"entities/site.dfc": withoutTheCampus(t)})

	shallow := filepath.Join(t.TempDir(), "checkout")
	origin, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	git(t, t.TempDir(), "clone", "--quiet", "--depth", "1", "--branch", "the-change", "file://"+origin, shallow)

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitLoad, run([]string{"review", "--root", shallow, "--against", "origin/main"}, &stdout, &stderr))

	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "fetch-depth: 0")
}
