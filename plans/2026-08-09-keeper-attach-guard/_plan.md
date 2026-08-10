# Plan: keeper — the operator-present guard throws away a written handoff

## Objective

Stop the keeper from cancelling a context restart after the agent already wrote a good handoff,
and make the reason visible when the keeper does decide to wait.

## Status

implemented; live smoke pending

## Done means...

1. A cycle whose handoff lands on time always reaches `/clear`, even when the operator becomes
   active during the wait. Verified by the scenario test in **hk-yzsef**.
2. `pollAwaitingHandoff` reads the handoff file on every tick. The operator guard covers only the
   calls that send keys to the pane. Verified by unit tests added under **hk-f7k1r**.
3. A cycle that the keeper holds for the operator ends in a *parked* outcome, not an abort. The
   terminal event names the real reason and never says `handoff_timeout` when the handoff exists.
   Verified by unit tests added under **hk-5kc14** and **hk-osl2r**.
4. The in-cycle "is the operator here" question has exactly one definition, and that definition is
   a real operator message inside `keeper.cadence.operator_turn_lookback`. Verified by **hk-n1y46**.
5. `CycleState.LastOperatorAttachedEmit`, `CyclerConfig.OperatorAttachedSampleInterval` and
   `defaultOperatorAttachedSampleInterval` are gone. Verified by `grep` returning nothing and by
   `make full` staying green. **hk-xopn9**.
6. An operator who sees no restart can run one command and learn why. Verified by hand against a
   live keeper. **hk-vlpa4**.
7. Smoke test GREEN: on a live lane, hold the pane open, let the context cross the act threshold,
   watch the handoff prompt land, keep reading the pane, and confirm the restart still happens.

## The incident

Lane alpha, cycle `cyc-20260806T141756-000015`, 2026-08-09 local time.

| time | what happened |
|---|---|
| 21:33:47 | the keeper injected `/session-handoff …/HANDOFF-alpha.md` with the cycle marker |
| 21:34:50 | the agent wrote `HANDOFF-alpha.md`. The marker is on line 2 and it is correct |
| 21:36:49 | the keeper aborted with reason `handoff_timeout`. No `/clear`. No restart |

The event log holds only `session_keeper_handoff_started` and `session_keeper_cycle_aborted` for
this cycle. Cycles 11 through 14 on the same lane completed normally, so the setup is sound and the
failure is intermittent.

The keeper wrote nothing to its log pane about the decision.

## Root cause

`internal/keeper/shell.go`, `pollAwaitingHandoff`.

The tick starts by asking whether an operator is attached to the pane. `OperatorAttached` in
`internal/keeper/tmuxresolve.go` runs `tmux list-clients -F '#{client_activity}'` and returns true
when any client had keyboard activity in the last 5 minutes. When that is true the tick does two
things:

1. It returns **before** it reads the handoff file. The marker is never seen, so `EvNonceObserved`
   never fires.
2. At the deadline it skips `sampleHandoffFreshness`. That function exists to rescue exactly this
   case — a handoff that is on disk and newer than the injection instant. Skipping it removes the
   last chance to notice the file.

The cycle then falls to `stepAbort`, the one path that never sends `/clear`.

The timing shows the guard flipped in the middle of the wait. Gate 7 in `internal/keeper/step.go`
refuses to open a cycle at all while the operator is attached, so the operator was quiet at
21:33:47 and became active before 21:36:49. Reading the pane after the prompt appears is enough.
`client_activity` does not advance on pane output — sampled every 20 seconds for two minutes on a
live idle client and it never moved — so the trigger really is human input, including input that is
only a scroll or a focus change.

## Why this is worse than one lost restart

- The agent keeps running with a full context. That is the condition the keeper exists to prevent.
- The event says `handoff_timeout`. That is false and it is the only record. Anyone reading the
  event stream will look for a slow agent and find nothing wrong.
- `stepAbort` increments `ConsecutiveHandoffTimeouts`. Three of these above the force threshold
  call `ForceRestartFn`, which kills and respawns the agent. A guard meant to protect the operator
  therefore marches toward the most destructive action in the system.
- Nothing is logged. The Gate-7 emission is a deliberate no-op, so the real reason is invisible.

