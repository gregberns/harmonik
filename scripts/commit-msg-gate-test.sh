#!/usr/bin/env bash
# commit-msg-gate-test.sh — self-test for commit-msg-gate.sh.
#
# The gate's value is entirely in the cases where it goes RED. Every assertion
# below that matters builds a commit the validator must refuse and watches the
# gate refuse it. The one green assertion is the live check on this repository,
# and it is here so that a scope that quietly became empty shows up as a
# missing count rather than as a pass.

set -uo pipefail

GATE_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
GATE="${GATE_DIR}/commit-msg-gate.sh"
REPO_ROOT="$(cd -- "${GATE_DIR}/.." && pwd)"
PASS=0
FAIL=0

check() {
  local label="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    PASS=$(( PASS + 1 ))
  else
    FAIL=$(( FAIL + 1 ))
    echo "FAIL: ${label} — expected exit ${expected}, got ${actual}"
  fi
}

check_says() {
  local label="$1" needle="$2" file="$3"
  if grep -q -- "$needle" "$file"; then
    PASS=$(( PASS + 1 ))
  else
    FAIL=$(( FAIL + 1 ))
    echo "FAIL: ${label} — output did not contain '${needle}'"
    sed 's/^/    /' "$file"
  fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# ---------------------------------------------------------------------------
# A scratch repository, so a deliberately bad commit message never enters this
# one's history.
# ---------------------------------------------------------------------------
scratch="$tmp/scratch"
mkdir -p "$scratch"
git -C "$scratch" init -q
git -C "$scratch" config user.name 'gate self test'
git -C "$scratch" config user.email 'gate@example.invalid'
git -C "$scratch" config commit.gpgsign false

# commit_in_scratch <message> [file] — the optional file lets a side branch
# touch something of its own, so that merging it back tests the merge and not
# this helper's conflict handling.
commit_in_scratch() {
  local msg="$1" target="${2:-file.txt}"
  echo "$RANDOM$RANDOM" >>"$scratch/$target"
  git -C "$scratch" add "$target"
  printf '%s\n' "$msg" >"$tmp/msg.txt"
  git -C "$scratch" commit -q -F "$tmp/msg.txt"
}

good_msg='fix(gate): a well formed subject with the trailers this repo requires

Body text so the commit is not trivial.

Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": ["no-reviewer-reached"], "notes": "self-test fixture"}'

commit_in_scratch "$good_msg"
# Every case below names this commit as the grandfather baseline. Without
# that the scratch repository does not contain the real one, the gate drops
# to advisory mode, and every rejection case would pass while proving
# nothing. That is the shape this whole file exists to refuse.
first=$(git -C "$scratch" rev-parse HEAD)

# 1. A good tip passes.
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/good.out" 2>&1
check "a well formed tip commit passes" 0 "$?"

# 2. A fabricated approval naming the author is refused. This is the exact
#    shape that landed here repeatedly.
commit_in_scratch 'fix(gate): a subject that is fine while the trailer is not

Reviewed-By: self
Review-Verdict: {"schema_version": 1, "verdict": "APPROVE", "flags": [], "notes": "approved"}'
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/self.out" 2>&1
check "an APPROVE that names the author is refused" 1 "$?"
check_says "the refusal names the offending commit" "REJECTED" "$tmp/self.out"

# 3. A commit with no trailers at all is refused.
commit_in_scratch 'fix(gate): no review trailers anywhere in this message

Body text so the commit is not trivial.'
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/none.out" 2>&1
check "a commit with no review trailers is refused" 1 "$?"

# 4. An unusable type is refused — 'style' reads as valid and is not.
commit_in_scratch 'style(gate): a type this repo does not allow

Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": ["no-reviewer-reached"], "notes": "self-test fixture"}'
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/style.out" 2>&1
check "a disallowed commit type is refused" 1 "$?"

# 5. RANGE mode finds a bad commit that is no longer the tip. This is the case
#    the inner-loop check cannot see and the one the merge decision is for.
commit_in_scratch "$good_msg"
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" ) >"$tmp/range.out" 2>&1
check "range mode reports rather than refusing" 0 "$?"
check_says "range mode names the bad commit buried under a good tip" "REJECTED" "$tmp/range.out"
check_says "range mode counts what it read" "commits checked" "$tmp/range.out"
check_says "range mode says it is advice" "ADVICE" "$tmp/range.out"

# 6. The cap is announced, never silent. A gate that trims its own scope
#    without saying so reads as full coverage when it is not.
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" COMMIT_MSG_GATE_MAX=1 bash "$GATE" ) >"$tmp/cap.out" 2>&1
check_says "a capped scope says how many it skipped" "were NOT checked" "$tmp/cap.out"

# 7. A baseline this repository does not have drops to advice and SAYS so.
#    Dropping quietly would be the fail-open.
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE=0000000000000000000000000000000000000000 bash "$GATE" ) >"$tmp/unknown.out" 2>&1
check_says "an unknown baseline is announced" "is not in this repository" "$tmp/unknown.out"

