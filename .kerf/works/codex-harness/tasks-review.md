# Tasks-pass review — codex-harness

Reviewer: independent sub-agent (cross-checked the filed beads via `br show`/`br dep list`/`br dep
tree`). **Verdict: APPROVE.** 2 MINOR notes, resolved/noted below.

## Checks — all PASS
- **Requirement coverage (C1–C6 / R1.1–R6.5):** every requirement maps to ≥1 task bead. C1→T1/T2/T3;
  C2→T7/T8/T9 (R2.3 Retask carried by T8 adapter + T12 reviewer-iter routing); C3→T10/T11 (R3.5
  MUST-TEST in T17); C4→T4/T5/T6; C5→T12/T13/T14 (R5.3 verdict-reuse via T12+T14); C6→T15/T16/T17/T18
  + scenario/exploratory.
- **DAG:** T1 sole root; T8←{T7,T3}; T12←{T3,T4,T8}; T14←{T12,T5}; T17←{T11,T14}; scenario
  hk-vfmn9←{T13,T15}; exploratory hk-qxfj0←{T4,T5} — all confirmed against the filed beads. Acyclic
  (`br dep cycles`: none). Valid topological sort matching 06-integration's 6 landing steps.
- **Test beads:** exactly one scenario (hk-vfmn9) + one exploratory (hk-qxfj0); neither is a root;
  both depend on the features they exercise. No orphans (T15 twin chains into hk-vfmn9).

## Findings
- **[MINOR] R6.5 / R3.5 MUST-TEST traceability.** The two empirical MUST-TEST items (codex `exec`
  env-precedence honored; codex reviewer reliably writes the structured verdict) are absorbed into
  T17's docs/checklist bead rather than distinct validation beads. Acceptable for a docs-checklist
  model. **Resolved:** added an explicit MUST-TEST enumeration to T17's scope in 07-tasks.md so the
  two checks aren't lost as prose; T17's acceptance criteria will enumerate both at impl time.
- **[MINOR] ASCII diagram cosmetic.** The 07-tasks.md execution-order diagram renders T17's edges
  slightly ambiguously vs. the table; the **table + filed bead deps are authoritative** (and correct).
  Noted; left as-is (cosmetic).

## Outcome
APPROVE. The corpus is ready to dispatch; T1 (hk-e8omz) is the sole start-ready bead. Advancing to
`ready`.
