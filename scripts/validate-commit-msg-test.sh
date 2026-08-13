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
# CASE 12a — a non-approving verdict may not name the author either.
#
# CASE 12 above says this arm accepts a reviewer name the repo does not ship,
# and that is right. It is not a licence to name YOURSELF. A REQUEST_CHANGES
# trailer asserts that a reviewer read the change, the same claim an APPROVE
# makes, and the audit that counts reviewed commits is a grep for the trailer
# key and a name — it never reads the verdict value.
#
# THE COUNT, AND IT IS SMALL — say the small one. Over `main..HEAD`, 986 commits
# carry a `Reviewed-By:` line and 128 of those values name the author. But 119
# of the 128 sit on APPROVE or CLEAN, where the exact-name rule already refused
# the value before this case existed. Only 9 sat on the arm this case pins:
# 8 DRIFT_MINOR and 1 REQUEST_CHANGES, six of the eight reading
# `self (agent-config-reviewer …)`. Quoting 128 here would have made the gap
# look fourteen times larger than it is. This recipe prints the commit total,
# the Reviewed-By total, the self-naming total and the split by verdict — the
# 119 and the 9 are sums of that split. It does NOT re-derive the
# `six of the eight` clause above, which needs the reviewer VALUES and not
# their counts: for that one, print `v, rev` in place of `cnt[v]++` — printing
# the bare value drops the verdict label, which leaves `of the eight` to
# inference. The recipe is meant to be pasted whole and to run as written:
#
#   git log main..HEAD --format='%x02%H%n%B' | awk -v RS='\002' '
#   NR>1 {
#     rev=""; ver=""
#     n=split($0, L, "\n")
#     for (i=1; i<=n; i++) {
#       if (rev=="" && L[i] ~ /^Reviewed-By:/)    rev=L[i]
#       if (ver=="" && L[i] ~ /^Review-Verdict:/) ver=L[i]
#     }
#     total++
#     if (rev=="") next
#     carry++
#     if (tolower(rev) !~ /(^|[^a-z])self([^a-z]|$)/) next
#     selfn++
#     v="(none)"
#     if (match(ver, /"verdict"[ ]*:[ ]*"[A-Z_]+"/)) {
#       s=substr(ver, RSTART, RLENGTH); match(s, /[A-Z_]+"$/)
#       v=substr(s, RSTART, RLENGTH-1)
#     }
#     cnt[v]++
#   }
#   END {
#     printf "commits %d / carry Reviewed-By %d / self-naming %d\n", total, carry, selfn
#     for (k in cnt) printf "  %-16s %d\n", k, cnt[k]
#   }'
#
# It takes the FIRST `^Reviewed-By:` per commit, keeps the values matching
# /(^|[^a-z])self([^a-z]|$)/ case-folded, and groups them by the `verdict` field
# of that commit's `^Review-Verdict:` line.
#
# SAY WHEN THE COUNT WAS TAKEN, because `main..HEAD` is machine-local and moves
# under you: measured 2026-08-13 at 6576ba60c, it printed 1187 commits, 986
# carrying the line, 128 self-naming, split {APPROVE 116, CLEAN 3,
# DRIFT_MINOR 8, REQUEST_CHANGES 1}. A later run that disagrees is not
# evidence this comment was wrong — re-read it against that revision first.
#
# A SEPARATE TRAP, worth spelling out because it is silent: awk above does its
# own matching, but any hand variant of this that reaches for `grep` should say
# /usr/bin/grep. A grep that does not match this pattern returns 0, and a 0 here
# reads as "nothing to fix" rather than as a broken command — the same shape of
# failure as the recipe that could not match at all. An earlier session recorded
# an interactive `grep` resolving to ugrep and producing exactly that 0; no ugrep
# is installed on this machine and that specific cause was NOT reproducible here,
# so treat the tool name as unconfirmed and the rule as cheap either way. The
# validator itself is unaffected, and that half IS confirmed: it runs under
# non-interactive bash, which does not see an interactive shell's alias.
#
# WHAT THE 128 ACTUALLY SHOW, which is worth more than the 9: most are not a
# bare `self` but a sentence — `agent-reviewer (self-applied — no Agent tool
# available in this session)` and its variants. They say the author followed a
# harness instruction not to spawn a sub-agent, applied the reviewer's own
# checklist to their work, and recorded it honestly. That is a real thing to
# have done. It is also not an independent review, and NOT_REVIEWED is the
# verdict for it.
#
# HOW THESE TWO CASES DISCRIMINATE. Take the check out of the arm and both go
# RED. Replace it with the WIDER exact-name rule and both stay green — CASE 12
# and the DRIFT case are what go red there. So the pair pins the narrow rule,
# and it takes all four cases together to say which rule this arm holds.
# ---------------------------------------------------------------------------
expect_fail "a self-authored REQUEST_CHANGES is refused" \
    "must not be self-authored" \
'fix(workspace): widen the verdict read retry

Reviewed-By: self
Review-Verdict: {"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["missing-tests"],"notes":"No test covers the truncated-read path."}'

expect_fail "a self-review dressed as a harness name is refused on a DRIFT verdict" \
    "must not be self-authored" \
'chore(agents): sync the skill registry

Reviewed-By: Codex self-review
Review-Verdict: {"schema_version":1,"verdict":"DRIFT_MAJOR","flags":["skill-registry-drift"],"notes":"The embed and the checked-in copy disagree."}'

# ---------------------------------------------------------------------------
# CASE 12b — the flags key stays OPTIONAL on the non-approving verdicts.
#
# `APPROVE|CLEAN)` holds two requirements, not one: the reviewer identity, and
# a `flags` key that is present and an array. Only the identity half was
# pinned against a widened arm. All five cases for REQUEST_CHANGES, DRIFT_MINOR
# and DRIFT_MAJOR happened to carry a `flags` key, so moving the flags block
# out of the approval arm onto every verdict left this suite green while an
# honest non-approving commit that omits `flags` started being refused.
#
# That direction is the failure this whole file exists to stop: make the
# honest verdicts the expensive ones to write and the author drifts back
# toward APPROVE. NOT_REVIEWED's exemption is pinned by CASE 2. This pins the
# other three. Refs hk-rdjxx.
# ---------------------------------------------------------------------------
expect_pass "a non-approving verdict needs no flags key" \
'fix(workspace): widen the verdict read retry

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"REQUEST_CHANGES","notes":"No test covers the truncated-read path."}'

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
# CASE 20 — a `Review-Verdict:` key with nothing after it is named, on both
# parsers, in the same words.
#
# THIS CASE USED TO ASSERT SOMETHING ELSE, and what changed is worth stating.
# It drove the same message and asked for `unreadable 'notes' state` — the
# fail-closed default arm of the notes `case`. It reached that arm by accident:
# jq was handed an empty document, printed no record, and the tab-read left all
# four fields blank, so `notes` held the empty string and fell to the default.
# It was jq-only, because the fallback raised on the parse and refused one step
# earlier with different words. An empty trailer producing two different
# refusals is the divergence this pair of programs exists to remove, so the
# parsers now both count the documents they were given and both say `NOJSON`.
#
# WHAT THAT COSTS, SAID PLAINLY. The default arm of the notes `case` is now
# unreachable through any payload. Every record has four columns, the two free
# ones carry a sentinel for each way they can be empty, the two state ones are
# closed word sets, and NOJSON / MULTIDOC / NOTOBJ are intercepted before the
# read. So no message can drive that arm any more. It is kept — a `case` over a
# parser's output with no default is a `case` that shrugs at a record it could
# not read — and it is covered by reading, not by a test. A test that reached it
# would have to stub the parser, and a suite that re-implements the thing under
# test passes for the wrong reason.
#
# What is asserted instead is stronger than what it replaces: the same message
# is refused by BOTH parsers, with the same words, and the words name the real
# problem rather than a downstream symptom.
# ---------------------------------------------------------------------------
empty_trailer_msg='feat(cli): add the promote subcommand

Reviewed-By: agent-reviewer
Review-Verdict:'

expect_fail "an empty Review-Verdict value is refused and named" \
    "carries no JSON at all" \
    "$empty_trailer_msg"

if [ -n "${nojq:-}" ]; then
    assertions=$((assertions + 1))
    printf '%s\n' "$empty_trailer_msg" >"$work/msg"
    if PATH="$nojq" "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
        fail "without jq, an empty Review-Verdict value was accepted"
    elif grep -qF "carries no JSON at all" "$work/out"; then
        pass "without jq, an empty Review-Verdict value is refused in the same words"
    else
        fail "without jq, the empty Review-Verdict value was refused for the wrong reason"
        sed 's/^/    /' "$work/out" >&2
    fi
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
# A REVIEWER NAME MAY NOT BE A GLOB.
#
# The rule above says an approval must name a reviewer skill that git tracks,
# so that an untracked `mkdir` cannot mint a trusted name. The first version of
# that rule handed the directory name straight to a git pathspec, and a git
# pathspec GLOBS. An empty, untracked directory called `*reviewer` therefore
# matched the REAL agent-reviewer's tracked SKILL.md, and `Reviewed-By:
# *reviewer` on an APPROVE exited 0. The check meant to stop a minted reviewer
# was the way one got minted.
#
# These cases need their own repository: the exploit is a directory beside the
# real reviewer skills, and this one's tree is not a place to put one.
# ---------------------------------------------------------------------------
glob_repo="$work/globrepo"
mkdir -p "$glob_repo/scripts" "$glob_repo/.claude/skills/agent-reviewer"
cp "$VALIDATOR" "$glob_repo/scripts/validate-commit-msg.sh"
chmod +x "$glob_repo/scripts/validate-commit-msg.sh"
printf 'the real reviewer skill\n' >"$glob_repo/.claude/skills/agent-reviewer/SKILL.md"
git -C "$glob_repo" init -q
git -C "$glob_repo" config user.email 'test@example.invalid'
git -C "$glob_repo" config user.name 'validate-commit-msg-test'
git -C "$glob_repo" config commit.gpgsign false
git -C "$glob_repo" add scripts/validate-commit-msg.sh .claude/skills/agent-reviewer/SKILL.md >/dev/null 2>&1
git -C "$glob_repo" commit -q -m 'seed' >/dev/null 2>&1

# The exploit: untracked, empty, and named so that git globs it onto the real one.
mkdir -p "$glob_repo/.claude/skills/*reviewer"

run_in_glob_repo() {
    printf '%s\n' "$1" >"$glob_repo/msg"
    "$glob_repo/scripts/validate-commit-msg.sh" "$glob_repo/msg" >"$work/out" 2>&1
}

