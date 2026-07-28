# C2 research — Process/session lifecycle

## Questions

1. Which contract owns each lifecycle edge?
2. What lifecycle path is duplicated in production?
3. Which differences among single, review, and DOT are semantic?
4. What survives restart?
5. Which existing leaves and staged owners are reusable?

## Findings

### Exact normative split

| Edge | Owner |
| --- | --- |
| Handler selection/launch and generation-local idempotency | HC-001, HC-003, HC-003a, HC-004 |
| `Session` lifetime and state | HC-002, HC-064–HC-067 |
| One mode-neutral watcher, progress observation, event publication | HC-007, HC-011, HC-011a, HC-INV-001, HC-INV-007 |
| Genuine ready before work | HC-039, HC-056, HC-INV-004 |
| Narrow input seam | HC-069–HC-071, HC-INV-008; RAC stops at `InputPort` |
| Bounded handler cancellation | HC-018 |
| OS parentage, exactly-once raw wait/reap, shutdown signal/kill | PL-011, PL-012, PL-014, PL-016, PL-021b, PL-INV-005 |
| Startup/orphan/restart composition | PL-005, PL-006, PL-024, PL-025, PL-INV-003 |
| Event ordering/durability/non-authority | EV-008, EV-015–EV-018, EV-021, EV-022, EV-024, EV-INV-001 |

Behavior below `InputPort` remains owned by `specs/agent-input.md` and is not
part of RAC.

### Repeated production lifecycle

Single, review, and DOT paths repeat:

```text
build launch facts
-> register hook session
-> launch Session/watcher
-> install ready callback
-> wait ready
-> send mode input
-> observe watcher/process and hook outcome
-> close hook window
-> cancel/kill/wait as needed
-> stop auxiliaries
-> force teardown
```

The reusable leaves are `handler.Handler`, `handler.Session`,
`handlercontract.Watcher`, `lifecycle.WaitOwner`,
`runloop.DispatchSegment`, `runloop.WaitWithSocketGrace`,
`runlaunch.ForceTeardownSession`, `hook.SessionStore`, and
`runloop.PerRunEventTap`.

RL-01 R3 adds reviewed `SessionRegistration` and owned `EventSubscription`
handles, but production callers still discard them through compatibility
APIs. Substrate-neutral watcher lifetime, joinable auxiliaries, bounded
`PhaseScope.Close`, and sleep removal remain absent.

### Mode differences that must remain policy

- Single mode alone may detach an independent tmux session and worktree for
  restart adoption instead of terminating them on daemon shutdown.
- Review mode has separate implementer/reviewer phase windows, resume versus
  fresh-launch policy, phase-specific watchdogs, and bounded iterations.
- DOT owns per-node lifecycle; reviewer heartbeats deliberately do not extend
  implementer progress, and cognition-gate cleanup has explicit ordering.
- DOT currently contains unbounded reap waits where single/review use
  `runlaunch.KillReapTimeout`. This is a nonconforming/legacy migration state,
  not a behavior to normalize silently.

The shared lifecycle owner should standardize resource lifetime, not mode
policy. It needs explicit `terminate-and-join` and narrowly authorized
`detach-for-restart-adoption` shutdown dispositions.

### Restart and partial observation

Durable Run, queue, git, and Beads facts survive. `Session`, watcher, hook
registration, subscriptions, callbacks, and goroutines are generation-local.
Events cannot reconstruct them under EV-INV-001.

Current `adoptLiveRunSession` observes tmux existence only. It does not restore
the watcher, input path, or phase result; disappearance causes durable recovery
and re-dispatch. `WaitWithSocketGrace` separately observes process exit and a
bounded late hook outcome. The target result must preserve those separate
facts.

### Normative/code drift

PL-014/PL-016 assign raw `cmd.Wait` to the watcher, while production
`handler.session.runWait` uses `lifecycle.WaitOwner` and the watcher reads
progress independently. Handler-contract §6.1 signatures also differ from
production Handler/Session signatures.

RAC must cite the reviewed specs as target authority and record current code as
a nonconforming migration state. It must not amend either owner or bless the
drift.

### Blocking Ack-contract contradiction

HC-069/HC-070 and handler-contract §6.1 define synchronous
`Ack.outcome = Delivered | Rejected`, with positive acceptance represented by
the later `agent_input_acked` event. RSM-027 instead requires synchronous
`Accepted | Rejected | Degraded` and assigns a distinct liveness rule to
`Degraded`. Both are direct RAC dependencies; their shapes cannot be composed.

RAC cannot select either vocabulary. A coordinated owner-spec amendment must
resolve HC-069/HC-070, RSM-027, and the cited
AIS-003/AIS-004/AIS-INV-001 semantics before RAC spec drafting. Until then,
architecture may state only that lifecycle consumes the eventual reviewed
`InputPort`/Ack contract.

## Pattern, risks, and decision status

Use a constructor-complete phase scope retaining session, watcher/join,
registration, subscriptions, `InputPort`, auxiliaries, cancellation, and
ordered cleanup. Return a mode-neutral observation result. Never expose
`*exec.Cmd` or a cleanup callback bag to mode code.

The normative/code drift needs an executable companion migration proposal.
The Ack contradiction is a pre-draft blocker and needs a coordinated
owner-spec decision; neither may be resolved by RAC prose.
