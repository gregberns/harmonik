package sentinel_test

// signals_test.go — unit tests for the stall-sentinel signal library.
//
// Replays synthetic event streams against ComputeSnapshot to verify:
//   - per-run last-event-age and phase transitions
//   - lane forward-progress rollups
//   - live-crew set derived from agent_presence beats
//   - ExpectsProgress predicate (Layer B false-positive guard)
//
// Spec: .kerf/works/stall-sentinel/02-analysis.md §Signal-library-core.
// Bead: hk-mxxsl.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/sentinel"
)

// --- helpers ---

// makeSignalsProjectDir creates a temp project with .harmonik/events/ ready.
func makeSignalsProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return dir
}

// signalsEventsPath returns the canonical events.jsonl path for a project dir.
func signalsEventsPath(projectDir string) string {
	return sentinel.EventsPathForProject(projectDir)
}

// appendEvent writes one JSONL event to path, with an optional RunID on the envelope.
func appendEvent(t *testing.T, path string, evType core.EventType, ts time.Time, runID *core.RunID, payload []byte) {
	t.Helper()
	// Must be a UUIDv7 (not a random v4): ComputeSnapshot derives its ScanAfter
	// cursor from scanStart via eventIDFloorForTime, a lexicographic UUIDv7
	// floor. A random v4 ID sorts before that floor ~50% of the time regardless
	// of ts, silently dropping the event and flaking window-based assertions.
	v7, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	id := core.EventID(v7)
	ev := core.Event{
		EventID:         id,
		SchemaVersion:   1,
		Type:            string(evType),
		TimestampWall:   ts,
		SourceSubsystem: "test",
		Payload:         payload,
		RunID:           runID,
	}
	line, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatalf("write event: %v", err)
	}
}

// newRunID generates a fresh core.RunID.
func newRunID() core.RunID {
	return core.RunID(uuid.New())
}

// mustMarshal marshals v to JSON or calls t.Fatal.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// --- tests ---

// TestComputeSnapshot_EmptyStream verifies that an active run with no events
// gets phase=started and last-event-age equal to time since StartedAt.
func TestComputeSnapshot_EmptyStream(t *testing.T) {
	t.Parallel()
	dir := makeSignalsProjectDir(t)
	eventsPath := signalsEventsPath(dir)

	runID := newRunID()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	startedAt := now.Add(-10 * time.Minute)

	activeRuns := []run.Record{
		{
			RunID:     runID.String(),
			BeadID:    "hk-test01",
			QueueName: "main",
			StartedAt: startedAt,
		},
	}

	snap := sentinel.ComputeSnapshot(
		context.Background(),
		eventsPath,
		activeRuns,
		15*time.Minute,
		nil,
		now,
	)

	sig, ok := snap.Runs[runID.String()]
	if !ok {
		t.Fatalf("run %s not in snapshot", runID)
	}
	if sig.Phase != sentinel.RunPhaseStarted {
		t.Errorf("phase = %s; want started", sig.Phase)
	}
	if sig.LastEventAt != startedAt {
		t.Errorf("LastEventAt = %v; want %v (startedAt baseline)", sig.LastEventAt, startedAt)
	}
	wantAge := now.Sub(startedAt)
	if sig.LastEventAge != wantAge {
		t.Errorf("LastEventAge = %v; want %v", sig.LastEventAge, wantAge)
	}
	// Lane should exist with no forward progress.
	lane, laneOK := snap.Lanes["main"]
	if !laneOK {
		t.Fatal("lane 'main' not in snapshot")
	}
	if !lane.LastForwardProgressAt.IsZero() {
		t.Errorf("LastForwardProgressAt should be zero for empty stream; got %v", lane.LastForwardProgressAt)
	}
	// A run in the active registry IS mid-flight (registry entry exists until completion).
	// ExpectsProgress must be true even with no events confirming it yet.
	if !lane.ExpectsProgress() {
		t.Error("ExpectsProgress should be true: run exists in registry (mid-flight by definition)")
	}
}

