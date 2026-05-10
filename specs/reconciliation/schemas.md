# Reconciliation — Schemas

```yaml
---
title: Reconciliation — Schemas
spec-id: reconciliation
status: supplement
spec-template-version: 1.1
version: 0.4.0
last-updated: 2026-04-24
---
```

> INFORMATIVE: This is a sibling file of [spec.md](spec.md) per the multi-file split rule (§Multi-file split of the spec template). It carries §6 content; the normative shell, §§1–5, §7–12, and appendices, live in `spec.md`. Requirement IDs are spec-wide; references like `RC-025` resolve against the spec as a whole. spec-id `reconciliation` matches the spec-id in [spec.md]; `status: supplement` indicates this file carries schema detail only. For cross-sibling citations from other specs, prefer [reconciliation/schemas.md §6.N]; for cross-sibling citations into the main shell, prefer [reconciliation/spec.md §N].

## 6. Schemas and data shapes

### 6.1 Record schemas

```
RECORD SnapshotToken:
    git_head_hash          : String     -- SHA of project HEAD (or reference investigator reads from)
    beads_audit_entry_id   : String     -- ID of most recent Beads audit-log entry at capture time
    captured_at_timestamp  : Timestamp  -- RFC 3339 wall-clock; advisory display only per [event-model.md §4.3]
```

```
RECORD InvestigatorInput:                                        -- LOGICAL VIEW; NOT a daemon-assembled record (RC-015)
    snapshot_token         : SnapshotToken                      -- bounds the investigator's view (RC-015)
    target_run_id          : UUID                               -- the outer run being reconciled
    target_workflow_id     : UUID
    target_workflow_version: String
    target_bead_id         : String | None                      -- Beads bead ID when bead-bound
    bead_record            : BeadRecord | None                  -- Beads record as-of beads_audit_entry_id
    last_checkpoint        : Checkpoint                         -- tip of target run's task branch at snapshot
    last_transition        : Transition                         -- record at last_checkpoint; per [execution-model.md §6.1]
    jsonl_tail             : List<EventEnvelope>                -- events since last checkpoint; observational only (RC-014)
    workspace_observation  : WorkspaceObservation               -- path exists, branch state, WIP files
    session_log_ref        : String | None                      -- CASS handle per [workspace-model.md §4.7] when present
    category               : ReconciliationCategory             -- classification that dispatched this investigator
    playbook_ref           : String                             -- per-category playbook per RC-016
    budget_wall_clock_seconds : Integer                         -- mandatory budget per RC-017
```

> INFORMATIVE: `InvestigatorInput` documents the logical fields the investigator MUST be able to retrieve via its skills bounded by the snapshot token; the daemon does NOT assemble a single record carrying these fields. Per RC-015, the investigator self-assembles by querying Beads-CLI, git-inspection, workspace-inspection, and the bounded JSONL reader, all bounded by `snapshot_token`.

```
RECORD WorkspaceObservation:                                    -- read-only point-in-time observation; renamed from WorkspaceState 2026-05-09 (hk-63oh.80) to avoid cross-spec collision with workspace-model.md §6.1 ENUM
    path                : String          -- absolute path to the run's worktree
    path_exists         : Bool            -- filesystem probe result
    branch_tip_hash     : String | None   -- current task-branch tip; null if worktree missing
    wip_present         : Bool            -- true if `git status --porcelain` is non-empty or untracked files exist
    git_in_progress_op  : GitInProgressOp -- none | rebase | merge | cherry-pick | bisect; Cat 6a trigger when not none
```

```
ENUM GitInProgressOp:
    none
    rebase
    merge
    cherry-pick
    bisect
```

```
RECORD VerdictEvent:
    verdict              : Verdict              -- one of seven enum values (see below)
    investigator_run_id  : UUID                 -- run_id of the reconciliation workflow
    target_run_id        : UUID                 -- run_id of the outer run being reconciled
    evidence_ref         : String | None        -- git commit hash of the reconciliation commit carrying evidence
    context              : String | None        -- required iff verdict = resume-with-context; MUST be empty otherwise
    checkpoint_ref       : UUID | None          -- transition_id; required iff verdict = reset-to-checkpoint; null otherwise
    snapshot_token       : SnapshotToken        -- bound investigator view; consumed by staleness check (RC-024)
    schema_version       : Integer              -- N-1 readable per [spec.md §6.4]
```

```
ENUM Verdict:
    resume-here                 -- dispatch the current node with the same or a fresh agent; no context change
    resume-with-context         -- dispatch the current node with investigator-supplied context injected
    reset-to-checkpoint         -- intra-run rollback to a named earlier checkpoint; keeps worktree and run_id
    reopen-bead                 -- mark the bead `open`; subsequent claim produces a new run with fresh worktree
    accept-close-with-note      -- close is legitimate; annotate the audit gap; write bead close if needed
    no-op-accept                -- investigator confirms current state is legitimate; no mechanical action; run continues
    escalate-to-human           -- investigator cannot resolve; pause affected run, emit operator-observable event
```

