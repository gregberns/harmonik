#!/usr/bin/env bash
set -euo pipefail

# runloop-emitter-gate.sh — P2 unit E5 RT16 freeze tripwire
# (plans/2026-07-21-p2-extraction/RT16-emitterport-conversion.md §4 step 8;
#  _plan.md §3.2).
#
# The DOT run path reaches its event bus through EmitterPort — the type alias
# that (as of P2 LIFT L0) lives in internal/runloop/ports.go, structurally reached
# in internal/daemon via the (*workLoopDeps).emitterPort() constructor — not
# through the workLoopDeps bus field. RT16 converted 108 direct field reads on
# the six MOVER files to port reads. RT18 then re-signed the consumers around
# RunPorts, P2 split dot_cascade.go into core/helpers, and LIFT L6 moved
# runbridge.go into internal/runloop. The ownership table below pins those
# current files and their measured code sites. A new field read on a mover, or a
# consumer leaving the port, un-does that.
#
# depguard cannot express "reach this dependency through its port", so this grep
# ratchet is the only thing holding the line. workloop.go takes ~3.7 commits/day;
# without a gate the count silently regrows during the continuing extraction.
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
# today's dot_cascade_core.go/helpers.go split for exactly that reason. Test files
# are out of scope (two
# historical-fix comments in pasteinject_hk*_test.go name the old idiom and are
# documentation, not code).
#
# KNOWN LIMITATION, stated so the next maintainer knows it was a choice: this
# matches the RECEIVER NAME, so a read through a differently-named receiver —
# `func (d *workLoopDeps) f() { _ = d.bus }` — is invisible to it. That is a
# convention dependency, not a live hole: all 11 current workLoopDeps methods
# and every current function parameter of that type use the name `deps`.
# Matching `\.bus\b` instead would fire on bootstate.go's unrelated `bs.bus`
# field and make the gate red on arrival. If the receiver convention ever
# breaks, widen this and re-measure the budgets rather than loosening them.
FIELD_RE='deps\.bus'

# count_matches counts OCCURRENCES, not lines. `grep -c` counts matching LINES,
# and a run-path line can legitimately carry two reads — the same trap this
# slice's own recipe corrections flagged. Measured: rewriting one already-counted
# runWorkLoop line to hold two reads takes workloop.go from 9 occurrences to 10
# while `grep -c` still reports 9, and the exact-9 assertion below — the whole
# point of the ratchet — sails straight through. The `|| true` is required
# because this script runs under `set -o pipefail` and grep exits 1 on no match.
#
# -E is load-bearing in both counting helpers: PORT_RE below is an alternation,
# and counting it under basic regex would match the literal string "|" and
# silently report 0.
count_matches() {
    { grep -oE "$2" "$1" || true; } | wc -l | tr -d ' '
}

# strip_go_comments excludes line and block comments. It intentionally serves
# only the narrow, measured port spellings below; FIELD_RE retains its stricter
# comments-count behavior above.
strip_go_comments() {
    awk '
        BEGIN { in_block = 0 }
        {
            line = $0
            code = ""
            while (length(line) > 0) {
                if (in_block) {
                    end = index(line, "*/")
                    if (end == 0) {
                        line = ""
                        continue
                    }
                    line = substr(line, end + 2)
                    in_block = 0
                    continue
                }

                block = index(line, "/*")
                slash = index(line, "//")
                if (slash > 0 && (block == 0 || slash < block)) {
                    code = code substr(line, 1, slash - 1)
                    line = ""
                    continue
                }
                if (block > 0) {
                    code = code substr(line, 1, block - 1)
                    line = substr(line, block + 2)
                    in_block = 1
                    continue
                }
                code = code line
                line = ""
            }
            print code
        }
    ' "$@"
}

