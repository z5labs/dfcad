// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sexpr "github.com/z5labs/sexpr-go"
)

// pinnedURL is the URL testdata/model pins, and the one every expected value
// below was computed under.
//
// Every literal in this file was produced by implementations outside this
// package: the UUIDs by Python's uuid module, which is a direct reading of RFC
// 4122, and the 22-character encodings by IfcOpenShell's `ifcopenshell.guid`,
// which is what the IFC tooling everyone else uses encodes with. That is what
// makes them worth asserting on — a literal recorded from this package's own
// output would pin nothing but this package's own arithmetic, and would agree
// with itself just as happily while agreeing with no IFC reader anywhere.
const pinnedURL = "https://example.org/models/riverside"

func TestDeriveGlobalID(t *testing.T) {
	testCases := []struct {
		name string
		url  string
		id   ID
		want GlobalID
	}{
		{
			name: "derives the same 22 characters IfcOpenShell derives",
			url:  pinnedURL,
			id:   "site:S-101",
			want: "2GX9NtsjvT$PykCkbFuEnE",
		},
		{
			name: "derives a different value for a neighbouring id",
			url:  pinnedURL,
			id:   "site:S-102",
			want: "0PY5ytv0nHaB1Jwii$MSoq",
		},
		{
			name: "derives over the whole id, colons in the local part included",
			url:  pinnedURL,
			id:   "survey:2026:CP-3",
			want: "2XpoyhyDXKmxPzd8LNCH4Y",
		},
		{
			name: "derives for a geometric node, which is an id like any other",
			url:  pinnedURL,
			id:   "geom:v-1",
			want: "2N8Me4f59T6A2iOtNg6E1E",
		},
		{
			name: "derives for a frame",
			url:  pinnedURL,
			id:   "frame:site-grid",
			want: "0FsX_0u1DKewfKXr4uD6T$",
		},
		{
			name: "distinguishes ids which differ only in case, because an id compares byte for byte",
			url:  pinnedURL,
			id:   "site:s-101",
			want: "0qXiMTqBTTdx4qRBLiqFED",
		},
		{
			name: "derives a wholly different value under a URL differing by a trailing slash",
			url:  pinnedURL + "/",
			id:   "site:S-101",
			want: "11bwa2cbPQBBB0jmm5Je1o",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := DeriveGlobalID(testCase.url, testCase.id)

			assert.Equal(t, testCase.want, got)
			assert.Len(t, got.String(), globalIDLength)
		})
	}
}

// TestDeriveGlobalIDNamespace checks the first half of the derivation on its
// own, which is what anybody reading the registry can reproduce from the URL
// recorded there.
//
// The last two cases are the vectors Python's uuid module documents, which are
// the ones reproduced wherever a UUIDv5 implementation states what it agrees
// with, so a value computed here is checkable against an implementation which
// never saw this package.
func TestDeriveGlobalIDNamespace(t *testing.T) {
	testCases := []struct {
		name      string
		namespace uuid
		id        string
		want      string
	}{
		{
			name:      "derives the project namespace from the pinned URL",
			namespace: uuidURLNamespace,
			id:        pinnedURL,
			want:      "bf22703b-ecd8-5c1f-929c-021883f35524",
		},
		{
			name:      "derives the URL namespace vector every UUIDv5 implementation agrees on",
			namespace: uuidURLNamespace,
			id:        "http://python.org/",
			want:      "4c565f0d-3f5a-5890-b41b-20cf47701c5e",
		},
		{
			name: "derives the DNS namespace vector under the other namespace RFC 4122 fixes",
			namespace: uuid{
				0x6b, 0xa7, 0xb8, 0x10, 0x9d, 0xad, 0x11, 0xd1,
				0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8,
			},
			id:   "python.org",
			want: "886313e1-3b8a-5372-9b90-0c9aee199e5d",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := uuidV5(testCase.namespace, testCase.id)

			assert.Equal(t, testCase.want, got.String())

			// The version and the variant are what make it a version 5 UUID
			// rather than sixteen bytes of a hash.
			assert.Equal(t, byte(0x50), got[6]&0xf0)
			assert.Equal(t, byte(0x80), got[8]&0xc0)
		})
	}

	assert.Equal(t,
		"bf22703b-ecd8-5c1f-929c-021883f35524",
		DeriveGlobalIDNamespace(pinnedURL),
		"the exported derivation is the same one, so the registry's URL can be checked without exporting anything",
	)
}

