# logmine — Iteration 4 Findings (run 2026-06-13)

Crew **logmine** (was liet) · epic **hk-mhmaw** · queue **logmine-q** · window **2026-06-13
~02:58 → 18:58 local** (continues `findings-iter3.md`; high-water `019ec069`). F-numbering
continues at **F41**.

**Method:** 6 read-only parallel sub-agents over distinct slices — (1) failures/wedges +
sub-agent transcripts, (2) reconciliation/ledger-dep, (3) review loop, (4) daemon
lifecycle/keeper/queue, (5) comms bus, (6) daemon stdout + git churn + qa-scratch. Window =
**1,254 events** (lines 20606→21859 of events.jsonl), 37 comms messages, 40 trunk commits.
Every finding deduplicated across slices and anchored to a durable artifact (event_id /
file:line / sha). **[T]** = triangulated across ≥2 independent slices.

## Headline — the harvest cursor was silently dropping ~90% of the window (F41)

This run surfaced a **methodology defect in the harvest pipeline itself**, and it is the
material finding of iter-4. The cross-run cursor is recorded as a `> high-water: <event_id>`
footer with a UTC-`Z` timestamp. The Wave-1 slices windowed by **string-comparing**
`.timestamp_wall > "<high-water-Z>"`. But **events.jsonl stamps `timestamp_wall` in two
different formats** depending on the emitting subsystem:

- `session_keeper_*` events → **UTC-`Z`** (e.g. `2026-06-13T09:56:53.834913Z`) — 258 events.
- **all daemon-core events** (`node_dispatch_*`, `run_*`, `reviewer_*`, `reconciliation_*`,
  `agent_*`, `launch_initiated`, …) → **local offset** (e.g. `2026-06-13T03:01:29.68477-07:00`)
  — 996 events.

The iter-3 high-water happened to land on a `session_keeper_no_gauge` event (Z-format). String-
comparing a Z-format threshold against a mostly-local-offset stream filters by *local-clock
digits* rather than true chronology, so slices 1–4 saw only events whose local-clock string
sorted after `09:56` — **309 of 1,254 events (~25%), and only the post-09:58Z tail**. The
morning band (02:58→09:56 local, ~7h) was dropped. Because the next run windows from *this*
run's high-water, **that band is never recovered** — every daily run permanently loses a
UTC-offset-sized slice.

**Impact this run:** the first pass reported "1 run, 1 review, 2 reconciliations, zero
failures, 0 daemon churn." The frozen line-anchored window actually contained **~30 runs, 31
reviewer cycles, 9 reconciliations, ≥8 `run_stale` events, and a daemon restart/activation
(~06:20 local)** — the first pass saw ~10% of operational activity. Slices 5 (comms `--since
9h`) and 6 (git `--since 12h`, daemon-log tail) used **duration** filters and were correctly
windowed, which is how the discrepancy was caught.

**F41 splits into two beads:**
- **F41a (pipeline/methodology, `crew:liet`, dispatchable):** the harvest MUST window by
  **event_id / line position**, not timestamp-string. Fix `pipeline.md` Wave-1: resolve the
  high-water `event_id` to its line (`grep -n`), then slice `tail -n +<line+1>` (append order ==
  chronological order). Drop all `.timestamp_wall > "...Z"` filtering.
- **F41b (harmonik defect, `crew:stilgar`, digest-only):** events.jsonl serializes
  `timestamp_wall` inconsistently across subsystems (keeper = UTC-`Z`, daemon-core =
  local-offset). One event log should use one canonical time format (recommend RFC3339 UTC-`Z`
  everywhere). This inconsistency is the root enabler of F41a and breaks *any* downstream
  timestamp-based tooling. **[T]** — confirmed by the format-count split (258 Z / 996 offset)
  and the under-windowing it produced.

---

## Register (prioritized)

