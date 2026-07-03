# logmine — Findings iter-8 (2026-06-17)

**Crew:** logmine · **Epic:** hk-mhmaw · **Queue:** logmine-q
**Window:** line-anchored from iter-7 high-water `019ed1ce-b7c3-769f-a5fd-005f77bca6a2`
→ **3055 events** (lines 33259–36313 of events.jsonl), 2026-06-16T19:01Z → 2026-06-17T21:03Z (~26h).
**Method:** 6-slice READ-ONLY fan-out over the frozen snapshot `/tmp/logmine-window.jsonl`
(whole file = window, no timestamp-string filter per F41a). Each slice classified prior `Fxx`
FIXED-confirm vs RECURRING; every finding anchored to event_id / sha / file:line. Slice-6's
whole-file claims (non-ff race, scenario-wedge, merge_fmt_failed) were RE-VERIFIED against the
frozen window and found to be **out-of-window pollution → discarded** (in-window counts: 0/0/0).

---

## HEADLINE

**A heavy fix-landing day — the biggest FIXED-delta payoff of any iter so far.** 80 run_started /
59 completed / 19 failed, 95% APPROVE verdicts, 50 commits to main. **SEVEN prior P1/P2 findings
landed and verified FIXED this cycle** (F55, F56, F49, F62, F57, plus F38/F1/F6/F3/F11/F37-F51
holding clean). Health verdict: **GREEN, with one acute live ops risk (disk 96%) and three new
daemon/comms defects.**

Top three items:
1. **F65 (P1, NEW — ACUTE/LIVE) — disk back to 96%, only 9.1Gi free.** go-build cache = **20G**
   reclaimable (`go clean -cache`). F50 two-iter improving trend REVERSED. ENOSPC spawn failures
   imminent. → **hk-sxlb**.
2. **F64 (P1, NEW [T]) — daemon restart strands in-flight spawn panes → `can't find pane` →
   no_commit.** A gap in F56's two-phase drain: the *implementer-initial paste* window isn't
   covered, so a bead seeded by the new daemon pastes into a dead pre-restart pane. 4 events
   ~3min after the 21:57Z restart; correlates 1:1 with no_commit (the 47%-of-failures class). → **hk-1too**.
3. **F66 (P1, NEW = F58 RECURRING) — ad-hoc implementer identity injects onto the comms bus**
   (`hk-3391-impl`, 21:07Z; iter-7 was `hk-t1wd-impl`). 2-iteration pattern, no tracking bead until
   now. → **hk-xnnd**.

---

## FIXED-CONFIRM (the payoff — prior findings verified resolved this window)

| Prior | What | Evidence this window |
|---|---|---|
| **F55** (hk-2yvx CLOSED) | keeper `operator_attached` event-log spam (was 2603 = 51% of iter-7) | **19 events** total; cadence 5s *only while operator attached*, gated to attach windows. FIXED-confirm [T]. |
| **F56** (hk-86eh CLOSED) | daemon restart cancels in-flight DOT runs | **0** context-cancel within 3s of any of the 8 `daemon_started`; captain comms confirm two-phase drain working. FIXED-confirm. *(But see F64 — drain has a spawn-pane gap.)* |
| **F49** (hk-dyim CLOSED) | merge commits omit `Reviewed-By:`/`Review-Verdict:` trailers | Post-redeploy (fix `6313877e` @10:22, daemon up 17:01) merges carry real trailers — verified on `57509ca2` (`Reviewed-By: agent-reviewer` + `Review-Verdict: {…APPROVE…}`). FIXED (deploy-timing artifact on pre-fix merges). |
| **F57** (hk-4p2h) | hard 20:00 per-node agentic timeout kills opus reviewers | **RESOLVED LIVE during harvest**: irulan landed `b42d289d` @21:16Z (reviewer budgets 5→10 min/kline, ceiling 40→60). |
| **F62** (hk-f1uh CLOSED) | no-progress guard fails one-shot-complete beads | no recurrence; one-shot completions clean. FIXED-confirm. |
| **F38 / F1 / F6 / F3 / F11 / F37 / F51** | reviewer run_stale FP / recon / ledger-dep / comms dedupe / identity / spawn-wedge | all hold clean: 0 reviewer run_stale, 30/30 recon clean (0 beads closed, 1 benign self-heal), 0 ledger-dep deferrals, 815/815 unique event_ids, 8 lane identities only, 0 never-spawned wedge. FIXED-confirm. |

---

## NEW FINDINGS (filed this iter)

