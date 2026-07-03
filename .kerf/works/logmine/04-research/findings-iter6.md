# logmine — Findings iter-6 (2026-06-15)

**Crew:** logmine · **Epic:** hk-mhmaw · **Queue:** logmine-q
**Window:** line-anchored from iter-5 high-water `019ec829-2fb1-791c-af0c-80707ae55b6a`
→ 2002 events (lines 26184–28185 of events.jsonl), 2026-06-14T22:03Z → 2026-06-16T04:26Z UTC.
**Method:** 6-slice READ-ONLY fan-out over the frozen snapshot `/tmp/logmine-window.jsonl`
(whole file = window, no timestamp-string filter per F41a). Each slice classified prior `Fxx`
FIXED-confirm vs RECURRING; every finding anchored to event_id / sha / file:line.

---

## HEADLINE

**The window is a remote-substrate sprint, not a daemon batch.** ~20 of 30 commits are
`feat/fix(remote-substrate)` **gap #7** work committed *directly* by interactive captain/crew
sessions (signature: `Co-Authored-By: Claude Opus 4.8 (1M context)`, NO `Refs:`-merge trailers),
plus a few salvage cherry-picks. The daemon's own queue was light; its handful of beads merge-stalled
then salvaged. **No work was lost** — every "failure" landed on main (false-fail rate ~100%).

Health verdict: **GREEN-but-noisy.**
- Reconciliation + ledger-dep: cleanest domain (35 runs, 0 errors, 0 deferrals). FIXED-confirm.
- Review loop: 17 launched / 17 verdicts / 0 unreviewed-merges. Healthy. But every merge-stalled
  bead recovered via salvage (no auto-re-dispatch).
