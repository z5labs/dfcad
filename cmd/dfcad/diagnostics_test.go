// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/dfcad"
)

// carried is the diagnostics a run's object carries, decoded, together with
// the bytes they were written as.
//
// It requires that `diagnostics` is the last field of the object, which is
// where docs/machine-output.md says it is, and that it holds at least one
// entry: an object which carries the key carries it because something was
// rendered.
func carried(t *testing.T, stdout string) ([]dfcad.Diagnostic, json.RawMessage) {
	t.Helper()

	result := object(t, stdout)
	keys := objectKeys(t, json.RawMessage(stdout))
	require.NotEmpty(t, keys)
	require.Equal(t, "diagnostics", keys[len(keys)-1], "diagnostics is the last field of the object")

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(stdout), &fields))

	raw := fields["diagnostics"]
	require.NotNil(t, result["diagnostics"])

	var diagnostics []dfcad.Diagnostic
	require.NoError(t, json.Unmarshal(raw, &diagnostics))
	require.NotEmpty(t, diagnostics, "an object carries diagnostics only where the run rendered one")

	return diagnostics, raw
}

// rerendered is each diagnostic rendered for a person, one by one and in the
// order given, quoting the files the run quoted.
func rerendered(t *testing.T, diagnostics []dfcad.Diagnostic) string {
	t.Helper()

	var out strings.Builder
	for _, diagnostic := range diagnostics {
		require.NoError(t, diagnostic.Render(&out, dfcad.FileSources{}))
	}

	return out.String()
}

// carriedAbout is the diagnostics a run's object carries, decoded with the
// entities and nodes each is about.
func carriedAbout(t *testing.T, stdout string) []aboutDiagnostic {
	t.Helper()

	_, raw := carried(t, stdout)

	var diagnostics []aboutDiagnostic
	require.NoError(t, json.Unmarshal(raw, &diagnostics))

	return diagnostics
}

// reencoded is diagnostics written again, the way the object writes them.
func reencoded(t *testing.T, diagnostics []aboutDiagnostic) []byte {
	t.Helper()

	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	require.NoError(t, encoder.Encode(diagnostics))

	return bytes.TrimRight(out.Bytes(), "\n")
}

// assertRoundTrips holds the property which says the machine form and the
// human form are one set of diagnostics: the object's entries, rendered one by
// one, are the run's stderr byte for byte, and decoding them and writing them
// again gives back the bytes the object held.
//
// It is only asked of a run whose stderr holds nothing but diagnostics, which
// is every run at the default format and verbosity that writes no `dfcad
// <cmd>:` line of its own.
func assertRoundTrips(t *testing.T, stdout, stderr string) []dfcad.Diagnostic {
	t.Helper()

	diagnostics, raw := carried(t, stdout)

	assert.Equal(t, stderr, rerendered(t, diagnostics), "the object's diagnostics rendered in order are the run's stderr")
	assert.Equal(t, string(raw), string(reencoded(t, carriedAbout(t, stdout))), "decoding the diagnostics and writing them again gives back the same bytes")

	return diagnostics
}

// spannedIn reports whether any diagnostic is about the file named.
func spannedIn(diagnostics []dfcad.Diagnostic, path string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Span.Start.Path == path {
			return true
		}
	}
	return false
}

// TestEveryObjectCarriesTheDiagnosticsItsRunRendered walks every command over a
// tree whose load reports an error, and holds each one that writes an object to
// carrying exactly what it rendered on stderr.
//
// It walks [commands] rather than naming them, as
// [TestEveryCommandWhichReadsTheModelSaysTheLoadRefusedIt] does, so that a
// command added later is held to it the day it is added: whatever it renders,
// its object carries, or this fails.
func TestEveryObjectCarriesTheDiagnosticsItsRunRendered(t *testing.T) {
	for _, cmd := range commands {
		if readsNoModel[cmd.name] {
			continue
		}

		t.Run(cmd.name+" carries every diagnostic it rendered", func(t *testing.T) {
			t.Chdir(tree(t, unloadable(t)))

			var stdout, stderr bytes.Buffer
			code := run(sample(t, cmd), &stdout, &stderr)
			require.Contains(t, []int{exitSuccess, exitLoad}, code, stderr.String())

			// A discovery read answers through the refusal, `check` reports
			// it, and everything else writes the refusal: every one of them
			// writes an object.
			require.NotEmpty(t, stdout.String(), "a run over a refused load writes an object")

			diagnostics := assertRoundTrips(t, stdout.String(), stderr.String())
			assert.True(t, spannedIn(diagnostics, "registry.dfc"), "the load's error names the registry it is about")
		})
	}
}

