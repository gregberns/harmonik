# EM Pilot Decomposition-Quality Review (r1) — 2026-04-27

Reviewer: decomposition-quality reviewer (per `pilot-review-protocol.md` v0.2 §3.2). Pilot under review: `docs/decompose-to-tasks/em-pilot.md` v0.1.0 against `specs/execution-model.md` v0.3.3, with discipline `docs/decompose-to-tasks/discipline.md` v0.6 (post-AR-r1+r2 + F-pilot-AR-10 patch).

**Result.** The pilot is structurally sound at the granularity / coalesce / sensor / schema level — descriptions faithfully cite their source requirements, the four §2.3 coalesces are well-justified by the shared-shape / shared-procedure test, the F8b shared-function-body tiebreaker is applied correctly to EM-016 and EM-038 (so the zero-multi-step-splits decision is sound), and the three sensor beads are real verification mechanisms (not invariant restatements). Most defects cluster around the v0.6 supporting-cite reclassifications (one is mis-justified — EM-040 should fire §3.1 step 5 invariant-as-target exemption, not F-pilot-AR-10 supporting-cite; this is a docs-tightening rather than an outcome-changing finding) and around several missed term-use edges per §3.1 step 5 that the pilot enumerates inconsistently. Two `class` findings about discipline gaps surface (typed-ID-alias coalesce silence already self-flagged as F-pilot-EM-5, dual-ownership trailer-row pattern self-flagged as F-pilot-EM-6); a third class finding about the supporting-cite-vs-invariant-target overlap is new.

## 1. Sampled beads (14)

Weighted toward riskier classes per protocol §3.2:

- **All 4 §2.3 coalesces (Q1+Q2):** `em-005`, `em-023`, `em-041`, `em-043`.
- **The 0 multi-step splits flagged for revisit (Q1+Q3):** `em-016`, `em-038` (the F8b tiebreaker calls).
- **All 3 sensors (Q1+Q4):** `em-inv-001`, `em-inv-004`, `em-inv-005` (esp. `em-inv-005` per F-pilot-EM-7).
- **4 schema beads (Q1+Q5):** `em-schema.run-id` (typed-ID alias, F-pilot-EM-5), `em-schema.checkpoint-trailers` (primitive-shape with dual-ownership rows, F-pilot-EM-6), `em-schema.outcome` (touched by v0.3.3 RC patches; structurally non-trivial), `em-schema.transition` (touched by v0.3.0 outcome_status promotion; cross-spec edge to AR-032).
- **3 random first-class beads (Q1, regression check):** `em-001`, `em-040`, `em-046b`.

## 2. Per-sample findings

(Sampled beads with NO issue: `em-016` Q3, `em-038` Q3 [§2.2 F8b applied correctly — see §2.5 below]; `em-005` Q2, `em-023` Q2, `em-041` Q2 [coalesces sound]; `em-inv-001` Q4, `em-inv-004` Q4 [sensors are real mechanisms with named persona/scenario tests]; `em-schema.run-id` Q5, `em-schema.outcome` Q5, `em-schema.transition` Q5; `em-001` Q1, `em-046b` Q1. They pass cleanly.)

### 2.1 `em-005` — coalesce is sound; one supporting-cite re-classification questionable

- **Spec reqs covered:** EM-005 + EM-005a (coalesced).
- **Question flagged:** Q1 (description-vs-spec on the AR §4.6 supporting-cite reclassification) + Q2 (coalesce soundness).
- **Q2 result.** PASS. EM-005 declares the Outcome record; EM-005a extends with `kind` discriminator + `payload` envelope (additive in v0.3.3). They share the Outcome shape (single record); separately implementing them produces beads whose descriptions reduce to "see anchor" — exactly the §2.3 trigger. The pilot's coalesce description correctly enumerates both source IDs (`req:EM-005, req:EM-005a`) and the derived schema beads (`em-schema.outcome`, `em-schema.outcome-kind`, `em-schema.outcome-status`).
- **Q1 concern.** EM-005a's body (line 120) contains: "future outcome variants … extend the enum via the amendment protocol per [architecture.md §4.6] and MUST cite their payload schema in the owning subsystem spec. Adding a discriminator value is an additive schema change per §6.4 (N-1 readable per §4.4.EM-022)…" The pilot reclassifies the `[architecture.md §4.6]` cite as supporting per F-pilot-AR-10 with the rationale "future outcome variants extend via amendment protocol — sub-clause about future extensions, not load-bearing for the current shape." Apply F-pilot-AR-10's operational test: remove the `[architecture.md §4.6]` cite. The remaining sentence is "future outcome variants extend the enum via the amendment protocol and MUST cite their payload schema." Is this independently testable? The "amendment protocol" itself is the load-bearing concept (without that anchor, "amendment protocol" becomes a dangling term). AR §4.6 is the section that defines the amendment protocol. **However**, the cite is attached to a forward-looking conditional ("future outcome variants…"), not to a current normative obligation. EM-005a's MVH normative content (the `kind` discriminator + payload envelope) does not depend on the amendment-protocol cite being resolvable for current-version validity. **Reading the operational test charitably:** the cite supports a future-extension forward declaration; supporting-cite call defensible. But borderline.
- **Severity.** MINOR. The reclassification is defensible but the rationale in the pilot's row note is thin ("sub-clause about future extensions, not load-bearing for the current shape" understates the load-bearing role of the amendment-protocol concept).
- **Lane.** `class`. The supporting-cite test (F-pilot-AR-10) handles the typical case (peer-rule cross-reference for consistency); future-extension forward declarations are a borderline pattern not explicitly addressed. Multiple specs will hit "future variants extend via amendment protocol" cites. Per protocol §4.1 probe 3 (silence): a gap in coverage is `class`.
- **Suggested fix.** Pilot: rewrite the row note to be explicit ("the cite attaches to a forward-extension statement that does not affect MVH-level testability of EM-005a's current discriminator-and-payload shape; supporting-cite per F-pilot-AR-10"). Discipline: §3.1 step 1 may benefit from a worked example showing a forward-extension cite as a typical supporting-cite.

