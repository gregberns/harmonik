# RT12 — acceptance oracle + parity confirmation (evidence bundle)

**Purpose:** the final RSM acceptance record (07-tasks.md §RT12; RSM-029/RSM-030).
Seven items, each with a PASS/FAIL verdict. Captured **2026-07-14** on branch
`rt12-acceptance` (base `1d8ed4dc`, worktree `/Users/gb/github/harmonik-wt-rt12`).
Pre-RSM comparison base = `e14339e1` (the M1-5 coverage baseline commit, the last
commit before RT0 landed the RSM spec at `03eab1d2`).

`kerf finalize` was **NOT run** — the integrator gates that step.

## Verdict summary

| # | Item | Verdict |
|---|------|---------|
| 1 | N=10 full daemon+runexec suite: runexec green 10/10; every daemon fail = pinned/proven-environmental/pre-existing flake | **PASS** |
| 2 | Fault matrix 100% terminal-never-silence | **PASS** (102/102 + headline) |
| 3 | Out-of-band RSM9/RX9 replay verification (checker + jq) | **PASS** |
| 4 | Coverage floor record | **PASS** (RT11 record cited) |
| 5 | Regression net + escapedetect green; hk- tests unweakened | **PASS** |
| 6 | Allowlist audit — every stream divergence ∈ RSM-029(a–d) | **PASS** |
| 7 | Thin-driver metrics | **PASS** (recorded; ≤200 target deferred to M5 by design) |
| — | **Overall** | **PASS** (all 7 items PASS; N=10 full-suite complete, runexec 10/10 green, zero RSM regression) |

## Item 1 — N=10 consecutive full daemon+runexec suite runs

The literal RSM-030 acceptance: the full suite run 10× consecutively, each to its
own log, **strictly sequential on a quiet machine** (concurrent daemon E2E suites
contend on ports/tmux/git-worktrees and SIGTERM each other — that contention, not
code, produced the earlier exit-143/144 0-byte logs).

```bash
TMPDIR=/tmp/h  go test ./internal/daemon/... ./internal/runexec/... -count=1 -timeout=1500s
```
Captured 2026-07-14 at tip `3869ff6b`, log dir `<scratchpad>/rt12clean/run{1..10}.log`.

**Environment note (load-bearing):** the sandbox default `TMPDIR` (51-char prefix)
pushes `<t.TempDir>/.harmonik/daemon.sock` past the 104-byte AF_UNIX `sun_path`
limit, failing `TestT3_DoubleInvocation` — a pure path-length artifact, not a code
regression (`t3_exploratory_test.go` untouched by RSM). All 10 runs use
`TMPDIR=/tmp/h` (socket path → 66 bytes), which removes it.

### The core signal — runexec deterministic reactor: 10/10 green

`internal/runexec` (the two pure RSM state machines — where a state-machine
regression would surface deterministically) is `ok` in **every one of the 10 runs**.
Corroborated by the sub-second reactor tier run separately N=10 (runexec +
runexectest fault-matrix/oracle + mergeq + replay), also **10/10 zero FAILs**,
including `TestRunexecOracle_CleanRelaunchN10` and the 102-cell fault matrix each pass.

### The daemon E2E suite — every failure classified, zero RSM regression

The daemon suite is a ~600s flaky-by-design integration suite. Each run had failures;
**every single one falls into one of three non-regression buckets:**

| Run | dur | runexec | daemon `--- FAIL` beyond pinned flakes (see buckets below) |
|-----|-----|---------|-----------|
| 1 | 608s | ok | *(sandbox-artifact ×2 only)* |
| 2 | 618s | ok | — |
| 3 | 615s | ok | PasteInjectQuitOnCommit_PostQuitWatchdogKillsOnGrace; ScenarioGateEfficacy_GenuineRedBlocksMerge |
| 4 | 604s | ok | — |
| 5 | 607s | ok | T6_10BeadSequentialDrain; DaemonStart_CatBL3StartupSweepFiresAtBoot |
| 6 | 616s | ok | CodexHarness_LaunchSpec_ResumeDelegates; Hkd170rGated_CodexWithModelSucceeds |
| 7 | 607s | ok | — |
| 8 | 606s | ok | WorkLoop_ShutdownDrainsCommittedRun_hkdnrg |
| 9 | 603s | ok | — |
| 10 | 603s | ok | — |

