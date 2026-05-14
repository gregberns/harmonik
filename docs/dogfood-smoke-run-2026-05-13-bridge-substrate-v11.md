# Dogfood Smoke Run — 2026-05-13 — Bridge + Substrate (v11, post hk-53y35 + hk-rf4ux)

**Verdict:** **FUNCTIONAL GREEN / OPERATIONAL RED** — claude executes the bead end-to-end correctly, but the daemon never observes `bead_closed`.
**Bead:** smoke-9pc (independent confirmation run from main HEAD)
**Date:** 2026-05-14
**HEAD:** `283c5ec` (chore(beads): sync — close hk-53y35, hk-rf4ux)
**Prior runs:** v3 RED (no task) → v4 RED (socket) → v5 RED (Wait deadlock) → v6 RED (callback) → v7 RED (pane target) → v8 RED (trust path) → v9 RED (paste ordering) → v10 RED (splash race + permissions)

---

## What works (functional GREEN)

Inside `$SMOKE_DIR = /private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.uijvftI60b`:

- `marker.txt` contents:
      initial
      SMOKE-OK
- Worktree git log:
      2cc526e Add SMOKE-OK marker line to marker.txt
      608e6b4 initial commit

Claude, running entirely inside the daemon's tmux substrate with no human intervention:

1. Started cleanly past the welcome splash (hk-rf4ux fix: `SendEnterToLastPane` + 750ms `splashDismissDelay` works).
2. Received the kick-off "Please read .harmonik/agent-task.md and begin." submitted to the REPL.
3. Read `agent-task.md`.
4. Edited `marker.txt` via the `Edit` tool — no per-tool permission dialog (hk-53y35 fix: `permissions.allow` in materialized `.claude/settings.json` works).
5. Ran `git add marker.txt && git commit` via the `Bash` tool — no dialog.
6. Announced completion: `⏺ Added SMOKE-OK line to marker.txt and committed (2cc526e).`

End-to-end, claude executed the bead correctly without operator input. **This is the milestone the umbrella hk-lj1p9 was built to deliver.**

## What doesn't work (operational RED)

The event stream stops at `agent_ready`:

| event | count |
|---|---|
| daemon_started | 1 |
| daemon_orphan_sweep_completed | 1 |
| run_started | 1 |
| handler_capabilities | 1 |
| session_log_location | 1 |
| skills_provisioned | 1 |
| launch_initiated | 1 |
| agent_ready | 1 |

No `Stop` hook event, no `outcome_emitted`, no `agent_completed`, no `bead_closed`. The smoke bead (smoke-9pc) remains `◐` (in_progress) in `br list`. The daemon's workloop goroutine sits at `sess.Wait`. The capacity gate is jammed — if a second bead were ready, it could not be dispatched.

Root cause (filed as **hk-cmybm**, P1): In interactive TUI mode, Claude Code's `Stop` hook fires on session exit (e.g. `/quit`, Ctrl-C), not after each assistant response. After claude finishes the bead, it sits at the REPL waiting for next input — the daemon's bead-completion signal never arrives.

CHB-014's "stop-hook outcome envelope" implicitly assumed each response triggers Stop. That assumption is incorrect for interactive mode.

## Fix paths for hk-cmybm

a) **Kick-off includes /quit instruction.** Inject a follow-up `/quit\n` after the task is complete (e.g. the daemon paste-injects `/quit` after observing the worktree commit, or the kick-off message ends with "...and then run `/quit` to end the session").

b) **Agent-side outcome emission.** Have the agent call `hk emit-outcome` via the RequestHandler socket path (currently stubbed as `noopRequestHandler` per hk-tjl40 commit `2104168`). Requires implementing the real handler and a per-bead `CLAUDE.md` or skill instructing claude to call it.

c) **Daemon-side inference.** Spec amendment: daemon infers `bead_closed` when (i) the worktree branch lands a new commit AND (ii) claude transitions to idle for N seconds. Cleaner separation but more work.

(a) is cheapest and validates the path; (b) is the architecturally correct long-term answer; (c) is a fallback if (a)/(b) prove unreliable.

## Confirmed-working umbrella fixes (no regressions)

All 10 prior fixes remain confirmed in v11's run:

| Bead | Fix | Confirmed |
|---|---|---|
| hk-tjl40 | socket bind in daemon.Start | ✓ daemon.sock present |
| hk-smuku | Wait uses stored PID | ✓ no deadlock |
| hk-lj1p9.4 | SetAgentReadyCallback + pasteInjectOnLaunch wired | ✓ agent_ready fires |
| hk-yngq2 | pane ID stable target | ✓ paste-inject uses %NNNN |
| hk-o5eww | EvalSymlinks trust path | ✓ no trust dialog |
| hk-zchbu | paste-inject after waitAgentReady | ✓ ordering OK |
| hk-rf4ux | SendEnter + 750ms splash delay | ✓ kick-off submitted |
| hk-53y35 | permissions.allow in settings.json | ✓ no tool dialogs |
| hk-lj1p9.3 | EnsureWorktreeTrust env override + flock | ✓ no ~/.claude.json contention |

## Next

File **hk-cmybm** is filed as P1. Investigation should start with fix path (a) — simplest and reproducible inside the smoke loop.
