# Dimension 1 — How harmonik drives Claude today (the implicit Harness interface)

> Research findings from parallel code-reading sub-agents, repo `/Users/gb/github/harmonik`.
> Every claim anchored to `file:line`. This is the contract a second harness must satisfy.

## Part A — Launch spec, session-id minting, tmux substrate

### A.1 The exact launch surface (argv)

argv is built as a string slice in `internal/daemon/claudelaunchspec.go:357-376`, in order:
1. Session selector: `--resume <uuid>` (implementer-resume phase) or `--session-id <uuid>` (all
   other phases) — `claudelaunchspec.go:358-362`.
2. `--model <alias>` when `rc.model != ""` — `:363-365`.
3. `--effort <level>` when `rc.effort != ""` (vocab `{low,medium,high,xhigh,max}`) — `:366-368`.
4. `--dangerously-skip-permissions` iff the workspace canonicalizes under the harmonik worktrees
   root (`isHarmonikManagedWorktree`, `:436-453`) — `:374-376`.

**No** `--print` / `--output-format` / `--remote-control` / `-p`: claude runs as a **fully
interactive REPL in a tmux pane**, not headless. argv is deny-list-checked by
`CheckForbiddenFlags` — `:379`. Binary is opaque (`rc.handlerBinary`, `:408`): `"claude"` or a twin.

### A.2 Session-id minting (caller-minted UUIDv7)

`MintClaudeSessionID` uses `uuid.NewV7()` — `internal/handler/claudehandler_chb006_024.go:147`.
Fresh for single / implementer-initial / reviewer; **reused** from the prior launch for
implementer-resume (`:120-131`). Reviewer must always mint fresh and fails-fast if handed a prior
id (`:138-144`). The id is passed both as the `--session-id`/`--resume` argv value AND as env
`HARMONIK_CLAUDE_SESSION_ID`.

### A.3 Environment (`ClaudeEnvVars`, `claudehandler_chb006_024.go:232-310`)

Starts from `BaseEnv`, then:
- **Strips** `HARMONIK_SECRET_*` (`:243-245`) and credential deny-list `ANTHROPIC_API_KEY` /
  `ANTHROPIC_AUTH_TOKEN` / `CLAUDE_CODE_OAUTH*` (`:196-204`, `:246-253`).
- **Appends required** `HARMONIK_*`: `RUN_ID`, `DAEMON_SOCKET`, `WORKSPACE_PATH`,
  `HANDLER_SESSION_ID`, `CLAUDE_SESSION_ID`, `WORKFLOW_ID`, `NODE_ID`, and hard-coded
  `HARMONIK_AGENT_TYPE=claude-code` — `:258-267`.
- **Optional**: `HARMONIK_WORKFLOW_MODE`, `_PHASE`, `_ITERATION_COUNT`, `_BEAD_ID` — `:271-282`.
- **Re-emits credentials as empty overrides** `ANTHROPIC_API_KEY=` / `ANTHROPIC_AUTH_TOKEN=` /
  `CLAUDE_CODE_OAUTH_TOKEN=` (`:292-296`) so the tmux server env can't leak live creds via the
  additive `-e` mechanism (CI-003). **← This is the credit-burn guard precedent for codex.**

cwd = the per-bead git worktree (`spec.WorkDir = rc.workspacePath`, `:411`).

Side-channel disk state before launch: `.claude/settings.json` (hook bridge), `~/.claude.json`
trust pre-seed (`EnsureWorktreeTrust`), `.harmonik/agent-task.md` carrying the task body
(`WriteAgentTask`); a `settings.local.json` shadow check fails-fast — `:236-294`.

### A.4 tmux substrate (harness-AGNOSTIC)

`tmux new-window -P -F '#{pane_id}' -d -t <session>: -n <window> -c <cwd> -e KEY=VAL... -- <command>`
— `internal/daemon/osadapter.go:486-504`. argv joined with spaces into one command string
(`tmuxsubstrate.go:368-371`). Session naming `harmonik-<project_hash>-<session_name>`
(`provenance.go:114`). Spawn semaphore caps concurrency (`tmuxsubstrate.go:309-349`). PID/liveness
via `kill(pid,0)` + `#{pane_pid}` (`tmuxsubstrate.go:900-989`). `Stdout()` returns nil
(`tmuxsubstrate.go:1049`) — there is **no stdout pipe**; progress comes via the hook bridge.

