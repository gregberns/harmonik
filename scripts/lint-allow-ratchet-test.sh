#!/usr/bin/env bash
# lint-allow-ratchet-test.sh — proves the allow-list ratchet is a verdict.
#
# The ratchet is the only mechanical thing standing between a red lint step and
# the repair that makes it green without fixing anything: append the pair to
# tools/lintreport/allow.txt. Every way this check can be wrong is silent — it
# tolerates the new pair, it cannot find a base and calls that a pass, or it is
# not wired into the inner loop at all. Each has a case below.
#
# The cases run inside a scratch git repository with a scratch list, so they
# cost about a second and never touch the real one.

set -uo pipefail

repo_root=$(git rev-parse --show-toplevel) || {
    echo "lint-allow-ratchet-test: not inside a git worktree" >&2
    exit 1
}
cd "$repo_root" || exit 1
subject="$repo_root/scripts/lint-allow-ratchet.sh"

work=$(mktemp -d "${TMPDIR:-/tmp}/lint-allow-ratchet-test-XXXXXX") || exit 1
trap 'rm -rf "$work"' EXIT INT TERM

failures=0
assertions=0

fail() {
    printf 'lint-allow-ratchet-test: FAIL: %s\n' "$*" >&2
    failures=$((failures + 1))
}

pass() {
    printf 'lint-allow-ratchet-test: ok: %s\n' "$*"
}

# fixture — a scratch repository whose HEAD carries a two-pair allow list.
fixture() {
    local dir="$1"
    mkdir -p "$dir/tools"
    git -C "$dir" init --quiet
    git -C "$dir" config user.email test@example.com
    git -C "$dir" config user.name test
    printf '# a comment line\n\nalpha.go\terrcheck\nbeta.go\tcyclop\n' >"$dir/tools/allow.txt"
    git -C "$dir" add tools/allow.txt
    git -C "$dir" commit --quiet -m seed
    printf 'x\n' >"$dir/other.txt"
    git -C "$dir" add other.txt
    git -C "$dir" commit --quiet -m second
}

# run_subject <dir> — runs the ratchet inside a scratch repository.
run_subject() {
    ( cd "$1" && LINT_ALLOW_LIST=tools/allow.txt "$subject" ) >"$work/out" 2>&1
}

# ---------------------------------------------------------------------------
# CASE 1 — an unchanged list passes.
#
# A ratchet that fails on a clean tree is deleted within a day, so this case is
# as load-bearing as the ones below.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
fixture "$work/clean"
if run_subject "$work/clean"; then
    pass "an unchanged allow list passes"
else
    fail "an unchanged allow list did not pass"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 2 — a pair added in the WORKING TREE fails.
#
# This is the window where the temptation lives: the judge goes red, somebody
# appends the pair, and runs the gate again.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
fixture "$work/dirty"
printf 'gamma.go\tfunlen\n' >>"$work/dirty/tools/allow.txt"
run_subject "$work/dirty"
status=$?
if [ "$status" -eq 1 ] && grep -q 'gamma.go' "$work/out"; then
    pass "an uncommitted new pair fails and is named"
else
    fail "an uncommitted new pair did not fail (exit $status, wanted 1)"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 3 — a pair added in the TOP COMMIT fails.
#
# The gate also runs after a commit. Without this window the pair would pass
# every run from then on.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
fixture "$work/committed"
printf 'gamma.go\tfunlen\n' >>"$work/committed/tools/allow.txt"
git -C "$work/committed" add tools/allow.txt
git -C "$work/committed" commit --quiet -m 'tolerate one more'
run_subject "$work/committed"
status=$?
if [ "$status" -eq 1 ] && grep -q 'gamma.go' "$work/out"; then
    pass "a committed new pair fails and is named"
else
    fail "a committed new pair did not fail (exit $status, wanted 1)"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 4 — REMOVING a pair passes. The list may get shorter.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
fixture "$work/shorter"
printf '# a comment line\nalpha.go\terrcheck\n' >"$work/shorter/tools/allow.txt"
if run_subject "$work/shorter"; then
    pass "removing a pair passes"
else
    fail "removing a pair was refused, so the list can never be cleaned"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 5 — reordering and re-commenting change nothing.
