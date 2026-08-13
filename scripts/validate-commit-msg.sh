#!/usr/bin/env bash
# scripts/validate-commit-msg.sh — commit-message validator (run via the
# agent-driven /check flow; git hooks are retired).
#
# Usage: validate-commit-msg.sh <commit-msg-file>
#
# Validates:
#   1. Subject line matches Conventional Commits format (closed type set).
#   2. Subject length ≤72 characters and no trailing period.
#   3. Non-trivial commits carry Reviewed-By: and Review-Verdict: trailers.
#   4. Review-Verdict: value is well-formed JSON matching agent-reviewer
#      schema v1: schema_version=1, verdict ∈ {APPROVE, REQUEST_CHANGES,
#      NOT_REVIEWED} or config-reviewer schema v1: verdict ∈ {CLEAN,
#      DRIFT_MINOR, DRIFT_MAJOR}. `notes` is required and must not be empty.
#
# The honest no-reviewer form (AGENTS.md, the `git commit -F` rule):
#
#   Reviewed-By: none — no reviewer was reached for this commit
#   Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED",
#                    "flags": ["no-reviewer-reached"], "notes": "…"}
#
# The rule says a commit whose reviewer could not be reached records that fact
# and lands anyway, and that such a trailer carries NO verdict of APPROVE.
# NOT_REVIEWED is that record. It passes. Before this was here, the honest
# sentence was the ONE shape the validator rejected, which pushed the author
# toward writing down an approval nobody gave.
#
# A NOT_REVIEWED verdict must not name a reviewer this repo has, ANYWHERE in
# its `Reviewed-By:` text. The verdict says no reviewer was reached; a line
# reading `agent-reviewer` says one did. Two commits on this branch carry that
# pair, and they poison the one audit the approval rules were built to make
# possible — a grep for `agent-reviewer` over the history reads them as
# reviewed work. So the name is what is searched for, on a word boundary,
# across every `Reviewed-By:` line the message carries: punctuation around it
# and a second trailer line beside it are both ways of answering that grep.
# The refusal points at the honest form above.
#
# An APPROVE (and its config-reviewer twin CLEAN) is held to more than the
# other verdicts, so that the honest form is always the cheaper thing to write:
#   - Reviewed-By must be exactly the name of a reviewer skill this repo has
#     and that git tracks (`.claude/skills/*reviewer*/SKILL.md`). Nothing may
#     follow the name; detail about the run goes in the verdict's `notes`.
#   - Reviewed-By must not name the author ("self").
#   - The JSON must carry the `flags` key that the reviewer skill always emits.
# None of this proves a reviewer ran — a shell script cannot see session state,
# and it never could. What it does is remove the vague middle ground: a false
# APPROVE now has to name agent-reviewer outright, which one grep can audit.
#
# Trivial-commit bypass: add the trailer `Trivial: true` to the commit
# message to skip the Reviewed-By / Review-Verdict requirement.
# Use ONLY for typos, whitespace fixes, and obvious one-liners — the
# agent-reviewer is still required for all other commits per build-practices.md.
#
# A MERGE is not a bypass. Git writes the subject of a merge and of a
# `--fixup`, so neither can match Conventional Commits and the subject rules
# stand aside for both. The trailer rules do not: a merge LANDS, on the
# integration branch and on main, so it answers for its content like any other
# commit. A merge that needs no review says so — `Trivial: true`, or the
# NOT_REVIEWED form above. A `fixup!` / `squash!` commit is pre-rebase scratch
# that never lands under that subject, so it keeps the trailer exemption — with
# the caveat that "never lands" is a claim about a rebase somebody still has to
# run. Nothing here can check that `--autosquash` happened, so a `fixup!` that
# is pushed unsquashed carries whatever trailer it likes, including none.
#
# Exit 0 = OK; exit 1 = validation failure. Every problem found is reported, not
# just the first, and each is numbered on its own line.

set -uo pipefail

ERRORS=()
ERROR_COUNT=0

err() {
  ERROR_COUNT=$(( ERROR_COUNT + 1 ))
  ERRORS+=( "  [${ERROR_COUNT}] $*" )
}

