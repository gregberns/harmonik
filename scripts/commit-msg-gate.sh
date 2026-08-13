#!/usr/bin/env bash
# commit-msg-gate.sh — run the commit-message validator against REAL commits.
#
# WHY THIS EXISTS. scripts/validate-commit-msg.sh already refuses every
# fabricated-reviewer trailer and every malformed verdict this repo has on
# record. Nothing ran it on a real commit. Its self-test ran it on fixtures,
# one Go test ran it on the daemon merge path, and three places in
# docs/foundation/project-level/build-practices.md said it ran "via the /check
# flow" — which .claude/commands/check.md never mentioned and no target
# invoked. That is why commits carrying an approval nobody gave landed AFTER
# the rule that refuses them: the rule was written, and then nothing read it.
#
# A doctrine that names a flow, where the flow does not do the thing, is the
# defect. This is the flow doing the thing.
#
# THE SCOPE, AND WHY IT IS NOT THE WHOLE HISTORY. The history already holds
# commits this validator refuses. Measured 2026-08-13: 30 of the last 120.
# It is a sliding window over a moving history, so it drifts both ways; the
# command, not the number, is the thing to keep:
#
#   git log --format=%H -n 120 | while read s; do
#     git log -1 --format=%B "$s" > /tmp/m
#     COMMIT_MSG_CLEANUP=verbatim bash scripts/validate-commit-msg.sh /tmp/m \
#       >/dev/null 2>&1 || echo "$s"
#   done | wc -l
# A gate over all of history would be red on arrival and would stay red, and
# the only way to green it would be to rewrite published commits or to weaken
# the validator. Both are worse than the problem.
#
# So the scope is a ratchet, the same shape as tools/lintreport/allow.txt:
# everything from BASELINE forward must pass, and BASELINE only ever moves
# forward. At the time it was set, every commit in scope already passed — the
# gate went in green over more than forty real commits, not over an empty set
# (the count and the command that re-derives it are on the BASELINE note
# below). A gate whose
# scope is empty cannot fail, and this repo collects that as a defect rather
# than as a passing test.
#
# WHAT IT DOES NOT PROVE. A shell script cannot see whether a reviewer ran. It
# reads what the commit CLAIMS. What it removes is the vague middle: an
# approval nobody gave now has to be written out in the exact shape one grep
# can audit, and the honest "no reviewer was reached" form stays the cheaper
# thing to write.
#
# TWO MODES, because the two gate targets want different answers.
#   --head-only : check the commit just made, and REFUSE it. Costs about a
#                 tenth of a second, so it belongs in the inner loop. It is the
#                 only mode that fails a build, and it is the one place where
#                 failing is fair: the commit is yours, it is the tip, and
#                 amending it costs nothing.
#   (default)   : read every commit from the grandfather baseline forward and
#                 REPORT. It does not fail the build.
#
# WHY THE RANGE ONLY REPORTS, because a gate that cannot fail is normally the
# defect this repo collects. Everything in that range is already committed, and
# most of it arrived by merge from another lane. This project refuses outright
# to amend a commit another lane can see, so there is no legal repair for a bad
# message in there. A gate that refuses what cannot be fixed does not get
# obeyed; it gets deleted. So the range is the ledger — it says how much bad
# message this branch is carrying and names every one — and the tip check is
# the enforcement. Each of the commits that carried an approval nobody gave was
# the tip when it was written. The tip check is where they would have died.
#
# Exit 0 = every commit in scope is well-formed; exit 1 = at least one is not.

set -uo pipefail

# Resolved against this script's own directory, not the working directory, so
# the gate can be pointed at a scratch repository and still find the validator.
# The self-test needs that to build a commit with a bad message without putting
# one in this repository's history.
GATE_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
VALIDATOR="${GATE_DIR}/validate-commit-msg.sh"

