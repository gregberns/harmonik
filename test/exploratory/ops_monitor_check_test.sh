#!/usr/bin/env bash
# ops_monitor_check_test.sh — harness for scripts/ops-monitor-check.sh
#
# Injects each failure scenario via a stubbed 'harmonik' binary on PATH;
# asserts correct comms signal tier (immediate vs ≤15m digest), latest.json
# content, all-green sends nothing, and inert-queue suppression.
#
# DONE-CHECK:
#   [x] daemon-down          → immediate signal
#   [x] paused-queue         → immediate signal (non-inert crew online)
#   [x] single-mode          → immediate signal (max_concurrent==1)
#   [x] stale-crew ×2 misses → digest signal
#   [x] ready-unstaffed      → digest signal
#   [x] idle-fleet           → digest signal
#   [x] all-green            → no comms sent
#   [x] inert-queue suppression (main queue paused → no alert)
#   [x] review-gate bypass   → immediate signal (reviewer_launched, NO reviewer_verdict)
#   [x] review-gate clean    → no flag (reviewer_launched has matching verdict)
#   [x] review-gate grace    → no flag (fresh reviewer_launched, verdict may be in flight)
#   [x] review-gate suppress → no flag (auto-close/noChange run_completed, no reviewer) [R6]
#   [x] review-gate short-circuit → immediate signal (node_dispatch_requested node_id=review*,
#                                   NO reviewer_launched, NO reviewer_verdict) [hk-orni / hk-2vpj]
#   [x] review-gate req+verdict   → no flag (review node requested AND a matching verdict)
#   [x] backlog-ready        → digest signal (br ready beads + free slot)
#   [x] backlog suppressed   → no flag when all slots busy
#   [x] checks map present, schema_version=2
#
# Usage:
#   bash test/exploratory/ops_monitor_check_test.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$REPO_ROOT/scripts/ops-monitor-check.sh"

# ── Test state ────────────────────────────────────────────────────────────────

PASS=0
FAIL=0
FAILURES=()

pass() { PASS=$((PASS+1)); echo "  PASS: $1"; }
fail() {
  FAIL=$((FAIL+1))
  FAILURES+=("$1")
  echo "  FAIL: $1"
}

assert_contains() {
  local label="$1" needle="$2" haystack="$3"
  if echo "$haystack" | grep -qF "$needle"; then
    pass "$label"
  else
    fail "$label (needle='$needle' not found)"
  fi
}

assert_not_contains() {
  local label="$1" needle="$2" haystack="$3"
  if ! echo "$haystack" | grep -qF "$needle"; then
    pass "$label"
  else
    fail "$label (needle='$needle' should NOT be present)"
  fi
}

assert_eq() {
  local label="$1" expected="$2" actual="$3"
  if [[ "$actual" == "$expected" ]]; then
    pass "$label"
  else
    fail "$label (expected='$expected' actual='$actual')"
  fi
}

assert_json_bool() {
  local label="$1" file="$2" field="$3" expected_bool="$4"
  local actual
  actual=$(python3 -c "import json; d=json.load(open('$file')); print(str(d.get('$field',None)).lower())" 2>/dev/null || echo "")
  if [[ "$actual" == "$expected_bool" ]]; then
    pass "$label"
  else
    fail "$label (json[$field]: expected='$expected_bool' actual='$actual')"
  fi
}