```
ENUM ReconciliationCategory:
    cat-0                       -- infrastructure unavailable
    cat-1                       -- idempotent rerun
    cat-2                       -- non-idempotent in-flight
    cat-3                       -- store disagreement (generic)
    cat-3a                      -- torn Beads write
    cat-3b                      -- verdict-unexecuted
    cat-3c                      -- inverse premature-close
    cat-4                       -- recoverable known state
    cat-5                       -- clean restart
    cat-6a                      -- integrity violation, LLM-triageable
    cat-6b                      -- integrity violation, mechanically unrecoverable
```

```
ENUM MalformationReason:
    unknown-verdict-value       -- verdict field not in the Verdict enum
    missing-required-field      -- required field absent (e.g., context when verdict=resume-with-context)
    extra-fields                -- top-level fields not in VerdictEvent schema
    wrong-type                  -- field present with wrong JSON type
    multiple-verdicts           -- reconciliation workflow emitted more than one verdict event
    verdict-after-terminal      -- verdict event emitted after the workflow's terminal event
```

```
RECORD BudgetExhaustedPayload:
    run_id              : UUID        -- the reconciliation workflow's run_id
    workflow_id         : UUID        -- workflow definition id
    budget_seconds      : Integer     -- declared wall-clock budget
    elapsed_seconds     : Integer     -- actual elapsed when terminated
```

```
RECORD StaleVerdictPayload:
    snapshot_token           : SnapshotToken     -- captured at dispatch time
    current_git_head_hash    : String            -- re-captured HEAD at execution time
    current_beads_audit_id   : String            -- re-captured Beads audit entry id
    divergence_reason        : StaleDivergenceReason
```

```
ENUM StaleDivergenceReason:
    git-branch-advanced         -- target run's task branch has new commit since snapshot
    beads-audit-advanced        -- target bead's Beads audit entries have advanced since snapshot
```

```
RECORD MalformedVerdictPayload:                                  -- payload of `reconciliation_verdict_malformed` (RC-023)
    investigator_run_id : UUID
    target_run_id       : UUID
    malformation_reason : MalformationReason
    raw_verdict_excerpt : String
```

```
RECORD VerdictExecutedPayload:                                   -- payload of `reconciliation_verdict_executed` (RC-025)
    investigator_run_id   : UUID
    target_run_id         : UUID
    verdict               : Verdict
    executed_at_timestamp : Timestamp                            -- RFC 3339 wall-clock; advisory display only
    action_summary        : String                               -- short prose describing the mechanical action taken
```

> INFORMATIVE: `BeadRecord`, `EventEnvelope`, `Checkpoint`, `Transition` are defined in their owning specs ([beads-integration.md §4.3], [event-model.md §4.1], [execution-model.md §6.1]). Reconciliation cites them; it does not redefine them. `LaunchSpec` is defined in [handler-contract.md §6.1]; the investigator subprocess is launched via a standard LaunchSpec (RC-015a) with `LaunchSpec.snapshot_token` carrying the JSON-serialized `SnapshotToken` (RC-015).

### 6.2 Verdict-execution table

The daemon's mechanical action per verdict. Consumed by RC-025.

