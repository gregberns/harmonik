package keeper_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// precompactAction extracts the "action" field from a
// session_keeper_precompact_blocked event payload.
func precompactAction(t *testing.T, ev keeper.EmittedEvent) string {
	t.Helper()
	var p core.SessionKeeperPrecompactBlockedPayload
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatalf("unmarshal precompact payload: %v", err)
	}
	return p.Action
}

// newPrecompactCycler builds a Cycler for precompact tests. It wires the same
// fakes as cycle_test.go but adds a no-op ClearPrecompactTriggerFn so tests
// can control the marker file directly.
func newPrecompactCycler(
	t *testing.T,
	projectDir string,
	em keeper.Emitter,
	isManaged bool,
	holdingDispatch bool,
	readHandoff func(string) (string, error),
	readGaugeFn func(string, string) (*keeper.CtxFile, time.Time, error),
) *keeper.Cycler {
	t.Helper()

	spy := &cycleSpyInjector{} // injector shared but not inspected in gate tests
	jc := &journalCapture{}
	const cycleID = "cyc-precompact-test"
	nonce := "<!-- KEEPER:" + cycleID + " -->"

	if readHandoff == nil {
		// Default: immediately return nonce on first call.
		readHandoff = func(_ string) (string, error) {
			return "# Handoff\n\n" + nonce + "\n", nil
		}
	}
	if readGaugeFn == nil {
		readGaugeFn = func(_, _ string) (*keeper.CtxFile, time.Time, error) {
			return &keeper.CtxFile{Pct: 95.0, SessionID: "sess-new"}, time.Now(), nil
		}
	}

	cfg := keeper.CyclerConfig{
		AgentName:      "precompact-agent",
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 500 * time.Millisecond,
		ClearSettle:    50 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return isManaged },
		HandoffFilePath: func(_, agent string) string {
			return filepath.Join(projectDir, "HANDOFF-"+agent+".md")
		},
		ReadHandoff:              readHandoff,
		TruncateHandoffFn:        func(_ string) error { return nil },
		InjectFn:                 spy.inject,
		ReadGaugeFn:              readGaugeFn,
		CrispIdleFn:              func(_, _ string) bool { return false }, // not used by RunForPrecompact
		HoldingDispatchFn:        func(_, _ string) bool { return holdingDispatch },
		WriteJournalFn:           jc.write,
		ClearPrecompactTriggerFn: func(_, _ string) error { return nil }, // no-op; test controls marker
	}
	return keeper.NewCycler(cfg, em)
}

// TestRunForPrecompact_NotManaged verifies that an unmanaged agent emits
// "not_managed" and does not run the cycle.
func TestRunForPrecompact_NotManaged(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	cycler := newPrecompactCycler(t, t.TempDir(), em, false /*isManaged*/, false, nil, nil)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: "sess-abc"}
	if err := cycler.RunForPrecompact(context.Background(), cf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := em.EventsOfType(core.EventTypeSessionKeeperPrecompactBlocked)
	if len(events) != 1 {
		t.Fatalf("want 1 precompact_blocked event, got %d", len(events))
	}
	if got := precompactAction(t, events[0]); got != "not_managed" {
		t.Errorf("action = %q, want %q", got, "not_managed")
	}

	// Cycle must not have fired.
	if got := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(got) != 0 {
		t.Errorf("cycle started unexpectedly: %d handoff_started events", len(got))
	}
}

// TestRunForPrecompact_EmptySessionID verifies that an empty session_id skips
// the cycle with action "hold_dispatch_skip".
func TestRunForPrecompact_EmptySessionID(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	cycler := newPrecompactCycler(t, t.TempDir(), em, true /*isManaged*/, false, nil, nil)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: ""}
	if err := cycler.RunForPrecompact(context.Background(), cf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := em.EventsOfType(core.EventTypeSessionKeeperPrecompactBlocked)
	if len(events) != 1 {
		t.Fatalf("want 1 precompact_blocked event, got %d", len(events))
	}
	if got := precompactAction(t, events[0]); got != "hold_dispatch_skip" {
		t.Errorf("action = %q, want %q", got, "hold_dispatch_skip")
	}
}

// TestRunForPrecompact_HoldingDispatch verifies that HoldingDispatch=true skips
// the cycle with action "hold_dispatch_skip".
func TestRunForPrecompact_HoldingDispatch(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	cycler := newPrecompactCycler(t, t.TempDir(), em, true /*isManaged*/, true /*holdingDispatch*/, nil, nil)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: "sess-abc"}
	if err := cycler.RunForPrecompact(context.Background(), cf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := em.EventsOfType(core.EventTypeSessionKeeperPrecompactBlocked)
	if len(events) != 1 {
		t.Fatalf("want 1 precompact_blocked event, got %d", len(events))
	}
	if got := precompactAction(t, events[0]); got != "hold_dispatch_skip" {
		t.Errorf("action = %q, want %q", got, "hold_dispatch_skip")
	}

	if got := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(got) != 0 {
		t.Errorf("cycle started unexpectedly")
	}
}

