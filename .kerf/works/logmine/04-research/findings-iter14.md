# logmine — iter-14 findings (2026-06-21, full harvest)

**Window:** line-anchored from iter-13 high-water `019ee6aa` → 3571 events, lines 39177–42747,
**2026-06-20T20:14Z → 2026-06-21T17:30Z** (~21h). Method: frozen `/tmp/logmine-window.jsonl`,
6 read-only slices (failures · reconciliation · review-loop · daemon-lifecycle/keeper · comms · git-churn/transcripts).
This is an **active, high-volume** window (vs iter-12/13 quiet micro-harvests) — it spans the
2026-06-20 nine-initiative burst aftermath and a real multi-lane deploy day.

## Headline

**Health is GREEN on the data plane** (reconcile resets all fired, 47/47 reviewer APPROVE, no
BLOCK-then-merge, no thrash/re-fix chains, no escaped-worktree). **The defects are in the
reliability/observability skin:**
- **[P1] Silent ~7h11m supervisor+daemon outage** (06:54→14:12Z) with NO detector — the auto-revive
  net itself was down; recovery was human-triggered. **(new bead hk-pen9)**
- **[P1] Local disk hit 2.68GB free vs 10GB watermark** + **36 leaked worktrees live now** —
  RECURRING. **(enriched hk-ldzp)**
- **[P1] Remote re-dispatch loop, no backoff/cap** — hk-icdz dispatched 6× all agent_ready_timeout.
  **(enriched hk-vjsv)** + 2 unsalvaged strands need `harmonik promote` (hk-sj6a, hk-zlwq).
- **[P2] review-bypass FP recurred on a SILENT reviewer** — hk-ijtw's fix only covers `run_failed`;
  a reviewer that just goes quiet still rings the IMMEDIATE channel (19× incl. 3 post-fix).
  **(new bead hk-usz0 — logmine-lane, dispatchable)**

## Findings register

