#!/usr/bin/env bash
# lint-allow-ratchet.sh — the allow list may get shorter. It may never get longer.
#
# THE PROBLEM THIS SOLVES. `make fast` now ends with the whole-tree lint judge
# (scripts/lint-allow.sh). That judge fails on a finding whose file-and-linter
# identity is not in tools/lintreport/allow.txt. There is one repair that turns that
# red into a green in ten seconds and leaves the defect in the tree: append the
# identity to the allow list. Every occurrence of hk-dp69a named that repair as the
# tempting one, and the judge itself can only ask people not to do it.
#
# This step makes the promise mechanical. It compares the finding identities in
# the list against the identities the same list held before, and it fails when one
# appears that was not there. It does not care about line numbers, comments,
# order, or the number of lines. It cares about the tolerated identity set.
#
# THE TWO WINDOWS, and why both are needed.
#
#   WORKING TREE against HEAD. This is the window where the temptation lives.
#   A lane sees `make fast` go red on lint, edits the allow list, and runs it
#   again. The second run fails here, before the edit is ever committed.
#
#   HEAD against its parents. The gate also runs AFTER a commit. Without this
#   window a pair added and committed in one step would pass every later run
#   forever. A merge commit is judged against the union of all its parents, so
#   a merge that carries a pair one side already had is not a new pair.
#
# WHY NOT A BASE ON main. Measured on 2026-08-11: tools/lintreport/allow.txt
# does not exist at the merge base with main, so every one of its 582 pairs
# reads as new and the check would be red the day it lands. A base that is
# stale by one program cannot ratchet anything.
#
# WHAT THIS DOES NOT CATCH, said out loud so nobody has to rediscover it.
# The base is one commit back, so the window SLIDES. A pair committed at commit
# N fails at N and passes from N+1 onward. And the union rule lets a merge carry
# in a pair that a side branch added without ever running the gate, because the
# union accepts anything any parent held. So this is a per-commit ratchet, not a
# history-wide invariant: it stops the edit while it is being made and it stops
# the commit that makes it. A durable base — the merge base with the integration
# branch, or a committed high-water mark — closes both, and neither is available
# yet for the reason below.
#
# The alternative for the merge case is to judge against the FIRST parent alone.
# That closes the merge path and costs a red build to whoever runs the merge for
# a pair another lane added. Union was chosen because lanes land on a shared
# integration branch here, so first-parent would fire on the wrong person often.
#
# WHY NOT A COMMITTED LINE COUNT. A count falls as readily when somebody
# silences a finding as when somebody fixes one, and a swap — delete one pair,
# add another — leaves the count untouched. scripts/lint-allow.sh already
# carries that reasoning for the verdict itself. It applies here too.
#
# EXIT CODES
#   0 — no pair was added.
#   1 — a pair was added. This is the verdict, and the build stops.
#   2 — could not reach a verdict: no allow list, an unreadable base, or a
#       history too shallow to name one. An unresolvable base is inconclusive,
#       and inconclusive fails closed.

set -uo pipefail

# Byte order everywhere. `sort` and `comm` must agree on collation, and under a
# UTF-8 locale they do not: `comm` would read a correctly sorted list as
# unsorted and answer with a set difference that is not one. That failure is
# silent in the direction that matters, because a missed line reads as "no pair
# was added".
export LC_ALL=C

repo_root=$(git rev-parse --show-toplevel) || {
    echo "lint-allow-ratchet: not inside a git worktree" >&2
    exit 2
}
cd "$repo_root" || exit 2

# Overridable so the self-test can point at a scratch list inside a scratch
# repository. The path is read by `git show <rev>:<path>`, so it must stay
# RELATIVE to the repository root.
allow="${LINT_ALLOW_LIST:-tools/lintreport/allow.txt}"

die() {
    printf 'lint-allow-ratchet: %s\n' "$*" >&2
    exit 2
}

[ -f "$allow" ] || die "no allow list at $allow.
  A missing list is not an empty list, and a gate that shrugs at a missing step
  has not produced a verdict."
