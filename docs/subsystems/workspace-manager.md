---
title: "S06: Workspace Manager"
status: seed
type: subsystem
solves: [P04]
uses: [adze, git worktree]
language: Go
related: [docs/concepts/kilroy.md, docs/concepts/symphony.md, docs/concepts/gas-town-hooks.md, docs/subsystems/agent-runner.md, docs/subsystems/orchestrator-core.md]
created: 2026-04-13
updated: 2026-04-19
---

# S06: Workspace Manager

## Summary
The workspace manager creates, configures, and cleans up working environments for each workflow's agent activity. The default unit of isolation is a **git worktree per workflow branch** -- not per agent. Multiple agents working on the same workflow (impl agent → review agent → revision agent → QA agent) share a single workspace, taking it sequentially. Parallel workflows get their own worktrees and merge through controlled convergence. The orchestrator's job is to schedule work so parallel branches do not produce conflicts that are unsafe to merge.

## Purpose
When multiple workflows run concurrently against the same codebase, they collide. Workflow A modifies a file that Workflow B is reading. Workflow C runs tests while Workflow D is mid-refactor, producing spurious failures. These collisions are silent and insidious -- they manifest as mysterious bugs, flaky tests, and agent confusion.

The workspace manager eliminates collisions by giving each *workflow branch* its own isolated worktree. A workspace is a git worktree: a full checkout of the repository at a specific branch point, in its own directory, with its own working tree state. Agents within the workflow read and write freely within the workspace. Changes from parallel workflow branches merge through controlled convergence points -- and merge conflicts are resolved by an agent (or escalated), not avoided through file-reservation schemes.

This is the **Gas Town pattern**: isolated environments + merges, with agents capable of resolving the conflicts merges produce. It is *not* the agent-mail pattern of file reservations across a shared workspace -- that approach was considered and rejected. Reasons: file reservation requires agents to predict their file footprint up front (they often can't), creates coordination overhead that scales poorly, and serializes work that worktree isolation could parallelize. Agents resolving merge conflicts is an acceptable cost; the orchestrator's job is to schedule branches so that conflicts are tractable.

## Key Responsibilities
- **Worktree-per-workflow-branch lifecycle.** Create a git worktree when a workflow branch starts, hand it sequentially to the agents the workflow dispatches, clean it up after the branch merges or is abandoned.
- **Environment setup.** Configure each workspace using adze: install dependencies, set environment variables, configure tools. Workspaces created from the same specification must be identical.
- **Workspace lifecycle hooks.** Provide hook points for other subsystems to inject behavior: `after_create` (install dependencies), `before_agent_handoff` (validate clean state for next agent), `after_agent_handoff` (capture diff/artifacts), `before_remove` (archive results, run final checks).
- **Branch management.** Track which worktrees correspond to which workflow branches. Manage the mapping between logical workflow branches and physical git branches.
- **Merge convergence support.** Provide the orchestrator with the primitives to merge parallel branches: dry-run merges to detect conflicts, apply merges, surface conflicts to a designated resolver agent.

## Interfaces

**Inputs:**
- Workspace creation requests from orchestrator (S01) at workflow-branch start
- Agent handoff signals (sequential agents within a workflow)
- Environment specifications from workflow definitions

**Outputs:**
- Workspace paths and branch names returned to agent runner (S04)
- Workspace lifecycle events emitted to event bus (S03)
- Merge results (success / conflict + locations) returned to orchestrator
- Cleanup confirmations after workspace removal

## Design Principles
- **Isolation between workflow branches, sharing within a branch.** Parallel workflow branches always get separate worktrees. Sequential agents within a single workflow branch share the worktree. This is the unit of isolation that matches actual collaboration patterns -- impl agent does its work, review agent reads the result in place, revision agent edits in place.
- **Merges over reservations.** When parallel branches need to converge, merge. Conflicts are resolved by agents (or escalated to humans for unsafe cases). The system does not attempt to prevent conflicts at write-time through file reservations.
- **Orchestrator schedules to limit conflict.** Reducing merge pain is the orchestrator's job: avoid scheduling parallel branches that touch overlapping file sets when alternatives exist. The workspace manager does not enforce this; it provides the merge primitives the orchestrator uses.
- **Reproducible environments.** Adze handles the environment setup, ensuring consistency across workspaces and across time.
- **Cheap creation, clean removal.** Git worktrees are lightweight -- creating one is fast. The workspace manager must also ensure clean removal: no orphaned branches, no leftover directories, no leaked environment state.

## Implementation Direction

**Primary: git worktrees + adze.** `git worktree add` for isolation, adze for environment setup. Uses existing tools, well-understood model, minimal infrastructure.

**Deferred: container-based isolation.** Process-level isolation has appeal for security and reproducibility, but adds significant operational complexity (image management, runtime, networking). Revisit when worktree-only proves insufficient.

**Deferred: overlay filesystems.** Disk-saving optimization. Worth considering only when worktree disk usage becomes a real constraint.

## Open Questions
1. Conflict resolution: which agent role resolves merge conflicts -- a dedicated "merge agent" type, the original implementer, or always escalate to a reviewer? Initial guess: dedicated role with policy controlling escalation thresholds.
2. What is the lifecycle of a workspace after its workflow branch merges? Persist for N days for debugging/review, or clean up immediately?
3. How do we handle workspaces for non-git artifacts -- databases, cloud resources, external service configurations -- that workflows may need alongside their code workspace?
4. How does the workspace manager interact with the bootstrap "stop on major issue" control -- when a workflow halts, do its workspaces persist indefinitely until the operator decides what to do?

## Cross-References
- [S01: Orchestrator Core](orchestrator-core.md) -- requests workspace creation, owns branch scheduling and merge convergence
- [S04: Agent Runner](agent-runner.md) -- launches agents inside the workspaces this subsystem creates
- [S03: Event Bus](event-bus.md) -- receives workspace lifecycle events
- [P04: System Coherence at Scale](../problems/system-coherence.md) -- workspace isolation prevents coherence degradation
- [Gas Town Hooks](../concepts/gas-town-hooks.md) -- the isolation-plus-merges pattern this subsystem follows
- [Kilroy concept digest](../concepts/kilroy.md) -- parallel isolation pattern via git worktrees
