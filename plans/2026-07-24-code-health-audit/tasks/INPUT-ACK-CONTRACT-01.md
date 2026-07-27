# INPUT-ACK-CONTRACT-01 — Reconcile run input-ack consumption

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: none
- Blocks: `ARCH-01`, `PS-01`, `BR-00`
- Work type: bounded normative drift correction; no production implementation

## Objective

Remove the stale three-valued input-acceptance model from
`specs/run-state-machine.md` and reconcile five stale “positive synchronous
Ack” restatements in the otherwise binary owner contracts. The coordinated result
must agree across `specs/agent-input.md` AIS-003/AIS-004/AIS-005,
`specs/handler-contract.md` HC-069/HC-070, and RSM-027:

- synchronous `Ack.outcome` is binary `Delivered | Rejected`;
- `Delivered` confirms driver handoff, not positive agent acceptance;
- positive acceptance is the asynchronous `agent_input_acked` event;
- absence of positive acceptance terminates as `agent_input_stale`;
- the input sequence correlates the synchronous handoff with its asynchronous
  terminal.

This task corrects one consumer and five contradictory owner-spec
restatements. It must not change the Ack record, event schemas, timing
ownership, or production Go.

## Authority and preflight

Read all of:

- `specs/agent-input.md` AIS-001, AIS-003, AIS-004, AIS-005, AIS-INV-001 and its
  revision history;
- `specs/handler-contract.md` HC-069, HC-070, HC-INV-008, the `Ack` record, and
  its revision history;
- `specs/run-state-machine.md` RSM-024 through RSM-027, RSM-INV-001, §13, and
  its revision history;
- the Git history for the three clauses, sufficient to establish which owner
  contract landed last and whether the mismatch is drift rather than an open
  design choice;
- ARCH-01 change-design findings that name `INPUT-ACK-CONTRACT-01`.

Record exact input blob IDs and the chronology conclusion in the task evidence.
The known input includes five stale restatements: the `front-stop` glossary
entry and AIS-005 in agent-input, plus the HC-056, HC-057, and HC-070
“Front-stop” paragraphs in handler-contract. They call the synchronous Ack a
positive acceptance signal, contradicting the Ack schemas and the surrounding
AIS-003/HC-070 text. Stop if additional owner-contract contradictions appear,
or if Git history shows the three-valued model or any of those five sentences
was a later operator-approved decision.

## Kerf work and exclusive lease

Create one local spec work named `input-ack-contract` using the spec jig. Follow
the printed Kerf passes through review, but do not invoke `kerf finalize`.

Allowed writes are exactly:

- `.kerf/works/input-ack-contract/**`
- `specs/agent-input.md`, limited to the `front-stop` glossary entry, AIS-005,
  directly dependent cross-references, version metadata, and one
  revision-history row
- `specs/handler-contract.md`, limited to the HC-056, HC-057, and HC-070
  “Front-stop” paragraphs, directly dependent cross-references/conformance
  wording, version metadata, and one revision-history row
- `specs/run-state-machine.md`
- `plans/2026-07-24-code-health-audit/tasks/evidence/INPUT-ACK-CONTRACT-01.yaml`

Do not edit production Go, tests, event schemas, the Ack record in either owner
spec, the spec registry, other task cards, or `TASK-INDEX.yaml`. If either owner
spec needs a normative edit beyond the five named stale restatements and their
directly dependent wording, stop and return the exact contradiction.

## Required work

1. Amend RSM-027 without renumbering it:
   - `Ack{Delivered}` records successful driver handoff but does not by itself
     advance a transition that requires positive agent acceptance;
   - `Ack{Rejected}` routes to the existing fail-closed liveness edge;
   - correlated `agent_input_acked` is the positive-acceptance event;
   - correlated `agent_input_stale` routes to the existing fail-closed edge;
   - remove `Accepted`, `Degraded`, “three-valued acceptance class,” and every
   behavior derived from them.
2. Amend the agent-input `front-stop` glossary entry and AIS-005, plus the
   HC-056, HC-057, and HC-070 “Front-stop composition” paragraphs, so
   synchronous `Ack{Delivered}` is an earlier delivery-handoff observation, not
   positive acceptance. Preserve their rule that input delivery composes in
   front of and does not replace readiness, heartbeat, or commit watchdogs.
3. Reconcile RSM-024, RSM-INV-001, §1/§2 scope language, and §13 wherever they
   imply that a `Delivered` return is positive acceptance or that `Degraded`
   exists. Do not widen beyond input-ack consumption.
4. Preserve ownership: AIS owns the port, Ack, async events, timer, and
   output-or-stale definition; RSM owns only reactor consumption and routing.
5. Preserve correlation using `input_seq` with this exact consumption rule:
   consume the first synchronous outcome once; `Delivered` leaves positive
   acceptance pending and `Rejected` fail-closes. After `Delivered`, the first
   correlated asynchronous terminal (`agent_input_acked` or
   `agent_input_stale`) wins. Drop only repeated or late observations; never
   drop the first `agent_input_acked` merely because `Delivered` was observed.
   Do not add a second timer.
6. Bump each changed spec's patch version and add one dated revision-history
   row identifying the coordinated drift correction. Do not change status or
   unrelated requirements.

## Acceptance

- RSM contains no live `Accepted` or `Degraded` Ack outcome.
- Every Ack/event route is total:
  `Delivered -> await correlated async terminal`,
  `Rejected -> fail closed`,
  `agent_input_acked -> acceptance path`, and
  `agent_input_stale -> fail closed`.
- No wording treats a successful tmux write as positive acceptance.
- None of the five named owner-spec restatements calls synchronous Ack positive
  acceptance; their schemas and all unrelated semantics remain unchanged.
- RSM does not redefine the input timer or event payload.
- Diffs in AIS and HC are limited to the exact leased prose, metadata, directly
  dependent conformance/cross-reference wording, and revision rows.
- Kerf validation and independent cross-spec review approve the amendment.

## Verification

- `kerf show input-ack-contract`
- `kerf square input-ack-contract`
- repository spec validation and cross-reference checks scoped to the changed
  spec
- exact grep proving no live three-valued Ack vocabulary remains in RSM
- independent `sol_xhigh` review against all three pinned specs
- post-commit `make check-fast` (record unrelated baseline failures separately)

## Escalate when

Stop if the correct fix would change the Ack record, delivery outcomes, async
event or timer semantics, event payloads, production behavior, or a locked
decision.

## Return

Return the Kerf artifacts, evidence, exact spec diff, verification output, and
independent verdict. **COMMIT EXPLICITLY.**
