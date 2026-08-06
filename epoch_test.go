// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDerivationEpoch(t *testing.T) {
	testCases := []struct {
		name     string
		render   func(Epoch) string
		expected string
	}{
		{
			name:     "renders an ISO 8601 date and time in UTC",
			render:   Epoch.ISO8601,
			expected: "1970-01-01T00:00:00Z",
		},
		{
			name:     "renders the time stamp of a part 21 exchange file without a zone",
			render:   Epoch.STEP,
			expected: "1970-01-01T00:00:00",
		},
		{
			name:     "renders a PDF date string",
			render:   Epoch.PDF,
			expected: "D:19700101000000Z",
		},
		{
			name:     "renders as its ISO 8601 form when it is printed",
			render:   Epoch.String,
			expected: "1970-01-01T00:00:00Z",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			digest, err := DigestOf(derivedFixture)
			require.NoError(t, err)

			assert.Equal(t, testCase.expected, testCase.render(DerivationEpoch(digest)))
		})
	}
}

func TestDerivationEpochIsAFunctionOfTheSource(t *testing.T) {
	t.Run("derives one epoch for a tree however often it is asked", func(t *testing.T) {
		digest, err := DigestOf(derivedFixture)
		require.NoError(t, err)

		assert.Equal(t, DerivationEpoch(digest), DerivationEpoch(digest))
	})

	t.Run("derives the same epoch for a tree which changed", func(t *testing.T) {
		root := copyTree(t, derivedFixture)

		before, err := DigestOf(root)
		require.NoError(t, err)

		moveOneCorner(t, root)

		after, err := DigestOf(root)
		require.NoError(t, err)
		require.NotEqual(t, before, after, "the edit reached the digest")

		assert.Equal(t, DerivationEpoch(before), DerivationEpoch(after),
			"the epoch is a constant of the source, not a value folded out of the digest")
	})

	t.Run("derives an epoch for a tree which could not be read", func(t *testing.T) {
		unknown := Digest{}
		require.False(t, unknown.Known())

		known, err := DigestOf(derivedFixture)
		require.NoError(t, err)

		assert.Equal(t, DerivationEpoch(known), DerivationEpoch(unknown),
			"an unreadable tree still has to reach its diagnostic")
	})

	t.Run("derives an epoch nobody derived", func(t *testing.T) {
		known, err := DigestOf(derivedFixture)
		require.NoError(t, err)

		assert.Equal(t, DerivationEpoch(known), Epoch{}, "the zero Epoch is the epoch")
	})

	t.Run("reads no clock", func(t *testing.T) {
		digest, err := DigestOf(derivedFixture)
		require.NoError(t, err)

		epoch := DerivationEpoch(digest)

		assert.Equal(t, int64(0), epoch.Seconds())
		assert.Equal(t, time.Unix(0, 0).UTC(), epoch.Time())
		assert.True(t, epoch.Time().Before(time.Now()), "no run of this test happened in 1970")
	})
}

func TestArtefactBytesAreIdenticalAcrossRuns(t *testing.T) {
	t.Run("writes the same bytes twice for a tree nothing changed", func(t *testing.T) {
		root := copyTree(t, derivedFixture)

		first := exportedHeader(t, root)
		second := exportedHeader(t, root)

		assert.True(t, bytes.Equal(first, second), "an export of an unchanged tree is byte-identical")
	})

	t.Run("writes the same bytes for a copy of a tree as for the tree", func(t *testing.T) {
		original := exportedHeader(t, derivedFixture)
		copied := exportedHeader(t, copyTree(t, derivedFixture))

		assert.True(t, bytes.Equal(original, copied),
			"nothing about the machine an export ran on reaches a byte of it")
	})

	t.Run("writes different bytes for a tree which changed", func(t *testing.T) {
		root := copyTree(t, derivedFixture)

		before := exportedHeader(t, root)

		moveOneCorner(t, root)

		after := exportedHeader(t, root)

		assert.False(t, bytes.Equal(before, after),
			"the provenance stamp moves with the tree even though the time stamp does not")
	})
}

// exportedHeader stands in for an exporter, which is what makes the byte
// identity above worth asserting: the two fields every target format has — a
// time stamp the format demands and the provenance 0021 requires beside it —
// written the way an exporter would write them, from a tree read off disk.
//
// It is deliberately the whole of the machinery a real exporter would use for
// these two fields, so that an exporter reaching for a clock instead of for
// [DerivationEpoch] is a difference this test can see.
func exportedHeader(t *testing.T, root string) []byte {
	t.Helper()

	digest, err := DigestOf(root)
	require.NoError(t, err)

	epoch := DerivationEpoch(digest)

	var out bytes.Buffer
	fmt.Fprintf(&out, "FILE_NAME('model','%s');\n", epoch.STEP())
	fmt.Fprintf(&out, "FILE_DESCRIPTION(('derived from %s'));\n", digest)

	return out.Bytes()
}
