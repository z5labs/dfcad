// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package ifc

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"strconv"
)

// The entities this package knows the attribute list of, and the number of
// optional attributes each writes as absent after the ones its family shares.
//
// A table rather than a switch because that is what it is: the schema fixes an
// attribute list per entity, this package writes the shared head of each
// family from the values it was given and the tail as absent, and the only
// thing which varies is how long that tail is. Adding an entity is a line
// here, which is the point — a caller wanting IfcSpatialZone should not have
// to fork a serialiser.
//
// Nothing in either table is a judgement about a model. They are IFC4's
// attribute lists, transcribed.
var (
	// spatialElements are the IfcSpatialStructureElement subtypes, whose
	// shared head ends at CompositionType.
	spatialElements = map[Entity]int{
		// RefLatitude, RefLongitude, RefElevation, LandTitleNumber,
		// SiteAddress.
		EntitySite: 5,
		// ElevationOfRefHeight, ElevationOfTerrain, BuildingAddress.
		EntityBuilding: 3,
		// Elevation.
		EntityBuildingStorey: 1,
		// PredefinedType, ElevationWithFlooring.
		EntitySpace: 2,
	}

	// products are the IfcElement subtypes whose attribute list ends
	// `..., Tag, PredefinedType` — which is most of the building elements,
	// and is what makes one table entry enough for each of them.
	//
	// The ones which do not are absent on purpose rather than by oversight:
	// IfcDoor and IfcWindow carry an overall height, an overall width and an
	// operation type, and IfcPile carries a construction type. Writing one of
	// those from this table would produce an instance with the wrong number
	// of attributes, so each is its own entry the day somebody needs it.
	products = map[Entity]int{
		EntityProxy:      1,
		"IFCBEAM":        1,
		"IFCCOLUMN":      1,
		"IFCCOVERING":    1,
		"IFCCURTAINWALL": 1,
		"IFCFOOTING":     1,
		"IFCMEMBER":      1,
		"IFCPLATE":       1,
		"IFCRAILING":     1,
		"IFCRAMP":        1,
		"IFCROOF":        1,
		"IFCSLAB":        1,
		"IFCSTAIR":       1,
		"IFCWALL":        1,
	}
)

// Products is every entity this package can write a [Product] as, in name
// order.
//
// It is exported because a caller mapping its own vocabulary onto IFC has to
// be able to ask before it maps: a classification naming an entity this
// package has no attribute list for is a thing to write as [EntityProxy], and
// finding that out from a refusal after the whole model has been built is
// finding it out too late to do anything but fail.
func Products() []Entity { return keys(products) }

// SpatialElements is every entity this package can write a [Spatial] as, in
// name order.
func SpatialElements() []Entity { return keys(spatialElements) }

// The relationship entities, named here because they are written from three
// places each and a typo in one of them is a file which loads with a
// relationship missing.
const (
	entityAggregates Entity = "IFCRELAGGREGATES"
	entityContains   Entity = "IFCRELCONTAINEDINSPATIALSTRUCTURE"
	entityAssigns    Entity = "IFCRELASSIGNSTOGROUP"
	entityBoundary   Entity = "IFCRELSPACEBOUNDARY"
	entityConnection Entity = "IFCCONNECTIONCURVEGEOMETRY"
	entityProject    Entity = "IFCPROJECT"
	entityContext    Entity = "IFCGEOMETRICREPRESENTATIONCONTEXT"
	entityUnits      Entity = "IFCUNITASSIGNMENT"
	entitySIUnit     Entity = "IFCSIUNIT"
	entityConversion Entity = "IFCCONVERSIONBASEDUNIT"
	entityMeasure    Entity = "IFCMEASUREWITHUNIT"
	entityExponents  Entity = "IFCDIMENSIONALEXPONENTS"
	entityLocal      Entity = "IFCLOCALPLACEMENT"
	entityAxis       Entity = "IFCAXIS2PLACEMENT3D"
	entityPoint      Entity = "IFCCARTESIANPOINT"
	entityDirection  Entity = "IFCDIRECTION"
)

// The georeference entities, named here for the reason the ones above are.
const (
	entityProjectedCRS  Entity = "IFCPROJECTEDCRS"
	entityMapConversion Entity = "IFCMAPCONVERSION"
)

// The shape and property entities, named here for the reason the ones above
// are.
const (
	entitySubcontext    Entity = "IFCGEOMETRICREPRESENTATIONSUBCONTEXT"
	entityProductShape  Entity = "IFCPRODUCTDEFINITIONSHAPE"
	entityShape         Entity = "IFCSHAPEREPRESENTATION"
	entityPolyline      Entity = "IFCPOLYLINE"
	entityClosedProfile Entity = "IFCARBITRARYCLOSEDPROFILEDEF"
	entityVoidedProfile Entity = "IFCARBITRARYPROFILEDEFWITHVOIDS"
	entityExtruded      Entity = "IFCEXTRUDEDAREASOLID"
	entityPropertySet   Entity = "IFCPROPERTYSET"
	entityProperty      Entity = "IFCPROPERTYSINGLEVALUE"
	entityDefines       Entity = "IFCRELDEFINESBYPROPERTIES"
)

