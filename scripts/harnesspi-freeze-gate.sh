#!/usr/bin/env bash
set -euo pipefail

# harnesspi-freeze-gate.sh — P2 unit E1c freeze tripwire
# (plans/2026-07-21-p2-extraction/E1c-pi.md §5.2; _plan.md §3.2).
#
# The pi HARNESS IMPLEMENTATION left internal/daemon in unit E1c: the
# handlercontract.Harness impl (piharness.go), the argv/env launch-spec builder
# (pilaunchspec.go), the NDJSON parser and session-id/agent_end interceptor
# (pijsonlparser.go), the fail-closed PI-040 billing guard (pibillingguard.go)
# and the PI-030/031 Refs:<bead> trailer fallback (picommit.go) now live in
# internal/harness/pi. The commit primitives pi shares with codex left one level
# below in E1a-0, to internal/harness/shared, so no harness impl has to import a
# sibling to reach them.
#
# depguard fences the IMPORT edge (internal/harness/pi must not import
# internal/daemon, nor a sibling impl) but it cannot forbid CREATING a file.
# This grep ratchet closes the other door: it fails the build if a
# pi-harness-shaped file reappears in internal/daemon, or if one of the moved
# symbols is re-declared there.
#
# ONE pi-NAMED daemon file legitimately STAYS and is carved out BY NAME below —
# not by glob, because a glob would let a genuine harness file slip back in
# under a similar name:
#   - pi_profile_resolve.go    CLAIM-TIME daemon wiring, not harness impl. It
#                              consumes the daemon's projectconfig types
#                              (PiHarnessConfig / PiProfileConfig), emits bead
#                              label-conflict events, and is called from three
#                              places in workloop.go. It decides WHICH pi profile
#                              a bead gets; that is a daemon decision. Moving it
#                              would drag the whole projectconfig type-family out
#                              of the daemon.
#
# Test files legitimately STAY in internal/daemon and are carved out by the
# `! -name '*_test.go'` filter — they exercise the extracted package through the
# registry (registry assembly, routed launch-spec parity, project config, DOT
# work-loop e2e), which is the point.
#
# newHarnessRegistry stays in internal/daemon on purpose: the daemon is the
# composition root. Registering pi is a daemon act; implementing pi is not.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No new pi-harness source file in the daemon package. The one standing
#     exception above is excluded by exact name.
while IFS= read -r f; do
    echo "harnesspi-freeze-gate: FORBIDDEN pi-harness file in internal/daemon: $f" >&2
    HITS=$((HITS + 1))
done < <(find internal/daemon -maxdepth 1 -type f \
             \( -name 'pi*.go' -o -name '*piharness*.go' -o -name '*pilaunch*.go' \) \
             ! -name '*_test.go' \
             ! -name 'pi_profile_resolve.go')

# (2) No re-declaration of the funcs/vars that moved to internal/harness/pi.
#     Both the pre-move daemon-private name and the post-move exported name are
#     listed: re-introducing either one in the daemon is a re-implementation of
#     the extracted concern.
for sym in NewPiHarness buildPiLaunchSpec buildPiEnv buildPiModelsJSON \
           resolvePiAPIKeyValue ensurePiRefsTrailer commitAllWithPiRefsTrailer \
           parsePiNDJSONEvent capturePiUsage newPiSessionIDInterceptor \
           runPiBillingGuard emitPiBillingGuard piDefaultHome \
           piAuthIndicatesPersistentCredential isPiAPIKeyPattern \
           piProviderCredentialKeys piSeedPromptTemplate; do
    if grep -rn --include='*.go' -E "^(func|var|const)[[:space:]]+${sym}\b" internal/daemon >/dev/null 2>&1; then
        echo "harnesspi-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        grep -rn --include='*.go' -E "^(func|var|const)[[:space:]]+${sym}\b" internal/daemon >&2
        HITS=$((HITS + 1))
    fi
done

# (3) No re-declaration of the types that moved to internal/harness/pi.
#     PiHarnessConfig / PiProfileConfig are NOT listed: those are the daemon's
#     own projectconfig types and stay.
for sym in PiHarness piRunCtx piRunArtifacts piEvent piTokenUsage \
           piSessionIDInterceptor piNDJSONLine piAuthFile; do
    if grep -rn --include='*.go' -E "^type[[:space:]]+${sym}[[:space:]]+(struct|interface)" internal/daemon >/dev/null 2>&1; then
        echo "harnesspi-freeze-gate: FORBIDDEN re-declaration of type ${sym} in internal/daemon:" >&2
        grep -rn --include='*.go' -E "^type[[:space:]]+${sym}[[:space:]]+(struct|interface)" internal/daemon >&2
        HITS=$((HITS + 1))
    fi
done

# (4) internal/harness/shared must stay BELOW every harness impl. A pi import
#     there would turn the leaf into a hub and re-couple codex and claude to pi
#     through the back door. depguard also denies this; the duplicate is cheap
#     and it keeps the whole invariant readable in one file.
if grep -rn --include='*.go' 'gregberns/harmonik/internal/harness/pi' internal/harness/shared >/dev/null 2>&1; then
    echo "harnesspi-freeze-gate: FORBIDDEN internal/harness/shared -> internal/harness/pi import:" >&2
    grep -rn --include='*.go' 'gregberns/harmonik/internal/harness/pi' internal/harness/shared >&2
    HITS=$((HITS + 1))
fi

# (5) No sibling-impl edge: harness/pi must not reach into harness/codex or
#     harness/claude. This is the one E1c cares about most — five of pi's six
#     outbound symbols used to live in codexcommit.go, so pi -> codex is the
#     edge that would form by default. Cross-harness helpers belong in
#     internal/harness/shared; a sibling edge is a daemon back-edge in disguise
#     (_plan.md §6).
for sib in codex claude; do
    if grep -rn --include='*.go' "gregberns/harmonik/internal/harness/${sib}" internal/harness/pi >/dev/null 2>&1; then
        echo "harnesspi-freeze-gate: FORBIDDEN internal/harness/pi -> internal/harness/${sib} import:" >&2
        grep -rn --include='*.go' "gregberns/harmonik/internal/harness/${sib}" internal/harness/pi >&2
        HITS=$((HITS + 1))
    fi
done

# (6) The live-pi oracle must not go silently vacuous. `go test -run <re>` exits
#     0 when the filter matches nothing, so if the Makefile's test-pi-live
#     target still points at ./internal/daemon/... after the move it passes
#     while running ZERO tests. Assert the target names the package that now
#     owns TestPiA_LiveSingleTurn.
if grep -qE '^\s*PI_LIVE=1 go test .*-run TestPiA_ \./internal/daemon' Makefile; then
    echo "harnesspi-freeze-gate: FORBIDDEN — Makefile test-pi-live still targets ./internal/daemon/...;" >&2
    echo "  TestPiA_LiveSingleTurn moved to internal/harness/pi in P2 E1c, so that target matches zero" >&2
    echo "  tests and exits 0 — a silent green on the only real-pi oracle." >&2
    HITS=$((HITS + 1))
fi

if [ "$HITS" -ne 0 ]; then
    echo "harnesspi-freeze-gate: FAIL — the pi harness was extracted in P2 E1c; build on internal/harness/pi, do not reopen internal/daemon" >&2
    exit 1
fi
echo "harnesspi-freeze-gate: OK — the pi harness stays out of internal/daemon"
