#!/usr/bin/env bash
set -euo pipefail

# workloop-scheduler-freeze-gate.sh — Seam A split ratchet
# (plans/2026-07-27-delete-and-rewrite/DECOMPOSITION-MAP.md §2 "Seam A").
#
# The dispatch SCHEDULER left internal/daemon/workloop.go for
# internal/daemon/scheduler.go. Both files are the same Go package, so the
# compiler cannot hold the line: nothing stops a later edit from pasting
# runWorkLoop or one of its helpers back beside beadRunOne, and the two sides
# would fuse again with a green build. This grep ratchet closes that door.
#
# What crosses the seam is ONE immutable dispatch decision out (run id, bead
# record, queue coordinates, per-item overrides, worker or nil, local-slot flag,
# extra context) and one boolean plus a terminal summary back. The scheduler is
# one goroutine for the daemon's life and reaches br, queue.json, kerf and the
# disk. The run path is one goroutine per bead for 10 to 90 minutes and reaches
# tmux, ssh, git and the harness CLIs. Keeping them in separate files keeps that
# difference visible.
#
# The cadenced maintenance then left the scheduler for
# internal/daemon/loopmaintenance.go (DECOMPOSITION-MAP §3 Step 2). So the gate
# now holds THREE files apart, not two, and it carries two inventories: symbols
# that belong in scheduler.go and symbols that belong in loopmaintenance.go.
# Neither set may appear in workloop.go.
#
# The gate asserts FIVE things, not one:
#   (1) all three files still exist. A gate whose target was deleted stops
#       testing what it claims to test and passes on an empty grep. Three sibling
#       gates broke exactly that way in July 2026, so this one names the failure.
#   (2) no inventoried symbol is declared in workloop.go, and beadRunOne is not
#       declared in scheduler.go.
#   (3) every inventoried symbol is declared EXACTLY ONCE, in its own expected
#       file. A symbol that is gone, or that moved to some third daemon file,
#       fails here on purpose: re-derive the inventory below in the same commit
#       that moves the symbol. Do not delete the check to get green.
#   (4) the four loopMaintenance METHODS are not declared in workloop.go. Checks
#       (2) and (3) can only see column-0 declarations, and Go lets a method sit
#       in any file of its package while the receiver type stays put, so this is
#       a separate scan. A mutation test proved the bypass real.
#   (5) runWorkLoop's body is still IN scheduler.go, not just its name. Checks
#       (2) and (3) freeze names. They do not stop the whole loop body moving to
#       workloop.go behind a pass-through called runWorkLoop, which fuses the seam
#       with every name check green. Measured in CODE lines, so padding the shim
#       with comments does not clear it. See §"the body check" below.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

SCHED="internal/daemon/scheduler.go"
WORKLOOP="internal/daemon/workloop.go"
MAINT="internal/daemon/loopmaintenance.go"

HITS=0

# (1) Both sides of the seam must still be on disk. Check this FIRST: every scan
#     below is a grep, and a grep over a missing file finds nothing, which reads
#     as success.
for f in "$SCHED" "$WORKLOOP" "$MAINT"; do
    if [ ! -f "$f" ]; then
        echo "workloop-scheduler-freeze-gate: $f is GONE — this gate is now testing nothing." >&2
        echo "  Re-derive the gate against the file that replaced it, in the commit that removed this one." >&2
        HITS=$((HITS + 1))
    fi
done
if [ "$HITS" -ne 0 ]; then
    echo "workloop-scheduler-freeze-gate: FAIL — a named target is missing; the inventory is stale" >&2
    exit 1
fi

# The scheduler inventory. Derived 2026-07-29 by use-site analysis: each of these
# has ZERO occurrences inside beadRunOne and zero uses from any run-path file, so
# runWorkLoop (or another scheduler symbol) is its only reader.
SCHEDULER_SYMS=(
    workloopPollInterval
    shutdownDrainTimeout
    claimSkipInProgressCooldown
    windowCleaner
    maxItemAttempts
    queuePreClaimAttemptKey
    queueSelection
    effectiveQueueWorkers
    selectNextQueue
    snapshotFleet
    projectActiveGroup
    runWorkLoop
    autoCloseStaleBlockersOnClaimFailure
    drainCancelledQueue
    workloopSleep
    workloopIdleWait
    scheduleAwareIdleWait
    hasEnabledScheduledJob
    activateFirstPendingGroupLocked
    markQueueItemFailureReason
    evaluateGroupAdvanceWithOutcome
    extractTmuxAdapterFromSubstrate
    adoptLiveRunSession
    strandedBeadHasOnDiskRun
)

