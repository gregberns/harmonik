#!/usr/bin/env bash
set -euo pipefail

# keeper-coverage-gate.sh — measured coverage floor on the NEW keeper reactor
# files (T13; measurement-design §6.4; census Acceptance Oracle condition 4).
#
# Measures per-FILE statement coverage of the new Step/reactor files
# (step.go, shell.go, ports.go — the T6/T7 rebuild) from an in-package
# `go test -coverprofile` run over ./internal/keeper/..., and gates each file
# against its RATIFIED floor in scripts/keeper-coverage-floor.baseline.
# The floor is measured-and-stated (a ratchet), not "suite passes".
#
# Per-file statement coverage is computed from the raw coverprofile blocks
# (covered-statements / total-statements per file), not `go tool cover -func`
# (which is per-function). Zero-daemon, zero-token, deterministic.
#
# Usage:
#   scripts/keeper-coverage-gate.sh [existing-coverprofile]
#     With no argument, runs:
#       go test -count=1 -coverprofile=<tmp> ./internal/keeper/...
#
# Exit 0: every gated file >= its floor. Exit 1: a file dropped below floor.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BASELINE="$REPO_ROOT/scripts/keeper-coverage-floor.baseline"
[ -f "$BASELINE" ] || { echo "keeper-coverage-gate: missing $BASELINE" >&2; exit 2; }

cd "$REPO_ROOT"

PROFILE="${1:-}"
CLEANUP=""
if [ -z "$PROFILE" ]; then
    PROFILE="$(mktemp -t keeper-cov)"
    CLEANUP="$PROFILE"
    echo "keeper-coverage-gate: measuring (go test -count=1 -coverprofile ./internal/keeper/...)"
    go test -count=1 -coverprofile="$PROFILE" ./internal/keeper/... >/dev/null
fi
[ -s "$PROFILE" ] || { echo "keeper-coverage-gate: empty/missing profile $PROFILE" >&2; exit 2; }

# Per-file coverage from raw profile blocks, gated against the baseline floors.
FAIL=0
while read -r file floor; do
    case "$file" in ''|\#*) continue ;; esac
    line=$(awk -F: -v want="$file" '
        NR > 1 {
            f = $1; sub(".*/internal/keeper/", "", f)
            if (f != want) next
            split($2, a, " ")           # a[2]=numstmt a[3]=hitcount
            tot += a[2]; if (a[3] > 0) cov += a[2]
        }
        END {
            if (tot == 0) { print "MISSING" }
            else printf "%.1f %d %d", 100 * cov / tot, cov, tot
        }' "$PROFILE")
    if [ "$line" = "MISSING" ] || [ -z "$line" ]; then
        echo "keeper-coverage-gate: FAIL $file — no statements in profile (file deleted/renamed? update the baseline deliberately)" >&2
        FAIL=1
        continue
    fi
    pct=${line%% *}
    rest=${line#* }
    if awk -v got="$pct" -v floor="$floor" 'BEGIN { exit !(got + 0 < floor + 0) }'; then
        echo "keeper-coverage-gate: FAIL $file — measured ${pct}% (${rest% *}/${rest#* } stmts) < ratified floor ${floor}%" >&2
        FAIL=1
    else
        echo "keeper-coverage-gate: OK   $file — measured ${pct}% (${rest% *}/${rest#* } stmts) >= floor ${floor}%"
    fi
done < "$BASELINE"

[ -n "$CLEANUP" ] && rm -f "$CLEANUP"

if [ "$FAIL" -ne 0 ]; then
    echo "keeper-coverage-gate: coverage floor REGRESSED" >&2
    exit 1
fi
echo "keeper-coverage-gate: all floors held"
