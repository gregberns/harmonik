#!/usr/bin/env bash
# Re-exec under bash 4+ if the default `bash` on PATH is too old (macOS ships 3.2,
# which lacks `declare -A`). Homebrew installs bash 4+ to /opt/homebrew/bin/bash.
if [[ "${BASH_VERSINFO[0]:-0}" -lt 4 ]]; then
    for candidate in /opt/homebrew/bin/bash /usr/local/bin/bash; do
        if [[ -x "$candidate" ]]; then
            exec "$candidate" "$0" "$@"
        fi
    done
    echo "coverage-gate.sh requires bash >= 4 (associative arrays). Found: ${BASH_VERSION:-unknown}." >&2
    echo "Install via Homebrew: brew install bash" >&2
    exit 1
fi

# coverage-gate.sh — Harmonik coverage enforcement gate (hk-pvcs.5)
#
# RULES (all locked, per STATUS.md "Decisions in force"):
#   1. internal/core/** packages: must reach ≥ 95.0% line coverage.
#   2. All other internal/** packages: must reach ≥ 90.0% floor.
#   3. No package may regress more than 0.3 percentage points below its
#      recorded value in coverage.baseline at the repo root.
#
# INPUTS:
#   $1  — optional path to an existing coverage profile (coverage.out format).
#          If omitted, the script runs:
#            go test -coverprofile=/tmp/harmonik-coverage.$$.out \
#                    -covermode=atomic ./...
#          and uses the generated file.
#
# BASELINE FORMAT (coverage.baseline):
#   One entry per line:
#     <package-path> <coverage-pct>
#   Lines starting with '#' are comments. Empty lines are ignored.
#   Example:
#     github.com/gregberns/harmonik/internal/core/scheduler 97.3
#   The file starts as a comment-only stub (zero packages). As packages land,
#   the CI operator or agent bumps entries via a dedicated "protected rule file"
#   commit (see quality-checks.md §Agent-enforceability). Deleting or lowering
#   an entry without such a commit is detectable by the same tooling that watches
#   rule-file diffs.
#
# DESIGN DECISIONS:
#   - Shell (bash) chosen for MVH; Go rewrite filed as follow-up if logic grows.
#   - Coverage is extracted from the "go tool cover -func" output, which reports
#     per-function coverage and a final total per package. We aggregate to
#     per-package total coverage using the "total:" line emitted by go tool cover.
#   - If internal/core/ does not exist (current state: no Go code), the 95% rule
#     is vacuously satisfied.
#   - If no internal/** packages exist, the 90% floor is vacuously satisfied.
#   - If the baseline file is absent or contains no entries, the regression gate
#     is vacuously satisfied (no baseline to regress from).
#   - Packages with 0 statements are skipped (no testable code).
#
# EXIT CODES:
#   0 — all gates passed (or vacuously satisfied)
#   1 — one or more gates failed; per-package report printed to stdout

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASELINE_FILE="${REPO_ROOT}/coverage.baseline"

