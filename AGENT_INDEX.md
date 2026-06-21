# Harmonik -- Agent Discovery Index

> **Start here.** This is the master index for the harmonik knowledge base. Every document in the project is reachable from this file within two hops.
>
> **New to harmonik?** See [README.md](README.md) for what it is and how to install it.
>
> **Reading order on boot:** AGENT_INDEX (this map) → [STATUS.md](STATUS.md) (phase + locked decisions) → [`.harmonik/context/captain-lanes.md`](.harmonik/context/captain-lanes.md) (live lanes, medium-term) → [HANDOFF.md](HANDOFF.md) (this-session state).
>
> **Roadmap & progress:** [ROADMAP.md](ROADMAP.md). **Live lanes / epics-in-progress:** [`.harmonik/context/captain-lanes.md`](.harmonik/context/captain-lanes.md).

## What Is Harmonik?

Harmonik is a composable agentic orchestration system. Its objective: **maximize our one truly scarce resource -- human time and attention** -- by building structured, self-improving systems where agents autonomously move work from idea through implementation.

Core architectural principle: **deterministic skeleton, probabilistic organs**. The orchestration layer (workflows, transitions, governance) is fully deterministic. The intelligence layer (planning, critique, synthesis) is probabilistic (LLM-driven).

## How This Knowledge Base Works

- [Methodology](docs/methodology/METHODOLOGY.md) -- Document conventions, status lifecycle, cross-reference rules
- [Agent Guide](docs/methodology/AGENT_GUIDE.md) -- How to navigate, create, and update documents
- [Testing Methodology](docs/methodology/TESTING.md) -- Five test layers (unit / integration / scenario / crash-recovery / property); coverage targets; twin conformance; testing during bootstrap and self-build

## Knowledge Base Map

### Problems -- What We're Solving
[docs/problems/INDEX.md](docs/problems/INDEX.md)

| ID | Problem | Summary |
|----|---------|---------|
| P01 | [Human Attention Scarcity](docs/problems/human-attention-scarcity.md) | Human engineers are the bottleneck; every re-explanation wastes the scarcest resource |
| P02 | [Agent Persistence Gap](docs/problems/agent-persistence-gap.md) | Agents stop when humans stop pushing; no autonomous problem pursuit |
| P03 | [Knowledge Loss](docs/problems/knowledge-loss.md) | What agents learn dies with the session; no institutional memory |
| P04 | [System Coherence at Scale](docs/problems/system-coherence.md) | Entropy grows as systems get larger; architecture degrades without enforcement |
| P05 | [Agent Behavior Enforcement](docs/problems/behavior-enforcement.md) | Agents are probabilistic; need deterministic mechanisms to ensure process compliance |
| P06 | [Workflow Composition](docs/problems/workflow-composition.md) | Connecting independent workflows into larger systems is complex |
| P07 | [Feedback Loop Absence](docs/problems/feedback-loops.md) | Systems that execute but don't learn from their own execution |

### Goals -- What We Want to Achieve
[docs/goals/INDEX.md](docs/goals/INDEX.md)

| ID | Goal | Summary |
|----|------|---------|
| G01 | [Structured-Emergent Systems](docs/goals/structured-emergent-systems.md) | Highly structured systems that allow emergent behavior |
| G02 | [Persistent Problem Pursuit](docs/goals/persistent-problem-pursuit.md) | Systems that keep pushing on problems without constant human re-direction |
| G03 | [Independent Process-Following Actors](docs/goals/independent-process-following-actors.md) | Agents that reliably follow defined processes |
| G04 | [Learning and Improvement Loops](docs/goals/learning-and-improvement-loops.md) | Systems that learn from execution and improve over time |
| G05 | [Idea-to-Implementation Pipeline](docs/goals/idea-to-implementation-pipeline.md) | End-to-end automation from goal to working software |
| G06 | [Bootstrapping and Self-Building](docs/goals/bootstrapping-self-building.md) | Hand-build a minimum viable harmonik, then have it build itself |
| G07 | [End-to-End Testability Without Real Agents](docs/goals/end-to-end-testability.md) | Full system testable via digital twin binaries; no token spend in tests |

### Concepts -- External Ideas We're Drawing From
[docs/concepts/INDEX.md](docs/concepts/INDEX.md)

