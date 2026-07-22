#!/usr/bin/env bash
# Admiral-owned crew-liveness watcher.
# Polls each expected crew's tmux pane every 20s (pane existence = liveness,
# NOT comms presence — presence drops at 120s idle and looks like death).
# On a crew vanishing it logs a DIED line the admiral reads; the admiral/captain
# re-spawns. Recurring deaths (assessor) are the reason this exists.
cd /Users/gb/github/harmonik || exit 1
LOG="plans/2026-07-17-prod-readiness-watch/WATCH-crew.log"
STATE="/private/tmp/claude-502/-Users-gb-github-harmonik/2afe6092-3436-4b10-8a8f-5f4899e117ff/scratchpad"
SESS_PREFIX="harmonik-a3dc45482890-crew-"
CREWS="assessor bravo"
echo "$(date -u +%FT%TZ) CREW-WATCH-START watching: $CREWS" >> "$LOG"
for c in $CREWS; do echo up > "$STATE/crewstate-$c"; done
while true; do
  for c in $CREWS; do
    prev=$(cat "$STATE/crewstate-$c" 2>/dev/null || echo up)
    if tmux has-session -t "${SESS_PREFIX}${c}" 2>/dev/null; then
      [ "$prev" = "down" ] && echo "$(date -u +%FT%TZ) CREW-BACK $c" >> "$LOG"
      echo up > "$STATE/crewstate-$c"
    else
      [ "$prev" = "up" ] && echo "$(date -u +%FT%TZ) CREW-DIED $c — pane ${SESS_PREFIX}${c} gone" >> "$LOG"
      echo down > "$STATE/crewstate-$c"
    fi
  done
  sleep 20
done