assert_json_list_contains() {
  local label="$1" file="$2" field="$3" needle="$4"
  local actual
  actual=$(python3 -c "
import json
d = json.load(open('$file'))
lst = d.get('$field', [])
print('yes' if any('$needle' in str(x) for x in lst) else 'no')
" 2>/dev/null || echo "no")
  if [[ "$actual" == "yes" ]]; then
    pass "$label"
  else
    fail "$label (json[$field] does not contain '$needle')"
  fi
}

assert_json_list_empty() {
  local label="$1" file="$2" field="$3"
  local actual
  actual=$(python3 -c "import json; d=json.load(open('$file')); print(len(d.get('$field',[])))" 2>/dev/null || echo "")
  if [[ "$actual" == "0" ]]; then
    pass "$label"
  else
    fail "$label (json[$field] should be empty, got length=$actual)"
  fi
}

# ── Fixture builder ───────────────────────────────────────────────────────────
# Creates a temp project tree with a stubbed 'harmonik' binary.
# Always writes captured comms sends to $PROJ/.harmonik/ops-monitor/comms_sent.log
# Prints the temp project dir on stdout.

setup_fixture() {
  local tmpdir
  tmpdir="$(mktemp -d)"
  mkdir -p "$tmpdir/.harmonik/events" "$tmpdir/.harmonik/ops-monitor"

  local hk_qs_exit=0
  local hk_qs_json='{"status":"ok"}'
  local hk_ql_json='{"queues":[],"max_concurrent":4}'
  local hk_cw_json=''
  local state_json=''
  local events_content=''
  local br_ready_json='[]'   # `br ready --limit 0 --json` output for the stub

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --hk-queue-status-exit)   hk_qs_exit="$2";    shift 2 ;;
      --hk-queue-status-json)   hk_qs_json="$2";    shift 2 ;;
      --hk-queue-list-json)     hk_ql_json="$2";    shift 2 ;;
      --hk-comms-who-json)      hk_cw_json="$2";    shift 2 ;;
      --state-json)             state_json="$2";    shift 2 ;;
      --events-jsonl)           events_content="$2";shift 2 ;;
      --br-ready-json)          br_ready_json="$2"; shift 2 ;;
      *) echo "setup_fixture: unknown arg $1" >&2; return 1 ;;
    esac
  done

  if [[ -n "$state_json" ]]; then
    printf '%s\n' "$state_json" > "$tmpdir/.harmonik/ops-monitor/state.json"
  fi
  if [[ -n "$events_content" ]]; then
    printf '%s\n' "$events_content" > "$tmpdir/.harmonik/events/events.jsonl"
  fi

  # The comms capture log lives at a fixed known path inside the project.
  local comms_log="$tmpdir/.harmonik/ops-monitor/comms_sent.log"

  # Escape values for embedding in the stub (avoid single-quote conflicts)
  local qs_exit="$hk_qs_exit"
  local qs_json_escaped
  qs_json_escaped=$(printf '%s' "$hk_qs_json" | sed "s/'/'\\\\''/g")
  local ql_json_escaped
  ql_json_escaped=$(printf '%s' "$hk_ql_json" | sed "s/'/'\\\\''/g")
  local cw_json_escaped
  cw_json_escaped=$(printf '%s' "$hk_cw_json" | sed "s/'/'\\\\''/g")

  local stub_bin="$tmpdir/bin/harmonik"
  mkdir -p "$tmpdir/bin"

  # Write stub using a heredoc so variable expansion is controlled
  cat > "$stub_bin" <<EOF
#!/usr/bin/env bash
# Stub harmonik
HK_QS_EXIT=${qs_exit}
HK_QS_JSON='${qs_json_escaped}'
HK_QL_JSON='${ql_json_escaped}'
HK_CW_JSON='${cw_json_escaped}'
COMMS_LOG='${comms_log}'

case "\$*" in
  "queue status --json"|"queue status")
    if [[ \$HK_QS_EXIT -ne 0 ]]; then exit \$HK_QS_EXIT; fi
    printf '%s\n' "\$HK_QS_JSON"
    ;;
  "queue list --json"|"queue list")
    printf '%s\n' "\$HK_QL_JSON"
    ;;
  "comms who --json"|"comms who")
    if [[ -n "\$HK_CW_JSON" ]]; then printf '%s\n' "\$HK_CW_JSON"; fi
    ;;
  comms\ send\ *)
    args=("\$@")
    body=""
    for ((i=0; i<\${#args[@]}; i++)); do
      if [[ "\${args[i]}" == "--" ]]; then
        body="\${args[i+1]}"
        break
      fi
    done
    printf '%s\n' "\$body" >> "\$COMMS_LOG"
    ;;
  *)
    echo "stub: unhandled harmonik \$*" >&2
    exit 1
    ;;
esac
EOF
  chmod +x "$stub_bin"

  # Stub `br` so the backlog-readiness check is deterministic in tests.
  local br_ready_escaped
  br_ready_escaped=$(printf '%s' "$br_ready_json" | sed "s/'/'\\\\''/g")
  local br_bin="$tmpdir/bin/br"
  cat > "$br_bin" <<EOF
#!/usr/bin/env bash
# Stub br
BR_READY_JSON='${br_ready_escaped}'
case "\$*" in
  ready*--json*|*ready*--json*)
    printf '%s\n' "\$BR_READY_JSON"
    ;;
  *)
    exit 0
    ;;
esac
EOF
  chmod +x "$br_bin"

  printf '%s' "$tmpdir"
}

