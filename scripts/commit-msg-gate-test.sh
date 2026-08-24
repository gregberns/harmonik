#!/usr/bin/env bash
# commit-msg-gate-test.sh — self-test for commit-msg-gate.sh.
#
# The gate's value is entirely in the cases where it goes RED. Every assertion
# below that matters builds a commit the validator must refuse and watches the
# gate refuse it. EIGHT assertions expect a green exit rather than a red one,
# and most of those are controls — they prove the gate did not trade a
# fail-open for a gate that refuses everything. Count them with
# `grep -c '^check ".*" 0 "$?"' scripts/commit-msg-gate-test.sh` — the anchor
# matters, because without it the command counts this comment line too and
# answers 9. The live check
# on this repository is one of the seven, and it is here so that a scope that
# quietly became empty shows up as a missing count rather than as a pass. This
# header used to say there was one.

set -uo pipefail

GATE_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
GATE="${GATE_DIR}/commit-msg-gate.sh"
REPO_ROOT="$(cd -- "${GATE_DIR}/.." && pwd)"

# The gate calls `harmonik commit-msg validate`, and it builds that binary when
# COMMIT_MSG_VALIDATOR does not already name one. This file drives the gate more
# than a dozen times, so it builds once here and exports the path. `make
# commit-msg-check` sets the same variable and hands down its own build.
if [ -z "${COMMIT_MSG_VALIDATOR:-}" ]; then
  validator_dir=$(mktemp -d)
  COMMIT_MSG_VALIDATOR="${validator_dir}/commit-msg-validator"
  if ! ( cd "$REPO_ROOT" && go build -o "$COMMIT_MSG_VALIDATOR" ./cmd/harmonik ); then
    echo "commit-msg-gate-test: FAIL — could not build the commit-message validator" >&2
    exit 1
  fi
fi
export COMMIT_MSG_VALIDATOR
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
trap 'rm -rf "$tmp"; if [ -n "${validator_dir:-}" ]; then rm -rf -- "$validator_dir"; fi' EXIT

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

# 4. A subject the OLD validator refused for its shape now passes. `Revert "..."`
#    is not Conventional Commits and `git revert` writes it unasked, so the
#    format half refused a message no author chose and no command amends. The
#    format half is gone, and this case is here so that putting it back shows up
#    as a failure rather than as a quiet return of a rule nobody argued for.
commit_in_scratch 'Revert "feat(gate): a change that turned out to be wrong"

This reverts commit 0000000000000000000000000000000000000000.

Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": ["no-reviewer-reached"], "notes": "self-test fixture"}'
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/revert.out" 2>&1
check "a Revert subject passes; the gate reads trailers, not subject shape" 0 "$?"

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

# 8d. THE GATE AND THE AUDIT AGREE ABOUT A COMMENTED-OUT TRAILER.
#
#     A commented-out line is not a claim. The audit that asks this history
#     which commits a reviewer read is ANCHORED —
#     `git log --grep '^Reviewed-By: agent-reviewer'` — so it walks past a `#`
#     line, and internal/commitmsg reads trailer lines at column zero only, so
#     it walks past the same one. The pair is the property. Either side alone
#     is a claim about a rule; together they are a claim about AGREEMENT, and a
#     drift on either side turns this case red.
#
#     This case used to assert the opposite, against an UNANCHORED grep, and the
#     cost of that spelling was real: a docs commit that quoted the trailer
#     format as an example, while honestly recording that no reviewer was
#     reached, was refused for the example.
#
#     `<sha>^!` scopes the grep to the ONE commit. `git log -1 --grep` does
#     not: it walks ANCESTORS and returns the newest match, so it answers yes
#     for a commit whose own message is clean. That mistake was made here once.
commit_in_scratch 'fix(gate): a reviewer name written on a comment line, claiming nothing

Reviewed-By: none — no reviewer was reached for this commit
# Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "self-test fixture"}'
hash_sha=$(git -C "$scratch" rev-parse HEAD)
hash_stored=0
git -C "$scratch" log -1 --format=%B "$hash_sha" >"$tmp/hashline.msg"
grep -qF '# Reviewed-By: agent-reviewer' "$tmp/hashline.msg" && hash_stored=1
check "git stored the comment line, so there is something to disagree about" 1 "$hash_stored"
hash_audit_hits="$(git -C "$scratch" log "${hash_sha}^!" --grep '^Reviewed-By: agent-reviewer' --format=%H)"
hash_audited=0
[ -n "$hash_audit_hits" ] && hash_audited=1
check "the anchored audit does NOT count the #-hidden line as reviewed work" 0 "$hash_audited"
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/hashline.out" 2>&1
check "and the gate agrees with the audit: it passes" 0 "$?"

