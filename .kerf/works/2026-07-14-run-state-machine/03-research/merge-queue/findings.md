# 03-Research / merge-queue — C2 findings (mergeMu split)

> Pass 3 (Research), component C2. All `file:line` verified against the working tree
> 2026-07-14 (`internal/daemon/workloop.go`, 8184 lines). Method: full walkthrough of
> `lockedMergeRunBranchToMain`/`mergeRunBranchToMain` + a census of every `mergeMu`
> acquisition, per-step `cmd.Dir` attribution (main checkout vs run worktree), and the
> failure/retry semantics at all five call-sites.

---

## 1. `mergeMu` census — exactly three lexical acquisition regions

Declared `workloop.go:384`; production init `:1161` ("hk-yyso7: global
merge-serialisation across all queues"); test override `WithMergeMutex`
(`daemon.go:619`, `:2205`).

| Region | Lock/Unlock | Guards | IO held under lock |
|---|---|---|---|
| **A — remote base-sync + worktree-add** | Lock `:3637`; Unlock `:3651` (err) / `:3667` | `ensureBaseOnWorker` (`:3648`) + remote `wtFactory` → `workspace.CreateWorktree` (`:3665`) | SSH git fetch/push to the worker, `git worktree add` on the worker, box-A `.git/index.lock` writes — seconds over SSH |
| **B — escape-worktree check** | Lock `:5170`; Unlock `:5174` | `checkMainWorkingTreeDirty(activeRepo,…)` (`:5172`; helper `:7763`) | one `git status --porcelain` + `check-ignore` — read-only, brief |
| **C — the merge sequence** | Lock `:6513` / `defer` Unlock inside `lockedMergeRunBranchToMain` (`:6512`) | entire `mergeRunBranchToMain` (`:6544`) | rebase → `go build`/`go vet` → gofumpt/gci → update-ref → `git push origin` → `git reset --hard` → `br sync` |

## 2. `mergeRunBranchToMain` walkthrough — per-step Dir attribution

The main checkout is `activeRepo` (== `deps.projectDir` for local beads; cross-repo
target resolved `:3269`/`:3284`, which also nils `effectiveMergeProtectBranches`
`:3298–:3300`). The per-run worktree is `wtPath`.

**Guards (read-only):** protected-branch fail-closed `:6547–:6560`; step 1
`rev-parse` runBranch `:6565` (Dir=projectDir, missing → noChange); step 1b
`rev-parse` targetBranch `:6577`, `mainTip==runTip` → noChange `:6588`;
`runTip==headSHA` guard `:6598` (hk-cwxow).

**Rebase (worktree-local, reads shared target ref):** `wtPath` computed `:6611`;
hk-sfy7f fallback `git worktree add` when absent (remote runs) `:6629–:6637`
(Dir=projectDir — touches shared `.git/worktrees`) with deferred remove `:6634`;
`discardDirtyChurn` `:6653` / `commitResidualDelta` `:6662` / `cleanUntrackedFiles`
`:6677` / `git rebase <target>` `:6679` — all Dir=wtPath; conflict → `rebase --abort`
`:6687` + `rebase_conflict` `:6692`; re-resolve tips `:6696`/`:6701`; hk-zmpd
rebase-drop guard `:6713` → `rebase_dropped_commits`; `stripRunContextFromMerge`
`:6729` (Dir=wtPath, advances runTip).

**The true critical section — 3-attempt loop `:6750–:6994` (`maxPushAttempts=3`):**
- FF-check `git merge-base --is-ancestor mainTip runTip` `:6755` (Dir=projectDir).
  Non-FF + attempts left → re-resolve mainTip `:6772`, rebase in worktree `:6786`,
  continue `:6817`; non-FF last attempt → `non_ff_merge` `:6768`.
- **update-ref** `git update-ref refs/heads/<target> runTip` `:6821` (Dir=projectDir)
  — mutates the shared target ref, which is the CHECKED-OUT branch of the main checkout.
- **build gate UNDER the lock and AFTER update-ref** `:6843–:6879` — `go build ./...`
  + `go vet ./...` (Dir=buildDir; buildDir=wtPath if present else projectDir `:6844`);
  cold-cache retry `:6860`; failure → rollback update-ref to mainTip `:6870` +
  `merge_build_failed` `:6876`.
- **fmt gate** `runMergeFmtCheck` `:6889` (helper `:7429`) — gofumpt/gci
  (Dir=buildDir); when auto-fixable, commits in the worktree `:7532`/`:7544` then
  **a second shared-ref mutation** `git update-ref` `:7558–:7559` (Dir=projectDir);
  all failures roll back (`rollback()` `:7431`) → `merge_fmt_failed`.
- **push** `git push origin <target>` `:6895` (Dir=projectDir); success → break
  `:6899`; failure → rollback `:6906`; non-FF rejection with attempts left →
  `git fetch` `:6923`, read remote tip `:6934`, update-ref to remote tip `:6947`,
  rebase in worktree `:6961`, hk-zmpd re-guard `:6983`, loop; else `push_failed` `:6918`.

**Post-merge main-checkout refresh (EM-054, still under the lock):**
`git restore --staged .` `:7015` (best-effort) → **`git reset --hard HEAD`** `:7036`
(Dir=projectDir; resyncs the main checkout; failure non-fatal
`working_tree_refresh_failed` `:7042`) → **br sync** `:7050–:7065` (if
`git diff --name-only mainTip runTip` `:7051` touches `.beads/issues.jsonl`, run
`br sync --import-only` `:7056`, Dir=projectDir, mutates the beads SQLite; failure
non-fatal `bead_sync_failed`) → success `:7067`.

**Rollback pairing:** every shared-ref mutation inside the loop is paired with a
best-effort `update-ref … mainTip` rollback (`:6870`, `:6906`, fmt `:7431`); comments
`:6902–:6905` note Cat-3 reconciliation (EM-INV-005) catches local-ahead-of-remote on
next startup.

## 3. The five call-sites — identical parameter lists, different wrappers

All five call `lockedMergeRunBranchToMain(<ctx>, deps.mergeMu, activeRepo, runID,
deps.bus, beadID, headSHA, deps.targetBranch, effectiveMergeProtectBranches,
deps.brPath)`:

| Line | Terminal branch | ctx | Retry wrapper | Pre-steps |
|---|---|---|---|---|
| `:3904` | review-loop approve | ctx | **outer `maxMergeStepRetries=2`** loop `:3891–:3913` on `isRetryableMergeReason` (`:6445`: `rebase_conflict`/`non_ff_merge`/`merge_fmt_failed` prefixes); re-amends review trailers per attempt `:3899` | scenario gate `:3853`, `preMergeSync` `:3864`, trailer amend `:3881` |
| `:4123` | DOT cascade success | ctx | none | `preMergeSync` `:4106`, trailer amend `:4118`; carve-out `:4138–:4142`: `rebase_dropped_commits` + already-approved-on-main → treated as noChange (skip reopen) |
| `:5252` | agent_completed (stop-hook WORK_COMPLETE/REVIEWER_VERDICT) | ctx | none | scenario gate `:5235`, `preMergeSync` `:5244` |
| `:5302` | exit-0 auto-close (socketOutcome==nil && exit 0 && !watcherFailed) | ctx | none | scenario gate `:5285`, `preMergeSync` `:5294` |
| `:5374` | shutdown-drain (ctx cancelled, hk-dnrg) | **`bgCtx=context.Background()`** `:5372` | none | **NO preMergeSync**; only runs if `resolveWorktreeHEAD(wtPath)` shows a commit beyond headSHA `:5373` |

`preMergeSync` (`:3574–:3589`, closure in `runAgentImplementer`): remote-only
(`rbc != nil`) fetch of `refs/heads/run/<id>` from the worker onto box-A
(`fetchRunBranchBoxA` `:3581`, hk-7bwx); emits `worker_offline` + disables the worker
on SSH failure `:3583`. **Runs OUTSIDE mergeMu.** No-op for local runs.

## 4. The two non-merge mergeMu uses — invariants that must survive the split

**(a) Region A (remote fetch+create, `:3623–:3668`).** Comment `:3623–:3635`: with
`ControlMaster=no` (hk-zexsj) each SSH command is an independent connection; a
sibling's `git fetch` racing `git worktree add` at the remote OS level leaves an
empty-HEAD worktree (hk-lt091), and box-A `.git/index.lock` contention (hk-h8u7p).
`:401–:403` states this serialisation is DELIBERATELY still under `mergeMu` even
though `worktreeCreateMu` exists. **Breaks if merely dropped:** empty-HEAD remote
worktrees + index.lock races. The replacement must still span
ensureBaseOnWorker+create as one exclusive section — and because both a merge's
projectDir git ops and the create's box-A ops touch box-A `.git`, the design must
decide the exclusion-domain scope explicitly, not silently drop it.

**(b) Region B (escape check `:5162–:5175`, hk-zguy6).** A sibling's merge transiently
dirties the MAIN checkout between update-ref (`:6821`) and reset-hard (`:7036`);
holding mergeMu across `checkMainWorkingTreeDirty` guarantees no sibling is mid-merge,
so no false `implementer_escaped_worktree` reopen (`:5180`). Regression-tested:
`escapedetect_hkooexj_test.go:252–315,374–497`. **Breaks if dropped:** spurious
reopens whenever a sibling merge is in its update-ref→reset-hard window. The escape
check must remain mutually exclusive with the merge critical section.

## 5. `worktreeCreateMu` (`:407`, hk-5qp7z) — the existing partial split

Production init `:1162`; threaded via `WithCreateMutex` in the remote `wtFactory`
(`:3602`); taken inside `workspace.CreateWorktree` (`createworktree.go:154`,
Lock/Unlock `:163–:166`) around the entire worktree-add + HEAD-resolve retry loop.
Today the remote create at `:3665` is NESTED inside mergeMu — both locks held. So
`worktreeCreateMu` serialises creation among concurrent creators but has NOT
decoupled create from the merge lock.

## 6. Failure/retry semantics (behavior to preserve)

- Inner 3-attempt loop (`:6750`) covers FF-retry + push-non-FF-retry with
  re-fetch/re-rebase.
- Outer retry only at the review-loop site (`:3891`, 2 attempts, retryable reasons).
- Terminal merge failure at every site → EM-053 pattern: `emitOutcomeEmitted(…,
  "rejected", reason)` + `ReopenBead` + `emitDone(false)` (e.g. `:3914–:3919`,
  `:5253–:5259`, `:5303–:5309`); reopened bead → `open`; QM-002a reverts the queue
  item to pending (effective requeue).
- noChange/success → `emitOutcomeEmitted(…, "approved")` + `closeBeadWithHistoryTrim`
  + `emitBeadClosedAndMaybeEpic` + `emitDone(true)` (e.g. `:5260–:5274`).
- Events emitted from inside the merge: `merge_build_failed` (`:6873`),
  `working_tree_refresh_failed` (`:7042`, non-fatal), `bead_sync_failed` (`:7059`,
  non-fatal).

## 7. The answer to C2's design question (b) — the true critical section

**Must stay serialised (shared-state read-modify-write, all Dir=projectDir):**
FF-check `:6755` → update-ref `:6821` → fmt's second update-ref `:7558` → push
`:6895` → rollbacks (`:6870`/`:6906`/`:7431`) → `restore --staged` `:7015` →
`reset --hard HEAD` `:7036` → `br sync` `:7056`; plus the temp `worktree add/remove`
(`:6630`/`:6634`) touching shared `.git`; plus Region B's escape read; plus Region
A's box-A git ops.

**Movable outside the lock (worktree-local):** churn-discard/residual-commit/clean
(`:6653–:6677`), the rebase `:6679`, `stripRunContextFromMerge` `:6729`,
`go build`/`go vet` `:6852`, gofumpt/gci format+commit (`:7457–:7550`). Caveat: these
READ the shared target tip, so out-of-lock work is speculative and MUST be
re-validated under the lock — which the existing retry loop already does (re-resolve
mainTip + re-run FF-check per attempt). That retry structure is the natural seam.

**Ordering property to preserve:** one global single-writer on the shared target ref
across ALL queues (hk-yyso7). Named queues can reach the merge concurrently; the
replacement must strictly serialise the critical section while allowing the
speculative phase (rebase/build/fmt) to run concurrently per-run.

## 8. Implications carried to Change Design

1. Today build+vet run AFTER update-ref (under the lock, with rollback). A split
   that validates BEFORE the ref moves changes the transient window (no advanced ref
   during build) — outcome-preserving, and strictly safer for the escape check
   (§4b), whose exposure window shrinks to update-ref→reset only.
2. The fmt gate can auto-commit INTO the run worktree (`:7532–:7544`) and advance the
   ref again `:7558` — the speculative phase must include fmt, and the tip presented
   to the critical section is the post-fmt tip.
3. The shutdown-drain site's `bgCtx` (`:5372`) means the queue must accept
   submissions whose ctx outlives the run ctx.
4. Region A is remote-only; M4 re-plumbs it later, but C2 must keep an equivalent
   exclusion NOW (hk-lt091/hk-h8u7p) — keep it inside the same exclusion domain as
   the merge critical section until M4 owns it.
