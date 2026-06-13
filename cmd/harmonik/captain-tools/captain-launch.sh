#!/usr/bin/env bash
# captain-launch.sh — launch the harmonik Captain LLM session with a STABLE,
# caller-minted --session-id, then arm the session-keeper against it.
#
# WHY the --session-id matters (restart-continuity, load-bearing):
#   The captain is a `claude --remote-control` session, exactly like a crew. The
#   session-keeper's in-process wind-down cycle (handoff → /clear → /session-resume,
#   internal/keeper/cycle.go) can only REBIND to the same conversation if the
#   session has a STABLE id to `--resume`. Crews mint+reuse a uuid via
#   `harmonik crew start` (internal/daemon/crewstart.go resolveSessionID) — that is
#   exactly what lets a keeper-cycled crew come back as the same session. The
#   captain, historically launched as a bare `claude --remote-control captain` with
#   NO --session-id, had no stable id, so the keeper could only ever WARN it (the
#   non-destructive warn injection, internal/keeper/injector.go, whose text ends
#   "…then run /quit"); when the captain obeyed /quit it exited and nothing
#   respawned it → dead and stayed dead. Minting --session-id here mirrors the crew
#   model and gives the keeper a session to rebind.
#
# WHY captain.sentinel + captain.pid matter (orphan-sweep exclusion, load-bearing):
#   The daemon's orphan sweep (internal/daemon/orphansweep.go probeCaptainSentinel)
#   kills all harmonik-<hash>-* tmux sessions that look orphaned. Without a sentinel
#   the captain's harmonik-<hash>-captain session is indistinguishable from a dead
#   implementer session and gets reaped. Writing captain.sentinel + captain.pid to
#   .harmonik/cognition/ (same directory as supervisor.sentinel) tells the sweep to
#   skip the captain session when that PID is still live (PL-006d mechanism ii).
#
# Usage:
#   captain-launch.sh [tmux-session-name]
#
# Required environment (no default — must be set by the caller):
#   HK_PROJECT — absolute path to the harmonik project directory.
#                Example: export HK_PROJECT=/path/to/harmonik
#
# Optional overrides:
#   CAP_TMUX   — first positional arg, or $CAP_TMUX, or harmonik-<hash>-captain
#   CAP_NAME   — $CAP_NAME, or "captain"  (the --remote-control / comms identity)
#   CAP_WARN   — $CAP_WARN, or 30   (keeper --warn-pct; bare 80/90 defeats intent on a 1M window)
#   CAP_ACT    — $CAP_ACT,  or 35   (keeper --act-pct)

set -euo pipefail

# HK_PROJECT is required — no hardcoded default so this script is portable across
# machines and users. Set it in your shell profile or before invoking this script.
: "${HK_PROJECT:?HK_PROJECT must be set to the harmonik project directory (e.g. export HK_PROJECT=/path/to/harmonik)}"

# Compute the project-scoped hash (PL-006a: first 12 hex chars of SHA-256(realpath)).
# Used to qualify the captain tmux session name so it lives in the same
# harmonik-<hash>-* namespace as all other daemon-managed sessions.
PROJ_HASH="$(harmonik project-hash --project "$HK_PROJECT")"

CAP_TMUX="${1:-${CAP_TMUX:-harmonik-${PROJ_HASH}-captain}}"
CAP_NAME="${CAP_NAME:-captain}"
CAP_WARN="${CAP_WARN:-30}"
CAP_ACT="${CAP_ACT:-35}"

# Mint a stable session id ONCE; the keeper's clear→resume cycle re-binds to it.
SID="$(uuidgen)"

echo "captain-launch: name=$CAP_NAME tmux=$CAP_TMUX session_id=$SID warn=$CAP_WARN act=$CAP_ACT project=$HK_PROJECT"

# 1) Launch the captain in its own tmux session with the MINTED --session-id.
#    Interactive, remote-controllable (operator can watch at claude.ai/code).
#    HARMONIK_AGENT is set explicitly so keeper-statusline.sh uses the correct
#    name without relying on the tmux-name fallback or a hardcoded default in
#    ~/.claude/settings.json (hk-67k: ctx-pollution fix follow-up).
tmux new-session -d -s "$CAP_TMUX" -e "HARMONIK_AGENT=$CAP_NAME" \
  "claude --dangerously-skip-permissions --remote-control \"$CAP_NAME\" --session-id \"$SID\""

# 2) Write captain.sentinel and captain.pid to .harmonik/cognition/ so the daemon's
#    orphan sweep skips the captain session while it is live (PL-006d mechanism ii).
#    The sentinel content mirrors supervisor.sentinel (schema_version=1).
#    The PID is the first pane's process PID — alive for the duration of the session.
COGNITION_DIR="$HK_PROJECT/.harmonik/cognition"
mkdir -p "$COGNITION_DIR"
printf 'schema_version=1\n' > "$COGNITION_DIR/captain.sentinel"
CAPTAIN_PID="$(tmux display-message -p -t "${CAP_TMUX}:" "#{pane_pid}")"
printf '%s\n' "$CAPTAIN_PID" > "$COGNITION_DIR/captain.pid"

# 3) Arm the session-keeper AFTER the captain is up. The keeper drives the
#    in-process handoff→/clear→/session-resume cycle against this tmux session.
#    --warn-pct/--act-pct must be passed explicitly (bare defaults are 80/90).
#    Keeper session stays on hk-<name> prefix (outside harmonik-<hash>-* sweep namespace).
tmux new-session -d -s "hk-keeper-$CAP_NAME" \
  "harmonik keeper --agent \"$CAP_NAME\" --tmux \"$CAP_TMUX\" --warn-pct $CAP_WARN --act-pct $CAP_ACT"

echo "captain-launch: captain up in tmux '$CAP_TMUX' (session_id $SID, pane_pid $CAPTAIN_PID); keeper armed in tmux 'hk-keeper-$CAP_NAME' (warn $CAP_WARN / act $CAP_ACT)."
echo "captain-launch: sentinel written to $COGNITION_DIR/captain.sentinel; daemon orphan sweep will skip '$CAP_TMUX' while PID $CAPTAIN_PID is live (PL-006d ii)."
echo "captain-launch: NOTE — a stable --session-id is what lets the keeper's clear→resume cycle survive (mirrors the crew model)."