### 2.2 `em-023` — coalesce is sound; description faithful

- **Spec reqs covered:** EM-023 + EM-023a (coalesced).
- **Question flagged:** Q1 + Q2.
- **Q2 result.** PASS. EM-023 names the cadence rule ("one commit per durable transition"); EM-023a is the durability decision procedure (the table of `transition_kind × outcome_status` valid combinations + PARTIAL_SUCCESS evidence + synthesized-Outcome carve-out). They share the durability-check code path: an implementer of EM-023 cannot test correctness without implementing the EM-023a table, and EM-023a's table does not stand without the cadence rule it constrains. Inseparable.
- **Q1 result.** PASS. The pilot description enumerates the durable kinds, outcome.status values, the partial_success evidence flag, the new outcome_status field on Transition (added in v0.3.0), and the daemon-synthesized-outcome carve-out for context-restore + reconciliation. Faithfully tracks lines 287–311 of the spec.

### 2.3 `em-041` — coalesce is sound; "term-use of EM-046" missing intra-spec edge

- **Spec reqs covered:** EM-041 + EM-041a (coalesced).
- **Question flagged:** Q1 + Q2.
- **Q2 result.** PASS. EM-041 is the cascade ordering; EM-041a is the pre-cascade context-update ordering. They share the cascade entry point (the cascade reads context that EM-041a established). Inseparable.
- **Q1 concern.** EM-041's pilot row lists predecessors `em-005, em-002, em-schema.outcome, em-schema.edge`. EM-041's spec body uses the term "cascade" mechanically (well-defined within the rule itself). EM-041a's body "applied to the run's shared `context` BEFORE the edge-selection cascade of §4.10.EM-041 evaluates condition expressions" is itself the rule, so no out-edge. No missed term-use issue at this row directly.
- **Severity / Lane.** None for this row. NO finding.

### 2.4 `em-043` — coalesce is sound

- **Spec reqs covered:** EM-043 + EM-043a (coalesced).
- **Question flagged:** Q2.
- **Result.** PASS. EM-043 is the cycle-bounding rule (per-edge cap → fail with `compilation_loop`); EM-043a is the per-(run_id, edge) counter storage rule. Same code path; the implementer of EM-043 cannot ship without the counter store. Coalesce sound.

### 2.5 `em-016` and `em-038` — F8b tiebreaker applied correctly

- **Spec reqs covered:** EM-016, EM-038.
- **Question flagged:** Q3 (multi-step split soundness via §2.2 F8b).
- **Q3 result for EM-016.** PASS. EM-016's spec body (line 216): "tree construction (`git write-tree`), commit-object creation (`git commit-tree` …), and reference advance (`git update-ref` …) execute as a sequence whose atomicity boundary is the reference advance." Three steps, exactly. Independently testable in principle (each git op breakable on its own). Umbrella loses meaning if stripped (the atomicity-boundary clause is the entire reason this is normative). All three §2.2 signals fire. **F8b applies:** the three git ops are exactly one cohesive function body in any plausible Go implementation (a `func checkpoint(...)` that calls `git.write-tree → git.commit-tree → git.update-ref` in sequence with no stable testable boundary between them other than the third call returning success). The pilot's F8b decision is correct: keep one bead with three sub-bullets in the description.
- **Q3 result for EM-038.** PASS. EM-038's spec body (lines 471–482) lists 6 validator categories: DOT parseability, sub-workflow resolution + acyclicity, reference resolution, attribute type checks, reachability, cycle-bound check. Three signals fire. **F8b applies:** the six categories share the validator code body — one validation function that runs each check in sequence and accumulates failures. The pilot's F8b decision is correct: keep one bead with the six categories as sub-bullets.
- **Severity / Lane.** None. NO finding. The F-pilot-EM-1 self-flagged class finding (request that discipline §2.2 grow a worked example showing F8b firing) is well-motivated and aligns with this reviewer's read.

