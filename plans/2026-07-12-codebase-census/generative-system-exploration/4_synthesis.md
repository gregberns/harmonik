# 4 — Synthesis: the fresh panel's answer (2026-07-13, overnight)

> Six fresh agents were run against the operator's event-substrate direction (doc 3) and its
> three open questions (where to start, quality mechanisms, measurement). This is the
> consolidated read-out. **Headline: the direction is sound, mostly grounded in real code, and
> the template is ALREADY BUILT. It deflates to one concrete first vertical — session-restart —
> whose first property test IS the resume-hang fix.** No simulation, no metabolism engine yet.

## The convergent answer (all six agree on the shape)
1. **Start with session-restart.** Only candidate that is self-contained (low blast radius),
   already event-sourced, and a clean exemplar of the whole method. (Sequencer ranked it #1 over
   remote/agent-input/god-function on 5 axes.)
2. **The resume-hang fix is its first invariant, not a parallel task.** The load-bearing property
   test — *"every restart cycle reaches a terminal event within a bounded window; never silence"* —
   IS STEP-0a. Proving-vertical and live-bug-fix are one motion. The other two direct fixes
   (0b false-close, queue two-writer `rpc.go:1016`) stay a parallel plain-fix track.
3. **The template already exists and passes tests.** The Codex app-server work (`internal/apptap`,
   `internal/codexwire`, `internal/codexreactor` + `fake.go`, `internal/codexdigitaltwin`,
   `internal/codextest` L0–L3) is a *built, committed, green* instance of exactly the
   capture→layer→swap-a-fake pattern — with fault injection and a ~95%-zero-token test taxonomy.
   **First move: extract its generic seam (`EventSource`/`Effector`/`FakeEffector` + twin replay +
   L0–L3) into `internal/substrate`, leaving codex as the first instantiation; re-instantiate for
   session-restart.**
4. **Measurement: replay + fault-injection against a frozen baseline — NOT live A/B.** Live A/B is
   too costly/high-variance (~15–20 runs/arm). The honest, cheap, deterministic test is running
   old vs new logic over the *same recorded event streams* (catches the hang/false-close class,
   which are logic bugs), plus fault-injection pass-rate, confirmed by a longitudinal fleet trend.

## The reality check (the operator's "we already have the events" — half-right)
- **Boundary events exist**: 476 restart cycles recorded (`session_keeper_handoff_started` 507 →
  `cycle_complete` 427 / `cycle_aborted` 79 / `clear_unconfirmed` 347), 60 days of data. Enough to
  prove cycles happen and to replay at boundary granularity.
- **The interior 7-step stream does NOT exist on the bus.** Steps 3/4/5(success)/6 —
  handoff-file-written, model-done/final-response, clear-sent, new-session-up — are only in a
  per-agent `.harmonik/keeper/<agent>.cycle` journal that is **overwritten every cycle** (transient
  recovery state, not history). **So the true first build step is instrumenting 3–4 durable interior
  events.** Small, and it's the prerequisite for replay.
- **The missing ClockPort is the #1 testability blocker.** `cycle.go` calls `time.Now()` directly in
  8+ places and `injector.go` sleeps real wall-clock; timeouts/nonce-poll races can't be driven
  deterministically until time is a port.

## The invariants to encode (the "quality mechanism", concretely)
Session-restart (ordered): SR3 handoff-write-done **before** clear; **SR4 /clear NEVER before
model-done** (load-bearing); SR6 brief only after new-session confirmed; SR7 no overlapping
restarts; **SR9 bounded liveness — every cycle terminates or emits `restart_failed`** (= the
resume-hang). Run-lifecycle: RL1 daemon-owns-close; **RL3 close requires fix present, not an ID
mention** (false-close — already partly landed); **RL5 no `mergeMu` held across network IO** (wants
a real static lint, not the substrate); **RL7 stalled run → terminal signal within deadline**
(silent-hang — `postreadyhang.go` + `stalewatch.go` already exist). The three worth auto-feeding a
future "metabolism": RL7, RL3, SR4/SR7 — each recurring, expensive, and today hard to detect.

## Baseline captured TODAY (frozen, read-only: `.harmonik/events/baseline-2026-07-13/`)
- 237,099 events, 2026-05-14 → 2026-07-13 (~60 days). 89 MB event log + beads snapshot, `chmod a-w`.
- **run-completion rate = 1155/2142 = 54%** (run_failed 1045, run_stale 287).
- **restart-completion = 427/507 = 84%**, but **clear_unconfirmed = 347** (a large, under-examined
  failure surface in the restart path itself).
- These are the numbers any rebuild must beat. Losing this snapshot was the one unrecoverable risk;
  it is now preserved.

## The concrete first steps (deflated program)
1. **Instrument the 3–4 missing interior restart events** as durable bus events (handoff-written,
   model-done, clear-sent-success, new-session-up) + add a **ClockPort**. (Prereq for everything.)
2. **Extract `internal/substrate`** from the codex reactor/test harness (the generic seam + twin +
   L0–L3). Re-instantiate for session-restart with `ClaudeHandler` + `TmuxHandler` ports.
3. **Build the offline replay harness** over the 476 recorded cycles; assert the transition sequence.
4. **Encode SR9 (bounded liveness) as the first property test = the resume-hang fix.** Then SR3/SR4/SR6/SR7.
5. **In parallel (plain fixes, single-writer):** 0b false-close, queue two-writer `rpc.go:1016`.
6. **Measurement:** run steps 3–4 as replay-regression vs the frozen baseline; report against the
   54% / 84% / clear-unconfirmed numbers.

## Open decisions for the operator (morning)
- **Lift the freeze** for step 1–2 (instrument events + extract substrate) — low blast radius, single-writer?
- Confirm **session-restart as the first vertical** (panel unanimous) and STEP-0a folded in as SR9.
- Confirm **replay + fault-injection vs baseline** as the measurement standard (kills the live-A/B worry).
- Everything speculative (metabolism engine, 1000-session emergence, the sim) stays parked until one
  encoded invariant demonstrably changes one real vertical.

## Status
Panel complete; direction validated and made concrete; baseline frozen. Fleet still FROZEN; keeper
HELD; nothing dispatched. Awaiting operator's go on steps 1–2.