### A.5 Claude-specific primitives a codex harness may lack

- `--session-id`/`--resume` with caller-minted **UUIDv7** (the whole resume model assumes Claude's
  persistent-session-by-id semantics) — `claudelaunchspec.go:358-362`.
- `--dangerously-skip-permissions` (trust-dialog suppression) — `:374`.
- `--model`/`--effort` Claude vocab — `:363-368`.
- Forbidden-flag deny-list `--fork-session`/`--bare`/`--no-session-persistence` + env
  `CLAUDE_CODE_SKIP_PROMPT_HISTORY` — `claudehandler_chb006_024.go:52-62`.
- The **hook bridge** (`.claude/settings.json` hooks + `~/.claude.json` trust + `settings.local.json`
  shadow rule) — Claude's hook system is how harmonik gets progress/completion signals; there is no
  stdout pipe.
- Interactive-REPL kick-off: splash-dismiss Enter + bracketed-paste + submit-Enter + `/quit`.

## Part B — Prompt injection, re-task, commit/completion detection

### B.1 Prompt injection (Claude-TUI-specific) — `internal/daemon/pasteinject.go`

`pasteInjectImplementerInitial` (`pasteinject.go:879`):
1. **Splash dismiss** — `SendEnterToLastPane` fires a *bare Enter keypress* (NOT `-l` literal) to
   dismiss claude's React/ink welcome splash — `:888-896` (bare key-event form required, `:91-99`).
2. **750ms grace** (`splashDismissDelay`, `:56`) — `:850-858`.
3. **Paste** `"Please read .harmonik/agent-task.md and begin.\n"` via tmux `load-buffer`+`paste-buffer`
   into buffer `harmonik-<sessionID>-task` — `:898-904`, `:1428`.
4. **Submit Enter** — a *second* bare Enter, because in bracketed-paste mode the payload `\n` is
   NOT dispatched as a key event (hk-8cq23) — `:905-910`.

Ordering invariant: paste MUST fire after `agent_ready`, else the splash swallows the text
(`workloop.go:2291-2308`).

### B.2 Re-task (review-loop iteration ≥2) — reuses the SAME live session

`pasteInjectImplementerResume` (`pasteinject.go:932`) pastes one combined task+feedback message
(single buffer/single Enter, hk-poy7k) with a **bounded Enter retry** (`sendResumeSubmitEnter`,
`:1000-1014`) because a fresh `claude --resume` REPL is intermittently not input-ready. Reviewer
phase pastes a "write `.harmonik/review.json`" prompt — `:1024-1059`.

### B.3 Quit-on-commit + watchdogs

`pasteInjectQuitOnCommit` (`pasteinject.go:451`) polls worktree HEAD every 500ms; on a new commit
sends `/quit` (claude slash command) via `SendQuitToLastPane` to fire the Stop hook (`:677`).
Post-commit /quit watchdog (hk-5s7tg): waits `postQuitKillGrace = 60s` (`:391`) then force-`Kill`s
so `sess.Wait` unblocks even if `/quit` hit a stale pane (`:694-711`). noChange grace path
(`fireNoChangePath`, `:526`): sends `/quit` unconditionally, waits `noChangeKillDelay = 30s`
(`:375`), then `Kill`, then closes `noChangeTimeoutCh`. Kill triggers: `launchHeartbeatTimeout`
(180s, `:364`), `heartbeatStalenessThreshold` (8m, `:337`), `commitPollTimeout` (30m, heartbeat-
extended, `:302`), absolute `commitHardCeiling` (90m, never extended, `:318`).

Agents cannot run slash commands from their tool API (`:406-408`) — so `/quit` is the daemon's only
programmatic session-exit lever, and it depends on the Stop hook firing (`workloop.go:2315-2318`).
Pane liveness via `pgrep`/`ps comm` matching `"claude"`/`"node"` (`:165, 217-258`).

### B.4 Commit/completion detection — PURE GIT, harness-AGNOSTIC

- Detection: `git rev-parse HEAD` in the worktree (`resolveWorktreeHEAD`,
  `sessioncontext_chb023.go:176-191`) vs captured pre-run parent `headSHA`; commit landed iff
  `curHead != headSHA` (`workloop.go:2384-2385`).
- noChange detected 3 git-only ways: poll-loop kill paths; merge-time `runTip == mainTip || runTip
  == headSHA || branch-missing → noChange` (`workloop.go:3497-3529`); single-mode guard
  `noCommitGuardShouldReopen` reopens bead when `curHeadSHA == parentSHA` unless already on main
  (`workloop.go:2649-2656`).