case "$allow" in
    /*|../*|*/../*) die "LINT_ALLOW_LIST must name a repository-relative path" ;;
    *.txt) ;;
    *) die "LINT_ALLOW_LIST must name a .txt allow list, not executable product code" ;;
esac

# pairs <file> — the tolerated set, with comments, blank lines and order removed.
#
# Blank lines are dropped by sed and NOT by `grep -v`. grep exits 1 when it
# matches nothing, and under `pipefail` that status reaches the caller, so a
# fully cleaned allow list — the state this whole ratchet exists to reach —
# would have read as "could not read the list" and failed the build from inside
# gate-static. sed exits 0 whether it deletes a line or not.
pairs() {
    awk '
        { sub(/#.*/, ""); sub(/[[:space:]]+$/, "") }
        /^[[:space:]]*$/ { next }
        length($1) != 64 || $1 !~ /^[0-9a-f]+$/ || NF != 2 { bad=1; next }
        { print $1 "\t" $2 }
        END { if (bad) exit 2 }
    ' "$1" | sort -u
}

is_legacy() {
    sed -e 's/#.*$//' -e '/^[[:space:]]*$/d' "$1" |
        awk 'NF == 2 && (length($1) != 64 || $1 !~ /^[0-9a-f]+$/) { found=1 } END { exit !found }'
}

# Project content identities back to the legacy path/linter groups during the
# one format transition. A new identity passes only when its location comment
# names a group the old list already tolerated.
legacy_pairs() {
    awk '
        /^[[:space:]]*#/ || /^[[:space:]]*$/ { next }
        length($1) == 64 && $1 ~ /^[0-9a-f]+$/ {
            if (NF < 4 || $3 != "#") { bad=1; next }
            path=$4; sub(/:[0-9]+$/, "", path); print path "\t" $2; next
        }
        NF == 2 { print $1 "\t" $2; next }
        { bad=1 }
        END { if (bad) exit 2 }
    ' "$1" | sort -u
}

# scheme_version <file> — the declared key scheme, read from a
# `# key-scheme: N` header line. pairs() and readAllow() both strip everything
# from the first `#` onward, so a header changes no compared pair; it exists
# only for this function to read. A file with no such header reads as scheme
# 0, which is every allow list this repository has ever committed until the
# first migration lands — so this reads as "unchanged" everywhere until then.
scheme_version() {
    awk '
        /^#[[:space:]]*key-scheme:[[:space:]]*[0-9]+[[:space:]]*$/ {
            match($0, /[0-9]+/)
            print substr($0, RSTART, RLENGTH)
            found=1
            exit
        }
        END { if (!found) print 0 }
    ' "$1"
}

# lint_report — the CURRENT tree's golangci-lint findings, as JSON, memoized
# for the life of this run. A key-scheme migration is judged against what the
# tree actually contains today, not against a text diff of the allow list —
# that recomputation is the whole reason a migration costs seconds instead of
# milliseconds, and it must happen at most once even if both windows below
# need it.
lint_report() {
    [ -s "$work/current-report.json" ] && return 0
    local tools_home tools_dir linter status
    if [ -n "${TOOLS_DIR:-}" ]; then
        tools_dir="$TOOLS_DIR"
    else
        tools_home=$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null | sed 's|/\.git/*$||')
        tools_dir="${tools_home:-$repo_root}/.tools"
    fi
    linter="$tools_dir/golangci-lint"
    [ -x "$linter" ] || die "no golangci-lint at $linter.
  A key-scheme migration cannot be judged against the current tree without it.
  Run 'make tools' to install it."
    status=0
    scripts/with-lane-gocache.sh "$linter" run --allow-parallel-runners \
        --issues-exit-code=0 --max-issues-per-linter=0 --max-same-issues=0 \
        --timeout="${LINT_ALLOW_RATCHET_TIMEOUT:-15m}" \
        --output.text.path=/dev/null \
        --output.json.path="$work/current-report.json" >/dev/null || status=$?
    [ "$status" -eq 0 ] || die "golangci-lint failed (exit $status) while judging a key-scheme migration."
    [ -s "$work/current-report.json" ] || die "golangci-lint wrote an empty report while judging a key-scheme migration."
}

