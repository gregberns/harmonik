#!/usr/bin/env bash
# core-loop-assert-test.sh — self-test for the core-loop-proof assertion library (T2, hk-1yxhh).
#
# Folds checked-in golden event streams (scenarios/core-loop-proof/testdata/) through
# scripts/core-loop-assert.jq and asserts the per-gap verdict for each. This is the
# reproducible, ZERO-TOKEN definition of "T2 green": the assertion contract holds against
# known inputs without a live daemon. A full live matrix green is T9.
#
# Exit 0 iff every case matches its expected verdict.

set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LIB="$ROOT/scripts/core-loop-assert.jq"
TD="$ROOT/scenarios/core-loop-proof/testdata"
command -v jq >/dev/null 2>&1 || { echo "jq required" >&2; exit 2; }

pass=0; fail=0
# check_ref <name> <remote-ndjson> <local-ref-ndjson> <spec-json> <gap> <expected-verdict>
# gap2 parity: the local reference stream is passed as $ref_events (a JSON array).
check_ref() {
    local name="$1" stream="$2" refstream="$3" spec="$4" gap="$5" want="$6" got ref
    ref="$(jq -s '.' "$refstream")"
    got="$(jq -n --slurpfile events "$stream" --argjson spec "$spec" --argjson ref_events "$ref" -f "$LIB" \
             | jq -r --arg g "$gap" '.[] | select(.gap==$g) | .verdict')"
    if [ "$got" = "$want" ]; then pass=$((pass+1)); echo "ok   — $name ($gap=$got)";
    else fail=$((fail+1)); echo "FAIL — $name: $gap expected '$want' got '$got'" >&2; fi
}

# check <name> <ndjson> <spec-json> <gap> <expected-verdict>
check() {
    local name="$1" stream="$2" spec="$3" gap="$4" want="$5" got
    # -f runs the library as THE program; the per-gap verdict is extracted by a second jq
    # (an inline filter alongside -f would be parsed as an input file, not a program).
    got="$(jq -n --slurpfile events "$stream" --argjson spec "$spec" --argjson ref_events null -f "$LIB" \
             | jq -r --arg g "$gap" '.[] | select(.gap==$g) | .verdict')"
    if [ "$got" = "$want" ]; then
        pass=$((pass+1)); echo "ok   — $name ($gap=$got)"
    else
        fail=$((fail+1)); echo "FAIL — $name: $gap expected '$want' got '$got'" >&2
    fi
}

# check_detail <name> <ndjson> <spec-json> <gap> <expected-verdict> <detail-substring>
# Same as check, plus the REASON. A verdict alone cannot tell a right answer from a right
# answer reached for the wrong reason, and t10 has already shipped one of those: it compared
# the observed branch against the literal `main` while the scratch daemon lands on
# scratch/main, so a landing on the trunk was reported as "landed on some other branch". The
# verdict was fail either way. Only the detail string says which check actually fired.
check_detail() {
    local name="$1" stream="$2" spec="$3" gap="$4" want="$5" want_detail="$6" got_verdict got_detail row
    row="$(jq -n --slurpfile events "$stream" --argjson spec "$spec" --argjson ref_events null -f "$LIB" \
             | jq -c --arg g "$gap" '.[] | select(.gap==$g)')"
    got_verdict="$(printf '%s' "$row" | jq -r '.verdict')"
    got_detail="$(printf '%s' "$row" | jq -r '.detail')"
    if [ "$got_verdict" = "$want" ] && case "$got_detail" in *"$want_detail"*) true;; *) false;; esac; then
        pass=$((pass+1)); echo "ok   — $name ($gap=$got_verdict)"
    else
        fail=$((fail+1)); echo "FAIL — $name: $gap expected '$want' with detail containing '$want_detail'; got '$got_verdict' / '$got_detail'" >&2
    fi
}

