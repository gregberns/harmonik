#!/usr/bin/env bash
set -euo pipefail

# transport-freeze-gate.sh — P2 unit E4 freeze tripwire
# (plans/2026-07-21-p2-extraction/E4-ssh.md §5b; _plan.md §3.2).
#
# The reverse-tunnel half of the remote-execution transport concern LEFT
# internal/daemon in slice E4a: `ssh -N -R` reverse tunnelling plus its readiness
# gate now live in internal/transport/tunnel. depguard fences the IMPORT edge
# (internal/transport must not import internal/daemon) but it cannot forbid
# CREATING a file, so this grep ratchet closes the other door: it fails the build
# if a new reverse-tunnel-shaped file appears in internal/daemon, or if one of the
# moved symbols is re-declared there. Extracted concern = closed door: P3 and
# everyone else builds on internal/transport/tunnel.
#
# NOT YET COVERED: the DD1 worker<->box-A git sync (internal/daemon/codesync_rs_b8.go)
# is slice E4b and is still in internal/daemon. When E4b lands, add
# '*codesync*.go' to the file scan and fetchRunBranchBoxA / ensureBaseOnWorker /
# fetchBaseOnWorker / pushBaseToWorker to the symbol list.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No new reverse-tunnel-named source file in the daemon package.
while IFS= read -r f; do
    echo "transport-freeze-gate: FORBIDDEN transport file in internal/daemon: $f" >&2
    HITS=$((HITS + 1))
done < <(find internal/daemon -maxdepth 1 -type f \
             \( -name '*reversetunnel*.go' -o -name '*revtunnel*.go' \))

# (2) No re-declaration of the symbols that moved to internal/transport/tunnel.
for sym in buildReverseTunnelArgs allocateReverseTunnelPort releaseReverseTunnelPort \
           waitWorkerSocketLive ensureWorkerHarmonikDir resolveAgentDaemonSocket \
           workerTCPEndpoint sshHostOpts workerHarmonikPath reverseTunnelRunner; do
    if grep -rn --include='*.go' -E "^(func|var|const)[[:space:]]+${sym}\b" internal/daemon >/dev/null 2>&1; then
        echo "transport-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        grep -rn --include='*.go' -E "^(func|var|const)[[:space:]]+${sym}\b" internal/daemon >&2
        HITS=$((HITS + 1))
    fi
done

if [ "$HITS" -ne 0 ]; then
    echo "transport-freeze-gate: FAIL — the reverse-tunnel concern was extracted in P2 E4a; build on internal/transport/tunnel, do not reopen internal/daemon" >&2
    exit 1
fi
echo "transport-freeze-gate: OK — reverse-tunnel concern stays out of internal/daemon"
