# Reviewer budget and activity findings

## Questions

1. Is the reviewer allowance a registered ControlPoint budget?
2. Which facts currently extend the deadline?
3. What does heartbeat prove?
4. Which policy can be pure and which effects need ports?

## Ownership

The diff-scaled reviewer verdict allowance is daemon-local watchdog and artifact policy in `internal/daemon/pasteinject.go`, not state in the ControlPoint Registry. It therefore belongs with review-loop execution semantics and Process Lifecycle, with Event Model owning the existing diagnostic event.

Handler Contract HC-026a/HC-057 owns heartbeat as session-liveness evidence. `internal/runexec/dispatch.go` RSM-006 already enforces that a bare daemon heartbeat is not work progress. Control Points needs no amendment for this budget.

## Current behavior

- Base allowance is `reviewFileTimeout` plus `reviewFilePerKLineBudget` per changed kLOC, capped by `reviewFileHardCeiling`.
- The watchdog observes verdict-file creation, pane/process liveness, and `agent_heartbeat`.
- At the base deadline, recent heartbeat or live pane can extend the deadline. Continuous heartbeat is bounded by `min(2 * budget, hard ceiling)` after hk-4u1mb.
- When the deadline is exhausted, the watchdog writes `reviewer-budget-exceeded.json`, sends quit/kill, and the coordinator later reads the marker and emits `reviewer_budget_exceeded`.
- The watchdog is launched as an unowned goroutine with a run-wide context and an event-tap subscription that cannot be unsubscribed or joined.

Current tests document the repaired but still mixed semantics: heartbeat may extend the wait as evidence that the session is alive/reasoning, yet it cannot extend indefinitely to the flat hard ceiling. This is not the same as proving reviewer work-product activity.

## Required semantic clarification

Preserve behavior while naming the two clocks:

1. **Work allowance** is derived from diff size and ends when the durable verdict appears. This work does
   not invent intermediate reviewer-progress evidence.
2. **Liveness grace** may defer termination of a live reasoning session, but is independently capped and does not reset or replenish work allowance.

Heartbeat, whether handler- or daemon-emitted, is only liveness evidence. Pane liveness is also only liveness evidence. Neither should be described as work progress or an activity-reset budget. The existing bounded `2 * budget` extension is a grace ceiling, not additional earned work allowance.

## Pure boundary and ports

Extract a pure budget transition:

`BudgetState + BudgetObservation + Now -> BudgetDecision`

Inputs include base/hard/grace deadlines, changed-line count, verdict presence, recent heartbeat, pane liveness, and terminal state. Outputs are `continue`, `extend_liveness_grace(until)`, `complete_verdict`, or `exhaust(reason, sentinel fields)`.

Effects remain behind narrow adapters:

- clock/timer;
- verdict/artifact observation;
- liveness observation;
- sentinel write;
- quit/kill;
- diagnostic event emission.

The phase lifetime scope owns and joins the watchdog and its subscription. The budget kernel does not know about goroutines, channels, filesystem paths, tmux, or the event bus.

## Tests needed

- table/property tests for diff scaling, clamping, monotone deadlines, and all decision branches;
- heartbeat and pane liveness never mutate work allowance;
- continuous heartbeat stops at the liveness-grace ceiling;
- verdict at every deadline boundary wins deterministically over exhaustion;
- fake-clock tests with no sleeps;
- handler- and daemon-emitted heartbeat produce identical liveness decisions;
- observer cancellation closes `Done`, unsubscribes, and occurs before reviewer-worktree cleanup;
- concurrent verdict arrival, timer expiry, and kill produce one terminal decision and one sentinel;
- sentinel fields and `reviewer_budget_exceeded` payload agree;
- local reviewer-projection placement is explicit, with no accidental worker runner use.

## Risks

- Renaming the existing extension as “activity” would contradict RSM-006 and invite future deadline resets from synthetic daemon heartbeats.
- Removing the bounded liveness grace during refactor would change shipped behavior.
- A generic watcher port that combines liveness, verdict I/O, kill, and event emission would preserve the current dependency knot instead of isolating it.
