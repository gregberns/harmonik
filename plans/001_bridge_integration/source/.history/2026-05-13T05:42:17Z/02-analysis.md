# 02 — Analysis

Synthesized from three read-only investigator passes against `main` at commit `9ad4969`.

## Affected areas

| Subsystem | Path | Role |
|---|---|---|
| Daemon work loop | `internal/daemon/workloop.go` | The integration gap. Builds bare `LaunchSpec`, awaits stdout-EOF. |
| Daemon composition root | `internal/daemon/daemon.go` | AdapterRegistry constructed and sealed but not forwarded to handler. |
| Daemon Unix socket | `internal/daemon/socket.go` | Accepts hook-relay envelopes but `HookRelayHandler` arg always `nil`. |
| Hook-session dedup store | `internal/daemon/hookrelay_chb025.go` | CHB-025 last-received-wins store exists; not instantiated. |
| Handler (Go-side) | `internal/handler/handler.go`, `claudehandler_chb006_024.go` | Generic Launch + claude-specific pure functions; no driver wires them. |
| Workspace settings.json | `internal/workspace/claudesettings_wm040a.go` | `MaterializeClaudeSettings` complete, called nowhere. |
| Hook-relay subcommand | `internal/hookrelay/`, `cmd/harmonik` | Wired (cmd/harmonik/main.go:64–74). |
| Twin | `cmd/harmonik-twin-claude/` | Exercises wire format end-to-end. |
| Process-lifecycle (orphans) | `internal/lifecycle/orphansweep.go`, `provenance.go` | Tmux **kill** primitives + naming functions exist; **create** primitives absent. |
| Specs | `specs/claude-hook-bridge.md`, `specs/handler-contract.md`, `specs/process-lifecycle.md`, `specs/workspace-manager.md`, `specs/event-model.md` | CHB v0.4 complete; substrate amendments missing. |

## What's already there

### Bridge pieces (callable, untested in integration)

`internal/handler/claudehandler_chb006_024.go` exports:

- `CheckForbiddenFlags(argv, env) error` — line 74 — CHB-007 guard.
- `MintClaudeSessionID(phase, priorID) ClaudeSessionIDResult` — line 117 — CHB-008/009.
- `ClaudeEnvVars(cfg ClaudeEnvConfig) []string` — line 186 — CHB-006 env-var set in `KEY=VALUE` form.
- `CheckSettingsLocalJSON(workspacePath) error` — line 259 — CHB-024.
- `PreExecMessages(spec, sessionID, logPath, workspacePath, ...) ([]json.RawMessage, error)` — line 315 — CHB-018 ordering.
- `MapWaitReturnToTerminalEvent(sessionID, exitCode, waitErr, outcome) TerminalEventPayload` — line 453 — CHB-020 three-branch logic.
- `RunHeartbeatLoop(ctx, sessionID, interval, done, emit)` — line 516 — CHB-019 timer-driven heartbeats.
- `DeriveCIaudeTranscriptPath(workspacePath, claudeSessionID) string` — line 545 — CHB-018 session_log_location derivation.

`internal/workspace/claudesettings_wm040a.go`:

- `ClaudeSettingsPath(workspacePath) string` — line 16.
- `MaterializeClaudeSettings(workspacePath, sessionLogPath) error` — line 116 — CHB-001..005 atomic write.
- `ClaudeSettingsWorktreeGitignoreLine` — line 23.

`internal/daemon/socket.go`:

- `RunSocketListener(ctx, sockPath, h RequestHandler, hr HookRelayHandler) error` — line 124 — accepts both SocketRequest (op=...) and hook-relay envelopes (type=...); dispatches the latter only when `hr` is non-nil.

`internal/daemon/hookrelay_chb025.go`:

- `HookSessionStore` — CHB-025 per-session accumulator for `outcome_emitted` last-received-wins.

`internal/lifecycle/orphansweep.go`:

- `TmuxSessionLister` interface + `OSTmuxSessionLister` (line 29–68) — `tmux list-sessions`.
- `TmuxSessionKiller` interface + `OSTmuxSessionKiller` (line 38–82) — `tmux kill-session`.
- `SweepOrphanTmuxSessions(ctx, lister, killer, prefix, eventBus)` (line 110–177) — prefix-scoped sweep with 100ms/2s polling per PL-006.

`internal/lifecycle/provenance.go`:

- `TmuxSessionPrefix() string` — line 99–108 — returns `harmonik-<project_hash>-`.
- `TmuxSessionName(sessionName) string` — line 110–116 — returns `harmonik-<project_hash>-<sessionName>`.

`internal/core/daemonevents_hqwn59.go`:

- `DaemonOrphanSweepCompletedPayload.TmuxSessionsKilled` (line 730–732) — event field already wired.

### Integration callers (where bridge pieces *should* be called from)

- `internal/daemon/workloop.go:486–493` (single-mode): builds plain `LaunchSpec{Binary, Args, Env, WorkDir, Role}` with no env-var injection, no settings materialization, no session-id minting.
- `internal/daemon/workloop.go:504–510` (single-mode completion): `<-watcher.Done()` then `sess.Wait(ctx)`. No socket-side path for `outcome_emitted`.
- `internal/daemon/reviewloop.go:176` (review-loop): already uses `SessionIDInterceptor` (a stdout-side wrapper). Same integration gap as single-mode for env vars, settings, and outcome-side completion.
- `internal/daemon/daemon.go:285–310` (composition root): builds `AdapterRegistry`, registers ClaudeCodeAdapter, seals — but does not pass registry into `handler.NewHandler`. Comment at line 297–298: *"the registry is not currently forwarded to the handler (post-MVH wiring adds that seam)."*
- `internal/daemon/socket.go:RunSocketListener` callers in daemon.Start: pass `hr = nil` so all hook-relay traffic is rejected as `bad_envelope`.

