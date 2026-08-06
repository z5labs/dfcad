// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package gml

import (
	"bytes"
	"errors"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateGolden rewrites the recorded documents under testdata from what the
// writer produces now.
//
// It is spelled `-update` because every package in this module which records a
// copy of something it produces spells it that way, so `go test ./... -update`
// regenerates the lot.
var updateGolden = flag.Bool("update", false, "rewrite the golden files under testdata")

// plaza is the collection every test below is run over.
//
// It is four shapes rather than one because those are the four cases a vector
// document has to carry: a plain rectangle, a rectangle with a courtyard taken
// out of it, one thing which covers two disjoint areas, and a feature whose
// text holds every character XML has to escape. Coordinates are in the
// millions, which is what a projected system in feet looks like, so a document
// which fell back to an exponent would say so here rather than in somebody's
// survey.
func plaza() Collection {
	return Collection{
		ID:        "riverside",
		Namespace: "https://example.org/models/riverside",
		Prefix:    "riverside",
		Type:      "region",
		CRS:       "EPSG:6543",
		Features: []Feature{
			{
				ID: "site.P-01",
				Properties: []Property{
					{Name: "id", Value: "site:P-01"},
					{Name: "label", Value: `Plot one, "the yard" & <the shed>`},
					{Name: "kind", Value: "Site"},
				},
				Surfaces: []Polygon{{
					Exterior: LinearRing{Positions: []Position{
						{Easting: 3502100.5, Northing: 552000.25},
						{Easting: 3502140.5, Northing: 552000.25},
						{Easting: 3502140.5, Northing: 552024.25},
						{Easting: 3502100.5, Northing: 552024.25},
						{Easting: 3502100.5, Northing: 552000.25},
					}},
					Interior: []LinearRing{{Positions: []Position{
						{Easting: 3502114.5, Northing: 552008.25},
						{Easting: 3502126.5, Northing: 552008.25},
						{Easting: 3502126.5, Northing: 552016.25},
						{Easting: 3502114.5, Northing: 552016.25},
						{Easting: 3502114.5, Northing: 552008.25},
					}}},
				}},
			},
			{
				ID: "site.S-101",
				Properties: []Property{
					{Name: "id", Value: "site:S-101"},
					{Name: "label", Value: "Meeting Room A"},
					{Name: "kind", Value: "Space"},
				},
				Surfaces: []Polygon{
					{Exterior: LinearRing{Positions: []Position{
						{Easting: 3502104.5, Northing: 552004.25},
						{Easting: 3502108.5, Northing: 552004.25},
						{Easting: 3502108.5, Northing: 552007.25},
						{Easting: 3502104.5, Northing: 552007.25},
						{Easting: 3502104.5, Northing: 552004.25},
					}}},
					{Exterior: LinearRing{Positions: []Position{
						{Easting: 3502130.5, Northing: 552004.25},
						{Easting: 3502134.5, Northing: 552004.25},
						{Easting: 3502134.5, Northing: 552007.25},
						{Easting: 3502130.5, Northing: 552007.25},
						{Easting: 3502130.5, Northing: 552004.25},
					}}},
				},
			},
		},
	}
}

// written is the document the collection produces, requiring that it was
// produced at all.
func written(t *testing.T, collection Collection) string {
	t.Helper()

	var out bytes.Buffer
	require.NoError(t, Write(&out, collection))

	return out.String()
}

func TestWrite(t *testing.T) {
	got := written(t, plaza())

	assert.Equal(t, golden(t, "plaza.gml", got), got,
		"the recorded document is stale; regenerate it with: go test ./gml -update")
}

// golden is a recorded document, rewritten from got under -update.
func golden(t *testing.T, name, got string) string {
	t.Helper()

	path := filepath.Join("testdata", name)

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// TestWriteIsReadBackAsTheCollectionItWasGiven is the round trip, and it is
// the test the golden above cannot be: a recorded document says what the bytes
// are, and this says that those bytes still mean what they were written to
// mean.
//
// It reads through reader_test.go, which is the standard library's parser and
// a walk of the tree it produces, so nothing here is the writer agreeing with
// itself.
func TestWriteIsReadBackAsTheCollectionItWasGiven(t *testing.T) {
	collection := plaza()

	got := read(t, written(t, collection))

	assert.Equal(t, collection.Namespace, got.Namespace)
	assert.Equal(t, collection.ID, got.ID)
	assert.Equal(t, collection.Type, got.Type)
	assert.Equal(t, collection.CRS, got.CRS)
	assert.Equal(t, collection.Features, got.Features)
}

// TestWriteDeclaresTheExtentTheFeaturesActuallyCover is its own function
// because the envelope is a claim about the rest of the document rather than a
// part of it, and the thing worth asserting is that the two agree.
func TestWriteDeclaresTheExtentTheFeaturesActuallyCover(t *testing.T) {
	collection := plaza()

	got := read(t, written(t, collection))
	require.True(t, got.Bounded)

	lower := Position{Easting: math.Inf(1), Northing: math.Inf(1)}
	upper := Position{Easting: math.Inf(-1), Northing: math.Inf(-1)}

	for _, feature := range collection.Features {
		for _, surface := range feature.Surfaces {
			for _, ring := range append([]LinearRing{surface.Exterior}, surface.Interior...) {
				for _, at := range ring.Positions {
					lower.Easting = math.Min(lower.Easting, at.Easting)
					lower.Northing = math.Min(lower.Northing, at.Northing)
					upper.Easting = math.Max(upper.Easting, at.Easting)
					upper.Northing = math.Max(upper.Northing, at.Northing)
				}
			}
		}
	}

	assert.Equal(t, lower, got.Lower)
	assert.Equal(t, upper, got.Upper)
}

// TestWriteIsByteIdenticalForOneCollection is its own function because it is
// about two writes rather than one: the golden says what the bytes are, and
// this says that nothing outside the collection reaches them.
func TestWriteIsByteIdenticalForOneCollection(t *testing.T) {
	assert.Equal(t, written(t, plaza()), written(t, plaza()))
}

// TestWriteCarriesTheCoordinateReferenceSystemWithoutReadingIt walks what the
// package promises about the identifier: it reaches every geometry, it reaches
// it unchanged, and nothing about it is checked.
func TestWriteCarriesTheCoordinateReferenceSystem(t *testing.T) {
	testCases := []struct {
		name     string
		crs      string
		expected string
	}{
		{
			name:     "carries an authority and a code",
			crs:      "EPSG:6543",
			expected: "EPSG:6543",
		},
		{
			name:     "carries a urn, which is another spelling of the same thing",
			crs:      "urn:ogc:def:crs:EPSG::6543",
			expected: "urn:ogc:def:crs:EPSG::6543",
		},
		{
			name:     "carries text no register would recognise, because it reads none of it",
			crs:      "a system nobody has heard of",
			expected: "a system nobody has heard of",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			collection := plaza()
			collection.CRS = testCase.crs

			got := read(t, written(t, collection))

			assert.Equal(t, testCase.expected, got.CRS)
		})
	}
}

// TestWriteNamesNoSystemWhereItWasGivenNone is its own function because the
// absence is the assertion: a collection with no identifier is written without
// one rather than with an empty one, which would be a document naming a system
// called nothing.
func TestWriteNamesNoSystemWhereItWasGivenNone(t *testing.T) {
	collection := plaza()
	collection.CRS = ""

	source := written(t, collection)

	assert.NotContains(t, source, "srsName")
	assert.Contains(t, source, `srsDimension="2"`)
}

// TestWriteIdentifiesEveryElementGMLRequiresAnIdentifierFor is its own
// function because it is about the document as a whole rather than about any
// feature in it: the ids of the geometries are derived, and a derivation which
// collided would produce a document two readers disagree about.
func TestWriteIdentifiesEveryElementGMLRequiresAnIdentifierFor(t *testing.T) {
	source := written(t, plaza())

	found := identifiers(t, source)

	assert.Equal(t, []string{
		"riverside",
		"site.P-01", "site.P-01.geometry", "site.P-01.surface.1",
		"site.S-101", "site.S-101.geometry", "site.S-101.surface.1", "site.S-101.surface.2",
	}, found)

	seen := make(map[string]bool, len(found))
	for _, id := range found {
		assert.False(t, seen[id], "%s is written twice", id)
		seen[id] = true
	}
}

// TestWriteEscapesWhatXMLCannotCarryLiterally is its own function because what
// it asserts is about the bytes rather than about the tree: a reader recovers
// the text either way, and the point is that the document is well formed on
// the way there.
func TestWriteEscapesWhatXMLCannotCarryLiterally(t *testing.T) {
	source := written(t, plaza())

	assert.NotContains(t, source, "<the shed>")
	assert.Contains(t, source, "&lt;the shed&gt;")
	assert.Contains(t, source, "&amp;")
}

// TestWriteWritesAnEmptyCollectionAsOne is its own function because it is a
// different shape of answer: a caller with nothing to write gets a document
// holding no members, and an envelope over no positions is left out rather
// than written over nothing.
func TestWriteWritesAnEmptyCollectionAsOne(t *testing.T) {
	collection := plaza()
	collection.Features = nil

	source := written(t, collection)

	assert.NotContains(t, source, "featureMember")
	assert.NotContains(t, source, "boundedBy")

	got := read(t, source)
	assert.Empty(t, got.Features)
	assert.False(t, got.Bounded)
}

// square is a closed ring, which every refusal below starts from and breaks in
// exactly one way.
func square() LinearRing {
	return LinearRing{Positions: []Position{
		{Easting: 0, Northing: 0},
		{Easting: 4, Northing: 0},
		{Easting: 4, Northing: 3},
		{Easting: 0, Northing: 3},
		{Easting: 0, Northing: 0},
	}}
}

// one is a collection of a single feature carrying the surfaces given, which
// is the smallest thing the writer will accept.
func one(surfaces ...Polygon) Collection {
	return Collection{
		ID:        "collection",
		Namespace: "https://example.org/models/riverside",
		Prefix:    "riverside",
		Type:      "region",
		Features:  []Feature{{ID: "feature", Surfaces: surfaces}},
	}
}

// TestWriteRefusesADocumentNoReaderCouldRead walks every refusal.
//
// Each case is asserted as the whole error value, which is its type and every
// field of it at once, and no case asserts on the text of a message.
func TestWriteRefusesADocumentNoReaderCouldRead(t *testing.T) {
	testCases := []struct {
		name       string
		collection func() Collection
		expected   error
	}{
		{
			name: "a collection naming no namespace, which nothing in it could be written in",
			collection: func() Collection {
				collection := one(Polygon{Exterior: square()})
				collection.Namespace = ""
				return collection
			},
			expected: MissingNamespaceError{},
		},
		{
			name: "a namespace bound to the prefix GML's own elements are written under",
			collection: func() Collection {
				collection := one(Polygon{Exterior: square()})
				collection.Prefix = Prefix
				return collection
			},
			expected: ReservedPrefixError{Prefix: "gml", Bound: Namespace},
		},
		{
			name: "a namespace bound to a prefix the specification reserves",
			collection: func() Collection {
				collection := one(Polygon{Exterior: square()})
				collection.Prefix = "xmlns"
				return collection
			},
			expected: ReservedPrefixError{Prefix: "xmlns"},
		},
		{
			name: "the same, spelled so that a comparison on the letters is the only one which catches it",
			collection: func() Collection {
				collection := one(Polygon{Exterior: square()})
				collection.Prefix = "XmlSchema"
				return collection
			},
			expected: ReservedPrefixError{Prefix: "XmlSchema"},
		},
		{
			name: "a feature type XML cannot spell",
			collection: func() Collection {
				collection := one(Polygon{Exterior: square()})
				collection.Type = "region:outdoor"
				return collection
			},
			expected: NotAnNCNameError{What: "the feature type", Name: "region:outdoor"},
		},
		{
			name: "a feature identified by something which is not a name",
			collection: func() Collection {
				collection := one(Polygon{Exterior: square()})
				collection.Features[0].ID = "site:P-01"
				return collection
			},
			expected: NotAnNCNameError{What: "the id of a feature", Name: "site:P-01"},
		},
		{
			name: "a property named what the geometry is written under",
			collection: func() Collection {
				collection := one(Polygon{Exterior: square()})
				collection.Features[0].Properties = []Property{{Name: "geometry", Value: "here"}}
				return collection
			},
			expected: ReservedPropertyError{Feature: "feature", Name: "geometry"},
		},
		{
			name: "two features sharing one identifier",
			collection: func() Collection {
				collection := one(Polygon{Exterior: square()})
				collection.Features = append(collection.Features, collection.Features[0])
				return collection
			},
			expected: DuplicateIDError{ID: "feature"},
		},
		{
			name: "a feature whose identifier collides with a geometry's derived one",
			collection: func() Collection {
				collection := one(Polygon{Exterior: square()})
				second := collection.Features[0]
				second.ID = "feature.geometry"
				collection.Features = append(collection.Features, second)
				return collection
			},
			expected: DuplicateIDError{ID: "feature.geometry"},
		},
		{
			name: "a feature covering nothing",
			collection: func() Collection {
				return one()
			},
			expected: NoGeometryError{Feature: "feature"},
		},
		{
			name: "a ring of too few positions to bound anything",
			collection: func() Collection {
				ring := square()
				ring.Positions = ring.Positions[:3]
				return one(Polygon{Exterior: ring})
			},
			expected: TooFewPositionsError{Feature: "feature", Positions: 3},
		},
		{
			name: "a ring which does not close",
			collection: func() Collection {
				ring := square()
				ring.Positions[len(ring.Positions)-1] = Position{Easting: 0, Northing: 1}
				return one(Polygon{Exterior: ring})
			},
			expected: UnclosedRingError{
				Feature: "feature",
				First:   Position{Easting: 0, Northing: 0},
				Last:    Position{Easting: 0, Northing: 1},
			},
		},
		{
			name: "a hole which does not close, which is a ring like any other",
			collection: func() Collection {
				hole := square()
				hole.Positions[len(hole.Positions)-1] = Position{Easting: 1, Northing: 1}
				return one(Polygon{Exterior: square(), Interior: []LinearRing{hole}})
			},
			expected: UnclosedRingError{
				Feature: "feature",
				First:   Position{Easting: 0, Northing: 0},
				Last:    Position{Easting: 1, Northing: 1},
			},
		},
		{
			name: "a coordinate which is not a number",
			collection: func() Collection {
				ring := square()
				ring.Positions[1].Northing = math.Inf(1)
				return one(Polygon{Exterior: ring})
			},
			expected: NonFiniteCoordinateError{Feature: "feature", Easting: 4, Northing: math.Inf(1)},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var out bytes.Buffer

			err := Write(&out, testCase.collection())

			assert.Equal(t, testCase.expected, err)
			assert.Empty(t, out.String(), "a document which is refused is not half written first")
		})
	}
}