// crossedModel is the fixture model with both of its footprint rings drawn
// through themselves: the plot's north-east and north-west corners swapped, and
// Room C's the same.
//
// `check` passes over it, because no rule reads a ring; every derivation which
// reads either ring refuses it.
func crossedModel(t *testing.T) map[string]string {
	t.Helper()

	files := model()
	geometry := files["entities/geometry.dfc"]

	for _, corners := range [][2]string{
		{"(value (40.0 12.0 0.0) m)", "(value (20.0 12.0 0.0) m)"},
		{"(value (133.0 209.0 0.0) m)", "(value (125.0 209.0 0.0) m)"},
	} {
		require.Contains(t, geometry, corners[0], "the fixture no longer holds the corner this swaps")
		require.Contains(t, geometry, corners[1], "the fixture no longer holds the corner this swaps")

		geometry = strings.Replace(geometry, corners[0], "\x00", 1)
		geometry = strings.Replace(geometry, corners[1], corners[0], 1)
		geometry = strings.Replace(geometry, "\x00", corners[1], 1)
	}

	files["entities/geometry.dfc"] = geometry

	return files
}

// planCrossedGeometry is a room's ring whose last two corners are swapped, so
// that its east and west walls cross in the middle of the room.
const planCrossedGeometry = `
(vertex geom:V-31 (frame frame:building)
  (position (value (0.0 10.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-32 (frame frame:building)
  (position (value (4.0 10.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-33 (frame frame:building)
  (position (value (0.0 12.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))
(vertex geom:V-34 (frame frame:building)
  (position (value (4.0 12.0 0.0) m) (source "Interior control set IC-01") (method method:total-station)
    (accuracy (independent 0.004 m)) (date "2026-02-18")))

(edge geom:E-31 (frame frame:building) (vertices geom:V-31 geom:V-32))
(edge geom:E-32 (frame frame:building) (vertices geom:V-32 geom:V-33))
(edge geom:E-33 (frame frame:building) (vertices geom:V-33 geom:V-34))
(edge geom:E-34 (frame frame:building) (vertices geom:V-34 geom:V-31))

(loop geom:L-31 (frame frame:building) (edges geom:E-31 geom:E-32 geom:E-33 geom:E-34))
`

// crossedPlanFixture is the plan fixture with a room inside the storey whose
// ring crosses itself. The model() fixture's plot contains nothing, so it is
// the one derivation that needs a storey of its own to refuse.
func crossedPlanFixture() map[string]string {
	return map[string]string{
		"registry.dfc":          planRegistry,
		"entities/model.dfc":    planEntities + planUnreadableEntities,
		"entities/geometry.dfc": planGeometry + planCrossedGeometry,
	}
}

// invocation is a command's name followed by its arguments.
func invocation(name string, args ...string) []string {
	return append([]string{name}, args...)
}

// TestEveryDerivationCarriesTheRingItRefused is the case the story was filed
// with: a footprint ring drawn through itself, which `check` passes over and
// every derivation reading the ring refuses with exit 1. The object said the
// answer was refused and only stderr said why.
func TestEveryDerivationCarriesTheRingItRefused(t *testing.T) {
	testCases := []struct {
		name    string
		files   func(t *testing.T) map[string]string
		args    []string
		crossed []string
	}{
		{
			name:    "measure carries the ring of the room it could not measure",
			files:   crossedModel,
			args:    invocation("measure", samples["measure"]...),
			crossed: []string{"geom:L-21"},
		},
		{
			name:    "tessellate carries the ring of the plot it could not draw",
			files:   crossedModel,
			args:    invocation("tessellate", samples["tessellate"]...),
			crossed: []string{"geom:L-11"},
		},
		{
			name:    "site carries both rings of the fit it could not decide",
			files:   crossedModel,
			args:    invocation("site", samples["site"]...),
			crossed: []string{"geom:L-11", "geom:L-21"},
		},
		{
			name:    "plan carries the ring of the room it left undrawn",
			files:   func(*testing.T) map[string]string { return crossedPlanFixture() },
			args:    invocation("plan", wholeStorey("site:L-01")...),
			crossed: []string{"geom:L-31"},
		},
		{
			name:    "export carries both rings of the model it would not write",
			files:   crossedModel,
			args:    invocation("export", "--position", "position", "--tolerance", "coincident", "--chord", "chord-deviation"),
			crossed: []string{"geom:L-11", "geom:L-21"},
		},
		{
			name:    "export-map carries both rings of the map it would not write",
			files:   crossedModel,
			args:    invocation("export-map", samples["export-map"]...),
			crossed: []string{"geom:L-11", "geom:L-21"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(tree(t, testCase.files(t)))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitCheck, run(testCase.args, &stdout, &stderr), stderr.String())

			diagnostics := assertRoundTrips(t, stdout.String(), stderr.String())

			for _, loop := range testCase.crossed {
				found := false
				for _, diagnostic := range diagnostics {
					if diagnostic.Severity == dfcad.SeverityError &&
						strings.Contains(diagnostic.Message, "expected the loop "+loop+" not to cross itself") {
						found = true
						assert.NotEmpty(t, diagnostic.Hint, "the hint rendered on stderr is carried too")
						assert.Greater(t, diagnostic.Span.Start.Line, 0, "the refusal says where the ring is")
					}
				}
				assert.Truef(t, found, "the refusal of %s is in the object", loop)
			}
		})
	}
}

