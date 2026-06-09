#!/usr/bin/env bash
# scenario-gate.sh — affected-package-scoped, fail-open commit gate.
#
# Replaces the whole-repo `go test ./... && go test -tags=scenario ./...` that
# standard-bead.dot's commit_gate node used to run. The full-repo form lets a
# pre-existing RED test in an UNRELATED package block EVERY bead — fatal once
# standard-bead.dot becomes the dispatch default (hk-30vlb). Instead this gate:
#
#   1. Computes the AFFECTED Go packages from the bead's diff vs. the merge base
#      (the set of package dirs whose files changed). `go test ./pkg/...` then
#      covers those packages AND their dependents — Go's `...` recursion and the
#      build graph mean a changed leaf is re-tested through every importer it
#      compiles into, so dependents are not silently skipped.
#   2. Runs ONLY those packages' unit tests, then re-runs the scenario-touching
#      subset under `-tags=scenario`, NOT the whole repo.
#   3. Is FAIL-OPEN on gate-infrastructure errors: a timeout, signal kill
#      (OOM/SIGKILL/SIGSEGV), or compile/build/setup failure produces a WARNING
#      and exits 0 (ALLOW). Only a genuine test FAILURE (`--- FAIL` / `FAIL`
#      with exit 1) blocks (exit 1).
#
# This mirrors the daemon's Go-side gate so the script and the daemon agree:
#   - affected-package computation        ← affectedScenarioPkgs / isScenarioTouching
#                                            / fileToGoPackagePattern (internal/daemon/scenariogate.go:247-300)
#   - failure-vs-infra-error classification ← classifyScenarioGateError / isSignalKill
#                                            / isCompileFailure / isGenuineTestFailure
#                                            (internal/daemon/scenariogate.go:125-226)
#
# A "scenario-touching" file lives under test/scenario/ or internal/scenario/, or
# (for .go files) contains a `//go:build scenario` or legacy `// +build scenario`
# build tag — matching isScenarioTouching exactly.
#
# Base SHA resolution: honors $HK_GATE_BASE_SHA when set (the daemon may export
# the parent commit it branched the worktree off); otherwise falls back to the
# merge base against the integration target (origin/main, then main). On any base
# failure the gate FAILS OPEN (the daemon's changedFilesSince does the same: a
# git error means "no scenario pkgs", a no-op).
#
# Exit codes:
#   0  — all affected tests green, OR gate could not produce a verdict (fail-open)
#   1  — a genuine test FAILURE was observed (BLOCK the merge)
#
# Bead: hk-u830m (delivers hk-n7fw3 intent). Mirrors internal/daemon/scenariogate.go.

set -uo pipefail

# ── logging ──────────────────────────────────────────────────────────────────
log()  { printf 'scenario-gate: %s\n' "$*" >&2; }
warn() { printf 'scenario-gate: WARNING: %s — ALLOWING merge (fail-open, hk-ur428)\n' "$*" >&2; }

# ── base-SHA resolution (fail-open) ──────────────────────────────────────────
# Resolve the commit the diff is measured against. Mirrors changedFilesSince's
# `<base>..HEAD` semantics. A resolution failure is treated as "no affected
# packages" (fail-open), exactly like changedFilesSince returning an error.
resolve_base() {
    if [ -n "${HK_GATE_BASE_SHA:-}" ]; then
        if git rev-parse --verify --quiet "${HK_GATE_BASE_SHA}^{commit}" >/dev/null 2>&1; then
            printf '%s' "${HK_GATE_BASE_SHA}"
            return 0
        fi
        warn "HK_GATE_BASE_SHA=${HK_GATE_BASE_SHA} is not a valid commit"
        return 1
    fi
    # Fall back to the merge base against the integration target.
    local ref base
    for ref in origin/main main origin/master master; do
        if git rev-parse --verify --quiet "${ref}^{commit}" >/dev/null 2>&1; then
            if base=$(git merge-base "${ref}" HEAD 2>/dev/null) && [ -n "$base" ]; then
                printf '%s' "$base"
                return 0
            fi
        fi
    done
    return 1
}

# ── changed-file enumeration ─────────────────────────────────────────────────
# Mirrors changedFilesSince (scenariogate.go:230-242): `git diff --name-only
# <base>..HEAD`. Returns file paths relative to the repo root, one per line.
changed_files() {
    local base="$1"
    git diff --name-only "${base}..HEAD" 2>/dev/null
}