assertions=$((assertions + 1))
if run_in_glob_repo 'fix(gate): a subject that is otherwise fine

Reviewed-By: *reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"minted"}'; then
    fail "an untracked directory named with a glob minted a trusted reviewer"
    sed 's/^/    /' "$work/out" >&2
elif grep -qF "must name a reviewer skill this repo has" "$work/out"; then
    pass "a glob-named reviewer directory does not mint a reviewer"
else
    fail "the glob-named reviewer was refused for the wrong reason"
    sed 's/^/    /' "$work/out" >&2
fi

# The negative control for the case above. If the tracked lookup broke
# outright, the case above would pass while proving nothing.
assertions=$((assertions + 1))
if run_in_glob_repo 'fix(gate): a subject that is otherwise fine

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"a real review"}'; then
    pass "the real tracked reviewer name is still accepted in that repository"
else
    fail "the tracked lookup rejected the genuine reviewer name"
    sed 's/^/    /' "$work/out" >&2
fi

# An untracked reviewer skill with an ordinary name is refused too. Nothing
# covered this rule before, which is why the glob got through it.
mkdir -p "$glob_repo/.claude/skills/forged-reviewer"
printf 'a skill nobody reviewed\n' >"$glob_repo/.claude/skills/forged-reviewer/SKILL.md"
assertions=$((assertions + 1))
if run_in_glob_repo 'fix(gate): a subject that is otherwise fine

Reviewed-By: forged-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"minted"}'; then
    fail "an untracked reviewer skill minted a trusted reviewer"
    sed 's/^/    /' "$work/out" >&2
elif grep -qF "must name a reviewer skill this repo has" "$work/out"; then
    pass "an untracked reviewer skill does not mint a reviewer"
else
    fail "the untracked reviewer was refused for the wrong reason"
    sed 's/^/    /' "$work/out" >&2
fi

# STAGED is not TRACKED. `git add` alone used to be enough, and the same agent
# writes the index and the commit message in one operation — so a reviewer
# could be minted with no commit and no review at all.
git -C "$glob_repo" add .claude/skills/forged-reviewer/SKILL.md >/dev/null 2>&1
assertions=$((assertions + 1))
if run_in_glob_repo 'fix(gate): a subject that is otherwise fine

Reviewed-By: forged-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"minted"}'; then
    fail "a merely STAGED reviewer skill minted a trusted reviewer"
    sed 's/^/    /' "$work/out" >&2
elif grep -qF "must name a reviewer skill this repo has" "$work/out"; then
    pass "a staged-but-uncommitted reviewer skill does not mint a reviewer"
else
    fail "the staged reviewer was refused for the wrong reason"
    sed 's/^/    /' "$work/out" >&2
fi

# The same skill, once it is COMMITTED, is accepted — that is the whole point
# of the rule: a reviewer arrives as a diff somebody can decline.
git -C "$glob_repo" commit -q -m 'add a reviewer skill' >/dev/null 2>&1
assertions=$((assertions + 1))
if run_in_glob_repo 'fix(gate): a subject that is otherwise fine

Reviewed-By: forged-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"a real review"}'; then
    pass "a reviewer skill git tracks is accepted"
else
    fail "a tracked reviewer skill was refused, so the rule is not about tracking"
    sed 's/^/    /' "$work/out" >&2
fi

# ---------------------------------------------------------------------------
# A MERGE ANSWERS FOR ITSELF.
#
# The validator recognises a git-written subject so a merge is not failed for
# not being Conventional Commits. That recognition used to be spent twice: it
# also set the trivial-bypass flag, and the trivial-bypass flag gates the whole
# trailer block. So presence, JSON, reviewer identity, the verdict enum and the
# refusal of BLOCK were all skipped for any commit whose subject started with
# `Merge `.
#
# Measured before the split, on the same trailers under two subjects:
#   Merge branch 'work/evil' into main + Reviewed-By: nobody + APPROVE -> exit 0
#   feat(evil): land something         + Reviewed-By: nobody + APPROVE -> exit 1
#
# The asymmetry is the tell. The bypass reads subject TEXT, not parent count, so
# a merge whose subject somebody rewrote by hand was already getting the full
# check, and only one that kept git's default subject went unread. Merges land
# on the integration branch and on main. There was no version of this where the
# ones that keep the default subject are the ones that need no reviewer.
#
# There is no merge case anywhere in this file before these. That is why the
# hole survived a suite this long.
# ---------------------------------------------------------------------------
expect_fail "a merge cannot carry an approval from a reviewer this repo does not have" \
    "must name a reviewer skill this repo has" \
"Merge branch 'work/evil' into main

Reviewed-By: nobody-ran-this
Review-Verdict: {\"schema_version\":1,\"verdict\":\"APPROVE\",\"flags\":[],\"notes\":\"Fabricated.\"}"

expect_fail "a merge with no trailers at all is refused like any other commit" \
    "missing required trailer 'Reviewed-By:'" \
"Merge branch 'work/bravo-lane' into work/alpha-integration-merge"

# The two ways a merge says it needs no reviewer. Both have to work, or the
# rule above is unliveable and the next person deletes it.
expect_pass "a merge that declares itself trivial still passes" \
"Merge branch 'work/bravo-lane' into work/alpha-integration-merge

Trivial: true"

expect_pass "a merge carrying the honest no-reviewer form passes" \
"Merge branch 'work/bravo-lane' into work/alpha-integration-merge

Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {\"schema_version\": 1, \"verdict\": \"NOT_REVIEWED\", \"flags\": [\"no-reviewer-reached\"], \"notes\": \"A lane merge with no reviewer session available.\"}"

# The negative control for all four above. A merge subject is still exempt from
# the three SUBJECT rules — that exemption is correct and is not what changed.
# Without this, folding merges entirely into the normal path would leave every
# case above green while making the validator refuse every real merge.
expect_pass "a merge subject is still exempt from the Conventional Commits rules" \
"Merge remote-tracking branch 'origin/work/a-very-long-branch-name-that-runs-well-past-the-seventy-two-character-subject-limit.

Trivial: true"

# A `fixup!` / `squash!` commit keeps the trailer exemption, and this is the
# line the fix draws. That commit is scratch: `git rebase --autosquash` folds it
# into another one and it never lands under this subject, so there is no landed
# change for a reviewer to be quoted about. A merge lands. The old flag could
# not tell the two apart because it was one flag.
expect_pass "a fixup! commit still needs no trailers" \
'fixup! feat(gate): the merge decision refuses a box that cannot answer'

expect_pass "a squash! commit still needs no trailers" \
'squash! feat(gate): the merge decision refuses a box that cannot answer'

# ---------------------------------------------------------------------------
# A NOT_REVIEWED VERDICT MAY NOT NAME A REVIEWER.
#
# NOT_REVIEWED shared an arm with REQUEST_CHANGES and the DRIFT verdicts, and
# that arm checks nothing about `Reviewed-By:`. So a commit could say "no
# reviewer was reached" in its verdict while the line above it said
# `agent-reviewer`. Two commits on this branch carry that pair
# (4b6a63243, bc98a3dfb).
#
# The cost is not the contradiction, it is what the contradiction does to the
# audit. The approval rules exist so that one grep over the history can list
# every commit attributed to a real reviewer. A commit that names the reviewer
# and disclaims the review in the same breath is counted by that grep and is
# not evidence of anything.
#
# The fix is a check of its own, NOT the approval check. Reusing
# check_approval_identity here asserts the opposite thing — the value MUST be a
# known reviewer — so it refuses `Reviewed-By: none — no reviewer was reached`,
# the exact sentence this repo asks for. The pass case below is the control
# that catches it.
#
# HOW MANY REAL COMMITS THAT TIGHTENING WOULD REFUSE is measured and dated in
# ONE place, the note on check_no_reviewer_named in the validator itself, and
# is deliberately not restated here. It used to be restated here, and the two
# copies disagreed — the validator said 15 commits and this file said 18, for
# the same measurement over the same range. Neither was re-derived after the
# range grew, so the pair rotted into two different wrong numbers, and a reader
# who checked one against the other learned only that one of them was stale.
# ---------------------------------------------------------------------------
expect_fail "a NOT_REVIEWED verdict naming a real reviewer is a contradiction" \
    "must not name a reviewer this repo has" \
'fix(gate): stop a stranded item from reading as drained

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}'

# The parenthetical spelling. An approval is refused for carrying a qualifier at
# all, so nothing before this had to look past one on another verdict — and
# `agent-reviewer (unreachable this session)` reads to a human, and to the
# audit grep, as a reviewer that ran.
expect_fail "a NOT_REVIEWED verdict naming a real reviewer with a note after it is refused" \
    "must not name a reviewer this repo has" \
'fix(gate): stop a stranded item from reading as drained

Reviewed-By: agent-reviewer (unreachable this session)
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}'

# The config reviewer is on the same list, so the same contradiction in its name
# is refused. Reading the list rather than one hard-coded name is what makes
# this true without a second rule.
expect_fail "a NOT_REVIEWED verdict naming the config reviewer is refused too" \
    "must not name a reviewer this repo has" \
'chore(agents): sync the skill registry

Reviewed-By: agent-config-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}'

# THE CONTROL. The honest form has to stay the cheapest thing to write, so the
# new check must refuse a reviewer NAME and nothing else. This goes red the
# moment the contradiction check is spelled as the approval check.
expect_pass "the honest no-reviewer form is untouched by the contradiction check" \
'fix(gate): stop a stranded item from reading as drained

Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": ["no-reviewer-reached"], "notes": "No reviewer was reachable from this session."}'

# Free text that is not a reviewer name is left alone. The check refuses a
# claim, not a wording — a validator that argues with how an author phrases a
# true sentence teaches the author to stop writing it.
expect_pass "a NOT_REVIEWED verdict may describe the absence in its own words" \
'fix(gate): stop a stranded item from reading as drained

Reviewed-By: nobody — the session was ending and had no sub-agents
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reachable."}'