// TestComputeSnapshot_RunStarted verifies that a run_started event advances
// phase to started, updates lastEventAt, and marks lane forward progress.
func TestComputeSnapshot_RunStarted(t *testing.T) {
	t.Parallel()
	dir := makeSignalsProjectDir(t)
	eventsPath := signalsEventsPath(dir)

	runID := newRunID()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	startedAt := now.Add(-10 * time.Minute)
	eventAt := now.Add(-8 * time.Minute)

	activeRuns := []run.Record{
		{RunID: runID.String(), BeadID: "hk-aaa", QueueName: "main", StartedAt: startedAt},
	}

	rid := runID // copy for pointer
	appendEvent(t, eventsPath, core.EventTypeRunStarted, eventAt, &rid, mustMarshal(t, map[string]any{
		"run_id":           runID.String(),
		"workflow_id":      uuid.New().String(),
		"workflow_version": "v1",
		"workspace_path":   "/tmp/ws",
	}))

	snap := sentinel.ComputeSnapshot(context.Background(), eventsPath, activeRuns, 15*time.Minute, nil, now)

	sig := snap.Runs[runID.String()]
	if sig.Phase != sentinel.RunPhaseStarted {
		t.Errorf("phase = %s; want started", sig.Phase)
	}
	if sig.LastEventAt != eventAt {
		t.Errorf("LastEventAt = %v; want %v", sig.LastEventAt, eventAt)
	}

	// Lane forward progress should be set.
	lane := snap.Lanes["main"]
	if lane.LastForwardProgressAt != eventAt {
		t.Errorf("LastForwardProgressAt = %v; want %v", lane.LastForwardProgressAt, eventAt)
	}

	// mid-flight → ExpectsProgress true
	if !lane.ExpectsProgress() {
		t.Error("ExpectsProgress should be true: run is mid-flight")
	}
}

// TestComputeSnapshot_PhaseTransitions exercises the full phase ladder:
// started → in-implementation → verdict-fired → terminal.
func TestComputeSnapshot_PhaseTransitions(t *testing.T) {
	t.Parallel()
	dir := makeSignalsProjectDir(t)
	eventsPath := signalsEventsPath(dir)

	runID := newRunID()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	startedAt := now.Add(-30 * time.Minute)

	activeRuns := []run.Record{
		{RunID: runID.String(), BeadID: "hk-bbb", QueueName: "crew1", StartedAt: startedAt},
	}

	rid := runID
	t0 := startedAt.Add(1 * time.Minute)
	t1 := startedAt.Add(5 * time.Minute)
	t2 := startedAt.Add(10 * time.Minute)

	// run_started
	appendEvent(t, eventsPath, core.EventTypeRunStarted, t0, &rid, mustMarshal(t, map[string]any{
		"run_id": runID.String(), "workflow_id": uuid.New().String(),
		"workflow_version": "v1", "workspace_path": "/tmp/ws",
	}))

	// implementer_phase_complete
	appendEvent(t, eventsPath, core.EventTypeImplementerPhaseComplete, t1, &rid, mustMarshal(t, map[string]any{
		"run_id": runID.String(), "phase": "implementer",
	}))

	// reviewer_verdict
	verdictAt := t2
	appendEvent(t, eventsPath, core.EventTypeReviewerVerdict, verdictAt, &rid, mustMarshal(t, map[string]any{
		"run_id": runID.String(), "verdict": "REQUEST_CHANGES",
	}))

	snap := sentinel.ComputeSnapshot(context.Background(), eventsPath, activeRuns, 15*time.Minute, nil, now)

	sig := snap.Runs[runID.String()]
	if sig.Phase != sentinel.RunPhaseVerdictFired {
		t.Errorf("after reviewer_verdict, phase = %s; want verdict-fired", sig.Phase)
	}
	if sig.VerdictAt != verdictAt {
		t.Errorf("VerdictAt = %v; want %v", sig.VerdictAt, verdictAt)
	}

	// Now add run_completed and recompute.
	termAt := verdictAt.Add(5 * time.Minute)
	appendEvent(t, eventsPath, core.EventTypeRunCompleted, termAt, &rid, mustMarshal(t, map[string]any{
		"run_id": runID.String(),
	}))

	snap2 := sentinel.ComputeSnapshot(context.Background(), eventsPath, activeRuns, 15*time.Minute, nil, now)

	sig2 := snap2.Runs[runID.String()]
	if sig2.Phase != sentinel.RunPhaseTerminal {
		t.Errorf("after run_completed, phase = %s; want terminal", sig2.Phase)
	}
	// Terminal run → not in mid-flight list.
	lane2 := snap2.Lanes["crew1"]
	for _, id := range lane2.MidFlightRunIDs {
		if id == runID.String() {
			t.Error("terminal run should not appear in MidFlightRunIDs")
		}
	}
}

