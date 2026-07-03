# C3 Spec Draft — handler-contract.md §4.2a + execution-model.md EM-005 v2 / EM-005a / EM-005b (Pass-5, phase-3-dot)

> Pass-5 spec-draft for **component C3** of `phase-3-dot`. This document is the integration-ready draft prose for the three additive extensions C3 commits to: a new handler-facing per-node-type Outcome surface section (HC §4.2a), an additive v1→v2 bump to the `Outcome` schema adding `failure_class` (EM-005 v2), and a new `OutcomeKind` value `gate_decision` with its payload record (EM-005a extension + new EM-005b). Plus a new context-update discipline section (HC §5.x) committing the D8 per-workflow registered-key list with warn-and-drop semantics.
>
> **Source of truth:** `04-design/C3-handler-contract-outcome-design.md` (locked at pass-4 review round-2 APPROVE). All structural / placement decisions trace there.
>
> **Spec ownership split** (preserved from §0 of the design): the `Outcome` record is normatively owned by `specs/execution-model.md §4.1 EM-005` + EM-005a. `specs/handler-contract.md` cross-checks and adds handler-facing per-node-type expectations. Schema-shape changes land in EM; handler-author obligations land in HC. C3 respects this split absolutely.

---

## 0. Spec-position summary (for the pass-6 integration reviewer)

Three integration targets, three independent edits:

| Target spec | Edit kind | New ID(s) | What it adds |
|---|---|---|---|
| `specs/handler-contract.md` (new §4.2a; new §5.6) | Additive section + new requirements | HC-058, HC-059, HC-060, HC-061, HC-062 | Per-node-type Outcome surface table; `failure_class` handler-hint rule; `kind = gate_decision` handler-emission rule; `context_updates` discipline. |
| `specs/execution-model.md §4.1 EM-005` | Additive field on existing record (schema bump v1→v2) | EM-005 amended; new EM-005c | New optional `failure_class` field on FAIL Outcome; rationale and N-1 readability bump from v0.3.3 → v0.3.4. |
| `specs/execution-model.md §4.1 EM-005a` + new EM-005b | Additive enum value + new payload-record requirement | EM-005a amended; new EM-005b | Adds `gate_decision` to `OutcomeKind`; binds the new payload record. |
| `specs/control-points.md §6.1.8` (Gate payload section) | New sub-record schema | new CP-058 | Declares `GateDecisionPayload` schema, the five-field rationale record referenced by EM-005b. |

### GateDecisionPayload location — choice committed

**Decision:** `GateDecisionPayload` lives in `specs/control-points.md §6.1.8` (the Gate payload schemas section), declared under a new requirement **CP-058**. EM-005b cites it by name; EM does NOT redeclare its fields.

**Rationale** (resolves design OQ-C3-1): gate-decision semantics are CP-bound. Every field of the record (`policy_id`, `decision`, `decision_actor`, `decision_evidence_ref`, `resolution_signal_id`) is sourced from control-points concepts — `policy_id` resolves to a CP `name` per CP-002, `decision_actor` enumerates governance roles per CP §4.7, `resolution_signal_id` is the EM-042a gate-resolution-signal correlation owned by control-points. Co-locating the record schema next to CP-006/CP-009 keeps the read path local for control-points authors and avoids the precedent of EM owning a CP-shaped record. EM cites the record by name (the same pattern EM-005a uses for `VerdictEvent`, which is owned by `reconciliation/schemas.md §6.1`, not by EM). This is the design's leaned position.

### Citation correction (resolves design OQ-C3-3)

The original brief cited "EM-046c" for gate-resolution-signal correlation. The actual normative requirement is **EM-042a** ("Gate-deny continuation protocol"). This draft uses EM-042a wherever the citation would appear; no occurrence of "EM-046c" remains.

---

## 1. EDIT A — `specs/handler-contract.md` new §4.2a "Outcome surface (handler-facing cross-check)"

> **Insertion point in handler-contract.md:** immediately after §4.2 HC-010 ("Session log path emission", line ~210), before §4.3 ("Concurrency model", line ~216). The new section §4.2a sits at the end of §4.2 because it cross-checks the wire-protocol Outcome delivery requirement HC-008 lives in §4.2.

### 4.2a Outcome surface (handler-facing cross-check)

