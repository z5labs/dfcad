// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"strings"

	"github.com/z5labs/dfcad"
	"github.com/z5labs/dfcad/ifc"
)

// The flags export takes to name the vocabulary a coordinate reference system
// is written under, named here because the usage and the errors which refuse
// them name them.
const (
	flagCRS           = "crs"
	flagCRSDefinition = "crs-definition"
)

// georeference is the vocabulary a run reads a coordinate reference system
// under, and it has no default and never will
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
//
// Which predicate a project writes its coordinate reference system under is
// that project's word for it, exactly as which predicate carries a room's
// height is. A default here would put a georeference into the artefacts of
// every model which happened to use the same spelling for something else.
type georeference struct {
	// identifier is the predicate the identifier of the system is written
	// under. A run which names none exports without a georeference.
	identifier string

	// definition is the predicate the full definition of the system is written
	// under, where the project holds one. It is of no use without the
	// identifier.
	definition string
}

// named reports whether the run asked for a georeference at all.
func (g georeference) named() bool { return g.identifier != "" }

// UnusableCRSVocabularyError is a run which named the predicate a coordinate
// reference system is defined under without naming the one it is identified
// under.
//
// It is a usage error rather than a definition quietly dropped. IfcProjectedCRS
// is named and its definition is optional, so a definition with nothing to
// attach it to is not something this can write — and a run which asked for a
// georeference and got none would have to find that out by opening the file.
type UnusableCRSVocabularyError struct{}

// Error implements [error].
func (UnusableCRSVocabularyError) Error() string {
	return fmt.Sprintf(
		"expected the predicate a coordinate reference system is identified under alongside --%s, found no --%s: a "+
			"definition is written beside the identifier and there is nothing to write it beside",
		flagCRSDefinition, flagCRS,
	)
}

// crsVocabularyOf reports a run which asked for a definition without saying
// what it defines.
func crsVocabularyOf(named georeference) error {
	if named.definition != "" && named.identifier == "" {
		return UnusableCRSVocabularyError{}
	}
	return nil
}

// recordedCRS is where the model says it sits on the earth, in the model's own
// terms rather than in any one format's.
//
// It is two strings and no more, because two strings is the whole of what this
// tool holds about a coordinate reference system: an entry in somebody else's
// register, and that register's own text about it. Which field of which format
// each becomes is that format's business, which is what keeps a second
// exporter from re-reading the model to find out where it is.
type recordedCRS struct {
	// Identifier is the authority and code naming the system.
	Identifier string

	// Definition is the register's full definition of it, where the project
	// holds one, exactly as it was written.
	Definition string

	// Frame is the frame the system was read off, which is the root and is
	// refused anywhere else. It is carried because the conversion into the
	// system is the offset between this frame and the frame the coordinates
	// were written in, and a record which did not say which frame it describes
	// could not be asked for that offset.
	Frame dfcad.ID

	// Span is where that frame was declared, which is what a refusal about the
	// offset points at.
	Span dfcad.Span
}

// projected is the record as IFC holds it: an IfcProjectedCRS naming the system
// and an IfcMapConversion into it.
//
// at is where the coordinates this file holds sit in that system, which is what
// the conversion states. It comes in rather than being assumed: the root frame
// is the projected coordinate reference system the chain is rooted at
// ([SPEC §7.5](SPEC.md#75-frame)) and every coordinate in the file is written
// in the root frame
// ([0024](docs/decisions/0024-every-coordinate-in-an-export-is-written-in-the-root-frame.md)),
// so the offset is nothing — but it is nothing because those two facts were
// checked and not because the writer decided so. The export which assumed it
// wrote an identity conversion over coordinates nothing had carried, which
// placed every room of a levelled model at the system's origin.
//
// The three factors stay absent whatever comes in. A rotation or a scale
// between the model and the system would be geodesy, which this engine does
// not do ([0023](docs/decisions/0023-the-map-export-names-its-coordinate-system-in-the-file.md)),
// so there is no arrangement of frames which produces one to write.
func (s *recordedCRS) projected(at dfcad.Point) *ifc.Georeference {
	if s == nil {
		return nil
	}

	return &ifc.Georeference{
		CRS: ifc.ProjectedCRS{
			// Copied byte for byte, both of them. What the identifier names and
			// what the definition says are the register's business, and this is
			// the one place a transcription error could be introduced.
			Name:        s.Identifier,
			Description: s.Definition,
		},
		Conversion: ifc.MapConversion{
			Eastings:         at[0],
			Northings:        at[1],
			OrthogonalHeight: at[2],
		},
	}
}

