#!/usr/bin/env bash
# Checks that the reviewer PROMPT carries every check and every flag the
# reviewer CONTRACT declares.
#
# Two artifacts describe the Tier-1 reviewer, and only one of them reaches a
# model:
#
#   .claude/skills/agent-reviewer/SKILL.md  — the contract a human or a subagent
#                                             reads.
#   .claude/skills/agent-reviewer/run       — the script that builds the prompt
#                                             sent to `claude -p`. This is what
#                                             the automated reviewer sees.
#
# A check added to SKILL.md alone never reaches the running reviewer. That is
# not a style problem. The reviewer that approved a 1412-line deletion as a
# "clean diff" was answering a prompt that named six checks; the contract by
# then declared eight (hk-2h2fa).
#
# So this test derives the expected checks and flags FROM SKILL.md and requires
# the run script to carry them. It does not hold a copy of either list. Adding a
# check to the contract turns this red until the prompt carries it too.

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
skill="$repo_root/.claude/skills/agent-reviewer/SKILL.md"
runner="$repo_root/.claude/skills/agent-reviewer/run"

fail() {
    echo "agent-reviewer-prompt-parity-test: $1" >&2
    exit 1
}

[[ -f "$skill" ]] || fail "missing contract: $skill"
[[ -f "$runner" ]] || fail "missing prompt script: $runner"

# ── The checks the contract declares ─────────────────────────────────────────
# Numbered headings under "## Tier-1 reviewer responsibilities", e.g.
#   ### 3. Test adequacy
# The heading text is the identity of the check. A prompt that renumbers to the
# right count without carrying the text is not carrying the check.
mapfile -t check_titles < <(
    sed -n '/^## Tier-1 reviewer responsibilities/,/^## Flag vocabulary/p' "$skill" \
        | sed -n 's/^### [0-9]\{1,\}\. \(.*\)$/\1/p'
)

(( ${#check_titles[@]} > 0 )) || fail "no numbered checks found in the contract"

# ── The flags the contract declares ──────────────────────────────────────────
# Rows of the flag-vocabulary table: | `tag` | when to use |
mapfile -t flags < <(
    sed -n '/^## Flag vocabulary/,/^## Output format/p' "$skill" \
        | sed -n 's/^| `\([a-z0-9-]\{1,\}\)` |.*/\1/p'
)

(( ${#flags[@]} > 0 )) || fail "no flag vocabulary found in the contract"

# ── The prompt the runner builds ─────────────────────────────────────────────
# Ask the runner to BUILD its prompt and print it. Reading the script text
# instead was tried and it is not a test: a check name pasted into a comment
# satisfies it while the model still never sees the name. The prompt has to be
# the thing under test, so it has to be generated.
#
# --print-prompt builds the prompt and returns without calling a model, so this
# stays a few milliseconds and costs nothing.
prompt="$("$runner" --print-prompt 2>/dev/null)" || fail "runner does not support --print-prompt"
[[ -n "$prompt" ]] || fail "runner printed an empty prompt"

# ── Every check must be named in the prompt ──────────────────────────────────
# The prompt must carry each check's heading TEXT, not a paraphrase and not a
# number. Paraphrase matching was tried first and it lies in both directions:
# it passed "Production wire-up" as if it were "Production call-site wiring",
# and it failed "Bead / codename match" against "Bead/codename match" over a
# slash. Neither answer is worth having. Verbatim headings cost the prompt
# nothing and make drift impossible to misread.
#
# Both sides are normalized the same way — lowercased, every non-alphanumeric
# run folded to a single space — so punctuation and line wrapping do not
# decide the verdict.
normalize() {
    tr '[:upper:]' '[:lower:]' \
        | sed 's/[^a-z0-9]/ /g; s/  */ /g' \
        | tr -d '\n' \
        | sed 's/  */ /g; s/^ //; s/ $//'
}

prompt_norm="$(printf '%s' "$prompt" | normalize)"

missing_checks=()
for title in "${check_titles[@]}"; do
    key="$(printf '%s' "$title" | normalize)"
    case "$prompt_norm" in
        *"$key"*) ;;
        *) missing_checks+=("$title") ;;
    esac
done

# ── Every flag must be offered to the model ──────────────────────────────────
# A flag the prompt never names is a flag the reviewer cannot raise.
missing_flags=()
for flag in "${flags[@]}"; do
    case "$prompt" in
        *"$flag"*) ;;
        *) missing_flags+=("$flag") ;;
    esac
done

# ── The contract's own check count must match its sections ───────────────────
# The contract states the count in prose ("Perform all eight checks in order")
# and again in the example invocation prompt. A section added without updating
# those tells the reviewer to stop early.
declared_words="$(grep -o 'Perform all [a-z]\{3,\} checks' "$skill" | head -1 | awk '{print $3}')"
count_word() {
    case "$1" in
        1) echo one ;; 2) echo two ;; 3) echo three ;; 4) echo four ;;
        5) echo five ;; 6) echo six ;; 7) echo seven ;; 8) echo eight ;;
        9) echo nine ;; 10) echo ten ;; *) echo "$1" ;;
    esac
}
expected_word="$(count_word "${#check_titles[@]}")"

status=0

if (( ${#missing_checks[@]} > 0 )); then
    echo "agent-reviewer-prompt-parity-test: the prompt in ${runner#"$repo_root"/} does not carry these contract checks:" >&2
    printf '  - %s\n' "${missing_checks[@]}" >&2
    status=1
fi

if (( ${#missing_flags[@]} > 0 )); then
    echo "agent-reviewer-prompt-parity-test: the prompt does not offer these contract flags:" >&2
    printf '  - %s\n' "${missing_flags[@]}" >&2
    status=1
fi

if [[ "$declared_words" != "$expected_word" ]]; then
    echo "agent-reviewer-prompt-parity-test: the contract says 'Perform all ${declared_words} checks' but defines ${#check_titles[@]} (${expected_word})" >&2
    status=1
fi

if (( status != 0 )); then
    echo "agent-reviewer-prompt-parity-test: FAILED" >&2
    exit 1
fi

echo "agent-reviewer-prompt-parity-test: passed (${#check_titles[@]} checks, ${#flags[@]} flags)"
