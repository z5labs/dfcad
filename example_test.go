// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad_test

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/z5labs/dfcad"
)

func ExampleParse() {
	source := `(node site:S-101
  (label "Meeting Room B")
  (kind Space))
`

	file, err := dfcad.Parse("entities/level-1.dfc", strings.NewReader(source))
	if err != nil {
		fmt.Println(err)
		return
	}

	// The label, two lists down, still knows where it was written.
	label := file.Nodes[0].Children[2].Children[1]

	fmt.Println(label.Span.Start)
	fmt.Println(source[label.Span.Start.Offset:label.Span.End.Offset])

	// Output:
	// entities/level-1.dfc:2:10
	// "Meeting Room B"
}

func ExampleParse_failure() {
	_, err := dfcad.Parse("entities/level-1.dfc", strings.NewReader("(date 2026-03-14)\n"))

	var parseErr dfcad.ParseError
	if errors.As(err, &parseErr) {
		fmt.Println(parseErr.Position)
	}

	// Output:
	// entities/level-1.dfc:1:7
}

func ExampleLoad() {
	// Every entity file beneath the root arrives in a deterministic order, one
	// at a time, and a file which fails to load does not stop the walk.
	for file, err := range dfcad.Load("testdata/model") {
		if err != nil {
			fmt.Println("could not load:", err)
			continue
		}
		fmt.Printf("%s: %d top-level forms\n", file.Path, len(file.Nodes))
	}

	// Output:
	// testdata/model/entities/level-1.dfc: 2 top-level forms
	// testdata/model/registry/registry.dfc: 4 top-level forms
}

func ExampleDiagnostic_Render() {
	const path = "entities/level-1.dfc"

	source := `(node site:S-101
  (label "Meeting Room B")
  (position (value (0.0 4.05 0.0))))
`

	file, err := dfcad.Parse(path, strings.NewReader(source))
	if err != nil {
		fmt.Println(err)
		return
	}

	// The value written without the unit which has to follow it.
	value := file.Nodes[0].Children[3].Children[1]

	diagnostic := dfcad.Diagnostic{
		Severity: dfcad.SeverityError,
		Span:     value.Span,
		Message:  "expected a unit after the value, found none",
		Hint:     "units are registry data; a frame declares the one its coordinates are in",
	}

	if err := diagnostic.Render(os.Stdout, dfcad.Sources{path: []byte(source)}); err != nil {
		fmt.Println(err)
	}

	// Output:
	// entities/level-1.dfc:3:13: error: expected a unit after the value, found none
	// 3 |   (position (value (0.0 4.05 0.0))))
	//   |             ^^^^^^^^^^^^^^^^^^^^^^
	//   = hint: units are registry data; a frame declares the one its coordinates are in
}

func ExampleDiagnostics() {
	const path = "entities/level-1.dfc"

	source := `(node site:S-101
  (label "Meeting Room B"))
(node site:S-101
  (label "Meeting Room C"))
`

	file, err := dfcad.Parse(path, strings.NewReader(source))
	if err != nil {
		fmt.Println(err)
		return
	}

	// One pass reports every problem it finds, in a deterministic order, even
	// though these two are collected in the order they happen to be noticed.
	var diagnostics dfcad.Diagnostics

	diagnostics.Add(dfcad.Diagnostic{
		Severity: dfcad.SeverityError,
		Span:     file.Nodes[1].Children[1].Span,
		Message:  "expected an unused id, found site:S-101, which is already defined",
		Related: []dfcad.RelatedLocation{
			{Span: file.Nodes[0].Children[1].Span, Message: "first defined here"},
		},
	})
	diagnostics.Add(dfcad.Diagnostic{
		Severity: dfcad.SeverityWarning,
		Span:     file.Nodes[0].Children[2].Span,
		Message:  "expected a (type ...) child before the claims, found none",
	})

	if err := diagnostics.Render(os.Stdout, dfcad.Sources{path: []byte(source)}); err != nil {
		fmt.Println(err)
	}

	fmt.Println(diagnostics.HasErrors())

	// Output:
	// entities/level-1.dfc:2:3: warning: expected a (type ...) child before the claims, found none
	// 2 |   (label "Meeting Room B"))
	//   |   ^^^^^^^^^^^^^^^^^^^^^^^^
	// entities/level-1.dfc:3:7: error: expected an unused id, found site:S-101, which is already defined
	// 3 | (node site:S-101
	//   |       ^^^^^^^^^^
	// entities/level-1.dfc:1:7: note: first defined here
	// 1 | (node site:S-101
	//   |       ^^^^^^^^^^
	// true
}
