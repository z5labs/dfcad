#!/usr/bin/env bash
#
# Copyright (c) 2026 Z5Labs and Contributors
#
# This software is released under the MIT License.
# https://opensource.org/licenses/MIT
#
# gate.sh runs `dfcad fmt --check` and `dfcad check` over one model root and
# says, by its exit code, whether that model may be merged. Given --against it
# runs `dfcad review` as well, which asks the question the other two cannot: not
# whether this revision of the model is sound, but whether the change to it
# needs an explanation.
#
# It is the repository-specific half of CI. The Go half — fmt, vet, lint and
# `go test -race` — belongs to the Go chain's Ci in the z5labs daggerverse module and is
# not restated here or anywhere else in this repository. See README.md beside
# this file for what a consuming data repository has to change to adopt it.
#
# Usage:
#
#	gate.sh --binary <dfcad> --root <model root> [--results <dir>] \
#	        [--against <ref>] [--policy <check>=<ruling>]...
#	gate.sh --image <ref> [--runtime <command>] --root <model root> ...
#	gate.sh --contract
#
# Run it from the directory paths should be reported relative to, which in CI is
# the repository root: the paths dfcad writes are the ones it walked, and
# GitHub resolves an annotation's file against the repository root.
#
# The engine is either a dfcad binary on this host (--binary) or the published
# image, pinned by digest and run through a container runtime (--image). The
# second is what a data repository has: the registry is the distribution
# channel, and there is no file to install. Everything after the engine is
# chosen is the same for both, so an image run annotates exactly what a binary
# run does, at exactly the same paths.

set -euo pipefail

# The version of the machine output contract the filters below are written
# against. Every result object dfcad writes carries the same number in
# `.version`, and every stage here refuses to read one which does not match.
#
# It is checked rather than assumed because of what a mismatch looks like from
# outside: contract v2 made every span a string where it had been two nested
# objects, the filters went on reading `.span.start.line`, and for three months
# the gate emitted no annotation on any repository while still exiting non-zero.
# A gate which blocks and says nothing looks exactly like a gate doing its job.
#
# `gate.sh --contract` prints it, which is how selftest.sh asks the binary it is
# about to run whether its `.contracts.output` is the one these filters read.
readonly OUTPUT_CONTRACT=2

binary=""
image=""
runtime=""
root=""
results=""
against=""
policies=()

usage() {
	cat >&2 <<'EOF'
usage: gate.sh --binary <dfcad> --root <model root> [--results <dir>] \
               [--against <ref>] [--policy <check>=<ruling>]...
       gate.sh --image <ref> [--runtime <command>] --root <model root> ...
       gate.sh --contract

	--binary   the dfcad executable to run. In CI this is the binary the
	           standard pipeline built, so the gate and the shipped artifact
	           cannot diverge.
	--image    the dfcad image to run instead of a binary, pinned by digest:
	           ghcr.io/z5labs/dfcad@sha256:<digest>. It is pulled if the
	           runtime does not already hold it, and nothing but the runtime
	           need be installed. Exactly one of --binary and --image is given.
	--runtime  the container runtime --image is run with. Defaults to docker;
	           podman works too.
	--root     the model root to gate.
	--results  where to write the structured results. Defaults to a temporary
	           directory, which is what a local run wants; CI passes a path it
	           then uploads.
	--against  the branch this revision is being merged into. Given one, the
	           gate also runs `dfcad review` against the merge base of it and
	           HEAD. Left out, the review stage does not run at all: a checkout
	           with no history to reach — a tarball, a shallow clone, a model
	           root which is not in a repository — has no second revision to be
	           compared with, and a gate which refused those would be one
	           nobody could adopt incrementally.
	--policy   what one kind of review finding means: failure, warning or
	           ignored. Repeatable, and passed straight through.
	--contract print the machine output contract version this gate reads and
	           exit. A build whose `dfcad version` reports a different
	           `.contracts.output` writes a shape these filters do not read.

Exit codes: 0 the model may be merged, 1 it may not, 64 a usage error, and 69
the engine could not be run at all — no runtime, or an image which will not
pull or will not start. A 69 ran no stage, and is never a pass.
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--binary)
		binary="${2:-}"
		shift 2
		;;
	--image)
		image="${2:-}"
		shift 2
		;;
	--runtime)
		runtime="${2:-}"
		shift 2
		;;
	--root)
		root="${2:-}"
		shift 2
		;;
	--results)
		results="${2:-}"
		shift 2
		;;
	--against)
		against="${2:-}"
		shift 2
		;;
	--policy)
		policies+=(--policy "${2:-}")
		shift 2
		;;
	--contract)
		printf '%s\n' "$OUTPUT_CONTRACT"
		exit 0
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "gate.sh: unknown argument $1" >&2
		usage
		exit 64
		;;
	esac
