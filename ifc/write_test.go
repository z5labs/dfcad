// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package ifc

import (
	"errors"
	"flag"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateGolden rewrites the golden files under testdata from what this package
// produced, so that a deliberate change to the bytes is a diff to review
// rather than a string literal to retype.
//
//	go test ./ifc -update
//
// The golden is the whole of the byte-identity property: an assertion that two
// writes in one process agree only tests that the writer agrees with itself,
// which is the one thing a broken writer also does.
var updateGolden = flag.Bool("update", false, "rewrite the golden files under testdata")

// golden returns the expected file held in testdata/name, having first
// rewritten it from got when -update was passed.
func golden(t *testing.T, name string, got string) string {
	t.Helper()

	path := "testdata/" + name
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

// origin is the placement everything in the fixture is placed by, which is
// also what makes the fixture exercise interning: one point and one axis
// placement stand behind every element in it.
func origin() *Placement {
	return &Placement{Location: Point{}}
}

// fixture is the model the golden below holds, and is one of everything this
// package writes: the four spatial elements nested as IFC nests them, a
// product whose type has an entity of its own, a product whose type has none,
// and a zone assigning things from two different storeys.
//
// It is a function rather than a variable so that a test which changes one
// field of it cannot change it for the next test.
func fixture() Model {
	return Model{
		Header: Header{
			Description:   []string{"ViewDefinition [CoordinationView]"},
			Name:          "model.ifc",
			TimeStamp:     "1970-01-01T00:00:00",
			Author:        []string{""},
			Organisation:  []string{""},
			Preprocessor:  "dfcad",
			Originating:   "dfcad",
			Authorisation: "",
		},
		Units: UnitAssignment{Units: []Unit{
			SIUnit{Type: "LENGTHUNIT", Name: "METRE"},
			SIUnit{Type: "AREAUNIT", Name: "SQUARE_METRE"},
			SIUnit{Type: "VOLUMEUNIT", Name: "CUBIC_METRE"},
			SIUnit{Type: "PLANEANGLEUNIT", Name: "RADIAN"},
		}},
		Context: RepresentationContext{
			Type:      "Model",
			Dimension: 3,
			Precision: 0.00001,
			World:     Placement{Location: Point{}},
		},
		Project: Project{
			GlobalID:   "0Ig1S2wRr2WQeQMwAKN3aq",
			Name:       "Riverside example",
			LongName:   "Riverside",
			Aggregates: "1Ig1S2wRr2WQeQMwAKN3aq",
			Sites: []Spatial{{
				Entity:      EntitySite,
				GlobalID:    "2Ig1S2wRr2WQeQMwAKN3aq",
				Name:        "site:P-01",
				LongName:    "Plot one",
				Composition: CompositionElement,
				Placement:   origin(),
				Aggregates:  "3Ig1S2wRr2WQeQMwAKN3aq",
				Children: []Spatial{{
					Entity:      EntityBuilding,
					GlobalID:    "4Ig1S2wRr2WQeQMwAKN3aq",
					Name:        "site:B-01",
					LongName:    "Block A",
					Composition: CompositionElement,
					Placement:   origin(),
					Aggregates:  "5Ig1S2wRr2WQeQMwAKN3aq",
					Children: []Spatial{{
						Entity:      EntityBuildingStorey,
						GlobalID:    "6Ig1S2wRr2WQeQMwAKN3aq",
						Name:        "site:L-01",
						LongName:    "Level one",
						Composition: CompositionElement,
						Placement:   origin(),
						Aggregates:  "7Ig1S2wRr2WQeQMwAKN3aq",
						Children: []Spatial{{
							Entity:      EntitySpace,
							GlobalID:    "8Ig1S2wRr2WQeQMwAKN3aq",
							Name:        "site:S-101",
							LongName:    "Meeting Room A",
							ObjectType:  "MeetingRoom",
							Composition: CompositionElement,
							Placement:   &Placement{Location: Point{X: 4, Y: 3, Z: 0}},
							Contains:    "9Ig1S2wRr2WQeQMwAKN3aq",
							Products: []Product{{
								Entity:     "IFCWALL",
								GlobalID:   "AIg1S2wRr2WQeQMwAKN3aq",
								Name:       "site:W-01",
								ObjectType: "PartitionWall",
								Placement:  origin(),
							}, {
								Entity:     EntityProxy,
								GlobalID:   "BIg1S2wRr2WQeQMwAKN3aq",
								Name:       "site:F-01",
								ObjectType: "Fitting",
								Placement:  origin(),
							}},
						}},
					}},
				}},
			}},
			Groups: []Group{{
				GlobalID:   "CIg1S2wRr2WQeQMwAKN3aq",
				Name:       "site:C-01",
				LongName:   "West campus",
				ObjectType: "Campus",
				Assignment: "DIg1S2wRr2WQeQMwAKN3aq",
				Members:    []GlobalID{"8Ig1S2wRr2WQeQMwAKN3aq", "BIg1S2wRr2WQeQMwAKN3aq"},
			}},
		},
	}
}

// written is the fixture serialised, which every test below reads.
func written(t *testing.T, model Model) string {
	t.Helper()

	var out strings.Builder
	require.NoError(t, Write(&out, model))

	return out.String()
}

func TestWrite(t *testing.T) {
	got := written(t, fixture())

	assert.Equal(t, golden(t, "spatial.ifc", got), got,
		"the emitted file is stale; regenerate it with: go test ./ifc -update")
}

// TestWriteIsAFunctionOfTheModel is the property the golden above is holding
// still, asserted directly as well: nothing outside the model reaches a byte
// of the output.
//
// It does not discharge the golden and the golden does not discharge it. This
// catches a map ranged into the output, which Go randomises per run; the
// golden catches everything else, including a deliberate looking change to the
// bytes which nobody reviewed.
func TestWriteIsAFunctionOfTheModel(t *testing.T) {
	first := written(t, fixture())

	for range 8 {
		assert.Equal(t, first, written(t, fixture()))
	}
}

// enclosed is source wrapped in the sections an exchange file needs, so that a
// case below is the one line it is about.
func enclosed(data string) string {
	return "ISO-10303-21;\nHEADER;\nENDSEC;\nDATA;\n" + data + "\nENDSEC;\nEND-ISO-10303-21;\n"
}

// TestReadRefuses checks the reader the assertions below go through, which is
// the one thing those assertions cannot check for themselves: a reader which
// accepted anything would report every file as well formed, including the ones
// this writer got wrong.
func TestReadRefuses(t *testing.T) {
	testCases := []struct {
		name     string
		source   string
		expected string
	}{
		{
			name:     "a file which does not open as an exchange file",
			source:   "IFCPROJECT();\n",
			expected: "ISO-10303-21",
		},
		{
			name:     "an instance in the data section with no number",
			source:   enclosed("IFCDIRECTION((0.,0.,1.));"),
			expected: `'#'`,
		},
		{
			name:     "a string literal which is never closed",
			source:   enclosed("#1=IFCSITE('unterminated);"),
			expected: "a closing quote",
		},
		{
			name:     "an enumeration which is never closed",
			source:   enclosed("#1=IFCSIUNIT(*,.LENGTHUNIT"),
			expected: "a closing dot",
		},
		{
			name:     "an attribute list which is never closed",
			source:   enclosed("#1=IFCDIRECTION((0.,0.,1.);"),
			expected: "a comma or a closing parenthesis",
		},
		{
			name:     "something which is not an attribute at all",
			source:   enclosed("#1=IFCDIRECTION(!);"),
			expected: "an attribute",
		},
		{
			name:     "a real whose digits are not a number",
			source:   enclosed("#1=IFCDIRECTION((1.2.3));"),
			expected: "a real",
		},
		{
			name:     "text after the end of the file",
			source:   enclosed("#1=IFCDIRECTION((0.,0.,1.));") + "IFCPROJECT();\n",
			expected: "the end of the file",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := read(testCase.source)

			var got SyntaxError
			require.ErrorAs(t, err, &got)

			assert.Equal(t, testCase.expected, got.Want)
			assert.GreaterOrEqual(t, got.Offset, 0)
		})
	}
}

// TestReadRefusesOneInstanceNumberWrittenTwice is its own function because it
// is about a relation between two instances rather than about the text of one,
// and it carries a different field.
func TestReadRefusesOneInstanceNumberWrittenTwice(t *testing.T) {
	_, err := read(enclosed("#1=IFCDIRECTION((0.,0.,1.));\n#1=IFCDIRECTION((1.,0.,0.));"))

	var got DuplicateInstanceError
	require.ErrorAs(t, err, &got)

	assert.Equal(t, 1, got.Number)
}

func TestWriteReadsBackUnderAnIndependentReader(t *testing.T) {
	source := written(t, fixture())

	parsed, err := read(source)
	require.NoError(t, err, "the emitted file parses as an exchange file")

	t.Run("declares the schema as IFC4", func(t *testing.T) {
		var schema []item
		for _, entity := range parsed.header {
			if entity.keyword == "FILE_SCHEMA" {
				schema = entity.attributes
			}
		}

		require.Len(t, schema, 1)
		require.Equal(t, itemList, schema[0].form)
		require.Len(t, schema[0].items, 1)
		assert.Equal(t, "IFC4", schema[0].items[0].text)
	})

	t.Run("stamps the header with the epoch it was given rather than a clock", func(t *testing.T) {
		var name []item
		for _, entity := range parsed.header {
			if entity.keyword == "FILE_NAME" {
				name = entity.attributes
			}
		}

		require.Len(t, name, 7)
		assert.Equal(t, "model.ifc", name[0].text)
		assert.Equal(t, "1970-01-01T00:00:00", name[1].text)
	})

	t.Run("numbers its instances from one, ascending, without a gap", func(t *testing.T) {
		require.NotEmpty(t, parsed.order)

		for i, number := range parsed.order {
			assert.Equal(t, i+1, number)
		}
	})

	t.Run("resolves every reference it writes", func(t *testing.T) {
		var walk func(items []item)
		walk = func(items []item) {
			for _, one := range items {
				switch one.form {
				case itemReference:
					_, held := parsed.instance(one.at)
					assert.True(t, held, "#%d is referenced and not written", one.at)
				case itemList:
					walk(one.items)
				}
			}
		}

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			walk(held.attributes)
		}
	})

	t.Run("writes no owner history at all", func(t *testing.T) {
		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			assert.NotEqual(t, "IFCOWNERHISTORY", held.keyword)
		}
	})

	t.Run("leaves the owner history attribute of every rooted object absent", func(t *testing.T) {
		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if !strings.HasPrefix(held.keyword, "IFC") || !rooted(held) {
				continue
			}

			require.GreaterOrEqual(t, len(held.attributes), 2, held.keyword)
			assert.Equal(t, itemAbsent, held.attributes[1].form,
				"%s carries an owner history", held.keyword)
		}
	})

	t.Run("writes every real with a decimal point", func(t *testing.T) {
		var walk func(items []item)
		walk = func(items []item) {
			for _, one := range items {
				switch one.form {
				case itemReal:
					assert.Contains(t, one.text, ".")
				case itemList:
					walk(one.items)
				}
			}
		}

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			walk(held.attributes)
		}
	})

	t.Run("interns the points and directions it shares", func(t *testing.T) {
		points := 0
		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if held.keyword == "IFCCARTESIANPOINT" {
				points++
			}
		}

		// The world coordinate system, every element placed at the origin,
		// and the one space which is not: two points for the whole file.
		assert.Equal(t, 2, points)
	})

	t.Run("gives every rooted object an identifier of the length IFC fixes", func(t *testing.T) {
		seen := map[string]int{}

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if !rooted(held) {
				continue
			}

			require.NotEmpty(t, held.attributes, held.keyword)
			require.Equal(t, itemString, held.attributes[0].form, held.keyword)
			assert.Len(t, held.attributes[0].text, Length, held.keyword)

			seen[held.attributes[0].text]++
		}

		for id, count := range seen {
			assert.Equal(t, 1, count, "%s identifies more than one object", id)
		}
	})

	t.Run("writes each entity with the attribute count IFC4 fixes for it", func(t *testing.T) {
		// Transcribed from IFC4 rather than read off the writer's own tables,
		// which is the whole point of a second opinion.
		counts := map[string]int{
			"IFCPROJECT":                        9,
			"IFCSITE":                           14,
			"IFCBUILDING":                       12,
			"IFCBUILDINGSTOREY":                 10,
			"IFCSPACE":                          11,
			"IFCZONE":                           6,
			"IFCWALL":                           9,
			"IFCBUILDINGELEMENTPROXY":           9,
			"IFCRELAGGREGATES":                  6,
			"IFCRELCONTAINEDINSPATIALSTRUCTURE": 6,
			"IFCRELASSIGNSTOGROUP":              7,
			"IFCGEOMETRICREPRESENTATIONCONTEXT": 6,
			"IFCUNITASSIGNMENT":                 1,
			"IFCSIUNIT":                         4,
			"IFCLOCALPLACEMENT":                 2,
			"IFCAXIS2PLACEMENT3D":               3,
			"IFCCARTESIANPOINT":                 1,
			"IFCDIRECTION":                      1,
		}

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)

			want, known := counts[held.keyword]
			require.True(t, known, "%s is written and this test does not know it", held.keyword)
			assert.Len(t, held.attributes, want, "#%d=%s", number, held.keyword)
		}
	})

	t.Run("aggregates the decomposition and contains the products", func(t *testing.T) {
		aggregations, containments, assignments := 0, 0, 0

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			switch held.keyword {
			case "IFCRELAGGREGATES":
				aggregations++
			case "IFCRELCONTAINEDINSPATIALSTRUCTURE":
				containments++
			case "IFCRELASSIGNSTOGROUP":
				assignments++
			}
		}

		// Project to site, site to building, building to storey, storey to
		// space.
		assert.Equal(t, 4, aggregations)
		assert.Equal(t, 1, containments)
		assert.Equal(t, 1, assignments)
	})
}