- Comms bus: 122 messages, 0 identity collisions, all FLEET HOLD/GREEN locks respected. GREEN.
- Disk: **IMPROVED** to 88% / 23 GiB free (iter-5's 94%/12 GiB note was off-trend). FIXED-confirm.

Material items: one NEW remote-substrate worktree-isolation race (the only genuine NEW defect),
plus three RECURRING daemon/keeper items that enrich existing beads.

---

## FINDINGS REGISTER (iter-6)

| ID | P | Finding | Anchor | Class | Disposition |
|----|---|---------|--------|-------|-------------|
| F48 | P2 | **Remote-substrate worktree-isolation loss** — daemon reports missing `.harmonik/worktrees/<run_id>` on the worker node; chdir error; 4-dispatch escalation (fmt→missing→no-advance→timeout) on hk-rs-validate-remote-898a. Suggests TOCTOU race in remote worktree lifecycle. [T] | event `019ec983`; run_failed chain on hk-rs-validate-remote-898a | **NEW** (daemon/remote code) | FILE crew:stilgar bead (NEEDS-REPRO); enrich hk-rs-validate-remote-898a; digest to captain |
| F37/F51 | P1 | **Spawn-semaphore stale at `launch_initiated`** — 9/13 run_stale fire at launch_initiated age ~600s; never-spawned launch-wedged runs. hk-4l7zs slot-leak class not fully closed. [T] (slice1+slice4) | run_stale `019ecab*`,`019ecb3*`,`019ecb9*`,`019ecbb*` | **RECURRING** (daemon) | ENRICH hk-0z5x (no_commit reaper misses NEVER-SPAWNED launch-wedged runs — exact match) + hk-3js5m |
| F45 | P2 | **Keeper warn emitted BELOW threshold** — 4/19 warns at pct 27–28 < warn_pct 30. `belowWarnThreshold` (sessionkeeper, ~line 456–458) Tokens-vs-Pct path bug persists despite iter-5 diagnosis. [T] | `019ec890`,`019ecb05`,`019ecb5f`,`019ecbba` | **RECURRING** (daemon/keeper) | ENRICH hk-4zy9 with sub-diagnosis |
| F47 | P2 | **Keeper no_gauge for captain** — 751 events (722 stale + 29 foreign_session) over continuous ~30h; captain never regains gauge connection; keeper-rebind/auto-recovery (hk-mejt) not catching it. Ties to F46 restart_now_blocked (7×, not_crisp_idle). [T] | `019ec82a`…`session_keeper_no_gauge` span; `019ec835` restart_now_blocked | **RECURRING/WORSENING** (daemon/keeper) | ENRICH hk-4zy9; relate hk-tt9q (crew gauge wiring), hk-xjlq (captain restart-now) |
| F49 | P2 | **Main commits carry NO Reviewed-By/Review-Verdict trailer** — 0/30 in 36h. RECONCILED: review *events* fire (17 verdicts) but window's commits are direct interactive remote-substrate work + salvage cherry-picks (both bypass daemon trailer-write). NOT a review-loop outage. | `git log --since=36h` trailer scan; sampled c5669a16/04a9bddf/6b0343e6 = Co-Authored-By only | **NEW (process)** NEEDS-VERIFY | Digest to captain: verify a *clean daemon merge* still writes trailers; if so, benign |
| F50 | P3 | **Disk IMPROVED** — 88% used / 23 GiB free (was 94%/12 GiB iter-5). 38 worktrees / 462M, no leak; orphan sweep keeps pace (8 sweeps). | live `df -h`; `git worktree list` | **FIXED-confirm (trend reversed)** | Correct iter-5's note; no action |
| F51b | P3 | **CI revert tug-of-war** — 67ef8a6c reverts 11b761af (`continue-on-error` in scenario.yml). | sha 67ef8a6c↔11b761af | **RECURRING** (CI lane) | DIGEST-ONLY (bounds: do NOT touch .github); relate hk-963f, hk-gnsh |
| F52 | P3 | **Daemon restart churn** — 8 daemon_started/26h, `binary_commit_hash:"unknown"` (supervisor/keeper respawn, not crash+rebuild); clusters at deploy cycles. | 8× daemon_started; correlates slice-5 RESTART-TO-LOCAL | **BENIGN** (deploy cadence) | No bead; correlates with active gap#7 deploy coordination |
| F53 | P3 | **context-cancelled during implement** — 2 beads (hk-rs-b9, hk-1vlz), both recovered. Root unknown (keeper handoff / operator /quit / DOT cutoff). | `019ec84f`,`019ecbb4` | **MONITOR** | Watch iter-7; no bead (2×, both recovered) |
| F41b | P2 | **events.jsonl mixed-format timestamp_wall** — keeper=UTC-Z, daemon-core=local-offset; still present (the reason we line-anchor). | keeper vs daemon event ts | **RECURRING** | Recommend captain RE-OPEN hk-umao (iter-5 noted it closed prematurely) |
| — | — | **hk-rs-validate-remote-898a work landed (8e84dff9) but bead still OPEN** | git log; br list | reconcile candidate | Flag to captain (not a logmine close; daemon owns terminal) |

### FIXED-confirm this window (recurrence payoff)
F1/F6 reconciliation+ledger-dep · F3/F11 comms dedupe+identity · F38 reviewer-node run_stale
false-positive (0 reviewer stales) · F42 commit_gate-cap strands work (auto-salvage fix ea796f80
/ hk-1vlz deployed; 0 new occurrences) · F43 fmt-convergence (8fbb79df gci-before-gofumpt on main)
· F33 tree-pollution holds · F29 no context.json force-add · F41a harvest cursor (line-anchored
window captured full 2002-event span correctly).

### False-fail verification (slice 1, git-triangulated — ALL merged)
| Bead | merged sha | run_failed cause |
|------|-----------|------------------|
| hk-rs-b7-worktree-ieb1 | 019a1131 | rebase_conflict |
| hk-rs-b9-liveness-1m9n | fa03ee35 | merge_build_failed + context_cancelled |
| hk-rs-validate-remote-898a | 8e84dff9 | fmt→missing-worktree→no-advance→timeout (F48) |
| hk-as8z | a90f95c8 | merge_fmt_failed (T9+T11 already on main 41e4fcdb) |
| hk-1vlz | ea796f80 | context_cancelled |

---

## LANE ROUTING (Wave 2/3)
- **F48** (remote/daemon code) → FILE `crew:stilgar` bead, digest to captain. Do NOT dispatch from logmine-q.
- **F37/F51, F45, F47** (daemon/keeper) → ENRICH existing beads (hk-0z5x, hk-4zy9). Daemon lane → digest, no dispatch.
- **F49, F41b, reconcile flag** → digest to captain.
- **F51b** (CI) → digest only; bounds forbid touching .github.
- **No logmine-q-dispatchable fix this run** — all material items are daemon/remote/CI lane (consistent with iter-3/4/5). The one doc-candidate (orchestrator-rules "ground-truth via `git show origin/HEAD`", slice 5 F54) is marginal and partially covered by c55ec864 already landed; left for captain to greenlight.

---

> high-water: 019eceae-18a0-7952-ba99-6894b8cbd612  (2026-06-16T04:26:16Z agent_presence — last line of the frozen iter-6 window; next daily run resolves THIS event_id to its line and slices forward, per F41a)
