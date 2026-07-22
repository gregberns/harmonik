# Remote-worker failure taxonomy (gb-mbp)

> Scope: every DISTINCT failure signature the daemon's remote-substrate worker (`gb-mbp`, macOS,
> Tailscale SSH, repo `/Users/gb/harmonik-worker/repo`) has produced over the project's history.
> Purpose: stop conflating separate bugs, and isolate which are downstream symptoms of one root
> cause. Evidence: `.harmonik/workers.yaml` embedded incident log, `br show`, and the live code.
> Read-only pass, 2026-07-12.

A recurring meta-pattern: the same **operator-visible symptom string** ("empty HEAD", "ErrMalformed",
"agent_ready_timeout") has been produced by MORE THAN ONE distinct underlying defect at different
dates. The error label the daemon prints (e.g. "concurrent remote create race") is frequently WRONG
about the cause — it was written for the first-observed instance and never updated. Treat the string
as a symptom, never as a diagnosis.

---

## The taxonomy

### 1. Empty-HEAD worktree-create (the dominant one)

This label covers **THREE genuinely different root causes** that all surface as the same string
`git worktree add exited 0 but HEAD did not resolve`. They must not be merged.

| Facet | Beads | Root cause | Concurrency dependence | Status |
|---|---|---|---|---|
| **1a. Concurrent create race** | hk-iaj1w (P2, closed 06-25), hk-5qp7z (P1, closed 07-05) | N simultaneous `git worktree add` against the ONE shared worker repo race on HEAD/index resolution | **N>1 only** (needs ≥3 concurrent) | Claimed-fixed; superseded as the live cause |
| **1b. Unpushed-base / base-missing** | hk-zno2t (P1, in_progress), hk-2hfyt (early framing) | Daemon branched remote worktrees on box-A's LOCAL HEAD which was AHEAD of GitHub `origin/main`; worker `fetch origin` never brings the unpushed SHA → `worktree add <missing-obj>` yields exit-0 empty-HEAD | **N=1 too** (deterministic, every dispatch) | Mitigated (push origin main); durable fix in flight |
| **1c. SSH-runner-wrapped checkout no-op** | hk-2hfyt (P1, OPEN, reopened 07-12) | Base commit IS present on worker (fetched, `cat-file -t`=commit) and MANUAL `git worktree add` SUCCEEDS — only the daemon's ssh-runner-wrapped checkout no-ops; `resolveWorktreeHEADViaRunner` returns empty | **N=1, fleet quiet** | **RECURRING-after-claimed-fix — the current live cause** |

- **Symptom (exact):** `create worktree failed: workspace: WorktreeCreationFailed: git worktree add -b "run/<id>" "<path>" "<sha>": git worktree add exited 0 but HEAD did not resolve in "<path>" (concurrent remote create race): <nil>` / git output `(empty HEAD after git worktree add — hk-iaj1w)`.
- **Where it surfaces:** `internal/workspace/createworktree.go:257` (`resolveWorktreeHEADViaRunner`) → error synthesized at `:264`. The mitigations live in `internal/daemon/codesync_rs_b8.go` (`fetchBaseOnWorker` :74 with a post-fetch `cat-file -t` guard, `pushBaseToWorker`, `ensureBaseOnWorker`) and the `createMu`/`worktreeCreateMu` serialization at `internal/daemon/workloop.go:3367`/`:407`.
- **Claimed fixes + commits:** 1a → `fb11aabd` (hk-iaj1w single-create retry) + `d92b16de` createMu (hk-5qp7z); 1b → `git push origin main` (op mitigation) + `ensureBaseOnWorker`/`pushBaseToWorker` in `codesync_rs_b8.go` (hk-2hfyt/hk-zno2t); serialize-under-mergeMu `2b92169de` (hk-lt091).
- **Current status:** **RECURRING.** 2026-07-05 the createMu fix (1a) was declared PROVEN (7/7 canaries at slots:3) and slots ramped 3→6. 2026-07-07 a fleet-wide wave recurred at **max_concurrent=1** → refuted the concurrency race → reframed to 1b/1c → gb-mbp disabled. 2026-07-11 recurred again (unpushed base d0c57dc7). 2026-07-12 stilgar's 6 runs (hk-thbbv, hk-2i36s×2, hk-nxcvi×3) ALL died empty-HEAD at concurrency=1 with base present and manual add working → RETRACTED the missing-base read, isolated to 1c (wrapped-checkout no-op). hk-2hfyt reopened, owned by hawat; gb-mbp durably disabled.
- **Key correction to the plan's framing:** the plan (00) attributes empty-HEAD to an "unpushed base ref" (1b). As of 2026-07-12 that is SUPERSEDED — base is present and the live defect is the wrapped-checkout no-op (1c). Note the code ALREADY carries the cat-file-verify + push-fallback (1b hardening), yet the outage still recurs — direct evidence the live cause is NOT base-reachability.

