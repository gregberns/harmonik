# logmine iter-19 findings — 2026-06-26

**Window:** line-anchored from iter-18 high-water `019f0027` → 4,342 events, lines 142819–147160.
**Harvest date:** 2026-06-26
**Method:** 6-slice pipeline per `pipeline.md`

---

## Prior findings carry-forward (iter-18 → iter-19)

| Finding | Bead | Status |
|---------|------|--------|
| N1 — remote worktree empty HEAD race | hk-iaj1w | **CLOSED** (fixed fb11aabd; ABSENT this window) |
| N2 — truncated remote review.json | hk-4u1mb | **CLOSED** (reviewer-watchdog fix landed); new wider-retry hk-l489f (af3cd88d) also landed. One N2 recurrence seen 06:33Z on hk-h106 (Slices 1+3 triangulate [T]) — new fix unvalidated on remote as gb-mbp was disabled before retest. |
| N3 — keeper sub-threshold warn | hk-eovln | **CLOSED** (threshold fix merged ~04:40Z). One pre-fix instance observed 03:31Z (admiral pct=20 with warn_pct=80) — was before the fix landed. FIXED-CONFIRM in post-fix period. |
| N4 — watch `commit_landed=true` false IMMEDIATE | hk-ymyhj | **ABSENT** this window |
| N5 — dispatch claim-skip spin-loop | hk-403fw | **CLOSED** (cherry-pick salvaged as f1dbdfa8); **FIXED-CONFIRM** — zero `bead_claim_skipped` events in 4,342-event window |
| stale_intents=11 | hk-birxh | **CLOSED** (intent-GC fix merged ~04:43Z). stale_intents=11 persists at first post-fix restart (05:39Z f1dbdfa8 sweep); may be expected — GC logic now correct but 11 pre-existing records may require a future trigger condition. Monitor next iter. |

---

## Window health metrics

| Metric | iter-18 | iter-19 | Direction |
|--------|---------|---------|-----------|
| run_failed | 10 | 21 | ↑ higher — but STAGE-2 churn accounts for ~8 orphans/restarts |
| group_failure pauses | 6 | 13 | ↑ higher — post-STAGE-2 fast-fail cluster contributes |
| keeper_warn | 3 | 4 (1 sub-threshold pre-fix; 3 at-threshold) | → flat |
| keeper_restart_now | 0 | 0 | → same |
| bus noise (ops-monitor) | 2.3% | 31.9% | ↑ regression — watch-stalled storm (see N-A) |
| worktree count | 4–5 | 78 | ↑ regression — methodology? prior was daemon-only |
| stale_intents | 11 | 11 (GC fix landed mid-window) | → fix in-flight |
| run_completed success | — | 30/30 | ✓ 100% |
| daemon restarts | — | 11 (3 binaries) | STAGE-2 deploy churn |
| bead_claim_skipped | ~153 (hk-8u2al) | **0** | ↓↓ fixed |

**Self-healing rate:** 14/17 failing beads (82%) ultimately landed via git commits or admin close. 3 beads remain OPEN at window end: hk-h106 (P1), hk-3tgzt (P2, held), hk-0lwje (P2, no retry in window).

---

## New findings

### N-A — watch-stalled storm persists after hk-ohb8p fix → **filing new bead** (crew:stilgar)

**Severity: P2** [T: Slices 4+5 triangulate]

hk-ohb8p (closed, landed c973e9e4) gated watch-stalled on `actionable_events_past_cursor`. Despite the fix deploying in STAGE-2 at 05:39Z, **57 watch-stalled IMMEDIATEs fired after the deploy** (82 total in 48h). Root shape: the `watch-stalled` ops-monitor probe fires when the watch session is polling rather than maintaining a persistent subscribe process. The fix's ACTIONABLE_EVENT_TYPES gate doesn't suppress this probe's signal. Comms bus noise jumped from iter-18's 2.3% to 31.9%, mostly driven by this storm. Captain fielded 57 spurious IMMEDIATEs post-fix.

### N-B — Remote agent 36-min launch hang with no stall detection → **filing new bead** (crew:stilgar)

**Severity: P1** [T: Slices 1+3 triangulate]

hk-3tgzt: `run_started` at 05:54Z → `skills_provisioned` at 05:54Z → 36-min silence → never reached `agent_ready` → gurney manually killed agent (PID 79317). Zero `launch_stall_detected` or `agent_ready_timeout` events fired during the 36-min hang. Remote slot was consumed with zero throughput and no alerting. Bead currently OPEN P2, held per captain (local-only re-run vs park). The detection gap means a single hanging remote run can silently consume the remote worker slot for the full run-budget window.

### N-C — Keeper not auto-armed on crew boot → **filing new bead** (crew:stilgar)

**Severity: P2** [Slice 5]

4 crew-boot incidents in 48h where the crew started successfully but without an armed keeper watcher: thufir (20:10Z), leto (20:25Z), paul (04:41Z), stilgar (06:16Z). Each required captain to manually `keeper enable --agent X --tmux Y`. Pattern: no auto-arm on `harmonik start crew` or `harmonik crew stop` + restart. ops-monitor detects the gap via heartbeat absence and escalates, consuming a captain action-cycle per incident.