// TestComputeSnapshot_HeartbeatUpdatesAge verifies that agent_heartbeat events
// advance LastEventAt without changing the run phase.
func TestComputeSnapshot_HeartbeatUpdatesAge(t *testing.T) {
	t.Parallel()
	dir := makeSignalsProjectDir(t)
	eventsPath := signalsEventsPath(dir)

	runID := newRunID()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	startedAt := now.Add(-20 * time.Minute)
	hbAt := now.Add(-3 * time.Minute)

	activeRuns := []run.Record{
		{RunID: runID.String(), BeadID: "hk-ccc", QueueName: "main", StartedAt: startedAt},
	}

	rid := runID
	appendEvent(t, eventsPath, core.EventTypeAgentHeartbeat, hbAt, &rid, mustMarshal(t, map[string]any{
		"session_id": uuid.New().String(),
		"phase":      "reasoning",
	}))

	snap := sentinel.ComputeSnapshot(context.Background(), eventsPath, activeRuns, 15*time.Minute, nil, now)

	sig := snap.Runs[runID.String()]
	if sig.Phase != sentinel.RunPhaseStarted {
		t.Errorf("heartbeat must not change phase; got %s", sig.Phase)
	}
	if sig.LastEventAt != hbAt {
		t.Errorf("LastEventAt = %v; want %v (heartbeat time)", sig.LastEventAt, hbAt)
	}
	wantAge := now.Sub(hbAt)
	if sig.LastEventAge != wantAge {
		t.Errorf("LastEventAge = %v; want %v", sig.LastEventAge, wantAge)
	}
}

// TestComputeSnapshot_BeadClosedAdvancesLaneProgress verifies that a bead_closed
// event advances the lane's LastForwardProgressAt.
func TestComputeSnapshot_BeadClosedAdvancesLaneProgress(t *testing.T) {
	t.Parallel()
	dir := makeSignalsProjectDir(t)
	eventsPath := signalsEventsPath(dir)

	runID := newRunID()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	startedAt := now.Add(-25 * time.Minute)
	closeAt := now.Add(-5 * time.Minute)

	activeRuns := []run.Record{
		{RunID: runID.String(), BeadID: "hk-ddd", QueueName: "main", StartedAt: startedAt},
	}

	rid := runID
	appendEvent(t, eventsPath, core.EventTypeBeadClosed, closeAt, &rid, mustMarshal(t, map[string]any{
		"run_id":  runID.String(),
		"bead_id": "hk-ddd",
	}))

	snap := sentinel.ComputeSnapshot(context.Background(), eventsPath, activeRuns, 15*time.Minute, nil, now)

	lane := snap.Lanes["main"]
	if lane.LastForwardProgressAt != closeAt {
		t.Errorf("LastForwardProgressAt = %v; want %v (bead_closed time)", lane.LastForwardProgressAt, closeAt)
	}
}

// TestComputeSnapshot_LiveCrew verifies that recent agent_presence beats populate
// LiveCrews, and stale beats are excluded.
func TestComputeSnapshot_LiveCrew(t *testing.T) {
	t.Parallel()
	dir := makeSignalsProjectDir(t)
	eventsPath := signalsEventsPath(dir)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ttl := 10 * time.Minute

	// Active agent: last_seen 5 min ago (within TTL).
	recentAt := now.Add(-5 * time.Minute)
	// Stale agent: last_seen 20 min ago (beyond TTL).
	staleAt := now.Add(-20 * time.Minute)

	appendPresence := func(agent string, ts time.Time) {
		appendEvent(t, eventsPath, core.EventType("agent_presence"), ts, nil, mustMarshal(t, map[string]any{
			"agent":     agent,
			"status":    "online",
			"last_seen": ts.UTC().Format(time.RFC3339),
			"reason":    "refresh",
		}))
	}

	appendPresence("captain", recentAt)
	appendPresence("stale-crew", staleAt)

	snap := sentinel.ComputeSnapshot(context.Background(), eventsPath, nil, ttl, nil, now)

	// captain should be live; stale-crew should not.
	found := make(map[string]bool)
	// LiveCrews should be in all lanes — but here no lanes exist (no runs).
	// Let's verify via a lane we inject.
	snap2 := sentinel.ComputeSnapshot(context.Background(), eventsPath, nil, ttl,
		[]sentinel.LaneStateInput{{LaneName: "main", QueueNonEmpty: true}},
		now,
	)
	lane := snap2.Lanes["main"]
	for _, c := range lane.LiveCrews {
		found[c] = true
	}
	_ = snap // suppress unused warning

	if !found["captain"] {
		t.Error("captain (recent presence) should be in LiveCrews")
	}
	if found["stale-crew"] {
		t.Error("stale-crew (beyond TTL) should not be in LiveCrews")
	}
}

