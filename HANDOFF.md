<!-- PP-TRIAL:v2 2026-05-10 main — first big stream session; 43 commits, ~80 beads closed, Open 487→353, Ready 487→26 -->

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
`.claude/implementer-protocol.md` (updated 2026-05-10, revised again later same day per L-015) is now authoritative for implementer behavior. Key rules: (a) implementer CLOSES OWN BEADS via `br close` after each commit (clarified — agent owns), (b) implementer DOES THE BEADS NAMED IN ITS BRIEF AND EXITS — no free-claiming from `br ready`; orchestrator owns refill (this REPLACES the prior "continue claiming until 250k" rule, which was for the main thread, not implementers — see L-015), (c) implementer DOES NOT ASK questions back. The orchestrator brief should NOT re-state these — point to the protocol and trust it. **Brief template**: appendix of `.claude/implementer-protocol.md`; orchestrator briefs are now parameter-fills against it (worktree path/branch + scope + ONE sibling pointer + optional deferral shape — ≤15 lines). Briefs MUST NOT include "after close, continue claiming X" lines — the implementer exits when SCOPE is done.

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

Main at `0ce48e4`, pushed clean. `go test ./internal/core/... ./internal/specaudit/...` green. No active worktrees. No in-flight implementers. Open=353, Blocked=327 (per `br stats`), Ready=26.

# This session — what changed

First substantive stream session. Dispatched ~13 implementer waves over the course of the session, ran STREAM-NOT-WAVES merge-and-refill on every return, landed **43 commits** with ~80 bead closures across 7 spec areas. Highlights:

1. **Spec-corpus sensors** in `internal/specaudit/` — SH-INV-001/002/003/005, EV-002c/008/009/012/014b/014d/016/017/034/036, RC categories §8.2–§8.11a, HC-007/009/010/015/033/039/052, ON-023, plus the §8 taxonomy umbrella and corpus tests for the §10.1 conformance scenarios. Pattern is now well-established: per-req file, body-window scan, `Tags: mechanism` check.

2. **§8 event-row payload structs** — three batched waves (§8.1+§8.2 / §8.3–§8.6 / §8.7+§8.8) landed all ~79 row beads (`hk-hqwn.59.*`) plus `eventreg_hqwn59.go` startup-time registry per EV-034 plus the `core.EventType` enum. Includes the deliberately-merged conflict in `eventreg_hqwn59.go init()` between sibling implementers — additive resolution kept all six `register*Events()` calls.

3. **Handler-contract implementation lane** — wire-protocol structs (HC-007/009/010/039), `watcher_hc011.go` with NDJSON read-loop + recover barrier, `AdapterRegistry`, `RunLock` mutex discipline, `schemachecker_hc033`. Then the typed-alias-deferral closeout: `EventEmitter` narrow interface substituted for the `EventPublisher` placeholder in the watcher (avoids 6-method coupling and an import cycle).

4. **Workspace lease-lock** — `LeaseLockPath`, `WriteLeaseLockAtomic`, `ReleaseLeaseLock`, `WriteLeaseReleasedMarker`, `WorkspaceLocalEventsPath` per WM-013a/b. Production functions wired into the existing WM-031/034/038a tests.

5. **Lifecycle PL-005 startup-sequence sensor** — full 12-step ordering harness (steps 0–9 + 3a + 8a) including Cat 0 halt path and step-8a hash-mismatch refusal.

6. **EventBus interface** in `internal/eventbus/eventbus.go` — 6-method `Emit/Subscribe/Seal/ReplayFrom/DeadLetterReplay/Drain` plus `TailTruncationCallback`, godoc-cited to specs/event-model.md §6.1. Interface-only (no impl yet).

7. **§10.1 conformance scenarios** — three smoke/regression scenarios authored (`scenarios/smoke/twin-launch-and-ready.yaml`, `scenarios/smoke/checkpoint-and-merge.yaml`, `scenarios/regression/twin-failure-classification.yaml`) plus the shared `scenarios/_workflows/smoke-one-node.dot` and a `conformancecorpus_test.go` corpus test. Bootstrap §1 acceptance test floor is now reachable.

8. **Reconciliation** — §8 category spec-corpus sensors for hk-63oh.63–.71 + .72 plus a shape-contract harness at `internal/core/reconciliationcategoryharness_rc003b_test.go`. Last implementer hit an API outage mid-dispatch; the 3 commits it had already made landed cleanly.

# Friction observed (candidate L-013 / L-014)

