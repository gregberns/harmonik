<!-- TIER: 2 (operational state, days cadence)
     LOADED BY: captain @ STARTUP Step 0b; NOT loaded by crews or implementers
     OWNER: captain, updated at session end (before HANDOFF.md) or on any crew/epic change
     DO NOT PUT HERE: standing behavioral rules (→ orchestrator-rules skill);
                      this-session salvage / run-id play-by-play (→ HANDOFF.md tier-1);
                      durable phase/locked decisions (→ project.yaml tier-3) -->

# Tier-2 context: captain lane registry + medium-term tracker (days cadence)
# Captain reads on every boot (STARTUP.md Step 0b) BEFORE re-deriving lanes.
# Stable across /clear cycles; verify every claim against live ground-truth at Step 2.

## active_lanes  (as of 2026-06-18 ~21:25 UTC — LEAN park, operator away)

| crew | epic_id | epic_title (plain English) | queue | model |
|---|---|---|---|---|
| _(none — 0-crew lean park)_ | — | — | — | — |

- **logmine STOOD DOWN 2026-06-18 ~21:25 UTC** (autonomous, §0): harvest COMPLETE this cycle (logmine-q 0 pending, hk-gu3v salvaged), crew was idle + context filling (~187.5k, no crew-keeper auto-clear on this deploy per hk-tt9q) → teardown honors hk-rl4b token-burn + hk-ev9e (auto-teardown-after-daily-run intent). **No work lost** — durable mission file `.harmonik/crew/missions/logmine.md` + `br show hk-mhmaw --assignee` mirror → `harmonik crew start logmine --queue logmine-q --mission .harmonik/crew/missions/logmine.md` re-hydrates next cycle / on operator return. Fleet now = daemon + captain + supervisor (minimal burn).

## Epics in progress

Live initiative/epic roster (absorbs STATUS.md "Active work lanes"). Verify status against `kerf next` / `br show <epic>` at boot — these are cached claims.

| Initiative | Epic | Status |
|---|---|---|
| keeper-redesign | `hk-gffc` | ✅ substantively DONE (keystones landed Jun16-18). Remaining is operator-gated: hk-34ac (re-scope), hk-gfpd (.sid-deploy call), hk-nlio (stranded-open, needs close) |
| Fleet sleep/wake | `hk-rl4b` | ⛔ not built — quiesce idle LLM-session token burn when operator away + work drained; genuine-drain guard required. Drives the LEAN posture. assignee=paul (crew not up) |
| leanfleet (fleet token-efficiency) | `hk-itoc` | 🟡 not staffed — restart-earlier, model-tiering, noise-cut |
| token-burn analysis | `hk-bsdr` | 🟡 not staffed (P1) |
| remote-substrate phase-1 | `hk-rs-phase1` | 🟡 not staffed (P1) — enables 2nd machine / scale-out; e2e needs gb-mbp worker |
| flywheel goal-persistence | `hk-0oca` | 🟡 not staffed (P1) |
| codex soak | `hk-0639` | ⛔ PAUSED (operator 2026-06-19) |
| Auto test/CI restoration | `hk-kjkbw` | 🟡 near-done (~26/27 beads closed) |

## Active operator directives (dated)

> set: 2026-06-19 · expires: ~2026-06-22 (3-day scale-out push — re-confirm or expire after the window)
> STANDING for EVERY captain across ALL restarts within the window. These OVERRIDE any
> stale "lean park / one-at-a-time / operator away" posture in a handoff. On conflict, THESE win.

- **set:2026-06-19 expires:~2026-06-22** — ONE-AT-A-TIME IS RETIRED. Run multiple lanes/crews in parallel (file-disjoint). The prior "work one item at a time" directive is NO LONGER in effect.
- **set:2026-06-19 expires:~2026-06-22** — Scale OUT across many sessions over ~3 days. Lots of context budget is available, but do NOT run too much at once — stage lanes; don't blast the whole fleet up at once.
- **set:2026-06-19 expires:~2026-06-22** — The daemon MUST dispatch EVERY bead through DOT — the SONNET triple-review graph (repo-root workflow.dot == sonnet-triple-review). NEVER the opus DOT. NEVER single/no-review mode. Verify via run_started workflow_mode (STARTUP 5c grep).
- **set:2026-06-19 expires:~2026-06-22** — Captain ORCHESTRATES; it does NOT do the work and does NOT micromanage. Allocate + direct; crews own their own tasks. Once the fleet is running, the captain is mostly QUIET — occasionally check crews; use FRESH-CONTEXT sub-agents to verify crew work.
- **set:2026-06-19 expires:~2026-06-22** — Captain ensures PROCESS, not task content: (1) double-check crew work, (2) ensure reviewers are ACTUALLY used, (3) ensure work is ACTUALLY TESTED before integration. Set a ~30-min check-in loop; do not babysit between ticks.
- **set:2026-06-19 expires:~2026-06-22** — A DAEMON issue takes PRECEDENCE over everything else.
- **set:2026-06-19 expires:~2026-06-22** — EVERY session (captain + crews + flywheel + watchdog) MUST stay under 300k tokens of context. The built-in keeper has reliability issues. Run an INDEPENDENT Sonnet context-watchdog: a 30-min loop that idles between ticks, checks every session's context, and FORCE-restarts any that exceed the cap.
- **set:2026-06-19 expires:~2026-06-22** — Internet is FLAKY. The agent<->Anthropic API loop works, but other internet calls (WebFetch, gh, package downloads, SSH to the remote box) may fail. Propagate this caveat to EVERY crew, especially remote-substrate.

## Lane staffing priority order (set 2026-06-19)

1. **remote-substrate** — FIRST, before anything else (enables the 2nd machine / scale-out)
2. **daemon-reliability** — make the daemon truly reliable
3. **token-usage / leanfleet** — move orchestration work off the captain onto a cheaper Sonnet agent
4. **flywheel positive/negative** — finish planning, create impl beads, push to remote; enables long non-stop cycles
5. then: keeper-limit reduction, logmine, churn (harah/gurney)

## parked

- daemon-reliability lane (paul): **hk-sj6a (P1, reviewer-stall wedge — #1 reliability blocker)** + **hk-53p3 (P1, promote-close-path gap; reconcile can't close promote cherry-picks)** are the priority pair. Plus hk-30jd/v144/tnui (daemon bugs), hk-ldzp (disk GC). NOT staffed while operator away (token-burn directive). **RECOMMEND on operator return: stand up paul targeting hk-sj6a + hk-53p3 first.**
- **STRANDED in_progress/open, pending hk-53p3 (do NOT raw `br close` — would reverse the locked beads-own-transitions decision):** hk-gu3v (fix on main, in_progress) + hk-nlio (prior promote-salvage, open). Both auto-close once hk-53p3 lands and `harmonik reconcile` runs.
- hk-tagp / main queue: paused-by-failure; remote-substrate e2e needing gb-mbp worker. Do NOT re-dispatch to main.
- hk-rty1 (P1): stranded in_progress one-liner (default→triple-review); needs split/reset to unstick.

## paused

- **codex (hk-0639)** — operator PAUSED 2026-06-19 (do not staff).
- **gh-bugs** — only do beads that ALREADY EXIST and do NOT need GitHub (no gh access / flaky internet).
