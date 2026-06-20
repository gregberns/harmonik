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
#   NO --session-id, had no stable id; the keeper could only inject the advisory
#   warn text but could not rebind after a clear (no session to --resume). Minting
#   --session-id here mirrors the crew model: the keeper can checkpoint the captain
#   via restart-now (internal/keeper/injector.go) and resume the same conversation.
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
#   CAP_WARN_ABS — $CAP_WARN_ABS, or 200000  (keeper --warn-abs-tokens; absolute-token
#                  thresholds; on a 1M window the percentage flags were inert because abs
#                  caps (200k/215k) always won — pass abs tokens directly for an
#                  unambiguous band. hk-5da7.)
#   CAP_ACT_ABS  — $CAP_ACT_ABS,  or 215000  (keeper --act-abs-tokens)

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
CAP_WARN_ABS="${CAP_WARN_ABS:-200000}"   # keeper --warn-abs-tokens
CAP_ACT_ABS="${CAP_ACT_ABS:-215000}"     # keeper --act-abs-tokens

# Mint a stable session id ONCE; the keeper's clear→resume cycle re-binds to it.
# Lowercase it: macOS uuidgen emits uppercase, but the keeper's identity gate
# (isPrimarySID, internal/keeper/sessionid.go) only trusts a lowercase UUIDv4, and
# the self-heal respawn below re-launches with --resume "$SID" — both want the same
# canonical lowercase id so the resume matches and the .sid binding stays primary.
SID="$(uuidgen | tr '[:upper:]' '[:lower:]')"

echo "captain-launch: name=$CAP_NAME tmux=$CAP_TMUX session_id=$SID warn_abs=$CAP_WARN_ABS act_abs=$CAP_ACT_ABS project=$HK_PROJECT"

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

# 3) Generate the dead-pane self-heal respawn script (hk-opuv). When the captain
#    claude session DIES — its tmux pane drops to a shell prompt — the armed keeper's
#    idle-respawn path (internal/keeper/watcher.go maybeRespawn) runs this command
#    via `sh -c`. It relaunches ONLY the captain agent pane, resuming the SAME
#    session-id (--resume "$SID", NOT a fresh --session-id that would fork a new
#    conversation), and refreshes captain.pid so the daemon orphan sweep keeps
#    skipping the freshly-relaunched session. It deliberately does NOT arm a second
#    keeper: the keeper that runs this script is the only one (no dup keeper).
#    The script is generated (not inlined into the keeper argv) to avoid nesting the
#    respawn command's quotes inside the keeper's own tmux-new-session string.
RESPAWN_SCRIPT="$COGNITION_DIR/captain-respawn.sh"
cat > "$RESPAWN_SCRIPT" <<EOF
#!/usr/bin/env bash
# captain-respawn.sh — GENERATED by captain-launch.sh (hk-opuv). DO NOT EDIT.
# Dead-pane self-heal: relaunch ONLY the captain agent pane, resuming the SAME
# session-id. Does NOT arm a keeper (the keeper that runs this is the only one).
set -euo pipefail
tmux kill-session -t "$CAP_TMUX" 2>/dev/null || true
tmux new-session -d -s "$CAP_TMUX" -e "HARMONIK_AGENT=$CAP_NAME" 'claude --dangerously-skip-permissions --remote-control $CAP_NAME --resume $SID'
tmux display-message -p -t "$CAP_TMUX:" '#{pane_pid}' > "$COGNITION_DIR/captain.pid"
EOF
chmod +x "$RESPAWN_SCRIPT"

