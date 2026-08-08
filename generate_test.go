// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sexpr "github.com/z5labs/sexpr-go"
)

// generatedTrees is how many random files the properties below are checked
// against.
//
// The number is a budget rather than a target: each file is small, and what
// finds a defect is the variety of shapes rather than the count of them. It is
// low enough that the suite stays fast under -race and high enough that a rule
// which holds for most inputs and not all of them shows up.
const generatedTrees = 300

// TestGeneratedTreesSurviveTheRoundTrip checks the printing properties against
// input nobody wrote.
//
// A hand-written corpus covers the constructs somebody thought of, and the
// shapes it does not reach are exactly the ones a bug hides in: a form whose
// children happen to sort into a different order, a comment before the last
// child of a form which then holds its default, a list which fits on one line
// until the child before it grows. A generator reaches those by trying, and
// every file it produces is checked to be valid before anything is asserted
// about it, so a failure here is a failure of the printer rather than of the
// generator.
func TestGeneratedTreesSurviveTheRoundTrip(t *testing.T) {
	for seed := range uint64(generatedTrees) {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			src := generate(seed)

			file, err := Parse("generated.dfc", strings.NewReader(src))
			require.NoError(t, err, "the generated source parses:\n%s", src)

			for _, diagnostic := range Validate(file) {
				t.Fatalf("the generated source is valid: %s\n%s", diagnostic, src)
			}

			var once strings.Builder
			require.NoError(t, Print(&once, file))

			read, err := Parse("generated.dfc", strings.NewReader(once.String()))
			require.NoError(t, err, "canonical output parses:\n%s", once.String())

			// The tree survives: what parsing the output gives back is what the
			// printer was given to write.
			assert.Equal(t, unpositioned(canonical(file)), unpositioned(treeOf(read)), "source:\n%s", src)

			// And printing is a fixed point: canonical form prints as itself.
			var twice strings.Builder
			require.NoError(t, Print(&twice, read))
			assert.Equal(t, once.String(), twice.String(), "source:\n%s", src)

			// What the generator produces is valid, so its canonical printing is
			// too — the printer is not permitted to write a file which no longer
			// loads.
			for _, diagnostic := range Validate(read) {
				t.Errorf("the canonical printing is valid: %s\n%s", diagnostic, once.String())
			}
		})
	}
}

// generate writes one random file which is a valid dfcad file.
//
// It is driven by the tables in forms.go rather than by a grammar written out
// again here, so the shapes it produces are the shapes the format permits and
// stay that way when a form is added. What it chooses is how many of each
// optional and repeatable child to write, what to write in the positional
// arguments, where to break a line and where to put a comment; what it never
// chooses is whether the result is well formed.
func generate(seed uint64) string {
	g := &generator{rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}

	var out strings.Builder
	for range 1 + g.rng.IntN(4) {
		for range g.rng.IntN(2) {
			out.WriteString(g.comment())
			out.WriteByte('\n')
		}

		c := topLevelForm.children[g.rng.IntN(len(topLevelForm.children))]
		out.WriteString(g.form(c.tag, c.form, 0))
		out.WriteByte('\n')
	}

	return out.String()
}

// generator holds the randomness one file is written from.
type generator struct {
	rng *rand.Rand
}

// form writes one form of the format, its children chosen within the arities
// its table gives them.
func (g *generator) form(tag string, f *form, depth int) string {
	// A depth bound is needed because a form which permits a claim permits a
	// form which permits a claim, and the recursion is otherwise bounded only
	// by luck.
	deep := depth >= 4

	var children []string
	counts := make(map[string]int)
	for _, c := range f.children {
		n := g.count(c.arity, deep)
		counts[c.tag] = n
		for range n {
			children = append(children, g.form(c.tag, c.form, depth+1))
		}
	}

	if f.claims != nil && !deep {
		for range g.rng.IntN(3) {
			children = append(children, g.form(g.predicate(), f.claims, depth+1))
		}
	}

	if f.free {
		for range g.rng.IntN(3) {
			children = append(children, "("+g.parameter()+" "+g.atom()+")")
		}
	}

	// Positional arguments come before the children, because that is what makes
	// them positional arguments: everything up to the first child form is one.
	// A form with children takes as few as it may, so that a value written as a
	// transform is not also written as a scalar beside it.
	n := f.args.min
	if len(children) == 0 || f.args.min == f.args.max {
		n = g.count(f.args, deep)
	}

	args := make([]string, 0, n)
	for range n {
		args = append(args, g.atom())
	}

	// Two forms of the format are not permitted to be empty however their
	// children fell out. Growing the arguments is the fix where the form takes
	// any, and repeating the first repeatable child is the fix where it does
	// not.
	for len(args)+len(children) < f.minElements {
		if f.args.permits(len(args) + 1) {
			args = append(args, g.atom())
			continue
		}

		c := f.children[0]
		if !c.arity.permits(counts[c.tag] + 1) {
			break
		}
		counts[c.tag]++
		children = append(children, g.form(c.tag, c.form, depth+1))
	}

	return g.write(tag, args, children, depth)
}

