package daemon

// orphansweep_pl006d_hkqp3_test.go — unit tests for the PL-006d crew and
// captain sentinel exemptions (hk-qp3).
//
// What is tested:
//
//   - probeCaptainSentinel: absent sentinel → false.
//   - probeCaptainSentinel: sentinel present + live PID → true, sentinel kept.
//   - probeCaptainSentinel: sentinel present + dead PID → false, sentinel removed.
//   - probeCaptainSentinel: sentinel present + missing pidfile → false, sentinel removed.
//
//   - probeCrewRegistrySessions: empty registry → 0 skipped, empty excludes.
//   - probeCrewRegistrySessions: crew session absent from snapshot → 0 skipped (launch-in-flight).
//   - probeCrewRegistrySessions: crew session present + live PID → 1 skipped, added to excludes.
//   - probeCrewRegistrySessions: crew session present + dead PID → 0 skipped (let sweep handle it).
//   - probeCrewRegistrySessions: nil adapter → 0 skipped.
//
// Spec refs:
//   - process-lifecycle.md §4.2 PL-006d mechanism (ii) — captain sentinel.
//   - process-lifecycle.md §4.2 PL-006d mechanism (iii) — crew registry probe.
//
// Bead: hk-qp3.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// Captain sentinel fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

func hkqp3WriteCaptainSentinel(t *testing.T, projectDir string) {
	t.Helper()
	cognitionDir := filepath.Join(projectDir, ".harmonik", "cognition")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(cognitionDir, 0o755); err != nil {
		t.Fatalf("hkqp3WriteCaptainSentinel: MkdirAll: %v", err)
	}
	sentinelPath := filepath.Join(cognitionDir, "captain.sentinel")
	if err := os.WriteFile(sentinelPath, []byte("schema_version=1\n"), 0o644); err != nil {
		t.Fatalf("hkqp3WriteCaptainSentinel: WriteFile: %v", err)
	}
}

func hkqp3WriteCaptainPidfile(t *testing.T, projectDir string, pid int) {
	t.Helper()
	cognitionDir := filepath.Join(projectDir, ".harmonik", "cognition")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(cognitionDir, 0o755); err != nil {
		t.Fatalf("hkqp3WriteCaptainPidfile: MkdirAll: %v", err)
	}
	pidfilePath := filepath.Join(cognitionDir, "captain.pid")
	if err := os.WriteFile(pidfilePath, []byte(fmt.Sprintf("%d\n", pid)), 0o644); err != nil {
		t.Fatalf("hkqp3WriteCaptainPidfile: WriteFile: %v", err)
	}
}

func hkqp3CaptainSentinelExists(projectDir string) bool {
	sentinelPath := filepath.Join(projectDir, ".harmonik", "cognition", "captain.sentinel")
	_, err := os.Stat(sentinelPath)
	return err == nil
}

// ─────────────────────────────────────────────────────────────────────────────
// probeCaptainSentinel unit tests
// ─────────────────────────────────────────────────────────────────────────────

// TestHKQP3_ProbeCaptainSentinel_Absent verifies that when no captain.sentinel
// file exists, probeCaptainSentinel returns false.
func TestHKQP3_ProbeCaptainSentinel_Absent(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)

	live := probeCaptainSentinel(context.Background(), projectDir, hkqp3ProjectHash, nil, nil, nil)
	if live {
		t.Error("absent sentinel: live = true, want false")
	}
}

// TestHKQP3_ProbeCaptainSentinel_LivePID verifies that when captain.sentinel is
// present and captain.pid holds the test process's own PID (always live),
// probeCaptainSentinel returns true and does NOT remove the sentinel.
func TestHKQP3_ProbeCaptainSentinel_LivePID(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	hkqp3WriteCaptainSentinel(t, projectDir)
	hkqp3WriteCaptainPidfile(t, projectDir, os.Getpid())

	live := probeCaptainSentinel(context.Background(), projectDir, hkqp3ProjectHash, nil, nil, nil)
	if !live {
		t.Error("live PID: live = false, want true")
	}
	if !hkqp3CaptainSentinelExists(projectDir) {
		t.Error("live PID: sentinel was removed; must be kept when PID is live")
	}
}

