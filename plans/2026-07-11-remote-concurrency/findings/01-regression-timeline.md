# Remote-Worker (gb-mbp) Concurrency Saga — Regression Timeline

**Scope:** the macOS SSH worker `gb-mbp` (Tailscale 100.87.151.114, repo `/Users/gb/harmonik-worker/repo`, GitHub-origin).
**Primary source:** the ~20 dated incident annotations embedded in `.harmonik/workers.yaml` (`enabled:`/`max_slots:` fields), cross-referenced against `git log`, the named beads, and `run_started` counts from `.harmonik/events/events.jsonl`.
**Status as of 2026-07-12:** gb-mbp DISABLED (durable + live), fleet routing LOCAL, `hk-2hfyt` REOPENED and owned by hawat.

Empirical dispatch volume (run_started with `payload.worker_name=="gb-mbp"`, by day):

| Day | runs | Day | runs |
|---|---|---|---|
| 06-15 | 3 | 07-03 | 9 |
| 06-16 | 6 | 07-04 | 12 |
| 06-20 | 17 | 07-05 | **58** |
| 06-21 | 10 | 07-06 | 1 |
| 06-23 | 4 | 07-07 | 12 |
| 06-24 | 1 | 07-11 | 34 |
| 06-25 | 8 | 07-12 | **53** |
| 06-26 | 5 | | |
| 06-30 | 7 | | |

The two big spikes (07-05: 58, 07-12: 53) are exactly the two "ramp then storm" days — waves of runs that mostly fast-failed.

---

## Timeline table