// TestGlobalIDEncoding checks the 22-character encoding against values encoded
// elsewhere, on UUIDs which did not come from a derivation.
//
// It is its own function because it asserts on the other half of the pipeline:
// a hash is not involved, and a mistake in the alphabet or in IFC's grouping
// would be invisible in a test which only ever compared two derivations to each
// other.
func TestGlobalIDEncoding(t *testing.T) {
	testCases := []struct {
		name string
		uuid string
		want GlobalID
	}{
		{
			name: "encodes a real IFC GlobalId back to the characters it was published as",
			uuid: "a16bfc45-7156-4558-b57c-544102ce43fb",
			want: "2XQ$n5SLP5MBLyL442paFx",
		},
		{
			name: "encodes the smallest value as twenty-two zeroes",
			uuid: "00000000-0000-0000-0000-000000000000",
			want: "0000000000000000000000",
		},
		{
			name: "encodes the largest value, which uses the last character of the alphabet",
			uuid: "ffffffff-ffff-ffff-ffff-ffffffffffff",
			want: "3$$$$$$$$$$$$$$$$$$$$$",
		},
		{
			name: "encodes a value covering every group boundary",
			uuid: "01234567-89ab-cdef-0123-456789abcdef",
			want: "018qLdYQlDxm4ZHMU9gytl",
		},
		{
			name: "encodes a version 5 UUID minted outside this package",
			uuid: "886313e1-3b8a-5372-9b90-0c9aee199e5d",
			want: "28OnFXEufJSfkG39hk6PvT",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := parseUUID(t, testCase.uuid).globalID()

			require.Len(t, got.String(), globalIDLength)
			assert.Equal(t, testCase.want, got)

			// Every character is one IFC reads, and the leading one is never
			// above 3: two characters hold twelve bits where the first byte
			// needs eight.
			assert.Empty(t, strings.Trim(got.String(), globalIDAlphabet))
			assert.LessOrEqual(t, got.String()[0], byte('3'))
		})
	}
}

// TestDeriveGlobalIDIsPure is the property the whole arrangement rests on: the
// derivation reads its two arguments and nothing else.
//
// Repeated calls agreeing is the weaker half and is asserted here; the literals
// in the tables above are the stronger half, because a value which depended on
// a clock, a random source, an environment variable or a map iteration would
// have to match a string recorded on another machine, in another process, under
// another implementation, to go on passing.
func TestDeriveGlobalIDIsPure(t *testing.T) {
	const id ID = "site:S-101"

	first := DeriveGlobalID(pinnedURL, id)

	for range 64 {
		assert.Equal(t, first, DeriveGlobalID(pinnedURL, id))
	}

	// And the derivation holds no state between two ids either: interleaving
	// them changes neither.
	other := DeriveGlobalID(pinnedURL, "site:S-102")

	assert.NotEqual(t, first, other)
	assert.Equal(t, first, DeriveGlobalID(pinnedURL, id))
	assert.Equal(t, other, DeriveGlobalID(pinnedURL, "site:S-102"))
}

// TestGlobalIDIgnoresTheLabel is the half of
// [0002](docs/decisions/0002-immutable-id-mutable-label.md) this derivation
// depends on, asserted end to end rather than by inspection of the signature.
//
// A label is display text and is free to change. If a rename moved a GlobalID,
// every downstream system holding the old one would see the object deleted and
// a new one created, which is exactly the breakage deriving from the id exists
// to avoid.
func TestGlobalIDIgnoresTheLabel(t *testing.T) {
	registry, _ := LoadRegistry("testdata/model")

	source, err := os.ReadFile("testdata/model/entities/level-1.dfc")
	require.NoError(t, err)

	before, beforeGlobalID := renamed(t, registry, string(source), "Meeting Room B")
	after, afterGlobalID := renamed(t, registry, string(source), "Board Room")

	require.NotEqual(t, before.Label(), after.Label(), "the two loads differ in the label")
	assert.Equal(t, before.ID(), after.ID())
	assert.Equal(t, beforeGlobalID, afterGlobalID)
	assert.Equal(t, GlobalID("2GX9NtsjvT$PykCkbFuEnE"), afterGlobalID)
}

