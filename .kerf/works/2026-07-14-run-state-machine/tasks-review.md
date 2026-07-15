# Tasks review — self-review (signoffs waived, 2026-07-14)

**Verdict: APPROVE — advance to ready.**

- Every RX requirement + invariant is owned by ≥1 task (spot-map: RX-001/005→RT5;
  RX-002/012/014→RT2/RT3; RX-006/008→RT6/RT9; RX-009..011→RT5/RT7/RT8 via the
  transitional shell; RX-016→RT4; RX-017→RT1; RX-018/019→RT7/RT12 audit;
  RX-INV-003→RT8/RT10/RT11; RX-INV-005→RT3).
- Graph is acyclic; the single-writer daemon chain (RT1→RT3→RT4→RT7→RT8/RT9) is
  explicit; the three DISJOINT lanes (RT2; RT5/6; RT10) maximize the safe
  parallelism without violating the census single-writer constraint.
- External gates (M1-1, M1-5) stated at the top rather than duplicated per-task;
  the M2 edge is contract-satisfied (no false block).
- Each acceptance is out-of-band and mechanically checkable; parity acceptance
  is concrete (goldens byte-equal except the RX-019 allowlist; escapedetect
  unmodified; thin-driver metrics recorded).
- Risk noted for the implementer: RT7 is the largest diff (the P8→P18 rewire);
  its "each increment green" discipline is the census risk mitigation — the
  task text says so.