| Verdict | Mechanical action | Idempotency key / rule | Emits |
|---|---|---|---|
| `resume-here` | Re-dispatch the outer run's current node (no context change). | Idempotent at dispatch layer (dispatch check sees outer run already running; no re-dispatch). | `reconciliation_verdict_executed`; `node_dispatch_requested` (if dispatch proceeds) |
| `resume-with-context` | Re-dispatch the outer run's current node with investigator `context` injected into the run's shared context per [execution-model.md §4.1 EM-005]. | Idempotent at dispatch layer; context injection is additive (repeated application produces the same context map). | `reconciliation_verdict_executed`; `node_dispatch_requested` |
| `reset-to-checkpoint` | Revert the outer run to the checkpoint named by `checkpoint_ref`. Record as a new transition with `transition_kind = architectural-rollback` and `rollback_to_state_id` per [execution-model.md §4.10 EM-044, EM-045]. Re-run from the reverted state. | Idempotent: if the current run state already matches the target `state_id`, no-op. | `reconciliation_verdict_executed`; `state_entered`; `transition_event` |
| `reopen-bead` | The daemon's verdict-executor invokes the BI-CLI adapter's reopen path per [beads-integration.md §4.4 BI-010] (binding from `reopen-bead` verdict to `reopen` op per BI-010a status-mapping table); the adapter's BI-031 status-check-before-reissue protocol per [beads-integration.md §4.10] handles idempotency. Clear in-flight tracking; a subsequent claim produces a new run with fresh worktree + fresh branch per [workspace-model.md §4.9 WM-034]. WIP capture is mandatory per RC-019. | Idempotency via BI-031b status-check (BI-031); if bead already `open`, status-match → no-op. | `reconciliation_verdict_executed`; Beads audit entry for reopen |
| `accept-close-with-note` | Append the investigator's annotation to the reconciliation commit. If the bead is not already `closed`, write close through the adapter with idempotency key `<target_run_id>:close`. | Idempotent: repeated re-runs append the same annotation and see the close already landed. | `reconciliation_verdict_executed`; Beads audit entry for close (if not already closed) |
| `no-op-accept` | No mechanical action beyond emitting `reconciliation_verdict_executed` and appending the verdict-executed commit. The outer run is left untouched; the next ordinary dispatch cycle treats the run as in its ordinary state. | Idempotent: repeated application produces identical emissions (dedup by `target_run_id` on the execution event). | `reconciliation_verdict_executed` |
| `escalate-to-human` | RC emits `operator_escalation_required` event; the operator-event consumption surface per [operator-nfr.md §4.1 ON-002] is the operator's read path. The outer run remains in its current state (no synthesized 'quarantined' state); subsequent dispatch cycles may re-classify if the underlying condition changes. | Deduplicated by `target_run_id` (subsequent emissions are no-ops). | `reconciliation_verdict_executed`; `operator_escalation_required` |

After every mechanical action, the daemon MUST append a commit to the investigator's task branch carrying the trailer `Harmonik-Verdict-Executed: true` per RC-025. This is the verdict-executed commit; it is payload-free (presence-only marker). Trailer shape is declared in §6.4 below (RC-owned since the EM v0.2.0 trailer rollback).

### 6.3 Category × detector × action table (canonical)

This is the canonical detection-rule + action-mapping table. The duplicate in [spec.md §8.12] preserves the taxonomy-first reading order; divergence between the two is a lint failure.

| Category | Detection-rule summary | Default action | Investigator? | Auto-resolver? | Typical verdict |
|---|---|---|---|---|---|
| Cat 0 | `br --version` fails within T=5s, Beads SQLite locked by non-harmonik, git index locked, `.harmonik/` unwritable, or filesystem full (per RC-012). | halt classification + `degraded` status | No | Yes (wait-and-retry) | — |
| Cat 1 | Last checkpoint's node has `idempotency_class = idempotent` per [execution-model.md §4.2 EM-009]. | auto-resume by re-spawning | No | Yes (spawn the node) | — |
| Cat 2 | Node has `idempotency_class ∈ {non-idempotent, recoverable-non-idempotent}`; bead `in_progress`; no `run_completed` or `run_failed` event since checkpoint. | investigator workflow | Yes | No | `resume-with-context` / `reset-to-checkpoint` / `reopen-bead` |
| Cat 3 | Inter-store disagreement not matching 3a/3b/3c: duplicate `transition_id` across commits in same run, bead `in_progress` with worktree missing and no terminal marker, etc. | investigator workflow (git-wins orientation per RC-INV-001) | Yes | No | `accept-close-with-note` / `reopen-bead` / `no-op-accept` |
| Cat 3a | Intent-log entry at `.harmonik/beads-intents/<idempotency_key>.json` present AND the bead's current Beads coarse-status is neither pre-state nor post-state of the intended transition (per [beads-integration.md §4.10] BI-029, BI-031, BI-031b). | auto-resolve via adapter status-check-before-reissue | No | Yes (BI-031b status-check classifies and adapter routes) | — |
| Cat 3b | Investigator-run task branch has `reconciliation_verdict_emitted` commit AND no subsequent `Harmonik-Verdict-Executed: true` commit. | auto-resolve via RC-026 re-execution (staleness-checked) | No | Yes (re-execute verdict) | — |
| Cat 3c | Merge commit on target branch exists for run R with success terminal state; bead for R still `in_progress`; no subsequent in-flight checkpoints for R. | auto-verdict `accept-close-with-note` + mechanical close via adapter | No | Yes (direct close-write) | — |
| Cat 4 | Agent was in well-defined retry/backoff at crash (rate-limited pending retry, waiting for human gate). | auto-resume with pending action | No | Yes (re-arm retry/gate) | — |
| Cat 5 | Nothing in-flight for this run (includes orphaned branches from prior reopened runs per RC-010). | normal startup; proceed to `ready` | No | Yes (no-op) | — |
| Cat 6a | Workspace missing AND transition-record absent; OR trailer-vs-sibling-file mismatch; OR worktree has uncommitted git-in-progress op (rebase/merge/cherry-pick/bisect); OR bead `in_progress` with two+ task branches each advertising `Harmonik-Run-ID` without `Harmonik-Verdict-Executed`. | investigator workflow | Yes | No | `escalate-to-human` (default; investigator MAY downgrade) |
| Cat 6b | JSONL corrupt past byte offset; OR JSONL references checkpoint hash missing from git object database; OR `git fsck` fails. | auto-escalate to operator without investigator spawn | No | N/A (operator intervention) | — |

