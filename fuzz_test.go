// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedFuzz adds every fixture of the corpus to a fuzz target's seed corpus,
// along with the handful of inputs which are shorter to write than to find.
//
// The seeds matter more than they look. A fuzzer starting from nothing spends
// its budget discovering that a file is parenthesised; one starting from a
// loadable model spends it on what happens when a loadable model is mutated,
// which is the input this package will actually be handed. The invalid corpus
// is seeded too, because a file somebody is part way through fixing is exactly
// the file fmt is run on.
func seedFuzz(f *testing.F) {
	f.Helper()

	for _, pattern := range []string{
		filepath.Join(validCorpus, "*"),
		filepath.Join(invalidCorpus, "*"+Extension),
		filepath.Join("testdata", "print", "*"),
		filepath.Join("testdata", "validate", "*"+Extension),
	} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			f.Fatal(err)
		}

		for _, path := range paths {
			src, err := os.ReadFile(path)
			if err != nil {
				f.Fatal(err)
			}
			f.Add(src)
		}
	}

	for _, src := range []string{
		"",
		"()",
		"(",
		")",
		"nil",
		"'a",
		"(a . b)",
		"#|",
		`"`,
		`"\q"`,
		"12abc",
		"\xff",
		"\xef\xbb\xbf(a)",
		"(a\r\n  b)\r\n",
		"; comment with no form after it\n",
	} {
		f.Add([]byte(src))
	}
}

// FuzzParse checks that loading arbitrary bytes reports rather than crashes.
//
// The parse layer is the one every other layer treats as ground truth, and the
// input it is handed is a file somebody wrote — which is to say, a file which
// may be halfway through an edit, truncated by a crashed editor, or not text at
// all. A panic there is not a diagnostic anybody can act on: it takes down the
// process which was going to report the twenty other things wrong with the tree.
//
// What is asserted is only that every step terminates and returns. Which
// diagnostics arbitrary bytes deserve is not a question with an answer; that a
// file either loads or is reported is.
func FuzzParse(f *testing.F) {
	seedFuzz(f)

	f.Fuzz(func(t *testing.T, src []byte) {
		file, err := parse("fuzz.dfc", src)
		if err != nil {
			// A failure is a diagnostic somebody can be shown, which is the
			// other half of not crashing.
			require.NotEmpty(t, diagnose("fuzz.dfc", err).Message)
			return
		}

		require.NotNil(t, file)

		// Everything the loaded tree is then handed to has to survive it too. A
		// tree which parsed is a tree fmt will print and check will validate.
		Validate(file)

		var out strings.Builder
		require.NoError(t, Print(&out, file))
	})
}

// FuzzPrintRoundTrip checks that whatever loads survives being printed.
//
// This is the property the whole of the format rests on, checked against input
// nobody chose: canonical output parses, it parses back to the tree it was
// printed from, and printing it again produces the same bytes. A printer which
// lost a digit, escaped a string so that it read back as different text, or
// swallowed the datum after a comment would produce stable, plausible, wrong
// bytes — and nothing which compared output against a recorded string would
// notice.
func FuzzPrintRoundTrip(f *testing.F) {
	seedFuzz(f)

	f.Fuzz(func(t *testing.T, src []byte) {
		file, err := parse("fuzz.dfc", src)
		if err != nil {
			return
		}

		var once strings.Builder
		require.NoError(t, Print(&once, file))

		read, err := parse("fuzz.dfc", []byte(once.String()))
		require.NoError(t, err, "canonical output parses")

		var twice strings.Builder
		require.NoError(t, Print(&twice, read))

		assert.Equal(t, once.String(), twice.String(), "canonical form is a fixed point")
		assert.Equal(t, unpositioned(canonical(file)), unpositioned(treeOf(read)), "the tree survives the round trip")
	})
}
