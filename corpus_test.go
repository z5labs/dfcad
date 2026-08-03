// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sexpr "github.com/z5labs/sexpr-go"
)

// The fixture corpus is two directories, and the split between them is what
// each one is for rather than where it came from.
//
// testdata/corpus/valid holds one file per construct of the specification,
// plus one combined model which is what a real one looks like. Every file in
// it loads and validates clean, and its canonical printing is recorded beside
// it as a golden .want file, so a change to the printer arrives as a diff over
// every construct at once rather than as a number in a Go literal.
//
// testdata/corpus/invalid holds one file per error case, each with the
// diagnostic it produces recorded beside it as a golden .txt file. The cases
// here are the ones about a file and its lexis — the encoding, the delegated
// grammar, and the constructs specification section 2 excludes. The cases about
// the shape of a form live in testdata/validate, which is where the validator's
// own tests read them from; testdata/README.md maps every stated error case to
// whichever of the two holds it.
const (
	validCorpus   = "testdata/corpus/valid"
	invalidCorpus = "testdata/corpus/invalid"
)

// fixtures lists the entity files of one corpus directory, in lexical order.
func fixtures(t *testing.T, dir string) []string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(dir, "*"+Extension))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "%s holds fixtures", dir)

	return paths
}

// loaded parses one fixture, failing the test where it does not parse.
func loaded(t *testing.T, path string) (*File, []byte) {
	t.Helper()

	src, err := os.ReadFile(path)
	require.NoError(t, err)

	file, err := Parse(path, bytes.NewReader(src))
	require.NoError(t, err)

	return file, src
}

// TestCorpusLoads checks that every fixture of the valid corpus is in fact
// valid: it parses, and the validator finds nothing wrong with it.
//
// It is the assertion the rest of the corpus rests on. A fixture which was
// quietly malformed would still round-trip and still print idempotently — the
// printer writes back what it does not recognise — so every property below it
// would pass while the corpus covered nothing.
func TestCorpusLoads(t *testing.T) {
	for _, path := range fixtures(t, validCorpus) {
		t.Run(path, func(t *testing.T) {
			file, _ := loaded(t, path)

			for _, diagnostic := range Validate(file) {
				t.Errorf("unexpected diagnostic: %s", diagnostic)
			}
		})
	}
}

// TestCorpusCoversEveryForm checks that the valid corpus writes every form of
// the format at least once.
//
// The tables in forms.go are the format as this package implements it, so
// walking them is what makes "covers every construct" a fact rather than a
// claim in a comment: a form added to a table with no fixture writing it fails
// here, and the fix is a fixture rather than an exception.
func TestCorpusCoversEveryForm(t *testing.T) {
	written := make(map[string]bool)
	for _, path := range fixtures(t, validCorpus) {
		file, _ := loaded(t, path)
		for _, node := range file.Nodes {
			collectTags(node, written)
		}
	}

	seen := make(map[*form]bool)

	var walk func(f *form)
	walk = func(f *form) {
		if f == nil || seen[f] {
			return
		}
		seen[f] = true

		for _, c := range f.children {
			assert.True(t, written[c.tag], "a fixture of the corpus writes the (%s ...) form", c.tag)
			walk(c.form)
		}
		walk(f.claims)
	}
	walk(topLevelForm)
}

// TestCorpusPrintsCanonically compares every valid fixture against its recorded
// canonical printing.
//
// The fixtures are written the way somebody would write them rather than in
// canonical form already, so the golden beside each one is what canonicalising
// that construct does: the child order, the sorting, the defaults left out and
// the numbers respelled, all readable as an entity file.
func TestCorpusPrintsCanonically(t *testing.T) {
	for _, path := range fixtures(t, validCorpus) {
		t.Run(path, func(t *testing.T) {
			file, _ := loaded(t, path)

			var out strings.Builder
			require.NoError(t, Print(&out, file))

			assert.Equal(t, recorded(t, path, ".want", out.String()), out.String())
		})
	}
}