done

if [ -n "$binary" ] && [ -n "$image" ]; then
	echo "gate.sh: --binary and --image each name the engine to gate with; pass one of them" >&2
	usage
	exit 64
fi

if { [ -z "$binary" ] && [ -z "$image" ]; } || [ -z "$root" ]; then
	echo "gate.sh: --root is required, and so is one of --binary and --image" >&2
	usage
	exit 64
fi

if [ -n "$runtime" ] && [ -z "$image" ]; then
	echo "gate.sh: --runtime says what runs --image, and no --image was given" >&2
	usage
	exit 64
fi

if [ -n "$binary" ] && [ ! -x "$binary" ]; then
	echo "gate.sh: $binary is not an executable" >&2
	exit 64
fi

if [ ! -d "$root" ]; then
	echo "gate.sh: $root is not a directory" >&2
	exit 64
fi

if [ -z "$results" ]; then
	results="$(mktemp -d)"
fi
mkdir -p "$results"

# Where an image run's review stage materialises the merge base, removed again
# however the gate ends. Empty on every other run.
base_tree=""
cleanup() {
	if [ -n "$base_tree" ]; then
		rm -rf -- "$base_tree"
	fi
}
trap cleanup EXIT

# unavailable is the one way the gate stops after its arguments were accepted
# and before any stage ran: a sentence on stderr and exit 69. A gate which could
# not start its engine ran nothing, and must never read as one which ran and
# found nothing.
unavailable() {
	echo "gate.sh: $1; the gate ran no stage over ${root}" >&2
	exit 69
}

# dfcad is the engine, as the words a stage is run with. Every stage below runs
# `"${dfcad[@]}" <command> <flags>` and none of them knows which kind of engine
# it has, which is what makes an image run annotate exactly as a binary run
# does rather than nearly.
dfcad=()

if [ -n "$binary" ]; then
	dfcad=("$binary")