- **L-013 — Bead-claim race.** Two implementers (hqwn57 + hqwn11) both claimed and produced sensors for `hk-hqwn.11` (EV-008) because each had a "continue claiming `kind:req` in `spec:event-model`" rule. Two distinct test files landed with the same target req. Resolved by deleting the redundant later file in commit `b8e2d73`. **Mitigation pattern that worked**: when two scopes share a namespace, partition by section (e.g., hqwn59a §8.1+§8.2, hqwn59b §8.3–§8.6, hqwn59c §8.7+§8.8) — explicit, non-overlapping numeric ranges. **Mitigation pattern that didn't**: vague "continue claiming kind:req" — implementers picked the same beads.
- **L-014 — `--force` bead-close pattern.** Most `hk-hqwn.*` and `hk-i0tw.*` sensor implementers used `br close --force` because the bead's declared `blocks` deps were themselves OPEN sensor beads (structural, not real prereq). Pattern is sound — the sensor is the deliverable, the blocker is design-level not code-level — but worth surfacing: a sensor-to-sensor `blocks` edge is a structural smell. Possible automation: convert sensor-to-sensor blocks edges to `related` (similar to the L-011 parent-child fix, scoped narrower).
- **API outage during last dispatch.** Implementer made 3 commits before erroring out; bead-close was incomplete on its tail end. Pattern for resilience: after API errors, run `br stats` and check whether the bead was actually closed before retrying.
- **`git add -A` swept embedded worktrees + `.claire/`.** Mid-session dedupe commit accidentally staged `.claude/worktrees/agent-*/` (embedded git repos) and a stray `.claire/agent-a27b...` test file. Recovered via `git reset --soft HEAD~1` and selective re-stage. **Action item: add `.claude/worktrees/` and `.claire/` to `.gitignore`.**

# Current lane

Ready=26. Top blockers (from `br blocked` analysis):
- `hk-zs0.14` blocks 16 (DEFERRED to 2027-01-01)
- `hk-8i31.74` blocks 15 (LaunchSpec record — itself blocked on hk-b3f.* and hk-zs0.54)
- `hk-zs0.41` blocks 12 (DEFERRED)
- `hk-8i31.55` blocks 11 (HC-046/047 — blocked on .74)
- `hk-a8bg.21` blocks 10
- `hk-zs0.21` blocks 10 (DEFERRED)
- `hk-8mup.16` blocks 10
- `hk-8mwo.4` blocks 10

Three of the top eight are deferred zs0 cognition beads. The remaining gridlock is the `hk-8i31.74 → .55` chain (LaunchSpec record needed) plus `hk-a8bg.21`, `hk-8mup.16`, `hk-8mwo.4`. Probably need to verify whether ready=26 is the genuine prereq topology or another structural artifact.

**Next session priority order:**
1. **Verify ready=26 is genuine.** Run `br stats` Open vs Ready check, then `br blocked` distribution. If Open ≫ Ready and a single non-deferred blocker dominates, consider the L-014 sensor-to-sensor blocks-edge conversion.
2. **Pivot to top non-deferred blockers** if queue is genuinely thin: `hk-8i31.74` (LaunchSpec) is the highest-leverage; needs typed-alias deferral against hk-b3f.*. `hk-a8bg.*` (Control Points) and `hk-8mup.*` (Process Lifecycle) are the next two epics with cluster-blockers.
3. **If queue stays drained**, the `harmonik-foundation` kerf work is the venue for the cognition decisions that unblock the zs0.* deferred set.

# Open follow-up

- **L-013 / L-014 entries** — append to `docs/orchestration-learnings.md` next session.
- **`.gitignore` entries** — add `.claude/worktrees/` and `.claire/` to project `.gitignore`.
- **L-009 fixture-first spec review** — still open; not session-blocking.
- **Epic-progress tooling** — `br epic status` still broken post-L-011 conversion.
- **`harmonik-foundation` kerf work** — 16d idle on the bench; venue for the deferred zs0 cognition decisions.
- **3 typed-alias follow-up beads** created this session: hk-hqwn.71/.72/.73/.74/.75 (payload-field aliases for §8.4/§8.5/§8.6/§8.7/§8.8 events).

# Quick references

- `docs/orchestration-learnings.md` — friction log; read on every resume.
- `.claude/implementer-protocol.md` — implementer rules + brief-template appendix.
- `br ready --limit 0 | grep -v "\[epic\]"` — true dispatchable queue (skip epic containers).
- `br blocked --limit 0 --json | python3 -c "..."` — top-blocker distribution (recipe in directives).
- `STATUS.md` — high-level project state.