| # | Pri | Verdict | Title | Anchor | Lane → action |
|---|-----|---------|-------|--------|---------------|
| F14-1 | **P1** | NEW | Supervisor+daemon both dead ~7h11m, no detector, no self-heal | daemon_started gap 06:42:59Z→13:54:06Z; recovery 14:12:10Z | daemon → **hk-pen9** (digest; paul) |
| F14-2 | **P1** | RECURRING | disk_low 2.68GB vs 10GB watermark; 36 leaked worktrees live | ev `019ee8a6-f212` 05:28Z; `git worktree list`=37 | daemon → **hk-ldzp** enriched (escalate; paul) |
| F14-3 | **P1** | RECURRING | Remote re-dispatch loop, NO backoff/cap — hk-icdz 6× agent_ready_timeout @90s | runs `019ee715`→`019ee7eb` | daemon → **hk-vjsv** enriched (paul/chani) |
| F14-4 | **P1** | REAL strand | 2 committed-but-unmergeable strands need promote-salvage | hk-sj6a `38276854`@`run/019eeac6`; hk-zlwq `21071f2e`@`run/019eeb36` (neither main-ancestor) | daemon → digest (captain: `harmonik promote`) |
| F14-5 | **P2** | NEW/RECURRING | review-bypass FP on SILENT reviewer (hk-ijtw fix covers only run_failed) | run `019ee8d8-7769` (hk-4lrj), 19 alerts 06:28→15:13Z incl. 3 post-fix (085dea6f@14:38) | logmine → **hk-usz0** (dispatchable) |
| F14-6 | **P2** | NEW class | box-A `fetchRunBranchBoxA` can't find `run/` ref (worker ran+committed, branch not visible) | run `019ee849-7df0` (hk-h106) | daemon → **hk-zsn7** (digest; paul/chani) |
| F14-7 | **P2** | RECURRING | hk-tnui: reviewer trailer stamped on daemon plumbing-commit, not bead-work commit (14/14) | 14 trailer-less work commits incl. `6272ba34`,`2f22a291` | daemon → **hk-tnui** enriched (paul) |
| F14-8 | **P2** | RECURRING | hk-v144: scheduled-hourly reconcile drops `completed` event (13/14 hourly; 24/24 startup OK) | started-no-completed ×13 (`019ee6e1`…`019eeb27`) | daemon → **hk-v144** enriched (paul) |
| F14-9 | **P2** | RECURRING | hk-vx1i no-progress FP re-witnessed on duplicate dispatch of landed work (hk-kj7d) | run `019eea9e-b0f0`; `a18638ea` already on main | daemon → **hk-vx1i** enriched (paul) |
| F14-10 | **P2** | RECURRING | chani remote-substrate blocker addressed `--to operator` only → no in-fleet fallback owner while operator away | chani chain 22:38Z→01:10Z then silent; reaped as ghost crew | coord → digest (captain) |
| F14-11 | **P2** | NEW | captain keeper rejected config 4× (all 26 keys missing) then self-armed — confirm block durable | evs `019eeab6-4204`…`019eeab7-9b03` 15:04–15:06Z | ops → digest (captain: confirm `.harmonik/config.yaml`) |
| F14-12 | **P2** | RECURRING | deploy-into-active-queue tax: 17 group_failure pauses + 45 operator-cancels (36 paused-by-failure) | `019ee6ab-6cd6`…; cancel burst 01:08Z | ops → digest (captain: restart-quiesce before redeploy) |
| F14-13 | P3 | benign | 24 daemon_started = deliberate deploy churn (12 distinct binary SHAs), not a crash-loop | first `019ee6e9-8606` pid76204 | — (interpretation note) |
| F14-14 | P3 | known | keeper /clear-confirm timing flakiness 3/3 (cycle completes ~1.5s later anyway) | `019eead7-fd17`,`019eeb00-e1de`,`019eeb2f-636f` | logmine note (widen settle window?) |
| F14-15 | P3 | noise | `session_keeper_idle_crew` misnamed — 1653 events = captain below-idle-floor poll, NOT staffing gap | all `agent:captain reason:below_idle_threshold` | note (rename candidate; but event-type = log contract, do NOT casually dispatch) |
| F14-16 | P3 | benign | bead_claim_skipped storm: 15× on hk-53p3 in 32s (`status_changed_between_select_and_claim`) | 15:10:23–15:10:54Z | daemon note (debounce) |
| F14-17 | P3 | clean | git churn coherent single-lane; no revert/fixup/re-fix chains; no CWD-drift; qa-scratch empty | 134 commits, 74 transcripts | — |

## FIXED-confirm / retired classes (the recurrence-delta payoff)
- **Strand auto-reset (hk-1h5q / hk-e3fy):** FIXED-CONFIRM — fired (`reconciliation_completed` reset=3 @23:42, reset=1 @06:22).
- **hk-53p3 reconcile epic-FP:** retired — 0 epic-typed mismatches this window; bead CLOSED `019eeabd-05b1` 15:11Z. All 3 mismatches were already-merged tasks, correctly reset.
- **crew-stale FP (hk-gu3v):** no recurrence.
- **hk-ijtw (run_failed review-bypass FP):** the run_failed arm IS fixed (no recurrence of THAT class); the **silent-reviewer arm is the new gap** (F14-5 / hk-usz0).

## Wave-2/3 actions taken
- **Filed:** hk-usz0 (logmine-lane, P2, dispatchable), hk-pen9 (daemon, P1), hk-zsn7 (daemon, P2).
- **Enriched:** hk-ldzp, hk-v144, hk-tnui, hk-vx1i, hk-vjsv.
- **Digest → captain** (`--topic findings`): F14-1/2/3/4/6/10/11/12 (daemon + coordination, NOT logmine-fixable).
- **logmine-q dispatch:** HELD pending captain greenlight — hk-usz0 is a clean single-script logmine-lane fix, but dispatch waits on token-burn priority + the captain confirming the lane is free.

> **Window:** line-anchored from iter-13 high-water `019ee6aa` → 3571 events, lines 39177–42747,
> high-water: 019eeb3b-fd35-77bc-b174-38f4f2b5cea1  (2026-06-21T17:30:37Z session_keeper_idle_crew — last line of the frozen iter-14 window; next run resolves THIS event_id to its line and slices forward, per F41a line-anchoring)
