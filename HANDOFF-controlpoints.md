<!-- PP-TRIAL:v2 2026-06-09 ~04:25 MST. main @ 6a32e057 (code 04dc9435). controlpoints lane. CONCURRENCY PERMANENT-WEDGE FIXED + DEPLOYED + validated by a 3-concurrent smoke (2/3 committed concurrently, suppress-spam 1 vs 217). Captain plan 5/15 landed. Do NOT clobber HANDOFF.md / HANDOFF-named-queues.md / HANDOFF-flywheel.md. -->

ROLE: orchestrator, controlpoints lane. This session: (1) salvaged the in-flight captain beads, (2) ROOT-CAUSED + FIXED the concurrent-dispatch wedge that blocked parallel running (operator-directed top priority), (3) fixed commit_gate so beads can land. Next: confirm the smoke merges, then run the rest of the captain plan CONCURRENTLY.

# Status — parallel dispatch RESTORED (permanent wedge fixed); residual launch-serialization slowdown remains

## What got fixed + deployed this session (all on main, daemon running the new binary, pid 4655, -c6, dot mode)
1. **`hk-37giq` — the real concurrency fix (`53ead2aa`).** The per-run heartbeat channel `tapCh` (`dot_cascade.go:549`) was consumed by TWO competitors (`waitAgentReady` + the `pasteInjectQuitOnCommit` watchdog); exclusive channel receive → under concurrency `waitAgentReady` starved the watchdog → it spun in launch-suppression forever (217× "launch-heartbeat-timeout suppressed"). Fix = fan-out the tap so the watchdog gets its OWN subscription. Reviewed APPROVE + race-clean test (old=0/50 starve, new=50/50). **VALIDATED:** post-deploy 3-concurrent smoke shows **1** suppress line vs 217 — the permanent wedge is GONE. The old `7641f648` 12m-ceiling is KEPT as a backstop (it only time-bounded the spin).
2. **`hk-p258q` — commit_gate quarantine (`a0117ba8`+`04dc9435`).** commit_gate runs `go build && go vet && bash scripts/scenario-gate.sh`; the scenario-gate's affected-unit step ran `go test ./internal/daemon/...` which has 22 deterministic reds → EVERY daemon-touching bead looped commit_gate→implement to the cap, never merging (silently blocked the captain plan all session). Fix = composition fix (9→11 golden counts) + 21 real-daemon-E2E tests quarantined behind `-short` + gate passes `-short`. `go test -short ./internal/daemon/` = GREEN. (Operator APPROVED shelving these temporarily; restore in a separate CI lane — bead to file.)

