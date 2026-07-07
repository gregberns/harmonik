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
  its intended branch with `main` byte-pinned; RED before the fix, GREEN after. The
  two-sided `assert_t10` golden coverage (wrong-branch → fail, intended-branch → pass)
  stays in `scripts/core-loop-assert-test.sh` as permanent assertion-logic tests.
