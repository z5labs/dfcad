// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package ifc

// Schema is the schema token an exchange file this package writes declares in
// its header, spelled exactly as ISO 10303-21 requires it.
//
// It is a constant rather than a field of [Header] because it is not a choice
// a caller makes: what this package writes is IFC4, attribute list by
// attribute list, and a file which declared another schema while carrying
// IFC4's entities would be read by a conforming reader as a file of a
// different format.
const Schema = "IFC4"

// Entity is the name of an IFC entity type, spelled the way an exchange file
// spells one: upper case, with no separators.
//
// Which entity a thing is is the caller's decision, so this is a string rather
// than a closed enumeration of the ones this package has constants for. What
// is closed is the set this package knows the attribute list of — see
// [UnknownEntityError] — because an entity written with the wrong number of
// attributes is a file no reader can load, and guessing is how that happens.
type Entity string

// The spatial structure entities, which are the ones a [Spatial] may be.
//
// They are IFC's spatial decomposition and they nest in this order: a site
// holds buildings, a building holds storeys, a storey holds spaces, and a
// space holds spaces. That nesting is the caller's to respect; this package
// writes the tree it is given.
const (
	EntitySite           Entity = "IFCSITE"
	EntityBuilding       Entity = "IFCBUILDING"
	EntityBuildingStorey Entity = "IFCBUILDINGSTOREY"
	EntitySpace          Entity = "IFCSPACE"
)

// EntityZone is the entity a [Group] is, and is the only one it may be.
//
// A zone is not part of the spatial decomposition: it is a group with members
// assigned to it, which is what lets one thing belong to several zones while
// sitting in exactly one storey.
const EntityZone Entity = "IFCZONE"

// EntityProxy is the entity a product whose type nothing better describes is
// written as.
//
// The IFC specification blesses it for exactly that case: a proxy is "an
// element which is not covered by a more specific entity", and its ObjectType
// is where the name of what it actually is goes. A caller with no mapping for
// something is meant to write one of these rather than to leave the thing out
// or to force it into an entity which means something else.
const EntityProxy Entity = "IFCBUILDINGELEMENTPROXY"

// Composition is IfcElementCompositionEnum: whether a spatial element is a
// thing, a part of one, or a collection of them.
//
// The empty value writes nothing, which is the encoding of an optional
// attribute that was not given.
type Composition string

// The compositions IFC defines.
const (
	CompositionComplex Composition = "COMPLEX"
	CompositionElement Composition = "ELEMENT"
	CompositionPartial Composition = "PARTIAL"
)

// Model is one IFC4 exchange file: everything which will be written, and
// nothing about how it is written.
//
// It is a value with no methods and no hidden state, so a caller can build one
// in a single pass, compare two of them, and hand the same one to [Write]
// twice for the same bytes.
type Model struct {
	// Header is what the file says about itself.
	Header Header

	// Units is what every measurement in the file is expressed in.
	Units UnitAssignment

	// Context is the geometric representation context every shape in the file
	// would be placed in. It is written whether or not anything carries a
	// shape, because IfcProject requires one.
	Context RepresentationContext

	// Georeference is where that coordinate space sits on the earth. A nil
	// georeference writes nothing at all, which is what a file nobody has
	// placed has, and is not the same as one placed at the origin of a
	// coordinate reference system nobody named.
	Georeference *Georeference

	// Project is the root of the spatial decomposition. Exactly one is
	// written: IFC allows one IfcProject per file and everything else hangs
	// off it.
	Project Project
}

// Header is the ISO 10303-21 header of the file.
//
// Every field is written exactly as it is given, including [Header.TimeStamp],
// which is a string rather than a time so that this package cannot be the
// thing which reads a clock. A caller which wants a byte-identical file for an
// unchanged model derives that stamp from the model rather than from the run.
type Header struct {
	// Description is the FILE_DESCRIPTION list, one entry per string.
	Description []string

	// Name is the FILE_NAME name: conventionally the file's own name, and
	// never an absolute path, which would put the machine the export ran on
	// into the artefact.
	Name string

	// TimeStamp is the FILE_NAME time stamp, in the zoneless ISO 8601 form
	// part 21 spells one in: 1970-01-01T00:00:00.
	TimeStamp string

	// Author and Organisation are the FILE_NAME author and organisation
	// lists, one entry per string.
	Author       []string
	Organisation []string

	// Preprocessor is what wrote the file and Originating is the system it was
	// written from. Both are free text and neither may carry a version which
	// moves with a build, or two exports of an unchanged model stop being the
	// same bytes.
	Preprocessor string
	Originating  string

	// Authorisation is the FILE_NAME authorisation field.
	Authorisation string
}