## The concurrency truth (settled by a 12-agent fan-out + 2 adversarial verifiers; DO NOT re-litigate)
- **NOT a regression — LATENT.** The wedge predates every revertable commit by ~2 weeks. **A rollback can't fix it and would drop the hk-9vp51 P0.** Don't roll back. (The original handoff's "regressed today" framing was wrong.)
- Exonerated with file:line: CLAUDE_CONFIG_HOME/trust-flock (hk-bfvby), new-window mutex scope (hk-oihnf), session-nesting (hk-9vp51), merge serializer, exit-127/PATH (already fixed by `cmd.Env=os.Environ()`). Don't chase these.

## Residual (concurrency WORKS but is SLOW to launch) — the next optimization, not a blocker
The 3-concurrent smoke still emits `launch_stall_detected` (~57s) per run, but the runs **RECOVER** (~2 min) and progress — the launches are **serialized by `newWindowMu`** (`tmuxsubstrate.go:627`, daemon-wide new-window lock). So 3 concurrent beads launch ~1-at-a-time (~1 min each) then implement/review run concurrently. This is a SLOWDOWN, not the wedge. → **`hk-goczd` (P2)** covers scrutinizing/narrowing `newWindowMu` (+ the forceTeardownSession backstop + heartbeat re-registration).

## Resume path
1. **Confirm the smoke landed.** Queue `019eac1c` (T11 `hk-zblnu` docs, T8 `hk-yj2j6` cmd, T9 `hk-4z0gp` daemon-tests). At handoff: T11 + T8 had committed (implementer_phase_complete, commit_landed=true) and were advancing to commit_gate; T9 launching. Check `harmonik queue status` + `harmonik subscribe`. Expect run_completed (merge) for all three; T9 exercises the `-short` quarantine. If T9 flakes at the gate, that's `hk-6ra3p` (the `TestReviewLoopBridge_CHB009` flaky red), not a real problem — re-dispatch it.
2. **Then RUN THE REST OF THE CAPTAIN PLAN CONCURRENTLY.** Daemon is at `-c6` (concurrency is restored). Submit remaining beads as a **STREAM** group (NOT wave — wave is single-active QM-027). Remaining: **T3 `hk-o50hy`** (re-dispatch — its pre-fix run hit the cap), T4 `hk-tfxjp`(scenario), T10 `hk-rbpss`(smoke), T12 `hk-cvg1j`(skill), T13 `hk-bejpi`(skill), T14 `hk-zi4ej`(scenario), T15 `hk-495is`(explore). (T8/T9/T11 landing via the smoke.) DAG below. Scenario/smoke (T4/T10/T14) are `//go:build scenario` — the gate SKIPS them; RUN THEM YOURSELF under the supervisor.
3. If a residual concurrency problem appears, it is the launch-serialization slowdown (hk-goczd), NOT the wedge — do not re-pin -c1 reflexively; the daemon now makes progress concurrently.

## Captain plan: 5/15 landed
Landed: T1 `hk-xdxws`, T5 `hk-i1ue4`, T6 `hk-kbqto` (prior); T2 `hk-w6y70`, T7 `hk-5tg5o` (salvaged this session: cherry-picked from their committed worktrees + reviewer APPROVE). T3/T8/T9/T11 in-flight or imminent. Remaining: T3,T4,T8,T9,T10,T11,T12,T13,T14,T15.

### The 15 beads (`codename:captain`)
| T | bead | what | deps |
|---|------|------|------|
| T1 | hk-xdxws | C1 child status on `br show` | — (landed) |
| T2 | hk-w6y70 | C1 emit `epic_completed` at-most-once | T1 (landed) |
| T3 | hk-o50hy | C1 boot-seed the guard from event log | T2 (re-dispatch) |
| T4 | hk-tfxjp | scenario: C1 emit/at-most-once/boot-seed | T3 |
| T5 | hk-i1ue4 | C2 `internal/crew` registry pkg | — (landed) |
| T6 | hk-kbqto | C2 `buildCrewLaunchSpec` | — (landed) |
| T7 | hk-5tg5o | C2 daemon crew-start/stop handler | T5,T6 (landed) |
| T8 | hk-yj2j6 | C2 `harmonik crew` CLI | T7 (in smoke) |
| T9 | hk-4z0gp | C2 unit tests | T7 (in smoke) |
| T10 | hk-rbpss | smoke: C2 crew on real `claude --remote-control` | T8 |
| T11 | hk-zblnu | C3 mission-handoff schema → `specs/crew-handoff-schema.md` | T2 (in smoke) |
| T12 | hk-cvg1j | C3 crew-launch skill | T11 |
| T13 | hk-bejpi | C4 captain skill | T12,T8,T3 |
| T14 | hk-zi4ej | scenario: end-to-end captain+crew | T4,T9,T10,T13 |
| T15 | hk-495is | explore: operator CLI surface | T8,T13 |

DAG: C1 (T1→T2→T3/T4) ∥ C2 (T5/T6→T7→T8/T9/T10) → C3 (T11→T12) → C4 (T13) → T14/T15. **Caveat:** T3 (hk-o50hy) and T7 (hk-5tg5o) both edit `internal/daemon/daemon.go` — T7 landed, so T3 now rebases on it (one-at-a-time merge handles it). T11 creates `specs/crew-handoff-schema.md` (operator-resolved doc home). Plan: `docs/plans/captain/SPEC.md` (read first) + `c1..c4-spec.md`.

## Open follow-ups (beads filed)
- **hk-pj4b6 (P1)** — commit_gate loop-no-escape: a no-commit re-entry implementer + DISCARDED gate output (`cmd.Run()` at `dot_cascade.go:792`) → loops to cap with nobody able to see why. Fix = capture gate stdout/stderr + emit a diagnostic event + short-circuit no-commit re-entry → needs-attention. **Highest-value hardening** (it's why diagnosing tonight took 12 agents).
- **hk-goczd (P2)** — narrow `newWindowMu` (the launch-serialization slowdown) + forceTeardownSession backstop (`dot_cascade.go:553`/`dot_gate.go:333`) + heartbeat re-registration on implement re-entry.
- **hk-6ra3p (P2)** — `TestReviewLoopBridge_CHB009` flaky under `-short` → can intermittently flake the gate for daemon beads.
- **hk-jgxqc** — the original "concurrent wedge" bead; can CLOSE as fixed-by-hk-37giq (the permanent wedge) once the smoke fully merges, leaving the slowdown to hk-goczd. Verify first.
- File a bead to restore the 21 quarantined E2E tests in a separate full-suite CI lane.

## Coordination
named-queues (daemon-owner lane) was HOLDING serial, waiting on me; I drove the fix per operator direction, synced on `harmonik comms` (topics `concurrency-rootcause`/`daemon-restart`). My comms identity = `captain` (always pass `--from captain`). The tap-fanout touches their spawn lane — `53ead2aa` is theirs to review if they want.

# Translations
T1–T15 = the 15 captain beads (`codename:captain`). tapCh = per-run heartbeat event channel. waitAgentReady = launch-readiness poller. pasteInjectQuitOnCommit = post-commit /quit watchdog. commit_gate = daemon's `go build && go vet && scenario-gate.sh` step (now `-short`). launch_stall_detected = implementer-spawn-stalled detector (fires if launch_initiated lags run_started by 30s; recovers under the current slowdown). newWindowMu = daemon-wide tmux new-window lock (serializes launches = the residual slowdown). hk-37giq = tap fan-out (THE wedge fix). hk-p258q = commit_gate -short quarantine. hk-pj4b6 = gate loop-no-escape (open P1). 7641f648 = launch-suppression-ceiling backstop. wave = single-active queue (QM-027, don't use); stream = concurrent queue (use this). -c1/-c4/-c6 = daemon `--max-concurrent`.
