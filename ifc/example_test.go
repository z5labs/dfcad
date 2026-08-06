// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package ifc_test

import (
	"fmt"
	"os"

	"github.com/z5labs/dfcad/ifc"
)

// A model is built as a value and handed to Write, which is the whole of the
// interface: one site holding one building, in metres, with the identifiers
// the caller derived.
func ExampleWrite() {
	model := ifc.Model{
		Header: ifc.Header{
			Name: "model.ifc",
			// The stamp is supplied rather than read, which is what makes two
			// writes of an unchanged model the same bytes.
			TimeStamp:    "1970-01-01T00:00:00",
			Preprocessor: "dfcad",
			Originating:  "dfcad",
		},
		Units: ifc.UnitAssignment{Units: []ifc.SIUnit{
			{Type: "LENGTHUNIT", Name: "METRE"},
		}},
		Context: ifc.RepresentationContext{Type: "Model", Dimension: 3},
		Project: ifc.Project{
			GlobalID:   "0Ig1S2wRr2WQeQMwAKN3aq",
			Name:       "Riverside",
			Aggregates: "1Ig1S2wRr2WQeQMwAKN3aq",
			Sites: []ifc.Spatial{{
				Entity:      ifc.EntitySite,
				GlobalID:    "2Ig1S2wRr2WQeQMwAKN3aq",
				Name:        "Plot one",
				Composition: ifc.CompositionElement,
				Placement:   &ifc.Placement{},
				Aggregates:  "3Ig1S2wRr2WQeQMwAKN3aq",
				Children: []ifc.Spatial{{
					Entity:      ifc.EntityBuilding,
					GlobalID:    "4Ig1S2wRr2WQeQMwAKN3aq",
					Name:        "Block A",
					Composition: ifc.CompositionElement,
					Placement:   &ifc.Placement{},
				}},
			}},
		},
	}

	if err := ifc.Write(os.Stdout, model); err != nil {
		fmt.Println(err)
	}

	// Output:
	// ISO-10303-21;
	// HEADER;
	// FILE_DESCRIPTION((),'2;1');
	// FILE_NAME('model.ifc','1970-01-01T00:00:00',(),(),'dfcad','dfcad','');
	// FILE_SCHEMA(('IFC4'));
	// ENDSEC;
	// DATA;
	// #1=IFCSIUNIT(*,.LENGTHUNIT.,$,.METRE.);
	// #2=IFCUNITASSIGNMENT((#1));
	// #3=IFCCARTESIANPOINT((0.,0.,0.));
	// #4=IFCAXIS2PLACEMENT3D(#3,$,$);
	// #5=IFCGEOMETRICREPRESENTATIONCONTEXT($,'Model',3,$,#4,$);
	// #6=IFCPROJECT('0Ig1S2wRr2WQeQMwAKN3aq',$,'Riverside',$,$,$,$,(#5),#2);
	// #7=IFCLOCALPLACEMENT($,#4);
	// #8=IFCSITE('2Ig1S2wRr2WQeQMwAKN3aq',$,'Plot one',$,$,#7,$,$,.ELEMENT.,$,$,$,$,$);
	// #9=IFCLOCALPLACEMENT(#7,#4);
	// #10=IFCBUILDING('4Ig1S2wRr2WQeQMwAKN3aq',$,'Block A',$,$,#9,$,$,.ELEMENT.,$,$,$);
	// #11=IFCRELAGGREGATES('3Ig1S2wRr2WQeQMwAKN3aq',$,$,$,#8,(#10));
	// #12=IFCRELAGGREGATES('1Ig1S2wRr2WQeQMwAKN3aq',$,$,$,#6,(#8));
	// ENDSEC;
	// END-ISO-10303-21;
}

