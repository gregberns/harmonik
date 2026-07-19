# 03-Research / C2 — Explicit merge queue (the mergeMu split)

> Pass 3 (Research), component C2. All `file:line` verified against the current
> working tree (branch `phase1-session-restart-substrate`, 2026-07-14),
> `internal/daemon/workloop.go`. Delegated to a fresh-context sub-agent; parent
> (design agent) owns this write. This is the hardest concurrency question in M3
> and a hard prerequisite for M4 (remote merges).

## Research questions

1. Step-by-step walkthrough of `mergeRunBranchToMain` with per-step shared-state / idempotency / lock-need classification.
2. The true critical section that MUST be serialized vs. what can run speculatively.
3. All `mergeMu` sites (current lines), incl. the two non-merge uses.
4. `worktreeCreateMu` semantics and interaction with `mergeMu`.
5. The project's queue/serialization idiom to reuse.
6. `br sync --import-only` + working-tree refresh placement/ordering.
7. Fairness/ordering guarantees across queues.

## Findings

### Entry points and retry structure

- `lockedMergeRunBranchToMain` (workloop.go:6512-6518) holds `mu *sync.Mutex` across the **entire** body (`defer mu.Unlock()`), then calls `mergeRunBranchToMain` (:6544-7068). So TODAY everything below - `go build ./...`, `go vet`, gofumpt/gci, `git push origin`, `git fetch origin`, `br sync` - runs under the global lock.
- **Outer retry loop** (`maxMergeStepRetries=2`, up to 3 calls): ONLY at the review-loop call site (:3891-3913). Retries on `isRetryableMergeReason` (:6445-6452: prefixes `rebase_conflict`, `non_ff_merge`, `merge_fmt_failed`), re-amending review trailers before each retry (:3894-3903) because a prior rebase rewrote HEAD. Other four sites call once.
- **Inner retry loop** (`maxPushAttempts=3`, :6750-6994): one `pushAttempt` counter shared by FF-check failure (local target advanced, hk-1u4wp) and push non-FF rejection (origin advanced, hk-svieq). Both re-resolve target tip and **re-rebase** before retry: :6772-6817 (FF-check loss) and :6922-6991 (push loss). **This is the lose-the-race -> re-rebase -> re-validate loop the decompose review asked about - it already exists.**

### Walkthrough of `mergeRunBranchToMain` (:6544-7068)

Legend: **Lock** = must be under merge lock (LOCK), can run speculatively outside with re-validation (SPEC), or lock-irrelevant (FREE).

