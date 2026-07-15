# 04-Design / C5 — resume-hang bounded-liveness invariant + parity/oracle

> Pass 4 design for C5 + the M3-wide parity/measurement plan. Elaborates pins D7
> (liveness), D2 (clock), D9 (parity), D10 (M2 consume). Research:
> `03-research/c5-resume-liveness/findings.md` (LF), `03-research/c4-runexec-reactor/findings.md`
> (RF). Template: SK-INV-005/SK-015 (`specs/session-keeper.md:191-193, :264-266`),
> `SR9Checker` (`internal/replay/checkers.go:149-194`), P1 measurement-design.
> Target spec: NEW `specs/run-state-machine.md` §Liveness (RSM).

## Current state
The resumed-then-silent run (census REPORT §4) has NO stated invariant and NO test asserting
any bound. Termination is an EMERGENT property of 12 stacked caulks (LF §Q2), three actively
DEFEATED by the daemon's own 300s heartbeat (run_stale 10min, heartbeat-staleness 8min, the
30-min progress budget) and one <30-min bound enabled only by an emit-API accident (the
run_id-less fallback `agent_ready`, reviewloop.go:660). The 2s `resumeReadyFallbackGrace`
(reviewloop.go:110) BYPASSES the agent-ready timeout on every iteration ≥2. No `specs/` file
owns any of it; `run_stale` is cited to a non-existent event-model §8.12.1.

## Target state

### RSM-LIVENESS-1 — the invariant (the SR9 daemon peer)
For every run `r` that emits `implementer_resumed(r,i)`, exactly one run-correlated terminal
(`review_loop_cycle_complete(r)` with outcome, `run_completed(r)`, or `run_failed(r)`) or
failure-class event (`agent_ready_timeout(r)`, `agent_input_stale(r)`, `run_stale(r)`) MUST
follow within the bounded window. Neither ⇒ conformance failure. **Silence is FORBIDDEN.**
Structurally: every `EvTimerFired` edge in `Dispatch` lands in a state with an outgoing action
(keeper SK-INV-005 shape, verbatim).

### RSM-LIVENESS-2 — the bound (real constants, LF §Q4; the heartbeat split, D7)
Two composed tiers:
- **Primary (post-M2): the resume SEED stale bound ≈ 30s.** The resume seed is submitted via
  `ActSubmitSeed` → M2's `Submit` resolves ack / `ErrInputStale` within `input_ack_timeout`
  (30s, ClockPort-timed, IN-003). A stalled resume surfaces as `agent_input_stale` at 30s —
  MUCH tighter than any prior bound (D10).
- **Backstop (tmux-today + post-ack silence): the ready + working window.**
  `W_resume = effectiveAgentReadyTimeout(150s local / 210s remote) [ready] + commitHardCeiling
  (90m frozen-watchdog absolute) [working] + stop_hook_grace(3s) + overhead`. The tight
  headline guarantee is the READY sub-bound (resume→ready-or-fail ≤ agentReadyTimeout),
  replacing the 2s caulk. The 90m working ceiling is TODAY'S bound (frozen watchdog, D6 scope);
  tightening it is the watchdog-dissolution follow-on.

**The heartbeat split (D7, the central move).** `agent_heartbeat` (300s, daemon-goroutine
liveness) is separated from agent PROGRESS: `EvHeartbeat` no longer counts as progress in the
Dispatch machine; only agent-derived signals (`EvAgentReady` run_id-stamped, `EvCommitObserved`
worktree-HEAD advance, M2 input acks, tool/outcome events) advance the working state. This is
the ONE sanctioned observable divergence (hang → terminate).

### RSM-LIVENESS-3 — fail-CLOSED (LF §Q7)
The timeout edge kills + reopens (ReadyTimeout → Failed → Run Finalizing(reopen)), riding the
existing `ReviewLoopFailures` budget for anti-thrash. NOT fail-open (the keeper's model-done
bound is fail-open because a written handoff must not strand; a daemon resume with no ready
signal has nothing to preserve). Matches today's `agent_ready_timeout` failure semantics
(LF §1) → parity-compatible.

### RSM-LIVENESS-4 — NO new M3 events; reuse + attribution fix (D7, revised)
Q(b) settled: NO new durable event. The existing `agent_ready_timeout` + terminal family +
**M2's run-scoped `agent_input_submitted`/`acked`/`stale`** (D10) suffice. Two fixes make the
boundary replay-visible: (i) stamp run_id + iteration on the synthetic ready (reviewloop.go:660
+ workloopeventsource.go:145); (ii) accurate per-dispatch tracking. HOME the spec-orphaned
`run_stale` in the RSM spec and reconcile the `agentReadyTimeout` comment drift.

## The checker, corpus, tiers, oracle (D9)

