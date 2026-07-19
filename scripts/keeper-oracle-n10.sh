#!/usr/bin/env bash
set -euo pipefail

# keeper-oracle-n10.sh — census Acceptance Oracle condition 1 for the keeper
# vertical (T13; measurement-design §6.1, §7 metric 9).
#
# Runs the keeper + keepertwin + keepertest suites N consecutive times with
# -count=1 and reports all-green. Replay is deterministic (fake ClockPort,
# virtual time), so ANY flake across the N runs is itself a finding, not
# noise — the script fails fast on the first red iteration.
#
# Zero-daemon, zero-token: `go test` only. No LLM, no tmux (L3 is env-gated
# off by default), no `harmonik` binary. This is what T14 invokes.
#
# Usage:
#   scripts/keeper-oracle-n10.sh [N]      # default N=10 (or $KEEPER_ORACLE_N)
#
# Exit 0: N consecutive green. Exit 1: a red iteration (reported).

N="${1:-${KEEPER_ORACLE_N:-10}}"
case "$N" in
    ''|*[!0-9]*) echo "keeper-oracle-n10: N must be a positive integer, got '$N'" >&2; exit 2 ;;
esac
if [ "$N" -lt 1 ]; then
    echo "keeper-oracle-n10: N must be >= 1, got $N" >&2
    exit 2
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

PACKAGES="./internal/keeper/... ./internal/keepertest/... ./internal/keepertwin/..."

echo "keeper-oracle: N=$N consecutive runs over: $PACKAGES"
start_all=$(date +%s)
i=1
while [ "$i" -le "$N" ]; do
    start=$(date +%s)
    # shellcheck disable=SC2086 # PACKAGES is a deliberate word-split list
    if ! go test -count=1 $PACKAGES; then
        echo "keeper-oracle: RED at iteration $i/$N — deterministic replay flaked or regressed (a finding, not noise)" >&2
        exit 1
    fi
    end=$(date +%s)
    echo "keeper-oracle: iteration $i/$N green ($((end - start))s)"
    i=$((i + 1))
done
end_all=$(date +%s)
echo "keeper-oracle: ALL GREEN — $N/$N consecutive ($((end_all - start_all))s total)"