# 8e. THE OTHER HALF OF THE PAIR. The same name written where it IS a claim —
#     at column zero, on the Reviewed-By trailer — is counted by the anchored
#     audit and refused by the gate. Without this, 8d is satisfied by a gate
#     that has stopped reading the rule at all.
commit_in_scratch 'fix(gate): the same reviewer name written where it is a claim

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "self-test fixture"}'
claim_sha=$(git -C "$scratch" rev-parse HEAD)
claim_audit_hits="$(git -C "$scratch" log "${claim_sha}^!" --grep '^Reviewed-By: agent-reviewer' --format=%H)"
claim_audited=0
[ -n "$claim_audit_hits" ] && claim_audited=1
check "the anchored audit DOES count the column-zero trailer as reviewed work" 1 "$claim_audited"
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/claim.out" 2>&1
check "a NOT_REVIEWED that names a real reviewer is refused" 1 "$?"
check_says "the refusal names the reviewer the trailer claimed" "must not name a reviewer this repo has" "$tmp/claim.out"

# 8f. The control for 8d. A normal `git commit -F` message with no comment line
#     in it must behave exactly as it did before any of this — otherwise the fix
#     has traded a fail-open for a build nobody can green.
commit_in_scratch "$good_msg"
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/nohash.out" 2>&1
check "a message with no comment line is unaffected" 0 "$?"

# 8g. ONE GIT SETTING USED TO TURN THIS GATE OFF.
#
#     The gate reads a message git has ALREADY STORED. The validator, left to
#     itself, resolves the cleanup mode from `commit.cleanup` — a setting that
#     describes the NEXT commit, not the one being judged. So `commit.cleanup=
#     strip` made it drop comment lines out of messages written and stored long
#     before the setting existed. The gate pins the mode to `verbatim` itself,
#     because a stored message has nothing left to clean up.
#
#     THE FIXTURE IS A RELOCATED SUBJECT, which is where the fail-open still
#     lives. Stripping the leading `#` line moves the subject down onto the
#     `fixup!` line, and a `fixup!` subject is EXEMPT from the trailer rules
#     entirely — `git rebase --autosquash` folds such a commit into another one
#     and it never lands under that subject. So the stripped view sees a commit
#     that owes nothing, while the message git stored is an ordinary commit with
#     no review trailers at all. One setting, in a config file nobody reads at
#     commit time, and the gate waves it through.
#
#     The stored subject is measured rather than assumed. If the fixture ever
#     stops leading with the comment line, this case must go red rather than
#     pass on a commit that was never at risk.
commit_in_scratch '# fix(gate): a comment line that git stores as the subject
fixup! an earlier commit whose subject this is not

Body text so the commit is not trivial.'
strip_subject=$(git -C "$scratch" log -1 --format=%s HEAD)
strip_stored=0
[ "${strip_subject:0:1}" = "#" ] && strip_stored=1
check "git stored the # line as the subject, so there is something to hide" 1 "$strip_stored"

git -C "$scratch" config commit.cleanup strip
git -C "$scratch" config core.commentChar '#'
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/strip.out" 2>&1
check "commit.cleanup=strip does not let a stripped subject mint an exemption" 1 "$?"
check_says "the refusal is the one the stored message earns" "missing required trailer" "$tmp/strip.out"

# The control. Under the same setting an ordinary message must still pass,
# or the fix has traded a fail-open for a gate that refuses everything.
commit_in_scratch "$good_msg"
( cd "$scratch" && COMMIT_MSG_GATE_BASELINE="$first" bash "$GATE" --head-only ) >"$tmp/stripok.out" 2>&1
check "under commit.cleanup=strip a clean message still passes" 0 "$?"
git -C "$scratch" config --unset commit.cleanup
git -C "$scratch" config --unset core.commentChar

# 9. The live assertion on this repository. It must check a non-empty scope —
#    a gate whose scope has silently emptied is the defect, not a pass.
#
#    WHAT IT DOES NOT ASSERT: that every in-scope commit passes. Range mode is
#    advisory by construction and exits 0 whatever it finds, so an exit-status
#    check here cannot see a refusal and never could. The label used to claim
#    the stronger thing, and by 2026-08-13 that claim was false while this
#    assertion stayed green: 57 commits in scope, 2 refused (4b6a63243 and
#    bc98a3dfb). Both were ACCEPTED by the validator of the day they landed
#    and are refused by a later tightening — which is the case the ratchet
#    exists to carry without rewriting history, not a defect. The count is
#    reported by the gate and floored below; re-derive with
#    `bash scripts/commit-msg-gate.sh`.
( cd "$REPO_ROOT" && bash "$GATE" ) >"$tmp/live.out" 2>&1
check "the live gate runs to completion over a non-empty scope" 0 "$?"
# LIVE_SCOPE_FLOOR — the ratchet's own ratchet. Moving the grandfather baseline
# forward is the one edit the gate exists to catch, and without a floor it was
# free: shifting it by 5, 10, 20 or 40 commits left this suite green. The
# baseline covered more than forty commits the day it was set and that number
# only grows, so a scope below this floor means somebody moved it. Re-derive
# the size on the day it landed with
# `git rev-list 4102b4e2e582e90ddca141afd0494ff31db187d6..a6c22ee50 | wc -l` —
# 44 with the gate's own commit, 43 without it. This line used to say 42, and
# no reading of this history produces 42.
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
