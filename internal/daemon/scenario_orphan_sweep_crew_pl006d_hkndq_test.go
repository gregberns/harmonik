package daemon

// scenario_orphan_sweep_crew_pl006d_hkndq_test.go — scenario tests for the
// PL-006d crew-session orphan-sweep exclusion (hk-ndq).
//
// # Scenarios
//
//   Scenario A — LIVE crew skipped: crew "alpha" is running; crew_sessions_skipped=1;
//                session NOT killed via either sweep path.
//
//   Scenario B — LIVE crew idle-at-zsh skipped: crew "bravo" has only a "zsh" window
//                (which the generic all-zsh classifier in ltmux.SweepOrphanTmuxSessions
//                would kill as an idle-shell orphan), but because the first-pane PID
//                is live the crew registry probe (mechanism iii) exempts it;
//                session NOT killed despite the all-zsh state.
//
//   Scenario C — DEAD crew reaped, registry preserved: crew "charlie"'s first-pane PID
//                is 0 (dead/invalid); crew_sessions_skipped=0; the session IS killed
//                by the generic sweep; REVIEW-FIX: crew.Remove is NOT called —
//                the registry record survives the sweep so crew restart can re-register.
//
//   Scenario D — stop-in-flight not reaped (REVIEW-FIX coverage): crew "delta" has a
//                registry record but its session is absent from the tmux snapshot
//                (mid keeper-restart / stop-in-flight interleaving); the probe skips
//                it conservatively; session NOT killed; registry NOT removed.
//
//   Scenario E — payload mapping: crew_sessions_skipped flows to ToPayload correctly.
//
// # Spec refs
//
//   process-lifecycle.md §4.2 PL-006d mechanism (iii) — crew registry-record +
//   live-pane-PID probe.
//
// # Bead
//
//   hk-ndq.
//
// # Helper prefix
//
//   All package-level identifiers in this file use the "hkndq" prefix.

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// hkndqFakeAdapter — injectable ltmux.Adapter for crew-sweep tests
// ─────────────────────────────────────────────────────────────────────────────

// hkndqFakeAdapter is a minimal ltmux.Adapter for testing crew-session
// exclusion within RunOrphanSweep. It returns fixed sessions, a fixed
// first-pane PID, and a fixed window list.  KillSession appends to killed so
// tests can assert which sessions the adapter path killed.
type hkndqFakeAdapter struct {
	mu       sync.Mutex
	sessions []string
	panePID  int      // returned by WindowPanePID for every handle
	windows  []string // returned by ListWindows for every session
	killed   []string // sessions killed via KillSession
}

func (a *hkndqFakeAdapter) ListSessions(_ context.Context) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.sessions))
	copy(out, a.sessions)
	return out, nil
}

func (a *hkndqFakeAdapter) WindowPanePID(_ context.Context, _ ltmux.WindowHandle) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.panePID, nil
}

func (a *hkndqFakeAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.windows))
	copy(out, a.windows)
	return out, nil
}

func (a *hkndqFakeAdapter) KillSession(_ context.Context, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.killed = append(a.killed, name)
	return nil
}