// profileArea is the IfcProfileTypeEnum member every profile this package
// writes is.
//
// A profile is an area rather than a curve because it is swept into a solid.
// The other member, CURVE, is for a profile swept into a shell, which is a
// solid this package does not write.
const profileArea = "AREA"

// Write serialises model as an ISO 10303-21 exchange file.
//
// The bytes are a pure function of the model. Two calls with equal models
// produce equal bytes, in any process, on any machine and at any time — see
// the package comment for what that rules out and why it matters.
//
// Nothing is written to w until the whole model has been serialised
// successfully. An artefact is all or nothing: a file half written is one a
// later run would find on disk and read as the export of that model, and the
// refusals below are exactly the cases where that would be wrong.
func Write(w io.Writer, model Model) error {
	out := &writer{
		interned: make(map[string]reference),
		objects:  make(map[GlobalID]reference),
		contexts: make(map[string]reference),
	}

	if err := out.model(model); err != nil {
		return err
	}

	buffered := bufio.NewWriter(w)
	if err := out.render(buffered, model.Header); err != nil {
		return err
	}

	return buffered.Flush()
}

// instance is one line of the data section: its number, its entity and its
// already encoded attribute list.
//
// The attributes are encoded when the instance is created rather than when the
// file is rendered, because that encoding is also what the instance is
// interned by. Two points at the same coordinates encode identically, which is
// what makes them one instance without a comparison written for each type.
type instance struct {
	number     int
	entity     Entity
	attributes string
}

// writer is one serialisation in progress: the instances so far, the interner
// over them, and the identifiers already used.
type writer struct {
	instances []instance

	// interned maps an instance's rendered form to the instance which already
	// holds it, for the types where sharing is the convention: points,
	// directions, the placements over them and the polylines through them.
	//
	// Rooted objects are never interned. Two spaces with the same name and
	// the same placement are two spaces, and merging them would be this
	// package deciding something about the model.
	interned map[string]reference

	// objects maps a rooted object's identifier to the instance which holds
	// it. It is what resolves a group's members, and what catches an
	// identifier written on two objects.
	objects map[GlobalID]reference

	// contexts maps a representation context's identifier to the instance
	// which holds it. The model's own context is under the empty string, so a
	// shape which names no subcontext resolves through the same lookup as one
	// which does.
	contexts map[string]reference

	// subcontexts are the subcontext identifiers in the order they were
	// declared, which is what a refusal lists as the alternatives.
	subcontexts []string

	// boundaries are the space boundaries met during the spatial walk, in the
	// order they were met, held until every product has been written.
	//
	// They are deferred for the reason a group's members are: a boundary names
	// the element by its identifier, that element stands in whichever spatial
	// element contains it, and the walk may not have reached it yet. Resolving
	// as they are met would refuse a wall in the next storey for not existing.
	boundaries []pending
}

// pending is one space boundary and the space which stated it, waiting for the
// walk to finish.
type pending struct {
	// space is the instance of the space the boundary belongs to.
	space reference

	// of is the identifier of that space, which a refusal names.
	of GlobalID

	// boundary is what was written on it.
	boundary SpaceBoundary
}

// model serialises the whole model into instances.
//
// The order is the traversal, and the traversal is the file's numbering: units
// first because the project references them, then the context for the same
// reason, then the georeference, which converts out of that context, then the
// project, then the spatial decomposition depth first in the order the caller
// wrote it, then the groups, which may assign anything above them, and last the
// space boundaries, which may name any element the walk wrote.
func (w *writer) model(model Model) error {
	units, err := w.units(model.Units)
	if err != nil {
		return err
	}

	context, err := w.context(model.Context)
	if err != nil {
		return err
	}

	if err := w.georeference(model.Georeference, context); err != nil {
		return err
	}

	project := model.Project

	root, err := w.rooted(entityProject, project.GlobalID, []value{
		text(project.GlobalID),
		absent{}, // OwnerHistory
		optionalText(project.Name),
		optionalText(project.Description),
		absent{}, // ObjectType
		optionalText(project.LongName),
		optionalText(project.Phase),
		list{context},
		units,
	})
	if err != nil {
		return err
	}

	sites, err := w.spatial(project.Sites, 0)
	if err != nil {
		return err
	}

	if err := w.aggregates(project.Aggregates, root, sites); err != nil {
		return err
	}

	if err := w.groups(project.Groups); err != nil {
		return err
	}

	return w.spaceBoundaries()
}

