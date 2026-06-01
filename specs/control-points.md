# Control Points

```yaml
---
title: Control Points
spec-id: control-points
requirement-prefix: CP
status: reviewed
spec-category: foundation-cross-cutting
spec-shape: requirements-first
version: 0.4.3
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-06-01
depends-on:
  - architecture
  - execution-model
  - event-model
  - handler-contract
---
```

## 1. Purpose

This spec defines the **ControlPoint** — a single unified primitive parameterized by `kind` into four kinds (`Gate`, `Hook`, `Guard`, `Budget`) — and the surrounding policy surface: role permissions, freedom profiles, the policy-expression grammar, the cognition-tagged evaluator pattern with persisted-verdict replay-safety, and the skill-declaration surface consumed by handlers. The ControlPoint registry is owned by S02 (Policy Engine); Gate and Guard invocation is owned by S01 (Orchestrator); Hook dispatch is owned by S05 (Hook System). The spec stops at the boundary of each owner — it defines *what* a ControlPoint is and *what* each owner may assume, not *how* each owner implements dispatch or invocation.

It is a separate spec from `execution-model.md` because control-points is the normative home of the policy surface that every other subsystem consumes by reference; folding it into execution-model would put policy grammar inside the run spine.

## 2. Scope

### 2.1 In scope

- The unified `ControlPoint` primitive and its `Kind` enum (`Gate`, `Hook`, `Guard`, `Budget`).
- Per-Kind semantics: trigger, evaluator input/output, outcome-action enum, and boundary-classification rule.
- ControlPoint registry: shape, owner (S02 Policy Engine), registration path, scope, idempotency.
- Ownership split: S02 owns registry; S01 owns Gate and Guard invocation; S05 owns Hook dispatch.
- Gate semantics (allow / deny / escalate-to-human; goal, approval, quality subtypes).
- Hook semantics (event match; side-effect composition; lifecycle types).
- Guard semantics (edge-reorder only; mechanism-only).
- Budget semantics (per role / per run / per state; dispatch-time enforcement; warning threshold; per-chunk accrual).
- Role permission schemas and freedom profiles.
- Policy expression grammar — `expr-lang/expr` adopted as the normative grammar.
- Cognition-tagged evaluator replay-safety contract via persisted verdict.
- Skill-declaration surface (node `required_skills`, role default skill sets).
- Policy YAML document required sections.
- Config-loading precedence for policy / role / freedom-profile / budget overrides.

### 2.2 Out of scope

- Hook **dispatch loop** mechanics (subscription fan-out, failure-isolation, ordering engine) — owned by the S05 subsystem spec per §4.10.
- Gate and Guard **invocation mechanics** inside the edge cascade — owned by [execution-model.md §4.10] and the S01 subsystem spec per §4.10.
- Reconciliation wall-clock budget integration — reconciliation.md §4.4 owns the per-reconciliation outer bound; this spec owns the Budget primitive only.
- Policy document schema evolution (field-level migration paths) — deferred per OQ-CP-001.
- Skill storage layout and injection mechanism — owned by [handler-contract.md §4.11]; this spec owns only the *declaration* surface.
- Role names, MVH-required vs. deferred role list, and role semantics — owned by [architecture.md §4.8]; this spec owns permission schemas keyed by those role names.
- Event payload shapes for control-point-emitted events — owned by [event-model.md §6.3]; this spec owns the emission WHEN.
- The DOT workflow grammar itself — owned by [execution-model.md §4.1].

## 3. Glossary

- **ControlPoint** — a single typed primitive parameterized by `kind` that unifies the four surfaces listed below. (see §4.1)
- **Kind** — one of `Gate`, `Hook`, `Guard`, `Budget`. Each kind has its own trigger, evaluator shape, and outcome-action enum. (see §4.1)
- **Gate** — a ControlPoint that fires on a transition attempt and returns `allow`, `deny`, or `escalate-to-human`. (see §4.2)
- **Hook** — a ControlPoint that fires on event match and produces side-effects; never halts a run. (see §4.3)
- **Guard** — a ControlPoint that fires during edge evaluation and reorders the candidate edge list; cannot add, remove, or block edges. (see §4.4)
- **Budget** — a ControlPoint that declares a consumable allowance (tokens, wall-clock, iterations) and fires on accrual, warning-threshold, and exhaustion. (see §4.5)
- **Evaluator** — the function invoked by a ControlPoint when its trigger fires; mechanism-tagged (deterministic expression) or cognition-tagged (model delegation). (see §4.1, §4.8)
- **Freedom profile** — a per-state constraint bundle naming tool whitelist, directory write access, model tier, budgets, and max iterations. (see §4.6)
- **Policy expression** — a boolean or scalar expression evaluated against run context, outcome, and event; grammar fixed at `expr-lang/expr`. (see §4.7)
- **Policy document** — a declarative YAML document declaring metadata, roles, freedom profiles, gates, and budgets. (see §4.7)
- **Persisted verdict** — the durably-recorded output of a cognition-tagged evaluator, used during replay to avoid re-invoking the model. (see §4.8)
- **Skill declaration** — a node- or role-level list of skill names the handler MUST provision at agent launch. (see §4.11)
- **Registry** — the in-process table of registered ControlPoint instances, keyed by name, owned by S02. (see §4.9)

## 4. Normative requirements

### 4.1 ControlPoint as a unified primitive

#### CP-001 — ControlPoint is a single typed primitive

A `ControlPoint` MUST be a single typed record (one Go struct, one lifecycle, one registration path) parameterized by `kind ∈ {Gate, Hook, Guard, Budget}`. Common fields: `name` (unique within a daemon registry), `kind`, `trigger`, `evaluator`, `outcome_action`, plus a Kind-specific typed payload per §4.2–§4.5. The unification is in the primitive's shape; the semantics per Kind are explicit and NOT interchangeable.

Tags: mechanism

#### CP-002 — ControlPoint name is unique within the registry

Every ControlPoint MUST carry a unique `name` (String) that identifies it in the §4.9 registry. Policy YAML documents reference ControlPoints by `name`; DOT workflow attributes (`gate_ref`, `policy_ref`, `freedom_profile_ref`, `budget_ref` per [execution-model.md §4.2]) resolve to registered names.

Tags: mechanism

#### CP-003 — Evaluator is boundary-classified

Every ControlPoint's `evaluator` MUST be classified as `mechanism` or `cognition` per [architecture.md §4.1]. A mechanism-tagged evaluator is a pure deterministic expression (condition, schema match, threshold check) over its declared input. A cognition-tagged evaluator delegates to a model with a specified prompt, input shape, and response schema per §4.8.

Tags: mechanism

#### CP-004 — Per-Kind semantics are fixed

The four kinds' triggers, evaluator inputs, evaluator return types, outcome-action enums, and boundary-classification rules MUST match the table in §4.1.CP-005. No ControlPoint may deviate from its Kind's row.

Tags: mechanism

#### CP-005 — Per-Kind semantics table

| Kind | Trigger | Evaluator input | Evaluator returns | Outcome-action enum | Boundary rule |
|---|---|---|---|---|---|
| **Gate** | Transition attempt (pre-selection consumes outcome; post-selection consumes the chosen edge) | Current state, candidate transition, outcome | `{allow, deny}` + optional `reason` | `{allow, deny, escalate-to-human}` | Mechanism OR cognition |
| **Hook** | Event match (see §4.3 for lifecycle event types) | Matching event + subscription context | Side-effect descriptor (event emission, state mutation, external action) | `{fire-side-effect, no-op}` (a Hook never halts a run) | Mechanism OR cognition |
| **Guard** | Edge evaluation (during the deterministic cascade of [execution-model.md §4.10]) | Edge set, current state, outcome | Reordered edge list (subset or permutation of input edges) | `{reorder-edges}` (Guards produce no other action) | Mechanism only |
| **Budget** | Dispatch attempt; per-chunk accrual; threshold cross | Counter state, proposed dispatch cost, per-chunk delta | Verdict: `{admit, warn, deny}` | `{admit, warn, deny}` | Mechanism only |

Tags: mechanism

> INFORMATIVE: `Budget` is a distinct Kind rather than a sub-category of Gate because its trigger (dispatch + per-chunk accrual) and its evaluator input (counter state, not a candidate transition) do not fit a Gate's transition-attempt trigger. The unification holds at the primitive level (one struct, one registry, one lifecycle) without forcing Budget into a Gate's shape.

### 4.2 Gate semantics

#### CP-006 — Gate fires on transition attempt

A `Gate` MUST fire on a transition attempt. The evaluator receives the current state, the candidate transition (after the edge cascade selects it or before, per attach-point), and the handler outcome that led to the attempt. The evaluator returns `allow` or `deny` with an optional human-readable `reason`; the outcome-action MAY be elevated to `escalate-to-human` by policy when the evaluator's `deny` is configured to escalate.

Tags: mechanism

#### CP-007 — Gate attach points

A Gate MAY be attached to nodes (pre-entry, post-exit) or to edges (before-selection, after-selection). The attach point MUST be declared on the Gate record; a Gate without a declared attach point fails registration. When multiple Gates are registered at the same attach point, the S01 invocation layer MUST honor declaration order and MUST short-circuit on the first non-`allow` verdict (declared-order + first-non-allow is a record-side ordering property consumed by S01 per [execution-model.md §4.10], not an S01-internal choice).

Tags: mechanism

#### CP-008 — Gate subtypes declare intent

A Gate MUST declare a `subtype ∈ {goal-gate, approval-gate, quality-gate}`. `goal-gate` expresses a policy-level goal assertion that cannot be bypassed by the run; `approval-gate` requires a named approver (human role or agent role per [architecture.md §4.8]); `quality-gate` requires a prior verification node's outcome to satisfy a policy expression. Subtype is informative for operators; enforcement uses the evaluator and outcome-action fields only.

Tags: mechanism

#### CP-009 — Gate denial leaves the run in the source state

On evaluator `deny`, the run MUST remain in the source state; the transition MUST NOT advance. A `gate_denied` event is emitted per [event-model.md §8.2] with payload `{run_id, gate_name, reason}`. Retry policy (if any) is governed by the node's policy per [execution-model.md §4.2], not by the Gate record.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### CP-010 — Gate invocation is owned by S01

Gate invocation during the transition cascade MUST be performed by S01 (Orchestrator Core) consulting the §4.9 registry. This spec defines the Gate record and its outcome semantics; it does NOT define the invocation mechanics — those belong to the S01 subsystem spec and are constrained by [execution-model.md §4.10].

Tags: mechanism

#### CP-011 — Gate evaluator MAY be cognition-tagged

A Gate's evaluator MAY be cognition-tagged (delegating to a model) when the policy requires judgment that a mechanism-tagged expression cannot express. Cognition-tagged Gate evaluators MUST satisfy the replay-safety contract of §4.8 (persisted-verdict).

Tags: cognition
Axes: llm-freedom=bounded; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

### 4.3 Hook semantics

#### CP-012 — Hook fires on event match

A `Hook` MUST fire when a subscribed event matches its `trigger` (an event-name match, optionally with a subscription filter over the event payload). The evaluator receives the matched event and subscription context; the evaluator returns a side-effect descriptor declaring one of: (a) emit a new event, (b) apply a declared state mutation (via a mechanism-tagged helper, never direct write), (c) perform an external action (via a handler-owned effector). Hooks MUST NOT block, halt, or alter the run's transition progression.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### CP-013 — Hook lifecycle event types

A Hook's `trigger` MUST match one of the declared lifecycle event types. The MVH-baseline Hook-trigger set MUST include: `on_agent_started`, `on_agent_output`, `on_agent_completed`, `on_timeout`, `on_review_required`, `on_transition_attempted`, `on_checkpoint_written`, `on_checkpoint_failed`. Hook-trigger names form a separate Hook-namespace that maps to [event-model.md §8] event types (e.g., `on_agent_started` subscribes to the `agent_started` event; `on_checkpoint_written` subscribes to the `checkpoint_written` event per [operator-nfr.md §4.5]); the `on_` prefix distinguishes Hook-subscription names from raw event-type names. Subsystems MAY declare additional Hook trigger types via the subsystem envelope per [architecture.md §4.4]; declared triggers are registered at daemon init. An unrecognized trigger fails registration.

Tags: mechanism

#### CP-014 — Hook ordering is deterministic

When multiple Hooks match a single event, they MUST execute in an order determined by (a) declaring-subsystem priority (explicit integer declared by the subsystem envelope), (b) within a subsystem, declaration order. The ordering is deterministic and reproducible across daemon restarts given identical registered Hooks.

Tags: mechanism

#### CP-015 — Hook failures are typed and do not halt the chain

A Hook evaluator MUST return a typed failure descriptor on error; per-Hook failure MUST NOT halt the Hook chain unless the Hook record declares `halt_on_failure = true`. Unhandled errors at the evaluator boundary are wrapped by the S05 dispatcher per [handler-contract.md §4.5]: timeout and resource-exhaustion failures map to `ErrTransient`; schema-violation, type-check, and registration-state failures map to `ErrDeterministic`. The mapping rule is fixed in the S05 subsystem spec; this spec names the two error-class surface as the only legal wrap targets.