---

### 2. review.json truncation / ErrMalformed

| Field | Detail |
|---|---|
| **Symptom** | `read reviewer verdict: workspace: review verdict ErrMalformed: json parse error at <wt>/.harmonik/review.json: unexpected end of JSON input` — file EXISTS but is truncated; FAST fail (~11–20s after reviewer_launched, NOT a budget timeout) |
| **Where** | `internal/workspace/reviewverdict.go` `ReadReviewVerdictVia` (remote `cat` read, ~:126) → `parseReviewVerdict` `json.Unmarshal` fails. Real defect on the WRITE side: `internal/daemon/pasteinject.go:2336` `pasteInjectQuitOnReviewFile` quit-watchdog killed claude the instant review.json merely EXISTED (existence-only stat), mid-Write → permanently truncated |
| **Hypothesized cause** | Quit-watchdog force-kills the reviewer agent mid-write on the remote path (existence-triggered, not valid-verdict-triggered); the SSH `cat` then reads a partial/unflushed file that same-kernel local page-cache timing never exposed |
| **Beads** | hk-clrts (P1, closed 06-25), hk-qts7r (P1, closed 06-30), cousin of hk-92ih3 |
| **Claimed fixes** | `e4122ac9` read-retry (hk-clrts) → INSUFFICIENT; `af3cd88d` retry-window widen 1.5s→6.3s (hk-l489f) → INSUFFICIENT (still racy); `9860e8a2` gate the kill on a valid-complete verdict + retry-until-valid + atomic write (hk-qts7r) → validated live (4 clean APPROVE reads, zero ErrMalformed) |
| **Status** | fixed-clean (as of `9860e8a2`, 06-30 live validation) — but was RECURRING across two prior "fixed" claims before it held |
| **Concurrency** | N=1 (a single reviewer write-vs-read TOCTOU); load makes it more likely but not required |

---

### 3. Reviewer agent_ready_timeout under load

| Field | Detail |
|---|---|
| **Symptom** | review-stage agent (`review_tests`/`review_correctness` node) fails `agent_ready_timeout` — the reviewer claude never signals agent_ready within the window. ALL occurrences worker=gb-mbp; ZERO local. Beads re-dispatch and eventually merge → throughput DRAG, not a hard block |
| **Where** | `internal/daemon/agentready.go:56` `defaultAgentReadyTimeout = 90s`, applied per-agent-spawn (reviewer gets a fresh 90s at `reviewloop.go:721`/~:1430); emit at `workloop.go:7507`; HC-056 reap at `workloop.go:4858`/`:4891` |
| **Hypothesized cause** | The reviewer is a 2nd agent spawned atop up-to-6 resident implementers on ONE Mac mini → CPU/disk contention delays the SessionStart dial past 90s. The expensive claude COLD-START is NOT serialized/rate-limited per worker (worktree-create IS under createMu; agent-spawn is not) |
| **Beads** | hk-5z1f0 (P3, OPEN, assignee hawat) |
| **Claimed fix** | Merged `fd92adfc` (per plan 00) — raise 90s→150s (part A) + a per-worker spawn-admission semaphore (part B). **NOT deployed, NOT proven** |
| **Status** | open |
| **Concurrency** | **N>1 only** — local gated ≤4 never trips; only fires at 6-concurrent remote. This is the #1 ceiling on remote throughput |
| **Caveat** | The plan flags the "cold start" explanation as suspect ("can't be a cold start if we just ran on that box"). Treat the real wait (tmux spawn vs SSH round-trip vs semaphore vs model handshake) as unverified. |

