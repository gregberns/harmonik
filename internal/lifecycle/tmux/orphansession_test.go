package tmux

import (
	"context"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// ──────────────────────────────────────────────────────────────────────────────
// Fake Adapter for session-sweep tests (orphanSessionFixture prefix)
// ──────────────────────────────────────────────────────────────────────────────

// orphanSessionFixtureAdapter is a deterministic in-memory fake of the [Adapter]
// interface used in orphan-session-sweep tests. It holds session→windows maps,
// per-session pane PIDs, and records which sessions were killed.
type orphanSessionFixtureAdapter struct {
	// sessions maps session name → slice of window names.
	sessions map[string][]string

	// panePIDs maps session name → pane PID returned by WindowPanePID.
	// When a session has no entry, WindowPanePID returns (0, nil).
	panePIDs map[string]int

	// panePIDErr maps session name → error returned by WindowPanePID.
	panePIDErr map[string]error

	// killedSessions records the session names passed to KillSession.
	killedSessions []string

	// listSessionsErr, if non-nil, is returned by ListSessions.
	listSessionsErr error

	// listWindowsErr maps session name → error returned by ListWindows.
	listWindowsErr map[string]error
}

// ProbeTmux always succeeds for the fake adapter.
func (a *orphanSessionFixtureAdapter) ProbeTmux(_ context.Context) error { return nil }

// ListSessions returns the configured error or the session names.
func (a *orphanSessionFixtureAdapter) ListSessions(_ context.Context) ([]string, error) {
	if a.listSessionsErr != nil {
		return nil, a.listSessionsErr
	}
	names := make([]string, 0, len(a.sessions))
	for name := range a.sessions {
		names = append(names, name)
	}
	return names, nil
}

// ListWindows returns the windows for a given session.
func (a *orphanSessionFixtureAdapter) ListWindows(_ context.Context, session string) ([]string, error) {
	if a.listWindowsErr != nil {
		if err, ok := a.listWindowsErr[session]; ok {
			return nil, err
		}
	}
	windows, ok := a.sessions[session]
	if !ok {
		return nil, ErrNoSession
	}
	out := make([]string, len(windows))
	copy(out, windows)
	return out, nil
}

// NewWindowIn is not exercised by session-sweep tests; returns an empty outcome.
func (a *orphanSessionFixtureAdapter) NewWindowIn(_ context.Context, _ NewWindowIn) Outcome {
	return Outcome{}
}

// KillWindow is not exercised by session-sweep tests; returns nil.
func (a *orphanSessionFixtureAdapter) KillWindow(_ context.Context, _ WindowHandle) error {
	return nil
}

// WindowPanePID returns the configured PID or error for the session.
// The handle is expected to be "session:" (first window); we extract the
// session name by stripping the trailing colon.
func (a *orphanSessionFixtureAdapter) WindowPanePID(_ context.Context, handle WindowHandle) (int, error) {
	target := string(handle)
	// Strip trailing ":" to get the session name.
	session := strings.TrimSuffix(target, ":")

	if a.panePIDErr != nil {
		if err, ok := a.panePIDErr[session]; ok {
			return 0, err
		}
	}
	if a.panePIDs != nil {
		if pid, ok := a.panePIDs[session]; ok {
			return pid, nil
		}
	}
	return 0, nil
}

// KillSession records the session name and returns nil (idempotent success).
func (a *orphanSessionFixtureAdapter) KillSession(_ context.Context, sessionName string) error {
	a.killedSessions = append(a.killedSessions, sessionName)
	return nil
}

// LoadBuffer is a no-op stub to satisfy the [Adapter] interface.
func (a *orphanSessionFixtureAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}

// PasteBuffer is a no-op stub to satisfy the [Adapter] interface.
func (a *orphanSessionFixtureAdapter) PasteBuffer(_ context.Context, _, _ string) error {
	return nil
}

// SendKeysLiteral is a no-op stub to satisfy the [Adapter] interface.
func (a *orphanSessionFixtureAdapter) SendKeysLiteral(_ context.Context, _, _ string) error {
	return nil
}

// SendKeysEnter is a no-op stub to satisfy the [Adapter] interface.
func (a *orphanSessionFixtureAdapter) SendKeysEnter(_ context.Context, _ string) error {
	return nil
}

// SendKeysQuit is a no-op stub to satisfy the [Adapter] interface.
func (a *orphanSessionFixtureAdapter) SendKeysQuit(_ context.Context, _ string) error {
	return nil
}

// WriteToPane is a no-op stub to satisfy the [Adapter] interface.
func (a *orphanSessionFixtureAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

// WindowPaneID is a no-op stub to satisfy the [Adapter] interface.
func (a *orphanSessionFixtureAdapter) WindowPaneID(_ context.Context, _ WindowHandle) (string, error) {
	return "", nil
}

// orphanSessionFixtureNewAdapter constructs a fresh fake adapter.
func orphanSessionFixtureNewAdapter(sessions map[string][]string) *orphanSessionFixtureAdapter {
	return &orphanSessionFixtureAdapter{sessions: sessions}
}

// orphanSessionFixtureProjectHash returns a core.ProjectHash of exactly 12
// lowercase hex chars, used across session-sweep tests.
func orphanSessionFixtureProjectHash() core.ProjectHash {
	return core.ProjectHash("abcdef012345")
}

// orphanSessionFixtureSessionPrefix is the expected "harmonik-<hash>-" prefix
// for the fixture project hash.
const orphanSessionFixtureSessionPrefix = "harmonik-abcdef012345-"

// ──────────────────────────────────────────────────────────────────────────────
// SweepOrphanTmuxSessions tests (hk-kqdpf.3)
// ──────────────────────────────────────────────────────────────────────────────

// TestSweepOrphanTmuxSessions_NilAdapterReturnsZero verifies that a nil adapter
// produces (0, nil) — no-op sweep.
func TestSweepOrphanTmuxSessions_NilAdapterReturnsZero(t *testing.T) {
	t.Parallel()

	hash := orphanSessionFixtureProjectHash()
	killed, err := SweepOrphanTmuxSessions(context.Background(), hash, nil, nil, nil)
	if err != nil {
		t.Errorf("nil adapter: unexpected error: %v", err)
	}
	if killed != 0 {
		t.Errorf("nil adapter: killed = %d, want 0", killed)
	}
}

// TestSweepOrphanTmuxSessions_AllZshWindowsKilled verifies that a session
// matching the project hash prefix whose windows are all "zsh" is killed.
func TestSweepOrphanTmuxSessions_AllZshWindowsKilled(t *testing.T) {
	t.Parallel()

	hash := orphanSessionFixtureProjectHash()
	sessionName := orphanSessionFixtureSessionPrefix + "default"

	adapter := orphanSessionFixtureNewAdapter(map[string][]string{
		sessionName: {"zsh", "zsh"},
	})

	killed, err := SweepOrphanTmuxSessions(context.Background(), hash, adapter, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if killed != 1 {
		t.Errorf("killed = %d, want 1", killed)
	}
	if len(adapter.killedSessions) != 1 || adapter.killedSessions[0] != sessionName {
		t.Errorf("killedSessions = %v, want [%q]", adapter.killedSessions, sessionName)
	}
}

// TestSweepOrphanTmuxSessions_DeadPIDKilled verifies that a session whose first
// pane reports PID 0 (treated as invalid/dead) is killed.
func TestSweepOrphanTmuxSessions_DeadPIDKilled(t *testing.T) {
	t.Parallel()

	hash := orphanSessionFixtureProjectHash()
	sessionName := orphanSessionFixtureSessionPrefix + "default"

	adapter := orphanSessionFixtureNewAdapter(map[string][]string{
		// Non-zsh window so condition 1 doesn't trigger.
		sessionName: {"claude-agent"},
	})
	// PID 0 is invalid — treated as dead.
	adapter.panePIDs = map[string]int{sessionName: 0}

	killed, err := SweepOrphanTmuxSessions(context.Background(), hash, adapter, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if killed != 1 {
		t.Errorf("killed = %d, want 1 (zero PID should be treated as dead)", killed)
	}
}

// TestSweepOrphanTmuxSessions_OtherProjectHashUntouched verifies that a session
// whose name starts with "harmonik-" but has a DIFFERENT 12-char hash is NOT
// killed.
func TestSweepOrphanTmuxSessions_OtherProjectHashUntouched(t *testing.T) {
	t.Parallel()

	hash := orphanSessionFixtureProjectHash() // "abcdef012345"
	otherSession := "harmonik-999999abcdef-default"

	adapter := orphanSessionFixtureNewAdapter(map[string][]string{
		// All-zsh windows — would normally trigger a kill.
		otherSession: {"zsh"},
	})

	killed, err := SweepOrphanTmuxSessions(context.Background(), hash, adapter, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if killed != 0 {
		t.Errorf("killed = %d, want 0 (other project hash must be untouched)", killed)
	}
	if len(adapter.killedSessions) != 0 {
		t.Errorf("killedSessions = %v, want empty (other project must be untouched)", adapter.killedSessions)
	}
}

// TestSweepOrphanTmuxSessions_NonHarmonikSessionUntouched verifies that sessions
// without the harmonik- prefix are not touched.
func TestSweepOrphanTmuxSessions_NonHarmonikSessionUntouched(t *testing.T) {
	t.Parallel()

	hash := orphanSessionFixtureProjectHash()

	adapter := orphanSessionFixtureNewAdapter(map[string][]string{
		"my-operator-session": {"zsh"},
		"tmux-main":           {"zsh"},
	})

	killed, err := SweepOrphanTmuxSessions(context.Background(), hash, adapter, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if killed != 0 {
		t.Errorf("killed = %d, want 0 (non-harmonik sessions must be untouched)", killed)
	}
}

// TestSweepOrphanTmuxSessions_LiveSessionNotKilled verifies that a session with
// non-zsh windows and a live PID (PID > 1, kill(pid, 0) succeeds) is not killed.
func TestSweepOrphanTmuxSessions_LiveSessionNotKilled(t *testing.T) {
	t.Parallel()

	hash := orphanSessionFixtureProjectHash()
	sessionName := orphanSessionFixtureSessionPrefix + "default"

	adapter := orphanSessionFixtureNewAdapter(map[string][]string{
		sessionName: {"claude-agent", "zsh"},
	})
	// Use PID 1 (init/launchd) which is always alive so kill(1, 0) succeeds.
	// This simulates a live workload session.
	adapter.panePIDs = map[string]int{sessionName: 1}

	killed, err := SweepOrphanTmuxSessions(context.Background(), hash, adapter, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if killed != 0 {
		t.Errorf("killed = %d, want 0 (live session with non-zsh windows must not be killed)", killed)
	}
	if len(adapter.killedSessions) != 0 {
		t.Errorf("killedSessions = %v, want empty", adapter.killedSessions)
	}
}

// TestSweepOrphanTmuxSessions_MultipleSessions verifies that multiple orphaned
// sessions from the same project hash are all killed, and a live session is
// spared.
func TestSweepOrphanTmuxSessions_MultipleSessions(t *testing.T) {
	t.Parallel()

	hash := orphanSessionFixtureProjectHash()
	orphan1 := orphanSessionFixtureSessionPrefix + "default"
	orphan2 := orphanSessionFixtureSessionPrefix + "other"
	live := orphanSessionFixtureSessionPrefix + "live"

	adapter := orphanSessionFixtureNewAdapter(map[string][]string{
		orphan1: {"zsh"},
		orphan2: {"zsh", "zsh"},
		live:    {"claude-agent"},
	})
	// Live session has PID 1 (always alive).
	adapter.panePIDs = map[string]int{live: 1}

	killed, err := SweepOrphanTmuxSessions(context.Background(), hash, adapter, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if killed != 2 {
		t.Errorf("killed = %d, want 2", killed)
	}
	for _, s := range adapter.killedSessions {
		if s == live {
			t.Errorf("live session %q must not be killed; killedSessions = %v", live, adapter.killedSessions)
		}
	}
}

// TestSweepOrphanTmuxSessions_EmptySessionList verifies that an empty session
// list produces (0, nil) cleanly.
func TestSweepOrphanTmuxSessions_EmptySessionList(t *testing.T) {
	t.Parallel()

	hash := orphanSessionFixtureProjectHash()
	adapter := orphanSessionFixtureNewAdapter(map[string][]string{})

	killed, err := SweepOrphanTmuxSessions(context.Background(), hash, adapter, nil, nil)
	if err != nil {
		t.Errorf("empty sessions: unexpected error: %v", err)
	}
	if killed != 0 {
		t.Errorf("empty sessions: killed = %d, want 0", killed)
	}
}

// TestSweepOrphanTmuxSessions_ListSessionsError verifies that a ListSessions
// error is propagated as a non-nil error return.
func TestSweepOrphanTmuxSessions_ListSessionsError(t *testing.T) {
	t.Parallel()

	hash := orphanSessionFixtureProjectHash()
	adapter := orphanSessionFixtureNewAdapter(map[string][]string{})
	adapter.listSessionsErr = ErrNoSession

	_, err := SweepOrphanTmuxSessions(context.Background(), hash, adapter, nil, nil)
	if err == nil {
		t.Error("ListSessions error: want non-nil error, got nil")
	}
	if !strings.Contains(err.Error(), "ListSessions") {
		t.Errorf("ListSessions error: message %q does not mention ListSessions", err.Error())
	}
}

// TestSweepOrphanTmuxSessions_ListWindowsErrorSkipsSession verifies that a
// ListWindows error on one session causes that session to be skipped (non-fatal).
func TestSweepOrphanTmuxSessions_ListWindowsErrorSkipsSession(t *testing.T) {
	t.Parallel()

	hash := orphanSessionFixtureProjectHash()
	errorSession := orphanSessionFixtureSessionPrefix + "broken"
	goodSession := orphanSessionFixtureSessionPrefix + "orphan"

	adapter := orphanSessionFixtureNewAdapter(map[string][]string{
		errorSession: {},
		goodSession:  {"zsh"},
	})
	adapter.listWindowsErr = map[string]error{
		errorSession: ErrNoSession,
	}

	killed, err := SweepOrphanTmuxSessions(context.Background(), hash, adapter, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// errorSession is skipped; goodSession (all-zsh) is killed.
	if killed != 1 {
		t.Errorf("killed = %d, want 1 (error session skipped, orphan session killed)", killed)
	}
}

// TestSessionOrphanPrefix verifies that sessionOrphanPrefix produces
// "harmonik-<12-char-hash>-" for a valid ProjectHash.
func TestSessionOrphanPrefix(t *testing.T) {
	t.Parallel()

	hash := core.ProjectHash("abcdef012345")
	got := sessionOrphanPrefix(hash)
	want := "harmonik-abcdef012345-"
	if got != want {
		t.Errorf("sessionOrphanPrefix(%q) = %q, want %q", hash, got, want)
	}
}

// TestCountNonZshWindows verifies the helper for various window name lists.
func TestCountNonZshWindows(t *testing.T) {
	t.Parallel()

	cases := []struct {
		windows []string
		want    int
	}{
		{nil, 0},
		{[]string{}, 0},
		{[]string{"zsh"}, 0},
		{[]string{"zsh", "zsh"}, 0},
		{[]string{"claude-agent"}, 1},
		{[]string{"zsh", "claude-agent"}, 1},
		{[]string{"claude-agent", "zsh", "reviewer"}, 2},
	}

	for _, tc := range cases {
		got := countNonZshWindows(tc.windows)
		if got != tc.want {
			t.Errorf("countNonZshWindows(%v) = %d, want %d", tc.windows, got, tc.want)
		}
	}
}
