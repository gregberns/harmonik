#!/usr/bin/env bash

set -euo pipefail

# reachability-gate.sh answers one question and only one: does production reach
# this code?
#
# Not "is this function correct" — "is this function ever called by anything
# that runs when the daemon runs". A guard whose emitter has no production call
# site runs nowhere, and its tests pass anyway because they call the guard
# directly. Reading never catches that. A call graph does.
#
# Method: golang.org/x/tools/cmd/deadcode builds a Rapid Type Analysis call
# graph rooted at the production main packages ONLY. Test binaries are not
# roots. Any function the graph cannot reach is production-unreachable, and RTA
# resolves calls through func values, interface methods and reflection, so an
# interface-heavy tree does not defeat it.
#
# The gate is a ratchet, not an absolute. The baseline holds every function that
# is already production-unreachable. The gate fails only when a NEW name joins
# the set — which is the moment a reviewer wants to be asked whether the code is
# a seam, a not-yet-wired subsystem, or a protection that cannot protect.
#
# The fire rate is low, and the package graph is why, not the baseline. deadcode
# loads only what `go list -deps ./cmd/...` reaches, so internal/testhelpers and
# every other test-only package are invisible to it and contribute nothing. The
# -filter default also keeps the report inside this module, so a dependency bump
# cannot churn the file. What remains is a narrow and correct class: a new seam
# added INSIDE a production-linked package, which substrate.FakeClock already
# shows. That one should be asked about.
#
# Two configurations are measured, because deadcode is valid for exactly one
# GOOS/GOARCH and this tree ships to both. Pinning them also keeps the baseline
# identical on a developer mac and in CI. About 3 seconds warm. The FIRST run on
# any machine also builds the other platform's standard library, which is slower
# and is a one-time cost that the Go build cache then absorbs.
#
# Blind spots, stated so nobody reads a pass as more than it is:
#   - It sees Go call graphs. It does not see a state that is never entered, a
#     switch case no production input selects, a config key nothing sets, or a
#     process-name grep that matches nothing. Those need a reader.
#   - Build-tagged test tiers (scenario, integration) are not roots here by
#     design: a function only a scenario test reaches is still production-dead.
#   - //go:linkname is not understood. This tree has none today.

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)
baseline=${REACHABILITY_BASELINE:-$script_dir/reachability.baseline}
deadcode_bin=${DEADCODE_BIN:-${TOOLS_HOME:-$repo_root}/.tools/deadcode}
mode=check

# The production entry points. A binary absent from this list makes every
# function only it reaches look dead, so the list is checked against the tree
# below rather than trusted.
prod_mains=(
    ./cmd/harmonik
    ./cmd/harmonik-twin-claude
    ./cmd/harmonik-twin-codex
    ./cmd/harmonik-twin-generic
    ./cmd/harmonik-twin-pi
    ./cmd/harmonik-twin-session
)

# Both shipped targets. The self-test narrows this to one config to stay cheap.
# Nothing else should. A name dead on only one target still belongs in the
# baseline.
configs=(
    "darwin arm64"
    "linux amd64"
)
if [[ -n ${REACHABILITY_CONFIGS:-} ]]; then
    IFS=';' read -r -a configs <<<"$REACHABILITY_CONFIGS"
fi

usage() {
    echo "usage: scripts/reachability-gate.sh [--write-baseline]" >&2
    echo "  install the tool with: make tools" >&2
}

case ${1:-} in
    "") ;;
    --write-baseline) mode='write' ;;
    -h|--help) usage; exit 0 ;;
    *) usage; exit 2 ;;
