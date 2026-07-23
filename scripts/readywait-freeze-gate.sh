#!/usr/bin/env bash
set -euo pipefail

# readywait-freeze-gate.sh — P2 unit E5 RT14 freeze tripwire
# (plans/2026-07-21-p2-extraction/RT14-dispatchsegment-conversion.md §4 C7; _plan.md §3.2).
#
# The open-coded agent_ready WAIT left internal/daemon in slice RT14. Every
# launch/ready/brief segment now runs on the runexec Dispatch machine via
# dispatchSegment (dispatchsegment.go) — a ClockPort-timed, FakeClock-drivable
# bound. depguard cannot express "do not re-hand-roll a wall-clock wait", so this
# grep ratchet closes that door.
#
# NOT policed here (deliberately): the Working-phase watchdogs
# (pasteinject.go, dot_gate.go's pasteInjectQuitOnGateFile, waitsocketgrace.go,
# postreadyhang.go) still use raw wall-clock. dispatchsegment.go's header places
# them OUTSIDE the RT8 segment boundary; slice RT19c owns them. Adding them here
# would make this gate red on landing.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No re-declaration of the retired ready-wait symbols anywhere in the daemon
#     (recursive — a sub-package is the obvious evasion).
for sym in waitAgentReady agentEventSource chanAgentEventSource newChanAgentEventSource; do
    MATCHES="$(grep -rn --include='*.go' -E "^[[:space:]]*(func|var|const|type)?[[:space:]]*${sym}\b[[:space:]]*(=|struct|interface|func|\()" internal/daemon 2>/dev/null || true)"
    if [ -n "$MATCHES" ]; then
        echo "readywait-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
done

# (2) The run-path files that are wall-clock CLEAN today stay clean. The
#     dispatch path must remain FakeClock-drivable end to end.
#
#     agentready.go is NOT in this list because RT14 deleted the file outright.
#     RT19b-3 had already moved its four surviving policy scalars to
#     internal/runlaunch, so once waitAgentReady + agentEventSource went, nothing
#     was left. Check (1) is what keeps those symbols from coming back — pinning
#     a deleted path here would make the gate fail on its own landing commit.
for f in internal/daemon/dispatchsegment.go internal/daemon/runshell.go \
         internal/daemon/runbridge.go internal/daemon/reviewloop.go \
         internal/daemon/dot_cascade.go internal/daemon/workloopeventsource.go; do
    if [ ! -f "$f" ]; then
        echo "readywait-freeze-gate: pinned file $f is gone — re-derive this gate's file list" >&2
        HITS=$((HITS + 1))
        continue
    fi
    N="$(grep -cE 'time\.(After|Now|NewTimer|NewTicker|Tick|Sleep)\(' "$f" || true)"
    if [ "$N" -ne 0 ]; then
        echo "readywait-freeze-gate: FORBIDDEN raw wall-clock in $f ($N site(s)) — use substrate.After / the ClockPort:" >&2
        grep -nE 'time\.(After|Now|NewTimer|NewTicker|Tick|Sleep)\(' "$f" >&2
        HITS=$((HITS + 1))
    fi
done

# (3) beadRunOne stays wall-clock clean. Anchored on its two boundary symbols so
#     the range survives the line drift that RT15 will cause.
START="$(grep -n '^func beadRunOne' internal/daemon/workloop.go | head -1 | cut -d: -f1)"
END="$(awk -v s="$START" 'NR>s && /^func /{print NR; exit}' internal/daemon/workloop.go)"
if [ -n "$START" ] && [ -n "$END" ]; then
    MATCHES="$(awk -v s="$START" -v e="$END" 'NR>=s && NR<=e' internal/daemon/workloop.go \
               | grep -nE 'time\.(After|Now|NewTimer|NewTicker|Tick|Sleep)\(' || true)"
    if [ -n "$MATCHES" ]; then
        echo "readywait-freeze-gate: FORBIDDEN raw wall-clock inside beadRunOne (offsets from line $START):" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
else
    echo "readywait-freeze-gate: could not locate beadRunOne in workloop.go — re-derive this gate" >&2
    HITS=$((HITS + 1))
fi

# (4) The seam must still exist. A gate whose target was renamed away silently
#     stops testing what it claims to test.
if ! grep -q '^type dispatchSegment struct' internal/daemon/dispatchsegment.go; then
    echo "readywait-freeze-gate: dispatchSegment is gone — re-derive this gate" >&2
    HITS=$((HITS + 1))
fi

# (5) Every agent-launch site binds through the seam. RT14 took the number of
#     sites that hand-roll their own ready wait to ZERO; each of the four
#     consumers must still construct a dispatchSegment.
for f in internal/daemon/workloop.go internal/daemon/dot_gate.go \
         internal/daemon/reviewloop.go internal/daemon/dot_cascade.go; do
    if ! grep -q '&dispatchSegment{' "$f"; then
        echo "readywait-freeze-gate: $f no longer builds a dispatchSegment — a launch site left the seam" >&2
        HITS=$((HITS + 1))
    fi
done

if [ "$HITS" -ne 0 ]; then
    echo "readywait-freeze-gate: FAIL — the open-coded agent_ready wait was retired in P2 E5 RT14; bind onto dispatchSegment, do not re-hand-roll it" >&2
    exit 1
fi
echo "readywait-freeze-gate: OK — the ready wait stays on the Dispatch machine"
