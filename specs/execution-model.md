# Execution Model

```yaml
---
title: Execution Model
spec-id: execution-model
requirement-prefix: EM
status: reviewed
spec-category: foundation-cross-cutting
spec-shape: requirements-first
version: 0.8.2
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-05-31
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
- Node type enum (`agentic`, `non-agentic`, `gate`, `sub-workflow`) and node idempotency-class tag (`idempotent`, `non-idempotent`, `recoverable-non-idempotent`). (The pre-C1 `control-point` value is collapsed under `gate`/`non-agentic` per C1 + C4 bundle — see §4.2.EM-006.)
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
- Concurrency primitives: in-flight-run capacity gate, claim-write serialization, and `max_concurrent` configuration (§4.11).

### 2.2 Out of scope

- Event schemas, payload field lists, and event names — owned by [event-model.md §8]. This spec declares emission obligations; event-model declares names and shapes.
- Handler interface, LaunchSpec shape, agent-lifecycle events — owned by [handler-contract.md §4.1, §4.2].
- Workspace leasing, branching conventions, branch-creation lifecycle, merge semantics — owned by [workspace-model.md §4.3, §4.2, §4.5].
- Reconciliation-category classification, investigator-agent contract, verdict vocabulary — owned by [reconciliation/spec.md §8 (Cat 0–6), §4.4 (RC-015–019), §4.5 (RC-020–025)].
- Beads-CLI adapter, terminal-transition writes, idempotency-keyed intent log — owned by [beads-integration.md §4.2 BI-002, §4.4, §4.10].
- Policy expression grammar, control-point primitive, freedom profiles, budget counter semantics — owned by [control-points.md §4.7, §4.1, §4.6, §4.5].
- Operator control semantics (pause/stop/upgrade between tasks), operator-event emission — owned by [operator-nfr.md §4.3].
- Daemon process lifecycle, startup sequence, command surface — owned by [process-lifecycle.md §4.1, §4.2].
- Execution-queue data model, identity, group state machine, validation, append semantics, and queue lifecycle — owned by [queue-model.md §3 QM-002, §4 QM-010..012, §5, §6, §7, §8]. This spec consumes the queue as dispatch input; queue-model declares its structure.

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
- **workflow_mode** — a `Run`-record field carrying one of `{single, review-loop, dot}`. Resolved at claim time per §4.3.EM-012a; immutable for the run's lifetime. Selects the run's dispatch shape: `single` is the historical one-handler-per-node default; `review-loop` is the hardcoded two-node implementer→reviewer cycle defined in §4.3.EM-015d; `dot` is the general workflow-graph walker defined in §7.5 (binding document for `workflow_mode = dot`). (see §4.3.EM-012, §4.3.EM-012a, §4.3.EM-015d, §7.5)
- **claude_session_id** — the Claude Code session identifier (a UUIDv7 minted handler-side per [handler-contract.md §4.10 HC-045c] and reported to the daemon via the handler's `handler_capabilities` progress-stream message per [claude-hook-bridge.md §4.7 CHB-018]) and consumed by `claude --resume <id>` to continue the same Claude session across subprocess launches. Stored in `Run.context.claude_session_id` under `review-loop` mode per §4.3.EM-015d via the durable-checkpoint discipline of [claude-hook-bridge.md §4.6 CHB-023]. Distinct from harmonik's internal `session_id` event field per [event-model.md §8.3], which is a UUIDv7 generated by the handler per [handler-contract.md §4.1] and is opaque to non-handler consumers. The two identifiers MUST NOT be conflated: `claude_session_id` is the Claude-Code session continuity identifier; `session_id` is the harmonik handler-session identifier.
- **iteration_count** — under `review-loop` mode, the 1-based count of implementer→reviewer cycles the run has completed at a given moment. Stored in `Run.context.iteration_count`. Capped at 3 per §4.3.EM-015e. (see §4.3.EM-015d, §4.3.EM-015e)
- **needs-attention** — under `review-loop` mode, the close-path marker applied to the bead when the cycle terminates without an `APPROVE` verdict (cap-hit, `BLOCK`, or no-progress early-exit). Operator-drained only; no auto-retry. The Beads label and write semantics are owned by [operator-nfr.md §4.3] and [beads-integration.md §4.4]; this spec names the close path.
- **active queue** — the queue currently loaded in the daemon and authoritative as dispatch input per [queue-model.md §3 QM-002]. The daemon dispatches exclusively from the active queue; absent an active queue, no dispatch occurs (§4.11, §7.4).
- **queue group** — a group of queue items within the active queue per [queue-model.md §5]. A group is the unit of group-advance gating per §4.3.EM-015f.
- **max_concurrent** — the daemon-wide ceiling on concurrently-running runs, configured at startup per §4.11.EM-051. Drives both the in-flight-run capacity gate (§4.11.EM-049) and the claim-write serialization token-pool (§4.11.EM-050).

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

Additionally, an `Outcome` MAY carry `failure_class ∈ {transient, structural, deterministic, canceled, budget_exhausted, compilation_loop}` (the `FailureClass` enum declared in §8), present ONLY when `status = FAIL`. The field is a handler-emitted HINT; the daemon back-fills from the §8 sentinel classification path per [handler-contract.md §4.5 HC-020] when the handler omits the field, and the daemon's classification is authoritative on disagreement per [handler-contract.md §4.2a HC-059]. The post-classifier engine-side `Outcome` MUST carry `failure_class` when `status = FAIL`. The field is carrier-only; classification authority remains with the daemon.

Classifier input for the failure taxonomy (§8) is the handler-returned `ErrX` sentinel per [handler-contract.md §4.5], NOT fields of the `Outcome` record.

Tags: mechanism

#### EM-005a — Outcome carries a kind discriminator and optional kind-typed payload

An `Outcome` MUST carry a `kind ∈ {default, reconciliation_verdict, gate_decision}` discriminator that names the shape of its extension `payload`. The discriminator's wire-level alias is the `outcome_kind` field on the handler-contract `outcome_emitted` event per [handler-contract.md §4.3 HC-008]; the daemon MUST set `Outcome.kind` from the handler-returned `outcome_kind` without rewriting.

The discriminator's semantics:

- `kind = default` — the legacy ordinary outcome shape. `payload` MUST be absent. Every requirement in this spec that consumes `Outcome` (the cascade per §4.10.EM-041, the durability decision per §4.5.EM-023a, the Transition.outcome_status mapping per §6.1) operates on `default` outcomes unchanged; no §4 requirement reads `payload` for routing or durability decisions.
- `kind = reconciliation_verdict` — the outcome carries a reconciliation investigator's verdict. `payload` MUST be the `VerdictEvent` record per [reconciliation/schemas.md §6.1]; this spec does NOT redeclare the `VerdictEvent` fields. Per [reconciliation/spec.md §4.5 RC-022a], the daemon's verdict-executor consumes the payload to construct the verdict-and-verdict-executed commit pair on the investigator's task branch under the §4.5.EM-026 verdict-only-commit exception. The `VerdictEvent` payload is opaque to the cascade and to the durability decision (which consumes `outcome_status` per §4.5.EM-023a and §6.1); the verdict-executor consumes the payload via the reconciliation outcome-spine path, not via the ordinary cascade.
- `kind = gate_decision` — the outcome carries a gate evaluator's structured decision rationale. `payload` MUST be the `GateDecisionPayload` record per [control-points.md §6.1.8 CP-058]; this spec does NOT redeclare the record's fields. Per EM-005b below, the gate-evaluator handler MUST emit this kind when the gate's policy requires structured rationale capture per [control-points.md §4.8 CP-040]; otherwise `kind = default` is permitted. The `GateDecisionPayload` is opaque to the cascade and to the durability decision (the cascade reads `status`, `preferred_label`, and `kind` only; `payload.*` is not a legal edge-condition LHS per the D4 LHS whitelist). The handler-side gate emission discipline is normative per [handler-contract.md §4.2a HC-060]. Gate-decision payload cross-reference: `[control-points.md §6.1.8 CP-058]`.

The enum is closed at v1; future outcome variants (e.g., improvement-loop verdicts, operator-CLI dispatch outcomes) extend the enum via the amendment protocol per [architecture.md §4.6] and MUST cite their payload schema in the owning subsystem spec. Adding a discriminator value is an additive schema change per §6.4 (N-1 readable per §4.4.EM-022); a reader observing an unknown `kind` value MUST route the outcome to reconciliation per [reconciliation/spec.md §8.11 Cat 6a] rather than silently degrading to `default`.

(Example application of the amendment protocol: the `gate_decision` value is added at v0.3.4 per EM-005b and is paired with the `GateDecisionPayload` record at [control-points.md §6.1.8 CP-058]; the existing N-1 routing rule applies — a v0.3.3 reader encountering `kind = gate_decision` MUST route to reconciliation Cat 6a, NOT silently degrade to `default`.)

Existing consumers of the v0.3.2 `Outcome` shape remain conforming: `kind` defaults to `default` and `payload` defaults to absent, and no §4 requirement that predates v0.3.3 reads either field. The extension is strictly additive.

Tags: mechanism

#### EM-005b — Gate-decision Outcome variant

A gate-evaluator handler (per [handler-contract.md §4.2a HC-058], gate row) emitting an Outcome that the gate's policy designates as requiring structured rationale capture per [control-points.md §4.8 CP-040] MUST emit `Outcome.kind = "gate_decision"` and `Outcome.payload = GateDecisionPayload(...)` per [control-points.md §6.1.8 CP-058]. When rationale capture is not required by policy, `Outcome.kind = "default"` is permitted and `payload` MUST be absent.

The five `GateDecisionPayload` fields (declared in CP-058) carry the gate's audit record: `policy_id` (the gate's registry name per [control-points.md §4.1 CP-002]), `decision` (`{allow, deny, escalate-to-human}` per [control-points.md §6.1.6 GateAction]), `decision_actor` (the actor that produced the decision; `"mechanism"` for mechanism-tagged evaluators or the role name for cognition-tagged evaluators per CP-058), `decision_evidence_ref` (optional audit pointer; commit SHA, event correlation id, etc.), and `resolution_signal_id` (optional; required when `decision = escalate-to-human`; correlates the EM-042a gate-resolution signal when the daemon resolves a `gate-pending` sub-state). A `gate_decision` Outcome's `status` MUST be `SUCCESS` regardless of the `decision` value — `allow`, `deny`, and `escalate-to-human` are all *successfully-evaluated* Gate verdicts, per [control-points.md §6.1.8 CP-058]. The cascade routes on the decision (via `outcome.preferred_label` carrying the decision string, or `outcome.kind = gate_decision`), NOT on `status`; `status = FAIL` is reserved STRICTLY for a Gate that could not be evaluated (see the next paragraph). The run remaining in the source state on a `deny` is governed by the cascade routing on the decision per [control-points.md §4.2 CP-009], not by the Outcome's status.

A Gate that CANNOT be evaluated (evaluator dispatch failure, missing input, cognition delegation path unavailable, escalation backend down) MUST return an Outcome with `status = FAIL`, a `failure_class` per §8, and NO `gate_decision` payload. The eval-failure path and the decision path are mutually exclusive: a FAIL gate Outcome carries no payload, and a `kind = gate_decision` Outcome is always `status = SUCCESS`.

The cascade per §4.10 EM-041 routes on the decision. Because every evaluated Gate is `status = SUCCESS`, `status` alone CANNOT distinguish `allow` from `deny` from `escalate-to-human`; the gate-evaluator handler MUST surface the decision on `outcome.preferred_label` (the decision string) so edge conditions of the form `outcome.preferred_label == 'deny'` route correctly per [workflow-graph.md §6 WG-014 / §7 WG-019]. `outcome.kind = "gate_decision"` is additionally a legal edge-condition LHS for discriminating gate outcomes from non-gate outcomes; `payload.*` subfields are NOT legal LHS. Workflows that need audit access to the payload consult it via the persisted-verdict path per [control-points.md §4.8 CP-040], not via the cascade.

`kind = gate_decision` and the `failure_class` field per EM-005c are MUTUALLY EXCLUSIVE carriers on a Gate outcome — they are NOT co-emitted. The two encode disjoint facts about disjoint outcomes:

- A Gate that *evaluated* — whatever the verdict — emits `status = SUCCESS`, `kind = gate_decision`, `payload = GateDecisionPayload{..., decision: ∈ {allow, deny, escalate-to-human}}`, and NO `failure_class`. A *policy* deny is a successful evaluation: `status = SUCCESS`, `payload = GateDecisionPayload{policy_id: "budget-gate", decision: deny, ...}`, no `failure_class`. The cascade routes the deny to the source state (or a deny edge) by reading the decision, not the status.
- A Gate that *could not evaluate* (e.g. its evaluator hit a budget exhaustion before producing a verdict) emits `status = FAIL`, `failure_class = budget_exhausted`, `kind = default`, and NO payload. There is no verdict to carry, so there is no `gate_decision` payload.

A single Gate outcome therefore carries `kind = gate_decision` (SUCCESS, decision in payload) OR `failure_class` (FAIL, no payload), never both. `failure_class` answers "why did this run fail?"; it has no meaning on a Gate that succeeded in evaluating, even when the verdict was deny.

The schema-version bump for the `gate_decision` enum value is the same v0.3.3 → v0.3.4 bump as EM-005c (the `failure_class` addition); the two extensions land coordinated at v0.3.4. Per the closing paragraph of EM-005a, adding a discriminator value is itself an additive schema change; v0.3.3 readers route unknown `kind = gate_decision` to reconciliation Cat 6a per [reconciliation/spec.md §8.11].

Tags: mechanism

#### EM-005c — Outcome schema v2: `failure_class` additive field

The `Outcome` record is bumped from schema v0.3.3 → v0.3.4 to admit the optional `failure_class` field per the EM-005 paragraph above. The bump is strictly additive: no existing field changes name, type, default, or position; no §4 requirement that predates v0.3.4 reads the new field.

Consumers of the v0.3.3 shape remain conforming: `failure_class` defaults to absent on handler emission, and the daemon-side classification path of [handler-contract.md §4.5 HC-020] (the sentinel classifier) remains the authoritative input for the failure taxonomy of §8. The v0.3.4 field is a carrier added so that `failure_class` becomes a first-class edge-condition LHS term per the D4 whitelist — the cascade of §4.10 EM-041 MAY consult `outcome.failure_class` as an edge predicate input.

N-1 readability per §6.4 is preserved: a v0.3.3 reader observing a v0.3.4 outcome with `failure_class` present MUST treat the field as unknown-and-optional and fall through to sentinel-based classification per HC-020. A v0.3.4 reader observing a v0.3.3 outcome with `failure_class` absent MUST consult HC-020 for the classification (same path as today — the field's absence is the default state).

`compilation_loop` is a daemon-only classification per [handler-contract.md §4.2a HC-059]; a v0.3.4 reader MAY refuse a handler-emitted `failure_class = compilation_loop` and override to `structural` with a logged disagreement.

Tags: mechanism

### 4.2 Node attributes

#### EM-006 — Node type is one of four declared kinds

A `Node` MUST declare `type ∈ {agentic, non-agentic, gate, sub-workflow}`. The four kinds are mutually exclusive; each node has exactly one type. No other type is accepted by the workflow validator of §4.9. The pre-C1 `control-point` value is collapsed under `gate` and `non-agentic` per the C1 amendment to [workflow-graph.md §4 WG-001/WG-002] and the C4 amendment to [control-points.md §4.12 CP-053/CP-054]; legacy `control-point` nodes are rejected by the validator post-bundle-landing (a §6.4 breaking change).

Tags: mechanism

#### EM-007 — Agentic, non-agentic, and gate nodes carry a handler reference; sub-workflow nodes do not (EM-060)

A node of type `agentic`, `non-agentic`, or `gate` MUST declare a `handler_ref` resolving to a handler registered per [handler-contract.md §4.1]. A node of type `sub-workflow` MUST NOT declare `handler_ref` (its handler discipline is delegated to the expanded sub-workflow's nodes per §4.8.EM-034). The `handler_ref` semantics per node type:

| Node type | `handler_ref` requirement | Handler role |
|---|---|---|
| `agentic` | MUST declare | Agent subprocess launched per [handler-contract.md §4.1] / [handler-contract.md §4.2]; produces `Outcome` per §4.1.EM-005. |
| `non-agentic` | MUST declare | Deterministic handler invoked per [handler-contract.md §4.1]; produces `Outcome` per §4.1.EM-005. The four-axis tags per §4.2.EM-011 distinguish non-agentic from agentic at authoring time; the spec-layer dispatch action is identical. |
| `gate` | MUST declare | Gate-evaluator handler per [control-points.md §4.12 CP-053/CP-054]; consumes the node's `gate_ref` and produces `Outcome` per §4.1.EM-005. The pre-C4 prose that `gate` nodes "MUST NOT declare `handler_ref`" is superseded. |
| `sub-workflow` | MUST NOT declare | Handler discipline delegated to the expanded sub-workflow's nodes per §4.8.EM-034. |

The pre-D3 `control-point` node-type per §4.2.EM-006 is collapsed under `gate` and `non-agentic` per the C1 amendment to [workflow-graph.md §4 WG-001/WG-002] and the C4 amendment to [control-points.md §4.12 CP-053/CP-054]; legacy `control-point` nodes are not accepted by the validator post-bundle-landing (the §4.2.EM-006 catalog bump is a §6.4 breaking change).

Reference resolution is validated at workflow ingest per §4.9.EM-038; for `dot`-mode runs the validator additionally enforces §7.5.3 item 7.

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

A `Run` MUST carry a stable `run_id`, `workflow_id`, `workflow_version`, `input` (a workspace reference per [workspace-model.md §4.1], not inline payload), `workflow_mode ∈ {single, review-loop, dot}` (resolved at claim time per §4.3.EM-012a; immutable for the run's lifetime; defaults to `single`), current `state`, `context` (a shared key-value map updated per §4.10.EM-041a), `start_time`, and optional `end_time`. A run executes EXACTLY ONE workflow invocation against EXACTLY ONE input; multi-workflow or multi-input runs are not permitted. Transition records for the run are discoverable via the task-branch commit range whose commits carry the run's `Harmonik-Run-ID` trailer; no separate `transitions` field on the `Run` record is required.

When `workflow_mode = review-loop`, the run's `context` map MUST carry, at minimum, the reserved keys `iteration_count` (Integer, 1..3, incremented per §4.3.EM-015d), `last_verdict` (one of `{APPROVE, REQUEST_CHANGES, BLOCK}` per the agent-reviewer JSON schema v1, or `null` before iteration 1's reviewer returns), `claude_session_id` (the Claude Code session identifier captured from the implementer's first launch per §4.3.EM-015d; reused on subsequent implementer launches via `claude --resume`), and `last_diff_hash` (String, the SHA-256 of `git diff <parent>..<head>` output on the run's task branch at the end of the prior iteration; used by the no-progress early-exit detector per §4.3.EM-015e). These keys are normative for `review-loop`; their absence in a `review-loop` run is an authoring error detected at claim time. For `workflow_mode = single`, no `context` keys are reserved by this spec. For `workflow_mode = dot`, the per-workflow context-key registry per [workflow-graph.md §10 WG-031] is the authoritative source of allowed `context.<key>` LHS references; this spec reserves no context keys for `dot` mode.

Tags: mechanism

#### EM-012a — Workflow-mode resolution precedence

The daemon's claim path MUST resolve a run's `workflow_mode` (per §4.3.EM-012) by walking the following tiers in order and selecting the first tier that produces a non-empty value:

1. **Per-task.** The bead's `workflow:<mode>` label per [beads-integration.md §4.3 BI-009a]. Accepted values: `workflow:single`, `workflow:review-loop`, `workflow:dot`. If the bead carries more than one `workflow:<mode>` label, the daemon MUST treat tier 1 as absent AND MUST emit `bead_label_conflict` per [event-model.md §8.8] before continuing the walk.
2. **Per-project.** The project-level configuration default. The resolution function MUST tolerate this tier's absence at MVH and pass through to tier 3; the tier slot is reserved for post-MVH operator configuration and is non-normative for MVH conformance.
3. **Per-daemon.** The daemon configuration default per [process-lifecycle.md §4.1 PL-004a].
4. **Built-in fallback.** `single`.

Resolution MUST run exactly once per run at claim time. The resolved value MUST be sealed into the Run record (§6.1) before any node in the run is dispatched and MUST NOT be re-evaluated for the lifetime of the run. The resolved value MUST be surfaced on the `run_started` event payload's `workflow_mode` field per [event-model.md §8.1] for downstream consumers.

A tier-1 input that names an unknown mode (a `workflow:<mode>` label whose `<mode>` value is not in `{single, review-loop, dot}`) MUST cause tier 1 to be treated as absent AND MUST emit `bead_label_conflict` with the offending label recorded in the payload.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### EM-012b — Model/effort resolution precedence

Tags: mechanism

The daemon's claim path MUST resolve a run's `(model, effort)` pair (the `ModelPreference` sealed into the Run record per §6.1) by walking the following tiers in order and selecting the first tier that produces a non-empty value:

1. **Per-task.** The bead's `model:<alias>` label AND the optional `effort:<level>` label. `effort` is a closed enum: accepted values are `low`, `medium`, `high`, `xhigh`, `max`. If the bead carries more than one `model:<alias>` label or more than one `effort:<level>` label, the daemon MUST treat tier 1 as absent AND MUST emit `bead_label_conflict` per [event-model.md §8.8] before continuing the walk. An unrecognised `effort:<value>` label MUST cause tier 1 to be treated as absent AND MUST emit `bead_label_conflict` with the offending label recorded in the payload. `model` and `effort` are resolved independently within tier 1: if a `model:<alias>` label is present but no `effort:<level>` label is present, only `model` is taken from tier 1; the walk for `effort` continues to tier 2.
2. **Per-project.** `.harmonik/config.yaml` at the project root, loaded once at daemon startup and cached for the lifetime of the process (restart required to reload). The normative schema is:

   ```yaml
   schema_version: 1          # required; only 1 is accepted at MVH
   agents:                    # optional map; absent = no per-project defaults
     claude-code:             # key is the core.AgentType string form
       model: sonnet          # optional; omitted = defer to tier 3
       effort: medium         # optional; omitted = defer to tier 3
     claude-twin:
       model: ""              # explicit empty = defer to tier 3
       effort: ""
   ```

   Agent-type lookup MUST use the `core.AgentType` string form as the map key (e.g. `claude-code`, `claude-twin`). Unknown agent keys MUST be silently ignored (forward-compatibility). An empty file (zero `schema_version` and empty `agents` map) MUST be treated as absent.

   Validation rules (all enforced by `LoadProjectConfig` in `internal/daemon/projectconfig.go`, the MVH reference implementation):
   - File absent → zero-value `ProjectConfig`, nil error; walk continues to tier 3.
   - File present and parseable with `schema_version = 1` → use entries; unknown agent keys are dropped silently.
   - File present but `schema_version ≠ 1` → `ErrUnsupportedConfigVersion`; daemon MUST refuse to start.
   - File present but YAML is malformed → `ErrMalformedConfigYAML`; daemon MUST refuse to start.
   - Absent `model` or `effort` key within a matched agent entry → treat that field as empty for tier-2 and continue the walk to tier 3 for that field only.
2.5. **Operator env-var default.** The process-environment variables `HARMONIK_CLAUDE_MODEL` and `HARMONIK_CLAUDE_EFFORT`, read at each tier-walk invocation (i.e. at claim time, not daemon startup). Because env vars are read at claim time, changing a variable takes effect on the next dispatch without a daemon restart — this is the intended "hot-reload" property. Rules:
   - `HARMONIK_CLAUDE_MODEL`: if non-empty, apply the same shape constraint as tier-1 (`model` must match `^[A-Za-z0-9._:/-]+$` and be ≤ 128 chars). A value that fails shape validation MUST be silently skipped; the walk continues to tier 3.
   - `HARMONIK_CLAUDE_EFFORT`: if non-empty, must be a member of the closed enum `{low, medium, high, xhigh, max}`. A value outside the enum MUST be silently skipped; the walk continues to tier 3.
   - `model` and `effort` are resolved independently within tier 2.5, using the same per-field independence rule as all other tiers.
   - The env-var names are exported constants (`daemon.EnvModelKey`, `daemon.EnvEffortKey`) in `internal/daemon/modelpreference.go`.
3. **Per-agent-type compiled default.** A static map keyed by `core.AgentType` providing a `(model, effort)` pair. Lives adjacent to the handler adapter wiring; entries are normative for the binding they describe and additive over time.
4. **Built-in fallback.** Empty for both fields — the handler tool applies its own default.

`model` and `effort` are resolved independently: each walks the tier list separately, and the first non-empty value wins for that field. The tier walk MUST run exactly once per run at claim time. The resolved `(model, effort)` pair MUST be sealed into the Run record as a `ModelPreference` descriptor (per §6.1) before any node in the run is dispatched, and the **tier walk** MUST NOT be re-evaluated for the lifetime of the run. The sealed pair is the **run-level default**: every node in the run uses it unless the node carries its own per-node override (EM-012b-NODE). The resolved pair MUST be surfaced on the `run_started` event payload per [event-model.md §8.1] for downstream consumers.

The `ModelPreference` descriptor is opaque to harmonik below the descriptor layer: harmonik validates the **shape** of `model` (see §6.1 for the normative invariants), not its value. Handler-side launch failure is the authoritative compatibility check for whether the resolved model is accepted by the underlying tool.

**EM-012b-NODE — Per-node override (tier 0).** Under `workflow_mode = dot`, an `agentic` node MAY carry a `model` and/or `effort` attribute per [workflow-graph.md §4 WG-042]. When present, the node's attribute value takes precedence over the run-level `ModelPreference` default (the tiers-1..4 result sealed per EM-012b) **for that node's dispatch only**. The per-node value is **static graph data** read from the already-loaded, already-validated workflow graph at the moment that node is dispatched; it is NOT a second resolution walk and MUST NOT re-evaluate bead labels (tier 1), project config (tier 2), compiled defaults (tier 3), or the fallback (tier 4). The run-level `ModelPreference` sealed into the Run record (§6.1) at claim time is unchanged by per-node overrides; the override is applied when the per-node launch specification is built, by substituting the node's value into that single node's launch `(model, effort)`. `model` and `effort` override independently: a node carrying only `model` inherits the run-level `effort`, and a node carrying only `effort` inherits the run-level `model`. Because the resolution inputs (the claim-time run-level seal plus the static graph attributes fixed at load time) are both immutable for the run's lifetime, replay determinism is preserved: a replay re-derives the same per-node `(model, effort)` for every node. The per-node value is opaque below the descriptor layer on the same terms as the run-level pair (shape-validated `model`, closed-enum `effort`; handler-side launch failure is authoritative).

Informative precedence summary, highest first:

```
tier 0    per-node attr (model="…" / effort="…" on the agentic node)   [EM-012b-NODE; static graph data, per node]
tier 1    per-task bead label (model:<alias> / effort:<level>)          (EM-012b, run-level)
tier 2    per-project .harmonik/config.yaml                             (EM-012b, run-level)
tier 2.5  operator env var HARMONIK_CLAUDE_MODEL / HARMONIK_CLAUDE_EFFORT  (EM-012b, run-level; hk-c5oxy)
tier 3    per-agent-type compiled default                               (EM-012b, run-level)
tier 4    built-in fallback (empty)                                     (EM-012b, run-level)
```

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

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

The daemon MUST emit the `run_started` event (per §6.5 and [event-model.md §8.1]) after `create_run` has allocated the run's `run_id` AND after the Beads atomic-claim write per [beads-integration.md §4.3 BI-009] has persisted the claim AND before any node in the run is dispatched. The `run_started` event payload MUST carry the `run_id`, `workflow_id`, `workflow_version`, and (when the run is bead-tied per §4.3.EM-014) `bead_id`. When the run originated from a queued dispatch per [queue-model.md §4 QM-010..012], the `run_started` event payload MUST additionally carry the optional `queue_id` and `queue_group_index` fields per [event-model.md §8.10]. These fields are absent for non-queued dispatch. If the daemon crashes between `run_id` allocation and `run_started` emission, the run is reconstructable from the Beads atomic-claim record per §4.7.EM-031a (no orphan runs).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-015b — run_completed and run_failed emission on terminal state

The daemon MUST emit exactly one of `run_completed` or `run_failed` (per §6.5 and [event-model.md §8.1]) when the run reaches terminal state per §4.3.EM-015c. `run_completed` emits when the run enters a node in `terminal_node_ids` with `outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS}`. `run_failed` emits when the classifier (§8) produces a terminal verdict, when the cascade returns `FAIL` (per §4.10.EM-046a or §4.10.EM-043), or when an operator cancel is observed per §7.1. The `run_failed` event payload MUST carry the failure class per §8 and the `last_checkpoint` SHA per §4.5.EM-025. When the run originated from a queued dispatch per [queue-model.md §4 QM-010..012], `run_completed` and `run_failed` event payloads MUST additionally carry the optional `queue_id` and `queue_group_index` fields per [event-model.md §8.10]. These fields are absent for non-queued dispatch. The terminal-transition bead write per [beads-integration.md §4.4 BI-010] MUST follow the terminal event emission; it MUST NOT precede it.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-015c — Terminal-state detection rule

A run reaches terminal state when (a) its current `node_id` is in the workflow's `terminal_node_ids` list (per §6.1) AND its last `outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS}` — terminating as `completed`; OR (b) the classifier (§8) produces a terminal failure verdict — terminating as `failed`; OR (c) an operator `stop --immediate` signal is observed per [operator-nfr.md §4.3] — terminating as `canceled`. Terminal detection MUST be evaluated after every state advance per §7.4 and MUST be the condition on the orchestrator's outer loop. A run that has reached terminal state MUST NOT be re-dispatched; a subsequent re-run against the same bead per §4.3.EM-014 produces a new run with a fresh `run_id`.

Tags: mechanism

#### EM-015d — Review-loop mode lifecycle

Tags: mechanism

A run with `workflow_mode = review-loop` (per §4.3.EM-012) MUST execute a hardcoded two-node cycle: `implementer → reviewer → {APPROVE: close, REQUEST_CHANGES: implementer, BLOCK: close-needs-attention, iteration-cap: close-needs-attention, no-progress: close-needs-attention}`. The graph is fixed by this spec and is NOT obligated to be expressed as a DOT document at MVH; the v1 driver is mode-specific code on the daemon's claim and dispatch path. This sub-graph shape is representable as an instance of the general workflow-graph model defined for `dot` mode (see §7.5 — `dot` mode binding).

The cycle MUST observe:

- **Single `run_id`.** The run's `run_id` is stable across every implementer-launch and reviewer-launch within the cycle. EM-015's intra-run-loops-are-not-new-runs rule applies unmodified: a `REQUEST_CHANGES` verdict routing back to the implementer is a run-internal loop, NOT a new run.
- **Multiple sessions under one run.** Each implementer-launch and each reviewer-launch is an OS subprocess and emits its own `session_id` on the §8.3 agent lifecycle events per [event-model.md §8.3]. The `session_id` event field is distinct from `Run.context.claude_session_id` per §3 Glossary.
- **Same Claude session resumed across implementer iterations.** The daemon MUST capture the implementer's Claude Code session identifier from the handler's `handler_capabilities` progress-stream message per [claude-hook-bridge.md §4.7 CHB-018] / [handler-contract.md §4.10 HC-045c]: the handler subprocess mints a UUIDv7 BEFORE exec'ing Claude and reports the value to the daemon via `handler_capabilities` carrying the `claude_session_id` payload field. The daemon MUST persist the captured value into `Run.context.claude_session_id` via the durable-checkpoint discipline of [claude-hook-bridge.md §4.6 CHB-023] (the persistence is a checkpoint-commit-class durable transition per §4.5.EM-023a, completing BEFORE the daemon returns the connection-accept ACK that gates the handler's `claude --session-id <uuid>` exec). On every subsequent implementer iteration (every `REQUEST_CHANGES` cycle), the daemon MUST resume the same Claude session via `claude --resume <claude_session_id>` per HC-045c. The implementer subprocess is fresh per iteration; the Claude session is logically continuous.
- **Reviewer launches are fresh per iteration.** Each reviewer launch is its own Claude session; the reviewer MUST NOT be resumed across iterations. The reviewer's verdict is read from the worktree at `.harmonik/review.json` (path normative per [workspace-model.md §4.7]); this spec consumes the verdict for routing.
- **Per-iteration state.** Before launching the reviewer, the daemon MUST compute `last_diff_hash` per §4.3.EM-015e and write it into `Run.context.last_diff_hash`. After the reviewer's verdict file lands, the daemon MUST read `.harmonik/review.json`, validate it against the `agent-reviewer` JSON schema v1, archive the file to `.harmonik/review.iter-<N>.json` (where `<N>` is the current `iteration_count`), and update `Run.context.last_verdict` to the verdict value.
- **Per-iteration events.** Each implementer resume MUST emit `implementer_resumed` per [event-model.md §8.1a]; each reviewer launch MUST emit `reviewer_launched`; the verdict-file read MUST emit `reviewer_verdict` carrying the agent-reviewer schema v1 fields verbatim plus `run_id`, `session_id`, and `iteration_count`. On no-progress detection per §4.3.EM-015e, the daemon MUST emit `no_progress_detected`. On cap-hit per §4.3.EM-015e, the daemon MUST emit `iteration_cap_hit`. On cycle termination (by any path), the daemon MUST emit exactly one `review_loop_cycle_complete` per [event-model.md §8.1a] carrying the `completion_reason`.
- **Iteration counter.** The daemon MUST initialize `Run.context.iteration_count = 1` at claim time and increment it by 1 immediately before each implementer dispatch after the first. `iteration_count` MUST NOT exceed the cap defined by §4.3.EM-015e.
- **Reviewer-feedback delivery to implementer-resume (EM-015d-RFD).** Before launching an `implementer-resume` (i.e., any implementer dispatch for iteration N ≥ 2), the daemon MUST execute the following ordered steps:

  1. **Write the reviewer-feedback file.** The daemon MUST write `${workspace_path}/.harmonik/reviewer-feedback.iter-<N-1>.md` (where `<N-1>` is the just-completed iteration's `iteration_count`) containing:
     - a header line: `# Reviewer feedback — iteration <N-1>` followed by a blank line;
     - the `verdict` field from the archived `review.iter-<N-1>.json` (one of `APPROVE`, `REQUEST_CHANGES`, `BLOCK`);
     - the `flags` array from the archived verdict, formatted as a Markdown unordered list (empty list rendered as the string `(none)`);
     - the full `notes` field from the archived verdict (no truncation; the truncated `prior_verdict_summary` on the bus event per [event-model.md §8.1a.1] is a separate derivative; the on-disk file carries the full text);
     - a `diff_summary` section containing the SHA-256 hex value stored in `Run.context.last_diff_hash` preceded by the label `diff_hash:` and, if available, the line-count of `git diff <parent>..<head>` output at the time the hash was computed, labelled `diff_lines:`.

     The write MUST use the atomic temp-write + rename + `fsync(parent_dir)` discipline of [workspace-model.md §4.7 WM-026]. The temp file MUST be created in the same directory (`.harmonik/`) so the rename is within the same filesystem. The file MUST exist and be readable before step 2 executes; the daemon MUST treat a failure to create the file as a daemon-side error and route the run per [handler-contract.md §4.6] failure handling without launching the implementer-resume.

  2. **Paste-inject the read instruction.** Only AFTER the file from step 1 exists on disk (i.e., the atomic rename in step 1 has completed), the daemon MUST inject the following instruction into the resumed Claude pane via the tmux paste mechanism defined by [process-lifecycle.md §4.3 PL-021b-PASTE] (specified by EM-015d-RIA step 3 for the reviewer launch; the same mechanism applies here for the implementer-resume):

     > Before continuing, read `.harmonik/reviewer-feedback.iter-<N-1>.md` in your worktree. It contains the prior reviewer's verdict, flags, and notes for iteration `<N-1>`. Address every flag marked `REQUEST_CHANGES` before proceeding.

     The paste-inject MUST occur AFTER `claude --resume <claude_session_id>` has reattached to the implementer's transcript (i.e., after the pane is live and the session has resumed, not during or before the `tmux new-window` spawn). The ordering invariant is: **file exists → pane is live → paste-inject fires**. The daemon MUST NOT paste-inject before the pane's process is observable via the tmux adapter (i.e., `WindowPanePID` returns a non-zero PID for the new window). If the paste-inject fails (tmux error, pane gone), the daemon MUST log the failure and route the run per [handler-contract.md §4.6] failure handling.

  The `reviewer-feedback.iter-<N-1>.md` file MUST be excluded from checkpoint commits via the [workspace-model.md §4.7 WM-013e] `.gitignore` hygiene set; it is workflow-control state, not work product. The file is NOT removed after the run; it persists in the worktree for post-run inspection.

  Tags (sub-clause EM-015d-RFD): mechanism
  Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