# REQUEST_CHANGES naming the real reviewer is TRUTHFUL and must keep passing:
# the reviewer ran and declined. This is the COMMON shape in this history, not
# an edge case — measured 2026-08-13, 120 commits carry `agent-reviewer` on the
# Reviewed-By trailer beside a REQUEST_CHANGES verdict:
#
#   git log --format=%H | while read s; do
#     m=$(git log -1 --format=%B "$s")
#     grep -q '^Reviewed-By:.*agent-reviewer' <<<"$m" &&
#       grep -q '"verdict"[[:space:]]*:[[:space:]]*"REQUEST_CHANGES"' <<<"$m" &&
#       echo "$s"
#   done | wc -l
#
# The command is here because the number that used to stand alone on this line
# said 69, and 69 does not come back from any reading of this history — the
# trailer-anchored count is 120 and the loosest count that matches the words
# anywhere in a message is 170. A bare number nobody can re-derive is not
# evidence, and it cannot even be shown to be stale.
#
# BOTH FIGURES ARE A READING TAKEN ON A DATE, and the date is the load-bearing
# half. Both only ever rise, and both rose while this comment was being
# written — the anchored command above returned 122 later the same day, and
# the loose count moved with it. That is the reason the pair is here at all:
# 120 against 170 is a CONTRAST measured at ONE MOMENT, and the gap between
# the two readings is what shows 69 came from neither. The gap is the claim;
# the digits are the perishable half.
#
# 69 was not a figure that decayed, and this is checkable rather than
# rhetorical. It has one introducing commit, 96fa11a73, and the anchored
# command above run AGAINST THAT COMMIT returns 122 — so 69 was not a reading
# of this history at the moment it was written either. Note that this figure,
# unlike the two above it, does NOT rot: `git log <fixed-sha>` walks ancestors
# only, so re-running it against 96fa11a73 returns 122 in a year as well. A
# count pinned to a commit is durable; a count pinned to a date is not. That is
# the difference worth copying.
#
# So do not bump these one at a time as you notice them — that produces a pair
# measured on two different days, which is not a contrast and not evidence of
# anything. Re-run both commands together and re-date the line, or take the
# digits out and let the commands carry the point on their own.
# CASE 12 above uses a name outside the known set, so it cannot see a check that
# widened onto this arm and only refuses known names — this one can.
expect_pass "REQUEST_CHANGES naming the real reviewer still passes" \
'fix(workspace): widen the verdict read retry

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["missing-tests"],"notes":"No test covers the truncated-read path."}'

expect_pass "DRIFT_MINOR naming the real config reviewer still passes" \
'chore(agents): sync the skill registry

Reviewed-By: agent-config-reviewer
Review-Verdict: {"schema_version":1,"verdict":"DRIFT_MINOR","flags":["skill-registry-drift"],"notes":"The embed and the checked-in copy differ by one line.","proposed_diff":"-a\n+b"}'

# DRIFT_MAJOR is the third verdict on the same path and it was the one with no
# case at all. All three truthful non-approving verdicts are pinned now —
# REQUEST_CHANGES, DRIFT_MINOR, DRIFT_MAJOR — because they are the verdicts an
# author writes when a reviewer DID run and did not approve, and making any of
# them harder to write pushes the author back toward APPROVE.
#
# CLEAN IS NOT ONE OF THEM. It is the config reviewer's APPROVE, and the
# validator says so in one branch: `APPROVE|CLEAN)` holds both to the approval
# bar. Measured on `Reviewed-By: decompose_queue_specs`, a name this repo does
# not have: under CLEAN it is refused, under DRIFT_MAJOR it passes.
#
# BOTH HALVES ARE PINNED NOW, AND IN TWO DIFFERENT PLACES. CASE 13 holds the
# CLEAN half: two cases, both CLEAN, one naming the real config reviewer and
# one naming `decompose_queue_specs` and refused for it. The DRIFT half is the
# last case in this block — the same unknown name under DRIFT_MAJOR, expected
# to PASS. Together they pin the `APPROVE|CLEAN)` arm from both sides, so a
# reader can cite one case for one half and neither claim outruns its evidence.
#
# Until that case existed only the CLEAN side was held. The two DRIFT cases
# above it both name the REAL reviewer, so they pass on the approval arm's
# behaviour as much as on their own and cannot discriminate the boundary; if
# `APPROVE|CLEAN)` widened onto the DRIFT arm tomorrow, nothing here would have
# gone red and honest non-approving commits would start being refused. An
# earlier draft of this paragraph cited CASE 13 for both halves anyway, which
# is a measurement that is correct attached to a citation that does not carry
# it.
#
# This note used to count CLEAN among the four, which put one verdict on both
# sides of the line the whole section is drawing.
expect_pass "DRIFT_MAJOR naming the real config reviewer still passes" \
'chore(agents): sync the skill registry

Reviewed-By: agent-config-reviewer
Review-Verdict: {"schema_version":1,"verdict":"DRIFT_MAJOR","flags":["enforced-config-drift"],"notes":"The linter config and the idiom list disagree; this needs acknowledgment before the pass advances.","proposed_diff":"-a\n+b"}'

# THE OTHER SIDE OF THE `APPROVE|CLEAN)` BOUNDARY. Same unknown name CASE 13
# refuses under CLEAN, here under DRIFT_MAJOR, and it must PASS: a reviewer who
# declined has not been minted as an approver, so a non-approving verdict may
# name anyone. This goes red if that branch ever widens onto the DRIFT arm —
# which would make the honest verdicts the expensive ones to write and push the
# author back toward APPROVE, the failure this whole file exists to prevent.
expect_pass "DRIFT_MAJOR may name a reviewer outside the known set" \
'chore(agents): sync the skill registry

Reviewed-By: decompose_queue_specs
Review-Verdict: {"schema_version":1,"verdict":"DRIFT_MAJOR","flags":["enforced-config-drift"],"notes":"The linter config and the idiom list disagree; this needs acknowledgment before the pass advances.","proposed_diff":"-a\n+b"}'

# ---------------------------------------------------------------------------
# THE CONTRADICTION CHECK READS ALL THE TEXT, NOT ONE PARSED-OUT VALUE.
#
# Every negative case in the block above hands the check a value that is
# EXACTLY a reviewer name, or exactly `name (note)`. That shape is what an
# equality comparison sees, and an equality comparison is what the first
# version of the check used — so a green suite of those cases said nothing
# about any other shape. Two whole classes walked through it.
#
# The first is the sharper one, and it is worth stating as a lesson rather than
# as a case: `REVIEWED_BY` is a grep, so it holds EVERY `Reviewed-By:` line the
# message has. Two lines can never equal one name. Under APPROVE that
# non-equality REFUSES the commit — measured, the same duplication under
# APPROVE exits 1 — and under NOT_REVIEWED the identical non-equality ACCEPTED
# it. A mirror-image check inherited its twin's comparison, and the direction
# it failed in flipped with it. That is the thing to look for the next time two
# checks are written as a pair.
#
# The second is ordinary decoration. A trailing period, a comma either side, a
# bracket, a dash and a sentence — none of them is equal to a name, and the
# ones that leave the name at the FRONT of the value still answer
# `git log --grep 'Reviewed-By: agent-reviewer'`, which is the audit this whole
# rule exists to keep honest. A comma or a bracket BEFORE the name moves it out
# of that grep's reach, so those shapes are refused without the audit asking
# for it. The over-refusal note below sorts which is which. The test of a
# refusal here is not "is this value a reviewer name" but "would this line be
# counted as reviewed work by somebody grepping the history".
# ---------------------------------------------------------------------------

# A second Reviewed-By line, in both orders. Neither is a parse error and
# neither is caught by any rule above; a message may carry two trailers.
expect_fail "a duplicate Reviewed-By line cannot smuggle the reviewer in after the honest form" \
    "must not name a reviewer this repo has" \
'fix(gate): stop a stranded item from reading as drained

Reviewed-By: none — no reviewer was reached for this commit
Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}'

expect_fail "a duplicate Reviewed-By line cannot smuggle the reviewer in before it" \
    "must not name a reviewer this repo has" \
'fix(gate): stop a stranded item from reading as drained

Reviewed-By: agent-reviewer
Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}'

# The decoration set. Each of these exited 0 against the equality comparison.
not_reviewed_msg() {
    printf '%s' 'fix(gate): stop a stranded item from reading as drained

Reviewed-By: '"$1"'
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}'
}

for decorated in \
    'agent-reviewer.' \
    'agent-reviewer,' \
    ',agent-reviewer' \
    'agent-reviewer — never ran' \
    'agent-reviewer was not reachable' \
    'agent-reviewer [unreachable]' \
    'agent-reviewer (codex) (unreachable)' \
    '(agent-reviewer)' \
    'AGENT-REVIEWER' \
    'none (agent-reviewer down)' \
    'not agent-reviewer' \
    'x-agent-reviewer'
do
    expect_fail "a NOT_REVIEWED verdict is refused for naming the reviewer as '${decorated}'" \
        "must not name a reviewer this repo has" \
        "$(not_reviewed_msg "$decorated")"
done

# SIX OF THE TWELVE ARE OVER-REFUSALS, and they are not the trailing run of
# the list — the audit sorts them, not their position. `git log --grep
# 'Reviewed-By: agent-reviewer'` is a literal, case-sensitive search, so it
# counts a shape only when the name survives EXACTLY and at the front of the
# value. These six do not: `,agent-reviewer` and `(agent-reviewer)` put a
# character in front of the name, `AGENT-REVIEWER` is at the front but in the
# wrong case, and `none (agent-reviewer down)`, `not agent-reviewer` and
# `x-agent-reviewer` all carry text before it. Refusing those six is the
# validator being stricter than the audit it answers to. Re-derive without a
# scratch repository: for each shape, ask whether `Reviewed-By: <shape>`
# contains the literal text `Reviewed-By: agent-reviewer`. An earlier note
# here said the last three. That named a trailing run, which is not the line
# the audit draws, and it missed three of the six. They are pinned anyway, for
# two reasons: they are what the check actually does, and a comment in the
# validator that claimed free text
# was left alone was wrong about exactly these. They fail in the honest
# direction and the refusal prints the honest form to write instead. Relaxing
# them means reading intent out of prose, which is how `agent-reviewer2` got
# through in the first place.
#
# The `(agent-reviewer)` and `agent-reviewer (codex) (unreachable)` cases are
# the strip misfiring rather than a shape nobody thought of, and they are why
# the fix stopped stripping and started searching.
# `${base%\(*}` takes the shortest match from the end, so it removed only the
# FINAL parenthetical and left `agent-reviewer (codex)` to compare; and when the
# whole value was parenthesised it left an EMPTY string, which is unequal to
# every name and so passed.

# THE CONTROLS. A wider match is the easy way to make every case above green
# and the rule unusable, so the honest spellings are pinned here — ten of them,
# in one place, next to the cases that could break them.
#
# Ten, not one, because the refusal above works by SEARCHING the text rather
# than by comparing it to a value, and a search is widened by accident. Every
# spelling here is a true sentence that names no reviewer, and `git log --grep
# 'Reviewed-By: agent-reviewer'` counts none of them — which is the same test
# the refusals are judged by, pointed the other way. Recording that no reviewer
# was reached is REQUIRED by AGENTS.md, so a false refusal here does the exact
# damage this file exists to prevent: it makes the honest sentence the
# expensive one to write.
for honest in \
    'none — no reviewer was reached for this commit' \
    'none' \
    'nobody — session ending' \
    'no reviewer was reached' \
    'nobody' \
    'n/a' \
    '(none)' \
    'NONE' \
    'unreachable' \
    'none reached — this session had no sub-agents to review with'
