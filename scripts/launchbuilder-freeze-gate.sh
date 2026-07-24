#!/usr/bin/env bash
set -euo pipefail

# launchbuilder-freeze-gate.sh — P2 unit E5 RT17 freeze tripwire
# (plans/2026-07-21-p2-extraction/RT17-launchspec-conversion.md §4 step 5;
#  _plan.md §3.2).
#
# The DOT run path reaches its pre-built launch-spec builder through the narrow
# (*workLoopDeps).launchBuilder() accessor in internal/daemon/runports.go — a
# pure alias over the launchSpecBuilder field — not through the field directly.
# RT17 converted the four run-path field READS (reviewloop x2, dot_cascade,
# dot_gate) plus the single-mode binding to accessor calls, so RT18.11's deletion
# of the two copy-mutation assignments (workloop.go, still exactly 2 here) can
# re-resolve those readers into rp.Launch without hunting field reads scattered
# across the sub-drivers. A new raw field read on a sub-driver un-does that.
#
# depguard cannot express "reach this dependency through its accessor", so this
# grep ratchet is the only thing holding the line. workloop.go takes ~3.7
# commits/day; without a gate the count silently regrows before RT18 lands.
#
# Exit 0: clean. Exit 1: the accessor was bypassed.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# FIELD_RE — the raw field read this gate rations. Plain text: COMMENTS COUNT.
# Stripping them would need a Go parser, and a gate that misses a real site is
# worse than one that occasionally objects to prose — so in the sub-driver files,
# describe the field without writing its literal spelling ("the pre-built
# launch-spec builder", not the field access). RT17 reworded the six prose lines
# in reviewloop.go/dot_cascade.go/dot_gate.go for exactly that reason. Test files
# are out of scope (documentation, not code).
#
# KNOWN LIMITATION, stated so the next maintainer knows it was a choice: this
# matches the RECEIVER NAME, so a read through a differently-named receiver —
# `func (d *workLoopDeps) f() { _ = d.launchSpecBuilder }` — is invisible to it.
# That is a convention dependency, not a live hole: all workLoopDeps methods name
# their receiver `deps`, and all movers take `deps` as a value parameter. Matching
# `\.launchBuilder\b` bare would fire on unrelated fields; if the receiver
# convention ever breaks, widen this and re-measure the budgets rather than
# loosening them.
FIELD_RE='deps\.launchSpecBuilder'

# count_matches counts OCCURRENCES, not lines. `grep -c` counts matching LINES,
# and a run-path line can legitimately carry two reads. Rewriting one already
# counted line to hold two reads takes a file from N occurrences to N+1 while
# `grep -c` still reports N, and the exact assertion below — the whole point of
# the ratchet — sails straight through. The `|| true` is required because this
# script runs under `set -o pipefail` and grep exits 1 on no match.
#
# -E is load-bearing: CALL_RE below counts under ERE; both callers pass an ERE,
# so the flag is set here once rather than per call.
count_matches() {
    { grep -oE "$2" "$1" || true; } | wc -l | tr -d ' '
}

# ---------------------------------------------------------------------------
# (A) Per-file budget over EVERY non-test Go file under internal/daemon.
#
#     Recursive on purpose. internal/daemon has real sub-packages (bootconfig,
#     router, scenariotest) and a `find -maxdepth 1` scan would let a sub-package
#     hold any number of field reads. A file absent from the table is budgeted at
#     ZERO, so a brand-new daemon file cannot smuggle one in.
#
#     The three SUB-DRIVER files (reviewloop.go, dot_cascade.go, dot_gate.go) and
#     runports.go are asserted EXACTLY, in both directions. The sub-drivers are 0
#     (every read + comment converted/reworded); runports.go is 1 (the accessor
#     body itself, `return deps.launchSpecBuilder`) — a shrink there means the
#     accessor was deleted, which tooth C also pins.
#
#     NON-SUB-DRIVERS are budgeted at their measured count as a CEILING only.
#     workloop.go keeps the `== nil` guard, the TWO copy-mutation assignments and
#     its doc comments (measured 7); reviewerharness_hkiv748.go carries 3 doc
#     comments. These are not RT17's targets, so a legitimate shrink there (e.g.
#     RT18 rewording) is pure improvement and must not fail the build.
declare -a EXACT_FILES=(
    "internal/daemon/reviewloop.go   0"
    "internal/daemon/dot_cascade.go  0"
    "internal/daemon/dot_gate.go     0"
    "internal/daemon/runports.go     1"
)
declare -a CEILING_FILES=(
    "internal/daemon/workloop.go               7"
    "internal/daemon/reviewerharness_hkiv748.go 3"
)

