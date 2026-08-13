#!/usr/bin/env bash
#
# codex-nudge.sh — keep a long-running Codex lane working.
#
# A Codex session stops when it finishes a turn and then waits. Often it has more
# to do and only needs to be told to continue. This watcher detects that state and
# injects one line of text into the session's tmux pane.
#
# Rationale, thresholds and the measurement behind the keyword gate:
#   plans/2026-07-27-delete-and-rewrite/LANES.md §5
#
# STATUS: the detector is verified against the local session records (170 of 174
# July sessions end in a `task_complete` record, and `last_agent_message` is
# present). The INJECTION half has not been run against a live Codex session.
# Watch the first few nudges before leaving it unattended.
#
# The session must run inside tmux. There is no other way to reach it.
#   tmux new-session -d -s crew-<lane> -c <worktree> 'codex'
#
# Usage:
#   scripts/codex-nudge.sh <tmux-target> <lane-name> <repo-path> [rollout-file]
#
# Give the rollout file explicitly when more than one Codex session shares a repo
# path. Without it the watcher picks the most recently written one, which can
# follow the wrong session.
#
# Environment:
#   IDLE_SECS    seconds at the prompt before it counts as stalled (default 300)
#   MAX_NUDGES   consecutive nudges before it escalates instead (default 3)
#   POLL_SECS    seconds between checks (default 60)

set -uo pipefail

if [ $# -lt 3 ]; then
  sed -n '2,30p' "$0" >&2
  exit 64
fi

TARGET=$1
LANE=$2
REPO=$3
PINNED=${4:-}

IDLE_SECS=${IDLE_SECS:-300}
MAX_NUDGES=${MAX_NUDGES:-3}
POLL_SECS=${POLL_SECS:-60}

SESSION_ROOT="$HOME/.codex/sessions"

# The veto. When the agent's last message asks the operator something, a nudge is
# the wrong answer. Measured over 36 stops: this fired on 7, and on all 7 the
# operator's own reply was a decision rather than "keep going". Zero false nudges.
ASK_RE='requires direction|need (your |operator )?(direction|decision|approval|permission|guidance)|blocked (until|on|by)|which (approach|path|option)|permission to|should I|shall I|do you want|let me know'

log() { printf '%s [%s] %s\n' "$(date '+%H:%M:%S')" "$LANE" "$*"; }

if ! tmux has-session -t "$TARGET" 2>/dev/null; then
  log "no tmux session at '$TARGET' — start the Codex lane inside tmux first"
  exit 69
fi

# Take the newest match WITHOUT piping into `head -1`. Under pipefail `head`
# leaves after one line, `ls` upstream dies of SIGPIPE, and the pipeline reports
# that death as its own status. On this machine the glob already matches 226
# session files and `ls -t` emits 24 KB of paths, which is past the size where
# the SIGPIPE actually lands — the shipped pipeline returns 1 while printing the
# right answer. Capture, then slice the first line with no second process.
find_rollout() {
  if [ -n "$PINNED" ]; then
    printf '%s\n' "$PINNED"
    return
  fi
  local matches sorted
  matches=$(grep -ls "\"cwd\":\"$REPO\"" "$SESSION_ROOT"/*/*/*/rollout-*.jsonl 2>/dev/null) || return 0
  [ -n "$matches" ] || return 0
  sorted=$(printf '%s\n' "$matches" | xargs -r ls -t 2>/dev/null) || return 0
  printf '%s\n' "${sorted%%$'\n'*}"
}

# Prints "<record-type>\t<last agent message on one line>".
read_tail() {
  tail -1 "$1" | python3 -c '
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    print("\t"); raise SystemExit          # a partly written line: treat as mid-turn
p = d.get("payload", {}) or {}
msg = (p.get("last_agent_message") or "").replace("\n", " ").replace("\t", " ")
print("%s\t%s" % (p.get("type", ""), msg))
'
}

nudges=0
log "watching $TARGET — stalled after ${IDLE_SECS}s idle, at most $MAX_NUDGES nudges in a row"

while sleep "$POLL_SECS"; do
  if ! tmux has-session -t "$TARGET" 2>/dev/null; then
    log "tmux session gone — stopping"
    exit 0
  fi

  rollout=$(find_rollout)
  if [ -z "$rollout" ] || [ ! -f "$rollout" ]; then
    log "no session record found under $SESSION_ROOT for cwd $REPO"
    continue
  fi

  age=$(( $(date +%s) - $(stat -f %m "$rollout") ))
  if [ "$age" -lt "$IDLE_SECS" ]; then
    nudges=0            # it is working; forget the streak
    continue
  fi

  IFS=$'\t' read -r kind msg < <(read_tail "$rollout")

  # Anything other than task_complete means mid-turn, or an approval dialog the
  # agent is holding. Neither is ours to interrupt. An approval dialog will idle
  # here forever, which is correct: it needs the operator, not a nudge.
  if [ "$kind" != "task_complete" ]; then
    continue
  fi

  # Here-string, not `printf ... | grep -qiE`. This veto is the reason the
  # watcher is safe to leave running: it must fire when the agent is asking YOU
  # something. Piped, `grep -q` leaves on the match and the writer's SIGPIPE
  # becomes the pipeline's status under pipefail, so the veto silently never
  # fires and the nudge goes out over the top of the question. A here-string has
  # no writer process to kill.
  if grep -qiE "$ASK_RE" <<<"$msg"; then
    log "BLOCKED, needs you: ...${msg: -240}"
    continue
  fi

  if [ "$nudges" -ge "$MAX_NUDGES" ]; then
    log "nudged $nudges times and it keeps stopping — needs you: ...${msg: -240}"
    continue
  fi

  tmux load-buffer -b codexnudge - <<EOF
Keep going. Re-read HANDOFF-$LANE.md for where you left off, continue the next unfinished item, and do not redo work that is already done. If you are genuinely blocked on a decision only the operator can make, say so plainly and stop.
EOF
  tmux paste-buffer -b codexnudge -t "$TARGET" -d
  sleep 1
  tmux send-keys -t "$TARGET" Enter

  nudges=$(( nudges + 1 ))
  log "nudged (${nudges}/${MAX_NUDGES}), idle ${age}s"
done