# BASELINE — the newest commit whose message this validator refuses. Commits
# from here FORWARD are in scope; this one and everything under it are
# grandfathered.
#
# Moving this forward is how you grandfather a new failure, so treat an edit to
# this line as the thing a reviewer looks at hardest. There is exactly one
# legitimate reason to move it: the branch was rebased and these commits have
# new hashes. Widening it to make a red gate green is the move this gate
# exists to prevent.
#
# It was chosen by walking outward from HEAD and validating each commit the
# range added, stopping at the first refusal. That put it at 44 commits on the
# day it landed — 43 before the gate's own commit — and every one of them
# passed against the validator OF THAT DAY. Re-derive with:
#
#   git rev-list 4102b4e2e582e90ddca141afd0494ff31db187d6..a6c22ee50 | wc -l
#
# Two of them are refused by the validator as it stands now, because a later
# tightening moved the line under commits already written. That is the ratchet
# working: the scope reports them and nothing amends them. The number written
# here used to be 42, which this history does not produce.
#
#   4102b4e2e Merge branch 'work/alpha-integration-merge' into work/kilo-keeper
BASELINE="${COMMIT_MSG_GATE_BASELINE:-4102b4e2e582e90ddca141afd0494ff31db187d6}"

# MAX_COMMITS — an upper bound on the walk, so the inner loop cannot grow
# without limit as the branch does. When it bites, the gate says so out loud
# and names how many it skipped. A cap that trims silently reads as full
# coverage when it is not.
MAX_COMMITS="${COMMIT_MSG_GATE_MAX:-300}"

HEAD_ONLY=0
case "${1:-}" in
  --head-only) HEAD_ONLY=1 ;;
  '') ;;
  *) echo "commit-msg-gate: unknown argument '$1' (expected --head-only or nothing)" >&2; exit 1 ;;
esac

fail_hard() {
  echo "commit-msg-gate: FAIL — $*" >&2
  exit 1
}

[ -x "$VALIDATOR" ] || [ -f "$VALIDATOR" ] || fail_hard "$VALIDATOR is missing"

git rev-parse --verify HEAD >/dev/null 2>&1 || fail_hard "no HEAD to check"

# Choose the scope. No case here is "check nothing".
scope_note=''
# ADVISORY — set when this line of history is outside the ratchet. The gate
# still reads the commits and still says what is wrong with them, and it does
# not fail the build.
#
# WHY THERE IS A SOFT MODE AT ALL, because a gate with one is worth distrusting.
# The ratchet grandfathers everything under one baseline commit. A branch that
# does not descend from that commit cannot be grandfathered by it, so enforcing
# here would hold a lane to a standard its history had no way to meet — and
# measured on the day this landed, several lanes' tip commits fail. Their
# authors cannot repair them either: amending a commit another lane can already
# see is the one thing this project refuses outright. So enforcing would leave
# those lanes with a red inner loop, no legal repair, and one obvious way out,
# which is to delete the gate.
#
# Nothing escapes through here. The moment that work merges into the line that
# carries the baseline, its commits enter the enforced range and the merge
# decision reads every one of them. The soft mode moves WHEN a bad message is
# refused. It does not decide whether it is.
ADVISORY=0

if [ "$HEAD_ONLY" -eq 1 ]; then
  # `git rev-parse HEAD`, NOT `git rev-list --no-merges -n 1 HEAD`. The second
  # spelling returns the newest non-merge ANCESTOR when the tip is a merge, so
  # on a merge branch it silently judged a commit the author did not write and
  # cannot amend, while printing "tip only". Merge subjects are recognised by
  # the validator now, so there is nothing left to skip past.
  commits=$(git rev-parse HEAD)
  scope_note='the commit just made; the merge decision reads the whole range'
  if ! git cat-file -e "${BASELINE}^{commit}" 2>/dev/null; then
    ADVISORY=1
    scope_note="the grandfather baseline ${BASELINE:0:9} is not in this repository, so this is ADVICE, not a verdict"
  elif ! git merge-base --is-ancestor "$BASELINE" HEAD 2>/dev/null; then
    ADVISORY=1
    scope_note="this history does not descend from the grandfather baseline ${BASELINE:0:9}, so this is ADVICE, not a verdict"
  fi
