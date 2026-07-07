# logmine — iter-20 findings (2026-07-04)

**Window:** `/tmp/logmine-window.jsonl`, 17,813 events, 2026-06-26T19:13Z → 2026-07-04T19:42Z (~8 days).
**Method:** line-anchored forward from iter-19 high-water (never timestamp-string filtered — F41). 6 read-only
harvest slices (run-failures / reconciliation / review-loop / daemon-lifecycle+keeper / comms-bus / git-churn),
consolidated + deduped below. Run by crew **duncan** (pipeline authored for liet).

> **Provenance note:** Wave-1 harvest was fanned out in the prior duncan session; the sub-agents re-attached
> and completed their reports into the resumed session's context (contrary to the handoff's "assume lost" caveat).
> All 6 slice reports landed; no re-run was needed.

---

## Priority register

| # | Finding | Pri | Class | Lane | Bead / action |
|---|---------|-----|-------|------|---------------|
| 1 | Daemon crash-loop 17:14 & 17:17 (07-04) → 8 runs abandoned; + ~21-min restart cycle 06:14/06:36/06:57 | **P1** | NEW (recurs disproven-crash pattern) | stilgar | NEW bead → digest captain |
| 2 | Empty-HEAD worktree-create race resurrected (4 fails, cites hk-iaj1w) | **P1** | REVERSION (iter19 N1 was FIXED-CONFIRM) | stilgar | NEW bead → digest captain |
| 3 | watch-stalled/watch-down escalation storm — 410 msgs, false vs live presence; watch is a zombie (present, not consuming since 06-30) | **P1** | RECURRING-WORSE (iter19 N-A) | stilgar | NEW bead → digest captain |
| 4 | leto keeper gauge dead ~23h, never re-armed; 291 `no_gauge` (275 leto) | **P1** | RECURRING-WORSE (iter19 N-C) | stilgar | NEW bead → digest captain |
| 5 | Reviewer verdict truncated/absent still wedging (13 run_failed) | **P1** | RECURRING (fix did not eliminate) | stilgar | COMMENT hk-4u1mb |
| 6 | Ledger-dep false-defer: blocker closed ~50s BEFORE deferral fired | **P1** | NEW | stilgar | NEW bead → digest captain |
| 7 | Harness-eval chronic no-progress cluster (eval-* beads, 0 commits, ~36 fails) + reviewer-budget on tiny eval diffs | **P2** | NEW | **duncan** | NEW bead → duncan-q |
| 8 | Cold-cache merge-stage build failure (stdlib import miss) | **P2** | RECURRING (iter19 N-F) | stilgar | NEW bead → digest captain |
| 9 | Keeper handoff cycles abort 24% on `handoff_timeout` (18/75) | **P2** | NEW | stilgar | NEW bead → digest captain |
| 10 | `/clear` unconfirmed 48× (captain 26×) — session_id-flip family | **P2** | RECURRING | stilgar | NEW bead → digest captain |
| 11 | `claim_write_lost` recurring — 3 beads each reconciled twice at back-to-back startups | **P2** | RECURRING-class | stilgar | fold into #1 (restart-churn) |
| 12 | Advisory-RC no-op re-dispatch loop — hk-w2ow re-run 4× over 4 days, never reconcile-closes | **P2** | RECURRING (advisory-RC-hides class) | stilgar | NEW bead → digest captain |
| 13 | Pi-harness fragile hotspot (26 commits, fix-took-3-tries on pibillingguard) | **P2** | NEW (subsystem stabilizing) | **duncan** | NEW bead → duncan-q (hardening) |
| 14 | Worktree leak: 58 worktrees (~3.1G); stale *locked* `agent-a974b0ef…` ~11 days, never reaped | **P2** | RECURRING | stilgar | NEW bead → digest captain |
| 15 | Same dep chain re-deferred whole 4× over 7.5h (churn, not false-defer) | P3 | NEW | stilgar/liet | note in doc |
| 16 | stale_intents floor of 11 never clears despite GC | P3 | RECURRING | stilgar | COMMENT hk-birxh |
| 17 | `comms log --since` rejects `Nd`/`Nh` durations | P3 | NEW (tooling) | duncan/liet | fold into duncan tooling bead |
| 18 | `~/.claude.json` write-lock contention aborts launch (2 fails) | P3 | NEW | stilgar | note in doc |
| 19 | Pi billing-guard fail-closed on missing OPENROUTER_API_KEY (correct, but consumed a slot) | P3 | NEW (config) | duncan | fold into duncan tooling bead |
| 20 | keeper precompact anti-loop guard fired 22× (gurney 18×) | P3 | NEW (guard working) | stilgar | note in doc |
| 21 | daemon-fallback double-commit / index-sweep reverted a committed task file | P3 | RECURRING (checkout-reverts class) | stilgar | note in doc |

