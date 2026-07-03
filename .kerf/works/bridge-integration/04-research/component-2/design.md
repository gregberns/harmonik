# Component 2 — Design: `internal/lifecycle/tmux` + `hk tmux-start`

Implements locked decisions D2/D3/D4 and resolves open questions OQ1 (pane creation owner) and substrate session-name reconciliation.

## 1. Resolutions to open questions

### OQ1 — Pane creation owner

**Resolution: the daemon creates the tmux window; the handler subprocess inherits its pty by being spawned BY tmux via `tmux new-window -- <argv>`.**

The daemon (composition root) calls `tmux.Adapter.NewWindowInSession(...)`, which issues `tmux new-window -d -t <session>: -n <window> -c <cwd> -e KEY=VAL... -- <argv>`. The tmux server forks the child with a pty as stdio; the daemon never holds a pty fd. The "handle" returned to the caller is a value-typed `WindowHandle{Session, Name, PID}` — `PID` is retrieved via `tmux display-message -p -t <session>:<window> '#{pane_pid}'` immediately after `new-window` returns.

Rationale:
- Approach (a) from findings — tmux owns the pty, daemon owns nothing pty-ish.
- The bridge wire (NDJSON, hook envelopes) flows through the daemon Unix socket (`hook-relay` subcommand) and the existing `cmd.Wait()` exit code path, neither of which requires a stdout pipe from the child.
- `handler.Session` (`internal/handler/session.go`) — which today holds `*exec.Cmd` and stdin/stdout pipes — is bypassed entirely for claude under tmux. A new `tmux.WindowHandle` type plays the equivalent role, exposing `Kill(ctx)`, `Wait(ctx)`, and `Outcome()`.
- The handler subprocess does NOT create its own window. That preserves the existing daemon-as-composition-root pattern (PL-014 spawn-site owner, PL-006a provenance discipline) and avoids the hand-off-pty-FD complexity that approach (b) would force.

### PTY library vs `tmux pipe-pane`

**Resolution: neither. The daemon never reads pane output.** All bridge messages flow through the Unix socket via `harmonik hook-relay`. `creack/pty` is not introduced. `tmux pipe-pane` is not used. The pty exists exclusively for operator ergonomics — `tmux attach`, `select-window`, type into the live claude session.

This means `handler-contract.md §6.1 Session.Attach()` for `agent_type = claude-code` returns NOT a Go `io.Reader` but a *resource locator* — the tmux session+window name string — that an operator-facing `attach` CLI passes to `tmux attach -t <name>`. The spec amendment refines HC-052 accordingly.

### Session-name reconciliation in $TMUX-reuse mode

The orphan sweep currently filters sessions by `TmuxSessionPrefix(projectHash)`. When operating inside an existing operator session under D3, the session name is the operator's choice and won't match.

**Resolution: tag windows with a project-hash sentinel; sweep at the window level.**

Window names produced by the adapter follow this convention:
- D3-fresh (we own the session): session = `harmonik-<hash>-default`, window = `<bead_id>` (single) or `<bead_id>/i<n>` / `<bead_id>/r<n>` (review-loop). Sweep filters by session-name prefix as today (no change to existing code).
- D3-reuse ($TMUX set): session = operator's choice; window = `hk-<hash6>-<bead_id>[/<phase><n>]` where `<hash6>` is the first 6 hex chars of the project hash. Sweep adds a parallel **window**-level sweep that enumerates all sessions, lists their windows, and matches the `hk-<hash6>-` prefix.

Rationale: requiring the operator session match harmonik naming violates D3's "use the operator's existing session" promise. Tracking windows in a state file introduces a new persistence concern and a new failure mode (state file vs reality drift); `tmux list-windows` is ground truth. The sentinel approach makes sweep symmetric (sessions OR windows) and uses the existing project_hash provenance marker pattern at a different scope.

## 2. Package layout

```
internal/lifecycle/tmux/
├── doc.go              — package docstring, scope, spec citations
├── adapter.go          — Adapter interface + WindowHandle struct + WindowName helpers
├── osadapter.go        — OSAdapter (production, shells to tmux binary)
├── handle.go           — WindowHandle methods (Kill/Wait/PID lookup), Outcome shape
├── subcommand.go       — `hk tmux-start` subcommand implementation
├── windowname.go       — deterministic name derivation per D4 + sentinel-prefix mode
├── orphanwindow.go     — window-level orphan sweep (parallel to orphansweep.go session sweep)
└── *_test.go           — unit tests with mocked shellouts; integration test gated by `-tags tmux_real`
```

Depguard component-matrix entry: `internal/lifecycle/tmux` may import `internal/lifecycle` (provenance), `internal/core` (ProjectHash, BeadID typed values), and stdlib only. It MUST NOT import `internal/daemon`, `internal/handler`, `internal/workspace`, or `internal/handlercontract` — substrate is a leaf.

