# logmine — findings iter-22 (crew kynes)

**Window:** `019f4f2a` (iter-21 high-water) → `019f554b`. **18,814 events**, 2026-07-11T03:13Z → 2026-07-12T07:46Z (~28.5h). Snapshot: `/tmp/logmine-window-iter22.jsonl`. 6-slice read-only fan-out (run-fails · review-loop · daemon+keeper · comms+presence · **daemon/merge-incident [weighted]** · transcripts+git).
**Focus (captain):** the recurring DAEMON/MERGE-INCIDENT cluster piter's Phase-1 verdict flagged as the codex lane's real cost (dirty-index, unpushed-base, disk-watermark, reviewer pasteinject wedge, fmt-gate).

---

## HEADLINE — the merge cluster was largely neutralized *this window*, but by a redeploy boundary that splits "confirmed" from "committed-only"

Four fixes landed 00:35Z–07:22Z. The **live fleet daemon's last restart was 06:25:54Z on binary `d1fbf715`**. That boundary is load-bearing:

| Fix | Commit | Landed | In the 06:25Z binary? | Verdict |
|---|---|---|---|---|
| pasteinject seed shorten | hk-zn3vs | 00:35Z | ✅ (deployed 01:16Z) | **CONFIRMED** — 0 recur / 7h / 46 clean reviewers |
| non_ff rebase-and-retry | hk-sfy7f `61697960` | 05:47Z | ✅ (deployed 05:50Z) | **CONFIRMED** — 10→0 at 05:47Z |
| fmt-commit subject conventional | `1b97559a`+`9610d205` | 06:07/06:13Z | ✅ (deployed 06:25Z) | **CONFIRMED-thin** — merge_fmt_failed 10→0 after 06:25Z restart |
| governor liveness gate | hk-uxyf1 `3d8f606f` | — | ✅ (deployed 06:25Z) | consistent-fixed — 0/101 LivenessViolated, thin post-deploy |
| sibling-merge commit-msg gate | hk-2jeel `65cfb767` | 06:50Z | ❌ (after last restart) | **COMMITTED-ONLY — cannot confirm in-window** |
| no_gauge log-once-per-transition | hk-1q7bt `33ae3b5b` | 06:53Z | ❌ | **COMMITTED-ONLY** — no_gauge still 683/window |
| crew-start keeper auto-arm | hk-p006e `e710147e` | 07:22Z | ❌ | **COMMITTED-ONLY** — keeper-missing live |
| branch reaper | hk-fpjxi `b25e9919` | 00:34Z | n/a (manual CLI, no auto-caller) | **INERT** — bloat still growing |

> **Correction to the slice-5 read:** slice 5 credited hk-2jeel (06:50Z) as the fmt-gate fix and called `1b97559a`/`9610d205` red herrings. Deploy-timing refutes that — hk-2jeel was never running in-window; the fmt-gate relief after 06:25Z is the `1b97559a`+`9610d205` redeploy. hk-2jeel's own effect is still unverified.

**Operational consequence (load-bearing → captain):** the live daemon (`d1fbf715`, 06:25Z) is missing hk-2jeel + hk-1q7bt + hk-p006e + hk-fpjxi and everything merged since. A **fleet redeploy + restart of the shannon/schmidhuber keeper watchers** is the single highest-leverage next action; without it the no_gauge flood (683/window) and keeper-missing persist regardless of the merged fixes.

**False-fail reminder:** `run_failed`=117 but 35/40 distinct beads (87.5%) landed on main. Only **one true wedge** (hk-7l1w8). Miners MUST triangulate `git log --all --grep`.

---

## Prioritized register

