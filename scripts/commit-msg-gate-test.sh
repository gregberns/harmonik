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
# The merge declares itself trivial. A merge is no longer exempt from the
# review-trailer rules -- it lands, so it answers for itself -- and this case is
# about WHICH commit the gate reads, not about what a merge owes a reviewer.
git -C "$scratch" merge -q --no-ff -m 'Merge branch mergetip

Trivial: true' mergetip
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/merge.out" 2>&1
check "a merge tip is judged on its own message, not an ancestor's" 0 "$?"
check_says "exactly one commit was read" "1 commits checked" "$tmp/merge.out"

# 8d. THE COMMENT LINES GIT ACTUALLY KEEPS. This is the only place the defect
#     could be seen: it is a difference between the stored message and the
#     validator's view of it, so a fixture file cannot show it.
#
#     `git commit -F` — the spelling AGENTS.md mandates — gets cleanup mode
#     `whitespace`, which does NOT remove comment lines. The validator stripped
#     every `#` line anyway, so a fabricated trailer written on one landed in
#     the commit while being invisible to every check. Measured against the
#     unfixed validator, through this gate, on this exact commit: the audit
#     grep counted it and the gate exited 0.
#
#     BOTH HALVES ARE ASSERTED, and the first is not decoration. "The gate
#     refuses it" is satisfied by a validator that refuses everything, and it
#     is also satisfied by a fixture that has drifted into a shape the audit no
#     longer counts — at which point the case still passes and guards nothing.
#     The property is agreement with the audit, so the audit is measured.
#
#     `<sha>^!` scopes the grep to the ONE commit. `git log -1 --grep` does
#     not: it walks ANCESTORS and returns the newest match, so it answers yes
#     for a commit whose own message is clean. That mistake was made here once.
commit_in_scratch 'fix(gate): a fabricated approval hidden behind a comment line

Reviewed-By: none — no reviewer was reached for this commit
# Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "self-test fixture"}'
hash_sha=$(git -C "$scratch" rev-parse HEAD)
hash_audit_hits="$(git -C "$scratch" log "${hash_sha}^!" --grep 'Reviewed-By: agent-reviewer' --format=%H)"
hash_audited=0
[ -n "$hash_audit_hits" ] && hash_audited=1
check "the audit grep counts the #-hidden trailer as reviewed work" 1 "$hash_audited"
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/hashline.out" 2>&1
check "a real commit with a #-prefixed fabricated trailer is refused" 1 "$?"
check_says "the refusal names the reviewer the comment line minted" "must not name a reviewer this repo has" "$tmp/hashline.out"

# 8e. THE SAME STRIP RELOCATED THE SUBJECT. With the `#` line dropped, the
#     subject rules landed on line 2. The stored subject here is 137
#     characters, nearly twice the ceiling this repo pins at 72, and the line
#     the validator read instead is a clean 57. Against the unfixed validator
#     this commit exited 0.
#
#     The stored length is measured rather than assumed, for the same reason as
#     above: if the fixture ever stops being over the ceiling, this case must
#     go red rather than pass on a subject that was never long.
hash_long=$(printf '%*s' 130 '' | tr ' ' 'x')
commit_in_scratch "# fix: ${hash_long}

fix(gate): the short valid subject the strip read instead

Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {\"schema_version\": 1, \"verdict\": \"NOT_REVIEWED\", \"flags\": [], \"notes\": \"self-test fixture\"}"
reloc_subject=$(git -C "$scratch" log -1 --format=%s HEAD)
check "git stores the # line as the subject, over the ceiling" 137 "${#reloc_subject}"
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/reloc.out" 2>&1
check "a real commit whose stored subject is a # line is refused" 1 "$?"
check_says "the refusal measures the line git stored, not the one under it" "subject line is 137 chars; max is 72" "$tmp/reloc.out"

# 8f. The control. A normal `git commit -F` message with no comment line in it
#     must behave exactly as it did before any of this — otherwise the fix has
#     traded a fail-open for a build nobody can green.
commit_in_scratch "$good_msg"
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/nohash.out" 2>&1
check "a message with no comment line is unaffected" 0 "$?"

# 8g. ONE GIT SETTING USED TO TURN THIS GATE OFF.
#
#     The gate reads a message git has ALREADY STORED. The validator, left to
#     itself, resolves the cleanup mode from `commit.cleanup` — a setting that
#     describes the NEXT commit, not the one being judged. So setting
#     `commit.cleanup=strip` made the validator strip comment lines out of
#     messages that were written and stored long before the setting existed,
#     and every fabricated `# Reviewed-By:` trailer already in the range went
#     invisible again. Measured on this scratch repository: 0 rejected with the
#     setting and 1 rejected with `COMMIT_MSG_CLEANUP=verbatim` on the same
#     commit. The gate now sets that mode itself, because a stored message has
#     nothing left to clean up.
#
#     The stored message is measured rather than assumed. If the fixture ever
#     stops carrying the comment line, this case must go red rather than pass
#     on a commit that never had one.
commit_in_scratch 'fix(gate): a fabricated approval the strip setting used to hide

Reviewed-By: none — no reviewer was reached for this commit
# Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "self-test fixture"}'
git -C "$scratch" log -1 --format=%B HEAD >"$tmp/strip.msg"
strip_stored=0
grep -qF '# Reviewed-By: agent-reviewer' "$tmp/strip.msg" && strip_stored=1
check "git stored the comment line, so there is something to hide" 1 "$strip_stored"

git -C "$scratch" config commit.cleanup strip
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/strip.out" 2>&1
check "commit.cleanup=strip does not blind the gate to a stored # trailer" 1 "$?"
check_says "the refusal still names the reviewer the comment line minted" "must not name a reviewer this repo has" "$tmp/strip.out"

# The control. Under the same setting an ordinary message must still pass,
# or the fix has traded a fail-open for a gate that refuses everything.
commit_in_scratch "$good_msg"
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/stripok.out" 2>&1
check "under commit.cleanup=strip a clean message still passes" 0 "$?"
git -C "$scratch" config --unset commit.cleanup

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
