# Execution Model

```yaml
---
title: Execution Model
spec-id: execution-model
requirement-prefix: EM
status: reviewed
spec-shape: requirements-first
version: 0.3.3
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-04-25
depends-on:
  - architecture
---
```

## 1. Purpose

This spec defines harmonik's core execution data model — `workflow`, `node`, `edge`, `run`, `state`, `transition`, `checkpoint`, `outcome` — and the outcome-spine contract that threads through handler, hook, gate, transition, and event. It names the git-checkpoint-trail durability contract, the run-scoped transition-record sibling-file storage pattern (`.harmonik/transitions/<run_id>/<transition_id>.json`), the per-node idempotency-class tag that drives reconciliation classification, the failure taxonomy the rest of the system routes on, and the three-store authority rule (git / Beads / JSONL).

It is normative for every subsystem that produces, consumes, or reasons about runs. The spec is a separate file from `architecture.md` because the execution-data-model is large enough to warrant its own surface and because multiple other specs (event-model, handler-contract, control-points, reconciliation, workspace-model, beads-integration) cite its types directly.

## 2. Scope

### 2.1 In scope

- Core types: `Workflow`, `Node`, `Edge`, `Run`, `State`, `Transition`, `Checkpoint`, `Outcome`.
- Typed ID aliases: `RunID`, `StateID`, `TransitionID`, `NodeID`, `BeadID`.
- Node type enum (`agentic`, `non-agentic`, `gate`, `control-point`, `sub-workflow`) and node idempotency-class tag (`idempotent`, `non-idempotent`, `recoverable-non-idempotent`).
- Checkpoint contract: one git commit per successful durable transition, structured trailers, and transition-record sibling file at `.harmonik/transitions/<run_id>/<transition_id>.json`.
- Checkpoint cadence: every durable transition commits; reconciliation workflows are an explicit exception (verdict-only commit).
- Durability decision table: a mechanical rule over `transition_kind` × `outcome_status` that classifies each transition as durable or not.
- Outcome spine: the integrated flow from handler outcome through hook and gate to transition and event, including the `transition` record as the canonical durable form and `transition_event` as its projection.
- Failure taxonomy: six classes (`transient`, `structural`, `deterministic`, `canceled`, `budget_exhausted`, `compilation_loop`) with detection rule, default response, escalation path, and emitted event type each.
- Run-vs-bead distinction: one run per workflow execution against one input; a bead may have zero, one, or many runs.
- Three-store authority: git = completion authority (wins on disagreement); Beads = bead content + coarse status; JSONL = observational only.
- Sub-workflow composition: single-run expansion; parent's checkpoint trail covers nested execution; node-ID namespacing rule on expansion.
- Pre-run workflow validation obligations (parseability, sub-workflow resolution and acyclicity, reference resolution, attribute types, reachability, cycle bounds, start/terminal declaration).
- Backtracking representation: hybrid transition-kind tag with `rollback_to_state_id` for architectural and policy rollbacks.
- Cycle detection: per-edge traversal caps for cycle-bounding, and the traversal-counter storage locus.
- Active-run discovery rule used at restart.

### 2.2 Out of scope

- Event schemas, payload field lists, and event names — owned by [event-model.md §8]. This spec declares emission obligations; event-model declares names and shapes.
- Handler interface, LaunchSpec shape, agent-lifecycle events — owned by [handler-contract.md §4.1, §4.2].
- Workspace leasing, branching conventions, branch-creation lifecycle, merge semantics — owned by [workspace-model.md §4.3, §4.2, §4.5].
- Reconciliation-category classification, investigator-agent contract, verdict vocabulary — owned by [reconciliation/spec.md §8 (Cat 0–6), §4.4 (RC-015–019), §4.5 (RC-020–025)].
- Beads-CLI adapter, terminal-transition writes, idempotency-keyed intent log — owned by [beads-integration.md §4.2 BI-002, §4.4, §4.10].
- Policy expression grammar, control-point primitive, freedom profiles, budget counter semantics — owned by [control-points.md §4.7, §4.1, §4.6, §4.5].
- Operator control semantics (pause/stop/upgrade between tasks), operator-event emission — owned by [operator-nfr.md §4.3].
- Daemon process lifecycle, startup sequence, command surface — owned by [process-lifecycle.md §4.1, §4.2].

## 3. Glossary

- **workflow** — a named, versioned directed graph of nodes and edges, represented on disk as a DOT document. (see §4.1)
- **node** — a graph vertex of one of five declared types, optionally carrying handler, policy, skill, and idempotency-class attributes. (see §4.2)
- **edge** — a directed transition between two nodes, optionally carrying a policy-expression condition, label, weight, and ordering key. (see §4.1)
- **run** — one execution of one workflow against one input, identified by a stable `run_id`. Replaces ambiguous "task" / "cycle" / "work item" usage. (see §4.3)
- **state** — a position in a run's progression; a durable checkpoint boundary. (see §4.1, §4.4)
- **transition** — a move from one state to another, recorded as the full AlphaGo trace (prior state, actor role, candidate actions, chosen action, policy version, evidence, verifier metrics, confidence, next state). (see §4.1, §4.4)
- **checkpoint** — a git commit whose tree carries the work product AND a transition-record sibling file, and whose message carries structured trailers. (see §4.4)
- **outcome** — the handler-produced result of a node's execution. Fields: status, preferred label, suggested next IDs, context updates, notes. (see §4.1)
- **idempotency-class** — a per-node tag driving reconciliation behavior; one of `idempotent`, `non-idempotent`, `recoverable-non-idempotent`. (see §4.2)
- **durable transition** — a transition satisfying the decision procedure of §4.5.EM-023a: `transition_kind ∈ {forward, local-patchback, architectural-rollback, policy-rollback, context-restore}` AND `outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS}`. Durable transitions MUST be checkpointed per §4.5. (see §4.5.EM-023a)
- **terminal node** — a node declared in the workflow's `terminal_node_ids` list (§6.1). A workflow run enters a terminal state when it reaches any node in this list. (see §4.9)
- **in-flight run** — a run whose current state is neither `completed`, `failed`, nor `canceled`. (see §7.1, §4.7.EM-031a)
- **task branch** — per [workspace-model.md §4.2], the branch on which a run's node commits accumulate.
- **bead** — per [beads-integration.md §4.3 BI-005, BI-008], an atomic queued work item in the Beads store. A bead-run relationship is defined in §4.3.
- **transition-record sibling file** — the typed JSON file at the canonical path `.harmonik/transitions/<run_id>/<transition_id>.json` that carries the full `Transition` record per §4.1.EM-004. Run-scoping of the path is normative (§4.4.EM-018); it is the structural guarantee that cross-run merges, cherry-picks, and replay-tree construction cannot collide. (see §4.4)
- **outcome spine** — per §4.6, the integrated flow: handler outcome → hook dispatch → gate evaluation → transition selection → event emission.

## 4. Normative requirements

### 4.1 Core types

#### EM-001 — Workflow is a named, versioned directed graph

A `Workflow` MUST be a named, versioned directed graph with a stable `workflow_id`, `name`, `version`, a set of `nodes`, a set of `edges`, a declared `start_node_id` and non-empty `terminal_node_ids` list (see §6.1), an ordered reference list of policies resolved at workflow-load time (cited from [control-points.md §6.3]), and a `metadata` map. On-disk representation is a DOT document per [architecture.md §4.10] three-artifact separation.

Tags: mechanism

#### EM-002 — Edge is a directed transition with deterministic selection inputs

An `Edge` MUST carry `from_node`, `to_node`, an optional `condition` expression (policy expression — see [control-points.md §6.4]), an optional `preferred_label`, a `weight`, and an `ordering_key`. These fields MUST be sufficient to drive the deterministic edge-selection cascade of §4.10 without consulting any other store.

Tags: mechanism

#### EM-003 — State is a position in a run's progression

