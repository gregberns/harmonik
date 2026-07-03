# logmine — findings iter-16 (2026-06-23)

**Window:** line-anchored from iter-15 high-water `019eebae-6202-730d-8812-55500868b764` →
**85,451 events**, lines 44959–130409, 2026-06-21T19:35:35Z → 2026-06-23T18:59:38Z (~47h).

> **Mission-claim correction:** `missions/logmine.md` (staffed 06-21T17:28Z) claimed last run = iter-10
> (06-18) and pointed at the 06-20 135-commit burst. Ground-truth at boot: iters 11–15 already ran
> (iter-15 completed 06-21 12:40 local, high-water 06-21T19:35Z); the 06-20 burst is fully mined in
> iters 12–14. The real unmined window was the ~2-day tail since iter-15. This is **iter-16**.

## Window shape
85,451 events, of which **52,431 (61.4%) are `session_keeper_idle_crew` noise** (→ hk-qshh8, still
unfixed) and **23,701 (28%) are `governor_signal`**. Real-signal residue ~9k events: 345
reviewer_launched / 337 reviewer_verdict, 114 run_started / 64 run_completed / 108 run_failed,
615 agent_message, 41/40 reconciliation, 526 bead_claim_skipped, 14 daemon_started.

## Findings register

| # | Finding | Class | Sev | Route | Bead |
|---|---------|-------|-----|-------|------|
| N1 | daemon boot-loop on `br-version-incompatible` (exit 8) ×4 (06-22T23:59→06-23T11:01); ~11h intermittent fleet-down; self-resolved 11:07 (br now 0.2.10). **[T]** slices 1+4 | **NEW** | P1 | daemon-lane → digest | **hk-631kp (filed)** |
| F1 | DOT-mode review-gate **bypass** — 3 dot runs merged with 0 review nodes (hk-aev8t/hk-t48rg/hk-3pbox); verified hk-aev8t graph = start→implement→commit_gate, **no review node**. Correctness defect; iter-13 class recurs | **NEW (HIGH)** | P1 | daemon-lane → digest | **hk-a8xjg (filed)** |
| DK4 | `session_keeper_warn` fires when **pct < warn_pct** (7/7 spurious: pct=20/51/68 vs warn_pct=80); false signal | **NEW** | P2 | keeper-lane → digest | **hk-lbo9w (filed)** |
| DK3 | governor never sets `LivenessViolated` despite ConsecutiveZeroCycles=3274 / MovementScore=0 / HasOpportunity=true; detection blind-spot | **NEW** | P2 | governor-lane → digest | **hk-drygf (filed)** |
| DK7 | orphan-sweep observes `stale_intents` (21→11) but `intents_gc_d=0` every sweep — never GC'd | **NEW** | P3 | daemon-lane → digest | **hk-6umeh (filed)** |
| TX | `hk-7xgu4` re-entrant iter-2 implementer thrash — dominant throughput sink, 19× `total-budget-stale-active` (30min-stale/63min-total). **[T]** slices 1+3+4 | RECURRING | P1 | daemon-lane → digest | **hk-7xgu4 (enriched)** |
| SP | idle_crew spam — 52,431 events (61.4%), 18.5/min, no fix landed | RECURRING (4th+ iter) | P2 | keeper-lane → digest | **hk-qshh8 (enriched)** |
| CW | review-gate cry-wolf — 2 live occurrences (budget-exceeded→relaunch, both benign) | RECURRING | P2 | logmine-q | **hk-spx63 (enriched)** |
| DOC | agent-comms skill: recv uses `--agent`, send/join/leave/who use `--name` (flag-split trap; hit ×2 this iter) | NEW (process) | P3 | **logmine-q (dispatchable)** | **hk-86lli (filed)** |

## Notable false-leads corrected
- **"108 run_failed" is inflated:** 77/108 are a SINGLE daemon-restart orphan burst @06-22T10:19 (all
  within ~200ms). Genuine failures = 31, and **all 31 landed on retry** — no literal in_progress strand
  (so `hk-e3fy` did not bite this window; bead stays OPEN, no fresh evidence).
- **`comms recv --follow` is NOT broken** (refutes a live suspicion + possibly stale-open `hk-yw5c`):
  verified streaming alive past 6s, exits only on SIGINT. The trap is the `--agent`/`--name` flag split
  (→ hk-86lli). Captain may want to spot-verify whether hk-yw5c is stale-open.
- **`hk-y3frr` (cache-reaper TOCTOU) CLOSED but RESIDUAL:** 7 in-window `merge_build_failed (go vet)`
  go-cache-wipe failures predate the fix `af4256c7`; stopgap (reaper-off) still active per `7c3160db`.
  No clean post-fix recurrence spotted — lean FIXED-confirm; worth a close-out check.

## Clean / no-finding slices
- **Reconciliation:** F-iter15-B FIXED (hk-v144 CLOSED 06-21; scheduled-hourly now emits paired
  completed at parity); F-iter15-E now **self-heals** (in_progress-queue-absent observed AND reconciled
  via `claim_write_lost`, e.g. hk-pqgtm observed 12:30:52 → reconciled 12:30:54 → closed). 0 ledger-dep
  deferrals. The single unpaired recon `started` was a restart-interruption artifact, benign.
- **Comms:** CLEAN — 648 messages, no mis-routes / identity confusion / wake failures / dropped operator
  escalations. agent_presence: 27 joins / 0 leaves (restart re-joins, harmless; `comms who` can show
  ghost-online post-restart). captain→* broadcast mid-restart race held but bit nothing (N3 cursor covers).
- **Keeper handoffs:** 31 clean cycles + 5 clean `cycle_aborted` (defined terminal, not silent hang) + 0
  silent orphans + **0 `session_keeper_act` force-cuts** (no session hit hard ceiling).
- **Review verdict tally:** 300 APPROVE / 37 REQUEST_CHANGES / 0 BLOCK. The 8-event launched↔verdict gap
  is fully explained: 7 reviewers killed mid-review by their run's `run_failed`, 1 truncated at window edge.
- **Git churn:** 94 commits / 52 beads, 0 fixup/squash chains, 0 true reverts, 0 real CWD-drift/exit-17
  (46 mentions, all spec-prose, 0 `is_error`). daemon.go top hotspot (8 commits) = healthy feature
  velocity (8 distinct beads), not rework. Disk fine (/ 45%, 20Gi free).

## Watch-items (digest, no bead)
- **Worktree accumulation:** 78 worktrees total (38 `.claude/worktrees/agent-*` Agent-tool isolation
  leftovers, oldest pre-window 06-19; not auto-prunable, on-disk dirs remain). Disk not yet pressured.
  logmine-q cleanup candidate, low priority.
- **group_failure auto-pauses:** 20 queue_paused via group_failure (queues self-paused on bead failures)
  — the failure→pause path, tracked under hk-e3fy / hk-7xgu4.

## Cross-run dedup performed
- N1, F1, DK4, DK3, DK7 → no prior bead; filed fresh.
- TX → hk-7xgu4 (enriched). SP → hk-qshh8 (enriched). CW → hk-spx63 (enriched).
- hk-v144 CLOSED (recon F-iter15-B fixed); hk-y3frr CLOSED (residual noted); hk-yw5c likely stale-open.

> **Window:** line-anchored from iter-15 high-water `019eebae` → 85,451 events, lines 44959–130409.
> high-water: 019ef5da-3466-77cc-825d-7ea24de70190  (2026-06-23T18:59:38Z agent_ready — last line of the frozen iter-16 window; next run resolves THIS event_id to its line and slices forward, per F41a line-anchoring)