| Date (UTC) | Slots | What changed | Outcome | Root cause named |
|---|---|---|---|---|
| 06-15/06-16 | 3 | First real gb-mbp dispatches; subsystem wired (193 prior runs cited). | Bring-up. | — |
| 06-20 | ~3 | `4befa185` SSHRunner-quote + substrate-runner threading (hk-538l/hk-fxy9); `a74e5bde`-lineage DOT socket-rewrite. | DOT remote runs hit `agent_ready timeout (HC-056)`; review-loop mode worked. | hk-538l (DOT `waitAgentReady` blind to remote agent_ready) |
| **06-21** | **3** | hk-538l **PROVEN FIXED** via DOT e2e `hk-4lrj`; review-loop path proven e2e (`hk-620j` → merged `15ca1eb3`); hk-vjsv gate-heartbeat `6084971b`. | **HIGH-WATER "it worked":** implement node reached agent_ready + committed on worker; review-loop landed to main. | hk-vjsv (cold commit_gate >900s node timeout) remained OPERATIONAL, not a code defect |
| 06-23 21:55 | 3→off | **INCIDENT + DISABLE.** 3 in-flight runs failed in 4s on an overloaded worker (load 18). | First fleet-down disable. | hk-9a7rt (contention / "exited without advancing HEAD"), hk-92ih3 (remote reviewer hang — daemon can't stat remote review.json) |
| 06-24 | 1→re-en | hk-92ih3 fixed (remote-aware verdict stat); hk-9a7rt **refuted** (was `hk-scndr` `os.Executable()` fork-bomb, 8,495 leaked procs, not cold cache). Re-enabled 23:55. | Re-enable. | — |
| 06-25 18:46 | 1 | STAGE-2 re-enable (`ba51ad45` reviewer-verdict-read + gate-base). | — | — |
| **06-25 19:01** | 3→off | **INCIDENT.** 3/3 concurrent codex runs raced worktree-create → **"git rev-parse HEAD returned empty"**; plus `hk-h106` truncated review.json. | **First true empty-HEAD.** | **hk-iaj1w** (concurrent worktree-create empty-HEAD), **hk-clrts** (truncated remote review.json) |
| 06-25 | off | hk-iaj1w fixed out-of-daemon `fb11aabd` (retry empty-HEAD race); hk-clrts fixed `e4122ac9` (retry truncated read). STAGE-2 re-attempt aborted — new binary `ed49d1b0` crashed at boot, supervisor reverted to unfixed `ba51ad45`. | Both "fixed"; deploy blocked by boot crash. | — |
| **06-26 06:47** | 1→off | **DISABLE.** `hk-h106` died `review.json ErrMalformed` AGAIN — the `e4122ac9` retry fix **not holding on remote**. | review.json fix regressed. | **hk-qts7r** (retry-window approach racy) |
| 06-30 | off→1 | hk-qts7r fixed `9860e8a2` (gate quit-watchdog on a valid-complete verdict so reviewer isn't killed mid-write); hk-1s1or stall detector `d737d20a`. | review.json robust after 3 tries. | — |
| **06-30 19:10** | 3→1 | **REVERTED.** Concurrent-proof (hk-icdz + 5) did NOT hold: only 2/6 dispatched; gb-mbp run HUNG in `launch_initiated→agent_ready` gap. Explicitly **NOT** the worktree-HEAD race (hk-iaj1w held) and NOT review.json (hk-qts7r held). | Distinct launch-gap stall. | **hk-1s1or** (launch→agent_ready blind spot) |
| 07-03 | 1→3 | hk-1s1or CLOSED; fresh daemon `314568a6`; serialized re-validate CLEAN on `2a2a1882`. | Re-enable, ramping. | — |
| **07-04 17:14** | 4→off | **DISABLE.** Fleet-wide idle-hang recurrence: all 4 in-flight runs went daemon-silent 18–60min, agents dead but non-terminal, stale-detector stuck on `sess.Wait`. | Concurrent idle-hang. | **hk-xkou8** (concurrent agent_ready blind-spot + stale-detector stuck on sess.Wait) |
| 07-05 02–03 | 1→2→6 | hk-xkou8 fixed (`e0b02f77` symmetric sess.Wait bound); `hk-lt091` `2b92169de` serialize fetchBaseOnWorker (07-04); split-gate `997b98a4` (local≤4 / remote high). Serialized re-validate clean; ramping. | — | — |
| **07-05 07:26** | 6→1 | **REVERTED 6→1.** A 7-bead wave fast-failed **7/7 in ~7s** each: `git worktree add exited 0 but HEAD did not resolve (concurrent remote create race)`. Fires despite `fb11aabd`(iaj1w) + `2b92169de`(lt091) both in the binary. | **Empty-HEAD recurrence #1** (at slots>1). | **hk-5qp7z** (N-concurrent worktree-create; single-create retry doesn't cover N) |
| 07-05 08–09 | 1→3→6 | hk-5qp7z fixed `d92b16de` (serialize worktree-add+HEAD-resolve under **createMu**) + `fd76a69e` test. jessica **PROVEN 7/7 concurrent, zero empty-HEAD**. RAMP 3→6. 58 runs this day. | **"works-but-flaky" peak.** Fails at REVIEW node under 6-concurrent. | **hk-5z1f0** (reviewer agent_ready_timeout at max_slots:6 — throughput DRAG, not block) |
| 07-05 15:00 | 6 | hk-5z1f0 fix `fd92adfc` (remote agent_ready 90→150s + cold-start semaphore). | Merged; residual 2/10 reviewer-timeout at sustained-6. | hk-5z1f0 (residual = tuning) |
| 07-06 05:00 | off | DISABLED to force LOCAL-under-srt for a Pi loopback-tunnel canary (not a gb-mbp bug). | Deliberate. | — |
| 07-07 (GO) | off→6 | RE-ENABLED for 4→10 lever; hk-5qp7z declared **PROVEN-FIXED**. | — | — |
| **07-07 07:5x** | 6→off | **DISABLE.** Two waves (07:45, 07:55) failed ~all remote runs `git worktree add exited 0 but HEAD did not resolve`. **RULED OUT** concurrency (fails at max_concurrent=1), missing commit (fetched, still fails), git/fs (manual `git worktree add` on worker SUCCEEDS). | **Empty-HEAD recurrence #2** — now at concurrency=1. Refutes hk-5qp7z "proven-fixed." | **hk-2hfyt** (daemon's remote-RUNNER-wrapped checkout no-ops under ssh wrap) |
| 07-07 15:01 | off | jamis fix `756f0c52` (`ensureBaseOnWorker` — ensure base commit present before worktree-add). | "Ready for re-validation." | — |
| **07-11 ~15:13** | off | **FLEET-WIDE OUTAGE.** EVERY remote dispatch empty-HEAD (`hk-iaj1w`/`hk-2hfyt` signature). Root: daemon branched remote worktrees on primary HEAD `d0c57dc7` which was **UNPUSHED**; worker fetches GitHub → base object missing → empty HEAD. Immediate fix: `git push origin main`. | **Empty-HEAD recurrence #3** — reframed as unpushed-base. | **hk-zno2t** (pushed-base + clean merge-target) |
| 07-11 12:4x | off→6 | **RE-ENABLED** per operator directive (admiral relay `019f5135`); hawat validating via `hk-rs-validate-remote-898a`. Condition set: "re-disable only if that historic failure recurs." | Re-enable. | — |
| **07-12 11:52** | 6→off | **DISABLE (operator-pre-authorized).** stilgar's 6 runs (hk-thbbv, hk-2i36s×2, hk-nxcvi×3) ALL died empty-HEAD **at concurrency=1**, fleet quiet, **base PRESENT** on worker (fetched, `cat-file=commit`), **manual `git worktree add` SUCCEEDS** — only daemon's ssh-runner-wrapped checkout no-ops (`createworktree.go:257` `resolveWorktreeHEADViaRunner` returns empty). REFUTES the "stale-worker/missing-base" read. `hk-2hfyt` **REOPENED** → hawat. | **Empty-HEAD recurrence #4.** Current live root cause. | **hk-2hfyt** (remote-runner-wrapper checkout no-op) |
| 07-12 | off | `hk-zno2t` durable `716253e27` ("stop mislabeling empty-HEAD as a race"); hawat consolidating hk-zno2t + hk-2hfyt on the same funcs (createworktree.go + workloop.go ~:3605). hk-zno2t reframed: `ensureBaseOnWorker` ALREADY exists (workloop.go:3648) but SILENTLY fails to land the SHA / `pushBaseToWorker` returns nil on `git push` exit 0 without verifying the object landed (`codesync_rs_b8.go:113`). | **Fix in progress** — mock-CommandRunner unit test mandated (gb-mbp stays disabled; chicken-and-egg: fix needs remote, remote off due to bug). | hk-2hfyt + hk-zno2t (fail-loud `cat-file -t` guard after push) |

---

## The recurring pattern (narrative)

The saga is **not one bug**. It is a rotating cast of *distinct* remote-path defects, each of which surfaces only after the previous one is cleared — the worker is a "peel the onion" box where every fix exposes the next layer. Across ~4 weeks the disable/re-enable cycle repeated at least **11 times**, and the failures fall into five recurring *classes*:

1. **agent_ready delivery** (hk-538l DOT socket-rewrite; hk-1s1or launch-gap stall detector; hk-xkou8 concurrent agent_ready blind-spot; hk-5z1f0 reviewer cold-start timeout). Every ramp past 1 slot re-exposed an agent_ready gap at a new node or concurrency level.
2. **Remote review.json read** (hk-92ih3 detection → hk-clrts truncation → hk-qts7r racy-retry → quit-watchdog-kills-mid-write). Fixed three times before `9860e8a2` held.
3. **Cold-cache / resource** (hk-9a7rt, refuted as a fork-bomb hk-scndr).
4. **Concurrent-git contention** (hk-lt091 fetchBaseOnWorker under mergeMu).
5. **Empty-HEAD worktree-create** — the spine of the whole saga, and the one that keeps coming back under new names.

The tell: each incident note **explicitly rules out the prior root cause** ("NOT the worktree-HEAD race (hk-iaj1w held)"; "RULED OUT concurrency … missing commit … git/fs"; "REFUTES the earlier stale-worker/missing-base read"). The team is disciplined about not re-blaming a closed bug — which is exactly why the same *symptom* (`git worktree add exited 0 but HEAD did not resolve`) has been attributed to four different mechanisms.

---

## The empty-HEAD "declared fixed then recurred" count

The single symptom — `git worktree add` exits 0 but HEAD does not resolve (empty-HEAD worktree) — has been **declared fixed and then recurred FOUR distinct times**, and is now on its **fifth** fix attempt (currently open). Each claimed fix and its subsequent recurrence:

| # | Claimed-fix commit / bead | Date declared fixed | Mechanism claimed | Subsequent recurrence | New name |
|---|---|---|---|---|---|
| 1 | `fb11aabd` **hk-iaj1w** — retry empty-HEAD worktree-create race | 2026-06-25 | single-create retry guard | 2026-07-05 07:26 — 7/7 fast-fail at slots:6 | **hk-5qp7z** (N concurrent, retry doesn't cover N) |
| 2 | `d92b16de` **hk-5qp7z** — serialize add+HEAD under createMu; **PROVEN 7/7** | 2026-07-05 | concurrency race eliminated | 2026-07-07 07:5x — fails **at concurrency=1** | **hk-2hfyt** (runner-wrapped checkout no-op) |
| 3 | `756f0c52` **hk-2hfyt** (jamis) — ensureBaseOnWorker: ensure base present | 2026-07-07 | worker missing base commit | 2026-07-11 15:13 — fleet-wide, unpushed base | **hk-zno2t** (pushed-base reachability) |
| 4 | `git push origin main` (immediate) + `716253e27` **hk-zno2t** durable | 2026-07-11 | base was unpushed → push it | 2026-07-12 11:52 — stilgar 6/6 fail at concurrency=1, **base PRESENT**, manual add works | **hk-2hfyt REOPENED** (wrapped-checkout no-op) |
| 5 | *(open)* **hk-2hfyt** + **hk-zno2t** consolidated, fail-loud `cat-file` guard, mock-unit-tested | in progress 2026-07-12 | `pushBaseToWorker` returns nil without verifying object landed; runner-wrapped checkout | — | — |

Note the **naming loop**: attempt #2 (hk-5qp7z) and attempt #4 (hk-zno2t reopening) both land back on **hk-2hfyt** as the "real" cause — the reopened bead is the same one filed at recurrence #2. The mechanism has oscillated between two poles — *"the base commit isn't on the worker"* (hk-iaj1w race → hk-2hfyt jamis → hk-zno2t unpushed) and *"the base IS on the worker but the wrapped checkout no-ops"* (hk-5qp7z → hk-2hfyt reopened) — with each pole disproving the other in turn.

---

## What this timeline tells us

**The agent_ready and review.json classes look convergent; the empty-HEAD worktree-create class is a fix-of-fix loop.**

- **Convergent (real fixes, fewer failures):** the review.json class was hard-fixed by `9860e8a2` (gate the quit-watchdog on a *complete* verdict — a root-cause fix, not a wider retry window) and has not recurred since 06-30. The agent_ready class was progressively hardened (socket-rewrite → stall detector → sess.Wait bound → cold-start semaphore) and degraded from hard-block to a tolerable 2/10 review-node *drag* (hk-5z1f0), now deferred as tuning. These are genuine progress.

- **Fix-of-fix loop (same class, new names):** the empty-HEAD worktree-create failure has been "fixed" four times (hk-iaj1w → hk-5qp7z → hk-2hfyt/756f0c52 → hk-zno2t) and is failing again at **concurrency=1 with the base present** — i.e. under the *simplest possible* conditions, which every prior fix's proof (concurrent-wave canaries, pushed base) specifically did not exercise. The evidence for a loop rather than convergence:
  - Each fix was validated against the *previous* mechanism (hk-5qp7z proved 7/7 concurrent; hk-zno2t proved pushed base) but **never against the wrapped-runner checkout path** — hawat's 07-12 note admits `hk-rs-validate-898a` "didn't exercise the wrapped path."
  - The daemon's own error label (`concurrent remote create race`) is now known to be a *mislabel* (`716253e27` "stop mislabeling empty-HEAD as a race") — meaning several of these fixes were aimed at a phantom (concurrency) the symptom string invented.
  - The current fix is blocked by a chicken-and-egg (needs the live remote to prove, but the remote is off because of the bug), and two consecutive concrete-spec dispatches **no-commit'd** because the "build pushed-base reachability" mechanism *already existed* — the real defect is a **silent-failure / missing fail-loud guard** (`pushBaseToWorker` returns nil on `git push` exit 0 without verifying `cat-file -t <sha>` on the worker), which no prior fix added.

**Bottom line:** four of the five failure classes have genuinely converged. The fifth — empty-HEAD worktree-create — is a fix-of-fix loop that recurred because every fix optimized a plausible *mechanism* (race, then missing base, then unpushed base) without ever adding the one thing that would have caught the mislabel early: a **fail-loud post-checkout HEAD/`cat-file` verification on the worker runner path** that hard-errors instead of proceeding into an exit-0 empty-HEAD worktree. The 07-12 consolidation (hk-2hfyt + hk-zno2t under hawat, with the mandated `cat-file -t` guard + mock-unit test) is the first attempt that targets the *silent-success defect itself* rather than a specific upstream cause — which is what would break the loop.