// TestHKQP3_ProbeCaptainSentinel_DeadPID verifies that when captain.sentinel is
// present but the PID is dead, probeCaptainSentinel returns false and removes
// the stale sentinel.
func TestHKQP3_ProbeCaptainSentinel_DeadPID(t *testing.T) {
	t.Parallel()

	const deadPID = 99989
	if daemonOrphanSweepIsPidLive(deadPID) {
		t.Skipf("PID %d is live on this host; skipping", deadPID)
	}

	projectDir := daemonOrphanSweepTempProjectDir(t)
	hkqp3WriteCaptainSentinel(t, projectDir)
	hkqp3WriteCaptainPidfile(t, projectDir, deadPID)

	live := probeCaptainSentinel(context.Background(), projectDir, hkqp3ProjectHash, nil, nil, nil)
	if live {
		t.Error("dead PID: live = true, want false")
	}
	if hkqp3CaptainSentinelExists(projectDir) {
		t.Error("dead PID: sentinel file still exists; stale sentinel must be removed")
	}
}

// TestHKQP3_ProbeCaptainSentinel_MissingPidfile verifies that when
// captain.sentinel is present but captain.pid is absent, probeCaptainSentinel
// treats it as stale and removes the sentinel.
func TestHKQP3_ProbeCaptainSentinel_MissingPidfile(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	hkqp3WriteCaptainSentinel(t, projectDir)
	// No pidfile written.

	live := probeCaptainSentinel(context.Background(), projectDir, hkqp3ProjectHash, nil, nil, nil)
	if live {
		t.Error("missing pidfile: live = true, want false")
	}
	if hkqp3CaptainSentinelExists(projectDir) {
		t.Error("missing pidfile: sentinel file still exists after removal")
	}
}

// TestHKWUXG_ProbeCaptainSentinel_StalePidLiveSession reproduces the "captain
// dies suddenly" chain (hk-wuxg): the recorded captain.pid is STALE/dead (its
// pane PID churned across a keeper wind-down cycle) but the captain tmux session
// is genuinely alive. The hardened probe MUST trust the live session, return
// true, and KEEP the sentinel — so the next orphan sweep does not reap the live
// captain (the proximate cause of daemon_orphan_sweep_completed with
// captain_sessions_skipped:0).
func TestHKWUXG_ProbeCaptainSentinel_StalePidLiveSession(t *testing.T) {
	t.Parallel()

	const deadPID = 99989
	if daemonOrphanSweepIsPidLive(deadPID) {
		t.Skipf("PID %d is live on this host; skipping", deadPID)
	}

	projectDir := daemonOrphanSweepTempProjectDir(t)
	hkqp3WriteCaptainSentinel(t, projectDir)
	// captain.pid records a DEAD pid (stale write-once value).
	hkqp3WriteCaptainPidfile(t, projectDir, deadPID)

	// The captain tmux session is present in the snapshot with a LIVE pane PID.
	captainSession := lifecycle.TmuxSessionName(hkqp3ProjectHash, "captain")
	sessionSnapshot := map[string]struct{}{captainSession: {}}
	adapter := &hkqp3FakeAdapter{sessions: []string{captainSession}, panePID: os.Getpid()}

	live := probeCaptainSentinel(context.Background(), projectDir, hkqp3ProjectHash, adapter, nil, sessionSnapshot)
	if !live {
		t.Error("stale pid + live session: live = false, want true (must not reap a live captain)")
	}
	if !hkqp3CaptainSentinelExists(projectDir) {
		t.Error("stale pid + live session: sentinel was removed; must survive a live session with a stale pid")
	}
}

