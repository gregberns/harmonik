# core-loop-proof — known-RED cells

A **known-RED** cell is an assertion that is EXPECTED to fail today against a tracked
daemon defect. It is recorded here and self-tested as an expected-fail — it is NOT a
false-green, and it is deliberately excluded from T9's full-matrix green gate (the gate
is "full-matrix green **minus** the known-RED cells").

When the underlying defect lands, the assertion flips to `pass`, its expected-fail
self-test row breaks loudly, and that break is the signal to retire the entry here.

## (none — all known-RED cells retired)

There are currently no known-RED cells. The full-matrix green gate is therefore the plain
"full-matrix green" with no exclusions.

### Retired

- **t10 — per-bead integration-branch targeting (hk-lgykq).** RETIRED 2026-07-07. The
  defect (the `landTaskBranch` / per-bead `lands_on` path was dead code, so every run
  landed on the daemon-wide default target instead of the bead's intended integration
  branch) was fixed by wiring the resolved `baseBranch` as the merge target into all five
  merge call sites (`internal/daemon/workloop.go`, commit `ca23da59`). Proven live by the
  daemon E2E test `TestMergeToMain_PerBeadIntegrationTargetLandsOnBranch` — a run lands on
  its intended branch with `main` byte-pinned; RED before the fix, GREEN after.

  Re-specced 2026-07-10: `assert_t10` now proves the core-loop landing contract directly —
  it reds unless a run reaches the APPROVE success terminal (`run_completed` with
  `success==true`, summary not `needs-attention`) AND lands a commit
  (`implementer_phase_complete.commit_landed==true`). Golden coverage is the three fixtures
  `t10-success` (green), `t10-needs-attention` (red — wrong terminal), and
  `t10-approve-nocommit` (red — no landed commit) in `scripts/core-loop-assert-test.sh`.
  The older wrong-branch/intended-branch fixtures were superseded and removed.
