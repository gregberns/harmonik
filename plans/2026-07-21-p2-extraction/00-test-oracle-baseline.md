# P2 test oracle — measured baseline (2026-07-22)

**Why this file exists.** Every P2 unit's release gate says "full test suite green" (`_plan.md` §5.3).
That gate is **not usable as written**: `internal/daemon` is already red before any extraction work.
This file records the measured baseline so each unit can be verified against a *differential* oracle —
**no NEW failures** — instead of an unreachable zero.

> **Read this before you re-derive anything by hand.** Three rules govern every use of this file.
> They are stated once here and expanded in §"Known-flaky allowlist".
>
> 1. **A load-sensitive flake is confirmed by re-running it IN ISOLATION.** Isolated pass = load
>    artifact, not a regression.
> 2. **An isolation-sensitive flake is confirmed by re-running it IN THE FULL SUITE.** This is the
>    *opposite* procedure, and applying the wrong one inverts the answer. Exactly one entry is in this
>    class today: `TestMergeToMain_RealConflictWithBeadsLedger_Escalates`.
> 3. **Compare stable INTERSECTIONS across repeated runs, never one run against one run.** Three runs
>    of the *same* commit gave 123 / 3 / 9 failures; runs B and C shared 1 of 11 names. A single
>    before/after pair is not evidence (PROGRESS.md §"Verification verdict (Phase 5)").
> 4. **A test can be reported FAILED without any assertion failing.** Go's harness marks a test failed
>    when `t.TempDir()` cannot be removed (`TempDir RemoveAll cleanup: … directory not empty`), long
>    after the test body returned green. **Neither procedure above applies to those** — both re-runs
>    pass the assertion and tell you nothing. Exactly one entry is in this class today:
>    `TestL2_SSHLocalhost_TmuxPaneID_hk8u2al`.

## Measurement

Two runs, both `-count=1`:

- **HEAD** = `34509e60` checked out clean in a detached worktree (no staged work).
- **Working tree** = HEAD + the staged `gitprobe` / `harness/shared` extraction already in the index.

Full-suite run on the working tree: `go test ./internal/daemon/... -count=1 -timeout 25m`
→ `FAIL github.com/gregberns/harmonik/internal/daemon 604.513s`, 6 failing tests.
Sub-packages `daemon/bootconfig` and `daemon/router` were green.

Then the same 6 tests re-run **in isolation** (`-run` filter) at HEAD and on the working tree:

| Test | HEAD (isolated) | Working tree (isolated) | Classification |
|---|---|---|---|
| `TestThroughput_TenBeadsAtMaxFour` | FAIL | FAIL | **Hard pre-existing failure** |
| `TestScenario_Hk6ynv4_SubscribeStream_EndToEnd` | FAIL | PASS | Load-sensitive flake |
| `TestStopHookE2E_TwinRelayFastPath` | FAIL | PASS | Load-sensitive flake |
| `TestStopHookE2E_TwinRelayWaitGrace` | FAIL | PASS | Load-sensitive flake |
| `TestPasteInjectQuitOnCommit_PostQuitWatchdogKillsOnGrace` | PASS | PASS | ~~Flakes only under full-suite load~~ — **wrong, see below** |
| `TestT6_10BeadSequentialDrain` | PASS | PASS | Flakes only under full-suite load |

> **The `_PostQuitWatchdogKillsOnGrace` row above is superseded.** Its "load-sensitive" classification
> rests on the single isolated PASS in this table. It is **known-red at HEAD**: it fails in isolation
> too, roughly half the time. See §"Known-flaky allowlist" → known-red mechanism table. This is rule 3
> biting exactly as stated — one isolated pass is not a classification.

## Findings

1. **The staged `gitprobe` + `harness/shared` work introduces no regressions.** Every test that fails on
   the working tree also fails at HEAD, or passes in isolation on both. Nothing regressed.
