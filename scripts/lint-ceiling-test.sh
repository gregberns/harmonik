#!/usr/bin/env bash
# lint-ceiling-test.sh — proves the whole-tree lint ceiling is a verdict and not
# a decoration.
#
# The ceiling is one integer compared against one measured number. That is a
# small enough thing that it looks self-evidently correct, and small enough that
# every way it can be wrong is silent:
#
#   - the comparison points the wrong way, so adding findings passes;
#   - the count never arrives, and an empty string reads as zero, which is under
#     any ceiling, which is a pass;
#   - the linter dies and the failure is treated as a clean tree;
#   - the ratchet never writes, so ground that was won is given back;
#   - the step is not actually in `make full`, so none of the above matters.
#
# Each of those has a case below. The cases run against a FAKE golangci-lint and
# a scratch baseline file, so they cost about a second and never touch the real
# ceiling.
#
# What this file does NOT prove is that golangci-lint finds real findings in
# real Go code. That is the linter's job, and a stub cannot speak for it. The
# case that covers it is the acceptance run recorded in the commit message: a
# deliberate violation added to the tree, `make full` observed going red, the
# violation removed.

set -uo pipefail

repo_root=$(git rev-parse --show-toplevel) || {
    echo "lint-ceiling-test: not inside a git worktree" >&2
    exit 1
}
cd "$repo_root" || exit 1

work=$(mktemp -d "${TMPDIR:-/tmp}/lint-ceiling-test-XXXXXX")
trap 'rm -rf "$work"' EXIT INT TERM

failures=0
assertions=0

fail() {
    printf 'lint-ceiling-test: FAIL: %s\n' "$*" >&2
    failures=$((failures + 1))
}

pass() {
    printf 'lint-ceiling-test: ok: %s\n' "$*"
}

