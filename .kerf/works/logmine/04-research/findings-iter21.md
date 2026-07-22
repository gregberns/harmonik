# logmine — findings iter-21 (crew kynes)

**Window:** `019f2ea6` (iter-20 high-water) → `019f4f2a`. 47,468 events, 2026-07-04T19:42Z → 2026-07-11T03:13Z (~6.5 days, 2.7× prior window).
**Snapshot:** `/tmp/logmine-window-iter21.jsonl`. Slices: 1 run-fail · 3 review-loop · 4 daemon+keeper · 5 comms-bus · 6 transcripts+git. (Slice 2 reconciliation: clean/benign, no signal — carried from iter-20.)
**Headline:** `run_failed`=212 but **>50% are false-fail / self-healing** (restart-orphans 53, self-healing merge retries, verdict-absent all inflate the count). Miners MUST triangulate `git log --all --grep`.

## Prioritized register

| # | Finding | Sev | Lane | Bead action |
|---|---------|-----|------|-------------|
| F1 | Unreviewed merge via hk-du455 preserve-committed-tree branch — 6 dot runs closed with committed code, NO reviewer verdict, steady 07-05→07-11 | P1 | daemon | NET-NEW → captain digest |
| F2 | Review-trailer coverage collapse — 93/127 (73%) implementer commits land with no Reviewed-By/Review-Verdict despite 650 verdicts firing | P1 | daemon | ENRICH hk-x2spu |
| F3 | keeper `no_gauge` flood — 10,276 events (22% of window), per-poll nag on never-armed crews (leto/shannon/schmidhuber). Log-once-per-(agent,reason)-transition | P2 (LOW correctness / HIGH noise) | daemon | NET-NEW → captain digest |
| F4 | Runs orphaned by SIGTERM config-cycle restart — 53 (25% of run_failed) from 40 restarts/6.5d. Re-queue instead of fail; distinct benign event type so miners don't count them | P2 | daemon | ENRICH hk-k0eg |
| F5 | agent_ready_timeout on pi/ornith reviewer nodes — 11/12 lost work (highest work-loss rate of any cluster) | P2 | harness | ENRICH hk-vmxgk |
| F6 | Watch-stall escalation ladder ran to alert #91 / ~43h unresolved (214 ops-CRITICAL) — no auto-recovery, no ceiling | P1 | daemon/comms | ENRICH hk-c7tek |
| F7 | crew-start does not auto-arm keeper → keeper-missing ×34 (stilgar×16, hawat×9…) | P2 | daemon | NET-NEW → captain digest (see hk-nlrwj BLOCKED) |
| F8 | Dangling-branch bloat — 408 local `run/*` (352 unmerged) + 173 `worktree-agent-*`, oldest 2026-05-26, unbounded. Needs merged/orphaned-ref reaper | P3 | daemon | NET-NEW → captain digest |
| F9 | Presence is join-only heartbeat (10,525 join / 1 leave); gurney double-emits (3,583 rows in 26h, pairs ~1s apart) → zombie keys. Distinct reason:"refresh" + single-emit + leave-on-teardown | P2 | daemon/comms | NET-NEW → captain digest |
| F10 | implement exit=127 empty-PATH — 2 runs (`go: command not found` class, 90 tool-errors in transcripts) | P2 | harness | NET-NEW → captain digest (duncan) |
| F11 | governor LivenessViolated fires 47% of cycles (1,379/2,935) — conflates operator-pause quiet with liveness fault; gate on operator_pause_status | P2 | daemon | NET-NEW → captain digest |

## FIXED-confirmed / benign (recurrence classification)
- **verdict-channel mismatch (hk-3tgzt): FIXED** — 0 misrendered verdicts; advisory-RC path correctly labeled.
- **malformed verdict / missing schema_version (prior #5): FIXED** — all 650 verdicts carry schema_version:1.
- **empty-branch worktree_create (`-b " "`): FIXED** — 0 in window.
- **srt argv-wrap tcp-socket (37eca951): FIXED** — 2 hits predate SHA.
- **disk-pressure cache-wipe (merge_build_failed): RECOVERED in-window** — explicit "DISK RECOVERED"; healthy now (12Gi free).
- **restart-orphan / non-ff merge / merge_build / verdict-absent: RECURRING but benign/self-healing** — inflate run_failed, not real defects.
- **precompact hold_skip (178): AS-DESIGNED** (keeper hold override).

## Finding → bead map
- F1 → **hk-nwgj7** (NET-NEW, crew:stilgar, P1)
- F2 → **hk-x2spu** (enriched)
- F3 → **hk-1q7bt** (NET-NEW, crew:stilgar, P2)
- F4 → **hk-k0eg** (enriched)
- F5 → **hk-vmxgk** (enriched)
- F6 → **hk-c7tek** (enriched)
- F7 → **hk-p006e** (NET-NEW, crew:stilgar, P2)
- F8 → **hk-fpjxi** (NET-NEW, crew:stilgar, P3)
- F9 → **hk-ru45u** (NET-NEW, crew:stilgar, P2)
- F10 → **hk-cdpxu** (NET-NEW, crew:duncan, P2)
- F11 → **hk-uxyf1** (NET-NEW, crew:stilgar, P2)

All lanes = daemon/harness → captain digest; NOTHING dispatched to kynes-q this iter (no docs/skills/CI/launch-context findings).

> high-water: 019f4f2a-0453-7dc7-aa31-13d9807f9583
</content>
</invoke>