- **Reviewer input artifact (EM-015d-RIA).** Before launching the reviewer pane for any iteration (including iteration 1), the daemon MUST write the reviewer input artifact at `${workspace_path}/.harmonik/review-target.md` (path normative per [workspace-model.md §6.2 WM-RIA-001]). This file is the reviewer's sole structured context source; the reviewer is told via paste-inject (per the PL-021b-PASTE mechanism) to read it and produce `review.json` per [claude-hook-bridge.md §4.5 CHB-014]. The daemon MUST execute the following ordered steps:

  1. **Write `review-target.md` atomically.** The daemon MUST write `${workspace_path}/.harmonik/review-target.md` containing:
     - a header line: `# Review target — bead <bead_id>, iteration <N>` followed by a blank line;
     - **Bead context section** (`## Bead`): the bead's `id`, `title`, and full `body` as stored in the Beads ledger at the time of reviewer launch; no truncation;
     - **Diff section** (`## Diff range`): the base SHA (`Run.context.parent_commit` or the run's task-branch creation point) and the head SHA (current `HEAD` of the task branch at the moment the reviewer is launched), formatted as:
       ```
       base: <base_sha>
       head: <head_sha>
       ```
     - **Prior verdicts section** (`## Prior verdicts`) (OMITTED when `iteration_count = 1`): for each prior iteration `k` from 1 to `iteration_count - 1`, a sub-heading `### Iteration <k>` followed by the path reference `Verdict file: .harmonik/review.iter-<k>.json` and the one-line summary `verdict: <VERDICT>  flags: <flags-list or "(none)">  notes: <first 200 chars of notes field, truncated with "…" if longer>`. The full text is in the archived verdict file; the summary line is a navigation aid only.
     - **Reviewer-tier hints section** (`## Hints`) (MAY be empty; omit section entirely if no hints apply): any operator-configured reviewer-tier hints supplied via the run's `LaunchSpec.reviewer_hints` field (string); MUST be reproduced verbatim if present.

     The write MUST use the atomic temp-write + rename + `fsync(parent_dir)` discipline of [workspace-model.md §4.7 WM-026]. The temp file MUST be created in the same directory (`.harmonik/`) so the rename is within the same filesystem. The file MUST overwrite any prior-iteration `review-target.md` from the same run (the daemon re-generates it fresh for each reviewer launch; no append). The file MUST exist and be readable before step 2 executes; the daemon MUST treat a failure to create the file as a daemon-side error and route the run per [handler-contract.md §4.6] failure handling without launching the reviewer.

  2. **Spawn the reviewer pane.** Only AFTER the file from step 1 exists on disk (i.e., the atomic rename in step 1 has completed), the daemon MUST spawn the reviewer pane via `tmux new-window` per [process-lifecycle.md §4.3 PL-021b]. The pane MUST be observable (i.e., `WindowPanePID` returns a non-zero PID) before step 3 executes.

  3. **Paste-inject the start instruction.** Only AFTER the pane from step 2 is live, the daemon MUST inject the following instruction into the reviewer pane via the tmux paste mechanism defined by [process-lifecycle.md §4.3 PL-021b-PASTE]:

     > Read `.harmonik/review-target.md` in this worktree. It contains the bead context, the diff range to review, and any prior-iteration verdicts. Produce your verdict by writing `.harmonik/review.json` conforming to the agent-reviewer schema v1.

     The **ordering invariant** is: **file exists → pane live → paste-inject fires**. No step may execute before its predecessor completes. If the paste-inject fails (tmux error, pane gone), the daemon MUST log the failure and route the run per [handler-contract.md §4.6] failure handling.

  `review-target.md` MUST be excluded from checkpoint commits via the [workspace-model.md §4.7 WM-013e] `.gitignore` hygiene set; it is workflow-control state, not work product. The file is NOT removed after the run; it persists in the worktree for post-run inspection. Each reviewer launch overwrites the prior iteration's file; only the current iteration's content is live. The archived per-iteration verdict files (`review.iter-<N>.json`) are the durable record; `review-target.md` is ephemeral within a run.

  Tags (sub-clause EM-015d-RIA): mechanism
  Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

- **Implementer commit obligation is review-loop-scoped.** Under `workflow_mode = review-loop`, the implementer phase MUST advance the worktree HEAD (produce a commit) before the reviewer is launched; a clean implementer exit that does not advance HEAD is a cycle-internal failure routed per [handler-contract.md §4.6]. This commit obligation is specific to `review-loop` mode and does NOT apply to `workflow_mode = dot`. A `dot`-mode `agentic` node MAY relax the HEAD-advance requirement via the per-node `non_committing` attribute per [workflow-graph.md §4 WG-041]; that relaxation is gated on the per-node attribute and never reaches the `review-loop` path.

The `review-loop` cycle is NOT a sub-workflow per §4.8; the `EM-034` sub-workflow-expansion rule does not apply. Sub-workflow nodes MAY appear inside a node-level workflow whose `workflow_mode = single`, but `review-loop` itself is mode-driven, not graph-driven.

Tags: mechanism

#### EM-015e — Review-loop iteration cap and early-exit

A run with `workflow_mode = review-loop` (per §4.3.EM-012) MUST observe a hardcoded iteration cap of **3** at MVH. The cap is not operator-tunable for v1 conformance. The cap and the early-exit conditions below collectively determine the run's terminal-state classification per §4.3.EM-015c:

- **APPROVE early-exit.** On a reviewer verdict of `APPROVE` (regardless of `iteration_count`), the run MUST terminate as `completed` per §4.3.EM-015c with `outcome.status = SUCCESS`. The terminal-bead write per [beads-integration.md §4.4 BI-010] MUST follow the standard close path; no `needs-attention` marker is applied.
- **REQUEST_CHANGES with iterations remaining.** On a reviewer verdict of `REQUEST_CHANGES` AND `iteration_count < 3`, the daemon MUST increment `iteration_count` per §4.3.EM-015d, recompute `last_diff_hash` per the no-progress detector below, and re-dispatch the implementer via Claude-session resume per §4.3.EM-015d. The run MUST NOT terminate.
- **Cap-hit (REQUEST_CHANGES at iteration 3).** On a reviewer verdict of `REQUEST_CHANGES` AND `iteration_count = 3`, the daemon MUST emit `iteration_cap_hit` per [event-model.md §8.1a] and MUST terminate the run via the `needs-attention` close path. Bead-side, the run's terminal-transition write per [beads-integration.md §4.4 BI-010] MUST apply the `needs-attention` label per [operator-nfr.md §4.3].
- **BLOCK.** On any reviewer verdict of `BLOCK` (regardless of `iteration_count`), the run MUST terminate immediately via the `needs-attention` close path. Cap evaluation does not apply.
- **No-progress early-exit.** Before each reviewer launch from iteration 2 onward, the daemon MUST compute the SHA-256 hash of `git diff <parent>..<head>` output on the run's task branch (the diff of the worktree's current state against the run's parent commit). If the computed hash equals the prior iteration's `Run.context.last_diff_hash` value (i.e., the implementer produced a bit-identical diff against the parent), the daemon MUST emit `no_progress_detected` per [event-model.md §8.1a] AND MUST terminate the run via the `needs-attention` close path BEFORE launching the reviewer for the current iteration. The hash-equality test is the v1 detector; alternate detectors (Jaccard on changed-file set, Jaccard on hunk-set) MAY be substituted post-MVH via the amendment protocol per [architecture.md §4.6].

The terminal `run_completed` or `run_failed` event per §4.3.EM-015b MUST be preceded by exactly one `review_loop_cycle_complete` event per §4.3.EM-015d. The `review_loop_cycle_complete` payload's `completion_reason` MUST match the early-exit path taken: `approved` for APPROVE, `cap_hit` for the cap-hit path, `blocked` for BLOCK, `no_progress` for the no-progress detector, `error` for daemon-side failures observed within the cycle (e.g., verdict-file malformed or missing per §4.3.EM-015d).

The `needs-attention` close path is operator-drained only: the daemon MUST NOT auto-retry, MUST NOT auto-reopen the bead, and MUST NOT spawn a new run against the bead absent explicit operator action per [operator-nfr.md §4.3]. The semantics of the operator-drained queue are owned by [operator-nfr.md §4.3]; this spec is normative for when the bead enters that queue.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-015f — Queue-group advance is gated on all-terminal

The active queue group MUST NOT advance to the next group until every item in the active group has reached terminal status (`completed` or `failed`) per [queue-model.md §5 group state machine]. The daemon MUST evaluate the group's terminal condition after every `run_completed` or `run_failed` emission per §4.3.EM-015b whose `queue_id` and `queue_group_index` payload fields name the active group.

When the active group's terminal-count equals its item-count AND zero failures, the group transitions to `complete-success` and the queue advances. The daemon MUST emit `queue_group_completed` with `final_status = complete-success` then `queue_group_started` for the successor group per [event-model.md §8.10].

When the active group's terminal-count equals its item-count AND ≥1 failure, the group transitions to `complete-with-failures`, the queue transitions to `paused-by-failure` per [queue-model.md §8], and the daemon MUST emit `queue_group_completed` with `final_status = complete-with-failures` then `queue_paused` per [event-model.md §8.10]. The queue MUST NOT advance.

v0.1 has no `resume` operation; `complete-with-failures` is effectively terminal for the queue. The queue persists in `.harmonik/queue.json` per [queue-model.md §3 QM-001]; recovery is daemon-restart + re-submit.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

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

### 4.11 Concurrency

This section formalizes the daemon's concurrency primitives: the in-flight-run capacity gate, the claim-write serialization token-pool, and the `max_concurrent` configuration that drives both. These primitives sit beneath the queue-group state machine of [queue-model.md §5] and the dispatch loop of §7.4; they bound the daemon's parallel-dispatch surface uniformly across queue groups.

#### EM-049 — In-flight-run capacity gate

The daemon MUST cap the number of concurrently-running runs at `max_concurrent` (per §4.11.EM-051). Before dispatching a queued item, the daemon MUST ensure `in_flight_count() < max_concurrent`; if the gate is closed, the daemon MUST wait for a slot to open (an existing run reaching terminal per §4.3.EM-015b) before evaluating the next item. The gate applies uniformly across queue groups: a wave of N items with `max_concurrent = K` runs at most K at a time within that group. Advance-to-next-group MUST NOT occur until the wave's all-terminal condition is met per §4.3.EM-015f; the capacity gate does not interact with group advancement beyond pacing dispatch within the active group.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-050 — Claim-write serialization

The daemon MUST serialize concurrent `ClaimBead` writes (per [beads-integration.md §4.3 BI-009]) via a token-pool of size `max_concurrent` (per §4.11.EM-051). Each dispatch path MUST acquire a token before invoking `ClaimBead` and release it after the claim write returns (success or failure). This bounds concurrent SQLite-write surface; it is distinct from §4.11.EM-049's in-flight-run ceiling because claim writes are far shorter than run lifetimes and the two surfaces have different throughput characteristics. The token-pool primitive is implementation-abstract: a buffered-channel semaphore is one valid implementation; alternative primitives that satisfy the size-`max_concurrent` serialization contract are conforming.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EM-051 — `max_concurrent` configuration

The daemon MUST accept `max_concurrent` as a startup-time integer ≥ 1 (default 1) via the `--max-concurrent` flag per [process-lifecycle.md §4.1]. Runtime mutation is out of scope for v0.1; the value MUST be sealed at daemon startup and MUST NOT be re-read for the lifetime of the daemon process. Requirements §4.11.EM-049 and §4.11.EM-050 share the same `max_concurrent` value.

Tags: mechanism

> **EM-NOTE-WAKE (informative, 2026-05-30).** hk-24xn1 (daemon wake on submit/append) is **closed**. `QueueStore.SetQueue` (called on every accepted `queue-submit` and `queue-append`) signals `wakeC` via a buffered-1 channel; the workloop's `workloopSleep`/`workloopIdleWait` helpers select on `wakeC` alongside the poll timer (`internal/daemon/queuestore_hkj808w.go:97-104`). A newly submitted or appended item wakes the workloop at sub-poll-interval latency; no poll-interval wait. Earlier CLAUDE.md guidance ("append while idle sits `pending` until the next workloop tick") was accurate before hk-24xn1; it is now stale. §4.13 eager-refill relies on this sub-poll-interval guarantee.
>
> **EM-NOTE-STREAM-CONCURRENCY (informative, 2026-05-30; V2 correction).** A stream group at `max_concurrent > 1` DOES run multiple items concurrently. `streamEligible()` (`internal/queue/state.go:283`) SKIPS `dispatched` items — does not treat them as HOL blockers. The HOL rule (QM-035) applies only when the head is `deferred-for-ledger-dep`; a `dispatched` head is skipped and the next `pending` item is returned eligible. Earlier "use `--wave` when `--max-concurrent > 1`" guidance was INCORRECT and is SUPERSEDED. `--wave` is for immutable-at-submit group semantics (no mid-flight appends), NOT for enabling concurrency. Stream groups support both concurrency AND mid-flight append; wave groups support concurrency but are append-closed.

#### EM-066 — No-auto-pull (queue-only) daemon topology

The daemon MUST accept a startup-time boolean flag `--no-auto-pull` (alias `--queue-only`; `--no-auto-pull` is the canonical name) via [process-lifecycle.md §4.1]. The value MUST be sealed at daemon startup and MUST NOT be re-read for the lifetime of the daemon process (same discipline as §4.11.EM-051).

When `--no-auto-pull` is set, the daemon MUST NOT fall back to `br ready` for dispatch input. A daemon booted in this topology with no submitted queue MUST take the `idle_wait_for_queue_submission()` branch of §7.4 and dispatch **zero** runs — it MUST NOT emit `run_started`, MUST NOT spawn any agent subprocess, and MUST consume no agent credit — until a queue is submitted over the daemon socket per [queue-model.md §3 QM-002]. The §7.4 `queue IS None` branch MUST route to `idle_wait_for_queue_submission()` and MUST NOT call `br ready` while this flag is set.

When `--no-auto-pull` is NOT set, the daemon retains the historical single-daemon `br ready` fallback (§7.4 fallback branch) as an explicit opt-in for non-queue-driven deployments.

**Default (amended — hk-8vy18).** Queue-only is now the default for **all** daemon topologies: a bare `harmonik --project` boot with no submitted queue dispatches zero runs without requiring any explicit flag. The `br ready` fallback is opt-in via `--auto-pull`; `--no-auto-pull` is accepted as a no-op back-compat alias (it was the opt-in flag before this amendment; passing it is now redundant but harmless). This supersedes the earlier topology-scoped default (supervised ON, historical OFF).

Tags: mechanism

#### EM-067 — Operator-pause binding and defense-in-depth gate on the `br ready` fallback path

**Relationship to the §7.4 loop-top gate (normative clarification).** The §7.4 main-loop's loop-top check `IF should_pause_between_runs(): wait_for_resume(); CONTINUE` is the **primary** operator-pause gate, and it covers ALL dispatch paths — including the `br ready` fallback branch — because `should_pause_between_runs()` is precisely the between-task drain-gate hook of [operator-nfr.md §4.3 ON-008]: when the operator-control state is `pausing`/`paused` per ON-011, the loop-top check returns true and the loop `CONTINUE`s before control reaches the `queue IS None` branch at all. This requirement therefore does **not** introduce a new primary gate; the fallback path is already operator-pause-honoring by virtue of the loop-top ON-008 check. (This resolves the apparent redundancy: EM-067's inline branch gate is a belt-and-suspenders assertion, not the load-bearing protection, per the must-fix resolution recorded in [06-integration.md §EM-067 coherence resolution].)

