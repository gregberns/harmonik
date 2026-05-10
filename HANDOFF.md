<!-- PP-TRIAL:v2 2026-05-10 main — wave-3 landed; protocol clarified; learnings log seeded -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log for this orchestration. Read it on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`. The bootstrap process is itself a use-case for the harmonik product; this is how we capture the learnings that should be embedded in the daemon's design.

STREAM-NOT-WAVES (HARD RULE — replaces the v20 "PARALLEL FLOOR" framing).
The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. **On every implementer-completion notification, do exactly two things, in order:**

  1. Merge the returning implementer (rebase → ff-merge OR cherry-pick fallback → worktree teardown → bead-close-if-needed).
  2. Inspect dispatchable depth (`br ready --limit 0` minus in-flight claims minus excluded labels). If depth ≥ 1 and floor not met (target ≥10 active), spawn ONE replacement implementer. If depth = 0, note "queue draining" and stop spawning.

Do NOT wait for sibling implementers to return before refilling. Do NOT batch-summarize between dispatches. Per-return acknowledgment is ≤2 lines ("merged X, dispatched Y" OR "merged X, queue draining"). Full session summary lives at `/session-handoff` time, not inline.

If you find yourself writing a 50-line wave summary mid-session, stop — that's the L-001 / L-004 anti-pattern (`docs/orchestration-learnings.md`). Resume the stream.

DON'T ASK — EXECUTE.
On `/session-resume` with no hard blocker, EXECUTE — don't close the say-back with an A/B question (this is the user's standing directive; memory `feedback_resume_continue_directive`). Sub-agents inherit the same rule via `.claude/implementer-protocol.md` — they do not ask questions back; they make judgment calls and document reasoning in the commit body. If you (orchestrator) hit a genuine ambiguity, decide and document; user-readable explanation goes in the next handoff or the immediate ≤2-line ack.

IMPLEMENTER LIFECYCLE — ENFORCED IN PROTOCOL.
`.claude/implementer-protocol.md` (updated 2026-05-10) is now authoritative for implementer behavior. Key rules: (a) implementer CLOSES OWN BEADS via `br close` after each commit (clarified — agent owns), (b) implementer CONTINUES claiming ready beads until ~250 k tokens or queue empty, (c) implementer DOES NOT ASK questions back. The orchestrator brief should NOT re-state these — point to the protocol and trust it.

DISPATCH SHAPE.
- Implementers: `model=sonnet`, `effort=high`, `isolation=worktree`, `run_in_background=true`.
- Reviewers: `model=sonnet`, `effort=high`, no isolation.
- Briefs ≤15 lines (was 30 in v20 — protocol consolidation lets them shrink): worktree path + branch name, package filter or starting bead, ONE sibling-pattern pointer (file:line covering an acronym the bead cites), follow-up `br create` shape if the bead requires typed-alias deferral. **Do NOT paraphrase the bead body.** Implementer fetches via `br show` and reads cited spec.

PRE-FLIGHT (orchestrator, ≤3 reads per dispatch).
- Bead body via `br show <id> --format json`.
- The cited spec section.
- ONE canonical sibling for pattern conventions.
- Pre-dispatch grep for the bead's primary type name in the target package — if it exists, the bead may be SUBSUMED (sibling-pointer in brief; see L-008 for SUBSUMED-detection lift).

BEAD PICKING — POST-WAVE-3 SCOPE.
- Bootstrap-tagged queue is structurally drained as of v20.
- Dispatchable surface is `br ready --limit 0` MINUS:
  - `hk-zs0.*` cognition / mechanism architecture spec drafts (out-of-scope; require user-driven design decisions, not implementation).
  - hk-hqwn.{67,68} — CLOSED 2026-05-10. No longer excluded.
  - epic beads (`hk-b3f`, `hk-pvcs`) — these are containers, not work units.
- As of session-end 2026-05-10, dispatchable depth is THIN: roughly 5–9 non-zs0 ready beads. Most session work going forward needs cognition decisions to unblock the next wave (zs0.* drafts) — that's user-judgment work, not orchestrator dispatch work.
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

Main at `e737ef3`, pushed clean. `go test ./...` green. No active worktrees. No in-flight implementers. `.claude/implementer-protocol.md` updated this session.

# This session — beads closed

**~72 beads closed across 28 implementer dispatches in 3 waves** (waves 1+2 in batch-mode, wave 3 partway to stream-mode — see L-001 in the learnings log).

Notable landings:

- **Spec coverage:** ~30 sensor/harness fixtures across `internal/operatornfr/`, `internal/core/`, `internal/lifecycle/`, `internal/handlercontract/`, `internal/workspace/`, `internal/brcli/`.
- **Type promotions:** `core.HandlerRef`, `core.EventType`, `core.ErrorCategory`, `core.DivergenceCorroboration`, `core.IdempotencyClass` consistency, `Registry` interface + `ControlPoint` + `PolicyDocument` + `PolicyExprEvaluator` (cost-ceiling).
- **Spec corrections:** ON-013c × §7.1 reconciled, §7.2 8-step drain enumerated, RC-015 AR-005 tag fix, RC-015 invalid Axes values, RC-015 non-canonical role names, BI-025a/b/c invalid `idempotency=safe` → `idempotent`, hk-hqwn.67/.68 (§8 Axes annotations + uncited-event citations).
- **Process:** `.claude/implementer-protocol.md` now mandates "close own beads", "claim until 250 k", "don't ask questions back".

# Current lane

**The dispatchable bead surface is now structurally thin.** ~5–9 non-zs0/non-epic beads remain ready. Most of the remaining ~33 ready beads are `hk-zs0.*` cognition / architecture spec drafts — these need user-driven design decisions, not implementer dispatch.

Next action depends on the user:

- **If user lands cognition decisions** (zs0.* beads or new beads ingested from the unloaded-bootstrap-IDs list) → orchestrator dispatches a fresh stream against the new surface.
- **If user wants to continue draining the thin surface as-is** → orchestrator dispatches 1–3 implementers across the remaining `hk-sx9r.*`, `hk-63oh.71/72` (currently blocked on `hk-hqwn.59.50/51`), epic-tracked sub-beads.
- **If user wants to focus on the meta-process** → continue elaborating `docs/orchestration-learnings.md`, promote learnings to implementer-protocol or directives, draft the kerf works that turn `product-input`-tagged learnings into harmonik features.

# Open follow-up

- **154 unloaded bootstrap IDs** (HC=46, BI=20, SH=54+) — surfaced by hk-kle6.2 audit (prior session); not yet ingested. Optional Phase 1 corpus-management task; expanding scope:bootstrap surface would unlock additional dispatches.
- **L-009 spec-discrepancy detection** — open: should "fixture-first spec review" be a process gate?
- **Learnings log entries L-004, L-005, L-006** — `process-improvement-pending`; require process-design choices before they can be promoted to protocol.

# Quick references

- `docs/orchestration-learnings.md` — friction log; read on every resume.
- `.claude/implementer-protocol.md` — implementer rules (updated 2026-05-10).
- `br ready --limit 0` — full dispatchable queue.
- `STATUS.md` — high-level project state (may lag the handoff).
- `docs/decompose-to-tasks/bootstrap-subset.md` — 345-bead INCLUDE set.
- `docs/foundation/corpus-label-reconciliation-2026-05-09.md` — kle6.2 audit.
