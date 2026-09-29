// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"math"
	"slices"

	sexpr "github.com/z5labs/sexpr-go"
)

// Parameter is one parameter of a rule as data rather than as the text it was
// written in.
//
// It is the same parameter [Argument.String] renders, read the way the check
// reads it: a caller wanting the tolerance or the predicate a rule runs with
// takes it from here instead of parsing an s-expression back out of a string.
//
// It is one shape whatever the check declares. A parameter taking one value
// carries one, and a repeated one carries each of them, whether it was written
// as a sequence after its tag or as one parenthesised list — specification
// section 6.8 permits both, and they are one parameter.
//
// Nothing a value names is resolved. A tolerance is its name here, and what it
// was decided against is [Band.Floor]'s to report.
type Parameter struct {
	// Name is the tag the parameter was written with.
	Name string `json:"name"`

	// Type is what the check declares the parameter takes. It is empty where
	// the check declares no parameter by that name, which only a model the load
	// refused can hold.
	Type ParameterType `json:"type,omitempty"`

	// Values are the values written for it, one element per value and in the
	// order written: a float64 for a [ParameterReal], a bool for a
	// [ParameterBoolean], and for every other type a string holding the name,
	// the id or the text as written, unquoted.
	//
	// A value which is not an atom of the declared type is nil. Like an
	// undeclared name, that is only possible in a model the load refused, and a
	// nil is what says so rather than a guess at what was meant.
	Values []any `json:"values"`
}

// Parameters returns the parameters the rule was written with, as data, in the
// order they were written: Parameters()[i] and Arguments[i] are one parameter.
//
// It is nil for a rule written with no parameters.
func (r Rule) Parameters() []Parameter {
	return parametersOf(r.Arguments, r.Check)
}

// parametersOf reads every parameter of one rule as the check declares it.
//
// It is the one function every report of a rule is filled from — a violation, a
// band, an edge read straight, a curve drawn, and a listing — so that no two of
// them can disagree about what a rule was written with.
func parametersOf(arguments []Argument, check CheckDeclaration) []Parameter {
	if len(arguments) == 0 {
		return nil
	}

	out := make([]Parameter, 0, len(arguments))
	for _, argument := range arguments {
		out = append(out, argument.parameter(check))
	}
	return out
}

// parameter reads one parameter as the check declares it.
func (a Argument) parameter(check CheckDeclaration) Parameter {
	declared, ok := check.Parameter(a.Name)
	if !ok {
		// With no declaration there is no type any value could be an atom of,
		// and no telling whether a list is a repeated parameter's values or a
		// value in its own right. Each value written is reported as unreadable
		// rather than guessed at.
		return Parameter{Name: a.Name, Values: make([]any, len(a.Values))}
	}

	values := parameterValues(a, declared)

	out := Parameter{
		Name:   a.Name,
		Type:   declared.Type,
		Values: make([]any, 0, len(values)),
	}
	for _, value := range values {
		out.Values = append(out.Values, parameterValue(value, declared.Type))
	}
	return out
}

// parameterValue is one value of a parameter as data, or nil where it is not an
// atom of the type the check declares.
//
// It accepts what the load's validation accepts and nothing else, without
// resolving a name against a registry: a model the load accepted gives every
// value back, and one it refused gives back a nil wherever the value could not
// have been read as what the check takes.
func parameterValue(node *Node, declared ParameterType) any {
	switch declared {
	case ParameterReal:
		number, ok := node.Datum.(sexpr.Float)
		if !ok || math.IsInf(number.Value, 0) || math.IsNaN(number.Value) {
			return nil
		}
		return number.Value

	case ParameterBoolean:
		boolean, ok := node.Datum.(sexpr.Bool)
		if !ok {
			return nil
		}
		return boolean.Value

	case ParameterString:
		text, ok := node.Datum.(sexpr.String)
		if !ok {
			return nil
		}
		return text.Value
	}

	symbol, ok := node.Datum.(sexpr.Symbol)
	if !ok {
		return nil
	}

	switch declared {
	case ParameterID, ParameterFrame:
		if _, err := ParseID(symbol.Value); err != nil {
			return nil
		}

	case ParameterKind:
		if !slices.Contains(kinds, Kind(symbol.Value)) {
			return nil
		}

	case ParameterGeometry:
		if !slices.Contains(geometries, Geometry(symbol.Value)) {
			return nil
		}

	case ParameterTypeName, ParameterPredicate, ParameterTolerance:
		// A registry name is any symbol here. Whether the registry declares
		// it is the load's question, and resolving it is not what this reports.

	default:
		// A type outside the closed set is a declaration no registered check
		// makes, and nothing written against it has a reading.
		return nil
	}

	return symbol.Value
}
