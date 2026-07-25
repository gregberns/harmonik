#!/usr/bin/env bash
set -euo pipefail

# runloop-freeze-gate.sh — P2 LIFT extraction ratchet (chunk L0 and onward).
#
# The run machine's boundary contract — the narrow structural PORT interfaces
# (LedgerPort … RunRegistryPort) and the value/handle BUNDLES (RunPorts, RunEnv,
# SharedHandles) — left internal/daemon for internal/runloop (P2 LIFT chunk L0).
# depguard fences the IMPORT edge (internal/runloop must not import
# internal/daemon), but it cannot forbid RE-DECLARING one of the moved symbols
# back inside internal/daemon. This grep ratchet closes that door: it fails the
# build if any moved type symbol is re-declared in internal/daemon, or if the
# destination internal/runloop/ports.go disappears.
#
# RATCHETED design: L0 lifts only the shared port SURFACE. Chunks L1–L9 move the
# run-path FILES (beadRunOne, reviewloop, the DOT cascade, …) onto that surface.
# As each chunk lands, APPEND its moved filenames / symbols to the scans below so
# the door it opened cannot be reopened either.
#
# The daemon legitimately KEEPS the concrete adapters (daemonLedger, daemonMerge,
# daemonGate, daemonBudget, daemonWorktree, daemonLaunch, daemonRunRegistry) and
# the (*workLoopDeps) constructors (runPorts/runEnv/sharedHandles) — those are the
# daemon → runloop half of the boundary and are the CORRECT direction. So the
# scan targets the moved TYPE names only, and the `= runloop.` / alias forwarding
# arm below drops the legitimate daemon-local aliases (beadLedger, hookStoreIface)
# and any future forwarding decls.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No re-declaration of the moved bundle/port types anywhere under
#     internal/daemon (recursive — a sub-package is the obvious evasion of a
#     -maxdepth 1 scan). The alternation also catches a re-declaration inside a
#     grouped `type ( … )` / `var ( … )` / `const ( … )` block, which a bare
#     ^(type|var|const) anchor misses, and — via the `=` arm — a re-aliasing
#     dodge. The trailing grep -v drops the legitimate `= runloop.` forwarding
#     (e.g. `type beadLedger = runloop.BeadLedger`) and local re-assignments that
#     call a sibling leaf (`x = runmerge.Foo()`).
for sym in RunPorts RunEnv SharedHandles \
           LedgerPort EmitterPort WorktreePort MergePort LaunchPort \
           GatePort BudgetPort RunRegistryPort RunHandlePort PerRunEventTap; do
    MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E \
        "^[[:space:]]*(type[[:space:]]+|var[[:space:]]+|const[[:space:]]+)?${sym}\b[[:space:]]*(=|struct|interface|func|\()" \
        internal/daemon 2>/dev/null \
      | grep -vE '=[[:space:]]*(runloop|shared|claude|codex|pi|crewrun|queuewiring|tunnel|codesync|gitprobe|runmerge|runlaunch|substrate|projectconfig)\.' || true)"
    if [ -n "$MATCHES" ]; then
        echo "runloop-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
done

# The L1 constructor must not be recreated under its former daemon-local name.
MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E \
    '^[[:space:]]*func[[:space:]]+newPerRunEventTap\b' \
    internal/daemon 2>/dev/null || true)"
if [ -n "$MATCHES" ]; then
    echo "runloop-freeze-gate: FORBIDDEN re-declaration of newPerRunEventTap in internal/daemon:" >&2
    printf '%s\n' "$MATCHES" >&2
    HITS=$((HITS + 1))
fi

# (2) The destination must still exist. A gate whose target has been renamed away
#     silently stops testing what it claims to test. (Later chunks append their
#     moved run-path files here.)
for f in internal/runloop/ports.go internal/runloop/workloopeventsource.go; do
    if [ ! -f "$f" ]; then
        echo "runloop-freeze-gate: destination $f is gone — re-derive this gate" >&2
        HITS=$((HITS + 1))
    fi
done

if [ -f internal/daemon/workloopeventsource.go ]; then
    echo "runloop-freeze-gate: forbidden source file internal/daemon/workloopeventsource.go was recreated" >&2
    HITS=$((HITS + 1))
fi

if [ "$HITS" -ne 0 ]; then
    echo "runloop-freeze-gate: FAIL — the run machine's ports were extracted in P2 LIFT L0; build on internal/runloop, do not reopen internal/daemon" >&2
    exit 1
fi
echo "runloop-freeze-gate: OK — the run-path ports stay out of internal/daemon"