// units writes the unit assignment and returns the reference to it.
func (w *writer) units(assignment UnitAssignment) (reference, error) {
	if len(assignment.Units) == 0 {
		return 0, EmptyUnitsError{}
	}

	written := make(list, 0, len(assignment.Units))
	for _, assigned := range assignment.Units {
		at, err := w.unit(assigned)
		if err != nil {
			return 0, err
		}
		written = append(written, at)
	}

	return w.add(entityUnits, []value{written})
}

// unit writes one unit of an assignment, whichever of the two it is.
//
// The refusal at the end is what anything else gets, and it is here for the
// case a unit is added to this package and not added here: written as nothing
// at all is worse than refused.
//
// A caller can reach it, and the ways it can are refusals on purpose. The
// method which closes [Unit] has a value receiver, so a pointer to either of
// the two satisfies the interface as well — and the switch takes the values
// only, exactly as [writer.item] takes the values of an [Item]. A unit is a
// handful of fields copied into an assignment and encoded from that copy;
// taking `&SIUnit{…}` as well would be a second spelling of every unit for
// this package to keep in step, and one whose aliasing the encoding has no use
// for. A nil, typed or not, lands here too, which is a caller assigning
// nothing at all.
func (w *writer) unit(assigned Unit) (reference, error) {
	switch held := assigned.(type) {
	case SIUnit:
		return w.si(held)
	case ConversionBasedUnit:
		return w.conversion(held)
	default:
		return 0, UnknownUnitError{Unit: fmt.Sprintf("%T", assigned)}
	}
}

// si writes one IfcSIUnit.
func (w *writer) si(unit SIUnit) (reference, error) {
	return w.add(entitySIUnit, []value{
		derived{}, // Dimensions, which the schema derives
		enumeration(unit.Type),
		optionalEnumeration(unit.Prefix),
		enumeration(unit.Name),
	})
}

// conversion writes one IfcConversionBasedUnit and the three entities beneath
// it: its dimensional exponents, the SI unit its factor is over, and the
// factor itself.
//
// They are written in that order because each is referenced by the one after
// it, which is the same rule the whole traversal follows.
func (w *writer) conversion(unit ConversionBasedUnit) (reference, error) {
	exponents, err := w.exponents(unit.Dimensions)
	if err != nil {
		return 0, err
	}

	factor, err := w.measure(unit.Factor)
	if err != nil {
		return 0, err
	}

	return w.add(entityConversion, []value{
		exponents,
		enumeration(unit.Type),
		text(unit.Name),
		factor,
	})
}

// exponents writes one IfcDimensionalExponents.
func (w *writer) exponents(dimensions DimensionalExponents) (reference, error) {
	return w.add(entityExponents, []value{
		integer(dimensions.Length),
		integer(dimensions.Mass),
		integer(dimensions.Time),
		integer(dimensions.ElectricCurrent),
		integer(dimensions.ThermodynamicTemperature),
		integer(dimensions.AmountOfSubstance),
		integer(dimensions.LuminousIntensity),
	})
}

// measure writes one IfcMeasureWithUnit and the unit it is over.
func (w *writer) measure(factor MeasureWithUnit) (reference, error) {
	over, err := w.unit(factor.Unit)
	if err != nil {
		return 0, err
	}

	return w.add(entityMeasure, []value{
		typedReal{measure: factor.Measure, value: factor.Value},
		over,
	})
}

// context writes the geometric representation context and returns the
// reference to it.
func (w *writer) context(context RepresentationContext) (reference, error) {
	world, err := w.axis(context.World)
	if err != nil {
		return 0, err
	}

	north := value(absent{})
	if context.TrueNorth != nil {
		at, err := w.direction(*context.TrueNorth)
		if err != nil {
			return 0, err
		}
		north = at
	}

	precision := value(absent{})
	if context.Precision != 0 {
		precision = real(context.Precision)
	}

	at, err := w.add(entityContext, []value{
		optionalText(context.Identifier),
		optionalText(context.Type),
		integer(context.Dimension),
		precision,
		world,
		north,
	})
	if err != nil {
		return 0, err
	}

	w.contexts[""] = at

	for _, subcontext := range context.Subcontexts {
		if err := w.subcontext(subcontext, at); err != nil {
			return 0, err
		}
	}

	return at, nil
}