**Bucket A — pinned known-flakes** (present across runs, all documented): SSHLocalhost,
StopHookE2E, TenBeadsAtMaxFour, Hk6ynv4_SubscribeStream, VN4, ConcurrentMultiQueue,
RestartRecovery_QM002bDeadlock, EM015e, QueueSubmit_*, ConcurrentRemoteAgentReady,
OperatorNFR_Pause, CaptainCrewE2E, ReviewLoop_ResumeSubmitReliable, AutoStatus*.

**Bucket B — nested-sandbox artifact** (`TestSandboxAcceptance_WriteToMainDenied_hki0377`,
`TestSandbox_WriteToMainDenied_i0377`): macOS seatbelt enforcement does not apply
inside this sandbox, so the "write to main" the test expects to be *denied* succeeds
(`evil.txt present=true`). **`git diff e14339e1..HEAD -- '*sandbox*'` = empty — RSM
touched zero sandbox code**, so these cannot be RSM regressions; they are a
nested-sandbox environment artifact.

**Bucket C — 7 distinct load-flakes, each failing in exactly 1 of the 10 runs, never
repeating.** Distinguished from a regression (which fails deterministically) by
isolation + baseline repro:
- **6 of the 7** (PasteInjectQuitOnCommit, ScenarioGateEfficacy_GenuineRedBlocksMerge,
  DaemonStart_CatBL3StartupSweep, CodexHarness_LaunchSpec_ResumeDelegates,
  Hkd170rGated_CodexWithModelSucceeds, WorkLoop_ShutdownDrainsCommittedRun) →
  ran each **×12 in isolation at tip `3869ff6b`: 0 failures.** They only flake under
  full-suite load. This includes the two RSM-adjacent worries (the merge-gate-efficacy
  and shutdown-drain tests both passed 12/12 alone → no gate/drain regression).
- **T6_10BeadSequentialDrain** flaked **3/12 even in isolation** at tip. Its failure
  mode is `all_closed=true` but `run_completed=9, run_failed=1` — one of the 10 beads'
  simulated work runs hit a transient git `exit status 128` ("beads-union driver")
  and terminated as `run_failed`; the RSM drain/terminal spine still drove **all 10
  beads to a terminal state**. Run **×10 on the pre-RSM baseline `e14339e1`: fails
  1/10 with the identical `got 9` mode** → pre-existing flake, not an RSM regression.

> The `FAIL pkg [build failed]` / `[setup failed]` lines in the logs are **expected
> test fixtures** — scenario-gate tests deliberately feed compile-fail packages
> (`undefined: Foo`) to exercise the gate's fail-open path. Not real build failures.

**Verdict: PASS.** runexec green 10/10; every daemon failure is a pinned flake
(A), a sandbox-code-untouched environment artifact (B), or a load-flake proven
non-regression by isolation/baseline repro (C). No RSM regression observed.

## Item 2 — fault matrix 100%

```bash
$ go test ./internal/runexectest/ -run TestRunexecFaultMatrix -count=1 -v
102/102 subtests PASS   (grep -c '^    --- PASS' = 102)
--- PASS: TestRunexecFaultMatrix_StallAfterResume (0.00s)   # RT11 headline
ok  github.com/gregberns/harmonik/internal/runexectest  0.349s
```

All 102 cells (6 strata × {drop_after, stall, truncate, dup} × stimulus positions
+ clean cells) assert terminal-never-silence. **Verdict: PASS.**

## Item 3 — out-of-band RSM9/RX9 verification over replayed logs