## Patterns and conventions in use

- **Errors as wrapped values** with structural-error sentinels (`ErrStructural`, `errors.Is(err, handlercontract.ErrCanceled)`). Pattern visible at `claudehandler_chb006_024.go:335,350,362,373`.
- **EmitWithRunID for event bus**: events tagged with `run_id` envelope; see `eventbus.Filter(path, runID)`. Bridge wiring must use the same pattern (HC-005 fix `0534c0b` is the model).
- **CHB-021 twin parity invariant** (specs/claude-hook-bridge.md §4.8): daemon must be twin-blind. Any code path that special-cases `Binary == "claude"` violates this.
- **Subsystem isolation**: depguard is configured at the package level; cross-subsystem imports are restricted. New code in `internal/lifecycle/tmux` must respect the matrix.
- **Composition root in daemon.go**: all subsystem wiring happens in `daemon.Start`. New tmux substrate plumbing belongs there, not constructed lazily inside the workloop.
- **JSONL/intent-log discipline**: `BrAdapter` writes intent logs before transitioning bead state. New code must not bypass it.
- **Build-practices §Git hygiene**: gofmt-clean, lint-clean, `go test -race ./...` per CI; pre-existing flakes already triaged in HANDOFF.

## Constraints to preserve

- **CHB-001..027 unchanged.** Existing pieces are correct; amendments are additive.
- **Twin-blindness (CHB-022).** No daemon code may branch on `Binary == "claude"` to invoke the bridge. Mechanism = adapter-registry dispatch keyed on `agent_type`, not binary name.
- **Replay determinism.** Worktree path is deterministic from `run_id` (WM-002); new tmux window name must be similarly deterministic from `(bead_id, phase, iteration_count)`.
- **No silent degradation.** Per PL-021a, missing tmux is Cat-0 fail-fast (exit 22). The substrate adapter must check tmux availability at startup, not at spawn time, so the daemon refuses to start without tmux when required.
- **Foreground-binary model.** Locked decision 2026-05-08. The daemon runs in the foreground; orchestration via signals. No daemonization in this initiative.

## Spec landscape

| Spec | Section | Current state | What this initiative touches |
|---|---|---|---|
| `claude-hook-bridge.md` | §4.1–4.10 (CHB-001..027) | v0.4, complete | No revisions to existing clauses. Possibly +1 clause for tmux scope. |
| `handler-contract.md` | §6.1 Session.Attach | Method named, contract for return value vague | Refine: Attach for claude-code returns live pty reader. |
| `process-lifecycle.md` | §4.1 PL-006 / PL-006a (orphan sweep) | Implemented (kill side) | No revision; the create-side amendments slot in adjacent. |
| `process-lifecycle.md` | §4.4 PL-021 (ntm adapter) | Defines ntm as substrate | Skip ntm; new clauses for direct-tmux adapter alternative. |
| `process-lifecycle.md` | §4.10 PL-028 (`harmonik runner`) | Defines four-step lifecycle requiring tmux | Implement (with tmux session-reuse if `$TMUX` set; new `hk tmux-start` subcommand). |
| `workspace-manager.md` | §WM-002 (worktree path) | Deterministic naming | Add parallel WM clause for tmux window naming. |
| `event-model.md` | §8.5 workspace events | No pane lifecycle events | Decide: internal-only or emit events. |

## Recent git activity in affected areas

- `f285d2b` (hk-w5vra.11) — CHB-025 daemon-side dedup. Edits `internal/daemon/hookrelay_chb025.go`. Confirms HookSessionStore exists.
- `ea4464f` (hk-w5vra.5) — claude handler-process responsibilities. Edits `internal/handler/claudehandler_chb006_024.go`. Adds the exported pure functions.
- `1b88110` (hk-w5vra.6) — daemon persists claude_session_id to Run.context before claude exec (CHB-023). Touches workloop and run-context.
- `fb1bb8c` (hk-w5vra.4) — workspace materializer (CHB-001..005). Edits `internal/workspace/claudesettings_wm040a.go`.
- `f44f8fe` (hk-w5vra.3) — hook-relay subcommand (CHB-010..017).
- `0534c0b` (hk-keb6o) — HC-005 LaunchSpec to stdin. Establishes the stdin-delivery pattern.
- `c4a1eef`, `001d121`, `4991ee6` — recent gitignore activity around worktrees and .claire scratch.

No in-flight branches touching these files (other than the now-deleted `spec/claude-hook-bridge`).

## Code health / tech debt relevant to this work

- **harmonik-twin-claude ~80% duplicates harmonik-twin-generic** (HANDOFF v35 §Tech debt). Will bite when twin needs to learn tmux substrate. Likely worth refactoring to an internal package as part of Stream B substrate work; if so, do it after the bridge is GREEN.
- **Pre-existing test flakes** in `TestT4_ReopenThenRedispatch`, `TestWorkLoop_FailedHandlerReopensBead`, `TestT5_RedactionHC031ByFieldName`. Don't try to fix as part of this initiative; flag if they get worse.
- **`binary_commit_hash` always "unknown"** (hk-mz0x4) — ldflags injection not wired. Adjacent to but separate from this initiative.
- **`HARMONIK_PROJECT_HASH` not injected** (hk-nvrvp) — `provenance.go` exists but is not called from the env-construction path. This initiative's env-var injection naturally fixes it; close hk-nvrvp when the wiring lands.
- **No `agent_ready` timeout** (hk-do7te) — workloop blocks indefinitely on `<-watcher.Done()`. The new completion path (with socket-side wait) is the natural place to add a timeout. Close hk-do7te when timeout lands.