# The maintenance inventory. Same rules as SCHEDULER_SYMS, different expected
# file: these belong in $MAINT.
#
# periodicCoordinatorReapInterval and loopMaintenanceState were scheduler symbols
# until DECOMPOSITION-MAP §3 Step 2 lifted the cadenced maintenance out of the
# poll loop (2026-07-29). They are the cadence state, so they went with it. The
# rest of the list is new in that commit. The forbidden-in-workloop.go scan
# applies to these EXACTLY as it does to the scheduler symbols: the run path has
# no business owning the loop's periodic state either.
MAINTENANCE_SYMS=(
    periodicCoordinatorReapInterval
    loopMaintenanceState
    maintenanceObservation
    loopMaintenance
    newLoopMaintenance
)

# The loopMaintenance METHODS cannot go in MAINTENANCE_SYMS: decl_pattern needs
# `func` immediately followed by the symbol, and a method reads
# `func (m *loopMaintenance) name(`. So they get their own scan below.
#
# An earlier version of this gate listed that as an accepted residual and claimed
# "a method cannot move to workloop.go without its receiver type going first".
# THAT CLAIM IS FALSE GO. A method declaration may sit in any file of the package
# while its receiver type stays put, so
# `func (m *loopMaintenance) tickBeforeDispatch(...)` in workloop.go compiles and
# fuses the seam. A mutation test proved that bypass GREEN. Hence METHOD_PAT.
MAINTENANCE_METHODS=(
    tickBeforeDispatch
    reapCoordinatorSessions
    tickBeforeSelect
    sentinelBlocksDispatch
)

# method_pattern matches a method declaration with ANY receiver. `[^)]*` cannot
# over-run the receiver list because a receiver holds no `)`.
#
# Verified 2026-07-29: one match per name across non-test internal/, all four in
# loopmaintenance.go, zero false positives.
method_pattern() {
    printf '^func \\([^)]*\\) %s\\(' "$1"
}

# FORBIDDEN-LOCATION ONLY — no declared-exactly-once check for methods, and that
# asymmetry with scan_inventory is deliberate.
#
# A package-level func/type/const/var name is unique per package by Go's own
# rules, so "exactly one declaration" is a safe assertion for MAINTENANCE_SYMS. A
# METHOD name is NOT unique: any number of types in one package may each declare
# the same method name. That is not hypothetical here — package daemon declares
# `tick` FOUR times, on dashboardGate, movementGovernor, BandwidthTuner
# (bandwidthtuner.go) and QuiesceArbiter (quiesce.go). Since method_pattern
# matches any receiver, a package-wide count would go red the day some unrelated
# type gained a method of the same name — a false failure, and the advice it would
# print (edit the inventory) would be wrong.
#
# Scoping the scan to workloop.go keeps the assertion true: no type declared
# anywhere should be growing these four method names inside the run-path file.
# Residual, stated: if some other receiver in workloop.go ever legitimately needs
# one of these four names, this goes red. That is a loud, quick-to-diagnose false
# positive on a name nobody has a reason to reuse, which is the right trade
# against leaving the bypass open.
for msym in "${MAINTENANCE_METHODS[@]}"; do
    MPAT="$(method_pattern "$msym")"
    MBACK="$(grep -n -E "$MPAT" "$WORKLOOP" 2>/dev/null || true)"
    if [ -n "$MBACK" ]; then
        echo "workloop-scheduler-freeze-gate: FORBIDDEN — the loopMaintenance method ${msym} is declared in ${WORKLOOP}; it belongs in ${MAINT}:" >&2
        printf '%s\n' "$MBACK" >&2
        echo "  A method may legally live in any file of the package, so the compiler will not stop this. This gate does." >&2
        HITS=$((HITS + 1))
    fi
    if ! grep -qE "$MPAT" "$MAINT" 2>/dev/null; then
        echo "workloop-scheduler-freeze-gate: ${msym} is no longer declared in ${MAINT} — re-derive this gate's method list" >&2
        HITS=$((HITS + 1))
    fi
done

# decl_pattern builds the anchored declaration regex for one symbol: a top-level
# func/type/const/var declaration at column 0. The keyword is REQUIRED. An
# optional keyword also matches every call site (`\tsnapshotFleet(lq, …)`) and
# makes the count assertion below useless, which is how a first draft of this
# gate reported six false failures against a correct tree.
decl_pattern() {
    printf '^(func|var|const|type)[[:space:]]+%s\\b' "$1"
}

