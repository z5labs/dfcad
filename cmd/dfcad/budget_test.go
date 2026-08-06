// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tiktoken-go/tokenizer"

	"github.com/z5labs/dfcad"
)

// This file is the gate the whole query surface rests on.
//
// The bet the engine makes is that an agent which has never seen a model is
// better off asking it two questions than reading it: that `list-types` and
// `list-instances` cost a few hundred tokens where the files cost tens of
// thousands. That is a claim about a number, and until the number is measured
// it is an estimate dressed as an argument.
//
// So it is measured, with a real byte pair encoder rather than a
// characters-per-token rule of thumb, against a model somebody could have
// authored rather than against the four-node fixture the corpus uses to
// exercise the printer. What comes out is written into docs/token-budget.md by
// [TestTheRecordedTokenBudgetIsCurrent], so the claim in the repository is the
// measurement rather than a recollection of it.

// updateGolden rewrites everything this package holds a recorded copy of: the
// measurements in docs/token-budget.md, and the golden artefacts under
// testdata.
//
//	go test ./cmd/dfcad -update
//
// It is the same flag, spelled the same way, as the one the root package's
// golden files are regenerated with, and it is one flag rather than one per
// kind of record for the same reason: a change to a command reaches all of
// them at once, and regenerating half of them leaves a tree nobody can read a
// diff of. A measurement is a golden too — it is derived from committed
// inputs, it changes when the output of a command changes, and the diff is
// what says the change was intended.
var updateGolden = flag.Bool("update", false, "rewrite the golden files under testdata and the measurements in docs/token-budget.md")

const (
	// budgetRoot is the representative model everything here is measured
	// against. See its README for what it holds and why it is that size.
	budgetRoot = "testdata/budget"

	// budgetRecord is where the measurements are written down. It is outside
	// this package because the result is documentation rather than test data:
	// somebody deciding whether to build on the query surface reads it, and
	// they should not have to find it under a testdata directory to do so.
	budgetRecord = "../../docs/token-budget.md"

	// moduleFile is where the tokenizer's version is read from.
	moduleFile = "../../go.mod"
)

// The generated block of the record sits between these two markers. Everything
// outside them is prose somebody wrote — what the gate is, and what was decided
// once the numbers were in — and regeneration must not touch it.
const (
	beginMeasurements = "<!-- begin measurements -->"
	endMeasurements   = "<!-- end measurements -->"
)

// tokenizerModule is the byte pair encoder the counts come from. Its version is
// read out of go.mod rather than written here, so the record cannot claim to
// have been measured with a version the module no longer requires.
//
// It embeds the rank tables, so nothing here reaches the network and CI counts
// the same tokens an author does.
const tokenizerModule = "github.com/tiktoken-go/tokenizer"

// encoding is one byte pair encoder the paths are counted under, together with
// what it is the tokenizer of.
//
// Two of them, because the gate is a claim about the arrangement rather than
// about one vendor's vocabulary. A partitioning that lands inside its budget
// under one encoding and outside it under another has not been measured; it has
// been lucky. Both are OpenAI encodings because those are the ones with a
// published, offline implementation — no Claude tokenizer is distributable, so
// the honest thing is to name what was used rather than to imply coverage the
// measurement does not have.
type encoding struct {
	// name is the encoding, which is what the record reports as the tokenizer.
	name tokenizer.Encoding

	// models is what that encoding tokenizes for, so a reader can tell whether
	// it is close to the model they are budgeting for.
	models string
}

var encodings = []encoding{
	{name: tokenizer.O200kBase, models: "GPT-5, GPT-4.1, GPT-4o, the o-series"},
	{name: tokenizer.Cl100kBase, models: "GPT-4, GPT-3.5 Turbo"},
}

// call is one invocation of the command line interface, as an agent would make
// it.
type call struct {
	// name is how the record spells the invocation.
	name string

	// args is what run is given, minus the program name.
	args []string
}

