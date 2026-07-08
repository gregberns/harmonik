#!/usr/bin/env bash
# run-st5-scenario.sh — end-to-end runner for the ST5 merge-race DOT-workflow
# scenario (bead hk-psrnc).
#
# Runs TestScenario_MergeRace_ST5 from test/scenario/. That test boots
# harmonik daemon.Start in-process against its own t.TempDir() — it NEVER
# touches the fleet daemon. guard_path / assert_not_supervised (adapted from
# scripts/scratch-daemon.sh) are applied to a temporary directory as
# defense-in-depth before handing off to go test.
#
# Requirements:
#   - br on PATH (used by the Go test for bead lifecycle)
#   - go toolchain on PATH
#
# Usage:
#   ./scripts/run-st5-scenario.sh
#   SCENARIO_TIMEOUT=600s ./scripts/run-st5-scenario.sh
#
# Exit: 0 on PASS, 1 on FAIL or precondition error.
#
# Bead: hk-psrnc (ST5 merge-race DOT-workflow zero-token end-to-end proof).

set -euo pipefail

echo "[st5] SAFETY: daemon.Start is in-process against t.TempDir(); fleet daemon is never touched." >&2

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"

# ── Fleet-safety helpers (adapted from scripts/scratch-daemon.sh) ─────────────

die() { echo "[st5] ERROR: $*" >&2; exit 1; }

# fleet_root: canonical repo root of THIS script's own checkout.
fleet_root() {
    local d
    d="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel 2>/dev/null)" || return 0
    [ -n "$d" ] || return 0
    ( cd "$d" && pwd -P )
}

scratch_bin() { echo "$1/.harmonik/bin/harmonik"; }

# guard_path: reject empty, "/", or the fleet root so a typo can never operate
# on the live fleet checkout. Echoes the resolved path on success.
guard_path() {
    local p="${1:-}"
    [ -n "$p" ] || die "scratch-path is required"
    case "$p" in /) die "refusing to operate on '/'" ;; esac
    local resolved
    if [ -d "$p" ]; then
        resolved="$( cd "$p" && pwd -P )"
    else
        local parent leaf
        parent="$(dirname "$p")"; leaf="$(basename "$p")"
        [ -d "$parent" ] || die "parent directory of '$p' does not exist"
        resolved="$( cd "$parent" && pwd -P )/$leaf"
    fi
    local fleet
    fleet="$(fleet_root)"
    if [ -n "$fleet" ] && [ "$resolved" = "$fleet" ]; then
        die "refusing: '$resolved' is the fleet checkout — use a separate scratch path"
    fi
    echo "$resolved"
}

# assert_not_supervised: refuse if the project has a live supervisor session.
# Skips silently when the scratch binary is absent (e.g. a fresh temp dir).
assert_not_supervised() {
    local scratch="$1" bin hash
    bin="$(scratch_bin "$scratch")"
    [ -x "$bin" ] || return 0
    hash="$("$bin" project-hash --project "$scratch" 2>/dev/null)" || return 0
    [ -n "$hash" ] || return 0
    if tmux has-session -t "hk-${hash}-supervise" 2>/dev/null; then
        die "refusing: '$scratch' has a live supervisor session (hk-${hash}-supervise)"
    fi
}

# ── Pre-flight ────────────────────────────────────────────────────────────────

REPO_ROOT="$(git -C "$SELF_DIR" rev-parse --show-toplevel 2>/dev/null)" \
    || die "not inside a git repository"

command -v br >/dev/null 2>&1 \
    || die "br not on PATH — required by TestScenario_MergeRace_ST5"

command -v go >/dev/null 2>&1 \
    || die "go not on PATH"

# Apply guard_path + assert_not_supervised to a temporary scratch directory.
# This confirms the helpers work and that the temp path is not the fleet root.
SCRATCH_TMP="$(mktemp -d "/tmp/hk-st5.XXXXXX")"
trap 'rm -rf "$SCRATCH_TMP"' EXIT INT TERM
SCRATCH="$(guard_path "$SCRATCH_TMP")"
assert_not_supervised "$SCRATCH"

# ── Run ───────────────────────────────────────────────────────────────────────

echo "[st5] running TestScenario_MergeRace_ST5 from $REPO_ROOT" >&2

cd "$REPO_ROOT"
exec go test \
    -tags=scenario \
    -run "^TestScenario_MergeRace_ST5$" \
    -timeout "${SCENARIO_TIMEOUT:-300s}" \
    -v \
    ./test/scenario/