// srsName is the record as a vector format holds it: the identifier, and
// nothing else.
//
// The definition has nowhere to go and is dropped rather than folded in
// somewhere it would not be read. GML names a system and does not carry a
// register's text about it, so a definition written into an attribute or a
// property would be a field this format's readers do not look at — and a
// caller which found it there would have to decide whether it was the
// authority on anything, which is exactly the question a name answers.
func (s *recordedCRS) srsName() string {
	if s == nil {
		return ""
	}

	return s.Identifier
}

// georeferenced is where the model says it sits on the earth, and whatever
// stopped it saying so.
//
// The identifier is recorded and never interpreted. Interpreting it would mean
// a geodetic library, which means cgo, which breaks the static image this tool
// ships as, and a licensed parameter dataset besides — for a capability no
// answer here needs, because every cross-frame answer in this engine is a
// similarity transform in the plane the survey was already projected into.
func georeferenced(
	registry *dfcad.Registry,
	frames *dfcad.Frames,
	named georeference,
) (*recordedCRS, []dfcad.Diagnostic) {
	if !named.named() {
		return nil, nil
	}

	root, rooted := frames.Root()

	diags := misplacedCRS(registry, root, rooted, named)

	if !rooted {
		return nil, diags
	}

	// Every check below runs whatever the ones before it found. An identifier
	// which is not one and a definition in the wrong unit are two independent
	// mistakes, and reporting the first and stopping turns fixing them into a
	// guessing loop.
	identifier, identifiers, refused := oneValue(root, named.identifier, flagCRS)
	diags = append(diags, refused...)

	definition, definitions, refused := oneValue(root, named.definition, flagCRSDefinition)
	diags = append(diags, refused...)

	text := ""
	if identifiers == 1 {
		holds, ok := identifier.Text()
		switch {
		case !ok:
			diags = append(diags, wrongShape(identifier, named.identifier,
				"the identifier of a coordinate reference system",
				fmt.Sprintf("%s carries a string: (%s \"EPSG:6543\")", named.identifier, named.identifier)))
		default:
			if diagnostic := malformedIdentifier(holds, identifier.Span(), named.identifier); diagnostic != nil {
				diags = append(diags, *diagnostic)
				break
			}
			text = holds
		}
	}

	written := ""
	if definitions == 1 {
		holds, ok := definition.Text()
		switch {
		case !ok:
			diags = append(diags, wrongShape(definition, named.definition,
				"the definition of a coordinate reference system",
				fmt.Sprintf("%s carries a string holding the definition verbatim", named.definition)))
		default:
			if diagnostic := mismatchedUnit(holds, root, definition.Span()); diagnostic != nil {
				diags = append(diags, *diagnostic)
				break
			}
			written = holds
		}
	}

	// Gated on the identifier being absent rather than on its being unusable.
	// A frame carrying two of them has already been told it carries two, and
	// adding "only the definition" beside that would name a mistake nobody
	// made.
	if definitions == 1 && identifiers == 0 {
		diags = append(diags, dfcad.Diagnostic{
			Severity: dfcad.SeverityError,
			Span:     definition.Span(),
			Message: fmt.Sprintf(
				"expected the coordinate reference system to be identified under %s beside its definition, found "+
					"only the definition: a projected system is written into the artefact by name",
				named.identifier),
			Hint: fmt.Sprintf("write (%s \"EPSG:6543\") on the frame %s", named.identifier, root.ID),
		})
	}

	if len(diags) > 0 || text == "" {
		return nil, diags
	}

	return &recordedCRS{Identifier: text, Definition: written, Frame: root.ID, Span: root.Span}, nil
}

// wrongShape is the diagnostic for a plain value which is not the text the
// vocabulary needs.
//
// It says what was written rather than only what was not, because a coordinate
// where a string belongs is a predicate declared for something else, and the
// shape it was found in is what leads to that declaration.
func wrongShape(value dfcad.Value, predicate, what, hint string) dfcad.Diagnostic {
	return dfcad.Diagnostic{
		Severity: dfcad.SeverityError,
		Span:     value.Span(),
		Message: fmt.Sprintf("expected %s under %s, found a %s value",
			what, predicate, spellShape(value.Shape())),
		Hint: hint,
	}
}