// path is a sequence of calls answering one question from a cold start, and the
// budget it has to answer it in.
type path struct {
	// name is what the record and the subtests call it.
	name string

	// what is the question the path answers, in one line.
	what string

	// calls are made in order, and the path's cost is their total.
	calls []call

	// target is what the story asked the path to cost, and it is the gate. It
	// is not asserted on: raising it to meet a measurement would turn the claim
	// into a description of whatever the code happens to do. The record reports
	// both figures and which way round they came out.
	//
	// Zero is a path nobody set a target for, which is measured to stop it
	// drifting rather than gated. [path.gated] is the question, so that a
	// target of zero cannot be read as a path which must cost nothing.
	target int

	// ceiling is the most the path may cost under any encoding measured, and it
	// is what the test asserts. It sits just above what was measured, so that
	// while the target is missed the miss cannot quietly get worse — a change
	// that adds a field to a discovery answer fails here and is reviewed
	// against the gate rather than absorbed into it.
	ceiling int
}

// gated reports whether anybody set a target for the path.
func (p path) gated() bool {
	return p.target > 0
}

// met reports whether the path came in under what the story asked of it. A path
// nobody asked anything of is not met and not missed.
func (p path) met(costs []int) bool {
	if !p.gated() {
		return false
	}
	for _, cost := range costs {
		if cost > p.target {
			return false
		}
	}
	return true
}

// The four paths measured.
//
// Discovery is the pair of calls the query surface is partitioned around: what
// kinds of thing are there, and which of them exist. The cold question is the
// whole of what the engine is for — a dimensional answer from an agent that has
// read nothing — and it is discovery plus the one call that turns a name into a
// number.
//
// That `resolve` is the whole of the fetch is the finding of the partitioning
// review, issue #113. The path first measured for issue #38 ran `get` before
// `resolve`, and `get` is not a targeted fetch: it is the retrieval of a thing
// entire, with every claim written on it and where each was written, which is a
// different question from how big the room is. It is still measured, below, so
// that the figure #38 recorded stays comparable — it is simply no longer what
// the gate is read off.
//
// The warm question is the same question asked by an agent that has already
// paid for the vocabulary. It is measured because the two costs scale with
// different things: `list-types` grows with the registry and is paid once per
// cold start, and the rest grows with neither and is paid per question.
//
// `MeetingRoom` is the type asked about because it is a middling one in this
// model: six instances against `Office`'s sixteen and `OfficeBuilding`'s one.
// Measuring the smallest type would report the arrangement at its best.
var (
	listTypes    = call{name: "dfcad list-types", args: []string{"list-types", "--root", budgetRoot}}
	listRooms    = call{name: "dfcad list-instances MeetingRoom", args: []string{"list-instances", "MeetingRoom", "--root", budgetRoot}}
	getRoom      = call{name: "dfcad get site:S-111", args: []string{"get", "site:S-111", "--root", budgetRoot}}
	resolveArea  = call{name: "dfcad resolve site:S-111 area", args: []string{"resolve", "site:S-111", "area", "--root", budgetRoot}}
	describeType = call{
		name: "dfcad list-types --describe",
		args: []string{"list-types", "--describe", "--root", budgetRoot},
	}
	classifyTypes = call{
		name: "dfcad list-types --classification",
		args: []string{"list-types", "--classification", "--root", budgetRoot},
	}
	evidenceArea = call{
		name: "dfcad resolve site:S-111 area --evidence",
		args: []string{"resolve", "site:S-111", "area", "--evidence", "--root", budgetRoot},
	}
	measureRoom = call{
		name: "dfcad measure site:S-111",
		args: []string{
			"measure", "site:S-111",
			"--position", "position", "--tolerance", "boundary-closure",
			"--root", budgetRoot,
		},
	}
	listCorners = call{
		name: "dfcad list-geometry --predicate position --family vertex",
		args: []string{
			"list-geometry",
			"--predicate", "position", "--family", "vertex",
			"--root", budgetRoot,
		},
	}
)

