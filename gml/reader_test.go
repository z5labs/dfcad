// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package gml

import (
	"encoding/xml"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// This file is a reader, and it shares nothing with the writer beside it.
//
// It exists because "the document is readable" asserted by the code which
// produced it is the writer agreeing with itself, and a writer which emits an
// unbalanced tag, a ring in the wrong element or an identifier it also used
// somewhere else agrees with itself perfectly. So the checks in write_test.go
// go through this instead: the standard library's XML parser over the bytes,
// and a walk of the tree it produces which knows GML's element names and
// nothing at all about how those bytes came to be.
//
// Nothing here calls into write.go, and it must stay that way. A reader which
// reused the writer's escaping, its number formatting or its element name
// constants would stop being a second opinion the moment one of them was
// wrong — so the names below are written out again on purpose, and the
// duplication is the point rather than an oversight.

// tree is one element of a parsed document, with its attributes, its text and
// its children.
//
// It is the whole of the parsing: [xml.Unmarshal] resolves the namespace
// prefixes to their URIs and hands back the document as it stands, and
// everything below is a walk of that. A struct per GML element would be this
// file agreeing with a schema instead of with the bytes.
type tree struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Text     string     `xml:",chardata"`
	Children []tree     `xml:",any"`
}

// document is a collection as this reader sees one.
//
// The prefixes are not in it because a parser resolves them away, which is the
// right answer: two documents differing only in their prefixes say the same
// thing. That the writer spells them the way it says it does is what the
// golden fixture is for.
type document struct {
	Namespace string
	ID        string
	Type      string
	CRS       string
	Features  []Feature

	// Lower and Upper are the corners of the envelope the document declares,
	// which is a claim about the features under it and is checked against
	// them.
	Lower, Upper Position
	Bounded      bool
}

// read parses a document and walks it into the collection it describes.
func read(t *testing.T, source string) document {
	t.Helper()

	var root tree
	require.NoError(t, xml.Unmarshal([]byte(source), &root))

	require.Equal(t, "FeatureCollection", root.XMLName.Local)

	read := document{
		Namespace: root.XMLName.Space,
		ID:        attributeOf(t, root, Namespace, "id"),
	}

	for _, child := range root.Children {
		switch {
		case child.XMLName.Space == Namespace && child.XMLName.Local == "boundedBy":
			read.Lower, read.Upper, read.Bounded = readEnvelope(t, child)

		case child.XMLName.Space == Namespace && child.XMLName.Local == "featureMember":
			require.Len(t, child.Children, 1, "a feature member holds one feature")

			member := child.Children[0]
			require.Equal(t, root.XMLName.Space, member.XMLName.Space,
				"a feature is written in the collection's own namespace")

			if read.Type == "" {
				read.Type = member.XMLName.Local
			}
			require.Equal(t, read.Type, member.XMLName.Local, "every feature is of one type")

			feature, crs := readFeature(t, member)
			if read.CRS == "" {
				read.CRS = crs
			}
			require.Equal(t, read.CRS, crs, "every geometry names one coordinate reference system")

			read.Features = append(read.Features, feature)

		default:
			t.Fatalf("unexpected child of the collection: %s", child.XMLName.Local)
		}
	}

	return read
}

// readEnvelope is the extent a document declares.
func readEnvelope(t *testing.T, bounded tree) (lower, upper Position, ok bool) {
	t.Helper()

	envelope := only(t, bounded, Namespace, "Envelope")

	corners := make(map[string]Position, 2)
	for _, corner := range []string{"lowerCorner", "upperCorner"} {
		positions := readPositions(t, only(t, envelope, Namespace, corner).Text)
		require.Len(t, positions, 1, "a corner is one position")
		corners[corner] = positions[0]
	}

	return corners["lowerCorner"], corners["upperCorner"], true
}

// readFeature is one feature: its properties, its shape, and the coordinate
// reference system its shape is in.
func readFeature(t *testing.T, member tree) (Feature, string) {
	t.Helper()

	feature := Feature{ID: attributeOf(t, member, Namespace, "id")}

	var crs string

	for _, child := range member.Children {
		require.Equal(t, member.XMLName.Space, child.XMLName.Space,
			"a property and the geometry are in the feature's own namespace")

		if child.XMLName.Local == "geometry" {
			// A feature carries one geometry, and which of the two kinds it is
			// is read off the element rather than told to this reader. A
			// document holding both under one property would fail here, which
			// is what a second opinion is for.
			require.Len(t, child.Children, 1, "a geometry property holds one geometry")

			switch shape := child.Children[0]; shape.XMLName.Local {
			case "MultiSurface":
				feature.Surfaces, crs = readSurfaces(t, shape)
			case "MultiPoint":
				feature.Points, crs = readPoints(t, shape)
			default:
				t.Fatalf("unexpected geometry: %s", shape.XMLName.Local)
			}

			continue
		}

		require.Empty(t, child.Children, "a property holds text and nothing else")
		feature.Properties = append(feature.Properties, Property{
			Name:  child.XMLName.Local,
			Value: child.Text,
		})
	}

	return feature, crs
}

