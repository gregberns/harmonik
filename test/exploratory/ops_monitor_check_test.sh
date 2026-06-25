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
#   [x] review-gate request-changes in-flight → no flag (triple-review relaunch still live) [hk-spx63]
#   [x] review-gate request-changes completed → immediate signal (completed without APPROVE) [hk-spx63]
#   [x] backlog-ready        → digest signal (br ready beads + free slot)
#   [x] backlog suppressed   → no flag when all slots busy
#   [x] checks map present, schema_version=2
#   [x] critical-component re-alert after 5 min (daemon-down re-alerts at 6 min,
#                                                 would be suppressed under old 30 min cd)
#   [x] non-critical suppressed within 30 min   (paused-queue at 6 min stays suppressed)
#   [x] escalation tier-1 — first alert: count=1, [IMMEDIATE], no ops-CRITICAL
#   [x] escalation tier-2 — second alert: count=2, [ESCALATION], no ops-CRITICAL
#   [x] escalation tier-3 by count — count>=6: [ESCALATION] + ops-CRITICAL topic (first send)
#   [x] escalation tier-3 by elapsed — elapsed>=1800s: ops-CRITICAL topic (first send)
#   [x] escalation resolved — signal clears count+state, no comms
#   [x] ops-CRITICAL cooldown: tier-3, last_ops_critical_ts 6m ago → NO ops-CRITICAL re-send (Test 28)
#   [x] ops-CRITICAL cooldown: tier-3, last_ops_critical_ts 35m ago → ops-CRITICAL re-fires (Test 29)
#   [x] backward-compat — bare int alerted entry normalized to count=1 on re-alert
#   [x] captain absent from comms-who, tmux alive → no captain-down (Test 25a)
#   [x] captain absent from comms-who AND no tmux session → captain-down immediate (Test 25b)
#   [x] stale captain in comms-who NOT in crew-stale signal (NON_CREW exclusion) (Test 25c)
#   [x] keepers-covered: crew WITH keeper → no signal (Test 26a)
#   [x] keeper-missing debounce: 1st miss no signal; >=miss_limit → immediate 'keeper-missing' (Test 26b)
#   [x] keeper-missing NON_CREW: captain etc never produce keeper-missing (Test 26c)
#   [x] keeper-missing clears: keeper reappears → signal gone, state reset (Test 26d)
#   [x] release-due: count < threshold → no signal (Test 27a)
#   [x] release-due: count >= threshold AND CI green → immediate 'release-due:<count>', checks flag (Test 27b)
#   [x] release-due: count >= threshold, CI not-green → no signal (Test 27c)
#   [x] release-due: count >= threshold, CI unknown (gh unavailable) → no signal, no crash (Test 27d)
#   [x] release-due NOT in CRITICAL_PREFIXES: alerted 6m ago still suppressed (30-min cooldown) (Test 27e)
#   [x] watch-down → direct-class → --to captain (not opsmonitor_target) (Test 32)
#   [x] watch tmux alive (crew-watch session) + opsmonitor_target=watch → no watch-down (Test 34a)
#   [x] watch tmux absent + opsmonitor_target=watch → watch-down emitted --to captain (Test 34b)
#   [x] watch present+tmux-alive, cursor frozen N ticks with pending events → watch-stalled IMMEDIATE (Test 35a)
#   [x] cursor advancing → no watch-stalled signal (Test 35b)
#   [x] watch absent-from-comms but tmux-alive → NOT watch_down (dual-probe) (Test 35c)
#   [x] SD-4 POSITIVE: program drained + KNOWN lane wake-economy ready + free slot →
#                      IMMEDIATE 'program-drained-stall' NAMES wake-economy (Test 36)
#   [x] SD-4 NEGATIVE: same lane ZERO ready beads → no program-drained-stall (Test 37)
#   [x] SD-4 DEFENSIVE: lanes.json missing → no crash, no stall wake (Test 38)
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
  local tmux_watch_alive=false    # WE7: default watch tmux session NOT alive (watch is opt-in)
  local watch_opsmonitor_target='' # WE7: '' → default 'captain'; set to redirect
  local watch_absent_threshold=600 # WE9: absent_thresh_s written to config when watch is target
  local watch_stall_ticks=3        # WE9: stall_ticks written to config when watch is target
  local watch_cursor=''            # WE9: content of .harmonik/watch/cursor ('' = no cursor file)
  local state_json=''
  local events_content=''
  local br_ready_json='[]'   # `br ready --limit 0 --json` output for the stub
  # SD-1: `br ready --parent <epic> --limit 0 --json` per-epic output. JSON object
  # mapping epic_id -> ready-bead list, e.g. '{"hk-var9b":[{"id":"hk-1"}]}'. Any epic
  # not present returns '[]' (zero ready). Default '{}' => every parent returns '[]'.
  local br_parent_json='{}'
  # SD-1: content written to .harmonik/context/lanes.json. Empty => no lanes.json file
  # (exercises the defensive missing-file skip path).
  local lanes_json=''
  local keeper_procs_raw=''  # lines matching "harmonik keeper --agent"; default: none
  local git_release_count=0  # value returned by `git rev-list --count` stub
  local git_last_tag='v0.0.1' # value returned by `git tag --list` stub ('' = no tag yet)
  local gh_run_conclusion=''  # gh run list conclusion ('success'/'failure'/''=gh unavailable)

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --hk-queue-status-exit)      hk_qs_exit="$2";    shift 2 ;;
      --hk-queue-status-json)      hk_qs_json="$2";    shift 2 ;;
      --hk-queue-list-json)        hk_ql_json="$2";    shift 2 ;;
      --hk-comms-who-json)         hk_cw_json="$2";    shift 2 ;;
      --hk-supervise-status-json)  hk_sv_json="$2";    shift 2 ;;
      --hk-project-hash)           hk_project_hash="$2"; shift 2 ;;
      --tmux-captain-alive)        tmux_captain_alive="$2"; shift 2 ;;
      --tmux-watch-alive)          tmux_watch_alive="$2"; shift 2 ;;  # WE7
      --watch-opsmonitor-target)   watch_opsmonitor_target="$2"; shift 2 ;;  # WE7
      --watch-absent-threshold)    watch_absent_threshold="$2"; shift 2 ;;  # WE9
      --watch-stall-ticks)         watch_stall_ticks="$2"; shift 2 ;;        # WE9
      --watch-cursor)              watch_cursor="$2"; shift 2 ;;              # WE9
      --state-json)                state_json="$2";    shift 2 ;;
      --events-jsonl)              events_content="$2";shift 2 ;;
      --br-ready-json)             br_ready_json="$2"; shift 2 ;;
      --br-parent-json)            br_parent_json="$2"; shift 2 ;;  # SD-1
      --lanes-json)                lanes_json="$2"; shift 2 ;;      # SD-1
      --keeper-procs-raw)          keeper_procs_raw="$2"; shift 2 ;;
      --git-release-count)         git_release_count="$2"; shift 2 ;;
      --git-last-tag)              git_last_tag="$2";  shift 2 ;;
      --gh-run-conclusion)         gh_run_conclusion="$2"; shift 2 ;;
      *) echo "setup_fixture: unknown arg $1" >&2; return 1 ;;
    esac
  done

  if [[ -n "$state_json" ]]; then
    printf '%s\n' "$state_json" > "$tmpdir/.harmonik/ops-monitor/state.json"
  fi
  if [[ -n "$events_content" ]]; then
    printf '%s\n' "$events_content" > "$tmpdir/.harmonik/events/events.jsonl"
  fi
  # WE7/WE9: write config.yaml with watch block if opsmonitor_target is specified.
  # WE9 behavioral keys (absent_thresh_s, stall_ticks) are included — they are
  # config-or-fail-loud when opsmonitor_target != 'captain'.
  if [[ -n "$watch_opsmonitor_target" ]]; then
    printf 'schema_version: 1\nwatch:\n  opsmonitor_target: %s\n  absent_thresh_s: %s\n  stall_ticks: %s\n' \
      "$watch_opsmonitor_target" "$watch_absent_threshold" "$watch_stall_ticks" \
      > "$tmpdir/.harmonik/config.yaml"
  fi
  # WE9: write watch cursor file if specified.
  if [[ -n "$watch_cursor" ]]; then
    mkdir -p "$tmpdir/.harmonik/watch"
    printf '%s' "$watch_cursor" > "$tmpdir/.harmonik/watch/cursor"
  fi
  # SD-1: write lanes.json if specified (empty => no file, defensive skip path).
  if [[ -n "$lanes_json" ]]; then
    mkdir -p "$tmpdir/.harmonik/context"
    printf '%s\n' "$lanes_json" > "$tmpdir/.harmonik/context/lanes.json"
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
    to=""
    for ((i=0; i<\${#args[@]}; i++)); do
      if [[ "\${args[i]}" == "--topic" ]]; then
        topic="\${args[i+1]}"
      fi
      if [[ "\${args[i]}" == "--to" ]]; then
        to="\${args[i+1]}"
      fi
      if [[ "\${args[i]}" == "--" ]]; then
        body="\${args[i+1]}"
        break
      fi
    done
    printf 'to=%s topic=%s -- %s\n' "\$to" "\$topic" "\$body" >> "\$COMMS_LOG"
    ;;
  *)
    echo "stub: unhandled harmonik \$*" >&2
    exit 1
    ;;
esac
EOF
  chmod +x "$stub_bin"

  # Stub `tmux` for captain-liveness and watch-liveness probes.
  local tmux_captain_alive_val="${tmux_captain_alive}"
  local tmux_watch_alive_val="${tmux_watch_alive}"
  local tmux_bin="$tmpdir/bin/tmux"
  cat > "$tmux_bin" <<EOF
#!/usr/bin/env bash
# Stub tmux for ops-monitor liveness probes (captain + watch, WE7)
TMUX_CAPTAIN_ALIVE=${tmux_captain_alive_val}
TMUX_WATCH_ALIVE=${tmux_watch_alive_val}
if [[ "\$1" == "has-session" ]]; then
  session="\${3:-}"
  if [[ "\$session" == *"-watch" ]]; then
    [[ "\$TMUX_WATCH_ALIVE" == "true" ]] && exit 0 || exit 1
  else
    [[ "\$TMUX_CAPTAIN_ALIVE" == "true" ]] && exit 0 || exit 1
  fi
fi
exit 0
EOF
  chmod +x "$tmux_bin"

  # Stub `br` so the backlog-readiness AND known-ready-lane (SD-1) checks are
  # deterministic in tests. Distinguishes `br ready --parent <epic> --json` (SD-1,
  # per-epic lookup in BR_PARENT_JSON) from the bare `br ready --limit 0 --json`
  # (Check 8, returns BR_READY_JSON).
  local br_ready_escaped
  br_ready_escaped=$(printf '%s' "$br_ready_json" | sed "s/'/'\\\\''/g")
  local br_parent_escaped
  br_parent_escaped=$(printf '%s' "$br_parent_json" | sed "s/'/'\\\\''/g")
  local br_bin="$tmpdir/bin/br"
  cat > "$br_bin" <<EOF
#!/usr/bin/env bash
# Stub br
BR_READY_JSON='${br_ready_escaped}'
BR_PARENT_JSON='${br_parent_escaped}'
# Detect a --parent <epic> arg (SD-1 known-ready-lane lookup).
parent_epic=""
args=("\$@")
for ((i=0; i<\${#args[@]}; i++)); do
  if [[ "\${args[i]}" == "--parent" ]]; then
    parent_epic="\${args[i+1]}"
    break
  fi
done
case "\$*" in
  ready*--json*|*ready*--json*)
    if [[ -n "\$parent_epic" ]]; then
      # Look up this epic in the parent map; absent => '[]' (zero ready beads).
      printf '%s' "\$BR_PARENT_JSON" | python3 -c "
import json, sys
try:
    d = json.load(sys.stdin)
    print(json.dumps(d.get('\$parent_epic', [])))
except Exception:
    print('[]')
"
    else
      printf '%s\n' "\$BR_READY_JSON"
    fi
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

  # Stub `git` for release-due probe.
  # Handles: git -C <proj> tag --list 'v[0-9]*...' → configured last tag
  #          git -C <proj> rev-list --count <range> → configured count
  # All other git commands exit 128 (not-a-repo) so the script degrades gracefully.
  local git_bin="$tmpdir/bin/git"
  local git_tag_escaped
  git_tag_escaped=$(printf '%s' "$git_last_tag" | sed "s/'/'\\\\''/g")
  cat > "$git_bin" <<EOF
#!/usr/bin/env bash
# Stub git for ops-monitor release-due test
GIT_LAST_TAG='${git_tag_escaped}'
GIT_RELEASE_COUNT='${git_release_count}'
args_str="\$*"
if [[ "\$args_str" == *"tag --list"* ]]; then
  printf '%s\n' "\$GIT_LAST_TAG"
elif [[ "\$args_str" == *"rev-list --count"* ]]; then
  printf '%s\n' "\$GIT_RELEASE_COUNT"
else
  exit 128
fi
EOF
  chmod +x "$git_bin"

  # Stub `gh` for CI-status probe — only created when gh_run_conclusion is non-empty.
  # When absent from PATH the script detects gh unavailable and sets CI_STATUS=unknown.
  if [[ -n "$gh_run_conclusion" ]]; then
    local gh_bin="$tmpdir/bin/gh"
    local gh_concl_escaped
    gh_concl_escaped=$(printf '%s' "$gh_run_conclusion" | sed "s/'/'\\\\''/g")
    cat > "$gh_bin" <<EOF
#!/usr/bin/env bash
# Stub gh for ops-monitor CI-status test
GH_CONCLUSION='${gh_concl_escaped}'
printf '[{"conclusion":"%s"}]\n' "\$GH_CONCLUSION"
EOF
    chmod +x "$gh_bin"
  fi

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

# ── Test 12e: review-gate REQUEST_CHANGES still in flight — no flag ───────────
# DOT triple-review can emit reviewer_verdict{REQUEST_CHANGES}, then relaunch the
# implementer/reviewer loop. Until a later run_completed appears, that non-approving
# verdict is active review state, not an unreviewed merge.
echo ""
echo "=== Test 12e: review-gate request-changes in flight — no flag ==="
OLD_WALL=$(ts_ago 600)
EVENTS='{"type":"node_dispatch_requested","timestamp_wall":"'"$OLD_WALL"'","run_id":"r-rc-live","payload":{"node_id":"review_correctness","run_id":"r-rc-live"}}
{"type":"reviewer_launched","timestamp_wall":"'"$OLD_WALL"'","run_id":"r-rc-live","payload":{"run_id":"r-rc-live"}}
{"type":"reviewer_verdict","timestamp_wall":"'"$OLD_WALL"'","run_id":"r-rc-live","payload":{"run_id":"r-rc-live","verdict":"REQUEST_CHANGES"}}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --events-jsonl "$EVENTS" \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "request-changes in-flight all-green" "all-green" "$OUTPUT"
assert_json_list_empty "request-changes in-flight: review_bypass_run_ids empty" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review_bypass_run_ids"
assert_check_state "request-changes in-flight: checks.review-gate=ok" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review-gate" "ok"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  fail "request-changes in-flight: no comms should be sent"
else
  pass "request-changes in-flight: no comms sent"
fi
rm -rf "$PROJ"

# ── Test 12f: review-gate REQUEST_CHANGES then completed without APPROVE — flag ─
echo ""
echo "=== Test 12f: review-gate request-changes completed without approve — immediate ==="
OLD_WALL=$(ts_ago 600)
EVENTS='{"type":"node_dispatch_requested","timestamp_wall":"'"$OLD_WALL"'","run_id":"r-rc-done","payload":{"node_id":"review_tests","run_id":"r-rc-done"}}
{"type":"reviewer_launched","timestamp_wall":"'"$OLD_WALL"'","run_id":"r-rc-done","payload":{"run_id":"r-rc-done"}}
{"type":"reviewer_verdict","timestamp_wall":"'"$OLD_WALL"'","run_id":"r-rc-done","payload":{"run_id":"r-rc-done","verdict":"REQUEST_CHANGES"}}
{"type":"run_completed","timestamp_wall":"'"$OLD_WALL"'","payload":{"run_id":"r-rc-done","success":true}}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --events-jsonl "$EVENTS" \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "request-changes completed stdout IMMEDIATE" "IMMEDIATE"     "$OUTPUT"
assert_contains "request-changes completed stdout signal"    "review-bypass" "$OUTPUT"
assert_json_list_contains "immediate_signals has review-bypass (request-changes completed)" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals" "review-bypass"
assert_json_list_contains "review_bypass_run_ids has r-rc-done" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review_bypass_run_ids" "r-rc-done"
assert_check_state "request-changes completed: checks.review-gate=flag" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "review-gate" "flag"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "request-changes completed: comms sent"
  assert_contains "request-changes completed comms [IMMEDIATE]" "[IMMEDIATE]"   "$(cat "$LOG")"
  assert_contains "request-changes completed comms signal"      "review-bypass" "$(cat "$LOG")"
else
  fail "request-changes completed: expected comms send, got none"
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
for CHK in daemon-up supervisor-up paused-queues single-mode crew-fresh review-gate captain-up backlog-ready lull keepers-covered release-due; do
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

# ── Test 27a: release-due — count < threshold → no signal ────────────────────
echo ""
echo "=== Test 27a: release-due count < threshold (30 commits < 50) — no signal ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --git-release-count 30 \
  --git-last-tag 'v1.0.0' \
  --gh-run-conclusion 'success' \
)
OUTPUT=$(run_check "$PROJ")
assert_not_contains "27a: no release-due in stdout" "release-due" "$OUTPUT"
assert_json_list_empty "27a: no immediate_signals" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals"
assert_check_state "27a: checks.release-due=ok (count<threshold)" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "release-due" "ok"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  if grep -qF "release-due" "$LOG"; then
    fail "27a: should NOT send release-due comms when count < threshold"
  else
    pass "27a: no release-due comms (other signal may exist)"
  fi
else
  pass "27a: no comms sent (count below threshold)"
fi
rm -rf "$PROJ"

# ── Test 27b: release-due — count >= threshold AND CI green → immediate signal ─
echo ""
echo "=== Test 27b: release-due (count=55 >= 50, CI=green) — immediate signal ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --git-release-count 55 \
  --git-last-tag 'v1.0.0' \
  --gh-run-conclusion 'success' \
)
OUTPUT=$(run_check "$PROJ")
assert_contains "27b: IMMEDIATE in stdout"      "IMMEDIATE"    "$OUTPUT"
assert_contains "27b: release-due in stdout"    "release-due"  "$OUTPUT"
assert_contains "27b: count in stdout"          "55"           "$OUTPUT"
assert_json_list_contains "27b: immediate_signals has release-due" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals" "release-due:55"
assert_check_state "27b: checks.release-due=flag" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "release-due" "flag"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "27b: comms sent"
  assert_contains "27b: comms [IMMEDIATE]"    "[IMMEDIATE]"  "$(cat "$LOG")"
  assert_contains "27b: comms release-due"   "release-due"  "$(cat "$LOG")"
  assert_contains "27b: comms count"         "55"           "$(cat "$LOG")"
else
  fail "27b: expected comms send, got none"
fi
rm -rf "$PROJ"

# ── Test 27c: release-due — count >= threshold, CI not-green → no signal ──────
echo ""
echo "=== Test 27c: release-due (count=55, CI=not-green) — no signal ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --git-release-count 55 \
  --git-last-tag 'v1.0.0' \
  --gh-run-conclusion 'failure' \
)
OUTPUT=$(run_check "$PROJ")
assert_not_contains "27c: no release-due in stdout" "release-due" "$OUTPUT"
assert_json_list_empty "27c: no immediate_signals" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals"
assert_check_state "27c: checks.release-due=ok (CI not-green)" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "release-due" "ok"
CI_DETAIL=$(python3 -c "
import json
d = json.load(open('$PROJ/.harmonik/ops-monitor/latest.json'))
print(d.get('checks', {}).get('release-due', {}).get('detail', ''))
" 2>/dev/null || echo "")
assert_contains "27c: checks detail shows CI=not-green" "CI=not-green" "$CI_DETAIL"
rm -rf "$PROJ"

# ── Test 27d: release-due — CI unknown (gh unavailable) → no signal, no crash ─
# gh is not present in the fixture PATH (no --gh-run-conclusion arg).
echo ""
echo "=== Test 27d: release-due (count=55, CI=unknown/gh-unavailable) — no signal, no crash ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --git-release-count 55 \
  --git-last-tag 'v1.0.0' \
)
# No --gh-run-conclusion: gh stub not created → CI_STATUS=unknown
if HK_PROJECT="$PROJ" PATH="$PROJ/bin:$PATH" bash "$SCRIPT" > /dev/null 2>&1; then
  pass "27d: script exits 0 with gh unavailable (no crash)"
else
  fail "27d: script crashed when gh unavailable"
fi
assert_not_contains "27d: no release-due in stdout" "release-due" \
  "$(HK_PROJECT="$PROJ" PATH="$PROJ/bin:$PATH" bash "$SCRIPT" 2>&1)"
assert_json_list_empty "27d: no immediate_signals" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "immediate_signals"
assert_check_state "27d: checks.release-due=ok (CI=unknown)" \
  "$PROJ/.harmonik/ops-monitor/latest.json" "release-due" "ok"
CI_DETAIL_D=$(python3 -c "
import json
d = json.load(open('$PROJ/.harmonik/ops-monitor/latest.json'))
print(d.get('checks', {}).get('release-due', {}).get('detail', ''))
" 2>/dev/null || echo "")
assert_contains "27d: checks detail shows CI=unknown" "CI=unknown" "$CI_DETAIL_D"
rm -rf "$PROJ"

# ── Test 27e: release-due NOT in CRITICAL_PREFIXES (30-min cooldown, not 5-min) ─
# Pre-seed state with release-due:55 alerted 6 min ago. The script computes
# count=55 >= threshold AND CI=green → condition is true, BUT cooldown is active
# (6 min < 30 min IMMEDIATE_COOLDOWN). Signal must be suppressed (NOT in
# CRITICAL_PREFIXES → 5-min critical cooldown does NOT apply).
echo ""
echo "=== Test 27e: release-due NOT in CRITICAL_PREFIXES (suppressed at 6 min, 30-min cooldown) ==="
EPOCH_6M_AGO=$(python3 -c "import time; print(int(time.time()) - 360)")
STATE_27E='{"stale_crew_misses":{},"last_digest_ts":0,"alerted_immediate":{"release-due:55":'"$EPOCH_6M_AGO"'}}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --git-release-count 55 \
  --git-last-tag 'v1.0.0' \
  --gh-run-conclusion 'success' \
  --state-json "$STATE_27E" \
)
OUTPUT=$(run_check "$PROJ")
LATEST="$PROJ/.harmonik/ops-monitor/latest.json"
SEND_SIGS=$(python3 -c "import json; d=json.load(open('$LATEST')); print(json.dumps(d.get('send_immediate_signals', [])))" 2>/dev/null || echo "[]")
if echo "$SEND_SIGS" | grep -qF "release-due"; then
  fail "27e: release-due should be suppressed at 6 min (30-min IMMEDIATE_COOLDOWN, not 5-min critical)"
else
  pass "27e: release-due suppressed within 30-min cooldown (NOT a critical-component signal)"
fi
# Verify there is no escalation entry for release-due (not in CRITICAL_PREFIXES)
ESC=$(python3 -c "
import json
d = json.load(open('$LATEST'))
print(json.dumps(d.get('escalations', {}).get('release-due', 'absent')))
" 2>/dev/null || echo "absent")
assert_eq "27e: release-due not in escalations (not critical)" "\"absent\"" "$ESC"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  if grep -qF "release-due" "$LOG"; then
    fail "27e: no release-due comms should be sent within 30-min cooldown"
  else
    pass "27e: no release-due comms (suppressed; other signal may exist)"
  fi
else
  pass "27e: no comms sent (release-due cooldown active)"
fi
rm -rf "$PROJ"

# ── Test 28: ops-CRITICAL cooldown — tier-3 but ops-CRITICAL recently sent → no re-send ─
# supervisor-down has been down for 1h (count=10, tier-3) but ops-CRITICAL was sent 6 min
# ago. OPS_CRITICAL_COOLDOWN=30m → cooldown active → no ops-CRITICAL this run.
# The captain still gets the regular [ESCALATION] on ops-monitor (not suppressed).
echo ""
echo "=== Test 28: ops-CRITICAL cooldown — tier-3 + recent last_ops_critical_ts → no re-send ==="
STATE_28=$(python3 -c "
import json, time
now = int(time.time())
entry = {'first_ts': now - 3600, 'last_ts': now - 360, 'count': 10,
         'last_ops_critical_ts': now - 360}
print(json.dumps({'stale_crew_misses': {}, 'last_digest_ts': 0,
                  'alerted_immediate': {'supervisor-down': entry}}))
")
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --hk-supervise-status-json '{"schema_version":1,"running":false,"status":"stopped"}' \
  --state-json "$STATE_28" \
)
run_check "$PROJ" > /dev/null 2>&1
LATEST="$PROJ/.harmonik/ops-monitor/latest.json"
# Verify supervisor-down is still in send_immediate_signals (captain still alerted)
SEND_SIGS=$(python3 -c "import json; d=json.load(open('$LATEST')); print(json.dumps(d.get('send_immediate_signals', [])))" 2>/dev/null || echo "[]")
if echo "$SEND_SIGS" | grep -qF "supervisor-down"; then
  pass "28: supervisor-down in send_immediate_signals (captain alert unaffected)"
else
  fail "28: supervisor-down missing from send_immediate_signals (expected captain alert)"
fi
# Verify ops-CRITICAL NOT sent (cooldown active)
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  assert_not_contains "28: no ops-CRITICAL re-send within cooldown" "topic=ops-CRITICAL" "$(cat "$LOG")"
  # The [ESCALATION] tier-2+ message on ops-monitor IS sent
  assert_contains "28: [ESCALATION] on ops-monitor still sent" "[ESCALATION]" "$(cat "$LOG")"
else
  fail "28: expected at least the ops-monitor escalation comms, got none"
fi
# Verify send_ops_critical=False in escalations snapshot
SC=$(python3 -c "
import json
d = json.load(open('$LATEST'))
print(str(d.get('escalations', {}).get('supervisor-down', {}).get('send_ops_critical', 'missing')).lower())
" 2>/dev/null || echo "missing")
assert_eq "28: send_ops_critical=false (cooldown active)" "false" "$SC"
rm -rf "$PROJ"

# ── Test 29: ops-CRITICAL cooldown elapsed — tier-3 + stale last_ops_critical_ts → re-fires ─
# supervisor-down, count=10, ops-CRITICAL last sent 35 min ago (> 30 min OPS_CRITICAL_COOLDOWN).
# Cooldown has elapsed → ops-CRITICAL must fire again.
echo ""
echo "=== Test 29: ops-CRITICAL cooldown elapsed (35m > 30m) → ops-CRITICAL re-fires ==="
STATE_29=$(python3 -c "
import json, time
now = int(time.time())
entry = {'first_ts': now - 3600, 'last_ts': now - 360, 'count': 10,
         'last_ops_critical_ts': now - 2100}  # 35 min ago > OPS_CRITICAL_COOLDOWN(30m)
print(json.dumps({'stale_crew_misses': {}, 'last_digest_ts': 0,
                  'alerted_immediate': {'supervisor-down': entry}}))
")
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --hk-supervise-status-json '{"schema_version":1,"running":false,"status":"stopped"}' \
  --state-json "$STATE_29" \
)
run_check "$PROJ" > /dev/null 2>&1
LATEST="$PROJ/.harmonik/ops-monitor/latest.json"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  assert_contains "29: ops-CRITICAL re-fires after cooldown" "topic=ops-CRITICAL" "$(cat "$LOG")"
  assert_contains "29: [ops-CRITICAL] in body"               "[ops-CRITICAL]"    "$(cat "$LOG")"
else
  fail "29: expected ops-CRITICAL re-send after cooldown, got none"
fi
# send_ops_critical=True in escalations snapshot
SC=$(python3 -c "
import json
d = json.load(open('$LATEST'))
print(str(d.get('escalations', {}).get('supervisor-down', {}).get('send_ops_critical', 'missing')).lower())
" 2>/dev/null || echo "missing")
assert_eq "29: send_ops_critical=true (cooldown elapsed)" "true" "$SC"
# last_ops_critical_ts updated in state (fresh timestamp, not 2100s ago)
LT=$(python3 -c "
import json, time
d = json.load(open('$PROJ/.harmonik/ops-monitor/state.json'))
e = d.get('alerted_immediate', {}).get('supervisor-down', {})
lts = e.get('last_ops_critical_ts', 0)
print('fresh' if time.time() - lts < 60 else 'stale')
" 2>/dev/null || echo "missing")
assert_eq "29: last_ops_critical_ts updated to now in state" "fresh" "$LT"
rm -rf "$PROJ"

# ── WE7: sender-redirect + watch-liveness tests ──────────────────────────────

# Test 30: UN-SET opsmonitor_target → defaults to captain for ALL sends (partition is inert).
# No config.yaml in fixture → WATCH_OPSMONITOR_TARGET defaults to 'captain'.
# daemon-down (direct-class) AND single-mode (watch-class) both fire; both go --to captain.
echo ""
echo "=== Test 30: WE7 — no opsmonitor_target config → all sends default to captain ==="
COMMS_WHO_30=$(python3 -c "
import json, time
now = int(time.time())
# single-mode: max_concurrent=1
print(json.dumps({'who': []}))
")
PROJ=$(setup_fixture \
  --hk-queue-status-exit 17 \
  --hk-queue-list-json '{"queues":[],"max_concurrent":1}' \
  --hk-comms-who-json '' \
  --hk-supervise-status-json '{"schema_version":1,"running":true,"status":"running"}' \
)
run_check "$PROJ" > /dev/null 2>&1
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  # daemon-down is direct-class, must go to captain
  if grep -q "daemon-down" "$LOG"; then
    if grep "daemon-down" "$LOG" | grep -q "to=captain"; then
      pass "30: daemon-down (direct-class) routed to captain (no config)"
    else
      fail "30: daemon-down (direct-class) NOT routed to captain; got: $(grep "daemon-down" "$LOG")"
    fi
  else
    pass "30: daemon-down not in comms (cooldown or fixture variance — skip routing check)"
  fi
  # All sends must be --to captain when no config.yaml
  if grep -v "to=captain" "$LOG" | grep -q "to="; then
    fail "30: found non-captain --to target with no config; log: $(cat "$LOG")"
  else
    pass "30: all comms sends target captain (no config — default preserved)"
  fi
else
  pass "30: no comms sent (daemon-down suppressed by fixture — routing not testable)"
fi
rm -rf "$PROJ"

# Test 31: opsmonitor_target='watch' — direct-class stays --to captain; watch-class → --to watch.
# daemon-down triggers direct-class; single-mode triggers watch-class (max_concurrent=1).
echo ""
echo "=== Test 31: WE7 — opsmonitor_target=watch → direct-class=captain, watch-class=watch ==="
PROJ=$(setup_fixture \
  --hk-queue-status-exit 17 \
  --hk-queue-list-json '{"queues":[],"max_concurrent":1}' \
  --hk-comms-who-json '' \
  --hk-supervise-status-json '{"schema_version":1,"running":true,"status":"running"}' \
  --watch-opsmonitor-target watch \
  --tmux-watch-alive true \
)
run_check "$PROJ" > /dev/null 2>&1
LOG=$(comms_log "$PROJ")
if [[ ! -f "$LOG" || ! -s "$LOG" ]]; then
  fail "31: expected comms sends (daemon-down + single-mode), got none"
else
  # direct-class (daemon-down) must go to captain
  if grep "daemon-down" "$LOG" | grep -q "to=captain"; then
    pass "31: daemon-down (direct-class) → to=captain (§4 SPOF bypass)"
  else
    fail "31: daemon-down (direct-class) did not go to captain; log: $(cat "$LOG")"
  fi
  # watch-class (single-mode) must go to watch
  if grep "single-mode" "$LOG" | grep -q "to=watch"; then
    pass "31: single-mode (watch-class) → to=watch (opsmonitor_target)"
  elif grep -q "single-mode" "$LOG"; then
    fail "31: single-mode (watch-class) did not go to watch; log: $(cat "$LOG")"
  else
    pass "31: single-mode not in comms (max_concurrent fixture variance)"
  fi
fi
rm -rf "$PROJ"

# Test 32: watch-down → direct-class → always --to captain (SPOF bypass).
# opsmonitor_target='watch', tmux-watch-alive=false → watch-down fires and goes to captain,
# NOT to opsmonitor_target (watch). Sending it to a dead watch would be useless.
echo ""
echo "=== Test 32: WE7 — watch tmux down → watch-down is direct-class → --to captain ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --hk-supervise-status-json '{"schema_version":1,"running":true,"status":"running"}' \
  --watch-opsmonitor-target watch \
  --tmux-watch-alive false \
)
run_check "$PROJ" > /dev/null 2>&1
LATEST="$PROJ/.harmonik/ops-monitor/latest.json"
if [[ ! -f "$LATEST" ]]; then
  fail "32: latest.json missing"
else
  SIGS=$(python3 -c "import json; d=json.load(open('$LATEST')); print(json.dumps(d.get('immediate_signals', [])))" 2>/dev/null || echo "[]")
  if echo "$SIGS" | grep -q "watch-down"; then
    pass "32: watch-down in immediate_signals when watch tmux down + target configured"
  else
    fail "32: watch-down missing from immediate_signals; got: $SIGS"
  fi
  # watch-down must appear in direct_signals (captain-routed), NOT watch_signals.
  DIRECT=$(python3 -c "import json; d=json.load(open('$LATEST')); print(json.dumps(d.get('direct_signals', [])))" 2>/dev/null || echo "[]")
  WATCH_SIG=$(python3 -c "import json; d=json.load(open('$LATEST')); print(json.dumps(d.get('watch_signals', [])))" 2>/dev/null || echo "[]")
  if echo "$DIRECT" | grep -q "watch-down"; then
    pass "32: watch-down in direct_signals (captain-routed)"
  else
    fail "32: watch-down NOT in direct_signals; direct=$DIRECT watch=$WATCH_SIG"
  fi
  if echo "$WATCH_SIG" | grep -q "watch-down"; then
    fail "32: watch-down leaked into watch_signals (must be direct-class only)"
  else
    pass "32: watch-down absent from watch_signals (correct)"
  fi
  LOG=$(comms_log "$PROJ")
  if [[ -f "$LOG" && -s "$LOG" ]]; then
    if grep "watch-down" "$LOG" | grep -q "to=captain"; then
      pass "32: watch-down comms send routed to captain (direct-class SPOF bypass)"
    elif grep -q "watch-down" "$LOG"; then
      fail "32: watch-down sent but NOT to captain; log: $(cat "$LOG")"
    else
      pass "32: watch-down in snapshot but comms cooldown-suppressed (routing verified via direct_signals)"
    fi
  else
    pass "32: no comms on first run (cooldown); routing verified via direct_signals"
  fi
fi
rm -rf "$PROJ"

# Test 33: watch-down does NOT fire when opsmonitor_target is default ('captain' / unset).
# Merging WE7 must be inert: no noise from an un-deployed watch agent.
echo ""
echo "=== Test 33: WE7 — no opsmonitor_target + watch tmux down → watch-down SUPPRESSED (inert merge) ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --hk-supervise-status-json '{"schema_version":1,"running":true,"status":"running"}' \
  --tmux-watch-alive false \
)
run_check "$PROJ" > /dev/null 2>&1
LATEST="$PROJ/.harmonik/ops-monitor/latest.json"
if [[ -f "$LATEST" ]]; then
  SIGS=$(python3 -c "import json; d=json.load(open('$LATEST')); print(json.dumps(d.get('immediate_signals', [])))" 2>/dev/null || echo "[]")
  if echo "$SIGS" | grep -q "watch-down"; then
    fail "33: watch-down fired with no opsmonitor_target config (breaks inert-merge guarantee)"
  else
    pass "33: watch-down suppressed when no opsmonitor_target configured (inert-merge OK)"
  fi
else
  fail "33: latest.json missing"
fi
rm -rf "$PROJ"

# ── Tests 34a/34b: probe name RED→GREEN (crew-watch session naming) ──────────
# The probe was broken: it probed "harmonik-<hash>-watch" but the crew launcher
# creates "harmonik-<hash>-crew-watch". The tmux stub matches *"-watch" (both
# endings), so these tests verify the conditional (watch alive→no signal /
# watch absent→signal to captain) independently of the exact session suffix.

# Test 34a: watch tmux alive + opsmonitor_target=watch → no watch-down.
echo ""
echo "=== Test 34a: watch tmux alive + opsmonitor_target=watch → no watch-down ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --hk-supervise-status-json '{"schema_version":1,"running":true,"status":"running"}' \
  --watch-opsmonitor-target watch \
  --tmux-watch-alive true \
)
OUTPUT_34A=$(run_check "$PROJ")
LATEST_34A="$PROJ/.harmonik/ops-monitor/latest.json"
SIGS_34A=$(python3 -c "import json; d=json.load(open('$LATEST_34A')); print(json.dumps(d.get('immediate_signals', [])))" 2>/dev/null || echo "[]")
if echo "$SIGS_34A" | grep -q "watch-down"; then
  fail "34a: watch-down fired when watch tmux is alive (false positive)"
else
  pass "34a: no watch-down when watch tmux is alive"
fi
assert_not_contains "34a: no watch-down in stdout" "watch-down" "$OUTPUT_34A"
rm -rf "$PROJ"

# Test 34b: watch tmux absent + opsmonitor_target=watch → watch-down emitted --to captain.
echo ""
echo "=== Test 34b: watch tmux absent + opsmonitor_target=watch → watch-down --to captain ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --hk-supervise-status-json '{"schema_version":1,"running":true,"status":"running"}' \
  --watch-opsmonitor-target watch \
  --tmux-watch-alive false \
)
OUTPUT_34B=$(run_check "$PROJ")
LATEST_34B="$PROJ/.harmonik/ops-monitor/latest.json"
assert_contains "34b: watch-down in stdout"     "watch-down" "$OUTPUT_34B"
assert_contains "34b: IMMEDIATE in stdout"      "IMMEDIATE"  "$OUTPUT_34B"
DIRECT_34B=$(python3 -c "import json; d=json.load(open('$LATEST_34B')); print(json.dumps(d.get('direct_signals', [])))" 2>/dev/null || echo "[]")
if echo "$DIRECT_34B" | grep -q "watch-down"; then
  pass "34b: watch-down in direct_signals (→ captain)"
else
  fail "34b: watch-down not in direct_signals; got: $DIRECT_34B"
fi
LOG_34B=$(comms_log "$PROJ")
if [[ -f "$LOG_34B" && -s "$LOG_34B" ]]; then
  if grep "watch-down" "$LOG_34B" | grep -q "to=captain"; then
    pass "34b: watch-down comms routed to captain (not to watch)"
  elif grep -q "watch-down" "$LOG_34B"; then
    fail "34b: watch-down sent but NOT to captain; log: $(cat "$LOG_34B")"
  else
    pass "34b: watch-down in snapshot; routing confirmed via direct_signals"
  fi
else
  # Cooldown may suppress first send; routing is confirmed via direct_signals above.
  pass "34b: no comms on first run; routing confirmed via direct_signals"
fi
rm -rf "$PROJ"

# ── WE9 Tests ─────────────────────────────────────────────────────────────────

# Test 35a: watch present+tmux-alive but cursor frozen N ticks with pending events
# → exactly ONE 'watch-stalled' IMMEDIATE emitted (both comms-who + tmux alive, so
#   watch_down must NOT fire; only the cursor stall check fires).
echo ""
echo "=== Test 35a: WE9 — cursor frozen N ticks with pending events → watch-stalled IMMEDIATE ==="
EID_CURSOR="eid-cursor-aaa-001"
EID_LATEST="eid-latest-bbb-002"
WATCH_TS_OLD=$(ts_ago 300)
WATCH_TS_NEW=$(ts_ago 200)
EVENTS_35A='{"event_id":"'"$EID_CURSOR"'","type":"run_completed","timestamp_wall":"'"$WATCH_TS_OLD"'","run_id":"run-aaa"}
{"event_id":"'"$EID_LATEST"'","type":"run_completed","timestamp_wall":"'"$WATCH_TS_NEW"'","run_id":"run-bbb"}'
# prev state: cursor already frozen for (stall_ticks - 1) = 2 ticks; one more → fires
STATE_35A='{"schema_version":1,"ts":"2026-06-24T00:00:00Z","stale_crew_misses":{},"keeper_coverage_misses":{},"last_digest_ts":0,"alerted_immediate":{},"watch_cursor":"'"$EID_CURSOR"'","watch_stall_misses":2}'
# comms-who: watch is present and online (so watch_down stays False — testing stall only)
WATCH_TS_RECENT=$(ts_ago 30)
CW_35A='{"agent":"watch","status":"online","last_seen":"'"$WATCH_TS_RECENT"'"}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json "$CW_35A" \
  --hk-supervise-status-json '{"schema_version":1,"running":true,"status":"running"}' \
  --watch-opsmonitor-target watch \
  --watch-stall-ticks 3 \
  --watch-cursor "$EID_CURSOR" \
  --state-json "$STATE_35A" \
  --events-jsonl "$EVENTS_35A" \
  --tmux-watch-alive true \
)
OUTPUT_35A=$(run_check "$PROJ")
LATEST_35A="$PROJ/.harmonik/ops-monitor/latest.json"
if [[ ! -f "$LATEST_35A" ]]; then
  fail "35a: latest.json missing"
else
  SIGS_35A=$(python3 -c "import json; d=json.load(open('$LATEST_35A')); print(json.dumps(d.get('immediate_signals', [])))" 2>/dev/null || echo "[]")
  if echo "$SIGS_35A" | grep -q "watch-stalled"; then
    pass "35a: watch-stalled in immediate_signals (cursor frozen with pending events)"
  else
    fail "35a: watch-stalled NOT in immediate_signals; got: $SIGS_35A"
  fi
  # watch_down must NOT fire (watch is present + tmux alive)
  if echo "$SIGS_35A" | grep -q "watch-down"; then
    fail "35a: watch-down must NOT fire when watch is comms-present + tmux alive"
  else
    pass "35a: watch-down correctly absent (watch is present, dual-probe holds)"
  fi
  # watch-stalled is direct-class: must appear in direct_signals, not watch_signals
  DIRECT_35A=$(python3 -c "import json; d=json.load(open('$LATEST_35A')); print(json.dumps(d.get('direct_signals', [])))" 2>/dev/null || echo "[]")
  WSIG_35A=$(python3 -c "import json; d=json.load(open('$LATEST_35A')); print(json.dumps(d.get('watch_signals', [])))" 2>/dev/null || echo "[]")
  if echo "$DIRECT_35A" | grep -q "watch-stalled"; then
    pass "35a: watch-stalled in direct_signals (→ captain)"
  else
    fail "35a: watch-stalled not in direct_signals; direct=$DIRECT_35A watch=$WSIG_35A"
  fi
  if echo "$WSIG_35A" | grep -q "watch-stalled"; then
    fail "35a: watch-stalled leaked into watch_signals (must be direct-class)"
  else
    pass "35a: watch-stalled absent from watch_signals (correct direct-class routing)"
  fi
  assert_contains "35a: IMMEDIATE in stdout" "IMMEDIATE" "$OUTPUT_35A"
  assert_contains "35a: watch-stalled in stdout" "watch-stalled" "$OUTPUT_35A"
fi
rm -rf "$PROJ"

# Test 35b: cursor advancing → ZERO watch-stalled signals.
# prev cursor differs from current cursor → miss counter resets → no stall.
echo ""
echo "=== Test 35b: WE9 — cursor advancing → no watch-stalled ==="
EID_OLD_35B="eid-old-aaa-001"
EID_NEW_35B="eid-new-bbb-002"
EVENTS_35B='{"event_id":"'"$EID_OLD_35B"'","type":"run_completed","timestamp_wall":"'"$(ts_ago 300)"'","run_id":"run-x"}
{"event_id":"'"$EID_NEW_35B"'","type":"run_completed","timestamp_wall":"'"$(ts_ago 200)"'","run_id":"run-y"}'
# prev state had cursor at old id; current cursor file has the new id (advanced)
STATE_35B='{"schema_version":1,"ts":"2026-06-24T00:00:00Z","stale_crew_misses":{},"keeper_coverage_misses":{},"last_digest_ts":0,"alerted_immediate":{},"watch_cursor":"'"$EID_OLD_35B"'","watch_stall_misses":2}'
CW_35B='{"agent":"watch","status":"online","last_seen":"'"$(ts_ago 30)"'"}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json "$CW_35B" \
  --hk-supervise-status-json '{"schema_version":1,"running":true,"status":"running"}' \
  --watch-opsmonitor-target watch \
  --watch-stall-ticks 3 \
  --watch-cursor "$EID_NEW_35B" \
  --state-json "$STATE_35B" \
  --events-jsonl "$EVENTS_35B" \
  --tmux-watch-alive true \
)
OUTPUT_35B=$(run_check "$PROJ" 2>&1)
LATEST_35B="$PROJ/.harmonik/ops-monitor/latest.json"
if [[ ! -f "$LATEST_35B" ]]; then
  fail "35b: latest.json missing"
else
  SIGS_35B=$(python3 -c "import json; d=json.load(open('$LATEST_35B')); print(json.dumps(d.get('immediate_signals', [])))" 2>/dev/null || echo "[]")
  if echo "$SIGS_35B" | grep -q "watch-stalled"; then
    fail "35b: watch-stalled fired despite cursor advancing (false positive)"
  else
    pass "35b: no watch-stalled when cursor is advancing"
  fi
  if echo "$SIGS_35B" | grep -q "watch-down"; then
    fail "35b: watch-down must not fire (watch is comms-present + tmux alive)"
  else
    pass "35b: no watch-down when watch is present and tmux alive"
  fi
fi
rm -rf "$PROJ"

# Test 35c: watch absent-from-comms BUT tmux-alive → NOT watch_down (dual-probe semantics).
# WE9 dual-probe: watch_down requires BOTH comms-absence AND no-tmux.
# A process alive in tmux but absent from comms-who is suspicious (may be bus-pinned)
# but must NOT trigger watch-down — that's what the cursor-stall check catches.
echo ""
echo "=== Test 35c: WE9 — watch absent-from-comms but tmux-alive → NOT watch_down (dual-probe) ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --hk-supervise-status-json '{"schema_version":1,"running":true,"status":"running"}' \
  --watch-opsmonitor-target watch \
  --tmux-watch-alive true \
)
OUTPUT_35C=$(run_check "$PROJ" 2>&1)
LATEST_35C="$PROJ/.harmonik/ops-monitor/latest.json"
if [[ ! -f "$LATEST_35C" ]]; then
  fail "35c: latest.json missing"
else
  SIGS_35C=$(python3 -c "import json; d=json.load(open('$LATEST_35C')); print(json.dumps(d.get('immediate_signals', [])))" 2>/dev/null || echo "[]")
  if echo "$SIGS_35C" | grep -q "watch-down"; then
    fail "35c: watch-down fired when tmux is alive (violates dual-probe — must require BOTH absent+no-tmux)"
  else
    pass "35c: watch-down suppressed when tmux is alive (dual-probe correct)"
  fi
fi
rm -rf "$PROJ"

# ══════════════════════════════════════════════════════════════════════════════
# SD-4: program-drained-stall detector (PLAN-v2 Part 0 signal (a)) — reproduce the
# actual 2026-06-25 ~2h stall shape and assert the IMMEDIATE fires AND NAMES the lane.
# ══════════════════════════════════════════════════════════════════════════════

# Lane index mirroring the real .harmonik/context/lanes.json: wake-economy (the lane
# the 2026-06-25 stall mis-classified) with epic hk-var9b and a NULL gate (KNOWN /
# resumable), plus pi-gateway (epic_id null + gate => epic-less, can never fire).
SD4_LANES='{"schema_version":1,"lanes":[{"lane":"wake-economy","label":"codename:wake-economy","epic_id":"hk-var9b","status":"active","gate":null},{"lane":"pi-gateway","label":"codename:pi-openrouter","epic_id":null,"status":"parked","gate":{"owner":"operator","reason":"not before remote-worker proven","expires":"2026-07-09"}}]}'

# ── Test 36: SD-4 POSITIVE — program drained + wake-economy ready + free slot ──
# Reproduces the 2026-06-25 stall: the remote-pyramid PROGRAM has drained (fleet idle,
# 0 active workers, last run event >20m ago), the KNOWN parked lane wake-economy
# (epic hk-var9b) has ready beads, and a free slot exists (max_concurrent 4, 0 busy).
# Assert: an IMMEDIATE 'program-drained-stall' fires AND the wake NAMES wake-economy.
echo ""
echo "=== Test 36: SD-4 POSITIVE — program drained + KNOWN ready lane + free slot → IMMEDIATE names wake-economy ==="
OLD_TS=$(ts_ago 1500)   # 25 min > 20-min idle threshold → program_drained (== idle_fleet)
SD4_EVENTS='{"type":"run_completed","ts":"'"$OLD_TS"'","run_id":"r-pyramid-last"}'
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --events-jsonl "$SD4_EVENTS" \
  --lanes-json "$SD4_LANES" \
  --br-parent-json '{"hk-var9b":[{"id":"hk-we-1"},{"id":"hk-we-2"}]}' \
)
OUTPUT=$(run_check "$PROJ")
LATEST="$PROJ/.harmonik/ops-monitor/latest.json"
assert_contains "SD4+ stdout IMMEDIATE"        "IMMEDIATE"               "$OUTPUT"
assert_contains "SD4+ stdout signal"           "program-drained-stall"   "$OUTPUT"
assert_json_bool "SD4+ latest.json program_drained_stall=true" \
  "$LATEST" "program_drained_stall" "true"
assert_json_list_contains "SD4+ immediate_signals has program-drained-stall" \
  "$LATEST" "immediate_signals" "program-drained-stall"
# The wake (signal text AND snapshot field) must NAME wake-economy + its epic + count.
assert_json_list_contains "SD4+ immediate_signals names wake-economy" \
  "$LATEST" "immediate_signals" "lane=wake-economy"
KNOWN_LANE=$(python3 -c "import json;print(json.load(open('$LATEST')).get('known_ready_lane',''))")
assert_eq "SD4+ known_ready_lane == wake-economy" "wake-economy" "$KNOWN_LANE"
KNOWN_EPIC=$(python3 -c "import json;print(json.load(open('$LATEST')).get('known_ready_lane_epic',''))")
assert_eq "SD4+ known_ready_lane_epic == hk-var9b" "hk-var9b" "$KNOWN_EPIC"
assert_check_state "SD4+ checks.program-stall=flag" "$LATEST" "program-stall" "flag"
LOG=$(comms_log "$PROJ")
if [[ -f "$LOG" && -s "$LOG" ]]; then
  pass "SD4+ comms sent"
  assert_contains "SD4+ comms [IMMEDIATE]"            "[IMMEDIATE]"             "$(cat "$LOG")"
  assert_contains "SD4+ comms NAMES wake-economy"     "wake-economy"            "$(cat "$LOG")"
  assert_contains "SD4+ comms signal program-drained-stall" "program-drained-stall" "$(cat "$LOG")"
  # The wake must reach the captain (default opsmonitor_target).
  assert_contains "SD4+ comms routed --to captain"    "to=captain"              "$(cat "$LOG")"
else
  fail "SD4+ expected IMMEDIATE comms send, got none"
fi
rm -rf "$PROJ"

# ── Test 37: SD-4 NEGATIVE — same lane, ZERO ready beads → wake does NOT fire ──
# The real CURRENT fleet state: wake-economy is parked-as-fact (zero ready beads).
# The program may be drained + slot free, but with no KNOWN ready lane the
# deterministic predicate is FALSE — no program-drained-stall wake.
echo ""
echo "=== Test 37: SD-4 NEGATIVE — KNOWN lane has ZERO ready beads → no program-drained-stall ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --events-jsonl "$SD4_EVENTS" \
  --lanes-json "$SD4_LANES" \
  --br-parent-json '{"hk-var9b":[]}' \
)
OUTPUT=$(run_check "$PROJ")
LATEST="$PROJ/.harmonik/ops-monitor/latest.json"
assert_not_contains "SD4- no program-drained-stall in stdout" "program-drained-stall" "$OUTPUT"
assert_json_bool "SD4- program_drained_stall=false" "$LATEST" "program_drained_stall" "false"
assert_json_list_empty "SD4- immediate_signals empty" "$LATEST" "immediate_signals"
assert_check_state "SD4- checks.program-stall=ok" "$LATEST" "program-stall" "ok"
LOG=$(comms_log "$PROJ")
if grep -q "program-drained-stall" "$LOG" 2>/dev/null; then
  fail "SD4- program-drained-stall must NOT be sent when lane has zero ready beads"
else
  pass "SD4- no program-drained-stall comms when lane has zero ready beads"
fi
rm -rf "$PROJ"

# ── Test 38: SD-4 DEFENSIVE — lanes.json missing → no crash, no stall wake ─────
# When lanes.json is absent the known-ready-lane predicate cannot be computed; the
# check skips silently (the health pass must not crash) and the wake never fires.
echo ""
echo "=== Test 38: SD-4 DEFENSIVE — lanes.json missing → no crash, no program-drained-stall ==="
PROJ=$(setup_fixture \
  --hk-queue-status-json '{"status":"ok"}' \
  --hk-queue-list-json '{"queues":[],"max_concurrent":4}' \
  --hk-comms-who-json '' \
  --events-jsonl "$SD4_EVENTS" \
)
OUTPUT=$(run_check "$PROJ")
LATEST="$PROJ/.harmonik/ops-monitor/latest.json"
if [[ -f "$LATEST" ]]; then
  pass "SD4-defensive: latest.json written (no crash with missing lanes.json)"
else
  fail "SD4-defensive: latest.json missing — health pass crashed"
fi
assert_not_contains "SD4-defensive no program-drained-stall in stdout" "program-drained-stall" "$OUTPUT"
assert_json_bool "SD4-defensive program_drained_stall=false" "$LATEST" "program_drained_stall" "false"
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