do
    expect_pass "the honest form '${honest}' is still accepted" \
        "$(not_reviewed_msg "$honest")"
done

# ---------------------------------------------------------------------------
# THE NAME BOUNDARY IS ASYMMETRIC, BECAUSE THE AUDIT IS.
#
# The decoration set above pins punctuation around the name. It says nothing
# about a letter or a digit stuck to the END of it, and the check used to
# require a non-alphanumeric character on BOTH sides. So four shapes exited 0:
#
#     Reviewed-By: agent-reviewer2
#     Reviewed-By: agent-reviewers
#     Reviewed-By: agent-reviewerx
#     Reviewed-By: agent-reviewer0
#
# Every one of them answers `git log --grep 'Reviewed-By: agent-reviewer'`,
# which is the audit this whole rule exists to keep honest. Measured on a
# scratch repository holding all nine shapes below as real commits, that grep
# returns exactly these four and none of the four in the control loop.
#
# THE NINTH SHAPE ANSWERS TO A DIFFERENT AUDIT, which is why it is not in
# either count above. `agent-config-reviewer9` does not contain the text
# `Reviewed-By: agent-reviewer`, so the grep named here never sees it. It is
# refused because this repo has a SECOND reviewer and therefore a second audit,
# `git log --grep 'Reviewed-By: agent-config-reviewer'`, which that shape does
# answer. Four suffixes plus four controls is eight; the ninth is here to say
# the rule is per reviewer name, not per the one name this section happens to
# be about.
#
# The trailing boundary is therefore gone and the LEADING one stays, and the
# two loops here are the statement of that asymmetry. Delete the leading
# boundary as well and the control loop goes red, because `xxagent-reviewer`
# is NOT counted by the audit and refusing it would be a false fail on a name
# that makes no claim. The test to apply to any change here is not "is this a
# reviewer name" — it is "would somebody grepping the history count this line
# as reviewed work".
# ---------------------------------------------------------------------------
for suffixed in \
    'agent-reviewer2' \
    'agent-reviewers' \
    'agent-reviewerx' \
    'agent-reviewer0' \
    'agent-config-reviewer9'
do
    expect_fail "a NOT_REVIEWED verdict is refused for naming the reviewer as '${suffixed}'" \
        "must not name a reviewer this repo has" \
        "$(not_reviewed_msg "$suffixed")"
done

# THE CONTROLS FOR THE LOOP ABOVE. A prefix does not answer the audit grep, so
# refusing one would be a false fail. These are what stops the fix for the
# suffixes from being spelled as "match the name anywhere".
for prefixed in \
    'xxagent-reviewer' \
    'notagent-reviewer' \
    '9agent-reviewer' \
    'superagent-reviewer'
do
    expect_pass "a name the audit does not count, '${prefixed}', is still accepted" \
        "$(not_reviewed_msg "$prefixed")"
done

# ---------------------------------------------------------------------------
# THE LEADING PAD IS THE OTHER HALF OF THAT BOUNDARY, AND IT HAD NO CASE.
#
# The prefixed control loop directly above pins the boundary REQUIREMENT — the
# `[!a-z0-9]` in `*[!a-z0-9]"$known_lc"*` — by demanding that `xxagent-reviewer`
# still PASS, which it cannot if the pattern matches the name anywhere. (The
# suffixed loop before it pins the opposite thing: that no such character is
# required AFTER the name.) Neither pins what SUPPLIES that character when the
# name sits at index 0 of the folded haystack, which is the one leading space
# in `padded=" ${hay} "`.
#
# Measured in a detached worktree at 6dbeb5c5a: delete that single space and
# this file still reported all 173 assertions it held that day passing, while
# the message below — refused before the mutation — exited 0. The count is
# pinned to a commit rather than to a date, so it stays re-derivable: take the
# file back with `git show 6dbeb5c5a:scripts/validate-commit-msg-test.sh` and
# run it. (`git log` alone cannot produce an assertion count — it is the
# shorthand the 120/69 block uses for the durability discipline, not a command
# that answers this.) The claim is that a green suite said nothing about the
# guard.
#
# THE TWO SPACES ARE TOLD APART BY TWO DIFFERENT MUTATIONS, not by one reading.
# Delete the LEADING space and this case is the only failure in the file.
# Delete the TRAILING one and everything stays green, this case included — it
# is inert, and provably so rather than by sampling: the pattern ends in `*`,
# so nothing ever reads the end of the string. Do not expect this case to
# notice the trailing space; nothing does, and nothing needs to.
#
# THIS SHAPE DOES NOT ANSWER THE AUDIT GREP, AND SAYING SO IS THE POINT.
# `git log --grep 'Reviewed-By: agent-reviewer'` does not count
# `agent-reviewer Reviewed-By: none` — the name is in front of the key, not
# after it. Nor could any case do better, and the reason is structural rather
# than a gap in imagination: the key-strip above replaces `reviewed-by:` with a
# SPACE, so every occurrence the audit DOES count arrives with a boundary
# already in front of it and matches with the pad or without it. A name reaches
# index 0 only when the first `Reviewed-By:` line STARTS with it, which is
# exactly the shape that grep walks past.
#
# So this belongs with the six over-refusals in the decoration set, and shares
# their first reason: it is what the check does. The second reason is its own —
# the rule it keeps is positional consistency, that a reviewer name inside a
# `Reviewed-By:` line is refused wherever in the line it sits. Without the pad,
# index 0 alone is exempt, nothing in the check says so, and an author who
# finds that hole gets a commit the contradiction check was written to refuse.
# ---------------------------------------------------------------------------
expect_fail "a reviewer name at the very start of the haystack is still refused" \
    "must not name a reviewer this repo has" \
'fix(scope): a perfectly ordinary subject

Body text here.

agent-reviewer Reviewed-By: none
Reviewed-By: none — no reviewer was reached
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": ["no-reviewer-reached"], "notes": "No reviewer was reached."}'

# ---------------------------------------------------------------------------
# THE CAPTURE MUST NOT BE NARROWER THAN THE AUDIT IT DEFENDS.
#
# `REVIEWED_BY` is captured with an ANCHORED `grep -E '^Reviewed-By:'`. The
# audit is an UNANCHORED substring match. One leading space therefore hid a
# trailer from the validator and left it in plain sight of the audit:
#
#     Reviewed-By: none — no reviewer was reached
#       Reviewed-By: agent-reviewer
#
# That message exited 0, and `git log --grep 'Reviewed-By: agent-reviewer'`
# counts it. The duplicate-trailer cases above could not see this: both of
# their lines start in column one, so the anchored capture held both and the
# check had all the text it needed. Indentation is what takes the text away.
#
# The fix re-derives the haystack for the contradiction check alone, unanchored
# over the whole message. It does NOT widen the shared capture — see the two
# controls under this block for the reason.
# ---------------------------------------------------------------------------
expect_fail "an indented second Reviewed-By line cannot hide the reviewer from the check" \
    "must not name a reviewer this repo has" \
'fix(gate): stop a stranded item from reading as drained

Reviewed-By: none — no reviewer was reached for this commit
  Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}'

expect_fail "a tab-indented second Reviewed-By line is caught too" \
    "must not name a reviewer this repo has" \
"fix(gate): stop a stranded item from reading as drained

Reviewed-By: none — no reviewer was reached for this commit
$(printf '\t')Reviewed-By: agent-reviewer
Review-Verdict: {\"schema_version\": 1, \"verdict\": \"NOT_REVIEWED\", \"flags\": [], \"notes\": \"No reviewer was reached.\"}"

expect_fail "the indented line is caught in the other order as well" \
    "must not name a reviewer this repo has" \
'fix(gate): stop a stranded item from reading as drained

  Reviewed-By: agent-reviewer
Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}'

# THE CONTROL FOR THE CHOICE OF FIX. Widening the SHARED capture was the other
# way to close the hole, and it would have reached the presence check and
# check_approval_identity — which demands the value be EXACTLY a reviewer name.
# An honest APPROVE that quotes the string `Reviewed-By:` in its prose would
# then hold two lines, fail that equality, and be refused. This case goes red
# the moment somebody widens the shared capture instead.
expect_pass "an APPROVE whose body quotes the trailer key in prose still passes" \
'docs(gate): explain what the trailer rule asks for

The rule says a landed commit carries a Reviewed-By: line naming the reviewer
that ran, and a Review-Verdict: line quoting what it said.

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Documentation only; the wording matches the validator."}'

# The consequence of the same widening, stated on purpose rather than left to be
# discovered. Prose that puts the reviewer name directly after the trailer key
# answers the audit grep — the commit is COUNTED as reviewed — so a NOT_REVIEWED
# verdict carrying that prose is refused, wherever in the message it sits. The
# remedy is to write the name without the key in front of it: "the
# agent-reviewer skill could not be reached" carries no `Reviewed-By:` and is
# left alone.
expect_fail "prose that answers the audit grep is refused under NOT_REVIEWED too" \
    "must not name a reviewer this repo has" \
'fix(gate): stop a stranded item from reading as drained

I tried to get Reviewed-By: agent-reviewer onto this commit and could not.

Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}'

# ---------------------------------------------------------------------------
# THE 72-CHARACTER SUBJECT CEILING.
#
# The rule has been in the validator from the start and nothing asserted it.
# Measured: raising the constant to 7200 left this whole suite green, so a
# regression in it would have been silent. Both halves are here — the ceiling
# and the value that sits exactly on it — because a case that only proves "long
# subjects fail" is satisfied by a validator that refuses every subject.
# ---------------------------------------------------------------------------
approve_trailers='

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"A real review."}'