| # | Lines | Step | State | Lock |
|---|---|---|---|---|
| 0 | 6545-6560 | Fail-closed guards (targetBranch empty / in ProtectBranches, hk-6r6xv) | none | FREE |
| 1 | 6565-6572 | `git rev-parse refs/heads/run/<id>` -> `runTip`; missing -> noChange | R per-run ref | SPEC |
| 1b | 6577-6591 | `git rev-parse refs/heads/<target>` -> `mainTip`; `mainTip==runTip` -> noChange | R **shared target ref** (goes stale) | SPEC (must re-validate) |
| 1c | 6598-6600 | hk-cwxow: `runTip==headSHA` -> noChange | none | FREE |
| 2a | 6611,6629-6637 | hk-sfy7f: if wtPath absent, `git worktree add` in projectDir (remote fallback); deferred removeWorktree under lock today | W projectDir `.git` worktree metadata | needs a **repo-IO lock**, not necessarily the merge lock |
| 2b | 6653,6662,6677 | discardDirtyChurn(:7109)/commitResidualDelta(:7204)/cleanUntrackedFiles(:7306) - clean per-run worktree; residual delta -> commit on run branch | R/W per-run worktree; W object DB + per-run ref | SPEC |
| 2c | 6679-6694 | `git rebase <target>` in wtPath; fail -> `rebase --abort` -> `rebase_conflict` | R shared target ref (once), W per-run ref + object DB | SPEC |
| 2d | 6696-6705 | Re-resolve runTip, mainTip post-rebase | R refs | SPEC |
| 2e | 6713-6721 | hk-zmpd rebase-drop guard: post-rebase `runTip==mainTip` w/ commits -> `rebase_dropped_commits` | none | FREE |
| 2f | 6729-6739 | stripRunContextFromMerge (strip `.harmonik/run-context/**` via commit in wtPath); re-resolve runTip | W per-run worktree/ref, object DB | SPEC |
| - | 6750 | `for pushAttempt := 1..3 {` | | |
| 3 | 6755-6757 | **FF check** `git merge-base --is-ancestor mainTip runTip` | R object DB (meaningful only if mainTip CURRENT) | **LOCK** (the validation read) |
| 3-r | 6765-6817 | FF-fail retry: re-resolve mainTip, re-rebase, re-resolve runTip, drop guard, `continue` | as 2c | SPEC (lost-race recovery) |
| 3a | 6821-6828 | **`git update-ref refs/heads/<target> runTip`** - the landing. **NOT CAS**: no `<oldvalue>` arg, so git does NOT verify ref still == mainTip; atomicity is provided ENTIRELY by mergeMu today | **W shared target ref** | **LOCK** (or convert to CAS `update-ref <new> <old>`) |
| 3b | 6843-6879 | **Post-merge build gate** `go build ./...`+`go vet ./...` in wtPath (fallback projectDir); cold-cache single retry (hk-44ab2, cacheReapMu race); fail -> **rollback** `update-ref <target> mainTip` (:6870), emit `merge_build_failed`, return | build: R worktree, safe; rollback: **W shared target ref** (blind) | build = SPEC on rebased tree; rollback forces it inside today |
| 3c | 6889-6891 -> 7429-7569 | **Fmt gate** runMergeFmtCheck: gofumpt+gci to fixpoint; may auto-format + `git add -A`+commit in wtPath (:7530-7550) then **update-ref target AGAIN to fmt-commit tip** (:7554-7565); every failure -> `rollback()` blind update-ref to mainTip (:7430) | W per-run worktree/object DB + **W shared target ref (twice-landing)** | fmt run = SPEC; ref advance + rollback = **LOCK** today |
| 4 | 6895-6900 | **`git push origin <target>`** - network; success -> break | W **remote** target ref | inside today; see conclusion |
| 4-r | 6902-6991 | Push-fail: **blind rollback** local ref -> mainTip (:6906); if non-FF + attempts left: `git fetch origin` (:6923), rev-parse `refs/remotes/origin/<target>` (:6933), update-ref local -> remote tip (:6947), re-rebase (:6958), re-resolve+drop guard, loop; else terminal `push_failed`. Comment :6902-6905: rollback-fail -> Cat-3/EM-INV-005 reconciliation at next startup | W shared local target ref (rollback+advance), network | **LOCK** under current blind-rollback design |
| 5a | 7015-7020 | `git restore --staged .` in **projectDir** (clear index; prevents phantom staged-deletes feeding false escape) - best-effort | W shared index | **LOCK** (or same queue) |
| 5b | 7036-7043 | `git reset --hard HEAD` in **projectDir** (EM-054 refresh) - non-fatal, `working_tree_refresh_failed` | W **shared working tree + index** | **LOCK** (other half of hk-zguy6) |
| 6 | 7050-7065 | If `git diff --name-only mainTip runTip` touches `.beads/issues.jsonl`: `br sync --import-only` in projectDir (BL-MRG-004/005) - non-fatal `bead_sync_failed` | R working-tree JSONL (just refreshed by 5b), W beads SQLite | can leave ref window, but ordered after this merge's 5b |
| 7 | 7067 | `return mergeOutcome{success:true}` | | |

**Rollback summary**: rebase conflict -> `rebase --abort` (nothing shared). Build/fmt fail -> blind `update-ref` back to mainTip. Push fail -> blind rollback then retry or terminal. Reset-hard / br-sync fail -> merge already durable, non-fatal. **Every rollback is a blind restore to a remembered mainTip, NOT a CAS** - sound only while the global lock guarantees no interleaving.

## The true critical section (conclusion)

