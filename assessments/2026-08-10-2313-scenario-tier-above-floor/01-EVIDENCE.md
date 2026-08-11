# Evidence

> Append-only, written as you go. Every result that will be cited in the verdict gets a row here
> first. Do not reconstruct this at the end — a record assembled from memory agrees with the verdict
> because the same mind produced both.

## The tree this was run from

| | |
|---|---|
| Checkout | `/Users/gb/github/harmonik-wt/bravo` (worktree of `/Users/gb/github/harmonik`) |
| Branch / HEAD | `work/bravo-reachability` @ `84ad44e20` |
| Safety check | `grep -c -- '--rev' scripts/scratch-daemon.sh` → `22` (22 = safe) |
| Working tree | `git status --porcelain` empty at 23:13 |
| Scratch clone | none — no scratch daemon this session; the scenario tier runs in-tree by design |
| Pinned revision | n/a; the tier stamps its own binaries, see below |

The scenario tier builds its binaries with an explicit `-ldflags -X main.commitHash=84ad44e2...`,
so the artifacts under test name the commit directly. Copied from the run log:

    go build -ldflags "-X main.commitHash=84ad44e20fcccc4ad8a990c2a65b4b16a820375a" -o /tmp/harmonik ./cmd/harmonik

No `+local-edits` suffix appeared anywhere, and the tree was clean before the build.

## Runs

| # | When | Command | Revision | Exit | Log | Notes |
|---|---|---|---|---|---|---|
| 1 | 23:14–23:25 | `make test-scenario` | `84ad44e20` | **2** | `scratchpad/scenario-run1.log` | **First run of this tier above the 10 GiB disk floor.** 23 GiB free throughout. 9 failures, 2 skips. Box load 10.89/13.61. |
| 2 | 23:26–23:38 | `make test-scenario` | `84ad44e20` | **2** | `scratchpad/scenario-run2.log` | Repeat, box load ~5.4. 6 failures, 2 skips. |
| 3 | 23:41 | `go test -race -tags=scenario -count=1 -run '^TestWorkLoop_HC056Timeout_ReopenAndRepickup$' ./internal/daemon/` | `84ad44e20` | 0 | inline | **ok 3.878s** — passes alone, vs a 20s deadline it blew in the tier |
| 4 | 23:41 | `go test -race ... -run '^TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency$' ./internal/daemon/` | `84ad44e20` | 0 | inline | **ok 13.695s** — passes alone, vs 60s |
| 5 | 23:43 | `go test -race ... -run '^TestScenario_Codex_EmptyModel_FullLifecycle$' ./test/scenario/` | `84ad44e20` | 1 | inline | **FAILS alone**, 4 of 4 overall → `hk-5ji8t` |

**The exit code is read from inside the log, not from the runner.** Run 1's harness reported the
command "completed (exit code 0)" because the pipeline ended in `tail`. The tier's own line is:

    EXIT_LINE rc=2

This is the `Background runs lie about exit codes` trap from the corpus README, hit live. It would
have recorded a red tier as green.

### Run 1 — per package

    ok   cmd/harmonik          20.683s
    FAIL internal/daemon      587.064s      (8 failures)
    ok   internal/keeper       12.320s
    ok   internal/runloop      10.361s
    ok   internal/sentinel     11.088s
    FAIL test/scenario         90.800s      (1 failure)
    scenario skips: 2

### Run 1 — the 9 failures, and the shape they all share

| Test | Elapsed | Message |
|---|---|---|
| `TestStripRunContext_NeverLandsOnMain` | 41.56s | `CloseBead call count = 0; want ≥ 1` |
| `TestMergeToMain_NonFFReopen` | 40.94s | `ReopenBead call count = 0`; no `outcome_emitted` |
| `TestT4RealDB_ConcurrentClaimExclusion` | 32.09s | `bead t5-2va was not closed within 20s` |
| `TestScenario_CommitGateCapTerminates_hki8g59` | 61.33s | `cascade did not terminate within budget` |
| `TestWorkLoop_HC056Timeout_ReopenAndRepickup` | 30.79s | `timed out waiting for re-pickup` |
| `TestMergeToMain_PreCommittedWorktreeAndIdleAgentStillFails` | 31.02s | `timed out waiting for bead close/reopen` |
| `TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency` | 60.56s | `timed out ... closed=3` of 10 |
| `TestT2_RunFailedEventContainsExitCode` | 60.84s | `timed out waiting for reopen` |
| `TestScenario_Codex_EmptyModel_FullLifecycle` | 28.16s | (`test/scenario`) |