# grouped_pattern matches the one spelling column-0 anchoring cannot see: a
# const/var/type declared INSIDE a grouped `const ( … )` / `var ( … )` /
# `type ( … )` block, where the keyword sits on an earlier line. A func cannot
# appear in such a block, so there is no `func` case.
#
# This is a REAL dodge, not a theoretical one, and it is scanned in BOTH
# directions for a reason. Move a scheduler scalar into a grouped block in
# workloop.go and decl_pattern finds nothing anywhere: the count check below
# reports "has 0 declarations", and its advice — drop the symbol from the
# inventory — is exactly what the evader wanted. So grouped hits feed the SAME
# inventory scan as column-0 hits, and a grouped hit in workloop.go gets its own
# message that says "grouped declaration", never "missing".
#
# The optional middle group is the declared type: a grouped const can be typed
# (`workloopPollInterval time.Duration = 2 * time.Second`) where a column-0 one
# usually is not. Verified 2026-07-29 to produce zero matches across all of
# internal/ for every inventoried symbol, so it adds no false-failure surface.
#
# Residual, stated rather than hidden: an implicit-iota continuation line inside
# a const block is a bare identifier, and no regex can tell it from ordinary
# prose without a Go parser. No inventoried symbol is of that shape.
grouped_pattern() {
    printf '^[[:space:]]+%s([[:space:]]+[][*.[:alnum:]_]+)?[[:space:]]*(=|struct[[:space:]]*\\{|interface[[:space:]]*\\{)' "$1"
}

# scan_inventory checks one inventory against one expected file. $1 is the file
# every symbol must be declared in; $2.. are the symbols. The two inventories
# differ ONLY in that expected file — the forbidden-in-workloop.go scan and the
# declared-exactly-once scan are identical for both.
scan_inventory() {
    local want=$1
    shift
    local sym PAT GPAT BACK GBACK DECLS COUNT
    for sym in "$@"; do
        PAT="$(decl_pattern "$sym")"
        GPAT="$(grouped_pattern "$sym")"

        # (2) Never in workloop.go. This is the specific regression the gate exists
        #     for. It runs BEFORE the count check on purpose: the count check ends in
        #     `continue`, and a symbol that moved back into workloop.go is exactly the
        #     case where the count is not 1. Ordered the other way round, a real
        #     regression is reported as a missing symbol and the reader is advised to
        #     edit the inventory.
        BACK="$(grep -n -E "$PAT" "$WORKLOOP" 2>/dev/null || true)"
        if [ -n "$BACK" ]; then
            echo "workloop-scheduler-freeze-gate: FORBIDDEN — ${sym} is declared in ${WORKLOOP}; it belongs in ${want}:" >&2
            printf '%s\n' "$BACK" >&2
            HITS=$((HITS + 1))
        fi
        GBACK="$(grep -n -E "$GPAT" "$WORKLOOP" 2>/dev/null || true)"
        if [ -n "$GBACK" ]; then
            echo "workloop-scheduler-freeze-gate: FORBIDDEN — ${sym} is declared in ${WORKLOOP} inside a grouped const/var/type block:" >&2
            printf '%s\n' "$GBACK" >&2
            echo "  The symbol is NOT missing — it was found in a grouped declaration. Move it back to ${want}." >&2
            HITS=$((HITS + 1))
        fi

        # (3) Declared exactly once across the non-test daemon sources, in the
        #     expected file. Both spellings count, so a grouped declaration reads
        #     as present rather than as missing.
        DECLS="$( { grep -rn --include='*.go' --exclude='*_test.go' -E "$PAT" internal/daemon 2>/dev/null || true
                    grep -rn --include='*.go' --exclude='*_test.go' -E "$GPAT" internal/daemon 2>/dev/null || true
                  } | grep -E '.' || true)"
        COUNT="$(printf '%s' "$DECLS" | grep -c . || true)"
        if [ "$COUNT" != "1" ]; then
            echo "workloop-scheduler-freeze-gate: ${sym} has ${COUNT} declarations in internal/daemon, expected 1:" >&2
            printf '%s\n' "$DECLS" >&2
            echo "  A symbol that was deleted or renamed must be removed from this gate's inventory in the SAME commit." >&2
            HITS=$((HITS + 1))
            continue
        fi
        if ! printf '%s' "$DECLS" | grep -q "^${want}:"; then
            echo "workloop-scheduler-freeze-gate: ${sym} is declared outside ${want}:" >&2
            printf '%s\n' "$DECLS" >&2
            echo "  Moving it on purpose? Move it in this gate's inventory in the SAME commit." >&2
            HITS=$((HITS + 1))
        fi
    done
}