// Project is IfcProject: the root every other object in the file reaches.
type Project struct {
	// GlobalID is the identifier of the project itself.
	GlobalID GlobalID

	// Name, Description, LongName and Phase are IfcProject's text
	// attributes. An empty one is written as absent.
	Name        string
	Description string
	LongName    string
	Phase       string

	// Aggregates is the identifier of the IfcRelAggregates which joins the
	// project to Sites. It is required when there is a site and unread when
	// there is not: a relationship is a rooted object and carries an
	// identifier of its own, which the caller derives like any other.
	Aggregates GlobalID

	// Sites are the spatial structure beneath the project.
	Sites []Spatial

	// Groups are the zones the file declares. They are written beneath the
	// project because a group is not part of the spatial decomposition and
	// has nowhere else to hang.
	Groups []Group
}

// Spatial is one element of the spatial decomposition: a site, a building, a
// storey or a space.
//
// The four are one type rather than four because they differ only in the name
// they are written under and in the optional attributes which follow the ones
// they share — and this package writes those as absent. A caller which needs
// a storey's elevation is asking for a field here, not for a type of its own.
type Spatial struct {
	// Entity is which of the four this is. Anything else is
	// [UnknownEntityError].
	Entity Entity

	// GlobalID is the identifier of the element.
	GlobalID GlobalID

	// Name, Description, ObjectType and LongName are the text attributes
	// every spatial element carries. An empty one is written as absent.
	Name        string
	Description string
	ObjectType  string
	LongName    string

	// Composition is the element's IfcElementCompositionEnum. The empty
	// value writes an absent attribute.
	Composition Composition

	// Placement is where the element's own coordinate system sits inside its
	// parent's. A nil placement writes an absent ObjectPlacement, which is
	// what an element nobody has located yet has.
	Placement *Placement

	// Representation is the element's shape, expressed in the coordinate
	// system Placement establishes. A nil representation writes an absent
	// Representation, which is what an element nobody has drawn has.
	Representation *Representation

	// Properties are the property sets attached to the element, each with the
	// IfcRelDefinesByProperties which attaches it.
	//
	// They are where anything which is not geometry and not one of IFC's own
	// attributes goes: where a number came from, who measured it and how well
	// it is known. A receiving system surfaces them beside the object, which
	// is what makes a value in one distinguishable from a value somebody
	// assumed.
	Properties []PropertySet

	// Aggregates is the identifier of the IfcRelAggregates joining this
	// element to Children, and is required when there are any.
	Aggregates GlobalID

	// Children are the spatial elements decomposed out of this one.
	Children []Spatial

	// Contains is the identifier of the IfcRelContainedInSpatialStructure
	// joining this element to Products, and is required when there are any.
	Contains GlobalID

	// Products are the things contained in this element rather than
	// decomposed out of it. The distinction is IFC's and it is not cosmetic:
	// a storey is part of a building, and a wall is a thing standing in a
	// storey.
	Products []Product

	// Boundaries are the space boundary relationships this element is the
	// space of, in the order they are written.
	//
	// Only an [EntitySpace] may carry any. IfcRelSpaceBoundary's RelatingSpace
	// is an IfcSpaceBoundarySelect, and a storey is not one — a boundary
	// written on anything else is [BoundaryOnNonSpaceError] rather than an
	// instance a reader has to decide what to do with.
	//
	// Each names a [Product] written elsewhere in the same model, by the
	// identifier that product carries, exactly as a [Group]'s members do. One
	// which names an object this model does not hold is
	// [UnknownBoundaryElementError].
	Boundaries []SpaceBoundary
}