2. **One genuine pre-existing failure:** `TestThroughput_TenBeadsAtMaxFour` fails identically at HEAD in
   isolation. It is a throughput/timing assertion and is out of P2 scope — do not let it block a unit,
   and do not "fix" it inside an extraction commit (that would violate the pure-move rule, §5.1).
3. **Five load-sensitive flakes.** All are timing/e2e-shaped (watchdog grace windows, stop-hook relay
   deadlines, 10-bead drain throughput, subscribe-stream end-to-end). They fail when the machine is
   loaded and pass when it is not. During the baseline run three recon agents and a worktree checkout
   were competing for CPU.

## Consequences for the P2 release gate

Replace `_plan.md` §5.3 "whole-repo `go test ./...` is green" with:

> **Differential green.** Run `go test ./internal/daemon/... ./internal/harness/... -count=1 -timeout 25m`
> before and after the unit. Extract the failing-test name set from each. The unit passes iff the
> after-set contains **no name absent from the before-set**. A shrinking set is fine. A test that moved
> packages must be matched by its new package-qualified name, not treated as disappeared.

Extraction commands, for reuse:

```bash
go test ./internal/daemon/... ./internal/harness/... -count=1 -timeout 25m 2>&1 \
  | grep -E '^--- FAIL' | sort -u > after-failures.txt
comm -13 before-failures.txt after-failures.txt   # must be EMPTY
```

**Do not run the full suite concurrently with agent fan-out or a parallel build** — that is what
manufactured five of the six baseline failures. Serialize the verification run.

## Addendum 2026-07-22 (post-extraction): three more oracles, found the hard way

The single `internal/daemon` oracle above turned out to be insufficient. Three more surfaces were
discovered *during* the extraction, each by a verifier rather than by the original baseline:

**1. `specaudit` is a SECOND red oracle, and it is load-bearing.**
`go test -tags specaudit ./internal/specaudit/...` is already red at HEAD (7 top-level failures,
identical set at `20cbd18d^`). It is not decorative: two of those sensors **scan source files by
hardcoded path**, so an extraction can legitimately break them —
`hc045a_claudecode_bridge_pointer_test.go` pinned `internal/daemon/claudeharness.go`, and
`wminv003_task_branch_append_only_test.go` allowlisted `internal/daemon/codexcommit.go` by literal
string. Neither is compiled by `go build`, `go test` or `golangci-lint`. Both were updated in the same
commits as their moves; both verified passing under the tag.

**Beware a false characterization** that was made and caught: these were reported as "all `specs/*.md`
front matter." They are not. `TestONINV006SocketOps` fails on a *code* anchor (`switch req.Op` in
`internal/daemon/socket.go`) and `TestSHINV001NoTestModeBranches` on `cmd/harmonik/eval_guardrails_lygpp.go:91`.

**2. `TestMergeToMain_RealConflictWithBeadsLedger_Escalates` is ISOLATION-sensitive — the inverse of
every other flake here.** It **passes in the full suite** and **fails in isolation** (identical 30s
timeout at `34509e60`, `805a9d76`, `20cbd18d`, `bfb87bfd`). This matters procedurally: the five
load-sensitive flakes are confirmed by re-running *in isolation*; this one must be confirmed by
re-running *in the full suite*. Applying the wrong procedure inverts the answer.

**3. The disk watermark silently fakes failures.** The daemon self-checks free disk and pauses
dispatch: `disk-check: available=7626MiB watermark=10240MiB — dispatch paused`. Below the watermark,
dispatch/throughput/timing tests fail for reasons unrelated to any code change
(`TestWorkLoop_SubmittedPendingGroupBootstrapsToActive` was observed failing this way). **Check
`df -h` before trusting a differential run**, and treat any timing failure as suspect until the box is
above 10 GiB free.

**4. Testing the working tree tests OTHER AGENTS' uncommitted work.** When a second agent shares the
checkout, `go test` compiles their dirty files too. Run the differential suite against a **clean
detached worktree at your own HEAD**, never the shared working tree.

## Corpus change — RT14 (2026-07-22)

