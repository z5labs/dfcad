// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
)

// diffContext is how many unchanged lines are written either side of a change.
// Three is what every unified diff uses, which is what lets the output be read
// by anything that already reads one.
const diffContext = 3

// maxDiffDepth bounds the edit script search.
//
// Finding the shortest edit script costs time and memory in the number of
// differing lines, and a file whose canonical form shares almost nothing with
// what was written — a file whose forms have all been reordered — is the case
// where that number is largest and the answer is least useful. Past the bound
// the search gives up and the differing region is written as one replacement,
// which is a correct diff and a less minimal one.
const maxDiffDepth = 1000

// unified is the unified diff from old to new, labelled with path.
//
// The output is the format `patch` and every diff viewer already read: two
// header lines naming the file before and after, and then one hunk per run of
// changed lines with [diffContext] unchanged lines either side of it. Two
// inputs that are equal produce no output at all rather than an empty diff, so
// a caller can test the result rather than parse it.
//
// A file that does not end in a line feed is a different file from the same
// text that does, and the diff says so: the line is written with the marker a
// unified diff uses for it, which is what stops fmt from reporting a change it
// then appears not to describe.
func unified(path string, old, new []byte) string {
	script := editScript(diffLines(old), diffLines(new))

	hunks := hunks(script, diffContext)
	if len(hunks) == 0 {
		return ""
	}

	var out strings.Builder

	// The two names are the file as it is and the file as it would be. They
	// differ so that the diff says which side is which, and neither is a
	// directory prefix, so that an absolute path stays one.
	fmt.Fprintf(&out, "--- %s.orig\n+++ %s\n", path, path)
	for _, h := range hunks {
		h.write(&out)
	}

	return out.String()
}

// diffLines splits src into lines, each keeping its terminator.
//
// Keeping the line feed on the line is what lets a file that does not end in
// one differ from the same text that does. Splitting on the terminator and
// dropping it makes those two files equal, and a diff reporting no change on a
// file about to be rewritten is worse than no diff at all.
func diffLines(src []byte) []string {
	var out []string

	for len(src) > 0 {
		i := bytes.IndexByte(src, '\n')
		if i < 0 {
			out = append(out, string(src))
			break
		}
		out = append(out, string(src[:i+1]))
		src = src[i+1:]
	}

	return out
}

// edit is one line of an edit script: a line kept, removed or added.
type edit struct {
	// op is ' ' for a line both files hold, '-' for one only the first holds,
	// and '+' for one only the second does. It is the character a unified diff
	// writes the line with.
	op byte

	// text is the line, terminator included where it has one.
	text string
}

// editScript is the sequence of kept, removed and added lines that turns a
// into b.
//
// Removals come before the additions they sit beside, which is the order a
// unified diff is read in and the order every other implementation writes.
func editScript(a, b []string) []edit {
	var out []edit

	ai, bi := 0, 0
	for _, match := range common(a, b) {
		for ; ai < match[0]; ai++ {
			out = append(out, edit{op: '-', text: a[ai]})
		}
		for ; bi < match[1]; bi++ {
			out = append(out, edit{op: '+', text: b[bi]})
		}
		out = append(out, edit{op: ' ', text: a[match[0]]})
		ai, bi = match[0]+1, match[1]+1
	}

	for ; ai < len(a); ai++ {
		out = append(out, edit{op: '-', text: a[ai]})
	}
	for ; bi < len(b); bi++ {
		out = append(out, edit{op: '+', text: b[bi]})
	}

	return out
}

// common is the lines a and b share, as pairs of indices into each, in order.
//
// The ends the two files share are matched directly and only what is left is
// searched. A formatting change usually touches part of a file, so trimming
// first is what keeps the search over the differing region rather than over
// the whole file.
func common(a, b []string) [][2]int {
	var out [][2]int

	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		out = append(out, [2]int{prefix, prefix})
		prefix++
	}

	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}

	for _, match := range myers(a[prefix:len(a)-suffix], b[prefix:len(b)-suffix]) {
		out = append(out, [2]int{match[0] + prefix, match[1] + prefix})
	}

	for i := suffix; i > 0; i-- {
		out = append(out, [2]int{len(a) - i, len(b) - i})
	}

	return out
}