**Every one is a wall-clock deadline, and not one is a failed logical assertion.** Read from the
sources:

    mergetomain_hkftyvo_test.go              WithTimeout(t.Context(), 30*time.Second)
    mergetomain_stripruncontext_hk4je_test.go WithTimeout(t.Context(), 30*time.Second)
    t5_realdb_concurrent_test.go             WithTimeout(..., 15s) and (..., 30s)
    workloop_hc056_reopen_test.go            WithTimeout(t.Context(), 20*time.Second)
    t2_scenarios_test.go                     WithTimeout(context.Background(), 3*time.Second)
    scenario_commit_gate_cap_hki8g59_test.go WithTimeout(t.Context(), 60*time.Second)

Each elapsed time sits at or just above its test's own deadline. The `CloseBead call count = 0`
and `ReopenBead call count = 0` messages are not a different class: they are what the assertion
prints AFTER its wait context expired.

`scenario_commit_gate_cap_hki8g59_test.go` is worth naming, because its message reads like a
regression and is not one on this evidence. It says `the traversal cap did NOT bound the
implement↔commit_gate loop (infinite-loop regression)`, but its budget is a 60s clock that its own
comment calls "the safety net", and its event trace shows only TWO completed implement passes
against a cap of 3. That is the cap working and the box being slow, not an unbounded loop.

### Run 1 — the disk guard did NOT fire

This had to be checked before anything else, because `LP-014` says any test that waits for a bead
to close is testing the disk, and the log carries 306 `dispatch paused` lines.

    grep -c "dispatch paused"                                   → 306
    disk-check lines NOT from a DiskLow/AdmissionOrder fixture  → 0

All 306 come from tests that inject the value on purpose — `TestDiskLowBranch*`,
`TestAdmissionOrder_DiskLowLatchSkipsClaim` — with synthetic readings of `available=0MiB`,
`available=10239MiB` and `available=4398046511104MiB`. **Not one pause came from a real reading.**

Free space sampled every 20s for the run's duration, 40 samples, 23:15:50 → 23:28:51:

    min 22134 MiB   max 23169 MiB   watermark 10240 MiB

The floor was never approached — the minimum sample is more than double the watermark. Note the
contrast with `LP-014`, where the number fell measurably across three tests because the run was
starting from 9.8 GiB; with this much headroom the tier's own consumption does not matter.

So the disk floor is genuinely cleared, and `hk-scenario-tier-nondeterministic-xt1wa`'s stated
precondition is now satisfied. Disk is no longer an available explanation for these failures.

### Run 1 — two `FAIL ... [build failed]` lines that are NOT failures

    FAIL	pkg [build failed]
    FAIL	pkg [setup failed]
    FAIL	scenariopkg.test/scenariopkg [build failed]

These are fixture strings printed INSIDE passing tests
(`TestClassifyScenarioGateError_CompileFail`, which asserts the scenario-gate classifier reads
compile failures correctly). All three tests PASS. A grep for `^FAIL` counts them as package
failures and overstates the damage — worth knowing before anyone greps this log.

Packages carrying `//go:build scenario` files, printed by the target itself:

    ./cmd/harmonik  ./internal/daemon  ./internal/keeper  ./internal/runloop
    ./internal/sentinel  ./test/scenario

The tier runs them as one `go test -v -race -tags=scenario -timeout 10m` invocation.

## The mechanical fact this run rests on

`LP-013` states that `make test-scenario` "sits behind `lint-allow`". Read from the `Makefile`:

- `test-scenario` is its own `.PHONY` target. Its only prerequisite is `build-all`.
- `full` orders the steps `gate-static → gate-test-compile → tests → lint-allow →
  test-subprocess → test-scenario → module-hygiene`.

So `lint-allow` aborts `full` before `test-scenario` is reached, but it does not block the target.
**The tier was never blocked. It was unreached by the one command anybody ran.** Both stated
blockers on `hk-scenario-tier-nondeterministic-xt1wa` — the lint failure and the disk floor — were
therefore clear at the moment this session started, and the bead's precondition ("someone re-runs
above 10 GiB free") was satisfiable with a single command.

## Conditions

Things that invalidate a timing result if they were true while it ran. Check before and after.

| | Before (23:14) | After |
|---|---|---|
| `df -m /System/Volumes/Data` | 23144 MiB free | *pending* |
| Sub-agents active in this tree | none | none |
| **Other load on the box** | **yes — a second lane was running `make fast` in `/Users/gb/github/harmonik` from 23:12** | *pending* |

The concurrent `make fast` is recorded because it matters to the question the bead asks. This is a
multi-lane box, so a `-race` tier competing with another lane's build is the NORMAL condition here,
not a spoiled measurement. Any timing-sensitive failure in this run has to be weighed against it.

Free disk sampled every 20s for the duration (`scratchpad/disk-during-scenario.log`). First four
samples: 23144 → 23169 → 23155 MiB. **Flat.** This contradicts nothing in `LP-014`, which measured
the drop under a `make full` starting at 9.8 GiB — with 23 GiB of headroom the tier's own
consumption does not move the number meaningfully.

## NOT RUN

