// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sexpr "github.com/z5labs/sexpr-go"
)

func TestTxClassify(t *testing.T) {
	testCases := []struct {
		name            string
		classifications []ExternalClassification
		expected        []ExternalClassification
	}{
		{
			name:            "writes a system and a code onto a declared type",
			classifications: []ExternalClassification{{System: "IFC4", Code: "IfcZone"}},
			expected:        []ExternalClassification{{System: "IFC4", Code: "IfcZone"}},
		},
		{
			name: "carries a type's several schemes at once",
			classifications: []ExternalClassification{
				{System: "Uniclass2015", Code: "En_90_10"},
				{System: "IFC4", Code: "IfcZone"},
				{System: "OmniClass", Code: "13-11 00 00"},
			},
			// Canonical form sorts repeated children by their inline
			// rendering, so what is read back is in that order rather than in
			// the order the changes were made.
			expected: []ExternalClassification{
				{System: "IFC4", Code: "IfcZone"},
				{System: "OmniClass", Code: "13-11 00 00"},
				{System: "Uniclass2015", Code: "En_90_10"},
			},
		},
		{
			name: "keeps a code neither half of which the engine reads",
			classifications: []ExternalClassification{
				{System: "  a system nobody registered  ", Code: "not/a*code (any scheme would recognise)"},
			},
			expected: []ExternalClassification{
				{System: "  a system nobody registered  ", Code: "not/a*code (any scheme would recognise)"},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := writeFixture(t)
			tx := begin(t, root)

			for _, classification := range testCase.classifications {
				require.NoError(t, tx.Classify("Campus", classification))
			}

			commit(t, tx)

			registry, diags := LoadRegistry(root)
			require.Empty(t, diags)

			declared, ok := registry.Type("Campus")
			require.True(t, ok)

			got := make([]ExternalClassification, 0, len(declared.Classifications))
			for _, classification := range declared.Classifications {
				got = append(got, ExternalClassification{
					System: classification.System,
					Code:   classification.Code,
				})
			}

			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestTxClassifyRefuses is its own function because its assertions are the
// other shape: nothing is written, and what is worth asserting on is the error
// rather than the model which came back.
func TestTxClassifyRefuses(t *testing.T) {
	t.Run("names the declared types when the type is not one", func(t *testing.T) {
		tx := begin(t, writeFixture(t))

		err := tx.Classify("Campsu", ExternalClassification{System: "IFC4", Code: "IfcZone"})

		var undeclared UndeclaredTypeError
		require.ErrorAs(t, err, &undeclared)
		assert.Equal(t, "Campsu", undeclared.Type)
		assert.Equal(t, []string{"Campus"}, undeclared.Declared)
	})

	t.Run("names the code a type already carries in that system", func(t *testing.T) {
		tx := begin(t, writeFixture(t))

		require.NoError(t, tx.Classify("Campus", ExternalClassification{System: "IFC4", Code: "IfcZone"}))

		err := tx.Classify("Campus", ExternalClassification{System: "IFC4", Code: "IfcSite"})

		var already AlreadyClassifiedError
		require.ErrorAs(t, err, &already)
		assert.Equal(t, "Campus", already.Type)
		assert.Equal(t, "IFC4", already.System)
		assert.Equal(t, "IfcZone", already.Code)
	})

	t.Run("names each half of the pair which was left blank", func(t *testing.T) {
		testCases := []struct {
			name           string
			classification ExternalClassification
			missing        []string
		}{
			{
				name:           "no system",
				classification: ExternalClassification{Code: "IfcZone"},
				missing:        []string{"system"},
			},
			{
				name:           "no code",
				classification: ExternalClassification{System: "IFC4"},
				missing:        []string{"code"},
			},
			{
				name:           "neither",
				classification: ExternalClassification{},
				missing:        []string{"system", "code"},
			},
		}

		for _, testCase := range testCases {
			t.Run(testCase.name, func(t *testing.T) {
				tx := begin(t, writeFixture(t))

				err := tx.Classify("Campus", testCase.classification)

				var incomplete IncompleteClassificationError
				require.ErrorAs(t, err, &incomplete)
				assert.Equal(t, testCase.missing, incomplete.Missing)
			})
		}
	})

	t.Run("refuses a transaction which has already committed", func(t *testing.T) {
		tx := begin(t, writeFixture(t))
		commit(t, tx)

		err := tx.Classify("Campus", ExternalClassification{System: "IFC4", Code: "IfcZone"})

		assert.ErrorIs(t, err, ErrFinished)
	})
}

// TestClassifyingLeavesTheRestOfTheFileAlone is the property the versioning
// rule of specification section 10 rests on for this child.
//
// Adding an optional child is MINOR only while the bytes `fmt` produces for a
// file which does not write it are unchanged; a change to those is MAJOR,
// because it turns every model's next format run into a whole-tree diff. The
// declarations either side of the one which gained a classification are what
// says so here — they are printed by the same pass, and a printer which had
// started spelling a type entry differently would show up in them.
func TestClassifyingLeavesTheRestOfTheFileAlone(t *testing.T) {
	root := writeFixture(t)

	registryPath := filepath.Join(root, "registry.dfc")

	// The fixture is put into canonical form first, so that what is compared
	// afterwards is the effect of the change rather than the effect of the
	// commit having printed a hand-written file.
	canonicalised(t, registryPath)
	before := printings(t, registryPath)

	tx := begin(t, root)
	require.NoError(t, tx.Classify("Campus", ExternalClassification{System: "IFC4", Code: "IfcZone"}))
	commit(t, tx)

	after := printings(t, registryPath)

	require.Equal(t, len(before), len(after), "the change adds no form and removes none")

	for tag, printed := range before {
		if tag == "type Campus" {
			// One line added inside the form, and the closing parenthesis the
			// declaration already ended on moved onto it.
			assert.Equal(t, strings.TrimSuffix(printed, ")")+"\n  (classification \"IFC4\" \"IfcZone\"))",
				after[tag],
				"the classification is the whole of what changed in the declaration it was written on")
			continue
		}

		assert.Equal(t, printed, after[tag],
			"the %s form prints exactly as it did before the format gained the child", tag)
	}
}

// printings is each top-level form of a file, canonically printed on its own,
// by the tag and positional name it was written with.
//
// Form by form rather than the whole file, because the two questions are
// different: whether a form which writes no classification still prints the same
// bytes is the one specification section 10 makes MAJOR, and comparing whole
// files would answer it with a diff that also holds the line the change added.
func printings(t *testing.T, path string) map[string]string {
	t.Helper()

	src, err := os.ReadFile(path)
	require.NoError(t, err)

	file, err := Parse(path, strings.NewReader(string(src)))
	require.NoError(t, err)

	out := make(map[string]string, len(file.Nodes))
	for _, node := range file.Nodes {
		tag, ok := formTag(node)
		require.True(t, ok)

		if arg, ok := argument(node, 0); ok {
			if symbol, ok := arg.Datum.(sexpr.Symbol); ok {
				tag += " " + symbol.Value
			}
		}

		var printed strings.Builder
		require.NoError(t, Print(&printed, &File{Path: path, Nodes: []*Node{node}}))

		out[tag] = strings.TrimSuffix(printed.String(), "\n")
	}

	return out
}

// canonicalised rewrites the file at path in canonical form and returns what it
// now holds.
func canonicalised(t *testing.T, path string) string {
	t.Helper()

	src, err := os.ReadFile(path)
	require.NoError(t, err)

	file, err := Parse(path, strings.NewReader(string(src)))
	require.NoError(t, err)

	var printed strings.Builder
	require.NoError(t, Print(&printed, file))
	require.NoError(t, os.WriteFile(path, []byte(printed.String()), 0o644))

	return printed.String()
}

// TestClassificationRoundTrips is the property the format is judged by rather
// than an expected string: what a classification prints as has to read back as
// the same pair, whatever is in either half of it.
func TestClassificationRoundTrips(t *testing.T) {
	testCases := []struct {
		name           string
		classification ExternalClassification
	}{
		{
			name:           "an ordinary pair",
			classification: ExternalClassification{System: "IFC4", Code: "IfcZone"},
		},
		{
			name:           "a code carrying the characters a string escapes",
			classification: ExternalClassification{System: `a "quoted" system`, Code: "a\ttab\\and a \\ backslash"},
		},
		{
			name:           "a code which is not ASCII",
			classification: ExternalClassification{System: "Système", Code: "Bâtiment—90.10"},
		},
		{
			name:           "a code which would read back as a number were it not a string",
			classification: ExternalClassification{System: "OmniClass", Code: "23"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := writeFixture(t)

			tx := begin(t, root)
			require.NoError(t, tx.Classify("Campus", testCase.classification))
			commit(t, tx)

			first, diags := LoadRegistry(root)
			require.Empty(t, diags)

			// Print what was read and read it again. An expected output string
			// can pass while that output no longer reads back, which is the
			// failure this shape of test exists to catch.
			require.NotEmpty(t, canonicalised(t, filepath.Join(root, "registry.dfc")))

			second, diags := LoadRegistry(root)
			require.Empty(t, diags)

			for _, registry := range []*Registry{first, second} {
				declared, ok := registry.Type("Campus")
				require.True(t, ok)
				require.Len(t, declared.Classifications, 1)

				assert.Equal(t, testCase.classification.System, declared.Classifications[0].System)
				assert.Equal(t, testCase.classification.Code, declared.Classifications[0].Code)
			}
		})
	}
}

// TestClassifyingIsRefusedInWordsACallerCanAssertOn checks the errors carry
// what made them rather than only a sentence, which is what a caller branches
// on.
func TestClassifyingIsRefusedInWordsACallerCanAssertOn(t *testing.T) {
	var undeclared UndeclaredTypeError
	require.True(t, errors.As(UndeclaredTypeError{Type: "Campsu"}, &undeclared))
	assert.Contains(t, undeclared.Error(), "this model declares no type at all",
		"a model with no vocabulary is a different answer from a misspelling")
}
