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

# Inert queues / dead-crew glob patterns — paused-by-failure on these NEVER fires an
# immediate alert. Add exact names or fnmatch-style globs. Editable here.
INERT_SUPPRESS_JSON='["main","remote-substrate","chani-q*","duncan-q*","liet-q*","stilgar-q*"]'
# Queues that are always alert-worthy even when their crew is offline.
LIVE_ALLOW_JSON='[]'
# Re-alert cooldown for the SAME still-active immediate signal (seconds).
IMMEDIATE_COOLDOWN=1800  # 30 minutes

mkdir -p "$OUT_DIR"

# ── Helpers ───────────────────────────────────────────────────────────────────

hk() { (cd "$PROJ" && harmonik "$@" 2>&1); }

py3() { python3 -c "$@"; }

# ── Load previous state ───────────────────────────────────────────────────────

PREV_STALE_MISSES='{}'
PREV_LAST_DIGEST=0
PREV_ALERTED_IMMEDIATE='{}'
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
import json, sys, os, datetime, fnmatch

proj               = '$PROJ'
ts                 = '$TS'
ts_epoch           = int('$TS_EPOCH')
daemon_up          = '$DAEMON_UP' == 'true'
stale_thresh       = int('$STALE_THRESHOLD')
miss_limit         = int('$MISS_LIMIT')
idle_thresh        = int('$IDLE_THRESHOLD')
cooldown           = int('$DIGEST_COOLDOWN')
last_run_ts        = int('$LAST_RUN_EVENT_TS')
prev_misses        = json.loads('$PREV_STALE_MISSES')
prev_digest        = int('$PREV_LAST_DIGEST')
prev_alerted       = json.loads('''$PREV_ALERTED_IMMEDIATE''')
immediate_cooldown = int('$IMMEDIATE_COOLDOWN')
inert_suppress     = json.loads('''$INERT_SUPPRESS_JSON''')
live_allow         = json.loads('''$LIVE_ALLOW_JSON''')
comms_raw          = '''$COMMS_WHO_NDJSON'''
qlist_raw          = '''$QUEUE_LIST_JSON'''

# ── Parse events.jsonl for recent agent_message activity ────────────────────
# Used as a presence fallback: comms send does NOT refresh the presence
# heartbeat, so an agent posting status while its presence is stale by
# last_seen would fire a false crew-stale alert. We suppress it if the
# sender has an agent_message within stale_thresh seconds (hk-gu3v).
import os as _os
last_msg_ts = {}  # agent_name -> epoch of most recent agent_message
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
        for _line in _raw.decode('utf-8', errors='replace').splitlines():
            _line = _line.strip()
            if not _line:
                continue
            try:
                _ev = json.loads(_line)
                if _ev.get('type') != 'agent_message':
                    continue
                _tw = _ev.get('timestamp_wall', '')
                if not _tw:
                    continue
                _ts_clean = _tw[:19].replace('T', ' ')
                _d = _dt.datetime.strptime(_ts_clean, '%Y-%m-%d %H:%M:%S')
                _epoch = int(_d.replace(tzinfo=_dt.timezone.utc).timestamp())
                _payload = _ev.get('payload', {})
                if isinstance(_payload, str):
                    _payload = json.loads(_payload)
                _sender = _payload.get('from', '')
                if _sender and _epoch > last_msg_ts.get(_sender, 0):
                    last_msg_ts[_sender] = _epoch
            except Exception:
                pass
    except Exception:
        pass

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
new_misses = {}
stale_signal_crews = []
for name, info in crew_status.items():
    miss_count = prev_misses.get(name, 0)
    # comms send does NOT refresh presence, so fall back to agent_message
    # recency: if the agent posted a message within stale_thresh, treat as
    # active even when the presence last_seen is stale (hk-gu3v).
    effective_stale = info['stale']
    if effective_stale:
        _msg_ts = last_msg_ts.get(name, 0)
        if _msg_ts and (ts_epoch - _msg_ts) <= stale_thresh:
            effective_stale = False
            info['msg_override'] = ts_epoch - _msg_ts  # age of most recent msg, for snapshot
    if effective_stale:
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

# ── De-dup + cooldown for immediate signals ──────────────────────────────────
# Build new_alerted: {signal_key: first_alert_epoch} persisted across runs.
# A signal is sent only on its first occurrence (edge) or when the cooldown
# has expired and it is still active. Clear entries when the condition resolves.
new_alerted = {}
send_immediate_signals = []

for sig in immediate_signals:
    if sig in prev_alerted:
        age = ts_epoch - prev_alerted[sig]
        if age >= immediate_cooldown:
            send_immediate_signals.append(sig)
            new_alerted[sig] = ts_epoch  # reset timer on re-alert
        else:
            new_alerted[sig] = prev_alerted[sig]  # keep original timestamp
    else:
        send_immediate_signals.append(sig)  # new edge: send immediately
        new_alerted[sig] = ts_epoch

# Drop resolved signals (not in this run's immediate_signals) from alerted set.
# They will re-alert fresh if the condition recurs.

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
    'send_immediate_signals': send_immediate_signals,
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
    'alerted_immediate': new_alerted,
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

# Send [IMMEDIATE] only for signals that are new or whose cooldown has expired.
if [[ "$SEND_IMMEDIATE" != "[]" ]]; then
  SIGNALS_TEXT=$(py3 "import json; sigs=json.loads('$SEND_IMMEDIATE'); print(' | '.join(sigs))")
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
  SEND_TEXT=$(py3 "import json; sigs=json.loads('$SEND_IMMEDIATE'); print('(suppressed)' if not sigs else '')")
  echo "ops-monitor: IMMEDIATE: $SIGNALS_TEXT $SEND_TEXT@ $TS"
else
  SIGNALS_TEXT=$(py3 "import json; sigs=json.loads('$DIGEST'); print(' | '.join(sigs))")
  echo "ops-monitor: digest($SEND_DIGEST): $SIGNALS_TEXT @ $TS"
fi
