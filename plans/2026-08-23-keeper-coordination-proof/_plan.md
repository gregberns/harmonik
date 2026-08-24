# Keeper coordination proof

Date: 2026-08-23

## Objective

Make the keeper reliable as a complete process. The pure cycle reactor is useful. It is not enough.
The keeper must also prove that its files, hooks, pane writes, process lifecycle, and session changes
coordinate correctly under delay, failure, restart, and reordered events.

Keep the change small. Remove state and duplicate paths where possible. Do not add a second test
framework beside the existing keeper twin.

## Current evidence

The 2026-08-23 alpha incident was a live watcher failure with no process crash:

- The watcher process stayed alive from 2026-08-12.
- The gauge stayed fresh.
- Alpha reached 247,246 tokens. This was above NOTICE, WARN, and HARD.
- A stale `.dispatching` file made `HoldingDispatch` true forever.
- The gate returned without an event or warning.
- WARN stayed silent because watcher threshold math used `pct < warn_pct OR tokens < warn_abs`.
- The hard ceiling ran only in the foreign-session branch. Alpha had a healthy managed session.
- `keeper doctor` could not resolve the explicit `hk-alpha` target and reported a skipped pane check as
  green.
- `restart-now` could not resolve `hk-alpha` without an explicit target.
- The manual restart path queued ACK, `/clear`, and the resume brief while alpha was still active. The
  pane processed the brief before `/clear`.

The current keeper replay gate is green. This does not contradict the incident. Its fault matrix
changes reactor input events. Its effect sink cannot fail. The live smoke uses a bare shell. It passes
when the three input strings appear in the pane. It does not prove that `/clear` ran before the brief.

## Assessment

### What is strong

- `Cycle.Step` is a pure transition function.
- The cycle has typed events, actions, timers, and durable cycle IDs.
- The marked handoff and later Stop rule prevents an unapproved clear.
- The cycle journal supports recovery after process loss.
- The real tmux twin can exercise the status hooks and session ID change.

These parts are worth keeping.

### What is weak

#### 1. There are two controllers

`Cycle` controls the handoff and clear protocol. `Watcher.Run` controls gauge state, NOTICE, WARN,
message delivery, hard ceiling, liveness recovery, and several cooldowns through local booleans and
timestamps. The second controller is not replayed by the keeper simulator.

The incident occurred in this second controller. A strong cycle reactor did not help because the
watcher never entered it.

#### 2. The tests stop before the effect boundary

The L2 sink records requested effects. It does not model effect failure, partial completion, duplicate
completion, delayed completion, or process death between an effect and its observation.

The L3 test proves that text appeared in a shell pane. It does not prove these facts:

- the agent received the message
- the command ran
- the old session ended
- the new session became ready
- the brief landed in the new session
- the watcher recovered after a crash at each boundary.

#### 3. Several actions report success before their outcome exists

The automatic shell ignores pane injection errors. It records `cleared` after requesting `/clear`.
After the clear backstop expires, it injects the brief and records `complete` without observing a new
session. The `restart-now` command injects all three inputs in one synchronous call.

An input write is an attempt. It is not completion.

#### 4. Safety gates can become permanent silent states

`.dispatching` is a manual presence marker with no owner, lease, session binding, or doctor check.
The gate has no blocked event. Similar early returns make an eligible HARD session look idle from the
outside.

Fail-closed is valid for one destructive effect. Fail-closed plus silence plus no expiry is not safe.

#### 5. Message delivery is mixed with destructive-action guards

An attached operator changes the NOTICE into prose that omits `restart-now`. It also suppresses the
second-band message. This violates the operator direction that every agent must receive the exact
self-service command. Operator activity can guard an autonomous clear. It must not hide the recovery
instruction.

#### 6. `restart-now` is a duplicate and unsafe coordinator

The command has its own handoff validation, tmux resolution, dispatch gate, injection order, and event
path. It does not use the automatic cycle tail.

An agent normally runs the command inside its own active turn. The command cannot wait for that turn's
Stop without blocking the turn. Sending `/clear` before the tool call returns is unsafe. This is a
structural conflict, not a timeout tuning problem.

#### 7. Deployment is outside the proof

The running alpha and bravo keepers still use `/tmp/harmonik-keeper-80c5e23ca`, built on 2026-08-12.
Their process being alive does not prove that they run the tested commit. Doctor checks the installed
binary, not the executable held by the watcher process. A deploy can therefore pass tests and leave the
old behavior live.