esac
[[ $# -le 1 ]] || { usage; exit 2; }

if [[ ! -x $deadcode_bin ]]; then
    if command -v deadcode >/dev/null 2>&1; then
        deadcode_bin=$(command -v deadcode)
    else
        echo "reachability-gate: ERROR: no deadcode binary at $deadcode_bin" >&2
        echo "reachability-gate: run 'make tools' first" >&2
        exit 2
    fi
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/harmonik-reachability.XXXXXX")
baseline_tmp=
cleanup() {
    rm -rf "$tmp"
    [[ -z $baseline_tmp ]] || rm -f "$baseline_tmp"
}
trap cleanup EXIT

cd "$repo_root"

# Read and validate the baseline BEFORE spending the analysis. A missing or
# malformed baseline is a setup error, and a setup error should report in
# milliseconds rather than after a whole-program call graph.
baseline_data=$tmp/baseline-data
if [[ $mode == check ]]; then
    if [[ ! -f $baseline ]]; then
        echo "reachability-gate: ERROR: missing $baseline" >&2
        echo "reachability-gate: ratify a clean tree with --write-baseline" >&2
        exit 2
    fi
    if ! awk '
        /^[[:space:]]*($|#)/ { next }
        NF != 2 { bad = 1; next }
        $1 !~ /^[a-z0-9]+\/[a-z0-9]+$/ { bad = 1; next }
        seen[$1 " " $2]++ { duplicate = 1 }
        { print $1, $2 }
        END { if (bad || duplicate) exit 1 }
    ' "$baseline" | LC_ALL=C sort -u >"$baseline_data"; then
        echo "reachability-gate: ERROR: malformed or duplicate entry in $baseline" >&2
        exit 2
    fi
fi

# Guard the entry-point list itself. A main package nobody listed here is the
# same defect this gate exists to find, one level up.
declare -a listed_mains=()
for m in "${prod_mains[@]}"; do listed_mains+=("${m#./}"); done
actual_mains=$tmp/actual-mains
if ! go list -f '{{if eq .Name "main"}}{{.Dir}}{{end}}' ./cmd/... \
    | sed "s#^$repo_root/##" | awk 'NF' | LC_ALL=C sort -u >"$actual_mains"; then
    echo "reachability-gate: ERROR: go list ./cmd/... failed" >&2
    exit 2
fi
printf '%s\n' "${listed_mains[@]}" | LC_ALL=C sort -u >"$tmp/listed-mains"
if ! cmp -s "$actual_mains" "$tmp/listed-mains"; then
    echo "reachability-gate: FAILED: the production entry-point list is stale" >&2
    comm -23 "$actual_mains" "$tmp/listed-mains" | sed 's/^/  unlisted main: /' >&2
    comm -13 "$actual_mains" "$tmp/listed-mains" | sed 's/^/  listed but gone: /' >&2
    echo "  edit prod_mains in scripts/reachability-gate.sh, then re-ratify" >&2
    exit 1
fi

measured=$tmp/measured
: >"$measured"
# shellcheck disable=SC2016  # a Go text/template. $p is deadcode's, not the shell's.
tmpl='{{$p := .Path}}{{range .Funcs}}{{printf "%s.%s\n" $p .Name}}{{end}}'
for cfg in "${configs[@]}"; do
    read -r goos goarch <<<"$cfg"
    raw=$tmp/raw-$goos-$goarch
    if ! GOOS=$goos GOARCH=$goarch "$deadcode_bin" -f "$tmpl" "${prod_mains[@]}" >"$raw" 2>"$tmp/err-$goos"; then
        echo "reachability-gate: ERROR: deadcode failed for $goos/$goarch" >&2
        sed 's/^/  /' "$tmp/err-$goos" >&2
        exit 2
    fi
    # An empty report is a claim, and it can be wrong the same way a test can.
    # A tree this size having zero production-unreachable functions means the
    # analysis did not run, not that the tree is clean.
    if [[ ! -s $raw ]]; then
        echo "reachability-gate: ERROR: deadcode produced no output for $goos/$goarch" >&2
        exit 2
    fi
    awk -v cfg="$goos/$goarch" 'NF { print cfg, $0 }' "$raw" >>"$measured"
done

LC_ALL=C sort -u -o "$measured" "$measured"

if [[ $mode == write ]]; then
    baseline_dir=$(dirname "$baseline")
    baseline_name=$(basename "$baseline")
    if ! baseline_tmp=$(mktemp "$baseline_dir/.${baseline_name}.tmp.XXXXXX"); then
        echo "reachability-gate: ERROR: cannot create a temporary baseline in $baseline_dir" >&2
        exit 2
    fi
    {
        echo '# reachability.baseline — functions no production entry point can reach.'
        echo '#'
        echo '# Each line is: <GOOS>/<GOARCH> <package-import-path>.<function>'
        echo '# Rooted at the cmd/** main packages only. Test binaries are NOT roots, so a'
        echo '# name here may still be reached by a test — that is the point.'
        echo '#'
        echo '# The two configurations agree name-for-name today, so every entry appears'
        echo '# twice. That is the evidence, not waste: no production file is currently'
        echo '# platform-scoped. The day one is, the two lists diverge and say so.'
        echo '#'
        echo '# A name in this file is one of three things, and the difference matters:'
        echo '#   1. A test seam (a fake clock, a recording emitter). Correct. Leave it.'
        echo '#   2. Inert leftovers — a superseded wrapper, an unused accessor. Harmless.'
        echo '#   3. A protection that cannot protect — a guard, gate, lock, reaper or'
        echo '#      detector that production never calls. That one is a defect.'
        echo '#'
        echo '# Adding a line means accepting one of those three. Removing lines is always'
        echo '# welcome: it means something got wired up or deleted.'
        echo '#'
        echo '# This file has no room for a reason, and --write-baseline rewrites it whole.'
        echo '# So say WHY in the commit body of the commit that adds a line. Without that,'
        echo '# a real defect ratified in the same week as a real test seam is indistinguishable'
        echo '# from it forever after.'
        echo '#'
        echo '# Re-ratify deliberately with: scripts/reachability-gate.sh --write-baseline'
        cat "$measured"
    } >"$baseline_tmp"
    if ! mv "$baseline_tmp" "$baseline"; then
        echo "reachability-gate: ERROR: cannot replace the baseline: $baseline" >&2
        exit 2
    fi
    baseline_tmp=
    echo "reachability-gate: wrote $baseline ($(wc -l <"$measured" | tr -d ' ') entries)"
    exit 0
fi

added=$tmp/added
removed=$tmp/removed
comm -13 "$baseline_data" "$measured" >"$added"
comm -23 "$baseline_data" "$measured" >"$removed"

if [[ -s $removed ]]; then
    echo "reachability-gate: NOTICE: $(wc -l <"$removed" | tr -d ' ') name(s) left the unreachable set"
    sed 's/^/  now reachable or deleted: /' "$removed"
    echo "reachability-gate: NOTICE: re-ratify with --write-baseline to bank the improvement"
fi

if [[ -s $added ]]; then
    echo "reachability-gate: FAILED: $(wc -l <"$added" | tr -d ' ') name(s) became production-unreachable" >&2
    sed 's/^/  /' "$added" >&2
    echo >&2
    echo "  Ask, in this order:" >&2
    echo "    Is it a guard, gate, lock, reaper or detector? Then it cannot protect. Wire it." >&2
    echo "    Is it a test seam? Move it to a _test.go file or internal/testhelpers." >&2
    echo "    Is it neither? Delete it, or ratify with --write-baseline and say why" >&2
    echo "      in the commit body — the baseline itself has no room for a reason." >&2
    exit 1
fi

echo "reachability-gate: OK ($(wc -l <"$measured" | tr -d ' ') known-unreachable names, no new ones)"
