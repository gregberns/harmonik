# P2 test oracle — measured baseline (2026-07-22)

**Why this file exists.** Every P2 unit's release gate says "full test suite green" (`_plan.md` §5.3).
That gate is **not usable as written**: `internal/daemon` is already red before any extraction work.
This file records the measured baseline so each unit can be verified against a *differential* oracle —
**no NEW failures** — instead of an unreachable zero.

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
| `TestPasteInjectQuitOnCommit_PostQuitWatchdogKillsOnGrace` | PASS | PASS | Flakes only under full-suite load |
| `TestT6_10BeadSequentialDrain` | PASS | PASS | Flakes only under full-suite load |

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

## Known-flaky allowlist (as of 2026-07-22)

```
# confirm these by re-running IN ISOLATION (isolated pass => load artifact)
TestThroughput_TenBeadsAtMaxFour                            # hard pre-existing, out of P2 scope
TestScenario_Hk6ynv4_SubscribeStream_EndToEnd               # load-sensitive
TestStopHookE2E_TwinRelayFastPath                           # load-sensitive
TestStopHookE2E_TwinRelayWaitGrace                          # load-sensitive
TestPasteInjectQuitOnCommit_PostQuitWatchdogKillsOnGrace    # load-sensitive
TestT6_10BeadSequentialDrain                                # load-sensitive
TestPasteInjectCommitBudget_IdleActivePane_HKukx            # load-sensitive; ADDED 2026-07-22 (RT14 run)
TestWorkLoop_ShutdownDrainsCommittedRun_hkdnrg              # load-sensitive; ADDED 2026-07-22 (RT15 run)
TestT2_ExitZeroNoSignal                                     # load-sensitive; ADDED 2026-07-22 (RT15 run)
TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency          # load-sensitive; ADDED 2026-07-22 (RT15 run)
TestM4C7_D2Chokepoint_IsHarnessAgnostic                     # hard pre-existing (build-tagged fixture compile); out of P2 scope

# confirm this one by re-running IN THE FULL SUITE (isolation is what breaks it)
TestMergeToMain_RealConflictWithBeadsLedger_Escalates       # ISOLATION-sensitive; pre-dates P2

# non-deterministic under parallel load; now in internal/harness/codex
TestCodexHarness_LaunchSpec_ResumeDelegates                 # flaky
TestCodexHarness_LaunchSpec_InitialDelegates                # flaky
TestCodexHarness_LaunchSpec_CustomBinary                    # flaky

# any dispatch/throughput/timing failure while `df -h` shows <10 GiB free — see Addendum §3
```

If a unit's after-set contains one of these and the before-set did not, **re-run that test in isolation**
before calling it a regression. Isolated failure = real; isolated pass = load artifact.

**Added 2026-07-22 during RT14's differential run:**
`TestPasteInjectCommitBudget_IdleActivePane_HKukx` failed in the full suite and **passed in
isolation** (1.5s), so it is a load artifact by this file's own rule, not a regression. It post-dates
the original baseline measurement (its file `pasteinject_hk9vp51_test.go` last landed at `5c1e0deb3`),
which is why it was absent from the table above. RT14 never touched that file — it covers the
Working-phase no-commit ceiling, which `dispatchsegment.go` places outside the RT8 segment boundary
and which slice RT19c owns.

## Suite cost

`internal/daemon` alone is ~605s (~10 min) under load, ~70s for a 6-test filtered subset. Budget one
full serialized verification run per unit, not per edit. Use `go build ./internal/... ./cmd/...` and
`go vet ./internal/...` (both seconds) as the fast inner loop.
