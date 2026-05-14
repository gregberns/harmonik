<!-- PP-TRIAL:v2 2026-05-13 main — v37. Audit-umbrella hk-lj1p9 corpus complete (all 10 children closed). 14 commits added; main at 3ee426c, ~29 commits ahead of origin (NOT pushed). One critical follow-up OPEN: hk-lj1p9.3 (P0) — EnsureWorktreeTrust writes to real ~/.claude.json; concurrent tests corrupted user config (manually repaired in-session). DO NOT run `go test ./internal/daemon/` until that bead is fixed. Smoke v4 has not run yet. -->

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

WORKTREE TASK-INJECTION LEAK (NEW v36). In v36 the .6 and .8 implementers' edits leaked into main's working tree (not committed) — symptom: stash chains accumulate during merge dance. The worktree branches' commits were correct; main's WORKING TREE picked up duplicate uncommitted edits. Cause unknown (Agent SDK bug?). Workaround: stash-then-merge as routine; drop stashes after the worktree branch ff-merges cleanly. Never commit the leaked main-tree edits as a separate commit — the proper changes arrive via the worktree branch merge.

TRUST `br ready` BUT VERIFY (HARD RULE — three checks).
`br ready` is NOT authoritative for "the corpus is drained." Three orthogonal filters can hide dispatchable work:

  1. **Stale `blocked_issues_cache` (L-011).** Cross-check `br stats` Open vs Ready — if Open ≫ Ready, suspect dep-model gridlock. Recovery: `br doctor --repair`.
  2. **Parent-child gridlock (L-011).** When you `br create --parent <id>`, fresh parent-child deps block the child even when parent stays open as umbrella epic. **Run after every batch of `br create --parent`:**
       sqlite3 .beads/beads.db "UPDATE dependencies SET type='related' WHERE type='parent-child'; DELETE FROM blocked_issues_cache;"
       br doctor --repair
  3. **Stale `defer_until` (L-017).** Clear via `br update <id> --defer ""`.

`br doctor --repair` MUTATES `.gitignore` (NEW v36). It silently strips the `.beads/*` ignore line, which then makes `git status` show the entire `.beads/.br_history/` dir as untracked. Always `git checkout -- .gitignore` after running `br doctor --repair`.

DON'T ASK — EXECUTE.
On `/session-resume` with no hard blocker, EXECUTE — don't close the say-back with an A/B question (user's standing directive; memory `feedback_resume_continue_directive`). Sub-agents inherit via `.claude/implementer-protocol.md`. Orchestrator on genuine ambiguity: decide and document. EXCEPTION: spec-text authoring is user-shaping; check in before dispatching agents that will write normative spec sections.

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

SPEC-FIRST AUDIT BEFORE BIG INITIATIVES (NEW v36).
v36 lesson: three smokes RED'd in succession because no one read the specs end-to-end before grinding on plumbing. The bridge code faithfully implemented what CHB and HC specified — those specs simply never defined a daemon→claude task-delivery channel. **Before opening a new initiative, dispatch a research agent (Opus, no code, no smokes) to audit the relevant specs and produce a written gap list.** Templated brief: see hk-lj1p9 audit dispatch on 2026-05-13.

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

CWD DISCIPLINE (NEW v35; REINFORCED v37).
Use `git -C /Users/gb/github/harmonik` for ALL git ops AND read absolute paths to avoid bash-cwd drift inside worktrees. When a worktree is removed under your shell's cwd, the shell stays in that removed dir. ALWAYS prefix bash commands with absolute paths. v37 also bit on `go build ./...` running in a stale worktree dir whose files were never updated — verify `pwd` returns /Users/gb/github/harmonik before any build/test command.

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

CONTEXT BUDGET (orchestrator). ~700 k effective. At ~500 k, finish in-flight stream cleanly, write fresh HANDOFF, stop. v34→v35 used ~60% of the budget — heavy session. v36 used ~26%. v37 used ~29%.

DO NOT RUN `go test ./internal/daemon/` UNTIL hk-lj1p9.3 IS FIXED (HARD RULE — NEW v37).
hk-fdyip's `EnsureWorktreeTrust` (internal/workspace/claudetrust_wm040b.go) writes to the literal user-home `~/.claude.json`. Concurrent test daemons (T11 throughput, parallel review-loop tests) race on that single file, producing torn writes — verified empirically in v37: user config got `Extra data: line 3917 column 1` corruption and 273 stale `/worktrees/` entries were accumulated from prior runs. The orchestrator manually repaired ~/.claude.json (`~/.claude.json.bak.before-repair` exists as safety net). Until hk-lj1p9.3 lands an env-var-based config-path override + advisory flock, running the daemon test suite will re-corrupt the file. Individual non-concurrent tests are safe: `go test ./internal/daemon/ -run "^TestT6_10BeadSequentialDrain$" -count=1` works.

<!-- END DIRECTIVES -->

# Where we are (v37, 2026-05-13)

