# Research Findings — Handler-Contract Outcome (C3)

> Pass 3 (`research`) component **C3** of the `phase-3-dot` kerf work. Absorbs partial G1 (node-type → outcome shape mapping) and partial G5 (failure_class field surfacing). **No design decisions.** Five focus areas inventoried; open questions catalogued for pass-4.

## 1. Current Outcome shape (as found)

The `Outcome` type is normatively owned by **`specs/execution-model.md §4.1 EM-005`** (not by handler-contract.md, despite C3's labeling — `handler-contract.md §4.1 HC-008` simply names execution-model as the type's home and obligates handlers to produce one). The current shape:

| Field | Type | Required | Notes |
|---|---|---|---|
| `status` | enum `{SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}` | yes | Routing primary; classifier input per §4.5 |
| `preferred_label` | string \| absent | optional | Routing hint, cascade step (b) |
| `suggested_next_ids` | list<string> \| absent | optional | Routing hint, cascade step (c) |
| `context_updates` | map<string,any> | yes (may be empty) | Applied pre-cascade per EM-041a |
| `notes` | string | yes (may be empty) | Freeform, no schema |
| `kind` | enum `{default, reconciliation_verdict}` | yes (defaults `default`) | EM-005a discriminator |
| `payload` | kind-typed record \| absent | optional | Absent for `default`; `VerdictEvent` for `reconciliation_verdict` |

**Classifier input** for failure taxonomy is the **handler-returned `ErrX` sentinel** per HC-020/§4.5, **not** an `Outcome` field. There is no `failure_class` field on Outcome today.

The discriminator enum is **closed at MVH** (EM-005a); future variants amend per architecture.md §4.6. An unknown `kind` value routes the outcome to reconciliation per Cat 6a — **not** silently degraded to `default`.

## 2. Adequacy across the 5 node types

Pass-2 names five node-type categories: agentic, non-agentic, gate, control-point, sub-workflow. Cross-checking each against the current shape:

### 2.1 Agentic (Kilroy `codergen` analog, e.g. `claude-code`)
The shape was designed for this case. `status` + `preferred_label` + `suggested_next_ids` + `context_updates` + `notes` directly matches Attractor §3.5. **No identified gap.** Caveat: agentic nodes are where `failure_class` would carry the most information (see §3).

### 2.2 Non-agentic (Kilroy `tool` analog)
Tool/shell-style nodes have a tighter outcome surface — typically exit-code + stdout/stderr — but the daemon-side handler synthesizes an `Outcome` from those. `status` mapping is trivial; `context_updates` carries captured stdout/stderr summaries. **No identified gap**, but: notes-as-stderr is informal and unparseable. Open question whether structured `tool_result` belongs in `payload` under a new `kind`.

### 2.3 Gate (Kilroy `wait.human` / Attractor goal-gate / conditional)
Gates are the **first provable gap**. A gate node's outcome is essentially binary (permit / deny), but a deny outcome's *rationale* is operationally and audit-critically important: which policy fired, which input failed, who the deciding actor was (operator vs. policy engine), and the gate's resolution-signal correlation id per EM-046c. The current shape forces this into `notes` (freeform, unparseable) or into `context_updates` (key collision risk across nodes). Neither is normative. Attractor's `goal_gate=true` doesn't help — Attractor stores rationale in stage artifacts, not the Outcome. Kilroy is silent. **This is a `payload` extension candidate** (new `kind = gate_decision`).

### 2.4 Control-point (harmonik-specific; Q1 from pass-1 still open)
Control-points produce policy outcomes: budget-check decision, freedom-profile validation, skill-provisioning verdict. Some of these flow as `agent_failed` events with sentinel classes (§4.6, e.g. `ErrSkillProvisioningFailed`) — *not* as Outcomes. If Q1 lands control-point as a first-class node type, its Outcome surface needs a decision on whether to lift those sentinels into a structured `payload` (e.g. `kind = control_point_verdict` with `policy_id`, `decision`, `reason_code`). If Q1 lands control-point as a *flag* on existing types, no new payload variant is required.

