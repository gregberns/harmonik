package keeper_test

// cycle_restart_now_test.go — unit tests for RunOnDemand and RestartNow marker
// I/O (gates.go WriteRestartNowMarker / ReadRestartNowMarker).
//
// These are FOCUSED UNIT tests — no real daemon, no scenario build tag, no tmux.
// The 30-min commit budget constraint is therefore not a concern.
// Refs: hk-wjzf, ON-059.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// --- helpers ------------------------------------------------------------------

// restartNowCyclerConfig builds a minimal CyclerConfig for RunOnDemand tests.
// All injectable fields are stubbed so no disk I/O or tmux calls occur.
func restartNowCyclerConfig(
	projectDir, agent string,
	em keeper.Emitter,
	spy *cycleSpyInjector,
	jc *journalCapture,
	isManaged bool,
	crispIdle bool,
	holdingDispatch bool,
	lastFiredSID string, // empty = no prior cycle
	readHandoffFn func(string) (string, error),
	readGaugeFn func(string, string) (*keeper.CtxFile, time.Time, error),
	readRestartNowMarkerFn func(string, string) (*keeper.RestartNowMarker, error),
	clearRestartNowTriggerFn func(string, string) error,
) (keeper.CyclerConfig, *keeper.Cycler) {
	cfg := keeper.CyclerConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		TmuxTarget:   "fake-pane",
		ActPct:       90.0,
		WarnPct:      80.0,
		ClearSettle:  1 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
		CycleIDGen:   func() string { return "cyc-test-000001" },
		IsManagedFn:  func(_, _ string) bool { return isManaged },
		HandoffFilePath: func(pd, ag string) string {
			return filepath.Join(pd, "HANDOFF-"+ag+".md")
		},
		ReadHandoff:       readHandoffFn,
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		ReadGaugeFn:       readGaugeFn,
		CrispIdleFn:       func(_, _ string) bool { return crispIdle },
		HoldingDispatchFn: func(_, _ string) bool { return holdingDispatch },
		WriteJournalFn:    jc.write,
		ReadJournalFn: func(_ string) (*keeper.CycleJournal, error) {
			return nil, os.ErrNotExist
		},
		ClearPrecompactTriggerFn: func(_, _ string) error { return nil },
		ClearRestartNowTriggerFn: clearRestartNowTriggerFn,
		ReadRestartNowMarkerFn:   readRestartNowMarkerFn,
		AppendHandoffFn:          func(_, _ string) error { return nil },
		SetManagedSessionFn:      func(_, _, _ string) error { return nil },
		SetTmuxEnvFn:             func(_ context.Context, _, _, _ string) error { return nil },
		OperatorAttachedFn:       func(_ string) bool { return false },
		HandoffTimeout:           50 * time.Millisecond,
	}
	c := keeper.NewCycler(cfg, em)
	if lastFiredSID != "" {
		// Warm the anti-loop state by running a fake MaybeRun with a matched
		// gated-out ctx so only lastFiredSID is set, not an actual cycle.
		// Instead: inject the SID via a helper to set lastFiredSID.
		keeper.SetCyclerLastFiredSID(c, lastFiredSID)
	}
	return cfg, c
}

// freshHandoff returns a ReadHandoff fake that always returns content
// containing the given nonce marker.
func freshHandoff(nonce string) func(string) (string, error) {
	return func(_ string) (string, error) {
		return "# Handoff\n\n<!-- KEEPER:" + nonce + " -->\n\nContent.", nil
	}
}

// noNonceHandoff returns a ReadHandoff fake that returns content WITHOUT a nonce.
func noNonceHandoff() func(string) (string, error) {
	return func(_ string) (string, error) {
		return "# Handoff\n\nNo nonce here.", nil
	}
}

// fixedGauge returns a ReadGaugeFn that always returns a CtxFile with the
// given session_id.
func fixedGauge(sid string) func(string, string) (*keeper.CtxFile, time.Time, error) {
	return func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 50.0, SessionID: sid}, time.Now(), nil
	}
}

// --- tests -------------------------------------------------------------------