// myers is the longest common subsequence of a and b, as pairs of indices into
// each, by Myers' shortest-edit-script algorithm.
//
// It returns nothing when the two share no line and when the search reaches
// [maxDiffDepth] without finishing. Both mean the same thing to the caller —
// nothing here is common, so the whole of a is replaced by the whole of b —
// which is why the bound needs no separate answer.
func myers(a, b []string) [][2]int {
	n, m := len(a), len(b)
	if n == 0 || m == 0 {
		return nil
	}

	// The furthest-reaching path on diagonal k after d edits is v[offset+k].
	// A path after d edits lies on a diagonal between -d and d, so bounding d
	// bounds the array as well as the number of them kept.
	depth := min(n+m, maxDiffDepth)
	offset := depth
	v := make([]int, 2*depth+1)

	// trace[d] is v as it was before the d-th edit, which is what the path is
	// reconstructed from once an end is reached.
	trace := make([][]int, 0, depth+1)

	for d := 0; d <= depth; d++ {
		trace = append(trace, slices.Clone(v))

		for k := -d; k <= d; k += 2 {
			var x int
			switch {
			case k == -d, k != d && v[offset+k-1] < v[offset+k+1]:
				x = v[offset+k+1]
			default:
				x = v[offset+k-1] + 1
			}

			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}
			v[offset+k] = x

			if x >= n && y >= m {
				return backtrack(trace, offset, n, m)
			}
		}
	}

	return nil
}

// backtrack walks the recorded search back from the end of both files,
// collecting the pairs of lines the path matched.
func backtrack(trace [][]int, offset, n, m int) [][2]int {
	var out [][2]int

	x, y := n, m
	for d := len(trace) - 1; d > 0; d-- {
		v := trace[d]
		k := x - y

		// Which diagonal the path arrived from is the same choice the search
		// made on the way out, read off the state it made it in.
		var previous int
		switch {
		case k == -d, k != d && v[offset+k-1] < v[offset+k+1]:
			previous = k + 1
		default:
			previous = k - 1
		}

		px := v[offset+previous]
		py := px - previous

		for x > px && y > py {
			x, y = x-1, y-1
			out = append(out, [2]int{x, y})
		}

		x, y = px, py
	}

	// The path before any edit is the run of equal lines the two files open
	// with, which the trimming above has already taken when there was one.
	for x > 0 && y > 0 {
		x, y = x-1, y-1
		out = append(out, [2]int{x, y})
	}

	slices.Reverse(out)

	return out
}

// hunk is one run of changed lines with the unchanged lines around it, and
// where in each file it begins.
type hunk struct {
	// a and b are the 0-based line numbers the hunk starts at in each file.
	a, b int

	// an and bn are how many lines of each it covers.
	an, bn int

	// edits are the lines themselves, in order.
	edits []edit
}

// hunks groups an edit script into the hunks a unified diff writes: every
// changed line, with context unchanged lines either side, and two changes
// close enough to share their context written as one.
func hunks(script []edit, context int) []hunk {
	var changes []int
	for i, e := range script {
		if e.op != ' ' {
			changes = append(changes, i)
		}
	}
	if len(changes) == 0 {
		return nil
	}

	// lines[i] is how many lines of each file the first i edits account for,
	// which is where a hunk starting at i begins and how long it runs.
	as, bs := make([]int, len(script)+1), make([]int, len(script)+1)
	for i, e := range script {
		as[i+1], bs[i+1] = as[i], bs[i]
		if e.op != '+' {
			as[i+1]++
		}
		if e.op != '-' {
			bs[i+1]++
		}
	}

	var out []hunk
	for start := 0; start < len(changes); {
		// Two changes separated by no more than twice the context share it,
		// and writing them as two hunks would write those lines twice.
		end := start
		for end+1 < len(changes) && changes[end+1]-changes[end] <= 2*context+1 {
			end++
		}

		from := max(changes[start]-context, 0)
		to := min(changes[end]+context, len(script)-1)

		out = append(out, hunk{
			a:     as[from],
			b:     bs[from],
			an:    as[to+1] - as[from],
			bn:    bs[to+1] - bs[from],
			edits: script[from : to+1],
		})

		start = end + 1
	}

	return out
}

// write appends the hunk's header and its lines.
func (h hunk) write(out *strings.Builder) {
	fmt.Fprintf(out, "@@ -%s +%s @@\n", lineRange(h.a, h.an), lineRange(h.b, h.bn))

	for _, e := range h.edits {
		out.WriteByte(e.op)
		out.WriteString(e.text)

		// A line with no terminator is the last line of its file, and the
		// marker is how a unified diff says so.
		if !strings.HasSuffix(e.text, "\n") {
			out.WriteString("\n\\ No newline at end of file\n")
		}
	}
}

// lineRange is how a hunk header spells the lines it covers: the 1-based
// number of the first, and the count where it is not one.
//
// A range covering nothing is written at the line it would have started at,
// which is how a unified diff spells an insertion into a file that has no line
// there.
func lineRange(start, count int) string {
	switch count {
	case 0:
		return fmt.Sprintf("%d,0", start)
	case 1:
		return fmt.Sprintf("%d", start+1)
	default:
		return fmt.Sprintf("%d,%d", start+1, count)
	}
}
