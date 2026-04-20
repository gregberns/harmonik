---
title: "S03: Event Bus"
status: seed
type: subsystem
solves: [P06]
uses: [all subsystems]
language: Go
related: [docs/concepts/kilroy.md, docs/concepts/alphago-system.md, docs/subsystems/orchestrator-core.md, docs/subsystems/improvement-loop.md, docs/subsystems/memory-layer.md]
created: 2026-04-13
updated: 2026-04-19
---

# S03: Event Bus

## Summary
The event bus is harmonik's communication backbone. It provides publish-subscribe event routing so that subsystems can communicate without direct coupling. Every state transition, agent output, policy decision, and verification result flows through the event bus as a typed, schema-enforced event. Events are persisted for replay, analysis, and the improvement loop.

## Purpose
A composed system of nine subsystems needs a communication substrate that prevents tight coupling. Without an event bus, the orchestrator calls the agent runner which calls the hook system which calls the verifier -- and changing any subsystem's interface breaks everything downstream. The event bus decouples producers from consumers. The orchestrator emits a `state_transition` event; any subsystem that cares about state transitions subscribes. No subsystem needs to know who else is listening.

Events are also harmonik's primary data asset. The improvement loop (S09) analyzes event streams to find bottlenecks, failure patterns, and optimization opportunities. The memory layer (S08) indexes events for future retrieval. Without a persistent event stream, the system cannot learn from its own execution (P07).

## Key Responsibilities
- **Event routing (pub/sub).** Subsystems publish typed events. Other subsystems subscribe to event types they care about. Routing is topic-based.
- **Event persistence.** Every event is stored durably for replay, debugging, and analysis. Events are the source of truth for what happened during workflow execution.
- **Event schema enforcement.** Each event type has a defined schema. Malformed events are rejected at publish time, not discovered downstream.
- **Backpressure management.** When consumers fall behind, the bus applies backpressure rather than dropping events. This prevents silent data loss.
- **Event filtering.** Subscribers can filter by event type, workflow ID, agent ID, or custom predicates. Subsystems receive only the events they need.

## Interfaces

**Inputs (publish):**
- State transitions from orchestrator core (S01)
- Policy decisions and violations from policy engine (S02)
- Agent lifecycle events from agent runner (S04)
- Hook execution results from hook system (S05)
- Verification results from verifier layer (S07)
- Memory updates from memory layer (S08)

**Outputs (subscribe):**
- Improvement loop (S09) subscribes to all events for analysis
- Memory layer (S08) subscribes to session events for indexing
- Orchestrator core (S01) subscribes to agent completion events
- Any subsystem subscribes to the event types it needs

**Event schema (example):**
```yaml
event:
  id: uuid
  type: state_transition | agent_completed | verification_result | policy_violation | ...
  timestamp: iso8601
  workflow_id: string
  source_subsystem: string
  payload: typed per event type
```

## Design Principles
- **Events are first-class citizens.** Events are not logging afterthoughts. They are the primary mechanism for inter-subsystem communication and the primary data source for system improvement. Design events with the same care as API contracts.
- **Schema-first.** Define event schemas before implementing producers or consumers. Schemas are the contract. Changes to schemas follow a versioning protocol.
- **Replay-safe.** The event stream can be replayed from any point to reconstruct system state. This means events must be idempotent or consumers must handle deduplication.

## Implementation Direction

**Decision: in-process pub/sub backed by JSONL files on disk.**

- **In-process pub/sub** for live subscribers (other subsystems within the running harmonik process). Low latency, zero infrastructure, fits the single-machine MVH (see [bootstrap.md](../bootstrap.md)).
- **JSONL persistence on disk** for every published event. One line per event, append-only files, organized by workflow ID (and rotated by size or time within a workflow). The file is the source of truth; the in-process bus is a notification mechanism over it.
- **Improvement loop (S09) reads the JSONL** -- it does not need to be a live subscriber. Reading-from-disk decouples slow analysis from real-time execution.
- **CASS / memory layer (S08) also reads the JSONL** for events it needs (separate from agent-process logs, which are sourced from agent-specific log files -- see [memory-layer.md](memory-layer.md)).

Graduation path: if/when distributed deployment is needed, swap the in-process bus for NATS JetStream or Redis Streams, *keeping* the JSONL persistence as the canonical record. JSONL files remain the audit trail regardless of transport.

### Storage Considerations

JSONL volume is going to be substantial. A single self-build cycle will emit hundreds-to-thousands of events; many concurrent cycles compound it. Open considerations:

- **Rotation.** Per-workflow files capped by size; older segments compressed or moved to cold storage.
- **Indexing.** JSONL alone does not support fast filtered queries; for the improvement loop we may need to build a sidecar index (sqlite over the JSONL, for instance) when query patterns crystallize.
- **Retention.** Indefinite retention is cheap until it isn't. We need an explicit retention policy before storage costs become a surprise.
- **Location.** Single canonical event-log root directory (configurable, defaulting to something like `~/.harmonik/events/<workflow-id>/`) so consumers (improvement loop, scenario harness assertions, debugging tools) all know where to look.

## Open Questions
1. JSONL rotation policy -- size-based, time-based, or workflow-end-based?
2. Retention policy -- indefinite by default, or TTL with archival to cold storage?
3. Sidecar index format -- sqlite, DuckDB, or defer until query patterns are known?
4. What is the right event granularity -- should every agent output line be an event, or only significant lifecycle moments (started, completed, failed)?
5. How are agent-process logs (e.g., Claude's `~/.claude/projects/<uuid>.jsonl`) related to the event bus? They are *not* the same stream -- agent-process logs are produced by the agent binary and consumed by CASS; events are produced by harmonik subsystems. Need to be clear on both flows in any consumer doc.
