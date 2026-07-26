#!/usr/bin/env bash
set -euo pipefail

# harnessclaude-freeze-gate.sh — P2 unit E1b freeze tripwire
# (plans/2026-07-21-p2-extraction/E1b-claude.md §5.1; _plan.md §3.2).
#
# The claude HARNESS IMPLEMENTATION left internal/daemon in unit E1b: the
# handlercontract.Harness impl (claudeharness.go) and the launch-spec builder
# that threads the claude-hook-bridge pieces together (claudelaunchspec.go) now
# live in internal/harness/claude. The launch DTO they share with codex and pi
# (LaunchCtx / LaunchArtifacts) and the model/effort validators left one level
# below in E1b-prep, to internal/harness/shared, so no harness impl has to
# import a sibling to reach them.
#
# depguard fences the IMPORT edge (internal/harness/claude must not import
# internal/daemon, nor a sibling impl) but it cannot forbid CREATING a file.
# This grep ratchet closes the other door: it fails the build if a
# claude-harness-shaped file reappears in internal/daemon, or if one of the
# moved symbols is re-declared there.
#
# THREE claude-NAMED daemon files legitimately STAY and are carved out by name
# below. They are not the harness; the shared name is a historical accident:
#   - claudeheartbeat.go       the run loop's HARNESS-BLIND agent_heartbeat
#                              emitter. Called on codex and pi runs too
#                              (dot_gate.go's cognition gate routes all three),
#                              so moving it would create a daemon ->
#                              harness/claude edge on every NON-claude run.
#                              Follow-up: rename to runheartbeatemitter.go.
#   - claudeworktreesweep.go   a boot-time janitor for .claude/worktrees/agent-*
#                              dirs left by the INTERACTIVE Claude Code CLI.
#                              Sole caller is RunOrphanSweep; not reachable from
#                              handlercontract.HarnessRegistry, so the plan's
#                              §3.4 boundary test excludes it.
#   - pasteinject.go           the real claude Seed/Retask/Teardown REPL driver.
#                              Called DIRECTLY by workloop/dot_cascade/dot_gate/
#                              reviewloop rather than through the Harness seam,
#                              and interleaved with harness-blind run-loop
#                              machinery. Belongs to P2 unit E5.
#
# Test files legitimately STAY in internal/daemon and are carved out by the
# `! -name '*_test.go'` filter — they exercise the extracted package through the
# registry (routed-vs-direct spec parity, harness pinning, review-loop resume),
# which is the point.
#
# newHarnessRegistry stays in internal/daemon on purpose: the daemon is the
# composition root. Registering claude is a daemon act; implementing claude is not.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No new claude-harness source file in the daemon package. The three
#     standing exceptions above are excluded by exact name.
while IFS= read -r f; do
    echo "harnessclaude-freeze-gate: FORBIDDEN claude-harness file in internal/daemon: $f" >&2
    HITS=$((HITS + 1))
done < <(find internal/daemon -type f \
             \( -name 'claude*.go' -o -name '*claudeharness*.go' -o -name '*claudelaunch*.go' \) \
             ! -name '*_test.go' \
             ! -name 'claudeheartbeat.go' \
             ! -name 'claudeworktreesweep.go')

# (2) No re-declaration of the funcs/vars that moved to
#     internal/harness/claude. Both the pre-move daemon-private name and the
#     post-move exported name are listed: re-introducing either one in the
#     daemon is a re-implementation of the extracted concern.
for sym in NewClaudeHarness NewHarness buildClaudeLaunchSpec BuildLaunchSpec \
           isHarmonikManagedWorktree; do
    MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E "^[[:space:]]*(func|var|const|type)?[[:space:]]*${sym}\b[[:space:]]*(=|struct|interface|func|\()" internal/daemon 2>/dev/null | grep -vE '=[[:space:]]*(shared|claude|codex|pi|crewrun|queuewiring|tunnel|codesync|gitprobe)\.' || true)"
    if [ -n "$MATCHES" ]; then
        echo "harnessclaude-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
done

# (3) No re-declaration of the types that moved to internal/harness/claude, or
#     of the launch DTO that moved to internal/harness/shared in E1b-prep.
for sym in ClaudeHarness Harness claudeRunCtx claudeRunArtifacts \
           LaunchCtx LaunchArtifacts ModelPreferenceError; do
    MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E "^[[:space:]]*(type[[:space:]]+)?${sym}\b[[:space:]]*(=|struct|interface)" internal/daemon 2>/dev/null | grep -vE '=[[:space:]]*(shared|claude|codex|pi|crewrun|queuewiring|tunnel|codesync|gitprobe)\.' || true)"
    if [ -n "$MATCHES" ]; then
        echo "harnessclaude-freeze-gate: FORBIDDEN re-declaration of type ${sym} in internal/daemon:" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
done

# (4) internal/harness/shared must stay BELOW every harness impl. A claude
#     import there would turn the leaf into a hub and re-couple codex and pi to
#     claude through the back door. depguard also denies this; the duplicate is
#     cheap and it keeps the whole invariant readable in one file.
if grep -rn --include='*.go' 'gregberns/harmonik/internal/harness/claude' internal/harness/shared >/dev/null 2>&1; then
    echo "harnessclaude-freeze-gate: FORBIDDEN internal/harness/shared -> internal/harness/claude import:" >&2
    grep -rn --include='*.go' 'gregberns/harmonik/internal/harness/claude' internal/harness/shared >&2
    HITS=$((HITS + 1))
fi

# (5) No sibling-impl edge: harness/claude must not reach into harness/codex or
#     harness/pi (and vice versa). Cross-harness helpers belong in
#     internal/harness/shared — a sibling edge is a daemon back-edge in disguise
#     (_plan.md §6).
for sib in codex pi; do
    if grep -rn --include='*.go' "gregberns/harmonik/internal/harness/${sib}" internal/harness/claude >/dev/null 2>&1; then
        echo "harnessclaude-freeze-gate: FORBIDDEN internal/harness/claude -> internal/harness/${sib} import:" >&2
        grep -rn --include='*.go' "gregberns/harmonik/internal/harness/${sib}" internal/harness/claude >&2
        HITS=$((HITS + 1))
    fi
done

if [ "$HITS" -ne 0 ]; then
    echo "harnessclaude-freeze-gate: FAIL — the claude harness was extracted in P2 E1b; build on internal/harness/claude, do not reopen internal/daemon" >&2
    exit 1
fi
echo "harnessclaude-freeze-gate: OK — the claude harness stays out of internal/daemon"