else
  # The ledger. It reports and never refuses, so it is advisory by construction.
  ADVISORY=1
  if ! git cat-file -e "${BASELINE}^{commit}" 2>/dev/null; then
    commits=$(git rev-parse HEAD)
    scope_note="the grandfather baseline ${BASELINE:0:9} is not in this repository, so only the tip was read"
  elif ! git merge-base --is-ancestor "$BASELINE" HEAD 2>/dev/null; then
    commits=$(git rev-parse HEAD)
    scope_note="this history does not descend from the grandfather baseline ${BASELINE:0:9}, so only the tip was read"
  else
    commits=$(git rev-list "${BASELINE}..HEAD")
    if [ -z "$commits" ]; then
      commits=$(git rev-parse HEAD)
      scope_note="HEAD is the grandfather baseline, so the tip itself was read"
    fi
  fi
fi

total=0
[ -n "$commits" ] && total=$(grep -c . <<<"$commits")

skipped=0
if [ "$total" -gt "$MAX_COMMITS" ]; then
  skipped=$(( total - MAX_COMMITS ))
  commits=$(head -n "$MAX_COMMITS" <<<"$commits")
  total="$MAX_COMMITS"
fi

if [ "$total" -eq 0 ]; then
  fail_hard "no commit was checked; a gate that checks nothing cannot fail and is not evidence"
fi

msgfile=$(mktemp)
report=$(mktemp)
trap 'rm -f "$msgfile" "$report"' EXIT

rejected=0
while read -r sha; do
  [ -n "$sha" ] || continue
  git log -1 --format=%B "$sha" >"$msgfile"
  # The validator runs here with its cleanup mode pinned to `verbatim`, and
  # that pin is not a preference. What `git log --format=%B` hands
  # back is the message git ALREADY STORED. Every cleanup rule ran before the
  # commit was written, so there is nothing left for a cleanup mode to remove
  # and a `#` line in here is text a reader and the audit grep both see.
  #
  # Without this the validator resolved the mode from `commit.cleanup`, which
  # belongs to the commit that has not been made yet. Measured on a repository
  # with `commit.cleanup=strip` set: a real commit carrying
  # `# Reviewed-By: agent-reviewer` in its stored message was reported as 0
  # rejected, and the same commit with `COMMIT_MSG_CLEANUP=verbatim` was
  # rejected. One setting, in a config file nobody reads at commit time, turned
  # the whole gate off for the fabricated trailers it exists to find.
  if ! COMMIT_MSG_CLEANUP=verbatim bash "$VALIDATOR" "$msgfile" >"$report" 2>&1; then
    rejected=$(( rejected + 1 ))
    echo "commit-msg-gate: REJECTED ${sha:0:9}  $(git log -1 --format=%s "$sha")" >&2
    sed 's/^/    /' "$report" >&2
  fi
done <<<"$commits"

[ -n "$scope_note" ] && echo "commit-msg-gate: NOTE — ${scope_note}"
[ "$skipped" -gt 0 ] && echo "commit-msg-gate: NOTE — capped at ${MAX_COMMITS} commits; ${skipped} older in-scope commits were NOT checked"

echo "commit-msg-gate: ${total} commits checked, ${rejected} rejected"

if [ "$rejected" -gt 0 ]; then
  if [ "$ADVISORY" -eq 1 ]; then
    echo "commit-msg-gate: ADVICE — ${rejected} commit(s) above carry a message this" >&2
    echo "  repository refuses. Nothing fails here: these commits are already" >&2
    echo "  written, and amending one another lane can see is the thing this" >&2
    echo "  project refuses outright. The number is the point — it is what this" >&2
    echo "  branch is carrying. A new commit is refused at the moment it is" >&2
    echo "  made, which is the only moment it is free to fix." >&2
    exit 0
  fi
  echo "commit-msg-gate: FAIL — fix the commit message, or record the honest" >&2
  echo "  no-reviewer form. Do NOT move BASELINE in this script to make this" >&2
  echo "  green; that is the one edit this gate exists to catch." >&2
  exit 1
fi
exit 0