### What is over-architected

- `WatcherConfig` carries policy, production functions, test callbacks, recovery policy, message
  templates, and unrelated dashboard work.
- Threshold policy exists in both watcher and cycle code. The two implementations already disagree.
- The watcher has a local NOTICE and WARN state machine outside the declared cycle machine.
- `restart-now` implements a second restart transaction.
- The replay corpus preserves old outcomes, including degraded outcomes that should now be considered
  failures. Historical parity is useful evidence. It must not be the safety oracle.
- Test-only callbacks such as `OnPollTickFn` expose scheduler details instead of testing visible
  behavior.

## Target contract

### One request machine and one destructive tail

Use one state model for NOTICE, WARN, HARD request delivery, explicit restart intent, marked handoff,
Stop, clear, session change, and resume delivery. The watcher becomes an event collector and effect
runner. It does not keep a second set of request booleans.

The destructive tail starts only from one of these authorities:

1. a marked handoff followed by a later Stop
2. an accepted `restart-now` request followed by the current turn's later Stop.

Both paths use the same clear, session-change, and brief steps.

### `restart-now` returns before a detached driver clears

`restart-now` must:

1. resolve the project and agent
2. verify a non-empty handoff for the current session
3. verify that a live keeper owns the agent lock
4. start a detached restart driver with the old session ID and exact pane target
5. return so the tool call can end and the agent can stop.

The detached driver gives the active turn a short grace period. It submits `/clear` once. It then
waits for the SessionStart channel to report a different session ID. It submits the resume brief only
after that observation. It fails visibly if session turnover does not occur. It does not submit a
second clear.

The existing marked-handoff plus Stop path remains valid when the command is not run. There is no
receipt-readiness-handoff-clear conversation. The explicit command is one signal and one detached
transaction. Operator activity gates do not block this explicit path.

### Message delivery is independent of operator avoidance

Every NOTICE and WARN message for an eligible primary session must include the exact rendered
`harmonik keeper restart-now --agent <name>` command. Operator attachment and recent operator turns do
not remove the command. They do not suppress the message forever.

Operator activity can defer autonomous destructive effects. It cannot suppress observation, request
delivery, handoff validation, or explicit request acceptance.

### Effects use attempt and observation pairs

Track these pairs:

| Attempt | Required observation |
|---|---|
| keeper message submitted | matching automation envelope in transcript, or explicit unknown receipt |
| `/session-handoff` submitted | marked handoff for the request ID |
| `/clear` submitted | session ID changes from the prior valid ID |
| resume brief submitted | brief envelope appears in the new session transcript |

Do not label the cycle complete before the new session and resume delivery are observed. If the clear
cannot be confirmed, emit `restart_failed` and keep the handoff. Do not inject the brief into an
unconfirmed session.

### Gates are observable

When a session is at or above HARD and a gate blocks progress, emit a throttled state event with:

- agent and session ID
- tokens and band
- gate name
- gate age when it has one
- next retry time
- whether a self-service request remains available.

Doctor must report the same blocking fact. A skipped check is not green.

The manual `.dispatching` marker no longer authorizes an indefinite veto. The handoff and Stop
protocol is the main active-work guard. Keep `.dispatching` as advisory telemetry during migration, or
replace it with a renewable owner-bound lease. Do not keep a permanent presence-only safety gate.

## Failure testing

Extend the existing keeper twin. Do not create a parallel test framework.

### Layer A: deterministic coordination simulation

Run the request machine and effect runner under a fake clock. Each effect port can return an error,
apply the effect without returning success, return success without applying it, apply it late, apply it
twice, or reorder its observation.

Generate event schedules from a seed. Record the seed on failure. Explore at least these dimensions:

- Stop before and after handoff marker
- operator turn before and after request delivery
- stale, missing, partial, and wrong-session files
- watcher crash before and after every durable write and pane attempt
- dropped, delayed, duplicate, and reordered pane input
- delayed session ID change
- gauge absent, stale, foreign, and recovered
- stale dispatch and hold markers
- event-log and journal write failures
- keeper process restart while a request is pending
- configuration reload during each phase.

The oracle checks invariants. It does not trust keeper terminal events as proof.

### Layer B: adversarial tmux twin