The `Outcome` type is owned normatively by [execution-model.md §4.1 EM-005] + EM-005a. Handlers MUST emit Outcomes that conform to that schema; the present section is informative for handler authors **except** for the per-node-type emission obligations enumerated in HC-058 below, which are normative. Where this section and EM-005 conflict, EM-005 wins; this section catalogues handler-facing obligations only.

The handler subprocess delivers the `Outcome` per §4.2.HC-008 as the payload of the final `outcome_emitted` progress-stream message. The watcher (§4.3.HC-011) is the publisher of the bus-side `outcome_emitted` event; the handler does not write the bus directly. Schema-shape changes to the Outcome record are not handler-contract changes — they are coordinated via EM-005a's amendment protocol per [architecture.md §4.6] and surface here only as new emission obligations.

#### HC-058 — Per-node-type Outcome emission obligations

The Outcome a handler emits MUST conform to the per-node-type emission obligations in the table below. The table is normative for the MUST / MUST-NOT clauses each cell names; cells marked "MAY" or "permitted" are informative.

The node-type taxonomy is the collapsed 4-type taxonomy from [execution-model.md §4.2 EM-006] reduced for handler-contract purposes (`control-point` is not a handler-dispatched node per D3 and does not appear in the table). The taxonomy axis here is the **node type that produced the run-dispatch**, not the workflow-graph type — sub-workflow nodes do not themselves dispatch handlers; their expanded children do (see the sub-workflow row note).