var (
	discovery = path{
		name:    "discovery",
		what:    "what kinds of thing are in this model, and which meeting rooms exist",
		target:  500,
		ceiling: 460,
		calls:   []call{listTypes, listRooms},
	}

	coldQuestion = path{
		name:    "a dimensional question from a cold start",
		what:    "how big is Meeting Room B on level 1, starting from nothing",
		target:  500,
		ceiling: 530,
		calls:   []call{listTypes, listRooms, resolveArea},
	}

	warmQuestion = path{
		name:    "the same question once the vocabulary is known",
		what:    "how big is Meeting Room B on level 1, for an agent which has already read list-types",
		target:  300,
		ceiling: 280,
		calls:   []call{listRooms, resolveArea},
	}

	wholeRetrieval = path{
		name:    "the same question by way of a whole retrieval",
		what:    "how big is Meeting Room B on level 1, retrieving the thing itself on the way",
		ceiling: 820,
		calls:   []call{listTypes, listRooms, getRoom, resolveArea},
	}

	derivedQuestion = path{
		name: "the same question answered from the geometry rather than from a claim",
		what: "how big is Meeting Room B on level 1 by the corners it is drawn on, starting from nothing",
		// No target. `resolve` answers what somebody wrote down and this computes
		// what the corners come to, and the two are not the same question: a
		// budget set for the first would be a gate on the second for reasons
		// nobody argued. What it costs is measured so that it cannot drift.
		ceiling: 960,
		calls:   []call{listTypes, listRooms, measureRoom},
	}

	geometricDiscovery = path{
		name: "finding the geometry which carries a measurement",
		what: "which corners of this model anybody has surveyed a position for",
		// No target, and it could not honestly have one. The two gated paths
		// answer a question about one named thing, and their cost does not grow
		// with the model; this enumerates a family across the whole tree, so
		// what it costs is the size of the answer rather than of the
		// arrangement. A budget written for the first would be a gate on the
		// second for reasons nobody argued.
		//
		// It is measured because the alternative to asking it is reading both
		// geometry files, and because the per-entry payload is the thing which
		// drifts: a field added to a listed geometric node is paid once per
		// corner, which is fifty-six times here and once in `get`.
		ceiling: 2500,
		calls:   []call{listCorners},
	}

	paths = []path{discovery, coldQuestion, warmQuestion, wholeRetrieval, derivedQuestion, geometricDiscovery}
)

// answer runs one call and returns what it wrote to stdout, which is the whole
// of what the caller pays for. Anything on stderr is for a person and never
// reaches the model, so it is discarded rather than counted.
func answer(t testing.TB, c call) string {
	t.Helper()

	var stdout bytes.Buffer
	code := run(c.args, &stdout, io.Discard)
	require.Equal(t, exitSuccess, code, "%s succeeds", c.name)

	return stdout.String()
}

// codecFor is the byte pair encoder of one encoding.
func codecFor(t testing.TB, e encoding) tokenizer.Codec {
	t.Helper()

	codec, err := tokenizer.Get(e.name)
	require.NoError(t, err)

	return codec
}

// tokens is how many tokens text costs under one encoder.
func tokens(t testing.TB, codec tokenizer.Codec, text string) int {
	t.Helper()

	n, err := codec.Count(text)
	require.NoError(t, err)

	return n
}

// answers is what every call of a path wrote, in order.
//
// A path is answered once and counted many times, because what a command writes
// does not depend on which tokenizer reads it. Running the whole path again per
// encoding would load the model twice to count the same bytes, which is a load
// per encoding added to every figure in the record.
func answers(t testing.TB, p path) []string {
	t.Helper()

	out := make([]string, 0, len(p.calls))
	for _, c := range p.calls {
		out = append(out, answer(t, c))
	}

	return out
}