# 8. A history that does not descend from the baseline gets advice, not a
#    verdict — and it still SAYS what is wrong. The tip here is bad on purpose,
#    so this is the case that proves advisory mode reports rather than shrugs.
git -C "$scratch" checkout -q -b sidebranch "$first"
commit_in_scratch "$good_msg"
side=$(git -C "$scratch" rev-parse HEAD)
git -C "$scratch" checkout -q -
commit_in_scratch 'fix(gate): a bad trailer on a history outside the ratchet

Reviewed-By: self
Review-Verdict: {"schema_version": 1, "verdict": "APPROVE", "flags": [], "notes": "approved"}'
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$side" bash "$GATE" --head-only ) >"$tmp/nonanc.out" 2>&1
check "a history outside the ratchet does not fail the build" 0 "$?"
check_says "an off-ratchet history is announced as advice" "ADVICE" "$tmp/nonanc.out"
check_says "advisory mode still names the bad commit" "REJECTED" "$tmp/nonanc.out"

# 8b. The same bad tip, on a history that DOES descend from the baseline, is a
#     verdict. Without this pair, advisory mode could have swallowed everything
#     and every other case here would still pass.
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/onratchet.out" 2>&1
check "the same bad tip ON the ratchet fails the build" 1 "$?"

# 8c. A MERGE tip is judged as itself. Selecting the tip with
#     `rev-list --no-merges -n 1` returns the newest non-merge ANCESTOR, so on a
#     merge branch the gate judged a commit the author did not write and could
#     not amend, while printing that it had checked the tip. This branch is a
#     merge branch, so that was not hypothetical.
#     The side branch carries a REFUSED message and the merge itself carries a
#     clean one. Correct behaviour reads the merge and passes. The old spelling
#     reads the side branch's commit and fails. A side branch with a good
#     message could not tell those two apart, and did not.
git -C "$scratch" checkout -q -b mergetip "$first"
commit_in_scratch 'fix(gate): a refused message on a merged side branch

Reviewed-By: self
Review-Verdict: {"schema_version": 1, "verdict": "APPROVE", "flags": [], "notes": "approved"}' side.txt
git -C "$scratch" checkout -q -
git -C "$scratch" merge -q --no-ff -m 'Merge branch mergetip' mergetip
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/merge.out" 2>&1
check "a merge tip is judged on its own message, not an ancestor's" 0 "$?"
check_says "exactly one commit was read" "1 commits checked" "$tmp/merge.out"

# 9. The live assertion on this repository. It must check a non-empty scope —
#    a gate whose scope has silently emptied is the defect, not a pass.
( cd "$REPO_ROOT" && bash "$GATE" ) >"$tmp/live.out" 2>&1
check "this repository's own in-scope commits all pass" 0 "$?"
# LIVE_SCOPE_FLOOR — the ratchet's own ratchet. Moving the grandfather baseline
# forward is the one edit the gate exists to catch, and without a floor it was
# free: shifting it by 5, 10, 20 or 40 commits left this suite green. The
# baseline covered 42 commits the day it was set and that number only grows, so
# a scope below this floor means somebody moved it.
LIVE_SCOPE_FLOOR=40
live_count=$(sed -n 's/^commit-msg-gate: \([0-9][0-9]*\) commits checked.*/\1/p' "$tmp/live.out")
if [ -n "$live_count" ] && [ "$live_count" -ge "$LIVE_SCOPE_FLOOR" ]; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the live scope covered '${live_count:-nothing}' commits, under the floor of ${LIVE_SCOPE_FLOOR}; the grandfather baseline has been moved forward"
  sed 's/^/    /' "$tmp/live.out"
fi

# 10. A scope that empties fails CLOSED. This is the case a mutation caught:
#     with the cap set to zero the gate had nothing to check, and every other
#     assertion here still passed, because none of them could tell "checked
#     everything and found nothing wrong" from "checked nothing".
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" COMMIT_MSG_GATE_MAX=0 bash "$GATE" ) >"$tmp/empty.out" 2>&1
check "an empty scope is a hard failure even in the reporting mode" 1 "$?"
check_says "the empty-scope refusal says why" "cannot fail and is not evidence" "$tmp/empty.out"

echo "commit-msg-gate-test: $(( PASS + FAIL )) assertions, ${FAIL} failed"
[ "$FAIL" -eq 0 ] || exit 1
