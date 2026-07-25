# Review-loop event truth and ordering design

## Current state

Current characterization includes dispatch-intent `reviewer_launched`, false/mismatched review identities,
no positive input terminal in JSONL, discarded F-emission errors, two cancellation paths without
cycle-complete, unclassified ordinary `implementer_phase_complete`, and callback ready events without
identity payload. These defects are pinned by a current oracle rather than blessed.

## Target state

Amend Event Model:

- adopt existing `implementer_phase_complete` as an O daemon event with its current payload; define it as
  logical work-result publication, not quiescence;
- define `reviewer_launched` as dispatch committed before `Handler.Launch`, so launch failure may follow
  without `launch_initiated`;
- source reviewer launch/verdict identities from reviewer launch artifacts and resumed implementer ID from
  its launch artifacts;
- require callback `agent_ready` payload identity;
- restrict cap final verdict to actionable `REQUEST_CHANGES`;
- preserve raw flagless request-changes in verdict while cycle-complete records normalized approval;
- require F producers to propagate append/fsync failure into the existing durability/quarantine path;
  O failure remains explicit best effort;
- add no new events; use existing `agent_input_acked`/`agent_input_stale`.

Define the target partial-order oracle:

- run-start first;
- optional resumed intent -> launch -> ready -> input terminal -> implementer logical completion;
- reviewer dispatch -> reviewer launch -> ready -> input terminal -> verdict;
- request-changes with room -> next resume;
- approve/block -> cycle-complete -> terminal run event;
- cap -> cap-hit -> cycle-complete -> failure;
- fix-up stall -> stalled -> cycle-complete -> failure, no reviewer;
- malformed/missing/launch failure -> no verdict, cycle-complete(error), failure.

Require envelope/payload run-ID agreement, monotone iterations, per-launch handler IDs, stable implementer
continuity, fresh reviewer continuity, and exactly one successfully persisted cycle-complete for every
entered cycle, including error/cancellation outcomes. Event-substrate failure is classified separately as
a durability failure rather than a phantom cycle completion. No cycle event follows completion, and one
input terminal exists per delivered submission.

Stage current characterization/pure extraction first, then identity/cancellation/F-error correction, then
input-terminal wiring, then enable the amended oracle.

## Rationale

Separating dispatch intent from actual launch preserves causality; artifact-derived identity makes traces
joinable; explicit F failure gives durability meaning; logical completion preserves order while C4 owns
quiescence.

## Traceability

- Component C6; Event Model durability, lifecycle/review taxonomy, payloads, ordering, conformance.
- Dependencies: C1 routing, C2 watcher/ready, C4 logical-result boundary.
- Non-goals: no new event type or silent event reordering.
