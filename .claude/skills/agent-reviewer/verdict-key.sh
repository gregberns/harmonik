#!/usr/bin/env bash
# .claude/skills/agent-reviewer/verdict-key.sh — the one definition of a verdict
# cache key. Sourced, not run.
#
# TWO scripts resolve a verdict file and they must agree, or the commit gate
# fails closed on every diff:
#
#   .claude/skills/agent-reviewer/run  — writes the verdict
#   scripts/check-verdict.sh           — reads it back and gates the commit
#
# They used to hold two copies of the same hash algorithm, kept in step by a
# comment. That held only while the algorithm never changed. The moment the key
# grew to cover the contract, the writer moved and the reader did not, and
# `make agent-review` refused every commit — the writer stored a verdict under
# one name and the reader looked for another. The reviewer caught it before it
# landed, under the deletion-accounting check it was in the middle of adding.
#
# So the key lives here once and both sides call it.

# agent_reviewer_contract <skill-dir>
#
# The normative section of the contract: the citation rules through the flag
# vocabulary. This is the text the prompt carries, so it is the text the key
# covers. Prose outside this span does not change a review and must not
# invalidate a cached verdict.
agent_reviewer_contract() {
    local skill_md="$1/SKILL.md"
    [[ -f "$skill_md" ]] || {
        echo "agent-reviewer: contract not found: $skill_md" >&2
        return 1
    }
    local contract
    contract="$(sed -n '/^## Citing code in a normative doc/,/^## Output format/p' "$skill_md" \
        | sed '$d')"
    [[ -n "$contract" ]] || {
        echo "agent-reviewer: contract section is empty in $skill_md" >&2
        return 1
    }
    printf '%s\n' "$contract"
}

# agent_reviewer_verdict_key <skill-dir> <diff>
#
# A verdict is an answer to a question, so the key covers both the diff and the
# contract the diff was judged against. Keying on the diff alone kept serving
# verdicts produced by an older, shorter contract: a newly added check would
# never run against anything already reviewed.
agent_reviewer_verdict_key() {
    local skill_dir="$1"
    local diff="$2"
    local contract
    contract="$(agent_reviewer_contract "$skill_dir")" || return 1

    if command -v sha256sum &>/dev/null; then
        printf '%s\n%s' "$contract" "$diff" | sha256sum | cut -c1-16
    else
        printf '%s\n%s' "$contract" "$diff" | shasum -a 256 | cut -c1-16
    fi
}

# agent_reviewer_diff <ref>
#
# Working-tree diff first, then the committed diff. Both sides must compute the
# diff the same way as well as hash it the same way.
agent_reviewer_diff() {
    local ref="$1"
    local diff
    diff="$(git diff "$ref" 2>/dev/null)"
    if [[ -z "$diff" ]]; then
        diff="$(git diff "${ref}..HEAD" 2>/dev/null)"
    fi
    printf '%s' "$diff"
}