// TestRunOnDemand_GatePass verifies the happy path: fresh handoff with matching
// nonce, matching session_id, and all safety gates satisfied → /clear and
// /session-resume injected, journal phases recorded.
func TestRunOnDemand_GatePass(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "captain"
	sessionID := "sess-abc"
	nonce := "cyc-20260613T000000-000001"
	requestedAt := time.Now().UTC().Add(-5 * time.Second)

	// Write a real handoff file so os.Stat succeeds and mtime >= requestedAt.
	handoffPath := filepath.Join(projectDir, "HANDOFF-captain.md")
	handoffContent := "# Handoff\n\n<!-- KEEPER:" + nonce + " -->\n\nContent."
	if err := os.WriteFile(handoffPath, []byte(handoffContent), 0o600); err != nil {
		t.Fatal(err)
	}

	marker := &keeper.RestartNowMarker{
		Nonce:       nonce,
		RequestedAt: requestedAt,
		SessionID:   sessionID,
	}

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	var clearCalled bool
	_, c := restartNowCyclerConfig(
		projectDir, agent, em, spy, jc,
		true,  // isManaged
		true,  // crispIdle
		false, // holdingDispatch
		"",    // no prior fired SID
		func(_ string) (string, error) { return handoffContent, nil },
		fixedGauge(sessionID),
		func(_, _ string) (*keeper.RestartNowMarker, error) { return marker, nil },
		func(_, _ string) error { clearCalled = true; return nil },
	)

	ctx := context.Background()
	cf := &keeper.CtxFile{Pct: 50.0, SessionID: sessionID}
	if err := c.RunOnDemand(ctx, cf); err != nil {
		t.Fatalf("RunOnDemand unexpected error: %v", err)
	}

	// Marker must be consumed once.
	if !clearCalled {
		t.Error("ClearRestartNowTrigger was not called")
	}

	// No restart_now_blocked event.
	blocked := em.EventsOfType(core.EventTypeSessionKeeperRestartNowBlocked)
	if len(blocked) != 0 {
		t.Errorf("unexpected restart_now_blocked events: %v", blocked)
	}

	// Journal phases: confirmed → cleared → resumed → complete.
	phases := jc.snapshot()
	wantPhases := []string{"confirmed", "cleared", "resumed", "complete"}
	if len(phases) != len(wantPhases) {
		t.Fatalf("journal phases %v, want %v", phases, wantPhases)
	}
	for i, p := range wantPhases {
		if phases[i] != p {
			t.Errorf("phase[%d] = %q, want %q", i, phases[i], p)
		}
	}

	// /clear and /session-resume must be injected.
	texts := spy.texts()
	foundClear, foundResume := false, false
	for _, txt := range texts {
		if txt == "/clear" {
			foundClear = true
		}
		if txt != "" && txt[0] == '/' && len(txt) > 16 && txt[:16] == "/session-resume " {
			foundResume = true
		}
	}
	if !foundClear {
		t.Errorf("no /clear injection; got texts: %v", texts)
	}
	if !foundResume {
		t.Errorf("no /session-resume injection; got texts: %v", texts)
	}
}

