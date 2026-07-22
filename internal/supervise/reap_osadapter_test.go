package supervise

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOSReapAdapter_NoServerIsEmpty(t *testing.T) {
	installReapTmuxFixture(t, "no server running on /tmp/tmux-test/default")

	got, err := (osReapAdapter{}).ListFlywheelSessions(context.Background())
	if err != nil || got != nil {
		t.Fatalf("no server: got sessions=%v err=%v, want nil,nil", got, err)
	}
}

func TestOSReapAdapter_UnexpectedFailureIsReturned(t *testing.T) {
	installReapTmuxFixture(t, "permission denied")

	got, err := (osReapAdapter{}).ListFlywheelSessions(context.Background())
	if err == nil {
		t.Fatalf("unexpected failure: got sessions=%v err=nil, want error", got)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("unexpected failure: error %q omits command output", err)
	}
}

func TestOSReapAdapter_KillMissingSessionIsNoOp(t *testing.T) {
	installReapTmuxFixture(t, "can't find session: missing")

	if err := (osReapAdapter{}).KillSession(context.Background(), "harmonik-test-flywheel"); err != nil {
		t.Fatalf("kill missing session: %v, want nil", err)
	}
}

func TestOSReapAdapter_KillUnexpectedFailureIsReturned(t *testing.T) {
	installReapTmuxFixture(t, "permission denied")

	err := (osReapAdapter{}).KillSession(context.Background(), "harmonik-test-flywheel")
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("kill unexpected failure: err=%v, want error containing command output", err)
	}
}

func installReapTmuxFixture(t *testing.T, output string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' \"$HARMONIK_TEST_TMUX_OUTPUT\"\nexit 1\n"
	path := filepath.Join(dir, "tmux")
	//nolint:gosec // G306: executable test script, 0755 is intentional
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write tmux fixture: %v", err)
	}
	t.Setenv("HARMONIK_TEST_TMUX_OUTPUT", output)
	t.Setenv("PATH", dir)
}
