#!/usr/bin/env bash
#
# Copyright (c) 2026 Z5Labs and Contributors
#
# This software is released under the MIT License.
# https://opensource.org/licenses/MIT
#
# selftest.sh runs gate.sh against the deliberately broken models beside it and
# requires it to block — non-zero, with every stage having answered, and with an
# annotation on the line each finding names.
#
# A gate which has never been seen to fail is not known to be a gate. Running
# the failure on every build is what keeps that true: a change which makes the
# gate silently pass everything — a swallowed exit code, a jq filter which
# stopped matching, a binary which is not there — fails here, in the run that
# made it.
#
# It requires the annotations as well as the exit code, because the gate has
# already been silently wrong in exactly the gap between the two. Contract v2
# made every span a string; the filters went on reading `.span.start.line`; the
# gate went on exiting non-zero by crashing, and for three months emitted no
# annotation on any repository. Every assertion below which counts an annotation
# or matches one against the span it came from is there for that.
#
# Usage:
#
#	selftest.sh --binary <dfcad>
#
# Run it from the repository root, which is what makes the paths in the second
# run's annotations the relative ones a reader recognises. It works from
# anywhere — the first run is made inside a repository it builds itself, and the
# second names its root absolutely from anywhere else — but the log then reads in
# absolute paths.

set -euo pipefail

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

binary=""
while [ $# -gt 0 ]; do
	case "$1" in
	--binary)
		binary="${2:-}"
		shift 2
		;;
	*)
		echo "selftest.sh: unknown argument $1" >&2
		exit 64
		;;
	esac
done

if [ -z "$binary" ]; then
	echo "selftest.sh: --binary is required" >&2
	exit 64
fi

if [ ! -x "$binary" ]; then
	echo "selftest.sh: $binary is not an executable" >&2
	exit 64
fi

# Resolved now, because one of the two gate runs below is made from a throwaway
# repository elsewhere on the disk, and `./dfcad` does not name the same file
# from there.
binary="$(cd -- "$(dirname -- "$binary")" && pwd)/$(basename -- "$binary")"

fail() {
	echo "::error::the gate did not block the deliberately broken model: $1"
	exit 1
}

# What the gate reads, against what this build writes.
#
# The two are the same number until somebody bumps the contract, and the run
# which bumps it is the one that has to notice: a filter reading the shape the
# previous version wrote matches nothing and reports nothing, which is
# indistinguishable from a stage with nothing to say. Asking the binary what it
# implements is what makes that a failure here rather than a silence everywhere.
contract="$("${here}/gate.sh" --contract)"
build_contract="$("$binary" version | jq -r '.contracts.output')"
if [ "$contract" != "$build_contract" ]; then
	echo "::error::gate.sh reads output contract ${contract} and this build of dfcad writes ${build_contract}: the filters in .github/gate/gate.sh are what have to change, and until they do the gate blocks without saying anything"
	exit 1
fi
echo "gate.sh and this build of dfcad both speak output contract ${contract}"

# run_gate runs one gate over one root and captures what it wrote.
#
# The gate's own annotations would be read by Actions as annotations on this
# run, and this run is the one where they are expected. They are captured rather
# than emitted so that a deliberate failure does not decorate a green pull
# request with errors somebody then goes looking for.
#
# GITHUB_STEP_SUMMARY goes with them. The gate appends a row to the run's
# runtime table, and these rows report the failures they were asked to produce:
# left in, a green run's table would carry lines reading exit 1 and exit 2, which
# is the one thing somebody skimming the summary would stop at.
gate_exit=0
run_gate() {
	local dir="$1" root="$2" results="$3"
	shift 3

	set +e
	(
		cd "$dir" &&
			env -u GITHUB_STEP_SUMMARY \
				"${here}/gate.sh" --binary "$binary" --root "$root" \
				--results "$results" "$@"
	) >"${results}/gate.log" 2>&1
	gate_exit=$?
	set -e

	# Prefixed, which is what makes them inert. Without it the gate's own
	# `::error::` lines would decorate this run with the failures which are
	# the point of the run, and its `::group::` lines would nest inside this
	# one, which Actions does not support.
	#
	# The prefix is a visible marker rather than indentation: Actions strips
	# leading whitespace before it looks for a workflow command, so two
	# spaces in front of `::error` neutralise nothing — measured on run
	# 30981085140, where an indented line still produced an annotation.
	# Anything non-blank ahead of the colons does work, and saying "captured"
	# says why the line is there.
	#
	# Only what is echoed is rewritten. The assertions below read the log
	# file, which still holds exactly what the gate wrote.
	sed 's/^::/[captured] ::/' "${results}/gate.log"
}