// EncodeGlobalID is the encoding on its own, for a caller which has derived
// the 128 bits some other way.
func ExampleEncodeGlobalID() {
	fmt.Println(ifc.EncodeGlobalID([16]byte{
		0xa1, 0x6b, 0xfc, 0x45, 0x71, 0x56, 0x45, 0x58,
		0xb5, 0x7c, 0x54, 0x41, 0x02, 0xce, 0x43, 0xfb,
	}))

	// Output:
	// 2XQ$n5SLP5MBLyL442paFx
}

// A space carries the outline its model states and the solid a viewer can
// draw, as two representations of one shape definition.
//
// The footprint is a plan and carries no elevation; the body is that plan
// swept upwards, and the level it sits at is the placement of the extrusion.
// Both curves close by repeating their first point, which is what IFC asks of
// a profile's curves.
func ExampleRepresentation() {
	outline := ifc.Polyline{Points: []ifc.Point2D{
		{X: 0, Y: 0}, {X: 4, Y: 0}, {X: 4, Y: 3}, {X: 0, Y: 3}, {X: 0, Y: 0},
	}}

	model := ifc.Model{
		Header: ifc.Header{Name: "model.ifc", TimeStamp: "1970-01-01T00:00:00"},
		Units:  ifc.UnitAssignment{Units: []ifc.SIUnit{{Type: "LENGTHUNIT", Name: "METRE"}}},
		Context: ifc.RepresentationContext{
			Type:      "Model",
			Dimension: 3,
			Subcontexts: []ifc.Subcontext{
				{Identifier: "Body", Type: "Model", TargetView: "MODEL_VIEW"},
				{Identifier: "FootPrint", Type: "Model", TargetView: "PLAN_VIEW"},
			},
		},
		Project: ifc.Project{
			GlobalID:   "0Ig1S2wRr2WQeQMwAKN3aq",
			Aggregates: "1Ig1S2wRr2WQeQMwAKN3aq",
			Sites: []ifc.Spatial{{
				Entity:    ifc.EntitySpace,
				GlobalID:  "2Ig1S2wRr2WQeQMwAKN3aq",
				Name:      "Meeting Room A",
				Placement: &ifc.Placement{},
				Representation: &ifc.Representation{Shapes: []ifc.Shape{{
					Context:    "FootPrint",
					Identifier: "FootPrint",
					Type:       "Curve2D",
					Items:      []ifc.Item{outline},
				}, {
					Context:    "Body",
					Identifier: "Body",
					Type:       "SweptSolid",
					Items: []ifc.Item{ifc.ExtrudedArea{
						Profile:   ifc.ArbitraryProfile{Outer: outline},
						Direction: ifc.Direction{Z: 1},
						Depth:     2.7,
					}},
				}}},
			}},
		},
	}

	if err := ifc.Write(os.Stdout, model); err != nil {
		fmt.Println(err)
	}

	// Output:
	// ISO-10303-21;
	// HEADER;
	// FILE_DESCRIPTION((),'2;1');
	// FILE_NAME('model.ifc','1970-01-01T00:00:00',(),(),'','','');
	// FILE_SCHEMA(('IFC4'));
	// ENDSEC;
	// DATA;
	// #1=IFCSIUNIT(*,.LENGTHUNIT.,$,.METRE.);
	// #2=IFCUNITASSIGNMENT((#1));
	// #3=IFCCARTESIANPOINT((0.,0.,0.));
	// #4=IFCAXIS2PLACEMENT3D(#3,$,$);
	// #5=IFCGEOMETRICREPRESENTATIONCONTEXT($,'Model',3,$,#4,$);
	// #6=IFCGEOMETRICREPRESENTATIONSUBCONTEXT('Body','Model',*,*,*,*,#5,$,.MODEL_VIEW.,$);
	// #7=IFCGEOMETRICREPRESENTATIONSUBCONTEXT('FootPrint','Model',*,*,*,*,#5,$,.PLAN_VIEW.,$);
	// #8=IFCPROJECT('0Ig1S2wRr2WQeQMwAKN3aq',$,$,$,$,$,$,(#5),#2);
	// #9=IFCLOCALPLACEMENT($,#4);
	// #10=IFCCARTESIANPOINT((0.,0.));
	// #11=IFCCARTESIANPOINT((4.,0.));
	// #12=IFCCARTESIANPOINT((4.,3.));
	// #13=IFCCARTESIANPOINT((0.,3.));
	// #14=IFCPOLYLINE((#10,#11,#12,#13,#10));
	// #15=IFCSHAPEREPRESENTATION(#7,'FootPrint','Curve2D',(#14));
	// #16=IFCARBITRARYCLOSEDPROFILEDEF(.AREA.,$,#14);
	// #17=IFCDIRECTION((0.,0.,1.));
	// #18=IFCEXTRUDEDAREASOLID(#16,#4,#17,2.7);
	// #19=IFCSHAPEREPRESENTATION(#6,'Body','SweptSolid',(#18));
	// #20=IFCPRODUCTDEFINITIONSHAPE($,$,(#15,#19));
	// #21=IFCSPACE('2Ig1S2wRr2WQeQMwAKN3aq',$,'Meeting Room A',$,$,#9,#20,$,$,$,$);
	// #22=IFCRELAGGREGATES('1Ig1S2wRr2WQeQMwAKN3aq',$,$,$,#8,(#21));
	// ENDSEC;
	// END-ISO-10303-21;
}