# no_leak_models forbids a FOREIGN family's node-model pin on this cell's harness (T4).
CODEX='{"schema_version":1,"cell":"codex:local","seed_bead":"hk-clp-codex","expect":{"harness_selected":{"agent_type":"codex","tier":1},"model_selected":{"harness":"codex","model":null,"no_leak_models":["claude-opus-4-8","deepseek-reasoner"]}},"gaps":["gap1","gap3","gap4"]}'
PI='{"schema_version":1,"cell":"pi:local","seed_bead":"hk-clp-pi","expect":{"harness_selected":{"agent_type":"pi","tier":1},"model_selected":{"harness":"pi","model":"deepseek-reasoner","no_leak_models":["claude-opus-4-8"]}},"gaps":["gap1"]}'
CLAUDE='{"schema_version":1,"cell":"claude:local","seed_bead":"hk-clp-claude","expect":{"harness_selected":{"agent_type":"claude-code","tier":1},"model_selected":{"harness":"claude-code","model":"claude-opus-4-8","no_leak_models":["deepseek-reasoner"]}},"gaps":["gap1","gap5"]}'

check "codex gap1 pass"          "$TD/codex-local-pass.ndjson"      "$CODEX" gap1 pass
check "codex gap3 pending"       "$TD/codex-local-pass.ndjson"      "$CODEX" gap3 pending
check "codex gap4 pending"       "$TD/codex-local-pass.ndjson"      "$CODEX" gap4 pending
check "pi gap1 pass"             "$TD/pi-local-pass.ndjson"         "$PI"     gap1 pass
check "claude gap1 pass"         "$TD/claude-local-pass.ndjson"     "$CLAUDE" gap1 pass
check "claude gap5 pending"      "$TD/claude-local-pass.ndjson"     "$CLAUDE" gap5 pending
check "pi node-pin LEAK gap1 fail (T4/hk-lfrub)" "$TD/pi-local-modelleak.ndjson" "$PI" gap1 fail
check "codex tier-leak gap1 fail" "$TD/codex-local-tierleak.ndjson" "$CODEX" gap1 fail
check "codex missing gap1 fail"  "$TD/codex-local-missing.ndjson"   "$CODEX" gap1 fail

# gap4 — dispatch field fidelity (T5). Spec carries expect.dispatch.
DISP='{"schema_version":1,"seed_bead":"hk-clp-codex","expect":{"dispatch":{"workflow_mode":"single","workflow_id_present":true}},"gaps":["gap4"]}'
check "codex gap4 dispatch pass"       "$TD/codex-local-dispatch-pass.ndjson"     "$DISP" gap4 pass
check "codex gap4 review-loop override fail" "$TD/codex-local-dispatch-override.ndjson" "$DISP" gap4 fail
check "codex gap4 no run_started fail" "$TD/codex-local-missing.ndjson"           "$DISP" gap4 fail
check "codex gap4 pending when no expect.dispatch" "$TD/codex-local-pass.ndjson"  "$CODEX" gap4 pending

# gap4 on a version-2 run_started record. workflow_mode is "dot" for every run, so the
# no-review path is told apart from the reviewed path by review_policy and
# workflow_selection_source. Refs: hk-oeqn9, hk-gap4-workflow-mode-drift-7xwat.
V2='{"schema_version":1,"seed_bead":"hk-clp-codex","expect":{"dispatch":{"workflow_mode":"dot","review_policy":"no_review","workflow_selection_source":"legacy_single_label","workflow_id_present":true}},"gaps":["gap4"]}'
V2POL='{"schema_version":1,"seed_bead":"hk-clp-codex","expect":{"dispatch":{"workflow_mode":"dot","review_policy":"reviewed"}},"gaps":["gap4"]}'
V2SRC='{"schema_version":1,"seed_bead":"hk-clp-codex","expect":{"dispatch":{"workflow_mode":"dot","workflow_selection_source":"project_default"}},"gaps":["gap4"]}'
check "codex gap4 v2 no-review pass"        "$TD/codex-local-dispatch-v2-pass.ndjson" "$V2"    gap4 pass
check "codex gap4 v2 wrong review_policy fail" "$TD/codex-local-dispatch-v2-pass.ndjson" "$V2POL" gap4 fail
check "codex gap4 v2 wrong selection source fail" "$TD/codex-local-dispatch-v2-pass.ndjson" "$V2SRC" gap4 fail