## 3. Public Go signatures

### `internal/lifecycle/tmux/adapter.go`

```go
package tmux

import (
    "context"
    "errors"

    "github.com/gregberns/harmonik/internal/core"
)

// Adapter is the substrate interface the daemon uses to host handler subprocesses
// inside tmux windows. The OSAdapter implementation shells out to the tmux binary.
//
// Spec ref: process-lifecycle.md §4.7 PL-021b.
type Adapter interface {
    // Available probes for the tmux binary on PATH and verifies the minimum
    // version (3.0). Returns nil if tmux is present and compatible; an error
    // if missing or too old. Daemon MUST fail-fast on non-nil with exit 22.
    Available(ctx context.Context) error

    // ResolveSession returns the session name the daemon must create windows
    // in. If $TMUX is set, returns (envSession, false, nil) — windows must be
    // tagged with the hk-<hash6>- sentinel. If $TMUX is empty, returns
    // ("", false, ErrNoSession).
    ResolveSession(ctx context.Context, projectHash core.ProjectHash) (sessionName string, ownsSession bool, err error)

    // EnsureSession creates a tmux session with the given name if absent.
    // Used by `hk tmux-start`. Idempotent.
    EnsureSession(ctx context.Context, sessionName, cwd string) error

    // NewWindowInSession creates a new tmux window in sessionName, executes
    // argv inside it with cwd and env, and returns a WindowHandle.
    NewWindowInSession(ctx context.Context, in NewWindowIn) (WindowHandle, error)

    // KillWindow closes the window identified by handle. Idempotent.
    KillWindow(ctx context.Context, h WindowHandle) error

    // ListWindows enumerates window names in sessionName matching the
    // hk-<hash6>- sentinel prefix for projectHash. Used by orphan sweep.
    ListWindows(ctx context.Context, sessionName string, projectHash core.ProjectHash) ([]string, error)
}

type NewWindowIn struct {
    SessionName string
    WindowName  string
    Cwd         string
    Env         []string  // KEY=VALUE entries forwarded via tmux -e
    Argv        []string  // binary + args; argv[0] is the binary
}

type WindowHandle struct {
    SessionName string
    WindowName  string
    PID         int  // pane_pid as reported by tmux at create time
}

type Outcome struct {
    ExitCode int
    Signal   int  // 0 if cleanly exited
}

var (
    ErrNoSession       = errors.New("tmux: $TMUX unset; run `hk tmux-start` first")
    ErrWindowCollision = errors.New("tmux: window name already exists in session")
    ErrTmuxMissing     = errors.New("tmux: not on PATH or version below 3.0")
)

type ErrTmuxFailure struct {
    Cmd    []string
    Stderr string
    Err    error
}
```

### `internal/lifecycle/tmux/windowname.go`

```go
// WindowName derives the tmux window name per D4 and session-reuse sentinel.
//
// Single-mode, daemon-owned session:     <bead_id>
// Single-mode, $TMUX reuse:              hk-<hash6>-<bead_id>
// Review-loop implementer iter N, owned: <bead_id>/i<N>
// Review-loop implementer iter N, reuse: hk-<hash6>-<bead_id>/i<N>
// Review-loop reviewer iter N, owned:    <bead_id>/r<N>
// Review-loop reviewer iter N, reuse:    hk-<hash6>-<bead_id>/r<N>
//
// Spec ref: workspace-model.md WM-002a; process-lifecycle.md §4.7 PL-021b.
func WindowName(beadID core.BeadID, phase Phase, iteration int, projectHash core.ProjectHash, ownsSession bool) string

type Phase int
const (
    PhaseSingle Phase = iota
    PhaseImplementer
    PhaseReviewer
)
```

### `internal/lifecycle/tmux/subcommand.go`

```go
// RunTmuxStart is the entry point for `hk tmux-start`.
//   1. If $TMUX is non-empty, prints "already inside tmux" and returns 0.
//   2. Otherwise, computes sessionName = TmuxSessionName(projectHash, "default").
//   3. EnsureSession(sessionName, cwd=projectDir).
//   4. Execs `tmux attach-session -t <sessionName>` via syscall.Exec.
//
// Flag surface:
//   --session-name <name>   Override default session name (must keep harmonik-<hash>- prefix).
//   --exec-hk               Run `hk` as the session's initial command.
func RunTmuxStart(ctx context.Context, args []string, projectDir string, projectHash core.ProjectHash) int
```

Wired in `cmd/harmonik/main.go` adjacent to existing `hook-relay` dispatch.

## 4. Substrate seam threading

`internal/handler/handler.go:LaunchSpec` gains an optional `Substrate` field:

```go
// Substrate, when non-nil, indicates the subprocess MUST be hosted inside a
// substrate-managed pty (e.g. tmux window) rather than spawned directly via
// exec.CommandContext. When nil, Handler.Launch preserves current behavior.
//
// Spec ref: handler-contract.md HC-054 (refined); process-lifecycle.md PL-021b.
Substrate Substrate
```

Where `Substrate` is a tiny interface defined in the handler package (NOT importing tmux — depguard violation otherwise; the daemon composition root constructs a concrete tmux-backed `Substrate` and passes it in):

```go
type Substrate interface {
    SpawnWindow(ctx context.Context, in SubstrateSpawn) (SubstrateSession, error)
}

type SubstrateSpawn struct {
    WindowName string  // pre-computed by tmux.WindowName, opaque to handler
    Cwd        string
    Env        []string
    Argv       []string
}

type SubstrateSession interface {
    Kill(ctx context.Context) error
    Wait(ctx context.Context) error
    Outcome() Outcome
    PID() int
    Stdout() io.Reader  // nil for substrate-hosted sessions — bridge wire is socket
}
```

The daemon composition root (`internal/daemon/daemon.go:Start`) builds:

```go
tmuxAdapter := tmux.OSAdapter{}
if err := tmuxAdapter.Available(ctx); err != nil { /* exit 22 */ }
sessionName, ownsSession, err := tmuxAdapter.ResolveSession(ctx, projectHash)
if errors.Is(err, tmux.ErrNoSession) {
    fmt.Fprintln(os.Stderr, "harmonik: $TMUX unset; run `hk tmux-start` first")
    return 24
}
substrate := tmuxsubstrate.New(tmuxAdapter, sessionName, ownsSession, projectHash)
```

The bridge adapter is the natural place to inject `LaunchSpec.Substrate = substrate` so the daemon path stays twin-blind: the adapter registry keyed on `agent_type` decides whether the substrate seam is engaged, not `Binary == "claude"`.

## 5. Lifecycle

### Init (daemon startup, in PL-005 ordering)
1. Composition root builds `tmux.OSAdapter`.
2. Cat 0 pre-check — `tmuxAdapter.Available(ctx)`; on error, exit 22.
3. `tmuxAdapter.ResolveSession(ctx, projectHash)`. If `$TMUX` set, record `(operatorSessionName, ownsSession=false)`. Else, daemon refuses with exit 24 and a friendly directive.
4. Orphan sweep: existing `SweepOrphanTmuxSessions` runs (filters by project-hash session prefix). Additionally, **window-level** sweep runs.

### Spawn (workloop bead dispatch)
1. Workloop calls handler-adapter; adapter computes `windowName := tmux.WindowName(beadID, phase, iter, projectHash, ownsSession)`.
2. Adapter builds `LaunchSpec{..., Substrate: substrate}`.
3. `Handler.Launch` detects non-nil Substrate, calls `Substrate.SpawnWindow(...)`.
4. Substrate translates to `tmuxAdapter.NewWindowInSession(...)` — tmux forks claude with pty stdio inside the named window.
5. Adapter returns a `SubstrateSession`; handler returns it adapted to `handler.Session`.
6. No `SpawnWatcher` is wired (Stdout returns nil). Completion observation is `HookSessionStore.WaitForOutcome(...)` || `SubstrateSession.Wait(ctx)`.

### Attach (operator)
The operator runs `tmux attach -t <sessionName>` and `select-window -t <sessionName>:<windowName>`. Window name is published in `agent_ready` payload as `tmux_window_name`.

### Kill / Sweep
`SubstrateSession.Kill(ctx)` issues `tmux kill-window`. PL-006 session-level sweep unchanged. PL-021c adds parallel window-level sweep: iterate all sessions, list windows, kill any matching `hk-<hash6>-` for *this* daemon's project hash.

## 6. What this design explicitly does NOT do

- Does NOT introduce `creack/pty` or any other pty library.
- Does NOT use `tmux pipe-pane`. Daemon never reads pane output.
- Does NOT touch CHB-001..027 wire format; substrate is orthogonal to bridge protocol.
- Does NOT add branching on `Binary == "claude"`. Substrate engagement driven by adapter-registry dispatch on `agent_type` (twin-blind, CHB-022).

## 7. Tests

- **Unit** — `Adapter` interface mocked via a `fakeTmux` that records argv and returns canned stdout.
- **Determinism** — `WindowName` table tests covering all phase × owned/reuse × iteration cases.
- **Integration (tag-gated `//go:build tmux_real`)** — exercise real `tmux` binary in ephemeral session.
- **Orphan sweep window-level** — extends `orphansweep_pl006_test.go` patterns.
- **`hk tmux-start`** — exec-replacement test using a faked tmux binary on PATH.