budget_for() { # path -> "exact <n>" | "ceiling <n>" | "exact 0"
    local p="$1" row
    for row in "${EXACT_FILES[@]}"; do
        set -- $row
        [ "$1" = "$p" ] && { echo "exact $2"; return; }
    done
    for row in "${CEILING_FILES[@]}"; do
        set -- $row
        [ "$1" = "$p" ] && { echo "ceiling $2"; return; }
    done
    echo "exact 0"
}

while IFS= read -r f; do
    got="$(count_matches "$f" "$FIELD_RE")"
    [ "$got" -eq 0 ] && continue
    read -r kind want <<<"$(budget_for "$f")"
    if [ "$got" -gt "$want" ]; then
        echo "launchbuilder-freeze-gate: $f has $got raw launchSpecBuilder read(s), budget is $want:" >&2
        grep -n "$FIELD_RE" "$f" >&2
        HITS=$((HITS + 1))
    elif [ "$kind" = "exact" ] && [ "$got" -lt "$want" ]; then
        echo "launchbuilder-freeze-gate: $f has $got raw launchSpecBuilder read(s), budget asserts exactly $want." >&2
        echo "  A SHRINK on runports.go means the launchBuilder() accessor was deleted; on a" >&2
        echo "  sub-driver it should already be 0. If the shrink is deliberate, lower the" >&2
        echo "  budget in this script and say why." >&2
        HITS=$((HITS + 1))
    fi
done < <(find internal/daemon -type f -name '*.go' ! -name '*_test.go' | sort)

# Missing-file guard: a budgeted file that has been renamed or deleted makes the
# whole per-file table meaningless without ever failing check (A), because the
# find loop simply never visits it.
for row in "${EXACT_FILES[@]}" "${CEILING_FILES[@]}"; do
    set -- $row
    if [ ! -f "$1" ]; then
        echo "launchbuilder-freeze-gate: budgeted file $1 is gone — re-derive this gate" >&2
        HITS=$((HITS + 1))
    fi
done

# ---------------------------------------------------------------------------
# (B) Assignment ratchet. The two copy-mutation assignments in workloop.go
#     (deps.launchSpecBuilder = routedLaunchSpecBuilder(...) and = claude.
#     BuildLaunchSpec) are RT18.11's to delete, not RT17's — RT17 only installs
#     the read accessor as their now-satisfied precondition. Freeze them at
#     EXACTLY 2 in workloop.go and 0 everywhere else, both directions. A 3rd
#     anywhere is regrowth (or an inline local-based resolution that adds no
#     accessor call — Design 1's gate hole); a shrink below 2 means RT18.11 may
#     have landed and the budget must be lowered deliberately.
#     ` = ` (space-equals-space) excludes `== nil` and comment prose.
ASSIGN_RE='deps\.launchSpecBuilder = '
while IFS= read -r f; do
    got="$(count_matches "$f" "$ASSIGN_RE")"
    if [ "$f" = "internal/daemon/workloop.go" ]; then
        want=2
    else
        want=0
    fi
    if [ "$got" -gt "$want" ]; then
        echo "launchbuilder-freeze-gate: $f has $got launchSpecBuilder assignment(s), budget is $want:" >&2
        grep -n "$ASSIGN_RE" "$f" >&2
        HITS=$((HITS + 1))
    elif [ "$f" = "internal/daemon/workloop.go" ] && [ "$got" -lt "$want" ]; then
        echo "launchbuilder-freeze-gate: workloop.go has $got launchSpecBuilder assignment(s), budget asserts exactly $want." >&2
        echo "  RT18.11 may have landed — lower this budget deliberately." >&2
        HITS=$((HITS + 1))
    fi