// surveyFoot is how long a US survey foot is in metres: 1200/3937, exactly.
//
// It is the unit the fixture below is authored in because it is the one whose
// factor no decimal spelling terminates, which is what makes writing the
// conversion rather than applying it the whole point. A file which converted
// its coordinates to metres would round every one of them.
const surveyFoot = 1200.0 / 3937.0

// converted is the fixture authored in a unit the SI has no name for, with the
// plot a hundred of them east of the origin.
//
// The three conversions differ in their dimensional exponent and in their
// factor, which is what makes them a length, an area and a volume rather than
// one unit written three times. The plane angle stays an IfcSIUnit: a radian is
// a radian whatever a model's lengths are in.
func converted() Model {
	model := fixture()

	model.Units = UnitAssignment{Units: []Unit{
		ConversionBasedUnit{
			Type:       "LENGTHUNIT",
			Dimensions: DimensionalExponents{Length: 1},
			Name:       "US survey foot",
			Factor: MeasureWithUnit{
				Measure: "LENGTHMEASURE",
				Value:   surveyFoot,
				Unit:    SIUnit{Type: "LENGTHUNIT", Name: "METRE"},
			},
		},
		ConversionBasedUnit{
			Type:       "AREAUNIT",
			Dimensions: DimensionalExponents{Length: 2},
			Name:       "square US survey foot",
			Factor: MeasureWithUnit{
				Measure: "AREAMEASURE",
				Value:   surveyFoot * surveyFoot,
				Unit:    SIUnit{Type: "AREAUNIT", Name: "SQUARE_METRE"},
			},
		},
		ConversionBasedUnit{
			Type:       "VOLUMEUNIT",
			Dimensions: DimensionalExponents{Length: 3},
			Name:       "cubic US survey foot",
			Factor: MeasureWithUnit{
				Measure: "VOLUMEMEASURE",
				Value:   surveyFoot * surveyFoot * surveyFoot,
				Unit:    SIUnit{Type: "VOLUMEUNIT", Name: "CUBIC_METRE"},
			},
		},
		SIUnit{Type: "PLANEANGLEUNIT", Name: "RADIAN"},
	}}

	model.Project.Sites[0].Placement = &Placement{Location: Point{X: 100}}

	return model
}

func TestWriteConversionBasedUnits(t *testing.T) {
	got := written(t, converted())

	assert.Equal(t, golden(t, "converted.ifc", got), got,
		"the emitted file is stale; regenerate it with: go test ./ifc -update")
}

// TestWriteConversionBasedUnitsIsAFunctionOfTheModel is here for the reason
// [TestWriteIsAFunctionOfTheModel] is: the golden catches a reviewed change to
// the bytes and this catches a map ranged into them.
func TestWriteConversionBasedUnitsIsAFunctionOfTheModel(t *testing.T) {
	first := written(t, converted())

	for range 8 {
		assert.Equal(t, first, written(t, converted()))
	}
}

