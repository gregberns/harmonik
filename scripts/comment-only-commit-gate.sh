#!/usr/bin/env bash
# comment-only-commit-gate.sh — a Go change may not be all comment and no code.
#
# THE PROBLEM THIS SOLVES. Agents pick comment-shaped work because it always
# succeeds. Measured over the 300 commits before 2026-08-23: eight changed five
# or fewer lines of Go CODE and twenty or more lines of Go COMMENT, and five of
# those re-edit the same two daemon test files — a claim corrected, then the
# correction corrected. That is the buildup the 89,773-line cut removed, arriving
# again one commit at a time.
#
# WHY NOT A LENGTH CAP. The obvious rule is a cap on how long a comment may be.
# Every one of those eight commits is made of SHORT comment blocks, so a cap
# fires on none of them. A cap also cannot pass this tree without a large
# path-keyed allow list, which is the shape that already has a rename bug here
# (hk-uny5m). The thing worth gating is the SHAPE OF THE WORK, not the length of
# the prose.
#
# THE RULE. Across the Go files in the change: if no non-comment line changed,
# and the comment lines did not get fewer, that is a comment-only change and the
# build stops. Attach it to the code work it describes, or delete more than it
# adds.
#
# WHAT IS NOT A COMMENT, even though it starts with two slashes. A build tag, a
# //nolint, a //lint:ignore and a //line all decide what the compiler or the
# linter does. They are code. Without this, a build-tag refactor reads as
# comment-only: commit 327918085 added 129 `//go:build specaudit` lines and no
# other Go line, and an earlier draft of this gate refused it.
#
# WHAT ELSE PASSES.
#   - A net reduction, so a deletion sweep (tools/commentcut) is legal.
#   - A change that also edits a NORMATIVE doc — specs/, docs/foundation/, a
#     SKILL.md. Amending a spec and syncing the Go comment that cites it is one
#     piece of work, and the Go half alone reads as comment-only. Six commits in
#     this history have that shape. Refusing them makes "let the comment go
#     stale" the only legal outcome, which inverts the point.
#   - Anything at or below the grandfather baseline. This history holds 27
#     comment-only commits; without the baseline, checking one out leaves the
#     build red for a reason that has nothing to do with what is being checked,
#     and a bisect reports this gate instead of the bug.
#
# WINDOW 1 FIRES MID-WORK, ON PURPOSE. Reword a comment before you write the
# code and `make fast` goes red. That is the nudge, not a defect. Finish the
# code, or drop the comment edit. Window 1 cannot see untracked .go files
# (`git diff HEAD` skips them); window 2 catches those once they are committed.
#
# KNOWN BLIND SPOT. A `//`-leading line inside a backtick raw string counts as a
# comment, so editing only such lines in a fixture can fire. Tracking backtick
# parity in awk costs more than the case is worth.
#
# EXIT CODES
#   0 — no comment-only Go change.
#   1 — a comment-only Go change. This is the verdict, and the build stops.
#   2 — could not reach a verdict. Inconclusive fails closed.

set -uo pipefail
export LC_ALL=C

# BASELINE — the newest commit this gate grandfathers. Commits at or below it are
# never judged. Move it forward only to grandfather more; moving it back widens
# what is enforced.
BASELINE="${COMMENT_ONLY_GATE_BASELINE:-b2dce6aed}"

repo_root=$(git rev-parse --show-toplevel) || {
    echo "comment-only-commit-gate: not inside a git worktree" >&2
    exit 2
}
cd "$repo_root" || exit 2

die() {
    printf 'comment-only-commit-gate: %s\n' "$*" >&2
    exit 2
}

