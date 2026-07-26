#!/usr/bin/env bash

set -euo pipefail

# cmd-coverage-gate.sh keeps command-package coverage from moving backwards.
# It intentionally has no aspirational absolute floor: the ratified baseline is
# the current truth, and a drop of 0.3 percentage points or more fails.

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)
baseline=${CMD_COVERAGE_BASELINE:-$script_dir/cmd-coverage.baseline}
go_bin=${GO_BIN:-go}
regression_max=0.3
improvement_notice=0.3
mode=check

usage() {
    echo "usage: scripts/cmd-coverage-gate.sh [--write-baseline]" >&2
}

case ${1:-} in
    "") ;;
    --write-baseline) mode='write' ;;
    -h|--help) usage; exit 0 ;;
    *) usage; exit 2 ;;
esac
[[ $# -le 1 ]] || { usage; exit 2; }

tmp=$(mktemp -d "${TMPDIR:-/tmp}/harmonik-cmd-coverage.XXXXXX")
baseline_tmp=
cleanup() {
    rm -rf "$tmp"
    [[ -z $baseline_tmp ]] || rm -f "$baseline_tmp"
}
trap cleanup EXIT
packages=$tmp/packages
measured=$tmp/measured
profile=$tmp/coverage.out

cd "$repo_root"

# Go metadata, rather than filesystem guesses, defines a production package.
# .GoFiles/.CgoFiles exclude external tests and directories containing tests only.
if ! "$go_bin" list -f '{{if or .GoFiles .CgoFiles}}{{.ImportPath}}{{end}}' ./cmd/... \
    | awk 'NF' | LC_ALL=C sort -u >"$packages"; then
    echo "cmd-coverage-gate: ERROR: go list ./cmd/... failed" >&2
    exit 2
fi
if [[ ! -s $packages ]]; then
    echo "cmd-coverage-gate: ERROR: no production packages found under ./cmd/..." >&2
    exit 2
fi

echo "cmd-coverage-gate: measuring fresh cmd/** coverage (-count=1, -covermode=atomic)"
if ! "$go_bin" test -count=1 -covermode=atomic -coverprofile="$profile" ./cmd/...; then
    echo "cmd-coverage-gate: ERROR: cmd/** tests failed; coverage was not evaluated" >&2
    exit 2
fi
if [[ ! -s $profile ]] || ! grep -q '^mode: atomic$' "$profile"; then
    echo "cmd-coverage-gate: ERROR: go test did not produce an atomic coverage profile" >&2
    exit 2
fi

# Aggregate raw statement blocks by their exact package directory. Avoid prefix
# matching: cmd/harmonik must not accidentally absorb cmd/harmonik/supervise.
awk '
    NR == FNR { wanted[$1] = 1; next }
    /^mode:/ { next }
    {
        file = $1
        sub(/:.*/, "", file)
        pkg = file
        sub(/\/[^\/]+$/, "", pkg)
        if (!(pkg in wanted)) next
        statements[pkg] += $2
        if ($3 > 0) covered[pkg] += $2
    }
    END {
        for (pkg in wanted) {
            if (statements[pkg] == 0) {
                printf "%s MISSING\n", pkg
            } else {
                printf "%s %.1f\n", pkg, 100 * covered[pkg] / statements[pkg]
            }
        }
    }
' "$packages" "$profile" | LC_ALL=C sort >"$measured"

if grep -q ' MISSING$' "$measured"; then
    echo "cmd-coverage-gate: ERROR: profile has no statements for:" >&2
    awk '$2 == "MISSING" { print "  " $1 }' "$measured" >&2
    exit 2
fi

if [[ $mode == write ]]; then
    baseline_dir=$(dirname "$baseline")
    baseline_name=$(basename "$baseline")
    if [[ ! -d $baseline_dir ]]; then
        echo "cmd-coverage-gate: ERROR: baseline directory does not exist: $baseline_dir" >&2
        exit 2
    fi
    # The temporary file must share the destination directory so rename(2) is
    # atomic even when TMPDIR and the repository are on different filesystems.
    if ! baseline_tmp=$(mktemp "$baseline_dir/.${baseline_name}.tmp.XXXXXX"); then
        echo "cmd-coverage-gate: ERROR: cannot create temporary baseline in $baseline_dir" >&2
        exit 2
    fi
    {
        echo '# cmd-coverage.baseline — ratified cmd/** package coverage'
        echo '# Format: <full-package-import-path> <statement-coverage-percent>'
        echo '# Update deliberately with: scripts/cmd-coverage-gate.sh --write-baseline'
        echo '# Package membership must exactly match production packages from go list ./cmd/....'
        cat "$measured"
    } >"$baseline_tmp"
    if ! mv "$baseline_tmp" "$baseline"; then
        echo "cmd-coverage-gate: ERROR: cannot replace baseline: $baseline" >&2
        exit 2
    fi
    baseline_tmp=
    echo "cmd-coverage-gate: wrote $baseline"
    exit 0
fi

if [[ ! -f $baseline ]]; then
    echo "cmd-coverage-gate: ERROR: missing $baseline" >&2
    echo "cmd-coverage-gate: ratify a clean tree with --write-baseline" >&2
    exit 2
fi

baseline_data=$tmp/baseline-data
if ! awk '
    /^[[:space:]]*($|#)/ { next }
    NF != 2 || $2 !~ /^[0-9]+([.][0-9]+)?$/ || $2 < 0 || $2 > 100 { bad = 1; next }
    seen[$1]++ { duplicate = 1 }
    { print $1, $2 }
    END { if (bad || duplicate) exit 1 }
' "$baseline" | LC_ALL=C sort >"$baseline_data"; then
    echo "cmd-coverage-gate: ERROR: malformed or duplicate baseline entry in $baseline" >&2
    exit 2
fi

cut -d' ' -f1 "$baseline_data" >"$tmp/baseline-packages"
if ! cmp -s "$packages" "$tmp/baseline-packages"; then
    echo "cmd-coverage-gate: FAILED: baseline package set is stale" >&2
    comm -23 "$packages" "$tmp/baseline-packages" | sed 's/^/  missing: /' >&2
    comm -13 "$packages" "$tmp/baseline-packages" | sed 's/^/  stale:   /' >&2
    exit 1
fi

status=0

# Both inputs are built with `LC_ALL=C sort`, so the join must use the same
# collation. A bare `join` inherits the ambient locale, and under a UTF-8
# locale punctuation is ignored at the primary collation level -- so
# `cmd/harmonik/digest` sorts before `cmd/harmonik-twin-claude`, the opposite
# of the C order these files are written in. Mismatched collation makes `join`
# silently drop rows, the loop body never runs, and the gate reports success
# having compared nothing. Fail closed on that instead: the joined row count
# must equal the package count.
LC_ALL=C join "$baseline_data" "$measured" >"$tmp/joined"
joined_rows=$(wc -l <"$tmp/joined" | tr -d ' ')
expected_rows=$(wc -l <"$packages" | tr -d ' ')
if [[ $joined_rows -ne $expected_rows ]]; then
    echo "cmd-coverage-gate: FAILED: joined $joined_rows row(s) for $expected_rows package(s)" >&2
    echo "  the baseline and measured sets did not align; refusing to report a pass" >&2
    exit 1
fi

while read -r pkg base actual; do
    result=$(awk -v base="$base" -v actual="$actual" -v max="$regression_max" \
        -v improve="$improvement_notice" 'BEGIN {
            delta = base - actual
            # Inputs are recorded to one decimal place. The epsilon prevents
            # binary floating-point representation from turning 0.3 into a
            # value infinitesimally below the contractual boundary.
            if (delta + 0.00001 >= max) { printf "FAIL %.1f", delta; exit }
            if (-delta + 0.00001 >= improve) { printf "NOTICE %.1f", -delta; exit }
            print "OK 0.0"
        }')
    kind=${result%% *}
    delta=${result#* }
    case $kind in
        FAIL)
            echo "cmd-coverage-gate: FAIL $pkg baseline=${base}% actual=${actual}% regression=${delta}pp (must be < ${regression_max}pp)" >&2
            echo fail >"$tmp/failed"
            ;;
        NOTICE)
            echo "cmd-coverage-gate: NOTICE $pkg improved ${delta}pp (${base}% -> ${actual}%); consider raising the baseline"
            ;;
        OK)
            echo "cmd-coverage-gate: OK $pkg ${actual}% (baseline ${base}%)"
            ;;
    esac
done <"$tmp/joined"

if [[ -f $tmp/failed ]]; then
    status=1
fi
if [[ $status -ne 0 ]]; then
    echo "cmd-coverage-gate: FAILED" >&2
    exit "$status"
fi
echo "cmd-coverage-gate: all package baselines held"
