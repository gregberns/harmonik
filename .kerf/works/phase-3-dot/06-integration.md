# Integration Review

> Pass-6 of kerf work `phase-3-dot`. Inputs: five pass-5 spec drafts (C1 workflow-graph, C2 execution-model §7.5 / EM-007 / §10.1, C3 handler-contract / EM-005 amendments, C4 control-points binding, C5 examples + review-loop.dot), the pass-5 review (`spec-draft-review.md`), and the entire `specs/` corpus (16 specs incl. `reconciliation/`).
>
> **Verdict: REQUEST_CHANGES (1 BLOCKER, 5 SHOULD-FIX, 6 NIT).** The blocker is C5 (the canonical example) using node-type vocabulary that contradicts C1's closed enum and uses an attribute name (`node_type`) C1 does not declare. Cannot ship pass-7 until C5 round-trips through C1's validator-as-written. The 5 SHOULD-FIX items are citation errors / terminology drift / a `workflow_ref` vs. `sub_workflow_ref` naming collision with existing EM. The NITs are deferrable cross-ref tightenings.

## Cross-Reference Checks Performed

For each pass-5 draft, the following existing specs were cross-checked. Spec files referenced below are at `/Users/gb/github/harmonik/specs/`.

| Draft | Specs cross-checked | Specific checks |
|---|---|---|
| C1 (`specs/workflow-graph.md` NEW) | `execution-model.md` (EM-001, EM-002, EM-005, EM-005a, EM-006, EM-007, EM-008, EM-009, EM-010, EM-011, EM-015d, EM-034 family, EM-040, EM-041, EM-041a, EM-043, EM-046a, EM-046b, §6.1, §6.4, §8); `handler-contract.md` (§4.1, §4.3, §4.5 HC-020, §4.5 typed errors, §4.5 Outcome, §5); `control-points.md` (§4.5 Gate, §4.6 Budget, §4.7 grammar, §4.13 CP-036); `architecture.md` (§4.6 amendment protocol, §4.10 three-artifact); `beads-integration.md` (§4.4 BI-010); `operator-nfr.md` (§4.3 needs-attention); `scenario-harness.md`; `reconciliation/spec.md` (§8) | every WG-NNN → EM-/HC-/CP- cross-ref resolved; closed-enum membership (status, OutcomeKind, FailureClass, node-type) verified vs. owning spec; attribute-name consistency vs. EM §6.1 schema |
| C2 (`specs/execution-model.md` §7.5 + EM-007 + §10.1) | `execution-model.md` (§4.1 EM-001, EM-005, EM-005a; §4.2 EM-006, EM-007, EM-008, EM-011; §4.3 EM-012, EM-012a, EM-015a, EM-015d, EM-015d-RFD, EM-015d-RIA; §4.5 EM-023a; §4.8 EM-034 family; §4.9 EM-038; §4.10 EM-041 family, EM-042, EM-042a, EM-043, EM-046a, EM-046b; §4.11 EM-051; §6.1; §6.4; §7.3; §7.4; §8; §10.1); `beads-integration.md` (BI-005, BI-010); `handler-contract.md` (§4.1, §4.5 HC-020); `control-points.md` (§4.5, §4.8 CP-040, §6.3, "Node-Type Binding"); `workspace-model.md` (§4.5, §4.7); `reconciliation/spec.md` (§8.4 Cat 3); C1 (sibling, for §-name citations); C3, C4 (sibling cross-refs) | §-anchor existence in EM; EM-NNN IDs not yet allocated; conformance-lift gating language; sibling-draft section name references (C1 §Schema, §Node Types, §Edge Conditions, §Edge Condition LHS Whitelist, §Cascade Invariant, §Context-Keys) |
| C3 (`handler-contract.md` §4.2a + §5.6; `execution-model.md` EM-005 v2 + EM-005a ext + EM-005b/c) | `handler-contract.md` (§4.1, §4.2 HC-006/8/10, §4.3, §4.4, §4.5 HC-020 + Err* sentinels, §4.5a HC-020a, §4.8 twin parity, §4.11 skill injection, §5 Invariants, §6.1 RECORD Outcome cite, §9, §10.2 Test-surface obligations); `execution-model.md` (§4.1 EM-005, EM-005a; §4.5 EM-023a; §4.9 EM-040 DOT validation; §4.10 EM-041 cascade + EM-041a context-updates + EM-046b RETRY; §8 failure classes; §6.1 RECORD Outcome; §6.4 schema bump); `control-points.md` (§4.1 CP-002, §4.5 Gate, §4.8 CP-040 persisted verdict, §6.1.1 Gate payload, planned §6.1.8); `reconciliation/spec.md` (§8.11 Cat 6a); `reconciliation/schemas.md` (§6.1 VerdictEvent); C1 §10 reserved-attribute set (for `context_keys`); C4 §6.1.8 CP-058 (cited destination of `GateDecisionPayload`) | every HC-NNN/EM-NNN/CP-NNN cross-ref resolved; EM-005 schema-bump sequencing (v0.3.3→v0.3.4); EM-005a `payload` row type currently reads `VerdictPayload | None` (sufficient if `VerdictPayload` is a union alias, but the type alias is named in §6.1 INFORMATIVE note as resolving only to `VerdictEvent` at MVH — see F3 below); EM-042a citation (replaces erroneous "EM-046c" — verified absent from drafts) |
| C4 (`control-points.md` §4.12 + §4.13 + CP-038a + §6.1.8 + §6.5 events) | `control-points.md` (§4.1 CP-002, §4.2 Gate CP-006/8/9, §4.5 Budget CP-022/23, §4.7 policy expressions, §4.8 CP-040/CP-040a persisted verdict + envelope hash, §4.9 CP-049/CP-050, §4.10 ownership split, §4.11 skills, §4.13 CP-036 by-name binding, §6.1 records, §6.3 skill_sets, §6.4 grammar, §6.5 events, §6.6 N-1, §9, §11 OQs, §12 revision history, CP-INV-003 cognition no-silent-replay); `execution-model.md` (§4.10 EM-041 / EM-042 / EM-042a; §7.5 EM-007 amendment in C2; §8 failure classes; §6.1 Outcome record); `event-model.md` (§6.3 payload schemas for new events); `handler-contract.md` (§Outcome `kind` discriminator); `reconciliation/spec.md` (§4.2 Cat 6 escalation); C1 (Node-type catalog); C3 (cites destination); C5 (no direct cross-ref) | CP-NNN ID range past CP-052; §4.7 CP-036 vs CP-055 `*_ref` list reconciliation; `policy_ref` deprecation impact on existing usages (CP-049's "policy_ref naming a skill_sets entry" prose; CP-036's enumerated list of legal refs); CP-038a vs CP-040a envelope hash symmetry; OQ-CP-006 deferral |
| C5 (`specs/examples/` README + `review-loop.dot`) | C1 (Node-type catalog, attribute name, edge dialect, terminal-node convention, schema_version); C2 (validator obligations, dispatch-table outputs); C3 (Outcome `preferred_label` verdict values); `execution-model.md` (EM-015d/EM-015e topology + `APPROVE/REQUEST_CHANGES/BLOCK` enum, EM-015d-RFD reviewer-feedback delivery, `completion_reason` enum `{approved, cap_hit, blocked, no_progress, error}`); `handler-contract.md` (agent-reviewer JSON schema v1 verdict enum); `scenario-harness.md` (two-layer testing surface) | every DOT attribute used vs. C1's reserved set; every node `type` value vs. C1 WG-001 closed enum; every edge-condition LHS / RHS literal vs. C1 §6 dialect; verdict-label values vs. EM §6.1 ENUM + agent-reviewer schema; terminal-node IDs vs. C1 §8 WG-022; `schema_version` and `terminal_node_ids` syntax vs. C1 §10 WG-031 reserved-attribute set + §11 WG-033 |