# gap4 workflow_id VALUE. The selection source fixes the graph, so the cell can name it:
# legacy_single_label always loads the embedded no-review-bead graph.
V2WID='{"schema_version":1,"seed_bead":"hk-clp-codex","expect":{"dispatch":{"workflow_mode":"dot","workflow_id":"no-review-bead","workflow_id_present":true}},"gaps":["gap4"]}'
V2WIDX='{"schema_version":1,"seed_bead":"hk-clp-codex","expect":{"dispatch":{"workflow_mode":"dot","workflow_id":"standard-bead"}},"gaps":["gap4"]}'
check "codex gap4 v2 workflow_id match pass" "$TD/codex-local-dispatch-v2-pass.ndjson" "$V2WID"  gap4 pass
check "codex gap4 v2 wrong workflow_id fail" "$TD/codex-local-dispatch-v2-pass.ndjson" "$V2WIDX" gap4 fail

# workflow_id_present on its own. Every cell now asserts it true, because a run_started
# record with an empty descriptor cannot be emitted (core.RunStartedPayload.Valid).
V2PRES='{"schema_version":1,"seed_bead":"hk-clp-codex","expect":{"dispatch":{"workflow_mode":"dot","workflow_id_present":true}},"gaps":["gap4"]}'
check "codex gap4 v2 workflow_id present pass" "$TD/codex-local-dispatch-v2-pass.ndjson"  "$V2PRES" gap4 pass
check "codex gap4 v2 empty workflow_id fail"   "$TD/codex-local-dispatch-v2-nowid.ndjson" "$V2PRES" gap4 fail

# gap4 dispatched-node containment. node_dispatch_requested carries run_id only, so the
# set is scoped by joining on the seed bead's run_started.run_id. The v2 fixture dispatches
# implement + close on THIS run and reviewer on a SIBLING run; the sibling must not count.
V2NODES='{"schema_version":1,"seed_bead":"hk-clp-codex","expect":{"dispatch":{"workflow_mode":"dot","nodes":{"required":["implement"],"forbidden":["review","reviewer"]}}},"gaps":["gap4"]}'
V2NODEMISS='{"schema_version":1,"seed_bead":"hk-clp-codex","expect":{"dispatch":{"workflow_mode":"dot","nodes":{"required":["start"]}}},"gaps":["gap4"]}'
V2NODEBAN='{"schema_version":1,"seed_bead":"hk-clp-codex","expect":{"dispatch":{"workflow_mode":"dot","nodes":{"forbidden":["close"]}}},"gaps":["gap4"]}'
check "codex gap4 v2 node set pass (sibling run ignored)" "$TD/codex-local-dispatch-v2-pass.ndjson" "$V2NODES"    gap4 pass
check "codex gap4 v2 missing required node fail"          "$TD/codex-local-dispatch-v2-pass.ndjson" "$V2NODEMISS" gap4 fail
check "codex gap4 v2 forbidden node dispatched fail"      "$TD/codex-local-dispatch-v2-pass.ndjson" "$V2NODEBAN"  gap4 fail
# A stream with NO node_dispatch_requested at all cannot satisfy a required-node claim.
NONODES='{"schema_version":1,"seed_bead":"hk-clp-codex","expect":{"dispatch":{"workflow_mode":"single","nodes":{"required":["implement"]}}},"gaps":["gap4"]}'
check "codex gap4 no node events at all fail" "$TD/codex-local-dispatch-pass.ndjson" "$NONODES" gap4 fail

# gap3 — provider comms through the sandbox (T6). Spec carries expect.provider.
PROV='{"schema_version":1,"seed_bead":"hk-clp-codex","expect":{"provider":{"enabled":true}},"gaps":["gap3"]}'
check "codex gap3 real commit pass"    "$TD/codex-provider-commit.ndjson"         "$PROV" gap3 pass
check "codex gap3 explicit-fail pass"  "$TD/codex-provider-explicit-fail.ndjson"  "$PROV" gap3 pass
check "codex gap3 silent no-commit fail" "$TD/codex-provider-silent-nocommit.ndjson" "$PROV" gap3 fail
check "codex gap3 pending when no expect.provider" "$TD/codex-local-pass.ndjson"   "$CODEX" gap3 pending

