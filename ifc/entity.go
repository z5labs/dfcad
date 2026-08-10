// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package ifc

import "slices"

// Support is what this package can do with the entity a caller's mapping names
// for a product.
//
// A mapping from somebody else's vocabulary onto IFC4 goes wrong in two ways
// which look identical in the file — both reach it as an [EntityProxy] — and
// which have nothing in common as mistakes. A code naming an IFC4 product this
// package has no attribute list for is a gap in this package: the thing is what
// the author said it was, the proxy is a faithful stand-in for it, and the fix
// is a line in the table here. A code naming no IFC4 product at all is a
// mistake in the mapping: a misspelling, or a code naming something a product
// cannot be, and the proxy is standing in for nothing anybody meant.
//
// [Supports] is what tells them apart, so that a caller can say which it hit
// rather than reporting a proxy and leaving the reader to guess.
type Support int

const (
	// SupportUnknown is a code IFC4 defines no product entity for.
	//
	// Either the name is misspelled, or it names an entity a product may not
	// be — a relationship, a property set, a type object. Both are mistakes in
	// the mapping rather than gaps in this package, and neither is fixed by
	// this package growing an attribute list.
	SupportUnknown Support = iota

	// SupportProduct is an IFC4 product entity this package has no attribute
	// list for, which is written as an [EntityProxy] naming what it is.
	//
	// The classification is right and the file is short of it. IFC4 fixes an
	// attribute list per entity and this package writes exactly the ones it
	// has transcribed, so the answer to one it has not is a proxy and a report
	// rather than an instance with the wrong number of attributes — which is a
	// file no reader loads.
	SupportProduct

	// SupportWritable is an entity this package writes a [Product] as, which
	// is [Products].
	SupportWritable
)

// String implements [fmt.Stringer], in the words a report uses.
func (s Support) String() string {
	switch s {
	case SupportWritable:
		return "writable"
	case SupportProduct:
		return "unwritten"
	default:
		return "unknown"
	}
}

// Supports says what this package can do with entity, written as a product.
//
// The name is matched exactly, in the upper case an exchange file spells one
// in: a caller holding a mapping written `IfcWall` upper-cases it before
// asking, because that is the spelling this package writes and compares.
//
// The three answers are the three things a caller can do about it, which is
// why the middle one exists at all. See [Support].
func Supports(entity Entity) Support {
	if _, writable := products[entity]; writable {
		return SupportWritable
	}
	if _, defined := productEntities[entity]; defined {
		return SupportProduct
	}
	return SupportUnknown
}

// ProductEntities is every IFC4 product entity, in name order, whether or not
// this package can write one.
//
// It is exported for the same reason [Products] is, and it answers a different
// question: [Products] is what a mapping may name today, and this is what IFC4
// defines for a mapping to name at all. A code in this set and not in that one
// is a gap somebody can close; a code in neither is a code to fix.
func ProductEntities() []Entity {
	out := make([]Entity, 0, len(productEntities))
	for entity := range productEntities {
		out = append(out, entity)
	}
	slices.Sort(out)
	return out
}