# known_reviewers — the reviewer identities an APPROVE / CLEAN may name.
#
# Read off the filesystem rather than hard-coded, so adding a reviewer skill is
# enough and this list cannot rot behind it. Today that is agent-reviewer and
# agent-config-reviewer. If the skills directory cannot be found (the script is
# callable from anywhere, including a bare worktree), fall back to those two
# names rather than to "anything goes" — an unreadable directory must not turn
# into a free pass.
known_reviewers() {
  local root skills found=""
  root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel 2>/dev/null || true)"
  [[ -z "$root" ]] && root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." 2>/dev/null && pwd || true)"
  skills="${root}/.claude/skills"
  # A reviewer identity has to be one git tracks. The directory alone is not
  # enough: this reads the working tree, so an untracked `mkdir` beside the real
  # skills would mint a name the validator then trusts, and minting a reviewer
  # is the exact act the approval rule exists to prevent. Requiring the skill to
  # be tracked means a new reviewer arrives the way anything else does — as a
  # diff somebody can decline.
  if [[ -n "$root" && -d "$skills" ]]; then
    local d name
    for d in "$skills"/*reviewer*; do
      [[ -d "$d" ]] || continue
      name="$(basename "$d")"
      # A reviewer name is a plain identifier. Without this, a directory named
      # with glob metacharacters walks straight into the pathspec below, and a
      # git pathspec GLOBS: an empty, untracked `mkdir '*reviewer'` matched the
      # real agent-reviewer's tracked SKILL.md and minted `*reviewer` as a
      # trusted identity. That is the mkdir-mints-a-reviewer act this tracked
      # check was added to stop, walking in through the check itself.
      [[ "$name" =~ ^[A-Za-z0-9._-]+$ ]] || continue
      # Read HEAD, not the index. `git ls-files` answers "is it staged", and the
      # same agent writes the index and the commit message in one operation —
      # so `git add` alone minted a trusted reviewer, with no commit and no
      # review. Asking HEAD means the skill is in the history somebody already
      # accepted. `:(literal)` is unnecessary against a HEAD path, which does
      # not glob; the name-shape guard above is what closes that door now.
      git -C "$root" cat-file -e "HEAD:.claude/skills/${name}/SKILL.md" 2>/dev/null || continue
      found+="${name}"$'\n'
    done
  fi
  if [[ -z "$found" ]]; then
    found=$'agent-reviewer\nagent-config-reviewer\n'
  fi
  printf '%s' "$found"
}

# check_approval_identity — the extra bar an APPROVE / CLEAN has to clear.
#
# Reads the global REVIEWED_BY (the whole `Reviewed-By:` line). An approval must
# name one of the reviewer skills this repo has, and NOTHING else on the line —
# `agent-reviewer` passes, `agent-reviewer (codex harness)` and `Codex reviewer`
# do not. Detail about the run belongs in the verdict's `notes`. The
# parenthetical used to pass, which meant `agent-reviewer (myself)` read as an
# approval by a real reviewer. And the value must not name the author: the rule
# says do not author your own approval, so a value that says "self" is refused
# here.
#
# This proves the name is a real reviewer. It cannot prove that reviewer ran.
check_approval_identity() {
  local verdict_name="$1" value base known matched=false
  value="${REVIEWED_BY#Reviewed-By:}"
  value="${value#"${value%%[![:space:]]*}"}"   # trim leading space
  value="${value%"${value##*[![:space:]]}"}"   # trim trailing space

  if [[ -z "$value" ]]; then
    err "the ${verdict_name} verdict needs a 'Reviewed-By:' value naming the reviewer that ran."
    return
  fi

  if printf '%s\n' "$value" | grep -qiE '(^|[^a-z])self([^a-z]|$)'; then
    err "the ${verdict_name} verdict must not be self-authored (Reviewed-By: ${value})."
    err "  The rule is: quote the reviewer's verdict, never author your own approval."
    err "  If no reviewer was reached, record that instead: \"verdict\": \"NOT_REVIEWED\"."
    return
  fi

  # The whole value must be the reviewer's name, with nothing after it.
  #
  # This used to drop one trailing parenthetical before comparing, so that
  # `agent-reviewer (codex harness)` could carry a note about which harness ran.
  # The affordance cost more than it bought. Everything inside the parentheses
  # was free text nobody checked, which meant the two rules above could be
  # answered and defeated in the same line: `agent-reviewer (myself)` passed the
  # self-authorship rule that exists to stop exactly that, and
  # `agent-reviewer (Kierkegaard)` read to a human as an attribution to
  # Kierkegaard while satisfying a check that asked for a reviewer this repo has.
  # Both were measured passing before this changed.
  #
  # A name with nothing after it is worth more than a name with a comment after
  # it: one grep audits every approving commit, and there is no room left in
  # the line to say something the audit cannot see. Grep it case-insensitively
  # — the comparison below folds case so this script can run under the bash 3.2
  # that ships with macOS, so `AGENT-REVIEWER` is accepted and a case-sensitive
  # audit would miss it. Detail about the run belongs in
  # the verdict's own `notes`, where it is inside the JSON the reviewer emits
  # rather than beside it.
  base="$value"

  # Lower-cased with tr, not with ${x,,}: this script has to run under the
  # bash 3.2 that ships with macOS as well as a modern one.
  local base_lc known_lc
  base_lc="$(printf '%s\n' "$base" | tr '[:upper:]' '[:lower:]')"
  while IFS= read -r known; do
    [[ -z "$known" ]] && continue
    known_lc="$(printf '%s\n' "$known" | tr '[:upper:]' '[:lower:]')"
    if [[ "$base_lc" == "$known_lc" ]]; then
      matched=true
      break
    fi
  done < <(known_reviewers)

  if [[ "$matched" != true ]]; then
    err "the ${verdict_name} verdict must name a reviewer skill this repo has; got '${value}'."
    err "  Known reviewers: $(known_reviewers | tr '\n' ' ')"
    err "  Write the name alone. A qualifier after it is no longer accepted:"
    err "  put which harness ran, or when, in the verdict's own \"notes\" field."
    err "  If no reviewer was reached, record that instead: \"verdict\": \"NOT_REVIEWED\"."
  fi
}

# check_no_reviewer_named — the bar a NOT_REVIEWED verdict has to clear.
#
# This is the mirror image of check_approval_identity, and calling that one
# from here would be the wrong fix: it asserts the value MUST be a known
# reviewer, so it refuses `Reviewed-By: none — no reviewer was reached`, which
# is the exact sentence this repo asks an author to write.
#
# What that costs is a CLASS, not a count: every commit whose Reviewed-By value
# is an honest description of an absent reviewer rather than a name. Measured
# 2026-08-13 over the gate's enforced range, which was 54 commits that day: 22
# of them are refused by that naive tightening, and most carry the honest
# `none — no reviewer was reached` form. Both numbers move with every commit,
# which is why the class is written first and the measurement is dated. Re-read
# it with the range the gate itself uses:
#
#   for s in $(git rev-list "${COMMIT_MSG_GATE_BASELINE}..HEAD"); do
#     git log -1 --format=%B "$s" | grep -m1 '^Reviewed-By:'
#   done
#
# An earlier version of this note also said the tightening refuses HEAD. That
# was true when it was written and is not now — HEAD moves, so a claim pinned
# to it decays into a false statement without anyone touching the line.
#
# What NOT_REVIEWED owes is the opposite promise. The verdict states that no
# reviewer was reached, so the Reviewed-By line must not name one.
#
# WHY THIS SCANS THE WHOLE CAPTURE INSTEAD OF PARSING A VALUE OUT OF IT, and
# this is the part worth reading before you write another check in this pair.
# The first version parsed one value and compared it for equality, the way
# check_approval_identity does. Equality is the right test THERE and the wrong
# test HERE, because the two checks fail in opposite directions:
#
#   - `REVIEWED_BY` is a grep over the message, so it holds EVERY
#     `Reviewed-By:` line, not one. Two lines can never equal a reviewer name.
#     Under APPROVE that refuses the commit, which is safe. Under NOT_REVIEWED
#     the same non-equality ACCEPTED it, so a commit reading
#     `Reviewed-By: none …` followed by `Reviewed-By: agent-reviewer` exited 0
#     — and `git log --grep 'Reviewed-By: agent-reviewer'`, the audit this rule
#     exists to keep honest, counts it.
#   - Equality also means any decoration escapes. `agent-reviewer.`,
#     `,agent-reviewer`, `agent-reviewer [unreachable]` and `(agent-reviewer)`
#     all exited 0, and every one of them still answers that same grep.
#
# So a check that mirrors another one must not borrow its comparison. Work out
# which way each one fails open. Here that means: search all the text for the
# name, rather than parse the text down to a value and demand it match.
#
# THE BOUNDARY IS ASYMMETRIC, AND THE ASYMMETRY IS THE WHOLE POINT. A boundary
# character is required BEFORE the name and NOT after it. That looks like a
# half-finished rule until you check it against the audit, which is the only
# thing that decides it:
#
#     git log --grep 'Reviewed-By: agent-reviewer'
#
# That grep is a SUBSTRING match, so `Reviewed-By: agent-reviewer2` answers it
# and is counted as reviewed work, while `Reviewed-By: xxagent-reviewer` does
# not answer it and is not. A trailing boundary character therefore let four
# shapes through that the audit counts — `agent-reviewer2`, `agent-reviewers`,
# `agent-reviewerx`, `agent-reviewer0` all exited 0 — and a LEADING boundary
# dropped as well would refuse `xxagent-reviewer`, which the audit does not
# count, so refusing it would be a false fail on a name that is not a claim.
# Measured on a scratch repository: of the eight, `git log --grep` returns
# exactly the four with the suffix and none of the four with a prefix.
#
# The test to apply to any future change here is not "is this value a reviewer
# name". It is "would somebody grepping the history count this line as
# reviewed work". Match the grep, in both directions.
#
# Bash glob matching does it, in one pass, with no pipeline: `printf … |
# grep -qE` would add a site of the shape scripts/pipefail-grepq-gate.sh
# refuses, and the one allowed site in this file is justified on the capture
# being a single short line, which is the very claim the duplicate-trailer case
# above disproves.
#
# Only reviewer skills this repo HAS are searched for, the same list an
# approval is checked against.
#
# WHAT THIS DOES TO FREE TEXT, stated as it behaves rather than as it was
# wished. Free text is NOT left alone. The search runs over every
# `Reviewed-By:` line the message carries, case-folded, and asks only for a
# non-alphanumeric character before the name — so a sentence that HAPPENS to
# contain a reviewer's name is refused too. Measured: `none (agent-reviewer
# down)`, `not agent-reviewer`, `x-agent-reviewer` and `AGENT-REVIEWER` are all
# refused, and `git log --grep 'Reviewed-By: agent-reviewer'` counts none of
# them. Those four are OVER-refusals, and they are left standing on purpose:
# they fail in the honest direction, the refusal prints the honest form to
# write instead, and the alternative is a rule that reads intent out of prose —
# which is how `Reviewed-By: agent-reviewer2` got through in the first place.
#
# The plain honest spellings this repo asks for carry no reviewer name at all,
# so none of this reaches them: `none`, `nobody`, `n/a`, `unreachable`,
# `none — no reviewer was reached for this commit`. They pass, and the suite
# pins ten of them. A validator that argued with the wording of an honest
# sentence would teach the author to stop writing it.
check_no_reviewer_named() {
  local hay padded known known_lc matched_name='' wide

  # THIS CHECK DOES NOT READ `REVIEWED_BY`, AND THAT IS DELIBERATE.
  #
  # `REVIEWED_BY` is captured with an ANCHORED `grep -E '^Reviewed-By:'`. The
  # audit it defends is UNANCHORED. So one leading space hid a trailer from the
  # validator while leaving it in plain sight of the audit:
  #
  #     Reviewed-By: none — no reviewer was reached
  #       Reviewed-By: agent-reviewer      <- indented; invisible to ^ anchor
  #
  # That message exited 0 and `git log --grep 'Reviewed-By: agent-reviewer'`
  # counts it. A capture must never be narrower than the audit it defends.
  #
  # WHICH FIX, AND WHY. Two were on the table: widen `REVIEWED_BY` itself, or
  # refuse any message carrying a second `Reviewed-By:` occurrence. Neither was
  # taken. Widening the shared capture would change the two OTHER things that
  # read it — the presence check and check_approval_identity, which demands the
  # value be EXACTLY a reviewer name — so an honest APPROVE that quotes the
  # string `Reviewed-By:` anywhere in its body would suddenly hold two lines,
  # fail that equality, and be refused. Counting occurrences has the same
  # defect and adds a rule about duplication that nobody asked for; a message
  # may legitimately carry two trailers.
  #
  # So the widening is scoped to the check that owes the audit its answer: this
  # one re-derives its own haystack from the whole message, unanchored, exactly
  # as the audit greps it. The anchored capture keeps its narrower meaning for
  # everything else, where narrow fails CLOSED.
  wide="$(printf '%s\n' "$STRIPPED" | grep -E 'Reviewed-By:' || true)"

  # Fold case and flatten the capture to one line, so a name split across two
  # `Reviewed-By:` lines is still one string to search. tr, not ${x,,}: this
  # script has to run under the bash 3.2 that ships with macOS.
  hay="$(printf '%s' "$wide" | tr '[:upper:]' '[:lower:]' | tr '\n' ' ')"

  # Drop the trailer keys themselves. No reviewer this repo has is a substring
  # of `Reviewed-By:` today, so this changes nothing now — it is here so that
  # naming a future skill `by-reviewer` cannot make every commit refuse itself.
  hay="${hay//reviewed-by:/ }"

  # Pad, so a name at the very start or the very end of the text has a boundary
  # character on both sides and needs no second pattern.
  padded=" ${hay} "

  while IFS= read -r known; do
    [[ -z "$known" ]] && continue
    known_lc="$(printf '%s' "$known" | tr '[:upper:]' '[:lower:]')"
    # Leading boundary only. See the asymmetry note above: a trailing one
    # exempts every alphanumeric suffix, and the audit grep does not.
    if [[ "$padded" == *[!a-z0-9]"$known_lc"* ]]; then
      matched_name="$known"
      break
    fi
  done < <(known_reviewers)

  [[ -z "$matched_name" ]] && return

  err "the NOT_REVIEWED verdict must not name a reviewer this repo has; found '${matched_name}'."

  # Quote the author's own lines back, one per line, exactly as they wrote
  # them. This used to print the flattened haystack instead — every
  # `Reviewed-By:` line joined into one lower-cased string with the keys
  # stripped out and the spaces squeezed. Measured on a real commit in this
  # history, that produced `Reviewed-By: agent-reviewer (myself) -> passed
  # agent-reviewer (kierkegaard) -> passed kierkegaard -> refused
  # agent-reviewer`, a sentence that appears nowhere in the message. A refusal
  # that quotes text the author cannot find teaches them nothing.
  local shown_line
  while IFS= read -r shown_line; do
    [[ -z "$shown_line" ]] && continue
    err "  ${shown_line}"
  done <<<"$wide"
  err "  NOT_REVIEWED says no reviewer was reached. Naming one contradicts it,"
  err "  and an audit that greps the history for a reviewer name counts it as reviewed."
  err "  Write the honest form instead, on ONE Reviewed-By line:"
  err "    Reviewed-By: none — no reviewer was reached for this commit"
  err "  If the reviewer DID run and declined, the verdict is REQUEST_CHANGES."
}
MSG_FILE="${1:-}"
if [[ -z "$MSG_FILE" || ! -f "$MSG_FILE" ]]; then
  echo "validate-commit-msg [1]: no commit-message file provided or file not found" >&2
  exit 1
fi

# ── 1. Read the message the way GIT WILL STORE IT ────────────────────────────
#
# This was `grep -v '^#'`, unconditionally, and every check below reads what it
# produces. That made the validator's view of the message differ from the
# commit, in the one direction that matters.
#
# `git commit -F <file>` — the spelling AGENTS.md mandates — gets cleanup mode
# `whitespace`, because git only defaults to `strip` when the message is edited
# in an editor. `whitespace` does NOT remove comment lines. So a `#` line LANDS
# IN THE STORED COMMIT while being invisible to every check here. Measured end
# to end through scripts/commit-msg-gate.sh, on a real commit whose body read
# `Reviewed-By: none — no reviewer was reached` on one line and
# `# Reviewed-By: agent-reviewer` on the next: the gate exited 0, and
# `git log <sha>^! --grep 'Reviewed-By: agent-reviewer'` counted the commit as
# reviewed work. The strip also RELOCATED THE SUBJECT — a message whose real
# first line was `# fix: <137 chars>` had the subject rules applied to line 2,
# so a stored subject of 137 characters passed a 72-character ceiling.
#
# THE MODE THIS ASSUMES, AND WHY IT IS `whitespace`: comments are CONTENT. The
# two ways this script is called agree on that, and the third way does not
# exist here.
#   - scripts/commit-msg-gate.sh hands over an ALREADY-STORED message, read
#     with `git log -1 --format=%B`. Every cleanup rule ran before that commit
#     was written, so a `#` line in it is text a reader and the audit grep both
#     see. The gate says so out loud — it sets `COMMIT_MSG_CLEANUP=verbatim` —
#     because otherwise this script reads `commit.cleanup`, which describes the
#     NEXT commit and not the one being judged. With that setting on `strip`,
#     the gate stripped the fabricated trailers it exists to find.
#   - A pre-commit check on a message file bound for `git commit -F` gets
#     `whitespace` from git, as above.
#   - The editor path, where `strip` is right and git's own instructional
#     comments genuinely are removed, reaches a validator through a commit-msg
#     hook. This repo's git hooks are retired and validation is agent-driven,
#     so nothing here is on that path — but a caller that is can say so.
#
# The assumption is neither silent nor fixed. `commit.cleanup` is read when it
# is set, `COMMIT_MSG_CLEANUP` overrides both, and `COMMIT_MSG_EXPLAIN=1` makes
# this script print which mode it resolved and where the value came from. That
# is what makes the assumption checkable rather than asserted.
#
# ONLY `strip` REMOVES ANYTHING. `whitespace`, `verbatim` and `scissors` all
# keep comment lines, so all three resolve to the same behaviour here.
#
# SCISSORS IS DELIBERATELY NOT MODELLED FURTHER. `--cleanup=scissors` drops
# everything below git's scissors line, so modelling it means DELETING text
# before the checks run — and a scissors pattern one character off deletes text
# git would have kept, which hides a fabricated trailer.
#
# ON THE TWO PATHS THIS SCRIPT SERVES, NOT CUTTING IS EXACT. Git truncates at
# the scissors line only when the message is to be EDITED. The gate hands over
# a message git has already stored, and `git commit -F` does not open an
# editor, so on both of them git cuts nothing and reading the whole message is
# what git stored.
#
# The claim stops there. On an editor path git WOULD cut, and a trailer below
# the scissors line would be read here and dropped by git — which turns a
# missing-trailer refusal into a pass. That is an under-report, so "not cutting
# can only over-report" is not true in general and is not claimed. Nothing in
# this repo sets scissors and no caller here is on an editor path. A caller
# that is must model the cut before it calls this script.
CLEANUP_MODE="${COMMIT_MSG_CLEANUP:-}"
CLEANUP_SOURCE='the default for git commit -F, which this repo mandates'
if [[ -n "$CLEANUP_MODE" ]]; then
  CLEANUP_SOURCE='COMMIT_MSG_CLEANUP'
else
  CLEANUP_MODE="$(git config --get commit.cleanup 2>/dev/null || true)"
  [[ -n "$CLEANUP_MODE" ]] && CLEANUP_SOURCE='git config commit.cleanup'
fi
# `default` is git's word for "strip if an editor was used, whitespace
# otherwise". Both callers here are the non-editor ones, so it means
# whitespace.
if [[ -z "$CLEANUP_MODE" || "$CLEANUP_MODE" == "default" ]]; then
  CLEANUP_MODE=whitespace
fi

# The comment marker is git's to choose, not this script's to assume.
# `core.commentString` (git 2.45+) wins over `core.commentChar`. A hard-coded
# `#` is wrong in BOTH directions against a repo that sets either one: it drops
# lines git stores, and it keeps lines git drops.
COMMENT_PREFIX="$(git config --get core.commentString 2>/dev/null || true)"
[[ -z "$COMMENT_PREFIX" ]] && COMMENT_PREFIX="$(git config --get core.commentChar 2>/dev/null || true)"
[[ -z "$COMMENT_PREFIX" ]] && COMMENT_PREFIX='#'
# `auto` asks git to pick a marker that begins NO line of the message. Under
# `auto`, therefore, no line of the author's own text is a comment — that is
# what the setting guarantees — so stripping any line would delete text git
# stores. An empty marker means strip nothing.
[[ "$COMMENT_PREFIX" == "auto" ]] && COMMENT_PREFIX=''

if [[ -n "${COMMIT_MSG_EXPLAIN:-}" ]]; then
  echo "validate-commit-msg: cleanup mode '${CLEANUP_MODE}' from ${CLEANUP_SOURCE}; comment marker '${COMMENT_PREFIX:-<none>}'" >&2
fi

# Matched with a bash glob against a QUOTED prefix, not with a regex. The
# marker is whatever git config says, so it can be `;`, `|`, `$` or `//`, and
# every one of those means something else to grep -E. Git's own test is the
# same one: a line is a comment when it BEGINS with the marker, with no leading
# whitespace allowed.
if [[ "$CLEANUP_MODE" == "strip" && -n "$COMMENT_PREFIX" ]]; then
  STRIPPED=''
  while IFS= read -r msg_line || [[ -n "$msg_line" ]]; do
    [[ "$msg_line" == "$COMMENT_PREFIX"* ]] && continue
    STRIPPED+="${msg_line}"$'\n'
  done <"$MSG_FILE"
  STRIPPED="${STRIPPED%$'\n'}"
else
  STRIPPED="$(cat "$MSG_FILE")"
fi

# ── 2. Extract subject line (first non-blank line) ───────────────────────────
SUBJECT="$(printf '%s\n' "$STRIPPED" | awk 'NF{print;exit}')"

# ── 3. Git-written subjects ──────────────────────────────────────────────────
# Git writes a merge subject and `git commit --fixup` writes a fixup subject.
# Neither is Conventional Commits and neither ever will be, so these are
# recognised BEFORE the subject rules rather than after them.
#
# It used to sit below those rules, where it exempted the trailers and nothing
# else. Every merge commit therefore failed on the subject format, and the only
# way to keep the validator usable was to hide merges from it — which meant a
# merge could carry any message at all, including an approval nobody gave, and
# nothing ever read it. Recognising the shape first is what lets a merge be
# checked instead of skipped.
#
# A merge and a fixup are told apart here because they earn different things.
# Both are exempt from the three subject rules below, for the same reason: an
# author did not write either subject. Only the fixup is exempt from the
# trailer rules. A `fixup!` / `squash!` commit is pre-rebase scratch that is
# folded away and never lands under that subject; a merge lands on the
# integration branch and on main and carries other people's work with it.
#
# The single flag that used to stand for both was folded into the trivial
# bypass, so the whole trailer block — presence, JSON, reviewer identity, the
# verdict enum, the refusal of BLOCK — was skipped for every merge. Measured:
# `Merge branch 'work/evil' into main` with a fabricated `Reviewed-By:` and an
# APPROVE exited 0 and said nothing, while the same trailers under a `feat(…)`
# subject were refused. Worse, the bypass reads subject TEXT and not parent
# count, so a merge whose subject was rewritten by hand already got the full
# check and only one keeping git's default subject slipped past.
IS_MERGE=false
if grep -qE '^Merge ' <<<"$SUBJECT"; then
  IS_MERGE=true
fi

IS_FIXUP=false
if grep -qE '^(fixup!|squash!) ' <<<"$SUBJECT"; then
  IS_FIXUP=true
fi

# The three subject-format rules below stand aside for both shapes.
SUBJECT_IS_GIT_WRITTEN=false
if [[ "$IS_MERGE" == "true" || "$IS_FIXUP" == "true" ]]; then
  SUBJECT_IS_GIT_WRITTEN=true
fi

# ── 3. Conventional Commits subject validation ────────────────────────────────
# Pattern per bead hk-kv7fe: type[(scope)]: description
# Scope is restricted to lower-case alphanumerics, commas, hyphens.
# Breaking-change suffix (!) allowed per CC spec even if not listed in bead.
# Types (closed set per build-practices.md §Decisions, "Types (closed set)"):
#   feat fix refactor test docs chore spec build perf
# 9 types only; ci/revert/style are NOT canonical here. Update both this regex
# AND build-practices.md if the set ever expands.
CC_PATTERN='^(feat|fix|refactor|test|docs|chore|spec|build|perf)(\([a-z0-9,:-]+\))?(!)?: .+'
if [[ "$SUBJECT_IS_GIT_WRITTEN" == "false" ]] && ! grep -qE "$CC_PATTERN" <<<"$SUBJECT"; then
  err "subject does not match Conventional Commits format."
  err "  Expected: <type>[(<scope>)][!]: <description>"
  err "  Allowed types: feat fix refactor test docs chore spec build perf"
  err "  Scope (if present) must be lowercase alphanumeric + commas/hyphens/colons."
  err "  Got: $SUBJECT"
fi

# ── 4. Subject length ─────────────────────────────────────────────────────────
SUBJECT_LEN="${#SUBJECT}"
if [[ "$SUBJECT_IS_GIT_WRITTEN" == "false" ]] && (( SUBJECT_LEN > 72 )); then
  err "subject line is ${SUBJECT_LEN} chars; max is 72."
  err "  Got: $SUBJECT"
fi

# ── 5. Trailing-period check ──────────────────────────────────────────────────
if [[ "$SUBJECT_IS_GIT_WRITTEN" == "false" ]] && grep -qE '\.$' <<<"$SUBJECT"; then
  err "subject must not end with a period."
fi

# ── 6. Trivial-bypass detection ───────────────────────────────────────────────
# If the message contains `Trivial: true` anywhere in the trailer block,
# skip the Reviewed-By / Review-Verdict requirement.
IS_TRIVIAL=false
if grep -qE '^Trivial: true$' <<<"$STRIPPED"; then
  IS_TRIVIAL=true
fi
# A `fixup!` / `squash!` commit is scratch. It is written to be folded into
# another commit by `git rebase --autosquash` and it does not survive that, so
# there is nothing for a reviewer to be quoted about. The exemption rests on
# that rebase being run, which this script cannot see and does not check: a
# `fixup!` pushed unsquashed is exempt from every trailer rule below. That is
# accepted, not overlooked. A MERGE is not scratch and is deliberately absent
# from this line: it lands, so it answers for itself.
if [[ "$IS_FIXUP" == "true" ]]; then
  IS_TRIVIAL=true
fi

# ── 8. Reviewed-By + Review-Verdict trailer validation ───────────────────────
if [[ "$IS_TRIVIAL" == "false" ]]; then
  REVIEWED_BY="$(printf '%s\n' "$STRIPPED" | grep -E '^Reviewed-By:' || true)"
  REVIEW_VERDICT_LINE="$(printf '%s\n' "$STRIPPED" | grep -E '^Review-Verdict:' || true)"

  if [[ -z "$REVIEWED_BY" ]]; then
    err "missing required trailer 'Reviewed-By:' on a non-trivial commit."
    err "  Add 'Trivial: true' trailer to bypass for typos/whitespace fixes."
  fi

  if [[ -z "$REVIEW_VERDICT_LINE" ]]; then
    err "missing required trailer 'Review-Verdict:' on a non-trivial commit."
    err "  Add 'Trivial: true' trailer to bypass for typos/whitespace fixes."
  fi

  # Only validate JSON structure if the trailer is present.
  if [[ -n "$REVIEW_VERDICT_LINE" ]]; then
    # ── 9. JSON well-formedness + required fields ─────────────────────────
    VERDICT_JSON="${REVIEW_VERDICT_LINE#Review-Verdict: }"
    VERDICT_JSON="${VERDICT_JSON#Review-Verdict:}"  # handle no-space variant

    # Parse JSON using jq (preferred) or Python fallback.
    #
    # Four fields come back on one line, separated by tabs:
    #   1 schema_version  — the value as text; __ABSENT__ / __EMPTY__
    #   2 verdict         — the value as text; __ABSENT__ / __NULL__ / __EMPTY__
    #   3 notes state     — ok | empty | badtype | absent
    #   4 flags state     — array | notarray | absent
    #
    # FIELDS 1 AND 2 ARE FREE TEXT, and the earlier note here said they were
    # not. They hold whatever the author put in the JSON. Fields 3 and 4 are
    # closed word sets, so `notes` and `flags` text cannot reach the record —
    # but a tab, a newline, a carriage return or a backslash in a
    # schema_version or a verdict can, and a raw tab shifts every field after
    # it. jq's `@tsv` escapes those four characters, so the jq path held. The
    # fallback joined the fields with a bare tab and did not. Measured with jq
    # hidden from PATH: a schema_version of "1<TAB>APPROVE<TAB>ok<TAB>array"
    # carried a BLOCK verdict past every check and exited 0, while the same
    # message with jq present exited 1.
    #
    # ESCAPING THAT LIST WAS NOT ENOUGH, and the way it failed is worth more
    # than the fix. The list was copied from what `@tsv` was believed to
    # escape. Measured on jq 1.7.1, byte by byte over the control range,
    # `@tsv` escapes backslash, tab, newline, carriage return AND NUL (as
    # `\0`), and passes every other control byte through raw. NUL was the one
    # left out, and NUL is the one byte bash's command substitution DROPS. So
    # a verdict of `APPROVE<NUL>` came back from the fallback as `APPROVE`,
    # and a schema_version of `1<NUL>` as `1`: exit 0 on the fallback, exit 1
    # on jq, and under the bash 3.2 that ships with macOS not even a warning.
    # A verdict the Go reader in internal/workspace/reviewverdict.go will not
    # read as APPROVE was accepted as one, beside `Reviewed-By: agent-reviewer`.
    #
    # SO THE RULE IS NO LONGER "ESCAPE THE RIGHT LIST". A control byte has no
    # business in a schema version or a verdict word, and every attempt to
    # carry one is an attempt to move the field boundaries. Both parsers now
    # REFUSE the class: a schema_version or a verdict whose decoded value holds
    # any C0 byte or DEL is reported as `__CONTROL__`, whatever the byte is.
    # That is stronger than a longer escape list, because it does not have to
    # be kept in step with a jq release: no control byte reaches the join at
    # all, so a change in what `@tsv` escapes cannot reopen this.
    #
    # THE ESCAPING STAYS, AND IT IS NOT DEAD. `@tsv` still applies to the jq
    # side and the fallback still mirrors backslash, tab, newline and carriage
    # return. A backslash is not a control byte, so that arm is live on every
    # value that carries one. The three control arms cannot fire while the
    # refusal above holds — they are kept because jq applies them
    # unconditionally, so a fallback that dropped them would turn any
    # regression in the refusal into a silent column shift on ONE parser and
    # not the other, which is the exact shape that let a BLOCK verdict land.
    #
    # NO FIELD MAY BE EMPTY, and this is load-bearing rather than tidy. The
    # record is read back with `IFS=$'\t' read`, and a TAB IS IFS WHITESPACE:
    # bash strips a leading run of it and collapses consecutive ones. So an
    # empty column did not read back as an empty column — it vanished, and
    # every field after it shifted one place left. Measured on a verdict with
    # no `schema_version` key: schema_version read the VERDICT, verdict read
    # the notes state, notes read the flags state, and the validator refused
    # the commit while naming three fields that were all fine. Measured again
    # on `{"verdict": ""}`, after the first column had its sentinel and the
    # second did not: the same shift, reported as an unreadable notes state
    # and an unknown verdict value `ok`. Both failed closed, which is why a
    # green suite never saw either; both told the author to fix the wrong
    # field. The two free-text fields therefore carry a sentinel for every way
    # they can be empty, and the two state fields are word sets that are never
    # empty. Together that is what keeps the record aligned.
    #
    # NO DIFFERENCE IN VERDICT REMAINS BETWEEN THE TWO PARSERS on any payload
    # the suite drives. ONE DIFFERENCE IN WORDING DOES, and it is named here
    # rather than rounded off: on a truncated document jq says it could not
    # parse the JSON and Python says which character it stopped at. Both refuse
    # it, so the verdict agrees; the text does not, and the battery holds that
    # one payload to the same verdict instead of the same words. Parse-error
    # text belongs to the parser that emitted it.
    #
    # Say "verdict" and not "difference" in a sentence like this one. The
    # earlier note here claimed a smaller set of differences than was live
    # while three more were open, and the sentence that replaced it overshot in
    # the other direction — it read as though the parsers now agreed on every
    # byte, which the suite's own exit-status-only payload contradicts. The
    # three that were live then, all found by reading the two programs against
    # each other rather than by a failing test:
    #
    #   - `"schema_version": 1e0` — jq renders it `1` and ACCEPTED the commit;
    #     Python renders it `1.0` and refused. The suite pinned `1e3`, where
    #     the two agree, and never tried `1e0`, where they did not. jq 1.7.1
    #     cannot tell `1e0` from `1` at all: `tostring` and `tojson` both give
    #     `1`, while it preserves `1.0` and `1E+3`. So this one cannot be
    #     closed inside the parsers, and it is closed before them — see the
    #     text guards below.
    #   - A byte-order mark before the JSON — jq strips a LEADING one and
    #     accepted the commit; Python refused. Also closed before the parsers.
    #   - TWO JSON documents on the trailer line — jq read the first and
    #     silently ignored the rest, so an APPROVE followed by a BLOCK exited
    #     0 and the tail was validated by nothing. jq now reads the line
    #     slurped and counts what it got, and the fallback counts the same way.
    #
    # WHAT DECIDES WHICH BEHAVIOUR IS RIGHT is not "whichever parser we have".
    # It is the canonical reader — `internal/workspace/reviewverdict.go`, whose
    # `SchemaVersion` field is an `int`. Measured against Go's encoding/json:
    # it refuses `1.0`, `1e0`, `1e3`, `100000000000000000000` and `"1"` for
    # that field, refuses a leading byte-order mark, and refuses a second
    # document after the first. On every one of those except `"1"` this
    # validator now agrees with it.
    #
    # THE ONE PLACE IT STILL DOES NOT, stated because the next reader will
    # otherwise have to measure it again: `"schema_version": "1"` — the STRING
    # rather than the number — is accepted here and refused by Go. Both parsers
    # accept it, so it is not a divergence between them and no test here goes
    # red on it; it is a divergence from the canonical reader. Closing it needs
    # the record to carry the JSON type of the field, which would make three
    # of the sentinels in column 1 unreachable, so it is a change to the record
    # shape rather than a patch, and it is not made here.
    VERDICT_FIELD=""
    SCHEMA_VERSION=""
    NOTES_STATE=""
    FLAGS_STATE=""
    PARSE_ERROR=""

    # Read SLURPED (`jq -s`), so the whole trailer line becomes an array of the
    # documents it holds and the count is a value this program can test. Plain
    # `jq` reads a STREAM: it applied the filter to the first document and said
    # nothing about the rest, so
    # `{… "verdict":"APPROVE" …} {… "verdict":"BLOCK" …}` exited 0 and the
    # BLOCK was read by nothing. `explode`, not a regex, decides the control
    # test — Oniguruma's spelling of a control-range class is not the same as
    # Python's, and two regex dialects is the kind of difference this pair of
    # programs exists to avoid.
    JQ_PROG='
      def ctl: explode | any(. < 32 or . == 127);
      def free: if . == "" then "__EMPTY__" elif ctl then "__CONTROL__" else . end;
      if length == 0 then "NOJSON" elif length > 1 then "MULTIDOC" else
      ( .[0]
        | if type != "object" then "NOTOBJ" else
          [ (if has("schema_version") then ((.schema_version|tostring) | free) else "__ABSENT__" end),
            (if has("verdict") then (if .verdict == null then "__NULL__" else ((.verdict|tostring) | free) end) else "__ABSENT__" end),
            (if has("notes") then
               (if (.notes|type) != "string" then "badtype"
                elif (.notes|test("\\S")) then "ok"
                else "empty" end)
             else "absent" end),
            (if has("flags") then (if (.flags|type) == "array" or .flags == null then "array" else "notarray" end) else "absent" end)
          ] | @tsv end ) end'

    # The fallback, kept beside the jq program on purpose: the two are one
    # contract, and a reader has to be able to compare them without opening a
    # second file. Written as a here-document rather than inline in the
    # `python3 -c` argument, because the escaping this program does needs
    # backslash literals and a double-quoted shell argument eats them.
    #
    # `as_text` mirrors jq's `tostring`, which the earlier `str()` did not: a
    # JSON `true` is `true` and not `True`, and an object is compact JSON and
    # not a Python repr.
    #
    # `tsv` mirrors jq's `@tsv`. Backslash goes first — escape it after the
    # others and the backslashes they insert get escaped a second time. The
    # characters are written with `chr` so this file carries no backslash
    # literal that a future edit could mis-count.
    #
    # `free` is the control-byte refusal, and it is the reason `tsv` no longer
    # has to keep pace with jq's escape list. `ctl` walks code points rather
    # than running a regex, so the two programs decide the same question with
    # no regex dialect between them.
    #
    # The decode loop mirrors `jq -s`: raw_decode until the text is spent, then
    # count. `json.load` read ONE document and refused a trailing second one
    # with a parse error, where slurped jq reports two — same refusal, but a
    # different word for it, and this pair is compared byte for byte. Counting
    # in both makes the answer the same sentence. It also keeps jq's own
    # behaviour on trailing text that is not JSON at all: the second decode
    # fails, and both report a parse error.
    PY_PROG="$(cat <<'PY_FALLBACK'
import sys, json, re

BS = chr(92)


def as_text(x):
    if isinstance(x, bool):
        return 'true' if x else 'false'
    if isinstance(x, str):
        return x
    return json.dumps(x, separators=(',', ':'), ensure_ascii=False)


def ctl(s):
    for ch in s:
        if ord(ch) < 32 or ord(ch) == 127:
            return True
    return False


def free(s):
    if s == '':
        return '__EMPTY__'
    if ctl(s):
        return '__CONTROL__'
    return s


def tsv(s):
    s = s.replace(BS, BS + BS)
    s = s.replace(chr(9), BS + 't')
    s = s.replace(chr(10), BS + 'n')
    s = s.replace(chr(13), BS + 'r')
    return s


text = sys.stdin.read()
decoder = json.JSONDecoder()
docs = []
idx = 0
try:
    while True:
        while idx < len(text) and text[idx] in ' ' + chr(9) + chr(10) + chr(13):
            idx += 1
        if idx >= len(text):
            break
        obj, idx = decoder.raw_decode(text, idx)
        docs.append(obj)
except Exception as e:
    print('PARSE_ERROR: ' + str(e), file=sys.stderr)
    sys.exit(1)
if len(docs) == 0:
    print('NOJSON')
    sys.exit(0)
if len(docs) > 1:
    print('MULTIDOC')
    sys.exit(0)
d = docs[0]
if not isinstance(d, dict):
    print('NOTOBJ')
    sys.exit(0)
if 'schema_version' not in d:
    sv = '__ABSENT__'
else:
    sv = free(as_text(d['schema_version']))
if 'verdict' not in d:
    v = '__ABSENT__'
elif d['verdict'] is None:
    v = '__NULL__'
else:
    v = free(as_text(d['verdict']))
if 'notes' not in d:
    n = 'absent'
elif not isinstance(d['notes'], str):
    n = 'badtype'
elif re.search(r'\S', d['notes']):
    n = 'ok'
else:
    n = 'empty'
if 'flags' not in d:
    f = 'absent'
elif d['flags'] is None or isinstance(d['flags'], list):
    f = 'array'
else:
    f = 'notarray'
print(chr(9).join([tsv(sv), tsv(v), tsv(n), tsv(f)]))
PY_FALLBACK
)"

    # ── 9a. Two questions decided on the TEXT, before any parser runs ────
    #
    # Both were divergences, and both had jq as the permissive side — which
    # matters, because jq is the path this script takes on any machine that
    # has it. Neither can be closed inside the parsers, so neither is asked of
    # them: a guard that runs before the fork cannot fork.
    #
    # A LEADING BYTE-ORDER MARK. Measured: jq strips one and accepted the
    # commit, Python refused it, and Go's encoding/json refuses it
    # (`invalid character 'ï' looking for beginning of value`). Only a LEADING
    # mark is refused. jq rejects one anywhere else as a parse error already,
    # and a U+FEFF inside the `notes` string is ordinary text that Go accepts —
    # refusing that would be inventing a rule.
    #
    # WHAT THIS GUARD BUYS, SAID EXACTLY, because it is less than it looks. The
    # number-literal whitelist below would refuse a leading mark on its own: the
    # mark is not inside a string, so it survives the string strip and is not a
    # character a JSON structure may hold. Measured with this guard disabled and
    # the whitelist left in place: the commit is still refused, on both parsers,
    # byte for byte — 2 assertions go red and both of them are about the WORDS.
    # So this guard closes no hole. It names the cause, and it runs first for
    # that reason alone. Do not describe it as the thing that stops a
    # byte-order mark.
    #
    # A NUMBER LITERAL jq READS AND JSON DOES NOT HAVE. Measured:
    # `"schema_version": 1e0` is rendered `1` by jq, which ACCEPTED an APPROVE,
    # and `1.0` by Python, which refused. jq 1.7.1 cannot see the difference —
    # it canonicalises `1e0` to `1` while preserving `1.0` and `1E+3` — so no
    # jq program can decide this and the rule is stated over the text instead.
    #
    # `1e0` WAS NOT ALONE, and finding the rest is why this guard is a
    # whitelist. jq's number reader is looser than JSON's in more places than
    # one, and every one of them renders to something jq will compare happily.
    # Measured, jq accepting and Python refusing each time: `01`, `+1`, `00`,
    # `007`, `.5`, `5.`, `1.`, `nan` and `infinity`. TWO OF THEM RENDER AS
    # EXACTLY `1` — `01` and `+1` — so both were ACCEPTED APPROVALS on the jq
    # path, found by driving payloads at the pair rather than by reading it.
    # A blacklist of `.`, `e` and `E` would have caught `1e0` and shipped
    # beside `01`. That is the shape this branch keeps repairing: a fix that
    # closes the member it was shown and leaves the class open.
    #
    # SO THE RULE IS POSITIVE. Once the strings are out of the way, the only
    # characters a schema-v1 verdict can legally still hold are JSON structure,
    # whitespace, digits and a minus sign. Anything else — a letter, a plus, a
    # decimal point, an exponent — is refused without needing to be enumerated.
    # A leading zero is the one illegal integer that survives that test, so it
    # is asked for separately.
    #
    # Go is what decides which behaviour is right, not whichever parser is
    # installed: `SchemaVersion int` in internal/workspace/reviewverdict.go
    # refuses `1.0`, `1e0`, `1e3` and `"1"`, and encoding/json refuses `01` and
    # `+1` as invalid JSON.
    #
    # HOW THE TEXT IS READ. String literals are removed first with the standard
    # JSON string pattern, then the three bare words `true`, `false` and `null`.
    # What is left is structure, numbers and whitespace. The limit, stated
    # rather than discovered later: on a payload whose quoting is already
    # broken, the string pattern cannot match what is not a string, so prose
    # inside an unterminated string is read as structure and reported as a bad
    # literal instead of as bad JSON. That message is less exact, the payload is
    # refused either way, and it is refused identically on both parsers — which
    # is the property this guard exists to hold.
    VERDICT_TEXT_ERROR=""
    VERDICT_JSON_TRIMMED="${VERDICT_JSON#"${VERDICT_JSON%%[![:space:]]*}"}"
    case "$VERDICT_JSON_TRIMMED" in
      $'\xef\xbb\xbf'*)
        VERDICT_TEXT_ERROR="Review-Verdict trailer begins with a byte-order mark."
        ;;
    esac
    if [[ -z "$VERDICT_TEXT_ERROR" ]]; then
      VERDICT_JSON_BARE="$(printf '%s\n' "$VERDICT_JSON" | sed -E 's/"([^"\\]|\\.)*"//g; s/true|false|null//g')"
      # Held in variables because bash 3.2 treats a QUOTED right-hand side of
      # `=~` as a literal string, and an unquoted one here would be re-globbed.
      VERDICT_BARE_OK='^[][{}:,0-9[:space:]-]*$'
      VERDICT_LEADING_ZERO='(^|[^0-9])0[0-9]'
      if [[ ! "$VERDICT_JSON_BARE" =~ $VERDICT_BARE_OK ]]; then
        VERDICT_TEXT_ERROR="Review-Verdict JSON holds a number literal that JSON does not have."
      elif [[ "$VERDICT_JSON_BARE" =~ $VERDICT_LEADING_ZERO ]]; then
        VERDICT_TEXT_ERROR="Review-Verdict JSON holds a number with a leading zero."
      fi
    fi

    if [[ -n "$VERDICT_TEXT_ERROR" ]]; then
      PARSE_ERROR="$VERDICT_TEXT_ERROR"
    elif command -v jq &>/dev/null; then
      if PARSED="$(printf '%s\n' "$VERDICT_JSON" | jq -s -r "$JQ_PROG" 2>/dev/null)"; then
        :
      else
        PARSE_ERROR="jq could not parse JSON"
      fi
    else
      # jq absent: fall back to Python (macOS + most Linux envs).
      PARSED="$(printf '%s\n' "$VERDICT_JSON" | python3 -c "$PY_PROG" 2>&1)"
      PY_EXIT=$?
      if (( PY_EXIT != 0 )); then
        PARSE_ERROR="$PARSED"
      fi
    fi

    if [[ -z "$PARSE_ERROR" ]]; then
      case "$PARSED" in
        NOTOBJ)
          PARSE_ERROR="Review-Verdict is valid JSON but not a JSON object"
          ;;
        NOJSON)
          PARSE_ERROR="the Review-Verdict trailer carries no JSON at all"
          ;;
        MULTIDOC)
          # Two documents on one trailer line. The first used to be validated
          # and the rest read by nothing, so an APPROVE could be followed by a
          # BLOCK and the commit exited 0.
          PARSE_ERROR="the Review-Verdict trailer holds more than one JSON document; only the first would be read"
          ;;
        *)
          IFS=$'\t' read -r SCHEMA_VERSION VERDICT_FIELD NOTES_STATE FLAGS_STATE <<<"$PARSED"
          ;;
      esac
    fi

    if [[ -n "$PARSE_ERROR" ]]; then
      # A text guard already says what is wrong in its own words. Only the
      # parser's own failures get the "not valid JSON" heading, because for a
      # byte-order mark or a `1e0` the JSON parses fine and that heading would
      # be false.
      if [[ -n "$VERDICT_TEXT_ERROR" ]]; then
        err "$VERDICT_TEXT_ERROR"
        err "  Every reader has to agree on what this trailer says, and they do not agree on this."
      else
        err "Review-Verdict trailer is not valid JSON."
        err "  Parse error: $PARSE_ERROR"
      fi
      err "  Got: $VERDICT_JSON"
    else
      # ── 10. schema_version check ─────────────────────────────────────
      if [[ "$SCHEMA_VERSION" == "__CONTROL__" ]]; then
        err "Review-Verdict 'schema_version' holds a control character."
        err "  A schema version is a number. A control byte in it moves the field"
        err "  boundaries of the record this validator reads, so it is refused."
        err "  Got: $VERDICT_JSON"
      elif [[ "$SCHEMA_VERSION" != "1" ]]; then
        err "Review-Verdict JSON missing or wrong 'schema_version' (expected 1, got '${SCHEMA_VERSION}')."
        err "  Got: $VERDICT_JSON"
      fi

      # ── 11. notes field required and non-empty ───────────────────────
      # `notes` is a required field of schema v1 in every statement of it —
      # the agent-reviewer skill, the config-reviewer skill, and the Go reader
      # in internal/workspace/reviewverdict.go, which rejects a verdict file
      # with absent or empty notes. Only this validator used to let it through.
      case "$NOTES_STATE" in
        ok) ;;
        absent)
          err "Review-Verdict JSON is missing the required 'notes' field."
          err "  schema v1 requires: schema_version, verdict, notes."
          err "  Got: $VERDICT_JSON"
          ;;
        empty)
          err "Review-Verdict JSON has an empty 'notes' field."
          err "  notes carries the reason for the verdict; it must say something."
          err "  Got: $VERDICT_JSON"
          ;;
        badtype)
          err "Review-Verdict JSON field 'notes' must be a string."
          err "  Got: $VERDICT_JSON"
          ;;
        *)
          # Fail closed. The parser is meant to hand back one of the four words
          # above. Anything else means the record came apart — a notes string
          # holding a tab is the way that happens — and a validator that shrugs
          # at a record it could not read is a validator that approves it.
          err "Review-Verdict parser returned an unreadable 'notes' state ('${NOTES_STATE}')."
          err "  Got: $VERDICT_JSON"
          ;;
      esac

      # ── 12. verdict field present ────────────────────────────────────
      if [[ "$VERDICT_FIELD" == "__ABSENT__" || -z "$VERDICT_FIELD" ]]; then
        err "Review-Verdict JSON is missing the 'verdict' field."
        err "  Got: $VERDICT_JSON"
      elif [[ "$VERDICT_FIELD" == "__EMPTY__" ]]; then
        # The key is present and says nothing. This gets its own state rather
        # than reading as "missing", because the author has to be told which of
        # the two they wrote. The sentinel is also what stops an empty value
        # collapsing the record — see the parser note above.
        err "Review-Verdict 'verdict' is an empty string."
        err "  Write the verdict the reviewer gave."
        err "  If no reviewer was reached, say so: \"verdict\": \"NOT_REVIEWED\"."
        err "  Got: $VERDICT_JSON"
      elif [[ "$VERDICT_FIELD" == "__CONTROL__" ]]; then
        # A control byte in the verdict word. Refused as a class rather than
        # read: bash's command substitution DROPS a NUL, so `APPROVE<NUL>` came
        # back from the fallback as the word `APPROVE` and was accepted as an
        # approval, while jq refused the same message. See the parser note
        # above for the measurement.
        err "Review-Verdict 'verdict' holds a control character."
        err "  A verdict is a plain word. A control byte in it is not read the same"
        err "  way by every reader, so it is refused rather than guessed at."
        err "  Got: $VERDICT_JSON"
      elif [[ "$VERDICT_FIELD" == "__NULL__" ]]; then
        # A null verdict is the honest intent written in a shape that cannot be
        # told apart from a truncated trailer. This history holds both: trailers
        # cut off just after `"verdict": null`, and trailers that parse cleanly
        # with a null verdict. Say the absence in words instead.
        err "Review-Verdict 'verdict' is null."
        err "  A null verdict cannot be told apart from a truncated trailer."
        err "  If no reviewer was reached, say so: \"verdict\": \"NOT_REVIEWED\"."
        err "  Got: $VERDICT_JSON"
      else
        # ── 13. verdict enum check ────────────────────────────────────
        # agent-reviewer schema v1: APPROVE, REQUEST_CHANGES, BLOCK
        # config-reviewer schema v1: CLEAN, DRIFT_MINOR, DRIFT_MAJOR
        # AGENTS.md no-reviewer-reached rule: NOT_REVIEWED
        case "$VERDICT_FIELD" in
          APPROVE|CLEAN)
            # ── 14. an approval is held to more than the rest ─────────
            check_approval_identity "$VERDICT_FIELD"
            if [[ "$FLAGS_STATE" == "absent" ]]; then
              err "the ${VERDICT_FIELD} verdict must carry the 'flags' key that the reviewer skill emits."
              err "  Use \"flags\": [] when the reviewer raised nothing."
              err "  Got: $VERDICT_JSON"
            elif [[ "$FLAGS_STATE" == "notarray" ]]; then
              err "Review-Verdict JSON field 'flags' must be an array."
              err "  Got: $VERDICT_JSON"
            elif [[ "$FLAGS_STATE" != "array" ]]; then
              err "Review-Verdict parser returned an unreadable 'flags' state ('${FLAGS_STATE}')."
              err "  Got: $VERDICT_JSON"
            fi
            ;;
          NOT_REVIEWED)
            # ── 14a. the honest form has to stay honest ───────────────
            # This verdict lands, and it is meant to. The one thing it may
            # not do is name a reviewer on the line above it.
            check_no_reviewer_named
            ;;
          REQUEST_CHANGES|DRIFT_MINOR|DRIFT_MAJOR)
            # OK — these may land in commits, and they may name a reviewer.
            # `Reviewed-By: agent-reviewer` with a REQUEST_CHANGES or a DRIFT
            # verdict is a TRUE statement: the reviewer ran and declined. 69
            # commits in this history say it. Nothing here to check.
            ;;
          BLOCK)
            err "BLOCK verdict must not be committed (fix first)."
            err "  Review-Verdict: $VERDICT_JSON"
            ;;
          *)
            err "unknown verdict value '$VERDICT_FIELD'."
            err "  Allowed (agent-reviewer): APPROVE, REQUEST_CHANGES"
            err "  Allowed (config-reviewer): CLEAN, DRIFT_MINOR, DRIFT_MAJOR"
            err "  Allowed when no reviewer was reached: NOT_REVIEWED"
            err "  BLOCK = fix before committing, never in a commit."
            err "  Got: $VERDICT_JSON"
            ;;
        esac
      fi
    fi
  fi
fi

# ── Final: emit all errors or exit clean ─────────────────────────────────────
if (( ${#ERRORS[@]} > 0 )); then
  echo "validate-commit-msg: validation failed:" >&2
  for line in "${ERRORS[@]}"; do
    echo "$line" >&2
  done
  exit 1
fi

exit 0
