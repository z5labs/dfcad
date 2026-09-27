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

// cased is the bodied fixture with a cased opening in its wall: a doorway with
// no door, which is a product of the model in its own right rather than the
// void behind something standing in it.
//
// The opening stands in the room like any product, with its own placement and
// its own body, and the one thing written for it beyond that is the
// relationship saying which element it voids.
func cased() Model {
	model := bodied()

	space := &model.Project.Sites[0].Children[0].Children[0].Children[0]

	space.Products = append(space.Products, Product{
		Entity:     "IFCOPENINGELEMENT",
		GlobalID:   "SIg1S2wRr2WQeQMwAKN3aq",
		Name:       "site:O-01",
		ObjectType: "opening",
		Placement:  origin(),
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
	})

	model.Project.Voids = []Void{{
		GlobalID: "TIg1S2wRr2WQeQMwAKN3aq",
		Host:     "AIg1S2wRr2WQeQMwAKN3aq",
		Opening:  "SIg1S2wRr2WQeQMwAKN3aq",
	}}

	return model
}

func TestWriteVoids(t *testing.T) {
	got := written(t, cased())

	assert.Equal(t, golden(t, "cased.ifc", got), got,
		"the emitted file is stale; regenerate it with: go test ./ifc -update")
}

// TestWriteVoidsIsAFunctionOfTheModel is the byte-identity property over the
// voids, held for the reason it is held over everything else this package
// writes.
func TestWriteVoidsIsAFunctionOfTheModel(t *testing.T) {
	first := written(t, cased())

	for range 8 {
		assert.Equal(t, first, written(t, cased()))
	}
}

func TestWriteVoidsReadsBackUnderAnIndependentReader(t *testing.T) {
	source := written(t, cased())

	parsed, err := read(source)
	require.NoError(t, err, "the emitted file parses as an exchange file")

	every := func(keyword string) []int {
		var numbers []int
		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if held.keyword == keyword {
				numbers = append(numbers, number)
			}
		}
		return numbers
	}

	named := func(t *testing.T, name string) int {
		t.Helper()

		for _, number := range parsed.order {
			held, _ := parsed.instance(number)
			if rooted(held) && len(held.attributes) > 2 && held.attributes[2].text == name {
				return number
			}
		}

		require.Fail(t, "the file holds an object named "+name)
		return 0
	}

	t.Run("writes the opening as the one opening element in the file", func(t *testing.T) {
		openings := every("IFCOPENINGELEMENT")

		require.Len(t, openings, 1, "no second void is cut for the opening to stand in")
		assert.Equal(t, named(t, "site:O-01"), openings[0])
	})

	t.Run("voids the wall with the opening", func(t *testing.T) {
		voids := every("IFCRELVOIDSELEMENT")
		require.Len(t, voids, 1)

		held, _ := parsed.instance(voids[0])
		require.Len(t, held.attributes, 6, "IFC4 fixes six attributes on IfcRelVoidsElement")
		assert.Equal(t, "TIg1S2wRr2WQeQMwAKN3aq", held.attributes[0].text)
		assert.Equal(t, named(t, "site:W-01"), held.attributes[4].at, "RelatingBuildingElement is the wall")
		assert.Equal(t, named(t, "site:O-01"), held.attributes[5].at, "RelatedOpeningElement is the opening")
	})

	t.Run("fills it with nothing", func(t *testing.T) {
		assert.Empty(t, every("IFCRELFILLSELEMENT"))
	})

	t.Run("keeps the opening contained in the room it stands in", func(t *testing.T) {
		contains := every("IFCRELCONTAINEDINSPATIALSTRUCTURE")
		require.Len(t, contains, 1)

		held, _ := parsed.instance(contains[0])

		var contained []int
		for _, one := range held.attributes[4].items {
			contained = append(contained, one.at)
		}

		assert.Contains(t, contained, named(t, "site:O-01"))
	})

	t.Run("writes the void after both products it names", func(t *testing.T) {
		voids := every("IFCRELVOIDSELEMENT")
		require.Len(t, voids, 1)

		assert.Greater(t, voids[0], named(t, "site:W-01"))
		assert.Greater(t, voids[0], named(t, "site:O-01"))
	})
}

// TestWriteLeavesAModelWithNoVoidAsItWas is the other half of the golden
// above: the field is new, and a model which sets none is written to exactly
// the bytes it was written to before there was one.
func TestWriteLeavesAModelWithNoVoidAsItWas(t *testing.T) {
	got := written(t, voided())

	assert.Equal(t, golden(t, "voided.ifc", got), got)
}

