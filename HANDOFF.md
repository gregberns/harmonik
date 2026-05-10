<!-- HANDOFF v20 2026-05-09 main — bootstrap drained + post-mvh wave landed -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal. Stop closing responses with A/B questions when the lane is clear — execute.

PARALLEL FLOOR — HARD MINIMUM 10 ACTIVE IMPLEMENTERS.
Past sessions ran 2–3. That is unacceptable. Always have ≥10 implementer agents in flight. Refill on review-dispatch, not review-return: when you spawn a reviewer, immediately spawn the next implementer if the ready queue has non-overlapping work. The orchestrator's job is throughput, not careful sequencing.

IMPLEMENTER LIFECYCLE — WORK UNTIL ~250K, THEN HANDOFF.
Each implementer keeps claiming and working ready beads (sequentially, multiple commits on the same worktree branch) until its context exceeds ~250k tokens. Then it stops and runs the `/session-handoff` skill before exiting. The orchestrator merges what landed and dispatches a fresh implementer. Single-bead-per-agent is wasteful — implementers should consume multiple package-scoped beads per session.

DISPATCH SHAPE.
- Implementers: model=sonnet, effort=high, isolation=worktree, run_in_background=true.
- Reviewers: model=sonnet, effort=high, no isolation.
- Briefs ≤30 lines: bead-id(s), worktree pointer, helper prefix, sibling pointer, follow-up `br create` if needed.
- NEVER paraphrase the bead body. Implementer fetches via `br show` and reads cited spec.

PRE-FLIGHT (orchestrator, ≤3 reads per dispatch).
- Bead body via `br show <id> --format json`.
- The cited spec section.
- ONE canonical sibling for pattern conventions.
- Pre-dispatch grep for the bead's primary type name in the target package — if it exists, the bead may be SUBSUMED.

BEAD PICKING.
- Bootstrap-tagged queue is **structurally drained** as of v20. The dispatchable surface is now the broader `br ready` queue (140+ beads), MINUS the explicit out-of-scope set:
  - `hk-zs0.*` cognition / mechanism beads (architecture spec drafts).
  - `hk-hqwn.{67,68}` event-model spec-fix beads.
- `kle6.2` audit (this session) labeled 445 untagged beads as `post-mvh` — that label is a coarse classifier, NOT a hard scope cap. Many post-mvh beads are core spec coverage and should be implemented.
- Same-package-different-file = parallel-safe.
- Same-file conflict (3+ beads on one file) → ONE implementer with sequential commits.
- Bundle by package when possible: implementer claims one package's worth of ready beads and works them sequentially up to its 250k budget.

OUT OF SCOPE FOR DISPATCH (do NOT dispatch even from broader queue):
- `hk-zs0.*` cognition / mechanism beads (architecture spec drafts).
- `hk-hqwn.{67,68}` event-model spec-fix beads.

STANDING CONVENTIONS (full version: `.claude/implementer-protocol.md`).
- Bead body wins over docs; spec wins over bead body for normative content. Surface discrepancies.
- Typed-alias deferral: real follow-up bead via `br create`, ID substituted into godoc BEFORE commit. Implementers have been omitting this — orchestrator MUST inline-amend if missed (see hk-b3f.109 amend in this session).
- gofmt-clean struct alignment; lint clean; tests pass before commit.
- Worktree discipline: implementer commits in their worktree, never main. Verify `git branch --show-current` before each commit.
- Specaudit watchdog: every new normative requirement in `specs/*.md` MUST carry a `Tags: mechanism` or `Tags: cognition` line within 30 lines of its heading. Failures surface in `internal/specaudit/ar005_tags_test.go`.