// TestRunForPrecompact_AntiLoop verifies that the anti-loop gate suppresses a
// second cycle on the same session_id (same as MaybeRun anti-loop behaviour).
func TestRunForPrecompact_AntiLoop(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	const sid = "sess-fired"

	// Use a nonce that the handoff immediately returns so the first call cycles fully.
	const cycleID = "cyc-precompact-test"
	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := func(_ string) (string, error) {
		return "# Handoff\n\n" + nonce + "\n", nil
	}
	// Gauge: initially returns sid, then after first cycle returns "sess-new".
	callCount := 0
	readGaugeFn := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		callCount++
		if callCount < 5 {
			return &keeper.CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
		}
		return &keeper.CtxFile{Pct: 95.0, SessionID: "sess-new"}, time.Now(), nil
	}

	cycler := newPrecompactCycler(t, t.TempDir(), em, true, false, readHandoff, readGaugeFn)

	ctx := context.Background()
	cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}

	// First call: should trigger cycle.
	if err := cycler.RunForPrecompact(ctx, cf); err != nil {
		t.Fatalf("first call error: %v", err)
	}
	firstEvents := em.EventsOfType(core.EventTypeSessionKeeperPrecompactBlocked)
	if len(firstEvents) != 1 {
		t.Fatalf("want 1 precompact_blocked after first call, got %d", len(firstEvents))
	}
	if got := precompactAction(t, firstEvents[0]); got != "cycle_triggered" {
		t.Errorf("first call action = %q, want %q", got, "cycle_triggered")
	}

	// Second call on same session: anti-loop should suppress.
	if err := cycler.RunForPrecompact(ctx, cf); err != nil {
		t.Fatalf("second call error: %v", err)
	}
	allEvents := em.EventsOfType(core.EventTypeSessionKeeperPrecompactBlocked)
	if len(allEvents) != 2 {
		t.Fatalf("want 2 precompact_blocked events total, got %d", len(allEvents))
	}
	if got := precompactAction(t, allEvents[1]); got != "anti_loop_suppressed" {
		t.Errorf("second call action = %q, want %q", got, "anti_loop_suppressed")
	}
}

// TestRunForPrecompact_HappyPath verifies the "cycle_triggered" path: all gates
// pass, the cycle runs to completion, and handoff_started + cycle_complete are
// emitted.
func TestRunForPrecompact_HappyPath(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	const (
		prevSID = "sess-before"
		newSID  = "sess-after"
	)

	const cycleID = "cyc-precompact-test"
	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := func(_ string) (string, error) {
		return "# Handoff\n\n" + nonce + "\n", nil
	}
	readGaugeFn := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: newSID}, time.Now(), nil
	}

	cycler := newPrecompactCycler(t, t.TempDir(), em, true /*isManaged*/, false, readHandoff, readGaugeFn)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.RunForPrecompact(context.Background(), cf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// precompact_blocked with "cycle_triggered"
	pcEvents := em.EventsOfType(core.EventTypeSessionKeeperPrecompactBlocked)
	if len(pcEvents) != 1 {
		t.Fatalf("want 1 precompact_blocked, got %d", len(pcEvents))
	}
	if got := precompactAction(t, pcEvents[0]); got != "cycle_triggered" {
		t.Errorf("action = %q, want %q", got, "cycle_triggered")
	}

	// Cycle must have fired: handoff_started present.
	if got := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(got) == 0 {
		t.Error("expected handoff_started event; got none")
	}
	// Cycle must have completed.
	if got := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete); len(got) == 0 {
		t.Error("expected cycle_complete event; got none")
	}
}

// TestHasPrecompactTrigger verifies the filesystem marker helpers.
func TestHasPrecompactTrigger(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const agent = "test-agent"

	if keeper.HasPrecompactTrigger(dir, agent) {
		t.Error("expected HasPrecompactTrigger = false before marker is written")
	}

	keeperDir := filepath.Join(dir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(keeperDir, agent+".precompact")
	if err := os.WriteFile(markerPath, []byte("2026-06-08T00:00:00Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if !keeper.HasPrecompactTrigger(dir, agent) {
		t.Error("expected HasPrecompactTrigger = true after marker is written")
	}

	if err := keeper.ClearPrecompactTrigger(dir, agent); err != nil {
		t.Fatalf("ClearPrecompactTrigger: %v", err)
	}
	if keeper.HasPrecompactTrigger(dir, agent) {
		t.Error("expected HasPrecompactTrigger = false after marker is cleared")
	}
}

// TestClearPrecompactTrigger_Idempotent verifies that clearing an absent marker
// is not an error.
func TestClearPrecompactTrigger_Idempotent(t *testing.T) {
	t.Parallel()
	if err := keeper.ClearPrecompactTrigger(t.TempDir(), "absent-agent"); err != nil {
		t.Errorf("expected no error for absent marker, got: %v", err)
	}
}
