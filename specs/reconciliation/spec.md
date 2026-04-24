# Reconciliation

```yaml
---
title: Reconciliation
spec-id: reconciliation
requirement-prefix: RC
status: draft
spec-shape: taxonomy-first
version: 0.1.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-04-23
depends-on:
  - execution-model
  - event-model
  - handler-contract
  - control-points
  - beads-integration
---
```

## 1. Purpose

This spec defines harmonik's reconciliation model — the restart-time and store-divergence-recovery contract that classifies every in-flight run into a bounded set of detection categories, maps each category to a default resolution action, and specifies the investigator-agent contract, verdict vocabulary, verdict execution, and staleness rules for LLM-triaged cases. Reconciliation runs as a normal harmonik workflow (no separate subsystem); this spec owns the taxonomy, the dispatch contract, and the investigator inputs.

It is the concrete answer to "why we don't need a dedicated database for in-flight workflow state" (locked decision #12): deterministic reconstruction from git + Beads, plus agent-driven investigation for ambiguous cases. This spec is a separate file from `execution-model.md` because the taxonomy is cross-cutting (consumed by `process-lifecycle.md` §8.2 startup, `operator-nfr.md` §7.8 restart RTO, and `workspace-model.md` §5.9 re-run rule) and because its shape is taxonomy-first rather than requirements-first.

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
- Beads terminal-transition adapter idempotency and intent-log mechanics — owned by [beads-integration.md §10.8a].
- Event payload schemas for reconciliation events — owned by [event-model.md §3.2].
- DOT authoring details for the reconciliation workflow library (node-level policies, specific prompts) — owned by the S01 Orchestrator Core subsystem spec, post-MVH.
- Failure-commit policy (MVH: no failure commits) — owned by [execution-model.md §4.5].
- Agent-subprocess silent-hang detection mechanics — owned by [handler-contract.md §4.6].

## 3. Glossary

- **reconciliation workflow** — a DOT workflow tagged `workflow_class = reconciliation` whose run executes the investigator playbook for a single reconciliation dispatch; bounded to a single verdict commit. (see §4.1)
- **investigator-agent** — the cognition-tagged agent node within a reconciliation workflow that reads state, reasons about divergence, and emits a verdict event. (see §4.4)
- **verdict** — one of six enum values (`resume-here`, `resume-with-context`, `reset-to-checkpoint`, `reopen-bead`, `accept-close-with-note`, `escalate-to-human`) that the daemon executes mechanically after emission. (see §4.5)
- **verdict commit** — the single checkpoint commit a reconciliation workflow emits, carrying the verdict event's payload plus investigator-produced evidence. (see §4.1, §4.5)
- **verdict-executed commit** — a second commit on the investigator's task branch carrying the `Harmonik-Verdict-Executed: true` trailer, marking the daemon's mechanical action as durable. (see §4.5)
- **snapshot token** — the `(git_head_hash, beads_audit_entry_id, captured_at_timestamp)` tuple captured at investigator-dispatch time that bounds the investigator's view of state. (see §4.4)
- **detector** — a deterministic Go function that classifies an in-flight run into one of the 11 categories of §8 by inspecting git + Beads + (optionally) JSONL divergence evidence. (see §4.3)
- **action-mapping layer** — the dispatch table in §8 that maps each category to its default daemon action. (see §4.2)
- **Cat N** — shorthand for detection category N in the §8 taxonomy; references like "Cat 3a" resolve to §8.4a.
- **run-scoped detector** — a detector whose inputs are filtered by `Harmonik-Run-ID` trailer, not by `Harmonik-Bead-ID`; a bead with multiple runs has each run classified independently. (see §4.3)
- **divergence-evidence read** — a scoped JSONL read used only to identify inter-store inconsistency (checkpoint missing from git, transition-file missing, etc.); forbidden as a source for `run_id`, `state_id`, `transition_id`, or bead-status facts. (see §4.3, [execution-model.md §4.7], [event-model.md §3.6])

Canonical-location terms cited from elsewhere: `run`, `state`, `transition`, `checkpoint`, `idempotency_class` — all defined in [execution-model.md §3]. `bead` — defined in [beads-integration.md §10.3].

> INFORMATIVE: Reading order for this spec is `1, 2, 3, 8, 4, 5, 6, 7, 9, 10, 11, 12, A` per `spec-shape: taxonomy-first`. Section numbers are stable; only the on-page sequence shifts. §8 appears next, then §4 references it.

## 8. Error and failure taxonomy

This is the spec's center of gravity. The 11 detection categories below classify every in-flight run at restart time or on detected store divergence. Requirements in §4 reference these by number and sub-letter. Action mapping is in §8.12.

Detectors below assume the orphan sweep of [process-lifecycle.md §8.2 step 1a] has completed. No harmonik-owned orphan process or stale worktree lock is live at classification time.

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

> INFORMATIVE: Reconciliation-workflow nodes are never classified here because reconciliation workflows are not checkpointed mid-investigation (RC-002); the recursion question does not arise (RC-INV-002).

### 8.3 Cat 2 — Non-idempotent in-flight

**Detection rule.** DOT node whose `idempotency_class` is `non-idempotent` or `recoverable-non-idempotent` per [execution-model.md §4.2 EM-009]; AND the bead is in `in_progress`; AND there is no `run_completed` or `run_failed` event for the run since that checkpoint.

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

**Detection rule.** The daemon's on-disk intent log at `.harmonik/beads-intents/` (per [beads-integration.md §10.8a]) records an outstanding `br` write for `(target_run_id, transition_id, op)` AND the Beads audit log at restart time either shows no corresponding entry OR shows an entry matching the idempotency key.

**Default response.** Auto-resolve via adapter re-issue per [beads-integration.md §10.8a]: read the audit-log entry to determine whether the write landed; if so, mark the in-memory operation complete without re-writing; if not, re-issue the `br` call with the same idempotency key.

**Escalation path.** If the audit log is itself ambiguous or missing the idempotency field, escalate to Cat 3 generic (investigator-dispatched).

**Emitted event.** `store_divergence_detected` (class=torn-beads-write) + `reconciliation_category_assigned` (category=3a).

**Investigator?** No (common case). **Auto-resolver?** Yes.

### 8.5 Cat 3b — Verdict-unexecuted

**Detection rule.** An investigator-run's task branch contains a `reconciliation_verdict_emitted` commit AND there is no subsequent `Harmonik-Verdict-Executed: true` commit on the same branch (per RC-025, [execution-model.md §6.2]).

**Default response.** Auto-resolve via RC-026 re-execution: the daemon performs the staleness check (RC-024); if stale, re-dispatches fresh reconciliation; if not stale, executes the verdict per RC-025 and writes the verdict-executed commit. No new investigator is spawned.

**Escalation path.** If the re-execution itself fails (e.g., `br` call errors out), route to Cat 3 generic.

**Emitted event.** `reconciliation_category_assigned` (category=3b).

**Investigator?** No. **Auto-resolver?** Yes.

### 8.6 Cat 3c — Inverse premature-close (terminal-transition-without-Beads-write)

**Detection rule.** A merge commit exists on the target branch (main or integration per [workspace-model.md §5.8]) tagged with `Harmonik-Run-ID R` (for run R whose workflow reached a success terminal state), the bead for R is still `in_progress` in Beads, and no subsequent in-flight checkpoints for R exist.

**Default response.** Auto-verdict `accept-close-with-note` with mechanical close-write (routed through the idempotency-keyed adapter per [beads-integration.md §10.8a]). No investigator is spawned.

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

**Default response.** Normal startup; proceed to `ready` per [process-lifecycle.md §8.2]. No-op.

**Escalation path.** None.

**Emitted event.** `reconciliation_category_assigned` (category=5).

**Investigator?** No. **Auto-resolver?** Yes (no-op).

### 8.11 Cat 6a — Integrity violation, LLM-triageable

**Detection rule.** Structurally wrong data whose cause an investigator can reason about and whose resolution might be operator-actionable. Detectors include:

- Workspace path referenced by in-flight bead does not exist on disk AND the sibling's transition-record file is absent.
- Trailer-vs-sibling-file mismatch on a checkpoint commit (e.g., `Harmonik-Transition-ID` trailer present but sibling file missing per [execution-model.md §4.4 EM-018]).
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
| Cat 3 (generic store disagreement) | investigator workflow | Yes | `accept-close-with-note` / `reopen-bead` | No (escalates through investigator) |
| Cat 3a (torn Beads write) | auto-resolve via adapter re-issue | No | — | Yes ([beads-integration.md §10.8a]) |
| Cat 3b (verdict-unexecuted) | auto-resolve via RC-026 re-execution | No | — | Yes (re-run verdict action) |
| Cat 3c (inverse premature-close) | auto-verdict `accept-close-with-note` + mechanical close | No | — | Yes (direct close-write) |
| Cat 4 (recoverable known state) | auto-resume with pending action | No | — | Yes (re-arm retry/gate) |
| Cat 5 (clean restart) | normal startup; proceed to `ready` | No | — | Yes (no-op) |
| Cat 6a (integrity, LLM-triageable) | investigator workflow | Yes | `escalate-to-human` (usually) | No (investigator required) |
| Cat 6b (integrity, mechanically unrecoverable) | auto-escalate without investigator | No | — | N/A (operator intervention) |

> INFORMATIVE: This table is duplicated in [schemas.md §6.3] as the canonical tabular schema; the copy here preserves the taxonomy-first reading order for §8. The two copies MUST stay in sync; divergence is a lint failure.

### 8.13 Failure-commit deferral

Reconciliation does NOT require a git commit for every failed transition. Failure events per [event-model.md §3.2] record the failure; checkpoints record successful durable states per [execution-model.md §4.5]. Revisit if the improvement loop later needs `git bisect` over failures. If that need materializes, failure-commits become an additive change (a new optional kind of checkpoint commit) without breaking the current contract. Tracked in OQ-RC-001.

## 4. Normative requirements

Per the `spec-shape: taxonomy-first` declaration in front matter, §8 (category taxonomy) appears above this section and is the center of gravity; requirements below reference §8 categories by number and sub-letter. Subsection numbering under §4 is per-spec.

### 4.1 Reconciliation-as-workflow

#### RC-001 — Reconciliation runs as a harmonik workflow

Reconciliation MUST run as a normal harmonik workflow: DOT-defined per [execution-model.md §4.1], dispatched deterministically by the daemon, and event-logged per [event-model.md §3.2]. There MUST NOT be a separate reconciliation subsystem. Each investigator-required category (see §8) has its own reconciliation workflow in the S01-shipped library (see RC-004).

Tags: mechanism

#### RC-002 — Reconciliation workflows emit exactly one checkpoint commit

A reconciliation workflow MUST emit exactly one checkpoint commit — the **verdict commit** — on the investigator-run's task branch. The verdict commit MUST carry the `reconciliation_verdict_emitted` event's payload (per §6) plus any evidence the investigator produced under `.harmonik/reconciliation/<investigator_run_id>/`. Intermediate state transitions within the reconciliation workflow MUST NOT be checkpointed. This is an explicit exception to [execution-model.md §4.5 EM-023] and is keyed on the workflow-library metadata tag `workflow_class = reconciliation`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### RC-003 — Bounded recursion follows from the verdict-only commit rule

A daemon crash during reconciliation MUST leave no mid-investigation durable state. On restart, the outer run whose reconciliation was interrupted MUST be re-classified from its original category against unchanged git + Beads state, and a fresh reconciliation workflow MUST be dispatched. Reconciliation-workflow nodes MUST NOT be classified under §8 Cat 1 or Cat 2; the recursion-bounding rule of RC-002 ensures no in-flight investigator state exists for a detector to classify.

Tags: mechanism

#### RC-004 — Reconciliation workflow library is S01-owned

The reconciliation workflow library — the concrete DOT workflows and YAML policies that implement investigator-required categories (Cat 2, Cat 3 generic, Cat 6a per §8) — MUST be owned by S01 (Orchestrator Core) and ship as part of the S01 package. S01 MUST ship: (a) a DOT workflow per investigator-required category; (b) YAML policies naming the wall-clock budget (§4.4 RC-017) and the skill-injection set (investigators require Beads-CLI and git-inspection skills at minimum per [control-points.md §6.11] and [handler-contract.md §4.11]); (c) the investigator-agent prompt templates.

Tags: mechanism

#### RC-005 — Detectors and verdict-execution mechanics are NOT in the workflow library

The §4.3 detectors MUST live in the daemon's Go code (mechanism-tagged functions), NOT in the S01 workflow library. The §4.5 verdict-execution mechanics (action dispatch, verdict-executed commit emission, idempotency-key adapter calls) MUST also live in the daemon's Go code. The S01 library owns investigator reasoning only.

Tags: mechanism

#### RC-006 — Upgrade discipline — daemon code and library ship together

A new reconciliation category (added via the amendment protocol per [architecture.md §1.5]) MUST ship a daemon-code change (detector + action-map entry per §8 taxonomy) AND a workflow-library addition in S01 (for investigator-required categories) in the same harmonik release. Split releases are forbidden.

Tags: mechanism

### 4.2 Action-mapping dispatch

#### RC-007 — Action-mapping is the dispatch contract; taxonomy is the detection contract

The §8 category taxonomy MUST classify *what went wrong*; the action-mapping table in §8.12 MUST specify *what the daemon does by default* for each class. The default action is normative: any deviation (e.g., a policy override routing Cat 1 through an investigator instead of auto-resume) MUST be declared in the reconciliation workflow's YAML with rationale.

Tags: mechanism

#### RC-008 — Auto-resolver categories MUST have a deterministic resolver

Every category whose default action in §8.12 is an auto-resolver (Cat 0 wait-and-retry, Cat 1 re-spawn, Cat 3a adapter re-issue, Cat 3b verdict re-execution, Cat 3c direct close-write, Cat 4 retry/gate re-arm, Cat 5 no-op, Cat 6b operator escalation) MUST have a deterministic implementation in the daemon's Go code. Investigator-required categories (Cat 2, Cat 3 generic, Cat 6a) MUST have a playbook per §4.4 RC-015.

Tags: mechanism

#### RC-009 — Taxonomy shape is settled

The 6-detection-category taxonomy plus §8.12 action-mapping is the shape, resolved 2026-04-24 per user decision. Authoring agents MUST NOT re-open the 3-action-vs-6-category framing. Any future amendment MUST follow the protocol of [architecture.md §1.5].

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

- `br --version` returns successfully within timeout T (T = 5 seconds by default) AND reports a version compatible with the pin per [beads-integration.md §10.8];
- a trial `br list --limit 1` or equivalent returns without error;
- `git rev-parse HEAD` succeeds;
- `.harmonik/` is writable.

Any prerequisite failure MUST halt classification. The daemon MUST emit `infrastructure_unavailable` per [event-model.md §3.2] naming the specific prerequisite that failed, MUST transition to `degraded` status per [process-lifecycle.md §8.2], and MUST retry at a configurable cadence. No in-flight run is classified until Cat 0 clears.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### RC-013 — Detector emits `reconciliation_category_assigned`

After classifying an in-flight run into a §8 category, a detector MUST emit a `reconciliation_category_assigned` event per [event-model.md §3.2] carrying the run_id, assigned category, and detection-rule name. Emission MUST precede any dispatch of a reconciliation workflow or auto-resolver.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### RC-014 — JSONL divergence-evidence scope is bounded

Detectors and investigators MAY read JSONL for the permitted uses below; all other uses are forbidden.

**Permitted.**

- Detecting that a checkpoint commit referenced in a JSONL `checkpoint_written` event is missing from git (triggers Cat 6b).
- Detecting that JSONL is corrupt / unparseable past a byte offset (triggers Cat 6b).
- Detecting that a `transition_event` exists in JSONL but no corresponding transition-record file exists in the checkpoint tree referenced by the event (triggers Cat 3 per [execution-model.md §4.4 EM-018]).
- Supplying observational context to an investigator agent so it can reason about the sequence of events leading to divergence.

**Forbidden.** A detector MUST NOT:

- Use JSONL as the source of last-known `run_id`, `state_id`, or `transition_id` for any in-flight run. Those are derived from git per [execution-model.md §4.7].
- Use JSONL to decide which bead is in-flight. Beads (queried via `br`) is authoritative per [beads-integration.md §10.7].
- Reconstruct the `state` or `transition` object from JSONL payloads.

On detecting divergence, the detector MUST emit `store_divergence_detected` per [event-model.md §3.2] with the divergence class, the triggering JSONL entry reference, the conflicting git/Beads facts, and the implied §8 category. The investigator workflow for the resulting Cat MUST consume this event, not the JSONL tail directly.

Tags: mechanism

### 4.4 Investigator-agent contract

#### RC-015 — Investigator inputs are bound to a snapshot token

An investigator-agent node MUST receive the following inputs, each resolved against the snapshot token (§6 for schema):

- Snapshot token: `{git_head_hash, beads_audit_entry_id, captured_at_timestamp}` captured by the daemon at investigator-dispatch time.
- Run metadata (`run_id`, `workflow_id`, `workflow_version`) of the outer run being reconciled.
- Bead ID and the Beads record for that bead as-of `beads_audit_entry_id`.
- Git state at the last checkpoint as-of `git_head_hash` (commit hash, tree, trailers, and transition-record sibling file per [execution-model.md §4.4 EM-018]).
- JSONL tail since the last checkpoint as-of snapshot, observational only per RC-014.
- Workspace state (path exists? branch state? WIP files present?) as-of snapshot.
- Agent session log if one exists (CASS-indexed per [workspace-model.md §5.3]).

The investigator MUST receive commits in git-DAG-parentage order per RC-011.

Tags: cognition
Axes: llm-freedom=bounded; io-determinism=best-effort; replay-safety=unsafe; idempotency=non-idempotent

> INFORMATIVE: The investigator delegates to a Claude Code agent (role: `investigator`), model-class per the YAML policy attached to the reconciliation workflow (S01-shipped per RC-004). Input shape is the `InvestigatorInput` record in [schemas.md §6.1]; output shape is the `VerdictEvent` in [schemas.md §6.1].

#### RC-016 — Investigator playbook per category

For each category with an investigator (§8.3 Cat 2, §8.4 Cat 3 generic, §8.11 Cat 6a), the S01-shipped YAML policy MUST define a **playbook**: a specific ordered sequence of checks producing evidence for the verdict. Playbooks MUST name the investigator's expected evidence outputs (captured in the reconciliation commit) and the verdict-selection rubric.

Tags: cognition
Axes: llm-freedom=bounded; io-determinism=best-effort; replay-safety=unsafe; idempotency=non-idempotent

#### RC-017 — Every reconciliation workflow declares a wall-clock budget

Every reconciliation workflow (per RC-001) MUST declare a wall-clock budget: a hard ceiling, measured from the workflow's `run_started` event to its terminal event, beyond which the daemon forcibly terminates the workflow. The budget MUST be declared as a YAML policy field `wall_clock_seconds` (positive integer, required) attached to the reconciliation workflow's DOT via `budget_ref` per [control-points.md §6.5]. In the absence of an explicit budget, the S01-shipped packaging (RC-004) MUST supply a per-category default:

- Cat 2 default: 600 seconds.
- Cat 3 generic default: 300 seconds.
- Cat 6a default: 900 seconds.

Tags: mechanism

#### RC-018 — Budget exhaustion terminates with fallback verdict

On wall-clock budget exhaustion, the daemon MUST:

- emit `reconciliation_budget_exhausted` per [event-model.md §3.2] with payload `(run_id, workflow_id, budget_seconds, elapsed_seconds)`;
- issue a **default verdict of `escalate-to-human`** on the outer (target) run. This verdict MUST be indistinguishable from an investigator-emitted `escalate-to-human` in the operator-facing surface;
- kill the investigator subprocess per [handler-contract.md §4.6] cleanup rules;
- NOT write a reconciliation commit. The budget-exhausted event + daemon-default verdict are the durable trace.

Because reconciliation workflows are not checkpointed mid-investigation (RC-002), budget exhaustion MUST NOT leave an in-flight reconciliation state for re-classification.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### RC-019 — Investigator captures WIP before emitting `reopen-bead`

Before emitting a `reopen-bead` verdict, the investigator MUST capture any recoverable WIP from the outer run's worktree into the reconciliation commit. Concretely, the investigator MUST: (a) run `git status --porcelain` and enumerate untracked files in the worktree; (b) capture a diff plus file listing; (c) include the capture in the reconciliation commit's body and/or as annotated files under `.harmonik/reconciliation/<investigator_run_id>/wip-capture/`. This obligation is mandatory for `reopen-bead` verdicts and OPTIONAL for other verdicts (which keep the worktree and retain WIP by default).

Tags: cognition
Axes: llm-freedom=bounded; io-determinism=best-effort; replay-safety=unsafe; idempotency=non-idempotent

### 4.5 Verdict vocabulary and execution

#### RC-020 — Verdict vocabulary is the six-value enum

A verdict event's `verdict` field MUST be exactly one of the six enum values listed in [schemas.md §6.1 Verdict]. Every verdict MUST have defined mechanical semantics (§6.2 Verdict-execution table). Any other value is a malformed verdict and MUST be handled per RC-023.

Tags: mechanism

#### RC-021 — Exactly one verdict event per reconciliation workflow

A reconciliation workflow MUST emit exactly one `reconciliation_verdict_emitted` event over its lifetime. The emission of the first verdict event MUST mark the workflow terminal; any subsequent verdict event from the same workflow is a structural violation and MUST be handled per RC-023 with malformation reason `multiple-verdicts`.

Tags: mechanism

#### RC-022 — Verdict commit lands on investigator's task branch

The verdict event's emission MUST be atomic with the investigator's verdict commit: a single `git commit` on the investigator's task branch carrying the verdict event's payload (per [schemas.md §6.1]) and any evidence files under `.harmonik/reconciliation/<investigator_run_id>/`. No intermediate checkpoints are written per RC-002.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### RC-023 — Malformed-verdict handling produces a fallback escalation

On any of the following malformation conditions, the daemon MUST:

- emit `reconciliation_verdict_malformed` per [event-model.md §3.2] with payload `(investigator_run_id, target_run_id, malformation_reason, raw_verdict_excerpt)` where `malformation_reason ∈ {unknown-verdict-value, missing-required-field, extra-fields, wrong-type, multiple-verdicts, verdict-after-terminal}`;
- issue a **fallback verdict of `escalate-to-human`** on the target (outer) run;
- terminate the reconciliation workflow (kill the investigator subprocess);
- NOT attempt to interpret the malformed payload.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

> RATIONALE: Trusting LLM-generated text to conform to a schema is a ZFC violation at the classification layer. The malformed-verdict path converts cognitive failure into a deterministic escalation. See [architecture.md §1.2].

#### RC-024 — Verdict staleness check precedes execution

Before executing a verdict per RC-025, the daemon MUST re-capture the current `(git_head_hash', beads_audit_entry_id')` and compare against the snapshot token captured at investigator-dispatch time (RC-015). The verdict is **stale** if either:

- the target run's checkpoint trail has gained a new commit since the snapshot, OR
- the target run's bead has changed status in the Beads audit log since the snapshot.

On staleness, the daemon MUST: emit `reconciliation_verdict_stale` per [event-model.md §3.2] with payload `(snapshot_token, current_values, divergence_reason)`; NOT execute the verdict; re-dispatch a fresh reconciliation workflow against the new state.

Changes to sibling beads or to the daemon's JSONL event log MUST NOT trigger staleness. Only changes to the target run's git branch or the target bead's Beads audit entries count.

Tags: mechanism

#### RC-025 — Verdict execution is durable and idempotent

When a reconciliation workflow emits a verdict event (per RC-020) and the staleness check of RC-024 passes, the daemon MUST:

- perform the verdict's mechanical action per the verdict-execution table ([schemas.md §6.2]);
- emit `reconciliation_verdict_executed` per [event-model.md §3.2] with payload `(investigator_run_id, target_run_id, verdict, executed_at_timestamp, action_summary)`;
- append a second commit to the investigator's task branch carrying the trailer `Harmonik-Verdict-Executed: true` (payload-free presence-only marker per [execution-model.md §6.2]).

Each verdict's mechanical action MUST be idempotent:

- `reopen-bead` → `br reopen <bead_id>` with idempotency key `<target_run_id>:reopen` per [beads-integration.md §10.8a]. If the bead is already `open`, no-op with success.
- `resume-here`, `resume-with-context`, `reset-to-checkpoint` → dispatching the outer run's next node is idempotent at the dispatch layer (the next dispatch check sees the outer run already running and does not re-dispatch).
- `accept-close-with-note` → appends an annotation to the reconciliation commit and writes the close to Beads if not already closed; idempotent on re-run.
- `escalate-to-human` → emits `operator_escalation_required` and marks the outer run quarantined; subsequent emissions are deduplicated by `target_run_id`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### RC-026 — Verdict-execution discovery on restart

The startup detector of [process-lifecycle.md §8.2 step 5] MUST treat a reconciliation workflow as resolved ONLY if both the verdict commit AND the verdict-executed commit (per RC-025) are present on the investigator's branch. A reconciliation workflow with a verdict commit but no verdict-executed commit MUST be classified as §8.5 Cat 3b with the dedicated auto-resolver re-attempting the verdict's mechanical action under a fresh staleness check (RC-024).

Tags: mechanism

#### RC-027 — Operator verdict-override — spec-draft obligation

A per-reconciliation-workflow policy option MUST allow operators to pause the daemon's verdict-execution step until operator confirmation or veto via `harmonik confirm-verdict <run_id>` / `harmonik veto-verdict <run_id> [--promote-to escalate-to-human]`. Default: execution proceeds without operator confirmation. Operators opt in by policy. This obligation applies to all investigator-dispatched categories (Cat 2, Cat 3 generic, Cat 6a per §8.12).

Tags: mechanism

> INFORMATIVE: OQ-RC-002 tracks the final grammar and exit codes for the confirm/veto CLI commands.

### 4.6 Re-run vs. intra-run

#### RC-028 — `reopen-bead` verdict triggers a new run on subsequent claim

A `reopen-bead` verdict MUST clear the in-flight tracking for the target bead. A subsequent claim of the bead MUST produce a new run with a fresh worktree and a fresh branch per [workspace-model.md §5.9]. The new run MUST receive a fresh `run_id`; continuation of the prior `run_id` after a `reopen-bead` verdict is forbidden.

Tags: mechanism

#### RC-029 — Intra-run rollbacks keep the worktree and run_id

A `reset-to-checkpoint` verdict is an intra-run rollback. The worktree and `run_id` MUST be preserved; the run reverts to the named checkpoint and re-runs from there per [execution-model.md §4.10 EM-044] (transition_kind + rollback_to_state_id representation).

Tags: mechanism

#### RC-030 — Reconciliation does NOT drive intra-run loops

Intra-run loops (workflow edges routing back to an earlier node) are NOT produced by reconciliation. Those loops are ordinary workflow-graph traversal handled by edge conditions and Guard/Gate control-points per [control-points.md §6.2, §6.4]. Reconciliation handles only restart and store-divergence cases.

Tags: mechanism

### 4.7 Testing obligation

#### RC-031 — Crash-recovery-tested

Every detector under §4.3 / §8, every verdict-execution path under §4.5, and every staleness-check path under RC-024 MUST be exercised by at least one crash-recovery scenario test before landing. The crash-recovery test layer is described in `docs/methodology/TESTING.md`; this obligation migrates to a `[testing.md §<layer>]` cross-reference within one revision cycle after `testing.md` is finalized (tracked in OQ-RC-003).

Tags: mechanism

## 5. Invariants

#### RC-INV-001 — Git wins on completion disagreement

If Beads reports a bead as `closed` but no merge commit with `Harmonik-Bead-ID` matching that bead exists in the project's git history, OR if a JSONL transition event references a checkpoint commit that does not exist in git, the divergence MUST route through reconciliation Cat 3 or Cat 6 per §8. Silent auto-reconciliation of completion facts is forbidden. This invariant is the reconciliation projection of [execution-model.md §5 EM-INV-005].

Tags: mechanism

#### RC-INV-002 — No mid-investigation durable state

A daemon crash during a reconciliation workflow MUST leave no mid-investigation durable state. On restart, the outer run whose reconciliation was interrupted MUST be re-classified against unchanged git + Beads state, and a fresh reconciliation workflow MUST be dispatched. This invariant bounds recursion: reconciliation-of-reconciliation never arises because there is no in-flight reconciliation state for a detector to classify.

Tags: mechanism

#### RC-INV-003 — The pair (verdict-emitted, verdict-executed) is the complete durable record

For every reconciliation workflow that reaches a verdict, the durable record on the investigator's task branch MUST consist of exactly two commits: the verdict commit (RC-002) and the verdict-executed commit (RC-025). A verdict commit without a matching verdict-executed commit MUST classify as §8.5 Cat 3b.

Tags: mechanism

#### RC-INV-004 — Snapshot-bounded investigator view

Every investigator's inputs MUST be computed relative to a snapshot token captured at dispatch time (RC-015). The daemon MUST refuse to execute a verdict whose snapshot has been invalidated by advance of the target run's git branch or target bead's Beads audit log (RC-024).

Tags: mechanism

#### RC-INV-005 — Detectors filter by run_id, never bead_id

Every detector MUST filter in-flight state by `Harmonik-Run-ID` trailer. A single bead with multiple runs MUST have each run classified independently. Filtering by `Harmonik-Bead-ID` for classification purposes is forbidden.

Tags: mechanism

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

Reconciliation event payloads ([event-model.md §3.2]) are versioned via the event envelope's `schema_version` field. Verdict schema is N-1 readable per [operator-nfr.md §7.5]. A breaking verdict-enum change (renaming or removing a value) requires a migration release scheduled at an operator pause per [operator-nfr.md §7.3]. Additive changes (new optional evidence fields, new verdict-execution metadata) bump `schema_version` but do not break readers.

### 6.5 Co-owned event payloads

This spec's requirements drive emission of the following events whose payload schemas are declared in [event-model.md §3.2]:

- `reconciliation_category_assigned` — emitted after a detector classifies an in-flight run (RC-013).
- `reconciliation_verdict_emitted` — emitted when the investigator produces a verdict (RC-021); payload per [schemas.md §6.1 VerdictEvent].
- `reconciliation_verdict_executed` — emitted after the daemon's mechanical action lands (RC-025).
- `reconciliation_verdict_stale` — emitted when the staleness check fails (RC-024).
- `reconciliation_verdict_malformed` — emitted on schema violation (RC-023).
- `reconciliation_budget_exhausted` — emitted on wall-clock budget exhaustion (RC-018).
- `store_divergence_detected` — emitted on JSONL divergence-evidence detection (RC-014).
- `infrastructure_unavailable` — emitted on Cat 0 prerequisite failure (RC-012).
- `operator_escalation_required` — emitted on `escalate-to-human` verdict execution (RC-025).

This spec is normative for WHEN each event fires; [event-model.md §3.2] is normative for the payload shape.

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
    -- Filter by run_id (RC-010, RC-INV-005)
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

Every branch point above corresponds to a normative requirement: Cat 0 pre-check (RC-012), detector classification (RC-010, RC-013), action dispatch (RC-007, RC-008), schema validation (RC-020, RC-023), staleness (RC-024), mechanical execution (RC-025), verdict-executed commit (RC-025, RC-INV-003).

## 9. Cross-references

### 9.1 Depends on

- **[execution-model.md §4.1]** — Workflow, Node, Edge types used by reconciliation DOT workflows.
- **[execution-model.md §4.2 EM-009]** — `idempotency_class` tag; consumed by Cat 1 and Cat 2 detectors.
- **[execution-model.md §4.3 EM-014]** — one-bead-many-runs rule; reconciliation verdicts (`reopen-bead`) trigger this per RC-028.
- **[execution-model.md §4.4 EM-016, EM-017, EM-018]** — checkpoint contract, trailer schema, transition-record sibling file; detectors read these.
- **[execution-model.md §4.5 EM-023, EM-026]** — checkpoint cadence; RC-002 is the reconciliation exception.
- **[execution-model.md §4.7]** — state reconstruction uses git + Beads only; informs RC-014 forbidden uses.
- **[execution-model.md §5 EM-INV-005]** — git wins on completion; RC-INV-001 is the reconciliation projection.
- **[execution-model.md §6.2]** — `Harmonik-Verdict-Executed` trailer.
- **[event-model.md §3.2]** — event taxonomy; all reconciliation events cited in §6.5 are declared there.
- **[event-model.md §3.6]** — observational-vs-state-reconstruction replay split; grounds RC-014 scope.
- **[handler-contract.md §4.6]** — error propagation and agent-subprocess cleanup; consumed by RC-018.
- **[handler-contract.md §4.11]** — skill injection; investigators receive Beads-CLI and git-inspection skills per RC-004.
- **[control-points.md §6.5]** — YAML policy surface for budget declarations (RC-017).
- **[control-points.md §6.11]** — skill-declaration surface consumed by investigator configuration.
- **[beads-integration.md §10.4]** — terminal-transition writes; `reopen-bead` and close verdicts route through here.
- **[beads-integration.md §10.7]** — store-authority rules; RC-INV-001 cross-references.
- **[beads-integration.md §10.8a]** — adapter idempotency and intent log; Cat 3a and RC-025 consume this.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from the foundation corpus. Populated at finalize.

### 9.3 Co-references (read-only consumption)

- **[workspace-model.md §5.1 Lease-by-run]** — reconciliation verdicts trigger workspace-model lease behaviors; no normative dependency on workspace-model internals.
- **[workspace-model.md §5.3 Session-log metadata]** — investigator inputs include the CASS-indexed session log when present (RC-015).
- **[workspace-model.md §5.8 Branching model]** — Cat 3c detector reads merge commits on the target branch; no dependency on branch-lifecycle internals.
- **[workspace-model.md §5.9 Re-run rule]** — `reopen-bead` verdicts (RC-028) trigger fresh-worktree creation there; this spec owns the verdict, workspace-model owns the lifecycle.
- **[process-lifecycle.md §8.2 Startup sequence]** — the daemon dispatches reconciliation during startup per RC-012, RC-026; this spec owns the taxonomy, process-lifecycle owns the startup sequence.
- **[operator-nfr.md §7.3 Operator control state machine]** — pause carve-out for reconciliation workflows; no normative dependency on the state-machine internals.
- **[operator-nfr.md §7.5 Checkpoint-format stability]** — N-1 compatibility contract for verdict schema (§6.4).
- **[operator-nfr.md §7.8 Restart RTO]** — reconciliation dispatch is accounted for in the RTO definition there.
- **[architecture.md §1.2 ZFC test]** — RC-023 rationale invokes the ZFC violation framing.
- **[architecture.md §1.5 Amendment protocol]** — new reconciliation categories and verdict enum values require this protocol (RC-006, RC-009).

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforming to Core MVH MUST pass every requirement RC-001 through RC-031 and every invariant RC-INV-001 through RC-INV-005. No requirement is deferred at MVH.

**Post-MVH extensions.** The operator verdict-override CLI surface (RC-027) MAY ship as a follow-on within one release after MVH; it is required to claim Core MVH conformance only if operators have opted in via policy.

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose. Each requirement's test obligation:

- **RC-001 — RC-006 (reconciliation-as-workflow).** Structural tests verifying reconciliation workflows carry the `workflow_class = reconciliation` tag; integration tests verifying S01 packaging ships the required DOT + YAML per RC-004.
- **RC-007 — RC-009 (action-mapping dispatch).** Unit tests covering the §8.12 action table for each of the 11 categories; lint verifying the §8.12 table and [schemas.md §6.3] match.
- **RC-010 — RC-014 (detectors).** Crash-recovery scenario tests (RC-031) exercising each detector in §8; negative tests verifying detectors do not misclassify across run_ids (RC-INV-005); tests verifying JSONL forbidden-uses (RC-014).
- **RC-015 — RC-019 (investigator contract).** End-to-end tests with twin handler: verify snapshot-token plumbing; wall-clock budget enforcement (RC-018); WIP-capture on `reopen-bead` (RC-019).
- **RC-020 — RC-027 (verdict and execution).** Schema-validation tests for every verdict enum value and every malformation reason (RC-023); staleness-detection tests (RC-024); verdict-execution idempotency tests (RC-025); restart-discovery tests for Cat 3b (RC-026).
- **RC-028 — RC-030 (re-run vs. intra-run).** Scenario tests verifying `reopen-bead` produces a new `run_id`; `reset-to-checkpoint` preserves `run_id`.
- **RC-031 (crash-recovery).** See `docs/methodology/TESTING.md` crash-recovery layer. Migration to `[testing.md §<layer>]` tracked in OQ-RC-003.

### 10.3 Excluded conformance claims

- This spec does NOT grant conformance over: the specific DOT contents of S01-shipped reconciliation workflows (post-MVH subsystem spec); investigator-agent prompt quality (cognition-tagged, not mechanism-verifiable); the operator CLI surface beyond the per-command grammar named in RC-027 (owned by a separate operator-CLI spec per [operator-nfr.md §7.10]); the `br` CLI's internal idempotency guarantees (owned by [beads-integration.md §10.8a] and the upstream Beads project).
- This spec does NOT guarantee performance or throughput bounds on reconciliation dispatch; those are operator-observable in [operator-nfr.md §7.8] (restart RTO) and are not requirements of this spec.

## 11. Open questions

#### OQ-RC-001 — Failure-commit additive extension

Question: When (if ever) should failed transitions emit checkpoint commits to enable `git bisect` over failures in the improvement loop? Reconciliation does not currently require them (§8.13).
Owner: foundation-author
Blocks: none (MVH decision: no failure commits)
Default-if-unresolved: No failure commits. Revisit when the improvement-loop spec lands and can demonstrate a concrete need.

#### OQ-RC-002 — Operator verdict-override CLI grammar

Question: RC-027 names `harmonik confirm-verdict <run_id>` and `harmonik veto-verdict <run_id> [--promote-to escalate-to-human]`. The final grammar, exit codes, interactions with `harmonik status`, and authorization model (who can override verdicts) are not finalized.
Owner: foundation-author
Blocks: RC-027 surface details
Default-if-unresolved: Keep the grammar as named in RC-027; operators opt in by policy; authorization defers to the operator-CLI spec per [operator-nfr.md §7.10].

#### OQ-RC-003 — Migrate test-obligation prose to testing.md references

Question: §10.2 currently names test obligations in prose. The template §10.2 expects cross-references to `[testing.md §<layer>]` once testing.md lands.
Owner: foundation-author
Blocks: none (MVH prose obligations are in place)
Default-if-unresolved: Keep prose obligations; migrate within one revision cycle after testing.md is finalized.

#### OQ-RC-004 — Post-MVH `recoverable-non-idempotent` resume protocol

Question: [execution-model.md §4.2 EM-010] reserves `recoverable-non-idempotent` as a post-MVH node-idempotency class with a declared resume protocol. Reconciliation's Cat 2 detector currently groups `recoverable-non-idempotent` with `non-idempotent`; when the class lands with its own resume protocol, Cat 2 may split into Cat 2 and Cat 2a.
Owner: foundation-author
Blocks: none (MVH decision: group under Cat 2)
Default-if-unresolved: Group under Cat 2. Amendment protocol ([architecture.md §1.5]) applies when the class is introduced.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft from components.md Component 9 + round-2 amendments; split into spec.md + schemas.md. |