// A georeference says where the file's coordinate space sits on the earth: the
// projected system it is expressed in, and the conversion into it.
//
// Both strings are written and neither is read. Where the model's own frame is
// the projected system, the conversion is the identity — the offsets are nought
// because the schema requires them, and the rotation and the scale are absent,
// which is the schema saying there is none rather than a writer stating a fit
// nobody measured.
func ExampleGeoreference() {
	model := ifc.Model{
		Header: ifc.Header{Name: "model.ifc", TimeStamp: "1970-01-01T00:00:00"},
		Units: ifc.UnitAssignment{Units: []ifc.SIUnit{
			{Type: "LENGTHUNIT", Name: "METRE"},
		}},
		Context: ifc.RepresentationContext{Type: "Model", Dimension: 3},
		Georeference: &ifc.Georeference{
			CRS: ifc.ProjectedCRS{
				Name:        "EPSG:25831",
				Description: `PROJCS["ETRS89 / UTM zone 31N"]`,
			},
		},
		Project: ifc.Project{GlobalID: "0Ig1S2wRr2WQeQMwAKN3aq", Name: "Riverside"},
	}

	if err := ifc.Write(os.Stdout, model); err != nil {
		fmt.Println(err)
	}

	// Output:
	// ISO-10303-21;
	// HEADER;
	// FILE_DESCRIPTION((),'2;1');
	// FILE_NAME('model.ifc','1970-01-01T00:00:00',(),(),'','','');
	// FILE_SCHEMA(('IFC4'));
	// ENDSEC;
	// DATA;
	// #1=IFCSIUNIT(*,.LENGTHUNIT.,$,.METRE.);
	// #2=IFCUNITASSIGNMENT((#1));
	// #3=IFCCARTESIANPOINT((0.,0.,0.));
	// #4=IFCAXIS2PLACEMENT3D(#3,$,$);
	// #5=IFCGEOMETRICREPRESENTATIONCONTEXT($,'Model',3,$,#4,$);
	// #6=IFCPROJECTEDCRS('EPSG:25831','PROJCS["ETRS89 / UTM zone 31N"]',$,$,$,$,$);
	// #7=IFCMAPCONVERSION(#5,#6,0.,0.,0.,$,$,$);
	// #8=IFCPROJECT('0Ig1S2wRr2WQeQMwAKN3aq',$,'Riverside',$,$,$,$,(#5),#2);
	// ENDSEC;
	// END-ISO-10303-21;
}