# gap2 — remote==local parity (T7). Remote cell spec + a local reference stream.
REM='{"schema_version":1,"substrate":"remote","seed_bead":"hk-clp-codex","expect":{},"gaps":["gap2"]}'
LOC='{"schema_version":1,"substrate":"local","seed_bead":"hk-clp-codex","expect":{},"gaps":["gap2"]}'
check_ref "gap2 remote==local pass"        "$TD/gap2-remote-match.ndjson"   "$TD/gap2-local-ref.ndjson" "$REM" gap2 pass
check_ref "gap2 remote diverges (terminal) fail" "$TD/gap2-remote-diverge.ndjson" "$TD/gap2-local-ref.ndjson" "$REM" gap2 fail
check      "gap2 SKIP-LOUD pending (no ref)" "$TD/gap2-remote-match.ndjson"  "$REM" gap2 pending
check      "gap2 pending on local cell"      "$TD/gap2-local-ref.ndjson"     "$LOC" gap2 pending

# gap5 — claude worktree startup -> agent_ready (T8). Spec carries expect.agent_ready.
AR='{"schema_version":1,"seed_bead":"hk-clp-claude","expect":{"agent_ready":{"required":true}},"gaps":["gap5"]}'
check "gap5 agent_ready pass"    "$TD/claude-agent-ready-pass.ndjson"    "$AR"     gap5 pass
check "gap5 timeout fail"        "$TD/claude-agent-ready-timeout.ndjson" "$AR"     gap5 fail
check "gap5 stall fail"          "$TD/claude-agent-ready-stall.ndjson"   "$AR"     gap5 fail
check "gap5 pending when no expect.agent_ready" "$TD/claude-agent-ready-pass.ndjson" "$CLAUDE" gap5 pending

# t10 — branch-targeting acceptance (GIT-VERIFIED, D2). t10 no longer reads the (never-
# emitted) workspace_merge_status event; the matrix runner injects the git-observed landing
# branch as ._observed_lands_on and assert_t10 compares it against expect.lands_on. The
# event stream is unused, so these rows carry ._observed_lands_on directly. Two-sided:
# landed-on-intended-branch PASSES; main-advanced FAILS. Per-bead targeting is LIVE (hk-lgykq;
# proven by daemon E2E TestMergeToMain_PerBeadIntegrationTargetLandsOnBranch).
#
# t10 NAMES NO BRANCH of its own. Both branch names are injected by the runner:
# expect.lands_on (resolved from the seed's target_branch, else from the daemon's
# defaults.lands_on) and ._trunk_branch (defaults.lands_on). The trunk here is spelled
# scratch/main because that is what scratch-daemon.sh isolate_push_target writes, and the
# rows below prove t10 follows the injected name rather than a literal `main` — the trunk
# row advances scratch/main and must FAIL, and the row that advances the LITERAL main while
# the trunk is scratch/main must fail as a landing on the wrong branch, not as a trunk move.
T10='{"schema_version":1,"seed_bead":"hk-clp-codex","expect":{"lands_on":"integration/core-loop-proof"},"_trunk_branch":"scratch/main","gaps":["t10"]}'
T10_PASS="$(printf '%s' "$T10" | jq -c '._observed_lands_on="integration/core-loop-proof"')"
T10_TRUNK="$(printf '%s' "$T10" | jq -c '._observed_lands_on="scratch/main"')"
T10_MAIN="$(printf '%s' "$T10" | jq -c '._observed_lands_on="main"')"
T10_NONE="$(printf '%s' "$T10" | jq -c '._observed_lands_on="none"')"
T10_NOTRUNK="$(printf '%s' "$T10" | jq -c '._observed_lands_on="integration/core-loop-proof" | del(._trunk_branch)')"
T10_SENTINEL="$(printf '%s' "$T10" | jq -c '._observed_lands_on="scratch/main" | .expect.lands_on="@resolved"')"
# A cell whose seed declares no target_branch: it lands ON the trunk, and that is a pass.
T10_DEFAULT="$(printf '%s' "$T10" | jq -c '.expect.lands_on="scratch/main" | ._observed_lands_on="scratch/main"')"
check        "t10 landed-on-intended-branch pass" "$TD/t10-would-pass.ndjson" "$T10_PASS" t10 pass
# check_detail, not check: the trunk-advanced and third-branch rows are the two that a t10
# comparing against the literal `main` gets RIGHT for the WRONG REASON. Both stay 'fail'
# under that defect. The reason is what separates them.
check_detail "t10 trunk advanced fail"            "$TD/t10-known-red.ndjson"  "$T10_TRUNK" t10 fail "the trunk 'scratch/main' advanced"
check_detail "t10 landed-on-a-third-branch fail"  "$TD/t10-known-red.ndjson"  "$T10_MAIN" t10 fail "landed on 'main' != intended"
check        "t10 nothing-landed fail"            "$TD/t10-would-pass.ndjson" "$T10_NONE" t10 fail
check        "t10 no trunk injected pending"      "$TD/t10-would-pass.ndjson" "$T10_NOTRUNK" t10 pending
check        "t10 unsubstituted sentinel pending" "$TD/t10-would-pass.ndjson" "$T10_SENTINEL" t10 pending
check_detail "t10 default-landing cell pass"      "$TD/t10-would-pass.ndjson" "$T10_DEFAULT" t10 pass "landed on the trunk 'scratch/main'"