// subcontext writes one view of the context above and records it under its
// identifier, which is what a shape names to be written in it.
//
// The four attributes it inherits are written as derived rather than as
// absent. They are not values which were left out: the dimension, the
// precision, the world coordinate system and true north of a subcontext are
// the parent's, and the schema says so by redeclaring them.
func (w *writer) subcontext(subcontext Subcontext, parent reference) error {
	if subcontext.Identifier == "" {
		return UnnamedSubcontextError{}
	}
	if _, held := w.contexts[subcontext.Identifier]; held {
		return DuplicateSubcontextError{Identifier: subcontext.Identifier}
	}

	at, err := w.add(entitySubcontext, []value{
		text(subcontext.Identifier),
		optionalText(subcontext.Type),
		derived{}, // CoordinateSpaceDimension
		derived{}, // Precision
		derived{}, // WorldCoordinateSystem
		derived{}, // TrueNorth
		parent,
		absent{}, // TargetScale
		optionalEnumeration(subcontext.TargetView),
		optionalText(subcontext.UserDefinedTargetView),
	})
	if err != nil {
		return err
	}

	w.contexts[subcontext.Identifier] = at
	w.subcontexts = append(w.subcontexts, subcontext.Identifier)

	return nil
}

// georeference writes the projected coordinate reference system and the map
// conversion out of context into it.
//
// A nil georeference writes nothing, which is the whole of what a file nobody
// has placed on the earth needs. The conversion is written second because it
// references the system, and neither is written at all unless the system is
// named: the name is what a reader does something with, and the offsets on
// their own say only that the coordinates are somewhere.
func (w *writer) georeference(georeference *Georeference, context reference) error {
	if georeference == nil {
		return nil
	}

	crs := georeference.CRS
	if crs.Name == "" {
		return UnnamedCRSError{}
	}

	at, err := w.add(entityProjectedCRS, []value{
		text(crs.Name),
		optionalText(crs.Description),
		optionalText(crs.GeodeticDatum),
		optionalText(crs.VerticalDatum),
		optionalText(crs.MapProjection),
		optionalText(crs.MapZone),
		// MapUnit, which the unit assignment already states for the whole
		// file. Writing it a second time here is a second place for it to be
		// wrong, and the two would not have to agree.
		absent{},
	})
	if err != nil {
		return err
	}

	conversion := georeference.Conversion

	_, err = w.add(entityMapConversion, []value{
		context,
		at,
		real(conversion.Eastings),
		real(conversion.Northings),
		real(conversion.OrthogonalHeight),
		optionalReal(conversion.XAxisAbscissa),
		optionalReal(conversion.XAxisOrdinate),
		optionalReal(conversion.Scale),
	})

	return err
}

// spatial writes a list of sibling spatial elements beneath the placement
// their parent was placed by, and returns the references to them in the order
// they were written.
//
// The recursion is depth first and in the caller's order, which is what makes
// the numbering a property of the model rather than of the run: sorting is the
// caller's to do, because the order a decomposition is written in is a fact
// about the model and not about the format.
func (w *writer) spatial(elements []Spatial, under reference) ([]value, error) {
	written := make([]value, 0, len(elements))

	for _, element := range elements {
		tail, known := spatialElements[element.Entity]
		if !known {
			return nil, UnknownEntityError{
				Entity:   element.Entity,
				Position: "a spatial element",
				Known:    keys(spatialElements),
			}
		}

		placement, err := w.placement(element.Placement, under)
		if err != nil {
			return nil, err
		}

		// The shape comes before the element which carries it, because an
		// instance may only reference one already written.
		representation, err := w.representation(element.Representation, element.GlobalID)
		if err != nil {
			return nil, err
		}

		attributes := []value{
			text(element.GlobalID),
			absent{}, // OwnerHistory
			optionalText(element.Name),
			optionalText(element.Description),
			optionalText(element.ObjectType),
			placement,
			representation,
			optionalText(element.LongName),
			optionalEnumeration(string(element.Composition)),
		}
		attributes = append(attributes, absents(tail)...)

		at, err := w.rooted(element.Entity, element.GlobalID, attributes)
		if err != nil {
			return nil, err
		}

		below, ok := placement.(reference)
		if !ok {
			below = 0
		}

		children, err := w.spatial(element.Children, below)
		if err != nil {
			return nil, err
		}

		if err := w.aggregates(element.Aggregates, at, children); err != nil {
			return nil, err
		}

		if err := w.products(element, at, below); err != nil {
			return nil, err
		}

		if err := w.properties(element.Properties, at); err != nil {
			return nil, err
		}

		if err := w.bounded(element, at); err != nil {
			return nil, err
		}

		written = append(written, at)
	}

	return written, nil
}

