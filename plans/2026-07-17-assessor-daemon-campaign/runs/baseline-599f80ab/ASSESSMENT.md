---
schema_version: 2
spawned_by: admiral
pass_id: baseline-599f80ab
pass_kind: baseline
pin_sha: 599f80ab7f8fc7ef5169db8e37210b04aeb5ccb3
pin_branch: phase1-session-restart-substrate
release_sha: 9152ea5c   # = pin + test-only (4 keeper scenario_*_test.go, 795 ins, ZERO product source; admiral diff-verified)
prior_baseline: a0591ba3 (PASS)
measured_against: good-enough-principles.md
verdict: PASS
verdict_kind: PASS (authoritative baseline)
completed: 2026-07-19T03:24:44Z
---

# Baseline ASSESSMENT — pin 599f80ab (release 9152ea5c = pin + test-only)

## VERDICT: **PASS**

**One-line rationale (reasoned judgment, NOT a bead tally):** Across all four gate legs plus the regression
green-tree, there is **no P0, no product regression, and no gating bucket-(1) finding**. Every claimed-done acceptance
item reconciles to a real commit/diff/test. The single deterministic test red (daemon Throughput) was independently
proven a load artifact, not a product defect. Residual findings are all bounded, non-core-reaching, or pre-existing —
Tier-1/Tier-2, tracked, non-gating. Verdict is my reasoned judgment over the artifacts, not a row count; the admiral
owns the release call.

---

## Legs (D1 delegation — I orchestrated subagent legs, then folded their evidence into this judgment)

| Leg | Result | Load-bearing evidence (assessor-verified) |
|---|---|---|
| **§4A HARD GATE** (isolation preflight) | **PASS** | fails=0; binary `--version` == pin (anti-stale); daemon dry-boot binds to sandbox dir + socket; S6 watcher armed, **0 hits entire campaign** (fd 13 / th 15–16 flat). |
| **LT** — live core-loop (`make core-loop-lt`) | **GREEN** | `MATRIX_JSON gate:true all_green:true` (pi:local end-to-end); gap1/gap3/gap4/t10 pass; mid-run structural failure AUTO-REOPENED → cell resolved green (workloop.go recovery correct). Exercises the dispatch delta. |
| **CR** — cold diff review (a0591ba3..599f80ab) | **CLEAN** | No P0/P1. Sole P2 (F1 codexdriver ResidentSession.Close drain-order) is **LATENT** — assessor independently confirmed `NewResidentSession` has ZERO production callers (grep: def + test only). Dispatch wiring, slot/lease, keeper gating all confirmed correct. |
| **XT** — adversarial break-testing | **SURVIVED** (no P0) | codexdriver FIFO flood/close-race, watchdog always-fail, codexwire malformed handshake, claude CONFIG_DIR isolation all survived under `-race`. **Finding 4 (keeper gauge-drop) REFUTED** by assessor's executed verification — intended exactly-one-clear behavior (TestZJ1Y), admiral's busy-pane case preserved. |
| **Regression green-tree** | **GREEN-with-known-issues** | 5 target pkgs green outright; keeper green (1 ms-timer heartbeat flake, 0/20 isolated); all 5 daemon reds are load/timing flakes with invariants intact. See below. |

---

## Claimed-done reconciliation (first-class duty — artifacts over the ledger)

