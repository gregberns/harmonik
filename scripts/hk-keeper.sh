#!/usr/bin/env bash
# hk-keeper.sh — Robust harmonik daemon keeper.
#
# Runs OUTSIDE tmux so it survives tmux-session kills. Launches the daemon
# inside a non-"harmonik"-prefixed tmux session ("hkdkeeper") so an over-broad
# `harmonik-*` orphan sweep can't kill it.
#
# Strips ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN so the daemon bills the
# subscription, not the API credit pool (see codename:credfence).
#
# Usage:
#   ./scripts/hk-keeper.sh [/path/to/project] [max-concurrent]
#
# Or via env vars:
#   HK_PROJECT=/path/to/project HK_CONCURRENCY=6 ./scripts/hk-keeper.sh
#
# Defaults:
#   HK_PROJECT        — first positional arg, or $HK_PROJECT, or CWD
#   HK_CONCURRENCY    — second positional arg, or $HK_CONCURRENCY, or 6
#   HK_LOG            — $HK_LOG, or /tmp/hk-daemon.log
#   HK_SESS           — $HK_SESS, or hkdkeeper
#
# Work-project deployment (repos where main must never be auto-pushed):
#   Set HK_TARGET_BRANCH and HK_PROTECT_BRANCH to engage integration-branch mode.
#   HK_TARGET_BRANCH  — daemon merges/pushes here instead of main (e.g. "integration")
#   HK_PROTECT_BRANCH — deny-list branch; daemon fail-closes any run targeting it (e.g. "main")
#
#   Example:
#     HK_PROJECT=/path/to/repo \
#     HK_TARGET_BRANCH=integration \
#     HK_PROTECT_BRANCH=main \
#     ./scripts/hk-keeper.sh
#
#   Alternatively, add config/branching.yaml to the repo (no flags needed):
#     protect_branches: [main]
#     target_branch: integration

set -euo pipefail

PROJ="${1:-${HK_PROJECT:-$(pwd)}}"
CONCURRENCY="${2:-${HK_CONCURRENCY:-6}}"
LOG="${HK_LOG:-/tmp/hk-daemon.log}"
SESS="${HK_SESS:-hkdkeeper}"

# Optional work-project integration-branch flags.
TARGET_BRANCH="${HK_TARGET_BRANCH:-}"
PROTECT_BRANCH="${HK_PROTECT_BRANCH:-}"

BRANCH_FLAGS=""
if [[ -n "$TARGET_BRANCH" ]]; then
  BRANCH_FLAGS="$BRANCH_FLAGS --target-branch $TARGET_BRANCH --forbid-default-main"
fi
if [[ -n "$PROTECT_BRANCH" ]]; then
  BRANCH_FLAGS="$BRANCH_FLAGS --protect-branch $PROTECT_BRANCH"
fi

echo "hk-keeper: project=$PROJ concurrency=$CONCURRENCY log=$LOG sess=$SESS${BRANCH_FLAGS:+ branch_flags=$BRANCH_FLAGS}"

while true; do
  # Liveness check = a harmonik daemon PROCESS exists. This covers the
  # restart-backoff window where the process is alive but the socket is not
  # yet open — don't relaunch during that window.
  if pgrep -f "harmonik --project $PROJ" >/dev/null 2>&1; then
    sleep 15
    continue
  fi

  echo "===KEEPER relaunch (no daemon proc) $(date '+%F %T')===" >> "$LOG"
  tmux kill-session -t "$SESS" 2>/dev/null || true
  # Remove stale socket only after confirming the daemon process is gone.
  rm -f "$PROJ/.harmonik/daemon.sock"
  tmux new-session -d -s "$SESS" \
    "env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN \
      harmonik --project $PROJ --no-auto-pull --max-concurrent $CONCURRENCY $BRANCH_FLAGS \
      2>&1 | tee -a $LOG"
  sleep 25
done
