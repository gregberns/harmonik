---
title: "I07: Filesystem as Coordination Substrate"
status: seed
type: idea
relates-to: [P02, P03, G02]
sources: [docs/concepts/symphony.md, docs/concepts/harness-engineering.md]
created: 2026-04-13
updated: 2026-04-13
---

# I07: Filesystem as Coordination Substrate

## Core Concept

Agents coordinate through persistent artifacts on disk, not through conversation history or shared memory. Progress files, spec documents, review notes, test results, state markers -- all git-tracked. Each agent session starts fresh and rebuilds context from the filesystem. The filesystem is the single source of truth.

## Why Not Conversation History

Conversation history is the default coordination mechanism for LLM agents: the context window contains everything the agent has seen and done. This breaks in several ways:

- **Ephemeral**: When a session ends, the conversation history is gone. A new session starts with no memory of what happened before (P03).
- **Per-session**: Two agents cannot share a conversation history. They operate in isolated context windows with no native mechanism for cross-agent communication.
- **Bounded**: Context windows have limits. Long-running tasks overflow the window, and older context is lost or compressed.
- **Unversioned**: There is no diff, no blame, no history of how the conversation evolved. Debugging a bad outcome requires replaying the full conversation.

The filesystem has none of these problems. Files persist across sessions. Multiple agents can read the same files. Files have no size limit. Git provides full version history, diff, blame, and rollback.

## The Pattern in Practice

### Symphony's Workspace Lifecycle
Symphony creates a workspace directory for each task. The workspace contains the task spec, progress updates, and final output. The daemon (orchestrator) monitors workspaces. Agents read from and write to their workspace. Coordination happens through the filesystem.

### kerf's Bench
kerf uses a bench directory as the workspace for spec decomposition. Beads (work items) are files on disk. An agent picks up a bead by reading its file, does work, and writes results back. The bench is the coordination substrate.

### Harness Engineering's Filesystem-Backed Coordination
The harness engineering pattern places guides (instruction files) and sensors (output files) on the filesystem. An agent reads its guide, does work, writes to its sensor location. The harness reads sensor output and decides what happens next. All coordination flows through files.

### agent-mail's Git-Backed Audit Trail
agent-mail uses git commits as messages between agents. Every communication is a commit -- persistent, versioned, auditable. The git log IS the communication history.

## What Gets Written to Disk

For harmonik, the coordination substrate should include:

- **State files**: Current workflow state, per-task status, agent assignments. The orchestrator reads these to make transition decisions.
- **Spec artifacts**: Task specifications, acceptance criteria, architectural constraints. Agents read these to understand their work.
- **Progress markers**: What has been done, what remains, what is blocked. Enables resume after session interruption.
- **Review artifacts**: Review comments, approval/rejection decisions, change requests. Flow from reviewer to implementor via files.
- **Verification results**: Test output, linter results, type check results. The verifier layer writes these; the orchestrator reads them.

## Design Constraints

- **Atomic writes**: File updates must be atomic to prevent agents from reading half-written state. Git commits provide natural atomicity.
- **Conflict resolution**: Two agents must not write conflicting updates to the same file. Workspace isolation (one agent per workspace) prevents this. Shared state files need a locking or merge protocol.
- **Discoverability**: Agents must be able to find the files they need. Convention-based paths (e.g., `.harmonik/state.json`, `.harmonik/tasks/TASK-001/spec.md`) make discovery deterministic.

## Open Questions

1. What is the right file format for state files? JSON for machine parsing, Markdown for human readability, or both?
2. How do we handle the cold-start problem -- an agent needs filesystem context to start, but the context does not exist until agents have run?
3. How much filesystem structure should be standardized versus workflow-specific?
4. What is the performance ceiling? At what scale does filesystem I/O become a bottleneck?

## Cross-References
- [P02: Agent Persistence Gap](../problems/agent-persistence-gap.md) -- Filesystem artifacts survive session boundaries
- [P03: Knowledge Loss](../problems/knowledge-loss.md) -- Git-tracked files prevent knowledge loss
- [G02: Persistent Problem Pursuit](../goals/persistent-problem-pursuit.md) -- Persistent artifacts enable persistent pursuit
- [Symphony](../concepts/symphony.md) -- Workspace lifecycle pattern