// Product is one thing standing in a spatial element.
//
// It is written under whichever entity the caller names, out of the set this
// package knows the attribute list of. A caller with nothing better to say
// writes [EntityProxy] and names what the thing is in ObjectType.
type Product struct {
	// Entity is what the product is written as.
	Entity Entity

	// GlobalID is the identifier of the product.
	GlobalID GlobalID

	// Name, Description and ObjectType are the text attributes every product
	// carries. An empty one is written as absent.
	Name        string
	Description string
	ObjectType  string

	// Placement is where the product sits inside the spatial element which
	// contains it.
	Placement *Placement

	// Representation is the product's shape, expressed in the coordinate system
	// Placement establishes. A nil representation writes an absent
	// Representation, which is what a product nobody has drawn has.
	//
	// It is the same field a [Spatial] carries and it is written the same way,
	// because the schema makes no distinction: both are IfcProduct, and the
	// attribute is at the same position in both attribute lists. A wall with a
	// body and a room with one are one mechanism rather than two.
	Representation *Representation

	// Properties are the property sets attached to the product, each with the
	// IfcRelDefinesByProperties which attaches it, exactly as a [Spatial]'s
	// are.
	//
	// They are here for the reason they are there: a shape built from a
	// measurement is worth no more than the measurement, and a receiving system
	// surfaces the set beside the object so that whoever opens the file can see
	// which.
	Properties []PropertySet
}

// SpaceBoundary is IfcRelSpaceBoundary: the relationship between a space and
// one of the elements bounding it.
//
// It is a relationship rather than a geometry, and that is what it is for. A
// wall between two rooms is one element which both of them reach, so the fact
// that they share it is a fact about the three objects and not about any
// coordinates — and a receiving system given the relationship does not have to
// recover it by comparing outlines and hoping the arithmetic agrees.
//
// [SpaceBoundary.Element] is mandatory in the schema, which is the constraint
// the whole type turns on: there is no space boundary without an element to
// name. A caller holding a boundary with nothing between the two sides has
// nothing to write here, and saying so is its business rather than this
// package's.
type SpaceBoundary struct {
	// GlobalID is the identifier of the relationship itself, which is a rooted
	// object like any other.
	GlobalID GlobalID

	// Name and Description are the relationship's text attributes. An empty
	// one is written as absent.
	Name        string
	Description string

	// Element is the identifier of the building element the space is bounded
	// by, and is required: [MissingBoundaryElementError] otherwise.
	Element GlobalID

	// Physical is whether the boundary is realised by something built or is
	// the open line between two rooms. It is required, because the schema does
	// not make it optional and a reader cannot tell the two apart from
	// anything else in the file.
	Physical PhysicalOrVirtual

	// Internal is whether the space is bounded by the element from the inside
	// of the building or from the outside of it. It is required for the reason
	// Physical is.
	Internal InternalOrExternal

	// Connection is where the two meet, and is nil where the caller holds no
	// geometry for it.
	//
	// The attribute is optional in the schema, which is what makes a purely
	// logical export a complete one: the relationship says which space is
	// bounded by which element, and a caller with no curve to draw for it says
	// nothing rather than drawing an approximation nobody asked for.
	Connection *ConnectionCurve
}

// PhysicalOrVirtual is IfcPhysicalOrVirtualEnum: whether a boundary is
// realised by something built.
type PhysicalOrVirtual string

// The members IFC defines.
//
// [BoundaryVirtual] is here because the schema has it, not because every
// caller can produce one: IFC writes a virtual boundary by naming an
// IfcVirtualElement, so a caller with no element at all has no boundary to
// write either way. Which of these a boundary is is the caller's to decide and
// never this package's to infer.
const (
	PhysicalBoundary   PhysicalOrVirtual = "PHYSICAL"
	BoundaryVirtual    PhysicalOrVirtual = "VIRTUAL"
	BoundaryNotDefined PhysicalOrVirtual = "NOTDEFINED"
)

// InternalOrExternal is IfcInternalOrExternalEnum: which side of the building
// envelope a boundary is on.
type InternalOrExternal string

// The members IFC defines.
//
// The three qualified external ones say what is on the far side of the
// boundary — the ground, water, or a fire compartment — and each is external.
const (
	BoundaryInternal      InternalOrExternal = "INTERNAL"
	BoundaryExternal      InternalOrExternal = "EXTERNAL"
	BoundaryExternalEarth InternalOrExternal = "EXTERNAL_EARTH"
	BoundaryExternalWater InternalOrExternal = "EXTERNAL_WATER"
	BoundaryExternalFire  InternalOrExternal = "EXTERNAL_FIRE"
	BoundaryUndecided     InternalOrExternal = "NOTDEFINED"
)