# Call-site pins must count executable spellings: otherwise a removed
# ports.Emitter access plus a comment containing that text can leave the ratchet
# falsely green.
count_code_matches() {
    strip_go_comments "$1" | { grep -oE "$2" || true; } | wc -l | tr -d ' '
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
#     MOVERS (budget 0 / workloop.go 8) are asserted EXACTLY, in both
#     directions. A shrink is not silently accepted because the survivors in
#     workloop.go are a deliberate, documented carve-out — runWorkLoop, the outer
#     queue-claim loop, x7, plus evaluateGroupAdvanceWithOutcome. Those functions
#     stay in internal/daemon forever (E5-dot-runloop.md §1c), so converting them
#     is churn on the tree's
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
    # 9 -> 8 on 2026-07-28: the dead activateFirstPendingGroup (superseded by
    # activateFirstPendingGroupLocked, zero callers) was deleted, and with it the
    # 9th read — its post-unlock deps.bus.Emit loop. Ratchet down, never up.
    #
    # 8 -> 3 on 2026-07-28 (subsystem-partition-01): NOT a global sed — RT16 §7
    # risk 1 is what this assertion exists to catch, so read the reason. Five of
    # the eight reads were the outer poll loop's dashboard-forcing-gate and
    # sentinel-governor blocks, which were lifted verbatim out of runWorkLoop into
    # dashboardgate.go and movementgovernor.go so those two non-core subsystems
    # can be switched off and never constructed (CHARTER §4). They are still the
    # SAME outer-queue-claim-loop reads and still never leave internal/daemon —
    # they are re-budgeted below, not converted and not deleted. Ratchet down.
    "internal/daemon/workloop.go             3"
    # reviewloop.go left this list with the review-loop retirement that deleted
    # the file. agentlaunch.go took its place on 2026-07-29: the launch-path
    # collapse made it the single launch path, and it reaches the bus only
    # through the port, so it belongs at the same zero budget.
    "internal/daemon/agentlaunch.go          0"
    "internal/daemon/dot_cascade_core.go     0"
    "internal/daemon/dot_cascade_helpers.go  0"
    "internal/daemon/dot_gate.go             0"
    "internal/daemon/sub_workflow_runner.go  0"
)
declare -a CEILING_FILES=(
    "internal/daemon/runports.go                    3"
    "internal/daemon/diskcheck_hksxlb.go            3"
    "internal/daemon/eagerfill_em063.go             2"
    "internal/daemon/workloop_handlerpause_kac8g.go 3"
    # subsystem-partition-01, 2026-07-28: the outer poll loop's dashboard-gate and
    # movement-governor blocks, lifted out of runWorkLoop so each can be switched
    # off and never constructed. Same category as the diskcheck / eager-fill rows
    # above — outer-loop instrumentation, not an RT16 mover — so CEILING, and a
    # later shrink is pure improvement rather than a build failure.
    "internal/daemon/dashboardgate.go               2"
    "internal/daemon/movementgovernor.go            2"
)