scan_inventory "$SCHED" "${SCHEDULER_SYMS[@]}"
scan_inventory "$MAINT" "${MAINTENANCE_SYMS[@]}"

# (2b) The reverse direction. beadRunOne is the run driver and belongs in
#      workloop.go; pulling it into scheduler.go fuses the seam from the other
#      side.
BRO="$(grep -n -E '^[[:space:]]*func[[:space:]]+beadRunOne\b' "$SCHED" 2>/dev/null || true)"
if [ -n "$BRO" ]; then
    echo "workloop-scheduler-freeze-gate: FORBIDDEN — the run driver beadRunOne is declared in ${SCHED}:" >&2
    printf '%s\n' "$BRO" >&2
    HITS=$((HITS + 1))
fi
if ! grep -qE '^[[:space:]]*func[[:space:]]+beadRunOne\b' "$WORKLOOP" 2>/dev/null; then
    echo "workloop-scheduler-freeze-gate: beadRunOne is no longer declared in ${WORKLOOP} — re-derive this gate" >&2
    HITS=$((HITS + 1))
fi

# (5) THE BODY CHECK — the one that makes this gate about code and not names.
#
# Everything above freezes WHERE A NAME IS DECLARED. None of it notices this:
# move runWorkLoop's whole body into workloop.go as runWorkLoopImpl and leave a
# three-line pass-through named runWorkLoop in scheduler.go. Every symbol is
# still declared exactly once, still in scheduler.go, still absent from
# workloop.go. `go build`, `go vet` and checks (1)-(4) all pass, scheduler.go
# falls to roughly 950 lines and workloop.go climbs past 4,800, and the seam this
# gate exists to hold is completely fused. A reviewer demonstrated exactly that.
#
# So assert that the loop BODY is here, by measuring the runWorkLoop declaration.
#
# COUNT CODE LINES, NOT RAW LINES. A first version of this check counted raw
# lines in the declaration span, and a review broke it in one move: pad the
# one-statement shim with 205 comment lines. It measures 208 against a floor of
# 200, `go build` and `go vet` exit 0, and the gate goes GREEN with the seam
# fully fused. A comment-padded shim is the dangerous shape because it can look
# like ordinary engineering — a short function with a long explanation above the
# call. So blank lines and comment lines do not count.
#
# WHY A DECLARATION FLOOR AND NOT A FILE-LINE FLOOR ON scheduler.go:
#   - Extraction INTO a new helper in scheduler.go is line-neutral for the file,
#     so a file count does not track the property we care about.
#   - The decomposition plan's own next steps (DECOMPOSITION-MAP.md §3 Steps 2-4)
#     lift lines OUT of this loop into new files and packages, so scheduler.go
#     shrinks as planned work lands. A file floor tight enough to catch a PARTIAL
#     hollow-out would go red on that planned work. The pass-through bypass lands
#     the file at ~950, so a file floor would have little margin on either side.
#
# WHY 200. Measured 2026-07-29, not estimated:
#   - runWorkLoop is 1,353 raw lines and 796 code lines. The loop is heavily
#     commented, so the two numbers are far apart — a review guessed the code
#     count at "roughly 1,400" and it is 796. Read 796, not 1,353, when moving
#     this number. (beadRunOne, for scale: 1,773 raw, 873 code.)
#   - Either pass-through shim, padded or not, measures 3 code lines. 200 is 66x
#     that, and no amount of comment padding moves it.
#   - 200 against 796 means the loop can shed three quarters of its code before
#     this objects.
#   - Step 2 (the cadenced-maintenance lift) has now landed and it moved the
#     number far less than the plan implied. It took runWorkLoop from 1,422 raw /
#     811 code to 1,353 raw / 796 code: 69 raw and 15 code lines. The plan priced
#     Step 2 at "~300 lines" because it counted the comment-heavy span in the
#     loop, not the statements. Steps 3-6 lift larger, genuinely statement-dense
#     chunks, but re-derive each one the same way before trusting its estimate.
#   - It is a FLOOR, so the loop may grow without limit. It cannot cry wolf on
#     ordinary work.
#   - If runWorkLoop ever becomes a genuinely thin orchestrator over phase
#     functions, this check SHOULD go red: that is an architectural change, so
#     move the number in the same commit and record why. Do not nudge it to get
#     green.
#
# TWO RESIDUALS, stated so nobody mistakes this for total cover:
#   - The counter is line-based, not a Go parser. It does not know string
#     literals, so a line INSIDE a raw string literal that begins with `//` is
#     counted as a comment. That direction is safe: it can only UNDER-count,
#     which makes the floor harder to clear, never easier. The reverse — a `//`
#     inside a string on a line that also has code — is not affected, because
#     only lines that START with a comment marker are skipped. There is no raw
#     string literal in this declaration today: all 10 backticks in the span sit
#     inside `//` comments or inside one double-quoted string.
#   - Padding with real statements, or with a raw string literal used as
#     filler, still clears the floor. Accepted: any size floor can be padded
#     with real code, and 200 lines of filler cannot pass as ordinary
#     engineering the way a long comment can. This check raises the price of the
#     bypass; it does not make it impossible.
#   - The symbol inventories above are hardcoded snapshots, so brand-new
#     scheduler logic written directly into workloop.go under a name nobody has
#     listed still passes. That needs a reviewer, not a grep. This is the most
#     important limit on this whole script: it freezes the names it was told
#     about, and nothing teaches it a new one.
#   - Methods ARE now checked, but only for location (see MAINTENANCE_METHODS),
#     not for count. A method name is not unique per package, so a count would
#     false-fail. A method added to workloop.go on some OTHER receiver, under a
#     name not in that list, is not caught.
RUNWORKLOOP_MIN_CODE_LINES=200

