// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package gml_test

import (
	"errors"
	"fmt"
	"os"

	"github.com/z5labs/dfcad/gml"
)

// A collection is built as a value and handed to Write, which is the whole of
// the interface: one plot with a courtyard taken out of it, in the system the
// caller names and this package never reads.
func ExampleWrite() {
	collection := gml.Collection{
		ID:        "riverside",
		Namespace: "https://example.org/models/riverside",
		Prefix:    "riverside",
		Type:      "region",
		// Carried into the document as the srsName of every geometry, exactly
		// as it is written here. Nothing in this package parses it.
		CRS: "EPSG:6543",
		Features: []gml.Feature{{
			ID: "site.P-01",
			Properties: []gml.Property{
				{Name: "id", Value: "site:P-01"},
				{Name: "label", Value: "Plot one"},
			},
			Surfaces: []gml.Polygon{{
				// Easting then northing, and the first position repeated as
				// the last, which is what closes a ring.
				Exterior: gml.LinearRing{Positions: []gml.Position{
					{Easting: 0, Northing: 0},
					{Easting: 40, Northing: 0},
					{Easting: 40, Northing: 24},
					{Easting: 0, Northing: 24},
					{Easting: 0, Northing: 0},
				}},
				// The courtyard, which survives as a hole rather than becoming
				// a second feature over the top of the first.
				Interior: []gml.LinearRing{{Positions: []gml.Position{
					{Easting: 14, Northing: 8},
					{Easting: 26, Northing: 8},
					{Easting: 26, Northing: 16},
					{Easting: 14, Northing: 16},
					{Easting: 14, Northing: 8},
				}}},
			}},
		}},
	}

	if err := gml.Write(os.Stdout, collection); err != nil {
		fmt.Println(err)
	}

	// Output:
	// <?xml version="1.0" encoding="UTF-8"?>
	// <riverside:FeatureCollection xmlns:riverside="https://example.org/models/riverside" xmlns:gml="http://www.opengis.net/gml/3.2" gml:id="riverside">
	//   <gml:boundedBy>
	//     <gml:Envelope srsName="EPSG:6543" srsDimension="2">
	//       <gml:lowerCorner>0 0</gml:lowerCorner>
	//       <gml:upperCorner>40 24</gml:upperCorner>
	//     </gml:Envelope>
	//   </gml:boundedBy>
	//   <gml:featureMember>
	//     <riverside:region gml:id="site.P-01">
	//       <riverside:id>site:P-01</riverside:id>
	//       <riverside:label>Plot one</riverside:label>
	//       <riverside:geometry>
	//         <gml:MultiSurface gml:id="site.P-01.geometry" srsName="EPSG:6543" srsDimension="2">
	//           <gml:surfaceMember>
	//             <gml:Polygon gml:id="site.P-01.surface.1">
	//               <gml:exterior>
	//                 <gml:LinearRing>
	//                   <gml:posList>0 0 40 0 40 24 0 24 0 0</gml:posList>
	//                 </gml:LinearRing>
	//               </gml:exterior>
	//               <gml:interior>
	//                 <gml:LinearRing>
	//                   <gml:posList>14 8 26 8 26 16 14 16 14 8</gml:posList>
	//                 </gml:LinearRing>
	//               </gml:interior>
	//             </gml:Polygon>
	//           </gml:surfaceMember>
	//         </gml:MultiSurface>
	//       </riverside:geometry>
	//     </riverside:region>
	//   </gml:featureMember>
	// </riverside:FeatureCollection>
}

// A thing which is at a place and covers nothing is a feature of points rather
// than of surfaces: a control point, a manhole, a distribution panel. It is
// written as a gml:MultiPoint under the same geometry property a surface goes
// under, because GML holds one geometry per feature — which is why a feature
// carries one kind or the other and never both.
func ExampleWrite_point() {
	collection := gml.Collection{
		ID:        "riverside",
		Namespace: "https://example.org/models/riverside",
		Prefix:    "riverside",
		Type:      "region",
		CRS:       "EPSG:6543",
		Features: []gml.Feature{{
			ID: "control.CP-3",
			Properties: []gml.Property{
				{Name: "id", Value: "control:CP-3"},
				{Name: "label", Value: "Survey monument 3"},
			},
			Points: []gml.Position{{Easting: 12, Northing: 8}},
		}},
	}

	if err := gml.Write(os.Stdout, collection); err != nil {
		fmt.Println(err)
	}

	// Output:
	// <?xml version="1.0" encoding="UTF-8"?>
	// <riverside:FeatureCollection xmlns:riverside="https://example.org/models/riverside" xmlns:gml="http://www.opengis.net/gml/3.2" gml:id="riverside">
	//   <gml:boundedBy>
	//     <gml:Envelope srsName="EPSG:6543" srsDimension="2">
	//       <gml:lowerCorner>12 8</gml:lowerCorner>
	//       <gml:upperCorner>12 8</gml:upperCorner>
	//     </gml:Envelope>
	//   </gml:boundedBy>
	//   <gml:featureMember>
	//     <riverside:region gml:id="control.CP-3">
	//       <riverside:id>control:CP-3</riverside:id>
	//       <riverside:label>Survey monument 3</riverside:label>
	//       <riverside:geometry>
	//         <gml:MultiPoint gml:id="control.CP-3.geometry" srsName="EPSG:6543" srsDimension="2">
	//           <gml:pointMember>
	//             <gml:Point gml:id="control.CP-3.point.1">
	//               <gml:pos>12 8</gml:pos>
	//             </gml:Point>
	//           </gml:pointMember>
	//         </gml:MultiPoint>
	//       </riverside:geometry>
	//     </riverside:region>
	//   </gml:featureMember>
	// </riverside:FeatureCollection>
}

// A refusal is a value with the fields which made it, so a caller mapping its
// own vocabulary onto this one can tell what to fix without reading a message.
func ExampleWrite_refusal() {
	err := gml.Write(os.Stdout, gml.Collection{
		ID:        "riverside",
		Namespace: "https://example.org/models/riverside",
		Prefix:    "riverside",
		Type:      "region",
		Features: []gml.Feature{{
			// An id in this model's own spelling, which XML has no way to
			// write: the colon is what separates a prefix from a name.
			ID:       "site:P-01",
			Surfaces: []gml.Polygon{{Exterior: gml.LinearRing{}}},
		}},
	})

	var refused gml.NotAnNCNameError
	if errors.As(err, &refused) {
		fmt.Printf("%s: %q\n", refused.What, refused.Name)
	}

	// Output:
	// the id of a feature: "site:P-01"
}