// TestCorpusRejectsEveryInvalidFixture checks that every fixture of the invalid
// corpus is reported, and reported the way its golden says.
//
// The two halves are one test on purpose. A golden which was empty would record
// that the format accepts something the specification says it rejects, and it
// would go on passing for as long as nobody read it; requiring the rendering to
// be non-empty is what stops an accepted fixture from looking like a covered
// error case.
func TestCorpusRejectsEveryInvalidFixture(t *testing.T) {
	for _, path := range fixtures(t, invalidCorpus) {
		t.Run(path, func(t *testing.T) {
			got := reported(t, path)

			require.NotEmpty(t, got, "the fixture is reported rather than accepted")
			assert.Equal(t, recorded(t, path, ".txt", got), got)
		})
	}
}

// reported is everything a fixture is reported with, rendered the way the
// command line interface renders it.
//
// A file has one or the other and never both: a file which does not parse has
// no tree to validate, and the failure that stopped it is a load error rather
// than a diagnostic until [diagnose] places it. Rendering the two the same way
// is what lets one golden format hold both.
func reported(t *testing.T, path string) string {
	t.Helper()

	src, err := os.ReadFile(path)
	require.NoError(t, err)

	var diagnostics Diagnostics
	switch file, err := parse(path, src); {
	case err != nil:
		diagnostics.Add(diagnose(path, err))
	default:
		diagnostics.Add(Validate(file)...)
	}

	var rendered strings.Builder
	require.NoError(t, diagnostics.Render(&rendered, Sources{path: src}))

	return rendered.String()
}

// recorded returns the golden held beside a fixture, having first rewritten it
// from got when -update was passed.
func recorded(t *testing.T, path, extension, got string) string {
	t.Helper()

	golden := strings.TrimSuffix(path, Extension) + extension
	if *updateGolden {
		require.NoError(t, os.WriteFile(golden, []byte(got), 0o644))
	}

	want, err := os.ReadFile(golden)
	require.NoError(t, err)

	return string(want)
}

// TestNestingBound is the one stated limit which is checked here rather than
// as a fixture: a file nested past the bound is twenty thousand parentheses,
// which is a fixture nobody can read and a golden nobody can review.
//
// The bound itself is delegated. What is worth pinning here is that reaching it
// is a diagnostic with a position rather than a stack exhaustion — a limit which
// took the process down with it would be no limit at all, and it would take the
// report of everything else wrong with the tree down too.
func TestNestingBound(t *testing.T) {
	testCases := []struct {
		name     string
		depth    int
		reported bool
	}{
		{
			name:     "loads a file nested as deeply as the bound permits",
			depth:    sexpr.MaxDepth,
			reported: false,
		},
		{
			name:     "reports a file nested one past the bound rather than exhausting the stack",
			depth:    sexpr.MaxDepth + 1,
			reported: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			src := strings.Repeat("(", testCase.depth) + "a" + strings.Repeat(")", testCase.depth)

			file, err := Parse("nesting.dfc", strings.NewReader(src))

			if !testCase.reported {
				require.NoError(t, err)
				require.NotNil(t, file)
				return
			}

			var got sexpr.MaxDepthExceededError

			require.ErrorAs(t, err, &got)

			var placed ParseError

			require.ErrorAs(t, err, &placed)
			assert.Equal(t, "nesting.dfc", placed.Position.Path)
			assert.Positive(t, placed.Position.Line)
		})
	}
}

// TestCorpusGoldensAreComplete checks that every fixture has the golden its
// directory records, and that no golden is left behind by a fixture which was
// renamed or removed.
//
// An orphaned golden is not harmless. It is a recording of behaviour nothing
// runs, which reads in review as coverage and is not.
func TestCorpusGoldensAreComplete(t *testing.T) {
	testCases := []struct {
		name      string
		dir       string
		extension string
	}{
		{
			name:      "records the canonical printing of every valid fixture",
			dir:       validCorpus,
			extension: ".want",
		},
		{
			name:      "records the diagnostic of every invalid fixture",
			dir:       invalidCorpus,
			extension: ".txt",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			want := make(map[string]bool)
			for _, path := range fixtures(t, testCase.dir) {
				golden := strings.TrimSuffix(path, Extension) + testCase.extension
				want[golden] = true

				assert.FileExists(t, golden, "%s has a golden", path)
			}

			found, err := filepath.Glob(filepath.Join(testCase.dir, "*"+testCase.extension))
			require.NoError(t, err)

			for _, golden := range found {
				assert.True(t, want[golden], "%s belongs to a fixture", golden)
			}
		})
	}
}