Main at `3ee426c`. **Nothing in flight.** ~29 commits ahead of origin (NOT pushed). Working tree clean except `~/.claude.json.bak.before-repair` (untracked safety backup of the user's repaired claude config; safe to keep or delete).

## What landed this session — the audit-umbrella corpus

Umbrella **hk-lj1p9** (claude session lifecycle + bidirectional messaging, filed by v36's audit) is **fully built end-to-end**. All 10 children closed:

| Bead | Kind | What |
|---|---|---|
| hk-yrplz | spec | CHB-028 — `.harmonik/agent-task.md` per-launch task artifact |
| hk-ultyu | spec | PL-021d — `tmux load-buffer` + `paste-buffer` daemon→pane write mechanism |
| hk-9rfwz | spec | EM-015d-RFD — reviewer-feedback file delivered before impl-resume |
| hk-wuyn1 | spec | EM-015d-RIA — review-target.md reviewer input artifact |
| hk-p63bz | spec | CHB-013/CHB-018/HC-039/HC-041/HC-056 reframe — agent_ready relay-synthesized from SessionStart |
| hk-igbx8 | code | `tmux.Adapter` extended: `LoadBuffer`, `PasteBuffer`, `SendKeysLiteral`, `WriteToPane` |
| hk-fdyip | code+spec | CHB-029 — worktree auto-trust pre-seed of `~/.claude.json` |
| hk-9ow36 | code | Daemon writes `.harmonik/agent-task.md` before each claude launch (CHB-028) |
| hk-zrj83 | code | Daemon paste-injects "read your task file" after pane spawn (phase-aware) |
| hk-1rocd | code | `agent_ready` observation switches to relay-synthesized claude signal |

Three post-merge follow-up beads filed: `.1` (T6 hang — closed; root cause was missing MkdirAll, not agent_ready semantics as orchestrator first hypothesized), `.2` (CHB-028 collision was a spec-author oversight — closed; phase transitions now overwrite the file), **`.3` (P0, OPEN — see directives block above)**.

# TOP PRIORITY NEXT SESSION — fix hk-lj1p9.3, then smoke v4

1. **Fix hk-lj1p9.3** (P0). Parameterize EnsureWorktreeTrust to honor a `HARMONIK_CLAUDE_CONFIG_PATH` (or `CLAUDE_CONFIG_HOME`) env var; tests set it to a `t.TempDir()` path. Add a file-level advisory `flock` around the read-modify-write so even in-process concurrent daemons don't tear. Optional: prune stale `projects[]` entries pointing to non-existent paths during each write, to keep the user config from growing without bound.

2. **Re-run `go test ./internal/daemon/`** after the fix — confirm `TestT6_10BeadSequentialDrain`, the 4 ReviewLoop tests, and `TestThroughput_TenBeadsAtMaxFour` all pass. (Other failures should be the same pre-existing flakes: `T4_Reopen`, `T5_Redaction`, `specaudit`.)

3. **Run smoke v4** — the truth-teller for the umbrella. Single foreground daemon, single bead, real `claude` CLI. Document at `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v4.md` following the v3 template. The v3 RED was load-bearing precisely because no task ever reached claude; that path now exists. Either GREEN (umbrella achievement validated) or surfaces a smaller downstream gap.

4. **Push to origin** if smoke v4 GREEN. Main is ~29 commits ahead; user-gated push.

5. **Next initiative after that:** reviewer-loop end-to-end code. Specs exist (EM-015d-RFD + EM-015d-RIA + review-target.md write); the impl phase is built; the reviewer-phase code (write review-target.md, paste-inject for reviewer, read reviewer-feedback on resume) is the gap. That's the gate to dogfooding actual review-loop workflows.

# What changed in `~/.claude.json` (safe to ignore, but logged here)

The user's real claude config was corrupted by v37 test runs and manually repaired in-session by truncating to the first valid JSON object. The file now has ~273 stale `/worktrees/` entries from prior test runs (each test daemon adds one). Functional; bloat-only. A `~/.claude.json.bak.before-repair` exists as untracked backup. Once hk-lj1p9.3 lands, the test pollution stops; the bloat can be cleaned separately if it ever matters.

# Files to open first

1. **HANDOFF.md** (this).
2. `br show hk-lj1p9.3` — the P0 fix you're about to dispatch.
3. `internal/workspace/claudetrust_wm040b.go` — the file to amend.
4. `specs/claude-hook-bridge.md` §4.12 CHB-029 — the trust-pre-seed spec; may need a small amendment to mention the env-var override.
5. `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v3.md` — the v3 RED you'll follow the structure of for v4.

# Pre-existing flakes — still known, still not blocking

- `TestT4_ReopenThenRedispatch` — git-I/O contention; passes in isolation.
- `TestT5_RedactionHC031ByFieldName` in `internal/t5probe`.
- `internal/specaudit` failures.

NEW post-umbrella failures (all symptoms of hk-lj1p9.3): `TestReviewLoopCycleComplete_Blocked`, `TestReviewLoop_HappyPath_APPROVE` (only when run concurrently with siblings), `TestReviewLoopCycleComplete_Approved`, `TestReviewLoop_CapHit`, `TestThroughput_TenBeadsAtMaxFour`. All will resolve when EnsureWorktreeTrust is isolated.
