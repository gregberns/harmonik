# logmine iter-17 — Findings (2026-06-24)

**Window:** line-anchored from iter-16 high-water `019ef5da` → 8,622 events (lines 130409–139031)  
**Coverage:** 2026-06-23T18:59Z → 2026-06-24T~19:00Z  
**Commits in window:** ~60 commits, 16 unique beads (all closed) + codex auto-commits  
**New initiatives landed:** Wake-economy WE1–WE8, BL1/BL2/BL3 reconciliation detectors, ops-monitor fixes, lazy-boot gate (Lever 3), boot-spike Lever 1

---

## Prioritized Register

| ID | Sev | Finding | Lane | Status | Bead |
|----|-----|---------|------|--------|------|
| N1 | P1 | ops-monitor fires every 5m for entire persistent-condition window — 54% bus noise | crew:stilgar | filed | hk-ohb8p |
| N2 | P2 | Leto keeper config_rejected: missing operator_turn_lookback + post_answer_grace — ran unprotected | crew:logmine | filed | hk-zou19 |
| N3 | P2 | Intent GC dead: stale_intents=11, intents_gc_d=0 across all 4 orphan sweeps | crew:stilgar | filed | hk-birxh |
| E1 | P2 | run_completed still lacks workflow_mode field (run_started resolved) | crew:paul | enriched | hk-zhysl |
| E2 | P3 | hk-a8yve genuine no-op bead fails with exited-without-HEAD — codex DOT false-failure | crew:duncan | enriched | hk-a8yve |
| E3 | P3 | hk-slvko verdict-absent (reviewer launched, no verdict emitted, codex harness) | crew:duncan | enriched | hk-slvko |
| W1 | P1 | Spawn-cap saturation burst (19:33–19:42Z): 3 run_failed + 6 spawn_cap_blocked + 3 launch_stall | watch | self-healed | — |
| W2 | P2 | hk-fozq: 3 consecutive implement failures seen — RESOLVED: bead was closed pre-window (superseded by hk-uc1q3 + hk-998cb) | — | closed | hk-fozq |
| W3 | P2 | Go build cache corruption: 3 beads hit stdlib-not-in-std error (04:38–09:23Z) — all self-healed | watch | self-healed | — |
| W4 | P2 | Captain handoff_timeout ×3 on session ffdc15e1 — correlates with 03:49 daemon restart | watch | self-healed | — |
| DK1 | P2 | BL3 operator_escalation_required event type not yet observed in production — new detector not yet triggered | digest | — | — |
| DK2 | P2 | Wake-economy new event types (WE1–WE8) absent from bus; watch uses agent_message only — no structured telemetry | digest | — | — |
| DK3 | P3 | 56 stale worktrees (>1 day) + 1 locked worktree (agent-a974b0efc2bf94289) | watch | accumulating | — |
| DK4 | P3 | 7 dot runs used single-reviewer `review` node (not triple review_correctness/design/tests path) | digest | — | — |

---

## Slice Summaries

### Slice 1 — Run Failures
- 20 run_failed total: spawn_cap×3, no_head_advance×8, daemon_orphan×3, merge_build_failed×3, verdict_absent×2, close_needs_attention×1
- Fleet-wide canary pairs: 21:51Z (hk-fozq+hk-usn8o, same HEAD) and 23:29Z (hk-uc1q3+hk-ryjxk) — both pairs self-healed
- 13/16 failed beads = FIXED-confirm (closed in window). hk-9cj9f/hk-a8yve/hk-slvko = codex canaries (open, noted above)
- Go build cache TOCTOU: 3 beads hit partial cache wipe in 5h burst, self-healed on retry (recurring known issue)

### Slice 2 — Reconciliation + Ledger-Dep
- 26/26 reconciliations succeeded (100%). Zero ledger-dep deferrals active.
- BL3 `operator_escalation_required` never fired (detector landed but condition not yet triggered)
- 17 group_failure auto-pauses (baseline; slight decrease from iter-16 ~20)
- **[T]** stale_intents=11 constant across all sweeps (Slice 2 + Slice 4 triangulated)