A `State` MUST carry `state_id`, `run_id`, `node_id`, `entered_at`, and a `transition_history` reference (commit-range on the task branch filtered by the run's `Harmonik-Run-ID` trailer; see §4.7 and §6.1). States are durable checkpoints per §4.4; a new state MUST NOT be observable outside the run until its checkpoint commit has landed.

Tags: mechanism

#### EM-004 — Transition is the full AlphaGo trace record

A `Transition` MUST carry `transition_id`, `run_id`, `from_state`, `to_state`, `actor_role`, `candidate_actions` (the full set considered), `chosen_action`, `policy_version`, `evidence` (structured), `verifier_metrics`, `confidence`, `transition_kind`, optional `rollback_to_state_id`, and `schema_version`. The record MUST be durable per §4.4 and reachable via the sibling-file contract of §4.4.

Tags: mechanism

#### EM-005 — Outcome is the handler-produced node result

An `Outcome` MUST carry `status ∈ {SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}`, `preferred_label` (optional routing hint), `suggested_next_ids` (optional routing hint; the deterministic cascade of §4.10 consults this as a hint after condition and `preferred_label` match and does not override them), `context_updates` (a key-value map applied to the run's shared context per §4.10.EM-041a), `notes` (freeform), `kind` (an `OutcomeKind` discriminator per §4.1.EM-005a; defaults to `default`), and `payload` (an optional kind-discriminated extension envelope per §4.1.EM-005a; absent when `kind = default`). The outcome shape is the handler's obligation per [handler-contract.md §4.1]; this spec owns the type.

Classifier input for the failure taxonomy (§8) is the handler-returned `ErrX` sentinel per [handler-contract.md §4.5], NOT fields of the `Outcome` record.

Tags: mechanism

#### EM-005a — Outcome carries a kind discriminator and optional kind-typed payload

An `Outcome` MUST carry a `kind ∈ {default, reconciliation_verdict}` discriminator that names the shape of its extension `payload`. The discriminator's wire-level alias is the `outcome_kind` field on the handler-contract `outcome_emitted` event per [handler-contract.md §4.3 HC-008]; the daemon MUST set `Outcome.kind` from the handler-returned `outcome_kind` without rewriting.

The discriminator's semantics:

- `kind = default` — the legacy ordinary outcome shape. `payload` MUST be absent. Every requirement in this spec that consumes `Outcome` (the cascade per §4.10.EM-041, the durability decision per §4.5.EM-023a, the Transition.outcome_status mapping per §6.1) operates on `default` outcomes unchanged; no §4 requirement reads `payload` for routing or durability decisions.
- `kind = reconciliation_verdict` — the outcome carries a reconciliation investigator's verdict. `payload` MUST be the `VerdictEvent` record per [reconciliation/schemas.md §6.1]; this spec does NOT redeclare the `VerdictEvent` fields. Per [reconciliation/spec.md §4.5 RC-022a], the daemon's verdict-executor consumes the payload to construct the verdict-and-verdict-executed commit pair on the investigator's task branch under the §4.5.EM-026 verdict-only-commit exception. The `VerdictEvent` payload is opaque to the cascade and to the durability decision (which consumes `outcome_status` per §4.5.EM-023a and §6.1); the verdict-executor consumes the payload via the reconciliation outcome-spine path, not via the ordinary cascade.

The enum is closed at MVH; future outcome variants (e.g., improvement-loop verdicts, operator-CLI dispatch outcomes) extend the enum via the amendment protocol per [architecture.md §4.6] and MUST cite their payload schema in the owning subsystem spec. Adding a discriminator value is an additive schema change per §6.4 (N-1 readable per §4.4.EM-022); a reader observing an unknown `kind` value MUST route the outcome to reconciliation per [reconciliation/spec.md §8.11 Cat 6a] rather than silently degrading to `default`.

Existing consumers of the v0.3.2 `Outcome` shape remain conforming: `kind` defaults to `default` and `payload` defaults to absent, and no §4 requirement that predates v0.3.3 reads either field. The extension is strictly additive.

Tags: mechanism

### 4.2 Node attributes

#### EM-006 — Node type is one of five declared kinds

A `Node` MUST declare `type ∈ {agentic, non-agentic, gate, control-point, sub-workflow}`. The five kinds are mutually exclusive; each node has exactly one type. No other type is accepted by the workflow validator of §4.9.

Tags: mechanism

#### EM-007 — Agentic nodes carry a handler reference

A node of type `agentic` MUST declare a `handler_ref` resolving to a handler registered per [handler-contract.md §4.1]. Non-agentic, gate, and control-point nodes MUST NOT declare `handler_ref`; a sub-workflow node MUST NOT declare `handler_ref` (its handler discipline is delegated to the expanded sub-workflow's nodes).

Tags: mechanism

#### EM-008 — Node carries handler-timeout, required-skills, and policy references

Each node MAY declare `timeout` (positive integer, seconds), `required_skills` (a list of skill names resolved per [control-points.md §4.11] and [handler-contract.md §4.11]), and references to policies, gates, freedom profiles, and budgets (`policy_ref`, `gate_ref`, `freedom_profile_ref`, `budget_ref`) per [control-points.md §6.3]. Reference resolution is validated at workflow ingest per §4.9.

Tags: mechanism

#### EM-009 — Node carries an idempotency-class tag

Each node MUST carry an `idempotency_class ∈ {idempotent, non-idempotent, recoverable-non-idempotent}`, either declared explicitly on the node or inherited from a per-node-type default declared in a YAML policy (per [control-points.md §6.3]). Attribute absence is an authoring error detected by the workflow validator of §4.9.

> INFORMATIVE: Reconciliation consumes this tag per [reconciliation/spec.md §8.2 Cat 1] and [reconciliation/spec.md §8.3 Cat 2] detectors; it drives the default classification of a crashed node without further cognition.

Tags: mechanism

#### EM-010 — Idempotency-class default mapping by node role

Absent a policy override, the following MVH-baseline per-node-type idempotency-class defaults MUST apply: reviewer, researcher, lint, test, typecheck, and analysis nodes default to `idempotent`; builder and merge nodes default to `non-idempotent`. A YAML policy MAY override any of these defaults. Post-MVH node types MAY register `recoverable-non-idempotent` defaults with a declared resume protocol.

Tags: mechanism

#### EM-011 — Node carries four-axis tagging per architecture

Every node declared in a workflow MUST carry the four-axis tags (`llm-freedom`, `io-determinism`, `replay-safety`, `idempotency`) and the `mechanism | cognition` tag per [architecture.md §4.1, §4.2]. The `idempotency` axis MUST match the node's `idempotency_class` per §4.2. Workflow validation (§4.9) enforces this.

Tags: mechanism

### 4.3 Run model

#### EM-012 — Run executes one workflow against one input

A `Run` MUST carry a stable `run_id`, `workflow_id`, `workflow_version`, `input` (a workspace reference per [workspace-model.md §4.1], not inline payload), current `state`, `context` (a shared key-value map updated per §4.10.EM-041a), `start_time`, and optional `end_time`. A run executes EXACTLY ONE workflow invocation against EXACTLY ONE input; multi-workflow or multi-input runs are not permitted. Transition records for the run are discoverable via the task-branch commit range whose commits carry the run's `Harmonik-Run-ID` trailer; no separate `transitions` field on the `Run` record is required.

Tags: mechanism

#### EM-013 — Run ID propagates as commit trailer and event field

The `run_id` of a run MUST appear as the `Harmonik-Run-ID` trailer on every checkpoint commit for that run (per §4.4) and as the `run_id` field on every event scoped to the run (per [event-model.md §4.1]). The run_id is the join key across git, Beads (via optional `Harmonik-Bead-ID` trailer — see [beads-integration.md §4.6 BI-018] and §4.3.EM-014), and JSONL.

Tags: mechanism

#### EM-014 — Bead-to-run relationship is many-runs-per-bead

A `Run` MAY be tied to a bead via `bead_id` (per [beads-integration.md §4.3 BI-005]). A bead MAY have zero runs (not yet claimed), one run (active or completed), or many runs across its lifetime (a prior run failed fundamentally — crash, unrecoverable error, or `reopen-bead` verdict per [reconciliation/spec.md §4.5 RC-020] — and a subsequent claim spawned a new run). A new run following a fresh claim MUST receive a fresh worktree and a fresh branch per [workspace-model.md §4.9].

Tags: mechanism

#### EM-015 — Intra-run loops are not new runs

A workflow edge routing back to an earlier node is a run-internal loop; it is NOT a new run. The run's `run_id` is stable across loop traversals, and the task branch continues to accumulate checkpoint commits (one per durable transition, per §4.5). Re-runs occur ONLY after fundamental failure per §4.3.EM-014.

Tags: mechanism

#### EM-015a — run_started emission on dispatch

The daemon MUST emit the `run_started` event (per §6.5 and [event-model.md §8.1]) after `create_run` has allocated the run's `run_id` AND after the Beads atomic-claim write per [beads-integration.md §4.3 BI-009] has persisted the claim AND before any node in the run is dispatched. The `run_started` event payload MUST carry the `run_id`, `workflow_id`, `workflow_version`, and (when the run is bead-tied per §4.3.EM-014) `bead_id`. If the daemon crashes between `run_id` allocation and `run_started` emission, the run is reconstructable from the Beads atomic-claim record per §4.7.EM-031a (no orphan runs).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-015b — run_completed and run_failed emission on terminal state

The daemon MUST emit exactly one of `run_completed` or `run_failed` (per §6.5 and [event-model.md §8.1]) when the run reaches terminal state per §4.3.EM-015c. `run_completed` emits when the run enters a node in `terminal_node_ids` with `outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS}`. `run_failed` emits when the classifier (§8) produces a terminal verdict, when the cascade returns `FAIL` (per §4.10.EM-046a or §4.10.EM-043), or when an operator cancel is observed per §7.1. The `run_failed` event payload MUST carry the failure class per §8 and the `last_checkpoint` SHA per §4.5.EM-025. The terminal-transition bead write per [beads-integration.md §4.4 BI-010] MUST follow the terminal event emission; it MUST NOT precede it.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-015c — Terminal-state detection rule

A run reaches terminal state when (a) its current `node_id` is in the workflow's `terminal_node_ids` list (per §6.1) AND its last `outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS}` — terminating as `completed`; OR (b) the classifier (§8) produces a terminal failure verdict — terminating as `failed`; OR (c) an operator `stop --immediate` signal is observed per [operator-nfr.md §4.3] — terminating as `canceled`. Terminal detection MUST be evaluated after every state advance per §7.4 and MUST be the condition on the orchestrator's outer loop. A run that has reached terminal state MUST NOT be re-dispatched; a subsequent re-run against the same bead per §4.3.EM-014 produces a new run with a fresh `run_id`.

Tags: mechanism

### 4.4 Checkpoint contract

#### EM-016 — Checkpoint is a git commit whose tree carries the work product and the transition record

A `Checkpoint` MUST be represented as a single git commit landing on the run's task branch (per [workspace-model.md §4.2]). The commit tree MUST contain both (a) the work product (files at the transition) and (b) a transition-record sibling file at the canonical path `.harmonik/transitions/<run_id>/<transition_id>.json`. The commit MUST be atomic: tree construction (`git write-tree`), commit-object creation (`git commit-tree` including the message and trailers), and reference advance (`git update-ref` on the task branch) execute as a sequence whose atomicity boundary is the reference advance. Before the reference advance lands, the transition is NOT observable to any other subsystem; the sibling file, work product, and trailers are all part of the tree made visible atomically by the reference advance.

Between `git write-tree` / `git commit-tree` (which stream loose objects into `.git/objects/`) and `git update-ref` (which makes the ref visible), a crash MAY leave loose objects in the repository whose ref was never advanced. Such orphan loose objects are NOT harmful: they carry no reference, MUST NOT be treated as observable state by any subsystem, and are eligible for reclamation by `git gc` per git's standard object-database discipline. The atomicity boundary of EM-016 covers reference visibility; it does NOT claim atomicity of the loose-object writes themselves.

The task branch MUST exist before any checkpoint is attempted; branch-creation lifecycle is owned by [workspace-model.md §4.2, §4.9].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-017 — Checkpoint commit carries structured trailers

Every checkpoint commit MUST carry the trailers `Harmonik-Run-ID`, `Harmonik-State-ID`, `Harmonik-Transition-ID`, and `Harmonik-Schema-Version`. The trailer `Harmonik-Bead-ID` MUST be present when the run is tied to a bead per §4.3.EM-014 and MUST be absent otherwise. Trailers are a cheap index for git-log scanning; authoritative fields live in the sibling file of §4.4.EM-018.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-017a — Corrupted-checkpoint fallback rule

If a checkpoint commit's `Harmonik-Transition-ID` trailer is present but the expected sibling file at `.harmonik/transitions/<run_id>/<transition_id>.json` is missing, truncated, or fails schema validation, the daemon MUST treat the commit as a corrupted checkpoint and dispatch a reconciliation workflow per [reconciliation/spec.md §8.11 Cat 6a] (LLM-triageable) or [reconciliation/spec.md §8.11a Cat 6b] (mechanically unrecoverable) as the detector classifies. The daemon MUST NOT silently proceed and MUST NOT re-attempt the write against the same commit.

The reconciliation workflow dispatched for this Cat 6 condition is bound by the verdict-only-commit rule of §4.5.EM-026 and [reconciliation/spec.md §4.1 RC-002]: it emits exactly one verdict commit and MUST NOT emit intermediate checkpoints. A corrupted checkpoint in the reconciliation workflow itself therefore cannot recur without producing a corrupted verdict commit — the recursion is bounded to at most one reconciliation level per [reconciliation/spec.md §4.1 RC-003]. If the verdict commit of a Cat 6 reconciliation is itself detected as corrupted on a subsequent restart, it MUST escalate to operator attention as Cat 6b per [reconciliation/spec.md §8.11a] rather than dispatching a nested reconciliation workflow.

Tags: mechanism

#### EM-018 — Transition record sibling file MUST be present at canonical path

For every checkpoint commit, the commit tree MUST contain a typed JSON file at `.harmonik/transitions/<run_id>/<transition_id>.json` containing the full `Transition` record per §4.1.EM-004. The `<run_id>` path component MUST be the run's `run_id`; the `<transition_id>` path component MUST be the transition's `transition_id`. The file MUST carry a `schema_version` integer field matching the commit's `Harmonik-Schema-Version` trailer.

Run-scoping of the path is a structural uniqueness guarantee: cross-run merges, cherry-picks, and replay-tree construction from distinct runs cannot collide at the sibling-file path because each run's transitions occupy a disjoint sub-directory. A reader that needs the record for a given `(run_id, transition_id)` pair retrieves it by `git show <commit>:.harmonik/transitions/<run_id>/<transition_id>.json` per §4.4.EM-019.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-018a — Transition ID generation contract

Every `transition_id` MUST be generated as a UUID v7 per [event-model.md §4.1]. Generation MUST occur in the daemon process (not in agent subprocesses) so that a single generation locus exists per project; daemon-local generation MAY mix a per-process monotonic counter into the random bits of the UUIDv7 tail to reduce same-millisecond collision probability to zero within a process. Cross-run uniqueness of `transition_id` values is NOT required by this contract: the run-scoped sibling-file path of §4.4.EM-018 (`.harmonik/transitions/<run_id>/<transition_id>.json`) provides the structural collision guarantee. Within a single run, `transition_id` values MUST be unique — the daemon-local UUIDv7 generator provides this guarantee mechanically.

Tags: mechanism

#### EM-019 — Transition record is discoverable by git-show

Given a `run_id`, a `transition_id`, and a commit on the run's task branch whose `Harmonik-Run-ID` and `Harmonik-Transition-ID` trailers match, the transition record MUST be retrievable via `git show <commit>:.harmonik/transitions/<run_id>/<transition_id>.json`. No cross-commit index MAY be required for retrieval. The `run_id` path component is always available to readers: it is either the run the reader is already scoped to, or it is present on the commit's `Harmonik-Run-ID` trailer per §4.4.EM-017.

Tags: mechanism

#### EM-020 — Transition records are immutable

Once committed, a transition-record file MUST NEVER be rewritten. A new transition in the same run adds a new file under a new `transition_id`; it MUST NOT modify any prior file. History-rewriting (amend, rebase, filter-branch) against committed transition records is a policy violation detected by the workflow validator and by post-hoc audit tooling per §4.4.EM-020a.

Tags: mechanism

#### EM-020a — Audit tool detection rule for transition-record integrity

A post-hoc audit tool MUST, for every commit reachable from every active or archived task branch, parse the `Harmonik-Run-ID` and `Harmonik-Transition-ID` trailers and verify that exactly one sibling file exists at `.harmonik/transitions/<run_id_trailer>/<transition_id_trailer>.json` in the commit's tree. The tool MUST flag as integrity violations: (a) a trailer pair with no matching sibling file; (b) a sibling file under `.harmonik/transitions/` not matching any trailer pair on its own commit; (c) within a single run's sub-directory (`.harmonik/transitions/<run_id>/`), a `transition_id` appearing on more than one commit across the run's task-branch history; (d) a sibling file whose `schema_version` disagrees with its commit's `Harmonik-Schema-Version` trailer; (e) a sibling file whose path `<run_id>` component disagrees with its commit's `Harmonik-Run-ID` trailer. The tool MUST use `git interpret-trailers --parse` (trailer-block-only mode) to avoid misreading commits whose message body contains trailer-like lines. Flagged commits MUST route to reconciliation per [reconciliation/spec.md §4.3 RC-010].

Tags: mechanism

#### EM-021 — Large evidence payloads externalize under the transition directory

Large `evidence` or `verifier_metrics` payloads MUST be externalized to sibling files under `.harmonik/transitions/<run_id>/<transition_id>/evidence/*` and referenced from the primary record by relative path. Externalized evidence files are part of the commit's tree and inherit the atomicity boundary of §4.4.EM-016 (they are NOT written outside the tree for "speed"; writing them outside the tree is non-conforming). The primary `<transition_id>.json` SHOULD remain single-digit KB for cheap parseability.

Tags: mechanism

#### EM-022 — Checkpoint schema is N-1 readable

The transition-record sibling file and the commit trailers MUST carry a `schema_version` integer that increments on normative change. Readers MUST accept N-1 (i.e., the immediately prior schema version) per [operator-nfr.md §4.5]. Breaking changes require a migration release.

Tags: mechanism

### 4.5 Checkpoint cadence

#### EM-023 — One commit per successful durable transition

The system MUST emit exactly one checkpoint commit (per §4.4) for every durable state transition (as defined by §4.5.EM-023a). No in-flight run MAY advance past a durable state transition without first landing its checkpoint commit; the `transition` record is considered final only after the commit exists.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-023a — Durability decision procedure

A transition is durable iff BOTH of the following hold:

| Factor | Durable values |
|---|---|
| `transition_kind` | `forward`, `local-patchback`, `architectural-rollback`, `policy-rollback`, `context-restore` |
| `outcome.status` | `SUCCESS`, `PARTIAL_SUCCESS` |

Transitions with `outcome.status = RETRY` are NOT durable (they are intra-run loops per §4.3.EM-015 and do not advance state); RETRY re-dispatch protocol is §4.10.EM-046b. Transitions with `outcome.status = FAIL` are NOT durable (failure handling per §4.5.EM-025 and §8). Gate denial (per §4.10.EM-042) leaves the run in the source state and does NOT constitute a durable transition. Validator rejection (per §4.9) prevents the run from starting and does NOT constitute a durable transition.

For `outcome.status = PARTIAL_SUCCESS`, the `Transition` record MUST carry a `partial_success=true` evidence flag so downstream consumers can distinguish partial from full success.

The Transition record MUST carry an `outcome_status` field set to the `Outcome.status` of the transition's associated outcome (see §6.1 Transition). The decision procedure's `outcome.status` input is this field; implementers MUST NOT reconstruct the association by any other path.

`context-restore` and reconciliation-directed transitions are daemon-produced, not handler-produced. For these, the daemon synthesizes an `Outcome` with `status = SUCCESS` and `actor_role ∈ {daemon, reconciliation}` per §4.10.EM-046; EM-023a applies unchanged. The synthesized Outcome is recorded in the `Transition` record's evidence map under `evidence.synthesized_outcome=true`.

Tags: mechanism

#### EM-024 — Git always knows the last durable state of every in-flight run

At any time, for every in-flight run (per §4.7.EM-031a), the tip of the run's task branch MUST be the run's last-durable-state checkpoint commit. This invariant is the precondition for the state-reconstruction contract of §4.7 and for the reconciliation detectors of [reconciliation/spec.md §4.3 RC-010].

Tags: mechanism

#### EM-024a — Branch-tip monotonicity check

The daemon MUST persist, per in-flight run, the last-observed task-branch-tip SHA in run metadata (e.g., under `.harmonik/run-tips/<run_id>`). On any subsequent read of the task-branch tip (at startup per §4.7.EM-031a, at dispatch per §7.4, or at checkpoint advance per §7.2), the daemon MUST verify that the new tip SHA is a fast-forward descendant of the persisted prior tip SHA (the prior tip is in the ancestor chain of the new tip). If the new tip is not a fast-forward descendant, the daemon MUST route the discrepancy to reconciliation per [reconciliation/spec.md §8.4 Cat 3] (store disagreement, branch rewound externally) and MUST NOT advance the run against the new tip. A missing prior-tip file for a run observed for the first time is NOT a violation; the daemon initializes the persisted tip on first observation.

This requirement defends EM-024 against external force-push, operator `git reset --hard`, and CI-system auto-rebase scenarios that rewind the task branch under the daemon's feet.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-025 — Failed transitions MUST NOT create checkpoint commits at MVH

A failed transition (per §4.5.EM-023a, i.e., `outcome.status = FAIL` or a classifier verdict of `transient|structural|deterministic|canceled|budget_exhausted|compilation_loop` per §8) MUST emit a failure event per [event-model.md §8] (see §8 for the per-class event mapping) but MUST NOT create a checkpoint commit for Core MVH conformance. The failure event MUST reference the last successful checkpoint commit's SHA as its `last_checkpoint` correlation field, providing an anchor to the git trail. Post-MVH introduction of failure commits (to support `git bisect` over failures for the improvement loop) is an additive change and does not break the current contract.

Tags: mechanism

#### EM-025a — Emission ordering for transition events relative to the reference advance

A pre-commit `transition_event` emission (the transition event per §4.6.EM-028) MUST NOT precede the reference advance (`git update-ref`) of §4.4.EM-016. The emission order is: `git update-ref` returns success first, then the daemon emits the transition event, `checkpoint_written` event, and state-entered event per §7.2. Emitting the transition event before the reference advance would leave observers with a transition reference whose commit is never durable if the reference advance fails (e.g., ENOSPC between `commit-tree` and `update-ref`), producing divergence-evidence false positives against reconciliation detectors.

ENOSPC or EIO during the checkpoint sequence MUST be classified as `transient` per §8.1 with a bounded retry cap; on cap exhaustion the class reclassifies to `structural` per §8.2. On retry, a new `transition_id` is generated (per §4.4.EM-018a); any evidence files written under `.harmonik/transitions/<run_id>/<failed_transition_id>/evidence/*` by the failed attempt MUST be removed from the worktree before the retry, or MAY be reclaimed by a periodic sweeper that removes `.harmonik/transitions/<run_id>/<transition_id>/` sub-directories whose `<transition_id>` is not referenced by any trailer on any commit reachable from the run's task branch.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-026 — Reconciliation workflows are an explicit exception to EM-023

Reconciliation workflows (per [reconciliation/spec.md §4.1 RC-002]) MUST emit exactly one checkpoint commit per reconciliation-run — the verdict commit — and MUST NOT emit intermediate checkpoints. The exception is keyed on the workflow's `workflow_class = reconciliation` metadata tag; absence of the field means ordinary workflow (obeys EM-023 unchanged).

Tags: mechanism

### 4.6 Outcome spine

#### EM-027 — Handler outcome threads through hook, gate, transition, and event as one integrated flow

The handler outcome produced per [handler-contract.md §4.1], the hook dispatch per [control-points.md §4.3], the gate evaluation per [control-points.md §4.2], the transition selection per §4.10, and the transition event per [event-model.md §8.1] are one integrated flow. Each segment MUST consume the immediately prior segment's typed output and produce the typed input of the next; no segment may bypass another.

Tags: mechanism

#### EM-028 — Transition record is the canonical durable form; transition_event is its projection

The `Transition` record stored at `.harmonik/transitions/<transition_id>.json` per §4.4.EM-018 is the canonical, authoritative durable form of every transition. The transition event emitted to the event bus per [event-model.md §8.1] MUST be a projection of that record: the event payload cites the transition by `transition_id`, `run_id`, and checkpoint commit hash; the full record is recoverable by `git show` per §4.4.EM-019.

Tags: mechanism

#### EM-029 — Transition event MUST NOT duplicate the full trace payload

The transition event payload MUST NOT carry the full AlphaGo trace payload (candidate_actions, evidence, verifier_metrics). Those fields live only in the sibling file. This prevents storage duplication and schema drift between event and trace.

Tags: mechanism

#### EM-030 — Consumers requiring full audit fidelity MUST read the transition record from git

A consumer that needs the complete AlphaGo trace fidelity (post-hoc audit, improvement-loop analysis, scenario-harness assertions) MUST read the sibling file from git per §4.4.EM-019. Streaming consumers that need only event-level metadata MAY read the transition event from the bus.

Tags: mechanism

### 4.7 State reconstruction

#### EM-031 — State reconstruction uses git plus Beads only

On restart, the daemon MUST reconstruct every in-flight run's durable state by walking the git checkpoint trail (identified by `Harmonik-Run-ID` trailers) and querying Beads for bead-side state via `br` per [beads-integration.md §4.2 BI-002]. The JSONL event log MUST NOT be walked to reconstruct state. Observational JSONL reads for divergence-evidence detection are permitted per [reconciliation/spec.md §4.3 RC-014]; such reads MUST NOT be relied upon to reconstitute a run's state beyond what git + Beads already establish.

A consumer reading JSONL for divergence-evidence purposes per this requirement MUST tolerate a torn last line: if the final line of a JSONL file is unparseable AND is not terminated by a newline, the consumer MUST discard that line and treat the remainder of the file as valid rather than raising a Cat 6b integrity signal per [reconciliation/spec.md §8.11a]. An unparseable line anywhere but at the tail, or an unparseable tail line terminated by a newline, IS a Cat 6b signal. Additionally, a JSONL-only divergence signal MUST NOT trigger [reconciliation/spec.md §8.4 Cat 3] (store disagreement) without git-side corroboration: the divergence detector MUST verify the git state first and only flag Cat 3 when git disagrees with the JSONL tail, preventing post-crash torn-tail JSONL from producing false Cat 3 alerts.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### EM-031a — Active-run discovery at startup

At startup, before state reconstruction begins, the daemon MUST determine the set of in-flight runs by querying Beads for beads in non-terminal state AND scanning the project's git refs for task branches matching the naming convention declared in [workspace-model.md §4.2]. The union of (Beads-linked runs) ∪ (branches whose tip carries a `Harmonik-Run-ID` trailer matching no terminal-state bead) is the active-run set. A run whose current state is `completed`, `failed`, or `canceled` is NOT in the active-run set.

If Beads is unreachable at startup (SQLite locked, CLI missing, `br` hang beyond timeout), active-run discovery MUST NOT proceed; the daemon MUST defer classification per the Cat 0 pre-check in [reconciliation/spec.md §8.1] and enter `degraded` status per [process-lifecycle.md §4.3]. A naive implementation that falls back to git-only classification would silently mis-classify every bead-tied run as "no terminal-state bead found," producing false reconciliations.

Before dispatching any reconciliation workflow for an in-flight run, the daemon MUST NOT modify the run's worktree (no `git clean`, no `git checkout`, no branch switch, no file deletion). Worktree state at crash time is an investigator input per [reconciliation/spec.md §4.4 RC-019]; pre-dispatch cleanup would destroy the WIP evidence the investigator depends on. Workspace-model enforces the same read-only-until-investigator-ran rule per [workspace-model.md §4.9].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### EM-032 — Deterministic replay contract

Given the run's git checkpoint trail and its Beads record, the run's state MUST be reconstructable to any point for debugging, audit, scenario-harness assertions, and restart reconciliation per [reconciliation/spec.md §4.1 RC-001]. "Transition history" in this spec refers to the git checkpoint trail, NOT the JSONL event tail.

Tags: mechanism

#### EM-033 — No workflow-level transactionality

A run that commits N nodes and fails on node N+1 MUST leave all N prior checkpoints durable. There is NO rollback of prior checkpoints on later failure. State-at-failure is preserved in git per EM-024; recovery is handled by [reconciliation/spec.md §8] categories.

Tags: mechanism

### 4.8 Sub-workflow composition

#### EM-034 — Sub-workflow node expands in place within the parent run

A node of type `sub-workflow` MUST expand in place at runtime: the sub-workflow's nodes and edges become part of the parent run's execution graph. The sub-workflow MUST NOT spawn a child run; the parent `run_id` is the sole run identifier for the entire nested execution.

Expansion is keyed on the sub-workflow's version as resolved at workflow-load time; a sub-workflow registry update between load and runtime expansion MUST NOT change the expanded shape. The load-time pin survives until run terminal state. Durable backing for the pin is normative per §4.8.EM-034c.

Tags: mechanism

#### EM-034a — Sub-workflow node-ID namespacing

On expansion, every sub-workflow node's `node_id` MUST be rewritten to the form `<parent_node_id>/<sub_node_id>`. The parent's `node_id` remains unchanged. For nested expansions (a sub-workflow referencing another sub-workflow), the rule composes left-to-right: a grandparent node `A` containing sub-workflow node `B` containing sub-workflow node `C` yields expanded node ID `A/B/C`. Nodes produced by this rule are unique within the expanded run graph; collisions at the sub-workflow source level are an authoring error detected by the validator per §4.9.

Sub-workflow expansion is a runtime operation performed by the daemon after the pre-run validator (§4.9) completes; the validator verifies resolvability and acyclicity but does NOT statically inline the sub-workflow's graph into the parent. State and transition records carry the namespaced `node_id`.

Tags: mechanism

#### EM-034b — Sub-workflow reference graph MUST be acyclic

The directed graph whose vertices are workflows and whose edges are sub-workflow references (A → B if workflow A contains a sub-workflow node referencing workflow B) MUST be acyclic. Self-reference and mutual reference are authoring errors detected by the pre-run validator per §4.9.EM-038. Detection MUST occur during the transitive resolution pass; a cycle MUST fail validation before any node executes.

Tags: mechanism

#### EM-034c — Sub-workflow expansion pin is durable on the entry checkpoint

When a parent run enters a sub-workflow node, the entry checkpoint (the checkpoint commit whose state transitions the run from the sub-workflow node to its expanded `start_node_id`) MUST carry the resolved sub-workflow pin in the `Transition` record's `evidence` map under the reserved key `evidence.sub_workflow_pin` with the shape `{ sub_workflow_ref: String, sub_workflow_version: String, resolved_workflow_id: UUID }`. On restart, the daemon MUST reconstruct the pinned expansion by reading this evidence key from the most recent `sub_workflow_entered` transition record on the run's task branch, NOT by re-consulting the sub-workflow registry. Registry updates between crash and restart therefore cannot alter the run's expansion. This requirement makes EM-034's "load-time pin survives until run terminal state" machine-checkable.

For nested expansions (sub-workflow containing sub-workflow), each entry checkpoint carries its own `evidence.sub_workflow_pin`; the outer run's expansion at restart is reconstructed by walking the checkpoint trail in commit order and applying each pin at its entry boundary.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-035 — Parent run's checkpoint trail covers nested sub-workflow execution

Every durable transition inside an expanded sub-workflow MUST emit a checkpoint commit on the parent run's task branch per §4.5.EM-023. There MUST NOT be a separate sub-workflow checkpoint trail.

Tags: mechanism

#### EM-036 — Sub-workflow entry and exit emit lifecycle events

On entering a sub-workflow expansion, the daemon MUST emit the sub-workflow-entered lifecycle event declared in [event-model.md §8.1], and the entry checkpoint MUST carry the expansion pin per §4.8.EM-034c. On exiting, it MUST emit the sub-workflow-exited lifecycle event declared in [event-model.md §8.1]; the exit event's terminal-outcome correlation field is composed per §4.8.EM-036a. Both events correlate via `run_id` and the parent namespaced `node_id` per §4.8.EM-034a. Event names and payload field lists are normative in event-model.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-036a — Sub-workflow terminal outcome is the last-expanded-node's Outcome

The `Outcome` that escapes a sub-workflow node — consumed by the parent's edge-selection cascade (§4.10.EM-041) on the edges leaving the sub-workflow node — MUST be the `Outcome` produced by the last node in the expanded sub-workflow executed before the sub-workflow reached a node in its own `terminal_node_ids` list. The sub-workflow-exited event's terminal-outcome correlation field MUST carry the same `Outcome`. `context_updates` applied by the last-expanded-node's `Outcome` have already been applied to the run's shared context per §4.10.EM-041a prior to escape, so the parent's cascade observes post-update context state.

Sub-workflows with multiple terminal nodes reach exactly one at runtime (the one the expanded execution traversed into); the `Outcome` that produced the terminal-reaching transition is the sub-workflow's terminal outcome. The parent's `sub-workflow` node MUST NOT declare its own `Outcome` shape — it inherits the expanded terminal outcome mechanically.

Tags: mechanism

#### EM-037 — Sub-workflow composition is the ONLY composition mechanism

A workflow MUST NOT extend, inherit, or runtime-rewrite another workflow. Composition MUST be exclusively via `sub-workflow` nodes referencing named sub-workflows resolved at workflow-load time. Proposals introducing runtime rewrites, inheritance, or dynamic node insertion fail this requirement.

Tags: mechanism

### 4.9 Validation obligations

#### EM-038 — Pre-run validator MUST run before any node executes

Before any node in a workflow executes, a workflow-attribute validator MUST run to completion. The validator's scope is:

- DOT parseability.
- Sub-workflow resolution, transitively: every `sub-workflow` reference resolves to a registered workflow, and every resolved sub-workflow is validated recursively. The sub-workflow reference graph MUST be acyclic per §4.8.EM-034b; the validator detects cycles during transitive resolution and fails.
- Reference resolution: every `handler_ref`, `policy_ref`, `gate_ref`, `freedom_profile_ref`, `budget_ref`, and each entry in `required_skills` resolves to a registered target.
- Attribute type checks: every enum-valued attribute (including `idempotency_class`, `type`) matches the enum; timeouts and budgets are positive integers; required attributes are present; the workflow declares `start_node_id` and a non-empty `terminal_node_ids` list.
- Reachability: every node is reachable from `start_node_id`; every node can reach at least one node in `terminal_node_ids`.
- Cycle-bound check: every cycle in the graph has a declared per-edge traversal cap (see §4.10.EM-043).

Any validator failure MUST prevent the workflow from starting.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### EM-039 — Validator is mechanism-tagged

Every validator check MUST be mechanism-tagged; delegation to cognition is forbidden. Semantic judgments (is this policy expression "good"? is this node name "descriptive"?) belong in reviewer nodes, not the validator.

Tags: mechanism

#### EM-040 — Agents that generate DOT MUST run validation before submission

An agent that produces a DOT workflow MUST run the validator before submitting the workflow to the daemon. Submission paths that skip validation are structural violations of the centralized-controller principle per [architecture.md §4.9]. "Submission" is the daemon RPC that ingests a workflow for dispatch; see [process-lifecycle.md §4.10] for the command surface.

Tags: mechanism

### 4.10 Edge selection, backtracking, and cycles

#### EM-041 — Deterministic edge-selection cascade

On exiting a node, the daemon MUST select the next edge by a deterministic cascade in this order: (a) edges whose `condition` expressions evaluate true against the current run context and outcome; (b) within the condition-matched set, prefer the edge whose `label` matches `outcome.preferred_label` if any; (c) otherwise prefer edges matching `outcome.suggested_next_ids` as a routing hint; (d) break remaining ties by `weight` (higher first); (e) break final ties by lexical order of `ordering_key`. The cascade MUST be deterministic: identical inputs produce identical output.

Tags: mechanism

#### EM-041a — Context update ordering

An `Outcome`'s `context_updates` map MUST be applied to the run's shared `context` BEFORE the edge-selection cascade of §4.10.EM-041 evaluates condition expressions. Cascade conditions therefore observe post-update context state.

Tags: mechanism

#### EM-042 — Guards reorder; Gates permit or deny; edges otherwise follow EM-041

Transition guards per [control-points.md §6.4] MAY reorder the candidate edge list before the cascade of EM-041 runs; they MUST NOT add, remove, or block edges. Gates per [control-points.md §6.2] MAY permit, deny, or escalate the chosen transition after the cascade selects it; gate denial leaves the run in the source state and does NOT constitute a durable transition per §4.5.EM-023a (i.e., no checkpoint is written). Guards precede the cascade; gates follow it.

Tags: mechanism

#### EM-042a — Gate-deny continuation protocol

When the cascade of §7.3 returns `STAY(current_state)` as a result of gate denial per §4.10.EM-042, the run MUST enter a `gate-pending` sub-state of `running`. In `gate-pending`, the daemon MUST NOT re-dispatch the source node and MUST NOT re-run the cascade against the same context and outcome (doing so would loop indefinitely, because the gate is a deterministic function of context and outcome that did not change). The daemon MUST wait for a gate-resolution signal declared in [control-points.md §6.2] (a policy-driven context change, an operator override, or a timeout per the gate's policy configuration). On receipt of a gate-resolution signal, the daemon re-evaluates the cascade; if the gate now permits, the run advances normally; if the gate still denies and the gate's policy declares a timeout, the run fails with class `structural` per §8.2.

Tags: mechanism

#### EM-043 — Every cycle carries a per-edge traversal cap

Every cycle in a workflow graph MUST have at least one edge carrying a declared per-edge traversal cap (a positive integer). When a traversal cap is reached during a run, the daemon MUST fail the transition with failure-class `compilation_loop` per §8. This is harmonik's Kilroy-parity cycle-bounding mechanism for MVH.

Tags: mechanism

#### EM-043a — Traversal-counter storage locus

The per-edge traversal counter MUST be maintained per `(run_id, edge)` tuple in daemon memory for the duration of the run AND MUST be recoverable from the task branch's git history at restart by scanning the run's commit trail and counting prior traversals of the edge (the edge is identified by its `from_node` and `to_node` fields captured on each durable transition's `Transition` record). Daemon-memory counters are non-authoritative across restart; the git-derived count is authoritative. When a single edge participates in multiple cycles, it shares one counter per-run per §11 OQ-EM-004.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### EM-044 — Backtracking is represented as transition_kind plus rollback_to_state_id

A `Transition` MAY carry a `transition_kind ∈ {forward, local-patchback, architectural-rollback, policy-rollback, context-restore}` field and an optional `rollback_to_state_id` field. Forward transitions MUST omit `rollback_to_state_id`; `architectural-rollback` and `policy-rollback` MUST populate it with the target earlier state ID. The `local-patchback` and `context-restore` kinds MUST omit `rollback_to_state_id` (they do not relocate the run's graph position). The hybrid shape (kind tag + optional pointer) is the canonical representation of harmonik's four rollback types.

Tags: mechanism

#### EM-045 — Rollback transitions are recorded as new transitions, not history-rewrites

A rollback MUST be represented as a new transition (new `transition_id`, new checkpoint commit) whose `rollback_to_state_id` points at an earlier state. The earlier state's checkpoint commit MUST NOT be altered or removed; git history is append-only per §4.4.EM-020.

Tags: mechanism

#### EM-046 — Context restore is agent-scoped and does not alter graph state

A `context-restore` transition restores a prior context window for an agent without altering the run's graph state. The checkpoint still lands (the transition is durable per §4.5.EM-023a); the `transition_kind` tag distinguishes it from forward progress. `rollback_to_state_id` MUST be absent for `context-restore` per §4.10.EM-044. A context-restore transition is initiated by the daemon or by a reconciliation verdict per [reconciliation/spec.md §4.5 RC-020], not by a handler; the `Outcome` associated with a context-restore transition is synthesized by the daemon with `status = SUCCESS` and an `actor_role` of `daemon` or the role of the verdict-executing subsystem. The context-restore is durable per EM-023a under the synthesized Outcome.

Tags: mechanism

#### EM-046a — No-matching-edge failure class

If the edge-selection cascade of §7.3 produces an empty match set (no outgoing edge has a satisfiable condition for the current context and outcome), the run MUST fail with failure class `structural` per §8.2, with the classification reason `no_outgoing_edge_matches`. The daemon MUST emit `run_failed` with class `structural` and a reason field identifying the node and outcome at which the cascade produced no match. This case is reachable in practice through policy-expression edits that render all outgoing edges false in some context; the `structural` class signals the appropriate response (re-planning, not retry).

Tags: mechanism

#### EM-046b — RETRY outcome re-dispatch protocol

An `Outcome` with `status = RETRY` MUST cause the daemon to re-dispatch the same node against the run's current state. The RETRY outcome's `context_updates` map MUST be applied to the run's shared `context` before re-dispatch per §4.10.EM-041a (pre-cascade application applies here; the cascade itself is NOT run after a RETRY). Re-dispatch observes attempt caps: the daemon MUST track per-node attempt count (in-memory for the duration of the run, re-derivable from git log scan on restart by counting commits whose `Harmonik-State-ID` matches the state's `state_id` and whose transition has `outcome.status = RETRY` encoded in its evidence map) and MUST transition to failure class `transient` per §8.1 on retry-count threshold per the node's retry policy. A RETRY outcome is NOT durable per §4.5.EM-023a and MUST NOT produce a checkpoint commit; the re-dispatch state transitions observed by the daemon are `running` → `retry-armed` → `retrying` → `running` and are internal to the run, not emitted as a distinct state machine in §7.1.

Tags: mechanism

## 5. Invariants

#### EM-INV-001 — Git is the state-reconstruction source

The git checkpoint trail MUST be sufficient, together with the Beads store, to reconstruct any run's current durable state. JSONL event replay MUST NOT be used for state reconstruction. Every subsystem that consumes run state (reconciliation, operator-nfr, process-lifecycle, scenario-harness) MUST honor this precedence.

Tags: mechanism

#### EM-INV-004 — No subsystem may implement workflow-level transactionality

Any subsystem that writes to git, Beads, or workspace state MUST NOT implement an undo-previous-N-operations primitive that atomically rolls back prior checkpoints, prior bead status writes, or prior workspace branch advances when a later transition fails. Prior writes remain durable; recovery routes through [reconciliation/spec.md §8] categories. This invariant spans four subsystems whose authored primitives could each violate it independently: execution-model (no multi-checkpoint undo primitive — §4.7.EM-033 is the local requirement), workspace-model (no branch-level atomic undo of a multi-commit advance per [workspace-model.md §4.2]), beads-integration (no bead-level atomic undo of a prior terminal write per [beads-integration.md §4.10]), and reconciliation (no verdict that atomically undoes prior durable checkpoints per [reconciliation/spec.md §4.5]). A subsystem could ship a conforming local requirement yet violate this invariant by introducing a primitive (e.g., a "rewind to last merge" CLI) that, when composed, produces atomic multi-subsystem undo. The invariant forbids such primitives at the authoring surface.

Tags: mechanism

#### EM-INV-005 — Git wins on completion disagreement

If Beads reports a bead as `closed` but no merge commit with `Harmonik-Bead-ID` matching that bead exists in the project's git history, OR if a transition event in JSONL references a checkpoint commit that does not exist in git, the divergence MUST be treated as a reconciliation flag and NOT silently auto-reconciled. Every subsystem that observes this class of divergence MUST route it through [reconciliation/spec.md §8.4 Cat 3] and [beads-integration.md §4.7 BI-022]. No subsystem may silently prefer Beads or JSONL over git.

Tags: mechanism

> INFORMATIVE: Three v0.1 invariants were retired in v0.2 per the template's §5 selection test (they were requirements-shaped, scoped within a single §4 subsystem): EM-INV-002 (one-commit-per-durable-transition — duplicate of EM-023), EM-INV-003 (transition-record-discoverable-by-git-show — duplicate of EM-019), and EM-INV-006 (one-run-per-workflow-input — duplicate of EM-012). Their IDs are retired and are not reused. EM-INV-001, EM-INV-004, and EM-INV-005 survive the selection test as genuine cross-subsystem invariants.

## 6. Schemas and data shapes

### 6.1 Typed ID aliases and record schemas

Every UUID-backed identifier in this spec is a typed alias of `UUID` to permit downstream specs to cite `[execution-model.md §6.1 RunID]` etc. The aliases:

```
TYPE RunID        = UUID   -- UUIDv7 per [event-model.md §4.1]; unique across the project
TYPE StateID      = UUID   -- UUIDv7; unique per run-entry
TYPE TransitionID = UUID   -- UUIDv7; globally unique per §4.4.EM-018a
TYPE NodeID       = String -- unique within a workflow; namespaced per §4.8.EM-034a on sub-workflow expansion
TYPE BeadID       = String -- opaque stable bead identifier per [beads-integration.md §4.3 BI-008]
TYPE CommitRange  = (first_commit_sha: String, last_commit_sha: String) -- inclusive range on a task branch
```

External types cited but not defined here:

- `WorkspaceRef` — defined in [workspace-model.md §4.1].
- `PolicyExpression`, `PolicyRef` — defined in [control-points.md §6.4].
- `ActionDescriptor` — defined in [handler-contract.md §4.1] as the typed descriptor of a handler-considered action (see OQ-EM-005 for bootstrap resolution).
- `AxisTags`, `ModeTag` — defined in [architecture.md §4.1, §4.2].

```
RECORD Workflow:
    workflow_id        : UUID               -- stable identifier for the workflow definition
    name               : String             -- human-readable name
    version            : String             -- semver-ish version
    nodes              : List<Node>         -- the vertices
    edges              : List<Edge>         -- the directed edges
    start_node_id      : NodeID             -- designated entry node; validated per §4.9.EM-038
    terminal_node_ids  : List<NodeID>       -- non-empty; reaching any of these ends the run
    policies           : List<PolicyRef>    -- resolved policy references (see [control-points.md §6.3])
    metadata           : Map<String, String> -- free-form key/value
    workflow_class     : String | None      -- optional class tag; at MVH the only accepted value is "reconciliation" (flags the §4.5.EM-026 exception); absence means ordinary workflow. Validator per §4.9.EM-038 MUST reject any other non-None value.
    schema_version     : Integer            -- N-1 readable per §4.4.EM-022
```

```
RECORD Node:
    node_id             : NodeID          -- unique within the workflow; §4.8.EM-034a namespacing applies under expansion
    type                : NodeType        -- one of {agentic, non-agentic, gate, control-point, sub-workflow}
    handler_ref         : String | None   -- required when type = agentic; forbidden otherwise
    timeout             : Integer | None  -- positive seconds
    required_skills     : List<String>    -- resolved per [control-points.md §4.11]
    policy_ref          : String | None   -- see [control-points.md §6.3]
    gate_ref            : String | None
    freedom_profile_ref : String | None
    budget_ref          : String | None
    idempotency_class   : IdempotencyClass -- one of {idempotent, non-idempotent, recoverable-non-idempotent}
    axes                : AxisTags        -- four-axis classification per [architecture.md §4.1]
    mode_tag            : ModeTag         -- one of {mechanism, cognition}
    sub_workflow_ref    : String | None   -- required when type = sub-workflow
```

```
ENUM NodeType:
    agentic
    non-agentic
    gate
    control-point
    sub-workflow
```

```
ENUM IdempotencyClass:
    idempotent
    non-idempotent
    recoverable-non-idempotent
```

```
RECORD Edge:
    from_node        : NodeID
    to_node          : NodeID
    condition        : PolicyExpression | None       -- optional; see [control-points.md §6.4]
    label            : String | None                 -- optional routing label
    preferred_label  : String | None                 -- optional preferred label (informative hint; cascade matches outcome.preferred_label against Edge.label per §4.10.EM-041)
    weight           : Integer                       -- tie-breaker; default 0
    ordering_key     : String                        -- lexical tie-break
    traversal_cap    : Integer | None                -- cycle-bounding per §4.10.EM-043
```

```
RECORD Run:
    run_id             : RunID                -- stable run identifier; UUIDv7 per [event-model.md §4.1]
    workflow_id        : UUID                 -- resolved workflow
    workflow_version   : String               -- pinned version at dispatch time
    input              : WorkspaceRef         -- workspace reference per [workspace-model.md §4.1]
    bead_id            : BeadID | None        -- present when tied to a bead (see [beads-integration.md §4.3 BI-008])
    state              : State                -- current state
    context            : Map<String, Any>     -- shared context; updated per §4.10.EM-041a
    start_time         : Timestamp            -- RFC 3339 wall clock
    end_time           : Timestamp | None     -- set on terminal transition
```

```
RECORD State:
    state_id            : StateID
    run_id              : RunID
    node_id             : NodeID              -- namespaced per §4.8.EM-034a when under sub-workflow expansion
    entered_at          : Timestamp
    transition_history  : CommitRange         -- commit range on the task branch filtered by the run's Harmonik-Run-ID trailer
```

```
RECORD Transition:
    transition_id        : TransitionID
    run_id               : RunID
    from_state           : State
    to_state             : State
    actor_role           : String                -- role name per [architecture.md §4.8]; {daemon, reconciliation} for synthesized outcomes per §4.10.EM-046
    candidate_actions    : List<ActionDescriptor> -- the full set considered
    chosen_action        : ActionDescriptor
    policy_version       : String
    evidence             : Map<String, Any>      -- structured; see §4.4.EM-021 externalization rule; reserved keys: sub_workflow_pin (§4.8.EM-034c), synthesized_outcome (§4.5.EM-023a)
    verifier_metrics     : Map<String, Any>      -- structured
    confidence           : Float | None
    outcome_status       : OutcomeStatus         -- the associated Outcome.status; drives §4.5.EM-023a durability decision
    transition_kind      : TransitionKind        -- per §4.10.EM-044
    rollback_to_state_id : StateID | None        -- set iff transition_kind ∈ {architectural-rollback, policy-rollback}
    schema_version       : Integer
```

```
ENUM TransitionKind:
    forward
    local-patchback
    architectural-rollback
    policy-rollback
    context-restore
```

```
RECORD Checkpoint:
    commit_hash            : String             -- git commit SHA on the task branch
    run_id                 : RunID              -- matches Harmonik-Run-ID trailer
    state_id               : StateID            -- matches Harmonik-State-ID trailer
    transition_id          : TransitionID       -- matches Harmonik-Transition-ID trailer
    bead_id                : BeadID | None      -- matches Harmonik-Bead-ID trailer when present
    schema_version         : Integer            -- matches Harmonik-Schema-Version trailer
    transition_record_path : String             -- always ".harmonik/transitions/<run_id>/<transition_id>.json" per §4.4.EM-018
```

```
RECORD Outcome:
    status              : OutcomeStatus       -- one of {SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}
    preferred_label     : String | None       -- routing hint
    suggested_next_ids  : List<NodeID>        -- routing hint; not an override per §4.10.EM-041
    context_updates     : Map<String, Any>    -- applied to run.context per §4.10.EM-041a (pre-cascade)
    notes               : String              -- freeform
    kind                : OutcomeKind         -- discriminator per §4.1.EM-005a; defaults to `default`
    payload             : VerdictPayload | None -- kind-discriminated extension envelope per §4.1.EM-005a; absent when kind=default; when kind=reconciliation_verdict, MUST be a VerdictEvent per [reconciliation/schemas.md §6.1]
```

```
ENUM OutcomeStatus:
    SUCCESS
    FAIL
    RETRY
    PARTIAL_SUCCESS
```

```
ENUM OutcomeKind:
    default                     -- ordinary handler outcome; payload MUST be absent
    reconciliation_verdict      -- reconciliation investigator verdict; payload MUST be a VerdictEvent per [reconciliation/schemas.md §6.1] (RC-022a)
```

> INFORMATIVE: The `VerdictPayload` type alias is the discriminated-union payload shape; at MVH it resolves only to `VerdictEvent` per [reconciliation/schemas.md §6.1]. Future `OutcomeKind` values introduced via the amendment protocol per [architecture.md §4.6] add their own variant; the `VerdictPayload` name is retained as the umbrella alias to keep the schema slot stable. EM does NOT redeclare `VerdictEvent` fields — it cites the RC-owned record by name. The v0.3.3 wire-protocol stability commitment for `OQ-RC-010` is delivered by this slot.

### 6.2 Checkpoint commit trailer format

Every checkpoint commit's message MUST end with a trailer block. Trailer keys and value types:

| Trailer | Type | Required? | Notes |
|---|---|---|---|
| `Harmonik-Run-ID` | UUID | Required | Run identifier. Owning spec: execution-model. |
| `Harmonik-State-ID` | UUID | Required | Current state after the transition. Owning spec: execution-model. |
| `Harmonik-Transition-ID` | UUID | Required | The transition recorded by this commit. Owning spec: execution-model. |
| `Harmonik-Schema-Version` | Integer | Required | Matches sibling file's `schema_version`. Owning spec: execution-model. |
| `Harmonik-Bead-ID` | String | Conditional | Present iff the run is bead-tied (§4.3.EM-014). Owning spec: execution-model. |
| `Harmonik-Workflow-Class` | Enum | Conditional | One of `{reconciliation}` at MVH; future workflow classes (e.g., `improvement-loop`) extend the enum via the amendment protocol per [architecture.md §4.6]. Present on every checkpoint commit emitted by a workflow whose `Workflow.workflow_class` field (per §6.1) is set; absent when the workflow has no class set (ordinary workflows). Used by reconciliation dispatch dedup per [reconciliation/spec.md §4.1 RC-002] to identify reconciliation-workflow checkpoint commits and by RC-003b's Cat 5 vs Cat 6a tiebreak. Owning spec: reconciliation. |
| `Harmonik-Target-Run-ID` | UUID | Conditional | The `run_id` being reconciled (the outer run's identifier). Present on reconciliation-workflow checkpoint commits only (i.e., commits whose `Harmonik-Workflow-Class = reconciliation`); absent on all other commits. RC-002's dispatch dedup keys on `(workflow_class, target_run_id)` per [reconciliation/spec.md §4.1 RC-002a]. The trailer's value is distinct from the commit's `Harmonik-Run-ID` trailer (which carries the investigator-run's `run_id`); the two trailers MUST coexist on every reconciliation-workflow checkpoint commit. Owning spec: reconciliation. |

> INFORMATIVE: Trailer parsing uses standard `git interpret-trailers` conventions (key: value lines in the trailer block). No exotic parser required. The `Harmonik-Workflow-Class` and `Harmonik-Target-Run-ID` trailers are RC-owned but registered here so that EM's trailer-lint tooling and audit tooling per §4.4.EM-020a recognize them as legitimate (any trailer not in this registry is a lint violation). The `Harmonik-Verdict-Executed` trailer (declared in [reconciliation/schemas.md §6.4]) is RC-owned and is NOT cross-listed in this registry per the EM v0.2.0 trailer-rollback decision; an EM trailer-lint tool MUST treat it as a known RC-owned extension per the cross-spec coordination note in [reconciliation/schemas.md §6.4].

### 6.3 Failure classes (tabular)

See §8 for per-class detection rule, default response, escalation path, and emitted event type.

### 6.4 Schema evolution

All schemas in this spec carry a `schema_version` integer. The compatibility contract is N-1 readable per [operator-nfr.md §4.5]: a reader MUST accept the immediately prior schema version (N-1); breaking changes require a migration release scheduled at an operator pause per [operator-nfr.md §4.3]. Additive changes (new optional field) are non-breaking and bump the version; renaming or removing fields is breaking.

### 6.5 Co-owned event payloads

This spec's requirements drive emission of the following events whose names and payload schemas are declared in [event-model.md §8]:

- Run lifecycle — `run_started` (on dispatch against a bead or standalone input), `run_completed` (on success terminal state), `run_failed` (on failure terminal state; payload includes the failure class per §8).
- State lifecycle — a state-entered event (on entry to a new state) and a state-exited event (on exit from a state, prior to transition selection).
- Transition projection — a transition event (projection of the `Transition` record per §4.6.EM-028).
- Checkpoint lifecycle — a checkpoint-written event emitted after every checkpoint commit lands; payload includes `run_id`, `state_id`, `transition_id`, optional `bead_id`.
- Sub-workflow lifecycle — a sub-workflow-entered event on expansion entry and a sub-workflow-exited event on expansion exit, per §4.8.EM-036.

This spec is normative for WHEN each event fires; [event-model.md §8] is normative for names, payload shapes, and any rename.

## 7. Protocols and state machines

### 7.1 Run state machine

The run's high-level states and transitions:

| From | Event | Guard | To | Emits |
|---|---|---|---|---|
| `pending` | dispatch | bead claimed (if bead-tied) | `running` | run_started |
| `pending` | operator cancel before dispatch | operator stop command | `canceled` | run_failed (class `canceled`) |
| `running` | durable transition | checkpoint commit lands | `running` | state-entered, transition event, checkpoint-written |
| `running` | terminal success | node in `terminal_node_ids` reached with `outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS}` | `completed` | run_completed |
| `running` | terminal failure | classifier verdict per §8 | `failed` | run_failed |
| `running` | operator immediate-stop | `stop --immediate` (operator event emitted by [operator-nfr.md §4.3]) | `canceled` | run_failed (class `canceled`) |
| `running` | budget exhausted at dispatch | budget check per [control-points.md §4.5] (budget event emitted there) | `failed` | run_failed (class `budget_exhausted`) |

> INFORMATIVE: Pause and graceful-stop operator controls operate BETWEEN runs per [operator-nfr.md §4.3] and do not appear as run-state transitions. Upstream events (`operator_stopped`, `budget_exhausted`) are emitted by their owning specs; execution-model emits `run_failed` with the classifying `class` field.

### 7.2 Checkpoint-and-emit sequence (protocol pseudocode)

```
FUNCTION checkpoint_and_emit(run, from_state, to_state, transition, outcome):
    ASSERT is_durable(transition.transition_kind, outcome.status)  -- per §4.5.EM-023a
    transition_id = transition.transition_id
    sibling_path = ".harmonik/transitions/" + run.run_id + "/" + transition_id + ".json"
    tree = workspace_tree_at(run) + write_transition_record(sibling_path, transition)
    message = format_commit_message(transition, outcome) + format_trailers(run, from_state, to_state, transition)
    -- Atomic sequence per §4.4.EM-016:
    tree_sha = git.write_tree(tree)
    commit_sha = git.commit_tree(tree_sha, parent=branch_tip(run.task_branch), message)
    git.update_ref(run.task_branch, commit_sha)  -- atomicity boundary
    -- Event emission ordering per §4.5.EM-025a: all emissions follow a successful update-ref.
    -- If `run.workflow_class == "reconciliation"`, callers MUST observe the §4.5.EM-026 exception
    -- (verdict-only commit; no intermediate checkpoint_and_emit calls).
    checkpoint = Checkpoint(commit_sha, run.run_id, to_state.state_id, transition_id,
                            run.bead_id, transition.schema_version, sibling_path)
    emit_event(state_exited, run, from_state)
    emit_event(transition_event, run, transition_id, commit_sha)
    emit_event(checkpoint_written, run, checkpoint)
    emit_event(state_entered, run, to_state)
    update_persisted_tip(run.run_id, commit_sha)  -- §4.5.EM-024a
    RETURN checkpoint
```

The `git.update_ref` step is non-idempotent state mutation (EM-016 axes line). Re-running the whole function against identical inputs produces a second commit with a different commit_sha; callers MUST guard re-entry by checking whether the transition has already been recorded (look up `transition_id` under `.harmonik/transitions/<run_id>/` on the task branch HEAD).

### 7.3 Edge-selection cascade (protocol pseudocode)

```
FUNCTION select_next_edge(run, current_state, outcome):
    apply_context_updates(run, outcome.context_updates)  -- §4.10.EM-041a
    candidate_edges = edges_out_of(current_state.node_id)
    candidate_edges = apply_guards(run, current_state, outcome, candidate_edges)  -- per [control-points.md §6.4]
    matched = [e FOR e IN candidate_edges WHERE evaluate_condition(e.condition, run.context, outcome)]
    IF outcome.preferred_label IS NOT None:
        preferred = [e FOR e IN matched WHERE e.label == outcome.preferred_label]
        IF preferred: matched = preferred
    ELSE IF outcome.suggested_next_ids:
        suggested = [e FOR e IN matched WHERE e.to_node IN outcome.suggested_next_ids]
        IF suggested: matched = suggested
    matched = sort(matched, BY -weight, ordering_key)
    IF not matched:
        RETURN FAIL(class=structural, reason=no_outgoing_edge_matches)  -- §8.2 + §4.10.EM-046a
    chosen = matched[0]
    IF chosen.traversal_cap IS NOT None AND traversal_count(run, chosen) >= chosen.traversal_cap:
        RETURN FAIL(class=compilation_loop)
    gate_verdict = evaluate_gate(run, current_state, chosen, outcome)  -- per [control-points.md §6.2]
    IF gate_verdict == deny: RETURN STAY(current_state)       -- §4.10.EM-042a — gate-pending sub-state
    IF gate_verdict == escalate: RETURN ESCALATE
    RETURN chosen
```

Every branch point above corresponds to a normative requirement: context-update ordering (§4.10.EM-041a), guard reordering (§4.10.EM-042), condition evaluation (§4.10.EM-041), preferred_label / suggested_next_ids hints (§4.10.EM-041), weight + ordering_key tie-break (§4.10.EM-041), cycle cap (§4.10.EM-043), no-matching-edge failure (§4.10.EM-046a), gate deny/escalate (§4.10.EM-042, §4.10.EM-042a).

### 7.4 Run main loop (protocol pseudocode)

The normative shape of the orchestrator's main loop. This is the single source of truth for how a run is created, dispatched, advanced, and terminated. S01 (Orchestrator Core) consumes this directly.

```
FUNCTION orchestrator_main_loop():
    -- Startup phase: per §4.7.EM-031a and §4.5.EM-024a.
    active_runs = discover_active_runs()            -- §4.7.EM-031a (Beads + branch-trailer scan)
    FOR run IN active_runs:
        verify_branch_tip_monotonicity(run)         -- §4.5.EM-024a
        resume_run(run)                             -- §4.7.EM-031 (state reconstruction from git + Beads)

    -- Steady-state loop.
    WHILE NOT shutdown_requested():
        IF should_pause_between_runs():             -- [operator-nfr.md §4.3] pause-between-runs
            wait_for_resume(); CONTINUE
        ready_beads = beads.ready_work_query()      -- [beads-integration.md §4.5 BI-013]
        IF not ready_beads:
            idle_wait(); CONTINUE
        bead = pick_one(ready_beads)                -- caller policy; MVH: oldest-first tiebreak
        workflow = resolve_workflow(bead)           -- [beads-integration.md §4.3 BI-005] workflow_name field
        IF NOT validator.validate(workflow):        -- §4.9.EM-038
            beads.mark_validation_failed(bead); CONTINUE

        run = create_run(bead, workflow)            -- §4.3.EM-012; run_id allocated in daemon per §4.4.EM-018a
        persist_run_id(run)                         -- in-memory active-run table + Beads atomic-claim per [beads-integration.md §4.3 BI-009]
        emit_event(run_started, run)                -- §6.5; emission is mandatory per §4.3.EM-015a

        execute_workflow(run)                        -- inner loop; see below
        finalize_run(run)                            -- emits run_completed or run_failed per §4.3.EM-015b

FUNCTION execute_workflow(run):
    WHILE NOT terminal_reached(run):
        node = current_node(run)
        IF is_terminal(node, run.workflow.terminal_node_ids):  -- §6.1 terminal_node_ids
            run.terminal_outcome = last_outcome(run)
            RETURN
        outcome, transition = dispatch_node(run, node)         -- handler path per [handler-contract.md §4.1]
        -- Reconciliation exception: if run.workflow_class == "reconciliation",
        -- the dispatch produces a single verdict-bearing transition; EM-026 applies.
        IF outcome.status == RETRY:                            -- §4.10.EM-046b
            apply_context_updates(run, outcome.context_updates)
            IF retry_count(run, node) >= retry_cap(node):
                fail_run(run, class=transient, reason=retry_cap_exhausted); RETURN
            CONTINUE                                            -- re-dispatch same node
        IF is_durable(transition.transition_kind, outcome.status):  -- §4.5.EM-023a
            checkpoint = checkpoint_and_emit(run, run.state, transition.to_state, transition, outcome)  -- §7.2
            run.state = checkpoint.state_id
        ELSE IF outcome.status == FAIL:
            classify_and_fail(run, outcome, transition)         -- §8 classifier
            RETURN
        -- Else: non-durable forward-progress edge cases route per §4.5.EM-023a decision procedure.
        next = select_next_edge(run, run.state, outcome)        -- §7.3
        IF next IS FAIL:
            fail_run(run, class=next.class, reason=next.reason); RETURN
        IF next IS STAY:                                         -- gate-deny; §4.10.EM-042a
            enter_gate_pending(run); wait_for_gate_resolution(run); CONTINUE
        IF next IS ESCALATE:
            escalate_to_operator(run); RETURN
        advance_to(run, next)                                    -- increment traversal counter per §4.10.EM-043a; run.current_node = next.to_node

FUNCTION finalize_run(run):
    IF run.state == terminal_success:
        emit_event(run_completed, run)                          -- §4.3.EM-015b
        write_terminal_bead_transition(run)                     -- [beads-integration.md §4.4 BI-010]
    ELSE IF run.state == terminal_failure:
        emit_event(run_failed, run, class=run.failure_class, last_checkpoint=run.last_checkpoint_sha)  -- §4.5.EM-025
        write_terminal_bead_transition(run)                     -- [beads-integration.md §4.4 BI-010]
```

The pseudocode's terminal-detection condition (`is_terminal(node, terminal_node_ids)`) is the normative implementation of §7.1's `running → completed` guard; the normative requirements for the lifecycle emissions this loop makes are §4.3.EM-015a (run_started), §4.3.EM-015b (run_completed / run_failed), and §4.3.EM-015c (terminal detection). The inner loop's `RETRY`, `is_durable`, `FAIL`, `STAY`, `ESCALATE` branches each correspond to explicit normative requirements; every state-advance step lands on §4.5.EM-023a's decision table or §4.10.EM-041's cascade.

## 8. Error and failure taxonomy

Harmonik's failure classes. Classifier is mechanism-tagged: classification MUST be determinable from the handler-returned `ErrX` sentinel per [handler-contract.md §4.5] plus the daemon-observed traversal-cap state, without semantic judgment.

The `structural` and `compilation_loop` classes are DISJOINT: `compilation_loop` is emitted the moment a traversal cap is hit (§4.10.EM-043) and a handler-returned `ErrStructural` does NOT transition to `compilation_loop`. The two paths do not overlap at emission time.

| # | Class | Detection rule | Default response | Escalation path | Emitted event |
|---|---|---|---|---|---|
| 8.1 | `transient` | Handler returns `ErrTransient` per [handler-contract.md §4.5]. | Retry unchanged, with bounded attempts and exponential backoff per the node's retry policy. | After attempt cap exhausted, reclassify as `structural` and re-evaluate. | run_failed (if terminal) or a retry event (transient in-run). |
| 8.2 | `structural` | Handler returns `ErrStructural`. | Retry only after an approach change — typically an edge routes to a re-planning node. | Operator notification via run_failed terminal event. | run_failed (if terminal) or a structured retry event. |
| 8.3 | `deterministic` | Handler returns `ErrDeterministic`. | MUST NOT retry. Fail the run; preserve state for post-mortem. | Operator notification via run_failed terminal event. | run_failed with class `deterministic`. |
| 8.4 | `canceled` | Handler returns `ErrCanceled`, or the daemon observes a `stop --immediate` operator signal (operator_stopped emitted by [operator-nfr.md §4.3]). | Graceful cleanup of handler subprocess; preserve last durable checkpoint. | Operator signal is the escalation; no retry. | run_failed with class `canceled`. |
| 8.5 | `budget_exhausted` | Budget counter at dispatch time would exceed remaining budget per [control-points.md §4.5] (`budget_exhausted` emitted there); OR handler returns `ErrBudget`. | Deny the dispatch; do not launch the handler. | Policy-defined: some budgets escalate to an operator gate; others terminate the run. | run_failed with class `budget_exhausted`. |
| 8.6 | `compilation_loop` | Daemon-observed: the per-edge traversal cap per §4.10.EM-043 has been reached at cascade evaluation. | Cap further retries; fail the run. | Operator notification via run_failed; post-mortem for pattern analysis. | run_failed with class `compilation_loop`. |

> INFORMATIVE: "Compilation loop" is named after the revision-loop pattern: fixes introduce new regressions, forming a cycle the system must bound. The term is retained for MVH despite being narrower than the phenomenon; alternate naming post-MVH is permitted via the amendment protocol per [architecture.md §4.6].

Per [handler-contract.md §4.5], every error returned across a subsystem boundary MUST wrap with one of the sentinel categories `ErrTransient`, `ErrStructural`, `ErrDeterministic`, `ErrCanceled`, `ErrBudget`. The `compilation_loop` class is a harmonik-level classification of a daemon-observed traversal-cap event; its detection does NOT route through the handler-error sentinel (the handler is not consulted at cap-hit). See OQ-EM-006 for coordination on whether to add `ErrCompilationLoop` as a sixth sentinel.

Failure classes are emitted as payload fields on run_failed events per [event-model.md §8.1]; the event schema is normative for the payload shape.

## 9. Cross-references

### 9.1 Depends on

- **[architecture.md §4.1]** — four-axis classification; every node and operation in this spec is tagged on the axes defined there.
- **[architecture.md §4.2]** — ZFC test; validator (§4.9) and classifier (§8) are mechanism-tagged; cognition-tagged nodes are scoped to the handler path.
- **[architecture.md §4.3]** — the required triple (search + verifier + traces); §4.1.EM-004 `Transition` is the trace record and §4.10.EM-044 backtracking representation is the search substrate.
- **[architecture.md §4.9]** — centralized-controller principle; the daemon owns the edge cascade, checkpoint emission, and validator invocation.
- **[architecture.md §4.10]** — three-artifact separation; workflow-as-DOT, bead-as-queue-item, spec-as-normative-document are disjoint.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from the foundation corpus. Populated at finalize.

### 9.3 Co-references (read-only consumption)

- **[event-model.md §4.1]** — event envelope; the run_id, state_id, and transition_id fields this spec originates are consumed by events shaped there. UUIDv7 recommendation for `transition_id` (per §4.4.EM-018a) is declared there.
- **[event-model.md §8]** — event taxonomy; the co-owned events listed in §6.5 have their names and payload schemas declared there. This spec's co-dependency with event-model is resolved directionally: execution-model owns types, event-model owns wire formats (per components.md §Co-dependency resolution rules).
- **[event-model.md §4.4]** — fsync policy; the checkpoint-written event drives an fsync boundary.
- **[event-model.md §4.5]** — observational-vs-state-reconstruction replay split; this spec names git + Beads as the state-reconstruction source (§4.7.EM-031).
- **[handler-contract.md §4.1]** — handler interface emitting `Outcome` instances conforming to §4.1.EM-005; ActionDescriptor is defined there.
- **[handler-contract.md §4.5]** — error sentinels (ErrTransient/Structural/Deterministic/Canceled/Budget) that drive the classifier in §8.
- **[handler-contract.md §4.11]** — skills declaration surface referenced by node `required_skills` in §4.2.EM-008.
- **[control-points.md §6.4 Transition-guard semantics]** — this spec's §4.10.EM-042 cites the guard surface; does not depend on control-points' internals.
- **[control-points.md §6.3 Policy schema]** — this spec's §4.2.EM-008 references policy YAML refs; PolicyExpression grammar lives in control-points.
- **[control-points.md §4.5 Budget enforcement]** — budget_exhausted event emission and budget counter semantics are owned there; this spec consumes the outcome (§8.5).
- **[control-points.md §4.11 Skill declaration surface]** — this spec's §4.2.EM-008 references the `required_skills` surface declared there.
- **[reconciliation/spec.md §4.1 RC-002 Reconciliation checkpoint cadence]** — reconciliation workflows are an exception to §4.5.EM-023; the cadence rule here is normative, the exception there is normative.
- **[reconciliation/spec.md §4.3 RC-010 Detection rules]** — detectors consume the `idempotency_class` tag declared in §4.2.EM-009 and the trailer schema of §6.2.
- **[reconciliation/spec.md §4.5 RC-020 Verdict vocabulary]** — intra-run rollback verdicts (`resume-here`, `resume-with-context`, `reset-to-checkpoint`) produce §4.10.EM-044 rollback transitions; this spec owns the transition shape, reconciliation owns the verdict vocabulary.
- **[workspace-model.md §4.1]** — WorkspaceRef type used by `Run.input`.
- **[workspace-model.md §4.2 Branching model]** — task-branch naming, lifecycle, and existence precondition (§4.4.EM-016) are owned there.
- **[workspace-model.md §4.9 Re-run rule]** — fundamental-failure re-runs trigger fresh-worktree creation there; this spec owns the one-bead-many-runs shape (§4.3.EM-014).
- **[beads-integration.md §4.6 Bead-ID propagation (BI-017 through BI-020)]** — the `Harmonik-Bead-ID` trailer shape is declared here; the bead store contract is declared there.
- **[operator-nfr.md §4.3 Operator control semantics]** — operator_stopped event emission and between-runs pause/stop/upgrade ownership are there; this spec's §7.1 and §8.4 consume but do not emit.
- **[operator-nfr.md §4.5 Checkpoint-format stability]** — N-1 compatibility contract referenced by §4.4.EM-022.
- **[process-lifecycle.md §4.2 Startup sequence]** — daemon walks the git checkpoint trail defined here during the reconciliation phase of startup; the submission RPC surface referenced by §4.9.EM-040 is declared there.

> INFORMATIVE: All inter-spec citations in this section are forward-references to specs not yet drafted; per template §Cross-reference convention ("Bootstrap-only — citing foundation docs"), these citations are expected to migrate to bootstrap form `[docs/foundation/components.md §<N>]` if the target spec is not finalized at this spec's `reviewed` gate. The normative migration obligation is tracked in OQ-EM-005.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforming to Core MVH MUST pass every requirement in EM-001 through EM-046 (including sub-requirements EM-015a, EM-015b, EM-015c, EM-017a, EM-018a, EM-020a, EM-023a, EM-024a, EM-025a, EM-031a, EM-034a, EM-034b, EM-034c, EM-036a, EM-041a, EM-042a, EM-043a, EM-046a, EM-046b) and invariants EM-INV-001, EM-INV-004, and EM-INV-005 (the three invariants surviving the §5 selection test; EM-INV-002, EM-INV-003, EM-INV-006 are retired). No requirement is deferred at MVH.

**Post-MVH extensions.** Failure-commit emission (deferred per §4.5.EM-025) and `recoverable-non-idempotent` node-type defaults (§4.2.EM-010) are additive extensions to Core MVH; neither is required to claim Core MVH conformance.

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose. Each requirement's test obligation:

- **EM-001 — EM-005 (core types).** Schema conformance tests: every persisted `Transition` file validates against the §6.1 record schema; every checkpoint commit's trailers validate against §6.2.
- **EM-006 — EM-011 (node attributes).** Workflow-validator unit tests covering each attribute check in EM-038, including negative cases (missing `idempotency_class`, invalid `handler_ref`).
- **EM-012 — EM-015, EM-015a — EM-015c (run model, lifecycle emissions).** End-to-end scenario tests covering one-bead-many-runs after `reopen-bead` verdict; intra-run-loop traversal verified distinct from re-run; Run.context mutation ordering verified pre-cascade; `run_started` emission verified after atomic-claim persist (EM-015a); `run_completed` and `run_failed` emission verified on terminal state with correct payload (EM-015b); terminal-state detection verified against `terminal_node_ids` and classifier verdicts (EM-015c).
- **EM-016 — EM-022 (checkpoint contract).** Crash-recovery scenario tests: kill the daemon between `git write-tree` and `git update-ref`; verify no partial state is observable AND orphan loose objects are eligible for `git gc` (EM-016 clarification); verify the trailer-and-sibling-file atomicity invariant; verify corrupted-checkpoint fallback (EM-017a) dispatches reconciliation AND bounds recursion at one level; verify audit tool (EM-020a) detects all five integrity violations (including run_id/trailer disagreement); verify sibling file is under `.harmonik/transitions/<run_id>/<transition_id>.json` (path scoping); verify cross-run merges and cherry-picks do not collide at the sibling-file path.
- **EM-023 — EM-026 (checkpoint cadence).** Integration tests verifying every durable transition produces exactly one commit; reconciliation workflows produce exactly one verdict commit; failure transitions produce zero commits; PARTIAL_SUCCESS produces a durable commit with `partial_success=true` flag; gate-denied transitions produce zero commits; branch-tip monotonicity check (EM-024a) flags externally-rewound task branches; transition-event emission never precedes `git update-ref` (EM-025a); ENOSPC retries with new transition_id and evidence-orphan cleanup.
- **EM-027 — EM-030 (outcome spine).** Cross-subsystem tests with twin handler: verify the full flow from handler outcome to transition-event projection; verify consumer retrieving the full trace via `git show`.
- **EM-031 — EM-033 (state reconstruction).** Restart scenario tests: destroy the daemon; confirm full state reconstructable from git + Beads without JSONL reads; confirm active-run discovery (EM-031a) correctly identifies in-flight runs from ref scan + Beads query; confirm no rollback on later-transition failure; confirm JSONL torn-tail does not produce false Cat 6b signal (EM-031); confirm Beads-unreachable triggers Cat 0 and `degraded` status rather than silent git-only fallback; confirm worktree state is preserved across crash → reconciliation dispatch.
- **EM-034 — EM-037 (sub-workflow).** Nested-workflow scenario tests: single run_id across nesting; checkpoint commits all on parent branch; namespaced `node_id` appears in state and transition records; mutual sub-workflow reference rejected by validator; sub-workflow-entered/exited lifecycle events fire; expansion pin is readable from the entry checkpoint's evidence map after daemon restart (EM-034c); registry updates between crash and restart do not change the run's expanded graph; sub-workflow terminal outcome at the parent's cascade matches the last-expanded-node's Outcome (EM-036a).
- **EM-038 — EM-040 (validation).** Validator unit tests for every failure mode listed in EM-038, including sub-workflow-reference cycle detection and missing `start_node_id` / empty `terminal_node_ids`.
- **EM-041 — EM-046, EM-046a, EM-046b (edge selection, backtracking, cycles).** Edge-cascade unit tests enumerating every precedence case; cycle-cap tests verifying `compilation_loop` failure at cap (disjoint from `structural`); traversal-counter recovery across restart verified by re-derivation from git log; rollback-transition tests verifying new `transition_id` and unchanged earlier commit; no-matching-edge scenario produces `structural` failure with reason `no_outgoing_edge_matches` (EM-046a); gate-deny enters `gate-pending` and waits for gate-resolution signal (EM-042a); RETRY re-dispatches the same node with context_updates applied pre-redispatch and fails as `transient` at retry-cap exhaustion (EM-046b).

Migration to `[testing.md §<layer>]` cross-references occurs within one revision cycle once testing.md lands; this obligation is tracked in OQ-EM-003.

### 10.3 Excluded conformance claims

- This spec does NOT grant conformance over: handler-specific wire protocol details (owned by [handler-contract.md]); the reconciliation category classifier (owned by [reconciliation/spec.md]); JSONL format specifics (owned by [event-model.md]); policy-expression grammar (owned by [control-points.md]); operator-CLI surface (deferred to a separate spec per [operator-nfr.md §4.10]).
- This spec does NOT guarantee performance or throughput bounds on checkpoint emission; those are operator-observable in [operator-nfr.md §4.8] (restart RTO) and are not requirements of this spec.

## 11. Open questions

#### OQ-EM-001 — Failure-commit policy for `git bisect` over failures

Question: Should failed transitions emit checkpoint commits (a new class of failure-commit) to enable `git bisect` in the improvement loop?
Owner: foundation-author
Blocks: none (MVH decision: no failure commits per §4.5.EM-025)
Default-if-unresolved: No failure commits. Revisit when the improvement-loop spec lands and can demonstrate a concrete need.

#### OQ-EM-002 — Schema-version bump policy for checkpoint and transition records

Question: Are additive field changes to the `Transition` record (new optional fields) the only allowed non-breaking change, or are there other non-breaking classes (rename via alias, type widening)?
Owner: foundation-author
Blocks: none (defaults to additive-only for MVH)
Default-if-unresolved: Additive-only. Aliases and widening are treated as breaking and require a migration release per [operator-nfr.md §4.5].

#### OQ-EM-003 — Migrate test-obligation prose to testing.md references

Question: Section §10.2 currently names test obligations in prose. The template §10.2 expects cross-references to `[testing.md §<layer>]` once testing.md lands.
Owner: foundation-author
Blocks: none (MVH prose obligations are in place)
Default-if-unresolved: Keep prose obligations; migrate within one revision cycle after testing.md is finalized.

#### OQ-EM-004 — `traversal_cap` declaration locus when edge participates in multiple cycles

Question: When a single edge participates in multiple cycles within a workflow graph, §4.10.EM-043 requires a cap on at least one edge in each cycle; the cap's semantics if the edge is shared across cycles are clarified in §4.10.EM-043a (per-edge-per-run counter shared across cycles passing through that edge) but pathological multi-cycle cases have not been audited in practice.
Owner: foundation-author
Blocks: none (per §4.10.EM-043a the counter is per-(run_id, edge))
Default-if-unresolved: Per-(run_id, edge). Re-audit if pathological multi-cycle cases appear in practice.

#### OQ-EM-005 — Bootstrap-citation migration for forward-referenced specs

Question: The inter-spec citations in §9.1 and §9.3 reference specs not yet drafted as of 2026-04-23 (only this spec exists at v0.2). Per template §Cross-reference convention, these citations MUST migrate to bootstrap form `[docs/foundation/components.md §<N>]` if the target is not finalized at this spec's `reviewed` gate. `ActionDescriptor` (defined in handler-contract §4.1 per §6.1) is the most load-bearing forward cite.
Owner: foundation-author
Blocks: spec advancing to `status: reviewed`
Default-if-unresolved: At the next review pass, any forward cite whose target is not yet `reviewed` rewrites to the bootstrap form; re-migrate once the target finalizes.

#### OQ-EM-006 — Add `ErrCompilationLoop` sentinel or retain sub-tag mechanism

Question: §8 now treats `structural` and `compilation_loop` as disjoint classes at the daemon level, but handler-contract §4.5 defines only five `ErrX` sentinels. Should handler-contract add `ErrCompilationLoop` as a sixth sentinel, or does the daemon-observed-only detection path (cap hit at cascade) mean the handler never needs to return this class?
Owner: foundation-author + handler-contract-author
Blocks: §8.6 handler-vs-daemon boundary clarity
Default-if-unresolved: Daemon-observed only. Handler-contract retains five sentinels; `compilation_loop` is never a handler-returned class.

#### OQ-EM-007 — Sub-workflow's terminal outcome composition when the sub-workflow has branching terminals

Resolved in v0.3 by §4.8.EM-036a. The sub-workflow's terminal outcome is the `Outcome` produced by the last expanded-node executed before hitting a terminal node; aggregation/composition is explicitly rejected because the sub-workflow's execution is sequential in run time and exactly one terminal is reached. Parent cascade on outgoing edges consumes this `Outcome` mechanically. The OQ is retained here as a retired entry per the IDs-do-not-reuse discipline; no further action is required.

Question: (resolved, see EM-036a)
Owner: foundation-author (resolved)
Blocks: none
Default-if-unresolved: (resolved)

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft. |
| 2026-04-23 | 0.2.0 | foundation-author | Round-1 reviewer integration. Dropped event-model from `depends-on` (breaks cycle; moved to §9.3). Added typed ID aliases (RunID/StateID/TransitionID/NodeID/BeadID/CommitRange). Added `start_node_id` and `terminal_node_ids` to Workflow schema. Added `context` field to Run schema. Added new requirements EM-017a (corrupted-checkpoint fallback), EM-018a (transition_id uniqueness contract — chose UUIDv7 uniqueness over run-scoped path as less disruptive), EM-020a (audit tool rule), EM-023a (durability decision table; includes PARTIAL_SUCCESS handling), EM-031a (active-run discovery), EM-034a (sub-workflow node-ID namespacing), EM-034b (sub-workflow reference acyclicity), EM-041a (context-update ordering), EM-043a (traversal-counter storage locus). Retired invariants EM-INV-002 (duplicate of EM-023), EM-INV-003 (duplicate of EM-019), and EM-INV-006 (duplicate of EM-012) per §5 selection test; surviving invariants keep original IDs EM-INV-001, EM-INV-004, EM-INV-005 (retired IDs are not reused per template). Fixed MUST/SHOULD discipline on EM-010 (normative defaults), EM-017 (added fallback), EM-020 (removed redundant Axes per declaration exemption — moved to EM-017/EM-018 write requirements), EM-025 (strengthened to MUST NOT at MVH), EM-039 (positive phrasing), EM-041 (collapsed double-modal). Defined "durable transition" mechanically in glossary + EM-023a. Defined "terminal node" and "in-flight run" in glossary. Deferred sub-workflow event names to event-model per §2.2 scope discipline (EM-036). Fixed emission ownership in §7.1 and §8 (operator_stopped, budget_exhausted owned upstream). Clarified `compilation_loop` vs `structural` as disjoint at emission. Removed `Harmonik-Verdict-Executed` from §6.2 trailer table (deferred to reconciliation §9.5b). Added EM-016 atomic-commit plumbing (write-tree / commit-tree / update-ref). Added EM-046 rollback-to-state-id constraint for context-restore. Added OQ-EM-005 (bootstrap migration), OQ-EM-006 (ErrCompilationLoop coordination), OQ-EM-007 (sub-workflow terminal outcome composition). |
| 2026-04-24 | 0.3.0 | foundation-author | Round-2 reviewer feedback integrated: path-scoped transition records, main-loop pseudocode, sub-workflow pin durability, 8 xref fixes, 4 missing-case requirements. Path-scoped the sibling-file path from `.harmonik/transitions/<transition_id>.json` to `.harmonik/transitions/<run_id>/<transition_id>.json` (EM-018/EM-019/EM-020a/EM-021/§6.1 Checkpoint/§7.2); EM-018a reframed from globally-unique contract to daemon-local generation rule (structural uniqueness now provided by path scoping). Added §7.4 main-loop protocol pseudocode (`orchestrator_main_loop` + `execute_workflow`) plus EM-015a (run_started emission), EM-015b (run_completed/run_failed emission), EM-015c (terminal detection rule). Added EM-034c (sub-workflow expansion pin durable on entry checkpoint's evidence map), EM-036a (sub-workflow terminal outcome is last-expanded-node's Outcome; resolves OQ-EM-007). Added EM-024a (branch-tip monotonicity check vs external force-push), EM-025a (emission ordering + ENOSPC handling + evidence orphan cleanup), EM-042a (gate-deny continuation via gate-pending sub-state), EM-046a (no-matching-edge failure class = structural), EM-046b (RETRY re-dispatch protocol). Clarified EM-016 loose-object atomicity (orphan loose objects eligible for git gc). Extended EM-017a with reconciliation-recursion bound (one level). Extended EM-031 with JSONL torn-tail tolerance + git-corroboration rule for divergence-evidence. Extended EM-031a with Beads-unreachable cross-link to Cat 0 + worktree-preservation rule (no pre-dispatch cleanup). Reframed EM-INV-004 to span four subsystems' authoring surfaces. Updated EM-046 to name daemon-synthesized Outcome for context-restore; EM-023a references the synthesized-Outcome rule and adds `outcome_status` as first-class Transition field (§6.1). Added `workflow_class` validator constraint (MVH only value: "reconciliation"). Fixed 8+ broken cross-refs: `[beads-integration.md §10.x]` → `§4.x` / `BI-NNN` (EM-013, EM-014, EM-031, EM-INV-005, §2.2, §6.1 BeadID, §6.1 Run.bead_id, §9.3); `[reconciliation.md §9.x]` → `§4.x` / `§8` / `RC-NNN` (EM-017a, EM-024, EM-025a, EM-031, EM-031a, EM-033, EM-INV-004, EM-INV-005, §2.2, §4.2 informative, §9.3). ID freeze at `reviewed`: new IDs EM-015a, EM-015b, EM-015c, EM-024a, EM-025a, EM-034c, EM-036a, EM-042a, EM-046a, EM-046b are assigned; no IDs retired in v0.3. Status draft → reviewed. |
| 2026-04-24 | 0.3.1 | foundation-author | Corpus-wide cleanup pass (no semantic changes). Migrated legacy architecture.md citation anchors to the §4.N map per the v0.2 NOTE: §1.1→§4.1 (×4 in §4.1.EM-001 four-axis tagging clause, §6.1 AxisTags/ModeTag reference, §6.1 Node.axes comment, and §9 cross-refs), §1.2→§4.2 (×2 in §4.2.EM-011 ZFC-tag obligation and §9 cross-refs), §1.3→§4.3 (×1 in §9 cross-refs), §1.5→§4.6 (×1 in §A.3 "Compilation loop" amendment-protocol informative block), §1.6→§4.8 (×1 in §6.1 Transition.actor_role comment), §1.8→§4.9 (×3 in §4.9 validator skip-path clause, §9 cross-refs, and §A.3 workflow-transactionality rationale), §1.9→§4.10 (×2 in §4.1.EM-001 three-artifact clause and §9 cross-refs). No requirement IDs, invariants, or schemas were touched. |
| 2026-04-24 | 0.3.2 | foundation-author | Corpus citation-drift cleanup pass 2: migrated legacy §N.N cross-spec anchors to current template §N.N form per the central remap table; ~45 citations fixed across the file. EV anchors: `§3.1→§4.1` (envelope), `§3.2→§8.1`/§6.3/§8 (event taxonomy vs payload registry per context), `§3.4→§4.4` (fsync), `§3.6→§4.5` (replay), `§3.7→§4.3` (bus/consumer). WM: `§5.1→§4.1` (worktree), `§5.8→§4.2` (branching), `§5.9→§4.9` (re-run rule), `§5.3→§4.7` (session log pipeline). ON: `§7.3→§4.3` (operator-control), `§7.5→§4.5` (N-1 compat), `§7.8→§4.8` (RTO), `§7.10→§4.10` (deferral). PL: `§8.1→§4.1` (daemon scope), `§8.2→§4.2` (startup sequence), `§8.5→§4.5` (agent subprocess), `§8.6→§4.6` (daemon vs orchestrator-agent distinction). Reconciliation path fix: `[reconciliation.md §N]` → `[reconciliation/spec.md §N]` (multi-file spec). CP: `§6.5→§6.3`/§6.4/§4.11 (policy YAML, grammar, skill surface per context), `§6.9→§4.5` (budget), `§6.11→§4.11` (skill discriminator). No requirement IDs, invariants, or schemas touched. |
| 2026-04-25 | 0.3.3 | foundation-author | Coordination patch wave landing two RC R2 cross-spec requests against EM. (1) Outcome-record extension for `reconciliation_verdict` envelope per RC-022a (resolves OQ-RC-010): chose approach (a) — discriminated variant — adding new EM-005a normative requirement, extending RECORD Outcome (§6.1) with `kind: OutcomeKind` (defaulting to `default`) and `payload: VerdictPayload | None` (cited to [reconciliation/schemas.md §6.1] VerdictEvent; not redeclared here), and adding ENUM OutcomeKind with values `{default, reconciliation_verdict}`. Approach (a) chosen over (b) because (i) it mirrors HC-008's existing `outcome_kind` discriminator on `outcome_emitted` so the daemon assigns `Outcome.kind` from the wire field without rewriting, (ii) it generalizes for future variants (improvement-loop, operator-CLI dispatch) under the amendment protocol, and (iii) it keeps the cascade and durability decision unchanged for `kind=default` outcomes — strictly additive at MVH. EM-005 expanded to enumerate the new fields; the `payload` is opaque to §4.10 cascade and §4.5.EM-023a durability decision (consumed only by the RC-025a verdict-executor). Unknown `kind` values route to Cat 6a per [reconciliation/spec.md §8.11]; no silent fallback. Existing v0.3.2 Outcome consumers remain conforming (default values mean no field reads change). (2) §6.2 trailer registry adds two RC-owned trailers per RC-002 / OQ-RC-002: `Harmonik-Workflow-Class` (Enum; conditional; values `{reconciliation}` at MVH; identifies reconciliation-workflow checkpoint commits for RC-002 dispatch dedup and RC-003b Cat 5 vs Cat 6a tiebreak; future workflow-class values extend via amendment protocol) and `Harmonik-Target-Run-ID` (UUID; conditional; the `run_id` being reconciled; coexists with the investigator-run's `Harmonik-Run-ID` on every reconciliation-workflow commit; RC-002a dedup keys on `(workflow_class, target_run_id)`). Both rows cite `reconciliation` as the owning spec; existing rows annotated with execution-model ownership for symmetry. The §6.2 informative note expanded to clarify that EM trailer-lint tooling and §4.4.EM-020a audit tooling MUST recognize the RC-owned trailers (any unknown trailer is a lint violation), and that the `Harmonik-Verdict-Executed` trailer remains RC-owned and not cross-listed per the EM v0.2.0 trailer-rollback decision. New requirement ID: EM-005a (assigned in the gap after EM-005; FROZEN per ID discipline). No IDs retired; no IDs renumbered. Schema additions are N-1 readable per §4.4.EM-022. Status remains `reviewed`. |

## A. Appendices

### A.3 Rationale

**Why transition records are sibling files, not commit-message-only trailers.** The AlphaGo trace fields are large (candidate_actions, evidence, verifier_metrics). Embedding them in commit messages bloats `git log` output, complicates parsing, and mixes human-readable audit text with structured data. The sibling-file pattern (trailer-as-index + file-as-body) keeps commit messages human-scannable while preserving deterministic retrieval via `git show`. Alternatives considered and rejected: trailer-only (message bloat), a separate non-git store (schema drift, extra store to reconcile), in-commit large-blob references (complicates retrieval and breaks N-1 readability).

**Why the outcome-spine is one flow with two views (record and event).** An earlier proposal considered two independent writes — one to git (trace) and one to the event bus (transition event) — which would introduce schema drift and storage duplication. The chosen shape (one record, two retrieval paths) is cheaper in storage, eliminates drift, and matches the observable-vs-authoritative split already enforced for state reconstruction.

**Why failure commits are deferred.** MVH does not have an improvement loop that needs `git bisect` over failures. Adding failure commits pre-emptively would bloat the checkpoint trail on routine failure (timeouts, rate limits) and complicate the reconciliation detectors' "last durable state" discovery. The design slot is preserved per §4.5.EM-025 so the additive change is cheap if needed.

**Why workflow-level transactionality is explicitly forbidden.** Rolling back prior checkpoints on later-transition failure would require either (a) rewriting git history (forbidden by §4.4.EM-020) or (b) a shadow state store (which would duplicate git's role and invite drift). The chosen shape — checkpoints are append-only, recovery is via reconciliation categories — is simpler and matches the "deterministic skeleton, probabilistic organs" architectural frame. See [architecture.md §4.9] and the reconciliation category taxonomy at [reconciliation/spec.md §8].

**Why v0.3 switched from UUIDv7 globally-unique transition IDs to a run-scoped sibling-file path.** v0.1's sibling-file path `.harmonik/transitions/<transition_id>.json` is un-scoped; r1 flagged that cross-run merges or cherry-picks (improvement-loop scenarios, scenario-harness replays) could collide. v0.2 closed this with EM-018a: "every `transition_id` MUST be UUIDv7 AND globally unique." r2's skeptic review observed that UUIDv7 provides only probabilistic uniqueness — a 74-bit random tail appended to a millisecond timestamp — and promoting this to MUST without a mechanical enforcement locus is a contract over an assumption. Three defensible shapes were surfaced: (a) name a daemon-only generation locus with a per-process monotonic counter; (b) drop the MUST to SHOULD and rely on EM-020a audit; (c) scope the sibling-file path by `run_id`.

v0.3 chose (c) — path-scoping under `<run_id>/` — because it is a structural guarantee rather than a probabilistic one: cross-run path collision is impossible by construction when each run's transitions live in a disjoint sub-directory. The cascading path changes r2's integration described (EM-018, EM-019, EM-020a, §6.1 Checkpoint, §7.2 pseudocode) are mechanical and well-bounded. EM-018a is retained as the daemon-local generation rule (UUIDv7 with optional monotonic counter within a single daemon process) so that within-run uniqueness of `transition_id` is mechanical; globally-unique across runs is no longer asserted. This is the r2-correct fix: v0.2 chose "less disruptive" over "mechanically sound"; v0.3 chooses "mechanically sound" and absorbs the surface cost.

**Why the transition record carries `outcome_status` as a first-class field.** v0.2 keyed EM-023a's durability decision on `outcome.status` while the Transition record itself carried no such field; the association was implicit in the cascade flow (§7.3). r2's skeptic flagged this as an assumption whose implementer must cross-read §7.3 to know which Outcome belongs to which transition. v0.3 adds `outcome_status` directly to the Transition RECORD (§6.1) and names the field in EM-023a as the decision-procedure input. The association is now mechanical and schema-anchored rather than dataflow-implicit.

**Why context-restore produces a daemon-synthesized Outcome.** v0.2's EM-023a included `context-restore` as a durable transition_kind, but EM-046 described it as a daemon/operator/reconciliation-initiated operation that does not route through a handler. r2's skeptic flagged the category error: what produces `outcome.status` for a context-restore? v0.3 resolves via EM-046 (amended) + EM-023a (amended): the daemon synthesizes an `Outcome` with `status = SUCCESS` and `actor_role ∈ {daemon, reconciliation}`, recorded in the Transition record's evidence map under `evidence.synthesized_outcome=true`. EM-023a applies unchanged under the synthesized Outcome; no carve-out is required.

**Why sub-workflow expansion pin is durable on the entry checkpoint.** r2's crash-adversary review identified the strongest single gap: v0.2 claims the load-time sub-workflow expansion pin "survives until run terminal state" but provides no durable backing. A crash between parent-sub-workflow-entry and first nested checkpoint produces a non-restartable run, and a sub-workflow registry update across the crash could change the expanded graph. v0.3 adds EM-034c: the entry checkpoint's Transition record carries `evidence.sub_workflow_pin` with `{sub_workflow_ref, sub_workflow_version, resolved_workflow_id}`. On restart, the daemon reconstructs the pinned expansion by reading this key from the most recent sub_workflow_entered transition record, NOT by re-consulting the registry. This uses existing infrastructure (EM-021 evidence externalization) so it costs nothing at the schema layer.

**Why the invariant EM-INV-004 was reframed to be genuinely cross-subsystem.** r2's skeptic flagged EM-INV-004 as borderline under the template's selection test: v0.2 framed it as "no subsystem may implement a mechanism that atomically rolls back prior checkpoints" with an adjective list of constrained subsystems, which reads as a §4 requirement with scope gloss. v0.3 reframes the invariant as "any subsystem that writes to git, Beads, or workspace state MUST NOT implement an undo-previous-N-operations primitive" — a property that each of execution-model, workspace-model, beads-integration, and reconciliation could violate independently at their authoring surfaces. The invariant now passes the selection test: a subsystem could ship a conforming local requirement yet still violate this invariant by shipping a composable primitive. The failure mode the invariant guards is composition-level atomic undo, which no single §4 can prevent on its own.

**Why the main-loop protocol was elevated to §7.4.** r2's orchestrator-implementer review found that v0.2's §7.2 (checkpoint-and-emit) and §7.3 (cascade) are clean drop-in pseudocode but the end-to-end main loop — from bead-claim to run-termination — is not expressible as a closed function against the spec alone. Three specific gaps: the `pick_one → create_run → dispatch` prefix has no owning section; the "when does a run end" decision lacks a single requirement; the dispatch-to-cascade handoff has no protocol analog. v0.3 adds §7.4 with `orchestrator_main_loop` and `execute_workflow` pseudocode, anchored by normative EM-015a (run_started emission), EM-015b (run_completed/run_failed emission), and EM-015c (terminal detection). S01 (Orchestrator Core) now has a single reading target rather than a four-spec cross-read.
