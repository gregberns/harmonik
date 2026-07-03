# session-keeper — Decomposition (components)

Six components. Phase 1 (warn-only, non-destructive) = C1+C2(warn mode)+C3+C6. Phase 2 (full cycle) adds C4+C5 and C2's reset mode.

## C1 — gauge-signal
A statusLine script (shipped in-repo, referenced from each orchestrator's `~/.claude/settings.json`) that reads `context_window.used_percentage` from its stdin JSON and writes it to a keeper-owned gauge file `.harmonik/keeper/<agent>.ctx` (atomic write). Carries the live session id so the watcher can detect a post-`/clear` session change.
- *Depends on:* Claude Code v2.1.132+. *Reuses:* nothing (new).
- *Done:* a running orchestrator updates its gauge file within ~1s of each assistant message (S1).

## C2 — context-watcher
The polling loop (one per orchestrator). Reads the gauge file, applies two thresholds (warn ~80%, act ~90%), idle-gates on a Stop-boundary signal, debounces, and decides the action. **Warn mode** (Phase 1): emit warn event + inject wrap-up prompt, never reset. **Reset mode** (Phase 2): drive C4. Models its probe/backoff/crash-window logic on `internal/supervise/supervisor.go Supervisor.Run()`.
- *Depends on:* C1, C3. *Reuses:* supervisor.go patterns; event bus.
- *Done:* crosses 80% → exactly one warn injection at an idle boundary, no other effect (S2).

## C3 — injector + target-registry
The tmux `send-keys` step that delivers a prompt/slash-command to the orchestrator's pane, plus the **agent-name ⇄ tmux-target** convention it needs. tmux target derived from `$HARMONIK_AGENT` + the `harmonik-<project_hash>-<agent>` naming convention (`internal/lifecycle/provenance.go:106-116`). Reuses the external-`send-keys` model (unaffected by the Stop-hook no-TTY limit).
- *Depends on:* the naming convention. *Reuses:* tmux send-keys mechanics from pasteinject.
- *Done:* a prompt lands in the correct pane and submits (bracketed-paste + Enter).

## C4 — handoff-cycle (Phase 2)
Orchestrates the intent-preserving reset when C2 fires reset mode, **at an idle boundary**: inject `/session-handoff HANDOFF-<agent>.md` → poll that file until its first-line **date/branch stamp** changes (mtime alone is unreliable; shared file is raced) → inject `/clear` → inject `/session-resume HANDOFF-<agent>.md`. Includes the **anti-loop cleared-marker** `.harmonik/keeper/<agent>.json {last_cleared_at, resumed_stamp, session_id}` — suppress re-trigger until a *new* session id is seen AND the gauge drops. **Daemon-coordination:** never fire while the agent holds an in-flight dispatch (check live queue).
- *Note:* the per-agent stamped handoff path **already exists** as a project convention (`HANDOFF-flywheel.md` / `-named-queues.md` / `-controlpoints.md`) — C4 reuses it rather than inventing one.
- *Depends on:* C2, C3. *Done:* full cycle completes, resumed session reads the fresh handoff and continues its lane; never mid-dispatch; never double-fires (S3, S4).

## C5 — compaction-backstop (Phase 2)
A `PreCompact` hook (Claude Code v2.1.140+) that blocks native auto-compaction (`decision:block`/exit 2) and signals the watcher to run C4 instead — the safety net if C2 misses the threshold.
- *Depends on:* C4. *Done:* native auto-compaction never fires on a supervised session (S5).

## C6 — events
New `session_keeper_*` event types (constant in `internal/core/eventtype.go`, emitted via `EmitWithRunID`, eventbus.go:152): `session_keeper_warn`, `session_keeper_handoff_started`, `session_keeper_cycle_complete`, `session_keeper_suppressed_loop`. For observability + replay; lets peers/operator see keeper activity on the bus.
- *Depends on:* event bus. *Done:* each keeper action emits a typed event visible in `harmonik subscribe`.

## Open architecture fork (needs a call before spec draft)
**How is the watcher hosted?**
- **(A) New `harmonik keeper` subcommand** — a standalone external watcher, one process per orchestrator (`harmonik keeper --agent flywheel`), reusing `supervisor.go` patterns. Clean separation; the orchestrator claudes are *not* currently under `harmonik supervise` (the supervisor watches the **daemon**, not the orchestrator panes), so this models reality directly.
- **(B) Generalize `harmonik supervise`** to supervise N targets including orchestrator claudes with context-watching. More unified, but enlarges the supervise contract (named-queues' lane) and couples orchestrator-keeping to daemon-supervision.
- *Leaning A* (matches the fact that orchestrator panes are independent of the daemon-supervisor; smaller blast radius in named-queues' lane). Confirm before spec draft.