# transition_check <base_rev> <new_pairs_file> <label> — the one path that can
# judge a declared key-scheme migration. A re-key changes almost every row's
# TEXT while tolerating the same set of findings, and a text compare reads
# that as hundreds of pairs added; this recomputes from the tree instead.
#
# (i) is `-remap` rule 3, and it carries the whole soundness proof: every
# CURRENT finding's OLD-scheme digest must already sit in the OLD-scheme allow
# list. It is checked by building tools/lintreport AS IT STOOD AT <base_rev>
# and running it, with the allow list as it stood at <base_rev>, against a
# report of the CURRENT tree. Exit 0 from that judge IS (i) — there is no
# separate reimplementation of the old digest here to get wrong, and a finding
# this refuses is named by the judge itself, never by a count.
#
# (ii) old-to-new is an injection, never a bijection: shrinking is fine,
# growing is not. judge() already prints the shrink as "N allow-list entries
# are now clean"; reading that count back out and bounding <label>'s pair
# count by the old count minus it is what refuses a row with no old row
# behind it — a fabricated addition, or a header bumped with the keying code
# left unchanged — without asserting a bijection that legitimate stale rows
# would fail.
transition_check() {
    local base_rev="$1" new_pairs_file="$2" label="$3"
    local old_src old_bin old_allow judge_out status stale old_count new_count

    lint_report

    old_src="$work/base-src-$base_rev"
    old_bin="$work/base-bin-$base_rev"
    old_allow="$work/base-allow-$base_rev"
    judge_out="$work/transition-$base_rev.out"
    mkdir -p "$old_src" "$old_bin"

    git archive "$base_rev" -- tools/lintreport 2>/dev/null | tar -x -C "$old_src"
    [ -f "$old_src/tools/lintreport/main.go" ] || die \
        "could not read tools/lintreport as it stood at $base_rev.
  A key-scheme migration cannot be judged without the OLD keying code to check
  the current tree against."
    [ -f "$old_src/tools/lintreport/go.mod" ] || printf 'module lintreport-base\n\ngo 1.21\n' \
        >"$old_src/tools/lintreport/go.mod"
    scripts/with-lane-gocache.sh go build -C "$old_src/tools/lintreport" -o "$old_bin/lintreport" . || die \
        "could not build tools/lintreport as it stood at $base_rev."

    git show "$base_rev:$allow" >"$old_allow" 2>/dev/null || die \
        "could not read $allow as it stood at $base_rev."

    status=0
    "$old_bin/lintreport" -allow "$old_allow" "$work/current-report.json" >"$judge_out" 2>&1 || status=$?
    case "$status" in
        0 | 1) ;;
        *) die "the $base_rev-scheme lintreport could not reach a verdict (exit $status) judging the current tree." ;;
    esac

    if [ "$status" -ne 0 ]; then
        printf 'lint-allow-ratchet: FAIL — %s declares a key-scheme migration that tolerates a finding the scheme at %s never did:\n\n' \
            "$label" "$base_rev" >&2
        cat "$judge_out" >&2
        cat >&2 <<EOF

  Every finding in the current tree must already be tolerated under the
  scheme at $base_rev — the findings named above are not. This is not a
  count: a re-key may carry every old row forward, and it may not adopt one
  that was never tolerated. Fix the finding, or leave the build red and
  report it.
EOF
        exit 1
    fi

    stale=$(grep -oE '^[0-9]+ allow-list entries are now clean' "$judge_out" | grep -oE '^[0-9]+')
    stale="${stale:-0}"
    pairs "$old_allow" >"$work/base-pairs-$base_rev" || die "could not read $allow as it stood at $base_rev."
    old_count=$(wc -l <"$work/base-pairs-$base_rev" | tr -d ' ')
    new_count=$(wc -l <"$new_pairs_file" | tr -d ' ')
    if [ "$new_count" -gt $((old_count - stale)) ]; then
        printf 'lint-allow-ratchet: FAIL — %s declares a key-scheme migration and grew the allow list:\n' "$label" >&2
        printf '  %s tolerated pairs at %s, %s of them reported clean, %s tolerated pairs in %s.\n' \
            "$old_count" "$base_rev" "$stale" "$new_count" "$label" >&2
        cat >&2 <<EOF

  A key-scheme migration may re-key every row. It may not add one: every row
  in the new list must trace back to a row the old list already held. Remove
  whatever this migration added beyond a faithful re-key.
EOF
        exit 1
    fi
}

work=$(mktemp -d) || die "could not create a temporary directory"
trap 'rm -rf "$work"' EXIT

# ---------------------------------------------------------------------------
# Window 1 — the working tree against HEAD.
# ---------------------------------------------------------------------------
git show "HEAD:$allow" >"$work/head-raw" 2>/dev/null || die \
    "could not read $allow at HEAD.
  Either the list is untracked or this checkout has no HEAD. Both leave the
  question unanswered, and an unanswered ratchet is a failure, not a pass."
if is_legacy "$work/head-raw"; then
    legacy_pairs "$allow" >"$work/now" || die "could not verify the legacy-list migration in $allow"
    legacy_pairs "$work/head-raw" >"$work/head" || die "could not read the legacy list at HEAD"