// ConnectionCurve is IfcConnectionCurveGeometry: where two things meet, as
// each of them holds it.
//
// There are two curves rather than one because the two objects have coordinate
// systems of their own, and the line where a room meets a wall is a different
// run of numbers in each. A caller which holds only one of them writes only
// that one, which is the ordinary case and is what the second being optional
// is for.
type ConnectionCurve struct {
	// OnRelating is the curve in the coordinate system of the space, and is
	// required.
	OnRelating Polyline

	// OnRelated is the same curve in the coordinate system of the element, and
	// is nil where the caller holds no second expression of it.
	OnRelated *Polyline
}

// Group is IfcZone: a set of things administered together, which has members
// rather than contents.
type Group struct {
	// GlobalID is the identifier of the zone.
	GlobalID GlobalID

	// Name, Description, ObjectType and LongName are the zone's text
	// attributes. An empty one is written as absent.
	Name        string
	Description string
	ObjectType  string
	LongName    string

	// Assignment is the identifier of the IfcRelAssignsToGroup which assigns
	// Members to the zone, and is required when there are any.
	Assignment GlobalID

	// Members are the identifiers of the objects assigned to the zone. Each
	// has to be an object written elsewhere in the same model — a spatial
	// element, a product or another zone — and one which is not is
	// [UnknownMemberError] rather than a dangling reference in the file.
	Members []GlobalID
}

// Placement is where one coordinate system sits inside another: IFC's
// IfcLocalPlacement over an IfcAxis2Placement3D.
//
// A placement is relative to the placement of whatever contains the thing it
// places, which is what makes moving a building move everything in it. The
// placement of a thing nothing contains is relative to the world coordinate
// system of the file.
type Placement struct {
	// Location is the origin of the placed coordinate system.
	Location Point

	// Axis is its z direction and RefDirection its x. Both are optional in
	// IFC and both are absent together far more often than not: an element
	// which is not rotated relative to its parent needs neither, and writing
	// the two default directions on every element in a model is noise in
	// every diff of it.
	Axis         *Direction
	RefDirection *Direction
}

// Point is an IfcCartesianPoint.
//
// Points are interned: two placements at the same coordinates are one instance
// in the file, and which instance that is does not depend on which of them was
// reached first in some other run.
type Point struct {
	X, Y, Z float64
}

// Direction is an IfcDirection.
//
// Directions are interned exactly as [Point] is, and for the same reason: a
// model in which everything points the same way should say so once.
type Direction struct {
	X, Y, Z float64
}

// UnitAssignment is IfcUnitAssignment: what every measurement in the file is
// expressed in.
//
// It is written whether or not anything is measured, because IfcProject
// requires it. A model with no units at all is an [EmptyUnitsError] rather
// than a file whose numbers mean nothing in particular.
type UnitAssignment struct {
	// Units are the units the file assigns, in the order they are written.
	// The order is the caller's and is preserved, because it is one of the
	// things a golden fixture of the file is holding still.
	Units []Unit
}

// Unit is one unit a [UnitAssignment] assigns.
//
// The set is closed and always will be, exactly as [Item] is and for the same
// reason: a unit this package has no attribute list for cannot be assigned, so
// an assignment which compiles is one which can be written.
type Unit interface {
	// unit is unexported so that the set stays this package's.
	unit()
}

// SIUnit is one IfcSIUnit: a quantity, the SI unit it is measured in, and the
// prefix on that unit.
type SIUnit struct {
	// Type is the IfcUnitEnum member the unit measures, spelled as IFC
	// spells it: LENGTHUNIT, AREAUNIT, VOLUMEUNIT, PLANEANGLEUNIT.
	Type string

	// Prefix is the IfcSIPrefix, spelled as IFC spells it: MILLI, KILO and
	// the rest. Empty is no prefix, which is what a metre has.
	Prefix string

	// Name is the IfcSIUnitName, spelled as IFC spells it: METRE,
	// SQUARE_METRE, CUBIC_METRE, RADIAN.
	Name string
}

func (SIUnit) unit() {}

