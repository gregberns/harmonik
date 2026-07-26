#!/usr/bin/env bash
set -euo pipefail

# runlaunch-freeze-gate.sh — P2 unit E5 RT19b freeze tripwire
# (plans/2026-07-21-p2-extraction/RT19b-stranded-run-path-helpers.md §4 step 17;
#  _plan.md §3.2).
#
# Sixteen symbols that were stranded in internal/daemon/workloop.go and
# internal/daemon/agentready.go — the CHB-018 pre-exec relay, the HC-056
# readiness deadlines, the launch/ready anomaly events, the phase-complete
# report, the force-teardown backstop, the ClockPort After helper, the
# LaunchArtifacts agent-type accessor and the Refs-trailer main-history probe —
# now live in internal/runlaunch, internal/substrate and internal/harness/shared.
# depguard fences the IMPORT edge; it cannot forbid RE-DECLARING a symbol, so
# this ratchet closes the other door.
#
# DELIBERATE DEVIATION from the runmerge/transport gates: no broad file-name
# scan. internal/daemon legitimately retains ~25 other emit* functions
# (emitRunStarted, emitRunCompleted, emitPostAgentReadyHang, emitReviewerVerdict
# and friends) which are E5 LIFT targets, not RT19b targets. A '*events*.go'
# scan here would fire on correct code. Symbols are the precise contract.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No re-declaration of the moved funcs/vars/consts anywhere under
#     internal/daemon (recursive — a sub-package is the obvious evasion of a
#     -maxdepth 1 scan). The alternation also catches a re-declaration inside a
#     grouped `var ( … )` / `const ( … )` block, which a bare ^(func|var|const)
#     anchor misses. The trailing grep -v drops legitimate forwarding
#     assignments to the extracted packages (export_test.go shims).
for sym in clockAfter After \
           agentReadyKillReapTimeout KillReapTimeout \
           defaultAgentReadyTimeout DefaultAgentReadyTimeout \
           defaultRemoteAgentReadyTimeout DefaultRemoteAgentReadyTimeout \
           effectiveAgentReadyTimeout EffectiveAgentReadyTimeout \
           ErrAgentReadyTimeout \
           beadAlreadySubsumedInMain MainHistoryHasRefsTrailer \
           forceTeardownSession ForceTeardownSession \
           emitPreExecMessage EmitPreExecMessage \
           preExecMsgType \
           emitPreExecBeforeLaunch EmitPreExecBeforeLaunch \
           emitImplementerPhaseComplete EmitImplementerPhaseComplete \
           emitSpawnCapBlocked EmitSpawnCapBlocked \
           emitTmuxNewWindowTimeout EmitTmuxNewWindowTimeout \
           emitAgentReadyTimeout EmitAgentReadyTimeout \
           artifactAgentType ArtifactAgentType; do
    MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E \
        "^[[:space:]]*(func|var|const)?[[:space:]]*${sym}\b[[:space:]]*(=|struct|interface|func|\()" \
        internal/daemon 2>/dev/null \
      | grep -vE '=[[:space:]]*(shared|claude|codex|pi|crewrun|queuewiring|tunnel|codesync|gitprobe|runmerge|runlaunch|substrate)\.' || true)"
    if [ -n "$MATCHES" ]; then
        echo "runlaunch-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
done

# (2) Narrow file-name check — only names that cannot mean anything but the
#     extracted concern. Deliberately NOT '*events*.go' (see header).
while IFS= read -r f; do
    echo "runlaunch-freeze-gate: FORBIDDEN launch-effects file in internal/daemon: $f" >&2
    HITS=$((HITS + 1))
done < <(find internal/daemon -type f \
             \( -name '*preexec*.go' -o -name '*spawncap*.go' \
                -o -name '*agentreadytimeout*.go' -o -name '*forceteardown*.go' \
                -o -name '*clockafter*.go' -o -name '*runlaunch*.go' \) \
             ! -name '*_test.go')

# (3) The run path must not re-acquire a raw time.After for the ready/reap
#     bound. This check is ABSOLUTE: it covers every non-test file under
#     internal/daemon. RT19b-3 landed it with a named, temporary
#     --exclude='dot_gate.go' because the gate node's ready wait was the last
#     surviving wall-clock site on this bound and converting it there would have
#     been a logic change inside an extraction (_plan.md §5.1). RT14 phase B
#     converted that site to substrate.After(deps.clock, …), so the exclusion —
#     which was a blind spot, not a policy — is deleted.
MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E \
    'time\.After\((agentReadyKillReapTimeout|runlaunch\.KillReapTimeout)\)' internal/daemon 2>/dev/null || true)"
if [ -n "$MATCHES" ]; then
    echo "runlaunch-freeze-gate: FORBIDDEN wall-clock time.After on the reap bound — use substrate.After(clk, runlaunch.KillReapTimeout):" >&2
    printf '%s\n' "$MATCHES" >&2
    HITS=$((HITS + 1))
fi

# (4) The destinations must still exist. A gate whose targets have been renamed
#     away silently stops testing what it claims to test.
for f in internal/runlaunch/events.go internal/runlaunch/deadlines.go \
         internal/runlaunch/teardown.go internal/substrate/clock.go \
         internal/harness/shared/refstrailer.go internal/harness/shared/launchctx.go; do
    if [ ! -f "$f" ]; then
        echo "runlaunch-freeze-gate: destination $f is gone — re-derive this gate" >&2
        HITS=$((HITS + 1))
    fi
done

if [ "$HITS" -ne 0 ]; then
    echo "runlaunch-freeze-gate: FAIL — the run path's launch-time effects were extracted in P2 E5 RT19b; build on internal/runlaunch, do not reopen internal/daemon" >&2
    exit 1
fi
echo "runlaunch-freeze-gate: OK — the launch-time effects stay out of internal/daemon"