## Contradictions Found

### Contradiction 1 — BLOCKER — C5 dot uses node-type vocabulary that contradicts C1's closed enum

`05-spec-drafts/C5-review-loop.dot` declares nodes with `node_type="entry"` (line 37), `node_type="agentic"` (lines 45, 54), and `node_type="terminal"` (lines 61, 68). Two conflicts with C1:

- **Attribute name.** C1 uses the attribute `type` throughout — `05-spec-drafts/C1-workflow-graph.md:113` ("A node with `type=gate`..."), `:121` ("A node with `type=sub-workflow`..."), `:538-543` (`type="agentic"`...) and C1 §10 WG-031 line 378 lists `type` as a reserved attribute. The reserved-attribute list does NOT include `node_type`. Under C1's strict reserved-attribute policy (WG-031 strict positions), an attribute matching no reserved name on the *node* position would be permissive-with-warning — but `type` would then be *missing*, which is a strict failure per C1 §9 WG-024 ("The `type` attribute on every node is present").
- **Closed-enum membership.** C1 §4 WG-001 locks the node-type enum at `{agentic, non-agentic, gate, sub-workflow}`. Neither `entry` nor `terminal` is in the set. C1 WG-001 explicitly states a loader MUST reject a graph that declares a node with any `type` value outside this set.

