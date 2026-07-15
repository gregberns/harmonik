# Spec-draft review — `run-state-machine` (self-review, signoffs waived, 2026-07-14)

**Verdict: APPROVE — advance to integration.**

- Requirement coverage: every M3-D pin maps to ≥1 RX requirement (D1→RX-001/002,
  D2→RX-005/006, D3→RX-007, D4→RX-017, D5→RX-012..015, D6→RX-016, D7→RX-010 +
  RX-INV-002/003, D8→RX-008, D9→RX-006/020, D10→RX-018, D11→RX-009..011,
  D12→RX-019, D14→§6). No requirement lacks a design anchor.
- Numbering: RX-001..RX-020 contiguous; RX-INV-001..005; prefix RX verified free
  in `_registry.yaml`.
- The one wording-supersession (RX-015 vs problem-space §2 "push outside the
  lock") is explicit IN the requirement text and in 05-changelog — not silent.
- What-not-how check: RX-016 pins the boundary without freezing the field list
  (bundle composition stays design-owned) — deliberate; reviewer may prefer the
  full port list normative. Kept loose so M5's re-cut doesn't force a spec rev.
- Dependencies: depends-on replay-substrate only (real requirement reuse);
  session-keeper/event-model/execution-model/process-lifecycle informative —
  matches how the draft cites them.
