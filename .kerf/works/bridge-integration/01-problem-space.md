# 01 — Problem Space

## Summary

Ship the claude-hook-bridge end-to-end: when the harmonik daemon picks up a ready bead, it launches the real `claude` CLI as a full interactive session inside a named tmux pane the operator can attach to, claude performs real work in a worktree, and completion is observed via the Stop hook flowing through the daemon's Unix socket (not stdout EOF). The bridge spec corpus (CHB-001..027 in `specs/claude-hook-bridge.md` plus amendments in HC / WM / PL / EM) describes the wire format and is correct; what is missing is (a) the integration wiring inside the daemon work loop, (b) the tmux/ntm substrate that hosts the claude subprocess, and (c) a small set of additive spec amendments to make the substrate normative.

## Goals

1. The dogfood smoke runs **GREEN** against the real `claude` CLI: one disposable bead, claude appends `SMOKE-OK` to `marker.txt` and commits in the worktree, the daemon receives `outcome_emitted` via the Stop hook, the bead is closed as success only when actual work was performed.
2. Each claude subprocess runs inside a named tmux pane derived deterministically from `run_id`; the operator can `tmux attach -t <name>` and **see + interact with** the live session.
3. The tmux substrate is reusable for non-claude handlers (the twin, future agent types). It is not a one-off for claude.
4. The integration is additive — no existing CHB-001..027 clauses are revised; existing unit tests for the bridge pieces keep passing without change.
5. Replay safety preserved: pane name is a deterministic function of `(run_id, session_id, phase, iteration_count)` so a replayed run resolves to the same pane name.

## Non-goals

- **Daemonization.** Foreground `hk` binary remains the runtime (locked decision 2026-05-08). No detached process, no JSON-RPC operator socket, no pidfile work in this initiative.
- **DOT workflow mode.** The third dispatch mode stays an empty spec slot; this work is `single` + `review-loop` only.
- **Multi-project.** One project at a time, current model.
- **Cloud / remote execution shape.** Tmux + local subprocess only. The execution-shape seam at HC-052 stays; we just fill in the local-tmux adapter, not a cloud one.
- **Reconciliation.** Out of scope.
- **Refactoring the bridge pieces.** `internal/handler/claudehandler_chb006_024.go`, `internal/workspace/claudesettings_wm040a.go`, `internal/hookrelay/`, `cmd/harmonik-twin-claude/` stay as-is; this work *calls* them.
- **Reviewer-phase claude wiring.** Single-mode end-to-end first; review-loop integration is a follow-up once single is GREEN.

## Constraints

