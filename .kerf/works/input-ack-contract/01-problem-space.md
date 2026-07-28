# Problem Space — input-ack-contract

Task: `INPUT-ACK-CONTRACT-01` (plan `2026-07-24-code-health-audit`).
Work type: bounded normative drift correction. No production implementation.

## What is wrong

Three owner/consumer specs disagree about what a synchronous input `Ack` means.

1. **`specs/run-state-machine.md` RSM-027** told the run reactor to honour a
   *three-valued acceptance class* of `Ack` — `Accepted` / `Rejected` /
   `Degraded` — and derived behaviour from it (`Accepted` advances the
   dispatch; a "never-confirmed `Degraded`" feeds the fail-closed edge).
   No such type exists. `Ack.Outcome` is binary.

2. Five sentences inside the two **owner** contracts still call the
   *synchronous* `Ack` a "positive acceptance signal", contradicting the `Ack`
   record and the async-event contract stated a few paragraphs away in the same
   files:
   - `specs/agent-input.md` — the §3 `front-stop` glossary entry;
   - `specs/agent-input.md` — AIS-005 "Front-stop, not replacement";
   - `specs/handler-contract.md` — HC-070 "Front-stop composition, NOT replacement";
   - `specs/handler-contract.md` — HC-056 "Front-stop composition (HC-070)";
   - `specs/handler-contract.md` — HC-057 "Front-stop composition (HC-070)".

The contradiction is load-bearing, not cosmetic: `ARCH-01` (run-architecture
contract) is blocked on it — see
`.kerf/works/run-architecture-contract/04-design/c2-process-session-lifecycle-design.md`
("The exact Ack shape is intentionally not stated here: HC-070 and RSM-027
contradict each other") and `change-design-review.md` Round 2.

## What must be true afterwards

- Synchronous `Ack.outcome` is binary: `Delivered | Rejected`.
- `Delivered` confirms **driver handoff**, not positive agent acceptance.
- Positive acceptance is the **asynchronous `agent_input_acked` event**.
- Absence of positive acceptance terminates as **`agent_input_stale`**.
- The input sequence (`input_seq`) correlates the synchronous handoff with its
  asynchronous terminal.
- Every input-seam observation has a defined route (totality).

## Goals

- Delete the three-valued model and everything derived from it in RSM.
- Make the five stale owner-spec restatements say "delivery handoff", while
  preserving their actual rule: input delivery composes **in front of** and does
  **not** replace the readiness, heartbeat, or commit watchdogs.
- Preserve ownership: AIS owns the port, `Ack`, async events, timer, and the
  output-or-stale definition; RSM owns only reactor consumption and routing.

## Non-goals / must-not-change

- The `Ack` record in either owner spec.
- Event payload schemas or their registration (`event-model.md` owns those).
- Timer semantics or the bounded window value (AIS-INV-001 / HC-INV-008).
- Production Go, tests, the spec registry, other task cards, `TASK-INDEX.yaml`.
- No second timer may be introduced in the consumer.

## Constraints

- RSM-027 must be amended in place, not renumbered.
- Diffs in AIS and HC are limited to the leased prose, directly dependent
  cross-reference/conformance wording, version metadata, and one revision row.

## Success criteria

- RSM contains no live `Accepted` or `Degraded` Ack outcome.
- Four total routes are stated: `Delivered` → await correlated async terminal;
  `Rejected` → fail closed; `agent_input_acked` → acceptance path;
  `agent_input_stale` → fail closed.
- No wording anywhere treats a successful tmux write as positive acceptance.
- RSM does not redefine the input timer or event payload.
