#!/usr/bin/env bash
# lint-allow.sh — the whole-tree lint verdict that `make full` fails on.
#
# THE PROBLEM THIS SOLVES. A bare `golangci-lint run` over this tree reports
# more than a thousand findings, so it exits non-zero on every commit and can
# never be a merge verdict. The old `make check` said so out loud and told
# readers never to gate on it. The result was that the whole-tree linter watched
# nothing: a commit could add findings all day and no build went red.
#
# WHAT REPLACED THE COUNT. This step used to compare one number against a
# committed ceiling. That is a weak verdict on three counts. A count falls just
# as readily when somebody silences a finding as when somebody fixes one. It
# cannot tell "fixed two in the daemon, added two in the queue" from "nothing
# changed". And it never says WHERE the debt sits, so it gives a reader no way
# to aim.
#
# The allow list names each tolerated finding by a hash of its linter, message,
# and enclosing syntax. A location comment keeps the list useful to readers but
# is not part of the identity. Moving unchanged code does not change the hash.
# tools/lintreport owns the judging and
# the reporting; this file owns reaching a verdict safely and owns nothing else.
#
# WHY FILE-AND-LINTER AND NOT FILE-AND-LINE. Line numbers rot within days. A
# list keyed by line would churn on every unrelated edit, and a list nobody can
# keep current is a list people delete.
#
# THE SILENT NO-OP THIS REPO HAS BEEN BURNED BY. Reading a verdict out of
# another command's output is exactly the shape that fails quietly: a linter
# that dies on an unknown flag prints nothing, and "nothing" run through a
# lenient reader looks like a clean tree, which is a pass. So every way of NOT
# reaching an answer is a hard failure here and never a zero.
#
# WHY --issues-exit-code=0 IS SAFE HERE AND NOWHERE ELSE. golangci-lint's own
# exit code cannot be the verdict, because it goes non-zero on grandfathered
# findings. It is turned off so that the linter reports findings as DATA and
# lintreport judges them. That is why the run below is checked for a real
# failure separately, and why a missing or unreadable report is fatal.
# scripts/gate-fails-closed-test.sh bans that flag from any step of `make fast`
# or `make full` for good reason; it lives behind this script boundary, which is
# the same place the old ceiling kept it.
#
# EXIT CODES
#   0 — every finding is on the allow list.
#   1 — a finding is not on the allow list. This is the verdict; the build stops.
#   2 — could not reach a verdict: no allow list, a linter that failed, an
#       unreadable report, or a tree that does not compile. Fails closed.

set -uo pipefail

repo_root=$(git rev-parse --show-toplevel) || {
    echo "lint-allow: not inside a git worktree" >&2
    exit 2
}
cd "$repo_root" || exit 2

# Overridable so the test can point at a scratch list. Everything else about the
# run is identical to the real one.
allow="${LINT_ALLOW_LIST:-tools/lintreport/allow.txt}"

die() {
    printf 'lint-allow: %s\n' "$*" >&2
    exit 2
}

[ -f "$allow" ] || die "no allow list at $allow.
  The whole-tree lint cannot be judged without one, and a merge decision that
  skips a step has not produced a verdict.
  To create it: run the linter to JSON, then
      go run ./tools/lintreport -allow $allow -write <report.json>
  Seed it ONCE, on a settled tree, and commit the result."

# TOOLS_DIR is where the Makefile puts golangci-lint. Honour an inherited value
# first, so a test or an unusual checkout can point elsewhere.
#
# THE DEFAULT MUST NOT BE "$repo_root/.tools". A linked worktree has its own
# top level and no .tools of its own — the toolchain lives once, beside the main
# checkout — so that spelling makes this script unusable from every worktree,
# which is where most work in this repo happens. The Makefile already solves it
# by asking git for the COMMON git dir, which is shared by every worktree, and
# stripping the trailing /.git. Same computation here, so both agree.
if [ -n "${TOOLS_DIR:-}" ]; then
    tools_dir="$TOOLS_DIR"
else
    tools_home=$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null | sed 's|/\.git/*$||')
    tools_dir="${tools_home:-$repo_root}/.tools"
fi
linter="$tools_dir/golangci-lint"
[ -x "$linter" ] || die "no golangci-lint at $linter.
  A missing linter is a failure, not a clean tree. Run 'make tools' to install it."

report=$(mktemp) || die "could not create a temporary file for the lint report"
trap 'rm -f "$report"' EXIT