else
	runtime="${runtime:-docker}"

	if ! command -v "$runtime" >/dev/null 2>&1; then
		unavailable "the container runtime ${runtime} is not on the path, so ${image} cannot be run: install it, or name the one this runner has with --runtime"
	fi

	# Pulled only when the runtime does not already hold it, so that an image
	# loaded locally — which is how the self-test is handed the one the pipeline
	# just built — is run without a registry being asked about it. The pull's
	# progress goes to stderr, because stdout is not this script's to fill with
	# prose.
	if ! "$runtime" image inspect "$image" >/dev/null 2>&1; then
		if ! "$runtime" pull "$image" >&2; then
			unavailable "${runtime} could not pull ${image}"
		fi
	fi

	# The image runs as 65532, which can read a checkout but cannot write into
	# the results directory, and the review stage writes its summary there. So
	# it runs as the user invoking the gate, whose files those are. A rootless
	# runtime maps its container's root back to exactly that user, and maps the
	# user's own id to a subordinate one which owns nothing here — so there the
	# user to run as is 0, which is nobody's root.
	rootless=false
	case "$(basename -- "$runtime")" in
	podman)
		if [ "$("$runtime" info --format '{{.Host.Security.Rootless}}' 2>/dev/null || true)" = true ]; then
			rootless=true
		fi
		;;
	*)
		if "$runtime" info --format '{{json .SecurityOptions}}' 2>/dev/null | grep -q 'name=rootless'; then
			rootless=true
		fi
		;;
	esac
	if [ "$rootless" = true ]; then
		user="0:0"
	else
		user="$(id -u):$(id -g)"
	fi

	# Every directory the engine reads is mounted at the path it has on this
	# host, and the container starts in this directory. That is the whole of
	# how an image run's paths come out the same as a binary run's: dfcad
	# writes the paths it walked, and inside the container it walks exactly the
	# paths it was given out here. Mounting the model at /model instead would
	# put /model/... into every annotation, and GitHub would resolve none of
	# them.
	#
	# The working directory and the model are mounted read-only, because no
	# stage writes to either. The results directory is writable, because the
	# review stage's summary is written into it from inside the container.
	root_abs="$(cd -- "$root" && pwd)"
	results_abs="$(cd -- "$results" && pwd)"
	mounts=(-v "${PWD}:${PWD}:ro")
	case "${root_abs}/" in
	"${PWD%/}"/*) ;;
	*) mounts+=(-v "${root_abs}:${root_abs}:ro") ;;
	esac
	mounts+=(-v "${results_abs}:${results_abs}")

	# image_engine sets dfcad to run the image over what is mounted now. It is
	# a function because the review stage mounts one more tree before it runs.
	#
	# No network, because the engine speaks no protocol and a gate has no
	# reason to hand it one. The SELinux label is disabled rather than every
	# mount relabelled: `:z` would rewrite the labels on the checkout of a host
	# which enforces SELinux, and does nothing on one which does not.
	image_engine() {
		dfcad=(
			"$runtime" run --rm
			--network none
			--security-opt label=disable
			--user "$user"
			"${mounts[@]}"
			-w "$PWD"
			"$image"
		)
	}
	image_engine

	# Asked before any stage, so that an image which will not start — built
	# for a platform this runner cannot execute, or refused a mount — is one
	# sentence here rather than three stages which each wrote nothing. The
	# answer is also the log's record of which engine this run gated with.
	if ! identity="$("${dfcad[@]}" version)" ||
		! identity="$(jq -er '"dfcad \(.build.version) (commit \(.build.commit), output contract \(.contracts.output))"' <<<"$identity" 2>/dev/null)"; then
		unavailable "${image} did not answer \`dfcad version\` under ${runtime}"
	fi
	echo "gate.sh: gating with ${identity}, from ${image}" >&2
fi

# A model root is named in a result file, and a path is not a filename. The
# slashes become dashes so that two roots gated in one run do not overwrite
# each other's results. Leading dots and dashes go with them: a root beginning
# with one — .github/gate/broken, which is the self-test's — would otherwise
# name a hidden file, and actions/upload-artifact leaves hidden files out unless
# it is told otherwise, so those are results nobody uploads.
#
# Stripping can take the whole name, and for the likeliest root of all: a data
# repository gating the tree it is checked out in passes `--root .`, which
# sanitizes to nothing and would leave the results named `.fmt.json`. The
# fallback is what keeps that case from being the one that silently produces no
# artifact.
slug="$(printf '%s' "$root" | tr -c 'A-Za-z0-9._-' '-' | sed 's/^[.-]*//')"
if [ -z "$slug" ]; then
	slug="model-root"
fi

# The field separator between jq and the loops which read it. It is the ASCII
# unit separator rather than a tab because `read` folds runs of whitespace into
# one delimiter however IFS is set, so a record with an empty field — a file
# which is merely not canonical, and so has no line to report — would arrive
# with its message in the line's place.
readonly FS=$'\x1f'

# annotate emits one GitHub Actions annotation. Outside Actions the workflow
# commands mean nothing and the line still reads, which is what makes a local
# run of this script worth reading.
#
# A message is one line: GitHub reads a newline as the end of the command, so a
# multi-line message would put everything after the first line into the log as
# prose and lose it from the annotation.
annotate() {
	local level="$1" file="$2" line="$3" col="$4" message="$5"
	local location="file=${file}"
	[ -n "$line" ] && location="${location},line=${line}"
	[ -n "$col" ] && location="${location},col=${col}"
	printf '::%s %s::%s\n' "$level" "$location" "${message//$'\n'/ }"
}

