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

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --hk-queue-status-exit)   hk_qs_exit="$2";    shift 2 ;;
      --hk-queue-status-json)   hk_qs_json="$2";    shift 2 ;;
      --hk-queue-list-json)     hk_ql_json="$2";    shift 2 ;;
      --hk-comms-who-json)      hk_cw_json="$2";    shift 2 ;;
      --state-json)             state_json="$2";    shift 2 ;;
      --events-jsonl)           events_content="$2";shift 2 ;;
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
