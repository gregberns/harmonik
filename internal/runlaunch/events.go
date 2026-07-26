package runlaunch

// events.go — the CHB-018 pre-exec announcement relay, the launch/ready anomaly
// events, and the implementer phase-complete report.
//
// Carved out of internal/daemon/workloop.go by P2 unit E5 RT19b (pure move). No
// emission string and no core.EventType constant changed, and the ORDERING
// inside EmitPreExecBeforeLaunch (launch_initiated held back until after
// SpawnWindow returns, hk-4l7zs) is preserved exactly.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// EmitPreExecMessage emits a single CHB-018 pre-exec progress message on the
// bus using the message's embedded "type" field as the event type.
//
// Each pre-exec message is compact JSON with a top-level "type" field matching
// one of the §8.3 event-type constants (handler_capabilities,
// session_log_location, skills_provisioned, agent_ready). Parsing the type
// avoids emitting all four under a single catch-all envelope, which would
// break per-type JSONL filtering for consumers.
//
// If the type field cannot be parsed the message is still emitted under the
// agent_ready type as a safe fallback (no information is lost; the payload
// is the ground truth).
//
// Spec: specs/claude-hook-bridge.md §4.7 CHB-018.
// Bead: hk-gql20.14.
// Origin: internal/daemon/workloop.go emitPreExecMessage.
func EmitPreExecMessage(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, msg json.RawMessage) {
	var envelope struct {
		Type string `json:"type"`
	}
	eventType := core.EventTypeAgentReady // safe fallback
	if err := json.Unmarshal(msg, &envelope); err == nil && envelope.Type != "" {
		eventType = core.EventType(envelope.Type)
	}
	_ = bus.EmitWithRunID(ctx, runID, eventType, msg) //nolint:errcheck // best-effort observability emit; a bus failure must never fail the run, and the underlying condition is already surfaced on the reopen/done path
}

// preExecMsgType extracts the "type" field of a pre-exec message, or "" on
// parse failure.
func preExecMsgType(msg json.RawMessage) string {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(msg, &envelope); err == nil {
		return envelope.Type
	}
	return ""
}

// EmitPreExecBeforeLaunch emits every pre-exec message EXCEPT launch_initiated
// and returns the launch_initiated message (if any) for the caller to emit
// AFTER SpawnWindow/Launch returns.
//
// hk-4l7zs: launch_initiated previously fired BEFORE SpawnWindow. When the spawn
// semaphore was wedged (a leaked slot), SpawnWindow blocked indefinitely yet the
// daemon had already emitted launch_initiated — so operators (and the stale
// watcher) saw a "launched" run that had, in fact, never spawned a tmux window.
// Deferring launch_initiated until the window is actually live makes the event
// mean what it says and lets launch_stall_detected fire correctly when the spawn
// is wedged. Ordering of the other pre-exec messages is preserved.
//
// Origin: internal/daemon/workloop.go emitPreExecBeforeLaunch.
func EmitPreExecBeforeLaunch(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, msgs []json.RawMessage) (launchInitiated json.RawMessage) {
	for _, msg := range msgs {
		if preExecMsgType(msg) == string(core.EventTypeLaunchInitiated) {
			launchInitiated = msg
			continue
		}
		EmitPreExecMessage(ctx, bus, runID, msg)
	}
	return launchInitiated
}

// EmitImplementerPhaseComplete emits an implementer_phase_complete event
// (hk-cd8yu) immediately after the implementer session ends.
//
// stderrTail is the raw stderr bytes captured by waitWithSocketGrace; only the
// first 200 bytes are included in the event payload per the spec.
// duration is the wall-clock time from implementer launch to session end.
//
// Spec ref: hk-cd8yu.
// Origin: internal/daemon/workloop.go emitImplementerPhaseComplete.
func EmitImplementerPhaseComplete(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, exitCode int, stderrTail []byte, commitLanded bool, duration time.Duration) {
	const maxStderrHead = 200
	stderrHead := ""
	if len(stderrTail) > 0 {
		head := stderrTail
		if len(head) > maxStderrHead {
			head = head[:maxStderrHead]
		}
		stderrHead = string(head)
	}
	pl := core.ImplementerPhaseCompletePayload{
		RunID:           runID,
		ExitCode:        exitCode,
		StderrTailHead:  stderrHead,
		CommitLanded:    commitLanded,
		DurationSeconds: duration.Seconds(),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeImplementerPhaseComplete, b) //nolint:errcheck // best-effort observability emit; a bus failure must never fail the run, and the underlying condition is already surfaced on the reopen/done path
}