else
    pairs "$allow" >"$work/now" || die "could not read $allow"
    pairs "$work/head-raw" >"$work/head" || die "could not read $allow as it stands at HEAD."
fi

# A declared key-scheme change re-hashes essentially every row, so a text
# compare of $work/now against $work/head would read the whole list as added.
# transition_check is the only path that can judge that case; when the scheme
# has not moved, the cheap compare below is unchanged from before this branch
# existed.
now_scheme=$(scheme_version "$allow")
head_scheme=$(scheme_version "$work/head-raw")
if [ "$now_scheme" != "$head_scheme" ]; then
    transition_check HEAD "$work/now" "the working tree"
else
    added_uncommitted=$(comm -23 "$work/now" "$work/head")
    if [ -n "$added_uncommitted" ]; then
        printf 'lint-allow-ratchet: FAIL — the allow list gained a pair in the working tree:\n\n' >&2
        printf '%s\n' "$added_uncommitted" | sed 's/^/  + /' >&2
        cat >&2 <<EOF

  The allow list only ever gets shorter. Adding a pair to it is the one repair
  that is not allowed, because it keeps the finding and hides it from every
  later run.

  Fix the finding, or leave the build red and report it.
EOF
        exit 1
    fi
fi

# ---------------------------------------------------------------------------
# Window 2 — HEAD against the union of its parents.
# ---------------------------------------------------------------------------
parents=$(git rev-parse 'HEAD^@' 2>/dev/null)
[ -n "$parents" ] || die "HEAD has no parent, so there is no earlier list to
  compare against. In a real checkout this means the history is shallow. Fetch
  the full history: an unresolvable base is inconclusive, never a pass."

: >"$work/parents"
base_found=0
legacy_parent=0
for parent in $parents; do
    # A parent that does not carry the list tolerated nothing on that side of
    # the history, so it contributes nothing to the union. A parent that
    # carries an EMPTY list is a base, and an empty base is the goal state —
    # so the two cases are tracked apart.
    if git cat-file -e "$parent:$allow" 2>/dev/null; then
        base_found=1
        git show "$parent:$allow" >"$work/parent-$parent" || die \
            "could not read $allow at $parent."
        if is_legacy "$work/parent-$parent"; then legacy_parent=1; fi
    fi
done
if [ "$legacy_parent" -eq 1 ]; then
    legacy_pairs "$work/head-raw" >"$work/head" || die "could not verify the committed legacy-list migration"
    for parent in $parents; do
        [ -f "$work/parent-$parent" ] || continue
        legacy_pairs "$work/parent-$parent" >>"$work/parents" || die "could not read $allow as it stands at $parent"
    done
else
    for parent in $parents; do
        [ -f "$work/parent-$parent" ] || continue
        pairs "$work/parent-$parent" >>"$work/parents" || die "could not read $allow as it stands at $parent"
    done
fi
sort -u "$work/parents" -o "$work/parents"

if [ "$base_found" -eq 0 ]; then
    die "no parent of HEAD carries $allow, so every pair in it reads as new.
  This is the ratchet failing to find a base rather than a real regression."
fi

# Same reasoning as window 1, applied to the committed side: if any parent
# declares a different key scheme than HEAD, HEAD is the migration commit and
# a text compare against that parent cannot judge it. Mixed schemes across
# parents of one merge pick the last differing parent as the base; a merge
# that carries a migration on more than one side at once is outside what this
# check was built to judge.
head_scheme=$(scheme_version "$work/head-raw")
parents_scheme_changed=0
migration_parent=""
for parent in $parents; do
    [ -f "$work/parent-$parent" ] || continue
    if [ "$(scheme_version "$work/parent-$parent")" != "$head_scheme" ]; then
        parents_scheme_changed=1
        migration_parent="$parent"
    fi
done
if [ "$parents_scheme_changed" -eq 1 ]; then
    transition_check "$migration_parent" "$work/head" "the commit that landed the key-scheme migration"
else
    added_committed=$(comm -23 "$work/head" "$work/parents")
    if [ -n "$added_committed" ]; then
        printf 'lint-allow-ratchet: FAIL — the top commit added a pair to the allow list:\n\n' >&2
        printf '%s\n' "$added_committed" | sed 's/^/  + /' >&2
        cat >&2 <<EOF

  The allow list only ever gets shorter. Remove the pair and fix the finding it
  is hiding.
EOF
        exit 1
    fi
fi

printf 'lint-allow-ratchet: PASS — %s tolerated pairs, none added.\n' "$(wc -l <"$work/now" | tr -d ' ')"
