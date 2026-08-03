// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad_test

import (
	"errors"
	"fmt"
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
