<!-- PP-TRIAL:v2 2026-05-10 main — v24, 93 commits, ~197 beads closed, Open 353→156, L-015 protocol fix landed (with addendum) -->

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

REBASE-SKIP for duplicate-bead commits (added v24 from L-015 mass merge). When a long-running OLD-protocol implementer's branch carries a commit for a bead that was ALREADY closed by a newer-protocol dispatch in the same session, `git rebase main` will hit add/add or content conflicts on the same file. Use `git rebase --skip` to drop the duplicate — the newer-protocol dispatch's version is canonical. Two duplicates auto-resolved cleanly this session (i3152's hk-hqwn.59.75/.59.28). Cross-package signature mismatches DO NOT surface as text conflicts (e.g. mup11's `internal/lifecycle/orphansweep.go` calling `workspace.SweepStaleLeaseLocks(..., nil)` after ml3rw changed the param type to `WorktreeRootConfig`); always run `go build ./...` after the last merge of a session and inline-fix.

**WORKTREE TEARDOWN DOES NOT KILL THE AGENT (added v24 from L-015 addendum).** Calling `git worktree remove --force --force` on an active sub-agent's worktree does NOT terminate the agent process. The agent keeps running, can recreate the worktree (its bash sessions still hold its old CWD or `cd` back), and continues making bash calls — `br close` writes hit the SQLite ledger directly; `git commit` calls land on the recreated branch. Both sx5860 and mup11 kept running ~90 min past my merge-and-teardown this session, accumulating ~55 free-claim bead closures (no commits, just `br close`) and 5 fresh commits on a resurrected mup11 worktree. The agent platform has no kill signal; you must wait for the agent to return naturally OR finish the session and accept that its late writes land in the next session's reconciliation. **At session end, before writing HANDOFF, check `br stats` Open count and `git worktree list` ONE MORE TIME** — if Open dropped further or worktrees reappeared, an OLD-protocol agent is still active and you need to merge its tail before the handoff is accurate.

`br close` failures from `blocks` deps → flip to `related`:
    br dep remove <id> <other> ; br dep add <id> <other> --type related ; br close <id> -r "..."

`br update -d` does NOT exist — use `--description` or `--body`. `--notes` adds without overwriting. `br create` flags: `-p` priority, `--labels "a,b,c"`, `--parent <id>`.

REBASE-CONFLICT ON `go.mod` — DO NOT USE `git reset --soft main`. Use `git rebase -i main` to drop the offending hunk, or `--strategy-option theirs` for go.mod/go.sum specifically. Verify `git -C "$WT" diff main -- go.mod go.sum` is empty before continuing.

CONTEXT BUDGET (orchestrator). ~700 k effective on this 1M-context model. At ~500 k, finish in-flight stream cleanly, write fresh HANDOFF, stop.

<!-- END DIRECTIVES -->

# State

Main at `d727453`, working tree clean, NOT pushed. Full `go test ./internal/...` green (verified post-final-merge). No active worktrees, no in-flight implementers. **Open=156, Closed=815, Ready=11.**

(Initial handoff at `220a16e` recorded Open=210 / Ready=23 — that was incomplete: sx5860 and mup11 kept running for ~90 min after I had supposedly merged-and-torn-down their worktrees, accumulating ~55 more bead closures and 5 fresh commits on a resurrected mup11 worktree. Final reconciliation merge committed those — see directives §"WORKTREE TEARDOWN DOES NOT KILL THE AGENT".)

# What changed this session

**88 commits, ~143 beads closed.** Two parallel work streams ran:

1. **Stream of single-bead implementers under the new tightened protocol** (post-L-015): mwo4 (9-bead workspace cluster), hqwn345/hqwn78/hqwn5975/hqwn2628 (typed aliases + JSONL writer + §8 event rows), ml3rw (CP-037 typed surface), nptq0 (ErrorCategory extension), i0tw12/13/16 (scenario harness SH-012/013/015a), hqwn1718/hqwn13 (consumer-class sensors), rec1541 (RC-011/029 sensors), mwo27/mwo22/mwo47 (workspace SUBSUMED + WM-035), pvcs (meta-decomposition closure). Each: single bead, exit, no free-claim. Zero collisions on these.