Three distinct shared resources are conflated under one lock today:
1. **The target branch ref** (`refs/heads/<target>` box A): writes at update-ref land (:6821), fmt-gate second land (:7558), rollbacks (:6870,:6906,:7430), advance-to-remote (:6947).
2. **The shared projectDir working tree + index**: restore --staged (:7015), reset --hard (:7036), hk-sfy7f worktree add (:6630), plus the READ side - the escape check (:5172).
3. **The remote ref** (`git push origin` :6895) with its blind-rollback coupling to resource 1.

**The window that MUST be serialized (per target branch):**

> **re-validate (FF-check with freshly read mainTip) -> update-ref [-> fmt-gate ref-advance] -> push origin (+ rollback on failure) -> restore --staged -> reset --hard**

i.e. :6755 -> :7043, *minus* the build/vet/fmt executions themselves. Rationale:
- **FF-check + update-ref** atomic together (validation read + write). Alternative: git-native CAS `git update-ref refs/heads/X <new> <expected-old>` shrinks it to one command - but rollbacks must then also become CAS or be redesigned.
- **push + rollback** stay in the window *under the current design* because the rollback blindly restores mainTip - outside the lock it would erase a sibling's subsequent landing. Moving push out requires a semantic change (local-land = commit point, push async w/ EM-INV-005 reconciliation) = **M4 territory, not a free C2 win**.
- **restore/reset** stay in the window because of hk-zguy6: the escape check (:5162-5175) is race-free only because the whole merge sequence is under mergeMu. If the queue serializes only the ref, the transient HEAD != working-tree window reappears and escape false-positives return. **The escape check must therefore also go through the same queue** (a read-only "quiescent point" job), or the tree-refresh must join the ref window.

**What can run OUTSIDE (speculatively), re-validated under the lock:**
- Steps 1-2f entirely: tip resolution, worktree cleanup, **the rebase**, the strip commit - per-run worktree + per-run ref + concurrency-safe object DB only.
- **The build+vet gate and the fmt run** - the big win: multi-second-to-minute `go build`+`go vet` currently under the global lock. They validate the *rebased tree*, a pure function of (mainTip-at-rebase, runTip); if the under-lock FF re-validation confirms the target still equals the rebased base, the speculative verdict holds. **Subtlety:** after an FF-loss re-rebase, build/fmt must be **re-run** on the new tree. Today they sit INSIDE the `for` (:6843 onward), so the loop already re-executes per attempt - a hoisted C2 design MUST preserve per-re-rebase re-execution (do not build-once-before-enqueue).
- The fmt auto-format **commit** can be created outside; only its landing needs the window (fold to a single final-tip landing, not the current two-step land-then-advance).
- `br sync --import-only` after leaving the ref window (see Q6).
- Already outside and staying: `preMergeSync` (:3574-3589), scenario gate (with hk-ur428 caveat), `appendReviewTrailersToHEAD` (:3881,:4118).

### Q3 — All mergeMu sites

- **Definition**: `mergeMu *sync.Mutex` field workloop.go:384 (doc :368-383, hk-yyso7: named queues -> concurrent completions -> non-FF race). Production wiring :1161. Test override daemon.go:613-619 (`daemonTestHooks.mergeMu` via `WithMergeMutex`), applied :2205-2207.
- **Merge call sites** (all `lockedMergeRunBranchToMain(<ctx>, deps.mergeMu, activeRepo, runID, deps.bus, beadID, headSHA, deps.targetBranch, effectiveMergeProtectBranches, deps.brPath)`): :3904 (review-loop, in outer retry loop :3891-3913), :4123 (DOT, hk-whru3/hk-vbv3b already-on-main special-case :4138-4142), :5252 (single agent_completed), :5302 (auto-close exit-0), :5374 (shutdown-drain, hk-dnrg, `context.Background()`).
- **Non-merge use A - :3636-3668**: serializes `ensureBaseOnWorker` (remote fetch/push base to worker) + `wtFactory(...)` (worktree create). Protects: (i) hk-lt091 - ControlMaster=no, sibling `fetchBaseOnWorker` races `git worktree add` at remote-OS level -> empty-HEAD worktree, retry-immune; (ii) hk-h8u7p - local worktree-add racing `projectDir/.git/index.lock`. **Needs the MERGE lock? No** - needs (a) worker-repo git-op mutual exclusion, (b) projectDir repo-IO exclusion vs the merge's tree-refresh. Comment :400-403 admits current arrangement is deliberate over-serialization. Under C2: worker ops under per-worker/create mutex; local adds ordered through the same projectDir-IO serialization the merge queue's reset/restore uses.
- **Non-merge use B - :5169-5175**: the escape check `checkMainWorkingTreeDirty` (:7763-7789; doc :7753-7756: "caller holds mergeMu across this call (hk-zguy6), so no sibling merge can be mid-flight between update-ref and reset-hard"). Needs exclusion **only against the update-ref->reset-hard window** - under C2, enqueue on (or fence by) the merge queue; a narrower "tree-quiescent" read slot suffices.

