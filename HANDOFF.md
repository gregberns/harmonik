<!-- PP-TRIAL:v2 2026-05-12 main — v35. Hook-bridge build COMPLETE on main. 10 beads closed this session under hk-w5vra umbrella (.1 rename, .2 claude-twin, .3 hook-relay, .4 settings.json, .5 handler responsibilities, .6 daemon session_id, .8/.9/.10 spec amendments, .11 daemon dedup). Only .7 (real-claude re-smoke) remains. Main is 30+ commits ahead of origin; NOT pushed. -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read it on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`.

STREAM-NOT-WAVES (HARD RULE). The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. **On every implementer-completion notification, do exactly two things, in order:**

  1. Merge the returning implementer (rebase → ff-merge OR cherry-pick fallback → worktree teardown → bead-close-if-needed).
  2. Inspect dispatchable depth (`br ready --label next-init --limit 0` minus in-flight claims). If depth ≥ 1, spawn ONE replacement. If depth = 0, note "queue draining" and stop spawning.

Per-return acknowledgment is ≤2 lines ("merged X, dispatched Y" OR "merged X, queue draining"). Full session summary lives at `/session-handoff` time, not inline.

CANONICAL TRIAGE VIEW (HARD RULE). **`br ready --label next-init`** is the only queue the orchestrator dispatches from. The `next-init` label is rotated per-initiative — currently tagged on the `claude-adapter-real` initiative (the hk-w5vra.* family). When the current initiative closes, retire the label or rotate it to the next initiative's beads. Per the bv caveat: `bv --robot-triage` does not respect `post-mvh` or `status=deferred`; use `bv --robot-triage --label next-init` as the workaround.

ISOLATED-WORKTREE STALE-BASE BUG (HARD RULE — NEW in v35).
The Agent SDK's `isolation: "worktree"` mode has been cutting worktrees from commit `ecbe43e` (an old base) rather than current main this session. EVERY agent dispatched with `isolation: "worktree"` should be instructed in its brief to:

    cd <your worktree path>
    git fetch origin
    git rebase main

BEFORE reading any spec or code. Verify base via `git log --oneline -5`. Without this, the worktree lacks recent commits, and either: (a) the agent's commit shows whole-file creation on subsequent diffs, OR (b) the agent modifies files that were renamed on main (e.g., `cmd/harmonik-twin-claude/main.go` was renamed to `cmd/harmonik-twin-generic/main.go` by `.1`). Symptom of (b): rebase fails with file-location conflicts; agent's "modified" file actually targets the renamed-away version.

The `.5` and `.11` dispatches this session included the rebase instruction and ff-merged cleanly. Earlier dispatches (`.2`, `.8`, `.9`, `.10`) didn't and required manual diff extraction. **Always include this in implementer briefs.**

TRUST `br ready` BUT VERIFY (HARD RULE — three checks).
`br ready` is NOT authoritative for "the corpus is drained." Three orthogonal filters can hide dispatchable work:

  1. **Stale `blocked_issues_cache` (L-011).** Cross-check `br stats` Open vs Ready — if Open ≫ Ready, suspect dep-model gridlock. Recovery: `br doctor --repair`.
  2. **Parent-child gridlock (L-011).** When you `br create --parent <id>`, fresh parent-child deps block the child even when parent stays open as umbrella epic. **Run after every batch of `br create --parent`:**
       sqlite3 .beads/beads.db "UPDATE dependencies SET type='related' WHERE type='parent-child'; DELETE FROM blocked_issues_cache;"
       br doctor --repair
  3. **Stale `defer_until` (L-017).** Clear via `br update <id> --defer ""`.

DON'T ASK — EXECUTE.
On `/session-resume` with no hard blocker, EXECUTE — don't close the say-back with an A/B question (user's standing directive; memory `feedback_resume_continue_directive`). Sub-agents inherit via `.claude/implementer-protocol.md`. Orchestrator on genuine ambiguity: decide and document.

IMPLEMENTER LIFECYCLE — ENFORCED IN PROTOCOL.
`.claude/implementer-protocol.md` (updated 2026-05-10) is authoritative. Key rules: (a) implementer CLOSES OWN BEADS via `br close` after each commit, (b) implementer DOES THE BEADS NAMED IN ITS BRIEF AND EXITS — no free-claiming, (c) implementer DOES NOT ASK questions back.

DISPATCH SHAPE.
- Implementers: `model=sonnet`, `effort=high`, `isolation=worktree`, `run_in_background=true`. **REBASE FIRST per the hard rule above.**
- Reviewers: `model=sonnet`, `effort=high`, no isolation.
- Briefs ≤15 lines: see brief-template appendix in `.claude/implementer-protocol.md`. **Do NOT paraphrase the bead body.** Implementer fetches via `br show`.

PRE-FLIGHT (orchestrator, ≤3 reads per dispatch).
- Bead body via `br show <id> --format json`.
- The cited spec section or roadmap row.
- ONE canonical sibling for pattern conventions.

BEAD PICKING — POST-AUDIT SCOPE.
- **Canonical triage view: `br ready --label next-init`.**
- Same-package-different-file = parallel-safe.
- Same-file conflict → serialize via per-bead dispatch.

STANDING CONVENTIONS (full version: `.claude/implementer-protocol.md`).
- Bead body wins over docs; spec wins over bead body for normative content.
- Typed-alias deferral: real follow-up bead via `br create`, ID substituted into godoc BEFORE commit.
- gofmt-clean, lint clean, tests pass before commit.
- Worktree discipline: implementer commits in their worktree, never main.

REVIEWER TIER DISCIPLINE.
- MEDIUM = defect against THIS bead's acceptance criteria.
- Cross-cutting / future-bead / spec-doc concerns = MINOR or follow-up.

INLINE-AMEND CEILING.
Trivial single-line text fix, literal one-line code fix, mechanical multi-line refactor → orchestrator inline-amends, no fix-agent. Above ~3 mechanical edits in 1 file → spawn fix-agent on existing worktree.

CWD DISCIPLINE (NEW v35).
Use `git -C /Users/gb/github/harmonik` for ALL git ops AND read absolute paths to avoid bash-cwd drift inside worktrees. When a worktree is removed under your shell's cwd, the shell stays in that removed dir. ALWAYS prefix bash commands with absolute paths.

MERGE DANCE — RUN FROM `/Users/gb/github/harmonik`.

    cd /Users/gb/github/harmonik
    for id in <agent-id-1> <agent-id-2>; do
      WTPATH="/Users/gb/github/harmonik/.claude/worktrees/agent-$id"
      BRANCH="worktree-agent-$id"
      git -C "$WTPATH" rebase main
      git -C /Users/gb/github/harmonik merge --ff-only "$BRANCH"
      git -C /Users/gb/github/harmonik worktree remove --force --force "$WTPATH"
      git -C /Users/gb/github/harmonik branch -d "$BRANCH"
    done

FALLBACK — cherry-pick when worktree dir is gone. To merge a leftover branch whose worktree was already removed, use `git -C /Users/gb/github/harmonik cherry-pick <sha>` against the branch tip.

REBASE-SKIP for duplicate-bead commits. When a long-running OLD-protocol implementer's branch carries a commit for a bead ALREADY closed by a newer-protocol dispatch, `git rebase main` will hit add/add or content conflicts. Use `git rebase --skip`. Cross-package signature mismatches DO NOT surface as text conflicts; always run `go vet ./...` after the last merge of a session.

`br close` failures from `blocks` deps → flip to `related`:
    br dep remove <id> <other> ; br dep add <id> <other> --type related ; br close <id> -r "..."

`br update -d` does NOT exist — use `--description` or `--body`. `--notes` adds without overwriting. `br update --defer ""` clears `defer_until` (see L-017). `br create` flags: `-p` priority, `--labels "a,b,c"`, `--parent <id>`.

CONTEXT BUDGET (orchestrator). ~700 k effective. At ~500 k, finish in-flight stream cleanly, write fresh HANDOFF, stop. v34→v35 used ~60% of the budget — heavy session.

<!-- END DIRECTIVES -->

# Where we are (v35, 2026-05-12)

Main at `f285d2b`. **Nothing in flight.** Stream is drained except `.7` (re-smoke). Working tree on `spec/claude-hook-bridge` branch with user's pre-session WIP (.gitignore, CLAUDE.md, research/planning-protocols/STATUS.md, untracked .beads/) preserved.

## The big win this session

**The hook-bridge is feature-complete.** Bead `hk-w5vra` is the umbrella for "claude doesn't speak harmonik's NDJSON protocol" — the protocol-translation gap that caused the original RED smoke. The user authored a 456-line spec for the bridge mid-session (`specs/claude-hook-bridge.md`, CHB-001..027 across v0.1→v0.4), and 10 beads landed under it:

| Bead | Commit | What |
|---|---|---|
| `hk-keb6o` | `0534c0b` (pre-session) | Wire LaunchSpec JSON to subprocess stdin (HC-005) |
| `.1` | `e84da74` | Rename old generic-but-named-claude twin to `harmonik-twin-generic` |
| `.2` | `12ed9db` | Build new `harmonik-twin-claude` mirroring real-claude lifecycle (CHB-021/022) |
| `.3` | `f44f8fe` | Implement `harmonik hook-relay <event-kind>` subcommand (CHB-010..017) |
| `.4` | `fb1bb8c` | Materialize `.claude/settings.json` in workspace (CHB-001..005) |
| `.5` | `ea4464f` | Claude-code handler-process responsibilities (CHB-006..009, 018..020, 024) |
| `.6` | `1b88110` | Daemon persists `claude_session_id` to `Run.context` before claude exec (CHB-023) |
| `.8` | `b38c441` | Spec amendment: CHB-025 Stop-hook dedup gate (daemon last-wins) |
| `.9` | `405a517` | Spec amendment: CHB-027 orphan-connection silent-drop + §8 `bridge_partial_write` entry |
| `.10` | `feb6494` | Spec amendment: CHB-026 concurrent-socket serialization rule (per-conn FIFO, across unordered) |
| `.11` | `f285d2b` | Daemon side: CHB-025 last-received-wins dedup for `outcome_emitted` |

Plus a spec-review pass (`df06fb9`) that surfaced the 3 spec-amendment beads (.8/.9/.10).

# TOP PRIORITY NEXT SESSION — `hk-w5vra.7` re-smoke

Same procedure as the original (RED) smoke `hk-1n0cw.2`: isolated scratch beads dir, one disposable bead, run hk against the real `claude` CLI. With the bridge now in place this should be GREEN. Procedure documented in the bead body — `br show hk-w5vra.7`.

**OPEN THIS SESSION IN tmux** (user's standing reminder). The user wants to attach to the tmux panes of the Claude subprocesses hk spawns. Running the orchestrator session inside tmux is the prerequisite. Figure out the inspect-the-subprocess-pane workflow early.

If the smoke surfaces gaps: file follow-up beads under `hk-w5vra` and iterate. If GREEN: close `hk-w5vra` and `hk-1n0cw` epics; remove the `next-init` label from any remaining beads or rotate to a new initiative.

# After re-smoke is GREEN: DOT slow-roll

User's plan: slow-roll DOT (the third workflow dispatch mode, currently an empty spec slot per commit `caf4b57`).

1. Run ONE DOT task end-to-end; review logs, output, full result.
2. If clean, run TWO DOT tasks IN SERIES; review.
3. If clean, run TWO DOT tasks IN PARALLEL; review.
4. Then scale further.

**DOT experiments to try once basic rollout works:**
- Multi-agent arrangements — including one node being the ralph (review-loop) loop.
- Add deterministic steps to a DOT graph — e.g., a node that checks whether the implementer's branch is off current `main`, and if not, sends a message back to the implementer agent to update/rebase.

Before any DOT implementation work: open question whether to kerf-plan it (`kerf new dot-workflow-mode --jig spec`) or write a roadmap-first (like `POST_MVH_PARALLELISM_ROADMAP.md`). User hasn't decided.

# Carry-forward reminders for the NEXT session

**tmux integration** — test the tmux implementation hk already has.

**Daemonization** — submit-work-and-walk-away model: detached process, pidfile, socket, JSON-RPC operator control per locked decision 2026-05-08.

**Push to origin.** Main is 30+ commits ahead of `origin/main` and NOT pushed. User decides when to push; orchestrator should NOT auto-push.

# Tech debt filed this session

- **harmonik-twin-claude duplicates ~80% of harmonik-twin-generic** (version.go, scriptdriver.go, wire.go, wire_test.go, wire_ndjson_test.go, scriptdriver_test.go, silentHang_hc026_test.go, crashRecov_hc024_test.go, main_test.go). Right move = refactor to an internal package both binaries import. NOT urgent; the two binaries diverge in `main.go` (twin-claude has --scenario), `scenarios.go` (new), `e2e_chb021_test.go` (new). File a follow-up bead when the duplication starts to bite.
- **Isolated-worktree stale-base bug.** The Agent SDK cuts worktrees from `ecbe43e` rather than current main; cause unknown. Mitigation = rebase-first in brief (now a hard rule). Probably worth a bug report to the SDK if reproducible.

# Pre-existing flakes — still known, still not blocking

- `TestT4_ReopenThenRedispatch` — git-I/O contention; passes in isolation.
- `TestWorkLoop_FailedHandlerReopensBead` — same root cause.
- `TestT5_RedactionHC031ByFieldName` in `internal/t5probe`.
- `TestSession_Outcome_StderrTail` / `TestSession_Outcome_NonZeroExit` in `internal/handler` — confirmed pre-existing on main during `hk-keb6o` work.
- `internal/specaudit` failures — pre-existing on main (multiple beads this session confirmed).

# Files to open first (next session)

1. **This file (HANDOFF.md)** — you're reading it.
2. `docs/orchestration-learnings.md` — friction log; consider appending the worktree-stale-base bug.
3. `specs/claude-hook-bridge.md` (v0.4) — the spec the implementation realizes.
4. `docs/dogfood-smoke-trace.md` and `docs/dogfood-smoke-run-2026-05-12.md` — the RED baseline.
5. `internal/handler/claudehandler_chb006_024.go` — `.5`'s handler-process implementation; the new behavior the smoke will exercise.
6. `cmd/harmonik-twin-claude/` — the new claude twin.
7. `br show hk-w5vra.7` — the re-smoke bead body.

# Blocking question for the user

None. Re-smoke `.7` is the dispatchable next step; if GREEN, DOT slow-roll. User wanted to be hands-on for the re-smoke (per next-init guidance), so don't auto-dispatch — confirm at session start.
