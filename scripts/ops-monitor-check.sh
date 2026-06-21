#!/usr/bin/env bash
# ops-monitor-check.sh — One-pass deterministic fleet health check.
#
# Checks (in order):
#   1. daemon-up        — harmonik queue status exit 17 = daemon down
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
#            immediate : daemon-down | paused-queue | single-mode | review-bypass
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
stale_thresh            = int('$STALE_THRESHOLD')
miss_limit              = int('$MISS_LIMIT')
idle_thresh             = int('$IDLE_THRESHOLD')
cooldown                = int('$DIGEST_COOLDOWN')
crew_msg_active_window  = int('$CREW_MSG_ACTIVE_WINDOW')
last_run_ts             = int('$LAST_RUN_EVENT_TS')
prev_misses             = json.loads('$PREV_STALE_MISSES')
prev_digest             = int('$PREV_LAST_DIGEST')
prev_alerted            = json.loads('''$PREV_ALERTED_IMMEDIATE''')
immediate_cooldown      = int('$IMMEDIATE_COOLDOWN')
inert_suppress     = json.loads('''$INERT_SUPPRESS_JSON''')
live_allow         = json.loads('''$LIVE_ALLOW_JSON''')
comms_raw          = '''$COMMS_WHO_NDJSON'''
qlist_raw          = '''$QUEUE_LIST_JSON'''
ready_count        = int('$READY_COUNT')
review_window      = int('$REVIEW_GATE_WINDOW')
review_grace       = int('$REVIEW_GATE_GRACE')
reviewer_stale_window = int('$REVIEWER_STALE_WINDOW')

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
# ENTERED a review path (emitted reviewer_launched) yet produced NO reviewer_verdict.
# Runs that auto-closed / made no change / were subsumed never launch a reviewer (by
# design — workloop.go ~:3811) so they must NOT be flagged. We therefore join
# reviewer_launched -> reviewer_verdict by run_id, NOT run_completed -> verdict.
reviewer_launched_ts = {}  # run_id -> epoch of the reviewer_launched event (review entered)
verdict_run_ids      = set() # run_ids that have a reviewer_verdict event
failed_run_ids       = set() # run_ids whose terminal event is run_failed (merged nothing)
# Short-circuit signal (hk-orni / hk-2vpj follow-up): a run that REQUESTED a review node
# (node_dispatch_requested with node_id starting 'review' — review / review_correctness /
# review_design / review_tests / reviewer) but for which the reviewer never launched (no
# reviewer_launched event). On live data this cleanly separates engine-short-circuited
# reviewers (8 run_ids) from legitimately review-less auto-closes (0 of 181 request a
# review node). node_id lives under .payload; run_id is top-level.
review_requested_ts  = {}  # run_id -> epoch of the earliest review-node dispatch request
run_last_event_ts    = {}  # run_id -> epoch of the most-recent event seen for that run (any type)

def _ev_epoch(_tw, _dt):
    _ts_clean = _tw[:19].replace('T', ' ')
    _d = _dt.datetime.strptime(_ts_clean, '%Y-%m-%d %H:%M:%S')
    return int(_d.replace(tzinfo=_dt.timezone.utc).timestamp())

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
                        reviewer_launched_ts[_rid] = _ev_epoch(_tw, _dt)

                elif _etype == 'reviewer_verdict':
                    # reviewer_verdict carries run_id at top level AND in payload.
                    _rid = _ev.get('run_id') or _payload.get('run_id') or ''
                    if _rid:
                        verdict_run_ids.add(_rid)

                elif _etype == 'run_failed':
                    # run_failed = terminal failure; the run merged nothing. Collect so the
                    # review-gate can suppress false-positive bypass alerts on failed runs
                    # (e.g. reviewer_budget_exceeded / 'verdict absent').
                    _rid = _ev.get('run_id') or (_payload.get('run_id') if isinstance(_payload, dict) else '') or ''
                    if _rid:
                        failed_run_ids.add(_rid)

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

                # Track the most-recent event timestamp per run_id (any event type).
                # Used to detect abandoned reviewers: reviewer_launched with no subsequent
                # verdict and no event at all for reviewer_stale_window seconds (hk-usz0).
                _any_rid = _ev.get('run_id') or (_payload.get('run_id') if isinstance(_payload, dict) else '') or ''
                _any_tw = _ev.get('timestamp_wall', '')
                if _any_rid and _any_tw:
                    _any_e = _ev_epoch(_any_tw, _dt)
                    if _any_e > run_last_event_ts.get(_any_rid, 0):
                        run_last_event_ts[_any_rid] = _any_e
            except Exception:
                pass
    except Exception:
        pass