### RSM-LIVENESS-5 — checker (extends internal/replay, upstream-additive)
`internal/replay` is (agent_name, cycle_id)-keyed today. M3 adds a **run-keyed track**
(new file `internal/replay/runcheckers.go`): a `RunTrace{RunID, Seen, ResumedAt, Terminal}`
accumulator + `RunChecker`/`RunFinalizer` mirroring the existing shapes, with:
- **`RunLivenessChecker`** (the SR9Checker mirror, checkers.go:149): Finalize flags (a)
  `implementer_resumed` seen with `Terminal==""` and no failure-class event — the
  resumed-but-silent gap (LF §Q2 proved replay-visible); (b) terminal-exclusivity breach.
- **`RunOrderingChecker`**: `launch_initiated(r)` precedes `agent_ready(r)` precedes
  brief/seed-delivery; `outcome_emitted(approved)` precedes `bead_closed`.

### RSM-LIVENESS-6 — corpus + tiers + oracle
- **Corpus:** extract per-`run_id` streams (EventID-sorted) + `summary.json` goldens from the
  frozen `events.jsonl` baseline into `testdata/daemon-runs/baseline-<date>/`, stratified
  {single, review-loop-resume, dot, merge-failure, reopen, shutdown-drain}, strata pinned in a
  manifest (P1 anchor discipline, D9). `harmonik subscribe --json` is the sanctioned reader
  (NEVER hand-grep by run_id — major-issue-fanout F14). A `StimulusSynthesizer` maps each
  summary → a reactor input schedule (streams record OUTPUTS; inputs synthesized from outcomes
  — P1 trace-driven-Twin). If resume-bearing strata are thin, synthesize schedules from the
  transition table (L0 already covers the space) — the corpus is parity evidence, not the only net.
- **L0** pure-Step table tests (every row incl. no-ops) + properties: terminal exclusivity;
  every TimerFired edge → ≥1 action or terminal (SK-015 structural rule over random
  interleavings); no ActSubmitBrief before EvAgentReady (structural).
- **L1** corpus replay: synthesized schedules → reactor → golden action sequences + the run
  checkers over re-emitted streams. Zero-token.
- **L2** shell + fakes: `FakeClock` drive-loop tests (deterministic timeout races), fake
  mergequeue recording-runner (DoD-2 check, c2 design), fault matrix via `substrate.Twin`:
  **`FaultStall` after `implementer_resumed`** (THE census fault-injection test: stalled agent
  on relaunch ⇒ agent_ready_timeout/input_stale + reopen within virtual-time window, never
  silence), plus FaultDropAfter/Truncate/Dup across strata.
- **L3** the existing hk- regression net (~466 tests) green per commit (primary parity
  envelope) + N=10 consecutive clean relaunch cycles under FakeClock (census DoD).
- **Oracle (out-of-band):** N=10 green full-suite; fault matrix 100% terminal-never-silence;
  jq over a replayed log confirms `RunLivenessChecker` flags a seeded hung-run fixture and is
  absent on post-fix streams (the daemon does not grade itself); coverage floor (M1→M3 audit)
  measured on internal/daemon/runexec + mergequeue + runshell.go; escape-check suite green
  unmodified.

### RSM-LIVENESS-7 — parity allowlist (D9)
The ONLY permitted stream divergences: (a) SR9 fix class (resume bound replaces the 2s caulk;
DOT back-edge resumes gain the bound — they have NO caulk today, LF §Q2; a formerly-hung run
now terminates); (b) run_id stamped on the synthetic ready; (c) escape-window shrink; (d) no
transient ref advance during build failures (c2 design). Everything else — event names, order,
reason/summary strings (incl. "(agent_completed)"/"(auto-close)", TF), bead transitions —
byte-compatible, asserted by L1 goldens. Honest limitation vs P1 D13: no old-vs-new PURE
differential (old code is IO-bound); compensated by the regression net + goldens + the M1→M3
coverage floor gating the extraction.

## Rationale
The invariant + FakeClock property test is the correctness deliverable of M3 (census STEP-0a).
Reuse-over-mint (D7) is decisive post-M2: M2's input events cover the confirm/stale milestones.
The heartbeat split is what actually removes the 12-caulk stack — every watchdog becomes a
reactor edge whose input is agent-derived.

## Requirements traceability
02-components C5 → RSM-LIVENESS-1..7. Goal "resume-hang cannot recur silently" (01 §2),
success-criterion 3 → RSM-LIVENESS-1/2/5/6.

## PLANNER-RECONCILE
- **[D7]** The heartbeat-semantics split changes an observable (hang → terminate) — the ONE
  sanctioned divergence; it touches hk-nvjk false-stale suppression and the accidental <30-min
  reaper bound. Confirm intended M3 scope (ROADMAP says yes).
- **[D10]** Post-M2 the primary resume-hang detector is M2's 30s `agent_input_stale`, with the
  ready/working window as backstop — confirm the two bounds stack and the C5 property test
  asserts both layers. `run_stale` staying masked for a WORKING-but-idle agent is out of M3
  scope (stall-sentinel owns stall semantics per reconciliation.md — reuse its signatures);
  the run checker catches the resumed-silent class offline regardless.