# subject_of_length <n> — a Conventional Commits subject of exactly n characters.
subject_of_length() {
    local head='fix(gate): '
    printf '%s%s' "$head" "$(printf '%*s' "$(( $1 - ${#head} ))" '' | tr ' ' 'x')"
}

expect_pass "a subject of exactly 72 characters sits on the ceiling and passes" \
    "$(subject_of_length 72)${approve_trailers}"

expect_fail "a subject of 73 characters is over the ceiling and fails" \
    "max is 72" \
    "$(subject_of_length 73)${approve_trailers}"

# ---------------------------------------------------------------------------
# THE schema_version FIELD IS ONE OF THE THREE THE SCHEMA REQUIRES.
#
# schema_version, verdict and notes. The last two have cases above; this one had
# none, and measured, making the check vacuous left the suite green. All the ways
# it can be wrong are pinned, because they take different paths through the
# parser: a wrong number, a value that is not a number at all, an absent key, and
# a key whose value is the empty string.
#
# THE LAST TWO ARE NOT DECORATION. The parser hands four tab-separated fields
# back to `IFS=$'\t' read`, and a tab is IFS WHITESPACE — bash strips a leading
# run of it and collapses consecutive ones. An EMPTY first column therefore did
# not read back as empty, it vanished, and every field shifted one place left.
# Measured on the absent-key message below, before the parser was made to emit a
# sentinel: schema_version read the VERDICT, verdict read the notes state, notes
# read the flags state, and the validator refused the commit while naming three
# fields that were all fine. It failed CLOSED, which is why a green suite never
# saw it, and it pointed the author at the wrong field. CASE 14 pins that notes
# TEXT cannot shift the record; this pins that an ABSENT field cannot either.
# ---------------------------------------------------------------------------
expect_fail "a schema_version of 2 is refused" \
    "missing or wrong 'schema_version'" \
'feat(queue): chain the claim through

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":2,"verdict":"APPROVE","flags":[],"notes":"A real review."}'

expect_fail "a schema_version that is not a number at all is refused" \
    "missing or wrong 'schema_version'" \
'feat(queue): chain the claim through

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":"banana","verdict":"APPROVE","flags":[],"notes":"A real review."}'

# The expected text names the sentinel, not just the field. Without that, the
# case passes on the shifted record too — there the message read
# `expected 1, got 'APPROVE'`, which contains the same substring and is a lie.
expect_fail "an absent schema_version is refused, and named as absent" \
    "expected 1, got '__ABSENT__'" \
'feat(queue): chain the claim through

Reviewed-By: agent-reviewer
Review-Verdict: {"verdict":"APPROVE","flags":[],"notes":"A real review."}'

expect_fail "an empty-string schema_version is refused, and named as empty" \
    "expected 1, got '__EMPTY__'" \
'feat(queue): chain the claim through

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":"","verdict":"APPROVE","flags":[],"notes":"A real review."}'

# The alignment itself, asserted rather than inferred. On the shifted record the
# same message ALSO produced "unknown verdict value 'ok'" and "unreadable
# 'notes' state ('array')" — two complaints about fields that were correct.
# One missing key must produce one complaint.
assertions=$((assertions + 1))
printf '%s\n' 'feat(queue): chain the claim through

Reviewed-By: agent-reviewer
Review-Verdict: {"verdict":"APPROVE","flags":[],"notes":"A real review."}' >"$work/msg"
if "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
    fail "an absent schema_version was accepted"
elif grep -qF "unknown verdict value" "$work/out" || grep -qF "unreadable 'notes' state" "$work/out"; then
    fail "an absent schema_version shifted the record and mis-named other fields"
    sed 's/^/    /' "$work/out" >&2
else
    pass "an absent schema_version does not shift the other fields"
fi

# The same absent key on the OTHER parser. Two parsers, one contract — and the
# empty first column came out of the shared @tsv shape, so the fallback carried
# the identical shift and needed the identical sentinel.
if [ -n "${nojq:-}" ]; then
    assertions=$((assertions + 1))
    printf '%s\n' 'feat(queue): chain the claim through

Reviewed-By: agent-reviewer
Review-Verdict: {"verdict":"APPROVE","flags":[],"notes":"A real review."}' >"$work/msg"
    if PATH="$nojq" "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
        fail "without jq, an absent schema_version was accepted"
    elif grep -qF "expected 1, got '__ABSENT__'" "$work/out"; then
        pass "without jq, an absent schema_version is named as absent"
    else
        fail "without jq, the absent schema_version was refused for the wrong reason"
        sed 's/^/    /' "$work/out" >&2
    fi
fi

# The control. Without it, a validator that refused every schema_version would
# keep the cases above green.
expect_pass "a schema_version of 1 is accepted" \
'feat(queue): chain the claim through

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"A real review."}'

# ---------------------------------------------------------------------------
# THE SECOND FREE-TEXT COLUMN. An empty verdict string.
#
# `schema_version` got a sentinel for the empty case and `verdict` did not, so
# the shift the sentinel exists to stop came straight back one column over.
# Measured on the message below, on BOTH parsers: the validator refused it —
# it failed closed — while reporting "unreadable 'notes' state ('array')" and
# "unknown verdict value 'ok'". Two complaints, about two fields that were
# correct, and no mention of the field the author left empty.
#
# The expected text names the empty verdict. A case that only asked for a
# non-zero exit passed on the shifted record too, which is how this survived a
# green suite.
# ---------------------------------------------------------------------------
empty_verdict_msg='feat(queue): chain the claim through

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "", "notes": "n", "flags": []}'

expect_fail "an empty verdict string is refused, and named as empty" \
    "'verdict' is an empty string" \
    "$empty_verdict_msg"

# The alignment itself. One empty field must produce one complaint.
assertions=$((assertions + 1))
printf '%s\n' "$empty_verdict_msg" >"$work/msg"
if "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
    fail "an empty verdict string was accepted"
elif grep -qF "unknown verdict value" "$work/out" || grep -qF "unreadable 'notes' state" "$work/out"; then
    fail "an empty verdict string shifted the record and mis-named other fields"
    sed 's/^/    /' "$work/out" >&2
else
    pass "an empty verdict string does not shift the other fields"
fi

# The same message on the OTHER parser. The shift came out of the shared
# record shape, so both parsers carried it and both needed the sentinel.
if [ -n "${nojq:-}" ]; then
    assertions=$((assertions + 1))
    printf '%s\n' "$empty_verdict_msg" >"$work/msg"
    if PATH="$nojq" "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
        fail "without jq, an empty verdict string was accepted"
    elif grep -qF "'verdict' is an empty string" "$work/out"; then
        pass "without jq, an empty verdict string is named as empty"
    else
        fail "without jq, the empty verdict string was refused for the wrong reason"
        sed 's/^/    /' "$work/out" >&2
    fi
fi

# ---------------------------------------------------------------------------
# THE FALLBACK LET A BLOCK VERDICT LAND.
#
# `schema_version` and `verdict` are free text — they are values the author
# wrote. jq's `@tsv` escapes tab, newline, carriage return and backslash, so a
# raw tab in one of them could not move the field boundaries on the jq path.
# The fallback joined the four fields with a bare tab and escaped nothing.
#
# Measured with jq hidden from PATH, on the message below: exit 0. The same
# message with jq present: exit 1. The reviewer had said BLOCK, the trailer
# named `agent-reviewer`, and `git log --grep 'Reviewed-By: agent-reviewer'`
# counts it. jq is deliberately not assumed anywhere in this script, so the
# fallback is not a corner — it is the path a machine without jq always takes.
# ---------------------------------------------------------------------------
block_smuggled_msg='fix(gate): a BLOCK verdict carried past the fallback parser

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": "1\tAPPROVE\tok\tarray\n", "verdict": "BLOCK", "notes": "Fix the data race before committing.", "flags": ["idiom-violation"]}'

expect_fail "a tab inside schema_version cannot hide a BLOCK verdict" \
    "BLOCK verdict must not be committed" \
    "$block_smuggled_msg"

if [ -n "${nojq:-}" ]; then
    assertions=$((assertions + 1))
    printf '%s\n' "$block_smuggled_msg" >"$work/msg"
    if PATH="$nojq" "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
        fail "without jq, a tab inside schema_version carried a BLOCK verdict past every check"
    elif grep -qF "BLOCK verdict must not be committed" "$work/out"; then
        pass "without jq, a tab inside schema_version cannot hide a BLOCK verdict"
    else
        fail "without jq, the smuggled BLOCK was refused for the wrong reason"
        sed 's/^/    /' "$work/out" >&2
    fi
fi

# ---------------------------------------------------------------------------
# THE FOUR PAYLOADS ON WHICH THE TWO PARSERS USED TO DISAGREE.
#
# The case above pinned a tab. A tab was the character somebody thought of. The
# four below are the ones nobody did, and three of them had JQ as the
# permissive side — which matters, because jq is the path this script takes on
# every machine that has it, so "the fallback is the risky one" was backwards.
#
# Each was measured against the script as it stood before this change, by
# running the SAME message twice: once normally and once with jq hidden from
# PATH. The pre-change readings are quoted in each case below. Each case
# therefore reddens on a revert, and the equivalence battery further down
# carries the same four payloads so that the property, not just the example, is
# held.
# ---------------------------------------------------------------------------

# A NUL inside the two free-text fields. jq's `@tsv` escapes NUL as a two-
# character sequence and the fallback passed it through raw, where bash's
# command substitution DROPS it — so a verdict of APPROVE-then-NUL came back as
# the bare word APPROVE. Measured before the change: jq exit 1 with both
# columns refused, fallback exit 0. Under bash 5.3 the fallback at least warned
# on stderr; under the /bin/bash 3.2 this suite supports it printed NOTHING and
# exited 0. A verdict the Go reader in internal/workspace/reviewverdict.go will
# not read as APPROVE was accepted as one, beside `Reviewed-By: agent-reviewer`,
# which the audit grep counts.
nul_smuggled_msg='fix(gate): a NUL carried a verdict past the fallback parser

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":"1\u0000","verdict":"APPROVE\u0000","notes":"n","flags":[]}'

expect_fail "a NUL in the verdict is refused" \
    "'verdict' holds a control character" \
    "$nul_smuggled_msg"

expect_fail "a NUL in the schema_version is refused" \
    "'schema_version' holds a control character" \
    "$nul_smuggled_msg"

if [ -n "${nojq:-}" ]; then
    assertions=$((assertions + 1))
    printf '%s\n' "$nul_smuggled_msg" >"$work/msg"
    if PATH="$nojq" "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
        fail "without jq, a NUL carried an APPROVE past every check"
    elif grep -qF "'verdict' holds a control character" "$work/out"; then
        pass "without jq, a NUL in the verdict is refused"
    else
        fail "without jq, the NUL verdict was refused for the wrong reason"
        sed 's/^/    /' "$work/out" >&2
    fi
fi

# The class, not the member. A NUL is the one control byte bash drops, so a fix
# that added NUL to the escape list would pass the case above and leave every
# other control byte reaching the record. Measured on jq 1.7.1: `@tsv` escapes
# backslash, tab, newline, carriage return and NUL, and passes U+0001, U+0007,
# U+0008, U+001B, U+001F and U+007F through RAW. This case drives U+0001, one
# of the raw ones. It is what tells an escape-list fix from a class fix.
expect_fail "a control byte that @tsv does not escape is refused too" \
    "'verdict' holds a control character" \
'fix(gate): a control byte jq passes through unescaped

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version":1,"verdict":"APPROVE\u0001","notes":"n","flags":[]}'

# `1e0`. Measured before the change: jq exit 0 — an ACCEPTED approval — and
# fallback exit 1. jq renders it `1`, Python renders it `1.0`, and jq 1.7.1
# cannot tell `1e0` from `1` at all, so no jq program can decide this and the
# rule is stated over the text instead. The suite pinned `1e3`, where the two
# agree, and never tried `1e0`, where they did not: a test that certifies the
# payload on which the claim is true.
expect_fail "a schema_version of 1e0 is refused" \
    "number literal that JSON does not have" \
'fix(gate): an exponent literal jq reads as the integer 1

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1e0, "verdict": "APPROVE", "notes": "n", "flags": []}'

