// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routeRegistry is the vocabulary the routing tests are judged against.
//
// The four rules are disjoint over what they cover, which is what a registry
// has to be for routing to answer at all: `rooms` and `corridors` share a kind
// and are told apart by their type, and `geometry` matches on a namespace alone
// because a vertex has neither of the other two axes to match on.
const routeRegistry = `(project
  (label "Routing fixture")
  (globalid-namespace "https://example.org/models/route"))

(namespace geom (description "Geometric nodes minted by this model."))
(namespace site (description "Semantic nodes minted by this model."))

(type Campus
  (kind Zone)
  (geometry absent)
  (description "A group of things administered together."))

(type Corridor
  (kind Space)
  (geometry area)
  (description "A circulation space between rooms."))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings."))

(route corridors
  (kind Space)
  (type Corridor)
  (file "entities/circulation.dfc")
  (description "Circulation, kept apart from the rooms it connects."))

(route geometry (namespace geom) (file "geometry/level-1.dfc"))

(route rooms (kind Space) (type MeetingRoom) (file "entities/level-1.dfc"))

(route zones (kind Zone) (file "entities/zones.dfc"))
`

// routes loads a registry from source, requiring it to load without complaint.
func routes(t *testing.T, src string) *Registry {
	t.Helper()

	root := tree(t, map[string]string{"registry.dfc": src})

	registry, diags := LoadRegistry(root)
	require.Empty(t, diags)
	require.NotNil(t, registry)

	return registry
}

func TestRegistryDestination(t *testing.T) {
	testCases := []struct {
		name     string
		subject  Subject
		expected Destination
	}{
		{
			name:     "routes a node by its kind and its type",
			subject:  Subject{ID: "site:S-101", Kind: KindSpace, Type: "MeetingRoom"},
			expected: Destination{Path: "entities/level-1.dfc", Rule: "rooms"},
		},
		{
			name:     "tells two rules of one kind apart by their type",
			subject:  Subject{ID: "site:S-102", Kind: KindSpace, Type: "Corridor"},
			expected: Destination{Path: "entities/circulation.dfc", Rule: "corridors"},
		},
		{
			name:     "matches a rule which leaves a criterion out",
			subject:  Subject{ID: "site:Z-01", Kind: KindZone, Type: "Campus"},
			expected: Destination{Path: "entities/zones.dfc", Rule: "zones"},
		},
		{
			name:     "routes a geometric node, which has neither a kind nor a type, by its namespace",
			subject:  Subject{ID: "geom:V-01"},
			expected: Destination{Path: "geometry/level-1.dfc", Rule: "geometry"},
		},
	}

	registry := routes(t, routeRegistry)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := registry.Destination(testCase.subject)

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestRegistryDestinationRefusesWhatTheRulesDoNotPlace is its own function
// because it asserts about a refusal rather than about an answer: what a caller
// reads is the node it could not place and the rules there were, and neither of
// those is a field of a [Destination].
func TestRegistryDestinationRefusesWhatTheRulesDoNotPlace(t *testing.T) {
	testCases := []struct {
		name              string
		registry          string
		subject           Subject
		expectedAmbiguous bool
		expectedMatched   []string
		expectedConsulted []string
	}{
		{
			name:              "refuses a node no rule matches, naming every rule consulted",
			registry:          routeRegistry,
			subject:           Subject{ID: "site:F-01", Kind: KindElement, Type: "Corridor"},
			expectedConsulted: []string{"corridors", "geometry", "rooms", "zones"},
		},
		{
			name:              "refuses a node no rule matches when the model declares no rule at all",
			registry:          routeRegistry[:strings.Index(routeRegistry, "(route corridors")],
			subject:           Subject{ID: "site:S-101", Kind: KindSpace, Type: "MeetingRoom"},
			expectedConsulted: nil,
		},
		{
			name: "refuses a node more than one rule matches, naming the ones which matched",
			registry: routeRegistry + `
(route everything (file "entities/misc.dfc"))
`,
			subject:           Subject{ID: "site:S-101", Kind: KindSpace, Type: "MeetingRoom"},
			expectedAmbiguous: true,
			expectedMatched:   []string{"everything", "rooms"},
			expectedConsulted: []string{"corridors", "everything", "geometry", "rooms", "zones"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := routes(t, testCase.registry)

			_, err := registry.Destination(testCase.subject)

			var refused RoutingError
			require.ErrorAs(t, err, &refused)

			assert.Equal(t, testCase.subject, refused.Subject)
			assert.Equal(t, testCase.expectedAmbiguous, refused.Ambiguous())
			assert.Equal(t, testCase.expectedMatched, named(refused.Matched))
			assert.Equal(t, testCase.expectedConsulted, named(refused.Consulted))

			// The node and the rules are in the message as well as in the
			// fields, because the message is what an author reads.
			assert.Contains(t, err.Error(), string(testCase.subject.ID))
		})
	}
}

// named is the name of each rule, which is what an expectation about a refusal
// reads as.
func named(routes []Route) []string {
	if len(routes) == 0 {
		return nil
	}

	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, route.Name)
	}
	return out
}