// ConversionBasedUnit is one IfcConversionBasedUnit: a named unit stated as a
// factor over an SI one.
//
// It is how IFC4 writes a unit the SI has no name for — a foot, an inch, a
// degree — and it names the conversion rather than applying it: the file's
// numbers stay in the unit they were authored in, and the factor beside them
// says what one of that unit is. A reader multiplies by [ConversionBasedUnit]'s
// factor to get metres, which is what makes exporting a model nobody measured
// in metres lossless.
//
// The name is written for a human and the factor is written for a reader, and
// the two must not disagree: readers exist which key off the name and hold
// their own table of factors for it, so two units which differ have to be named
// differently. Writing one name over two factors is how a model ends up read at
// the wrong scale by a tool which never looked at the number.
type ConversionBasedUnit struct {
	// Type is the IfcUnitEnum member the unit measures, spelled as IFC
	// spells it: LENGTHUNIT, AREAUNIT, VOLUMEUNIT.
	Type string

	// Dimensions are the exponents of the base quantities the unit is
	// composed of. A length is (1,0,0,0,0,0,0), the area of it (2,0,…) and
	// the volume (3,0,…): the exponent is the thing which makes one
	// conversion unit a length and the next the square of that length.
	Dimensions DimensionalExponents

	// Name is the IfcLabel the unit is known by, which is what a reader shows
	// and what a careless one keys off.
	Name string

	// Factor is how much of the SI unit one of this unit is.
	Factor MeasureWithUnit
}

func (ConversionBasedUnit) unit() {}

// DimensionalExponents is IfcDimensionalExponents: the exponent of each SI
// base quantity in a derived one.
//
// The seven are the SI's own, in the schema's order. Everything this package
// writes one for is a length, an area or a volume, so only the first is ever
// other than nought — but the entity takes all seven and a file which wrote
// fewer would not load.
type DimensionalExponents struct {
	Length                   int
	Mass                     int
	Time                     int
	ElectricCurrent          int
	ThermodynamicTemperature int
	AmountOfSubstance        int
	LuminousIntensity        int
}

// MeasureWithUnit is IfcMeasureWithUnit: a number and the unit it is in.
type MeasureWithUnit struct {
	// Measure is the IfcValue member the number is written as, spelled as IFC
	// spells it without its prefix: LENGTHMEASURE, AREAMEASURE,
	// VOLUMEMEASURE.
	//
	// It has to be given because the attribute is declared as a select over
	// every measure type in the schema, so the value carries its own type
	// rather than taking one from the attribute. A bare number there is a
	// file readers reject.
	Measure string

	// Value is the number.
	Value float64

	// Unit is what that number is in.
	Unit Unit
}

// RepresentationContext is IfcGeometricRepresentationContext: the coordinate
// space every shape in the file is expressed in.
type RepresentationContext struct {
	// Identifier and Type are the context's identifier and type, which are
	// conventionally absent and "Model" respectively.
	Identifier string
	Type       string

	// Dimension is the coordinate space dimension, which is 3 for anything
	// this package writes.
	Dimension int

	// Precision is how close two points have to be to be the same point, in
	// the length unit of the assignment above. Zero writes an absent
	// attribute rather than a precision of nothing.
	Precision float64

	// World is the world coordinate system: the placement everything
	// unplaced is relative to.
	World Placement

	// TrueNorth is where north is in that coordinate system, and is absent
	// far more often than not.
	TrueNorth *Direction

	// Subcontexts are the views of this context a shape may be expressed in,
	// in the order they are written. A shape names one of them by its
	// identifier, and one which names an identifier no subcontext here carries
	// is [UnknownSubcontextError].
	//
	// They are not a formality. The plan outline of a room and the solid a
	// viewer draws it as are the same object seen two ways, and it is the
	// subcontext which says which of the two a shape is for — so a reader
	// which wants outlines can take them without also taking the solids.
	Subcontexts []Subcontext
}

// Subcontext is IfcGeometricRepresentationSubContext: one view of the
// coordinate space its parent context establishes.
//
// It inherits the dimension, the precision, the world coordinate system and
// true north from that parent, which the schema writes as derived rather than
// as absent — the values exist and are simply not this instance's to state.
type Subcontext struct {
	// Identifier is the context identifier, which is what a [Shape] names to
	// be written in it: conventionally "Body", "FootPrint", "Axis". It is
	// required, and a subcontext without one is [UnnamedSubcontextError].
	Identifier string

	// Type is the context type, which is "Model" for anything this package
	// writes.
	Type string

	// TargetView is the IfcGeometricProjectionEnum member the view is for,
	// spelled as IFC spells it: MODEL_VIEW, PLAN_VIEW, and the rest. The
	// empty value writes an absent attribute.
	TargetView string

	// UserDefinedTargetView is the name of the view where TargetView is
	// USERDEFINED, and is absent otherwise.
	UserDefinedTargetView string
}