---

### 4. Launch-gap / idle hang (launch_initiated → agent_ready never fires)

| Field | Detail |
|---|---|
| **Symptom** | run reaches `launch_initiated` (SSH launch starts) then HANGS 18–60min; agents dead but run non-terminal; daemon-silent; stale-detector stuck on `sess.Wait`. Eventually a ~30min `agent_ready_timeout`. NO stall-detection event fires during the gap (blind spot) |
| **Where** | `internal/daemon/stalewatch.go` — `launchStallThreshold` (30s) only covers run_started→launch_initiated; once `launchInitiatedSeen=true` the launch-stall check is SUPPRESSED (~:85, :110-115) → the launch_initiated→agent_ready gap has no detector |
| **Hypothesized cause** | (cause) SSH tunnel multiplexing on the readiness signal — hk-cnp17, ControlMaster=no per tunnel `reversetunnel.go:238`. (safety-net gap) stalewatch has no threshold for the launch→ready gap |
| **Beads** | hk-1s1or (P1, closed 06-30, the safety net) + hk-xkou8 (P1, closed 07-05, the concurrent-slot variant). Also the earlier hk-icdz repro (06-30) |
| **Claimed fixes** | `d737d20a` new `agent_ready_stall_detected` detector, default 3m threshold, detection-only (hk-1s1or); `e0b02f77` symmetric 10s bound on impl+rev `sess.Wait` (hk-xkou8) |
| **Status** | fixed-clean (both landed + serialized re-validate passed on `2a2a1882`) — but note hk-xkou8's reviewer FLAGGED no concurrent-slot repro test committed and the root cause was reframed to sess.Wait-only |
| **Concurrency** | hk-1s1or single-slot; hk-xkou8 is explicitly the **N>1** variant (idle-hang at max_slots>1 hung all 4 in-flight runs) |

---

### 5. Non-fast-forward merge / merge-retry gap

| Field | Detail |
|---|---|
| **Symptom** | `merge-to-main failed: non_ff_merge: main advanced concurrently`. implement+review SUCCEED and the commit exists — only the FINAL ff-merge back to main fails |
| **Where** | `internal/daemon/workloop.go:374` (non-fast-forward rejection) + `:3887` retryable-failure branch (rebase_conflict / non_ff_merge / merge_fmt_failed) |
| **Hypothesized cause** | Main advances from OTHER lanes landing DURING a multi-minute run window. NOT a dispatch race — any bead whose runtime overlaps a competing merge non-ffs. The daemon failed the whole run instead of rebase-and-retrying the already-made commit |
| **Beads** | hk-sfy7f (P1, closed 07-12), hk-zno2t (mentions the merge-target facet), earlier "benign non_ff_merge race" notes throughout workers.yaml |
| **Claimed fix** | `cca9a638`/`61697960` — on non_ff_merge, rebase the run's already-made commit onto current origin/main and RETRY the ff-merge in a bounded loop |
| **Status** | fixed-clean pending redeploy — logmine iter-22 confirmed non_ff_merge 10→0 at 05:47Z after 05:50Z restart; all affected beads landed, no permanent loss |
| **Concurrency** | **N>1** by nature (needs a competing lane), but fleet-wide (local+remote), NOT remote-specific. Fails safe (retryable) even before the fix |

---

### 6. Implementer "exited without advancing HEAD" under worker overload

