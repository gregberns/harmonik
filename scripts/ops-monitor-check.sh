#!/usr/bin/env bash
# ops-monitor-check.sh — One-pass deterministic fleet health check.
#
# Checks (in order):
#   1. daemon-up        — harmonik queue status exit 17 = daemon down
#   1a. supervisor-up   — harmonik supervise status --json; file-surface, no daemon needed.
#                         supervisor-down is [IMMEDIATE]; when BOTH daemon and supervisor are
#                         down the fleet has no self-healing path (hk-pen9: 7h11m gap).
#   2. paused-queues    — main queue or active crew queue paused-by-failure
#   3. single-mode      — max_concurrent == 1 (throughput bottleneck)
#   4. crew-staleness   — comms last_seen >150s; signals after 2 consecutive misses;
#                         suppressed if crew posted an agent_message within 900s (comms
#                         send does not refresh presence — hk-gu3v)
#   5. ready-unstaffed  — queue has pending_items > 0 but no workers and no online crew
#   6. idle-fleet       — no active workers + last run event >20m ago (lull detect)
#   7. review-gate      — a run_id that ENTERED-OR-REQUESTED a review (reviewer_launched
#                         event OR node_dispatch_requested node_id=review*) but produced
#                         NO matching reviewer_verdict = review BYPASSED (M2 code-half;
#                         CE4 + hk-orni short-circuit follow-up). Deterministic
#                         run_id↔verdict JOIN over the last N review-anchored run_ids,
#                         NOT a (broken) workflow_mode grep, and NOT a bare
#                         completed-without-verdict join (R6 fix hk-ayvx): the daemon has
#                         a LEGITIMATE review-less close path — MVH twin-blind
#                         `auto-close: exit=0`, noChange, and subsumed completions
#                         merge+close with NO reviewer BY DESIGN (workloop.go ~:3811).
#                         Those neither launch NOR request a reviewer, so they are absent
#                         from both anchors and stay suppressed instead of firing ~180
#                         false `review-bypass` alerts (alert fatigue would bury the REAL
#                         bypass). The node_dispatch_requested arm closes the hk-2vpj
#                         engine-short-circuit blind spot (hk-orni): the engine REQUESTS a
#                         review node but the reviewer never launches (no
#                         reviewer_launched), so a change merges+closes UNREVIEWED — now
#                         caught because a review node WAS requested yet no verdict exists
#                         (8 live run_ids, 0 auto-close false positives). Multi-iteration
#                         DOT runs (MEDIUM): launch/request and verdict are joined on the
#                         SAME run_id, so a verdict on ANY iteration clears the run; a
#                         terminal-close run_id that never launched OR requested a reviewer
#                         is in neither anchor, so unmatched `dot: reached terminal node
#                         close` run_ids can't false-flag either.
#   8. backlog-ready    — `br ready --limit 0` shows ready beads (captain staffing
#                         signal — judgment item, surfaced via digest)
#
# This is the DETERMINISTIC slice of the captain's /loop 12m health tick (CE4,
# leanfleet D4/D6). It runs as a cheap bash schedule (NOT an Opus turn); the captain
# reads latest.json once per tick and only escalates on FLAGGED judgment items.
#
# Writes:  $PROJ/.harmonik/ops-monitor/latest.json  (machine snapshot, always)
#          The snapshot includes a `checks` digest map: name -> {state:ok|flag, detail}
#          so the captain can read one structured signal-vs-digest object.
# Sends:   harmonik comms --from ops-monitor --to captain --topic ops-monitor
#            immediate : daemon-down | supervisor-down | paused-queue | single-mode | review-bypass
#            digest    : ready-unstaffed | idle-fleet | backlog-ready  (≤15m cooldown)
#            all-green : write json, send nothing
#
# Usage:
#   HK_PROJECT=/path/to/repo ./scripts/ops-monitor-check.sh
#   (defaults HK_PROJECT to cwd)
#
# State persistence: $PROJ/.harmonik/ops-monitor/state.json
#   stale_crew_misses  : {crew_name: consecutive_miss_count}
#   last_digest_ts     : unix epoch of last digest send
#
# Refs: hk-k2px, hk-ayvx (CE4), leanfleet D4/D6 (epic hk-itoc)

set -euo pipefail

PROJ="${HK_PROJECT:-$(pwd)}"
TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
TS_EPOCH=$(date +%s)
OUT_DIR="$PROJ/.harmonik/ops-monitor"
STATE_FILE="$OUT_DIR/state.json"
LATEST_FILE="$OUT_DIR/latest.json"
EVENTS_FILE="$PROJ/.harmonik/events/events.jsonl"

STALE_THRESHOLD=150   # seconds: crew last_seen older than this = stale
MISS_LIMIT=2          # consecutive stale observations before signaling
IDLE_THRESHOLD=1200   # 20 minutes: no active workers + no recent event = idle
DIGEST_COOLDOWN=900   # 15 minutes: minimum gap between digest sends
CREW_MSG_ACTIVE_WINDOW=900  # seconds: treat crew as active if it posted an agent_message within
                             # this window even when presence last_seen is stale (hk-gu3v).
                             # comms send does NOT refresh presence, so a keeper-restarting agent
                             # posting status every ~12m would exceed the 150s stale_thresh but
                             # IS demonstrably active.  900s (15m) ≈ one posting cadence + buffer.
REVIEW_GATE_WINDOW=10 # review-gate: examine the last N run_completed run_ids
REVIEW_GATE_GRACE=180 # seconds: a run_completed younger than this is skipped (its
                      # reviewer_verdict may still be in flight — avoids false bypass)
REVIEWER_STALE_WINDOW=3600 # seconds: a reviewer that launched but produced no verdict
                            # and has emitted no event for this long is considered
                            # abandoned — suppress bypass alert (hk-usz0 follow-up to hk-ijtw)
CAPTAIN_ABSENT_THRESHOLD=600   # seconds the captain may be absent from comms-who before we
                                # tmux-probe and (if also no session) alert captain-down

# Inert queues / dead-crew glob patterns — paused-by-failure on these NEVER fires an
# immediate alert. Add exact names or fnmatch-style globs. Editable here.
INERT_SUPPRESS_JSON='["main","remote-substrate","chani-q*","duncan-q*","liet-q*","stilgar-q*"]'
# Queues that are always alert-worthy even when their crew is offline.
LIVE_ALLOW_JSON='[]'
# Re-alert cooldown for the SAME still-active immediate signal (seconds).
IMMEDIATE_COOLDOWN=1800  # 30 minutes
# Shorter re-alert cooldown for critical-component down signals (daemon / supervisor / fleet /
# captain). A component that stays down should produce ~6 alerts in 30 min, not 1.
CRITICAL_IMMEDIATE_COOLDOWN=300  # 5 minutes
# Escalation thresholds: when a critical signal has been down long enough, escalate to the
# ops-CRITICAL operator-wake topic (tier 3). Either condition triggers tier 3.
OPS_CRITICAL_COUNT=6        # alert-count at which we escalate (≈ 30 min at 5-min critical cadence)
OPS_CRITICAL_ELAPSED=1800   # OR: seconds a critical signal has been down before operator-wake
RELEASE_DUE_COMMIT_THRESHOLD=50  # unreleased commits on main since last vX.Y.Z tag (per spec §202)
# Minimum gap between consecutive ops-CRITICAL operator-wake sends for a persistent signal.
# The underlying check and the captain's every-5m [ESCALATION] on ops-monitor are unaffected;
# only the operator-page channel is throttled.
OPS_CRITICAL_COOLDOWN=1800  # 30 minutes
# hk-ohb8p Fix B: persistent-condition throttle. A critical signal re-alerts at the
# 5-min CRITICAL_IMMEDIATE_COOLDOWN cadence ONLY while it is fresh. Once it has been
# firing continuously past the persistence threshold (tier-3: count>=OPS_CRITICAL_COUNT
# OR elapsed>=OPS_CRITICAL_ELAPSED — i.e. the fix has had ~30 min to land and the
# condition is clearly known/acknowledged), its captain-facing re-alert cadence drops
# to PERSISTENT_IMMEDIATE_COOLDOWN so a known-and-in-flight condition stops flooding the
# bus every 5 min for hours. A NEW distinct signal-identity still fires at the fast 5-min
# cadence; when this condition clears its state is dropped and it re-alerts fresh.
PERSISTENT_IMMEDIATE_COOLDOWN=1800  # 30 minutes (was 5 min before the condition persisted)
# hk-7o4i0 Fix A: flap-resistant retention window for flap-prone critical signals
# (watch-stalled). When such a signal clears for a tick its {first_ts,count} is NOT
# discarded immediately (the old behavior re-fired a fresh count=1 IMMEDIATE on every
# re-trip, bypassing the persistence throttle and producing the ~57-IMMEDIATE post-deploy
# storm). Instead the state is RETAINED across this wall-clock window so intermittent
# re-trips ACCUMULATE toward the cooldown/persistence throttle above. ~3 ticks at the
# 5-min ops-monitor cadence: long enough to bridge a one-tick flap, short enough that a
# genuinely-resolved signal still ages out and re-alerts fresh on real recurrence.
WATCH_STALL_FLAP_GRACE=900  # 15 minutes (~3 ops-monitor ticks)
# hk-7o4i0 Fix B: deploy/restart suppression window. While the watch is BOOTING — its
# tmux session (re)started this recently, OR the daemon emitted a restart/startup marker
# this recently — its escalation cursor is frozen because it is coming up, NOT because it
# is stalled. Suppress watch-stalled for this window so a post-deploy actionable backlog
# past the (legitimately) frozen cursor does not storm the bus during the boot.
WATCH_RESTART_SUPPRESS_WINDOW=600  # 10 minutes

mkdir -p "$OUT_DIR"

# ── Helpers ───────────────────────────────────────────────────────────────────

hk() { (cd "$PROJ" && harmonik "$@" 2>&1); }

py3() { python3 -c "$@"; }

# ── Load previous state ───────────────────────────────────────────────────────

