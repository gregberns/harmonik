# Pass-5 Spec Draft — Changelog

> Pass-5 of kerf work `phase-3-dot`. Five component drafts written; one new spec file + extensions to three existing spec files + one new directory. Cross-component sweep applied before reviewer dispatch resolved three contradictions inline (C1↔C2/C4 EM-007 amendment alignment, C3↔C4 GateDecisionPayload double-declaration, C1 reserved-attribute set missing `context_keys`).

## Files affected

| Spec file (target) | Change kind | Draft source | Design source |
|---|---|---|---|
| `specs/workflow-graph.md` (NEW) | New normative spec, ~600 lines | `05-spec-drafts/C1-workflow-graph.md` | `04-design/C1-workflow-graph-design.md` |
| `specs/execution-model.md` §7.5 + EM-007 + §10.1 | Extension: new §7.5 binding, in-place EM-007 amendment, in-place §10.1 conformance lift | `05-spec-drafts/C2-execution-model-dot.md` | `04-design/C2-execution-model-dot-design.md` |
| `specs/handler-contract.md` §4.2a + §5.6 | Extension: per-node-type Outcome surface table + context-update discipline | `05-spec-drafts/C3-handler-contract-outcome.md` (EDIT A, E) | `04-design/C3-handler-contract-outcome-design.md` |
| `specs/execution-model.md` EM-005 (v0.3.3 → v0.3.4 additive bump) + EM-005a enum extension + new EM-005b/c | Additive amendments to existing Outcome record | `05-spec-drafts/C3-handler-contract-outcome.md` (EDIT B, C) | `04-design/C3-handler-contract-outcome-design.md` |
| `specs/control-points.md` §4.12 + §4.13 + §6.1.8 + §4.7 (CP-038a) + §6.5 | Extension: workflow-graph binding + GateDecisionPayload + mechanism-Gate schema-drift envelope | `05-spec-drafts/C4-control-points-binding.md` | `04-design/control-point-node-type-design.md` |
| `specs/examples/` (NEW directory) + `specs/examples/review-loop.dot` + `specs/examples/README.md` | New directory + canonical normative example | `05-spec-drafts/C5-review-loop.dot` + `C5-examples-README.md` | `04-design/C5-examples-design.md` |

## Requirement-IDs assigned

| Spec | New IDs | Range |
|---|---|---|
| `workflow-graph.md` | 38 new requirements | WG-001 … WG-038 |
| `execution-model.md` | 7 new requirements | EM-055 … EM-061 (+ in-place EM-007 amendment, in-place §10.1 lift) |
| `execution-model.md` (Outcome) | 2 new sub-requirements | EM-005b, EM-005c (+ EM-005a enum extension) |
| `handler-contract.md` | 5 new requirements | HC-058 … HC-062 |
| `control-points.md` | 6 new requirements + 1 sub-amendment | CP-053 … CP-058 + CP-038a |

## D-decisions traced (closed at pass-3/-4, applied at pass-5)

| D | Position | Applied in |
|---|---|---|
| D-attractor-adoption | Adopt Attractor outcome+context model verbatim; deviate only with reason | All 5 drafts |
| D-edge-cascade-invariant | 5-step cascade + unconditional-edge-fallback invariant | C1 §5 WG-009/-012, C2 §7.5.2, C5 edge 6 |
| D-verdict-surfacing | `preferred_label` for verdict | C1 §7, C3 EDIT A, C5 conditions |
| D1 | `failure_class` IS a legal LHS | C1 §6 WG-013, C3 EDIT B |
| D2 | `failure_class` top-level field on FAIL Outcome | C3 EDIT B (EM-005c), C1 §7 |
| D3 | Drop control-point from node-type catalog; gate-node = handler-evaluating-Gate-CP | C1 §4 (4-type catalog), C4 §4.12 CP-053 |
| D4 | Edge-condition LHS whitelist (Outcome fields + whitelisted context keys) | C1 §6 WG-014, C5 edge conditions |
| D5 | Edge condition dialect: equality + `&&` only; no `<`/`>` | C1 §6 WG-013/-016, C5 (all edges) |
| D7 | `kind = gate_decision` payload extension | C3 EDIT C (EM-005b), C4 §6.1.8 CP-058 |
| D8 | Per-workflow registered context-key list; warn-and-drop unregistered | C3 EDIT E HC-062, C1 reserved-attr set |
| D9 | Mixed unknown-attribute policy (strict for enums + reserved; permissive elsewhere with warning + AST retention) | C1 §10 WG-031/-032 |
| D10 | `schema_version` graph-level only (not per-node) | C1 §11 WG-033/-035 |
| D11 | `specs/examples/` for canonical; project-local out-of-scope at v1 | C1 §12 WG-038, C5 README |
| D12 | Distinct terminal node IDs (`close`, `close-needs-attention`) | C1 §8 WG-021/-023, C5 (both terminals) |

## Cross-component sweep — contradictions resolved at pass-5 (post-draft, pre-reviewer)

| # | Contradiction | Authoritative source | Patched in |
|---|---|---|---|
| 1 | C1 OQ-WG-001 picked option (b) "preserve EM-007 unchanged"; but C2 EM-007 amendment + C4 CP-054 require `handler_ref` on `gate` | C2/C4 (design layer locked option (a)) | C1 WG-005, WG-024, OQ-WG-001 (resolved) |
| 2 | C3 EDIT D declared GateDecisionPayload at §6.1.1 CP-062; C4 §6.1.8 already declared it at CP-058 | C4 (took ownership per OQ-C3-1 deferral) | C3 EDIT D (retracted, deferred to C4); all C3 internal refs updated to §6.1.8 CP-058 |
| 3 | C3 EDIT E referenced graph-level `context_keys` DOT attribute; C1 reserved-attribute set did not list it | C3 (added the attribute) | C1 §10 WG-031 reserved set |

## Open questions surfaced at pass-5 (for pass-6 integration or follow-up beads)

| OQ | Owner | Status |
|---|---|---|
| OQ-WG-001 | C1 | RESOLVED at pass-5 — see contradiction #1 above. Retained as history marker. |
| OQ-WG-002 … OQ-WG-009 | C1 | Carried to pass-6 |
| OQ-EM-016 | C2/C3 | Carried; pass-6 confirms `Outcome.failure_class` (D2) and `run_failed.failure_class` event field agree |
| OQ-HC-013 … OQ-HC-015 | C3 | Carried |
| OQ-CP-006, OQ-CP-007 | C4 | Carried; CP-038a envelope-hash type-sharing |
| (C5 `terminal_node_ids` syntax) | C5 | Pinned as semicolon-delimited string at v1; C1 owns the canonical encoding decision |

## Required pass-6 follow-up

Per the jig pass-5 acceptance: every substantially-changed spec area needs **(1) scenario-test bead** + **(2) exploratory-test bead** filed before advancing to integration. These are NOT closed by this pass-5 draft; pass-6 integration begins with filing those beads (10 beads total — 5 components × 2 test types).

## File line counts (drafts)

| File | Lines |
|---|---|
| `C1-workflow-graph.md` | 594 |
| `C2-execution-model-dot.md` | 183 |
| `C3-handler-contract-outcome.md` | 300 |
| `C4-control-points-binding.md` | 172 |
| `C5-examples-README.md` | 59 |
| `C5-review-loop.dot` | 106 |
| **Total** | **1414** |
