#!/usr/bin/env bash
# validate-commit-msg-test.sh — the assertions for scripts/validate-commit-msg.sh.
#
# The validator decides whether a commit message may land, and for a while it
# decided it backwards. It rejected the one honest thing an author can write
# when no reviewer could be reached, and it accepted an APPROVE attributed to a
# name that exists nowhere in this repository. An author who runs the gate and
# sees the honest sentence go red and the unattributable approval go green is
# being taught to write down a verdict nobody gave. These cases exist so that
# shape cannot come back without the build going red.
#
# Every case drives the REAL script against a real message file. There is no
# stub of the validator here — a test that re-implements the thing under test
# passes for the wrong reason. The one case that does not run the script in
# place (CASE 21) runs a byte-for-byte copy of it, and says why.
#
# Each case names what it would catch. If you cannot say what a case catches,
# it is not an assertion, it is decoration.

set -uo pipefail

repo_root=$(git rev-parse --show-toplevel) || {
    echo "validate-commit-msg-test: not inside a git worktree" >&2
    exit 1
}
cd "$repo_root" || exit 1

VALIDATOR="$repo_root/scripts/validate-commit-msg.sh"
[ -x "$VALIDATOR" ] || {
    echo "validate-commit-msg-test: $VALIDATOR is missing or not executable" >&2
    exit 1
}

work=$(mktemp -d) || exit 1
trap 'rm -rf "$work"' EXIT

failures=0
assertions=0

fail() {
    printf 'validate-commit-msg-test: FAIL: %s\n' "$*" >&2
    failures=$((failures + 1))
}

pass() {
    printf 'validate-commit-msg-test: ok: %s\n' "$*"
}

# run_validator <message> — writes the message to a file, runs the real
# validator, leaves its output in $work/out and returns its exit status.
run_validator() {
    printf '%s\n' "$1" >"$work/msg"
    "$VALIDATOR" "$work/msg" >"$work/out" 2>&1
}

# expect_pass <name> <message>
expect_pass() {
    assertions=$((assertions + 1))
    if run_validator "$2"; then
        pass "$1"
    else
        fail "$1 — validator rejected a message it must accept"
        sed 's/^/    /' "$work/out" >&2
    fi
}

# expect_fail <name> <expected-substring> <message>
#
# Both halves matter. "The validator exited non-zero" is satisfied for free by a
# validator that died for an unrelated reason, so each negative case also names
# the message it must produce.
expect_fail() {
    assertions=$((assertions + 1))
    if run_validator "$3"; then
        fail "$1 — validator ACCEPTED a message it must reject"
    elif grep -qF "$2" "$work/out"; then
        pass "$1"
    else
        fail "$1 — validator rejected it, but not for the stated reason (wanted: $2)"
        sed 's/^/    /' "$work/out" >&2
    fi
}

# ---------------------------------------------------------------------------
# CASE 1 — the honest no-reviewer form passes.
#
# The rule in AGENTS.md says a commit whose reviewer could not be reached
# records that fact and lands anyway. Before NOT_REVIEWED was in the enum this
# was the ONE shape the validator refused, and commits in this repository
# already carry it (`git log --grep=NOT_REVIEWED`). Delete NOT_REVIEWED from the
# enum and this goes red.
# ---------------------------------------------------------------------------
expect_pass "the honest no-reviewer form passes" \
'fix(gate): stop a stranded item from reading as drained

Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": ["no-reviewer-reached"], "notes": "No reviewer was reachable from this session, so this trailer records that absence and carries no approval."}'

# ---------------------------------------------------------------------------
# CASE 2 — the honest form is not held to the approval bar.
#
# NOT_REVIEWED says outright that no reviewer ran, so there is no reviewer name
# to check and no flags key to demand. If the approval checks ever leak onto
# this path the honest form gets more expensive than the dishonest one, which
# is the whole defect this file guards.
# ---------------------------------------------------------------------------
expect_pass "the honest form needs no reviewer name and no flags key" \
'docs(specs): record the gap the guard left open

