// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package ifc

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupports(t *testing.T) {
	testCases := []struct {
		name     string
		entity   Entity
		expected Support
	}{
		{
			name:     "writes a wall as the entity the classification names",
			entity:   "IFCWALL",
			expected: SupportWritable,
		},
		{
			name:     "writes a door as the entity the classification names",
			entity:   "IFCDOOR",
			expected: SupportWritable,
		},
		{
			name:     "writes a window as the entity the classification names",
			entity:   "IFCWINDOW",
			expected: SupportWritable,
		},
		{
			name:     "writes the proxy it falls back to",
			entity:   EntityProxy,
			expected: SupportWritable,
		},
		{
			name:     "names a product it has no attribute list for as one",
			entity:   "IFCPILE",
			expected: SupportProduct,
		},
		{
			name:     "names a deprecated product as one IFC4 still defines",
			entity:   "IFCWALLSTANDARDCASE",
			expected: SupportProduct,
		},
		{
			name:     "names a service a house model is full of as a product",
			entity:   "IFCOUTLET",
			expected: SupportProduct,
		},
		{
			name:     "names a spatial element as a product, because IFC4 does",
			entity:   "IFCSPATIALZONE",
			expected: SupportProduct,
		},
		{
			name:     "knows no product a relationship names",
			entity:   "IFCRELSPACEBOUNDARY",
			expected: SupportUnknown,
		},
		{
			name:     "knows no product a misspelling names",
			entity:   "IFCWAHL",
			expected: SupportUnknown,
		},
		{
			name:     "knows no product a type object names",
			entity:   "IFCWALLTYPE",
			expected: SupportUnknown,
		},
		{
			name:     "knows no product an empty code names",
			entity:   "",
			expected: SupportUnknown,
		},
		{
			name:     "compares in the case an exchange file spells an entity in",
			entity:   "IfcWall",
			expected: SupportUnknown,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, Supports(testCase.entity))
		})
	}
}

// TestEveryWritableProductIsAProductIFC4Defines is its own function because it
// is a property of the two tables rather than a case in either: an entity this
// package writes which IFC4 does not define as a product is a transcription
// mistake, and it would report itself as writable while the set a registry is
// authored against disagreed.
func TestEveryWritableProductIsAProductIFC4Defines(t *testing.T) {
	defined := ProductEntities()

	for _, entity := range Products() {
		assert.Contains(t, defined, entity)
		assert.Equal(t, SupportWritable, Supports(entity))
	}
}

// TestProductEntitiesIsInNameOrder is its own function for the reason the
// ordering exists: a caller rendering the set into a message, a completion or a
// piece of documentation should get the same list every run.
func TestProductEntitiesIsInNameOrder(t *testing.T) {
	first := ProductEntities()
	second := ProductEntities()

	require.NotEmpty(t, first)
	assert.Equal(t, first, second)
	assert.True(t, slices.IsSorted(first))
}

// TestProductEntitiesCannotBeChangedByACaller is its own function because the
// set is a fact about IFC4: a caller which appended to what it was handed must
// not be able to make this package recognise an entity nobody transcribed.
func TestProductEntitiesCannotBeChangedByACaller(t *testing.T) {
	held := ProductEntities()
	held[0] = "IFCWAHL"

	assert.Equal(t, SupportUnknown, Supports("IFCWAHL"))
	assert.NotEqual(t, held, ProductEntities())
}

func TestSupportString(t *testing.T) {
	testCases := []struct {
		name     string
		support  Support
		expected string
	}{
		{name: "names what it writes", support: SupportWritable, expected: "writable"},
		{name: "names what IFC4 defines and it does not write", support: SupportProduct, expected: "unwritten"},
		{name: "names what IFC4 defines no product for", support: SupportUnknown, expected: "unknown"},
		{name: "names a value from nowhere as unknown", support: Support(42), expected: "unknown"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, testCase.support.String())
		})
	}
}

// TestWriteWritesADoorAndAWindowWithTheirOwnAttributeLists is its own function
// because what it asserts is an attribute count rather than a value: IfcDoor
// and IfcWindow each add five attributes to IfcElement where a building element
// adds one, and an instance written with a building element's tail is a file no
// reader loads.
//
// The counts are transcribed from IFC4 rather than read off the table under
// test, for the reason every assertion which goes through the reader is.
func TestWriteWritesADoorAndAWindowWithTheirOwnAttributeLists(t *testing.T) {
	testCases := []struct {
		name     string
		entity   Entity
		expected int
	}{
		{
			// GlobalId, OwnerHistory, Name, Description, ObjectType,
			// ObjectPlacement, Representation, Tag, OverallHeight,
			// OverallWidth, PredefinedType, OperationType,
			// UserDefinedOperationType.
			name:     "writes a door with the thirteen attributes IFC4 gives one",
			entity:   "IFCDOOR",
			expected: 13,
		},
		{
			// The same, with PartitioningType and
			// UserDefinedPartitioningType in place of the operation pair.
			name:     "writes a window with the thirteen attributes IFC4 gives one",
			entity:   "IFCWINDOW",
			expected: 13,
		},
		{
			// GlobalId, OwnerHistory, Name, Description, ObjectType,
			// ObjectPlacement, Representation, Tag, PredefinedType.
			name:     "writes a wall with the nine attributes IFC4 gives one",
			entity:   "IFCWALL",
			expected: 9,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			model := fixture()
			space := &model.Project.Sites[0].Children[0].Children[0].Children[0]
			space.Products[0].Entity = testCase.entity

			parsed, err := read(written(t, model))
			require.NoError(t, err)

			for _, number := range parsed.order {
				held, _ := parsed.instance(number)
				if held.keyword != string(testCase.entity) {
					continue
				}

				assert.Len(t, held.attributes, testCase.expected)

				return
			}

			require.Fail(t, "the file holds a "+string(testCase.entity))
		})
	}
}
