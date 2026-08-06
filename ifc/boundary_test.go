// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package ifc

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// module is the import path prefix of the repository this package happens to
// live in.
const module = "github.com/z5labs/dfcad"

// TestThePackageImportsNothingOfThisModule is the boundary, enforced rather
// than intended.
//
// This package is a file format library which the engine beside it is the
// first caller of, and the test of whether that is real is whether it could be
// moved to a repository of its own with no edit to its source. That is cheap
// to keep true and expensive to recover once it is not, so it is checked here
// rather than reviewed.
//
// The check is over the package's own source rather than over its tests. A
// test may reach for an assertion library, and a test which reached for the
// engine would be a different failure — it would not travel with the package,
// but it would not be shipped with it either.
func TestThePackageImportsNothingOfThisModule(t *testing.T) {
	for path, imports := range sources(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			for _, imported := range imports {
				assert.NotEqual(t, module, imported)
				assert.False(t, strings.HasPrefix(imported, module+"/"),
					"%s imports %s, which is a package of the module this one has to be able to leave",
					path, imported)
			}
		})
	}
}

// TestThePackageImportsOnlyTheStandardLibrary is the same boundary, one step
// stronger, and it is the one which actually holds.
//
// Importing no package of this module is not enough on its own: a third party
// dependency which itself imported the engine would satisfy the test above and
// break the property it is protecting. Every import here resolves inside the
// standard library, whose first path element never carries a dot, so the
// transitive set is closed by inspection rather than by walking it.
func TestThePackageImportsOnlyTheStandardLibrary(t *testing.T) {
	for path, imports := range sources(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			for _, imported := range imports {
				domain, _, _ := strings.Cut(imported, "/")
				assert.NotContains(t, domain, ".",
					"%s imports %s, which is not in the standard library", path, imported)
			}
		})
	}
}

// sources is every non-test file of this package, with the paths it imports.
func sources(t *testing.T) map[string][]string {
	t.Helper()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	files := make(map[string][]string)

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		require.NoError(t, err)

		imports := make([]string, 0, len(parsed.Imports))
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			require.NoError(t, err)
			imports = append(imports, path)
		}

		files[name] = imports
	}

	require.NotEmpty(t, files, "the package has source files to check")

	return files
}