# 3b) Generate the VERIFIED self-restart entrypoint (hk-9mpk / hk-uldg). The
#    keeper's AUTOMATIC ACT-cycle restart-now is in-process (internal/keeper/
#    restartnow.go) and not externally overridable — that is the documented
#    hk-vpnp boundary. But every EXPLICIT restart the captain (or the operator)
#    fires MUST route through keeper-restart-verified.sh, NOT bare
#    `harmonik keeper restart-now`: the wrapper fires restart-now, parses the
#    printed nonce=rn-..., then runs `await-ack --kind restart` in a SEPARATE OS
#    process that SURVIVES the captain's /clear — turning an ASSUMED restart into
#    a VERIFIED-came-back-up one (exit 3 + session_keeper_ack_timeout if the ACK
#    never lands). This generated helper pre-binds $CAP_NAME and $HK_PROJECT so
#    the captain fires ONE command, and resolves the wrapper by the repo
#    scripts/captain-tools/ path (dirname "$0") — no out-of-git dependency. The
#    wrapper's contract is `<agent> [--project DIR] [--timeout] [--poll]`; extra
#    args (e.g. --timeout) pass through.
VERIFIED_RESTART_WRAPPER="$(cd "$(dirname "$0")" && pwd)/keeper-restart-verified.sh"
RESTART_SCRIPT="$COGNITION_DIR/captain-restart-verified.sh"
cat > "$RESTART_SCRIPT" <<EOF
#!/usr/bin/env bash
# captain-restart-verified.sh — GENERATED by captain-launch.sh (hk-9mpk). DO NOT EDIT.
# VERIFIED self-restart: route the captain's restart-now through the await-ack
# wrapper so the restart is confirmed-landed, not assumed. Agent + project are
# pre-bound; any extra args (e.g. --timeout 45s) pass through to the wrapper.
exec "$VERIFIED_RESTART_WRAPPER" "$CAP_NAME" --project "$HK_PROJECT" "\$@"
EOF
chmod +x "$RESTART_SCRIPT"

# 4) Arm the session-keeper AFTER the captain is up. The keeper drives the
#    in-process handoff→/clear→/session-resume cycle against this tmux session.
#    --warn-abs-tokens/--act-abs-tokens are passed explicitly: on a 1M window the
#    percentage flags were inert because the abs caps (200k/215k) always won, so we
#    pass abs tokens directly for an unambiguous band (hk-5da7).
#    --respawn-cmd wires the dead-pane self-heal: the keeper runs RESPAWN_SCRIPT
#    once the gauge goes stale and the pane is at a shell prompt (90s cooldown).
#    Keeper session stays on hk-<name> prefix (outside harmonik-<hash>-* sweep namespace).
tmux new-session -d -s "hk-keeper-$CAP_NAME" \
  "harmonik keeper --agent \"$CAP_NAME\" --tmux \"$CAP_TMUX\" --warn-abs-tokens $CAP_WARN_ABS --act-abs-tokens $CAP_ACT_ABS --respawn-cmd \"$RESPAWN_SCRIPT\""

echo "captain-launch: captain up in tmux '$CAP_TMUX' (session_id $SID, pane_pid $CAPTAIN_PID); keeper armed in tmux 'hk-keeper-$CAP_NAME' (warn_abs $CAP_WARN_ABS / act_abs $CAP_ACT_ABS)."
echo "captain-launch: sentinel written to $COGNITION_DIR/captain.sentinel; daemon orphan sweep will skip '$CAP_TMUX' while PID $CAPTAIN_PID is live (PL-006d ii)."
echo "captain-launch: self-heal respawn wired — keeper will relaunch '$CAP_TMUX' via $RESPAWN_SCRIPT (--resume $SID, agent-pane only, no dup keeper) if the captain pane dies."
echo "captain-launch: NOTE — a stable --session-id is what lets the keeper's clear→resume cycle survive (mirrors the crew model)."
echo "captain-launch: VERIFIED RESTART wired — any explicit captain restart (the WARN-required self-restart, or an operator restart) routes through the await-ack wrapper, NOT bare 'harmonik keeper restart-now', so the restart is confirmed-landed:"
echo "  $RESTART_SCRIPT            # pre-bound: agent=$CAP_NAME project=$HK_PROJECT"
echo "  (equivalent to: $VERIFIED_RESTART_WRAPPER \"$CAP_NAME\" --project \"$HK_PROJECT\")"