Reviewed-By: none
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "notes": "Documents only. No reviewer was reached."}'

# ---------------------------------------------------------------------------
# CASE 3 — a well-formed APPROVE passes.
#
# The bar has to be clearable, or the fix has just moved the pressure. This is
# exactly what `AppendReviewTrailersToHEAD` in internal/runmerge stamps on the
# implementer's HEAD commit before it fast-forwards main onto it: the fixed
# value `agent-reviewer` plus the marshalled schema-v1 verdict.
# ---------------------------------------------------------------------------
expect_pass "a well-formed APPROVE passes" \
'feat(s04): add the claude-twin handler adapter

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"All nine checks pass. Diff matches bead scope."}'

# ---------------------------------------------------------------------------
# CASE 4 — an APPROVE naming a reviewer that does not exist fails.
#
# Commits in this history carry an APPROVE attributed to `implement_claim_chain`
# or `implement_queue_chain` (`git log --grep='^Reviewed-By:.*implement_'`).
# Neither name appears in any graph, workflow file or source file here — outside
# this test file, the only place either string exists is a commit message. An
# approval has to name a reviewer this repo actually has.
# ---------------------------------------------------------------------------
expect_fail "an APPROVE with a bogus reviewer name fails" \
    "must name a reviewer skill this repo has" \
'feat(queue): chain the claim through

Reviewed-By: implement_claim_chain
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Looks right."}'

# ---------------------------------------------------------------------------
# CASE 5 — a qualifier after a real reviewer name is refused.
#
# This case asserted the opposite until 2026-08-12, on the reasoning that the
# reviewer runs on more than one harness and refusing the qualifier would push
# authors to strip context off a true statement. What the affordance actually
# bought was a span of free text that no rule read, sitting on the one line the
# approval rules are written about — so the rules could be answered and
# defeated in the same breath. Both of the next two cases were measured passing
# before this changed.
#
# The context has somewhere better to go: `notes`, inside the JSON the reviewer
# emits rather than beside it.
# ---------------------------------------------------------------------------
expect_fail "a qualifier after a real reviewer name is refused" \
    "must name a reviewer skill this repo has" \
'fix(daemon): honor run_id in the worktree path

Reviewed-By: agent-reviewer (codex harness, fresh context)
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Checked against the workspace spec."}'

# ---------------------------------------------------------------------------
# CASE 5a — a qualifier cannot smuggle in a reviewer this repo does not have.
#
# 27 commits on this branch carry an APPROVE from a reviewer named nowhere in
# the repository. Written bare, that name is refused. Written after a real one
# it used to pass, and it read to a human as an attribution to the name in the
# parentheses.
# ---------------------------------------------------------------------------
expect_fail "a qualifier cannot smuggle in an unknown reviewer" \
    "must name a reviewer skill this repo has" \
'fix(daemon): honor run_id in the worktree path

Reviewed-By: agent-reviewer (Kierkegaard)
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Checked against the workspace spec."}'

# ---------------------------------------------------------------------------
# CASE 5b — an author saying the approval is their own no longer passes.
#
# This is the sharper of the two, and it is worth saying exactly which rule
# catches it, because the obvious answer is wrong. The self-authorship check is
# a word match on "self", and "myself" does not match it — the letter before
# "self" is a letter, so the word boundary fails. That check never saw this
# line. What refuses it is the name rule: the value is not exactly a reviewer
# this repo has.
#
# So the self-authorship check is narrower than its name suggests, and the
# exact-name rule is what closes the gap around it. Assert the reason and not
# only the exit code, or this case would read as evidence for a check that did
# not run.
# ---------------------------------------------------------------------------
expect_fail "an author claiming their own approval in a qualifier is refused" \
    "must name a reviewer skill this repo has" \
'fix(daemon): honor run_id in the worktree path

Reviewed-By: agent-reviewer (myself)
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Checked against the workspace spec."}'

