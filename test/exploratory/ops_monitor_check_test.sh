#!/usr/bin/env bash
# ops_monitor_check_test.sh — harness for scripts/ops-monitor-check.sh
#
# Injects each failure scenario via a stubbed 'harmonik' binary on PATH;
# asserts correct comms signal tier (immediate vs ≤15m digest), latest.json
# content, all-green sends nothing, and inert-queue suppression.
#
# DONE-CHECK:
#   [x] daemon-down          → immediate signal
#   [x] supervisor-down      → immediate signal (supervisor not running; no auto-revive)
#   [x] fleet-down           → immediate signal (both daemon and supervisor down; hk-pen9)
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
#   [x] critical-component re-alert after 5 min (daemon-down re-alerts at 6 min,
#                                                 would be suppressed under old 30 min cd)
#   [x] non-critical suppressed within 30 min   (paused-queue at 6 min stays suppressed)
#   [x] escalation tier-1 — first alert: count=1, [IMMEDIATE], no ops-CRITICAL
#   [x] escalation tier-2 — second alert: count=2, [ESCALATION], no ops-CRITICAL
#   [x] escalation tier-3 by count — count>=6: [ESCALATION] + ops-CRITICAL topic
#   [x] escalation tier-3 by elapsed — elapsed>=1800s: ops-CRITICAL topic
#   [x] escalation resolved — signal clears count+state, no comms
#   [x] backward-compat — bare int alerted entry normalized to count=1 on re-alert
#   [x] captain absent from comms-who, tmux alive → no captain-down (Test 25a)
#   [x] captain absent from comms-who AND no tmux session → captain-down immediate (Test 25b)
#   [x] stale captain in comms-who NOT in crew-stale signal (NON_CREW exclusion) (Test 25c)
#   [x] keepers-covered: crew WITH keeper → no signal (Test 26a)
#   [x] keeper-missing debounce: 1st miss no signal; >=miss_limit → immediate 'keeper-missing' (Test 26b)
#   [x] keeper-missing NON_CREW: captain etc never produce keeper-missing (Test 26c)
#   [x] keeper-missing clears: keeper reappears → signal gone, state reset (Test 26d)
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
  local hk_sv_json='{"schema_version":1,"running":true,"status":"running"}'
  local hk_project_hash='aabb1122ccdd'
  local tmux_captain_alive=true   # default: captain tmux session alive (happy path)
  local state_json=''
  local events_content=''
  local br_ready_json='[]'   # `br ready --limit 0 --json` output for the stub
  local keeper_procs_raw=''  # lines matching "harmonik keeper --agent"; default: none

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --hk-queue-status-exit)      hk_qs_exit="$2";    shift 2 ;;
      --hk-queue-status-json)      hk_qs_json="$2";    shift 2 ;;
      --hk-queue-list-json)        hk_ql_json="$2";    shift 2 ;;
      --hk-comms-who-json)         hk_cw_json="$2";    shift 2 ;;
      --hk-supervise-status-json)  hk_sv_json="$2";    shift 2 ;;
      --hk-project-hash)           hk_project_hash="$2"; shift 2 ;;
      --tmux-captain-alive)        tmux_captain_alive="$2"; shift 2 ;;
      --state-json)                state_json="$2";    shift 2 ;;
      --events-jsonl)              events_content="$2";shift 2 ;;
      --br-ready-json)             br_ready_json="$2"; shift 2 ;;
      --keeper-procs-raw)          keeper_procs_raw="$2"; shift 2 ;;
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
  local sv_json_escaped
  sv_json_escaped=$(printf '%s' "$hk_sv_json" | sed "s/'/'\\\\''/g")

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
HK_SV_JSON='${sv_json_escaped}'
HK_PROJECT_HASH='${hk_project_hash}'
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
  "supervise status --json"|"supervise status")
    printf '%s\n' "\$HK_SV_JSON"
    ;;
  "project-hash")
    printf '%s\n' "\$HK_PROJECT_HASH"
    ;;
  comms\ send\ *)
    args=("\$@")
    body=""
    topic="ops-monitor"
    for ((i=0; i<\${#args[@]}; i++)); do
      if [[ "\${args[i]}" == "--topic" ]]; then
        topic="\${args[i+1]}"
      fi
      if [[ "\${args[i]}" == "--" ]]; then
        body="\${args[i+1]}"
        break
      fi
    done
    printf 'topic=%s -- %s\n' "\$topic" "\$body" >> "\$COMMS_LOG"
    ;;
  *)
    echo "stub: unhandled harmonik \$*" >&2
    exit 1
    ;;
esac
EOF
  chmod +x "$stub_bin"

  # Stub `tmux` for captain-liveness probe (has-session -t harmonik-*-captain).
  local tmux_alive_val="${tmux_captain_alive}"
  local tmux_bin="$tmpdir/bin/tmux"
  cat > "$tmux_bin" <<EOF
#!/usr/bin/env bash
# Stub tmux for ops-monitor captain-liveness test
TMUX_CAPTAIN_ALIVE=${tmux_alive_val}
if [[ "\$1" == "has-session" ]]; then
  if [[ "\$TMUX_CAPTAIN_ALIVE" == "true" ]]; then
    exit 0
  else
    exit 1
  fi
fi
exit 0
EOF
  chmod +x "$tmux_bin"

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

  # Stub `ps` for keeper-coverage probe.
  # The script runs: ps -axo command= 2>/dev/null | grep -F "harmonik keeper --agent"
  # The stub emits keeper_procs_raw; grep then filters to matching lines naturally.
  local ps_bin="$tmpdir/bin/ps"
  local keeper_procs_escaped
  keeper_procs_escaped=$(printf '%s' "$keeper_procs_raw" | sed "s/'/'\\\\''/g")
  cat > "$ps_bin" <<EOF
#!/usr/bin/env bash
# Stub ps: returns configured keeper proc cmdlines regardless of flags
printf '%s\n' '${keeper_procs_escaped}'
EOF
  chmod +x "$ps_bin"

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
  --keeper-procs-raw "harmonik keeper --agent alice --tmux hk-aabb1122ccdd-alice" \
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
for CHK in daemon-up supervisor-up paused-queues single-mode crew-fresh review-gate captain-up backlog-ready lull keepers-covered; do
  assert_check_state "checks.$CHK present (green)" \
    "$PROJ/.harmonik/ops-monitor/latest.json" "$CHK" "ok"
done
rm -rf "$PROJ"

# ── Test 16: supervisor-down → immediate signal ───────────────────────────────
# Daemon is up but supervisor is not running. DaemonWatchdog is dead so the fleet
# has no auto-revive path for the daemon if it crashes (hk-pen9).
echo ""
echo "=== Test 16: supervisor-down — immediate comms ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --hk-supervise-status-json '{"schema_version":1,"running":false,"status":"stopped"}' \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "supervisor-down stdout IMMEDIATE" "IMMEDIATE"       "$OUTPUT"
assert_contains "supervisor-down stdout signal"    "supervisor-down" "$OUTPUT"
assert_json_list_contains "immediate_signals has supervisor-down" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals" "supervisor-down"
assert_json_bool "latest.json daemon_up=true"      "$PROJ/.harmonik/ops-monitor/latest.json" "daemon_up" "true"
assert_json_bool "latest.json supervisor_up=false" "$PROJ/.harmonik/ops-monitor/latest.json" "supervisor_up" "false"
assert_check_state "checks.supervisor-up flagged" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "supervisor-up" "flag"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "supervisor-down: comms sent"
  assert_contains "supervisor-down comms [IMMEDIATE]" "[IMMEDIATE]"       "$(cat "$LOG")"
  assert_contains "supervisor-down comms signal"      "supervisor-down"   "$(cat "$LOG")"
else
  fail "supervisor-down: expected comms send, got none"
fi
rm -rf "$PROJ"

# ── Test 17: fleet-down (daemon + supervisor both down) → immediate ────────────
# Both daemon and supervisor are dead. No auto-revive, no dispatch: fleet-down.
echo ""
echo "=== Test 17: fleet-down (daemon + supervisor both down) — immediate ==="
PROJ=$(setup_fixture \
  --hk-queue-status-exit 17 \
  --hk-queue-status-json '' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --hk-supervise-status-json '{"schema_version":1,"running":false,"status":"stopped"}' \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "fleet-down stdout IMMEDIATE"  "IMMEDIATE"  "$OUTPUT"
assert_contains "fleet-down stdout signal"     "fleet-down" "$OUTPUT"
assert_json_list_contains "immediate_signals has fleet-down" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals" "fleet-down"
assert_json_bool "latest.json daemon_up=false"     "$PROJ/.harmonik/ops-monitor/latest.json" "daemon_up" "false"
assert_json_bool "latest.json supervisor_up=false" "$PROJ/.harmonik/ops-monitor/latest.json" "supervisor_up" "false"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "fleet-down: comms sent"
  assert_contains "fleet-down comms [IMMEDIATE]" "[IMMEDIATE]"  "$(cat "$LOG")"
  assert_contains "fleet-down comms signal"      "fleet-down"   "$(cat "$LOG")"
else
  fail "fleet-down: expected comms send, got none"
fi
rm -rf "$PROJ"

# ── Test 18: critical signal re-alerts after 5 min (CRITICAL_IMMEDIATE_COOLDOWN) ─
# daemon-down alerted 6 min ago as a bare-int (old format) → backward-compat:
# normalized to count=1, re-alert increments to count=2 → tier-2 → [ESCALATION].
# Verifies: cooldown works AND escalation tier advances correctly from a bare-int state.
echo ""
echo "=== Test 18: daemon-down re-alerts after 5 min (critical cooldown, tier-2) ==="
EPOCH_6M_AGO=$(python3 -c "import time; print(int(time.time()) - 360)")
STATE_18='{"stale_crew_misses":{},"last_digest_ts":0,"alerted_immediate":{"daemon-down":'"$EPOCH_6M_AGO"'}}'
PROJ=$(setup_fixture \
  --hk-queue-status-exit 17 \
  --hk-comms-who-json '' \
  --state-json "$STATE_18" \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "critical re-alert: stdout IMMEDIATE" "IMMEDIATE"   "$OUTPUT"
assert_contains "critical re-alert: stdout daemon-down" "daemon-down" "$OUTPUT"
LATEST="$PROJ/.harmonik/ops-monitor/latest.json"
SEND_SIGS=$(python3 -c "import json; d=json.load(open('$LATEST')); print(json.dumps(d.get('send_immediate_signals', [])))" 2>/dev/null || echo "[]")
if echo "$SEND_SIGS" | grep -qF "daemon-down"; then
  pass "critical re-alert: daemon-down in send_immediate_signals"
else
  fail "critical re-alert: daemon-down missing from send_immediate_signals (got $SEND_SIGS)"
fi
# After re-alert count = 2 → tier 2 → [ESCALATION]
TIER=$(python3 -c "
import json
d = json.load(open('$LATEST'))
print(d.get('escalations', {}).get('daemon-down', {}).get('tier', 'missing'))
" 2>/dev/null || echo "missing")
assert_eq "critical re-alert: escalation tier=2 (count=2)" "2" "$TIER"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "critical re-alert: comms sent"
  assert_contains "critical re-alert: comms [ESCALATION]" "[ESCALATION]" "$(cat "$LOG")"
  assert_contains "critical re-alert: comms daemon-down"  "daemon-down"  "$(cat "$LOG")"
  assert_not_contains "critical re-alert: no ops-CRITICAL yet (count=2)" "topic=ops-CRITICAL" "$(cat "$LOG")"
else
  fail "critical re-alert: expected comms send, got none"
fi
rm -rf "$PROJ"

# ── Test 19: non-critical signal suppressed at 5 min (uses 30 min cooldown) ──
# paused-queue alerted 6 min ago → 6 min < 30 min global cd → MUST be suppressed.
echo ""
echo "=== Test 19: paused-queue suppressed at 6 min (non-critical 30 min cooldown) ==="
CREW_TS=$(ts_ago 10)
COMMS_WHO_19='{"agent":"myagent","status":"online","last_seen":"'"$CREW_TS"'"}'
QLIST_19='{"queues":[{"name":"myagent-q","status":"paused-by-failure","workers":0,"pending_items":0,"failed_items":1}],"max_concurrent":4}'
EPOCH_6M_AGO=$(python3 -c "import time; print(int(time.time()) - 360)")
# paused-queue signal key includes the queue name
STATE_19='{"stale_crew_misses":{},"last_digest_ts":0,"alerted_immediate":{"paused-queue:myagent-q":'"$EPOCH_6M_AGO"'}}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json "$QLIST_19" \
  --hk-comms-who-json "$COMMS_WHO_19" \
  --state-json "$STATE_19" \
)
OUTPUT=$(run_check "$PROJ")
LATEST="$PROJ/.harmonik/ops-monitor/latest.json"
SEND_SIGS=$(python3 -c "import json; d=json.load(open('$LATEST')); print(json.dumps(d.get('send_immediate_signals', [])))" 2>/dev/null || echo "[]")
if [[ "$SEND_SIGS" == "[]" ]]; then
  pass "non-critical suppressed: send_immediate_signals empty (still within 30 min cd)"
else
  fail "non-critical suppressed: expected empty send_immediate_signals, got $SEND_SIGS"
fi
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  fail "non-critical suppressed: no comms should be sent within 30 min cooldown"
else
  pass "non-critical suppressed: no comms sent (cooldown active)"
fi
rm -rf "$PROJ"

# ── Test 20: escalation tier-1 — first alert: count=1, [IMMEDIATE], no ops-CRITICAL ─
echo ""
echo "=== Test 20: escalation tier-1 (fresh daemon-down, count=1, [IMMEDIATE]) ==="
PROJ=$(setup_fixture --hk-queue-status-exit 17 --hk-comms-who-json '')
run_check "$PROJ" > /dev/null 2>&1
LATEST="$PROJ/.harmonik/ops-monitor/latest.json"
COUNT=$(python3 -c "
import json
d = json.load(open('$PROJ/.harmonik/ops-monitor/state.json'))
e = d.get('alerted_immediate', {}).get('daemon-down')
print(e.get('count', 0) if isinstance(e, dict) else 'bare-int')
")
assert_eq "tier-1: count=1 in state" "1" "$COUNT"
TIER=$(python3 -c "
import json; d=json.load(open('$LATEST'))
print(d.get('escalations', {}).get('daemon-down', {}).get('tier', 'missing'))
")
assert_eq "tier-1: escalations.daemon-down.tier=1" "1" "$TIER"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  assert_contains     "tier-1: [IMMEDIATE] in comms"     "[IMMEDIATE]"   "$(cat "$LOG")"
  assert_not_contains "tier-1: no [ESCALATION]"          "[ESCALATION]"  "$(cat "$LOG")"
  assert_not_contains "tier-1: no ops-CRITICAL"          "topic=ops-CRITICAL" "$(cat "$LOG")"
else
  fail "tier-1: expected comms send, got none"
fi
rm -rf "$PROJ"

# ── Test 21: escalation tier-2 — count=2, [ESCALATION], no ops-CRITICAL ─────
echo ""
echo "=== Test 21: escalation tier-2 (count=1→2, [ESCALATION], no ops-CRITICAL) ==="
EPOCH_6M_AGO=$(python3 -c "import time; print(int(time.time()) - 360)")
STATE_21=$(python3 -c "
import json, time
now = int(time.time())
entry = {'first_ts': now - 360, 'last_ts': now - 360, 'count': 1}
print(json.dumps({'stale_crew_misses': {}, 'last_digest_ts': 0,
                  'alerted_immediate': {'daemon-down': entry}}))
")
PROJ=$(setup_fixture --hk-queue-status-exit 17 --hk-comms-who-json '' --state-json "$STATE_21")
run_check "$PROJ" > /dev/null 2>&1
LATEST="$PROJ/.harmonik/ops-monitor/latest.json"
COUNT=$(python3 -c "
import json
d = json.load(open('$PROJ/.harmonik/ops-monitor/state.json'))
e = d.get('alerted_immediate', {}).get('daemon-down')
print(e.get('count', 0) if isinstance(e, dict) else 'bare-int')
")
assert_eq "tier-2: count=2 in state" "2" "$COUNT"
TIER=$(python3 -c "
import json; d=json.load(open('$LATEST'))
print(d.get('escalations', {}).get('daemon-down', {}).get('tier', 'missing'))
")
assert_eq "tier-2: escalations.daemon-down.tier=2" "2" "$TIER"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  assert_contains     "tier-2: [ESCALATION] in comms"    "[ESCALATION]"       "$(cat "$LOG")"
  assert_contains     "tier-2: alert #2 in body"         "alert #2"           "$(cat "$LOG")"
  assert_not_contains "tier-2: no ops-CRITICAL"          "topic=ops-CRITICAL" "$(cat "$LOG")"
else
  fail "tier-2: expected comms send, got none"
fi
rm -rf "$PROJ"

# ── Test 22: escalation tier-3 by count — count>=6 sends ops-CRITICAL ────────
echo ""
echo "=== Test 22: escalation tier-3 by count (count=5→6, ops-CRITICAL sent) ==="
STATE_22=$(python3 -c "
import json, time
now = int(time.time())
entry = {'first_ts': now - 1500, 'last_ts': now - 360, 'count': 5}
print(json.dumps({'stale_crew_misses': {}, 'last_digest_ts': 0,
                  'alerted_immediate': {'daemon-down': entry}}))
")
PROJ=$(setup_fixture --hk-queue-status-exit 17 --hk-comms-who-json '' --state-json "$STATE_22")
run_check "$PROJ" > /dev/null 2>&1
LATEST="$PROJ/.harmonik/ops-monitor/latest.json"
COUNT=$(python3 -c "
import json
d = json.load(open('$PROJ/.harmonik/ops-monitor/state.json'))
e = d.get('alerted_immediate', {}).get('daemon-down')
print(e.get('count', 0) if isinstance(e, dict) else 'bare-int')
")
assert_eq "tier-3-count: count=6 in state" "6" "$COUNT"
TIER=$(python3 -c "
import json; d=json.load(open('$LATEST'))
print(d.get('escalations', {}).get('daemon-down', {}).get('tier', 'missing'))
")
assert_eq "tier-3-count: escalations.daemon-down.tier=3" "3" "$TIER"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  assert_contains "tier-3-count: [ESCALATION] in ops-monitor message" "[ESCALATION]"       "$(cat "$LOG")"
  assert_contains "tier-3-count: ops-CRITICAL topic sent"             "topic=ops-CRITICAL" "$(cat "$LOG")"
  assert_contains "tier-3-count: [ops-CRITICAL] in body"             "[ops-CRITICAL]"     "$(cat "$LOG")"
else
  fail "tier-3-count: expected comms send, got none"
fi
rm -rf "$PROJ"

# ── Test 23: escalation tier-3 by elapsed — elapsed>=1800s sends ops-CRITICAL ─
echo ""
echo "=== Test 23: escalation tier-3 by elapsed (elapsed>1800s, ops-CRITICAL sent) ==="
STATE_23=$(python3 -c "
import json, time
now = int(time.time())
entry = {'first_ts': now - 1900, 'last_ts': now - 360, 'count': 3}
print(json.dumps({'stale_crew_misses': {}, 'last_digest_ts': 0,
                  'alerted_immediate': {'daemon-down': entry}}))
")
PROJ=$(setup_fixture --hk-queue-status-exit 17 --hk-comms-who-json '' --state-json "$STATE_23")
run_check "$PROJ" > /dev/null 2>&1
LATEST="$PROJ/.harmonik/ops-monitor/latest.json"
TIER=$(python3 -c "
import json; d=json.load(open('$LATEST'))
print(d.get('escalations', {}).get('daemon-down', {}).get('tier', 'missing'))
")
assert_eq "tier-3-elapsed: escalations.daemon-down.tier=3" "3" "$TIER"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  assert_contains "tier-3-elapsed: ops-CRITICAL topic sent" "topic=ops-CRITICAL" "$(cat "$LOG")"
else
  fail "tier-3-elapsed: expected ops-CRITICAL comms, got none"
fi
rm -rf "$PROJ"

# ── Test 24: resolved critical signal — state cleared, no comms ───────────────
echo ""
echo "=== Test 24: resolved critical signal — state cleared, no comms ==="
STATE_24=$(python3 -c "
import json, time
now = int(time.time())
entry = {'first_ts': now - 600, 'last_ts': now - 360, 'count': 3}
print(json.dumps({'stale_crew_misses': {}, 'last_digest_ts': 0,
                  'alerted_immediate': {'daemon-down': entry}}))
")
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --state-json "$STATE_24" \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "resolved: all-green" "all-green" "$OUTPUT"
ENTRY=$(python3 -c "
import json
d = json.load(open('$PROJ/.harmonik/ops-monitor/state.json'))
e = d.get('alerted_immediate', {}).get('daemon-down')
print('present' if e is not None else 'cleared')
")
assert_eq "resolved: daemon-down entry cleared from state" "cleared" "$ENTRY"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  fail "resolved: no comms should be sent after resolution"
else
  pass "resolved: no comms sent after resolution"
fi
rm -rf "$PROJ"

# ── Test 25a: captain absent from comms-who, tmux alive → no captain-down ────
# The "live false-negative": comms presence ages out while the captain is still
# attached to its tmux session. tmux probe confirms alive → must NOT alert.
echo ""
echo "=== Test 25a: captain absent from comms-who, tmux session alive → no captain-down ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --tmux-captain-alive true \
)
OUTPUT=$(run_check "$PROJ")
assert_not_contains "25a: no captain-down in stdout" "captain-down" "$OUTPUT"
assert_json_list_empty "25a: immediate_signals empty" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals"
assert_check_state "25a: checks.captain-up=ok (tmux alive)" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "captain-up" "ok"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  fail "25a: no comms should be sent when captain tmux is alive"
else
  pass "25a: no comms sent (captain alive via tmux)"
fi
rm -rf "$PROJ"

# ── Test 25b: captain absent from comms-who AND no tmux session → captain-down ─
# Both indicators absent: alert captain-down on the 5-min critical cooldown.
echo ""
echo "=== Test 25b: captain absent from comms-who AND no tmux session → captain-down ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --tmux-captain-alive false \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "25b: captain-down stdout IMMEDIATE" "IMMEDIATE"    "$OUTPUT"
assert_contains "25b: captain-down in stdout"        "captain-down" "$OUTPUT"
assert_json_list_contains "25b: immediate_signals has captain-down" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals" "captain-down"
assert_check_state "25b: checks.captain-up=flag" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "captain-up" "flag"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "25b: comms sent for captain-down"
  assert_contains "25b: comms [IMMEDIATE]"    "[IMMEDIATE]"    "$(cat "$LOG")"
  assert_contains "25b: comms captain-down"   "captain-down"   "$(cat "$LOG")"
else
  fail "25b: expected comms send for captain-down, got none"
fi
rm -rf "$PROJ"

# ── Test 25c: stale captain in comms-who is NOT emitted as crew-stale ─────────
# captain is in comms-who but aged past stale_thresh (150s) yet within
# captain_absent_thresh (600s). With NON_CREW exclusion the crew-stale loop
# must not fire for captain. tmux alive → captain_down = False.
echo ""
echo "=== Test 25c: stale captain in comms-who NOT emitted as crew-stale (NON_CREW exclusion) ==="
STALE_CAPTAIN_TS=$(ts_ago 300)   # 300s > stale_thresh(150s) but < captain_absent_thresh(600s)
CW_WITH_CAPTAIN='{"agent":"captain","status":"online","last_seen":"'"$STALE_CAPTAIN_TS"'"}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json "$CW_WITH_CAPTAIN" \
  --tmux-captain-alive true \
)
# Run twice to ensure misses don't accumulate for captain
run_check "$PROJ" > /dev/null 2>&1
OUTPUT=$(run_check "$PROJ")
assert_not_contains "25c: no crew-stale for captain"    "crew-stale"   "$OUTPUT"
assert_not_contains "25c: no captain-down (tmux alive)" "captain-down" "$OUTPUT"
assert_contains     "25c: all-green"                    "all-green"    "$OUTPUT"
assert_check_state "25c: checks.captain-up=ok (within absent-thresh)" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "captain-up" "ok"
CAPTAIN_MISSES=$(python3 -c "
import json
d = json.load(open('$PROJ/.harmonik/ops-monitor/state.json'))
print(d.get('stale_crew_misses', {}).get('captain', 0))
")
assert_eq "25c: captain not in stale_crew_misses" "0" "$CAPTAIN_MISSES"
rm -rf "$PROJ"

# ── Test 26a: keeper-covered — online crew has matching keeper → no signal ────
echo ""
echo "=== Test 26a: keeper-covered (crew has keeper) — no keeper-missing signal ==="
CREW_TS=$(ts_ago 10)
CW_26A='{"agent":"paul","status":"online","last_seen":"'"$CREW_TS"'"}'
KEEPER_LINE_26A='harmonik keeper --agent paul --tmux harmonik-xxx-crew-paul:agent --warn-abs-tokens 200000 --act-abs-tokens 215000'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json "$CW_26A" \
  --keeper-procs-raw "$KEEPER_LINE_26A" \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "26a: all-green" "all-green" "$OUTPUT"
assert_not_contains "26a: no keeper-missing in stdout" "keeper-missing" "$OUTPUT"
assert_check_state "26a: checks.keepers-covered=ok" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "keepers-covered" "ok"
assert_json_list_empty "26a: no immediate_signals" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals"
rm -rf "$PROJ"

# ── Test 26b: keeper-missing debounce — 1st miss silent; >=miss_limit → immediate ─
echo ""
echo "=== Test 26b: keeper-missing debounce (1st miss silent, 2nd miss → immediate) ==="
CREW_TS=$(ts_ago 10)
CW_26B='{"agent":"paul","status":"online","last_seen":"'"$CREW_TS"'"}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json "$CW_26B" \
  --keeper-procs-raw '' \
)

# Run 1: first miss — must NOT signal yet (debounce, miss_count=1 < miss_limit=2)
run_check "$PROJ" > /dev/null 2>&1
K_MISSES=$(python3 -c "
import json
d = json.load(open('$PROJ/.harmonik/ops-monitor/state.json'))
print(d.get('keeper_coverage_misses', {}).get('paul', 0))
")
assert_eq "26b: keeper miss count after run 1 = 1" "1" "$K_MISSES"
LOG_26B=$(comms_log "$PROJ")
if [[ -f "$LOG_26B" && -s "$LOG_26B" ]]; then
  if grep -qF "keeper-missing" "$LOG_26B"; then
    fail "26b: run1 should NOT signal keeper-missing on first miss"
  else
    pass "26b: run1 no keeper-missing comms (other signal may exist)"
  fi
else
  pass "26b: run1 no comms sent"
fi

# Run 2: second miss — must emit immediate keeper-missing:paul
OUTPUT_26B=$(run_check "$PROJ")
K_MISSES2=$(python3 -c "
import json
d = json.load(open('$PROJ/.harmonik/ops-monitor/state.json'))
print(d.get('keeper_coverage_misses', {}).get('paul', 0))
")
assert_eq "26b: keeper miss count after run 2 = 2" "2" "$K_MISSES2"
assert_contains "26b: run2 IMMEDIATE in stdout"       "IMMEDIATE"      "$OUTPUT_26B"
assert_contains "26b: run2 keeper-missing in stdout"  "keeper-missing" "$OUTPUT_26B"
assert_json_list_contains "26b: immediate_signals has keeper-missing" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals" "keeper-missing"
assert_check_state "26b: checks.keepers-covered=flag" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "keepers-covered" "flag"
if [[ -f "$LOG_26B" && -s "$LOG_26B" ]]; then
  pass "26b: run2 comms sent"
  assert_contains "26b: comms [IMMEDIATE]"      "[IMMEDIATE]"     "$(cat "$LOG_26B")"
  assert_contains "26b: comms keeper-missing"   "keeper-missing"  "$(cat "$LOG_26B")"
  assert_contains "26b: comms names paul"       "paul"            "$(cat "$LOG_26B")"
else
  fail "26b: expected comms send on second miss, got none"
fi
rm -rf "$PROJ"

# ── Test 26c: NON_CREW exclusion — captain never produces keeper-missing ──────
echo ""
echo "=== Test 26c: NON_CREW exclusion (captain online, no keeper) — no keeper-missing ==="
STALE_CAPT=$(ts_ago 10)
CW_26C='{"agent":"captain","status":"online","last_seen":"'"$STALE_CAPT"'"}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json "$CW_26C" \
  --keeper-procs-raw '' \
  --tmux-captain-alive true \
)
# Run twice to ensure misses don't accumulate for NON_CREW agents
run_check "$PROJ" > /dev/null 2>&1
OUTPUT_26C=$(run_check "$PROJ")
assert_not_contains "26c: no keeper-missing for captain" "keeper-missing" "$OUTPUT_26C"
CAP_KMISS=$(python3 -c "
import json
d = json.load(open('$PROJ/.harmonik/ops-monitor/state.json'))
print(d.get('keeper_coverage_misses', {}).get('captain', 0))
")
assert_eq "26c: captain not in keeper_coverage_misses" "0" "$CAP_KMISS"
assert_check_state "26c: checks.keepers-covered=ok (no crew)" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "keepers-covered" "ok"
rm -rf "$PROJ"

# ── Test 26d: keeper-missing clears — keeper reappears → signal gone + state reset ─
echo ""
echo "=== Test 26d: keeper-missing clears (keeper reappears → signal gone, state reset) ==="
CREW_TS=$(ts_ago 10)
CW_26D='{"agent":"jamis","status":"online","last_seen":"'"$CREW_TS"'"}'
# Pre-seed state with miss_count=2 (already at signal threshold)
STATE_26D=$(python3 -c "
import json
print(json.dumps({'stale_crew_misses': {}, 'keeper_coverage_misses': {'jamis': 2},
                  'last_digest_ts': 0, 'alerted_immediate': {}}))
")
KEEPER_LINE_26D='harmonik keeper --agent jamis --tmux harmonik-xxx-crew-jamis:agent --warn-abs-tokens 200000 --act-abs-tokens 215000'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json "$CW_26D" \
  --keeper-procs-raw "$KEEPER_LINE_26D" \
  --state-json "$STATE_26D" \
)
OUTPUT_26D=$(run_check "$PROJ")
assert_not_contains "26d: no keeper-missing after keeper reappears" "keeper-missing" "$OUTPUT_26D"
assert_check_state "26d: checks.keepers-covered=ok after recovery" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "keepers-covered" "ok"
JAMIS_KMISS=$(python3 -c "
import json
d = json.load(open('$PROJ/.harmonik/ops-monitor/state.json'))
print(d.get('keeper_coverage_misses', {}).get('jamis', 'missing'))
")
assert_eq "26d: jamis keeper_coverage_misses reset to 0" "0" "$JAMIS_KMISS"
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