# tally reads a unified diff on stdin and prints "code added removed".
#
# A line is comment when its first non-blank characters open or continue a
# comment AND it is not a directive. Blank lines count as neither. Leading
# whitespace is stripped first, so a re-indent registers as a CODE change.
tally() {
    awk '
        /^\+\+\+/ || /^---/ { next }
        /^[+-]/ {
            sign = substr($0, 1, 1)
            line = substr($0, 2)
            sub(/^[ \t]+/, "", line)
            if (line == "") next
            # Directives are code: they change what the compiler or linter does.
            if (line ~ /^\/\/go:/ || line ~ /^\/\/ \+build/ || line ~ /^\/\/nolint/ ||
                line ~ /^\/\/lint:ignore/ || line ~ /^\/\/line /) { code++; next }
            if (line ~ /^\/\// || line ~ /^\/\*/ || line ~ /^\*\// || line ~ /^\*[ \t]/ || line == "*") {
                if (sign == "+") added++; else removed++
            } else {
                code++
            }
        }
        END { printf "%d %d %d\n", code + 0, added + 0, removed + 0 }
    '
}

# touches_normative_doc <diff-command...> — true when the same change edits a
# doc an agent is expected to act on.
touches_normative_doc() {
    local files
    files=$("$@" --name-only -- 'specs/**' 'docs/foundation/**' '**/SKILL.md' 2>/dev/null) || return 1
    [ -n "$files" ]
}

# judge <label> <diff-command...> — returns 1 when the window is comment-only.
judge() {
    local label="$1"; shift
    local out code added removed
    out=$("$@" -- '*.go' 2>/dev/null | tally) || die "could not read the $label diff"
    read -r code added removed <<<"$out"

    [ "$((added + removed))" -gt 0 ] || return 0   # no Go comment moved
    [ "$code" -eq 0 ] || return 0                  # real code changed
    [ "$((added - removed))" -ge 0 ] || return 0   # net deletion: a sweep

    if touches_normative_doc "$@"; then
        printf 'comment-only-commit-gate: %s edits a normative doc, so its Go comment sync is part of that work.\n' "$label"
        return 0
    fi

    printf 'comment-only-commit-gate: FAIL — %s changes Go comments and no Go code.\n\n' "$label" >&2
    printf '  comment lines added    %s\n' "$added" >&2
    printf '  comment lines removed  %s\n' "$removed" >&2
    printf '  code lines changed     %s\n\n' "$code" >&2
    cat >&2 <<'EOF'
  A commit whose whole deliverable is comment prose is not work. It always
  succeeds, which is why it keeps getting picked, and it is how the comment
  volume grows back.

  Three ways forward, in order of preference:
    1. Attach the comment change to the code change it describes.
    2. Drop the comment change. A wrong comment on code nobody is touching is
       a defect to file, not a commit to make.
    3. Delete more comment than you add. A net reduction always passes.
EOF
    return 1
}

status=0

# Window 1 — the working tree against HEAD. This is where the edit is still
# being made, and the cheapest place to say so.
judge "the working tree" git diff HEAD || status=1

# Window 2 — HEAD against its parent, so a comment-only change cannot simply be
# committed past window 1. A merge authors nothing of its own and is skipped.
# Grandfathered history is skipped too, so an old checkout is not held red for a
# commit that predates the rule.
if ! git cat-file -e "${BASELINE}^{commit}" 2>/dev/null; then
    echo "comment-only-commit-gate: baseline ${BASELINE} is not in this repository, so only the working tree was judged"
elif ! git merge-base --is-ancestor "$BASELINE" HEAD 2>/dev/null; then
    echo "comment-only-commit-gate: this history does not descend from the baseline ${BASELINE}, so only the working tree was judged"
elif [ "$(git rev-parse "${BASELINE}^{commit}")" = "$(git rev-parse HEAD)" ]; then
    echo "comment-only-commit-gate: HEAD is the baseline, so only the working tree was judged"
else
    parents=$(git rev-list --parents -n 1 HEAD) || die "could not read HEAD's parents"
    set -- $parents
    if [ "$#" -eq 2 ]; then
        judge "HEAD" git diff "$2" HEAD || status=1
    fi
fi

[ "$status" -eq 0 ] && echo "comment-only-commit-gate: ok — no comment-only Go change"
exit "$status"
