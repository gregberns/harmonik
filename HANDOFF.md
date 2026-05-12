<!-- PP-TRIAL:v2 2026-05-12 main — v33. Workflow-modes corpus fully landed (32/32 children + epic closed). T-WM-027 SUBSUMED+ assertion landed; nothing in flight at session-end. -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read it on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`.

STREAM-NOT-WAVES (HARD RULE). The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. **On every implementer-completion notification, do exactly two things, in order:**

  1. Merge the returning implementer (rebase → ff-merge OR cherry-pick fallback → worktree teardown → bead-close-if-needed).
  2. Inspect dispatchable depth (`br ready --limit 0` minus in-flight claims minus excluded labels). If depth ≥ 1 and floor not met (target ≥10 active), spawn ONE replacement implementer. If depth = 0, note "queue draining" and stop spawning.

Per-return acknowledgment is ≤2 lines ("merged X, dispatched Y" OR "merged X, queue draining"). Full session summary lives at `/session-handoff` time, not inline.

TRUST `br ready` BUT VERIFY (HARD RULE — three checks).
`br ready` is NOT authoritative for "the corpus is drained." Three orthogonal filters can hide dispatchable work; check all three:

  1. **Stale `blocked_issues_cache` (L-011).** Cross-check `br stats` Open vs Ready — if Open ≫ Ready, suspect dep-model gridlock not corpus drain. Inspect blocker distribution: `br blocked --limit 0 --json | python3 -c "import json,sys;from collections import Counter;d=json.load(sys.stdin);d=d.get('issues',d) if isinstance(d,dict) else d;c=Counter();[c.update(b.get('blocked_by',[])) for b in d];print(c.most_common(20))"`. Recovery: `br doctor --repair` rebuilds the cache.
  2. **Parent-child gridlock (L-011).** If a single epic appears as the blocker for many beads: `sqlite3 .beads/beads.db "UPDATE dependencies SET type='related' WHERE type='parent-child'"`, wipe `blocked_issues_cache`, `br doctor --repair`. Backup `.beads/beads.db` first.
  3. **Stale `defer_until` (L-017).** A bead with `status=open` can still carry `defer_until: <future-date>` from a prior `br update --defer` and silently filter out of `br ready`. Detect via JSON: `br list --status open --limit 0 --json | python3 -c "import json,sys;d=json.load(sys.stdin)['issues'];print([(b['id'],b['defer_until']) for b in d if b.get('defer_until')])"`. Clear via `br update <id> --defer ""`.

DON'T ASK — EXECUTE.
On `/session-resume` with no hard blocker, EXECUTE — don't close the say-back with an A/B question (user's standing directive; memory `feedback_resume_continue_directive`). Sub-agents inherit via `.claude/implementer-protocol.md` — they make judgment calls and document reasoning in commit body. Orchestrator on genuine ambiguity: decide and document.

IMPLEMENTER LIFECYCLE — ENFORCED IN PROTOCOL.
`.claude/implementer-protocol.md` (updated 2026-05-10) is authoritative. Key rules: (a) implementer CLOSES OWN BEADS via `br close` after each commit, (b) implementer DOES THE BEADS NAMED IN ITS BRIEF AND EXITS — no free-claiming from `br ready`; orchestrator owns refill, (c) implementer DOES NOT ASK questions back. Brief template: appendix of `.claude/implementer-protocol.md`. Briefs MUST NOT include "after close, continue claiming X" lines.

DISPATCH SHAPE.
- Implementers: `model=sonnet`, `effort=high`, `isolation=worktree`, `run_in_background=true`.
- Reviewers: `model=sonnet`, `effort=high`, no isolation.
- Briefs ≤15 lines: see brief-template appendix in `.claude/implementer-protocol.md`. **Do NOT paraphrase the bead body.** Implementer fetches via `br show`.

PRE-FLIGHT (orchestrator, ≤3 reads per dispatch).
- Bead body via `br show <id> --format json`.
- The cited spec section.
- ONE canonical sibling for pattern conventions.
- Pre-dispatch grep for the bead's primary type name in the target package — if it exists, the bead may be SUBSUMED (sibling-pointer in brief; see L-008).

BEAD PICKING — POST-AUDIT SCOPE.
- **Canonical triage view (current initiative): `br ready --label next-init`.** The `next-init` label tags the active initiative's beads (currently the post-MVH parallelism epic `hk-e61c3` + its 6 children + `hk-p4xbw`). Drive dispatch from this view; do NOT free-roam `br ready` while a `next-init` set is open. When the current initiative closes, retire the label or rotate it to the next initiative. Per `bv` caveat: bv's robot-triage does not respect `post-mvh` or `status=deferred`; using `--label next-init` (`bv --robot-triage --label next-init`) is the workaround.
- Dispatchable depth: **`br ready --limit 0`** — filter rule: `... | grep -v "\[epic\]"`.
- **`post-mvh` label exclusion is HARD.** Always check labels before dispatching: `br show <id> --format json | python3 -c "..."`.
- Same-package-different-file = parallel-safe.
- Same-file conflict (3+ beads on one file) → ONE implementer with sequential commits OR serialize via per-bead dispatch.
- **Forward-port detection.** When dispatching beads in waves where dep beads are still in-flight, implementers may forward-port already-closed-but-not-yet-merged code into their worktrees. Rebase typically auto-skips identical content. Real conflicts surface in test files where both copies modified the same fixture; spawn fix-agent with `git checkout --ours` for the duplicated bits.
- **SUBSUMED beads are common in late waves.** When dispatching beads whose dependencies bundled in extra work (T-WM-020 packed cap-hit, no-progress, RC→APPROVE smoke tests inline), pre-grep the target package for the primary deliverable and add a sibling pointer to the brief; implementer will close as SUBSUMED without a commit.

STANDING CONVENTIONS (full version: `.claude/implementer-protocol.md`).
- Bead body wins over docs; spec wins over bead body for normative content. Surface discrepancies via L-009 channel.
- Typed-alias deferral: real follow-up bead via `br create`, ID substituted into godoc BEFORE commit.
- gofmt-clean, lint clean, tests pass before commit.
- Worktree discipline: implementer commits in their worktree, never main.

REVIEWER TIER DISCIPLINE.
- MEDIUM = defect against THIS bead's acceptance criteria.
- Cross-cutting / future-bead / spec-doc concerns = MINOR or follow-up.

INLINE-AMEND CEILING.
Trivial single-line text fix, literal one-line code fix, mechanical multi-line refactor → orchestrator inline-amends, no fix-agent. Above ~3 mechanical edits in 1 file → spawn fix-agent on existing worktree.

MERGE DANCE — RUN FROM `/Users/gb/github/harmonik`.
Use `git -C /Users/gb/github/harmonik` for ALL git ops to avoid bash-cwd drift inside worktrees.

    cd /Users/gb/github/harmonik
    for id in <agent-id-1> <agent-id-2>; do
      WTPATH="/Users/gb/github/harmonik/.claude/worktrees/agent-$id"
      BRANCH="worktree-agent-$id"
      git -C "$WTPATH" rebase main
      git -C /Users/gb/github/harmonik merge --ff-only "$BRANCH"
      git -C /Users/gb/github/harmonik worktree remove --force --force "$WTPATH"
      git -C /Users/gb/github/harmonik branch -d "$BRANCH"
    done
    git -C /Users/gb/github/harmonik push origin main

FALLBACK — cherry-pick when worktree dir is gone. A fix-agent that runs `git worktree remove` (or any agent that for any reason leaves the worktree dir absent) leaves the branch behind. To merge it, use `git -C /Users/gb/github/harmonik cherry-pick <sha>` against the branch tip. Validated v32: hk-xvpwb post-conflict landed via `git cherry-pick 782460d`.

REBASE-SKIP for duplicate-bead commits. When a long-running OLD-protocol implementer's branch carries a commit for a bead ALREADY closed by a newer-protocol dispatch in the same session, `git rebase main` will hit add/add or content conflicts. Use `git rebase --skip`. Cross-package signature mismatches DO NOT surface as text conflicts; always run `go vet ./...` after the last merge of a session and inline-fix.

`br close` failures from `blocks` deps → flip to `related`:
    br dep remove <id> <other> ; br dep add <id> <other> --type related ; br close <id> -r "..."

`br update -d` does NOT exist — use `--description` or `--body`. `--notes` adds without overwriting. `br update --defer ""` clears `defer_until` (see L-017). `br create` flags: `-p` priority, `--labels "a,b,c"`, `--parent <id>`.

REBASE-CONFLICT ON `go.mod` — DO NOT USE `git reset --soft main`. Use `git rebase -i main` to drop the offending hunk, or `--strategy-option theirs` for go.mod/go.sum specifically.

`bv` USAGE (added to CLAUDE.md v32). `bv --robot-triage` is a graph-aware triage sidecar. Reads only — `br` writes. Bare `bv` launches an interactive TUI that blocks the session; use only `--robot-*` flags. **Caveat:** bv ranks by graph metrics and does NOT respect the `post-mvh` label exclusion — its top picks may be deferred/post-mvh. Always cross-check labels before dispatching from a bv recommendation.

CONTEXT BUDGET (orchestrator). ~700 k effective. At ~500 k, finish in-flight stream cleanly, write fresh HANDOFF, stop. v32→v33 spanned the session without hitting the ceiling (~25% used at handoff).

<!-- END DIRECTIVES -->

# State (v33, 2026-05-12)

Main at `ecbe43e` (pushed). **Nothing in flight.** Stream is fully drained.

**Workflow-modes corpus FULLY LANDED**: epic `hk-7om2q` CLOSED, all 32 child beads CLOSED (some with commits, some SUBSUMED by T-WM-020's wide deliverable that bundled cap-hit, no-progress, and happy/RC smoke tests inline). T-WM-027 added one missing assertion on review-verdict archive (commit `ecbe43e`).

# What landed v33 (incremental since v32 handoff)

- **T-WM-020** (`bd2c4f5`) — Daemon work-loop review-loop dispatch driver. ~1k lines across `reviewloop.go` + `reviewloop_test.go` + `eventtype.go` + `workloop.go` wiring. This is the central integration node.
- **T-WM-024** (`ffe0ea2`) — `review_loop_cycle_complete` exactly-once emission tests across all 5 termination paths.
- **T-WM-025** (`598d839`) — `run_id` propagation assertions for every §8.1a payload.
- **T-WM-022** (`b8b9067`) — No-progress termination acceptance test (impl was SUBSUMED in T-WM-020).
- **T-WM-021** (SUBSUMED) — cap-hit path already shipped in T-WM-020.
- **T-WM-026** (SUBSUMED) — happy-path smoke already shipped in T-WM-020.
- **T-WM-027** (`ecbe43e`) — RC→APPROVE smoke; SUBSUMED+ — added missing `review.iter-1.json` archive assertion.
- **Epic hk-7om2q CLOSED.**
- **hk-nmiww** (`0aafef8`) — br show field-name regression guard.
- **hk-5wbzj** (`00b3791`) — Concurrent ClaimBead characterisation test (surfaced **a real finding — see below**).
- **hk-33tcf** (`12c5d1d`) — beadLedger interface godoc on intentional body-read absence.
- **hk-4oyc2** + **hk-5dade** closed as SUBSUMED (already documented).

# Notable finding from hk-5wbzj (file follow-up bead next session)

`br --claim` (br v0.1.45) is NOT atomically exclusive at the application level: two concurrent `br --db <path> update <id> --claim` calls BOTH return exit 0 even when the bead is already `in_progress`. The characterisation test `internal/daemon/t5_realdb_concurrent_test.go` documents this as `FINDING_DOUBLE_DISPATCH` and passes regardless. **MVH is safe today** (single work loop per daemon); concurrent runs (post-MVH) need either br-level atomicity or a harmonik-side "check status before dispatch" guard. **Next session: file a P2 bead under the parallelism-prep label, body pointing at that test.**

# Pre-existing test failures observed this session (track separately)

- `TestRunEM012_Spec*` in `internal/core` — fixture clipping picks up EM-012a paragraph instead of EM-012 — observed during T-WM-002.
- `TestHC004LaunchIdempotentOnRunIDNodeID` in `internal/specaudit` — spec text the test scans for is not present in the spec — observed during T-WM-004.
- `TestT4_ReopenThenRedispatch` — flaky exploratory — observed during hk-7s9z9.
- `TestT6_UnicodeHeavyBody` — long-running timeout — observed during T-WM-008.
- `TestT6_10BeadSequentialDrain` — pre-existing timing flake — observed during hk-5wbzj.

None block any current bead. **Consider a small "exploratory-test hygiene" sweep bead next session.**

# Where to go next

1. **File the follow-up beads** described above (br --claim atomicity finding; exploratory-test hygiene sweep).
2. **Update STATUS.md / TASKS.md** to reflect workflow-modes shipped (per CLAUDE.md decisions-locked-in list).
3. **Resume `br ready` triage.** Without workflow-modes, the queue is mostly `post-mvh`-labeled. The natural next initiative is the **post-MVH parallel-runs work** that the four parallelism-prep beads were prep for (hk-cdb9f AdapterRegistry RWMutex, hk-fx6zl per-run Drain, hk-7s9z9 RunRegistry, hk-5zode JSONLWriter batching — all landed v32). User should pick the next direction; ask if uncertain.

# Files to open first

- Latest commits: `git -C /Users/gb/github/harmonik log --oneline -20` — get a feel for v33 shape
- `internal/daemon/reviewloop.go` and `internal/daemon/reviewloop_test.go` — the workflow-modes integration core
- `internal/daemon/t5_realdb_concurrent_test.go` — the `FINDING_DOUBLE_DISPATCH` test (basis for the follow-up bead)
- `.claude/implementer-protocol.md` — unchanged
- `STATUS.md` and `TASKS.md` — likely need an update for the shipped epic

# Blocking question for the user

None. Continue once T-WM-027 returns. If it's SUBSUMED, cleanup is one worktree-teardown and we're done with the corpus.