// products writes the things contained in one spatial element, and the
// relationship containing them.
func (w *writer) products(element Spatial, in reference, under reference) error {
	if len(element.Products) == 0 {
		return nil
	}

	contained := make(list, 0, len(element.Products))
	for _, product := range element.Products {
		tail, known := products[product.Entity]
		if !known {
			return UnknownEntityError{
				Entity:   product.Entity,
				Position: "a product",
				Known:    keys(products),
			}
		}

		placement, err := w.placement(product.Placement, under)
		if err != nil {
			return err
		}

		attributes := []value{
			text(product.GlobalID),
			absent{}, // OwnerHistory
			optionalText(product.Name),
			optionalText(product.Description),
			optionalText(product.ObjectType),
			placement,
			absent{}, // Representation
			absent{}, // Tag
		}
		attributes = append(attributes, absents(tail)...)

		at, err := w.rooted(product.Entity, product.GlobalID, attributes)
		if err != nil {
			return err
		}

		contained = append(contained, at)
	}

	if element.Contains == "" {
		return MissingGlobalIDError{Entity: entityContains, Of: element.GlobalID}
	}

	_, err := w.rooted(entityContains, element.Contains, []value{
		text(element.Contains),
		absent{}, // OwnerHistory
		absent{}, // Name
		absent{}, // Description
		contained,
		in,
	})

	return err
}

// aggregates writes the IfcRelAggregates joining one object to the things
// decomposed out of it.
//
// An object with nothing decomposed out of it gets no relationship, and its
// identifier is unread: a relationship with an empty set of related objects is
// invalid in the schema, and a caller which has no children has nothing to
// derive an identifier for.
func (w *writer) aggregates(id GlobalID, of reference, children []value) error {
	if len(children) == 0 {
		return nil
	}

	if id == "" {
		return MissingGlobalIDError{Entity: entityAggregates}
	}

	_, err := w.rooted(entityAggregates, id, []value{
		text(id),
		absent{}, // OwnerHistory
		absent{}, // Name
		absent{}, // Description
		of,
		list(children),
	})

	return err
}

// bounded records the space boundaries one spatial element states, refusing
// any written on something which is not a space.
//
// Nothing is written here. The relationship names an element which may stand
// anywhere in the decomposition, so it is held until the walk has finished and
// written by [writer.spaceBoundaries].
func (w *writer) bounded(element Spatial, at reference) error {
	if len(element.Boundaries) == 0 {
		return nil
	}

	if element.Entity != EntitySpace {
		return BoundaryOnNonSpaceError{Entity: element.Entity, Of: element.GlobalID}
	}

	for _, boundary := range element.Boundaries {
		w.boundaries = append(w.boundaries, pending{space: at, of: element.GlobalID, boundary: boundary})
	}

	return nil
}

// spaceBoundaries writes the relationships between the spaces and the elements
// bounding them.
//
// They come last so that every element one of them may name has been written,
// which is what lets a boundary be stated on the space alone: a room does not
// have to know where in the decomposition the wall beside it was put.
func (w *writer) spaceBoundaries() error {
	for _, held := range w.boundaries {
		boundary := held.boundary

		if boundary.Element == "" {
			return MissingBoundaryElementError{Space: held.of, Boundary: boundary.GlobalID}
		}

		element, known := w.objects[boundary.Element]
		if !known {
			return UnknownBoundaryElementError{
				Space:    held.of,
				Boundary: boundary.GlobalID,
				Element:  boundary.Element,
			}
		}

		if boundary.Physical == "" {
			return UnclassifiedBoundaryError{
				Space:     held.of,
				Boundary:  boundary.GlobalID,
				Attribute: "PhysicalOrVirtualBoundary",
			}
		}
		if boundary.Internal == "" {
			return UnclassifiedBoundaryError{
				Space:     held.of,
				Boundary:  boundary.GlobalID,
				Attribute: "InternalOrExternalBoundary",
			}
		}

		// The geometry comes before the relationship which carries it, because
		// an instance may only reference one already written.
		connection, err := w.connection(boundary.Connection)
		if err != nil {
			return err
		}

		if _, err := w.rooted(entityBoundary, boundary.GlobalID, []value{
			text(boundary.GlobalID),
			absent{}, // OwnerHistory
			optionalText(boundary.Name),
			optionalText(boundary.Description),
			held.space,
			element,
			connection,
			enumeration(string(boundary.Physical)),
			enumeration(string(boundary.Internal)),
		}); err != nil {
			return err
		}
	}

	return nil
}

// connection writes an IfcConnectionCurveGeometry, and is absent for a
// boundary the caller holds no geometry for.
func (w *writer) connection(curve *ConnectionCurve) (value, error) {
	if curve == nil {
		return absent{}, nil
	}

	relating, err := w.polyline(curve.OnRelating)
	if err != nil {
		return nil, err
	}

	related := value(absent{})
	if curve.OnRelated != nil {
		at, err := w.polyline(*curve.OnRelated)
		if err != nil {
			return nil, err
		}
		related = at
	}

	return w.add(entityConnection, []value{relating, related})
}

