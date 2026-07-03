# logmine — iter-13 findings (2026-06-20, micro-harvest)

**Window:** line-anchored from iter-12 high-water `019ee57f` → 86 events, lines 39091–39176,
**2026-06-20T14:46Z → 20:13Z** (~5.5h, quiet). Method: frozen `/tmp/logmine-window.jsonl`,
2 read-only slices (review-bypass+failures; stranded-beads+keeper+daemon+comms).

## Headline

**HEALTH GREEN, low volume, but two genuine items + an unattended-coordination problem.**
- **NEW ops-monitor FALSE-POSITIVE class** (filed **hk-ijtw**, P2): the `[IMMEDIATE] review-bypass`
  alert fired on a run that *failed and merged nothing*. Same cry-wolf risk as the crew-stale FP.
- **NEW useful observability event** `reconciliation_mismatch_observed` — with an epic false-positive
  (enriched hk-53p3).
- **Coordination gap (escalate):** chani's remote-substrate blocker sat **~90min+ unanswered**, paul's
  daemon-restart blocker needs a confirm, and **jamis idled ~2.5h** with capacity unassigned.

## Findings register

| F | Pri | Verdict | Description | Anchor | Lane → action |
|---|-----|---------|-------------|--------|---------------|
| F13-1 | P2 | **NEW → filed hk-ijtw** | ops-monitor review-gate FP: a `run_failed` run that emitted `reviewer_launched` but no `reviewer_verdict` trips review-bypass FOREVER (predicate joins event-stream, no run_failed exclusion). Fired [IMMEDIATE] on hk-gu3v's failed reviewer-ceiling run 019edc40-632b (commit 1b9751aa NOT on main → merged nothing). | alert ev `019ee6aa-60a1`; `scripts/ops-monitor-check.sh:331-360,193` | crew:logmine (script). Fix = add run_failed terminal-state exclusion. |
| F13-2 | P2 | **NEW → ENRICH hk-53p3** | `reconciliation_mismatch_observed{bead_inprogress_queue_absent}` is a NEW + useful daemon event (surfaces stranded in_progress beads). BUG: fires a FALSE POSITIVE on open EPICs (hk-lkbw, type:epic, legitimately queue-absent). Observer should exclude type:epic. | ev `019ee610-a00d`, `019ee6aa-46a5` | crew:paul (observer) |
| F13-3 | P3 | NEW → note | Keeper smoke harness (`keeper-smoke-live`) emits `session_keeper_ack_timeout{reason:no_tmux_target}` into the PROD event stream → looks like a real keeper failure to log-mining. Consider namespacing / `smoke:true` flag. | ev `019ee5d6-995e`, `019ee5d6-996e` | crew:logmine (process) |
| F-S6.6 | P3 | RECURRING | 21 `stale_intents_observed` persist un-GC'd (`intents_gc_d:0`) across both window restarts — same static counter as iter-12. | sweeps `019ee5d9`, `019ee6aa-44d8` | crew:paul → digest note (known) |
| F12-A | P1 | chani-owned | remote-substrate `agent_ready_timeout`@90s (run 019ee585) → run_failed → queue group_failure pause. Single causal chain, KNOWN. Root cause now refined by chani: intermittent SILENT daemon-side remote worktree-create/launch fail (NOT connectivity, NOT reverse-tunnel — both refuted). | run `019ee585-3834`; queue `019ee585-3654` | crew:chani / crew:paul (daemon worktree-create) |

## Stranded-bead resolution (the 3 reconciliation_mismatch beads — all non-wedges)
- **hk-yfcc** (keeper W4): was stranded in_progress → **auto-RESET by the 17:25 scheduled reconcile**
  (`reconciliation_completed` ev `019ee610-a0d4`, `beads_reset:1`); work on main `ff024c89`; now CLOSED.
  The observed→reset path WORKS for already-merged task beads.
- **hk-gh1m** (assetsync reconcile engine): already-merged `f7c286ac`; CLOSED. Reconcile gap resolved.
- **hk-lkbw** (keeper-config EPIC): the epic false-positive above (F13-2). Not a wedge.

## FIXED-CONFIRMs / clean
- **Daemon:** 2 restarts (redeploy `b43638ca`→`4235ec69`=HEAD), both clean — 0 live work reaped,
  captain/crews skipped, zero config drift (`max_concurrent:4, workflow_mode:dot`).
- **Keeper:** the 2 ack_timeouts are smoke-test, not a real session (NOT the hk-5da7 class).
- **Strand auto-reset (hk-1h5q / hk-e3fy):** both CLOSED; the observed→reset path fired correctly
  for hk-yfcc this window.

## Coordination blockers (digest → captain; NOT logmine's to fix)
1. **chani — remote-substrate e2e BLOCKED, ~90min+ unanswered.** Escalated to OPERATOR 16:40:29Z with
   2 asks; real bug = intermittent SILENT daemon-side remote worktree-create/launch fail → route to
   paul/daemon lane. (Maps to the known remote-substrate worktree-create blocker.)
2. **paul — cross-lane BLOCKER 14:50:55Z:** `paul-q` dispatch UNSAFE until a daemon restart (global
   `workerRegistry`, `workloop.go:583-590`). A restart DID fire 20:12:58Z (PID 67614, HEAD `4235ec69`)
   — captain should CONFIRM it picked up the fix paul needs.
3. **jamis — IDLE ~2.5h** (≥10 "no epic assigned" heartbeats) while chani's lane is blocked. Staffing gap.

## Wave-2/3 actions
Filed **hk-ijtw** (ops-monitor FP, P2, crew:logmine). Enriched **hk-53p3** (recon-mismatch new event +
epic FP) and **hk-sj6a** (ops-monitor FP cross-ref). **No logmine-q dispatch** — hk-ijtw is a clean
single-file logmine-lane fix but dispatch is HELD for captain greenlight (token-burn priority + active
chani/paul blockers consuming the lane). Digest → captain `--topic findings`.

> **Window:** line-anchored from iter-12 high-water `019ee57f` → 86 events, lines 39091–39176,
> high-water: 019ee6aa-60a1-7a12-878e-5a6a485e21ff  (2026-06-20T20:13:06Z agent_message — the ops-monitor review-bypass alert, last line of the frozen iter-13 window; next run resolves THIS event_id to its line and slices forward, per F41a)