| # | Finding | Sev | Lane | vs prior | Bead action |
|---|---------|-----|------|----------|-------------|
| M1 | **Merge stage discards a passed APPROVE verdict on retryable merge failure** — 31 runs got APPROVE then `outcome:rejected`+`run_failed` at merge (dirty-index 11 / non_ff 10 / fmt 10) and re-ran the *whole* implement+review. Self-heals but wastes a full review pass each time. The systemic waste that outlives the per-cause fixes. | **P1** | daemon | NET (extends F4/hk-k0eg) | **NET-NEW → hk-f9xzs** |
| M2 | **`agent-reviewer` subagent type unregistered in worktree harness** — 10 spawn-fails / 4 sessions; in-worktree review can't produce a verdict → root of the 44% missing-trailer residue. | **P1** | harness | NET-NEW | **NET-NEW → hk-q6nve** |
| M3 | **Fleet daemon redeploy + keeper-watcher restart + post-fix durability verify** — live binary d1fbf715 (06:25Z) lacks hk-2jeel/hk-1q7bt/hk-p006e/hk-fpjxi; verify no_gauge→0, fmt sibling-merge→0, keeper-arm on next crew-start, next window. | **P1** | daemon/ops | NET-NEW | **NET-NEW → hk-kvxsm** |
| M4 | **Reviewer pasteinject wedge — VERIFY-CLOSE** — 27/29 wedged runs failed; hk-zn3vs (00:35Z) gave 0 recur over 7h/46 reviewers. Add 48h regression watch on `pasteinject_failed{phase:reviewer}`. | P1→verify | daemon | FIXED-confirm | ENRICH hk-1kdd7 (closed) + watch |
| M5 | **Branch reaper inert** — hk-fpjxi merged but 0 auto-callers; `run/*` 408→512, `worktree-agent-*` 173→230, still growing. Wire `ReapBranches` into `harmonik supervise` housekeeping. | P2 | daemon | RECURRING (refutes hk-fpjxi-as-complete) | **NET-NEW → hk-2i36s** |
| M6 | **Daemon crash-loop** — 17 `supervisor_revival cause:unexpected_exit` in 28.5h (~14/day, incl. 6-in-40min burst 00:37–01:17Z); root of the 53 restart-orphan false-fails (45% of run_failed). Supervisor can't distinguish crash from SIGTERM-redeploy. | P2 | daemon | RECURRING vs F4 | ENRICH hk-k0eg |
| M7 | **worktree_create concurrent-remote-create race** — 17× `git worktree add exited 0 but HEAD did not resolve`, remote-substrate path (`~/harmonik-worker/repo`). TOCTOU in the remote provisioner. | P2 | daemon/remote | NET-NEW | **NET-NEW → hk-ziu3i** (remote-substrate) |
| M8 | **Presence flood = 53% of ALL log volume** — 10,012 join / 0 leave / 0 refresh, 10 agents, zombie keys never reaped. Single biggest log-volume source. | P2 | comms | RECURRING vs F9 | ENRICH hk-ru45u |
| M9 | **Trailer coverage 55%** (up from 27%) — commit-msg gate now enforcing (49 sessions rejected, 0 `Trivial:true` escapes); residual gap tied to M1 (discarded verdicts never re-attach trailer) + M2 (no in-worktree reviewer). | P2 | daemon | RECURRING/↑ F2 | ENRICH hk-x2spu |
| M10 | **non_ff (10→0 @05:47Z) + dirty-index (11→0)** self-healed, hk-sfy7f the owner; record the in-window before/after. | P2 | daemon | FIXED-likely | ENRICH hk-sfy7f (closed, record) |
| M11 | **commodore `comms recv --follow` reconnect storm** — 3,723 presence events (20% of window) in a 90-min burst; hk-62r8w/d1fbf715 the fix, no recurrence in post-fix tail. | P2 | comms | FIXED-consistent | ENRICH hk-62r8w (closed, baseline) |
| M12 | **Unreviewed-merge via hk-du455 preserve-committed-tree path** — 2 runs (down from 6 iter-21). Trending benign. | P2→P3 | daemon | RECURRING/↓ F1 | note hk-nwgj7 (closed) |
| M13 | disk_low watermark 17× (avail dipped 8.9GiB @06:34Z, recovered to 37GiB); go-cache autoclean lagged to 01:27Z. Blocked 0 merges. One n=1 GOROOT toolchain build-break (07-11 15:54Z), self-recovered. | P3 | daemon | benign | none |
| M14 | **True wedge hk-7l1w8** — only bead whose 6 fails never landed; cycled every failure mode. Bead shows CLOSED — verify it was genuinely salvaged, not reconcile-closed while stranded. | P2 | daemon | NET | digest → captain (verify) |

## FIXED-confirmed this iter
- **hk-uxyf1 governor liveness** — 0/101 LivenessViolated (thin post-deploy).
- **hk-zn3vs pasteinject** — 0 recur / 7h / 46 clean reviewers (strongest confirm).
- **hk-sfy7f non_ff** — 10→0 at 05:47Z deploy.
- **F10 implement exit-127 empty-PATH** — 0 occurrences.
- **flagless-RC wedge (hk-thbbv/hk-hfmg6)** — 0 in-window; all 6 RC carried real flags, resolved ≤iter-2.

## Explicitly UNCONFIRMED (deploy-timing, not code) — re-verify next window
- hk-2jeel (fmt sibling-merge gate), hk-1q7bt (no_gauge), hk-p006e (keeper-arm): all committed after the 06:25Z last-restart → not running in-window. **Do not close on this window.**

> high-water: 019f554b-188f-75d0-91f1-7a8246689ee5
