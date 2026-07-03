# C4 Spec-Draft — `specs/control-points.md` extension (Pass-5, phase-3-dot)

> **Status:** DRAFT. Integrates into `specs/control-points.md` at pass-6.
> **Scope:** node-type binding for the `gate` node-type (per D3 Framing A); symmetrization of the `*_ref` family with a new `skills_ref`; D7 Gate-decision Outcome payload (ownership taken here per C3 deferral); schema-version drift posture for mechanism-tagged Gates.
> **Cross-component:** consumes the 4-node-type catalog from C1 (`specs/workflow-graph.md`); consumes the EM-007 amendment from C2 (`specs/execution-model.md §7.5`); is consumed by C3 (`specs/handler-contract.md §Outcome`) for the `kind = gate_decision` payload reference.

---

## Insertion plan

Two new normative subsections are added to `specs/control-points.md`:

- **§4.12 — Workflow-graph binding** (new) — covers the `gate` node-type binding, the `gate_ref` + `handler_ref` pairing, and the symmetrized `*_ref` family.
- **§4.13 — Gate-decision Outcome payload** (new) — the D7 payload shape returned by `gate` node handlers. C4 takes ownership per C3's OQ-C3-1 deferral.

A targeted amendment lands in §4.7 (CP-038) for the mechanism-tagged Gate schema-drift question. One new entry lands in §6.1 (the `GateDecisionPayload` record). No existing CP-NNN is renumbered.

Next available requirement ID after scanning the existing file is **CP-053** (CP-052 is the highest base ID; CP-026a, CP-034b, CP-040a are sub-numbered amendments). IDs assigned below run CP-053 through CP-058.

---

## §4.12 — Workflow-graph binding (new subsection)

### CP-053 — A `gate` node is the dispatch surface for a Gate-kind ControlPoint

A workflow-graph node of type `gate` (per [workflow-graph.md §4.x Node-type catalog]) MUST be the dispatch surface for exactly one Gate-kind ControlPoint registered per §4.9. The Gate-kind ControlPoint is named by the node's `gate_ref` attribute per §4.7.CP-036; the node-type `gate` and the Kind `Gate` are bound by this requirement and not by any other rail.

This affirms D3 Framing A: ControlPoint is NOT itself a node-type in the workflow-graph taxonomy. The `gate` node-type is retained as a node whose handler evaluates a Gate-kind ControlPoint. The other three Kinds (Hook, Guard, Budget) have no node-type representation; they bind via subsystem dispatch (S05 for Hook; S01 for Guard via the edge cascade per [execution-model.md §4.10]; the handler runtime for Budget per §4.5.CP-023) and via attribute references (`budget_ref` on any node).

Tags: mechanism

### CP-054 — `gate` nodes carry `gate_ref` AND `handler_ref`

A `gate` node MUST carry both `gate_ref` (REQUIRED — names the registered Gate-kind ControlPoint per CP-002) and `handler_ref` (REQUIRED — names the Gate-evaluator handler that performs the dispatch per [execution-model.md §7.5 EM-007 amendment]). The two attributes are not interchangeable: `gate_ref` resolves to a `gates[]` entry in policy YAML (the evaluator semantics — mechanism expression or cognition delegation path); `handler_ref` resolves to a handler registered per [handler-contract.md §4.x] (the dispatch shell that drives the evaluator and returns the §4.13 Outcome payload).

This requirement closes pass-4 design follow-up #2 (the EM-007 + Gate-evaluator-dispatch reconciliation). The `gate` node-type is exempt from the EM-007 "non-agentic node-types MUST NOT carry `handler_ref`" rule via the EM-007 amendment in C2; under that amendment, `gate` joins `agentic` as the second node-type that requires `handler_ref`. The semantic difference: an `agentic` handler launches a model session whose Outcome is whatever the agent produces; a `gate` handler evaluates the named ControlPoint and returns a `GateDecisionPayload` (§4.13).

Tags: mechanism

### CP-055 — `*_ref` family is typed and disambiguated

DOT workflow attributes MUST use the typed `*_ref` family, each pinning a single category of policy target. The complete MVH family is:

| Attribute        | Targets                                            | Required on              | Optional on                  |
|------------------|----------------------------------------------------|--------------------------|------------------------------|
| `gate_ref`       | a `gates[]` entry (Gate-kind ControlPoint)         | every `gate` node        | (not legal elsewhere)        |
| `handler_ref`    | a registered handler per [handler-contract.md]     | every `agentic` and `gate` node | `non-agentic` nodes per [execution-model.md §7.5] |
| `freedom_profile_ref` | a `freedom_profiles[]` entry                  | (never required)         | any node                     |
| `budget_ref`     | a `budgets[]` entry (Budget-kind ControlPoint)     | (never required)         | any node, any edge           |
| `skills_ref`     | a `skill_sets[]` entry per §6.3                    | (never required)         | any node                     |
| `policy_ref`     | (DEPRECATED at MVH — see CP-056)                   | —                        | —                            |

