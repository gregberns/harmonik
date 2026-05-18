# Component 3 + 4 — Design

## 0. Scope recap

Land bridge wiring across single-mode (`workloop.go:486–541`) and review-loop (`reviewloop.go:155–250+`), plus the daemon-side socket completion path (`daemon.go:285–310`, `socket.go:124`). Component 2 (tmux substrate) is parallel; this design treats `LaunchSpec.Substrate` as a typed-but-opaque field that may be nil at integration time and ignored by the handler when nil (degrades to today's `exec.CommandContext` shape — Twin still runs).

## 1. `buildClaudeLaunchSpec` helper

Lives in `internal/daemon/claudelaunchspec.go`. Pure, no I/O beyond `workspace.MaterializeClaudeSettings` and `handler.CheckSettingsLocalJSON`.

```go
type claudeRunCtx struct {
    runID            core.RunID
    beadID           string
    workspacePath    string  // worktree path
    daemonSocket     string  // <ProjectDir>/.harmonik/daemon.sock
    workflowMode     core.WorkflowMode
    phase            handlercontract.ReviewLoopPhase  // "" for single
    iterationCount   int
    priorClaudeSessID *string                       // nil except implementer-resume
    handlerBinary    string                          // from Config (claude or twin)
    baseEnv          []string                        // Config.HandlerEnv + provenance
}

func buildClaudeLaunchSpec(
    ctx context.Context,
    rc claudeRunCtx,
) (handler.LaunchSpec, claudeRunArtifacts, error)
```

`claudeRunArtifacts` carries values the workloop needs after Launch — the `claudeSessionID`, the `sessionLogPath`, the `handlerSessionID`, the rendered PreExecMessages bytes, and a `Substrate` reference.

Body:

1. `mintRes, err := handler.MintClaudeSessionID(string(rc.phase), rc.priorClaudeSessID)` — bail on error.
2. `sessionLogPath := handler.DeriveCIaudeTranscriptPath(rc.workspacePath, mintRes.ClaudeSessionID)`.
3. `if err := workspace.MaterializeClaudeSettings(rc.workspacePath, sessionLogPath); err != nil { return ... }` — CHB-001..005 atomic write.
4. `if err := handler.CheckSettingsLocalJSON(rc.workspacePath); err != nil { return ... }` — CHB-024 guard.
5. Build `ClaudeEnvConfig`:
   - RunID, DaemonSocket, WorkspacePath, ClaudeSessionID set.
   - HandlerSessionID = fresh UUIDv7.
   - WorkflowID/NodeID derived from the bead (MVH: `bead/<beadID>`).
   - WorkflowMode, Phase, IterationCount, BeadID set when non-empty.
   - BaseEnv = `lifecycle.AppendProvenanceEnv(rc.baseEnv)` — fixes hk-nvrvp (HARMONIK_PROJECT_HASH injection).
6. `env := handler.ClaudeEnvVars(cfg)`.
7. `args := []string{"--session-id", mintRes.ClaudeSessionID}` (or `--resume <uuid>`). Append `cfg.HandlerArgs` after allow-listed flags.
8. `if err := handler.CheckForbiddenFlags(args, env); err != nil { return ... }` — CHB-007 guard.
9. Render PreExecMessages: `msgs, err := handler.PreExecMessages(runID, handlerSessID, nodeID, claudeSessID, sessionLogPath, nil)` — `skills = nil` at MVH.
10. Return `handler.LaunchSpec{Binary, Args, Env, WorkDir, Role, Substrate}`.

The helper is twin-blind: same spec for `claude` and `harmonik-twin-claude`. `Substrate` is opaque — `nil` falls back to `exec.CommandContext`.

## 2. Workloop completion path rewrite (`workloop.go` single-mode)

Replace lines 486–541 with:

```go
spec, artifacts, specErr := buildClaudeLaunchSpec(ctx, runCtxFromBead(...))
if specErr != nil { reopen with reason; emit run_completed=false; return }

deps.hookStore.RegisterHookSession(runID.String(), artifacts.claudeSessionID)
defer deps.hookStore.CloseHookSession(runID.String(), artifacts.claudeSessionID)

// Emit pre-exec messages on the bus BEFORE Launch (CHB-018 ordering).
for _, m := range artifacts.preExecMsgs { emitProgressEnvelope(ctx, deps.bus, runID, m) }

sess, watcher, launchErr := deps.h.Launch(ctx, spec)
if launchErr != nil { reopen; emit; return }

// Start CHB-019 heartbeats. Daemon-owned (OQ5 resolution).
hbDone := make(chan struct{})
go handler.RunHeartbeatLoop(ctx, artifacts.handlerSessID,
    handler.HeartbeatInterval, hbDone,
    daemonHeartbeatEmitter(deps.bus, runID))
defer close(hbDone)

// agent_ready timeout: fail-fast if agent_ready never observed.
readyCtx, readyCancel := context.WithTimeout(ctx, deps.agentReadyTimeout)
defer readyCancel()
readyErr := waitAgentReady(readyCtx, watcher, deps.adapter)
if readyErr != nil { kill+reap; reopen with reason; emit; return }

// Wait either watcher.Done() (handler exit) or socket-side outcome.
outcome, exitInfo := waitWithSocketGrace(ctx, deps, watcher, sess,
    runID.String(), artifacts.claudeSessionID)

term := handler.MapWaitReturnToTerminalEvent(
    artifacts.handlerSessID, exitInfo.exitCode, exitInfo.waitErr, outcome,
)
emitTerminalEvent(ctx, deps.bus, runID, term)

// Close / reopen decision per term.Type.
```

### `waitWithSocketGrace` pseudocode

```go
const stopHookGrace = 3 * time.Second  // OQ2 resolution

func waitWithSocketGrace(ctx, deps, watcher, sess, runID, claudeSessID) (
    *handler.ExportedOutcomeEmittedPayload, exitInfo,
) {
    select {
    case <-watcher.Done():
    case <-ctx.Done():
        sess.Kill(ctx)
        <-watcher.Done()
    }
    waitErr := sess.Wait(ctx)
    exit := sess.Outcome().ExitCode

    if outcome := readOutcomeFromStore(deps.hookStore, runID, claudeSessID); outcome != nil {
        return outcome, exitInfo{exit, waitErr}
    }
    waitCtx, cancel := context.WithTimeout(context.Background(), stopHookGrace)
    defer cancel()
    if outcome := deps.hookStore.WaitForOutcome(waitCtx, runID, claudeSessID); outcome != nil {
        return outcome, exitInfo{exit, waitErr}
    }
    return nil, exitInfo{exit, waitErr}  // CHB-020 branch 3
}
```

### OQ2 resolution — Stop-hook vs Wait-return precedence

**Stop hook wins.** When both signals arrive, `outcome_emitted` from the socket carries the semantic verdict; exit code is a coarse fallback for CHB-020 branch 3 only. Grace window: **3 seconds** after `cmd.Wait()` returns. Rationale: the Stop hook fires inside claude's shutdown — claude must exit before the hook finishes running and the relay completes the socket write. 3 seconds covers hook execution + relay process startup + socket round-trip with margin; longer windows risk operator-perceived hangs on crash cases where no hook will arrive.

## 3. Review-loop wiring (`reviewloop.go`)

Per-iteration: replace the bare `LaunchSpec` construction (lines 155–161 + implementer-resume variant + reviewer launch) with `buildClaudeLaunchSpec`. Phase string is `implementer-initial` (iter 1), `implementer-resume` (iter ≥ 2), or `reviewer`. The CHB-023 `StdoutWrapper` interceptor stays as-is on iter 1; helper attaches PreExecMessages emission and hookSessionStore registration around each phase boundary.

Reviewer phase MUST mint a fresh `claudeSessionID` per CHB-009: pass `priorClaudeSessID = nil` even when implementer's session ID is known.

Each phase Register/Close-s its own `(runID, reviewerClaudeSessID)` pair so late hooks from a closed reviewer don't bleed into the next iteration's implementer-resume.

## 4. Heartbeat ownership (OQ5)

**Decision: daemon emits heartbeats on claude's behalf.**

Rationale: building a `harmonik claude-handler` shim binary is the spec-purist choice (CHB-019 says "the handler-process emits heartbeats"), but it costs a fork+exec hop, complicates the operator's pane (heartbeats land in the shim's stderr, not claude's TUI), and duplicates the goroutine machinery `handler.RunHeartbeatLoop` already encapsulates. The daemon already owns the event bus and the per-run goroutine; emitting `agent_heartbeat{phase:"reasoning"}` from the daemon goroutine is functionally identical from the subscriber's perspective.

CHB-019 does NOT bind heartbeat-emission to a *separate* handler-process — it requires the `agent_heartbeat` event be observed every 300 s while claude is alive; the daemon-as-emitter satisfies that.

Spec amendment to HC: "for `agent_type=claude-code`, the daemon MAY emit `agent_heartbeat` on the handler-process's behalf". Shim binary becomes a clean post-MVH refactor.

## 5. `agent_ready` timeout — closes hk-do7te

Default: **30 seconds.** Configured as `Config.AgentReadyTimeout time.Duration` with zero-value fallback.

Mechanism: `waitAgentReady` consults the Adapter's `DetectReady` callback via a small observer goroutine that listens to the event bus filtered by run_id and closes a `ready` channel on first DetectReady=true.

On timeout: kill the session, reap, reopen the bead with reason `agent_ready_timeout`, emit `agent_failed{class=structural, sub_reason=agent_ready_timeout}`.

## 6. AdapterRegistry forwarding shape

Change `handler.NewHandler` signature:

```go
func NewHandler(
    publisher handlercontract.EventEmitter,
    deadLetter handlercontract.WatcherDeadLetterSink,
    registry *handlercontract.AdapterRegistry,  // NEW
) Handler
```

`registry` stored on `handler` struct; today's `Launch` does NOT consult it (no behaviour change). Goal at this pass: close the TODO at `daemon.go:298`. New unit test: passing `nil` registry panics with same defect-pattern as existing `publisher == nil` guard.

## 7. `HookRelayHandler` impl

`hookSessionStore` *already* implements `HookRelayHandler`. Wiring: in `daemon.Start`, after the orphan-sweep step, construct the store, bind the socket listener:

```go
hookStore := newHookSessionStore()
go func() {
    if err := RunSocketListener(ctx, sockPath, requestHandler, hookStore); err != nil {
        // log; non-fatal at MVH
    }
}()
```

`sockPath = filepath.Join(cfg.ProjectDir, ".harmonik", "daemon.sock")`. Forward `hookStore` into the work-loop deps struct so `waitWithSocketGrace` can consult it.

## OQ3 resolution — allowed claude flags (re-stated)

Allow-list: `{--session-id <uuid>, --resume <uuid>}`. Deny-list (unchanged from CHB-007): `{--fork-session, --bare, --no-session-persistence}` + env `CLAUDE_CODE_SKIP_PROMPT_HISTORY`. Not-passed-at-MVH (each gets a follow-up bead): `--add-dir`, `--allowed-tools`, `--mcp-server`, `--permission-mode`. Rationale: policy lives in worktree-materialized `.claude/settings.json`; CLI flags would shadow it and defeat determinism for replay.