// cost is what each of a path's answers costs under one encoder, and their
// total.
func cost(t testing.TB, codec tokenizer.Codec, answered []string) (perCall []int, total int) {
	t.Helper()

	for _, text := range answered {
		n := tokens(t, codec, text)
		perCall = append(perCall, n)
		total += n
	}

	return perCall, total
}

// modelFiles is every entity file of the representative model, in lexical
// order, with the bytes of each.
//
// Lexical rather than walk order, because the record lists them and a table
// that reorders itself when a file is added is a table nobody can diff.
func modelFiles(t testing.TB) (files []string, sources map[string][]byte) {
	t.Helper()

	sources = make(map[string][]byte)
	for found, err := range dfcad.Walk(budgetRoot) {
		require.NoError(t, err)

		src, err := os.ReadFile(found)
		require.NoError(t, err)

		files = append(files, filepath.ToSlash(found))
		sources[filepath.ToSlash(found)] = src
	}
	slices.Sort(files)

	return files, sources
}

// subjectFile is the file the question's answer was written in, taken from the
// span `get` reported rather than written down here. A hard-coded path would
// keep saying the same thing after the fixture moved the node.
func subjectFile(t testing.TB) string {
	t.Helper()

	var got struct {
		Entity struct {
			Span dfcad.Span `json:"span"`
		} `json:"entity"`
	}
	require.NoError(t, json.Unmarshal([]byte(answer(t, getRoom)), &got))
	require.NotEmpty(t, got.Entity.Span.Start.Path)

	return filepath.ToSlash(got.Entity.Span.Start.Path)
}

// TestTheDiscoveryPathDoesNotGetMoreExpensive holds the measured cost where it
// is.
//
// It asserts the ceiling rather than the target. The two gated paths now meet
// their targets, and asserting a target directly would make the first field
// anybody adds a failure reported as though the bet had come apart. What this
// is for is the change nobody measured: a field added to a listing entry, a
// span widened, a description restored to a default. Each arrives here as a
// failing test naming the path and the figure, to be weighed against the gate
// rather than absorbed into it by raising the constant.
//
// The ceiling sits just above what was measured, on both encodings, for the
// same reason. A ceiling with room in it is a ceiling nothing hits.
func TestTheDiscoveryPathDoesNotGetMoreExpensive(t *testing.T) {
	testCases := []struct {
		name string
		path path
	}{
		{
			name: "learns what the model holds without reading it",
			path: discovery,
		},
		{
			name: "answers a dimensional question from nothing",
			path: coldQuestion,
		},
		{
			name: "answers it again without paying for the vocabulary twice",
			path: warmQuestion,
		},
		{
			name: "retrieves the whole thing on the way to the same answer",
			path: wholeRetrieval,
		},
		{
			name: "computes the answer from the corners instead of reading a claim",
			path: derivedQuestion,
		},
		{
			name: "finds the corners a position was ever surveyed for",
			path: geometricDiscovery,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			answered := answers(t, testCase.path)

			for _, e := range encodings {
				_, total := cost(t, codecFor(t, e), answered)

				assert.LessOrEqual(t, total, testCase.path.ceiling,
					"%s costs %d tokens under %s, against a ceiling of %d and a target of %d",
					testCase.path.name, total, e.name, testCase.path.ceiling, testCase.path.target)
			}
		})
	}
}

// field is one payload field the breakdown prices, so that a partitioning
// review has somewhere specific to start.
//
// "Would it be cheaper without this" is only answerable against a measurement
// of the same answer without it, and a review handed a total has to guess. Each
// entry here is re-encoded from the answer with the named keys removed, and
// priced against the same answer re-encoded with them kept, so both sides go
// through the same normalisation and the difference is the field rather than
// the marshaller.
type field struct {
	// name is what the record calls it.
	name string

	// call is the answer it is priced in.
	call call

	// keys are the JSON object keys removed, at any depth.
	keys []string
}