Change the existing tmux twin from a cooperative parser into a controllable REPL model. It can stay
busy, queue input, delay Enter processing, reorder queued lines, drop one line, restart slowly, and
delay status hooks.

Run the real watcher process and real `InjectText`. Assert from independent files and pane capture that:

- no `/clear` runs before authority
- no brief runs before a new session
- one accepted request causes at most one clear
- a crash at every phase converges after restart
- a stale gate produces an alarm rather than silence
- `restart-now` returns only after the watcher accepted the request
- the later Stop causes the common tail to complete.

This tier must run in the keeper pre-deploy gate on hosts with tmux. A skipped live prerequisite must
make the deploy gate fail.

### Layer C: seeded soak and mutation checks

Run thousands of virtual schedules with bounded steps. Keep a small committed corpus of every found
failure. Add mutations that remove these load-bearing operations:

- watcher acceptance write
- handoff marker check
- Stop-after-request check
- session-change check
- brief receipt check
- blocked-gate event
- retry after a failed effect
- executable provenance check.

The harness must fail for each mutation. This proves that it can detect missing coordination, not only
that the production code passes.

### Layer D: deploy proof

Build one named binary from the reviewed commit. Start isolated alpha-like and bravo-like panes with
that binary. Record executable path, digest, commit, PID, target, and config digest. Kill and restart the
watcher once. Drive one full session cycle. Only then replace the live keepers.

After replacement, doctor must compare the live process executable digest with the intended binary.

## Work plan

### 1. Amend the normative contract

- Remove handoff age as `restart-now` authority. Bind the request to the current session instead.
- Require the exact self-service command in NOTICE and WARN regardless of operator activity.
- Make `restart-now` a durable accepted request followed by Stop.
- Forbid a brief and `cycle_complete` before session change is observed.
- Replace silent HARD gate returns with a throttled blocked-state event.
- Remove `.dispatching` as a permanent presence-only veto.

### 2. Add the failure harness before changing the coordinator

- Add effect-fault modes to the current L2 runner.
- Add the adversarial busy-pane and reordered-input cases to the current tmux twin.
- Add independent filesystem and transcript oracles.
- Prove the current `restart-now`, stale dispatch, and clear-unconfirmed paths fail.

### 3. Unify explicit and automatic restart coordination

- Add the durable accepted-request record.
- Route watcher acceptance and later Stop into the cycle reactor.
- Delete the direct ACK, `/clear`, brief burst from `RestartNow`.
- Use the existing automatic destructive tail.
- Remove the 10-minute handoff freshness window.
- Resolve the target from the live keeper record. Keep `--tmux` as an explicit diagnostic override.

### 4. Move request delivery into the reactor

- Represent NOTICE and WARN delivery state in `CycleState`.
- Emit message attempts as actions.
- Remove watcher-local `warnArmed`, `warnFired`, `pendingInject`, `settleWarnFired`, and related test
  callbacks.
- Use one threshold implementation from `CyclePolicy`.
- Keep operator guards on destructive effects only.

### 5. Make every unsafe wait observable

- Add blocked-state and restart-failed events.
- Add doctor checks for stale blockers, actual tmux target, pending request, last successful cycle, and
  live executable provenance.
- Evaluate the hard ceiling for every fresh gauge, not only foreign sessions.

### 6. Remove obsolete mechanisms

- Remove the manual presence-only dispatch veto after callers stop depending on it.
- Remove `HandoffFreshnessWindow`.
- Remove direct `RestartNow` injection orchestration.
- Remove duplicate watcher threshold math and warn state.
- Reclassify old degraded corpus outcomes as expected historical failures. Do not preserve them as
  acceptable completion.

### 7. Verify and deploy

- `make test-keeper-l012`
- the deterministic seeded fault campaign
- the required real-tmux coordination gate
- `make fast`
- `make full`
- isolated binary provenance and restart proof
- live alpha and bravo replacement only after all gates pass

## First implementation slice

The first slice is deliberately narrow:

1. Add failing adversarial tests for the live incident and unsafe `restart-now` ordering.
2. Amend `specs/session-keeper.md` for message delivery, explicit request authority, and confirmed
   session turnover.
3. Remove the handoff age rejection.
4. Ensure NOTICE and WARN always carry `restart-now`, independent of operator presence.
5. Make stale dispatch blocking visible while the durable accepted-request path is built.

This slice produces evidence before it changes the restart transaction.
