# 04-Design / liveness + parity — the SR9 peer, measurement, and the acceptance oracle

> Component design for C5 + the M3-wide parity/measurement plan, within pins
> M3-D7/M3-D10/M3-D12/M3-D14. Facts cite `03-research/liveness/findings.md` (LF)
> and `03-research/runexec/findings.md` (RF). Template: SK-INV-005/SK-015
> (`specs/session-keeper.md:191–:193, :264–:266`), `SR9Checker`
> (`internal/replay/checkers.go:149–:194`), P1 measurement-design.

## 1. The invariant, precisely (RX-INV-003, the SR9 daemon peer)

**Statement (spec §5):** For every run `r` that emits `implementer_resumed(r)`,
exactly one run-correlated terminal (`review_loop_cycle_complete(r)` with
outcome, `run_completed(r)`, or `run_failed(r)`) or failure-class event
(`agent_ready_timeout(r)`, `run_stale(r)`) MUST follow within the bounded
window. Neither ⇒ conformance failure. Structurally: every `EvTimerFired` edge
in `Dispatch` lands in a state with an outgoing action (keeper SK-INV-005
sentence, verbatim shape).

**Window derivation (from the real constants, LF §6):**
`W_resume = effectiveAgentReadyTimeout (150s local / 210s remote) [ready segment]
+ input_ack bound (30s default) [brief segment]
+ commitHardCeiling (90m, the frozen watchdog's absolute bound) [working segment]
+ stop_hook_grace (3s) + scheduler overhead` — i.e. the resume→terminal bound is
dominated by the watchdog ceiling exactly as the keeper's is by
ClearConfirmBackstop. The READY sub-bound (resume→ready-or-fail ≤
agentReadyTimeout) is the tight, headline guarantee replacing the 2s caulk.

**Fail direction (decompose C5 Q(d), settled):** fail-CLOSED — the timeout edge
kills + reopens (ReadyTimeout → Failed → Run Finalizing(reopen)), never
silently proceeds. (The keeper's model-done bound is fail-open because a written
handoff must not strand; a daemon resume with no ready signal has nothing to
preserve — reopen is the safe terminal.) This matches today's
agent_ready_timeout failure semantics (LF §1), so it is parity-compatible.

**Scope:** ALL dispatches ride the same edges — the builtin review-loop resume
(the census exemplar), DOT back-edge resumes (which today have NO caulk — LF
§3), and fresh launches. Q(b) settled: NO new durable event (M3-D10) — the
existing `agent_ready_timeout` + terminal family suffice; the two joinability
fixes (run_id on the synthetic ready; accurate per-dispatch tracking) make the
boundary replay-visible without new types.

## 2. The checker (extends internal/replay — upstream-additive, not a fork)

`internal/replay` is (agent_name, cycle_id)-keyed today (checkers.go:9–:11).
M3 adds a **run-keyed track**: a `RunState{RunID, Seen map[EventType]…,
ResumedAt, Terminal}` accumulator + `RunChecker`/`RunFinalizer` mirroring the
existing `Checker`/`Finalizer` shapes, and:

- **`RX9Checker`** (the SR9Checker mirror, checkers.go:149 shape): `Check` no-op;
  `Finalize` flags (a) `implementer_resumed` seen with `Terminal==""` and no
  failure-class event — the resumed-but-silent gap LF §7 proved is
  replay-visible; (b) terminal-exclusivity breach (both run_completed and
  run_failed).
- **`RX4Checker`** (ordering): `launch_initiated(r)` precedes `agent_ready(r)`
  precedes any brief-delivery evidence; `outcome_emitted(approved)` precedes
  `bead_closed`.

This is a substrate/replay EXTENSION (new file `internal/replay/runcheckers.go` +
run-state bookkeeping in Replay), consistent with the "genericization gaps are
substrate changes, not daemon-local copies" constraint (problem-space §4).

## 3. The corpus (daemon peer of P1's 507-cycle corpus)