### 2.6 `em-inv-005` — sensor description quality is good but predecessor-list emptiness is structurally wrong

- **Spec reqs covered:** EM-INV-005.
- **Question flagged:** Q4 (sensor as real mechanism) + Q1 (predecessor derivation).
- **Q4 result.** PASS on description. EM-INV-005's pilot description is a real sensor: "inject Beads-`closed`-no-merge-commit divergence and assert Cat 3 dispatch (NOT silent Beads-side correction); inject JSONL-references-missing-commit divergence and assert Cat 3 dispatch." Two named scenario tests with concrete injection conditions and assertion targets. This is a real verification mechanism, not an invariant restatement.
- **Q1 concern (the F-pilot-EM-7 case).** EM-INV-005's body (line 584) inline-cites only RC-owned and BI-owned IDs (RC §8.4 Cat 3 + BI §4.7 BI-022). With depends-on=architecture only, both filter out per §3.2. The pilot correctly sets the predecessor list to `(none — see notes)`. **However**, the spec body also enumerates the *constrained* surface — "if Beads reports a bead as `closed` but no merge commit with `Harmonik-Bead-ID` matching" — which uses the `Harmonik-Bead-ID` trailer term (owned by EM's own §6.2 trailer registry) and the merge-commit concept. Per discipline §3.1 step 5 (term-use edge), `em-inv-005` could fire term-use edges to `em-schema.checkpoint-trailers` (where `Harmonik-Bead-ID` is registered) and to `em-014` (where bead-tied runs are declared, and where `Harmonik-Bead-ID` semantics anchor). Neither edge is emitted. The "empty predecessor list" finding the pilot self-flagged as F-pilot-EM-7 is real, BUT the right fix is to apply the term-use rule, not to invent a sensor-degeneracy clause.
- **Severity.** MAJOR (predecessor list is incomplete; sensor cannot be implemented if the trailer registry isn't defined yet).
- **Lane.** `local` for the missing term-use edges (pilot didn't apply §3.1 step 5 to invariants — though F-pilot-AR-r2-2 says term-use edges from impl reqs to invariants don't fire, the *reverse* direction — invariants having term-use edges to schemas/impls they reference — is permitted and consistent with §2.5 F12 sensor↔impl one-way direction). A `class` co-tag is also warranted: the discipline's §3.1 step 5 doesn't explicitly say whether sensor beads can themselves emit term-use edges (the §2.5 v0.6 three-source enumeration is exhaustive for §10.2 derivation but doesn't address the *invariant-body* term-use case directly). Per protocol's bias rule, both tagged: primarily `class`.
- **Suggested fix.** Pilot: add `em-schema.checkpoint-trailers` and `em-014` as `em-inv-005` predecessors (term-use of `Harmonik-Bead-ID` trailer and bead-run linkage). Drop the F-pilot-EM-7 finding entirely OR restate it as "the discipline's §2.5 three-source enumeration may benefit from a fourth source: invariant-body term-use edges to the schemas/impls the invariant predicates over." Discipline §2.5 may benefit from explicit guidance: invariant-body term-uses derive sensor predecessor edges per §3.1 step 5, in addition to the §10.2 three-source enumeration.

### 2.7 `em-inv-001` — sensor predecessor list could include schema dependencies

- **Spec reqs covered:** EM-INV-001.
- **Question flagged:** Q4 + Q1.
- **Q4 result.** PASS. The sensor description names a concrete restart scenario test and a cross-spec scenario harness — real verification mechanisms.
- **Q1 concern.** Pilot row lists predecessors `em-031, em-031a, em-032, em-033`. The §10.2 group cite is "EM-031..EM-033 (state reconstruction)" which the pilot correctly enumerates plus EM-031a (a sub-req under §4.7). Spec body uses the terms "git checkpoint trail" (defined by the EM-016/EM-017 checkpoint contract) and "Beads store" (forward-cite filtered). The git-checkpoint-trail term-use *could* fire edges to `em-016` (checkpoint definition) and `em-017` (trailer schema) — both of which are required for the sensor's restart scenario test to even exist. Pilot does not emit these edges. Same shape as 2.6.
- **Severity.** MINOR (the §10.2 group cite already pulls in EM-031..EM-033 which transitively block on EM-016/EM-017, so the readiness ordering is preserved transitively even without these direct edges).
- **Lane.** `class` — same root cause as 2.6 (discipline silence on invariant-body term-use). Folded into the fix in 2.6.

### 2.8 `em-schema.checkpoint-trailers` — dual-ownership pattern surfaced correctly; one missing edge

- **Spec reqs covered:** §6.2 trailer registry (with two RC-owned rows).
- **Question flagged:** Q5 + Q1.
- **Q5 result.** PASS. The pilot description enumerates all 7 keys plus the RC-owned `Harmonik-Verdict-Executed` known-extension. Names value types, required/conditional status, and the §6.2 informative-note ownership annotation.
- **Q1 concern.** No outgoing edges in the pilot row (`(none)`). Per §3.1 step 5 term-use, every other bead that references a trailer key would fire an edge TO this schema bead (which the pilot does emit at em-017's row). But the trailer registry itself doesn't *use* terms whose definition is intra-EM — the trailer values reference UUID/Integer/Enum primitive types, none of which are EM-defined records. So "no outgoing edges" is correct from a term-use lens. **However**, the row notes claim "RC-owned trailer cross-listing per EM v0.3.3 §6.2 informative note" — the dual-ownership pattern surfaced as F-pilot-EM-6 is real and the pilot's choice (mint as a single EM bead, with RC-ownership annotated in description) is defensible. The §6.5 co-owned event-payload pattern (which discipline §2.11(d) addresses) is structurally analogous: there, EV's pilot will own the schema bead and EM's emitting beads edge to it. Here the registry-owning spec is EM but two specific *rows* are owned by RC — a different shape than co-owned events.
- **Severity.** MINOR (the bead is well-formed; the dual-ownership pattern is correctly self-flagged as a class finding).
- **Lane.** `class`, already self-flagged as F-pilot-EM-6.

### 2.9 `em-schema.transition` — Q5 field list complete; cross-spec edge to ar-032 sound

- **Spec reqs covered:** §6.1 RECORD Transition.
- **Question flagged:** Q5 + Q1.
- **Q5 result.** PASS. The pilot enumerates all 13 fields including the v0.3.0-added `outcome_status` field and the reserved evidence keys (`sub_workflow_pin`, `synthesized_outcome`). Spec lines 695–711 match exactly.
- **Q1 result.** PASS on the cross-spec edge `em-schema.transition → ar-032`. AR §4.8 is "Role taxonomy" and AR-032 is the "Seven roles named" rule (verified at architecture.md line 295). The §6.1 Transition record's `actor_role` comment is "role name per [architecture.md §4.8]; {daemon, reconciliation} for synthesized outcomes per §4.10.EM-046" (line 700). The cite is to a section anchor (§4.8), not a specific req ID, but the term-use rule (§3.1 step 5) pins to AR-032 because AR-032 is the rule that enumerates the 7-role vocabulary. Term-use pinning is sound.

### 2.10 `em-040` — supporting-cite reclassification is mis-justified (uses F-pilot-AR-10 where F-pilot-AR-r2-2 invariant-as-target should fire)

- **Spec reqs covered:** EM-040.
- **Question flagged:** Q1 (description-vs-spec on the supporting-cite reclassification rationale).
- **Concern.** The pilot's row note for `em-040` reads: "The `[architecture.md §4.9]` cite is supporting per F-pilot-AR-10 — EM-040's main claim ('Submission paths that skip validation are structural violations') is independently testable; 'centralized-controller principle' is a thematic anchor not a load-bearing input." Apply the F-pilot-AR-10 operational test: remove the cite. Remaining: "Submission paths that skip validation are structural violations of the centralized-controller principle." The phrase "of the centralized-controller principle" is a load-bearing concept — without that anchor, "structural violations" is undefined (structural violations of WHAT?). The "centralized-controller principle" was promoted to AR-INV-007 in AR v0.3.0 (per spec line 327: "AR-037 — Retired (promoted to AR-INV-007)"). EM-040's cite is to `[architecture.md §4.9]` which is the section that NOW houses AR-INV-007 (and AR-038/AR-039/AR-040). **The correct rationale for not emitting an edge** is the v0.6 §3.1 step 5 invariant-as-target exemption (F-pilot-AR-r2-2): emitting `em-040 → ar-inv-007` would reverse the §2.5 F12 sensor↔impl one-way rule (since EM-040 is itself a likely AR-INV-007 sensor predecessor when AR's pilot resolves §10.2 conformance-auditor bundling). The pilot's own §5 supporting-cite list says "The cite to AR-INV-007 (centralized-controller invariant) would also fail the §3.1 step 5 invariant-as-target exemption (F-pilot-AR-r2-2)" — so the pilot author IS aware of the invariant-target rule but invoked it as secondary rather than primary. The supporting-cite test would have produced a different answer (cite IS load-bearing to "structural violations" being well-defined); the invariant-as-target test is the one that genuinely fires.
- **Severity.** MINOR (outcome is correct: no edge emitted. The justification is mis-prioritized.) Not BLOCKER because the no-edge result is correct under either rationale; not MAJOR because no downstream behavior diverges.
- **Lane.** `class`. The discipline doesn't currently address rule-precedence when both F-pilot-AR-10 and F-pilot-AR-r2-2 could plausibly apply. Multiple specs will cite cross-cutting principles that are now invariants (AR-INV-007 centralized-controller, AR-INV-008 spec-as-source-of-truth, etc.) and the rule-precedence question will recur.
- **Suggested fix.** Pilot: rewrite `em-040` row note to lead with F-pilot-AR-r2-2 ("the cite-target AR §4.9 houses AR-INV-007 centralized-controller invariant; per §3.1 step 5 invariant-as-target exemption, no edge fires"); F-pilot-AR-10 supporting-cite analysis is moot when invariant-as-target already excludes the edge. Discipline: §3.1 may benefit from explicit precedence guidance ("when multiple no-edge rules apply, the invariant-as-target exemption takes precedence over supporting-cite analysis"). The `em-001 → AR §4.10` and `em-005a → AR §4.6` reclassifications in the pilot's §5 list are clean F-pilot-AR-10 cases (those AR sections are not invariants); only `em-040 → AR §4.9` has the rule-precedence ambiguity.

### 2.11 `em-001` — supporting-cite reclassification is borderline; CP forward-cite mis-attributed

- **Spec reqs covered:** EM-001.
- **Question flagged:** Q1.
- **Concern.** Pilot row notes: "The DOT-doc cite to `[architecture.md §4.10]` is supporting per F-pilot-AR-10 (EM-001's claim is independently testable with the AR-§4.10 reference removed); no AR edge." Apply the F-pilot-AR-10 operational test: EM-001's body (line 81) ends "On-disk representation is a DOT document per [architecture.md §4.10] three-artifact separation." Remove the cite: "On-disk representation is a DOT document." Independently testable? Yes — the DOT-doc claim is the EM-001 declaration. The "three-artifact separation" thematic anchor is a description of WHY DOT was chosen, not a load-bearing input to whether something is or isn't a workflow. Supporting-cite call is sound. PASS on this part.
- **Second concern.** The same row also says "The `[control-points.md §6.3]` cite is forward (depends-on=architecture only); see F-pilot-EM-2." But EM-001 line 81 cites `[control-points.md §6.3]` for "an ordered reference list of policies resolved at workflow-load time (cited from [control-points.md §6.3])". Per §3.1 step 1, this is a hard dep cite (the resolved-policy list IS load-bearing for the Workflow record's structural correctness — without resolving policies, the workflow is incomplete). Forward-cite filtering (depends-on=architecture only) is the correct procedure; calling it "forward" per F-pilot-EM-2 is the pilot's chosen lane (Option B per §8). Consistent with §3.2 + the pilot's stated forward-cite policy.
- **Severity.** None. NO finding.

## 3. Missing-coalesce flags

The pilot self-flagged F-pilot-EM-5 (six typed-ID aliases as separate primitive-shape beads vs candidate single coalesce of the 5 thin aliases). Reviewer validation:

**Validate F-pilot-EM-5 call.** The 6 typed-ID aliases (RunID, StateID, TransitionID, NodeID, BeadID, CommitRange) span:

- 5 thin UUID/String wrappers (RunID, StateID, TransitionID, NodeID, BeadID) — each is one TYPE declaration in §6.1. Pure typedef, no constraints beyond the underlying primitive.
- 1 tuple shape (CommitRange — a record with two fields) — structurally distinct.

Apply discipline §2.3 coalesce test to the 5 thin aliases:

1. **Single data shape or single code path?** Yes — all 5 are typed-alias declarations. In Go, all 5 would live in a single `types.go` file with 5 `type X = uuid.UUID` lines (or `type X string`). One implementer, one diff, one PR.
2. **Anchor-and-clarifications shape?** Weak. The 5 aliases are peer rules, not anchor-plus-clarifications. There is no obvious "anchor" alias; they are conceptually parallel.
3. **Splitting reduces to "see anchor"?** Yes. The descriptions are nearly identical: "Implement TYPE X = primitive (notes: ...)". An implementer reading 5 separate beads would observe redundant work-item count.

Test 1 fires; test 2 fails (no anchor structure); test 3 fires. Per §2.3 the cluster falls SHORT of the strict three-AND test (test 2 fails). The pilot's choice to keep 5 separate beads is mechanically correct under v0.6 discipline. **The class-lane fix the pilot proposes — discipline §2.6 grow guidance for typed-alias clusters — is the right call**, since multiple specs (EV payload-type aliases, RC verdict-shape aliases) will hit the same question. Not a coalesce smell at v0.6 — a coverage gap in §2.6 / §2.3 interaction.

Other coalesce-candidate clusters considered:

- **EM-027 + EM-028 + EM-029 + EM-030 (outcome spine).** Four sequential rules: outcome flow integration (EM-027), record-as-canonical (EM-028), event-must-not-duplicate (EM-029), full-fidelity-from-git (EM-030). They share the outcome-spine theme but address ORTHOGONAL concerns: EM-027 is about flow segmentation (no bypass), EM-028 is about canonical-form designation, EM-029 is about event-payload constraint, EM-030 is about consumer-side read paths. Each is independently testable and independently buggy. Same shape as discipline §2.3's BI-025a..e example (orthogonal concerns kept separate). Correctly NOT coalesced.
- **EM-034 + EM-034a + EM-034b + EM-034c (sub-workflow rules).** Four rules: expansion semantics (EM-034), node-ID namespacing (EM-034a), reference graph acyclicity (EM-034b), expansion-pin durability (EM-034c). EM-034 is the umbrella concept; EM-034a/b/c are clarifications/constraints/durability. These ARE candidates — EM-034b (acyclicity) is detected at validator time per EM-038 (separate bead); EM-034c is the durable-pin contract (separate code path — `evidence.sub_workflow_pin` reserved-key handling); EM-034a is node-ID namespacing (separate code path — string rewrite under expansion). Test 1 fails (NOT a single code path); they correctly stay separate.
- **EM-015 + EM-015a + EM-015b + EM-015c (run lifecycle emissions).** Four rules: intra-run loops are not new runs (EM-015), `run_started` emission (EM-015a), `run_completed`/`run_failed` emission (EM-015b), terminal-state detection (EM-015c). EM-015 is a non-emission rule about loops; the others are emission rules. Test 1 fails (the loop rule and the emission rules are different surfaces). Correctly separate.
- **EM-044 + EM-045 + EM-046 (rollback transitions).** EM-044 is the shape (transition_kind + rollback_to_state_id); EM-045 is "rollback as new transition, not history-rewrite"; EM-046 is the context-restore special case. Test 1 partial (shared rollback theme but EM-046's context-restore is a DIFFERENT transition kind with different `rollback_to_state_id` constraints); test 2 weak. Correctly NOT coalesced.

No additional missing-coalesce findings beyond F-pilot-EM-5.

## 4. Over-split flags

None at MAJOR severity.

- **EM-018 + EM-018a (transition record + ID generation).** EM-018 is the path-and-content rule; EM-018a is the ID-generation contract. They share the path-scoping rationale but are distinct surfaces (path/content vs. ID generator). Correctly separate.
- **EM-024 + EM-024a (branch-tip durability + monotonicity check).** EM-024 is the at-any-time-tip-is-durable invariant; EM-024a is the persisted-tip-SHA monotonicity check defending it. Two separate surfaces (declarative invariant vs detection mechanism). Correctly separate.
- **EM-046 + EM-046a + EM-046b (context-restore + no-match failure + RETRY re-dispatch).** Three orthogonal failure-edge rules. Correctly separate.

The pilot does not over-split. EM-016's 3-step git sequence and EM-038's 6 validator categories were correctly KEPT as single beads via §2.2 F8b — the right call.

## 5. New v0.6 supporting-cite verification

Per discipline §3.1 step 1 supporting-cite-vs-hard-dep clause (F-pilot-AR-10): the pilot reports 3 supporting-cite reclassifications. Audit each:

| Cite | Pilot rationale | Independent-testability test result | Reviewer call |
|---|---|---|---|
| EM-001 → AR §4.10 (three-artifact separation) | Thematic anchor, not load-bearing | PASS — "On-disk representation is a DOT document" stands without the cite | Sound supporting-cite |
| EM-005a → AR §4.6 (amendment protocol) | Sub-clause about future extensions | PASS (borderline) — the cite attaches to a future-extension forward declaration; current MVH normative content (kind discriminator + payload envelope) doesn't depend on the cite | Sound supporting-cite (defensible; see 2.1 for nuance) |
| EM-040 → AR §4.9 (centralized-controller) | "Centralized-controller is thematic anchor" | FAIL the supporting-cite test under literal reading — "structural violations of the centralized-controller principle" makes "structural violations" depend on the principle anchor; PASS the invariant-as-target test (AR §4.9 houses AR-INV-007) | Result correct (no edge) but rationale should be invariant-as-target (F-pilot-AR-r2-2), not supporting-cite (F-pilot-AR-10). See §2.10. |

**Net.** 2 of 3 reclassifications are clean F-pilot-AR-10 supporting-cite calls; the third (EM-040) reaches the right outcome via the wrong rule. Pilot-patch-lane fix: rewrite the EM-040 rationale. Discipline-patch-lane fix: §3.1 may benefit from precedence guidance when multiple no-edge rules apply.

## 6. Term-use edges across spec boundaries (cross-spec-to-AR audit)

Per protocol task: verify each of the 4 cross-spec edges to AR is justified by actual term-use or inline cite in EM's body.

| Pilot edge | EM source | AR-side ownership | Term-use justified? |
|---|---|---|---|
| `em-011 → ar-001` | "four-axis classification" (EM-011 body line 162) | AR-001 owns the four-axis classification rule | YES — load-bearing term defined by AR-001 |
| `em-011 → ar-005` | "`mechanism \| cognition` tag" (EM-011 body line 162) | AR-005 owns the ZFC mech/cog tag rule | YES — load-bearing term defined by AR-005 |
| `em-schema.node → ar-001` | "axes : AxisTags — four-axis classification per [architecture.md §4.1]" (§6.1 Node, line 639–640) | AR-001 (the four-axis classification rule); AxisTags is the typed surface | YES — schema definition pins to AR-001 |
| `em-schema.transition → ar-032` | "actor_role : String — role name per [architecture.md §4.8]" (§6.1 Transition, line 700) | AR-032 (Seven roles named); AR §4.8 is the section header for role taxonomy | YES — term-use of role-name vocabulary; pinning to AR-032 is sound (the section-anchor cite resolves to the rule that enumerates the 7 roles) |

All 4 cross-spec edges are justified. **However**, the pilot may have *under-emitted* a few cross-spec term-use edges:

### 6.1 Possible missing cross-spec edge: `em-schema.node → ar-005` (ModeTag)

EM §6.1 RECORD Node (line 640): "mode_tag : ModeTag — one of {mechanism, cognition}". `ModeTag` is defined per the spec's §6.1 external-types list (line 610): "AxisTags, ModeTag — defined in [architecture.md §4.1, §4.2]". AR §4.2 is the ZFC section, AR-005 is the ZFC mech/cog tag rule. Per the pilot's own pattern (em-11 fires both ar-001 and ar-005), `em-schema.node` should fire BOTH `ar-001` (AxisTags) AND `ar-005` (ModeTag). Pilot row only emits `ar-001, ar-005` — wait, re-reading the row (`em-schema.node`): tags `ar-001, ar-005`. **Both ARE emitted** at the schema-bead level. PASS. (Initial concern retracted upon careful re-read.)

### 6.2 Possible missing cross-spec edge: `em-046` (context-restore actor_role)

EM-046 body uses the term `actor_role` (with values `daemon` and "role of the verdict-executing subsystem"). Per the schema-bead pattern, `em-schema.transition` already edges to `ar-032` for the role vocabulary. EM-046's row should arguably edge to `ar-032` directly (term-use of "role" vocabulary at the rule level, not just the schema level). Pilot does not emit this edge. **However**, EM-046 also edges to `em-schema.transition` indirectly via its own predecessors (`em-023, em-044`); transitively, the role-vocabulary dep is reachable. Borderline.

- **Severity.** MINOR (transitive coverage exists).
- **Lane.** `local` (the discipline rule is clear; the pilot omitted a defensible-but-uncertain edge).

## 7. Consolidated findings

### BLOCKER (description doesn't match spec — implementation would diverge)

None.

### MAJOR (coalesce/split decision wrong, or sensor predecessor list materially incomplete)

| # | Finding | Sample | Severity | Lane |
|---|---|---|---|---|
| 1 | `em-inv-005` predecessor list omits term-use edges to `em-schema.checkpoint-trailers` and `em-014` (the trailer registry the invariant predicates over and the bead-run linkage that grounds Harmonik-Bead-ID semantics). The "empty predecessor list" the pilot self-flagged as F-pilot-EM-7 is real, but the right fix is to apply §3.1 step 5 term-use to invariant bodies, not to invent a sensor-degeneracy clause. | em-inv-005 | MAJOR | `class` (discipline silent on whether sensor beads emit term-use edges from invariant body terms) |

### MINOR (cosmetic / preference / borderline)

| # | Finding | Sample | Severity | Lane |
|---|---|---|---|---|
| 2 | `em-005`'s row note understates the load-bearing role of "amendment protocol" in EM-005a's future-extension forward declaration. Reclassification call is defensible, but the rationale should explicitly note that the cite attaches to a forward-extension statement that doesn't affect MVH-level testability. | em-005 | MINOR | `class` (forward-extension cite pattern not addressed by F-pilot-AR-10 worked examples) |
| 3 | `em-inv-001` predecessor list omits term-use edges to `em-016`/`em-017` (the git-checkpoint-trail concept the sensor predicates over). Transitive coverage exists via §10.2 group-cite-derived predecessors EM-031..EM-033 → EM-016/EM-017, so readiness ordering is preserved. | em-inv-001 | MINOR | `class` (same root cause as MAJOR finding 1; folded into that fix) |
| 4 | `em-040`'s row note attributes the no-edge result to F-pilot-AR-10 supporting-cite when F-pilot-AR-r2-2 invariant-as-target is the rule that genuinely fires (AR §4.9 houses AR-INV-007 centralized-controller). Outcome correct; rationale mis-prioritized. | em-040 | MINOR | `class` (rule-precedence between F-pilot-AR-10 and F-pilot-AR-r2-2 not addressed) |
| 5 | `em-046` could emit a direct `ar-032` term-use edge for "actor_role" vocabulary; transitive coverage via `em-schema.transition → ar-032` exists. | em-046 | MINOR | `local` |

### Class-lane class findings already self-flagged by the pilot (validated)

- **F-pilot-EM-1** (§2.2 F8b worked example for behavioural specs): VALIDATED. EM-016 and EM-038 are textbook F8b applications; the discipline would benefit from a worked example beyond BI-031 step 4's lettered sub-cases. `class`.
- **F-pilot-EM-5** (typed-ID-alias coalesce judgment): VALIDATED. The 5 thin UUID/String aliases are peer rules without an anchor structure; strict §2.3 says no coalesce, but the pattern will recur in EV / RC and discipline should provide explicit guidance. `class`.
- **F-pilot-EM-6** (dual-ownership trailer rows): VALIDATED. Structurally analogous to §2.11(d) co-owned event payloads but for trailer rows; the discipline doesn't enumerate this case. `class`.
- **F-pilot-EM-7** (em-inv-005 empty predecessor list): RE-FRAMED. The right fix is applying §3.1 step 5 term-use to invariant bodies (MAJOR finding 1 above), not a sensor-degeneracy clause. The discipline gap is in §2.5 / §3.1 step 5 interaction, not in §2.5 alone.

### Class-lane findings already self-flagged that are LOCAL upon reviewer audit

- **F-pilot-EM-4** (three intra-EM bidirectional cite resolutions): VALIDATED as `local`. The em-002↔em-041 (Edge ↔ cascade), em-018↔em-019 (sibling-file path ↔ git-show retrieval), and em-020↔em-020a (immutability ↔ audit tool) resolutions are textbook v0.6 applications: F13 slot-rule heuristic for the first two, F-pilot-AR-10 supporting-cite for the third. Pilot author applied the rules correctly.

## 8. Summary

- **0 BLOCKER** findings.
- **1 MAJOR** finding — `class` (em-inv-005 missing term-use edges from invariant body to schema/impl).
- **4 MINOR** findings — 3 `class`, 1 `local`.
- **5 of 5 reviewer findings** carry `class` co-tags; bias toward over-flagging per protocol §4.1 routes them all to the discipline lane unless overruled in synthesis.
- **All 4 §2.3 coalesces sound** (em-005, em-023, em-041, em-043).
- **F8b shared-function-body tiebreaker correctly applied** to em-016 (3-step atomic git) and em-038 (6-category validator). Zero-multi-step-splits decision is mechanically defensible.
- **All 4 cross-spec edges to AR justified** by term-use or inline cite in EM body.
- **Supporting-cite reclassifications:** 2 of 3 cleanly justified (EM-001, EM-005a); 1 (EM-040) reaches the right no-edge outcome via the wrong rule.
- **Pilot's seven self-flagged §8 findings:** 4 validated as accurately captured (F-pilot-EM-1/2/5/6); 1 re-framed (F-pilot-EM-7 → MAJOR finding 1 above); 2 unchallenged by this reviewer (F-pilot-EM-2 forward-cite, F-pilot-EM-3 missing §4.a envelope) — these are coverage / spec-author concerns, not decomposition-quality concerns, and outside this reviewer's remit.

Per protocol §4.1 probe rules: every finding above carries a `class` self-tag that routes to the discipline-patch lane unless explicitly overruled in synthesis.md. The pilot is structurally sound; the discipline patches needed are documentation-tightening (rule precedence, invariant-body term-use clarification) rather than structural rewrites.
