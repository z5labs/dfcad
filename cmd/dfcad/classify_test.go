// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// registryFile is the registry file of the fixture as it stands, which is where
// classify-type writes.
func registryFile(t *testing.T, root string) string {
	t.Helper()

	src, err := os.ReadFile(filepath.Join(root, "registry.dfc"))
	require.NoError(t, err)

	return string(src)
}

func TestRunClassifyType(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected []dfcad.ExternalClassification
	}{
		{
			name: "writes a system and a code onto the type's declaration",
			args: []string{"classify-type", "Campus", "IFC4", "IfcZone"},
			expected: []dfcad.ExternalClassification{
				{System: "IFC4", Code: "IfcZone"},
			},
		},
		{
			// The fixture's Parcel already carries an IFC4 class, so this is a
			// type gaining a second scheme rather than its first.
			name: "adds a scheme beside the ones the type already carries",
			args: []string{"classify-type", "Parcel", "Uniclass2015", "En_15"},
			expected: []dfcad.ExternalClassification{
				{System: "IFC4", Code: "IfcSite"},
				{System: "Uniclass2015", Code: "En_15"},
			},
		},
		{
			// Nothing here knows any scheme, so nothing here has an opinion
			// about a code which does not look like one.
			name: "writes a code no scheme would recognise, unread",
			args: []string{"classify-type", "Campus", "a system nobody registered", "not/a*code"},
			expected: []dfcad.ExternalClassification{
				{System: "a system nobody registered", Code: "not/a*code"},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, root := wrote(t, testCase.args...)

			require.Len(t, result.Files, 1)
			assert.Equal(t, "registry.dfc", filepath.Base(result.Files[0].Path))
			assert.Equal(t, dfcad.FileRewritten, result.Files[0].Status)

			// A type is declared under a name rather than an id, so the effect
			// says which type by name. A caller which read only the id would be
			// told that something of some type changed and not which.
			require.Len(t, result.Files[0].Effects, 1)
			assert.Equal(t, dfcad.OpModified, result.Files[0].Effects[0].Op)
			assert.Equal(t, "type", result.Files[0].Effects[0].Tag)
			assert.Equal(t, testCase.args[1], result.Files[0].Effects[0].Name)
			assert.Empty(t, result.Files[0].Effects[0].ID)

			registry, diags := dfcad.LoadRegistry(root)
			require.Empty(t, diags)

			declared, ok := registry.Type(testCase.args[1])
			require.True(t, ok)

			got := make([]dfcad.ExternalClassification, 0, len(declared.Classifications))
			for _, classification := range declared.Classifications {
				got = append(got, dfcad.ExternalClassification{
					System: classification.System,
					Code:   classification.Code,
				})
			}

			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestRunClassifyTypeIsRefused is its own function because its assertions are
// the other shape: nothing is written, and the thing worth reading is what was
// said on stderr.
func TestRunClassifyTypeIsRefused(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "names no type at all",
			args:     []string{"classify-type"},
			expected: "expected the type to classify",
		},
		{
			name:     "stops before the pair is written",
			args:     []string{"classify-type", "Campus", "IFC4"},
			expected: "found fewer than two arguments",
		},
		{
			name:     "writes more than the three arguments it takes",
			args:     []string{"classify-type", "Campus", "IFC4", "IfcZone", "IfcSite"},
			expected: "unexpected 1 argument: IfcSite",
		},
		{
			name:     "names a type no registry file declares",
			args:     []string{"classify-type", "Campsu", "IFC4", "IfcZone"},
			expected: "which no registry file declares",
		},
		{
			name:     "names a system the type is already classified in",
			args:     []string{"classify-type", "Parcel", "IFC4", "IfcZone"},
			expected: `already "IfcSite"`,
		},
		{
			name:     "leaves the code blank",
			args:     []string{"classify-type", "Campus", "IFC4", ""},
			expected: "found no code",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Contains(t, refusal(t, testCase.args...), testCase.expected)
		})
	}
}

// TestClassifyTypeWritesNothingOnADryRun checks the flag every write command
// takes does what it says here too: the change is described and the registry is
// left exactly as it was.
func TestClassifyTypeWritesNothingOnADryRun(t *testing.T) {
	root := tree(t, authored())
	before := registryFile(t, root)

	stdout, _ := invoke(t, exitSuccess, root, "classify-type", "--dry-run", "Campus", "IFC4", "IfcZone")

	result := listed[writeResult](t, stdout)

	assert.True(t, result.DryRun)
	require.Len(t, result.Files, 1)
	assert.Contains(t, result.Files[0].Diff, `+  (classification "IFC4" "IfcZone")`)

	assert.Equal(t, before, registryFile(t, root), "a dry run writes nothing")
}