// rendersOverModel is every command whose sample renders a diagnostic over
// [model], with the severity it renders.
//
// The fixture names no coordinate reference system, and a map written without
// one says so as a warning. A warning is a diagnostic, so that run's object
// carries it; every other run over the fixture renders nothing and so writes no
// key at all.
var rendersOverModel = map[string]dfcad.Severity{
	"export-map": dfcad.SeverityWarning,
}

// TestNoObjectCarriesDiagnosticsWhereNoneWereRendered is its own function
// because it asserts an absence: a run over a model with nothing to report
// writes the bytes it always did, with no `diagnostics` key and no
// `diagnostics-suppressed` either.
func TestNoObjectCarriesDiagnosticsWhereNoneWereRendered(t *testing.T) {
	for _, cmd := range commands {
		t.Run(cmd.name+" writes no diagnostics over a model with nothing to report", func(t *testing.T) {
			t.Chdir(tree(t, model()))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run(sample(t, cmd), &stdout, &stderr), stderr.String())

			result := object(t, stdout.String())
			assert.NotContains(t, result, "diagnostics-suppressed")

			severity, renders := rendersOverModel[cmd.name]
			if !renders {
				assert.Empty(t, stderr.String(), "the fixture renders nothing for this command")
				assert.NotContains(t, result, "diagnostics")
				return
			}

			diagnostics := assertRoundTrips(t, stdout.String(), stderr.String())
			for _, diagnostic := range diagnostics {
				assert.Equal(t, severity, diagnostic.Severity)
			}
		})
	}
}

// suppressedLine is the number on the stderr rendering's line saying how many
// diagnostics the limit held back, or zero where it wrote none.
var suppressedLine = regexp.MustCompile(`(?m)^(\d+) more diagnostics? suppressed by the limit of \d+$`)

// TestAnObjectSaysHowManyDiagnosticsTheLimitHeldBack loads a model with more
// problems than the limit renders, and asserts the object carries the ones
// rendered and the count of the rest, which is what stderr says in its last
// line.
func TestAnObjectSaysHowManyDiagnosticsTheLimitHeldBack(t *testing.T) {
	testCases := []struct {
		name       string
		nodes      int
		suppressed int
	}{
		{
			name:       "counts the diagnostics past the limit",
			nodes:      dfcad.DefaultDiagnosticLimit + 7,
			suppressed: 7,
		},
		{
			name:       "writes no count where the limit held nothing back",
			nodes:      dfcad.DefaultDiagnosticLimit,
			suppressed: 0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			files := model()

			var undeclared strings.Builder
			for n := range testCase.nodes {
				fmt.Fprintf(&undeclared, "\n(node site:X-%04d (kind Space) (type Undeclared) (geometry area) (frame frame:building))\n", n)
			}
			files["entities/site.dfc"] += undeclared.String()

			t.Chdir(tree(t, files))

			var stdout, stderr bytes.Buffer
			require.Equal(t, exitSuccess, run([]string{"list-instances"}, &stdout, &stderr), stderr.String())

			diagnostics, _ := carried(t, stdout.String())
			result := object(t, stdout.String())

			match := suppressedLine.FindStringSubmatch(stderr.String())
			if testCase.suppressed == 0 {
				assert.Nil(t, match, "stderr says nothing was held back")
				assert.NotContains(t, result, "diagnostics-suppressed")
				assert.Len(t, diagnostics, testCase.nodes)
				return
			}

			require.NotNil(t, match, "stderr says how many were held back")
			rendered, err := strconv.Atoi(match[1])
			require.NoError(t, err)

			assert.Equal(t, testCase.suppressed, rendered)
			assert.EqualValues(t, rendered, result["diagnostics-suppressed"])
			assert.Len(t, diagnostics, dfcad.DefaultDiagnosticLimit)

			// The count sits before the list, so the list is still last.
			keys := objectKeys(t, json.RawMessage(stdout.String()))
			assert.Equal(t, []string{"diagnostics-suppressed", "diagnostics"}, keys[len(keys)-2:])
		})
	}
}

