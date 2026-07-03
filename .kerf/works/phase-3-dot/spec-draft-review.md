# Pass-5 Spec-Draft Review — phase-3-dot

**Reviewed:** 2026-05-23
**Round:** 1
**Verdict:** APPROVE

## Per-component scorecard

| Component | Drift from design | Style consistent | IDs assigned | Reqs covered | Notes |
|-----------|-------------------|------------------|--------------|--------------|-------|
| C1 workflow-graph.md (NEW) | ✓ none (faithfully renders §4.1–§4.7 catalog + cascade + dialect + failure-class routing + schema-version contract per design §2) | ✓ headers/structure mirror execution-model.md (Purpose / Scope / Glossary / Normative reqs / Validation / Open questions / Cross-references); "Tags: mechanism" footers match existing convention | WG-001..WG-038 (38 reqs) | All G1/G2/G5/G6 gaps addressed; D-attractor-adoption, D-edge-cascade-invariant, D-verdict-surfacing, D1–D5, D7–D12 all traced in §16.1 diff table | OQ-WG-001 properly retained as resolved history marker |
| C2 execution-model.md §7.5 + EM-007 + §10.1 | ✓ none; five sub-parts (input contract / dispatch-equivalence / validator / dispatch table / conformance lift) match design exactly | ✓ EM-NNN headings, axes footers, §10.1 lift prose mirror existing §10.1 shape | EM-055..EM-061 (range starts at correct EM-055 after pre-existing EM-054 max) | All design Δ-edits covered: §4.2 EM-007 amendment, §7.5 binding, §10.1 lift gating clause | §7.5.3 item 7 explicitly requires `handler_ref` on BOTH `gate` AND `non-agentic` per EM-007 amendment ✓ |
| C3 handler-contract.md §4.2a + §5.6 + EM-005 v2 | ✓ none; per-node-type Outcome table + failure_class field + gate_decision kind + context-key discipline all match design | ✓ HC-NNN headers + "Tags: mechanism" + per-row testability notes consistent with HC-006a precedent | HC-058..HC-062; EM-005b, EM-005c (lettered-suffix pattern follows EM-005a precedent) | All EDITs A/B/C/E present; EDIT D properly retracted | Schema bump v0.3.3→v0.3.4 consistent across EM-005b and EM-005c |
| C4 control-points.md §4.12/§4.13/CP-038a | ✓ none; node-type binding + GateDecisionPayload + mechanism-Gate drift envelope match design | ✓ CP-NNN headers + axes footers + §6.5 event additions + §12 revision-history entry consistent with existing pattern | CP-053..CP-058 + CP-038a (correctly extends past pre-existing CP-052 max) | All four design pillars covered; OQ-CP-006 surfaced | CP-058 GateDecisionPayload owns the record; C3 cites it ✓ |
| C5 review-loop.dot + README | ✓ none; renders EM-015d/EM-015e topology in DOT; README matches design §3 layered-testing plan | ✓ DOT comments cite spec anchors; README mirrors specs/ prose style | N/A (artifact, not requirements) | Five scenario classes named for layer-2 (APPROVE, two-round-retry, BLOCK, cap-hit, no-progress) | All node IDs use `close-needs-attention` (hyphen); no `<`/`>` in conditions; 6th unconditional edge present at line 105 ✓ |

## Cross-component sweep verification

- **Patch 1** (gate handler_ref): VERIFIED at C1 WG-005 lines 113–115 ("MUST carry **both** ... `gate_ref` ... AND ... `handler_ref`"); WG-024 line 317 ("`handler_ref` is REQUIRED on `gate` and `non-agentic` nodes per the EM-007 amendment"); OQ-WG-001 marked RESOLVED at §13. C2 §7.5.3 item 7 + §7.5.4 dispatch table + EM-007-amended both consistent. C4 CP-054 likewise pairs `gate_ref` AND `handler_ref`. ✓
- **Patch 2** (GateDecisionPayload to C4): VERIFIED at C3 §4 EDIT D (line 199 — "DEFERRED to C4 §6.1.8 CP-058 ... EDIT D in this draft is intentionally empty"); HC-060 cites `[control-points.md §6.1.8 CP-058]`; EM-005b cites same location; C4 §6.1.8 declares the RECORD canonically. No double-declaration. ✓
- **Patch 3** (`context_keys` reserved): VERIFIED at C1 §10 WG-031 line 378 reserved-set list explicitly includes `context_keys` with cross-ref to HC-062. HC-062 (C3 EDIT E) declares the DOT graph-level attribute and cites C1 for the registry. ✓

## Findings

### F1 — `agent_type` catalog anchor TBD (severity: NIT) — C1
WG-003 (line 99) explicitly defers the cross-reference target to pass-6 via footnote `[^agent-type-tbd]`. This is properly surfaced as OQ-WG-009; no fix required at pass-5. Pass-6 integration must resolve the anchor.

### F2 — `policy_ref` deprecation potential cross-impact (severity: MINOR) — C4
CP-056 deprecates `policy_ref` and rejects it with `ErrDeterministic` at workflow-ingest. C1 WG-002's attribute table still lists `policy_ref` as a legal optional attribute on multiple node types (lines 84–87), and §10 WG-031 reserved-set includes `policy_ref`. These are not contradictory — C1's reserved set tracks the closed name-set the loader must recognize (including for rejection), and C4's CP-056 commits the rejection semantics. But pass-6 integration must reconcile the wording so C1's table makes the deprecation explicit. Not blocking; flagged for pass-6.

### F3 — EM-005a payload-type widening citation (severity: NIT) — C3
The existing EM-005a `RECORD Outcome` row reads `payload : VerdictPayload | None`. EM-005b's pass-6 transcription will need to widen that type to a union admitting `GateDecisionPayload`. C3 §2.3 amends the EM-005 field-list but does NOT spell out the EM-005a type-widening. Pass-6 transcription should add a one-line patch to EM-005a's `payload` row.

### F4 — Test-bead obligation per jig pass-5 (severity: NOT BLOCKING per jig) — all components
Per jig pass-5 acceptance, 2 test beads per substantially-changed area (scenario + exploratory) are required as a **pre-condition for advancing to integration**, not a review criterion for pass-5 approval. The changelog §"Required pass-6 follow-up" (line 64–66) acknowledges this — 10 beads (5 components × 2 types) to file at pass-6 entry. APPROVE-able as-is.

## Changelog completeness check

`05-changelog.md` accounts for: every draft file (with line counts §"File line counts"); every D-decision traced (§"D-decisions traced"); every cross-component patch resolved (§"Cross-component sweep"); every open question carried forward (§"Open questions surfaced at pass-5"). Traceability to design layer documented in §"Files affected" table. The pass-6 follow-up obligation (10 test beads) is explicit. **Changelog is complete.**

## Recommended action

**APPROVE → advance to integration (pass-6).**

0 BLOCKERs. 0 MAJORs. 3 MINOR/NIT findings (F1, F2, F3) are pass-6-integration concerns, not pass-5 blockers. F4 is a jig pre-condition for pass-6 entry, not a pass-5 review criterion.

At pass-6 entry:
1. File the 10 test beads (5 components × {scenario, exploratory}).
2. Resolve F1 (`agent_type` catalog anchor).
3. Resolve F2 (`policy_ref` deprecation in C1's attribute table).
4. Apply F3 (EM-005a `payload` type union widening).
5. Apply C3 §6 cross-reference catalogue additions.