Extract from the frozen `events.jsonl` baseline: per-`run_id` event streams
(EventID-sorted) + `summary.json` goldens (terminal type, outcome reason, mode,
iteration count, merge outcome) into `testdata/daemon-runs/baseline-<date>/`.
Selection: all runs with a terminal in the baseline window, stratified {single,
review-loop w/ resume, dot, merge-failure, reopen, shutdown-drain} — pin the
strata counts in a manifest at extraction (the P1 anchor-pinning discipline,
D13). A `StimulusSynthesizer` maps each summary to a reactor input schedule
(the streams record daemon OUTPUTS; inputs are synthesized from outcomes —
exactly P1's trace-driven-Twin reasoning, measurement-design §2).

## 4. Test tiers (RS-017 taxonomy, daemon vertical)

- **L0** — pure `Step` table tests for both machines (every row incl. explicit
  no-ops) + property tests: (i) terminal exclusivity; (ii) every TimerFired edge
  produces ≥1 action or a terminal (the SK-015 structural rule, machine-checked
  over random event interleavings); (iii) no ActInjectClear-analog violation —
  here: no ActDeliverInput(brief) before EvAgentReady (RX-INV-002 structural).
- **L1** — corpus replay: synthesized schedules → reactor → golden action
  sequences + `internal/replay` RX checkers over re-emitted streams. Zero-token.
- **L2** — shell + fakes: `FakeClock`-driven drive-loop tests (deterministic
  timeout races — the thing C1 exists for), fake mergeq recording-runner (DoD-2
  check, c2-merge-queue-design §5), fault matrix via `substrate.Twin`:
  `FaultStall` after `implementer_resumed` (THE census fault-injection test:
  stalled agent on relaunch ⇒ agent_ready_timeout + reopen within virtual-time
  window, never silence), `FaultDropAfter`/`FaultTruncate`/`FaultDup` across the
  dispatch strata.
- **L3** — the existing hk- regression net (~466 tests) green per commit — the
  primary parity envelope; plus N=10 consecutive clean relaunch cycles (census
  DoD) via the review-loop harness under FakeClock.

## 5. Parity plan (M3-D12 operationalized)

- **Allowlist (the ONLY permitted stream divergences):** (a) SR9 fix class —
  resume bound replaces the 2s caulk; DOT gains the bound; a formerly-hung run
  now terminates (agent_ready_timeout + reopen); (b) run_id stamped on the
  synthetic ready; (c) escape-window shrink; (d) no transient ref advance during
  build failures. Everything else — event names, order, reason/summary strings
  (incl. the "(agent_completed)"/"(auto-close)" labels, RF §6), bead
  transitions — byte-compatible, asserted by L1 goldens.
- **Honest limitation (vs P1 D13):** no old-vs-new pure differential is possible
  (the old code is IO-bound); compensated by the regression net + goldens +
  the M1-5 coverage-audit floor gating the extraction (constraint: the
  state-machine path must meet the measured floor from tests that exec product
  code).
- **Census-claim correction carried into the spec (LF §3):** the spec's
  motivation section states the TRUE current behavior (three uncoordinated
  wall-clock bounds; run-correlated silence in the gap; heartbeat masking
  run_stale; DOT uncaulked) — not the folklore "hangs forever".

## 6. Acceptance oracle (out-of-band, census Oracle)

(1) N=10 consecutive green full-suite runs incl. the new L0–L2; (2) fault
matrix 100% terminal-never-silence; (3) out-of-band verification: jq over a
replayed run log confirms the resumed-run gap is flagged by RX9 on a seeded
hung-run fixture and absent on post-fix streams (the daemon does not grade
itself — the log does); (4) coverage floor (M1-5 value) measured & recorded on
internal/runexec + internal/mergeq + runshell.go; (5) regression net green;
(6) escape-check suite green unmodified.

## 7. Self-review notes

- The window W_resume inherits the 90m watchdog ceiling — a reviewer may argue
  that is too loose to call "bounded". It is TODAY'S bound (frozen watchdog,
  M3-D3); the tight new guarantee is the ready sub-bound. Tightening the working
  segment is the watchdog-dissolution follow-on.
- run_stale remains masked by heartbeats for a WORKING-but-idle agent (LF §3.2)
  — out of M3 scope (stall-sentinel owns stall semantics; reconciliation.md
  cross-ref says reuse its signatures, don't redefine). The RX9 checker
  catches the class offline regardless.
- The corpus extraction assumes the baseline events.jsonl contains enough
  resume-bearing runs; if the strata are thin, the synthesizer generates
  schedules from the transition table instead (L0 property tests already cover
  the space) — the corpus is parity evidence, not the only net.