// TestWriteConversionBasedUnitsReadsBackUnderAnIndependentReader is the
// property the whole form exists for, asserted the way a receiving system
// would get it: read the file, take the factor it states, and multiply.
//
// Nothing here is told what a survey foot is. The scale comes out of the file,
// which is what a reader keying off the factor rather than off the name does,
// and the length it gives back is the length the model was authored with.
func TestWriteConversionBasedUnitsReadsBackUnderAnIndependentReader(t *testing.T) {
	source := written(t, converted())

	parsed, err := read(source)
	require.NoError(t, err, "the emitted file parses as an exchange file")

	length, held := conversionUnit(t, parsed, "LENGTHUNIT")
	require.True(t, held, "the assignment states a length unit as a conversion")

	t.Run("names the unit distinguishably from the other foot", func(t *testing.T) {
		assert.Equal(t, "US survey foot", length.name)
		assert.NotEqual(t, "foot", length.name,
			"the two feet differ by two parts per million, and a reader keying off the name reads one as the other")
	})

	t.Run("states the factor over the metre to the last digit the model held", func(t *testing.T) {
		assert.Equal(t, "LENGTHMEASURE", length.measure)
		assert.Equal(t, "METRE", length.over)
		assert.Equal(t, surveyFoot, length.factor)
	})

	t.Run("computes the authored length back in metres", func(t *testing.T) {
		// The plot is a hundred survey feet east of the origin, and the file
		// says so in survey feet: what a reader multiplies is the coordinate
		// it read, by the scale it read.
		site, held := parsed.instance(placementOf(t, parsed, "site:P-01"))
		require.True(t, held)

		require.Equal(t, itemList, site.attributes[0].form)
		require.Len(t, site.attributes[0].items, 3)
		assert.Equal(t, "100.", site.attributes[0].items[0].text,
			"the coordinate is the one the model was authored with, unconverted")

		easting, err := strconv.ParseFloat(site.attributes[0].items[0].text+"0", 64)
		require.NoError(t, err)

		assert.InDelta(t, 30.480060960121924, easting*length.factor, 1e-12)
	})

	t.Run("gives each of the three the dimensional exponent its quantity has", func(t *testing.T) {
		for _, testCase := range []struct {
			unit     string
			exponent string
		}{
			{unit: "LENGTHUNIT", exponent: "1"},
			{unit: "AREAUNIT", exponent: "2"},
			{unit: "VOLUMEUNIT", exponent: "3"},
		} {
			held, ok := conversionUnit(t, parsed, testCase.unit)
			require.True(t, ok, testCase.unit)

			assert.Equal(t, []string{testCase.exponent, "0", "0", "0", "0", "0", "0"}, held.exponents,
				testCase.unit)
		}
	})

	t.Run("leaves the plane angle an SI unit", func(t *testing.T) {
		assigned := assignment(t, parsed)

		var angles int
		for _, member := range assigned {
			held, ok := parsed.instance(member.at)
			require.True(t, ok)

			if held.keyword != "IFCSIUNIT" {
				continue
			}

			assert.Equal(t, "PLANEANGLEUNIT", held.attributes[1].text)
			assert.Equal(t, "RADIAN", held.attributes[3].text)
			angles++
		}

		assert.Equal(t, 1, angles, "the assignment's own SI unit is the angle and nothing else")
	})
}

// assignment is the members of the file's one IfcUnitAssignment.
func assignment(t *testing.T, parsed *file) []item {
	t.Helper()

	for _, number := range parsed.order {
		held, _ := parsed.instance(number)
		if held.keyword != "IFCUNITASSIGNMENT" {
			continue
		}

		require.Len(t, held.attributes, 1)
		require.Equal(t, itemList, held.attributes[0].form)

		return held.attributes[0].items
	}

	require.Fail(t, "the file states a unit assignment")

	return nil
}

// conversion is one IfcConversionBasedUnit as a reader which knows nothing of
// the writer finds it: the name it is known by, the exponents which say what
// quantity it measures, and the factor over the SI unit beneath it.
type conversion struct {
	name      string
	measure   string
	factor    float64
	over      string
	exponents []string
}

// conversionUnit resolves the conversion the assignment states for a quantity,
// following every reference by hand.
func conversionUnit(t *testing.T, parsed *file, quantity string) (conversion, bool) {
	t.Helper()

	for _, member := range assignment(t, parsed) {
		held, ok := parsed.instance(member.at)
		require.True(t, ok)

		if held.keyword != "IFCCONVERSIONBASEDUNIT" || held.attributes[1].text != quantity {
			continue
		}

		require.Len(t, held.attributes, 4)

		exponents, ok := parsed.instance(held.attributes[0].at)
		require.True(t, ok)
		require.Equal(t, "IFCDIMENSIONALEXPONENTS", exponents.keyword)

		out := conversion{name: held.attributes[2].text}
		for _, one := range exponents.attributes {
			require.Equal(t, itemInteger, one.form)
			out.exponents = append(out.exponents, one.text)
		}

		measure, ok := parsed.instance(held.attributes[3].at)
		require.True(t, ok)
		require.Equal(t, "IFCMEASUREWITHUNIT", measure.keyword)
		require.Len(t, measure.attributes, 2)

		require.Equal(t, itemTyped, measure.attributes[0].form)
		out.measure = strings.TrimPrefix(measure.attributes[0].text, "IFC")

		require.Len(t, measure.attributes[0].items, 1)
		require.Equal(t, itemReal, measure.attributes[0].items[0].form)

		factor, err := strconv.ParseFloat(measure.attributes[0].items[0].text, 64)
		require.NoError(t, err)
		out.factor = factor

		si, ok := parsed.instance(measure.attributes[1].at)
		require.True(t, ok)
		require.Equal(t, "IFCSIUNIT", si.keyword)
		out.over = si.attributes[3].text

		return out, true
	}

	return conversion{}, false
}

// placementOf is the instance number of the point an element's placement puts
// it at, found from the element's name.
func placementOf(t *testing.T, parsed *file, name string) int {
	t.Helper()

	for _, number := range parsed.order {
		held, _ := parsed.instance(number)
		if len(held.attributes) < 6 || held.attributes[2].text != name {
			continue
		}

		local, ok := parsed.instance(held.attributes[5].at)
		require.True(t, ok)
		require.Equal(t, "IFCLOCALPLACEMENT", local.keyword)

		axis, ok := parsed.instance(local.attributes[1].at)
		require.True(t, ok)
		require.Equal(t, "IFCAXIS2PLACEMENT3D", axis.keyword)

		return axis.attributes[0].at
	}

	require.Fail(t, "the file holds an element named "+name)

	return 0
}

// rooted reports whether a parsed instance is an IfcRoot subtype, which is
// what says its first attribute is a GlobalId and its second an owner history.
func rooted(held simple) bool {
	unrooted := []string{
		"IFCSIUNIT", "IFCUNITASSIGNMENT", "IFCCONVERSIONBASEDUNIT",
		"IFCMEASUREWITHUNIT", "IFCDIMENSIONALEXPONENTS",
		"IFCGEOMETRICREPRESENTATIONCONTEXT",
		"IFCGEOMETRICREPRESENTATIONSUBCONTEXT",
		"IFCLOCALPLACEMENT", "IFCAXIS2PLACEMENT3D", "IFCCARTESIANPOINT", "IFCDIRECTION",
		"IFCPRODUCTDEFINITIONSHAPE", "IFCSHAPEREPRESENTATION", "IFCPOLYLINE",
		"IFCARBITRARYCLOSEDPROFILEDEF", "IFCARBITRARYPROFILEDEFWITHVOIDS",
		"IFCEXTRUDEDAREASOLID", "IFCPROPERTYSINGLEVALUE", "IFCCONNECTIONCURVEGEOMETRY",
	}
	return !slices.Contains(unrooted, held.keyword)
}

