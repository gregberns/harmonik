package supervise

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeReapAdapter is a hand-rolled ReapAdapter for the orphan-reaper tests. It
// records every KillSession call so a test can assert EXACTLY which sessions
// were reaped (and, by omission, which were preserved).
type fakeReapAdapter struct {
	sessions []FlywheelSession
	listErr  error
	killed   []string
}

func (f *fakeReapAdapter) ListFlywheelSessions(_ context.Context) ([]FlywheelSession, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.sessions, nil
}

func (f *fakeReapAdapter) KillSession(_ context.Context, name string) error {
	f.killed = append(f.killed, name)
	return nil
}

// TestReap_KillsOnlyDeadFlywheel_LeavesDefaultUntouched is the core gate: a fake
// adapter that lists a dead harmonik-<hash>-flywheel session alongside a live
// harmonik-<hash>-default session. The reaper must kill ONLY the dead flywheel,
// emit one tmux_orphan_reaped event for it, and never touch -default.
//
// (The OS adapter pre-filters to the flywheel family, but the reaper re-asserts
// the name discipline on every candidate — so we feed -default in directly to
// prove the in-reaper I3 guard, not just the adapter filter.)
func TestReap_KillsOnlyDeadFlywheel_LeavesDefaultUntouched(t *testing.T) {
	daemonStart := time.Now()
	flywheel := "harmonik-0123456789ab-flywheel"
	def := "harmonik-0123456789ab-default"

	adapter := &fakeReapAdapter{
		sessions: []FlywheelSession{
			{Name: flywheel, PaneDead: true, Created: daemonStart.Add(-1 * time.Hour)},
			// -default is NOT in the flywheel family; the reaper must refuse it
			// even if (defensively) handed in. It is also pane-live.
			{Name: def, PaneDead: false, Created: daemonStart.Add(-1 * time.Hour)},
		},
	}

	result, err := ReapOrphanFlywheelSessions(context.Background(), adapter, ReapOptions{
		DaemonStartTime: daemonStart,
	})
	if err != nil {
		t.Fatalf("ReapOrphanFlywheelSessions: %v", err)
	}

	// Exactly the dead flywheel was killed.
	if len(adapter.killed) != 1 || adapter.killed[0] != flywheel {
		t.Fatalf("expected only %q killed, got %v", flywheel, adapter.killed)
	}
	// -default must never appear in the kill list.
	for _, k := range adapter.killed {
		if k == def {
			t.Fatalf("reaper killed the -default session %q — invariant I3 violated", def)
		}
	}

	// One tmux_orphan_reaped event, for the flywheel.
	if len(result.Events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(result.Events), result.Events)
	}
	ev := result.Events[0]
	if ev.Event != "tmux_orphan_reaped" {
		t.Errorf("event name = %q, want tmux_orphan_reaped", ev.Event)
	}
	if ev.Session != flywheel {
		t.Errorf("event session = %q, want %q", ev.Session, flywheel)
	}
	if len(result.Reaped) != 1 || result.Reaped[0] != flywheel {
		t.Errorf("result.Reaped = %v, want [%q]", result.Reaped, flywheel)
	}
}

// TestReap_NoTmuxServer_CleanNoOp verifies the no-tmux-server path: the OS
// adapter returns an empty list (no error), so the reaper is a clean no-op —
// zero scanned, zero reaped, zero events, no error.
func TestReap_NoTmuxServer_CleanNoOp(t *testing.T) {
	// Empty adapter models "tmux server absent / no sessions".
	adapter := &fakeReapAdapter{sessions: nil}
	result, err := ReapOrphanFlywheelSessions(context.Background(), adapter, ReapOptions{})
	if err != nil {
		t.Fatalf("expected clean no-op, got error: %v", err)
	}
	if result.Scanned != 0 || len(result.Reaped) != 0 || len(result.Events) != 0 {
		t.Fatalf("expected zero-everything no-op, got %+v", result)
	}
}