## Architectural issues behind it

**1. One guard is doing two unrelated jobs.** Observing a file is safe. Sending keystrokes is not.
The code guards both with one branch, so protecting the operator's keystrokes also blinds the
keeper. Split them. Reading must never be suppressed.

**2. A timeout is used to mean two different things.** `handoff_timeout` currently covers "the agent
never wrote a handoff" and "we chose not to act". The first is a failure and should count toward
escalation. The second is a decision and must not. Give the second its own outcome and let the
cycle stay parked with its handoff intact.

**3. There are two definitions of "the operator is present".** Gate 5d asks whether a real operator
message landed inside `operator_turn_lookback`, which this project already sets to 5m in
`.harmonik/config.yaml`. The in-cycle recheck asks whether a terminal client sent any bytes. The
second answers a different question and answers it wrong — a person reading output looks identical
to a person typing. The transcript probe is the better proxy and it already exists. Use one
definition.

**4. Silence on a destructive path is not diagnosable.** The no-op emission decision (logmine
TA3/F55) was about event volume and should stand. A one-line WARN per held cycle is not event
volume, and without it this incident leaves no trace at all.

**5. Dead machinery around the no-op.** `gateOperatorAttachedSample` maintains
`CycleState.LastOperatorAttachedEmit` to throttle an emission that no longer happens. Nothing reads
the field. `OperatorAttachedSampleInterval` and `defaultOperatorAttachedSampleInterval` exist only
to feed it. Delete all of it.

## Cleanup worth doing while in there

Keep these small. Do not restructure the state machine.

- Delete the dead Gate-7 sampling state (**hk-xopn9**).
- The `pollAwaitingHandoff` comment block is longer than the function and describes the behavior
  this plan changes. Rewrite it to match the new split.
- `sampleHandoffFreshness` carries a long invariant note that is still correct. Keep the note.
  Confirm the `mtime >= handoffInjectedAt` compare still holds after the split, because it becomes
  the primary path rather than the recovery path.
- `internal/keeper/watcher.go` is 2110 lines and `internal/keeper/step.go` is 1195. Do not split
  them in this plan. Note it and move on.

## Out of scope

- Any change to the thresholds, the poll interval or the handoff timeout.
- Any change to the warn delivery path.
- Splitting the large keeper files.

## Beads

| bead | title |
|---|---|
| hk-9pa7b | umbrella: operator-attached recheck discards a written handoff and aborts the restart |
| hk-f7k1r | split handoff observation from keystroke injection in the wait loop |
| hk-5kc14 | park the cycle instead of aborting when the operator is active and a handoff exists |
| hk-n1y46 | use the transcript operator-turn probe for the in-cycle recheck |
| hk-osl2r | make the suppression visible in logs and events |
| hk-xopn9 | remove the dead Gate-7 sampling state |
| hk-yzsef | scenario test — operator reads the pane during the handoff wait |
| hk-vlpa4 | keeper doctor reports a parked or held cycle |

## Evidence

- `.harmonik/keeper/alpha.cycle` — `{"cycle_id":"cyc-20260806T141756-000015","phase":"aborted","opened_at":"2026-08-10T04:33:47Z","updated_at":"2026-08-10T04:36:49Z","reason":"handoff_timeout"}`
- `HANDOFF-alpha.md` — mtime `2026-08-09T21:34:50-0700`, marker `<!-- KEEPER:cyc-20260806T141756-000015 -->` on line 2
- `.harmonik/events/events.jsonl` — `session_keeper_handoff_started` then `session_keeper_cycle_aborted` for cycle 15, with no `session_keeper_handoff_written` between them
- `tmux list-clients -t hk-alpha -F '#{client_activity}'` — frozen across a two-minute idle sample

## Ruled out

- The six `.harmonik/keeper/alpha.hold.*` markers. All expired on 2026-08-07.
- A handoff path mismatch. `handoffFilePathForAgent` builds `HANDOFF-<agent>.md`, and the injected
  prompt and the poll read both go through `HandoffPath()`.
- A dead watcher. Process 49870 has run since 2026-08-06 and completed four cycles on this lane.
- The boot-time doctor gaps. The keeper re-resolved `.managed` five times since and self-healed.
