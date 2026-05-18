# Component 3 + 4 — Research Findings

## AdapterRegistry: what dispatch the registry actually performs

`internal/handlercontract/adapterregistry_hc012.go` defines `AdapterRegistry` as a sealed map keyed by `core.AgentType` (only `claude-code` registered today, see `daemon.go:299-310`). `Register(agentType, adapter)` adds an entry; `ForAgent` returns the registered `Adapter` and seals the registry.

The `Adapter` surface (`internal/handlercontract/adapter.go`) is intentionally narrow:

```go
type Adapter interface {
    DetectReady(event core.EventEnvelope) bool
    DetectRateLimit(event core.EventEnvelope) (bool, time.Duration)
    CleanExitSequence(ctx, session) error
    RotateAccount(ctx) error
}
```

This is a **watcher-callback surface, not a launch-strategy surface.** The registry does NOT carry `MintClaudeSessionID`, `ClaudeEnvVars`, or `PreExecMessages`; those live in `internal/handler/claudehandler_chb006_024.go` as package-level pure functions. So "is AdapterRegistry just pass-through data?" — it's a **per-agent callback dispatch table** consumed by the Watcher's hot path and by cancellation. It is NOT the seam through which env-var construction or flag-building gets agent-type-keyed.

Implication: the `buildClaudeLaunchSpec` helper proposed for Component 3 should call the `handler` package's claude-specific pure functions directly today. The "AdapterRegistry forwarded into handler.NewHandler" TODO at daemon.go:297 is a *latent* seam — once a second agent type lands, the per-agent launch-prep code will pivot to a registry of `LaunchStrategy` adapters; until then forwarding the registry into `NewHandler` is shape-only plumbing.

## HookSessionStore API surface

`internal/daemon/hookrelay_chb025.go` declares `hookSessionStore` (unexported). Public method set:

- `newHookSessionStore() *hookSessionStore` — constructor.
- `RegisterHookSession(runID, claudeSessionID)` — idempotent open.
- `CloseHookSession(runID, claudeSessionID)` — closes the window.
- `LatestOutcome(runID, claudeSessionID) *json.RawMessage` — last-received-wins read.
- `HandleHookRelay(env) hookRelayAckMsg` — satisfies `HookRelayHandler` interface in `socket.go:21`.

Missing today: a `WaitForOutcome(ctx, runID, claudeSessionID) (json.RawMessage, error)` method that blocks until an outcome arrives or ctx is cancelled. Implementation requires per-session `chan struct{}` notify channels signalled by `updateOutcome`. This is additive.

The store type is unexported. Component 3/4 wiring requires either exporting it as `HookSessionStore`, or keeping it package-local and wiring through `daemon.go` within the same package (preferred — both the workloop and the hookrelay handler live in `internal/daemon/`, no export needed).

## `claude --help` — relevant flags

Per Anthropic's docs and the existing CHB-007 deny-list:

- `--session-id <uuid>` — start a session with specified UUID (CHB-008).
- `--resume <uuid>` — resume a prior session (CHB-008 implementer-resume).
- `--print` / `-p` — non-interactive; INCOMPATIBLE with interactive tmux substrate.
- `--add-dir <path>` — workspace boundary is `cmd.Dir`; not needed. **MVH: not passed.**
- `--allowed-tools` / `--disallowed-tools` — settings.json carries policy; CLI overrides would shadow. **MVH: not passed.**
- `--mcp-server`, `--mcp-config` — out of scope. **Not passed.**
- `--permission-mode <mode>` — same shadowing concern. **MVH: not passed.**
- `--fork-session`, `--bare`, `--no-session-persistence` — already on CHB-007 deny-list.

Allow-list at MVH: exactly `{--session-id, --resume}`.

## Socket-concurrency constraints from CHB-026

CHB-026 picks **Rule (C): per-connection FIFO, across-connection unordered.** `RunSocketListener` already conforms — each accepted connection dispatched to its own goroutine. The `hookSessionStore` uses a single mutex; concurrent relays serialize at the mutex.

CHB-027 (orphan-connection) handled in `handleSocketConn` — zero complete lines yields `bad_envelope`.

## Risks

1. **WaitForOutcome race.** If a Stop hook arrives BEFORE the workloop calls `RegisterHookSession`, the envelope is rejected `unknown_session`. Fix: register the session *before* `h.Launch` returns control.
2. **`--resume` semantics.** Claude's `--resume <uuid>` requires the session-state file present in `~/.claude/projects/<project-hash>/<uuid>.jsonl`. Cross-worktree resume may fail. Needs smoke test; fall back to `--session-id` if broken.
3. **AdapterRegistry shape pre-commitment.** Forwarding the registry into `handler.NewHandler` today introduces an unused field. Tag with a TODO bead so the seam is observably planned.
4. **Heartbeat owner ambiguity.** Resolved in design (OQ5).
5. **Pre-existing flakes** (HANDOFF v35) in `TestT4_ReopenThenRedispatch` etc. independent but will hit CI; do not let them mask new regressions.
