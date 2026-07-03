# logmine — Findings iter-11 (2026-06-18) · the reviewer-stall window

**Crew:** logmine · **Epic:** hk-mhmaw · **Queue:** logmine-q
**Window:** line-anchored from iter-10 high-water `019ed8df-cdc9-7320-abb4-e40cc8206650`
→ **799 events** (lines 38003–38801), 2026-06-18T03:57:12Z → 19:00:53Z (~15.1h).
**Mode:** 6-slice read-only fan-out over the frozen snapshot `/tmp/logmine-window.jsonl`
(failures/wedges · reconciliation · review-gate integrity · daemon+keeper · comms · git-churn/disk),
plus a main-thread reconciliation pass (orphan-vs-superseded, FIXED-sha-on-main checks).
logmine stayed OFF daemon-package files (paul owns them via hk-rl4b).

---

## HEADLINE — GREEN, with ONE recurring escalation.

The iter-10 context-cancel saga is **resolved and holding** (0 `context_cancelled` all window;
the 960deafc / 327a686f auto-reset confirmed). The window's dominant remaining friction is a
**single class seen from three angles**: a DOT reviewer node that is *dispatched but never emits a
verdict*, against which the daemon's stalewatch/reaper is **blind because it keys on
heartbeat-presence, not verdict-absence**. It wedged hk-zojj for ~50min, blocked the P0 keeper
lane, and cost **15 captain hand-authorization messages across 2 wedges** to clear by manual
salvage-promote. This is **already filed as hk-sj6a (P1)** — iter-11 enriches it with the precise
signature + the toil cost + the fix direction. Everything else in-window either landed, self-healed,
or is benign observability noise.

---

## Register (prioritized)

| ID | P | Status | Title | Lane | Anchor |
|---|---|---|---|---|---|
| F11-A | P1 | RECURRING→**enrich hk-sj6a** [T] | DOT reviewer dispatched-but-no-verdict; reaper keys on heartbeat not verdict-absence; no self-recovery → manual salvage | crew:paul (daemon) | run `019ed8e3-38be`; salvage `f116b17a`✓main |
| F65 | P1 | RECURRING→**enrich hk-sj6a** [T] | Manual wedge-salvage choreography = 15 comms msgs / 2 wedges; daemon could auto-promote+restart deterministically | crew:paul (daemon) | comms 04:39→05:01 (6 msgs) + 03:09→03:27 (9 msgs) |
| F66 | P1 | **NEW — file** [T] | `ops-monitor crew-stale:captain` FALSE POSITIVE — keys on presence `last_seen>150s`, but `comms send` doesn't refresh presence; fires while captain actively posts | crew:logmine (script) | fired 17:39:33Z + 17:54:35Z (3min after a status post); `scripts/ops-monitor-check.sh:38,184`, `comms.go:959` |
| F-S6.5 | P2 | **NEW — file** [T] | Disk/worktree pressure: 17 GiB free; 47 worktrees, ~11 stale `run/*` (Jun 8–10, orphaned) + 17 stale `.claude/*`; `prune` won't remove (registered) | crew:paul (daemon GC) + manual reap | `df` 17Gi; `du .harmonik/worktrees` 623M; oldest `019ea848…` Jun 8 |
| F-S6.2 | P3 | NEW — digest note | Run-context commit-pair churn: 8× `CHB-023`+`hk-4je` bookkeeping commit-pairs/window land gitignored `.harmonik/run-context/*` on main then strip → 16 no-code commits, mimics a fixup-chain | crew:paul (daemon merge) | 132dff23, debb1e45, bc3301e9 |
| F-S6.6 | P3 | NEW — digest note | `stale_intents_observed:21` static/non-decrementing across both sweeps (12h apart), `intents_gc_d:0`, intents dir empty = phantom counter / metrics smell | crew:paul (daemon) | sweeps 04:59Z + 17:23Z |
| F-recon-N5 | P3 | RECURRING (=iter10 N5) | Scheduled-hourly reconciliation emits `started` but no `completed` (14 started/0 completed); startup path emits both — observability gap, NOT a leak | crew:paul (daemon) | started 14×; completed `019ed919-9f06`, `019edbc2-6966` |
| F-keeper-46 | P2 | RECURRING + 1 NEW reason | Keeper restart friction self-heals: 3 `restart_now_blocked` (2× `anti_loop_suppressed` known, **1× `handoff_stale` NEW reason-code**), 3 `clear_unconfirmed`, 1 `cycle_aborted handoff_timeout` — all reached `cycle_complete`, 0 data loss | crew:paul (keeper) | cycles @05:00, 16:58, 17:32 |
| F-47 | P2 | RECURRING benign | 94× `session_keeper_no_gauge` (all captain/`foreign_session`, ~2min tick) = the known crew-gauge-not-wired drift; warn/act still fire via cycle path (3 warn→3 complete) | crew:paul (hk-gffc) | re-confirm hk-gfpd close |
| F11-B | P3 | **superseded — recommend DROP** [T] | hk-7rmv orphan `1575546d` stranded on dangling `run/019ed91b`; keeper-SID approach superseded by hk-3391 (`93f7000e`,`12853cc6` on main); orphan predates current main → no salvage | captain decision | bead hk-7rmv still OPEN P0 |