run_check() {
  local proj="$1"
  HK_PROJECT="$proj" PATH="$proj/bin:$PATH" bash "$SCRIPT" 2>&1
}

comms_log() { echo "$1/.harmonik/ops-monitor/comms_sent.log"; }

# ISO8601 timestamp N seconds in the past
ts_ago() {
  python3 -c "
import datetime
d = datetime.datetime.utcnow() - datetime.timedelta(seconds=int('$1'))
print(d.strftime('%Y-%m-%dT%H:%M:%SZ'))
"
}

# ── Test 1: all-green — no signals, no comms sent ─────────────────────────────
echo ""
echo "=== Test 1: all-green — no comms sent ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "all-green stdout"        "all-green"    "$OUTPUT"
assert_json_bool "latest.json daemon_up"  "$PROJ/.harmonik/ops-monitor/latest.json" "daemon_up" "true"
assert_json_bool "latest.json all_green"  "$PROJ/.harmonik/ops-monitor/latest.json" "all_green" "true"
assert_json_list_empty "no immediate_signals" "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals"
assert_json_list_empty "no digest_signals"    "$PROJ/.harmonik/ops-monitor/latest.json" "digest_signals"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  fail "all-green: no comms should be sent"
else
  pass "all-green: no comms sent"
fi
rm -rf "$PROJ"

# ── Test 2: daemon-down — immediate signal ────────────────────────────────────
echo ""
echo "=== Test 2: daemon-down — immediate comms ==="
PROJ=$(setup_fixture \
  --hk-queue-status-exit 17 \
  --hk-comms-who-json '' \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "daemon-down stdout IMMEDIATE" "IMMEDIATE"   "$OUTPUT"
assert_contains "daemon-down stdout signal"    "daemon-down" "$OUTPUT"
assert_json_bool "latest.json daemon_up=false" "$PROJ/.harmonik/ops-monitor/latest.json" "daemon_up" "false"
assert_json_list_contains "immediate_signals has daemon-down" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals" "daemon-down"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "daemon-down: comms sent"
  assert_contains "daemon-down comms [IMMEDIATE]" "[IMMEDIATE]" "$(cat "$LOG")"
  assert_contains "daemon-down comms signal"      "daemon-down" "$(cat "$LOG")"
else
  fail "daemon-down: expected comms send, got none"
fi
rm -rf "$PROJ"

# ── Test 3: paused-queue (non-inert crew online) — immediate ──────────────────
echo ""
echo "=== Test 3: paused-queue (non-inert crew online) — immediate ==="
CREW_TS=$(ts_ago 10)
COMMS_WHO='{"agent":"myagent","status":"online","last_seen":"'"$CREW_TS"'"}'
QLIST='{"queues":[{"name":"myagent-q","status":"paused-by-failure","workers":0,"pending_items":0,"failed_items":1}],"max_concurrent":4}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json "$QLIST" \
  --hk-comms-who-json "$COMMS_WHO" \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "paused-queue stdout IMMEDIATE" "IMMEDIATE"    "$OUTPUT"
assert_contains "paused-queue stdout signal"    "paused-queue" "$OUTPUT"
assert_json_list_contains "immediate_signals has paused-queue" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals" "paused-queue"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "paused-queue: comms sent"
  assert_contains "paused-queue comms [IMMEDIATE]" "[IMMEDIATE]"   "$(cat "$LOG")"
  assert_contains "paused-queue comms signal"      "paused-queue"  "$(cat "$LOG")"
else
  fail "paused-queue: expected comms send, got none"
fi
rm -rf "$PROJ"

# ── Test 4: single-mode (max_concurrent==1) — immediate ──────────────────────
echo ""
echo "=== Test 4: single-mode (max_concurrent==1) — immediate ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":1}' \
  --hk-comms-who-json '' \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "single-mode stdout IMMEDIATE" "IMMEDIATE"   "$OUTPUT"
assert_contains "single-mode stdout signal"    "single-mode" "$OUTPUT"
assert_json_list_contains "immediate_signals has single-mode" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals" "single-mode"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "single-mode: comms sent"
  assert_contains "single-mode comms [IMMEDIATE]" "[IMMEDIATE]"  "$(cat "$LOG")"
  assert_contains "single-mode comms signal"      "single-mode"  "$(cat "$LOG")"
else
  fail "single-mode: expected comms send, got none"
fi
rm -rf "$PROJ"

