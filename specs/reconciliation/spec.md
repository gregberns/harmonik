# Reconciliation

```yaml
---
title: Reconciliation
spec-id: reconciliation
requirement-prefix: RC
status: reviewed
spec-shape: taxonomy-first
spec-category: foundation-cross-cutting
version: 0.4.5
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-06-01
depends-on:
  - execution-model
  - event-model
  - handler-contract
  - control-points
  - beads-integration
  - workspace-model
  - process-lifecycle
  - operator-nfr
  - architecture
---
```

## 1. Purpose

This spec defines harmonik's reconciliation model — the restart-time and store-divergence-recovery contract that classifies every in-flight run into a bounded set of detection categories, maps each category to a default resolution action, and specifies the investigator-agent contract, verdict vocabulary, verdict execution, and staleness rules for LLM-triaged cases. Reconciliation runs as a normal harmonik workflow (no separate subsystem); this spec owns the taxonomy, the dispatch contract, and the investigator inputs.

It is the concrete answer to "why we don't need a dedicated database for in-flight workflow state" (locked decision #12): deterministic reconstruction from git + Beads, plus agent-driven investigation for ambiguous cases. This spec is a separate file from `execution-model.md` because the taxonomy is cross-cutting (consumed by `process-lifecycle.md` §4.2 startup, `operator-nfr.md` §4.8 restart RTO, and `workspace-model.md` §4.7 re-run rule) and because its shape is taxonomy-first rather than requirements-first.

## 2. Scope

### 2.1 In scope

- Reconciliation-as-workflow principle: reconciliation runs as ordinary harmonik workflows, not a separate subsystem.
- Bounded recursion: reconciliation workflows emit exactly one checkpoint commit (the verdict commit); intermediate state is not durable.
- Workflow-library authoring owner: S01 (Orchestrator Core) ships the reconciliation DOT workflows and YAML policies.
- Category taxonomy: 11 detection categories (Cat 0, 1, 2, 3, 3a, 3b, 3c, 4, 5, 6a, 6b) with detectors keyed on git DAG parentage and run-scoped filtering.
- Action-mapping layer: the default daemon response per category (auto-resume, auto-resolve, investigator-dispatch, auto-escalate).
- Investigator-agent contract: snapshot-token inputs, wall-clock budget, verdict schema, malformed-verdict handling.
- Verdict vocabulary (enum of six values) and durable verdict-execution record.
- JSONL divergence-evidence reads: scoped use for detecting inter-store inconsistency; forbidden as a state-reconstruction source.
- Re-run vs. intra-run distinction for verdict-driven re-claims.
- Crash-recovery testing obligation for every detector and verdict-execution path.

### 2.2 Out of scope

- Execution-model invariants (git wins on completion, checkpoint trailers, one-checkpoint-per-durable-transition) — owned by [execution-model.md §4.5, §5].
- Beads terminal-transition adapter idempotency and intent-log mechanics — owned by [beads-integration.md §4.10].
- Event payload schemas for reconciliation events — owned by [event-model.md §6.3] (payload registry) and the per-event subsections under [event-model.md §8].
- DOT authoring details for the reconciliation workflow library (node-level policies, specific prompts) — owned by the S01 Orchestrator Core subsystem spec, post-MVH.
- Failure-commit policy (MVH: no failure commits) — owned by [execution-model.md §4.5].
- Agent-subprocess silent-hang detection mechanics — owned by [handler-contract.md §4.6].
- `needs-attention`-labeled closed beads — NOT a reconciliation surface. Detectors operate on in-flight runs (RC-010); a closed bead with a `needs-attention` label is a dispatch-filter signal for the BI-013a ingestion workflow, not evidence of store divergence or an incomplete run.

## 3. Glossary

- **reconciliation workflow** — a DOT workflow tagged `workflow_class = reconciliation` whose run executes the investigator playbook for a single reconciliation dispatch; bounded to a single verdict commit. (see §4.1)
- **investigator-agent** — the cognition-tagged agent node within a reconciliation workflow that reads state, reasons about divergence, and emits a verdict event. (see §4.4)
- **verdict** — one of seven enum values (`resume-here`, `resume-with-context`, `reset-to-checkpoint`, `reopen-bead`, `accept-close-with-note`, `no-op-accept`, `escalate-to-human`) that the daemon executes mechanically after emission. (see §4.5)
- **divergence** — an observed inconsistency between two or more of the three stores (git, Beads, JSONL) for a single run, classified per §8 taxonomy and emitted as `store_divergence_detected` (when corroborable) or `divergence_inconclusive` (when only a single store is readable). (see §8.4, RC-014, RC-019a)
- **in-flight run** — a run for which a `run_started` event has been observed and no `run_completed` or `run_failed` event has yet been observed; the canonical definition lives at [operator-nfr.md §3]. (see §8, RC-010)
- **verdict commit** — the single checkpoint commit a reconciliation workflow emits, carrying the verdict event's payload plus investigator-produced evidence. (see §4.1, §4.5)
- **verdict-executed commit** — a second commit on the investigator's task branch carrying the `Harmonik-Verdict-Executed: true` trailer, marking the daemon's mechanical action as durable. (see §4.5)
- **snapshot token** — the `(git_head_hash, beads_audit_entry_id, captured_at_timestamp)` tuple captured at investigator-dispatch time that bounds the investigator's view of state. (see §4.4)
- **detector** — a deterministic Go function that classifies an in-flight run into one of the 11 categories of §8 by inspecting git + Beads + (optionally) JSONL divergence evidence. (see §4.3)
- **action-mapping layer** — the dispatch table in §8 that maps each category to its default daemon action. (see §4.2)
- **Cat N** — shorthand for detection category N in the §8 taxonomy; references like "Cat 3a" resolve to §8.4a.
- **run-scoped detector** — a detector whose inputs are filtered by `Harmonik-Run-ID` trailer, not by `Harmonik-Bead-ID`; a bead with multiple runs has each run classified independently. (see §4.3)
- **divergence-evidence read** — a scoped JSONL read used only to identify inter-store inconsistency (checkpoint missing from git, transition-file missing, etc.); forbidden as a source for `run_id`, `state_id`, `transition_id`, or bead-status facts. (see §4.3, [execution-model.md §4.7], [event-model.md §4.5])

Canonical-location terms cited from elsewhere: `run`, `state`, `transition`, `checkpoint`, `idempotency_class` — all defined in [execution-model.md §3]. `bead` — defined in [beads-integration.md §4.3].

> INFORMATIVE: Reading order for this spec is `1, 2, 3, 8, 4, 5, 6, 7, 9, 10, 11, 12, A` per `spec-shape: taxonomy-first`. Section numbers are stable; only the on-page sequence shifts. §8 appears next, then §4 references it.

## 8. Error and failure taxonomy

This is the spec's center of gravity. The 11 detection categories below classify every in-flight run at restart time or on detected store divergence. Requirements in §4 reference these by number and sub-letter. Action mapping is in §8.12.

Detectors below assume the orphan sweep of [process-lifecycle.md §4.2 PL-005] has completed. No harmonik-owned orphan process or stale worktree lock is live at classification time.

### 8.1 Cat 0 — Infrastructure unavailable

**Detection rule.** A prerequisite for classification itself is unreachable: `br` CLI missing, wrong version, or timing out beyond T = 5s; Beads SQLite locked by a non-harmonik process; git index locked; `.harmonik/` directory unwritable; filesystem full. Pre-check is mandatory per RC-012.

**Default response.** Halt reconciliation at the detector layer. Daemon transitions to `degraded` status (a pre-`ready` state) and waits for infrastructure resolution. No in-flight run is classified until Cat 0 clears.

**Escalation path.** Operator clears the prerequisite (restart `br`, unlock git index, free disk, fix permissions). Daemon retries at configurable cadence.

**Emitted event.** `infrastructure_unavailable` naming the specific prerequisite.

**Investigator?** No. **Auto-resolver?** Yes (wait-and-retry loop).

### 8.2 Cat 1 — Idempotent rerun

**Detection rule.** Last checkpoint's `node_id` refers to a DOT node whose `idempotency_class` attribute is `idempotent` (per [execution-model.md §4.2 EM-009]).

**Default response.** Auto-resume by re-spawning the interrupted node.

**Escalation path.** None at this layer; any subsequent failure is classified under the failure taxonomy of [execution-model.md §8].

**Emitted event.** `reconciliation_category_assigned` (category=1); dispatch produces a `node_dispatch_requested` per handler contract.

**Investigator?** No. **Auto-resolver?** Yes (re-spawn).

**Examples.** Reviewer agent mid-run, researcher agent, mechanism-tagged non-agentic nodes (lint, test, typecheck).

> INFORMATIVE: Reconciliation-workflow nodes are never classified here because reconciliation workflows are not checkpointed mid-investigation (RC-002); the recursion question does not arise (RC-003).

### 8.3 Cat 2 — Non-idempotent in-flight

**Detection rule.** DOT node whose `idempotency_class` is `non-idempotent` or `recoverable-non-idempotent` per [execution-model.md §4.2 EM-009]; AND the bead is in `in_progress`; AND there is no `run_completed` or `run_failed` event for the run since that checkpoint (see [event-model.md §8] per-event subsections). Cat 2 detection MUST use a controlled, indexed query against the bounded JSONL divergence-evidence reader (RC-014), NOT a live-follow tail of the event stream.

**Default response.** Dispatch a Cat 2 investigator workflow with the per-category playbook (RC-016).

**Escalation path.** Investigator verdict — typically `resume-with-context`, `reset-to-checkpoint`, or `reopen-bead` (RC-020). Budget-exhausted dispatches default-escalate (RC-018).

**Emitted event.** `reconciliation_category_assigned` (category=2); downstream investigator emits `reconciliation_verdict_emitted`.

**Investigator?** Yes. **Auto-resolver?** No.

**Examples.** Builder mid-implementation, merge agent mid-merge.

### 8.4 Cat 3 — Store disagreement (generic)

**Detection rule.** git, Beads, and JSONL tell inconsistent stories about the same run, and no Cat 3 sub-category (3a/3b/3c) matches. Examples: duplicate `transition_id` across commits; bead `in_progress` but worktree missing without a run-terminal marker.

**Default response.** Dispatch a Cat 3 generic investigator workflow with git-wins orientation per RC-INV-001.

**Escalation path.** Investigator verdict — typically `accept-close-with-note` or `reopen-bead`.

**Emitted event.** `store_divergence_detected` + `reconciliation_category_assigned` (category=3).

**Investigator?** Yes. **Auto-resolver?** No (escalates through investigator).

### 8.4a Cat 3a — Torn Beads write

**Detection rule.** An adapter intent file present at `.harmonik/beads-intents/<idempotency_key>.json` (per [beads-integration.md §4.10 BI-029]) AND the corresponding bead's current Beads coarse-status (read via `br show` per [beads-integration.md §4.10 BI-031]) is neither the pre-state nor the post-state of the intended transition. The adapter's status-check-before-reissue protocol (BI-031b) classifies this as a divergence; the Cat 3a auto-resolver routes through the same adapter.

**Default response.** Auto-resolve via the adapter's BI-031 protocol: the adapter's status-check classifies the case (status_match → no-op, pre-state → reissue, schema_mismatch → BrSchemaMismatch). When the status is neither pre nor post, route to investigator dispatch (no auto-resolve). Otherwise the adapter handles it directly without an investigator.

**Escalation path.** If the audit log is itself ambiguous or the adapter raises `BrSchemaMismatch`, escalate to Cat 3 generic (investigator-dispatched).

**Emitted event.** `store_divergence_detected` (class=torn-beads-write) + `reconciliation_category_assigned` (category=3a).

**Investigator?** No (common case). **Auto-resolver?** Yes.

### 8.5 Cat 3b — Verdict-unexecuted

**Detection rule.** An investigator-run's task branch contains a `reconciliation_verdict_emitted` commit AND there is no subsequent `Harmonik-Verdict-Executed: true` commit on the same branch (per RC-025, [schemas.md §6.4]).

**Default response.** Auto-resolve via RC-026 re-execution: the daemon performs the staleness check (RC-024); if stale, re-dispatches fresh reconciliation; if not stale, executes the verdict per RC-025 and writes the verdict-executed commit. No new investigator is spawned.

**Escalation path.** If the re-execution itself fails (e.g., `br` call errors out), route to Cat 3 generic.

**Emitted event.** `reconciliation_category_assigned` (category=3b).

**Investigator?** No. **Auto-resolver?** Yes.

### 8.6 Cat 3c — Inverse premature-close (terminal-transition-without-Beads-write)

**Detection rule.** A merge commit exists on the target branch (main or integration per [workspace-model.md §4.7]) tagged with `Harmonik-Run-ID R` (for run R whose workflow reached a success terminal state), the bead for R is still `in_progress` in Beads, and no subsequent in-flight checkpoints for R exist.

**Default response.** Auto-verdict `accept-close-with-note` with mechanical close-write (routed through the idempotency-keyed adapter per [beads-integration.md §4.10]). No investigator is spawned.

**Escalation path.** If the mechanical close-write fails persistently (adapter errors), escalate to Cat 3 generic.

**Emitted event.** `reconciliation_category_assigned` (category=3c); downstream `reconciliation_verdict_executed`.

**Investigator?** No. **Auto-resolver?** Yes.

> RATIONALE: Git is authoritative for completion (RC-INV-001); the divergence is deterministically resolvable in Beads's direction without LLM reasoning.

### 8.7 Cat 4 — Recoverable known state

**Detection rule.** Agent was in a well-defined retry/backoff state at crash time (e.g., rate-limited pending retry timer, waiting for a human gate).

**Default response.** Auto-resume with the pending action — re-spawn with the prior retry/backoff timer, or re-present the gate to the relevant actor.

**Escalation path.** If the retry attempt cap is reached, reclassify per [execution-model.md §8] failure taxonomy.

**Emitted event.** `reconciliation_category_assigned` (category=4).

**Investigator?** No. **Auto-resolver?** Yes.

### 8.8 Cat 5 — Clean restart

**Detection rule.** Nothing in-flight for this run. Includes orphaned branches from prior runs of beads that have since been re-claimed (per RC-010).

**Default response.** Normal startup; proceed to `ready` per [process-lifecycle.md §4.2]. No-op.

**Escalation path.** None.

**Emitted event.** `reconciliation_category_assigned` (category=5).

**Investigator?** No. **Auto-resolver?** Yes (no-op).

### 8.11 Cat 6a — Integrity violation, LLM-triageable

**Detection rule.** Structurally wrong data whose cause an investigator can reason about and whose resolution might be operator-actionable. Detectors include:

- Workspace path referenced by in-flight bead does not exist on disk AND the sibling's transition-record file is absent.
- Trailer-vs-sibling-file mismatch on a checkpoint commit (e.g., `Harmonik-Transition-ID` trailer present but sibling file missing per [execution-model.md §4.4 EM-018]).

  Precedence with Cat 3: "workspace path missing + sibling transition-record absent" classifies here (Cat 6a) only when priority-ordering per RC-003a routes it here. The priority order (Cat 0 → Cat 6b → Cat 6a → Cat 5 → Cat 3c → Cat 3b → Cat 3a → Cat 3 → Cat 2 → Cat 4 → Cat 1) ensures Cat 6a fires first on workspace-missing + sibling-absent, since integrity-violation evidence dominates generic store disagreement.
- Worktree has in-progress git operation (rebase, merge, cherry-pick, bisect) that is not the work product being tracked by harmonik — detected by checking for `.git/rebase-merge`, `.git/rebase-apply`, `.git/MERGE_HEAD`, `.git/CHERRY_PICK_HEAD`, `.git/BISECT_LOG` in the run's worktree.
- A bead in `in_progress` with two or more task branches each advertising an `Harmonik-Run-ID` without a `Harmonik-Verdict-Executed` marker (reopen-bead landed, Beads audit entry missing).

**Default response.** Dispatch Cat 6a investigator workflow. Default verdict: `escalate-to-human` (RC-020); an investigator MAY downgrade to a repair path.

**Escalation path.** Operator intervention via the escalation event.

**Emitted event.** `store_divergence_detected` + `reconciliation_category_assigned` (category=6a); downstream `reconciliation_verdict_emitted` (typically `escalate-to-human`).

**Investigator?** Yes. **Auto-resolver?** No.

### 8.11a Cat 6b — Integrity violation, mechanically unrecoverable

**Detection rule.** Structurally wrong data with no recovery path an LLM could meaningfully affect. Detectors include:

- JSONL is corrupt / unparseable past a byte offset such that the divergence-evidence reader (RC-014) cannot make a determination.
- A checkpoint commit hash referenced in JSONL is missing from git's object database.
- Git object database itself is corrupted (e.g., `git fsck` fails).

**Default response.** Auto-escalate to operator without investigator spawn. Emit `operator_escalation_required` with Cat 6b reason and target-run context. No reconciliation workflow is dispatched.

**Escalation path.** Operator intervention: restore from backup, rebuild worktree, repair git object database.

**Emitted event.** `reconciliation_category_assigned` (category=6b) + `operator_escalation_required`.

**Investigator?** No (spawning an investigator to "explain why git is corrupt" burns tokens on an operator-only problem). **Auto-resolver?** N/A (operator intervention).

### 8.12 Action-mapping layer — default resolution per category

The table below is the dispatch contract. RC-007 makes it normative; RC-008 makes auto-resolvers mandatory for auto-resolver categories.

| Category | Default action | Investigator spawned? | Typical verdict (if investigator) | Auto-resolver present? |
|---|---|---|---|---|
| Cat 0 (infra unavailable) | halt classification + `degraded` status | No | — | Yes (wait-and-retry) |
| Cat 1 (idempotent rerun) | auto-resume by re-spawning | No | — | Yes (spawn the node) |
| Cat 2 (non-idempotent in-flight) | investigator workflow | Yes | `resume-with-context` / `reset-to-checkpoint` / `reopen-bead` | No (investigator required) |
| Cat 3 (generic store disagreement) | investigator workflow | Yes | `accept-close-with-note` / `reopen-bead` / `no-op-accept` | No (escalates through investigator) |
| Cat 3a (torn Beads write) | auto-resolve via adapter re-issue | No | — | Yes ([beads-integration.md §4.10]) |
| Cat 3b (verdict-unexecuted) | auto-resolve via RC-026 re-execution | No | — | Yes (re-run verdict action) |
| Cat 3c (inverse premature-close) | auto-verdict `accept-close-with-note` + mechanical close | No | — | Yes (direct close-write) |
| Cat 4 (recoverable known state) | auto-resume with pending action | No | — | Yes (re-arm retry/gate) |
| Cat 5 (clean restart) | normal startup; proceed to `ready` | No | — | Yes (no-op) |
| Cat 6a (integrity, LLM-triageable) | investigator workflow | Yes | `escalate-to-human` (usually) | No (investigator required) |
| Cat 6b (integrity, mechanically unrecoverable) | auto-escalate without investigator | No | — | N/A (operator intervention) |

> INFORMATIVE: Dual-table ownership. [spec.md §8.12] (this table) is **authoritative on semantics** — the prose interpretation of each category's default action, escalation path, and investigator handoff. [schemas.md §6.3] is **authoritative on mechanical dispatch** — the tabular schema consumed by the daemon's action-mapping code and lint suite. The two MUST stay in sync; divergence is a lint failure. When the two disagree, the semantics side (§8.12 here) governs the specification's meaning; implementers adjust the mechanical table to match.

### 8.13 Failure-commit deferral

Reconciliation does NOT require a git commit for every failed transition. Failure events per [event-model.md §8] record the failure; checkpoints record successful durable states per [execution-model.md §4.5]. Revisit if the improvement loop later needs `git bisect` over failures. If that need materializes, failure-commits become an additive change (a new optional kind of checkpoint commit) without breaking the current contract. Tracked in OQ-RC-001.

## 4. Normative requirements

Per the `spec-shape: taxonomy-first` declaration in front matter, §8 (category taxonomy) appears above this section and is the center of gravity; requirements below reference §8 categories by number and sub-letter. Subsection numbering under §4 is per-spec.

### 4.1 Reconciliation-as-workflow

#### RC-001 — Reconciliation runs as a harmonik workflow

Reconciliation MUST run as a normal harmonik workflow: DOT-defined per [execution-model.md §4.1], dispatched deterministically by the daemon, and event-logged per [event-model.md §4.1] and the per-event subsections under [event-model.md §8]. There MUST NOT be a separate reconciliation subsystem. Each investigator-required category (see §8) has its own reconciliation workflow in the S01-shipped library (see RC-004).

> INFORMATIVE: The daemon emits a `reconciliation_started` event (class O per [event-model.md §8.6.1]) at the boundary where each reconciliation workflow is dispatched; the `trigger` field discriminates startup-scan, on-demand, and scheduled-hourly invocations (RC-020a).

Tags: mechanism

#### RC-002 — Reconciliation workflows emit exactly one checkpoint commit

A reconciliation workflow MUST emit exactly one checkpoint commit — the **verdict commit** — on the investigator-run's task branch. The verdict commit MUST carry the `reconciliation_verdict_emitted` event's payload (per §6 and [schemas.md §6.1]) plus any evidence the investigator produced under `.harmonik/reconciliation/<investigator_run_id>/`. Intermediate state transitions within the reconciliation workflow MUST NOT be checkpointed. This is an explicit exception to [execution-model.md §4.5 EM-023] and is keyed on the workflow-library metadata tag `workflow_class = reconciliation` (declared in [schemas.md §6.5] as RC's extension to the Workflow record owned by [execution-model.md §4.1]; acceptance by EM is tracked in OQ-RC-002).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### RC-003 — Bounded recursion follows from the verdict-only commit rule

A daemon crash during reconciliation MUST leave no mid-investigation durable state. On restart, the outer run whose reconciliation was interrupted MUST be re-classified from its original category against unchanged git + Beads state, and a fresh reconciliation workflow MUST be dispatched. Reconciliation-workflow nodes MUST NOT be classified under §8 Cat 1 or Cat 2; the recursion-bounding rule of RC-002 ensures no in-flight investigator state exists for a detector to classify.

Tags: mechanism

#### RC-002a — At most one reconciliation workflow per target run

For any given `target_run_id`, at most ONE reconciliation workflow MUST be in-flight at a time. The daemon MUST hold a per-run reconciliation lock at `.harmonik/reconciliation-locks/<target_run_id>.lock`, acquired via `flock(LOCK_EX|LOCK_NB)` per the fd-lifetime advisory-lock primitive of [process-lifecycle.md §4.1 PL-002a]. The kernel releases the lock automatically on daemon-process termination so that a subsequent daemon invocation can acquire the lock without operator intervention. The lock MUST be acquired before the reconciliation workflow's first emission (RC-013 category-assigned) and MUST be released ONLY after one of the following terminal states:

- Verdict-executed commit lands per RC-025 (success path).
- Budget exhaustion per RC-018 fires the fallback verdict (escalate-to-human).
- Malformed verdict per RC-023 routes to fallback.
- Investigator-process crash detected via watcher per [handler-contract.md §4.3 HC-011].
- Operator pauses the daemon mid-investigation; lock is released on `pausing` entry per [operator-nfr.md §4.7 ON-027].

A second reconciliation dispatch targeting the same `target_run_id` MUST attempt `flock(LOCK_EX|LOCK_NB)`; on `EWOULDBLOCK`, the second dispatch MUST emit `reconciliation_dispatch_deduplicated` (cross-spec coordination request to EV: add this event to §8.6) and skip without re-classification (NOT route to Cat 4 — Cat 4 is for retry/backoff state, not for lock contention; semantic hijack noted by R2).

On daemon startup, the orphan-sweep procedure of [process-lifecycle.md §4.2 PL-006] MUST enumerate `.harmonik/reconciliation-locks/*.lock` and remove stale lock files (lock not held; PID-of-creator no longer alive per `kill(pid, 0)` probe). Stale-lock detection follows the same pattern as PL-002a's stale-pidfile detection. Two concurrent investigators producing verdicts for the same target run is a structural violation and MUST be routed through RC-023 malformed-verdict handling with reason `multiple-verdicts`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### RC-002b — Lock acquisition and verdict-executed-commit are NOT atomic

Lock acquisition (RC-002a) and the verdict-executed-commit emission (RC-025 + schemas.md §6.4) are two physically distinct write operations and CANNOT be made atomic. On daemon startup, a stale lock file at `.harmonik/reconciliation-locks/<target_run_id>.lock` whose target run's investigator task branch carries a `Harmonik-Verdict-Executed: true` commit MUST be deleted (the lock outlived its useful purpose). A stale lock file whose investigator task branch carries NO verdict-executed commit MUST route the target run through Cat 3b (verdict-emitted-but-unexecuted) per §8.5.

Tags: mechanism

#### RC-003a — Category orthogonality via priority-ordered first-match

Detection categories are NOT mutually exclusive on evidence: a single in-flight run may satisfy the detection rules of multiple §8 categories. Detectors MUST apply the following priority order and emit the first category whose rule fires:

    Cat 0 → Cat 6b → Cat 6a → Cat 5 → Cat 3c → Cat 3b → Cat 3a → Cat 3 → Cat 2 → Cat 4 → Cat 1

Rationale: infrastructure and integrity evidence (Cat 0, 6b, 6a) dominates because the lower-priority detectors cannot be trusted in their presence; clean-restart (Cat 5) dominates reopened-run orphans; specific Cat 3 sub-cases (3c, 3b, 3a) dominate generic Cat 3; non-idempotent in-flight (Cat 2) dominates recoverable retry state (Cat 4) because the investigator rubric for Cat 2 subsumes Cat 4's auto-resume for non-idempotent work; idempotent-rerun (Cat 1) is terminal in the order because it is the cheapest auto-resume and should not mask higher-severity evidence. This rule resolves the Cat 3 vs Cat 6a "workspace missing" tension and the Cat 3c vs Cat 3b "verdict-unexecuted on a merged run" tension; both route by priority.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### RC-003b — Crashed reconciliation-workflow branch classification

A reconciliation workflow whose task branch exists but whose investigator subprocess died mid-run (no verdict commit yet emitted) MUST be classified as Cat 5 (clean re-dispatch) on subsequent reconciliation cadence ticks, NOT Cat 6a (multiple task branches). The discriminator is the `Harmonik-Workflow-Class: reconciliation` trailer per RC-002 (cross-spec coordination request to EM: add the trailer to the trailer registry; tracked as OQ-RC-002 — already exists). RC-003a's priority order applies; this rule scopes the Cat 5 vs Cat 6a tiebreak for the reconciliation-workflow case.

Tags: mechanism

#### RC-004 — Reconciliation workflow library is S01-owned

The reconciliation workflow library — the concrete DOT workflows and YAML policies that implement investigator-required categories (Cat 2, Cat 3 generic, Cat 6a per §8) — MUST be owned by S01 (Orchestrator Core) and ship as part of the S01 package. S01 MUST ship: (a) a DOT workflow per investigator-required category; (b) YAML policies naming the wall-clock budget (§4.4 RC-017) and the skill-injection set (investigators require Beads-CLI and git-inspection skills at minimum per [control-points.md §4.11] and [handler-contract.md §4.11]); (c) the investigator-agent prompt templates.

Tags: mechanism

#### RC-005 — Detectors and verdict-execution mechanics are NOT in the workflow library

The §4.3 detectors MUST live in the daemon's Go code (mechanism-tagged functions), NOT in the S01 workflow library. The §4.5 verdict-execution mechanics (action dispatch, verdict-executed commit emission, idempotency-key adapter calls) MUST also live in the daemon's Go code. The S01 library owns investigator reasoning only.

Tags: mechanism

#### RC-006 — Upgrade discipline — daemon code and library ship together

A new reconciliation category (added via the amendment protocol per [architecture.md §4.6]) MUST ship a daemon-code change (detector + action-map entry per §8 taxonomy) AND a workflow-library addition in S01 (for investigator-required categories) in the same harmonik release. Split releases are forbidden.

Tags: mechanism

### 4.2 Action-mapping dispatch

#### RC-007 — Action-mapping is the dispatch contract; taxonomy is the detection contract

The §8 category taxonomy MUST classify *what went wrong*; the action-mapping table in §8.12 MUST specify *what the daemon does by default* for each class. The default action is normative: any deviation (e.g., a policy override routing Cat 1 through an investigator instead of auto-resume) MUST be declared in the reconciliation workflow's YAML with rationale.

Tags: mechanism

#### RC-008 — Auto-resolver categories MUST have a deterministic resolver

Every category whose default action in §8.12 is an auto-resolver (Cat 0 wait-and-retry, Cat 1 re-spawn, Cat 3a adapter re-issue, Cat 3b verdict re-execution, Cat 3c direct close-write, Cat 4 retry/gate re-arm, Cat 5 no-op, Cat 6b operator escalation) MUST have a deterministic implementation in the daemon's Go code. Investigator-required categories (Cat 2, Cat 3 generic, Cat 6a) MUST have a playbook per §4.4 RC-015.

Tags: mechanism

#### RC-009 — Taxonomy shape is settled

The 11-category detection taxonomy plus §8.12 action-mapping is the shape, resolved 2026-04-24 per user decision. Authoring agents MUST NOT re-open the 3-action-vs-11-category framing. Any future amendment MUST follow the protocol of [architecture.md §4.6].

Tags: mechanism

### 4.3 Detectors

#### RC-010 — Detectors operate on runs, not beads

Detectors MUST classify each in-flight run independently. A bead with multiple runs over its lifetime (per [execution-model.md §4.3 EM-014]) MUST have each run classified separately. Detectors MUST filter checkpoints by `Harmonik-Run-ID` trailer, NOT by matching on `Harmonik-Bead-ID`. An orphaned task branch from a prior run whose bead has since been re-claimed MUST classify as Cat 5 for the old run.

Tags: mechanism

#### RC-011 — Ordering uses git DAG parentage and UUID v7, not wall clock

Detectors MUST order checkpoints within a run's task branch by **git DAG parentage** (parent-pointer chain) and events by **UUID v7 `event_id`** per [event-model.md §4.1]. Wall-clock timestamps (`timestamp_wall`, git commit `author_date` / `committer_date`) MUST NOT drive classification decisions. The most-recent checkpoint for a run MUST be identified as the tip of the run's task branch (the commit with no child in the run's branch-subgraph), NOT the commit with the latest wall-clock timestamp.

Tags: mechanism

#### RC-012 — Cat 0 pre-check runs before any other detector

Before any §8 category detector executes, the daemon MUST verify infrastructure prerequisites per §8.1:

- `br --version` returns successfully within timeout T (T = 5 seconds by default) AND reports a version compatible with the pin per [beads-integration.md §4.8];
- a trial `br list --limit 1` or equivalent returns without error;
- `git rev-parse HEAD` succeeds;
- `.harmonik/` is writable.

Any prerequisite failure MUST halt classification. The daemon MUST emit `infrastructure_unavailable` per [event-model.md §8] naming the specific prerequisite that failed, MUST transition to `degraded` status per [process-lifecycle.md §4.2], and MUST retry at a configurable cadence. No in-flight run is classified until Cat 0 clears.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### RC-012a — Post-`ready` Cat 0 does not transition daemon state

A Cat 0 prerequisite failure observed AFTER the daemon has reached `ready` (per [process-lifecycle.md §4.3 PL-009]) MUST emit `daemon_degraded{reason=infrastructure_unavailable}` per [event-model.md §8.7.5] and surface the failing prerequisite via [operator-nfr.md §4.9 ON-037] health probe, but MUST NOT transition the §6.1 daemon-status enum from `ready` to `degraded`. Daemon-level `degraded` enum entry is reserved for pre-`ready` Cat 0 failures per PL-010. This carve-out exists because reentrant `degraded` would tangle operator-control with reconciliation per the PL-010 NOTE.

Tags: mechanism

#### RC-013 — Detector emits `reconciliation_category_assigned`

After classifying an in-flight run into a §8 category, a detector MUST emit a `reconciliation_category_assigned` event per [event-model.md §8] carrying the run_id, assigned category, and detection-rule name. Emission MUST precede any dispatch of a reconciliation workflow or auto-resolver. Consumers of `reconciliation_category_assigned` MUST tolerate duplicate emissions (e.g., from crash-restart re-dispatch); dedup key is `(target_run_id, category, snapshot_token.git_head_hash)`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### RC-014 — JSONL divergence-evidence scope is bounded

Detectors and investigators MAY read JSONL for the permitted uses below; all other uses are forbidden.

**Permitted.**

- Detecting that a checkpoint commit referenced in a JSONL `checkpoint_written` event is missing from git (triggers Cat 6b).
- Detecting that JSONL is corrupt / unparseable past a byte offset (triggers Cat 6b).
- Detecting that a `transition_event` exists in JSONL but no corresponding transition-record file exists in the checkpoint tree referenced by the event (triggers Cat 3 per [execution-model.md §4.4 EM-018]).
- Cat 2 liveness probe (per §8.3): a controlled, indexed query answering "has `run_completed` or `run_failed` been emitted for this run since the last checkpoint?" The query MUST be issued against the bounded divergence-evidence reader, NOT a live-follow tail. Supplying observational context to an investigator agent so it can reason about the sequence of events leading to divergence.

**Forbidden.** A detector MUST NOT:

- Use JSONL as the source of last-known `run_id`, `state_id`, or `transition_id` for any in-flight run. Those are derived from git per [execution-model.md §4.7].
- Use JSONL to decide which bead is in-flight. Beads (queried via `br`) is authoritative per [beads-integration.md §4.7].
- Reconstruct the `state` or `transition` object from JSONL payloads.

On detecting divergence, the detector MUST emit `store_divergence_detected` per [event-model.md §8.6.8] with a `corroboration` value of either `git-corroborated` or `beads-corroborated` per EV-023a; observations corroborated by neither store route to `divergence_inconclusive` per [event-model.md §8.6.10] and classify as Cat 6a (post-emission readability blocker) or Cat 6b per RC-003a priority order. The investigator workflow for the resulting Cat MUST consume this event, not the JSONL tail directly.

Tags: mechanism

#### RC-019a — Evidence-corroboration contract for store-divergence events

Every `store_divergence_detected` emission MUST adhere to EV-023a per [event-model.md §4.5]: the detector MUST classify the candidate divergence into the `corroboration` enum value (`git-corroborated | beads-corroborated`) per [event-model.md §8.6.8] payload. Single-source observations whose corroboration cannot be established MUST emit `divergence_inconclusive` per [event-model.md §8.6.10] instead, NOT `store_divergence_detected`. Cat 6b (post-emission corroboration impossible) is reachable when neither git nor Beads is readable; Cat 6b emissions are exempt from corroboration via the dedicated escalation path of §8.11.

Tags: mechanism

#### RC-020a — Detector cadence

Detectors MUST run at three dispatch points:

- (a) **Daemon startup.** Full scan of in-flight runs once the orphan sweep of [process-lifecycle.md §4.2 PL-005] has completed and before the daemon transitions to `ready`.
- (b) **On-demand operator command.** Operator surface (`harmonik reconcile [--run <run_id>]`) triggers a scoped detector run; grammar tracked in OQ-RC-005.
- (c) **Scheduled cadence.** Background scan at a configurable interval; MVH default is **hourly**, configurable via operator YAML per [operator-nfr.md §4.3]. Post-MVH cadence tuning is tracked in OQ-RC-004.

Detectors MUST be idempotent across dispatch points: re-running a detector on the same `(target_run_id, snapshot)` MUST produce the same category assignment. Concurrent detector runs for the same `target_run_id` are serialized by the registry lock of RC-002a.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### RC-020b — Detector panic recovery

A detector that panics during evaluation per RC-003a's first-match priority order MUST be caught by a per-detector `recover()` barrier (per [process-lifecycle.md §4.6 PL-018a]'s per-goroutine recover obligation extended to detector functions). On panic, the detector is suspended for the daemon's lifetime and the priority-order evaluation falls through to the next detector. A diagnostic event `reconciliation_detector_panic{detector_class, error_class}` (cross-spec coordination request to EV) MUST emit before fall-through.

Tags: mechanism

### 4.4 Investigator-agent contract

#### RC-015 — Investigator inputs are bound by snapshot token

The investigator subprocess receives a standard LaunchSpec per [handler-contract.md §6.1] (`agent_type = claude-code`, `role = Researcher`). The `LaunchSpec.snapshot_token` field, declared as `String | None` in HC-006, MUST carry the JSON-serialized form of the SnapshotToken record per [schemas.md §6.1] (`{git_head_hash, beads_audit_entry_id, captured_at_timestamp}`).

The investigator does NOT receive a pre-assembled `InvestigatorInput` payload. The `InvestigatorInput` shape declared in [schemas.md §6.1] is a documented LOGICAL VIEW the investigator constructs at runtime by querying:

- Its Beads-CLI skill per [beads-integration.md §4.9 BI-027/BI-028] for the `BeadRecord` as-of `beads_audit_entry_id`.
- Its git-inspection skill (delivered per RC-016 playbook) for the checkpoint commit + transition-record sibling at `git_head_hash`.
- Its workspace-inspection skill for the worktree state.
- The bounded JSONL divergence-evidence reader (RC-014) for the JSONL tail since the last checkpoint.

The snapshot token IS the bounding discipline: any read whose authority precedes `git_head_hash` or `beads_audit_entry_id` is in-scope; reads beyond MUST be classified as out-of-scope per RC-014 forbidden-uses. The investigator's playbook (per RC-016) declares which of the listed inputs the investigator reads; the daemon does NOT pre-assemble an `InvestigatorInput.json` file.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### RC-015a — Investigator is an HC handler

The investigator subprocess is a handler-contract handler per [handler-contract.md §4.1]. Its launch follows the standard handler-launch sequence: dispatcher constructs LaunchSpec per HC-006, calls the launch primitive, watches via HC-011 for outcome emission. The `agent_type` value `claude-code` and `role` value `Researcher` are the canonical pair for the investigator function (see also CP-039). Reconciliation-specific behavior beyond the standard launch is captured in the investigator playbook (RC-016).

Tags: mechanism

> INFORMATIVE: The investigator delegates to a Claude Code agent (role: `investigator`), model-class per the YAML policy attached to the reconciliation workflow (S01-shipped per RC-004). Input is bounded by SnapshotToken (RC-015); output is the `VerdictEvent` in [schemas.md §6.1] emitted via the standard outcome envelope (RC-022a).

#### RC-016 — Investigator playbook per category

For each category with an investigator (§8.3 Cat 2, §8.4 Cat 3 generic, §8.11 Cat 6a), the S01-shipped YAML policy MUST define a **playbook**: a specific ordered sequence of checks producing evidence for the verdict. Playbooks MUST name the investigator's expected evidence outputs (captured in the reconciliation commit) and the verdict-selection rubric.

Tags: cognition
Axes: llm-freedom=bounded; io-determinism=best-effort; replay-safety=unsafe; idempotency=non-idempotent

#### RC-017 — Every reconciliation workflow declares a wall-clock budget

Every reconciliation workflow (per RC-001) MUST declare a wall-clock budget: a hard ceiling, measured from the workflow's `run_started` event to its terminal event, beyond which the daemon forcibly terminates the workflow. The budget MUST be declared as a YAML policy field `wall_clock_seconds` (positive integer, required) attached to the reconciliation workflow's DOT via `budget_ref` per [control-points.md §4.5]. In the absence of an explicit budget, the S01-shipped packaging (RC-004) MUST supply a per-category default:

- Cat 2 default: 600 seconds.
- Cat 3 generic default: 300 seconds.
- Cat 6a default: 900 seconds.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### RC-018 — Budget exhaustion terminates with fallback verdict

On wall-clock budget exhaustion, the daemon MUST:

- emit `reconciliation_budget_exhausted` (payload schema in [schemas.md §6.1] `BudgetExhaustedPayload`; WHEN/WHY contract here, WHAT contract in [event-model.md §8]);
- issue a **default verdict of `escalate-to-human`** on the outer (target) run. This verdict MUST be indistinguishable from an investigator-emitted `escalate-to-human` in the operator-facing surface;
- kill the investigator subprocess per [handler-contract.md §4.6] cleanup rules;
- NOT write a reconciliation commit. The budget-exhausted event + daemon-default verdict are the durable trace.

On budget-exhaustion, the daemon MUST: (1) terminate the investigator subprocess (SIGTERM, then SIGKILL after [handler-contract.md §4.3 HC-018] interval); (2) wait for watcher-observation of process termination per HC-011; (3) emit `budget_exhausted` (class F per [event-model.md §8.4.3]); (4) emit the fallback `escalate-to-human` verdict (class F per RC-021); (5) the verdict-executor (RC-025a) consumes the fallback as if it were investigator-emitted. Steps (3) and (4) are NOT atomic but each is fsync-boundary; on crash between them, the next daemon startup detects the budget-exhausted event with no subsequent verdict commit and routes through Cat 3b retry cap (RC-026a).

Because reconciliation workflows are not checkpointed mid-investigation (RC-002), budget exhaustion MUST NOT leave an in-flight reconciliation state for re-classification.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### RC-019 — Investigator captures WIP before emitting `reopen-bead`

Before emitting a `reopen-bead` verdict, the investigator MUST capture any recoverable WIP from the outer run's worktree into the reconciliation commit. Concretely, the investigator MUST: (a) run `git status --porcelain` and enumerate untracked files in the worktree; (b) capture a diff plus file listing; (c) include the capture in the reconciliation commit's body and/or as annotated files under `.harmonik/reconciliation/<investigator_run_id>/wip-capture/`. This obligation is mandatory for `reopen-bead` verdicts and OPTIONAL for other verdicts (which keep the worktree and retain WIP by default).

Tags: cognition
Axes: llm-freedom=bounded; io-determinism=best-effort; replay-safety=unsafe; idempotency=non-idempotent

### 4.5 Verdict vocabulary and execution

#### RC-020 — Verdict vocabulary is the seven-value enum

A verdict event's `verdict` field MUST be exactly one of the seven enum values listed in [schemas.md §6.1 Verdict]: `resume-here`, `resume-with-context`, `reset-to-checkpoint`, `reopen-bead`, `accept-close-with-note`, `no-op-accept`, `escalate-to-human`. Every verdict MUST have defined mechanical semantics (see [schemas.md §6.2] Verdict-execution table; [spec.md §8.12] is authoritative on prose semantics while [schemas.md §6.3] is authoritative on mechanical dispatch dispatchability). Any other value is a malformed verdict and MUST be handled per RC-023.

> INFORMATIVE: `no-op-accept` is the verdict for "investigator confirms current state is legitimate; no action required." It differs from `accept-close-with-note` in that the bead is NOT closed — the run continues in its current state. Typical Cat 5-adjacent cases where initial evidence looked like divergence but reasoning concludes the state is consistent.

Tags: mechanism

#### RC-021 — Exactly one verdict event per reconciliation workflow

A reconciliation workflow MUST emit exactly one `reconciliation_verdict_emitted` event over its lifetime. The emission of the first verdict event MUST mark the workflow terminal; any subsequent verdict event from the same workflow is a structural violation and MUST be handled per RC-023 with malformation reason `multiple-verdicts`.

Tags: mechanism

#### RC-022 — Verdict commit lands on investigator's task branch

The verdict event's emission MUST be atomic with the investigator's verdict commit: a single `git commit` on the investigator's task branch carrying the verdict event's payload (per [schemas.md §6.1]) and any evidence files under `.harmonik/reconciliation/<investigator_run_id>/`. No intermediate checkpoints are written per RC-002. Resolution of `resume-here` / `resume-with-context` dispatch targets (which node to re-spawn on the outer run) MUST cite the orchestrator's current-node resolution contract in [execution-model.md §7.1] and, for worktree interactions on `reset-to-checkpoint`, [workspace-model.md §4.4] (worktree reset) and [workspace-model.md §4.6] (merge).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### RC-022a — Verdict emission via outcome envelope

The investigator subprocess emits its verdict by writing a structured outcome via the standard handler-contract outcome path (HC-008 `outcome_emitted`). The outcome's `outcome_kind` MUST be `reconciliation_verdict`; the outcome payload MUST be the `VerdictEvent` record per [schemas.md §6.1]. The handler-contract watcher (HC-011) consumes the outcome event and surfaces the VerdictEvent to the daemon's reconciliation-verdict-executor (RC-025).

The investigator subprocess does NOT directly write the verdict commit; the daemon's verdict-executor MUST construct and commit the verdict-and-verdict-executed commit pair on the investigator's task branch. This separation ensures verdict-emission and verdict-execution share a single transactional boundary owned by the daemon.

Cross-spec coordination request to EM: extend the `Outcome` record in [execution-model.md §6.1] (or add a new outcome variant) to carry the verdict envelope. Tracked as new OQ-RC-010.

Tags: mechanism

#### RC-023 — Malformed-verdict handling produces a fallback escalation

On any of the following malformation conditions, the daemon MUST:

- emit `reconciliation_verdict_malformed` (payload shape declared in [schemas.md §6.1 MalformedVerdictPayload]; `MalformationReason` enum declared in [schemas.md §6.1]);
- issue a **fallback verdict of `escalate-to-human`** on the target (outer) run;
- terminate the reconciliation workflow (kill the investigator subprocess);
- NOT attempt to interpret the malformed payload.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

> RATIONALE: Trusting LLM-generated text to conform to a schema is a ZFC violation at the classification layer. The malformed-verdict path converts cognitive failure into a deterministic escalation. See [architecture.md §4.2].

#### RC-024 — Verdict staleness check precedes execution

Before executing a verdict per RC-025, the daemon MUST re-capture the current `(git_head_hash', beads_audit_entry_id')` and compare against the snapshot token captured at investigator-dispatch time (RC-015). The verdict is **stale** if either:

- the target run's checkpoint trail has gained a new commit since the snapshot, OR
- the target run's bead has changed status in the Beads audit log since the snapshot.

On staleness, the daemon MUST: emit `reconciliation_verdict_stale` (WHEN/WHY here; payload schema `StaleVerdictPayload` in [schemas.md §6.1]; WHAT contract owned by [event-model.md §8]); NOT execute the verdict; re-dispatch a fresh reconciliation workflow against the new state.

Changes to sibling beads or to the daemon's JSONL event log MUST NOT trigger staleness. Only changes to the target run's git branch or the target bead's Beads audit entries count.

Tags: mechanism

#### RC-025 — Verdict execution is durable and idempotent

When a reconciliation workflow emits a verdict event (per RC-020) and the staleness check of RC-024 passes, the daemon MUST:

- perform the verdict's mechanical action per the verdict-execution table ([schemas.md §6.2]);
- emit `reconciliation_verdict_executed` (WHEN/WHY here; payload shape declared in [schemas.md §6.1 VerdictExecutedPayload]);
- append a second commit to the investigator's task branch carrying the trailer `Harmonik-Verdict-Executed: true` (payload-free presence-only marker; trailer shape declared in [schemas.md §6.4] as RC-owned since the EM v0.2.0 trailer rollback).

Each verdict's mechanical action MUST be idempotent:

- `reopen-bead` → the daemon's verdict-executor invokes the BI-CLI adapter's reopen path per [beads-integration.md §4.4 BI-010] (binding from `reopen-bead` verdict to `reopen` op per BI-010a status-mapping table); the adapter's BI-031 status-check-before-reissue protocol per [beads-integration.md §4.10] handles idempotency.
- `resume-here`, `resume-with-context`, `reset-to-checkpoint` → dispatching the outer run's next node is idempotent at the dispatch layer (the next dispatch check sees the outer run already running and does not re-dispatch).
- `accept-close-with-note` → appends an annotation to the reconciliation commit and writes the close to Beads if not already closed; idempotent on re-run.
- `no-op-accept` → no mechanical action beyond emission of `reconciliation_verdict_executed` and the verdict-executed commit. The outer run is left untouched; subsequent dispatch cycles treat the run as ordinary.
- `escalate-to-human` → emits `operator_escalation_required`; the outer run remains in its current state-machine state (no synthesized `quarantined` state); subsequent emissions are deduplicated by `target_run_id`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### RC-025a — Daemon-side verdict-executor commits the verdict pair

The daemon-side verdict-executor (a deterministic Go subroutine, NOT a workflow node) consumes the VerdictEvent from RC-022a's outcome envelope and:

1. Validates the verdict per RC-020 / RC-023; on validation failure, routes through fallback per RC-023.
2. Re-captures the snapshot per RC-024 staleness check; on stale, routes through Cat 3b re-execution per §8.5.
3. Constructs and commits the `reconciliation_verdict_emitted` commit (verdict body + Harmonik-Verdict-ID trailer) on the investigator's task branch.
4. Mechanically applies the verdict's action per the table in [schemas.md §6.2].
5. Constructs and commits the `reconciliation_verdict_executed` commit with the `Harmonik-Verdict-Executed: true` trailer per [schemas.md §6.4] as a descendant of the verdict-emitted commit.
6. Emits `reconciliation_verdict_emitted` and `reconciliation_verdict_executed` events.
7. Releases the RC-002a lock per RC-002b.

The verdict-executor MUST be panic-safe (per [process-lifecycle.md §4.6 PL-018a]); on panic mid-step, the next daemon startup re-classifies via Cat 3b (verdict-emitted-but-unexecuted) per §8.5 and the verdict-executor re-runs idempotently.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### RC-026 — Verdict-execution discovery on restart

The startup detector of [process-lifecycle.md §4.2 PL-005] MUST treat a reconciliation workflow as resolved ONLY if both the verdict commit AND the verdict-executed commit (per RC-025) are present on the investigator's branch. A reconciliation workflow with a verdict commit but no verdict-executed commit MUST be classified as §8.5 Cat 3b with the dedicated auto-resolver re-attempting the verdict's mechanical action under a fresh staleness check (RC-024).

The daemon startup sequence MUST dispatch the reconciliation-workflow classification pass BEFORE any ordinary workflow dispatches (that is: reconciliation detectors run before the daemon transitions to `ready`; ordinary dispatch is gated behind detection completion). Fail-open reconciliation escalation — the question of whether the daemon should proceed to `ready` when reconciliation itself cannot classify (beyond Cat 0) — is tracked in OQ-RC-003.

Tags: mechanism

#### RC-026a — Cat 3b re-execution retry cap

A reconciliation Cat 3b verdict-execution that fails on a fresh staleness check (RC-024) MUST be retried with a durable attempt counter, recorded in `.harmonik/reconciliation-attempts/<target_run_id>.json` (atomic temp+rename+fsync per [workspace-model.md §4.7 WM-026]). The retry cap defaults to N=5; on cap exceeded, the run escalates to Cat 6b (operator escalation) per §8.11. Each retry MUST emit `reconciliation_verdict_execution_retry{target_run_id, attempt}` (cross-spec coordination request to EV).

Tags: mechanism

#### RC-027 — Operator verdict-override — spec-draft obligation

A per-reconciliation-workflow policy option MUST allow operators to pause the daemon's verdict-execution step until operator confirmation or veto via `harmonik confirm-verdict <run_id>` / `harmonik veto-verdict <run_id> [--promote-to escalate-to-human]`. Default: execution proceeds without operator confirmation. Operators opt in by policy. This obligation applies to all investigator-dispatched categories (Cat 2, Cat 3 generic, Cat 6a per §8.12).

Tags: mechanism

> INFORMATIVE: OQ-RC-002 tracks the EM workflow_class extension; the operator CLI grammar for confirm/veto is tracked under the operator-CLI spec per [operator-nfr.md §4.10].

### 4.6 Re-run vs. intra-run

#### RC-028 — `reopen-bead` verdict triggers a new run on subsequent claim

A `reopen-bead` verdict MUST clear the in-flight tracking for the target bead. A subsequent claim of the bead MUST produce a new run with a fresh worktree and a fresh branch per [workspace-model.md §4.9 WM-034]. The new run MUST receive a fresh `run_id`; continuation of the prior `run_id` after a `reopen-bead` verdict is forbidden.

Tags: mechanism

#### RC-029 — Intra-run rollbacks keep the worktree and run_id

A `reset-to-checkpoint` verdict is an intra-run rollback. The worktree and `run_id` MUST be preserved; the run reverts to the named checkpoint and re-runs from there per [execution-model.md §4.10 EM-044] (transition_kind + rollback_to_state_id representation).

Tags: mechanism

#### RC-030 — Reconciliation does NOT drive intra-run loops

Intra-run loops (workflow edges routing back to an earlier node) are NOT produced by reconciliation. Those loops are ordinary workflow-graph traversal handled by edge conditions and Guard/Gate control-points per [control-points.md §4.2, §4.4]. Reconciliation handles only restart and store-divergence cases.

Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

Tags: mechanism

### 4.7 Testing obligation

#### RC-031 — Crash-recovery-tested

Every detector under §4.3 / §8, every verdict-execution path under §4.5, and every staleness-check path under RC-024 MUST be exercised by at least one crash-recovery scenario test before landing. The crash-recovery test layer is described in `docs/methodology/TESTING.md`; this obligation migrates to a `[testing.md §<layer>]` cross-reference within one revision cycle after `testing.md` is finalized (tracked in OQ-RC-006).

Tags: mechanism

## 5. Invariants

Per AR-042, an invariant must be (a) non-trivially derivable from a single requirement, (b) cross-requirement / cross-subsystem in scope, and (c) mechanically sensed. Narrowed invariants follow. Retired IDs are preserved as stubs; requirement-grade rules remain in §4 at their original IDs.

#### RC-INV-001 — Reconciliation-as-workflow uniqueness across daemon lifecycle

Across every daemon lifetime, every reconciliation dispatch — including budget-exhausted fallbacks, malformed-verdict fallbacks, Cat 3b re-executions, and Cat 3c auto-verdicts — MUST run as a DOT-tagged workflow with `workflow_class = reconciliation` and MUST emit at most one `reconciliation_verdict_emitted` event per dispatch (RC-021). At no point during a daemon's lifetime may reconciliation "ride on top of" ordinary workflow state (i.e., a non-reconciliation workflow node MUST NOT invoke the verdict-execution mechanics of RC-025). This invariant cuts across RC-001, RC-002, RC-002a, RC-021, and RC-025, and closes the "could an agent in a normal workflow issue a verdict" ambiguity.

- **Sensor.** (a) Daemon MUST tag the Workflow record in its registry with `workflow_class`; startup audit log samples emitted workflows and asserts every `reconciliation_verdict_*` event traces back to a Workflow whose `workflow_class = reconciliation`. (b) JSONL query at audit time filters `reconciliation_verdict_emitted` events and joins against Workflow registry; any event whose source workflow's class is NOT `reconciliation` fails the audit. (c) Per-daemon-lifecycle audit log sample: one tag check per N verdicts (default N=10) at runtime, full scan at shutdown. Upstream-invariant cite per F-refs-EV-6 v0.8 sensor→sensor extension: **[execution-model.md §5 EM-INV-005]** — "git wins on completion" is the execution-model invariant this sensor enforces from the reconciliation side; the `rc-inv-001 → em-inv-005` cross-spec sensor→sensor edge closes the ambiguity that a JSONL event alone could override git-authoritative completion state.

Tags: mechanism

#### RC-INV-002 — [retired]

Retired 2026-04-24 under AR-042: restates RC-002 + RC-003 (verdict-only commit rule + re-classification-on-restart). Both rules remain normative at their original requirement IDs in §4.1. No semantic loss.

#### RC-INV-003 — [retired]

Retired 2026-04-24 under AR-042: restates RC-002 + RC-025 + RC-026 (the verdict-emitted + verdict-executed commit pair IS the durable record, and the re-discovery contract). Requirements remain normative at their original IDs in §4.5.

#### RC-INV-004 — Evidence-corroboration guarantee across detector runs

Every `store_divergence_detected` emission traced through any detector dispatch (startup, on-demand, scheduled per RC-020a) MUST carry a non-`inconclusive` `corroboration` value per EV-023a. This invariant cuts across detector dispatch points and the upstream EV contract: a single-source observation whose corroboration cannot be established MUST emit `divergence_inconclusive` instead. The invariant bounds false-positive investigator dispatches and is the reconciliation-side projection of [event-model.md §8.6.8] EV-023a.

- **Sensor.** Detector emission layer MUST validate `corroboration ∈ {git-corroborated, beads-corroborated}` before allowing the event to be written to JSONL; an `inconclusive` corroboration MUST route to `divergence_inconclusive` instead. Audit-log sample at daemon startup checks that every `store_divergence_detected` in the last N events carries a non-`inconclusive` corroboration.

Tags: mechanism

#### RC-INV-005 — [retired]

Retired 2026-04-24 under AR-042: restates RC-010 + RC-018's investigator-input contract. The `Harmonik-Run-ID`-trailer filtering rule remains normative at RC-010; the LaunchSpec-validation check on `InvestigatorInput` construction remains normative at RC-015. No semantic loss.

## 6. Schemas and data shapes

Full schemas live in [schemas.md](schemas.md). This section indexes the schemas defined for reconciliation and states the compatibility contract; concrete records, enums, and tables are in the sibling file.

- `VerdictEvent` — payload of `reconciliation_verdict_emitted`; see [schemas.md §6.1].
- `SnapshotToken` — bounded view captured at investigator dispatch; see [schemas.md §6.1].
- `InvestigatorInput` — the full input record passed to an investigator-agent; see [schemas.md §6.1].
- `MalformationReason` — enum for RC-023 handling; see [schemas.md §6.1].
- Detection-rule table — per-category detectors mapped to signals; see [schemas.md §6.3].
- Action-mapping table — category-to-default-action; see [schemas.md §6.3] (the canonical copy) and §8.12 (duplicated for taxonomy-first reading order).
- Verdict-execution table — enum-to-mechanical-action; see [schemas.md §6.2].

### 6.4 Schema evolution

Reconciliation event payloads (schemas in [schemas.md §6.1]; WHAT contract owned by [event-model.md §8]) are versioned via the event envelope's `schema_version` field per [event-model.md §6.3]. Verdict schema is N-1 readable per [operator-nfr.md §4.5]. A breaking verdict-enum change (renaming or removing a value) requires a migration release scheduled at an operator pause per [operator-nfr.md §4.3]. Additive changes (new optional evidence fields, new verdict-execution metadata) bump `schema_version` but do not break readers.

### 6.5 Co-owned event payloads

This spec's requirements drive emission of the following events whose payload schemas are owned by [event-model.md §8] per-event subsections and [event-model.md §6.3] payload registry:

- `reconciliation_category_assigned` — emitted after a detector classifies an in-flight run (RC-013).
- `reconciliation_verdict_emitted` — emitted when the investigator produces a verdict (RC-021); payload per [schemas.md §6.1 VerdictEvent].
- `reconciliation_verdict_executed` — emitted after the daemon's mechanical action lands (RC-025).
- `reconciliation_verdict_stale` — emitted when the staleness check fails (RC-024).
- `reconciliation_verdict_malformed` — emitted on schema violation (RC-023).
- `reconciliation_budget_exhausted` — emitted on wall-clock budget exhaustion (RC-018).
- `store_divergence_detected` — emitted on JSONL divergence-evidence detection (RC-014); subject to the corroboration invariant of RC-INV-004.
- `infrastructure_unavailable` — co-owned with [process-lifecycle.md §4.2] (PL-010: degraded-state transition on prerequisite failure); RC owns the Cat 0 emission trigger, PL owns the daemon-lifecycle response.
- `operator_escalation_required` — owned by this spec (RC-025 emits on `escalate-to-human` verdict). The operator-observable surface is delivered through the standard operator-event consumption path per [operator-nfr.md §4.1 ON-002]; ON does NOT own the event type itself.

This spec is normative for WHEN each event fires; [event-model.md §8] per-event subsections and [event-model.md §6.3] are normative for the payload shape. See §8.12 dual-table note: [spec.md §8.12] is authoritative on semantics (prose); [schemas.md §6.3] is authoritative on mechanical dispatch (tabular).

## 7. Protocols and state machines

### 7.1 Startup-reconciliation dispatch (protocol pseudocode)

```
FUNCTION reconcile_at_startup(project):
    -- Pre-check Cat 0 (RC-012)
    IF NOT cat0_pre_check(project):
        emit_event("infrastructure_unavailable", project, failed_prereq)
        daemon.status = "degraded"
        RETURN DEFER
    -- Walk git for checkpoints
    checkpoints = walk_git_for_run_trailers(project.repo)
    -- Query Beads for in-progress beads
    in_flight_beads = br_list(status="in_progress")
    -- Filter by run_id (RC-010)
    in_flight_runs = collect_runs_by_run_id(checkpoints, in_flight_beads)
    FOR run IN in_flight_runs:
        category = detect_category(run)                  -- §8 detectors
        emit_event("reconciliation_category_assigned", run, category)
        action = action_map[category]                     -- §8.12 table
        IF action.investigator_spawned:
            dispatch_reconciliation_workflow(run, category)   -- §4.4 playbook
        ELSE:
            invoke_auto_resolver(run, category)               -- RC-008
    daemon.status = "ready"
```

### 7.2 Verdict-execution sequence (protocol pseudocode)

```
FUNCTION execute_verdict(investigator_run, verdict_event):
    -- Schema-validate verdict (RC-020, RC-023)
    IF NOT valid_verdict_schema(verdict_event):
        emit_event("reconciliation_verdict_malformed", investigator_run, reason)
        fallback_verdict = "escalate-to-human"
        verdict_event = synthesize_fallback(investigator_run, fallback_verdict)
    -- Staleness check (RC-024)
    current = capture_snapshot(verdict_event.target_run_id)
    IF stale(verdict_event.snapshot_token, current):
        emit_event("reconciliation_verdict_stale", verdict_event, current)
        dispatch_fresh_reconciliation(verdict_event.target_run_id)
        RETURN STALE
    -- Execute mechanical action (RC-025, [schemas.md §6.2])
    action = verdict_action_map[verdict_event.verdict]
    action.execute(verdict_event)     -- idempotent; per-verdict semantics
    emit_event("reconciliation_verdict_executed", verdict_event)
    git.commit(investigator_run.task_branch, trailer("Harmonik-Verdict-Executed", "true"))
    RETURN OK
```

Every branch point above corresponds to a normative requirement: Cat 0 pre-check (RC-012), detector classification (RC-010, RC-013), action dispatch (RC-007, RC-008), schema validation (RC-020, RC-023), staleness (RC-024), mechanical execution (RC-025), verdict-executed commit (RC-025 and [schemas.md §6.4]).

## 9. Cross-references

### 9.1 Depends on

- **[execution-model.md §4.1]** — Workflow, Node, Edge types used by reconciliation DOT workflows; the Workflow record is extended by schemas.md §6.5 with `workflow_class` (acceptance tracked in OQ-RC-002).
- **[execution-model.md §4.2 EM-009]** — `idempotency_class` tag; consumed by Cat 1 and Cat 2 detectors.
- **[execution-model.md §4.3 EM-014]** — one-bead-many-runs rule; reconciliation verdicts (`reopen-bead`) trigger this per RC-028.
- **[execution-model.md §4.4 EM-016, EM-017, EM-018]** — checkpoint contract, trailer schema, transition-record sibling file; detectors read these.
- **[execution-model.md §4.5 EM-023, EM-026]** — checkpoint cadence; RC-002 is the reconciliation exception.
- **[execution-model.md §4.7]** — state reconstruction uses git + Beads only; informs RC-014 forbidden uses.
- **[execution-model.md §5 EM-INV-005]** — git wins on completion; RC-INV-001 cites this as the upstream execution-model invariant.
- **[event-model.md §4.1]** — event envelope shape; reconciliation events conform.
- **[event-model.md §4.3]** — event ordering (UUID v7); RC-011 cites this.
- **[event-model.md §4.5]** — observational-vs-state-reconstruction replay split; grounds RC-014 scope.
- **[event-model.md §6.3]** — payload registry; all reconciliation events cited in §6.5 are registered there.
- **[event-model.md §8]** — per-event subsections for all reconciliation events listed in §6.5.
- **[event-model.md §8.6.8, §8.6.10]** — EV-023a evidence-corroboration contract; RC-019a and RC-INV-004 are the reconciliation projection.
- **[handler-contract.md §4.6]** — error propagation and agent-subprocess cleanup; consumed by RC-018.
- **[handler-contract.md §4.11]** — skill injection; investigators receive Beads-CLI and git-inspection skills per RC-004.
- **[handler-contract.md §6.1]** — LaunchSpec record; `InvestigatorInput` in [schemas.md §6.1] is an adapted / extended LaunchSpec per RC-015.
- **[control-points.md §4.2]** — Guard control-point; cited by RC-030.
- **[control-points.md §4.4]** — Gate control-point; cited by RC-030.
- **[control-points.md §4.5]** — YAML policy surface for budget declarations (RC-017); `budget_ref` lives here.
- **[control-points.md §4.7]** — policy-ref surface for investigator policy attachment.
- **[control-points.md §4.11]** — skill-declaration surface consumed by investigator configuration.
- **[beads-integration.md §4.4]** — terminal-transition writes; `reopen-bead` and close verdicts route through here.
- **[beads-integration.md §4.7]** — store-authority rules; RC-INV-001 and RC-014 cross-reference.
- **[beads-integration.md §4.8]** — adapter layer / `br` CLI surface contract (BI-024-026); RC-012 cites for version-pin verification.
- **[beads-integration.md §4.10]** — idempotency contract / intent log / status-check-before-reissue (BI-029-032 + BI-031b); Cat 3a auto-resolver and RC-025 consume this.
- **[workspace-model.md §4.3]** — Lease-by-run; reconciliation verdicts trigger workspace-model lease behaviors (see §9.3).
- **[workspace-model.md §4.7]** — session-log CASS handle and re-run rule; RC-015 inputs and RC-028 consume this.
- **[process-lifecycle.md §4.2]** — Startup sequence / PL-005 orphan sweep and PL-010 degraded state; reconciliation dispatch ordering cites this.
- **[process-lifecycle.md §4.1 PL-002a]** — fd-lifetime advisory-lock (`flock`) primitive; RC-002a uses for `.harmonik/reconciliation-locks/<target_run_id>.lock`.
- **[process-lifecycle.md §4.2 PL-006]** — orphan sweep; RC-002a's stale-lock cleanup hooks here.
- **[operator-nfr.md §4.7 ON-027]** — pausing entry hook; RC-002a releases lock on operator pause.
- **[handler-contract.md §4.3 HC-011]** — handler-watcher outcome observation; RC-002a's investigator-crash detection consumes.
- **[operator-nfr.md §4.3]** — operator control state machine; pause carve-out for reconciliation workflows.
- **[operator-nfr.md §4.5]** — N-1 compatibility contract for verdict schema (§6.4).
- **[operator-nfr.md §4.1 ON-002]** — operator-event consumption path; the operator-observable surface for `operator_escalation_required` (event type owned by RC-025).
- **[operator-nfr.md §4.8 ON-031]** — restart RTO; reconciliation dispatch is accounted for in the RTO definition there.
- **[operator-nfr.md §4.10]** — multi-daemon / operator-CLI surface home for the RC-027 confirm/veto grammar.
- **[architecture.md §4.2]** — ZFC test; RC-023 rationale invokes this framing.
- **[architecture.md §4.6]** — amendment protocol; new reconciliation categories and verdict enum values require this (RC-006, RC-009).

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from the foundation corpus. Populated at finalize.

### 9.3 Co-references (read-only consumption)

- **[workspace-model.md §4.3 Lease-by-run]** — reconciliation verdicts trigger workspace-model lease behaviors; no normative dependency on workspace-model internals.
- **[workspace-model.md §4.7 Session-log / re-run rule]** — investigator inputs include the CASS-indexed session log when present (RC-015); `reopen-bead` triggers fresh-worktree creation there.
- **[operator-nfr.md §4.3 Operator control state machine]** — pause carve-out for reconciliation workflows; no normative dependency on the state-machine internals.
- **[operator-nfr.md §4.5 Checkpoint-format stability]** — N-1 compatibility contract for verdict schema (§6.4).
- **[operator-nfr.md §4.8 Restart RTO]** — reconciliation dispatch is accounted for in the RTO definition there.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforming to Core MVH MUST pass every requirement RC-001 through RC-031 (including the inserts RC-002a, RC-003a, RC-019a, RC-020a) and every invariant RC-INV-001 and RC-INV-004. RC-INV-002, RC-INV-003, RC-INV-005 are retired (not implementation obligations). No requirement is deferred at MVH.

**Post-MVH extensions.** The operator verdict-override CLI surface (RC-027) MAY ship as a follow-on within one release after MVH; it is required to claim Core MVH conformance only if operators have opted in via policy.

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose. Each requirement's test obligation:

- **RC-001 — RC-006 (reconciliation-as-workflow).** Structural tests verifying reconciliation workflows carry the `workflow_class = reconciliation` tag; integration tests verifying S01 packaging ships the required DOT + YAML per RC-004.
- **RC-007 — RC-009 (action-mapping dispatch).** Unit tests covering the §8.12 action table for each of the 11 categories; lint verifying the §8.12 table and [schemas.md §6.3] match.
- **RC-010 — RC-014 (detectors).** Crash-recovery scenario tests (RC-031) exercising each detector in §8; negative tests verifying detectors do not misclassify across run_ids (RC-010); tests verifying JSONL forbidden-uses (RC-014).
- **RC-017 (wall-clock budget declaration).** Structural tests verifying: (1) every S01-shipped reconciliation workflow DOT carries a `budget_ref` attribute on the workflow's root node or equivalent policy-attachment point; (2) the referenced Budget in the YAML policy has `resource = wall_clock_seconds` and `scope = per_run`; (3) when no explicit per-workflow budget is declared, the S01 packaging supplies the per-category defaults (Cat 2: 600s, Cat 3: 300s, Cat 6a: 900s); (4) negative test: a DOT that omits `budget_ref` and has no S01-packaging default MUST fail the workflow-library lint.
- **RC-015 — RC-019 (investigator contract).** End-to-end tests with twin handler: verify snapshot-token plumbing; wall-clock budget enforcement (RC-018); WIP-capture on `reopen-bead` (RC-019).
- **RC-020 — RC-027 (verdict and execution).** Schema-validation tests for every verdict enum value and every malformation reason (RC-023); staleness-detection tests (RC-024); verdict-execution idempotency tests (RC-025); restart-discovery tests for Cat 3b (RC-026).
- **RC-028 — RC-030 (re-run vs. intra-run).** Scenario tests verifying `reopen-bead` produces a new `run_id`; `reset-to-checkpoint` preserves `run_id`. **RC-030 (no intra-run loops from reconciliation).** Negative test: for every verdict enum value, verify that verdict execution does NOT produce a workflow edge routing back to a prior node; confirm that any loop-back transitions observed in a live run trace to edge conditions and Guard/Gate control-points (per [control-points.md §4.2, §4.4]), not to reconciliation verdict execution.
- **RC-031 (crash-recovery).** See `docs/methodology/TESTING.md` crash-recovery layer. Migration to `[testing.md §<layer>]` tracked in OQ-RC-006.

- **RC-002a (concurrent reconciliation).** Crash-recovery scenario: dispatch two reconciliation workflows against the same `target_run_id`; verify `flock(LOCK_EX|LOCK_NB)` on `.harmonik/reconciliation-locks/<target_run_id>.lock` causes the second dispatch to emit `reconciliation_dispatch_deduplicated` and skip without classification. Stale-lock recovery test: kill the daemon mid-investigation, restart, verify orphan-sweep cleans up the lock file via PL-006 and the next reconciliation cadence proceeds. **RC-002b (non-atomic recovery).** Stale-lock-with-verdict-executed test: place a lock file at the path while the investigator branch already carries `Harmonik-Verdict-Executed: true`; verify the lock is deleted on next startup and the run is NOT reclassified.
- **RC-003a (priority ordering).** Unit tests exercising each pair of overlapping detection rules (workspace-missing crossover Cat 3 vs Cat 6a; merged-run crossover Cat 3c vs Cat 3b; rate-limited-non-idempotent crossover Cat 2 vs Cat 4) verifying the declared first-match priority.
- **RC-019a (evidence corroboration).** Negative tests asserting detector emits `divergence_inconclusive` (NOT `store_divergence_detected`) when neither git nor Beads can corroborate the candidate divergence; positive tests verify `corroboration` enum value matches the corroborating store.
- **RC-020a (detector cadence).** Timer-mocked tests verifying scheduled-cadence dispatch at the hourly default; idempotency tests on re-running detectors over the same snapshot.
- **RC-INV-001 (reconciliation-as-workflow uniqueness).** Integration test spawning reconciliation workflows plus ordinary workflows concurrently; audit-log scan verifying every `reconciliation_verdict_*` event traces to a `workflow_class = reconciliation` Workflow.
- **RC-INV-004 (evidence-corroboration guarantee).** Audit test on a corpus of seeded divergence events verifying `corroboration ∈ {git-corroborated, beads-corroborated}` on every `store_divergence_detected`; single-source observations route to `divergence_inconclusive`.

### 10.3 Excluded conformance claims

- This spec does NOT grant conformance over: the specific DOT contents of S01-shipped reconciliation workflows (post-MVH subsystem spec); investigator-agent prompt quality (cognition-tagged, not mechanism-verifiable); the operator CLI surface beyond the per-command grammar named in RC-027 (owned by a separate operator-CLI spec per [operator-nfr.md §4.10]); the `br` CLI's internal idempotency guarantees (owned by [beads-integration.md §4.10] and the upstream Beads project).
- This spec does NOT guarantee performance or throughput bounds on reconciliation dispatch; those are operator-observable in [operator-nfr.md §4.8] (restart RTO) and are not requirements of this spec.

## 11. Open questions

#### OQ-RC-001 — Failure-commit additive extension

Question: When (if ever) should failed transitions emit checkpoint commits to enable `git bisect` over failures in the improvement loop? Reconciliation does not currently require them (§8.13).
Owner: foundation-author
Blocks: none (MVH decision: no failure commits)
Default-if-unresolved: No failure commits. Revisit when the improvement-loop spec lands and can demonstrate a concrete need.

#### OQ-RC-002 — EM acceptance of `workflow_class` Workflow-record extension

Question: RC declares `workflow_class = reconciliation` as an extension to the Workflow record owned by [execution-model.md §4.1]. Does EM accept this extension in its next revision (promoting `workflow_class` into EM's canonical Workflow schema), or does RC retain ownership of the tag in [schemas.md §6.5] as a subsystem-local extension?
Owner: foundation-author (coordination with execution-model owner)
Blocks: RC-002, RC-INV-001 citation stability (currently cite schemas.md §6.5); a single EM-owned schema would let RC cite [execution-model.md §4.1] directly.
Default-if-unresolved: Retain the tag in [schemas.md §6.5] as an RC-owned extension; RC-002 and RC-INV-001 cite schemas.md. Defer the promotion to EM's next minor version.

#### OQ-RC-003 — Fail-open reconciliation escalation rule

Question: RC-020a mandates that reconciliation dispatch runs BEFORE ordinary workflows on startup. If reconciliation's detectors themselves fail beyond Cat 0 (e.g., a detector panics on an unrecognized evidence shape that is not Cat 6b-classifiable), should the daemon (a) refuse to reach `ready` until the fault is resolved, or (b) fail open to `degraded` with an operator escalation and allow ordinary workflows to queue but not dispatch? The conservative default is (a); (b) would improve availability at the cost of letting new work accumulate against unclassified existing work.
Owner: foundation-author (coordination with process-lifecycle owner)
Blocks: none at MVH (default is (a) — refuse to reach `ready`).
Default-if-unresolved: Conservative (a). Daemon refuses to reach `ready` when reconciliation cannot classify; operator escalation required. Revisit post-MVH when operational data informs availability trade-off.

#### OQ-RC-004 — Detector cadence tuning post-MVH

Question: RC-020a names the default scheduled-cadence detector interval as hourly. Post-MVH, the interval may need to adjust based on observed divergence rates, JSONL tail growth, and operator preferences. What is the principled way to tune cadence — fixed per operator YAML? Adaptive based on divergence-rate telemetry? Workload-class-specific? This is deliberately deferred until there is observational data.
Owner: foundation-author
Blocks: none (MVH default: hourly, configurable via operator YAML).
Default-if-unresolved: Hourly default, operator-YAML configurable per [operator-nfr.md §4.3]. Revisit when operator telemetry is available.

#### OQ-RC-005 — Operator verdict-override CLI grammar

Question: RC-027 names `harmonik confirm-verdict <run_id>` and `harmonik veto-verdict <run_id> [--promote-to escalate-to-human]`. The final grammar, exit codes, interactions with `harmonik status`, and authorization model (who can override verdicts) are not finalized; they are expected to live in the operator-CLI spec per [operator-nfr.md §4.10].
Owner: operator-CLI spec author (inheriting from foundation-author)
Blocks: RC-027 surface details beyond per-command naming.
Default-if-unresolved: Keep the grammar as named in RC-027; operators opt in by policy; authorization defers to the operator-CLI spec. Migrated from prior OQ-RC-002 topic.

#### OQ-RC-006 — Migrate test-obligation prose to testing.md references

Question: §10.2 currently names test obligations in prose. The template §10.2 expects cross-references to `[testing.md §<layer>]` once testing.md lands.
Owner: foundation-author
Blocks: none (MVH prose obligations are in place)
Default-if-unresolved: Keep prose obligations; migrate within one revision cycle after testing.md is finalized. Migrated from prior OQ-RC-003 topic.

#### OQ-RC-007 — Post-MVH `recoverable-non-idempotent` resume protocol

Question: [execution-model.md §4.2 EM-010] reserves `recoverable-non-idempotent` as a post-MVH node-idempotency class with a declared resume protocol. Reconciliation's Cat 2 detector currently groups `recoverable-non-idempotent` with `non-idempotent`; when the class lands with its own resume protocol, Cat 2 may split into Cat 2 and Cat 2a.
Owner: foundation-author
Blocks: none (MVH decision: group under Cat 2)
Default-if-unresolved: Group under Cat 2. Amendment protocol ([architecture.md §4.6]) applies when the class is introduced. Migrated from prior OQ-RC-004 topic.

#### OQ-RC-008 — RC stricter corroboration vs EV-023a alignment

Question: Should RC require multi-store corroboration (≥2 of git/Beads/JSONL) stricter than EV-023a's single-authority rule? If yes, file an EV amendment to add `evidence.sources: List<{store, ref}>` to §8.6.8 payload.
Owner: foundation-author (coordination with event-model owner)
Blocks: none (default-if-unresolved holds RC aligned with EV-023a).
Default-if-unresolved: Stay aligned with EV-023a's single-authority semantics; do NOT impose stricter RC-side rules.

#### OQ-RC-009 — ON `quarantined` state declaration

Question: Should ON declare a normative `quarantined` state for runs awaiting operator escalation?
Owner: foundation-author (coordination with operator-nfr owner)
Blocks: none (RC currently does not synthesize a `quarantined` state).
Default-if-unresolved: No; the run stays in its current state-machine state and the escalation event is purely advisory.

#### OQ-RC-010 — EM Outcome record extension for verdict envelope

Question: RC-022a routes the investigator's verdict through HC-008 `outcome_emitted` with `outcome_kind = reconciliation_verdict`. Should EM's `Outcome` record in [execution-model.md §6.1] be extended (or a new variant added) to carry the verdict envelope shape?
Owner: foundation-author (coordination with execution-model owner)
Blocks: RC-022a wire-protocol stability (currently relies on outcome `payload` being free-form).
Default-if-unresolved: RC documents the `reconciliation_verdict` outcome_kind here; EM extends the canonical Outcome record in its next revision; no spec break in the interim because `payload` is free-form per HC-008.

#### OQ-RC-011 — WM-036 seventh verdict mapping

Question: WM §4.9 WM-036 maps six verdict values to workspace dispositions; the seventh (`no-op-accept`) added in RC v0.3.0 is not in WM's table.
Owner: foundation-author (coordination with workspace-model owner)
Blocks: none at MVH (no-op-accept has no workspace effect by definition).
Default-if-unresolved: Coordinate with WM's next revision to add a row mapping `no-op-accept` to a workspace disposition (likely 'no workspace action; outer run continues').

#### OQ-RC-012 — Operator-confirmation UX for RC-027

Question: RC-027 declares an operator-pause-and-confirm option per reconciliation workflow. What is the precise UX policy field shape, and which categories require operator-confirmation by default vs. opt-in?
Owner: foundation-author
Blocks: none at MVH (RC-027 default is execution proceeds without operator confirmation).
Default-if-unresolved: `confirm_required` policy field on the playbook (RC-016) defaulting to false; per-category default policy (require for Cat 6a, optional otherwise).

## A. Appendices

### A.4 Reverse-drift migration map (legacy section numbers)

Legacy citations into `reconciliation/spec.md` used the pre-taxonomy-first section layout (§9 for cross-references, §8 for taxonomy in a different position). The current layout places the taxonomy in §8 with subsections §8.1–§8.13 and cross-references in §9. This table maps legacy references to current section numbers for downstream specs that may cite prior versions. Follow the workspace-model v0.3.0 shape.

| Legacy citation | Current citation | Notes |
|---|---|---|
| `[reconciliation.md §9.1]` | `[reconciliation.md §9.1]` | Depends-on subsection; no renumbering, but entries expanded per v0.3.0. |
| `[reconciliation.md §9.2]` | `[reconciliation.md §9.2]` | Reverse-dependencies stub; unchanged. |
| `[reconciliation.md §9.3]` | `[reconciliation.md §9.3]` | Co-references; entries updated for v0.3.0 citations. |
| `[reconciliation.md §9.4]` | `[reconciliation.md §8.1]` | Cat 0 infrastructure-unavailable taxonomy row (legacy had taxonomy under §9.4). |
| `[reconciliation.md §9.5]` | `[reconciliation.md §8.2]` | Cat 1 idempotent-rerun. |
| `[reconciliation.md §9.6]` | `[reconciliation.md §8.3]` | Cat 2 non-idempotent in-flight. |
| `[reconciliation.md §9.7]` | `[reconciliation.md §8.4]` | Cat 3 generic store disagreement. |
| `[reconciliation.md §9.7a]` | `[reconciliation.md §8.4a]` | Cat 3a torn Beads write. |
| `[reconciliation.md §9.7b]` | `[reconciliation.md §8.5]` | Cat 3b verdict-unexecuted. |
| `[reconciliation.md §9.7c]` | `[reconciliation.md §8.6]` | Cat 3c inverse premature-close. |
| `[reconciliation.md §9.8]` | `[reconciliation.md §8.7]` | Cat 4 recoverable known state. |
| `[reconciliation.md §9.9]` | `[reconciliation.md §8.8]` | Cat 5 clean restart. |
| `[reconciliation.md §9.10a]` | `[reconciliation.md §8.11]` | Cat 6a integrity, LLM-triageable. |
| `[reconciliation.md §9.10b]` | `[reconciliation.md §8.11a]` | Cat 6b integrity, mechanically unrecoverable. |
| `[reconciliation.md §9.12]` | `[reconciliation.md §8.12]` | Action-mapping layer. |
| `[reconciliation.md §9.13]` | `[reconciliation.md §8.13]` | Failure-commit deferral (OQ-RC-001). |
| `[reconciliation.md §4.1 RC-001]` | `[reconciliation.md §4.1 RC-001]` | Reconciliation-as-workflow; unchanged. |
| `[reconciliation.md §4.1 RC-002]` | `[reconciliation.md §4.1 RC-002]` | Verdict-commit-only; updated to cite schemas.md §6.5 for workflow_class tag. |
| `[reconciliation.md §4.5 RC-025]` | `[reconciliation.md §4.5 RC-025]` | Verdict execution; trailer citation moved from EM §6.2 to schemas.md §6.4. |
| `[reconciliation.md §5 RC-INV-002]` | (retired) | Retired under AR-042; see §5 retirement stub. |
| `[reconciliation.md §5 RC-INV-003]` | (retired) | Retired under AR-042; see §5 retirement stub. |
| `[reconciliation.md §5 RC-INV-005]` | (retired) | Retired under AR-042; see §5 retirement stub. |

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-06-01 | 0.4.6 | agent (hk-63oh.38) | **RC-026a fulfilled — Cat 3b re-execution retry cap shipped.** Pure logic layer in `internal/core/verdictretrycap_rc026a.go`: `VerdictExecutionAttemptRecord` (JSON record for `.harmonik/reconciliation-attempts/<target_run_id>.json`; Valid() requires non-empty TargetRunID, Attempt ≥ 1, non-empty LastAttemptAt), `VerdictRetryCapDefault = 5`, `VerdictRetryDecision` (Allowed / NextAttempt / CapExceeded), `CheckVerdictRetryCap(record, cap)` pure function (nil record = 0 prior retries; next > cap → CapExceeded=true → Cat 6b; next ≤ cap → Allowed=true). I/O layer in `internal/lifecycle/verdictretrycap_rc026a.go`: `WriteVerdictAttemptAtomic(projectDir, record)` (WM-026 atomic temp+fsync+rename+parent-dir-fsync discipline), `ReadVerdictAttempt(projectDir, targetRunID)` (nil on absent = no retries recorded). Path helpers added to `internal/lifecycle/daemonpaths.go`: `ReconciliationAttemptsDir(projectDir)` (.harmonik/reconciliation-attempts/), `ReconciliationAttemptPath(projectDir, targetRunID)` (…/<targetRunID>.json). Tests cover: Valid() shape invariants, VerdictRetryCapDefault=5, nil-record first retry allowed at attempt=1, boundary (attempt 4→5 within cap), cap-at-5 exceeded, beyond-cap still-exceeded, NextAttempt always = current+1, Allowed/CapExceeded mutually exclusive, custom cap. No spec text or requirement IDs changed. Refs: hk-63oh.38. |
| 2026-06-01 | 0.4.5 | agent (hk-63oh.39) | **RC-027 fulfilled — Operator verdict-override surface shipped.** Core type layer: `OperatorVerdictOverridePolicy` (ConfirmRequired bool), `VerdictOverrideDecision` enum (confirm / veto), `VetoPromotion` enum (none / escalate-to-human), `OperatorVerdictOverrideRequest` struct (TargetRunID + Decision + VetoPromotion + Valid()), `PolicyRequiresConfirmation` pure function, `ApplyVetoPromotion` pure function mapping VetoPromotion → Verdict (none → no-op-accept; escalate-to-human → escalate-to-human). CLI commands added to harmonik binary: `harmonik confirm-verdict <run_id> [--project DIR]` and `harmonik veto-verdict <run_id> [--promote-to escalate-to-human] [--project DIR]`; both communicate via the daemon socket (op `confirm_verdict` / `veto_verdict`); exit 17 when daemon not running; exit 16 (operator-control-invalid-state) when no pending verdict exists for the run_id; exit 0 on success. Commands wired into main.go subcommand dispatch and listed in top-level usage. Tests cover: policy default (false), per-category S01 defaults (Cat 2 / Cat 3 = false; Cat 6a = true per OQ-RC-012), enum validity, OperatorVerdictOverrideRequest.Valid invariants (empty run_id; unknown decision; confirm-with-promotion forbidden), ApplyVetoPromotion mapping, wire-value alignment between VetoPromotionEscalateToHuman and VerdictEscalateToHuman, help-flag exit codes. No spec text or requirement IDs changed; no schemas touched. Refs: hk-63oh.39. |
| 2026-06-02 | 0.4.4 | agent (hk-63oh.24) | **RC-015a canonical role corrected: `investigator` (not `Researcher`).** RC-015a and RC-015 informative text updated: the canonical `(agent_type, role)` pair for the investigator subprocess is now `(claude-code, investigator)` — correcting a transcription error in v0.4.0 (the changelog had `role=investigator` but the spec text was written with `role=Researcher`). S01 library aligned: three DOT workflows (`cat-2.dot`, `cat-3.dot`, `cat-6a.dot`), three YAML policies (`cat-2.yaml`, `cat-3.yaml`, `cat-6a.yaml`), three prompt templates, and `README.md` all updated from `Researcher` to `investigator`. No new requirement IDs; no schemas touched. Refs: hk-63oh.24. |
| 2026-06-01 | 0.4.3 | agent (hk-63oh.8) | **RC-004 fulfilled — S01 reconciliation workflow library shipped.** Created `specs/s01/reconciliation/` library with three DOT workflows (`cat-2.dot`, `cat-3.dot`, `cat-6a.dot`), three YAML policies (`cat-2.yaml`, `cat-3.yaml`, `cat-6a.yaml` per CP-035 + RC-016 playbook extension), and three investigator-agent prompt templates (`cat-2-investigator.md`, `cat-3-investigator.md`, `cat-6a-investigator.md`). All DOT files carry `workflow_class="reconciliation"` per schemas.md §6.5, `budget_ref` per RC-017, and `required_skills` per RC-004/RC-016 (beads-cli + git-inspection minimum; Cat 2 and Cat 6a also include workspace-inspection). Wall-clock budgets: Cat 2=600s, Cat 3=300s, Cat 6a=900s. Investigator: `agent_type="claude-code"`, `role="Researcher"` per RC-015a. Cat 6a policy sets `confirm_required: true` per RC-027 opt-in recommendation (default verdict escalate-to-human). No requirement IDs, spec text, or schemas touched. Refs: hk-63oh.8. |
| 2026-06-01 | 0.4.2 | agent (hk-63oh.42) | **RC-030 Axes + §10.2 sensor.** Added `Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` to RC-030 per template obligation (RC-030 is a structural prohibition on reconciliation producing intra-run loop edges; no LLM, no IO, no state mutation → idempotent). Extended §10.2 RC-028–RC-030 test obligation to include an explicit negative test for RC-030: for every verdict enum value, verify verdict execution does NOT produce a back-edge in the workflow graph; confirm observed loop-backs trace to edge conditions and Guard/Gate control-points per [control-points.md §4.2, §4.4]. No requirement IDs or schemas touched. Refs: hk-63oh.42. |
| 2026-06-01 | 0.4.1 | agent (hk-63oh.26) | **RC-017 Axes + test obligation.** Added `Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` to RC-017 per template obligation (Axes lines on every LLM/IO/state-mutation requirement; RC-017's daemon-enforced wall-clock termination is a non-idempotent state mutation). Added §10.2 test obligation for RC-017 covering: DOT `budget_ref` presence, Budget `resource=wall_clock_seconds`+`scope=per_run` structural check, per-category default values (Cat 2: 600s, Cat 3: 300s, Cat 6a: 900s), and negative lint test for missing `budget_ref`. No requirement IDs or schemas touched. Refs: hk-63oh.26. |
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft from components.md Component 9 + round-2 amendments; split into spec.md + schemas.md. |
| 2026-04-24 | 0.2.0 | foundation-author | Corpus-wide cleanup pass (no semantic changes). Migrated legacy architecture.md citation anchors to the §4.N map per the v0.2 NOTE: §1.2→§4.2 (×2 in §4.5.RC-023 malformed-verdict RATIONALE and §9 cross-refs), §1.5→§4.6 (×4 in §4.1.RC-006 upgrade-discipline clause, §4.2.RC-009 taxonomy-shape lock clause, §9 cross-refs, and §11 open-question default). No requirement IDs, invariants, or schemas were touched. |
| 2026-04-24 | 0.3.0 | foundation-author | R1 integration pass addressing implementer, cross-spec-architect, and critic reviews. BLOCKING items applied: (B1) citation-drift migration across 52 of 66 outbound cites — migrated event-model.md §3.N anchors to §4.N / §6.3 / §8 per-event; beads-integration.md §10.N → §4.N; workspace-model.md §5.N → §4.3 / §4.7; process-lifecycle.md §8.2 → §4.2 / §4.3 with PL-005 and PL-010 IDs; operator-nfr.md §7.N → §4.N (§7.10 multi-daemon/operator-CLI → §4.10); control-points.md §6.N → §4.N including budget_ref (§6.5→§4.5), policy-ref (§6.5→§4.7 per context), §6.2→§4.2, §6.4→§4.4, §6.11→§4.11. (B2) Reclaimed ownership of `Harmonik-Verdict-Executed` commit trailer from EM (removed in EM v0.2.0); declared shape in [schemas.md §6.4]; all RC cites to [execution-model.md §6.2] for this trailer rewired to [schemas.md §6.4]. (B3) Added RC-003a category-orthogonality priority-ordering rule (Cat 0 → 6b → 6a → 5 → 3c → 3b → 3a → 3 → 2 → 4 → 1 first-match). (B4) Added RC-002a concurrent-reconciliation handling: at most one reconciliation workflow per target_run_id, 5s bounded wait via process-lifecycle.md §4.3 registry lock. (B5) Added RC-019a integrating EV-023a evidence-corroboration rule: every `store_divergence_detected` requires ≥2-store corroboration; Cat 6b exempt. (B6) Added AR-042 sensors to invariants RC-INV-001 and RC-INV-004. (B7) Retired RC-INV-002 (restated RC-002+RC-003), RC-INV-003 (restated RC-002+RC-025+RC-026), RC-INV-005 (restated RC-010+RC-015) as selection-test failures; preserved IDs as retirement stubs; rewrote RC-INV-001 to reconciliation-as-workflow uniqueness and RC-INV-004 to evidence-corroboration guarantee. IMPORTANT items applied: (I1) Added §A.4 reverse-drift migration map following WM v0.3.0 shape. (I2) Declared `workflow_class = reconciliation` extension in [schemas.md §6.5] with OQ-RC-002 tracking EM acceptance. (I3) Amended RC-014 and §8.3 Cat 2 to clarify controlled-indexed-query vs live-follow JSONL. (I4) Resolved Cat 3 vs Cat 6a workspace-missing precedence via RC-003a priority order; annotated in §8.11. (I5) Rewrote RC-015 so InvestigatorInput is an adapted/extended LaunchSpec per [handler-contract.md §6.1]; schema validation failure routes through RC-023. (I6) Added `no-op-accept` verdict to RC-020 (now seven-value enum) with mechanical action in RC-025 and [schemas.md §6.2]; annotated §8.12 Cat 3 typical-verdicts. (I7) Added RC-020a detector cadence + reconciliation-bootstrapping ordering in RC-026; filed OQ-RC-003 for fail-open reconciliation escalation. (I8) RC-020a declares daemon-startup / on-demand / scheduled-hourly dispatch points. (I9) Clarified §8.12 vs schemas.md §6.3 dual-table ownership — semantics vs mechanical dispatch. (I10) Cited EM orchestrator dispatch + WM §4.7 worktree interaction in RC-022 for `resume-here` / `reset-to-checkpoint`. (I11) §6.5 notes `infrastructure_unavailable` co-owned with PL-010 and `operator_escalation_required` co-owned with ON-008. (I12) Refactored RC-018, RC-023, RC-024, RC-025 payload-field leak into schemas.md references; RC declares WHEN/WHY only. (I13) Applied concrete resolutions via OQs for all 7 critic-flagged first-plausible-answer findings. (I14) Both files now carry matching spec-id `reconciliation`, sibling schemas.md gets `version: 0.3.0` and `status: supplement`. New requirement IDs: RC-002a, RC-003a, RC-019a, RC-020a. Retired invariant IDs (stubs preserved): RC-INV-002, RC-INV-003, RC-INV-005. New OQs: OQ-RC-002 (EM workflow_class extension), OQ-RC-003 (fail-open reconciliation), OQ-RC-004 (detector cadence tuning); OQ migrations: prior OQ-RC-002 operator-CLI grammar → OQ-RC-005, prior OQ-RC-003 testing.md → OQ-RC-006, prior OQ-RC-004 recoverable-non-idempotent → OQ-RC-007. Schemas added to [schemas.md]: VerdictExecutedTrailer (§6.4), WorkflowClassExtension (§6.5), plus `no-op-accept` addition to Verdict enum. Cross-spec coordination filings: EM acceptance of workflow_class extension (OQ-RC-002); operator-CLI grammar migration to operator-nfr.md-ownership (OQ-RC-005); testing.md migration (OQ-RC-006). Front-matter: version 0.2.0 → 0.3.0; added spec-category: runtime-subsystem; expanded depends-on to include workspace-model, process-lifecycle, operator-nfr, architecture. status remains draft. Template obligations preserved: spec-template-version 1.1; Axes: lines on every LLM/IO/state-mutation requirement; taxonomy-first reading order intact. |
| 2026-04-24 | 0.4.0 | foundation-author | R2 integration pass consuming skeptic, crash-adversary, and investigator-author reviews. STRUCTURAL: (A1) `spec-category: runtime-subsystem` → `foundation-cross-cutting` to resolve AR-053 envelope-obligation triggering against actual content (cross-cutting taxonomy + invariants imposed on every subsystem); no envelope section needed. (A2) §8.12 / schemas.md §6.3 dual-table sync — added `no-op-accept` to schemas.md §6.3 Cat 3 typical-verdict row to match prior R1 §8.12 update. (A3) Enum-cardinality consistency: RC-009 `6-detection-category` → `11-category`; §3 glossary `six enum values` → `seven`; schemas.md §6.1 VerdictEvent comment `one of six` → `seven`; added §3 glossary entries for `divergence` and `in-flight run` (latter cites operator-nfr.md §3). FABRICATED CITATION FIXES (BLOCKING): (B1) RC-019a / RC-014 / RC-INV-004 reframed against EV-023a actual semantics — EV §8.6.8 payload field is `corroboration: <enum: git-corroborated | beads-corroborated>` (scalar two-valued), NOT an `evidence.sources` array. Inconclusive divergences route to `divergence_inconclusive` per EV §8.6.10. Detector emission validates `corroboration ∈ {git-corroborated, beads-corroborated}` (no length-≥-2 array predicate exists). Filed OQ-RC-008 for stricter-than-EV corroboration. (B2) ON-008 fabrication fix — `operator_escalation_required` is now solely RC-owned (RC-025); operator-observable surface delivered via [operator-nfr.md §4.1 ON-002] consumption path. Dropped `quarantined` state cite from schemas.md §6.2 (no such state in ON). Filed OQ-RC-009 for ON `quarantined` state declaration question. (B3) BI §4.8 → §4.10 citation cluster fix — adapter-idempotency / intent-log / status-check-before-reissue cites moved to BI §4.10 (BI-029-032 + BI-031b); §4.8 retained for `br` CLI surface contract / version-pin verification (RC-012). Cross-references entry split into both targets. (B4) RC-002a lock primitive migrated from a fabricated `process-lifecycle.md §4.3 workflow registry` lock to disk-based `flock(LOCK_EX|LOCK_NB)` at `.harmonik/reconciliation-locks/<target_run_id>.lock` per PL-002a fd-lifetime advisory-lock primitive; orphan-sweep cleans stale locks via PL-006. Dropped Cat 4 `awaiting-lock` semantic-hijack disposition; second-dispatch on `EWOULDBLOCK` emits new `reconciliation_dispatch_deduplicated` and skips. Added new RC-002b for non-atomic-with-verdict-executed-commit recovery rule (lock + executed-commit are physically distinct; stale-lock-with-executed-commit deletes the lock; stale-lock-without routes Cat 3b). INVESTIGATOR-AUTHOR (BLOCKING): (C1) RC-015 reshape — investigator self-assembles via skills bounded by SnapshotToken (option B); LaunchSpec.snapshot_token serializes the SnapshotToken JSON. InvestigatorInput is now a documented LOGICAL VIEW (not a daemon-assembled record); schemas.md §6.1 annotated. Dropped fabricated `model class` / `prompt template reference` LaunchSpec-extension fields. New RC-015a — investigator is an HC handler per HC §4.1; `agent_type=claude-code, role=investigator` canonical. (C2) Verdict-emission protocol via outcome envelope — new RC-022a (investigator emits via HC-008 `outcome_emitted` with `outcome_kind=reconciliation_verdict`; daemon-side verdict-executor authors the verdict-and-verdict-executed commit pair) and new RC-025a (deterministic Go subroutine; panic-safe; idempotent across restart). Filed OQ-RC-010 for EM `Outcome` record extension. RECONCILIATION-BOOTSTRAP + RETRY CAP: (D1) New RC-003b — crashed reconciliation-workflow branches classify as Cat 5 (clean re-dispatch), not Cat 6a; discriminator is the `Harmonik-Workflow-Class: reconciliation` trailer. (D2) New RC-026a — Cat 3b retry cap (default N=5; durable counter at `.harmonik/reconciliation-attempts/<target_run_id>.json` via WM-026 atomic write; emits `reconciliation_verdict_execution_retry`). CAT 3a REFRAME: (E1) §8.4a Cat 3a detection rule rewritten against BI v0.3.0's BI-031 status-check-before-reissue protocol (drops the pre-BI-031 audit-log idempotency-key model). (E2) RC-025 reopen-bead bullet updated to cite [BI §4.4 BI-010] for verdict→op binding and [BI §4.10] for adapter idempotency via BI-031b. CITATION FIXES: (H1) RC-022 EM §4.1 → §7.1 (orchestrator state-machine, not orchestrator dispatch); WM §4.7 → §4.4 (worktree-reset) + §4.6 (merge). (H2) RC-028 fresh-worktree-on-reopen-bead WM §4.7 → §4.9 WM-034. CRASH-ADVERSARY AMENDMENTS: (I1) RC-013 — consumers MUST tolerate duplicate `reconciliation_category_assigned` emissions; dedup key `(target_run_id, category, snapshot_token.git_head_hash)`. (I3) New RC-020b — detector panic recovery via per-detector `recover()` barrier per PL-018a; suspended detector falls through to next; emits `reconciliation_detector_panic`. (I4) New RC-012a — post-`ready` Cat 0 carve-out: emits `daemon_degraded{reason=infrastructure_unavailable}` per EV §8.7.5 + ON-037 health probe surface, but does NOT transition daemon-status enum from `ready` to `degraded` (reentrant `degraded` is reserved for pre-`ready` Cat 0 per PL-010). (I5) RC-018 — kill-before-fallback step ordering for budget-exhausted: SIGTERM/SIGKILL investigator first (HC-018 interval), watcher-observe per HC-011, then emit `budget_exhausted` (class F per EV §8.4.3), then fallback verdict; both events fsync-boundary; crash between routes through Cat 3b retry cap (RC-026a). PAYLOAD REFACTOR (G2): RC-023 / RC-025 inline payload-field tuples moved into schemas.md §6.1 as named records (`MalformedVerdictPayload`, `VerdictExecutedPayload`); RC-023 / RC-025 cite by name. NEW OQs: OQ-RC-008 (RC-vs-EV corroboration alignment), OQ-RC-009 (ON quarantined state), OQ-RC-010 (EM Outcome extension), OQ-RC-011 (WM-036 seventh verdict mapping for `no-op-accept`), OQ-RC-012 (operator-confirmation UX for RC-027). NEW REQUIREMENT IDs: RC-002b, RC-003b, RC-012a, RC-015a, RC-020b, RC-022a, RC-025a, RC-026a. Retired invariants unchanged. Front-matter: version 0.3.0 → 0.4.0; status `draft` → `reviewed`; spec-category `runtime-subsystem` → `foundation-cross-cutting`. schemas.md: version 0.3.0 → 0.4.0; status remains `supplement`. Cross-spec coordination requests: EV §8.6 should add `reconciliation_dispatch_deduplicated`, `reconciliation_detector_panic`, `reconciliation_verdict_execution_retry` events; EV §8.7.5 confirm `daemon_degraded{reason}` enum extension; EM §6.1 extend `Outcome` record (or add variant) for `reconciliation_verdict` outcome envelope per RC-022a (tracked as OQ-RC-010); WM §4.9 add `no-op-accept` row to verdict-disposition table (tracked as OQ-RC-011). RC IDs frozen at v0.4.0. |