// misplacedCRS reports every frame other than the root which carries the
// vocabulary a coordinate reference system is written under.
//
// A coordinate reference system describes the root of the chain and nothing
// else. Every other frame is expressed relative to its parent by a measured
// transform, so one carrying a projected system would be naming a second
// georeference for the same model — and the two would not have to agree.
func misplacedCRS(registry *dfcad.Registry, root dfcad.Frame, rooted bool, named georeference) []dfcad.Diagnostic {
	var diags []dfcad.Diagnostic

	for frame := range registry.Frames() {
		if rooted && frame.ID == root.ID {
			continue
		}

		for _, predicate := range []string{named.identifier, named.definition} {
			if predicate == "" {
				continue
			}

			for _, value := range frame.Plain(predicate) {
				related := []dfcad.RelatedLocation{{Span: frame.Span, Message: "the frame is declared here"}}
				hint := fmt.Sprintf("write %s on the frame this model is rooted at", predicate)

				if rooted {
					related = append(related, dfcad.RelatedLocation{
						Span:    root.Span,
						Message: "the root of the chain is here",
					})
					hint = fmt.Sprintf("move %s to the frame %s", predicate, root.ID)
				}

				diags = append(diags, dfcad.Diagnostic{
					Severity: dfcad.SeverityError,
					Span:     value.Span(),
					Message: fmt.Sprintf(
						"expected a coordinate reference system on the root frame, found %s on %s: a projected system "+
							"is what the chain is rooted at, and every other frame reaches it through a measured "+
							"transform",
						predicate, frame.ID),
					Hint:    hint,
					Related: related,
				})
			}
		}
	}

	return diags
}

// oneValue is the single plain value written on the root frame under predicate,
// together with how many were written.
//
// The count is returned rather than a bare "there is one" because none and too
// many are different facts about the model, and a caller which folded them
// together would report a frame carrying two identifiers as carrying none.
//
// Repeating a predicate is the ordinary case in this format — two width claims
// on a node are two measurements — but a model is rooted at one coordinate
// reference system, so two of them is a disagreement rather than a pair of
// readings. There is nothing here which could choose between them and choosing
// would put a georeference into the artefact which half the model contradicts.
func oneValue(root dfcad.Frame, predicate, flag string) (dfcad.Value, int, []dfcad.Diagnostic) {
	if predicate == "" {
		return dfcad.Value{}, 0, nil
	}

	values := root.Plain(predicate)

	switch len(values) {
	case 0:
		return dfcad.Value{}, 0, nil
	case 1:
		return values[0], 1, nil
	}

	related := make([]dfcad.RelatedLocation, 0, len(values)-1)
	for _, value := range values[1:] {
		related = append(related, dfcad.RelatedLocation{Span: value.Span(), Message: "and here"})
	}

	return dfcad.Value{}, len(values), []dfcad.Diagnostic{{
		Severity: dfcad.SeverityError,
		Span:     values[0].Span(),
		Message: fmt.Sprintf(
			"expected one %s on the frame %s, found %d: a model is rooted at one coordinate reference system and "+
				"there is nothing here which could choose between two",
			predicate, root.ID, len(values)),
		Hint:    fmt.Sprintf("--%s names one predicate and the frame carries it once", flag),
		Related: related,
	}}
}

// malformedIdentifier reports an identifier which is not an authority and a
// code, and reports nothing else about it.
//
// The shape is the whole of the validation. Whether EPSG:6543 exists, what it
// projects and where it applies are questions for a register this tool does not
// carry and will not resolve — so the check is that somebody wrote down which
// register to ask and what to ask it for, which is the part a typo breaks.
func malformedIdentifier(identifier string, at dfcad.Span, predicate string) *dfcad.Diagnostic {
	authority, code, split := strings.Cut(identifier, ":")

	wrong := ""
	switch {
	case !split:
		wrong = "no authority"
	case authority == "":
		wrong = "an empty authority"
	case code == "":
		wrong = "an empty code"
	case strings.Contains(code, ":"):
		wrong = "more than one authority"
	case strings.ContainsFunc(identifier, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }):
		wrong = "white space in it"
	}

	if wrong == "" {
		return nil
	}

	return &dfcad.Diagnostic{
		Severity: dfcad.SeverityError,
		Span:     at,
		Message: fmt.Sprintf(
			"expected an authority and a code under %s, found %s in %q: nothing here resolves the identifier, so its "+
				"shape is the whole of what can be checked about it",
			predicate, wrong, identifier),
		Hint: "an identifier is written <authority>:<code>, as in EPSG:6543",
	}
}