// EmitSpawnCapBlocked emits a spawn_cap_blocked event (hk-4l7zs) when a launch's
// SpawnWindow times out waiting for a spawn-semaphore slot — the observable
// signature of a slot leak (every slot held by an acquired-but-never-released
// session). Non-fatal: emit-marshal errors are silently discarded; the launch
// failure is already surfaced via the reopen/done path.
//
// capSize/slotsInUse describe the saturated pool; when unknown (0) the payload
// still validates via a minimum capSize of 1 so the event is never dropped.
//
// Also called from the BOOT path (internal/daemon/bootstate.go) — this is why
// this package must be a leaf both the run path and the boot path can import.
//
// Origin: internal/daemon/workloop.go emitSpawnCapBlocked.
func EmitSpawnCapBlocked(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, waited time.Duration, slotsInUse, capSize int) {
	if bus == nil {
		return
	}
	if capSize <= 0 {
		capSize = 1
	}
	waitedMS := waited.Milliseconds()
	if waitedMS <= 0 {
		waitedMS = 1
	}
	pl := core.SpawnCapBlockedPayload{
		RunID:      runID.String(),
		WaitedMS:   waitedMS,
		SlotsInUse: slotsInUse,
		CapSize:    capSize,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeSpawnCapBlocked, b) //nolint:errcheck // best-effort observability emit; a bus failure must never fail the run, and the underlying condition is already surfaced on the reopen/done path
}

// EmitTmuxNewWindowTimeout emits a tmux_new_window_timeout event (hk-r1rup) when
// a launch's SpawnWindow times out waiting for the underlying `tmux new-window`
// call to return — the observable signature of a hung tmux invocation (the
// no-spawn wedge). Non-fatal: emit-marshal errors are silently discarded; the
// launch failure is already surfaced via the reopen/done path.
//
// waited is the duration the new-window call blocked before the bound fired;
// when unknown (<= 0) the payload still validates via a minimum waited_ms of 1
// so the event is never dropped. Mirrors EmitSpawnCapBlocked (hk-4l7zs).
//
// Also called from the BOOT path (internal/daemon/bootstate.go) — this is why
// this package must be a leaf both the run path and the boot path can import.
//
// Origin: internal/daemon/workloop.go emitTmuxNewWindowTimeout.
func EmitTmuxNewWindowTimeout(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, waited time.Duration) {
	if bus == nil {
		return
	}
	waitedMS := waited.Milliseconds()
	if waitedMS <= 0 {
		waitedMS = 1
	}
	pl := core.TmuxNewWindowTimeoutPayload{
		RunID:    runID.String(),
		WaitedMS: waitedMS,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeTmuxNewWindowTimeout, b) //nolint:errcheck // best-effort observability emit; a bus failure must never fail the run, and the underlying condition is already surfaced on the reopen/done path
}

// EmitAgentReadyTimeout emits an agent_ready_timeout event (hk-5cox8) when
// the HC-056 timeout fires — no agent_ready relay message arrived within the
// configured deadline. The event carries run_id, claude_session_id, and
// timeout_ms so post-hoc analysis can correlate which runs never became ready.
//
// effectiveTimeout: zero is replaced by DefaultAgentReadyTimeout (30s) to
// match the semantics of waitAgentReady.
//
// Origin: internal/daemon/workloop.go emitAgentReadyTimeout.
func EmitAgentReadyTimeout(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, claudeSessionID string, effectiveTimeout time.Duration) {
	if effectiveTimeout <= 0 {
		effectiveTimeout = DefaultAgentReadyTimeout
	}
	pl := core.AgentReadyTimeoutPayload{
		RunID:           runID,
		ClaudeSessionID: claudeSessionID,
		TimeoutMs:       effectiveTimeout.Milliseconds(),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeAgentReadyTimeout, b) //nolint:errcheck // best-effort observability emit; a bus failure must never fail the run, and the underlying condition is already surfaced on the reopen/done path
}