# ---------------------------------------------------------------------------
# CASE 5c — the bare reviewer name, which is now the only accepted form.
# ---------------------------------------------------------------------------
expect_pass "a bare reviewer name passes" \
'fix(daemon): honor run_id in the worktree path

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Checked against the workspace spec."}'

# ---------------------------------------------------------------------------
# CASE 6 — a self-authored APPROVE fails.
#
# The rule says quote the reviewer's verdict and never author your own
# approval. This is a word check, not proof: it catches the author who says
# plainly that the approval is their own.
# ---------------------------------------------------------------------------
expect_fail "a self-authored APPROVE fails" \
    "must not be self-authored" \
'refactor(keeper): fold the two threshold paths together

Reviewed-By: self
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Read it twice."}'

# ---------------------------------------------------------------------------
# CASE 7 — a missing `notes` field fails.
#
# schema v1 requires notes in every statement of it: the agent-reviewer skill,
# the config-reviewer skill, and the Go reader in internal/workspace
# (`parseReviewVerdict`), which rejects a verdict file with absent or empty
# notes. This validator was the one reader that let it through, and a large
# minority of the APPROVE trailers already in this history have no notes key at
# all.
# ---------------------------------------------------------------------------
expect_fail "a verdict with no notes field fails" \
    "missing the required 'notes' field" \
'feat(cli): add the promote subcommand

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[]}'

# ---------------------------------------------------------------------------
# CASE 8 — an empty `notes` field fails.
#
# Requiring the key alone would be satisfied by "notes": "". The field carries
# the reason for the verdict, so it has to say something.
# ---------------------------------------------------------------------------
expect_fail "a verdict with empty notes fails" \
    "empty 'notes' field" \
'feat(cli): add the promote subcommand

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"   "}'

# ---------------------------------------------------------------------------
# CASE 9 — an APPROVE with no `flags` key fails.
#
# The reviewer skill always emits flags, and the Go marshaller always writes
# it. Demanding it of an approval and not of the honest form is one more place
# where the true statement is the cheaper one to write.
# ---------------------------------------------------------------------------
expect_fail "an APPROVE with no flags key fails" \
    "must carry the 'flags' key" \
'feat(cli): add the promote subcommand

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","notes":"All checks pass."}'

# ---------------------------------------------------------------------------
# CASE 10 — a null verdict fails, and the message names the honest spelling.
#
# This history holds both shapes: trailers that were cut off just after
# `"verdict": null`, and trailers that parse cleanly with a null verdict. On the
# page they open the same way, so an absence written as null cannot be told
# apart from an absence caused by breakage. It has to be said in words.
# ---------------------------------------------------------------------------
expect_fail "a null verdict fails as null, not as an unknown enum value" \
    "Review-Verdict 'verdict' is null" \
'chore(gate): take the sub-agent branch

Reviewed-By: none reached — this session was directed not to spawn
Review-Verdict: {"schema_version":1,"verdict":null,"flags":[],"notes":"No reviewer was reached."}'

# ---------------------------------------------------------------------------
# CASE 11 — BLOCK still never lands.
#
# Carried over from the behaviour that was already right. It is here because
# the enum was rewritten around it.
# ---------------------------------------------------------------------------
expect_fail "a BLOCK verdict still cannot be committed" \
    "BLOCK verdict must not be committed" \
'feat(s04): add the adapter

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"BLOCK","flags":["spec-divergence"],"notes":"Return a typed alias per the handler contract."}'

# ---------------------------------------------------------------------------
# CASE 12 — REQUEST_CHANGES lands without clearing the approval bar.
#
# A verdict that is not an approval claims nothing that needs guarding. If the
# reviewer-name check ever widens to every verdict, this goes red.
# ---------------------------------------------------------------------------
expect_pass "REQUEST_CHANGES lands with a reviewer name outside the known set" \
'fix(workspace): widen the verdict read retry