| F# | Finding | Sev | Lane | Anchor | Status |
|----|---------|-----|------|--------|--------|
| F41a | Harvest cursor must be event_id/line-anchored, not timestamp-string | P1 | logmine | hk-2azm (self-fixed pipeline.md) | NEW |
| F41b | events.jsonl mixes UTC-Z (keeper) vs local-offset (daemon-core) timestamp_wall | P2 | stilgar (digest) | hk-umao; 258 Z / 996 offset | NEW [T] |
| F38 | run_stale reviewer-node false-positive — fix `77c0d2fa` deployed mid-window (06:21), 0 stale after | P2 | stilgar | hk-0z2 (CLOSED, enriched) | FIXED-confirm (provisional) [T] |
| F39 | keeper handoff-timeout — IMPROVED (1 clear_unconfirmed self-recovered vs iter-3's 9 aborts) | P3 | stilgar | hk-mzdm (enriched) | RECURRING (mild) |
| — | keeper gauges captain as foreign_session 26% / never live (253 no_gauge) | P3 | stilgar | hk-mzdm (enriched) | RECURRING |
| — | `git index.lock` collision on a merge (single, non-fatal) | P3 | stilgar (digest) | — (watch) | NEW |
| F30 | test-output tee'd into /tmp/hk-daemon.log (F14 grep-trap enabler) | P3 | stilgar (digest) | — | RECURRING |
| F34 | auto-memory MEMORY.md index over 24.4KB limit | P3 | logmine | self-fix (this run) | RECURRING |

---

## Slice 5 — Comms bus (CORRECTLY windowed) — HEALTHY, zero friction

37 messages; senders stilgar 24 / captain 11 / logmine 2 (flywheel/duncan/main silent). Topics
status 36 / error 1. Dominant thread: the fleet-portability impl lane — captain re-tasked
stilgar (epic hk-nboa), stilgar ran 21 beads clean on stilgar-q, captain ran coordinated daemon
restart/activation (~06:20 local), stood stilgar down.

Coordination quality all GOOD: `--from` present 37/37 (no identity collision); 37/37 unique
`event_id` (no dup deliveries); 0 mis-routes; the go-install/daemon-restart was announced and
acked both directions before acting; one lane-boundary heads-up (hk-ibb) handled textbook.

| Prior | Status | Evidence |
|---|---|---|
| F3 event_id reuse / N3 dedupe | FIXED-confirm [T] | 37/37 unique event_ids |
| F11 unguarded `--from` two-captains | FIXED-confirm [T] | 37/37 carry `--from`, single captain |
| F12 comms-latency false-STALLED | FIXED-confirm | stale-at-launch warns correctly labeled slow-work |
| F13 assignee-attribution round-trips | FIXED-confirm | stilgar mirrored assignee on adopt; subsumed-bead resolved in one send→ack |

## Slice 6 — Daemon stdout + git churn + qa-scratch (CORRECTLY windowed) — HEALTHY

Counts: 40 commits, 0 reverts, disk **91% / 17Gi free** (down from 89%/21Gi at iter-3 — ~4Gi
in ~13h; watch, no ENOSPC), 0 new qa-scratch files. Window = a docs/spec/feat fleet-portability
+ keeper-fix push.

NEW (both P3):
- **`git index.lock` collision during a merge** — `/tmp/hk-daemon.log:5132` `fatal: Unable to
  create '.../.git/index.lock': File exists`. Single occurrence, non-fatal (daemon retried, run
  merged). Watch for recurrence under concurrency. → `crew:stilgar` digest.
- **Keeper boot-grace fixup-chain [T]** — `internal/keeper/{watcher,cycle,cycle_test}.go` (3×
  each), `cmd/harmonik/keeper_cmd.go` (4×); 4 keeper `fix(...)` commits (`2ba7845e`, `74b1287c`,
  `fd5e1dc7`, `df727a7f`) iterated in-window. Not a defect — a fixup-chain on one subsystem —
  but the keeper boot-grace area is churning hard; pairs with F39 (keeper handoff-timeouts).

| Prior | Status | Evidence |
|---|---|---|
| F40 ShowBead exit-3 post-restart spike | FIXED [T] | fix `d1f57a0e` (br sync at startup); 0 occurrences after high-water |
| F29 context.json force-add | FIXED | 0 `git add -f` of gitignored files across 40 commits |
| F33 tree pollution | FIXED | qa-scratch gitignored; no tracked session-mutable churn |
| F30 test-output tee'd into /tmp/hk-daemon.log | RECURRING | ~20 test-fixture lines still polluting the daemon log (the F14 grep-trap enabler) → stilgar digest |
| F34 MEMORY.md over 24.4KB limit | RECURRING | self-own warning fired again (24.5KB); logmine non-token self-fix |
| disk / ENOSPC class | OK, watch | 91% / 17Gi; no agent-mail stderr.log; no ENOSPC |

---

## Slice 1 — Failures / wedges + transcripts (corrected) — no genuine failures

8 `run_stale` events, **8/8 recovered (0 genuine)**: each fired at age≈600s with `last_event_type:
launch_initiated`, then the same run_id emitted implementer_phase_complete → reviewer → run_completed +
bead_closed, and every bead's `Refs:` landed on main (hk-4f8/ibb/qbx/rcp7/ohd/qp3/7iyh/ndq). This is
**F38** (run_stale on the reviewer-launch/slow-spawn node). **Triangulated with the fix timeline:** the
F38 fix `77c0d2fa` committed 03:45 local and DEPLOYED at the 06:21 daemon restart; all 8 stale fired
*before* deploy (last 06:09), **zero after** → F38 reads as FIXED-confirm (provisional — small post-deploy
sample), NOT a live recurrence. 0 `run_failed`/`group_failure`(real)/`merge_conflict`/`implementer_escape`/
`spawn_failed`/`no_progress`. F37 `-c4` spawn mitigation holding (max active_run_count=4, 0 re-dispatches).
Transcripts: 61 worktree transcripts touched, **0 `is_error`**.

> Slice-1 also flagged a `queue_paused reason=group_failure fail_count:1` on an "all-pass" group as a
> mislabel; **slice 4 corrected this** — the failed item was hk-zlwi (genuinely subsumed / no-commit,
> operator-closed `--reason Subsumed`), not a recovered-stale miscount. Benign; not filed.

## Slice 2 — Reconciliation / ledger-dep (corrected) — CLEAN

9 cycles (8 hourly + 1 startup), all <350ms, **9/9 full `{examined,reset,closed}` payloads**, 0 closes
(all 30 bead closes via the normal completion path, 0 reconciliation-driven), **0 ledger-dep deferrals**,
no hk-dv8qv child-under-open-epic deadlock. F1/F6 FIXED-confirm.

## Slice 3 — Review loop (corrected, full 31-cycle sample) — 0 unreviewed merges

31 reviewer_verdict: **30 APPROVE / 1 REQUEST_CHANGES→APPROVE** (PL-031, converged iter-1→iter-2 in ~6
min) / 0 BLOCK / 0 verdict-absent. **UNREVIEWED-MERGE COUNT = 0 [T]×3** (launched↔verdict, run_completed↔
verdict, and bead_closed→verdict all 30=30=30, empty unreviewed set). DOT review-gate (fix `9816975c`)
HOLDS: 31/31 launches carry `workflow_mode:dot`, 31 `review`-node dispatches. hk-rssrg unreviewed-merge
class and the verdict-absent salvage class both FIXED-confirm over the full sample.

## Slice 4 — Daemon lifecycle / keeper / queue (corrected) — clean + the keeper-gauge drift

1 `daemon_started` (06:21:19 local, pid 49443, `max_concurrent=4, workflow_mode=dot, target=main,
no_auto_pull=true`), no restart-backoff (clean single restart, deployed the F38 fix). 1
`daemon_orphan_sweep_completed` (reaped 1 tmux session; captain/crew/coordinator preserved; gc'd 1 intent,
11 stale observed). **253 `session_keeper_no_gauge`, ALL captain (186 stale / 67 foreign_session,
~2.1-min cadence)** — the known gauge-not-wired drift; the 26% foreign_session share is the new angle
(enriched onto hk-mzdm). 1 keeper handoff cycle (clear_unconfirmed but cycle_complete + self-recovered;
F39 mild recurrence vs iter-3's 9 aborts). Queue: 10 groups, 1 fail = hk-zlwi (subsumed, benign).
F5/F31/F22 FIXED-confirm.

---

## Finding → bead map (Wave 2)

| Finding | Bead | Action |
|---|---|---|
| F41a harvest cursor (pipeline) | **hk-2azm** (NEW, codename:logmine, crew:logmine, P1) | logmine self-fixed pipeline.md this run; not daemon-dispatchable (gitignored bench) |
| F41b events.jsonl mixed timestamp formats | **hk-umao** (NEW, codename:logmine, crew:stilgar, P2) | digest to captain; do NOT dispatch from logmine-q |
| F38 fix validation | **hk-0z2** (CLOSED) | comment added: deploy timeline + 0 post-deploy stale |
| F39 / foreign_session no_gauge | **hk-mzdm** (OPEN, codename:session-keeper, P2) | comment added: 26% foreign_session quantification |
| git index.lock; F30 daemon-log pollution; disk 91% | — | digest-only (crew:stilgar), watch; no new bead |
| F34 auto-memory index over limit | — | logmine non-token self-fix this run |

---

## Pipeline note

iter-4's payoff was a **self-finding**: the recurring harvest's own cursor was dropping a
UTC-offset-sized band every run. Caught only because two of six slices use duration-based
windowing and disagreed with the four timestamp-string slices — a good argument for keeping
redundant windowing methods until F41a/F41b land.

> high-water: 019ec259-c438-7da4-88ec-87ca4b12725c  (2026-06-13T18:58:43Z — last event in the frozen line-anchored window; next daily run resolves THIS event_id to its line and slices forward, per F41a)