### FIXED-CONFIRM (the payoff of recurrence tracking)

- **hk-e3fy — DOT `context_cancelled` strand: FIXED, holding.** 0 `context_cancelled` events all window
  (960deafc + auto-reset 327a686f). [T across S1, S6]
- **hk-2vpj — silent review-gate bypass: FIXED on main (`4b1c7f91`, ancestor of HEAD).** Real (hk-7xr9
  merged via dot with reviewers 2..N skipped — root cause `dot_cascade.go:457` iter≥2 guard + `:493`
  APPROVE-exemption now gated `&& !isReviewer`). Exposure bounded to 1 bead; fix landed 18:58Z, 27min
  after the last bypassed run. Enrich hk-2vpj with the regression-witness traces (hk-7xr9 run
  `019edbdb-249f` = 1 verdict vs healthy hk-9hix run `019edbc6-4b0f` = 5 verdicts). [T: events+code+git]
- **hk-guez — cache-reaper merge-build race: FIX HOLDING.** 0 `merge_build_failed (go vet)` / 0 ENOSPC
  all window after 5c2276ca (proactive 60m reap removed). [T]
- **F13 — "whose-bead?" round-trips: 0 in-window.** Gap-1 `--assignee` epic-mirror held; no attribution toil recurred.
- **25-launched/24-verdict gap = benign window truncation** (hk-2vpj's own reviewer in-flight at the 19:00:53Z cutoff, run `019edc05-3485`), NOT an unreviewed merge.

---

## Notes for the digest

- **All 11 `run_completed` landed on main** (gy9j, uqs4, ypzd, 1tn2, z4cm, cu7g, nbmv, 3a2j, 9hix,
  kuyl, 7xr9 — grep-confirmed). Only 1 `run_failed` (hk-7rmv, budget-exhaust→no-progress) and it was
  superseded, not lost.
- **2 daemon restarts** (04:59:54Z wedge-release pid 22588; 17:23:30Z redeploy to binary 6e32c2ca pid
  19774) — both clean orphan-sweeps, no orphaned active runs, no `daemon_config` drift.
- **No panics / backoff / socket / pidfile / ENOSPC** in events; no daemon stdout logfile (events.jsonl
  is the sink). No reverts in-window; `fmt: gofumpt+gci` ×3 is routine auto-format.

> **Window:** line-anchored from iter-10 high-water `019ed8df` → 799 events, lines 38003–38801,
> 2026-06-18T03:57:12Z → 19:00:53Z.
> high-water: 019edc1b-8b5e-77d3-b358-4bf081eaf882  (2026-06-18T19:00:53Z agent_presence — logmine's own iter-11 comms-join, last line of the frozen window; next run resolves THIS event_id to its line and slices forward, per F41a)
</content>
</invoke>
