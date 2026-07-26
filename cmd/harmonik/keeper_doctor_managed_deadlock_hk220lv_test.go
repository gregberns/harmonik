// keeper_doctor_managed_deadlock_hk220lv_test.go — `.managed` present with NO
// live watcher is a SILENT DEADLOCK, not a healthy managed session. Bead:
// hk-220lv.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeManagedMarker creates .harmonik/keeper/<agent>.managed for a doctor cfg.
func writeManagedMarker(t *testing.T, cfg doctorConfig) {
	t.Helper()
	dir := filepath.Join(cfg.projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("setup: mkdir keeper dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, cfg.agentName+".managed"), []byte("\n"), 0o600); err != nil {
		t.Fatalf("setup: write .managed: %v", err)
	}
}

// TestKeeperDoctor_ManagedWithoutLiveWatcher_IsRed pins the hk-220lv field
// failure: the captain sat at a typed-but-unsent /clear waiting for a keeper
// that had died, while doctor reported ".managed present (handoff cycle is
// LIVE)". `.managed` is an OPT-IN marker on disk — it is evidence of consent,
// never evidence that anything is running. The pair (.managed present, no
// watcher process) is the deadlock itself and must read RED on the `managed`
// check, not only on `live-watcher`.
func TestKeeperDoctor_ManagedWithoutLiveWatcher_IsRed(t *testing.T) {
	t.Parallel()

	cfg, _ := makeDoctorCfg(t, "orchestrator")
	writeManagedMarker(t, cfg)
	cfg.liveKeeperFn = func(_, _ string) bool { return false }

	var stdout, stderr bytes.Buffer
	runKeeperDoctor(cfg, &stdout, &stderr)

	out := stdout.String()
	if strings.Contains(out, "✓ managed") {
		t.Errorf("managed must NOT be green when no watcher process is running — "+
			"that pair is the hk-220lv silent deadlock (handoff cycle consented to, "+
			"nothing driving it).\nstdout: %s", out)
	}
	if !strings.Contains(out, "no live keeper watcher") {
		t.Errorf("the managed check must NAME the missing watcher so the reader is not "+
			"left to correlate two checks by eye.\nstdout: %s", out)
	}
}

// TestKeeperDoctor_ManagedWithLiveWatcher_StaysGreen guards the other side: a
// managed agent WITH a live watcher is the healthy state and must stay green,
// so the new red is a real discriminator and not a blanket downgrade.
func TestKeeperDoctor_ManagedWithLiveWatcher_StaysGreen(t *testing.T) {
	t.Parallel()

	cfg, _ := makeDoctorCfg(t, "orchestrator")
	writeManagedMarker(t, cfg)
	cfg.liveKeeperFn = func(_, _ string) bool { return true }

	var stdout, stderr bytes.Buffer
	runKeeperDoctor(cfg, &stdout, &stderr)

	out := stdout.String()
	if !strings.Contains(out, "✓ managed") {
		t.Errorf("managed must stay green when a live watcher holds the flock.\nstdout: %s", out)
	}
}