REVIEWER TIER DISCIPLINE.
- MEDIUM = defect against THIS bead's acceptance criteria.
- Cross-cutting / future-bead / spec-doc concerns = MINOR or follow-up.
- Don't flag absent doc sections that aren't required here (## Why / ## Test plan / Reviewed-By trailer / Review-Verdict trailer).

INLINE-AMEND CEILING.
Trivial single-line text fix, literal one-line code fix, mechanical multi-line refactor verifiable by reading → orchestrator inline-amends, no fix-agent. Above ~3 mechanical edits in 1 file → spawn fix-agent on existing worktree. Re-review can be skipped after pure-deletion or trivial idiom-swap.

MERGE DANCE — RUN FROM `/Users/gb/github/harmonik`.
Use `git -C /Users/gb/github/harmonik` for ALL git ops to avoid bash-cwd drift inside worktrees. **CWD-DRIFT WARNING** — when a worktree is removed via the merge-dance loop, the bash shell's CWD can become stale, causing subsequent commands to fail with "No such file or directory". Always prefix with `git -C /Users/gb/github/harmonik` or explicit `cd /Users/gb/github/harmonik &&` before each merge step.

    cd /Users/gb/github/harmonik
    for id in <agent-id-1> <agent-id-2>; do
      WTPATH="/Users/gb/github/harmonik/.claude/worktrees/agent-$id"
      BRANCH="worktree-agent-$id"
      git -C "$WTPATH" rebase main
      git -C /Users/gb/github/harmonik merge --ff-only "$BRANCH"
      git -C /Users/gb/github/harmonik worktree unlock "$WTPATH" 2>/dev/null || true
      git -C /Users/gb/github/harmonik worktree remove --force --force "$WTPATH"
      git -C /Users/gb/github/harmonik branch -d "$BRANCH"
    done
    git -C /Users/gb/github/harmonik push origin main

**FALLBACK — cherry-pick when ff-merge fails.** Multiple times this session, `git merge --ff-only` reported "Already up to date" after rebase even though the new commit was clearly on the worktree branch (likely a cwd/index drift bug under heavy worktree churn). When this happens, **cherry-pick the worktree's tip commit directly onto main**: `git -C /Users/gb/github/harmonik cherry-pick <sha>`. Cleaner and reliable.

`br close` failures from `blocks` deps → flip to `related`:
    br dep remove <id> <other> ; br dep add <id> <other> --type related ; br close <id> -r "..."
Use `|| true` after each `br close` in chained pipelines.

`br update -d` does NOT exist — use `--description` or `--body`. `--notes` adds without overwriting. `br create` flags: `-p` priority, `--labels "a,b,c"`, `--parent <id>`.

REBASE-CONFLICT ON `go.mod` — DO NOT USE `git reset --soft main`.
If two beads both add the same `go.mod` dep and one rebases onto the other after merge, the rebase will conflict. **The fix is interactive rebase to drop the go.mod hunk, NOT `git reset --soft main`.** A worktree's working tree is frozen at its branch creation point — `reset --soft main` PRESERVES that stale tree and stages the (delta vs new main) as deletions of files that landed in other waves. The next commit would erase those files. Recovery cost: 30 min + closed bead requires re-implementation.

Correct go.mod conflict resolution:
1. `git -C "$WT" rebase --abort` (clean state).
2. `git -C "$WT" rebase -i main` and edit the offending commits to drop go.mod hunks (or use `--strategy-option theirs` for go.mod/go.sum specifically).
3. Verify `git -C "$WT" diff main -- go.mod go.sum` is empty before continuing.

RESUME RULE. On /session-resume with no hard blocker, EXECUTE — don't ask. Continue the current lane.

CONTEXT BUDGET (orchestrator). ~700k effective on this 1M model. At ~500k, finish in-flight wave cleanly, write fresh HANDOFF, stop.

<!-- END DIRECTIVES -->

# State

Main at `cf1d243`, pushed clean. `go test ./...` green across all packages (incl. internal/specaudit). No active worktrees. No in-flight implementers.

# This session — closed beads

**~38 beads closed across 3 dispatch waves + 1 recovery + 1 inline amend.**

## Recovery (start of session)

- `hk-i0tw.1` — Scenario YAML loader (SH-001 + alias-bomb fix). Re-applied via cherry-pick of `e40ecab` + `b52662a` from prior-session reflog onto main as `9236b16`. Bead reopened then re-closed. **Recovery procedure proven**: cherry-pick onto main, drop go.mod-conflict hunk via `git checkout HEAD -- go.mod go.sum && go mod tidy`.

## Wave 1 (initial dispatch — bootstrap-tagged surface)

- `hk-b3f.7/.8/.9` — SUBSUMED (Node already carries HandlerRef/Timeout/RequiredSkills/PolicyRef/GateRef/FreedomProfileRef/BudgetRef/IdempotencyClass with Valid() invariants)
- `hk-b3f.16` — RunStartedPayload typed record + 12 sensor tests (EM-015a)
- `hk-b3f.17` — RunCompletedPayload + RunFailedPayload + 27 sensor tests (EM-015b/EM-025); follow-up bead hk-b3f.109 filed for ErrorCategory typed alias
- `hk-b3f.30` — EM-024 branch-tip-equals-checkpoint sensor (4 tests)
- `hk-8i31.3` — HC-003 static-grep sensor for daemon real/twin branching (sibling pattern for many later sensors)
- `hk-8i31.77` — Twin binary §10.2 empty-type rejection in loadScriptFile
- `hk-8mwo.29/.30/.31/.32` — SUBSUMED (test-spec files at squashmerge_wm019_test.go, scratchworktree_wm019a_test.go, squashnonff_wm020_test.go, mergestatus_wm021_test.go cover the contracts end-to-end)
- `hk-kle6.2` — Corpus label reconciliation; 445 beads labeled `post-mvh`; audit at `docs/foundation/corpus-label-reconciliation-2026-05-09.md`
- `hk-kle6` (epic closed)
- `hk-8i31.78` — wire_ndjson_test.go with 6 wireFixture* tests covering well-formed sequence + 5 negative cases (HC-007a/b)
- `hk-b3f.10` — DefaultIdempotencyClassForNodeRole + NodeRole enum
- `hk-b3f.11` — EM-011 cross-field check Axes.Idempotency = IdempotencyClass in Node.Valid()
- `hk-b3f.42` — EM-033 no-transactionality sensor (4 tests)
- `hk-b3f.39` — EM-031 jsonldivergence_em031.go with torn-tail tolerance + 9 tests; ErrJSONLMidFileCorruption Cat-6b sentinel

## Wave 2 (post-mvh broader queue — after kle6.2 drain)

- `hk-8i31.23` — HC-019 context-values business-data sensor
- `hk-b3f.64` — EM-INV-004 no-transactionality static-grep sensor (17 patterns)
- `hk-8i31.22` — HC-018 CancelGoSideBound=500ms + CancelSubprocessBound=5s
- `hk-b3f.26` — EM-020a AuditViolationKind enum + AuditViolation record
- `hk-8i31.61` — HC-051 seam-boundary sensor (go-list import-graph scan)
- `hk-b3f.62` — EM-046b RetryCounter + 11 sensor tests
- `hk-b3f.21` — EM-017a corrupted-checkpoint detector + 8 tests
- `hk-b3f.55` — EM-042 Guards/Gates DispatchEdge pipeline + 13 tests
- `hk-b3f.43` — EM-034 SubWorkflowExpansion typed record
- `hk-8i31.81` — HC-028..034 + HC-INV-003 redaction-middleware fixture suite
- `hk-b3f.31` — EM-024a branch-tip monotonicity detector (ErrBranchTipRewound, 12 tests)
- `hk-hqwn.61` — Bus-overflow scenario harness (EV-011a + §8.8.4 bus_overflow payload sensors)

## Wave 3 (continued post-mvh queue)

- `hk-b3f.41` — EM-032 deterministic-replay sensor (5 tests)
- `hk-b3f.44` — EM-034a NamespaceNodeID concatenator
- `hk-b3f.45` — EM-034b SubWorkflowRefGraph DFS cycle detection
- `hk-hqwn.30` — EV-021 observational-replay advisory invariant sensor
- `hk-hqwn.31` — EV-022 reconstruction-not-JSONL sensor + DiscoverActiveRuns godoc citation
- `hk-b3f.56` — EM-042a GatePendingRecord + GateResolutionSignal enum (3 values)
- `hk-b3f.47/.49/.50` — EM-035 nested checkpoint, EM-036a sub-workflow terminal outcome, EM-037 sub-workflow as ONLY composition (18 tests across 3 commits)
- `hk-872.38.1/.38/.40` — BI-031 step 1 ReadIntentLogEntry + JSON tags + crash-recovery scan helpers + Cat 3a evidence sensor

## Inline orchestrator amends

- `f7e023b` — substitute hk-b3f.109 ID into RunFailedPayload.ErrorCategory godoc TODO (implementer for hk-b3f.17 omitted the typed-alias-deferral br create per protocol; orchestrator filed bead + amended godoc).
- `cf1d243` — add `Tags: mechanism` line to HC-036a in specs/handler-contract.md to satisfy AR-005 mutual-exclusion sensor (specaudit was failing at end-of-session).

## Lessons captured (also in directives)

- **Bootstrap surface is structurally drained.** kle6.2's audit revealed only 191 of 345 bootstrap-INCLUDE-set IDs are loaded into the corpus (HC=46, BI=20, SH=54+ missing). Of the 198 currently-tagged bootstrap beads, ~50+ have closed cumulative; the bootstrap-tagged ready queue is now ~1–2 beads at any time. **Use the broader `br ready` queue (currently 140+) MINUS the explicit out-of-scope set.** Most post-mvh beads are still core spec coverage.
- **CWD drift bug** under heavy worktree churn — see directives. When `git merge --ff-only` returns "Already up to date" but the worktree clearly has a new commit, fall back to `git cherry-pick <sha>`. Used ~5 times this session, never failed.
- **Typed-alias-deferral compliance** — implementers have been omitting the protocol-required `br create` follow-up. Orchestrator MUST inline-amend (file the bead + substitute the ID into the godoc) BEFORE pushing the implementer's commit forward, or the merge wave creates a TODO with no tracking.
- **Specaudit AR-005 watchdog** triggers on any new normative-requirement heading in `specs/*.md` without a nearby `Tags: mechanism` or `Tags: cognition` line. Implementers adding new spec sections must include the tag, or `internal/specaudit/ar005_tags_test.go` fails. Easy inline-amend if missed.

# Current lane

**Phase 1 implementation: behavioral beads, broader post-mvh surface.** Of 658 bead corpus: 198 `scope:bootstrap` (~80+ closed), 450 `post-mvh` (~30 closed via path-2 dispatch this session), 10 unlabeled epics. **140+ ready beads** across `br ready --limit 0`; 25 listed at end of session — first 7 non-zs0:

```
hk-872.38.2  Step 2 — br show <bead_id> for current coarse_status (BI)
hk-b3f.48    Sub-workflow entry/exit lifecycle events (continuation of .47/.49/.50 cluster)
hk-b3f.63    Sensor: git is the state-reconstruction source (sibling: .39, .41)
hk-hqwn.33   Replay cannot re-establish agent state or re-invoke LLMs
hk-8i31.19   Work queue per agent role
hk-8i31.38   Common prefix redaction rule (consumer of .81 fixture suite)
hk-8i31.79   Silent-hang FSM + heartbeat scenario harness
```

Plus zs0.* (out-of-scope) and hk-szv5 (still gated on hk-hqwn.59.82).

Dispatch in waves of ≥10 across non-overlapping packages — see directive note re: post-mvh elevation.

# Quick references

- `br ready --limit 0` — full dispatchable queue (NOT `-l scope:bootstrap`; that filter is structurally drained).
- `.claude/implementer-protocol.md` — full implementer rules.
- `STATUS.md` — high-level project state (may lag the handoff).
- `docs/decompose-to-tasks/bootstrap-subset.md` — 345-bead INCLUDE set (154 unloaded).
- `docs/foundation/trivial-slice-walkthrough.md` — 25 atomic ops × 7 groups → 55 owning bootstrap beads.
- `docs/foundation/corpus-label-reconciliation-2026-05-09.md` — kle6.2 audit; explains why bootstrap is drained.

# Open follow-up

- **hk-b3f.109** — typed-alias for `RunFailedPayload.ErrorCategory` (currently `*string` placeholder, godoc points to this bead). Filed this session.
- **154 unloaded bootstrap IDs** (HC=46, BI=20, SH=54+). Surfaced by kle6.2; not yet ingested. Optional Phase 1 corpus-management task; expanding scope:bootstrap surface would unlock additional bootstrap-tagged dispatches.
