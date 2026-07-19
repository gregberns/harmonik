#!/opt/homebrew/bin/bash
# agent-session-watchdog.sh — external liveness watchdog for keeper-watched agents
# NOTE: interpreter pinned to Homebrew bash 5.x (NOT /usr/bin/env bash). This script
# uses `declare -A` associative arrays under `set -u`; macOS's default /usr/bin/bash
# is 3.2 where `declare -A` silently fails and expansions like ${LAST_RESPAWN[$name]:-0}
# throw "unbound variable", crashing the watchdog. env-bash resolved to 3.2 when
# launched detached (no Homebrew in PATH) — hence the recurring watchdog death.
# whose tmux session can be destroyed by a crash or the daemon orphan sweep and
# which have NO armed keeper respawn (see hk-dtpoo). It checks `tmux has-session`
# and relaunches via the supported `harmonik crew start` path (which re-writes the
# crew-registry record => sweep protection) plus a bare keeper.
#
# Interim mitigation only. The durable fix (arm keeper --respawn-cmd + fix keeper
# pane-liveness + first-class admiral launch) is tracked in hk-dtpoo / hk-d8dj0.
#
# Trigger-independent: covers process crash, orphan-sweep kill, and force-clear death.

set -u

PROJECT="/Users/gb/github/harmonik"
HASH="a3dc45482890"
CHECK_INTERVAL=90          # seconds between liveness checks
RESPAWN_COOLDOWN=180       # min seconds between respawns of the SAME agent
CRASHLOOP_WINDOW=1800      # 30 min
CRASHLOOP_MAX=5            # > this many respawns in the window => back off + alert
LOG="$PROJECT/.harmonik/keeper/agent-session-watchdog.log"

# agents to guard: "name:mission-path"
AGENTS=(
  "admiral:$PROJECT/.harmonik/crew/missions/admiral.md"
  "commodore:$PROJECT/.harmonik/crew/missions/commodore.md"
)

cd "$PROJECT" || exit 1
export PATH="$HOME/go/bin:$PATH"

log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $*" >> "$LOG"; }

# per-agent state (bash 3.2 friendly: parallel assoc via files under a tmpdir)
declare -A LAST_RESPAWN
declare -A RESPAWN_TIMES   # space-separated epoch list within window

log "watchdog START pid=$$ interval=${CHECK_INTERVAL}s agents=[${AGENTS[*]%%:*}]"

now() { date +%s; }

respawn() {
  local name="$1" mission="$2"
  local session="harmonik-${HASH}-crew-${name}"
  # crash-loop guard: prune respawn timestamps outside the window
  local n; n=$(now)
  local kept="" cnt=0
  for t in ${RESPAWN_TIMES[$name]:-}; do
    if [ $(( n - t )) -lt $CRASHLOOP_WINDOW ]; then kept="$kept $t"; cnt=$((cnt+1)); fi
  done
  if [ "$cnt" -ge "$CRASHLOOP_MAX" ]; then
    log "CRASHLOOP $name: $cnt respawns in ${CRASHLOOP_WINDOW}s — backing off, NOT respawning. Alerting operator."
    harmonik comms send --to operator --from watchdog --topic error -- \
      "WATCHDOG: $name is CRASH-LOOPING ($cnt respawns/30min). Backed off — not respawning further. Needs manual look (likely the underlying claude crash cause; see hk-dtpoo)." >/dev/null 2>&1
    RESPAWN_TIMES[$name]="$kept"
    return 1
  fi
  log "RESPAWN $name: session $session absent — crew start + keeper"
  harmonik crew start "$name" --mission "$mission" >> "$LOG" 2>&1
  sleep 4
  if ! pgrep -f "harmonik keeper --agent $name" >/dev/null 2>&1; then
    nohup harmonik keeper --agent "$name" >/dev/null 2>&1 &
    log "RESPAWN $name: keeper restarted"
  fi
  RESPAWN_TIMES[$name]="$kept $n"
  LAST_RESPAWN[$name]=$n
  harmonik comms send --to operator --from watchdog --topic status -- \
    "WATCHDOG: $name session was down — relaunched (registry-protected crew + keeper). Conversation transcript remains on disk for --resume." >/dev/null 2>&1
}

while true; do
  for entry in "${AGENTS[@]}"; do
    name="${entry%%:*}"; mission="${entry#*:}"
    session="harmonik-${HASH}-crew-${name}"
    if tmux has-session -t "$session" 2>/dev/null; then
      continue
    fi
    # session absent — respect per-agent cooldown
    last="${LAST_RESPAWN[$name]:-0}"
    if [ $(( $(now) - last )) -lt $RESPAWN_COOLDOWN ]; then
      log "SKIP $name: session absent but within respawn cooldown"
      continue
    fi
    respawn "$name" "$mission"
  done
  sleep "$CHECK_INTERVAL"
done