var fields = []field{
	{
		name: "the descriptions `--describe` adds",
		call: describeType,
		keys: []string{"description"},
	},
	{
		// Priced because it is the field which decides whether the mapping to a
		// foreign schema belongs in the default discovery answer. It does not:
		// the cold path is already within a handful of tokens of its target, and
		// this is paid once by the one caller which is exporting rather than on
		// every cold start by every caller which is not.
		name: "the classifications `--classification` adds",
		call: classifyTypes,
		keys: []string{"classifications"},
	},
	{
		name: "the whole claim `--evidence` adds",
		call: evidenceArea,
		keys: []string{"claim"},
	},
	{
		name: "the spans in `get`",
		call: getRoom,
		keys: []string{"span"},
	},
	{
		name: "the accuracy beside the value in `resolve`",
		call: resolveArea,
		keys: []string{"accuracy"},
	},
	{
		// A computed area rests on one claim per corner rather than on one
		// claim, so its budget grows with the shape while the figures do not.
		// Pricing it is what a review of this call has to start from, and the
		// answer is not obvious: the terms are what say which corner to
		// re-survey, and 0017 puts an accuracy term by term in the answer.
		name: "the error budget in `measure`",
		call: measureRoom,
		keys: []string{"budget"},
	},
	{
		// Half of that budget is where each contributing claim was written,
		// which is provenance rather than accuracy, and priced separately so
		// that the two are not weighed as one field.
		name: "the claims named under each budget term in `measure`",
		call: measureRoom,
		keys: []string{"contributors"},
	},
}

// without is the answer re-encoded with the named keys removed at any depth.
func without(t testing.TB, answer string, keys ...string) string {
	t.Helper()

	var tree any
	require.NoError(t, json.Unmarshal([]byte(answer), &tree))

	out, err := json.Marshal(strip(tree, keys))
	require.NoError(t, err)

	return string(out)
}

// strip removes the named keys from every object in the tree.
func strip(tree any, keys []string) any {
	switch value := tree.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, child := range value {
			if slices.Contains(keys, key) {
				continue
			}
			out[key] = strip(child, keys)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = strip(child, keys)
		}
		return out
	default:
		return value
	}
}

// TestTheQueryPathCostsLessThanReadingTheFiles is the comparison the gate
// exists to make.
//
// The budget above says the path is cheap in absolute terms. This says it is
// cheap relative to the alternative, which is the claim that decides whether
// the query surface is worth having at all: an agent that would have read the
// model instead has to come out ahead, and by enough that the round trips are
// worth making.
func TestTheQueryPathCostsLessThanReadingTheFiles(t *testing.T) {
	files, sources := modelFiles(t)

	answered := make([][]string, len(paths))
	for i, p := range paths {
		answered[i] = answers(t, p)
	}

	for _, e := range encodings {
		codec := codecFor(t, e)

		_, whole := readingCost(t, codec, files, sources)
		for i, p := range paths {
			_, total := cost(t, codec, answered[i])

			assert.Less(t, total*4, whole,
				"%s costs %d tokens under %s against %d to read the model, which is less than four times cheaper",
				p.name, total, e.name, whole)
		}
	}
}

// readingCost is what the alternative costs: every entity file of the model,
// and the one file the answer happened to be written in.
//
// Both, because they bracket the honest comparison. Reading the whole model is
// what an agent with no query surface has to do, and it is the number the bet
// is made against. Reading one file is the best case for a reader who already
// knows where to look — which is knowledge discovery is what supplies — so it
// is the harder of the two to beat and the one worth reporting beside it.
//
// The files are read by the caller and passed in for the reason a path's
// answers are: the bytes do not change with the encoding counting them.
func readingCost(t testing.TB, codec tokenizer.Codec, files []string, sources map[string][]byte) (perFile map[string]int, whole int) {
	t.Helper()

	perFile = make(map[string]int, len(files))
	for _, file := range files {
		n := tokens(t, codec, string(sources[file]))
		perFile[file] = n
		whole += n
	}

	return perFile, whole
}