`skills_ref` is the typed disambiguation of the prior `policy_ref → skill_sets[]` overload identified in pass-3 research finding D14. Under D3 Framing A, attribute-based binding is the sole rail for policy attachment to graph elements; a typed family is required to keep the binding deterministic and to keep workflow-ingest's reachability AST-walker (§4.8.CP-040a item 4) free of category ambiguity.

Tags: mechanism

### CP-056 — `policy_ref` is deprecated at MVH; typed refs replace it

The `policy_ref` attribute named in legacy text of §4.7.CP-036 MUST NOT appear in MVH-conformant workflows. Every prior `policy_ref` usage maps onto exactly one typed attribute in the CP-055 family: a `policy_ref` whose target was a `gates[]` entry MUST be rewritten as `gate_ref`; a `policy_ref` whose target was a `freedom_profiles[]` entry MUST be rewritten as `freedom_profile_ref`; a `policy_ref` whose target was a `skill_sets[]` entry MUST be rewritten as `skills_ref`. Workflow-ingest MUST reject any DOT attribute named `policy_ref` with `ErrDeterministic`; the rejection message MUST name the typed replacement attribute(s) the author should use.

This is a breaking change to §4.7.CP-036's prose enumeration of valid `*_ref` attributes; the CP-036 normative statement (DOT attributes resolve to registered names) is unchanged in substance — only the enumerated list is updated. No v1 DOT corpus exists in production (phase-3-dot is the first wire-up of DOT mode), so the migration cost is zero.

Tags: mechanism

### CP-057 — `skills_ref` semantics

The `skills_ref` attribute, when present on a node, MUST resolve to a `skill_sets[]` entry per §6.3 whose `skills` list names the additional skills that node's handler MUST provision at agent launch beyond the role's `default_skills`. The effective skill set per §4.11.CP-050 becomes the set-union of (a) the node's declared inline `required_skills` (per §4.11.CP-049 option (a)), (b) the resolved `skills_ref` target's skills list (per §4.11.CP-049 option (b)), and (c) the assigned role's `default_skills`. The three-way union preserves the CP-050 monotonic-by-union property; resolution-time failures route through [handler-contract.md §4.11] per CP-049 fail-point B.

`skills_ref` is OPTIONAL on every node-type including `gate`. A `gate` node whose evaluator is mechanism-tagged typically declares no `skills_ref`; a `gate` node whose evaluator is cognition-tagged MAY declare `skills_ref` to provision skill packages the delegated role needs at evaluator dispatch time.

Tags: mechanism

---

## §4.13 — Gate-decision Outcome payload (new subsection)