Tags: mechanism

#### CP-016 — Hook dispatch is owned by S05; delivery is at-least-once

Hook dispatch (subscribing to the event bus, ordering, side-effect application, failure-isolation) MUST be performed by S05 (Hook System) consulting the §4.9 registry. This spec defines the Hook record and its outcome semantics; it does NOT define the dispatch loop — that belongs to the S05 subsystem spec and [event-model.md §4.3] (consumer taxonomy) and [event-model.md §6.3] (event shapes).

S05 MUST deliver each Hook's side-effect **at least once**. Duplicate delivery is acceptable for side-effects declared as `idempotency_class = idempotent` on the Hook's `SideEffect` record (§6.1.6); for `idempotency_class = non-idempotent`, S05 MUST bound delivery to at-most-once using a persisted delivery-receipt mechanism owned by the S05 subsystem spec. This spec names the at-least-once floor and the `idempotency_class` declaration; S05 owns the receipt mechanism.

Tags: mechanism

#### CP-017 — Hook evaluator MAY be cognition-tagged

A Hook's evaluator MAY be cognition-tagged (e.g., an `on_review_required` Hook that delegates to a reviewer agent). Cognition-tagged Hook evaluators MUST satisfy the replay-safety contract of §4.8. The delegation path — role (from [architecture.md §4.8]), model class, input shape, response schema — MUST be named on the Hook record.

Tags: cognition
Axes: llm-freedom=bounded; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

### 4.4 Guard semantics

#### CP-018 — Guard reorders edges only

A `Guard` MUST fire during edge evaluation of the deterministic cascade defined in [execution-model.md §4.10]. The evaluator receives the candidate edge set, current state, and outcome; the evaluator returns a reordered edge list that is a subset or permutation of the input. A Guard MUST NOT add edges not present in the input, remove edges, or block a transition. Gate semantics (deny / escalate) are NOT available to Guards.

Tags: mechanism

#### CP-019 — Guard precedes the cascade

In the edge-selection cascade of [execution-model.md §4.10 EM-042], Guards MUST run BEFORE the condition cascade. The cascade then operates on the Guard-reordered edge list as its input; subsequent precedence rules (condition match, preferred_label, suggested_next_ids, weight, ordering_key) are applied in the order defined by the execution-model spec.

Tags: mechanism

#### CP-020 — Guard MUST be mechanism-tagged

A Guard's evaluator MUST be mechanism-tagged. Cognition-tagged Guards are forbidden: they would place cognition inside the selection-logic layer, violating ZFC (the Zone of Forbidden Cognition) per [architecture.md §4.2]. Any Guard declaration carrying a cognition-tagged evaluator fails registration.

Tags: mechanism

#### CP-021 — Guard invocation is owned by S01

Guard invocation during the cascade MUST be performed by S01 (Orchestrator Core) consulting the §4.9 registry. This spec defines the Guard record and its outcome semantics; invocation is S01's obligation under [execution-model.md §4.10].

Tags: mechanism

### 4.5 Budget semantics

#### CP-022 — Budget declares a typed, scoped allowance

A `Budget` MUST declare: (a) `resource ∈ {tokens, wall_clock_seconds, iterations}`, (b) `scope ∈ {per_role, per_run, per_state, handler_account}`, (c) `limit` (positive integer), (d) `warning_threshold` (ratio in [0, 1]). Unless overridden per §4.7 config precedence, `warning_threshold` MUST default to 0.8. Multiple Budgets MAY apply to a single agent run; the tightest applicable Budget wins on any accrual check (tightest = smaller integer `limit` for the same `resource`).

> INFORMATIVE: a `handler_account`-scoped Budget represents a per-handler-account ceiling (a session-token cap or a daily quota) rather than a per-role/per-run/per-state allowance. Its exhaustion is handler-fatal per [handler-pause.md §4 HP-012] — the handler type is paused immediately, no hysteresis — distinct from `per_run` exhaustion, which is per-bead and MUST NOT trip a handler pause. NOTE on scope-as-classifier vs. registered Budget: the harmonik unified per-day spend cap of [cognition-loop.md §4.11 CL-090] is NOT a registered CP-022 `Budget` primitive instance — it meters cumulative USD/day, which is not a representable `BudgetResource` (the enum is `{tokens, wall_clock_seconds, iterations}`). Rather, the cognition-loop meter is a cognition-loop-side mechanism that, on exhaustion, emits an account-scoped `budget_exhausted` event whose `scope` field carries the value `handler_account` ([cognition-loop.md CL-090], [event-model.md §8.4.3]). This `scope`-value is what HP-012 reads to discriminate the account-scoped (handler-fatal) exhaustion from the per-run variant; HP-012 needs only the scope-value to discriminate, not a registered Budget. The `budget_scope = handler-account` wording used in [handler-pause.md HP-012] denotes this `scope` field carrying value `handler_account`; there is one field (`scope`), not a parallel `budget_scope` field.

Tags: mechanism

#### CP-023 — Budget is enforced at dispatch

The agent runner MUST check the Budget's remaining allowance AT DISPATCH (pre-exhaustion). If the pending dispatch would exceed the remaining limit, the runner MUST emit a `budget_exhausted` event per [event-model.md §8.4] and DENY the dispatch (the handler is NOT launched). The run's failure class MUST be `budget_exhausted` per [execution-model.md §8.5].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### CP-024 — Budget accrual is per-chunk