// TestTheRecordedTokenBudgetIsCurrent checks that docs/token-budget.md still
// reports what this package measures.
//
// The record is the deliverable — a number in a file somebody can read without
// running anything — and a recorded number nothing checks is one that was true
// once. Regenerate it with `go test ./cmd/dfcad -update` and review the diff.
func TestTheRecordedTokenBudgetIsCurrent(t *testing.T) {
	measured := measurements(t)

	src, err := os.ReadFile(budgetRecord)
	require.NoError(t, err)

	before, after := surrounding(t, string(src))
	want := before + measured + after

	if *updateGolden {
		require.NoError(t, os.WriteFile(budgetRecord, []byte(want), 0o644))
		return
	}

	assert.Equal(t, want, string(src),
		"docs/token-budget.md is stale; regenerate it with: go test ./cmd/dfcad -update")
}

// surrounding splits the record either side of the generated block, returning
// everything up to and including the opening marker and everything from the
// closing one on.
func surrounding(t testing.TB, record string) (before, after string) {
	t.Helper()

	begin := strings.Index(record, beginMeasurements)
	require.GreaterOrEqual(t, begin, 0, "the record holds %s", beginMeasurements)

	end := strings.Index(record, endMeasurements)
	require.Greater(t, end, begin, "the record holds %s after %s", endMeasurements, beginMeasurements)

	return record[:begin+len(beginMeasurements)] + "\n", "\n" + record[end:]
}