**Load-bearing content (single source of pause truth).** What EM-067 normatively establishes — and what the loop-top check alone does NOT — is the **binding of the fallback-path pause behavior to the single source of pause truth** produced by [operator-nfr.md §4.3 ON-056/ON-057] (`operator_pause_status`), the same signal that drives the queue-level `active → paused-by-drain` transition per [queue-model.md §8.5 QM-054]. This guarantees that the fallback dispatch branch and the active-queue dispatch branch (gated by §7.4 on `Queue.status`) honor ONE pause concept rather than two divergent ones. EM-067 defines no parallel pause concept; it names which pause state the fallback path is bound to.

**Defense-in-depth assertion (inline branch gate).** As a redundant safety assertion, when the fallback path is reached (which, per the clarification above, can only occur if an implementation has scoped `should_pause_between_runs()` more narrowly than the full ON-008 operator-control state — e.g. to handler-pause only — and thereby bypassed the loop-top operator-pause gate), the fallback branch MUST independently check the operator-control state: if it is `pausing` or `paused` per ON-011, the fallback path MUST NOT dispatch — it MUST take the §7.4 idle-wait branch and MUST NOT call `br ready` or emit `run_started` until the state returns to `running` (via `resuming`) per [operator-nfr.md §7.1]. A conforming implementation where the loop-top check covers the full operator-control state will find this inline branch never taken under operator pause; that is correct and intended (the assertion holds vacuously). The gate is effective once the ON-056/ON-057 producer emits in production; absent that producer both the loop-top check and this inline assertion behave as a daemon with no operator pause in effect.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

## 4.12 Run-branch merge-to-main

The two success branches in Step 9 of the daemon's single-run dispatch path (the
`ProgressMsgTypeAgentCompleted` branch and the `socketOutcome==nil && exit=0 &&
!watcherFailed` heuristic branch) omit merging the agent's commits onto `main`
before closing the bead. This section fills that gap.

#### EM-052 — Merge run-branch to main on success

Tags: mechanism

On both success branches (§4.3 Step 9 branch 1 — `agent_completed`; §4.3 Step 9
branch 2 — `exit=0` heuristic), the daemon MUST execute the following ordered
steps BEFORE calling `CloseBead`:

1. **Resolve run-branch tip.** Resolve the current tip SHA of the run-branch
   `run/<run_id>` via `git rev-parse refs/heads/run/<run_id>` in the project
   repository root. If the run-branch has no commits beyond the HEAD that was
   current at dispatch time, the daemon MUST treat the run as no-change and
   proceed to step 6 (skip merge, still close bead).

2. **Rebase run-branch onto main.** If the worktree directory for the run still
   exists on disk, the daemon MUST execute `git rebase main` from within the
   worktree directory immediately before the fast-forward check. This allows
   concurrent runs to succeed even when `main` has advanced since the worktree
   was cut (hk-j1aq5). If the rebase exits non-zero, the daemon MUST run
   `git rebase --abort` in the worktree directory, then proceed to the
   `rebase_conflict` reopen path (EM-053). After a successful rebase, the
   daemon MUST re-resolve the run-branch tip and the `main` tip before
   continuing (both may have changed). If the worktree directory does not exist,
   this step is skipped.

3. **Fast-forward check.** Resolve the current tip SHA of `main` via
   `git rev-parse refs/heads/main`. If `main` is an ancestor of the run-branch
   tip (i.e., the merge is a fast-forward), proceed to step 4. If `main` is NOT
   an ancestor (non-FF — a concurrent push occurred after the rebase), the
   daemon MUST NOT merge; instead it MUST invoke `ReopenBead` with reason
   `non_ff_merge: main advanced concurrently` and emit
   `outcome_emitted{kind=rejected, reason=non_ff_merge}` (per EM-053 below),
   then `run_failed`, and return without closing the bead.

4. **Fast-forward main.** Update `refs/heads/main` to the run-branch tip via
   `git update-ref refs/heads/main <run_branch_tip_sha>` in the project
   repository root.

4a. **Post-merge build gate.** Run `go build ./...` and `go vet ./...` against
    the merged tree (hk-o68j3/hk-ycp62). The build runs in the run-branch
    worktree directory when it still exists on disk — after step 2's rebase, the
    worktree reflects the combined `main + agent` content, so cross-bead compile
    errors (e.g. two parallel beads each declaring a package-level helper with
    the same name) are caught before the push makes them visible to other agents.
    If the worktree directory no longer exists, the build runs in the project
    root (best-effort). This step is a no-op when no `go.mod` file is present in
    the build directory so non-Go projects and bare-repo test fixtures are
    unaffected. On any `go build` or `go vet` failure:
    - The daemon MUST roll back the `update-ref` from step 4 (restore
      `refs/heads/main` to its pre-step-4 value).
    - The daemon MUST emit `merge_build_failed` via the event bus carrying
      `run_id`, `bead_id`, and the error output.
    - The daemon MUST proceed to the EM-053 reopen path with reason
      `merge_build_failed (go <subcommand>): <error>`.

5. **Push origin main.** Execute `git push origin main` from the project
   repository root (per push-autonomy directive). A push failure MUST NOT leave
   the bead closed; if push fails, the daemon MUST reopen the bead with reason
   `push_failed: <error>` and emit `outcome_emitted{kind=rejected,
   reason=push_failed}`, then `run_failed`.

6. **Emit outcome and close.** Emit `outcome_emitted{kind=approved}` via the
   event bus. Then call `CloseBead`. On `CloseBead` error, emit `run_failed`
   (not `run_completed`) as for the existing close-error path.

7. **Emit bead_closed.** After `CloseBead` succeeds, emit `bead_closed` via the
   event bus, carrying `run_id` and `bead_id`.

8. **Emit run_completed.** Emit `run_completed{success:true}` as the final event.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
Refs: hk-ftyvo, hk-j1aq5, hk-o68j3, hk-ycp62