# annotated requires an annotation at the position a span names.
#
# The span is read out of the result object and taken apart here, by a second
# reading of the contract in a different engine from the gate's: a filter and an
# assertion which shared their parsing could agree with each other about a form
# neither of them reads correctly. `path:line:column-line:column`, numbers from
# the right, so a path holding a colon or a dash still parses.
annotated() {
	local level="$1" span="$2" log="$3" what="$4"
	local path line col

	if [[ "$span" =~ ^(.*):([0-9]+):([0-9]+)(-[0-9]+:[0-9]+)?$ ]]; then
		path="${BASH_REMATCH[1]}"
		line="${BASH_REMATCH[2]}"
		col="${BASH_REMATCH[3]}"
	else
		fail "${what} reported the span ${span}, which is not a position this contract writes"
	fi

	grep -qF "::${level} file=${path},line=${line},col=${col}::" "$log" ||
		fail "${what} is at ${span} and no ::${level} annotation names that line"
}

# counted requires a stage to have annotated at least once.
#
# Zero is what a filter which stopped reading the contract produces, and it is
# also what a stage with nothing wrong produces — which is why every stage
# counted here is one this model gives something to say.
counted() {
	local stage="$1" file="$2" want="$3"
	local got
	got="$(jq -r --arg stage "$stage" '.annotations[$stage]' "$file")"
	if [ "$got" -lt "$want" ]; then
		fail "the ${stage} stage emitted ${got} annotations, want at least ${want}: its filter has stopped reading what dfcad writes"
	fi
	echo "the ${stage} stage annotated ${got} findings"
}

################################################################################
# The model which loads and is wrong, reviewed against the revision before it.
################################################################################

# The review stage is the one which needs two revisions, so the self-test makes
# them: a throwaway repository holding broken/ as its merge base and broken/ as
# it stands on a branch off it. The difference between the two is carried by the
# `*.dfc.prior` files, each of which is its neighbour as the base holds it and
# none of which the loader walks.
#
# Building the pair here rather than relying on this repository's own history is
# what makes the stage testable at all: the history of a checkout says nothing
# about a fixture, and a self-test which waited for somebody to change broken/
# would exercise the review filter on no run at all.
repo="$(mktemp -d)"
results_loads="$(mktemp -d)"

prior_count="$(find "${here}/broken" -name '*.dfc.prior' | wc -l)"
if [ "$prior_count" -eq 0 ]; then
	fail "broken/ holds no *.dfc.prior, so there is no prior revision for the review stage to be run against"
fi

cp -R "${here}/broken" "${repo}/broken"
while IFS= read -r -d '' prior; do
	cp "$prior" "${prior%.prior}"
done < <(find "${repo}/broken" -name '*.dfc.prior' -print0)

git init -q -b main "$repo"
git -C "$repo" add -A
git -C "$repo" -c user.name=selftest -c user.email=selftest@example.org \
	-c commit.gpgsign=false commit -qm "the model as the merge base holds it"
git -C "$repo" checkout -q -b change

cp -R "${here}/broken/." "${repo}/broken/"
git -C "$repo" add -A
git -C "$repo" -c user.name=selftest -c user.email=selftest@example.org \
	-c commit.gpgsign=false commit -qm "widen the room without measuring it"

echo "::group::self-test: the gate blocks a model which loads and is wrong"
run_gate "$repo" broken "$results_loads" --against main
echo "::endgroup::"

if [ "$gate_exit" -eq 0 ]; then
	fail "gate.sh exited 0 over broken/"
fi

log="${results_loads}/gate.log"
fmt_json="${results_loads}/broken.fmt.json"
check_json="${results_loads}/broken.check.json"
review_json="${results_loads}/broken.review.json"
review_md="${results_loads}/broken.review.md"
timing_json="${results_loads}/broken.timing.json"
annotations_json="${results_loads}/broken.annotations.json"

# timing.json among them, and this is the run where that matters: it is written
# after every stage has answered, so a gate which died partway through writing
# its annotations leaves it missing. That is exactly how the contract bump was
# eventually noticed, months late.
for file in "$fmt_json" "$check_json" "$review_json" "$review_md" \
	"$timing_json" "$annotations_json"; do
	[ -s "$file" ] || fail "$(basename "$file") was not written for a run which found violations"
done

if [ "$(jq -r '.contract' "$annotations_json")" != "$contract" ]; then
	fail "the gate recorded a contract other than ${contract}"
fi
if [ "$(jq -r '.unreadable' "$annotations_json")" != "false" ]; then
	fail "the gate could not read one of the results it wrote"
fi

