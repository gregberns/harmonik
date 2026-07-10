package daemon_test

// workloop_stranded_ondisk_hkr9edj_test.go — unit coverage for
// strandedBeadHasOnDiskRun, a follow-up to hk-l2xd1 (hk-r9edj).
//
// strandedBeadHasOnDiskRun gates the stranded in_progress auto-reset: it must
// report true (skip the reset) whenever it cannot be sure no on-disk run
// record exists for the bead, because a false negative races a live
// adoptLiveRunSession goroutine monitoring an independent tmux session. That
// includes the case where run.List itself fails (e.g. .harmonik/runs exists
// but is unreadable) — an error is not evidence of absence, so the function
// must be race-conservative and return true, not false.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// TestHKR9EDJ_StrandedBeadHasOnDiskRun_ListError: when .harmonik/runs cannot
// be listed (a file sits where the directory should be, so os.ReadDir fails
// with something other than ErrNotExist), the function must report true —
// race-conservative — rather than false.
func TestHKR9EDJ_StrandedBeadHasOnDiskRun_ListError(t *testing.T) {
	projectDir := t.TempDir()
	harmonikDir := filepath.Join(projectDir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", harmonikDir, err)
	}
	runsPath := filepath.Join(harmonikDir, "runs")
	// Put a regular file where the runs directory should be, forcing
	// os.ReadDir (and thus run.List) to fail with ENOTDIR rather than
	// ErrNotExist.
	if err := os.WriteFile(runsPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", runsPath, err)
	}

	if got := daemon.ExportedStrandedBeadHasOnDiskRun(projectDir, core.BeadID("hk-r9edj-error-case")); !got {
		t.Errorf("strandedBeadHasOnDiskRun = false on List error; want true (race-conservative)")
	}
}

// TestHKR9EDJ_StrandedBeadHasOnDiskRun_NoRunsDir: a missing .harmonik/runs
// directory is the ordinary "never had a run" case (run.List returns
// nil, nil) — must report false so the stranded-bead auto-reset can proceed.
func TestHKR9EDJ_StrandedBeadHasOnDiskRun_NoRunsDir(t *testing.T) {
	projectDir := t.TempDir()

	if got := daemon.ExportedStrandedBeadHasOnDiskRun(projectDir, core.BeadID("hk-r9edj-missing-dir")); got {
		t.Errorf("strandedBeadHasOnDiskRun = true with no runs dir; want false")
	}
}

// TestHKR9EDJ_StrandedBeadHasOnDiskRun_NoMatchingRecord: runs dir exists and
// lists cleanly but contains no record for beadID — must report false.
func TestHKR9EDJ_StrandedBeadHasOnDiskRun_NoMatchingRecord(t *testing.T) {
	projectDir := t.TempDir()
	runsDir := filepath.Join(projectDir, ".harmonik", "runs")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", runsDir, err)
	}
	otherRecord := `{"run_id":"01900000-0000-7000-8000-000000000099","bead_id":"hk-r9edj-other-bead"}`
	if err := os.WriteFile(filepath.Join(runsDir, "01900000-0000-7000-8000-000000000099.json"), []byte(otherRecord), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := daemon.ExportedStrandedBeadHasOnDiskRun(projectDir, core.BeadID("hk-r9edj-no-match")); got {
		t.Errorf("strandedBeadHasOnDiskRun = true with no matching record; want false")
	}
}

// TestHKR9EDJ_StrandedBeadHasOnDiskRun_MatchingRecord: a run record on disk
// for beadID means a live session may be monitoring it — must report true.
func TestHKR9EDJ_StrandedBeadHasOnDiskRun_MatchingRecord(t *testing.T) {
	projectDir := t.TempDir()
	runsDir := filepath.Join(projectDir, ".harmonik", "runs")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", runsDir, err)
	}
	beadID := core.BeadID("hk-r9edj-live")
	record := `{"run_id":"01900000-0000-7000-8000-000000000042","bead_id":"` + string(beadID) + `"}`
	if err := os.WriteFile(filepath.Join(runsDir, "01900000-0000-7000-8000-000000000042.json"), []byte(record), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := daemon.ExportedStrandedBeadHasOnDiskRun(projectDir, beadID); !got {
		t.Errorf("strandedBeadHasOnDiskRun = false with a matching on-disk record; want true")
	}
}
