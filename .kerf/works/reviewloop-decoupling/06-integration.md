# Integration review

## Scope and method

The integration pass examined every Markdown spec reachable under `specs/`, not only the eleven drafted
files. It performed:

- full-file diff and whitespace checks for every draft against its current source;
- Markdown cross-file link resolution across all drafts (`missing=0`);
- requirement/enum searches for review-loop, continuity identity, reviewer artifacts, phase lifetime,
  input ACK, heartbeat, transition kinds, needs-attention, and terminal events;
- targeted semantic reads of Architecture, Harness, Agent Input, Control Points, Workflow Graph,
  Run State Machine, Operator NFR, Beads Integration, Claude bridge/launch, Pi harness,
  Reconciliation/Schemas, Queue, remote/workspace, and lifecycle specifications;
- changelog/version/acceptance-bead reconciliation.

The eleven modified full-file drafts are:

`execution-model.md`, `event-model.md`, `handler-contract.md`, `process-lifecycle.md`,
`workspace-model.md`, `run-state-machine.md`, `operator-nfr.md`, `beads-integration.md`,
`claude-hook-bridge.md`, `claude-launchspec.md`, and `pi-harness.md`.

## Cross-reference checks

- Execution `TransitionKind` and Event Model now both contain `context-checkpoint`; the transition keeps
  `from_state_id == to_state_id`, emits checkpoint/transition facts, and emits no state exit/entry.
- Execution continuity uses Harness Contract's exact `Minted`/`Captured` policy vocabulary.
  `claude_session_id` is explicitly compatibility-named generic continuity state; handler `session_id`
  remains per launch.
- The Minted critical message is named `version_selected`. `HookRelayAck` and Agent Input `Ack` remain
  separate types.
- Handler, Claude bridge/launch, and Agent Input agree on spawn/launch, genuine deduplicated
  SessionStart-ready, then input resolution.
- Input resolution is internally three-way: synchronous Rejected, positively accepted, or stale.
  `agent_input_acked`/`agent_input_stale` remain O projections rather than durable advancement authority.
- Execution, Workspace, Handler, Claude, Pi, and Process Lifecycle agree that implementers use the one
  leased run workspace while each reviewer uses an unleased box-A exact-SHA projection.
- `WM-RIA-001` names the projection target path; verdict, feedback, budget diagnostic, reviewer session
  directory, and metadata transfer to the run archive before projection cleanup.
- Local/remote transfer is location-explicit, digest-verified, atomically published at the destination,
  and fails before observable outcome, Retask, or cleanup.
- Handler owns one authoritative lifecycle observer; Process Lifecycle owns cancellation/join and
  workspace release. ProcessExit harnesses do not acquire heartbeat-staleness machinery.
- Event, Execution, and Run State Machine agree that every entered review cycle has one durable
  cycle-complete followed by one terminal run event; failure events are intermediate witnesses.
- Execution, Operator NFR, and Beads Integration share the normalized outcome table:
  `approved` succeeds; `cap_hit`, `blocked`, `fixup_stalled`, and `error` use idempotent attention-close.
- Reviewer allowance remains Execution/Process policy, not a ControlPoint Budget. Heartbeat and pane
  presence are liveness only.
- Workflow Graph/DOT semantics, architecture dependency direction, and the agent-to-agent file-channel
  prohibition remain unchanged; the projection is S06/daemon-owned staging.

## Contradictions found and resolved

1. **Shared worktree versus reviewer projection.** Old WM/HC/CHB/CLS/Pi clauses required one physical
   worktree. Drafts now define one leased run workspace plus unleased per-iteration projections and
   phase-specific workspace paths.
2. **Projection cleanup versus retained evidence.** Reviewer logs/sidecars would have been deleted.
   Archive transfer and digest verification now precede cleanup.
3. **Generic orphan sweep versus intentionally unleased projection.** Projection candidates bypass
   no-lease cleanup; manifest/path/SHA/archive proof permits reclaim, otherwise evidence routes to
   `reviewer-projection-integrity` reconciliation.
4. **Pre-exec ready versus genuine ready.** Claude normative bodies now derive/deduplicate ready from
   SessionStart after launch rather than pre-emitting it.
5. **Continuity ACK ambiguity.** The checkpoint handshake now names `version_selected`; relay and input
   acknowledgements remain separate.
6. **Minted/Captured timing.** Minted durability gates first work; Captured identity is observed after
   launch and gates later resume/Retask.
7. **Input terminal mismatch.** Synchronous Rejected is represented without inventing an ack/stale event;
   only positive acceptance advances.
8. **Terminal alternatives.** RSM no longer treats cycle-complete and terminal run events as alternatives.
9. **Needs-attention gaps.** ON/BI now cover cap, block, fix-up stall, and error with a recoverable
   close-plus-label transaction; flagless request-changes normalized to approval is excluded.
10. **Stale event/enum details.** Event Model includes `context-checkpoint`, cap-hit is
    request-changes-only, reviewer identities are policy-aware, and `implementer_phase_complete` is an
    ordinary logical-result event.
11. **Watcher/pane/workspace lifetime.** Phase close seals callbacks, joins observer/process/pane work,
    transfers artifacts/session evidence, then releases projection/workspace.

## Consistency and changelog

Terminology is consistent on run workspace, reviewer projection, review-control archive, authoritative
lifecycle observer, logical phase outcome, quiescent completion, diff allowance, liveness grace,
continuity checkpoint, and attention-close.

`05-changelog.md` accounts for all eleven drafts, their predecessor/target versions, original and
integration designs, and the required scenario/exploratory acceptance beads.

## Assessment

The amended corpus is coherent. Remaining implementation defects are intentionally represented as tasks,
not spec exceptions. No unresolved normative contradiction blocks task decomposition.