# The same rule, stated as a class rather than as `1e0`. Go's encoding/json
# refuses `1.0`, `1e0` and `1e3` alike for an `int` field, so all three are
# refused here for one reason and not three.
expect_fail "a schema_version of 1.0 is refused for the same reason" \
    "number literal that JSON does not have" \
'fix(gate): a decimal point in a schema version

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1.0, "verdict": "APPROVE", "notes": "n", "flags": []}'

# `01` AND `+1`, AND THESE TWO ARE WHY THE GUARD IS A WHITELIST.
#
# They were not in the block this round was asked to fix. They were found by
# driving payloads at both parsers afterwards, because a fix aimed at `1e0`
# alone would have been a blacklist of `.`, `e` and `E` — and a blacklist would
# have passed the `1e0` case above and shipped with these two still open.
#
# Neither is JSON. RFC 8259 has no leading zero and no leading plus, and Go's
# encoding/json refuses both. jq reads both and renders each as exactly `1`, so
# BOTH WERE ACCEPTED APPROVALS on the jq path — measured exit 0 with jq present
# and exit 1 with it hidden. They are the same hole as `1e0` wearing different
# spelling, which is the argument for asking what a JSON integer may contain
# instead of listing what it may not.
expect_fail "a schema_version with a leading zero is refused" \
    "number with a leading zero" \
'fix(gate): a leading zero jq reads as the integer 1

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 01, "verdict": "APPROVE", "notes": "n", "flags": []}'

expect_fail "a schema_version with a leading plus is refused" \
    "number literal that JSON does not have" \
'fix(gate): a leading plus jq reads as the integer 1

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": +1, "verdict": "APPROVE", "notes": "n", "flags": []}'

# The control that keeps the leading-zero rule from being a rule about the
# digit zero. A zero inside a number, and a number that IS zero, are both
# ordinary. Without this, `(^|[^0-9])0[0-9]` could be tightened into something
# that refuses `100` and every case above would stay green.
expect_fail "a schema_version of 100 is refused as a version, not as a literal" \
    "expected 1, got '100'" \
'fix(gate): a plain integer that is not 1

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 100, "verdict": "APPROVE", "notes": "n", "flags": []}'

# The control for the guard above, and it is not decoration: the guard reads
# the raw text after removing string literals, so a validator that read the
# strings too would refuse every `notes` containing a full stop — which is
# every real one. This message must PASS.
expect_pass "a full stop inside notes is not read as a number" \
'fix(gate): ordinary prose in the notes field

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "APPROVE", "notes": "Checked the queue path. Version 1.0 of the spec agrees. E.g. this sentence.", "flags": []}'

# A byte-order mark before the JSON. Measured before the change: jq exit 0 —
# jq strips a leading mark — and fallback exit 1. Go refuses it. Written with
# an explicit byte escape rather than as a literal, so the character is visible
# to the next reader and a future edit cannot delete it without noticing.
#
# THESE TWO ASSERT THE WORDING, NOT THE REFUSAL, and the difference is worth
# knowing before trusting them. The number-literal whitelist refuses a leading
# mark on its own, because the mark is not inside a string and is not a
# character a JSON structure may hold. Measured with the dedicated guard
# disabled: the commit is still refused, on both parsers, byte for byte, and
# exactly these two cases go red — on the message, never on the verdict. They
# are kept because a refusal that misnames the cause sends the author to the
# wrong part of their trailer.
bom=$'\xef\xbb\xbf'
bom_msg="fix(gate): a byte-order mark before the verdict JSON