func TestWriteRefusesAVoidItCannotWrite(t *testing.T) {
	testCases := []struct {
		name     string
		model    func(model *Model)
		expected error
	}{
		{
			name:     "a void naming no element to void",
			model:    func(model *Model) { model.Project.Voids[0].Host = "" },
			expected: MissingOpeningHostError{},
		},
		{
			name:     "a void of an element the model does not write",
			model:    func(model *Model) { model.Project.Voids[0].Host = "ZZg1S2wRr2WQeQMwAKN3aq" },
			expected: UnknownOpeningElementError{},
		},
		{
			name:     "a void of a space, which is not an element",
			model:    func(model *Model) { model.Project.Voids[0].Host = "8Ig1S2wRr2WQeQMwAKN3aq" },
			expected: UnknownOpeningElementError{},
		},
		{
			name:     "a void of an annotation, which is not an element",
			model:    func(model *Model) { model.Project.Voids[0].Host = annotated(model) },
			expected: UnknownOpeningElementError{},
		},
		{
			name:     "a void of the opening itself, which IFC4 never voids",
			model:    func(model *Model) { model.Project.Voids[0].Host = "SIg1S2wRr2WQeQMwAKN3aq" },
			expected: MisplacedOpeningError{},
		},
		{
			name:     "a void whose opening the model does not write",
			model:    func(model *Model) { model.Project.Voids[0].Opening = "ZZg1S2wRr2WQeQMwAKN3aq" },
			expected: UnknownOpeningElementError{},
		},
		{
			name:     "a void whose opening is written as something other than an opening",
			model:    func(model *Model) { model.Project.Voids[0].Opening = "AIg1S2wRr2WQeQMwAKN3aq" },
			expected: MisplacedOpeningError{},
		},
		{
			name:     "a void whose relationship has no identifier",
			model:    func(model *Model) { model.Project.Voids[0].GlobalID = "" },
			expected: MissingGlobalIDError{},
		},
		{
			name: "an opening voiding two elements",
			model: func(model *Model) {
				space := &model.Project.Sites[0].Children[0].Children[0].Children[0]
				model.Project.Voids = append(model.Project.Voids, Void{
					GlobalID: "UIg1S2wRr2WQeQMwAKN3aq",
					Host:     space.Products[1].GlobalID,
					Opening:  "SIg1S2wRr2WQeQMwAKN3aq",
				})
			},
			expected: OpeningVoidsTwiceError{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			model := cased()
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

// TestWriteRefusesAnOpeningStandingWhereIFC4AllowsNone is its own function
// because the model it starts from is a different one: an [Opening] cut for
// something standing in it, naming a product written as an opening at one of
// its ends.
func TestWriteRefusesAnOpeningStandingWhereIFC4AllowsNone(t *testing.T) {
	withAnOpeningProduct := func() Model {
		model := voided()
		space := &model.Project.Sites[0].Children[0].Children[0].Children[0]
		space.Products = append(space.Products, Product{
			Entity:    "IFCOPENINGELEMENT",
			GlobalID:  "SIg1S2wRr2WQeQMwAKN3aq",
			Name:      "site:O-01",
			Placement: origin(),
		})
		return model
	}

	testCases := []struct {
		name      string
		model     func(model *Model)
		attribute string
	}{
		{
			name:      "an opening cut through an opening",
			model:     func(model *Model) { model.Project.Openings[0].Host = "SIg1S2wRr2WQeQMwAKN3aq" },
			attribute: "RelatingBuildingElement",
		},
		{
			name:      "an opening filled by an opening",
			model:     func(model *Model) { model.Project.Openings[0].Filling = "SIg1S2wRr2WQeQMwAKN3aq" },
			attribute: "RelatedBuildingElement",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			model := withAnOpeningProduct()
			testCase.model(&model)

			var out strings.Builder
			err := Write(&out, model)

			var got MisplacedOpeningError
			require.ErrorAs(t, err, &got)
			assert.Equal(t, GlobalID("MIg1S2wRr2WQeQMwAKN3aq"), got.Opening)
			assert.Equal(t, testCase.attribute, got.Attribute)
			assert.Equal(t, GlobalID("SIg1S2wRr2WQeQMwAKN3aq"), got.Element)
			assert.Equal(t, Entity("IFCOPENINGELEMENT"), got.Entity)

			assert.Empty(t, out.String())
		})
	}
}

// TestVoidRefusalsCarryWhatMadeThem asserts on the fields of each error,
// which is a different set of assertions from the table above.
func TestVoidRefusalsCarryWhatMadeThem(t *testing.T) {
	t.Run("an opening written as something else names what it was written as", func(t *testing.T) {
		model := cased()
		model.Project.Voids[0].Opening = "AIg1S2wRr2WQeQMwAKN3aq"

		var got MisplacedOpeningError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, "RelatedOpeningElement", got.Attribute)
		assert.Equal(t, GlobalID("AIg1S2wRr2WQeQMwAKN3aq"), got.Element)
		assert.NotEqual(t, Entity("IFCOPENINGELEMENT"), got.Entity)
	})

	t.Run("an opening voiding two elements names the opening and both", func(t *testing.T) {
		model := cased()
		space := &model.Project.Sites[0].Children[0].Children[0].Children[0]
		model.Project.Voids = append(model.Project.Voids, Void{
			GlobalID: "UIg1S2wRr2WQeQMwAKN3aq",
			Host:     space.Products[1].GlobalID,
			Opening:  "SIg1S2wRr2WQeQMwAKN3aq",
		})

		var got OpeningVoidsTwiceError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, GlobalID("SIg1S2wRr2WQeQMwAKN3aq"), got.Opening)
		assert.Equal(t, GlobalID("AIg1S2wRr2WQeQMwAKN3aq"), got.First)
		assert.Equal(t, space.Products[1].GlobalID, got.Second)
	})

	t.Run("a relationship with no identifier names the opening it belonged to", func(t *testing.T) {
		model := cased()
		model.Project.Voids[0].GlobalID = ""

		var got MissingGlobalIDError
		require.ErrorAs(t, Write(&strings.Builder{}, model), &got)

		assert.Equal(t, Entity("IFCRELVOIDSELEMENT"), got.Entity)
		assert.Equal(t, GlobalID("SIg1S2wRr2WQeQMwAKN3aq"), got.Of)
	})
}
