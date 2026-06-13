#!/usr/bin/env bash
# hk-supervise.sh — Out-of-band shell fallback for harmonik daemon supervision.
#
# Launches the harmonik daemon in a per-project tmux session named
# hk-<project_hash>-daemon-supervise (PL-006a / ON-058c). The session uses
# the `hk-` prefix, deliberately OUTSIDE the `harmonik-<hash>-` orphan-sweep
# namespace, so no PL-006d sentinel exemption is required.
#
# The in-binary `harmonik supervise` is the canonical per-project supervisor.
# This script is the supported out-of-band shell fallback for environments
# where the binary supervisor is unavailable or in recovery. Any hand-authored
# /tmp/hk-daemon-supervise.sh artifact is legacy and NOT part of the supported
# surface (specs/process-lifecycle.md C3 NOTE after §PL-019).
#
# Usage:
#   ./scripts/hk-supervise.sh [/path/to/project] [max-concurrent]
#
# Or via env vars:
#   HK_PROJECT=/path/to/project HK_CONCURRENCY=6 ./scripts/hk-supervise.sh
#
# Defaults:
#   HK_PROJECT        — first positional arg, or $HK_PROJECT, or CWD
#   HK_CONCURRENCY    — second positional arg, or $HK_CONCURRENCY, or 6
#   HK_LOG            — $HK_LOG, or /tmp/hk-<project_hash>-daemon.log  (ON-058c)
#   HK_SESS           — $HK_SESS, or hk-<project_hash>-daemon-supervise (ON-058c)
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
#     ./scripts/hk-supervise.sh
#
#   Alternatively, add .harmonik/branching.yaml to the repo (no flags needed):
#     version: 1
#     defaults:
#       lands_on: integration
#       protect_branches: [main]

set -euo pipefail

PROJ="${1:-${HK_PROJECT:-$(pwd)}}"
CONCURRENCY="${2:-${HK_CONCURRENCY:-6}}"

# Derive per-project hash (ON-058c/ON-058e) for hash-qualified /tmp resources.
# Guard: stale binary lacking `project-hash` degrades to unqualified names.
_HASH="$(harmonik project-hash --project "$PROJ" 2>/dev/null || true)"
LOG="${HK_LOG:-/tmp/hk-${_HASH:+${_HASH}-}daemon.log}"
SESS="${HK_SESS:-hk-${_HASH:+${_HASH}-}daemon-supervise}"

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

echo "hk-supervise: project=$PROJ concurrency=$CONCURRENCY log=$LOG sess=$SESS${BRANCH_FLAGS:+ branch_flags=$BRANCH_FLAGS}"

# Kill any existing session with this name before relaunching.
tmux kill-session -t "$SESS" 2>/dev/null || true
# Remove stale socket only after the old session is gone.
rm -f "$PROJ/.harmonik/daemon.sock"

# Strips ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN so the daemon bills the
# subscription, not the API credit pool (see codename:credfence).
tmux new-session -d -s "$SESS" \
  "env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN \
    harmonik --project $PROJ --no-auto-pull --max-concurrent $CONCURRENCY $BRANCH_FLAGS \
    2>&1 | tee -a $LOG"

echo "hk-supervise: daemon launched in session '$SESS'"