budget_for() { # path -> "exact <n>" | "ceiling <n>" | "exact 0"
    local p="$1" row path limit
    for row in "${EXACT_FILES[@]}"; do
        read -r path limit <<<"$row"
        [ "$path" = "$p" ] && { echo "exact $limit"; return; }
    done
    for row in "${CEILING_FILES[@]}"; do
        read -r path limit <<<"$row"
        [ "$path" = "$p" ] && { echo "ceiling $limit"; return; }
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
    read -r f _ <<<"$row"
    if [ ! -f "$f" ]; then
        echo "runloop-emitter-gate: budgeted file $f is gone — re-derive this gate" >&2
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
# P2 LIFT L0 moved the EmitterPort alias and the RunPorts.Emitter field to
# internal/runloop/ports.go (the run-path port SURFACE). The daemon KEEPS the
# emitterPort() constructor (daemon → runloop direction), now returning
# runloop.EmitterPort. The alias-ness argument is unchanged — it just lives one
# package over — so these three assertions follow the symbols to their new homes.
if ! grep -qE '^type EmitterPort = handlercontract\.EventEmitter$' internal/runloop/ports.go; then
    echo "runloop-emitter-gate: EmitterPort is no longer 'type EmitterPort = handlercontract.EventEmitter'" >&2
    echo "  in internal/runloop/ports.go. RT16's zero-risk argument rests on it being an ALIAS." >&2
    HITS=$((HITS + 1))
fi
if ! grep -q 'func (deps \*workLoopDeps) emitterPort() runloop.EmitterPort' internal/daemon/runports.go; then
    echo "runloop-emitter-gate: (*workLoopDeps).emitterPort is gone — re-derive this gate" >&2
    HITS=$((HITS + 1))
fi
if ! grep -qE '^\s*Emitter\s+EmitterPort$' internal/runloop/ports.go; then
    echo "runloop-emitter-gate: RunPorts.Emitter is gone — re-derive this gate" >&2
    HITS=$((HITS + 1))
fi

# ---------------------------------------------------------------------------
# (3) Pin the CALL SITES, not just the seam. "The seam still exists" passes even
#     if every site quietly abandoned it — the hole RT14's gate had to close
#     (PROGRESS.md §RT14, deviation 2). Each current owner must still reach the
#     emitter through the port at exactly the measured number of code sites.
#
#     PORT_RE is the MEASURED post-RT18/LIFT set of spellings, not an anticipated
#     one. count_code_matches excludes comments, exact counts reject stale/dummy
#     accesses, and every listed path is fail-closed on rename or deletion.
PORT_RE='\brp\.Emitter\b|\bports\.Emitter\b'
declare -a PORT_SITES=(
    "internal/daemon/workloop.go            4"  # beadRunOne x2; close + epic helpers x2
    "internal/daemon/agentlaunch.go         1"  # runAgentLaunch binds
    "internal/daemon/dot_cascade_core.go    2"  # driveDotWorkflow + dispatchDotAgenticNode bind
    "internal/daemon/dot_gate.go            2"  # executeCognitionGate binds; dispatchDotGateNode reads inline
    "internal/runloop/runbridge.go           3"  # three inline b.rp.Emitter reads, no local
    "internal/daemon/sub_workflow_runner.go 2"  # two inline r.ports.Emitter reads, no local
)
for row in "${PORT_SITES[@]}"; do
    read -r f want <<<"$row"
    if [ ! -f "$f" ]; then
        echo "runloop-emitter-gate: port consumer $f is gone — re-derive this gate" >&2
        HITS=$((HITS + 1))
        continue
    fi
    got="$(count_code_matches "$f" "$PORT_RE")"
    if [ "$got" -ne "$want" ]; then
        echo "runloop-emitter-gate: $f reaches the emitter through the port at $got code site(s), expected exactly $want" >&2
        echo "  — a run-path site left/bypassed the seam, or a stale/dummy access was added." >&2
        echo "  Bind the real site from RunPorts.Emitter; comments do not satisfy this count." >&2
        HITS=$((HITS + 1))
    fi
done

# Pin the owning SYMBOLS as well as aggregate per-file counts. This prevents a
# surviving or dummy access elsewhere in the same file from masking one owner
# that bypassed the port.
declare -a PORT_SYMBOL_SITES=(
    "internal/daemon/workloop.go|^func \\(deps \\*workLoopDeps\\) buildRunBundles\\(|buildRunBundles|1"
    "internal/daemon/workloop.go|^func beadRunOne\\(|beadRunOne|1"
    "internal/daemon/workloop.go|^func emitBeadClosedAndMaybeEpic\\(|emitBeadClosedAndMaybeEpic|1"
    "internal/daemon/workloop.go|^func maybeEmitEpicCompleted\\(|maybeEmitEpicCompleted|1"
    "internal/daemon/agentlaunch.go|^func runAgentLaunch\\(|runAgentLaunch|1"
    "internal/daemon/dot_cascade_core.go|^func driveDotWorkflow\\(|driveDotWorkflow|1"
    "internal/daemon/dot_cascade_core.go|^func dispatchDotAgenticNode\\(|dispatchDotAgenticNode|1"
    "internal/daemon/dot_gate.go|^func dispatchDotGateNode\\(|dispatchDotGateNode|1"
    "internal/daemon/dot_gate.go|^func executeCognitionGate\\(|executeCognitionGate|1"
    "internal/runloop/runbridge.go|^func \\(b \\*RunBridge\\) emit\\(|RunBridge.emit|1"
    "internal/runloop/runbridge.go|^func \\(b \\*RunBridge\\) mergeHook\\(|RunBridge.mergeHook|1"
    "internal/runloop/runbridge.go|^func \\(b \\*RunBridge\\) drainMergeHook\\(|RunBridge.drainMergeHook|1"
    "internal/daemon/sub_workflow_runner.go|^func \\(r \\*dotSubWorkflowRunner\\) Run\\(|dotSubWorkflowRunner.Run|1"
    "internal/daemon/sub_workflow_runner.go|^func dispatchSubWorkflowExpandedNode\\(|dispatchSubWorkflowExpandedNode|1"
)
for row in "${PORT_SYMBOL_SITES[@]}"; do
    IFS='|' read -r f signature symbol want <<<"$row"
    [ -f "$f" ] || continue # PORT_SITES already reports the stale owner path.

    declarations="$(grep -nE "$signature" "$f" || true)"
    declaration_count="$(printf '%s\n' "$declarations" | awk 'NF { n++ } END { print n + 0 }')"
    if [ "$declaration_count" -ne 1 ]; then
        echo "runloop-emitter-gate: could not uniquely locate $symbol in $f — re-derive this gate" >&2
        HITS=$((HITS + 1))
        continue
    fi

    start="${declarations%%:*}"
    end="$(awk -v start="$start" '
        NR > start && /^func / {
            print NR - 1
            found = 1
            exit
        }
        END {
            if (!found) {
                print NR
            }
        }
    ' "$f")"
    got="$(sed -n "${start},${end}p" "$f" \
        | strip_go_comments \
        | { grep -oE "$PORT_RE" || true; } \
        | wc -l \
        | tr -d ' ')"
    if [ "$got" -ne "$want" ]; then
        echo "runloop-emitter-gate: $f $symbol reaches the emitter through the port at $got code site(s), expected exactly $want" >&2
        HITS=$((HITS + 1))
    fi
done

if [ "$HITS" -ne 0 ]; then
    echo "" >&2
    echo "runloop-emitter-gate: FAIL — the run path reaches its bus through EmitterPort (P2 E5 RT16)." >&2
    echo "In a function that already binds it, use 'emit'. Otherwise use RunPorts.Emitter (for example" >&2
    echo "rp.Emitter, ports.Emitter, or b.rp.Emitter). If a read genuinely belongs to the OUTER" >&2
    echo "queue-claim loop (runWorkLoop and" >&2
    echo "friends, which never leave internal/daemon), raise workloop.go's budget here and say why in" >&2
    echo "the commit body." >&2
    exit 1
fi
echo "runloop-emitter-gate: OK — the run path stays on EmitterPort"
