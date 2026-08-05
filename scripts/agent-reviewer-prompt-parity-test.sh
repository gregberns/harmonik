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
# shellcheck disable=SC2016 # \1 is a sed backreference, so the single quotes are
# required. Double quotes would have the shell expand \1 to nothing and the flag
# list would come back empty, which reads as "the contract declares no flags".
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

# ── The prompt must carry the contract, not a list of its headings ───────────
# The two loops above are satisfied by pasting nine headings and seventeen tags
# into the prompt as a bare list. That is the degenerate repair, it was tried,
# and it is exactly the shape that produced this defect: a summary of the
# contract is a second copy of the contract, and second copies drift. So require
# the contract text ITSELF, verbatim, as a substring of the generated prompt.
#
# This is deliberately strict. Reformat the contract and this goes red until the
# prompt carries the new text — which is the whole point.
contract="$(sed -n '/^## Citing code in a normative doc/,/^## Output format/p' "$skill" | sed '$d')"
[[ -n "$contract" ]] || fail "could not extract the contract section from the skill"

if [[ "$prompt" != *"$contract"* ]]; then
    echo "agent-reviewer-prompt-parity-test: the generated prompt does not carry the contract text verbatim." >&2
    echo "  The prompt must include SKILL.md from '## Citing code in a normative doc' up to '## Output format'." >&2
    echo "  A restated or summarized version does not satisfy this — that summary is what drifted." >&2
    status=1
fi

if [[ "$declared_words" != "$expected_word" ]]; then
    echo "agent-reviewer-prompt-parity-test: the contract says 'Perform all ${declared_words} checks' but defines ${#check_titles[@]} (${expected_word})" >&2
    status=1
fi

# ── No numbered check may live outside the extracted range ───────────────────
# The extraction runs from "## Citing code in a normative doc" to "## Output
# format". A check added outside that span is invisible to the prompt and every
# assertion above still passes, because they all read the same range. Compare the
# range's count against the whole file's.
all_checks="$(grep -c '^### [0-9]\{1,\}\. ' "$skill" || true)"
if [[ "$all_checks" != "${#check_titles[@]}" ]]; then
    echo "agent-reviewer-prompt-parity-test: the contract defines ${all_checks} numbered checks but only ${#check_titles[@]} fall inside the span the prompt carries." >&2
    echo "  Checks must live between '## Citing code in a normative doc' and '## Output format'." >&2
    status=1
fi

