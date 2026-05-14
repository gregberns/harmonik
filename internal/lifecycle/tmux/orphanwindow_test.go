package tmux

import (
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// ──────────────────────────────────────────────────────────────────────────────
// Fake Adapter for window-sweep tests (orphanWindowFixture prefix per hk-gql20.9)
// ──────────────────────────────────────────────────────────────────────────────

// orphanWindowFixtureAdapter is a deterministic in-memory fake of the [Adapter]
// interface used in orphan-window-sweep tests. It holds session→windows maps
// and records which windows were killed.
type orphanWindowFixtureAdapter struct {
	// sessions maps session name → slice of window names.
	sessions map[string][]string

	// killed records the WindowHandle values passed to KillWindow in call order.
	killed []WindowHandle

	// listSessionsErr, if non-nil, is returned by ListSessions.
	listSessionsErr error

	// listWindowsErr, if set, is returned by ListWindows for any session whose
	// name is a key in the map.
	listWindowsErr map[string]error

	// killWindowErr, if non-nil, is returned by KillWindow for the window handle
	// named as the key.
	killWindowErr map[WindowHandle]error

	// removeOnKill, when true, removes the window from sessions on kill so that
	// post-kill polling immediately sees the window as gone.
	removeOnKill bool
}

// ProbeTmux always succeeds for the fake adapter.
func (a *orphanWindowFixtureAdapter) ProbeTmux(_ context.Context) error { return nil }

// ListSessions returns the configured error or the session names.
func (a *orphanWindowFixtureAdapter) ListSessions(_ context.Context) ([]string, error) {
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
func (a *orphanWindowFixtureAdapter) ListWindows(_ context.Context, session string) ([]string, error) {
	if a.listWindowsErr != nil {
		if err, ok := a.listWindowsErr[session]; ok {
			return nil, err
		}
	}
	windows, ok := a.sessions[session]
	if !ok {
		return nil, ErrNoSession
	}
	// Return a copy so callers cannot modify the map.
	out := make([]string, len(windows))
	copy(out, windows)
	return out, nil
}

// NewWindowIn is not exercised by sweep tests; returns an empty outcome.
func (a *orphanWindowFixtureAdapter) NewWindowIn(_ context.Context, _ NewWindowIn) Outcome {
	return Outcome{}
}

// KillWindow records the kill and, when removeOnKill is set, removes the window
// from the session so subsequent ListWindows calls no longer return it.
func (a *orphanWindowFixtureAdapter) KillWindow(_ context.Context, handle WindowHandle) error {
	if a.killWindowErr != nil {
		if err, ok := a.killWindowErr[handle]; ok {
			return err
		}
	}
	a.killed = append(a.killed, handle)
	if a.removeOnKill {
		// Parse "session:window" to remove from in-memory state.
		target := string(handle)
		colonIdx := strings.Index(target, ":")
		if colonIdx >= 0 {
			session := target[:colonIdx]
			window := target[colonIdx+1:]
			a.orphanWindowFixtureRemoveWindow(session, window)
		}
	}
	return nil
}

// WindowPanePID returns 0 for all handles in the fake adapter.
func (a *orphanWindowFixtureAdapter) WindowPanePID(_ context.Context, _ WindowHandle) (int, error) {
	return 0, nil
}

// KillSession is not exercised by window-sweep tests; returns nil.
func (a *orphanWindowFixtureAdapter) KillSession(_ context.Context, _ string) error {
	return nil
}

// LoadBuffer is a no-op stub to satisfy the [Adapter] interface.
func (a *orphanWindowFixtureAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}

// PasteBuffer is a no-op stub to satisfy the [Adapter] interface.
func (a *orphanWindowFixtureAdapter) PasteBuffer(_ context.Context, _, _ string) error {
	return nil
}

// SendKeysLiteral is a no-op stub to satisfy the [Adapter] interface.
func (a *orphanWindowFixtureAdapter) SendKeysLiteral(_ context.Context, _, _ string) error {
	return nil
}

// orphanWindowFixtureRemoveWindow removes windowName from the session's window list.
func (a *orphanWindowFixtureAdapter) orphanWindowFixtureRemoveWindow(session, windowName string) {
	windows := a.sessions[session]
	filtered := windows[:0]
	for _, w := range windows {
		if w != windowName {
			filtered = append(filtered, w)
		}
	}
	a.sessions[session] = filtered
}

// orphanWindowFixtureNewAdapter constructs a fresh fake adapter with the given
// sessions. removeOnKill=true so that post-kill polling exits promptly.
func orphanWindowFixtureNewAdapter(sessions map[string][]string, removeOnKill bool) *orphanWindowFixtureAdapter {
	return &orphanWindowFixtureAdapter{
		sessions:     sessions,
		removeOnKill: removeOnKill,
	}
}

// orphanWindowFixtureProjectHash returns a core.ProjectHash whose first 6 hex
// chars are "abcdef", used across window-sweep tests.
func orphanWindowFixtureProjectHash() core.ProjectHash {
	// ProjectHash is a 12-char lowercase hex string per core.ProjectHash.
	return core.ProjectHash("abcdef012345")
}

// orphanWindowFixtureSweepPrefix is the expected "hk-<hash6>-" prefix for the
// fixture project hash.
const orphanWindowFixtureSweepPrefix = "hk-abcdef-"

// orphanWindowFixtureShortenTimers replaces the package-level poll vars with
// very short durations for fast tests and restores them via t.Cleanup.
func orphanWindowFixtureShortenTimers(t *testing.T) {
	t.Helper()
	origInterval := windowSweepPollInterval
	origCeiling := windowSweepPollCeiling
	windowSweepPollInterval = time.Millisecond
	windowSweepPollCeiling = 50 * time.Millisecond
	t.Cleanup(func() {
		windowSweepPollInterval = origInterval
		windowSweepPollCeiling = origCeiling
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// SweepOrphanTmuxWindows tests (PL-021c)
// ──────────────────────────────────────────────────────────────────────────────

// TestPL021c_SweepOrphanTmuxWindows_NilAdapterReturnsZero verifies that a nil
// adapter produces (0, nil) — no-op sweep.
func TestPL021c_SweepOrphanTmuxWindows_NilAdapterReturnsZero(t *testing.T) {
	t.Parallel()

	hash := orphanWindowFixtureProjectHash()
	killed, err := SweepOrphanTmuxWindows(context.Background(), hash, nil, nil)
	if err != nil {
		t.Errorf("nil adapter: unexpected error: %v", err)
	}
	if killed != 0 {
		t.Errorf("nil adapter: killed = %d, want 0", killed)
	}
}

// TestPL021c_SweepOrphanTmuxWindows_StaleWindowKilled verifies that a window
// matching hk-<hash6>- in an operator session is killed and counted.
// NOTE: uses package-level timer vars — cannot be parallel.
func TestPL021c_SweepOrphanTmuxWindows_StaleWindowKilled(t *testing.T) {
	orphanWindowFixtureShortenTimers(t)

	hash := orphanWindowFixtureProjectHash()
	staleWindow := orphanWindowFixtureSweepPrefix + "my-task"

	adapter := orphanWindowFixtureNewAdapter(map[string][]string{
		"operator-session": {staleWindow, "other-window"},
	}, true /* removeOnKill */)

	killed, err := SweepOrphanTmuxWindows(context.Background(), hash, adapter, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if killed != 1 {
		t.Errorf("killed = %d, want 1", killed)
	}
	if len(adapter.killed) != 1 {
		t.Fatalf("adapter.killed len = %d, want 1: %v", len(adapter.killed), adapter.killed)
	}
	expectedHandle := WindowHandle("operator-session:" + staleWindow)
	if adapter.killed[0] != expectedHandle {
		t.Errorf("adapter.killed[0] = %q, want %q", adapter.killed[0], expectedHandle)
	}
}

// TestPL021c_SweepOrphanTmuxWindows_NonMatchingWindowsUntouched verifies that
// windows whose names do NOT start with hk-<hash6>- are not killed.
// NOTE: uses package-level timer vars — cannot be parallel.
func TestPL021c_SweepOrphanTmuxWindows_NonMatchingWindowsUntouched(t *testing.T) {
	orphanWindowFixtureShortenTimers(t)

	hash := orphanWindowFixtureProjectHash()

	adapter := orphanWindowFixtureNewAdapter(map[string][]string{
		"operator-session": {
			"my-task",         // no hk- prefix at all
			"hk-999999-other", // wrong hash6
			"harmonik-task",   // session-prefix style, not window prefix
		},
	}, true)

	killed, err := SweepOrphanTmuxWindows(context.Background(), hash, adapter, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if killed != 0 {
		t.Errorf("killed = %d, want 0 (non-matching windows must be untouched)", killed)
	}
	if len(adapter.killed) != 0 {
		t.Errorf("adapter.killed = %v, want empty (no windows should have been killed)", adapter.killed)
	}
}

// TestPL021c_SweepOrphanTmuxWindows_MultipleSessionsAndWindows verifies that
// matching windows across multiple sessions are all killed.
// NOTE: uses package-level timer vars — cannot be parallel.
func TestPL021c_SweepOrphanTmuxWindows_MultipleSessionsAndWindows(t *testing.T) {
	orphanWindowFixtureShortenTimers(t)

	hash := orphanWindowFixtureProjectHash()
	stale1 := orphanWindowFixtureSweepPrefix + "task-a"
	stale2 := orphanWindowFixtureSweepPrefix + "task-b"

	adapter := orphanWindowFixtureNewAdapter(map[string][]string{
		"session-one": {stale1, "unrelated-win"},
		"session-two": {stale2},
	}, true)

	killed, err := SweepOrphanTmuxWindows(context.Background(), hash, adapter, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if killed != 2 {
		t.Errorf("killed = %d, want 2", killed)
	}
	if len(adapter.killed) != 2 {
		t.Errorf("adapter.killed len = %d, want 2: %v", len(adapter.killed), adapter.killed)
	}
}

// TestPL021c_SweepOrphanTmuxWindows_EventPayloadCounterAccurate verifies that
// the killed count matches the adapter.killed slice length (payload counter
// accuracy requirement per PL-021c and the bead acceptance criteria).
// NOTE: uses package-level timer vars — cannot be parallel.
func TestPL021c_SweepOrphanTmuxWindows_EventPayloadCounterAccurate(t *testing.T) {
	orphanWindowFixtureShortenTimers(t)

	hash := orphanWindowFixtureProjectHash()
	staleWindows := []string{
		orphanWindowFixtureSweepPrefix + "alpha",
		orphanWindowFixtureSweepPrefix + "beta",
		orphanWindowFixtureSweepPrefix + "gamma",
	}

	adapter := orphanWindowFixtureNewAdapter(map[string][]string{
		"my-operator-session": append(staleWindows, "keep-me"),
	}, true)

	killed, err := SweepOrphanTmuxWindows(context.Background(), hash, adapter, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if killed != len(staleWindows) {
		t.Errorf("killed = %d, want %d (event payload counter must equal stale windows)", killed, len(staleWindows))
	}
	if len(adapter.killed) != len(staleWindows) {
		t.Errorf("adapter.killed len = %d, want %d", len(adapter.killed), len(staleWindows))
	}
}

// TestPL021c_SweepOrphanTmuxWindows_EmptySessionList verifies that an empty
// session list produces (0, nil) cleanly.
func TestPL021c_SweepOrphanTmuxWindows_EmptySessionList(t *testing.T) {
	t.Parallel()

	hash := orphanWindowFixtureProjectHash()
	adapter := orphanWindowFixtureNewAdapter(map[string][]string{}, false)

	killed, err := SweepOrphanTmuxWindows(context.Background(), hash, adapter, nil)
	if err != nil {
		t.Errorf("empty sessions: unexpected error: %v", err)
	}
	if killed != 0 {
		t.Errorf("empty sessions: killed = %d, want 0", killed)
	}
}

// TestPL021c_SweepOrphanTmuxWindows_ListSessionsError verifies that a
// ListSessions error is propagated as a non-nil error return.
func TestPL021c_SweepOrphanTmuxWindows_ListSessionsError(t *testing.T) {
	t.Parallel()

	hash := orphanWindowFixtureProjectHash()
	adapter := orphanWindowFixtureNewAdapter(map[string][]string{}, false)
	adapter.listSessionsErr = errors.New("tmux server gone")

	_, err := SweepOrphanTmuxWindows(context.Background(), hash, adapter, nil)
	if err == nil {
		t.Error("ListSessions error: want non-nil error, got nil")
	}
	if !strings.Contains(err.Error(), "ListSessions") {
		t.Errorf("ListSessions error: message %q does not mention ListSessions", err.Error())
	}
}

// TestPL021c_SweepOrphanTmuxWindows_SessionVanishesDuringListWindows verifies
// that a TOCTOU ErrNoSession from ListWindows is treated as non-fatal (the
// session vanished between ListSessions and ListWindows).
func TestPL021c_SweepOrphanTmuxWindows_SessionVanishesDuringListWindows(t *testing.T) {
	t.Parallel()

	hash := orphanWindowFixtureProjectHash()
	// Seed one session that will return ErrNoSession from ListWindows.
	adapter := orphanWindowFixtureNewAdapter(map[string][]string{
		"vanishing-session": {},
	}, false)
	adapter.listWindowsErr = map[string]error{
		"vanishing-session": ErrNoSession,
	}

	killed, err := SweepOrphanTmuxWindows(context.Background(), hash, adapter, nil)
	if err != nil {
		t.Errorf("TOCTOU ErrNoSession: want nil error, got %v", err)
	}
	if killed != 0 {
		t.Errorf("TOCTOU ErrNoSession: killed = %d, want 0", killed)
	}
}

// TestPL021c_SweepOrphanTmuxWindows_KillWindowErrorNonFatal verifies that a
// kill-window error (other than ErrNoSession) is logged and does not abort the
// sweep.
// NOTE: uses package-level timer vars — cannot be parallel.
func TestPL021c_SweepOrphanTmuxWindows_KillWindowErrorNonFatal(t *testing.T) {
	orphanWindowFixtureShortenTimers(t)

	hash := orphanWindowFixtureProjectHash()
	staleWindow := orphanWindowFixtureSweepPrefix + "task-x"

	adapter := orphanWindowFixtureNewAdapter(map[string][]string{
		"operator-session": {staleWindow},
	}, false)
	handle := WindowHandle("operator-session:" + staleWindow)
	adapter.killWindowErr = map[WindowHandle]error{
		handle: &ErrTmuxFailure{Op: "kill-window", ExitCode: 1, Stderr: "something went wrong"},
	}

	var logBuf strings.Builder
	logger := log.New(&logBuf, "", 0)

	// Should not return an error even though kill failed.
	killed, err := SweepOrphanTmuxWindows(context.Background(), hash, adapter, logger)
	if err != nil {
		t.Errorf("kill error non-fatal: unexpected error: %v", err)
	}
	// The window was still counted as killed (we incremented after the kill attempt).
	if killed != 1 {
		t.Errorf("kill error non-fatal: killed = %d, want 1", killed)
	}
	// The log must mention the error.
	if !strings.Contains(logBuf.String(), "kill-window") && !strings.Contains(logBuf.String(), "error") {
		t.Errorf("kill error non-fatal: expected log message about kill error, got %q", logBuf.String())
	}
}

// TestPL021c_SweepOrphanTmuxWindows_LoggerReceivesMessages verifies that a
// non-nil logger receives diagnostic output during a sweep with matching windows.
// NOTE: uses package-level timer vars — cannot be parallel.
func TestPL021c_SweepOrphanTmuxWindows_LoggerReceivesMessages(t *testing.T) {
	orphanWindowFixtureShortenTimers(t)

	hash := orphanWindowFixtureProjectHash()
	stale := orphanWindowFixtureSweepPrefix + "logged-task"

	adapter := orphanWindowFixtureNewAdapter(map[string][]string{
		"op-session": {stale},
	}, true)

	var logBuf strings.Builder
	logger := log.New(&logBuf, "", 0)

	killed, err := SweepOrphanTmuxWindows(context.Background(), hash, adapter, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if killed != 1 {
		t.Errorf("killed = %d, want 1", killed)
	}
	if logBuf.Len() == 0 {
		t.Error("expected logger to receive messages, got empty log buffer")
	}
}

// TestPL021c_WindowOrphanPrefix verifies that windowOrphanPrefix produces
// "hk-<hash6>-" for a valid 12-char ProjectHash.
func TestPL021c_WindowOrphanPrefix(t *testing.T) {
	t.Parallel()

	hash := core.ProjectHash("abcdef012345")
	got := windowOrphanPrefix(hash)
	want := "hk-abcdef-"
	if got != want {
		t.Errorf("windowOrphanPrefix(%q) = %q, want %q", hash, got, want)
	}
}

// TestPL021c_WindowOrphanPrefix_ShortHash verifies the defensive path when
// the ProjectHash is shorter than 6 chars (should not happen in production).
func TestPL021c_WindowOrphanPrefix_ShortHash(t *testing.T) {
	t.Parallel()

	hash := core.ProjectHash("abc")
	got := windowOrphanPrefix(hash)
	if !strings.HasPrefix(got, "hk-") || !strings.HasSuffix(got, "-") {
		t.Errorf("windowOrphanPrefix short hash: got %q, want hk-abc- shape", got)
	}
}