| Node type | `status` values | Allowed `payload.kind` values | `failure_class` semantics | `preferred_label` semantics | `context_updates` discipline |
|---|---|---|---|---|---|
| **agentic** | `SUCCESS`, `FAIL`, `RETRY`, `PARTIAL_SUCCESS` | `default`; `reconciliation_verdict` ONLY when the agentic node is a reconciliation investigator per [reconciliation/spec.md] | OPTIONAL on `FAIL`; handler-emitted value is a HINT; daemon back-fills from HC-020 sentinel if absent; daemon override is authoritative per HC-059. | MAY carry a verdict string when the workflow uses verdict-routing per the D-verdict-surfacing pattern (e.g. `"APPROVE"`, `"REQUEST_CHANGES"`, `"BLOCK"`). MUST NOT be used as machine-readable rationale carrier — that is `payload`'s role per HC-060. | Handler MAY emit any string keys; only **registered keys** per the active workflow's context-key list (see HC-062) are routable as edge-LHS terms per [execution-model.md §4.10 EM-041]. |
| **non-agentic** (tool-style) | `SUCCESS`, `FAIL`, `RETRY`, `PARTIAL_SUCCESS` | `default` only at MVH. Future tool-style payload kinds extend EM-005a per [architecture.md §4.6]. | OPTIONAL on `FAIL`; same handler-hint + daemon-back-fill rule as agentic. | MAY carry a routing hint string; same prose-vs-rationale split as agentic. | Same registered-key discipline as agentic; tool outputs typically populate `context_updates` with captured summaries. |
| **gate** | `SUCCESS` (≡ permit) OR `FAIL` (≡ deny). `RETRY` and `PARTIAL_SUCCESS` MUST NOT be emitted — gate evaluation is a single-shot decision per [control-points.md §4.2 CP-006]. | `default` OR `gate_decision`. `kind = gate_decision` is REQUIRED when the gate's audit posture (per [control-points.md §4.8 CP-040]) requires structured rationale capture; otherwise `default` is acceptable. A deny emitted as `default` without rationale capture is permitted at MVH; pass-2-and-beyond may tighten this. | OPTIONAL on `FAIL`; the failure class for a gate deny is informationally distinct from the gate rationale and MAY be set alongside `kind = gate_decision` (e.g. a `budget_exhausted` deny carries both `failure_class = budget_exhausted` and `kind = gate_decision` with `policy_id = budget-gate`). | The verdict-routing pattern: `preferred_label = "permit"` on `SUCCESS`, `"deny"` on `FAIL`. This is the cascade's routing carrier; `payload` is the audit carrier. | Gate evaluators SHOULD NOT emit `context_updates` (a gate's job is to permit / deny, not to mutate context); when emitted, the same registered-key discipline applies. |
| **sub-workflow** | N/A — sub-workflow boundary nodes MUST NOT emit an Outcome. | N/A | N/A | N/A | N/A. The parent's cascade observes the last-expanded-node's Outcome verbatim per [execution-model.md §4.10 EM-036a], including `kind`, `payload`, `failure_class`, and `context_updates`. Sub-workflow handler authors MUST NOT write a synthetic terminal Outcome at the boundary; the boundary is a graph-level construct, not a handler dispatch site. |

**Testability note** (mirrors §4.2.HC-006a): each row is independently testable against the twin handler harness per §4.8. A conforming handler asserts MUST clauses by Outcome shape (e.g. a gate handler emitting `RETRY` is non-conforming and the twin MUST flag it).

Tags: mechanism

#### HC-059 — `failure_class` is a handler hint; daemon classification is authoritative

A handler emitting `Outcome.status = FAIL` MAY set `Outcome.failure_class` to one of the six values declared in [execution-model.md §8] (`transient`, `structural`, `deterministic`, `canceled`, `budget_exhausted`, `compilation_loop`). The handler-emitted value is a HINT; the daemon back-fills `failure_class` from the §4.5 HC-020 sentinel classification path when the handler omits it, and the daemon's classification is AUTHORITATIVE when handler and daemon disagree.

On handler / daemon disagreement, the daemon MUST log the disagreement (event-model record `failure_class_disagreement` carrying both values plus the run-id and node-id) and proceed with the daemon's classification. At MVH the disagreement is log-only; whether to promote disagreement to a reconciliation Cat 6 escalation is OQ-HC-013 (see §11).

The value `compilation_loop` is **daemon-only**: handlers MUST NOT self-classify as `compilation_loop` because compilation-loop detection requires daemon-side state (per-node attempt history per [execution-model.md §4.10 EM-046b]) the handler does not have. A handler-emitted `failure_class = compilation_loop` MUST be overridden to `structural` by the daemon (treated as a malformed hint) and logged via the same `failure_class_disagreement` event.

The `failure_class` field is carrier-only; classification authority remains with the daemon per HC-020 and [execution-model.md §8]. EM-005 v2's `failure_class` row (per EDIT B below) is the schema home; this requirement governs handler-side emission discipline.

Tags: mechanism

#### HC-060 — `kind = gate_decision` is the rationale carrier for gate Outcomes

Handlers emitting Outcomes from a gate node (per HC-058) MUST follow the gate-decision rationale-carrier rule: when the gate's policy requires structured rationale capture per [control-points.md §4.8 CP-040], the handler MUST emit `outcome.kind = "gate_decision"` with `payload` conforming to the `GateDecisionPayload` schema declared in [control-points.md §6.1.8 CP-058]. When rationale capture is not required by policy, `kind = default` is permitted and `payload` MUST be absent.

The five `GateDecisionPayload` fields (per CP-058) are: `policy_id`, `decision`, `decision_actor`, `decision_evidence_ref` (optional), `resolution_signal_id` (optional; correlates the EM-042a gate-pending resolution signal). Handlers MUST populate `policy_id` and `decision` whenever they emit `kind = gate_decision`; the other fields are populated as available. `decision = permit` MUST correlate with `status = SUCCESS`; `decision = deny` MUST correlate with `status = FAIL`; cross-pairings are non-conforming.

The cascade per [execution-model.md §4.10 EM-041] reads `outcome.status` and `outcome.preferred_label` only; `outcome.payload.*` is NOT a legal LHS in edge conditions per [execution-model.md §4.10 — see D4 edge-condition LHS whitelist]. Workflows that need to discriminate gate-decision outcomes from non-gate outcomes structurally MAY route on `outcome.kind = "gate_decision"` (the discriminator IS legal LHS per D4); `payload` subfields are not LHS-routable.

`notes` (per [execution-model.md §4.1 EM-005]) is freeform string and MUST NOT be parsed by the engine. Handlers that need machine-readable gate rationale MUST use `payload` under `kind = gate_decision` — leaking rationale into `notes` is non-conforming because it defeats audit-replay (the persisted-verdict envelope hash of [control-points.md §4.8 CP-040a] depends on structured payloads, not freeform prose).

Tags: mechanism

#### HC-061 — Sub-workflow boundary handlers MUST NOT emit an Outcome

Per HC-058's sub-workflow row, a node of type `sub-workflow` is a graph-level expansion construct; it does NOT dispatch a handler subprocess. Therefore handler authors writing the implementation of a sub-workflow's *expanded children* MUST NOT emit a synthetic Outcome at the sub-workflow boundary. The parent's cascade observes the last-expanded-node's Outcome verbatim per [execution-model.md §4.10 EM-036a]; emitting a synthetic boundary Outcome would shadow the verbatim-propagation rule and is a structural error.

Twin parity (§4.8) MUST flag any handler that emits an Outcome on a node bound to a sub-workflow expansion as non-conforming with sentinel `ErrStructural` and sub-reason `subworkflow_boundary_emit`.

Tags: mechanism

#### Existing §6.1 RECORD Outcome cite update (incidental)

Update the existing cite-only comment block in handler-contract.md §6.1's `RECORD Outcome` entry from `-- see [execution-model.md §6.1 Outcome]` to `-- see [execution-model.md §6.1 Outcome v2]`. No fields enumerated locally; pure cite.

---

## 2. EDIT B — `specs/execution-model.md` EM-005 schema bump v1 → v2 (additive `failure_class` field)

> **Edit kind:** additive amendment to EM-005's existing field list + new requirement EM-005c documenting the bump. No existing field is renamed, removed, retyped, or repositioned. Schema-version bumps from v0.3.3 → v0.3.4 (the same N-1 protocol that admitted EM-005a's `kind`/`payload` extension at v0.3.3 — see EM-005a's closing paragraph).

### 2.1 Amendment to EM-005's field-list prose (in §4.1)

Append the following sentence to the existing EM-005 paragraph (immediately after the existing field list):

> Additionally, an `Outcome` MAY carry `failure_class ∈ {transient, structural, deterministic, canceled, budget_exhausted, compilation_loop}` (the `FailureClass` enum declared in §8), present ONLY when `status = FAIL`. The field is a handler-emitted HINT; the daemon back-fills from the §8 sentinel classification path per [handler-contract.md §4.5 HC-020] when the handler omits the field, and the daemon's classification is authoritative on disagreement per [handler-contract.md §4.2a HC-059]. The field is carrier-only; classification authority remains with the daemon.

### 2.2 New requirement EM-005c — Outcome schema v2 bump

#### EM-005c — Outcome schema v2: `failure_class` additive field

The `Outcome` record is bumped from schema v0.3.3 → v0.3.4 to admit the optional `failure_class` field per the EM-005 paragraph above. The bump is strictly additive: no existing field changes name, type, default, or position; no §4 requirement that predates v0.3.4 reads the new field.

Consumers of the v0.3.3 shape remain conforming: `failure_class` defaults to absent on emission, and the daemon-side classification path of [handler-contract.md §4.5 HC-020] (the sentinel classifier) remains the authoritative input for the failure taxonomy of §8. The v0.3.4 field is a carrier added so that `failure_class` becomes a first-class edge-condition LHS term per the D4 whitelist — the cascade of §4.10 EM-041 MAY consult `outcome.failure_class` as an edge predicate input.

N-1 readability per §6.4 is preserved: a v0.3.3 reader observing a v0.3.4 outcome with `failure_class` present MUST treat the field as unknown-and-optional and fall through to sentinel-based classification per HC-020. A v0.3.4 reader observing a v0.3.3 outcome with `failure_class` absent MUST consult HC-020 for the classification (same path as today — the field's absence is the default state).

`compilation_loop` is a daemon-only classification per HC-059; a v0.3.4 reader MAY refuse a handler-emitted `failure_class = compilation_loop` and override to `structural` with a logged disagreement.

Tags: mechanism

### 2.3 Amendment to §6.1 `RECORD Outcome` schema

Append the new field row to the existing `RECORD Outcome` block in §6.1 (insertion immediately after the `notes` row, before the `kind` row to keep optional-typed fields grouped):

```
    failure_class       : FailureClass | None  -- present only when status = FAIL; §8 enum; daemon-back-filled from HC-020 sentinel when handler omits; §4.1.EM-005c
```

And add the enum reference near the existing `OutcomeKind` enum declaration:

```
ENUM FailureClass:
    transient | structural | deterministic | canceled | budget_exhausted | compilation_loop
    -- declared in §8; surfaced here as Outcome's optional carrier per §4.1.EM-005c
```

(The enum is already declared informally across §8.1 – §8.6 and the compilation-loop addition in §8.x; this `ENUM` block in §6.1 is the schema-side citation, not a re-declaration. If §8 does not already wrap the six values in a named enum, pass-6 should align the §8 prose to use the `FailureClass` name.)

### 2.4 Schema-version-bump rationale paragraph

Append to §6.4 (schema evolution section) — a one-paragraph rationale row:

> **v0.3.3 → v0.3.4 — `Outcome.failure_class` additive.** EM-005c adds the optional `failure_class` field to the `Outcome` record. v0.3.3 readers treat the field as unknown-and-optional and fall through to sentinel-based classification per [handler-contract.md §4.5 HC-020]; v0.3.4 readers consult the field as a hint and override with the daemon's classification on disagreement. The bump is additive — no existing field is renamed or removed — and N-1 readability per §6.4 is preserved on both directions.

---

## 3. EDIT C — `specs/execution-model.md` EM-005a discriminator extension (`gate_decision` value) + new EM-005b

> **Edit kind:** additive enum-value extension to EM-005a's `OutcomeKind` enum + new requirement EM-005b binding the new payload record. The new `GateDecisionPayload` record itself is declared in EDIT D below (control-points.md §6.1.8 CP-058), per the §0 spec-position decision.

### 3.1 Amendment to EM-005a's enum

Replace the existing EM-005a opening sentence:

> An `Outcome` MUST carry a `kind ∈ {default, reconciliation_verdict}` discriminator …

with:

> An `Outcome` MUST carry a `kind ∈ {default, reconciliation_verdict, gate_decision}` discriminator …

Append a new bullet to EM-005a's "discriminator semantics" enumeration (after the existing `reconciliation_verdict` bullet):

> - `kind = gate_decision` — the outcome carries a gate evaluator's structured decision rationale. `payload` MUST be the `GateDecisionPayload` record per [control-points.md §6.1.8 CP-058]; this spec does NOT redeclare the record's fields. Per EM-005b below, the gate-evaluator handler MUST emit this kind when the gate's policy requires structured rationale capture per [control-points.md §4.8 CP-040]; otherwise `kind = default` is permitted. The `GateDecisionPayload` is opaque to the cascade and to the durability decision (the cascade reads `status`, `preferred_label`, and `kind` only; `payload.*` is not a legal edge-condition LHS per the D4 LHS whitelist). The handler-side gate emission discipline is normative per [handler-contract.md §4.2a HC-060].

### 3.2 Update the unknown-`kind` reconciliation routing paragraph

The existing EM-005a sentence "The enum is closed at MVH; future outcome variants … extend the enum via the amendment protocol per [architecture.md §4.6] …" remains correct; the closing-and-extension protocol is precisely how `gate_decision` is being added. Append a parenthetical clarification:

> (Example application of the amendment protocol: the `gate_decision` value is added at v0.3.4 per EM-005b and is paired with the `GateDecisionPayload` record at [control-points.md §6.1.8 CP-058]; the existing N-1 routing rule applies — a v0.3.3 reader encountering `kind = gate_decision` MUST route to reconciliation Cat 6a, NOT silently degrade to `default`.)

### 3.3 New requirement EM-005b — Gate-decision Outcomes carry structured rationale via `kind = gate_decision`

#### EM-005b — Gate-decision Outcome variant

A gate-evaluator handler (per [handler-contract.md §4.2a HC-058], gate row) emitting an Outcome that the gate's policy designates as requiring structured rationale capture per [control-points.md §4.8 CP-040] MUST emit `Outcome.kind = "gate_decision"` and `Outcome.payload = GateDecisionPayload(...)` per [control-points.md §6.1.8 CP-058]. When rationale capture is not required by policy, `Outcome.kind = "default"` is permitted and `payload` MUST be absent.

The five `GateDecisionPayload` fields (declared in CP-058) carry the gate's audit record: `policy_id` (the gate's registry name per [control-points.md §4.1 CP-002]), `decision` (`{permit, deny}`), `decision_actor` (`{policy_engine, operator, timeout}`), `decision_evidence_ref` (optional audit pointer; commit SHA, event correlation id, etc.), and `resolution_signal_id` (optional; correlates the EM-042a gate-resolution signal when the daemon resolves a `gate-pending` sub-state). `decision = permit` MUST correlate with `status = SUCCESS`; `decision = deny` MUST correlate with `status = FAIL`.

The cascade per §4.10 EM-041 MAY route on `outcome.kind = "gate_decision"` (the discriminator IS a legal edge-condition LHS); `payload.*` subfields are NOT legal LHS. Workflows that need to discriminate gate-decision outcomes from non-gate outcomes for routing purposes use the discriminator; workflows that need audit access to the payload consult it via the persisted-verdict path per [control-points.md §4.8 CP-040], not via the cascade.

`kind = gate_decision` and the `failure_class` field per EM-005c are orthogonal carriers. A gate deny due to budget exhaustion MAY emit ALL of: `status = FAIL`, `failure_class = budget_exhausted`, `kind = gate_decision`, `payload = GateDecisionPayload{policy_id: "budget-gate", decision: deny, ...}`. The two fields encode different facts (classification vs. rationale) and compose without conflict.

The schema-version bump for the `gate_decision` enum value is the same v0.3.3 → v0.3.4 bump as EM-005c (the failure_class addition); the two extensions land coordinated at v0.3.4. Per the closing paragraph of EM-005a, adding a discriminator value is itself an additive schema change; v0.3.3 readers route unknown `kind = gate_decision` to reconciliation Cat 6a per [reconciliation/spec.md §8.11].

Tags: mechanism

### 3.4 Sub-workflow propagation note (cross-ref to EM-036a)

No edit to EM-036a itself; the verbatim-propagation rule already covers `kind` and `payload`. The pass-6 integration reviewer may add a one-sentence cross-ref in EM-036a's prose if it improves discoverability:

> A sub-workflow whose last-expanded-node terminates with `kind = gate_decision` propagates the `GateDecisionPayload` verbatim to the parent; parent edges that don't expect the kind simply do not match on `outcome.kind = "gate_decision"` and route by `status` / `preferred_label` per the cascade of §4.10.

This is informative only and not required for C3's scope.

---

## 4. EDIT D — `GateDecisionPayload` declaration: DEFERRED to C4 §6.1.8 CP-058

> **Spec-position change (cross-component sweep, post-pass-5 draft):** C4 (control-points.md binding extension) took ownership of the `GateDecisionPayload` record declaration at §6.1.8 with requirement CP-058. C3 cites that location for every payload reference (see EDIT C / HC-060) but does NOT redeclare the record. The pre-sweep version of this EDIT D attempted a parallel declaration at §6.1.1 with CP-062; that has been retracted to avoid a double-declaration. EDIT D in this draft is intentionally empty other than this note. Pass-6 integration applies only EDITs A, B, C, E from this draft; C4's pass-5 draft owns the control-points.md changes including GateDecisionPayload.

---

## 5. EDIT E — `specs/handler-contract.md` new §5.6 (Context Update Discipline)

> **Insertion point:** within the existing §5 ("Invariants") section of handler-contract.md (which starts at line ~786). The discipline rule fits at §5 because it is an invariant about handler emission rather than a wire-protocol shape (which would belong in §4.2). New requirements: HC-062.

### 5.6 Context update discipline (per-workflow registered keys)

The `Outcome.context_updates` map (per [execution-model.md §4.10 EM-041a]) is a free-form key-value map at the wire-protocol level. Handlers MAY emit any string keys. However, only keys that are **registered for the active workflow** are routable as edge-condition LHS terms per the D4 LHS whitelist; unregistered keys are silently dropped from the routing-relevant context with a daemon-side WARNING log.

#### HC-062 — Context-update registered-key list is per-workflow; unregistered keys warn-and-drop

Every workflow MUST declare its `context_keys` registered-key list as a workflow-graph attribute. The list's declaration site is the `.dot` graph-level attribute `context_keys = "key1,key2,..."` per [workflow-graph.md §C1 design — context-key registry], NOT a separate YAML file (rationale: the keys are workflow-routing-relevant and belong in the workflow definition, co-located with the edges that route on them; per C1 design §11, the workflow validator is the natural enforcement site).

A handler emitting `Outcome.context_updates` containing a key NOT in the active workflow's registered list MUST trigger the following daemon-side discipline (resolves design OQ-C3-2 — lean: warn-and-drop):

1. The daemon MUST log an event `context_update_unregistered_key` carrying `{run_id, node_id, key, value_type, workflow_id}`; the value itself is NOT logged (it may contain PII).
2. The daemon MUST drop the key from the run's shared context update — the post-update context that the §4.10 EM-041a cascade observes does NOT include the unregistered key.
3. The handler-emitted Outcome remains otherwise conforming; the unregistered key does NOT cause the Outcome to fail validation or classification.

The warn-and-drop posture (rather than reject-as-structural) is deliberate: handler authors should NOT be obligated to know the workflow author's edge-LHS surface. The registered-key list is a routability registry, not an emission-legality registry. A handler that emits a key the workflow does not consume is harmless; the warning surfaces drift so workflow authors can either extend the registry or remove the emission.

Registered keys MUST conform to the identifier grammar declared by C1 (a subset of the policy-expression grammar of [control-points.md §6.4]); the validator rejects malformed registered-key declarations at workflow ingest. Reserved keys per [execution-model.md §4.3 EM-012] (`iteration_count`, `last_verdict`, `claude_session_id`, `last_diff_hash` under `workflow_mode = review-loop`) are implicitly registered for review-loop workflows and MUST NOT be re-declared in the workflow's `context_keys` attribute (re-declaration is a validator error).

Tags: mechanism

---

## 6. Cross-reference additions (catalogue)

The following one-liner cross-refs MUST be added at pass-6 integration:

| Add to | Cross-ref | Justification |
|---|---|---|
| `specs/handler-contract.md` §6.1 (existing `RECORD Outcome` cite-only block) | Update target from `§6.1 Outcome` to `§6.1 Outcome v2` | EM-005 bump per EDIT B. |
| `specs/handler-contract.md` §9.1 ("Depends on") | Add bullet: `control-points.md §6.1.8 CP-058 (GateDecisionPayload schema)` | HC-060 cites the record. |
| `specs/handler-contract.md` §9.3 ("Co-references") | Add bullet: `execution-model.md §4.1 EM-005b (gate-decision Outcome variant)` | Co-owned with EM. |
| `specs/execution-model.md` §EM-005a opening cross-refs | Add: `gate_decision payload — [control-points.md §6.1.8 CP-058]` | EM cites the record by name. |
| `specs/execution-model.md` §EM-042a closing prose | Add: "When the daemon resolves a `gate-pending` sub-state via the resolution signal, the daemon SHOULD populate `Outcome.payload.resolution_signal_id` with the inbound signal's id per [control-points.md §6.1.8 CP-058]." | Closes the EM-042a / CP-058 round-trip. |
| `specs/control-points.md` §6.1.1 sub-section header | Add a paragraph distinguishing the registration-side `Gate` payload from the runtime emission-side `GateDecisionPayload` record per CP-058. | Avoids reader confusion between two records both named "Gate payload." |

---

## 7. Open questions surfaced for pass-6

- **OQ-HC-013 — Handler/daemon `failure_class` disagreement: log-only or escalate?** (Carries forward from design OQ-C3-4.) **Lean:** log-only at v0.3.4. Promote to Cat 6 escalation if observed disagreement rate exceeds a threshold during pass-2 operational hardening. Owner: HC.
- **OQ-EM-016 — `GateDecisionPayload.schema_version` and EM/CP coordinated bumps.** When `GateDecisionPayload` evolves (v1 → v2), does EM-005b need a coordinated bump, or is the payload's schema versioned independently? **Lean:** independently — the payload is opaque to EM. Owner: EM.
- **OQ-CP-007 — Should `decision_actor = operator` carry the operator's role name** (resolving against `architecture.md §4.8`)? Adding a `decision_actor_role` sub-field would make audit trails richer but adds a field that requires registry resolution. **Lean:** defer to a v2 bump of `GateDecisionPayload`. Owner: CP.
- **OQ-HC-014 — `context_keys` declaration site: graph-level DOT attribute vs. workflow YAML companion.** This draft commits the DOT graph-level attribute per HC-062, with the justification that the keys are routing-relevant and co-locate with edges. C1's design pass (in flight) is the natural cross-check; if C1 lands the registry in a companion YAML, HC-062's prose must update to cite that location. Owner: C1 (resolves at pass-6 integration when C1 and C3 reconcile).
- **OQ-HC-015 — Twin-parity test coverage for gate-decision Outcomes.** §4.8 twin-parity asserts the handler twin emits conformant Outcomes; HC-060's gate-decision emission rule needs explicit twin coverage. **Lean:** add to §10.2 test-surface obligations at pass-6. Owner: HC.

---

## 8. Requirements traceability — C3 design § 4 mapping

| C3 Requirement (design §4) | Spec-draft section |
|---|---|
| G1 partial — per-node-type outcome surface | EDIT A §4.2a table (HC-058) |
| G5 partial — `failure_class` field on Outcome | EDIT B (EM-005 v2 amendment + EM-005c) + HC-059 |
| Gap-fill #1 — gate-node rationale | EDIT C (EM-005a + EM-005b) + EDIT D (CP-058) + HC-060 |
| D8 lean — per-workflow registered-key list | EDIT E (HC-062) |
| OQ-C3-1 — GateDecisionPayload location | §0 commit: lives in control-points.md §6.1.8 CP-058 |
| OQ-C3-2 — warn-vs-reject on unregistered keys | HC-062: warn-and-drop (committed lean) |
| OQ-C3-3 — EM-046c citation error | §0 + EDITS: all gate-resolution-signal references cite EM-042a |
| OQ-C3-4 — handler/daemon failure_class disagreement | HC-059 + OQ-HC-013 (carry-forward) |
| OQ-C3-5 — gate_decision allowed for both permit and deny | EM-005b + CP-058: both are valid; `decision_actor` distinguishes |

---

## 9. Style + integration notes for the pass-6 integration reviewer

- **Requirement-ID numbering.** New HC IDs (HC-058 … HC-062) extend from the highest-existing HC-057. New EM IDs (EM-005b, EM-005c) intentionally use the lettered-suffix pattern EM-005a already established for in-section amendments (rather than EM-058+ which would land far from EM-005's home and lose locality). New CP ID (CP-058) extends from the highest-existing CP-061-or-thereabouts; pass-6 should verify the next available CP slot and renumber if conflict.
- **N-1 readability.** Both EDIT B (failure_class) and EDIT C (gate_decision) bump v0.3.3 → v0.3.4 coordinated; no v1-reader corpus exists pre-MVH so the bump is risk-free, but the N-1 protocol is followed regardless for forward consistency.
- **No retypes.** No existing field is renamed, removed, or retyped in any of the four edits. Every change is purely additive.
- **Sub-workflow propagation.** The verbatim-propagation rule of EM-036a already covers `kind`, `payload`, and (after EDIT B) `failure_class`; no edit to EM-036a required, but a one-sentence informative note is suggested in §3.4.
- **Outcome-spine integration (EM-027).** No edit required; EM-027 already names "handler outcome → hook dispatch → gate evaluation → transition selection → event emission" as one integrated flow. The new fields ride that spine unchanged.
- **Twin parity.** §4.8 of handler-contract.md (twin parity) gains test obligations from HC-058, HC-060, and HC-061. Pass-6 should add the per-node-type emission obligations to §10.2 ("Test-surface obligations") as a new conformance test family.

---

## 10. Footer — Reviewer note

This draft was written against the pass-4 design `04-design/C3-handler-contract-outcome-design.md` (locked APPROVE round-2) and the actual current text of:
- `/Users/gb/github/harmonik/specs/handler-contract.md` (HC-001 … HC-057, §§ 1-12 + A.3)
- `/Users/gb/github/harmonik/specs/execution-model.md` (EM-001 … through current; verified EM-005 / EM-005a / EM-042a citations)
- `/Users/gb/github/harmonik/specs/control-points.md` (CP-001 … CP-061; §6.1.1 Gate payload location verified)

Citation-correction: the design noted the brief's "EM-046c" was actually EM-042a; this draft propagates EM-042a only (no occurrence of EM-046c).

GateDecisionPayload location: declaration owned by C4 (control-points.md §6.1.8, CP-058) per cross-component reconciliation. C3 cites the location for every reference; EM-005b cites by name; the CP-shaped record stays with CP. (Original C3 draft attempted a parallel declaration at §6.1.1 with CP-062; retracted in EDIT D to avoid double-declaration.)

Open questions surfaced (OQ-HC-013 … OQ-HC-015, OQ-EM-016, OQ-CP-007) are carried to pass-6 integration; none block the draft.

No BLOCK-grade issues identified during fresh-context self-review. Recommend pass-6 integration reviewer specifically verify:

1. The highest-existing CP-NNN ID and renumber CP-058 if a conflict appears.
2. The C1 design pass's commitment on `context_keys` declaration site (DOT attribute vs. YAML companion) — HC-062 commits to DOT graph-level attribute; if C1 lands differently, HC-062 must reconcile.
3. The §8 `FailureClass` enum declaration in execution-model.md — verify whether the six values are already named as an `ENUM FailureClass` block; if not, pass-6 should align §8's prose to use the named-enum form so EM-005c's `RECORD Outcome` row can cite the type cleanly.