# span_start is the jq every stage reads a position with, prepended to each
# filter below so that there is one reading of a span in this file rather than
# three.
#
# Under contract 2 a span is the string `path:line:column-line:column`, or
# `path:line:column` where its two ends are one point. The numbers are read from
# the right, which is what lets a path holding a colon or a dash parse
# unambiguously: the greedy `.*` gives back only the shortest tail which is a
# position, so `weird:path-1.dfc:5:7` is a file named `weird:path-1.dfc` at 5:7
# rather than a file named `weird` at a position nobody wrote.
#
# A span carrying no position is the path alone. It comes back with empty line
# and column and is annotated at file level rather than dropped, because a
# finding whose position is missing is still a finding.
#
# The fallback works because `capture` on input which does not match yields
# **empty** rather than raising — it is `match` underneath, and `match` yields no
# output — so `//` takes the right-hand side. Measured on jq 1.8.1, and it has
# been read the other way in review, which is why it is written down here.
#
# What `capture` does raise on is input which is not a string, and that is
# deliberately not caught. A span which arrives as an object is the v2 change
# running backwards — the shape this whole file was broken by — and emit turns
# that raise into a contract error naming the stage. A `try ... catch` here would
# turn it into a file-level annotation instead, which is the quiet failure again.
readonly SPAN_START='
def span_start:
	if . == null or . == "" then
		{path: "", line: "", column: ""}
	else
		capture("^(?<path>.*):(?<line>[0-9]+):(?<column>[0-9]+)(-[0-9]+:[0-9]+)?$")
		// {path: ., line: "", column: ""}
	end;
'

# What every filter's last step is. One record per annotation, five fields, the
# level first — so that fmt, check and review differ only in how they reach the
# finding and not in how it is emitted.
#
# The newline goes because `read` below splits on them whatever IFS says, so a
# message carrying one would arrive as two records with the second one's fields
# in the wrong places.
readonly RECORD='| join($fs) | gsub("\n"; " ")'

# What each stage annotated. A filter which stops reading the contract emits
# nothing, and a stage with nothing wrong emits nothing too; the count is what
# selftest.sh tells those two apart with, and it is the assertion the three
# months of silence would have failed.
fmt_annotations=0
check_annotations=0
review_annotations=0

# contract_error records that a result object could not be read at all. It is
# neither a violation in the model nor a stage which passed, so it fails the
# gate on its own: a gate which cannot read what the tool wrote does not know
# whether the model is sound, and must not say that it is.
contract_error=0

# emit runs one stage's filter over the result it wrote and annotates from what
# comes back. It is the only place in this file an annotation is emitted from.
#
# The result's `.version` is checked before the filter runs. A contract this
# file was not written against is reported once, as an annotation of its own,
# rather than as a jq error in the middle of whatever the stage printed — and
# the count is left at zero rather than reported as a stage which found nothing.
#
# It sets annotation_count rather than returning it, for the reason run_stage
# sets stage_exit: `return` is the exit code, and this one has to survive
# `set -e`. The filter's output is read into a variable first because a `while`
# loop at the end of a pipeline runs in a subshell, where a count would be
# incremented and then thrown away.
annotation_count=0
emit() {
	local stage="$1" file="$2" filter="$3"
	local version records status count=0

	annotation_count=0

	# Every one of these says the same thing in the end — nothing below this
	# line was annotated — and each says it as an error of its own rather than
	# against a file. The failure is in the gate, not at a line of anybody's
	# model, and an annotation pointing at a result file in a temporary
	# directory would be one GitHub resolves against the repository and drops.
	if [ ! -s "$file" ]; then
		contract_error=1
		echo "::error::the ${stage} stage wrote no result object, so nothing it found is annotated below"
		return 0
	fi

	version="$(jq -r '.version // "absent"' "$file" 2>/dev/null)" || version="unreadable"
	if [ "$version" != "$OUTPUT_CONTRACT" ]; then
		contract_error=1
		echo "::error::gate.sh reads output contract ${OUTPUT_CONTRACT} and the ${stage} stage wrote ${version}: nothing it found is annotated below, and the filters in .github/gate/gate.sh are what have to change"
		return 0
	fi

	set +e
	records="$(jq -r --arg fs "$FS" "${SPAN_START}${filter}${RECORD}" "$file")"
	status=$?
	set -e
	if [ "$status" -ne 0 ]; then
		contract_error=1
		echo "::error::gate.sh could not read the ${stage} stage's result: the filter failed against the object dfcad wrote, so nothing it found is annotated below"
		return 0
	fi

	if [ -n "$records" ]; then
		while IFS="$FS" read -r level path line col message; do
			annotate "$level" "$path" "$line" "$col" "$message"
			count=$((count + 1))
		done <<<"$records"
	fi
	annotation_count=$count
}

