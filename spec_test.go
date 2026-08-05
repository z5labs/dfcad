// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declaredSpecVersion matches the version SPEC.md states at the top of itself,
// which is the one place the specification declares which version it is.
var declaredSpecVersion = regexp.MustCompile(`(?m)^\*\*Specification version ([0-9]+\.[0-9]+)\.\*\*$`)

// TestSpecVersionIsWhatTheSpecificationDeclares reads SPEC.md and requires the
// constant to agree with it.
//
// The constant is a second copy of a fact the specification already states, and
// a second copy that drifts is worse than none: a caller which branches on it —
// or a bug report quoting `dfcad version` — would then be confidently wrong
// about which dialect of the format it is holding. Bumping the specification
// without bumping the constant fails here, which is the only moment at which the
// two are both in front of whoever is making the change.
func TestSpecVersionIsWhatTheSpecificationDeclares(t *testing.T) {
	t.Run("matches the version SPEC.md declares", func(t *testing.T) {
		specification, err := os.ReadFile("SPEC.md")
		require.NoError(t, err)

		match := declaredSpecVersion.FindSubmatch(specification)
		require.NotNil(t, match, "SPEC.md does not declare a version in the form the constant tracks")

		assert.Equal(t, string(match[1]), SpecVersion)
	})
}