// TestWriteRefusesANotANumberBeforeItComparesOne is its own function because
// it is about the order of two checks rather than about either of them: a
// not-a-number fails every comparison, including the one which decides whether
// a ring closes, so a writer which closed the ring first would report the
// wrong thing about it.
func TestWriteRefusesANotANumberBeforeItComparesOne(t *testing.T) {
	ring := square()
	ring.Positions[0].Easting = math.NaN()
	ring.Positions[len(ring.Positions)-1].Easting = math.NaN()

	err := Write(&bytes.Buffer{}, one(Polygon{Exterior: ring}))

	var refused NonFiniteCoordinateError
	require.ErrorAs(t, err, &refused)

	assert.Equal(t, "feature", refused.Feature)
	assert.True(t, math.IsNaN(refused.Easting))
}

// TestOrdinate is its own function because number formatting is where a format
// like this one quietly stops being readable, and the cases are about the
// spelling rather than about a document.
func TestOrdinate(t *testing.T) {
	testCases := []struct {
		name     string
		value    float64
		expected string
	}{
		{
			name:     "writes a whole number without a point",
			value:    4,
			expected: "4",
		},
		{
			name:     "writes the shortest decimal which reads back as the same number",
			value:    3502100.5,
			expected: "3502100.5",
		},
		{
			name:     "never writes an exponent, however large the figure",
			value:    12345678901234,
			expected: "12345678901234",
		},
		{
			name:     "never writes an exponent, however small the figure",
			value:    0.0000001,
			expected: "0.0000001",
		},
		{
			name:     "keeps the sign of a southing or a westing",
			value:    -12.25,
			expected: "-12.25",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, ordinate(testCase.value))
		})
	}
}