# What each stage decided, read from the results it wrote rather than from its
# log: the log is for a person and the JSON is the contract, and a self-test
# which grepped prose would pass on a run whose prose merely still looked right.
unformatted="$(jq -r '[.files[] | select(.status == "unformatted") | .path] | join(",")' "$fmt_json")"
if [ "$unformatted" != "broken/unformatted.dfc" ]; then
	fail "fmt --check flagged [${unformatted}], want only broken/unformatted.dfc"
fi

violations="$(jq -r '.violations | length' "$check_json")"
if [ "$violations" -lt 1 ]; then
	fail "check found ${violations} violations in a model whose type states an invariant its instance breaks"
fi

findings="$(jq -r '[.findings[] | select(.ruling != "ignored")] | length' "$review_json")"
if [ "$findings" -lt 1 ]; then
	fail "review found ${findings} findings between two revisions which move a corner with no measurement behind it"
fi

counted fmt "$annotations_json" 1
counted check "$annotations_json" 1
counted review "$annotations_json" 1

# A file which is merely not canonical has no line to land on, because what is
# wrong with it is the whole file's shape.
if ! grep -qF "::error file=broken/unformatted.dfc::" "$log"; then
	fail "no annotation named broken/unformatted.dfc"
fi

# And every finding which does name a line is annotated on it. This is the
# assertion the three months of silence would have failed: the filters were
# reading a shape the tool had stopped writing, and nothing between the exit code
# and the log could tell.
annotated error "$(jq -r '.violations[0].subject' "$check_json")" "$log" \
	"the first check violation"
annotated warning "$(jq -r '[.findings[] | select(.ruling != "ignored")][0].span' "$review_json")" "$log" \
	"the first review finding"

################################################################################
# The model which does not load.
################################################################################

# The other half of what the gate has to survive, and it cannot be the same
# model root: a file which does not parse stops the whole model loading, and a
# model which does not load runs no rule and produces no violation with a span
# on it. One root can hold the spanned check violation or the file which does not
# parse, and not both.
results_unloadable="$(mktemp -d)"
root_unloadable="${here#"$PWD"/}/unloadable"

echo "::group::self-test: the gate blocks ${root_unloadable}"
run_gate "$PWD" "$root_unloadable" "$results_unloadable"
echo "::endgroup::"

if [ "$gate_exit" -eq 0 ]; then
	fail "gate.sh exited 0 over ${root_unloadable}"
fi

log="${results_unloadable}/gate.log"
slug="$(printf '%s' "$root_unloadable" | tr -c 'A-Za-z0-9._-' '-' | sed 's/^[.-]*//')"
fmt_json="${results_unloadable}/${slug}.fmt.json"
check_json="${results_unloadable}/${slug}.check.json"
timing_json="${results_unloadable}/${slug}.timing.json"
annotations_json="${results_unloadable}/${slug}.annotations.json"

for file in "$fmt_json" "$check_json" "$timing_json" "$annotations_json"; do
	[ -s "$file" ] || fail "$(basename "$file") was not written"
done

# check is expected to refuse the model outright, because the node naming an
# undeclared type is a load failure rather than a rule which did not hold. The
# summary is what says the run got that far and reported nothing it did not run.
ran="$(jq -r '.summary.ran' "$check_json")"
if [ "$ran" != "0" ]; then
	fail "check reported ${ran} rules run against a model which does not load"
fi

# The summary above is the same summary a model with nothing wrong with it
# produces, so the object has to say which of the two it is. Without it, a
# consumer reading stdout — which is what the contract tells it to read — is
# told a model is sound when nothing has looked at it.
refused="$(jq -r '.refused' "$check_json")"
if [ "$refused" != "true" ]; then
	fail "check did not report a model which does not load as refused"
fi

# A file which does not parse carries a diagnostic with a span, which is the
# fmt filter's other branch and the only place in either model where it is
# exercised.
counted fmt "$annotations_json" 1
annotated error "$(jq -r '[.files[] | select(.status == "failed") | .diagnostics[0].span][0]' "$fmt_json")" \
	"$log" "the first fmt diagnostic"

# A model which does not load has nothing on stdout for the check stage to
# annotate from, so the gate says so itself and the log carries the file, the
# line and the caret. A stage which failed with no annotation at all reads as a
# stage which passed.
if [ "$(jq -r '.annotations.check' "$annotations_json")" != "0" ]; then
	fail "check annotated a model it could not load, which it has no machine form to annotate from"
fi
if ! grep -qF "::error::${root_unloadable} did not load" "$log"; then
	fail "no annotation said ${root_unloadable} did not load"
fi
if ! grep -qE "^${root_unloadable}/entities/room\.dfc:[0-9]+:[0-9]+: error: " "$log"; then
	fail "no diagnostic named ${root_unloadable}/entities/room.dfc with a line and a column"
fi

echo "the gate blocked both deliberately broken models, as it must, and annotated every finding which named a line"