# gap6 — dot review->implement round-trip, one model (D4). PASS iff REQUEST_CHANGES ->
# implementer re-dispatch -> APPROVE -> close AND every node that RESOLVED a model resolved
# the same one. FAIL if the reviewer approved on the first pass (no round-trip) or a foreign
# model reached the run.
#
# THE GOLDEN CARRIES THE REVIEWER NOW. It used to hold one harness_selected and two
# model_selected, all pi, and no reviewer selection events at all, so it proved nothing about
# the cell it stands for. The daemon never lets a reviewer inherit a SessionIDCaptured harness
# (internal/runloop ReviewerDefaultHarness), so a real pi dot run resolves claude-code at
# tier 3 for the review node and resolves NO model for it. Both events are in the stream now,
# in the CONVERGED ordering — the one where the reviewer launches LAST. That ordering broke
# both gaps at once: gap6 counted the reviewer's empty model as a foreign model, and gap1's
# last-wins read the reviewer's claude-code event as the run's harness. The shape is taken
# from the live capture .harmonik/lt-runs/pi-dot-local.ndjson. Bead: hk-vf4ju.
#
# The spec asserts gap1 beside gap6 for the same reason: the two gaps go red on OPPOSITE
# orderings, so a stream checked by one of them alone can still be red in practice.
GAP6='{"schema_version":1,"cell":"pi-dot:local","substrate":"local","seed_bead":"hk-clp-pidot","expect":{"harness_selected":{"agent_type":"pi","tier":1},"model_selected":{"harness":"pi","model":"ornith","no_leak_models":["claude-opus-4-8"]}},"gaps":["gap1","gap6"]}'
check_detail "gap6 real round-trip pass" "$TD/pi-dot-roundtrip-pass.ndjson" "$GAP6" gap6 pass "resolved no model"
check "gap6 no round-trip (first-pass APPROVE) fail" "$TD/pi-dot-noroundtrip-fail.ndjson" "$GAP6" gap6 fail
# Two leak shapes, and they arrive on DIFFERENT harnesses. gap6 must catch both, which is why
# its scope is the empty model string and NOT the harness family: gap1's family filter would
# hide the first of these two.
check_detail "gap6 foreign-harness model leak fail" "$TD/pi-dot-modelleak-fail.ndjson" "$GAP6" gap6 fail "claude-opus-4-8"
check_detail "gap6 own-harness model leak fail"     "$TD/pi-dot-ownharness-modelleak-fail.ndjson" "$GAP6" gap6 fail "claude-opus-4-8"
# gap1 on the same streams. The converged golden is the ordering in which the reviewer's
# claude-code tier-3 event is the LAST harness_selected for the seed bead; gap1 must still
# report the IMPLEMENTER's pi/tier-1 launch. The own-harness leak stream is the other side:
# the pi harness is right and the model on it is not, so gap1 fails on no_leak_models.
check_detail "gap1 on the converged round-trip reads the implementer, not the reviewer" \
             "$TD/pi-dot-roundtrip-pass.ndjson" "$GAP6" gap1 pass "harness=pi tier=1"
check_detail "gap1 own-harness model leak fail" \
             "$TD/pi-dot-ownharness-modelleak-fail.ndjson" "$GAP6" gap1 fail "LEAKED into harness pi"

echo "-----"
echo "core-loop-assert self-test: pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
