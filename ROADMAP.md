<!-- TIER: 4 (operational state, weeks–months / long cadence)
     LOADED BY: captain @ cold boot / milestone only (NOT every restart); implementer-orchestrator on demand
     OWNER: orchestrator, updated at milestone boundaries
     DO NOT PUT HERE: live lane/crew registry + dated directives (→ captain-lanes.md tier-2);
                      this-session state (→ HANDOFF.md tier-1) -->

# Harmonik Roadmap

> High-level epic roadmap; for live lane/initiative tracking see [captain-lanes.md](.harmonik/context/captain-lanes.md) (tier-2) and STATUS.md.

## We are HERE (2026-06-20)

Phase 1 + Phase 2-entry are GREEN. The persistent daemon, Captain & Crew system, and session-keeper all landed. Current work is operational hardening + fleet economy, tracked as June campaigns:

| Campaign | Epic | Status (2026-06-20) |
|---|---|---|
| **keeper-redesign** (per-session context-watcher hardening) | `hk-gffc` | ✅ COMPLETE — 11 beads closed, identity-first + blind-keeper alarm + per-crew keeper + 280K hard-ceiling |
| **captain-economy** (lean the captain boot + skill-lean) | `hk-unjy` | 🟡 IN PROGRESS — CE1 landed (boot ~81k→~55-60k); CE4/CE5/CE6 held |
| **productization** (deployable on any project + integration-branch enforcement) | `codename:productization` | 🟡 IN PROGRESS — P0 gate landed (integration-branch enforcement), embed/init scaffolding ongoing |
| **fleet economy** (sleep/wake, leanfleet, token-burn) | `hk-rl4b` / `hk-itoc` / `hk-bsdr` | 🟡 ranked, mostly unstaffed (LEAN park) |
| **remote-substrate** (distribute bead-work to a 2nd machine) | `hk-rs-phase1` | 🟡 phase-1, e2e blocked on gb-mbp worker |

_Earlier high-level epic order (current state → fully operational harmonik that orchestrators can drive at scale) follows._

## Current state

Phase 1 reached OPERATIONAL GREEN on 2026-05-14: harmonik dispatches a bead to a real Claude Code subprocess, Claude commits the work, the daemon detects completion and closes the bead — zero human input. Phase 2 entry (first-demo round-trip: daemon merges run-branch to main and closes the bead via harmonik rather than a sub-agent) reached GREEN on 2026-05-15 (`hk-09tne`). The extqueue v0.1 spec package is on branch `extqueue-v0.1-specs` awaiting merge; early implementation tasks (T10–T20) have already landed on main.

## Roadmap

| # | Status | Epic / Phase | Bead ID(s) | Description |
|---|--------|--------------|------------|-------------|
| 1 | DONE | Phase 1: smoke loop end-to-end | (closed) | Harmonik runs claude on one bead, zero human input — 24 s wall-clock. Achieved 2026-05-14. |
| 2 | DONE | Phase 2 entry: first-demo round-trip | hk-09tne (closed) | Daemon merges run-branch to main and closes the bead autonomously. Achieved 2026-05-15. |
| 3 | DONE | Post-phase-1 parallelism | hk-e61c3 (closed) | `--max-concurrent N` validated in t8/t11 throughput tests; capacity gate confirmed. |
| 4 | IN PROGRESS | extqueue v0.1 implementation | hk-lj0pb (epic) + 22 task beads | External orchestrator submits an ordered wave queue; daemon executes it, decoupling bead selection from daemon internals. |
| 5 | OPEN | Bridge completeness + session lifecycle | hk-gql20, hk-kqdpf (P0 epics) | Close remaining gaps: tmux substrate wiring, dual-path collapse, review-loop task injection. hk-lj1p9 (session lifecycle) closed 2026-05-15. |
| 6 | OPEN | imrest — bead in_progress as activity marker | hk-iuaed | Spec + impl: decouple `br claim` (activity marker, recoverable) from close/reopen (truth claim); add orphan-reset sweep on restart. |
| 7 | OPEN | CHB spec corpus implementation | hk-qo08q | Implement `specs/claude-hook-bridge.md` req-beads (CHB-NNN) including hook-relay subcommand, socket acceptor, and durable checkpoint. |
| 8 | TESTING (gated) | Phase 2 multi-bead E2E smoke | hk-1n0cw | Run a 5–10 bead queue through harmonik at `--max-concurrent N`; observe completion stream and merge dance. |
| 9 | NEXT | Handler pause-and-resume | hk-107gz, hk-m0k0a, hk-9hwbw, hk-37zy8, hk-kac8g, hk-ejyku, hk-39ryh, hk-siuo2, hk-ifqnj | Per-handler-type pause (not whole-queue) on handler-fatal failure classes (rate-limit hysteresis, budget-account exhaustion). Daemon persists pause state in `.harmonik/handler-state.json`, exposes `harmonik handler status/resume` CLI, holds dispatch for paused types while live types proceed. Required at Phase-2 scale: a single Claude rate-limit must not tomb a 50-bead wave. Spec: [`specs/handler-pause.md`](specs/handler-pause.md). |
| 10 | NEXT | Remaining spec-corpus implementation | hk-b3f, hk-hqwn, hk-8i31, hk-a8bg, hk-8mwo, hk-8mup, hk-sx9r, hk-63oh | Implement the 8 spec epics (EM, EV, HC, CP, WM, PL, ON, RC) that are open but not yet in the active critical path. |
| 11 | NEXT | Scenario Harness implementation | hk-i0tw | Implement `specs/scenario-harness.md` — the structured test surface that lets harmonik validate its own workflows declaratively. |
| 12 | FUTURE | Phase 3: DOT-defined bead processes | (not yet filed) | Replace hard-coded workloop/reviewloop with composable node graphs defined in DOT format; each node is a phase, edges encode verdict fan-out. |
