# C3 Design — `specs/handler-contract.md` §Outcome Cross-Check + Extension (Pass-4, phase-3-dot)

> Pass-4 design document for **component C3** of the `phase-3-dot` kerf work. C3 is the **handler-facing cross-check** of the `Outcome` envelope: confirming that `specs/handler-contract.md` correctly references the normatively-authoritative type in `specs/execution-model.md`, and surfacing two structural gaps (gate-node rationale, FAIL-outcome failure-class) that pass-3 research proved insufficient under the current shape. This is a DESIGN doc — it describes the section structure + content shape Pass-5 will draft as spec prose, not the spec prose itself.

## 0. Normative-ownership note (load-bearing)

The `Outcome` type is **normatively owned by `specs/execution-model.md §4.1 EM-005` + `EM-005a`** (the discriminator extension). `specs/handler-contract.md §4.2 HC-008` is the handler-facing delivery requirement — it obligates the handler subprocess to deliver an Outcome via the `outcome_emitted` progress-stream message, but it does **not** redeclare the type. C3 preserves this split:

- Schema changes (new field `failure_class`, new `OutcomeKind` value `gate_decision`, new payload variant for that kind) land in **execution-model.md**, not handler-contract.md.
- Handler-contract.md gains/keeps a handler-facing **cross-check section** that (a) cites EM-005 / EM-005a as the type's home, (b) enumerates handler obligations *per node-type category* (i.e. when a handler MAY/MUST set each optional field), and (c) names the daemon-side back-fill rule for `failure_class` (per D2) so handler authors understand authority lives with the daemon.

This split keeps the existing repo discipline (data specs ≠ runtime specs ≠ handler specs) intact. Per D-attractor-adoption §"Implications for pass-5 spec-draft": "EM-005 is the right home, not handler-contract."

## 1. Current state (as-found)

### 1.1 What `handler-contract.md §Outcome` says today

There is no §Outcome section in handler-contract.md. Outcome surfaces in the spec in three places:

- **§4.2 HC-008** — "The handler subprocess MUST deliver the run's `Outcome` (per [execution-model.md §4.1]) as the payload of a final `outcome_emitted` progress-stream message." Cites EM but does not redeclare the type.
- **§6.1 RECORD Outcome** — a *cite-only* comment block: `-- see [execution-model.md §6.1 Outcome]` (no fields enumerated locally).
- **§4.5 HC-020 — HC-023** — five sentinel-class error taxonomy (`ErrTransient`, `ErrStructural`, `ErrDeterministic`, `ErrCanceled`, `ErrBudgetExhausted`) plus two structural sub-sentinels. The sentinel path is the classifier input for the failure taxonomy; classification is **NOT** today carried on the Outcome itself.

Net assessment: handler-contract.md correctly defers ownership to execution-model.md and does not double-declare the type. The cross-check is already structurally clean. C3 does **not** need to invert this; it needs to extend the handler-facing surface where two gaps make handler obligations under-specified.

### 1.2 What `execution-model.md §EM-005 / EM-005a / EM-036a / EM-042a` say today

- **EM-005** — Outcome fields: `status ∈ {SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}`, `preferred_label` (optional), `suggested_next_ids` (optional), `context_updates` (map, may be empty), `notes` (freeform), `kind` (discriminator per EM-005a, defaults `default`), `payload` (kind-typed, absent for `default`).
- **EM-005a** — `OutcomeKind ∈ {default, reconciliation_verdict}`. Discriminator is closed at MVH; future variants amend per architecture.md §4.6. Unknown `kind` routes to reconciliation Cat 6a (NOT silent fallback).
- **EM-036a** — Sub-workflow terminal outcome is the last-expanded-node's Outcome verbatim. The parent's cascade observes it unchanged (including `kind`/`payload` propagation).
- **EM-042a** — Gate-deny transitions the run into `gate-pending` sub-state and waits for a gate-resolution signal (declared in `control-points.md §6.2`). Note: the brief references "EM-046c" for the gate-resolution-signal correlation; the actual spec ID is **EM-042a**, and the resolution-signal type is owned by control-points. C3 must use the correct citation in pass-5. **Surfaced as open question OQ-C3-3.**