# ---------------------------------------------------------------------------
# Determine coverage profile
# ---------------------------------------------------------------------------
if [[ $# -ge 1 && -f "$1" ]]; then
    PROFILE="$1"
    GENERATED=0
else
    PROFILE="/tmp/harmonik-coverage.$$.out"
    GENERATED=1
    echo "coverage-gate: running go test -coverprofile=${PROFILE} -covermode=atomic ./..."
    # Capture output; non-zero exit is fatal UNLESS no packages were matched
    # (go test exits 1 with "matched no packages" on a bare module — treat as
    # vacuous pass, not an error).
    go_output=$(go test -coverprofile="${PROFILE}" -covermode=atomic ./... 2>&1) || {
        if echo "${go_output}" | grep -q "matched no packages"; then
            echo "coverage-gate: no packages to test; vacuously passed"
            exit 0
        fi
        echo "${go_output}" >&2
        echo "coverage-gate: ERROR: go test failed; cannot compute coverage" >&2
        rm -f "${PROFILE}"
        exit 1
    }
    echo "${go_output}"
fi

cleanup() {
    [[ $GENERATED -eq 1 ]] && rm -f "${PROFILE}"
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Parse per-package coverage from "go tool cover -func"
# Output format:
#   <file>:<line>:  <function>  <pct>%
#   total:          (statements) <pct>%
# We collect only the "total:" line emitted after each package block.
# go tool cover -func does NOT group by package, so we drive it per-package
# by calling it with a filtered profile for each unique package.
# ---------------------------------------------------------------------------

# Collect all packages present in the coverage profile (second field, mod-path prefix).
# A coverage profile line looks like:
#   github.com/gregberns/harmonik/internal/foo/file.go:10.5,12.3 2 1
# The package path is the directory portion of the file path.

declare -A PKG_COVERAGE  # pkg -> coverage pct (float string, no %)

if [[ ! -s "${PROFILE}" ]]; then
    echo "coverage-gate: profile is empty; no packages to check (vacuously passed)"
    exit 0
fi

# Extract unique packages from profile (skip the 'mode:' header line).
mapfile -t ALL_PKGS < <(
    grep -v '^mode:' "${PROFILE}" \
    | awk -F: '{print $1}' \
    | sed 's|/[^/]*\.go$||' \
    | sort -u
)

if [[ ${#ALL_PKGS[@]} -eq 0 ]]; then
    echo "coverage-gate: no packages in profile; vacuously passed"
    exit 0
fi

# For each package, extract coverage using go tool cover on a filtered profile.
TMPFILTERED="/tmp/harmonik-cov-filtered.$$.out"
for pkg in "${ALL_PKGS[@]}"; do
    {
        head -1 "${PROFILE}"  # mode: line
        grep "^${pkg}/" "${PROFILE}" || true
    } > "${TMPFILTERED}"

    # If only the mode line exists, no statements — skip.
    line_count=$(wc -l < "${TMPFILTERED}")
    if [[ $line_count -le 1 ]]; then
        PKG_COVERAGE["${pkg}"]="SKIP"
        continue
    fi

    total_line=$(go tool cover -func="${TMPFILTERED}" 2>/dev/null | grep '^total:' || true)
    if [[ -z "${total_line}" ]]; then
        PKG_COVERAGE["${pkg}"]="SKIP"
        continue
    fi

    # Extract the percentage number (strip trailing %)
    pct=$(echo "${total_line}" | awk '{print $3}' | tr -d '%')
    PKG_COVERAGE["${pkg}"]="${pct}"
done
rm -f "${TMPFILTERED}"

# ---------------------------------------------------------------------------
# Load baseline
# ---------------------------------------------------------------------------
declare -A BASELINE  # pkg -> pct float string

if [[ -f "${BASELINE_FILE}" ]]; then
    while IFS= read -r line; do
        # Skip comments and empty lines
        [[ -z "${line}" || "${line}" == \#* ]] && continue
        pkg=$(echo "${line}" | awk '{print $1}')
        pct=$(echo "${line}" | awk '{print $2}')
        [[ -n "${pkg}" && -n "${pct}" ]] && BASELINE["${pkg}"]="${pct}"
    done < "${BASELINE_FILE}"
fi

# ---------------------------------------------------------------------------
# Apply gates — collect failures
# ---------------------------------------------------------------------------
FAILURES=()
MODULE_PREFIX="github.com/gregberns/harmonik"

CORE_PATTERN="${MODULE_PREFIX}/internal/core"
INTERNAL_PATTERN="${MODULE_PREFIX}/internal"

CORE_THRESHOLD=95.0
FLOOR_THRESHOLD=90.0
REGRESSION_MAX=0.3

# Packages exempt from FLOOR/CORE thresholds. Test infrastructure is the
# canonical case: internal/testhelpers exists to be invoked from other
# packages' tests; covering it standalone is a category error.
EXCLUDED_PACKAGES=(
    "${MODULE_PREFIX}/internal/testhelpers"
)
is_excluded() {
    local pkg="$1"
    for excl in "${EXCLUDED_PACKAGES[@]}"; do
        [[ "$pkg" == "$excl" ]] && return 0
    done
    return 1
}

# awk helper: returns 1 if $1 < $2 (floating point)
bc_lt() { awk -v a="$1" -v b="$2" 'BEGIN{exit !(a < b)}'; }
bc_sub() { awk -v a="$1" -v b="$2" 'BEGIN{printf "%.4f", a - b}'; }

for pkg in "${!PKG_COVERAGE[@]}"; do
    pct="${PKG_COVERAGE[${pkg}]}"
    [[ "${pct}" == "SKIP" ]] && continue
    is_excluded "${pkg}" && continue

    # Determine if this is an internal package
    is_internal=0
    is_core=0
    if [[ "${pkg}" == ${INTERNAL_PATTERN}/* || "${pkg}" == "${INTERNAL_PATTERN}" ]]; then
        is_internal=1
    fi
    if [[ "${pkg}" == ${CORE_PATTERN}/* || "${pkg}" == "${CORE_PATTERN}" ]]; then
        is_core=1
    fi

    # Gate 1: internal/core → 95% threshold
    if [[ $is_core -eq 1 ]]; then
        if bc_lt "${pct}" "${CORE_THRESHOLD}"; then
            FAILURES+=("CORE-THRESHOLD  ${pkg}  got=${pct}%  required>=${CORE_THRESHOLD}%")
        fi
    fi

    # Gate 2: all other internal → 90% floor
    if [[ $is_internal -eq 1 && $is_core -eq 0 ]]; then
        if bc_lt "${pct}" "${FLOOR_THRESHOLD}"; then
            FAILURES+=("FLOOR  ${pkg}  got=${pct}%  required>=${FLOOR_THRESHOLD}%")
        fi
    fi

    # Gate 3: regression vs baseline (applies to all internal packages)
    if [[ $is_internal -eq 1 && -n "${BASELINE[${pkg}]+x}" ]]; then
        base="${BASELINE[${pkg}]}"
        regression=$(bc_sub "${base}" "${pct}")
        # positive regression means we dropped; fail at >= max (spec: "<0.3% regression")
        if ! bc_lt "${regression}" "${REGRESSION_MAX}"; then
            FAILURES+=("REGRESSION  ${pkg}  baseline=${base}%  got=${pct}%  dropped=${regression}pp  max_allowed=${REGRESSION_MAX}pp")
        fi
    fi
done

# ---------------------------------------------------------------------------
# Report
# ---------------------------------------------------------------------------
if [[ ${#FAILURES[@]} -gt 0 ]]; then
    echo ""
    echo "coverage-gate: FAILED — ${#FAILURES[@]} violation(s):"
    echo ""
    printf "  %-14s  %-60s  %s\n" "GATE" "PACKAGE" "DETAIL"
    printf "  %-14s  %-60s  %s\n" "--------------" "------------------------------------------------------------" "------"
    for f in "${FAILURES[@]}"; do
        gate=$(echo "${f}" | awk '{print $1}')
        pkg=$(echo "${f}" | awk '{print $2}')
        rest=$(echo "${f}" | cut -d' ' -f3-)
        printf "  %-14s  %-60s  %s\n" "${gate}" "${pkg}" "${rest}"
    done
    echo ""
    exit 1
fi

echo "coverage-gate: all gates passed"
exit 0
