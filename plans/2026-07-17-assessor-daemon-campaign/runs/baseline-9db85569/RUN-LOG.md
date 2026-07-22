# RUN-LOG — assessor delta-assessment (hk-fufel / crit3)
- PASS_ID: baseline-9db85569
- PIN_SHA: 9db85569fdc11967e99ad1a51d495b8cd596bbb5
- PIN_BRANCH: origin/main
- TRIGGER: preempt — PR#33 (hk-fufel, "fix(handler): remote-cwd-aware direct-exec spawn" — CRIT3) merged; re-pinned from f8d3a42e.
- SCOPE: DELTA — 2 merge-touched files: internal/handler/handler.go (+36/-4), internal/handler/launch_remotecwd_hkfufel_test.go (+116 new). This is the crit3/hk-fufel fix on the handler.Launch direct-exec seam (sibling to hk-czb11's codexdriver seam).
- CRUX (extra rigor, admiral's top target): crit3 = "real dispatch ENOENTs while harness asserts true". CR must confirm the new hkfufel test spawns a REAL subprocess w/ a non-existent remote cwd (would ENOENT under the bug) + is bound to the PROD type — NOT a stub false-green. Sibling hk-czb11 was REFUTED-as-false-green on exactly this evidence.

## STATUS AT HANDOFF: legs IN FLIGHT (keeper ACT interrupted)
- CR leg (cold review, crit3 false-green crux) — SPAWNED, running on pinsrc-9db85569. Result NOT yet folded.
- MG/LT leg (regression + core-loop @pin) — SPAWNED, running on its own mg-/lt-9db85569 clones. Result NOT yet folded.
- Both are background subagents that likely do NOT survive the keeper /clear. NEXT SESSION: check for their completion notifications/output; if absent, RE-RUN both (same pin, same crux). Then fold into ONE reasoned delta verdict, file any regressions as found-by:assessor + baseline-9db85569 scope label, post to admiral --topic gate WITH PIN_SHA=9db85569.
- Clones present: pinsrc-9db85569 (CR, read-only), mg-9db85569 + lt-9db85569 (MG/LT). Reusable if HEAD==pin + porcelain empty.

## RESUME (keeper-restart) — leg reconciliation
- CR leg SURVIVED /clear + COMPLETED (task a509698f3be08d3c7). Verdict-input:
  - CRUX false-green: **REFUTED** — launch_remotecwd_hkfufel_test.go drives REAL h.Launch (Substrate:nil, no fake-registry shortcut); asserts inDirCalls==1, commandCalls==0, cmd.Dir=="" — bound to handler.go's real branch selection.
  - Real dispatch path fixed: handler.go:291-296 CommandInDir branch IS the dispatched path (codex SessionIDCaptured→Substrate nil'd→exec branch; workloop.go:4357 rc.runner=concrete tmux.SSHRunner; runner.go:139 CommandInDir matches handler.RemoteCwdRunner). Pre-fix f8d3a42e unconditionally set cmd.Dir=WorkDir → local ssh ENOENT. Real bug, really fixed.
  - Regression: REFUTED — local/non-remote branches byte-preserved vs f8d3a42e.
  - crit3 class: CONFIRMED cleared on both argv-spawn seams (handler + codexdriver hk-czb11), subject to yankee real-ssh E2E (CR is read-only).
  - F1 (tier-1, NON-BLOCKING): no compile-time `var _ handler.RemoteCwdRunner = tmux.SSHRunner{}` anywhere — weaker than hk-czb11 which HAS the binding. Latent false-green vector if SSHRunner.CommandInDir signature drifts. Production satisfies iface TODAY (independently verified). Recommend adding binding in internal/daemon or cmd/harmonik.
  - F2 (tier-1, NON-BLOCKING, pre-existing = hk-okqyx class): spec.Env not forwarded to remote child over ssh. Flag only, not re-filed.
- MG orphan DIED mid-run (incomplete logs: fmt/tools/vet only, no lint/test). RE-RUNNING MG + LT fresh.

## RESUME cont'd — leg state after orphan cleanup
- LT leg (a36bc180) COMPLETED: RED on pi:local. Root cause = agent_failed structural claude_exit_without_outcome exit=-1; gap3 commit_landed PASS (spawn+dispatch worked, real commit) but t10 fail (agent exited w/o outcome → auto-reopen → merge didn't run). pi:local was GREEN on both prior pins (26a4cfd9, f8d3a42e). Delta does NOT touch local path (CR: byte-preserved) → causal link to hk-fufel mechanistically implausible; failure is post-spawn agent-runtime exit, NOT dispatch/ENOENT.
- LT RE-RUN (a54972aa) IN FLIGHT (gocache-ltrerun, owns lt-scratch daemon) — flake-vs-reproducible decider.
- MG leg (a3216daa) IN FLIGHT on mg-9db85569 (clean, HEAD==pin verified).
- Killed ORPHAN combined MG+LT leg (ab547a6c) from pre-/clear session — 141k tokens, duplicative, contention risk. Agent stopped; core-loop-lt scratch daemons are --no-supervise so no revival. Sweep for leftover scratch daemon AFTER live legs self-teardown.

## VERDICT: PASS @9db85569 (posted --topic gate 019f7a46)
- MG leg (a3216daa) COMPLETE: CI-parity GREEN. fmt/vet/build/lint 0 branch-introduced (both --new-from-rev baselines). Full-suite go-test exit1 = non-attributable macOS noise (darwin-only Seatbelt sandbox tests, Linux-CI-skipped, hk-tch4t + go-clean-cache wipe cascade). internal/handler passes ISOLATED w/ new test. Merge-result parity holds.
- LT re-run (a54972aa) GREEN — flake confirmed non-reproducible (pi:local all-green, t10 landed).
- 4-leg fold: CR PASS + LT PASS + MG GREEN + reconciliation OK -> PASS.
- Finding F1 filed hk-hobdz (P3, found-by:assessor, known-issue+remediation:assigned). F2=hk-okqyx (not re-filed).
- ASSESSMENT.md written. Orphan (ab547a6c) killed + lt-scratch supervised leftover reaped. Fleet daemon 21849 untouched. Sweep CLEAN.
- POSTURE: ARMED STANDBY. preempt3-watcher (pid 38536) armed on origin/main==9db85569. Do NOT self-terminate (operator HIGH cadence).

## BOUNDED DEEPER BASELINE (admiral quiet-window directive) — ALL GREEN
- S3 DOT full-graph: GREEN — TestDotMode_E2E_CascadeTransitions, 4-node cascade start→implementer→reviewer→terminal, reviewer_launched+reviewer_verdict, bead_closed. No single-collapse (Tier-0 false-green guard satisfied). Closes the delta-pass gap (delta LT was workflow:single).
- S2 H13 lost-wakeup: GREEN — TestSessionStore_AgentReadyLatch_ConcurrentFireAndInstall, 200 iters under -race, zero drops/races. Exceeds ≥20/one-hang-BLOCK bar.
- S7 H8 remote-Kill no-local-PID: GREEN (fidelity limit) — TestRemoteKill_ForcefullyKillsWorkerPID_HKBTL1N; kill routes over SSH runner, s.pid never local syscall.Kill'd. LIMIT: recording-runner unit variant (no live remote worker) — live loopback-SSH PID-liveness = yankee-owned E2E boundary, already stamped this session.
- No findings filed (no confirmed defect). Optional S7 H6 not run (stop-condition (a): all-green).
- Teardown clean (killed lingering supervise shim 37043 first). Fleet a3dc45482890=12 sessions, 21849+38536 alive, real HEAD 3e8a96a1 unchanged. Spot-checked by assessor: base-9db85569 procs EMPTY.
- Artifact: BASELINE-S2S3S7.md. POSTURE remains ARMED STANDBY on pin 9db85569.
