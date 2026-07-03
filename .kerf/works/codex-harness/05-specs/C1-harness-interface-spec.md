# C1 — Harness seam (interface + AgentTypeCodex + registry) — change spec

## Requirements (from 03-components.md)
R1.1 `Harness` interface (LaunchSpec/Seed/Retask/Teardown/DetectReady/SessionIDPolicy/Completion).
R1.2 `core.AgentTypeCodex` + registry resolution. R1.3 claude reimplemented as `ClaudeHarness`, no
behavior delta, shared scaffolding stays outside per-harness `LaunchSpec`. R1.4 plugs into the
existing `deps.launchSpecBuilder` seam — no parallel dispatch path.

## Research summary
The harness-VARYING surface is exactly the five operations claude already performs
(`04-research/current-harness/findings.md` Part E). The cleanest insertion point is the existing
swappable `deps.launchSpecBuilder` function field (`dot_cascade.go:521-523`) plus the
`AdapterRegistry` keyed by `core.AgentType` (`adapterregistry_hc012.go:67`). NB anchor corrections
(change-spec-review): `ClaudeCodeAdapter.DetectReady` is at `internal/handler/adapter_claudecode.go:99`
(impl lives in `internal/handler/`, not `internal/handlercontract/`); `MintClaudeSessionID` is at
`claudehandler_chb006_024.go:119`; the `AgentType` enum is `internal/core/agenttype.go:14-18`. Two
harness properties
must be first-class because codex differs structurally: **SessionIDPolicy** (claude=Minted UUIDv7;
codex=Captured thread_id) and **Completion** (claude=EventStreamThenQuit; codex=ProcessExit) — the
latter governs whether the shared heartbeat-staleness kill path runs (decompose-review B1/B2).

## Approach
Define `Harness` next to the existing adapter registry. Keep the substrate, worktree, and
commit-detection layers untouched (they take opaque argv). Extract the claude-specific body of
`buildClaudeLaunchSpec` into a `ClaudeHarness` that satisfies the interface; leave shared scaffolding
(worktree-trust seeding, `agent-task.md` write, pre-exec messages) in the shared caller so codex
reuses it. The cascade resolves a harness (C4) and obtains its `LaunchSpec` builder + adapter from
the registry — replacing the always-claude `deps.launchSpecBuilder` default with a
harness-dispatched lookup. **No behavior change for claude**: `ClaudeHarness` produces byte-identical
argv/env/cwd to today.

```go
// internal/handlercontract/harness.go  (new)
type CompletionMode int
const ( CompletionEventStreamThenQuit CompletionMode = iota; CompletionProcessExit )
type SessionIDPolicy int
const ( SessionIDMinted SessionIDPolicy = iota; SessionIDCaptured )

type Harness interface {
    AgentType() core.AgentType
    LaunchSpec(rc RunCtx) (LaunchSpec, error)   // argv+env(cred-stripped)+cwd for one spawn
    Seed(sess Session, rc RunCtx) error          // deliver first-turn task
    Retask(sess Session, feedback string, rc RunCtx) error  // iter>=2 feedback
    Teardown(sess Session) error                 // claude:/quit+kill; codex:no-op
    DetectReady(ev Event) bool                   // map harness event -> agent_ready
    SessionIDPolicy() SessionIDPolicy
    Completion() CompletionMode
}
```

## Files & changes
- **NEW** `internal/handlercontract/harness.go` — the interface + the two enums.
- **NEW** `internal/handlercontract/harness_claude.go` — `ClaudeHarness` wrapping the existing claude
  launch/seed/teardown logic; thin delegation to current functions (no logic rewrite).
- **MODIFY** `internal/core/` agent-type enum — add `AgentTypeCodex` (alongside `AgentTypeClaudeCode`).
- **MODIFY** `internal/handlercontract/adapterregistry_hc012.go` — register `ClaudeHarness`; reserve
  `ForAgent(AgentTypeCodex)`.
- **MODIFY** `internal/daemon/dot_cascade.go:499-525` — `deps.launchSpecBuilder` becomes a lookup off
  the resolved harness (C4 wires the resolver; here just make the field harness-addressable).
- **MODIFY** `internal/daemon/claudelaunchspec.go` — extract the claude-specific body into
  `ClaudeHarness.LaunchSpec`; leave shared scaffolding (`MaterializeClaudeSettings`,
  `EnsureWorktreeTrust`, `WriteAgentTask`, `PreExecMessages`) callable by the shared path.

## Acceptance criteria
- AC1.1 `go build ./...` and `go vet ./...` pass with the new interface + `AgentTypeCodex`.
- AC1.2 A unit test asserts `ClaudeHarness.LaunchSpec` produces the **pure `(argv, env, cwd)`**
  return byte-identical to the pre-refactor `buildClaudeLaunchSpec` for a fixed `RunCtx` (golden
  test) — scoped to the return value, NOT the file-materialization side effects (those move to the
  shared caller; see AC1.6).
- AC1.6 A test asserts the **shared scaffolding side effects** still fire from the caller for a claude
  run — `MaterializeClaudeSettings` (`.claude/settings.json`), `EnsureWorktreeTrust` (`~/.claude.json`),
  `WriteAgentTask` (`.harmonik/agent-task.md`), `PreExecMessages` — i.e. side-effect parity with
  pre-refactor behavior, proving the extraction left scaffolding outside `LaunchSpec` (R1.3).
- AC1.3 `ClaudeHarness.Completion() == CompletionEventStreamThenQuit` and
  `SessionIDPolicy() == SessionIDMinted`.
- AC1.4 The daemon smoke (a real claude bead end-to-end) still passes with no behavior delta.
- AC1.5 No new dispatch path: the cascade reaches claude through the same
  `deps.launchSpecBuilder`/registry seam (grep shows one dispatch site, not an `if codex` branch).

## Verification
```
go build ./... && go vet ./...
go test ./internal/handlercontract/... ./internal/daemon/... -run 'Harness|LaunchSpec|Golden'
# live smoke (per HANDOFF discipline — tmux/session/spawn code must be live-smoked):
#   submit one trivial claude bead to a scratch queue; confirm reviewer_launched + Refs: commit
```

## Error handling / edge cases
- A harness whose `LaunchSpec` errors → run fails closed with the harness name in the event (no
  silent fallback to claude).
- `ForAgent(unknown)` → typed error, run fails closed.

## Migration / back-compat
Pure refactor for claude; `AgentTypeCodex` is registered but unreachable until C4 wires selection.
N-1 safe by construction (no caller selects codex yet).
