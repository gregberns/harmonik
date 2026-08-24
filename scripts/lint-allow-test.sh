#!/usr/bin/env bash
# lint-allow-test.sh — proves the whole-tree lint verdict is a verdict and not a
# decoration.
#
# The check is small: run a linter, compare each finding against a list. Small
# enough to look self-evidently correct, and small enough that every way it can
# be wrong is silent:
#
#   - a finding outside the list is tolerated, so the tree can regress freely;
#   - the report never arrives, and "no findings parsed" reads as a clean tree;
#   - the linter dies and the failure is treated as a pass;
#   - the tree does not compile, the finding set collapses to almost nothing,
#     and what survives is all grandfathered, so a broken build reports green;
#   - the allow list is missing and its absence reads as "nothing to check";
#   - the step is not actually in `make full`, so none of the above matters.
#
# Each of those has a case below. The cases run against a FAKE golangci-lint and
# a scratch allow list, so they cost about a second and never touch the real one.
#
# What this file does NOT prove is that golangci-lint finds real findings in
# real Go code. That is the linter's job and a stub cannot speak for it. The
# case that covers it is the acceptance run recorded in the commit message: a
# deliberate violation added to the tree, the gate observed going red, the
# violation removed.
#
# That acceptance run was repeated when the judge moved into `make fast`, and it
# is the evidence that the two lint steps see different things. Twenty branches
# were added INSIDE the body of `Config.window` in internal/sentinel/governor.go,
# leaving the declaration line untouched. The changed-line step printed
# "0 issues". This step named it at governor.go:141, at the declaration, which
# is outside the changed hunk. `make fast` then went red at that step.

set -uo pipefail

repo_root=$(git rev-parse --show-toplevel) || {
    echo "lint-allow-test: not inside a git worktree" >&2
    exit 1
}
cd "$repo_root" || exit 1

work=$(mktemp -d "${TMPDIR:-/tmp}/lint-allow-test-XXXXXX")
trap 'rm -rf "$work"' EXIT INT TERM

failures=0
assertions=0

fail() {
    printf 'lint-allow-test: FAIL: %s\n' "$*" >&2
    failures=$((failures + 1))
}

pass() {
    printf 'lint-allow-test: ok: %s\n' "$*"
}

# ---------------------------------------------------------------------------
# A fake golangci-lint.
#
# FAKE_ISSUES is a JSON array of issues it writes to --output.json.path, so a
# case can name the exact file-and-linter pairs it wants. FAKE_EXIT makes it
# fail AFTER writing a valid report, which is the case where a readable report
# could mask a dead linter. FAKE_EMPTY makes it write nothing at all.
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
if [ -n "${FAKE_EMPTY:-}" ]; then
    : > "$report"
    exit "${FAKE_EXIT:-0}"
fi
printf '{"Issues":%s}' "${FAKE_ISSUES:-[]}" > "$report"
exit "${FAKE_EXIT:-0}"
STUB
chmod +x "$work/tools/golangci-lint"

# One issue, spelled the way golangci-lint spells it.
issue() { # file linter
    printf '{"FromLinter":"%s","Text":"fake finding","Pos":{"Filename":"%s","Line":1}}' "$2" "$1"
}

seed_allow() { # allow-path source-file linter
    printf '{"Issues":[%s]}' "$(issue "$2" "$3")" >"$work/seed.json"
    go run ./tools/lintreport -allow "$1" -write "$work/seed.json" >/dev/null || exit 1
}

# run_subject <allow-list-path> — runs the real script against the fake linter.
run_subject() {
    TOOLS_DIR="$work/tools" LINT_ALLOW_LIST="$1" \
        scripts/lint-allow.sh >"$work/out" 2>&1
}

# ---------------------------------------------------------------------------
# CASE 1 — a finding on the list passes.
#
# The grandfathering half. Without it the gate blocks every change for months
# and gets deleted, which is how the whole-tree linter came to watch nothing.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
seed_allow "$work/allow-1" internal/daemon/workloop.go errcheck
FAKE_ISSUES="[$(issue internal/daemon/workloop.go errcheck)]" run_subject "$work/allow-1"
status=$?
if [ "$status" -eq 0 ]; then
    pass "a finding on the allow list passes"
else
    fail "a grandfathered finding was refused (exit $status)"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 2 — a finding NOT on the list fails.
#
# The whole point. If this case ever passes, the gate is a decoration.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
seed_allow "$work/allow-2" internal/daemon/workloop.go errcheck
FAKE_ISSUES="[$(issue internal/queue/append.go gosec)]" run_subject "$work/allow-2"
status=$?
if [ "$status" -eq 1 ]; then
    pass "a finding outside the allow list fails the build"
else
    fail "a NEW finding did not fail the build (exit $status, wanted 1)"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 3 — a new LINTER on an already-listed file fails.
#
# The pair is file AND linter. A list keyed on file alone would let a file that
# already tolerates one linter quietly start failing another.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
seed_allow "$work/allow-3" internal/daemon/workloop.go errcheck
FAKE_ISSUES="[$(issue internal/daemon/workloop.go gosec)]" run_subject "$work/allow-3"
status=$?
if [ "$status" -eq 1 ]; then
    pass "a new linter on an already-listed file fails the build"
else
    fail "a new linter on a listed file was tolerated (exit $status, wanted 1)"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 4 — a dead linter is a failure, not a clean tree.