// TestHKWUXG_ProbeCaptainSentinel_SessionGoneFallbackPidLive verifies the
// fallback: the captain session is absent from the snapshot (e.g. no-tmux
// daemon, or pre-snapshot launch window) but the recorded captain.pid is live →
// still protected.
func TestHKWUXG_ProbeCaptainSentinel_SessionGoneFallbackPidLive(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	hkqp3WriteCaptainSentinel(t, projectDir)
	hkqp3WriteCaptainPidfile(t, projectDir, os.Getpid())

	// Adapter present but the captain session is NOT in the snapshot.
	adapter := &hkqp3FakeAdapter{sessions: []string{}, panePID: 0}
	sessionSnapshot := map[string]struct{}{}

	live := probeCaptainSentinel(context.Background(), projectDir, hkqp3ProjectHash, adapter, nil, sessionSnapshot)
	if !live {
		t.Error("session absent + live recorded pid: live = false, want true (fallback must protect)")
	}
	if !hkqp3CaptainSentinelExists(projectDir) {
		t.Error("session absent + live recorded pid: sentinel was removed; must be kept")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// probeCrewRegistrySessions stub adapter
// ─────────────────────────────────────────────────────────────────────────────

// hkqp3FakeAdapter is a minimal ltmux.Adapter for testing probeCrewRegistrySessions.
// ListSessions returns a fixed session list; WindowPanePID returns a fixed PID;
// all other methods are no-op stubs.
type hkqp3FakeAdapter struct {
	mu       sync.Mutex
	sessions []string
	panePID  int // returned by WindowPanePID (0 = invalid/dead)
}

func (a *hkqp3FakeAdapter) ListSessions(_ context.Context) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.sessions))
	copy(out, a.sessions)
	return out, nil
}

func (a *hkqp3FakeAdapter) WindowPanePID(_ context.Context, _ ltmux.WindowHandle) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.panePID, nil
}