**Four tests LEFT the corpus** when RT14 phase C (`7d448afb`) deleted
`internal/daemon/agentready_hkgql2018_test.go`: the four unit tests of `waitAgentReady`, which RT14
retired along with the function. A differential run after RT14 will therefore see a **shrinking**
test set, which this oracle permits — record it here so it is not later read as a mystery loss.

Coverage transfer (each verified to exist, per the RT14 recipe §4 C3):

| Deleted case | Replacement |
|---|---|
| `DetectReady` true → nil | `runexec.TestDispatch_HappyPath` + `TestDispatchSegment_ResumeProbe_RunIDStampedReadyDelivers` |
| no events → `ErrAgentReadyTimeout` | `runexec.TestDispatch_ReadyTimeoutSR9Edge` + `TestDispatchSegment_DotResume_ReadyTimeoutEdge` |
| ctx cancel → `ctx.Err()` | `runexec.TestDispatch_AbortFromAnyNonTerminal` (the shell maps cancel onto `EvAborted`) |
| boundary race (ready wins at the timeout instant) | **none, by construction** — the race was an artifact of `waitAgentReady`'s wall-clock select over two channels; the machine resolves the same edge deterministically on one goroutine |

`twinparity_timing_property_test.go` stays in the corpus with the same test names; its stage-1
detector call was replaced by the timing predicate directly (same reason as the last row above).

## Known-flaky allowlist (as of 2026-07-23)

### The four classes, and how each one is confirmed

Every entry belongs to exactly one class. **The confirmation procedure is different per class, two of
them are opposites, and for the fourth NEITHER works** — running the wrong procedure gives you the
wrong answer with full confidence.

| Class | What it means | **How to confirm** |
|---|---|---|
| **load-sensitive** | Fails in the full suite, passes alone. A fixed wall-clock budget is exceeded because the machine is busy. | **Re-run IN ISOLATION**: `go test ./internal/daemon/ -run '^TestX$' -timeout 5m`. Isolated **pass** ⇒ load artifact, not a regression. Isolated **fail** ⇒ real. |
| **isolation-sensitive** | The mirror image. Fails alone, **passes** inside the full suite. | **Re-run IN THE FULL SUITE** — the isolated failure is the artifact. Do **not** "confirm" it by re-running it alone; that reproduces the artifact and reads as a real regression. |
| **known-red at HEAD** | Fails at the pre-change commit too. Not a regression under any procedure. | A/B the *unmodified* file at HEAD, ideally under `-count=8`. Never let one of these block a unit, and never "fix" one inside an extraction commit (§5.1 pure-move rule). |
| **harness-teardown** — *the failure is in the harness, not in the test* | Every assertion passes. The FAIL is emitted by Go's `testing` package **after** the test body returned, because `t.TempDir()` could not be removed. | **NEITHER re-run procedure applies — do not use them.** Isolation and full-suite both pass the assertion, so both are silent on the question. Confirm by **reading the FAIL text**: if the only detail under `--- FAIL` is a `TempDir RemoveAll cleanup: … directory not empty` line, with no assertion message, it is this class and it is not your change. |

And regardless of class: **compare stable intersections across repeated runs, never one run against one
run.** See the box at the top of this file.

### Greppable name list

