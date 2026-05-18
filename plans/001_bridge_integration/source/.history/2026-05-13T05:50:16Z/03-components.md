# 03 — Components

Five components. The first four can land in parallel; component 5 is the gate.

## Component 1 — Spec amendments

**Scope.** Additive amendments to `process-lifecycle.md`, `workspace-manager.md`, `handler-contract.md`, optional touch in `claude-hook-bridge.md`. Pure documentation; no Go code.

**Concrete deliverables.**

- `process-lifecycle.md` — new clauses for direct-tmux substrate (PL-021b: pane creation primitives, PL-021c: pane orphan recovery within the existing PL-006 sweep). Implements `harmonik tmux-start` subcommand contract (PL-028 refinement).
- `workspace-manager.md` — new clause (number assigned at writing time, mirror of WM-002) for deterministic tmux window naming from `(bead_id, phase, iteration_count)`.
- `handler-contract.md` — refinement of §6.1 `Session.Attach()` contract for `agent_type = claude-code`: returns live pty reader, not log tail.
- `claude-hook-bridge.md` — possibly one new clause (CHB-028) on twin parity for tmux substrate. May be unnecessary if the substrate stays at PL level; resolve during writing.

**Interface to other components.**

- Component 2 reads the PL-021b/c clauses to know what to implement.
- Component 4 reads the HC-052 refinement to know what `Attach()` must return.
- All other components require these spec deliverables before code merges.

**Traceability.** Goal 5 (replay determinism), success criterion 8 (spec amendments merged), constraint "All normative requirements live in specs/".

**Acceptance.** Each spec file passes existing `internal/specaudit` and a reviewer sub-agent confirms the amendments are additive (no existing clauses revised). Cross-spec consistency checked.

## Component 2 — `internal/lifecycle/tmux` adapter + `hk tmux-start` subcommand

**Scope.** A new Go package `internal/lifecycle/tmux` that wraps `tmux` shell commands behind a typed interface, plus a `hk tmux-start` CLI subcommand.

**Concrete deliverables.**

- New package `internal/lifecycle/tmux/` with `Adapter` interface and `OSAdapter` impl:
  - `EnsureSession(name string) error` — create session if absent (used by tmux-start).
  - `NewWindowInSession(session, windowName, cwd string, env []string, argv []string) (PtyHandle, error)` — create a window, exec argv inside it, return a handle that owns the pty.
  - `Attach(handle PtyHandle) (io.Reader, error)` — return a live reader from the pane's pty for `Session.Attach()` implementation.
  - `Kill(handle PtyHandle) error` — close the window.
  - `Available() error` — check tmux on PATH at startup; nil if present.
- `EnsureSession` and `NewWindowInSession` cooperate with existing `provenance.TmuxSessionName(...)` / `TmuxSessionPrefix()` and the orphan-sweep code so the create side and the kill side agree on names.
- New subcommand `hk tmux-start [--session-name <name>]`:
  - If `$TMUX` is set, refuse with a friendly error (already inside tmux).
  - Otherwise, exec `tmux new-session -s <name>` with hk inlined as the initial command.
  - Session-name default = `harmonik-<project_hash>`.
- Session-reuse logic: when the daemon starts inside an existing `$TMUX`, the adapter records that session name and creates windows there; when starting fresh-via-tmux-start, the same.

**Interface.**

- Daemon composition root (`daemon.go`) checks `tmux.OSAdapter.Available()` at startup. Refuses to start if tmux is required by config and absent (PL-021a fail-fast).
- Handler.Launch receives an optional `Substrate Substrate` field on `LaunchSpec`; when non-nil, Launch calls `Substrate.NewWindow(...)` instead of `exec.CommandContext` directly.

**Traceability.** Decisions D2 (skip ntm), D3 (session reuse), D4 (naming), success criteria 3 (operator attach), 4 (standalone start), 6 (orphan recovery).

**Acceptance.** Unit tests for `Adapter` (mocked tmux shellouts), an integration test that exercises a real tmux binary in a sandboxed session (skipped on CI without tmux), and a manual verification that `hk tmux-start` produces a usable session.

## Component 3 — Bridge daemon wiring

**Scope.** Plumb the existing bridge pieces into the daemon work loop. Touches `internal/daemon/workloop.go`, `internal/daemon/daemon.go`, and `internal/daemon/reviewloop.go`.

**Concrete deliverables.**

- A new helper `buildClaudeLaunchSpec(ctx, runCtx) (LaunchSpec, error)` invoked from both `workloop.go` single-mode branch and `reviewloop.go` per-phase launches:
  - Calls `handler.MintClaudeSessionID(phase, prior)` and stores result on `Run.context`.
  - Calls `workspace.MaterializeClaudeSettings(workspacePath, sessionLogPath)` for the worktree.
  - Builds `Env` via `handler.ClaudeEnvVars(cfg)`.
  - Builds `Args` with `--session-id <uuid>` (or `--resume <uuid>` for `implementer-resume`).
  - Calls `handler.CheckForbiddenFlags(argv, env)` as a guard.
  - Calls `handler.CheckSettingsLocalJSON(workspacePath)` as the CHB-024 guard.