// TestFmtCarriesItsDiagnosticsWhereItAlwaysDid is its own function because fmt
// is the one command whose object already carried the machine form, grouped by
// file, and gains no second copy of it at the top.
func TestFmtCarriesItsDiagnosticsWhereItAlwaysDid(t *testing.T) {
	t.Chdir(tree(t, map[string]string{"a.dfc": unparseable}))

	var stdout, stderr bytes.Buffer
	require.Equal(t, exitLoad, run([]string{"fmt"}, &stdout, &stderr))

	result := object(t, stdout.String())
	assert.NotContains(t, result, "diagnostics")
	assert.NotContains(t, result, "diagnostics-suppressed")

	var formatted fmtResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &formatted))
	require.Len(t, formatted.Files, 1)
	assert.NotEmpty(t, formatted.Files[0].Diagnostics)
}

// TestTheDiagnosticsInAnObjectDoNotDependOnTheFormat runs one refusal under
// every format and verbosity, and asserts that stdout is the same bytes each
// time and that what stderr renders of each diagnostic is unchanged by the
// object carrying it.
func TestTheDiagnosticsInAnObjectDoNotDependOnTheFormat(t *testing.T) {
	testCases := []struct {
		name  string
		flags []string
	}{
		{name: "under the default format", flags: nil},
		{name: "under the json format", flags: []string{"--format", "json"}},
		{name: "under the human format", flags: []string{"--format", "human"}},
		{name: "verbosely", flags: []string{"-v"}},
		{name: "verbosely under the human format", flags: []string{"--format", "human", "-v", "-v"}},
	}

	root := tree(t, crossedModel(t))
	t.Chdir(root)

	var expected bytes.Buffer
	var discarded bytes.Buffer
	require.Equal(t, exitCheck, run(invocation("measure", samples["measure"]...), &expected, &discarded))
	diagnostics, _ := carried(t, expected.String())
	rendered := rerendered(t, diagnostics)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append(append([]string{"measure"}, testCase.flags...), samples["measure"]...)
			require.Equal(t, exitCheck, run(args, &stdout, &stderr), stderr.String())

			assert.Equal(t, expected.String(), stdout.String(), "the format is how a run reports itself to a person and never changes stdout")
			assert.Contains(t, stderr.String(), rendered, "the rendering of each diagnostic is the one it always was")
		})
	}
}

// TestEmitWritesTheDiagnosticsAfterEveryOtherField drives emit directly, over
// the shapes of object a result can encode to, because the diagnostics are
// spliced into bytes the result has already been encoded to and each shape
// has its own edge.
func TestEmitWritesTheDiagnosticsAfterEveryOtherField(t *testing.T) {
	at := dfcad.Position{Path: "a.dfc", Line: 1, Column: 2}
	rendered := []dfcad.Diagnostic{{Severity: dfcad.SeverityWarning, Span: at.Span(), Message: "expected <a> & <b>"}}

	testCases := []struct {
		name       string
		result     any
		rendered   []dfcad.Diagnostic
		suppressed int
		expected   string
	}{
		{
			name:     "writes the object it always did where nothing was rendered",
			result:   newEnvelope("check"),
			expected: `{"version":2,"command":"check"}` + "\n",
		},
		{
			name:     "writes the diagnostics after the result's own fields",
			result:   newEnvelope("check"),
			rendered: rendered,
			expected: `{"version":2,"command":"check","diagnostics":[{"severity":"warning","span":"a.dfc:1:2","message":"expected <a> & <b>"}]}` + "\n",
		},
		{
			name:       "writes the count the limit held back before the diagnostics",
			result:     newEnvelope("check"),
			rendered:   rendered,
			suppressed: 3,
			expected:   `{"version":2,"command":"check","diagnostics-suppressed":3,"diagnostics":[{"severity":"warning","span":"a.dfc:1:2","message":"expected <a> & <b>"}]}` + "\n",
		},
		{
			name:     "writes no comma into a result with no fields of its own",
			result:   struct{}{},
			rendered: rendered,
			expected: `{"diagnostics":[{"severity":"warning","span":"a.dfc:1:2","message":"expected <a> & <b>"}]}` + "\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			stream := &diagnosticStream{Writer: &stderr, rendered: testCase.rendered, suppressed: testCase.suppressed}

			require.NoError(t, emit(&answerStream{Writer: &stdout, diagnostics: stream}, testCase.result))

			assert.Equal(t, testCase.expected, stdout.String())
			assert.Empty(t, stderr.String(), "writing the object renders nothing a second time")
		})
	}
}
