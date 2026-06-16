package main

// captain_launch_respawn_hkopuv_test.go — guards the captain launcher's
// --respawn-cmd self-heal wiring (hk-opuv).
//
// When the captain claude session dies (its tmux pane drops to a shell), the
// armed keeper's idle-respawn path (internal/keeper/watcher.go maybeRespawn)
// fires the operator-supplied --respawn-cmd. captain-launch.sh must therefore
// hand the keeper a respawn command that:
//
//   - resumes the SAME session-id (--resume "$SID"), not a fresh --session-id,
//     so the keeper's identity binding and the captain's conversation survive;
//   - relaunches ONLY the captain agent pane — it must NOT arm a second keeper
//     (the keeper running the respawn-cmd is the only keeper);
//   - refreshes captain.pid so the daemon orphan sweep keeps skipping the
//     freshly-relaunched session.
//
// These assertions read the canonical scripts/captain-tools/captain-launch.sh;
// the embedded copy is held byte-identical by TestCaptainLaunchShEmbedInSync.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readCaptainLaunchCanonical(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "scripts", "captain-tools", "captain-launch.sh")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read canonical captain-launch.sh: %v", err)
	}
	return string(b)
}

// TestCaptainLaunchArmsRespawnCmd verifies the keeper is armed WITH a
// --respawn-cmd so the dead-pane self-heal path can fire at all.
func TestCaptainLaunchArmsRespawnCmd(t *testing.T) {
	s := readCaptainLaunchCanonical(t)
	if !strings.Contains(s, "--respawn-cmd") {
		t.Fatalf("captain-launch.sh does not pass --respawn-cmd to the keeper; " +
			"dead-pane self-heal can never fire")
	}
}

// TestCaptainRespawnResumesSameSession verifies the respawn relaunches the
// agent with --resume "$SID" (the SAME minted session id) rather than minting a
// fresh --session-id, so the keeper rebinds and the conversation continues.
func TestCaptainRespawnResumesSameSession(t *testing.T) {
	s := readCaptainLaunchCanonical(t)
	if !strings.Contains(s, "--remote-control") {
		t.Fatalf("captain-launch.sh missing --remote-control launch (unexpected)")
	}
	// The respawn command must use --resume with the minted SID.
	if !strings.Contains(s, "--resume \"$SID\"") && !strings.Contains(s, "--resume $SID") {
		t.Fatalf("respawn command does not relaunch with --resume \"$SID\"; " +
			"a dead-pane relaunch must resume the SAME session-id, not mint a new one")
	}
}

// TestCaptainRespawnRefreshesPid verifies the respawn refreshes captain.pid so
// the daemon orphan sweep keeps skipping the relaunched session.
func TestCaptainRespawnRefreshesPid(t *testing.T) {
	s := readCaptainLaunchCanonical(t)
	// The captain.pid file is WRITTEN in two places once the feature lands:
	// initial launch (step 2) and inside the respawn command. Count the actual
	// write target (COGNITION_DIR/captain.pid), not comment mentions of the file.
	if got := strings.Count(s, "COGNITION_DIR/captain.pid"); got < 2 {
		t.Fatalf("respawn command does not refresh captain.pid; the relaunched "+
			"session would be reaped by the orphan sweep (found %d COGNITION_DIR/captain.pid writes, want >=2)",
			got)
	}
}
