#!/usr/bin/env bash
set -euo pipefail

# runloop-emitter-gate.sh — P2 unit E5 RT16 freeze tripwire
# (plans/2026-07-21-p2-extraction/RT16-emitterport-conversion.md §4 step 8;
#  _plan.md §3.2).
#
# The DOT run path reaches its event bus through EmitterPort — the type alias in
# internal/daemon/runports.go — not through the workLoopDeps bus field. RT16
# converted 108 direct field reads on the six MOVER files to 8 port reads, so
# RT18's re-signature of beadRunOne / runReviewLoop / driveDotWorkflow /
# dispatchDotAgenticNode / executeCognitionGate is an 8-line change instead of a
# 108-line one. A new field read on a mover un-does that.
#
# depguard cannot express "reach this dependency through its port", so this grep
# ratchet is the only thing holding the line. workloop.go takes ~3.7 commits/day;
# without a gate the count silently regrows before RT17/RT18 land.
#
# Exit 0: clean. Exit 1: the port was bypassed.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# FIELD_RE — the raw field read this gate rations. Plain text: COMMENTS COUNT.
# Stripping them would need a Go parser, and a gate that misses a real site is
# worse than one that occasionally objects to prose — so in these files, describe
# the field without writing its literal spelling ("the run emitter", not the
# field access). RT16 reworded the two hk-sj6a / hk-e7n76 prose lines in
# dot_cascade.go for exactly that reason. Test files are out of scope (two
# historical-fix comments in pasteinject_hk*_test.go name the old idiom and are
# documentation, not code).
#
# KNOWN LIMITATION, stated so the next maintainer knows it was a choice: this
# matches the RECEIVER NAME, so a read through a differently-named receiver —
# `func (d *workLoopDeps) f() { _ = d.bus }` — is invisible to it. That is a
# convention dependency, not a live hole: all nine current workLoopDeps methods
# name their receiver `deps`, and all five movers take `deps` as a value
# parameter. Matching `\.bus\b` instead would fire on bootstate.go's unrelated
# `bs.bus` field and make the gate red on arrival. If the receiver convention
# ever breaks, widen this and re-measure the budgets rather than loosening them.
FIELD_RE='deps\.bus'

# count_matches counts OCCURRENCES, not lines. `grep -c` counts matching LINES,
# and a run-path line can legitimately carry two reads — the same trap this
# slice's own recipe corrections flagged. Measured: rewriting one already-counted
# runWorkLoop line to hold two reads takes workloop.go from 10 occurrences to 11
# while `grep -c` still reports 10, and the exact-10 assertion below — the whole
# point of the ratchet — sails straight through. The `|| true` is required
# because this script runs under `set -o pipefail` and grep exits 1 on no match.
#
# -E is load-bearing: PORT_RE below is an alternation, and counting it under
# basic regex would match the literal string "|" and silently report 0 — which,
# for a check whose failure condition is "fewer than expected", would turn the
# call-site pin permanently RED rather than silently green. Both callers pass an
# ERE, so the flag is set here once rather than per call.
count_matches() {
    { grep -oE "$2" "$1" || true; } | wc -l | tr -d ' '
}