func TestWriteRefuses(t *testing.T) {
	testCases := []struct {
		name     string
		model    func(model *Model)
		expected error
	}{
		{
			name:     "a spatial element written as an entity it has no attribute list for",
			model:    func(model *Model) { model.Project.Sites[0].Entity = "IFCSPATIALZONE" },
			expected: UnknownEntityError{},
		},
		{
			name: "a product written as an entity it has no attribute list for",
			model: func(model *Model) {
				space := &model.Project.Sites[0].Children[0].Children[0].Children[0]
				space.Products[0].Entity = "IFCDOOR"
			},
			expected: UnknownEntityError{},
		},
		{
			name: "a group assigning something the model does not write",
			model: func(model *Model) {
				model.Project.Groups[0].Members = []GlobalID{"ZZg1S2wRr2WQeQMwAKN3aq"}
			},
			expected: UnknownMemberError{},
		},
		{
			name:     "a rooted object with no identifier",
			model:    func(model *Model) { model.Project.Sites[0].GlobalID = "" },
			expected: MissingGlobalIDError{},
		},
		{
			name:     "a decomposition whose relationship has no identifier",
			model:    func(model *Model) { model.Project.Aggregates = "" },
			expected: MissingGlobalIDError{},
		},
		{
			name: "a containment whose relationship has no identifier",
			model: func(model *Model) {
				space := &model.Project.Sites[0].Children[0].Children[0].Children[0]
				space.Contains = ""
			},
			expected: MissingGlobalIDError{},
		},
		{
			name:     "an assignment whose relationship has no identifier",
			model:    func(model *Model) { model.Project.Groups[0].Assignment = "" },
			expected: MissingGlobalIDError{},
		},
		{
			name: "one identifier written on two objects",
			model: func(model *Model) {
				model.Project.Sites[0].GlobalID = model.Project.GlobalID
			},
			expected: DuplicateGlobalIDError{},
		},
		{
			name:     "a model which states no unit",
			model:    func(model *Model) { model.Units = UnitAssignment{} },
			expected: EmptyUnitsError{},
		},
		{
			name:     "a unit this package has no attribute list for",
			model:    func(model *Model) { model.Units = UnitAssignment{Units: []Unit{nil}} },
			expected: UnknownUnitError{},
		},
		{
			// The method closing [Unit] has a value receiver, so a pointer to
			// one satisfies the interface and is not a unit this writes. It is
			// refused rather than followed, which is the choice [Item] makes
			// too: one spelling of a unit is one thing to keep in step.
			name: "a pointer to a unit rather than the unit",
			model: func(model *Model) {
				model.Units = UnitAssignment{Units: []Unit{&SIUnit{Type: "LENGTHUNIT", Name: "METRE"}}}
			},
			expected: UnknownUnitError{},
		},
		{
			name: "a typed nil where a unit belongs",
			model: func(model *Model) {
				model.Units = UnitAssignment{Units: []Unit{(*ConversionBasedUnit)(nil)}}
			},
			expected: UnknownUnitError{},
		},
		{
			name: "a conversion factor which is not a number",
			model: func(model *Model) {
				model.Units = UnitAssignment{Units: []Unit{ConversionBasedUnit{
					Type:       "LENGTHUNIT",
					Dimensions: DimensionalExponents{Length: 1},
					Name:       "foot",
					Factor: MeasureWithUnit{
						Measure: "LENGTHMEASURE",
						Value:   math.NaN(),
						Unit:    SIUnit{Type: "LENGTHUNIT", Name: "METRE"},
					},
				}}}
			},
			expected: UnrepresentableRealError{},
		},
		{
			name: "a coordinate which is not a number",
			model: func(model *Model) {
				model.Project.Sites[0].Placement = &Placement{Location: Point{X: math.NaN()}}
			},
			expected: UnrepresentableRealError{},
		},
		{
			name: "a precision which is not finite",
			model: func(model *Model) {
				model.Context.Precision = math.Inf(1)
			},
			expected: UnrepresentableRealError{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			model := fixture()
			testCase.model(&model)

			var out strings.Builder
			err := Write(&out, model)

			require.Error(t, err)
			assert.IsType(t, testCase.expected, err)

			// An artefact is all or nothing: a refusal leaves nothing behind
			// which a later run would read as the export of this model.
			assert.Empty(t, out.String())
		})
	}
}

// TestWriteRefusalsCarryWhatMadeThem is its own function because it asserts on
// the fields of each error rather than on which error it is, which is a
// different set of assertions from the table above.
func TestWriteRefusalsCarryWhatMadeThem(t *testing.T) {
	t.Run("an unknown entity names the position and what could have gone there", func(t *testing.T) {
		model := fixture()
		model.Project.Sites[0].Entity = "IFCSPATIALZONE"

		var got UnknownEntityError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, Entity("IFCSPATIALZONE"), got.Entity)
		assert.Equal(t, "a spatial element", got.Position)
		assert.Equal(t, []Entity{EntityBuilding, EntityBuildingStorey, EntitySite, EntitySpace}, got.Known)
	})

	t.Run("an unknown member names the group and the identifier it could not find", func(t *testing.T) {
		model := fixture()
		model.Project.Groups[0].Members = []GlobalID{"ZZg1S2wRr2WQeQMwAKN3aq"}

		var got UnknownMemberError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, GlobalID("CIg1S2wRr2WQeQMwAKN3aq"), got.Group)
		assert.Equal(t, GlobalID("ZZg1S2wRr2WQeQMwAKN3aq"), got.Member)
	})

	t.Run("a duplicated identifier names the identifier", func(t *testing.T) {
		model := fixture()
		model.Project.Sites[0].GlobalID = model.Project.GlobalID

		var got DuplicateGlobalIDError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, GlobalID("0Ig1S2wRr2WQeQMwAKN3aq"), got.GlobalID)
	})

	t.Run("a relationship with no identifier names the element it belonged to", func(t *testing.T) {
		model := fixture()
		space := &model.Project.Sites[0].Children[0].Children[0].Children[0]
		space.Contains = ""

		var got MissingGlobalIDError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, Entity("IFCRELCONTAINEDINSPATIALSTRUCTURE"), got.Entity)
		assert.Equal(t, GlobalID("8Ig1S2wRr2WQeQMwAKN3aq"), got.Of)
	})

	t.Run("an unrepresentable real carries the value", func(t *testing.T) {
		model := fixture()
		model.Context.Precision = math.Inf(1)

		var got UnrepresentableRealError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.True(t, math.IsInf(got.Value, 1))
	})
}

// TestWriteReportsADestinationItCannotWrite is its own function because the
// failure is the destination's rather than the model's, and what it asserts is
// that the error comes back rather than being swallowed by the buffering.
func TestWriteReportsADestinationItCannotWrite(t *testing.T) {
	expected := errors.New("no room")

	err := Write(refusing{err: expected}, fixture())

	assert.ErrorIs(t, err, expected)
}

// refusing is a destination which fails every write.
type refusing struct{ err error }

// Write implements [io.Writer].
func (r refusing) Write(p []byte) (int, error) { return 0, r.err }

// TestWriteOmitsWhatTheCallerDidNotGive is its own function because it is
// about absence rather than about content: an optional attribute nobody
// supplied is written as absent rather than as an empty string, which is a
// different statement.
func TestWriteOmitsWhatTheCallerDidNotGive(t *testing.T) {
	model := fixture()
	model.Project.Sites[0].Name = ""
	model.Project.Sites[0].LongName = ""
	model.Project.Sites[0].Composition = ""

	parsed, err := read(written(t, model))
	require.NoError(t, err)

	for _, number := range parsed.order {
		held, _ := parsed.instance(number)
		if held.keyword != "IFCSITE" {
			continue
		}

		assert.Equal(t, itemAbsent, held.attributes[2].form, "Name")
		assert.Equal(t, itemAbsent, held.attributes[7].form, "LongName")
		assert.Equal(t, itemAbsent, held.attributes[8].form, "CompositionType")

		return
	}

	t.Fatal("the fixture holds a site")
}

// TestWritePlacesEachElementInsideItsParent is its own function because what
// it asserts is the shape of a chain rather than the content of an instance:
// a local placement points at the placement of whatever contains it, which is
// what makes moving a building move everything in it.
func TestWritePlacesEachElementInsideItsParent(t *testing.T) {
	parsed, err := read(written(t, fixture()))
	require.NoError(t, err)

	// The site is placed relative to nothing, which is the world coordinate
	// system; everything below it is placed relative to something.
	relative := map[int]bool{}
	for _, number := range parsed.order {
		held, _ := parsed.instance(number)
		if held.keyword != "IFCLOCALPLACEMENT" {
			continue
		}
		relative[number] = held.attributes[0].form == itemReference
	}

	require.NotEmpty(t, relative)

	unplaced := 0
	for _, placed := range relative {
		if !placed {
			unplaced++
		}
	}

	assert.Equal(t, 1, unplaced, "exactly one placement is relative to the world")
}