---

## Confirmed FIXED (prior Fxx, no recurrence this window)

- **iter19 N5 — dispatch claim-skip spin-loop (hk-403fw / f1dbdfa8):** FIXED-CONFIRM. Only 8 `bead_claim_skipped`
  in 17.8k events (vs ~153-in-6min pre-fix), spread over 4 days, no spin signature. [T: S1, S2]
- **iter19 N3 — keeper sub-threshold warn (hk-eovln):** FIXED-CONFIRM. Both `session_keeper_warn` fire at
  `pct=80, warn_pct=80` (at-threshold); zero sub-threshold warns. [S4]
- **iter19 N4 — watch `commit_landed=true` false IMMEDIATE (hk-ymyhj):** absent. FIXED holds. [S1]
- **Stranded-in_progress auto-reset (hk-l2xd1):** CONFIRMED WORKING. All 6 `reconciliation_mismatch_observed`
  were followed by `beads_reset≥1` same/next tick. detect→reset pipeline healthy. [S2]
- **N3 dedup-on-event_id contract:** FIXED-CONFIRM. 0 duplicate event_ids in 1,886 agent_messages. [S5]
- **Unreviewed-merge class:** CLEAN. 59 `run_completed` all success=true; 0 flagless-RC (all 55 RC + 6 BLOCK
  carry ≥1 flag); the 14 verdict-less reviewer runs all terminate in run_failed, never a successful merge. [S3]
- **reviewer_launched telemetry blackout (iter-prior F7):** FIXED. 368 `reviewer_launched` / 89 uniq runs. [S3]
- **RC-fixup obscured-cause (F9):** IMPROVED. Now emits dedicated `review_fixup_stalled` w/ diff-hash + flags. [S3]
- **N-B remote launch-hang, zero stall detection:** PARTIAL FIX. 29 `agent_ready_stall_detected` now fire
  (was zero); `d737d20a` landed launch→agent_ready stall detection. [S1, S6]
- **N-E gci/lint absent from worktree PATH:** FIXED. `0c6263d1` symlinks `.tools` into worktrees. [S6]

## Reversions to flag (were FIXED, now RECURRING)

- **iter19 N1 (empty-HEAD worktree race, hk-iaj1w "CLOSED fixed fb11aabd")** → finding #2, back.
- **iter19 N2 (truncated review.json, hk-4u1mb)** → finding #5, fix did not eliminate.
- **iter19 N-F (cold-cache merge-fail)** → finding #8, persists into merge stage.
- **iter19 N-A (watch-stalled storm)** → finding #3, WORSE (410 vs 57; added watch-down variant).
- **iter19 N-C (keeper not auto-armed)** → finding #4, WORSE (running-gauge-death, 23h dead).

---

## Load-bearing gap surfaced this iter

The event stream has **no daemon-death / panic / supervisor-restart event type**. A restart is only visible as
the next `daemon_started`; the cause (crash vs revival vs deploy) is invisible to logmine. Findings #1/#2 cannot
be moved past "unexplained revival" without correlating daemon/supervisor **stderr** logs, which the event
stream cannot supply. Candidate hardening: emit a `daemon_shutdown{reason}` / `supervisor_revival{cause}` event.

---

## Key anchors (event_ids)

- #1 crash-loop: `019f2e1f-7e7b-7280-a54c-e5b68daa3f60` (17:14 start), `019f2e22-3b99-7ec9-acaf-923bf199e51b` (17:17); orphan sweeps `bead_in_progress_reset:4` each.
- #2 worktree race: run `019f2c0c`, 2026-07-04T07:34:06–19, beads hk-qe596/hk-gb3ln/hk-yw5c/hk-zhysl, base `9c3903b7`.
- #3 watch storm: 208 watch-stalled + 202 watch-down to captain, alert #181 elapsed "~96h"; 59 fired after watch presence resumed 06-30T21:00Z. watch last *sent* 06-30T21:01Z but beaconed presence through 07-04.
- #4 leto keeper: first stale 2026-07-03T20:13Z → last 2026-07-04T19:38Z, payload `{agent_name:leto, reason:stale}`, 275/291.
- #5 reviewer verdict: 7 `ErrMalformed EOF`, 1 schema_version-missing, 5 no-verdict; anchors 06-30T16:01 (hk-mkcwg), 07-03T19:31–20:08 (5 consecutive eval).
- #6 false-defer: hk-rlxgx deferred 04:10:08 (`019f262b-6f3e-78c6-ad15-b94d73f9d406`) on hk-p7smp already closed 04:09:18.

> high-water: 019f2ea6-ec15-71c1-be14-2040698d3324
