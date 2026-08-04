#!/usr/bin/env bash
# lint-ceiling.sh — the whole-tree lint ceiling that `make full` fails on.
#
# THE PROBLEM THIS SOLVES. A bare `golangci-lint run` over this tree reports
# more than a thousand findings, so it exits non-zero on every commit and can
# never be a merge verdict. The old `make check` said so out loud and told
# readers never to gate on it. The result was that the whole-tree linter
# watched nothing: a commit could add findings all day and no build went red.
#
# A ceiling turns that count into a verdict. The findings that are already here
# are grandfathered. A commit that ADDS one goes red. A commit that removes one
# lowers the ceiling, so the ground it won can never be given back.
#
# WHY A COMMITTED FILE AND NOT A DERIVED NUMBER.
#
# The alternative is to derive the ceiling by linting the parent commit. That
# was rejected on two counts. It doubles the most expensive step in the merge
# decision, because a full-tree lint has to run twice. And it needs the parent
# tree to be present and buildable, which is exactly the condition that already
# defeats `--new-from-rev=origin/main` in the release runner — the Makefile
# records that failure in the `release-validate` comment, where a detached tag
# checkout has no origin/main and the linter silently falls back to linting
# everything. A number in a file needs no history and no network.
#
# It also matches what this repo already does for the same shape of problem:
# coverage.baseline, scripts/codex-coverage-floor.baseline and
# scripts/keeper-coverage-floor.baseline are all committed ratchet files.
#
# WHY THE FIXER DOES NOT HAVE TO REMEMBER ANYTHING. When the count comes in
# UNDER the ceiling this script rewrites the baseline to the lower number and
# says so. The fixer commits the file with the fix, the same way they commit any
# other file their change touched. There is no separate "now bump the ceiling"
# step to forget, and forgetting to commit it costs only a missed ratchet, never
# a false pass.
#
# THE MERGE CASE, said out loud. Two branches that each lower the ceiling change
# the same line of the same file, so git raises a conflict and a human picks a
# number. Whatever they pick, the next `make full` on the merged tree measures
# the real count and ratchets down again. The ceiling can lag the truth after a
# merge. It cannot rise.
#
# WHY IT SHELLS OUT TO `make lint-full-count` RATHER THAN COUNTING ITSELF. That
# target already runs golangci-lint over the whole tree under .golangci.yml,
# already refuses to publish a count when typecheck failed, and already has a
# test (scripts/lint-full-count-concurrency-test.sh) covering the two ways it
# can lie. Counting again here would be a second implementation to keep in step
# with the first. This file owns the VERDICT and owns nothing else.
#
# THE SILENT NO-OP THIS REPO HAS BEEN BURNED BY. Reading a number out of another
# command's stdout is exactly the shape that fails quietly: a linter that dies on
# an unknown flag prints nothing, and "nothing" run through a lenient parser
# reads as zero findings, which is under any ceiling, which is a pass. So the
# parse below is anchored and total. A missing count is a HARD FAILURE and never
# a zero. scripts/lint-ceiling-test.sh drives that case directly.
#
# EXIT CODES
#   0 — at or under the ceiling. The baseline may have been lowered.
#   1 — over the ceiling. This is the verdict; the build must stop.
#   2 — could not reach a verdict: no baseline, an unreadable baseline, a
#       linter that failed, or a count that could not be parsed. Fails closed.

set -uo pipefail

repo_root=$(git rev-parse --show-toplevel) || {
    echo "lint-ceiling: not inside a git worktree" >&2
    exit 2
}
cd "$repo_root" || exit 2

# Overridable so the test can point at a scratch baseline. Everything else about
# the run is identical to the real one, including the make target it calls.
baseline="${LINT_CEILING_BASELINE:-scripts/lint-ceiling.baseline}"

die() {
    printf 'lint-ceiling: %s\n' "$*" >&2
    exit 2
}