# decl_code_lines counts the NON-BLANK, NON-COMMENT lines of a top-level
# declaration: its `func` line through the next column-0 `}`.
#
# gofmt puts the closing brace of a top-level func at column 0 and indents every
# line inside the body, so the span is exact. Verified against scheduler.go on
# 2026-07-29: every column-0 `}` line belongs to a top-level declaration, so
# nothing in the file smuggles an extra one in. Anchoring on the next `^func`
# instead would break when the declaration is the last one in the file.
#
# Block comments are tracked across lines, including one opened after code on the
# same line, so a `/* … */` pad cannot be counted as code either.
decl_code_lines() {
    local file=$1 sym=$2 start end
    start="$(grep -n -E "^func[[:space:]]+${sym}\b" "$file" | head -1 | cut -d: -f1)"
    if [ -z "$start" ]; then
        echo ""
        return
    fi
    end="$(awk -v s="$start" 'NR>=s && /^}/ {print NR; exit}' "$file")"
    if [ -z "$end" ]; then
        echo ""
        return
    fi
    awk -v s="$start" -v e="$end" '
        NR < s { next }
        NR > e { exit }
        {
            line = $0
            sub(/^[[:space:]]+/, "", line)
            if (inblock) {
                i = index(line, "*/")
                if (i == 0) { next }
                line = substr(line, i + 2)
                inblock = 0
                sub(/^[[:space:]]+/, "", line)
            }
            while (line ~ /^\/\*/) {
                i = index(line, "*/")
                if (i == 0) { inblock = 1; line = ""; break }
                line = substr(line, i + 2)
                sub(/^[[:space:]]+/, "", line)
            }
            if (line == "") { next }
            if (line ~ /^\/\//) { next }
            n++
            j = index(line, "/*")
            if (j > 0) {
                rest = substr(line, j + 2)
                if (index(rest, "*/") == 0) { inblock = 1 }
            }
        }
        END { print n + 0 }
    ' "$file"
}

RWL_CODE="$(decl_code_lines "$SCHED" runWorkLoop)"
if [ -z "$RWL_CODE" ]; then
    echo "workloop-scheduler-freeze-gate: could not measure the runWorkLoop declaration in ${SCHED} — re-derive this gate" >&2
    HITS=$((HITS + 1))
elif [ "$RWL_CODE" -lt "$RUNWORKLOOP_MIN_CODE_LINES" ]; then
    echo "workloop-scheduler-freeze-gate: FORBIDDEN — runWorkLoop in ${SCHED} has only ${RWL_CODE} code lines (blank and comment lines do not count), floor is ${RUNWORKLOOP_MIN_CODE_LINES}." >&2
    echo "  The scheduler loop's BODY must live in ${SCHED}, not just its name. A short runWorkLoop means the" >&2
    echo "  body moved somewhere else — most likely into ${WORKLOOP} behind a pass-through, which fuses the seam" >&2
    echo "  with every name check still green. Padding the shim with comments does not clear this floor." >&2
    echo "  If the loop legitimately became a thin orchestrator, move the floor in the same commit and say why." >&2
    HITS=$((HITS + 1))
fi

if [ "$HITS" -ne 0 ]; then
    echo "workloop-scheduler-freeze-gate: FAIL — the scheduler was split out of workloop.go at Seam A; keep it in ${SCHED}" >&2
    exit 1
fi
echo "workloop-scheduler-freeze-gate: OK — the scheduler stays out of workloop.go"
