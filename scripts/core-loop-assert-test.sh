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
T10='{"schema_version":1,"seed_bead":"hk-clp-codex","expect":{"lands_on":"integration/core-loop-proof"},"gaps":["t10"]}'
T10_PASS="$(printf '%s' "$T10" | jq -c '._observed_lands_on="integration/core-loop-proof"')"
T10_MAIN="$(printf '%s' "$T10" | jq -c '._observed_lands_on="main"')"
T10_NONE="$(printf '%s' "$T10" | jq -c '._observed_lands_on="none"')"
check "t10 landed-on-intended-branch pass" "$TD/t10-would-pass.ndjson" "$T10_PASS" t10 pass
check "t10 landed-on-main fail"            "$TD/t10-known-red.ndjson"  "$T10_MAIN" t10 fail
check "t10 nothing-landed fail"            "$TD/t10-would-pass.ndjson" "$T10_NONE" t10 fail

# gap6 — dot review->implement round-trip, same model (D4). PASS iff REQUEST_CHANGES ->
# implementer re-dispatch -> APPROVE -> close AND every model_selected == the pinned model.
# FAIL if the reviewer approved on the first pass (no round-trip) or a foreign model leaked.
GAP6='{"schema_version":1,"seed_bead":"hk-clp-pidot","substrate":"local","expect":{"model_selected":{"harness":"pi","model":"ornith"}},"gaps":["gap6"]}'
check "gap6 real round-trip pass"      "$TD/pi-dot-roundtrip-pass.ndjson"  "$GAP6" gap6 pass
check "gap6 no round-trip (first-pass APPROVE) fail" "$TD/pi-dot-noroundtrip-fail.ndjson" "$GAP6" gap6 fail
check "gap6 same-model leak (claude) fail" "$TD/pi-dot-modelleak-fail.ndjson"  "$GAP6" gap6 fail

echo "-----"
echo "core-loop-assert self-test: pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