// Georeference is where the file's coordinate space sits on the earth:
// IfcMapConversion together with the IfcProjectedCRS it converts into.
//
// The two are one type because neither is writable without the other. A map
// conversion with no target coordinate reference system converts into nothing,
// and a projected coordinate reference system nothing converts into is a name
// no reader can use.
//
// Nothing here interprets either of them. The identifier is a label and the
// definition is text, and this package neither resolves an authority code nor
// reads a projection definition — see [ProjectedCRS] for what follows from
// that.
type Georeference struct {
	// CRS is the projected coordinate reference system the conversion targets.
	CRS ProjectedCRS

	// Conversion is how a coordinate in this file's space becomes one in that
	// system.
	Conversion MapConversion
}

// ProjectedCRS is IfcProjectedCRS: the projected coordinate reference system a
// file's coordinates end up in.
//
// Every field is a string this package writes and never reads. An authority
// code is an identifier in somebody else's register, and a definition is that
// register's own text; resolving either would mean a geodetic dataset and the
// arithmetic over it, which is a product rather than a field of a struct.
type ProjectedCRS struct {
	// Name is the identifier of the system, conventionally `EPSG:6543`. It is
	// required: a coordinate reference system with no name is
	// [UnnamedCRSError] rather than a georeference nobody can act on.
	Name string

	// Description is the full definition of the system where one is held —
	// well known text, most often — written exactly as it was given. An empty
	// one is written as absent.
	Description string

	// GeodeticDatum, VerticalDatum, MapProjection and MapZone are the
	// remaining optional attributes IFC gives the entity. An empty one is
	// written as absent.
	GeodeticDatum string
	VerticalDatum string
	MapProjection string
	MapZone       string
}

// MapConversion is IfcMapConversion: the transform from this file's engineering
// coordinates into the coordinates of a [ProjectedCRS].
//
// The three offsets are required by the schema and the three factors are not.
// The distinction is load bearing rather than a convenience: an absent factor
// is the schema's own statement that there is no rotation and no scale, and a
// factor written out is one somebody measured. A writer which filled them in
// with the identity would be stating a fit nobody made.
type MapConversion struct {
	// Eastings, Northings and OrthogonalHeight are where this file's origin
	// sits in the target system. All three are written, including zero, which
	// is what the schema requires.
	Eastings         float64
	Northings        float64
	OrthogonalHeight float64

	// XAxisAbscissa and XAxisOrdinate are the direction of this file's X axis
	// in the target system. A nil one is written as absent, which the schema
	// reads as no rotation.
	XAxisAbscissa *float64
	XAxisOrdinate *float64

	// Scale is the factor between a length in this file and a length in the
	// target system. A nil one is written as absent, which the schema reads as
	// unity — and is what a file whose coordinates are already in the target
	// system has, because there is no scale in it to state.
	Scale *float64
}

// Representation is IfcProductDefinitionShape: every shape one product has.
//
// It is one definition holding several shapes rather than one shape, because
// an object is drawn more than one way and the alternatives are not
// interchangeable. A room's plan outline is what its model states; the solid
// beside it is what a viewer can draw. Keeping them in one definition is what
// says they are the same object, and keeping them apart is what says which is
// which.
type Representation struct {
	// Name and Description are the definition's text attributes. An empty one
	// is written as absent.
	Name        string
	Description string

	// Shapes are the representations the definition holds. There is at least
	// one: a definition holding none is [EmptyRepresentationError] rather
	// than an object with an empty shape.
	Shapes []Shape
}

// Shape is IfcShapeRepresentation: one way of drawing a product, in one view
// of the coordinate space.
type Shape struct {
	// Context is the identifier of the [Subcontext] the shape is expressed
	// in. The empty string is the model's own [RepresentationContext], which
	// is what a file declaring no subcontext has.
	Context string

	// Identifier is the RepresentationIdentifier: what this drawing of the
	// product is, spelled as IFC spells it — "Body", "FootPrint", "Axis".
	Identifier string

	// Type is the RepresentationType: what the items below are, spelled as
	// IFC spells it — "SweptSolid", "Curve2D", "Brep".
	Type string

	// Items are the geometry the shape is made of. There is at least one: a
	// shape holding none is [EmptyShapeError].
	Items []Item
}