// footprint is the outline of the room in the shaped fixture below, closed by
// repeating its first point as its last.
func footprint() Polyline {
	return Polyline{Points: []Point2D{
		{X: 0, Y: 0}, {X: 6, Y: 0}, {X: 6, Y: 4}, {X: 0, Y: 4}, {X: 0, Y: 0},
	}}
}

// lightWell is the hole taken out of it, wound the other way round.
func lightWell() Polyline {
	return Polyline{Points: []Point2D{
		{X: 2, Y: 1}, {X: 2, Y: 3}, {X: 4, Y: 3}, {X: 4, Y: 1}, {X: 2, Y: 1},
	}}
}

// shaped is the fixture above with the space drawn: the outline its model
// states, the solid a viewer can draw, and where the height behind that solid
// came from.
//
// The two shapes sit in two subcontexts of one context and in one shape
// definition, which is the arrangement the whole of this geometry exists to
// make: one object, seen two ways, with nothing saying the second is the
// object's own statement about itself.
func shaped() Model {
	model := fixture()

	model.Context.Subcontexts = []Subcontext{
		{Identifier: "Body", Type: "Model", TargetView: "MODEL_VIEW"},
		{Identifier: "FootPrint", Type: "Model", TargetView: "PLAN_VIEW"},
	}

	space := &model.Project.Sites[0].Children[0].Children[0].Children[0]

	space.Representation = &Representation{
		Shapes: []Shape{{
			Context:    "FootPrint",
			Identifier: "FootPrint",
			Type:       "Curve2D",
			Items:      []Item{footprint(), lightWell()},
		}, {
			Context:    "Body",
			Identifier: "Body",
			Type:       "SweptSolid",
			Items: []Item{ExtrudedArea{
				Profile:   ArbitraryProfile{Outer: footprint(), Inner: []Polyline{lightWell()}},
				Position:  Placement{Location: Point{Z: 3}},
				Direction: Direction{Z: 1},
				Depth:     2.7,
			}},
		}},
	}

	space.Properties = []PropertySet{{
		GlobalID:    "EIg1S2wRr2WQeQMwAKN3aq",
		Defines:     "FIg1S2wRr2WQeQMwAKN3aq",
		Name:        "dfcad_HeightProvenance",
		Description: "Where the height the body was swept through came from.",
		Properties: []Property{
			{Name: "Predicate", Value: "clear-height"},
			{Name: "Source", Value: "Survey SR-2026-011, Acme Surveys"},
			{Name: "Method", Description: "The method the claim names.", Value: "method:total-station"},
		},
	}}

	return model
}

func TestWriteShapes(t *testing.T) {
	got := written(t, shaped())

	assert.Equal(t, golden(t, "shaped.ifc", got), got,
		"the emitted file is stale; regenerate it with: go test ./ifc -update")
}

// TestWriteShapesIsAFunctionOfTheModel is the byte-identity property over the
// geometry, which the golden above cannot hold on its own for the reason the
// spatial one cannot: a writer which ranged a map into a polyline would be
// stale in one run out of many rather than in all of them.
func TestWriteShapesIsAFunctionOfTheModel(t *testing.T) {
	first := written(t, shaped())

	for range 8 {
		assert.Equal(t, first, written(t, shaped()))
	}
}

func TestWriteShapesReadsBackUnderAnIndependentReader(t *testing.T) {
	source := written(t, shaped())

	parsed, err := read(source)
	require.NoError(t, err, "the emitted file parses as an exchange file")

	t.Run("writes each entity with the attribute count IFC4 fixes for it", func(t *testing.T) {
		// Transcribed from IFC4 rather than read off the writer's own tables,
		// which is the whole point of a second opinion.
		counts := map[string]int{
			"IFCGEOMETRICREPRESENTATIONSUBCONTEXT": 10,
			"IFCPRODUCTDEFINITIONSHAPE":            3,
			"IFCSHAPEREPRESENTATION":               4,
			"IFCPOLYLINE":                          1,
			"IFCARBITRARYPROFILEDEFWITHVOIDS":      4,
			"IFCEXTRUDEDAREASOLID":                 4,
			"IFCPROPERTYSET":                       5,
			"IFCPROPERTYSINGLEVALUE":               4,
			"IFCRELDEFINESBYPROPERTIES":            6,
		}

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)

			want, known := counts[held.keyword]
			if !known {
				continue
			}

			assert.Len(t, held.attributes, want, "#%d=%s", number, held.keyword)
		}
	})

	t.Run("resolves every reference it writes", func(t *testing.T) {
		var walk func(items []item)
		walk = func(items []item) {
			for _, one := range items {
				switch one.form {
				case itemReference:
					_, held := parsed.instance(one.at)
					assert.True(t, held, "#%d is referenced and not written", one.at)
				case itemList, itemTyped:
					walk(one.items)
				}
			}
		}

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			walk(held.attributes)
		}
	})

	t.Run("inherits the four attributes a subcontext takes from its parent", func(t *testing.T) {
		found := 0

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if held.keyword != "IFCGEOMETRICREPRESENTATIONSUBCONTEXT" {
				continue
			}
			found++

			for _, at := range []int{2, 3, 4, 5} {
				assert.Equal(t, itemDerived, held.attributes[at].form,
					"attribute %d of a subcontext is derived, not absent", at+1)
			}

			assert.Equal(t, itemReference, held.attributes[6].form, "ParentContext")
		}

		assert.Equal(t, 2, found)
	})

	t.Run("holds both shapes of the space in one shape definition", func(t *testing.T) {
		definitions := 0
		var shapes []int

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if held.keyword != "IFCPRODUCTDEFINITIONSHAPE" {
				continue
			}
			definitions++

			require.Equal(t, itemList, held.attributes[2].form)
			for _, one := range held.attributes[2].items {
				require.Equal(t, itemReference, one.form)
				shapes = append(shapes, one.at)
			}
		}

		require.Equal(t, 1, definitions)
		require.Len(t, shapes, 2)

		identifiers := make([]string, 0, len(shapes))
		for _, at := range shapes {
			held, ok := parsed.instance(at)
			require.True(t, ok)
			require.Equal(t, "IFCSHAPEREPRESENTATION", held.keyword)
			identifiers = append(identifiers, held.attributes[1].text)
		}

		assert.Equal(t, []string{"FootPrint", "Body"}, identifiers)
	})

	t.Run("draws the hole as an inner curve of the profile the body is swept from", func(t *testing.T) {
		found := 0

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if held.keyword != "IFCARBITRARYPROFILEDEFWITHVOIDS" {
				continue
			}
			found++

			assert.Equal(t, itemEnum, held.attributes[0].form)
			assert.Equal(t, "AREA", held.attributes[0].text)
			assert.Equal(t, itemReference, held.attributes[2].form, "OuterCurve")

			require.Equal(t, itemList, held.attributes[3].form, "InnerCurves")
			assert.Len(t, held.attributes[3].items, 1)
		}

		assert.Equal(t, 1, found)
	})

	t.Run("sweeps the profile through a positive depth along a direction", func(t *testing.T) {
		found := 0

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if held.keyword != "IFCEXTRUDEDAREASOLID" {
				continue
			}
			found++

			assert.Equal(t, itemReference, held.attributes[0].form, "SweptArea")
			assert.Equal(t, itemReference, held.attributes[1].form, "Position")
			assert.Equal(t, itemReference, held.attributes[2].form, "ExtrudedDirection")

			require.Equal(t, itemReal, held.attributes[3].form, "Depth")
			depth, err := strconv.ParseFloat(held.attributes[3].text, 64)
			require.NoError(t, err)
			assert.Positive(t, depth)
		}

		assert.Equal(t, 1, found)
	})

	t.Run("writes every point of a curve with two coordinates", func(t *testing.T) {
		planar := 0

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if held.keyword != "IFCPOLYLINE" {
				continue
			}

			require.Equal(t, itemList, held.attributes[0].form)
			require.GreaterOrEqual(t, len(held.attributes[0].items), 2)

			for _, one := range held.attributes[0].items {
				require.Equal(t, itemReference, one.form)

				point, ok := parsed.instance(one.at)
				require.True(t, ok)
				require.Equal(t, "IFCCARTESIANPOINT", point.keyword)
				require.Equal(t, itemList, point.attributes[0].form)
				assert.Len(t, point.attributes[0].items, 2)
				planar++
			}
		}

		assert.Positive(t, planar)
	})

	t.Run("names the type of every property value it writes", func(t *testing.T) {
		found := 0

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if held.keyword != "IFCPROPERTYSINGLEVALUE" {
				continue
			}
			found++

			assert.Equal(t, itemString, held.attributes[0].form, "Name")
			require.Equal(t, itemTyped, held.attributes[2].form, "NominalValue")
			assert.Equal(t, "IFCTEXT", held.attributes[2].text)
			assert.Equal(t, itemAbsent, held.attributes[3].form, "Unit")
		}

		assert.Equal(t, 3, found)
	})

	t.Run("attaches the property set to the space it was written on", func(t *testing.T) {
		sets := map[int]bool{}
		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if held.keyword == "IFCPROPERTYSET" {
				sets[number] = true
			}
		}

		require.Len(t, sets, 1)

		found := 0
		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if held.keyword != "IFCRELDEFINESBYPROPERTIES" {
				continue
			}
			found++

			require.Equal(t, itemList, held.attributes[4].form, "RelatedObjects")
			require.Len(t, held.attributes[4].items, 1)

			related, ok := parsed.instance(held.attributes[4].items[0].at)
			require.True(t, ok)
			assert.Equal(t, "IFCSPACE", related.keyword)

			require.Equal(t, itemReference, held.attributes[5].form, "RelatingPropertyDefinition")
			assert.True(t, sets[held.attributes[5].at])
		}

		assert.Equal(t, 1, found)
	})
}