// TestRunOnDemand_StaleHandoff verifies that when the handoff mtime is BEFORE
// marker.requested_at, RunOnDemand emits restart_now_blocked{reason=handoff_stale}
// and returns nil (non-destructive).
func TestRunOnDemand_StaleHandoff(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "captain"
	sessionID := "sess-def"
	nonce := "cyc-20260613T000000-000002"

	// Handoff written BEFORE requestedAt.
	handoffPath := filepath.Join(projectDir, "HANDOFF-captain.md")
	handoffContent := "# Handoff\n\n<!-- KEEPER:" + nonce + " -->\n\nOld content."
	if err := os.WriteFile(handoffPath, []byte(handoffContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// requestedAt is 1 minute in the FUTURE relative to the written file.
	requestedAt := time.Now().UTC().Add(60 * time.Second)
	marker := &keeper.RestartNowMarker{
		Nonce:       nonce,
		RequestedAt: requestedAt,
		SessionID:   sessionID,
	}

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	var clearCalled bool
	_, c := restartNowCyclerConfig(
		projectDir, agent, em, spy, jc,
		true, true, false, "",
		func(_ string) (string, error) { return handoffContent, nil },
		fixedGauge(sessionID),
		func(_, _ string) (*keeper.RestartNowMarker, error) { return marker, nil },
		func(_, _ string) error { clearCalled = true; return nil },
	)

	ctx := context.Background()
	cf := &keeper.CtxFile{Pct: 50.0, SessionID: sessionID}
	if err := c.RunOnDemand(ctx, cf); err != nil {
		t.Fatalf("RunOnDemand unexpected error: %v", err)
	}

	if !clearCalled {
		t.Error("ClearRestartNowTrigger was not called (consume-once required even on gate fail)")
	}

	blocked := em.EventsOfType(core.EventTypeSessionKeeperRestartNowBlocked)
	if len(blocked) != 1 {
		t.Fatalf("expected 1 restart_now_blocked, got %d", len(blocked))
	}

	// No /clear or /session-resume injected.
	for _, txt := range spy.texts() {
		if txt == "/clear" {
			t.Error("unexpected /clear injection on stale handoff gate fail")
		}
	}
}

// TestRunOnDemand_NonceMismatch verifies that when the handoff does NOT contain
// the expected nonce, RunOnDemand emits restart_now_blocked{reason=nonce_mismatch}.
func TestRunOnDemand_NonceMismatch(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "captain"
	sessionID := "sess-ghi"
	nonce := "cyc-20260613T000000-000003"

	// Handoff has a DIFFERENT nonce.
	handoffPath := filepath.Join(projectDir, "HANDOFF-captain.md")
	handoffContent := "# Handoff\n\n<!-- KEEPER:cyc-WRONG-NONCE -->\n\nContent."
	if err := os.WriteFile(handoffPath, []byte(handoffContent), 0o600); err != nil {
		t.Fatal(err)
	}

	marker := &keeper.RestartNowMarker{
		Nonce:       nonce,
		RequestedAt: time.Now().UTC().Add(-5 * time.Second),
		SessionID:   sessionID,
	}

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	var clearCalled bool
	_, c := restartNowCyclerConfig(
		projectDir, agent, em, spy, jc,
		true, true, false, "",
		func(_ string) (string, error) { return handoffContent, nil },
		fixedGauge(sessionID),
		func(_, _ string) (*keeper.RestartNowMarker, error) { return marker, nil },
		func(_, _ string) error { clearCalled = true; return nil },
	)

	ctx := context.Background()
	cf := &keeper.CtxFile{Pct: 50.0, SessionID: sessionID}
	if err := c.RunOnDemand(ctx, cf); err != nil {
		t.Fatalf("RunOnDemand unexpected error: %v", err)
	}

	if !clearCalled {
		t.Error("ClearRestartNowTrigger was not called (consume-once required even on gate fail)")
	}

	blocked := em.EventsOfType(core.EventTypeSessionKeeperRestartNowBlocked)
	if len(blocked) != 1 {
		t.Fatalf("expected 1 restart_now_blocked, got %d", len(blocked))
	}

	for _, txt := range spy.texts() {
		if txt == "/clear" {
			t.Error("unexpected /clear injection on nonce-mismatch gate fail")
		}
	}
}

// TestRunOnDemand_SessionIDMismatch verifies that when marker.session_id differs
// from cf.SessionID, RunOnDemand emits restart_now_blocked{reason=session_id_mismatch}.
func TestRunOnDemand_SessionIDMismatch(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "captain"
	currentSID := "sess-current"
	markerSID := "sess-old"
	nonce := "cyc-20260613T000000-000004"

	handoffPath := filepath.Join(projectDir, "HANDOFF-captain.md")
	handoffContent := "# Handoff\n\n<!-- KEEPER:" + nonce + " -->\n\nContent."
	if err := os.WriteFile(handoffPath, []byte(handoffContent), 0o600); err != nil {
		t.Fatal(err)
	}

	marker := &keeper.RestartNowMarker{
		Nonce:       nonce,
		RequestedAt: time.Now().UTC().Add(-5 * time.Second),
		SessionID:   markerSID, // different from currentSID
	}

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	var clearCalled bool
	_, c := restartNowCyclerConfig(
		projectDir, agent, em, spy, jc,
		true, true, false, "",
		func(_ string) (string, error) { return handoffContent, nil },
		fixedGauge(currentSID),
		func(_, _ string) (*keeper.RestartNowMarker, error) { return marker, nil },
		func(_, _ string) error { clearCalled = true; return nil },
	)

	ctx := context.Background()
	cf := &keeper.CtxFile{Pct: 50.0, SessionID: currentSID}
	if err := c.RunOnDemand(ctx, cf); err != nil {
		t.Fatalf("RunOnDemand unexpected error: %v", err)
	}

	if !clearCalled {
		t.Error("ClearRestartNowTrigger was not called (consume-once required)")
	}

	blocked := em.EventsOfType(core.EventTypeSessionKeeperRestartNowBlocked)
	if len(blocked) != 1 {
		t.Fatalf("expected 1 restart_now_blocked, got %d", len(blocked))
	}

	for _, txt := range spy.texts() {
		if txt == "/clear" {
			t.Error("unexpected /clear on session_id_mismatch")
		}
	}
}

// TestRunOnDemand_AntiLoopSuppressed verifies that when lastFiredSID matches
// the current session_id, RunOnDemand emits restart_now_blocked{reason=anti_loop_suppressed}.
func TestRunOnDemand_AntiLoopSuppressed(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "captain"
	sessionID := "sess-jkl"
	nonce := "cyc-20260613T000000-000005"

	handoffPath := filepath.Join(projectDir, "HANDOFF-captain.md")
	handoffContent := "# Handoff\n\n<!-- KEEPER:" + nonce + " -->\n\nContent."
	if err := os.WriteFile(handoffPath, []byte(handoffContent), 0o600); err != nil {
		t.Fatal(err)
	}

	marker := &keeper.RestartNowMarker{
		Nonce:       nonce,
		RequestedAt: time.Now().UTC().Add(-5 * time.Second),
		SessionID:   sessionID,
	}

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	var clearCalled bool
	_, c := restartNowCyclerConfig(
		projectDir, agent, em, spy, jc,
		true, true, false,
		sessionID, // lastFiredSID == sessionID → anti-loop
		func(_ string) (string, error) { return handoffContent, nil },
		fixedGauge(sessionID),
		func(_, _ string) (*keeper.RestartNowMarker, error) { return marker, nil },
		func(_, _ string) error { clearCalled = true; return nil },
	)

	ctx := context.Background()
	cf := &keeper.CtxFile{Pct: 50.0, SessionID: sessionID}
	if err := c.RunOnDemand(ctx, cf); err != nil {
		t.Fatalf("RunOnDemand unexpected error: %v", err)
	}

	if !clearCalled {
		t.Error("ClearRestartNowTrigger was not called (consume-once required)")
	}

	blocked := em.EventsOfType(core.EventTypeSessionKeeperRestartNowBlocked)
	if len(blocked) != 1 {
		t.Fatalf("expected 1 restart_now_blocked, got %d", len(blocked))
	}
}

// TestRestartNowMarker_RoundTrip verifies that WriteRestartNowMarker +
// ReadRestartNowMarker preserve all fields correctly (JSON round-trip).
func TestRestartNowMarker_RoundTrip(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "captain"

	want := &keeper.RestartNowMarker{
		Nonce:       "cyc-20260613T120000-000001",
		RequestedAt: time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC),
		SessionID:   "sess-roundtrip",
	}

	if err := keeper.WriteRestartNowMarker(projectDir, agent, want); err != nil {
		t.Fatalf("WriteRestartNowMarker: %v", err)
	}

	got, err := keeper.ReadRestartNowMarker(projectDir, agent)
	if err != nil {
		t.Fatalf("ReadRestartNowMarker: %v", err)
	}

	if got.Nonce != want.Nonce {
		t.Errorf("Nonce: got %q, want %q", got.Nonce, want.Nonce)
	}
	if !got.RequestedAt.Equal(want.RequestedAt) {
		t.Errorf("RequestedAt: got %v, want %v", got.RequestedAt, want.RequestedAt)
	}
	if got.SessionID != want.SessionID {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, want.SessionID)
	}
}

