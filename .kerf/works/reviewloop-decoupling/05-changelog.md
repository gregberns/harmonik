# Spec-draft changelog — `reviewloop-decoupling`

> Pass 5 (`spec-draft`) changelog. One entry per affected spec area. Component writers append their own
> sections; Pass 6 reconciles cross-spec references and any overlapping edits.

## C2 — Handler session and observer lifecycle

Draft: `05-spec-drafts/handler-contract.md`. Design: `04-design/handler-contract-design.md`.

| Target spec | Status | Version | Changes |
|---|---|---:|---|
| `specs/handler-contract.md` | modified | 0.8.0 → 0.9.0 | HC-011/011a and HC-INV-001 define exactly one substrate-neutral authoritative lifecycle observer per active session; add its cancellation/completion surface and separate it from process wait/reap and auxiliary observers. HC-039/041/056 and HC-INV-004 correct launch→ready→input ordering and deduplicate ready. HC-057/HC-INV-007 add the daemon-heartbeat publication carve-out while preserving equivalent session-liveness semantics and requiring producer completion. Conformance covers both substrates, watcher/heartbeat completion, blocked-source cancellation, and ready races. |

No new HC requirement ID or event type is introduced. Existing ACK, process-provenance, kill, and
heartbeat-payload contracts are unchanged.

## C4 — Interactive process and phase lifetime

Draft: `05-spec-drafts/process-lifecycle.md`. Design:
`04-design/process-lifecycle-design.md`.

| Target spec | Status | Version | Changes |
|---|---|---:|---|
| `specs/process-lifecycle.md` | modified | 0.6.0 → 0.7.0 | NEW PL-014b defines phase-owned resources and the quiescent advancement barrier, including callback admission sealing, bounded cancellation plus observable completion, substrate-aware watcher/process/pane ordering, workspace retention on timeout, and typed ownership transfer. PL-014 is reconciled with direct/tmux parentage and one direct-process wait owner; PL-016 separates process exit ownership from authoritative lifecycle classification. Conformance adds ordered lifecycle, race, leak, timeout-retention, and ownership-transfer coverage. |

PL-014b was collision-checked against the existing PL-014/PL-014a sequence. No process class, event,
input mechanism, provenance rule, escalation interval, tmux policy, or public event order changes.

## C1, C5, C7 — Execution, continuity checkpoint, and reviewer allowance

Draft: `05-spec-drafts/execution-model.md`. Designs:
`04-design/execution-model-design.md`, `04-design/checkpoint-ack-design.md`, and
`04-design/reviewer-budget-design.md`.

| Target spec | Status | Version | Changes |
|---|---|---:|---|
| `specs/execution-model.md` | modified | 0.9.3 → 0.10.0 | EM-012/015d/015e use durable prior-HEAD state and an exact continuity-checkpoint baseline, preserve raw verdict while defining flagless REQUEST_CHANGES routing, add `fixup_stalled`, define implementer/reviewer identity selection and needs-attention outcomes, and specify the deterministic cycle projection. Minted and Captured continuity receive separate acquisition sequences with one explicit-location checkpoint transaction and error/crash matrix. Reviewer verdict allowance is defined as diff allowance plus separately capped liveness grace; heartbeat/pane presence do not become work activity. Conformance adds exhaustive decisions, restart/crash, negotiation, allowance, and trace cases. |

No ControlPoint budget, review-loop subsystem, generic effect interpreter, or event-publication stage is
introduced. Historical `no_progress` remains readable.

## C6 — Event truth and ordering

Draft: `05-spec-drafts/event-model.md`. Design: `04-design/event-model-design.md`.

| Target spec | Status | Version | Changes |
|---|---|---:|---|
| `specs/event-model.md` | modified | 0.7.1 → 0.7.2 | Adopts the existing ordinary `implementer_phase_complete` logical-result event, defines `reviewer_launched` as pre-launch dispatch intent, requires launch-artifact identity correlation and ready payload identity, aligns cap/fix-up/flagless-verdict traces, documents existing `reviewer_budget_exceeded`, and requires F-emission failures to enter the existing durability failure path. Conformance separates shipped characterization from the amended target oracle and uses existing input terminal events. |