### N-D — Leto HOLD directive lost via keeper `/clear` (rehydration gap) → **filing new bead** (crew:stilgar)

**Severity: P2** [Slice 5]

Captain sent explicit HOLD to leto at ~04:24Z (do not dispatch codex until STAGE-2 completes). Keeper cycled at ~04:30Z; on resume, leto rehydrated without the HOLD directive and dispatched hk-5uboy + hk-97kvz to codex-lane at 05:28Z. Both failed in 8–10s fast-fail (remote, no HEAD advance). Captain caught it retroactively. Root shape: HOLD/directive state lives in conversation context, not in a persistent store; keeper `/clear` wipes it. This is the documented "Crew wake + context-clear" rehydration gap — crew directives issued between `/clear` cycles are silently lost.

### N-E — gci + golangci-lint absent from worktree PATH → **filing new bead** (crew:stilgar)

**Severity: P2** [Slice 6]

Agent worktrees systematically missing `gci` (import formatter) and `golangci-lint` from PATH. Consequence: fmt-commits pass in the main session (where tools are installed) but fail with Exit 127 in worktrees — agents skip fmt/lint silently and commit non-compliant code. The 6 codex daemon-fallback commits are partly symptomatic of this: without lint catching issues, fallback auto-commits land unchecked on critical files (keeper/cycle.go, keeper/watcher.go, supervisor shim/start). Root: worktree spawn does not inherit the full tool PATH from the main session.

### N-F — disk_low cache-wipe → cold-cache merge-fail 8.5h later → **filing new bead** (crew:stilgar)

**Severity: P2** [T: Slices 1+2+3 triangulate]

`disk_low` fired at 19:43Z with `go_cache_clean_attempted=true` (wiped stdlib go-build cache). At 04:09Z (~8.5h later), hk-birxh's merge-stage `go vet` failed: "could not import bufio/encoding/json/…" — all stdlib cache entries absent. Healed on retry (04:23→04:42). This extends the documented TOCTOU pattern to a slow timescale: the disk-low reap is non-concurrent but its effect persists until the next warm build. Any merge-stage run in the hours after a `disk_low` event is at risk of cold-cache failure. Confirmed in-window by 3 independent slices.

---

## Enriched beads (comments added)

| Bead | Reason |
|------|--------|
| hk-tswe0 | Observation: stale_intents=11 persists at first post-fix restart (05:39Z) — may need next-iter confirmation |
| hk-q54s8 | STAGE-2 succeeded at 05:39Z via f1dbdfa8 cherry-pick salvage (hk-403fw) |
| hk-3tgzt | 36-min remote launch hang (no stall detection) observed as run failure on this bead's queue slot in this window; hk-fzm92 covers the verdict-channel mismatch separately |

---

## Operational highlights

- **hk-tswe0 cluster COMPLETE**: 4/4 paul-lane reliability beads landed (hk-eovln, hk-birxh, hk-4u1mb, hk-403fw salvaged). Major stability improvement.
- **STAGE-2 deploy drama resolved 05:39Z**: f1dbdfa8 via cherry-pick salvage. Two churn clusters (20:22–20:31Z, 02:51–03:20Z) with 5 supervisor false-reverts. No data loss.
- **hk-l489f landed (af3cd88d)**: reviewer retry window widened to ~6.3s. Not yet re-validated on remote (gb-mbp disabled).
- **hk-ymyhj (watch false IMMEDIATE) ABSENT** — good news, no recurrence.
- **N5 (claim-skip spin-loop) FIXED-CONFIRM** — zero occurrences in window.
- **3 beads OPEN at window end**: hk-h106 (P1, remote reviewer), hk-3tgzt (P2, held), hk-0lwje (P2, no retry).
- **Disk still 93%** (14Gi free) — no improvement; continues to pose ENOSPC risk.
- **Stale locked worktree** `agent-a974b0efc2bf94289` still present (6 days, likely dead PID).

---

## Bead map

| Finding | Bead | Lane | Action |
|---------|------|------|--------|
| N-A: watch-stalled storm post-hk-ohb8p | NEW | crew:stilgar | file; digest to captain |
| N-B: remote 36-min launch hang, no stall detection | NEW | crew:stilgar | file; digest to captain |
| N-C: keeper not auto-armed on crew boot | NEW | crew:stilgar | file; digest to captain |
| N-D: Leto HOLD lost via keeper /clear | NEW | crew:stilgar | file; digest to captain |
| N-E: gci/golangci-lint absent from worktree PATH | NEW | crew:stilgar | file; digest to captain |
| N-F: disk_low cache-wipe → cold-cache merge-fail | NEW | crew:stilgar | file; digest to captain |
| hk-q54s8 | STAGE-2 bead | — | enrich: STAGE-2 succeeded 05:39Z |
| hk-tswe0 | paul cluster | — | enrich: stale_intents post-fix observation |
| hk-3tgzt | open P2 | — | enrich: 36-min hang run observed this window |

---

> **Window:** line-anchored from iter-18 high-water `019f0027` → 4,342 events, lines 142819–147160.
> high-water: 019f054e-6520-7d9c-8833-8b06a18755b2  (2026-06-26T19:00:51Z agent_presence — last event in window snapshot at findings-write time; next run resolves THIS event_id to its line and slices forward)