Every agent-output chunk MUST emit a `budget_accrual` event within the same handler tick that produces the chunk (bounded by the handler's chunk-emission cadence per [handler-contract.md §4.2]). Per-chunk granularity is explicitly retained for MVH per [event-model.md §8.9]; a future log-level filter MAY suppress chunk events at consumer boundaries without changing the emission contract.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### CP-025 — Budget warning threshold fires at 80% by default

When cumulative accrual crosses the `warning_threshold` fraction of `limit`, the runner MUST emit a `budget_warning` event per [event-model.md §8.4] and continue. The threshold check uses live in-handler counters (the handler tracks accrual against remaining budget in real time per its own tick cadence). The threshold value is governed by §4.5.CP-022 (default 0.8, operator-overridable per §4.7).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### CP-026 — Budget counter state is internal; observable only via events

The Budget counter state MUST be internal to the handler. It MUST NOT be written to any durable store other than through the typed `budget_accrual`, `budget_warning`, and `budget_exhausted` events. Cross-subsystem reads of the counter MUST go through the event bus per [event-model.md §4.3]; there is no `GetBudgetCounter()` surface.

Tags: mechanism

#### CP-026a — Budget counters are rehydrated from JSONL `budget_accrual` event replay on daemon restart

On daemon restart with one or more in-flight runs, Budget counters MUST be rehydrated by replaying `budget_accrual` events from the JSONL event log per [event-model.md §4.4] durability contract. Replay begins at the `run_started` event for each in-flight run and consumes every subsequent `budget_accrual` event for that run, summing per-Budget deltas to reconstruct the pre-crash counter state. JSONL event replay is the sole authoritative rehydration source; no other durable store (git checkpoint, snapshot file, operator-supplied counter) is a legal rehydration source. This follows from CP-026 (the event stream is the only durable store the counter reads from).

Implementations MUST NOT start an in-flight run's handler with a zero counter when prior `budget_accrual` events exist in the JSONL log for that run; doing so would double-spend the already-accrued allowance. Rehydration is complete before the handler accepts any dispatch per §4.5.CP-023.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### CP-027 — Reconciliation wall-clock budget is an outer bound

Reconciliation workflows carry a mandatory wall-clock Budget per [reconciliation/spec.md §4.4 RC-017]. That budget MUST register as a Budget instance with `resource = wall_clock_seconds`, `scope = per_run`, `scope_target = <reconciliation_run_id>`. The outer-bound enforcement mechanism lives in [reconciliation/spec.md §4.4]; this spec contributes the normative composition rule: when a reconciliation wall-clock Budget is active for a run, inner per-role or per-state Budgets MUST NOT extend the effective wall-clock beyond the outer bound. On any conflict between an inner Budget limit and the outer wall-clock Budget remaining, the outer Budget wins (a dispatch admissible under the inner Budget but not the outer's remaining allowance is DENIED with failure class `budget_exhausted` per [execution-model.md §8.5]).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.6 Role permissions and freedom profiles

#### CP-028 — Role permission schema

Every role declared in a policy document MUST carry a `permission_schema` with fields: `allowed_tools` (list of tool names), `writable_paths` (list of workspace-relative globs), `readable_paths` (list of workspace-relative globs; defaults to `["**"]`), `model_tier` (optional string), `default_skills` (list of skill names; see §4.11), `allowed_hooks` (list of Hook `name`s that may modify this role's behavior), and `invocable_by` (list of agent roles that may spawn this role). Role name resolution and semantics are owned by [architecture.md §4.8].

Tags: mechanism

#### CP-029 — MVH-required roles carry concrete default permission sets

For each MVH-required role (Planner, Builder, Reviewer per [architecture.md §4.8]), the policy layer MUST supply a concrete default permission set (read-only vs. write to specific directories, tool whitelist, `default_skills`, spawn-source roles, Hooks that may modify behavior). Default sets are shipped at harmonik init and are overridable by higher-precedence layers per §4.7.

Tags: mechanism

#### CP-030 — Declared-but-deferred roles carry empty shells

For each declared-but-deferred role (Researcher, Verifier, Scheduler, Governor per [architecture.md §4.8]), the policy layer MUST supply a permission shell with `allowed_tools = []`, `writable_paths = []`, and `default_skills = []`. Activating a deferred role in MVH requires a foundation amendment per [architecture.md §4.6]; shell declarations are activation-time-filled.

Tags: mechanism

#### CP-031 — Default skills include the Beads-CLI skill

Every MVH-required role's `default_skills` MUST include the Beads-CLI skill per [docs/foundation/components.md §10.9]. This is a concrete instance of the general skill-injection pattern in §4.11 and [handler-contract.md §4.11]; roles MAY declare additional defaults; nodes MAY declare additional `required_skills` per [execution-model.md §4.2].

Tags: mechanism

#### CP-032 — Freedom profile is a per-state constraint bundle

A `FreedomProfile` MUST carry: `tool_whitelist` (list of tool names; intersects with role's `allowed_tools`), `writable_paths` (intersects with role's `writable_paths`), `model_tier` (optional), `token_budget` (Budget name or inline), `wall_clock_budget` (Budget name or inline), `max_iterations` (positive integer). Freedom profiles compose as agents traverse states by **intersection** per §4.6.CP-033: the tightest applicable profile wins on every field.

Tags: mechanism

#### CP-033 — Freedom-profile tightest-wins semantics

When multiple freedom profiles apply to a state (e.g., a role-default profile and a node-level `freedom_profile_ref`), the effective profile is the per-field intersection: for list-valued fields, set intersection; for integer-valued fields, the smaller value; for `model_tier`, the less-capable tier per the harmonik-level ordering declared in the `_registry.yaml` tier table (MVH ordering: `haiku < sonnet < opus`, where `<` means "less capable"). Enums without a declared ordering are not composable by tightest-wins; both layers MUST declare compatible values or registration fails. "Tightest wins" is deterministic and mechanism-tagged.

Tags: mechanism

### 4.7 Policy expression grammar and policy documents

#### CP-034 — Policy expressions use the `expr-lang/expr` grammar

The policy-expression grammar MUST be the grammar defined by `expr-lang/expr` (`<https://github.com/expr-lang/expr>`). Expressions are evaluated against a typed environment: `run` (the current Run record per [execution-model.md §4.3]), `outcome` (the Outcome record per [execution-model.md §4.1]), `event` (the matched event record when evaluated inside a Hook; nil otherwise), `context` (the run's shared context map), and `policy_meta` (the policy document's `metadata` block). Expressions MUST be side-effect-free.

Tags: mechanism

> INFORMATIVE: The adoption of `expr-lang/expr` is deliberate: it is a maintained Go-native expression language with a known grammar and a published safety model (no I/O, no loops by default, bounded evaluation cost). Alternatives (writing a custom grammar; embedding a general-purpose language; `cel-go`) were considered; see §A.3 for the cel-go comparison.

#### CP-034b — Policy expression evaluation MUST be bound by a harmonik-level cost ceiling

Policy-expression evaluation MUST be performed with a harmonik-level cost ceiling that is NOT exposed as a policy-level knob. Two bounds MUST be set by the MVH implementation:

1. **Primary bound — AST step count (deterministic).** A per-evaluation AST-visit count ceiling. This is the normative bound; it is deterministic across runtime versions and host-clock speed. Where `expr-lang/expr` does not ship a step-counter, implementations MUST wrap the evaluator to count AST visits and abort on ceiling cross. `expr.MaxNodes(...)` (a compile-time ceiling on the AST's SIZE) is a complementary static bound but does NOT substitute for the step counter.
2. **Secondary bound — wall-clock soft-cap (best-effort).** `expr.Timeout(...)` MUST also be set as a safety net for evaluators that bypass or under-count the primary bound. Wall-clock is non-deterministic across runtime / host-clock speed; it is a backstop, not a peer.

A pathological expression that would otherwise stall evaluation MUST abort with a typed `ErrDeterministic` per [handler-contract.md §4.5]. The abort and the accompanying event emission are a durability pair: the `policy_expression_exceeded_cost` event MUST be emitted to the event bus and reach JSONL durability per [event-model.md §4.4] BEFORE the evaluator wrapper returns control to its caller. On a crash between abort and event durability, the replayer MUST treat absence of the event as unresolved — the replay must re-run the evaluator, rely on the cost ceiling to re-abort, and emit the event on the replay.

The event payload MUST carry a `bound_fired` discriminator valued in `{ast_steps, wall_clock}` identifying which bound triggered the abort. Operators diagnosing cost-ceiling crossings depend on this discriminator; re-adding it post-MVH is a breaking event-payload change.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

> NOTE: The `io-determinism=deterministic` axis above applies ONLY when the AST-step-count bound fired (`bound_fired=ast_steps`). A `bound_fired=wall_clock` abort is tagged `io-determinism=best-effort` on the event record itself — a wall-clock abort can produce different verdicts on a slow CI runner versus a fast dev box and is documented as the non-deterministic backstop. Implementations MUST include the per-abort io-determinism tag in the event payload.

#### CP-035 — Policy YAML document required sections

A policy YAML document MUST contain sections: `metadata` (with `name`, `version`, `author`, `schema_version`), `roles[]` (role permission schemas per §4.6), `freedom_profiles[]` (per §4.6), `gates[]` (Gate definitions referenceable by DOT `gate_ref`), `hooks[]` (Hook definitions), `guards[]` (Guard definitions), `budgets[]` (Budget declarations per §4.5). Missing any required section is a validation error detected at registration time.

Tags: mechanism

#### CP-036 — DOT attributes reference policy YAML by name

DOT node and edge attributes `policy_ref`, `gate_ref`, `freedom_profile_ref`, and `budget_ref` (per [execution-model.md §4.2]) MUST carry the `name` of a policy element registered per §4.9. DOT attributes MUST NOT embed policy bodies inline. This preserves the three-artifact separation per [architecture.md §4.10] (DOT is the graph; YAML is the policy; each is a separate artifact).

Tags: mechanism

#### CP-037 — Config-loading precedence

Policy-source precedence is (highest first): (1) runtime override set by operator before launch, (2) operator-policy file (persistent operator preferences), (3) workflow definition (per-workflow overrides), (4) default configuration shipped with harmonik. Resolution is deep-merge with higher-precedence values replacing lower-precedence. A change to a higher-precedence layer takes effect on the next operator pause per [operator-nfr.md §4.3] — there are NO mid-run policy reloads.

Tags: mechanism

#### CP-038 — Policy schema version is per-document and N-1 readable

Every policy YAML document MUST carry a `schema_version` integer in its `metadata` block. Readers MUST accept N-1 (the immediately prior schema version) per [operator-nfr.md §4.5]. Breaking changes require a migration release.

Tags: mechanism

#### CP-038a — Mechanism-tagged Gate evaluators participate in the persisted-verdict envelope-hash discipline

A mechanism-tagged Gate evaluator's verdict is NOT persisted under §4.8.CP-040 (which applies to cognition-tagged evaluators only). However, when a mechanism-tagged Gate's evaluator expression text, its policy document's `schema_version`, or its workflow-ingest reachability set (per §4.8.CP-040a item 4) changes between the run's original evaluation and a replay attempt, the replay path MUST detect the drift and emit a `gate_definition_drift` event per §6.5 carrying the prior and current values for the changed inputs.

Drift detection for mechanism-tagged Gates uses the same envelope inputs declared in §4.8.CP-040a items 1, 4, and 5 (expression text; reachable `context_subset`; `policy_meta` at registered `schema_version`); items 2 and 3 (prompt template, skill packages) do not apply to mechanism-tagged evaluators and are absent from the envelope. The hash algorithm is SHA-256 over the canonical JSON of the three applicable inputs in declared order, mirroring CP-040a.

On detected drift, the replay path MUST NOT silently re-evaluate using the new expression text. The run's Cat 6 reconciliation handler per [reconciliation/spec.md §4.2] is the only authority that can authorize re-evaluation under the new definition; mirroring CP-INV-003's escalation rule for cognition-tagged evaluators. A mechanism re-evaluation under a Cat 6 verdict MUST emit a `gate_redefined_under_cat_6` event carrying the prior decision (read from the JSONL `gate_allowed` / `gate_denied` event for the original transition) and the new decision.

This requirement closes pass-4 design follow-up #5 (the schema-version-bump documentation for the EM-006 5→4 collapse). The collapse itself is a one-shot migration (no v1 corpus); the ongoing drift-detection discipline for mechanism-tagged Gates is what CP-038a normalizes.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.8 Cognition-tagged evaluator replay-safety

#### CP-039 — Cognition-tagged evaluators name the delegation path

A cognition-tagged evaluator (Gate per §4.2, Hook per §4.3) MUST name its delegation path explicitly on the ControlPoint record: the invoked role (from [architecture.md §4.8]), the model class (e.g., `reviewer-tier-1`), the input shape (a declared input schema), and the response schema (a declared output schema). A reviewer verifies the path at registration; unnamed paths fail registration.

Tags: cognition
Axes: llm-freedom=bounded; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### CP-040 — Cognition-tagged evaluator verdict is persisted

Every cognition-tagged evaluator's verdict MUST be persisted to the run's durable trace at invocation time as a `GateVerdictRecord` (Gate) or `HookVerdictRecord` (Hook) per §6.1.6. For Gates, the record is written into the Transition record's `evidence` field per [execution-model.md §4.1] (keyed by `gate_name`) BEFORE the transition advances. For Hooks, the record is written to the path `.harmonik/hooks/<run_id>/<hook_invocation_id>.json` on the run's task branch per [workspace-model.md §4.2] and emitted as a `hook_verdict_persisted` event per [event-model.md §8.2].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### CP-040a — Persisted verdict carries an input-envelope hash

Every persisted cognition-tagged verdict record MUST include an `input_envelope_hash` field computed deterministically over the evaluator's resolved input envelope. The canonical envelope input set is:

1. `expression_text` — the full source text of the evaluator's mechanism-tagged expression when present (nil for pure-cognition evaluators with no expression).
2. `prompt_template` — the resolved prompt template body at the `prompt_template_ref` version pinned on the ControlPoint record (not the template ref, the resolved body).
3. `skill_packages` — the set of skill-package identifiers and versions snapshotted from the originating handler launch's `skills_provisioned` event (§4.11.CP-050) payload. The replayer MUST read `skills_provisioned` event payload as the source of truth for "what versions were provisioned at the originating launch"; it MUST NOT read current skill-package versions from disk.
4. `context_subset` — the subset of `run.context` **reachable from the expression or prompt template via static AST walk**. AST-walk reachability is the default mechanism: workflow-ingest (§4.7.CP-035) type-checks every mechanism-tagged expression against the §6.4 environment and emits a reachable-path set as a first-class by-product; the persister consumes that set to compute the subset. For cognition-tagged evaluators whose prompt template's templating grammar is not AST-walkable by this implementation, the conservative fallback is the **whole `run.context` map** — any context change busts the hash and escalates to Cat 6 per §4.8.CP-041. Implementations MUST declare which of the two modes they apply; mixing is forbidden within a single ControlPoint.
5. `policy_meta` — the `metadata` block of the policy document that declared the ControlPoint, at its registered `schema_version`.

The hash algorithm MUST be SHA-256 over a canonicalized JSON serialization of the five inputs above in the declared order. The hash is co-stored with the verdict. The AST-walker reachability output is part of the replay-safety trust base: a bug there silently changes the hash surface. The AST walker's output schema (the reachable-path set) MUST be named in the workflow-ingest pass and MUST be stable across runtime versions for a given expression text; non-determinism in the walker is a structural bug.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### CP-041 — Replay consumes the persisted verdict only when the envelope hash matches

On replay or reconciliation-driven resume (per [reconciliation/spec.md §4.5]), a cognition-tagged evaluator MUST NOT be re-invoked when a persisted verdict exists for the same `{control_point_name, run_id, transition_id | event_id}` key AND the `input_envelope_hash` recomputed from the current envelope matches the stored hash. On envelope-hash match, the replayer MUST read the persisted verdict from git and proceed. On envelope-hash MISMATCH, the replayer MUST NOT silently re-invoke the model; the replayer MUST emit a `verdict_envelope_mismatch` event and escalate to a Cat 6 reconciliation verdict path per [reconciliation/spec.md §4.2], which is the only authority that can authorize re-invocation.

The §7.2 pseudocode is the **sole** invocation path for cognition-tagged evaluators across **all Kinds** (Gates per §4.2 and Hooks per §4.3). When replaying a Hook dispatch chain across a crash, the S05 dispatcher MUST consult the persisted HookVerdictRecord (§4.8.CP-040, persisted at `.harmonik/hooks/<run_id>/<hook_invocation_id>.json`) via §7.2 BEFORE re-dispatching the Hook to the evaluator. CP-012 is refined by this requirement: a cognition-tagged Hook's evaluator is invoked directly ONLY on the original launch; every subsequent invocation (replay, reconciliation resume, daemon restart) routes through §7.2 and returns the persisted verdict on hash match, or escalates to Cat 6 on mismatch.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### CP-042 — Verdict persistence is the boundary between mechanism and cognition

The persistence of the verdict is mechanism-tagged; the production of the verdict is cognition-tagged. The two MUST split into separate operations per [architecture.md §4.1]: the cognition-tagged evaluator produces the verdict record (idempotency=idempotent, since an LLM is called freshly); the mechanism-tagged persister writes it to git (idempotency=non-idempotent). This split is the concrete application of the single-tag rule per the spec-template §4.N+1.

Tags: mechanism

### 4.9 ControlPoint registration

#### CP-043 — Registry is a single in-process table owned by S02

The ControlPoint registry MUST be a single in-process table (Go map keyed by `name`) owned by S02 (Policy Engine). S02 constructs ControlPoint instances by reading policy YAML per §4.7 and calling the registration surface defined in §4.9.CP-044. The registry is the single source of truth for registered ControlPoints within a daemon.

Tags: mechanism

#### CP-044 — Registration surface

S02 MUST expose the Registry interface declared in §6.1.7. Registration is **re-registration-safe on identical body**: registering a ControlPoint with an already-registered `name` and an identical body succeeds silently; registering a different body under an existing `name` MUST fail at startup with a specific error code. The "body" of a ControlPoint for equality purposes is the tuple `(kind, trigger, evaluator, payload)`; `name`, `axes`, and `schema_version` are NOT part of the body (a schema-version bump on an otherwise-identical ControlPoint MUST NOT reject as divergent). Body equality is computed over a canonicalized serialization (field-order-independent, whitespace-normalized). Subsystems register ControlPoints during daemon init via their subsystem envelope per [architecture.md §4.4].

Tags: mechanism

#### CP-045 — Registry scope is daemon-local

The registry MUST be daemon-scoped (process-scoped per [architecture.md §4.4]). Cross-daemon sharing MUST NOT occur; every per-project daemon maintains its own independent registry. Daemon restart rebuilds the registry from policy YAML as a startup step.

Tags: mechanism

#### CP-046 — Registry lookups are deterministic

All registry lookups declared in §6.1.7 (`LookupByName`, `LookupByTrigger`, `LookupByAttachPoint`) MUST be deterministic: given an identical registry state, identical query inputs produce identical outputs. List-returning lookups MUST apply a total ordering (by `name` ascending) before returning so iteration order is reproducible across Go runtime versions. The registry MUST NOT incorporate nondeterministic inputs (wall-clock time, PID, map iteration order exposed to callers).

Tags: mechanism

### 4.10 Ownership split

#### CP-047 — S02 owns the registry; S05 owns Hook dispatch; S01 owns Gate and Guard invocation

S02 (Policy Engine) owns the §4.9 registry and the registration path. S05 (Hook System) owns the Hook dispatch loop (subscribing to the event bus, ordering Hooks per §4.3.CP-014, applying side-effects, isolating failures); S05 consults the registry but does NOT own it. S01 (Orchestrator Core) owns Gate and Guard invocation during the edge cascade (per [execution-model.md §4.10]); S01 consults the registry but does NOT own it. No other subsystem may invoke a ControlPoint except through these three owner paths.

Tags: mechanism

#### CP-048 — Control-point effects are observable only via events

Every ControlPoint effect (Gate allow/deny/escalate, Hook side-effect, Guard reorder/error, Budget admit/warn/deny) MUST emit a typed event per [event-model.md §8]: `gate_allowed`, `gate_denied`, `gate_escalated`, `hook_fired`, `hook_failed`, `guard_reordered`, `guard_failed`, `budget_accrual`, `budget_warning`, `budget_exhausted`. No ControlPoint outcome may cross a subsystem boundary by any path other than a typed event.

Tags: mechanism

### 4.11 Skill declaration

#### CP-049 — Nodes MAY declare `required_skills`

A DOT workflow node MAY declare `required_skills` as either (a) a comma-separated list of skill names as a DOT attribute value, or (b) a YAML policy reference via `policy_ref` naming a skill set declared in the §6.3 `skill_sets[]` block. The declared set is checked at workflow-ingest time per [execution-model.md §4.9] validation.

The skill-declaration-to-provisioning pipeline splits into two fail-points:

**Ingest-time syntactic validity (this spec).** Every declared skill name MUST match the skill-name shape defined in [handler-contract.md §4.11] (lowercase hyphenated identifier, optionally suffixed with `@<version>`). Syntactic violations fail workflow-ingest before registration; the policy engine rejects the workflow with `ErrDeterministic`.

**Launch-time package resolution (handler-contract).** Resolution of a declared skill-name to a concrete skill package on disk happens at agent launch per [handler-contract.md §4.11]. A syntactically-valid skill-name that does not resolve to an installed package produces `ErrSkillProvisioningFailed` and routes through handler launch failure per [handler-contract.md §4.11].

Declaration-time is this spec's surface; resolution-time is handler-contract's. The two fail-points are not equivalent; they are a covering partition.

Tags: mechanism

#### CP-050 — Effective skill set is the union of node-level and role-default skills

The effective skill set for an agent launched into a node MUST be the set-union of (a) the node's declared `required_skills` and (b) the assigned role's `default_skills` per §4.6.CP-028. This spec owns the UNION computation. Consumption — resolution of declared skills against available skill packages, provisioning into the agent process shape, emission of the `skills_provisioned` event, and fail-launch on resolution failure — is owned entirely by [handler-contract.md §4.11]; CP-050 does not re-specify those mechanics.

Tags: mechanism

#### CP-051 — Skill declaration is mechanism-tagged

Skill declaration, resolution, and provisioning MUST be mechanism-tagged: no cognition participates in determining the effective skill set. The skill contents themselves (prompts, tool definitions packaged inside a skill) MAY be consumed by cognition at runtime, but the pipeline from declaration to provisioning is deterministic.

Tags: mechanism

#### CP-052 — Beads-CLI skill is the motivating default

The Beads-CLI skill per [docs/foundation/components.md §10.9] MUST be a default skill in every MVH-required role per §4.6.CP-031. Any agent requiring Beads queries or status updates depends on its presence; node-level declaration supplements the role default when additional skills are needed.

Tags: mechanism

### 4.12 Workflow-graph binding

#### CP-053 — A `gate` node is the dispatch surface for a Gate-kind ControlPoint

A workflow-graph node of type `gate` (per [workflow-graph.md §4 WG-001/WG-002]) MUST be the dispatch surface for exactly one Gate-kind ControlPoint registered per §4.9. The Gate-kind ControlPoint is named by the node's `gate_ref` attribute per §4.7.CP-036; the node-type `gate` and the Kind `Gate` are bound by this requirement and not by any other rail.

This affirms D3 Framing A: ControlPoint is NOT itself a node-type in the workflow-graph taxonomy. The `gate` node-type is retained as a node whose handler evaluates a Gate-kind ControlPoint. The other three Kinds (Hook, Guard, Budget) have no node-type representation; they bind via subsystem dispatch (S05 for Hook; S01 for Guard via the edge cascade per [execution-model.md §4.10]; the handler runtime for Budget per §4.5.CP-023) and via attribute references (`budget_ref` on any node).

Tags: mechanism

#### CP-054 — `gate` nodes carry `gate_ref` AND `handler_ref`

A `gate` node MUST carry both `gate_ref` (REQUIRED — names the registered Gate-kind ControlPoint per CP-002) and `handler_ref` (REQUIRED — names the Gate-evaluator handler that performs the dispatch per [execution-model.md §4.2 EM-007 amendment + §7.5]). The two attributes are not interchangeable: `gate_ref` resolves to a `gates[]` entry in policy YAML (the evaluator semantics — mechanism expression or cognition delegation path); `handler_ref` resolves to a handler registered per [handler-contract.md §4.2a + §6.1 RECORD Outcome] (the dispatch shell that drives the evaluator and returns the §4.13 Outcome payload).

This requirement closes pass-4 design follow-up #2 (the EM-007 + Gate-evaluator-dispatch reconciliation). The `gate` node-type is exempt from the EM-007 "non-agentic node-types MUST NOT carry `handler_ref`" rule via the EM-007 amendment in C2; under that amendment, `gate` joins `agentic` as the second node-type that requires `handler_ref`. The semantic difference: an `agentic` handler launches a model session whose Outcome is whatever the agent produces; a `gate` handler evaluates the named ControlPoint and returns a `GateDecisionPayload` (§4.13).

Tags: mechanism

#### CP-055 — `*_ref` family is typed and disambiguated

DOT workflow attributes MUST use the typed `*_ref` family, each pinning a single category of policy target. The complete MVH family is:

| Attribute        | Targets                                            | Required on              | Optional on                  |
|------------------|----------------------------------------------------|--------------------------|------------------------------|
| `gate_ref`       | a `gates[]` entry (Gate-kind ControlPoint)         | every `gate` node        | (not legal elsewhere)        |
| `handler_ref`    | a registered handler per [handler-contract.md]     | every `agentic` and `gate` node | `non-agentic` nodes per [execution-model.md §4.2 EM-007 amendment + §7.5] |
| `freedom_profile_ref` | a `freedom_profiles[]` entry                  | (never required)         | any node                     |
| `budget_ref`     | a `budgets[]` entry (Budget-kind ControlPoint)     | (never required)         | any node, any edge           |
| `skills_ref`     | a `skill_sets[]` entry per §6.3                    | (never required)         | any node                     |
| `policy_ref`     | (DEPRECATED at MVH — see CP-056)                   | —                        | —                            |

`skills_ref` is the typed disambiguation of the prior `policy_ref → skill_sets[]` overload identified in pass-3 research finding D14. Under D3 Framing A, attribute-based binding is the sole rail for policy attachment to graph elements; a typed family is required to keep the binding deterministic and to keep workflow-ingest's reachability AST-walker (§4.8.CP-040a item 4) free of category ambiguity.

Tags: mechanism

#### CP-056 — `policy_ref` is deprecated at MVH; typed refs replace it

The `policy_ref` attribute named in legacy text of §4.7.CP-036 MUST NOT appear in MVH-conformant workflows. Every prior `policy_ref` usage maps onto exactly one typed attribute in the CP-055 family: a `policy_ref` whose target was a `gates[]` entry MUST be rewritten as `gate_ref`; a `policy_ref` whose target was a `freedom_profiles[]` entry MUST be rewritten as `freedom_profile_ref`; a `policy_ref` whose target was a `skill_sets[]` entry MUST be rewritten as `skills_ref`. Workflow-ingest MUST reject any DOT attribute named `policy_ref` with `ErrDeterministic`; the rejection message MUST name the typed replacement attribute(s) the author should use.

This is a breaking change to §4.7.CP-036's prose enumeration of valid `*_ref` attributes; the CP-036 normative statement (DOT attributes resolve to registered names) is unchanged in substance — only the enumerated list is updated. No v1 DOT corpus exists in production (phase-3-dot is the first wire-up of DOT mode), so the migration cost is zero.

Tags: mechanism

#### CP-057 — `skills_ref` semantics

The `skills_ref` attribute, when present on a node, MUST resolve to a `skill_sets[]` entry per §6.3 whose `skills` list names the additional skills that node's handler MUST provision at agent launch beyond the role's `default_skills`. The effective skill set per §4.11.CP-050 becomes the set-union of (a) the node's declared inline `required_skills` (per §4.11.CP-049 option (a)), (b) the resolved `skills_ref` target's skills list (per §4.11.CP-049 option (b)), and (c) the assigned role's `default_skills`. The three-way union preserves the CP-050 monotonic-by-union property; resolution-time failures route through [handler-contract.md §4.11] per CP-049 fail-point B.

`skills_ref` is OPTIONAL on every node-type including `gate`. A `gate` node whose evaluator is mechanism-tagged typically declares no `skills_ref`; a `gate` node whose evaluator is cognition-tagged MAY declare `skills_ref` to provision skill packages the delegated role needs at evaluator dispatch time.

Tags: mechanism

### 4.13 Gate-decision Outcome payload

> **Ownership note.** C3 (`specs/handler-contract.md §Outcome`) defers definition of the `kind = gate_decision` Outcome payload to this spec, per OQ-C3-1. The wrapper Outcome shape (the `Outcome` record carrying a `kind` discriminator and a typed payload) is owned by [execution-model.md §6.1] (Outcome record) and [handler-contract.md §4.2a + §6.1 RECORD Outcome] (handler-contract's `kind` enum). C4 owns the `GateDecisionPayload` shape only.

#### CP-058 — `gate` node handlers return a `GateDecisionPayload`

A handler dispatched against a `gate` node per CP-054 MUST return an Outcome whose `kind` discriminator equals `gate_decision` and whose payload conforms to the `GateDecisionPayload` record declared in §6.1.8. The payload's five fields capture both the evaluator's verdict and the audit trail required to interpret the decision under replay:

1. `policy_id` — String. The `name` of the Gate-kind ControlPoint that was evaluated. MUST equal the resolved target of the node's `gate_ref` per CP-054.
2. `decision` — GateAction. One of `{allow, deny, escalate-to-human}` per §6.1.6 `GateAction`. Identical semantics to CP-006 / CP-009 / CP-008 (subtype-driven escalation).
3. `decision_actor` — String. Names the actor that produced the decision. For mechanism-tagged Gate evaluators, this MUST be the literal string `"mechanism"`. For cognition-tagged Gate evaluators, this MUST be the role name from the `DelegationPath` per §6.1.5 (e.g., `"reviewer"`). The mechanism / cognition distinction is recoverable from this field without re-reading the ControlPoint record.
4. `decision_evidence_ref` — String | None. For cognition-tagged Gate evaluators, this MUST be the path to the persisted `GateVerdictRecord` per §4.8.CP-040 (within the Transition's `evidence` field, keyed by `gate_name`). For mechanism-tagged Gate evaluators, this MAY be None when no auxiliary evidence is produced, or it MAY name an event-stream record (e.g., a `gate_allowed` / `gate_denied` event correlation key) when the evaluator emits auxiliary context.
5. `resolution_signal_id` — String | None. When `decision == escalate-to-human`, this MUST name the resolution signal the run is waiting on (per [execution-model.md §8] escalation semantics); the run enters quarantine pending external resolution per CP-009 and the corresponding §8 entry. When `decision ∈ {allow, deny}`, this field MUST be None.

The Outcome wrapper (status / failure_class / artifact_refs / etc.) follows [execution-model.md §6.1] conventions; a `gate_decision` Outcome's `status` MUST be `SUCCESS` regardless of the `decision` field (a `deny` is a successfully-evaluated Gate, not a failed run; the run staying in the source state per CP-009 is governed by the cascade, not by the Outcome's status). A handler that cannot evaluate the Gate (e.g., evaluator dispatch failure, cognition delegation path unavailable) MUST return an Outcome with `status = FAILURE` and a `failure_class` per [execution-model.md §8]; that Outcome MUST NOT carry a `gate_decision` payload.

Tags: mechanism

#### CP-059 — Egress whitelist governs agent network access per policy

A role's `permission_schema.egress_whitelist` declares the domain patterns the policy permits for outbound network access from agents assigned to that role. The field is optional; when absent (or `None` in the resolved `PermissionSchema`), egress is unrestricted — equivalent to the pre-ON-025 default, preserving backward compatibility. An explicitly empty list (`[]`) means deny all network egress. Domain patterns are glob-style strings; a bare hostname matches that hostname only; a `*` wildcard matches any single label (e.g., `*.anthropic.com` matches `api.anthropic.com` but NOT `foo.bar.anthropic.com`); a double wildcard `**` matches any number of labels (e.g., `**.anthropic.com` matches at any depth). The resolved `egress_whitelist[]` value from the role's `PermissionSchema` MUST be propagated into `LaunchSpec.egress_whitelist` at claim time per [handler-contract.md §4.11.HC-048b]; the handler enforces the list during skill provisioning. Network egress by the agent process at runtime (beyond provisioning) is governed by the same whitelist; enforcement at that level is owned by the sandbox subsystem (S06) and is deferred post-MVH; this spec states the policy-surface obligation.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

## 5. Invariants

#### CP-INV-001 — Registry is the single source of truth

Every ControlPoint observable by the daemon MUST be resolvable through the §4.9 registry. No subsystem may maintain a private ControlPoint store, a shadow registry, or a lazy-resolved ControlPoint surface. Divergence between the registry and any subsystem-local cache is a structural invariant violation.

Tags: mechanism

#### CP-INV-002 — Control-point effects are observable only through events

Every ControlPoint effect that crosses a subsystem boundary MUST be observable through one of the typed events declared in §4.10.CP-048. No ControlPoint outcome may be communicated across subsystems by shared memory, direct method call across an owner boundary, or out-of-band signal.

Tags: mechanism

#### CP-INV-003 — Cognition-tagged evaluators never silently re-invoke the model during replay

For every cognition-tagged evaluator invocation that has a persisted verdict on the run's task branch with a matching `input_envelope_hash` per §4.8.CP-040a, replay MUST consume the persisted verdict. On envelope-hash mismatch, replay MUST escalate to Cat 6 rather than re-invoke silently. Re-invocation during replay is permitted ONLY under an explicit Cat 6 reconciliation verdict per [reconciliation/spec.md §4.2]. Any replay path that silently re-invokes a model — whether because the verdict is missing, the envelope has drifted, or the replayer ignored the hash check — violates this invariant.

Tags: mechanism

> NOTE: Earlier drafts included CP-INV-004 (Guards mechanism-only) and CP-INV-005 (Budget counter state internal). Both fail the invariant-vs-requirement selection test (spec-template §5): each constraint is entirely resolvable inside one §4 subsection (§4.4 and §4.5 respectively). The normative weight is carried by §4.4.CP-020 and §4.5.CP-026; §5 is not the right place for restatements. Downstream consumers citing `CP-INV-004` or `CP-INV-005` should re-cite the §4 requirements.

## 6. Schemas and data shapes

### 6.1 Record schemas

```
RECORD ControlPoint:
    name                 : String         -- unique within the daemon registry
    kind                 : Kind           -- one of {Gate, Hook, Guard, Budget}
    trigger              : Trigger        -- Kind-specific trigger record
    evaluator            : Evaluator      -- mechanism- or cognition-tagged
    outcome_action       : OutcomeAction  -- one of the Kind's declared enum
    payload              : KindPayload    -- typed per Kind (see 6.1.1 - 6.1.4)
    axes                 : AxisTags       -- four-axis tags per [architecture.md §4.1]
    mode_tag             : ModeTag        -- one of {mechanism, cognition}
    schema_version       : Integer        -- N-1 readable per §4.7.CP-038
```

```
ENUM Kind:
    Gate
    Hook
    Guard
    Budget
```

```
RECORD Evaluator:
    mode                 : ModeTag                 -- mechanism | cognition
    expression           : PolicyExpression | None -- set when mode = mechanism
    delegation_path      : DelegationPath | None   -- set when mode = cognition (see 6.1.5)
```

#### 6.1.1 Gate payload

```
RECORD GatePayload:
    subtype              : GateSubtype         -- one of {goal-gate, approval-gate, quality-gate}
    attach_point         : AttachPoint         -- one of {node-pre-entry, node-post-exit, edge-before-selection, edge-after-selection}
    named_approver       : String | None       -- required when subtype = approval-gate
    verification_ref     : String | None       -- required when subtype = quality-gate
```

```
ENUM GateSubtype:
    goal-gate
    approval-gate
    quality-gate
```

```
ENUM AttachPoint:
    node-pre-entry
    node-post-exit
    edge-before-selection
    edge-after-selection
```

#### 6.1.2 Hook payload

```
RECORD HookPayload:
    trigger_event        : String                 -- event name from the registered lifecycle set
    subscription_filter  : PolicyExpression | None -- optional filter over event payload
    side_effect_kind     : SideEffectKind         -- one of {emit-event, state-mutation, external-action}
    halt_on_failure      : Bool                   -- default false; see §4.3.CP-015
    subsystem_priority   : Integer                -- declared by subsystem envelope
```

```
ENUM SideEffectKind:
    emit-event
    state-mutation
    external-action
```

#### 6.1.3 Guard payload

```
RECORD GuardPayload:
    applies_to_node      : String | None        -- node_id scoping the guard (None = all)
```

> NOTE: Guard payload is intentionally thin; the reorder logic lives entirely in the evaluator (mechanism-only per §4.4.CP-020).

#### 6.1.4 Budget payload

```
RECORD BudgetPayload:
    resource             : BudgetResource        -- one of {tokens, wall_clock_seconds, iterations}
    scope                : BudgetScope           -- one of {per_role, per_run, per_state, handler_account}
    limit                : Integer               -- positive; allowance ceiling
    warning_threshold    : Float                 -- ratio in [0, 1]; default 0.8
    scope_target         : ScopeTarget           -- wildcard | predicate | list | singleton

RECORD ScopeTarget:                              -- one of the following syntactic shapes:
    -- "*"                               wildcard: matches every target of the declared scope
    -- "node_type:<type>"                 predicate: matches all targets with declared attribute
    -- ["<id-1>", "<id-2>", ...]          list: matches the enumerated targets
    -- "<single-id>"                      singleton: role name | run_id | state_id per scope
```

```
ENUM BudgetResource:
    tokens
    wall_clock_seconds
    iterations
```

```
ENUM BudgetScope:
    per_role
    per_run
    per_state
    handler_account            -- per-handler-account ceiling (session-token cap / daily quota); handler-fatal per [handler-pause.md HP-012]; see CP-022 INFORMATIVE note (§4.5)
```

#### 6.1.5 Delegation path (cognition-tagged evaluator)

```
RECORD DelegationPath:
    role                 : String                -- role name per [architecture.md §4.8]
    model_class          : String                -- e.g., "reviewer-tier-1"
    input_schema_ref     : String                -- registered input schema name
    response_schema_ref  : String                -- registered response schema name
    prompt_template_ref  : String                -- registered prompt template (provisioned via skill per §4.11)
```

#### 6.1.6 Verdict records and side-effect descriptor

Tags: mechanism

```
RECORD GateVerdictRecord:
    gate_name            : String                -- ControlPoint name
    action               : GateAction            -- {allow, deny, escalate-to-human}
    reason               : String | None         -- human-readable reason; REQUIRED when action != allow
    cognition_meta       : CognitionMeta | None  -- set when verdict produced by cognition-tagged evaluator
    input_envelope_hash  : String                -- SHA-256 hex per §4.8.CP-040a
    produced_at          : Timestamp             -- monotonic-clock-safe timestamp of production

RECORD HookVerdictRecord:
    hook_name            : String                -- ControlPoint name
    invocation_id        : UUID                  -- unique per Hook firing
    side_effect          : SideEffect            -- descriptor produced by the evaluator
    failed               : Bool                  -- true when evaluator returned typed failure
    reason               : String | None
    cognition_meta       : CognitionMeta | None
    input_envelope_hash  : String                -- SHA-256 hex per §4.8.CP-040a
    produced_at          : Timestamp

RECORD CognitionMeta:
    delegation_path      : DelegationPath        -- snapshot of the path used (see §6.1.5)
    model_response_digest: String                -- hash of the raw model output (for audit)
    token_usage          : Integer | None        -- tokens consumed producing the verdict

RECORD SideEffect:
    kind                 : SideEffectKind        -- {emit-event, state-mutation, external-action}
    target               : String                -- event name | state key | effector id
    payload              : Map<String, Any>      -- opaque to this spec; interpreted by the S05 dispatcher per §4.3.CP-016
    idempotency_class    : IdempotencyClass      -- per [execution-model.md §6.1]; CP-016 delivery rules apply to the {idempotent, non-idempotent} subset

-- IdempotencyClass is declared by [execution-model.md §6.1] with three values:
--   idempotent, non-idempotent, recoverable-non-idempotent.
-- CP-016 delivery semantics apply to two of those values at the SideEffect
-- dispatch layer: idempotent (S05 MAY deliver duplicates; handlers retry-safe)
-- and non-idempotent (S05 MUST bound to at-most-once via persisted receipt).
-- recoverable-non-idempotent is a node-level concept; SideEffect descriptors
-- carrying that value are treated as non-idempotent by the S05 dispatcher.
-- This is a filter, not a redeclaration; IdempotencyClass is owned by EM §6.1.

ENUM GateAction:
    allow
    deny
    escalate-to-human
```

> NOTE: The mechanism-tagged `state-mutation` helper is the S05-dispatcher-owned writer that interprets `SideEffect.target` as a typed state key and applies the payload; this spec declares the descriptor shape only. The `external-action` `target` names a handler-owned effector registered per [handler-contract.md §4.11]; effector semantics are handler-owned. If a future requirement needs these surfaces as first-class INTERFACEs, they are declared in S05's subsystem spec.

#### 6.1.7 Registry interface

```
INTERFACE Registry:
    Register(cp) -> error                                      -- re-registration-safe on identical body per §4.9.CP-044; fails on divergent body under existing name
    LookupByName(name) -> (ControlPoint, Bool)                 -- deterministic; Bool is false when name not registered
    LookupByTrigger(trigger) -> List<ControlPoint>             -- returns Hooks/Gates whose trigger matches; sorted by name ascending
    LookupByAttachPoint(attach_point) -> List<ControlPoint>    -- returns Gates at the named attach point; sorted by declaration order
    All() -> List<ControlPoint>                                -- returns every registered ControlPoint in name-ascending order (used by §7.1 post-registration audit)
```

Each method MUST satisfy §4.9.CP-046 (determinism). The registry is daemon-scoped per §4.9.CP-045 and owned by S02 per §4.10.CP-047.

#### 6.1.8 GateDecisionPayload record (new)

```
RECORD GateDecisionPayload:
    policy_id              : String         -- name of the evaluated Gate-kind ControlPoint (CP-002)
    decision               : GateAction     -- {allow, deny, escalate-to-human} per §6.1.6
    decision_actor         : String         -- "mechanism" | <role-name> per CP-058
    decision_evidence_ref  : String | None  -- path to GateVerdictRecord (cognition) or event correlation key (mechanism); None permitted for mechanism evaluators with no auxiliary evidence
    resolution_signal_id   : String | None  -- escalation resolution-signal name; non-None iff decision == escalate-to-human
```

This record is the typed payload referenced by the `kind = gate_decision` discriminator in [handler-contract.md §4.2a + §6.1 RECORD Outcome]. Field-shape evolution follows §6.6 (N-1 readable per CP-038).

### 6.2 Role and freedom-profile schemas

```
RECORD Role:
    name                 : String                -- per [architecture.md §4.8]
    permission_schema    : PermissionSchema
    status               : RoleStatus            -- mvh-required | declared-but-deferred
```

```
RECORD PermissionSchema:
    allowed_tools        : List<String>
    writable_paths       : List<String>          -- workspace-relative globs
    readable_paths       : List<String>          -- default ["**"]
    model_tier           : String | None
    default_skills       : List<String>          -- MUST include "beads-cli" for MVH roles (§4.6.CP-031)
    allowed_hooks        : List<String>          -- Hook names that may modify behavior
    invocable_by         : List<String>          -- role names permitted to spawn this role
    egress_whitelist     : List<String> | None   -- domain patterns permitted for agent network egress; None = unrestricted; [] = deny all (§4.11.CP-059)
```

```
RECORD FreedomProfile:
    name                 : String
    tool_whitelist       : List<String>
    writable_paths       : List<String>
    model_tier           : String | None
    token_budget_ref     : String | None         -- Budget name; see §4.5
    wall_clock_budget_ref: String | None         -- Budget name; see §4.5
    max_iterations       : Integer
```

### 6.3 Policy YAML document shape

This YAML template is regenerated from the §6.1 / §6.2 records. Every normative field declared on a record appears here; optional fields are flagged; required-when-X fields are labeled. Authors who find a normative field missing from this template SHOULD treat it as a template bug and file against OQ-CP-001.

```yaml
metadata:
  name: <policy-name>
  version: <semver-ish>
  author: <author>
  schema_version: <integer>    # N-1 readable per §4.7.CP-038

roles:
  - name: <role-name>
    permission_schema:
      allowed_tools: [<tool-name>, ...]
      writable_paths: [<glob>, ...]
      readable_paths: [<glob>, ...]    # optional; defaults to ["**"]
      model_tier: <tier>               # optional; see CP-033 for ordering
      default_skills: [<skill-name>, ...]
      allowed_hooks: [<hook-name>, ...]
      invocable_by: [<role-name>, ...]
      egress_whitelist: [<domain-pattern>, ...]  # optional; omit = unrestricted; [] = deny all; per §4.11.CP-059
    status: mvh-required | declared-but-deferred

freedom_profiles:
  - name: <profile-name>
    tool_whitelist: [<tool-name>, ...]
    writable_paths: [<glob>, ...]
    model_tier: <tier>                 # optional
    token_budget_ref: <budget-name>    # optional; references budgets[].name
    wall_clock_budget_ref: <budget-name>  # optional; references budgets[].name
    max_iterations: <int>

gates:
  - name: <gate-name>
    subtype: goal-gate | approval-gate | quality-gate
    attach_point: node-pre-entry | node-post-exit | edge-before-selection | edge-after-selection
    named_approver: <role-or-human-id>    # REQUIRED when subtype = approval-gate
    verification_ref: <node-name>         # REQUIRED when subtype = quality-gate
    evaluator:
      mode: mechanism | cognition
      expression: "<expr-lang expression>"   # REQUIRED when mode = mechanism
      delegation_path:                       # REQUIRED when mode = cognition
        role: <role>
        model_class: <class>
        input_schema_ref: <name>
        response_schema_ref: <name>
        prompt_template_ref: <name>

hooks:
  - name: <hook-name>
    trigger_event: on_agent_started | on_agent_output | on_agent_completed
      | on_timeout | on_review_required | on_transition_attempted
      | on_checkpoint_written | on_checkpoint_failed
    subscription_filter: "<expr-lang expression>"   # optional
    side_effect_kind: emit-event | state-mutation | external-action
    halt_on_failure: false              # default false
    subsystem_priority: <integer>
    evaluator:                          # REQUIRED per §4.3.CP-012
      mode: mechanism | cognition
      expression: "<expr-lang expression returning SideEffect or Null>"   # when mode = mechanism
      delegation_path:                  # when mode = cognition
        role: <role>
        model_class: <class>
        input_schema_ref: <name>
        response_schema_ref: <name>
        prompt_template_ref: <name>
    side_effect_target: <event-name | state-key | effector-id>   # default target when evaluator returns a target-less SideEffect
    side_effect_payload:                # default payload; evaluator output merges over this
      <key>: <expr-template or literal>
    idempotency_class: idempotent | non-idempotent   # per §4.3.CP-016; default non-idempotent

guards:
  - name: <guard-name>
    applies_to_node: <node-id>          # optional; None = all nodes
    evaluator:
      mode: mechanism
      expression: "<expr-lang expression returning List<Edge>>"

budgets:
  - name: <budget-name>
    resource: tokens | wall_clock_seconds | iterations
    scope: per_role | per_run | per_state | handler_account
    limit: <int>
    warning_threshold: 0.8              # default 0.8
    scope_target: "*"                   # wildcard, predicate, list, or singleton per §6.1.4 ScopeTarget

skill_sets:                             # named skill sets referenceable from DOT policy_ref (§4.11.CP-049)
  - name: <skill-set-name>
    skills: [<skill-name>, ...]         # resolved at workflow-ingest time; unknown names fail validation
```

### 6.4 Policy expression grammar (adopted)

```
grammar: expr-lang/expr                    -- https://github.com/expr-lang/expr
environment:
    run         : Run                      -- [execution-model.md §6.1 Run]  (key paths inlined below)
    outcome     : Outcome                  -- [execution-model.md §6.1 Outcome]  (key paths inlined below)
    event       : Event | None             -- matched event (Hook context only); None outside Hook context
    context     : Map<String, Any>         -- run's shared context
    policy_meta : Map<String, String>      -- document metadata
    edges       : List<Edge>               -- Guard context only; None outside Guard context
purity       : side-effect-free            -- expressions MUST NOT perform I/O or state mutation
evaluation   : bounded                     -- implementation MUST set AST-step counter and expr.Timeout per §4.7.CP-034b
type-check   : at workflow-ingest          -- expressions MUST type-check against the environment at registration per §4.7.CP-035
```

#### 6.4.1 Inlined key field paths

The full `Run`, `Outcome`, `Event`, and `Edge` shapes are defined in other specs (cross-references above). Policy authors MUST be able to write common expressions without cross-spec reading; the following key paths are **stable and canonical** at the MVH schema version. A field missing from this table is NOT forbidden — authors MAY dereference any path the full shape documents — but the following paths are the 20 most-used and are guaranteed to exist and to carry the declared types.

```
-- Run (see [execution-model.md §6.1] for full shape)
run.id                       : String        -- unique run identifier
run.state                    : String        -- current node id; see also run.state_id
run.bead_id                  : String | None -- linked Beads record id; None for runs without a bead
run.workflow_version         : String        -- pinned version of the DOT workflow
run.paused                   : Bool          -- true when daemon is in operator pause per [operator-nfr.md §4.3]
run.next_node                : NodeHandle    -- candidate target (available at Gate attach_point = node-pre-entry only)
run.next_node.id             : String
run.next_node.agent_type     : String        -- per [handler-contract.md §6.1]; e.g., "claude-code", "ntm"
run.context                  : Map<String, Any>  -- alias of top-level context binding

-- Outcome (see [execution-model.md §6.1])
outcome.status               : String        -- one of {SUCCESS, FAILURE, ESCALATED, ...}
outcome.failure_class        : String | None -- one of [execution-model.md §8] classes; None on SUCCESS
outcome.artifact_refs        : List<String>  -- artifact identifiers produced by the outcome

-- Event (see [event-model.md §6.3])
event.type                   : String        -- canonical event name
event.payload                : Map<String, Any>  -- event-typed payload; fields per event-model
event.payload.run_id         : String | None
event.payload.bead_id        : String | None -- present on checkpoint and bead-lifecycle events
event.payload.exit_code      : Integer | None  -- present on agent-completed events

-- Edge (Guard context; see [execution-model.md §4.10])
edge.target                  : NodeHandle    -- same shape as run.next_node
edge.target.id               : String
edge.weight                  : Integer | None
edge.preferred_label         : String | None
```

#### 6.4.2 Return-shape conventions per Kind

One expression grammar, four return conventions. Authors MUST honor the convention for their ControlPoint's Kind; a return shape mismatch is a type-check error at workflow-ingest per §4.7.CP-035.

| Kind | Expression return shape | Interpretation |
|---|---|---|
| **Gate** (evaluator expression) | `Bool` | `true` → `allow`; `false` → `deny` using the record's declared `reason`. Structured returns are not supported at MVH. |
| **Gate** (`subscription_filter`-style filters if added) | `Bool` | Filter predicate. |
| **Hook** (evaluator expression) | `SideEffect` struct or `Null` | `Null` → no-op; a `SideEffect` struct is dispatched per §4.3.CP-016. Struct literal syntax follows the §6.1.6 `SideEffect` shape. |
| **Hook** (`subscription_filter`) | `Bool` | Predicate filter on event payload. |
| **Guard** (evaluator expression) | `List<Edge>` | Reordered edge list that is a subset or permutation of the input `edges` binding per §4.4.CP-018. |
| **Budget** (any expression) | N/A at MVH | Budget evaluation is not authored in `expr-lang`; Budget enforcement is entirely mechanism-tagged per §4.5. |

> NOTE: Expressions MUST be type-checked at registration against the environment. Dereferencing `event` outside a Hook-context expression (where `event` is `None`) is a type-check error detected at ingest, not a runtime panic. Dereferencing `edges` outside a Guard-context expression is also a type-check error.

> EXAMPLE (mechanism-tagged Gate expression — returns Bool):
>
> ```
> outcome.status == "SUCCESS" && run.workflow_version == policy_meta.required_version
> ```
>
> EXAMPLE (Hook subscription filter — returns Bool):
>
> ```
> event.type == "agent_completed" && event.payload.exit_code != 0
> ```
>
> EXAMPLE (Hook evaluator expression — returns SideEffect struct):
>
> ```
> { kind: "emit-event", target: "checkpoint_audit_logged",
>   payload: { run_id: event.payload.run_id, bead_id: event.payload.bead_id },
>   idempotency_class: "idempotent" }
> ```
>
> EXAMPLE (Guard evaluator expression — returns List<Edge>):
>
> ```
> sortBy(edges, "target.weight")
> ```

### 6.5 Co-owned event payloads

This spec's requirements drive emission of the following events whose payload schemas are declared in [event-model.md §6.3]:

- `gate_allowed` — emitted when a Gate evaluator returns `allow`.
- `gate_denied` — emitted when a Gate evaluator returns `deny`; payload includes `run_id`, `gate_name`, `reason`.
- `gate_escalated` — emitted when a Gate policy elevates `deny` to `escalate-to-human`.
- `hook_fired` — emitted after a Hook side-effect is applied.
- `hook_failed` — emitted when a Hook evaluator returns a typed failure.
- `guard_reordered` — emitted when a Guard produces a non-identity reorder.
- `guard_failed` — emitted when a Guard evaluator errors at invocation time per §8 (the cascade proceeds on the un-reordered edge list).
- `budget_accrual` — emitted on every chunk accrual per §4.5.CP-024.
- `budget_warning` — emitted at warning threshold per §4.5.CP-025.
- `budget_exhausted` — emitted on dispatch denial per §4.5.CP-023.
- `skills_provisioned` — emitted after the handler provisions the effective skill set per §4.11.CP-050.
- `hook_verdict_persisted` — emitted after a cognition-tagged Hook verdict is written to git per §4.8.CP-040.
- `verdict_envelope_mismatch` — emitted when replay detects a persisted verdict's `input_envelope_hash` does not match the current envelope per §4.8.CP-041.
- `control_points_registration_started` — emitted once per daemon init BEFORE Pass 1 of the §7.1 registration batch; carries a `batch_id` correlation value.
- `control_points_registered` — emitted once per daemon init after the §7.1 registration pass completes; carries the same `batch_id`. Absence of this event paired with a prior `control_points_registration_started` of the same `batch_id` in JSONL signals a crashed-mid-registration batch.
- `policy_expression_exceeded_cost` — emitted when a policy-expression evaluation exceeds the harmonik-level cost ceiling per §4.7.CP-034b; payload carries a `bound_fired ∈ {ast_steps, wall_clock}` discriminator and a per-abort `io_determinism` tag.
- `gate_definition_drift` — emitted when a mechanism-tagged Gate's envelope inputs (per CP-038a) differ between the original evaluation and a replay attempt. Payload includes `run_id`, `gate_name`, `prior_envelope_hash`, `current_envelope_hash`, and a `changed_inputs` list naming which of `{expression_text, context_subset, policy_meta}` changed.
- `gate_redefined_under_cat_6` — emitted when a Cat 6 reconciliation verdict authorizes mechanism-tagged Gate re-evaluation under a drifted definition. Payload includes `run_id`, `gate_name`, `prior_decision`, `new_decision`, and the `cat_6_verdict_id`.

This spec is normative for WHEN each event fires; [event-model.md §6.3] is normative for the payload shape.

### 6.6 Schema evolution

All schemas in this spec carry a `schema_version` integer. Compatibility is N-1 readable per [operator-nfr.md §4.5]: a reader MUST accept the immediately prior schema version; breaking changes require a migration release scheduled at an operator pause per [operator-nfr.md §4.3]. Additive changes (new optional field) are non-breaking; renaming or removing fields is breaking.

## 7. Protocols and state machines

### 7.1 ControlPoint registration sequence

```
FUNCTION register_control_points_at_startup(daemon):
    policy_docs = load_policy_yaml_chain(daemon.config)         -- per §4.7 precedence
    emit_event("control_points_registration_started", batch_id=daemon.boot_id)
    -- Pass 1: parse and validate every document, build symbol table.
    FOR doc IN policy_docs:
        validate_document_schema(doc)                            -- §4.7.CP-035
    symbol_table = build_symbol_table(policy_docs)               -- names of budgets, freedom profiles, skill sets
    -- Pass 2: resolve cross-document references and register; missing refs fail startup per OQ-CP-005 default.
    FOR doc IN policy_docs:
        FOR cp_decl IN doc.gates + doc.hooks + doc.guards + doc.budgets:
            cp = construct_control_point(cp_decl, symbol_table)  -- PURE: no events, no writes, no dispatch
            IF cp.kind == Guard AND cp.evaluator.mode == cognition:
                FAIL(class=structural, reason="cognition-tagged Guard forbidden")  -- §4.4.CP-020
            existing = s02.registry.LookupByName(cp.name)
            IF existing IS NOT None:
                IF body_equal(existing, cp):
                    CONTINUE                                     -- re-registration-safe per §4.9.CP-044
                ELSE:
                    FAIL(class=structural, reason="duplicate registration with divergent body")  -- §4.9.CP-044
            s02.registry.Register(cp)
    emit_event("control_points_registered", batch_id=daemon.boot_id, count=len(s02.registry.All()))
```

`construct_control_point` MUST be pure: no event emission, no durable writes, no dispatch, no Hook firing, no audit-record side-effect during construction. Purity is a registration-sequence invariant; any side-effect during construction makes partial-registration non-atomic and violates the daemon-restart recovery story of §4.9.CP-045.

The paired `control_points_registration_started` / `control_points_registered` events bracket the batch: if `control_points_registration_started` appears in JSONL without a matching `control_points_registered` (same `batch_id`), the batch is presumed **crashed mid-registration** and MUST be replayed from scratch on daemon restart — the in-process registry is ephemeral (§4.9.CP-045), so replay rebuilds from policy YAML. A daemon-startup subsystem MUST NOT consume registry state across a batch gap.

Every branch above corresponds to a normative requirement: §4.7.CP-035 (schema validation), §4.4.CP-020 (cognition-Guard rejection), §4.9.CP-044 (re-registration-safe on identical body, divergent-body rejection). The two-pass structure resolves cross-document references per OQ-CP-005 default.

### 7.2 Cognition-tagged evaluator invocation with persisted verdict

```
FUNCTION invoke_cognition_evaluator(cp, invocation_ctx):
    verdict_key       = persisted_verdict_key(cp, invocation_ctx)    -- §4.8.CP-041
    current_envelope  = compute_envelope(cp, invocation_ctx)         -- §4.8.CP-040a
    current_hash      = sha256(canonicalize_json(current_envelope))
    existing = read_persisted_verdict_from_git(verdict_key)
    IF existing IS NOT None:
        IF existing.input_envelope_hash == current_hash:
            RETURN existing                                           -- replay: no re-invocation per CP-INV-003
        ELSE:
            emit_event("verdict_envelope_mismatch", verdict_key, stored=existing.input_envelope_hash, current=current_hash)
            escalate_to_cat_6(verdict_key, current_envelope)          -- §4.8.CP-041 re-invocation gate
            RETURN cat_6_verdict(verdict_key)
    verdict = dispatch_to_role(cp.evaluator.delegation_path, invocation_ctx)  -- §4.8.CP-039 (cognition-tagged)
    verdict.input_envelope_hash = current_hash                       -- §4.8.CP-040a
    persist_verdict_to_git(verdict_key, verdict)                     -- §4.8.CP-040 (mechanism-tagged)
    emit_event("hook_verdict_persisted", verdict_key, verdict)
    RETURN verdict
```

The two sides split per §4.8.CP-042: `dispatch_to_role` is cognition-tagged; `compute_envelope`, `persist_verdict_to_git`, and the replay-read+hash-compare path are mechanism-tagged. The Cat 6 escalation path is the only legal route to re-invocation.

## 8. Error and failure taxonomy

Not applicable as a separate taxonomy; ControlPoint failures map onto the execution-model failure classes per [execution-model.md §8]:

- Gate `deny` (non-escalating) → run stays in source state; the per-node retry policy governs retries; no additional failure class introduced.
- Gate `escalate-to-human` → run enters quarantine state awaiting external resolution; emits `gate_escalated`; not a failure class.
- Hook evaluator error with `halt_on_failure = false` → typed failure event; chain continues.
- Hook evaluator error with `halt_on_failure = true` → maps to `ErrTransient` or `ErrDeterministic` per [handler-contract.md §4.5] by the S05 dispatcher.
- Guard error at invocation time → maps to `ErrStructural`; the cascade proceeds on the un-reordered edge list and emits `guard_failed`.
- Budget exhaustion → `budget_exhausted` failure class per [execution-model.md §8.5].

## 9. Cross-references

### 9.1 Depends on

- **[architecture.md §4.1]** — four-axis classification; every ControlPoint carries the axes.
- **[architecture.md §4.2]** — ZFC rule; Guard mechanism-only restriction (§4.4.CP-020) is the ZFC enforcement point at the selection-logic layer.
- **[architecture.md §4.4]** — subsystem envelope; registration path consumes it (§4.9.CP-044).
- **[architecture.md §4.6]** — amendment protocol; declared-but-deferred role activation requires it (§4.6.CP-030).
- **[architecture.md §4.8]** — role taxonomy; role names and MVH-required vs. deferred classification are owned there.
- **[architecture.md §4.9]** — centralized-controller principle; the three owners (S01, S02, S05) embody it.
- **[architecture.md §4.10]** — three-artifact separation; DOT references YAML by name (§4.7.CP-036).
- **[execution-model.md §4.1]** — Run, State, Transition, Outcome types; policy expressions evaluate against them (§4.7.CP-034).
- **[execution-model.md §4.2]** — node attributes; DOT carries `gate_ref` / `policy_ref` / `freedom_profile_ref` / `budget_ref`.
- **[execution-model.md §4.10]** — edge-selection cascade; Guard precedes, Gate follows.
- **[execution-model.md §8]** — failure taxonomy; Budget exhaustion maps to `budget_exhausted`.
- **[event-model.md §8]** — event taxonomy; §6.5 events have their payload schemas there.
- **[event-model.md §4.3]** — consumer taxonomy; Hooks are parallel consumers per §4.3.CP-012.
- **[handler-contract.md §4.1]** — handler outcome shape; Hooks consume lifecycle events emitted by handlers.
- **[handler-contract.md §4.2]** — chunk-emission cadence; Budget accrual inherits it (§4.5.CP-024).
- **[handler-contract.md §4.5]** — error category wrapping; Hook failure mapping inherits it (§4.3.CP-015).
- **[handler-contract.md §4.11]** — skill-injection mechanism; consumer of this spec's §4.11 declaration surface.
- **[workflow-graph.md §4 WG-001/WG-002]** — the 4-node-type catalog (`agentic`, `non-agentic`, `gate`, `sub-workflow`); C4 binds the `gate` node-type to the Gate-kind ControlPoint here (CP-053).
- **[execution-model.md §4.2 EM-007 amendment + §7.5]** — the amended dispatch-table rule that `gate` node-type carries `handler_ref` alongside `agentic`; C4 consumes this at CP-054.
- **[handler-contract.md §4.2a + §6.1 RECORD Outcome]** — the `kind` discriminator enum including `gate_decision`; C4 owns the payload shape (§6.1.8) referenced by that enum.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from the foundation corpus. Populated at finalize.

### 9.3 Co-references (read-only consumption)

- **[reconciliation/spec.md §4.4 Wall-clock budget]** — reconciliation workflows carry a mandatory wall-clock Budget; this spec owns the Budget primitive, reconciliation owns the outer-bound enforcement (§4.5.CP-027).
- **[reconciliation/spec.md §4.5 Verdict vocabulary]** — a Cat 6 verdict may flag a persisted cognition verdict as stale, permitting re-invocation per §4.8.CP-041.
- **[workspace-model.md §4.2 Branching model]** — persisted Hook verdicts land on the run's task branch defined there; merge semantics under §4.5.
- **[operator-nfr.md §4.3 Operator-control semantics]** — policy reloads are bound to the between-task invariant (§4.7.CP-037).
- **[operator-nfr.md §4.5 Checkpoint-format stability]** — N-1 compatibility contract applies to policy schema (§4.7.CP-038).
- **[beads-integration.md §4.9 Beads-CLI skill]** — the motivating default skill instance (§4.6.CP-031, §4.11.CP-052).
- **[docs/foundation/components.md §9.4a]** — reconciliation wall-clock budget (bootstrap citation; migrates to reconciliation.md §4.4 once finalized).
- **[docs/foundation/components.md §10.9]** — Beads-CLI skill declaration (bootstrap citation; migrates to beads-integration.md §4.9 once finalized).

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforming to Core MVH MUST pass every requirement CP-001 through CP-058 (including CP-026a, CP-034b, CP-038a, and CP-040a) and every invariant CP-INV-001 through CP-INV-003. No requirement is deferred at MVH.

**Post-MVH extensions.** Declared-but-deferred role activation (§4.6.CP-030) is an additive extension requiring foundation amendment; not required to claim Core MVH conformance.

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose. Each requirement's test obligation:

- **CP-001 — CP-005 (unified primitive).** Schema conformance tests: every registered ControlPoint validates against §6.1; the per-Kind table is honored at registration.
- **CP-006 — CP-011 (Gate).** Gate-attach-point unit tests; allow/deny/escalate verdict tests; S01-invocation tests using a twin orchestrator per the testing-methodology document.
- **CP-012 — CP-017 (Hook).** Hook-dispatch-ordering tests; halt-on-failure semantics; cognition-tagged Hook verdict persistence; failure-isolation with `halt_on_failure = false`.
- **CP-018 — CP-021 (Guard).** Reorder-only assertion tests (attempted add/remove/block MUST fail registration); cognition-Guard rejection at registration; S01-invocation ordering (Guard precedes condition cascade).
- **CP-022 — CP-027 (Budget).** Dispatch-time denial tests; chunk-accrual event emission tightness (events bounded to handler tick); warning threshold at 0.8 default; tightest-wins composition with freedom-profile budgets; JSONL-replay rehydration test (CP-026a) covering daemon-restart-with-in-flight-run sum-from-`run_started` reconstruction.
- **CP-028 — CP-033 (roles and freedom profiles).** Role-default-permission tests; declared-but-deferred shells empty; freedom-profile tightest-wins arithmetic.
- **CP-034 — CP-038 (policy grammar and documents).** `expr-lang/expr` round-trip tests; policy YAML required-section validation; config-precedence deep-merge tests; N-1 readability tests; cost-ceiling abort tests (CP-034b) using pathologically-deep expressions, with `bound_fired` discriminator assertion (ast_steps primary; wall_clock backstop); abort-and-emit durability pair test (crash between abort and event durability triggers replay re-abort, not silent drop).
- **CP-039 — CP-042 (cognition-tagged replay-safety).** Verdict-persistence-at-invocation tests; envelope-hash computation determinism tests (CP-040a); replay-reads-persisted-verdict-on-hash-match tests (no second model call observable); replay-escalates-to-Cat-6-on-hash-mismatch tests; Cat 6 stale-verdict re-invocation tests.
- **CP-043 — CP-046 (registration).** Idempotent-by-name tests; divergent-body rejection; daemon-local scope; deterministic-lookup tests.
- **CP-047 — CP-048 (ownership split).** Cross-subsystem tests verifying no ControlPoint effect crosses a boundary by any path other than a typed event.
- **CP-049 — CP-052 (skill declaration).** Workflow-ingest validation tests for `required_skills`; set-union effective-skill-set arithmetic; `skills_provisioned` event emission; Beads-CLI default presence.
- **CP-053 — CP-058 + CP-038a (workflow-graph binding and gate-decision payload).** `gate` node-type binding test (CP-053: registration must reject a `gate` node without `gate_ref`); `gate_ref` + `handler_ref` pairing test (CP-054: both required); typed `*_ref` family tests (CP-055: each attribute resolves to its correct policy target type); `policy_ref` rejection test (CP-056: workflow-ingest returns `ErrDeterministic` with replacement attribute named); `skills_ref` set-union arithmetic test (CP-057: union of inline + `skills_ref` target + role default); `GateDecisionPayload` round-trip test (CP-058: `gate` handler returns `kind=gate_decision` with all five fields populated; `deny` → `status=SUCCESS`; evaluator failure → `status=FAILURE` no payload); mechanism-tagged Gate drift detection (CP-038a: envelope-hash comparison across definition change emits `gate_definition_drift`; Cat 6 re-evaluation emits `gate_redefined_under_cat_6`).

Migration to `[testing.md §<layer>]` cross-references occurs within one revision cycle once testing.md lands; this obligation is tracked in OQ-CP-002.

### 10.3 Excluded conformance claims

- This spec does NOT grant conformance over: Hook dispatch loop internals (owned by the S05 subsystem spec); Gate and Guard invocation mechanics inside the edge cascade (owned by the S01 subsystem spec per [execution-model.md §4.10]); the reconciliation wall-clock budget's outer-bound enforcement (owned by reconciliation/spec.md §4.4); skill package storage and injection mechanism (owned by [handler-contract.md §4.11]); event payload shapes (owned by [event-model.md §6.3]); role names and MVH-required vs. deferred classification (owned by [architecture.md §4.8]).
- This spec does NOT specify `expr-lang/expr` version pinning or specific numeric sandboxing limits; those are deferred per OQ-CP-003 and are bounded by the CP-034b requirement that the implementation set `expr.MaxNodes` and `expr.Timeout` to harmonik-level (non-policy-tunable) values.

## 11. Open questions

#### OQ-CP-001 — Policy document schema evolution beyond N-1

Question: The §4.7.CP-038 schema-version contract specifies N-1 readability. Policy documents are operator-authored; a richer migration story (field renames via alias map, deprecation windows, per-field migrations) may be needed as the foundation matures.
Owner: foundation-author
Blocks: none (MVH defaults to N-1 additive-only)
Default-if-unresolved: N-1 additive-only. Aliases and renames are breaking and require a migration release.

#### OQ-CP-002 — Migrate test-obligation prose to testing.md references

Question: §10.2 currently names test obligations in prose. The template §10.2 expects cross-references to `[testing.md §<layer>]` once testing.md lands.
Owner: foundation-author
Blocks: none (MVH prose obligations are in place)
Default-if-unresolved: Keep prose obligations; migrate within one revision cycle after testing.md is finalized.

#### OQ-CP-003 — `expr-lang/expr` version-pin policy

Question: The adopted policy-expression grammar is `expr-lang/expr`. Should the spec pin to a specific release (e.g., via `go.mod` pin + operator-nfr external-input protocol) or float to the minor-version level?
Owner: foundation-author
Blocks: none (implementation defaults to the pinned version per operator-nfr external-input protocol)
Default-if-unresolved: Pin to a specific release; a version bump is a harmonik-level upgrade per [operator-nfr.md §4.5].

#### OQ-CP-004 — Hook verdict path when no run is active

Question: §4.8.CP-040 writes cognition-tagged Hook verdicts to `.harmonik/hooks/<run_id>/...` on the run's task branch. Hooks may fire on events unscoped to a run (e.g., daemon-lifecycle events). Where do their verdicts persist?
Owner: foundation-author
Blocks: CP-040 completeness for unscoped Hooks
Default-if-unresolved: Daemon-scoped Hooks with cognition-tagged evaluators are forbidden in MVH; if needed, the verdict persists to a daemon-scoped harmonik-repo branch under `.harmonik/daemon/hooks/...`. Revisit when a concrete daemon-scoped cognition Hook use-case appears.

#### OQ-CP-005 — Cross-policy reference resolution order

Question: Policy documents may reference Budgets / freedom profiles defined in other policy documents (via `name`). What is the load-order rule for cross-document references, and what happens on a missing reference at startup vs. at mid-operation re-register?
Owner: foundation-author
Blocks: §4.9 registration semantics for multi-document policies
Default-if-unresolved: All policies load in a single startup phase per §4.7 precedence; cross-document references resolve after all documents are parsed but before registration; missing references fail startup.

#### OQ-CP-006 — Mechanism-tagged Gate drift envelope: re-use vs. separate hash

Question: CP-038a defines a separate envelope hash for mechanism-tagged Gates (three inputs) parallel to CP-040a's cognition envelope hash (five inputs). Should the two hashes share a record shape, or remain separate types? MVH ships them as separate types (no shared `GateEnvelopeHash` record) to keep cognition's persisted-verdict pathway visually distinct from mechanism's per-replay-attempt recompute pathway.
Owner: foundation-author
Blocks: none (MVH defaults to separate types)
Default-if-unresolved: Keep separate; revisit if a third Kind develops a hash discipline.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-06-01 | 0.4.3 | agent (hk-63oh.26) | **CP-027 citation fix + Axes.** Migrated bootstrap citation `[docs/foundation/components.md §9.4a]` → `[reconciliation/spec.md §4.4 RC-017]` (reconciliation spec is now finalized at v0.4.1); updated inline reference `reconciliation.md §4.4` → `[reconciliation/spec.md §4.4]`. Added missing `Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` to CP-027 per template obligation. No requirement IDs or schemas touched. Refs: hk-63oh.26. |
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft. |
| 2026-04-24 | 0.2.0 | foundation-author | Round-1 integration: migrated self-citations (architecture/reconciliation/workspace-model/operator-nfr) to new §4.x numbering; added INTERFACE Registry (§6.1.7) and GateVerdictRecord / HookVerdictRecord / SideEffect / CognitionMeta (§6.1.6); added CP-040a (input-envelope hash) and rewrote CP-041 + CP-INV-003 for envelope-match replay-safety; added CP-034b (evaluation-cost ceiling); collapsed CP-050 to the union-computation only (handler-contract owns resolution/provisioning); split skill fail-points between ingest-time syntactic validity and launch-time package resolution in CP-049; extended §6.3 YAML with `skill_sets[]`; added `guard_failed`, `verdict_envelope_mismatch`, `policy_expression_exceeded_cost`, `control_points_registered` to §6.5; strengthened CP-027 with wall-clock composition rule; fixed CP-032/CP-033 tightest-wins wording and model_tier ordering; demoted CP-INV-004/CP-INV-005 per template §5 selection test; clarified CP-007 ordering as record-side declaration; Hook trigger namespace clarified in CP-013; §A.3 rationale added for cel-go comparison and for registration-layer Kind peerage; §7.1 registration sequence restructured to two passes for cross-document references. Downstream specs (execution-model, handler-contract, event-model, reconciliation, operator-nfr, beads-integration) carry ~30 citations to CP at legacy §6.x addresses; those specs MUST migrate their citations to the §4.x addresses used here. |
| 2026-04-24 | 0.3.1 | foundation-author | Corpus-wide cleanup pass (no semantic changes). Completed AR-MIG-001 `handler_type` → `agent_type` rename at §6.3 policy-expression key path (`run.next_node.handler_type` → `run.next_node.agent_type`); corrected the accompanying cross-reference from `[handler-contract.md §4.1]` to `[handler-contract.md §6.1]` (the LaunchSpec RECORD declaring `agent_type` lives in §6.1, not §4.1). No requirement IDs, invariants, or schemas were touched. |
| 2026-04-24 | 0.3.2 | foundation-author | Corpus citation-drift cleanup pass 2: migrated legacy §N.N cross-spec anchors to current template §N.N form per the central remap table; ~15 citations fixed. EV: `§3.2→§6.3`/§8/§8.2/§8.4/§4.3/§4.4 per context (payload registry, taxonomy, specific lifecycle per cite), `§3.7→§4.3` (consumer taxonomy) at §9.1 cross-refs. Reconciliation path fix: `[reconciliation.md §4.N]→[reconciliation/spec.md §4.N]` at §4.8 CP-040a replay-safe rule, §5 CP-INV-003 invariant, and §9.3 cross-refs (both §4.4 wall-clock budget and §4.5 verdict vocabulary). No requirement IDs, invariants, or schemas touched. |
| 2026-05-23 | 0.4.0 | phase-3-dot/C4 | Added §4.12 (workflow-graph binding: CP-053 `gate` node-type binding, CP-054 `gate_ref` + `handler_ref` pairing, CP-055 typed `*_ref` family, CP-056 `policy_ref` deprecation, CP-057 `skills_ref` semantics); added §4.13 (Gate-decision Outcome payload: CP-058 + §6.1.8 `GateDecisionPayload` record, ownership taken per C3 OQ-C3-1 deferral); added CP-038a (mechanism-tagged Gate schema-version drift discipline mirroring CP-040a / CP-INV-003 envelope-hash + Cat 6 escalation); added `gate_definition_drift` and `gate_redefined_under_cat_6` events to §6.5; added OQ-CP-006 for envelope-hash type-sharing question. No existing requirements renumbered; CP-053 through CP-058 assigned. |
| 2026-05-31 | 0.4.2 | agent (hk-sx9r.30) | **ON-025 egress whitelist policy surface (CP-059).** Added `egress_whitelist : List<String> | None` to `PermissionSchema` RECORD (§6.1): `None`/absent = unrestricted; `[]` = deny all; domain patterns are glob-style. Added matching `egress_whitelist` field to §6.3 Policy YAML template under `permission_schema:`. Added **CP-059** — Egress whitelist governs agent network access per policy: declares glob pattern semantics, backward-compatible default (absent = unrestricted), and propagation obligation into `LaunchSpec.egress_whitelist` at claim time per [handler-contract.md §4.11.HC-048b]. Runtime enforcement beyond provisioning is deferred to S06 sandbox subsystem. New ID: CP-059. No existing requirements renumbered. Cross-spec coordination: handler-contract.md gains HC-048b (provisioning enforcement); event-model.md §8.3.8 `skills_provisioned` gains `rejected_skills[]?` field. |
| 2026-05-31 | 0.4.1 | agent (kerf `credfence` work) | Additive Budget-scope amendment. CP-022's `scope` enum extended to `{per_role, per_run, per_state, handler_account}` and an INFORMATIVE note added explaining that a `handler_account`-scoped Budget is the per-handler-account ceiling whose exhaustion is handler-fatal per [handler-pause.md HP-012] (resolving handler-pause §13 deferred item #3), reconciling the `scope` field name against HP-012's `budget_scope` wording. The note clarifies that the harmonik unified per-day cap of [cognition-loop.md CL-090] is NOT a registered CP-022 Budget (USD/day is not a representable `BudgetResource`); rather its exhaustion event carries `scope = handler_account` so HP-012 can discriminate the account-scoped variant. No existing requirement renumbered; CP-001 single-primitive invariant and CP-005 per-Kind table unchanged (an enum value is not a new primitive). Source: kerf `credfence` change design. |

## A. Appendices

### A.3 Rationale

**Why a single primitive with four Kinds rather than four separate types.** Gate, Hook, Guard, and Budget share a registration path, a policy-source ownership (S02), a per-daemon registry, and a set of evaluator/trigger/outcome-action concepts. Exposing four separate primitives would duplicate the registration surface, the policy-YAML loader, and the axis-tagging obligation. The unification holds at the primitive level (one struct, one registry, one lifecycle) while the Kind-specific semantics are explicit and non-interchangeable per the §4.1.CP-005 table. The earlier three-Kind proposal (Gate, Hook, Guard) omitted Budget; adding Budget as a fourth Kind is consistent with its registration path and its event-based observability but distinct from Gate at the trigger layer (Budget fires on dispatch and per-chunk accrual, not on transition attempt).

**Why Guards are mechanism-only.** A Guard reorders the edge set that the deterministic cascade of [execution-model.md §4.10] operates on. Cognition in a Guard would let a model decide which transitions the system considers — a direct ZFC violation per [architecture.md §4.2]. Routing cognition into selection logic also breaks the replay-safety story: a cognition-reordered edge list has no persisted-verdict capture analog (the cascade's next step consumes the reordered list immediately). The cleaner design is to let Gates carry cognition (they operate on the already-selected transition and can persist their verdict) while keeping Guards deterministic.

**Why cognition-tagged evaluators persist verdicts rather than recomputing on replay.** Replay-safety is a first-class invariant (CP-INV-003). Re-invoking a model during replay introduces nondeterminism (model responses drift across calls, even at temperature 0 across model-version bumps), storage duplication (each replay consumes tokens), and a pathological failure mode where a model now-unavailable breaks replay. The persisted-verdict pattern makes cognition's output behave like any other durable trace record: written once, read many times, migrate-able under N-1 schema evolution.

**Why `expr-lang/expr` and not a custom grammar.** A custom grammar duplicates the work of maintaining a lexer, parser, AST walker, type system, and safety model (no I/O, bounded cost, no recursion explosion). `expr-lang/expr` is Go-native, maintained, has a published safety profile, and ships with a type-checking mode that enables workflow-ingest-time validation of expressions against the policy environment schema. Embedding a general-purpose language (Starlark, Lua) overshoots the policy-expression need and opens a surface inviting cognition to creep into selection logic through clever expression authorship. The `expr-lang/expr` adoption is narrow enough to stay in the mechanism layer and rich enough to express every policy expression this foundation has named.

**Why Budget is a Kind rather than a Gate subtype.** Budget's trigger is dispatch + per-chunk accrual, not transition attempt. A Gate's evaluator input is the candidate transition; Budget's evaluator input is the counter state. Forcing Budget into a Gate's shape would either (a) redefine what a Gate's trigger means (breaking the per-Kind table), or (b) require a pseudo-transition-attempt at every chunk boundary (runtime waste). Making Budget its own Kind keeps the primitive coherent: the unification is "one registry, one lifecycle, one set of common fields"; the semantics per Kind are specialized because the real-world triggers differ.

**Why `Kind` is peerage at the registration layer, not structural peerage.** A round-1 critic observed that the §4.1.CP-005 per-Kind table shows disjoint trigger / evaluator-input / outcome-action columns, and argued this means the four Kinds are NOT structural peers — so calling them "Kinds" (a taxonomy word) over-promises. The observation is correct about structural disjointness; the word "Kind" here is deliberately registration-layer peerage: one struct, one registry, one lifecycle, one axis-tagging obligation, one `name` namespace, one `schema_version` discipline. That peerage IS what Gate/Hook/Guard/Budget share, and it is load-bearing: consuming subsystems (S01, S02, S05) use a single dispatch shape to route any ControlPoint by `kind` + `trigger`. Demoting Budget to a sibling primitive registered in a separate registry would duplicate the registration surface, the YAML loader, and the axis-tagging discipline, which buys nothing except the absence of the word "Kind." Keeping four Kinds and making the registration-layer peerage explicit in this rationale is the cheaper fix than renaming the enum. Future additions (e.g., a Throttle primitive) land as a fifth Kind iff they satisfy the same registration-layer peerage test; a new primitive that does NOT share the registry / lifecycle / axis-tagging surface should be a sibling primitive in a different spec, not a Kind.

**Why `expr-lang/expr` and not cel-go.** CEL (Common Expression Language; `cel-go` in the Go ecosystem) is the obvious comparator — used by Kubernetes admission webhooks, Envoy proxy config, OPA-adjacent systems — and shares every property cited in the INFORMATIVE block under CP-034: Go-native, maintained, side-effect-free by construction, bounded evaluation cost, type-checkable. Evaluated on four axes: (a) **grammar safety** — both are safe by construction; tie. (b) **type system and ingest-time validation** — CEL's type system is protobuf-informed (strong structural typing against proto schemas); `expr-lang/expr` offers Go-struct-based type checking via `expr.Env()` plus `expr.AsBool()` / `expr.AsInt()` coercion. For the Run/Outcome/Event records declared in §6.4 (Go structs, not protobuf), `expr-lang/expr`'s Go-struct environment is a more direct fit; CEL would require a proto-schema mirror of every evaluator-input type. (c) **ecosystem familiarity** — CEL is broadly known in cloud-native infra; `expr-lang/expr` is less widespread but has a shallower learning curve and no proto-schema prerequisite. (d) **expressiveness vs. cognition-creep surface** — `expr-lang/expr` includes pipeline operators and map/filter builtins that make policy expressions slightly more permissive; CEL is more constrained. This spec already declares expressions side-effect-free and bounded, so the marginal cognition-creep risk is contained by §4.7.CP-034 + CP-034b (evaluation-cost ceiling, below). Net: CEL would be defensible; `expr-lang/expr` is chosen because the Go-struct-native environment removes the proto-schema burden and the foundation has no other dependency on CEL or protobuf. If harmonik later adopts protobuf-defined run/outcome types or integrates with a CEL-using ecosystem (Kubernetes admission, OPA policy), switching grammars is a mechanical migration release per CP-038 — §6.4's environment is the only thing that changes.