// Item is one piece of geometry a [Shape] is made of.
//
// The set is closed and always will be. Every implementation is in this
// package, which is what the unexported method fixes: a caller cannot add a
// geometry this package has no attribute list for, so a shape which compiles
// is a shape which can be written.
type Item interface {
	// item is unexported so that the set stays this package's.
	item()
}

// Polyline is IfcPolyline: a run of straight segments through points in the
// plane.
//
// It carries two dimensional points because that is what the profiles and the
// plan outlines this package writes are made of. A polyline is closed by
// repeating its first point as its last, which is what an
// [ArbitraryProfile]'s curves have to do and what a plan outline of an area
// conventionally does.
type Polyline struct {
	// Points are the points it runs through, in order. There are at least
	// two: a polyline through fewer is [ShortPolylineError].
	Points []Point2D
}

func (Polyline) item() {}

// Point2D is an IfcCartesianPoint in the plane.
//
// It is a separate type from [Point] rather than a [Point] with its z ignored,
// because the two are different instances in the file and a reader tells them
// apart: a point with two coordinates is in a curve of a profile or a plan,
// and one with three is somewhere in the model. Points are interned exactly as
// [Point] is.
type Point2D struct {
	X, Y float64
}

// ArbitraryProfile is the cross section a solid is swept from:
// IfcArbitraryClosedProfileDef, or IfcArbitraryProfileDefWithVoids where it
// has holes in it.
//
// Which of the two entities is written is decided by whether there are inner
// curves, rather than by the caller naming one. They are the same profile with
// and without holes, and a caller which had to pick would be picking a schema
// entity rather than describing a shape.
type ArbitraryProfile struct {
	// Name is the profile's name, which is absent far more often than not.
	Name string

	// Outer is the curve bounding the profile. It has to be closed, and one
	// which is not is [OpenCurveError].
	Outer Polyline

	// Inner are the curves taken out of it, each closed for the same reason.
	// They are IFC's InnerCurves, and they are what a courtyard, a lift core
	// or a light well is.
	Inner []Polyline
}

// ExtrudedArea is IfcExtrudedAreaSolid: a profile swept along a direction for
// a depth.
type ExtrudedArea struct {
	// Profile is the cross section which is swept.
	Profile ArbitraryProfile

	// Position is where the profile's own plane sits in the coordinate system
	// of whatever the shape belongs to. The profile is drawn in that plane's
	// xy, so this is where a floor level goes.
	Position Placement

	// Direction is the direction the profile is swept along, expressed in
	// Position's coordinate system.
	Direction Direction

	// Depth is how far it is swept, in the file's length unit. It is strictly
	// positive: [NonPositiveDepthError] otherwise, because a solid of no
	// thickness is not a solid and IFC's own measure for it is a positive
	// length.
	Depth float64
}

func (ExtrudedArea) item() {}

// PropertySet is IfcPropertySet: named properties attached to one object.
type PropertySet struct {
	// GlobalID is the identifier of the property set itself.
	GlobalID GlobalID

	// Defines is the identifier of the IfcRelDefinesByProperties which
	// attaches the set to the object holding it. It is required, because a
	// property set nothing attaches is a set no reader reaches.
	Defines GlobalID

	// Name and Description are the set's text attributes. The name is what a
	// reader shows the set under.
	Name        string
	Description string

	// Properties are what the set holds. There is at least one: a set holding
	// none is [EmptyPropertySetError].
	Properties []Property
}

// Property is IfcPropertySingleValue whose value is an IfcText.
//
// Text rather than a choice of measure types because what these carry is
// provenance — a source, a method, a date — and a number written as text is
// still readable while a source written as a length is not writable at all. A
// property whose value is a measurement belongs in a quantity set, which is a
// different entity and a different story.
type Property struct {
	// Name is what the property is called, and is required:
	// [UnnamedPropertyError] otherwise.
	Name string

	// Description is the property's description, absent when empty.
	Description string

	// Value is the property's nominal value. An empty one writes an absent
	// NominalValue, which is a property which is present and says nothing.
	Value string
}
