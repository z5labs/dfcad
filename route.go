// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// Subject is the new node a routing decision is about.
//
// It is the three axes a node has before it has anything else — the namespace
// of its id, its kind and its type — because those are the three a routing rule
// matches on and the three an author has already decided by the time they ask
// where the node goes. Nothing here is a [SemanticNode]: the node being routed
// does not exist yet, which is the whole reason the question is being asked.
type Subject struct {
	// ID is the id the node will be written with. Its namespace is what a rule
	// matching on one compares against.
	ID ID

	// Kind is the kind it will declare.
	Kind Kind

	// Type is the type name it will declare.
	Type string
}

// String spells the subject for a message about it, which names every axis it
// has because a refusal to route says which node it could not place.
//
// An axis a geometric node does not have is not printed as an empty one. "kind
// , type " reads as something the caller forgot rather than as the vertex it
// describes, and a vertex has no kind to have forgotten.
func (s Subject) String() string {
	var axes []string
	if s.Kind != "" {
		axes = append(axes, "kind "+string(s.Kind))
	}
	if s.Type != "" {
		axes = append(axes, "type "+s.Type)
	}

	if len(axes) == 0 {
		return fmt.Sprintf("%s (no kind and no type)", s.ID)
	}
	return fmt.Sprintf("%s (%s)", s.ID, strings.Join(axes, ", "))
}

// Destination is the file a new node is written to, and what chose it.
//
// The rule is carried beside the path because "where did this go" and "why
// there" are one question to whoever reads the answer. A destination which came
// from a `--file` override names no rule and says so, which is what keeps an
// override from reading as a rule nobody can find in the registry.
type Destination struct {
	// Path is the target file, relative to the model root and written with
	// forward slashes.
	Path string `json:"path"`

	// Rule is the name of the routing rule which chose it. Empty when the
	// destination was overridden.
	Rule string `json:"rule,omitempty"`

	// Overridden reports whether the destination was named outright rather than
	// routed.
	Overridden bool `json:"overridden"`
}

// RoutingError reports a new node the routing rules do not place.
//
// Both reasons are the same failure seen from two sides — the rules do not say
// where this node goes — and both are refusals rather than defaults. A model
// which files an unmatched node somewhere plausible is a model in which the
// rules describe part of the tree and nobody knows which part
// ([0015](docs/decisions/0015-the-cli-is-the-primary-write-path.md)); the fix
// is a rule, and a fix nobody is told to make is one nobody makes.
type RoutingError struct {
	// Subject is the node which could not be placed.
	Subject Subject

	// Matched are the rules which matched it, in name order. It holds two or
	// more for an ambiguous routing and none for an unmatched one, which is what
	// tells the two apart without matching a message.
	Matched []Route

	// Consulted is every routing rule the registry declares, in name order. It
	// is the whole set rather than the ones which matched, because the answer to
	// an unmatched node is what the rules do say — which is where the missing
	// rule goes.
	Consulted []Route
}

// Error implements the [error] interface.
func (e RoutingError) Error() string {
	if len(e.Matched) > 1 {
		return fmt.Sprintf(
			"%s matches more than one routing rule: %s",
			e.Subject, strings.Join(rules(e.Matched), ", "),
		)
	}

	if len(e.Consulted) == 0 {
		return fmt.Sprintf("%s matches no routing rule: this model declares none at all", e.Subject)
	}

	return fmt.Sprintf(
		"%s matches no routing rule: the rules consulted were %s",
		e.Subject, strings.Join(rules(e.Consulted), ", "),
	)
}

// Ambiguous reports whether more than one rule matched, as opposed to none.
func (e RoutingError) Ambiguous() bool { return len(e.Matched) > 1 }

// rules spells a set of routing rules as "name (criteria) -> file", which is
// what makes a refusal actionable: the rule which should have matched and did
// not is read off the list rather than looked up in the registry.
func rules(routes []Route) []string {
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, fmt.Sprintf("%s (%s) -> %s", route.Name, route.criteria(), route.File))
	}
	return out
}

// OverrideError reports a `--file` which names somewhere a node may not be
// written.
//
// It is the same judgement [Tx.Insert] makes of a target path, made before the
// model is loaded rather than after, so that an invocation naming an impossible
// file is answered as the wrong invocation it is rather than as a change which
// was refused.
type OverrideError struct {
	// Path is the file, as it was asked for.
	Path string

	// Err is why it was refused: [ErrOutsideModel] or [ErrNotAnEntityFile].
	Err error
}

// Error implements the [error] interface.
func (e OverrideError) Error() string {
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

// Unwrap returns the underlying failure.
func (e OverrideError) Unwrap() error {
	return e.Err
}

// Route returns the destination of a `--file` override, checking that it names
// somewhere a node may actually be written.
//
// A relative path is kept as written, cleaned, because it is resolved against
// the model root by whoever writes it. An absolute one is refused rather than
// silently accepted: the routing rules are relative, the model root is where
// every path in this format is measured from, and a caller which has an
// absolute path already knows the root to make it relative to.
func Override(file string) (Destination, error) {
	clean := path.Clean(file)

	switch {
	case file == "", path.IsAbs(file), clean == "..", strings.HasPrefix(clean, "../"):
		return Destination{}, OverrideError{Path: file, Err: ErrOutsideModel}
	case path.Ext(clean) != Extension:
		return Destination{}, OverrideError{Path: file, Err: ErrNotAnEntityFile}
	}

	return Destination{Path: clean, Overridden: true}, nil
}

// Destination returns the file a new node belongs in, per the routing rules the
// registry declares.
//
// Exactly one rule must match. A node matched by none and a node matched by
// several are both a [RoutingError] naming the node and the rules consulted,
// and neither is resolved by picking one — a rule chosen by being written first,
// or by being the most specific, is a filing decision made by the tool and
// visible in nothing the author wrote. Making both a refusal is what keeps the
// registry the whole answer to where things go: rules which overlap are made
// disjoint, and a node nothing covers gets a rule.
//
// The path which comes back is relative to the model root. Turning it into a
// file is [Tx.Insert]'s job, which resolves it against the root the transaction
// holds.
func (r *Registry) Destination(subject Subject) (Destination, error) {
	consulted := slices.Collect(r.Routes())

	var matched []Route
	for _, route := range consulted {
		if route.Matches(subject) {
			matched = append(matched, route)
		}
	}

	if len(matched) != 1 {
		return Destination{}, RoutingError{Subject: subject, Matched: matched, Consulted: consulted}
	}

	return Destination{Path: matched[0].File, Rule: matched[0].Name}, nil
}