// TestRestartNowMarker_Atomic verifies that WriteRestartNowMarker actually
// creates the marker file and HasRestartNowTrigger detects it, and that
// ClearRestartNowTrigger removes it.
func TestRestartNowMarker_Atomic(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "captain"

	if keeper.HasRestartNowTrigger(projectDir, agent) {
		t.Fatal("HasRestartNowTrigger should be false before write")
	}

	m := &keeper.RestartNowMarker{
		Nonce:       "cyc-test",
		RequestedAt: time.Now().UTC(),
		SessionID:   "sess-x",
	}
	if err := keeper.WriteRestartNowMarker(projectDir, agent, m); err != nil {
		t.Fatalf("WriteRestartNowMarker: %v", err)
	}

	if !keeper.HasRestartNowTrigger(projectDir, agent) {
		t.Error("HasRestartNowTrigger should be true after write")
	}

	if err := keeper.ClearRestartNowTrigger(projectDir, agent); err != nil {
		t.Fatalf("ClearRestartNowTrigger: %v", err)
	}

	if keeper.HasRestartNowTrigger(projectDir, agent) {
		t.Error("HasRestartNowTrigger should be false after clear")
	}

	// ClearRestartNowTrigger should be idempotent.
	if err := keeper.ClearRestartNowTrigger(projectDir, agent); err != nil {
		t.Errorf("ClearRestartNowTrigger (idempotent): %v", err)
	}
}
