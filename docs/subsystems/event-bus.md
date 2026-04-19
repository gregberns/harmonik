---
title: "S03: Event Bus"
status: seed
type: subsystem
solves: [P06]
uses: [all subsystems]
related: [docs/concepts/kilroy.md, docs/concepts/alphago-system.md, docs/subsystems/orchestrator-core.md, docs/subsystems/improvement-loop.md]
created: 2026-04-13
updated: 2026-04-13
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

## Candidate Implementations
- **In-process event bus.** For single-machine deployment: a typed pub/sub library within the harmonik process. Advantage: zero infrastructure, low latency. Risk: no persistence without explicit storage layer.
- **Redis Streams.** Persistent, ordered event log with consumer groups. Advantage: mature, supports backpressure, built-in persistence. Risk: external dependency.
- **NATS JetStream.** Lightweight message broker with persistence. Advantage: designed for this exact use case. Risk: another moving part.
- **File-based event log.** Append-only files (one per workflow). Advantage: zero dependencies, git-friendly. Risk: limited query capabilities, no real-time pub/sub.

## Open Questions
1. Should the event bus start as an in-process library (simplest) and graduate to an external broker (Redis/NATS) only when distributed deployment is needed?
2. How long should events be retained? Indefinitely for analysis, or with a TTL and archival strategy?
3. What is the right event granularity -- should every agent output line be an event, or only significant lifecycle moments (started, completed, failed)?
