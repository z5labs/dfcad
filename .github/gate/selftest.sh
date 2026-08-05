#!/usr/bin/env bash
#
# Copyright (c) 2026 Z5Labs and Contributors
#
# This software is released under the MIT License.
# https://opensource.org/licenses/MIT
#
# selftest.sh runs gate.sh against the deliberately broken model in broken/ and
# requires it to block — non-zero, with both stages having answered, and with an
# annotation naming the file and the line.
#
# A gate which has never been seen to fail is not known to be a gate. Running
# the failure on every build is what keeps that true: a change which makes the
# gate silently pass everything — a swallowed exit code, a jq filter which
# stopped matching, a binary which is not there — fails here, in the run that
# made it, rather than the first time somebody relies on the gate.
#
# Usage:
#
#	selftest.sh --binary <dfcad>
#
# Run it from the repository root, which is where gate.sh reports paths from.

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

root="${here#"$PWD"/}/broken"
results="$(mktemp -d)"
log="${results}/gate.log"

echo "::group::self-test: the gate blocks ${root}"

# The gate's own annotations would be read by Actions as annotations on this
# run, and this run is the one where they are expected. They are captured
# rather than emitted so that a deliberate failure does not decorate a green
# pull request with three errors somebody then goes looking for.
#
# GITHUB_STEP_SUMMARY goes with them. The gate appends a row to the run's
# runtime table, and this gate's row reports the failure it was asked to
# produce: left in, a green run's table would carry a line reading exit 1 and
# exit 2, which is the one thing somebody skimming the summary would stop at.
set +e
env -u GITHUB_STEP_SUMMARY \
	"${here}/gate.sh" --binary "$binary" --root "$root" --results "$results" \
	>"$log" 2>&1
gate_exit=$?
set -e

# Prefixed, which is what makes them inert. Without it the gate's own
# `::error::` lines would decorate this run with three failures which are the
# point of the run, and its `::group::` lines would nest inside this one, which
# Actions does not support.
#
# The prefix is a visible marker rather than indentation: Actions strips leading
# whitespace before it looks for a workflow command, so two spaces in front of
# `::error` neutralise nothing — measured on run 30981085140, where an indented
# line still produced an annotation. Anything non-blank ahead of the colons does
# work, and saying "captured" says why the line is there.
#
# Only what is echoed is rewritten. The assertions below read $log, which still
# holds exactly what the gate wrote.
sed 's/^::/[captured] ::/' "$log"
echo "::endgroup::"

fail() {
	echo "::error::the gate did not block the deliberately broken model: $1"
	exit 1
}

if [ "$gate_exit" -eq 0 ]; then
	fail "gate.sh exited 0"
fi

# What each stage decided, read from the results it wrote rather than from its
# log: the log is for a person and the JSON is the contract, and a self-test
# which grepped prose would pass on a run whose prose merely still looked right.
fmt_json="${results}/github-gate-broken.fmt.json"
check_json="${results}/github-gate-broken.check.json"
timing_json="${results}/github-gate-broken.timing.json"

for file in "$fmt_json" "$check_json" "$timing_json"; do
	[ -s "$file" ] || fail "$(basename "$file") was not written"
done

unformatted="$(jq -r '[.files[] | select(.status == "unformatted") | .path] | join(",")' "$fmt_json")"
if [ "$unformatted" != "${root}/unformatted.dfc" ]; then
	fail "fmt --check flagged [${unformatted}], want only ${root}/unformatted.dfc"
fi

# check is expected to refuse the model outright, because the node naming an
# undeclared type is a load failure rather than a rule which did not hold. The
# summary is what says the run got that far and reported nothing it did not
# run.
ran="$(jq -r '.summary.ran' "$check_json")"
if [ "$ran" != "0" ]; then
	fail "check reported ${ran} rules run against a model which does not load"
fi

# The annotation naming the file and the line is the whole of criterion five:
# somebody reading the log has to be able to fix the model without reproducing
# the failure locally.
if ! grep -qF "::error file=${root}/unformatted.dfc::" "$log"; then
	fail "no annotation named ${root}/unformatted.dfc"
fi
if ! grep -qE "^${root}/entities/room\.dfc:[0-9]+:[0-9]+: error: " "$log"; then
	fail "no diagnostic named ${root}/entities/room.dfc with a line and a column"
fi

echo "the gate blocked the deliberately broken model, as it must (exit ${gate_exit})"