# ---------------------------------------------------------------------------
# Measure.
#
# --issues-exit-code=0 so findings come back as data rather than as a verdict;
# see the header. The two --max flags are off because a truncated report would
# silently shrink the finding set, and a shrunken set is a pass.
#
# The timeout is set here rather than taken from .golangci.yml, whose 5m is
# right for the changed-line runs in `make fast` but is under a cold whole-tree
# run. Not disabled: a linter that hangs must fail rather than hang.
# ---------------------------------------------------------------------------
lint_status=0
scripts/with-lane-gocache.sh "$linter" run \
    --allow-parallel-runners \
    --issues-exit-code=0 \
    --max-issues-per-linter=0 \
    --max-same-issues=0 \
    --timeout="${LINT_FULL_TIMEOUT:-15m}" \
    --output.text.path=/dev/null \
    --output.json.path="$report" >/dev/null || lint_status=$?

if [ "$lint_status" -ne 0 ]; then
    die "golangci-lint failed (exit $lint_status), so there is no report to judge.
  This is the linter itself failing, not findings: findings are reported as data."
fi

[ -s "$report" ] || die "golangci-lint wrote an empty report.
  An empty report is NOT a clean tree. Something stopped the linter before it
  could write its findings."

# ---------------------------------------------------------------------------
# Refuse to judge a tree that does not compile.
#
# golangci-lint reports a compile error as a `typecheck` finding and then sees
# almost nothing else, because the packages downstream of the break never load.
# The finding set collapses, every surviving pair is on the allow list, and the
# run passes. That is a false green produced by a broken build, so it is caught
# here before lintreport ever sees the report. The old ceiling refused the same
# case for the same reason.
# ---------------------------------------------------------------------------
if ! command -v jq >/dev/null 2>&1; then
    die "jq is not on PATH, so the report cannot be checked for compile errors.
  Judging without that check risks passing a tree that does not build."
fi

typecheck_count=$(jq '[.Issues[]? | select(.FromLinter == "typecheck")] | length' "$report") \
    || die "could not read $report as JSON. An unreadable report is a failure, not a clean tree."

case "$typecheck_count" in
    '') die "could not count typecheck findings in the lint report." ;;
    *[!0-9]*) die "typecheck finding count came back as '$typecheck_count', which is not a number." ;;
esac

if [ "$typecheck_count" -gt 0 ]; then
    jq -r '.Issues[]? | select(.FromLinter == "typecheck")
           | "  \(.Pos.Filename):\(.Pos.Line): \(.Text)"' "$report" >&2
    die "the tree does not compile ($typecheck_count typecheck findings, shown above).
  No lint verdict is possible: the packages downstream of a compile error never
  load, so the finding set collapses and everything left looks allowed.
  Fix the build first."
fi

# ---------------------------------------------------------------------------
# Judge.
#
# The judge is BUILT first rather than run with `go run`, for two reasons.
# `go run` would compile against the default shared GOCACHE while the lint step
# above uses the per-checkout one, which is the cross-lane cache corruption
# scripts/with-lane-gocache.sh exists to prevent. And `go run` returns 1 both
# when the tool reports an unlisted finding and when the tool fails to COMPILE.
# Those must never share an exit code: one is a verdict on the tree and the
# other is a broken toolchain. Building separately keeps them apart.
#
# lintreport then exits 0 when every finding is allowed and 1 when one is not.
# Anything else is a tool failure and must not read as either verdict.
# ---------------------------------------------------------------------------
bindir=$(mktemp -d) || die "could not create a temporary directory for the judge"
trap 'rm -f "$report"; rm -rf "$bindir"' EXIT

scripts/with-lane-gocache.sh go build -o "$bindir/lintreport" ./tools/lintreport \
    || die "could not build tools/lintreport, so no verdict is possible.
  This is the judging tool failing to compile, not a finding in your change."

judge_status=0
"$bindir/lintreport" -allow "$allow" "$report" || judge_status=$?

case "$judge_status" in
    0) exit 0 ;;
    1)
        cat >&2 <<EOF

lint-allow: FAIL — findings above are in files that are not on the allow list.

  The grandfathered findings are allowed. New ones are not.

  To see only the findings your change added:
      $linter run --new-from-rev=HEAD~1
  That is the same check 'make fast' runs.

  Adding your file to $allow to make this pass is the one repair
  that is not allowed. The list only ever gets shorter.
EOF
        exit 1
        ;;
    *) die "lintreport exited $judge_status, which is neither a pass nor a fail.
  A verdict tool that cannot answer is a failure, never an approval." ;;
esac