# ── Review-gate: runs that ENTERED-OR-REQUESTED review (window, past grace) w/o verdict ──
# A run is review-BYPASSED if it either (a) emitted reviewer_launched (entered a review
# path) OR (b) requested a review node via node_dispatch_requested node_id=review* (the
# engine asked for review) — yet has NO matching reviewer_verdict. Case (b) catches the
# hk-2vpj engine-short-circuit class: the engine REQUESTS a review node but the reviewer
# never launches, so no reviewer_launched event exists and the change merges UNREVIEWED.
# Auto-close / noChange / subsumed runs neither launch nor request a reviewer, so they are
# absent from both maps and cannot be flagged (R6 suppression preserved, hk-ayvx).
# We take the UNION of the launched and review-requested run_ids, key each by the EARLIEST
# review event (launch or request) for the grace check, window over the most-recent such
# run_ids, and skip any younger than review_grace — its verdict may still be in flight.
# MEDIUM (multi-iteration DOT): launch/request and reviewer_verdict are joined on the SAME
# run_id; if ANY iteration of a run emits a verdict under that run_id the run is cleared. A
# terminal-close run_id that never launched OR requested a reviewer is in neither map, so
# unmatched `dot: reached terminal node close` run_ids never false-flag.
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
    # Stale/abandoned reviewer: reviewer_launched but no verdict AND no event for
    # reviewer_stale_window seconds — the reviewer session died without a terminal
    # event (no run_failed, no reviewer_verdict). Not a merge-safety hole; suppress
    # to avoid alert-fatigue on IMMEDIATE (hk-usz0, follow-up to hk-ijtw).
    _last_ev_ts = run_last_event_ts.get(_rid, _lts)
    if (ts_epoch - _last_ev_ts) > reviewer_stale_window and _rid not in verdict_run_ids:
        continue  # abandoned reviewer: silent past stale window, merged nothing
    if _rid not in verdict_run_ids:
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
new_misses = {}
stale_signal_crews = []
for name, info in crew_status.items():
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
if review_bypass_run_ids:
    immediate_signals.append('review-bypass:' + ','.join(review_bypass_run_ids))

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

all_green = not immediate_signals and not digest_signals

# ── Checks digest map (signal-vs-digest; one structured object for the captain) ─
# Each deterministic check -> {state: 'ok'|'flag', detail: <str>}. The captain reads
# this single map and escalates only on the JUDGMENT-flagged items (D6).
checks = {
    'daemon-up':     {'state': 'ok' if daemon_up else 'flag',
                      'detail': 'reachable' if daemon_up else 'queue status exit 17'},
    'paused-queues': {'state': 'flag' if paused_queues else 'ok',
                      'detail': ','.join(paused_queues) if paused_queues else 'none'},
    'single-mode':   {'state': 'flag' if single_mode else 'ok',
                      'detail': 'max_concurrent=1' if single_mode else 'max_concurrent=' + str(max_concurrent)},
    'crew-fresh':    {'state': 'flag' if stale_signal_crews else 'ok',
                      'detail': ','.join(c['crew'] for c in stale_signal_crews) if stale_signal_crews else 'all <150s'},
    'review-gate':   {'state': 'flag' if review_bypass_run_ids else 'ok',
                      'detail': ('unreviewed:' + ','.join(review_bypass_run_ids)) if review_bypass_run_ids else 'all review-launched/requested runs have a verdict'},
    'backlog-ready': {'state': 'flag' if backlog_ready else 'ok',
                      'detail': ('ready=' + str(ready_count) + ' free_slot') if backlog_ready else 'ready=' + str(ready_count)},
    'lull':          {'state': 'flag' if idle_fleet else 'ok',
                      'detail': ('idle ' + str(idle_age_s) + 's') if idle_fleet else 'active' if total_workers > 0 else 'idle<thresh'},
}

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
    'schema_version': 2,
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
    'ready_count': ready_count,
    'backlog_ready': backlog_ready,
    'review_bypass_run_ids': review_bypass_run_ids,
    'checks': checks,
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
