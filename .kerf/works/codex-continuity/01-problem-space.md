# Problem space: typed Codex continuity keeper

## Change

Build one Codex continuity vertical. It keeps a crew moving after a stopped
turn. It also pauses when the crew reports a durable blocked or no-assignment
state.

The current shell pilot proves that an attached Codex tmux session can receive
a continuation. It is not a production design. It reads assistant prose,
reimplements tmux delivery, and has no controller-owned state.

## Goals

- The framework does not read or interpret assistant prose.
- A model reports its work status through a typed command or an existing typed
  decision event.
- One controller owns each continuity state transition.
- The policy is a pure typed reducer.
- Harness adapters only decode lifecycle signals and deliver typed input.
- The first vertical serves an attached Codex tmux crew.
- The same core can later accept Codex notify, Codex App Server, Claude, and
  other harness adapters.
- Delivery reuses an existing typed port. It does not construct tmux commands.
- Delivery, retry, cap, and restart behavior have explicit durable semantics.

## Non-goals

- Do not change Claude context reset behavior in this work.
- Do not make the current shell pilot production code.
- Do not infer completion, blocking, or quality from assistant text.
- Do not let a crew record its own terminal state.
- Do not require the Codex App Server for the first attached-session vertical.

## Constraints

- Follow `PRINCIPLES.md` and `docs/concepts/zero-framework-cognition.md`.
- Keep the current pilot stopped. It is experimental evidence only.
- Reuse the existing `decision_needed` path for decision blocks.
- Keep code changes in `work/codex-continuity`.
- Keep Kerf artifacts in this main-checkout work directory by operator approval.
- Do not alter the shared Kerf link.

## Success criteria

- The policy input types contain no assistant message text.
- A Codex turn-ended event and an accepted work-state snapshot determine the
  next action without I/O.
- An open decision for a crew pauses continuation without a second status path.
- A typed external-block or awaiting-assignment declaration pauses continuation.
- Only an authorized controller records terminal state.
- The current attached Codex source has one narrow adapter.
- Tmux delivery uses a shared typed boundary and has a tested delivery record.
- Replay tests cover duplicate, delayed, reordered, and crash-boundary events.
- A live tmux test proves that a continuation submits without operator Enter.

## Initial design decisions

- Treat an accepted `decision_needed` event as the source of `WaitingDecision`.
- Use explicit controller release events. A decision answer does not infer a
  continuation prompt.
- Model run identity from the first vertical.
- Make cap exhaustion a durable pause and escalation, not another prompt.
- Use at-least-once delivery with a durable prompt claim and a bounded cap.

## Spec areas to inspect

- `specs/hitl-decisions.md`
- `specs/process-lifecycle.md`
- `specs/event-model.md`
- the keeper and harness substrate contracts