Reviewed-By: Codex independent review
Review-Verdict: {"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["missing-tests"],"notes":"No test covers the truncated-read path."}'

# ---------------------------------------------------------------------------
# CASE 13 — the config-reviewer CLEAN verdict is held to the approval bar too.
#
# CLEAN is that reviewer's "nothing to fix", so it carries the same claim an
# APPROVE does and gets the same treatment.
# ---------------------------------------------------------------------------
expect_pass "a CLEAN verdict from the config reviewer passes" \
'chore(agents): sync the skill registry

Reviewed-By: agent-config-reviewer
Review-Verdict: {"schema_version":1,"verdict":"CLEAN","flags":[],"notes":"No drift between the embed and the checked-in copy.","proposed_diff":""}'

expect_fail "a CLEAN verdict with an unknown reviewer fails" \
    "must name a reviewer skill this repo has" \
'chore(agents): sync the skill registry

Reviewed-By: decompose_queue_specs
Review-Verdict: {"schema_version":1,"verdict":"CLEAN","flags":[],"notes":"No drift."}'

# ---------------------------------------------------------------------------
# CASE 14 — notes text cannot shift the other fields.
#
# The parser hands four fields back as one tab-separated record. A notes string
# holding a tab would move the columns and hand a later check a value the JSON
# never said. This message is an approval whose ONLY defect is the missing
# flags key — the field a shift would land on — so the shift is what decides
# the outcome. Case 17 runs it again through the Python fallback, which has no
# @tsv escaping doing the work for it and is where this actually bites.
# ---------------------------------------------------------------------------
expect_fail "a tab inside notes cannot shift the flags field out of view" \
    "must carry the 'flags' key" \
'feat(queue): chain the claim through

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","notes":"one\tarray\tok\tarray\ttwo"}'

# ---------------------------------------------------------------------------
# CASE 15 — a truncated trailer fails.
#
# Commits here end mid-object because the trailer was wrapped. An unparseable
# trailer must never read as a verdict.
# ---------------------------------------------------------------------------
expect_fail "a truncated JSON trailer fails" \
    "not valid JSON" \
'docs(plans): record the analysis

Reviewed-By: none reached
Review-Verdict: {"schema_version": 1, "verdict": null, "reviewed": false,'

# ---------------------------------------------------------------------------
# CASE 16 — the trivial bypass still bypasses.
#
# The whole trailer block is optional on a typo fix. Every rule added above
# runs after this branch, so a mistake in the wiring shows up here.
# ---------------------------------------------------------------------------
expect_pass "the Trivial: true bypass still skips the trailer rules" \
'docs(readme): fix a typo in the boot order

Trivial: true'

# ---------------------------------------------------------------------------
# CASE 17 — the Python fallback agrees with jq.
#
# The validator parses with jq when it is there and with python3 when it is
# not. Two parsers, one contract. This runs the honest form and a bogus
# approval with jq hidden from PATH; a fallback that drifted would show up as
# an accepted approval or a rejected honest form.
# ---------------------------------------------------------------------------
if command -v python3 >/dev/null 2>&1; then
    nojq="$work/nojq"
    mkdir -p "$nojq"
    # A PATH with the real tools but no jq: symlink everything the validator
    # needs, and leave jq out.
    for tool in bash env sh grep sed awk printf tr basename dirname git python3 cat; do
        real=$(command -v "$tool" 2>/dev/null) && ln -sf "$real" "$nojq/$tool"
    done

    assertions=$((assertions + 1))
    printf '%s\n' 'fix(gate): stop a stranded item from reading as drained

Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reachable."}' >"$work/msg"
    if PATH="$nojq" "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
        pass "without jq, the Python fallback accepts the honest form"
    else
        fail "without jq, the Python fallback REJECTED the honest form"
        sed 's/^/    /' "$work/out" >&2
    fi

    assertions=$((assertions + 1))
    printf '%s\n' 'feat(queue): chain the claim through

Reviewed-By: implement_claim_chain
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Looks right."}' >"$work/msg"
    if PATH="$nojq" "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
        fail "without jq, the Python fallback ACCEPTED an approval with a bogus reviewer"
    elif grep -qF "must name a reviewer skill this repo has" "$work/out"; then
        pass "without jq, the Python fallback rejects an approval with a bogus reviewer"
    else
        fail "without jq, the fallback rejected the bogus approval for the wrong reason"
        sed 's/^/    /' "$work/out" >&2
    fi

    assertions=$((assertions + 1))
    printf '%s\n' 'feat(cli): add the promote subcommand

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[]}' >"$work/msg"
    if PATH="$nojq" "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
        fail "without jq, the Python fallback ACCEPTED a verdict with no notes"
    elif grep -qF "missing the required 'notes' field" "$work/out"; then
        pass "without jq, the Python fallback rejects a verdict with no notes"
    else
        fail "without jq, the fallback rejected the notes-less verdict for the wrong reason"
        sed 's/^/    /' "$work/out" >&2
    fi

    # A whitespace-only notes value on the fallback. The two parsers reach the
    # `empty` state by different routes — jq asks `test("\\S")`, python3 asks
    # `re.search(r'\S', ...)` — so "the key is there" is the answer a drifted
    # fallback gives, and only this case sees it. Without it, a fallback that
    # calls empty notes `ok` passes the whole suite.
    assertions=$((assertions + 1))
    printf '%s\n' 'feat(cli): add the promote subcommand

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"   "}' >"$work/msg"
    if PATH="$nojq" "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
        fail "without jq, the Python fallback ACCEPTED a verdict with empty notes"
    elif grep -qF "empty 'notes' field" "$work/out"; then
        pass "without jq, the Python fallback rejects a verdict with empty notes"
    else
        fail "without jq, the fallback rejected the empty-notes verdict for the wrong reason"
        sed 's/^/    /' "$work/out" >&2
    fi

    # The tab-in-notes message again, on the parser that has no @tsv escaping
    # doing the work for it. If the fallback ever passes the notes text through
    # instead of reducing it to a state word, the columns shift here.
    assertions=$((assertions + 1))
    printf '%s\n' 'feat(queue): chain the claim through

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","notes":"one\tarray\tok\tarray\ttwo"}' >"$work/msg"
    if PATH="$nojq" "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
        fail "without jq, a tab inside notes shifted the fields and hid a missing flags key"
    elif grep -qF "must carry the 'flags' key" "$work/out"; then
        pass "without jq, a tab inside notes cannot shift the fields"
    else
        fail "without jq, the tab-in-notes message failed for the wrong reason"
        sed 's/^/    /' "$work/out" >&2
    fi
