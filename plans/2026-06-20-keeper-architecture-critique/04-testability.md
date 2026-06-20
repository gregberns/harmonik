# 04 — Testability / QA Critique of the Session-Keeper

**Lens:** "Is everything imperatively tied together causing an inability to test, which
leads to consistent failure?"

**Verdict (one line):** The hypothesis is **REFUTED as stated, but points at the real
defect**: the keeper is *exhaustively seamed and testable* — the gate logic is
near-fully unit-tested deterministically — yet the **side-effect surface that actually
fails in production (the real tmux paste/submit timing and the CLI→watcher hand-off) is
testable-but-untested by default**. The failures are not caused by untestability; they
are caused by **tests that mock the one thing that breaks**, plus the only real-tmux test
being dark (`//go:build integration`, Tier-3 `check-full` only) AND aimed at a
cooperative fake instead of a real Claude REPL.

---

## 1. The seams exist — and they are excellent

`CyclerConfig` (`internal/keeper/cycle.go:34-187`) is a textbook dependency-injection
surface. **Every** side effect is an injectable function with a production default wired
in `applyDefaults()` (`cycle.go:195-308`): `InjectFn`, `ReadGaugeFn`, `CrispIdleFn`,
`HoldingDispatchFn`, `WriteJournalFn`/`ReadJournalFn`, `SetManagedSessionFn`,
`SetTmuxEnvFn`, `OperatorAttachedFn`, `SleepingCheckFn`, `ForceRestartFn`,
`SendEscapeFn`, `CycleIDGen`, `ClearPrecompactTriggerFn`, etc. Time is handled via
context (`sleepCtx`, `injector.go:137`), not naked `time.Sleep`, in the core path.

The watcher mirrors this: `ReadManagedSessionFn`, `WriteManagedSessionFn`, `ReadSidFn`,
`LiveRecoverFn`, `PollInterval`, `Staleness`, `IdleQuiesce` are all injectable
(`watcher_test.go:104-128`, `:455-460`). The default suite runs **green, deterministic,
4.0s, 74.4% coverage**, no real tmux, no flaky sleeps.

So the operator's "imperatively tied together → can't test" premise is **factually
wrong for the decision logic**. This is some of the most seam-rich code in the repo.

## 2. But the seams stop exactly at the failure point

Per-function coverage in the **default** suite (the one CI runs) tells the story:

| Function (`internal/keeper/injector.go`) | Coverage | What it is |
|---|---|---|
| `InjectText` (real tmux load-buffer→paste→Enter) | **10.5%** | only the empty-target guard runs |
| `sendEnter` (the submit Enter — hk-89g race) | **0.0%** | **never executed** |
| `SendEscapeKey` (busy-pane preempt — hk-qoz) | **0.0%** | never executed |
| `InjectWrapUpWarning` / `InjectOnDemandRestartWarning` | **0.0%** | never executed |
| `SetTmuxEnv` (HARMONIK_AGENT inherit) | 66.7% | error branch only |
| `sleepCtx` | 100% | the one piece that *is* unit-tested |

