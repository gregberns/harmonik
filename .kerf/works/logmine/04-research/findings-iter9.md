# logmine — Findings iter-9 (2026-06-18) · POST-INCIDENT

**Crew:** logmine · **Epic:** hk-mhmaw · **Queue:** logmine-q
**Window:** line-anchored from iter-8 high-water `019ed765-21e6-708c-83a2-0e66666cf833`
→ **1123 events** (lines 36314–37436), 2026-06-17T21:03Z → 2026-06-18T00:49Z (~3.75h).
**Mode:** captain-directed POST-INCIDENT harvest — two named incidents mined FIRST, then a
proportionate 3-slice fan-out over the frozen snapshot `/tmp/logmine-w9.jsonl`. Keeper findings
feed `hk-gffc`; daemon fixes route to paul (logmine stayed OFF daemon-package files).

---

## HEADLINE

**GREEN — a clean recovery window with a large FIXED-delta.** 23 run_started / 16 completed /
6 failed, but **0% unrecovered work loss** (every failed bead's work landed on main). Both
operator-flagged incidents are resolved. The keeper-redesign + leanfleet + prior fixes retired a
whole cluster of recurring findings this window.

---

## The two incidents (mined first)

**Incident 1 — cache-reaper merge-outage → CONFIRMED FIXED.** The proactive 60-min `go clean -cache`
reaper was racing in-flight merge-builds, producing `merge_build_failed (go vet: could not import …)`
on hk-bc4x (run 019ed808) and hk-95uf (run 019ed803) — both **false-fails** (work landed; both
recovered on re-dispatch). Fix `5c2276ca` disabled the proactive reaper (kept the reactive
10GiB-watermark reap) and the daemon restarted 00:29:53Z. **Zero** real build/merge/go-vet failure
events after 00:29:53 (by-type scan clean; only comms bodies *quote* the terms). (captain: hk-guez stopgap)

**Incident 2 — hk-e3fy DOT context_cancel strand → ENRICHED.** 3 post-`hk-1h5q`-deploy strands —
hk-3391 (019ed766), hk-95uf (019ed7df), hk-8hr1 (019ed803, TA1 P0) — all `dot: agentic node
"implement" failed: context cancelled`, all stranded `in_progress`, all needed manual captain reset.
Precise gap recorded on **hk-e3fy**: `hk-1h5q` (c7062bb7) auto-resets the *implementer-wait*
context_cancel path but **not** the *DOT-cascade agentic-node-failed* path. Two separable issues —
TRIGGER = ~30-min node-budget cancel (scope/F57 family; hk-8hr1 was over-scoped, since descoped by
captain), STRAND = the missing auto-reset (hk-e3fy, daemon lane → paul).

---

## FIXED-CONFIRM (the payoff)

| Prior | Evidence this window |
|---|---|
| **F64 / hk-1too** (pasteinject stale-pane → no_commit) | **CLOSED** + zero pasteinject_failed / launch_stall events in window. |
| **F58 / hk-xnnd** (ad-hoc `<bead>-impl` identity on bus) | **CLOSED**. The hk-3391-impl recurrence (21:07Z) is the one already captured by hk-xnnd — no new info. |
| **F57** (review-node 20:00 agentic timeout) | FIXED by `b42d289d`; hk-4p2h + hk-s001 reviewers completed APPROVE cleanly, 0 reviewer context_cancel. |
| **F49** (merge trailers) | RESOLVED — all 16 bead_closed merges carry `Reviewed-By:` + `Review-Verdict:` (spot-checked, e.g. a51f24b1). |
| **F47 / hk-gfpd** (captain never holds gauge) | RESOLVED post-`.sid`-deploy — 3 in-window no_gauge are transient `foreign_session`; paul verified 0 real post-deploy. **→ recommend close hk-gfpd.** |
| **F38** (reviewer run_stale FP) | 0 run_stale. **F1/F6** recon+ledger-dep: 5/5 clean, 1 legit reset, 0 deadlock. |
| **F55** (keeper operator_attached spam) | 35 events, NOMINAL (cycle-visibility, not the 2603-flood). **F3/F11**: 134/134 unique event_ids, single captain. |
| **spawn-wedge** (hk-4l7zs) | 0 agent_ready_timeout / no-progress; 23 runs spawned cleanly. |

---

## Still-open / recurring (routed, no new bead)

- **hk-e3fy** (DOT strand) — OPEN, enriched; fix is daemon-lane (paul): extend auto-reset to the
  dot agentic-node-failed branch.
- **F59** (paused-queue pattern) — 5 group_failure pauses (the 5 failed beads); recovered
  **out-of-window** (logmine-q resumed by me; hk-8hr1/hk-95uf reset by captain). The "paused queues
  persist until acted on" pattern stands; the new ops-monitor now alerts on LIVE crew-queue pauses only.
- **F46** (restart_now_blocked=not_crisp_idle, ×4) — NOMINAL this window (legit in-flight work), not a defect.
- **F44/handoff_timeout** (×1, 21:20Z, during the hk-3391 gate-block) — transient; next 3 cycles clean.
- **F45** (keeper warn near/below threshold, ×4 at 27–28%) — keeper-gauge, feeds hk-gffc; the new
  TA1 band (200K/215K) + keeper-redesign should address.

**Self-inflicted-and-already-fixed:** the leanfleet ops-monitor briefly flooded the bus
(`[IMMEDIATE] paused-queue:main` ×4, 23:55–23:57Z) — caught by the captain, hardened + verified via
hk-bc4x (suppress-list + immediate de-dup/cooldown). A clean closed loop: the noise-cutter held to its own bar.

---

> **Window:** line-anchored from iter-8 high-water `019ed765` → 1123 events, lines 36314–37436, 2026-06-17T21:03Z→2026-06-18T00:49Z.
> high-water: 019ed834-01ee-75c4-9043-40f588dd70f6  (2026-06-18T00:49:07Z agent_message — last line of the frozen iter-9 window; next run resolves THIS event_id to its line and slices forward, per F41a)
