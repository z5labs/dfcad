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
		Units: UnitAssignment{Units: []SIUnit{
			{Type: "LENGTHUNIT", Name: "METRE"},
			{Type: "AREAUNIT", Name: "SQUARE_METRE"},
			{Type: "VOLUMEUNIT", Name: "CUBIC_METRE"},
			{Type: "PLANEANGLEUNIT", Name: "RADIAN"},
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

// rooted reports whether a parsed instance is an IfcRoot subtype, which is
// what says its first attribute is a GlobalId and its second an owner history.
func rooted(held simple) bool {
	unrooted := []string{
		"IFCSIUNIT", "IFCUNITASSIGNMENT", "IFCGEOMETRICREPRESENTATIONCONTEXT",
		"IFCLOCALPLACEMENT", "IFCAXIS2PLACEMENT3D", "IFCCARTESIANPOINT", "IFCDIRECTION",
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