### 6.4 Verdict-executed commit trailers

RC owns the shape of the `Harmonik-Verdict-Executed` commit trailer as of spec v0.3.0 (reclaimed from execution-model.md after EM v0.2.0 removed the trailer from its §6 trailer registry and deferred to reconciliation).

```
TRAILER Harmonik-Verdict-Executed:
    key   : "Harmonik-Verdict-Executed"  -- exact key string (case-sensitive per git trailer grammar)
    value : "true"                         -- fixed literal; any other value is malformed (RC-023)
    presence_semantics : marker-only       -- the trailer's presence marks execution; no payload is carried
```

Placement. The trailer MUST appear on a commit that is a descendant of the verdict commit on the same investigator task branch. It MUST NOT appear on the verdict commit itself (the verdict commit carries `Harmonik-Run-ID`, `Harmonik-Workflow-Class: reconciliation`, and the verdict payload per [schemas.md §6.1 VerdictEvent] but NOT this trailer).

Emission contract. The trailer is emitted exactly once per reconciliation workflow whose verdict is executed. If the same verdict is re-executed under Cat 3b auto-resolution (RC-026), a new verdict-executed commit is appended; the prior one is NOT rewritten. Cat 3b idempotency relies on presence-check (RC-INV-004 sensor semantics).

Cross-spec coordination. Execution-model.md §6 trailer registry MUST note (via its own revision history) that `Harmonik-Verdict-Executed` is owned here. Any EM trailer-lint tool MUST treat this trailer as an RC-owned extension; not cross-listing is a lint failure.

### 6.5 Workflow-record extension — `workflow_class`

RC extends the Workflow record owned by [execution-model.md §4.1] with a subsystem-local tag:

```
EXTENSION OF ExecutionModel.Workflow:
    workflow_class : WorkflowClass   -- optional tag, absent means "ordinary"
```

```
ENUM WorkflowClass:
    reconciliation              -- the workflow implements an RC-category investigator or auto-resolver dispatch
                                --   (scoped to S01-shipped DOT per RC-004)
```

Semantics. A Workflow whose `workflow_class = reconciliation` is subject to RC-002 (exactly-one-checkpoint commit per workflow), RC-002a (at most one per target_run_id), and RC-INV-001 (uniqueness audit sensor). Workflows without `workflow_class` set are ordinary workflows; reconciliation requirements do not apply.

Ownership. This extension is RC-owned; its acceptance into EM's canonical Workflow record is tracked in OQ-RC-002. If EM accepts, RC-002 and RC-INV-001 will re-point at [execution-model.md §4.1]; until then, this §6.5 is the canonical declaration.

Future enum growth. Additional workflow classes (e.g., `improvement-loop`, `operator-cli-handler`) are reserved for future use by their respective subsystems; not declared here.

### 6.6 Co-owned event payloads

Cross-reference only. Payload schemas for the following events are owned by [event-model.md §8] per-event subsections and [event-model.md §6.3] payload registry; this spec is normative for the emission timing per [spec.md §4] requirements.

- `reconciliation_category_assigned` — after classification (RC-013).
- `reconciliation_verdict_emitted` — on investigator verdict emission (RC-021); payload is `VerdictEvent` above.
- `reconciliation_verdict_executed` — after mechanical action lands (RC-025).
- `reconciliation_verdict_stale` — on staleness check failure (RC-024); payload is `StaleVerdictPayload` above.
- `reconciliation_verdict_malformed` — on schema violation (RC-023); payload carries `MalformationReason` enum above.
- `reconciliation_budget_exhausted` — on wall-clock budget exhaustion (RC-018); payload is `BudgetExhaustedPayload` above.
- `store_divergence_detected` — on JSONL divergence-evidence detection (RC-014); emissions subject to RC-INV-004 corroboration guarantee.
- `infrastructure_unavailable` — on Cat 0 prerequisite failure (RC-012); co-owned with [process-lifecycle.md §4.2] PL-010.
- `operator_escalation_required` — on `escalate-to-human` verdict execution (RC-025); event type owned by this spec, with operator-observable surface delivered via [operator-nfr.md §4.1 ON-002].

Dual-table note (cross-file). [spec.md §8.12] is authoritative on semantics (prose interpretation of each category's default action); [schemas.md §6.3] is authoritative on mechanical dispatch (tabular schema consumed by daemon code and lint). The two MUST stay in sync; divergence is a lint failure.
