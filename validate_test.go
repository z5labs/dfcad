// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validated loads a fixture from testdata/validate, validates it and renders
// the diagnostics the way the command line interface would.
//
// The rendering is what is compared rather than the [Diagnostic] values because
// it holds every field of every one of them — the position, the message, the
// hint and each related location, against the source line each points at — and
// a reviewer can read what a fixture is meant to say without reconstructing it
// from struct literals.
func validated(t *testing.T, name string) string {
	t.Helper()

	path := "testdata/validate/" + name + ".dfc"

	src, err := os.ReadFile(path)
	require.NoError(t, err)

	var diagnostics Diagnostics
	diagnostics.Add(Validate(loadValidateFixture(t, name))...)

	var rendered strings.Builder
	require.NoError(t, diagnostics.Render(&rendered, Sources{path: src}))

	return rendered.String()
}

// expected returns the rendering held in testdata/validate/name.txt, having
// first rewritten it from got when -update was passed.
func expected(t *testing.T, name string, got string) string {
	t.Helper()

	path := "testdata/validate/" + name + ".txt"
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

func TestValidate(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
	}{
		{
			name:    "names an unknown tag, where it is, and the nearest known one",
			fixture: "unknown-tag",
		},
		{
			name:    "names a required child which is missing and the form which wanted it",
			fixture: "missing-child",
		},
		{
			name:    "names both occurrences of a child which may not repeat, and lets a claim repeat",
			fixture: "repeated-child",
		},
		{
			name:    "names both the form and the context when a form is written where it is not permitted",
			fixture: "misplaced-form",
		},
		{
			name:    "names the form and what it expected when the arguments do not add up",
			fixture: "argument-count",
		},
		{
			name:    "reports a form written with nothing in it",
			fixture: "empty-form",
		},
		{
			name:    "reports something which is not a form at all",
			fixture: "not-a-form",
		},
		{
			name:    "checks the children of a claim without treating a repeated predicate as one",
			fixture: "claim-children",
		},
		{
			name:    "reports every independent problem in one pass",
			fixture: "one-pass",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := validated(t, testCase.fixture)

			assert.Equal(t, expected(t, testCase.fixture, got), got)
		})
	}
}

// accepted are the fixtures which hold nothing wrong. Between them they write
// every child of every form at least once, which TestValidateAcceptsEveryForm
// is what checks.
var accepted = []string{"registry", "entities", "every-form"}

// loadValidateFixture parses a fixture from testdata/validate.
func loadValidateFixture(t *testing.T, name string) *File {
	t.Helper()

	path := "testdata/validate/" + name + ".dfc"

	src, err := os.ReadFile(path)
	require.NoError(t, err)

	file, err := Parse(path, bytes.NewReader(src))
	require.NoError(t, err)

	return file
}

// TestValidateAccepts is its own function because its assertion is the other
// shape: a fixture which is well formed has nothing to render, and the thing
// worth reporting on a failure is the diagnostics themselves.
func TestValidateAccepts(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
	}{
		{
			name:    "accepts the registry of the specification's worked example",
			fixture: "registry",
		},
		{
			name:    "accepts the entities of the specification's worked example",
			fixture: "entities",
		},
		{
			name:    "accepts the forms the worked example does not reach",
			fixture: "every-form",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			for _, diagnostic := range Validate(loadValidateFixture(t, testCase.fixture)) {
				t.Errorf("unexpected diagnostic: %s", diagnostic)
			}
		})
	}
}

// TestValidateAcceptsEveryForm checks that the fixtures which are accepted
// exercise every child of every form.
//
// A table of forms nothing loads is a table nothing tests: a child described
// with the wrong arity, or with a description no fixture ever reaches, would
// pass every other test here. Adding a form to the tables therefore means
// writing it into a fixture, which is the point.
func TestValidateAcceptsEveryForm(t *testing.T) {
	written := make(map[string]bool)
	for _, fixture := range accepted {
		for _, node := range loadValidateFixture(t, fixture).Nodes {
			collectTags(node, written)
		}
	}

	seen := make(map[*form]bool)

	var walk func(f *form)
	walk = func(f *form) {
		if f == nil || seen[f] {
			return
		}
		seen[f] = true

		for _, c := range f.children {
			assert.True(t, written[c.tag], "a fixture writes the (%s ...) form", c.tag)
			walk(c.form)
		}
		walk(f.claims)
	}
	walk(topLevelForm)
}

// collectTags records the tag of every form written anywhere beneath a node.
func collectTags(node *Node, into map[string]bool) {
	if tag, ok := formTag(node); ok {
		into[tag] = true
	}
	for _, child := range node.Children {
		collectTags(child, into)
	}
}

func TestValidateEmptyInput(t *testing.T) {
	testCases := []struct {
		name string
		file *File
	}{
		{
			name: "reports nothing for a file which holds no forms",
			file: &File{Path: "entities/level-1.dfc"},
		},
		{
			name: "reports nothing for a file which was never loaded",
			file: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Empty(t, Validate(testCase.file))
		})
	}
}

// TestReservedTags pins the set section 4.2 of the specification writes down
// against the set the tables produce.
//
// The reserved set is derived rather than written twice, and this is what says
// the derivation is the same set: a form which gains a child tag reserves that
// tag against every predicate in every consuming repository, which is a change
// to the specification and not one to make by editing a table.
func TestReservedTags(t *testing.T) {
	want := []string{
		"assert", "backed-by", "boundary", "edges", "frame", "geometry", "kind",
		"label", "member-of", "parent", "retired", "transform", "type", "unit",
		"vertices", "within",
	}

	got := slices.Sorted(maps.Keys(forms().reserved))

	assert.Equal(t, want, got)
}

// TestFormsAreComplete checks the tables for the two mistakes a table cannot
// catch on its own: a child with no description to validate it against, and a
// form whose arguments cannot be described in a diagnostic.
func TestFormsAreComplete(t *testing.T) {
	seen := make(map[*form]bool)

	var walk func(f *form)
	walk = func(f *form) {
		if f == nil || seen[f] {
			return
		}
		seen[f] = true

		assert.NotEmpty(t, f.argsDesc, "a form describes what may follow its tag")

		for _, c := range f.children {
			assert.NotEmpty(t, c.tag, "a child is written with a tag")
			if assert.NotNil(t, c.form, "the (%s ...) child is described", c.tag) {
				walk(c.form)
			}
		}
		walk(f.claims)
	}
	walk(topLevelForm)
}

func TestNearest(t *testing.T) {
	testCases := []struct {
		name       string
		tag        string
		candidates []string
		want       string
	}{
		{
			name:       "offers the tag two adjacent letters were swapped in",
			tag:        "ndoe",
			candidates: topLevelForm.tags(),
			want:       "node",
		},
		{
			name:       "offers the tag a letter is missing from",
			tag:        "tolerace",
			candidates: topLevelForm.tags(),
			want:       "tolerance",
		},
		{
			name:       "offers nothing when nothing is close",
			tag:        "sproket",
			candidates: topLevelForm.tags(),
			want:       "",
		},
		{
			name:       "offers nothing for a short tag two edits from a short one",
			tag:        "kine",
			candidates: []string{"unit"},
			want:       "",
		},
		{
			name:       "offers nothing when the form permits no children at all",
			tag:        "label",
			candidates: nil,
			want:       "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := nearest(testCase.tag, testCase.candidates)

			assert.Equal(t, testCase.want != "", ok)
			if testCase.want != "" {
				assert.Equal(t, testCase.want, got)
			}
		})
	}
}