# ── The MODEL path must use the same prompt ──────────────────────────────────
# Everything above reads --print-prompt. On its own that is a hole, and a
# reviewer found it: hand-write a two-line prompt in the branch that calls the
# model, leave --print-prompt alone, and every assertion above still passes while
# the reviewer is asked nothing. The seam is not the production path.
#
# So drive the real thing. Stub `claude` on PATH in a throwaway repo, let the
# runner invoke it, and read what it actually sent.
model_path_check() {
    local tmp bin repo captured reply out
    local diff_marker="PARITY-DIFF-REACHED-THE-MODEL"
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' RETURN

    bin="$tmp/bin"
    repo="$tmp/repo"
    captured="$tmp/captured-prompt"
    reply="$tmp/reply"
    mkdir -p "$bin" "$repo"

    # The stub records stdin and answers with a canned response.
    cat > "$bin/claude" <<STUB
#!/usr/bin/env bash
cat > "$captured"
cat "$reply"
STUB
    chmod +x "$bin/claude"

    # The canned response carries Go snippets with braces BEFORE the verdict.
    # The extractor used to match first-brace-to-last, so this response made it
    # hard-fail. The contract it now ships is full of such snippets, so a
    # reviewer quoting one is ordinary, not exotic.
    cat > "$reply" <<'REPLY'
Findings per check:
2. Idiom compliance — the diff writes `defer func() { _ = f.Close() }()`, a finding.
   A type assertion must be `if v, ok := x.(T); ok { use(v) }`.

{"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["idiom-violation"],"notes":"Close is discarded."}
REPLY

    git -C "$repo" init -q
    git -C "$repo" config user.email parity@test.invalid
    git -C "$repo" config user.name parity
    echo one > "$repo/f.txt"
    git -C "$repo" add f.txt
    git -C "$repo" commit -q --no-gpg-sign -m "seed"
    echo "$diff_marker" >> "$repo/f.txt"

    # A throwaway repo under mktemp, not one of this project's worktrees, so the
    # never-cd rule does not apply. The runner resolves its git root from cwd.
    out="$(cd "$repo" && PATH="$bin:$PATH" "$runner" --diff HEAD 2>/dev/null)" || {
        echo "agent-reviewer-prompt-parity-test: the model path failed against a stubbed model." >&2
        echo "  The canned reply puts Go braces before the verdict — extraction must find the verdict anyway." >&2
        return 1
    }

    if [[ "$out" != *'"verdict":"REQUEST_CHANGES"'* ]]; then
        echo "agent-reviewer-prompt-parity-test: the model path did not return the stubbed verdict; got: ${out:0:200}" >&2
        return 1
    fi

    [[ -s "$captured" ]] || {
        echo "agent-reviewer-prompt-parity-test: the runner sent nothing to the model." >&2
        return 1
    }

    # Substring containment over a multi-line block, via bash pattern matching.
    # `grep -F` is the wrong tool and it silently passed this mutation: with a
    # multi-line pattern, grep treats every line as its OWN pattern and reports a
    # match when any single line hits. A two-line hand-written prompt that happens
    # to contain one blank line satisfied it.
    local sent
    sent="$(cat "$captured")"
    if [[ "$sent" != *"$contract"* ]]; then
        echo "agent-reviewer-prompt-parity-test: what the runner SENT TO THE MODEL does not carry the contract." >&2
        echo "  --print-prompt agreeing with the contract is not enough; the model path must use the same builder." >&2
        return 1
    fi

    # The DIFF has to arrive too, in full. Asserting only that the contract is
    # carried leaves the other half of the review unchecked: swapping
    # `git diff` for `git diff --stat`, or truncating the diff body, keeps every
    # other assertion green while the reviewer judges a summary instead of the
    # change. The seeded line below appears in the diff content and in no
    # boilerplate, so its absence means the change itself did not reach the model.
    if [[ "$sent" != *"$diff_marker"* ]]; then
        echo "agent-reviewer-prompt-parity-test: the DIFF did not reach the model intact." >&2
        echo "  Expected the changed line '${diff_marker}' in what was sent." >&2
        echo "  A stat summary or a truncated body is not the change under review." >&2
        return 1
    fi
    return 0
}

model_path_check || status=1

# ── A cached verdict must not outlive the contract it answered ───────────────
# The cache keys on the diff AND the contract. Before that, every verdict cached
# under the old six-check prompt was still served for an identical diff, so a
# newly added check would never run against anything already reviewed.
#
# Runs against a COPY of the skill directory, because the assertion needs to
# change the contract and must not touch the real one. The copy is byte-identical,
# so it is the same runner.
cache_key_check() {
    local tmp bin repo skillcopy reply first second
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' RETURN

    bin="$tmp/bin"
    repo="$tmp/repo"
    skillcopy="$tmp/skill"
    reply="$tmp/reply"
    mkdir -p "$bin" "$repo"
    cp -R "$(dirname "$runner")" "$skillcopy"

    cat > "$bin/claude" <<STUB
#!/usr/bin/env bash
cat > /dev/null
cat "$reply"
STUB
    chmod +x "$bin/claude"

    git -C "$repo" init -q
    git -C "$repo" config user.email parity@test.invalid
    git -C "$repo" config user.name parity
    echo one > "$repo/f.txt"
    git -C "$repo" add f.txt
    git -C "$repo" commit -q --no-gpg-sign -m "seed"
    echo two >> "$repo/f.txt"

    echo '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"first."}' > "$reply"
    first="$(cd "$repo" && PATH="$bin:$PATH" "$skillcopy/run" --diff HEAD 2>/dev/null)" || return 1
    [[ "$first" == *'"verdict":"APPROVE"'* ]] || {
        echo "agent-reviewer-prompt-parity-test: cache probe did not get the first verdict" >&2
        return 1
    }

    # Same diff, different answer available. The contract has changed, so the
    # cached answer must not be reused.
    #
    # The edit has to land INSIDE the span that is hashed. A first version of this
    # appended to the end of SKILL.md and failed against a correct runner: text
    # after '## Output format' is not part of the contract, so it does not change
    # the key and it should not. That is the runner behaving right.
    echo '{"schema_version":1,"verdict":"BLOCK","flags":["orphaned-reader"],"notes":"second."}' > "$reply"
    awk '
        { print }
        /^### 1\. Spec alignment$/ && !seen { print ""; print "Added by the cache-key check."; seen = 1 }
    ' "$skillcopy/SKILL.md" > "$tmp/skill.md.new"
    mv "$tmp/skill.md.new" "$skillcopy/SKILL.md"

    # A BLOCK verdict makes the runner exit 1 by design, so the exit status is not
    # the signal here — the emitted verdict is. Treating exit 1 as a crash made an
    # earlier version of this check fail silently against a correct runner.
    second="$(cd "$repo" && PATH="$bin:$PATH" "$skillcopy/run" --diff HEAD 2>/dev/null)" || true
    [[ -n "$second" ]] || {
        echo "agent-reviewer-prompt-parity-test: the second cache probe emitted nothing" >&2
        return 1
    }
    if [[ "$second" != *'"verdict":"BLOCK"'* ]]; then
        echo "agent-reviewer-prompt-parity-test: a verdict cached under the OLD contract was served after the contract changed." >&2
        echo "  The cache key must cover the contract, not the diff alone." >&2
        return 1
    fi
    return 0
}

cache_key_check || status=1

# ── The verdict WRITER and the verdict READER must resolve the same file ─────
# `run` writes .harmonik/verdicts/<key>.json and scripts/check-verdict.sh reads
# it back to gate the commit. They are separate scripts, so a change to the key
# in one orphans the other, and the symptom is not subtle: the gate refuses every
# commit, because the writer stored a verdict under one name and the reader looked
# for another.
#
# That is exactly what happened when the key grew to cover the contract, and the
# deletion-accounting check caught it. This assertion is that check, made
# mechanical, so the next change to the key does not need a reviewer to notice.
writer_reader_agree_check() {
    local tmp bin repo reply out
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' RETURN

    bin="$tmp/bin"
    repo="$tmp/repo"
    reply="$tmp/reply"
    mkdir -p "$bin" "$repo"

    cat > "$bin/claude" <<STUB
#!/usr/bin/env bash
cat > /dev/null
cat "$reply"
STUB
    chmod +x "$bin/claude"
    echo '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"agreed."}' > "$reply"

    # check-verdict.sh finds the key definition under the repo it is run in, so
    # the throwaway repo needs the skill directory in place.
    mkdir -p "$repo/.claude/skills" "$repo/scripts"
    cp -R "$(dirname "$runner")" "$repo/.claude/skills/agent-reviewer"
    cp "$repo_root/scripts/check-verdict.sh" "$repo/scripts/check-verdict.sh"

    git -C "$repo" init -q
    git -C "$repo" config user.email parity@test.invalid
    git -C "$repo" config user.name parity
    echo one > "$repo/f.txt"
    git -C "$repo" add f.txt
    git -C "$repo" commit -q --no-gpg-sign -m "seed"
    echo two >> "$repo/f.txt"

    # Write a verdict, then ask the reader for it. Same diff, same tree.
    (cd "$repo" && PATH="$bin:$PATH" ./.claude/skills/agent-reviewer/run --diff HEAD >/dev/null 2>&1) || {
        echo "agent-reviewer-prompt-parity-test: the writer failed while probing writer/reader agreement" >&2
        return 1
    }

    out="$(cd "$repo" && ./scripts/check-verdict.sh --diff HEAD 2>&1)" || {
        echo "agent-reviewer-prompt-parity-test: the verdict WRITER and the verdict READER disagree." >&2
        echo "  agent-reviewer/run stored a verdict and scripts/check-verdict.sh could not find it:" >&2
        printf '  %s\n' "$out" >&2
        echo "  Both must resolve the key through .claude/skills/agent-reviewer/verdict-key.sh." >&2
        return 1
    }
    return 0
}

writer_reader_agree_check || status=1

# ── A broken reviewer must refuse, never approve ─────────────────────────────
# `run` sets no shell options, so a failed source does not stop it. When that
# happened the diff came back empty, the empty-diff branch fired, and the gate
# emitted APPROVE with exit 0 against a real diff having never called a model.
# A review gate that fails open is worse than no gate: it produces a trailer that
# says the work was reviewed.
fails_closed_check() {
    local tmp repo out
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' RETURN

    repo="$tmp/repo"
    mkdir -p "$repo/.claude/skills"
    cp -R "$(dirname "$runner")" "$repo/.claude/skills/agent-reviewer"

    git -C "$repo" init -q
    git -C "$repo" config user.email parity@test.invalid
    git -C "$repo" config user.name parity
    echo one > "$repo/f.txt"
    git -C "$repo" add f.txt
    git -C "$repo" commit -q --no-gpg-sign -m "seed"
    echo two >> "$repo/f.txt"
    git -C "$repo" add f.txt
    git -C "$repo" commit -q --no-gpg-sign -m "a real change"

    rm "$repo/.claude/skills/agent-reviewer/verdict-key.sh"

    # No stubbed `claude` on PATH either — nothing here may reach a model.
    if out="$(cd "$repo" && ./.claude/skills/agent-reviewer/run --diff HEAD~1 2>/dev/null)"; then
        echo "agent-reviewer-prompt-parity-test: a BROKEN reviewer exited 0 against a real diff." >&2
        echo "  It emitted: ${out:0:160}" >&2
        echo "  The gate must fail closed when it cannot build a review." >&2
        return 1
    fi

    if [[ "$out" == *'"verdict":"APPROVE"'* ]]; then
        echo "agent-reviewer-prompt-parity-test: a broken reviewer emitted APPROVE." >&2
        return 1
    fi
    return 0
}

fails_closed_check || status=1

# ── The contract must be read as the STANDARD, not as material under review ──
# Eighth mutation, found by a reviewer: move the contract inside the fenced diff
# block and every assertion above still passes while the model reads the contract
# as part of the change it is judging.
if [[ "$prompt" == *"$contract"* ]]; then
    before_diff="${prompt%%'## Diff (git diff'*}"
    if [[ "$before_diff" != *"$contract"* ]]; then
        echo "agent-reviewer-prompt-parity-test: the contract appears AFTER the diff section starts." >&2
        echo "  It must precede the diff, or the model reads the standard as material under review." >&2
        status=1
    fi
fi

if (( status != 0 )); then
    echo "agent-reviewer-prompt-parity-test: FAILED" >&2
    exit 1
fi

echo "agent-reviewer-prompt-parity-test: passed (${#check_titles[@]} checks, ${#flags[@]} flags)"