Classifier input today is the **sentinel** (`ErrX` per HC-020), not an Outcome field. There is no `failure_class` field on Outcome and no gate-decision payload variant.

## 2. Target state — section structure + content shape

The C3 deliverables span two specs: handler-contract.md gains a new §Outcome cross-check section; execution-model.md gains two additive schema bumps (per D2, plus the new `gate_decision` kind from D7 below).

### 2.1 In `specs/handler-contract.md` — new §4.2a "Outcome surface (handler-facing cross-check)"

Sub-section content shape:

1. **Authoritative pointer.** One paragraph: "The `Outcome` type is owned by execution-model.md §4.1 EM-005 + EM-005a. Handlers MUST emit Outcomes that conform to that schema; the present section is informative for handler authors and normative only for the per-node-type emission obligations enumerated below."

2. **Per-node-type emission obligation table.** A 4-row table (per D3's collapsed 4-type taxonomy: `agentic | non-agentic | gate | sub-workflow`), columns:
   - Node type
   - REQUIRED Outcome fields beyond the EM-005 floor (e.g. gate handlers MUST set `kind = gate_decision`)
   - OPTIONAL/permitted fields
   - Disallowed/inapplicable fields (e.g. sub-workflow handlers MUST NOT produce their own Outcome — propagates verbatim per EM-036a)
   
   Content per row:
   - **agentic** — full EM-005 surface; `preferred_label` MAY be a verdict string per D-verdict-surfacing (reviewer pattern); `failure_class` MAY be set as a HINT on FAIL (daemon back-fill is authoritative per D2).
   - **non-agentic** (tool-style) — full EM-005 surface; `context_updates` typically carries captured tool output summaries; `failure_class` HINT permitted.
   - **gate** — MUST set `kind = gate_decision` when emitting a deny outcome that should be auditable (see §2.3 below); MAY use the same kind for permits if rationale capture is desired; `payload` carries the gate-rationale record (D7).
   - **sub-workflow** — MUST NOT produce an Outcome; the parent observes the last-expanded-node's Outcome verbatim per EM-036a. Handler authors of sub-workflow expansions emit nothing at the sub-workflow boundary.

3. **`failure_class` handler-hint rule** (cross-refs EM-005 v2 after D2 lands). Three short paragraphs:
   - Handlers MAY set `Outcome.failure_class` on `status = FAIL` as a HINT, drawn from the §8 closed enum `{transient, structural, deterministic, canceled, budget_exhausted, compilation_loop}`.
   - The daemon back-fills `failure_class` from the HC-020 sentinel path when the handler omits it; the daemon's classification is AUTHORITATIVE — if handler and daemon disagree, daemon wins and logs disagreement (D2 follow-up #1 owns log-vs-escalate).
   - `compilation_loop` is **daemon-only** — handlers MUST NOT self-classify as `compilation_loop` (per D2 follow-up #2); a handler-set `compilation_loop` is overridden to `structural` by the daemon and logged.

4. **`kind = gate_decision` handler-emission rule** (cross-refs EM-005a v2 after D7 lands — see §2.3). Cite the payload schema by name; do not redeclare it.

5. **`context_updates` discipline cross-ref.** One paragraph pointing at workflow-graph.md (C1) for the per-workflow registered-key list (per D8 lean). Handlers writing context keys not registered for the active workflow MUST surface a `structural` failure at the daemon's pre-cascade validation step (the keys are silently dropped only at the validator's option; D8 lean says reject).

6. **`Outcome.notes` discipline note.** One sentence: "`notes` is freeform string and MUST NOT be parsed by the engine. Handlers that need machine-readable rationale MUST use the `payload` field under an appropriate `kind` (e.g. `gate_decision`)." This pre-empts the historical drift where rationale leaks into `notes`.

### 2.2 In `specs/execution-model.md` — EM-005 schema bump v1 → v2 (additive)

Per D2 — additive bump. Content shape:

- Add row to EM-005's field list: `failure_class : FailureClass | absent — present only when status = FAIL; one of the §8 6-class enum; daemon-back-filled from HC-020 sentinel when handler omits.`
- Update `RECORD Outcome` in §6.1 to include the new field.
- Add `ENUM FailureClass` reference at §6.1 (or inline cite §8 if that's where the enum lives in the locked taxonomy).
- Schema-version-bump rationale paragraph: "v2 adds the optional `failure_class` field; v1 readers treat it as unknown-and-optional and fall through to sentinel-based classification per HC-020. N-1 readability per §6.4 is preserved."
- Note that the field is **carrier-only**: classification authority remains with the daemon per HC-020 + §8; the handler-emitted value is a hint subject to override.

### 2.3 In `specs/execution-model.md` — EM-005a discriminator extension: `gate_decision`

This is the C3-owned **gap-fill #1** (gate-node rationale capture, research §6). Adds a third value to the `OutcomeKind` enum.

Content shape:

- Extend `ENUM OutcomeKind` from `{default, reconciliation_verdict}` to `{default, reconciliation_verdict, gate_decision}`. Per EM-005a's existing extension protocol, this is an additive schema change documented via the architecture.md §4.6 amendment protocol.
- Declare a new `GateDecisionPayload` record (or cite a new record in `specs/control-points.md` if that spec is the right home — see OQ-C3-1) with fields (sketch — final field list is D7's call, finalized in pass-5):
   - `policy_id : String` — the `gate_ref` name from the DOT node attribute.
   - `decision : Enum {permit, deny}` — the gate verdict (`status = SUCCESS` correlates with `permit`; `status = FAIL` with `deny`).
   - `decision_actor : Enum {policy_engine, operator, timeout}` — who decided.
   - `decision_evidence_ref : String | absent` — commit SHA, event correlation id, or other audit pointer.
   - `resolution_signal_id : SignalID | absent` — correlation id for the EM-042a gate-resolution signal (filled when the daemon resolves `gate-pending`).
- Cite the payload from a new EM requirement (suggested: **EM-005b** — "Gate-decision Outcomes carry structured rationale via `kind = gate_decision`"). Make it explicit that a gate-evaluator handler MAY emit `kind = default` for permits where no rationale capture is needed; `kind = gate_decision` is REQUIRED only for deny outcomes that the audit posture (control-points.md operator-NFR) requires to be auditable.
- Cascade interaction: the cascade (EM-041 + D-edge-cascade-invariant) reads `outcome.status` and `outcome.preferred_label` only; `outcome.payload.*` is **not** an LHS per D4. Routing on the gate verdict uses `outcome.preferred_label = "permit" | "deny"` per the D-verdict-surfacing pattern, OR `outcome.kind = "gate_decision"` if a workflow wants to discriminate gate-decision outcomes from non-gate outcomes structurally.

### 2.4 In `specs/handler-contract.md` — clarifying §6.1 RECORD Outcome cite

Update the existing cite-only comment block to point at EM-005 v2 (after the schema bump): `-- see [execution-model.md §6.1 Outcome v2]`. No fields enumerated locally — pure cite.

## 3. Rationale

### 3.1 Why the cross-check section in handler-contract.md (not a full re-declaration)

- **Preserves locked normative ownership** (pass-3 SUMMARY item #4). EM-005 is the type; HC restates handler obligations only. Double-declaring fields invites drift.
- **Per-node-type emission obligations are handler-facing**, not execution-model-facing. A handler author reading handler-contract.md needs to know "if I'm writing a gate handler, what must I emit?" Putting this in HC keeps the handler author's read path local.
- **D-attractor-adoption §"Implications for pass-5 spec-draft"** explicitly assigns the schema row to EM-005 and the handler obligation prose to handler-contract.md. C3 implements that split.

### 3.2 Why D2's `failure_class` placement (top-level field) is right for C3

C3's job is the handler-facing cross-check; D2 already committed the placement. Restating D2's argument briefly so this design stands alone:

- **Closes G5 (failure-class → edge routing).** A structured top-level field is the smallest delta that makes `failure_class` cascade-routable per D1.
- **`notes`-only (research Option B) is unparseable** — defeats G5.
- **`kind = failure` payload (research Option C) is invasive** — forces every FAIL outcome off `kind = default`. Gate-decision is correctly a kind because it's a genuinely different Outcome class; failure-class augments an existing FAIL outcome and shouldn't reshape its kind.
- **Daemon back-fill preserves HC-020 authority.** Handlers emit hints; daemon classifies.

### 3.3 Why D7's `kind = gate_decision` payload (this design's position)

Research §6 proved three structural reasons gate-node `status`-only is insufficient: (a) binary status loses rationale, (b) EM-042a gate-resolution correlation has no carrier on the current shape, (c) audit-NFR (control-points.md operator) requires "who decided on what evidence."

Options evaluated (research §8 item 2):

- **Keep `status`-only + lean on `preferred_label`/`notes`.** Rejected — research §6.1 explicitly: this **conflates routing-with-evidence** (`preferred_label` does double duty as routing hint and audit evidence) and leaves rationale unparseable in `notes`. Also: `preferred_label` is needed for verdict routing per D-verdict-surfacing; overloading it for gate rationale risks collision.
- **New `kind = gate_decision` payload (this design's lean).** Adopt EM-005a's existing extension protocol (the same mechanism that admitted `reconciliation_verdict`). Adds one enum value + one payload record; the cascade and durability decisions remain unchanged because they ignore `payload`. Gives gate-evaluators a structured audit surface with `policy_id`, `decision_actor`, `decision_evidence_ref`, and `resolution_signal_id` — the four fields research §6 named as load-bearing.

**Position: adopt the `kind = gate_decision` payload extension.** It is the right shape for gate-decisions for the same reason it was the *wrong* shape for failure-class (research §3 + D2 rationale): gate-decisions are genuinely a different Outcome class with rationale fields; failure-class is a single enum that augments. The two extensions are orthogonal — a gate deny on budget exhaustion emits `status = FAIL`, `failure_class = budget_exhausted` (top-level, per D2), `kind = gate_decision`, `payload = {policy_id, ...}` (per this design). D2 follow-up #4 explicitly anticipates this composition.

### 3.4 Why `context_updates` typing — per-workflow registered-key list (D8 position)

C3 must commit a position on D8 because the C3 per-node-type emission table references "what context keys a handler may set" and that depends on D8's typing discipline.

Options evaluated (research §5 + research §8 item 3):

- **Free-form map (status quo).** Rejected — leaves the edge-condition LHS whitelist (D4 row 5) un-validatable. D4 already commits `context.<key>` as LHS-legal only against a registered-key list; without D8 the whitelist row is inoperative.
- **Per-workflow registered-key list (research lean, D-attractor-adoption-compatible).** Each workflow's `.dot` file (or its companion config) declares the keys it considers part of its routing-relevant context. Handlers MAY emit other keys, but only registered keys are legal LHS in edge conditions. C2 validator enforces statically.
- **Typed schema in the spec (heavyweight).** Forces every workflow to import a schema; gold-plating for v1.

**Position: per-workflow registered-key list.** This is the smallest machinery that closes pass-2 Q6 (edge-LHS whitelist) and lets D4 row 5 work end-to-end. It matches the research lean and D4's stated dependency. Handler authors learn: "if I write `context_updates = {iteration_count: N}`, that key must be registered in the workflow's context-key list for downstream edges to route on it; emitting an unregistered key is a daemon-side WARNING (logged), not a structural failure." (Lean: warn-not-fail, because handlers should not be obligated to know the workflow's edge-LHS surface; the workflow author owns the registry. **Surfaced as open question OQ-C3-2** — pass-5 picks warn-vs-reject.)

## 4. Requirements traceability — `02-components.md §C3`

The C3 component sheet (`02-components.md` §C3) names two source-gap absorptions:

| C3 Requirement | Addressed by |
|---|---|
| **G1 partial — per-node-type outcome surface** | §2.1 per-node-type emission obligation table (4 rows under D3's collapsed taxonomy); §2.3 gate node's `kind = gate_decision` requirement; §2.1 sub-workflow's MUST-NOT-emit rule citing EM-036a. |
| **G5 partial — `failure_class` field on Outcome** | §2.2 EM-005 v2 schema bump (top-level field, per D2); §2.1 handler-hint + daemon-back-fill rule; §2.1 `compilation_loop` daemon-only carve-out. |

Plus the C3 sheet's three named open questions:

| Sheet OQ | Resolved by |
|---|---|
| **Q2 — `failure_class` required vs. inferable** | D2 (already landed): optional, daemon-back-filled from sentinel when handler omits. C3 §2.1 prose makes this handler-facing. |
| **`context_updates` typed schema vs. free-form** | §3.4 above — position: per-workflow registered-key list (D8 lean). Cross-refs C1 (workflow-graph.md) for the registry's location. |
| **Open from research §8 item 2 — gate-node Outcome surface** | §2.3 + §3.3 — position: `kind = gate_decision` payload extension per EM-005a. |

C3 sheet's dependency obligations:

- **Depends on C1.** Satisfied: this design cites C1 for the registered-key list location and for the §8 failure-class enum. C1's enum lockdown is already a pass-3 SUMMARY-resolved item.
- **Cross-checks current `handler-contract.md`.** Performed in §1.1; current spec correctly defers to EM-005 and needs an additive §4.2a section, not a rewrite.

## 5. Cross-component references

C3 sits inside a 5-component fan and must compose cleanly with each:

- **C1 (`specs/workflow-graph.md`)** — `04-design/D-attractor-adoption.md`, `04-design/D2-failure-class-placement.md`, `04-design/D4-edge-condition-lhs-whitelist.md`, `04-design/D5-edge-condition-dialect.md` (design files for C1 are in progress in this directory). C3 references C1 for: (a) the §8 failure-class enum, (b) the per-workflow registered-key list (D8 position), (c) the LHS whitelist that determines which Outcome fields are routable.
- **C2 (`specs/execution-model.md §dot` mode)** — design TBD in this directory. C3 schema bumps land *in* execution-model.md, but C2's dispatch-driver section is a separate extension; C3 does not encroach on it. C2's cascade prose must reference C3's `failure_class` field as an LHS-routable input.
- **C4 (`specs/control-points.md` binding)** — `04-design/control-point-node-type-design.md` (D3) collapsed the 5-type taxonomy to 4 types, dropping `control-point` as a node-type. C3's per-node-type table accordingly has 4 rows, not 5. C4's CP-036 attribute-binding rail is unaffected by C3.
- **C5 (`specs/examples/*.dot`)** — design TBD. C5's `review-loop.dot` example will exercise C3's `preferred_label` verdict surface (per D-verdict-surfacing) and may exercise `failure_class` routing if a "retry on transient, terminate on structural" example ships. A gate-decision example would exercise C3's `kind = gate_decision` payload but is not committed in C5's minimum scope.

## 6. Trade-offs accepted

- **Two extensions in one pass.** D2 (failure_class) and D7 (gate_decision) both touch EM-005's schema. We accept landing both at the v1→v2 bump rather than staging them — schema bumps are coordinated at the EM-005a amendment protocol, and there is no v1 corpus to break. (Pass-3 SUMMARY item #4 explicitly permits additive bumps.)
- **Per-node-type emission table is informative-with-normative-bullets, not a full closed grammar.** Handlers MAY emit fields beyond what the table names (the EM-005 floor is permissive); the table only adds MUST/MUST-NOT obligations where research proved them load-bearing.
- **`failure_class` and `gate_decision` are orthogonal carriers** — a gate handler denying due to budget exhaustion sets both. We accept the verbosity because the two fields encode different facts (classification vs. rationale).
- **Reviewer verdict stays on `preferred_label`** (per D-verdict-surfacing, not on a `kind = verdict` payload). C3 does not invent a `verdict` kind. If post-MVH reviewer-rationale capture becomes load-bearing, the gate-decision pattern can be lifted (D-verdict-surfacing follow-up).
- **Sub-workflow verbatim propagation includes `kind`/`payload`.** A sub-workflow whose terminal is a gate-decision propagates `kind = gate_decision` to the parent. Acceptable; parent edges that don't expect it will simply not match on `outcome.kind = "gate_decision"` and route by `status` / `preferred_label` instead.

## 7. Open questions surfaced (for pass-5 spec-draft)

- **OQ-C3-1** — Where does `GateDecisionPayload` (the record schema) physically live? Two candidates: (a) inline in `specs/execution-model.md §6.1` next to `RECORD Outcome` (mirrors `VerdictPayload` precedent); (b) in `specs/control-points.md` as a control-points-owned record cited by EM. **Lean: (b)** — control-points owns Gate-kind semantics; EM cites by name. Pass-5 confirms.
- **OQ-C3-2** — When a handler emits an unregistered `context_updates` key under D8's per-workflow registered-key list: warn-and-drop or reject as `structural`? **Lean: warn-and-drop** — handlers shouldn't need to know the workflow author's edge-LHS surface; the registry is for routability, not emission legality. Pass-5 picks.
- **OQ-C3-3** — The brief cited "EM-046c" for gate-resolution-signal correlation; the actual spec ID is **EM-042a**, and signal-type ownership is in `control-points.md §6.2`. Pass-5 cross-ref must use the correct citations. (Surfaced here so pass-5 doesn't propagate the stale brief reference.)
- **OQ-C3-4** — Handler-emitted vs. daemon-classified `failure_class` disagreement: log-only or escalate to reconciliation? Carried from D2 follow-up #1. **Lean: log-only at v2**; promote if disagreement rate is non-trivial.
- **OQ-C3-5** — Does `kind = gate_decision` require `status = FAIL` for a deny, or may a permit (status=SUCCESS) also carry the payload for audit-on-permit? **Lean: both allowed** — `decision_actor` + `decision = permit` is a valid audit record. Pass-5 confirms.

---

## Footer — Reviewer note

Reviewer sub-agent was not dispatched in this pass (sub-agent tool not available). Per the D2/D3 pattern in this directory, a **fresh-context re-read pass** was performed against (a) the §0 normative-ownership note, (b) the §2 target-state structure, (c) the §3 rationale bullets, and (d) the §4 traceability table. Re-read confirmed:

- The cross-check section in handler-contract.md does NOT redeclare Outcome fields (preserving pass-3 SUMMARY item #4 — EM-005 ownership).
- D2's `failure_class` placement is restated, not relitigated; C3 adopts the landed position.
- D7's `gate_decision` payload position is committed *here* (this is C3's first-pass commitment site) and is consistent with EM-005a's extension protocol and with D2's "kind for failure was wrong, kind for gate is right" distinction.
- The brief's EM-046c citation is correctly flagged as EM-042a (OQ-C3-3) — would have propagated stale into pass-5 without surfacing.

No BLOCK-grade issues identified. Recommend a fresh-context reviewer agent run before pass-5 spec-draft begins, with specific attention to OQ-C3-1 (GateDecisionPayload location) and OQ-C3-3 (citation correction).