// measurements renders the generated block of the record.
func measurements(t testing.TB) string {
	t.Helper()

	var out strings.Builder

	fmt.Fprintf(&out, "\n## The tokenizer\n\n")
	fmt.Fprintf(&out, "Counted with `%s`, at the version go.mod pins:\n\n", tokenizerModule)
	fmt.Fprintf(&out, "| Encoding | Version | The tokenizer of |\n")
	fmt.Fprintf(&out, "|----------|---------|------------------|\n")
	for _, e := range encodings {
		fmt.Fprintf(&out, "| `%s` | %s | %s |\n", e.name, tokenizerVersion(t), e.models)
	}

	files, sources := modelFiles(t)

	fmt.Fprintf(&out, "\n## The model\n\n")
	fmt.Fprintf(&out, "`cmd/dfcad/%s`, which holds %s.\n\n", budgetRoot, summary(t))
	fmt.Fprintf(&out, "| File | Bytes | Lines |\n")
	fmt.Fprintf(&out, "|------|-------|-------|\n")

	var totalBytes, totalLines int
	for _, file := range files {
		src := sources[file]
		totalBytes += len(src)
		totalLines += lineCount(src)

		fmt.Fprintf(&out, "| `%s` | %d | %d |\n",
			strings.TrimPrefix(file, budgetRoot+"/"), len(src), lineCount(src))
	}
	fmt.Fprintf(&out, "| **the model** | **%d** | **%d** |\n", totalBytes, totalLines)

	subject := subjectFile(t)

	answered := make([][]string, len(paths))
	for i, p := range paths {
		answered[i] = answers(t, p)
	}

	for i, p := range paths {
		fmt.Fprintf(&out, "\n## The cost of %s\n\n", p.name)
		fmt.Fprintf(&out, "Answering: %s.\n\n", p.what)
		fmt.Fprintf(&out, "| Call |")
		for _, e := range encodings {
			fmt.Fprintf(&out, " `%s` |", e.name)
		}
		fmt.Fprintf(&out, "\n|------|")
		for range encodings {
			fmt.Fprintf(&out, "-------|")
		}
		fmt.Fprintf(&out, "\n")

		costs := make([][]int, len(encodings))
		totals := make([]int, len(encodings))
		for j, e := range encodings {
			costs[j], totals[j] = cost(t, codecFor(t, e), answered[i])
		}
		for k, c := range p.calls {
			fmt.Fprintf(&out, "| `%s` |", c.name)
			for j := range encodings {
				fmt.Fprintf(&out, " %d |", costs[j][k])
			}
			fmt.Fprintf(&out, "\n")
		}
		fmt.Fprintf(&out, "| **the whole path** |")
		for j := range encodings {
			fmt.Fprintf(&out, " **%d** |", totals[j])
		}

		if !p.gated() {
			fmt.Fprintf(&out, "\n\nNo target: nothing asked this path to cost anything in particular. "+
				"Regression ceiling %d tokens.\n", p.ceiling)
			continue
		}

		verdict := "**missed**"
		if p.met(totals) {
			verdict = "**met**"
		}
		fmt.Fprintf(&out, "\n\nTarget %d tokens: %s. Regression ceiling %d tokens.\n",
			p.target, verdict, p.ceiling)
	}

	fmt.Fprintf(&out, "\n## Where the tokens go\n\n")
	fmt.Fprintf(&out, "What each answer costs with one field removed. Both figures in a cell are of the\n")
	fmt.Fprintf(&out, "answer re-encoded from its parsed form, so that the difference between them is the\n")
	fmt.Fprintf(&out, "field rather than the marshaller. That re-encoding sorts object keys, so a \"down\n")
	fmt.Fprintf(&out, "from\" figure differs by a token or two from the same call in the tables above.\n\n")
	fmt.Fprintf(&out, "| Field | Answer |")
	for _, e := range encodings {
		fmt.Fprintf(&out, " `%s` without it |", e.name)
	}
	fmt.Fprintf(&out, "\n|-------|--------|")
	for range encodings {
		fmt.Fprintf(&out, "--------|")
	}
	fmt.Fprintf(&out, "\n")
	for _, f := range fields {
		fmt.Fprintf(&out, "| %s | `%s` |", f.name, f.call.name)

		text := answer(t, f.call)
		with, withoutIt := without(t, text), without(t, text, f.keys...)
		for _, e := range encodings {
			codec := codecFor(t, e)

			fmt.Fprintf(&out, " %d, down from %d |",
				tokens(t, codec, withoutIt), tokens(t, codec, with))
		}
		fmt.Fprintf(&out, "\n")
	}

	fmt.Fprintf(&out, "\n## The cost of reading the files instead\n\n")
	fmt.Fprintf(&out, "| What is read |")
	for _, e := range encodings {
		fmt.Fprintf(&out, " `%s` |", e.name)
	}
	fmt.Fprintf(&out, "\n|--------------|")
	for range encodings {
		fmt.Fprintf(&out, "-------|")
	}
	fmt.Fprintf(&out, "\n")

	whole := make([]int, len(encodings))
	one := make([]int, len(encodings))
	for i, e := range encodings {
		perFile, total := readingCost(t, codecFor(t, e), files, sources)
		whole[i] = total
		one[i] = perFile[subject]
	}
	fmt.Fprintf(&out, "| the whole model |")
	for i := range encodings {
		fmt.Fprintf(&out, " %d |", whole[i])
	}
	fmt.Fprintf(&out, "\n| `%s` alone, the file the answer is written in |", strings.TrimPrefix(subject, budgetRoot+"/"))
	for i := range encodings {
		fmt.Fprintf(&out, " %d |", one[i])
	}
	fmt.Fprintf(&out, "\n")

	fmt.Fprintf(&out, "\n## The ratio\n\n")
	fmt.Fprintf(&out, "| Path | Against the whole model | Against the one file |\n")
	fmt.Fprintf(&out, "|------|-------------------------|----------------------|\n")
	for i, p := range paths {
		fmt.Fprintf(&out, "| %s |", p.name)

		var wholeRatios, oneRatios []string
		for j, e := range encodings {
			_, total := cost(t, codecFor(t, e), answered[i])
			wholeRatios = append(wholeRatios, ratio(whole[j], total))
			oneRatios = append(oneRatios, ratio(one[j], total))
		}
		fmt.Fprintf(&out, " %s |", strings.Join(wholeRatios, ", "))
		fmt.Fprintf(&out, " %s |\n", strings.Join(oneRatios, ", "))
	}
	fmt.Fprintf(&out, "\nOne figure per encoding, in the order of the table above.\n")

	return out.String()
}

