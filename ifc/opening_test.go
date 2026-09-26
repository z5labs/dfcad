// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package ifc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// doorway is the plan of the void cut through the wall of the bodied fixture:
// a metre of its run, as wide as the wall is thick.
func doorway() Polyline {
	return Polyline{Points: []Point2D{
		{X: 1, Y: -0.05}, {X: 2, Y: -0.05}, {X: 2, Y: 0.05}, {X: 1, Y: 0.05}, {X: 1, Y: -0.05},
	}}
}

// voided is the bodied fixture with a door set in its wall and a part
// decomposed out of its proxy.
//
// The two are the two things one element standing inside another can be. The
// door stands in the room like any product and fills an opening cut through
// the wall; the baluster is a part of the fitting, and reaches the storey only
// through it. The opening is written after both, because it names the wall and
// the door by identifier and either could stand anywhere in the file.
func voided() Model {
	model := bodied()

	space := &model.Project.Sites[0].Children[0].Children[0].Children[0]

	space.Products = append(space.Products, Product{
		Entity:     "IFCDOOR",
		GlobalID:   "JIg1S2wRr2WQeQMwAKN3aq",
		Name:       "site:D-01",
		ObjectType: "Doorset",
		Placement:  origin(),
	})

	fitting := &space.Products[1]
	fitting.Aggregates = "KIg1S2wRr2WQeQMwAKN3aq"
	fitting.Parts = []Product{{
		Entity:     "IFCMEMBER",
		GlobalID:   "LIg1S2wRr2WQeQMwAKN3aq",
		Name:       "site:F-01-A",
		ObjectType: "Baluster",
		Placement:  origin(),
	}}

	model.Project.Openings = []Opening{{
		GlobalID:  "MIg1S2wRr2WQeQMwAKN3aq",
		Host:      "AIg1S2wRr2WQeQMwAKN3aq",
		Voids:     "NIg1S2wRr2WQeQMwAKN3aq",
		Filling:   "JIg1S2wRr2WQeQMwAKN3aq",
		Fills:     "OIg1S2wRr2WQeQMwAKN3aq",
		Placement: origin(),
		Representation: &Representation{Shapes: []Shape{{
			Context:    "Body",
			Identifier: "Body",
			Type:       "SweptSolid",
			Items: []Item{ExtrudedArea{
				Profile:   ArbitraryProfile{Outer: doorway()},
				Position:  Placement{Location: Point{Z: 3}},
				Direction: Direction{Z: 1},
				Depth:     2.1,
			}},
		}}},
	}}

	return model
}

func TestWriteOpenings(t *testing.T) {
	got := written(t, voided())

	assert.Equal(t, golden(t, "voided.ifc", got), got,
		"the emitted file is stale; regenerate it with: go test ./ifc -update")
}

// TestWriteOpeningsIsAFunctionOfTheModel is the byte-identity property over
// the openings and the parts, held for the reason it is held over everything
// else this package writes.
func TestWriteOpeningsIsAFunctionOfTheModel(t *testing.T) {
	first := written(t, voided())

	for range 8 {
		assert.Equal(t, first, written(t, voided()))
	}
}

// TestWriteLeavesAModelWithNoOpeningAndNoPartAsItWas is the other half of the
// golden above: the fields are new, and a model which sets neither is written
// to exactly the bytes it was written to before there were any.
func TestWriteLeavesAModelWithNoOpeningAndNoPartAsItWas(t *testing.T) {
	got := written(t, bodied())

	assert.Equal(t, golden(t, "bodied.ifc", got), got)
	assert.NotContains(t, got, "IFCOPENINGELEMENT")
}