# ── Test 5: stale-crew (2 consecutive misses) — digest ───────────────────────
echo ""
echo "=== Test 5: stale-crew (2 misses) — digest ==="
STALE_TS=$(ts_ago 300)   # 5 min > 150s threshold
CW='{"agent":"alice","status":"online","last_seen":"'"$STALE_TS"'"}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json "$CW" \
)

# Run 1: first miss — should NOT signal yet
run_check "$PROJ" > /dev/null 2>&1
MISSES=$(python3 -c "
import json
d = json.load(open('$PROJ/.harmonik/ops-monitor/state.json'))
print(d.get('stale_crew_misses', {}).get('alice', 0))
")
assert_eq "stale-crew: miss count after run 1 = 1" "1" "$MISSES"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  fail "stale-crew run1: should not send comms on first miss"
else
  pass "stale-crew run1: no comms on first miss"
fi

# Run 2: second miss — should emit digest
OUTPUT2=$(run_check "$PROJ")
assert_contains "stale-crew run2 digest"      "digest"     "$OUTPUT2"
assert_contains "stale-crew run2 signal name" "crew-stale" "$OUTPUT2"
assert_json_list_contains "digest_signals has crew-stale" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "digest_signals" "crew-stale"
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "stale-crew run2: comms sent"
  assert_contains "stale-crew comms [DIGEST]" "[DIGEST]"    "$(cat "$LOG")"
  assert_contains "stale-crew comms signal"   "crew-stale"  "$(cat "$LOG")"
else
  fail "stale-crew run2: expected digest comms, got none"
fi
rm -rf "$PROJ"

# ── Test 6: ready-unstaffed — digest ─────────────────────────────────────────
echo ""
echo "=== Test 6: ready-unstaffed — digest ==="
QLIST='{"queues":[{"name":"crew1-q","status":"active","workers":0,"pending_items":3,"failed_items":0}],"max_concurrent":4}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json "$QLIST" \
  --hk-comms-who-json '' \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "ready-unstaffed stdout digest" "digest"           "$OUTPUT"
assert_contains "ready-unstaffed stdout signal" "ready-unstaffed"  "$OUTPUT"
assert_json_list_contains "digest_signals has ready-unstaffed" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "digest_signals" "ready-unstaffed"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "ready-unstaffed: comms sent"
  assert_contains "ready-unstaffed comms [DIGEST]" "[DIGEST]"          "$(cat "$LOG")"
  assert_contains "ready-unstaffed comms signal"   "ready-unstaffed"   "$(cat "$LOG")"
else
  fail "ready-unstaffed: expected digest comms, got none"
fi
rm -rf "$PROJ"

# ── Test 7: idle-fleet (>20m no run events) — digest ─────────────────────────
echo ""
echo "=== Test 7: idle-fleet (>20m no run events) — digest ==="
OLD_TS=$(ts_ago 1500)   # 25 min > 20 min threshold
EVENTS='{"type":"run_completed","ts":"'"$OLD_TS"'","run_id":"r1"}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --events-jsonl "$EVENTS" \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "idle-fleet stdout digest" "digest"     "$OUTPUT"
assert_contains "idle-fleet stdout signal" "idle-fleet" "$OUTPUT"
assert_json_bool "latest.json idle_fleet=true" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "idle_fleet" "true"
assert_json_list_contains "digest_signals has idle-fleet" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "digest_signals" "idle-fleet"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "idle-fleet: comms sent"
  assert_contains "idle-fleet comms [DIGEST]" "[DIGEST]"    "$(cat "$LOG")"
  assert_contains "idle-fleet comms signal"   "idle-fleet"  "$(cat "$LOG")"
else
  fail "idle-fleet: expected digest comms, got none"
fi
rm -rf "$PROJ"

# ── Test 8: inert-queue suppression (main queue paused → no alert) ────────────
echo ""
echo "=== Test 8: inert-queue suppression (main queue paused) — no alert ==="
CREW_TS=$(ts_ago 10)
CW='{"agent":"main","status":"online","last_seen":"'"$CREW_TS"'"}'
QLIST='{"queues":[{"name":"main","status":"paused-by-failure","workers":0,"pending_items":0,"failed_items":2}],"max_concurrent":4}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json "$QLIST" \
  --hk-comms-who-json "$CW" \
)
OUTPUT=$(run_check "$PROJ")
assert_not_contains "inert suppression: no paused-queue in stdout" "paused-queue" "$OUTPUT"
assert_json_list_empty "inert suppression: no immediate_signals" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  fail "inert suppression: should NOT have sent comms"
else
  pass "inert suppression: no comms sent"
fi
rm -rf "$PROJ"

# ── Test 9: latest.json always written even on daemon-down ───────────────────
echo ""
echo "=== Test 9: latest.json written on daemon-down ==="
PROJ=$(setup_fixture --hk-queue-status-exit 17 --hk-comms-who-json '')
run_check "$PROJ" > /dev/null 2>&1
if [[ -f "$PROJ/.harmonik/ops-monitor/latest.json" ]]; then
  pass "latest.json exists after daemon-down"
else
  fail "latest.json missing after daemon-down"
fi
rm -rf "$PROJ"

# ── checks-map assertion helper ───────────────────────────────────────────────
assert_check_state() {
  local label="$1" file="$2" check="$3" expected="$4"
  local actual
  actual=$(python3 -c "
import json
d = json.load(open('$file'))
print(d.get('checks', {}).get('$check', {}).get('state', 'MISSING'))
" 2>/dev/null || echo "MISSING")
  if [[ "$actual" == "$expected" ]]; then
    pass "$label"
  else
    fail "$label (checks[$check].state expected='$expected' actual='$actual')"
  fi
}

# ── Test 10: review-gate bypass — reviewer_launched but NO reviewer_verdict ────
echo ""
echo "=== Test 10: review-gate bypass (reviewer launched, no verdict) — immediate ==="
OLD_WALL=$(ts_ago 600)   # 10 min old → past the 180s grace, judgeable
# A run_id 'r-bypass' that LAUNCHED a reviewer (entered the review path) but has NO
# matching reviewer_verdict → genuine review bypass. The accompanying run_completed
# is incidental; the flag is driven by reviewer_launched ∖ reviewer_verdict (R6 fix).
EVENTS='{"type":"reviewer_launched","timestamp_wall":"'"$OLD_WALL"'","run_id":"r-bypass","payload":{"run_id":"r-bypass"}}
{"type":"run_completed","timestamp_wall":"'"$OLD_WALL"'","payload":{"run_id":"r-bypass","success":true}}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --events-jsonl "$EVENTS" \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "review-bypass stdout IMMEDIATE" "IMMEDIATE"     "$OUTPUT"
assert_contains "review-bypass stdout signal"    "review-bypass" "$OUTPUT"
assert_json_list_contains "immediate_signals has review-bypass" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals" "review-bypass"
assert_json_list_contains "review_bypass_run_ids has r-bypass" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review_bypass_run_ids" "r-bypass"
assert_check_state "checks.review-gate=flag" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review-gate" "flag"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "review-bypass: comms sent"
  assert_contains "review-bypass comms [IMMEDIATE]" "[IMMEDIATE]"   "$(cat "$LOG")"
  assert_contains "review-bypass comms signal"      "review-bypass" "$(cat "$LOG")"
else
  fail "review-bypass: expected comms send, got none"
fi
rm -rf "$PROJ"

# ── Test 11: review-gate clean — reviewer launched AND a matching verdict ──────
echo ""
echo "=== Test 11: review-gate clean (reviewer launched + verdict) — no flag ==="
OLD_WALL=$(ts_ago 600)
EVENTS='{"type":"reviewer_launched","timestamp_wall":"'"$OLD_WALL"'","run_id":"r-ok","payload":{"run_id":"r-ok"}}
{"type":"run_completed","timestamp_wall":"'"$OLD_WALL"'","payload":{"run_id":"r-ok","success":true}}
{"type":"reviewer_verdict","timestamp_wall":"'"$OLD_WALL"'","run_id":"r-ok","payload":{"run_id":"r-ok","verdict":"APPROVE"}}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --events-jsonl "$EVENTS" \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "review-gate clean all-green" "all-green" "$OUTPUT"
assert_json_list_empty "review_bypass_run_ids empty" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review_bypass_run_ids"
assert_check_state "checks.review-gate=ok" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review-gate" "ok"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  fail "review-gate clean: no comms should be sent"
else
  pass "review-gate clean: no comms sent"
fi
rm -rf "$PROJ"

# ── Test 12: review-gate grace — fresh reviewer_launched NOT yet judged ───────
echo ""
echo "=== Test 12: review-gate grace (fresh reviewer launch, verdict in flight) — no flag ==="
FRESH_WALL=$(ts_ago 30)   # <180s grace → skip, do not call bypass
EVENTS='{"type":"reviewer_launched","timestamp_wall":"'"$FRESH_WALL"'","run_id":"r-fresh","payload":{"run_id":"r-fresh"}}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --events-jsonl "$EVENTS" \
)
OUTPUT=$(run_check "$PROJ")
assert_json_list_empty "grace: review_bypass_run_ids empty" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review_bypass_run_ids"
assert_check_state "grace: checks.review-gate=ok" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review-gate" "ok"
rm -rf "$PROJ"

# ── Test 12b: review-gate SUPPRESSION — legitimate review-LESS close path ─────
# The daemon auto-closes runs with NO reviewer (MVH twin-blind `auto-close: exit=0`,
# noChange, subsumed — workloop.go ~:3811). Those emit run_completed but NEVER
# reviewer_launched, so they must NOT be flagged (R6 fix hk-ayvx). This is the
# regression the old completed∖verdict join produced (~180 false positives).
echo ""
echo "=== Test 12b: review-gate suppression (auto-close + noChange, no reviewer) — no flag ==="
OLD_WALL=$(ts_ago 600)   # past grace, so the only reason NOT to flag is the launch-gate
# Two legitimate review-less closes: an auto-close and a noChange-subsumed run, each
# with a run_completed but NO reviewer_launched / reviewer_verdict.
EVENTS='{"type":"run_completed","timestamp_wall":"'"$OLD_WALL"'","payload":{"run_id":"r-autoclose","success":true,"summary":"auto-close: exit=0"}}
{"type":"run_completed","timestamp_wall":"'"$OLD_WALL"'","payload":{"run_id":"r-nochange","success":true,"summary":"noChange-subsumed: bead found in main"}}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --events-jsonl "$EVENTS" \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "suppression: all-green (no review-bypass)" "all-green" "$OUTPUT"
assert_not_contains "suppression: no review-bypass in stdout" "review-bypass" "$OUTPUT"
assert_json_list_empty "suppression: review_bypass_run_ids empty" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review_bypass_run_ids"
assert_check_state "suppression: checks.review-gate=ok" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review-gate" "ok"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  fail "suppression: no comms should be sent for review-less auto-close runs"
else
  pass "suppression: no comms sent for auto-close/noChange runs"
fi
rm -rf "$PROJ"

# ── Test 12c: review-gate SHORT-CIRCUIT — review REQUESTED, never launched, no verdict ─
# The hk-2vpj engine-short-circuit class (hk-orni): the engine emitted
# node_dispatch_requested node_id=review* (a review WAS requested) but the reviewer never
# launched (NO reviewer_launched) and produced NO reviewer_verdict — the change merged +
# closed UNREVIEWED. The launched-only gate (R6) MISSED this; the node_dispatch_requested
# arm catches it. node_id lives under .payload; run_id is top-level.
echo ""
echo "=== Test 12c: review-gate short-circuit (review requested, never launched, no verdict) — immediate ==="
OLD_WALL=$(ts_ago 600)   # past the 180s grace, judgeable
EVENTS='{"type":"node_dispatch_requested","timestamp_wall":"'"$OLD_WALL"'","run_id":"r-shortcircuit","payload":{"node_id":"review_correctness","run_id":"r-shortcircuit"}}
{"type":"run_completed","timestamp_wall":"'"$OLD_WALL"'","payload":{"run_id":"r-shortcircuit","success":true}}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --events-jsonl "$EVENTS" \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "short-circuit stdout IMMEDIATE" "IMMEDIATE"     "$OUTPUT"
assert_contains "short-circuit stdout signal"    "review-bypass" "$OUTPUT"
assert_json_list_contains "immediate_signals has review-bypass (short-circuit)" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals" "review-bypass"
assert_json_list_contains "review_bypass_run_ids has r-shortcircuit" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review_bypass_run_ids" "r-shortcircuit"
assert_check_state "short-circuit: checks.review-gate=flag" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review-gate" "flag"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "short-circuit: comms sent"
  assert_contains "short-circuit comms [IMMEDIATE]" "[IMMEDIATE]"   "$(cat "$LOG")"
  assert_contains "short-circuit comms signal"      "review-bypass" "$(cat "$LOG")"
else
  fail "short-circuit: expected comms send, got none"
fi
rm -rf "$PROJ"

# ── Test 12d: review-gate REQUEST + VERDICT — review requested AND a verdict — no flag ─
# A run that requested a review node AND produced a reviewer_verdict (the normal path,
# whether or not a separate reviewer_launched event exists) is cleared by the verdict.
echo ""
echo "=== Test 12d: review-gate request + verdict (review requested, verdict present) — no flag ==="
OLD_WALL=$(ts_ago 600)
EVENTS='{"type":"node_dispatch_requested","timestamp_wall":"'"$OLD_WALL"'","run_id":"r-reqok","payload":{"node_id":"review","run_id":"r-reqok"}}
{"type":"run_completed","timestamp_wall":"'"$OLD_WALL"'","payload":{"run_id":"r-reqok","success":true}}
{"type":"reviewer_verdict","timestamp_wall":"'"$OLD_WALL"'","run_id":"r-reqok","payload":{"run_id":"r-reqok","verdict":"APPROVE"}}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --events-jsonl "$EVENTS" \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "request+verdict all-green" "all-green" "$OUTPUT"
assert_json_list_empty "request+verdict: review_bypass_run_ids empty" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review_bypass_run_ids"
assert_check_state "request+verdict: checks.review-gate=ok" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review-gate" "ok"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  fail "request+verdict: no comms should be sent"
else
  pass "request+verdict: no comms sent"
fi
rm -rf "$PROJ"

# ── Test 13: backlog-ready — br ready shows beads + free slot — digest ────────
echo ""
echo "=== Test 13: backlog-ready (ready beads + free slot) — digest ==="
BR_READY='[{"id":"hk-1"},{"id":"hk-2"},{"id":"hk-3"}]'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --br-ready-json "$BR_READY" \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "backlog-ready stdout digest" "digest"        "$OUTPUT"
assert_contains "backlog-ready stdout signal" "backlog-ready" "$OUTPUT"
assert_json_bool "latest.json backlog_ready=true" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "backlog_ready" "true"
assert_json_list_contains "digest_signals has backlog-ready" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "digest_signals" "backlog-ready"
assert_check_state "checks.backlog-ready=flag" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "backlog-ready" "flag"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "backlog-ready: comms sent"
  assert_contains "backlog-ready comms [DIGEST]" "[DIGEST]"      "$(cat "$LOG")"
  assert_contains "backlog-ready comms signal"   "backlog-ready" "$(cat "$LOG")"
else
  fail "backlog-ready: expected digest comms, got none"
fi
rm -rf "$PROJ"

# ── Test 14: backlog-ready suppressed when no free slot ───────────────────────
echo ""
echo "=== Test 14: backlog-ready suppressed (ready beads but all slots busy) ==="
BR_READY='[{"id":"hk-1"}]'
# 4 active workers == max_concurrent 4 → no free slot → no backlog-ready flag.
QLIST='{"queues":[{"name":"crewa-q","status":"active","workers":4,"pending_items":0,"failed_items":0}],"max_concurrent":4}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json "$QLIST" \
  --hk-comms-who-json '' \
  --br-ready-json "$BR_READY" \
)
OUTPUT=$(run_check "$PROJ")
assert_json_bool "latest.json backlog_ready=false (no slot)" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "backlog_ready" "false"
assert_check_state "checks.backlog-ready=ok (no slot)" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "backlog-ready" "ok"
rm -rf "$PROJ"

# ── Test 15: checks map present + schema_version 2 ────────────────────────────
echo ""
echo "=== Test 15: checks map present, schema_version=2 ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
)
run_check "$PROJ" > /dev/null 2>&1
SCHEMA=$(python3 -c "import json; print(json.load(open('$PROJ/.harmonik/ops-monitor/latest.json')).get('schema_version'))")
assert_eq "schema_version=2" "2" "$SCHEMA"
for CHK in daemon-up paused-queues single-mode crew-fresh review-gate backlog-ready lull; do
  assert_check_state "checks.$CHK present (green)" \
    "$PROJ/.harmonik/ops-monitor/latest.json" "$CHK" "ok"
done
rm -rf "$PROJ"

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "════════════════════════════════════════════"
echo "Results: $PASS passed, $FAIL failed"
if [[ ${#FAILURES[@]} -gt 0 ]]; then
  echo ""
  echo "Failed assertions:"
  for f in "${FAILURES[@]}"; do
    echo "  - $f"
  done
  echo "════════════════════════════════════════════"
  exit 1
fi
echo "════════════════════════════════════════════"
exit 0