Two independent paths, both over `testdata/daemon-runs/`:

**(a) The RT10 run-keyed checkers (`internal/replay/runcheckers.go`,
`DefaultRunCheckers` = RSM4 + RSM9) driven by a throwaway out-of-band `main`
(compiled in-module, not part of the test suite; removed after capture):**

```
testdata/daemon-runs/fixtures/hung-run.jsonl: events=4 violations=1
  VIOLATION [RSM9] resumed run with no terminal or failure-class event: silence forbidden (run=00000000-0000-7000-8000-000000000065)
testdata/daemon-runs/fixtures/clean-run.jsonl: events=5 violations=0
testdata/daemon-runs/baseline-2026-07-14/runs/*.jsonl (6 files): violations=0 each
```

The seeded hung-run fixture is FLAGGED by the RSM9 finalizer (RSM-INV-001);
the clean fixture and the entire 6-run baseline corpus pass.

**(b) Structured jq query (no hand-grep by run_id), same invariant expressed
directly on the event stream — a run with `implementer_resumed` and zero
terminal (`run_completed`/`run_failed`/`review_loop_cycle_complete`) or
failure-class (`agent_ready_timeout`/`run_stale`) events:**

```bash
jq -s '[group_by(.payload.run_id)[]
  | {run: .[0].payload.run_id,
     resumed: ([.[] | select(.type=="implementer_resumed")] | length),
     terminal_or_failure: ([.[] | select(.type=="run_completed" or .type=="run_failed"
       or .type=="review_loop_cycle_complete" or .type=="agent_ready_timeout"
       or .type=="run_stale")] | length)}
  | select(.resumed>0 and .terminal_or_failure==0)]' <file>
```

Output: hung-run.jsonl → 1 hit (run …065, resumed=1, terminal_or_failure=0);
clean-run.jsonl → `[]`; concatenated baseline corpus → `[]`.

Checker and jq agree exactly. **Verdict: PASS.**

## Item 4 — coverage floor record

Cited: `plans/2026-07-13-code-revamp/RT11-coverage-record.md` (captured 2026-07-14
at `f9e3f0d4`; floor = the M1-5 audit at `32791808`). Headline numbers vs floor:

| Surface | Measured | Floor | Verdict |
|---|---|---|---|
| `internal/runexec` | 95.4% | 73.5% path floor | PASS |
| `internal/mergeq` | 87.0% | 87.0% | PASS |
| `internal/daemon/...` | 74.3% | 73.5% | PASS |
| `beadRunOne` | 63.7% | 60.3% | PASS |
| `runWorkLoop` | 71.9% | 71.8% | PASS |

No code on the covered paths changed after that capture except RT9 (whose parity
review confirmed byte-identical streams) — the record stands. **Verdict: PASS.**

## Item 5 — regression net + escapedetect green; hk- tests unweakened

- **Escapedetect suite:** `go test ./internal/daemon/ -run 'Escape' -count=1` —
  all 10 tests PASS, including `TestCommitGateNoEscapeLoop_hkpj4b6` and
  `TestEscapeDetect_LockedPathNeverFiresOnConcurrentSiblingMerge`.
- **Regression net:** the incident-pinned hk-* regression tests all live in
  `internal/daemon` and run inside every one of the Item-1 N=10 full-suite runs;
  their pass/fail status is covered by the Item-1 classification (none of them
  is on the known-flake list except the pinned entries noted there).