- **Spec discipline.** All normative requirements live in `specs/`. Amendments are additive only; CHB-001..027 do not change.
- **Determinism.** Pane names, env-var values, and worktree paths must all be deterministic functions of `LaunchSpec` inputs.
- **ntm pin.** Per PL-021a, missing tmux is a Cat-0 fail-fast (exit 22); the integration MUST honor that. No silent degradation to pipe mode.
- **Twin parity.** `harmonik-twin-claude` continues to exercise the same wire format via the same daemon path. The daemon stays twin-blind (CHB-022). If twin doesn't run under tmux at MVH, the spec must say so explicitly.
- **Existing test surface preserved.** All passing tests in `internal/daemon/`, `internal/handler/`, `internal/workspace/`, `internal/hookrelay/` keep passing. New tests added for the new wiring.
- **Foreground tmux.** The operator is already inside a tmux session (today's working assumption). The claude panes are new windows or split panes within an `hk`-owned session, not a parallel session the operator has to find.

## Success criteria (verifiable)

1. **Real-claude smoke GREEN.** `/tmp/hk --project <scratch-dir>` with one disposable workflow:single bead and `claude` as the handler binary results in: claude session ID minted, `.claude/settings.json` materialized in the worktree before exec, tmux window created with the expected name pattern, claude exec'd inside that window, Stop hook fires through `harmonik hook-relay`, daemon receives `outcome_emitted`, bead closed as `done` only because the work happened (marker.txt mutated, commit made on the worktree branch).
2. **Operator can attach.** `tmux list-windows -t harmonik-<project-hash>` shows the per-run window during the smoke; `tmux select-window` brings up the live claude TUI; the operator can type into it.
3. **Determinism check.** Computing the pane name from the (run_id, phase, iteration_count) of a recorded run reproduces the exact name observed at runtime.
4. **Orphan recovery.** A `SIGKILL` to `hk` mid-run leaves a stale tmux window; the next `hk` startup runs `SweepOrphanTmuxSessions`, kills it, and the run_id-keyed window is gone within 2s (per PL-006).
5. **Twin parity.** `harmonik-twin-claude` runs through the same daemon path and the bead closes the same way; the daemon does not branch on twin-vs-real.
6. **Review-loop smoke GREEN (stretch within this initiative).** Same shape with a `workflow:review-loop` bead: implementer and reviewer phases both run as real claude sessions in named tmux windows; the iteration loop terminates via the existing APPROVE / REQUEST_CHANGES / BLOCK / cap-hit / no-progress paths; bead closed correctly per the verdict.
7. **Standalone start.** A `hk tmux-start` (or similarly-named) subcommand creates a fresh tmux session and execs hk inside it, so the smoke flow works from a non-tmux shell without ceremony. When hk is started from inside an existing tmux session (the operator's, e.g. our current `harmonik` session), claude windows appear alongside the operator's existing windows in that same session.
8. **Spec amendments merged.** The additive amendments are present in `specs/` and reviewed: PL-021b (pane lifecycle), WM-* (deterministic window naming), HC-052 refinement (`Session.Attach()` contract for claude-code), CHB-028 (twin parity / tmux scope), plus a new clause for "reuse operator's tmux session if `$TMUX` is set, else create one via the start subcommand."

## Workstreams (provisional)

Three streams are largely parallelizable. Final decomposition lands in `07-tasks.md` after the design pass.

- **Stream A — Spec amendments.** PL-021b/c (pane lifecycle), WM-* (deterministic pane naming, mirror of WM-002 worktree), HC-052 refinement (`Session.Attach()` contract for claude-code), optional CHB-028 (twin parity for tmux). Pure-doc work, no code. Lands first; informs Streams B and C.
- **Stream B — Tmux/ntm substrate.** ntm adapter binding (or direct tmux invocation if ntm is skipped at MVH), `harmonik runner` command (PL-028 four-step lifecycle), tmux session/pane creation primitives, `Session.Attach()` implementation that returns a live pty reader for the pane. Independent of Stream C at the package level: the substrate is the *thing that gives* a pty + working directory; Stream C is the *thing that emits messages and observes hooks*.
- **Stream C — Bridge wiring in daemon.** Workspace creation calls `MaterializeClaudeSettings`. Workloop populates `LaunchSpec.Env` via `ClaudeEnvVars`, builds argv with `--session-id`, checks forbidden flags. `HookRelayHandler` is implemented and wired into `RunSocketListener`. Completion path waits on either watcher.Done() or `outcome_emitted` from `HookSessionStore`. `PreExecMessages` emitted before exec. `RunHeartbeatLoop` started after exec. Terminal event derived via `MapWaitReturnToTerminalEvent`.

**Streams B and C converge** at the moment the workloop hands the LaunchSpec to a substrate-aware handler that spawns inside a tmux pane rather than via `exec.CommandContext` directly. That seam is the focal design decision of the design pass.

## Decisions taken (locked at this pass)

- **D1 — Both modes in scope.** The daemon-side wiring is shared between `workflow:single` and `workflow:review-loop`. Doing the wiring right fixes both at once. Single-mode smoke is the dispatchable GREEN gate; review-loop smoke is a stretch within the same initiative — if review-loop is non-trivially harder than expected, it splits into a follow-up, but the default is to land both.
- **D2 — Skip ntm at MVH; shell out to `tmux` directly.** A small adapter in `internal/lifecycle/tmux` owns the tmux primitives (session create, window create, attach pty, list/kill for orphan sweep). ntm binding is its own initiative after this one. The existing `OSTmuxSessionLister` / `OSTmuxSessionKiller` interfaces in `internal/lifecycle/orphansweep.go` are the model; we add the creation side alongside them.
- **D3 — Tmux session reuse.** If `$TMUX` is set when hk starts, hk creates new windows inside the operator's existing session. If `$TMUX` is empty, hk does NOT silently create a session — the operator runs a new `hk tmux-start` subcommand which creates a session and execs `hk` inside it.
- **D4 — Naming.** Window names use the **bead id** (e.g. `smoke-67u`), not a UUID. For review-loop, the phase + iteration suffix the bead id (e.g. `smoke-67u/r1` for reviewer iter 1). Deterministic from `(bead_id, phase, iteration_count)`. No project-hash prefix on windows because the operator's session is the namespace; project-hash stays on **session names** for the orphan sweep (PL-006a).
- **D5 — Three parallel streams** (spec / substrate / wiring) with reviewer sub-agents per stream. Streams B and C converge at one seam: the workloop hands the LaunchSpec to a substrate-aware handler factory that spawns inside a tmux window rather than via `exec.CommandContext`.

## Open questions for the design pass

- **OQ1 — Pane creation owner.** Does the daemon create the window and hand the pty into the handler subprocess, or does the handler subprocess (`harmonik runner` style) create its own window? PL-028 implies the runner command does it. Affects whether the substrate is invoked from `internal/daemon/` or `internal/lifecycle/` and how the daemon receives the pty handle.
- **OQ2 — Stop-hook vs Wait-return precedence.** When both signals fire (claude exits AND Stop hook arrives via socket), which wins? CHB-020 + CHB-025 specify last-received-wins for `outcome_emitted` dedup, but don't speak to the case where `cmd.Wait()` returns first. Needs a tiebreaker rule (likely: wait briefly for outcome_emitted after Wait-return; if it doesn't arrive within N seconds, derive terminal from exit code).
- **OQ3 — What `claude` flags to pass.** `--session-id <uuid>` is mandated by CHB-008. `--print` is excluded by the interactive requirement. What about `--add-dir`, `--allowed-tools`, `--mcp-server`, `--permission-mode`? Spec what we pass and what we explicitly do NOT, mirroring CHB-007's forbidden-flag list with an "allowed flags" list.
- **OQ4 — Twin under tmux.** Does `harmonik-twin-claude` also run under tmux for substrate parity, or does the twin keep its current `exec.CommandContext` shape for test ergonomics? Default position: twin runs under tmux when invoked via the daemon, retains pipe-mode for unit tests that exec it directly.
- **OQ5 — Heartbeats inside tmux.** `RunHeartbeatLoop` writes timer-driven heartbeats to the daemon. Today it would write to the handler subprocess's stdout pipe. Under tmux, the handler-process role (CHB-018..020) needs a wire. Confirm whether the heartbeat emitter lives in the daemon (with claude as a dumb terminal child) or whether we need a tiny pre-exec wrapper script that emits handler_capabilities / agent_ready / heartbeats before exec'ing into claude.