func TestWriteRefusesGeometryItCannotWrite(t *testing.T) {
	testCases := []struct {
		name     string
		model    func(model *Model)
		expected error
	}{
		{
			name:     "a subcontext with no identifier",
			model:    func(model *Model) { model.Context.Subcontexts[0].Identifier = "" },
			expected: UnnamedSubcontextError{},
		},
		{
			name: "one identifier on two subcontexts",
			model: func(model *Model) {
				model.Context.Subcontexts[1].Identifier = model.Context.Subcontexts[0].Identifier
			},
			expected: DuplicateSubcontextError{},
		},
		{
			name: "a shape in a subcontext the file does not declare",
			model: func(model *Model) {
				shapedSpace(model).Representation.Shapes[0].Context = "Axis"
			},
			expected: UnknownSubcontextError{},
		},
		{
			name: "a representation holding no shapes",
			model: func(model *Model) {
				shapedSpace(model).Representation.Shapes = nil
			},
			expected: EmptyRepresentationError{},
		},
		{
			name: "a shape holding no items",
			model: func(model *Model) {
				shapedSpace(model).Representation.Shapes[0].Items = nil
			},
			expected: EmptyShapeError{},
		},
		{
			name: "a geometry this package has no attribute list for",
			model: func(model *Model) {
				shapedSpace(model).Representation.Shapes[0].Items = []Item{unwritable{}}
			},
			expected: UnknownItemError{},
		},
		{
			name: "a polyline through one point",
			model: func(model *Model) {
				shapedSpace(model).Representation.Shapes[0].Items = []Item{
					Polyline{Points: []Point2D{{X: 1, Y: 1}}},
				}
			},
			expected: ShortPolylineError{},
		},
		{
			name: "a profile whose outer curve does not close",
			model: func(model *Model) {
				solid := body(model)
				solid.Profile.Outer.Points = solid.Profile.Outer.Points[:len(solid.Profile.Outer.Points)-1]
				shapedSpace(model).Representation.Shapes[1].Items[0] = *solid
			},
			expected: OpenCurveError{},
		},
		{
			name: "a profile whose inner curve does not close",
			model: func(model *Model) {
				solid := body(model)
				solid.Profile.Inner[0].Points = solid.Profile.Inner[0].Points[:len(solid.Profile.Inner[0].Points)-1]
				shapedSpace(model).Representation.Shapes[1].Items[0] = *solid
			},
			expected: OpenCurveError{},
		},
		{
			name: "a solid swept through no depth at all",
			model: func(model *Model) {
				solid := body(model)
				solid.Depth = 0
				shapedSpace(model).Representation.Shapes[1].Items[0] = *solid
			},
			expected: NonPositiveDepthError{},
		},
		{
			name: "a solid swept through a depth which is not a number",
			model: func(model *Model) {
				solid := body(model)
				solid.Depth = math.NaN()
				shapedSpace(model).Representation.Shapes[1].Items[0] = *solid
			},
			expected: NonPositiveDepthError{},
		},
		{
			name: "a property set holding no properties",
			model: func(model *Model) {
				shapedSpace(model).Properties[0].Properties = nil
			},
			expected: EmptyPropertySetError{},
		},
		{
			name: "a property with no name",
			model: func(model *Model) {
				shapedSpace(model).Properties[0].Properties[0].Name = ""
			},
			expected: UnnamedPropertyError{},
		},
		{
			name: "a property set with no identifier",
			model: func(model *Model) {
				shapedSpace(model).Properties[0].GlobalID = ""
			},
			expected: MissingGlobalIDError{},
		},
		{
			name: "a property set nothing attaches to an object",
			model: func(model *Model) {
				shapedSpace(model).Properties[0].Defines = ""
			},
			expected: MissingGlobalIDError{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			model := shaped()
			testCase.model(&model)

			var out strings.Builder
			err := Write(&out, model)

			require.Error(t, err)
			assert.IsType(t, testCase.expected, err)

			// An artefact is all or nothing: a refusal leaves nothing behind
			// which a later run would read as the export of this model.
			assert.Empty(t, out.String())
		})
	}
}

// TestGeometryRefusalsCarryWhatMadeThem is its own function because it asserts
// on the fields of each error rather than on which error it is, which is a
// different set of assertions from the table above.
func TestGeometryRefusalsCarryWhatMadeThem(t *testing.T) {
	t.Run("an unknown subcontext names it and what could have been named instead", func(t *testing.T) {
		model := shaped()
		shapedSpace(&model).Representation.Shapes[0].Context = "Axis"

		var got UnknownSubcontextError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, "Axis", got.Context)
		assert.Equal(t, []string{"Body", "FootPrint"}, got.Known)
	})

	t.Run("an open curve names the two ends which do not meet", func(t *testing.T) {
		model := shaped()
		solid := body(&model)
		solid.Profile.Outer.Points = solid.Profile.Outer.Points[:4]
		shapedSpace(&model).Representation.Shapes[1].Items[0] = *solid

		var got OpenCurveError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, Point2D{X: 0, Y: 0}, got.First)
		assert.Equal(t, Point2D{X: 0, Y: 4}, got.Last)
		assert.False(t, got.Inner)
	})

	t.Run("a non-positive depth carries the depth", func(t *testing.T) {
		model := shaped()
		solid := body(&model)
		solid.Depth = -2.7
		shapedSpace(&model).Representation.Shapes[1].Items[0] = *solid

		var got NonPositiveDepthError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.InDelta(t, -2.7, got.Depth, 0)
	})
}

// shapedSpace is the one space the shaped fixture draws.
func shapedSpace(model *Model) *Spatial {
	return &model.Project.Sites[0].Children[0].Children[0].Children[0]
}

// body is a copy of the solid that space is drawn as, for a case which changes
// one field of it.
func body(model *Model) *ExtrudedArea {
	solid := shapedSpace(model).Representation.Shapes[1].Items[0].(ExtrudedArea)

	solid.Profile.Outer.Points = slices.Clone(solid.Profile.Outer.Points)
	solid.Profile.Inner = slices.Clone(solid.Profile.Inner)
	for i := range solid.Profile.Inner {
		solid.Profile.Inner[i].Points = slices.Clone(solid.Profile.Inner[i].Points)
	}

	return &solid
}

// unwritable is a geometry this package has no attribute list for.
//
// It is here because [Item] is closed by an unexported method rather than by
// the compiler: nothing outside this package can write one, and something
// inside it can, which is exactly the mistake the refusal it provokes is for.
type unwritable struct{}

func (unwritable) item() {}

// partyWall is the run of the room's outline the wall beside it stands along,
// which is the connection geometry of the boundary between the two.
//
// It is two dimensional for the reason a footprint is: the curve is expressed
// in the plane the space is drawn in, and repeating an elevation on every
// point of it would be a third statement of something the placement already
// carries.
func partyWall() Polyline {
	return Polyline{Points: []Point2D{{X: 0, Y: 0}, {X: 6, Y: 0}}}
}

