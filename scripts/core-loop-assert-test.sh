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
# check <name> <ndjson> <spec-json> <gap> <expected-verdict>
check() {
    local name="$1" stream="$2" spec="$3" gap="$4" want="$5" got
    # -f runs the library as THE program; the per-gap verdict is extracted by a second jq
    # (an inline filter alongside -f would be parsed as an input file, not a program).
    got="$(jq -n --slurpfile events "$stream" --argjson spec "$spec" -f "$LIB" \
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

echo "-----"
echo "core-loop-assert self-test: pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