# now_ms is the wall clock in milliseconds. Runtime is recorded so that a check
# set which becomes slow is visible in the run that made it slow, rather than
# the first time somebody notices CI dragging.
#
# %N is GNU date's, which is what the ubuntu runners have. On a host whose date
# is BSD's this prints the seconds with a literal "3N" after them, and the
# timings become nonsense while the gate's verdict stays correct — the numbers
# are a record, not a threshold anything is compared against.
now_ms() {
	date +%s%3N
}

# run_stage runs one dfcad subcommand, keeping the two streams apart the way the
# machine output contract requires: stdout is the result object and goes to a
# file, stderr is for whoever wrote the model and goes to the log.
#
# It sets stage_exit and stage_ms rather than returning them, because the exit
# code is the answer and a `return` here would collide with `set -e`.
stage_exit=0
stage_ms=0
run_stage() {
	local out="$1"
	shift

	local started ended
	started="$(now_ms)"
	set +e
	"${dfcad[@]}" "$@" >"$out"
	stage_exit=$?
	set -e
	ended="$(now_ms)"
	stage_ms=$((ended - started))
}

fmt_json="${results}/${slug}.fmt.json"
check_json="${results}/${slug}.check.json"

echo "::group::dfcad fmt --check --root ${root}"
run_stage "$fmt_json" fmt --check --root "$root"
fmt_exit=$stage_exit
fmt_ms=$stage_ms

