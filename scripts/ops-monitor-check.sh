#!/usr/bin/env bash
# ops-monitor-check.sh — One-pass deterministic fleet health check.
#
# Checks (in order):
#   1. daemon-up        — harmonik queue status exit 17 = daemon down
#   2. paused-queues    — main queue or active crew queue paused-by-failure
#   3. single-mode      — max_concurrent == 1 (throughput bottleneck)
#   4. crew-staleness   — comms last_seen >150s; signals after 2 consecutive misses
#   5. ready-unstaffed  — queue has pending_items > 0 but no workers and no online crew
#   6. idle-fleet       — no active workers + last run event >20m ago
#
# Writes:  $PROJ/.harmonik/ops-monitor/latest.json  (machine snapshot, always)
# Sends:   harmonik comms --from ops-monitor --to captain --topic ops-monitor
#            immediate : daemon-down | paused-queue | single-mode
#            digest    : ready-unstaffed | idle-fleet  (≤15m cooldown)
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
# Refs: hk-k2px, leanfleet D4 (epic hk-itoc)

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

mkdir -p "$OUT_DIR"

# ── Helpers ───────────────────────────────────────────────────────────────────

hk() { (cd "$PROJ" && harmonik "$@" 2>&1); }

py3() { python3 -c "$@"; }

# ── Load previous state ───────────────────────────────────────────────────────

PREV_STALE_MISSES='{}'
PREV_LAST_DIGEST=0
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

# ── Python analysis: produce JSON snapshot ────────────────────────────────────

ANALYSIS=$(py3 "
import json, sys, os, datetime

proj         = '$PROJ'
ts           = '$TS'
ts_epoch     = int('$TS_EPOCH')
daemon_up    = '$DAEMON_UP' == 'true'
stale_thresh = int('$STALE_THRESHOLD')
miss_limit   = int('$MISS_LIMIT')
idle_thresh  = int('$IDLE_THRESHOLD')
cooldown     = int('$DIGEST_COOLDOWN')
last_run_ts  = int('$LAST_RUN_EVENT_TS')
prev_misses  = json.loads('$PREV_STALE_MISSES')
prev_digest  = int('$PREV_LAST_DIGEST')
comms_raw    = '''$COMMS_WHO_NDJSON'''
qlist_raw    = '''$QUEUE_LIST_JSON'''

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

            # Paused signal: paused-by-failure on main queue, or on an online
            # crew's queue (derive crew name: strip trailing '-q' suffix)
            if qstatus == 'paused-by-failure':
                crew_guess = qname[:-2] if qname.endswith('-q') else qname
                is_main    = qname == 'main'
                is_active_crew = crew_guess in online_crews
                if is_main or is_active_crew:
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
new_misses = {}
stale_signal_crews = []
for name, info in crew_status.items():
    miss_count = prev_misses.get(name, 0)
    if info['stale']:
        miss_count += 1
        new_misses[name] = miss_count
        if miss_count >= miss_limit:
            stale_signal_crews.append({'crew': name, 'last_seen_s': info['last_seen_s'], 'misses': miss_count})
    else:
        new_misses[name] = 0  # reset on recovery

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

# ── Build signal lists ───────────────────────────────────────────────────────
immediate_signals = []
digest_signals    = []

if not daemon_up:
    immediate_signals.append('daemon-down')
if paused_queues:
    immediate_signals.append('paused-queue:' + ','.join(paused_queues))
if single_mode:
    immediate_signals.append('single-mode:max_concurrent=1')

if stale_signal_crews:
    names = ','.join(c['crew'] for c in stale_signal_crews)
    digest_signals.append('crew-stale:' + names)
if ready_unstaffed:
    digest_signals.append('ready-unstaffed:' + ','.join(ready_unstaffed))
if idle_fleet:
    digest_signals.append('idle-fleet:age=' + str(idle_age_s) + 's')

all_green = not immediate_signals and not digest_signals

# ── Determine whether to send digest (cooldown) ──────────────────────────────
# send_digest: would send digest (no immediate preemption, cooldown expired)
# immediate preemption is computed here so state correctly reflects actual send
send_digest = bool(digest_signals) and not immediate_signals and (ts_epoch - prev_digest) >= cooldown
new_last_digest = ts_epoch if send_digest else prev_digest

# ── Snapshot ─────────────────────────────────────────────────────────────────
snapshot = {
    'schema_version': 1,
    'ts': ts,
    'daemon_up': daemon_up,
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
    'immediate_signals': immediate_signals,
    'digest_signals': digest_signals,
    'all_green': all_green,
    'send_digest': send_digest,
}

# ── New state ─────────────────────────────────────────────────────────────────
new_state = {
    'schema_version': 1,
    'ts': ts,
    'stale_crew_misses': new_misses,
    'last_digest_ts': new_last_digest,
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
DIGEST=$(py3 "import json,sys; d=json.loads(sys.stdin.read()); print(json.dumps(d['snapshot']['digest_signals']))" <<< "$ANALYSIS")
SEND_DIGEST=$(py3 "import json,sys; d=json.loads(sys.stdin.read()); print(d['snapshot']['send_digest'])" <<< "$ANALYSIS")
ALL_GREEN=$(py3 "import json,sys; d=json.loads(sys.stdin.read()); print(d['snapshot']['all_green'])" <<< "$ANALYSIS")

send_comms() {
  local body="$1"
  (cd "$PROJ" && harmonik comms send \
    --from ops-monitor \
    --to captain \
    --topic ops-monitor \
    -- "$body") 2>&1 || true
}

if [[ "$IMMEDIATE" != "[]" ]]; then
  SIGNALS_TEXT=$(py3 "import json; sigs=json.loads('$IMMEDIATE'); print(' | '.join(sigs))")
  send_comms "[IMMEDIATE] ops-monitor: $SIGNALS_TEXT | ts=$TS | see .harmonik/ops-monitor/latest.json"
elif [[ "$SEND_DIGEST" == "True" && "$DIGEST" != "[]" ]]; then
  SIGNALS_TEXT=$(py3 "import json; sigs=json.loads('$DIGEST'); print(' | '.join(sigs))")
  send_comms "[DIGEST] ops-monitor: $SIGNALS_TEXT | ts=$TS | see .harmonik/ops-monitor/latest.json"
fi

# ── Summary line (stdout for operator visibility) ─────────────────────────────

if [[ "$ALL_GREEN" == "True" ]]; then
  echo "ops-monitor: all-green @ $TS"
elif [[ "$IMMEDIATE" != "[]" ]]; then
  SIGNALS_TEXT=$(py3 "import json; sigs=json.loads('$IMMEDIATE'); print(' | '.join(sigs))")
  echo "ops-monitor: IMMEDIATE: $SIGNALS_TEXT @ $TS"
else
  SIGNALS_TEXT=$(py3 "import json; sigs=json.loads('$DIGEST'); print(' | '.join(sigs))")
  echo "ops-monitor: digest($SEND_DIGEST): $SIGNALS_TEXT @ $TS"
fi
