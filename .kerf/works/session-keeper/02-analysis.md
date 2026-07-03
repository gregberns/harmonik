# session-keeper — Analysis (current territory)

Factual map of what exists today in the areas hk-ekap1 touches, from 3 investigations (Claude Code surfaces; pasteinject+supervise; handoff/resume skills + registry). Anchored to file:line where known.

## A. The context-fill signal (Claude Code harness)

- **Only live source is the statusLine script.** Its stdin JSON carries `context_window.used_percentage` + `remaining_percentage` (v2.0+), and a full breakdown `context_window.current_usage` {input/output/cache_creation/cache_read tokens} + `total_input_tokens` (current-context, not cumulative) at **v2.1.132+**. Re-runs are event-driven (after each assistant message, after `/compact`, on mode toggles), debounced ~300ms; an optional `refreshInterval` adds timer re-runs.
- **Dead end (confirmed):** the session transcript `.jsonl` does **not** carry per-message token counts. Polling the transcript cannot yield context fill. statusLine is the *only* gauge.
- **Stop hook** fires after each response (idle boundary), runs arbitrary shell, can block (`decision:block`/exit 2). BUT since v2.1.139+ hooks run with **no controlling terminal** — a hook **cannot** itself `tmux send-keys`. (Irrelevant to us: the *external* supervisor injects, not the hook.)
- **PreCompact hook** fires before auto-compaction and **can block it** (`decision:block`/exit 2), v2.1.140+. This is our **backstop** if the watcher misses the threshold.
- **Min-version envelope for the full design: Claude Code v2.1.140+** (statusLine breakdown 132, no-TTY-hook 139, PreCompact 140).

## B. Injection + supervision (harmonik)

- **Two distinct supervision paths exist — we want the orchestrator one, not the implementer one:**
  - **Orchestrator-supervision = `internal/supervise/supervisor.go` `Supervisor.Run()` (191-332).** Supervises a *single long-running child* (the daemon / orchestrator) via: health-probe + heartbeat-file freshness (389-431), restart policy (PolicyNever/OnFailure), crash-loop sliding window (5/60s), backoff+jitter (1s→60s). Target id = `Spec.Command` + `Spec.HeartbeatPath` — **no run_id, no tmux coupling.** Launched into tmux session `harmonik-flywheel` (`cmd/harmonik/supervise/start.go:149`). **← session-keeper's context-watcher belongs here.**
  - **Implementer-supervision = `internal/daemon/pasteinject.go` + `workloop.go beadRunOne()`.** Per-*bead* injection: `WriteLastPane()` (pasteinject.go:764) + `SendEnterToLastPane()` (771) deliver a prompt to the bead's tmux pane; `pasteInjectQuitOnCommit()` (416-620) watches for commit/staleness and sends `/quit`. **This is NG3 (implementer claudes) — NOT our path.** (One investigator suggested hooking `beadRunOne`; that's wrong for session-keeper — it would watch implementers, not orchestrators.)
- **Prompt-injection primitive is reusable:** the tmux `send-keys` mechanics behind `WriteLastPane`/`SendEnterToLastPane` (and the `/tmp/hk-daemon-supervise.sh` external watcher) are the model for the keeper's inject step. The keeper does the `send-keys` *externally* — unaffected by the Stop-hook no-TTY limit.
- **Event bus:** emit via `EmitWithRunID(ctx, runID, eventType, payload)` (eventbus.go:152); new event types are constants in `internal/core/eventtype.go`, F-class ones registered in the fsync map (busimpl.go:115). Lets the keeper emit `session_keeper_*` events for observability/replay.

## C. Handoff / resume skills + the missing registry

- **`/session-handoff`** (`~/.claude/skills/session-handoff/SKILL.md`): prose-only, writes `./HANDOFF.md` by default (accepts a path arg), requires the `<!-- PP-TRIAL:v2 DATE branch -->` first line, preserves any `<!-- ORCHESTRATION DIRECTIVES -->` block. **mtime is not a reliable completion signal** (advances only on rewrite, and the shared file is raced by 3 agents) → confirm completion by reading the **first-line date/branch stamp** (+ ideally a keeper-injected session-id token), and use a **per-agent path** `HANDOFF.<agent>.md`, not the shared `HANDOFF.md`.
- **`/session-resume`** (`~/.claude/skills/session-resume/SKILL.md`): reads `./HANDOFF.md` (or path arg) and continues — **takes no session-name arg, no interactive picker, no tmux awareness.** *Correction to the bead:* the "named-session-or-picker-hangs" risk is the **harness** `claude --resume`/`/resume` picker (agent A), NOT this skill. Our flow keeps **one live tmux pane** and injects `/session-handoff → /clear → /session-resume <path>` in place — so **no `claude --resume` picker is ever invoked**, and the picker-hang risk is largely moot. (Naming still matters for the keeper to *target* the pane — see registry gap.)
- **Three registry/state gaps the keeper must close (none exist today):**
  1. **agent-name ⇄ tmux-session map.** `comms who` gives liveness only (`agent/status/last_seen`, 120s TTL; `cmd/harmonik/comms.go:598-643`) — **no tmux session/pane/pid.** tmux naming convention is `harmonik-<project_hash>-<session_name>` (`internal/lifecycle/provenance.go:106-116`); agent identity is `$HARMONIK_AGENT` (`comms.go:195`). Convention to add: orchestrator's `$HARMONIK_AGENT` ⇒ derivable tmux target.
  2. **Per-session context gauge file.** Nothing writes context fill anywhere (`~/.claude/.session-stats.json` tracks tool counts only). The statusLine script must write `used_percentage` to a keeper-owned path (e.g. `.harmonik/keeper/<agent>.ctx`).
  3. **Anti-loop cleared-marker.** No post-`/clear` sentinel exists. Keeper must write `.harmonik/keeper/<agent>.json {last_cleared_at, resumed_handoff_stamp}` and suppress re-trigger until it sees a *new* session id AND the gauge has actually dropped.

## D. Constraints to preserve

- Don't touch the implementer path (pasteinject/beadRunOne) — orchestrator-only (NG1/NG3).
- Reuse existing skills as-is (NG2): keeper *triggers* `/session-handoff`/`/session-resume`, doesn't redesign them.
- All keeper state under gitignored `.harmonik/` (escape-detector safe; consistent with comms).
- Lane: implementation touches `internal/supervise/*` (named-queues' lane) → co-design with named-queues (C7).

## E. Net new surface session-keeper introduces

1. A **statusLine script** (shipped in repo, referenced from the orchestrator's settings) that writes the gauge file.
2. A **context-watcher** inside / alongside `Supervisor.Run()` that polls the gauge, idle-gates, and injects.
3. Three small **conventions/state files** (B/C registry gaps above).
4. A **PreCompact hook** backstop.
5. New **`session_keeper_*` events** on the bus.
6. **Phase 1** exercises 1+2(warn-only)+5; **Phase 2** adds the full handoff→clear→resume cycle + 3 + 4.
