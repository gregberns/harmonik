# Pass-6 Integration Review

**Reviewer:** general-purpose sub-agent, fresh context, round 1.
**Date:** 2026-05-23.
**Artifact reviewed:** `06-integration.md`.

## Verdict

```json
{
  "schema_version": 1,
  "verdict": "APPROVE",
  "flags": ["minor-coverage-gap-em-nodetype-enum"],
  "notes": "Spot-checks confirm all four sampled contradictions (C5 node_type vs C1 closed enum; workflow_ref vs sub_workflow_ref; lowercase vs uppercase verdict enum; BI-005 absent workflow_ref field) plus the WG-T03 phantom citation. Cross-reference table sampling held up; CI-1..CI-7 are substantive (none are pedantic) and well-scoped. One small omission worth noting in pass-7: the existing EM §4.2 'ENUM NodeType' block still lists 'control-point' as a member alongside the four, which C1 WG-001 amends but the EM block itself needs an in-place edit — fold into the pass-7 C1/EM reconciliation task. Otherwise the BLOCKER + 5 SHOULD-FIX + 6 NIT triage is correct and carryable into pass-7."
}
```

## Reviewer notes (preamble to JSON)

The spot-checks confirm the integration analysis's key findings:

- **BLOCKER (C1 vs C5):** C1 §4 WG-001 locks `type` to `{agentic, non-agentic, gate, sub-workflow}` and uses attribute name `type`. C5 uses `node_type` with values `entry`/`agentic`/`terminal`. Real contradiction. ✓
- **sub_workflow_ref vs workflow_ref:** EM line 949 declares `sub_workflow_ref`; C1 WG-006 uses `workflow_ref`. Real. ✓
- **Verdict labels lowercase:** C5 uses `'approved'`, `'changes_requested'`, `'blocked'`; EM-015d uses uppercase `{APPROVE, REQUEST_CHANGES, BLOCK}`. Real. ✓
- **BI-005 lacks workflow_ref:** Confirmed — BI-005 only owns `title`, `description`, `type`. ✓
- **C1 WG-031 reserved set:** Includes `workflow_ref` (not `sub_workflow_ref`), `policy_ref` still reserved, `context_keys` listed and explicitly cross-references HC-062 (so CI-3's "asymmetry" is partially mitigated — WG-031 already binds the attribute, leaving only the OQ-WG-002 declaration-mechanism wording open). Minor nuance but the analysis flagged it correctly.
- **WG-T03 in C5 README:** Confirmed — C1 uses `WG-021`/`WG-022`/`WG-023`. ✓

**Coverage check:** The analysis examined all 16 specs in `specs/` per its table, including `reconciliation/`, `workspace-model.md`, `operator-nfr.md`, `architecture.md`, `scenario-harness.md`. It did not separately walk `event-model.md §6.3` (flagged as deferrable in the cross-ref table) and did not deeply walk `process-lifecycle.md` or `queue-model.md` — both reasonable omissions since the drafts don't touch those surfaces directly. The CI-7 `start_node` / `start_node_id` inconsistency is real and well-caught.

**One additional finding (reviewer-only):** The pre-existing EM `ENUM NodeType` (line ~953) still lists `control-point` as a member alongside the four. C1 WG-001 explicitly addresses this ("control-point value … is NOT a node type in `workflow_mode=dot`") so it's resolved by the C1 amendment — but the EM `ENUM NodeType` block itself would need a pass-6 edit to drop `control-point`. The integration analysis does not flag this. Minor — could be added as a SHOULD-FIX or rolled into the C1+EM reconciliation, but doesn't change the verdict.

**Assessment:** Analysis is accurate, well-cited, and the BLOCKER/SHOULD-FIX/NIT triage is defensible. The BLOCKER is correctly characterized — the canonical witness example must round-trip the C1 validator. Per the reviewer brief explicitly, BLOCKER/SHOULD-FIX items are acceptable to carry into pass-7 as the first tasks. The recommendation is actionable and properly sequenced.