- "Already landed" = **`Refs: <beadID>` commit trailer** — `beadAlreadySubsumedInMain` line-exact
  greps `git log main --format=%B -20` (`workloop.go:2668-2684`).
- "Run done" = `sess.Wait` returns (`workloop.go:2265, 2353`), guaranteed by the
  `/quit`→Stop-hook→force-kill chain.

**None of this inspects Claude internals.** As long as the agent commits with the
`Refs: <beadID>` trailer, harmonik detects "done" with zero harness-specific code.

## Part C — What a codex harness must re-provide vs inherits free (from A+B)

**Inherits free (harness-agnostic git + tmux layer):** the entire commit/completion-detection layer
(HEAD-SHA polling, `Refs:<bead>` trailer subsumption, merge noChange logic, reopen-on-no-commit
guard) and the whole worktree/branch/merge machinery; the tmux substrate (new-window, cwd/env `-e`
injection, PID-liveness, spawn semaphore); credential env-stripping pattern.

**Must re-provide (Claude-CLI-coupled layer):** (1) first-turn prompt delivery; (2) re-task for
review-loop iterations; (3) a programmatic session-exit equivalent to `/quit`+Stop-hook; (4) a
pane/process-liveness probe; (5) the `agent_heartbeat`/`agent_ready` event signals the
budget/staleness logic consumes (`pasteinject.go:577`, `workloop.go:2306`).

**Key simplification opportunity:** a non-interactive `codex exec "<prompt>"` that **exits on
completion** would obviate the splash-dismiss, dual-Enter, `/quit` injection, and post-quit
force-kill *entirely* — the commit-detection layer underneath carries over unchanged. The
interactive-TUI machinery exists only because claude is driven as a live REPL with no headless exit.

## Part D — Worktree isolation, review-loop reuse, workflow-node harness selector

### D.1 Worktree lifecycle — `internal/workspace/createworktree.go`, `internal/daemon/workloop.go`

- **Base SHA:** `resolveParentCommit(...)` (`workloop.go:1680`); `resolveHEAD` (`git rev-parse HEAD`,
  `workloop.go:2842-2854`) is the fallback start-point. Worktree is cut from an explicit SHA, not
  `HEAD`, to avoid racing operator activity (`createworktree.go:88-95`).
- **Create:** `productionWorktreeFactory` (`workloop.go:2824`) → `workspace.CreateWorktree`
  (`createworktree.go:123`) runs `git worktree add -b run/<run_id> <path> <parentCommit>`
  (`:136`). Called at `workloop.go:1720`.
- **Merge one-at-a-time:** `lockedMergeRunBranchToMain` (`workloop.go:3447`) holds `deps.mergeMu`
  and calls `mergeRunBranchToMain` (`:3473`): rebase run-branch onto target (`:3531`), FF-only
  check (`:3598`), FF the target (`:3611`). Target = `deps.targetBranch =
  resolveTargetBranch(cfg.TargetBranch)` default `main` (`:576-584`).
- **Remove:** `removeWorktree` → `git worktree remove --force --force` (`:2896-2908`).
- **Reviewer worktree:** separate short-lived **detached-HEAD** worktree at
  `<run>-reviewer-<iter>` via `CreateReviewerWorktree` (`createworktree.go:37-72`).

### D.2 Review-loop reuses the implementer spawn path — `internal/daemon/reviewloop.go`

The reviewer uses the **same** `buildClaudeLaunchSpec` helper as the implementer (implementer spec
`reviewloop.go:227`; reviewer spec `:814`). Both build a `claudeRunCtx` differing only in `phase`
(`ReviewLoopPhaseReviewer` vs `…ImplementerInitial/Resume`, `:798` vs `:209`) and
`priorClaudeSessID` (reviewer always `nil`, CHB-009, `:800`). Both wrap `deps.substrate` in
`newPerRunSubstrate` (`:246`, `:830`) and launch via the same `handler.Launch` + pasteinject path.
DOT cascade does the same in one place (`dot_cascade.go:499-525`), choosing phase via
`nodeIsReviewer(node)` (`:248, 461-466, 802-805`).

### D.3 DOT model — `internal/workflowvalidator/dotparser.go`, `internal/core/node.go`, `standard-bead.dot`