| Source | Key Ideas |
|--------|-----------|
| [Kilroy](docs/concepts/kilroy.md) | Graph-as-workflow, deterministic routing, git-native checkpointing |
| [Symphony](docs/concepts/symphony.md) | Daemon orchestration, prompt-as-policy, multi-turn execution |
| [Harness Engineering](docs/concepts/harness-engineering.md) | Guides+sensors, constrain-to-empower, entropy management |
| [Zero Framework Cognition](docs/concepts/zero-framework-cognition.md) | Thin shells, delegate all cognition, mechanism vs policy |
| [AlphaGo-Modeled System](docs/concepts/alphago-system.md) | Deterministic skeleton, controlled openings, meta-process |
| [Gas Town Hooks](docs/concepts/gas-town-hooks.md) | Hook-driven behavior enforcement, bead lifecycle events |
| [Digital Twins](docs/concepts/digital-twins.md) | Separate twin binaries simulate real agent processes for token-free testing |

### Components -- Tools We Have
[docs/components/INDEX.md](docs/components/INDEX.md)

**Internal** (our tools):
| Component | Purpose |
|-----------|---------|
| [kerf](docs/components/internal/kerf.md) | Spec-writing CLI. Planning/specification layer. Jigs, beads, works. |
| [adze](docs/components/internal/adze.md) | Machine configuration. Environment setup. Dependency graphs. |

**External** (third-party tools):
| Component | Purpose |
|-----------|---------|
| [ntm](docs/components/external/ntm.md) | Tmux-based agent process management. Swarm orchestration. |
| ~~agent-mail~~ | ~~Agent communication (MCP server)~~ — **UNINSTALLED 2026-06-08** (runaway 18 GB log caused disk-full wedges). Replaced by `harmonik comms` bus. |
| [CASS](docs/components/external/cass.md) | Session search. Episodic memory. Cross-agent knowledge. |
| [CASS Memory](docs/components/external/cass-memory.md) | Institutional memory. Three-layer cognition. Playbook rules. |
| [Kilroy](docs/components/external/kilroy.md) | Workflow execution engine. DOT graph pipelines. |

### Subsystems -- How Harmonik Decomposes
[docs/subsystems/INDEX.md](docs/subsystems/INDEX.md)

| ID | Subsystem | Purpose | Key Components |
|----|-----------|---------|----------------|
| S01 | [Orchestrator Core](docs/subsystems/orchestrator-core.md) | State machine + workflow execution | kilroy (or custom) |
| S02 | [Policy Engine](docs/subsystems/policy-engine.md) | Role permissions + transition guards | config/YAML |
| S03 | [Event Bus](docs/subsystems/event-bus.md) | Pub-sub communication backbone | custom or external |
| S04 | [Agent Runner](docs/subsystems/agent-runner.md) | Spawn, monitor, manage agent processes | ntm |
| S05 | [Hook System](docs/subsystems/hook-system.md) | Bridge between agents and workflow | Claude Code hooks |
| S06 | [Workspace Manager](docs/subsystems/workspace-manager.md) | Isolated work environments | adze, agent-mail |
| S07 | [Scenario Harness](docs/subsystems/scenario-harness.md) | End-to-end test harness driving full workflows against twin agent binaries | digital twins, orchestrator |
| ~~S07~~ | ~~[Verifier Layer (archived)](docs/subsystems/verifier-layer.md)~~ | ~~Quality gates~~ -- *responsibilities migrated to orchestrator + policy* | -- |
| S08 | [Memory Layer](docs/subsystems/memory-layer.md) | Long-term knowledge storage + retrieval | CASS, CASS memory |
| S09 | [Improvement Loop](docs/subsystems/improvement-loop.md) | Self-improving meta-process | memory, event bus |

### Ideas -- Brainstorm Captures
[docs/ideas/INDEX.md](docs/ideas/INDEX.md)