// write assembles a form from its tag, its arguments and its children, laying
// it out the way somebody would have and sometimes the way nobody would.
//
// The layout is chosen at random on purpose. No rule of the format depends on
// where a line break falls, so a file broken differently must print the same,
// and the only way to say that is to write one.
func (g *generator) write(tag string, args, children []string, depth int) string {
	var out strings.Builder

	out.WriteByte('(')
	out.WriteString(tag)
	for _, arg := range args {
		out.WriteByte(' ')
		out.WriteString(arg)
	}

	indent := strings.Repeat(" ", 2*(depth+1))
	broken := len(children) > 0 && g.rng.IntN(3) > 0

	for _, child := range children {
		switch {
		case broken:
			out.WriteByte('\n')
			for range g.rng.IntN(2) {
				out.WriteString(indent)
				out.WriteString(g.comment())
				out.WriteByte('\n')
			}
			out.WriteString(indent)
		default:
			out.WriteByte(' ')
		}
		out.WriteString(child)
	}

	// A comment which no sibling follows stays at the end of the form it was
	// written in, which is its own rule and its own way of going wrong.
	if broken && g.rng.IntN(4) == 0 {
		out.WriteByte('\n')
		out.WriteString(indent)
		out.WriteString(g.comment())
		out.WriteByte('\n')
		out.WriteString(strings.Repeat(" ", 2*depth))
	}

	out.WriteByte(')')

	return out.String()
}

// count is how many of something to write, within the arity which permits it.
//
// An unbounded arity is capped rather than followed, because a file with two
// hundred of one child says nothing a file with two does not. deep asks for the
// fewest permitted, which is how the recursion ends.
func (g *generator) count(a arity, deep bool) int {
	if deep {
		return a.min
	}

	max := a.max
	if max < 0 || max > a.min+2 {
		max = a.min + 2
	}

	return a.min + g.rng.IntN(max-a.min+1)
}

// predicates are the names a generated claim is written under.
//
// None of them is a reserved structural tag, which is what makes a child
// written under one a claim rather than a misplaced form, and none of them is
// spelled close enough to a tag of the format for a diagnostic to suggest it.
var predicates = []string{"width", "height", "colour", "clearance", "position", "occupancy", "area", "span"}

// predicate picks the name of a generated claim.
func (g *generator) predicate() string {
	return predicates[g.rng.IntN(len(predicates))]
}

// parameters are the names a generated assertion parameter is written under.
// A check's parameters belong to the check registry, so any tag is one.
var parameters = []string{"tolerance", "of", "minimum", "against", "required"}

// parameter picks the name of a generated assertion parameter.
func (g *generator) parameter() string {
	return parameters[g.rng.IntN(len(parameters))]
}

// texts are the strings a generated file is written with. Each is chosen for
// something the printer has to do with it: escape a character, leave a
// non-ASCII letter alone, or hold nothing at all.
var texts = []string{
	"Meeting Room B",
	`a "quoted" word`,
	`a back\slash`,
	"a\ttab and a\nline break",
	"Grünstraße 12, 一メートル",
	"\x00\x1f\u0085",
	"",
}

// reals are the numbers a generated file is written with, spelled the way the
// awkward ones are awkward: a trailing zero which carries no meaning, two
// spellings of one value, a decimal nothing shorter reads back, and the ends of
// the range.
var reals = []string{
	"0.0", "-0.0", "8.50", "8.5", "3.0e2", "100.0", "0.30000000000000004",
	"-0.0017453", "401235.117", "1e+21", "5e-324", "1.7976931348623157e+308",
}

// atom writes one datum which holds nothing, or, now and then, the one thing
// which is written like an argument and is not an atom: the parenthesised
// components of a coordinate.
//
// nil is never written. It is a legal S-expression and no part of this format,
// so a generator which produced one would be generating a file the validator is
// required to reject.
func (g *generator) atom() string {
	switch g.rng.IntN(12) {
	case 0, 1, 2:
		return g.id()
	case 3, 4:
		return g.name()
	case 5, 6:
		return quoteText(texts[g.rng.IntN(len(texts))])
	case 7, 8:
		return reals[g.rng.IntN(len(reals))]
	case 9:
		return strconv.Itoa(g.rng.IntN(1000) - 500)
	case 10:
		return []string{"#t", "#f", "#true", "#false"}[g.rng.IntN(4)]
	default:
		var out strings.Builder
		out.WriteByte('(')
		for i := range 2 + g.rng.IntN(2) {
			if i > 0 {
				out.WriteByte(' ')
			}
			out.WriteString(reals[g.rng.IntN(len(reals))])
		}
		out.WriteByte(')')
		return out.String()
	}
}

