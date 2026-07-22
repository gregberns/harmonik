# Spec-Draft Review — `agent-input-substrate` (M2)

> Round 1, INDEPENDENT reviewer (fresh-context sub-agent, 2026-07-14). The most critical review —
> drafted text becomes normative at finalization. Inputs: all `05-spec-drafts/*`, the `04-design/*`
> designs, and `diff` of each modified draft against its repo spec on `phase1-session-restart-substrate`.

## Verdict: APPROVE (no required fixes)

All 10 DoD criteria PASS + all 3 targeted confirmations.

## DoD criteria
1. One draft per target file — PASS (agent-input.md new + 3 full modified + _registry.yaml + changelog).
2. Full-file (not diff) drafts — PASS (diffs show only insertions against a complete base).
3. New-spec conventions — PASS (front-matter v1.1; full 12-section RS/SK layout; AIS-00x style; AIS-INV-001/002; revision history).
4. Every target state reflected — PASS (AIS-001…017 + INV-001/002 ↔ design §4; HC/PL/SK amendments ↔ siblings).
5. No gold-plating — PASS.
6. No accidental removal/alteration — PASS (see diffs).
7. Normative text — PASS (MUST/MUST-NOT; rationale isolated in PLANNER-RECONCILE/notes).
8. Cross-refs / ID collisions — PASS (see ID findings).
9. Changelog traceability — PASS.
10. Formatting — PASS.

## Diff findings (only intended sections changed; no accidental deletions)
- handler-contract.md: 0.5.4→0.7.0; new §4.1a (HC-069/070/071); HC-INV-008; front-stop clauses on
  HC-056/057; HC-054 observation-peer line; §6.1 (no-op SendInput removed, InputPort/InputRequest/Ack
  added); §6.4 event registration; §9.3 AIS co-ref; §10.1/§10.2; history row. Nothing else touched.
- process-lifecycle.md: 0.5.4→0.5.5; PL-021b "Observation-only after AIS" subclause; PL-021d retitled
  + demotion clause + C6 deletion-boundary note + edited rationale; §9.3 co-ref; history row. Items
  1–8 spawn/kill verbs untouched.
- session-keeper.md: 0.1.0→0.2.0; SK-002 carve-out NOTE; new §4.10/SK-021; §9.1 PL-021d clause; §11
  deferred note (2→3); history row. SK-002 interface block untouched; no SK renumbering.

## ID-collision / cross-reference findings
- HC-069/070/071 + HC-INV-008 genuinely NEW: repo tops at HC-068 (§4.2a consumed HC-058–068); each
  defined exactly once. The reconciliation away from the stale HC-058/059/060 is correct.
- agent-input.md cross-refs point to HC-069/070/071; the sole "HC-058" hit is inside the explanatory
  ID-FREEZE note, not a live cross-ref.
- SK-021 new (repo max SK-020), defined once. AIS prefix reserved in _registry.yaml.

## Targeted confirmations
- HC version 0.7.0 non-duplicate (0.6.0 already exists 2026-05-30; 0.7.0 nowhere in history).
- SK-021 genuinely new/unused.
- PL-021d load-buffer/paste-buffer/send-keys MUST sentences preserved BYTE-IDENTICAL (repo 776/784/795
  == draft 782/790/801).

## Advance
Criteria met → status advances `spec-draft → integration`.
