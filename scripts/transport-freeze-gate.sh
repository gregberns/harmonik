#!/usr/bin/env bash
set -euo pipefail

# transport-freeze-gate.sh — P2 unit E4 freeze tripwire
# (plans/2026-07-21-p2-extraction/E4-ssh.md §5b; _plan.md §3.2).
#
# The remote-execution transport concern LEFT internal/daemon in slices E4a and
# E4b: `ssh -N -R` reverse tunnelling plus its readiness gate now live in
# internal/transport/tunnel, and the DD1 worker<->box-A git sync lives in
# internal/transport/codesync. depguard fences the IMPORT edge
# (internal/transport must not import internal/daemon) but it cannot forbid
# CREATING a file, so this grep ratchet closes the other door: it fails the build
# if a new transport-shaped file appears in internal/daemon, or if one of the
# moved symbols is re-declared there. Extracted concern = closed door: P3 and
# everyone else builds on internal/transport/{tunnel,codesync}.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No new transport-named source file in the daemon package.
while IFS= read -r f; do
    echo "transport-freeze-gate: FORBIDDEN transport file in internal/daemon: $f" >&2
    HITS=$((HITS + 1))
done < <(find internal/daemon -type f \
             \( -name '*reversetunnel*.go' -o -name '*revtunnel*.go' \
                -o -name '*codesync*.go' \))

# (2) No re-declaration of the symbols that moved to internal/transport/{tunnel,codesync}.
for sym in buildReverseTunnelArgs allocateReverseTunnelPort releaseReverseTunnelPort \
           waitWorkerSocketLive ensureWorkerHarmonikDir resolveAgentDaemonSocket \
           workerTCPEndpoint sshHostOpts workerHarmonikPath reverseTunnelRunner \
           fetchRunBranchBoxA ensureBaseOnWorker fetchBaseOnWorker pushBaseToWorker \
           workerSSHURL; do
    MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E "^[[:space:]]*(func|var|const|type)?[[:space:]]*${sym}\b[[:space:]]*(=|struct|interface|func|\()" internal/daemon 2>/dev/null | grep -vE '=[[:space:]]*(shared|claude|codex|pi|crewrun|queuewiring|tunnel|codesync|gitprobe)\.' || true)"
    if [ -n "$MATCHES" ]; then
        echo "transport-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
done

if [ "$HITS" -ne 0 ]; then
    echo "transport-freeze-gate: FAIL — the ssh/transport concern was extracted in P2 E4a/E4b; build on internal/transport/{tunnel,codesync}, do not reopen internal/daemon" >&2
    exit 1
fi
echo "transport-freeze-gate: OK — transport concern stays out of internal/daemon"
