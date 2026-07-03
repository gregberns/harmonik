# logmine — findings iter-15 (2026-06-21)

**Window:** line-anchored from iter-14 high-water `019eeb3b-fd35-77bc-b174-38f4f2b5cea1` →
2211 events, lines 42748–44950, 2026-06-21T17:30:42Z → 19:35:34Z (~2h tail).

> **Mission-claim correction:** the mission file (`missions/logmine.md`) claimed last run = iter-10
> (2026-06-18) and pointed at the 06-20 135-commit burst as the high-value unmined window. Ground-truth
> at boot: iters 11–14 already ran (iter-14 completed today 10:38 local, high-water 17:30:37Z) and the
> 06-20 burst is fully mined in iters 12–14. The real unmined window was the 2h tail since iter-14.
> This is iter-15.

## Window shape
2211 events, but **1349 (61%) are `session_keeper_idle_crew` noise** (→ F-iter15-A). Real-signal
~860 events: 34 reviewer_verdict / 34 reviewer_launched, 13 run_started, 10 run_completed, 1 run_failed,
98 agent_message, 18 governor_signal, 3 reconciliation_started / 2 completed, 4 queue_item_reconciled.

## Findings register

| # | Finding | Class | Severity | Route | Bead |
|---|---------|-------|----------|-------|------|
| A | keeper emits `session_keeper_idle_crew` on every no-op below-idle poll (~10.8/min, 1349/2h = 61% of window) → events.jsonl log-spam | **NEW** | P2 | internal/keeper → digest, captain assigns | **hk-qshh8 (filed)** |
| B | scheduled-hourly `reconciliation_started` emits no paired `reconciliation_completed` on no-op (beads_reset=0) | RECURRING (4th+ iter) | P2 | daemon-lane | **hk-v144 (enriched)** |
| C | reaper blind to post-`agent_ready` IMPLEMENTER strands — hk-sj6a silent in_progress ~3.5h, hk-kgwv re-wedged ~33min post-restart | RECURRING, **FIXED this window** | P1 | daemon-lane (paul) | fix landed `3cb51c4b`; hardening **hk-tn36** |
| D | `run_failed` on hk-lacr = `rebase_conflict` in `eagerfill_em063.go` (parallel-worktree merge-ordering); salvaged off-band | one-off | P2 | daemon-lane | self-healed |
| E | 2 beads stranded `in_progress` w/ no queue entry — `bead_inprogress_queue_absent` hk-a8bg.35 / hk-a8bg.16; reconciliation OBSERVES but does not auto-heal (`br show` can't resolve either id) | NEW (low-conf) | P3 | daemon-lane triage | digest only |

## Clean / no-finding slices
- **Review loop:** 34 launched == 34 verdict (no stalled reviewers); 28 APPROVE / 6 REQUEST_CHANGES / 0 BLOCK; **zero unreviewed-merge** (all 10 run_completed have a verdict). hk-kgwv churned highest (8 verdicts, 5/3 split) but resolved. The iter-13 review-bypass class does NOT recur in-window.
- **Keeper handoffs:** 3 handoff_started / 3 clear_unconfirmed / 3 cycle_complete = 3 CLEAN bracketed cycles (95–180s each); clear_unconfirmed is the documented tmux-paste path, each confirmed by a cycle_complete ~1–2s later. No orphaned clears, no hard-ceiling, no held-session force-cut.
- **Comms:** no mis-routes / identity confusion. Captain ran a clean `🔴 FLEET HOLD — DAEMON REBUILD` broadcast @19:33:55Z with per-crew re-task. No unanswered operator escalations. Design note (not a defect): broadcast `captain → *` lands on crews mid-restart before re-join completes; captain mitigates by re-messaging individuals.
- **Governor:** all 18 signals Level=0, LivenessViolated=false, HasOpportunity=true (movement 290–320). Note: governor's 30-min window did NOT flag the reaper-blind strands (looked healthy) — covered by hk-tn36 hardening.

## Cross-run dedup performed
- C → existing **hk-tn36** (fix already landed); digest-only.
- B → existing **hk-v144**; enriched, not duplicated.
- A → no prior bead (leanfleet hk-itoc "noise-cut" is token-noise, not events.jsonl idle_crew spam); filed fresh.

> **Window:** line-anchored from iter-14 high-water `019eeb3b` → 2211 events, lines 42748–44950,
> high-water: 019eebae-6202-730d-8812-55500868b764  (2026-06-21T19:35:34Z governor_signal — last line of the frozen iter-15 window; next run resolves THIS event_id to its line and slices forward, per F41a line-anchoring)
