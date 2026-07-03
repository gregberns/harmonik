# Tasks Review (autonomous, round 1)

Reviewer lens on `07-tasks.md` against `SPEC.md` + all component specs.

## Checklist
- **Every SPEC.md section covered by >=1 task.** PASS — coverage map in 07-tasks §Coverage.
- **Every acceptance criterion appears in a task.** PASS — S1 acceptance (round-trip + scenario
  test + README anchor) is the body of every W-* bead; S3 smoke is W-DUAL.
- **Dependencies correct + DAG.** PASS — W-DUAL→{W-TRIPLE,W-CONSENSUS,W-D1}; capability beads→
  {SOON, W-D2}. Capability beads (hk-l8rpd/sdnzj/69asi/karlz) are pre-existing OPEN beads, so the
  dep edges point at real ids. No cycles.
- **Tasks appropriately sized.** PASS — one workflow = one bead = one fixture+test+README-subsection;
  a coherent single-commit (or tight-series) unit.
- **Integration tasks exist + ordered.** PASS — W-D1/W-D2 are the integration (composition) beads;
  ordered after their composed NOW/SOON sets via dep edges.
- **>=1 scenario-test bead + >=1 exploratory-test bead.** PASS — every W-* is a scenario-test bead;
  W-DUAL is the exploratory/live-smoke bead.

## Findings
- NIT: priorities follow the brief exactly (NOW=P1, DEMO=P1, SOON=P2). Recorded.
- NIT: the brief says "one bead per workflow"; 21 workflows + 1 epic = 22 beads proposed. The
  DEFERRED `parallel-review-consolidate` correctly gets NO bead. Consistent.

## Verdict
APPROVE. No unresolved findings. Advance to ready.
