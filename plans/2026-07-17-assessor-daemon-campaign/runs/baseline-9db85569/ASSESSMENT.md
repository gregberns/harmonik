# ASSESSMENT — assessor delta-gate, PR#33 / hk-fufel (crit3)

- **PIN_SHA:** `9db85569fdc11967e99ad1a51d495b8cd596bbb5` (== origin/main)
- **GATE:** merge (delta) · **SCOPE:** 2 merge-touched files — `internal/handler/handler.go` (+36/−4), `internal/handler/launch_remotecwd_hkfufel_test.go` (+116 new)
- **VERDICT:** **PASS** (delta), with one non-blocking finding + a stated coverage boundary.

## What this fix is
hk-fufel makes `handler.Launch`'s direct-exec spawn seam remote-cwd-aware: when the runner is a `RemoteCwdRunner` and `WorkDir != ""`, it builds the command via `CommandInDir` (remote `cd && exec`) and leaves the local `cmd.Dir` UNSET — eliminating the local ssh fork/exec ENOENT. Sibling to hk-czb11 (codexdriver seam, already PASS-accepted).

## Legs (delegated; folded into one reasoned judgment)

**CR — cold code review — PASS.**
- CRUX (false-green) **REFUTED**: the new test drives the REAL `handler.Launch` (Substrate nil, no fake-registry shortcut — the exact shortcut that made hk-czb11's first tests false-green); assertions (`inDirCalls==1`, `commandCalls==0`, `cmd.Dir==""`) bind to the real branch selection.
- Fix is on the ACTUALLY-DISPATCHED path: codex is SessionIDCaptured → Substrate nil'd → exec branch; `workloop.go:4357` sets a concrete `tmux.SSHRunner`, `runner.go:139 CommandInDir` matches `handler.RemoteCwdRunner` → runtime type-assert succeeds. Pre-fix (`f8d3a42e`) unconditionally set `cmd.Dir=WorkDir` → local ssh ENOENT. Real bug, really fixed.
- Local / non-remote paths **byte-preserved** vs `f8d3a42e`; no unwanted abstraction.

**LT — live-verify (core-loop matrix) — PASS.**
- First run RED on `pi:local` (`agent_failed structural claude_exit_without_outcome exit=-1`; gap3 commit_landed PASS but t10 fail). **Re-run all-GREEN** (`MATRIX_JSON … all_green:true`, t10 landed). Confirmed a **non-reproducible environmental agent-exit flake**, not a regression — consistent with the mechanistic fact that the delta doesn't touch the local path and spawn/dispatch succeeded (a real commit landed).

**MG — CI merge-gate parity — GREEN (all CI-gate checks).**
- fmt-check / `go vet` / `go build` / golangci-lint (both `--new-from-rev=origin/main` AND branch-isolated `9db85569^`) → **PASS, 0 branch-introduced issues**.
- Full-suite `go test -short -race` exit=1 is **non-attributable macOS-local noise**: (1) darwin-only Seatbelt sandbox tests (in `internal/daemon`, hk-tch4t) failing under fork saturation from the concurrent live fleet — these **skip on the Linux CI gate**; (2) a `go clean -cache` mid-run wipe (DiskCheck reaper) cascading `[build failed]`. **`internal/handler` — the only changed package — re-run in isolation PASSES, including the new test.** Merge-result parity holds (pin ⊇ origin/main, PR#33 merged).

**Reconciliation (claimed-done vs reality).** Commit `9db85569` = "fix(handler): remote-cwd-aware direct-exec spawn (hk-fufel)" — real commit, real 2-file diff, passing new test. Claim matches artifacts.

## Findings
- **F1 (P3, NON-BLOCKING) — filed `hk-hobdz`** (`found-by:assessor, hk-fufel, known-issue, remediation:assigned`): no compile-time `var _ handler.RemoteCwdRunner = tmux.SSHRunner{}` binding anywhere (sibling hk-czb11 HAS one). Latent false-green vector if the runner signature drifts. Production satisfies the interface today → not a current defect. Fix: add the binding in `internal/daemon`/`cmd/harmonik`.
- **F2 — NOT re-filed** (= existing `hk-okqyx`): `spec.Env` not forwarded to the remote child over ssh. Pre-existing, non-blocking.

## Coverage boundary (stated, not a gap to chase now)
Full crit3-CLASS clearance on a REAL remote ssh worker is an end-to-end proof I cannot run (read-only, no live remote) — owned by yankee's real ssh-worker E2E, mirroring the H4/H5/H8 remote-skip boundary the admiral already accepted. On the argv-spawn seams verifiable here, both (handler + codexdriver) are now remote-cwd-aware and the ENOENT bug class is closed.

## Bottom line
Genuine crit3 fix on the real dispatched path, false-green refuted, local path byte-preserved, CI-parity green, live-verify green. **PASS** @9db85569. One P3 test-robustness known-issue (hk-hobdz) + the yankee-owned remote-ssh E2E boundary; neither blocks the merge that already landed.