// groups writes the zones and the assignments which give them their members.
//
// They come last so that everything they may assign has been written, which is
// what lets a member be named by the identifier of the object rather than by a
// position in a tree the caller would have to keep in step.
func (w *writer) groups(groups []Group) error {
	assignments := make([]struct {
		group   Group
		zone    reference
		members list
	}, 0, len(groups))

	for _, group := range groups {
		zone, err := w.rooted(EntityZone, group.GlobalID, []value{
			text(group.GlobalID),
			absent{}, // OwnerHistory
			optionalText(group.Name),
			optionalText(group.Description),
			optionalText(group.ObjectType),
			optionalText(group.LongName),
		})
		if err != nil {
			return err
		}

		assignments = append(assignments, struct {
			group   Group
			zone    reference
			members list
		}{group: group, zone: zone})
	}

	// The members are resolved in a second pass so that a zone may assign
	// another zone, whichever order the two were written in.
	for i := range assignments {
		group := assignments[i].group

		for _, member := range group.Members {
			at, held := w.objects[member]
			if !held {
				return UnknownMemberError{Group: group.GlobalID, Member: member}
			}
			assignments[i].members = append(assignments[i].members, at)
		}
	}

	for _, assignment := range assignments {
		if len(assignment.members) == 0 {
			continue
		}

		if assignment.group.Assignment == "" {
			return MissingGlobalIDError{Entity: entityAssigns, Of: assignment.group.GlobalID}
		}

		_, err := w.rooted(entityAssigns, assignment.group.Assignment, []value{
			text(assignment.group.Assignment),
			absent{}, // OwnerHistory
			absent{}, // Name
			absent{}, // Description
			assignment.members,
			absent{}, // RelatedObjectsType
			assignment.zone,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// placement writes an IfcLocalPlacement relative to under, and is absent for
// an element the caller placed nowhere.
func (w *writer) placement(placement *Placement, under reference) (value, error) {
	if placement == nil {
		return absent{}, nil
	}

	relative, err := w.axis(*placement)
	if err != nil {
		return nil, err
	}

	to := value(absent{})
	if under != 0 {
		to = under
	}

	return w.intern(entityLocal, []value{to, relative})
}

// axis writes an IfcAxis2Placement3D and the point and directions beneath it.
func (w *writer) axis(placement Placement) (reference, error) {
	location, err := w.point(placement.Location)
	if err != nil {
		return 0, err
	}

	axis, err := w.optionalDirection(placement.Axis)
	if err != nil {
		return 0, err
	}

	reference, err := w.optionalDirection(placement.RefDirection)
	if err != nil {
		return 0, err
	}

	return w.intern(entityAxis, []value{location, axis, reference})
}

// point writes an IfcCartesianPoint.
func (w *writer) point(at Point) (reference, error) {
	return w.intern(entityPoint, []value{list{real(at.X), real(at.Y), real(at.Z)}})
}

// direction writes an IfcDirection.
func (w *writer) direction(along Direction) (reference, error) {
	return w.intern(entityDirection, []value{list{real(along.X), real(along.Y), real(along.Z)}})
}

// optionalDirection is [writer.direction] for a direction which may not be
// there.
func (w *writer) optionalDirection(along *Direction) (value, error) {
	if along == nil {
		return absent{}, nil
	}
	return w.direction(*along)
}

// representation writes an IfcProductDefinitionShape and everything beneath
// it, and is absent for an object nobody has drawn.
func (w *writer) representation(representation *Representation, of GlobalID) (value, error) {
	if representation == nil {
		return absent{}, nil
	}

	if len(representation.Shapes) == 0 {
		return nil, EmptyRepresentationError{Of: of}
	}

	shapes := make(list, 0, len(representation.Shapes))
	for _, shape := range representation.Shapes {
		at, err := w.shape(shape)
		if err != nil {
			return nil, err
		}
		shapes = append(shapes, at)
	}

	return w.add(entityProductShape, []value{
		optionalText(representation.Name),
		optionalText(representation.Description),
		shapes,
	})
}

// shape writes one IfcShapeRepresentation and the geometry it holds.
func (w *writer) shape(shape Shape) (reference, error) {
	context, held := w.contexts[shape.Context]
	if !held {
		return 0, UnknownSubcontextError{Context: shape.Context, Known: slices.Clone(w.subcontexts)}
	}

	if len(shape.Items) == 0 {
		return 0, EmptyShapeError{Identifier: shape.Identifier}
	}

	items := make(list, 0, len(shape.Items))
	for _, item := range shape.Items {
		at, err := w.item(item)
		if err != nil {
			return 0, err
		}
		items = append(items, at)
	}

	return w.add(entityShape, []value{
		context,
		optionalText(shape.Identifier),
		optionalText(shape.Type),
		items,
	})
}

// item writes one piece of a shape's geometry.
//
// The refusal at the end is unreachable through the exported API — [Item] is
// closed by an unexported method — and it is here for the case it is
// reachable from: a geometry added to this package and not added here would
// otherwise be written as nothing at all.
func (w *writer) item(item Item) (reference, error) {
	switch held := item.(type) {
	case Polyline:
		return w.polyline(held)
	case ExtrudedArea:
		return w.extruded(held)
	default:
		return 0, UnknownItemError{Item: fmt.Sprintf("%T", item)}
	}
}

// polyline writes an IfcPolyline and the points beneath it.
//
// It is interned like the points it runs through, which is what makes the
// outline of a room and the curve bounding the profile it is swept from one
// instance rather than two identical ones. A polyline carries no identity in
// the model — it is a run of coordinates — so two which encode the same way
// are the same curve wherever each is referenced from.
func (w *writer) polyline(line Polyline) (reference, error) {
	if len(line.Points) < 2 {
		return 0, ShortPolylineError{Points: len(line.Points)}
	}

	points := make(list, 0, len(line.Points))
	for _, at := range line.Points {
		held, err := w.point2D(at)
		if err != nil {
			return 0, err
		}
		points = append(points, held)
	}

	return w.intern(entityPolyline, []value{points})
}

// point2D writes an IfcCartesianPoint in the plane.
func (w *writer) point2D(at Point2D) (reference, error) {
	return w.intern(entityPoint, []value{list{real(at.X), real(at.Y)}})
}

// profile writes the cross section a solid is swept from, as whichever of the
// two arbitrary profile entities its holes make it.
func (w *writer) profile(profile ArbitraryProfile) (reference, error) {
	outer, err := w.closed(profile.Outer, false)
	if err != nil {
		return 0, err
	}

	if len(profile.Inner) == 0 {
		return w.add(entityClosedProfile, []value{
			enumeration(profileArea),
			optionalText(profile.Name),
			outer,
		})
	}

	inner := make(list, 0, len(profile.Inner))
	for _, curve := range profile.Inner {
		at, err := w.closed(curve, true)
		if err != nil {
			return 0, err
		}
		inner = append(inner, at)
	}

	return w.add(entityVoidedProfile, []value{
		enumeration(profileArea),
		optionalText(profile.Name),
		outer,
		inner,
	})
}

// closed writes one curve of a profile, refusing one which does not close.
func (w *writer) closed(curve Polyline, inner bool) (reference, error) {
	if len(curve.Points) < 2 {
		return 0, ShortPolylineError{Points: len(curve.Points)}
	}

	first, last := curve.Points[0], curve.Points[len(curve.Points)-1]
	if first != last {
		return 0, OpenCurveError{First: first, Last: last, Inner: inner}
	}

	return w.polyline(curve)
}

// extruded writes an IfcExtrudedAreaSolid: a profile swept along a direction.
func (w *writer) extruded(solid ExtrudedArea) (reference, error) {
	// Written as a comparison against zero rather than as `<=` so that a depth
	// which is not a number is refused here, naming the depth, rather than
	// reaching the encoder as a real with no part 21 spelling.
	if !(solid.Depth > 0) {
		return 0, NonPositiveDepthError{Depth: solid.Depth}
	}

	profile, err := w.profile(solid.Profile)
	if err != nil {
		return 0, err
	}

	position, err := w.axis(solid.Position)
	if err != nil {
		return 0, err
	}

	along, err := w.direction(solid.Direction)
	if err != nil {
		return 0, err
	}

	return w.add(entityExtruded, []value{profile, position, along, real(solid.Depth)})
}

// properties writes the property sets attached to one object, and the
// relationships which attach them.
func (w *writer) properties(sets []PropertySet, of reference) error {
	for _, set := range sets {
		if len(set.Properties) == 0 {
			return EmptyPropertySetError{GlobalID: set.GlobalID}
		}

		written := make(list, 0, len(set.Properties))
		for _, property := range set.Properties {
			if property.Name == "" {
				return UnnamedPropertyError{Set: set.GlobalID}
			}

			at, err := w.add(entityProperty, []value{
				text(property.Name),
				optionalText(property.Description),
				optionalTypedText(property.Value),
				absent{}, // Unit
			})
			if err != nil {
				return err
			}

			written = append(written, at)
		}

		at, err := w.rooted(entityPropertySet, set.GlobalID, []value{
			text(set.GlobalID),
			absent{}, // OwnerHistory
			optionalText(set.Name),
			optionalText(set.Description),
			written,
		})
		if err != nil {
			return err
		}

		if set.Defines == "" {
			return MissingGlobalIDError{Entity: entityDefines, Of: set.GlobalID}
		}

		if _, err := w.rooted(entityDefines, set.Defines, []value{
			text(set.Defines),
			absent{}, // OwnerHistory
			absent{}, // Name
			absent{}, // Description
			list{of},
			at,
		}); err != nil {
			return err
		}
	}

	return nil
}

// rooted adds an instance of a rooted object: one which carries a GlobalId,
// which is therefore not interned and which no other object may share an
// identifier with.
func (w *writer) rooted(entity Entity, id GlobalID, attributes []value) (reference, error) {
	if id == "" {
		return 0, MissingGlobalIDError{Entity: entity}
	}
	if _, held := w.objects[id]; held {
		return 0, DuplicateGlobalIDError{GlobalID: id}
	}

	at, err := w.add(entity, attributes)
	if err != nil {
		return 0, err
	}

	w.objects[id] = at

	return at, nil
}

// intern adds an instance of a type where sharing is the convention, and
// returns the existing instance where one already holds the same value.
func (w *writer) intern(entity Entity, attributes []value) (reference, error) {
	encoded, err := encode(attributes)
	if err != nil {
		return 0, err
	}

	key := string(entity) + encoded
	if at, held := w.interned[key]; held {
		return at, nil
	}

	at := w.append(entity, encoded)
	w.interned[key] = at

	return at, nil
}

// add adds an instance which is never shared.
func (w *writer) add(entity Entity, attributes []value) (reference, error) {
	encoded, err := encode(attributes)
	if err != nil {
		return 0, err
	}

	return w.append(entity, encoded), nil
}

// append records an instance and hands back the reference to it. It is the one
// place an instance number is assigned, which is what makes the numbering the
// traversal order and nothing else.
func (w *writer) append(entity Entity, attributes string) reference {
	number := len(w.instances) + 1
	w.instances = append(w.instances, instance{number: number, entity: entity, attributes: attributes})
	return reference(number)
}

// render writes the header and the data section.
func (w *writer) render(out io.Writer, header Header) error {
	written := &errWriter{to: out}

	written.line("ISO-10303-21;")
	written.line("HEADER;")

	description, err := encode([]value{texts(header.Description), text("2;1")})
	if err != nil {
		return err
	}
	written.line("FILE_DESCRIPTION(" + description + ");")

	name, err := encode([]value{
		text(header.Name),
		text(header.TimeStamp),
		texts(header.Author),
		texts(header.Organisation),
		text(header.Preprocessor),
		text(header.Originating),
		text(header.Authorisation),
	})
	if err != nil {
		return err
	}
	written.line("FILE_NAME(" + name + ");")

	schema, err := encode([]value{list{text(Schema)}})
	if err != nil {
		return err
	}
	written.line("FILE_SCHEMA(" + schema + ");")

	written.line("ENDSEC;")
	written.line("DATA;")

	for _, held := range w.instances {
		written.line("#" + strconv.Itoa(held.number) + "=" + string(held.entity) + "(" + held.attributes + ");")
	}

	written.line("ENDSEC;")
	written.line("END-ISO-10303-21;")

	return written.err
}

// encode renders an attribute list as the comma separated text between an
// entity's parentheses.
func encode(attributes []value) (string, error) {
	var dst []byte

	for i, attribute := range attributes {
		if i > 0 {
			dst = append(dst, ',')
		}

		var err error
		dst, err = attribute.encode(dst)
		if err != nil {
			return "", err
		}
	}

	return string(dst), nil
}

// absents is count optional attributes which are not written.
func absents(count int) []value {
	out := make([]value, count)
	for i := range out {
		out[i] = absent{}
	}
	return out
}

// keys is a table's entities in name order, for a message which lists what
// could have been written instead.
func keys(table map[Entity]int) []Entity {
	out := make([]Entity, 0, len(table))
	for entity := range table {
		out = append(out, entity)
	}
	slices.Sort(out)
	return out
}

// errWriter writes lines until one of them fails and then writes nothing more.
//
// The lines are a rendering of a model which has already been serialised
// successfully, so the only thing which can go wrong here is the destination.
// Checking each write at its call site would put an error path between every
// two lines of a function whose subject is the layout of a file.
type errWriter struct {
	to  io.Writer
	err error
}

// line writes one line, terminated by a line feed.
//
// The terminator is a line feed on every platform. Part 21 permits either, and
// a file whose line endings depended on the machine it was written on would
// not be the same bytes twice — which is the property this package exists to
// have.
func (w *errWriter) line(written string) {
	if w.err != nil {
		return
	}
	_, w.err = io.WriteString(w.to, written+"\n")
}