#
# The verdict is about the SET of pairs. A list that churns on formatting would
# fail on unrelated edits, and a gate that cries wolf gets switched off.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
fixture "$work/reordered"
printf 'beta.go\tcyclop\n# another comment\nalpha.go\terrcheck\n' >"$work/reordered/tools/allow.txt"
if run_subject "$work/reordered"; then
    pass "reordering and re-commenting pass"
else
    fail "a formatting-only change failed the ratchet"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 6 — a missing allow list fails closed, and does not read as "nothing to
# tolerate, so nothing was added".
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
fixture "$work/missing"
rm "$work/missing/tools/allow.txt"
run_subject "$work/missing"
status=$?
if [ "$status" -eq 2 ]; then
    pass "a missing allow list fails closed"
else
    fail "a missing allow list did not fail closed (exit $status, wanted 2)"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 7 — a merge is judged against the union of its parents.
#
# One side adds nothing the other side did not already carry, so the merge
# introduces no new pair and must pass. Judging a merge against its first
# parent alone would fail the innocent commit that joins two lanes.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
fixture "$work/merge"
git -C "$work/merge" checkout --quiet -b side
printf '# a comment line\nalpha.go\terrcheck\n' >"$work/merge/tools/allow.txt"
git -C "$work/merge" commit --quiet -am 'clean beta'
git -C "$work/merge" checkout --quiet -
# The branch we merge INTO must move too, or git fast-forwards and no merge
# commit exists — which would make this case silently test nothing.
printf 'y\n' >"$work/merge/third.txt"
git -C "$work/merge" add third.txt
git -C "$work/merge" commit --quiet -m third
git -C "$work/merge" merge --quiet --no-edit side >/dev/null 2>&1
parents=$(git -C "$work/merge" rev-parse HEAD^@ | wc -l | tr -d ' ')
[ "$parents" -eq 2 ] || fail "the merge fixture produced $parents parent(s), so this case tests nothing"
if run_subject "$work/merge"; then
    pass "a merge that adds no pair passes"
else
    fail "a merge that adds no pair was refused"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 8 — a FULLY CLEANED list passes.
#
# An empty tolerated set is the state this ratchet exists to reach, and the
# first version of the script failed it: blank lines were dropped by `grep -v`,
# grep exits 1 when it matches nothing, and under `pipefail` that reached the
# caller as "could not read the list". So the goal state read as a broken gate,
# from inside gate-static, where it would have blocked every test.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
fixture "$work/cleaned"
printf '# every pair has been cleaned\n' >"$work/cleaned/tools/allow.txt"
run_subject "$work/cleaned"
status=$?
if [ "$status" -eq 0 ]; then
    pass "a fully cleaned allow list passes"
else
    fail "an empty allow list did not pass (exit $status, wanted 0)"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 9 — an empty list at HEAD that gains a pair still FAILS.
#
# The guard above must not turn into "an empty base means anything goes".
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
fixture "$work/cleaned-then-grown"
printf '# every pair has been cleaned\n' >"$work/cleaned-then-grown/tools/allow.txt"
git -C "$work/cleaned-then-grown" commit --quiet -am 'clean the list'
printf 'gamma.go\tfunlen\n' >>"$work/cleaned-then-grown/tools/allow.txt"
# COMMITTED, so this case reaches the SECOND window. Left uncommitted it would
# be caught by the working-tree window and never exercise the empty base at all.
git -C "$work/cleaned-then-grown" commit --quiet -am 'tolerate one more'
run_subject "$work/cleaned-then-grown"
status=$?
if [ "$status" -eq 1 ] && grep -q 'gamma.go' "$work/out"; then
    pass "a pair added to an empty list still fails"
else
    fail "a pair added to an empty list did not fail (exit $status, wanted 1)"
    cat "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# CASE 10 — the ratchet is actually wired into the inner loop.
#
# Every case above is worthless if the step never runs. `make -n` expands the
# real step list, so a comment naming the script cannot satisfy this.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
fast_steps=$(HARMONIK_GATE_SELFTEST=1 make -n fast 2>/dev/null)
if grep -q 'scripts/lint-allow-ratchet\.sh' <<<"$fast_steps"; then
    pass "make fast runs the allow-list ratchet"
else
    fail "make fast does NOT run the ratchet, so the allow list can grow freely"
fi

# ---------------------------------------------------------------------------
printf 'lint-allow-ratchet-test: %d assertions, %d failures\n' "$assertions" "$failures"
[ "$failures" -eq 0 ] || exit 1
echo "lint-allow-ratchet-test: PASS"