// lineCount is how many lines a file holds.
//
// Canonical form ends with exactly one newline, so counting newlines is right
// for every file of this fixture. It is not right in general — a file whose
// last line has no terminator holds that line all the same — and a size table
// that quietly loses one is worse than no table, so the last line is counted
// whether or not anything terminates it.
func lineCount(src []byte) int {
	lines := bytes.Count(src, []byte("\n"))
	if len(src) > 0 && !bytes.HasSuffix(src, []byte("\n")) {
		lines++
	}
	return lines
}

// ratio is how many times cheaper now is than was, to one decimal place.
func ratio(was, now int) string {
	if now == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f×", float64(was)/float64(now))
}

// summary is the one line the engine's own loader writes about the model, which
// is the model's size stated by the thing that read it rather than by whoever
// wrote this file.
func summary(t testing.TB) string {
	t.Helper()

	graph, diagnostics := dfcad.LoadGraph(budgetRoot)
	require.Empty(t, diagnostics, "the representative model loads clean")

	return graph.Summary().String()
}

// tokenizerVersion is the version of the byte pair encoder the counts came
// from, read out of go.mod.
//
// Out of go.mod rather than written down here, because a version somebody
// retypes is one that goes stale at the next upgrade, and a record that names
// the wrong tokenizer is worse than one that names none. The test binary's own
// build information does not carry it: a test-only dependency of the package
// under test is not among [debug.ReadBuildInfo]'s deps.
func tokenizerVersion(t testing.TB) string {
	t.Helper()

	src, err := os.ReadFile(moduleFile)
	require.NoError(t, err)

	for line := range strings.Lines(string(src)) {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == tokenizerModule {
			return fields[1]
		}
	}

	t.Fatalf("%s requires %s", moduleFile, tokenizerModule)
	return ""
}

// BenchmarkDiscovery measures the two calls the query surface is partitioned
// around.
//
// It reports `tokens/op` beside `ns/op`, which is the figure the gate is about.
// The wall clock is measured too and is not the point: a discovery call that
// took a second and cost forty tokens would still be the right arrangement,
// where one that cost forty thousand would not be at any speed.
func BenchmarkDiscovery(b *testing.B) { benchmarkPath(b, discovery) }

// BenchmarkColdQuestion measures a dimensional answer from an agent that has
// read nothing: discovery, then `get`, then `resolve`.
func BenchmarkColdQuestion(b *testing.B) { benchmarkPath(b, coldQuestion) }

// BenchmarkReadingTheModel measures the alternative — every entity file of the
// model, read whole — which is what the two above are a saving against.
func BenchmarkReadingTheModel(b *testing.B) {
	codec := codecFor(b, encodings[0])

	var total int
	for b.Loop() {
		files, sources := modelFiles(b)
		_, total = readingCost(b, codec, files, sources)
	}

	b.ReportMetric(float64(total), "tokens/op")
}

// benchmarkPath times one path and reports what it costs.
//
// The calls are inside the loop, unlike everywhere else here, because a
// benchmark measures the work: an iteration that counted an answer somebody
// else had already fetched would report the tokenizer's speed rather than the
// path's.
func benchmarkPath(b *testing.B, p path) {
	codec := codecFor(b, encodings[0])

	var total int
	for b.Loop() {
		_, total = cost(b, codec, answers(b, p))
	}

	b.ReportMetric(float64(total), "tokens/op")
}
