---
title: MCP Agent Mail
status: explored
type: component
category: external
source: https://github.com/Dicklesworthstone/mcp_agent_mail_rust
related: [docs/components/internal/kerf.md, docs/components/external/ntm.md, docs/problems/system-coherence.md]
created: 2026-04-13
updated: 2026-04-13
---

# MCP Agent Mail

## Summary
Agent Mail is a Rust binary providing 36 MCP tools across 9 functional clusters for agent-to-agent coordination. It gives agents persistent identities, threaded messaging, advisory file reservations, and build coordination -- all backed by a Git audit trail with SQLite as a rebuildable index. Agent Mail is the communication fabric that lets independent agent processes coordinate without sharing memory or direct coupling.

## Key Capabilities

### Persistent Identity
Agents receive memorable adjective+noun names (e.g., "swift-falcon") that persist across sessions. This means an agent's identity, history, and relationships survive session boundaries -- a direct counter to the ephemeral nature of most agent interactions.

### Threaded Messaging
Structured messages with explicit recipients, subjects, thread IDs, importance levels, and acknowledgment requirements. This is not a raw message queue -- it is a communication system with enough structure for agents to manage complex multi-party conversations.

### Advisory File Reservations
Glob-based path reservations with TTL expiration. An agent claims files it intends to edit; other agents see the reservation and avoid conflicts. Self-healing when agents crash -- reservations expire automatically, preventing permanent deadlocks.

### Git-Backed Audit Trail
Every artifact (message, reservation, identity record) is a JSON file committed to Git. SQLite indexes provide fast queries, but the index is rebuildable from the Git history. This makes the coordination layer fully auditable and recoverable.

### Build Slots
Lease-based concurrency control for shared resources like build systems and test suites. Agents acquire a slot before running a build, preventing resource contention that would otherwise cause flaky test results or build failures.

### Cross-Project Product Bus
Agents working in different repositories can communicate through shared product associations. This enables coordination across repo boundaries without requiring all agents to share a single workspace.

### Contact Policies
Configurable trust boundaries between agents. Policies control who can message whom, what importance levels are permitted, and what file paths an agent can reserve. This prevents runaway coordination where every agent talks to every other agent.

### Tool Clusters
The 36 tools organize into 9 clusters: identity, messaging, contacts, file reservations, search, workflow macros, product bus, build slots, and admin. Each cluster is independently useful; agents only need the clusters relevant to their role.

## Integration Points for Harmonik

Agent Mail is the **communication and coordination layer**. Its role in the system:

- **Universal coordination substrate**: All inter-agent communication flows through Agent Mail. This includes kerf dispatching beads, NTM reporting status, and agents coordinating file access.
- **Identity continuity**: Persistent agent identities directly address the Agent Persistence Gap (P02). An agent's work history and relationships are retrievable across sessions.
- **Conflict prevention**: File reservations and build slots prevent the coordination failures that multiply as agent count grows, addressing System Coherence at Scale (P04).
- **Git audit trail aligns with architecture principles**: The Git-backed store matches harmonik's principle that state lives in Git. The coordination layer is as auditable as the code it coordinates.
- **Cross-project coordination**: The product bus enables harmonik to orchestrate work across multiple repositories, which is necessary for any real system composed of multiple services.

## Limitations and Gaps

- **Advisory, not enforced**: File reservations are advisory. A misbehaving agent can ignore reservations and edit files anyway. The system depends on agents respecting the protocol.
- **No message ordering guarantees**: Messages are delivered but ordering across threads is not strictly guaranteed. Agents must handle out-of-order delivery.
- **Single-machine default**: The default deployment serves agents on one machine. Multi-machine coordination requires configuring shared Git repositories or network access to the Agent Mail server.
- **MCP dependency**: Agents must support the MCP protocol to use Agent Mail tools. Agents without MCP support cannot participate in the coordination layer.

## Open Questions

1. Should Agent Mail's messaging be the only inter-agent communication channel, or are there cases where direct communication (shared files, pipes) is appropriate?
2. How should contact policies be configured in harmonik? Should they be derived from the workflow graph (agents in the same pipeline can communicate) or managed separately?
3. What is the failure mode when Agent Mail is unavailable? Should agents be able to degrade gracefully to uncoordinated operation?
