#!/usr/bin/env bash
# commit-msg-gate.sh — run the commit-message validator against REAL commits.
#
# WHY THIS EXISTS. The validator already refuses every fabricated-reviewer
# trailer and every malformed verdict this repo has on record. Nothing ran it on
# a real commit. Its own tests ran it on fixtures,
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
#   go build -o /tmp/hk ./cmd/harmonik
#   git log --format=%H -n 120 | while read s; do
#     git log -1 --format=%B "$s" > /tmp/m
#     COMMIT_MSG_CLEANUP=verbatim /tmp/hk commit-msg validate /tmp/m \
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
# WHAT IT DOES NOT PROVE. Nothing here can see whether a reviewer ran. It
# reads what the commit CLAIMS. What it removes is the vague middle: an
# approval nobody gave now has to be written out in the exact shape one grep
# can audit, and the honest "no reviewer was reached" form stays the cheaper
# thing to write.
#
# TWO MODES, and ONLY ONE OF THEM IS WIRED.
#   --head-only : check the commit just made, and REFUSE it. Reading the message
#                 costs about a hundredth of a second. It is the only mode that
#                 fails a build, and it is the one place where failing is fair:
#                 the commit is yours, it is the tip, and amending it costs
#                 nothing. `make full` calls this, through commit-msg-check, and
#                 it is the only caller in the tree.
#   (default)   : read every commit from the grandfather baseline forward and
#                 REPORT. It does not fail the build. NO TARGET RUNS THIS. It was
#                 dropped from `make full` on 2026-08-23 — measured there, it read
#                 236 commits in 47 seconds, named 109 rejections and exited 0
#                 anyway. It stays callable by hand for exactly that report.
#
# WHY THE RANGE ONLY REPORTED, because a gate that cannot fail is normally the
# defect this repo collects. Everything in that range is already committed, and
# most of it arrived by merge from another lane. This project refuses outright
# to amend a commit another lane can see, so there is no legal repair for a bad
# message in there. A gate that refuses what cannot be fixed does not get
# obeyed; it gets deleted. So the tip check is the whole of the enforcement now.
# Each of the commits that carried an approval nobody gave was the tip when it
# was written. The tip check is where they would have died — on the one push
# that carried them as the tip, and nowhere else.
#
# Exit 0 = every commit in scope is well-formed; exit 1 = at least one is not.

set -uo pipefail

# Resolved against this script's own directory, not the working directory, so
# the gate can be pointed at a scratch repository and still find the validator.
# The self-test needs that to build a commit with a bad message without putting
# one in this repository's history.
GATE_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
HARMONIK_REPO="$(cd -- "${GATE_DIR}/.." && pwd)"

# THE VALIDATOR IS A GO COMMAND NOW, not the shell script this gate used to
# call. The rules live in internal/commitmsg, which is pure, and
# `harmonik commit-msg validate` is the seam that resolves the two things a pure
# function cannot: the reviewer names this repository tracks at HEAD, and the
# cleanup mode git will apply. The contract this loop depends on is unchanged —
# exit 0 clean, exit 1 with every problem numbered on stderr.
#
# BUILT ONCE, HERE, rather than run through `go run` per commit. This loop reads
# up to 300 commits, and `go run` pays link-and-start on every one of them.
#
# --repo pins the reviewer set and the git config to THIS repository. Without it
# the command would read them from the working directory, which on every scratch
# path is the repository under test rather than the one that ships the reviewer
# skills, and an APPROVE naming a real reviewer would then be refused for naming
# a name the scratch repo does not have.
#
# COMMIT_MSG_VALIDATOR names an already-built binary and skips the build. The
# self-test drives this gate fourteen times, so without it fourteen relinks are
# paid to answer one question. Nothing else changes: the same binary runs the
# same verb, and an unset variable still builds, so a caller that knows nothing
# about this is not asked to.
VALIDATOR_DIR=''
VALIDATOR="${COMMIT_MSG_VALIDATOR:-}"
if [ -z "$VALIDATOR" ]; then
  VALIDATOR_DIR="$(mktemp -d)"
  VALIDATOR="${VALIDATOR_DIR}/commit-msg-validator"
  trap 'rm -rf -- "$VALIDATOR_DIR"' EXIT
  if ! ( cd "$HARMONIK_REPO" && go build -o "$VALIDATOR" ./cmd/harmonik ); then
    echo "commit-msg-gate: FAIL — could not build the commit-message validator from ${HARMONIK_REPO}" >&2
    exit 1
  fi
fi

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

[ -x "$VALIDATOR" ] || fail_hard "the commit-message validator did not build at $VALIDATOR"

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
# WHAT THE SOFT MODE COSTS, now that the range mode is unwired. It used to be
# fair to say nothing escaped: a bad message went advisory on its own lane and
# the merge decision read the whole range once that lane landed. Since
# 2026-08-23 no target runs the range, so there is no second reading. A commit
# whose lane does not descend from the baseline is reported here and refused
# nowhere. That is a real hole, not a deferral.
ADVISORY=0

if [ "$HEAD_ONLY" -eq 1 ]; then
  # `git rev-parse HEAD`, NOT `git rev-list --no-merges -n 1 HEAD`. The second
  # spelling returns the newest non-merge ANCESTOR when the tip is a merge, so
  # on a merge branch it silently judged a commit the author did not write and
  # cannot amend, while printing "tip only". Merge subjects are recognised by
  # the validator now, so there is nothing left to skip past.
  commits=$(git rev-parse HEAD)
  scope_note='the commit just made, and nothing before it'
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
trap 'rm -f "$msgfile" "$report"; if [ -n "$VALIDATOR_DIR" ]; then rm -rf -- "$VALIDATOR_DIR"; fi' EXIT

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
  if ! COMMIT_MSG_CLEANUP=verbatim "$VALIDATOR" commit-msg validate --repo "$HARMONIK_REPO" "$msgfile" >"$report" 2>&1; then
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
