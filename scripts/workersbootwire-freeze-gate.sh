#!/usr/bin/env bash
set -euo pipefail

# workersbootwire-freeze-gate.sh — P2 unit E4c freeze tripwire
# (plans/2026-07-21-p2-extraction/E4-ssh.md §4 slice E4c; _plan.md §3.2).
#
# The remote-worker registry BOOT WIRING left internal/daemon in E4c: turning a
# loaded workers.Config into a live *workers.Registry (BuildRegistry), the
# runner-injectable core that runs the B6 boot health check
# (BuildRegistryWithRunner), and the transport resolver that picks the probe
# runner (BootHealthRunner) now live in internal/workers/bootwire.go.
#
# depguard fences the IMPORT edge (internal/workers must not import
# internal/daemon) but it cannot forbid CREATING a file, so this grep ratchet
# closes the other door: it fails the build if a worker-registry-construction
# file reappears in internal/daemon, or if one of the moved symbols is
# re-declared there.
#
# NAMED CARVE-OUTS — real, non-boot-wiring uses of a matching name:
#
#   internal/daemon/runports.go, workloop.go and friends legitimately HOLD a
#       *workers.Registry and CALL workers.BuildRegistry. Holding and calling is
#       the point; only re-DECLARING the construction here is forbidden, so this
#       gate matches declarations, not references.
#   *_test.go — daemon tests that drive the whole work loop still construct
#       registries through the workers package; they are not a re-declaration.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No worker-registry-construction source file in the daemon package
#     (recursive — a sub-package is the obvious evasion of a -maxdepth 1 scan).
while IFS= read -r f; do
    echo "workersbootwire-freeze-gate: FORBIDDEN worker-registry boot-wiring file in internal/daemon: $f" >&2
    HITS=$((HITS + 1))
done < <(find internal/daemon -type f \
             \( -name '*workerregistry*.go' -o -name '*workerbootwire*.go' \
                -o -name '*bootwire*.go' -o -name '*boothealth*.go' \))

# (2) No re-declaration of the funcs that moved to internal/workers. The
#     alternation also catches a re-declaration inside a grouped `var ( … )` /
#     `const ( … )` block, which a bare ^(func|var|const) anchor misses. The
#     trailing filter drops delegations of the form `x = workers.BuildRegistry`,
#     which are calls into the extracted package, not a second copy of it.
for sym in buildWorkerRegistry BuildRegistry buildWorkerRegistryWithRunner BuildRegistryWithRunner \
           bootHealthRunner BootHealthRunner; do
    MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E "^[[:space:]]*(func|var|const)?[[:space:]]*${sym}\b[[:space:]]*(=|struct|interface|func|\()" internal/daemon 2>/dev/null | grep -vE '=[[:space:]]*workers\.' || true)"
    if [ -n "$MATCHES" ]; then
        echo "workersbootwire-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
done

# (3) The daemon must not re-acquire its own workers.NewRegistry construction
#     path. Building the registry is the extracted concern; the daemon calls
#     workers.BuildRegistry and holds the result.
MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -F 'workers.NewRegistry(' internal/daemon 2>/dev/null || true)"
if [ -n "$MATCHES" ]; then
    echo "workersbootwire-freeze-gate: FORBIDDEN direct workers.NewRegistry construction in internal/daemon — call workers.BuildRegistry:" >&2
    printf '%s\n' "$MATCHES" >&2
    HITS=$((HITS + 1))
fi

# (4) The destination must still exist. A gate whose target has been renamed
#     away silently stops testing what it claims to test.
if [ ! -f internal/workers/bootwire.go ]; then
    echo "workersbootwire-freeze-gate: internal/workers/bootwire.go is gone — re-derive this gate" >&2
    HITS=$((HITS + 1))
fi

if [ "$HITS" -ne 0 ]; then
    echo "workersbootwire-freeze-gate: FAIL — the worker-registry boot wiring was extracted in P2 E4c; build on internal/workers, do not reopen internal/daemon" >&2
    exit 1
fi
echo "workersbootwire-freeze-gate: OK — the worker-registry boot wiring stays out of internal/daemon"
