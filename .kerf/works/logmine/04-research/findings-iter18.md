# logmine iter-18 — Findings (2026-06-25)

**Window:** line-anchored from iter-17 high-water `019efb0a` → 3,699 events (lines 139119–142818)
**Coverage:** 2026-06-24T~19:10Z → 2026-06-25T~19:00Z
**Commits in window:** 27 commits (11 feat, 8 fix, 3 fmt, 4 chore/docs, 1 test-only)
**Daemon binary:** 83736a96 (pre-fix, 3 restarts) + ba51ad45 (post hk-t1t00 fix, 1 restart at 18:47Z)
**Run outcomes:** 24 completed, 10 failed, 1 stale

---

## Recurrence table (prior Fxx patterns)

| Prior ID | Pattern | iter-18 status |
|----------|---------|----------------|
| F41 (timestamp-TZ mixed) | window-by-event_id not timestamp | APPLIED (line anchor used) |
| DK3 (locked worktree agent-a974b0efc2bf94289) | persistent locked worktree | RECURRING |
| iter-17 N3 (hk-birxh stale_intents=11) | intent GC never runs | RECURRING — comment added |
| iter-17 verdict-absent (hk-slvko) | reviewer exits without writing verdict | RECURRING on hk-h106 (×2 verdict_absent, ×1 agent_ready_timeout) |
| iter-17 N1 (ops-monitor storm, hk-ohb8p) | bus noise from persistent conditions | RESOLVED — 54%→2.3% noise |
| hk-t1t00 commit_gate stale-origin loop | gate loop on pre-fix daemon | RESOLVED after 18:47Z restart |
| spawn_cap_blocked saturation | concurrent launch saturation | ABSENT this window |
| Go build cache TOCTOU | proactive reap mid-build | ABSENT this window |
| daemon_orphan transient | orphan on restart, self-heals | RECURRING (1 event, hk-we6, resolved) |

---

## New findings

### N1 — Remote worker worktree-init empty HEAD (P2) → **filed hk-iaj1w** (crew:stilgar)
3 beads (hk-vo31l, hk-dyqy, hk-gx46b) dispatched concurrently to gb-mbp at 18:58Z. All failed within 2s: `git rev-parse HEAD returned empty in .harmonik/worktrees/<run_id>`. Worktrees created but HEAD uninitialized. Queue auto-paused with `fail_count=3`. Captain disabled gb-mbp. Beads stuck IN_PROGRESS pending orphan sweep. Root shape: concurrent worktree-create races on remote worker path.

### N2 — Malformed/truncated review.json on remote reviewer (P2) → **enriched hk-4u1mb**
hk-h106 run 5 (18:51Z, fixed daemon): reviewer wrote review.json but file was truncated (unexpected EOF). Reviewer lived ~11s — early crash/OOM, not timeout. Prior verdict_absent = file never written; this = file written but partial. Hard run_failed instead of retry. Distinct from hk-4u1mb's main "heartbeats but never writes" case.

### N3 — Keeper sub-threshold warn fires (P2) → **filed hk-eovln** (crew:stilgar)
3 anomalous `session_keeper_warn` events: admiral at pct=21%, pct=20% with warn_pct=80 configured. Should not fire. Secondary trigger path in warn logic bypasses threshold check. Also: 1 `session_keeper_cycle_aborted` on admiral (handoff_timeout at 17:03Z) + immediate `session_keeper_no_gauge` (stale) — keeper lost admiral session after abort. 0 keeper_restart_now across whole window.

### N4 — watch `commit_landed=true` false IMMEDIATE escalation (P2) → **filed hk-ymyhj** (crew:stilgar)
At 18:51Z watch escalated IMMEDIATE: "hk-h106 proof IS on main." Captain corrected: `commit_landed=true` = worktree commit, not merged to main. Watch misreads field name. Risk: false-escalation on any remote bead that implements successfully but fails review. Affects all remote-worker runs.

### N5 — Dispatch claim-skip spin-loop, no backoff (P3) → **filed hk-403fw** (crew:stilgar)
hk-8u2al triggered 153 `bead_claim_skipped` events in 6.3 min at ~2.5s cadence (no backoff). All reason=status_changed_between_select_and_claim, observed_status=in_progress. Produced a duplicate run; both completed successfully. Bus noise + duplicate-run risk. Bead now closed; pattern persists.

---

## Enriched beads (comments added)

| Bead | Reason |
|------|--------|
| hk-4u1mb | Truncated review.json variant (partial-write, ~11s reviewer crash) |
| hk-birxh | RECURRING iter-18: stale_intents=11 across all 4 daemon restarts, 8+ total |
| hk-h106 | 5-failure chain: commit_gate loop (runs 1-4, fixed), truncated-JSON (run 5, remote path) |

---

## Bead Map

| Finding | Bead | Lane | Action |
|---------|------|------|--------|
| N1: remote worktree empty HEAD race | hk-iaj1w | crew:stilgar | filed; digest to captain |
| N2: truncated review.json | hk-4u1mb | crew:stilgar | enriched; digest to captain |
| N3: keeper sub-threshold warn | hk-eovln | crew:stilgar | filed; digest to captain |
| N4: watch commit_landed false-positive | hk-ymyhj | crew:stilgar | filed; digest to captain |
| N5: dispatch claim-skip spin-loop | hk-403fw | crew:stilgar | filed; digest to captain |
| E1: hk-birxh stale intents | hk-birxh | crew:stilgar | enriched (RECURRING) |
| E2: hk-h106 failure chain | hk-h106 | P1 open task | enriched |

---

## Operational highlights

- **commit_gate stale-origin loop FIXED** (hk-t1t00, c417be8f): ba51ad45 daemon resolves gate in 23s vs prior 45+ min loops.
- **ops-monitor storm RESOLVED** (hk-ohb8p): bus noise down from 54% to 2.3%.
- **run-survive-restart LANDED** (hk-o85ye): bead-runs survive daemon SIGKILL.
- **stranded in-progress auto-reset LANDED** (hk-l2xd1): daemon resets orphaned in-progress beads on startup.
- **Worktree count collapsed**: gb-mbp 56–64 → 4–5 worktrees (cleanup landed).
- **Disk 93% full** (13GiB free): proactive reap defers during merge-builds; watchable.
- **DK3 locked worktree** (agent-a974b0efc2bf94289): still locked, persistent.
- **gurney-q manual intervention ×3** in window: operator_drain ×2 + group_failure ×1; pattern of repeated manual work on that lane.

---

## Health trend

| Metric | iter-17 | iter-18 | Direction |
|--------|---------|---------|-----------|
| run_failed | ~17 | 10 | ↓ improved |
| group_failure pauses | 17 | 6 | ↓ improved |
| keeper_warn | 7 | 3 | ↓ improved |
| bus noise (ops-monitor) | 54% | 2.3% | ↓ improved |
| worktree count (gb-mbp) | 56–64 | 4–5 | ↓ improved |
| stale_intents | 11 | 11 | → unchanged |
| keeper_restart_now | 0 | 0 | → same |

---

> **Window:** line-anchored from iter-17 high-water `019efb0a` → 3,699 events, lines 139119–142818.
> high-water: 019f0027-ea3b-7d36-a5c8-191252106ade  (2026-06-25T19:00:43Z agent_presence — last event in window snapshot at findings-write time; next run resolves THIS event_id to its line and slices forward)