| ID | Idea | Priority |
|----|------|----------|
| I01 | [Hook-Driven Agent Behavior](docs/ideas/hook-driven-agent-behavior.md) | High -- core mechanism |
| I02 | [Deterministic Skeleton, Probabilistic Organs](docs/ideas/deterministic-skeleton-probabilistic-organs.md) | High -- architectural principle |
| I03 | [Composable Multi-Workflow Systems](docs/ideas/composable-multi-workflow-systems.md) | High -- system composition |
| I04 | [AlphaGo Search for Coding](docs/ideas/alphago-search-for-coding.md) | Medium -- optimization |
| I05 | [Model Pyramid Cost Optimization](docs/ideas/model-pyramid-cost-optimization.md) | Medium -- cost management |
| I06 | [Agent Specialization Through Constraints](docs/ideas/agent-specialization-through-constraints.md) | Medium -- agent design |
| I07 | [Filesystem as Coordination Substrate](docs/ideas/filesystem-as-coordination-substrate.md) | Medium -- coordination |

### Collaboration Log
[docs/log/INDEX.md](docs/log/INDEX.md)

Most recent entries:
- [2026-06-09: Captain & Crew lands](docs/plans/captain/SESSION.md) -- 15/15 tasks on main; crew system live via `claude --remote-control`; tapCh race (18h incident) fixed
- [2026-06-03: Productization P0 gate](docs/INITIATIVES.md) -- Integration-branch enforcement landed; `harmonik init` / operating-manual in progress
- [2026-06-01: harmonik comms bus](docs/orchestration-protocol-v2.md) -- `harmonik comms send/recv/who/log`; file-outbox convention retired
- [2026-05-30: Persistent daemon model](docs/orchestration-protocol-v2.md) -- `harmonik --project` + supervisor; `harmonik run` → legacy/solo path
- [2026-05-14: Phase 1 operational milestone](docs/historical/dogfood-smoke-traces/dogfood-smoke-run-2026-05-14-operational-green.md) -- Harmonik runs Claude end-to-end on a bead; zero human input
- [2026-04-24: Spec Corpus Foundation Pass](docs/log/2026-04-24-spec-corpus-foundation.md) -- 10 specs authored; 5 reviewed *(historical)*
- [2026-04-13: Initial Brainstorm Session](docs/log/2026-04-13-initial-brainstorm.md) -- First comprehensive capture *(historical)*

### Specs (normative)
- [specs/](specs/) — 10 foundation specs + `_registry.yaml` prefix reservations
- 5 reviewed (v0.3): architecture, execution-model, event-model, handler-contract, control-points
- 5 draft (v0.1): workspace-model, process-lifecycle, operator-nfr, reconciliation, beads-integration
- Template: [docs/foundation/spec-template.md](docs/foundation/spec-template.md) (v1.1)

### Foundation alignment
- [docs/foundation/](docs/foundation/) — problem-space, components, OVERVIEW, core-scope, spec-template, project-level/

### Reviews — *ARCHIVED (Apr 2026 foundation/spec persona reviews; see [docs/reviews/README.md](docs/reviews/README.md))*
- [docs/reviews/2026-04-23-foundation-phase3/](docs/reviews/2026-04-23-foundation-phase3/) — six-persona Phase 3 review of foundation
- [docs/reviews/2026-04-24-project-level/](docs/reviews/2026-04-24-project-level/) — three-persona review of project-level docs
- [docs/reviews/2026-04-24-spec-template/](docs/reviews/2026-04-24-spec-template/) — implementer + critic review of template
- `docs/reviews/2026-04-24-{architecture,execution-model,event-model,handler-contract,control-points}-r{1,2}/` — 6 reviewers per batch-1 spec

### Reviews
- [2026-04-23 Foundation Phase 3 Review](docs/reviews/2026-04-23-foundation-phase3/README.md) -- Six reviewer personas on the amended 10-component foundation; synthesis + round-2 amendment plan

## Plans
- [docs/bootstrap.md](docs/bootstrap.md) -- Bootstrap and self-build plan (companion to G06)