else
    echo "validate-commit-msg-test: python3 absent; skipping the fallback cases" >&2
fi

# ---------------------------------------------------------------------------
# CASE 18 — the subject rules still hold.
#
# Two of them, kept because the parser rewrite sits in the same file and a
# broken early return would take the whole subject block with it.
# ---------------------------------------------------------------------------
expect_fail "a subject outside the type set still fails" \
    "Conventional Commits" \
'wibble(gate): do a thing

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"fine"}'

expect_fail "a subject ending in a period still fails" \
    "must not end with a period" \
'fix(gate): do a thing.

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"fine"}'

# ---------------------------------------------------------------------------
# CASE 19 — valid JSON that is not an object fails.
#
# Most of the bad trailers in this history are a bare word where the object
# should be, and the closest thing to that which still parses is the word in
# quotes. A JSON string, number or array carries no schema_version, no verdict
# and no notes, so every field check below would read the same empty value and
# say nothing useful. Both parsers answer NOTOBJ here and the trailer is
# refused once, by shape. Delete either NOTOBJ branch and this goes red.
# ---------------------------------------------------------------------------
expect_fail "a JSON string where the verdict object belongs fails" \
    "valid JSON but not a JSON object" \
'feat(queue): chain the claim through

Reviewed-By: agent-reviewer
Review-Verdict: "APPROVE"'