### 2.5 Sub-workflow
The sub-workflow-exit Outcome is governed by **`execution-model.md §4.8 EM-036a`**: the escaping Outcome MUST be the last-expanded-node's Outcome verbatim — the parent's cascade observes it unchanged. So the existing shape *is* what propagates. But: `kind` and `payload` propagate too. A sub-workflow whose terminal node had `kind = gate_decision` (per §2.3, hypothetical) would surface that to the parent's cascade. **This is fine for routing** (cascade only reads `status`/`preferred_label`/`suggested_next_ids`) but the parent's downstream handlers receive a payload shape they may not understand. See §4.

## 3. `failure_class` — top-level vs. nested

Kilroy/Attractor define 6 classes (`transient_infra`, `budget_exhausted`, `compilation_loop`, `deterministic`, `canceled`, `structural`). Attractor's handler contract: handlers return `failure_class` in `meta`; the engine's own classification is a safety net. Audit V3.6/V4.3/V4.9 flags a spec-vs-code inversion: spec treats `status` as primary, code treats `failure_class` as primary.

Harmonik's current state:
- Failure classification lives in the **error sentinel** path (`ErrTransient` / `ErrDeterministic` / `ErrCanceled` / `ErrStructural` / `ErrBudgetExhausted` per HC-020), not in the Outcome.
- A handler that crashed without emitting `outcome_emitted` is classified via sentinel; a handler that emitted `outcome_emitted` with `status = FAIL` carries **no class today** — the engine cannot distinguish a transient API error from a deterministic auth failure when both surface as FAIL through the outcome path.

Three placement options for pass-4 design:
- **A. Top-level field on Outcome** (`Outcome.failure_class`, present only when `status = FAIL`). Aligns with Attractor's `meta.failure_class`. Pro: machine-readable; cascade can route on it (G5 routing in C1). Con: schema churn; existing consumers must be N-1 readable per EM-022.
- **B. In `notes`.** No schema change. Con: unparseable; cascade cannot read it; defeats G5 partial.
- **C. New `kind = failure` payload variant.** Compatible with EM-005a's extension protocol. Pro: structurally clean. Con: forces every FAIL outcome to use a non-`default` kind, which is invasive — most FAIL outcomes today are `default`.

**Strong lean (not a decision):** Option A. The classifier (§8) routes on class; making it a first-class field is the smallest delta that closes G5.

## 4. Sub-workflow outcome lifting

EM-036a is normative and clear: the parent sees the **verbatim** terminal-node Outcome from the sub-workflow. Three findings:

1. **No synthesis happens.** The parent's `sub-workflow` node "MUST NOT declare its own `Outcome` shape — it inherits the expanded terminal outcome mechanically" (EM-036a).
2. **`context_updates` are already applied** to the run's shared context inside the sub-workflow expansion (EM-041a) before the parent observes them. The parent's cascade sees post-update context.
3. **`kind`/`payload` propagation is undefined.** If the sub-workflow's terminal node emits a non-`default` kind (hypothetically `gate_decision`), the parent inherits it. This may or may not be desired — the parent's outgoing edges may not expect a gate-decision payload. Open question for pass-4.