```
# LOAD-SENSITIVE — confirm by re-running IN ISOLATION (isolated pass => load artifact)
TestScenario_Hk6ynv4_SubscribeStream_EndToEnd
TestStopHookE2E_TwinRelayFastPath
TestStopHookE2E_TwinRelayWaitGrace
TestT6_10BeadSequentialDrain
TestWorkLoop_ShutdownDrainsCommittedRun_hkdnrg              # ADDED 2026-07-22 (RT15 run)
TestT2_ExitZeroNoSignal                                     # ADDED 2026-07-22 (RT15 run)
TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency          # ADDED 2026-07-22 (RT15 run)
TestPasteInjectQuitOnCommit_BriefDeliveredGateOpensOnClose  # ADDED 2026-07-23 (hk-8cxxb)
TestScenario_FailingImplementer_RunFailed                   # ADDED 2026-07-23 (hk-8cxxb)
TestScenario_SupervisorRevive_DaemonStart_WiresKeepaliveGoroutine  # ADDED 2026-07-23 (hk-8cxxb)
TestScenario_ReviewLoop_ResumeSubmitReliable                # ADDED 2026-07-23 (hk-8cxxb)
TestRunBead_CancelNotCalledOnFailure                        # ADDED 2026-07-23 (hk-8cxxb)
TestConcurrentRemoteAgentReady_BothBeadsReopened            # ADDED 2026-07-23 (hk-8cxxb)
TestPasteInjectActivityAware_ProgressingPaneSurvivesCeiling # ADDED 2026-07-23 (hk-8cxxb)
TestQuitOnReviewFile_ContinuousHeartbeatsExtendBudget       # ADDED 2026-07-23 (hk-8cxxb)
TestScenarioGateEfficacy_GenuineRedBlocksMerge              # ADDED 2026-07-23 (hk-8cxxb)
TestDaemonStart_SocketBindsBeforeRestartBackoffSleep        # ADDED 2026-07-23 (lane S)
TestCodexHarness_LaunchSpec_ResumeDelegates                 # now in internal/harness/codex
TestCodexHarness_LaunchSpec_InitialDelegates                # now in internal/harness/codex
TestCodexHarness_LaunchSpec_CustomBinary                    # now in internal/harness/codex

# ISOLATION-SENSITIVE — confirm by re-running IN THE FULL SUITE (isolation is what breaks it)
TestMergeToMain_RealConflictWithBeadsLedger_Escalates       # pre-dates P2

# KNOWN-RED AT HEAD — not a regression under any procedure
TestThroughput_TenBeadsAtMaxFour                            # out of P2 scope
TestM4C7_D2Chokepoint_IsHarnessAgnostic                     # build-tagged fixture compile; out of P2 scope
TestPasteInjectCommitBudget_IdleActivePane_HKukx            # RECLASSIFIED 2026-07-23 (was load-sensitive)
TestPasteInjectQuitOnCommit_NewCommitNoKill                 # ADDED 2026-07-23 (hk-8cxxb)
TestPasteInjectQuitOnCommit_PostQuitWatchdogKillsOnGrace    # RECLASSIFIED 2026-07-23 (was load-sensitive — MISFILED, see mechanism)

# HARNESS-TEARDOWN — the assertion PASSES; the FAIL comes from Go's testing package.
# NEITHER re-run procedure applies. Read the FAIL text instead.
TestL2_SSHLocalhost_TmuxPaneID_hk8u2al                      # ADDED 2026-07-23 (lane S); TempDir RemoveAll vs. tmux server

# any dispatch/throughput/timing failure while `df -h` shows <10 GiB free — see Addendum §3
```

### Mechanism per entry

A name alone is not enough: without the mechanism an agent cannot tell whether a *new* failure it is
looking at is the same phenomenon. Each mechanism below was read out of the test source. Where the
source did not establish one, the row says so rather than guessing.

**Load-sensitive**

