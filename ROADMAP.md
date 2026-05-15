# Harmonik Roadmap

_Last updated: 2026-05-15. High-level epic order from current state to a fully operational harmonik that orchestrators can drive at scale._

## Current state

Phase 1 reached OPERATIONAL GREEN on 2026-05-14: harmonik dispatches a bead to a real Claude Code subprocess, Claude commits the work, the daemon detects completion and closes the bead — zero human input. Phase 2 entry (first-demo round-trip: daemon merges run-branch to main and closes the bead via harmonik rather than a sub-agent) reached GREEN on 2026-05-15 (`hk-09tne`). The extqueue v0.1 spec package is on branch `extqueue-v0.1-specs` awaiting merge; early implementation tasks (T10–T20) have already landed on main.

## Roadmap

| # | Status | Epic / Phase | Bead ID(s) | Description |
|---|--------|--------------|------------|-------------|
| 1 | DONE | Phase 1: smoke loop end-to-end | (closed) | Harmonik runs claude on one bead, zero human input — 24 s wall-clock. Achieved 2026-05-14. |
| 2 | DONE | Phase 2 entry: first-demo round-trip | hk-09tne (closed) | Daemon merges run-branch to main and closes the bead autonomously. Achieved 2026-05-15. |
| 3 | DONE | Post-MVH parallelism | hk-e61c3 (closed) | `--max-concurrent N` validated in t8/t11 throughput tests; capacity gate confirmed. |
| 4 | IN PROGRESS | extqueue v0.1 implementation | hk-lj0pb (epic) + 22 task beads | External orchestrator submits an ordered wave queue; daemon executes it, decoupling bead selection from daemon internals. |
| 5 | OPEN | Bridge completeness + session lifecycle | hk-lj1p9, hk-gql20, hk-kqdpf (P0 epics) | Close remaining gaps: tmux substrate wiring, dual-path collapse, review-loop task injection, worktree auto-trust (hk-fdyip P0). |
| 6 | OPEN | imrest — bead in_progress as activity marker | hk-iuaed | Spec + impl: decouple `br claim` (activity marker, recoverable) from close/reopen (truth claim); add orphan-reset sweep on restart. |
| 7 | OPEN | CHB spec corpus implementation | hk-qo08q | Implement `specs/claude-hook-bridge.md` req-beads (CHB-NNN) including hook-relay subcommand, socket acceptor, and durable checkpoint. |
| 8 | TESTING (gated) | Phase 2 multi-bead E2E smoke | hk-1n0cw | Run a 5–10 bead queue through harmonik at `--max-concurrent N`; observe completion stream and merge dance. |
| 9 | NEXT | Remaining spec-corpus implementation | hk-b3f, hk-hqwn, hk-8i31, hk-a8bg, hk-8mwo, hk-8mup, hk-sx9r, hk-63oh | Implement the 8 spec epics (EM, EV, HC, CP, WM, PL, ON, RC) that are open but not yet in the active critical path. |
| 10 | NEXT | Scenario Harness implementation | hk-i0tw | Implement `specs/scenario-harness.md` — the structured test surface that lets harmonik validate its own workflows declaratively. |
| 11 | FUTURE | Phase 3: DOT-defined bead processes | (not yet filed) | Replace hard-coded workloop/reviewloop with composable node graphs defined in DOT format; each node is a phase, edges encode verdict fan-out. |