### Slice 3 — Review Loop
- 276 launched / 267 verdicts — 9-event gap fully explained by 4 failed runs (no unreviewed successful merges)
- Verdict tally: 237 APPROVE (88.8%) / 29 REQUEST_CHANGES (10.9%) / 1 BLOCK (0.4%)
- BLOCK on hk-27ghc (BL1/BL2/BL3 — missing tests + spec-code gap): protocol worked, bead recovered and closed ✓
- `run_completed` confirmed missing workflow_mode in all 58 events; `run_started` has it in all 77

### Slice 4 — Daemon Lifecycle + Keeper
- 4 daemon restarts (3 within 2.5h at 20:38/21:57/22:54, then stable 5h gap to 03:49) — correlates with active daemon-reliability work
- 7 session_keeper_warn; 0 session_keeper_act (no keeper-triggered restarts)
- session_keeper_config_rejected for leto (missing required keys) → filed hk-zou19
- 26 session_keeper_clear_unconfirmed (known /clear no-ACK issue, elevated but expected)
- Wake-economy: captain health tick (cron_tick) = 0 ✓ WE cutover working; new watch event types absent (uses agent_message)

### Slice 5 — Comms Bus
- ops-monitor supervisor-down storm: 215/400 messages (54%) — single condition, 10+ hours
- No liet/logmine identity confusion; routing clean
- Captain response time: 1–7 min on all crew escalations ✓
- No topic==assign for logmine; logmine self-terminated at 19:44 (prior window) as expected
- Dual-topic storm (ops-monitor + ops-CRITICAL simultaneously) doubles per-event noise

### Slice 6 — Git Churn + Daemon Log
- 60 commits, 16 beads all closed; 0 reverts/fixups; 8 codex auto-commits (fallback path active)
- High-churn: reconciliation.go/workloop.go/daemon.go (3–5 commits each) — active feature work, not rework
- Disk: 60% full (/ 228GiB, 17GiB used, 11GiB free) — watch
- 64 worktrees total (23 .claude/worktrees/agent-*, 1 locked); down from 78 in iter-16
- Flywheel tmux session present — confirmed active flywheel-v1-completion work (hk-dbpp/wxd8/xq1i open)

---

## Cross-run Fixed-Confirm vs RECURRING

| Prior finding class | iter-17 verdict |
|---------------------|----------------|
| Spawn-cap saturation bursts | RECURRING (self-healed; known saturation not leak) |
| Daemon orphan on restart | RECURRING / MITIGATED (auto-recovery works) |
| Go build cache TOCTOU | RECURRING (3 hits this window, self-healed; root fix pending) |
| group_failure auto-pause ~20/window | RECURRING (baseline at 17, normal) |
| Verdict-absent reviewer | RECURRING (hk-slvko in codex harness path) |
| Intent GC stale_intents=11 | RECURRING (persistent for ≥2 windows; now filed) |

---

## Bead Map

| Finding | Bead | Action |
|---------|------|--------|
| N1: ops-monitor no-suppress | hk-ohb8p | filed; digest to captain (crew:stilgar) |
| N2: leto keeper config gap | hk-zou19 | filed; dispatch candidate (crew:logmine) |
| N3: intent GC dead | hk-birxh | filed; digest to captain (crew:stilgar) |
| E1: run_completed workflow_mode | hk-zhysl | enriched with comment |
| E2: hk-a8yve false no-op failure | hk-a8yve | enriched with comment |
| E3: hk-slvko verdict-absent | hk-slvko | enriched with comment |

---

> **Window:** line-anchored from iter-16 high-water `019ef5da` → 8,622 events, lines 130409–139031.
> high-water: 019efb0a-6986-7795-a679-e45c300bb104  (2026-06-24T19:10:24Z governor_signal — last event in events.jsonl at findings-write time; next run resolves THIS event_id to its line and slices forward, per F41a line-anchoring)