| Test (file, all under `internal/daemon/` unless noted) | Mechanism |
|---|---|
| `TestPasteInjectQuitOnCommit_BriefDeliveredGateOpensOnClose` (`pasteinject_hk930o3_test.go`) | Gives itself a 5s ctx while a background goroutine sleeps 30ms then calls `hk930o3AddCommit`, which shells out to `git add` + `git commit`. Under load the two git subprocesses exceed the budget and the ctx expires before the 5ms poll sees the new HEAD. Observed at **5.74s** and **5.37s**. Nothing in production is involved. |
| `TestScenario_FailingImplementer_RunFailed` (`scenario_failing_implementer_test.go`) | Real `daemon.Start` end-to-end against a real `br` wrapper script and the `harmonik-twin-claude` binary, with three stacked fixed wall-clock budgets: `AgentReadyTimeout` 5s, `runFailedPollBudget` 20s for `run_failed` to reach the JSONL, and 5s for `daemon.Start` to return after cancel. Every one is a subprocess-spawn budget, so every one stretches with machine load. |
| `TestScenario_SupervisorRevive_DaemonStart_WiresKeepaliveGoroutine` (`scenario_supervisor_revive_default_selfheal_hku0pz_test.go`) | `t.Parallel()`. Asserts the keepalive goroutine that `daemon.Start` launches records ≥1 `EnsureSession` call within a fixed **2s** budget, polling every 2ms, with `daemon.WithSessionKeepalive(5ms)`. The assertion is on goroutine scheduling latency — a loaded scheduler *is* the failure mode. |
| `TestScenario_ReviewLoop_ResumeSubmitReliable` (`reviewloop_resume_submit_hkip33d_test.go`) | Drives a full REQUEST_CHANGES→APPROVE cycle with `AgentReadyTimeout` 8s and `resumeSubmitRetryDelay` shrunk to 50ms, inside a 90s ctx, against a real `/bin/sh` handler script. Its own header names the second mechanism: it contends on the process-global `~/.claude.json` trust lock (`rlIsolateClaudeConfig`) — "the dominant intermittent `-short` red" — which is why it is deliberately **not** `t.Parallel()`. |
| `TestRunBead_CancelNotCalledOnFailure` (`run_hkicecw_test.go`) | `t.Parallel()`. The pass path is to **wait out a 10s timer inside a 15s ctx**, leaving ~5s of headroom for git fixture setup, work-loop start and `/bin/sh -c 'exit 1'`. The tighter constraint is the inner fast path: once the first `ReopenBead` lands it gives the queue only **200ms** to settle into `queue.QueueStatusPausedByFailure` before asserting. |
| `TestConcurrentRemoteAgentReady_BothBeadsReopened` (`concurrent_remote_agent_ready_hk4hso5_test.go`) | Production budgets are sub-second (`AgentReadyTimeout` 150ms, kill-reap overridden to 50ms) but the assertion is a hard **2s** `time.After` on `bothReopenedCh`, with `MaxConcurrent=2` driving two concurrent runs that each build a temp worktree and spawn `/bin/sh`. Not `t.Parallel()` (it mutates a package global via `ExportedSetAgentReadyKillReapTimeout`), but it still competes with the rest of the suite. |
| `TestPasteInjectActivityAware_ProgressingPaneSurvivesCeiling` (`pasteinject_hkaz4fd_test.go`) | 5s ctx and a 5s commit budget. The background goroutine churns the worktree 12× at 25ms (≈300ms nominal) before shelling `git add` + `git commit`, and the production code under test shells `git status --porcelain` on **every 5ms poll** to compute the activity fingerprint — hundreds of git subprocesses inside a 5s window. Under load the commit lands after the deadline and the test fails with "the run hung instead of exiting on commit". |
| `TestQuitOnReviewFile_ContinuousHeartbeatsExtendBudget` (`pasteinject_hksj6a_test.go`) | The tightest assertion in the set. Measures `time.Since(startedAt)` around `ExportedPasteInjectQuitOnReviewFile` and requires the result inside a **15ms–150ms** window (`>= budget-5ms`, `<= hardCeiling/2`) on a 5ms poll with a 5ms heartbeat pump. Any scheduler stall past ~150ms trips "Kill fired too late". |
| `TestScenarioGateEfficacy_GenuineRedBlocksMerge` (`scenario_gate_efficacy_hkv5dyg_test.go`) | `t.Parallel()`, and its harness `runGateEfficacyWorkLoop` allows **85s** for a path that *compiles and runs* `go test -tags=scenario` on a freshly-written package inside a real git worktree and then merges to main. The harness comment sizes that budget for "cold-cache compilation"; a cold `GOCACHE` (which `with-isolated-gocache.sh` guarantees) plus load puts the compile alone in the same order as the budget. |
| `TestWorkLoop_ShutdownDrainsCommittedRun_hkdnrg` (`workloop_shutdown_drain_committed_hkdnrg_test.go`) | A fixed `time.Sleep(300 * time.Millisecond)` "to give the handler a moment to launch" before cancelling the context. Under load the `/bin/sh -c 'sleep 60'` handler may not be running yet, so the cancel takes a different branch than the drain path being asserted. Its own skip comment says it "spawns sleep 60 + real git worktrees — heavy under parallel load", and it is `t.Parallel()`. Outer budgets: 20s to claim, 30s to drain. |
| `TestT2_ExitZeroNoSignal` (`t2_scenarios_test.go`) | `t.Parallel()`, **6s** poll deadline inside an 8s ctx, for a path that runs `workloopFixturePreCommitWorktreeFactory` (real `git worktree add` + commit), spawns `/bin/sh`, then merges and closes the bead. 6s is thin for three git subprocess round-trips on a loaded box. |
| `TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency` (`workloop_test.go`) | `t.Parallel()`, 10 beads through the real worktree factory and the real launch-spec path, each paying ~3s `stopHookGrace`, against a **25s** poll deadline / 30s ctx. At `MaxConcurrent=4` the nominal cost is already ~8s of pure grace before any git work. |
| `TestDaemonStart_SocketBindsBeforeRestartBackoffSleep` (`restartbackoff_socketbind_hkuzvt9_test.go`) | `t.Parallel()`. Seeds a restart record with two prior boots so the boot computes `n=2` → delay `2×base` with `base: 3s`, then polls for `.harmonik/daemon.sock` for exactly `backoffBase/2` = **1.5s** while a real `daemon.Start` boots in a goroutine. The hk-uzvt9 invariant it guards (bind must not wait on the backoff sleep) is itself load-independent; the **1.5s bind budget** is not, and a contended box overruns it. Failure text is the guard's own "daemon.sock not found … within 1.5s". Measured 3/3 passing in isolation. |
| `TestCodexHarness_LaunchSpec_ResumeDelegates` / `_InitialDelegates` / `_CustomBinary` (`internal/harness/codex/harness_test.go`) | **Partly established.** The test bodies are pure argv assertions, but `codex.Harness.LaunchSpec` is **not** pure: before building argv it calls `os.Getwd()` and then `cleanCodexStaleWAL`, which reads `<cwd>/.harmonik/config.yaml`, globs `state_*.sqlite-wal` under the resolved `CODEX_HOME`, `os.Stat`s each match, shells out to **`lsof`** to decide whether the WAL is held, and can `os.Remove` it. All three tests are `t.Parallel()`, so they contend on machine-global state outside any `t.TempDir()` — including the operator's live `~/.codex` — and on `lsof`, whose latency is load-dependent. **Not established:** why *these three* and not the sibling `LaunchSpec` callers in the same file (e.g. `_CredentialKeysStripped`). Treat the selection as unexplained. |

