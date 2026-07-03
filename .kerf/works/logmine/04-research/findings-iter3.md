# logmine — Iteration 3 Findings (run 2026-06-13)

Crew **logmine** (was liet) · epic **hk-mhmaw** · queue **logmine-q** · window **2026-06-11 ~13:30 →
2026-06-13 02:52 local** (continues `findings-iter2.md`). F-numbering continues at **F37**.

**Method:** 6 read-only parallel sub-agents over distinct slices — (1) failures/wedges + sub-agent
transcripts, (2) reconciliation/ledger-dep, (3) review loop, (4) daemon lifecycle/keeper/queue,
(5) comms bus, (6) daemon stdout + git churn + qa-scratch. Window ≈ **4,846 events** (lines 15749→20594
of events.jsonl), 338 comms messages, 147 trunk commits, 814KB daemon log. Every finding is deduplicated
across slices and anchored to a durable artifact (event_id / file:line / commit sha). **[T]** =
triangulated across ≥2 independent slices. Method rule honored: structured `jq` over `.timestamp_wall`,
no hand-grep by run_id (F14).

## Headline

**The window is operationally HEALTHY — the iter-2 fix wave largely held.** Review loop converges
(54 APPROVE / 2 RC→APPROVE both at iter-2; **ZERO unreviewed merges even across the review-loop→DOT
workflow switch** at 06-12T03:46 — the DOT-cascade `reviewer_launched` fix 9816975c holds); reconciliation
nominal (40 cycles, <1ms, complete payloads); no real daemon crashes (disk 89% / 21GB free); no queue
poison; **F29 context.json pollution FIXED** (0 force-adds in 147 commits); **F33 tree pollution FIXED**
(commit 66422893 untracked issues.jsonl + gitignored cruft); **keeper now DEPLOYED and firing for crews**
(F36 root cause cleared).

**The one material NEW operational event is the spawn-semaphore slot-leak (F37 / hk-4l7zs-class)** — a
daemon-wide spawn wedge at window-start (`-c8`) that drove a +687% re-hydration-churn spike (F36 symptom).
It was mitigated mid-window by three layered moves: drop to `-c4`, `hk-hzj` (spawn stagger), `hk-yaj`
(SpawnWindow self-heal). **Next run must verify the wedge does not recur at higher concurrency.**

> The remaining open items are low-grade recurring noise (F28 run_stale false-positives, F30 test-output
> tee, F40 ShowBead-post-restart) plus one keeper-reliability signal (F39 captain handoff-timeouts).

## Register (prioritized) — NEW / RECURRING this window

| ID  | Finding | Pri | Lane | [T] | Bead |
|-----|---------|-----|------|-----|------|
| F37 | **Spawn-semaphore slot-leak recurred** — daemon-wide spawn wedge 06-11T13:29 at `-c8`: 3 of 4 active runs stuck at `launch_initiated`, no `run_started` (hk-ya51z, hk-da3rr [self-recovered ~13m], hk-fejtb). Captain ack'd as hk-4l7zs class. Mitigated mid-window: `-c4` (06-12T03:46) + `hk-hzj` spawn-stagger (06-12 06:22) + `hk-yaj` SpawnWindow self-heal (06-12 15:16). | P1 | stilgar | [T]×3 | **enrich hk-3js5m** |
| F38 | **`run_stale` false-positive on reviewer-launch nodes** — 19 `run_stale` in-window, ALL benign (work landed on main); 0% precision. 10-min detector too tight for the reviewer-launch node. (Sharper restatement of F28; prior reviewer-timeout bead hk-sah87 closed but the run-level detector is a *different* mechanism.) | P2 | stilgar | [T] | **hk-0z2** |
| F39 | **Keeper handoff-timeout cluster on captain** — 9 `session_keeper_cycle_aborted` (reason=handoff_timeout), ALL captain, dense 06-12T11:42–13:44 (5 in ~2h); + 5 `clear_unconfirmed`. Crews unaffected. Likely interacts with in-flight keeper fixes (forced-clear catch-22 82a6e8b5, retry-after-abort). | P2 | stilgar/keeper (surface-to-captain) | — | **hk-oe1** |
| F40 | **`QM-002b ShowBead failed` (br exit 3) spike post-restart** — ~31 consecutive bead-show failures immediately after each daemon restart until `br sync` settles; non-fatal (retried) but pollutes log. | P3 | stilgar | — | **hk-n2y** |
| F30 | **Test/scenario output tee'd into LIVE daemon log RECURRING** — 39 fixture lines (orphansweep_test, scenario-gate timeouts) still in `/tmp/hk-daemon.log`; the F14 grep-trap enabler persists. Prior bead hk-nun closed. | P2 | stilgar/process | [T] | **enrich hk-nun** |
| F34 | **MEMORY.md over limit RECURRING** — auto-memory index 26.2KB (limit 24.4KB); warning fired this session too (self-own). Lives in `~/.claude/projects/.../memory/` (not repo). | P3 | logmine (self-fix) | — | self-own (trim) |

Lane key: **stilgar** = daemon/keeper/comms code (file/enrich + ping captain; do NOT dispatch from
logmine-q — avoids colliding with the daemon lane). **logmine** = docs/skills/process logmine-q can
dispatch + self-owned fixes. **surface-to-captain** = ops/credential decision.

