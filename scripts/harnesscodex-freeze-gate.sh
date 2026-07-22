#!/usr/bin/env bash
set -euo pipefail

# harnesscodex-freeze-gate.sh — P2 unit E1a freeze tripwire
# (plans/2026-07-21-p2-extraction/E1a-codex-harness.md §5.2; _plan.md §3.2).
#
# The codex HARNESS IMPLEMENTATION left internal/daemon in slice E1a-1: the
# handlercontract.Harness impl, the launch-spec builder, the JSONL parser and
# thread-id interceptor, the stale-WAL guard, the ChatGPT billing guard, the
# post-exit Refs-trailer fallback and the no-work detector now live in
# internal/harness/codex. The cross-harness half (the Refs-trailer primitives,
# the resume seed prompt, the env-key splitter) lives one level below, in
# internal/harness/shared, so the pi harness can reach it without importing
# codex.
#
# depguard fences the IMPORT edge (internal/harness/codex must not import
# internal/daemon; internal/harness/shared must not import any concrete
# harness) but it cannot forbid CREATING a file. This grep ratchet closes the
# other door: it fails the build if a codex-harness-shaped file reappears in
# internal/daemon, or if one of the moved symbols is re-declared there.
#
# Test files legitimately STAY in internal/daemon and are carved out by the
# `! -name '*_test.go'` filter. Four are codex-NAMED but are daemon tests:
#   - codex_spawnproof_hk47u9z_test.go   stalewatch never-spawned reaper
#   - codex_daemon_commit_hkgd9r_test.go full RunWorkLoop integration oracle
#   - codex_empty_model_hkd170r_test.go  daemon-side harness ROUTING
#   - crossharness_empty_model_test.go   pi-vs-codex invariant in ONE table
# They exercise the extracted package through the registry, which is the point.
#
# newHarnessRegistry stays in internal/daemon on purpose: the daemon is the
# composition root. Registering codex is a daemon act; implementing codex is not.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No new codex-harness source file in the daemon package.
while IFS= read -r f; do
    echo "harnesscodex-freeze-gate: FORBIDDEN codex-harness file in internal/daemon: $f" >&2
    HITS=$((HITS + 1))
done < <(find internal/daemon -maxdepth 1 -type f \
             \( -name 'codex*.go' -o -name '*codexharness*.go' -o -name '*codexlaunch*.go' \) \
             ! -name '*_test.go')

# (2) No re-declaration of the funcs/consts/vars that moved to
#     internal/harness/codex. Both the pre-move daemon-private name and the
#     post-move exported name are listed: re-introducing either one in the
#     daemon is a re-implementation of the extracted concern.
for sym in NewCodexHarness NewHarness buildCodexLaunchSpec BuildLaunchSpec \
           buildCodexEnv resolveCodexHome codexSeedPromptTemplate \
           codexCredentialDenyKeys ensureCodexRefsTrailer EnsureRefsTrailer \
           commitAllWithRefsTrailer parseCodexJSONLEvent captureCodexThreadID \
           newCodexThreadIDInterceptor cleanCodexStaleWAL reapCodexWALBackupDirs \
           materializeForcedLoginMethod assertChatGPTPlan runCodexBillingGuard \
           emitCodexBillingGuard configDeclaresChatGPTLogin \
           codexNoWorkSuspected NoWorkSuspected codexNoWorkFloor NoWorkFloor \
           codexNoWorkDurationFloorDefault emitImplementerNoWorkSuspected \
           EmitImplementerNoWorkSuspected; do
    if grep -rn --include='*.go' -E "^(func|var|const|type)[[:space:]]+${sym}\b" internal/daemon >/dev/null 2>&1; then
        echo "harnesscodex-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        grep -rn --include='*.go' -E "^(func|var|const|type)[[:space:]]+${sym}\b" internal/daemon >&2
        HITS=$((HITS + 1))
    fi
done

# (3) No re-declaration of the types that moved to internal/harness/codex.
for sym in CodexHarness codexRunCtx RunCtx codexEventKind codexEvent \
           codexTokenUsage codexJSONLLine codexRunArtifacts \
           codexThreadIDInterceptor codexWALGuardConfig codexAuthFile \
           ErrMissingCodexStaleWALMaxBytes ErrMissingStaleWALMaxBytes; do
    if grep -rn --include='*.go' -E "^type[[:space:]]+${sym}[[:space:]]+(struct|interface|int)" internal/daemon >/dev/null 2>&1; then
        echo "harnesscodex-freeze-gate: FORBIDDEN re-declaration of type ${sym} in internal/daemon:" >&2
        grep -rn --include='*.go' -E "^type[[:space:]]+${sym}[[:space:]]+(struct|interface|int)" internal/daemon >&2
        HITS=$((HITS + 1))
    fi
done

# (4) internal/harness/shared must stay BELOW every harness impl. A codex import
#     there re-couples pi to codex through the leaf — the exact back-edge the
#     E1a-0 split removed. depguard also denies this; the duplicate is cheap and
#     it keeps the whole invariant readable in one file.
if grep -rn --include='*.go' 'gregberns/harmonik/internal/harness/codex' internal/harness/shared >/dev/null 2>&1; then
    echo "harnesscodex-freeze-gate: FORBIDDEN internal/harness/shared -> internal/harness/codex import:" >&2
    grep -rn --include='*.go' 'gregberns/harmonik/internal/harness/codex' internal/harness/shared >&2
    HITS=$((HITS + 1))
fi

if [ "$HITS" -ne 0 ]; then
    echo "harnesscodex-freeze-gate: FAIL — the codex harness was extracted in P2 E1a; build on internal/harness/codex, do not reopen internal/daemon" >&2
    exit 1
fi
echo "harnesscodex-freeze-gate: OK — the codex harness stays out of internal/daemon"