A related open question (not in this component's scope but flagged): a sub-workflow has multiple terminal nodes; EM-036a picks "the one the expanded execution traversed into." `Outcome.preferred_label` from the terminal node propagates — which means a sub-workflow can effectively *suggest* parent-edge routing. This is powerful but undocumented as a design pattern.

## 5. `context_updates` propagation rules

EM-041a is the authority: `context_updates` are merged into the run's shared `context` **before** the edge-selection cascade evaluates condition expressions. Three findings:

1. **Scope is the run.** Updates are visible to every subsequent node in the same run; not to sibling runs, not to parent runs (no parent-run context exists — runs are flat), not to checkpoints' frozen state beyond the natural one-commit-per-node trail.
2. **No typed schema.** `context_updates` is `map<string, any>`. No key registration, no namespace discipline, no collision rule. Q2-adjacent: "does `context_updates` need a typed schema, or remains a free-form map per existing spec?" (from pass-2 §C3).
3. **Sibling propagation is implicit.** In parallel branches (deferred for Phase-3 per pass-1 §5) Attractor's rule is that branches receive *isolated clones*, and parent-branch Outcomes do NOT merge back. Harmonik has not specified this; out of Phase-3 scope but flagged.

The edge-condition LHS whitelist (pass-2 Q6) interacts here: which `context_updates` keys are legal in `condition="key=value"` expressions? A typed schema (or at minimum a registered-key list per workflow) would close both questions together.

## 6. Gate-node outcome: is `status` sufficient?

Provably no — three structural reasons:

1. **Binary status loses rationale.** A gate that denies for "budget exhausted at policy P-12" vs. "operator override timeout" both surface as `status = FAIL`. The cascade can disambiguate via `preferred_label` (e.g. `preferred_label = "budget_denied"` vs. `"override_timeout"`), but this conflates routing-hint and audit-evidence.
2. **EM-046c gate-resolution signal correlation.** When a gate enters `gate-pending` and later resolves, the resolution signal must be correlated back to the originating gate Outcome. No correlation id field exists on the current shape.
3. **Audit trail requirements.** Operator-NFR (referenced from §4.6.HC-026b) and the broader audit posture require *who decided, on what evidence*. `notes` is freeform; not auditable.

A `payload` extension under a new `kind = gate_decision` is the natural shape — fields would be (sketch, **not** a design decision): `policy_id`, `decision_actor` (`policy_engine` / `operator` / `timeout`), `decision_evidence_ref` (commit SHA or event correlation id), `resolution_signal_id`. Implementation in pass-4.

## 7. Cross-component dependencies

- **C1 (workflow-graph.md)** — must declare which condition-LHS fields the cascade reads (`status`, `preferred_label`, `suggested_next_ids`, `failure_class` if adopted, whitelist of `context_updates` keys). C3 cannot finalize its surface until C1 names the routable fields.
- **C2 (execution-model.md §`dot` mode)** — owns the dispatch driver. If `failure_class` becomes a top-level routing input, C2's cascade documentation must reference it. EM-005a's discriminator routing rule (unknown `kind` → reconciliation Cat 6a) is also C2's concern.
- **C4 (control-points.md binding)** — Q1 (control-point as node type vs. flag) is the upstream decision. If first-class, C3 may need a `kind = control_point_verdict` payload variant.
- **Reconciliation spec** — already consumes `kind = reconciliation_verdict`. New kinds added in this work need similar consumer specs or explicit "no consumer beyond cascade" notes.

## 8. Top-3 open design decisions for pass-4

1. **`failure_class` placement.** Top-level Outcome field (Option A in §3), `notes`-only (Option B), or new `kind = failure` payload (Option C). Affects schema, routability, EM-022 N-1 compat. **Lean: A.**
2. **Gate-node Outcome surface.** Keep `status`-only and lean on `preferred_label`/`notes` (no schema change); or add `kind = gate_decision` payload (EM-005a extension protocol). Affects audit posture and gate-resolution correlation. **Lean: payload extension.**
3. **`context_updates` typing discipline.** Free-form map (status quo); or per-workflow registered-key list; or typed schema in the spec. Affects edge-condition LHS whitelist (pass-2 Q6) and cross-node collision risk. **Lean: per-workflow registered-key list — minimal new machinery.**

Two secondary decisions (lower priority):

4. Sub-workflow `kind`/`payload` propagation rule — propagate verbatim (status quo per EM-036a) or normalize to `default` at the sub-workflow boundary?
5. Should `Outcome.notes` carry any structure (e.g. an optional `evidence_refs: list<string>`)?

## 9. Provably-insufficient cases (vs. design-room)

- **Gate-node rationale capture** (§6) — provably insufficient given EM-046c correlation and audit requirements; `notes` cannot be parsed by the cascade or by audit tooling.
- **FAIL-outcome failure-class disambiguation** (§3) — provably insufficient: two outcomes with identical `status = FAIL` carry no class today, making the C1 edge cascade unable to route on failure class as a secondary input (a Phase-3 stated requirement per pass-1 Gap 5).

Everything else in §2 is "schema is adequate but evidence-capture is informal" — i.e. design-room, not provably-insufficient.
