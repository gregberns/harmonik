---
schema_version: 2
spawned_by: admiral
pass_id: baseline-f8d3a42e
pin_sha: f8d3a42efadbd3c29a70fa2b3ec91f42e9c8adb8
pin_branch: origin/main
measured_against: good-enough-principles.md
verdict: PASS
scope: DELTA (merge-touched paths of PR#32 / hk-czb11 only)
---

# ASSESSMENT — delta of PR#32 (hk-czb11: codexdriver remote-cwd-aware ssh spawn)

## VERDICT: PASS — merge-touched paths at f8d3a42e
Reasoned judgment (not a bead tally): the merge delivers a genuine, correctly-wired, narrow fix
with green CI-parity + core-loop + a cold review that **refutes** the false-green concern the
captain fan-out raised. Claimed-done reconciles against the actual commit/diff/tests. No tier-0.

## Trigger
Preempt: PR#32 merged to origin/main, advancing the pin from 26a4cfd9 → f8d3a42e. Re-pinned and
delta-assessed the 6 merge-touched files only. NOT hk-fufel (handler.go untouched — crit3 pending).

## Merge-touched surface (6 files, +392/-2)
cmd/harmonik/substrate_select.go · internal/codexdriver/driver.go · internal/lifecycle/tmux/runner.go
+ 3 new tests (spawn_remotecwd_czb11 · substrate_select_commandindir_czb11 · runner_commandindir_czb11).

## Legs
- **CR (cold review) — no tier-0.** False-green REFUTED: the driver test spawns a REAL twin
  subprocess with a non-existent remote cwd — would ENOENT under the bug — and asserts SpawnWindow
  SUCCEEDS with local cmd.Dir=="" (spawn_remotecwd_czb11_test.go:80-114); the router test binds the
  assertion to the production type via a compile-time `var _ RemoteCwdRunner` proof; the runner test
  pins the exact `cd <q(dir)> && exec` ssh argv (a legit string-level contract for ssh transport).
  ENOENT genuinely eliminated on the codexdriver seam (driver.go:218-228 leaves local cmd.Dir unset,
  applies remote cwd on the worker; ssh runs in daemon cwd, on PATH). Local path byte-identical — no
  regression. Scope honest: fixes ONLY codexdriver.spawn; zero handler.go changes → does NOT clear crit3.
- **MG (CI parity) — PASS.** fmt-check(real, tools installed) / vet / build / golangci-lint
  `--new-from-rev=origin/main` (0 branch-introduced issues) / `go test -short -race` on all
  merge-touched pkgs (codexdriver 14.5s, lifecycle/tmux 12.6s, cmd/harmonik 39.0s, +digest +supervise)
  — all exit 0, no races. (fmt-check on a fresh clone false-greened until pinned tools installed —
  caught and re-run for a real result.)
- **LT (core-loop) — PASS.** `make core-loop-lt` scratch built at the pin (log-confirmed f8d3a42e):
  MATRIX_JSON green=1 red=0 pending=0 skip=0, gate:true, all_green:true, EXIT_CODE=0. gap1/gap3/gap4/t10
  pass. First run, no flake.

## Claimed-done reconciliation
PR#32 claims "remote-cwd-aware ssh spawn." Verified against the actual diff/tests: the remote-cwd
fix is real, wired to production (substrate_select.go Options.Runner=router; router implements
CommandInDir; driver.spawn takes the remote branch), and local behavior is byte-preserved. The claim
reconciles. The change does NOT claim to fix crit3/hk-fufel, and does not — honestly scoped.

## Findings (evidence, not the gate)
- **hk-okqyx** (P2, found-by:assessor, known-issue, main-health) — driver.go:226-227 sets cmd.Env on
  the LOCAL ssh process; ssh doesn't forward to the remote child (no SendEnv). Remote codex children
  may miss env vars. **PRE-EXISTING, NOT branch-introduced → not a blocker for this delta.** Admiral
  to adjudicate remediation track.
- (cosmetic) duplicate RemoteCwdRunner interface decl in codexdriver + handler — justified by the
  depguard no-tmux-import boundary; idiomatic Go structural typing. Not a defect.

## Residual risk for the admiral
1. **hk-okqyx** — remote env-forwarding gap (main-health, pre-existing); "remote spawn works
   end-to-end" is not fully proven by this merge.
2. **Remote live fault-injection not re-run in this delta** — docker/remote substrate; the codexdriver
   seam is covered by CR + the merged tests, and full remote E2E is CI's leg (admiral-confirmed known
   coverage boundary, not a gap to chase).
3. **crit3 / hk-fufel NOT in this merge** — the analogous handler.Launch seam (handler.go) is still
   pending in the uncommitted tree; it is the admiral's top delta target for the next merge.

## Coverage
Legs RUN: CR + MG + LT (3/3). Scope = delta (merge-touched paths), not a full S1–S7 re-run — correct
for a targeted merge delta. Critic: dry (no additional angle outstanding for this 6-file surface).
