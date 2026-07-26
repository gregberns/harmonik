# Reviewer verdict allowance and liveness evidence design

## Current state

The reviewer watchdog is daemon policy, not a registered ControlPoint budget. It derives an initial
allowance from a base plus changed-kLOC, capped by a hard ceiling, then permits bounded extensions from
pane liveness or recent heartbeat. Exhaustion writes a sentinel and later emits
`reviewer_budget_exceeded`. Comments often call heartbeat “activity,” despite HC-057 and RSM-006 defining
it as liveness, not work progress.

## Target state

Add an EM-015d reviewer-verdict-allowance clause rather than changing Control Points. Preserve shipped
defaults, formula, extension increments/ceilings, start point, and terminal reasons unless a later reviewed
timing change says otherwise.

Define:

- **Diff allowance:** initial size-derived window.
- **Liveness grace:** independently capped postponement while the session remains live.
- **Reviewer-work activity:** affirmative agent-derived work evidence.

Heartbeat from either emitter and pane presence are liveness only. They do not reset allowance or count as
work activity. Add no speculative progress signal.

Specify a pure policy over configuration, changed lines, time, verdict presence, pane liveness, heartbeat
recency, and reseed state. It returns continue, extend-to-deadline, reseed-once, verdict-complete, or
budget-exceeded with diagnostic values.

The imperative observer owns timers, subscription, verdict I/O, pane probe, reseed, quit/kill, sentinel,
cancellation, and join. Keep separate shipped-characterization and amended-semantic traces.

Document the existing `reviewer_budget_exceeded` type in Event Model without making it a ControlPoint
budget. DOT reuse is outside this work.

## Rationale

This preserves production timing while removing the semantic fiction that heartbeat proves reviewer work
and isolates deterministic policy from process/filesystem/event effects.

## Traceability

- Component: C7; EM-015d, HC-057, RSM-006, CP-022–026 exclusion, Event Model diagnostic, C4 lifetime.
- Tests: formula/clamps, unknown diff, liveness ceilings, emitter equivalence, no work-progress mutation,
  verdict races, reseed, sentinel/event agreement, cancellation/join.