func TestOverride(t *testing.T) {
	testCases := []struct {
		name     string
		file     string
		expected Destination
	}{
		{
			name:     "takes the path as the destination, naming no rule",
			file:     "entities/somewhere-else.dfc",
			expected: Destination{Path: "entities/somewhere-else.dfc", Overridden: true},
		},
		{
			name:     "cleans the path, so that one file is one destination",
			file:     "entities/./nested/../level-1.dfc",
			expected: Destination{Path: "entities/level-1.dfc", Overridden: true},
		},
		{
			name:     "takes a file at the model root",
			file:     "level-1.dfc",
			expected: Destination{Path: "level-1.dfc", Overridden: true},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := Override(testCase.file)

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

// TestOverrideRefusesAFileTheModelWouldNotReadBack is its own function because
// it asserts about which rule the path broke rather than about a destination.
func TestOverrideRefusesAFileTheModelWouldNotReadBack(t *testing.T) {
	testCases := []struct {
		name     string
		file     string
		expected error
	}{
		{
			name:     "refuses a file whose extension no walk picks up",
			file:     "entities/level-1.txt",
			expected: ErrNotAnEntityFile,
		},
		{
			name:     "refuses a file with no extension at all",
			file:     "entities/level-1",
			expected: ErrNotAnEntityFile,
		},
		{
			name:     "refuses an absolute path, which is not measured from the model root",
			file:     "/srv/models/level-1.dfc",
			expected: ErrOutsideModel,
		},
		{
			name:     "refuses a path which climbs out of the model root",
			file:     "../elsewhere/level-1.dfc",
			expected: ErrOutsideModel,
		},
		{
			name:     "refuses an empty path, which names no file",
			file:     "",
			expected: ErrOutsideModel,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Override(testCase.file)

			var refused OverrideError
			require.ErrorAs(t, err, &refused)
			assert.Equal(t, testCase.file, refused.Path)
			assert.ErrorIs(t, err, testCase.expected)
		})
	}
}

func TestLoadRegistryReadsRoutingRules(t *testing.T) {
	testCases := []struct {
		name     string
		rule     string
		expected Route
	}{
		{
			name: "reads all three criteria and the file",
			rule: `(route rooms
  (namespace site)
  (kind Space)
  (type MeetingRoom)
  (file "entities/level-1.dfc")
  (description "Meeting rooms of level 1."))`,
			expected: Route{
				Name:        "rooms",
				Namespace:   "site",
				Kind:        KindSpace,
				Type:        "MeetingRoom",
				File:        "entities/level-1.dfc",
				Description: "Meeting rooms of level 1.",
			},
		},
		{
			name: "leaves a criterion which was not written empty, which matches anything",
			rule: `(route geometry (namespace geom) (file "geometry/level-1.dfc"))`,
			expected: Route{
				Name:      "geometry",
				Namespace: "geom",
				File:      "geometry/level-1.dfc",
			},
		},
		{
			name: "reads a rule with no criteria at all as the catch-all it is",
			rule: `(route everything (file "entities/misc.dfc"))`,
			expected: Route{
				Name: "everything",
				File: "entities/misc.dfc",
			},
		},
		{
			name: "cleans the path, so that one file is one destination",
			rule: `(route rooms (file "entities/./level-1.dfc"))`,
			expected: Route{
				Name: "rooms",
				File: "entities/level-1.dfc",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := routes(t, routeRegistryHead+testCase.rule+"\n")

			got, ok := registry.Route(testCase.expected.Name)
			require.True(t, ok)

			// The span is where it was written, which no expectation here can
			// predict and every other field is the point of.
			got.Span = Span{}
			assert.Equal(t, testCase.expected, got)

			assert.True(t, registry.Declares(SortRoute, testCase.expected.Name))
			assert.Equal(t, []string{testCase.expected.Name}, registry.Names(SortRoute))
			assert.Equal(t, []Route{got}, cleared(slices.Collect(registry.Routes())))
		})
	}
}

// routeRegistryHead is the least a registry can declare and still judge the
// rules written after it: the project, the namespaces a rule may name and the
// types it may name.
const routeRegistryHead = `(project
  (label "Routing fixture")
  (globalid-namespace "https://example.org/models/route"))

(namespace geom (description "Geometric nodes minted by this model."))
(namespace site (description "Semantic nodes minted by this model."))

(type MeetingRoom
  (kind Space)
  (geometry area)
  (description "An enclosed room used for meetings."))

`

// cleared drops the spans, so that an expectation is about what was declared
// rather than about where a fixture happened to put it.
func cleared(routes []Route) []Route {
	out := make([]Route, 0, len(routes))
	for _, route := range routes {
		route.Span = Span{}
		out = append(out, route)
	}
	return out
}

// TestRegistryDestinationIgnoresARuleItCouldNotRead is its own function because
// it is about a registry which did not load cleanly, which every other test here
// requires not to happen.
//
// A criterion which was written and could not be read is spelled, in the struct,
// exactly the way a criterion nobody wrote is spelled — and that spelling means
// "matches anything". A rule dropped into that state would file every node the
// broken axis was there to exclude, so it matches nothing instead and the
// diagnostic stands as the whole of the answer.
func TestRegistryDestinationIgnoresARuleItCouldNotRead(t *testing.T) {
	testCases := []struct {
		name string
		rule string
	}{
		{
			name: "does not widen a rule whose kind is not one of the seven",
			rule: `(route broken (kind Room) (file "entities/broken.dfc"))`,
		},
		{
			name: "does not route through a rule whose file is not a path a node may be written to",
			rule: `(route broken (kind Space) (file "entities/broken.txt"))`,
		},
		{
			name: "does not route through a rule whose file is not a string",
			rule: `(route broken (kind Space) (file broken))`,
		},
		{
			name: "does not widen a rule whose namespace is not a symbol",
			rule: `(route broken (namespace "site") (file "entities/broken.dfc"))`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := tree(t, map[string]string{
				"registry.dfc": routeRegistryHead + testCase.rule + "\n",
			})

			registry, diags := LoadRegistry(root)
			require.NotEmpty(t, diags, "the rule was reported")

			// The rule is declared — it has a name, and a second rule taking that
			// name is still a duplicate — and it places nothing.
			_, ok := registry.Route("broken")
			require.True(t, ok)

			for _, subject := range []Subject{
				{ID: "site:S-101", Kind: KindSpace, Type: "MeetingRoom"},
				{ID: "geom:V-01"},
			} {
				_, err := registry.Destination(subject)

				var refused RoutingError
				require.ErrorAsf(t, err, &refused, "routing %s", subject)
				assert.Empty(t, refused.Matched, "a rule which did not read matches nothing")
			}
		})
	}
}

// TestRoutingErrorsAreDistinguishable checks the one thing a caller branching on
// these errors needs: they are distinguishable by type, and an override failure
// still carries the reason a target was refused.
//
// What a registry loader makes of a rule it cannot use — a target no walk
// reaches, a criterion nothing declares — is diagnostics rather than an error,
// and is covered by testdata/registry/routes.
func TestRoutingErrorsAreDistinguishable(t *testing.T) {
	_, unmatched := routes(t, routeRegistryHead).Destination(Subject{ID: "site:S-101"})
	_, overridden := Override("notes.md")

	assert.True(t, errors.As(unmatched, &RoutingError{}))
	assert.False(t, errors.As(unmatched, &OverrideError{}))

	assert.True(t, errors.As(overridden, &OverrideError{}))
	assert.False(t, errors.As(overridden, &RoutingError{}))
	assert.ErrorIs(t, overridden, ErrNotAnEntityFile)
}