# Every file the run flagged, annotated where it stands. A file which does not
# parse carries diagnostics with a span, so the annotation lands on the line; a
# file which parses and is merely not canonical has no span to land on, because
# what is wrong with it is the whole file's shape.
emit fmt "$fmt_json" '
	.files[]
	| select(.status == "unformatted" or .status == "failed")
	| if (.diagnostics // []) | length > 0 then
		.path as $path
		| .diagnostics[]
		| (.span | span_start) as $at
		| ["error", $path, $at.line, $at.column, .message]
	  else
		["error", .path, "", "", (.error // "not in canonical form: run dfcad fmt to rewrite it")]
	  end
'
fmt_annotations=$annotation_count

# The exact hunks, so that a formatting failure is fixed from the log rather
# than from a local reproduction. --diff writes nothing and implies --check, so
# this cannot change what the gate just decided.
if [ "$fmt_exit" -ne 0 ]; then
	"${dfcad[@]}" fmt --diff --root "$root" >/dev/null || true
fi
echo "::endgroup::"

echo "::group::dfcad check --root ${root}"
run_stage "$check_json" check --root "$root" -v
check_exit=$stage_exit
check_ms=$stage_ms

# A violation carries the span of the thing which failed and the span of the
# rule which failed it. The annotation goes on the subject, because that is the
# thing somebody has to change, and names the rule's position in the message so
# the other end is one click away in the log.
#
# `declared` goes into the message as it was written. A span is a string under
# this contract, and the string is exactly the `path:line:column` form an editor
# and a terminal already jump to, so taking it apart to put it back together
# would only be a way of getting it wrong.
emit check "$check_json" '
	.violations[]
	| (.subject | span_start) as $at
	| [
		"error",
		$at.path,
		$at.line,
		$at.column,
		(.check + " failed on " + .instance + ": " + .message
		 + " (declared at " + .declared + ")")
	  ]
'
check_annotations=$annotation_count

# A model which does not load exits 2 with its diagnostics rendered on stderr
# above and nothing on stdout to annotate from: the machine form of a load
# diagnostic is carried by the commands which report per file, and check
# reports per rule. The log has the file, the line and the caret; this says so
# rather than leaving the run with no annotation at all.
if [ "$check_exit" -eq 2 ]; then
	echo "::error::${root} did not load; the diagnostics above name the file and line"
fi
echo "::endgroup::"

# The third question, and the one neither of the others can be asked: not
# whether this revision of the model is sound, but whether the change to it
# needs an explanation. It runs only when a branch to compare against was named,
# because it is the one stage which needs two revisions rather than one.
#
# From the image it runs differently, and says the same thing. `review
# --against` reads the merge base out of git, and the published image carries no
# git (docs/decisions/0026). So an image run does the git half on this runner —
# which has git, because it checked the model out — and hands the image both
# trees with `review --base-root`. What that gives up is attribution: a finding
# names no commit, because the image is given no history to name one from.

# base_root is the model root as the merge base holds it, once materialise_base
# has written it out.
base_root=""

# materialise_base writes the merge base of HEAD and --against into a directory
# of its own and sets base_root to the model root within it. It is the part of
# `review --against` the image cannot do, and it refuses what that refuses, for
# the same reasons: a model root outside a working tree has no second revision,
# and a shallow history reports a merge base at the point it was cut off —
# which the two revisions never shared, and a review against which would report
# the branch's whole ancestry as this change.
#
# The whole revision is archived rather than a checkout added, as the engine
# does it: a worktree is a mutation of the repository, and one left behind by a
# killed run refuses the next. It fails with a sentence on stderr, and the
# caller turns that into the stage's exit 2.
materialise_base() {
	local toplevel prefix shallow merge_base

	if ! command -v git >/dev/null 2>&1; then
		echo "gate.sh: the review stage reads the merge base out of git on this runner, and git is not on the path" >&2
		return 1
	fi

	if ! toplevel="$(git -C "$root" rev-parse --show-toplevel 2>/dev/null)"; then
		echo "gate.sh: ${root} is not inside a git working tree, so there is no revision to review it against" >&2
		return 1
	fi
	prefix="$(git -C "$root" rev-parse --show-prefix)"
	# The file listing where a shallow history is cut off, which is empty or
	# absent for a full one. `--git-path` answers relative to the directory
	# git was run in unless the path is absolute; it is joined here rather than
	# asked for with `--path-format=absolute`, which needs git 2.31, because
	# this script is copied onto runners whose git nobody here chose.
	shallow="$(git -C "$root" rev-parse --git-path shallow)"
	case "$shallow" in
	/*) ;;
	*) shallow="${root}/${shallow}" ;;
	esac

	if ! merge_base="$(git -C "$root" merge-base HEAD "$against" 2>/dev/null)"; then
		if [ -s "$shallow" ]; then
			echo "gate.sh: the history of ${toplevel} is shallow and does not reach its merge base with ${against}: fetch the full history — \`git fetch --unshallow\`, or \`fetch-depth: 0\` on actions/checkout" >&2
		else
			echo "gate.sh: HEAD and ${against} have no merge base, so there is no revision to review ${root} against" >&2
		fi
		return 1
	fi
	if [ -s "$shallow" ] && grep -qxF "$merge_base" "$shallow"; then
		echo "gate.sh: the history of ${toplevel} is shallow and is cut off at ${merge_base}, which git then reports as the merge base with ${against}: fetch the full history — \`git fetch --unshallow\`, or \`fetch-depth: 0\` on actions/checkout" >&2
		return 1
	fi

	base_tree="$(mktemp -d)"
	if ! git -C "$toplevel" archive --format=tar "$merge_base" | tar -x -f - -C "$base_tree"; then
		echo "gate.sh: the merge base ${merge_base} could not be read out of ${toplevel}" >&2
		return 1
	fi

	base_root="${base_tree}${prefix:+/${prefix%/}}"
	mounts+=(-v "${base_tree}:${base_tree}:ro")
	image_engine
	echo "gate.sh: comparing against the merge base ${merge_base} of HEAD and ${against}, read out of git on this runner" >&2
}

review_json="${results}/${slug}.review.json"
review_md="${results}/${slug}.review.md"
review_exit=0
review_ms=0

if [ -n "$against" ]; then
	echo "::group::dfcad review --root ${root} --against ${against}"
	if [ -z "$image" ]; then
		run_stage "$review_json" review --root "$root" --against "$against" \
			--annotate "$review_md" "${policies[@]+"${policies[@]}"}"
		review_exit=$stage_exit
		review_ms=$stage_ms
	elif materialise_base; then
		# The summary is named absolutely, because it is written from inside
		# the container and the results directory is mounted there at the
		# path it has out here, which a relative name need not reach.
		run_stage "$review_json" review --root "$root" --base-root "$base_root" \
			--annotate "${results_abs}/${slug}.review.md" "${policies[@]+"${policies[@]}"}"
		review_exit=$stage_exit
		review_ms=$stage_ms
	else
		# What `review --against` does when it cannot reach its second
		# revision: exit 2, nothing on stdout, the reason on stderr above. The
		# empty result is then reported below exactly as it is for a binary
		# run which failed the same way.
		: >"$review_json"
		review_exit=2
	fi

	# A finding carries the span of the change and the ruling the policy gave
	# it, so the annotation lands on the line and says how much it matters. A
	# finding the policy acknowledged is in the result and nowhere else, which
	# is what "ignored" means, so it is filtered out here as it is on stderr.
	#
	# The span of a finding whose side is "base" points into the merge base,
	# which is a file this checkout may no longer hold — GitHub then drops the
	# annotation, and the message still reads in the log. Saying which revision
	# the line is in is what keeps that from looking like a wrong line number.
	emit review "$review_json" '
		.findings[]
		| select(.ruling != "ignored")
		| (.span | span_start) as $at
		| [
			(if .ruling == "failure" then "error" else "warning" end),
			$at.path,
			$at.line,
			$at.column,
			(.kind + " on " + .subject + " (in the " + .side + " revision): " + .message
			 + (if .commit then " [" + (.commit.sha[0:12]) + " " + .commit.summary + "]" else "" end))
		  ]
	'
	review_annotations=$annotation_count

	# A review which could not read one of its two revisions exits 2 with
	# nothing on stdout to annotate from, and the reason — a shallow checkout, a
	# branch which is not there, a merge base which does not load — is on stderr
	# above. It is said here too, because a stage which failed with no
	# annotation at all reads as a stage which passed.
	if [ "$review_exit" -eq 2 ]; then
		echo "::error::${root} could not be reviewed against ${against}; the reason is above"
	fi
	echo "::endgroup::"
fi

# Written on every run, including — and especially — the runs where a stage
# found something. A gate is slowest on the model it has the most to say about,
# so timings recorded only when everything passed would be the ones nobody
# needs; and this file going missing was how the annotations were found to have
# stopped, three months after they did.
total_ms=$((fmt_ms + check_ms + review_ms))
timing="${results}/${slug}.timing.json"
jq -n \
	--arg root "$root" \
	--argjson fmt "$fmt_ms" \
	--argjson check "$check_ms" \
	--argjson review "$review_ms" \
	--argjson total "$total_ms" \
	'{root: $root, milliseconds: {fmt: $fmt, check: $check, review: $review, total: $total}}' \
	>"$timing"

# How many annotations each stage placed, which is the only record of the gate
# having spoken at all. `contract` is what it was read against, so a result
# somebody downloads says which version of the contract produced these counts
# rather than leaving it to be inferred from the date.
annotations="${results}/${slug}.annotations.json"
jq -n \
	--arg root "$root" \
	--argjson contract "$OUTPUT_CONTRACT" \
	--argjson unreadable "$contract_error" \
	--argjson fmt "$fmt_annotations" \
	--argjson check "$check_annotations" \
	--argjson review "$review_annotations" \
	'{root: $root, contract: $contract, unreadable: ($unreadable == 1),
	  annotations: {fmt: $fmt, check: $check, review: $review}}' \
	>"$annotations"

printf 'gate %s: fmt %sms (exit %s, %s annotations), check %sms (exit %s, %s annotations), review %sms (exit %s, %s annotations)\n' \
	"$root" "$fmt_ms" "$fmt_exit" "$fmt_annotations" \
	"$check_ms" "$check_exit" "$check_annotations" \
	"$review_ms" "$review_exit" "$review_annotations" >&2

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	printf '| `%s` | %s | %s | %s | %s | %s | %s |\n' \
		"$root" "$fmt_ms" "$fmt_exit" "$check_ms" "$check_exit" "$review_ms" "$review_exit" \
		>>"$GITHUB_STEP_SUMMARY"

	# The review's own summary goes after the table row rather than where it was
	# written, because appending it between the header and the rows would break
	# the table it lands in the middle of. It is written to a file first for
	# exactly that reason, and the file is uploaded with the rest of the
	# results.
	if [ -s "$review_md" ]; then
		cat "$review_md" >>"$GITHUB_STEP_SUMMARY"
	fi
fi

if [ "$contract_error" -ne 0 ] ||
	[ "$fmt_exit" -ne 0 ] || [ "$check_exit" -ne 0 ] || [ "$review_exit" -ne 0 ]; then
	exit 1
fi