# ── scenario-touching detection ──────────────────────────────────────────────
# Mirrors isScenarioTouching (scenariogate.go:267-282): a file is scenario-
# touching when its path is under test/scenario/ or internal/scenario/, OR (for
# .go files) its content carries a //go:build scenario or // +build scenario tag.
is_scenario_touching() {
    local f="$1"
    case "$f" in
        test/scenario/*|internal/scenario/*) return 0 ;;
    esac
    case "$f" in
        *.go) ;;
        *) return 1 ;;
    esac
    [ -f "$f" ] || return 1
    if grep -qE '//go:build scenario|// \+build scenario' "$f" 2>/dev/null; then
        return 0
    fi
    return 1
}

# ── file → recursive package pattern ─────────────────────────────────────────
# Mirrors fileToGoPackagePattern (scenariogate.go:291-300): a .go file at
# dir/foo.go maps to ./dir/...; a file at the module root maps to ./....
# Non-.go files map to "" (skipped by the callers).
file_to_pkg_pattern() {
    local f="$1"
    case "$f" in
        *.go) ;;
        *) return 0 ;;  # non-Go → empty (caller skips)
    esac
    local dir
    dir=$(dirname "$f")
    if [ "$dir" = "." ]; then
        printf './...'
    else
        printf './%s/...' "$dir"
    fi
}

# ── go test runner with fail-open classification ─────────────────────────────
# Mirrors classifyScenarioGateError + isSignalKill + isCompileFailure +
# isGenuineTestFailure (scenariogate.go:125-226). Runs `go test [tags] <pkgs>`
# with a timeout and classifies the result:
#   - rc 0                          → pass            → return 0 (non-block)
#   - timeout (rc 124 from timeout) → fail-open WARN  → return 0
#   - signal kill (rc >128, or
#     "signal: killed/segmentation") → fail-open WARN → return 0
#   - compile/build/setup failure
#     (rc 2, or build/setup markers) → fail-open WARN → return 0
#   - genuine failure (rc 1 with
#     "--- FAIL" / "FAIL" marker)    → BLOCK          → return 1
#   - any other non-zero rc          → fail-open WARN  → return 0 (unclassified)
run_go_test() {
    local class="$1"; shift   # human label e.g. "unit" / "scenario"
    # Build a single always-non-empty `go test` arg vector. macOS ships bash 3.2,
    # where `"${empty_array[@]}"` under `set -u` is an UNBOUND-VARIABLE error — so
    # we never expand a possibly-empty array; gotest_args always starts with
    # "test" and ends with the package patterns ($@).
    local -a gotest_args=(test)
    local tagdesc=""
    if [ "$class" = "scenario" ]; then
        gotest_args+=(-tags=scenario)
        tagdesc="-tags=scenario "
    else
        # Affected-unit step runs in -short mode: heavy real-daemon / real-binary
        # E2E tests opt out of the per-bead commit_gate via testing.Short()
        # (e.g. internal/daemon's ~21 daemon.Start / twin-binary / review-loop
        # tests, which are red on main for environmental reasons). They still run
        # without -short in the full CI lane. See skipRealDaemonE2EInShort
        # (internal/daemon/shortskip_hkp258q_test.go). Refs: hk-p258q.
        gotest_args+=(-short)
        tagdesc="-short "
    fi
    gotest_args+=("$@")        # each remaining arg is one package pattern
    local pkglist="$*"          # space-joined, for log/warn messages only

    # Prefer `timeout` when available so a hung suite is a fail-open timeout
    # rather than an indefinite hang. SIGTERM first (rc 124), KILL after grace.
    local out rc
    if command -v timeout >/dev/null 2>&1; then
        out=$(timeout --kill-after=30s "${SCENARIO_GATE_TIMEOUT:-600}s" \
            go "${gotest_args[@]}" 2>&1)
        rc=$?
    else
        out=$(go "${gotest_args[@]}" 2>&1)
        rc=$?
    fi

    if [ "$rc" -eq 0 ]; then
        log "${class} tests passed for: ${pkglist}"
        return 0
    fi

    # Keep only the tail of the output, matching the daemon's maxOut=2000 cap.
    local trimmed
    trimmed=$(printf '%s' "$out" | tail -c 2000)

    # Timeout — `timeout` exits 124 (or 137 on KILL after grace). Not a verdict.
    if [ "$rc" -eq 124 ] || [ "$rc" -eq 137 ]; then
        warn "could not produce a verdict (timeout) for go test ${tagdesc}${pkglist} (rc=${rc})"
        printf '%s\n' "$trimmed" >&2
        return 0
    fi

    # Signal kill (SIGKILL on OOM, SIGSEGV on crash). `go test` propagates the
    # child signal: rc = 128 + signum (>128). Also catch the textual markers.
    if [ "$rc" -gt 128 ] \
        || printf '%s' "$trimmed" | grep -q 'signal: killed' \
        || printf '%s' "$trimmed" | grep -q 'signal: segmentation'; then
        warn "could not produce a verdict (signal-kill) for go test ${tagdesc}${pkglist} (rc=${rc})"
        printf '%s\n' "$trimmed" >&2
        return 0
    fi

    # Compile / build / setup failure. `go test` returns exit 2 for build
    # failures (vs 1 for test failures); also catch the telltale markers.
    if [ "$rc" -eq 2 ] \
        || printf '%s' "$trimmed" | grep -qF '[build failed]' \
        || printf '%s' "$trimmed" | grep -qF '[setup failed]' \
        || printf '%s' "$trimmed" | grep -qF 'build constraints exclude all Go files'; then
        warn "could not produce a verdict (compile-fail) for go test ${tagdesc}${pkglist} (rc=${rc})"
        printf '%s\n' "$trimmed" >&2
        return 0
    fi

    # Genuine test failure: exit 1 with a FAIL marker = tests RAN and FAILED.
    if [ "$rc" -eq 1 ] && printf '%s' "$trimmed" | grep -qE '(--- FAIL|^FAIL|[[:space:]]FAIL)'; then
        log "BLOCK: genuine test FAILURE for go test ${tagdesc}${pkglist} (rc=${rc})"
        printf '%s\n' "$trimmed" >&2
        return 1
    fi

    # Unclassified non-zero rc → fail-open (do not false-block on gate noise).
    warn "could not produce a verdict (unclassified) for go test ${tagdesc}${pkglist} (rc=${rc})"
    printf '%s\n' "$trimmed" >&2
    return 0
}

# ── main ─────────────────────────────────────────────────────────────────────
main() {
    # Run from the worktree/repo root. The daemon sets cmd.Dir = wtPath; for a
    # manual run we resolve the repo root from this script's git context.
    local root
    if ! root=$(git rev-parse --show-toplevel 2>/dev/null); then
        warn "not inside a git repository; cannot compute affected packages"
        exit 0
    fi
    cd "$root" || { warn "cannot cd to repo root ${root}"; exit 0; }

    local base
    if ! base=$(resolve_base); then
        warn "could not resolve a diff base (HK_GATE_BASE_SHA / merge-base)"
        exit 0
    fi
    log "diff base = ${base}"

    # Enumerate changed files (fail-open on a git error).
    local files
    if ! files=$(changed_files "$base"); then
        warn "could not enumerate changed files vs ${base}"
        exit 0
    fi
    if [ -z "$files" ]; then
        log "no changed files vs ${base}; nothing to test"
        exit 0
    fi

    # Build the affected-package sets (newline-delimited accumulators, deduped
    # with sort -u, then read back into arrays so each pattern stays a distinct
    # `go test` argument):
    #   unit_pkgs     — every changed .go file's recursive package pattern.
    #   scenario_pkgs — only scenario-touching files' patterns (affectedScenarioPkgs).
    local unit_acc="" scenario_acc=""
    while IFS= read -r f; do
        [ -n "$f" ] || continue
        local pat
        pat=$(file_to_pkg_pattern "$f")
        [ -n "$pat" ] || continue
        unit_acc="${unit_acc}${pat}"$'\n'
        if is_scenario_touching "$f"; then
            scenario_acc="${scenario_acc}${pat}"$'\n'
        fi
    done <<< "$files"

    local -a unit_pkgs=() scenario_pkgs=()
    while IFS= read -r p; do [ -n "$p" ] && unit_pkgs+=("$p"); done \
        < <(printf '%s' "$unit_acc" | grep -v '^$' | sort -u)
    while IFS= read -r p; do [ -n "$p" ] && scenario_pkgs+=("$p"); done \
        < <(printf '%s' "$scenario_acc" | grep -v '^$' | sort -u)

    local block=0

    # ── unit tests (affected pkgs only) ──────────────────────────────────────
    if [ "${#unit_pkgs[@]}" -gt 0 ]; then
        log "affected unit packages: ${unit_pkgs[*]}"
        if ! run_go_test "unit" "${unit_pkgs[@]}"; then
            block=1
        fi
    else
        log "no affected Go packages; skipping unit tests"
    fi

    # ── scenario tests (scenario-touching pkgs only) ─────────────────────────
    if [ "${#scenario_pkgs[@]}" -gt 0 ]; then
        log "affected scenario packages: ${scenario_pkgs[*]}"
        # The scenario twin binary is needed by some scenario tests. Build it
        # fail-open: a build error here must not block (it's gate machinery).
        if [ -d ./cmd/harmonik-twin-claude ]; then
            if ! go build -tags=scenario -o harmonik-twin-claude ./cmd/harmonik-twin-claude 2>/dev/null; then
                warn "could not build scenario twin binary (cmd/harmonik-twin-claude)"
            fi
        fi
        if ! run_go_test "scenario" "${scenario_pkgs[@]}"; then
            block=1
        fi
    else
        log "no scenario-touching packages; skipping scenario suite"
    fi

    if [ "$block" -eq 1 ]; then
        log "RESULT: BLOCK (a genuine test failure was observed)"
        exit 1
    fi
    log "RESULT: PASS (affected tests green or fail-open)"
    exit 0
}

main "$@"
