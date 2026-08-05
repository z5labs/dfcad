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
# `go test -race` — belongs to GoApp.Ci in the z5labs daggerverse module and is
# not restated here or anywhere else in this repository. See README.md beside
# this file for what a consuming data repository has to change to adopt it.
#
# Usage:
#
#	gate.sh --binary <dfcad> --root <model root> [--results <dir>] \
#	        [--against <ref>] [--policy <check>=<ruling>]...
#
# Run it from the directory paths should be reported relative to, which in CI is
# the repository root: the paths dfcad writes are the ones it walked, and
# GitHub resolves an annotation's file against the repository root.

set -euo pipefail

binary=""
root=""
results=""
against=""
policies=()

usage() {
	cat >&2 <<'EOF'
usage: gate.sh --binary <dfcad> --root <model root> [--results <dir>] \
               [--against <ref>] [--policy <check>=<ruling>]...

	--binary   the dfcad executable to run. In CI this is the binary the
	           standard pipeline built, so the gate and the shipped artifact
	           cannot diverge.
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
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--binary)
		binary="${2:-}"
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

if [ -z "$binary" ] || [ -z "$root" ]; then
	echo "gate.sh: --binary and --root are both required" >&2
	usage
	exit 64
fi

if [ ! -x "$binary" ]; then
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
	"$binary" "$@" >"$out"
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
jq -r '
	.files[]
	| select(.status == "unformatted" or .status == "failed")
	| if (.diagnostics // []) | length > 0 then
		.path as $path
		| .diagnostics[]
		| [$path, (.span.start.line | tostring), (.span.start.column | tostring), .message]
	  else
		[.path, "", "", (.error // "not in canonical form: run dfcad fmt to rewrite it")]
	  end
	| join($fs)
' --arg fs "$FS" "$fmt_json" | while IFS="$FS" read -r path line col message; do
	annotate error "$path" "$line" "$col" "$message"
done

# The exact hunks, so that a formatting failure is fixed from the log rather
# than from a local reproduction. --diff writes nothing and implies --check, so
# this cannot change what the gate just decided.
if [ "$fmt_exit" -ne 0 ]; then
	"$binary" fmt --diff --root "$root" >/dev/null || true
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
jq -r '
	.violations[]
	| [
		.subject.start.path,
		(.subject.start.line | tostring),
		(.subject.start.column | tostring),
		(.check + " failed on " + .instance + ": " + .message
		 + " (declared at " + .declared.start.path
		 + ":" + (.declared.start.line | tostring)
		 + ":" + (.declared.start.column | tostring) + ")")
	  ]
	| join($fs)
' --arg fs "$FS" "$check_json" | while IFS="$FS" read -r path line col message; do
	annotate error "$path" "$line" "$col" "$message"
done

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
review_json="${results}/${slug}.review.json"
review_md="${results}/${slug}.review.md"
review_exit=0
review_ms=0

if [ -n "$against" ]; then
	echo "::group::dfcad review --root ${root} --against ${against}"
	run_stage "$review_json" review --root "$root" --against "$against" \
		--annotate "$review_md" "${policies[@]+"${policies[@]}"}"
	review_exit=$stage_exit
	review_ms=$stage_ms

	# A finding carries the span of the change and the ruling the policy gave
	# it, so the annotation lands on the line and says how much it matters. A
	# finding the policy acknowledged is in the result and nowhere else, which
	# is what "ignored" means, so it is filtered out here as it is on stderr.
	#
	# The span of a finding whose side is "base" points into the merge base,
	# which is a file this checkout may no longer hold — GitHub then drops the
	# annotation, and the message still reads in the log. Saying which revision
	# the line is in is what keeps that from looking like a wrong line number.
	jq -r '
		.findings[]
		| select(.ruling != "ignored")
		| [
			.span.start.path,
			(.span.start.line | tostring),
			(.span.start.column | tostring),
			(if .ruling == "failure" then "error" else "warning" end),
			(.kind + " on " + .subject + " (in the " + .side + " revision): " + .message
			 + (if .commit then " [" + (.commit.sha[0:12]) + " " + .commit.summary + "]" else "" end))
		  ]
		| join($fs)
	' --arg fs "$FS" "$review_json" | while IFS="$FS" read -r path line col level message; do
		annotate "$level" "$path" "$line" "$col" "$message"
	done

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

printf 'gate %s: fmt %sms (exit %s), check %sms (exit %s), review %sms (exit %s)\n' \
	"$root" "$fmt_ms" "$fmt_exit" "$check_ms" "$check_exit" "$review_ms" "$review_exit" >&2

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

if [ "$fmt_exit" -ne 0 ] || [ "$check_exit" -ne 0 ] || [ "$review_exit" -ne 0 ]; then
	exit 1
fi