#
# The stub writes a perfectly readable report and THEN exits non-zero. A reader
# that only looks at the report sees no findings and calls it green.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
seed_allow "$work/allow-4" internal/daemon/workloop.go errcheck
FAKE_ISSUES="[]" FAKE_EXIT=3 run_subject "$work/allow-4"
status=$?
if [ "$status" -eq 2 ]; then
    pass "a linter that failed is a hard failure, not a pass"
else
    fail "a dead linter did not fail closed (exit $status, wanted 2)"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 5 — an empty report is a failure, not a clean tree.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
seed_allow "$work/allow-5" internal/daemon/workloop.go errcheck
FAKE_EMPTY=1 run_subject "$work/allow-5"
status=$?
if [ "$status" -eq 2 ]; then
    pass "an empty lint report is a hard failure, not a pass"
else
    fail "an empty report did not fail closed (exit $status, wanted 2)"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 6 — a tree that does not compile cannot produce a verdict.
#
# This is the subtle one and it is the reason the check exists. golangci-lint
# reports a compile error as a `typecheck` finding and then sees almost nothing
# else, because the packages downstream of the break never load. The finding set
# collapses, everything left is grandfathered, and the run passes. A false green
# produced by a broken build is worse than no gate.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
seed_allow "$work/allow-6" internal/daemon/workloop.go errcheck
FAKE_ISSUES="[$(issue internal/daemon/broken.go typecheck)]" run_subject "$work/allow-6"
status=$?
if [ "$status" -eq 2 ]; then
    pass "a tree that does not compile fails closed instead of passing"
else
    fail "a typecheck finding did not fail closed (exit $status, wanted 2)"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 7 — a missing allow list is a failure, not "nothing to check".
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
FAKE_ISSUES="[]" run_subject "$work/allow-does-not-exist"
status=$?
if [ "$status" -eq 2 ]; then
    pass "a missing allow list is a hard failure"
else
    fail "a missing allow list did not fail closed (exit $status, wanted 2)"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 8 — a missing linter is a failure, not a clean tree.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
seed_allow "$work/allow-8" internal/daemon/workloop.go errcheck
TOOLS_DIR="$work/no-such-tools" LINT_ALLOW_LIST="$work/allow-8" \
    scripts/lint-allow.sh >"$work/out" 2>&1
status=$?
if [ "$status" -eq 2 ]; then
    pass "a missing golangci-lint is a hard failure"
else
    fail "a missing linter did not fail closed (exit $status, wanted 2)"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 9 — the step is actually wired into `make full`.
#
# Every case above is worthless if the target never runs. `make -n` expands the
# real step list rather than reading the Makefile text, so this cannot be
# satisfied by a comment that mentions the target.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
steps=$(HARMONIK_GATE_SELFTEST=1 make -n full 2>/dev/null)
if grep -q 'scripts/lint-allow\.sh' <<<"$steps"; then
    pass "make full runs the lint allow-list step"
else
    fail "make full does NOT run lint-allow, so nothing above is enforced"
fi

# ---------------------------------------------------------------------------
# CASE 10 — `make fast` still lints CHANGED LINES.
#
# Carried over from the ceiling's test, and it matters MORE under an allow list
# than it did under a count. The list is keyed on file and linter with no
# number, so a file already on the list can take on more findings of that same
# linter without failing. The --new-from-rev run in gate-static is what catches
# those, and 304 of the 636 listed pairs are in internal/daemon alone. Delete
# that step and nothing else in this file notices.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
fast_steps=$(HARMONIK_GATE_SELFTEST=1 make -n fast 2>/dev/null)
if grep -q -- '--new-from-rev' <<<"$fast_steps"; then
    pass "make fast still lints changed lines with --new-from-rev"
else
    fail "make fast does NOT lint changed lines, so a finding added to an already-listed file goes unseen"
fi

# ---------------------------------------------------------------------------
# CASE 11 — the tolerated set is printed even when the run passes.
#
# "No new lint findings" must never be readable as "this tree is clean". The
# report names what it is ignoring on every run, or the number quietly becomes
# invisible again.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
seed_allow "$work/allow-10" internal/daemon/workloop.go errcheck
FAKE_ISSUES="[$(issue internal/daemon/workloop.go errcheck)]" run_subject "$work/allow-10"
if grep -q 'IGNORED' "$work/out"; then
    pass "a passing run still prints what it is tolerating"
else
    fail "a passing run did not report the tolerated findings"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 12 — the inner loop runs this step too.
#
# The judge used to run in `make full` alone. A finding that a whole-function
# linter reports at a declaration outside the changed hunk is invisible to the
# --new-from-rev step, so the whole-tree judge is the ONLY step that can see it,
# and running it only at the merge decision handed every such finding to the
# next person to run the gate (hk-dp69a, five occurrences). Deleting it from
# `make fast` restores that hole silently, so it is asserted here.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
if grep -q 'scripts/lint-allow\.sh' <<<"$fast_steps"; then
    pass "make fast runs the whole-tree allow-list judge"
else
    fail "make fast does NOT run lint-allow, so a finding outside a changed hunk waits for the next merge decision"
fi

# ---------------------------------------------------------------------------
printf 'lint-allow-test: %d assertions, %d failures\n' "$assertions" "$failures"
[ "$failures" -eq 0 ] || exit 1
echo "lint-allow-test: PASS"
