// Package claude is the Claude Code implementation of the
// handlercontract.Harness seam: the launch-spec builder that threads together
// every claude-hook-bridge piece (session-id mint/resume, transcript path,
// .claude/settings.json materialization, worktree trust, agent-task.md write,
// the CHB-006 env set, argv construction and the CHB-007 deny-list guard, the
// CHB-018 pre-exec messages) and the Harness adapter that wraps it.
//
// The package sits BELOW the daemon and never imports it. That direction is the
// whole point: a P3 container must be able to link this harness without
// dragging the daemon monolith in with it. The daemon remains the composition
// root — newHarnessRegistry (internal/daemon/harnessregistry.go) constructs a
// claude.Harness and registers it under core.AgentTypeClaudeCode, and the
// routed launch path, the review loop and the DOT cascade call
// claude.BuildLaunchSpec. Nothing here calls back. depguard enforces the edge
// (.golangci.yml, rule "harness-claude"); the harnessclaude-freeze-gate.sh
// ratchet enforces that the concern does not reappear in internal/daemon under
// a new filename.
//
// Unlike codex and pi, claude does NOT detach entirely through
// handlercontract.Harness. routedLaunchSpecBuilder short-circuits past the seam
// for claude (harnessregistry.go) because handlercontract.SpawnSpec cannot
// carry the post-launch artifacts — claudeSessionID, sessionLogPath,
// preExecMsgs — that the run loop consumes. So the effective seam this package
// exits behind is the pre-existing LaunchPort.BuildSpec (internal/daemon/
// runports.go), whose DTO now lives in internal/harness/shared as LaunchCtx /
// LaunchArtifacts. That DTO is shared, not claude-owned: buildCodexRoutedLaunchSpec
// feeds the same struct to codex and pi. Relocating it (P2 unit E1b-prep) was a
// move, not a new port; widening SpawnSpec WOULD have been a new seam, which the
// extraction plan forbids.
//
// What deliberately did NOT come along:
//
//   - internal/daemon/pasteinject.go — the real claude Seed/Retask/Teardown
//     REPL driver. workloop/dot_cascade/dot_gate/reviewloop call it DIRECTLY,
//     not through the Harness seam, and it is interleaved with harness-blind
//     run-loop machinery (commit polling, heartbeat staleness, the reviewer
//     budget sentinel). It belongs to P2 unit E5, or to its own slice once the
//     Harness seam is widened.
//   - internal/daemon/claudeheartbeat.go — misnamed. Its only symbol emits the
//     run loop's harness-blind agent_heartbeat and is called on codex and pi
//     runs too (dot_gate.go's cognition gate routes all three). Moving it would
//     create a daemon -> harness/claude edge on every NON-claude run.
//   - internal/daemon/claudeworktreesweep.go — a boot-time janitor for
//     .claude/worktrees/agent-* dirs left behind by the interactive Claude Code
//     CLI. Sole caller is RunOrphanSweep; it is not reachable from
//     handlercontract.HarnessRegistry, so the plan's boundary test excludes it.
//
// Origin: internal/daemon/claudeharness.go and claudelaunchspec.go, relocated
// wholesale by P2 unit E1b (plans/2026-07-21-p2-extraction/E1b-claude.md §1).
// The move was pure: error strings, log output and behaviour are byte-identical
// to the daemon-side originals, including the ones that still say "daemon:".
// Rewording observable output is a follow-up, not part of a relocation.
//
// Spec: specs/claude-hook-bridge.md §4.2 CHB-006..009, §4.7 CHB-018..019, §4.9
// CHB-024; specs/handler-contract.md §4.2 HC-055, §4.10 HC-045a / HC-055a/b;
// specs/harness-contract.md §2.
package claude