done < <(find internal/daemon -type f -name '*.go' ! -name '*_test.go' | sort)

# ---------------------------------------------------------------------------
# (C) The seam must still exist. The accessor is a pure alias over the field, so
#     every downstream nil-check, per-node pin/route and call stays byte-identical
#     — that is the entire zero-behaviour argument for RT17.
if ! grep -q 'func (deps \*workLoopDeps) launchBuilder()' internal/daemon/runports.go; then
    echo "launchbuilder-freeze-gate: (*workLoopDeps).launchBuilder is gone — re-derive this gate" >&2
    HITS=$((HITS + 1))
fi
if ! grep -q 'type LaunchPort interface' internal/daemon/runports.go; then
    echo "launchbuilder-freeze-gate: LaunchPort interface is gone — re-derive this gate" >&2
    HITS=$((HITS + 1))
fi
if ! grep -qE '^func launchPort\(' internal/daemon/runports.go; then
    echo "launchbuilder-freeze-gate: launchPort() constructor is gone — re-derive this gate" >&2
    HITS=$((HITS + 1))
fi
if ! grep -qE '^\s*Launch\s+LaunchPort$' internal/daemon/runports.go; then
    echo "launchbuilder-freeze-gate: RunPorts.Launch is gone — re-derive this gate" >&2
    HITS=$((HITS + 1))
fi

# ---------------------------------------------------------------------------
# (D) Pin the CALL SITES, not just the seam. "The accessor still exists" passes
#     even if every site quietly abandoned it — the hole RT14's gate had to close.
#     Each mover must still reach the builder through the accessor at least as many
#     times as RT17 left it doing.
#
#     CALL_RE is the MEASURED set of spellings. When RT18.11 re-resolves the
#     readers into rp.Launch and deletes the assignments, teeth B and D go red and
#     RT18 must update them deliberately — the intended re-sign behaviour, exactly
#     as the emitter gate documents for its own PORT_SITES.
CALL_RE='deps\.launchBuilder\(\)'
declare -a CALL_SITES=(
    "internal/daemon/reviewloop.go   2"  # runReviewLoop: implementer + reviewer readers
    "internal/daemon/dot_cascade.go  1"  # dispatchDotAgenticNode reader
    "internal/daemon/dot_gate.go     1"  # executeCognitionGate reader
    "internal/daemon/workloop.go     1"  # beadRunOne single-mode binding
)
for row in "${CALL_SITES[@]}"; do
    set -- $row
    f="$1"; want="$2"
    [ -f "$f" ] || continue   # the missing-file guard above already counted it
    got="$(count_matches "$f" "$CALL_RE")"
    if [ "$got" -lt "$want" ]; then
        echo "launchbuilder-freeze-gate: $f reaches the builder through the accessor only $got time(s), expected >= $want" >&2
        echo "  — a run-path site left the seam. Use deps.launchBuilder(), or update this budget if RT18 re-signed it." >&2
        HITS=$((HITS + 1))
    fi
done

if [ "$HITS" -ne 0 ]; then
    echo "" >&2
    echo "launchbuilder-freeze-gate: FAIL — the run path reaches its launch-spec builder through" >&2
    echo "deps.launchBuilder() (P2 E5 RT17). Use deps.launchBuilder() to capture the builder func." >&2
    echo "The two workloop.go assignments belong to RT18.11; if it landed, lower the budgets here" >&2
    echo "and say why in the commit body." >&2
    exit 1
fi
echo "launchbuilder-freeze-gate: OK — the run path stays on deps.launchBuilder()"
