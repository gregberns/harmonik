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

# One collation for the whole script, and it has to be exported, not per-command.
# Every sort here already said LC_ALL=C. `comm` did not, so it compared C-sorted
# input under the caller's en_US.UTF-8 and reported the SAME name as both newly
# unreachable and newly reachable. Measured on this tree: the truth is 0 new
# names and 16 improvements, and the defect reported 12 new names and 28
# improvements, with all 12 appearing in BOTH lists. The 16 were still printed —
# buried in a list of 28 — so the harm is the fabricated red, not a hidden win.
# A gate that fabricates its own red is worse than no gate, because the first fix
# attempt goes looking in the wrong place. sort and comm must agree, so set it
# once for both.
export LC_ALL=C

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)
baseline=${REACHABILITY_BASELINE:-$script_dir/reachability.baseline}
mode=check

# The pinned tool lives in the MAIN checkout's .tools, never in a lane worktree.
# `make tools` derives that path from --git-common-dir and installs there, so a
# gate that looked under its own repo_root found nothing whenever it ran from a
# worktree — which is where every dispatched agent runs. Derive the same path the
# Makefile does, so a direct run and the self-test's direct runs both work.
# The `if !` wrapper is not style. A plain assignment takes the exit status of
# its command substitution, and pipefail hands it git's, so under `set -e` the
# shell would exit AT the assignment and the fallback below would be dead code —
# silently, with exit 128 outside a repo and 127 with no git, both of which
# violate this script's own "a setup error exits 2" rule. `if !` is exempt from
# set -e. The Makefile derives this the same way and never had the problem,
# because $(shell ...) cannot abort a recipe.
if ! tools_home=$(git -C "$repo_root" rev-parse --path-format=absolute --git-common-dir 2>/dev/null | sed 's|/\.git/*$||'); then
    tools_home=
fi
[[ -n $tools_home ]] || tools_home=$repo_root
deadcode_bin=${DEADCODE_BIN:-$tools_home/.tools/deadcode}

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
        # Usable, but say so. The baseline is only reproducible against the
        # pinned version, so an unpinned binary is a candidate explanation for
        # any failure this run reports — and a silent fallback is how the
        # worktree path defect above stayed invisible on the one box that had a
        # deadcode on PATH.
        deadcode_bin=$(command -v deadcode)
        echo "reachability-gate: WARNING: no pinned tool at $tools_home/.tools/deadcode" >&2
        echo "reachability-gate: WARNING: using unpinned $deadcode_bin — run 'make tools'" >&2
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
#
# The guard scans ./cmd/... and nothing else, so it closes that defect only for
# the directory this repo ships binaries from. Other main packages exist under
# ./scripts, ./tools and ./test/twins and are deliberately not roots. A NEW
# production binary placed outside ./cmd/ would be invisible to both the list and
# this check, and every function only it reaches would read as unreachable. If a
# shipped binary ever moves out of ./cmd/, widen this.
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
    # mktemp makes the file 0600, and mv carries that mode onto the baseline. Git
    # tracks only the exec bit, so the change does not propagate and instead turns
    # into one developer's tree quietly differing from everyone else's.
    if ! chmod 644 "$baseline_tmp"; then
        echo "reachability-gate: ERROR: cannot set the mode on the new baseline" >&2
        exit 2
    fi
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