# ---------------------------------------------------------------------------
# A fake golangci-lint that reports exactly FAKE_FINDINGS findings.
#
# It writes the same JSON shape the real one writes under --output.json.path,
# which is what `make lint-full-count` counts. FAKE_EXIT makes it fail AFTER
# writing a valid report, which is the case where a readable report could mask
# a dead linter.
# ---------------------------------------------------------------------------
mkdir -p "$work/tools"
cat >"$work/tools/golangci-lint" <<'STUB'
#!/usr/bin/env bash
set -uo pipefail
report=
for arg in "$@"; do
    case "$arg" in
        --output.json.path=*) report=${arg#*=} ;;
    esac
done
if [ -z "$report" ]; then
    echo "fake golangci-lint: no --output.json.path given" >&2
    exit 2
fi
n=${FAKE_FINDINGS:-0}
linter=${FAKE_LINTER_NAME:-revive}
{
    printf '{"Issues":['
    i=0
    while [ "$i" -lt "$n" ]; do
        [ "$i" -gt 0 ] && printf ','
        printf '{"FromLinter":"%s","Text":"fake finding %d"}' "$linter" "$i"
        i=$((i + 1))
    done
    printf ']}\n'
} >"$report"
exit "${FAKE_EXIT:-0}"
STUB
chmod +x "$work/tools/golangci-lint"

# write_baseline <path> <number-or-empty>
write_baseline() {
    local path="$1" n="$2"
    {
        echo "# scratch ceiling for lint-ceiling-test.sh"
        echo ""
        [ -n "$n" ] && echo "$n"
    } >"$path"
}

# run_ceiling <baseline-path> <fake-findings> [extra env assignments...]
#
# Sets RUN_STATUS and RUN_OUT. Runs the REAL script against the fake linter.
run_ceiling() {
    local baseline="$1" findings="$2"
    shift 2
    RUN_OUT="$work/out.$$"
    env FAKE_FINDINGS="$findings" \
        TOOLS_DIR="$work/tools" \
        LINT_CEILING_BASELINE="$baseline" \
        HARMONIK_GATE_SELFTEST=1 \
        "$@" \
        scripts/lint-ceiling.sh >"$RUN_OUT" 2>&1
    RUN_STATUS=$?
}

# ---------------------------------------------------------------------------
# THE VERDICT — the ceiling must block a rise and allow a hold.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
base="$work/over.baseline"
write_baseline "$base" 5
run_ceiling "$base" 7
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "7 findings against a ceiling of 5 exited 0; a new finding does not block a merge"
    cat "$RUN_OUT" >&2
elif [ "$(grep -vE '^[[:space:]]*(#|$)' "$base" | tr -d '[:space:]')" != "5" ]; then
    fail "going over the ceiling rewrote the baseline; the ceiling must never rise"
else
    pass "7 findings against a ceiling of 5 blocked (exit $RUN_STATUS) and left the ceiling at 5"
fi

assertions=$((assertions + 1))
base="$work/equal.baseline"
write_baseline "$base" 5
run_ceiling "$base" 5
if [ "$RUN_STATUS" -ne 0 ]; then
    fail "5 findings against a ceiling of 5 exited $RUN_STATUS; standing still must pass"
    cat "$RUN_OUT" >&2
else
    pass "5 findings against a ceiling of 5 passed"
fi

# ---------------------------------------------------------------------------
# THE RATCHET — fixing a finding lowers the ceiling with no separate step.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
base="$work/under.baseline"
write_baseline "$base" 5
run_ceiling "$base" 3
new=$(grep -vE '^[[:space:]]*(#|$)' "$base" | tr -d '[:space:]')
if [ "$RUN_STATUS" -ne 0 ]; then
    fail "3 findings against a ceiling of 5 exited $RUN_STATUS; removing findings must pass"
    cat "$RUN_OUT" >&2
elif [ "$new" != "3" ]; then
    fail "the ceiling did not ratchet: baseline reads '$new', expected 3"
elif ! grep -q '^# scratch ceiling' "$base"; then
    fail "the ratchet ate the comment header; it must rewrite only the number"
else
    pass "3 findings against a ceiling of 5 passed and lowered the ceiling to 3 in place"
fi

# The ratchet must be idempotent: a second run at the same count is a no-op pass.
assertions=$((assertions + 1))
run_ceiling "$base" 3
if [ "$RUN_STATUS" -ne 0 ]; then
    fail "re-running at the ratcheted ceiling exited $RUN_STATUS"
    cat "$RUN_OUT" >&2
else
    pass "re-running at the ratcheted ceiling of 3 passed"
fi

# ...and the lowered ceiling now blocks what the old one allowed.
assertions=$((assertions + 1))
run_ceiling "$base" 4
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "4 findings passed after the ceiling ratcheted to 3; ground won was given back"
    cat "$RUN_OUT" >&2
else
    pass "the ratcheted ceiling of 3 blocks 4 findings, which the old ceiling of 5 allowed"
fi

# ---------------------------------------------------------------------------
# FAIL CLOSED — every way the number can fail to arrive is a failure, never a
# zero and never a skip.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
run_ceiling "$work/does-not-exist.baseline" 0
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "a missing ceiling file exited 0; a skipped step is not a verdict"
    cat "$RUN_OUT" >&2
else
    pass "a missing ceiling file blocked (exit $RUN_STATUS)"
fi

assertions=$((assertions + 1))
base="$work/empty.baseline"
write_baseline "$base" ""
run_ceiling "$base" 0
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "a ceiling file with no number exited 0"
    cat "$RUN_OUT" >&2
else
    pass "a ceiling file with no number blocked (exit $RUN_STATUS)"
fi

assertions=$((assertions + 1))
base="$work/garbage.baseline"
write_baseline "$base" "lots"
run_ceiling "$base" 0
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "a ceiling file reading 'lots' exited 0"
    cat "$RUN_OUT" >&2
else
    pass "a non-numeric ceiling blocked (exit $RUN_STATUS)"
fi

# A linter that writes a valid report and THEN dies. The report is readable, so
# a check that only looks at the report calls this a clean tree.
assertions=$((assertions + 1))
base="$work/linterdied.baseline"
write_baseline "$base" 500
run_ceiling "$base" 1 env FAKE_EXIT=7
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "a linter that exited 7 was judged as 1 finding under a ceiling of 500"
    cat "$RUN_OUT" >&2
else
    pass "a linter that exited 7 blocked (exit $RUN_STATUS) even with a readable report"
fi

# Broken compilation. golangci-lint reports it as a typecheck finding, and a
# count taken from a tree that does not compile means nothing.
assertions=$((assertions + 1))
base="$work/typecheck.baseline"
write_baseline "$base" 500
run_ceiling "$base" 1 env FAKE_LINTER_NAME=typecheck
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "a typecheck failure was judged as 1 finding under a ceiling of 500"
    cat "$RUN_OUT" >&2
else
    pass "a typecheck failure blocked (exit $RUN_STATUS)"
fi

# ---------------------------------------------------------------------------
# THE SILENT NO-OP.
#
# This is the specific shape this repo has been burned by: a lint command dies
# on an unknown flag, prints nothing, and the nothing is parsed as a clean
# result. Drive it directly by putting a `make` on PATH that succeeds and says
# nothing, and by one that prints a line that ALMOST matches.
# ---------------------------------------------------------------------------
mkdir -p "$work/quietmake"
cat >"$work/quietmake/make" <<'STUB'
#!/usr/bin/env bash
printf '%s' "${FAKE_MAKE_SAYS:-}"
exit "${FAKE_MAKE_EXIT:-0}"
STUB
chmod +x "$work/quietmake/make"

# run_with_quiet_make <baseline> <what make prints> [what make exits with]
run_with_quiet_make() {
    local baseline="$1" says="$2" rc="${3:-0}"
    RUN_OUT="$work/quiet.out"
    env PATH="$work/quietmake:$PATH" \
        FAKE_MAKE_SAYS="$says" \
        FAKE_MAKE_EXIT="$rc" \
        LINT_CEILING_BASELINE="$baseline" \
        HARMONIK_GATE_SELFTEST=1 \
        scripts/lint-ceiling.sh >"$RUN_OUT" 2>&1
    RUN_STATUS=$?
}

base="$work/quiet.baseline"
write_baseline "$base" 500

assertions=$((assertions + 1))
run_with_quiet_make "$base" ""
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "a lint step that printed NOTHING and exited 0 was read as a clean tree"
    cat "$RUN_OUT" >&2
else
    pass "a lint step that printed nothing blocked (exit $RUN_STATUS) instead of counting zero"
fi

assertions=$((assertions + 1))
run_with_quiet_make "$base" "full lint findings: none
"
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "'full lint findings: none' was accepted as a count"
    cat "$RUN_OUT" >&2
else
    pass "a count line that is not a number blocked (exit $RUN_STATUS)"
fi

# Positive control for the three cases above: the same stubbed `make`, printing
# a WELL-FORMED count, must pass. Without this, every case above is satisfied
# for free by a script that rejects everything.
assertions=$((assertions + 1))
run_with_quiet_make "$base" "full lint findings: 500
"
if [ "$RUN_STATUS" -ne 0 ]; then
    fail "a well-formed count of 500 against a ceiling of 500 exited $RUN_STATUS; the cases above prove nothing"
    cat "$RUN_OUT" >&2
else
    pass "a well-formed count of 500 against a ceiling of 500 passed — the rejections above are selective"
fi

# A lint step that FAILS and still prints a well-formed number under the ceiling.
#
# This case exists because a mutation proved it had to. Deleting the exit-status
# check from lint-ceiling.sh SURVIVED every other case in this file: when
# `make lint-full-count` fails it also prints no count, so the missing-count
# guard caught it and the status check looked redundant. It is not redundant. It
# is the only guard that acts when a failing step still emits a number, and a
# guard nothing exercises is a guard that can be deleted without the build
# noticing.
assertions=$((assertions + 1))
run_with_quiet_make "$base" "full lint findings: 1
" 7
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "a lint step that exited 7 was judged on the number it printed anyway"
    cat "$RUN_OUT" >&2
elif [ "$(grep -vE '^[[:space:]]*(#|$)' "$base" | tr -d '[:space:]')" != "500" ]; then
    fail "a failing lint step ratcheted the ceiling on a count it had no right to publish"
else
    pass "a lint step that exited 7 blocked (exit $RUN_STATUS) despite printing a valid count of 1"
fi

# ---------------------------------------------------------------------------
# WIRING — none of the above matters if the step is not in the merge decision.
#
# `make -n` expands the real step list and recurses into the sub-makes, so this
# reads what would actually run rather than the Makefile text.
# ---------------------------------------------------------------------------
full_steps=$(HARMONIK_GATE_SELFTEST=1 make -n full 2>/dev/null)
fast_steps=$(HARMONIK_GATE_SELFTEST=1 make -n fast 2>/dev/null)

assertions=$((assertions + 1))
if [ -z "$full_steps" ]; then
    fail "wiring: 'make -n full' produced nothing, so nothing was checked"
elif ! printf '%s\n' "$full_steps" | grep -vE '^[[:space:]]*#' | grep -q 'scripts/lint-ceiling\.sh'; then
    fail "wiring: 'make full' does not run scripts/lint-ceiling.sh; the ceiling is not in the merge decision"
else
    pass "wiring: 'make full' runs scripts/lint-ceiling.sh"
fi

assertions=$((assertions + 1))
if [ -z "$fast_steps" ]; then
    fail "wiring: 'make -n fast' produced nothing, so nothing was checked"
elif ! printf '%s\n' "$fast_steps" | grep -q -- '--new-from-rev='; then
    fail "wiring: 'make fast' does not lint changed lines"
else
    pass "wiring: 'make fast' lints changed lines with --new-from-rev"
fi

# The whole-tree lint belongs to the merge decision only. If it creeps into the
# inner loop, `make fast` stops being fast and people stop running it.
assertions=$((assertions + 1))
if printf '%s\n' "$fast_steps" | grep -vE '^[[:space:]]*#' | grep -q 'scripts/lint-ceiling\.sh'; then
    fail "wiring: 'make fast' runs the whole-tree ceiling; that belongs to 'make full' alone"
else
    pass "wiring: 'make fast' does not run the whole-tree ceiling"
fi

# The changed-line lint must not be piped anywhere. A linter that dies on an
# unknown flag exits non-zero and make stops — but only while its status is the
# status of the step. Put it on the left of a pipe and the status becomes the
# status of whatever is on the right, and an unknown flag reads as a clean run.
assertions=$((assertions + 1))
lint_lines=$(printf '%s\n' "$fast_steps" | grep -vE '^[[:space:]]*#' | grep -- '--new-from-rev=')
if printf '%s\n' "$lint_lines" | grep -qE '\||--issues-exit-code=0'; then
    fail "wiring: the changed-line lint pipes its output or suppresses its exit code:"
    printf '%s\n' "$lint_lines" >&2
else
    pass "wiring: the changed-line lint reports its own exit status directly"
fi

# ---------------------------------------------------------------------------
# THE REAL CEILING FILE — it ships, and it parses.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
real="scripts/lint-ceiling.baseline"
if [ ! -f "$real" ]; then
    fail "$real is missing; 'make full' will fail closed until it is committed"
else
    real_value=$(grep -vE '^[[:space:]]*(#|$)' "$real" | head -n 1 | tr -d '[:space:]')
    case "$real_value" in
        '' | *[!0-9]*) fail "$real holds '$real_value', which is not a whole number" ;;
        *) pass "$real holds a whole number ($real_value)" ;;
    esac
fi

# ---------------------------------------------------------------------------
printf 'lint-ceiling-test: %d assertions, %d failed\n' "$assertions" "$failures"
if [ "$assertions" -lt 19 ]; then
    printf 'lint-ceiling-test: only %d assertions ran; this file expects 19\n' "$assertions" >&2
    exit 1
fi
[ "$failures" -eq 0 ]