func (a *hkqp3FakeAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *hkqp3FakeAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (a *hkqp3FakeAdapter) NewWindowIn(_ context.Context, _ ltmux.NewWindowIn) ltmux.Outcome {
	return ltmux.Outcome{}
}
func (a *hkqp3FakeAdapter) KillWindow(_ context.Context, _ ltmux.WindowHandle) error { return nil }
func (a *hkqp3FakeAdapter) KillSession(_ context.Context, _ string) error            { return nil }
func (a *hkqp3FakeAdapter) WindowPaneID(_ context.Context, _ ltmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *hkqp3FakeAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error { return nil }
func (a *hkqp3FakeAdapter) PasteBuffer(_ context.Context, _, _ string) error       { return nil }
func (a *hkqp3FakeAdapter) SendKeysLiteral(_ context.Context, _, _ string) error   { return nil }
func (a *hkqp3FakeAdapter) SendKeysEnter(_ context.Context, _ string) error        { return nil }
func (a *hkqp3FakeAdapter) SendKeysQuit(_ context.Context, _ string) error         { return nil }
func (a *hkqp3FakeAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

// ensure compile-time satisfaction
var _ ltmux.Adapter = (*hkqp3FakeAdapter)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// probeCrewRegistrySessions unit tests
// ─────────────────────────────────────────────────────────────────────────────

const hkqp3ProjectHash = core.ProjectHash("aabbccdd1122")

// TestHKQP3_ProbeCrewRegistrySessions_EmptyRegistry verifies that an empty
// crew registry yields 0 skipped and no additions to excludeSessions.
func TestHKQP3_ProbeCrewRegistrySessions_EmptyRegistry(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	adapter := &hkqp3FakeAdapter{sessions: nil, panePID: os.Getpid()}
	sessionSnapshot := map[string]struct{}{}
	excludeSessions := map[string]struct{}{}

	skipped := probeCrewRegistrySessions(
		context.Background(), projectDir, hkqp3ProjectHash,
		adapter, nil, sessionSnapshot, excludeSessions,
	)

	if skipped != 0 {
		t.Errorf("empty registry: skipped = %d, want 0", skipped)
	}
	if len(excludeSessions) != 0 {
		t.Errorf("empty registry: excludeSessions len = %d, want 0", len(excludeSessions))
	}
}

// TestHKQP3_ProbeCrewRegistrySessions_SessionAbsent verifies that when a crew
// record exists but the session is absent from the tmux snapshot, the session
// is NOT added to excludeSessions (launch-in-flight; conservative skip).
func TestHKQP3_ProbeCrewRegistrySessions_SessionAbsent(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	// Write a crew record.
	if err := crew.Write(projectDir, crew.Record{Name: "alpha", SessionID: "sid-1", Queue: "q"}); err != nil {
		t.Fatalf("crew.Write: %v", err)
	}

	crewSession := lifecycle.TmuxSessionName(hkqp3ProjectHash, "crew-alpha")

	// Snapshot does NOT contain the crew session.
	adapter := &hkqp3FakeAdapter{sessions: []string{}, panePID: os.Getpid()}
	sessionSnapshot := map[string]struct{}{} // absent
	excludeSessions := map[string]struct{}{}

	skipped := probeCrewRegistrySessions(
		context.Background(), projectDir, hkqp3ProjectHash,
		adapter, nil, sessionSnapshot, excludeSessions,
	)

	if skipped != 0 {
		t.Errorf("session absent: skipped = %d, want 0 (launch-in-flight must not be exempted)", skipped)
	}
	if _, ok := excludeSessions[crewSession]; ok {
		t.Errorf("session absent: %q added to excludeSessions; must not be added", crewSession)
	}
}

// TestHKQP3_ProbeCrewRegistrySessions_LiveSession verifies that when a crew
// record exists, its session is in the snapshot, and the pane PID is the test
// process's own PID (always live), the session is added to excludeSessions
// and 1 is returned.
func TestHKQP3_ProbeCrewRegistrySessions_LiveSession(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	if err := crew.Write(projectDir, crew.Record{Name: "beta", SessionID: "sid-2", Queue: "q"}); err != nil {
		t.Fatalf("crew.Write: %v", err)
	}

	crewSession := lifecycle.TmuxSessionName(hkqp3ProjectHash, "crew-beta")

	// Snapshot contains the crew session; adapter returns a live PID.
	sessionSnapshot := map[string]struct{}{crewSession: {}}
	adapter := &hkqp3FakeAdapter{sessions: []string{crewSession}, panePID: os.Getpid()}
	excludeSessions := map[string]struct{}{}

	skipped := probeCrewRegistrySessions(
		context.Background(), projectDir, hkqp3ProjectHash,
		adapter, nil, sessionSnapshot, excludeSessions,
	)

	if skipped != 1 {
		t.Errorf("live session: skipped = %d, want 1", skipped)
	}
	if _, ok := excludeSessions[crewSession]; !ok {
		t.Errorf("live session: %q not in excludeSessions; must be added", crewSession)
	}
}

// TestHKQP3_ProbeCrewRegistrySessions_DeadPID verifies that when a crew
// session is in the snapshot but its pane PID is 0 (dead/invalid), the session
// is NOT added to excludeSessions and 0 is returned.
// crew.Remove must NOT be called (the dead-PID branch must not GC the record).
func TestHKQP3_ProbeCrewRegistrySessions_DeadPID(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	if err := crew.Write(projectDir, crew.Record{Name: "gamma", SessionID: "sid-3", Queue: "q"}); err != nil {
		t.Fatalf("crew.Write: %v", err)
	}

	crewSession := lifecycle.TmuxSessionName(hkqp3ProjectHash, "crew-gamma")

	// Snapshot contains the crew session; adapter returns PID=0 (dead/invalid).
	sessionSnapshot := map[string]struct{}{crewSession: {}}
	adapter := &hkqp3FakeAdapter{sessions: []string{crewSession}, panePID: 0}
	excludeSessions := map[string]struct{}{}

	skipped := probeCrewRegistrySessions(
		context.Background(), projectDir, hkqp3ProjectHash,
		adapter, nil, sessionSnapshot, excludeSessions,
	)

	if skipped != 0 {
		t.Errorf("dead PID: skipped = %d, want 0", skipped)
	}
	if _, ok := excludeSessions[crewSession]; ok {
		t.Errorf("dead PID: %q added to excludeSessions; dead session must not be exempted", crewSession)
	}

	// Verify crew record was NOT removed (REVIEW-FIX: no crew.Remove in sweep).
	rec, loadErr := crew.Load(projectDir, "gamma")
	if loadErr != nil {
		t.Errorf("dead PID: crew.Load after probe: %v — crew.Remove must NOT be called from sweep", loadErr)
	}
	if rec.Name != "gamma" {
		t.Errorf("dead PID: crew record name = %q, want %q", rec.Name, "gamma")
	}
}

// TestHKQP3_ProbeCrewRegistrySessions_NilAdapter verifies that a nil adapter
// returns 0 without panicking.
func TestHKQP3_ProbeCrewRegistrySessions_NilAdapter(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	if err := crew.Write(projectDir, crew.Record{Name: "delta", SessionID: "sid-4", Queue: "q"}); err != nil {
		t.Fatalf("crew.Write: %v", err)
	}

	sessionSnapshot := map[string]struct{}{}
	excludeSessions := map[string]struct{}{}

	skipped := probeCrewRegistrySessions(
		context.Background(), projectDir, hkqp3ProjectHash,
		nil, nil, sessionSnapshot, excludeSessions,
	)

	if skipped != 0 {
		t.Errorf("nil adapter: skipped = %d, want 0", skipped)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// hk-aoapq: live-session fallback when registry is cold/empty/corrupt
// ─────────────────────────────────────────────────────────────────────────────

// TestHKAOAPQ_LiveSessionFallback_EmptyCrewDir reproduces the exact incident
// scenario (hk-aoapq): a live crew tmux session exists in the snapshot but the
// .harmonik/crew/ directory is empty (crew.json not written yet or wiped).
// The live-session fallback MUST protect the session and return skipped >= 1.
func TestHKAOAPQ_LiveSessionFallback_EmptyCrewDir(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	// No crew JSON files — crew.List returns nil, nil.

	crewSession := lifecycle.TmuxSessionName(hkqp3ProjectHash, "crew-leto")

	sessionSnapshot := map[string]struct{}{crewSession: {}}
	adapter := &hkqp3FakeAdapter{sessions: []string{crewSession}, panePID: os.Getpid()}
	excludeSessions := map[string]struct{}{}

	skipped := probeCrewRegistrySessions(
		context.Background(), projectDir, hkqp3ProjectHash,
		adapter, nil, sessionSnapshot, excludeSessions,
	)

	if skipped != 1 {
		t.Errorf("empty crew dir + live session: skipped = %d, want 1 (live-session fallback must protect)", skipped)
	}
	if _, ok := excludeSessions[crewSession]; !ok {
		t.Errorf("empty crew dir: %q not in excludeSessions; must be protected by live-session fallback", crewSession)
	}
}

// TestHKAOAPQ_LiveSessionFallback_EmptyJSON reproduces the truncation scenario:
// the crew JSON file exists on disk but is 0-byte (concurrent write / partial
// fsync at restart time). crew.List returns a stub Record{Name: "leto"} which
// allows the registry pass to probe the session directly. The session MUST be
// protected and skipped = 1 returned.
func TestHKAOAPQ_LiveSessionFallback_EmptyJSON(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)

	// Write a 0-byte crew JSON to simulate truncation.
	crewDir := filepath.Join(projectDir, ".harmonik", "crew")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(crewDir, 0o755); err != nil {
		t.Fatalf("MkdirAll crew dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(crewDir, "leto.json"), []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFile empty crew json: %v", err)
	}

	crewSession := lifecycle.TmuxSessionName(hkqp3ProjectHash, "crew-leto")

	sessionSnapshot := map[string]struct{}{crewSession: {}}
	adapter := &hkqp3FakeAdapter{sessions: []string{crewSession}, panePID: os.Getpid()}
	excludeSessions := map[string]struct{}{}

	skipped := probeCrewRegistrySessions(
		context.Background(), projectDir, hkqp3ProjectHash,
		adapter, nil, sessionSnapshot, excludeSessions,
	)

	if skipped != 1 {
		t.Errorf("empty crew json + live session: skipped = %d, want 1 (must not reap live crew on registry corruption)", skipped)
	}
	if _, ok := excludeSessions[crewSession]; !ok {
		t.Errorf("empty crew json: %q not in excludeSessions; must be protected", crewSession)
	}
}

// TestHKAOAPQ_LiveSessionFallback_CorruptJSON verifies that a corrupt (malformed)
// crew JSON file does not cause the sweep to reap the live crew session.
// crew.List returns a stub which the registry pass or the live-session fallback
// converts to protection (hk-aoapq).
func TestHKAOAPQ_LiveSessionFallback_CorruptJSON(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)

	crewDir := filepath.Join(projectDir, ".harmonik", "crew")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(crewDir, 0o755); err != nil {
		t.Fatalf("MkdirAll crew dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(crewDir, "admiral.json"), []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile corrupt crew json: %v", err)
	}

	crewSession := lifecycle.TmuxSessionName(hkqp3ProjectHash, "crew-admiral")

	sessionSnapshot := map[string]struct{}{crewSession: {}}
	adapter := &hkqp3FakeAdapter{sessions: []string{crewSession}, panePID: os.Getpid()}
	excludeSessions := map[string]struct{}{}

	skipped := probeCrewRegistrySessions(
		context.Background(), projectDir, hkqp3ProjectHash,
		adapter, nil, sessionSnapshot, excludeSessions,
	)

	if skipped != 1 {
		t.Errorf("corrupt crew json + live session: skipped = %d, want 1", skipped)
	}
	if _, ok := excludeSessions[crewSession]; !ok {
		t.Errorf("corrupt crew json: %q not in excludeSessions; must be protected", crewSession)
	}
}

// TestHKAOAPQ_LiveSessionFallback_MultipleSessions verifies that multiple live
// crew sessions are all protected when the crew directory is empty (hk-aoapq).
func TestHKAOAPQ_LiveSessionFallback_MultipleSessions(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	// No crew JSON files.

	letoSession := lifecycle.TmuxSessionName(hkqp3ProjectHash, "crew-leto")
	admiralSession := lifecycle.TmuxSessionName(hkqp3ProjectHash, "crew-admiral")
	// Include an unrelated session to confirm the prefix filter works.
	unrelatedSession := lifecycle.TmuxSessionName(hkqp3ProjectHash, "default")

	sessionSnapshot := map[string]struct{}{
		letoSession:      {},
		admiralSession:   {},
		unrelatedSession: {},
	}
	adapter := &hkqp3FakeAdapter{
		sessions: []string{letoSession, admiralSession, unrelatedSession},
		panePID:  os.Getpid(),
	}
	excludeSessions := map[string]struct{}{}

	skipped := probeCrewRegistrySessions(
		context.Background(), projectDir, hkqp3ProjectHash,
		adapter, nil, sessionSnapshot, excludeSessions,
	)

	if skipped != 2 {
		t.Errorf("two crew sessions: skipped = %d, want 2", skipped)
	}
	if _, ok := excludeSessions[letoSession]; !ok {
		t.Errorf("%q not in excludeSessions", letoSession)
	}
	if _, ok := excludeSessions[admiralSession]; !ok {
		t.Errorf("%q not in excludeSessions", admiralSession)
	}
	if _, ok := excludeSessions[unrelatedSession]; ok {
		t.Errorf("unrelated session %q was incorrectly added to excludeSessions", unrelatedSession)
	}
}