**Isolation-sensitive**

| Test | Mechanism |
|---|---|
| `TestMergeToMain_RealConflictWithBeadsLedger_Escalates` (`mergetomain_hkpphof_test.go`) | **Mechanism: not yet established — do not guess.** What *is* established: the failure signature is the test's own 30s `context.WithTimeout` expiring without `ledger.doneCh` closing, identical at `34509e60`, `805a9d76`, `20cbd18d` and `bfb87bfd`; the test is `t.Parallel()` and drives a real rebase conflict on both `work.txt` and `.beads/issues.jsonl`. What is **ruled out**: load. It fails when the box is idle and passes when it is busy — the inverse of every other entry here — so the load-sensitive reasoning does not transfer. Confirm only by re-running it inside the full suite. |

**Known-red at HEAD**

| Test | Mechanism |
|---|---|
| `TestThroughput_TenBeadsAtMaxFour` (`t11_throughput_test.go`) | Not a timeout — a **wall-clock ratio assertion**: parallel per-bead must be < 3× sequential per-bead, measured by running a 3-bead `MaxConcurrent=1` daemon and a 10-bead `MaxConcurrent=4` daemon *concurrently in the same process* against the real `br` binary. The number it measures is a property of the machine, not of the code, so no code change makes it deterministic. Out of P2 scope. |
| `TestPasteInjectCommitBudget_IdleActivePane_HKukx` (`pasteinject_hk9vp51_test.go`) | **Reclassified 2026-07-23** from load-sensitive. Asserts `elapsed < 100ms` for a kill whose nominal path is budget 40ms + kill delay 5ms + 3 polls ≈ 60ms — a ~40ms margin over a 5ms poll. Observed failure: `kill fired after 274ms — too close to hard ceiling (120ms)`. RT19.0 proved it pre-existing by A/B on the unmodified file under `-count=8` (6 failures pre-edit, 5 post-edit). The 2026-07-22 note below, which called it a load artifact on the strength of one isolated pass, is superseded — see rule 3 at the top of this file. |
| `TestPasteInjectQuitOnCommit_NewCommitNoKill` (`pasteinject_hktrjef_test.go`) | 3s ctx while a background goroutine sleeps 30ms then shells `git add` + `git commit`. `commitPollTimeout` is 5s, so the **test's own 3s ctx**, not the production timeout, is the binding constraint: if the two git subprocesses do not finish inside 3s the poller never sees the new HEAD and `SendQuitToLastPane` stays at 0. Red at HEAD alongside `_IdleActivePane_HKukx` in the same RT19.0 A/B. |
| `TestPasteInjectQuitOnCommit_PostQuitWatchdogKillsOnGrace` (`pasteinject_hktrjef_test.go`) | **Reclassified 2026-07-23 — it was MISFILED as load-sensitive, and the misfiling pointed readers at the wrong conclusion.** The documented procedure for load-sensitive is "re-run it alone"; run alone, this test **fails about half the time at HEAD**, so an agent following the file would have read its own change as a real regression. It is known-red: not a regression under any procedure. Mechanism (same shape as its file-mate `_NewCommitNoKill`, only tighter): the test gives itself a **2s** ctx while a background goroutine sleeps 20ms then shells `git add` + `git commit`; meanwhile the production poll loop shells `git rev-parse HEAD` (`gitprobe.ResolveWorktreeHEADVia`) on a **5ms** ticker — ~200 forks/sec competing with the very commit it is waiting for. If the commit does not land inside 2s, HEAD never changes, the ctx expires, and neither `/quit` nor the post-quit watchdog ever fires: `SendQuitToLastPane calls: want 1, got 0` at ~2.5s wall. Measured: 3 of 6 isolated runs failing (RT19c verification pass) and 2 of 3 here, on an idle box with 22 GiB free — both failures burning the full 2s ctx. |
| `TestM4C7_D2Chokepoint_IsHarnessAgnostic` | **Mechanism: not re-derived this pass.** Carried forward from the 2026-07-22 entry: build-tagged fixture does not compile. Out of P2 scope. |