| Field | Detail |
|---|---|
| **Symptom** | `node "implement" (implementer) exited without advancing HEAD past <sha>`; on an overloaded worker (load 18) all in-flight implementers fast-fail within ~4s |
| **Where** | `internal/daemon/workloop.go:4166` (noChange-subsumed: implementer exited without advancing HEAD) |
| **Hypothesized cause** | Two DISTINCT things wear this string: (a) genuine CPU/process contention on an overloaded worker (the 06-23 incident); (b) a **fork-bomb** — hk-9a7rt's actual root cause was 8,495 leaked `supervise.test` procs (os.Executable() test fork-bomb, hk-scndr), load 22+, NOT cold-cache. Also note 1b/1c empty-HEAD surfaces this same string in its `dot` Notes field (hk-2hfyt, hk-zno2t) |
| **Beads** | hk-9a7rt (P1, closed 06-24 — premise REFUTED), hk-scndr (fork-bomb), hk-fozq/hk-usn8o (06-23 overload incident) |
| **Claimed fix** | hk-9a7rt closed as "premise refuted" — GOCACHE was warm; the 900s commit_gate blow-through was the fork-bomb, cleaned on the worker |
| **Status** | The commit_gate-timeout framing = fixed-clean (refuted/not-a-bug). The underlying "advance HEAD" string is a shared symptom, not one bug |
| **Concurrency** | overload variant N>1; but the string also appears at N=1 as a downstream of the empty-HEAD family |
| **CAUTION** | workers.yaml cites "hk-9a7rt (contention)" for the 06-23 "exited without advancing HEAD" incident, but the hk-9a7rt BEAD is about commit_gate/fork-bomb and was closed refuted. The citation conflates two things. |

---

### 7. Remote review-detection hang (reviewer never auto-quits)

| Field | Detail |
|---|---|
| **Symptom** | remote reviewer writes a valid APPROVE to `<remote-wt>/.harmonik/review.json` then idles at the prompt instead of exiting; run hangs ~30min in "budget elapsed but reviewer active (pane-active)" loop until a human `/exit`s the pane |
| **Where** | `internal/daemon/pasteinject.go:2271` — `pasteInjectQuitOnReviewFile` detected the verdict with a box-A LOCAL `os.Stat(verdictPath)`. For a remote run the file lives on the WORKER, so the box-A stat NEVER succeeds → never sends /quit → Stop hook (`internal/hookrelay/hookrelay.go:313`, runs on worker only on reviewer exit) never fires → verdict stranded |
| **Hypothesized cause** | verdict-detection stat was never made remote-aware (the `statTaskFileVia`/runner seam already existed for the implementer commit-poll but was not wired into verdict detection) |
| **Beads** | hk-92ih3 (P1, closed 06-24) |
| **Claimed fix** | make verdict-detection stat remote-aware via the existing `statTaskFileVia` seam (nil runner → os.Stat, local byte-identical) |
| **Status** | fixed-clean (closed 06-24) — but is the direct upstream of #2: once detection was made existence-based-remote, the quit-watchdog then killed mid-write (hk-qts7r), requiring the "valid-complete verdict" gate |
| **Concurrency** | N=1 (a detection-plumbing gap, not load-dependent) |

---

### 8. SelectWorker not firing / run went LOCAL unexpectedly

| Field | Detail |
|---|---|
| **Symptom** | with `enabled:true` + fixed binary, a proof bead was NOT routed to gb-mbp — ran LOCAL (pane %6), daemon log had ZERO worker-registry engagement (no gb-mbp / SelectWorker / workers.Load lines) |
| **Where** | `internal/daemon/workloop.go:1223` (SelectWorker returns nil while Enabled==false) / :1249 (unhealthy → skipped → falls back local) |
| **Hypothesized cause** | **RESOLVED AS A FALSE SIGNAL** (captain, 2026-06-24). Worker activity logs to `.harmonik/events/events.jsonl` as TYPED events (`run_started.worker_name`), NEVER to daemon stderr — grepping the daemon log for SelectWorker/gb-mbp/workers.Load ALWAYS returns 0 regardless of routing. Confirm routing via `events.jsonl` `run_started.worker_name=gb-mbp`, never by log-grep |
| **Beads** | (no dedicated bead — an investigation artifact) |
| **Status** | not-a-bug / observability trap. Real gates on SelectWorker skipping: `Enabled==false` (in-memory `worker disable`) or boot health-check failure |
| **Concurrency** | n/a |
| **METHODOLOGY NOTE** | This is the load-bearing "never hand-grep by run_id" lesson — false negatives from log-grep produced a phantom bug. Use `harmonik subscribe --json` / structured jq on events.jsonl. |