The injector unit test (`injector_test.go`) covers **only** (a) the empty-target guard,
(b) `sleepCtx` cancellation, and (c) three constant-value assertions
(`TestInjectText_SettleConstants` checks `submitSettle==750ms`, `submitRetries==2`,
`submitRetryDelay==400ms`). It **asserts the timing magic numbers but never executes the
paste/settle/Enter/retry sequence those numbers govern.** That sequence —
load-buffer → paste-buffer → 750ms settle → send-keys Enter → 2× retry — is the
*literal* production failure (memory: "local-tmux-paste /clear works but is UNRELIABLE —
TIMING, not executability"; hk-89g: "the line sits unsubmitted"). It has **zero unit
coverage**. This is the canonical anti-pattern: the suite mocks the very thing that
breaks (`cycleSpyInjector.inject` just appends the text to a slice; `cycle_test.go:15-33`).

## 3. The one real-tmux test is dark *and* aimed at a cooperative fake

`cycle_twin_e2e_integration_test.go` (45 KB) IS a faithful real-tmux test: real
`keeper.InjectText`, real `keeper-statusline.sh` pipeline, real `.ctx`/`.idle`/`.managed`
markers, real `tmux load-buffer`/`paste-buffer`/`send-keys`. Two problems:

1. **It does not run by default.** Tag `//go:build integration`; Makefile only runs
   `-tags=integration` under **Tier-3 `check-full`** (`Makefile:234-236`), and it
   `t.Skip()`s when tmux is absent. So on every normal `go test`/CI tier it contributes
   nothing.
2. **It tests a cooperative twin, not a real Claude REPL.** The "session" is
   `cmd/harmonik-twin-session`, a fake that, by its own header, "parses that real
   multi-line shape natively" and is built to *succeed*. The timing-race the production
   pane exhibits (a real Claude REPL still ingesting a bracketed paste when the Enter
   lands) is **structurally absent** — the twin reacts on a clean trigger match. So even
   the real-tmux test cannot reproduce the production timing failure; it validates the
   wire contract, not the race.

The offline reactive harness (`cycle_reactive_harness_test.go`) is a genuinely good
addition — it closes the *causal* loop (proves `/clear` → SID-flip is caused by the
inject, not time-faked) — but injection is still "a plain Go function call — no tmux."

## 4. Known-failure → regression-test map

| Known failure signature | Regression test? | Where |
|---|---|---|
| session-id flips/re-mints on `/clear`; must re-resolve | **YES (good)** | `waitForNewSessionID` exercised in `cycle_test.go`, `cycle_reactive_*`; `TestWatcher_AdoptsSameAgentNewSidAfterExternalClear` (`watcher_test.go:480+`) |
| stale `.managed` / foreign_session swallow | **YES (good)** | `TestWatcher_IgnoresForeignSessionGauge` (`watcher_test.go:427-471`) — deterministic, in-process |
| gauge not wired for crews | **NO (default)** | only `*_1m_gauge_integration_test.go` etc., all `//go:build integration`; no default-tag crew-gauge test |
| paste/submit **timing** unreliable (hk-89g) | **NO** | constants asserted; sequence never run (`sendEnter` 0%); real timing only in the cooperative twin |
| `restart-now` silent no-op / false-success when no keeper alive (bug B) | **NO — test asserts the no-op path** | `keeper_restart_now_hk4zy9_test.go` checks `runKeeperRestartNow→exit 0→ReadRestartNowMarker`; i.e. it verifies the marker was *written*, **never that it is consumed or that a keeper exists**. No liveness assertion. |
| CLI→watcher end-to-end consume of restart-now | **NO** | per prior investigation README: "the two halves meet only at the struct shape"; consume side uses an injected in-memory stub, not a disk marker the write-side wrote |
| `restart-now --project` arg-ordering exit 2 (bug A) | partial | `keeper_cmd_parser_parity_hkt1wd_test.go` (4 tests) — but bug A reproduced live, so parity coverage is incomplete |
| bash hooks (`keeper-statusline.sh` jq / `[1m]`-window inference) | **NONE** | zero bash-hook tests; silent regression risk |

## 5. Is "~28 test files" hiding low coverage of failure-prone paths?

Partly. The file count is real and the **gate-logic** coverage is genuine (74.4% of
`internal/keeper`, 3.3:1 test:code). But the volume is concentrated on the **cheap,
pure, branch-heavy** surface — threshold math, anti-loop gates, boot-grace, journal
phase transitions — because that surface is trivially unit-testable. The **expensive,
impure** surface (real paste timing, hooks, CLI↔watcher↔real-Claude) is where the file
count thins to a single dark integration test against a cooperative fake. So yes: a
healthy aggregate number masks ~0% effective coverage of the production-failure paths.

Secondary smell: the gate logic is *so* branch-heavy (boot-grace + MaxBootGraceTotal +
force-retry-interval + anti-loop-suppression + same-SID/novel-SID/force-threshold
permutations across `MaybeRun` AND `RunForPrecompact`) that the tests largely re-derive a
combinatorial truth table. That is testability working — but it is also evidence the
*design* accreted one injectable escape-hatch per incident (hk-4f8, hk-ibb, hk-hz9,
hk-qoz, hk-uxu…), which is the complexity critique's domain, not testability's.

## 6. Conclusion

- **Untestability is NOT a root cause.** The seams are present and the deterministic
  suite is strong for the decision logic.
- **The failures are testable-but-untested**, with one aggravating factor: where a test
  *does* exist for a failing path it **either mocks the failing mechanism**
  (`cycleSpyInjector`), **asserts the no-op as success** (`restart-now` marker-written),
  or **runs only in a dark tier against a cooperative fake** (twin e2e).
- **Highest-leverage fixes** (surgical, no rewrite):
  1. A default-tag test that drives the **real `InjectText` paste/settle/Enter/retry**
     sequence against a faked-exec or PTY harness so `sendEnter`/settle timing is
     exercised — currently 0%.
  2. An **end-to-end CLI→disk-marker→watcher-consume** test that fails when no keeper is
     live (covers bug B / the operator's actual pain), replacing the current
     marker-written-equals-success assertion.
  3. **Promote the twin e2e into a tier that actually runs**, and add a variant whose
     fake REPL injects realistic ingest latency so the paste-timing race can regress
     visibly.
  4. **Add bash-hook tests** (jq path, `[1m]`-window inference) — currently zero.
