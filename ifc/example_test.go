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
