#!/usr/bin/env bash
# Admiral-owned crew-liveness watcher (v2 — covers all live windows + auto-respawn).
# Polls each expected crew's tmux pane every 15s (pane existence = liveness,
# NOT comms presence — presence drops at 120s idle and looks like death).
#
# Covers: assessor (release-gate crew, respawn WITH keeper), bravo (worker,
# respawn), captain (log only — captain respawn is an operator call).
#
# On up->down edge: log CREW-DIED and (assessor/bravo) auto-respawn.
# While still down: retry respawn every RETRY_CYCLES polls (cooldown) so a
# failed boot self-heals without a respawn storm.
# On down->up edge: log CREW-BACK.
# All lines UTC-stamped to WATCH-crew.log (admiral tails this).
set -u
REPO="/Users/gb/github/harmonik"
cd "$REPO" || exit 1
LOG="$REPO/plans/2026-07-17-prod-readiness-watch/WATCH-crew.log"
STATE="/private/tmp/claude-502/-Users-gb-github-harmonik/ff4139a9-8b70-4be9-a738-b8e16a70f3b4/scratchpad/crewstate-v2"
mkdir -p "$STATE"
SESS_HOST="harmonik-a3dc45482890"
POLL=15
RETRY_CYCLES=10   # ~150s cooldown between respawn retries while down

# name : tmux-session : role : mission-path
# role: assessor (respawn+keeper) | worker (respawn) | captain (log only)
# bravo REMOVED 2026-07-19 (admiral): the worker spec here auto-respawned bravo
# ~5s after every teardown, re-violating the operator's permanent-teardown order
# (dead OAuth-wall zombie). Do NOT re-add. Only the assessor (release-gate crew)
# gets an auto-respawn net; captain respawn is an operator call (log-only here).
SPECS=(
  "assessor:${SESS_HOST}-crew-assessor:assessor:${REPO}/HANDOFF-assessor.md"
  "captain:${SESS_HOST}-captain:captain:"
)

ts() { date -u +%FT%TZ; }

respawn_worker() {
  local name="$1" mission="$2"
  echo "$(ts) CREW-RESPAWN $name — harmonik crew start (worker)" >> "$LOG"
  ( harmonik crew start "$name" --mission "$mission" >> "$LOG" 2>&1 ) &
}

respawn_assessor() {
  local name="$1" mission="$2"
  echo "$(ts) CREW-RESPAWN $name — harmonik crew start + keeper (assessor)" >> "$LOG"
  (
    harmonik crew start "$name" --mission "$mission" >> "$LOG" 2>&1
    sleep 4
    nohup harmonik keeper --agent "$name" >> "$LOG" 2>&1 &
    echo "$(ts) CREW-RESPAWN $name — keeper armed pid $!" >> "$LOG"
  ) &
}

echo "$(ts) CREW-WATCH-V2-START watching: assessor(keeper) bravo(worker) captain(log-only) poll=${POLL}s" >> "$LOG"
for spec in "${SPECS[@]}"; do
  name="${spec%%:*}"
  echo up > "$STATE/state-$name"
  echo 0  > "$STATE/downc-$name"
done

while true; do
  for spec in "${SPECS[@]}"; do
    name="${spec%%:*}"; rest="${spec#*:}"
    sess="${rest%%:*}"; rest="${rest#*:}"
    role="${rest%%:*}"; mission="${rest#*:}"
    prev=$(cat "$STATE/state-$name" 2>/dev/null || echo up)
    downc=$(cat "$STATE/downc-$name" 2>/dev/null || echo 0)
    if tmux has-session -t "$sess" 2>/dev/null; then
      [ "$prev" = "down" ] && echo "$(ts) CREW-BACK $name" >> "$LOG"
      echo up > "$STATE/state-$name"
      echo 0  > "$STATE/downc-$name"
    else
      if [ "$prev" = "up" ]; then
        echo "$(ts) CREW-DIED $name — pane $sess gone" >> "$LOG"
        downc=0
        case "$role" in
          assessor) respawn_assessor "$name" "$mission" ;;
          worker)   respawn_worker  "$name" "$mission" ;;
          captain)  echo "$(ts) CREW-NOTE captain down — NOT auto-respawning (operator call)" >> "$LOG" ;;
        esac
      else
        downc=$((downc+1))
        if [ "$downc" -ge "$RETRY_CYCLES" ]; then
          downc=0
          case "$role" in
            assessor) echo "$(ts) CREW-RETRY $name (still down)" >> "$LOG"; respawn_assessor "$name" "$mission" ;;
            worker)   echo "$(ts) CREW-RETRY $name (still down)" >> "$LOG"; respawn_worker  "$name" "$mission" ;;
            captain)  : ;;
          esac
        fi
      fi
      echo down > "$STATE/state-$name"
      echo "$downc" > "$STATE/downc-$name"
    fi
  done
  sleep "$POLL"
done