Reviewed-By: agent-reviewer
Review-Verdict: ${bom}{\"schema_version\": 1, \"verdict\": \"APPROVE\", \"notes\": \"n\", \"flags\": []}"

expect_fail "a leading byte-order mark is refused" \
    "begins with a byte-order mark" \
    "$bom_msg"

if [ -n "${nojq:-}" ]; then
    assertions=$((assertions + 1))
    printf '%s\n' "$bom_msg" >"$work/msg"
    if PATH="$nojq" "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
        fail "without jq, a leading byte-order mark was accepted"
    elif grep -qF "begins with a byte-order mark" "$work/out"; then
        pass "without jq, a leading byte-order mark is refused in the same words"
    else
        fail "without jq, the byte-order mark was refused for the wrong reason"
        sed 's/^/    /' "$work/out" >&2
    fi
fi

# The control for the byte-order-mark guard. Only a LEADING mark is refused. A
# U+FEFF inside the notes string is ordinary text that Go accepts, and refusing
# it would be inventing a rule this repo does not have.
expect_pass "a byte-order mark inside notes is ordinary text" \
"fix(gate): an invisible character in the reviewer's prose

Reviewed-By: agent-reviewer
Review-Verdict: {\"schema_version\": 1, \"verdict\": \"APPROVE\", \"notes\": \"a${bom}b\", \"flags\": []}"

# TWO JSON documents on one trailer line. Measured before the change: jq exit
# 0. Plain `jq` reads a STREAM — it applied the filter to the first document
# and said nothing about the rest — so an APPROVE followed by a BLOCK exited 0
# and the BLOCK was read by nothing. The tail is not a corner: it is the half
# of the trailer a human reader would see first.
two_doc_msg='fix(gate): a second verdict document nothing reads

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "APPROVE", "notes": "n", "flags": []} {"schema_version": 1, "verdict": "BLOCK", "notes": "Fix the data race first.", "flags": []}'

expect_fail "two JSON documents on the trailer line are refused" \
    "more than one JSON document" \
    "$two_doc_msg"

if [ -n "${nojq:-}" ]; then
    assertions=$((assertions + 1))
    printf '%s\n' "$two_doc_msg" >"$work/msg"
    if PATH="$nojq" "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
        fail "without jq, two JSON documents on the trailer line were accepted"
    elif grep -qF "more than one JSON document" "$work/out"; then
        pass "without jq, two JSON documents are refused in the same words"
    else
        fail "without jq, the two-document trailer was refused for the wrong reason"
        sed 's/^/    /' "$work/out" >&2
    fi
fi

# ---------------------------------------------------------------------------
# TWO PARSERS, ONE RECORD — ASSERTED RATHER THAN ASSUMED.
#
# The cases above pin single messages. This pins the property behind them: for
# the same JSON, the two parsers must return the SAME BYTES. A case-by-case
# suite only ever covers the payloads somebody thought of, and the defect above
# was a whole CLASS — every character `@tsv` escapes and the fallback did not.
#
# 36 payloads and one control. All 37 agree.
#
# THE NUMBER BELOW IS STATED AGAINST A MUTANT ANYBODY CAN REBUILD, and the one
# it replaces was not. The earlier note here counted disagreements against "the
# script before the fix" — a working tree that was never committed, so nobody
# but its author could reproduce the figure, and an unverifiable number is
# worth less than none. Rebuild this one instead: from the committed script,
# take out the four guards the parser note names — the control-byte refusal
# (`ctl`, and the `__CONTROL__` arm of `free`, in both programs), the document
# count (`jq -s` plus the decode loop), the byte-order-mark guard, and the
# number-literal whitelist
# with its leading-zero rule. Measured with those four removed: 8 of the 36
# payloads disagree and 22 assertions in this file go red.
#
# TAKE EACH GUARD OUT WHOLE, AND TAKE OUT ONLY THE GUARD. Two of the four
# share a function with something that is not part of them, so the boundary
# has to be given in both directions or the figure will not match.
#
# The document count is `jq -s` and the `length` test wrapped around it AND
# the Python `raw_decode`-until-spent loop. All of it goes, including the
# empty-TRAILER answer, which rides on that apparatus without being a guard of
# its own. Leave either half standing and the count falls: two rebuilds that
# each kept a different half measured 20 red and 19 red.
#
# The control-byte refusal is `ctl` and the `__CONTROL__` arm of `free`. The
# `__EMPTY__` arm of that same function STAYS. It is the empty-FIELD rule the
# parser note calls load-bearing — a separate guard that happens to live in
# the same three lines. Delete `free` entire and four more assertions go red
# and the figure reads 26.
#
# THE PARAGRAPHS BELOW CANNOT CATCH EITHER MISTAKE, which is why both
# boundaries are spelled out here rather than left to the reader. The
# control-byte readings disagree on exactly the same eight payloads with the
# same six-verdict and two-wording split, so a wrong rebuild reads its way
# down this note finding every sub-claim confirmed. Earlier versions of this
# paragraph sent three people to three different mutants, and 22, 20 and 19
# all came back honestly measured. If the guards change, restate the removal
# here before restating the number.
#
# SIX OF THE EIGHT DISAGREE ON THE VERDICT ITSELF rather than on the wording,
# and FIVE OF THOSE SIX have jq accepting a commit the fallback refuses: `1e0`,
# `01`, `+1`, a leading byte-order mark, and two JSON documents on one line.
# The sixth is the NUL, where the fallback is the one that accepts. The
# remaining two differ only in the text of a refusal both parsers reach
# (`1e3` and `nan`). Read that split before assuming the fallback is the risky
# half of this pair: jq is the default path and it was the looser one.
#
# ONE PAYLOAD IS COMPARED ON EXIT STATUS ONLY, and the reason is stated rather
# than waved at. The text of a parse error belongs to the parser that emitted
# it — jq says it could not parse the JSON, Python says which character it
# stopped at — so a truncated document is held to the same VERDICT and not to
# the same words. That is the only remaining member of that category. `1e3`
# used to sit here too, on the reasoning that jq prints `1E+3` where Python
# prints `1000.0`. It has moved up: the non-integer-number rule is now decided
# on the text before either parser runs, so neither parser renders it at all
# and the two refusals are byte-identical. When a claim about a difference
# stops being true, the payload moves rather than the sentence being softened.
#
# Needs both parsers present. With jq missing, both halves would run the
# fallback and the comparison would prove nothing, so it says it skipped.
# ---------------------------------------------------------------------------
if [ -n "${nojq:-}" ] && command -v jq >/dev/null 2>&1; then
    equiv_identical=(
'{"schema_version": "1\tAPPROVE\tok\tarray\n", "verdict": "BLOCK", "notes": "Fix the data race before committing.", "flags": ["idiom-violation"]}'
'{"schema_version": 1, "verdict": "APPROVE\tok\tarray", "notes": "n", "flags": []}'
'{"schema_version": 1, "verdict": "APPROVE\nok\narray", "notes": "n", "flags": []}'
'{"schema_version": 1, "verdict": "APPROVE\rok\rarray", "notes": "n", "flags": []}'
'{"schema_version": 1, "verdict": "APPROVE\\tok", "notes": "n", "flags": []}'
'{"schema_version": 1, "verdict": "a\\\tb\\\nc", "notes": "n", "flags": []}'
'{"schema_version": "\t", "verdict": "APPROVE", "notes": "n", "flags": []}'
'{"schema_version": "1", "verdict": "", "notes": "n", "flags": []}'
'{"schema_version": "", "verdict": "APPROVE", "notes": "n", "flags": []}'
'{"verdict": "APPROVE", "notes": "n", "flags": []}'
'{"schema_version": 1, "notes": "n", "flags": []}'
'{"schema_version": 1, "verdict": null, "notes": "n", "flags": []}'
'{"schema_version": null, "verdict": "APPROVE", "notes": "n", "flags": []}'
'{"schema_version": true, "verdict": false, "notes": "n", "flags": []}'
'{"schema_version": {"a":1}, "verdict": [1,2], "notes": "n", "flags": []}'
'{"schema_version": 1, "verdict": 1, "notes": "n", "flags": []}'
'{"schema_version": 1, "verdict": "APPROVÉé", "notes": "n", "flags": []}'
'{"schema_version": 1, "verdict": "   ", "notes": "n", "flags": []}'
'{"schema_version": " 1 ", "verdict": "APPROVE", "notes": "n", "flags": []}'
'{"schema_version": 1, "verdict": "APPROVE", "notes": "a\tb\nc\\d", "flags": []}'
'{"schema_version": 1, "verdict": "APPROVE", "notes": "n", "flags": "oops"}'
'{"schema_version": 1, "verdict": "APPROVE", "notes": null, "flags": null}'
'[1,2]'
'{"schema_version": 1.0, "verdict": "APPROVE", "notes": "n", "flags": []}'
'{"schema_version": 100000000000000000000, "verdict": "APPROVE", "notes": "n", "flags": []}'
'{"schema_version": 1e3, "verdict": "APPROVE", "notes": "n", "flags": []}'
'{"schema_version":"1\u0000","verdict":"APPROVE\u0000","notes":"n","flags":[]}'
'{"schema_version":1,"verdict":"APPROVE\u0001","notes":"n","flags":[]}'
'{"schema_version": 1e0, "verdict": "APPROVE", "notes": "n", "flags": []}'
'{"schema_version": 01, "verdict": "APPROVE", "notes": "n", "flags": []}'
'{"schema_version": +1, "verdict": "APPROVE", "notes": "n", "flags": []}'
'{"schema_version": nan, "verdict": "APPROVE", "notes": "n", "flags": []}'
'{"schema_version": 100, "verdict": "APPROVE", "notes": "n", "flags": []}'
"${bom}{\"schema_version\": 1, \"verdict\": \"APPROVE\", \"notes\": \"n\", \"flags\": []}"
'{"schema_version": 1, "verdict": "APPROVE", "notes": "n", "flags": []} {"schema_version": 1, "verdict": "BLOCK", "notes": "n", "flags": []}'
    )
    equiv_exit_only=(
'{"schema_version": 1, "verdict": "APPROVE", "notes": "n", "flags": [] '
    )

    # equiv_run <verdict-json> — runs the REAL validator over the same message
    # twice, once with jq on PATH and once without, and leaves the two outputs
    # and the two exit statuses behind for the caller.
    equiv_run() {
        printf '%s\n' "fix(gate): an adversarial verdict payload

Reviewed-By: agent-reviewer
Review-Verdict: $1" >"$work/msg"
        "$VALIDATOR" "$work/msg" >"$work/equiv-jq" 2>&1
        equiv_jq_exit=$?
        PATH="$nojq" "$VALIDATOR" "$work/msg" >"$work/equiv-py" 2>&1
        equiv_py_exit=$?
    }

    for equiv_payload in "${equiv_identical[@]}"; do
        assertions=$((assertions + 1))
        equiv_run "$equiv_payload"
        if [ "$equiv_jq_exit" != "$equiv_py_exit" ]; then
            fail "the parsers disagreed on exit status (${equiv_jq_exit} vs ${equiv_py_exit}): ${equiv_payload}"
            sed 's/^/    jq: /' "$work/equiv-jq" >&2
            sed 's/^/    py: /' "$work/equiv-py" >&2
        elif ! cmp -s "$work/equiv-jq" "$work/equiv-py"; then
            fail "the parsers returned different text: ${equiv_payload}"
            diff "$work/equiv-jq" "$work/equiv-py" | sed 's/^/    /' >&2
        else
            pass "both parsers agree byte for byte: ${equiv_payload}"
        fi
    done

    for equiv_payload in "${equiv_exit_only[@]}"; do
        assertions=$((assertions + 1))
        equiv_run "$equiv_payload"
        if [ "$equiv_jq_exit" != "$equiv_py_exit" ]; then
            fail "the parsers disagreed on the verdict (${equiv_jq_exit} vs ${equiv_py_exit}): ${equiv_payload}"
            sed 's/^/    jq: /' "$work/equiv-jq" >&2
            sed 's/^/    py: /' "$work/equiv-py" >&2
        else
            pass "both parsers reach the same verdict: ${equiv_payload}"
        fi
    done

    # The control. Every payload above is refused by design, so a validator
    # that refused everything would keep all of them green while proving
    # nothing about agreement. This one must be ACCEPTED by both parsers.
    assertions=$((assertions + 1))
    equiv_run '{"schema_version": 1, "verdict": "APPROVE", "notes": "A real review.", "flags": []}'
    if [ "$equiv_jq_exit" = "0" ] && [ "$equiv_py_exit" = "0" ]; then
        pass "both parsers accept a well-formed APPROVE"
    else
        fail "the parsers refused a well-formed APPROVE (${equiv_jq_exit} and ${equiv_py_exit})"
        sed 's/^/    jq: /' "$work/equiv-jq" >&2
        sed 's/^/    py: /' "$work/equiv-py" >&2
    fi
else
    echo "validate-commit-msg-test: jq or python3 absent; skipping the parser-equivalence battery" >&2
fi

# ---------------------------------------------------------------------------
# A REFUSAL QUOTES WHAT THE AUTHOR WROTE.
#
# The NOT_REVIEWED refusal used to print the haystack it searched: every
# `Reviewed-By:` line joined into one lower-cased string, the trailer keys
# removed and the spaces squeezed. On a real commit in this history that came
# out as `Reviewed-By: agent-reviewer (myself) -> passed agent-reviewer
# (kierkegaard) -> passed kierkegaard -> refused agent-reviewer` — a sentence
# that is in no message anywhere. An author cannot search their file for it.
#
# Both halves are asserted: the author's line is quoted, and the flattened
# form is gone. Only the first would pass on output that printed both.
# ---------------------------------------------------------------------------
quoted_refusal_msg='fix(gate): a second trailer names a reviewer the verdict denies

Reviewed-By: none — no reviewer was reached for this commit
Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}'

expect_fail "the refusal quotes the author's own Reviewed-By line" \
    "  Reviewed-By: none — no reviewer was reached for this commit" \
    "$quoted_refusal_msg"

assertions=$((assertions + 1))
printf '%s\n' "$quoted_refusal_msg" >"$work/msg"
if "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; then
    fail "a second Reviewed-By line naming a reviewer was accepted"
elif grep -qF "none — no reviewer was reached for this commit agent-reviewer" "$work/out"; then
    fail "the refusal printed the flattened haystack instead of the author's lines"
    sed 's/^/    /' "$work/out" >&2
else
    pass "the refusal does not print the flattened haystack"
fi

# ---------------------------------------------------------------------------
# THE COMMENT LINES GIT ACTUALLY KEEPS.
#
# Every check in the validator reads a stripped copy of the message, and the
# strip used to be `grep -v '^#'`, unconditionally. `git commit -F` — the
# spelling AGENTS.md mandates — gets cleanup mode `whitespace`, which does NOT
# remove comment lines. So a `#` line landed in the stored commit while being
# invisible to every check.
#
# Both cases below were measured PASSING against the unfixed script, end to end
# through scripts/commit-msg-gate.sh on a real commit, not just against a
# message file — the whole point is that the stored message differs from the
# validator's view. commit-msg-gate-test.sh carries the real-commit half; these
# two are the message-file half, and they go red on the same one-line revert.
# ---------------------------------------------------------------------------

# A fabricated reviewer on a `#` line. `git log --grep 'Reviewed-By:
# agent-reviewer'` counts the stored commit, so the validator must refuse it —
# agreement with that grep, in both directions, is the whole property.
hash_fabrication='fix(gate): a fabricated approval hidden behind a comment line

Reviewed-By: none — no reviewer was reached for this commit
# Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}'

expect_fail "a #-prefixed fabricated trailer is refused" \
    "must not name a reviewer this repo has" \
    "$hash_fabrication"

# The subject rules, applied to the line git stores as the subject. The strip
# did not only hide text, it RELOCATED the subject: with the `#` line gone, the
# rules landed on the next line instead. Here the real first line is 137
# characters and the second is a clean 65-character subject, so an exit 0 means
# the ceiling this suite pins was measured against the wrong line.
hash_first_line="# fix: $(printf '%*s' 130 '' | tr ' ' 'x')"
hash_relocation="${hash_first_line}