// bounded is the fixture above with the room's boundaries stated: which
// element bounds it, on which side of the envelope, and where the two meet.
//
// The two boundaries are deliberately unalike. One names the wall, is internal
// and carries the curve the two share; the other names the proxy, is external
// and carries no geometry at all — which is the ordinary case for an exporter
// with nothing drawn, and is what makes the connection optional worth having.
func bounded() Model {
	model := fixture()

	space := &model.Project.Sites[0].Children[0].Children[0].Children[0]

	space.Boundaries = []SpaceBoundary{{
		GlobalID:   "EIg1S2wRr2WQeQMwAKN3aq",
		Name:       "geom:E-101-AB",
		Element:    "AIg1S2wRr2WQeQMwAKN3aq",
		Physical:   PhysicalBoundary,
		Internal:   BoundaryInternal,
		Connection: &ConnectionCurve{OnRelating: partyWall()},
	}, {
		GlobalID: "FIg1S2wRr2WQeQMwAKN3aq",
		Name:     "geom:E-101-BC",
		Element:  "BIg1S2wRr2WQeQMwAKN3aq",
		Physical: PhysicalBoundary,
		Internal: BoundaryExternal,
	}}

	return model
}

func TestWriteSpaceBoundaries(t *testing.T) {
	got := written(t, bounded())

	assert.Equal(t, golden(t, "bounded.ifc", got), got,
		"the emitted file is stale; regenerate it with: go test ./ifc -update")
}

// TestWriteSpaceBoundariesIsAFunctionOfTheModel is the byte-identity property
// over the relationships, which the golden cannot hold on its own for the
// reason the spatial one cannot: a writer which ranged a map into them would
// be stale in one run out of many rather than in all of them.
func TestWriteSpaceBoundariesIsAFunctionOfTheModel(t *testing.T) {
	first := written(t, bounded())

	for range 8 {
		assert.Equal(t, first, written(t, bounded()))
	}
}

func TestWriteSpaceBoundariesReadsBackUnderAnIndependentReader(t *testing.T) {
	source := written(t, bounded())

	parsed, err := read(source)
	require.NoError(t, err, "the emitted file parses as an exchange file")

	t.Run("writes each entity with the attribute count IFC4 fixes for it", func(t *testing.T) {
		// Transcribed from IFC4 rather than read off the writer's own tables,
		// which is the whole point of a second opinion.
		counts := map[string]int{
			"IFCRELSPACEBOUNDARY":        9,
			"IFCCONNECTIONCURVEGEOMETRY": 2,
			"IFCPOLYLINE":                1,
		}

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)

			want, known := counts[held.keyword]
			if !known {
				continue
			}

			assert.Len(t, held.attributes, want, "#%d=%s", number, held.keyword)
		}
	})

	t.Run("resolves every reference it writes", func(t *testing.T) {
		var walk func(items []item)
		walk = func(items []item) {
			for _, one := range items {
				switch one.form {
				case itemReference:
					_, held := parsed.instance(one.at)
					assert.True(t, held, "#%d is referenced and not written", one.at)
				case itemList:
					walk(one.items)
				}
			}
		}

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			walk(held.attributes)
		}
	})

	t.Run("relates the space to the element on either side of it", func(t *testing.T) {
		relationships := boundaries(parsed)
		require.Len(t, relationships, 2)

		space, held := parsed.instance(relationships[0].attributes[4].at)
		require.True(t, held)
		assert.Equal(t, "IFCSPACE", space.keyword)

		element, held := parsed.instance(relationships[0].attributes[5].at)
		require.True(t, held)
		assert.Equal(t, "IFCWALL", element.keyword)
	})

	t.Run("carries the classification it was given as a schema enumeration", func(t *testing.T) {
		relationships := boundaries(parsed)
		require.Len(t, relationships, 2)

		for _, at := range []int{7, 8} {
			for _, held := range relationships {
				assert.Equal(t, itemEnum, held.attributes[at].form)
			}
		}

		assert.Equal(t, "PHYSICAL", relationships[0].attributes[7].text)
		assert.Equal(t, "INTERNAL", relationships[0].attributes[8].text)
		assert.Equal(t, "EXTERNAL", relationships[1].attributes[8].text)
	})

	t.Run("writes the connection geometry it was given and omits the one it was not", func(t *testing.T) {
		relationships := boundaries(parsed)
		require.Len(t, relationships, 2)

		curve, held := parsed.instance(relationships[0].attributes[6].at)
		require.True(t, held, "the first boundary carries a connection")
		assert.Equal(t, "IFCCONNECTIONCURVEGEOMETRY", curve.keyword)

		// The curve on the related element is the one the caller did not hold,
		// which is absent rather than a copy of the other.
		assert.Equal(t, itemAbsent, curve.attributes[1].form)

		assert.Equal(t, itemAbsent, relationships[1].attributes[6].form,
			"a boundary with no geometry writes none rather than approximating one")
	})

	t.Run("writes every boundary after every element one of them may name", func(t *testing.T) {
		last := 0
		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if held.keyword == "IFCWALL" || held.keyword == "IFCBUILDINGELEMENTPROXY" {
				last = number
			}
		}

		require.NotZero(t, last)

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if held.keyword != "IFCRELSPACEBOUNDARY" {
				continue
			}
			assert.Greater(t, number, last,
				"a boundary may name any element in the file, so it is written after all of them")
		}
	})
}

// boundaries is every space boundary a parsed file holds, in the order it
// writes them.
func boundaries(parsed *file) []simple {
	var out []simple

	for _, number := range parsed.order {
		held, _ := parsed.instance(number)
		if held.keyword == "IFCRELSPACEBOUNDARY" {
			out = append(out, held)
		}
	}

	return out
}

func TestWriteRefusesASpaceBoundaryItCannotWrite(t *testing.T) {
	testCases := []struct {
		name     string
		model    func(model *Model)
		expected error
	}{
		{
			name: "a boundary on something which is not a space",
			model: func(model *Model) {
				storey := &model.Project.Sites[0].Children[0].Children[0]
				storey.Boundaries = space(model).Boundaries
			},
			expected: BoundaryOnNonSpaceError{},
		},
		{
			name:     "a boundary naming no element at all",
			model:    func(model *Model) { space(model).Boundaries[0].Element = "" },
			expected: MissingBoundaryElementError{},
		},
		{
			name: "a boundary naming an element the model does not write",
			model: func(model *Model) {
				space(model).Boundaries[0].Element = "ZZg1S2wRr2WQeQMwAKN3aq"
			},
			expected: UnknownBoundaryElementError{},
		},
		{
			name:     "a boundary which does not say whether it is physical",
			model:    func(model *Model) { space(model).Boundaries[0].Physical = "" },
			expected: UnclassifiedBoundaryError{},
		},
		{
			name:     "a boundary which does not say whether it is internal",
			model:    func(model *Model) { space(model).Boundaries[0].Internal = "" },
			expected: UnclassifiedBoundaryError{},
		},
		{
			name:     "a boundary with no identifier of its own",
			model:    func(model *Model) { space(model).Boundaries[0].GlobalID = "" },
			expected: MissingGlobalIDError{},
		},
		{
			name: "a connection curve through one point",
			model: func(model *Model) {
				space(model).Boundaries[0].Connection = &ConnectionCurve{
					OnRelating: Polyline{Points: []Point2D{{X: 1, Y: 1}}},
				}
			},
			expected: ShortPolylineError{},
		},
		{
			name: "a connection curve through a coordinate which is not a number",
			model: func(model *Model) {
				space(model).Boundaries[0].Connection = &ConnectionCurve{
					OnRelating: Polyline{Points: []Point2D{{X: math.NaN()}, {X: 1}}},
				}
			},
			expected: UnrepresentableRealError{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			model := bounded()
			testCase.model(&model)

			var out strings.Builder
			err := Write(&out, model)

			require.Error(t, err)
			assert.IsType(t, testCase.expected, err)

			// An artefact is all or nothing: a refusal leaves nothing behind
			// which a later run would read as the export of this model.
			assert.Empty(t, out.String())
		})
	}
}

// space is the one space of the fixture, which every case above reaches into.
func space(model *Model) *Spatial {
	return &model.Project.Sites[0].Children[0].Children[0].Children[0]
}

