# 06 — Integration

No cross-component contradictions: T1/T2 share no files (different test files, different packages read-only
via existing seams). T3 is additive (new shell-driven test files) and only depends on T1/T2 having proven
their primitives — no shared state at build time. T4 is a doc+spec deliverable, not code, so it cannot
conflict with T1–T3's Go test files.

**Initialization order:** T1 and T2 build in any order (fully independent worktree beads). T3 is dispatched
only after T1+T2 close (avoids wasting the L2 serial slot on primitives not yet proven). T4's presence-matrix
and B1-pin evidence come from specific T1/T2 beads, so it dispatches after those two land (not necessarily
after all of T1/T2).

**Shared state:** all four tranches write to the epic branch `integration/comms-test` in isolated worktrees;
the daemon commit-gate (build+vet+test) is the only shared merge choke point, one bead lands at a time.

**Error handling across components:** none of the components change runtime error handling — they add test
coverage over already-decided behavior (per the non-goal in `01-problem-space.md`).

SPEC.md assembles `05-specs/*.md` verbatim by tranche; no new requirements introduced at this pass.