// No-op stubs for the remaining Adapter surface.
func (a *hkndqFakeAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *hkndqFakeAdapter) NewWindowIn(_ context.Context, _ ltmux.NewWindowIn) ltmux.Outcome {
	return ltmux.Outcome{}
}
func (a *hkndqFakeAdapter) KillWindow(_ context.Context, _ ltmux.WindowHandle) error { return nil }
func (a *hkndqFakeAdapter) WindowPaneID(_ context.Context, _ ltmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *hkndqFakeAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error { return nil }
func (a *hkndqFakeAdapter) PasteBuffer(_ context.Context, _, _ string) error       { return nil }
func (a *hkndqFakeAdapter) SendKeysLiteral(_ context.Context, _, _ string) error   { return nil }
func (a *hkndqFakeAdapter) SendKeysEnter(_ context.Context, _ string) error        { return nil }
func (a *hkndqFakeAdapter) SendKeysQuit(_ context.Context, _ string) error         { return nil }
func (a *hkndqFakeAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

// compile-time interface check
var _ ltmux.Adapter = (*hkndqFakeAdapter)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Scenario A — live crew session skipped
// ─────────────────────────────────────────────────────────────────────────────

// TestHKNDQ_ScenarioA_LiveCrewSkipped verifies that RunOrphanSweep sets
// crew_sessions_skipped=1 and does NOT kill the session for a crew member
// whose first-pane PID is live.
//
// Spec ref: process-lifecycle.md §4.2 PL-006d mechanism (iii) — alive → exempt.
func TestHKNDQ_ScenarioA_LiveCrewSkipped(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	hash := lifecycle.ComputeProjectHash(projectDir)

	// Register crew member "alpha".
	if err := crew.Write(projectDir, crew.Record{Name: "alpha", SessionID: "sid-a", Queue: "q-a"}); err != nil {
		t.Fatalf("ScenarioA: crew.Write: %v", err)
	}

	crewSession := lifecycle.TmuxSessionName(hash, "crew-alpha")

	// Adapter: session present, non-zsh window, live PID (own process).
	adapter := &hkndqFakeAdapter{
		sessions: []string{crewSession},
		panePID:  os.Getpid(),
		windows:  []string{"claude-code"},
	}
	// Lister also presents the crew session — it should be skipped by excludeSessions.
	lister := &daemonOrphanSweepFakeTmuxLister{sessions: []string{crewSession}}
	killer := &daemonOrphanSweepFakeTmuxKiller{}

	result, err := RunOrphanSweep(
		t.Context(),
		projectDir,
		hash,
		time.Now(),
		OrphanSweepConfig{
			TmuxAdapter:   adapter,
			TmuxLister:    lister,
			TmuxKiller:    killer,
			HandlerLister: &daemonOrphanSweepFakeHandlerLister{},
			BrLister:      &daemonOrphanSweepFakeBrLister{},
		},
	)
	if err != nil {
		// git worktree prune may fail on a non-git temp dir; non-fatal.
		t.Logf("ScenarioA: RunOrphanSweep error (possibly worktree prune): %v", err)
	}

	if result.CrewSessionsSkipped != 1 {
		t.Errorf("ScenarioA: CrewSessionsSkipped = %d, want 1", result.CrewSessionsSkipped)
	}
	if result.TmuxSessionsKilled != 0 {
		t.Errorf("ScenarioA: TmuxSessionsKilled = %d, want 0 (live crew must not be killed)", result.TmuxSessionsKilled)
	}
	for _, s := range killer.killed {
		if s == crewSession {
			t.Errorf("ScenarioA: lifecycle killer killed %q; live crew session must be exempted", crewSession)
		}
	}
	for _, s := range adapter.killed {
		if s == crewSession {
			t.Errorf("ScenarioA: adapter killer killed %q; live crew session must be exempted", crewSession)
		}
	}

	// Payload field surfaces.
	payload := result.ToPayload()
	if payload.CrewSessionsSkipped != 1 {
		t.Errorf("ScenarioA: payload.CrewSessionsSkipped = %d, want 1", payload.CrewSessionsSkipped)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario B — live crew idle at zsh skipped
// ─────────────────────────────────────────────────────────────────────────────

// TestHKNDQ_ScenarioB_LiveCrewIdleAtZsh_NotKilled verifies that RunOrphanSweep
// skips a crew session whose only window is named "zsh" (idle-at-prompt state),
// provided the first-pane PID is live.
//
// Without PL-006d mechanism (iii), the all-zsh classification in
// ltmux.SweepOrphanTmuxSessions would kill this session.  The crew registry
// probe exempts it before the generic orphan classifier is reached, so the
// session survives.
//
// Spec ref: process-lifecycle.md §4.2 PL-006d — "A crew idle at a zsh prompt
// BETWEEN tasks is classified orphaned and would be killed by this path."
// mechanism (iii) exempts it: alive → excluded from BOTH sweep paths.
func TestHKNDQ_ScenarioB_LiveCrewIdleAtZsh_NotKilled(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	hash := lifecycle.ComputeProjectHash(projectDir)

	if err := crew.Write(projectDir, crew.Record{Name: "bravo", SessionID: "sid-b", Queue: "q-b"}); err != nil {
		t.Fatalf("ScenarioB: crew.Write: %v", err)
	}

	crewSession := lifecycle.TmuxSessionName(hash, "crew-bravo")

	// Adapter: session present, ALL windows named "zsh" (idle-at-prompt), live PID.
	// The all-zsh state triggers sessionIsOrphaned=true in ltmux — but because the
	// crew probe adds crewSession to excludeSessions first, ltmux never inspects it.
	adapter := &hkndqFakeAdapter{
		sessions: []string{crewSession},
		panePID:  os.Getpid(), // live
		windows:  []string{"zsh"},
	}
	lister := &daemonOrphanSweepFakeTmuxLister{sessions: []string{crewSession}}
	killer := &daemonOrphanSweepFakeTmuxKiller{}

	result, err := RunOrphanSweep(
		t.Context(),
		projectDir,
		hash,
		time.Now(),
		OrphanSweepConfig{
			TmuxAdapter:   adapter,
			TmuxLister:    lister,
			TmuxKiller:    killer,
			HandlerLister: &daemonOrphanSweepFakeHandlerLister{},
			BrLister:      &daemonOrphanSweepFakeBrLister{},
		},
	)
	if err != nil {
		t.Logf("ScenarioB: RunOrphanSweep error (possibly worktree prune): %v", err)
	}

	if result.CrewSessionsSkipped != 1 {
		t.Errorf("ScenarioB: CrewSessionsSkipped = %d, want 1 (live-at-zsh crew must be exempted)", result.CrewSessionsSkipped)
	}
	if result.TmuxSessionsKilled != 0 {
		t.Errorf("ScenarioB: TmuxSessionsKilled = %d, want 0 (live crew must not be killed even when idle-at-zsh)", result.TmuxSessionsKilled)
	}
	for _, s := range killer.killed {
		if s == crewSession {
			t.Errorf("ScenarioB: lifecycle killer killed %q; idle-at-zsh crew with live PID must be exempted", crewSession)
		}
	}
	for _, s := range adapter.killed {
		if s == crewSession {
			t.Errorf("ScenarioB: adapter killed %q; idle-at-zsh crew with live PID must be exempted", crewSession)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario C — dead crew session reaped; registry preserved (REVIEW-FIX)
// ─────────────────────────────────────────────────────────────────────────────

// TestHKNDQ_ScenarioC_DeadCrewReaped_RegistryPreserved verifies that when a
// crew session's first-pane PID is dead (0), the session is NOT exempted and IS
// reaped by the generic sweep, AND that the crew registry record is NOT removed.
//
// REVIEW-FIX: the dead-PID branch must NOT call crew.Remove; the sweep lets the
// generic path handle the session.  The registry survives so the crew can
// re-register after a restart without conflicts.
//
// Spec ref: process-lifecycle.md §4.2 PL-006d mechanism (iii) — dead → not
// excluded; spec says crew.Remove is called, but the REVIEW-FIX constrains this:
// session present + PID dead → let generic sweep handle it (do NOT call
// crew.Remove).
func TestHKNDQ_ScenarioC_DeadCrewReaped_RegistryPreserved(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	hash := lifecycle.ComputeProjectHash(projectDir)

	if err := crew.Write(projectDir, crew.Record{Name: "charlie", SessionID: "sid-c", Queue: "q-c"}); err != nil {
		t.Fatalf("ScenarioC: crew.Write: %v", err)
	}

	crewSession := lifecycle.TmuxSessionName(hash, "crew-charlie")

	// Adapter: session present, non-zsh window, PID=0 (dead/invalid).
	// probeCrewRegistrySessions sees pid <= 0 → does NOT exempt.
	// sessionIsOrphaned sees: non-zsh → condition1 not triggered; pid=0 → orphaned.
	adapter := &hkndqFakeAdapter{
		sessions: []string{crewSession},
		panePID:  0, // dead/invalid
		windows:  []string{"claude-code"},
	}
	// Lister also presents the crew session; lifecycle path kills it too.
	lister := &daemonOrphanSweepFakeTmuxLister{sessions: []string{crewSession}}
	killer := &daemonOrphanSweepFakeTmuxKiller{}

	result, err := RunOrphanSweep(
		t.Context(),
		projectDir,
		hash,
		time.Now(),
		OrphanSweepConfig{
			TmuxAdapter:   adapter,
			TmuxLister:    lister,
			TmuxKiller:    killer,
			HandlerLister: &daemonOrphanSweepFakeHandlerLister{},
			BrLister:      &daemonOrphanSweepFakeBrLister{},
		},
	)
	if err != nil {
		t.Logf("ScenarioC: RunOrphanSweep error (possibly worktree prune): %v", err)
	}

	// Dead crew must NOT be exempted.
	if result.CrewSessionsSkipped != 0 {
		t.Errorf("ScenarioC: CrewSessionsSkipped = %d, want 0 (dead crew must not be exempted)", result.CrewSessionsSkipped)
	}

	// Dead crew session must be reaped (killed) by at least one sweep path.
	// lifecycle.SweepOrphanTmuxSessions kills via TmuxKiller; ltmux kills via
	// adapter.KillSession.  TmuxSessionsKilled accounts for both.
	if result.TmuxSessionsKilled == 0 {
		t.Error("ScenarioC: TmuxSessionsKilled = 0, want >= 1 (dead crew session must be reaped)")
	}

	// REVIEW-FIX: crew registry record must NOT be removed.
	// The sweep must not call crew.Remove on a dead-pane crew — the generic sweep
	// kills the session; the registry is left for potential restart.
	rec, loadErr := crew.Load(projectDir, "charlie")
	if loadErr != nil {
		t.Errorf("ScenarioC (REVIEW-FIX): crew.Load after sweep: %v — crew.Remove must NOT be called for dead-pane crew", loadErr)
	} else if rec.Name != "charlie" {
		t.Errorf("ScenarioC (REVIEW-FIX): crew record name = %q, want \"charlie\"", rec.Name)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario D — stop-in-flight not reaped (REVIEW-FIX coverage)
// ─────────────────────────────────────────────────────────────────────────────

// TestHKNDQ_ScenarioD_StopInFlight_NotReaped verifies that when a crew member
// has a registry record but its session is ABSENT from the tmux snapshot, the
// crew is skipped conservatively — the session is not killed and the registry
// record is not removed.
//
// This models the mid keeper-restart case: the session was stopped (or is still
// spawning) so it does not appear in the live session list.  On restart the
// crew re-registers; the retained registry record is essential so the daemon
// does not mistake the re-launch for a name collision.
//
// REVIEW-FIX: session absent from snapshot → launch-in-flight; skip
// conservatively (do NOT call crew.Remove).
//
// Spec ref: process-lifecycle.md §4.2 PL-006d mechanism (iii) — session absent
// from snapshot → conservative skip.
func TestHKNDQ_ScenarioD_StopInFlight_NotReaped(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	hash := lifecycle.ComputeProjectHash(projectDir)

	if err := crew.Write(projectDir, crew.Record{Name: "delta", SessionID: "sid-d", Queue: "q-d"}); err != nil {
		t.Fatalf("ScenarioD: crew.Write: %v", err)
	}

	crewSession := lifecycle.TmuxSessionName(hash, "crew-delta")

	// Adapter: session NOT in the sessions list (absent from snapshot).
	adapter := &hkndqFakeAdapter{
		sessions: []string{},  // empty — crewSession is absent
		panePID:  os.Getpid(), // live, but session is absent so probe never reaches PID check
		windows:  []string{"claude-code"},
	}
	// Lister also does not include the crew session.
	lister := &daemonOrphanSweepFakeTmuxLister{sessions: []string{}}
	killer := &daemonOrphanSweepFakeTmuxKiller{}

	result, err := RunOrphanSweep(
		t.Context(),
		projectDir,
		hash,
		time.Now(),
		OrphanSweepConfig{
			TmuxAdapter:   adapter,
			TmuxLister:    lister,
			TmuxKiller:    killer,
			HandlerLister: &daemonOrphanSweepFakeHandlerLister{},
			BrLister:      &daemonOrphanSweepFakeBrLister{},
		},
	)
	if err != nil {
		t.Logf("ScenarioD: RunOrphanSweep error (possibly worktree prune): %v", err)
	}

	// Absent session is not added to excludeSessions (not exempted), but there
	// is nothing to kill because the session is not in the lister either.
	if result.CrewSessionsSkipped != 0 {
		t.Errorf("ScenarioD: CrewSessionsSkipped = %d, want 0 (absent session is not exempted via the skip counter)", result.CrewSessionsSkipped)
	}
	if result.TmuxSessionsKilled != 0 {
		t.Errorf("ScenarioD: TmuxSessionsKilled = %d, want 0 (absent session cannot be killed)", result.TmuxSessionsKilled)
	}
	for _, s := range killer.killed {
		if s == crewSession {
			t.Errorf("ScenarioD: lifecycle killer killed absent session %q; must not kill absent crew", crewSession)
		}
	}
	for _, s := range adapter.killed {
		if s == crewSession {
			t.Errorf("ScenarioD: adapter killed absent session %q; must not kill absent crew", crewSession)
		}
	}

	// REVIEW-FIX: registry record must NOT be removed for absent (stop-in-flight) crew.
	rec, loadErr := crew.Load(projectDir, "delta")
	if loadErr != nil {
		t.Errorf("ScenarioD (REVIEW-FIX): crew.Load after sweep: %v — crew.Remove must NOT be called for absent-session crew", loadErr)
	} else if rec.Name != "delta" {
		t.Errorf("ScenarioD (REVIEW-FIX): crew record name = %q, want \"delta\"", rec.Name)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario E — payload mapping
// ─────────────────────────────────────────────────────────────────────────────

// TestHKNDQ_ScenarioE_PayloadMapping verifies that crew_sessions_skipped and
// captain_sessions_skipped are correctly mapped by OrphanSweepResult.ToPayload.
//
// Spec ref: process-lifecycle.md §4.2 PL-006d — "crew_sessions_skipped:
// <integer ≥ 0>" in daemon_orphan_sweep_completed payload.
// Event-model ref: event-model.md §8.7.14 (additive-tolerance per EV-029).
func TestHKNDQ_ScenarioE_PayloadMapping(t *testing.T) {
	t.Parallel()

	result := OrphanSweepResult{
		CrewSessionsSkipped:    2,
		CaptainSessionsSkipped: 1,
		SweptAt:                time.Now(),
	}

	payload := result.ToPayload()

	if payload.CrewSessionsSkipped != 2 {
		t.Errorf("ScenarioE: payload.CrewSessionsSkipped = %d, want 2", payload.CrewSessionsSkipped)
	}
	if payload.CaptainSessionsSkipped != 1 {
		t.Errorf("ScenarioE: payload.CaptainSessionsSkipped = %d, want 1", payload.CaptainSessionsSkipped)
	}
	if !payload.Valid() {
		t.Error("ScenarioE: payload.Valid() = false, want true")
	}
}
