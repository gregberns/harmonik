#!/usr/bin/env bash
# lint-changed-test.sh — proves the changed-line lint step tells the truth about
# a run that reached no answer, and keeps failing on one that did.
#
# THE CASE. golangci-lint analyses the whole tree even when it reports only the
# lines you changed. With a cold analysis cache that work can exceed the cap in
# .golangci.yml, and the linter then exits 4 having named no file and no finding.
# What an agent saw was a red `make fast` with nothing failing in it, which reads
# exactly like a code failure. docs/disk-reclaim.md section 0 tells operators to
# clear that cache, so the project itself puts a reader in that state.
#
# TWO WAYS TO GET THIS WRONG, and this file refuses both.
#
#   MAKE THE TIMEOUT PASS. The obvious repair, and the one that turns the gate
#   into a fail-open gate: a step that reached no answer would report a pass.
#   Case 1 asserts the exit stays non-zero.
#
#   EXCUSE EVERYTHING. A wrapper that prints "this is not a verdict" on every
#   failure hides real findings behind the same words. Case 2 runs a genuine
#   finding through the same wrapper and asserts the inconclusive wording is
#   absent.
#
# Case 4 is what keeps the wrapper from becoming decoration: the real gate-static
# recipe must call it, and no step of that recipe may reach golangci-lint around
# it.

set -uo pipefail

# Recursion guard. script-tests runs this file, and case 4 expands `make -n`,
# which recurses into script-tests. The outer run holds the assertions.
if [ -n "${HARMONIK_GATE_SELFTEST:-}" ]; then
    echo "lint-changed-test: nested under the gate; the outer run holds the assertions"
    exit 0
fi

repo_root=$(git rev-parse --show-toplevel) || {
    echo "lint-changed-test: not inside a git worktree" >&2
    exit 1
}
cd "$repo_root" || exit 1

failures=0
assertions=0

fail() {
    printf 'lint-changed-test: FAIL: %s\n' "$*" >&2
    failures=$((failures + 1))
}

pass() {
    printf 'lint-changed-test: ok: %s\n' "$*"
}

# The wording the reader depends on. Named once, asserted present in the
# inconclusive case and absent in the finding case, so the two can never drift
# into meaning the same thing.
inconclusive_phrase='not a verdict on your code'

# run_wrapper <stub-rc> <stub-stderr-line>
#
# Runs the real scripts/lint-changed.sh with a stub linter first in its argv.
# Sets globals rather than echoing: a $(...) capture would lose the scratch dir.
#   WRAP_STATUS  the wrapper's exit status
#   WRAP_OUT     the combined output file
#   WRAP_DIR     the scratch directory, for the caller to remove
run_wrapper() {
    local rc="$1" line="$2"
    WRAP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/lint-changed-XXXXXX")
    mkdir -p "$WRAP_DIR/bin"
    : >"$WRAP_DIR/marker"
    cat >"$WRAP_DIR/bin/golangci-lint" <<STUB
#!/usr/bin/env bash
printf '%s\n' "\$*" >>"$WRAP_DIR/marker"
printf '%s\n' "$line" >&2
exit $rc
STUB
    chmod +x "$WRAP_DIR/bin/golangci-lint"
    WRAP_OUT="$WRAP_DIR/out"
    env HARMONIK_LANE_GOCACHE_ROOT="$WRAP_DIR/gocache" \
        scripts/lint-changed.sh "$WRAP_DIR/bin/golangci-lint" \
        run --allow-parallel-runners --new-from-rev=HEAD~1 >"$WRAP_OUT" 2>&1
    WRAP_STATUS=$?
    WRAP_CALLS=$(wc -l <"$WRAP_DIR/marker" | tr -d ' ')
}

# ---------------------------------------------------------------------------
# 1. A TIMEOUT IS INCONCLUSIVE, AND INCONCLUSIVE STILL FAILS.
#
# Both halves are the assertion. Non-zero alone is satisfied by a wrapper that
# died before it reached the linter, and the wording alone is satisfied by a
# wrapper that prints the words and then exits 0.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
run_wrapper 4 'level=error msg="Timeout exceeded: try increasing it by passing --timeout option"'
if [ "$WRAP_STATUS" -eq 0 ]; then
    fail "a lint timeout: the wrapper exited 0, so a step that reached no answer reports a pass"
    cat "$WRAP_OUT" >&2
