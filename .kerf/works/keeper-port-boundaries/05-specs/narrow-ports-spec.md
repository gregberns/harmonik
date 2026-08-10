# Narrow Ports Specification

## Requirements

Each port represents one source or one effect family. The reactor receives only value observations.

## Research summary

`GaugePort.Snapshot` joins unrelated sources. `PanePort.Capture` is not part of the automatic cycle. Activity also needs the latest user turn.

## Approach

Add `PaneWriter`, `ContextStore`, `ActivityProbe`, `HandoffDocument`, and `CycleJournalStore`. Use small source probes for managed, idle, dispatch, sleep, hold, and operator presence. A shell-owned sampler composes `GateSnapshot` with current lazy-read guards.

Keep pane capture outside `CycleDeps`. Adapt await-ack separately later.

The contracts are:

```go
type PaneWriter interface {
    Inject(context.Context, string, string) error
    SendEscape(context.Context, string) error
    SetEnv(context.Context, string, string, string) error
}
type ContextStore interface {
    ReadGauge() (*CtxFile, time.Time, error)
    SetManagedSession(string) error
    ClearPrecompactTrigger() error
}
type ActivityProbe interface {
    IdleMarkerModTime() (time.Time, bool)
    LastUserTurn(string) (time.Time, bool)
    LastAssistantTurn(string) (time.Time, bool)
}
type ManagedProbe interface { IsManaged() bool }
type IdleProbe interface { CrispIdle() bool }
type DispatchProbe interface { HoldingDispatch() bool }
type SleepProbe interface { Sleeping(string) bool }
type HoldProbe interface { Held() bool }
type OperatorPresenceProbe interface { Attached(string) bool }
type HandoffDocument interface {
    Path() string
    Read() (string, error)
    ModTime() (time.Time, bool)
    ScrubNonce() error
}
type CycleJournalStore interface {
    Write(*CycleJournal) error
    Read() (*CycleJournal, error)
}
```

`IdleProbe.CrispIdle` is the entry predicate. `ActivityProbe.IdleMarkerModTime` reads the post-handoff model-done marker.

## Files and changes

- Replace broad definitions and adapters incrementally in `internal/keeper/ports.go`.
- Update reads and effects in `internal/keeper/shell.go`.
- Update construction in `internal/keeper/cycle.go`.
- Add adapter contract tests in `internal/keeper/ports_test.go`.

## Error handling and compatibility

Preserve read order and fail-open or fail-closed behavior. Preserve file formats and journal failure policy. Do not claim snapshot atomicity.

## Acceptance criteria

- Transcript and tmux methods do not exist on the context store.
- Pane capture is not required by an automatic cycle.
- Gate sampling preserves empty-session, empty-target, and disabled-window guards.
- Recovery can read managed state without sampling unrelated gates.
- `LastAssistantTurn` remains available when post-answer grace is disabled.
- Probe error and Boolean behavior matches current fail-open or fail-closed behavior.
- An opened journal write error stops entry. Later journal errors remain best effort.
- `ScrubNonce` preserves all non-nonce handoff bytes.

## Verification

Run `go test ./internal/keeper ./internal/keepertest` and `go test -tags=integration ./internal/keeper`.