#### EM-053 — Non-FF, rebase-conflict, push-failure, and build-gate reopen path

When step 2 of §4.12.EM-052 detects a rebase conflict, OR when step 3 detects a
non-fast-forward condition, OR when step 4a detects a build/vet failure on the
merged tree, OR when step 5 detects a push failure, the daemon MUST:

1. Emit `outcome_emitted{kind=rejected, reason=<"rebase_conflict"|"non_ff_merge"|"merge_build_failed"|"push_failed">}`
   via the event bus.
2. Call `ReopenBead` with a descriptive reason string.
3. Emit `run_failed` (not `run_completed`).
4. Return without calling `CloseBead` or emitting `bead_closed`.

The run-branch tip commit is preserved intact; the operator can inspect the work
and retry or merge manually.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
Refs: hk-ftyvo, hk-j1aq5

#### EM-054 — Working-tree refresh after successful merge-to-main

Tags: mechanism

After step 4 (`git update-ref`) and step 5 (`git push`) of §4.12.EM-052 both
succeed, the daemon MUST refresh the project working tree to match the new HEAD
using a two-sub-step sequence:

**Step 6a — staged-index restore (best-effort):**

```
git restore --staged .
```

run from the project repository root. This clears the index to match HEAD without
touching the working tree. After `git update-ref` advances HEAD, the index is
still at the pre-merge state: files added or modified by the merged commit appear
as "staged deletions" in the index relative to the new HEAD. If `git reset --hard
HEAD` (step 6b) subsequently fails (non-fatal per the refresh-failure policy
below), those phantom staged changes would persist into the next bead's run and
trigger false `implementer_escaped_worktree` positives in
`checkMainWorkingTreeDirty`. Running `git restore --staged .` first — which is
lighter (index-only, no working-tree I/O) and less likely to fail under lock
contention — ensures the staged index is clean even on a step-6b failure.

Step 6a is best-effort: if `git restore --staged .` fails, log a warning to
stderr and continue to step 6b.

**Step 6b — working-tree reset:**

```
git reset --hard HEAD
```

run from the project repository root. `git reset --hard HEAD` re-syncs both the
index and the working tree to the new HEAD in a single atomic operation, correctly
handles deletions (files removed by the agent's commits will be removed from
disk), and its semantics are stable across git versions available in CI
environments.

**Uncommitted-changes policy.** If the project working tree has uncommitted
changes at the time of the refresh (i.e., `git status --porcelain` is non-empty
before the reset), that is an abnormal state — the operator is expected to keep
the project working tree clean while the daemon is running. The daemon MUST:

1. Log a warning to stderr describing the uncommitted changes.
2. Still execute `git reset --hard HEAD`. The daemon owns the working tree during
   operation; preserving uncommitted operator changes is not a daemon
   responsibility.

**Refresh failure.** If `git reset --hard HEAD` fails (e.g., I/O error, git
lock contention), the merge itself has already succeeded and is durable — the
ref and remote are already updated. The daemon MUST NOT reopen the bead on
refresh failure. Instead it MUST:

1. Log a warning to stderr with the error detail.
2. Emit a `working_tree_refresh_failed` event on the bus carrying `run_id`,
   `bead_id`, and the error message.
3. Continue to the CloseBead step normally.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
Refs: hk-4goy3, hk-my7y8

## 4.13 Eager refill obligation

This section formalizes the daemon's duty to keep the active stream group's in-flight count as close to `max_concurrent` as possible from the existing ready queue, without crossing the ceiling and without auto-dispatching beads the orchestrator just created. The refill path is **pure code** (mechanism-tagged, no LLM). LLM intervention is outside this section's scope (it belongs to the orchestrator-level replenishment surface; see `docs/orchestration-protocol-v2.md`). The daemon's role here: when a slot opens, fill it from the ready queue; nothing more.

#### EM-062 — Eager-refill trigger and compute

On every `run_terminal` event (after `finalize_run` per §7.4 completes) AND on every dispatch-loop poll tick, the daemon MUST evaluate:

```
FUNCTION eager_refill_eval():
    IF active_queue() IS None: RETURN
    IF active_queue().status NOT IN {active}: RETURN
    group = active_queue().active_group()
    IF group IS None OR group.kind != "stream": RETURN
    available = max_concurrent - in_flight_count()       # §4.11 EM-049
    IF available <= 0: RETURN                            # WIP cap full; hard stop
    pending_in_group = COUNT(item FOR item IN group.items WHERE item.status == "pending")
    deficit = available - pending_in_group
    IF deficit <= 0: RETURN                              # existing pending will fill; no action
    candidates = pre_screen(kerf_next(limit = deficit * OVERFETCH_FACTOR))   # EM-063
    eligible = candidates[: min(deficit, len(candidates))]
    IF eligible IS NOT EMPTY:
        queue_append(active_stream_group=group, bead_ids=eligible)   # [queue-model.md §7 QM-040]
```