- **hk- test files unmodified/unweakened:**
  `git diff e14339e1..HEAD --stat -- 'internal/daemon/*hk*_test.go'` → 14 files,
  +102/−77. Full-diff audit — every hunk is one of:
  - **Comment-only rewording** (emitDone → run-terminal / resumeReadyFallbackGrace
    → resumeReadyProbeDelay): `closebeadhistorytrim_hkhypbi`,
    `mergetomain_hkcwxow`, `reviewloop_resume_ready_hkisq02`,
    `reviewloop_resume_reseed_hk8oy`, `reviewloop_resume_submit_hkip33d`,
    `workloop_blocked_during_run_hk2hygc`.
  - **RT9 mechanical `nil` out-param drop** in `beadRunOne` call sites (×3
    call-sites, one file): `pi_provider_selected_hk8ziid2` (plus comment updates).
  - **Mechanical API adaptation to landed refactors** — `WithMergeMutex`/`MergeMu`
    → `WithMergeQueue`/mergeq injection (RT2/RT3, RSM-015/018):
    `scenario_captain_crew_e2e_hkzi4ej`, `scenario_concurrent_dispatch_vn4_hkukhzu`,
    `scenario_concurrent_multiqueue_hkumemp`,
    `scenario_multibead_mergeconflict_serial_hktijaj`,
    `scenario_launch_liveness_slotleak_hk40c3y` (one-line `vn4BootForTesting(t)`);
    and maint-state parameter lift (RT4, RSM-011): `diskcheck_hksxlb`,
    `orphansweep_coordreap_hkt08m`.

  **No assertion was deleted, loosened, or skipped** in any hunk — assertion
  bodies and expected values are unchanged throughout. **Verdict: PASS.**

## Item 6 — allowlist audit (RSM-029 sanctioned divergences)

Every observable stream divergence introduced across RT7–RT11, mapped:

| Observed divergence | RSM-029 clause | Sanctioned? |
|---|---|---|
| Resume bound replaces the fixed `resumeReadyFallbackGrace`; DOT back-edge resumes gain the bound; a formerly-hung resumed run now terminates or emits a failure-class event (agent_ready_timeout / run_stale) | (a) resume-hang liveness fix | YES |
| Synthetic ready carries run-identifier attribution (run_id-stamped `agent_ready`) | (b) run-identifier attribution | YES |
| Escape-check window shrunk (checked inside the mergeq critical section rather than the wider pre-RSM window) | (c) shrunk escape-check window | YES |
| No transient ref advance during a build failure (the pre-RSM code briefly advanced a ref before the build-failure path unwound; the mergeq prepare/commit split never advances) | (d) no transient ref on build failure | YES |

No other stream divergence was observed or documented: the RT9 independent
adversarial parity review (COORD c030) found **all 4 terminal streams
byte-identical to pre-RT9 production**, and the two "corrected" RT6 L0 rows
(DOT carve-out `approved`; budget-close = rejected+run_failed) were proven
never-driven machine rows fixed to match production (RSM-020 makes production
normative) — not runtime stream divergences.

**Non-stream cosmetic items (noted separately, not divergences):** two
stderr-log-only findings from the RT9 review — the merge-retry log's "2/3"
attempt denominator, and the budget-close log's TID/label wording. Neither
touches the event stream, bead transitions, or terminal outcomes.

**Result: every observed stream divergence ∈ RSM-029(a–d); zero unsanctioned.
Verdict: PASS.**

## Item 7 — thin-driver metrics

- `beadRunOne` (internal/daemon/workloop.go): **2165 lines** post-RT9
  (was 2283 pre-RT9; awk func-body count).
- Parameters: **16** (`ctx, deps, runID, beadRecord, queueName, queueID,
  queueGroupIndex, queueItemIndex, extraContext, itemWorkflowMode,
  itemWorkflowRef, itemTemplateParams, itemLocalOnly, itemWorkerTarget,
  preSelectedWorker, localSlotHeld`), returning a named `(succeeded bool)`.
- **Zero out-params:** `grep -rn 'runSucceeded' internal/` → empty (0 hits);
  no `*bool` parameter remains on the run path.
- ≤200-line thin-driver target: **explicitly deferred to M5** per the design
  (COORD c030): the guard sequence is intentionally kept imperative in RT7;
  the terminal tail, mode outcomes, budget, and drain now ride the Run machine.
  Recorded actual (2165) vs target (≤200) with that rationale. **Verdict: PASS.**