The delta `a0591ba3..599f80ab` (keeper + codexdriver + daemon claudelaunchspec/projectconfig/crewidlereap/**workloop
isolation-guard** + workspace + sessioncapture; `go build ./...` CLEAN) was reconciled leg-by-leg against real
commits/diffs/tests:

- **keeper gauge-drop gating (hk-u7j83, HEAD commit under pin):** claim = suppress defensive re-inject only on gauge-drop.
  Verified against `step.go:431` predicate (`ev.CF != nil && belowActThreshold`) + `TestZJ1Y…ExactlyOneClear` — claim
  holds; the admiral's busy-pane (nil-CF / at-or-above-act) case still fires the needed re-inject. **Reconciles.**
- **codex isolation-boundary guard (hk-5h759, workloop.go):** claim = fail-closed when a codex crew has no ssh-worker
  boundary. Verified against the diff myself — the guard is gated on `deps.codexRequireIsolationBoundary` (set iff
  `HARMONIK_SUBSTRATE=codexdriver`), sits BEFORE worktree/tunnel setup, and does NOT touch the retry/emission path. In the
  current pi/claude runtime the flag is false → inert. **Reconciles; no dispatch regression.**
- **codexdriver resident-session (G4a/G4b):** claim = pre-spawn WAL-guard + proactive watchdog. Verified inert in
  production (no `NewResidentSession` caller) — scaffolding for a future codex cutover, not live. **Reconciles as latent.**
- **T8/T9 keeper work (6zbg1, qji8g):** release SHA 9152ea5c adds 4 keeper scenario tests (795 ins, ZERO product source,
  admiral diff-verified). Baseline at 599f80ab is authoritative for 9152ea5c. **Reconciles — test-only advance.**

No claimed-done item lacks a corresponding commit/diff/test. No previously-fixed corpus bug regressed.

---

## Regression green-tree detail (the last leg; the deterministic red is the one that mattered)

Per-package (`go test ./internal/<pkg>/... -count=1`, default TMPDIR, isolated GOCACHE): keeper / daemon-subpkgs
(bootconfig, router) / codexwire / codexdriver / workspace / sessioncapture / core — **green** (daemon whole-pkg run hit
the 10-min timeout under this box's resource pressure; per-test isolation is the reliable signal, `go build ./...` clean).

Six reds, all classified:

| Red test | Class | Determinism | Assessor disposition |
|---|---|---|---|
| keeper `Heartbeat_KeepsLiveGaugeFresh` | ENV/LOAD-FLAKE | 0/20 isolated | ms-timer slip under load; gauge-refresh invariant intact. Non-gating. |
| daemon `ProcessGroupKillsOrphanedGrandchild` | ENV/LOAD-FLAKE | 0/5 isolated | red only in the timed-out overloaded run. Non-gating. |
| daemon `StopHookE2E_TwinRelayFastPath` | ENV/LOAD-FLAKE | 0/5 isolated | 18s E2E starved under load. Non-gating. |
| daemon `StopHookE2E_TwinRelayWaitGrace` | ENV/LOAD-FLAKE | 0/5 isolated | 20s E2E starved under load. Non-gating. |
| daemon `Throughput_TenBeadsAtMaxFour` | ENV/LOAD-FLAKE (deterministic-under-sustained-load) | 5/5 (box always loaded) | **NOT a regression — assessor-verified.** 16 vs 10 `run_started` = legit re-dispatches under CPU starvation; every envelope has a DISTINCT run_id (`envelope.run_id == payload.run_id`, no double-emit). `workloop.go` delta is the inert codex guard + a struct realign — never touches the retry path. The `exactly 10` assertion encodes a spare-CPU-headroom assumption. Non-gating. |
| daemon `OperatorNFR_PauseWithRunInFlight` | ENV/LOAD-FLAKE | 1/5 isolated | `pausing`/`paused` emitted within 1ms, observed swapped; all drain/resume/dispatch-restore invariants PASS. Non-gating (same subscribe-observer ordering class as hk-xrn8r). |

---

## hk-xrn8r disposition (admiral asked me to fold this in)

`TestScenario_Hk6ynv4_SubscribeStream_EndToEnd` — **admiral gating bucket (3): TEST-DEFECT, non-gating, P2.**
Root cause: the live subscribe/observer stream is dispatched per-emit via fresh goroutines (`busimpl.go:650`, documented
`:74-78`) and does NOT guarantee inter-event order; only the durable JSONL is emit-ordered (appended under lock before
dispatch), and the real causality tool asserts over that file (`causality.go:44-47`). The test over-asserts strict order
on the socket surface (`subscribe_scenario_hk6ynv4_test.go:175-178`). Flake rates: 599f80ab 5/10 vs a0591ba3 7/10 —
**same signature, PRE-EXISTING (worse at the older baseline), NOT delta-introduced.** Product delivers correctly.
Fix: assert the event SET on the live stream, route ordering via `causality.go`. **Needs a named test-fix owner.**

The daemon `OperatorNFR_Pause` red above is the same subscribe-observer ordering class — one shared test-robustness
theme, not a product-ordering bug.

---

## Findings table (evidence for the record — NOT the gate)

| Bead | Finding | Sev | Tier | Disposition |
|---|---|---|---|---|
| hk-mlkec | F1 codexdriver `ResidentSession.Close` sets closed before drain → buffered submits get ErrResidentClosed | P2 | 1 | LATENT — no production caller; non-gating known-issue, needs owner |
| hk-sf7yp | F5 T8 residual TOCTOU: `stepAwaitModelDone→stepEnterClearing` operator-attach window uncovered | P3 | 1 | non-gating known-issue; propose T8 owner (6zbg1) to close the residual edge |
| hk-2hpeh | F7 scrub greedy value-regex leaks 2nd concatenated secret tail | P2 | 1 | **PRE-EXISTING at a0591ba3** (git-verified), NOT delta — admiral bucket-(2); non-gating for this baseline, real leak, needs named fix-owner |
| hk-xrn8r | subscribe-stream strict-order over-assert | P2 | 1 | bucket-(3) test-defect, pre-existing, non-gating; needs test-fix owner |
| — (CR) | F2 dead code `leaderDeferTextForHandoff`; F3 redundant global-trust write | P3 | 2 | cosmetic housekeeping, note only |

No `remediation:blocking` finding. Nothing floored to Tier-0 by the risk-floor coupling (the workloop dispatch change is
inert in the current runtime; the keeper gating change is the intended, tested behavior).

---

## Residual risk (for the admiral's release call)

1. **Codex isolation guard is scaffolding, not live-exercised end-to-end.** Inert at this pin (pi/claude substrate; flag
   false). Its remote-ssh-boundary branch will first go hot at yankee's codex cutover — **re-baseline required then**
   (a product-source commit will land, per your re-pin directive).
2. **LT ran single-mode pi:local, not the full DOT graph.** Covered-by-invariance (delta doesn't touch the DOT engine) +
   prior baseline covered DOT-attempt; local ornith can't converge a multi-turn DOT round-trip (needs codex-minimal).
   Logged caveat, non-gating.
3. **Two secret-leak / drain-order known-issues (hk-2hpeh, hk-mlkec)** are real but pre-existing/latent — carry named
   fix-owners so they aren't lost; neither reaches the current core loop.
4. **This box is resource-constrained** — several E2E/throughput tests flake purely on CPU starvation. Product invariants
   held in every case; the flakes are a test-environment signal, not a product signal.

---

## Coverage

- **Suites RUN:** §4A · LT · CR · XT · regression green-tree · S6 (always-on) · hk-xrn8r rerun. **SKIPPED (logged):** codex
  rows (operator "codex minimal"); remote substrate (no worker); full DOT-graph LT (covered-by-invariance); live
  context-fill keeper drive (covered by unit tests + LT).
- **Completeness critic: DRY.** No unrun modality that would change the verdict; the one load-bearing unverified claim
  (Throughput = regression?) was executed and refuted. Remaining gaps are the pre-existing logged caveats above.
- Full detail: `RUN-LOG.md`, `REGRESSION-TREE.md`, `CR-FINDINGS.md`, `XT-FINDINGS.md`, `LOG-WATCH-FINDINGS.md`,
  `COVERAGE-MANIFEST.md` (all in this RUN_DIR).

---

**Recommendation to the admiral: PASS the 599f80ab baseline (authoritative for release 9152ea5c = pin + test-only).**
The assessor recommends; the admiral makes the release call and holds the epic→main PR / deploy decision.