// TestNCName is its own function for the reason above: what XML will take as a
// name is a rule of the format rather than a property of any document.
func TestNCName(t *testing.T) {
	testCases := []struct {
		name     string
		given    string
		expected bool
	}{
		{name: "takes a plain word", given: "region", expected: true},
		{name: "takes dots, hyphens and digits after the first character", given: "site.P-01", expected: true},
		{name: "takes an underscore first", given: "_01", expected: true},
		{name: "refuses a digit first", given: "01", expected: false},
		{name: "refuses a colon, which is what separates a prefix from a name", given: "site:P-01", expected: false},
		{name: "refuses a space", given: "Plot one", expected: false},
		{name: "refuses a slash", given: "ifc/project", expected: false},
		{name: "refuses nothing at all", given: "", expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, ncname(testCase.given))
		})
	}
}

// TestWriteReportsADestinationItCannotWriteTo is its own function because the
// failure is the writer's rather than the collection's, and it is the one
// error here which is not a refusal.
func TestWriteReportsADestinationItCannotWriteTo(t *testing.T) {
	err := Write(refusing{}, plaza())

	assert.ErrorIs(t, err, errRefused)
}

// refusing is a writer which accepts nothing, which is a full disk or a closed
// pipe as this package meets one.
type refusing struct{}

func (refusing) Write([]byte) (int, error) { return 0, errRefused }

// errRefused is what it fails with. It is a sentinel because there is nothing
// to carry: the test asks whether the writer's own error came back, not what
// it said.
var errRefused = errors.New("the destination accepts nothing")

// TestTheDocumentIsWellFormedXML is its own function because it is the
// weakest and most important thing that can be said about the output: whatever
// else it holds, a parser which knows nothing about GML gets to the end of it.
func TestTheDocumentIsWellFormedXML(t *testing.T) {
	source := written(t, plaza())

	assert.True(t, strings.HasPrefix(source, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"))
	assert.True(t, strings.HasSuffix(source, "\n"))

	// The reader is a full parse, so reaching the end of it is the assertion.
	assert.NotEmpty(t, read(t, source).Features)
}