**Harness-teardown** — *the assertion passes; the harness fails the test after it returned*

| Test | Mechanism |
|---|---|
| `TestL2_SSHLocalhost_TmuxPaneID_hk8u2al` (`scenario_ssh_localhost_l2_hk8u2al_test.go`) | **Do not apply either re-run procedure — neither one can answer the question.** The test's own assertion (`#{pane_id}` survives the ssh → remote-shell quoting chain and comes back as `%N`) passes; the FAIL is `TempDir RemoveAll cleanup: … directory not empty`, raised by Go's `testing` package. Mechanism: the test starts a real tmux server over `ssh localhost` with `HOME=<t.TempDir()>` and an isolated `-L hk8u2al-test` socket. `t.TempDir()` registers its `RemoveAll` cleanup at the moment it is called and the `kill-server` cleanup is registered later, so LIFO does run kill-server first — but the server's teardown is **asynchronous**: the dying login shells write `$HOME/.zsh_history` after `kill-server` has already returned. Verified directly by driving the same tmux/HOME shape by hand outside the suite: the sandbox HOME is empty while the server runs and gains `.zsh_history` only after kill-server. When that write lands between `RemoveAll`'s readdir and its rmdir, Go's cleanup calls `c.Errorf("TempDir RemoveAll cleanup: %v", err)` (`$GOROOT/src/testing/testing.go`, `T.TempDir`) and an already-green test is reported FAILED. Measured 28/28 and 8/8 assertion-passing across two campaigns, plus 1/1 here. |

