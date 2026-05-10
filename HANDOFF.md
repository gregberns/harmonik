<!-- PP-TRIAL:v2 2026-05-10 main — dep-model gridlock fixed; ready surface = 487 -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log for this orchestration. Read it on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`. The bootstrap process is itself a use-case for the harmonik product; this is how we capture the learnings that should be embedded in the daemon's design.

STREAM-NOT-WAVES (HARD RULE — replaces the v20 "PARALLEL FLOOR" framing).
The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. **On every implementer-completion notification, do exactly two things, in order:**

  1. Merge the returning implementer (rebase → ff-merge OR cherry-pick fallback → worktree teardown → bead-close-if-needed).
  2. Inspect dispatchable depth (`br ready --limit 0` minus in-flight claims minus excluded labels). If depth ≥ 1 and floor not met (target ≥10 active), spawn ONE replacement implementer. If depth = 0, note "queue draining" and stop spawning.

Do NOT wait for sibling implementers to return before refilling. Do NOT batch-summarize between dispatches. Per-return acknowledgment is ≤2 lines ("merged X, dispatched Y" OR "merged X, queue draining"). Full session summary lives at `/session-handoff` time, not inline.

If you find yourself writing a 50-line wave summary mid-session, stop — that's the L-001 / L-004 anti-pattern (`docs/orchestration-learnings.md`). Resume the stream.

TRUST `br ready` BUT VERIFY (HARD RULE — added v22 from L-011).
`br ready` is NOT authoritative for "the corpus is drained." Beads' index treats some structural edges as blocking; in v21 this hid 485 dispatchable beads behind their open parent epics. Before declaring "queue drained":
  1. Cross-check `br stats` Open count vs Ready to Work — if Open ≫ Ready, suspect dep-model gridlock not corpus drain.
  2. Inspect blocker distribution: `br blocked --limit 0 --json | python3 -c "import json,sys;from collections import Counter;d=json.load(sys.stdin);c=Counter();[c.update(b['blocked_by']) for b in d];print(c.most_common(20))"`. If a single epic appears as the blocker for many beads, parent-child gridlock is back.
  3. Recovery (only if gridlock is back): `sqlite3 .beads/beads.db "UPDATE dependencies SET type='related' WHERE type='parent-child'"`, wipe `blocked_issues_cache`, `br doctor --repair`. Backup `.beads/beads.db` first. **Trade-off**: `br epic status` reports 0/0 children after the conversion — we accept this for MVH.

DON'T ASK — EXECUTE.
On `/session-resume` with no hard blocker, EXECUTE — don't close the say-back with an A/B question (this is the user's standing directive; memory `feedback_resume_continue_directive`). Sub-agents inherit the same rule via `.claude/implementer-protocol.md` — they do not ask questions back; they make judgment calls and document reasoning in the commit body. If you (orchestrator) hit a genuine ambiguity, decide and document; user-readable explanation goes in the next handoff or the immediate ≤2-line ack.

IMPLEMENTER LIFECYCLE — ENFORCED IN PROTOCOL.
`.claude/implementer-protocol.md` (updated 2026-05-10) is now authoritative for implementer behavior. Key rules: (a) implementer CLOSES OWN BEADS via `br close` after each commit (clarified — agent owns), (b) implementer CONTINUES claiming ready beads until ~250 k tokens or queue empty, (c) implementer DOES NOT ASK questions back. The orchestrator brief should NOT re-state these — point to the protocol and trust it. **Brief template**: appendix of `.claude/implementer-protocol.md`; orchestrator briefs are now parameter-fills against it (worktree path/branch + scope + ONE sibling pointer + optional deferral shape — ≤15 lines).

DISPATCH SHAPE.
- Implementers: `model=sonnet`, `effort=high`, `isolation=worktree`, `run_in_background=true`.
- Reviewers: `model=sonnet`, `effort=high`, no isolation.
- Briefs ≤15 lines: see brief-template appendix in `.claude/implementer-protocol.md`. **Do NOT paraphrase the bead body.** Implementer fetches via `br show` and reads cited spec.

PRE-FLIGHT (orchestrator, ≤3 reads per dispatch).
- Bead body via `br show <id> --format json`.
- The cited spec section.
- ONE canonical sibling for pattern conventions.
- Pre-dispatch grep for the bead's primary type name in the target package — if it exists, the bead may be SUBSUMED (sibling-pointer in brief; see L-008 for SUBSUMED-detection lift).

BEAD PICKING — POST-DEP-MODEL-FIX SCOPE.
- Dispatchable depth is **`br ready --limit 0`** (487 beads as of session-end 2026-05-10), MINUS any in-flight claims. The `hk-zs0.*` cognition spec drafts are **deferred until 2027-01-01** — they will not appear in `br ready`. Epics (`hk-b3f`, `hk-pvcs`, `hk-872`, `hk-hqwn`, etc.) ARE in `br ready` post-conversion but should still be skipped — they are containers, not work units. Filter rule: `br ready --limit 0 | grep -v "\[epic\]"`.
- Same-package-different-file = parallel-safe.
- Same-file conflict (3+ beads on one file) → ONE implementer with sequential commits.

STANDING CONVENTIONS (full version: `.claude/implementer-protocol.md`).
- Bead body wins over docs; spec wins over bead body for normative content. Surface discrepancies via the L-009 channel (orchestration-learnings.md).
- Typed-alias deferral: real follow-up bead via `br create`, ID substituted into godoc BEFORE commit. Implementer protocol covers this; orchestrator inline-amends only if missed.
- gofmt-clean struct alignment; lint clean; tests pass before commit.
- Worktree discipline: implementer commits in their worktree, never main.
- Specaudit watchdog: every new normative requirement in `specs/*.md` MUST carry a `Tags: mechanism` or `Tags: cognition` line within 30 lines of its heading. Failures surface in `internal/specaudit/ar005_tags_test.go`.

REVIEWER TIER DISCIPLINE.
- MEDIUM = defect against THIS bead's acceptance criteria.
- Cross-cutting / future-bead / spec-doc concerns = MINOR or follow-up.

INLINE-AMEND CEILING.
Trivial single-line text fix, literal one-line code fix, mechanical multi-line refactor verifiable by reading → orchestrator inline-amends, no fix-agent. Above ~3 mechanical edits in 1 file → spawn fix-agent on existing worktree. Re-review can be skipped after pure-deletion or trivial idiom-swap.

MERGE DANCE — RUN FROM `/Users/gb/github/harmonik`.
Use `git -C /Users/gb/github/harmonik` for ALL git ops to avoid bash-cwd drift inside worktrees. CWD-DRIFT WARNING — when a worktree is removed via the merge-dance loop, the bash shell's CWD can become stale. Always prefix with `git -C /Users/gb/github/harmonik` or explicit `cd /Users/gb/github/harmonik &&` before each merge step.

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

FALLBACK — cherry-pick when ff-merge fails (validated again this session — see L-010). When `git merge --ff-only` reports "Already up to date" after rebase but the worktree clearly has a new commit, fall back to `git -C /Users/gb/github/harmonik cherry-pick <sha>`. If cherry-pick conflicts (rare), resolve manually and continue. Do NOT use `git reset --soft main` from a worktree — it preserves stale tree state and stages deletions of files in other waves.

`br close` failures from `blocks` deps → flip to `related`:
    br dep remove <id> <other> ; br dep add <id> <other> --type related ; br close <id> -r "..."

`br update -d` does NOT exist — use `--description` or `--body`. `--notes` adds without overwriting. `br create` flags: `-p` priority, `--labels "a,b,c"`, `--parent <id>`.

REBASE-CONFLICT ON `go.mod` — DO NOT USE `git reset --soft main`. Use `git rebase -i main` to drop the offending hunk, or `--strategy-option theirs` for go.mod/go.sum specifically. Verify `git -C "$WT" diff main -- go.mod go.sum` is empty before continuing.

CONTEXT BUDGET (orchestrator). ~700 k effective on this 1M-context model. At ~500 k, finish in-flight stream cleanly, write fresh HANDOFF, stop.

<!-- END DIRECTIVES -->

# State

Main at `805bf16`, pushed clean. `go test ./...` last green at v21 (no code changes this session — only doc + local Beads DB). No active worktrees. No in-flight implementers.

# This session — what changed

Two structural fixes, both targeting the "queue is drained" false-positive that had been blocking the last 3 sessions.

1. **Deferred all 48 `hk-zs0.*` cognition spec drafts** until 2027-01-01 via `br defer`. They no longer appear in `br ready`. They are user-judgment work (foundation/architecture decisions); the kerf work `harmonik-foundation` (on the bench, jig=spec, status=research) is the right venue when the user is ready.

2. **Fixed Beads dep-model gridlock (L-011).** Beads' SQL index `idx_dependencies_blocking` treats `parent-child` edges as blocking. The corpus had 992 parent-child edges; every sub-bead was "blocked" by its open parent epic → full DAG gridlock. `br ready` returned 2; the real dispatchable surface was hundreds of beads. Converted all parent-child → related in the local DB. **`br ready` went from 2 → 487.** Backup at `.beads/beads.db.bak-20260510-003338`. The change lives only in the local SQLite + flushed JSONL; `.beads/` is gitignored, so on a fresh clone the gridlock would return — see directives "TRUST `br ready` BUT VERIFY" for recovery.

   Trade-off: `br epic status` now reports 0/0 children for every epic. Acceptable for MVH; epic rollup can be derived from labels (`spec:event-model` etc.) when needed.

3. **Brief-template appendix landed** in `.claude/implementer-protocol.md` (L-005 fix). Orchestrator briefs collapse from ~25 lines to ≤15 parameter-fills. Commit `805bf16`.

# Current lane

The dispatchable surface is now **487 beads deep**, spanning every implementation epic: `hk-872.*` (Beads Integration), `hk-hqwn.*` (Event Model — including the §8 row beads), `hk-8i31.*` (Handler Contract), `hk-63oh.*` (Reconciliation), `hk-sx9r.*` (Operator NFR), `hk-8mwo.*` (Workspace), `hk-8mup.*` (Process Lifecycle), `hk-a8bg.*` (Control Points), `hk-i0tw.*` (Scenario Harness), `hk-b3f.*` (Execution Model), `hk-872.*` (BI), `hk-ahvq.*` (twin-binary scaffolding).

**Next session: dispatch a stream.** Pick a coherent slice (e.g., `hk-hqwn.*` event-row beads — many are blocked-by-1 on pre-existing closed beads, so they should fall straight into ready), pre-flight 3 reads per dispatch, spawn 6–10 implementers in parallel, then run STREAM-NOT-WAVES on returns. The brief template in the protocol appendix should make briefs cheap.

# Open follow-up

- **L-009 fixture-first spec review** — still open; not session-blocking.
- **154 unloaded bootstrap IDs** — kle6.2 audit (HC=46, BI=20, SH=54+); ingestion still optional.
- **Epic-progress tooling** — `br epic status` is broken post-conversion. If the user wants epic rollup back, two paths: (a) restore parent-child edges and modify Beads' index to exclude parent-child from the blocking set, or (b) write a label-based rollup script.
- **`harmonik-foundation` kerf work** — 16d idle on the bench; this is the venue for the deferred zs0 work when the user is ready to land cognition decisions.

# Quick references

- `docs/orchestration-learnings.md` — friction log; read on every resume (L-011 is new and important).
- `.claude/implementer-protocol.md` — implementer rules + brief-template appendix.
- `br ready --limit 0 | grep -v "\[epic\]"` — true dispatchable queue (skip epic containers).
- `STATUS.md` — high-level project state.
- `docs/decompose-to-tasks/bootstrap-subset.md` — 345-bead INCLUDE set.
