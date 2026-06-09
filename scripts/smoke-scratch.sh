#!/usr/bin/env bash
# smoke-scratch.sh — Run harmonik smoke in a throw-away temp project.
#
# All smoke commits land in the scratch repo; the harmonik main trunk is never
# touched. Addresses logmine F17: real-daemon smoke validation churned trunk
# history with 6 commits + 3 cleanups that netted to zero code on main.
#
# Usage:
#   ./scripts/smoke-scratch.sh [options]
#
# Options (env vars):
#   HARMONIK_BIN    — harmonik binary (default: build from source → /tmp/harmonik-scratch-$$)
#   SMOKE_TIMEOUT   — passed to harmonik smoke --timeout (default: 20m)
#   SKIP_BUILD      — set to 1 to use HARMONIK_BIN as-is without rebuilding
#   KEEP_DIR        — set to 1 to skip temp-dir cleanup (debug)
#
# Exit codes mirror harmonik smoke:
#   0  — PASS (all 5 signals observed)
#   1  — setup or assertion failure
#   2  — timeout
#   17 — daemon not running (startup failure)
#
# Refs: hk-nk9pu (logmine F17 — smoke scratch lane)

set -euo pipefail

REPO_ROOT="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
HARMONIK_BIN="${HARMONIK_BIN:-}"
SMOKE_TIMEOUT="${SMOKE_TIMEOUT:-20m}"
SKIP_BUILD="${SKIP_BUILD:-0}"
KEEP_DIR="${KEEP_DIR:-0}"

SESS="hk-smoke-scratch-$$"
LOG="/tmp/hk-smoke-scratch-$$.log"
SMOKE_DIR=""

cleanup() {
    local exit_code=$?
    echo "[smoke-scratch] cleanup (exit=$exit_code)"
    tmux kill-session -t "$SESS" 2>/dev/null || true
    rm -f "$LOG"
    if [ -n "$SMOKE_DIR" ] && [ -d "$SMOKE_DIR" ] && [ "$KEEP_DIR" != "1" ]; then
        rm -rf "$SMOKE_DIR"
        echo "[smoke-scratch] removed $SMOKE_DIR"
    elif [ -n "$SMOKE_DIR" ] && [ "$KEEP_DIR" = "1" ]; then
        echo "[smoke-scratch] KEEP_DIR=1 — leaving $SMOKE_DIR for inspection"
    fi
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------------------
# Step 1: build harmonik binary.
# ---------------------------------------------------------------------------
if [ "$SKIP_BUILD" = "1" ] && [ -n "$HARMONIK_BIN" ]; then
    echo "[smoke-scratch] using pre-built binary: $HARMONIK_BIN"
else
    BIN="/tmp/harmonik-scratch-$$"
    echo "[smoke-scratch] building harmonik → $BIN"
    go build -C "$REPO_ROOT" -o "$BIN" ./cmd/harmonik
    HARMONIK_BIN="$BIN"
fi

# ---------------------------------------------------------------------------
# Step 2: create throw-away git repo.
# ---------------------------------------------------------------------------
SMOKE_DIR=$(mktemp -d /tmp/hk-smoke-scratch.XXXXXX)
echo "[smoke-scratch] scratch dir: $SMOKE_DIR"

git -C "$SMOKE_DIR" init -q
git -C "$SMOKE_DIR" config user.email "smoke@harmonik.local"
git -C "$SMOKE_DIR" config user.name  "Smoke Runner"
echo "# harmonik smoke scratch repo — $(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$SMOKE_DIR/README.md"
mkdir -p "$SMOKE_DIR/docs"
git -C "$SMOKE_DIR" add -A
git -C "$SMOKE_DIR" commit -q -m "initial"

# ---------------------------------------------------------------------------
# Step 3: harmonik init (creates .harmonik/ + br init + branching.yaml).
# ---------------------------------------------------------------------------
echo "[smoke-scratch] running harmonik init..."
"$HARMONIK_BIN" init "$SMOKE_DIR" --force --no-supervise 2>&1 | sed 's/^/  [init] /'

# Commit the init artifacts so the daemon starts with a clean tree.
git -C "$SMOKE_DIR" add -A
git -C "$SMOKE_DIR" commit -q -m "harmonik init" 2>/dev/null || true

# ---------------------------------------------------------------------------
# Step 4: start scratch daemon in a dedicated tmux session.
# Strips API keys per codename:credfence so it bills the subscription pool.
# ---------------------------------------------------------------------------
echo "[smoke-scratch] starting daemon (session=$SESS)..."
# shellcheck disable=SC2016
tmux new-session -d -s "$SESS" \
    "env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN \
      '$HARMONIK_BIN' --project '$SMOKE_DIR' \
      --no-auto-pull \
      --max-concurrent 1 \
      --workflow-mode review-loop \
      2>&1 | tee '$LOG'"

# Wait up to 45s for the daemon socket.
SOCK="$SMOKE_DIR/.harmonik/daemon.sock"
echo "[smoke-scratch] waiting for daemon socket..."
for i in $(seq 1 45); do
    if [ -S "$SOCK" ]; then
        echo "[smoke-scratch] daemon ready (${i}s)"
        break
    fi
    sleep 1
    if [ "$i" = "45" ]; then
        echo "[smoke-scratch] ERROR: daemon socket not ready after 45s"
        echo "[smoke-scratch] --- daemon log ---"
        cat "$LOG" 2>/dev/null || echo "(empty)"
        exit 17
    fi
done

# ---------------------------------------------------------------------------
# Step 5: run smoke against the scratch project.
# ---------------------------------------------------------------------------
echo "[smoke-scratch] running smoke (timeout=$SMOKE_TIMEOUT)..."
set +e
"$HARMONIK_BIN" smoke --project "$SMOKE_DIR" --timeout "$SMOKE_TIMEOUT"
SMOKE_EXIT=$?
set -e

if [ "$SMOKE_EXIT" = "0" ]; then
    echo "[smoke-scratch] PASS"
else
    echo "[smoke-scratch] FAIL (exit=$SMOKE_EXIT)"
    echo "[smoke-scratch] --- daemon log tail ---"
    tail -40 "$LOG" 2>/dev/null || echo "(empty)"
fi

exit $SMOKE_EXIT