If a unit's after-set contains one of these and the before-set did not, apply that entry's class
procedure from the table above **before** calling it a regression — and for the harness-teardown entry,
read the FAIL text rather than re-running anything.

**Historical note, 2026-07-22 (RT14 differential run), superseded:**
`TestPasteInjectCommitBudget_IdleActivePane_HKukx` failed in the full suite and passed in isolation
once (1.5s), and was recorded as a load artifact on that basis. RT19.0's `-count=8` A/B on the
unmodified file later showed it is red at HEAD, so it now sits in the known-red class. The general
lesson is rule 3: **one isolated pass is not a classification.** It post-dates the original baseline
measurement (its file `pasteinject_hk9vp51_test.go` last landed at `5c1e0deb3`), which is why it was
absent from the table above. RT14 never touched that file — it covers the Working-phase no-commit
ceiling, which `dispatchsegment.go` places outside the RT8 segment boundary and which slice RT19c owns.

**Provenance of the 2026-07-23 additions (hk-8cxxb).** The nine `internal/daemon` names marked
`ADDED 2026-07-23` were surfaced across six full `./internal/daemon/` runs during RT19c — four on a
working tree, two on a detached clean worktree at HEAD — each observed failing in the full suite and
passing in isolation. `TestPasteInjectQuitOnCommit_NewCommitNoKill` and the `_IdleActivePane_HKukx`
reclassification come from RT19.0's A/B. Mechanisms above were derived by reading each test's source
in this pass; **no test was executed while writing this entry** — the box was at 3.1 GiB free, below
the 10 GiB watermark of Addendum §3, under which timing results are not trustworthy anyway.

**Provenance of the 2026-07-23 corrections (lane S).** Three changes, all originating in another
lane's full verification pass and all re-derived here from test source before being written down:

- `_PostQuitWatchdogKillsOnGrace` moved load-sensitive → known-red. **Independently corroborated**: the
  2s-ctx-vs-git-subprocess mechanism was read out of `pasteinject_hktrjef_test.go` and
  `pasteInjectQuitOnCommit` in `internal/daemon/pasteinject.go`, and three isolated runs reproduced it
  (2 FAIL / 1 PASS, both failures burning the full 2s with `want 1, got 0`). Caveat on those three
  runs: they ran in the shared working tree, where another lane holds an uncommitted edit to
  `internal/daemon/pasteinject.go` — that edit is confined to `bufferName`, which this path never
  calls, so the runs are equivalent to HEAD for this test, but they are not a clean-worktree
  measurement in the sense of Addendum §4.
- `TestDaemonStart_SocketBindsBeforeRestartBackoffSleep` added as load-sensitive. **Independently
  corroborated**: the 1.5s budget is `backoffBase/2` with `backoffBase = 3 * time.Second`, read from
  `restartbackoff_socketbind_hkuzvt9_test.go`. Isolated run passed here.
- `TestL2_SSHLocalhost_TmuxPaneID_hk8u2al` added as the first harness-teardown entry.
  **Independently corroborated**, including the `.zsh_history`-after-kill-server race reproduced by
  hand and the `c.Errorf("TempDir RemoveAll cleanup: …")` call confirmed in the Go source. The
  28/28 and 8/8 pass counts are carried on the reporting lane's word; the 1/1 is from here.

Box state for these runs: 22 GiB free, above the Addendum §3 watermark.

## Suite cost

`internal/daemon` alone is ~605s (~10 min) under load, ~70s for a 6-test filtered subset. Budget one
full serialized verification run per unit, not per edit. Use `go build ./internal/... ./cmd/...` and
`go vet ./internal/...` (both seconds) as the fast inner loop.