- `make full` — not run this session. `lint-allow` is known-red on `hk-pw1wv` (alpha's), so a full
  pass would abort before the tier and measure nothing new.
- `make core` — not run. No gate is being issued, so the HARD gate is not in scope.
- LT (`make core-loop-lt`) — not run. No gate.
- Everything the scenario tier itself skips: to be copied from the run's own `scenario skips: N`
  line and the `--- SKIP:` entries. *pending*

## Reproduce it

    cd /Users/gb/github/harmonik-wt/bravo        # any checkout at 84ad44e20, clean tree
    df -m /System/Volumes/Data                   # confirm > 10240 MiB free FIRST
    out=$(make test-scenario 2>&1); rc=$?; echo "rc=$rc"

## Run 2 — result

    ok   cmd/harmonik          (cached)
    FAIL internal/daemon      544.631s      (5 failures)
    ok   internal/keeper       (cached)
    ok   internal/runloop      (cached)
    ok   internal/sentinel     (cached)
    FAIL test/scenario         77.872s      (1 failure)
    scenario skips: 2
    EXIT_LINE rc=2

**Caveat on the comparison, and it matters.** Four of the six packages report `(cached)` — Go
reused run 1's results because nothing changed. Only `internal/daemon` and `test/scenario` were
genuinely re-run. Every failure in both runs lives in those two packages, so the failure-set
comparison is sound; but run 2 is NOT an independent re-measurement of the four that passed.

Failures: `TestBootSweep_DoesNotRemoveTheWorktreeOfARunThatOutlivedTheDaemon` (3.18s),
`TestWorkLoop_HC056Timeout_ReopenAndRepickup` (21.58s),
`TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency` (60.25s),
`TestParallelSmoke_TwoBeadsConcurrent` (21.04s), `TestThroughput_TenBeadsAtMaxFour` (67.90s),
`TestScenario_Codex_EmptyModel_FullLifecycle` (26.92s).

Real (non-fixture) disk pauses in run 2: **0**. Load fell from 9.15 to 5.38 over the run's start.

### The intersection

|  | Run 1 (load 10.89) | Run 2 (load ~5.4) |
|---|---|---|
| Failures | 9 | 6 |
| **Failed in both** | `HC056Timeout_ReopenAndRepickup`, `ClaimSemaphore_BoundsClaimConcurrency`, `Codex_EmptyModel_FullLifecycle` | |

`TestScenario_CommitGateCapTerminates_hki8g59` failed in run 1 and PASSED in run 2 — which settles
the "infinite-loop regression" question its message raises. It is a slow box, not a broken cap.

`ClaimSemaphore` is worth one more line: it closed 3 of 10 beads in run 1 and 9 of 10 in run 2,
reporting `peak_concurrent_claims=1` both times. The varying count is the starvation; the constant
`peak=1` is the test's own fixture shape, not a finding.

## Live legs — what actually went through the process

| | |
|---|---|
| Work driven through the loop | **None.** No scratch daemon, no real agent, no dispatched bead this session. |
| Cases re-run from `test/exploratory/cases/` | `LP-013` (its premise refuted — see findings), `LP-014` (its disk mechanism confirmed absent above the floor) |
| New cases written this session | `LP-018` — protocol: is this suite red because the code is broken or the box is busy |

**This session drove no live dispatched run, and that is the honest gap in it.** The tier is an
in-process test suite, not the daemon under real work. The handoff's standing next step — drive one
real bead end to end on a scratch daemon — is still not done.

### Observations

| Question asked | What each surface said | What was actually true |
|---|---|---|
| Did the tier pass? | The task runner: "completed (exit code 0)". | `EXIT_LINE rc=2`. The pipeline ended in `tail`. |
| Did the daemon pause on disk? | 306 `dispatch paused` lines in the log. | Zero real pauses. All 306 injected by fixtures. |
| Did three packages fail to build? | Three `FAIL ... [build failed]` lines. | None. Fixture strings inside passing tests. |
| Why did the codex run fail? | The test: "the codex shim likely rejected a leaked `--model`". | `dot: traversal cap hit at node "commit_gate"`. No `--model` leak. |
| Is the traversal cap regressed? | The test: "the traversal cap did NOT bound the implement↔commit_gate loop (infinite-loop regression)". | Cap fine; test passed in run 2 on a quieter box. |

**Five surfaces, five wrong answers, all of them confident.** That is the finding of this session
more than any individual bug.

---

## CORRECTION — 2026-08-11 00:05

> Added after the entries above, which are left exactly as written. The record is not edited to
> agree with what was learned later.

**The claim withdrawn:** that run 2 ran on a quieter box ("load ~5.4"), and that the 9-vs-6 failure
counts correlate with load. Both the run-2 row in the Runs table and the intersection table state
this. Both are wrong.

**What the full sample shows.** The load figure was taken from the opening samples of run 2. The
complete 60-sample series says:

    run 2 load: min 3.19, max 34.24, 9 of 60 samples above 15
                peak at 23:34:43, mid `internal/daemon`
    run 2 free: min 19889 MiB, max 22397 MiB

Run 1 has only a single `uptime` reading near its end (10.89 / 13.61) — it was sampled for disk,
not load. So **run 2 peaked higher than anything measured during run 1**, and the "quieter box"
framing is backwards. The tier generates its own load; neither run was quiet, and there is no clean
low-load measurement of this tier in this record.

**What survives.** The starvation conclusion does not rest on the load comparison. It rests on runs
3 and 4 in the table above: the two load-varying stable failures pass ALONE in 3.878s and 13.695s
against deadlines of 20s and 60s that they blew inside the tier. That margin is the evidence, and
it is independent of what `uptime` said.

**What does not survive.** The 9-vs-6 difference is unexplained, and this record should not be read
as explaining it. Two runs is a small sample for any claim about which tests are stable, so the
intersection of 3 is a this-tree-this-night result rather than a standing list.

**Disk is unaffected.** Run 2's minimum free space, 19889 MiB, is still nearly double the 10240 MiB
watermark, and the zero-real-pauses result holds for both runs.

**Method note worth carrying forward.** Sampling a box for load at the start of a run and quoting
that number for the whole run is how this error happened. `uptime` at one instant does not describe
an eleven-minute parallel test run, and on a box whose load the run itself creates, the opening
sample is the least representative one available. LP-018 has been corrected so it no longer teaches
the load correlation as a diagnostic step.