# ---------------------------------------------------------------------------
# (1) Per-file budget over EVERY non-test Go file under internal/daemon.
#
#     Recursive on purpose. internal/daemon has three real sub-packages
#     (bootconfig, router, scenariotest) and a `find -maxdepth 1` scan — the
#     known blind spot in the transport gate, PROGRESS.md §5 item 3 — would let a
#     sub-package hold any number of field reads. A file absent from the table
#     is budgeted at ZERO, so a brand-new daemon file cannot smuggle one in.
#
#     MOVERS (budget 0 / workloop.go 9) are asserted EXACTLY, in both
#     directions. A shrink is not silently accepted because the survivors in
#     workloop.go are a deliberate, documented carve-out — runWorkLoop, the outer
#     queue-claim loop, x7, plus activateFirstPendingGroup and
#     evaluateGroupAdvanceWithOutcome. Those functions stay in internal/daemon
#     forever (E5-dot-runloop.md §1c), so converting them is churn on the tree's
#     hottest file for zero extraction value. RT16 §7 risk 1 predicts an
#     implementer reaching for a global sed; an exact assertion is what turns
#     that into a RED gate instead of a silent scope creep.
#
#     RT18.9 lowered this 10→9: maybeEmitEpicCompleted's single deps.bus read
#     was folded onto ports.Emitter so emitBeadClosedAndMaybeEpic (its caller,
#     invoked from the runBridge close hook) could take the RunPorts/SharedHandles
#     bundles instead of deps — the precondition for dropping deps from
#     newRunBridge. That is strictly MORE port usage, not scope creep.
#
#     NON-MOVERS are budgeted at their measured count as a CEILING only. They are
#     not RT16's targets (boot/disk/eager-fill instrumentation, and the port
#     definition itself), so a legitimate shrink there is pure improvement and
#     must not fail the build.
declare -a EXACT_FILES=(
    "internal/daemon/workloop.go             9"
    "internal/daemon/reviewloop.go           0"
    "internal/daemon/dot_cascade.go          0"
    "internal/daemon/dot_gate.go             0"
    "internal/daemon/runbridge.go            0"
    "internal/daemon/sub_workflow_runner.go  0"
)
declare -a CEILING_FILES=(
    "internal/daemon/runports.go                    3"
    "internal/daemon/diskcheck_hksxlb.go            3"
    "internal/daemon/eagerfill_em063.go             2"
    "internal/daemon/workloop_handlerpause_kac8g.go 3"
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
        echo "runloop-emitter-gate: $f has $got raw bus-field read(s), budget is $want:" >&2
        grep -n "$FIELD_RE" "$f" >&2
        HITS=$((HITS + 1))
    elif [ "$kind" = "exact" ] && [ "$got" -lt "$want" ]; then
        echo "runloop-emitter-gate: $f has $got raw bus-field read(s), budget asserts exactly $want." >&2
        echo "  A SHRINK here is the global-sed failure mode (RT16 §7 risk 1): the surviving" >&2
        echo "  reads belong to the outer queue-claim loop, which never leaves internal/daemon." >&2
        echo "  If the shrink is deliberate, lower the budget in this script and say why." >&2
        HITS=$((HITS + 1))
    fi
done < <(find internal/daemon -type f -name '*.go' ! -name '*_test.go' | sort)

# Missing-file guard: a budgeted file that has been renamed or deleted makes the
# whole per-file table meaningless without ever failing check (1), because the
# find loop simply never visits it.
for row in "${EXACT_FILES[@]}" "${CEILING_FILES[@]}"; do
    set -- $row
    if [ ! -f "$1" ]; then
        echo "runloop-emitter-gate: budgeted file $1 is gone — re-derive this gate" >&2
        HITS=$((HITS + 1))
    fi
done

# ---------------------------------------------------------------------------
# (2) The seam must still exist, AND still be an ALIAS. The `=` is the entire
#     safety argument for RT16: because EmitterPort is a type alias rather than a
#     defined type, the field and the port hold the same static type, so the
#     conversion cannot change a method set, an interface conversion or a nil
#     check. Turning it into `type EmitterPort handlercontract.EventEmitter`
#     would compile at some call sites and silently change others.
if ! grep -qE '^type EmitterPort = handlercontract\.EventEmitter$' internal/daemon/runports.go; then
    echo "runloop-emitter-gate: EmitterPort is no longer 'type EmitterPort = handlercontract.EventEmitter'" >&2
    echo "  in internal/daemon/runports.go. RT16's zero-risk argument rests on it being an ALIAS." >&2
    HITS=$((HITS + 1))
fi
if ! grep -q 'func (deps \*workLoopDeps) emitterPort() EmitterPort' internal/daemon/runports.go; then
    echo "runloop-emitter-gate: (*workLoopDeps).emitterPort is gone — re-derive this gate" >&2
    HITS=$((HITS + 1))
fi
if ! grep -qE '^\s*Emitter\s+EmitterPort$' internal/daemon/runports.go; then
    echo "runloop-emitter-gate: RunPorts.Emitter is gone — re-derive this gate" >&2
    HITS=$((HITS + 1))
fi

# ---------------------------------------------------------------------------
# (3) Pin the CALL SITES, not just the seam. "The seam still exists" passes even
#     if every site quietly abandoned it — the hole RT14's gate had to close
#     (PROGRESS.md §RT14, deviation 2). Each mover must still reach the emitter
#     through the port at least as many times as RT16 left it doing.
#
#     PORT_RE is the MEASURED set of spellings, not an anticipated one. RT18
#     re-signs these functions to take a ports bundle; each re-signed reader
#     spells the emitter `ports.Emitter`, so RT18-S added `\bports\.Emitter\b`
#     here as the first re-sign landed (runReviewLoop). The per-file PORT_SITES
#     counts are a 1-for-1 spelling swap and stay satisfied — do not lower them.
PORT_RE='emitterPort\(\)|\brp\.Emitter\b|runPorts\(\)\.Emitter|\bports\.Emitter\b'
declare -a PORT_SITES=(
    "internal/daemon/workloop.go            2"  # beadRunOne binds; emitBeadClosedAndMaybeEpic reads the bundle
    "internal/daemon/reviewloop.go          1"  # runReviewLoop binds
    "internal/daemon/dot_cascade.go         2"  # driveDotWorkflow + dispatchDotAgenticNode bind
    "internal/daemon/dot_gate.go            2"  # executeCognitionGate binds; dispatchDotGateNode reads inline
    "internal/daemon/runbridge.go           3"  # three inline b.rp.Emitter reads, no local
    "internal/daemon/sub_workflow_runner.go 2"  # two inline r.deps.emitterPort() reads, no local
)
for row in "${PORT_SITES[@]}"; do
    set -- $row
    f="$1"; want="$2"
    [ -f "$f" ] || continue   # the missing-file guard above already counted it
    got="$(count_matches "$f" "$PORT_RE")"
    if [ "$got" -lt "$want" ]; then
        echo "runloop-emitter-gate: $f reaches the emitter through the port only $got time(s), expected >= $want" >&2
        echo "  — a run-path site left the seam. Bind 'emit' from the port, or update this budget if RT18 re-signed it." >&2
        HITS=$((HITS + 1))
    fi
done

if [ "$HITS" -ne 0 ]; then
    echo "" >&2
    echo "runloop-emitter-gate: FAIL — the run path reaches its bus through EmitterPort (P2 E5 RT16)." >&2
    echo "In a function that already binds it, use 'emit'. Otherwise use deps.emitterPort(), rp.Emitter" >&2
    echo "or b.rp.Emitter. If a read genuinely belongs to the OUTER queue-claim loop (runWorkLoop and" >&2
    echo "friends, which never leave internal/daemon), raise workloop.go's budget here and say why in" >&2
    echo "the commit body." >&2
    exit 1
fi
echo "runloop-emitter-gate: OK — the run path stays on EmitterPort"
