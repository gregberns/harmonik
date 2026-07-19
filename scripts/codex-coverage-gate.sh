#!/usr/bin/env bash
set -euo pipefail

# codex-coverage-gate.sh — measured coverage floor on the structured Codex
# input-driver reactor files (T9; harness-acceptance-design §"Coverage floor").
#
# Measures per-FILE statement coverage of the driver reactor files
# (codexinput/reactor.go, codexdriver/driver.go, codexdriver/session.go) from an
# in-package `go test -coverprofile` run over the driver packages, and gates each
# file against its RATIFIED floor in scripts/codex-coverage-floor.baseline. The
# floor is measured-and-stated (a ratchet), not "suite passes".
#
# Per-file statement coverage is computed from the raw coverprofile blocks
# (covered-statements / total-statements per file). Zero-daemon, zero-token.
#
# Usage:
#   scripts/codex-coverage-gate.sh [existing-coverprofile]
#     With no argument, runs:
#       go test -count=1 -coverprofile=<tmp> ./internal/codexinput/... ./internal/codexdriver/...
#
# Exit 0: every gated file >= its floor. Exit 1: a file dropped below floor.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BASELINE="$REPO_ROOT/scripts/codex-coverage-floor.baseline"
[ -f "$BASELINE" ] || { echo "codex-coverage-gate: missing $BASELINE" >&2; exit 2; }

cd "$REPO_ROOT"

PROFILE="${1:-}"
CLEANUP=""
if [ -z "$PROFILE" ]; then
    PROFILE="$(mktemp -t codex-cov)"
    CLEANUP="$PROFILE"
    echo "codex-coverage-gate: measuring (go test -count=1 -coverprofile ./internal/codexinput/... ./internal/codexdriver/...)"
    go test -count=1 -coverprofile="$PROFILE" ./internal/codexinput/... ./internal/codexdriver/... >/dev/null
fi
[ -s "$PROFILE" ] || { echo "codex-coverage-gate: empty/missing profile $PROFILE" >&2; exit 2; }

# Per-file coverage from raw profile blocks, gated against the baseline floors.
# Baseline keys are paths under internal/ (e.g. codexinput/reactor.go).
FAIL=0
while read -r file floor; do
    case "$file" in ''|\#*) continue ;; esac
    line=$(awk -F: -v want="$file" '
        NR > 1 {
            f = $1; sub(".*/internal/", "", f)
            if (f != want) next
            split($2, a, " ")           # a[2]=numstmt a[3]=hitcount
            tot += a[2]; if (a[3] > 0) cov += a[2]
        }
        END {
            if (tot == 0) { print "MISSING" }
            else printf "%.1f %d %d", 100 * cov / tot, cov, tot
        }' "$PROFILE")
    if [ "$line" = "MISSING" ] || [ -z "$line" ]; then
        echo "codex-coverage-gate: FAIL $file — no statements in profile (file deleted/renamed? update the baseline deliberately)" >&2
        FAIL=1
        continue
    fi
    pct=${line%% *}
    rest=${line#* }
    if awk -v got="$pct" -v floor="$floor" 'BEGIN { exit !(got + 0 < floor + 0) }'; then
        echo "codex-coverage-gate: FAIL $file — measured ${pct}% (${rest% *}/${rest#* } stmts) < ratified floor ${floor}%" >&2
        FAIL=1
    else
        echo "codex-coverage-gate: OK   $file — measured ${pct}% (${rest% *}/${rest#* } stmts) >= floor ${floor}%"
    fi
done < "$BASELINE"

[ -n "$CLEANUP" ] && rm -f "$CLEANUP"

if [ "$FAIL" -ne 0 ]; then
    echo "codex-coverage-gate: coverage floor REGRESSED" >&2
    exit 1
fi
echo "codex-coverage-gate: all floors held"