// TestReap_NilAdapter_NoOp verifies a nil adapter is a safe no-op.
func TestReap_NilAdapter_NoOp(t *testing.T) {
	result, err := ReapOrphanFlywheelSessions(context.Background(), nil, ReapOptions{})
	if err != nil {
		t.Fatalf("nil adapter should be a no-op, got error: %v", err)
	}
	if result.Scanned != 0 || len(result.Reaped) != 0 {
		t.Fatalf("nil adapter reaped something: %+v", result)
	}
}

// TestReap_PreservesLivePaneAndProtectedAndNewer covers the three preservation
// rules in one pass: a live-pane flywheel, the protected (just-created) session,
// and a flywheel created AFTER the daemon start — none may be reaped, only the
// genuinely-orphaned dead+old one is.
func TestReap_PreservesLivePaneAndProtectedAndNewer(t *testing.T) {
	daemonStart := time.Now()
	orphan := "harmonik-aaaaaaaaaaaa-flywheel"    // dead + old → reap
	live := "harmonik-bbbbbbbbbbbb-flywheel"      // live pane → keep
	protected := "harmonik-cccccccccccc-flywheel" // just created → keep
	newer := "harmonik-dddddddddddd-flywheel"     // created after daemon → keep

	adapter := &fakeReapAdapter{
		sessions: []FlywheelSession{
			{Name: orphan, PaneDead: true, Created: daemonStart.Add(-1 * time.Hour)},
			{Name: live, PaneDead: false, Created: daemonStart.Add(-1 * time.Hour)},
			{Name: protected, PaneDead: true, Created: daemonStart.Add(-1 * time.Hour)},
			{Name: newer, PaneDead: true, Created: daemonStart.Add(1 * time.Minute)},
		},
	}

	result, err := ReapOrphanFlywheelSessions(context.Background(), adapter, ReapOptions{
		DaemonStartTime: daemonStart,
		ProtectSession:  protected,
	})
	if err != nil {
		t.Fatalf("ReapOrphanFlywheelSessions: %v", err)
	}
	if len(adapter.killed) != 1 || adapter.killed[0] != orphan {
		t.Fatalf("expected only %q killed, got %v", orphan, adapter.killed)
	}
	if result.Skipped != 3 {
		t.Errorf("expected 3 skipped (live, protected, newer), got %d", result.Skipped)
	}
}

// TestReap_ListError_Propagates verifies a list error surfaces as an error
// (and nothing is killed).
func TestReap_ListError_Propagates(t *testing.T) {
	adapter := &fakeReapAdapter{listErr: errors.New("boom")}
	_, err := ReapOrphanFlywheelSessions(context.Background(), adapter, ReapOptions{})
	if err == nil {
		t.Fatal("expected error from list failure, got nil")
	}
	if len(adapter.killed) != 0 {
		t.Fatalf("nothing should be killed on list error, got %v", adapter.killed)
	}
}

// TestIsFlywheelOrphanName guards the name discipline: ONLY
// harmonik-<12hex>-flywheel matches; the protected fleet families do not.
func TestIsFlywheelOrphanName(t *testing.T) {
	yes := []string{
		"harmonik-0123456789ab-flywheel",
		"harmonik-aaaaaaaaaaaa-flywheel",
	}
	no := []string{
		"harmonik-0123456789ab-default",
		"harmonik-0123456789ab-captain",
		"harmonik-0123456789ab-crew-paul",
		"harmonik-0123456789ab-supervise",
		"hk-0123456789ab-daemon-supervise",
		"harmonik-flywheel",
		"harmonik-0123456789ab-flywheel-extra",
		"harmonik-XYZ-flywheel",
		"",
	}
	for _, n := range yes {
		if !IsFlywheelOrphanName(n) {
			t.Errorf("IsFlywheelOrphanName(%q) = false, want true", n)
		}
	}
	for _, n := range no {
		if IsFlywheelOrphanName(n) {
			t.Errorf("IsFlywheelOrphanName(%q) = true, want false (must never reap)", n)
		}
	}
}
