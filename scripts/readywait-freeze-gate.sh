#!/usr/bin/env bash
set -euo pipefail

# readywait-freeze-gate.sh — P2 unit E5 RT14 + RT19c freeze tripwire
# (plans/2026-07-21-p2-extraction/RT14-dispatchsegment-conversion.md §4 C7;
#  plans/2026-07-21-p2-extraction/RT19c-workingphase-watchdogs.md §6 step 3;
#  _plan.md §3.2).
#
# The open-coded agent_ready WAIT left internal/daemon in slice RT14. Every
# launch/ready/brief segment now runs on the runexec Dispatch machine via
# dispatchSegment (dispatchsegment.go) — a ClockPort-timed, FakeClock-drivable
# bound. depguard cannot express "do not re-hand-roll a wall-clock wait", so this
# grep ratchet closes that door.
#
# RT19c widened check (2) from the six dispatch-path files to the whole run path:
# the Working-phase watchdogs (pasteinject.go, dot_gate.go's
# pasteInjectQuitOnGateFile, waitsocketgrace.go, postreadyhang.go) now take a
# substrate.ClockPort too, so raw wall-clock is forbidden in them as well.
#
# The forbidden set is WALLCLOCK_RE below, and it is deliberately WIDER than the
# regex RT19c's own inventory used: that one had no Since/Until and so missed the
# two time.Since(loopStart) reads in pasteInjectQuitOnReviewFile that RT19c then
# had to convert anyway.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# WALLCLOCK_RE — the wall-clock entry points forbidden in the pinned files. ONE
# definition, used by checks (2) and (3), so the two can never drift apart.
#
# Since/Until are here because RT19c's own planning grep omitted them and so
# missed the two time.Since(loopStart) calls in pasteInjectQuitOnReviewFile — an
# elapsed-time read is exactly as clock-bound as a deadline, and mixing a wall
# elapsed with a virtual deadline is the failure mode this gate exists to stop.
# AfterFunc is here because time.After( does NOT match time.AfterFunc(.
#
# time.Now().Sub(x) — Since spelled the long way — needs no separate alternative:
# Now already covers it. Deliberately NOT policed: time.Duration(n)*time.Second
# (a unit conversion, no clock read, and dot_cascade.go legitimately has four),
# and context.WithTimeout/WithDeadline (wall-bound, but ctx deadlines are not yet
# on the ClockPort and dot_cascade.go already carries one — pinning it would make
# the gate red on arrival rather than ratchet anything).
#
# The match is plain text — COMMENTS COUNT. Stripping them would need a Go parser
# (or a sed hack that silently creates false negatives), and a gate that misses a
# real site is worse than one that occasionally objects to prose. So: in these ten
# files, describe a forbidden call without writing its literal call syntax
# ("a stdlib ticker panics on a non-positive interval", not the code).
WALLCLOCK_RE='time\.(After|AfterFunc|Now|NewTimer|NewTicker|Tick|Sleep|Since|Until)\('

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
#     dispatch path AND the Working-phase watchdogs must remain FakeClock-drivable
#     end to end.
#
#     agentready.go is NOT in this list because RT14 deleted the file outright.
#     RT19b-3 had already moved its four surviving policy scalars to
#     internal/runlaunch, so once waitAgentReady + agentEventSource went, nothing
#     was left. Check (1) is what keeps those symbols from coming back — pinning
#     a deleted path here would make the gate fail on its own landing commit.
#
#     The last four entries are RT19c's: pasteinject.go (the two sibling commit /
#     review-file watchdogs plus the three splash/backoff/submit stragglers),
#     dot_gate.go (pasteInjectQuitOnGateFile), waitsocketgrace.go (the stop-hook
#     grace) and postreadyhang.go (the post-agent_ready progress bound).
for f in internal/daemon/dispatchsegment.go internal/daemon/runshell.go \
         internal/daemon/runbridge.go internal/daemon/reviewloop.go \
         internal/daemon/dot_cascade.go internal/daemon/workloopeventsource.go \
         internal/daemon/pasteinject.go internal/daemon/dot_gate.go \
         internal/daemon/waitsocketgrace.go internal/daemon/postreadyhang.go; do
    if [ ! -f "$f" ]; then
        echo "readywait-freeze-gate: pinned file $f is gone — re-derive this gate's file list" >&2
        HITS=$((HITS + 1))
        continue
    fi
    N="$(grep -cE "$WALLCLOCK_RE" "$f" || true)"
    if [ "$N" -ne 0 ]; then
        echo "readywait-freeze-gate: FORBIDDEN raw wall-clock in $f ($N site(s)) — use substrate.After / the ClockPort:" >&2
        grep -nE "$WALLCLOCK_RE" "$f" >&2
        HITS=$((HITS + 1))
    fi
done

# (3) beadRunOne stays wall-clock clean. Anchored on its two boundary symbols so
#     the range survives the line drift that RT15 will cause.
START="$(grep -n '^func beadRunOne' internal/daemon/workloop.go | head -1 | cut -d: -f1)"
END="$(awk -v s="$START" 'NR>s && /^func /{print NR; exit}' internal/daemon/workloop.go)"
if [ -n "$START" ] && [ -n "$END" ]; then
    MATCHES="$(awk -v s="$START" -v e="$END" 'NR>=s && NR<=e' internal/daemon/workloop.go \
               | grep -nE "$WALLCLOCK_RE" || true)"
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