// TestSpaceBoundaryRefusalsCarryWhatMadeThem is its own function because it
// asserts on the fields of each error rather than on which error it is, which
// is a different set of assertions from the table above.
func TestSpaceBoundaryRefusalsCarryWhatMadeThem(t *testing.T) {
	t.Run("a boundary on the wrong thing names it and what it was written as", func(t *testing.T) {
		model := bounded()
		storey := &model.Project.Sites[0].Children[0].Children[0]
		storey.Boundaries = space(&model).Boundaries

		var got BoundaryOnNonSpaceError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, EntityBuildingStorey, got.Entity)
		assert.Equal(t, GlobalID("6Ig1S2wRr2WQeQMwAKN3aq"), got.Of)
	})

	t.Run("a boundary with no element names the space which stated it", func(t *testing.T) {
		model := bounded()
		space(&model).Boundaries[0].Element = ""

		var got MissingBoundaryElementError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, GlobalID("8Ig1S2wRr2WQeQMwAKN3aq"), got.Space)
		assert.Equal(t, GlobalID("EIg1S2wRr2WQeQMwAKN3aq"), got.Boundary)
	})

	t.Run("an unknown element names the identifier it could not find", func(t *testing.T) {
		model := bounded()
		space(&model).Boundaries[0].Element = "ZZg1S2wRr2WQeQMwAKN3aq"

		var got UnknownBoundaryElementError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, GlobalID("8Ig1S2wRr2WQeQMwAKN3aq"), got.Space)
		assert.Equal(t, GlobalID("EIg1S2wRr2WQeQMwAKN3aq"), got.Boundary)
		assert.Equal(t, GlobalID("ZZg1S2wRr2WQeQMwAKN3aq"), got.Element)
	})

	t.Run("an unclassified boundary names the attribute the schema wanted", func(t *testing.T) {
		model := bounded()
		space(&model).Boundaries[0].Internal = ""

		var got UnclassifiedBoundaryError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, GlobalID("EIg1S2wRr2WQeQMwAKN3aq"), got.Boundary)
		assert.Equal(t, "InternalOrExternalBoundary", got.Attribute)
	})
}

// TestWriteBoundsNothingWhereTheCallerStatedNoBoundary is its own function
// because what it asserts is an absence over a whole file: the relationship is
// written for a caller which states one and for nobody else, so a model
// unchanged from before this existed is unchanged in its bytes.
func TestWriteBoundsNothingWhereTheCallerStatedNoBoundary(t *testing.T) {
	source := written(t, fixture())

	assert.NotContains(t, source, "IFCRELSPACEBOUNDARY")
	assert.NotContains(t, source, "IFCCONNECTIONCURVEGEOMETRY")
}

// georeferenced is the fixture placed on the earth, which is the one thing a
// spatial model cannot say for itself.
//
// The definition is a fragment rather than a whole well known text string
// because what this package does with it is nothing: it is written out exactly
// as it arrives, so the only property a longer one would exercise is the string
// encoder, which has its own tests.
func georeferenced() Model {
	model := fixture()
	model.Georeference = &Georeference{
		CRS: ProjectedCRS{
			Name:        "EPSG:6543",
			Description: `PROJCS["NAD83(2011) / Louisiana South (ftUS)"]`,
		},
	}

	return model
}

func TestWriteGeoreference(t *testing.T) {
	got := written(t, georeferenced())

	assert.Equal(t, golden(t, "georeferenced.ifc", got), got,
		"the emitted file is stale; regenerate it with: go test ./ifc -update")
}

// TestWriteGeoreferenceIsAFunctionOfTheModel is the property the golden holds
// still, asserted directly for the reason [TestWriteIsAFunctionOfTheModel] is.
func TestWriteGeoreferenceIsAFunctionOfTheModel(t *testing.T) {
	first := written(t, georeferenced())

	for range 8 {
		assert.Equal(t, first, written(t, georeferenced()))
	}
}

func TestWriteGeoreferenceReadsBackUnderAnIndependentReader(t *testing.T) {
	parsed, err := read(written(t, georeferenced()))
	require.NoError(t, err)

	var crs, conversion simple
	for _, number := range parsed.order {
		held, _ := parsed.instance(number)

		switch held.keyword {
		case "IFCPROJECTEDCRS":
			crs = held
		case "IFCMAPCONVERSION":
			conversion = held
		}
	}

	require.Equal(t, "IFCPROJECTEDCRS", crs.keyword, "the file holds the projected system")
	require.Equal(t, "IFCMAPCONVERSION", conversion.keyword, "the file holds the conversion into it")

	t.Run("names the system and carries its definition verbatim", func(t *testing.T) {
		require.Len(t, crs.attributes, 7)

		assert.Equal(t, itemString, crs.attributes[0].form)
		assert.Equal(t, "EPSG:6543", crs.attributes[0].text)

		assert.Equal(t, itemString, crs.attributes[1].form)
		assert.Equal(t, `PROJCS["NAD83(2011) / Louisiana South (ftUS)"]`, crs.attributes[1].text)
	})

	t.Run("leaves every attribute the caller did not give absent", func(t *testing.T) {
		for _, at := range []int{2, 3, 4, 5, 6} {
			assert.Equal(t, itemAbsent, crs.attributes[at].form, "attribute %d", at)
		}
	})

	t.Run("converts out of the model's own representation context", func(t *testing.T) {
		require.Len(t, conversion.attributes, 8)

		require.Equal(t, itemReference, conversion.attributes[0].form)

		source, ok := parsed.instance(conversion.attributes[0].at)
		require.True(t, ok)
		assert.Equal(t, "IFCGEOMETRICREPRESENTATIONCONTEXT", source.keyword)
	})

	t.Run("converts into the system written beside it", func(t *testing.T) {
		require.Equal(t, itemReference, conversion.attributes[1].form)
		assert.Equal(t, crs.number, conversion.attributes[1].at)
	})

	t.Run("writes the identity: no offset stated, and no rotation and no scale at all", func(t *testing.T) {
		for _, at := range []int{2, 3, 4} {
			assert.Equal(t, itemReal, conversion.attributes[at].form, "attribute %d", at)
			assert.Equal(t, "0.", conversion.attributes[at].text, "attribute %d", at)
		}

		for _, at := range []int{5, 6, 7} {
			assert.Equal(t, itemAbsent, conversion.attributes[at].form, "attribute %d", at)
		}
	})
}

// TestWriteWritesNoGeoreferenceWhereTheCallerStatedNone is its own function
// because the absence is the assertion: a file nobody has sited says nothing
// about where it is, rather than saying it is at the origin of a system nobody
// named.
func TestWriteWritesNoGeoreferenceWhereTheCallerStatedNone(t *testing.T) {
	got := written(t, fixture())

	assert.NotContains(t, got, "IFCPROJECTEDCRS")
	assert.NotContains(t, got, "IFCMAPCONVERSION")
}

// TestWriteRefusesAGeoreferenceWithNoName is its own function because it is a
// refusal rather than a reading: the name is the whole of what a receiving
// system can act on, since nothing here resolves it.
func TestWriteRefusesAGeoreferenceWithNoName(t *testing.T) {
	model := fixture()
	model.Georeference = &Georeference{CRS: ProjectedCRS{Description: "PROJCS[]"}}

	err := Write(&strings.Builder{}, model)

	var got UnnamedCRSError
	require.ErrorAs(t, err, &got)
}

// TestWriteStatesAMapConversionTheCallerMeasured is its own function because
// it is about the other half of the optionality: a factor written out is one
// somebody measured, and it has to survive the write.
func TestWriteStatesAMapConversionTheCallerMeasured(t *testing.T) {
	abscissa, ordinate, scale := 0.9999985, 0.0017453, 1.0000034

	model := georeferenced()
	model.Georeference.Conversion = MapConversion{
		Eastings:         401235.117,
		Northings:        3172884.902,
		OrthogonalHeight: 44.318,
		XAxisAbscissa:    &abscissa,
		XAxisOrdinate:    &ordinate,
		Scale:            &scale,
	}

	parsed, err := read(written(t, model))
	require.NoError(t, err)

	for _, number := range parsed.order {
		held, _ := parsed.instance(number)
		if held.keyword != "IFCMAPCONVERSION" {
			continue
		}

		assert.Equal(t, "401235.117", held.attributes[2].text)
		assert.Equal(t, "3172884.902", held.attributes[3].text)
		assert.Equal(t, "44.318", held.attributes[4].text)
		assert.Equal(t, "0.9999985", held.attributes[5].text)
		assert.Equal(t, "0.0017453", held.attributes[6].text)
		assert.Equal(t, "1.0000034", held.attributes[7].text)

		return
	}

	t.Fatal("the model carries a map conversion")
}
