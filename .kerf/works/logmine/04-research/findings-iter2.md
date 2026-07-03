# logmine — Iteration 2 Findings (run 2026-06-11)

Crew **liet** · epic **hk-mhmaw** · queue **liet-q** · window **2026-06-09 ~21:00 UTC → 2026-06-11**
(continues `findings.md`, which covered 2026-06-08 → 2026-06-09). F-numbering continues at **F23**.

**Method:** 6 read-only parallel sub-agents over distinct slices — failures/wedges, reconciliation/queue
lifecycle, review subsystem, daemon-log, comms-bus, process/workflow/docs. Window ≈ **6,258 events**
(5× the prior pass's 1,248), 599 comms messages, ~180 trunk commits, 688KB daemon log. Every finding is
deduplicated across slices and anchored to a durable artifact (event_id / file:line / commit sha).
**[T]** = triangulated across ≥2 independent slices. Method rule honored: structured `jq` over
`.timestamp_wall`, **no hand-grep by run_id** (per F14).

## Headline

**The in-window run-failure rate (~52%) is mostly FALSE.** The dominant `run_failed` classes are
**post-gate false-fails** — the work was reviewed (APPROVE) and merged to `main`, but the run is recorded
as failed because a *terminal* step (br-close, push) tripped. Genuine failures are ≈19 `no-progress
iter-2/3`. The false-fail noise is itself the problem: it wastes re-dispatch slots, obscures real
failures, and feeds the F14 hand-grep diagnosis trap.

> **Single highest-leverage fix (covers F23 + F24 + the push clusters at once):** when a run's `Refs:`
> commit is already on `main`, the daemon should reconcile it as **success-with-warning**, not `run_failed`.

## Register (prioritized) — NEW / RECURRING this window

| ID  | Finding | Pri | Lane | [T] | Bead |
|-----|---------|-----|------|-----|------|
| F23 | br-close `BrUnavailable` terminal-transition **false-fail** — root cause: `.beads/.br_history` ~3.5MB snapshot bloat → `br close` >10s → SIGKILL (hits even at `-c1`); work lands, run marked failed | P1 | stilgar/daemon-infra | [T]×4 | hk-hdbls, hk-g8hv2, hk-hypbi (enriched) |
| F24 | restart-cancellation residual false-fails — every bead failing ≥2× has its `Refs:` on main; improved (ctx-cancel 24→6) but not eliminated | P2 | stilgar | [T] | F2 class (hk-lhv8i pre-screen) |
| F25 | daemon push token lacks GitHub **`workflow` OAuth scope** → any `.github/workflows/` edit rejected (`refusing to allow an OAuth App…`); 6–8 runs, ~5 beads wedged on release.yml; **blocks F16/CI-restoration** | P1 | surface-to-captain (credential) | [T] | **hk-3dz** |
| F26 | `~/.claude.json` trust **write-lock starvation** under wide concurrency — 16 acquire-timeout retries on one bead at `-c8` (`claudetrust_wm040b.go:36`) | P1 | stilgar | — | **hk-z16** |
| F27 | `no-progress iter-2/3` is the dominant *genuine* fail (19); `review_fixup_stalled` event only fires on **flagged** stalls, so most carry no cause | P2 | stilgar/review | [T] | hk-8oy (enriched) |
| F28 | `run_stale` false-positives — 10-min detector too tight for the **reviewer-launch** node; 70/80 stale events fire but the run finishes fine; only ~10 (>20min) are genuine wedges | P2 | stilgar | [T] | hk-sah87 (enriched) |
| F29 | **CHB-023** force-commits gitignored `context.json` onto `main` via `git add -f` (`sessioncontext_chb023.go:157-159`) — **66 of ~180 trunk commits (37%)**, 117 files now tracked, grows ~1/run forever | P1 | daemon-infra (surface-to-captain: design call) | — | **hk-4je** |
| F30 | scenario/`go test` output tee'd into the **live** daemon log → ~30-40% of the "error/fail" surface is test fixtures (the F14 grep-trap enabler); F21 WARN noise classes still undemoted | P2 | stilgar + process | [T] | **hk-nun** |
| F31 | crew **pre-assign** poisons daemon br-claim (`already assigned to chani` ×6) + F5 queue-poison (`queue_already_active -32010` ×14, abated after 06-10T03:30) | P1 | surface-to-captain | [T] | operational (hk-amed0 class; verify hk-u6m4l) |
| F32 | known-wedge bead re-dispatched 4× (canary-class violation, F15 residual) — no dispatch-time skiplist | P2 | process/liet | — | note |
| F33 | tracked session-mutable files (`.beads/issues.jsonl`) + untracked `.claude-pid`, `.beads.bak.*` pollute the tree → `implementer_escaped_worktree` false-fail risk; daemon `reset --hard` clobbers live edits | P3 | liet/process | — | **hk-yru** |
| F34 | `MEMORY.md` index over the 24.4KB limit (30.8KB; warning fired this session) + ~25 retro process-proposals undispatched | P3 | liet/process | — | self-own + note |
| F36 | heavy crew **re-hydration churn** — no session-keeper deployed; each daemon restart cascades a fleet re-hydration round (119 re-hydrate messages) | P2 | surface-to-captain | — | hk-ekap1 + keeper lane |

Lane key: **stilgar** = daemon/comms code (file + ping captain; do NOT dispatch from liet-q).
**liet** = docs/skills/process/CI liet-q can dispatch. **surface-to-captain** = credential/infra/ops decision.

## FIXED-confirmed this window (record; daemon owns closes — do NOT reopen)

- **F1** detect/repair now keeps pace — 8 `bead_inprogress_queue_absent` → 9 `queue_item_reconciled`, each ≤12s.
- **F3** distinct `event_id` per pause event — 0 duplicate event_ids in-window (N3 holds).
- **F6** `reconciliation_completed` now emits (24 in-window, payload `{beads_examined,beads_reset,beads_closed}`).
- **F7** `reviewer_launched` emit restored by the 06-10T04:43 redeploy (06-09 was still dark on old binary).
- **F9** `review_fixup_stalled` event class landed (`event-model.md §8.1a`); residual routing gap → F27.
- **F10** stale-intent ledger now GCs (`stale_intents_observed` 1023→11 at the 06-10T19:28 sweep).
- **F11** two-captains `--from` guard landed (hk-z0f02) — 0 conflicting-identity events in-window.
- **F12** no comms-latency false-STALLED class; "stalled" hits were all real reviewer/spawn stalls.
- **F15** no real-bead wedge-canary re-dispatch except the F32 residual.
- **F17** net-zero smoke trunk-thrash did not recur — **superseded** by F29's net-additive pollution.
- **F22** supervisor-orphan + concurrency drift gone — exactly 1 supervisor + 1 daemon, `-c8` matches `MAXC=8`.
- **spawn-wedge (hk-4l7zs)** not observed — 0 runs match the true signature; the launch/run-started count
  gap is an **artifact** (launch_initiated covers reviewer + per-iteration launches). Do NOT misread it as a slot-leak.

## Counter-findings (note; do NOT file)

- iter-2 review loop is **healthy** and converges: 95×iter-1 / 10×iter-2 / 1×iter-3; 8 runs RC→APPROVE; max iterations capped.
- `review_gate_anomaly` is a **new healthy** early-warning detector (3-consecutive-gate tripwire), not a failure.
- **Zero unreviewed merges**; zero `merge_conflict`; `run_completed` (≈64) == `bead_closed` (≈64).
- No daemon crash / deadlock / nil-panic / ENOSPC / socket-bind failure (`SIGKILL` lines are scenario fixtures + operator restarts).
- Bus / presence / volume **healthy**: 599 messages, peak 61/hr, 1 clean `leave`, no looping agent.
- `exit-137` daemon exits are **operator/redeploy** SIGKILLs, not crashes (supervisor auto-revives in ~2s).

## Bead-filing record (Iteration 2)

NEW beads filed (codename:logmine): **F25, F26, F29, F30, F33** — see register. Enrichment comments added to
existing beads: **hk-hdbls / hk-g8hv2 / hk-hypbi** (F23 history-bloat root cause + success-reconcile ask),
**hk-sah87** (F28 stale-noise angle), **hk-8oy** (F27 all-iter routing). Surface-to-captain (no code bead):
**F25 credential**, **F31 poison**, **F36 keeper**. Self-owned: **F34 MEMORY.md trim**.

## Pipeline note (recurring-automation)

This 6-slice parallel-harvest method is the re-runnable artifact. Iteration-2 confirms it: each slice
deduped against the prior register and **confirmed 12 prior findings FIXED** — the highest-value output of
a *recurring* pipeline is precisely this fixed-vs-recurring delta. Next step (kerf): promote the slice
definitions + dedup/triangulation rules into a `change-spec` so the harvest can be scheduled, not hand-run.

> high-water: 019eb861-36df-7bea-8ca3-6ee1f8867aaf  (2026-06-11T13:30:39 local — last event processed by the iter-2 harvest; the next daily run windows from here)
