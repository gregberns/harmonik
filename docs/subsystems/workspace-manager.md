---
title: "S06: Workspace Manager"
status: seed
type: subsystem
solves: [P04]
uses: [adze, agent-mail, git]
related: [docs/concepts/kilroy.md, docs/concepts/symphony.md, docs/components/external/agent-mail.md, docs/subsystems/agent-runner.md]
created: 2026-04-13
updated: 2026-04-13
---

# S06: Workspace Manager

## Summary
The workspace manager creates, configures, and cleans up isolated working environments for each agent and task. It ensures that parallel agents cannot interfere with each other by giving each one its own git worktree, directory structure, and environment configuration. Workspace isolation is the structural foundation for system coherence at scale (P04).

## Purpose
When multiple agents work on the same codebase simultaneously, they collide. Agent A modifies a file that Agent B is reading. Agent C runs tests while Agent D is mid-refactor, producing spurious failures. These collisions are silent and insidious -- they manifest as mysterious bugs, flaky tests, and agent confusion.

The workspace manager eliminates collisions by giving each agent its own isolated workspace. A workspace is a git worktree: a full checkout of the repository at a specific branch point, in its own directory, with its own working tree state. Agents read and write freely within their workspace. Changes merge back to the main branch only through controlled convergence points in the workflow.

## Key Responsibilities
- **Git worktree creation and cleanup.** Create a worktree for each agent task, branching from the appropriate point in the repository. Clean up worktrees when tasks complete or are abandoned.
- **Environment setup.** Configure the workspace environment using adze: install dependencies, set environment variables, configure tools. Each workspace gets a consistent, reproducible environment.
- **File reservation coordination.** Integrate with agent-mail to manage file reservations. When an agent claims files in its workspace, other agents are informed through agent-mail to avoid conflicting work.
- **Workspace lifecycle hooks.** Provide hook points for other subsystems to inject behavior: `after_create` (install dependencies), `before_run` (inject configuration), `after_run` (collect artifacts), `before_remove` (archive results).
- **Branch management.** Track which worktrees correspond to which workflow branches. Manage the mapping between logical workflow branches and physical git branches.

## Interfaces

**Inputs:**
- Workspace creation requests from agent runner (S04)
- Environment specifications from workflow definitions
- File reservation data from agent-mail

**Outputs:**
- Workspace paths and branch names returned to agent runner (S04)
- Workspace lifecycle events emitted to event bus (S03)
- File reservation requests sent to agent-mail
- Cleanup confirmations after workspace removal

## Design Principles
- **Isolation by default.** Every agent gets its own workspace. Shared workspaces are never the default -- they must be explicitly configured for the rare cases where agents need to collaborate on the same working tree.
- **Reproducible environments.** Two workspaces created from the same specification must be identical. Adze handles the environment setup, ensuring consistency across agents and across time.
- **Cheap creation, clean removal.** Git worktrees are lightweight -- creating one is fast. The workspace manager must also ensure clean removal: no orphaned branches, no leftover directories, no leaked environment state.

## Candidate Implementations
- **Git worktrees + adze.** The straightforward approach: `git worktree add` for isolation, adze for environment setup. Advantage: uses existing tools, well-understood model. Risk: worktree management at scale (dozens of concurrent agents) may need optimization.
- **Container-based isolation.** Each workspace is a lightweight container with its own filesystem. Advantage: stronger isolation (process-level, not just filesystem-level). Risk: heavier weight, slower creation, more infrastructure.
- **Worktrees with overlay filesystems.** Use filesystem overlays to share read-only base layers across workspaces. Advantage: reduces disk usage for large repositories. Risk: complexity, platform-specific.

## Open Questions
1. How should the workspace manager handle merge conflicts when converging parallel workspaces? Should it attempt automatic resolution, or always escalate to an agent or human?
2. What is the lifecycle of a workspace after its agent completes? Should it persist for debugging/review, or be cleaned up immediately to conserve resources?
3. How do we handle workspaces for non-git artifacts -- databases, cloud resources, external service configurations -- that agents may need alongside their code workspace?

## Cross-References
- [S04: Agent Runner](agent-runner.md) -- requests workspace creation before agent launch
- [S01: Orchestrator Core](orchestrator-core.md) -- parallel branch isolation depends on workspace isolation
- [S03: Event Bus](event-bus.md) -- receives workspace lifecycle events
- [P04: System Coherence at Scale](../problems/system-coherence.md) -- workspace isolation prevents coherence degradation
- [Agent Mail component](../components/external/agent-mail.md) -- file reservation coordination
- [Kilroy concept digest](../concepts/kilroy.md) -- parallel isolation pattern via git worktrees