// renamed loads one entity source with its label replaced, and reports the node
// it came back as together with the GlobalID derived for it.
func renamed(t *testing.T, registry *Registry, source, label string) (*SemanticNode, GlobalID) {
	t.Helper()

	root := t.TempDir()
	path := filepath.Join(root, "level-1"+Extension)

	rewritten := strings.Replace(source, `(label "Meeting Room B")`, `(label "`+label+`")`, 1)
	require.Contains(t, rewritten, `(label "`+label+`")`, "the label the fixture writes is the one replaced")
	require.NoError(t, os.WriteFile(path, []byte(rewritten), 0o600))

	nodes, _ := LoadNodes(root, registry)

	node, ok := nodes.Node("site:S-101")
	require.True(t, ok)

	globalID, ok := registry.GlobalID(node.ID())
	require.True(t, ok)

	return node, globalID
}

// TestGlobalIDsDoNotCollideAcrossTheCorpus derives a GlobalID for every id
// written anywhere in the fixture corpus and checks that no two distinct ids
// arrive at the same one.
//
// Uniqueness of a GlobalID follows from uniqueness of the id, so what this
// guards is the derivation rather than the model: a truncated hash, a group
// written twice, or an encoding which dropped the low bits would collapse
// distinct ids onto one identifier, and every one of those bugs still produces
// 22 plausible characters per id.
func TestGlobalIDsDoNotCollideAcrossTheCorpus(t *testing.T) {
	derived := make(map[GlobalID]ID)

	for _, id := range corpusIDs(t) {
		globalID := DeriveGlobalID(pinnedURL, id)

		if first, seen := derived[globalID]; seen {
			t.Errorf("%s and %s both derive %s", first, id, globalID)
			continue
		}
		derived[globalID] = id
	}

	// The scan itself has to have found something. A corpus walk which quietly
	// matched nothing would collide with nothing either, and would pass.
	require.Greater(t, len(derived), 20, "the corpus writes enough ids for the check to mean anything")
}

// corpusIDs is every distinct id written anywhere under testdata, in the order
// the walk found them.
//
// Files which do not parse are skipped rather than failed on: the invalid
// corpus holds files which are meant not to, and every symbol of the ones which
// do is offered to [ParseID], so an id in a fixture nobody thought about here
// is still covered.
func corpusIDs(t *testing.T) []ID {
	t.Helper()

	seen := make(map[ID]bool)

	var ids []ID
	require.NoError(t, filepath.WalkDir("testdata", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != Extension {
			return err
		}

		src, err := os.ReadFile(path)
		require.NoError(t, err)

		file, err := parse(path, src)
		if err != nil {
			return nil
		}

		for _, node := range file.Nodes {
			collectIDs(node, seen, &ids)
		}
		return nil
	}))

	return ids
}

// collectIDs adds every symbol of a tree which is a well-formed id.
func collectIDs(node *Node, seen map[ID]bool, into *[]ID) {
	if symbol, ok := node.Datum.(sexpr.Symbol); ok {
		if id, err := ParseID(symbol.Value); err == nil && !seen[id] {
			seen[id] = true
			*into = append(*into, id)
		}
	}

	for _, child := range node.Children {
		collectIDs(child, seen, into)
	}
}

// TestGlobalIDWithoutAProject checks that a registry with no project
// declaration answers rather than derives.
//
// A model without one is a load error, so this is the registry a caller who did
// not read the diagnostics is holding. Deriving from the empty URL would hand
// that caller 22 characters which look like every other GlobalID and identify a
// model nobody pinned.
func TestGlobalIDWithoutAProject(t *testing.T) {
	registry, _ := LoadRegistry("testdata/registry/empty")

	globalID, ok := registry.GlobalID("site:S-101")

	assert.False(t, ok)
	assert.Empty(t, globalID)

	// A registry nothing loaded at all answers the same way rather than
	// panicking, as every other query on one does.
	globalID, ok = (*Registry)(nil).GlobalID("site:S-101")

	assert.False(t, ok)
	assert.Empty(t, globalID)
}

// parseUUID reads the canonical 8-4-4-4-12 text a UUID is published in.
func parseUUID(t *testing.T, text string) uuid {
	t.Helper()

	decoded, err := hex.DecodeString(strings.ReplaceAll(text, "-", ""))
	require.NoError(t, err)
	require.Len(t, decoded, 16)

	var parsed uuid
	copy(parsed[:], decoded)

	// The text and the bytes have to agree, or a vector recorded here would be
	// checked against something other than the value it was published as.
	require.Equal(t, text, parsed.String())

	return parsed
}