PREV_STALE_MISSES='{}'
PREV_LAST_DIGEST=0
PREV_ALERTED_IMMEDIATE='{}'
PREV_KEEPER_COVERAGE_MISSES='{}'
PREV_WATCH_CURSOR=''
PREV_WATCH_STALL_MISSES=0
if [[ -f "$STATE_FILE" ]]; then
  PREV_STALE_MISSES=$(py3 "
import json, sys
try:
    d = json.load(open('$STATE_FILE'))
    print(json.dumps(d.get('stale_crew_misses', {})))
except Exception:
    print('{}')
")
  PREV_LAST_DIGEST=$(py3 "
import json
try:
    d = json.load(open('$STATE_FILE'))
    print(d.get('last_digest_ts', 0))
except Exception:
    print(0)
")
  PREV_ALERTED_IMMEDIATE=$(py3 "
import json
try:
    d = json.load(open('$STATE_FILE'))
    print(json.dumps(d.get('alerted_immediate', {})))
except Exception:
    print('{}')
")
  PREV_KEEPER_COVERAGE_MISSES=$(py3 "
import json
try:
    d = json.load(open('$STATE_FILE'))
    print(json.dumps(d.get('keeper_coverage_misses', {})))
except Exception:
    print('{}')
")
  PREV_WATCH_CURSOR=$(py3 "
import json
try:
    d = json.load(open('$STATE_FILE'))
    print(d.get('watch_cursor', ''))
except Exception:
    print('')
")
  PREV_WATCH_STALL_MISSES=$(py3 "
import json
try:
    d = json.load(open('$STATE_FILE'))
    print(d.get('watch_stall_misses', 0))
except Exception:
    print(0)
")
fi

# ── Check 1: Daemon up ────────────────────────────────────────────────────────

DAEMON_UP=true
QUEUE_LIST_JSON=""
QUEUE_STATUS_JSON=""

hk_exit=0
QUEUE_STATUS_JSON=$(hk queue status --json) || hk_exit=$?
if [[ $hk_exit -eq 17 ]]; then
  DAEMON_UP=false
fi

# ── Check 1a: Supervisor up ──────────────────────────────────────────────────
# File-surface: reads supervisor.pid + kill(0). No daemon required.
# supervisor-down is [IMMEDIATE]: when the supervisor is dead, DaemonWatchdog
# is also dead, so daemon deaths become permanent until a human intervenes.
# When BOTH daemon and supervisor are down this is fleet-down (hk-pen9).

SUPERVISOR_UP=false
supervisor_exit=0
SUPERVISOR_STATUS_JSON=$(hk supervise status --json 2>/dev/null) || supervisor_exit=$?
if [[ $supervisor_exit -eq 0 && -n "$SUPERVISOR_STATUS_JSON" ]]; then
  sv_running=$(printf '%s' "$SUPERVISOR_STATUS_JSON" | py3 "
import json, sys
try:
    d = json.load(sys.stdin)
    print('true' if d.get('running') else 'false')
except Exception:
    print('false')
" 2>/dev/null || echo "false")
  if [[ "$sv_running" == "true" ]]; then
    SUPERVISOR_UP=true
  fi
fi

# ── Check captain liveness: tmux probe (authoritative confirm) ───────────────
# comms presence alone is unreliable: a quietly-monitoring captain's presence
# ages out of comms who while its tmux session stays alive and attached.
# Require BOTH comms absence AND no tmux session before concluding captain is down.
CAPTAIN_TMUX_ALIVE=false
_CAPTAIN_HASH=$( (cd "$PROJ" && harmonik project-hash) 2>/dev/null ) || true
if [[ -n "$_CAPTAIN_HASH" ]]; then
  tmux has-session -t "harmonik-${_CAPTAIN_HASH}-captain" 2>/dev/null \
    && CAPTAIN_TMUX_ALIVE=true || true
fi

# ── Read watch routing target (WE7) ──────────────────────────────────────────
# watch.opsmonitor_target defaults to 'captain' (§7 exception: NOT fail-loud).
# Flip to 'watch' ONLY after MVP-standup AND 'keeper doctor watch' is green.
WATCH_OPSMONITOR_TARGET=captain
if [[ -f "$PROJ/.harmonik/config.yaml" ]]; then
  _watch_target=$(py3 "
import re, sys
try:
    txt = open('$PROJ/.harmonik/config.yaml').read()
    m = re.search(r'^\s*opsmonitor_target:\s*(.+)', txt, re.MULTILINE)
    if m:
        sys.stdout.write(m.group(1).strip().strip('\"'))
        sys.exit(0)
except Exception:
    pass
sys.stdout.write('captain')
" 2>/dev/null || echo captain)
  WATCH_OPSMONITOR_TARGET="${_watch_target:-captain}"
fi

# ── Check watch liveness: tmux probe (basic) ─────────────────────────────────
# WE7: basic process/tmux-down → escalate. Alive-but-stalled dual-probe+cursor
# advancement is WE9 (below).
WATCH_TMUX_ALIVE=false
if [[ -n "$_CAPTAIN_HASH" ]]; then
  tmux has-session -t "harmonik-${_CAPTAIN_HASH}-crew-watch" 2>/dev/null \
    && WATCH_TMUX_ALIVE=true || true
fi

# ── hk-7o4i0 Fix B: watch tmux session start epoch (deploy/restart suppression) ─
# A watch whose tmux session (re)started within WATCH_RESTART_SUPPRESS_WINDOW is booting,
# not stalled. session_created is epoch seconds; 0 == unknown (no session / probe failed)
# → no suppression from this source.
WATCH_SESSION_CREATED=0
if [[ -n "$_CAPTAIN_HASH" && "$WATCH_TMUX_ALIVE" == "true" ]]; then
  _wsc=$(tmux display-message -p -t "harmonik-${_CAPTAIN_HASH}-crew-watch" '#{session_created}' 2>/dev/null || echo "")
  if [[ "$_wsc" =~ ^[0-9]+$ ]]; then
    WATCH_SESSION_CREATED="$_wsc"
  fi
fi

# ── WE9: watch absent threshold + stall ticks (config-or-fail-loud) ──────────
# These only matter when the watch is the routing target (opsmonitor_target != captain).
# Unlike CAPTAIN_ABSENT_THRESHOLD (hardcoded 600s), watch thresholds have no safe default
# — they MUST be explicit so operators know what they're tuning.
WATCH_ABSENT_THRESHOLD=0
WATCH_STALL_TICKS=0
if [[ "$WATCH_OPSMONITOR_TARGET" != "captain" ]]; then
  _wat=$(py3 "
import re, sys
try:
    txt = open('$PROJ/.harmonik/config.yaml').read()
    m = re.search(r'^\s*absent_thresh_s:\s*(\d+)', txt, re.MULTILINE)
    if m:
        print(m.group(1).strip())
        sys.exit(0)
except Exception:
    pass
print('MISSING')
" 2>/dev/null || echo MISSING)
  if [[ "$_wat" == "MISSING" || -z "$_wat" ]]; then
    echo "ops-monitor-check: FAIL — watch.absent_thresh_s not set in $PROJ/.harmonik/config.yaml; run: harmonik watch config --example" >&2
    exit 1
  fi
  WATCH_ABSENT_THRESHOLD="$_wat"

  _wst=$(py3 "
import re, sys
try:
    txt = open('$PROJ/.harmonik/config.yaml').read()
    m = re.search(r'^\s*stall_ticks:\s*(\d+)', txt, re.MULTILINE)
    if m:
        print(m.group(1).strip())
        sys.exit(0)
except Exception:
    pass
print('MISSING')
" 2>/dev/null || echo MISSING)
  if [[ "$_wst" == "MISSING" || -z "$_wst" ]]; then
    echo "ops-monitor-check: FAIL — watch.stall_ticks not set in $PROJ/.harmonik/config.yaml; run: harmonik watch config --example" >&2
    exit 1
  fi
  WATCH_STALL_TICKS="$_wst"
fi

# ── WE9: read watch escalation cursor (for cursor-advancement stall check) ───
WATCH_CURSOR=""
if [[ -f "$PROJ/.harmonik/watch/cursor" ]]; then
  WATCH_CURSOR=$(tr -d '[:space:]' < "$PROJ/.harmonik/watch/cursor" 2>/dev/null || echo "")
fi

# ── Collect data (skip if daemon is down) ─────────────────────────────────────

if [[ "$DAEMON_UP" == "true" ]]; then
  QUEUE_LIST_JSON=$(hk queue list --json) || QUEUE_LIST_JSON="{}"
fi

COMMS_WHO_NDJSON=""
hk_comms_exit=0
COMMS_WHO_NDJSON=$(hk comms who --json) || hk_comms_exit=$?
# exit 17 from comms who means daemon down (already handled); other errors: tolerate
if [[ $hk_comms_exit -eq 17 ]]; then
  DAEMON_UP=false
fi

# ── Check 8: Backlog-readiness (br ready --limit 0) ────────────────────────────
# Deterministic count of beads ready to staff. The COUNT is deterministic; the
# staffing DECISION (which crew, which lane) stays on the Opus captain — we only
# surface the count so the captain wakes when ready work exists, not every 12m.
READY_COUNT=0
if command -v br >/dev/null 2>&1; then
  BR_READY_JSON=$( (cd "$PROJ" && br ready --limit 0 --json) 2>/dev/null ) || BR_READY_JSON=""
  if [[ -n "$BR_READY_JSON" ]]; then
    READY_COUNT=$(printf '%s' "$BR_READY_JSON" | py3 "
import sys, json
try:
    d = json.load(sys.stdin)
    # br --json may emit a bare list or {'issues': [...]} — handle both.
    if isinstance(d, dict):
        items = d.get('issues') or d.get('ready') or d.get('beads') or []
    elif isinstance(d, list):
        items = d
    else:
        items = []
    print(len(items))
except Exception:
    print(0)
" 2>/dev/null || echo 0)
  fi
fi

# ── Check 9: known-ready-lane (lanes.json + br ready --parent) — SD-1 ──────────
# SD-1 (admiral-operating-framework, PLAN-v2 Part 0 signal (a)): the DETERMINISTIC,
# agent-EXTERNAL stall detector. The 2026-06-25 ~2h stall happened because the
# admiral mis-CLASSIFIED a known, parked, already-ranked lane (token-opt /
# wake-economy) as "a brand-new initiative only the operator may rank," and every
# self-scored audit (C3 / anti-pattern G) ratified the idle fleet through that wrong
# frame. The fix removes the JUDGMENT from the trigger: a script computes the fact
# and pushes a lane-NAMED wake the captain cannot self-score its way out of.
#
# This block computes, for each lane in .harmonik/context/lanes.json:
#   - the lane is a candidate iff its epic_id is non-null AND its gate is null OR
#     EXPIRED (gate.expires < now == absent; LAPSE→autonomous default, Part 1b), AND
#   - `br ready --parent <epic_id> --limit 0 --json` returns >=1 ready bead.
# "KNOWN" = present in the index (a fact read from a file), NOT "in the live kerf-next
# feed right now." Lanes with epic_id:null contribute ZERO (the index invariant
# guarantees an epic-less lane carries a gate, so it can never fire the wake).
#
# The first matching lane's name + epic + ready-count are passed into the python
# analysis, which AND-combines them with program_drained (== idle_fleet, Check 6)
# and free_slot (== the backlog-ready free-slot computation) to push the SD-1
# [IMMEDIATE] wake naming the lane.
#
# Defensive: missing/unparseable lanes.json OR no jq/br → log a WARN, skip the check
# (KNOWN_READY_LANE stays empty), never crash the health pass.
LANES_FILE="$PROJ/.harmonik/context/lanes.json"
KNOWN_READY_LANE=""        # first candidate lane name with >=1 ready bead (empty = none)
KNOWN_READY_LANE_EPIC=""   # its epic_id
KNOWN_READY_LANE_COUNT=0   # its ready-bead count
if [[ -f "$LANES_FILE" ]]; then
  if ! command -v jq >/dev/null 2>&1; then
    echo "ops-monitor-check: WARN — jq not found; skipping known-ready-lane check (SD-1)" >&2
  elif ! command -v br >/dev/null 2>&1; then
    echo "ops-monitor-check: WARN — br not found; skipping known-ready-lane check (SD-1)" >&2
  elif ! jq -e . "$LANES_FILE" >/dev/null 2>&1; then
    echo "ops-monitor-check: WARN — lanes.json missing/unparseable; skipping known-ready-lane check (SD-1)" >&2
  else
    # Emit "lane<TAB>epic_id" for each candidate lane: non-null epic_id AND gate is
    # null OR its expires is absent OR expired (gate.expires < now). gate.expires may be
    # DATE-ONLY (the live index writes "2026-07-09") OR full RFC3339 ("2026-07-09T00:00:00Z");
    # fromdateiso8601 needs the full form, so try (expires + "T00:00:00Z") FIRST (parses a
    # date-only value), fall back to the raw value (parses an already-full RFC3339), and
    # only if BOTH fail (missing/null/malformed) default to 0 == past == candidate (the
    # safe LAPSE->autonomous default). A well-formed FUTURE gate in either format EXCLUDES
    # the lane; only a genuinely past or unparseable expires INCLUDES it.
    _LANE_CANDIDATES=$(jq -r --argjson now "$TS_EPOCH" '
      (.lanes // [])[]
      | select(.epic_id != null)
      | select(
          (.gate == null)
          or ((.gate.expires // null) == null)
          or (
              ( ((.gate.expires + "T00:00:00Z") | fromdateiso8601?)
                // (.gate.expires | fromdateiso8601?)
                // 0 ) < $now
            )
        )
      | "\(.lane)\t\(.epic_id)"
    ' "$LANES_FILE" 2>/dev/null) || _LANE_CANDIDATES=""
    while IFS=$'\t' read -r _lane _epic; do
      [[ -z "$_lane" || -z "$_epic" ]] && continue
      _PARENT_JSON=$( (cd "$PROJ" && br ready --parent "$_epic" --limit 0 --json) 2>/dev/null ) || _PARENT_JSON=""
      [[ -z "$_PARENT_JSON" ]] && continue
      _CNT=$(printf '%s' "$_PARENT_JSON" | py3 "
import sys, json
try:
    d = json.load(sys.stdin)
    if isinstance(d, dict):
        items = d.get('issues') or d.get('ready') or d.get('beads') or []
    elif isinstance(d, list):
        items = d
    else:
        items = []
    print(len(items))
except Exception:
    print(0)
" 2>/dev/null || echo 0)
      if [[ "$_CNT" =~ ^[0-9]+$ ]] && (( _CNT >= 1 )); then
        KNOWN_READY_LANE="$_lane"
        KNOWN_READY_LANE_EPIC="$_epic"
        KNOWN_READY_LANE_COUNT="$_CNT"
        break
      fi
    done <<< "$_LANE_CANDIDATES"
  fi
fi

# ── Check events for idle-fleet ───────────────────────────────────────────────

LAST_RUN_EVENT_TS=0
if [[ -f "$EVENTS_FILE" ]]; then
  # Tail last 500 lines to find recent run events efficiently
  LAST_RUN_EVENT_TS=$(tail -500 "$EVENTS_FILE" 2>/dev/null | py3 "
import sys, json
last_ts = 0
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        ev = json.loads(line)
        etype = ev.get('type', '') or ev.get('event_type', '')
        if etype in ('run_started', 'run_completed', 'run_failed', 'merge_completed'):
            ts = ev.get('ts') or ev.get('timestamp') or ''
            if ts:
                # parse ISO8601 to epoch (no dateutil; use basic parsing)
                import datetime
                ts_clean = ts[:19].replace('T', ' ')
                try:
                    dt = datetime.datetime.strptime(ts_clean, '%Y-%m-%d %H:%M:%S')
                    epoch = int(dt.replace(tzinfo=datetime.timezone.utc).timestamp())
                    if epoch > last_ts:
                        last_ts = epoch
                except Exception:
                    pass
    except Exception:
        pass
print(last_ts)
" 2>/dev/null || echo 0)
fi

# ── Capture keeper process list (keeper-coverage check) ──────────────────────
# One ps scan captures all harmonik keeper cmdlines; passed into the python block.
# macOS pgrep -a/-l cannot inspect args; ps -axo command= is portable and avoids
# N per-crew shell-outs (hk-hnk4j).
KEEPER_PROCS_RAW=$(ps -axo command= 2>/dev/null | grep -F "harmonik keeper --agent" || true)

# ── Check release-due: unreleased commits since last vX.Y.Z tag ──────────────
# Soft prompt only — never tags or releases anything; captain decides.
# Degrade gracefully: git/gh errors → count=0 / CI_STATUS=unknown → no signal.
RELEASE_COMMIT_COUNT=0
CI_STATUS=unknown
_LAST_TAG=""
_LAST_TAG=$(git -C "$PROJ" tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname 2>/dev/null | head -1) || true
if [[ -n "$_LAST_TAG" ]]; then
  RELEASE_COMMIT_COUNT=$(git -C "$PROJ" rev-list --count "${_LAST_TAG}..origin/main" 2>/dev/null || echo 0)
else
  RELEASE_COMMIT_COUNT=$(git -C "$PROJ" rev-list --count origin/main 2>/dev/null || echo 0)
fi
[[ "$RELEASE_COMMIT_COUNT" =~ ^[0-9]+$ ]] || RELEASE_COMMIT_COUNT=0
if command -v gh >/dev/null 2>&1; then
  _GH_OUT=""
  _GH_OUT=$( (cd "$PROJ" && gh run list --branch main --limit 1 --json conclusion 2>/dev/null) ) || true
  _GH_CONCLUSION=""
  _GH_CONCLUSION=$(printf '%s' "$_GH_OUT" | python3 -c "
import json, sys
try:
    items = json.load(sys.stdin)
    print(items[0].get('conclusion', '') if items else '')
except Exception:
    print('')
" 2>/dev/null) || true
  if [[ "$_GH_CONCLUSION" == "success" ]]; then
    CI_STATUS=green
  elif [[ -n "$_GH_CONCLUSION" ]]; then
    CI_STATUS=not-green
  fi
fi

# ── Python analysis: produce JSON snapshot ────────────────────────────────────

ANALYSIS=$(py3 "
import json, sys, os, datetime, fnmatch

proj               = '$PROJ'
ts                 = '$TS'
ts_epoch           = int('$TS_EPOCH')
daemon_up          = '$DAEMON_UP' == 'true'
supervisor_up      = '$SUPERVISOR_UP' == 'true'
captain_tmux_alive      = '$CAPTAIN_TMUX_ALIVE' == 'true'
watch_tmux_alive        = '$WATCH_TMUX_ALIVE' == 'true'
watch_opsmonitor_target = '$WATCH_OPSMONITOR_TARGET'
captain_absent_thresh = int('$CAPTAIN_ABSENT_THRESHOLD')
stale_thresh            = int('$STALE_THRESHOLD')
miss_limit              = int('$MISS_LIMIT')
idle_thresh             = int('$IDLE_THRESHOLD')
cooldown                = int('$DIGEST_COOLDOWN')
crew_msg_active_window  = int('$CREW_MSG_ACTIVE_WINDOW')
last_run_ts             = int('$LAST_RUN_EVENT_TS')
prev_misses             = json.loads('$PREV_STALE_MISSES')
prev_digest             = int('$PREV_LAST_DIGEST')
prev_alerted            = json.loads('''$PREV_ALERTED_IMMEDIATE''')
immediate_cooldown          = int('$IMMEDIATE_COOLDOWN')
critical_immediate_cooldown = int('$CRITICAL_IMMEDIATE_COOLDOWN')
persistent_immediate_cooldown = int('$PERSISTENT_IMMEDIATE_COOLDOWN')
ops_critical_count   = int('$OPS_CRITICAL_COUNT')
ops_critical_elapsed = int('$OPS_CRITICAL_ELAPSED')
inert_suppress     = json.loads('''$INERT_SUPPRESS_JSON''')
live_allow         = json.loads('''$LIVE_ALLOW_JSON''')
comms_raw          = '''$COMMS_WHO_NDJSON'''
qlist_raw          = '''$QUEUE_LIST_JSON'''
ready_count        = int('$READY_COUNT')
review_window      = int('$REVIEW_GATE_WINDOW')
review_grace       = int('$REVIEW_GATE_GRACE')
reviewer_stale_window = int('$REVIEWER_STALE_WINDOW')
keeper_procs_raw      = '''$KEEPER_PROCS_RAW'''
prev_keeper_misses    = json.loads('$PREV_KEEPER_COVERAGE_MISSES')
release_commit_count  = int('$RELEASE_COMMIT_COUNT')
ci_status             = '$CI_STATUS'
release_due_threshold = int('$RELEASE_DUE_COMMIT_THRESHOLD')
ops_critical_cooldown = int('$OPS_CRITICAL_COOLDOWN')
watch_absent_thresh   = int('$WATCH_ABSENT_THRESHOLD')
watch_stall_ticks     = int('$WATCH_STALL_TICKS')
prev_watch_cursor     = '$PREV_WATCH_CURSOR'
prev_watch_stall_misses = int('$PREV_WATCH_STALL_MISSES')
watch_cursor          = '$WATCH_CURSOR'
# hk-7o4i0: flap-retention grace + deploy/restart suppression inputs.
watch_session_created = int('$WATCH_SESSION_CREATED')
watch_restart_suppress_window = int('$WATCH_RESTART_SUPPRESS_WINDOW')
watch_stall_flap_grace = int('$WATCH_STALL_FLAP_GRACE')
# SD-1: first KNOWN ready lane (non-null epic, ungated/expired-gate, >=1 ready bead).
# Empty lane name == no known-ready lane this tick (the predicate is false).
known_ready_lane       = '''$KNOWN_READY_LANE'''
known_ready_lane_epic  = '''$KNOWN_READY_LANE_EPIC'''
known_ready_lane_count = int('$KNOWN_READY_LANE_COUNT')

# ── Parse events.jsonl for recent agent_message activity ────────────────────
# Used as a presence fallback: comms send does NOT refresh the presence
# heartbeat, so an agent posting status every ~12m while presence is stale
# (no re-join beat after keeper restart) fires a false crew-stale alert. We
# suppress it if the sender has an agent_message within crew_msg_active_window
# (900s / 15m) — large enough to cover one full captain posting cadence plus
# buffer (hk-gu3v).
import os as _os
last_msg_ts = {}  # agent_name -> epoch of most recent agent_message
# Review-gate (M2 code-half; R6 fix): a run is review-BYPASSED only if it actually
# ENTERED a review path (emitted reviewer_launched) yet completed without APPROVE.
# Runs that auto-closed / made no change / were subsumed never launch a reviewer (by
# design — workloop.go ~:3811) so they must NOT be flagged. We therefore join
# reviewer_launched -> reviewer_verdict{APPROVE} by run_id, not bare run_completed.
reviewer_launched_ts = {}  # run_id -> epoch of the reviewer_launched event (review entered)
verdict_run_ids      = set() # run_ids that have any reviewer_verdict event
approving_verdict_run_ids = set() # run_ids that have reviewer_verdict{APPROVE}
# Codex-DOT path (hk-346mi): single-reviewer DOT (harness=codex + reviewer_harness=claude-code)
# closes the bead via CHB protocol (outcome_emitted kind=approved + bead_closed) rather than
# emitting reviewer_launched / reviewer_verdict. outcome_emitted(kind=approved) is the daemon
# merge-sequence event and confirms the review path was honored. The review-gate check skips
# any run_id present in this set (same treatment as approving_verdict_run_ids).
outcome_approved_run_ids = set() # run_ids with outcome_emitted{kind=approved}
failed_run_ids       = set() # run_ids whose terminal event is run_failed (merged nothing)
completed_run_ts     = {} # run_id -> epoch of the most-recent run_completed event
completed_run_seq    = {} # run_id -> event-order index of most-recent run_completed event
# Short-circuit signal (hk-orni / hk-2vpj follow-up): a run that REQUESTED a review node
# (node_dispatch_requested with node_id starting 'review' — review / review_correctness /
# review_design / review_tests / reviewer) but for which the reviewer never launched (no
# reviewer_launched event). On live data this cleanly separates engine-short-circuited
# reviewers (8 run_ids) from legitimately review-less auto-closes (0 of 181 request a
# review node). node_id lives under .payload; run_id is top-level.
review_requested_ts  = {}  # run_id -> epoch of the earliest review-node dispatch request
run_last_event_ts    = {}  # run_id -> epoch of the most-recent event seen for that run (any type)
run_last_event_type  = {}  # run_id -> event type of the most-recent event (used to detect active reviewer)
latest_review_event_ts      = {} # run_id -> epoch of latest reviewer_launched/reviewer_verdict
latest_review_event_seq     = {} # run_id -> event-order index of latest reviewer_launched/reviewer_verdict
latest_review_event_type    = {} # run_id -> reviewer_launched|reviewer_verdict
latest_review_event_verdict = {} # run_id -> verdict string for reviewer_verdict, else ''

def _ev_epoch(_tw, _dt):
    _ts_clean = _tw[:19].replace('T', ' ')
    _d = _dt.datetime.strptime(_ts_clean, '%Y-%m-%d %H:%M:%S')
    return int(_d.replace(tzinfo=_dt.timezone.utc).timestamp())

# hk-7o4i0 Fix B: most-recent daemon (re)start marker epoch (0 = none in window).
daemon_restart_ts = 0
# WE9: cursor-advancement tracking (initialised before events loop; updated inside)
latest_event_id = ''  # last event_id seen in the scan window
_cursor_seq = -1      # _event_seq where watch_cursor was found; -1 = not in window
events_past_cursor = False
# WE9 / hk-ohb8p Fix A: actionable-event tracking for the cursor-stall check.
# A frozen cursor while only BENIGN churn (run lifecycle, heartbeats, bead create/
# close, agent_message) flows past it is the watch CORRECTLY declining to escalate a
# healthy fleet — NOT a stall. watch-stalled must fire only when escalation-WORTHY
# work is accumulating past the cursor and the watch is ignoring it. We reuse the
# watch's own notion of "actionable" — the failure / halt / decision / safety event
# classes the watch exists to escalate — rather than inventing a new taxonomy.
#   _last_actionable_seq — _event_seq of the LAST actionable event in the window
#                          (-1 = none). Compared to _cursor_seq below: an actionable
#                          event AFTER the cursor means the watch is sitting on real work.
_last_actionable_seq = -1
actionable_events_past_cursor = False
# Escalation-worthy event types (the watch's escalation surface). Deliberately EXCLUDES
# routine churn: run_started / run_completed / agent_message / agent_heartbeat /
# agent_ready / bead_closed / queue_* / node_dispatch_* / metric / *_completed lifecycle.
ACTIONABLE_EVENT_TYPES = frozenset((
    'run_failed', 'run_stale', 'run_canceled',
    'agent_failed', 'agent_ready_timeout', 'agent_warning_silent_hang',
    'post_agent_ready_hang', 'launch_stall_detected', 'tmux_new_window_timeout',
    'liveness_halt', 'no_progress_detected', 'loop_observed_phantom_done',
    'worker_unhealthy', 'worker_offline', 'worker_tunnel_failed',
    'merge_build_failed', 'merge_conflict_escalation',
    'review_bypassed', 'review_fixup_stalled', 'review_gate_anomaly',
    'reviewer_budget_exceeded', 'verdict_envelope_mismatch',
    'spawn_cap_blocked', 'budget_exhausted', 'disk_low',
    'infrastructure_unavailable', 'daemon_degraded', 'daemon_startup_failed',
    'resource_breach', 'bus_overflow', 'governor_signal', 'liveness_violated',
    'operator_escalation_required', 'gate_escalated',
    'decision_required', 'decision_needed',
    'store_divergence_detected', 'bead_ledger_corrupt', 'bead_sync_failed',
    'session_keeper_watcher_dead', 'session_keeper_blind', 'session_keeper_hard_ceiling',
    'session_keeper_cycle_aborted',
))

_events_path = _os.path.join(proj, '.harmonik', 'events', 'events.jsonl')
if _os.path.isfile(_events_path):
    import datetime as _dt
    try:
        with open(_events_path, 'rb') as _ef:
            _ef.seek(0, 2)
            _file_size = _ef.tell()
            _read_start = max(0, _file_size - 256 * 1024)  # last 256 KB
            _ef.seek(_read_start)
            _raw = _ef.read()
        _event_seq = 0
        for _line in _raw.decode('utf-8', errors='replace').splitlines():
            _line = _line.strip()
            if not _line:
                continue
            _event_seq += 1
            try:
                _ev = json.loads(_line)
                _etype = _ev.get('type', '')
                _payload = _ev.get('payload', {})
                if isinstance(_payload, str):
                    try:
                        _payload = json.loads(_payload)
                    except Exception:
                        _payload = {}

                if _etype == 'agent_message':
                    _tw = _ev.get('timestamp_wall', '')
                    if not _tw:
                        continue
                    _epoch = _ev_epoch(_tw, _dt)
                    _sender = _payload.get('from', '')
                    if _sender and _epoch > last_msg_ts.get(_sender, 0):
                        last_msg_ts[_sender] = _epoch

                elif _etype == 'reviewer_launched':
                    # A reviewer/DOT review node WAS started for this run_id. run_id is
                    # top-level (and mirrored in payload); epoch from timestamp_wall.
                    _rid = _ev.get('run_id') or (_payload.get('run_id') if isinstance(_payload, dict) else '') or ''
                    _tw = _ev.get('timestamp_wall', '')
                    if _rid and _tw:
                        _e = _ev_epoch(_tw, _dt)
                        reviewer_launched_ts[_rid] = _e
                        if _e >= latest_review_event_ts.get(_rid, 0):
                            latest_review_event_ts[_rid] = _e
                            latest_review_event_seq[_rid] = _event_seq
                            latest_review_event_type[_rid] = 'reviewer_launched'
                            latest_review_event_verdict[_rid] = ''

                elif _etype == 'reviewer_verdict':
                    # reviewer_verdict carries run_id at top level AND in payload.
                    _rid = _ev.get('run_id') or _payload.get('run_id') or ''
                    if _rid:
                        verdict_run_ids.add(_rid)
                        _verdict = _payload.get('verdict', '') if isinstance(_payload, dict) else ''
                        if _verdict == 'APPROVE':
                            approving_verdict_run_ids.add(_rid)
                        _tw = _ev.get('timestamp_wall', '')
                        if _tw:
                            _e = _ev_epoch(_tw, _dt)
                            if _e >= latest_review_event_ts.get(_rid, 0):
                                latest_review_event_ts[_rid] = _e
                                latest_review_event_seq[_rid] = _event_seq
                                latest_review_event_type[_rid] = 'reviewer_verdict'
                                latest_review_event_verdict[_rid] = _verdict

                elif _etype == 'run_failed':
                    # run_failed = terminal failure; the run merged nothing. Collect so the
                    # review-gate can suppress false-positive bypass alerts on failed runs
                    # (e.g. reviewer_budget_exceeded / 'verdict absent').
                    _rid = _ev.get('run_id') or (_payload.get('run_id') if isinstance(_payload, dict) else '') or ''
                    if _rid:
                        failed_run_ids.add(_rid)

                elif _etype == 'outcome_emitted':
                    # Codex-DOT path (hk-346mi): single-reviewer DOT with reviewer_harness=claude-code
                    # closes the bead via CHB protocol, emitting outcome_emitted(kind=approved) without
                    # ever emitting reviewer_launched / reviewer_verdict. Capture run_ids here; the
                    # review-gate check treats review_requested_ts + outcome_approved_run_ids as reviewed.
                    # Note: run_id lives in the payload (bus.Emit with no run_id in the envelope).
                    _rid = _ev.get('run_id') or (_payload.get('run_id') if isinstance(_payload, dict) else '') or ''
                    if _rid:
                        _kind = _payload.get('kind', '') if isinstance(_payload, dict) else ''
                        if _kind == 'approved':
                            outcome_approved_run_ids.add(_rid)

                elif _etype == 'run_completed':
                    # Judge review-bypass only after the daemon says the run completed.
                    # In DOT triple-review, REQUEST_CHANGES relaunches implementation/review;
                    # until a later run_completed arrives, the review loop is still active.
                    _rid = _ev.get('run_id') or (_payload.get('run_id') if isinstance(_payload, dict) else '') or ''
                    _tw = _ev.get('timestamp_wall', '')
                    if _rid and _tw:
                        _e = _ev_epoch(_tw, _dt)
                        if _e >= completed_run_ts.get(_rid, 0):
                            completed_run_ts[_rid] = _e
                            completed_run_seq[_rid] = _event_seq

                elif _etype in ('daemon_started', 'daemon_ready'):
                    # hk-7o4i0 Fix B: a daemon (re)start marker. Used to suppress
                    # watch-stalled during the post-deploy boot window — the watch's cursor
                    # is frozen because it is coming up with the daemon, not because it is
                    # ignoring escalation-worthy work.
                    _tw = _ev.get('timestamp_wall', '')
                    if _tw:
                        _e = _ev_epoch(_tw, _dt)
                        if _e > daemon_restart_ts:
                            daemon_restart_ts = _e

                elif _etype == 'node_dispatch_requested':
                    # The engine requested a node for this run. node_id is under .payload.
                    # Only review-node requests matter here (prefix 'review' catches review,
                    # review_correctness, review_design, review_tests, reviewer — and NOT
                    # close/commit_gate/implement/start/consolidate/finalize).
                    _nid = _payload.get('node_id', '') if isinstance(_payload, dict) else ''
                    if isinstance(_nid, str) and _nid.startswith('review'):
                        _rid = _ev.get('run_id') or (_payload.get('run_id') if isinstance(_payload, dict) else '') or ''
                        _tw = _ev.get('timestamp_wall', '')
                        if _rid and _tw:
                            _e = _ev_epoch(_tw, _dt)
                            # keep the EARLIEST review-request epoch for the run
                            if _rid not in review_requested_ts or _e < review_requested_ts[_rid]:
                                review_requested_ts[_rid] = _e

                # Track the most-recent event timestamp AND type per run_id (any event type).
                # Timestamp used to detect abandoned reviewers (hk-usz0).
                # Type used to detect actively-running reviewers: if reviewer_launched is
                # the most-recent event and no verdict has arrived, the reviewer is still
                # working — suppress the bypass alert (hk-oytx).
                _any_rid = _ev.get('run_id') or (_payload.get('run_id') if isinstance(_payload, dict) else '') or ''
                _any_tw = _ev.get('timestamp_wall', '')
                if _any_rid and _any_tw:
                    _any_e = _ev_epoch(_any_tw, _dt)
                    if _any_e >= run_last_event_ts.get(_any_rid, 0):
                        run_last_event_ts[_any_rid] = _any_e
                        run_last_event_type[_any_rid] = _etype

                # WE9: track latest event_id and cursor position for cursor-advancement check
                _eid_any = _ev.get('event_id', '') or ''
                if _eid_any:
                    latest_event_id = _eid_any
                    if _eid_any == watch_cursor:
                        _cursor_seq = _event_seq
                # hk-ohb8p Fix A: record the position of the last escalation-worthy event.
                if _etype in ACTIONABLE_EVENT_TYPES:
                    _last_actionable_seq = _event_seq
            except Exception:
                pass
        # WE9: compute events_past_cursor after scanning the window
        if watch_cursor:
            if _cursor_seq >= 0:
                # cursor found in window; events past it if any were processed after
                events_past_cursor = _event_seq > _cursor_seq
                # hk-ohb8p Fix A: of those, is any ESCALATION-WORTHY? Only an actionable
                # event strictly after the cursor means the watch is ignoring real work.
                actionable_events_past_cursor = _last_actionable_seq > _cursor_seq
            else:
                # cursor not in last 256KB: either too old (events past it) or file empty
                events_past_cursor = bool(latest_event_id and latest_event_id != watch_cursor)
                # Cursor scrolled out of the 256KB window. If there is ANY actionable
                # event in the visible window, the watch is behind on escalation-worthy
                # work; benign-only churn past an out-of-window cursor is not a stall.
                actionable_events_past_cursor = events_past_cursor and _last_actionable_seq >= 0
    except Exception:
        pass

# ── Review-gate: runs that ENTERED-OR-REQUESTED review (window, past grace) w/o approval ──
# A run is review-BYPASSED if it either (a) emitted reviewer_launched (entered a review
# path) OR (b) requested a review node via node_dispatch_requested node_id=review* (the
# engine asked for review) — yet completed with NO matching APPROVE verdict. Case (b) catches the
# hk-2vpj engine-short-circuit class: the engine REQUESTS a review node but the reviewer
# never launches, so no reviewer_launched event exists and the change merges UNREVIEWED.
# Auto-close / noChange / subsumed runs neither launch nor request a reviewer, so they are
# absent from both maps and cannot be flagged (R6 suppression preserved, hk-ayvx).
# We take the UNION of the launched and review-requested run_ids, key each by the EARLIEST
# review event (launch or request) for the grace check, window over the most-recent such
# run_ids, and skip any younger than review_grace — its verdict may still be in flight.
# MEDIUM (multi-iteration DOT): launch/request and reviewer_verdict are joined on the SAME
# run_id; only a terminal APPROVE clears the run. A REQUEST_CHANGES verdict remains in-flight
# unless a subsequent run_completed arrives without an approving verdict. A
# terminal-close run_id that never launched OR requested a reviewer is in neither map, so
# unmatched 'dot: reached terminal node close' run_ids never false-flag.
_review_anchor_ts = {}  # run_id -> earliest review-related epoch (launch or request)
for _rid, _e in reviewer_launched_ts.items():
    if _rid not in _review_anchor_ts or _e < _review_anchor_ts[_rid]:
        _review_anchor_ts[_rid] = _e
for _rid, _e in review_requested_ts.items():
    if _rid not in _review_anchor_ts or _e < _review_anchor_ts[_rid]:
        _review_anchor_ts[_rid] = _e
_recent_review = sorted(_review_anchor_ts.items(), key=lambda kv: kv[1])
_recent_review = _recent_review[-review_window:]
review_bypass_run_ids = []
for _rid, _lts in _recent_review:
    if (ts_epoch - _lts) < review_grace:
        continue  # too young to judge — verdict may still arrive
    if _rid in failed_run_ids:
        continue  # run_failed terminal: merged nothing, cannot be a bypass
    if _rid in approving_verdict_run_ids:
        continue  # terminal approving verdict exists: reviewed
    if _rid in outcome_approved_run_ids:
        continue  # approved via merge-outcome (codex single-reviewer DOT path — no reviewer_verdict emitted)
    # Judge only completed runs. A latest reviewer_launched OR REQUEST_CHANGES verdict
    # without a subsequent run_completed is an active review/fixup loop, not a bypass.
    _completed_ts = completed_run_ts.get(_rid, 0)
    _latest_review_ts = latest_review_event_ts.get(_rid, _lts)
    _completed_seq = completed_run_seq.get(_rid, 0)
    _latest_review_seq = latest_review_event_seq.get(_rid, 0)
    if not _completed_ts:
        continue
    if _completed_seq and _latest_review_seq:
        if _completed_seq <= _latest_review_seq:
            continue
    elif _completed_ts < _latest_review_ts:
        continue
    # Stale/abandoned reviewer: reviewer_launched but no verdict AND no event for
    # reviewer_stale_window seconds — the reviewer session died without a terminal
    # event (no run_failed, no reviewer_verdict). Not a merge-safety hole; suppress
    # to avoid alert-fatigue on IMMEDIATE (hk-usz0, follow-up to hk-ijtw).
    _last_ev_ts = run_last_event_ts.get(_rid, _lts)
    if (ts_epoch - _last_ev_ts) > reviewer_stale_window and _rid not in verdict_run_ids:
        continue  # abandoned reviewer: silent past stale window, merged nothing
    # Active reviewer: reviewer_launched is the most-recent event for this run and
    # the reviewer is still within the stale window — the reviewer session is in
    # progress, no verdict yet. The run cannot have merged (run_completed / merge_completed
    # would have updated run_last_event_type). Suppresses cry-wolf IMMEDIATE alerts
    # that fire during a long review (>180s grace but <3600s stale; hk-oytx).
    if run_last_event_type.get(_rid) == 'reviewer_launched' and _rid not in verdict_run_ids:
        continue  # reviewer actively working — verdict pending; not a bypass
    if _rid not in approving_verdict_run_ids:
        review_bypass_run_ids.append(_rid)

# ── Parse comms who ──────────────────────────────────────────────────────────
online_crews = {}   # crew_name -> last_seen_epoch
crew_status  = {}   # crew_name -> {last_seen_s, stale, online}
for line in comms_raw.strip().splitlines():
    line = line.strip()
    if not line:
        continue
    try:
        rec = json.loads(line)
        name = rec.get('agent', '')
        last_seen_str = rec.get('last_seen', '')
        if name and last_seen_str:
            ts_clean = last_seen_str[:19].replace('T', ' ')
            import datetime as dt_mod
            try:
                d = dt_mod.datetime.strptime(ts_clean, '%Y-%m-%d %H:%M:%S')
                epoch = int(d.replace(tzinfo=dt_mod.timezone.utc).timestamp())
                age = ts_epoch - epoch
                crew_status[name] = {
                    'last_seen_s': age,
                    'stale': age > stale_thresh,
                    'online': rec.get('status', '') == 'online'
                }
                if rec.get('status', '') == 'online':
                    online_crews[name] = epoch
            except Exception:
                pass
    except Exception:
        pass

# ── Parse queue list ─────────────────────────────────────────────────────────
queues = []
max_concurrent = 0
paused_queues  = []
ready_unstaffed = []

if daemon_up and qlist_raw.strip().startswith('{'):
    try:
        qdata = json.loads(qlist_raw)
        max_concurrent = qdata.get('max_concurrent', 0)
        for q in qdata.get('queues', []):
            qname   = q.get('name', '')
            qstatus = q.get('status', '')
            workers = q.get('workers', 0)
            pending = q.get('pending_items', 0)
            failed  = q.get('failed_items', 0)
            queues.append({'name': qname, 'status': qstatus, 'workers': workers,
                           'pending_items': pending, 'failed_items': failed})

            # Paused signal: alert only when queue is NOT inert AND its crew is
            # online (or the queue is in the explicit live-allow list).
            if qstatus == 'paused-by-failure':
                is_inert = any(fnmatch.fnmatch(qname, pat) for pat in inert_suppress)
                if not is_inert:
                    crew_guess    = qname[:-2] if qname.endswith('-q') else qname
                    is_crew_online = crew_guess in online_crews
                    is_live_allow  = qname in live_allow
                    if is_crew_online or is_live_allow:
                        paused_queues.append(qname)

            # Ready-unstaffed: pending items but workers==0 and crew not online
            if pending > 0 and workers == 0 and qstatus not in ('paused-by-failure', 'paused-by-drain'):
                crew_guess = qname[:-2] if qname.endswith('-q') else None
                if crew_guess and crew_guess not in online_crews:
                    ready_unstaffed.append(qname)
    except Exception as e:
        pass

# ── Active workers (for idle check) ─────────────────────────────────────────
total_workers = sum(q['workers'] for q in queues)

# ── Crew staleness with consecutive miss tracking ────────────────────────────
# Names that are NOT fleet crews — excluded from this loop to prevent bogus
# crew-stale signals. captain is covered by captain-up; the service agents
# (ops-monitor, ctx-watchdog, daemon, operator) are never crews.
NON_CREW = {'captain', 'ops-monitor', 'ctx-watchdog', 'daemon', 'operator', 'watch'}
new_misses = {}
stale_signal_crews = []
for name, info in crew_status.items():
    if name in NON_CREW:
        continue
    miss_count = prev_misses.get(name, 0)
    # comms send does NOT refresh presence, so fall back to agent_message
    # recency: if the agent posted a message within crew_msg_active_window
    # (900s), treat as active even when presence last_seen is stale (hk-gu3v).
    # 150s (stale_thresh) is too narrow — a captain posting every ~12m has
    # messages 500–700s old at check time, which still exceeded 150s.
    effective_stale = info['stale']
    if effective_stale:
        _msg_ts = last_msg_ts.get(name, 0)
        if _msg_ts and (ts_epoch - _msg_ts) <= crew_msg_active_window:
            effective_stale = False
            info['msg_override'] = ts_epoch - _msg_ts  # age of most recent msg, for snapshot
    if effective_stale:
        miss_count += 1
        new_misses[name] = miss_count
        if miss_count >= miss_limit:
            stale_signal_crews.append({'crew': name, 'last_seen_s': info['last_seen_s'], 'misses': miss_count})
    else:
        new_misses[name] = 0  # reset on recovery

# ── Keeper coverage check (hk-hnk4j) ────────────────────────────────────────
# Parse live keeper process cmdlines (ps -axo command= | grep -F 'harmonik keeper --agent').
# Token following --agent gives the watched agent name. One scan; no per-crew shell-outs.
live_keeper_agents = set()
for _kline in keeper_procs_raw.strip().splitlines():
    _kline = _kline.strip()
    if 'harmonik keeper --agent' not in _kline:
        continue
    _parts = _kline.split()
    for _ki, _ktok in enumerate(_parts):
        if _ktok == '--agent' and _ki + 1 < len(_parts):
            live_keeper_agents.add(_parts[_ki + 1])
            break

# For each online crew (NON_CREW excluded — keeper coverage is for CREWS; captain's own
# keeper is out of scope), debounce consecutive misses before signalling (cry-wolf guard
# analogous to stale_crew_misses, same miss_limit).
new_keeper_misses = {}
keeper_missing_crews = []
if daemon_up:  # when daemon is down comms data is empty; skip (daemon-down already alerts)
    for _kname in online_crews:
        if _kname in NON_CREW:
            continue
        _kmiss = prev_keeper_misses.get(_kname, 0)
        if _kname not in live_keeper_agents:
            _kmiss += 1
            new_keeper_misses[_kname] = _kmiss
            if _kmiss >= miss_limit:
                keeper_missing_crews.append(_kname)
        else:
            new_keeper_misses[_kname] = 0  # reset on keeper reappearance

# ── Single-mode check ────────────────────────────────────────────────────────
single_mode = daemon_up and max_concurrent == 1

# ── Idle-fleet check ─────────────────────────────────────────────────────────
idle_fleet = False
idle_age_s = 0
if daemon_up and total_workers == 0:
    if last_run_ts > 0:
        idle_age_s = ts_epoch - last_run_ts
        if idle_age_s > idle_thresh:
            idle_fleet = True
    # No events at all = unknown; don't signal (avoids false positive on fresh project)

# ── Captain liveness (captain-up check) ─────────────────────────────────────
# captain_present: the captain has a comms-who record within captain_absent_thresh
# AND is online. Apply the same agent_message fallback the crew-staleness loop uses.
# When the daemon is down, comms data is unavailable (crew_status empty) — rely on
# the tmux probe alone; if tmux is alive, the captain is up.
captain_info = crew_status.get('captain')
captain_present = False
if captain_info:
    _cap_stale = captain_info['last_seen_s'] > captain_absent_thresh
    if _cap_stale:
        _cap_msg_ts = last_msg_ts.get('captain', 0)
        if _cap_msg_ts and (ts_epoch - _cap_msg_ts) <= crew_msg_active_window:
            _cap_stale = False
    captain_present = captain_info['online'] and not _cap_stale
# captain_down requires BOTH comms absence/staleness AND no tmux session.
# If the tmux session is alive, captain is UP regardless of stale comms presence.
captain_down = not captain_present and not captain_tmux_alive

# ── WE9: watch comms presence + dual-probe ────────────────────────────────────
# watch_present: mirror of captain_present above (:758-770). Apply the same
# agent_message fallback: comms send does NOT refresh presence, so a watch posting
# within crew_msg_active_window is treated as present even when last_seen is stale.
watch_info = crew_status.get('watch')
watch_present = False
if watch_info:
    _watch_stale = watch_info['last_seen_s'] > watch_absent_thresh
    if _watch_stale:
        _watch_msg_ts = last_msg_ts.get('watch', 0)
        if _watch_msg_ts and (ts_epoch - _watch_msg_ts) <= crew_msg_active_window:
            _watch_stale = False
    # hk-8yh32.3 P5a: use absent_thresh directly (not the 120s comms TTL). The watch
    # beats comms presence every 270s via ScheduleWakeup; gating on 'online' (120s TTL)
    # marks it absent for the 150s gap between t+120 and t+270, causing false watch-down
    # between beats. Use the operator-configured watch_absent_thresh (must be > 270s) as
    # the sole staleness gate so the watch reads as present across its full beat cycle.
    watch_present = not _watch_stale
# hk-8yh32.3 P5b: compute restart-suppressed early so watch_down can also suppress
# during the post-join boot warmup window. Previously only watch_stalled used this;
# extending it to watch_down eliminates the one-tick false-positive at boot when the
# watch session has not yet joined comms (tmux session absent or fresh).
watch_recently_restarted = (
    watch_session_created > 0
    and 0 <= ts_epoch - watch_session_created <= watch_restart_suppress_window)
daemon_recently_restarted = (
    daemon_restart_ts > 0
    and 0 <= ts_epoch - daemon_restart_ts <= watch_restart_suppress_window)
watch_restart_suppressed = watch_recently_restarted or daemon_recently_restarted
# watch_down requires BOTH comms absence AND no tmux session (WE9 dual-probe, mirror of
# captain_down :773). A process pinned on the bus still refreshes last_seen while
# buffering every escalation — last_seen alone is insufficient (design §5).
# Only fires when watch is the routing target (opsmonitor_target != 'captain').
# hk-8yh32.3 P5b: additionally suppress during post-join boot warmup window so a
# one-tick gap at deploy time does not generate false watch-down immediates.
watch_down = (watch_opsmonitor_target != 'captain' and not watch_present
              and not watch_tmux_alive and not watch_restart_suppressed)

# ── WE9: cursor-advancement stall check ─────────────────────────────────────
# A watch alive but not advancing its escalation cursor while ESCALATION-WORTHY
# events accumulate → ops-monitor IMMEDIATE 'watch-stalled' after watch_stall_ticks
# consecutive ticks. Reuses the stale_crew_misses/prev_misses pattern (:692, :369).
#
# hk-ohb8p Fix A: the gate is actionable_events_past_cursor, NOT events_past_cursor.
# On a QUIET fleet the watch's escalation cursor legitimately stays frozen (there is
# nothing actionable to escalate) while benign backlog churn — bead create/close and
# routine run lifecycle (run_started / run_completed / agent_message / heartbeats) —
# keeps pushing events past the cursor. Gating on bare events_past_cursor fired an
# every-tick (~5 min) watch-stalled IMMEDIATE on a HEALTHY fleet (54% of bus traffic
# in the worst incident). We now only count the stall when at least one escalation-
# worthy event (failure / halt / decision / safety class — see ACTIONABLE_EVENT_TYPES)
# is sitting past the cursor unescalated. Benign churn no longer trips the counter.
new_watch_stall_misses = 0
watch_stalled = False
# hk-7o4i0 Fix B / hk-8yh32.3 P5b: deploy/restart suppression. watch_restart_suppressed
# is computed above (shared by watch_down and watch_stalled). While the watch is BOOTING
# — its tmux session (re)started within watch_restart_suppress_window, OR the daemon
# emitted a restart/startup marker that recently — its escalation cursor is frozen because
# it is coming up, not because it is stalled. Suppress watch-stalled (and hold the miss
# counter at 0) for that window so a post-deploy actionable backlog past the frozen cursor
# does not storm. The counter resets, so after the window it takes watch_stall_ticks fresh
# frozen ticks to (re)trip — a real post-boot stall is still caught.
if watch_opsmonitor_target != 'captain' and watch_cursor:
    cursor_frozen = prev_watch_cursor != '' and watch_cursor == prev_watch_cursor
    if cursor_frozen and actionable_events_past_cursor and not watch_restart_suppressed:
        new_watch_stall_misses = prev_watch_stall_misses + 1
        if new_watch_stall_misses >= watch_stall_ticks:
            watch_stalled = True
    else:
        new_watch_stall_misses = 0

# ── Release-due check ─────────────────────────────────────────────────────────
# Soft prompt only; never automatic. Normal IMMEDIATE_COOLDOWN applies (NOT a
# critical-component signal — release-due is not an infra failure).
release_due = release_commit_count >= release_due_threshold and ci_status == 'green'

# ── Build signal lists ───────────────────────────────────────────────────────
immediate_signals = []
digest_signals    = []

if not daemon_up:
    immediate_signals.append('daemon-down')
if not supervisor_up:
    # supervisor-down: DaemonWatchdog is also dead → no auto-revive path (hk-pen9).
    # Emit fleet-down when both are absent so the captain knows recovery is manual.
    sig = 'fleet-down:no-auto-revive' if not daemon_up else 'supervisor-down'
    immediate_signals.append(sig)
if paused_queues:
    immediate_signals.append('paused-queue:' + ','.join(paused_queues))
if single_mode:
    immediate_signals.append('single-mode:max_concurrent=1')
if review_bypass_run_ids:
    immediate_signals.append('review-bypass:' + ','.join(review_bypass_run_ids))
if captain_down:
    immediate_signals.append('captain-down')
if keeper_missing_crews:
    immediate_signals.append('keeper-missing:' + ','.join(sorted(keeper_missing_crews)))
if release_due:
    immediate_signals.append('release-due:' + str(release_commit_count))
if watch_down:
    immediate_signals.append('watch-down')
if watch_stalled:
    immediate_signals.append('watch-stalled')

if stale_signal_crews:
    names = ','.join(c['crew'] for c in stale_signal_crews)
    digest_signals.append('crew-stale:' + names)
if ready_unstaffed:
    digest_signals.append('ready-unstaffed:' + ','.join(ready_unstaffed))
if idle_fleet:
    digest_signals.append('idle-fleet:age=' + str(idle_age_s) + 's')
# Backlog-ready: only a JUDGMENT signal when there is ready work AND a free slot
# (total_active_workers < max_concurrent). The captain decides staffing; we just
# surface that staffing MAY be warranted so the captain wakes only then.
backlog_ready = daemon_up and ready_count > 0 and total_workers < max(max_concurrent, 1)
if backlog_ready:
    digest_signals.append('backlog-ready:count=' + str(ready_count))

# ── SD-1: program-drained-stall (DETERMINISTIC, agent-external) ───────────────
# PLAN-v2 Part 0 signal (a). Fires the lane-NAMED [IMMEDIATE] wake the captain
# cannot self-score its way out of, ON all three machine-computable predicates:
#   program_drained          == idle_fleet (Check 6: daemon up + 0 active workers +
#                               last run event >20m ago — the fleet-idle / queues-
#                               drained signal; PLAN: treat fleet-idle as the drain).
#   a-free-slot-exists       == total_workers < max(max_concurrent,1)  (the SAME
#                               free-slot computation backlog_ready uses; trivially
#                               true under idle_fleet but stated explicitly).
#   a-known-ready-lane-exists== KNOWN_READY_LANE is non-empty (computed in bash from
#                               lanes.json: a non-null-epic, ungated/expired-gate lane
#                               whose br-ready-parent epic query returned >=1 bead).
# Unlike backlog-ready (a low-priority DIGEST judgment hint that fires on ANY ready
# count + free slot), this is an [IMMEDIATE] that NAMES the lane + epic + ready count,
# so the captain has the staffing answer in hand on receipt — the audit is bypassed.
free_slot = total_workers < max(max_concurrent, 1)
program_drained_stall = bool(idle_fleet and free_slot and known_ready_lane)
if program_drained_stall:
    immediate_signals.append(
        'program-drained-stall:lane=%s,epic=%s,ready=%d' % (
            known_ready_lane, known_ready_lane_epic, known_ready_lane_count))

all_green = not immediate_signals and not digest_signals

# captain-up detail string
if captain_down:
    _cap_detail = 'absent from comms-who and no tmux session'
elif not captain_present and captain_tmux_alive:
    _cap_detail = 'tmux session alive (comms absent or stale)'
elif captain_info:
    _cap_detail = 'online (last_seen %ds)' % captain_info['last_seen_s']
else:
    _cap_detail = 'ok (tmux session alive)'

# WE9: watch-up detail string
if watch_stalled:
    _watch_detail = 'cursor frozen %d ticks with pending events' % new_watch_stall_misses
elif watch_down:
    _watch_detail = 'absent from comms-who and no tmux session'
elif not watch_present and watch_tmux_alive:
    _watch_detail = 'tmux session alive (comms absent or stale)'
elif watch_info:
    _watch_detail = 'online (last_seen %ds)' % watch_info['last_seen_s']
else:
    _watch_detail = 'ok (tmux alive)' if watch_tmux_alive else 'not deployed'

# ── Checks digest map (signal-vs-digest; one structured object for the captain) ─
# Each deterministic check -> {state: 'ok'|'flag', detail: <str>}. The captain reads
# this single map and escalates only on the JUDGMENT-flagged items (D6).
checks = {
    'daemon-up':     {'state': 'ok' if daemon_up else 'flag',
                      'detail': 'reachable' if daemon_up else 'queue status exit 17'},
    'supervisor-up': {'state': 'ok' if supervisor_up else 'flag',
                      'detail': 'running' if supervisor_up else 'not running — no auto-revive (hk-pen9)'},
    'paused-queues': {'state': 'flag' if paused_queues else 'ok',
                      'detail': ','.join(paused_queues) if paused_queues else 'none'},
    'single-mode':   {'state': 'flag' if single_mode else 'ok',
                      'detail': 'max_concurrent=1' if single_mode else 'max_concurrent=' + str(max_concurrent)},
    'crew-fresh':    {'state': 'flag' if stale_signal_crews else 'ok',
                      'detail': ','.join(c['crew'] for c in stale_signal_crews) if stale_signal_crews else 'all <150s'},
    'review-gate':   {'state': 'flag' if review_bypass_run_ids else 'ok',
                      'detail': ('unreviewed:' + ','.join(review_bypass_run_ids)) if review_bypass_run_ids else 'all review-launched/requested runs have a verdict'},
    'captain-up':    {'state': 'ok' if not captain_down else 'flag',
                      'detail': _cap_detail},
    'watch-up':      {'state': 'ok' if not watch_down and not watch_stalled else 'flag',
                      'detail': _watch_detail},
    'backlog-ready': {'state': 'flag' if backlog_ready else 'ok',
                      'detail': ('ready=' + str(ready_count) + ' free_slot') if backlog_ready else 'ready=' + str(ready_count)},
    'lull':          {'state': 'flag' if idle_fleet else 'ok',
                      'detail': ('idle ' + str(idle_age_s) + 's') if idle_fleet else 'active' if total_workers > 0 else 'idle<thresh'},
    'keepers-covered': {'state': 'flag' if keeper_missing_crews else 'ok',
                        'detail': ','.join(sorted(keeper_missing_crews)) if keeper_missing_crews else 'all online crews have keepers'},
    'release-due':     {'state': 'flag' if release_due else 'ok',
                        'detail': str(release_commit_count) + ' unreleased commits, CI=' + ci_status},
    'program-stall':   {'state': 'flag' if program_drained_stall else 'ok',
                        'detail': ('drained; KNOWN lane ' + known_ready_lane + ' (epic ' +
                                   known_ready_lane_epic + ') has ' + str(known_ready_lane_count) +
                                   ' ready + free slot') if program_drained_stall
                                  else ('known-ready-lane=' + (known_ready_lane or 'none') +
                                        ' idle=' + str(idle_fleet))},
}

# ── De-dup + cooldown for immediate signals ──────────────────────────────────
# Build new_alerted: persisted across runs.
# Non-critical signals: {signal_key: last_alert_epoch} (bare int, unchanged).
# Critical signals: {signal_key: {first_ts, last_ts, count}} (dict).
#   first_ts — epoch of the FIRST alert; never reset across re-alerts (measures total downtime).
#   last_ts  — epoch of the most recent send; used for cooldown math.
#   count    — incremented by 1 every time the signal is actually sent (including first edge).
# Back-compat: old bare-int entries for critical signals are normalized to
#   {first_ts: val, last_ts: val, count: 1} on first read.
#
# Escalation tiers (see escalations map below):
#   tier 1 = count == 1    — normal [IMMEDIATE]
#   tier 2 = count >= 2    — [ESCALATION] on ops-monitor
#   tier 3 = count >= OPS_CRITICAL_COUNT OR elapsed_s >= OPS_CRITICAL_ELAPSED
#            — additionally emit to --topic ops-CRITICAL (operator-wake channel)
#
# Critical-component signals (daemon-down / supervisor-down / fleet-down /
# captain-down) use CRITICAL_IMMEDIATE_COOLDOWN (5 min) instead of the global
# 30 min so a downed component re-alerts every ~5 min. captain-down arrives in
# sibling bead hk-mttt8; the prefix is listed here for forward-compatibility.
CRITICAL_PREFIXES = ('daemon-down', 'supervisor-down', 'fleet-down', 'captain-down', 'watch-down', 'watch-stalled')

def _dedup_key(sig):
    # release-due:<N> has a dynamic commit-count suffix that changes every tick.
    # Each new count was a fresh key, bypassing the 30-min cooldown and producing
    # 7 escalations in SOAK-1 (counts 102/174/207/212/216/265/266). Normalize to
    # 'release-due' so the cooldown persists across count increments. The full sig
    # (with count) is still used in send_immediate_signals and the comms body.
    if sig.startswith('release-due:'):
        return 'release-due'
    return sig

def _norm_alerted(val, is_crit):
    if val is None:
        return None
    if is_crit and isinstance(val, int):
        return {'first_ts': val, 'last_ts': val, 'count': 1}
    return val

new_alerted = {}
send_immediate_signals = []

for sig in immediate_signals:
    dedup_key = _dedup_key(sig)
    is_crit = any(sig.startswith(p) for p in CRITICAL_PREFIXES)
    cd = critical_immediate_cooldown if is_crit else immediate_cooldown
    prev_val = prev_alerted.get(dedup_key)
    prev_entry = _norm_alerted(prev_val, is_crit)
    if prev_entry is not None:
        if is_crit:
            last_ts = prev_entry.get('last_ts', prev_entry['first_ts'])
            # hk-ohb8p Fix B: persistent-condition throttle. Once THIS signal-identity
            # has been firing continuously long enough to reach tier-3 (count or elapsed
            # threshold) — i.e. it is a known, acknowledged, presumably-fix-in-flight
            # condition — drop its captain-facing re-alert cadence from 5 min to 30 min.
            # A NEW distinct signal (no prev_entry, or not yet persistent) keeps the fast
            # 5-min cadence. Keyed per-signal because prev_entry is per signal-identity.
            _prev_count   = prev_entry.get('count', 1)
            _prev_elapsed = ts_epoch - prev_entry.get('first_ts', ts_epoch)
            if _prev_count >= ops_critical_count or _prev_elapsed >= ops_critical_elapsed:
                cd = max(cd, persistent_immediate_cooldown)
            if ts_epoch - last_ts >= cd:
                send_immediate_signals.append(sig)
                new_alerted[dedup_key] = {
                    'first_ts': prev_entry['first_ts'],
                    'last_ts': ts_epoch,
                    'count': prev_entry.get('count', 1) + 1,
                    'last_ops_critical_ts': prev_entry.get('last_ops_critical_ts', 0),
                }
            else:
                # cooldown active; keep state. Drop any flap-grace marker — the signal is
                # active again, so a later clear should restart its grace window (Fix A).
                _kept = dict(prev_entry)
                _kept.pop('cleared_ts', None)
                new_alerted[dedup_key] = _kept
        else:
            age = ts_epoch - prev_entry
            if age >= cd:
                send_immediate_signals.append(sig)
                new_alerted[dedup_key] = ts_epoch  # reset timer on re-alert
            else:
                new_alerted[dedup_key] = prev_entry  # keep original timestamp
    else:
        send_immediate_signals.append(sig)  # new edge: send immediately
        if is_crit:
            new_alerted[dedup_key] = {'first_ts': ts_epoch, 'last_ts': ts_epoch, 'count': 1, 'last_ops_critical_ts': 0}
        else:
            new_alerted[dedup_key] = ts_epoch

# ── Escalation metadata for critical signals being sent ──────────────────────
# Keyed to send_immediate_signals so the bash send section can build per-tier messages.
escalations = {}
for sig in send_immediate_signals:
    if not any(sig.startswith(p) for p in CRITICAL_PREFIXES):
        continue
    entry = new_alerted.get(sig)
    if not isinstance(entry, dict):
        continue
    count = entry.get('count', 1)
    first_ts = entry.get('first_ts', ts_epoch)
    elapsed_s = ts_epoch - first_ts
    if count >= ops_critical_count or elapsed_s >= ops_critical_elapsed:
        tier = 3
    elif count >= 2:
        tier = 2
    else:
        tier = 1
    # Throttle the ops-CRITICAL operator-wake: even at tier 3, only page the operator
    # once per ops_critical_cooldown (30 min). The captain's every-5m [ESCALATION] on
    # the ops-monitor topic is unaffected; only the operator-wake channel is muted.
    send_ops_critical = False
    if tier >= 3:
        last_ops_crit_ts = entry.get('last_ops_critical_ts', 0)
        if ts_epoch - last_ops_crit_ts >= ops_critical_cooldown:
            send_ops_critical = True
            new_alerted[sig]['last_ops_critical_ts'] = ts_epoch
    escalations[sig] = {
        'count': count,
        'first_ts': first_ts,
        'elapsed_s': elapsed_s,
        'tier': tier,
        'send_ops_critical': send_ops_critical,
    }

# Drop resolved signals (not in this run's immediate_signals) from alerted set.
# They will re-alert fresh if the condition recurs — EXCEPT flap-prone signals, retained
# briefly below so an intermittent flap does not re-fire fresh every tick.
#
# ── hk-7o4i0 Fix A: flap-resistant retention for flap-prone critical signals ──
# A signal absent from this tick's immediate_signals is normally not copied into
# new_alerted, so it is DROPPED and re-alerts as a fresh count=1 edge if it recurs. For a
# FLAPPING signal (watch-stalled during a deploy: the escalation cursor advances one tick
# then re-freezes, or the actionable backlog momentarily drains) that fresh-edge re-fire
# bypasses the persistence throttle and storms the bus (~1 IMMEDIATE/tick across the whole
# post-deploy window — the observed ~57 storm). Retain such a signal's {first_ts,count}
# across watch_stall_flap_grace so intermittent re-trips ACCUMULATE toward the
# cooldown/persistence throttle instead of resetting. A genuinely-resolved signal still
# ages out after the grace window and re-alerts fresh on real recurrence. Scoped to
# FLAP_RETAIN_PREFIXES so non-flap criticals (daemon-down etc.) keep drop-on-resolve.
FLAP_RETAIN_PREFIXES = ('watch-stalled',)
# Use normalized dedup keys so 'release-due' (stored key) matches even when the raw
# signal this tick is 'release-due:N' (different count). Without this, a still-active
# release-due signal would be dropped from new_alerted as 'resolved' on every tick,
# causing the next tick to treat it as a fresh edge and re-alert.
_immediate_set = set(_dedup_key(s) for s in immediate_signals)
for _sig, _pv in prev_alerted.items():
    if _sig in new_alerted or _sig in _immediate_set:
        continue  # active this tick — already handled above
    if not any(_sig.startswith(p) for p in FLAP_RETAIN_PREFIXES):
        continue  # non-flap signal: drop on resolve (unchanged behavior)
    _pe = _norm_alerted(_pv, True)
    if not isinstance(_pe, dict):
        continue
    # First clear tick stamps cleared_ts=now; subsequent clear ticks measure age from it.
    _cleared_since = _pe.get('cleared_ts', ts_epoch)
    if ts_epoch - _cleared_since <= watch_stall_flap_grace:
        _retained = dict(_pe)
        _retained.setdefault('cleared_ts', ts_epoch)
        new_alerted[_sig] = _retained
    # else: grace expired → omit (drop); re-alerts fresh on recurrence.

# ── Determine whether to send digest (cooldown) ──────────────────────────────
# send_digest: would send digest (no immediate preemption, cooldown expired)
# immediate preemption is computed here so state correctly reflects actual send
send_digest = bool(digest_signals) and not immediate_signals and (ts_epoch - prev_digest) >= cooldown
new_last_digest = ts_epoch if send_digest else prev_digest

# ── Snapshot ─────────────────────────────────────────────────────────────────
snapshot = {
    'schema_version': 2,
    'ts': ts,
    'daemon_up': daemon_up,
    'supervisor_up': supervisor_up,
    'captain_down': captain_down,
    'max_concurrent': max_concurrent,
    'single_mode': single_mode,
    'queues': queues,
    'paused_queues': paused_queues,
    'crew_status': crew_status,
    'stale_crews': stale_signal_crews,
    'ready_unstaffed': ready_unstaffed,
    'idle_fleet': idle_fleet,
    'idle_fleet_age_s': idle_age_s,
    'total_active_workers': total_workers,
    'ready_count': ready_count,
    'backlog_ready': backlog_ready,
    # SD-1 deterministic program-drained stall detector (PLAN-v2 Part 0 signal (a)).
    'program_drained_stall': program_drained_stall,
    'known_ready_lane': known_ready_lane,
    'known_ready_lane_epic': known_ready_lane_epic,
    'known_ready_lane_count': known_ready_lane_count,
    'review_bypass_run_ids': review_bypass_run_ids,
    # hk-7o4i0 Fix B: true when watch-stalled was suppressed because the watch / daemon
    # was within its post-deploy restart window this tick.
    'watch_restart_suppressed': watch_restart_suppressed,
    'release_due': release_due,
    'release_commit_count': release_commit_count,
    'ci_status': ci_status,
    'checks': checks,
    'immediate_signals': immediate_signals,
    'send_immediate_signals': send_immediate_signals,
    # WE7/WE9 partition: direct-class (§4 SPOF bypass) → always --to captain;
    # watch-class → --to watch.opsmonitor_target (default 'captain').
    # watch-down and watch-stalled are direct-class: routing either to a dead/stalled watch is useless.
    'direct_signals': [s for s in send_immediate_signals if any(
        s.startswith(p) for p in ('daemon-down', 'supervisor-down', 'fleet-down', 'paused-queue', 'watch-down', 'watch-stalled'))],
    'watch_signals': [s for s in send_immediate_signals if not any(
        s.startswith(p) for p in ('daemon-down', 'supervisor-down', 'fleet-down', 'paused-queue', 'watch-down', 'watch-stalled'))],
    'escalations': escalations,
    'digest_signals': digest_signals,
    'all_green': all_green,
    'send_digest': send_digest,
}

# ── New state ─────────────────────────────────────────────────────────────────
new_state = {
    'schema_version': 1,
    'ts': ts,
    'stale_crew_misses': new_misses,
    'keeper_coverage_misses': new_keeper_misses,
    'last_digest_ts': new_last_digest,
    'alerted_immediate': new_alerted,
    'watch_cursor': watch_cursor,                  # WE9: persisted for next-tick frozen-cursor check
    'watch_stall_misses': new_watch_stall_misses,  # WE9: consecutive frozen-cursor miss count
}

print(json.dumps({'snapshot': snapshot, 'state': new_state}))
")

# ── Write latest.json ─────────────────────────────────────────────────────────

py3 "
import json, sys
d = json.loads(sys.stdin.read())
print(json.dumps(d['snapshot'], indent=2))
" <<< "$ANALYSIS" > "$LATEST_FILE"

# ── Update state.json ─────────────────────────────────────────────────────────

py3 "
import json, sys
d = json.loads(sys.stdin.read())
print(json.dumps(d['state'], indent=2))
" <<< "$ANALYSIS" > "$STATE_FILE"

# ── Send comms if signals present ─────────────────────────────────────────────

IMMEDIATE=$(py3 "import json,sys; d=json.loads(sys.stdin.read()); print(json.dumps(d['snapshot']['immediate_signals']))" <<< "$ANALYSIS")
SEND_IMMEDIATE=$(py3 "import json,sys; d=json.loads(sys.stdin.read()); print(json.dumps(d['snapshot']['send_immediate_signals']))" <<< "$ANALYSIS")
ESCALATIONS_JSON=$(py3 "import json,sys; d=json.loads(sys.stdin.read()); print(json.dumps(d['snapshot'].get('escalations', {})))" <<< "$ANALYSIS")
DIGEST=$(py3 "import json,sys; d=json.loads(sys.stdin.read()); print(json.dumps(d['snapshot']['digest_signals']))" <<< "$ANALYSIS")
SEND_DIGEST=$(py3 "import json,sys; d=json.loads(sys.stdin.read()); print(d['snapshot']['send_digest'])" <<< "$ANALYSIS")
ALL_GREEN=$(py3 "import json,sys; d=json.loads(sys.stdin.read()); print(d['snapshot']['all_green'])" <<< "$ANALYSIS")

# send_direct: always --to captain (§4 SPOF bypass for daemon/supervisor/paused).
# Used for direct-class signals: daemon-down, supervisor-down, fleet-down, paused-queue.
send_direct() {
  local body="$1"
  local topic="${2:-ops-monitor}"
  (cd "$PROJ" && harmonik comms send \
    --from ops-monitor \
    --to captain \
    --topic "$topic" \
    -- "$body") 2>&1 || true
}

# send_watch: routes to watch.opsmonitor_target (WE7 — defaults to 'captain').
# Used for watch-class signals (single-mode, review-bypass, captain-down,
# keeper-missing, release-due), DIGEST, and ops-CRITICAL.
send_watch() {
  local body="$1"
  local topic="${2:-ops-monitor}"
  (cd "$PROJ" && harmonik comms send \
    --from ops-monitor \
    --to "$WATCH_OPSMONITOR_TARGET" \
    --topic "$topic" \
    -- "$body") 2>&1 || true
}

# ── Extract WE7 signal partitions ─────────────────────────────────────────────
SEND_DIRECT=$(py3 "import json,sys; d=json.loads(sys.stdin.read()); print(json.dumps(d['snapshot']['direct_signals']))" <<< "$ANALYSIS")
SEND_WATCH_SIGS=$(py3 "import json,sys; d=json.loads(sys.stdin.read()); print(json.dumps(d['snapshot']['watch_signals']))" <<< "$ANALYSIS")

# Send immediate signals with tier-based escalation.
# Tier 1 (count==1): [IMMEDIATE] prefix on ops-monitor.
# Tier 2 (count>=2): [ESCALATION] prefix with count + elapsed on ops-monitor.
# Tier 3 (count>=OPS_CRITICAL_COUNT or elapsed>=OPS_CRITICAL_ELAPSED): also sends
#   a separate message to ops-CRITICAL (operator-wake channel, always send_watch).
if [[ "$SEND_IMMEDIATE" != "[]" ]]; then
  # Direct-class (daemon/supervisor/paused) → always --to captain (§4 SPOF bypass).
  if [[ "$SEND_DIRECT" != "[]" ]]; then
    DIRECT_TEXT=$(py3 "
import json
sigs = json.loads('$SEND_DIRECT')
escs = json.loads('''$ESCALATIONS_JSON''')
parts = []
for s in sigs:
    e = escs.get(s)
    if e and e.get('tier', 1) >= 2:
        m = e['elapsed_s'] // 60
        n = e['count']
        parts.append(f'[ESCALATION] {s} for >{m}m — alert #{n} — no self-healing path')
    else:
        parts.append('[IMMEDIATE] ' + s)
print(' | '.join(parts))
")
    send_direct "ops-monitor: $DIRECT_TEXT | ts=$TS | see .harmonik/ops-monitor/latest.json"
  fi

  # Watch-class (single-mode, review-bypass, captain-down, etc.) → send_watch.
  if [[ "$SEND_WATCH_SIGS" != "[]" ]]; then
    WATCH_TEXT=$(py3 "
import json
sigs = json.loads('$SEND_WATCH_SIGS')
escs = json.loads('''$ESCALATIONS_JSON''')
parts = []
for s in sigs:
    e = escs.get(s)
    if e and e.get('tier', 1) >= 2:
        m = e['elapsed_s'] // 60
        n = e['count']
        parts.append(f'[ESCALATION] {s} for >{m}m — alert #{n} — no self-healing path')
    else:
        parts.append('[IMMEDIATE] ' + s)
print(' | '.join(parts))
")
    send_watch "ops-monitor: $WATCH_TEXT | ts=$TS | see .harmonik/ops-monitor/latest.json"
  fi

  # Tier-3: operator-wake on ops-CRITICAL (always send_watch per WE7 §4).
  TIER3_PARTS=$(py3 "
import json
sigs = json.loads('$SEND_IMMEDIATE')
escs = json.loads('''$ESCALATIONS_JSON''')
parts = []
for s in sigs:
    e = escs.get(s)
    if e and e.get('send_ops_critical', False):
        m = e['elapsed_s'] // 60
        n = e['count']
        parts.append(f'{s} persistent >{m}m — alert #{n}')
print(' | '.join(parts))
")
  if [[ -n "$TIER3_PARTS" ]]; then
    send_watch "[ops-CRITICAL] $TIER3_PARTS — operator intervention required | ts=$TS | see .harmonik/ops-monitor/latest.json" "ops-CRITICAL"
  fi
elif [[ "$SEND_DIGEST" == "True" && "$DIGEST" != "[]" ]]; then
  SIGNALS_TEXT=$(py3 "import json; sigs=json.loads('$DIGEST'); print(' | '.join(sigs))")
  send_watch "[DIGEST] ops-monitor: $SIGNALS_TEXT | ts=$TS | see .harmonik/ops-monitor/latest.json"
fi

# ── Summary line (stdout for operator visibility) ─────────────────────────────

if [[ "$ALL_GREEN" == "True" ]]; then
  echo "ops-monitor: all-green @ $TS"
elif [[ "$IMMEDIATE" != "[]" ]]; then
  SIGNALS_TEXT=$(py3 "
import json
sigs = json.loads('$IMMEDIATE')
escs = json.loads('''$ESCALATIONS_JSON''')
parts = []
for s in sigs:
    e = escs.get(s)
    if e:
        parts.append(s + '(tier-' + str(e.get('tier',1)) + ',alert#' + str(e.get('count',1)) + ')')
    else:
        parts.append(s)
print(' | '.join(parts))
")
  SEND_TEXT=$(py3 "import json; sigs=json.loads('$SEND_IMMEDIATE'); print('(suppressed)' if not sigs else '')")
  echo "ops-monitor: IMMEDIATE: $SIGNALS_TEXT $SEND_TEXT@ $TS"
else
  SIGNALS_TEXT=$(py3 "import json; sigs=json.loads('$DIGEST'); print(' | '.join(sigs))")
  echo "ops-monitor: digest($SEND_DIGEST): $SIGNALS_TEXT @ $TS"
fi
