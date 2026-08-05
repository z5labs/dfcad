#!/usr/bin/env bash
#
# Copyright (c) 2026 Z5Labs and Contributors
#
# This software is released under the MIT License.
# https://opensource.org/licenses/MIT
#
# gate.sh runs `dfcad fmt --check` and `dfcad check` over one model root and
# says, by its exit code, whether that model may be merged.
#
# It is the repository-specific half of CI. The Go half — fmt, vet, lint and
# `go test -race` — belongs to GoApp.Ci in the z5labs daggerverse module and is
# not restated here or anywhere else in this repository. See README.md beside
# this file for what a consuming data repository has to change to adopt it.
#
# Usage:
#
#	gate.sh --binary <dfcad> --root <model root> [--results <dir>]
#
# Run it from the directory paths should be reported relative to, which in CI is
# the repository root: the paths dfcad writes are the ones it walked, and
# GitHub resolves an annotation's file against the repository root.

set -euo pipefail

binary=""
root=""
results=""

usage() {
	cat >&2 <<'EOF'
usage: gate.sh --binary <dfcad> --root <model root> [--results <dir>]

	--binary   the dfcad executable to run. In CI this is the binary the
	           standard pipeline built, so the gate and the shipped artifact
	           cannot diverge.
	--root     the model root to gate.
	--results  where to write the structured results. Defaults to a temporary
	           directory, which is what a local run wants; CI passes a path it
	           then uploads.
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
# name a hidden file, and results nobody's glob matches are results nobody
# uploads.
slug="$(printf '%s' "$root" | tr -c 'A-Za-z0-9._-' '-' | sed 's/^[.-]*//')"

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

total_ms=$((fmt_ms + check_ms))
timing="${results}/${slug}.timing.json"
jq -n \
	--arg root "$root" \
	--argjson fmt "$fmt_ms" \
	--argjson check "$check_ms" \
	--argjson total "$total_ms" \
	'{root: $root, milliseconds: {fmt: $fmt, check: $check, total: $total}}' \
	>"$timing"

printf 'gate %s: fmt %sms (exit %s), check %sms (exit %s)\n' \
	"$root" "$fmt_ms" "$fmt_exit" "$check_ms" "$check_exit" >&2

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	printf '| `%s` | %s | %s | %s | %s |\n' \
		"$root" "$fmt_ms" "$fmt_exit" "$check_ms" "$check_exit" \
		>>"$GITHUB_STEP_SUMMARY"
fi

if [ "$fmt_exit" -ne 0 ] || [ "$check_exit" -ne 0 ]; then
	exit 1
fi