## FIXED-confirmed this window (record; daemon owns closes — do NOT reopen)

- **F1** detect/repair pace — reconciliation 40 cycles, <1ms each, complete payloads. [slice 2]
- **F3** distinct `event_id` — 0 duplicates in 338 comms messages; N3 holds. [slice 5]
- **F6** `reconciliation_completed` emits with full `{beads_examined,beads_reset,beads_closed}` payload (40×). [slice 2]
- **F7** `reviewer_launched` emit continuous (13:32 06-11 → 01:37 06-13); **holds across the DOT switch** (9816975c). [slice 3]
- **F9** `review_fixup_stalled` class present; 0 firings (no genuine iter-2+ stalls). [slice 3]
- **F11** two-captains `--from` guard — single captain identity, 0 conflicts. [slice 5]
- **F12** no comms-latency false-STALLED class. [slice 5]
- **F22** supervisor-orphan / concurrency drift gone — 1 supervisor; the 8→4 drift is *deliberate* (paired with review-loop→DOT switch). [slice 4]
- **F24** restart-cancellation residual false-fails — every failing bead has its `Refs:` on main; no unmerged re-fails. [slice 1]
- **F27** `no-progress iter-2/3` genuine fail — **CLEARED**: 0 `no_progress_detected`, 0 beads past iter-2, both RC beads converged. [slice 1+3]
- **F29** CHB-023 context.json force-add — **NOT RECURRING**: 0 `git add -f` of gitignored files in 147 commits. [slice 6]
- **F31** crew pre-assign poison + F5 `queue_already_active` — 0 occurrences. [slice 4]
- **F33** tracked session-mutable files — **FIXED** by commit 66422893 (untracked issues.jsonl, gitignored `.claude-pid`/`.beads.bak*`/`docs/qa-scratch/`). [slice 6]
- **F36 (root)** crew re-hydration churn — **keeper now DEPLOYED + firing for crews** (gurney/liet/paul); 0 `daemon_re_hydrate_crew`. (Churn *symptom* still elevated this window — see F37, a different cause.) [slice 4]

## Counter-findings (note; do NOT file)

- Review loop **healthy**: 53 launched / 56 verdicts / 54 APPROVE / 2 RC→APPROVE (4–6m fixup cycles); 0 iter-3; `review_loop_cycle_complete` 33<56 is by-design (fires only on approval-close), not a gap. [slice 3]
- **DOT-mode preserves the review gate** — workflow switched review-loop→DOT mid-window yet zero unreviewed merges. Positive confirmation, worth recording.
- Reconciliation in steady state — 0 closes via reconciliation post-startup; all 53 `bead_closed` via the normal path (no premature/inverse closes). [slice 2]
- Daemon log: 2 "panic" lines + 39 "fail" lines are **test fixtures / operator SIGKILLs**, not crashes; 6 restarts all clean (rc=0); supervisor auto-revives. [slice 4+6]
- Comms bus healthy: 338 msgs, peak 49/hr, 0 mis-routes to offline agents, 0 dual-boot collisions. [slice 5]
- F10 stale-intent GC **inconclusive** (0 `stale_intents_observed` in-window — nothing to GC, not a regression). [slice 2]
- F32 known-wedge re-dispatch (hk-ukx×3, hk-igt×2, hk-o4j13×2) — these are the F37 spawn-wedge re-dispatches, not a separate canary violation. [slice 1, deduped vs 5]

## Bead-filing record (Iteration 3)

- **Enrich** hk-3js5m (F37 spawn-wedge window evidence + mitigation timeline + verify-next-run ask).
- **Enrich** hk-nun (F30 recurrence comment — fixtures still tee'd to live log).
- **NEW** codename:logmine beads: **F38 → hk-0z2** (run_stale reviewer-node FP, crew:stilgar), **F39 →
  hk-oe1** (keeper captain handoff-timeout cluster, crew:stilgar), **F40 → hk-n2y** (ShowBead post-restart noise, crew:stilgar P3).
- **Self-fix** (logmine, non-token): **F34** — trim the auto-memory index under the 24.4KB limit.
- **Surface-to-captain (digest):** F37 verify-next-run, F39 keeper-reliability call, plus the two
  still-open prior CI/credential items **hk-3dz** (F25 OAuth `workflow` scope) and **hk-4mten** (F16 CI
  continue-on-error) — both `.github/workflows/`-gated, OFF per bounds, digest-only.

## Pipeline note

This iter-3 pass confirms the 6-slice method again: it **confirmed 13 prior findings FIXED** and isolated
the single material new event (F37) with a clean mitigation timeline reconstructed across 3 slices. The
fixed-vs-recurring delta is the payoff. The wedge↔mitigation↔code-fix triangulation (comms escalation +
lifecycle `-c4` drop + git `hk-hzj`/`hk-yaj` landings) is exactly the cross-slice value a recurring
harvest exists to produce.

> high-water: 019ec069-b5ca-7dea-ad21-2f994708cf23  (2026-06-13T09:56:53Z / 02:52 local — last event processed by the iter-3 harvest; the next daily run windows from here)