### Q4 — worktreeCreateMu

Definition workloop.go:386-407 (hk-5qp7z), production :1162. Threaded only into the **remote** wtFactory (:3602 `WithCreateMutex`); consumed in `workspace.CreateWorktree` (createworktree.go:163-165, held for whole add + HEAD-resolve retry). Serializes remote `git worktree add` retry-loops against each other. But because use-A still wraps the whole create in mergeMu, worktreeCreateMu is currently a **nested/redundant** second lock (its independent value: other CreateWorktree callers; it documents the intended end-state where create no longer rides the merge lock). Lock order: `mergeMu` -> `worktreeCreateMu` (inside). No reverse order exists -> no deadlock today; C2 must keep it that way or eliminate the nesting.

### Q5 — The queue/serialization idiom

- `internal/queue` (doc.go:1-12) is the **queue data model** (Queue/Group/Item records, persistence, RPC validation - specs/queue-model.md), NOT a serializing-execution idiom. Nothing to reuse for goroutine serialization.
- Daemon concurrency idioms (workloop.go): **buffered-channel semaphore** is the house pattern - `claimSem := make(chan struct{}, effectiveMax)` :1519-1530 (EM-050; explicit note: keep ceiling in the scheduler, not the adapter), `agentSpawnSem` cap-3 :409-430,:1163, acquire/release :4621-4633. Plain mutexes: mergeMu, worktreeCreateMu, followUpLedgerMu(:229), emittedEpicsMu(:437). RWMutex: cacheReapMu(:883-894).
- **No existing serializing-goroutine/actor (channel-fed single worker) pattern in the daemon.** So C2's merge queue is a new structure. Best fits: (a) a channel-fed single merge goroutine per target branch (true FIFO, gives the escape check a "quiescent slot"), or (b) keep a mutex but shrink the held window. (a) is the only way to get explicit ordering and matches the deps-struct injection style (nil-able field, production-wired in newWorkLoopDeps, test hook via daemonTestHooks).

### Q6 — br sync + working-tree refresh placement

Both **inside** the critical section today: `git reset --hard HEAD` (:7036), `br sync --import-only` (:7050-7065). `br sync` reads the **working-tree** `.beads/issues.jsonl`, so must run after **this merge's** 5b reset materializes the merged JSONL; 5a/5b in turn follow update-ref. Across merges the import is a reconcile-to-current-JSONL, so a later merge's sync subsumes an earlier one; only intra-merge ordering is load-bearing. Contention if moved out: concurrent `br` SQLite writes (the BrDbLocked storms that motivated claimSem :1519-1523) - a moved-out br sync should still be serialized with itself (queue tail or dedicated slot). daemon.go:1141 has br timeouts/retries for this path (hk-zgt4u). eventreg_hqwn59.go:101 assumes the tree-refresh happens within the merge op.

### Q7 — Fairness / ordering