// productEntities is IfcProduct and its subtypes in IFC4, transcribed.
//
// It is the schema's own closed set and not a judgement about a model, exactly
// as the attribute-list tables in write.go are. The abstract supertypes are in
// it — IfcElement, IfcBuildingElement, IfcDistributionFlowElement — because
// what this set answers is whether a name is one IFC4 defines, and an author
// who classified something as an IfcBuildingElement made a different mistake
// from one who misspelled a wall.
//
// It is IfcProduct's subtree and nothing wider on purpose. A classification
// read here names what a product in the file is, so the question the set has to
// answer is whether IFC4 has a product of that name — and a set stretched to
// every entity in the schema would answer a question nothing here asks while
// being far harder to keep true.
//
// The deprecated ones are here too. IfcWallStandardCase is deprecated in IFC4
// and it is still an entity IFC4 defines, so a model classified against it has
// named something real and gets told that this package does not write it,
// rather than being told it invented a name.
var productEntities = map[Entity]struct{}{
	"IFCPRODUCT": {},

	"IFCANNOTATION": {},
	"IFCGRID":       {},
	"IFCPROXY":      {},

	"IFCPORT":             {},
	"IFCDISTRIBUTIONPORT": {},

	// IfcSpatialElement and its subtypes. A model classifying an element as
	// one of these has said the thing is a place rather than a thing standing
	// in one, which is a mistake this package can name precisely.
	"IFCSPATIALELEMENT":                  {},
	"IFCEXTERNALSPATIALSTRUCTUREELEMENT": {},
	"IFCEXTERNALSPATIALELEMENT":          {},
	"IFCSPATIALSTRUCTUREELEMENT":         {},
	"IFCBUILDING":                        {},
	"IFCBUILDINGSTOREY":                  {},
	"IFCSITE":                            {},
	"IFCSPACE":                           {},
	"IFCSPATIALZONE":                     {},

	// IfcElement, and IfcBuildingElement beneath it.
	"IFCELEMENT":                {},
	"IFCBUILDINGELEMENT":        {},
	"IFCBEAM":                   {},
	"IFCBEAMSTANDARDCASE":       {},
	"IFCBUILDINGELEMENTPROXY":   {},
	"IFCCHIMNEY":                {},
	"IFCCOLUMN":                 {},
	"IFCCOLUMNSTANDARDCASE":     {},
	"IFCCOVERING":               {},
	"IFCCURTAINWALL":            {},
	"IFCDOOR":                   {},
	"IFCDOORSTANDARDCASE":       {},
	"IFCFOOTING":                {},
	"IFCMEMBER":                 {},
	"IFCMEMBERSTANDARDCASE":     {},
	"IFCPILE":                   {},
	"IFCPLATE":                  {},
	"IFCPLATESTANDARDCASE":      {},
	"IFCRAILING":                {},
	"IFCRAMP":                   {},
	"IFCRAMPFLIGHT":             {},
	"IFCROOF":                   {},
	"IFCSHADINGDEVICE":          {},
	"IFCSLAB":                   {},
	"IFCSLABELEMENTEDCASE":      {},
	"IFCSLABSTANDARDCASE":       {},
	"IFCSTAIR":                  {},
	"IFCSTAIRFLIGHT":            {},
	"IFCWALL":                   {},
	"IFCWALLELEMENTEDCASE":      {},
	"IFCWALLSTANDARDCASE":       {},
	"IFCWINDOW":                 {},
	"IFCWINDOWSTANDARDCASE":     {},
	"IFCCIVILELEMENT":           {},
	"IFCELEMENTASSEMBLY":        {},
	"IFCGEOGRAPHICELEMENT":      {},
	"IFCTRANSPORTELEMENT":       {},
	"IFCVIRTUALELEMENT":         {},
	"IFCFURNISHINGELEMENT":      {},
	"IFCFURNITURE":              {},
	"IFCSYSTEMFURNITUREELEMENT": {},

	// IfcElementComponent and its subtypes.
	"IFCELEMENTCOMPONENT":          {},
	"IFCBUILDINGELEMENTPART":       {},
	"IFCDISCRETEACCESSORY":         {},
	"IFCFASTENER":                  {},
	"IFCMECHANICALFASTENER":        {},
	"IFCREINFORCINGELEMENT":        {},
	"IFCREINFORCINGBAR":            {},
	"IFCREINFORCINGMESH":           {},
	"IFCTENDON":                    {},
	"IFCTENDONANCHOR":              {},
	"IFCVIBRATIONISOLATOR":         {},
	"IFCFEATUREELEMENT":            {},
	"IFCFEATUREELEMENTADDITION":    {},
	"IFCPROJECTIONELEMENT":         {},
	"IFCFEATUREELEMENTSUBTRACTION": {},
	"IFCOPENINGELEMENT":            {},
	"IFCOPENINGSTANDARDCASE":       {},
	"IFCVOIDINGFEATURE":            {},
	"IFCSURFACEFEATURE":            {},

	// IfcDistributionElement, which is what a house model's services are.
	"IFCDISTRIBUTIONELEMENT":          {},
	"IFCDISTRIBUTIONCONTROLELEMENT":   {},
	"IFCACTUATOR":                     {},
	"IFCALARM":                        {},
	"IFCCONTROLLER":                   {},
	"IFCFLOWINSTRUMENT":               {},
	"IFCPROTECTIVEDEVICETRIPPINGUNIT": {},
	"IFCSENSOR":                       {},
	"IFCUNITARYCONTROLELEMENT":        {},
	"IFCDISTRIBUTIONFLOWELEMENT":      {},
	"IFCDISTRIBUTIONCHAMBERELEMENT":   {},
	"IFCENERGYCONVERSIONDEVICE":       {},
	"IFCAIRTOAIRHEATRECOVERY":         {},
	"IFCBOILER":                       {},
	"IFCBURNER":                       {},
	"IFCCHILLER":                      {},
	"IFCCOIL":                         {},
	"IFCCONDENSER":                    {},
	"IFCCOOLEDBEAM":                   {},
	"IFCCOOLINGTOWER":                 {},
	"IFCELECTRICGENERATOR":            {},
	"IFCELECTRICMOTOR":                {},
	"IFCENGINE":                       {},
	"IFCEVAPORATIVECOOLER":            {},
	"IFCEVAPORATOR":                   {},
	"IFCHEATEXCHANGER":                {},
	"IFCHUMIDIFIER":                   {},
	"IFCMOTORCONNECTION":              {},
	"IFCSOLARDEVICE":                  {},
	"IFCTRANSFORMER":                  {},
	"IFCTUBEBUNDLE":                   {},
	"IFCUNITARYEQUIPMENT":             {},
	"IFCFLOWCONTROLLER":               {},
	"IFCAIRTERMINALBOX":               {},
	"IFCDAMPER":                       {},
	"IFCELECTRICDISTRIBUTIONBOARD":    {},
	"IFCELECTRICTIMECONTROL":          {},
	"IFCFLOWMETER":                    {},
	"IFCPROTECTIVEDEVICE":             {},
	"IFCSWITCHINGDEVICE":              {},
	"IFCVALVE":                        {},
	"IFCFLOWFITTING":                  {},
	"IFCCABLECARRIERFITTING":          {},
	"IFCCABLEFITTING":                 {},
	"IFCDUCTFITTING":                  {},
	"IFCJUNCTIONBOX":                  {},
	"IFCPIPEFITTING":                  {},
	"IFCFLOWMOVINGDEVICE":             {},
	"IFCCOMPRESSOR":                   {},
	"IFCFAN":                          {},
	"IFCPUMP":                         {},
	"IFCFLOWSEGMENT":                  {},
	"IFCCABLECARRIERSEGMENT":          {},
	"IFCCABLESEGMENT":                 {},
	"IFCDUCTSEGMENT":                  {},
	"IFCPIPESEGMENT":                  {},
	"IFCFLOWSTORAGEDEVICE":            {},
	"IFCELECTRICFLOWSTORAGEDEVICE":    {},
	"IFCTANK":                         {},
	"IFCFLOWTERMINAL":                 {},
	"IFCAIRTERMINAL":                  {},
	"IFCAUDIOVISUALAPPLIANCE":         {},
	"IFCCOMMUNICATIONSAPPLIANCE":      {},
	"IFCELECTRICAPPLIANCE":            {},
	"IFCFIRESUPPRESSIONTERMINAL":      {},
	"IFCLAMP":                         {},
	"IFCLIGHTFIXTURE":                 {},
	"IFCMEDICALDEVICE":                {},
	"IFCOUTLET":                       {},
	"IFCSANITARYTERMINAL":             {},
	"IFCSPACEHEATER":                  {},
	"IFCSTACKTERMINAL":                {},
	"IFCWASTETERMINAL":                {},
	"IFCFLOWTREATMENTDEVICE":          {},
	"IFCDUCTSILENCER":                 {},
	"IFCFILTER":                       {},
	"IFCINTERCEPTOR":                  {},

	// IfcStructuralItem and IfcStructuralActivity, which an analysis model is.
	"IFCSTRUCTURALITEM":                 {},
	"IFCSTRUCTURALCONNECTION":           {},
	"IFCSTRUCTURALCURVECONNECTION":      {},
	"IFCSTRUCTURALPOINTCONNECTION":      {},
	"IFCSTRUCTURALSURFACECONNECTION":    {},
	"IFCSTRUCTURALMEMBER":               {},
	"IFCSTRUCTURALCURVEMEMBER":          {},
	"IFCSTRUCTURALCURVEMEMBERVARYING":   {},
	"IFCSTRUCTURALSURFACEMEMBER":        {},
	"IFCSTRUCTURALSURFACEMEMBERVARYING": {},
	"IFCSTRUCTURALACTIVITY":             {},
	"IFCSTRUCTURALACTION":               {},
	"IFCSTRUCTURALCURVEACTION":          {},
	"IFCSTRUCTURALLINEARACTION":         {},
	"IFCSTRUCTURALPLANARACTION":         {},
	"IFCSTRUCTURALPOINTACTION":          {},
	"IFCSTRUCTURALREACTION":             {},
	"IFCSTRUCTURALCURVEREACTION":        {},
	"IFCSTRUCTURALPOINTREACTION":        {},
	"IFCSTRUCTURALSURFACEREACTION":      {},
}
