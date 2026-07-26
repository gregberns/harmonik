# Event truth and ordering findings

## Questions

1. Which specs own event truth and cycle decisions?
2. What is the current observable partial order?
3. Where do identities, durability, or completion diverge?
4. What should a pure trace oracle and event port cover?

## Normative owners

- `specs/event-model.md` owns event names, payloads, identities, durability, and §8.1a causal order.
- `specs/execution-model.md` EM-015d/e owns when cycle facts occur and terminal routing.
- Handler, Claude bridge, and Agent Input specs own actual handler identity, ready, session continuity, and input acceptance.

One `run_id` spans the cycle. Each OS launch has its own handler `session_id`; implementer Claude continuity reuses one Claude session, while reviewers are fresh. `reviewer_verdict` and `review_loop_cycle_complete` are durable F events; most routing/intention events are O.

## Current partial order

Ignoring heartbeats and unrelated diagnostics:

- iteration 1 implementer: `run_started -> pre-exec facts -> launch_initiated -> agent_ready -> input attempt -> implementer_phase_complete`;
- later iterations prepend `implementer_resumed`;
- reviewer: pre-exec facts -> `reviewer_launched` -> actual launch -> `launch_initiated -> agent_ready -> input attempt -> reviewer_verdict`;
- approval/block: verdict -> cycle-complete -> terminal run event;
- cap: verdict -> cap-hit -> cycle-complete -> run-failed;
- stalled fix-up: implementer phase-complete -> fix-up-stalled -> cycle-complete, with no reviewer.

`reviewer_launched` is currently dispatch intent emitted before `Handler.Launch`; `launch_initiated` is proof that a live process/window exists. Positive tmux/Claude delivery is not currently visible in project JSONL: the dispatch machine synthesizes an internal input ACK when it starts asynchronous paste. Existing `agent_input_acked`/`agent_input_stale` types should be wired rather than adding an event.

## Drift

1. Review-loop helpers mint unrelated handler IDs and place the implementer's Claude ID on reviewer events instead of using reviewer launch artifacts.
2. F-event errors are discarded, allowing routing to continue after append/fsync failure.
3. Two cancellation returns omit the required exactly-once cycle-complete.
4. `implementer_phase_complete` has real consumers but no Event Model taxonomy row; comments call it F while code treats it as O.
5. Execution and Event models disagree on HEAD versus diff-hash progress, `review_fixup_stalled`/`fixup_stalled`, event count, and flagless `REQUEST_CHANGES`.
6. `reviewer_launched` naming is ambiguous between dispatch intent and actual launch.
7. Some `agent_ready` callback emissions have no identity payload, preventing correlation.

## Minimal amendments

- Align EM with shipped HEAD-advance and fix-up-stalled behavior and explicitly define flagless `REQUEST_CHANGES`.
- Define `reviewer_launched` as logical dispatch committed before launch, but require its IDs and `reviewer_verdict` IDs to equal the reviewer launch artifacts; do the same for resumed implementer handler identity.
- Adopt the already-existing `implementer_phase_complete` in Event Model and explicitly preserve its current O durability unless a deliberate durability change is approved.
- Fix the conformance event count and add identity, run-ID equality, F-write failure, cancellation, and delivery-terminal coverage.
- Add no new event type.

## Ordered trace oracle

Maintain two explicit oracles.

The **current characterization oracle** records shipped behavior, including dispatch-intent
`reviewer_launched`, missing positive input-acceptance events, best-effort F-helper errors, identity
drift, and the two cancellation paths that omit cycle-complete. Those defects are frozen as failing
target-conformance cases; extraction must not accidentally introduce additional drift.

The **amended target oracle** evaluates this partial order per run and iteration, ignoring
heartbeat/diagnostic interleavings:

- `run_started` first;
- `[implementer_resumed for N>=2] -> launch_initiated -> agent_ready -> input accepted/stale -> implementer_phase_complete`;
- when continuing, phase-complete -> reviewer dispatch -> reviewer launch -> ready -> input terminal -> verdict;
- `REQUEST_CHANGES` with room -> next implementer resume;
- approve/block -> cycle-complete -> terminal run event;
- cap -> cap-hit -> cycle-complete -> run-failed;
- stalled fix-up -> fix-up-stalled -> cycle-complete, with no reviewer;
- malformed/absent verdict -> no verdict event, exactly one cycle-complete(error);
- every envelope/payload run ID agrees, iterations are monotone, exactly one cycle-complete occurs, and no cycle event follows it.

## Boundary and tests

Keep cycle decisions pure:

`CycleState + Observation -> Decision{next phase, terminal result, ordered cycle intents}`.

The kernel owns only routing facts and payload values. A narrow event port enriches phase intents with actual launch artifacts, serializes emission, and returns errors. F failures propagate to the run/quarantine path; O policy is explicit best effort.

Stage work in this order:

1. Land current-characterization traces and the pure policy projection without changing I/O behavior.
2. Correct identity/cancellation/F-error behavior under the amended specs.
3. Wire the already-defined input accepted/stale terminal events as a separate corrective slice.

Tests should include exhaustive decision/property tables, both trace oracles, cross-event identity
coherence, F-emitter fault injection, fsync classification, input accepted/stale terminals, launch
failure after reviewer dispatch intent, cancellation, and the stale scenario test that still expects
`no_progress_detected/no_progress`.
