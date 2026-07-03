# 14 — Phase-2 Destructive-Cycle Dogfood Runbook (THROWAWAY ONLY)

Status: PENDING — execute only AFTER hk-kct9t (anti-loop + crash-recovery + defect fixes) lands
AND is independently re-reviewed. The cycle core (hk-22i70) is APPROVE_WITH_NOTES but
DEFECT-1 (empty session_id → unbounded /clear) and DEFECT-3 (.managed only in caller) are
HARD PREREQUISITES — do NOT dogfood the destructive cycle until those are fixed.

## ⚠️ SAFETY INVARIANT (load-bearing — re-read before every step)
- The cycle does a DESTRUCTIVE `/clear`. It acts ONLY on an agent with
  `.harmonik/keeper/<agent>.managed`.
- **NEVER** create `.harmonik/keeper/{flywheel,named-queues,controlpoints}.managed`.
- Use a THROWAWAY agent name + THROWAWAY tmux pane ONLY (pattern: `skdog`/`skfix`).
- Operator said (2026-06-07): do not terminate a live session. Be cautious with kills.
- Cleanup is mandatory: `tmux kill-session` + `rm .harmonik/keeper/<throwaway>.*` at the end,
  even on failure.

## Pre-req checks
1. `git -C /Users/gb/github/harmonik log --grep "Refs: hk-kct9t" --oneline` → landed.
2. Independent re-review of the hk-kct9t diff (a reviewer sub-agent): confirm DEFECT-1..4
   fixed, .managed checked INSIDE MaybeRun, tests still FAKES-only.
3. `go install ./cmd/harmonik` if the keeper binary changed (the `harmonik keeper` subcommand).

## Setup (throwaway)
Pick a unique throwaway name, e.g. `NAME=skdog$(short-rand)`. (Date.now/rand are fine in shell.)
1. `tmux new-session -d -s "$NAME"` — the throwaway pane the keeper will inject into.
2. In that pane launch claude NAMED + remote-controlled so the operator can watch:
   `claude --remote-control "$NAME"`  (operator views at claude.ai/code).
   The session MUST be named, or `/session-resume` opens the interactive picker and the
   injector HANGS (hard prereq from hk-ekap1).
3. Seed a handoff file the cycle will use: `HANDOFF-$NAME.md` with minimal content.
4. Opt-in the THROWAWAY: `touch .harmonik/keeper/$NAME.managed`  ← throwaway ONLY.

## Drive the cycle
5. Start the keeper against the throwaway:
   `harmonik keeper --agent "$NAME" --tmux "$NAME" --act-pct 90 ...` (confirm real flags
   from `harmonik keeper --help`; ensure the Cycler is actually wired — DEFECT-5 fix).
6. Force the gauge above act_pct while the throwaway is idle (CrispIdle true, not
   HoldingDispatch): write a high pct to the gauge file the keeper polls
   (`scripts/keeper-statusline.sh` target — supply a NON-empty session_id, per DEFECT-1).

## Observe — HAPPY PATH (expect, in order)
- event `session_keeper_handoff_started`
- `/session-handoff HANDOFF-$NAME.md` injected into the pane, with the
  `<!-- KEEPER:<cycle_id> -->` nonce directive
- the throwaway writes `<!-- KEEPER:<cycle_id> -->` into `HANDOFF-$NAME.md`
- ONLY THEN `/clear` injected; journal `.harmonik/keeper/$NAME.cycle` phase=cleared
- `/session-resume HANDOFF-$NAME.md` injected; new session_id observed
- event `session_keeper_cycle_complete`; journal closed
- ANTI-LOOP: it does NOT immediately re-fire on the resumed session (suppressed until new
  session_id AND pct<warn_pct) — watch for ~2 min, confirm no second cycle.

## Observe — ABORT PATH (the fail-safe; run this too)
- Repeat setup; this time DO NOT let the throwaway write the nonce (e.g. instruct it not to,
  or use a handoff the throwaway won't touch).
- Expect: `/session-handoff` injected, poll times out (~180s), event
  `session_keeper_cycle_aborted`, journal closed, and **NO `/clear` ever injected** — the
  session is untouched. This is the single most important property to see live.

## Crash-recovery (if hk-kct9t added it)
- Kill the keeper after phase=cleared but before resume; restart it; confirm it injects
  `/session-resume` to recover the wiped throwaway, emits `session_keeper_cycle_recovered`.

## Cleanup (ALWAYS)
- `tmux kill-session -t "$NAME"`
- `rm -f .harmonik/keeper/$NAME.managed .harmonik/keeper/$NAME.cycle HANDOFF-$NAME.md`
- Verify no stray `.managed` for any real agent remains:
  `ls .harmonik/keeper/*.managed` must NOT list flywheel/named-queues/controlpoints.

## Record results → here + close hk-ekap1 only after BOTH happy + abort paths pass live.

## RESULTS — 2026-06-08 (flywheel, injection-layer dogfood)
Ran the REAL `harmonik keeper` (binary @ 433b782e) against a throwaway `skdog1` tmux pane
running a dumb shell (NOT claude), with a fake gauge + throwaway `.harmonik/keeper/skdog1.managed`.
This validates the live-wired keeper's real deps (gauge-read → gating → tmux inject → journal)
end-to-end. It does NOT exercise a real claude executing /clear+/resume — that's the spike
(hk-vp9i8, already PASS) + the operator's `claude --remote-control` round-trip.

- **ABORT path — PASS (the fail-safe).** gauge pct=95, act-pct=90, session_id="dogfood-abort-1",
  CrispIdle. Cycle fired: journal cyc-20260608T101057-000001 phase=handoff_injected; pane shows
  `/session-handoff …HANDOFF-skdog1.md` + the nonce directive. Nonce NEVER written → after exactly
  180s (handoff_timeout) journal → phase=`aborted`, reason=`handoff_timeout`. **`/clear` count in
  pane = 0.** Session untouched. The single most important property, confirmed live.
- **HAPPY path — PASS.** New session_id "dogfood-happy-1" dipped to pct=50 (below warn 80) to re-arm
  the anti-loop, then pct=95 → 2nd cycle cyc-20260608T101057-000002. Wrote `<!-- KEEPER:cyc-…-000002 -->`
  to HANDOFF-skdog1.md → keeper confirmed → injection order in pane: `/session-handoff`, then `/clear`
  (ONLY after nonce), then `/session-resume`. Journal → phase=`complete`. (In a dumb shell the gauge
  doesn't get a new session_id post-/clear, so expect a best-effort `clear_unconfirmed` — non-fatal,
  by design; /session-resume still injected.)
- **Anti-loop re-arm — PASS.** 2nd cycle fired only after a NEW session_id was observed below warn-pct.
- **Cycle-ID format — confirms DEFECT-2 fix:** `cyc-<UTC-timestamp>-NNNNNN` (collision-resistant across restarts).
- **Teardown — clean:** keeper killed, skdog1 session killed, all `.harmonik/keeper/skdog1.*` + HANDOFF-skdog1.md removed; re-verified NO real `.managed` markers remain.

REMAINING before closing hk-ekap1: the operator's `claude --remote-control` full round-trip
(a real session actually clearing + resuming). Epic left OPEN for that.
