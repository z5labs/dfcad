// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// numbered is a file of count lines, each holding its own number, which is a
// body of context whose lines are all distinct and all uninteresting.
func numbered(prefix string, count int) string {
	var out strings.Builder
	for i := 1; i <= count; i++ {
		fmt.Fprintf(&out, "%s%d\n", prefix, i)
	}
	return out.String()
}

// replaced is text with the nth line replaced by line.
func replaced(text string, n int, line string) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	lines[n-1] = line
	return strings.Join(lines, "\n") + "\n"
}

func TestUnified(t *testing.T) {
	testCases := []struct {
		name         string
		old          string
		new          string
		expectedDiff string
	}{
		{
			name:         "writes nothing when the two are the same",
			old:          "a\nb\nc\n",
			new:          "a\nb\nc\n",
			expectedDiff: "",
		},
		{
			name: "writes the header and one hunk for a changed line",
			old:  "a\nb\nc\n",
			new:  "a\nB\nc\n",
			expectedDiff: `--- a.dfc.orig
+++ a.dfc
@@ -1,3 +1,3 @@
 a
-b
+B
 c
`,
		},
		{
			name: "writes a removal with no addition beside it",
			old:  "a\nb\nc\n",
			new:  "a\nc\n",
			expectedDiff: `--- a.dfc.orig
+++ a.dfc
@@ -1,3 +1,2 @@
 a
-b
 c
`,
		},
		{
			name: "writes an addition with no removal beside it",
			old:  "a\nc\n",
			new:  "a\nb\nc\n",
			expectedDiff: `--- a.dfc.orig
+++ a.dfc
@@ -1,2 +1,3 @@
 a
+b
 c
`,
		},
		{
			name: "writes every line of a file being emptied",
			old:  "a\nb\n",
			new:  "",
			expectedDiff: `--- a.dfc.orig
+++ a.dfc
@@ -1,2 +0,0 @@
-a
-b
`,
		},
		{
			name: "writes every line of an empty file being filled",
			old:  "",
			new:  "a\nb\n",
			expectedDiff: `--- a.dfc.orig
+++ a.dfc
@@ -0,0 +1,2 @@
+a
+b
`,
		},
		{
			name: "marks a file which does not end in a line feed",
			old:  "a\nb",
			new:  "a\nb\n",
			expectedDiff: `--- a.dfc.orig
+++ a.dfc
@@ -1,2 +1,2 @@
 a
-b
\ No newline at end of file
+b
`,
		},
		{
			name: "writes three lines of context either side of a change",
			old:  numbered("line ", 20),
			new:  replaced(numbered("line ", 20), 10, "changed"),
			expectedDiff: `--- a.dfc.orig
+++ a.dfc
@@ -7,7 +7,7 @@
 line 7
 line 8
 line 9
-line 10
+changed
 line 11
 line 12
 line 13
`,
		},
		{
			name: "writes two changes which share their context as one hunk",
			old:  numbered("line ", 20),
			new:  replaced(replaced(numbered("line ", 20), 8, "first"), 12, "second"),
			expectedDiff: `--- a.dfc.orig
+++ a.dfc
@@ -5,11 +5,11 @@
 line 5
 line 6
 line 7
-line 8
+first
 line 9
 line 10
 line 11
-line 12
+second
 line 13
 line 14
 line 15
`,
		},
		{
			name: "writes two changes which do not share their context as two hunks",
			old:  numbered("line ", 30),
			new:  replaced(replaced(numbered("line ", 30), 5, "first"), 25, "second"),
			expectedDiff: `--- a.dfc.orig
+++ a.dfc
@@ -2,7 +2,7 @@
 line 2
 line 3
 line 4
-line 5
+first
 line 6
 line 7
 line 8
@@ -22,7 +22,7 @@
 line 22
 line 23
 line 24
-line 25
+second
 line 26
 line 27
 line 28
`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := unified("a.dfc", []byte(testCase.old), []byte(testCase.new))

			assert.Equal(t, testCase.expectedDiff, got)
		})
	}
}

// applied is the two files an edit script describes, rebuilt from it: the
// lines it keeps or removes are the first, and the lines it keeps or adds are
// the second.
func applied(script []edit) (old, new string) {
	var a, b strings.Builder
	for _, e := range script {
		if e.op != '+' {
			a.WriteString(e.text)
		}
		if e.op != '-' {
			b.WriteString(e.text)
		}
	}
	return a.String(), b.String()
}

// TestEditScriptDescribesBothFiles checks the property a diff is only useful
// for having: the script turns exactly the first file into exactly the second.
//
// It is a property rather than a table of expected diffs because a diff which
// is merely plausible passes a comparison against a literal. One that does not
// reconstruct both files is wrong however it reads, including when the search
// gives up on a large change and writes the differing region as one
// replacement — which is why an input past the bound is one of the cases.
func TestEditScriptDescribesBothFiles(t *testing.T) {
	testCases := []struct {
		name string
		old  string
		new  string
	}{
		{name: "two empty files", old: "", new: ""},
		{name: "a file being emptied", old: "a\nb\nc\n", new: ""},
		{name: "an empty file being filled", old: "", new: "a\nb\nc\n"},
		{name: "a file with no line shared with the other", old: "a\nb\nc\n", new: "x\ny\nz\n"},
		{name: "a file whose lines were reordered", old: "a\nb\nc\nd\n", new: "d\nc\nb\na\n"},
		{name: "a file with repeated lines", old: "a\na\na\nb\n", new: "a\nb\na\na\n"},
		{name: "a final line feed being added", old: "a\nb", new: "a\nb\n"},
		{name: "a final line feed being removed", old: "a\nb\n", new: "a\nb"},
		{
			name: "a change past the depth the search is bounded to",
			old:  numbered("old ", maxDiffDepth+500),
			new:  numbered("new ", maxDiffDepth+500),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			script := editScript(diffLines([]byte(testCase.old)), diffLines([]byte(testCase.new)))

			old, new := applied(script)

			assert.Equal(t, testCase.old, old)
			assert.Equal(t, testCase.new, new)
		})
	}
}

// TestEditScriptDescribesTheCorpus is the same property over real files: every
// fixture the printer is tested against, against its own canonical printing,
// which is the pair fmt actually diffs.
func TestEditScriptDescribesTheCorpus(t *testing.T) {
	for _, path := range corpus(t) {
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(path)
			require.NoError(t, err)

			file, err := Parse(path, bytes.NewReader(src))
			if err != nil {
				t.Skip("the fixture does not parse, so it has no canonical printing")
			}

			var want bytes.Buffer
			require.NoError(t, Print(&want, file))

			script := editScript(diffLines(src), diffLines(want.Bytes()))

			old, new := applied(script)

			assert.Equal(t, string(src), old)
			assert.Equal(t, want.String(), new)
		})
	}
}
