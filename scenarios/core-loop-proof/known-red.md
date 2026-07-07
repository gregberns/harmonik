# core-loop-proof — known-RED cells

A **known-RED** cell is an assertion that is EXPECTED to fail today against a tracked
daemon defect. It is recorded here and self-tested as an expected-fail — it is NOT a
false-green, and it is deliberately excluded from T9's full-matrix green gate (the gate
is "full-matrix green **minus** the known-RED cells").

When the underlying defect lands, the assertion flips to `pass`, its expected-fail
self-test row breaks loudly, and that break is the signal to retire the entry here.

## t10 — per-bead integration-branch targeting (hk-lgykq)

- **Assertion:** a bead directed at integration branch X must LAND on X, not main
  (`assert_t10` in `scripts/core-loop-assert.jq`; asserts the merged
  `workspace_merge_status.target_branch == expect.lands_on`).
- **Why RED today:** per-bead / DOT integration-branch targeting is **dead code** — the
  `LandsOn` / `landTaskBranch` path (`internal/branching/branching.go:66`,
  `internal/daemon/workloop.go:3153`) is not wired into the live workloop merge, so every
  run lands on the daemon-wide default target (main).
- **Tracked by:** **hk-lgykq** (T10 hk-xke2i blocks it — the known-RED is the evidence).
- **Flip proof:** the `t10-would-pass` golden already shows the assertion passing when the
  merge targets the intended branch; the `t10-known-red` golden is today's reality.
- **Retire when:** hk-lgykq lands → the "t10 KNOWN-RED today" self-test row will fail
  (expected `fail`, gets `pass`); at that point delete that row and this section, and add
  `t10` to the relevant cells' `gaps` in `cells.json`.