- **F64 (P1) → hk-1too** — restart strands spawn panes → pasteinject `can't find pane: %356-%360`
  (4×, 22:00–22:04Z, run_ids `019ed273-7011/710b/85e9`, `019ed277-0cf1`) → no_commit on base
  `ac9d4fc5` (hk-84c2, hk-cr31×2, hk-y3g5). Root: F56 drain doesn't cover implementer-initial
  paste. crew:stilgar (internal/daemon/pasteinject.go,tmuxsubstrate.go). [T] slice-1/2 + slice-4.
- **F65 (P1) → hk-sxlb** — disk 96% / 9.1Gi free; go-build cache 20G (`go env GOCACHE`); worktrees
  only 570M. Durable fix: supervisor periodic `go clean -cache` + pre-spawn free-disk watermark
  guard. Immediate: `go clean -cache` at a lull. crew:stilgar.
- **F66 (P1) → hk-xnnd** — F58 recurrence; ad-hoc `<bead>-impl` identity on comms bus. Decision:
  allow+register vs fail-closed. crew:stilgar (comms identity/registry).

---

## RECURRING — still open (digest to existing lanes, no new bead)

- **F45 (P2) RECURRING** — keeper `warn` fires BELOW `warn_pct`: 6 warns all at pct 27–28 < 30.
  `hk-jgzg` was CLOSED but the warn-threshold path is unfixed. → keeper-redesign (paul); recommend reopen/verify.
- **F46 (P2) RECURRING** — `restart_now_blocked = not_crisp_idle` 5/5. Same `hk-jgzg`. → keeper-redesign (paul).
- **F47 (P2) — LIKELY FIXED post-deploy** — 6 in-window `no_gauge` are all captain & **pre-deploy**;
  paul's reconciliation shows ZERO real `no_gauge` fleet-wide AFTER the ~20:45Z keeper-redesign
  (.sid SessionStart-hook) deploy. Root bead `hk-gfpd` still OPEN → recommend captain/paul verify+close.
- **F59 (P2) RECURRING** — **3 queues currently paused-by-failure with no auto-resume**
  (`019ed692…`, `019ed6fb…`, `019ed737…`, all `reason:group_failure fail_count:1`). The captain
  boot HEALTHY false-green (`hk-t8ew`, doc fixed) is closed, but "queues stay paused" persists. → daemon/ops digest.
- **F13 (P2) RECURRING-WORSENED** — captain↔crew ownership/reopen round-trips = **~42** (iter-7 ~11),
  driven by higher dispatch velocity + crews can't `br update --status=open`. Root fix progressing:
  `hk-1h5q` (reset-gap, extends reset to noChange-timeout + context_cancelled) LANDED `c7062bb7`;
  `hk-60t8` (daemon auto-reset-on-timeout) in flight. → note progress.
- **F43 (P2) RECURRING-cosmetic** — 7 `fmt: auto-format via gofumpt+gci` commits in-window
  (`6b481bb6`,`41e90864`,`58617a79`,`8adcf59f`,`95045b70`,`e27ea4fe`,`5756b073`) but **0**
  `merge_fmt_failed`/`merge_build_failed` in-window → converging, not blocking. Churn only.
- **F67 (P3, info)** — 3 in-window push failures touching `.github/workflows/` (OAuth-app lacks
  `workflow` scope). This is the **known token-gated CI policy working as designed** — friction is
  that beads get authored that edit workflow files. Guard beads away from workflow edits; no fix bead.

---

## Failure-class breakdown (19 run_failed, frozen window)

- **9 no_commit_during_implementer (47%)** — ~4 are F64 stale-pane (real); rest no-work/already-landed (parents reachable from origin/main → false-fails).
- **6 context-cancelled** (implementer wait / node "implement" / commit_gate) — NOT restart-induced (F56 fixed); per-node-timeout/budget class.
- **3 agent_ready_timeout** (hk-01c3, hk-opuv, hk-zxnp) — implementer never responded in 90s; each paused then resumed its queue.
- **1 BLOCK at iteration 1** (hk-axtc) — landed on main anyway (false-fail).

---

> **Window:** line-anchored from iter-7 high-water `019ed1ce-b7c3-769f-a5fd-005f77bca6a2` → 3055 events, lines 33259–36313, 2026-06-16T19:01Z→2026-06-17T21:03Z.
> high-water: 019ed765-21e6-708c-83a2-0e66666cf833  (2026-06-17T21:03:09Z agent_presence — last line of the frozen iter-8 window; next daily run resolves THIS event_id to its line and slices forward, per F41a)