`OVERFETCH_FACTOR` SHOULD be 2 at v1 (pre-screen rejections don't leave an avoidable gap). Compile-time constant; not operator-tunable at v1.
Refill MUST fire AFTER all terminal-event processing for the tick (merge, reviewer-launch, `CloseBead`, group-advance evaluation per §4.3 EM-015f) completes for the current run. **Finishing in-flight work takes priority over pulling new work.**
Tags: mechanism

#### EM-063 — Pre-screen and provenance guard

`pre_screen` MUST apply a two-phase filter:

**Phase 1 — already-in-queue (queue.json authority, fastest).** For each candidate `bead_id`, inspect the active `queue.json` envelope in-memory. If present with `status ∈ {pending, dispatched, completed, failed}` → SKIP.

**Phase 2 — already-landed (git authority).** For candidates not eliminated by Phase 1, execute `git log --all --grep "Refs: <bead_id>" --oneline` against **`origin/main` (NOT local `main`)**. If ≥1 commit found → SKIP and log a `stale_open_bead_detected` informative event.

> INFORMATIVE: `origin/main` not local `main` avoids false positives during the two-phase terminal sequence (EM-052/EM-053): a bead whose push succeeded but whose local `main` has not yet been fast-forwarded would appear "not landed" on a local-only check, triggering a spurious re-dispatch.

**Provenance guard.** Refill MUST NOT dispatch a bead created by the daemon or orchestrator in the same workloop tick. Newly-created beads land `open` (not yet `ready`); `kerf next` returns only `ready` beads. So the readiness gate of `kerf next` is the normative enforcement; spec does not require a per-tick creation log.

**Result.** Ordered list of survivors in `kerf next` priority order. Ordering preserved into `queue_append`.

Tags: mechanism

## 4.14 Check-observed-before-submit guard

The orchestrator's idempotency obligation BEFORE `harmonik run --beads X` (i.e. before submitting a queue per [process-lifecycle.md §4.4 PL-003a]). Orchestrator-facing contract; the pre-screen of §4.13.EM-063 is the daemon-side refill contract. Both enforce the same invariant (never dispatch an already-claimed or already-landed bead) at their respective entry points.

#### EM-064 — Read-order and authority chain

Before submitting or appending any bead X to the execution queue, the orchestrator MUST walk this chain in order; skip X if any tier signals "already observed":

| Tier | Source | Check | Skip if |
|---|---|---|---|
| 1 | `queue.json` in-memory (or `queue-status`) | X present, `status ∈ {pending, dispatched, completed, failed}` | yes |
| 2 | `origin/main` git log | `git log origin/main --grep "Refs: X" --oneline` ≥1 commit | yes; optionally `br close X` if still open |
| 3 | Beads ledger | `br show X` `status ∈ {in_progress, closed}` | yes; daemon atomic claim enforces final barrier |
| 4 | `events.jsonl` | `run_started` for X with no subsequent terminal event | yes (in-flight) |

Tier 1 is fastest (in-memory queue scan); MUST be first. Tier 2 uses `origin/main` NOT local `main` (rationale per EM-063). Tiers 3+4 are supplementary; a conforming impl covering 1+2 satisfies the guard at v1 correctness. Tiers 3+4 SHOULD be checked in long-running orchestrator sessions to catch cross-session drift. The read order is normative; MUST NOT reverse (queue.json is the fastest and most current in-memory mirror).
Tags: mechanism

#### EM-065 — Submit/append targets the active stream group; no double-queue

Orchestrator MUST submit beads to the single active queue per [queue-model.md §3 QM-002]. If a queue is already active, the orchestrator MUST use `queue-append` against the active stream group (per [queue-model.md §7 QM-040]) rather than `queue-submit` (which would replace). A new submit while a queue is active is a QM-003 violation; the daemon will reject. EM-064 tier 1 catches the common case where the orchestrator's stale view would re-submit an in-flight bead.

> INFORMATIVE: The daemon's Beads atomic claim (`ClaimBead` per [beads-integration.md §4.3 BI-009]) is the final barrier at the execution layer. EM-064 is an orchestrator-layer pre-flight, complementary not replacement.

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
- `QueueID`, `QueueGroupIndex` — defined in [queue-model.md §4 QM-010..012].

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
    goal               : String | None     -- optional graph-level objective per [workflow-graph.md §4 WG-044]; threaded into agentic-node briefs via the run-level ExtraContext channel (§7.5); MAY contain template-param placeholders substituted at launch (§7.5, [workflow-graph.md §4 WG-045])
```

```
RECORD Node:
    node_id             : NodeID          -- unique within the workflow; §4.8.EM-034a namespacing applies under expansion
    type                : NodeType        -- one of {agentic, non-agentic, gate, sub-workflow}; see §4.2.EM-006 (control-point removed per C1 bundle)
    handler_ref         : String | None   -- required when type ∈ {agentic, non-agentic, gate}; forbidden on sub-workflow; see §4.2.EM-007 (EM-060 amendment)
    agent_type          : String | None   -- agent-type identifier per [architecture.md §6.1 AR-025]; required when type = agentic; DOT routing hint per [handler-contract.md §4.2]; e.g., "claude-code", "pi"
    timeout             : Integer | None  -- positive seconds; when node is a tool node (carries tool_command), also serves as the wall-clock kill bound (default 300); see [workflow-graph.md §4 WG-039]
    required_skills     : List<String>    -- resolved per [control-points.md §4.11]
    policy_ref          : String | None   -- see [control-points.md §6.3]
    gate_ref            : String | None
    freedom_profile_ref : String | None
    budget_ref          : String | None
    idempotency_class   : IdempotencyClass -- one of {idempotent, non-idempotent, recoverable-non-idempotent}
    axes                : AxisTags        -- four-axis classification per [architecture.md §4.1]
    mode_tag            : ModeTag         -- one of {mechanism, cognition}
    sub_workflow_ref    : String | None   -- required when type = sub-workflow
    model               : String | None   -- optional per-node model override; shape-validated per EM-012b-NODE; valid only when type = agentic; overrides the run-level ModelPreference.model for this node's dispatch; see [workflow-graph.md §4 WG-042]
    effort              : EffortLevel | None -- optional per-node effort override; closed enum (§6.1 EffortLevel); valid only when type = agentic; overrides the run-level ModelPreference.effort for this node's dispatch; see [workflow-graph.md §4 WG-042]
    tool_command        : String | None   -- optional shell command for a non-agentic tool node; valid only when type = non-agentic; dispatched by the built-in shell handler [handler-contract.md §4.1 HC-063]; reuses Node.timeout as kill bound; see [workflow-graph.md §4 WG-039]
    prompt              : String | None   -- optional inline brief; replaces bead-derived body for implementer-class agentic nodes; reviewer-class inert at v1; see [workflow-graph.md §4 WG-040]
    non_committing      : Boolean         -- default false; when true on an implementer-class agentic node, a clean exit yields SUCCESS without requiring HEAD-advance; see [workflow-graph.md §4 WG-041]
```

```
ENUM NodeType:
    agentic
    non-agentic
    gate
    sub-workflow
```

> NOTE: The `control-point` value present in pre-C1 versions of this enum is removed. It is collapsed under `gate` and `non-agentic` per the C1 amendment to [workflow-graph.md §4 WG-001/WG-002] and the C4 amendment to [control-points.md §4.12 CP-053/CP-054]. This is a §6.4 breaking change; the schema-version bump is recorded in §12 (v0.6.0, C2 bundle). (CI-8)

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
    workflow_mode      : WorkflowMode         -- one of {single, review-loop, dot}; resolved at claim per §4.3.EM-012a; immutable for run lifetime; defaults to `single`
    model_preference   : ModelPreference      -- (model, effort) pair sealed at claim per §4.3.EM-012b; passed to every handler via LaunchSpec per [handler-contract.md §4.10 HC-055a]
    input              : WorkspaceRef         -- workspace reference per [workspace-model.md §4.1]
    bead_id            : BeadID | None        -- present when tied to a bead (see [beads-integration.md §4.3 BI-008])
    queue_id           : QueueID | None       -- present when the run originated from a queued dispatch per [queue-model.md §4 QM-010..012]; absent for non-queued dispatch
    queue_group_index  : QueueGroupIndex | None  -- the active-group index at dispatch time per [queue-model.md §4 QM-010..012]; present iff queue_id is present
    state              : State                -- current state
    context            : Map<String, Any>     -- shared context; updated per §4.10.EM-041a; reserved keys under workflow_mode=review-loop per §4.3.EM-012: iteration_count, last_verdict, claude_session_id, last_diff_hash
    start_time         : Timestamp            -- RFC 3339 wall clock
    end_time           : Timestamp | None     -- set on terminal transition
    template_params    : Map<String, String> | None  -- per-run template-param map supplied at launch (--param KEY=VALUE per §7.5); sealed at claim time, applied exactly once to the raw .dot source before parse per [workflow-graph.md §4 WG-045]; None when no params supplied; sealing makes a replay re-substitute identically
```

```
ENUM WorkflowMode:
    single                      -- one-handler-per-node default (Core MVH); applies to ordinary workflow graphs
    review-loop                 -- hardcoded two-node implementer→reviewer cycle per §4.3.EM-015d; cap-3 per §4.3.EM-015e
    dot                         -- general workflow-graph walker; reserved for post-MVH; out of scope for Core MVH conformance
```

```
ENUM ReviewLoopCompletionReason:
    approved                    -- cycle ended with APPROVE verdict; run terminates SUCCESS per §4.3.EM-015e
    cap_hit                     -- REQUEST_CHANGES at iteration_count = 3; run terminates needs-attention per §4.3.EM-015e
    blocked                     -- BLOCK verdict at any iteration; run terminates needs-attention per §4.3.EM-015e
    no_progress                 -- bit-identical diff hash detected before reviewer launch per §4.3.EM-015e
    error                       -- daemon-side failure during cycle (e.g., verdict file malformed or missing per §4.3.EM-015e); run terminates needs-attention; per [event-model.md §8.1a.3]
```

```
RECORD ModelPreference:
    model  : String | None   -- opaque to harmonik below the descriptor layer; shape-validated (non-empty when present,
                             --   matches ^[A-Za-z0-9._:/-]+$, max 128 chars); value-validated by the handler at launch;
                             --   None when resolution chain produced empty (tier 4 built-in fallback)
    effort : EffortLevel | None  -- closed enum (see below); None when resolution chain produced empty
```

```
ENUM EffortLevel:
    low
    medium
    high
    xhigh
    max
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
    failure_class       : FailureClass | None  -- present only when status = FAIL; §8 enum; handler-emitted HINT, daemon-back-filled from HC-020 sentinel when handler omits; engine-side MUST be populated on FAIL; §4.1.EM-005c
    kind                : OutcomeKind         -- discriminator per §4.1.EM-005a; defaults to `default`
    payload             : VerdictPayload | None -- kind-discriminated extension envelope per §4.1.EM-005a; absent when kind=default; when kind=reconciliation_verdict, MUST be a VerdictEvent per [reconciliation/schemas.md §6.1]; when kind=gate_decision, MUST be a GateDecisionPayload per [control-points.md §6.1.8 CP-058]
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
    gate_decision               -- gate evaluator structured rationale; payload MUST be a GateDecisionPayload per [control-points.md §6.1.8 CP-058] (EM-005b)
```

```
ENUM FailureClass:
    transient | structural | deterministic | canceled | budget_exhausted | compilation_loop
    -- declared in §8; surfaced here as Outcome's optional carrier per §4.1.EM-005c
```

> INFORMATIVE: The `VerdictPayload` type alias is the discriminated-union payload shape. It resolves to a union: `VerdictEvent` (for `kind=reconciliation_verdict`, per [reconciliation/schemas.md §6.1]) and `GateDecisionPayload` (for `kind=gate_decision`, per [control-points.md §6.1.8 CP-058]). Future `OutcomeKind` values introduced via the amendment protocol per [architecture.md §4.6] add their own variant; the `VerdictPayload` name is retained as the umbrella alias to keep the schema slot stable. EM does NOT redeclare `VerdictEvent` or `GateDecisionPayload` fields — it cites the RC-owned and CP-owned records by name. The v0.3.3 wire-protocol stability commitment for `OQ-RC-010` is delivered by this slot.

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

**v0.3.3 → v0.3.4 — `Outcome.failure_class` additive + `gate_decision` kind value.** EM-005c adds the optional `failure_class` field to the `Outcome` record; EM-005b adds `gate_decision` to the `OutcomeKind` enum. Both changes are coordinated at v0.3.4. v0.3.3 readers treat `failure_class` as unknown-and-optional and fall through to sentinel-based classification per [handler-contract.md §4.5 HC-020]; v0.3.4 readers consult the field as a hint and override with the daemon's classification on disagreement. v0.3.3 readers encountering `kind = gate_decision` MUST route to reconciliation Cat 6a per [reconciliation/spec.md §8.11], NOT silently degrade to `default`. The bump is additive — no existing field is renamed or removed — and N-1 readability per §6.4 is preserved on both directions.

**attractor-parity — `Node` new optional fields (additive).** `Node.model`, `Node.effort`, `Node.tool_command`, `Node.prompt`, and `Node.non_committing` are new optional `Node` fields (additive, non-breaking, N-1 readable per §6.4 and [workflow-graph.md §11 WG-034]); `Node.timeout` is a reused slot whose comment is extended to cover tool-node kill-bound semantics. A reader at the prior schema treats the new fields as unknown-and-absent; the `timeout` field is already present.

**attractor-parity — `Workflow.goal` and `Run.template_params` (additive).** `Workflow.goal` and `Run.template_params` are new optional fields (additive, non-breaking, N-1 readable); a `goal=""` graph runs bead-driven and a run with no `template_params` performs a no-op substitution pass.

### 6.5 Co-owned event payloads

This spec's requirements drive emission of the following events whose names and payload schemas are declared in [event-model.md §8]:

- Run lifecycle — `run_started` (on dispatch against a bead or standalone input), `run_completed` (on success terminal state), `run_failed` (on failure terminal state; payload includes the failure class per §8). All three carry the resolved `workflow_mode` (per §4.3.EM-012a) on their payloads per [event-model.md §8.1]. When the run originated from a queued dispatch per [queue-model.md §4 QM-010..012], all three additionally carry the optional `queue_id` and `queue_group_index` fields per §4.3.EM-015a, §4.3.EM-015b and [event-model.md §8.10].
- Review-loop cycle (only when `workflow_mode = review-loop`) — `implementer_resumed` (on every implementer-launch after the first), `reviewer_launched` (on every reviewer-launch), `reviewer_verdict` (after `.harmonik/review.json` is read and validated; carries the agent-reviewer JSON schema v1 fields verbatim), `iteration_cap_hit` (on the cap-hit close path per §4.3.EM-015e), `no_progress_detected` (on the no-progress early-exit per §4.3.EM-015e), `review_loop_cycle_complete` (exactly once per cycle, before the terminal `run_completed` / `run_failed`).
- Queue lifecycle (only when an active queue is loaded per §7.4) — `queue_submitted` (on queue load), `queue_group_started` (on group activation per §4.3.EM-015f), `queue_group_completed` (on group terminal per §4.3.EM-015f; payload's `final_status` is `complete-success` or `complete-with-failures`), `queue_paused` (on `complete-with-failures` per §4.3.EM-015f), `queue_appended` (on append per [queue-model.md §7]), `queue_item_deferred_for_ledger_dep` (on ledger-blocked items per §7.4). Event names and payload field lists are normative in [event-model.md §8.10].
- State lifecycle — a `state_entered` event (on entry to a new state) and a `state_exited` event (on exit from a state, prior to transition selection).
- Transition projection — a transition event (projection of the `Transition` record per §4.6.EM-028).
- Checkpoint lifecycle — a checkpoint-written event emitted after every checkpoint commit lands; payload includes `run_id`, `state_id`, `transition_id`, optional `bead_id`.
- Sub-workflow lifecycle — a `sub_workflow_entered` event on expansion entry and a `sub_workflow_exited` event on expansion exit, per §4.8.EM-036.

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

> INFORMATIVE: Pause and graceful-stop operator controls operate BETWEEN runs per [operator-nfr.md §4.3] and do not appear as run-state transitions. Upstream events (`operator_stopped`, `budget_exhausted`) are emitted by their owning specs; execution-model emits `run_failed` with the classifying `class` field. The queue-group state machine ([queue-model.md §5]) is layered above this per-run machine: a queue item's `dispatched` corresponds to its run reaching `running`; the item's terminal (`completed`/`failed`) corresponds to the run's terminal event per §4.3.EM-015b. The two state machines do not share transitions.

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

    -- Steady-state loop. Dispatch input is the active queue per [queue-model.md §3 QM-002].
    WHILE NOT shutdown_requested():
        IF should_pause_between_runs():             -- [operator-nfr.md §4.3 ON-008] PRIMARY operator-pause gate (covers ALL paths incl. fallback)
            wait_for_resume(); CONTINUE             --   when operator-control IN {pausing,paused} this CONTINUEs before the fallback branch (§4.11.EM-067)
        queue = active_queue()                      -- [queue-model.md §3 QM-002] in-memory authority loaded from .harmonik/queue.json
        IF queue IS None:
            IF no_auto_pull():                      -- §4.11.EM-066 sealed startup flag (default ON; use --auto-pull to opt in to br-ready)
                idle_wait_for_queue_submission(); CONTINUE   -- quiet daemon: poll submission socket; MUST NOT fall back to br ready
            -- historical single-daemon topology (flag unset): br-ready fallback. Defense-in-depth re-assert per §4.11.EM-067
            -- (reachable under operator pause ONLY if should_pause_between_runs() was scoped narrower than ON-008; vacuous otherwise):
            IF operator_pause_state() IN {pausing, paused}:  -- [operator-nfr.md §4.3 ON-011] single source of pause truth (ON-056/ON-057)
                idle_wait(); CONTINUE                         -- §4.11.EM-067: fallback MUST NOT dispatch while paused (belt-and-suspenders)
            ready = br_ready()                      -- [beads-integration.md §4.5] opt-in fallback dispatch input
            IF ready IS EMPTY:
                idle_wait(); CONTINUE
            bead_record = ready[0]; dispatch_fallback(bead_record); CONTINUE
        IF Queue.status IN {paused-by-failure, paused-by-drain, completed}:  -- [queue-model.md §8] (RECORD field access)
            idle_wait(); CONTINUE                                              -- no dispatch while paused
        group = queue.active_group()                -- [queue-model.md §5] head-of-active-group
        IF group IS None:
            idle_wait(); CONTINUE
        IF in_flight_count() >= max_concurrent:     -- §4.11.EM-049 capacity gate
            wait_for_run_slot(); CONTINUE
        item = group.next_dispatchable()            -- [queue-model.md §5] head-of-stream OR
                                                    --   any-pending wave member with no open ledger blocker
        IF item IS None:
            wait_for_group_progress(); CONTINUE     -- all items dispatched-or-deferred; group still advancing
        IF ledger_blocks_open(item.bead_id):        -- [beads-integration.md §4.3 BI-005]
            mark_item(item, deferred-for-ledger-dep)
            emit_event(queue_item_deferred_for_ledger_dep, queue, group, item)  -- [event-model.md §8.10]
            CONTINUE
        workflow = resolve_workflow(item.bead_id)
        IF NOT validator.validate(workflow):        -- §4.9.EM-038
            queue.mark_item_failed(item, validation_failed); CONTINUE
        acquire_claim_token()                       -- §4.11.EM-050 claim-write serialization
        run = create_run(item.bead_id, workflow,
                         queue_id=queue.queue_id,
                         queue_group_index=group.group_index)  -- §6.1 Run record
        persist_run_id(run)                         -- Beads atomic-claim per [beads-integration.md §4.3 BI-009]
        release_claim_token()
        emit_event(run_started, run)                -- §4.3.EM-015a; payload carries queue_id + queue_group_index
        spawn_async execute_workflow(run) THEN finalize_run(run)  -- §4.11.EM-049 fan-out

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
        emit_event(run_completed, run)                          -- §4.3.EM-015b; payload carries queue_id + queue_group_index when present
        write_terminal_bead_transition(run)                     -- [beads-integration.md §4.4 BI-010]
    ELSE IF run.state == terminal_failure:
        emit_event(run_failed, run, class=run.failure_class, last_checkpoint=run.last_checkpoint_sha)  -- §4.5.EM-025
        write_terminal_bead_transition(run)                     -- [beads-integration.md §4.4 BI-010]
    -- §4.3.EM-015f: after terminal emission, queue-group-advance evaluation fires if the run is queued.
```

The pseudocode's terminal-detection condition (`is_terminal(node, terminal_node_ids)`) is the normative implementation of §7.1's `running → completed` guard; the normative requirements for the lifecycle emissions this loop makes are §4.3.EM-015a (run_started), §4.3.EM-015b (run_completed / run_failed), §4.3.EM-015c (terminal detection), and §4.3.EM-015f (queue-group advance). The inner loop's `RETRY`, `is_durable`, `FAIL`, `STAY`, `ESCALATE` branches each correspond to explicit normative requirements; every state-advance step lands on §4.5.EM-023a's decision table or §4.10.EM-041's cascade. The queue-pull and slot-acquire branches each correspond to [queue-model.md §5] group-advance rules and §4.11 concurrency requirements respectively.

## 7.5 — Workflow Mode: `dot` (BINDING DOCUMENT)

This subsection binds `workflow_mode = dot` (per §4.3.EM-012 and the resolution walk of §4.3.EM-012a) to the dispatch loop already published in §7.4 and the deterministic cascade already published in §7.3. §7.5 **does not introduce a new state machine, a new dispatch loop, or a new cascade**. Every dispatch primitive a `dot` run consumes — edge-selection cascade (§4.10.EM-041), context-update ordering (§4.10.EM-041a), guards and gates (§4.10.EM-042 / §4.10.EM-042a), per-edge traversal caps (§4.10.EM-043 / §4.10.EM-043a), no-match-edge → `structural` (§4.10.EM-046a), RETRY re-dispatch (§4.10.EM-046b), sub-workflow expansion (§4.8.EM-034 family), durability decision (§4.5.EM-023a), pre-run validation (§4.9.EM-038), and schema evolution (§6.4) — is consumed unchanged. The five sub-parts that follow are exactly: the input contract (§7.5.1), the dispatch-equivalence statement (§7.5.2), the validator obligations specific to `dot` (§7.5.3), the per-node-type dispatch table (§7.5.4), and the conformance lift plus the post-MVH parallel-fan-out reservation (§7.5.5).

The general workflow-graph vocabulary this subsection consumes — DOT artifact schema, node-type catalog, edge-condition LHS whitelist, edge-condition dialect, outcome-carrier routing, cascade invariant, per-workflow context-keys — is owned by [workflow-graph.md] (per D-attractor-adoption, D-edge-cascade-invariant, D-verdict-surfacing, D1, D3, D4, D5). §7.5 references those by section and WG-NNN ID; it does not re-declare them.

### 7.5.1 — Workflow Input Contract

#### EM-055 — `dot` workflow input contract: ingest, validate, return §6.1 record

For a run whose `workflow_mode = dot` (resolved per §4.3.EM-012a), the daemon's claim path MUST resolve the run's workflow artifact through the following ordered ingestion pipeline, executing entirely before §7.4 `execute_workflow` begins for the run:

1. **Locate the `.dot` artifact.** The artifact path is derived from the workflow-mode resolution chain per §4.3.EM-012a tier 3 (per-daemon configuration) and tier 4 (built-in fallback, where the fallback maps `dot` mode to a canonical default `.dot` artifact registered with the daemon). Per-bead override of the artifact path via a bead-schema extension is deferred to a future BI-NNN requirement in [beads-integration.md]; at v1 the path is sourced from per-daemon configuration only.
1a. **Accept per-run param map.** The `harmonik run` CLI (and the queue-item launch context) accepts repeated `--param KEY=VALUE` flags; the resulting `map[string]string` is the run's `template_params` (§6.1), sealed at claim time alongside `workflow_ref`. A run with no `--param` flags has `template_params = None` and the substitution pass of step 2a is a no-op.
2. **Read raw source text.** The daemon MUST read the `.dot` artifact as raw text before parsing.
2a. **Apply WG-045 substitution pass.** The daemon MUST apply the template-param substitution pass of [workflow-graph.md §4 WG-045] to the raw source text, replacing each `__KEY__` placeholder with the corresponding value from the sealed `template_params`. After substitution, any residual `__UPPER_SNAKE__` token is a launch-time error: the daemon MUST refuse to start the run and report the offending token(s) per [workflow-graph.md §4 WG-045]. A run with `template_params = None` skips substitution (no-op). This step MUST precede step 3 (the WG-046 ordering invariant: read source → substitute → parse → validate → dispatch).
3. **Parse to AST.** The daemon MUST parse the substituted source text to an in-memory graph AST conforming to the DOT subset defined by [workflow-graph.md §10 WG-031, §11 WG-033]. The parser library is an implementation detail; the parsed AST's surface MUST be sufficient to produce a §6.1 `Workflow` record without consulting any other store.
4. **Convert AST to §6.1 `Workflow` record.** Conversion is mechanical: graph attributes (e.g., `workflow_id`, `version`, `schema_version`, `start_node`, `terminal_nodes`, `goal`) populate the §6.1 `Workflow` header; node attributes populate §6.1 `Node` fields per [workflow-graph.md §4 WG-001/WG-002]; edge attributes populate §6.1 `Edge` fields per [workflow-graph.md §6 WG-013]. The conversion produces the same §6.1 record shape that §4.10 (cascade), §4.8 (sub-workflow), and §4.9 (validation) already consume for `single` and `review-loop` runs.
5. **Run pre-run validator.** The daemon MUST then invoke the §4.9.EM-038 pre-run validator on the produced `Workflow` record, augmented with the `dot`-specific obligations of §7.5.3 below.
6. **Surface `goal` into node briefs.** When the `Workflow.goal` field is non-empty, the daemon MUST surface it into every `agentic` node's task brief via the **run-level ExtraContext channel** (`AgentTaskPayload.ExtraContext`, rendered as the `## Extra Context` section of the `agent-task.md` artifact per [claude-hook-bridge.md §4 CHB-028]) as the run's stated objective. `goal` is constant across every node in the run and composes with — does NOT replace — any per-node `prompt` attribute (§4 WG-040) and the bead-derived body (see B↔E composition contract below).
7. **Return the `Workflow` record.** Only after steps 1–6 succeed MAY §7.4 `execute_workflow` begin. A failure at any step routes the run via §7.4's `queue.mark_item_failed(item, validation_failed)` path; no `run_started` event per §4.3.EM-015a is emitted.

**B↔E brief-composition contract (normative).** Both the graph-level `goal` (component E, run-level) and the node-level `prompt` (component B) write the `agentic`-node brief through the [claude-hook-bridge.md CHB-028] `agent-task.md` path. They MUST **compose, not double-inject**:

- **`goal` (E, run-level)** is threaded via the **run-level ExtraContext channel** (`AgentTaskPayload.ExtraContext`, rendered as the `## Extra Context` section) as the run's stated objective. It is constant across every node in the run.
- **`prompt` (B, node-level)** replaces the node-level task body (`AgentTaskPayload.Body`, rendered as the `## Task Description` section) for the node that carries it.
- **bead-derived body** remains the node brief (the `## Task Description` Body) when no per-node `prompt` is present.

`goal` and `prompt` occupy **distinct channels** (ExtraContext vs taskBody) and therefore do not collide; a node carrying both `goal` (run-level) and `prompt` (node-level) receives each exactly once. **Assembly order:** the `agent-task.md` renders `## Task Description` (the node body — `prompt` if present, else bead body) BEFORE `## Extra Context` (the run-level `goal`) — confirmed against `internal/workspace/agenttask_chb028.go` (`buildAgentTaskContent`: `Body` precedes `ExtraContext`).

**Resume semantics.** On daemon restart, runs whose `workflow_mode = dot` MUST re-execute steps 1–4 against the same `.dot` artifact. The daemon MUST NOT trust a serialized prior parse tree; reparse is cheap and removes a serialization surface. The §6.1 `Workflow.workflow_id` and `workflow_version` produced by the post-restart reparse MUST be identical to the pre-restart values sealed on the Run record per §6.1. A mismatch (artifact mutated under-foot between claim time and restart) MUST route to reconciliation per [reconciliation/spec.md §8.4 Cat 3] (artifact-state divergence); the daemon MUST NOT silently proceed with the post-restart parse.

**Concurrency-readiness.** The parser and the produced `Workflow` record MUST be `run_id`-keyed via the run claim; no daemon-global parser state, no shared parse cache across runs. This matches the §4.11 concurrency posture and the EM-051 `max_concurrent` ceiling: each in-flight `dot` run holds its own `Workflow` record.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 7.5.2 — Dispatch Equivalence with §7.4

#### EM-056 — Once the `Workflow` record is loaded, §7.4 and §7.3 apply unchanged

Once §7.5.1.EM-055 returns the §6.1 `Workflow` record for a `dot` run, §7.4 `execute_workflow` and §7.3 `select_next_edge` apply unchanged. The cascade evaluates edge `condition` expressions per the dialect declared in [workflow-graph.md §6 WG-013] against the LHS whitelist declared in [workflow-graph.md §6 WG-014]; routes via `preferred_label` per [workflow-graph.md §7 WG-019]; enforces the unconditional-edge fallback invariant per [workflow-graph.md §5 WG-011]; and applies per-edge `traversal_cap` per §4.10.EM-043. No new dispatch-loop state and no new cascade branching is introduced for `dot`.

Four clarifying clauses, each a restatement of an existing requirement (not a new normative obligation):

1. **Condition-evaluation site.** Condition evaluation runs **inside §7.3 `select_next_edge` (daemon-side)**, per the existing §7.3 pseudocode line `evaluate_condition(...)`. Handlers do not evaluate edge conditions; the cascade does. This is the same evaluation site `single` and `review-loop` runs use; `dot` introduces no handler-side condition obligation. (Restatement only; no new requirement.)
2. **Failure-class routing.** When step (a) of the §4.10.EM-041 cascade evaluates a condition referencing `outcome.failure_class` (admitted as a LHS identifier per D1 and the C1 whitelist), the value read is the `Outcome.failure_class` field per §4.1.EM-005 (as amended by [handler-contract.md §4.2a HC-058] to add the field). On a FAIL outcome where the handler did not set `failure_class`, the daemon's classifier per §8.1 reclassification rules MUST back-fill the field BEFORE §7.3 `select_next_edge` runs. The cascade reads only post-classifier `Outcome` values.
3. **Sub-workflow inside `dot`.** §4.8.EM-034 / EM-034a / EM-034b / EM-034c apply unchanged. A `dot` workflow MAY contain `sub-workflow`-type nodes referencing other `.dot` artifacts; expansion is namespaced per EM-034a, the reference graph is acyclic per EM-034b, and the expansion pin is durable on the entry checkpoint per EM-034c. **A `dot` workflow MUST NOT reference a `review-loop` sub-workflow** — the review-loop cycle is mode-driven, not graph-driven, per the §4.3.EM-015d carve-out. The §7.5.3 validator enforces.
4. **Review-loop artifacts are not applicable.** The `${workspace_path}/.harmonik/reviewer-feedback.iter-<N>.md` artifact per §4.3.EM-015d (sub-clause EM-015d-RFD) and the `${workspace_path}/.harmonik/review-target.md` artifact per §4.3.EM-015d (sub-clause EM-015d-RIA) are `review-loop`-mode-only. A `dot` run MUST NOT produce them; their absence on a `dot` run is not an authoring error and MUST NOT be flagged by the §7.5.3 validator. (The EM-015d cross-ref placeholder is resolved by §7.5.)

Tags: mechanism

### 7.5.3 — Validator Requirements

#### EM-057 — `dot`-specific validator obligations

The §4.9.EM-038 pre-run validator, when invoked per §7.5.1.EM-055 step 4 against a `dot`-ingested `Workflow` record, MUST enforce the following seven static checks. Any check failing causes the validator to return `false`; §7.4's `queue.mark_item_failed(item, validation_failed)` path runs and the run never starts.

1. **Schema-version compatibility.** The `.dot` artifact's graph-level `schema_version` attribute MUST satisfy the §6.4 N-1 readability rule against the daemon's max-known schema version. A `schema_version` more than one version below the daemon's max OR higher than the daemon's max is a validation failure.
2. **Node-type vocabulary.** Every node's `type` attribute MUST be in the closed node-type catalog declared by [workflow-graph.md §4 WG-001/WG-002] (`{agentic, non-agentic, gate, sub-workflow}` at v1; the pre-D3 `control-point` type per §4.2.EM-006 is collapsed under `gate` and `non-agentic` per the C4 amendment to [control-points.md §4.12 CP-053/CP-054]). Unknown `type` value is a validation failure — refuse-to-run, not warn-and-continue.
3. **Edge-condition LHS whitelist and dialect.** Every edge's `condition` attribute, if non-empty, MUST tokenize per the dialect declared in [workflow-graph.md §6 WG-013] (restricted equality + `&&`, no precedence resolver) AND reference only LHS identifiers in the whitelist declared in [workflow-graph.md §6 WG-014] (the closed 5-LHS set). `context.<key>` references MUST resolve to a key declared in the workflow's [workflow-graph.md §10 WG-031] per-workflow registry.
4. **Reachability.** Every non-terminal node MUST be reachable from the workflow's `start_node` (per §6.1 `Workflow.start_node_id`); every node listed in the workflow's `terminal_node_ids` (per §6.1) MUST be reachable from the `start_node`.
5. **Cycle bounding.** Every directed cycle in the graph MUST traverse at least one edge carrying a positive-integer `traversal_cap` per §4.10.EM-043. Cycle detection runs at validation time (Tarjan's SCC algorithm or equivalent is sufficient), not at dispatch time. A cycle with no capped edge is a validation failure.
6. **Sub-workflow reference acyclicity.** Per §4.8.EM-034b, the sub-workflow reference graph MUST be acyclic. The validator MUST verify acyclicity transitively across the artifact registry resolved per §7.5.1.EM-055 step 1.
7. **Required attributes by node type.** Each node MUST carry the attributes required for its declared `type` per the per-node-type attribute table in [workflow-graph.md §4 WG-001/WG-002]:
   - `agentic` nodes MUST carry `handler_ref` resolving to a handler registered per [handler-contract.md §4.1].
   - `non-agentic` nodes MUST carry `handler_ref` resolving to a handler registered per [handler-contract.md §4.1]. (This obligation is the §4.2.EM-007 amendment per EM-060.) When the node is a tool node (carries `tool_command` per [workflow-graph.md §4 WG-039]), `handler_ref` MUST be `shell`, resolving to the built-in `shell` handler per [handler-contract.md §4.1 HC-063]; the validator MAY emit a warning rather than fail when `tool_command` is present with a non-`shell` `handler_ref` at v1 (reserved to become a validation failure at the next schema major bump).
   - `gate` nodes MUST carry `gate_ref` resolving to a Gate policy per [control-points.md §6.3] AND `handler_ref` resolving to the Gate-evaluator handler registered per [control-points.md §4.12 CP-053/CP-054]. (This obligation is the §4.2.EM-007 amendment per EM-060.)
   - `sub-workflow` nodes MUST carry `sub_workflow_ref` resolving to another `.dot` artifact registered with the daemon per §7.5.1.EM-055 step 1. `sub-workflow` nodes MUST NOT carry `handler_ref` (the handler discipline is delegated to the expanded sub-workflow's nodes, per §4.8.EM-034).

Missing or unresolvable required attributes on any node type are validation failures.

Tags: mechanism

### 7.5.4 — Node-type Dispatch Table

#### EM-058 — Normative dispatch table for `dot` node types

For a `dot` run, §7.4 `execute_workflow`'s `dispatch_node` step MUST dispatch the current node according to the following table. The table names the dispatch action and the consumed Outcome contract for each node type in the closed catalog declared by [workflow-graph.md §4 WG-001/WG-002]. The table is normative for `dot` mode; for `single` and `review-loop` modes, the historical dispatch fork in `dispatch_node` is unchanged.

| Node type | Dispatch action | Outcome contract |
|---|---|---|
| `agentic` | Launch the handler referenced by the node's `handler_ref` per [handler-contract.md §4.1] (handler subprocess; LaunchSpec per [handler-contract.md §4.2]). | §4.1.EM-005 `Outcome` with `kind = default` per §4.1.EM-005a. Handler MAY emit any `status`; MAY emit `failure_class` as a hint on FAIL per the C3 amendment to [handler-contract.md §4.2a HC-058]. |
| `non-agentic` | When the node carries `tool_command` and `handler_ref="shell"`: the built-in `shell` handler of [handler-contract.md §4.1 HC-063] executes the command in the run's worktree and applies the exit-state → Outcome mapping of HC-063 / [workflow-graph.md §4 WG-039]. The `shell` handler MAY run in-process (no subprocess, no socket) per the HC-063 exception. When the node carries no `tool_command` (start / terminal / pass-through node), the engine synthesizes a `SUCCESS` Outcome without dispatching a handler (the `internal/daemon/dot_cascade.go` in-process path). Otherwise (a non-agentic node bound to a non-`shell` handler), invoke the handler referenced by the node's `handler_ref` per [handler-contract.md §4.1]; handler-internal determinism is the handler's responsibility per the node's four-axis tags (§4.2.EM-011). | §4.1.EM-005 `Outcome` with `kind = default` per §4.1.EM-005a. |
| `gate` | Launch the Gate-evaluator handler referenced by the node's `handler_ref` per [control-points.md §4.12 CP-053/CP-054], passing the node's `gate_ref` as input per [control-points.md §6.3]. | §4.1.EM-005 `Outcome`. Per [control-points.md §4.12 CP-053/CP-054], the Gate-evaluator handler's Outcome MAY use `kind = default` at v1; a future `kind = gate_decision` extension is reserved per §4.1.EM-005a's amendment protocol but is NOT required at v1 conformance. |
| `sub-workflow` | Expand per §4.8.EM-034: pin the sub-workflow on the entry checkpoint per §4.8.EM-034c, push the node-ID namespace per §4.8.EM-034a, and descend into the expanded sub-graph. The cascade and durability decision continue to apply within the expansion. | The sub-workflow's terminal-node `Outcome` propagates verbatim to the parent's cascade per §4.8.EM-036a. |

The table is **load-bearing for implementer epics** (the dispatcher in `internal/daemon/` consumes this table to wire the per-type dispatch fork); it is NOT a new state machine. The `agentic`-vs-`non-agentic` distinction collapses at the spec-layer dispatch action (both go through the handler registry) but is preserved at the node-type catalog layer because the four-axis tags per §4.2.EM-011 (`llm-freedom`, `io-determinism`, `replay-safety`, `idempotency`) and the idempotency-class default per §4.2.EM-010 differ between the two types.

> **Non-agentic dispatch sub-note (component A).** The `non-agentic` row admits three dispatch paths distinguished by node content: (1) a tool node (`tool_command` + `handler_ref="shell"`) runs the built-in `shell` handler per [handler-contract.md §4.1 HC-063], which MAY execute in-process; (2) a start / terminal / pass-through node (no `tool_command`) is synthesized to `SUCCESS` without a handler dispatch; (3) any other `non-agentic` node invokes its bound `handler_ref` via the handler registry exactly as `agentic` does. The "invoke the handler referenced by `handler_ref`" dispatch action of the prior EM-058 row is preserved for path (3); paths (1) and (2) are the spec-layer reconciliation of the in-process behavior at `internal/daemon/dot_cascade.go`.
>
> **Non-committing `agentic` dispatch sub-note (component C).** For a `dot`-mode implementer-class `agentic` node, the engine derives the node Outcome after a clean agent exit as follows: if the node carries `non_committing="true"` per [workflow-graph.md §4 WG-041], a clean exit yields `Outcome{status = SUCCESS}` regardless of whether the worktree HEAD advanced; otherwise a clean exit that did NOT advance HEAD is a node failure. A worktree whose HEAD cannot be resolved at all is a daemon-side error in BOTH modes (a broken worktree is a real failure). This derivation is `dot`-mode-only; `review-loop` mode's implementer commit obligation per §4.3 EM-015d is unchanged.

Tags: mechanism

### 7.5.5 — Conformance Lift and Forward-Compatibility Reservation

#### EM-059 — `dot` mode conformance lift plus parallel-fan-out reservation

**Conformance lift.** The §10.1 carve-out making `dot` mode a post-MVH extension ("no MVH dispatcher is obligated to drive a `dot` run") is removed per §10.1 / EM-061 below. Once the full phase-3-dot bundle (C1 + C2 + C3 + C4 + C5 + C6) lands, conforming implementations MUST drive `dot` mode per §7.5.1 through §7.5.4. The lift is gated on the C2-related schema bumps being recorded per §6.4: the §4.1.EM-005 `Outcome.failure_class` additive bump (per the C3 amendment) and the §4.2.EM-006 node-type-catalog breaking bump (per the C1 amendment collapsing `control-point` into `gate`/`non-agentic`).

**Parallel fan-out reservation.** MVH `dot` mode dispatches a run's nodes sequentially; parallel-node-type semantics (a node whose dispatch fans out to multiple concurrent sub-dispatches) are deferred to a post-MVH amendment per [architecture.md §4.6]. The deferral is consistent with [workspace-model.md §4.5] and [workspace-model.md §4.7]'s one-agent-per-worktree-at-MVH invariant; any future parallel-fan-out amendment to §7.5 will require a coordinated amendment to workspace-model.md to lift that invariant. No `.dot` schema field, no §6.1 record field, and no §7.4 dispatch primitive is reserved for parallel-fan-out at v1.

**Cross-reference resolutions.** The placeholder text in §4.3.EM-015d ("(cross-ref pending dot-mode spec)") is rewritten to "(see §7.5 — `dot` mode binding)." The §4.3.EM-012 clause "For `workflow_mode = dot`, reserved keys are out of scope for MVH conformance" is rewritten to "For `workflow_mode = dot`, the per-workflow context-key registry per [workflow-graph.md §10 WG-031] is the authoritative source of allowed `context.<key>` LHS references; this spec reserves no context keys for `dot` mode."

Tags: mechanism

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

> INFORMATIVE: Of the six classes in this section, two carry **handler-fatal sub-cases** in the daemon's handler-pause policy: `transient` (specifically `agent_rate_limited` observed twice consecutively without an intervening `agent_rate_limit_cleared`) and `budget_exhausted` (when the underlying budget point declares `budget_scope = handler-account`).  Classification authority remains in this spec (§8); the handler-pause controller is a downstream policy consumer described in [docs/components/internal/handler-pause-and-resume.md] and specified normatively in [specs/handler-pause.md §5.2].  The normative taxonomy table, Go constants, and classifier function are declared in [specs/handler-contract.md §4.5a HC-020a].  The taxonomy in §8 is unchanged by handler-pause.

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
- **[event-model.md §8.10]** — queue-lifecycle event cohort (`queue_submitted`, `queue_group_started`, `queue_group_completed`, `queue_paused`, `queue_appended`, `queue_item_deferred_for_ledger_dep`) and the optional `queue_id` / `queue_group_index` fields on `run_started` / `run_completed` / `run_failed` payloads.
- **[event-model.md §4.4]** — fsync policy; the checkpoint-written event drives an fsync boundary.
- **[event-model.md §4.5]** — observational-vs-state-reconstruction replay split; this spec names git + Beads as the state-reconstruction source (§4.7.EM-031).
- **[handler-contract.md §4.1]** — handler interface emitting `Outcome` instances conforming to §4.1.EM-005; ActionDescriptor is defined there.
- **[handler-contract.md §4.5]** — error sentinels (ErrTransient/Structural/Deterministic/Canceled/Budget) that drive the classifier in §8.
- **[handler-contract.md §4.11]** — skills declaration surface referenced by node `required_skills` in §4.2.EM-008.
- **[queue-model.md §3 QM-001, QM-002]** — queue persistence at `.harmonik/queue.json` and active-queue authority; consumed by §7.4 and §4.11.
- **[queue-model.md §4 QM-010..012]** — queue identity (`QueueID`, `QueueGroupIndex`); fields surfaced on Run records per §6.1 and on `run_*` event payloads per §4.3.EM-015a, §4.3.EM-015b.
- **[queue-model.md §5]** — queue-group state machine; consumed by §4.3.EM-015f and the §7.4 dispatch loop.
- **[queue-model.md §7]** — queue append semantics; `queue_appended` event emission per §6.5.
- **[queue-model.md §8]** — queue lifecycle (`paused-by-failure`, `paused-by-drain`, `completed`); consumed by §7.4's status check and §4.3.EM-015f's pause transition.
- **[queue-model.md §9]** — concurrency composition rules; reads §4.11's primitives.
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
- **[process-lifecycle.md §4.1]** — daemon configuration surface; `--max-concurrent` flag per §4.11.EM-051 is declared there.
- **[process-lifecycle.md §4.2 Startup sequence]** — daemon walks the git checkpoint trail defined here during the reconciliation phase of startup; the submission RPC surface referenced by §4.9.EM-040 is declared there.

> INFORMATIVE: All inter-spec citations in this section are forward-references to specs not yet drafted; per template §Cross-reference convention ("Bootstrap-only — citing foundation docs"), these citations are expected to migrate to bootstrap form `[docs/foundation/components.md §<N>]` if the target spec is not finalized at this spec's `reviewed` gate. The normative migration obligation is tracked in OQ-EM-005.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH (amended — EM-061).** An implementation conforming to Core MVH MUST pass every requirement in EM-001 through EM-046 (including sub-requirements EM-012a, EM-015a, EM-015b, EM-015c, EM-015d, EM-015e, EM-015f, EM-017a, EM-018a, EM-020a, EM-023a, EM-024a, EM-025a, EM-031a, EM-034a, EM-034b, EM-034c, EM-036a, EM-041a, EM-042a, EM-043a, EM-046a, EM-046b) and EM-049 through EM-054 (concurrency primitives §4.11, merge-to-main §4.12), **and EM-055 through EM-059 (`dot`-mode binding §7.5)**, **and EM-066 through EM-067 (no-auto-pull topology + operator-pause fallback gate §4.11)**, plus invariants EM-INV-001, EM-INV-004, and EM-INV-005 (the three invariants surviving the §5 selection test; EM-INV-002, EM-INV-003, EM-INV-006 are retired). For `workflow_mode = single` (the default per §4.3.EM-012a built-in fallback), the cascade (§4.10), checkpoint cadence (§4.5), and sub-workflow composition (§4.8) apply unchanged. For `workflow_mode = review-loop`, the hardcoded two-node cycle of §4.3.EM-015d MUST be observed; the cap and early-exit rules of §4.3.EM-015e MUST be enforced; the six review-loop events of §6.5 MUST be emitted. **For `workflow_mode = dot`, the input contract (§7.5.1.EM-055), dispatch equivalence (§7.5.2.EM-056), validator obligations (§7.5.3.EM-057), and node-type dispatch table (§7.5.4.EM-058) MUST be observed.** Dispatch input MUST be the active queue per §7.4. Queue-only is the default for all topologies (per §4.11.EM-066 — hk-8vy18); a bare boot with no submitted queue MUST dispatch zero runs. When `--auto-pull` is set (the historical single-daemon topology opt-in), the `br ready` fallback is a conforming opt-in, gated on operator-pause state per §4.11.EM-067. The `--no-auto-pull` flag is accepted as a no-op back-compat alias. The EM-007 amendment per §4.2 (admitting `handler_ref` on `non-agentic` and `gate` nodes) is normative at Core MVH.

**Post-MVH extensions (amended — EM-061).** Failure-commit emission (deferred per §4.5.EM-025) and `recoverable-non-idempotent` node-type defaults (§4.2.EM-010) are additive extensions to Core MVH; neither is required to claim Core MVH conformance. **Parallel fan-out in `dot` mode** (multiple concurrent sub-dispatches from a single node) is a post-MVH extension reserved per §7.5.5.EM-059; the v1 `dot` dispatcher dispatches sequentially. Runtime mutation of `max_concurrent` (§4.11.EM-051) is a post-MVH extension.

**Gating clause (EM-061).** The `dot`-mode conformance lift above is conditional on the full phase-3-dot bundle landing in the spec corpus: [workflow-graph.md] (C1) drafted at pass-6; [handler-contract.md §4.2a HC-058] amendment (C3) drafted at pass-6 with the §4.1.EM-005 `Outcome.failure_class` additive bump recorded per §6.4; [control-points.md §4.12 CP-053/CP-054] amendment (C4) drafted at pass-6; the canonical [specs/examples/review-loop.dot] artifact (C5) registered with the daemon; and the dispatcher implementation (C6) landed. Until all six are recorded, EM-055 through EM-059 are SHOULD-grade for early implementations and `dot` mode remains a post-MVH extension as in the pre-amendment §10.1 prose. The full lift to MUST-grade for Core MVH lands when the bundle's pass-6 transcription completes (tracked as a coordinated revision-history entry in §12).

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose. Each requirement's test obligation:

- **EM-001 — EM-005 (core types).** Schema conformance tests: every persisted `Transition` file validates against the §6.1 record schema; every checkpoint commit's trailers validate against §6.2.
- **EM-006 — EM-011 (node attributes).** Workflow-validator unit tests covering each attribute check in EM-038, including negative cases (missing `idempotency_class`, invalid `handler_ref`).
- **EM-012 — EM-015, EM-015a — EM-015c, EM-015f (run model, lifecycle emissions, queue-group advance).** End-to-end scenario tests covering one-bead-many-runs after `reopen-bead` verdict; intra-run-loop traversal verified distinct from re-run; Run.context mutation ordering verified pre-cascade; `run_started` emission verified after atomic-claim persist (EM-015a); `run_completed` and `run_failed` emission verified on terminal state with correct payload, including optional `queue_id` / `queue_group_index` fields when the run is queued (EM-015b); terminal-state detection verified against `terminal_node_ids` and classifier verdicts (EM-015c); group-advance gate verified: group does NOT advance until all-terminal; `complete-success` triggers `queue_group_completed` + `queue_group_started`; `complete-with-failures` triggers `queue_group_completed` + `queue_paused` and queue does NOT advance (EM-015f).
- **EM-016 — EM-022 (checkpoint contract).** Crash-recovery scenario tests: kill the daemon between `git write-tree` and `git update-ref`; verify no partial state is observable AND orphan loose objects are eligible for `git gc` (EM-016 clarification); verify the trailer-and-sibling-file atomicity invariant; verify corrupted-checkpoint fallback (EM-017a) dispatches reconciliation AND bounds recursion at one level; verify audit tool (EM-020a) detects all five integrity violations (including run_id/trailer disagreement); verify sibling file is under `.harmonik/transitions/<run_id>/<transition_id>.json` (path scoping); verify cross-run merges and cherry-picks do not collide at the sibling-file path.
- **EM-023 — EM-026 (checkpoint cadence).** Integration tests verifying every durable transition produces exactly one commit; reconciliation workflows produce exactly one verdict commit; failure transitions produce zero commits; PARTIAL_SUCCESS produces a durable commit with `partial_success=true` flag; gate-denied transitions produce zero commits; branch-tip monotonicity check (EM-024a) flags externally-rewound task branches; transition-event emission never precedes `git update-ref` (EM-025a); ENOSPC retries with new transition_id and evidence-orphan cleanup.
- **EM-027 — EM-030 (outcome spine).** Cross-subsystem tests with twin handler: verify the full flow from handler outcome to transition-event projection; verify consumer retrieving the full trace via `git show`.
- **EM-031 — EM-033 (state reconstruction).** Restart scenario tests: destroy the daemon; confirm full state reconstructable from git + Beads without JSONL reads; confirm active-run discovery (EM-031a) correctly identifies in-flight runs from ref scan + Beads query; confirm no rollback on later-transition failure; confirm JSONL torn-tail does not produce false Cat 6b signal (EM-031); confirm Beads-unreachable triggers Cat 0 and `degraded` status rather than silent git-only fallback; confirm worktree state is preserved across crash → reconciliation dispatch.
- **EM-034 — EM-037 (sub-workflow).** Nested-workflow scenario tests: single run_id across nesting; checkpoint commits all on parent branch; namespaced `node_id` appears in state and transition records; mutual sub-workflow reference rejected by validator; sub-workflow-entered/exited lifecycle events fire; expansion pin is readable from the entry checkpoint's evidence map after daemon restart (EM-034c); registry updates between crash and restart do not change the run's expanded graph; sub-workflow terminal outcome at the parent's cascade matches the last-expanded-node's Outcome (EM-036a).
- **EM-038 — EM-040 (validation).** Validator unit tests for every failure mode listed in EM-038, including sub-workflow-reference cycle detection and missing `start_node_id` / empty `terminal_node_ids`.
- **EM-041 — EM-046, EM-046a, EM-046b (edge selection, backtracking, cycles).** Edge-cascade unit tests enumerating every precedence case; cycle-cap tests verifying `compilation_loop` failure at cap (disjoint from `structural`); traversal-counter recovery across restart verified by re-derivation from git log; rollback-transition tests verifying new `transition_id` and unchanged earlier commit; no-matching-edge scenario produces `structural` failure with reason `no_outgoing_edge_matches` (EM-046a); gate-deny enters `gate-pending` and waits for gate-resolution signal (EM-042a); RETRY re-dispatches the same node with context_updates applied pre-redispatch and fails as `transient` at retry-cap exhaustion (EM-046b).
- **EM-049 — EM-051 (concurrency primitives).** Capacity-gate tests: with `max_concurrent = K` and a wave of N > K items, verify at most K runs are in-flight at any instant; verify slot-release on terminal triggers next dispatch (EM-049). Claim-serialization tests: concurrent ClaimBead writes are serialized through the token-pool of size `max_concurrent` (EM-050). Configuration tests: `--max-concurrent` accepts integer ≥ 1, defaults to 1, is sealed at startup (EM-051).
- **EM-066 — EM-067 (no-auto-pull topology + operator-pause fallback gate).** Quiet-daemon test: boot a daemon without `--auto-pull` (the default) and submit no queue; verify over a bounded observation window that zero `run_started` events are emitted, no agent subprocess is spawned, and the daemon sits in the `idle_wait_for_queue_submission` branch (EM-066). Historical-topology test: boot a daemon WITH `--auto-pull` with ≥1 ready bead and no queue; verify the `br ready` fallback dispatches `ready[0]` (EM-066 opt-in branch). Sealing test: verify the auto-pull configuration is sealed at startup and not re-read for the daemon's lifetime (parity with EM-051). Pause-gate test (observable outcome): with the fallback enabled (flag unset), ≥1 ready bead, no queue, and the daemon's operator-control state driven to `paused` (via the ON-056/ON-057 producer), verify no new `run_started` is emitted while paused — this is the observable EM-067 conformance criterion regardless of whether the primary §7.4 loop-top ON-008 gate or the inline defense-in-depth assertion enforces it — and that on `resume` (state → `running`) fallback dispatch of `ready[0]` resumes (EM-067). Single-source-of-truth test: assert the pause state observed by the fallback gate is the same `operator_pause_status` value (ON-056/ON-057) that drives the queue-level QM-054 transition — not a divergent pause concept.
- **EM-052 — EM-053 (merge-to-main on success).** Integration test: simulate a successful run on a worktree branch (`run/<run_id>`) with one commit; verify (a) `refs/heads/main` advances to the run-branch tip after Step 9 success branch executes, (b) a push-origin-main attempt is made, (c) `outcome_emitted{kind=approved}` event is emitted before `bead_closed`, (d) `bead_closed` event is emitted after `CloseBead`, (e) `run_completed{success:true}` is the final lifecycle event. Non-FF test: place an out-of-ancestry commit on `main` after the worktree branch is cut AND after the rebase completes; verify (f) `ReopenBead` is called, (g) `outcome_emitted{kind=rejected, reason=non_ff_merge}` is emitted, (h) `CloseBead` is NOT called (EM-053). Rebase test: advance `main` concurrently without conflicts; verify (i) rebase succeeds, (j) `refs/heads/main` advances to the rebased run-branch tip, (k) `outcome_emitted{kind=approved}` is emitted. Rebase-conflict test: advance `main` with a conflicting change; verify (l) `ReopenBead` is called with `rebase_conflict` reason, (m) `CloseBead` is NOT called. Build-gate test (EM-052 step 4a): commit a Go module (`go.mod`) to the initial `main` branch; agent commit introduces a compile error; verify (n1) `merge_build_failed` is emitted, (n2) `ReopenBead` is called, (n3) `CloseBead` is NOT called, (n4) `refs/heads/main` is NOT advanced (rollback fired). Vet-gate test: agent commit introduces a `go vet` failure; verify same assertions (n1)–(n4) with reason containing `go vet`. No-go.mod test: project dir has no `go.mod`; agent commits any file; verify (p) the build gate is skipped, normal success path runs (`CloseBead` called, `refs/heads/main` advances).
- **EM-054 (working-tree refresh after successful merge).** Integration test: after a successful merge-to-main (EM-052 path), verify that `git status --porcelain` in the project root is empty for files modified by the run-branch commit (i.e., the project working tree reflects HEAD). Refresh-failure test: inject a stub that makes `git reset --hard HEAD` fail; verify (a) `CloseBead` is still called (merge succeeded), (b) a `working_tree_refresh_failed` event is emitted, (c) `ReopenBead` is NOT called.
- **EM-055 — EM-059 (`dot`-mode binding §7.5).** The following test obligations apply to the `dot`-mode ingestion and dispatch path:
  - **Round-trip parse (EM-055).** Parse [specs/examples/review-loop.dot] through the §7.5.1 ingestion pipeline; verify the produced §6.1 `Workflow` record has `workflow_id`, `start_node_id`, and `terminal_node_ids` matching the DOT graph-level attributes. Verify that a `.dot` artifact with a missing `workflow_id` attribute fails ingestion before §7.4 starts.
  - **Restart-reparse equivalence (EM-055).** Simulate a daemon restart after `dot` ingestion completes: re-run steps 1–4; verify the produced `Workflow.workflow_id` and `workflow_version` match the pre-restart values. Mutate the artifact on disk between runs; verify the mismatch routes to reconciliation Cat 3 and does NOT silently proceed.
  - **Dispatch equivalence (EM-056).** Verify that a `dot` run evaluates edge conditions, applies `traversal_cap`, and handles no-match-edge (§4.10.EM-046a) identically to a `single` run when given the same graph topology expressed as a §6.1 record.
  - **Validator negative cases — all seven obligations (EM-057).** For each of the seven validator checks in §7.5.3: (1) schema-version out of N-1 window fails; (2) unknown `type` value fails; (3) edge-condition LHS outside the whitelist fails; (4) unreachable node fails; (5) cycle with no capped edge fails; (6) sub-workflow reference cycle fails; (7) missing required attribute by node type fails (separate case per type: missing `handler_ref` on `agentic`; missing `handler_ref` on `non-agentic`; missing `gate_ref` on `gate`; missing `sub_workflow_ref` on `sub-workflow`; forbidden `handler_ref` on `sub-workflow`). Each negative case MUST route to `queue.mark_item_failed(item, validation_failed)` and MUST NOT emit `run_started`.
  - **Dispatch-table coverage (EM-058).** For each of the four node types: construct a minimal `dot` workflow containing a node of that type; verify the correct dispatch action fires (handler subprocess for `agentic` / `non-agentic`; gate-evaluator handler for `gate`; sub-workflow expansion for `sub-workflow`). Verify the `agentic`-vs-`non-agentic` distinction is preserved in the four-axis tags but both route through the handler registry.

Migration to `[testing.md §<layer>]` cross-references occurs within one revision cycle once testing.md lands; this obligation is tracked in OQ-EM-003.

### 10.3 Excluded conformance claims

- This spec does NOT grant conformance over: handler-specific wire protocol details (owned by [handler-contract.md]); the reconciliation category classifier (owned by [reconciliation/spec.md]); JSONL format specifics (owned by [event-model.md]); policy-expression grammar (owned by [control-points.md]); operator-CLI surface (deferred to a separate spec per [operator-nfr.md §4.10]); the queue persistence file format, identity, group state machine, validation, append semantics, and queue lifecycle (owned by [queue-model.md]).
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
| 2026-06-10 | 0.8.3 | agent (hk-ycp62) | **Post-merge build gate: EM-052 step 4a, EM-053 extended, §10.2 test obligations added.** New **step 4a** inserted into §4.12.EM-052 between step 4 (update-ref) and step 5 (push): after fast-forwarding `refs/heads/main`, the daemon MUST run `go build ./...` and `go vet ./...` in the run-branch worktree directory (falls back to project root when worktree is absent), guarded by a `go.mod` presence check so non-Go repos are unaffected. On any failure: roll back the `update-ref`, emit `merge_build_failed` (new event type, registered by hk-o68j3), proceed to the EM-053 reopen path. **EM-053** title and trigger clause extended: `merge_build_failed` added as a fourth trigger alongside `rebase_conflict`, `non_ff_merge`, and `push_failed`. **Bug fix (hk-ycp62):** the initial hk-o68j3 implementation ran the build in `projectDir` (pre-merge working tree); amended to run in `wtPath` (the post-rebase worktree) so that cross-bead compile errors — e.g. two parallel beads each declaring a package-level helper — are caught in the merged content, not the old main content. **§10.2** test obligations for EM-052/EM-053 extended: build-gate test (assertions n1–n4), vet-gate test, and no-go.mod skip test (assertion p). §4.12 heading comment in `mergeRunBranchToMain` updated to list step 4a. **EM-052 Refs** updated to add `hk-o68j3, hk-ycp62`. No prior requirement IDs renumbered or retired; strictly additive + bug-fix over v0.8.2. Refs: hk-o68j3, hk-ycp62. |
| 2026-05-31 | 0.8.2 | agent (hk-8vy18) | **Flip auto-pull default OFF: queue-only by default; --auto-pull opt-in.** Amended **EM-066** §4.11 Default paragraph: queue-only is now the default for ALL daemon topologies (not topology-scoped). `--auto-pull` is the new opt-in flag to enable the historical `br ready` fallback; `--no-auto-pull` is retained as a no-op back-compat alias. Updated §7.4 pseudocode comment, §10.1 Core MVH conformance prose, and §10.2 EM-066 test obligations to reflect the new default. No requirement IDs added or retired; strictly amendatory over v0.8.1. Source: user directive 2026-05-31 (hk-8vy18). |
| 2026-05-31 | 0.8.1 | agent (kerf `pilot` work) | **Quiet daemon (no-auto-pull topology) + operator-pause fallback gate (A1).** Added **EM-066** (§4.11): startup-sealed boolean `--no-auto-pull` (alias `--queue-only`) flag — when set, the daemon MUST NOT fall back to `br ready` and a bare boot with no submitted queue dispatches zero runs (no `run_started`, no agent spawn, no credit); when unset, the historical single-daemon `br ready` fallback is retained as an opt-in. Default is topology-scoped: ON for the supervised (flywheel) topology, OFF (fallback retained) for the historical single-daemon topology. This reconciles the long-standing spec-vs-code contradiction (§7.4 / §10.1 already said "MUST NOT fall back to br ready"; the live binary still did). Added **EM-067** (§4.11): binds the `br ready` fallback path to the single source of pause truth from [operator-nfr.md §4.3 ON-056/ON-057] (`operator_pause_status`, the same signal driving QM-054) so the fallback and active-queue branches honor ONE pause concept; clarifies that the §7.4 loop-top `should_pause_between_runs()` check (the ON-008 between-task drain gate) is the PRIMARY operator-pause gate covering ALL paths including the fallback (so EM-067 is not a new primary gate), and that the inline fallback-branch check is a belt-and-suspenders assertion (vacuous when the loop-top check covers the full operator-control state). §7.4 main-loop pseudocode reconciled: the `queue IS None` branch is now a flag-gated two-way branch (idle-wait under `--no-auto-pull`; `br ready` fallback otherwise, with a defense-in-depth operator-pause re-assert). §10.1 Core MVH conformance refined (fallback non-conforming under `--no-auto-pull`; conforming opt-in otherwise) and extended to require EM-066/EM-067; §10.2 adds test obligations for EM-066/EM-067. New requirement IDs: EM-066, EM-067. No prior IDs renumbered or retired. Source: kerf `pilot` 04-design/execution-model-design.md. |
| 2026-05-30 | 0.8.0 | agent (flywheel spec-bundle hk-j7o3i) | **Flywheel additions: EM-NOTE-WAKE, EM-NOTE-STREAM-CONCURRENCY, §4.13 eager-refill (EM-062/063), §4.14 check-observed-before-submit guard (EM-064/065).** EM-NOTE-WAKE (informative): hk-24xn1 closed; sub-poll wake via `SetQueue→wakeC`; stale CLAUDE.md "append while idle sits pending" guidance superseded. EM-NOTE-STREAM-CONCURRENCY (informative, V2 correction): `streamEligible` skips `dispatched` items (no HOL block); `--wave` is append-closed not concurrency-required; earlier guidance "use `--wave` when `--max-concurrent > 1`" SUPERSEDED. §4.13 eager-refill: EM-062 trigger on `run_terminal` + every poll tick; `available = max_concurrent − in_flight_count`, deficit-based pull from `kerf next` ×2 OVERFETCH_FACTOR, `queue_append` survivors; refill fires AFTER terminal-event processing. EM-063 two-phase pre-screen: queue.json in-memory (Phase 1) then `git log origin/main` (Phase 2); `stale_open_bead_detected` informative event on Phase-2 hit; readiness gate of `kerf next` enforces no-self-dispatch. §4.14 check-observed-before-submit: EM-064 4-tier authority chain (queue.json → git origin/main → Beads → events.jsonl); tiers 1+2 mandatory for v1; `origin/main` not local `main`. EM-065: orchestrator uses `queue-append` against active stream group; daemon's Beads atomic claim is final barrier. New IDs: EM-062, EM-063, EM-064, EM-065. No prior IDs renumbered or retired. |
| 2026-05-27 | 0.7.1 | agent (hk-lt0w7) | **EM-005b gate-deny example aligned to CP-058 (deny→SUCCESS, not FAIL).** Resolved the OQ contradiction between EM-005b and [control-points.md §6.1.8 CP-058]: CP-058 wins (gate-semantics owner). A `gate_decision` Outcome's `status` is now `SUCCESS` regardless of `decision ∈ {allow, deny, escalate-to-human}` — all three are *successfully-evaluated* Gate verdicts; the cascade routes on the decision (`outcome.preferred_label` carrying the decision string, or `outcome.kind = gate_decision`), NOT on `status`. `status = FAIL` is reserved STRICTLY for a Gate that could not be evaluated, which carries a `failure_class` and NO `gate_decision` payload. Rewrote the EM-005b five-field paragraph (corrected the `decision` enum to `{allow, deny, escalate-to-human}`, dropped the wrong `decision=permit→SUCCESS / decision=deny→FAIL` correlation, added the eval-failure→FAIL-no-payload paragraph), rewrote the orthogonality paragraph (the two carriers `kind=gate_decision` and `failure_class` are now correctly MUTUALLY EXCLUSIVE on a Gate outcome, replacing the wrong co-emitted budget-exhaustion example), and amended the cascade-routing paragraph to require decision surfacing via `outcome.preferred_label`. No requirement IDs added, renumbered, or retired; EM-005b reworded in place. Refs: hk-lt0w7. |
| 2026-05-26 | 0.7.0 | agent (hk-wclep) | **C3 amendments: EM-005 v0.3.3→v0.3.4 schema bump, EM-005a `gate_decision` extension, new EM-005b + EM-005c.** EM-005 (§4.1) amended to add optional `failure_class` field description: handler-emitted HINT, daemon-back-fills from §8 sentinel when omitted, engine-side MUST be populated on FAIL, daemon authoritative on disagreement per HC-059 (CI-5 remediation: two-sided contract, handler-side optional, daemon-side mandatory post-classifier). EM-005a (§4.1) amended: `kind` enum extended from `{default, reconciliation_verdict}` to `{default, reconciliation_verdict, gate_decision}`; new bullet for `gate_decision` semantics; N-1 routing parenthetical clarification added. New **EM-005b** (§4.1) — gate-decision Outcome variant; `GateDecisionPayload` per [control-points.md §6.1.8 CP-058]; five-field record; `decision=permit` correlates `status=SUCCESS`, `decision=deny` correlates `status=FAIL`; cascade routes on `kind` discriminator only; `failure_class` and `kind=gate_decision` are orthogonal. New **EM-005c** (§4.1) — Outcome schema v2 bump rationale; v0.3.3→v0.3.4 additive; N-1 preserved. §6.1 RECORD Outcome: added `failure_class : FailureClass | None` row (before `kind`); `payload` row type-annotation widened to note `GateDecisionPayload` for `kind=gate_decision` (CI-6 remediation). §6.1 ENUM OutcomeKind: added `gate_decision` value. §6.1 new ENUM FailureClass: surfaced from §8 as Outcome carrier citation. §6.1 INFORMATIVE note (VerdictPayload): amended to clarify alias resolves to union of `VerdictEvent` (for `kind=reconciliation_verdict`) AND `GateDecisionPayload` (for `kind=gate_decision`) — CI-6 remediation preserving umbrella-alias design. §6.4 schema evolution: added v0.3.3→v0.3.4 rationale paragraph. CITATION CLEANUP: `[workflow-graph.md §C1 design — context-key registry]` → `[workflow-graph.md §10 WG-031]` (applied in HC-062, references EM not directly affected here). No existing EM IDs renumbered or retired. Refs: hk-wclep. |
| 2026-05-26 | 0.6.0 | agent (hk-f2bfv) | **C2 DOT amendments: new §7.5 (`dot` mode binding), EM-007 amendment, §10.1 conformance lift, NodeType enum update.** New §7.5 (five sub-sections) introduces the `dot` workflow input contract (EM-055), dispatch equivalence statement (EM-056), `dot`-specific validator obligations (EM-057), per-node-type dispatch table (EM-058), and conformance lift + parallel-fan-out reservation (EM-059). EM-007 (§4.2) amended in place to admit `handler_ref` on `non-agentic` and `gate` nodes; pre-C4 prohibition superseded (EM-060 bookkeeping handle). §10.1 Core MVH and Post-MVH paragraphs updated to include EM-055..EM-059 as mandatory conformance targets and add gating clause (EM-061 bookkeeping handle). §6.1 ENUM NodeType updated to drop `control-point` (CI-8; breaking change — §6.4 bump recorded here). §4.2.EM-006 updated to reflect four-type catalog. §3 Glossary `workflow_mode` entry extended with §7.5 cross-ref. §4.3.EM-015d cross-ref placeholder resolved to §7.5. §4.3.EM-012 `dot` context-key clause rewritten per §7.5.5.EM-059. §10.2 test obligations added for EM-055..EM-059. CI-1 remediation: all C1 named-anchor references in §7.5 rewritten to use WG-NNN IDs. Contradiction 4 remediation: BI-005 `workflow_ref` reference dropped from EM-055 step 1; artifact path sourced from per-daemon config only at v1. NIT citation fixes: `[handler-contract.md §Outcome]` → `[handler-contract.md §4.2a HC-058]`; `[control-points.md §Node-Type Binding]` → `[control-points.md §4.12 CP-053/CP-054]`. Cross-spec sequencing: C1 + C3 + C4 + C5 + C6 required for full conformance lift (gating clause). Refs: hk-f2bfv. |
| 2026-05-21 | 0.5.4 | agent (hk-j1aq5) | **Pre-merge rebase step: EM-052 step 2, EM-053 extended.** Inserts a new step 2 into EM-052: if the run worktree directory still exists, the daemon MUST `git rebase main` in the worktree before the FF check (hk-j1aq5). On rebase conflict: `git rebase --abort` then fall through to the EM-053 reopen path with reason `rebase_conflict`. On rebase success: re-resolve both the run-branch tip and `main` tip before the FF check. This eliminates spurious `non_ff_merge` failures when parallel agents land concurrently. Steps renumbered: old 2→3, 3→4, 4→5, 5→6, 6→7, 7→8. References in EM-053 ("step 2", "step 4"), EM-054 ("step 3 and step 4"), and §10.2 test obligations updated accordingly. EM-053 title extended to include `rebase_conflict` as a third trigger. No new requirement IDs; strictly amendatory over v0.5.3. Refs: hk-j1aq5. |
| 2026-05-18 | 0.5.3 | agent (hk-rwdvm) | **EM-015e grammar catchup: add ENUM ReviewLoopCompletionReason to §6.1.** EM-015e prose at §4.3 already carried all five values (`approved`, `cap_hit`, `blocked`, `no_progress`, `error`) and the code in `internal/core/reviewloopevents_hk7om2q4.go` already emits `error` on malformed-verdict paths. The §6.1 grammar section lacked a formal `ENUM ReviewLoopCompletionReason` block — added here to close the code↔spec surface gap. The `error` value's one-line rationale cites [event-model.md §8.1a.3] which is the normative authority for the `review_loop_cycle_complete` payload. No requirement IDs added, renumbered, or retired; strictly additive over v0.5.2. |
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft. |
| 2026-04-23 | 0.2.0 | foundation-author | Round-1 reviewer integration. Dropped event-model from `depends-on` (breaks cycle; moved to §9.3). Added typed ID aliases (RunID/StateID/TransitionID/NodeID/BeadID/CommitRange). Added `start_node_id` and `terminal_node_ids` to Workflow schema. Added `context` field to Run schema. Added new requirements EM-017a (corrupted-checkpoint fallback), EM-018a (transition_id uniqueness contract — chose UUIDv7 uniqueness over run-scoped path as less disruptive), EM-020a (audit tool rule), EM-023a (durability decision table; includes PARTIAL_SUCCESS handling), EM-031a (active-run discovery), EM-034a (sub-workflow node-ID namespacing), EM-034b (sub-workflow reference acyclicity), EM-041a (context-update ordering), EM-043a (traversal-counter storage locus). Retired invariants EM-INV-002 (duplicate of EM-023), EM-INV-003 (duplicate of EM-019), and EM-INV-006 (duplicate of EM-012) per §5 selection test; surviving invariants keep original IDs EM-INV-001, EM-INV-004, EM-INV-005 (retired IDs are not reused per template). Fixed MUST/SHOULD discipline on EM-010 (normative defaults), EM-017 (added fallback), EM-020 (removed redundant Axes per declaration exemption — moved to EM-017/EM-018 write requirements), EM-025 (strengthened to MUST NOT at MVH), EM-039 (positive phrasing), EM-041 (collapsed double-modal). Defined "durable transition" mechanically in glossary + EM-023a. Defined "terminal node" and "in-flight run" in glossary. Deferred sub-workflow event names to event-model per §2.2 scope discipline (EM-036). Fixed emission ownership in §7.1 and §8 (operator_stopped, budget_exhausted owned upstream). Clarified `compilation_loop` vs `structural` as disjoint at emission. Removed `Harmonik-Verdict-Executed` from §6.2 trailer table (deferred to reconciliation §9.5b). Added EM-016 atomic-commit plumbing (write-tree / commit-tree / update-ref). Added EM-046 rollback-to-state-id constraint for context-restore. Added OQ-EM-005 (bootstrap migration), OQ-EM-006 (ErrCompilationLoop coordination), OQ-EM-007 (sub-workflow terminal outcome composition). |
| 2026-04-24 | 0.3.0 | foundation-author | Round-2 reviewer feedback integrated: path-scoped transition records, main-loop pseudocode, sub-workflow pin durability, 8 xref fixes, 4 missing-case requirements. Path-scoped the sibling-file path from `.harmonik/transitions/<transition_id>.json` to `.harmonik/transitions/<run_id>/<transition_id>.json` (EM-018/EM-019/EM-020a/EM-021/§6.1 Checkpoint/§7.2); EM-018a reframed from globally-unique contract to daemon-local generation rule (structural uniqueness now provided by path scoping). Added §7.4 main-loop protocol pseudocode (`orchestrator_main_loop` + `execute_workflow`) plus EM-015a (run_started emission), EM-015b (run_completed/run_failed emission), EM-015c (terminal detection rule). Added EM-034c (sub-workflow expansion pin durable on entry checkpoint's evidence map), EM-036a (sub-workflow terminal outcome is last-expanded-node's Outcome; resolves OQ-EM-007). Added EM-024a (branch-tip monotonicity check vs external force-push), EM-025a (emission ordering + ENOSPC handling + evidence orphan cleanup), EM-042a (gate-deny continuation via gate-pending sub-state), EM-046a (no-matching-edge failure class = structural), EM-046b (RETRY re-dispatch protocol). Clarified EM-016 loose-object atomicity (orphan loose objects eligible for git gc). Extended EM-017a with reconciliation-recursion bound (one level). Extended EM-031 with JSONL torn-tail tolerance + git-corroboration rule for divergence-evidence. Extended EM-031a with Beads-unreachable cross-link to Cat 0 + worktree-preservation rule (no pre-dispatch cleanup). Reframed EM-INV-004 to span four subsystems' authoring surfaces. Updated EM-046 to name daemon-synthesized Outcome for context-restore; EM-023a references the synthesized-Outcome rule and adds `outcome_status` as first-class Transition field (§6.1). Added `workflow_class` validator constraint (MVH only value: "reconciliation"). Fixed 8+ broken cross-refs: `[beads-integration.md §10.x]` → `§4.x` / `BI-NNN` (EM-013, EM-014, EM-031, EM-INV-005, §2.2, §6.1 BeadID, §6.1 Run.bead_id, §9.3); `[reconciliation.md §9.x]` → `§4.x` / `§8` / `RC-NNN` (EM-017a, EM-024, EM-025a, EM-031, EM-031a, EM-033, EM-INV-004, EM-INV-005, §2.2, §4.2 informative, §9.3). ID freeze at `reviewed`: new IDs EM-015a, EM-015b, EM-015c, EM-024a, EM-025a, EM-034c, EM-036a, EM-042a, EM-046a, EM-046b are assigned; no IDs retired in v0.3. Status draft → reviewed. |
| 2026-04-24 | 0.3.1 | foundation-author | Corpus-wide cleanup pass (no semantic changes). Migrated legacy architecture.md citation anchors to the §4.N map per the v0.2 NOTE: §1.1→§4.1 (×4 in §4.1.EM-001 four-axis tagging clause, §6.1 AxisTags/ModeTag reference, §6.1 Node.axes comment, and §9 cross-refs), §1.2→§4.2 (×2 in §4.2.EM-011 ZFC-tag obligation and §9 cross-refs), §1.3→§4.3 (×1 in §9 cross-refs), §1.5→§4.6 (×1 in §A.3 "Compilation loop" amendment-protocol informative block), §1.6→§4.8 (×1 in §6.1 Transition.actor_role comment), §1.8→§4.9 (×3 in §4.9 validator skip-path clause, §9 cross-refs, and §A.3 workflow-transactionality rationale), §1.9→§4.10 (×2 in §4.1.EM-001 three-artifact clause and §9 cross-refs). No requirement IDs, invariants, or schemas were touched. |
| 2026-04-24 | 0.3.2 | foundation-author | Corpus citation-drift cleanup pass 2: migrated legacy §N.N cross-spec anchors to current template §N.N form per the central remap table; ~45 citations fixed across the file. EV anchors: `§3.1→§4.1` (envelope), `§3.2→§8.1`/§6.3/§8 (event taxonomy vs payload registry per context), `§3.4→§4.4` (fsync), `§3.6→§4.5` (replay), `§3.7→§4.3` (bus/consumer). WM: `§5.1→§4.1` (worktree), `§5.8→§4.2` (branching), `§5.9→§4.9` (re-run rule), `§5.3→§4.7` (session log pipeline). ON: `§7.3→§4.3` (operator-control), `§7.5→§4.5` (N-1 compat), `§7.8→§4.8` (RTO), `§7.10→§4.10` (deferral). PL: `§8.1→§4.1` (daemon scope), `§8.2→§4.2` (startup sequence), `§8.5→§4.5` (agent subprocess), `§8.6→§4.6` (daemon vs orchestrator-agent distinction). Reconciliation path fix: `[reconciliation.md §N]` → `[reconciliation/spec.md §N]` (multi-file spec). CP: `§6.5→§6.3`/§6.4/§4.11 (policy YAML, grammar, skill surface per context), `§6.9→§4.5` (budget), `§6.11→§4.11` (skill discriminator). No requirement IDs, invariants, or schemas touched. |
| 2026-04-25 | 0.3.3 | foundation-author | Coordination patch wave landing two RC R2 cross-spec requests against EM. (1) Outcome-record extension for `reconciliation_verdict` envelope per RC-022a (resolves OQ-RC-010): chose approach (a) — discriminated variant — adding new EM-005a normative requirement, extending RECORD Outcome (§6.1) with `kind: OutcomeKind` (defaulting to `default`) and `payload: VerdictPayload | None` (cited to [reconciliation/schemas.md §6.1] VerdictEvent; not redeclared here), and adding ENUM OutcomeKind with values `{default, reconciliation_verdict}`. Approach (a) chosen over (b) because (i) it mirrors HC-008's existing `outcome_kind` discriminator on `outcome_emitted` so the daemon assigns `Outcome.kind` from the wire field without rewriting, (ii) it generalizes for future variants (improvement-loop, operator-CLI dispatch) under the amendment protocol, and (iii) it keeps the cascade and durability decision unchanged for `kind=default` outcomes — strictly additive at MVH. EM-005 expanded to enumerate the new fields; the `payload` is opaque to §4.10 cascade and §4.5.EM-023a durability decision (consumed only by the RC-025a verdict-executor). Unknown `kind` values route to Cat 6a per [reconciliation/spec.md §8.11]; no silent fallback. Existing v0.3.2 Outcome consumers remain conforming (default values mean no field reads change). (2) §6.2 trailer registry adds two RC-owned trailers per RC-002 / OQ-RC-002: `Harmonik-Workflow-Class` (Enum; conditional; values `{reconciliation}` at MVH; identifies reconciliation-workflow checkpoint commits for RC-002 dispatch dedup and RC-003b Cat 5 vs Cat 6a tiebreak; future workflow-class values extend via amendment protocol) and `Harmonik-Target-Run-ID` (UUID; conditional; the `run_id` being reconciled; coexists with the investigator-run's `Harmonik-Run-ID` on every reconciliation-workflow commit; RC-002a dedup keys on `(workflow_class, target_run_id)`). Both rows cite `reconciliation` as the owning spec; existing rows annotated with execution-model ownership for symmetry. The §6.2 informative note expanded to clarify that EM trailer-lint tooling and §4.4.EM-020a audit tooling MUST recognize the RC-owned trailers (any unknown trailer is a lint violation), and that the `Harmonik-Verdict-Executed` trailer remains RC-owned and not cross-listed per the EM v0.2.0 trailer-rollback decision. New requirement ID: EM-005a (assigned in the gap after EM-005; FROZEN per ID discipline). No IDs retired; no IDs renumbered. Schema additions are N-1 readable per §4.4.EM-022. Status remains `reviewed`. |
| 2026-05-12 | 0.4.0 | foundation-author | Workflow-modes kerf integration (C2). Introduces `workflow_mode ∈ {single, review-loop, dot}` as a first-class Run-record field (EM-012 amended; §6.1 Run RECORD adds `workflow_mode`; new ENUM `WorkflowMode`). New EM-012a declares the four-tier mode-resolution precedence (per-bead label → project config → daemon default → built-in `single`); tier-1 conflicts and unknown-mode labels treat tier 1 as absent and emit `bead_label_conflict` per [event-model.md §8.8.6]. New EM-015d describes `review-loop` as a hardcoded two-node sub-case of the workflow-graph model (`implementer → reviewer → {APPROVE: close, REQUEST_CHANGES: implementer, BLOCK: close-needs-attention, iteration-cap: close-needs-attention, no-progress: close-needs-attention}`), with the single-`run_id`-across-iterations rule, the same-Claude-session-resumed-across-implementer-iterations rule (via `claude --resume <claude_session_id>`), and per-iteration event emissions (`implementer_resumed`, `reviewer_launched`, `reviewer_verdict`, `review_loop_cycle_complete`). New EM-015e declares the hardcoded iteration cap of 3 at MVH, the four early-exit conditions (APPROVE / REQUEST_CHANGES-with-iterations-remaining / cap-hit / BLOCK / no-progress diff-hash), and routes cap-hit / BLOCK / no-progress to the `needs-attention` close path (operator-drained per [operator-nfr.md §4.3]). EM-012 amended to enumerate the four reserved `context` keys under `review-loop` (`iteration_count`, `last_verdict`, `claude_session_id`, `last_diff_hash`). §3 Glossary adds entries for `workflow_mode`, `claude_session_id` (with explicit disambiguation from harmonik's `session_id` event field), `iteration_count`, and `needs-attention`. §6.5 co-owned events updated to list the six new review-loop events and note that `run_started` / `run_completed` / `run_failed` payloads now carry `workflow_mode`. No prior requirement IDs renumbered or retired; the additions are strictly additive over v0.3.3. Status remains `reviewed`. |
| 2026-05-12 | 0.4.1 | foundation-author | Replace EM-015d's `claude -p ... --output-format json` post-launch capture mechanism for `claude_session_id` with the bridge-aligned pre-exec capture path: handler mints UUIDv7 and reports via `handler_capabilities` per [claude-hook-bridge.md §4.7 CHB-018] / [handler-contract.md §4.10 HC-045c]; daemon persists via the durable-checkpoint discipline of [claude-hook-bridge.md §4.6 CHB-023] (a §4.5.EM-023a-class transition completing BEFORE the handler is permitted to exec Claude). Glossary entry for `claude_session_id` updated to match. No requirement IDs added, renumbered, or retired. Status remains `reviewed`. |
| 2026-05-14 | 0.4.1 | agent (hk-7zvh4) | **Model-selection spec amendment: 4-tier model/effort resolution chain.** New **EM-012b** (§4.3) — model/effort resolution precedence, mirroring EM-012a's tier-list structure: tier 1 per-task bead labels (`model:<alias>`, `effort:<level>`), tier 2 per-project `.harmonik/config.yaml`, tier 3 per-agent-type compiled default, tier 4 empty built-in fallback. `model` and `effort` are resolved independently (each walks tiers separately). Tier-1 multi-label conflict and unrecognised `effort` value both treat tier 1 as absent and emit `bead_label_conflict`. Sealed into Run record as `ModelPreference` at claim time; value-opacity invariant stated (harmonik validates shape not value; handler-side launch failure is authoritative). **§6.1 Run RECORD** gains `model_preference : ModelPreference`. **ModelPreference RECORD** and **EffortLevel ENUM** added to §6.1. No prior requirement IDs renumbered or retired; strictly additive over v0.4.0. Refs: hk-7zvh4, hk-cfhj2. |
| 2026-05-31 | 0.8.2 | agent (hk-c5oxy) | **EM-012b tier-2.5 amendment: operator env-var default.** Inserts tier 2.5 between tier-2 (per-project config) and tier-3 (compiled defaults) in the EM-012b model/effort resolution chain. The new tier reads `HARMONIK_CLAUDE_MODEL` and `HARMONIK_CLAUDE_EFFORT` from the process environment at each claim-time tier walk (not daemon startup), providing hot-reload semantics for foreground sessions. Shape validation for `model` (same regex as tier-1) and closed-enum validation for `effort` apply; invalid values are silently skipped and the walk continues to tier 3. Both env-var names are exported constants in `internal/daemon/modelpreference.go`. Updated informative precedence summary table to add tier 2.5 row. No prior requirement IDs renumbered or retired; strictly additive. Refs: hk-c5oxy. |
| 2026-05-19 | 0.5.2 | agent (hk-vqoh2) | **EM-012b tier-2 schema formalization.** Expanded tier-2 entry in EM-012b (§4.3) with the normative `.harmonik/config.yaml` schema (`schema_version: 1`, `agents` map keyed by `core.AgentType` string), agent-type lookup semantics (unknown keys silently ignored, forward-compat), and explicit validation rules: file absent → zero-value; `schema_version ≠ 1` → `ErrUnsupportedConfigVersion` (daemon MUST refuse to start); malformed YAML → `ErrMalformedConfigYAML` (daemon MUST refuse to start); absent per-agent field → defer to tier 3 for that field only; empty file → absent semantics. Cites `LoadProjectConfig` in `internal/daemon/projectconfig.go` as the MVH reference implementation. No new requirement IDs. Refs: hk-vqoh2. |
| 2026-05-15 | 0.5.2 | agent (hk-4goy3) | **Working-tree refresh after merge: §4.12, EM-054.** Adds EM-054 in §4.12 specifying that after a successful `update-ref` + push, the daemon MUST run `git reset --hard HEAD` in the project root to resync the working tree. Documents uncommitted-changes policy (log warning, still refresh), refresh-failure policy (log warning, emit `working_tree_refresh_failed`, continue to CloseBead). §10.1 Core MVH conformance extended to EM-054; §10.2 adds test obligation for EM-054. New requirement ID: EM-054. No prior IDs renumbered or retired. Refs: hk-4goy3. |
| 2026-05-14 | 0.5.1 | agent (hk-ftyvo) | **Merge-to-main on success: §4.12, EM-052, EM-053.** Adds new §4.12 (Run-branch merge-to-main) with two normative requirements. EM-052: on both Step 9 success branches the daemon MUST fast-forward `refs/heads/main` to the run-branch tip, push origin main, emit `outcome_emitted{kind=approved}`, call `CloseBead`, emit `bead_closed`, then `run_completed`. EM-053: on non-FF or push failure, the daemon MUST emit `outcome_emitted{kind=rejected}`, call `ReopenBead`, emit `run_failed`, and NOT close the bead. §10.1 Core MVH conformance extended to EM-049 through EM-053; §10.2 adds test obligation for EM-052–EM-053. New requirement IDs: EM-052, EM-053. No prior IDs renumbered or retired. Refs: hk-ftyvo. |
| 2026-05-14 | 0.5.0 | foundation-author | extqueue v0.1: dispatch-loop pulls from execution queue (TS-1); EM-015f group-advance gate (TS-2); run_* payloads carry optional queue_id/queue_group_index (TS-3); §4.11 concurrency formalization (TS-4); §7.1 INFORMATIVE updated (TS-5); §7.4 pick_one MVH-era prose dropped (TS-6). §7.4 main-loop replaces `ready_beads → pick_one` block with a queue-pull block keyed on the active queue per [queue-model.md §3 QM-002]; daemon MUST NOT fall back to `br ready`. New EM-015f (§4.3) gates queue-group advance on all-terminal; `complete-success` advances the queue, `complete-with-failures` transitions the queue to `paused-by-failure` and emits `queue_paused`. EM-015a and EM-015b extended with optional `queue_id` and `queue_group_index` payload fields per [event-model.md §8.10] when the run originated from a queued dispatch. §6.1 Run RECORD gains optional `queue_id : QueueID | None` and `queue_group_index : QueueGroupIndex | None`; both are sealed at dispatch. New §4.11 (Concurrency) lands the previously code-only concurrency primitives as normative requirements: EM-049 (in-flight-run capacity gate at `max_concurrent`), EM-050 (claim-write serialization token-pool of size `max_concurrent`), EM-051 (`--max-concurrent` configuration, ≥ 1, default 1, sealed at startup). §7.1 INFORMATIVE clarifies that the queue-group state machine of [queue-model.md §5] layers above the per-run state machine; the two do not share transitions. MVH-era `pick_one` policy prose retired with the §7.4 replacement. §6.5 co-owned events lists the six queue-lifecycle event names per [event-model.md §8.10]. §2.2 declares queue-model out-of-scope; §9.3 adds queue-model and process-lifecycle §4.1 co-references. §10.1 Core MVH conformance extended to require EM-015f and EM-049 through EM-051; §10.2 adds test obligations for EM-015f and EM-049 — EM-051. New requirement IDs: EM-015f, EM-049, EM-050, EM-051. No prior IDs renumbered or retired. |

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