Compounding: `C5-examples-README.md:21` advertises this as the canonical pinned example for C1's vocabulary: "Uses `node_type=\"entry\"`, `node_type=\"agentic\"`, and `node_type=\"terminal\"` — per `specs/workflow-graph.md §Node Types`." It is not — C1 §Node Types declares the opposite.

**Cited text — C1 (`05-spec-drafts/C1-workflow-graph.md:72`):**
> "The set of legal `type` values on a workflow node MUST be exactly `{agentic, non-agentic, gate, sub-workflow}`. A loader MUST reject a graph that declares a node with any `type` value outside this set..."

**Cited text — C5 (`05-spec-drafts/C5-review-loop.dot:37-68`):**
> `start [ node_type="entry", ...`
> `implementer [ node_type="agentic", handler_ref="claude-implementer", ...`
> `close [ node_type="terminal", ...`

**Resolution (BLOCKER for pass-7).** Patch C5 before pass-7. Two coordinated edits:
1. Rename attribute `node_type` → `type` on all five nodes. Update `C5-examples-README.md:21` accordingly.
2. Re-categorize `start` and the two terminal nodes. `start` is an `agentic` node only if a handler is bound; if its role is purely "emit the start event and follow the single outbound edge" (per the comment at `:34-35`), declare it as `non-agentic` with an inert handler, OR drop the explicit `start` node and use the EM §6.1 graph-level `start_node` attribute to point directly at `implementer`. The terminal nodes (`close`, `close-needs-attention`) carry no handler discipline at terminal-state-detection per C1 §8 WG-023 ("No outgoing edges are followed from a terminal node") — they need a `type` value from the closed enum nonetheless. Recommend `type="non-agentic"` with `handler_ref="noop"` (or whatever the daemon's no-op handler is), OR remove the explicit terminal-node attribute declarations entirely and rely on the graph-level `terminal_node_ids` list (which C5 already declares at line 31) being sufficient.

The second option (terminal-by-list-only) is cleaner if C1's intent is that membership in `terminal_node_ids` makes a node terminal regardless of its `type` declaration — but C1 §9 WG-024 says every node MUST carry a `type`. So a `type` value from the closed enum is required. Recommend `type="non-agentic"` for the terminal nodes with a no-op handler binding.

This is a BLOCKER because the canonical normative example is the witness that C1 + C2 + C3 are jointly implementable — if it doesn't round-trip the C1 validator as written, the pass-7 implementer epic has no anchor to verify against.

### Contradiction 2 — SHOULD-FIX — `workflow_ref` vs. `sub_workflow_ref` naming collision

C1 §4 WG-006 (`05-spec-drafts/C1-workflow-graph.md:121`) names the sub-workflow target attribute `workflow_ref`:

> "A node with `type=sub-workflow` MUST carry `workflow_ref` (a target workflow's `name` per [execution-model.md §4.1 EM-001]) and `workflow_version`..."

But existing `specs/execution-model.md:949` declares the schema field as `sub_workflow_ref`:

> `sub_workflow_ref    : String | None   -- required when type = sub-workflow`

And `execution-model.md:596` declares the evidence-map key `sub_workflow_ref` for the entry-checkpoint pin. C2 §7.5.3 item 7 sides with the EM existing name ("`sub-workflow` nodes MUST carry `sub_workflow_ref`...").

**Resolution.** C1 must rename `workflow_ref` → `sub_workflow_ref` to match the existing EM schema. This also affects C1 §10 WG-031's reserved-attribute set (currently lists `workflow_ref` — line 378) and the `sub-workflow` row of C1 §4 WG-002 (line 87). Mechanical rename; no semantic shift.

### Contradiction 3 — SHOULD-FIX — C5 verdict labels disagree with EM-015d enum

`C5-review-loop.dot:81,90,96` routes on lowercase verdict strings: `'approved'`, `'changes_requested'`, `'blocked'`.

EM-015d (`specs/execution-model.md:182,278,293,347-350`) and the agent-reviewer JSON schema v1 use uppercase: `{APPROVE, REQUEST_CHANGES, BLOCK}`. C1 §15.2 (line 264) and C1 example §15.1 also use uppercase: "`outcome.preferred_label == 'APPROVE'`".

Note the lowercase forms `{approved, cap_hit, blocked, no_progress, error}` are the `completion_reason` event-payload values per EM `RECORD Run` (`specs/execution-model.md:1006-1010`) — those are emitted by the daemon onto the `review_loop_cycle_complete` event, NOT what the reviewer emits as `outcome.preferred_label`. C5 has conflated the two enums.

**Resolution.** Update C5's three condition strings to use uppercase: `'APPROVE'`, `'REQUEST_CHANGES'`, `'BLOCK'`. Update C5 inline comments and README anchors accordingly. This is the same change C1's worked example (§15.1) already exemplifies.

### Contradiction 4 — SHOULD-FIX — C2 cites BI-005 as carrying a `workflow_ref` field; BI-005 does not

C2 §7.5.1 EM-055 step 1 (`05-spec-drafts/C2-execution-model-dot.md:39`) says:

> "The artifact path resolves from the run's bead via the bead's `workflow_ref` field per [beads-integration.md §4.3 BI-005] when present."

But `specs/beads-integration.md:111-115` BI-005 declares Beads owns `title`, `description`, and `type` — no `workflow_ref` field is defined. The Beads-owned bead schema in general (per BI-005 and BI-006) does not include a workflow pointer. Furthermore the cited anchor is §4.3 — BI-005 lives at §4.3 indeed (`Beads owns bead content`) but no field-level extension for workflow-mode targeting is anywhere in `beads-integration.md`.

**Resolution.** Either (a) declare the new `workflow_ref` field on the bead via a new BI-NNN requirement in `beads-integration.md` as part of phase-3-dot (cross-spec change, requires an integration-pass amendment to `beads-integration.md`), or (b) source the `.dot` artifact path purely from the per-daemon configuration / fallback chain (drop the per-bead override and reword C2 step 1). Recommend (b) at v1 to keep beads-integration.md untouched; (a) can be a follow-up bead. Either way, C2 §7.5.1 step 1 must lose the BI-005 reference as written.

### Contradiction 5 — SHOULD-FIX — C4 cite "[control-points.md §4.5 CP-023]" mismatch

C4 (`05-spec-drafts/C4-control-points-binding.md:28`) routes Budget binding "via the handler runtime for Budget per §4.5.CP-023." `specs/control-points.md:263` confirms CP-023 lives in §4.5 (Budget). OK — actually accurate. Withdrawn; not a contradiction.

### Contradiction 6 — SHOULD-FIX — C5 README cites `WG-T03`, which does not exist in C1

`C5-examples-README.md:20` reads: "Pinned by `specs/workflow-graph.md §WG-T03` (terminal-node declaration mechanism). The `terminal_node_ids` graph-level attribute..." C1's terminal-node requirements live at §8 WG-021/WG-022/WG-023 — there is no WG-T03 in C1.

**Resolution.** Replace `§WG-T03` with `§8 WG-021..WG-023` (or specifically WG-022 for the reserved IDs). Update the README anchor list at C5-README:20-25 to use real C1 anchors throughout.

## Consistency Issues Found

### CI-1 — SHOULD-FIX — C2 references "C1 §Schema" / "§Node Types" / "§Edge Conditions" / "§Cascade Invariant" / "§Context-Keys" / "§Schema Versioning" by section-name; C1 organizes by numbered sections (§4, §5, §6, §10, §11, §12)

C2 cites C1 by named anchors throughout (e.g., `05-spec-drafts/C2-execution-model-dot.md:40,41,56,74,75`). C1 does not use these as primary headings — its headings are numeric (`## 4. Node type catalog`, `## 6. Edge-condition language`, `## 11. Schema version`, `## 10. Unknown-attribute policy`).

**Resolution at pass-6.** Rewrite C2's C1-references to use the numeric anchors and the WG-NNN IDs that actually exist:
- "[workflow-graph.md §Schema]" → "[workflow-graph.md §10 WG-031, §11 WG-033]"
- "[workflow-graph.md §Node Types]" → "[workflow-graph.md §4 WG-001/WG-002]"
- "[workflow-graph.md §Edge Conditions]" → "[workflow-graph.md §6 WG-013]"
- "[workflow-graph.md §Edge Condition LHS Whitelist]" → "[workflow-graph.md §6 WG-014]"
- "[workflow-graph.md §Outcome-Carrier Routing]" → "[workflow-graph.md §7 WG-019]"
- "[workflow-graph.md §Cascade Invariant]" → "[workflow-graph.md §5 WG-011]"
- "[workflow-graph.md §Context-Keys]" → "[workflow-graph.md §10 WG-031]" (since C1 punts `context_keys` to the reserved-attr set and OQ-WG-002 — see CI-3 below)

### CI-2 — SHOULD-FIX — C1 §4 WG-002 attribute table still lists `policy_ref` as a legal optional attribute; C4 CP-056 deprecates it with `ErrDeterministic` rejection

Already flagged at pass-5 review F2. C1's table (lines 84-87) lists `policy_ref` as an optional attribute on `agentic`, `non-agentic`, `gate`, and `sub-workflow` nodes. C4 CP-056 hard-rejects any DOT attribute named `policy_ref` at workflow-ingest. Not a logical contradiction (a loader that implements C4's stricter rule still satisfies C1's permission to optionally accept `policy_ref` — by rejecting it), but a reader-facing inconsistency: C1's table tells the workflow author `policy_ref` is permitted; C4 says it is rejected.

**Resolution.** Update C1 WG-002's optional-attribute lists to drop `policy_ref` and add `skills_ref` + `freedom_profile_ref` per the typed-`*_ref` family in C4 CP-055. Add a note pointing to C4 CP-056. C1 §10 WG-031's reserved set (line 378) lists `policy_ref` — keep it there with a note that the name is reserved-and-rejected so the loader recognizes it for diagnostic purposes.

### CI-3 — SHOULD-FIX — `context_keys` declaration site disagreement between C3 HC-062 and C1 OQ-WG-002

C3 HC-062 (`05-spec-drafts/C3-handler-contract-outcome.md:215`) commits: "the `.dot` graph-level attribute `context_keys = \"key1,key2,...\"` per [workflow-graph.md §C1 design — context-key registry]". C3 commits this as a normative requirement.

C1 §10 WG-031 (`05-spec-drafts/C1-workflow-graph.md:378`) includes `context_keys` in the reserved-attribute set (per cross-component sweep item #3 already resolved). But C1 §13 OQ-WG-002 (line 467) explicitly leaves the **declaration mechanism** open: "The per-workflow context-key declaration mechanism (where context keys are declared, how their types are pinned, how a loader validates an edge condition's `context.<key>` against the declared schema) is pending." It does not normatively pin the DOT graph-level attribute as the declaration site.

So C3 commits a normative position that C1 frames as open. The two are reconcilable, but the bind is currently asymmetric.

**Resolution.** At pass-6 transcription, promote the C3 commitment to a C1 normative requirement: add a WG-NNN under §10 saying "`context_keys` is a graph-level DOT attribute, comma-separated identifier list per HC-062" and close OQ-WG-002 (or narrow it to "type-pinning is still open at v1"). Failing this promotion, demote HC-062 to a non-normative recommendation pending C1's resolution.

### CI-4 — NIT — Terminology drift: "workflow graph" / "DOT" / "DAG" / "graph"

The drafts use multiple terms across the same surface:
- C1 (`05-spec-drafts/C1-workflow-graph.md`) uses "workflow graph" (consistent throughout)
- C2 uses "`.dot` artifact" (16 occurrences), "DOT" (4 occurrences), "workflow graph" (3 occurrences), "graph AST" (1 occurrence) — all referring to the same artifact
- C3 uses "DOT" only in references to "DOT graph-level attribute" (1 occurrence in HC-062)
- C4 uses "DOT" 3× (in CP-055 and CP-056), "workflow-graph" 3× (in cross-refs), "graph" 1×
- C5 uses "DOT" (filename), "workflow graph" (README), "graph" (inline)

Not a blocker — the terms are used interchangeably in spec corpora elsewhere — but pass-6 transcription should lock one canonical noun ("workflow graph") and use "DOT" only as a qualifier for the on-disk artifact format. Add to C1 §3 glossary.

### CI-5 — NIT — `failure_class` is documented in three slightly-different shapes

C1 §7 WG-018 places `failure_class` "alongside `status` and `preferred_label` rather than nested" and says it MUST be present when `status == FAIL`, MUST be absent when `status == SUCCESS` or `PARTIAL_SUCCESS`, MAY be present when `status == RETRY`.

C3 EDIT B amendment text (`05-spec-drafts/C3-handler-contract-outcome.md:107`) says the field is "present ONLY when `status = FAIL`" (i.e., MUST be absent otherwise — slightly stricter than C1 WG-018's RETRY clause which allows it).

C3 HC-058 table row (line 52) says `failure_class` is "OPTIONAL on `FAIL`" for agentic and non-agentic — i.e., MAY be present, MAY be absent.

Three subtly-different bindings: C1 WG-018 says "MUST be present on FAIL"; C3 EDIT B says "present ONLY when FAIL" (silent on whether FAIL requires it); HC-058 says "OPTIONAL on FAIL." The probably-correct rule, given HC-059's "daemon back-fills when handler omits": the handler MAY omit it on FAIL (HC-058's OPTIONAL), but the post-classifier daemon-side Outcome MUST have it on FAIL (C1 WG-018, since the cascade reads it).

**Resolution.** Pass-6 transcription edits all three places to align on the handler-side rule (OPTIONAL on FAIL; absent on non-FAIL) and the engine-side rule (MUST be populated on FAIL after the daemon-side classifier runs, per C2 §7.5.2 clause 2). Frame this as the two-sided contract: handler-side optional emission, daemon-side mandatory back-fill.

### CI-6 — NIT — EM-005a `payload` row type-widening (per pass-5 review F3)

Pass-5 review F3 noted that `execution-model.md:1086`'s `RECORD Outcome.payload` row currently reads `VerdictPayload | None`, and C3 EDIT C adds `gate_decision` to the kind enum but does not spell out the type-widening. Confirmed: C3 §2.3 amends the EM-005 prose but does not patch the `RECORD Outcome` block's `payload` type alias.

**Resolution at pass-6.** Add a one-line patch to C3 EDIT C: amend EM §6.1 `RECORD Outcome` `payload` row to `: VerdictPayload | GateDecisionPayload | None` (or, cleaner, keep `VerdictPayload` as the union alias and amend the §6.1 INFORMATIVE note at execution-model.md:1103 to say the alias resolves to a union of `VerdictEvent` (for `kind=reconciliation_verdict`) and `GateDecisionPayload` (for `kind=gate_decision`)). The latter preserves EM-005a's design that `VerdictPayload` is an umbrella alias.

### CI-7 — NIT — `start_node` vs. `start_node_id` naming inconsistency

C1 §9 WG-027 (line 339) says "Exactly one node is declared as `start_node` per [execution-model.md §6.1]." C2 §7.5.3 item 4 (`05-spec-drafts/C2-execution-model-dot.md:76`) says "every non-terminal node MUST be reachable from the workflow's `start_node` (per §6.1 `Workflow.start_node_id`)". `execution-model.md` §6.1 (must check) uses `start_node_id` in the RECORD block. C1's WG-027 names the attribute as `start_node` but the underlying RECORD field is `start_node_id`. C5 (`05-spec-drafts/C5-review-loop.dot:30`) does NOT declare any `start_node` attribute (it omits it; relies on convention or position).

**Resolution.** Pass-6 should pick one canonical name (recommend `start_node` for the DOT attribute / `start_node_id` for the parsed record field — same convention as `terminal_node_ids`) and consistently reference. C5 should also add a `start_node="start";` graph-level attribute at line 30-31. Pair this fix with the C5 BLOCKER fix above.

## Cross-Reference Validity

Every `[text](file.md)`-style and `[file.md §N]` cross-reference in each draft was checked against the existing spec corpus. Results:

| Source | Target | Status |
|---|---|---|
| C1 → [execution-model.md §4.1 EM-001/-002/-005/-005a] | exists | OK (verified `specs/execution-model.md:113-149`) |
| C1 → [execution-model.md §4.2 EM-006/-007/-008/-009/-010/-011] | exists | OK |
| C1 → [execution-model.md §4.3 EM-015d] | exists | OK |
| C1 → [execution-model.md §4.9 EM-040/-043] | exists | OK |
| C1 → [execution-model.md §4.10 EM-034/-034a/-034b/-041/-046a/-046b] | exists | OK |
| C1 → [execution-model.md §6.1, §6.4, §7.5, §8] | §7.5 NEW per C2 (pre-existing §7.4 is end of §7 at line 1303); §8 exists; §6.1, §6.4 exist | OK (forward-ref to C2 §7.5 acceptable) |
| C1 → [handler-contract.md §4.1, §4.3, §4.4, §4.5] | exists (HC §-numbering verified at file headers) | OK |
| C1 → [control-points.md §4.5, §4.6, §4.7, §4.13 CP-036] | §4.5 = Budget; §4.6 = Role permissions; §4.7 = grammar; §4.13 does NOT exist (CP-036 lives at §4.7 line 344) | **CITATION ERROR.** §4.13 is a new subsection introduced by C4. C1 forward-cites it. Acceptable post-C4-landing. |
| C1 → [architecture.md §4.6, §4.10] | exists | OK |
| C1 → [beads-integration.md §4.4] | exists | OK |
| C1 → [operator-nfr.md §4.3] | exists | OK |
| C1 → [scenario-harness.md] | exists | OK |
| C1 → [reconciliation/spec.md §8] | exists (`specs/reconciliation/spec.md` §8 — verified by listing) | OK |
| C2 → [beads-integration.md §4.3 BI-005] | BI-005 exists at §4.3 line 111 but does NOT define `workflow_ref` | **See Contradiction 4.** |
| C2 → [workflow-graph.md §Schema/§Node Types/§Edge Conditions/etc.] | C1 does not use these as section names | **See CI-1.** |
| C2 → [handler-contract.md §Outcome] | not a section name; HC has §4.2a (new), §6.1 RECORD Outcome | **CITATION ERROR.** Use "§4.2a HC-058" or "§6.1 RECORD Outcome" |
| C2 → [control-points.md §Node-Type Binding] | not a section name; C4 introduces §4.12 | **CITATION ERROR.** Use "§4.12 CP-053/CP-054" |
| C2 → [workspace-model.md §4.5, §4.7] | exists | OK |
| C2 → [reconciliation/spec.md §8.4 Cat 3] | exists | OK (verified) |
| C2 → [specs/examples/review-loop.dot] | C5 artifact | OK |
| C3 → [handler-contract.md §4.5 HC-020] | exists (line 299) | OK |
| C3 → [execution-model.md §4.1 EM-005/-005a, §8, §4.10 EM-041/-041a/-046b, §4.9 EM-040, §6.4, §4.5 EM-023a] | exist | OK |
| C3 → [control-points.md §4.8 CP-040, §6.1.8 CP-058, §4.1 CP-002] | CP-040 exists (§4.8); §6.1.8 NEW per C4; CP-058 NEW per C4 | OK (forward-ref to C4) |
| C3 → [reconciliation/schemas.md §6.1] | exists | OK (existing EM-005a cite) |
| C3 → [architecture.md §4.6] | exists | OK |
| C3 → [reconciliation/spec.md §8.11] | exists | OK |
| C3 → [workflow-graph.md §C1 design — context-key registry] | "§C1 design" is not a heading anywhere; phrasing is informal | **CITATION ERROR / wording.** Pass-6 should use a real anchor: "[workflow-graph.md §10 WG-031]" once C1 lands. See CI-3. |
| C4 → [workflow-graph.md §4.x Node-type catalog] | C1 §4 WG-002 | **NIT-grade citation imprecision** (should be `§4 WG-001/WG-002`); fix at pass-6. |
| C4 → [execution-model.md §7.5 EM-007 amendment] | C2 §4.2 (in-place EM-007 amendment), and §7.5 introduces new EM-055..EM-059 | **NIT.** C4 cites "§7.5 EM-007 amendment" but the amendment is at §4.2 in C2. The amendment is *consumed* by §7.5 but lives in §4.2. Pass-6 should refine to "[execution-model.md §4.2 EM-007 amendment + §7.5]." |
| C4 → [handler-contract.md §Outcome / §4.x / §4.11] | "§Outcome" is not a section name; §4.11 = skill injection (exists) | **CITATION ERROR.** Use "[handler-contract.md §4.2a + §6.1 RECORD Outcome]" |
| C4 → [event-model.md §6.3] | event-model.md exists; §6.3 anchor must be verified | NOT VERIFIED in this review pass (deferrable). Pass-6 should confirm event-model.md §6.3 is the payload-schema home. |
| C4 → [reconciliation/spec.md §4.2] | exists | OK |
| C5 README → workflow-graph.md §WG-T03 | does not exist | **See Contradiction 6.** |
| C5 README → execution-model.md §EM-015d/§EM-015e | exist | OK |
| C5 README → handler-contract.md §Handler Binding / §Outcome | not actual section names | **CITATION ERROR.** Use "[handler-contract.md §4.1 HC-001]" and "[handler-contract.md §4.2a + §6.1 RECORD Outcome]" |

Summary: 8 citation errors needing pass-6 cleanup (3 from C2 referencing non-existent C1 named-anchors; 1 each from C3, C4, C5; 1 from C2 to BI-005 with no `workflow_ref` field; the WG-T03 in C5). All are mechanical fixes once C1/C2/C4 land at canonical anchor names.

## Changelog Verification

`05-spec-drafts/05-changelog.md` accurately accounts for every draft file delivered (six files including `C5-review-loop.dot` and `C5-examples-README.md`). The Files-affected table, Requirement-IDs-assigned table, D-decisions-traced table, and Cross-component-sweep-resolved table all match the drafts as written.

Two minor changelog-vs-draft consistency issues:

- **CV-1.** Changelog line 14 says "C4 §4.12 + §4.13 + §6.1.8 + §4.7 (CP-038a) + §6.5". C4 draft delivers all five (§4.12, §4.13, §6.1.8 via §4.13's RECORD block, CP-038a as §4.7 amendment, §6.5 event additions). OK.
- **CV-2.** Changelog line 21 says "execution-model.md: 7 new requirements EM-055 … EM-061 (+ in-place EM-007 amendment, in-place §10.1 lift)." C2 draft assigns EM-055 through EM-061, with EM-060 and EM-061 explicitly marked as bookkeeping handles for in-place amendments. OK.

The top-level `phase-3-dot/05-changelog.md` (separate from `05-spec-drafts/05-changelog.md`) was not re-checked field-by-field in this pass; the in-spec-drafts version is the operative authoritative changelog for pass-6 transcription. Pass-6 should confirm the two changelogs are consistent.

The Required-pass-6-follow-up section (line 64-66) correctly notes the 10 test beads (5 components × {scenario, exploratory}) are a pre-condition for advancing into pass-6 integration (per jig). These have not been filed. This is a JIG-LEVEL gate, not a content gate — pass-7 (Tasks) cannot start until those beads exist.

**Changelog verdict: accurate.** The drafts and the changelog are in sync. No discrepancies that block pass-7.

## Final Assessment

The spec corpus is **structurally coherent but not yet ship-ready as-is**. The four normative drafts (C1, C2, C3, C4) hold together remarkably well: the cross-component sweep already documented in the changelog (gate `handler_ref` amendment, `GateDecisionPayload` ownership in C4, `context_keys` reserved-attribute alignment) caught the three pre-existing contradictions before the reviewer saw them; the remaining cross-spec contradictions are all between a draft and the existing corpus (BI-005, EM `sub_workflow_ref` naming, EM-015d verdict enum) or between C5 and C1 (the BLOCKER). No pair of normative *drafts* contradicts each other.

The **one BLOCKER** is C5's `review-loop.dot` violating C1's closed node-type enum and using the wrong attribute name. Until C5 is round-tripped through C1's WG-001/WG-002/WG-024 as written, the pass-7 implementer epic has nothing to verify against — `internal/workflow/examples_test.go` (the layer-1 static round-trip test C5 README defines) would fail on day one. The fix is mechanical (~10 lines of C5) and does not affect any normative requirement.

The **5 SHOULD-FIX items** are either: (a) citation errors from drafts that referenced not-yet-existing anchors by section-name rather than by ID (fixable mechanically at pass-6 transcription), or (b) terminology / naming corrections (lowercase-vs-uppercase verdicts in C5; `workflow_ref` vs. `sub_workflow_ref` in C1; `policy_ref` deprecation reflected in C1 §4 WG-002; BI-005 / `workflow_ref` citation in C2). None of these reopens a design decision; all are surface-level fixes.

The **6 NITs** are deferrable — cross-spec ref tightenings, glossary unification, the `failure_class` three-shape alignment, the EM-005a `payload` type-alias widening. Pass-7 can proceed with NITs deferred to a follow-up; pass-6 transcription is the natural time to close them but they do not block pass-7 task spawning.

**Recommendation.**
1. Patch C5 (BLOCKER — Contradiction 1, ~10 lines): rename `node_type`→`type`, re-categorize the five nodes into C1's closed enum, drop or replace `node_type="entry"` and `node_type="terminal"`. Update README anchors (CI-7, Contradiction 6).
2. Patch C1 + C5 (SHOULD-FIX — Contradictions 2, 3): rename `workflow_ref`→`sub_workflow_ref`; update C5 verdict strings to uppercase.
3. Patch C2 (SHOULD-FIX — Contradiction 4, CI-1): drop the BI-005 `workflow_ref` reference (source artifact path from config/fallback only at v1); rewrite all named-anchor C1 references to use WG-NNN IDs.
4. Apply pass-5 review F1/F2/F3 (NIT — `agent_type` catalog anchor, C1 `policy_ref` deprecation note, EM-005a `payload` type union) — already known and deferred.
5. File the 10 test beads (jig pre-condition).
6. Then advance to pass-7 (Tasks).

The substantive design work is sound. The remediation is editorial.

## Pass-6 Reviewer Addendum (added post-APPROVE)

The pass-6 reviewer APPROVED the analysis above (see `integration-review.md`) and contributed **one additional SHOULD-FIX** missed by the original pass:

### CI-8 — SHOULD-FIX — `execution-model.md` `ENUM NodeType` still lists `control-point`

`specs/execution-model.md` (around line 953) declares:

> `ENUM NodeType { agentic, non-agentic, gate, sub-workflow, control-point }`

C1 §4 WG-001 amends this enum by removing `control-point` ("the `control-point` value … is NOT a node type in `workflow_mode=dot`"), but the EM `ENUM NodeType` block itself needs an in-place edit to drop `control-point`. C4 §4.12 / §4.13 also reinforces this: control-points bind by node-`type` value, not by being a node-type themselves.

**Resolution.** Fold into pass-7 as part of the C1↔EM reconciliation task: edit `specs/execution-model.md` ENUM NodeType to `{agentic, non-agentic, gate, sub-workflow}`. Mechanical edit, no design impact.
