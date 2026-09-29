// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import "github.com/z5labs/dfcad"

// declaredTolerances refuses a tolerance name the registry does not declare,
// in the error scaffold-loop gives for the same mistake: a
// [dfcad.UnknownAxisError] on the tolerance axis, carrying every name the
// registry does declare.
//
// Every command which takes a tolerance by name — --tolerance and --chord on
// measure, tessellate, plan, site, buildable, export and export-map — calls it
// once, after the model loads and before anything is derived from it, and a
// command which takes a tolerance by name later gets the same refusal by
// calling it too. Without it an undeclared name is answered by whatever happens
// to read it: nothing at all where nothing does, and where something does, one
// diagnostic per node pointing into a model which has nothing wrong with it and
// an exit code telling a CI job the model failed its check. The mistake is on
// the command line, so it is refused there, once.
//
// Only the name is checked. A declared tolerance in a unit other than a node's
// frame is still reported where it is read, because whether it fits depends on
// the frame each node is in, and that is a fact about the model.
//
// A name which was not given is not checked: whether a flag is required is the
// command's to say, through [vocabularyOf] and its kin, before the load.
func declaredTolerances(registry *dfcad.Registry, names ...string) error {
	for _, name := range names {
		if name == "" || registry.Declares(dfcad.SortTolerance, name) {
			continue
		}
		return dfcad.UnknownAxisError{
			Axis:      string(dfcad.SortTolerance),
			Value:     name,
			Permitted: registry.Names(dfcad.SortTolerance),
		}
	}
	return nil
}