2. **Three long-running OLD-protocol implementers** (i3152, mup11, sx5860) free-claimed extensively across spec boundaries. Returned in last hour with 14 / 17 / 27 commits respectively. Rebased onto main; 3 commits skipped as L-015 duplicates (hk-8mwo.45 WM-033 sweep, hk-hqwn.59.75 consumer_failed, hk-hqwn.59.28 skills_provisioned — all already closed by newer-protocol siblings). All other commits landed clean. Post-merge cleanup commit (`ac4b86b`) fixed two echoes: `internal/lifecycle/orphansweep.go` was passing `nil` to a workspace function whose signature ml3rw had changed; and sx5860's `rc011_*` / `rc029_*` specaudit fixture-helper symbols collided with rec1541's earlier-committed canonical names — deleted sx5860's redundant files.

3. **L-015 protocol fix** (commit `b74b1d4`) is the headline outcome. The "Continue claiming until 250k" HARD RULE in `.claude/implementer-protocol.md` was a main-thread budget rule mis-copied into the implementer surface; sub-agents read it and free-claimed past their assigned scope. User flagged it after the second collision (sx5860 jumped from `spec:operator-nfr` to `spec:event-model` to grab `hk-hqwn.8` while a sibling was simultaneously dispatched on the same bead). Replaced the rule with "Do your assigned bead(s) and exit"; updated HANDOFF directives' IMPLEMENTER LIFECYCLE block; added L-015 entry to `docs/orchestration-learnings.md`. Validated empirically: every new-protocol dispatch this session exited cleanly without collision.

# Lingering / next session

**Verify ready=11 is genuine, then dispatch — but the ceiling may already be the cognition gate.** Open(156) is still ≫ Ready(11), so run the blocker-distribution recipe (directives §"TRUST `br ready` BUT VERIFY"). However, the runaway sx5860 cascade ended its session reporting that "all 11 ready items are epics" and that the remaining 156 are blocked behind three chains: (a) `hk-zs0.*` DEFERRED architecture beads (.38/.39/.41/.52 — deferred until 2027-01-01), (b) `hk-a8bg.*` Control Points subtree (Gates/Hooks/Guards/Budgets/Roles — needs zs0 to undefer), (c) `hk-sx9r.22/23/24` upgrade contract (needs hk-a8bg.39 → CP). If that's correct, this session's bootstrap-corpus drain is complete and the next move is the `harmonik-foundation` kerf work to unblock the zs0 cognition decisions. Verify the chain claim before committing to that pivot.

**Open follow-up beads** filed this session: `hk-tyjfi` (typed `SkillVersion` alias for `skills_provisioned` `version?` field — already closed by mup11 in the merge wave), and an inline `TODO(hk-placeholder)` on `RateLimitSource` in `agentevents_hqwn59.go` that hqwn2628 left without a real follow-up bead — file one at session start: `br create -p high --labels "kind:schema,spec:event-model" -t "RateLimitSource typed enum or vocabulary"`.

**Watch for**: new L-015-style cross-spec free-claims should NOT happen anymore, but if a returning implementer has commits for beads not in its brief, that's a regression — file an L-016 entry. The `mup11` orphan-sweep production code (`internal/lifecycle/orphansweep.go`) was authored against the pre-ml3rw API surface and only got a one-line patch to compile; a brief diff-review of that file vs the latest workspace package contracts is worth doing in a fresh dispatch.

**Push.** Main is 93 commits ahead of `origin/main` (`3bcc684..d727453`). Run `git push origin main` at session start (or end-of-this-session if user wants).

# Quick references

- `docs/orchestration-learnings.md` — L-001 through L-015; read on resume.
- `.claude/implementer-protocol.md` — revised per L-015; brief template in appendix.
- `STATUS.md` — high-level project state.
- `git log 3bcc684..HEAD --oneline` — this session's 88 commits.