- Daemon-side emission of `handler_capabilities` → `session_log_location` → `skills_provisioned` → `agent_ready` (CHB-018) via the existing event bus before exec — using `handler.PreExecMessages(...)`.
- After exec, start `handler.RunHeartbeatLoop(...)` as a goroutine bounded by `ctx`.
- On Wait-return, call `handler.MapWaitReturnToTerminalEvent(...)` with the (sessionID, exitCode, waitErr, latestOutcomeFromStore) and emit the terminal event.
- AdapterRegistry forwarded into `handler.NewHandler` (closes the `daemon.go:298` TODO).

**Interface.**

- Reads from Component 2 (`Substrate` interface on LaunchSpec).
- Reads from Component 4 (`HookSessionStore` to fetch latest `outcome_emitted` payload at Wait-return time).

**Traceability.** Goals 1 (single-mode GREEN), 2 (review-loop GREEN), constraint "twin-blindness", success criteria 1 + 2 + 7.

**Acceptance.** Existing daemon tests pass; new tests exercise the buildLaunchSpec helper with twin binary; CHB-018 emission order verified; CHB-024 guard fires on a planted `settings.local.json`.

## Component 4 — Completion path (HookRelayHandler + workloop wait)

**Scope.** Implement the `HookRelayHandler` interface, wire it into `RunSocketListener`, integrate `HookSessionStore` with workloop completion.

**Concrete deliverables.**

- New file `internal/daemon/hookrelayhandler.go` (or similar): concrete `HookRelayHandler` that routes envelopes to `HookSessionStore.RecordMessage(...)` and acks per CHB-026 ordering rules.
- `daemon.Start` instantiates the store, passes the handler into `RunSocketListener` (today `nil`).
- Workloop completion path:
  - Single-mode: today `<-watcher.Done()` then `sess.Wait(ctx)`. New: wait on **either** watcher.Done() OR `HookSessionStore.WaitForOutcome(runID, claudeSessionID)`, whichever fires first. After Wait-return, give the store a brief grace window for a late Stop hook (OQ2 — design pass decides duration).
  - Review-loop: same shape at each phase boundary.
- New timeout on `agent_ready` reception (closes hk-do7te): bail out if `agent_ready` doesn't arrive within N seconds of process start.

**Interface.**

- Reads from Component 2's `Substrate` (no — actually no direct interface; this is parallel).
- Provides `LatestOutcome(runID, claudeSessionID)` and `WaitForOutcome(...)` to Component 3.

**Traceability.** Goal 1 (real Stop-hook completion path), success criterion 1 (Stop hook fires and is observed), constraint "no silent degradation".

**Acceptance.** New tests exercise the socket round-trip with `harmonik-twin-claude` as the message source; concurrent connections serialize per CHB-026; CHB-027 abrupt-EOF handling preserved; agent_ready timeout test added.

## Component 5 — End-to-end smoke (the GREEN gate)

**Scope.** Re-run the dogfood smoke against real `claude` with components 2–4 landed; verify GREEN per success criterion 1; if stretch-success, run the review-loop variant for criterion 2.

**Concrete deliverables.**

- Reproducible procedure documented in `docs/dogfood-smoke-run-<date>-bridge-green.md`.
- Updated `hk-w5vra.7` bead with result; if GREEN, close it and the parent `hk-w5vra` and the epic `hk-1n0cw`.
- Updated `HANDOFF.md` reflecting the new state.
- If RED: file follow-up beads under `hk-w5vra` and re-decompose.

**Interface.** Depends on Components 1, 2, 3, 4 merged to main.

**Traceability.** Goals 1, 2, 3. Success criteria 1, 2, 3, 5, 7.

**Acceptance.** Smoke procedure produces all of: (a) `.claude/settings.json` materialized, (b) tmux window created with expected name, (c) claude exec'd inside, (d) marker.txt mutated and committed by claude, (e) Stop hook arrives at daemon socket, (f) `outcome_emitted` recorded in store, (g) bead closed with reason matching the verdict.

## Dependency DAG

```
   Component 1 (Spec amendments)
       │
       ├─→ Component 2 (Tmux substrate)
       │       │
       ├─→ Component 3 (Bridge wiring) ─→ Component 5 (Smoke)
       │       │                              ▲
       ├─→ Component 4 (Completion path) ─────┘
```

Components 2, 3, 4 can develop in parallel under stubbed interfaces once Component 1's clauses are in flight (don't need to wait for final merge — the design is stable enough). Component 5 gates on all four.

## Goals → components traceability

| Goal | Components |
|---|---|
| G1 — Single-mode smoke GREEN | C1, C2, C3, C4, C5 |
| G2 — Operator attach to live pane | C1, C2 |
| G3 — Substrate reusable for non-claude | C1, C2 |
| G4 — Additive, no CHB revisions | C1 |
| G5 — Replay determinism | C1, C2 |

| Success criterion | Components |
|---|---|
| SC1 — Real-claude smoke GREEN | C5 |
| SC2 — Review-loop GREEN | C3 (review-loop path), C5 |
| SC3 — Operator can attach | C2 |
| SC4 — Standalone tmux-start | C2 |
| SC5 — Naming determinism | C1, C2 |
| SC6 — Orphan recovery | C2 (relies on existing sweep) |
| SC7 — Twin parity | C3 (twin-blind), C4 |
| SC8 — Spec amendments merged | C1 |

No orphan requirements.