// mismatchedUnit reports a definition whose linear unit is not the one the
// frame is authored in.
//
// The token is compared and the number beside it is not. A US survey foot is
// exactly 1200/3937 m and there are several correct decimal spellings of that
// which differ in their last bits, so a writer comparing the factor would
// refuse definitions which are right — while a definition saying `metre` over a
// frame authored in feet is wrong however it spells the number.
//
// A definition stating no linear unit token this recognises is copied
// unchecked. The check is a real disagreement caught where there is one, not a
// requirement that every notation a register writes be understood here.
func mismatchedUnit(definition string, root dfcad.Frame, at dfcad.Span) *dfcad.Diagnostic {
	unit, token, stated := definitionUnit(definition)
	if !stated || unit == root.Unit {
		return nil
	}

	return &dfcad.Diagnostic{
		Severity: dfcad.SeverityError,
		Span:     at,
		Message: fmt.Sprintf(
			"expected the coordinate reference system definition to be in %s, which the frame %s declares, found %q, "+
				"which is %s: the coordinates and the system they are projected in cannot be in two units",
			root.Unit, root.ID, token, unit),
		Hint: "the unit token is compared and the conversion factor beside it is not, because several correct " +
			"spellings of one factor differ in their last digits",
		Related: []dfcad.RelatedLocation{{Span: root.Span, Message: "the frame declares its unit here"}},
	}
}

// definitionUnit is the linear unit a definition states, the token it states it
// with, and whether it states one at all.
//
// The tokens are read out of the `UNIT[` and `LENGTHUNIT[` forms well known
// text writes them in, in the order they appear, and the first which names a
// linear unit wins: a projected system's own unit follows the geographic
// system it is projected from, whose unit is angular and is passed over here.
func definitionUnit(definition string) (dfcad.Unit, string, bool) {
	for _, token := range unitTokens(definition) {
		normalised := normalisedToken(token)

		for _, unit := range []dfcad.Unit{
			dfcad.UnitMillimetre,
			dfcad.UnitCentimetre,
			dfcad.UnitMetre,
			dfcad.UnitKilometre,
			dfcad.UnitFoot,
			dfcad.UnitSurveyFoot,
		} {
			for _, spelling := range linearUnitTokens[unit] {
				if normalised == spelling {
					return unit, token, true
				}
			}
		}
	}

	return "", "", false
}

// unitTokens are the quoted names of every unit form in a definition, in the
// order they were written.
func unitTokens(definition string) []string {
	const marker = "UNIT["

	var out []string

	upper := strings.ToUpper(definition)
	for at := 0; ; {
		found := strings.Index(upper[at:], marker)
		if found < 0 {
			return out
		}
		at += found + len(marker)

		if at >= len(definition) || definition[at] != '"' {
			continue
		}
		at++

		end := strings.IndexByte(definition[at:], '"')
		if end < 0 {
			return out
		}

		out = append(out, definition[at:at+end])
		at += end + 1
	}
}

// normalisedToken is a unit token with everything which is not a letter or a
// digit taken out, and the letters lowered.
//
// `US survey foot`, `US_survey_foot` and `ftUS` are the same unit written three
// ways by three registers, and none of the three is more correct than the
// others. Comparing what is left after the spacing and the case is what makes
// the check about the unit rather than about a register's punctuation.
func normalisedToken(token string) string {
	var out strings.Builder

	for _, r := range strings.ToLower(token) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
		}
	}

	return out.String()
}

// linearUnitTokens is how each of the engine's linear units is spelled in a
// coordinate reference system definition, normalised by [normalisedToken].
//
// The two feet are the reason this table is written out rather than derived
// from the unit names. `ft` and `usft` differ by two parts per million, which is
// invisible on a room and is four feet on a state plane coordinate
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)), and the registers
// spell them `foot` and `US survey foot` — so a table which mapped both to
// `foot` would agree with a definition which contradicts the model.
var linearUnitTokens = map[dfcad.Unit][]string{
	dfcad.UnitMillimetre: {"millimetre", "millimeter", "mm"},
	dfcad.UnitCentimetre: {"centimetre", "centimeter", "cm"},
	dfcad.UnitMetre:      {"metre", "meter", "m"},
	dfcad.UnitKilometre:  {"kilometre", "kilometer", "km"},
	dfcad.UnitFoot:       {"foot", "feet", "ft", "footinternational", "internationalfoot", "intlfoot"},
	dfcad.UnitSurveyFoot: {"ussurveyfoot", "ussurveyfeet", "surveyfoot", "footus", "ftus", "usft"},
}

// spellShape names a value's shape for a diagnostic which found the wrong one.
func spellShape(shape dfcad.Shape) string {
	if shape == "" {
		return "unreadable"
	}
	return string(shape)
}