elif [ "$WRAP_CALLS" -eq 0 ]; then
    fail "a lint timeout: the wrapper exited $WRAP_STATUS but never reached the linter, so this proves nothing"
    cat "$WRAP_OUT" >&2
elif ! grep -qi "$inconclusive_phrase" "$WRAP_OUT"; then
    fail "a lint timeout: blocked (exit $WRAP_STATUS) but the output does not say it is $inconclusive_phrase, so it still reads as a finding"
    cat "$WRAP_OUT" >&2
elif ! grep -qi 'cache' "$WRAP_OUT"; then
    fail "a lint timeout: says it is inconclusive but never names the cold cache, so the reader has nothing to act on"
    cat "$WRAP_OUT" >&2
else
    pass "a lint timeout: blocked (exit $WRAP_STATUS) and reported as inconclusive with the cold cache named"
fi
rm -rf "$WRAP_DIR"

# ---------------------------------------------------------------------------
# 2. A REAL FINDING IS NOT EXCUSED.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
run_wrapper 1 'internal/queue/store.go:41:2: ineffectual assignment to err (ineffassign)'
if [ "$WRAP_STATUS" -eq 0 ]; then
    fail "a real finding: the wrapper exited 0, so a reported finding does not fail the gate"
    cat "$WRAP_OUT" >&2
elif grep -qi "$inconclusive_phrase" "$WRAP_OUT"; then
    fail "a real finding: the wrapper called it $inconclusive_phrase, so every failure now reads as inconclusive"
    cat "$WRAP_OUT" >&2
elif ! grep -q 'ineffassign' "$WRAP_OUT"; then
    fail "a real finding: the linter's own output did not reach the reader"
    cat "$WRAP_OUT" >&2
else
    pass "a real finding: blocked (exit $WRAP_STATUS), reported as a finding, and the linter's output reached the reader"
fi
rm -rf "$WRAP_DIR"

# ---------------------------------------------------------------------------
# 3. A CLEAN RUN STILL PASSES.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
run_wrapper 0 '0 issues.'
if [ "$WRAP_STATUS" -ne 0 ]; then
    fail "a clean run: the wrapper exited $WRAP_STATUS on a linter that found nothing"
    cat "$WRAP_OUT" >&2
else
    pass "a clean run: the wrapper exited 0"
fi
rm -rf "$WRAP_DIR"

# ---------------------------------------------------------------------------
# 4. THE GATE ACTUALLY USES IT.
#
# `make -n` expands variables and recurses, so this reads the real step list
# rather than the Makefile text. A wrapper nothing calls is decoration, and a
# second step that reaches golangci-lint directly is the same hole reopened.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
steps=$(HARMONIK_GATE_SELFTEST=1 make -n gate-static 2>/dev/null)
if [ -z "$steps" ]; then
    fail "structural: 'make -n gate-static' produced nothing, so nothing was checked"
elif ! printf '%s\n' "$steps" | grep -q 'scripts/lint-changed.sh'; then
    fail "structural: no step of gate-static calls scripts/lint-changed.sh"
else
    bypass=$(printf '%s\n' "$steps" | grep -vE '^[[:space:]]*#' | grep 'golangci-lint' | grep -v 'lint-changed.sh')
    if [ -n "$bypass" ]; then
        fail "structural: gate-static reaches golangci-lint around the wrapper:"
        printf '%s\n' "$bypass" >&2
    else
        pass "structural: gate-static lints only through scripts/lint-changed.sh"
    fi
fi

# ---------------------------------------------------------------------------
printf 'lint-changed-test: %d assertions, %d failed\n' "$assertions" "$failures"
if [ "$assertions" -lt 4 ]; then
    printf 'lint-changed-test: only %d assertions ran; this file expects 4\n' "$assertions" >&2
    exit 1
fi
[ "$failures" -eq 0 ]
