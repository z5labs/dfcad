// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"strconv"
	"strings"
)

// SpecVersion is the version of the entity format specification this engine
// implements, in the MAJOR.MINOR form SPEC.md section 10 defines.
//
// It is a fact about the engine rather than about any file it reads: models
// carry no version stamp, deliberately, so a consumer that needs to know which
// dialect of the format it is holding reads it from here — or, from the command
// line, out of the object `dfcad version` writes. That is the reader SPEC.md
// section 10 sends here, and it is why the constant is exported from the root
// package rather than kept unexported beside the loader.
//
// It moves under the rules SPEC.md section 10 states, and it moves independently
// of the version of the tool and of the version of the machine output contract.
// docs/versioning.md is the relationship between the three.
const SpecVersion = "1.2"

// EntityFormat is one version of the entity format specification: the
// MAJOR.MINOR pair SPEC.md section 10 states at the top of itself.
//
// It is [SpecVersion] with the two components apart, because the two are
// compared rather than displayed. Whether a model loads is a question about the
// MAJOR on its own and about the MINOR under it, and a string comparison
// answers neither: "1.10" sorts before "1.9" and is the later of the two.
type EntityFormat struct {
	// Major is the component which moves when a currently valid file stops
	// loading, stops meaning what it meant, or stops printing to the same
	// bytes.
	Major int

	// Minor is the component which moves when an optional child, a value shape
	// or a whole new form is added. Files which loaded still load.
	Minor int
}

// String implements [fmt.Stringer], and is the one spelling of a version: the
// one SPEC.md states and the one [ParseEntityFormat] reads back.
func (f EntityFormat) String() string {
	return strconv.Itoa(f.Major) + "." + strconv.Itoa(f.Minor)
}

// LoadsUnder reports whether a model authored against f loads under an engine
// implementing engine.
//
// It is the rule of SPEC.md section 10 read from the other end. A MINOR bump
// adds an optional child, a value shape or a whole new form, so an engine loads
// every model of its own MAJOR up to and including its own MINOR: a 1.1 model
// holds nothing a 1.2 engine does not know. A model at a later MINOR may hold a
// form the engine has never heard of, and a MAJOR apart in either direction is
// the version effect of "anything that makes a currently valid file fail to
// load" — so neither loads.
//
// Being unable to load the *later* format is the obvious half. The other half
// is deliberate rather than conservative: a newer engine reading an older MAJOR
// is exactly the case that MAJOR was moved for, and reading such a file with
// this engine's rules would give it a meaning its author did not write.
func (f EntityFormat) LoadsUnder(engine EntityFormat) bool {
	return f.Major == engine.Major && f.Minor <= engine.Minor
}

// SpecFormat is the entity format this engine implements: [SpecVersion],
// parsed.
//
// It is a function rather than a variable so that no caller can assign to it.
// The version this engine implements is compiled in, and an engine which could
// be told it implements something else would report that to everybody who asked
// it — which is the failure this whole check exists to make impossible.
func SpecFormat() EntityFormat {
	// SpecVersion is a constant of this package, and spec_test.go requires it to
	// parse and to be what SPEC.md declares. Nothing a caller does reaches this.
	format, _ := ParseEntityFormat(SpecVersion)
	return format
}

// entityFormatSpelling is how a version is written, as somebody writing one
// reads it, which is what a message about one which is not says.
const entityFormatSpelling = "MAJOR.MINOR"

// MalformedEntityFormatError reports something written where an entity format
// version belongs which is not one.
//
// There is one spelling of a version and it is SPEC.md's, so the parse and the
// refusal are here rather than in each caller: a command taking one on its
// command line and a consumer asserting one from Go are held to the same
// spelling, and neither has a second copy of it to drift from.
type MalformedEntityFormatError struct {
	// Written is what was there instead.
	Written string
}