---

## Key analytical question: independent bugs, or shared root cause?

These are **not eight independent bugs.** They cluster into a small number of shared root
architectural causes, plus a couple of genuinely separate ones.

### Cluster A — the ssh-runner wrapper seam (the biggest shared root)
**#1c (wrapped-checkout no-op), #2 (truncated review.json read), #7 (remote review-detection blind
stat).** All three are the SAME architectural fault: **an operation that is correct on the local
same-filesystem path behaves differently when routed through the SSH runner** (`runner.Command(...).CombinedOutput()`
in `codesync_rs_b8.go`/`createworktree.go`/`reviewverdict.go`/`pasteinject.go`). Each was "fixed" by
retrofitting remote-awareness into ONE call site (verdict stat → verdict read → worktree HEAD-resolve),
and each fix pushed the fault to the NEXT un-hardened call site. #7 made detection remote-aware, which
exposed #2 (kill mid-write), whose read-retry fixes failed until the write itself was gated. #1c is the
still-open member: the wrapped checkout silently no-ops where a manual `git worktree add` succeeds. The
common signature — **"manual command on the worker works; the daemon's wrapped invocation does not"** —
is the tell. This is a partial-read / wrap-semantics / non-atomic-over-tunnel problem, not N distinct
git/json bugs. **The highest-value fix is to audit EVERY `runner.Command` remote call site for
wrap-vs-local divergence as one workstream, not bead-by-bead.**

### Cluster B — shared-worker contention (one Mac mini, up-to-6 concurrent agents)
**#3 (reviewer agent_ready_timeout), #4 (launch-gap idle hang at N>1), #6a (overload "no advance"),
#1a (concurrent create race).** These are all **load/contention on a single shared box + shared SSH
tunnel + shared repo**, appearing only (or predominantly) at N>1. The unifying gap: the daemon
serializes worktree-create (createMu) but NOT the expensive claude cold-start spawn, and the
stale-detector wedges on `sess.Wait` under concurrency. A per-worker spawn-admission semaphore (the
createMu analog, hk-5z1f0 part B) is the structural fix for most of this cluster.

### Cluster C — base-reachability (partly a red herring)
**#1b (unpushed base).** Real and mitigated, but per the 2026-07-12 retraction it is NOT the live
empty-HEAD cause; the code already carries the cat-file-verify + push-fallback yet the outage recurs
(→ Cluster A #1c). Keep as hygiene, not the fix.

### Genuinely independent
**#5 (non_ff_merge)** — a fleet-wide merge-race (local+remote), not remote-specific; fixed by
rebase-and-retry. **#8 (SelectWorker "LOCAL")** — not a bug at all, an observability trap.
**#6b (fork-bomb)** — an unrelated test-harness defect (hk-scndr) that masqueraded as remote overload.

### Bottom line
The dominant, still-open remote failure is **Cluster A, and specifically #1c** — the ssh-runner wrapper
mis-executing the worktree checkout. The repeated "fixed then recurring" history of the empty-HEAD and
review.json families is explained by fixing one wrapped call site at a time while the underlying
wrap-semantics fault stayed in the others. The concurrency ceiling (Cluster B) is a separate, real,
but throughput-drag problem, not a hard block. Base-reachability and non_ff_merge are largely resolved.
