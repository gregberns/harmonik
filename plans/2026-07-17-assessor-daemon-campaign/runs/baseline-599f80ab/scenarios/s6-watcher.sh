#!/usr/bin/env bash
# S6 adversarial log watcher — always-on for the whole campaign. Parameterized.
#   $1 = SCRATCH (scratch clone path)   $2 = FIND (LOG-WATCH-FINDINGS.md path)
# Emits FAIL-set hits + fd/thread growth to stdout; HEARTBEAT every HB_EVERY cycles.
set -uo pipefail
SCRATCH="${1:?scratch path required}"
FIND="${2:?findings path required}"
LOG="$SCRATCH/.harmonik/scratch-daemon.log"
PIDFILE="$SCRATCH/.harmonik/daemon.pid"
BASE_FD=13; BASE_TH=16
INTERVAL=20; HB_EVERY=15
FDLIMIT=$((BASE_FD + 60)); THLIMIT=$((BASE_TH + 40))
seen=0; cyc=0
echo "# S6 LOG-WATCH-FINDINGS — $(date -u +%FT%TZ) — watching $LOG" > "$FIND"
while :; do
  cyc=$((cyc+1))
  pid="$(head -n1 "$PIDFILE" 2>/dev/null | tr -d '[:space:]')"
  fd=0; th=0; up=DOWN
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    up=UP; fd=$(lsof -p "$pid" 2>/dev/null | wc -l | tr -d ' '); th=$(ps -M "$pid" 2>/dev/null | grep -c .)
  fi
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
  if [ "$up" = UP ] && { [ "$fd" -gt "$FDLIMIT" ] || [ "$th" -gt "$THLIMIT" ]; }; then
    ts="$(date -u +%FT%TZ)"
    printf 'S6_GROWTH %s fd=%s (base %s lim %s) th=%s (base %s lim %s)\n' "$ts" "$fd" "$BASE_FD" "$FDLIMIT" "$th" "$BASE_TH" "$THLIMIT"
    echo "- S6_GROWTH $ts fd=$fd th=$th" >> "$FIND"
  fi
  hb="$(date -u +%FT%TZ) cyc=$cyc daemon=$up pid=$pid fd=$fd th=$th"
  echo "- HB $hb" >> "$FIND"
  if [ $((cyc % HB_EVERY)) -eq 0 ]; then echo "S6_HEARTBEAT $hb"; fi
  sleep "$INTERVAL"
done