> **Ownership note.** C3 (`specs/handler-contract.md §Outcome`) defers definition of the `kind = gate_decision` Outcome payload to this spec, per OQ-C3-1. The wrapper Outcome shape (the `Outcome` record carrying a `kind` discriminator and a typed payload) is owned by [execution-model.md §6.1] (Outcome record) and [handler-contract.md §Outcome] (handler-contract's `kind` enum). C4 owns the `GateDecisionPayload` shape only.

### CP-058 — `gate` node handlers return a `GateDecisionPayload`

A handler dispatched against a `gate` node per CP-054 MUST return an Outcome whose `kind` discriminator equals `gate_decision` and whose payload conforms to the `GateDecisionPayload` record declared in §6.1.8. The payload's five fields capture both the evaluator's verdict and the audit trail required to interpret the decision under replay:

1. `policy_id` — String. The `name` of the Gate-kind ControlPoint that was evaluated. MUST equal the resolved target of the node's `gate_ref` per CP-054.
2. `decision` — GateAction. One of `{allow, deny, escalate-to-human}` per §6.1.6 `GateAction`. Identical semantics to CP-006 / CP-009 / CP-008 (subtype-driven escalation).
3. `decision_actor` — String. Names the actor that produced the decision. For mechanism-tagged Gate evaluators, this MUST be the literal string `"mechanism"`. For cognition-tagged Gate evaluators, this MUST be the role name from the `DelegationPath` per §6.1.5 (e.g., `"reviewer"`). The mechanism / cognition distinction is recoverable from this field without re-reading the ControlPoint record.
4. `decision_evidence_ref` — String | None. For cognition-tagged Gate evaluators, this MUST be the path to the persisted `GateVerdictRecord` per §4.8.CP-040 (within the Transition's `evidence` field, keyed by `gate_name`). For mechanism-tagged Gate evaluators, this MAY be None when no auxiliary evidence is produced, or it MAY name an event-stream record (e.g., a `gate_allowed` / `gate_denied` event correlation key) when the evaluator emits auxiliary context.
5. `resolution_signal_id` — String | None. When `decision == escalate-to-human`, this MUST name the resolution signal the run is waiting on (per [execution-model.md §8] escalation semantics); the run enters quarantine pending external resolution per CP-009 and the corresponding §8 entry. When `decision ∈ {allow, deny}`, this field MUST be None.

The Outcome wrapper (status / failure_class / artifact_refs / etc.) follows [execution-model.md §6.1] conventions; a `gate_decision` Outcome's `status` MUST be `SUCCESS` regardless of the `decision` field (a `deny` is a successfully-evaluated Gate, not a failed run; the run staying in the source state per CP-009 is governed by the cascade, not by the Outcome's status). A handler that cannot evaluate the Gate (e.g., evaluator dispatch failure, cognition delegation path unavailable) MUST return an Outcome with `status = FAILURE` and a `failure_class` per [execution-model.md §8]; that Outcome MUST NOT carry a `gate_decision` payload.

Tags: mechanism

### §6.1.8 — `GateDecisionPayload` record (new)

```
RECORD GateDecisionPayload:
    policy_id              : String         -- name of the evaluated Gate-kind ControlPoint (CP-002)
    decision               : GateAction     -- {allow, deny, escalate-to-human} per §6.1.6
    decision_actor         : String         -- "mechanism" | <role-name> per CP-058
    decision_evidence_ref  : String | None  -- path to GateVerdictRecord (cognition) or event correlation key (mechanism); None permitted for mechanism evaluators with no auxiliary evidence
    resolution_signal_id   : String | None  -- escalation resolution-signal name; non-None iff decision == escalate-to-human
```

This record is the typed payload referenced by the `kind = gate_decision` discriminator in [handler-contract.md §Outcome]. Field-shape evolution follows §6.6 (N-1 readable per CP-038).

---

## §4.7 amendment — schema-version drift for mechanism-tagged Gates

### CP-038 amendment (sub-clause CP-038a)

#### CP-038a — Mechanism-tagged Gate evaluators participate in the persisted-verdict envelope-hash discipline

A mechanism-tagged Gate evaluator's verdict is NOT persisted under §4.8.CP-040 (which applies to cognition-tagged evaluators only). However, when a mechanism-tagged Gate's evaluator expression text, its policy document's `schema_version`, or its workflow-ingest reachability set (per §4.8.CP-040a item 4) changes between the run's original evaluation and a replay attempt, the replay path MUST detect the drift and emit a `gate_definition_drift` event per §6.5 carrying the prior and current values for the changed inputs.

Drift detection for mechanism-tagged Gates uses the same envelope inputs declared in §4.8.CP-040a items 1, 4, and 5 (expression text; reachable `context_subset`; `policy_meta` at registered `schema_version`); items 2 and 3 (prompt template, skill packages) do not apply to mechanism-tagged evaluators and are absent from the envelope. The hash algorithm is SHA-256 over the canonical JSON of the three applicable inputs in declared order, mirroring CP-040a.

On detected drift, the replay path MUST NOT silently re-evaluate using the new expression text. The run's Cat 6 reconciliation handler per [reconciliation/spec.md §4.2] is the only authority that can authorize re-evaluation under the new definition; mirroring CP-INV-003's escalation rule for cognition-tagged evaluators. A mechanism re-evaluation under a Cat 6 verdict MUST emit a `gate_redefined_under_cat_6` event carrying the prior decision (read from the JSONL `gate_allowed` / `gate_denied` event for the original transition) and the new decision.

This requirement closes pass-4 design follow-up #5 (the schema-version-bump documentation for the EM-006 5→4 collapse). The collapse itself is a one-shot migration (no v1 corpus); the ongoing drift-detection discipline for mechanism-tagged Gates is what CP-038a normalizes.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

---

## §6.5 — Co-owned event payloads (additions)

Two new events are added to §6.5 (payload schemas owned by [event-model.md §6.3]):

- `gate_definition_drift` — emitted when a mechanism-tagged Gate's envelope inputs (per CP-038a) differ between the original evaluation and a replay attempt. Payload includes `run_id`, `gate_name`, `prior_envelope_hash`, `current_envelope_hash`, and a `changed_inputs` list naming which of `{expression_text, context_subset, policy_meta}` changed.
- `gate_redefined_under_cat_6` — emitted when a Cat 6 reconciliation verdict authorizes mechanism-tagged Gate re-evaluation under a drifted definition. Payload includes `run_id`, `gate_name`, `prior_decision`, `new_decision`, and the `cat_6_verdict_id`.

---

## §9 — Cross-references (additions)

Append to §9.1:

- **[workflow-graph.md §4.x Node-type catalog]** — the 4-node-type catalog (`agentic`, `non-agentic`, `gate`, `sub-workflow`); C4 binds the `gate` node-type to the Gate-kind ControlPoint here (CP-053).
- **[execution-model.md §7.5 EM-007 amendment]** — the amended dispatch-table rule that `gate` node-type carries `handler_ref` alongside `agentic`; C4 consumes this at CP-054.
- **[handler-contract.md §Outcome]** — the `kind` discriminator enum including `gate_decision`; C4 owns the payload shape (§6.1.8) referenced by that enum.

---

## §11 — Open questions (additions)

#### OQ-CP-006 — Mechanism-tagged Gate drift envelope: re-use vs. separate hash

Question: CP-038a defines a separate envelope hash for mechanism-tagged Gates (three inputs) parallel to CP-040a's cognition envelope hash (five inputs). Should the two hashes share a record shape, or remain separate types? MVH ships them as separate types (no shared `GateEnvelopeHash` record) to keep cognition's persisted-verdict pathway visually distinct from mechanism's per-replay-attempt recompute pathway.
Owner: foundation-author
Blocks: none (MVH defaults to separate types)
Default-if-unresolved: Keep separate; revisit if a third Kind develops a hash discipline.

---

## §12 — Revision history (entry to append at integration)

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-05-23 | 0.4.0 | phase-3-dot/C4 | Added §4.12 (workflow-graph binding: CP-053 `gate` node-type binding, CP-054 `gate_ref` + `handler_ref` pairing, CP-055 typed `*_ref` family, CP-056 `policy_ref` deprecation, CP-057 `skills_ref` semantics); added §4.13 (Gate-decision Outcome payload: CP-058 + §6.1.8 `GateDecisionPayload` record, ownership taken per C3 OQ-C3-1 deferral); added CP-038a (mechanism-tagged Gate schema-version drift discipline mirroring CP-040a / CP-INV-003 envelope-hash + Cat 6 escalation); added `gate_definition_drift` and `gate_redefined_under_cat_6` events to §6.5; added OQ-CP-006 for envelope-hash type-sharing question. No existing requirements renumbered; CP-053 through CP-058 assigned. |

---

## Draft notes (non-normative; remove at integration)

- **GateDecisionPayload ownership: taken.** Per C3 OQ-C3-1, the design preferred C4 ownership; rationale was that the payload's load-bearing field (`policy_id` referencing a Gate ControlPoint by name) is a CP-002 concept, and the cognition/mechanism `decision_actor` split mirrors §4.8's mechanism-vs-cognition boundary. ~30 lines added per the brief's estimate.
- **Schema-version drift posture: inlined as CP-038a.** Pass-4 design follow-up #5 + research finding both flagged this as unaddressed for mechanism-tagged Gates. Inlining (vs. deferring to a follow-up bead) is justified because the discipline is small (one CP, three envelope inputs, mirrors CP-040a's pattern with two inputs omitted), and because the lack of it would create an asymmetric replay-safety story (cognition Gates safe, mechanism Gates silently re-evaluable). The CP-INV-003 family is load-bearing for the foundation's replay-safety invariant; leaving mechanism Gates outside it for "MVP simplicity" would be the kind of half-built feature the project explicitly forbids.
- **EM-007 amendment dependency.** CP-054 cites [execution-model.md §7.5] for the amendment that lets `gate` nodes carry `handler_ref`. If C2's draft renumbers (e.g., to §7.6 or a sub-clause EM-007a), this draft's citation updates correspondingly at pass-6 integration.
- **`policy_ref` deprecation breaking-ness.** CP-056 is technically a breaking change to §4.7.CP-036's enumerated list (one of the legacy `*_ref` names is removed). It is accepted as a breaking change because (a) no v1 DOT corpus exists in production, (b) phase-3-dot is the first wire-up of DOT mode, (c) the replacement is mechanical (one attribute name swap per declaration). The §4.7.CP-038 N-1 readability discipline applies only to policy YAML schema_version, not to DOT attribute enumeration — so this is not a CP-038 violation.
- **`skills_ref` and CP-049 option (b).** CP-049 currently names two declaration forms: inline `required_skills` attribute, OR `policy_ref` naming a `skill_sets[]` entry. Under CP-056's deprecation, the second form becomes `skills_ref`. The CP-049 prose at integration MUST be updated to say "`skills_ref` naming a `skill_sets[]` entry" in place of "`policy_ref` naming a skill set declared in the §6.3 `skill_sets[]` block." This is a wording fix, not a semantic change; flagging here for the integration pass.
- **Length.** This draft (≈260 lines including the cover plan and draft notes) sits in the middle of the brief's 150–300 target range. The C4 design doc was 69 lines; the spec extension is ~3.7× larger because the spec carries normative wording, schemas, event entries, and cross-refs that the design doc summarized.
