#!/usr/bin/env bash
# Admiral-owned daemon liveness watcher.
# Polls the supervisor/daemon every 30s. On a DOWN transition it logs the event
# and revives via `harmonik supervise start` (the known-good path; hk-ky7ye:
# the in-daemon watchdog auto-revive is broken, `supervise start` works).
# The admiral reads WATCH-daemon.log each audit cycle so a death is never silent.
set -u
cd /Users/gb/github/harmonik || exit 1
LOG="plans/2026-07-17-prod-readiness-watch/WATCH-daemon.log"
last="up"
echo "$(date -u +%FT%TZ) WATCH-START admiral daemon watcher armed" >> "$LOG"
while true; do
  if harmonik supervise status 2>/dev/null | grep -q 'status:.*running' \
     && harmonik digest >/dev/null 2>&1; then
    if [ "$last" = "down" ]; then
      echo "$(date -u +%FT%TZ) DAEMON-RECOVERED (supervisor+comms back up)" >> "$LOG"
    fi
    last="up"
  else
    echo "$(date -u +%FT%TZ) DAEMON-DOWN detected — reviving via supervise start" >> "$LOG"
    harmonik supervise start >> "$LOG" 2>&1
    last="down"
  fi
  sleep 30
done
