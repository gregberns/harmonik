# Integration-pass review — codex-harness

Reviewer: independent sub-agent. **Verdict: APPROVE** — no BLOCKING issues; 2 MINOR, resolved
in-place. All paths/anchors cross-checked against the component specs and SPEC.md.

## Checks — all PASS
1. **Normative backing:** every SPEC claim traces to a component spec (N1→C3 B1; N2→C2 R2.7+C5;
   N3→C2 R2.2; N4→C2 R2.5; N5→C1 R1.4+C5; B1–B4→C3 R3.1–R3.4; B5→C3 R3.5/C6; selection→C4
   R4.1–R4.4). No orphan claims.
2. **Landing order:** C1→C4→C2→C3→C5→C6 is a valid topological sort of the DAG; each step is
   claude-safe and independently mergeable.
3. **Seam-point consistency:** the completion-mode bypass targets `dot_cascade.go:643` (the
   `go pasteInjectQuitOnCommit(...)` launch) consistently across SPEC N2, C5, and 06-integration;
   C5 correctly notes `workloop.go:2265` holds only the bare `sess.Wait`. launchSpecBuilder/registry
   seam consistent across SPEC N5, C1 R1.4, C4, C5.
4. **N-1 back-compat:** default resolves to claude at every tier; all additions additive;
   `HandlerBinary` preserved; proven by C1 AC1.2 golden + C6 R6.1 regression.
5. **No contradictions** between SPEC.md and the component specs.

## Findings + resolutions
- **[MINOR]** 03-components.md §C4 Responsibility prose omitted the per-node tier (showed 3 of 4
  tiers). **Resolved:** prose now reads "per-bead → per-queue → per-node DOT attr → global".
- **[MINOR]** SPEC §3 didn't surface the fail-closed conflict rule (duplicate selectors at one tier)
  that C4's error-handling specifies. **Resolved:** SPEC §3 now states duplicate/conflicting
  selectors at a tier → error (fail closed), making it normative rather than buried in a component.

## Outcome
APPROVE; both minor findings resolved. The DAG, seam points, and back-compat story are airtight.
Advancing to tasks.