// TestComputeSnapshot_PresenceOfflineRemovesAgent verifies that an offline beat
// removes the agent from the live set.
func TestComputeSnapshot_PresenceOfflineRemovesAgent(t *testing.T) {
	t.Parallel()
	dir := makeSignalsProjectDir(t)
	eventsPath := signalsEventsPath(dir)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ttl := 15 * time.Minute

	joinAt := now.Add(-10 * time.Minute)
	leaveAt := now.Add(-5 * time.Minute)

	appendEvent(t, eventsPath, core.EventType("agent_presence"), joinAt, nil, mustMarshal(t, map[string]any{
		"agent":     "paul",
		"status":    "online",
		"last_seen": joinAt.UTC().Format(time.RFC3339),
		"reason":    "join",
	}))
	appendEvent(t, eventsPath, core.EventType("agent_presence"), leaveAt, nil, mustMarshal(t, map[string]any{
		"agent":     "paul",
		"status":    "offline",
		"last_seen": leaveAt.UTC().Format(time.RFC3339),
		"reason":    "leave",
	}))

	snap := sentinel.ComputeSnapshot(context.Background(), eventsPath, nil, ttl,
		[]sentinel.LaneStateInput{{LaneName: "main"}},
		now,
	)
	lane := snap.Lanes["main"]
	for _, c := range lane.LiveCrews {
		if c == "paul" {
			t.Error("paul sent offline beat; should not be in LiveCrews")
		}
	}
}

// TestComputeSnapshot_ExpectsProgress_FalsePositiveGuard is the critical
// negative test: a correctly-idle crew (no mid-flight runs, no queue items,
// no assigned beads) must NOT trip ExpectsProgress, ensuring Layer B does
// not fire on a legitimately idle lane.
// Spec: 02-analysis.md §Layer B "critical false-positive guard".
func TestComputeSnapshot_ExpectsProgress_FalsePositiveGuard(t *testing.T) {
	t.Parallel()
	dir := makeSignalsProjectDir(t)
	eventsPath := signalsEventsPath(dir)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// Crew is live but queue is empty and no assigned bead.
	appendEvent(t, eventsPath, core.EventType("agent_presence"), now.Add(-2*time.Minute), nil,
		mustMarshal(t, map[string]any{
			"agent":     "crew-idle",
			"status":    "online",
			"last_seen": now.Add(-2 * time.Minute).UTC().Format(time.RFC3339),
			"reason":    "refresh",
		}),
	)

	snap := sentinel.ComputeSnapshot(context.Background(), eventsPath, nil, 15*time.Minute,
		[]sentinel.LaneStateInput{{LaneName: "idle-lane", QueueNonEmpty: false, HasAssignedBead: false}},
		now,
	)

	lane := snap.Lanes["idle-lane"]
	if lane.ExpectsProgress() {
		t.Error("correctly-idle crew must NOT trip ExpectsProgress (false-positive guard violation)")
	}
	if len(lane.LiveCrews) == 0 {
		t.Error("live crew should appear in LiveCrews even for idle lane")
	}
}

// TestComputeSnapshot_ExpectsProgress_QueueNonEmpty verifies that an injected
// QueueNonEmpty=true makes ExpectsProgress return true.
func TestComputeSnapshot_ExpectsProgress_QueueNonEmpty(t *testing.T) {
	t.Parallel()
	dir := makeSignalsProjectDir(t)
	eventsPath := signalsEventsPath(dir)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	snap := sentinel.ComputeSnapshot(context.Background(), eventsPath, nil, 15*time.Minute,
		[]sentinel.LaneStateInput{{LaneName: "q-lane", QueueNonEmpty: true}},
		now,
	)

	lane := snap.Lanes["q-lane"]
	if !lane.ExpectsProgress() {
		t.Error("QueueNonEmpty=true should make ExpectsProgress return true")
	}
}

// TestComputeSnapshot_ExpectsProgress_AssignedBead verifies that HasAssignedBead=true
// makes ExpectsProgress return true.
func TestComputeSnapshot_ExpectsProgress_AssignedBead(t *testing.T) {
	t.Parallel()
	dir := makeSignalsProjectDir(t)
	eventsPath := signalsEventsPath(dir)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	snap := sentinel.ComputeSnapshot(context.Background(), eventsPath, nil, 15*time.Minute,
		[]sentinel.LaneStateInput{{LaneName: "assign-lane", HasAssignedBead: true}},
		now,
	)

	lane := snap.Lanes["assign-lane"]
	if !lane.ExpectsProgress() {
		t.Error("HasAssignedBead=true should make ExpectsProgress return true")
	}
}

