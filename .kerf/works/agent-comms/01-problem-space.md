# agent-comms — Problem Space

## Summary

Multiple concurrent Claude Code sessions (orchestrator agents — e.g. `flywheel`,
`named-queues`) operate on one harmonik project, sharing a single daemon. They must
coordinate (announce shared-resource actions, hand off work, flag bugs). Today this is
done with files: v0 was a shared append-only `AGENT_COMMS.md`; v1 (landed 2026-06-01) is
per-agent outbox files under `.harmonik/comms/<name>.md` that each agent tails. The
file approach has proven friction — concurrent-append races (garbled entries),
tail-replay glitches when a file is rewritten, daemon escape-detector false-positives
from untracked repo-root files, and it does not generalize past hardcoded peers.

This work routes agent comms **through harmonik itself**: a typed, ordered, durable
`agent_message` carried on the existing event bus, sent via a `harmonik comms` CLI and
received via the existing `harmonik subscribe`. The daemon (already the single broker
per project) becomes the message broker too.

## Goals

- **G1 — N agents, dynamic membership.** Support any number of agents joining/leaving at
  runtime, with a lightweight presence registry so agents can discover and address each
  other. (Confirmed position 1.)
- **G2 — Durable delivery with a per-agent cursor.** A message sent while a recipient is
  heads-down (busy/not subscribed) is delivered on the recipient's next read — replay
  from its cursor, not live-stream-only. No silent drops. (Confirmed position 2 — the
  load-bearing decision.)
- **G3 — Directed and broadcast addressing.** Agents can send to a specific recipient
  (`to:<name>`) or broadcast to all; `subscribe` filters server-side by to/from/topic.
  (Confirmed position 3.)
- **G4 — Comms live in harmonik, not the working tree.** Retire the file-append protocol;
  no working-tree files for messages. Provide a read-only operator view
  (`harmonik comms log`) so a human can still read the conversation. (Confirmed position 4.)

## Non-goals

- The file outboxes (`.harmonik/comms/*.md`) as a long-term parallel channel — they are
  kept only as the migration bootstrap and are retired once this lands.
- Threading, attachments / large payloads, rich formatting.
- Cross-**project** comms (single-project scope for now).
- Inter-agent authentication / ACLs / trust boundaries.
- A human-facing comms UI beyond the read-only `harmonik comms log`.

## Constraints

- **Reuse existing infra:** the event bus (`.harmonik/events/events.jsonl`, typed events)
  as the durable log; `harmonik subscribe` (NDJSON + server-side heartbeat) as the
  delivery transport; the single per-project daemon as the broker.
- **No working-tree files** for messages — this is what caused the escape-detector
  false-positives (`hk-77q8e`) and append-races in the file approach.
- **Must not disrupt** existing event types or the queue/run event flow on the bus.
- Agents already run `harmonik subscribe` for run events, so comms receipt should compose
  with (or extend) that existing subscription, not require a separate transport.
- Single daemon per project (pidfile lock) — comms ride that daemon; if it restarts,
  durable messages must survive (they are events in the log).

## Success criteria

1. Agent A sends a directed message to B (`harmonik comms send --to B "..."`); B receives
   it via `harmonik subscribe` **even if B connects/subscribes after the send** (durable
   replay from B's cursor).
2. Agent A broadcasts; all currently-registered agents receive it.
3. **No working-tree files** are created for any message (verified: a send leaves
   `git status` unchanged → no escape-detector interaction, no append-race).
4. Messages are **totally ordered** (bus order) and **durable** — re-readable from the
   event log and surviving a daemon restart.
5. The operator can read recent comms via a read-only command (`harmonik comms log`).
6. Works for **N ≥ 2** agents with dynamic join/leave: an agent that joins later can
   address pre-existing agents and be addressed by them.

## Context / provenance

- Bead: `hk-uxm0j` (pinned to this work).
- Sibling work: `hk-ekap1` (auto session-lifecycle / context-threshold handoff) — both are
  "the supervisor coordinating long-running agents," sharing pasteinject + event-bus infra.
- Lane split: `named-queues` thread owns the **event-model** side (`agent_message` shape,
  bus delivery, subscribe-side filtering); `flywheel` thread owns this problem-space/spec
  and the `harmonik comms` CLI surface.
- The 4 positions above were confirmed by the user on 2026-06-01.
