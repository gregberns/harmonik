#!/usr/bin/env bash
# S6 adversarial log watcher — always-on for the whole campaign.
# Structured/log queries only. Emits to STDOUT (→ notification):
#   * every FAIL-set hit (level=error|fatal|panic|goroutine leak|WaitGroup misuse) — immediately
#   * a HEARTBEAT line every HB_EVERY cycles (proves liveness; a dead watcher that
#     "found nothing" is itself an S6 FAIL)
# Appends full heartbeats + hits to LOG-WATCH-FINDINGS.md (its own log).
set -uo pipefail
SCRATCH="/private/tmp/h-assessor/scratch-exploratory-2d308836"
FIND="/Users/gb/github/harmonik/plans/2026-07-17-assessor-daemon-campaign/runs/exploratory-2d308836/LOG-WATCH-FINDINGS.md"
LOG="$SCRATCH/.harmonik/scratch-daemon.log"
PIDFILE="$SCRATCH/.harmonik/daemon.pid"
BASE_FD=13; BASE_TH=16
INTERVAL=20      # seconds between cycles
HB_EVERY=15      # emit a stdout heartbeat every N cycles (~5 min)
FDLIMIT=$((BASE_FD + 40)); THLIMIT=$((BASE_TH + 30))
seen=0; cyc=0
while :; do
  cyc=$((cyc+1))
  pid="$(head -n1 "$PIDFILE" 2>/dev/null | tr -d '[:space:]')"
  fd=0; th=0; up=DOWN
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    up=UP; fd=$(lsof -p "$pid" 2>/dev/null | wc -l | tr -d ' '); th=$(ps -M "$pid" 2>/dev/null | grep -c .)
  fi
  # scan for NEW fail-set lines appended since last offset
  total=$(wc -l < "$LOG" 2>/dev/null | tr -d ' '); total=${total:-0}
  if [ "$total" -gt "$seen" ]; then
    new="$(tail -n +$((seen+1)) "$LOG" 2>/dev/null | grep -inE 'level=(error|fatal)|panic:|fatal error|goroutine [0-9]+ \[|WaitGroup( is)? (misuse|reused)|too many open files|leaked' )"
    if [ -n "$new" ]; then
      ts="$(date -u +%FT%TZ)"
      printf 'S6_HIT %s pid=%s:\n%s\n' "$ts" "$pid" "$new"
      { echo "### S6_HIT $ts (daemon pid=$pid)"; echo '```'; echo "$new"; echo '```'; } >> "$FIND"
    fi
    seen="$total"
  fi
  # fd/thread growth guard
  if [ "$up" = UP ] && { [ "$fd" -gt "$FDLIMIT" ] || [ "$th" -gt "$THLIMIT" ]; }; then
    ts="$(date -u +%FT%TZ)"
    printf 'S6_GROWTH %s fd=%s (base %s, lim %s) th=%s (base %s, lim %s)\n' "$ts" "$fd" "$BASE_FD" "$FDLIMIT" "$th" "$BASE_TH" "$THLIMIT"
    echo "- S6_GROWTH $ts fd=$fd th=$th (base fd=$BASE_FD th=$BASE_TH)" >> "$FIND"
  fi
  # heartbeat to own log every cycle; to stdout every HB_EVERY cycles
  hb="$(date -u +%FT%TZ) cyc=$cyc daemon=$up pid=$pid fd=$fd th=$th"
  echo "- HB $hb" >> "$FIND"
  if [ $((cyc % HB_EVERY)) -eq 0 ]; then echo "S6_HEARTBEAT $hb"; fi
  sleep "$INTERVAL"
done