func TestWriteOpeningsReadsBackUnderAnIndependentReader(t *testing.T) {
	source := written(t, voided())

	parsed, err := read(source)
	require.NoError(t, err, "the emitted file parses as an exchange file")

	named := func(t *testing.T, name string) (int, simple) {
		t.Helper()

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if rooted(held) && len(held.attributes) > 2 && held.attributes[2].text == name {
				return number, held
			}
		}

		require.Fail(t, "the file holds an object named "+name)
		return 0, simple{}
	}

	only := func(t *testing.T, keyword string) (int, simple) {
		t.Helper()

		var numbers []int
		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if held.keyword == keyword {
				numbers = append(numbers, number)
			}
		}

		require.Len(t, numbers, 1, "the file holds one %s", keyword)

		held, _ := parsed.instance(numbers[0])
		return numbers[0], held
	}

	t.Run("writes each entity with the attribute count IFC4 fixes for it", func(t *testing.T) {
		// Transcribed from IFC4 rather than read off the writer's own tables,
		// which is the whole point of a second opinion.
		counts := map[string]int{
			"IFCOPENINGELEMENT":  9,
			"IFCRELVOIDSELEMENT": 6,
			"IFCRELFILLSELEMENT": 6,
			"IFCRELAGGREGATES":   6,
			"IFCDOOR":            13,
			"IFCMEMBER":          9,
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

	t.Run("voids the wall with the opening", func(t *testing.T) {
		wall, _ := named(t, "site:W-01")
		opening, _ := only(t, "IFCOPENINGELEMENT")
		_, voids := only(t, "IFCRELVOIDSELEMENT")

		assert.Equal(t, wall, voids.attributes[4].at, "RelatingBuildingElement is the wall")
		assert.Equal(t, opening, voids.attributes[5].at, "RelatedOpeningElement is the opening")
	})

	t.Run("fills the opening with the door", func(t *testing.T) {
		door, _ := named(t, "site:D-01")
		opening, _ := only(t, "IFCOPENINGELEMENT")
		_, fills := only(t, "IFCRELFILLSELEMENT")

		assert.Equal(t, opening, fills.attributes[4].at, "RelatingOpeningElement is the opening")
		assert.Equal(t, door, fills.attributes[5].at, "RelatedBuildingElement is the door")
	})

	t.Run("states the opening goes through its host", func(t *testing.T) {
		_, opening := only(t, "IFCOPENINGELEMENT")

		assert.Equal(t, itemEnum, opening.attributes[8].form)
		assert.Equal(t, "OPENING", opening.attributes[8].text)
	})

	t.Run("places the opening relative to the wall it voids", func(t *testing.T) {
		_, wall := named(t, "site:W-01")
		_, opening := only(t, "IFCOPENINGELEMENT")

		local, held := parsed.instance(opening.attributes[5].at)
		require.True(t, held)
		require.Equal(t, "IFCLOCALPLACEMENT", local.keyword)

		assert.Equal(t, wall.attributes[5].at, local.attributes[0].at,
			"PlacementRelTo is the wall's own placement")
	})

	t.Run("gives the opening the body it was given", func(t *testing.T) {
		_, opening := only(t, "IFCOPENINGELEMENT")

		shape, held := parsed.instance(opening.attributes[6].at)
		require.True(t, held)
		assert.Equal(t, "IFCPRODUCTDEFINITIONSHAPE", shape.keyword)
	})

	t.Run("keeps the door contained in the room it stands in", func(t *testing.T) {
		door, _ := named(t, "site:D-01")
		_, contains := only(t, "IFCRELCONTAINEDINSPATIALSTRUCTURE")

		var contained []int
		for _, one := range contains.attributes[4].items {
			contained = append(contained, one.at)
		}

		assert.Contains(t, contained, door)
	})

	t.Run("relates a part to the storey only through its whole", func(t *testing.T) {
		fitting, _ := named(t, "site:F-01")
		baluster, _ := named(t, "site:F-01-A")
		_, contains := only(t, "IFCRELCONTAINEDINSPATIALSTRUCTURE")

		var contained []int
		for _, one := range contains.attributes[4].items {
			contained = append(contained, one.at)
		}

		assert.Contains(t, contained, fitting)
		assert.NotContains(t, contained, baluster, "a part is not contained in the spatial structure as well")

		var aggregated bool
		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if held.keyword != "IFCRELAGGREGATES" || held.attributes[4].at != fitting {
				continue
			}

			require.Len(t, held.attributes[5].items, 1)
			assert.Equal(t, baluster, held.attributes[5].items[0].at)
			aggregated = true
		}

		assert.True(t, aggregated, "the fitting aggregates the baluster")
	})

	t.Run("writes every opening after every element one of them may name", func(t *testing.T) {
		door, _ := named(t, "site:D-01")
		opening, _ := only(t, "IFCOPENINGELEMENT")

		assert.Greater(t, opening, door)
	})
}

func TestWriteRefusesAnOpeningItCannotWrite(t *testing.T) {
	testCases := []struct {
		name     string
		model    func(model *Model)
		expected error
	}{
		{
			name:     "an opening voiding nothing",
			model:    func(model *Model) { model.Project.Openings[0].Host = "" },
			expected: MissingOpeningHostError{},
		},
		{
			name:     "an opening voiding an element the model does not write",
			model:    func(model *Model) { model.Project.Openings[0].Host = "ZZg1S2wRr2WQeQMwAKN3aq" },
			expected: UnknownOpeningElementError{},
		},
		{
			name: "an opening voiding a space, which is not an element",
			model: func(model *Model) {
				model.Project.Openings[0].Host = "8Ig1S2wRr2WQeQMwAKN3aq"
			},
			expected: UnknownOpeningElementError{},
		},
		{
			name:     "an opening filled by an element the model does not write",
			model:    func(model *Model) { model.Project.Openings[0].Filling = "ZZg1S2wRr2WQeQMwAKN3aq" },
			expected: UnknownOpeningElementError{},
		},
		{
			name:     "a voiding whose relationship has no identifier",
			model:    func(model *Model) { model.Project.Openings[0].Voids = "" },
			expected: MissingGlobalIDError{},
		},
		{
			name:     "a filling whose relationship has no identifier",
			model:    func(model *Model) { model.Project.Openings[0].Fills = "" },
			expected: MissingGlobalIDError{},
		},
		{
			name: "an aggregation of parts whose relationship has no identifier",
			model: func(model *Model) {
				space := &model.Project.Sites[0].Children[0].Children[0].Children[0]
				space.Products[1].Aggregates = ""
			},
			expected: MissingGlobalIDError{},
		},
		{
			name: "a part written as an entity it has no attribute list for",
			model: func(model *Model) {
				space := &model.Project.Sites[0].Children[0].Children[0].Children[0]
				space.Products[1].Parts[0].Entity = "IFCPILE"
			},
			expected: UnknownEntityError{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			model := voided()
			testCase.model(&model)

			var out strings.Builder
			err := Write(&out, model)

			require.Error(t, err)
			assert.IsType(t, testCase.expected, err)

			// An artefact is all or nothing.
			assert.Empty(t, out.String())
		})
	}
}

// TestWriteWritesAVoidNothingFills is its own function because what it asserts
// is an absence: a void with no filling is written with the one relationship it
// has, and no IfcRelFillsElement pointing at nothing.
func TestWriteWritesAVoidNothingFills(t *testing.T) {
	model := voided()
	model.Project.Openings[0].Filling = ""
	model.Project.Openings[0].Fills = ""

	got := written(t, model)

	assert.Contains(t, got, "IFCOPENINGELEMENT(")
	assert.Contains(t, got, "IFCRELVOIDSELEMENT(")
	assert.NotContains(t, got, "IFCRELFILLSELEMENT(")
}

// TestOpeningRefusalsCarryWhatMadeThem asserts on the fields of each error,
// which is a different set of assertions from the table above.
func TestOpeningRefusalsCarryWhatMadeThem(t *testing.T) {
	t.Run("an opening voiding nothing names the opening", func(t *testing.T) {
		model := voided()
		model.Project.Openings[0].Host = ""

		var got MissingOpeningHostError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, GlobalID("MIg1S2wRr2WQeQMwAKN3aq"), got.Opening)
	})

	t.Run("an unknown host names the opening, the attribute and what it named", func(t *testing.T) {
		model := voided()
		model.Project.Openings[0].Host = "8Ig1S2wRr2WQeQMwAKN3aq"

		var got UnknownOpeningElementError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, GlobalID("MIg1S2wRr2WQeQMwAKN3aq"), got.Opening)
		assert.Equal(t, "RelatingBuildingElement", got.Attribute)
		assert.Equal(t, GlobalID("8Ig1S2wRr2WQeQMwAKN3aq"), got.Element)
	})

	t.Run("an unknown filling names the other attribute", func(t *testing.T) {
		model := voided()
		model.Project.Openings[0].Filling = "ZZg1S2wRr2WQeQMwAKN3aq"

		var got UnknownOpeningElementError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, "RelatedBuildingElement", got.Attribute)
		assert.Equal(t, GlobalID("ZZg1S2wRr2WQeQMwAKN3aq"), got.Element)
	})

	t.Run("a relationship with no identifier names the opening it belonged to", func(t *testing.T) {
		model := voided()
		model.Project.Openings[0].Fills = ""

		var got MissingGlobalIDError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, Entity("IFCRELFILLSELEMENT"), got.Entity)
		assert.Equal(t, GlobalID("MIg1S2wRr2WQeQMwAKN3aq"), got.Of)
	})

	t.Run("an aggregation with no identifier names the whole", func(t *testing.T) {
		model := voided()
		model.Project.Sites[0].Children[0].Children[0].Children[0].Products[1].Aggregates = ""

		var got MissingGlobalIDError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, Entity("IFCRELAGGREGATES"), got.Entity)
		assert.Equal(t, GlobalID("BIg1S2wRr2WQeQMwAKN3aq"), got.Of)
	})
}