Nodes carry `type`, `handler_ref`, `agent_type`, `tool_command`, `model`, `effort`, `prompt`,
`role` — parsed generically into an attr map (`dotparser.go:156-216`; struct `node.go:13-104`).
**Crucially, `handler_ref="claude-implementer"/"claude-reviewer"` does NOT select a binary** — it
only distinguishes role/phase (`dot_cascade.go:216, 802-805`). The actual binary is always
`deps.handlerBinary` (config `Config.HandlerBinary`, default `"claude"`, `daemon.go:92-99`,
`workloop.go:516-518`). Default workflow `standard-bead.dot`: start → implement → commit_gate
(shell) → review → close.

### D.4 SHARED vs VARYING (the seam map)

**SHARED (codex reuses unchanged):**
- tmux substrate — `SpawnWindow(ctx, SubstrateSpawn)` (`substrate.go:30-37`); handler builds opaque
  argv `append([]string{spec.Binary}, spec.Args...)` (`handler.go:291, 299`). **Harness-blind.**
- worktree create / rebase-merge-one-at-a-time / remove (§D.1) — pure git.
- commit-detection — `resolveWorktreeHEAD` vs `headSHA` (`workloop.go:2382-2385`).
- review-loop control flow / DOT cascade / queue / dispatch / no-progress detector — keyed on git
  state + Outcome, not harness.

**VARYING (must differ per harness), all concentrated in `buildClaudeLaunchSpec`
(`claudelaunchspec.go:220-424`):**
- argv (`:357-376`) — Claude-specific flags.
- Binary (`spec.Binary = rc.handlerBinary`, `:408`).
- Prompt seed / injection — pasteinject of `agent-task.md`/`review-target.md`;
  `MaterializeClaudeSettings` (`.claude/settings.json`, `:233-236`); `EnsureWorktreeTrust`
  (`~/.claude.json`, `:241`); `PreExecMessages` (`:384-397`).
- Re-task / teardown — `--resume <uuid>` (`:358-359`); `/quit` teardown via pasteinject.
- Ready-detection — `ClaudeCodeAdapter.DetectReady` keyed on `agent_ready`/NDJSON
  (`adapter_claudecode.go:88-95`).

### D.5 Where a harness selector naturally lives (TWO complementary seams)

1. **Per-node DOT attribute:** add `harness` / `agent_runtime` alongside `agent_type`/`handler_ref`
   in the node attr-map (`node.go`, `dotparser.go`) — already a generic string map, so parsing is
   free. The cascade routes `rc.handlerBinary` + which spec-builder/adapter off it.
2. **Adapter registry:** `handlercontract.AdapterRegistry` keyed by `core.AgentType`
   (`adapterregistry_hc012.go:67`; `ForAgent(AgentTypeClaudeCode)` at `dot_cascade.go:587`,
   `reviewloop.go`). Add a `core.AgentTypeCodex` adapter + a codex `buildLaunchSpec` via the
   **`deps.launchSpecBuilder` seam** (`dot_cascade.go:521-523`) — already a swappable function
   field, the **cleanest insertion point**.

## Part E — Synthesis: the implicit Harness contract

From A–D, the minimal contract a harness must satisfy (the seam), with everything else inherited:

| Contract method (proposed) | Claude impl today | What it produces |
|---|---|---|
| `LaunchSpec(ctx) → (argv, env, cwd)` | `buildClaudeLaunchSpec` `:220-424` | the spawn argv + env + worktree cwd |
| `Seed(session, taskPath)` | splash-dismiss + bracketed-paste `pasteinject.go:879` | first-turn prompt delivery |
| `Retask(session, feedback)` | `pasteInjectImplementerResume` `:932` | review-loop iter ≥2 follow-up |
| `Teardown(session)` | `/quit` + 60s grace + Kill `:677-711` | end the session so `sess.Wait` returns |
| `DetectReady(events)` | `ClaudeCodeAdapter.DetectReady` `adapter_claudecode.go:88-95` | the `agent_ready` signal |
| (session id) | caller-minted UUIDv7 `claudehandler_chb006_024.go:147` | persistent-session handle |

Everything below this line is SHARED and unchanged: tmux substrate, worktree mgmt, git
commit-detection (`Refs:<bead>` trailer), merge-one-at-a-time, review-loop control flow, DOT
cascade, queue/dispatch, watchdogs' *trigger* logic (the budget timers live in the shared loop; only
the *teardown action* they invoke is harness-specific).