expect_fail "a JSON array where the verdict object belongs fails" \
    "valid JSON but not a JSON object" \
'feat(queue): chain the claim through

Reviewed-By: agent-reviewer
Review-Verdict: [{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"fine"}]'

# ---------------------------------------------------------------------------
# CASE 20 — a notes state the parser never promised is refused, not ignored.
#
# The four words ok / empty / badtype / absent are what the parser is meant to
# hand back. The `Review-Verdict:` key with nothing after it produces none of
# them: jq is handed an empty document, returns no record at all, and the read
# leaves every field blank. Without the fail-closed default arm the empty notes
# state falls through every branch and the notes rule simply does not run — the
# validator says nothing about the field it was added to protect.
#
# jq only. Given the same empty value the python3 fallback raises on the parse
# and refuses the trailer one step earlier, which is a different message and an
# equally closed door.
# ---------------------------------------------------------------------------
if command -v jq >/dev/null 2>&1; then
    expect_fail "an unreadable notes state is refused rather than skipped" \
        "unreadable 'notes' state" \
'feat(cli): add the promote subcommand

Reviewed-By: agent-reviewer
Review-Verdict:'
else
    echo "validate-commit-msg-test: jq absent; skipping the unreadable-notes-state case" >&2
fi

# ---------------------------------------------------------------------------
# CASE 21 — the reviewer list falls back to names, never to "anything goes".
#
# known_reviewers reads `.claude/skills/*reviewer*` off the filesystem so that
# adding a reviewer skill is enough. The validator is callable from anywhere,
# including a bare worktree with no skills directory, and there the list comes
# back empty. An empty list compared against with `==` matches nothing, which
# would reject every approval; an empty list treated as "no opinion" would
# accept every approval. Neither is right, so the function falls back to the two
# known names. This copies the script byte-for-byte to a directory outside any
# repository and runs it there, which is the only place that branch is
# reachable. It is the one case in this file that does not run the script in
# place, and the copy is the point: the script has to work where its skills
# directory does not.
# ---------------------------------------------------------------------------
bare="$work/bare/scripts"
mkdir -p "$bare"
cp "$VALIDATOR" "$bare/validate-commit-msg.sh"
chmod +x "$bare/validate-commit-msg.sh"

if git -C "$bare" rev-parse --show-toplevel >/dev/null 2>&1; then
    # The scratch copy landed inside some git worktree, so it can still find a
    # .claude/skills directory and the fallback would not be what is under test.
    echo "validate-commit-msg-test: scratch dir is inside a git worktree; skipping the known_reviewers fallback cases" >&2
else
    assertions=$((assertions + 1))
    printf '%s\n' 'feat(s04): add the claude-twin handler adapter

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"All nine checks pass."}' >"$work/msg"
    if "$bare/validate-commit-msg.sh" "$work/msg" >"$work/out" 2>&1; then
        pass "with no skills directory, a real reviewer name is still accepted"
    else
        fail "with no skills directory, the fallback REJECTED a real reviewer name"
        sed 's/^/    /' "$work/out" >&2
    fi

    assertions=$((assertions + 1))
    printf '%s\n' 'feat(queue): chain the claim through

Reviewed-By: implement_claim_chain
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Looks right."}' >"$work/msg"
    if "$bare/validate-commit-msg.sh" "$work/msg" >"$work/out" 2>&1; then
        fail "with no skills directory, an unreadable reviewer list became a free pass"
    elif grep -qF "must name a reviewer skill this repo has" "$work/out"; then
        pass "with no skills directory, a bogus reviewer name is still rejected"
    else
        fail "with no skills directory, the bogus reviewer failed for the wrong reason"
        sed 's/^/    /' "$work/out" >&2
    fi
fi

# ---------------------------------------------------------------------------
printf 'validate-commit-msg-test: %d assertions, %d failures\n' "$assertions" "$failures"
[ "$failures" -eq 0 ] || exit 1
echo "validate-commit-msg-test: PASS"