// Error implements the [error] interface.
func (e MalformedEntityFormatError) Error() string {
	return fmt.Sprintf(
		"malformed entity format %s: a version is written as %s, two decimal components and nothing else",
		strconv.Quote(e.Written), entityFormatSpelling,
	)
}

// ParseEntityFormat reads a version written the one way SPEC.md section 10
// writes one.
//
// What comes back prints as what went in, and what does not print as what went
// in is refused: `1.02`, `01.2`, `+1.2` and `1.2.3` are each a version somebody
// meant and none of them is one this format has a spelling for. Accepting them
// would mean two spellings of one version, which is the beginning of a
// comparison that depends on which of them a caller happened to write.
func ParseEntityFormat(written string) (EntityFormat, error) {
	malformed := MalformedEntityFormatError{Written: written}

	major, minor, ok := strings.Cut(written, ".")
	if !ok {
		return EntityFormat{}, malformed
	}

	format := EntityFormat{}

	var err error
	if format.Major, err = strconv.Atoi(major); err != nil {
		return EntityFormat{}, malformed
	}
	if format.Minor, err = strconv.Atoi(minor); err != nil {
		return EntityFormat{}, malformed
	}

	// A negative component survives the round trip below, because a minus sign
	// prints back exactly as it was written. There is no version behind such a
	// component to compare against, so it is refused here rather than left to a
	// comparison which would quietly call it earlier than every real one.
	if format.Major < 0 || format.Minor < 0 {
		return EntityFormat{}, malformed
	}

	// The round trip is the rest of the rule: a leading zero, a plus sign, or
	// anything else Atoi tolerated which SPEC.md does not write comes back
	// spelled differently from how it went in.
	if format.String() != written {
		return EntityFormat{}, malformed
	}

	return format, nil
}

// UnsupportedEntityFormatError reports a model authored against an entity
// format this engine does not implement.
//
// It carries both versions because naming one of them answers half the
// question: an author told only that their model is 1.3 does not know whether
// to upgrade the engine or to stop writing 1.3, and an author told only that
// the engine is 1.2 has been told a fact about a binary rather than about their
// model.
type UnsupportedEntityFormatError struct {
	// Model is the format the model was authored against, as its author
	// asserted it.
	Model EntityFormat

	// Engine is the format this engine implements, which is [SpecFormat].
	Engine EntityFormat
}

// Error implements the [error] interface.
//
// The two readings are separate sentences because they are fixed in different
// places. An engine behind its model is upgraded; a MAJOR apart is a model and
// an engine which were never going to meet, and the fix is to pin the engine
// the model was written for.
func (e UnsupportedEntityFormatError) Error() string {
	if e.Model.Major != e.Engine.Major {
		return fmt.Sprintf(
			"model authored against entity format %s: this engine implements %s, and a file written for one major version does not load under an engine of another",
			e.Model, e.Engine,
		)
	}

	return fmt.Sprintf(
		"model authored against entity format %s: this engine implements %s, which is older, so the model may hold forms this engine would report as unknown",
		e.Model, e.Engine,
	)
}

// AssertEntityFormat reports whether a model authored against model loads under
// this engine.
//
// This is the engine noticing, which is the only place the noticing can happen:
// files carry no version stamp, deliberately (SPEC.md section 10), so a model at
// a format this engine does not implement is indistinguishable from a model with
// a misspelled tag in it — the loader reports the first form it does not
// recognise, which points the author at their file rather than at their engine.
// A consumer which knows which format it authored against says so, and gets that
// answer instead.
//
// The assertion is about the format and not about any particular file, so it is
// answered before a byte of the model is read. A run which cannot load the
// model's format has nothing to say about the model's contents, and reporting on
// them anyway is how a check which ran no rule comes to look like a model with
// nothing wrong with it.
func AssertEntityFormat(model EntityFormat) error {
	engine := SpecFormat()
	if model.LoadsUnder(engine) {
		return nil
	}
	return UnsupportedEntityFormatError{Model: model, Engine: engine}
}
