# 04-Design / merge-queue — `internal/mergeq` (the mergeMu split)

> Component design for C2, within pins M3-D1/M3-D5. Facts cite
> `03-research/merge-queue/findings.md` (MF).

## 1. The package

```go
package mergeq // internal/mergeq — leaf; depguard deny internal/daemon (TC-6 pattern)

type Queue struct{ /* submission chan; owner goroutine; slog */ }
func New(logger *slog.Logger) *Queue
func (q *Queue) Start(ctx context.Context)         // owner goroutine; drains FIFO
// Submit runs critical inside the exclusion domain, strictly FIFO across all
// submitters. Blocks until executed or ctx cancelled BEFORE execution starts
// (once started, critical runs to completion under its own ctx — the
// shutdown-drain submission passes a Background-derived ctx, MF §3 site 5).
func (q *Queue) Submit(ctx context.Context, label string, critical func(context.Context) error) error
```

- **Strict FIFO** (channel order), one executor goroutine: preserves hk-yyso7's
  global single-writer across ALL named queues (MF §7) and answers decompose
  Q(c) fairness — arrival order, no starvation possible because critical
  sections no longer contain builds (bounded to seconds).
- A queue (not a mutex) because: explicit ordering, wait-time observability
  (slog: label, depth, wait_ms — NO durable event in M3, M3-D10), a
  Submit-shaped surface M4 threads remote merges through (the HARD M4 prereq),
  and testability (a recording fake asserts what ran inside the domain).
- `worktreeCreateMu` (`workloop.go:407`) is UNCHANGED (it guards concurrent
  creators inside workspace.CreateWorktree, MF §5).

## 2. The merge split: prepare (outside) / commit (inside)

`mergeRunBranchToMain` (workloop.go:6544) refactors into two daemon-side
functions with the queue between them; total behavior per MF §2:

**`prepareMerge(runCtx, wt, observedMainTip) (prepared, outcome)`** — OUTSIDE
the queue, per-run-worktree-local (all Dir=wtPath/buildDir): guards (protected
branch, rev-parses, hk-cwxow), churn-discard / residual-commit / clean, `git
rebase <target>` (conflict → `rebase_conflict`), hk-zmpd drop guard,
stripRunContextFromMerge, `go build ./...` + `go vet ./...` (cold-cache retry),
gofumpt/gci INCLUDING the auto-fix commit into the worktree (MF §8.2 — the tip
presented onward is the post-fmt tip). Produces `prepared{runTip,
observedMainTip}` or a terminal outcome (`merge_build_failed`,
`merge_fmt_failed`, `rebase_conflict`, `rebase_dropped_commits`, `noChange`).

**`commitMerge(qctx, prepared) outcome`** — INSIDE `Queue.Submit`: re-resolve
mainTip; **if mainTip ≠ prepared.observedMainTip → return `ErrStale`** (caller
re-prepares — this is the re-validate-under-lock discipline MF §7 requires);
FF-check (`merge-base --is-ancestor`); `git update-ref`; `git push origin`
(non-FF push rejection → fetch + return ErrStale variant); rollback pairing on
any failure (`update-ref … mainTip`, best-effort + EM-INV-005 note, MF §2);
`git restore --staged .`; `git reset --hard HEAD`; conditional
`br sync --import-only`. All Dir=projectDir.

**The retry loop** (`maxPushAttempts=3`, MF §2) becomes the caller's
prepare↔commit cycle with identical attempt budget and identical
reason-string vocabulary (`isRetryableMergeReason` unchanged, `:6445`). The
review-loop's outer ×2 retry with trailer re-amend is untouched (a Run-machine
transition, c4-runexec-reactor-design §4).

**Key behavior deltas (both allowlisted, M3-D12):** build/fmt failures no longer
transiently advance the target ref (they now fail BEFORE update-ref — same
events emitted, safer transient state); the escape-check exposure window shrinks
to the commit phase only.

## 3. Outcome surface

`commitMerge`/`prepareMerge` outcomes map 1:1 onto today's `mergeOutcome`
reasons so `EvMergeResult` (c4-runexec-reactor-design §1) carries byte-identical reason
strings: `success`, `noChange`, `rebase_conflict`, `non_ff_merge`,
`merge_build_failed`, `merge_fmt_failed`, `push_failed`,
`rebase_dropped_commits` (+ the DOT alreadyApprovedOnMain interpretation stays
in the Run machine). Events emitted from inside the phases keep their exact
types/sites: `merge_build_failed` (prepare), `working_tree_refresh_failed` +
`bead_sync_failed` (commit, non-fatal).

## 4. The other two exclusion-domain members

1. **Escape check (hk-zguy6, MF §4b):** `checkMainWorkingTreeDirty` runs via
   `Queue.Submit(ctx, "escape-check", …)` — same domain, so no sibling is inside
   its update-ref→reset window during the read. Cheap (one `git status`).
   The regression tests (`escapedetect_hkooexj_test.go:252–:315,:374–:497`) must
   stay green unmodified.
2. **Remote base-sync + worktree-add (hk-lt091/hk-h8u7p, MF §4a):**
   `ensureBaseOnWorker` + create submit as ONE critical section, unchanged scope
   (box-A `.git` + worker git ops must exclude merge commits). M3 does NOT
   narrow this; M4 re-homes it. Explicitly recorded so the invariant is not
   silently dropped.

## 5. The mechanical DoD check (census M3 DoD 2, as amended by M3-D5)

`internal/mergeq` tests inject a recording command-runner into the daemon-side
prepare/commit closures and assert:
- **No build-class command** (`go build`, `go vet`, `gofumpt`, `gci`,
  `git rebase`) executes between Submit-entry and Submit-exit.
- The commit phase's command inventory is exactly the enumerated allowlist
  (rev-parse, merge-base, update-ref, push, fetch, restore, reset, diff,
  br sync) — a change-detector for critical-section creep.
- FIFO: N concurrent Submits execute in submission order.
- A stale-tip Submit returns ErrStale without mutating any ref.

## 6. Self-review notes

- **Push stays inside the domain** — the deliberate deviation from the
  problem-space's literal goal, argued in M3-D5 (rollback pairing + escape
  window). Reviewer challenge welcomed; the counter-design (push outside) must
  answer: who rolls back an advanced local ref when a later push fails after a
  sibling merged on top of it?
- **One domain for all three members** (vs a separate lock for Region A): kept
  single because both touch box-A `.git` (MF §4a) — split only when M4 gives
  Region A its own home.
- **ErrStale re-prepare loop** could theoretically livelock under continuous
  merge pressure; bounded by the same 3-attempt budget as today (behavior
  unchanged — today's loop re-rebases under the SAME contention).