// namespaces and locals are what a generated id is spelled from.
var (
	namespaces = []string{"site", "geom", "frame", "survey", "method"}
	locals     = []string{"S-101", "V-01", "building", "C-0031", "CP-3:north", "lot/17.3", "Sté"}
)

// id writes an identifier: a registered-looking namespace, a colon, and a local
// part which may itself hold colons and characters which are not ASCII.
func (g *generator) id() string {
	return namespaces[g.rng.IntN(len(namespaces))] + ":" + locals[g.rng.IntN(len(locals))]
}

// names are the registry names and closed-set members a generated file is
// written with. They are plain symbols, with no namespace and no colon.
var names = []string{
	"Space", "Element", "Zone", "area", "line", "absent", "m", "ft", "usft",
	"scalar", "coordinate", "transform", "text", "MeetingRoom", "Partition",
	"normal", "deprecated", "boundary-closure",
}

// name writes a registry name.
func (g *generator) name() string {
	return names[g.rng.IntN(len(names))]
}

// comments are what a generated comment is written as: a line comment, and a
// block comment which spans lines and nests.
var comments = []string{
	"; Shot twice: the earlier value was taken before the partition moved.",
	";",
	";; Two semicolons are one comment.",
	"#| A block comment. |#",
	"#| One which #| nests |# and spans\n   more than one line. |#",
}

// comment writes one comment.
func (g *generator) comment() string {
	return comments[g.rng.IntN(len(comments))]
}

// TestGeneratedTreesAreVaried checks the generator itself: that what it
// produces reaches the constructs the properties above are worth checking over.
//
// A generator which quietly produced the same short file three hundred times
// would pass every assertion in this file and test nothing. The thresholds are
// deliberately loose — the point is that each construct is reached at all, not
// how often.
func TestGeneratedTreesAreVaried(t *testing.T) {
	tags := make(map[string]bool)
	var commented, claimed, deep int

	for seed := range uint64(generatedTrees) {
		src := generate(seed)

		file, err := Parse("generated.dfc", strings.NewReader(src))
		require.NoError(t, err)

		for _, node := range file.Nodes {
			collectTags(node, tags)
		}

		commented += strings.Count(src, ";") + strings.Count(src, "#|")
		claimed += strings.Count(src, "(width ") + strings.Count(src, "(colour ")
		if depthOf(file) > 3 {
			deep++
		}
	}

	for _, c := range topLevelForm.children {
		assert.True(t, tags[c.tag], "the generator writes a (%s ...) form", c.tag)
	}

	assert.Greater(t, commented, 0, "the generator writes comments")
	assert.Greater(t, claimed, 0, "the generator writes claims")
	assert.Greater(t, deep, 0, "the generator writes a form nested more than three deep")
}

// depthOf is how deeply nested the deepest datum of a file is.
func depthOf(file *File) int {
	var of func(node *Node) int
	of = func(node *Node) int {
		deepest := 0
		for _, child := range node.Children {
			deepest = max(deepest, of(child))
		}
		return deepest + 1
	}

	deepest := 0
	for _, node := range file.Nodes {
		deepest = max(deepest, of(node))
	}

	return deepest
}

// TestGeneratedTreesAreDeterministic checks that a seed names one file.
//
// A failure of the property above is only worth reporting if the seed in the
// subtest name reproduces it, and a generator reading a shared source of
// randomness would not.
func TestGeneratedTreesAreDeterministic(t *testing.T) {
	for seed := range uint64(8) {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			assert.Equal(t, generate(seed), generate(seed))
		})
	}
}

// TestGeneratedAtomsAreLegal checks the one thing the generator can get wrong
// which the validator would not notice: writing a datum the format excludes.
//
// nil, a quote shorthand and a dotted pair are all legal S-expressions and are
// none of them legal here, so a generator which produced one would be producing
// input specification section 2 requires to be rejected — and the round-trip
// property would then be asserting something about a file which does not load.
func TestGeneratedAtomsAreLegal(t *testing.T) {
	for seed := range uint64(generatedTrees) {
		src := generate(seed)

		file, err := Parse("generated.dfc", strings.NewReader(src))
		require.NoError(t, err)

		var walk func(node *Node)
		walk = func(node *Node) {
			switch datum := node.Datum.(type) {
			case sexpr.Nil:
				t.Fatalf("seed %d wrote nil:\n%s", seed, src)
			case sexpr.Quote:
				t.Fatalf("seed %d wrote a quote shorthand:\n%s", seed, src)
			case sexpr.List:
				if datum.Tail != nil {
					t.Fatalf("seed %d wrote a dotted pair:\n%s", seed, src)
				}
				if len(datum.Elements) == 0 {
					t.Fatalf("seed %d wrote an empty list:\n%s", seed, src)
				}
			}

			for _, child := range node.Children {
				walk(child)
			}
		}

		for _, node := range file.Nodes {
			walk(node)
		}
	}
}