No new event type or silent durability promotion is introduced.

## C3 — Workspace placement and reviewer-control artifacts

Draft: `05-spec-drafts/workspace-model.md`. Design: `04-design/workspace-model-design.md`.

| Target spec | Status | Version | Changes |
|---|---|---:|---|
| `specs/workspace-model.md` | modified | 0.4.5 → 0.4.9 | WM-027a and canonical-path rules distinguish the authoritative local/remote run workspace from an unleased box-A reviewer projection identified by run, iteration, source ref/SHA, and location. Target, live verdict, and budget sentinel are projection staging artifacts; validated per-iteration verdict, current alias, feedback, and diagnostics transfer atomically to the run-workspace archive. Cleanup follows phase quiescence and transfer; startup reclamation validates the projection manifest and absence of a live owner. |

Class-F events remain routing/audit facts and do not replace durable Workspace Model artifacts.

## Acceptance-test beads

| Spec | Scenario | Exploratory |
|---|---|---|
| execution-model.md | `hk-0zkpw` | `hk-zfr49` |
| event-model.md | `hk-ehygf` | `hk-2eua9` |
| handler-contract.md | `hk-1fr7a` | `hk-a2yi3` |
| process-lifecycle.md | `hk-koqv8` | `hk-y6r40` |
| workspace-model.md | `hk-z7vqw` | `hk-5x9kz` |

## Integration-owner amendments

Pass 6 found that the review-loop changes made stale contracts in six unchanged owner specs observable.
Each was amended narrowly to reconcile its existing surface; none introduces a new subsystem.

| Target spec | Status | Version | Integration change |
|---|---|---:|---|
| `specs/run-state-machine.md` | modified | 0.2.0 → 0.2.1 | Requires durable cycle-complete before the run terminal, treats failure events as witnesses rather than substitute terminals, uses the AIS rejected/acked/stale model, and routes review-loop lifecycle failures through normalized error handling. |
| `specs/operator-nfr.md` | modified | 0.5.5 → 0.5.7 | Defines operator handling for normalized approved/cap/blocked/fix-up-stalled/error outcomes and the attention-close surface; built-in `no_progress` becomes historical only. |
| `specs/beads-integration.md` | modified | 0.8.0 → 0.8.1 | Adds the recoverable, idempotent attention-close transaction whose intent remains until both closed status and needs-attention label are confirmed. |
| `specs/claude-hook-bridge.md` | modified | 0.9 → 1.3 | Reconciles genuine SessionStart ready, continuity checkpoint before `version_selected`, phase workspace/projection paths, ACK vocabulary, and reviewer session-evidence transfer. |
| `specs/claude-launchspec.md` | modified | 0.1 → 0.2.0 | Reconciles launch/ready ordering, Minted checkpoint handshake, reviewer projection placement, and delegates normalized cycle routing to Execution Model. |
| `specs/pi-harness.md` | modified | 0.1.0 → 0.1.1 | Uses the run workspace for implementers and the mandatory reviewer projection for review-loop reviewers. |

Workspace Model additionally advances to 0.4.10 and Process Lifecycle to 0.7.1 after integration
reconciled one leased run workspace versus unleased projections, manifest-aware reclamation, remote
transfer, retained session evidence, and the archive-before-cleanup barrier.

## Integration acceptance-test beads

| Spec | Scenario | Exploratory |
|---|---|---|
| run-state-machine.md | `hk-k4300` | `hk-ljh9q` |
| operator-nfr.md | `hk-e3niy` | `hk-qmyo1` |
| beads-integration.md | `hk-tc36h` | `hk-tw05c` |
| claude-hook-bridge.md | `hk-gv3i7` | `hk-zzyne` |
| claude-launchspec.md | `hk-6npn8` | `hk-yko90` |
| pi-harness.md | `hk-8ox47` | `hk-9l4hb` |
