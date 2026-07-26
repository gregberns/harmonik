#!/usr/bin/env bash
set -euo pipefail

# crewrun-freeze-gate.sh — P2 unit E2 freeze tripwire
# (plans/2026-07-21-p2-extraction/E2-crew.md §5b; _plan.md §3.2).
#
# The crew LAUNCH CONTRACT left internal/daemon in slice E2a: the crew-start /
# crew-stop RPC payloads, the persistent-session launch-spec builder, the
# crew-scoped harness resolver, the mission-handoff front-matter readers, and
# the idle-completed-crew reaper now live in internal/crewrun. depguard fences
# the IMPORT edge (internal/crewrun must not import internal/daemon) but it
# cannot forbid CREATING a file, so this grep ratchet closes the other door: it
# fails the build if a crew-launch-shaped file reappears in internal/daemon, or
# if one of the moved symbols is re-declared there.
#
# internal/daemon/crewstart.go is the ONE sanctioned residue: the tmux-native
# crew-start handler. It cannot move today because crewstart.go type-asserts
# h.substrate to substrateWithAdapter, whose only method tmuxAdapter() is
# UNEXPORTED and therefore package-qualified — no out-of-package type can
# satisfy it and no out-of-package code can perform the assertion. Moving it is
# slice E2b, which needs an operator waiver of the no-new-seam rule
# (E2-crew.md §2 RED FLAG). When E2b lands, drop crewstart.go from the
# sanctioned list below and the door closes completely.
#
# Test files legitimately STAY in internal/daemon and are carved out by the
# `! -name '*_test.go'` filter — they drive crewHandlerImpl, *tmuxSubstrate, or
# the orphan sweep, none of which moved:
#   - crewstart_*_test.go, scenario_captain_crew_e2e_hkzi4ej_test.go,
#     scenario_orphan_sweep_crew_pl006d_hkndq_test.go,
#     hknddg1_crewqueue_provenance_test.go
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No new crew-launch source file in the daemon package. crewstart.go is the
#     sanctioned E2b residue; the match is exact, not a prefix glob, because
#     three files in this package are named crew* but are not crew wiring
#     (E2-crew.md §7 R6).
while IFS= read -r f; do
    case "$f" in
        internal/daemon/crewstart.go) continue ;;
    esac
    echo "crewrun-freeze-gate: FORBIDDEN crew-launch file in internal/daemon: $f" >&2
    HITS=$((HITS + 1))
done < <(find internal/daemon -type f \
             \( -name 'crew*.go' -o -name '*crewlaunch*.go' -o -name '*crewidle*.go' \) \
             ! -name '*_test.go')

# (2) No re-declaration of the funcs/consts that moved to internal/crewrun.
for sym in JoinRemoteControlName buildCrewLaunchSpec BuildCrewLaunchSpec \
           resolveCrewHarness ResolveCrewHarness crewHarnessClaude \
           readMissionFrontMatter readMissionModel ReadMissionModel \
           readMissionHarness ReadMissionHarness frontMatterBlock \
           NewCrewIdleReaper; do
    MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E "^[[:space:]]*(func|var|const|type)?[[:space:]]*${sym}\b[[:space:]]*(=|struct|interface|func|\()" internal/daemon 2>/dev/null | grep -vE '=[[:space:]]*(shared|claude|codex|pi|crewrun|queuewiring|tunnel|codesync|gitprobe)\.' || true)"
    if [ -n "$MATCHES" ]; then
        echo "crewrun-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
done

# (3) No re-declaration of the types that moved to internal/crewrun.
for sym in CrewHandler CrewStartRequest CrewStopRequest CrewStartResult \
           CrewLaunchCtx crewLaunchCtx missionFrontMatter \
           CrewIdleReaper CrewIdleReaperConfig crewStopper crewQueueLookup; do
    MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E "^[[:space:]]*(type[[:space:]]+)?${sym}\b[[:space:]]*(=|struct|interface)" internal/daemon 2>/dev/null | grep -vE '=[[:space:]]*(shared|claude|codex|pi|crewrun|queuewiring|tunnel|codesync|gitprobe)\.' || true)"
    if [ -n "$MATCHES" ]; then
        echo "crewrun-freeze-gate: FORBIDDEN re-declaration of type ${sym} in internal/daemon:" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
done

if [ "$HITS" -ne 0 ]; then
    echo "crewrun-freeze-gate: FAIL — the crew launch contract was extracted in P2 E2a; build on internal/crewrun, do not reopen internal/daemon" >&2
    exit 1
fi
echo "crewrun-freeze-gate: OK — crew launch contract stays out of internal/daemon"
