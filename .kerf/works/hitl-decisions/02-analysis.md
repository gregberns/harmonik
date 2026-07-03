# hitl-decisions — Analysis

Concise current-state / gap / tensions. Detail lives in `01-problem-space.md` (goals, constraints, the four adopted decisions D1–D4) and will be resolved in the change-spec.

## What exists today (reuse, don't rebuild)

- **The event bus** (`agent-comms`, hk-uxm0j, FINALIZED): `events.jsonl` transport, typed events (`agent_message`, `agent_presence`, `operator_pause_status`, …), `harmonik comms send/recv/who/log`, `harmonik subscribe` NDJSON stream + heartbeat, durable per-agent cursor, **at-least-once (N3) → dedupe-on-`event_id`**. This is the exact substrate hitl-decisions extends.
- **The daemon** already hosts comms ops over its socket and runs reconciliation/orphan-sweep passes (a natural home for an orphaned-decision reaper).
- **session-keeper** (hk-ekap1) classifies agent idle/blocked states.
- **kerf** has a planning surface with a latent "what-needs-me across works" framing.

## The gap

Nothing lets an agent address a **human** with a typed, durable, aggregatable decision. Today a HITL point is prose buried in one agent thread: the human must be actively reading it; the agent stalls, guesses, or drops it; and there is **no cross-agent aggregation** of open human-decisions. The three missing pieces:

1. a **human-addressed typed event** (`decision_needed`) carrying question + options + context + who's-blocked, and its answer (`decision_resolved`) / cancellation (`decision_withdrawn`);
2. a **clean block/resume** for the emitting agent (block on its own `decision_id`, wake on the answer — no busy-wait, no guess);
3. a **cross-agent "what-needs-me" projection + one answer command** so nothing is buried and the queue is the source of truth.

## Key tensions (named by the problem-space review; each now resolved)

- **Write-discipline (C5/NG5):** agents must not write terminal bead state. → **D4**: the durable home is an **event-log projection**; any bead marker is **daemon-written only**.
- **Anti-burial vs. agent death:** a blocking agent that dies would orphan its decision, re-burying it. → **G6/S7**: orphaned decisions are **reaped to `withdrawn`** by the daemon/keeper.
- **Blocked vs. hung (C6):** a blocked-on-decision agent must not be reaped as a hang. → session-keeper **consults the open-decision projection** before reaping.
- **Idempotency (C7):** resolution keyed on `decision_id`, idempotent beyond per-event dedupe; single-human v1.

The analysis confirms the work is a **thin, additive layer** on the existing bus — no new transport, datastore, or always-on service (C1/C3/C4). The risk surface is entirely in the lifecycle/edge-cases, which the component decomposition (`03-components.md`) isolates.