# ---------------------------------------------------------------------------
# Read the ceiling.
#
# The file is comments plus exactly one line that is a bare integer. Anything
# else is a failure rather than a default, because every default here is a
# number that lets a commit through.
# ---------------------------------------------------------------------------
[ -f "$baseline" ] || die "no ceiling file at $baseline.
  The whole-tree lint ceiling cannot be checked without one, and a merge
  decision that skips a step has not produced a verdict.
  To create it: run 'make lint-full-count' and write the number into the file."

ceiling=$(grep -vE '^[[:space:]]*(#|$)' "$baseline" | head -n 1 | tr -d '[:space:]')
case "$ceiling" in
    '') die "$baseline holds no number. Expected one line that is a bare integer." ;;
    *[!0-9]*) die "$baseline holds '$ceiling', which is not a whole number." ;;
esac

# ---------------------------------------------------------------------------
# Measure.
#
# TOOLS_DIR and TOOLS_HOME are forwarded as make command-line overrides because
# the Makefile assigns TOOLS_DIR with `:=`, which beats the environment. Passing
# them on the command line is the only spelling make honours.
# ---------------------------------------------------------------------------
make_args=()
[ -n "${TOOLS_DIR:-}" ] && make_args+=("TOOLS_DIR=$TOOLS_DIR")
[ -n "${TOOLS_HOME:-}" ] && make_args+=("TOOLS_HOME=$TOOLS_HOME")

echo "lint-ceiling: linting the whole tree (ceiling $ceiling)"
count_output=$(make -s lint-full-count "${make_args[@]+"${make_args[@]}"}" 2>&1)
count_status=$?

if [ "$count_status" -ne 0 ]; then
    printf '%s\n' "$count_output" >&2
    die "the whole-tree lint failed (exit $count_status), so there is no count to judge."
fi

# Anchored, and the whole line. A lenient match here is the bug described above.
count=$(printf '%s\n' "$count_output" | sed -n 's/^full lint findings: \([0-9][0-9]*\)$/\1/p' | head -n 1)
if [ -z "$count" ]; then
    printf '%s\n' "$count_output" >&2
    die "the whole-tree lint printed no finding count.
  Expected a line reading 'full lint findings: <N>'.
  A lint step that prints nothing is NOT a clean tree, so this is a failure
  rather than a count of zero."
fi

# ---------------------------------------------------------------------------
# Judge.
# ---------------------------------------------------------------------------
if [ "$count" -gt "$ceiling" ]; then
    added=$((count - ceiling))
    cat >&2 <<EOF
lint-ceiling: FAIL — $count findings, ceiling $ceiling, $added added.

  This change adds lint findings to a tree that is already carrying $ceiling of
  them. The grandfathered ones are allowed. New ones are not.

  To see the ones you added:
      .tools/golangci-lint run --new-from-rev=HEAD~1
  That is the same check 'make fast' runs, and it reports only your lines.

  Raising $baseline to make this pass is the one repair that is not allowed.
  The ceiling only ever goes down.
EOF
    exit 1
fi

if [ "$count" -lt "$ceiling" ]; then
    # Ratchet. Rewrite the number line and leave every other line alone, so the
    # file keeps whatever a human wrote above it.
    tmp="$baseline.tmp.$$"
    awk -v n="$count" '
        /^[[:space:]]*(#|$)/ { print; next }
        !done { print n; done = 1; next }
        { print }
    ' "$baseline" > "$tmp" || { rm -f "$tmp"; die "could not rewrite $baseline"; }
    mv "$tmp" "$baseline" || die "could not replace $baseline"
    removed=$((ceiling - count))
    cat <<EOF
lint-ceiling: PASS — $count findings, down $removed from a ceiling of $ceiling.

  $baseline now reads $count. COMMIT IT with your fix.
  Leaving it uncommitted costs nothing but the ratchet: the ceiling stays where
  it was and the next run lowers it again.
EOF
    exit 0
fi

echo "lint-ceiling: PASS — $count findings, exactly at the ceiling."
exit 0