**No explicit ordering guarantee exists.** `mergeMu` is a bare `sync.Mutex` (barging with >1ms starvation fallback). Nothing depends on cross-queue merge order: every entrant re-rebases onto whatever the target is at acquisition (:6679,:6786,:6961), and the retry loops absorb losing. Dispatch fairness is separate (`rrCursor` round-robin, NQ-B1, :1542-1548). A FIFO channel-fed merge queue would **strengthen** ordering (bounds a loser's re-rebase count from "unbounded races" to "queue depth ahead"), breaking nothing.

## Patterns to follow

- Deps-struct injection: nil-able field on `workLoopDeps`, production wiring in `newWorkLoopDeps` (:1157-1166), test override via `daemonTestHooks` + `WithMergeMutex`-style option (daemon.go:613-619,:2205-2207). The merge queue should replace/wrap mergeMu behind the same seam so scenariotest/concurrent_merge.go(:100) keeps working.
- Buffered-channel semaphore = house bounded-concurrency idiom; single-goroutine-plus-channel = natural extension for a strict serializer.
- Keep failure taxonomy stable: `isRetryableMergeReason` (:6445) + reason prefixes (`rebase_conflict*`, `non_ff_merge*`, `merge_fmt_failed*`, `merge_build_failed`, `push_failed*`, `rebase_dropped_commits*`, `strip_run_context_failed`) are matched by callers (:4141 `strings.Contains(reason,"rebase_dropped_commits")`).

## Risks / conflicts

1. **update-ref is not CAS** (:6821). Every rollback is a blind restore to remembered mainTip (:6870,:6906,:7430). Correctness rests entirely on the lock. Any C2 shrink must either keep all ref writes in one serialized window OR convert land+rollback to CAS - half-measures reintroduce hk-yyso7.
2. **hk-zguy6 invariant**: escape check (:5169) AND reset-hard (:7036) must end up fenced by the *same* mechanism, or false `implementer_escaped_worktree` reopens return. Silently couples the merge queue to a non-merge consumer.
3. **Speculative build staleness**: hoisting build/vet/fmt is sound only with "re-rebase => re-build" preserved. Today the inner loop re-runs per attempt (:6843-6892 inside `for`); a naive build-once-before-enqueue validates a stale tree after an FF-retry rebase.
4. **Fmt gate lands the ref twice** (:6821 then :7558) and creates commits mid-window; folding to a single final-tip landing is a prerequisite for a small critical section.
5. **Blind push-rollback semantics** couple push to the window; making push async is an M4 semantic change, not a silent C2 side effect.
6. **hk-sfy7f worktree-add inside the merge** (:6629-6637) + non-merge-use-A both need a projectDir repo-IO exclusion story once mergeMu stops being the universal umbrella (hk-h8u7p regression risk).
7. **Scenario gate TODO hk-ur428** (scenariogate.go:83-90): gate runs pre-rebase OUTSIDE mergeMu; a sibling landing a conflicting scenario change between gate and merge is not re-gated. Threading the rebased SHA through the queue is the acknowledged fix - at minimum don't make it worse.
8. **Shutdown-drain call site** (:5374) uses `context.Background()` - the merge queue must accept work during shutdown drain (hk-dnrg) and not tear down before in-flight merges complete.
9. **Cache-reaper interaction** (hk-44ab2 :6860, cacheReapMu :883): moving build out lengthens exposure to concurrent cache reaps; the cold-cache retry may need to stay (or the build take `cacheReapMu.RLock` like dispatch at :2971).

## PLANNER-RECONCILE

- **C2's "true critical section" is smaller than the whole merge, but push+rollback cannot leave it without an M4-class semantic change.** The decompose open-question (b) asked "is it only update-ref, or the whole FF-check->update-ref window?" Answer: the serialized window is FF-check -> update-ref -> [fmt land] -> push (+blind rollback) -> restore --staged -> reset --hard; the build/vet/fmt *executions* and rebase move out speculatively. Making `git push` async (so the window ends at local update-ref) requires the local-land-is-commit-point + reconciliation redesign that ROADMAP assigns to M4. C2 should ship the "build/rebase-out, ref-window-in" split and keep push inside the window; flagged because a reviewer may expect push also removed and it cannot be without crossing into M4.