### Recent campaigns (2026-06-20 burst)
- [plans/2026-06-20-state-reassessment-and-doc-sync/](plans/2026-06-20-state-reassessment-and-doc-sync/) -- Authoritative reconciliation of the nine-initiative single-day burst: what landed, the want-vs-got gap, live-validation blockers, operator decisions
- [plans/2026-06-20-doc-instruction-audit/](plans/2026-06-20-doc-instruction-audit/) -- Three-kinds tracking model (docs / behavioral-contract skills / operational-state tiers), AGENTS.md→router, `harmonik sync-assets`
- [plans/2026-06-20-tmux-session-organization/](plans/2026-06-20-tmux-session-organization/) -- Unified `harmonik-<hash>-*` namespace, window-nesting, window-granular restart, `supervise reap`
- [plans/2026-06-20-fleet-sleep-wake-status-and-next/](plans/2026-06-20-fleet-sleep-wake-status-and-next/) -- Fleet-state Phase 0 sleep/wake markers, IsSleeping fail-closed, wake-pane resolution, orphan-marker reconcile
- [plans/2026-06-20-remote-node-telemetry-autoscale/](plans/2026-06-20-remote-node-telemetry-autoscale/) -- Worker-report resource snapshots + breach detection (P1+P2 landed, off-by-default); Phase-3 autoscale deferred

## Progress & live state

- **Roadmap, landed features, milestone log:** [ROADMAP.md](ROADMAP.md)
- **Live lanes, epics-in-progress, lane→crew registry:** [`.harmonik/context/captain-lanes.md`](.harmonik/context/captain-lanes.md)

## Agent Skills (operating contracts)
Booting into a specific role? Load its skill for the operating contract:
- `.claude/skills/orchestrator-rules` -- **LOAD-BEARING standing-rules contract** for any orchestrator (captain, implementer-orchestrator, solo): the single canonical statement of dispatch discipline, kerf-first priority, bead lifecycle (daemon owns terminal transitions; never pre-set in_progress), the review gate, the monitor pattern, CWD discipline (never `cd` into a worktree), autonomy/flow boundaries, and the major-issue fan-out trigger. Loaded as a CONTRACT at captain STARTUP and by the implementer-orchestrator on `/session-resume`. Points to the detail-owner skills; does not duplicate them.
- `.claude/skills/captain` -- captain session: boot runbook, lane organization, crew spawn/verify, surfaces
- `.claude/skills/crew-launch` -- crew session: boot sequence, OWN-queue loop, progress feed, keeper re-hydration
- `.claude/skills/keeper` -- per-session context-fill watcher (warn / handoff-clear-resume thresholds)
- `.claude/skills/harmonik-lifecycle` -- supervise / promote / reconcile / init operations
- `.claude/skills/harmonik-dispatch` -- main-agent daily loop (route ≥75% through the daemon queue)
- `.claude/skills/agent-comms`, `.claude/skills/beads-cli` -- comms bus + `br` write discipline

## Operational Protocols
- the `orchestrator-rules` skill (.claude/skills/orchestrator-rules/SKILL.md) -- Permanent orchestrator directives (dispatch, priority, autonomy, monitor pattern)
- [.harmonik/context/captain-lanes.md](.harmonik/context/captain-lanes.md) -- Live initiatives board: epic IDs, status, done/total counts, blocked items
- [docs/major-issue-fanout-protocol.md](docs/major-issue-fanout-protocol.md) -- Major-issue fan-out diagnosis protocol: when a wedge survives ≥2 fix attempts, fan out 10–15 agents at distinct angles + ≥2 adversarial verifiers; never hand-grep events.jsonl by run_id
- [docs/postmortems/2026-06-09-concurrent-dispatch-wedge.md](docs/postmortems/2026-06-09-concurrent-dispatch-wedge.md) -- tapCh competing-consumer race; 18h incident; 6 refuted hypotheses; fix + process lessons (motivating source for major-issue-fanout protocol)
- [docs/captain-restart.md](docs/captain-restart.md) -- Captain self-restart design (session-keeper on the captain session)
- [docs/orchestration-protocol-v2.md](docs/orchestration-protocol-v2.md) -- Persistent-daemon + queue-submit daily loop; harmonik comms bus; stream/wave queue semantics
- [docs/known-workarounds.md](docs/known-workarounds.md) -- Active workaround registry: worktree bugs, harness quirks, spawn-semaphore wedge mitigation

## Deep References
- [AlphaGo-Modeled Orchestration System](refs/AlphaGo-modeled-orch-system.md) -- 800+ line architectural reference document

## Original Notes (preserved)
- [docs/00_objective.md](docs/00_objective.md) -- Original objective statement
- [docs/01_architecture.md](docs/01_architecture.md) -- Architecture principles
- [docs/01_components.md](docs/01_components.md) -- Component references
- [docs/02_spec-management.md](docs/02_spec-management.md) -- Spec management references