// readSurfaces is the polygons of one multi surface, and the system they are
// in.
func readSurfaces(t *testing.T, surfaces tree) ([]Polygon, string) {
	t.Helper()

	require.Equal(t, strconv.Itoa(Dimension), attributeOf(t, surfaces, "", "srsDimension"))

	var out []Polygon

	for _, member := range surfaces.Children {
		require.Equal(t, "surfaceMember", member.XMLName.Local)

		polygon := only(t, member, Namespace, "Polygon")

		var surface Polygon
		var exteriors int

		for _, ring := range polygon.Children {
			positions := readRing(t, ring)

			switch ring.XMLName.Local {
			case "exterior":
				exteriors++
				surface.Exterior = positions
			case "interior":
				surface.Interior = append(surface.Interior, positions)
			default:
				t.Fatalf("unexpected ring of a polygon: %s", ring.XMLName.Local)
			}
		}

		require.Equal(t, 1, exteriors, "a polygon has one exterior ring")

		out = append(out, surface)
	}

	return out, attributeOf(t, surfaces, "", "srsName")
}

// readPoints is the positions of one multi point, and the system they are in.
func readPoints(t *testing.T, points tree) ([]Position, string) {
	t.Helper()

	require.Equal(t, strconv.Itoa(Dimension), attributeOf(t, points, "", "srsDimension"))

	var out []Position

	for _, member := range points.Children {
		require.Equal(t, "pointMember", member.XMLName.Local)

		at := readPositions(t, only(t, only(t, member, Namespace, "Point"), Namespace, "pos").Text)
		require.Len(t, at, 1, "a point is one position")

		out = append(out, at[0])
	}

	return out, attributeOf(t, points, "", "srsName")
}

// readRing is the positions of one ring.
func readRing(t *testing.T, ring tree) LinearRing {
	t.Helper()

	list := only(t, only(t, ring, Namespace, "LinearRing"), Namespace, "posList")

	return LinearRing{Positions: readPositions(t, list.Text)}
}

// readPositions is a coordinate list as GML writes one: the ordinates
// separated by white space, easting first.
func readPositions(t *testing.T, list string) []Position {
	t.Helper()

	ordinates := strings.Fields(list)
	require.Zero(t, len(ordinates)%Dimension, "a coordinate list holds whole positions")

	positions := make([]Position, 0, len(ordinates)/Dimension)

	for i := 0; i < len(ordinates); i += Dimension {
		easting, err := strconv.ParseFloat(ordinates[i], 64)
		require.NoError(t, err)

		northing, err := strconv.ParseFloat(ordinates[i+1], 64)
		require.NoError(t, err)

		positions = append(positions, Position{Easting: easting, Northing: northing})
	}

	return positions
}

// only is the one child of parent with the name given.
func only(t *testing.T, parent tree, space, local string) tree {
	t.Helper()

	var found []tree
	for _, child := range parent.Children {
		if child.XMLName.Space == space && child.XMLName.Local == local {
			found = append(found, child)
		}
	}

	require.Lenf(t, found, 1, "%s holds one %s", parent.XMLName.Local, local)

	return found[0]
}

// attributeOf is the value of one attribute, which is empty where the element
// carries none of that name.
func attributeOf(t *testing.T, element tree, space, local string) string {
	t.Helper()

	for _, attr := range element.Attrs {
		if attr.Name.Local != local {
			continue
		}
		// An unprefixed attribute is in no namespace, which the parser reports
		// as an empty space, so the two comparisons are the same one.
		if attr.Name.Space == space {
			return attr.Value
		}
	}

	return ""
}

// identifiers is every gml:id in a document, in the order they are written,
// which is what a check for a repeat reads.
func identifiers(t *testing.T, source string) []string {
	t.Helper()

	var root tree
	require.NoError(t, xml.Unmarshal([]byte(source), &root))

	var found []string

	var walk func(tree)
	walk = func(element tree) {
		if id := attributeOf(t, element, Namespace, "id"); id != "" {
			found = append(found, id)
		}
		for _, child := range element.Children {
			walk(child)
		}
	}
	walk(root)

	return found
}