// TestComputeSnapshot_MultipleRuns verifies correct isolation of two
// concurrent runs in the same lane.
func TestComputeSnapshot_MultipleRuns(t *testing.T) {
	t.Parallel()
	dir := makeSignalsProjectDir(t)
	eventsPath := signalsEventsPath(dir)

	runA := newRunID()
	runB := newRunID()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	startedAt := now.Add(-20 * time.Minute)

	activeRuns := []run.Record{
		{RunID: runA.String(), BeadID: "hk-aaa", QueueName: "team", StartedAt: startedAt},
		{RunID: runB.String(), BeadID: "hk-bbb", QueueName: "team", StartedAt: startedAt},
	}

	ridA, ridB := runA, runB

	// Run A: heartbeat at t-5min
	hbAt := now.Add(-5 * time.Minute)
	appendEvent(t, eventsPath, core.EventTypeAgentHeartbeat, hbAt, &ridA, mustMarshal(t, map[string]any{
		"session_id": uuid.New().String(), "phase": "tool_call",
	}))

	// Run B: reviewer_verdict at t-3min
	verdictAt := now.Add(-3 * time.Minute)
	appendEvent(t, eventsPath, core.EventTypeReviewerVerdict, verdictAt, &ridB, mustMarshal(t, map[string]any{
		"run_id": runB.String(), "verdict": "APPROVE",
	}))

	snap := sentinel.ComputeSnapshot(context.Background(), eventsPath, activeRuns, 15*time.Minute, nil, now)

	sigA := snap.Runs[runA.String()]
	if sigA.Phase != sentinel.RunPhaseStarted {
		t.Errorf("run A: phase = %s; want started", sigA.Phase)
	}
	if sigA.LastEventAt != hbAt {
		t.Errorf("run A: LastEventAt = %v; want %v", sigA.LastEventAt, hbAt)
	}

	sigB := snap.Runs[runB.String()]
	if sigB.Phase != sentinel.RunPhaseVerdictFired {
		t.Errorf("run B: phase = %s; want verdict-fired", sigB.Phase)
	}
	if sigB.VerdictAt != verdictAt {
		t.Errorf("run B: VerdictAt = %v; want %v", sigB.VerdictAt, verdictAt)
	}

	// Both runs mid-flight → lane expects progress.
	lane := snap.Lanes["team"]
	if !lane.ExpectsProgress() {
		t.Error("two mid-flight runs → ExpectsProgress should be true")
	}
	if len(lane.MidFlightRunIDs) != 2 {
		t.Errorf("MidFlightRunIDs len = %d; want 2", len(lane.MidFlightRunIDs))
	}
}

// TestComputeSnapshot_UnknownRunEvents verifies that events for runs not in
// the active registry are silently ignored.
func TestComputeSnapshot_UnknownRunEvents(t *testing.T) {
	t.Parallel()
	dir := makeSignalsProjectDir(t)
	eventsPath := signalsEventsPath(dir)

	ghostID := newRunID()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// Emit an event for a run that is NOT in activeRuns.
	rid := ghostID
	appendEvent(t, eventsPath, core.EventTypeAgentHeartbeat, now.Add(-1*time.Minute), &rid, mustMarshal(t, map[string]any{
		"session_id": uuid.New().String(), "phase": "reasoning",
	}))

	snap := sentinel.ComputeSnapshot(context.Background(), eventsPath, nil, 15*time.Minute, nil, now)

	if _, ok := snap.Runs[ghostID.String()]; ok {
		t.Error("ghost run (not in registry) should not appear in snapshot")
	}
}

// TestRunPhase_String verifies the human-readable labels.
func TestRunPhase_String(t *testing.T) {
	t.Parallel()
	cases := []struct {
		phase sentinel.RunPhase
		want  string
	}{
		{sentinel.RunPhaseStarted, "started"},
		{sentinel.RunPhaseInImplementation, "in-implementation"},
		{sentinel.RunPhaseVerdictFired, "verdict-fired"},
		{sentinel.RunPhaseTerminal, "terminal"},
	}
	for _, tc := range cases {
		if got := tc.phase.String(); got != tc.want {
			t.Errorf("RunPhase(%d).String() = %q; want %q", int(tc.phase), got, tc.want)
		}
	}
}