fix(gate): the short valid subject the strip used to read instead

Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {\"schema_version\": 1, \"verdict\": \"NOT_REVIEWED\", \"flags\": [], \"notes\": \"No reviewer was reached.\"}"

expect_fail "a #-prefixed first line is measured against the 72-char ceiling" \
    "subject line is 137 chars; max is 72" \
    "$hash_relocation"

expect_fail "a #-prefixed first line is the subject the format rules judge" \
    "Got: # fix: xxx" \
    "$hash_relocation"

# The control, and it is not decoration: without it, a validator that refused
# every `#` line would keep both cases above green while making a comment in a
# commit body an error nobody asked for. A `#` line is CONTENT now, and content
# that says nothing about a reviewer says nothing about a reviewer.
expect_pass "an ordinary # line in the body is content, not an error" \
'fix(gate): keep a comment in the body harmless

# A note to the next reader. Nothing about a reviewer here.

Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}'

# ---------------------------------------------------------------------------
# THE CLEANUP MODE IS RESOLVED, NOT ASSUMED.
#
# The cases above pin the default. These pin that it is a decision the script
# makes out loud and can be told to make differently — because the mode is the
# only thing that decides whether a `#` line is content or noise, and getting
# it wrong in the other direction (stripping what git keeps) is the fail-open
# these cases exist to stop coming back.
#
# Hermetic on purpose: GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM are pointed at
# /dev/null so a value in the developer's own git config cannot decide the
# result. A test whose answer depends on the machine it runs on is not an
# assertion.
# ---------------------------------------------------------------------------
cleanup_repo="$work/cleanup-repo"
mkdir -p "$cleanup_repo"
git -C "$cleanup_repo" init -q
git -C "$cleanup_repo" config user.name 'cleanup mode test'
git -C "$cleanup_repo" config user.email 'cleanup@example.invalid'

# run_validator_at <dir> <message> [VAR=VAL ...] — runs the REAL validator on
# <message> from <dir> with the named environment. The exit status is written
# to a FILE and read back from it, never taken off a pipeline, so nothing
# downstream can swallow a failure into a success.
run_validator_at() {
    local dir="$1" message="$2"
    shift 2
    printf '%s\n' "$message" >"$work/msg"
    ( cd "$dir" && env GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null "$@" \
        "$VALIDATOR" "$work/msg" >"$work/out" 2>&1; echo $? >"$work/exit" )
    cat "$work/exit"
}

# expect_at <name> <dir> <expected-exit> <expected-substring|-> <message> [VAR=VAL ...]
expect_at() {
    local name="$1" dir="$2" want_exit="$3" needle="$4" message="$5"
    shift 5
    assertions=$((assertions + 1))
    local got
    got="$(run_validator_at "$dir" "$message" "$@")"
    if [ "$got" != "$want_exit" ]; then
        fail "$name — wanted exit ${want_exit}, got ${got}"
        sed 's/^/    /' "$work/out" >&2
        return
    fi
    if [ "$needle" != "-" ] && ! grep -qF -- "$needle" "$work/out"; then
        fail "$name — exit was right but the output did not say '${needle}'"
        sed 's/^/    /' "$work/out" >&2
        return
    fi
    pass "$name"
}

# The default, stated in the script's own words. `git commit -F` gets
# `whitespace` from git, so that is what the validator models.
expect_at "the resolved cleanup mode is whitespace by default, and says so" \
    "$cleanup_repo" 0 "cleanup mode 'whitespace'" \
    'fix(gate): a clean message with no comment lines at all

Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}' \
    COMMIT_MSG_EXPLAIN=1

# The editor path. Under `strip` git DOES remove the comment, so the stored
# commit never carries the fabricated line and the audit grep never counts it —
# refusing there would be a false refusal, which this repo weighs the same as a
# false pass. The mode is the difference, and it has to be honoured.
expect_at "COMMIT_MSG_CLEANUP=strip removes the comment git would remove" \
    "$cleanup_repo" 0 "cleanup mode 'strip' from COMMIT_MSG_CLEANUP" \
    "$hash_fabrication" \
    COMMIT_MSG_CLEANUP=strip COMMIT_MSG_EXPLAIN=1

# The same message, same repository, mode read from git config instead of the
# environment. Without this the config lookup could be dead code.
git -C "$cleanup_repo" config commit.cleanup strip
expect_at "commit.cleanup=strip in git config is read and honoured" \
    "$cleanup_repo" 0 "from git config commit.cleanup" \
    "$hash_fabrication" \
    COMMIT_MSG_EXPLAIN=1

# The environment wins over the config. Same repository, still configured
# `strip`, and the caller asks for `whitespace`.
expect_at "COMMIT_MSG_CLEANUP overrides commit.cleanup" \
    "$cleanup_repo" 1 "must not name a reviewer this repo has" \
    "$hash_fabrication" \
    COMMIT_MSG_CLEANUP=whitespace

# `verbatim` keeps everything, so it behaves like `whitespace` here. Pinned so
# that a future edit which strips under any mode but `whitespace` goes red.
expect_at "commit.cleanup=verbatim keeps comment lines" \
    "$cleanup_repo" 1 "must not name a reviewer this repo has" \
    "$hash_fabrication" \
    COMMIT_MSG_CLEANUP=verbatim

# ---------------------------------------------------------------------------
# THE COMMENT MARKER IS GIT'S TO CHOOSE.
#
# A hard-coded `#` is wrong in BOTH directions against a repo that sets
# core.commentChar or core.commentString: it drops lines git stores, and it
# keeps lines git drops. Both directions are asserted, because a fix that only
# handles one of them leaves the other as the next escape.
# ---------------------------------------------------------------------------
git -C "$cleanup_repo" config core.commentChar ';'

expect_at "core.commentChar is the marker that gets stripped" \
    "$cleanup_repo" 0 "comment marker ';'" \
    'fix(gate): a fabricated approval behind the configured marker

Reviewed-By: none — no reviewer was reached for this commit
; Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}' \
    COMMIT_MSG_EXPLAIN=1

# The other direction, and the one that matters more: with `;` configured, a
# `#` line is ORDINARY TEXT that git stores. Hard-coding `#` would hide it.
expect_at "with core.commentChar set, a # line is content git keeps" \
    "$cleanup_repo" 1 "must not name a reviewer this repo has" \
    "$hash_fabrication"

# core.commentString (git 2.45+) is the multi-character spelling and wins over
# core.commentChar. A marker of more than one character defeats any check built
# on a single-character assumption.
git -C "$cleanup_repo" config core.commentString '//'
expect_at "core.commentString wins over core.commentChar" \
    "$cleanup_repo" 0 "comment marker '//'" \
    'fix(gate): a fabricated approval behind a two-character marker

Reviewed-By: none — no reviewer was reached for this commit
// Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}' \
    COMMIT_MSG_EXPLAIN=1

expect_at "with core.commentString set, the old marker is content git keeps" \
    "$cleanup_repo" 1 "must not name a reviewer this repo has" \
    'fix(gate): the previously configured marker is now ordinary text

Reviewed-By: none — no reviewer was reached for this commit
; Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}'

# `auto` asks git to pick a marker that begins no line of the message, so under
# `auto` no line of the author's text is a comment — that is what the setting
# guarantees. Stripping anything there would delete text git stores.
git -C "$cleanup_repo" config --unset core.commentString
git -C "$cleanup_repo" config core.commentChar auto
expect_at "core.commentChar=auto strips nothing, even under strip" \
    "$cleanup_repo" 1 "must not name a reviewer this repo has" \
    "$hash_fabrication"

# ---------------------------------------------------------------------------
# ASSERTION_FLOOR — THE SUITE CANNOT SKIP ITSELF TO NOTHING AND STILL SAY PASS.
#
# Measured on this file before the floor existed: with jq genuinely absent from
# PATH it printed `119 assertions, 0 failures`, then `PASS`, and exited 0.
# Twenty-nine assertions had vanished — the whole parser-equivalence battery
# and every fallback case that needs both programs — and the only trace was two
# notes on stderr. Nothing went red. A green suite that checked 119 things and
# a green suite that checked 148 read the same, which is the defect the sibling
# gate suite already named for itself: none of its assertions could tell
# "checked everything and found nothing wrong" from "checked nothing"
# (see LIVE_SCOPE_FLOOR and case 10 in scripts/commit-msg-gate-test.sh).
#
# Measured now, with the floor in place: a full run is 178 assertions and a run
# with jq hidden from PATH is 141, so the floor has to sit between them. It is
# set at 165 — close enough under the full count to be a real ratchet on
# deleting cases, and far enough over 141 that a silent skip cannot clear it.
#
# BOTH NUMBERS MOVE WHENEVER A CASE IS ADDED, and they moved together when the
# last three arrived (175 and 138 before them). Re-measure the pair rather than
# adjusting one — the floor's whole job is to sit between them, and a stale
# reading of either half hides whether it still does.
#
# HOW TO RE-MEASURE THE SECOND HALF, because getting it wrong is easy and
# silent. Build a PATH holding symlinks to every executable EXCEPT jq and run
# the suite under `bash`, not zsh: zsh's `command -v` returns alias text rather
# than a path, which produced a directory of broken symlinks and a reading that
# meant nothing. The jq-hidden run is SUPPOSED to end in one failure — this
# floor firing — so `141 assertions, 1 failures` is the right result there and
# a green run would mean the PATH still had jq in it.
#
# THE SELF-SKIP ITSELF IS RIGHT AND STAYS. A battery that compares two parsers
# cannot run with one parser installed, and pretending otherwise would make it
# compare the fallback against itself and pass for the wrong reason. What was
# missing is the consequence: a run that could not ask the question must not
# report that the answer was yes.
#
# SO A MACHINE WITHOUT jq CANNOT PRODUCE A GREEN RUN OF THIS SUITE, on purpose.
# The suite's subject is an agreement between two programs. With one of them
# absent the subject is not there to be tested, and the honest report is a
# failure that names what is missing — not a PASS with a smaller number.
#
# It is a RATCHET: it may go up as the suite grows and it must never come down
# to accommodate a skip. Adding a case needs no edit here; deleting nine does.
ASSERTION_FLOOR=165
if [ "$assertions" -lt "$ASSERTION_FLOOR" ]; then
    printf 'validate-commit-msg-test: FAIL: only %d assertions ran; the floor is %d\n' \
        "$assertions" "$ASSERTION_FLOOR" >&2
    printf 'validate-commit-msg-test: a run that skipped its way under the floor has not\n' >&2
    printf 'validate-commit-msg-test: checked what this suite exists to check. Install jq and\n' >&2
    printf 'validate-commit-msg-test: python3 and run it again.\n' >&2
    failures=$((failures + 1))
fi

printf 'validate-commit-msg-test: %d assertions, %d failures\n' "$assertions" "$failures"
[ "$failures" -eq 0 ] || exit 1
echo "validate-commit-msg-test: PASS"
